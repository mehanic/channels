# 🧪 Тести MP4: `mockBox` та `TestFullBoxFlags` — Повний розбір

Це **юніт-тести** для бібліотеки `go-mp4`, які перевіряють:
1. **`mockBox`** — мок-реалізація інтерфейсу для тестування динамічних полів
2. **`TestFullBoxFlags`** — перевірка бітових операцій з прапорами `FullBox`

---

## 🎭 Частина 1: `mockBox` — Мок для тестування динамічних полів

### 🔹 Навіщо потрібен `mockBox`?

Інтерфейс `ICustomFieldObject` визначає методи для роботи з **динамічними полями** (розмір яких залежить від інших полів):

```go
type ICustomFieldObject interface {
    GetFieldSize(name string, ctx Context) uint
    GetFieldLength(name string, ctx Context) uint
    IsOptFieldEnabled(name string, ctx Context) bool
    // ... інші методи ...
}
```

**Проблема**: Як протестувати парсер, якщо реальні бокси мають складну логіку обчислення розмірів?

**Рішення**: Створити **мок** — тестову заглушку, яка повертає передбачувані значення!

---

### 🔹 Структура `mockBox`

```go
type mockBox struct {
    Type         BoxType              // Тип боксу для ідентифікації
    DynSizeMap   map[string]uint      // "поле" → розмір у бітах
    DynLenMap    map[string]uint      // "поле" → довжина (кількість елементів)
    DynOptMap    map[string]bool      // "поле" → чи увімкнено опціональне поле
    IsPStringMap map[string]bool      // "поле" → чи це Pascal-рядок (з довжиною)
}
```

> 🎯 **Ідея**: Замість складної логіки — просто **мапи з передбачуваними значеннями**.

---

### 🔹 Реалізація методів інтерфейсу

#### ✅ `GetType()` — повертає тип боксу
```go
func (m *mockBox) GetType() BoxType {
    return m.Type  // Просто повертаємо те, що записали при створенні
}
```

#### ✅ `GetFieldSize()` — розмір динамічного поля
```go
func (m *mockBox) GetFieldSize(n string, ctx Context) uint {
    if s, ok := m.DynSizeMap[n]; !ok {
        // ❌ Поле не знайдено в мапі → тест має "впасти" з чіткою помилкою
        panic(fmt.Errorf("invalid name of dynamic-size field: %s", n))
    } else {
        // ✅ Поле є → повертаємо заздалегідь задане значення
        return s
    }
}
```

**Приклад використання в тесті:**
```go
mock := &mockBox{
    DynSizeMap: map[string]uint{
        "ConfigOBUs": 256,  // поле "ConfigOBUs" має розмір 256 біт
        "Data":       128,  // поле "Data" має розмір 128 біт
    },
}

size := mock.GetFieldSize("ConfigOBUs", ctx)  // поверне 256 ✅
size := mock.GetFieldSize("Unknown", ctx)     // panic! ❌ (як і має бути в тесті)
```

#### ✅ `GetFieldLength()` — довжина масиву/слайсу
```go
func (m *mockBox) GetFieldLength(n string, ctx Context) uint {
    if l, ok := m.DynLenMap[n]; !ok {
        panic(fmt.Errorf("invalid name of dynamic-length field: %s", n))
    }
    return l
}
```

**Навіщо окремо `Size` і `Length`?**
- `Size` — розмір **одного елемента** у бітах (напр., `uint32` = 32 біти)
- `Length` — **кількість елементів** у масиві (напр., 10 семплів у `trun`)

#### ✅ `IsOptFieldEnabled()` — чи є опціональне поле?
```go
func (m *mockBox) IsOptFieldEnabled(n string, ctx Context) bool {
    if enabled, ok := m.DynOptMap[n]; !ok {
        panic(fmt.Errorf("invalid name of dynamic-opt field: %s", n))
    }
    return enabled
}
```

**Приклад з реальним боксом `Tfhd`:**
```
📦 tfhd (Track Fragment Header) має опціональні поля:
• base-data-offset-present (flag 0x01)
• sample-description-index-present (flag 0x02)
• default-sample-duration-present (flag 0x08)
• ...

Якщо flags = 0x09 (біти 0 і 3 увімкнені):
→ IsOptFieldEnabled("BaseDataOffset") → true
→ IsOptFieldEnabled("DefaultSampleDuration") → true
→ IsOptFieldEnabled("SampleDescriptionIndex") → false
```

У моці це просто:
```go
mock := &mockBox{
    DynOptMap: map[string]bool{
        "BaseDataOffset":          true,
        "DefaultSampleDuration":   true,
        "SampleDescriptionIndex":  false,
    },
}
```

#### ✅ `IsPString()` — чи це рядок з довжиною?
```go
func (m *mockBox) IsPString(name string, bytes []byte, remainingSize uint64, ctx Context) bool {
    if b, ok := m.IsPStringMap[name]; ok {
        return b  // повертаємо задане значення
    }
    return true  // за замовчуванням — так, це P-рядок
}
```

