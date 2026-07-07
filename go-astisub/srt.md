# 📝 Глибокий розбір: SRT реалізація в astisub

Цей файл — **ядро підтримки формату SRT** (SubRip) у бібліотеці `astisub`. SRT — найпростіший і найпоширеніший формат субтитрів, ідеальний для базової інтеграції. Розберемо архітектуру, критичні деталі та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема SRT в astisub

```
┌────────────────────────────────────────┐
│ 📦 SRT File Structure                   │
├────────────────────────────────────────┤
│                                         │
│  [Optional BOM] \xEF\xBB\xBF           │
│                                         │
│  1                                     │
│  00:01:39,000 --> 00:01:41,040         │
│  (deep rumbling)                       │
│                                        │
│  2                                     │
│  00:02:04,080 --> 00:02:07,120         │
│  MAN:                                  │
│  How did we end up here?               │
│                                         │
│  Формат:                               │
│  • Номер (опціонально)                 │
│  • Таймінг: "HH:MM:SS,mmm --> HH:MM:SS,mmm" │
│  • Текст (1+ рядків, UTF-8)           │
│  • Пустий рядок-роздільник            │
│                                         │
└────────────────────────────────────────┘
```

---

## ⏱️ 1. Flexible Duration Parsing — Гнучкий парсинг часу

### Функція `parseDurationSRT`:

```go
func parseDurationSRT(i string) (d time.Duration, err error) {
    // Спробуємо різні сепаратори для мілісекунд:
    for _, s := range []string{",", ".", ":"} {
        if d, err = parseDuration(i, s, 3); err == nil {
            return  // перший успішний парсинг
        }
    }
    return  // всі спроби невдалі
}
```

### ✅ Підтримувані формати:

```
"00:01:39,000"  // стандарт: кома для мс
"00:01:39.000"  // альтернатива: крапка
"00:01:39:000"  // рідкісний варіант: двокрапка

// Навіть без пробілів навколо "-->":
"00:01:39,000-->00:01:41,040"  // ✅ працює
```

### ❌ Непідтримувані формати:

```
"00:01:39,0000"  // 4 цифри для мс (макс 3)
"01:39,000"      // пропущено години (іноді працює, але ненадійно)
```

### ✅ Ваш use-case: синхронізація з PTS у HLS

```go
// SRT timestamp → PTS конвертація для WebSocket
func (p *SubtitleProcessor) srtTimestampToPTS(ts string, basePTS uint64) (uint64, error) {
    // 1. Парсимо таймінг
    d, err := parseDurationSRT(ts)
    if err != nil {
        return 0, fmt.Errorf("parse SRT timestamp: %w", err)
    }
    
    // 2. Конвертуємо duration → 90kHz PTS
    ns := int64(d)
    pts := uint64(ns * 90 / 1e6)
    
    return basePTS + pts, nil
}

// Зворотна конвертація: PTS → SRT timestamp
func (p *SubtitleProcessor) ptsToSRTTimestamp(pts uint64, basePTS uint64) string {
    relative := pts - basePTS
    ns := int64(relative) * 1e6 / 90
    d := time.Duration(ns)
    
    // formatDuration(d, ",", 3) → "00:01:39,000"
    return formatDurationSRT(d)
}
```

---

## 📥 2. ReadFromSRT — парсинг SRT файлу

### Потік обробки:

```
1. Створення сканера рядків (newScanner)
   ↓
2. Цикл по рядках:
   ├─ Валідація UTF-8 (utf8.ValidString)
   ├─ Видалення BOM з першого рядка
   ├─ Якщо рядок містить "-->" → це таймінг:
   │  ├─ Розбиття: "00:01:39,000 --> 00:01:41,040"
   │  ├─ Парсинг StartAt/EndAt через parseDurationSRT
   │  ├─ Створення нового Item
   │  └─ Додавання до o.Items[]
   │
   ├─ Інакше → це текст:
   │  ├─ parseTextSrt(line, sa) → Line зі стилями
   │  └─ Додавання до поточного Item
   ↓
3. Повернення *Subtitles
```

### 🔍 Ключові моменти парсингу:

```go
// 1. Валідація UTF-8 (критично для арабської!)
if !utf8.ValidString(line) {
    err = fmt.Errorf("astisub: line %d is not valid utf-8", lineNum)
    return
}

// 2. Обробка таймінгів
if strings.Contains(line, srtTimeBoundariesSeparator) {
    // Розбиття за "-->"
    s1 := strings.Split(line, srtTimeBoundariesSeparator)  // ["00:01:39,000 ", " 00:01:41,040"]
    
    // Видалення зайвих параметрів після часу (напр. позиції)
    s2 := strings.Fields(s1[1])  // ["00:01:41,040", "position:...", ...]
    
    // Парсинг тільки першого елемента
    s.StartAt, _ = parseDurationSRT(s1[0])
    s.EndAt, _ = parseDurationSRT(s2[0])  // ігноруємо решту
}

// 3. Обробка тексту зі стилями
if l := parseTextSrt(line, sa); len(l.Items) > 0 {
    s.Lines = append(s.Lines, l)
}
```

### ✅ Ваш use-case: парсинг SRT з пам'яті (streaming)

```go
// ProcessSRTSegment — обробка SRT-сегменту в реальному часі
func (p *SubtitleProcessor) ProcessSRTSegment(data []byte, seqNum uint64, pts time.Duration) error {
    // 1. Валідація/конвертація кодування
    if !utf8.Valid(data) {
        log.Warn("SRT not UTF-8, attempting conversion")
        // Спроба авто-конвертації або помилка
        if converted, err := convertToUTF8(data); err == nil {
            data = converted
        } else {
            return fmt.Errorf("invalid encoding: %w", err)
        }
    }
    
    // 2. Парсинг з bytes.Reader
    reader := bytes.NewReader(data)
    subs, err := astisub.ReadFromSRT(reader)
    if err != nil {
        return fmt.Errorf("srt parse: %w", err)
    }
    
    // 3. Корекція часу відносно початку стріму
    streamOffset := pts.Sub(p.streamStartTime)
    for _, item := range subs.Items {
        item.StartAt += streamOffset
        item.EndAt += streamOffset
    }
    
    // 4. Експорт тексту для перекладу (видаляємо теги)
    arabicText := p.extractTextWithoutTags(subs)
    
    // 5. Асинхронний переклад + TTS
    go p.translateAndSend(seqNum, arabicText, subs.Items)
    
    return nil
}

// extractTextWithoutTags — витягує чистий текст, ігноруючи <b>, <i>, <font>
func (p *SubtitleProcessor) extractTextWithoutTags(subs *astisub.Subtitles) string {
    var text strings.Builder
    for _, item := range subs.Items {
        for _, line := range item.Lines {
            for _, li := range line.Items {
                text.WriteString(li.Text)  // вже unescapeHTML в parseTextSrt
                text.WriteString(" ")
            }
        }
        text.WriteString("\n")
    }
    return strings.TrimSpace(text.String())
}
```

---

## 🎨 3. parseTextSrt — парсинг стилів через HTML-токенізатор

### Архітектура:

```go
func parseTextSrt(i string, sa *StyleAttributes) (o Line) {
    // 1. Створення HTML-токенізатора
    tr := html.NewTokenizer(strings.NewReader(i))
    
    // 2. Цикл по токенах
    for {
        t := tr.Next()  // наступний токен
        
        switch t {
        case html.StartTagToken:  // <b>, <i>, <u>, <font>
            token := tr.Token()
            switch token.Data {
            case "b": sa.SRTBold = true
            case "i": sa.SRTItalics = true
            case "u": sa.SRTUnderline = true
            case "font":
                if c := htmlTokenAttribute(&token, "color"); c != nil {
                    sa.SRTColor, _ = newColorFromHTMLString(*c)
                }
            }
            
        case html.EndTagToken:  // </b>, </i>, </u>, </font>
            token := tr.Token()
            switch token.Data {
            case "b": sa.SRTBold = false
            case "i": sa.SRTItalics = false
            case "u": sa.SRTUnderline = false
            case "font": sa.SRTColor = nil
            }
            
        case html.TextToken:  // звичайний текст
            if s := strings.TrimSpace(raw); s != "" {
                // Копіюємо поточні стилі для цього фрагменту
                var styleAttributes *StyleAttributes
                if sa.SRTBold || sa.SRTColor != nil || sa.SRTItalics || sa.SRTUnderline {
                    styleAttributes = &StyleAttributes{
                        SRTBold:      sa.SRTBold,
                        SRTColor:     sa.SRTColor,
                        SRTItalics:   sa.SRTItalics,
                        SRTUnderline: sa.SRTUnderline,
                    }
                    styleAttributes.propagateSRTAttributes()
                }
                
                // Додаємо фрагмент з стилями
                o.Items = append(o.Items, LineItem{
                    InlineStyle: styleAttributes,
                    Text:        unescapeHTML(raw),  // &amp; → &, &lt; → <
                })
            }
        }
    }
    return
}
```

