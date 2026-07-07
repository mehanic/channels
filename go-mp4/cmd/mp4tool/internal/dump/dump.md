# 📋 `dump`: Інструмент для візуалізації структури MP4-файлів

Це **практичний CLI-інструмент** на основі бібліотеки `go-mp4`, який виводить **людсько-читабельну ієрархію боксів** у файлі з підтримкою фільтрації, форматування та детального вмісту.

---

## 🎯 Коротка відповідь

> **Це "рентген" для MP4-файлів**: він показує дерево боксів з розмірами, офсетами та вмістом, дозволяючи швидко аналізувати структуру файлу, знаходити проблеми або дебажити парсинг — подібно до `mp4dump`, `mediainfo` або `ffprobe -show_format`.

---

## 🗂️ Структура виводу

```bash
# 🔹 Базовий виклик:
$ mp4tool dump video.mp4

[ftyp] Size=32 MajorBrand="isom" MinorVersion=512 CompatibleBrands=[{isom}{iso2}{avc1}{mp41}]
[free] Size=8 Data=[...] (use "-full free" to show all)
[mdat] Size=6402 Data=[...] (use "-full mdat" to show all)
[moov] Size=1836
  [mvhd] Size=108 Version=0 Flags=0x000000 ...
  [trak] Size=743
    [tkhd] Size=92 TrackID=1 ...
    [edts] Size=36
      [elst] Size=28 EntryCount=1 ...
    [mdia] Size=607
      [mdhd] Size=32 Timescale=10240 Duration=10240 ...
      ...
```

**🔹 З прапорцями:**
```bash
# 🔹 Показати офсети та шістнадцяткові розміри:
$ mp4tool dump -offset -hex video.mp4
[ftyp] Offset=0x0 Size=0x20 ...

# 🔹 Показати повний вміст mdat:
$ mp4tool dump -full mdat video.mp4
[mdat] Size=6402 Data=[0x00 0x00 0x00 0x18 0x66 0x74 0x79 0x70 ...]

# 🔹 Показати ВСЕ (окрім великих боксів):
$ mp4tool dump -a video.mp4
```

---

## 🧱 Основні компоненти

### 🔹 Конфігурація через прапорці

```go
func Main(args []string) int {
    flagSet := flag.NewFlagSet("dump", flag.ExitOnError)
    
    // 🔹 -full: показати повний вміст конкретних типів боксів
    full := flagSet.String("full", "", "Show full content of specified box types")
    
    // 🔹 -a: показати все, окрім mdat/free/styp
    showAll := flagSet.Bool("a", false, "Show full content excepting mdat, free, styp")
    
    // 🔹 -offset: показати зміщення боксу у файлі
    offset := flagSet.Bool("offset", false, "Show offset of box")
    
    // 🔹 -hex: використовувати шістнадцятковий формат для розмірів
    hex := flagSet.Bool("hex", false, "Use hex for size and offset")
    
    // 🔹 Застарілі прапорці (для сумісності)
    mdat := flagSet.Bool("mdat", false, "Deprecated: use \"-full mdat\"")
    free := flagSet.Bool("free", false, "Deprecated: use \"-full free,styp\"")
    
    flagSet.Parse(args)
    // ...
}
```

**🎯 Призначення**: Дозволити користувачеві гнучко налаштувати рівень деталізації виводу.

---

### 🔹 `mp4dump` — контекст дампу

```go
type mp4dump struct {
    full    map[string]struct{}  // 🔹 Множина типів для повного виводу
    showAll bool                 // 🔹 Показувати все (окрім винятків)
    offset  bool                 // 🔹 Показувати офсети
    hex     bool                 // 🔹 Шістнадцятковий формат
}
```

**🎯 Призначення**: Інкапсулювати налаштування форматування для передачі у функції обробки.

---

### 🔹 Ефективне читання: `bufseekio`

```go
func (m *mp4dump) dumpFile(fpath string) error {
    file, err := os.Open(fpath)
    if err != nil { return err }
    defer file.Close()
    
    // 🔹 Буферизований ReadSeeker з історією блоків
    // • blockSize: 128KB — розмір буфера читання
    // • blockHistorySize: 4 — кількість блоків для зворотного seek
    return m.dump(bufseekio.NewReadSeeker(file, blockSize, blockHistorySize))
}
```

