# 🔍 Глибокий розбір тестів: `ByteRange` у пакеті `m3u8`

Цей файл містить **юніт-тести** для типу `ByteRange`, який реалізує парсинг та серіалізацію атрибута `BYTERANGE` у форматі HLS. Розберемо архітектуру тестування, покриття сценаріїв та best practices.

---

## 📦 Архітектура тестового файлу

```go
package test  // ⚠️ Рекомендація: має бути package m3u8_test для ізоляції

import (
    "testing"  // Стандартний фреймворк для тестів у Go
    
    "github.com/AlekSi/pointer"  // Helper для створення *int, *string тощо
    "github.com/etherlabsio/go-m3u8/m3u8"  // Тестований пакет
    "github.com/stretchr/testify/assert"  // Бібліотека для зручних ассерцій
)
```

### 🎯 Чому такі залежності?
| Залежність | Призначення | Альтернатива |
|------------|-------------|--------------|
| `testing` | Базовий фреймворк: `t.Run()`, `t.Error()`, паралельні тести | Вбудований, немає альтернативи |
| `stretchr/testify/assert` | Зручні ассерції: `assert.Equal()`, `assert.Nil()` | Стандартні `if x != y { t.Errorf(...) }` |
| `AlekSi/pointer` | Helper: `pointer.ToInt(4500)` замість ручного створення `*int` | Власний helper у пакеті |

### 🔍 Чому `package test`, а не `m3u8_test`?
```go
// ❌ Поточний варіант:
package test  // Може бути будь-яким, але не інформативним

// ✅ Рекомендований варіант:
package m3u8_test  // Чітко вказує: це тести ДЛЯ пакету m3u8

// Переваги m3u8_test:
// • Тести працюють через публічний API (як зовнішній клієнт)
// • Не мають доступу до приватних полів/функцій (encapsulation)
// • Краща документація: що публічно, а що — внутрішня реалізація
```

---

## 🧪 Структура тестів: патерн "Parse ↔ New ↔ String"

### 🔄 Цикл тестування серіалізації/десеріалізації
```
┌─────────────────────────────────────┐
│ Тест 1: Parse "4500@600"            │
│ • Вхідний рядок → NewByteRange()    │
│ • Перевірка полів: Length=4500, Start=600 │
│ • Кругова перевірка: br.String() == "4500@600" │
└─────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────┐
│ Тест 2: Parse "4500" (без offset)   │
│ • Вхідний рядок → NewByteRange()    │
│ • Перевірка: Length=4500, Start=nil │
│ • Кругова перевірка: br.String() == "4500" │
└─────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────┐
│ Тест 3: New + String (з offset)     │
│ • Створення об'єкта вручну          │
│ • Перевірка серіалізації: "4500@200"│
└─────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────┐
│ Тест 4: New + String (без offset)   │
│ • Створення об'єкта вручну          │
│ • Перевірка серіалізації: "4500"    │
└─────────────────────────────────────┘
```

### 🎯 Навіщо таке покриття?
```
✅ Парсинг різних форматів вводу:
   • "4500@600" → повний формат (length@offset)
   • "4500"     → скорочений формат (offset = початок файлу)

✅ Серіалізація різних станів об'єкта:
   • {Length: 4500, Start: 200} → "4500@200"
   • {Length: 4500, Start: nil} → "4500"

✅ Кругова перевірка (round-trip):
   • Parse(text) → String() == text
   • Це гарантує: парсинг і серіалізація — взаємно обернені операції
```

---

## 🔬 Детальний розбір кожного тесту

### Тест 1: `TestByteRange_Parse` — повний формат "length@offset"
```go
func TestByteRange_Parse(t *testing.T) {
    text := "4500@600"  // 🎯 Вхідний рядок: 4500 байт, починаючи з позиції 600
    
    // 🎯 Крок 1: Парсинг
    br, err := m3u8.NewByteRange(text)
    
    // 🎯 Крок 2: Перевірка відсутності помилок
    assert.Nil(t, err)  // ✅ Парсинг успішний
    
    // 🎯 Крок 3: Валідація структури результату
    assert.NotNil(t, br.Length)  // ✅ Length завжди має бути вказаний
    assert.NotNil(t, br.Start)   // ✅ Start є, бо був у вхідному рядку
    
    // 🎯 Крок 4: Перевірка значень
    assert.Equal(t, 4500, *br.Length)  // ✅ Дереференс *int для порівняння
    assert.Equal(t, 600, *br.Start)    // ✅ Offset = 600 байт
    
    // 🎯 Крок 5: Кругова перевірка (round-trip)
    assertToString(t, text, br)  // ✅ br.String() має повернути оригінальний "4500@600"
}
```

