# 🎬 Глибокий розбір: SSA/ASS реалізація в astisub

Цей файл — **повна реалізація підтримки формату SSA/ASS** (SubStation Alpha) у бібліотеці `astisub`. SSA/ASS — це потужний формат субтитрів з підтримкою складної анімації, позиціонування, стилів та ефектів, що використовується в аніме-спільноті та професійному відео.

---

## 🗺️ Архітектурна схема SSA в astisub

```
┌────────────────────────────────────────┐
│ 📦 SSA File Structure                   │
├────────────────────────────────────────┤
│                                         │
│  [Script Info] — метадані              │
│  ├─ Title, ScriptType, PlayResX/Y      │
│  ├─ Timer, Collisions, WrapStyle       │
│  └─ Comments (; коментарі)             │
│                                         │
│  [V4+ Styles] — глобальні стилі        │
│  ├─ Format: Name, Fontname, ...        │
│  └─ Style: Default,Arial,20,&H...      │
│                                         │
│  [Events] — діалоги з таймінгами       │
│  ├─ Format: Layer, Start, End, ...     │
│  └─ Dialogue: 0,0:01:39.00,...,Text    │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 Ключові константи та мапінги

### 1. Вирівнювання (Alignment) — нумпад-система:

```go
const (
    ssaAlignmentLeft                  = 1  // 📍 ліворуч-знизу
    ssaAlignmentCentered              = 2  // 📍 по центру-знизу  
    ssaAlignmentRight                 = 3  // 📍 праворуч-знизу
    ssaAlignmentTopTitle              = 4  // 📍 ліворуч-зверху
    ssaAlignmentLeftJustifiedTopTitle = 5  // 📍 по центру-зверху
    ssaAlignmentMidTitle              = 8  // 📍 по центру-середина
    // ... інші значення до 11 у v4.00+
)
```

> 💡 **Порада**: Значення 1-9 відповідають клавішам нумпада на клавіатурі (7=верх-ліво, 5=центр, 3=низ-право).

### 2. BorderStyle — тип обрамлення:

```go
const (
    ssaBorderStyleOutlineAndDropShadow = 1  // обводка + тінь (стандарт)
    ssaBorderStyleOpaqueBox            = 3  // непрозорий прямокутник-фон
)
```

### 3. Event Categories — типи подій:

```go
const (
    ssaEventCategoryDialogue = "Dialogue"  // звичайний діалог (обробляється)
    ssaEventCategoryComment  = "Comment"   // коментар (ігнорується)
    ssaEventCategoryCommand  = "Command"   // команда для плеєра
    // ... Sound, Picture, Movie
)
```

> ⚠️ **Важливо**: `ReadFromSSA` обробляє **тільки** `Dialogue` події. Інші типи пропускаються.

---

## 📥 ReadFromSSA — парсинг SSA файлу

### Потік обробки:

```
1. Створення сканера рядків (newScanner)
   ↓
2. Цикл по рядках:
   ├─ Визначення секції: [Script Info] / [V4+ Styles] / [Events]
   ├─ Пропуск коментарів (; ...) та пустих рядків
   ├─ Парсинг "Format:" → мапа індексів → назв полів
   ├─ Парсинг даних за форматом:
   │  ├─ Script Info → ssaScriptInfo.parse()
   │  ├─ Styles → newSSAStyleFromString()
   │  └─ Events → newSSAEventFromString()
   ↓
3. Конвертація внутрішніх структур:
   ├─ ssaStyle.style() → astisub.Style
   ├─ ssaEvent.item() → astisub.Item (тільки Dialogue)
   ↓