**🎯 Призначення**: Прискорити читання великих файлів за рахунок:
- ✅ Буферизації: менше системних викликів `read()`
- ✅ Історії блоків: швидкий `Seek()` назад без повторного читання з диску

---

### 🔹 Основна логіка: `dump()` з `ReadBoxStructure`

```go
func (m *mp4dump) dump(r io.ReadSeeker) error {
    _, err := mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
        line := bytes.NewBuffer(make([]byte, 0, terminalWidth))
        
        // 🔹 Крок 1: Відступи за глибиною вкладеності
        printIndent(line, len(h.Path)-1)
        
        // 🔹 Крок 2: Заголовок боксу
        fmt.Fprintf(line, "[%s]", h.BoxInfo.Type.String())
        if !h.BoxInfo.IsSupportedType() {
            fmt.Fprintf(line, " (unsupported box type)")
        }
        
        // 🔹 Крок 3: Розмір та офсет (з форматом hex/dec)
        sizeFormat := "%d"
        if m.hex { sizeFormat = "0x%x" }
        if m.offset {
            fmt.Fprintf(line, " Offset="+sizeFormat, h.BoxInfo.Offset)
        }
        fmt.Fprintf(line, " Size="+sizeFormat, h.BoxInfo.Size)
        
        // 🔹 Крок 4: Визначення, чи показувати повний вміст
        _, full := m.full[h.BoxInfo.Type.String()]
        if !full && (h.BoxInfo.Type == mp4.BoxTypeMdat() || ...) {
            // 🔹 Великі бокси: скорочений вивід
            fmt.Fprintf(line, " Data=[...] (use \"-full %s\" to show all)", h.BoxInfo.Type)
            fmt.Println(line.String())
            return nil, nil  // 🔹 Не розгортаємо дітей
        }
        full = full || m.showAll
        
        // 🔹 Крок 5: Підтримувані типи боксів
        if h.BoxInfo.IsSupportedType() {
            // 🔹 Оптимізація: не парсити великі бокси без дітей
            if !full && h.BoxInfo.Size-h.BoxInfo.HeaderSize >= 64 &&
                util.ShouldHasNoChildren(h.BoxInfo.Type) {
                fmt.Fprintf(line, " ... (use \"-full %s\" to show all)", h.BoxInfo.Type)
                fmt.Println(line.String())
                return nil, nil
            }
            
            // 🔹 Парсинг вмісту
            box, _, err := h.ReadPayload()
            if err != mp4.ErrUnsupportedBoxVersion {
                if err != nil { return nil, err }
                
                // 🔹 Людсько-читабельний рядок
                str, err := mp4.Stringify(box, h.BoxInfo.Context)
                if err != nil { return nil, err }
                
                // 🔹 Обрізання, якщо рядок задовгий для терміналу
                if !full && line.Len()+len(str)+2 > terminalWidth {
                    fmt.Fprintf(line, " ... (use \"-full %s\" to show all)", h.BoxInfo.Type)
                } else if str != "" {
                    fmt.Fprintf(line, " %s", str)
                }
                
                fmt.Println(line.String())
                _, err = h.Expand()  // 🔹 Рекурсивна обробка дітей
                return nil, err
            }
            fmt.Fprintf(line, " (unsupported box version)")
        }
        
        // 🔹 Крок 6: Непідтримувані типи — сирі дані
        if full {
            buf := bytes.NewBuffer(make([]byte, 0, h.BoxInfo.Size-h.BoxInfo.HeaderSize))
            h.ReadData(buf)  // 🔹 Копіювання сирих байт
            fmt.Fprintf(line, " Data=[")
            for i, d := range buf.Bytes() {
                if i != 0 { fmt.Fprintf(line, " ") }
                fmt.Fprintf(line, "0x%02x", d)
            }
            fmt.Fprintf(line, "]")
        } else {
            fmt.Fprintf(line, " Data=[...] (use \"-full %s\" to show all)", h.BoxInfo.Type)
        }
        fmt.Println(line.String())
        return nil, nil
    })
    return err
}
```

