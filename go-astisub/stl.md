# 📡 Глибокий розбір: STL (EBU Teletext) реалізація в astisub

Цей файл — **повна реалізація підтримки формату STL** (EBU Tech 3264) у бібліотеці `astisub`. STL — це професійний стандарт для телесубтитрів, що використовується в мовленні по всій Європі та Близькому Сходу. Розберемо архітектуру, критичні деталі та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема STL в astisub

```
┌────────────────────────────────────────┐
│ 📦 STL File Structure                   │
├────────────────────────────────────────┤
│                                         │
│  [GSI Block - 1024 bytes]              │
│  ├─ Code Page Number (3 bytes)         │
│  ├─ Disk Format Code "STL25.01" (8B)   │
│  ├─ Display Standard Code (1 byte)     │
│  ├─ Character Code Table (2 bytes)     │
│  ├─ Language Code (2 bytes)            │
│  ├─ Titles, Publisher, Dates...        │
│  ├─ Timecode: Start-of-Programme       │
│  └─ MaxRows=23, MaxChars=40            │
│                                         │
│  [TTI Blocks - 128 bytes each] × N    │
│  ├─ Subtitle Group/Number              │
│  ├─ Extension Block Number             │
│  ├─ Cumulative Status                  │
│  ├─ Timecode In/Out (4 bytes each)     │
│  ├─ Vertical Position (1 byte)         │
│  ├─ Justification Code (1 byte)        │
│  ├─ Comment Flag (1 byte)              │
│  └─ Text Field (112 bytes)             │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 Ключові константи та мапінги

### 1. Character Code Tables (мова → таблиця)

```go
const (
    stlCharacterCodeTableNumberLatin         uint16 = 12336  // "003"
    stlCharacterCodeTableNumberLatinCyrillic uint16 = 12337  // "004"
    stlCharacterCodeTableNumberLatinArabic   uint16 = 12338  // "006" ⭐
    stlCharacterCodeTableNumberLatinGreek    uint16 = 12339  // "007"
    stlCharacterCodeTableNumberLatinHebrew   uint16 = 12340  // "008"
)
```

> ⚠️ **Критично для арабської**: Завжди використовуйте `stlCharacterCodeTableNumberLatinArabic` (12338), інакше отримаєте `"????"` замість тексту.

### 2. Display Standard Codes

```go
const (
    stlDisplayStandardCodeOpenSubtitling = "0"  // "Відкриті" субтитри (як SRT)
    stlDisplayStandardCodeLevel1Teletext = "1"  // Teletext Level 1 (40×23, control codes)
    stlDisplayStandardCodeLevel2Teletext = "2"  // Teletext Level 2 (розширені можливості)
)
```

### 3. Framerate Mapping

```go
var stlFramerateMapping = astikit.NewBiMap().
    Set("STL25.01", 25).  // 25 fps → 100 units (frames×4)
    Set("STL30.01", 30)   // 30 fps → 120 units
```

> 💡 **Важливо**: Параметр `framerate` у функціях — це **fps**, але для розрахунку кадрів використовується `fps × 4`.

---

## ⏱️ Duration Handling — кодування часу

### Формат таймкоду у STL:
```
Строка: "12345678" → 12:34:56:78
Байти:  [0x0C][0x22][0x38][0x4E] → [HH][MM][SS][FF]
```

### Функції парсингу/форматування:

```go
// Парсинг строки → time.Duration
func parseDurationSTL(i string, framerate int) (time.Duration, error) {
    // "12345678" + 25fps → 12h34m56.780s
    // Формула кадрів: frames × 1000 / framerate
}

// Форматування duration → строка
func formatDurationSTL(d time.Duration, framerate int) string {
    // 12h34m56.780s + 25fps → "12345678"
}

// Бінарне кодування для TTI блоку
func formatDurationSTLBytes(d time.Duration, framerate int) []byte {
    // Повертає [4]byte: [HH][MM][SS][FF]
}

// Декодування бінарних даних
func parseDurationSTLBytes(b []byte, framerate int) time.Duration {
    // [4]byte → time.Duration
}
```

### ✅ Ваш use-case: синхронізація з PTS у HLS

```go
// PTS у HLS — 90kHz clock (1/90000 секунди)
// STL таймкоди — кадри (напр. 25fps = 40ms/frame)

type STLTimeSync struct {
    framerate      int  // 25 або 30
    segmentBasePTS uint64
}

