# 🔤 `stringify.go`: Людсько-читабельне представлення боксів у `go-mp4`

Це **модуль бібліотеки `go-mp4`**, який відповідає за **перетворення структур боксів у людино-читабельні рядки** для логування, дебагу та візуалізації.

---

## 🎯 Коротка відповідь

> **Це "pretty-printer" для боксів**: він бере будь-яку структуру боксу, описану тегами `mp4:"..."`, і перетворює її у зручний для читання рядок з підтримкою вкладеності, форматування чисел, рядків, масивів та спеціальних типів (UUID, ISO639-2, hex/dec).

---

## 🧱 Основні типи та функції

### 🔹 `stringifier` — внутрішня структура для побудови рядка

```go
type stringifier struct {
    buf    *bytes.Buffer  // 🔹 Буфер для накопичення результату
    src    IImmutableBox  // 🔹 Вихідна структура боксу
    indent string         // 🔹 Відступ для форматування (порожній або "  ")
    ctx    Context        // 🔹 Контекст парсингу (версія, прапорці, iTunes metadata...)
}
```

**🎯 Призначення**: Інкапсулювати стан під час рекурсивного обходу структури для побудови рядка.

---

### 🔹 `Stringify` / `StringifyWithIndent` — публічний API

```go
func Stringify(src IImmutableBox, ctx Context) (string, error) {
    return StringifyWithIndent(src, "", ctx)  // 🔹 Без відступів: компактий вивід
}

func StringifyWithIndent(src IImmutableBox, indent string, ctx Context) (string, error) {
    boxDef := src.GetType().getBoxDef(ctx)  // 🔹 Отримуємо метадані боксу
    if boxDef == nil { return "", ErrBoxInfoNotFound }
    
    v := reflect.ValueOf(src).Elem()  // 🔹 Рефлексія: отримуємо значення структури
    
    m := &stringifier{
        buf:    bytes.NewBuffer(nil),
        src:    src,
        indent: indent,  // 🔹 Напр. "  " для дерева з відступами
        ctx:    ctx,
    }
    
    err := m.stringifyStruct(v, boxDef.fields, 0, true)  // 🔹 Рекурсивний обхід
    if err != nil { return "", err }
    
    return m.buf.String(), nil  // 🔹 Готовий рядок
}
```

**🔢 Приклади виводу:**

```go
// 🔹 Без відступів (компактний):
Stringify(&Trun{SampleCount:3}, ctx) 
// → "SampleCount=3"

// 🔹 З відступами (дерево):
StringifyWithIndent(&Trun{SampleCount:3, Entries:[...]}, "  ", ctx)
// → {
//      SampleCount=3
//      Entries=[{SampleDuration=100}, {SampleDuration=101}, {SampleDuration=102}]
//    }
```

---

## 🔍 Рекурсивний обхід: `stringifyStruct`