### Тест 2: `TestByteRange_Parse_2` — скорочений формат "length"
```go
func TestByteRange_Parse_2(t *testing.T) {
    text := "4500"  // 🎯 Тільки довжина, offset не вказано
    
    br, err := m3u8.NewByteRange(text)
    
    assert.Nil(t, err)
    assert.NotNil(t, br.Length)  // ✅ Length обов'язковий
    assert.Nil(t, br.Start)      // ✅ Start = nil, бо не був у вхідному рядку
    
    assert.Equal(t, 4500, *br.Length)
    
    assertToString(t, text, br)  // ✅ br.String() == "4500"
}
```

### Тест 3: `TestByteRange_New` — створення об'єкта + серіалізація
```go
func TestByteRange_New(t *testing.T) {
    // 🎯 Створення об'єкта вручну через pointer helper
    br := &m3u8.ByteRange{
        Length: pointer.ToInt(4500),  // ✅ Замість: tmp := 4500; Length: &tmp
        Start:  pointer.ToInt(200),   // ✅ Cleaner code для тестів
    }
    
    // 🎯 Перевірка серіалізації
    assert.Equal(t, "4500@200", br.String())  // ✅ Очікуваний формат виводу
}
```

### Тест 4: `TestByteRange_New_2` — серіалізація без offset
```go
func TestByteRange_New_2(t *testing.T) {
    br := &m3u8.ByteRange{
        Length: pointer.ToInt(4500),
        // Start = nil за замовчуванням
    }
    
    // 🎯 Перевірка: nil Start не виводиться у рядку
    assert.Equal(t, "4500", br.String())  // ✅ Без "@200"
}
```

---

## 🛠️ Helper-функція `assertToString` (припустима реалізація)

```go
// ✅ Ця функція не показана у коді, але використовується у тестах
// Ймовірна реалізація:
func assertToString(t *testing.T, expected string, br *m3u8.ByteRange) {
    t.Helper()  // ✅ Важливо: позначає функцію як helper для коректного відображення ліній помилок
    
    actual := br.String()
    assert.Equal(t, expected, actual, 
        "ByteRange.String() should return original format for round-trip")
    
    // ✅ Додатково: перевірка, що парсинг результату дає той самий об'єкт
    br2, err := m3u8.NewByteRange(actual)
    assert.Nil(t, err, "String() output should be parseable")
    
    assert.Equal(t, *br.Length, *br2.Length, "Length should match after round-trip")
    if br.Start != nil {
        assert.NotNil(t, br2.Start, "Start should not be nil after round-trip")
        assert.Equal(t, *br.Start, *br2.Start, "Start should match after round-trip")
    } else {
        assert.Nil(t, br2.Start, "Start should remain nil after round-trip")
    }
}
```

### 🎯 Навіщо `t.Helper()`?
```go
// Без t.Helper():
// ❌ Помилка показує рядок у assertToString(), а не у тесті
// test.go:25: Expected "4500@200", got "4500"

// З t.Helper():
// ✅ Помилка показує реальне місце виклику у тесті
// test.go:15: Expected "4500@200", got "4500"  ← Правильна лінія!
```

---

## ⚠️ Критичний аналіз: що можна покращити

### 1️⃣ Назви тестів: нумерація замість опису
```go
// ❌ Поточні назви:
TestByteRange_Parse_2      // Що саме тестується у "2"?
TestByteRange_New_2        // Чим відрізняється від "New"?

// ✅ Рекомендовані назви (subtests або описові):
func TestByteRange_Parse_WithOffset(t *testing.T)      // "4500@600"
func TestByteRange_Parse_WithoutOffset(t *testing.T)    // "4500"
func TestByteRange_String_WithOffset(t *testing.T)      // {4500, 200} → "4500@200"
func TestByteRange_String_WithoutOffset(t *testing.T)   // {4500, nil} → "4500"

// ✅ Або використання subtests для групування:
func TestByteRange(t *testing.T) {
    t.Run("Parse/WithOffset", func(t *testing.T) { ... })
    t.Run("Parse/WithoutOffset", func(t *testing.T) { ... })
    t.Run("String/WithOffset", func(t *testing.T) { ... })
    t.Run("String/WithoutOffset", func(t *testing.T) { ... })
}
```

