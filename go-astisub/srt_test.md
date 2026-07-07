# 📝 Глибокий розбір: SRT формат в astisub

Цей тестовий файл демонструє **роботу з форматом SRT** (SubRip) — найпростішим і найпоширенішим форматом субтитрів. Розберемо архітектуру, ключові особливості та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Огляд тестованих сценаріїв

```
┌────────────────────────────────────────┐
│ 🎯 Ключові можливості SRT в astisub    │
├────────────────────────────────────────┤
│                                         │
│ 📦 Формат файлу (дуже простий):        │
│ 1                                       │
│ 00:01:39,000 --> 00:01:41,040          │
│ (deep rumbling)                         │
│                                         │
│ ⏱️  Гнучкий парсинг часу:                │
│ • "00:00:01.876" (крапка)               │
│ • "00:00:04:609" (двокрапка для мс)     │
│ • "-->" з пробілами або без             │
│                                         │
│ 🎨 Базові стилі (через теги):           │
│ <b>жирний</b>, <i>курсив</i>            │
│ <u>підкреслений</u>, <font color="..."> │
│                                         │
│ 🔤 UTF-8 вимога:                        │
│ • Не-UTF8 файли → помилка парсингу     │
│ • Автоматичне декодування для арабської│
│                                         │
│ 🔄 Round-trip тестування:               │
│ • Parse SRT → astisub → Write SRT      │
│ • Байт-в-байт порівняння з еталоном    │
│                                         │
└────────────────────────────────────────┘
```

---

## 📦 1. SRT Format Basics — Структура файлу

### Стандартний формат:

```srt
1
00:01:39,000 --> 00:01:41,040
(deep rumbling)

2
00:02:04,080 --> 00:02:07,120
MAN:
How did we end up here?
```

### Ключові правила:
| Елемент | Формат | Примітки |
|---------|--------|----------|
| **Номер** | Ціле число | Може бути пропущено (astisub проігнорує) |
| **Таймінг** | `HH:MM:SS,mmm --> HH:MM:SS,mmm` | Кома або крапка для мілісекунд |
| **Текст** | Будь-який UTF-8 | Багаторядковий, підтримка тегів |
| **Роздільник** | Пустий рядок | Обов'язковий між субтитрами |

### ✅ Ваш use-case: парсинг SRT з пам'яті (streaming)

```go
// ProcessSRTSegment — обробка SRT-сегменту в реальному часі
func (p *SubtitleProcessor) ProcessSRTSegment(data []byte, seqNum uint64, pts time.Duration) error {
    // 1. Парсинг з bytes.Reader (без тимчасових файлів)
    reader := bytes.NewReader(data)
    subs, err := astisub.ReadFromSRT(reader)
    if err != nil {
        return fmt.Errorf("srt parse: %w", err)
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

## ⏱️ 2. Flexible Duration Parsing — Гнучкий парсинг часу

### Тест `TestSRTParseDuration`:

```go
// Вхідні дані з різними форматами часу:
testData := `
1
00:00:01.876-->00:0:03.390
Duration without enclosing space

2
00:00:04:609-->00:0:05:985
Duration without colon milliseconds`

// Результат: обидва формати успішно парсяться
assert.Equal(t, 1*time.Second+876*time.Millisecond, s.Items[0].StartAt)  // крапка
assert.Equal(t, 4*time.Second+609*time.Millisecond, s.Items[1].StartAt)   // двокрапка
```

### Що підтримується:

```go
// ✅ Всі ці формати працюють:
"00:01:39,000 --> 00:01:41,040"  // стандарт (кома + пробіли)
"00:01:39.000-->00:01:41.040"    // крапка + без пробілів
"00:01:39:000 --> 00:01:41:040"  // двокрапка для мілісекунд
"0:01:39,000 --> 0:01:41,040"    // одна цифра для годин

