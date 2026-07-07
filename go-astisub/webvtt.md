
# 🧪 Глибокий розбір `astisub/webvtt.go` — реалізація парсингу та запису WebVTT субтитрів

Це **основний модуль** для роботи з форматом WebVTT (Web Video Text Tracks) у бібліотеці `astisub`. Файл реалізує повний цикл: читання/парсинг `.vtt` файлів, обробку метаданих, inline-тегів, голосових міток, конвертацію часових міток для MPEG-TS, та запис назад у валідний WebVTT формат. Розберемо архітектурно:

---

## 🧱 1. Архітектура: сканер-орієнтований парсинг зі станами

### 🔑 Ключові компоненти:

```go
// Constants — ідентифікатори блоків WebVTT
const (
    webvttBlockNameComment        = "comment"   // NOTE блоки
    webvttBlockNameRegion         = "region"    // Region: визначення
    webvttBlockNameStyle          = "style"     // STYLE блоки з CSS
    webvttBlockNameText           = "text"      // Текстові субтитри
    webvttDefaultStyleID          = "astisub-webvtt-default-style-id"
    webvttTimeBoundariesSeparator = "-->"       // Роздільник часу: 00:00:01.000 --> 00:00:03.000
    webvttTimestampMapHeader      = "X-TIMESTAMP-MAP"  // Мапування для MPEG-TS
)

// Regex для парсингу
var (
    webVTTRegexpInlineTimestamp = regexp.MustCompile(`<((?:\d{2,}:)?\d{2}:\d{2}\.\d{3})>`)  // <00:01:01.000>
    webVTTRegexpTag             = regexp.MustCompile(`(</*\s*([^\.\s]+)(\.[^\s/]*)*\s*([^/]*)\s*/*>)`)  // <v Name>, <c.red>, тощо
)
```

### 🔧 Головний цикл `ReadFromWebVTT()`:

```go
func ReadFromWebVTT(i io.Reader) (o *Subtitles, err error) {
    // 1. Ініціалізація
    o = NewSubtitles()
    scanner := newScanner(i)  // ← кастомний сканер для построчного читання
    
    // 2. Пропуск заголовку до "WEBVTT"
    for scanner.Scan() {
        line := scanner.Text()
        if strings.Fields(line)[0] == "WEBVTT" {
            break  // знайдено заголовок
        }
    }
    
    // 3. Основний цикл парсингу по рядках
    var item = &Item{}          // поточний субтитр
    var blockName string        // поточний тип блоку
    var comments []string       // накопичені коментарі
    var sa = &StyleAttributes{} // поточні стилі
    
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        
        switch {
        // Коментар: NOTE текст
        case strings.HasPrefix(line, "NOTE "):
            blockName = webvttBlockNameComment
            comments = append(comments, strings.TrimPrefix(line, "NOTE "))
            
        // Порожній рядок: скидання стану
        case len(line) == 0:
            if blockName != webvttBlockNameStyle || /* CSS завершення */ {
                blockName = ""  // вихід з блоку
            }
            sa.WebVTTTags = []WebVTTTag{}  // скидання тегів
            
        // Регіон: Region: id=fred lines=3 ...
        case strings.HasPrefix(line, "Region: "):
            // Парсинг параметрів регіону → o.Regions[id] = &Region{...}
            
        // Стиль: STYLE блок з CSS
        case strings.HasPrefix(line, "STYLE"):
            blockName = webvttBlockNameStyle
            // Ініціалізація стилів за замовчуванням
            
        // Часові межі: 00:00:01.000 --> 00:00:03.000 align:left
        case strings.Contains(line, webvttTimeBoundariesSeparator):
            blockName = webvttBlockNameText
            // Парсинг StartAt/EndAt, inline-стилів (align, position, region...)
            // Створення нового Item та додавання у o.Items
            
        // Мапування часу: X-TIMESTAMP-MAP=LOCAL:...,MPEGTS:...
        case strings.HasPrefix(line, webvttTimestampMapHeader):
            // Парсинг → o.Metadata.WebVTTTimestampMap = &WebVTTTimestampMap{...}
            
        // Текст субтитру (дефолтний кейс)
        default:
            switch blockName {
            case webvttBlockNameComment:
                comments = append(comments, line)  // накопичення коментарів
            case webvttBlockNameStyle:
                sa.WebVTTStyles = append(sa.WebVTTStyles, line)  // накопичення CSS
            case webvttBlockNameText:
                // Парсинг текстового контенту з тегами
                if l := parseTextWebVTT(line, sa); len(l.Items) > 0 {
                    item.Lines = append(item.Lines, l)
                }
            default:
                // Це ID субтитру (числовий індекс)
                index, _ = strconv.Atoi(line)
            }
        }
    }
    return
}
```

