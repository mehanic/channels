# 📡 Глибокий розбір: STL (EBU Teletext) підтримка в astisub

Цей тестовий файл демонструє **роботу з форматом STL** — професійним стандартом телесубтитрів (EBU 3264), який використовується в мовленні. Розберемо архітектуру, критичні нюанси та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Огляд тестованих сценаріїв

```
┌────────────────────────────────────────┐
│ 🎯 Ключові можливості STL в astisub    │
├────────────────────────────────────────┤
│                                         │
│ 📦 Формат файлу:                        │
│ • GSI блок (1024 байти) — метадані     │
│ • TTI блоки — дані субтитрів           │
│ • Кодування: 8-бітні символи + TTI     │
│                                         │
│ ⚙️  Конфігурація:                       │
│ • STLOptions{IgnoreTimecodeStartOfProgramme} │
│ • DisplayStandardCode: 0/1/2           │
│ • Framerate: 25/30 fps                 │
│                                         │
│ 🎨 Кольори:                             │
│ • 4-бітова палітра (16 кольорів)       │
│ • Конвертація: WebVTT/TTML → STL       │
│ • Round-trip збереження атрибутів      │
│                                         │
│ ⏱️  Таймкоди:                           │
│ • TCN + TCI: 4-байтне кодування        │
│ • Корекція offset через TSO parameter  │
│                                         │
│ 🔄 Конвертація форматів:                │
│ • TTML → STL (з авто-генерацією GSI)   │
│ • WebVTT → STL (з мапінгом кольорів)   │
│ • STL → WebVTT/TTML (пропагація)       │
│                                         │
└────────────────────────────────────────┘
```

---

## 📦 1. STL Format Basics — Структура файлу

### GSI Block (Group Subtitle Information) — 1024 байти:

```
Байти   Поле                          Значення приклад
─────────────────────────────────────────────────────
0-2     Code Page Number              "850" (Latin-1)
3-10    Disk Format Code              "STL25.01" (25fps, Level 1)
11      Display Standard Code         "0"=Open, "1"=Teletext Level 1, "2"=Level 2
12-14   Character Code Table Number   "003" (Latin/Cyrillic/Arabic)
15-16   Language Code                 "eng", "ara", "rus"
17-18   Original Programme Title      "News Bulletin"
19-34   Original Episode Title        "Episode 42"
35-49   Translated Programme Title    "Бюлетень новин"
50-64   Translated Episode Title      "Епізод 42"
65-79   Translator's Name             "Translation Team"
80-94   Translator's Contact          "contact@example.com"
95-109  Subtitle List Reference Code  "12345678"
110-121 Country of Origin             "FRA", "UKR", "SAU"
122-129 Publisher                     "Copyright Holder"
130-137 Editor's Name                 "Editor Name"
138-152 Editor's Contact              "editor@example.com"
153-157 Revision Date                 YYMMDD format
158-163 Revision Number               "00001"
164-169 Maximum Number of Displayable Characters/Row  "40"
170-175 Maximum Number of Displayable Rows            "23"
176-181 Time Code: Start-of-Programme (TCF)           HH:MM:SS:FF
182-187 Time Code: First Subtitle (TCF)               HH:MM:SS:FF
188-191 Total Number of Subtitle Groups               "0001"
...     (решта — зарезервовано/паддінг)
```

### TTI Block (Text and Timing Information) — один на субтитр:

```
Байти   Поле                          Опис
─────────────────────────────────────────────────────
0       Subtitle Group Number         Групування (0-255)
1       Subtitle Number               Номер в групі (0-255)
2       Extension Block Number        Для розширених даних
3       Extension Block Count         Кількість розширень
4       Time Code In (TCI)            Початок: HH:MM:SS:FF
8       Time Code Out (TCO)           Кінець: HH:MM:SS:FF
12      Vertical Position             Рядок 0-22 (teletext) або 0-99 (in-vision)
13      Justification                 1=unchanged, 2=left, 3=centered, 4=right
14      Comment Flag                  0=normal, 1=comment (ігнорується)
15-112  Text Field                    40 байт × 24 рядки = 960 байт (макс.)
```

> 💡 **Ключовий момент**: Текст у TTI — це **сирий 8-бітний потік**, який декодується через `Character Code Table` з GSI. Для арабської/кирилиці потрібна правильна таблиця кодування.