// ❌ Ці формати НЕ працюватимуть:
"00:01:39,0000 --> 00:01:41,040" // 4 цифри для мс (макс 3)
"01:39,000 --> 01:41,040"        // пропущено години (але іноді працює)
```

### ✅ Ваш use-case: синхронізація з PTS у HLS

```go
// SRT час → PTS конвертація
func (p *SubtitleProcessor) srtDurationToPTS(d time.Duration, segmentBasePTS uint64) uint64 {
    // Конвертуємо duration у наносекунди → 90kHz PTS
    ns := int64(d)
    pts := uint64(ns * 90 / 1e6)
    return segmentBasePTS + pts
}

// Форматування для запису у SRT
func (p *SubtitleProcessor) formatSRTTimestamp(d time.Duration) string {
    // astisub.formatDuration(d, ",", 3) → "00:01:39,000"
    return astisub.FormatDurationForSRT(d)  // уявна функція
}

// Приклад використання при експорті:
func (p *SubtitleProcessor) itemToSRT(item *astisub.Item) string {
    start := formatDuration(item.StartAt, ",", 3)
    end := formatDuration(item.EndAt, ",", 3)
    
    var text strings.Builder
    for _, line := range item.Lines {
        text.WriteString(line.String())
        text.WriteString("\n")
    }
    
    return fmt.Sprintf("%s\n%s --> %s\n%s\n", 
        "1",  // номер (можна генерувати послідовно)
        start, end, 
        strings.TrimSpace(text.String()))
}
```

---

## 🎨 3. Basic Styling in SRT — Базові стилі

### Підтримувані HTML-подібні теги:

```srt
<b>жирний текст</b>
<i>курсивний текст</i>
<u>підкреслений текст</u>
<font color="#FFD700">золотий текст</font>
```

### Тест `TestSRTStyled` — ключові перевірки:

```go
// 1. Парсинг стилів з тексту
s, _ := astisub.OpenFile("./testdata/example-in-styled.srt")

// 2. Перевірка атрибутів для кожного LineItem
assert.Equal(t, astisub.ColorLime, s.Items[0].Lines[0].Items[0].InlineStyle.SRTColor)
assert.True(t, s.Items[0].Lines[0].Items[0].InlineStyle.SRTBold)

// 3. Перевірка незакритих тегів (не "просочуються" на наступні субтитри)
assert.Nil(t, s.Items[8].Lines[0].Items[0].InlineStyle)  // стиль скинуто

// 4. Перевірка розбиття тексту з тегами на кілька LineItem
// "x<i>^3 * </i>x = 100" → 4 окремих фрагменти:
// [0] "x" (italic=false)
// [1] "^3 * " (italic=true)
// [2] "x" (italic=false)
// [3] " = 100" (italic=false)
assert.Len(t, s.Items[9].Lines[0].Items, 4)
assert.True(t, s.Items[9].Lines[0].Items[0].InlineStyle.SRTItalics)   // x
assert.Nil(t, s.Items[9].Lines[0].Items[1].InlineStyle)               // ^3 * 
assert.True(t, s.Items[9].Lines[0].Items[2].InlineStyle.SRTItalics)   // x
assert.Nil(t, s.Items[9].Lines[0].Items[3].InlineStyle)               // = 100
```

### 🔍 Як це працює:

```
Вхід: "x<i>^3 * </i>x = 100"
              ↓
      Знайдено <i> та </i>
              ↓
      Розбиття на 4 фрагменти:
      ├─ "x" → LineItem{Text: "x", InlineStyle: {SRTItalics: false}}
      ├─ "^3 * " → LineItem{Text: "^3 * ", InlineStyle: {SRTItalics: true}}
      ├─ "x" → LineItem{Text: "x", InlineStyle: {SRTItalics: false}}
      └─ " = 100" → LineItem{Text: " = 100", InlineStyle: {SRTItalics: false}}
```

### ✅ Ваш use-case: кольорове кодування мов

```go
// Оскільки SRT підтримує <font color>, можемо використовувати це для мов
var LanguageSRTColors = map[string]string{
    "ar": "#FFD700",  // золотий для арабської
    "en": "#4169E1",  // блакитний для англійської
    "ru": "#DC143C",  // червоний для російської
}