```go
func (m *stringifier) stringifyStruct(v reflect.Value, fs []*field, depth int, extended bool) error {
    // 🔹 Початок структури: "{" або без дужок для розширень
    if !extended {
        m.buf.WriteString("{")
        if m.indent != "" { m.buf.WriteString("\n"); depth++ }
    }
    
    // 🔹 Для кожного поля у метаданих:
    for _, f := range fs {
        fi := resolveFieldInstance(f, m.src, v, m.ctx)  // 🔹 Динамічні параметри
        
        if !isTargetField(m.src, fi, m.ctx) { continue }  // 🔹 Пропускаємо нецільові поля
        if f.cnst != "" || f.is(fieldHidden) { continue }  // 🔹 Пропускаємо const/hidden
        
        // 🔹 Форматування імені поля (якщо не розширення)
        if !f.is(fieldExtend) {
            if m.indent != "" { writeIndent(m.buf, m.indent, depth+1) }
            else if m.buf.Len() != 0 && lastByte != '{' { m.buf.WriteString(" ") }
            m.buf.WriteString(f.name)
            m.buf.WriteString("=")
        }
        
        // 🔹 Кастомна логіка через інтерфейс
        str, ok := fi.cfo.StringifyField(f.name, m.indent, depth+1, m.ctx)
        if ok {
            m.buf.WriteString(str)  // 🔹 Використовуємо кастомний вивід
            if !f.is(fieldExtend) && m.indent != "" { m.buf.WriteString("\n") }
            continue
        }
        
        // 🔹 Спеціальна обробка Version/Flags
        if f.name == "Version" {
            m.buf.WriteString(strconv.Itoa(int(m.src.GetVersion())))
        } else if f.name == "Flags" {
            fmt.Fprintf(m.buf, "0x%06x", m.src.GetFlags())  // 🔹 Формат: 0x000101
        } else {
            // 🔹 Стандартна рекурсивна обробка
            err := m.stringify(v.FieldByName(f.name), fi, depth)
            if err != nil { return err }
        }
        
        if !f.is(fieldExtend) && m.indent != "" { m.buf.WriteString("\n") }
    }
    
    // 🔹 Кінець структури: "}"
    if !extended {
        if m.indent != "" { writeIndent(m.buf, m.indent, depth) }
        m.buf.WriteString("}")
    }
    return nil
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: структура `Trun{SampleCount:3, Entries:[...]}`, indent="  "
│
▼
🔹 stringifyStruct(v, fields, depth=0, extended=true):
   │
   ├── 🔹 extended=true → не додаємо "{" на початку
   │
   ├── 🔹 Для поля "SampleCount":
   │   • isTargetField()=true → обробляємо
   │   • cnst="" , not hidden → продовжуємо
   │   • not fieldExtend → додаємо "SampleCount="
   │   • StringifyField()=false → стандартна обробка
   │   • f.name != "Version"/"Flags" → рекурсивний виклик
   │   • stringifyUint(3, ...) → додаємо "3"
   │   • indent!="" → додаємо "\n"
   │
   ├── 🔹 Для поля "Entries" (масив):
   │   • stringifySlice() → додаємо "Entries=[...]"
   │   • Рекурсивна обробка кожного елемента масиву
   │
   ▼
🔹 Вихід: "SampleCount=3\nEntries=[{SampleDuration=100}, ...]"
```

---

## 🔤 Форматування різних типів даних

### 🔹 Цілі числа: `stringifyInt` / `stringifyUint`

```go
func (m *stringifier) stringifyUint(v reflect.Value, fi *fieldInstance, depth int) error {
    if fi.is(fieldISO639_2) {
        // 🔹 Мова: 5-бітне кодування → "eng", "ukr"...
        m.buf.WriteString(string([]byte{byte(v.Uint() + 0x60)}))
    } else if fi.is(fieldUUID) {
        // 🔹 UUID: кожен байт у hex без роздільників
        fmt.Fprintf(m.buf, "%02x", v.Uint())
    } else if fi.is(fieldString) {
        // 🔹 Рядок: байт → символ
        m.buf.WriteString(string([]byte{byte(v.Uint())}))
    } else if fi.is(fieldHex) || (!fi.is(fieldDec) && v.Type().Kind() == reflect.Uint8) {
        // 🔹 Hex формат: 0xff для байт, якщо не вказано dec
        m.buf.WriteString("0x")
        m.buf.WriteString(strconv.FormatUint(v.Uint(), 16))
    } else {
        // 🔹 Десятковий формат за замовчуванням
        m.buf.WriteString(strconv.FormatUint(v.Uint(), 10))
    }
    return nil
}
```

**🔢 Приклади:**
```
🔹 fieldISO639_2: uint(5) → 'e' (5+0x60=0x65='e') → "eng" (три літери поспіль)
🔹 fieldUUID: [16]byte → "01020304-0506-0708-090a-0b0c0d0e0f10" (де '-' додається у stringifyArray)
🔹 fieldHex: uint8(255) → "0xff"
🔹 За замовчуванням: uint32(12345) → "12345"
```

---

### 🔹 Рядки: `stringifyString`

```go
func (m *stringifier) stringifyString(v reflect.Value, depth int) error {
    m.buf.WriteString("\"")  // 🔹 Початкова лапка
    m.buf.WriteString(util.EscapeUnprintables(v.String()))  // 🔹 Екранування недрюкованих символів
    m.buf.WriteString("\"")  // 🔹 Кінцева лапка
    return nil
}
```

**🔢 Приклади:**
```
🔹 "hello" → "\"hello\""
🔹 "foo\x00bar" → "\"foo.bar\""  // ← 0x00 замінено на '.'
🔹 "Line1\nLine2" → "\"Line1\nLine2\""  // ← \n зберігається
```

