# 🧱 Глибокий розбір: Ядро astisub — Типи, Утиліти та Конвертація

Цей файл — **серце бібліотеки `astisub`**. Він визначає всі ключові структури даних, утиліти парсингу та механізми конвертації між форматами субтитрів. Розберемо архітектуру та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема пакету

```
┌─────────────────────────────────────────┐
│ 📦 astisub — Core Types & Utilities      │
├─────────────────────────────────────────┤
│                                         │
│  🎯 Data Model:                         │
│  Subtitles → Items[] → Lines[] →        │
│  LineItems[] + StyleAttributes          │
│                                         │
│  🎨 Style System:                       │
│  StyleAttributes (60+ полів) →          │
│  propagate*() → формат-специфічні поля  │
│                                         │
│  ⏱️ Time Utilities:                     │
│  parseDuration() / formatDuration()     │
│  Add() / Fragment() / Unfragment()      │
│                                         │
│  🌐 Format I/O:                         │
│  Open() → авто-детект за розширенням    │
│  Write() → експорт у цільовий формат    │
│                                         │
│  🔧 Helpers:                            │
│  Color conversions, HTML escaping,      │
│  custom scanner для \r\n/\n/\r          │
│                                         │
└─────────────────────────────────────────┘
```

---

## 📦 1. Data Model — Ієрархія субтитрів

### Основна структура:

```go
type Subtitles struct {
    Items    []*Item              // Упорядкований список субтитрів
    Metadata *Metadata            // Глобальні метадані (мова, framerate тощо)
    Regions  map[string]*Region   // Регіони екрану (позиціонування)
    Styles   map[string]*Style    // Глобальні стилі (шрифти, кольори)
}

type Item struct {
    StartAt, EndAt time.Duration  // Таймінги відносно початку відео
    Lines          []Line         // Багаторядковий текст
    Region         *Region        // Посилання на регіон (опціонально)
    Style          *Style         // Посилання на глобальний стиль
    InlineStyle    *StyleAttributes // Інлайн-стилі (перевизначають глобальні)
    Comments       []string       // Коментарі (для SSA/ASS)
    Index          int            // Порядковий номер (для SRT)
}

type Line struct {
    Items     []LineItem  // Елементи рядка з різними стилями
    VoiceName string      // Ім'я мовця (для WebVTT)
}

type LineItem struct {
    Text        string              // Текст
    InlineStyle *StyleAttributes    // Інлайн-стилі для цього фрагменту
    Style       *Style              // Глобальний стиль
    StartAt     time.Duration       // Зсув відносно початку Item (для караоке)
}
```

### ✅ Ваш use-case: створення SubtitleMessage з Item

```go
// У вашому pipeline: конвертація astisub.Item → WebSocket SubtitleMessage
func (p *SubtitleProcessor) itemToMessage(item *astisub.Item, channelID string, seqNum uint64) *SubtitleMessage {
    // Збираємо текст з усіх рядків
    var arabicText, englishText, russianText strings.Builder
    
    for _, line := range item.Lines {
        for _, li := range line.Items {
            // Базовий текст
            baseText := li.Text
            
            // Тут можна додати логіку розпізнавання мови за стилем/коментарем
            arabicText.WriteString(baseText)
            arabicText.WriteString(" ")
        }
        arabicText.WriteString("\n")
    }
    
    // Розрахунок таймінгів у мс для WebSocket
    startMs := item.StartAt.Milliseconds()
    endMs := item.EndAt.Milliseconds()
    
    return &SubtitleMessage{
        Seq:          seqNum,
        TimeStart:    startMs,
        TimeEnd:      endMs,
        StartTimeUTC: time.Now().UTC().Format(time.RFC3339),
        Arabic:       strings.TrimSpace(arabicicText.String()),
        English:      englishText.String(),  // заповнюється після NLLB
        Russian:      russianText.String(),   // заповнюється після NLLB
        VideoSource:  p.getVideoSourceURL(seqNum), // парний/непарний логіка
        // AudioFile, TTS URLs заповнюються асинхронно
    }
}
```

