# 🎭 `IAnyType` / `AnyTypeBox`: Динамічні типи боксів у `go-mp4`

Це **фундаментальний механізм** бібліотеки `go-mp4`, який дозволяє **одній структурі Go представляти кілька типів боксів** — критично для підтримки боксів із однаковим вмістом, але різними 4-байтовими ідентифікаторами (наприклад, iTunes metadata `©nam`, `©ART`, `covr` тощо).

---

## 🎯 Коротка відповідь

> **Це "хамелеон" для боксів**: він дозволяє одній структурі даних мати динамічний тип, який встановлюється під час парсингу — ідеально для підтримки боксів із однаковим форматом, але різними іменами (наприклад, всі metadata бокси iTunes мають однакову структуру, але різні коди типу).

---

## 🧱 Основні компоненти

### 🔹 `IAnyType` — інтерфейс для боксів із динамічним типом

```go
type IAnyType interface {
    IBox           // 🔹 Базовий інтерфейс боксу (GetType, SetVersion, тощо)
    SetType(BoxType)  // 🔹 Метод для встановлення типу під час парсингу
}
```

**🎯 Призначення**: Дозволити бібліотеці **змінювати тип боксу після створення екземпляра** — критично для:
- ✅ iTunes metadata: `©nam`, `©ART`, `covr` мають однакову структуру `Data`, але різні типи
- ✅ QuickTime keys: нумеровані бокси типу `0x00000001`, `0x00000002`...
- ✅ Кастомні бокси: одна структура для кількох типів

**🔢 Приклад використання:**
```
🔹 Вхід: бокс типу "©nam" (iTunes title)
🔹 Створення: box := &Data{}  // ← одна структура для всіх metadata
🔹 Встановлення типу: box.SetType(StrToBoxType("©nam"))
🔹 Результат: box.GetType() == "©nam" ✅
```

---

### 🔹 `AnyTypeBox` — базова реалізація динамічного типу

```go
type AnyTypeBox struct {
    Box      // 🔹 Вбудовуємо базовий бокс (для сумісності з IBox)
    Type BoxType  // 🔹 Поле для зберігання динамічного типу
}

// 🔹 Реалізація GetType() з IBox
func (e *AnyTypeBox) GetType() BoxType {
    return e.Type  // ← Повертаємо поточний тип
}

// 🔹 Реалізація SetType() з IAnyType
func (e *AnyTypeBox) SetType(boxType BoxType) {
    e.Type = boxType  // ← Встановлюємо новий тип
}
```

**🎯 Призначення**: Надає **готову реалізацію** для структур, які потребують динамічного типу — достатньо вбудувати `AnyTypeBox` у свою структуру.

**🔢 Приклад використання:**
```go
// 🔹 Структура для iTunes metadata боксу
type Data struct {
    AnyTypeBox  // 🔹 Вбудовуємо для підтримки динамічного типу
    FullBox     // 🔹 Стандартний заголовок
    DataType    uint32  `mp4:"0,size=32"`
    DataLang    uint32  `mp4:"1,size=32"`
    Data        []byte  `mp4:"2,size=8,len=dynamic"`
}

// 🔹 Під час парсингу:
// 1. Створюємо екземпляр: box := &Data{}
// 2. Визначаємо тип з файлу: "©nam"
// 3. Встановлюємо тип: box.SetType(StrToBoxType("©nam"))
// 4. Тепер box.GetType() повертає "©nam" ✅
```

---

## 🔍 Чому це потрібно: Проблема однакових структур

### 🔹 Сценарій: iTunes metadata бокси

```
📦 iTunes metadata має багато боксів з однаковим форматом:
• ©nam (назва)      → тип "©nam", вміст: {DataType, DataLang, Data}
• ©ART (виконавець) → тип "©ART", вміст: {DataType, DataLang, Data}
• ©alb (альбом)     → тип "©alb", вміст: {DataType, DataLang, Data}
• covr (обкладинка)  → тип "covr", вміст: {DataType, DataLang, Data}

❌ Без IAnyType: потрібні 4 окремі структури з однаковими полями
✅ З IAnyType: одна структура `Data`, тип встановлюється динамічно
```