> 💡 `util.EscapeUnprintables()` замінює недрюковані символи (окрім \t,\n,\r) на '.' для читабельності.

---

### 🔹 Масиви: `stringifyArray`

```go
func (m *stringifier) stringifyArray(v reflect.Value, fi *fieldInstance, depth int) error {
    // 🔹 Визначення роздільників за типом
    begin, sep, end := "[", ", ", "]"
    if fi.is(fieldString) || fi.is(fieldISO639_2) {
        begin, sep, end = "\"", "", "\""  // 🔹 Рядок без роздільників
    } else if fi.is(fieldUUID) {
        begin, sep, end = "", "", ""  // 🔹 UUID без дужок
    }
    
    m.buf.WriteString(begin)
    
    // 🔹 Оптимізація для рядків: окремий буфер
    m2 := *m
    if fi.is(fieldString) { m2.buf = bytes.NewBuffer(nil) }
    
    size := v.Type().Size()
    for i := 0; i < int(size)/int(v.Type().Elem().Size()); i++ {
        if i != 0 { m2.buf.WriteString(sep) }
        m2.stringify(v.Index(i), fi, depth+1)  // 🔹 Рекурсивна обробка елемента
        
        // 🔹 Додавання '-' для UUID у певних позиціях
        if fi.is(fieldUUID) && (i == 3 || i == 5 || i == 7 || i == 9) {
            m.buf.WriteString("-")
        }
    }
    
    if fi.is(fieldString) {
        m.buf.WriteString(util.EscapeUnprintables(m2.buf.String()))  // 🔹 Фінальне екранування
    }
    
    m.buf.WriteString(end)
    return nil
}
```

**🔢 Приклади:**
```
🔹 [4]byte{'h','o','g','e'} з fieldString → "\"hoge\""
🔹 [16]byte для UUID → "01020304-0506-0708-090a-0b0c0d0e0f10"
🔹 [3]uint8{1,2,3} → "[0x1, 0x2, 0x3]" (hex для uint8 за замовчуванням)
```

---

### 🔹 Спеціальна обробка `Version` та `Flags`

```go
if f.name == "Version" {
    m.buf.WriteString(strconv.Itoa(int(m.src.GetVersion())))  // 🔹 Завжди десятковий формат
} else if f.name == "Flags" {
    fmt.Fprintf(m.buf, "0x%06x", m.src.GetFlags())  // 🔹 Завжди hex, 6 цифр: 0x000101
}
```

**🎯 Призначення**: Забезпечити **консистентний вивід** для найпоширеніших полів незалежно від тегів у структурі.

**Приклад:**
```
🔹 FullBox{Version:0, Flags:[3]byte{0,1,1}} → "Version=0 Flags=0x000101"
```

---

### 🔹 Кастомна логіка через `StringifyField`

```go
str, ok := fi.cfo.StringifyField(f.name, m.indent, depth+1, m.ctx)
if ok {
    m.buf.WriteString(str)  // 🔹 Використовуємо кастомний вивід
    // ...
    continue
}
```

**🎯 Призначення**: Дозволити структурам **перевизначати логіку виводу** для конкретних полів.

**Приклад з `Data` боксу (iTunes metadata):**
```go
func (data *Data) StringifyField(name string, indent string, depth int, ctx Context) (string, bool) {
    switch name {
    case "DataType":
        switch data.DataType {
        case DataTypeStringUTF8: return "UTF8", true  // ← замість "1"
        case DataTypeBinary: return "BINARY", true
        // ...
        }
    case "Data":
        if data.DataType == DataTypeStringUTF8 {
            return fmt.Sprintf("\"%s\"", util.EscapeUnprintables(string(data.Data))), true
        }
    }
    return "", false  // ← стандартна логіка
}
```

**Результат:**
```
🔹 Без кастомної логіки: "DataType=1 Data=[0x4c,0x61,0x76,0x66...]"
🔹 З кастомною логікою: "DataType=UTF8 Data=\"Lavf58.29.100\""  ← набагато зрозуміліше!
```

---

## 🔄 Потік даних: Повний приклад