### 🎯 Чому сканер-орієнтований підхід?

```
WebVTT — текстовий формат з чіткою структурою рядків:
• Кожен рядок — окремий логічний елемент (коментар, регіон, таймінг, текст)
• Порожні рядки розділяють блоки
• Інкрементальне читання дозволяє обробляти великі файли без завантаження в пам'ять

Переваги:
1. Низьке споживання пам'яті: обробка по рядках, не весь файл одразу
2. Стійкість до помилок: помилка в одному рядку не ламає весь парсинг
3. Легкість дебагу: кожен рядок можна логувати окремо
4. Підтримка streaming: можна читати з network stream без буферизації

У вашому CCTV HLS Processor: це дозволяє обробляти субтитри з камер у реальному часі,
не чекаючи завантаження всього файлу субтитрів.
```

---

## ⏱️ 2. `WebVTTTimestampMap` — мапування часу для синхронізації з MPEG-TS

### 🔍 Структура та формули:

```go
type WebVTTTimestampMap struct {
    Local  time.Duration  // WebVTT час (відносний, у форматі годинник)
    MpegTS int64          // MPEG-TS час (абсолютний, у ticks @ 90 kHz)
}

// Offset(): конвертація у зсув часу
func (t *WebVTTTimestampMap) Offset() time.Duration {
    if t == nil {
        return 0
    }
    // Формула: offset = (MpegTS / 90000) секунд - Local
    return time.Duration(t.MpegTS)*time.Second/90000 - t.Local
}

// String(): серіалізація у формат заголовку
func (t *WebVTTTimestampMap) String() string {
    mpegts := fmt.Sprintf("MPEGTS:%d", t.MpegTS)
    local := fmt.Sprintf("LOCAL:%s", formatDurationWebVTT(t.Local))
    return fmt.Sprintf("%s=%s,%s", webvttTimestampMapHeader, local, mpegts)
}
```

### 🔧 `parseWebVTTTimestampMap()` — парсинг заголовку:

```go
func parseWebVTTTimestampMap(line string) (*WebVTTTimestampMap, error) {
    // Розбиття: "X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:180000"
    splits := strings.Split(line, "=")
    if len(splits) <= 1 {
        return nil, errors.New("invalid X-TIMESTAMP-MAP, no '=' found")
    }
    right := splits[1]  // "LOCAL:00:00:00.000,MPEGTS:180000"
    
    var local time.Duration
    var mpegts int64
    
    // Парсинг ключ-значення пар, розділених комами
    for _, split := range strings.Split(right, ",") {
        pairs := strings.SplitN(split, ":", 2)  // ["LOCAL", "00:00:00.000"]
        if len(pairs) <= 1 {
            return nil, fmt.Errorf("invalid part %q didn't contain ':'", split)
        }
        
        switch strings.ToLower(strings.TrimSpace(pairs[0])) {
        case "local":
            local, err = parseDurationWebVTT(pairs[1])  // "00:00:00.000" → time.Duration
        case "mpegts":
            mpegts, err = strconv.ParseInt(pairs[1], 10, 0)  // "180000" → int64
        }
    }
    
    return &WebVTTTimestampMap{Local: local, MpegTS: mpegts}, nil
}
```

### 🎯 Практичне застосування у CCTV:

```
Сценарій: камера передає субтитри у WebVTT форматі з синхронізацією до відео.

Вхідний WebVTT:
WEBVTT
X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:180000

00:00:01.000 --> 00:00:03.000
Текст субтитру

Розрахунок:
• LOCAL:00:00:00.000 → 0 секунд
• MPEGTS:180000 → 180000 ticks @ 90 kHz = 2 секунди
• offset = 2s - 0s = 2 секунди

Конвертація субтитру:
• WebVTT start: 00:00:01.000 → 1 секунда
• Абсолютний PTS = 1s + offset = 1s + 2s = 3 секунди
• У ticks @ 90 kHz: 3 × 90000 = 270000

У вашому пайплайні:
1. Парсинг заголовку → WebVTTTimestampMap
2. Для кожного субтитру: absolutePTS = webvttPTS + timestampMap.Offset()
3. Використання absolutePTS для синхронізації з відео у HLS сегментах

Без цього: субтитри будуть відставати/випереджати відео на 2 секунди!
```