### 🔍 Як це працює на прикладі:

```
Вхід: "x<font color=\"#FFD700\"><b>золотий</b></font> текст"

Токенізація:
1. Text: "x" → LineItem{Text: "x", Style: nil}
2. StartTag: <font color="#FFD700"> → sa.SRTColor = золотий
3. StartTag: <b> → sa.SRTBold = true
4. Text: "золотий" → LineItem{Text: "золотий", Style: {Color: золотий, Bold: true}}
5. EndTag: </b> → sa.SRTBold = false
6. EndTag: </font> → sa.SRTColor = nil
7. Text: " текст" → LineItem{Text: " текст", Style: nil}

Результат: 3 окремих LineItem з різними стилями
```

### ✅ Ваш use-case: кольорове кодування мов

```go
// Оскільки SRT підтримує <font color>, можемо використовувати це для мов
var LanguageSRTColors = map[string]string{
    "ar": "#FFD700",  // золотий для арабської
    "en": "#4169E1",  // блакитний для англійської
    "ru": "#DC143C",  // червоний для російської
}

// Застосування кольору до субтитру (для подальшого експорту)
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
            
            // Текст з HTML-екрануванням
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

> ⚠️ **Увага**: `html.NewTokenizer` автоматично обробляє вкладені теги, але **не валідує** правильність закриття. Незакритий `<i>` буде скинутий між субтитрами, але краще уникати таких випадків.

---

## ✍️ 4. WriteToSRT — експорт у SRT формат

### Потік генерації:

```
1. Додавання BOM header (\xEF\xBB\xBF) для UTF-8 сумісності
   ↓
2. Цикл по Items:
   ├─ Запис номера (k+1)
   ├─ Запис таймінгів: formatDurationSRT(StartAt) + " --> " + formatDurationSRT(EndAt)
   ├─ Цикл по Lines:
   │  └─ Запис тексту з тегами через li.srtBytes()
   └─ Додавання пустого рядка-роздільника
   ↓
3. Видалення останнього зайвого \n
   ↓
4. Запис у io.Writer
```

### Форматування тексту з тегами (`li.srtBytes()`):

```go
func (li LineItem) srtBytes() (c []byte) {
    // 1. Отримуємо стилі
    var color string
    if li.InlineStyle != nil && li.InlineStyle.SRTColor != nil {
        color = li.InlineStyle.SRTColor.HTMLString()  // "#FFD700"
    }
    b := li.InlineStyle != nil && li.InlineStyle.SRTBold
    i := li.InlineStyle != nil && li.InlineStyle.SRTItalics
    u := li.InlineStyle != nil && li.InlineStyle.SRTUnderline
    
    // 2. Відкриваємо теги у правильному порядку
    if color != "" { c = append(c, []byte("<font color=\""+color+"\">")...) }
    if b { c = append(c, []byte("<b>")...) }
    if i { c = append(c, []byte("<i>")...) }
    if u { c = append(c, []byte("<u>")...) }
    
    // 3. Текст з HTML-екрануванням (& → &amp;, < → &lt;)
    c = append(c, []byte(escapeHTML(li.Text))...)
    
    // 4. Закриваємо теги у зворотному порядку
    if u { c = append(c, []byte("</u>")...) }
    if i { c = append(c, []byte("</i>")...) }
    if b { c = append(c, []byte("</b>")...) }
    if color != "" { c = append(c, []byte("</font>")...) }
    
    return
}
```

### ✅ Ваш use-case: експорт SRT для архіву або fallback

```go
// ExportSRTForArchive — підготовка SRT файлу для довгострокового зберігання
func (p *ArchiveProcessor) ExportSRTForArchive(subs *astisub.Subtitles, channelID string) ([]byte, error) {
    // 1. Застосування кольорів для мов (якщо потрібно)
    for _, item := range subs.Items {
        lang := detectLanguage(item)  // ваша логіка
        p.applySRTColor(item, lang)
    }
    
    // 2. Експорт у bytes.Buffer (без файлу)
    var buf bytes.Buffer
    if err := subs.WriteToSRT(&buf); err != nil {
        return nil, fmt.Errorf("srt write: %w", err)
    }
    
    return buf.Bytes(), nil
}

