# 🧪 Тест `TestStringify`: Перевірка людино-читабельного виводу боксів

Це **юніт-тест** для модуля `stringify.go` бібліотеки `go-mp4`, який перевіряє коректність роботи функцій **`Stringify()`** та **`StringifyWithIndent()`** — перетворення структур боксів у людино-читабельні рядки для логування та дебагу.

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що будь-яка структура боксу, описана тегами `mp4:"..."`, коректно перетворюється у зручний для читання рядок** — з підтримкою вкладеності, форматування чисел (hex/dec), рядків, масивів, UUID, булевих значень та спеціальних типів.

---

## 📋 Структура тесту

```go
func TestStringify(t *testing.T) {
    // 🔹 Внутрішня структура для тесту вкладеності
    type inner struct {
        Uint64 uint64 `mp4:"0,size=64,hex"`  // 🔹 hex-формат для 64-біт числа
    }

    // 🔹 Тестова структура з 16+ полями різних типів
    type testBox struct {
        AnyTypeBox
        FullBox       `mp4:"0,extend"`  // 🔹 Розширення базового боксу
        String        string   `mp4:"1,string"`  // 🔹 Рядок з лапками
        Int32         int32    `mp4:"2,size=32"`  // 🔹 Знакове ціле (dec)
        Int32Hex      int32    `mp4:"3,size=32,hex"`  // 🔹 Знакове ціле (hex)
        Int32HexMinus int32    `mp4:"4,size=32,hex"`  // 🔹 Від'ємне hex
        Uint32        uint32   `mp4:"5,size=32"`  // 🔹 Беззнакове ціле
        Bytes         []byte   `mp4:"6,size=8,string"`  // 🔹 Масив байт як рядок
        Ptr           *inner   `mp4:"7"`  // 🔹 Вказівник на вкладену структуру
        PtrEx         *inner   `mp4:"8,extend"`  // 🔹 Вказівник з розширенням
        Struct        inner    `mp4:"9"`  // 🔹 Вкладена структура
        StructEx      inner    `mp4:"10,extend"`  // 🔹 Вкладена структура з розширенням
        Array         [7]byte  `mp4:"11,size=8,string"`  // 🔹 Фіксований масив як рядок
        Bool          bool     `mp4:"12,size=1"`  // 🔹 1-бітове булеве значення
        UUID          [16]byte `mp4:"13,size=8,uuid"`  // 🔹 UUID форматування
        NotSorted15   uint8    `mp4:"15,size=8,dec"`  // 🔹 Перевірка сортування за order
        NotSorted16   uint8    `mp4:"16,size=8,dec"`
        NotSorted14   uint8    `mp4:"14,size=8,dec"`  // ← order=14 має бути перед 15,16
    }
    
    // 🔹 Реєстрація типу боксу
    boxType := StrToBoxType("test")
    AddAnyTypeBoxDef(&testBox{}, boxType)
    
    // 🔹 Вихідні дані для серіалізації у рядок
    box := testBox{
        AnyTypeBox: AnyTypeBox{Type: boxType},
        FullBox: FullBox{Version: 0, Flags: [3]byte{0x00, 0x00, 0x00}},
        String:        "abema.tv",
        Int32:         -1234567890,  // 🔹 Від'ємне число
        Int32Hex:      0x12345678,   // 🔹 Позитивне у hex
        Int32HexMinus: -0x12345678,  // 🔹 Від'ємне у hex
        Uint32:        1234567890,
        Bytes:         []byte{'A','B','E','M','A',0x00,'T','V'},  // 🔹 0x00 → '.'
        Ptr: &inner{Uint64: 0x1234567890},
        PtrEx: &inner{Uint64: 0x1234567890},  // 🔹 extend → поля на одному рівні
        Struct: inner{Uint64: 0x1234567890},  // 🔹 Вкладена структура у {}
        StructEx: inner{Uint64: 0x1234567890},  // 🔹 extend → поля на одному рівні
        Array:       [7]byte{'f','o','o',0x00,'b','a','r'},  // 🔹 "foo.bar"
        Bool:        true,  // 🔹 1-бітове значення
        UUID:        [16]byte{0x01,0x23,0x45,0x67,0x89,0xab,0xcd,0xef,0x01,0x23,0x45,0x67,0x89,0xab,0xcd,0xef},
        NotSorted14: 14, NotSorted15: 15, NotSorted16: 16,  // 🔹 Перевірка сортування
    }
    
    // 🔹 ТЕСТ 1: StringifyWithIndent з відступом " "
    str, err := StringifyWithIndent(&box, " ", Context{})
    require.NoError(t, err)
    assert.Equal(t, ` Version=0`+"\n"+` Flags=0x000000`+"\n"+... , str)
    
    // 🔹 ТЕСТ 2: Stringify без відступів (компактний формат)
    str, err = Stringify(&box, Context{})
    require.NoError(t, err)
    assert.Equal(t, `Version=0 Flags=0x000000 String="abema.tv"...`, str)
}
```

