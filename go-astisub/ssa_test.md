# 🎬 Глибокий розбір: SSA/ASS формат в astisub

Цей тестовий файл демонструє **роботу з форматом SSA/ASS** (SubStation Alpha) — потужним форматом субтитрів з підтримкою складної анімації, позиціонування та стилів. Розберемо архітектуру, ключові особливості та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Огляд тестованих сценаріїв

```
┌────────────────────────────────────────┐
│ 🎯 Ключові можливості SSA/ASS в astisub│
├────────────────────────────────────────┤
│                                         │
│ 📦 Формат файлу:                        │
│ • [Script Info] — метадані             │
│ • [V4+ Styles] — глобальні стилі       │
│ • [Events] — діалоги з таймінгами      │
│                                         │
│ 🎨 Система стилів (20+ атрибутів):      │
│ • Alignment, Colors (AABBGGRR)         │
│ • Font, Margins, Borders, Shadows      │
│ • Alpha, Encoding, Layer, Effects      │
│                                         │
│ ⚡ Inline-ефекти:                        │
│ • {\pos(x,y)} — позиціонування         │
│ • {\b1}, {\i1} — жирний/курсив         │
│ • {\c&H...&} — колір тексту            │
│ • Розбиття тексту на кілька LineItem   │
│                                         │
│ 🔄 Round-trip тестування:               │
│ • Parse SSA → astisub → Write SSA      │
│ • Порівняння байт-в-байт з еталоном    │
│ • Підтримка v4.00 та v4.00+            │
│                                         │
└────────────────────────────────────────┘
```

---

## 📦 1. SSA Format Structure — Структура файлу

### Основні секції:

```ini
[Script Info]
; Metadata
Title: SSA test
ScriptType: v4.00
PlayResX: 384
PlayResY: 288
Timer: 100.0
Collisions: Normal

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:01:39.00,0:01:41.04,Default,Cher,1234,2345,3456,test,(deep rumbling)
```

### ✅ Ваш use-case: парсинг SSA з пам'яті (без файлів)

```go
// ProcessSSASegment — обробка SSA-сегменту в реальному часі
func (p *SubtitleProcessor) ProcessSSASegment(data []byte, seqNum uint64, pts time.Duration) error {
    // 1. Парсинг з bytes.Reader
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
    
    // 3. Експорт тексту для перекладу (NLLB)
    arabicText := p.extractText(subs)
    
    // 4. Асинхронний переклад + TTS
    go p.translateAndSend(seqNum, arabicText, subs.Items)
    
    return nil
}
```

---

## 🎨 2. SSA Style System — Система стилів

### StyleAttributes для SSA (ключові поля):

```go
type StyleAttributes struct {
    // Вирівнювання (1-9 за нумпадом)
    SSAAlignment *int  // 7=top-left, 8=top-center, 9=top-right, etc.
    
    // Кольори у форматі 0xAABBGGRR (little-endian)
    SSAPrimaryColour   *Color  // основний колір тексту
    SSASecondaryColour *Color  // колір виділення (karaoke)
    SSAOutlineColour   *Color  // колір обводки
    SSABackColour      *Color  // колір фону
    
    // Прозорість (0.0-1.0)
    SSAAlphaLevel *float64
    
    // Шрифт
    SSAFontName string
    SSAFontSize *float64
    SSAEncoding *int  // code page
    
    // Стилі тексту
    SSABold   *bool
    SSAItalic *bool  // (не показана в тесті, але підтримується)
    
    // Межі та тіні
    SSABorderStyle *int      // 1=outline+drop shadow, 3=opaque box
    SSAOutline     *float64  // товщина обводки (пікселі)
    SSAShadow      *float64  // глибина тіні (пікселі)
    
    // Відступи
    SSAMarginLeft    *int
    SSAMarginRight   *int
    SSAMarginVertical *int
    
    // Додатково
    SSALayer  *int    // Z-order для перекриття
    SSAMarked *bool   // позначка для редагування
    SSAEffect string  // inline-ефекти ({\tag} syntax)
}
```

### 🎨 Колір у SSA: формат 0xAABBGGRR

```go
// Тест: парсинг кольору з рядка
c, _ := newColorFromSSAString("12345678", 16)
// Результат: Color{Alpha:0x12, Blue:0x34, Green:0x56, Red:0x78}

// Пояснення:
// "12345678" у hex = 0x12345678
// Розбиття: [Alpha=0x12][Blue=0x34][Green=0x56][Red=0x78]
// Це little-endian BGR+A формат (відмінний від HTML #RRGGBB!)

// Експорт назад у SSA-рядок:
c := &Color{Alpha: 0x80, Red: 0xFF, Green: 0x80, Blue: 0x00}
c.SSAString()  // → "800080ff" (AABBGGRR)
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

## ⚡ 3. Inline Effects — Динамічні теги в тексті

### Синтаксис {\tag}:

```
Формат: {\команда[параметри]}
Приклади:
  {\pos(400,570)}     — позиція (пікселі від лівого верхнього кута)
  {\b1} / {\b0}       — увімкнути/вимкнути жирний
  {\i1} / {\i0}       — увімкнути/вимкнути курсив
  {\c&H00FF00&}       — колір тексту (BGR формат!)
  {\fade(255,0,255,0,100,200,300)} — анімація прозорості
  {\move(x1,y1,x2,y2,t1,t2)} — анімація переміщення