---

## ⚙️ 2. STLOptions — Конфігурація парсингу

```go
type STLOptions struct {
    // Ігнорувати TCF (Time Code: Start-of-Programme) з GSI
    // Корисно, якщо ваш потік має власний таймлайн
    IgnoreTimecodeStartOfProgramme bool
}
```

### ✅ Ваш use-case: корекція таймінгів для HLS-сегментів

```go
// Проблема: STL файл має абсолютні таймкоди від початку програми,
// а ваш HLS-сегмент — відносні від початку сегменту.

// Рішення 1: Ігнорувати TCF при парсингу
opts := astisub.STLOptions{
    IgnoreTimecodeStartOfProgramme: true,
}
subs, err := astisub.ReadFromSTL(reader, opts)
// Тепер таймінги відраховуються від першого субтитру

// Рішення 2: Вручну зсунути після парсингу
streamOffset := segmentPTS.Sub(segmentStartTime)
subs.Add(-streamOffset)  // зсув назад до відносного часу
```

### Тест `TestIgnoreTimecodeStartOfProgramme`:
```go
// Файл має TCF = 00:01:39:00 (99 секунд)
// Без опції: перший субтитр має StartAt = 0
// З опцією: StartAt = 99s (зберігається абсолютне значення)

opts := astisub.STLOptions{IgnoreTimecodeStartOfProgramme: true}
s, _ := astisub.ReadFromSTL(r, opts)
assert.Equal(t, 99*time.Second, s.Items[0].StartAt)
```

> ⚠️ **Увага**: `IgnoreTimecodeStartOfProgramme=true` **не ігнорує** TCF, а навпаки — **зберігає** абсолютні значення. Назва опції може вводити в оману!

---

## 🎨 3. Color Handling — Кольори в STL

### Обмеження STL:
- **16 кольорів** (4 біти): black, red, green, yellow, blue, magenta, cyan, white + їхні "flash" версії
- **Немає alpha-каналу** — прозорість не підтримується
- **Колір застосовується до всього TTI-блоку**, не до окремих слів

### Мапінг кольорів з інших форматів:

```go
// WebVTT/TTML → STL конвертація (з testSTLColorsFromWebVTT):
// <c.lime>текст</c> → STLColor = ColorGreen
// <c.magenta>текст</c> → STLColor = ColorMagenta

// Мапінг у astisub:
var webvttToSTLColors = map[string]*astisub.Color{
    "black":   astisub.ColorBlack,
    "red":     astisub.ColorRed,
    "green":   astisub.ColorGreen,   // lime → green
    "yellow":  astisub.ColorYellow,
    "blue":    astisub.ColorBlue,
    "magenta": astisub.ColorMagenta,
    "cyan":    astisub.ColorCyan,
    "white":   astisub.ColorWhite,
    // Flash-версії (не підтримуються в astisub напряму)
}
```

### ✅ Ваш use-case: мультиязычне кольорове кодування

```go
// Оскільки STL підтримує тільки 8 базових кольорів,
// використовуємо їх для розрізнення мов:

var LanguageSTLColors = map[string]*astisub.Color{
    "ar": astisub.ColorYellow,  // золотий → yellow (найближчий)
    "en": astisub.ColorCyan,    // блакитний → cyan
    "ru": astisub.ColorMagenta, // червоний → magenta (або red)
}

// При конвертації з WebVTT/TTML в STL:
func (p *SubtitleProcessor) applySTLColor(item *astisub.Item, lang string) {
    if color, ok := LanguageSTLColors[lang]; ok {
        for lineIdx := range item.Lines {
            for itemIdx := range item.Lines[lineIdx].Items {
                li := &item.Lines[lineIdx].Items[itemIdx]
                if li.InlineStyle == nil {
                    li.InlineStyle = &astisub.StyleAttributes{}
                }
                // STLColor має пріоритет при експорті в STL
                li.InlineStyle.STLColor = color
            }
        }
    }
}
```