---

## 🎨 3. `parseTextWebVTT()` — парсинг текстового контенту з тегами

### 🔧 Використання HTML tokenizer для надійності:

```go
func parseTextWebVTT(i string, sa *StyleAttributes) (o Line) {
    // Створення токенайзера для парсингу HTML-подібних тегів
    tr := html.NewTokenizer(strings.NewReader(i))
    
    for {
        t := tr.Next()  // отримати наступний токен
        if err := tr.Err(); err != nil {
            break  // кінець або помилка
        }
        
        switch t {
        case html.EndTagToken:
            // Закриваючий тег: </i>, </v>, тощо
            if len(sa.WebVTTTags) > 0 {
                // Видалити останній тег зі стеку (LIFO)
                sa.WebVTTTags = sa.WebVTTTags[:len(sa.WebVTTTags)-1]
            }
            
        case html.StartTagToken:
            // Відкриваючий тег: <i>, <v Name>, <c.red>, тощо
            if matches := webVTTRegexpTag.FindStringSubmatch(string(tr.Raw())); len(matches) > 4 {
                tagName := matches[2]  // назва тегу: "i", "v", "c"
                
                // Парсинг класів: .red.bg_blue → ["red", "bg_blue"]
                var classes []string
                if matches[3] != "" {
                    classes = strings.Split(strings.Trim(matches[3], "."), ".")
                }
                
                // Парсинг анотації: <v Bob> → "Bob"
                annotation := ""
                if matches[4] != "" {
                    annotation = strings.TrimSpace(matches[4])
                }
                
                // Спеціальна обробка голосових міток <v Name>
                if tagName == "v" {
                    if o.VoiceName == "" {
                        // Зберегти тільки першу голосову мітку у рядку
                        o.VoiceName = annotation
                    } else {
                        // Ігнорувати наступні <v> (можна логувати)
                        log.Printf("astisub: found another voice name %q in %q. Ignore", annotation, i)
                    }
                    continue  // не додавати <v> у стек тегів
                }
                
                // Додати тег у стек для застосування до наступного тексту
                sa.WebVTTTags = append(sa.WebVTTTags, WebVTTTag{
                    Name:       tagName,
                    Classes:    classes,
                    Annotation: annotation,
                })
            }
            
        case html.TextToken:
            // Текстовий контент між тегами
            var styleAttributes *StyleAttributes
            if len(sa.WebVTTTags) > 0 {
                // Копіювати поточний стек тегів для цього текстового фрагменту
                tags := make([]WebVTTTag, len(sa.WebVTTTags))
                copy(tags, sa.WebVTTTags)
                styleAttributes = &StyleAttributes{WebVTTTags: tags}
                styleAttributes.propagateWebVTTAttributes()  // конвертація у TTML
            }
            
            // Парсинг тексту з інлайн-таймштампами
            o.Items = append(o.Items, parseTextWebVTTTextToken(styleAttributes, string(tr.Raw()))...)
        }
    }
    return
}
```

### 🔧 `parseTextWebVTTTextToken()` — обробка інлайн-таймштампів:

```go
func parseTextWebVTTTextToken(sa *StyleAttributes, line string) (ret []LineItem) {
    // Пошук всіх інлайн-таймштампів: <00:01:01.000>
    indexes := webVTTRegexpInlineTimestamp.FindAllStringSubmatchIndex(line, -1)
    
    if len(indexes) == 0 {
        // Немає таймштампів → один елемент з усім текстом
        return []LineItem{{
            InlineStyle: sa,
            Text:        unescapeHTML(line),  // &lt; → <, &amp; → &, тощо
        }}
    }
    
    // Текст до першого таймштампу
    if s := line[:indexes[0][0]]; strings.TrimSpace(s) != "" {
        ret = append(ret, LineItem{
            InlineStyle: sa,
            Text:        unescapeHTML(s),
        })
    }
    
    // Обробка кожного таймштампу
    for i, match := range indexes {
        // Текст між поточним та наступним таймштампом
        endIndex := len(line)
        if i+1 < len(indexes) {
            endIndex = indexes[i+1][0]
        }
        s := line[match[1]:endIndex]
        if strings.TrimSpace(s) == "" {
            continue  // пропустити порожній текст
        }
        
        // Парсинг таймштампу: "00:01:01.000" → time.Duration
        t, err := parseDurationWebVTT(line[match[2]:match[3]])
        if err != nil {
            log.Printf("astisub: parsing webvtt duration %s failed, ignoring: %v", 
                line[match[2]:match[3]], err)
            continue  // ігнорувати невалідний таймштамп
        }
        
        ret = append(ret, LineItem{
            InlineStyle: sa,
            StartAt:     t,  // відносний час від початку субтитру
            Text:        unescapeHTML(s),
        })
    }
    return
}
```