// Застосування кольору до субтитру
func (p *SubtitleProcessor) applySRTColor(item *astisub.Item, lang string) {
    if colorHex, ok := LanguageSRTColors[lang]; ok {
        for lineIdx := range item.Lines {
            for itemIdx := range item.Lines[lineIdx].Items {
                li := &item.Lines[lineIdx].Items[itemIdx]
                if li.InlineStyle == nil {
                    li.InlineStyle = &astisub.StyleAttributes{}
                }
                // SRTColor зберігається для внутрішньої обробки
                li.InlineStyle.SRTColor = astisub.ColorFromHex(colorHex)
            }
        }
    }
}

// Експорт у SRT з тегами <font>
func (p *SubtitleProcessor) itemToSRTWithColors(item *astisub.Item) string {
    var textBuilder strings.Builder
    
    for _, line := range item.Lines {
        for _, li := range line.Items {
            // Додаємо <font color> якщо є колір
            if li.InlineStyle != nil && li.InlineStyle.SRTColor != nil {
                hex := li.InlineStyle.SRTColor.HTMLString()  // "#FFD700"
                textBuilder.WriteString(fmt.Sprintf("<font color=\"%s\">", hex))
            }
            
            // Додаємо інші теги
            if li.InlineStyle != nil {
                if li.InlineStyle.SRTBold { textBuilder.WriteString("<b>") }
                if li.InlineStyle.SRTItalics { textBuilder.WriteString("<i>") }
                if li.InlineStyle.SRTUnderline { textBuilder.WriteString("<u>") }
            }
            
            // Текст
            textBuilder.WriteString(escapeHTML(li.Text))
            
            // Закриваємо теги у зворотному порядку
            if li.InlineStyle != nil {
                if li.InlineStyle.SRTUnderline { textBuilder.WriteString("</u>") }
                if li.InlineStyle.SRTItalics { textBuilder.WriteString("</i>") }
                if li.InlineStyle.SRTBold { textBuilder.WriteString("</b>") }
            }
            if li.InlineStyle != nil && li.InlineStyle.SRTColor != nil {
                textBuilder.WriteString("</font>")
            }
        }
        textBuilder.WriteString("\n")
    }
    
    return strings.TrimSpace(textBuilder.String())
}
```

> ⚠️ **Увага**: Не всі плеєри підтримують `<font color>` у SRT. VLC підтримує, але деякі веб-плеєри можуть ігнорувати. Для максимальної сумісності використовуйте прості теги `<b>`, `<i>`, `<u>`.

---

## 🔤 4. UTF-8 Encoding — Вимога до кодування

### Тест `TestNonUTF8SRT`:

```go
// Спроба відкрити файл у неправильному кодуванні
_, err := astisub.OpenFile("./testdata/example-in-non-utf8.srt")
assert.Error(t, err)  // очікуємо помилку
```

### Чому це важливо:
- SRT в astisub **вимагає UTF-8** для коректної роботи з міжнародними символами
- Арабська, кирилиця, ієрогліфи — всі потребують UTF-8
- Файли у Windows-1251, ISO-8859-1 тощо → помилка парсингу

### ✅ Ваш use-case: валідація вхідних даних

```go
// validateUTF8 — перевірка чи дані у валідному UTF-8
func validateUTF8(data []byte) error {
    if !utf8.Valid(data) {
        // Спроба авто-конвертації з поширених кодувань
        if converted, err := detectAndConvertEncoding(data); err == nil {
            return nil  // успішна конвертація
        }
        return fmt.Errorf("invalid UTF-8 encoding")
    }
    return nil
}

// detectAndConvertEncoding — спроба конвертації з інших кодувань
func detectAndConvertEncoding(data []byte) ([]byte, error) {
    // Спроба з Windows-1251 (кирилиця)
    if decoder := charmap.Windows1251.NewDecoder(); decoder != nil {
        if converted, err := decoder.Bytes(data); err == nil && utf8.Valid(converted) {
            return converted, nil
        }
    }
    // Додати інші кодування за потребою
    return nil, fmt.Errorf("unsupported encoding")
}