---

## 🎨 2. StyleAttributes — Універсальна система стилів

### Проблема, яку вирішує:
Різні формати субтитрів використовують **різні назви та одиниці** для одних і тих самих атрибутів:

| Атрибут | SRT | SSA/ASS | STL/Teletext | WebVTT | TTML |
|---------|-----|---------|--------------|--------|------|
| **Колір тексту** | `\c&H...` | `PrimaryColour` | `STLColor` | `<c.red>` | `ttml:color` |
| **Жирний** | немає | `\b1` | `STLItalics` | `<b>` | `ttml:fontWeight` |
| **Позиція** | `{\pos(x,y)}` | `MarginLeft` | `STLPosition` | `line:50%` | `ttml:origin` |
| **Вирівнювання** | немає | `Alignment` | `STLJustification` | `align:right` | `ttml:textAlign` |

### Рішення в astisub:
Єдиний `StyleAttributes` struct з **окремими полями для кожного формату** + методи `propagate*()` для конвертації.

```go
type StyleAttributes struct {
    // SRT-специфічні
    SRTBold      bool
    SRTColor     *Color
    SRTPosition  byte  // 1-9 numpad layout
    
    // SSA-специфічні
    SSAAlignment      *int
    SSAPrimaryColour  *Color
    SSAFontName       string
    SSAFontSize       *float64
    
    // STL/Teletext-специфічні
    STLJustification  *Justification
    STLPosition       *STLPosition
    TeletextColor     *Color
    TeletextDoubleHeight *bool
    
    // WebVTT-специфічні
    WebVTTAlign   string      // "left", "center", "right"
    WebVTTLine    string      // "10%", "50%", "90%"
    WebVTTTags    []WebVTTTag // [<b>, <i>, <c.red>]
    
    // TTML-специфічні
    TTMLTextAlign  *string
    TTMLOrigin     *string
    TTMLColor      *Color
    
    // ... ще 30+ полів
}
```

### 🔁 Механізм пропагації:

```go
// Приклад: STL → WebVTT конвертація
func (sa *StyleAttributes) propagateSTLAttributes() {
    // 1. Justification → WebVTTAlign + TTMLTextAlign
    if sa.STLJustification != nil {
        switch *sa.STLJustification {
        case JustificationRight:
            sa.WebVTTAlign = "right"
            sa.TTMLTextAlign = astikit.StrPtr("right")
        case JustificationLeft:
            sa.WebVTTAlign = "left" 
            sa.TTMLTextAlign = astikit.StrPtr("left")
        // Centered не встановлює явно — це default у веб-форматах
        }
    }
    
    // 2. VerticalPosition → WebVTTLine (%)
    if sa.STLPosition != nil && sa.STLPosition.MaxRows > 0 {
        // In-vision (0-99): пряме відображення
        sa.WebVTTLine = fmt.Sprintf("%d%%", sa.STLPosition.VerticalPosition*100/sa.STLPosition.MaxRows)
        
        // Teletext (1-23): корекція на -1 для кращого візуального відповідника
        if sa.STLPosition.MaxRows == 23 && sa.STLPosition.VerticalPosition > 0 {
            sa.WebVTTLine = fmt.Sprintf("%d%%", (sa.STLPosition.VerticalPosition-1)*100/sa.STLPosition.MaxRows)
        }
    }
    
    // 3. Кольори: STL → Teletext (для сумісності)
    if sa.STLColor != nil {
        sa.TeletextColor = sa.STLColor
    }
}
```

### ✅ Ваш use-case: мультиязычне позиціонування