### 🎯 Чому HTML tokenizer замість простого regex?

```
WebVTT теги схожі на HTML, але мають особливості:
• Вкладеність: <i>italic <b>bold</b> italic</i>
• Опціональні закриваючі теги: <v Bob>текст (без </v>)
• Невалідні теги: <v Bob>текст</vi> (помилковий закриваючий тег)

Простий regex не впорається з:
• Правильним управлінням стеком вкладених тегів
• Ігноруванням невідповідних закриваючих тегів
• Обробкою тексту з `<` та `>` символами (наприклад, математичні формули)

HTML tokenizer у Go:
• Вже реалізує правильний парсинг вкладених тегів
• Ігнорує невідповідні закриваючі теги
• Автоматично екранує спеціальні символи

У вашому коді: це забезпечує стійкість до "брудних" WebVTT файлів від реальних камер,
де теги можуть бути неповними або помилковими.
```

---

## ✍️ 4. `WriteToWebVTT()` — серіалізація субтитрів у валідний WebVTT

### 🔧 Структура генерації:

```go
func (s Subtitles) WriteToWebVTT(o io.Writer) (err error) {
    // 1. Перевірка: чи є субтитри для запису
    if len(s.Items) == 0 {
        return ErrNoSubtitlesToWrite
    }
    
    // 2. Заголовок та метадані
    c := append([]byte{}, []byte("WEBVTT")...)
    
    // X-TIMESTAMP-MAP, якщо встановлено
    if s.Metadata != nil && s.Metadata.WebVTTTimestampMap != nil {
        c = append(c, []byte("\n"+s.Metadata.WebVTTTimestampMap.String())...)
    }
    c = append(c, []byte("\n\n")...)
    
    // STYLE блоки, якщо є
    if len(style) > 0 {
        c = append(c, []byte(fmt.Sprintf("STYLE\n%s\n\n", strings.Join(style, "\n")))...)
    }
    
    // 3. Регіони (сортування за ID для детермінованого виводу)
    sort.Strings(k)  // k := []string{region IDs}
    for _, id := range k {
        // Форматування: Region: id=fred lines=3 regionanchor=0%,100% ...
        c = append(c, []byte("Region: id="+s.Regions[id].ID)...)
        // ... додавання інших параметрів регіону ...
        c = append(c, bytesLineSeparator...)
    }
    
    // 4. Основний цикл по субтитрах
    for index, item := range s.Items {
        // Коментарі (NOTE блоки)
        if len(item.Comments) > 0 {
            c = append(c, []byte("NOTE ")...)
            for _, comment := range item.Comments {
                c = append(c, []byte(comment+ "\n")...)
            }
        }
        
        // Індекс та часові межі
        c = append(c, []byte(strconv.Itoa(index+1) + "\n")...)  // 1-based indexing
        c = append(c, []byte(formatDurationWebVTT(item.StartAt))...)
        c = append(c, bytesWebVTTTimeBoundariesSeparator...)  // " --> "
        c = append(c, []byte(formatDurationWebVTT(item.EndAt))...)
        
        // Inline-стилі: align:left position:10%,line-left size:35%
        if item.InlineStyle != nil {
            if item.InlineStyle.WebVTTAlign != "" {
                c = append(c, []byte(" align:"+item.InlineStyle.WebVTTAlign)...)
            }
            // ... інші стилі: line, position, region, size, vertical ...
        }
        c = append(c, bytesLineSeparator...)  // новий рядок після таймінгів
        
        // Текстовий контент з тегами
        for _, l := range item.Lines {
            c = append(c, l.webVTTBytes()...)  // ← рекурсивна серіалізація рядка
        }
        c = append(c, bytesLineSeparator...)  // новий рядок після тексту
    }
    
    // 5. Видалення останнього переносу рядка та запис
    c = c[:len(c)-1]
    _, err = o.Write(c)
    return
}
```