**🔄 Потік даних:**
```
🔹 Вхід: io.ReadSeeker (файл з буферизацією)
│
▼
🔹 ReadBoxStructure(handler):
   │
   ├── 🔹 Для кожного боксу:
   │   • Формування рядка: [тип] + офсет + розмір
   │   • Перевірка: чи підтримується тип?
   │   • Перевірка: чи показувати повний вміст?
   │   │
   │   ├── 🔹 Якщо підтримується:
   │   │   • ReadPayload() → парсинг у структуру
   │   │   • Stringify() → людино-читабельний рядок
   │   │   • Обрізання, якщо задовго для терміналу
   │   │   • Expand() → рекурсія на дітей
   │   │
   │   ├── 🔹 Якщо не підтримується:
   │   │   • ReadData() → сирі байти
   │   │   • Форматування: [0x00 0x01 0x02 ...]
   │   │
   │   └── 🔹 Вивід рядка + перенос
   │
   ▼
🔹 Вихід: текст у stdout з ієрархією боксів
```

---

## 🔍 Ключові оптимізації

### 🔹 Розумне скорочення великих боксів

```go
if !full && (h.BoxInfo.Type == mp4.BoxTypeMdat() || ...) {
    fmt.Fprintf(line, " Data=[...] (use \"-full %s\" to show all)", h.BoxInfo.Type)
    return nil, nil  // 🔹 Не розгортаємо дітей
}
```

**🎯 Призначення**: Уникнути виводу мегабайтів сирих даних для `mdat`, `free` тощо, якщо користувач не запросив `-full`.

---

### 🔹 Перевірка на наявність дітей

```go
if !full && h.BoxInfo.Size-h.BoxInfo.HeaderSize >= 64 &&
    util.ShouldHasNoChildren(h.BoxInfo.Type) {
    fmt.Fprintf(line, " ... (use \"-full %s\" to show all)", h.BoxInfo.Type)
    return nil, nil  // 🔹 Пропускаємо парсинг
}
```

**🎯 Призначення**: Не витрачати час на парсинг великих боксів, які за специфікацією не мають вкладених структур (напр. `stsz`, `stco`).

**Приклад `util.ShouldHasNoChildren`:**
```go
func ShouldHasNoChildren(boxType mp4.BoxType) bool {
    switch boxType {
    case mp4.BoxTypeStsz(), mp4.BoxTypeStco(), mp4.BoxTypeCo64():
        return true  // ✅ Тільки дані, без дітей
    default:
        return false
    }
}
```

---

### 🔹 Адаптація до ширини терміналу

```go
if !full && line.Len()+len(str)+2 > terminalWidth {
    fmt.Fprintf(line, " ... (use \"-full %s\" to show all)", h.BoxInfo.Type)
}
```

**🎯 Призначення**: Запобігти "розламанню" виводу на кілька рядків, що ускладнює читання ієрархії.

**Визначення ширини:**
```go
func init() {
    if width, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
        terminalWidth = width  // ✅ Авто-детекція
    }
}
```

---

## 🛠️ Практичне використання

### 🔹 Аналіз структури файлу

```bash
# 🔹 Швидкий огляд:
$ mp4tool dump video.mp4 | head -20
[ftyp] Size=32 MajorBrand="isom" ...
[moov] Size=1836
  [mvhd] Size=108 ...
  [trak] Size=743
    [tkhd] Size=92 TrackID=1 ...
    [mdia] Size=607
      [mdhd] Size=32 Timescale=10240 ...
      [hdlr] Size=44 HandlerType="vide" ...

# 🔹 Пошук конкретних боксів:
$ mp4tool dump video.mp4 | grep trun
    [trun] Size=120 SampleCount=10 ...

# 🔹 Детальний аналіз таймстемпів:
$ mp4tool dump -full stts,ctts video.mp4
    [stts] Size=24 EntryCount=1 Entries=[{SampleCount=10 SampleDelta=1024}]
    [ctts] Size=88 EntryCount=4 Entries=[{SampleCount=1 SampleOffset=2048}, ...]
```

---

### 🔹 Дебаг парсингу

```bash
# 🔹 Перевірка, чи коректно парситься бокс:
$ mp4tool dump -full custom video.mp4
[custom] (unsupported box type) Size=64 Data=[0x00 0x01 0x02 ...]

# 🔹 Порівняння очікуваного/фактичного вмісту:
$ mp4tool dump -full moov video.mp4 | grep -A5 "avcC"
    [avcC] Size=49 ConfigurationVersion=0x1 Profile=0x64 ...
```