// STL duration → PTS для WebSocket
func (s *STLTimeSync) DurationToPTS(d time.Duration) uint64 {
    ns := int64(d)
    pts := uint64(ns * 90 / 1e6)  // наносекунди → 90kHz
    return s.segmentBasePTS + pts
}

// PTS → STL duration для запису в TTI
func (s *STLTimeSync) PTSToDuration(pts uint64) time.Duration {
    relative := pts - s.segmentBasePTS
    ns := int64(relative) * 1e6 / 90
    return time.Duration(ns)
}

// Форматування для TTI блоку
func (s *STLTimeSync) DurationToTTIBytes(d time.Duration) []byte {
    hh := byte(d / time.Hour)
    mm := byte((d % time.Hour) / time.Minute)
    ss := byte((d % time.Minute) / time.Second)
    
    // Кадри: залишок у наносекундах → кадри
    frames := byte((d % time.Second).Nanoseconds() * int64(s.framerate) / 1e9)
    
    return []byte{hh, mm, ss, frames}
}
```

---

## 🔤 stlCharacterHandler — декодування символів

### Архітектура:

```go
type stlCharacterHandler struct {
    accent string              // "буфер" для діакритики (напр. 0xC8 = umlaut)
    c      uint16             // ID таблиці (12336-12340)
    m      *astikit.BiMap     // мапа: byte → UTF-8 string
}
```

### Логіка декодування:

```go
func (h *stlCharacterHandler) decode(i byte) []byte {
    // 1. Шукаємо символ у таблиці
    vi, ok := h.m.Get(int(i))
    if !ok { return nil }
    v := vi.(string)
    
    // 2. Якщо є накопичена діакритика — комбінуємо
    if len(h.accent) > 0 {
        // NFC нормалізація: "a" + "\u0308" → "ä"
        o = norm.NFC.Bytes([]byte(v + h.accent))
        h.accent = ""  // скидаємо після використання
        return o
    }
    
    // 3. Для Latin таблиці: 0xC0-0xCF — це діакритичні префікси
    if h.c == stlCharacterCodeTableNumberLatin && i >= 0xc0 && i <= 0xcf {
        h.accent = v  // зберігаємо для наступного символу
        return nil    // нічого не повертаємо ще
    }
    
    // 4. Звичайний символ
    return []byte(v)
}
```

### 🌍 Таблиці кодування:

```go
// Latin таблиця (12336) — найбільш повна
stlCharacterCodeTables[12336] = astikit.NewBiMap().
    Set(0x20, " ").Set(0x41, "A")...  // базові символи
    Set(0xa0, "\u00A0")  // non-breaking space
    Set(0xc8, "\u0308")  // combining umlaut (діакритика)
    Set(0xe1, "Æ")...    // розширені символи
    // ... всього ~200 мапінгів
```

### ✅ Ваш use-case: підтримка арабської мови

```go
// Для Al Arabiya каналу:
func (p *SubtitleProcessor) createArabicDecoder() *stlCharacterHandler {
    // Використовуємо Arabic таблицю (12338)
    handler, err := newSTLCharacterHandler(stlCharacterCodeTableNumberLatinArabic)
    if err != nil {
        log.Fatal("failed to create Arabic decoder", "err", err)
    }
    return handler
}

// Обробка розриву між сегментами (важливо!)
func (p *SubtitleProcessor) onSegmentBoundary() {
    // Скидаємо decoder, щоб уникнути "залипання" діакритики
    // між останнім байтом одного сегменту та першим наступного
    p.characterHandler = p.createArabicDecoder()
}
```

> ⚠️ **Критично**: Якщо не скидати `stlCharacterHandler` між сегментами, control code (напр. 0xC8) з кінця одного сегменту може помилково комбінуватися з першим байтом наступного → некоректний символ.

---

## 🎨 stlStyler — inline-стилі

### Контрольні коди стилів:

```
Діапазон 0x00-0x07: Кольори
────────────────────────────
0x00 → Black    0x04 → Blue
0x01 → Red      0x05 → Magenta
0x02 → Green    0x06 → Cyan
0x03 → Yellow   0x07 → White

Діапазон 0x80-0x85: Стилі тексту
────────────────────────────────
0x80 → Start Italics     0x81 → Stop Italics
0x82 → Start Underline   0x83 → Stop Underline
0x84 → Start Boxing      0x85 → Stop Boxing
```

### Архітектура styler:

```go
type stlStyler struct {
    boxing    *bool   // nil = не змінювати
    color     *Color
    italics   *bool
    underline *bool
}