```
🔹 Вхід: &Trun{
    FullBox: FullBox{Version:0, Flags:[3]byte{0,1,1}},
    SampleCount: 3,
    Entries: []TrunEntry{
        {SampleDuration:100},
        {SampleDuration:101},
        {SampleDuration:102},
    },
}

🔹 StringifyWithIndent(src, "  ", ctx):
   │
   ▼
🔹 stringifyStruct(v, fields, depth=0, extended=true):
   │
   ├── 🔹 Поле "Version":
   │   • Спеціальна обробка: m.src.GetVersion() → "0"
   │
   ├── 🔹 Поле "Flags":
   │   • Спеціальна обробка: 0x%06x → "0x000101"
   │
   ├── 🔹 Поле "SampleCount":
   │   • stringifyUint(3) → "3"
   │
   ├── 🔹 Поле "Entries" (масив):
   │   • stringifySlice() → "Entries=["
   │   │
   │   ├── 🔹 Елемент 0: {SampleDuration:100}
   │   │   • stringifyStruct(..., extended=false) → "{SampleDuration=100}"
   │   │
   │   ├── 🔹 Елемент 1: {SampleDuration:101} → "{SampleDuration=101}"
   │   ├── 🔹 Елемент 2: {SampleDuration:102} → "{SampleDuration=102}"
   │   │
   │   • Додаємо "]" → "Entries=[{SampleDuration=100}, {SampleDuration=101}, {SampleDuration=102}]"
   │
   ▼
🔹 Вихід (з indent="  "):
{
  Version=0
  Flags=0x000101
  SampleCount=3
  Entries=[{SampleDuration=100}, {SampleDuration=101}, {SampleDuration=102}]
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Логування розпаршених боксів для дебагу

```go
func debugParsedBox(boxType mp4.BoxType, payload mp4.IBox, ctx mp4.Context) {
    // 🔹 Компактний вивід для логу
    str, err := mp4.Stringify(payload, ctx)
    if err != nil {
        log.Printf("❌ Failed to stringify %s: %v", boxType, err)
        return
    }
    log.Printf("📦 %s: %s", boxType, str)
    
    // 🔹 Детальний вивід для файлу дебагу
    if debugLevel >= 2 {
        detailed, _ := mp4.StringifyWithIndent(payload, "  ", ctx)
        debugFile.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", boxType, detailed))
    }
}

// 🔹 Приклад логу:
// 📦 trun: Version=0 Flags=0x000101 SampleCount=3 Entries=[{SampleDuration=100}, ...]
```

---

### 🔹 Приклад 2: Візуалізація структури файлу для аналізу

```go
func printBoxTreeDetailed(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        indent := strings.Repeat("  ", len(h.Path)-1)
        
        // 🔹 Заголовок боксу
        fmt.Printf("%s📦 %s @ %d+%d\n", 
            indent, h.BoxInfo.Type, h.BoxInfo.Offset, h.BoxInfo.Size)
        
        // 🔹 Якщо підтримується → парсимо та виводимо вміст
        if h.BoxInfo.IsSupportedType() {
            box, _, err := h.ReadPayload()
            if err != nil { return nil, err }
            
            // 🔹 Stringify з відступами для вкладеності
            str, err := mp4.StringifyWithIndent(box, "  ", h.BoxInfo.Context)
            if err != nil { return nil, err }
            
            // 🔹 Вивід з додатковим відступом
            for _, line := range strings.Split(str, "\n") {
                if line != "" {
                    fmt.Printf("%s  %s\n", indent, line)
                }
            }
        }
        
        return h.Expand()  // 🔹 Рекурсивно обробити дітей
    }
    
    _, err = mp4.ReadBoxStructure(f, handler)
    return err
}

