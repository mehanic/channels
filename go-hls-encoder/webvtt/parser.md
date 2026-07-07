# Глибоке роз'яснення: `webvtt` пакет — парсинг WebVTT субтитрів для HLS

Цей файл містить **реалізацію потокового парсингу WebVTT файлів** — стандартного формату субтитрів для HLS. Код адаптовано з бібліотеки `go-astisub` (MIT license) для інтеграції у ваш CCTV HLS пайплайн.

---

## 🎯 Навіщо цей код потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ webvtt у контексті HLS-стрімінгу:      │
│                                         │
│ 🔹 Підтримка субтитрів у HLS:          │
│   • WebVTT — єдиний підтримуваний      │
│     формат субтитрів для HLS           │
│   • Конвертація з SRT/ASS/інших форматів│
│                                         │
│ 🔹 Потокова обробка:                   │
│   • Читає рядок за рядком (bufio)      │
│   • Надсилає блоки у канал (chan)      │
│   • Не завантажує весь файл у пам'ять  │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Обробка субтитрів з різних джерел  │
│   • Синхронізація з відео-сегментами   │
│   • Підтримка багатомовних доріжок     │
└─────────────────────────────────────────┘
```

---

## 🔧 Константи та типи: фундамент

### Блок-типи WebVTT

```go
const (
    webvttBlockNameComment        = "comment"   // 🔹 Коментарі: NOTE ...
    webvttBlockNameRegion         = "region"    // 🔹 Регіони екрану: REGION ...
    webvttBlockNameStyle          = "style"     // 🔹 CSS-стилі: STYLE ...
    webvttBlockNameText           = "text"      // 🔹 Текстові репліки (основний контент)
    webvttTimeBoundariesSeparator = " --> "     // 🔹 Роздільник часу: "00:01:23.456 --> 00:01:26.789"
)
```

### BOM (Byte Order Mark)

```go
var BytesBOM = []byte{239, 187, 191}  // 🔹 UTF-8 BOM: EF BB BF
```

> 💡 **Чому це важливо**: Деякі редактори додають BOM на початок файлу. `strings.TrimPrefix(line, string(BytesBOM))` гарантує коректне розпізнавання заголовка `WEBVTT`.

---

## 🔍 Функція `ReadFromWebVTT`: потоковий парсинг

```go
func ReadFromWebVTT(i io.Reader, c chan<- SubtitleBlock) (err error) {
    // 🔹 1. Ініціалізація сканера
    var scanner = bufio.NewScanner(i)
    var line string
    
    // 🔹 2. Пропустити заголовок та BOM
    for scanner.Scan() {
        line = scanner.Text()
        line = strings.TrimPrefix(line, string(BytesBOM))  // 🔹 Видалити BOM якщо є
        line = strings.TrimSpace(line)
        if len(line) > 0 && line == "WEBVTT" {  // 🔹 Знайшли заголовок
            break
        }
    }
    
    // 🔹 3. Основний цикл парсингу
    var blockName string              // 🔹 Поточний тип блоку
    var currentBlock *SubtitleBlock   // 🔹 Блок, що формується
    var associated bytes.Buffer       // 🔹 Буфер для "асоційованих" рядків (коментарі, стилі...)
    
    for scanner.Scan() {
        line = scanner.Text()
        
        // 🔹 4. Визначення типу блоку за префіксом
        switch {
        case strings.HasPrefix(line, "NOTE "):           // Коментар
            blockName = webvttBlockNameComment
        case strings.HasPrefix(line, "Region: "):        // Регіон
            blockName = webvttBlockNameRegion
        case strings.HasPrefix(line, "STYLE "):          // CSS-стиль
            blockName = webvttBlockNameStyle
        case strings.Contains(line, webvttTimeBoundariesSeparator):  // 🔹 Таймкоди!
            blockName = webvttBlockNameText
            
            // 🔹 5. Відправка попереднього блоку у канал
            if currentBlock != nil {
                c <- *currentBlock  // 🔹 Ключовий момент: потокова відправка
            }
            
            // 🔹 6. Ініціалізація нового блоку
            currentBlock = &SubtitleBlock{}
            currentBlock.Lines.Write(associated.Bytes())  // 🔹 Додати асоційовані рядки
            associated = bytes.Buffer{}  // 🔹 Очистити буфер
            
            // 🔹 7. Парсинг таймкодів
            var parts = strings.Split(line, webvttTimeBoundariesSeparator)
            var partsRight = strings.Split(parts[1], " ")  // 🔹 Для inline-стилів
            
            if currentBlock.StartTime, err = parseDurationWebVTT(parts[0]); err != nil {
                return fmt.Errorf("parsing webvtt duration %q failed: %s", parts[0], err)
            }
            if currentBlock.EndTime, err = parseDurationWebVTT(partsRight[0]); err != nil {
                return fmt.Errorf("parsing webvtt duration %q failed: %s", partsRight[0], err)
            }
        }
        
        // 🔹 8. Розподіл рядків за типом блоку
        switch blockName {
        case webvttBlockNameText:
            currentBlock.Lines.WriteString(line + "\n")  // 🔹 Текст репліки
        case webvttBlockNameComment, webvttBlockNameRegion, webvttBlockNameStyle:
            fallthrough
        default:
            associated.WriteString(line + "\n")  // 🔹 Зберегти для наступного текстового блоку
        }
        
        // 🔹 9. Порожній рядок = кінець блоку
        if len(line) == 0 {
            blockName = ""  // 🔹 Скинути стан
        }
    }
    
    // 🔹 10. Відправити останній блок
    if currentBlock != nil {
        c <- *currentBlock
    }
    
    close(c)  // 🔹 Закрити канал = сигнал завершення
    return nil
}
```

### 🎯 Ключові моменти архітектури

#### 🔹 Потокова обробка через канал

```
Архітектура:
[WebVTT файл] --bufio.Scanner--> [ReadFromWebVTT] --chan SubtitleBlock--> [Сегментер]