---

### 🔹 Інтеграція у CCTV HLS Processor

```go
// 🔹 Приклад: валідація вхідного сегмента перед обробкою
func validateSegment(filePath string) error {
    var buf bytes.Buffer
    
    // 🔹 Запускаємо dump у "тихий" режим
    cmd := exec.Command("mp4tool", "dump", "-offset", "-hex", filePath)
    cmd.Stdout = &buf
    
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("dump failed: %w", err)
    }
    
    output := buf.String()
    
    // 🔹 Перевірка наявності обов'язкових боксів
    if !strings.Contains(output, "[moof]") {
        return fmt.Errorf("missing moof box — not a fragmented MP4")
    }
    if !strings.Contains(output, "[trun]") {
        return fmt.Errorf("missing trun box — no frame timestamps")
    }
    
    // 🔹 Перевірка розмірів (евристично)
    if strings.Contains(output, "Size=0x0") {
        log.Printf("⚠️  Zero-size box detected — possible corruption")
    }
    
    return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути `-full` для великих боксів | Вивід: `Data=[...]` замість реального вмісту | Використовуйте `-full mdat,stss,ctts` для детального аналізу |
| Ігнорування `IsSupportedType()` | Непідтримувані бокси парситься з помилками | Перевіряйте `IsSupportedType()` перед `ReadPayload()` |
| Неправильне форматування для терміналу | Вивід "розламується" на кілька рядків | Використовуйте `terminalWidth` для обрізання довгих рядків |
| Забути `h.Expand()` | Вкладені бокси не відображаються | Завжди викликайте `h.Expand()` після обробки батьківського боксу |
| Неправильна обробка `ErrUnsupportedBoxVersion` | Краш при парсингу нової версії боксу | Обробляйте цю помилку окремо: `if err != mp4.ErrUnsupportedBoxVersion` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При використанні dump для дебагу:
    • Використовуйте -offset -hex для точного аналізу позицій
    • Додавайте -full для боксів, вміст яких потрібно перевірити
    • Фільтруйте вивід через grep: | grep trun, | grep -A3 avcC

[ ] При інтеграції у конвеєр обробки:
    • Запускайте dump у "тихий" режим (без -full) для швидкої валідації
    • Парсіть вивід для перевірки наявності обов'язкових боксів
    • Логувайте попередження при виявленні аномалій (нульові розміри, неподтримувані версії)

[ ] Для оптимізації продуктивності:
    • Використовуйте bufseekio з blockSize=128KB для великих файлів
    • Уникайте -full для mdat/free, якщо не потрібен сирий вміст
    • Фільтруйте бокси за типом на ранніх етапах обробника

[ ] Для дебагу самого інструменту:
    • Логувайте terminalWidth: log.Printf("📏 Terminal width: %d", terminalWidth)
    • Перевіряйте обрізання: if line.Len() > terminalWidth { ... }
    • Тестуйте з різними типами файлів: фрагментовані, DRM, QuickTime

[ ] Для тестування:
    • Створюйте тестові файли з відомою структурою
    • Порівнюйте вивід dump з очікуваним через golden-файли
    • Тестуйте крайні випадки: порожні файли, пошкоджені заголовки, >4 ГБ файли
```

---

## 🎯 Висновок

> **`dump` — це "швейцарський ніж" для аналізу MP4-файлів**, який забезпечує:
> • ✅ Гнучке форматування: від компактного огляду до детального дампу сирих даних
> • ✅ Розумну оптимізацію: автоматичне скорочення великих боксів, адаптація до терміналу
> • ✅ Ефективне читання: буферизація з історією блоків для швидкого seek
> • ✅ Безпечну обробку: коректна робота з непідтримуваними типами/версіями боксів
> • ✅ Зручну інтеграцію: можливість використання як CLI-інструменту або бібліотеки

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Миттєва візуалізація структури вхідних сегментів для швидкої валідації
- 🛠️ Зручний дебаг парсингу через людино-читабельний вивід
- ⚡ Ефективна обробка великих файлів завдяки буферизації
- 🧪 Надійне тестування через порівняння виводу з очікуваним

Потребуєте допомоги з інтеграцією `dump` у ваш конвеєр валідації або з налаштуванням кастомного формату виводу? Напишіть — покажу готовий код для вашого сценарію! 🚀📋