// ExportSRTForWebFallback — спрощений SRT для веб-плеєрів
func (p *SubtitleExporter) ExportSRTForWebFallback(subs *astisub.Subtitles) ([]byte, error) {
    // 1. Видаляємо складні стилі (залишаємо тільки <b>, <i>, <u>)
    for _, item := range subs.Items {
        for _, line := range item.Lines {
            for i := range line.Items {
                if style := line.Items[i].InlineStyle; style != nil {
                    // Зберігаємо тільки базові стилі
                    style.SRTColor = nil  // видаляємо <font color>
                    // залишаємо Bold/Italics/Underline
                }
            }
        }
    }
    
    // 2. Експорт
    var buf bytes.Buffer
    if err := subs.WriteToSRT(&buf); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"????" замість арабського тексту** | Файл не у UTF-8 | Завжди валідуйте `utf8.ValidString()` перед парсингом; конвертуйте вхідні дані у UTF-8 |
| **Таймінги не парсяться** | Неправильний формат часу | Використовуйте `HH:MM:SS,mmm` з комою/крапкою; уникайте 4 цифр для мс |
| **Стилі "просочуються" між субтитрами** | Незакриті теги `<i>` без `</i>` | Astisub скидає стилі між Items, але краще закривати теги для надійності |
| **Колір не відображається у плеєрі** | Плеєр не підтримує `<font color>` | Використовуйте тільки `<b>`, `<i>`, `<u>` для максимальної сумісності |
| **Пусті субтитри після парсингу** | Неправильний роздільник рядків | Переконайтеся, що між субтитрами є пустий рядок (`\n\n`) |
| **BOM ламає парсинг** | Перший субтитр не розпізнається | Astisub автоматично видаляє BOM, але переконайтеся, що файл дійсно має UTF-8 BOM якщо потрібен |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування HTML-екранування:

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

### 2. Пакетний парсинг таймінгів:

```go
// Замість індивідуального parseDurationSRT для кожного таймінгу:
func batchParseSRTTimestamps(timestamps []string) ([]time.Duration, error) {
    results := make([]time.Duration, len(timestamps))
    for i, ts := range timestamps {
        d, err := parseDurationSRT(ts)
        if err != nil {
            return nil, err
        }
        results[i] = d
    }
    return results, nil
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
// ✅ 1. Валідація UTF-8 перед парсингом
if !utf8.Valid(srtData) {
    log.Warn("SRT not in UTF-8, attempting conversion")
    if converted, err := convertToUTF8(srtData); err == nil {
        srtData = converted
    } else {
        return fmt.Errorf("invalid encoding: %w", err)
    }
}

// ✅ 2. Парсинг без проміжних файлів
reader := bytes.NewReader(srtData)
subs, err := astisub.ReadFromSRT(reader)

// ✅ 3. Корекція таймінгів під сегмент
streamOffset := pts.Sub(streamStartTime)
for _, item := range subs.Items {
    item.StartAt += streamOffset
    item.EndAt += streamOffset
}

// ✅ 4. Застосування кольорів для мов (опціонально)
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
if err := validateArabicText(extractTextWithoutTags(subs)); err != nil {
    log.Warn("arabic text validation", "err", err)
}

// ✅ 7. Метрики
monitoring.SRTParsed.Inc()
monitoring.SRTParseLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [SRT Format Spec (Wikipedia)](https://en.wikipedia.org/wiki/SubRip#SRT_file_format) — опис формату
- 💻 [astisub srt.go](https://github.com/asticode/go-astisub/blob/master/srt.go) — вихідний код
- 🎬 [VLC SRT Support](https://wiki.videolan.org/Subtitles/) — підтримка у VLC
- 🧪 [astisub testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади файлів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **реальним часом** та **арабською мовою**:
> 1. **Завжди валідуйте UTF-8** перед парсингом — арабський текст вимагає правильного кодування.
> 2. **Використовуйте прості теги** `<b>`, `<i>`, `<u>` для стилів — вони мають найкращу сумісність.
> 3. **Для кольорів використовуйте `<font color="#RRGGBB">`** — підтримується у VLC, але може ігноруватися у веб-плеєрах.
> 4. **Кешуйте HTML-екранування** — якщо один і той самий текст зустрічається багато разів, не екрануйте його щоразу.
> 5. **Тестуйте round-trip** (SRT → astisub → SRT) для вашого каналу, щоб переконатися, що арабський текст зберігається коректно.

Потрібен приклад функції `convertToUTF8()` для авто-конвертації з Windows-1256 (Arabic) у UTF-8? Готовий допомогти! 🚀