func (s *stlStyler) parseSpacingAttribute(i byte) {
    switch i {
    case 0x01: s.color = ColorRed
    case 0x80: s.italics = astikit.BoolPtr(true)
    case 0x81: s.italics = astikit.BoolPtr(false)
    // ... інші коди
    }
}

// Перевірка чи стиль змінився
func (s *stlStyler) hasChanged(sa *StyleAttributes) bool {
    return s.color != sa.STLColor || s.italics != sa.STLItalics || ...
}

// Застосування змін до StyleAttributes
func (s *stlStyler) update(sa *StyleAttributes) {
    if s.color != nil { sa.STLColor = s.color }
    if s.italics != nil { sa.STLItalics = s.italics }
    // ...
}
```

### ✅ Ваш use-case: мультиязычне стилізування

```go
// Кольорове кодування мов для візуального розрізнення
var LanguageSTLColors = map[string]*astisub.Color{
    "ar": astisub.ColorYellow,   // золотий → yellow
    "en": astisub.ColorCyan,     // блакитний → cyan  
    "ru": astisub.ColorMagenta,  // червоний → magenta
}

// Застосування стилю до субтитру
func (p *SubtitleProcessor) applySTLStyle(item *astisub.Item, lang string, emphasize bool) {
    for lineIdx := range item.Lines {
        for itemIdx := range item.Lines[lineIdx].Items {
            li := &item.Lines[lineIdx].Items[itemIdx]
            
            if li.InlineStyle == nil {
                li.InlineStyle = &astisub.StyleAttributes{}
            }
            
            // Колір за мовою
            if color, ok := LanguageSTLColors[lang]; ok {
                li.InlineStyle.STLColor = color
            }
            
            // Курсив для акценту
            if emphasize {
                li.InlineStyle.STLItalics = astikit.BoolPtr(true)
            }
            
            // Проганяємо пропагацію для веб-експорту
            li.InlineStyle.propagateSTLAttributes()
        }
    }
}
```

---

## 📦 ReadFromSTL — парсинг STL файлу

### Потік обробки:

```
1. Читання GSI блоку (1024 байти)
   ↓
2. parseGSIBlock() → метадані + framerate + character table
   ↓
3. newSTLCharacterHandler(table) → декодер символів
   ↓
4. Цикл: читання TTI блоків (128 байт кожен)
   ├─ parseTTIBlock() → таймінги, позиція, текст
   ├─ Якщо displayStandardCode == "0" → parseOpenSubtitleRow()
   ├─ Інакше → parseTeletextRow() (з control codes)
   ├─ Створення astisub.Item з таймінгами та стилем
   └─ Додавання до s.Items[]
   ↓
5. Повернення *Subtitles
```

### Ключові моменти парсингу:

```go
// 1. Корекція таймінгів відносно TCF (Timecode Start-of-Programme)
item.StartAt = t.timecodeIn - o.Metadata.STLTimecodeStartOfProgramme
item.EndAt = t.timecodeOut - o.Metadata.STLTimecodeStartOfProgramme

// 2. Вибір парсера за displayStandardCode
if g.displayStandardCode == stlDisplayStandardCodeOpenSubtitling {
    // "Відкриті" субтитри — простий текст
    parseOpenSubtitleRow(i, ch, func() styler { return newSTLStyler() }, text)
} else {
    // Teletext — з control codes для кольорів/стилів
    parseTeletextRow(i, ch, func() styler { return newSTLStyler() }, text)
}