---

## 🔍 Детальний розбір форматування полів

### 🔹 Базові поля `FullBox`

```go
FullBox: FullBox{Version: 0, Flags: [3]byte{0x00, 0x00, 0x00}},
```

**Очікуваний вивід:**
```
Version=0 Flags=0x000000
```

**🎯 Особливості:**
- ✅ `Version` завжди у десятковому форматі
- ✅ `Flags` завжди у hex-форматі з 6 цифрами: `0x%06x`
- ✅ Спеціальна обробка у `stringifyStruct()` незалежно від тегів

---

### 🔹 Рядки та масиви байт

```go
String: "abema.tv",  // mp4:"1,string"
Bytes:  []byte{'A','B','E','M','A',0x00,'T','V'},  // mp4:"6,size=8,string"
Array:  [7]byte{'f','o','o',0x00,'b','a','r'},  // mp4:"11,size=8,string"
```

**Очікуваний вивід:**
```
String="abema.tv"
Bytes="ABEMA.TV"      // ← 0x00 замінено на '.'
Array="foo.bar"       // ← 0x00 замінено на '.'
```

**🎯 Особливості:**
- ✅ Тег `string` → вивід у лапках `""`
- ✅ `util.EscapeUnprintables()` замінює недрюковані символи (окрім `\t\n\r`) на `.`
- ✅ Для масивів: `begin="`, `sep=""`, `end="` → рядок без роздільників

---

### 🔹 Цілі числа: dec vs hex

```go
Int32:         -1234567890,  // mp4:"2,size=32" → dec
Int32Hex:      0x12345678,   // mp4:"3,size=32,hex" → hex
Int32HexMinus: -0x12345678,  // mp4:"4,size=32,hex" → -hex
Uint32:        1234567890,   // mp4:"5,size=32" → dec
```

**Очікуваний вивід:**
```
Int32=-1234567890
Int32Hex=0x12345678
Int32HexMinus=-0x12345678  // ← мінус перед 0x
Uint32=1234567890
```

**🎯 Логіка `stringifyInt/Uint`:**
```go
if fi.is(fieldHex) {
    if val >= 0 {
        m.buf.WriteString("0x" + strconv.FormatInt(val, 16))
    } else {
        m.buf.WriteString("-0x" + strconv.FormatInt(-val, 16))  // ← мінус окремо
    }
}
```

---

### 🔹 Булеві значення (1 біт)

```go
Bool: true,  // mp4:"12,size=1"
```

**Очікуваний вивід:**
```
Bool=true
```

**🎯 Особливості:**
- ✅ `stringifyBool()` використовує `strconv.FormatBool()` → `"true"`/`"false"`
- ✅ Незалежно від `size=1` — виводиться повне слово

---

### 🔹 UUID (16 байт)

```go
UUID: [16]byte{0x01,0x23,0x45,0x67,0x89,0xab,0xcd,0xef,0x01,0x23,0x45,0x67,0x89,0xab,0xcd,0xef},
```

**Очікуваний вивід:**
```
UUID=01234567-89ab-cdef-0123-456789abcdef
```

**🎯 Логіка форматування:**
```
🔹 stringifyArray() для fieldUUID:
• begin="", sep="", end="" → без дужок
• Кожен байт: %02x → дві hex-цифри
• Після байтів 3,5,7,9 → додаємо '-' для стандартного формату UUID

🔢 Результат:
01 23 45 67 - 89 ab - cd ef - 01 23 - 45 67 89 ab cd ef
↑           ↑        ↑        ↑        ↑
байт 3      байт 5   байт 7   байт 9   кінець
```