### 2️⃣ Відсутність тестів на помилки
```go
// ❌ Не тестуються невалідні вхідні дані:
// • "" (порожній рядок)
// • "abc" (не число)
// • "4500@" (неповний формат)
// • "@600" (відсутня довжина)
// • "-4500" (від'ємне значення)

// ✅ Додати тести на валідацію:
func TestByteRange_Parse_Invalid(t *testing.T) {
    cases := []struct{
        name  string
        input string
        wantErr bool
    }{
        {"empty", "", true},
        {"non-numeric", "abc", true},
        {"negative_length", "-4500", true},
        {"incomplete", "4500@", true},
        {"valid_no_offset", "4500", false},
        {"valid_with_offset", "4500@600", false},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            br, err := m3u8.NewByteRange(tc.input)
            if tc.wantErr {
                assert.Error(t, err, "expected error for input %q", tc.input)
                assert.Nil(t, br)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, br)
            }
        })
    }
}
```

### 3️⃣ Відсутність тестів на граничні значення
```go
// ✅ Додати перевірки для:
func TestByteRange_EdgeCases(t *testing.T) {
    cases := []struct{
        name     string
        input    string
        wantLen  int
        wantStart *int  // nil або значення
    }{
        {"zero_length", "0", 0, nil},
        {"zero_offset", "100@0", 100, pointer.ToInt(0)},
        {"large_values", "999999999@888888888", 999999999, pointer.ToInt(888888888)},
        {"max_int", fmt.Sprintf("%d", math.MaxInt32), math.MaxInt32, nil},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            br, err := m3u8.NewByteRange(tc.input)
            assert.NoError(t, err)
            assert.Equal(t, tc.wantLen, *br.Length)
            if tc.wantStart != nil {
                assert.NotNil(t, br.Start)
                assert.Equal(t, *tc.wantStart, *br.Start)
            } else {
                assert.Nil(t, br.Start)
            }
        })
    }
}
```

### 4️⃣ Відсутність fuzz-тестів (Go 1.18+)
```go
// ✅ Додати fuzzing для пошуку неочікуваних вхідних даних:
func FuzzByteRange_Parse(f *testing.F) {
    // 🎯 Seed corpus: відомі валідні/невалідні приклади
    f.Add("4500")
    f.Add("4500@600")
    f.Add("")
    f.Add("abc")
    
    f.Fuzz(func(t *testing.T, input string) {
        br, err := m3u8.NewByteRange(input)
        
        // 🎯 Якщо парсинг успішний — перевірити round-trip
        if err == nil && br != nil {
            output := br.String()
            br2, err2 := m3u8.NewByteRange(output)
            
            assert.NoError(t, err2)
            assert.Equal(t, *br.Length, *br2.Length)
            if br.Start != nil {
                assert.Equal(t, *br.Start, *br2.Start)
            }
        }
    })
}
// 🚀 Запуск: go test -fuzz=FuzzByteRange_Parse -fuzztime=30s
```