### Round-trip тестування (з тестів):
```go
// 1. Створюємо субтитри з кольорами
s := astisub.NewSubtitles()
s.Items = []*astisub.Item{{
    Lines: []astisub.Line{{Items: []astisub.LineItem{{
        Text: "Red text",
        InlineStyle: &astisub.StyleAttributes{STLColor: astisub.ColorRed},
    }}}},
}}

// 2. Експортуємо в STL
var buf bytes.Buffer
s.WriteToSTL(&buf)

// 3. Імпортуємо назад
s2, _ := astisub.ReadFromSTL(&buf, astisub.STLOptions{})

// 4. Перевіряємо збереження кольору
assert.Equal(t, astisub.ColorRed, s2.Items[0].Lines[0].Items[0].InlineStyle.STLColor)
```

> 💡 **Порада**: Для **арабської мови** переконайтеся, що `Character Code Table Number` у GSI встановлено на значення, що підтримує арабські символи (напр. "006" для Arabic). Інакше текст буде відображатися як "????".

---

## 🔄 4. Format Conversion — Конвертація між форматами

### TTML → STL (з тесту `TestTTMLToSTLGSIBlock`):

```go
// Проблема: TTML файли не мають STL-метаданих (framerate, display standard)
// Рішення: astisub авто-генерує GSI блок з дефолтними значеннями

s, _ := astisub.OpenFile("input.ttml")  // TTML без STL-метаданих
assert.Empty(t, s.Metadata.STLDisplayStandardCode)  // порожньо

// При експорті в STL:
var buf bytes.Buffer
s.WriteToSTL(&buf)  // авто-генерація GSI

// Перевірка GSI:
stlData := buf.Bytes()
diskFormatCode := string(stlData[3:11])  // байти 3-10
assert.True(t, diskFormatCode == "STL25.01" || diskFormatCode == "STL30.01")

displayStandardCode := string(stlData[11:12])  // байт 11
assert.True(t, displayStandardCode == "0" || displayStandardCode == "1")  // 0=Open, 1=Teletext
```

### Дефолтні значення при авто-генерації GSI:
```go
// Якщо Metadata не заповнена, astisub використовує:
defaultGSI := astisub.Metadata{
    Framerate:              25,  // дефолтний framerate
    STLDisplayStandardCode: "1", // Teletext Level 1 (найпоширеніший)
    STLMaximumNumberOfDisplayableCharactersInAnyTextRow: astikit.IntPtr(40),
    STLMaximumNumberOfDisplayableRows:                   astikit.IntPtr(23),
    // Інші поля — порожні/дефолтні
}
```

### ✅ Ваш use-case: підготовка STL для архіву або мовлення

```go
// Конвертація отриманих WebVTT-субтитрів у STL для архіву
func (p *ArchiveProcessor) webvttToSTL(webvttData []byte, channelID string) ([]byte, error) {
    // 1. Парсинг WebVTT
    subs, err := astisub.ReadFromWebVTT(bytes.NewReader(webvttData))
    if err != nil {
        return nil, fmt.Errorf("webvtt parse: %w", err)
    }
    
    // 2. Налаштування STL-метаданих з конфігурації каналу
    cfg := p.getChannelConfig(channelID)
    subs.Metadata = &astisub.Metadata{
        Framerate:              25,  // або з cfg
        Language:               cfg.LanguageCode,  // "ara", "eng", "rus"
        STLDisplayStandardCode: "1",  // Teletext Level 1
        STLMaximumNumberOfDisplayableCharactersInAnyTextRow: astikit.IntPtr(40),
        STLMaximumNumberOfDisplayableRows:                   astikit.IntPtr(23),
        STLCountryOfOrigin:     cfg.CountryCode,  // "SAU", "UKR", etc.
        STLPublisher:           cfg.Publisher,    // "Al Arabiya", etc.
        Title:                  cfg.ProgramTitle,
        // TCF (Time Code: Start-of-Programme) — опціонально
    }
    
    // 3. Застосування STL-кольорів для мов
    for _, item := range subs.Items {
        // Визначаємо мову за текстом або метаданими
        lang := detectLanguage(item)  // ваша логіка
        p.applySTLColor(item, lang)
    }
    
    // 4. Експорт у STL
    var buf bytes.Buffer
    if err := subs.WriteToSTL(&buf); err != nil {
        return nil, fmt.Errorf("stl write: %w", err)
    }
    
    return buf.Bytes(), nil
}
```

---