// 🔹 Приклад виводу:
// 📦 moov @ 24+10240
//   {
//     Version=0
//     Flags=0x000000
//     ...
//   }
//   📦 trak @ 100+2048
//     {
//       TrackID=1
//       ...
//     }
```

---

### 🔹 Приклад 3: Валідація метаданих через читабельний вивід

```go
func validateMetadata(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeIlst() {
            // 🔹 Парсимо iTunes metadata
            box, _, err := h.ReadPayload()
            if err != nil { return nil, err }
            
            // 🔹 Людсько-читабельний вивід для перевірки
            str, err := mp4.StringifyWithIndent(box, "  ", h.BoxInfo.Context)
            if err != nil { return nil, err }
            
            // 🔹 Перевірка наявності обов'язкових полів
            if !strings.Contains(str, "©nam") {
                return fmt.Errorf("missing title metadata")
            }
            if !strings.Contains(str, "DataType=UTF8") {
                log.Printf("⚠️  Metadata may have encoding issues")
            }
            
            fmt.Printf("✅ Metadata valid:\n%s\n", str)
        }
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(f, handler)
    return err
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути `fieldString` для масивів байт | Вивід: `[0x48,0x65,0x6c,0x6c,0x6f]` замість `"Hello"` | Завжди вказуйте `string` тег для рядкових полів: `[]byte \`mp4:"...,string"\`` |
| Неправильне форматування `Flags` | Вивід: `Flags=257` замість `Flags=0x000101` | Покладайтеся на спеціальну обробку у `stringifyStruct` для поля "Flags" |
| Ігнорування `StringifyField` інтерфейсу | Вивід: `DataType=1` замість `DataType=UTF8` | Реалізуйте `StringifyField()` для кастомного форматування важливих полів |
| Неправильна обробка `fieldISO639_2` | Вивід: `Language=5` замість `Language="eng"` | Використовуйте тег `iso639-2` для мовних кодів |
| Забути екранування недрюкованих символів | Логи з `^@` замість `.` важко читати | Завжди використовуйте `util.EscapeUnprintables()` для рядків |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні нових структур боксів:
    • Використовуйте `string` тег для рядкових полів: `[]byte \`mp4:"...,string"\``
    • Використовуйте `iso639-2` для мовних кодів: `uint16 \`mp4:"...,iso639-2"\``
    • Використовуйте `hex` для полів, які краще виглядають у шістнадцятковому форматі
    • Реалізуйте `StringifyField()` для кастомного форматування складних полів

[ ] Для дебагу та логування:
    • Використовуйте `Stringify()` для компактних логів: log.Printf("📦 %s", Stringify(box, ctx))
    • Використовуйте `StringifyWithIndent(box, "  ", ctx)` для детального виводу у файл
    • Екрануйте недрюковані символи через `util.EscapeUnprintables()` для читабельності

[ ] Для валідації метаданих:
    • Парсіть бокси через `ReadPayload()` перед `Stringify()`
    • Перевіряйте наявність обов'язкових полів у рядковому виводі
    • Логувайте попередження при неочікуваних форматах (напр. не-UTF8 рядки)

[ ] Для візуалізації структури:
    • Комбінуйте `ReadBoxStructure()` з `StringifyWithIndent()` для дерева боксів
    • Додавайте відступи залежно від глибини вкладеності
    • Фільтруйте великі бокси (mdat) для уникнення переповнення логу

[ ] Для тестування:
    • Порівнюйте очікуваний та фактичний вивід `Stringify()` у юніт-тестах
    • Тестуйте з різними `indent` значеннями: "" (компакт) vs "  " (дерево)
    • Перевіряйте обробку крайніх випадків: порожні масиви, максимальні значення, недрюковані символи
```

---

## 🎯 Висновок

> **`stringify.go` — це "перекладач" бінарних структур у людську мову**, який забезпечує:
> • ✅ Автоматичне форматування будь-якої структури боксу через рефлексію та теги `mp4:"..."`
> • ✅ Гнучку підтримку різних форматів: десяткові/шістнадцяткові числа, рядки, масиви, UUID
> • ✅ Спеціальну обробку поширених полів (`Version`, `Flags`) для консистентності
> • ✅ Кастомну логіку через інтерфейс `StringifyField()` для складних випадків
> • ✅ Зручне форматування з відступами для візуалізації ієрархії

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Миттєвий дебаг розпаршених боксів через читабельні логи
- 📊 Зручна візуалізація структури файлів для аналізу та валідації
- 🛠️ Легка інтеграція з системами моніторингу через стандартизований вивід
- 🧪 Надійне тестування через порівняння очікуваного/фактичного рядкового представлення

Потребуєте допомоги з налаштуванням `StringifyField()` для кастомного форматування ваших боксів або з інтеграцією дебаг-виводу у ваш конвеєр? Напишіть — покажу готовий приклад! 🚀🔤