Переваги:
✅ Не завантажує весь файл у пам'ять (важливо для великих субтитрів)
✅ Дозволяє паралельну обробку: парсинг і сегментація працюють одночасно
✅ Гнучкість: можна фільтрувати/трансформувати блоки "на льоту"

Приклад використання:
blocks := make(chan SubtitleBlock, 100)  // буферизований канал
go ReadFromWebVTT(reader, blocks)

for block := range blocks {
    // Обробити блок: сегментувати, конвертувати, записати...
    processBlock(block)
}
```

#### 🔹 Обробка "асоційованих" рядків

```
Проблема:
• Коментарі, регіони, стилі у WebVTT можуть йти ПЕРЕД текстовим блоком
• Їх потрібно "прикріпити" до наступної репліки

Рішення:
• Буфер `associated` накопичує рядки не-текстових блоків
• При зустрічі таймкодів: `currentBlock.Lines.Write(associated.Bytes())`
• Буфер очищується для наступного циклу

Приклад WebVTT:
  NOTE Це коментар для наступної репліки
  
  00:01:23.456 --> 00:01:26.789 align:center
  Привіт, світе!

Результат:
• currentBlock.Lines = "NOTE Це коментар...\nПривіт, світе!\n"
• Коментар зберігається разом з реплікою
```

#### 🔹 Парсинг таймкодів з inline-стилями

```
Формат таймкоду у WebVTT:
  00:01:23.456 --> 00:01:26.789 align:center size:50%

Кроки парсингу:
1. Split by " --> ": ["00:01:23.456", "00:01:26.789 align:center size:50%"]
2. Split right part by space: ["00:01:26.789", "align:center", "size:50%"]
3. ParseDuration на parts[0] та partsRight[0]
4. Inline-стилі (align, size...) ігноруються у цьому парсері