## ⏱️ 5. Timecode Handling — Робота з часом у STL

### Формат таймкоду в STL:
```
TCI/TCO: 4 байти = [HH][MM][SS][FF]
- HH: 0-23 (години)
- MM: 0-59 (хвилини)  
- SS: 0-59 (секунди)
- FF: 0-24/29 (кадри, залежить від framerate)

Приклад: 01:23:45:12 при 25fps = 1h 23m 45.48s
```

### Конвертація в time.Duration:
```go
// У astisub внутрішньо використовується time.Duration (наносекунди)
// Конвертація при парсингу:
func stlTimecodeToDuration(hh, mm, ss, ff byte, framerate int) time.Duration {
    frameDuration := time.Second / time.Duration(framerate)
    return time.Duration(hh)*time.Hour + 
           time.Duration(mm)*time.Minute + 
           time.Duration(ss)*time.Second + 
           time.Duration(ff)*frameDuration
}
```

### ✅ Ваш use-case: синхронізація з PTS у HLS

```go
// PTS у HLS — у одиницях 90kHz (1/90000 секунди)
// STL таймкоди — у кадрах (напр. 25fps = 40ms/frame)

func (p *SubtitleSync) stlToPTS(tci time.Duration, segmentStartTime time.Time, framerate int) uint64 {
    // 1. Конвертуємо duration у секунди
    seconds := float64(tci) / float64(time.Second)
    
    // 2. Додаємо зсув до початку сегменту
    absoluteTime := segmentStartTime.Add(tci)
    
    // 3. Конвертуємо у 90kHz одиниці (PTS формат)
    pts := uint64(absoluteTime.UnixNano() * 90 / 1e6)
    
    return pts
}

// Зворотна конвертація: PTS → STL timecode
func (p *SubtitleSync) ptsToSTL(pts uint64, segmentStartTime time.Time, framerate int) (hh, mm, ss, ff byte) {
    // 1. Конвертуємо PTS у time.Time
    t := time.Unix(0, int64(pts)*1e6/90)
    
    // 2. Віднімаємо початок сегменту для відносного часу
    relative := t.Sub(segmentStartTime)
    
    // 3. Розбиваємо на компоненти
    hh = byte(relative / time.Hour)
    mm = byte((relative % time.Hour) / time.Minute)
    ss = byte((relative % time.Minute) / time.Second)
    
    // 4. Кадри: залишок у наносекундах → кадри
    frameDuration := time.Second / time.Duration(framerate)
    ff = byte((relative % time.Second) / frameDuration)
    
    return
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Арабський текст як "????"** | Неправильна Character Code Table | Встановіть `Metadata.CharacterCodeTableNumber = "006"` для Arabic |
| **Таймінги зсунуті на 99с** | TCF (Start-of-Programme) не враховано | Використовуйте `IgnoreTimecodeStartOfProgramme: true` або ручний `subs.Add(-offset)` |
| **Колір не зберігається при конвертації** | WebVTT клас не мапиться на STL | Додайте кастомний мапінг у `propagateWebVTTAttributes()` |
| **GSI блок порожній після TTML→STL** | Metadata не заповнена перед `WriteToSTL()` | Встановіть хоча б `Framerate` та `STLDisplayStandardCode` |
| **Субтитри обрізаються на 40 символів** | Перевищено `MaxCharactersPerRow` | Увімкніть перенос рядків або зменшіть текст перед експортом |
| **Неправильний framerate у GSI** | "STL25.01" замість "STL30.01" | Встановіть `Metadata.Framerate = 30` перед записом |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування GSI блоку для каналу:
```go
// GSI блок однаковий для всіх субтитрів одного каналу
type ChannelSTLConfig struct {
    gsiBlock [1024]byte  // попередньо сформований
    hash     string      // для валідації змін
}

var gsiCache = sync.Map{}  // channelID → ChannelSTLConfig

