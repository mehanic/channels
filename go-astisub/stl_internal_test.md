# ⏱️🔤 STL Low-Level: Duration, Character Handler та Styler в astisub

Цей тестовий файл розкриває **три критичні низькорівневі механізми** бібліотеки `astisub` для роботи з форматом STL (EBU Teletext): кодування часу, декодування символів та обробка стилів. Розберемо детально для інтеграції у ваш **CCTV HLS Processor**.

---

## 🗺️ Огляд тестованих компонентів

```
┌────────────────────────────────────────┐
│ 🔑 Три ключові підсистеми STL          │
├────────────────────────────────────────┤
│                                         │
│ ⏱️  parseDurationSTL / formatDurationSTL│
│    • 4-байтне бінарне кодування        │
│    • Формат: [HH][MM][SS][FF]          │
│    • Підтримка framerate (25/30 fps)   │
│                                         │
│ 🔤 stlCharacterHandler                  │
│    • Stateful декодер (аккумулятор)    │
│    • Діакритика: 0xC8 + base → "ä"     │
│    • Таблиці: Latin / Cyrillic / Arabic│
│                                         │
│ 🎨 stlStyler                            │
│    • Inline-стилі: italics/underline/boxing │
│    • Контрольні коди: 0x80-0x85         │
│    • propagateSTLAttributes() інтеграція│
│                                         │
└────────────────────────────────────────┘
```

---

## ⏱️ 1. STL Duration — 4-байтне кодування часу

### Формат таймкоду в STL:

```
Байт    Поле        Діапазон    Опис
─────────────────────────────────────
0       Hours       0x00-0x23   0-23 години
1       Minutes     0x00-0x59   0-59 хвилин
2       Seconds     0x00-0x59   0-59 секунд
3       Frames      0x00-0x18/0x1D 0-24 (25fps) або 0-29 (30fps)
```

### Тест `TestSTLDuration`:

```go
// Парсинг строкового представлення "12345678" (12:34:56:78)
d, _ := parseDurationSTL("12345678", 100)  // 100 = 25fps (100 frames/сек)
// Результат: 12h + 34m + 56s + 780ms
// Пояснення: 78 frames при 25fps = 78/25 = 3.12s → 780ms

// Форматування назад у строку
s := formatDurationSTL(d, 100)  // → "12345678"

// Бінарне кодування (для запису в TTI блок)
b := formatDurationSTLBytes(d, 100)
// Результат: []byte{0x0c, 0x22, 0x38, 0x4e}
// 0x0c = 12 (години), 0x22 = 34 (хвилини), 0x38 = 56 (секунди), 0x4e = 78 (кадри)

// Зворотне парсинг бінарних даних
d2 := parseDurationSTLBytes([]byte{0x0c, 0x22, 0x38, 0x4e}, 100)
assert.Equal(t, d, d2)  // round-trip збігається
```

### 🔢 Формула конвертації кадрів → мілісекунди:

```go
// framerateUnits = frames per second × 4 (стандарт STL)
// Для 25fps: 25 × 4 = 100
// Для 30fps: 30 × 4 = 120

func framesToMilliseconds(frames byte, framerateUnits int) int {
    // frames у байті — це значення 0-24/29
    // framerateUnits — це 100 або 120
    return int(frames) * 1000 / (framerateUnits / 4)
}

// Приклад: 78 frames при 25fps (100 units)
// 78 × 1000 / 100 = 780ms ✓
```

### ✅ Ваш use-case: синхронізація з PTS у HLS