**Що таке P-рядок (Pascal string)?**
```
🔤 Звичайний C-рядок (нуль-термінований):
   "hello" → [68 65 6C 6C 6F 00]  // 6 байт, останній = 0

🔤 P-рядок (з довжиною на початку):
   "hello" → [05 68 65 6C 6C 6F]  // 6 байт, перший = довжина (5)
```

У MP4 деякі текстові поля використовують P-формат — мок дозволяє тестувати обидва варіанти.

---

## 🚩 Частина 2: `TestFullBoxFlags` — Тест бітових операцій

### 🔹 Контекст: Що таке `FullBox`?

```go
type FullBox struct {
    BaseCustomFieldObject
    Version uint8   `mp4:"0,size=8"`      // 1 байт: версія (зазвичай 0 або 1)
    Flags   [3]byte `mp4:"1,size=8"`      // 3 байти: прапори (24 біти)
}
```

**Стандарт ISO/IEC 14496-12**: `FullBox` — базовий тип для багатьох боксів, де перші 4 байти даних — це `version` + `flags`.

---

### 🔹 Методи роботи з прапорами

```go
// Отримати 24-бітне значення прапорів з [3]byte
func (box *FullBox) GetFlags() uint32 {
    flag := uint32(box.Flags[0]) << 16  // старший байт → біти 23-16
    flag ^= uint32(box.Flags[1]) << 8   // середній байт → біти 15-8
    flag ^= uint32(box.Flags[2])        // молодший байт → біти 7-0
    return flag
}

// Встановити прапори з uint32 у [3]byte
func (box *FullBox) SetFlags(flags uint32) {
    box.Flags[0] = byte(flags >> 16)  // виділити старші 8 біт
    box.Flags[1] = byte(flags >> 8)   // виділити середні 8 біт
    box.Flags[2] = byte(flags)        // виділити молодші 8 біт
}

// Додати прапор (бітова OR)
func (box *FullBox) AddFlag(flag uint32) {
    box.SetFlags(box.GetFlags() | flag)
}

// Видалити прапор (бітова AND з інверсією)
func (box *FullBox) RemoveFlag(flag uint32) {
    box.SetFlags(box.GetFlags() & (^flag))
}
```

> 🎯 **Ключове**: Прапори зберігаються як **3 окремих байти**, але працюємо з ними як з **24-бітним цілим**.

---

### 🔹 Покроковий розбір тесту

```go
func TestFullBoxFlags(t *testing.T) {
    box := FullBox{}  // створюємо порожній бокс
    
    // 🔹 Крок 1: Встановити прапори 0x35ac68
    box.SetFlags(0x35ac68)
    
    // 🔹 Крок 2: Перевірити, що байти розставлені правильно (big-endian)
    assert.Equal(t, byte(0x35), box.Flags[0])  // старший байт: 0x35
    assert.Equal(t, byte(0xac), box.Flags[1])  // середній: 0xac
    assert.Equal(t, byte(0x68), box.Flags[2])  // молодший: 0x68
    
    // 🔹 Крок 3: Перевірити зворотне перетворення
    assert.Equal(t, uint32(0x35ac68), box.GetFlags())  // має повернути те саме!
    
    // 🔹 Крок 4: Додати прапор 0x030000 (біти 17-18)
    // 0x35ac68 | 0x030000 = 0x37ac68
    box.AddFlag(0x030000)
    assert.Equal(t, uint32(0x37ac68), box.GetFlags())
    
    // 🔹 Крок 5: Видалити прапор 0x000900 (біти 8 і 11)
    // 0x37ac68 & ~0x000900 = 0x37ac68 & 0xfff6ff = 0x37a468
    box.RemoveFlag(0x000900)
    assert.Equal(t, uint32(0x37a468), box.GetFlags())
}
```

---

### 🔹 Візуалізація бітових операцій

```
🔢 Початкове значення: 0x35ac68
   Бінарно: 0011 0101  1010 1100  0110 1000
            ↑байт 0↑   ↑байт 1↑   ↑байт 2↑

🔹 SetFlags(0x35ac68):
   box.Flags[0] = 0x35  // 0011 0101
   box.Flags[1] = 0xac  // 1010 1100
   box.Flags[2] = 0x68  // 0110 1000

🔹 AddFlag(0x030000):
   0x35ac68  0011 0101 1010 1100 0110 1000
   0x030000  0000 0011 0000 0000 0000 0000
   OR        --------------------------------
   0x37ac68  0011 0111 1010 1100 0110 1000  ✅

🔹 RemoveFlag(0x000900):
   0x37ac68  0011 0111 1010 1100 0110 1000
   ~0x000900 1111 1111 1111 0110 1111 1111  (інверсія)
   AND       --------------------------------
   0x37a468  0011 0111 1010 0100 0110 1000  ✅
```