func (p *STLExporter) getGSIBlock(channelID string) *[1024]byte {
    if cached, ok := gsiCache.Load(channelID); ok {
        cfg := cached.(ChannelSTLConfig)
        return &cfg.gsiBlock
    }
    
    // Формуємо новий GSI з конфігурації каналу
    cfg := p.getChannelConfig(channelID)
    gsi := buildGSIBlock(cfg)  // ваша функція
    
    // Зберігаємо в кеш
    gsiCache.Store(channelID, ChannelSTLConfig{
        gsiBlock: gsi,
        hash:     computeHash(cfg),
    })
    
    return &gsi
}
```

### 2. Пакетна конвертація кольорів:
```go
// Замість індивідуального застосування для кожного LineItem:
func batchApplySTLColors(items []*astisub.Item, langColors map[string]*astisub.Color) {
    for _, item := range items {
        lang := detectLanguage(item)  // ваша логіка
        if color, ok := langColors[lang]; ok {
            for _, line := range item.Lines {
                for i := range line.Items {
                    if line.Items[i].InlineStyle == nil {
                        line.Items[i].InlineStyle = &astisub.StyleAttributes{}
                    }
                    line.Items[i].InlineStyle.STLColor = color
                }
            }
        }
    }
}
```

### 3. Lazy TCI/TCO кодування:
```go
// Не кодуйте таймкоди до моменту запису в буфер
type LazySTLItem struct {
    start, end time.Duration
    encoded    [8]byte  // TCI+TCO
    dirty      bool
}

func (l *LazySTLItem) Encode(framerate int) [8]byte {
    if !l.dirty {
        l.encoded = encodeTimecodes(l.start, l.end, framerate)
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
    Language:               "ara", // ISO 639-2 code
    STLDisplayStandardCode: "1",   // 0=Open, 1=Teletext L1, 2=L2
    STLMaximumNumberOfDisplayableCharactersInAnyTextRow: astikit.IntPtr(40),
    STLMaximumNumberOfDisplayableRows:                   astikit.IntPtr(23),
    STLCountryOfOrigin:     "SAU",
    // ... інші поля за потребою
}

// ✅ 2. Застосування STL-кольорів
for _, item := range subs.Items {
    lang := detectLanguage(item)
    if color, ok := LanguageSTLColors[lang]; ok {
        applySTLColor(item, color)
    }
}

// ✅ 3. Корекція таймінгів під сегмент
segmentOffset := segmentPTS.Sub(segmentStartTime)
subs.Add(-segmentOffset)  // зсув до відносного часу

// ✅ 4. Експорт у STL (без проміжного файлу)
var buf bytes.Buffer
if err := subs.WriteToSTL(&buf); err != nil {
    return fmt.Errorf("stl export: %w", err)
}
stlData := buf.Bytes()

// ✅ 5. Валідація GSI блоку (опціонально, для дебагу)
if len(stlData) >= 1024 {
    diskFormat := string(stlData[3:11])
    displayStd := string(stlData[11:12])
    log.Debug("STL GSI", "diskFormat", diskFormat, "displayStd", displayStd)
}

// ✅ 6. Метрики
monitoring.STLExported.Inc()
monitoring.STLExportLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [EBU Tech 3264-E](https://tech.ebu.ch/docs/tech/tech3264.pdf) — офіційна специфікація STL
- 📄 [EBU R 37](https://tech.ebu.ch/docs/r/r037.pdf) — Character Code Tables (Latin/Cyrillic/Arabic)
- 💻 [astisub stl.go](https://github.com/asticode/go-astisub/blob/master/stl.go) — вихідний код парсингу/запису
- 🧪 [astisub testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади STL файлів

---

> 💡 **Ключова рекомендація**: Оскільки ви працюєте з **реальним часом** та **мультиязычними субтитрами**:
> 1. **Кешуйте GSI блок** на рівні каналу — він не змінюється між сегментами.
> 2. **Використовуйте `IgnoreTimecodeStartOfProgramme: true`** при парсингу вхідних STL, щоб уникнути зсувів таймінгів.
> 3. **Обмежуйте довжину тексту** до 40 символів на рядок перед експортом — STL не підтримує автоматичний перенос.
> 4. **Тестуйте round-trip** (WebVTT → STL → WebVTT) для вашого каналу, щоб переконатися, що кольори та позиціонування зберігаються.
> 5. **Додайте валідацію Character Code Table** — неправильна таблиця призведе до "??????" замість арабського тексту.

Потрібен приклад функції `buildGSIBlock()` для авто-генерації GSI з вашої `ChannelConfig`? Готовий допомогти! 🚀