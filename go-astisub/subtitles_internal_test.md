# 🎨 Глибокий розбір: Color, Duration та STL Attributes в astisub

Цей тестовий файл демонструє **три критичні підсистеми** бібліотеки `astisub`: обробка кольорів, парсинг часу та конвертація атрибутів між форматами. Розберемо, як це використати у вашому **CCTV HLS Processor** для мультиязычних субтитрів.

---

## 📊 Огляд функціоналу

```
┌────────────────────────────────────────┐
│ 🔑 Три ключові можливості               │
├────────────────────────────────────────┤
│ 🎨 Color:                               │
│    • SSA: 0xAABBGGRR (little-endian)   │
│    • Decimal: "305419896" (base 10)    │
│    • HTML: "#RRGGBB" (без alpha)       │
│                                         │
│ ⏱️  Duration:                            │
│    • Гнучкий сепаратор: `,` або `.`    │
│    • Змінна точність: 2-3 цифри для мс  │
│    • Формати: `HH:MM:SS,mmm` / `H:MM:SS.mmm` │
│                                         │
│ 📐 STL Attributes → WebVTT/TTML:        │
│    • Justification: left/center/right  │
│    • Position: VerticalPosition → %    │
│    • Конвертація: телебачення → веб    │
└────────────────────────────────────────┘
```

---

## 🎨 1. Color — робота з кольорами

### Формати в astisub:

```go
type Color struct {
    Alpha uint8  // Прозорість (0x00-0xFF)
    Red   uint8
    Green uint8
    Blue  uint8
}
```

### Парсинг з SSA (Advanced SubStation Alpha):

```go
// SSA використовує 0xAABBGGRR порядок (little-endian BGR + Alpha)
c, _ := newColorFromSSAString("12345678", 16)
// Результат: Color{Alpha:0x12, Blue:0x34, Green:0x56, Red:0x78}

// Або десяткове представлення:
c, _ := newColorFromSSAString("305419896", 10)  // 0x12345678 = 305419896
```

### Експорт у веб-формати:

```go
c.HTMLString()   // "#785634" — тільки RGB, без alpha (CSS стандарт)
c.SSAString()    // "12345678" — для зворотньої сумісності
```

### ✅ Ваш use-case: кольорове кодування мов

```go
// У вашому SubtitleMessage pipeline:
var LanguageColors = map[string]astisub.Color{
    "ar": {Alpha: 0xFF, Red: 0xFF, Green: 0xD7, Blue: 0x00}, // золотий
    "en": {Alpha: 0xFF, Red: 0x41, Green: 0x69, Blue: 0xE1}, // блакитний
    "ru": {Alpha: 0xFF, Red: 0xDC, Green: 0x14, Blue: 0x3C}, // червоний
}

func (p *SubtitleProcessor) applyLanguageColor(item *astisub.Item, lang string) {
    if color, ok := LanguageColors[lang]; ok {
        for lineIdx := range item.Lines {
            for itemIdx := range item.Lines[lineIdx].Items {
                // Створюємо InlineStyle якщо немає
                if item.Lines[lineIdx].Items[itemIdx].InlineStyle == nil {
                    item.Lines[lineIdx].Items[itemIdx].InlineStyle = &astisub.StyleAttributes{}
                }
                // Застосовуємо колір (для форматів, що підтримують)
                item.Lines[lineIdx].Items[itemIdx].InlineStyle.TeletextColor = &color
            }
        }
    }
}
```

> ⚠️ **Увага**: `HTMLString()` ігнорує alpha-канал. Для напівпрозорих субтитрів у WebVTT використовуйте `WebVTTLine` позиціонування + CSS-класи на клієнті.

---

## ⏱️ 2. Duration — парсинг та форматування часу

### Гнучкий парсинг:

```go
// parseDuration(text, separator, millisecondPrecision)
parseDuration("12:34:56,123", ",", 3)  // → 12h34m56.123s
parseDuration("1:23:45.67", ".", 2)    // → 1h23m45.067s (.67 → 670ms при precision=2)
parseDuration("1:23:45:67", ":", 2)    // → 1h23m45.067s (двокрапка як десятковий сепаратор!)
```

### Форматування для різних форматів:

```go
// formatDuration(d, separator, millisecondPrecision)
formatDuration(12*time.Hour+34*time.Minute+56*time.Second+123*time.Millisecond, ",", 3)
// → "12:34:56,123" (SRT стандарт)

formatDuration(time.Second+234*time.Millisecond, ".", 3)
// → "00:00:01.234" (WebVTT стандарт)
```

### ✅ Ваш use-case: синхронізація таймінгів HLS

```go
// У VideoManifestProxy для корекції розривів:
func (p *VideoManifestProxy) normalizeSubtitleTiming(item *astisub.Item, 
                                                     segmentStartTime time.Time,
                                                     ptsOffset time.Duration) {
    // Корекція відносно початку сегменту
    item.StartAt += ptsOffset
    item.EndAt += ptsOffset
    
    // Якщо субтитр "випадає" за межі сегменту — обрізаємо
    segmentDuration := 10 * time.Second  // або з конфігурації
    if item.StartAt < 0 {
        item.StartAt = 0
    }
    if item.EndAt > segmentDuration {
        item.EndAt = segmentDuration
    }
}

// Експорт у WebVTT для HLS-плейлиста:
func (p *HLSGenerator) subtitleToWebVTT(item *astisub.Item) string {
    start := formatDuration(item.StartAt, ".", 3)  // WebVTT вимагає крапку
    end := formatDuration(item.EndAt, ".", 3)
    
    var text strings.Builder
    for _, line := range item.Lines {
        text.WriteString(line.String())
        text.WriteString("\n")
    }
    
    return fmt.Sprintf("%s --> %s\n%s\n", start, end, strings.TrimSpace(text.String()))
}
```

### 🐞 Edge cases з тестів:

```go
// ❌ Недостатньо цифр для мілісекунд:
parseDuration("12:34:56,1234", ",", 3)  // error: 4 цифри замість 3

// ✅ Автоматичне доповнення нулями:
parseDuration("12:34:56,1", ",", 3)  // → 12:34:56.100 (1 → 100ms)

// ✅ Різні сепаратори:
parseDuration("1:23:45.67", ".", 2)   // крапка
parseDuration("1:23:45:67", ":", 2)   // двокрапка як десятковий роздільник!
```

> 💡 **Порада**: Зберігайте `millisecondPrecision=3` для внутрішньої обробки, конвертуйте у `2` тільки при експорті у формати, що цього вимагають.

---

## 📐 3. STL Attributes Propagation — конвертація між форматами

### Проблема:
Телебачення (STL/Teletext) та веб (WebVTT/TTML) використовують **різні системи координат**:

| Атрибут | STL/Teletext | WebVTT | TTML |
|---------|-------------|--------|------|
| **Вирівнювання** | `JustificationLeft/Centered/Right` | `WebVTTAlign: "left"/"center"/"right"` | `TTMLTextAlign: *string` |
| **Позиція по вертикалі** | `VerticalPosition: 0-99` (in-vision) або `0-22` (teletext) | `WebVTTLine: "50%"` | — |
| **MaxRows** | `23` для teletext, `99` для in-vision | — | — |

### Логіка конвертації з тестів:

```go
// 1. Justification → WebVTTAlign + TTMLTextAlign
sa := &StyleAttributes{STLJustification: &JustificationRight}
sa.propagateSTLAttributes()
// Результат: WebVTTAlign="right", TTMLTextAlign="right"

// JustificationCentered НЕ встановлює WebVTTAlign (центрування за замовчуванням)

// 2. VerticalPosition → WebVTTLine (%)
// Формула для teletext (MaxRows=23):
//   WebVTTLine = "(VerticalPosition-1)*100/MaxRows" при VerticalPosition>0
//   WebVTTLine = "0%" при VerticalPosition==0

sa := &StyleAttributes{
    STLPosition: &STLPosition{VerticalPosition: 22, MaxRows: 23},
}
sa.propagateSTLAttributes()
// Результат: WebVTTLine = "(22-1)*100/23" = "91%"

// Для in-vision (MaxRows=99):
sa := &STLPosition{VerticalPosition: 50, MaxRows: 99}
// Результат: WebVTTLine = "50%" (пряме відображення)
```

### ✅ Ваш use-case: позиціонування мультиязычних субтитрів