```

### Тест `TestInBetweenSSAEffect`:

```go
// Вхідний текст: "First item{\pos(400,570)}Second item"
// Результат парсингу: 2 окремих LineItem

assert.Len(t, s.Items[0].Lines[0].Items, 2)

// Перший фрагмент: звичайний текст
assert.Equal(t, astisub.LineItem{Text: "First item"}, 
             s.Items[0].Lines[0].Items[0])

// Другий фрагмент: текст + inline-стиль
assert.Equal(t, astisub.LineItem{
    InlineStyle: &astisub.StyleAttributes{
        SSAEffect: "{\\pos(400,570)}",  // збережено як строку!
    },
    Text: "Second item",
}, s.Items[0].Lines[0].Items[1])
```

### 🔍 Як це працює:

```
Текст: "First item{\pos(400,570)}Second item"
                    ↓
            Знайдено {\pos...}
                    ↓
            Розбиття на 2 частини:
            ├─ "First item" → LineItem{Text: "First item"}
            └─ "Second item" → LineItem{
                   Text: "Second item",
                   InlineStyle: {SSAEffect: "{\\pos(400,570)}"}
               }
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

> ⚠️ **Увага**: `SSAEffect` зберігається як **сирий рядок** (`"{\\pos(400,570)}"`). Astisub не парсить теги автоматично — якщо потрібно програмно змінювати позицію, доведеться парсити цей рядок самостійно або використовувати окремі поля (якщо додати їх у `StyleAttributes`).

---

## 🔄 4. Round-Trip Testing — Гарантія сумісності

### Тест `TestSSA`:

```go
// 1. Відкриття вхідного файлу
s, err := astisub.OpenFile("./testdata/example-in.ssa")
assert.NoError(t, err)

// 2. Перевірка метаданих
assert.Equal(t, &astisub.Metadata{
    Comments:          []string{"Comment 1", "Comment 2"},
    SSACollisions:     "Normal",
    SSAOriginalScript: "asticode",
    SSAPlayResY:       astikit.IntPtr(600),
    SSAScriptType:     "v4.00",
    SSATimer:          astikit.Float64Ptr(100),
    Title:             "SSA test",
}, s.Metadata)

// 3. Перевірка стилів (3 стилі з різними атрибутами)
assert.Equal(t, 3, len(s.Styles))
assertSSAStyle(t, expectedStyle1, *s.Styles["1"])  // порівняння всіх полів

// 4. Перевірка Items: посилання на стилі + inline-атрибути
assert.Equal(t, s.Styles["1"], s.Items[0].Style)  // глобальний стиль
assertSSAStyleAttributes(t, expectedInline, *s.Items[0].InlineStyle)  // локальні перевизначення

// 5. Запис у буфер та порівняння з еталоном
var w bytes.Buffer
err = s.WriteToSSA(&w)
assert.NoError(t, err)

expected, _ := ioutil.ReadFile("./testdata/example-out.ssa")
assert.Equal(t, string(expected), w.String())  // байт-в-байт збіг!
```

### Чому це важливо для вашого проекту:

1. **Архівування**: Якщо ви зберігаєте субтитри у форматі SSA для довгострокового архіву, round-trip гарантує, що після перезапису не втратяться стилі.

2. **Конвертація форматів**: Можна парсити SSA → конвертувати у WebVTT для HLS → зберегти оригінал SSA для архіву.

3. **Валідація вхідних даних**: Якщо клієнт надсилає SSA-субтитри, round-trip тестування допомагає виявити некоректні теги.

---

## 🔄 5. Script Type: v4.00 vs v4.00+

### Тест `TestSSAv4plus`:

```go
// 1. Парсимо файл (зазвичай v4.00)
s, _ := astisub.OpenFile("./testdata/example-in.ssa")

// 2. Змінюємо тип скрипту на новіший
s.Metadata.SSAScriptType = "v4.00+"

// 3. Записуємо — вихідний файл відрізняється форматом деяких полів
err := s.WriteToSSA(&w)
assert.NoError(t, err)

// 4. Порівнюємо з еталоном для v4.00+
expected, _ := ioutil.ReadFile("./testdata/example-out-v4plus.ssa")
assert.Equal(t, string(expected), w.String())
```

### Ключові відмінності:

| Атрибут | v4.00 | v4.00+ (ASS) |
|---------|-------|--------------|
| **Колір** | `&HAABBGGRR&` | `&HAABBGGRR&` (той самий) |
| **Alignment** | 1-9 (нумпад) | 1-11 (додаткові опції) |
| **Effects** | Базові теги | Розширені: `\fad`, `\move`, `\t` анімації |
| **Encoding** | Code page number | UTF-8 підтримка краща |
| **Margins** | Пікселі | Може бути у % від екрану |

### ✅ Ваш use-case: вибір формату для HLS