```go
// PTS у HLS — у одиницях 90kHz (1/90000 секунди)
// STL таймкоди — у кадрах (напр. 25fps = 40ms/frame)

type STLTimecodeSync struct {
    framerate      int  // 25 або 30
    framerateUnits int  // 100 або 120
    segmentBasePTS uint64 // PTS початку сегменту
}

// Конвертація STL duration → PTS для відправки у WebSocket
func (s *STLTimecodeSync) durationToPTS(d time.Duration) uint64 {
    // 1. Конвертуємо duration у наносекунди
    ns := int64(d)
    
    // 2. Конвертуємо у 90kHz одиниці (стандарт MPEG-TS PTS)
    pts := uint64(ns * 90 / 1e6)
    
    // 3. Додаємо базовий PTS сегменту
    return s.segmentBasePTS + pts
}

// Зворотна конвертація: PTS → STL duration
func (s *STLTimecodeSync) ptsToDuration(pts uint64) time.Duration {
    // 1. Віднімаємо базовий PTS
    relative := pts - s.segmentBasePTS
    
    // 2. Конвертуємо з 90kHz у наносекунди
    ns := int64(relative) * 1e6 / 90
    
    return time.Duration(ns)
}

// Форматування для запису в TTI блок
func (s *STLTimecodeSync) durationToSTLBytes(d time.Duration) []byte {
    hh := byte(d / time.Hour)
    mm := byte((d % time.Hour) / time.Minute)
    ss := byte((d % time.Minute) / time.Second)
    
    // Кадри: залишок у мілісекундах → кадри
    ms := (d % time.Second).Milliseconds()
    frames := byte(ms * int64(s.framerateUnits/4) / 1000)
    
    return []byte{hh, mm, ss, frames}
}
```

> ⚠️ **Критичний нюанс**: Параметр `framerateUnits` у функціях astisub — це **frames × 4**, а не просто fps. Завжди передавайте `100` для 25fps або `120` для 30fps.

---

## 🔤 2. stlCharacterHandler — Stateful декодування символів

### Проблема, яку вирішує:
У STL **діакритичні знаки** (umlaut, accent) кодуються як **два байти**:
1. **Control code** (напр. `0xC8` для umlaut)
2. **Base character** (напр. `0x61` для 'a')

Результат: `0xC8 + 0x61 → "ä"`

### Архітектура декодера:

```go
type stlCharacterHandler struct {
    table     stlCharacterCodeTable  // Latin/Cyrillic/Arabic
    accumulator *byte  // "буфер" для control code (nil якщо немає)
}
```

### Логіка декодування (з тестів):

```go
// Латинська таблиця: stlCharacterCodeTableNumberLatin

h, _ := newSTLCharacterHandler(stlCharacterCodeTableNumberLatin)

// Звичайний символ: повертається одразу
o := h.decode(0x65)  // 'e'
assert.Equal(t, []byte("e"), o)

// Control code без наступного символу: повертає nil (чекає продовження)
o = h.decode(0xC8)  // umlaut prefix
assert.Equal(t, []byte(nil), o)  // нічого не повертаємо ще

// Наступний символ: комбінуємо з accumulator
o = h.decode(0x61)  // 'a'
assert.Equal(t, []byte("ä"), o)  // 0xC8 + 0x61 = "ä"

// Після комбінації accumulator скидається
o = h.decode(0x65)  // 'e' знову працює нормально
assert.Equal(t, []byte("e"), o)
```

### 📋 Таблиця діакритики для Latin (0xC8 — umlaut):

```
Control + Base → Результат
─────────────────────────
0xC8 + 0x61 ('a') → "ä"
0xC8 + 0x41 ('A') → "Ä"
0xC8 + 0x6f ('o') → "ö"
0xC8 + 0x4f ('O') → "Ö"
0xC8 + 0x75 ('u') → "ü"
0xC8 + 0x55 ('U') → "Ü"
0xC8 + 0x65 ('e') → "ë"
0xC8 + 0x45 ('E') → "Ë"
0xC8 + 0x69 ('i') → "ï"
0xC8 + 0x49 ('I') → "Ï"
```

### ✅ Ваш use-case: підтримка арабської/кириличної таблиць

```go
// Для арабської мови використовується інша таблиця:
h, _ := newSTLCharacterHandler(stlCharacterCodeTableNumberArabic)

// Арабські символи кодуються інакше — без діакритики,
// але з власною мапою 8-бітних кодів → UTF-8

// Приклад інтеграції у ваш парсер:
type ChannelCharacterDecoder struct {
    channelID string
    handler   *stlCharacterHandler
    lastSeq   uint64  // для відстеження розривів
}

func (d *ChannelCharacterDecoder) DecodeByte(b byte) []byte {
    result := d.handler.decode(b)
    
    // Якщо повернуто nil — це control code, чекаємо наступний байт
    // У real-time потоці це може означати, що дані прийшли частинами
    if result == nil {
        // Зберігаємо стан для наступного виклику
        // (в astisub це робиться внутрішньо через accumulator)
        return nil
    }
    
    return result
}

// Обробка розриву в потоці (напр. між сегментами HLS)
func (d *ChannelCharacterDecoder) OnSegmentBoundary() {
    // Скидаємо accumulator, щоб уникнути "залипання" control code
    // між сегментами
    d.handler = newSTLCharacterHandler(d.getTableForChannel())
}
```