---

### 🔹 Вкладені структури та вказівники

```go
Ptr:      &inner{Uint64: 0x1234567890},   // mp4:"7" → вкладена у {}
PtrEx:    &inner{Uint64: 0x1234567890},   // mp4:"8,extend" → поля на одному рівні
Struct:   inner{Uint64: 0x1234567890},    // mp4:"9" → вкладена у {}
StructEx: inner{Uint64: 0x1234567890},    // mp4:"10,extend" → поля на одному рівні
```

**Очікуваний вивід (з indent=" "):**
```
 Ptr={
  Uint64=0x1234567890
 }
 Uint64=0x1234567890        // ← PtrEx: поле на одному рівні (extend)
 Struct={
  Uint64=0x1234567890
 }
 Uint64=0x1234567890        // ← StructEx: поле на одному рівні (extend)
```

**🎯 Ключова різниця `extend`:**
| Тип | Тег | Вивід |
|-----|-----|-------|
| `Ptr` / `Struct` | `mp4:"7"` / `mp4:"9"` | `Ptr={Uint64=...}` ← вкладена структура у `{}` |
| `PtrEx` / `StructEx` | `mp4:"8,extend"` / `mp4:"10,extend"` | `Uint64=...` ← поле на тому ж рівні, без імені батька |

**🔄 Алгоритм `stringifyStruct`:**
```
🔹 extended=false (звичайна вкладеність):
   • Додаємо "{" на початку, "}" в кінці
   • Збільшуємо depth для відступів
   • Додаємо ім'я поля: "Ptr="

🔹 extended=true (розширення):
   • НЕ додаємо "{" / "}"
   • НЕ додаємо ім'я поля
   • Поля виводяться на тому ж рівні, що й батьківські
```

---

### 🔹 Сортування за `order`

```go
NotSorted15 uint8 `mp4:"15,size=8,dec"`  // order=15
NotSorted16 uint8 `mp4:"16,size=8,dec"`  // order=16
NotSorted14 uint8 `mp4:"14,size=8,dec"`  // order=14 ← має бути першим!
```

**Очікуваний вивід:**
```
NotSorted14=14
NotSorted15=15
NotSorted16=16
```

**🎯 Призначення**: Перевірити, що поля виводяться **у порядку `order`**, а не у порядку оголошення у структурі.

**🔑 Логіка у `buildFields()`:**
```go
sort.SliceStable(fs, func(i, j int) bool {
    return fs[i].order < fs[j].order  // ✅ Сортування за order
})
```

---

## 🔍 Порівняння `Stringify` vs `StringifyWithIndent`

### 🔹 Без відступів (`Stringify`)

```
Version=0 Flags=0x000000 String="abema.tv" Int32=-1234567890 
Int32Hex=0x12345678 Int32HexMinus=-0x12345678 Uint32=1234567890 
Bytes="ABEMA.TV" Ptr={Uint64=0x1234567890} Uint64=0x1234567890 
Struct={Uint64=0x1234567890} Uint64=0x1234567890 Array="foo.bar" 
Bool=true UUID=01234567-89ab-cdef-0123-456789abcdef 
NotSorted14=14 NotSorted15=15 NotSorted16=16
```

**🎯 Призначення**: Компактний вивід для логів, де важлива щільність інформації.

---

### 🔹 З відступами (`StringifyWithIndent(&box, " ", ctx)`)

```
 Version=0
 Flags=0x000000
 String="abema.tv"
 Int32=-1234567890
 Int32Hex=0x12345678
 Int32HexMinus=-0x12345678
 Uint32=1234567890
 Bytes="ABEMA.TV"
 Ptr={
  Uint64=0x1234567890
 }
 Uint64=0x1234567890
 Struct={
  Uint64=0x1234567890
 }
 Uint64=0x1234567890
 Array="foo.bar"
 Bool=true
 UUID=01234567-89ab-cdef-0123-456789abcdef
 NotSorted14=14
 NotSorted15=15
 NotSorted16=16
```

**🎯 Призначення**: Детальний вивід для дебагу, де важлива читабельність вкладеної структури.