### 5️⃣ Паралельне виконання тестів
```go
// ✅ Додати t.Parallel() для прискорення прогону:
func TestByteRange_Parse(t *testing.T) {
    t.Parallel()  // ✅ Дозволяє виконувати цей тест паралельно з іншими
    // ... решта коду ...
}

// ⚠️ Але: якщо тести залежать від спільного стану — не використовувати!
// У цьому випадку тести ізольовані → безпечно паралелізувати
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **partial fetch сегментів**:

### 🎯 Сценарій: тестування ByteRange для fMP4 init-файлу
```go
// У вашому пакеті тестів для segmentFinalizer:
func TestSegmentFinalizer_ByteRangeForInitFile(t *testing.T) {
    // 🎯 Init-файл: moov box на початку, дані після
    // Припустимо: moov = 1880 байт, починаючи з 0
    br := &m3u8.ByteRange{
        Length: pointer.ToInt(1880),
        Start:  pointer.ToInt(0),
    }
    
    // 🎯 Перевірка серіалізації для плейлиста
    expected := "1880@0"
    assert.Equal(t, expected, br.String())
    
    // 🎯 Інтеграція у SegmentItem
    seg := &m3u8.SegmentItem{
        Duration:  4.0,
        Segment:   "init.mp4",
        ByteRange: br,  // 🔗 Прив'язка ByteRange до сегмента
    }
    
    // 🎯 Перевірка повної серіалізації сегмента
    output := seg.String()
    assert.Contains(t, output, "#EXT-X-BYTERANGE:1880@0")
    assert.Contains(t, output, "init.mp4")
}
```

### 🎯 Сценарій: тестування partial fetch для live-сегментів
```go
func TestPartialFetch_ByteRangeCalculation(t *testing.T) {
    // 🎯 Сценарій: сегмент 4с, бітрейт 1Mbps → ~500KB
    // Клієнт хоче завантажити тільки перші 100KB для швидкого старту
    
    const (
        SegmentSize   = 500 * 1024  // 500KB
        PartialSize   = 100 * 1024  // 100KB для швидкого старту
    )
    
    br := &m3u8.ByteRange{
        Length: pointer.ToInt(PartialSize),
        Start:  pointer.ToInt(0),  // Початок файлу
    }
    
    // 🎯 Перевірка: ByteRange коректно формується
    assert.Equal(t, "102400", br.String())  // 100*1024 = 102400
    
    // 🎯 Імітація HTTP Range request
    rangeHeader := fmt.Sprintf("bytes=0-%d", PartialSize-1)
    assert.Equal(t, "bytes=0-102399", rangeHeader)
    
    // 🎯 Перевірка: сервер поверне правильний Content-Range
    // (це вже тест для HTTP-хендлера, але пов'язано з ByteRange)
}
```

### 🎯 Сценарій: валідація ByteRange у pipeline
```go
// У segmentFinalizer перед додаванням сегмента:
func (sf *SegmentFinalizer) validateByteRange(br *m3u8.ByteRange, fileSize int64) error {
    if br == nil {
        return nil  // ByteRange опціональний
    }
    
    if br.Length == nil {
        return fmt.Errorf("ByteRange.Length is required")
    }
    
    if *br.Length <= 0 {
        return fmt.Errorf("ByteRange.Length must be positive, got %d", *br.Length)
    }
    
    // 🎯 Якщо Start вказаний — перевірити, що не виходить за межі файлу
    if br.Start != nil {
        endPos := int64(*br.Start) + int64(*br.Length)
        if endPos > fileSize {
            return fmt.Errorf("ByteRange [%d@%d] exceeds file size %d", 
                *br.Length, *br.Start, fileSize)
        }
    }
    
    return nil
}

