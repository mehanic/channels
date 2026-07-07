# 🔍 Глибокий розбір: Helper-функції для тестування `m3u8`

Цей файл містить **універсальні утиліти для тестування**, які спрощують написання ассерцій для поліморфних типів та роботи з рядками у форматі M3U8. Розберемо архітектурно та детально.

---

## 📦 Архітектура файлу: патерн "Test Helpers"

```
┌─────────────────────────────────────┐
│ assertNotNilEqual()                 │
│ • Generic-подібна ассерція для *T   │
│ • Тип-безпека через type switch     │
└─────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────┐
│ assertEqualWithoutNewLine()         │
│ • Нормалізація рядків для порівняння│
│ • Видалення \n для гнучких тестів   │
└─────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────┐
│ assertToString()                    │
│ • Комбінація двох вище для Item     │
│ • Тестування поліморфної серіалізації│
└─────────────────────────────────────┘
```

### 🎯 Навіщо виділяти хелпери?
| Перевага | Пояснення |
|----------|-----------|
| **DRY (Don't Repeat Yourself)** | Один раз написали логіку порівняння `*int` → використовуємо у 50 тестах |
| **Читабельність тестів** | `assertNotNilEqual(t, 4500, br.Length)` зрозуміліше, ніж 5 рядків типу-перевірок |
| **Централізація змін** | Змінили логіку порівняння `*bool` → оновилося у всіх тестах автоматично |
| **Кращі повідомлення про помилки** | Можна додати кастомні повідомлення у одному місці |

---

## 🔬 Детальний розбір кожної функції

### 1️⃣ `assertNotNilEqual` — generic-подібна ассерція для покажчиків

```go
func assertNotNilEqual(t *testing.T, expected interface{}, ptr interface{}) {
    // 🎯 Крок 1: Перевірка, що покажчик не nil
    // Це критично: *T == nil ≠ *T з нульовим значенням!
    assert.NotNil(t, ptr)
    
    // 🎯 Крок 2: Type switch для безпечного дереференсу
    // ✅ Кожен case перевіряє тип через "коміркову змінну" ok
    switch ptr.(type) {
    case *string:
        s, ok := ptr.(*string)  // ✅ Type assertion з перевіркою
        assert.True(t, ok)       // ✅ Дублююча перевірка (параноїдально, але безпечно)
        assert.Equal(t, expected, *s)  // ✅ Порівняння розпакованого значення
        
    case *float64:
        f, ok := ptr.(*float64)
        assert.True(t, ok)
        assert.Equal(t, expected, *f)
        
    case *int:
        i, ok := ptr.(*int)
        assert.True(t, ok)
        assert.Equal(t, expected, *i)
        
    case *bool:
        b, ok := ptr.(*bool)
        assert.True(t, ok)
        assert.Equal(t, expected, *b)
        
    default:
        // 🎯 Крок 3: Захист від невідомих типів
        t.Fatal("not supported assert type")  // ❌ Зупиняє тест негайно
    }
}
```

#### 🎯 Приклад використання
```go
// ❌ Без хелпера (багато шаблонного коду):
func TestSomething(t *testing.T) {
    var ptr *int = pointer.ToInt(42)
    
    assert.NotNil(t, ptr)
    if ptr == nil {
        t.Fatal("ptr is nil")
    }
    actual := *ptr
    assert.Equal(t, 42, actual)
}

// ✅ З хелпером (чітко та лаконічно):
func TestSomething(t *testing.T) {
    var ptr *int = pointer.ToInt(42)
    assertNotNilEqual(t, 42, ptr)  // ✅ Один рядок замість п'яти
}
```

#### ⚠️ Потенційні проблеми
```go
// ❌ Проблема 1: interface{} втрачає type-safety на етапі компіляції
assertNotNilEqual(t, "42", ptr)  // ❌ Очікуємо int, передали string → помилка тільки в runtime!

// ✅ Рішення: використати generics (Go 1.18+)
func assertNotNilEqual[T comparable](t *testing.T, expected T, ptr *T) {
    t.Helper()
    assert.NotNil(t, ptr)
    if ptr != nil {
        assert.Equal(t, expected, *ptr)
    }
}
// Використання: assertNotNilEqual(t, 42, ptr)  // ✅ Типи перевіряються компілятором!

// ❌ Проблема 2: t.Fatal() зупиняє весь тест, навіть якщо є subtests
// ✅ Краще: t.Errorf() для продовження перевірок
default:
    t.Errorf("unsupported pointer type: %T", ptr)  // ✅ Логує помилку, але не зупиняє
    return
```

#### 🎯 Чому `assert.True(t, ok)` після type assertion?
```go
// Type assertion у switch вже гарантує тип, тому ok завжди true
// Це "параноїдальна" перевірка — можна прибрати для чистоти:

case *int:
    i := ptr.(*int)  // ✅ Без ok, бо switch вже перевірив тип
    assert.Equal(t, expected, *i)
```

---

### 2️⃣ `assertEqualWithoutNewLine` — нормалізація рядків для порівняння

```go
func assertEqualWithoutNewLine(t *testing.T, expected string, actual string) {
    // 🎯 Видалення ВСІХ символів \n з очікуваного рядка
    // strings.Replace(text, old, new, -1) = замінити всі входження
    removedNewLine := strings.Replace(expected, "\n", "", -1)
    
    // 🎯 Порівняння "плоских" рядків
    assert.Equal(t, removedNewLine, actual)
}
```

#### 🎯 Навіщо видаляти `\n`?
```
📋 Контекст: M3U8 теги часто містять переноси рядків:
#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00Z\n#EXTINF:4.0,\nseg.ts

🔄 Проблема: 
• Очікуваний рядок може бути записаний у тесті як multiline для читабельності
• А фактичний вивід може мати іншу кількість \n (напр. \r\n на Windows)
• Порівняння "як є" → хибні невдачі тестів

✅ Рішення: нормалізувати обидва рядки перед порівнянням:
• Видалити \n → порівнювати "плоский" контент
• Або: замінити \r\n → \n → trim → порівняти
```

#### ⚠️ Потенційні проблеми
```go
// ❌ Проблема 1: Видаляє \n тільки з expected, але не з actual!
// Якщо actual містить \n, а expected після видалення — ні → помилкове порівняння

// ✅ Рішення: нормалізувати ОБИДВА рядки
func assertEqualWithoutNewLine(t *testing.T, expected, actual string) {
    normalize := func(s string) string {
        s = strings.ReplaceAll(s, "\r\n", "\n")  // Уніфікувати переноси
        s = strings.ReplaceAll(s, "\n", "")       // Видалити всі \n
        return strings.TrimSpace(s)               // Видалити зайві пробіли
    }
    assert.Equal(t, normalize(expected), normalize(actual))
}

// ❌ Проблема 2: strings.Replace застарілий (Go 1.12+)
// ✅ Використовувати strings.ReplaceAll для читабельності:
removedNewLine := strings.ReplaceAll(expected, "\n", "")

// ❌ Проблема 3: Видалення \n може приховати реальні помилки форматування
// Напр.: очікуємо "A\nB", отримали "AB" → тест пройде, але формат зламано!

// ✅ Рішення: використовувати тільки для специфічних випадків,
// або додати окремий тест на наявність/відсутність переносів:
func TestString_HasNewlines(t *testing.T) {
    output := item.String()
    assert.Contains(t, output, "\n", "String() should include newlines between tags")
}
```

---

### 3️⃣ `assertToString` — комбінований хелпер для тестування `Item.String()`

```go
func assertToString(t *testing.T, expected string, item m3u8.Item) {
    // 🎯 Делегування до assertEqualWithoutNewLine
    // m3u8.Item — інтерфейс з методом String() string
    assertEqualWithoutNewLine(t, expected, item.String())
}
```

#### 🎯 Навіщо окремий хелпер для `Item`?
```
✅ Поліморфізм: працює з будь-яким типом, що реалізує m3u8.Item:
• SegmentItem, MediaItem, PlaylistItem, TimeItem, ByteRange, тощо

✅ Єдиний стиль тестування серіалізації:
• Усі тести на String() використовують однакову логіку нормалізації

✅ Легке розширення:
• Якщо потрібно додати логування або додаткові перевірки — змінити в одному місці
```

#### 🎯 Приклад використання у тестах
```go
// ✅ Тест для ByteRange
func TestByteRange_String(t *testing.T) {
    br := &m3u8.ByteRange{Length: pointer.ToInt(4500), Start: pointer.ToInt(200)}
    assertToString(t, "4500@200", br)  // ✅ br реалізує m3u8.Item через *ByteRange?
}

// ✅ Тест для SegmentItem
func TestSegmentItem_String(t *testing.T) {
    seg := &m3u8.SegmentItem{Duration: 4.0, Segment: "seg.ts"}
    assertToString(t, "#EXTINF:4.0,\nseg.ts", seg)  // ✅ \n буде видалено при порівнянні
}

// ⚠️ Увага: ByteRange має реалізувати m3u8.Item для цього хелпера!
// Якщо ні — буде помилка компіляції. Перевірте ієрархію типів.
```

---

## ⚠️ Критичний аналіз: потенційні покращення

### 1️⃣ Додати `t.Helper()` для коректного відображення помилок
```go
// ❌ Без t.Helper(): помилка вказує на рядок у хелпері, а не у тесті
// test_helpers.go:15: Expected "4500", got "4501"

// ✅ З t.Helper(): помилка вказує на реальне місце виклику у тесті
// byte_range_test.go:23: Expected "4500", got "4501"  ← Правильна лінія!

func assertNotNilEqual(t *testing.T, expected interface{}, ptr interface{}) {
    t.Helper()  // ✅ Додати першим рядком у ВСІХ хелперах
    // ... решта коду ...
}
```

### 2️⃣ Замінити `interface{}` на generics (Go 1.18+)
```go
// ✅ Сучасний підхід з type-safety на етапі компіляції:
func assertNotNilEqual[T comparable](t *testing.T, expected T, ptr *T) {
    t.Helper()
    if !assert.NotNil(t, ptr, "pointer should not be nil") {
        return  // Не панікувати, якщо ptr == nil
    }
    assert.Equal(t, expected, *ptr, "pointer value mismatch")
}

// ✅ Використання (тип виводиться автоматично):
var intPtr *int = pointer.ToInt(42)
assertNotNilEqual(t, 42, intPtr)  // ✅ T = int

var strPtr *string = pointer.ToString("hello")
assertNotNilEqual(t, "hello", strPtr)  // ✅ T = string

// ❌ Помилка компіляції при невідповідності типів:
assertNotNilEqual(t, "42", intPtr)  // ❌ compile error: cannot use "42" (untyped string) as int
```

### 3️⃣ Покращити нормалізацію рядків
```go
// ✅ Універсальна функція нормалізації для M3U8:
func normalizeM3U8(s string) string {
    // 1. Уніфікувати переноси рядків (Windows/Unix)
    s = strings.ReplaceAll(s, "\r\n", "\n")
    
    // 2. Видалити зайві пробіли на початку/кінці
    s = strings.TrimSpace(s)
    
    // 3. (Опціонально) Видалити \n для "плоского" порівняння
    //    Або замінити на спеціальний маркер для дебагу:
    // s = strings.ReplaceAll(s, "\n", "␤")
    
    return s
}

func assertEqualM3U8(t *testing.T, expected, actual string) {
    t.Helper()
    assert.Equal(t, normalizeM3U8(expected), normalizeM3U8(actual), 
        "M3U8 content mismatch")
}
```

### 4️⃣ Додати підтримку кастомних повідомлень про помилки
```go
// ✅ Гнучкіші хелпери з format-рядками:
func assertNotNilEqualf[T comparable](t *testing.T, expected T, ptr *T, msg string, args ...interface{}) {
    t.Helper()
    if !assert.NotNil(t, ptr, msg, args...) {
        return
    }
    assert.Equal(t, expected, *ptr, msg, args...)
}

// Використання:
assertNotNilEqualf(t, 42, ptr, "ByteRange.Length for segment %d", seqNum)
// Помилка: "ByteRange.Length for segment 100: expected 42, got 43"
```

### 5️⃣ Додати хелпер для тестування кругової серіалізації (round-trip)
```go
// ✅ Універсальний тест: Parse(text) → String() == text
func assertRoundTrip[T m3u8.Item](t *testing.T, newFunc func(string) (T, error), input string) {
    t.Helper()
    
    item, err := newFunc(input)
    if !assert.NoError(t, err, "failed to parse %q", input) {
        return
    }
    
    output := item.String()
    // Нормалізувати обидва для порівняння
    assert.Equal(t, normalizeM3U8(input), normalizeM3U8(output), 
        "round-trip failed: parse(%q).String() != original", input)
}

// Використання:
assertRoundTrip(t, m3u8.NewByteRange, "4500@200")
assertRoundTrip(t, m3u8.NewTimeItem, "#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00Z")
```

---

## 🔗 Інтеграція у тести вашого CCTV HLS Processor

### 🎯 Сценарій: тестування поліморфної серіалізації плейлиста
```go
func TestPlaylist_Serialization(t *testing.T) {
    pl := m3u8.NewPlaylist()
    pl.Target = 4
    pl.Sequence = 1000
    
    // 🎯 Додавання різних типів Item (поліморфізм)
    pl.AppendItem(&m3u8.MapItem{URI: "init.mp4"})
    pl.AppendItem(&m3u8.TimeItem{Time: time.Now().UTC()})
    pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg1.ts"})
    
    // 🎯 Тестування кожного елемента окремо
    assertToString(t, `#EXT-X-MAP:URI="init.mp4"`, pl.Items[0])
    assertToString(t, `#EXT-X-PROGRAM-DATE-TIME:...`, pl.Items[1])  // \n видалиться
    assertToString(t, `#EXTINF:4.0,\nseg1.ts`, pl.Items[2])
    
    // 🎯 Тестування всього плейлиста
    output, err := m3u8.Write(pl)
    assert.NoError(t, err)
    
    // 🎯 Перевірка наявності ключових тегів (без чутливості до \n)
    assert.Contains(t, output, "#EXT-X-MAP:URI=\"init.mp4\"")
    assert.Contains(t, output, "#EXTINF:4.0")
    assert.Contains(t, output, "seg1.ts")
}
```

### 🎯 Сценарій: тестування опціональних полів через `assertNotNilEqual`
```go
func TestMediaItem_OptionalFields(t *testing.T) {
    // 🎯 Парсинг рядка з опціональними атрибутами
    line := `TYPE=AUDIO,NAME="English",LANGUAGE="en",DEFAULT=YES`
    item, err := m3u8.NewMediaItem(line)
    assert.NoError(t, err)
    
    // 🎯 Перевірка обов'язкових полів (звичайні ассерції)
    assert.Equal(t, "AUDIO", item.Type)
    assert.Equal(t, "English", item.Name)
    
    // 🎯 Перевірка опціональних полів-покажчиків (через хелпер)
    assertNotNilEqual(t, "en", item.Language)    // *string
    assertNotNilEqual(t, true, item.Default)     // *bool
    
    // 🎯 Перевірка відсутніх опціональних полів
    assert.Nil(t, item.Forced)   // Не вказано у вхідному рядку → має бути nil
    assert.Nil(t, item.URI)      // Не потрібно для TYPE=AUDIO у Master Playlist
}
```

### 🎯 Сценарій: тестування кругової серіалізації для всіх типів
```go
func TestAllItems_RoundTrip(t *testing.T) {
    cases := []struct{
        name     string
        input    string
        newItem  func(string) (m3u8.Item, error)
    }{
        {"ByteRange/WithOffset", "4500@200", func(s string) (m3u8.Item, error) {
            return m3u8.NewByteRange(s)
        }},
        {"TimeItem", "#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00Z", func(s string) (m3u8.Item, error) {
            return m3u8.NewTimeItem(s)
        }},
        // ... додати інші типи ...
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            
            item, err := tc.newItem(tc.input)
            if !assert.NoError(t, err) {
                return
            }
            
            output := item.String()
            // Нормалізувати для порівняння
            assert.Equal(t, 
                normalizeM3U8(tc.input), 
                normalizeM3U8(output),
                "round-trip failed for %s", tc.name)
        })
    }
}
```

---

## 🧪 Приклад: повний тест з використанням хелперів

```go
// ✅ Тест для SegmentItem з комплексною перевіркою
func TestSegmentItem_Full(t *testing.T) {
    t.Parallel()
    
    // 🎯 Вхідний рядок для парсингу
    extinfLine := "#EXTINF:4.000,News Segment"
    uri := "https://cdn/seg1000.ts"
    
    // 🎯 Парсинг #EXTINF частини
    seg, err := m3u8.NewSegmentItem(extinfLine)
    assert.NoError(t, err)
    seg.Segment = uri  // URI встановлюється окремо
    
    // 🎯 Перевірка розпаршених полів
    assert.Equal(t, 4.0, seg.Duration)
    assertNotNilEqual(t, "News Segment", seg.Comment)  // ✅ Хелпер для *string
    
    // 🎯 Перевірка серіалізації (з нормалізацією \n)
    expected := `#EXTINF:4.000,News Segment
https://cdn/seg1000.ts`
    assertToString(t, expected, seg)  // ✅ Хелпер для Item.String()
    
    // 🎯 Додаткова перевірка: наявність переносів у виводі
    output := seg.String()
    assert.Contains(t, output, "\n", "String() should separate tag and URI with newline")
    
    // 🎯 Round-trip перевірка (парсинг виводу → порівняння)
    lines := strings.Split(output, "\n")
    assert.GreaterOrEqual(t, len(lines), 2, "output should have at least 2 lines")
    
    seg2, err := m3u8.NewSegmentItem(lines[0])  // Парсинг #EXTINF рядка
    assert.NoError(t, err)
    seg2.Segment = lines[1]  // URI з другого рядка
    
    assert.Equal(t, seg.Duration, seg2.Duration)
    assert.Equal(t, *seg.Comment, *seg2.Comment)
    assert.Equal(t, seg.Segment, seg2.Segment)
}
```

---

## 📋 Best Practices для тестових хелперів у Go

```
✅ Завжди додавати t.Helper() для коректного відображення помилок
✅ Уникати interface{} на користь generics (Go 1.18+) для type-safety
✅ Нормалізувати вхідні дані перед порівнянням (переноси, пробіли, регістр)
✅ Додавати кастомні повідомлення про помилки через msg/args патерн
✅ Тестувати самі хелпери! (мета-тести для інфраструктури тестування)
✅ Документувати призначення кожного хелпера коментарем
✅ Групувати хелпери за доменом: m3u8_helpers.go, http_helpers.go, тощо
```

---

## 🎯 Висновок

Ці хелпери — **практичний інструмент** для спрощення тестування поліморфних типів:

✅ Зменшення шаблонного коду у тестах  
✅ Централізація логіки порівняння покажчиків  
✅ Гнучка робота з рядками формату M3U8

**Для вашого проекту — рекомендовані покращення**:

1. ✅ Додати `t.Helper()` у всі хелпери для кращого дебагу
2. ✅ Замінити `interface{}` на generics при оновленні до Go 1.18+
3. ✅ Покращити нормалізацію рядків: уніфікувати \r\n/\n, trim пробіли
4. ✅ Додати хелпер для round-trip тестування: `Parse → String() == original`
5. ✅ Написати мета-тести для самих хелперів (перевірка їх коректності)

**Приклад міграції на generics**:
```go
// 🔄 До (interface{}):
func assertNotNilEqual(t *testing.T, expected interface{}, ptr interface{}) {
    t.Helper()
    assert.NotNil(t, ptr)
    switch p := ptr.(type) {
    case *int:
        assert.Equal(t, expected, *p)
    // ... інші типи ...
    }
}

// ✅ Після (generics):
func assertNotNilEqual[T comparable](t *testing.T, expected T, ptr *T) {
    t.Helper()
    if !assert.NotNil(t, ptr) {
        return
    }
    assert.Equal(t, expected, *ptr)
}
// ✅ Типи перевіряються компілятором, код коротший, безпечніший!
```

Потрібно допомогти з:
- 🧠 Міграцією хелперів на generics?
- 🔗 Написанням мета-тестів для перевірки самих хелперів?
- 🧪 Розширенням набору хелперів для специфічних сценаріїв HLS (напр. порівняння плейлистів з ігноруванням порядку тегів)?

Чекаю на ваші питання! 🛠️🧪