```go
// Конфігурація лейауту для каналу
type LanguageLayout struct {
    Position map[string]*astisub.STLPosition  // вертикальна позиція
    Align    map[string]astisub.Justification // вирівнювання
    Color    map[string]astisub.Color         // колір тексту
}

var DefaultLayout = LanguageLayout{
    Position: map[string]*astisub.STLPosition{
        "ar": {VerticalPosition: 20, MaxRows: 23},  // ~87% — внизу
        "en": {VerticalPosition: 15, MaxRows: 23},  // ~61% — середина-низ  
        "ru": {VerticalPosition: 10, MaxRows: 23},  // ~39% — середина
    },
    Align: map[string]astisub.Justification{
        "ar": astisub.JustificationRight,  // RTL мова
        "en": astisub.JustificationLeft,
        "ru": astisub.JustificationLeft,
    },
    Color: map[string]astisub.Color{
        "ar": {Alpha: 0xFF, Red: 0xFF, Green: 0xD7, Blue: 0x00}, // золотий
        "en": {Alpha: 0xFF, Red: 0x41, Green: 0x69, Blue: 0xE1}, // блакитний
        "ru": {Alpha: 0xFF, Red: 0xDC, Green: 0x14, Blue: 0x3C}, // червоний
    },
}

// Застосування лейауту до субтитру
func (p *SubtitleProcessor) applyLayout(item *astisub.Item, lang string, layout LanguageLayout) {
    for lineIdx := range item.Lines {
        for itemIdx := range item.Lines[lineIdx].Items {
            li := &item.Lines[lineIdx].Items[itemIdx]
            
            // Ініціалізуємо InlineStyle якщо немає
            if li.InlineStyle == nil {
                li.InlineStyle = &astisub.StyleAttributes{}
            }
            
            // Застосовуємо позицію
            if pos, ok := layout.Position[lang]; ok {
                posCopy := *pos
                li.InlineStyle.STLPosition = &posCopy
            }
            
            // Застосовуємо вирівнювання
            if align, ok := layout.Align[lang]; ok {
                alignCopy := align
                li.InlineStyle.STLJustification = &alignCopy
            }
            
            // Застосовуємо колір
            if color, ok := layout.Color[lang]; ok {
                colorCopy := color
                li.InlineStyle.TeletextColor = &colorCopy
            }
            
            // Проганяємо пропагацію для цільових форматів
            li.InlineStyle.propagateSTLAttributes()
            li.InlineStyle.propagateTeletextAttributes()
        }
    }
}
```

---

## ⏱️ 3. Time Utilities — Робота з часом

### parseDuration — гнучкий парсинг:

```go
// signature: parseDuration(input, millisecondSeparator, millisecondPrecision)
parseDuration("12:34:56,123", ",", 3)  // → 12h34m56.123s (SRT формат)
parseDuration("01:23:45.67", ".", 2)   // → 1h23m45.067s (WebVTT, 2 цифри)
parseDuration("1:23:45:67", ":", 2)    // → 1h23m45.067s (двокрапка як десятковий!)

// Edge cases з коду:
// - Автоматичне доповнення: "12:34:56,1" → 100ms при precision=3
// - Помилка при >3 цифрах: "12:34:56,1234" → error
// - Підтримка 2-частинного формату: "12:34,123" → 12m34.123s (без годин)
```

### formatDuration — експорт у різні формати:

```go
// signature: formatDuration(duration, millisecondSeparator, millisecondPrecision)
formatDuration(12*time.Hour+34*time.Minute+56*time.Second+123*time.Millisecond, ",", 3)
// → "12:34:56,123" (SRT)

formatDuration(time.Second+234*time.Millisecond, ".", 3)
// → "00:00:01.234" (WebVTT)

formatDuration(10*time.Millisecond, ".", 3)
// → "00:00:00.010" (зберігає провідні нулі)
```

### ✅ Ваш use-case: синхронізація з HLS-сегментами