4. Повернення *Subtitles
```

### 🔍 Деталі парсингу подій (Events):

```go
// newSSAEventFromString — парсинг рядка Dialogue
func newSSAEventFromString(header, content string, format map[int]string) (*ssaEvent, error) {
    // 1. Розбиття за комами
    items := strings.Split(content, ",")
    
    // 2. Останнє поле (Text) може містити коми → з'єднуємо назад
    items[len(format)-1] = strings.Join(items[len(format)-1:], ",")
    items = items[:len(format)]
    
    // 3. Парсинг за форматом
    for idx, item := range items {
        attr := format[idx]
        switch attr {
        case "Start", "End":
            e.start/end = parseDurationSSA(item)  // "0:01:39.00" → time.Duration
        case "Layer", "MarginL":
            value, _ := strconv.Atoi(item)
            e.layer = astikit.IntPtr(value)
        case "Marked":
            e.marked = astikit.BoolPtr(item == "Marked=1")
        case "Style":
            // "*Default" → "Default" (сумісність з ffmpeg)
            e.style = strings.TrimPrefix(item, "*")
        case "Text":
            e.text = strings.TrimSpace(item)  // текст з {\tag} ефектами
        }
    }
    return e, nil
}
```

### ✅ Ваш use-case: парсинг SSA з пам'яті (streaming)

```go
// ProcessSSASegment — обробка SSA-сегменту в реальному часі
func (p *SubtitleProcessor) ProcessSSASegment(data []byte, seqNum uint64, pts time.Duration) error {
    // 1. Парсинг з bytes.Reader (без тимчасових файлів)
    reader := bytes.NewReader(data)
    subs, err := astisub.ReadFromSSA(reader)
    if err != nil {
        return fmt.Errorf("ssa parse: %w", err)
    }
    
    // 2. Корекція часу відносно початку стріму
    streamOffset := pts.Sub(p.streamStartTime)
    for _, item := range subs.Items {
        item.StartAt += streamOffset
        item.EndAt += streamOffset
    }
    
    // 3. Експорт тексту для перекладу (видаляємо {\tag} ефекти)
    arabicText := p.extractTextWithoutEffects(subs)
    
    // 4. Асинхронний переклад + TTS
    go p.translateAndSend(seqNum, arabicText, subs.Items)
    
    return nil
}

// extractTextWithoutEffects — витягує чистий текст, ігноруючи {\tag}
func (p *SubtitleProcessor) extractTextWithoutEffects(subs *astisub.Subtitles) string {
    var text strings.Builder
    for _, item := range subs.Items {
        for _, line := range item.Lines {
            for _, li := range line.Items {
                // Видаляємо {\pos...} {\c...} тощо з тексту
                clean := ssaRegexpEffect.ReplaceAllString(li.Text, "")
                text.WriteString(clean)
                text.WriteString(" ")
            }
        }
        text.WriteString("\n")
    }
    return strings.TrimSpace(text.String())
}
```

---

## 🎨 ssaStyle — система стилів

### Структура стилю (20+ атрибутів):

```go
type ssaStyle struct {
    // Ідентифікація
    name     string  // "Default", "Title", "Subtitle"
    
    // Шрифт
    fontName string      // "Arial", "Noto Sans Arabic"
    fontSize *float64    // розмір у пікселях
    encoding *int        // code page (1=Default, 2=Symbol, 178=Arabic)
    
    // Кольори (формат &HAABBGGRR)
    primaryColour   *Color  // основний колір тексту
    secondaryColour *Color  // колір для karaoke-виділення
    outlineColour   *Color  // колір обводки
    backColour      *Color  // колір фону (для BorderStyle=3)
    
    // Стилі тексту
    bold      *bool
    italic    *bool
    underline *bool
    strikeout *bool
    
    // Геометрія
    alignment      *int      // 1-9 (нумпад)
    marginLeft     *int      // відступ зліва (пікселі)
    marginRight    *int      // відступ справа
    marginVertical *int      // відступ зверху/знизу
    
    // Ефекти
    borderStyle *int        // 1=outline+shadow, 3=opaque box
    outline     *float64    // товщина обводки
    shadow      *float64    // глибина тіні
    alphaLevel  *float64    // прозорість (0.0-1.0)
    angle       *float64    // кут повороту (градуси)
    scaleX      *float64    // масштабування по X (%)
    scaleY      *float64    // масштабування по Y (%)
    spacing     *float64    // міжсимвольний інтервал
}
```

### 🎨 Колір у SSA: формат `&HAABBGGRR`

```go
// Парсинг кольору з рядка
func newColorFromSSAColor(i string) (*Color, error) {
    // Перевірка формату
    s := i
    base := 10
    if strings.HasPrefix(i, "&H") {
        s = i[2:]      // видаляємо "&H" префікс
        base = 16      // шістнадцяткова система
    }
    return newColorFromSSAString(s, base)  // AABBGGRR → Color
}