### 🌍 Таблиці кодування в astisub:

```go
const (
    stlCharacterCodeTableNumberLatin     = "003"  // Latin + діакритика
    stlCharacterCodeTableNumberCyrillic  = "004"  // Кирилиця
    stlCharacterCodeTableNumberArabic    = "006"  // Арабська
    stlCharacterCodeTableNumberGreek     = "007"  // Грецька
    stlCharacterCodeTableNumberHebrew    = "008"  // Іврит
)

// Вибір таблиці з конфігурації каналу:
func (p *SubtitleProcessor) getTableForChannel(channelID string) string {
    cfg := p.getChannelConfig(channelID)
    switch cfg.LanguageCode {
    case "ara", "ar":
        return stlCharacterCodeTableNumberArabic
    case "rus", "ru", "ukr", "uk":
        return stlCharacterCodeTableNumberCyrillic
    case "ell", "el", "grc":
        return stlCharacterCodeTableNumberGreek
    case "heb", "he":
        return stlCharacterCodeTableNumberHebrew
    default:
        return stlCharacterCodeTableNumberLatin
    }
}
```

> ⚠️ **Критично**: Неправильна таблиця призведе до **"????"** замість тексту. Завжди встановлюйте `Metadata.CharacterCodeTableNumber` у GSI блоці відповідно до мови.

---

## 🎨 3. stlStyler — Inline-стилі в STL

### Контрольні коди стилів (0x80-0x8F):

```
Код     Дія                     Вплив на StyleAttributes
─────────────────────────────────────────────────────────
0x80    Start Italics           STLItalics = true
0x81    Stop Italics            STLItalics = false
0x82    Start Underline         STLUnderline = true
0x83    Stop Underline          STLUnderline = false
0x84    Start Boxing            STLBoxing = true  (рамка навколо тексту)
0x85    Stop Boxing             STLBoxing = false
0x86-0x8F  Reserved / Future use
```

### Архітектура stlStyler:

```go
type stlStyler struct {
    italics   *bool  // nil = не змінювати
    underline *bool
    boxing    *bool
}
```

### Тест `TestSTLStyler` — ключові сценарії:

```go
// 1. Parse spacing attributes
s := newSTLStyler()
s.parseSpacingAttribute(0x80)  // Start Italics
assert.Equal(t, stlStyler{italics: astikit.BoolPtr(true)}, *s)

s.parseSpacingAttribute(0x81)  // Stop Italics
assert.Equal(t, stlStyler{italics: astikit.BoolPtr(false)}, *s)

// 2. Has been set — чи були зміни?
s = newSTLStyler()
assert.False(t, s.hasBeenSet())  // нічого не змінено

s.boxing = astikit.BoolPtr(true)
assert.True(t, s.hasBeenSet())  // boxing змінено

// 3. Has changed — чи відрізняється від існуючих атрибутів?
sa := &StyleAttributes{}
assert.False(t, s.hasChanged(sa))  // sa порожня, boxing=nil

s.boxing = astikit.BoolPtr(true)
assert.True(t, s.hasChanged(sa))   // sa.STLBoxing=nil, s.boxing=true

sa.STLBoxing = s.boxing
assert.False(t, s.hasChanged(sa))  // тепер збігається

// 4. Update — застосування змін до StyleAttributes
s = newSTLStyler()
sa = &StyleAttributes{}
s.update(sa)  // нічого не змінює, бо s порожній

s.boxing = astikit.BoolPtr(true)
s.update(sa)
assert.Equal(t, StyleAttributes{STLBoxing: s.boxing}, *sa)  // boxing застосовано
```

### ✅ Ваш use-case: інтеграція з propagateSTLAttributes()