---

## 🎯 Чому цей тест критичний для вашого HLS-процесора?

### 🔹 Сценарій 1: Коректна інтерпретація `tfhd` боксу

```
📦 traf → tfhd (Track Fragment Header):
   flags = 0x000001 → є поле base-data-offset
   flags = 0x000002 → є поле sample-description-index
   flags = 0x000008 → є поле default-sample-duration

❌ Помилка в бітовій операції:
   • Прочитаєте "зайве" поле → зсув на 4 байти
   • Пропустите потрібне поле → неправильні таймстемпи
   • Результат: десинхронізація аудіо/відео у плеєрі
```

### 🔹 Сценарій 2: Модифікація прапорів при перезаписі

```
📝 Ви хочете додати синхронізацію до існуючого сегмента:
1. Прочитали tfhd з flags = 0x000000
2. Додали прапорець 0x000008 (default-sample-duration-present)
3. Записали назад через SetFlags()

✅ Тест гарантує, що бітова логіка працює коректно
```

### 🔹 Сценарій 3: Валідація вхідних даних

```
⚠️ Камера надсилає пошкоджений fMP4:
   • flags = 0xFFFFFF (всі біти увімкнені)
   • Але деякі поля відсутні у даних

✅ Ваш парсер, використовуючи IsOptFieldEnabled(), 
   коректно пропустить відсутні поля замість panic
```

---

## 🧪 Як написати свій тест з `mockBox`?

```go
func TestParseDynamicBox(t *testing.T) {
    // 1. Створити мок з передбачуваними значеннями
    mock := &mockBox{
        Type: StrToBoxType("test"),
        DynSizeMap: map[string]uint{
            "VariableData": 64,  // поле має фіксований розмір 64 біти
        },
        DynOptMap: map[string]bool{
            "OptionalField": true,  // це поле має бути присутнє
        },
    }
    
    // 2. Викликати парсер з моком замість реального боксу
    // (припустимо, у вас є функція ParseBox, що приймає ICustomFieldObject)
    result, err := ParseBox(mock, someReader, someContext)
    
    // 3. Перевірити результат
    require.NoError(t, err)
    assert.Equal(t, expectedValue, result.VariableData)
}
```

> 🎯 **Перевага**: Тест не залежить від реальних даних — ви контролюєте всі вхідні параметри.

---

## ⚠️ Поширені помилки при роботі з прапорами

| Помилка | Наслідок | Як уникнути |
|---------|----------|-------------|
| Використання `<< 8` замість `<< 16` для старшого байта | Прапори зсуваються на 8 біт | Пам'ятайте порядок: `[0]→16, [1]→8, [2]→0` |
| Забути `^` при `RemoveFlag` | Ви видаляєте НЕ ті біти | `flags & (^mask)` — інверсія обов'язкова! |
| Використання `int` замість `uint32` | Проблеми зі знаковою арифметикою | Завжди `uint32` для бітових операцій |
| Ігнорування `IsOptFieldEnabled` у парсері | Читаєте відсутні поля → помилка | Завжди перевіряйте перед читанням опціонального поля |

---

## 📋 Чекліст для вашого проекту

```
[ ] Переконайтеся, що тести проходять: go test ./mp4 -run TestFullBoxFlags -v

[ ] При парсингу tfhd/trun завжди перевіряйте прапори перед читанням полів:
    if tfhd.CheckFlag(0x000008) {
        // читати default-sample-duration
    }

[ ] При модифікації прапорів використовуйте AddFlag/RemoveFlag, 
    а не пряме присвоєння:
    box.AddFlag(0x000008)  // ✅
    box.Flags[2] |= 0x08   // ❌ (менш читабельно, легко помилитися)

[ ] Для тестування власних боксів з динамічними полями:
    • Створіть mockBox з потрібними мапами
    • Протестуйте парсинг без залежності від реальних файлів

[ ] Додайте логування для дебагу прапорів:
    log.Printf("🚩 tfhd flags: 0x%06x (binary: %024b)", 
        flags, flags)  // побачите, які біти увімкнені
```

---

## 🎯 Висновок

> **`mockBox` + `TestFullBoxFlags` = ваша гарантія коректної роботи з бітами MP4**.  
> Вони забезпечують:
> • ✅ Тестування динамічних полів без реальних файлів
> • ✅ Перевірку бітової арифметики для прапорів
> • ✅ Захист від десинхронізації при парсингу fMP4

Для вашого **CCTV HLS Processor** це означає:
- 🎥 Стабільна обробка таймстемпів з `trun` боксів
- 🔧 Безпечна модифікація прапорів при синхронізації
- 🧪 Можливість тестувати нові бокси без запуску повного конвеєра

Потребуєте допомоги з написанням тесту для конкретного боксу (`trun`, `tfhd`, `moof`) або з інтеграцією мока у ваш парсер? Напишіть — покажу готовий код! 🚀🎬