// Приклад:
// "&H800080FF" → Color{Alpha:0x80, Blue:0x00, Green:0x80, Red:0xFF}
// (напівпрозорий червоний)

// Експорт назад:
func newSSAColorFromColor(c *Color) string {
    return "&H" + c.SSAString()  // "&H800080FF"
}
```

### ✅ Ваш use-case: кольорове кодування мов

```go
// Оскільки SSA підтримує повний RGBA, можемо використовувати точні кольори
var LanguageSSAColors = map[string]*astisub.Color{
    "ar": {Alpha: 0x00, Red: 0xFF, Green: 0xD7, Blue: 0x00}, // золотий, непрозорий
    "en": {Alpha: 0x00, Red: 0x41, Green: 0x69, Blue: 0xE1}, // блакитний
    "ru": {Alpha: 0x00, Red: 0xDC, Green: 0x14, Blue: 0x3C}, // червоний
}

// Застосування кольору до субтитру
func (p *SubtitleProcessor) applySSAColor(item *astisub.Item, lang string) {
    if color, ok := LanguageSSAColors[lang]; ok {
        for lineIdx := range item.Lines {
            for itemIdx := range item.Lines[lineIdx].Items {
                li := &item.Lines[lineIdx].Items[itemIdx]
                if li.InlineStyle == nil {
                    li.InlineStyle = &astisub.StyleAttributes{}
                }
                // SSAPrimaryColour — основний колір тексту
                li.InlineStyle.SSAPrimaryColour = color
            }
        }
    }
}
```

---

## ⚡ Inline Effects — динамічні теги в тексті

### Регулярний вираз для пошуку тегів:

```go
var ssaRegexpEffect = regexp.MustCompile(`\{[^\{]+\}`)
// Пояснення: \{ — літера {, [^\{]+ — будь-які символи крім {, \} — літера }
// Приклади збігів: {\pos(400,570)}, {\c&H00FF00&}, {\b1}, {\fade(255,0,255,0,100,200,300)}
```

### Парсинг тексту з ефектами (в `ssaEvent.item()`):

```go
// Вхід: "First item{\pos(400,570)}Second item"
// Результат: 2 окремих LineItem

text := "First item{\pos(400,570)}Second item"
matches := ssaRegexpEffect.FindAllStringIndex(text, -1)
// matches = [[10,25]] — позиції {\pos(400,570)}

// Розбиття:
// 1. "First item" (0-10) → LineItem{Text: "First item"}
// 2. "{\pos(400,570)}" (10-25) → LineItem{InlineStyle: {SSAEffect: "{\pos(400,570)}"}}
// 3. "Second item" (25-конець) → додається до попереднього як Text
```

### 🎯 Популярні SSA-теги:

```
Позиціонування:
  {\pos(x,y)}        — абсолютна позиція (пікселі від лівого верхнього кута)
  {\anN}             — вирівнювання (N=1-9, нумпад-система)

Стилі тексту:
  {\b1}/{\b0}        — увімкнути/вимкнути жирний
  {\i1}/{\i0}        — увімкнути/вимкнути курсив
  {\u1}/{\u0}        — увімкнути/вимкнути підкреслення
  {\s1}/{\s0}        — увімкнути/вимкнути закреслення

Кольори:
  {\c&HBBGGRR&}      — колір тексту (BGR формат!)
  {\1a&HAA&}         — прозорість основного кольору
  {\3c&HBBGGRR&}     — колір обводки

Анімації (тільки ASS, v4.00+):
  {\fade(a1,a2,a3,t1,t2,t3,t4)} — плавна зміна прозорості
  {\move(x1,y1,x2,y2,t1,t2)}   — анімація переміщення
  {\t([t1,t2,][accel,]\tags)}  — трансформація інших тегів у часі