```go
// stlStyler використовується всередині parseTeletextRow()
// для обробки inline-стилів у потоці символів

func parseTeletextRow(i *Item, d decoder, fs func() styler, row []byte) {
    var s styler  // це stlStyler для STL-потоків
    
    for _, v := range row {
        // Перевіряємо чи це control code для стилю
        if fs != nil {
            s = fs()  // створюємо новий styler для цього рядка
        }
        
        switch v {
        case 0x80: s.parseSpacingAttribute(0x80)  // italic on
        case 0x81: s.parseSpacingAttribute(0x81)  // italic off
        case 0x82: s.parseSpacingAttribute(0x82)  // underline on
        // ... інші коди
        }
        
        // Якщо стиль змінився — розбиваємо LineItem
        if s != nil && s.hasChanged(li.InlineStyle) {
            appendTeletextLineItem(&l, li, s)  // закриваємо попередній
            li = LineItem{InlineStyle: &StyleAttributes{}}  // новий
            s.update(li.InlineStyle)  // застосовуємо поточний стиль
        }
        
        // Додаємо текст (якщо не control code)
        if v >= 0x20 {
            li.Text += string(d.decode(v))
        }
    }
}

// Після парсингу — пропагація для веб-форматів
func (sa *StyleAttributes) propagateSTLAttributes() {
    // Boxing → не має прямого аналога в WebVTT, ігноруємо
    // Italics/Underline → WebVTT теги <i>, <u>
    
    if sa.STLItalics != nil && *sa.STLItalics {
        sa.WebVTTTags = append(sa.WebVTTTags, WebVTTTag{Name: "i"})
    }
    if sa.STLUnderline != nil && *sa.STLUnderline {
        sa.WebVTTTags = append(sa.WebVTTTags, WebVTTTag{Name: "u"})
    }
    
    // Для TTML — мапінг у textAlign тощо
    // (див. повну реалізацію в styles.go)
}
```

### 🎯 Практичний приклад: кольорове кодування + стилі

```go
// Комбінація кольору та стилю для мультиязычних субтитрів
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
            
            // Курсив для акценту (напр. імена, цитати)
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

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"????" замість арабського тексту** | Неправильна Character Code Table | Встановіть `Metadata.CharacterCodeTableNumber = "006"` для Arabic |
| **Діакритика "з'їдає" наступний символ** | accumulator не скинуто між сегментами | Викликайте `OnSegmentBoundary()` або створюйте новий handler для кожного сегменту |
| **Стилі "просочуються" між субтитрами** | stlStyler не скидається після рядка | Переконайтеся, що `fs()` створює новий styler для кожного виклику `parseTeletextRow` |
| **Кадри неправильно конвертуються в мс** | Неправильний `framerateUnits` параметр | Завжди передавайте `100` для 25fps, `120` для 30fps (не просто fps!) |
| **Boxing не відображається у WebVTT** | Немає прямого аналога в веб-форматах | Ігноруйте або конвертуйте у CSS-клас `.boxed { border: 1px solid white; }` на клієнті |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування character handler на рівні каналу:
```go
type ChannelDecoderCache struct {
    channelID string
    handler   *stlCharacterHandler
    tableHash string  // для детекту змін конфігурації
}

var decoderCache = sync.Map{}  // channelID → ChannelDecoderCache

func (p *SubtitleProcessor) getDecoder(channelID string) *stlCharacterHandler {
    if cached, ok := decoderCache.Load(channelID); ok {
        c := cached.(ChannelDecoderCache)
        // Перевіряємо чи не змінилася конфігурація
        if c.tableHash == p.getTableHash(channelID) {
            return c.handler
        }
    }
    
    // Створюємо новий decoder
    table := p.getTableForChannel(channelID)
    handler, _ := newSTLCharacterHandler(table)
    
    decoderCache.Store(channelID, ChannelDecoderCache{
        channelID: channelID,
        handler:   handler,
        tableHash: p.getTableHash(channelID),
    })
    
    return handler
}
```

### 2. Пакетна конвертація таймкодів:
```go
// Замість індивідуального formatDurationSTL для кожного Item:
func batchFormatSTLDurations(items []*astisub.Item, framerateUnits int) [][4]byte {
    result := make([][4]byte, len(items))
    for i, item := range items {
        result[i] = formatDurationSTLBytes(item.StartAt, framerateUnits)
        // Для TTI потрібно і TCI, і TCO:
        // result[i] = [8]byte{TCI..., TCO...}
    }
    return result
}
```

### 3. Lazy style propagation:
```go
// Не викликайте propagateSTLAttributes() до моменту експорту
type LazySTLItem struct {
    item      *astisub.Item
    propagated bool
    webvttReady *astisub.Item  // кеш для веб-формату
}