```go
// У VideoManifestProxy: корекція таймінгів субтитрів під сегмент
func (p *VideoManifestProxy) syncSubtitlesToSegment(subs *astisub.Subtitles, 
                                                     segmentStartPTS time.Duration,
                                                     segmentDuration time.Duration) {
    for _, item := range subs.Items {
        // Зсуваємо відносно початку сегменту
        item.StartAt += segmentStartPTS
        item.EndAt += segmentStartPTS
        
        // Обрізаємо якщо виходить за межі сегменту
        if item.StartAt < 0 {
            item.StartAt = 0
        }
        if item.EndAt > segmentDuration {
            item.EndAt = segmentDuration
        }
        
        // Видаляємо порожні субтитри
        if item.StartAt >= item.EndAt {
            // позначити для видалення або встановити мінімальну тривалість
            item.EndAt = item.StartAt + 100*time.Millisecond
        }
    }
    
    // Сортуємо після модифікацій
    subs.Order()
}

// Експорт у WebVTT для HLS-плейлиста
func (p *HLSGenerator) itemToWebVTTCue(item *astisub.Item) string {
    // Форматуємо час: WebVTT вимагає крапку та 3 цифри для мс
    start := formatDuration(item.StartAt, ".", 3)
    end := formatDuration(item.EndAt, ".", 3)
    
    // Збираємо текст з підтримкою WebVTT-тегів
    var textBuilder strings.Builder
    for _, line := range item.Lines {
        for _, li := range line.Items {
            // Додаємо inline-теги якщо є стилі
            if li.InlineStyle != nil && len(li.InlineStyle.WebVTTTags) > 0 {
                for _, tag := range li.InlineStyle.WebVTTTags {
                    textBuilder.WriteString(tag.startTag())
                }
            }
            
            // Текст з HTML-екрануванням
            textBuilder.WriteString(escapeHTML(li.Text))
            
            // Закриваємо теги в зворотному порядку
            if li.InlineStyle != nil && len(li.InlineStyle.WebVTTTags) > 0 {
                for i := len(li.InlineStyle.WebVTTTags) - 1; i >= 0; i-- {
                    textBuilder.WriteString(li.InlineStyle.WebVTTTags[i].endTag())
                }
            }
        }
        textBuilder.WriteString("\n")
    }
    
    // Формуємо cue header з позиціонуванням
    cueHeader := fmt.Sprintf("%s --> %s", start, end)
    if len(item.Lines) > 0 && len(item.Lines[0].Items) > 0 {
        if style := item.Lines[0].Items[0].InlineStyle; style != nil {
            if style.WebVTTLine != "" {
                cueHeader += fmt.Sprintf(" line:%s", style.WebVTTLine)
            }
            if style.WebVTTAlign != "" {
                cueHeader += fmt.Sprintf(" align:%s", style.WebVTTAlign)
            }
            if style.WebVTTPosition != nil {
                cueHeader += fmt.Sprintf(" position:%s", style.WebVTTPosition.XPosition)
            }
        }
    }
    
    return fmt.Sprintf("%s\n%s\n", cueHeader, strings.TrimSpace(textBuilder.String()))
}
```

---

## 🔄 4. Manipulation Methods — Маніпуляції з субтитрами

### Add() — зсув таймінгів:
```go
// Зсуває всі таймінги на duration (може бути від'ємним)
// Автоматично видаляє субтитри, що стали повністю від'ємними
// Обрізає початок до 0 якщо StartAt < 0

subs.Add(500 * time.Millisecond)  // +0.5с до всіх
subs.Add(-2 * time.Second)         // -2с, видалить ранні субтитри
```

### Fragment() / Unfragment() — робота з перекриттями:
```go
// Fragment: розбиває субтитри на інтервали f
// Корисно для синхронізації з аудіо-чанками
subs.Fragment(4 * time.Second)  // розбиває по 4с межах

// Unfragment: зливає суміжні субтитри з однаковим текстом
// Оптимізує вивід, зменшує кількість повідомлень
subs.Unfragment()
```

### Merge() — об'єднання потоків:
```go
// Об'єднує Items, Regions, Styles з двох Subtitles
// Автоматично сортує за часом після об'єднання
arabicSubs.Merge(englishSubs)  // мультиязычні субтитри в одному потоці
```