Інше:
  {\n} або {\N}      — новий рядок (\N = hard break, \n = soft wrap)
  {\kN}              — karaoke: тривалість виділення у санти-секундах
```

### ✅ Ваш use-case: позиціонування перекладів

```go
// Стратегія: арабська — знизу, переклади — вище з позиціонуванням
func (p *SubtitleProcessor) positionByLanguage(item *astisub.Item, lang string, screenHeight int) {
    // Розрахунок вертикальної позиції (% від висоти екрану)
    yPos := map[string]int{
        "ar": 85,  // 85% — майже внизу
        "en": 70,  // 70% — середина-низ
        "ru": 55,  // 55% — середина
    }[lang]
    
    pixelY := screenHeight * yPos / 100
    
    for lineIdx := range item.Lines {
        for itemIdx := range item.Lines[lineIdx].Items {
            li := &item.Lines[lineIdx].Items[itemIdx]
            if li.InlineStyle == nil {
                li.InlineStyle = &astisub.StyleAttributes{}
            }
            // Додаємо позиціонування як SSA-ефект
            li.InlineStyle.SSAEffect = fmt.Sprintf("{\\pos(0,%d)}", pixelY)
        }
    }
}
```

> ⚠️ **Увага**: `SSAEffect` зберігається як **сирий рядок** (`"{\\pos(400,570)}"`). Astisub не парсить теги автоматично — якщо потрібно програмно змінювати позицію, доведеться парсити цей рядок самостійно.

---

## ⏱️ Duration Handling — робота з часом

### Формат часу в SSA:
```
"0:01:39.00" → 0 годин, 1 хвилина, 39 секунд, 0 центисекунд
Формат: H:MM:SS.cc (cc = centiseconds, 1/100 секунди)
```

### Функції парсингу/форматування:

```go
// Парсинг: "0:01:39.00" → 99 * time.Second
func parseDurationSSA(i string) (time.Duration, error) {
    return parseDuration(i, ".", 3)  // делегує до загальної функції
}

// Форматування: 99с → "0:01:39.00"
func formatDurationSSA(d time.Duration) string {
    return formatDuration(d, ".", 2)  // 2 цифри для центисекунд
}
```

### ✅ Ваш use-case: синхронізація з PTS у HLS

```go
// PTS у HLS — 90kHz clock (1/90000 секунди)
// SSA час — центисекунди (1/100 секунди)

type SSATimeSync struct {
    segmentBasePTS uint64
}

// SSA duration → PTS для WebSocket
func (s *SSATimeSync) DurationToPTS(d time.Duration) uint64 {
    ns := int64(d)
    pts := uint64(ns * 90 / 1e6)  // наносекунди → 90kHz
    return s.segmentBasePTS + pts
}

// PTS → SSA duration для запису
func (s *SSATimeSync) PTSToDuration(pts uint64) time.Duration {
    relative := pts - s.segmentBasePTS
    ns := int64(relative) * 1e6 / 90
    return time.Duration(ns)
}
```

---

## ✍️ WriteToSSA — експорт у SSA формат

### Потік генерації:

```
1. Запис [Script Info] блоку
   ├─ si.bytes() → форматування метаданих
   └─ Коментарі з префіксом ";"
   ↓
2. Запис [V4+ Styles] блоку (якщо є стилі)
   ├─ Визначення формату: тільки непорожні поля
   ├─ Сортування стилів за назвою
   └─ Запис "Style: ..." рядків
   ↓
3. Запис [Events] блоку
   ├─ Фіксований формат з 9+ полів (сумісність з VLC)
   ├─ Конвертація Item → ssaEvent → рядок
   └─ Запис "Dialogue: ..." рядків
   ↓
4. Повернення nil (успіх)
```

### 🎯 Ключові моменти експорту:

```go
// 1. Динамічний формат для стилів: тільки заповнені поля
func (s ssaStyle) updateFormat(formatMap map[string]bool, format []string) []string {
    if s.alignment != nil {
        format = ssaUpdateFormat(ssaStyleFormatNameAlignment, formatMap, format)
    }
    // ... перевірка всіх 20+ полів
    return format
}