```go
// Для веб-відтворення (HLS + WebVTT) краще використовувати простіші стилі:
func (p *SubtitleExporter) chooseOutputFormat(inputFormat string) string {
    switch inputFormat {
    case "ssa", "ass":
        // ASS має складні анімації, які не підтримуються у WebVTT
        // Конвертуємо у спрощений формат
        return "webvtt"
    case "srt", "vtt":
        return "webvtt"  // нативна підтримка
    case "stl", "teletext":
        return "webvtt"  // після конвертації атрибутів
    default:
        return "webvtt"
    }
}

// При конвертації: зберігаємо тільки підмножину стилів
func (p *SubtitleExporter) simplifySSAForWebVTT(item *astisub.Item) {
    for _, line := range item.Lines {
        for i := range line.Items {
            style := line.Items[i].InlineStyle
            if style != nil {
                // Зберігаємо тільки те, що підтримує WebVTT:
                // - колір (через <c.class> теги)
                // - жирний/курсив/підкреслення (через <b>, <i>, <u>)
                // - позиціонування (через line:/position: у cue header)
                
                // Ігноруємо: анімації, тіні, обводки, складне вирівнювання
                style.SSAEffect = ""  // видаляємо {\tag}
                style.SSAShadow = nil
                style.SSAOutline = nil
                // ... інші unsupported поля
            }
        }
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Колір відображається неправильно** | Плутанина між AABBGGRR та #RRGGBB | Використовуйте `Color.SSAString()` для експорту, `newColorFromSSAString()` для імпорту |
| **Inline-теги не розбивають текст** | `{\pos}` сприймається як звичайний текст | Переконайтеся, що теги у форматі `{\tag}` без пробілів після `{` |
| **Анімації не працюють у WebVTT** | `\fade`, `\move` ігноруються при конвертації | Видаляйте або замінюйте на статичні стилі перед експортом у WebVTT |
| **Шрифт не відображається** | `SSAFontName` не підтримується у веб-форматах | Ігноруйте або конвертуйте у CSS-клас на клієнті |
| **Margin не враховується** | `SSAMargin*` у пікселях, а WebVTT у % | Конвертуйте: `marginPercent = marginPx * 100 / PlayResX` |
| **Encoding проблеми з арабською** | "????" замість тексту при кодуванні | Встановіть `SSAEncoding = 1` (UTF-8) або використовуйте Unicode символи напряму |

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
// Замість індивідуального SSAString() для кожного Color:
func batchEncodeSSAColors(colors []*astisub.Color) []string {
    result := make([]string, len(colors))
    for i, c := range colors {
        if c != nil {
            result[i] = c.SSAString()  // "AABBGGRR"
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

func cloneAndConvertToWebVTT(src *astisub.Item) *astisub.Item {
    dst := *src
    dst.Lines = make([]astisub.Line, len(src.Lines))
    
    for li, line := range src.Lines {
        dst.Lines[li] = line  // shallow copy
        for ii := range line.Items {
            if style := line.Items[ii].InlineStyle; style != nil {
                // Конвертуємо тільки підтримувані поля
                newStyle := &astisub.StyleAttributes{}
                if style.SSAPrimaryColour != nil {
                    // Колір → WebVTT <c.class>
                    newStyle.WebVTTTags = append(newStyle.WebVTTTags, 
                        astisub.WebVTTTag{Name: "c", Classes: []string{colorToClass(style.SSAPrimaryColour)}})
                }
                // ... інші конвертації
                line.Items[ii].InlineStyle = newStyle
            }
        }
    }
    return &dst
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
    simplifySSAForWebVTT(item)
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

- 📄 [ASS Format Spec](https://github.com/libass/libass/wiki/ASS-Script-Format) — неофіційна, але повна документація
- 📄 [Aegisub Manual](https://docs.aegisub.org/3.2/ASS_Tags/) — довідка по тегах {\pos}, {\fade}, {\move} тощо
- 💻 [astisub ssa.go](https://github.com/asticode/go-astisub/blob/master/ssa.go) — вихідний код парсингу/запису
- 🎬 [libass](https://github.com/libass/libass) — бібліотека для рендерингу ASS субтитрів (C)
- 🧪 [astisub testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади SSA файлів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **реальним часом** та **HLS-стрімінгом**:
> 1. **Уникайте складних ASS-анімацій** (`\fade`, `\move`, `\t`) — вони не підтримуються у WebVTT і можуть "зламатися" при конвертації.
> 2. **Використовуйте `SSAPrimaryColour` для кольорового кодування мов** — це надійніше, ніж inline-теги `{\c&H...&}`.
> 3. **Кешуйте парсинг inline-тегів** — якщо один і той самий `{\pos(400,570)}` зустрічається багато разів, не парсьте його щоразу.
> 4. **Для арабської мови переконайтеся, що `SSAEncoding` сумісний з UTF-8** — інакше отримаєте "????".
> 5. **Тестуйте round-trip** (SSA → astisub → SSA) для вашого каналу, щоб переконатися, що стилі зберігаються коректно.

Потрібен приклад функції `parseSSAEffect()` для витягування позиції/кольору з рядка `"{\pos(400,570)\c&H00FF00&}"`? Готовий допомогти! 🚀