### 🔧 `Line.webVTTBytes()` та `LineItem.webVTTBytes()` — серіалізація з оптимізацією тегів:

```go
func (l Line) webVTTBytes() (c []byte) {
    // Голосова мітка на початку рядка
    if l.VoiceName != "" {
        c = append(c, []byte("<v "+l.VoiceName+">")...)
    }
    
    // Серіалізація кожного текстового фрагменту з урахуванням сусідніх елементів
    for idx := 0; idx < len(l.Items); idx++ {
        var previous, next *LineItem
        if idx > 0 { previous = &l.Items[idx-1] }
        if idx < len(l.Items)-1 { next = &l.Items[idx+1] }
        
        c = append(c, l.Items[idx].webVTTBytes(previous, next)...)
    }
    c = append(c, bytesLineSeparator...)  // перенос рядка в кінці
    return
}

func (li LineItem) webVTTBytes(previous, next *LineItem) (c []byte) {
    // 1. Інлайн-таймштамп, якщо є
    if li.StartAt > 0 {
        c = append(c, []byte("<"+formatDurationWebVTT(li.StartAt)+">")...)
    }
    
    // 2. Колір: або з WebVTT тегів <c.red>, або з TTML конвертації
    var color string
    var hasColorTags bool
    if li.InlineStyle != nil {
        // Перевірка наявності WebVTT color тегів
        for _, tag := range li.InlineStyle.WebVTTTags {
            if tag.Name == "c" { hasColorTags = true; break }
        }
        // Використання TTMLColor тільки якщо немає WebVTT тегів
        if !hasColorTags && li.InlineStyle.TTMLColor != nil {
            color = li.InlineStyle.TTMLColor.WebVTTString()
        }
    }
    if color != "" {
        c = append(c, []byte("<c."+color+">")...)
    }
    
    // 3. Відкриваючі теги: оптимізація — не відкривати, якщо вже відкрито у previous
    if li.InlineStyle != nil {
        for idx, tag := range li.InlineStyle.WebVTTTags {
            if previous != nil && previous.InlineStyle != nil && 
               len(previous.InlineStyle.WebVTTTags) > idx && 
               tag.Name == previous.InlineStyle.WebVTTTags[idx].Name {
                continue  // тег вже відкритий, не дублювати
            }
            c = append(c, []byte(tag.startTag())...)  // <i>, <b>, <c.red>, тощо
        }
    }
    
    // 4. Текст з екрануванням спецсимволів
    c = append(c, []byte(escapeHTML(li.Text))...)  // < → &lt;, & → &amp;, тощо
    
    // 5. Закриваючі теги: оптимізація — не закривати, якщо продовжується у next
    if li.InlineStyle != nil {
        for i := len(li.InlineStyle.WebVTTTags) - 1; i >= 0; i-- {  // зворотній порядок для вкладеності
            tag := li.InlineStyle.WebVTTTags[i]
            if next != nil && next.InlineStyle != nil && 
               len(next.InlineStyle.WebVTTTags) > i && 
               tag.Name == next.InlineStyle.WebVTTTags[i].Name {
                continue  // тег продовжується, не закривати
            }
            c = append(c, []byte(tag.endTag())...)  // </i>, </b>, </c>, тощо
        }
    }
    
    // 6. Закриття color тегу, якщо був відкритий
    if color != "" {
        c = append(c, []byte("</c>")...)
    }
    return
}
```

### 🎯 Чому оптимізація тегів критична?

```
Без оптимізації: кожен текстовий фрагмент має повний набір тегів
<i>italic</i> <i>more italic</i> → <i>italic</i><i>more italic</i>

З оптимізацією: теги відкриваються/закриваються тільки при зміні
<i>italic more italic</i> → один тег замість двох

Переваги:
1. Менший розмір файлу: особливо важливо для мережевої передачі субтитрів
2. Краща читабельність: людям легше редагувати оптимізований WebVTT
3. Сумісність: деякі парсери можуть мати проблеми з надмірною вкладеністю тегів

У вашому пайплайні: це зменшує bandwidth для передачі субтитрів у HLS,
особливо важливо для мобільних мереж з обмеженою пропускною здатністю.
```