// Використання при парсингу:
func (p *SubtitleProcessor) ProcessSRTSegment(data []byte, ...) error {
    // 1. Валідація/конвертація кодування
    if err := validateUTF8(data); err != nil {
        log.Warn("SRT encoding issue", "err", err)
        // Можна спробувати авто-конвертацію або повернути помилку
    }
    
    // 2. Парсинг
    reader := bytes.NewReader(data)
    subs, err := astisub.ReadFromSRT(reader)
    // ...
}
```

---

## 🔄 5. Round-Trip Testing — Гарантія сумісності

### Тест `TestSRT`:

```go
// 1. Відкриття вхідного файлу
s, err := astisub.OpenFile("./testdata/example-in.srt")
assert.NoError(t, err)

// 2. Перевірка контенту (через assertSubtitleItems)
assert.Equal(t, 6, len(s.Items))
assert.Equal(t, time.Minute+39*time.Second, s.Items[0].StartAt)
assert.Equal(t, "(deep rumbling)", s.Items[0].Lines[0].String())

// 3. Запис у буфер та порівняння з еталоном
var w bytes.Buffer
err = s.WriteToSRT(&w)
assert.NoError(t, err)

expected, _ := ioutil.ReadFile("./testdata/example-out.srt")
assert.Equal(t, string(expected), w.String())  // байт-в-байт збіг!
```

### Чому це важливо для вашого проекту:

1. **Простота формату**: SRT — найпростіший формат для дебагу та ручного редагування.

2. **Швидка конвертація**: Можна парсити будь-який формат → конвертувати у SRT для тестування → конвертувати назад.

3. **Fallback для клієнтів**: Якщо WebVTT не підтримується, SRT — універсальна резервна опція.

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"????" замість арабського тексту** | Файл не у UTF-8 | Конвертуйте вхідні дані у UTF-8 перед парсингом |
| **Таймінги не парсяться** | Неправильний формат часу | Використовуйте `HH:MM:SS,mmm` з комою або крапкою для мс |
| **Стилі "просочуються" між субтитрами** | Незакриті теги `<i>` без `</i>` | Astisub автоматично скидає стилі між Items, але краще закривати теги |
| **Колір не відображається у плеєрі** | Плеєр не підтримує `<font color>` | Використовуйте тільки `<b>`, `<i>`, `<u>` для максимальної сумісності |
| **Пусті субтитри після парсингу** | Неправильний роздільник рядків | Переконайтеся, що між субтитрами є пустий рядок (`\n\n`) |

---

## ⚡ Оптимізації для real-time обробки

### 1. Пакетний парсинг таймінгів:

```go
// Замість індивідуального parseDuration для кожного таймінгу:
func batchParseSRTTimestamps(timestamps []string) ([]time.Duration, error) {
    results := make([]time.Duration, len(timestamps))
    for i, ts := range timestamps {
        d, err := parseDuration(ts, ",", 3)  // делегуємо до astisub
        if err != nil {
            return nil, err
        }
        results[i] = d
    }
    return results, nil
}
```

### 2. Кешування HTML-екранування:

```go
// Текст субтитрів рідко містить спецсимволи, тому кешуємо результат
var htmlEscapeCache = sync.Map{}  // text → escaped

func escapeSRTText(text string) string {
    if cached, ok := htmlEscapeCache.Load(text); ok {
        return cached.(string)
    }
    
    escaped := htmlEscaper.Replace(text)  // & → &amp;, < → &lt; тощо
    htmlEscapeCache.Store(text, escaped)
    return escaped
}
```

### 3. Lazy style propagation для SRT → WebVTT:

```go
// Не конвертуйте стилі до моменту експорту
type LazySRTItem struct {
    item        *astisub.Item
    webvttReady *astisub.Item
    dirty       bool
}

func (l *LazySRTItem) GetWebVTTReady() *astisub.Item {
    if l.dirty || l.webvttReady == nil {
        l.webvttReady = cloneAndConvertSRTToWebVTT(l.item)
        l.dirty = false
    }
    return l.webvttReady
}