### ApplyLinearCorrection() — масштабування часу:
```go
// Лінійна корекція: мапить [actual1, actual2] → [desired1, desired2]
// Формула: newTime = a*oldTime + b, де a=(d2-d1)/(a2-a1), b=d1-a*a1

// ✅ Ваш use-case: корекція дрейфу між серверним та медіа-часом
subs.ApplyLinearCorrection(
    10*time.Second,  // actual1: медіа-час початку
    60*time.Second,  // actual2: медіа-час кінця  
    12*time.Second,  // desired1: серверний час початку
    65*time.Second,  // desired2: серверний час кінця
)
// Результат: субтитри масштабуються під новий таймлайн
```

### Optimize() / RemoveStyling() — очищення:
```go
// Optimize: видаляє невикористані Regions/Styles
// Зменшує розмір даних для передачі

// RemoveStyling: повністю видаляє всі стилі
// Корисно перед перекладом (NLLB працює з чистим текстом)
subs.RemoveStyling()
plainText := subs.Items[0].String()  // тільки текст, без тегів
```

---

## 🌐 5. Format I/O — Читання та запис

### Open() — авто-детект формату:
```go
// Визначає формат за розширенням файлу
astisub.Open(astisub.Options{Filename: "subs.srt"})      // → ReadFromSRT
astisub.Open(astisub.Options{Filename: "stream.ts", Teletext: TeletextOptions{Page: 888}})  // → ReadFromTeletext

// Supported extensions:
// .srt, .ssa, .ass, .stl, .ts (teletext), .ttml, .vtt
```

### Write() — експорт:
```go
// Експортує в цільовий формат за розширенням
subs.Write("output.vtt")  // → WriteToWebVTT

// Або запис у io.Writer для стрімінгу:
var buf bytes.Buffer
subs.WriteToWebVTT(&buf)  // відправка у WebSocket без проміжного файлу
```

### ✅ Ваш use-case: стрімінгова обробка без файлів

```go
// ProcessTeletextSegment — обробка одного сегменту в пам'яті
func (p *SubtitleProcessor) ProcessTeletextSegment(data []byte, seqNum uint64, pts time.Duration) error {
    // 1. Парсинг з bytes.Reader (без створення файлу)
    reader := bytes.NewReader(data)
    subs, err := astisub.ReadFromTeletext(reader, astisub.TeletextOptions{
        Page: p.channelConfig.TeletextPage,
        PID:  p.channelConfig.TeletextPID,
    })
    if err != nil {
        return fmt.Errorf("teletext parse: %w", err)
    }
    
    // 2. Корекція часу відносно початку стріму
    streamOffset := pts.Sub(p.streamStartTime)
    subs.Add(streamOffset)
    
    // 3. Підготовка для перекладу (видаляємо стилі)
    subs.RemoveStyling()
    
    // 4. Експорт тексту для NLLB
    arabicText := p.extractText(subs)
    
    // 5. Асинхронний переклад + TTS
    go p.translateAndSend(seqNum, arabicText, subs.Items)
    
    return nil
}

// extractText: збирає чистий текст з усіх Items
func (p *SubtitleProcessor) extractText(subs *astisub.Subtitles) string {
    var text strings.Builder
    for _, item := range subs.Items {
        text.WriteString(item.String())  // item.String() = concat всіх Lines
        text.WriteString(" ")
    }
    return strings.TrimSpace(text.String())
}
```

---

## 🎨 6. Color System — Конвертація кольорів

### Підтримувані формати:

```go
// SSA: 0xAABBGGRR (little-endian, Alpha first)
c, _ := newColorFromSSAString("12345678", 16)
// → Color{Alpha:0x12, Blue:0x34, Green:0x56, Red:0x78}

// HTML/TTML: "#RRGGBB" або named colors
c, _ := newColorFromHTMLString("#FFD700")  // золотий
c, _ := newColorFromHTMLString("red")       // named color

// WebVTT: CSS color classes
c := Color{Red: 255, Green: 255, Blue: 0}
c.WebVTTString()  // → "yellow" (з мапінгу популярних кольорів)
```

### Експорт у різні формати:

```go
c := &Color{Alpha: 0xFF, Red: 0xFF, Green: 0xD7, Blue: 0x00}  // золотий

c.SSAString()    // → "ffd700ff" (AABBGGRR у hex)
c.HTMLString()   // → "#ffd700" (тільки RGB, без alpha)
c.WebVTTString() // → "yellow" (якщо є в мапі, інакше "")
```

### ✅ Ваш use-case: кольорове кодування мов у WebVTT

```go
// Оскільки WebVTT не підтримує inline hex-кольори,
// використовуємо <c.class> теги + CSS на клієнті

func (p *SubtitleFormatter) colorToWebVTTClass(color *astisub.Color) string {
    if color == nil {
        return ""
    }
    
    // Мапінг наших кольорів у CSS-класи
    rgb := fmt.Sprintf("#%02x%02x%02x", color.Red, color.Green, color.Blue)
    classMap := map[string]string{
        "#ffd700": "gold",    // арабська
        "#4169e1": "blue",    // англійська
        "#dc143c": "red",     // російська
        "#00ff00": "lime",    // інші
    }
    
    if class, ok := classMap[rgb]; ok {
        return class
    }
    
    // Fallback: генеруємо унікальний клас
    return fmt.Sprintf("color_%02x%02x%02x", color.Red, color.Green, color.Blue)
}

// Використання при експорті:
func (p *SubtitleFormatter) itemToWebVTTCue(item *astisub.Item) string {
    var textBuilder strings.Builder
    
    for _, line := range item.Lines {
        for _, li := range line.Items {
            if li.InlineStyle != nil && li.InlineStyle.TeletextColor != nil {
                class := p.colorToWebVTTClass(li.InlineStyle.TeletextColor)
                textBuilder.WriteString(fmt.Sprintf("<c.%s>", class))
            }
            
            textBuilder.WriteString(escapeHTML(li.Text))
            
            if li.InlineStyle != nil && li.InlineStyle.TeletextColor != nil {
                textBuilder.WriteString("</c>")
            }
        }
        textBuilder.WriteString("\n")
    }
    
    // ... формування cue header
    return cueHeader + "\n" + strings.TrimSpace(textBuilder.String()) + "\n"
}
```

> 📝 **Примітка**: Клієнт має мати CSS для цих класів:
```css
.c.gold { color: #FFD700; }
.c.blue { color: #4169E1; }
.c.red { color: #DC143C; }
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Причина | Рішення |
|----------|---------|---------|
| **Колір не відображається у WebVTT** | `WebVTTString()` повертає "" для невідомих кольорів | Використовуйте `colorToWebVTTClass()` з fallback на генерацію унікального класу |
| **Таймінги "з'їжджають" після сегменту** | Не враховано `segmentStartPTS` при парсингу | Застосовуйте `subs.Add(streamOffset)` після парсингу |
| **Дублікати після Fragment/Unfragment** | Неправильний порівняння тексту з пробілами | Використовуйте `strings.TrimSpace()` перед порівнянням |
| **JustificationCentered ігнорується** | Це очікувана поведінка — центрування є default | Не встановлюйте `WebVTTAlign` явно для центрування |
| **Alpha-канал втрачається в HTML** | `HTMLString()` не включає alpha (CSS стандарт) | Для прозорості використовуйте окремі CSS-класи з `rgba()` на клієнті |
| **Повільний парсинг великих файлів** | `parseDuration` викликається для кожного субтитру | Кешуйте результати парсингу для повторюваних значень |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування пропагації стилів:
```go
// Замість виклику propagate*() для кожного Item:
type CachedStyle struct {
    key       string  // hash від вхідних атрибутів
    webvtt    *astisub.StyleAttributes
    ttml      *astisub.StyleAttributes
    timestamp time.Time
}

var styleCache = sync.Map{}  // channelID → map[styleKey]CachedStyle