---

## 🐞 5. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Відсутня валідація `X-TIMESTAMP-MAP` порядку параметрів**:
   ```go
   // Специфікація дозволяє будь-який порядок: LOCAL:...,MPEGTS:... або навпаки
   // Але ваш код працює з будь-яким порядком ✓
   // Однак: String() завжди генерує "LOCAL:...,MPEGTS:..." (канонічний порядок)
   // Це може призвести до відмінностей у roundtrip тестах, якщо вхідний файл мав інший порядок
   ```

2. **Необроблений випадок порожнього `WebVTTPosition`**:
   ```go
   func newWebVTTPosition(s string) *WebVTTPosition {
       if s == "" { return nil }
       parts := strings.Split(s, ",")
       if len(parts) != 2 {
           return &WebVTTPosition{XPosition: strings.TrimSpace(s)}  // ← але Alignment порожній!
       }
       // ...
   }
   // Якщо s = "10%" (без Alignment) → повертається Position з порожнім Alignment
   // Але String() перевіряє Alignment != "" → повертає тільки XPosition ✓
   // Однак: краще документувати цю поведінку або додати валідацію
   ```

3. **Витік пам'яті у `parseTextWebVTT()` при глибокій вкладеності**:
   ```go
   // При кожному текстовому токені:
   tags := make([]WebVTTTag, len(sa.WebVTTTags))
   copy(tags, sa.WebVTTTags)
   // Якщо вкладеність тегів велика (наприклад, 100 рівнів), копіювання стає дорогим
   // Краще: використовувати посилання або immutable структуру для тегів
   ```

4. **Некоректна обробка невалідних таймштампів у `parseTextWebVTTTextToken()`**:
   ```go
   t, err := parseDurationWebVTT(line[match[2]:match[3]])
   if err != nil {
       log.Printf("astisub: parsing webvtt duration %s failed, ignoring: %v", ...)
       continue  // ігноруємо таймштамп, але текст залишається
   }
   // Але якщо таймштамп невалідний, текст після нього може мати неправильний StartAt
   // Краще: або повертати помилку, або встановлювати StartAt = 0 для цього фрагменту
   ```

5. **Відсутня підтримка нових тегів у `webVTTRegexpTag`**:
   ```go
   // Регулярний вираз: `(</*\s*([^\.\s]+)(\.[^\s/]*)*\s*([^/]*)\s*/*>)`
   // Підтримує: <tag>, <tag.class>, <tag annotation>, </tag>
   // Але не підтримує: <tag.class1.class2> (тільки перший клас у matches[3])
   // matches[3] = ".class1.class2" → strings.Split(..., ".") → ["class1", "class2"] ✓
   // Однак: краще додати тест на множину класів для валідації
   ```