func (l *LazySTLItem) GetWebVTTReady() *astisub.Item {
    if !l.propagated {
        // Глибоке копіювання + пропагація тільки при потребі
        l.webvttReady = cloneWithPropagation(l.item)
        l.propagated = true
    }
    return l.webvttReady
}
```

---

## 📋 Чек-лист інтеграції

```go
// ✅ 1. Налаштування framerate для duration конвертації
const (
    Framerate25Units = 100  // 25 fps × 4
    Framerate30Units = 120  // 30 fps × 4
)

// ✅ 2. Ініціалізація character handler з правильною таблицею
table := getTableForChannel(channelID)  // "003", "004", "006" тощо
handler, err := newSTLCharacterHandler(table)
if err != nil {
    log.Error("failed to create character handler", "err", err)
}

// ✅ 3. Обробка control codes у потоці символів
for _, b := range teletextRow {
    decoded := handler.decode(b)
    if decoded != nil {
        textBuilder.Write(decoded)
    }
    // якщо decoded == nil — це control code, accumulator оновлено внутрішньо
}

// ✅ 4. Скидання accumulator між сегментами
func onNewSegment() {
    handler = newSTLCharacterHandler(table)  // скидаємо стан
}

// ✅ 5. Конвертація таймінгів у STL-формат для архіву
stlBytes := formatDurationSTLBytes(item.StartAt, Framerate25Units)
// Запис у TTI блок: байти 4-7 = TCI, 8-11 = TCO

// ✅ 6. Проганяння стилів перед веб-експортом
for _, item := range subs.Items {
    for _, line := range item.Lines {
        for i := range line.Items {
            if line.Items[i].InlineStyle != nil {
                line.Items[i].InlineStyle.propagateSTLAttributes()
            }
        }
    }
}

// ✅ 7. Метрики
monitoring.STLCharactersDecoded.Add(float64(decodedBytes))
monitoring.STLStyleChanges.Inc()  // якщо s.hasChanged() == true
```

---

## 🔗 Корисні посилання

- 📄 [EBU Tech 3264-E](https://tech.ebu.ch/docs/tech/tech3264.pdf) — специфікація STL, розділ 5.3 (Time Code), 6.2 (Character Tables)
- 📄 [EBU R 37](https://tech.ebu.ch/docs/r/r037.pdf) — Character Code Tables (деталі діакритики)
- 💻 [astisub stl.go](https://github.com/asticode/go-astisub/blob/master/stl.go) — реалізація `parseDurationSTL`, `stlCharacterHandler`
- 💻 [astisub styles.go](https://github.com/asticode/go-astisub/blob/master/styles.go) — `propagateSTLAttributes()`, `stlStyler`

---

> 💡 **Ключова рекомендація**: Оскільки ви працюєте з **арабською мовою** в реальному часі:
> 1. **Завжди використовуйте таблицю "006"** для Arabic — інакше отримаєте "????".
> 2. **Скидайте character handler між сегментами** — control code з кінця одного сегменту не повинен "злипатися" з початком наступного.
> 3. **Передавайте `framerateUnits = 100`** (не 25!) у функції duration — це найпоширеніша помилка.
> 4. **Тестуйте round-trip** (STL → astisub → STL) для вашого каналу, щоб переконатися, що діакритика та стилі зберігаються.
> 5. **Додайте логування** для `decoded == nil` випадків — це допоможе відлагодити проблеми з control codes у потоці.

Потрібен приклад повного циклу: **Teletext TS → astisub parse → WebSocket SubtitleMessage** з урахуванням арабської таблиці та синхронізації часу? Готовий допомогти! 🚀