func (p *SubtitleProcessor) getCachedStyle(channelID string, input *astisub.StyleAttributes) *astisub.StyleAttributes {
    key := styleHash(input)  // SHA256 від значень полів
    
    if cached, ok := styleCache.Load(channelID); ok {
        if style, ok := cached.(map[string]CachedStyle)[key]; ok {
            if time.Since(style.timestamp) < 5*time.Minute {
                return style.webvtt  // повертаємо готовий результат
            }
        }
    }
    
    // Обчислюємо вперше
    result := *input
    result.propagateSTLAttributes()
    result.propagateTeletextAttributes()
    
    // Зберігаємо в кеш
    // ... (код оновлення cache)
    
    return &result
}
```

### 2. Пакетна обробка таймінгів:
```go
// Замість індивідуального Add() для кожного Item:
func batchAdd(items []*astisub.Item, offset time.Duration) {
    for _, item := range items {
        item.StartAt += offset
        item.EndAt += offset
        // Видалення/обрізання — окремо після циклу
    }
    // Потім один виклик Order() замість сортування після кожного змінення
}
```

### 3. Lazy HTML escaping:
```go
// Не екрануйте текст до моменту експорту:
type LazyItem struct {
    item *astisub.Item
    escaped bool
}

func (l *LazyItem) Text() string {
    if !l.escaped {
        // Екрануємо тільки при першому читанні
        for _, line := range l.item.Lines {
            for i := range line.Items {
                line.Items[i].Text = escapeHTML(line.Items[i].Text)
            }
        }
        l.escaped = true
    }
    return l.item.String()
}
```

---

## 📋 Чек-лист інтеграції astisub

```go
// ✅ 1. Імпорт та ініціалізація
import "github.com/asticode/go-astisub"

// ✅ 2. Парсинг (без створення файлів)
reader := bytes.NewReader(segmentData)
subs, _ := astisub.ReadFromTeletext(reader, opts)

// ✅ 3. Корекція часу
streamOffset := pts.Sub(streamStartTime)
subs.Add(streamOffset)

// ✅ 4. Підготовка тексту для перекладу
subs.RemoveStyling()  // якщо стилі не потрібні для NLLB
text := extractText(subs)

// ✅ 5. Застосування лейауту (позиція/колір)
for _, item := range subs.Items {
    applyLayout(item, "ar", DefaultLayout)
    // propagate*() викликається всередині applyLayout
}

// ✅ 6. Експорт у цільовий формат
var buf bytes.Buffer
for _, item := range subs.Items {
    buf.WriteString(itemToWebVTTCue(item))
}
webvtt := buf.String()

// ✅ 7. Сортування перед відправкою (якщо були модифікації)
subs.Order()

// ✅ 8. Метрики
monitoring.SubtitlesParsed.Inc()
monitoring.SubtitleParseLatency.Observe(time.Since(receivedAt).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [astisub GitHub](https://github.com/asticode/go-astisub) — повна документація
- 📄 [Format Specifications](https://github.com/asticode/go-astisub#supported-formats) — деталі підтримуваних форматів
- 📄 [WebVTT Spec](https://w3c.github.io/webvtt/) — офіційна специфікація
- 📄 [TTML Profiling](https://www.w3.org/TR/ttml-imsc1.0.1/) — профіль для субтитрів
- 🧪 [astisub testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади файлів для тестів

---

> 💡 **Ключова рекомендація**: Оскільки ваш pipeline працює в **реальному часі** з сегментами 4-10с:
> 1. **Уникайте `Fragment()/Unfragment()` на кожному сегменті** — це O(n²) операції. Зберігайте "базові" субтитри в кеші каналу.
> 2. **Кешуйте результати `propagate*()`** — стилі рідко змінюються в межах одного каналу.
> 3. **Використовуйте `bytes.Reader` замість тимчасових файлів** — зменшує I/O latency.
> 4. **Екрануйте HTML тільки при експорті** — зберігайте сирий текст для перекладу.
> 5. **Додайте `format_version` у `SubtitleMessage`** — дозволить клієнту кешувати CSS-класи та уникати повторного парсингу.

Потрібен приклад інтеграції `ApplyLinearCorrection()` у ваш `VideoManifestProxy` для математичної синхронізації часу між медіа- та серверним таймлайном? Готовий допомогти! 🚀