// 3. Позиціонування: VerticalPosition → WebVTT line%
position := STLPosition{
    MaxRows:          g.maximumNumberOfDisplayableRows,  // зазвичай 23
    Rows:             len(rows),
    VerticalPosition: t.verticalPosition,  // 1-23 для teletext
}
styleAttributes.propagateSTLAttributes()  // конвертація у WebVTTLine
```

### ✅ Ваш use-case: парсинг без файлів (streaming)

```go
// ProcessSTLSegment — обробка одного сегменту з пам'яті
func (p *SubtitleProcessor) ProcessSTLSegment(data []byte, seqNum uint64, pts time.Duration) error {
    // 1. Парсинг з bytes.Reader (без тимчасових файлів)
    reader := bytes.NewReader(data)
    subs, err := astisub.ReadFromSTL(reader, astisub.STLOptions{
        IgnoreTimecodeStartOfProgramme: true,  // важливо для сегментів!
    })
    if err != nil {
        return fmt.Errorf("stl parse: %w", err)
    }
    
    // 2. Корекція часу відносно початку стріму
    streamOffset := pts.Sub(p.streamStartTime)
    for _, item := range subs.Items {
        item.StartAt += streamOffset
        item.EndAt += streamOffset
    }
    
    // 3. Застосування лейауту для мов
    for _, item := range subs.Items {
        lang := detectLanguage(item)  // ваша логіка
        p.applySTLStyle(item, lang, false)
    }
    
    // 4. Конвертація у WebSocket повідомлення
    for _, item := range subs.Items {
        msg := p.itemToMessage(item, seqNum)
        p.wsSender.Broadcast(p.channelID, msg)
    }
    
    return nil
}
```

> ⚠️ **Важливо**: `IgnoreTimecodeStartOfProgramme: true` критично для сегментів — інакше таймінги будуть відраховуватися від початку "програми", а не від початку сегменту.

---

## ✍️ WriteToSTL — експорт у STL формат

### Потік генерації:

```
1. newGSIBlock(s) → формування метаданих з дефолтами
   ↓
2. Запис GSI блоку (1024 байти) у io.Writer
   ↓
3. Для кожного item у s.Items:
   ├─ newTTIBlock(item, idx, displayStandardCode)
   ├─ encodeTextSTL() → конвертація UTF-8 → STL bytes
   ├─ formatDurationSTLBytes() → таймінги у [4]byte
   └─ Запис TTI блоку (128 байт)
   ↓
4. Повернення nil (успіх)
```

### Генерація GSI блоку з дефолтами:

```go
func newGSIBlock(s Subtitles) *gsiBlock {
    g := &gsiBlock{
        // Дефолтні значення (якщо Metadata не заповнена)
        characterCodeTableNumber: stlCharacterCodeTableNumberLatin,  // ⚠️ змінити для Arabic!
        codePageNumber:           stlCodePageNumberMultilingual,
        displayStandardCode:      stlDisplayStandardCodeLevel1Teletext,
        framerate:                25,
        languageCode:             stlLanguageCodeFrench,  // ⚠️ змінити!
        maximumNumberOfDisplayableRows: 23,
        maximumNumberOfDisplayableCharactersInAnyTextRow: 40,
        // ...
    }
    
    // Перезапис з Metadata якщо є
    if s.Metadata != nil {
        if s.Metadata.Framerate > 0 {
            g.framerate = s.Metadata.Framerate
        }
        // ... інші поля
    }
    
    return g
}
```

### ✅ Ваш use-case: експорт STL для архіву

```go
// ExportSTLForArchive — підготовка STL файлу для довгострокового зберігання
func (p *ArchiveProcessor) ExportSTLForArchive(subs *astisub.Subtitles, channelID string) ([]byte, error) {
    // 1. Налаштування метаданих з конфігурації каналу
    cfg := p.getChannelConfig(channelID)
    if subs.Metadata == nil {
        subs.Metadata = &astisub.Metadata{}
    }
    
    subs.Metadata.Framerate = 25  // або з cfg
    subs.Metadata.Language = cfg.LanguageCode  // "ara", "eng"
    subs.Metadata.STLDisplayStandardCode = "1"  // Teletext Level 1
    subs.Metadata.STLCountryOfOrigin = cfg.CountryCode  // "SAU"
    subs.Metadata.STLPublisher = cfg.Publisher  // "Al Arabiya"
    
    // 2. Критично: встановити правильну character table для Arabic!
    // Це робиться автоматично в newGSIBlock через languageCode мапінг,
    // але можна задати явно:
    // (потрібно модифікувати astisub або встановити languageCode="75" для Arabic)
    
    // 3. Експорт у bytes.Buffer (без файлу)
    var buf bytes.Buffer
    if err := subs.WriteToSTL(&buf); err != nil {
        return nil, fmt.Errorf("stl write: %w", err)
    }
    
    return buf.Bytes(), nil
}
```

> ⚠️ **Увага**: astisub автоматично вибирає `characterCodeTableNumber` на основі `languageCode` через `stlLanguageMapping`. Переконайтеся, що `Metadata.Language` встановлено правильно ("ara" для арабської), інакше отримаєте неправильну таблицю.

---

## 🔤 encodeTextSTL / decode — конвертація тексту

### Логіка кодування (UTF-8 → STL bytes):

```go
func encodeTextSTL(i string) []byte {
    // 1. NFD нормалізація: "ä" → "a" + "\u0308" (combining umlaut)
    i = string(norm.NFD.Bytes([]byte(i)))
    
    var o []byte
    for _, c := range i {
        // 2. Шукаємо пряме відображення в stlUnicodeMapping
        if v, ok := stlUnicodeMapping.GetInverse(string(c)); ok {
            o = append(o, v.(byte))
        
        // 3. Якщо це діакритика — вставляємо після попереднього символу
        } else if v, ok := stlUnicodeDiacritic.GetInverse(string(c)); ok {
            o = append(o[:len(o)-1], v.(byte), o[len(o)-1])
        
        // 4. Fallback: ASCII символ
        } else {
            o = append(o, byte(c))
        }
    }
    return o
}
```

### 🗺️ Мапінги:

```go
// stlUnicodeMapping: UTF-8 → STL byte
stlUnicodeMapping = astikit.NewBiMap().
    Set(byte('\x8a'), "\u000a").  // Line break
    Set(byte('\xa9'), "\u2018").  // ' (left single quote)
    Set(byte('\xe1'), "\u00C6").  // Æ
    // ... ~50 мапінгів