**🔢 Код без IAnyType (погано):**
```go
// ❌ Дублювання коду для кожного типу
type NameBox struct { FullBox; DataType uint32; DataLang uint32; Data []byte }
type ArtistBox struct { FullBox; DataType uint32; DataLang uint32; Data []byte }
type AlbumBox struct { FullBox; DataType uint32; DataLang uint32; Data []byte }
// ... і так для кожного metadata боксу
```

**🔢 Код з IAnyType (добре):**
```go
// ✅ Одна структура для всіх
type Data struct {
    AnyTypeBox  // ← Динамічний тип
    FullBox
    DataType uint32  `mp4:"0,size=32"`
    DataLang uint32  `mp4:"1,size=32"`
    Data     []byte  `mp4:"2,size=8,len=dynamic"`
}

// 🔹 Реєстрація для кількох типів:
AddAnyTypeBoxDef(&Data{}, StrToBoxType("©nam"))
AddAnyTypeBoxDef(&Data{}, StrToBoxType("©ART"))
AddAnyTypeBoxDef(&Data{}, StrToBoxType("covr"))
// ... і так далі
```

---

## 🔄 Як це працює: Потік парсингу

```
🔹 Вхід: файл з боксом типу "©nam"
│
▼
🔹 Крок 1: Визначення типу з файлу
   • Читаємо 4 байти: 0xA9, 0x6E, 0x61, 0x6D → "©nam"
│
▼
🔹 Крок 2: Пошук реєстрації боксу
   • Знаходимо: тип "©nam" → структура `Data` (реалізує IAnyType)
│
▼
🔹 Крок 3: Створення екземпляра
   • box := &Data{}  // ← Type ще не встановлено
│
▼
🔹 Крок 4: Встановлення динамічного типу
   • box.SetType(StrToBoxType("©nam"))  // ← Тепер box.Type = "©nam"
│
▼
🔹 Крок 5: Парсинг вмісту
   • Unmarshal читає DataType, DataLang, Data з файлу
   • box.GetType() повертає "©nam" для подальшої обробки
│
▼
🔹 Вихід: повністю розпаршений бокс із правильним типом ✅
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Реєстрація кастомних метаданих для камери

```go
// 🔹 Структура для метаданих камери (однакова для всіх типів)
type CameraMeta struct {
    AnyTypeBox  // 🔹 Динамічний тип: "cmeta", "cevt", "calarm"...
    FullBox
    Timestamp   uint64  `mp4:"0,size=64"`
    CameraID    uint32  `mp4:"1,size=32"`
    EventType   uint8   `mp4:"2,size=8"`
    Payload     []byte  `mp4:"3,size=8,len=dynamic"`
}

// 🔹 Реєстрація для кількох типів подій:
func init() {
    // 🔹 Один тип структури, кілька типів боксів
    AddAnyTypeBoxDef(&CameraMeta{}, StrToBoxType("cmeta"))  // Metadata
    AddAnyTypeBoxDef(&CameraMeta{}, StrToBoxType("cevt"))   // Event
    AddAnyTypeBoxDef(&CameraMeta{}, StrToBoxType("calarm")) // Alarm
}

// 🔹 Під час парсингу: бібліотека автоматично викличе SetType()
// • Для боксу "cevt" → box.Type = "cevt"
// • Для боксу "calarm" → box.Type = "calarm"
```

---

### 🔹 Приклад 2: Обробка metadata з динамічним типом

```go
// 🔹 Функція для обробки iTunes-style metadata
func processMetadata(box mp4.IBox) error {
    // 🔹 Перевірка: чи підтримує бокс динамічний тип?
    if anyBox, ok := box.(mp4.IAnyType); ok {
        boxType := anyBox.GetType()  // 🔹 Отримуємо фактичний тип
        
        // 🔹 Логіка залежно від типу
        switch boxType.String() {
        case "©nam":
            log.Printf("🎬 Title: %s", extractData(box))
        case "©ART":
            log.Printf("🎤 Artist: %s", extractData(box))
        case "covr":
            log.Printf("🖼️  Cover art: %d bytes", len(extractData(box)))
        default:
            log.Printf("📦 Unknown metadata type: %s", boxType)
        }
    }
    return nil
}