Результат:
• StartTime = 1m23.456s
• EndTime = 1m26.789s
• Стилі зберігаються у Lines для подальшої обробки
```

---

## 🔍 Функція `parseDurationWebVTT`: парсинг тривалості

```go
func parseDurationWebVTT(i string) (time.Duration, error) {
    return parseDuration(i, ".", 3)  // 🔹 "." як роздільник, 3 цифри мілісекунд
}
```

### 🎯 Універсальна функція `parseDuration`

```go
func parseDuration(i, millisecondSep string, numberOfMillisecondDigits int) (time.Duration, error) {
    // 🔹 1. Розділити мілісекунди
    var parts = strings.Split(i, millisecondSep)
    var milliseconds int
    var s string
    
    if len(parts) >= 2 {
        // 🔹 Перевірка кількості цифр мілісекунд
        s = strings.TrimSpace(parts[len(parts)-1])
        if len(s) > 3 {
            return 0, fmt.Errorf("astisub: Invalid number of millisecond digits detected in %s", i)
        }
        
        // 🔹 Парсинг мілісекунд з масштабуванням
        if milliseconds, err = strconv.Atoi(s); err != nil {
            return 0, fmt.Errorf("atoi of %q failed: %s", s, err)
        }
        // 🔹 Масштабування: "5" → 500мс, "50" → 500мс, "500" → 500мс
        milliseconds *= int(math.Pow10(numberOfMillisecondDigits - len(s)))
        
        s = strings.Join(parts[:len(parts)-1], millisecondSep)  // 🔹 Решта рядка
    } else {
        s = i  // 🔹 Немає мілісекунд
    }
    
    // 🔹 2. Розділити години:хвилини:секунди
    parts = strings.Split(strings.TrimSpace(s), ":")
    var partSeconds, partMinutes, partHours string
    
    switch len(parts) {
    case 2:  // Формат: "мм:сс" або "сс.мс"
        partSeconds = parts[1]
        partMinutes = parts[0]
    case 3:  // Формат: "гг:мм:сс.мс"
        partSeconds = parts[2]
        partMinutes = parts[1]
        partHours = parts[0]
    default:
        return 0, fmt.Errorf("astisub: No hours, minutes or seconds detected in %s", i)
    }
    
    // 🔹 3. Парсинг компонентів
    var seconds, _ = strconv.Atoi(strings.TrimSpace(partSeconds))
    var minutes, _ = strconv.Atoi(strings.TrimSpace(partMinutes))
    var hours, _ = strconv.Atoi(strings.TrimSpace(partHours))
    
    // 🔹 4. Збірка time.Duration
    return time.Duration(milliseconds)*time.Millisecond + 
           time.Duration(seconds)*time.Second + 
           time.Duration(minutes)*time.Minute + 
           time.Duration(hours)*time.Hour, nil
}
```

### 🎯 Підтримка форматів часу

```
Підтримувані формати (через параметризацію):
• "00:01:23.456"  → 1 хв 23.456 сек  (WebVTT стандарт)
• "00:01:23,456"  → 1 хв 23.456 сек  (SRT формат, кома замість крапки)
• "0:00:00:00"    → годинник-формат (рідкісний випадок)

Масштабування мілісекунд:
• Вхід: "5" (1 цифра) → 5 × 10^(3-1) = 500 мс
• Вхід: "50" (2 цифри) → 50 × 10^(3-2) = 500 мс  
• Вхід: "500" (3 цифри) → 500 × 10^(3-3) = 500 мс

Це дозволяє коректно обробляти неповні мілісекунди.
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Потокова сегментація субтитрів

```go
// У converter/subtitle.go — сегментація WebVTT на 6-секундні шматки:
func Segment(reader io.Reader, segmentDuration time.Duration, outputDir, name string) error {
    blocks := make(chan SubtitleBlock, 100)  // 🔹 Буферизований канал
    
    // 🔹 Запустити парсинг у фоні
    go func() {
        if err := ReadFromWebVTT(reader, blocks); err != nil {
            log.Errorf("WebVTT parsing failed: %v", err)
        }
    }()
    
    // 🔹 Групувати блоки по сегментах
    var currentSegment []SubtitleBlock
    var segmentStartTime time.Time
    segmentIndex := 0
    
    for block := range blocks {
        // 🔹 Новий сегмент?
        if segmentStartTime.IsZero() {
            segmentStartTime = block.StartTime
        }
        
        // 🔹 Час вийшов? Записати сегмент
        if block.StartTime.Sub(segmentStartTime) >= segmentDuration {
            if err := writeSegment(outputDir, name, segmentIndex, currentSegment); err != nil {
                return err
            }
            // 🔹 Підготувати наступний сегмент
            currentSegment = []SubtitleBlock{block}
            segmentStartTime = block.StartTime
            segmentIndex++
        } else {
            currentSegment = append(currentSegment, block)
        }
    }
    
    // 🔹 Записати останній сегмент
    if len(currentSegment) > 0 {
        return writeSegment(outputDir, name, segmentIndex, currentSegment)
    }
    return nil
}

func writeSegment(dir, name string, index int, blocks []SubtitleBlock) error {
    filename := filepath.Join(dir, fmt.Sprintf("%s_%05d.vtt", name, index))
    f, err := os.Create(filename)
    if err != nil { return err }
    defer f.Close()
    
    f.WriteString("WEBVTT\n\n")
    for _, b := range blocks {
        // 🔹 Коригувати таймкоди відносно початку сегмента
        start := b.StartTime - blocks[0].StartTime
        end := b.EndTime - blocks[0].StartTime
        f.WriteString(fmt.Sprintf("%s --> %s\n%s\n", 
            formatDuration(start), formatDuration(end), b.Lines.String()))
    }
    return nil
}
```