// 2. Фіксований формат для подій (сумісність з плеєрами)
var format = []string{
    "Marked", "Start", "End", "Style", "Name", 
    "MarginL", "MarginR", "MarginV", "Effect", "Text",
}
// Для v4.00+: "Marked" → "Layer"

// 3. Обробка тексту з ефектами
func (e *ssaEvent) string(format []string) string {
    // Текст записується "як є" з {\tag} всередині
    // Плеєр сам розпарсить теги при відтворенні
    return strings.Join(ss, ",")
}
```

### ✅ Ваш use-case: експорт SSA для архіву

```go
// ExportSSAForArchive — підготовка SSA файлу для довгострокового зберігання
func (p *ArchiveProcessor) ExportSSAForArchive(subs *astisub.Subtitles, channelID string) ([]byte, error) {
    // 1. Налаштування метаданих з конфігурації каналу
    cfg := p.getChannelConfig(channelID)
    if subs.Metadata == nil {
        subs.Metadata = &astisub.Metadata{}
    }
    
    subs.Metadata.SSAScriptType = "v4.00+"  // сучасний формат
    subs.Metadata.SSAPlayResX = astikit.IntPtr(1920)  // Full HD
    subs.Metadata.SSAPlayResY = astikit.IntPtr(1080)
    subs.Metadata.Title = cfg.ProgramTitle
    
    // 2. Додавання стилів для мов (якщо немає)
    if len(subs.Styles) == 0 {
        subs.Styles["Arabic"] = &astisub.Style{
            ID: "Arabic",
            InlineStyle: &astisub.StyleAttributes{
                SSAFontName:      "Noto Sans Arabic",
                SSAFontSize:      astikit.Float64Ptr(48),
                SSAPrimaryColour: LanguageSSAColors["ar"],
                SSAAlignment:     astikit.IntPtr(2),  // centered-bottom
            },
        }
    }
    
    // 3. Експорт у bytes.Buffer (без файлу)
    var buf bytes.Buffer
    if err := subs.WriteToSSA(&buf); err != nil {
        return nil, fmt.Errorf("ssa write: %w", err)
    }
    
    return buf.Bytes(), nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Колір відображається неправильно** | Плутанина між `&HAABBGGRR` та `#RRGGBB` | Використовуйте `newColorFromSSAColor()` для імпорту, `newSSAColorFromColor()` для експорту |
| **Inline-теги не розбивають текст** | `{\pos}` сприймається як звичайний текст | Переконайтеся, що теги у форматі `{\tag}` без пробілів після `{` |
| **Анімації не працюють у WebVTT** | `\fade`, `\move` ігноруються при конвертації | Видаляйте або замінюйте на статичні стилі перед експортом у WebVTT |
| **Шрифт не відображається** | `SSAFontName` не підтримується у веб-форматах | Ігноруйте або конвертуйте у CSS-клас на клієнті |
| **Margin не враховується** | `SSAMargin*` у пікселях, а плеєр очікує % | Конвертуйте: `marginPercent = marginPx * 100 / PlayResX` |
| **Encoding проблеми з арабською** | "????" замість тексту | Встановіть `SSAEncoding = 178` (Arabic code page) або використовуйте Unicode |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування парсингу inline-тегів:

```go
// Inline-теги рідко змінюються в межах одного каналу
type CachedSSAEffect struct {
    raw      string
    parsed   map[string]string  // tag → value
    position *Point             // якщо є {\pos(x,y)}
}

var effectCache = sync.Map{}  // effectString → CachedSSAEffect

func parseSSAEffectCached(raw string) *CachedSSAEffect {
    if cached, ok := effectCache.Load(raw); ok {
        return cached.(*CachedSSAEffect)
    }
    
    parsed := parseSSAEffect(raw)  // ваша функція парсингу
    cached := &CachedSSAEffect{raw: raw, parsed: parsed}
    
    // Витягуємо позицію якщо є
    if pos, ok := parsed["pos"]; ok {
        cached.position = parsePosition(pos)  // "400,570" → Point{400, 570}
    }
    
    effectCache.Store(raw, cached)
    return cached
}
```

### 2. Пакетна конвертація кольорів:

```go
// Замість індивідуального newSSAColorFromColor для кожного Color:
func batchEncodeSSAColors(colors []*astisub.Color) []string {
    result := make([]string, len(colors))
    for i, c := range colors {
        if c != nil {
            result[i] = newSSAColorFromColor(c)  // "&HAABBGGRR"
        }
    }
    return result
}
```

### 3. Lazy style propagation для SSA → WebVTT:

```go
// Не конвертуйте стилі до моменту експорту
type LazySSAItem struct {
    item        *astisub.Item
    webvttReady *astisub.Item
    dirty       bool
}

func (l *LazySSAItem) GetWebVTTReady() *astisub.Item {
    if l.dirty || l.webvttReady == nil {
        l.webvttReady = cloneAndConvertToWebVTT(l.item)
        l.dirty = false
    }
    return l.webvttReady
}
```

---

## 📋 Чек-лист інтеграції SSA

```go
// ✅ 1. Парсинг без проміжних файлів
reader := bytes.NewReader(ssaData)
subs, err := astisub.ReadFromSSA(reader)

// ✅ 2. Корекція таймінгів під сегмент
streamOffset := pts.Sub(streamStartTime)
for _, item := range subs.Items {
    item.StartAt += streamOffset
    item.EndAt += streamOffset
}

// ✅ 3. Застосування кольорів/позицій для мов
for _, item := range subs.Items {
    lang := detectLanguage(item)
    applySSAColor(item, lang)
    positionByLanguage(item, lang, screenHeight)
}

// ✅ 4. Спрощення для WebVTT експорту (якщо потрібно)
if targetFormat == "webvtt" {
    simplifySSAForWebVTT(item)  // видаляє {\tag}, анімації тощо
}

// ✅ 5. Експорт у цільовий формат
var buf bytes.Buffer
switch targetFormat {
case "ssa", "ass":
    subs.WriteToSSA(&buf)
case "webvtt":
    subs.WriteToWebVTT(&buf)
}

// ✅ 6. Валідація арабського тексту
if err := validateArabicText(extractText(subs)); err != nil {
    log.Warn("arabic text validation", "err", err)
}

// ✅ 7. Метрики
monitoring.SSAParsed.Inc()
monitoring.SSAParseLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [ASS Format Spec (неофіційний)](https://github.com/libass/libass/wiki/ASS-Script-Format) — найповніша документація
- 📄 [Aegisub Manual — ASS Tags](https://docs.aegisub.org/3.2/ASS_Tags/) — довідка по тегах {\pos}, {\fade}, {\move}
- 💻 [astisub ssa.go](https://github.com/asticode/go-astisub/blob/master/ssa.go) — вихідний код
- 🎬 [libass](https://github.com/libass/libass) — бібліотека для рендерингу ASS субтитрів (C)
- 🧪 [astisub testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади файлів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **реальним часом** та **арабською мовою**:
> 1. **Уникайте складних ASS-анімацій** (`\fade`, `\move`, `\t`) — вони не підтримуються у WebVTT і можуть "зламатися" при конвертації.
> 2. **Використовуйте `SSAPrimaryColour` для кольорового кодування мов** — це надійніше, ніж inline-теги `{\c&H...&}`.
> 3. **Встановіть `SSAEncoding = 178` (Arabic)** для коректного відображення арабських символів у старих плеєрах.
> 4. **Кешуйте парсинг inline-тегів** — якщо один і той самий `{\pos(400,570)}` зустрічається багато разів, не парсьте його щоразу.
> 5. **Тестуйте round-trip** (SSA → astisub → SSA) для вашого каналу, щоб переконатися, що стилі зберігаються коректно.

Потрібен приклад функції `parseSSAEffect()` для витягування позиції/кольору з рядка `"{\pos(400,570)\c&H00FF00&}"`? Готовий допомогти! 🚀