6. **`log.Printf` замість повернення помилок**:
   ```go
   // У парсингу: log.Printf("astisub: found another voice name...")
   // У продакшені це може залишитись непоміченим, а дані будуть втрачені
   // Краще: додати поле `Warnings []error` у Subtitles або повертати помилки для критичних випадків
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечної конвертації позиції
func (p *WebVTTPosition) Validate() error {
    if p.XPosition == "" {
        return errors.New("WebVTTPosition: XPosition cannot be empty")
    }
    // Додати валідацію формату: "10%", "50px", тощо
    return nil
}

// 2. Оптимізація копіювання тегів через sync.Pool
var tagSlicePool = sync.Pool{
    New: func() interface{} {
        return make([]WebVTTTag, 0, 10)  // початкова ємність для типової вкладеності
    },
}

func getTagSlice() []WebVTTTag {
    return tagSlicePool.Get().([]WebVTTTag)[:0]  // скинути довжину, зберегти ємність
}

func putTagSlice(tags []WebVTTTag) {
    tagSlicePool.Put(tags[:0])  // скинути перед поверненням у пул
}

// 3. Метрики для моніторингу парсингу
func (s *Subtitles) recordParseMetrics(lineNum int, blockName string, err error) {
    if err != nil {
        metrics.WebVTTParseErrors.WithLabelValues(blockName, err.Error()).Inc()
        return
    }
    metrics.WebVTTBlocksParsed.WithLabelValues(blockName).Inc()
    metrics.WebVTTLineProcessed.Observe(float64(lineNum))
}

// 4. Юніт-тести для оптимізації тегів
func TestLineItemWebVTTBytes_TagOptimization(t *testing.T) {
    // Два сусідніх елементи з однаковими тегами
    previous := &LineItem{
        InlineStyle: &StyleAttributes{WebVTTTags: []WebVTTTag{{Name: "i"}}},
        Text: "italic ",
    }
    current := &LineItem{
        InlineStyle: &StyleAttributes{WebVTTTags: []WebVTTTag{{Name: "i"}}},
        Text: "more italic",
    }
    
    result := current.webVTTBytes(previous, nil)
    expected := []byte("<i>more italic</i>")  // без дублювання <i>
    assert.Equal(t, expected, result)
}

// 5. Підтримка множини класів у регулярному виразі
// Поточний regex вже підтримує .class1.class2 у matches[3]
// Але краще додати тест:
func TestWebVTTRegexpTag_MultipleClasses(t *testing.T) {
    input := `<c.red.bg_blue.yellow>text</c>`
    matches := webVTTRegexpTag.FindStringSubmatch(input)
    require.Len(t, matches, 5)
    assert.Equal(t, "c", matches[2])  // tag name
    assert.Equal(t, ".red.bg_blue.yellow", matches[3])  // classes
    classes := strings.Split(strings.Trim(matches[3], "."), ".")
    assert.Equal(t, []string{"red", "bg_blue", "yellow"}, classes)
}
```

---

## 🎯 6. Інтеграція з вашим CCTV HLS Processor

### 📍 У `SubtitleImporter` — імпорт WebVTT субтитрів:

```go
type SubtitleImporter struct {
    timestampMap *astisub.WebVTTTimestampMap
}

func (imp *SubtitleImporter) ImportWebVTT(filePath string) error {
    file, err := os.Open(filePath)
    if err != nil { return fmt.Errorf("open WebVTT file: %w", err) }
    defer file.Close()
    
    subs, err := astisub.ReadFromWebVTT(file)
    if err != nil {
        return fmt.Errorf("parse WebVTT: %w", err)
    }
    
    // Збереження мапування часу для конвертації
    if subs.Metadata != nil {
        imp.timestampMap = subs.Metadata.WebVTTTimestampMap
    }
    
    // Конвертація у внутрішній формат
    for _, item := range subs.Items {
        // Конвертація відносного часу у абсолютний PTS
        var absoluteStart, absoluteEnd time.Duration
        if imp.timestampMap != nil {
            absoluteStart = item.StartAt + imp.timestampMap.Offset()
            absoluteEnd = item.EndAt + imp.timestampMap.Offset()
        } else {
            absoluteStart = item.StartAt
            absoluteEnd = item.EndAt
        }
        
        // Екстракція тексту з підтримкою інлайн-таймштампів
        for _, line := range item.Lines {
            for _, textItem := range line.Items {
                frame := &SubtitleFrame{
                    StartPTS: convertDurationToPTS(absoluteStart + textItem.StartAt),
                    EndPTS:   convertDurationToPTS(absoluteStart + textItem.StartAt + getDurationFromText(textItem.Text)),
                    Text:     textItem.Text,
                    Voice:    line.VoiceName,
                    Styles:   convertStyleAttributes(textItem.InlineStyle),
                }
                imp.subtitleQueue <- frame
            }
        }
    }
    return nil
}
```

### 📍 У `HLSGenerator` — генерація WebVTT для плейлиста:

```go
func (gen *HLSSubtitleGenerator) WriteWebVTTSegment(frames []*SubtitleFrame, outputPath string) error {
    // Конвертація внутрішніх кадрів у формат astisub
    subs := &astisub.Subtitles{
        Meta &astisub.Metadata{
            WebVTTTimestampMap: gen.timestampMap,  // для синхронізації з відео
        },
    }
    
    for i, frame := range frames {
        // Конвертація абсолютного PTS → відносний час WebVTT
        var relativeStart, relativeEnd time.Duration
        if gen.timestampMap != nil {
            relativeStart = frame.StartPTS - gen.timestampMap.Offset()
            relativeEnd = frame.EndPTS - gen.timestampMap.Offset()
        } else {
            relativeStart = frame.StartPTS
            relativeEnd = frame.EndPTS
        }
        
        item := &astisub.Item{
            StartAt: relativeStart,
            EndAt: relativeEnd,
            Index: i,
            Lines: []*astisub.Line{
                {
                    Items: []*astisub.LineItem{
                        {
                            Text: frame.Text,
                            InlineStyle: convertStylesToWebVTT(frame.Styles),
                        },
                    },
                    VoiceName: frame.Voice,
                },
            },
        }
        subs.Items = append(subs.Items, item)
    }
    
    // Запис у файл
    file, err := os.Create(outputPath)
    if err != nil {
        return fmt.Errorf("create WebVTT file: %w", err)
    }
    defer file.Close()
    
    return subs.WriteToWebVTT(file)
}
```

### 📍 У метриках — моніторинг якості парсингу:

```go
func (imp *SubtitleImporter) recordParseMetrics(subs *astisub.Subtitles, err error) {
    if err != nil {
        metrics.SubtitleParseErrors.WithLabelValues("WebVTT", err.Error()).Inc()
        return
    }
    
    metrics.SubtitleItemsParsed.WithLabelValues("WebVTT").Add(float64(len(subs.Items)))
    
    // Статистика по функціональності
    var voiceCount, timestampCount, regionCount int
    for _, item := range subs.Items {
        if item.Lines[0].VoiceName != "" { voiceCount++ }
        for _, line := range item.Lines {
            for _, textItem := range line.Items {
                if textItem.StartAt > 0 { timestampCount++ }
            }
        }
        if item.Region != nil { regionCount++ }
    }
    metrics.SubtitleFeaturesUsed.WithLabelValues("voice").Add(float64(voiceCount))
    metrics.SubtitleFeaturesUsed.WithLabelValues("inline-timestamp").Add(float64(timestampCount))
    metrics.SubtitleFeaturesUsed.WithLabelValues("region").Add(float64(regionCount))
    
    // Розмір тексту (для оптимізації буферів)
    totalTextSize := 0
    for _, item := range subs.Items {
        for _, line := range item.Lines {
            for _, textItem := range line.Items {
                totalTextSize += len(textItem.Text)
            }
        }
    }
    metrics.SubtitleTextSize.Observe(float64(totalTextSize))
}
```

---

## 🧭 Висновок: чому цей модуль — фундамент підтримки WebVTT

| Компонент | Роль у WebVTT обробці | Вартість помилки без нього |
|-----------|---------------------|---------------------------|
| **Сканер-орієнтований парсинг** | Інкрементальна обробка великих файлів без завантаження в пам'ять | OOM для великих субтитрів, неможливість streaming-обробки |
| **HTML tokenizer для тегів** | Надійний парсинг вкладених/неповних тегів | Помилки при обробці "брудних" субтитрів з реальних камер |
| **WebVTTTimestampMap** | Синхронізація субтитрів з відео у MPEG-TS/HLS | Розсинхронізація аудіо/відео/субтитрів → "не в попадання губ" |
| **Оптимізація тегів при записі** | Зменшення розміру файлу, краща сумісність | Зайве споживання bandwidth, проблеми з деякими плеєрами |
| **propagateWebVTTAttributes()** | Автоматична конвертація WebVTT → TTML стилів | Втрата інформації про позиціонування/кольори при експорті у інші формати |

> 🔑 **Головна ідея**: Цей код — **універсальний адаптер** для формату WebVTT, який абстрагує складність парсингу, валідації та серіалізації від бізнес-логіки вашого пайплайну. Без нього кожен компонент змушений був би реалізовувати власну логіку роботи з WebVTT, що призвело б до дублювання коду, помилок у синхронізації та важкої підтримки.

💡 **Фінальна порада**: 
1. Додайте валідацію `WebVTTPosition` та інших структурних полів
2. Оптимізуйте копіювання тегів через `sync.Pool` для глибокої вкладеності
3. Замініть `log.Printf` на структуроване логування або повернення попереджень
4. Додайте юніт-тести для оптимізації тегів та множини класів
5. Реалізуйте метрики для моніторингу якості парсингу у продакшені

Це перетворить цей модуль з "робочої реалізації" на "гарантовано надійний фундамент" для підтримки WebVTT субтитрів у вашому CCTV HLS Processor.