// ✅ Тест для цієї валідації:
func TestValidateByteRange(t *testing.T) {
    sf := &SegmentFinalizer{}
    
    cases := []struct{
        name     string
        br       *m3u8.ByteRange
        fileSize int64
        wantErr  bool
    }{
        {"nil_br", nil, 1000, false},
        {"valid", &m3u8.ByteRange{Length: pointer.ToInt(100), Start: pointer.ToInt(0)}, 1000, false},
        {"negative_length", &m3u8.ByteRange{Length: pointer.ToInt(-10)}, 1000, true},
        {"exceeds_file", &m3u8.ByteRange{Length: pointer.ToInt(500), Start: pointer.ToInt(800)}, 1000, true},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            err := sf.validateByteRange(tc.br, tc.fileSize)
            if tc.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

---

## 🧪 Приклад: розширені тести для `ByteRange`

```go
// ✅ Повний набір тестів з subtests та валідацією помилок:
func TestByteRange(t *testing.T) {
    t.Parallel()
    
    t.Run("Parse/Valid/WithOffset", func(t *testing.T) {
        t.Parallel()
        br, err := m3u8.NewByteRange("4500@600")
        assert.NoError(t, err)
        assert.Equal(t, 4500, *br.Length)
        assert.Equal(t, 600, *br.Start)
        assert.Equal(t, "4500@600", br.String())
    })
    
    t.Run("Parse/Valid/WithoutOffset", func(t *testing.T) {
        t.Parallel()
        br, err := m3u8.NewByteRange("4500")
        assert.NoError(t, err)
        assert.Equal(t, 4500, *br.Length)
        assert.Nil(t, br.Start)
        assert.Equal(t, "4500", br.String())
    })
    
    t.Run("Parse/Invalid/Empty", func(t *testing.T) {
        t.Parallel()
        br, err := m3u8.NewByteRange("")
        assert.Error(t, err)
        assert.Nil(t, br)
    })
    
    t.Run("Parse/Invalid/NonNumeric", func(t *testing.T) {
        t.Parallel()
        br, err := m3u8.NewByteRange("abc@def")
        assert.Error(t, err)
        assert.Nil(t, br)
    })
    
    t.Run("Parse/Invalid/NegativeLength", func(t *testing.T) {
        t.Parallel()
        br, err := m3u8.NewByteRange("-4500")
        assert.Error(t, err)  // Очікуємо помилку валідації
        assert.Nil(t, br)
    })
    
    t.Run("RoundTrip/WithOffset", func(t *testing.T) {
        t.Parallel()
        original := "1234@5678"
        br, err := m3u8.NewByteRange(original)
        assert.NoError(t, err)
        assert.Equal(t, original, br.String())
    })
    
    t.Run("RoundTrip/WithoutOffset", func(t *testing.T) {
        t.Parallel()
        original := "9999"
        br, err := m3u8.NewByteRange(original)
        assert.NoError(t, err)
        assert.Equal(t, original, br.String())
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — вимоги до BYTERANGE

```
✅ Формат: "N[@O]" де:
   • N = довжина у байтах (обов'язкова, додатне ціле)
   • O = зміщення від початку файлу (опціональне, додатне ціле)
   • Роздільник: символ "@" без пробілів

✅ Приклади валідних значень:
   • "1000"      → 1000 байт з початку файлу
   • "1000@500"  → 1000 байт, починаючи з позиції 500
   • "0@0"       → порожній діапазон (спеціальний випадок)

✅ Семантика:
   • Якщо O відсутнє: за замовчуванням 0 (початок файлу)
   • Якщо O вказане: завантажувати байти [O, O+N)
   • Кінець діапазону: O + N (не включаючи)

✅ Використання у HLS:
   • #EXT-X-BYTERANGE:1000@500 (перед #EXTINF)
   • Для partial fetch великих файлів (init.mp4, сегменти)
   • Економія трафіку при повторному завантаженні

✅ Обмеження:
   • N та O мають бути невід'ємними цілими
   • O + N не має перевищувати розмір файлу (валідація на стороні сервера)
   • Плеєри можуть ігнорувати BYTERANGE, якщо не підтримують partial requests
```

---

## 🎯 Висновок

Ці тести — **солідна основа** для валідації `ByteRange`:

✅ Покриття основних сценаріїв парсингу/серіалізації  
✅ Використання testify для читабельних ассерцій  
✅ Кругова перевірка (round-trip) для гарантії консистентності

**Для вашого проекту — рекомендовані покращення**:

1. ✅ Перейменувати тести за описовим патерном (`Parse_WithOffset` замість `Parse_2`)
2. ✅ Додати тести на невалідний ввід (помилки парсингу)
3. ✅ Додати граничні випадки (0, великі числа, переповнення)
4. ✅ Використати subtests для кращої організації та паралелізації
5. ✅ Розглянути fuzz-тести для пошуку неочікуваних багів

**Приклад інтеграції у ваш pipeline**:
```go
// У segmentFinalizer при створенні сегмента з ByteRange:
func (sf *SegmentFinalizer) createSegmentWithByteRange(seqNum int, offset, length int) (*m3u8.SegmentItem, error) {
    br := &m3u8.ByteRange{
        Length: pointer.ToInt(length),
    }
    if offset > 0 {
        br.Start = pointer.ToInt(offset)
    }
    
    // ✅ Валідація перед використанням
    if err := sf.validateByteRange(br, sf.fileSize); err != nil {
        return nil, fmt.Errorf("invalid ByteRange for seg %d: %w", seqNum, err)
    }
    
    return &m3u8.SegmentItem{
        Duration:  sf.segmentDuration,
        Segment:   fmt.Sprintf("seg%d.ts", seqNum),
        ByteRange: br,
    }, nil
}
```

Потрібно допомогти з:
- 🧠 Розширенням тестів на субтести та fuzzing?
- 🔗 Інтеграцією валідації `ByteRange` у ваш `segmentFinalizer`?
- 🧪 Написанням бенчмарків для парсингу/серіалізації?

Чекаю на ваші питання! 🛠️🧪