```go
// Стратегія: арабська — знизу, переклади — вище
var LanguagePositions = map[string]*astisub.STLPosition{
    "ar": {VerticalPosition: 20, MaxRows: 23},  // ~87% — майже внизу
    "en": {VerticalPosition: 15, MaxRows: 23},  // ~61% — середина-низ
    "ru": {VerticalPosition: 10, MaxRows: 23},  // ~39% — середина
}

func (p *SubtitleProcessor) positionByLanguage(item *astisub.Item, lang string) {
    if pos, ok := LanguagePositions[lang]; ok {
        for lineIdx := range item.Lines {
            if item.Lines[lineIdx].Items[0].InlineStyle == nil {
                item.Lines[lineIdx].Items[0].InlineStyle = &astisub.StyleAttributes{}
            }
            // Копіюємо позицію
            itemPos := *pos
            item.Lines[lineIdx].Items[0].InlineStyle.STLPosition = &itemPos
            // Проганяємо конвертацію для веб-форматів
            item.Lines[lineIdx].Items[0].InlineStyle.propagateSTLAttributes()
        }
    }
}
```

### Результат у WebVTT:

```vtt
00:01:23.456 --> 00:01:26.789 line:87% align:right
مرحبا بكم في النشرة الإخبارية

00:01:23.456 --> 00:01:26.789 line:61% align:right
Welcome to the news bulletin

00:01:23.456 --> 00:01:26.789 line:39% align:right
Добро пожаловать в новостной бюллетень
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// subtitle_formatter.go
type SubtitleFormatter struct {
    channelID         string
    defaultPrecision  int  // 3 для внутрішньої обробки
    targetFormat      string // "webvtt", "srt", "ttml"
    languageLayout    LanguageLayout // конфігурація позицій/кольорів
}

type LanguageLayout struct {
    Position map[string]*astisub.STLPosition
    Color    map[string]astisub.Color
    Align    map[string]astisub.Justification
}

func (f *SubtitleFormatter) FormatItem(item *astisub.Item, lang string) error {
    // 1. Застосовуємо позицію
    if pos, ok := f.languageLayout.Position[lang]; ok {
        f.applyPosition(item, pos)
    }
    
    // 2. Застосовуємо колір (для форматів, що підтримують)
    if color, ok := f.languageLayout.Color[lang]; ok {
        f.applyColor(item, &color)
    }
    
    // 3. Застосовуємо вирівнювання
    if align, ok := f.languageLayout.Align[lang]; ok {
        f.applyJustification(item, &align)
    }
    
    // 4. Проганяємо пропагацію атрибутів для цільового формату
    for _, line := range item.Lines {
        for _, li := range line.Items {
            if li.InlineStyle != nil {
                li.InlineStyle.propagateSTLAttributes()
                // Додаткові пропагації для інших форматів:
                // li.InlineStyle.propagateTeletextAttributes()
                // li.InlineStyle.propagateSSAAttributes()
            }
        }
    }
    
    return nil
}

func (f *SubtitleFormatter) ExportToWebVTT(item *astisub.Item) (string, error) {
    // Форматуємо час з precision=3 та сепаратором "."
    start := formatDuration(item.StartAt, ".", 3)
    end := formatDuration(item.EndAt, ".", 3)
    
    var cues strings.Builder
    for _, line := range item.Lines {
        // Збираємо текст з усіх LineItem у рядку
        var lineText strings.Builder
        for _, li := range line.Items {
            // Додаємо inline-стилі як WebVTT-теги якщо потрібно
            if li.InlineStyle != nil && li.InlineStyle.TeletextColor != nil {
                // Конвертуємо колір у CSS (спрощено)
                color := li.InlineStyle.TeletextColor.HTMLString()
                lineText.WriteString(fmt.Sprintf("<c.%s>", color[1:])) // "#RRGGBB" → "c.RRGGBB"
            }
            lineText.WriteString(li.Text)
            if li.InlineStyle != nil && li.InlineStyle.TeletextColor != nil {
                lineText.WriteString("</c>")
            }
        }
        cues.WriteString(lineText.String())
        cues.WriteString("\n")
    }
    
    // Формуємо WebVTT cue
    cue := fmt.Sprintf("%s --> %s", start, end)
    
    // Додаємо позицію/вирівнювання якщо є
    if len(item.Lines) > 0 && len(item.Lines[0].Items) > 0 {
        if style := item.Lines[0].Items[0].InlineStyle; style != nil {
            if style.WebVTTLine != "" {
                cue += fmt.Sprintf(" line:%s", style.WebVTTLine)
            }
            if style.WebVTTAlign != "" {
                cue += fmt.Sprintf(" align:%s", style.WebVTTAlign)
            }
        }
    }
    
    return fmt.Sprintf("%s\n%s\n", cue, strings.TrimSpace(cues.String())), nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Тест, що демонструє | Рішення для вашого проекту |
|----------|---------------------|---------------------------|
| **Некоректний колір у WebVTT** | `TestColor` | Використовуйте `HTMLString()` тільки для CSS-класів, не для inline-кольорів |
| **Час "з'їжджає" при експорті** | `TestFormatDuration` | Зберігайте `precision=3` внутрішньо, конвертуйте при експорті |
| **Позиція 0% замість очікуваної** | `TestPropagateSTLAttributes/STLPositionTeletextRow0` | Обробляйте `VerticalPosition=0` як окремий випадок (без `-1`) |
| **JustificationCentered ігнорується** | `TestPropagateSTLAttributes/JustificationCentered` | Це очікувана поведінка — центрування є default у WebVTT |
| **Alpha-канал втрачається** | `TestColor/HTMLString` | Для напівпрозорості використовуйте окремі CSS-класи на клієнті |

---

## 📋 Чек-лист інтеграції

```go
// ✅ 1. Налаштування precision для вашого формату
const (
    InternalPrecision = 3  // завжди 3 для внутрішньої обробки
    WebVTTPrecision   = 3  // WebVTT підтримує 3 цифри
    SRTPrecision      = 3  // SRT стандарт
)