**🔑 Відмінності у логіці:**
```
🔹 indent != "":
   • Додаємо "\n" після кожного поля
   • writeIndent() додає відступи залежно від depth
   • Вкладені структури отримують додатковий відступ

🔹 indent == "":
   • Додаємо " " між полями (якщо попередній символ не '{')
   • Без переносів рядка → компактно
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Логування розпаршених боксів

```go
func logParsedBox(boxType mp4.BoxType, payload mp4.IBox, ctx mp4.Context) {
    // 🔹 Компактний вивід для консольного логу
    str, err := mp4.Stringify(payload, ctx)
    if err != nil {
        log.Printf("❌ Failed to stringify %s: %v", boxType, err)
        return
    }
    log.Printf("📦 %s: %s", boxType, str)
    
    // 🔹 Детальний вивід для файлу дебагу (якщо увімкнено)
    if debugLevel >= 2 {
        detailed, _ := mp4.StringifyWithIndent(payload, "  ", ctx)
        debugFile.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", boxType, detailed))
    }
}

// 🔹 Приклад логу:
// 📦 trun: Version=0 Flags=0x000101 SampleCount=3 Entries=[{SampleDuration=100}, ...]
```

---

### 🔹 Приклад 2: Валідація метаданих через читабельний вивід

```go
func validateMetadata(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeIlst() {
            box, _, err := h.ReadPayload()
            if err != nil { return nil, err }
            
            // 🔹 Людсько-читабельний вивід для перевірки
            str, err := mp4.StringifyWithIndent(box, "  ", h.BoxInfo.Context)
            if err != nil { return nil, err }
            
            // 🔹 Перевірка наявності обов'язкових полів
            if !strings.Contains(str, `"©nam"`) && !strings.Contains(str, `Name="©nam"`) {
                return fmt.Errorf("missing title metadata")
            }
            if !strings.Contains(str, `DataType=UTF8`) {
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

### 🔹 Приклад 3: Експорт структури файлу для аналізу

```go
func exportBoxStructure(filePath, outputPath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    out, err := os.Create(outputPath)
    if err != nil { return err }
    defer out.Close()
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        indent := strings.Repeat("  ", len(h.Path)-1)
        
        // 🔹 Заголовок боксу
        fmt.Fprintf(out, "%s📦 %s @ %d+%d\n", 
            indent, h.BoxInfo.Type, h.BoxInfo.Offset, h.BoxInfo.Size)
        
        // 🔹 Якщо підтримується → парсимо та виводимо вміст
        if h.BoxInfo.IsSupportedType() {
            box, _, err := h.ReadPayload()
            if err != nil { return nil, err }
            
            str, err := mp4.StringifyWithIndent(box, "  ", h.BoxInfo.Context)
            if err != nil { return nil, err }
            
            // 🔹 Вивід з додатковим відступом
            for _, line := range strings.Split(str, "\n") {
                if line != "" {
                    fmt.Fprintf(out, "%s  %s\n", indent, line)
                }
            }
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
| Забути `string` тег для масивів байт | Вивід: `[0x48,0x65,0x6c,0x6c,0x6f]` замість `"Hello"` | Завжди вказуйте `string` тег: `[]byte \`mp4:"...,string"\`` |
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

> **Цей тест — ваш "золотий стандарт" для надійного людино-читабельного виводу боксів**.  
> Він гарантує:
> • ✅ Коректне форматування всіх типів даних: числа (dec/hex), рядки, масиви, UUID, bool
> • ✅ Правильну обробку вкладених структур та вказівників з підтримкою `extend`
> • ✅ Автоматичне сортування полів за `order`, незалежно від порядку у коді
> • ✅ Гнучкість: компактний вивід (`Stringify`) vs детальний з відступами (`StringifyWithIndent`)
> • ✅ Кастомну логіку через інтерфейс `StringifyField()` для складних випадків

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Миттєвий дебаг розпаршених боксів через читабельні логи
- 📊 Зручна візуалізація структури файлів для аналізу та валідації
- 🛠️ Легка інтеграція з системами моніторингу через стандартизований вивід
- 🧪 Надійне тестування через порівняння очікуваного/фактичного рядкового представлення

Потребуєте допомоги з налаштуванням `StringifyField()` для кастомного форматування ваших боксів або з інтеграцією дебаг-виводу у ваш конвеєр? Напишіть — покажу готовий приклад! 🚀🔤