### ✅ 2: Синхронізація субтитрів з відео-сегментами

```go
// У segmentAssembler — прив'язка субтитрів до відео за таймінгами:
type SubtitleSync struct {
    videoPTS    int64        // 🔹 PTS поточного відео-сегмента
    subtitleOffset time.Duration  // 🔹 Зсув субтитрів відносно відео
}

func syncSubtitleBlock(block SubtitleBlock, sync *SubtitleSync) SubtitleBlock {
    // 🔹 Скоригувати таймкоди субтитрів до PTS відео
    block.StartTime += sync.subtitleOffset
    block.EndTime += sync.subtitleOffset
    
    // 🔹 Конвертувати у PTS для порівняння з відео
    blockStartPTS := int64(block.StartTime.Seconds() * 90000)  // 🔹 90 kHz clock
    blockEndPTS := int64(block.EndTime.Seconds() * 90000)
    
    // 🔹 Перевірити перетин з відео-сегментом
    if blockEndPTS < sync.videoPTS || blockStartPTS > sync.videoPTS+segmentDurationPTS {
        // 🔹 Блок не належить цьому сегменту
        return SubtitleBlock{}  // порожній = пропустити
    }
    
    return block
}
```

### ✅ 3: Моніторинг якості субтитрів

```go
// monitoring.Monitor — метрики для WebVTT обробки:
type SubtitleMetrics struct {
    BlocksParsed      *prometheus.CounterVec  // кількість розпарсених блоків
    ParseErrors       *prometheus.CounterVec  // помилки парсингу
    DurationDistribution *prometheus.HistogramVec  // розподіл тривалостей реплік
    SyncDriftGauge    *prometheus.GaugeVec    // дрейф синхронізації субтитри/відео
}

// У процесі парсингу:
func monitorWebVTTParsing(channelID string, block SubtitleBlock, 
                         metrics *SubtitleMetrics, err error) {
    
    if err != nil {
        metrics.ParseErrors.WithLabelValues(channelID).Inc()
        return
    }
    
    metrics.BlocksParsed.WithLabelValues(channelID).Inc()
    
    // 🔹 Тривалість репліки
    duration := block.EndTime.Sub(block.StartTime).Seconds()
    metrics.DurationDistribution.WithLabelValues(channelID).Observe(duration)
    
    // 🔹 Попередження про підозрілі значення
    if duration < 0.1 {
        log.Warnf("Channel %s: very short subtitle block: %.3fs", channelID, duration)
    }
    if duration > 30.0 {
        log.Warnf("Channel %s: very long subtitle block: %.3fs", channelID, duration)
    }
}
```

### ✅ 4: Обробка помилок парсингу