// ✅ 2. Конфігурація мовного лейауту
layout := LanguageLayout{
    Position: map[string]*astisub.STLPosition{
        "ar": {VerticalPosition: 20, MaxRows: 23},
        "en": {VerticalPosition: 15, MaxRows: 23},
        "ru": {VerticalPosition: 10, MaxRows: 23},
    },
    Color: map[string]astisub.Color{
        "ar": {Alpha: 0xFF, Red: 0xFF, Green: 0xD7, Blue: 0x00},
        // ...
    },
    Align: map[string]astisub.Justification{
        "ar": JustificationRight,  // арабська — справа наліво
        "en": JustificationLeft,
        "ru": JustificationLeft,
    },
}

// ✅ 3. Форматування перед відправкою
formatter := NewSubtitleFormatter(channelID, "webvtt", layout)
for _, item := range subtitles.Items {
    for _, lang := range []string{"ar", "en", "ru"} {
        if err := formatter.FormatItem(item, lang); err != nil {
            log.Warn("format failed", "lang", lang, "err", err)
        }
    }
}

// ✅ 4. Експорт у цільовий формат
webvtt, _ := formatter.ExportToWebVTT(item)
// → відправка у WebSocket або запис у .vtt файл для HLS

// ✅ 5. Метрики
monitoring.SubtitleFormatted.Inc()
monitoring.SubtitleFormatLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [WebVTT Spec — Positioning](https://w3c.github.io/webvtt/#webvtt-cue-vertical-alignment)
- 📄 [TTML Align Attribute](https://www.w3.org/TR/ttml2/#style-attribute-textAlign)
- 📄 [EBU STL Spec](https://tech.ebu.ch/docs/tech/tech3264.pdf) — оригінальний стандарт телесубтитрів
- 💻 [astisub styles.go](https://github.com/asticode/go-astisub/blob/master/styles.go) — вихідний код пропагації атрибутів

---

> 💡 **Ключова рекомендація**: Оскільки ви працюєте з **реальним часом**, уникайте повторної пропагації атрибутів для кожного сегмента. Замість цього:
> 1. Зберігайте `StyleAttributes` у кеші каналу після першої пропагації
> 2. Клонуйте тільки при модифікації (copy-on-write)
> 3. Для WebSocket-відправки використовуйте попередньо відформатований текст, а не сиру структуру `astisub.Item`
> 4. Додайте `format_version` у `SubtitleMessage`, щоб клієнт міг кешувати CSS-класи для кольорів

Потрібен приклад інтеграції `propagateSTLAttributes()` у ваш `TTSPlaylistGenerator` для синхронізації позицій субтитрів з аудіо-чанками? Готовий допомогти! 🚀