func cloneAndConvertSRTToWebVTT(src *astisub.Item) *astisub.Item {
    dst := *src
    dst.Lines = make([]astisub.Line, len(src.Lines))
    
    for li, line := range src.Lines {
        dst.Lines[li] = line
        for ii := range line.Items {
            if style := line.Items[ii].InlineStyle; style != nil {
                // Конвертуємо SRT-стилі у WebVTT-теги
                newStyle := &astisub.StyleAttributes{}
                if style.SRTBold {
                    newStyle.WebVTTTags = append(newStyle.WebVTTTags, astisub.WebVTTTag{Name: "b"})
                }
                if style.SRTItalics {
                    newStyle.WebVTTTags = append(newStyle.WebVTTTags, astisub.WebVTTTag{Name: "i"})
                }
                if style.SRTUnderline {
                    newStyle.WebVTTTags = append(newStyle.WebVTTTags, astisub.WebVTTTag{Name: "u"})
                }
                if style.SRTColor != nil {
                    // Колір → <c.class> тег
                    class := colorToWebVTTClass(style.SRTColor)
                    newStyle.WebVTTTags = append(newStyle.WebVTTTags, 
                        astisub.WebVTTTag{Name: "c", Classes: []string{class}})
                }
                line.Items[ii].InlineStyle = newStyle
            }
        }
    }
    return &dst
}
```

---

## 📋 Чек-лист інтеграції SRT

```go
// ✅ 1. Парсинг без проміжних файлів
reader := bytes.NewReader(srtData)
subs, err := astisub.ReadFromSRT(reader)

// ✅ 2. Валідація UTF-8
if !utf8.Valid(srtData) {
    log.Warn("SRT not in UTF-8, attempting conversion")
    // спроба конвертації або помилка
}

// ✅ 3. Корекція таймінгів під сегмент
streamOffset := pts.Sub(streamStartTime)
for _, item := range subs.Items {
    item.StartAt += streamOffset
    item.EndAt += streamOffset
}

// ✅ 4. Застосування кольорів для мов
for _, item := range subs.Items {
    lang := detectLanguage(item)
    applySRTColor(item, lang)
}

// ✅ 5. Експорт у цільовий формат
var buf bytes.Buffer
switch targetFormat {
case "srt":
    subs.WriteToSRT(&buf)
case "webvtt":
    subs.WriteToWebVTT(&buf)  // автоматична конвертація стилів
}

// ✅ 6. Валідація арабського тексту
if err := validateArabicText(extractText(subs)); err != nil {
    log.Warn("arabic text validation", "err", err)
}

// ✅ 7. Метрики
monitoring.SRTParsed.Inc()
monitoring.SRTParseLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [SRT Format Spec (неофіційний)](https://en.wikipedia.org/wiki/SubRip#SRT_file_format) — опис формату
- 💻 [astisub srt.go](https://github.com/asticode/go-astisub/blob/master/srt.go) — вихідний код парсингу/запису
- 🎬 [VLC SRT Support](https://wiki.videolan.org/Subtitles/) — підтримка SRT у VLC
- 🧪 [astisub testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади SRT файлів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **реальним часом** та **арабською мовою**:
> 1. **Завжди валідуйте UTF-8** перед парсингом — арабський текст вимагає правильного кодування.
> 2. **Використовуйте прості теги** `<b>`, `<i>`, `<u>` для стилів — вони мають найкращу сумісність.
> 3. **Для кольорів використовуйте `<font color="#RRGGBB">`** — підтримується у VLC, але може ігноруватися у веб-плеєрах.
> 4. **Кешуйте HTML-екранування** — якщо один і той самий текст зустрічається багато разів, не екрануйте його щоразу.
> 5. **Тестуйте round-trip** (SRT → astisub → SRT) для вашого каналу, щоб переконатися, що арабський текст зберігається коректно.

Потрібен приклад функції `colorToWebVTTClass()` для конвертації `astisub.Color` у CSS-клас для WebVTT? Готовий допомогти! 🚀