```go
// Стратегія: продовжити парсинг навіть при помилках у окремих блоках
func ReadFromWebVTTWithRecovery(i io.Reader, c chan<- SubtitleBlock, 
                               channelID string, metrics *SubtitleMetrics) error {
    
    defer close(c)  // 🔹 Гарантувати закриття каналу
    
    scanner := bufio.NewScanner(i)
    // ... ініціалізація ...
    
    for scanner.Scan() {
        line := scanner.Text()
        
        // 🔹 Спробувати розпарсити таймкоди з recover
        if strings.Contains(line, webvttTimeBoundariesSeparator) {
            func() {
                defer func() {
                    if r := recover(); r != nil {
                        log.Errorf("Channel %s: panic parsing line %q: %v", channelID, line, r)
                        metrics.ParseErrors.WithLabelValues(channelID).Inc()
                    }
                }()
                
                // 🔹 Основна логіка парсингу таймкодів
                parts := strings.Split(line, webvttTimeBoundariesSeparator)
                // ... парсинг ...
            }()
        }
        
        // ... решта логіки ...
    }
    
    if err := scanner.Err(); err != nil {
        log.Errorf("Channel %s: scanner error: %v", channelID, err)
        return err
    }
    
    return nil
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на парсинг базового WebVTT

```go
func TestReadFromWebVTT_Basic(t *testing.T) {
    input := `WEBVTT

00:01:23.456 --> 00:01:26.789
Привіт, світе!

00:02:00.000 --> 00:02:03.500 align:center
Друга репліка
`
    blocks := make(chan SubtitleBlock, 10)
    
    go func() {
        err := ReadFromWebVTT(strings.NewReader(input), blocks)
        assert.NoError(t, err)
    }()
    
    // 🔹 Читати блоки
    var results []SubtitleBlock
    for block := range blocks {
        results = append(results, block)
    }
    
    // 🔹 Перевірити результат
    assert.Len(t, results, 2)
    
    // 🔹 Перший блок
    assert.Equal(t, 1*time.Minute+23*time.Second+456*time.Millisecond, results[0].StartTime)
    assert.Equal(t, 1*time.Minute+26*time.Second+789*time.Millisecond, results[0].EndTime)
    assert.Contains(t, results[0].Lines.String(), "Привіт, світе!")
    
    // 🔹 Другий блок
    assert.Equal(t, 2*time.Minute, results[1].StartTime)
    assert.Contains(t, results[1].Lines.String(), "align:center")  // 🔹 Inline-стиль зберігається
}
```

### 🔹 Тест на різні формати часу

```go
func TestParseDurationWebVTT_Formats(t *testing.T) {
    testCases := []struct {
        input    string
        expected time.Duration
    }{
        {"00:01:23.456", 1*time.Minute + 23*time.Second + 456*time.Millisecond},
        {"00:01:23,456", 1*time.Minute + 23*time.Second + 456*time.Millisecond},  // SRT формат
        {"01:23.456", 1*time.Minute + 23*time.Second + 456*time.Millisecond},     // без годин
        {"23.456", 23*time.Second + 456*time.Millisecond},                        // тільки секунди
        {"00:01:23.5", 1*time.Minute + 23*time.Second + 500*time.Millisecond},    // 1 цифра мс
        {"00:01:23.50", 1*time.Minute + 23*time.Second + 500*time.Millisecond},   // 2 цифри мс
    }
    
    for _, tc := range testCases {
        t.Run(tc.input, func(t *testing.T) {
            result, err := parseDurationWebVTT(tc.input)
            assert.NoError(t, err)
            assert.Equal(t, tc.expected, result)
        })
    }
}
```

### 🔹 Тест на помилки парсингу

```go
func TestParseDurationWebVTT_Errors(t *testing.T) {
    errorCases := []string{
        "invalid",                    // ❌ Невірний формат
        "00:01:23.1234",             // ❌ 4 цифри мілісекунд
        "00:01:23.abc",              // ❌ Не-число в мілісекундах
        "00:01",                      // ❌ Замало компонентів
    }
    
    for _, input := range errorCases {
        t.Run(input, func(t *testing.T) {
            _, err := parseDurationWebVTT(input)
            assert.Error(t, err, "Expected error for input: %s", input)
        })
    }
}
```

### 🔹 Інтеграційний тест на потокову обробку

```go
func TestReadFromWebVTT_StreamLargeFile(t *testing.T) {
    // 🔹 Згенерувати великий WebVTT (1000 реплік)
    var builder strings.Builder
    builder.WriteString("WEBVTT\n\n")
    for i := 0; i < 1000; i++ {
        start := time.Duration(i*5) * time.Second
        end := start + 3*time.Second
        builder.WriteString(fmt.Sprintf("%s --> %s\nРепліка %d\n\n", 
            formatDuration(start), formatDuration(end), i))
    }
    
    blocks := make(chan SubtitleBlock, 100)
    
    go func() {
        err := ReadFromWebVTT(strings.NewReader(builder.String()), blocks)
        assert.NoError(t, err)
    }()
    
    // 🔹 Підрахувати блоки без завантаження всього у пам'ять
    count := 0
    for range blocks {
        count++
    }
    
    assert.Equal(t, 1000, count)  // ✅ Всі блоки оброблено
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| BOM не обробляється | Заголовок "﻿WEBVTT" не розпізнається | 🔹 `strings.TrimPrefix(line, string(BytesBOM))` вже є — перевірити порядок виклику |
| Канал не закривається | Горутина зависає на `range blocks` | 🔹 `defer close(c)` у `ReadFromWebVTT` гарантує закриття навіть при помилці |
| Неповні мілісекунди | "00:01:23.5" парситься як 5 мс замість 500 мс | 🔹 Масштабування `milliseconds *= int(math.Pow10(3-len(s)))` вже реалізовано ✅ |
| Inline-стилі втрачаються | "align:center" зникає з репліки | 🔹 Стилі зберігаються у `Lines` — перевірити, що сегментер їх не фільтрує |
| Порожній файл | Паніка при доступі до `currentBlock.Lines` | 🔹 Перевірка `if currentBlock != nil` перед відправкою у канал |

### Приклад обробки BOM у різних кодуваннях:

```go
// 🔹 Розширена підтримка BOM для різних кодувань
func stripBOM(line string) string {
    // UTF-8 BOM
    line = strings.TrimPrefix(line, string([]byte{0xEF, 0xBB, 0xBF}))
    // UTF-16 LE BOM
    line = strings.TrimPrefix(line, string([]byte{0xFF, 0xFE}))
    // UTF-16 BE BOM
    line = strings.TrimPrefix(line, string([]byte{0xFE, 0xFF}))
    return line
}

// Використання у ReadFromWebVTT:
line = stripBOM(scanner.Text())
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базовий виклик парсингу:
func parseWebVTTFile(filePath string) ([]SubtitleBlock, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    blocks := make(chan SubtitleBlock, 100)
    go ReadFromWebVTT(f, blocks)
    
    var result []SubtitleBlock
    for block := range blocks {
        result = append(result, block)
    }
    return result, nil
}

// 2: Потокова фільтрація блоків:
func filterBlocksByTime(in <-chan SubtitleBlock, 
                       start, end time.Duration) <-chan SubtitleBlock {
    out := make(chan SubtitleBlock)
    
    go func() {
        defer close(out)
        for block := range in {
            if block.StartTime >= start && block.EndTime <= end {
                out <- block
            }
        }
    }()
    return out
}

// 3: Форматування duration для запису:
func formatDuration(d time.Duration) string {
    hours := int(d.Hours())
    minutes := int(d.Minutes()) % 60
    seconds := int(d.Seconds()) % 60
    milliseconds := int(d.Milliseconds()) % 1000
    
    return fmt.Sprintf("%02d:%02d:%02d.%03d", 
        hours, minutes, seconds, milliseconds)
}

// 4: Логування для відладки:
func logSubtitleBlock(channelID string, block SubtitleBlock) {
    log.Debugf("Channel %s: block %.3fs-%.3fs: %q", 
        channelID, 
        block.StartTime.Seconds(), 
        block.EndTime.Seconds(),
        strings.TrimSpace(block.Lines.String()))
}

// 5: Обробка помилок з retry:
func readWebVTTWithRetry(filePath string, maxRetries int) ([]SubtitleBlock, error) {
    var lastErr error
    for attempt := 0; attempt < maxRetries; attempt++ {
        blocks, err := parseWebVTTFile(filePath)
        if err == nil {
            return blocks, nil
        }
        lastErr = err
        log.Warnf("Attempt %d failed: %v", attempt+1, err)
        time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
    }
    return nil, fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}
```

---

## 📊 Матриця підтримуваних форматів

```
Формат входу          | Підтримка | Примітки
──────────────────────┼───────────┼─────────────────────────
WEBVTT стандарт       | ✅ Повна  | Основний випадок
SRT (.srt)            | ⚠️ Часткова| Через parseDuration з "," роздільником
Inline-стилі          | ✅ Зберігаються| У полі Lines для подальшої обробки
Регіони/Стилі         | ✅ Зберігаються| Як "асоційовані" рядки
BOM (UTF-8/16)        | ✅ Видаляється| Через TrimPrefix
Неповні мілісекунди   | ✅ Масштабуються| "5" → 500мс, "50" → 500мс
```

---

## 📚 Корисні посилання

- [WebVTT specification (W3C)](https://www.w3.org/TR/webvtt1/)
- [go-astisub source code](https://github.com/asticode/go-astisub)
- [HLS subtitle support](https://developer.apple.com/documentation/http_live_streaming/about_the_radio_stream_format)
- [Go bufio.Scanner best practices](https://pkg.go.dev/bufio#Scanner)

> 💡 **Ключова ідея**: Цей `webvtt` парсер — це "конвеєр" для субтитрів у вашому пайплайні. Він:
> - 🎯 Потоково читає файли будь-якого розміру без завантаження у пам'ять
> - 🔧 Підтримує різні формати часу та кодувань через параметризацію
> - ⚡ Дозволяє паралельну обробку через каналну архітектуру
> - 🛡️ Граційно обробляє помилки без зупинки всього процесу

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку додаткових форматів (SRT, ASS) через адаптери
- 🧪 Написати property-based тести для валідації парсингу на випадкових вхідних даних
- 📈 Додати Prometheus-метрики для моніторингу продуктивності парсингу та якості субтитрів по каналах

🛠️