// stlUnicodeDiacritic: combining marks → STL control code
stlUnicodeDiacritic = astikit.NewBiMap().
    Set(byte('\xc8'), "\u0308").  // 0xC8 → combining umlaut (¨)
    Set(byte('\xc1'), "\u0300").  // 0xC1 → combining grave (`)
    // ... 12 діакритик
```

### ✅ Ваш use-case: валідація арабського тексту

```go
// validateArabicText — перевірка чи текст сумісний з Arabic STL таблицею
func validateArabicText(text string) error {
    // Arabic STL таблиця підтримує обмежений набір символів
    // Перевіряємо чи немає неподтримуваних символів
    
    handler, _ := newSTLCharacterHandler(stlCharacterCodeTableNumberLatinArabic)
    
    for _, r := range text {
        // Спроба знайти символ у таблиці
        found := false
        for b := byte(0x20); b < 0xff; b++ {
            if decoded := handler.decode(b); string(decoded) == string(r) {
                found = true
                break
            }
        }
        if !found && !unicode.IsSpace(r) {
            return fmt.Errorf("unsupported Arabic character: %q (U+%04X)", r, r)
        }
    }
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"????" замість арабського тексту** | Неправильна Character Code Table | Встановіть `Metadata.Language = "ara"` або вручну `characterCodeTableNumber = 12338` |
| **Таймінги зсунуті на години** | TCF (Start-of-Programme) не враховано | Використовуйте `IgnoreTimecodeStartOfProgramme: true` при парсингу сегментів |
| **Діакритика "з'їдає" наступний символ** | accumulator не скинуто між сегментами | Створюйте новий `stlCharacterHandler` для кожного сегменту |
| **Кадри неправильно конвертуються** | Неправильний `framerate` параметр | Завжди передавайте `25` або `30` (не `100`/`120`!) у функції duration |
| **Boxing не відображається у WebVTT** | Немає прямого аналога в веб-форматах | Ігноруйте або конвертуйте у CSS-клас `.boxed { border: 1px solid white; }` |
| **Текст обрізається на 112 байт** | TTI Text Field має фіксований розмір | Увімкніть перенос рядків (`\x8a`) або скоротіть текст перед експортом |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування GSI блоку на рівні каналу:

```go
type CachedGSIBlock struct {
    data  [1024]byte
    hash  string  // для детекту змін конфігурації
    valid bool
}

var gsiCache = sync.Map{}  // channelID → CachedGSIBlock

func (p *STLExporter) getGSIBlock(channelID string) *[1024]byte {
    if cached, ok := gsiCache.Load(channelID); ok {
        c := cached.(CachedGSIBlock)
        if c.valid && c.hash == p.getConfigHash(channelID) {
            return &c.data
        }
    }
    
    // Генеруємо новий GSI
    subs := astisub.NewSubtitles()
    subs.Metadata = p.getChannelMetadata(channelID)
    gsi := newGSIBlock(*subs)
    data := gsi.bytes()
    
    // Кешуємо
    var cached [1024]byte
    copy(cached[:], data)
    gsiCache.Store(channelID, CachedGSIBlock{
        data:  cached,
        hash:  p.getConfigHash(channelID),
        valid: true,
    })
    
    return &cached
}
```

### 2. Пакетна конвертація таймкодів:

```go
// Замість індивідуального formatDurationSTLBytes для кожного Item:
func batchEncodeTTITimes(items []*astisub.Item, framerate int) [][8]byte {
    result := make([][8]byte, len(items))
    for i, item := range items {
        copy(result[i][0:4], formatDurationSTLBytes(item.StartAt, framerate))
        copy(result[i][4:8], formatDurationSTLBytes(item.EndAt, framerate))
    }
    return result
}
```

### 3. Lazy text encoding:

```go
// Не кодуйте текст до моменту запису у буфер
type LazySTLText struct {
    utf8     string
    encoded  []byte
    dirty    bool
}

func (l *LazySTLText) Encode() []byte {
    if l.dirty || l.encoded == nil {
        l.encoded = encodeTextSTL(l.utf8)
        l.dirty = false
    }
    return l.encoded
}
```

---

## 📋 Чек-лист інтеграції STL

```go
// ✅ 1. Налаштування метаданих перед експортом
subs.Metadata = &astisub.Metadata{
    Framerate:              25,  // обов'язково!
    Language:               "ara", // ISO 639-2 code для Arabic
    STLDisplayStandardCode: "1",   // 0=Open, 1=Teletext L1
    STLCountryOfOrigin:     "SAU",
    // ... інші поля
}

// ✅ 2. Парсинг з правильними опціями для сегментів
opts := astisub.STLOptions{
    IgnoreTimecodeStartOfProgramme: true,  // критично для HLS!
}
subs, err := astisub.ReadFromSTL(reader, opts)

// ✅ 3. Корекція таймінгів під сегмент
segmentOffset := pts.Sub(segmentStartTime)
for _, item := range subs.Items {
    item.StartAt += segmentOffset
    item.EndAt += segmentOffset
}

// ✅ 4. Застосування стилів/кольорів для мов
for _, item := range subs.Items {
    lang := detectLanguage(item)
    applySTLStyle(item, lang, false)
}

// ✅ 5. Експорт у STL (без проміжного файлу)
var buf bytes.Buffer
if err := subs.WriteToSTL(&buf); err != nil {
    return fmt.Errorf("stl export: %w", err)
}

// ✅ 6. Валідація Arabic тексту (опціонально)
if err := validateArabicText(extractText(subs)); err != nil {
    log.Warn("arabic text validation", "err", err)
}

// ✅ 7. Метрики
monitoring.STLParsed.Inc()
monitoring.STLParseLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [EBU Tech 3264-E](https://tech.ebu.ch/docs/tech/tech3264.pdf) — офіційна специфікація STL (GSI/TTI блоки, таймкоди, character tables)
- 📄 [EBU R 37](https://tech.ebu.ch/docs/r/r037.pdf) — Character Code Tables (деталі мапінгів для Latin/Arabic/Cyrillic)
- 💻 [astisub stl.go](https://github.com/asticode/go-astisub/blob/master/stl.go) — повний вихідний код
- 🧪 [astisub testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади STL файлів для тестів
- 🐍 [stl2srt.py](https://github.com/yanncoupin/stl2srt) — Python референсна реалізація (згадана в коді)

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **арабською мовою в реальному часі**:
> 1. **Завжди встановлюйте `Metadata.Language = "ara"`** перед парсингом/експортом — це автоматично вибере правильну Character Code Table.
> 2. **Використовуйте `IgnoreTimecodeStartOfProgramme: true`** при парсингу сегментів — інакше таймінги будуть зсунуті.
> 3. **Створюйте новий `stlCharacterHandler` для кожного сегменту** — це уникне "залипання" control codes між сегментами.
> 4. **Тестуйте round-trip** (STL → astisub → STL) для вашого каналу — переконайтеся, що арабський текст зберігається коректно.
> 5. **Додайте логування для `decode() == nil`** — це допоможе відлагодити проблеми з неподтримуваними символами.

Потрібен приклад повного циклу: **Teletext TS → astisub.ReadFromSTL → WebSocket SubtitleMessage → astisub.WriteToSTL для архіву** з урахуванням арабської мови та синхронізації часу? Готовий допомогти! 🚀