// 🔹 Допоміжна функція для витягування даних
func extractData(box mp4.IBox) []byte {
    if dataBox, ok := box.(*Data); ok {
        return dataBox.Data
    }
    return nil
}
```

---

### 🔹 Приклад 3: Створення кастомних боксів із динамічним типом

```go
// 🔹 Структура для кастомного боксу з динамічним типом
type CustomBox struct {
    AnyTypeBox  // ← Динамічний тип
    Version     uint8   `mp4:"0,size=8"`
    Flags       uint32  `mp4:"1,size=24"`
    Data        []byte  `mp4:"2,size=8,len=dynamic"`
}

// 🔹 Функція для створення боксу з потрібним типом
func NewCustomBox(boxType string, data []byte) (*CustomBox, error) {
    if len(boxType) != 4 {
        return nil, fmt.Errorf("box type must be 4 characters")
    }
    
    box := &CustomBox{
        AnyTypeBox: AnyTypeBox{
            Type: mp4.StrToBoxType(boxType),  // ← Встановлюємо тип
        },
        Version: 0,
        Flags:   0,
        Data:    data,
    }
    
    return box, nil
}

// 🔹 Використання:
box1, _ := NewCustomBox("cctv", []byte("camera1"))
box2, _ := NewCustomBox("alar", []byte("motion"))
// 🔹 box1.GetType() == "cctv", box2.GetType() == "alar" ✅
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути вбудувати `AnyTypeBox` | `SetType` не працює, тип завжди порожній | Завжди вбудовуйте `AnyTypeBox` у структури, що реалізують `IAnyType` |
| Використовувати `IAnyType` для боксів з фіксованим типом | Зайва складність, можливі помилки | Для стандартних боксів (`moov`, `trak`) використовуйте звичайний `GetType()` |
| Не викликати `SetType` під час парсингу | Бокс має порожній тип → помилки у логіці | Переконайтеся, що `UnmarshalAny` викликає `SetType` для `IAnyType` |
| Плутати `BoxType` зі рядком | Порівняння типів не працює | Завжди використовуйте `BoxType` та `MatchWith()` для порівняння |
| Реєструвати одну структуру для несумісних типів | Неправильний парсинг полів | Реєструйте `IAnyType` тільки для боксів з ідентичною структурою полів |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні кастомних боксів:
    • Вбудовуйте AnyTypeBox, якщо один тип структури має кілька типів боксів
    • Реєструйте через AddAnyTypeBoxDef для кожного підтримуваного типу
    • Перевіряйте GetType() під час обробки для розрізнення типів

[ ] Для підтримки iTunes/QuickTime metadata:
    • Використовуйте одну структуру `Data` з AnyTypeBox для всіх ©-боксів
    • Реєструйте кожен тип окремо: "©nam", "©ART", "covr"...
    • Обробляйте тип через GetType() для специфічної логіки

[ ] Для дебагу:
    • Логувайте тип боксу: log.Printf("📦 Type: %s", box.GetType())
    • Перевіряйте, чи box реалізує IAnyType: if _, ok := box.(mp4.IAnyType) { ... }
    • Тестуйте з різними типами для однієї структури

[ ] Для тестування:
    • Створюйте тестові бокси з різними типами для однієї структури
    • Перевіряйте, що GetType() повертає очікуваний тип після парсингу
    • Тестуйте round-trip: запис → читання → порівняння типу та даних
```

---

## 🎯 Висновок

> **`IAnyType` / `AnyTypeBox` — це "універсальний адаптер" для боксів**, який забезпечує:
> • ✅ Гнучкість: одна структура Go для кількох типів боксів
> • ✅ Економію коду: уникнення дублювання для однакових форматів
> • ✅ Підтримку стандартів: iTunes metadata, QuickTime keys, кастомні бокси
> • ✅ Безпеку типів: тип зберігається та перевіряється під час виконання
> • ✅ Сумісність: працює з існуючим інтерфейсом `IBox` без змін

Для вашого **CCTV HLS Processor** це означає:
- 🔄 Легке додавання нових типів метаданих без створення нових структур
- 🎯 Чітке розрізнення типів подій (metadata/event/alarm) через `GetType()`
- 📦 Економія пам'яті: одна структура замість десятків дублікатів
- 🛡️ Надійність: тип перевіряється під час виконання, не під час компіляції
- 🧪 Легке тестування: одна структура, кілька тестових типів

Потребуєте допомоги з реалізацією кастомних боксів із динамічним типом або з реєстрацією підтримки нових типів метаданих? Напишіть — покажу готовий код для вашого сценарію! 🚀🎭