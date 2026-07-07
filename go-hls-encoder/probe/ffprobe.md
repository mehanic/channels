# Глибоке роз'яснення: `probe` пакет — обгортка для ffprobe з JSON-парсингом

Цей файл містить **типізовану обгортку для ffprobe**, що дозволяє отримувати структуровані метадані про медіафайли: формати, потоки, кодеки, таймінги та теги. Це фундамент для валідації вхідних даних у вашому CCTV HLS пайплайні.

---

## 🎯 Навіщо цей код потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ probe у контексті HLS-енкодингу:       │
│                                         │
│ 🔹 Валідація вхідних потоків:          │
│   • Перевірка наявності відео/аудіо    │
│   • Детекція підтримуваних кодеків     │
│   • Витягування роздільної здатності   │
│   • Отримання тривалості для планування│
│                                         │
│ 🔹 Адаптивний енкодинг:                │
│   • Визначення бітрейту входу для      │
│     розрахунку вихідних варіантів      │
│   • Виявлення інтерлейсу для deinterlace│
│   • Детекція кольорового простору      │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Швидка перевірка цілісності запису │
│   • Моніторинг якості вхідного сигналу │
│   • Автоматичне налаштування енкодера  │
│     на основі метаданих                │
└─────────────────────────────────────────┘
```

---

## 🔧 Структури даних: типізація ffprobe виходу

### `ProbeData`: кореневий контейнер

```go
type ProbeData struct {
    Format  *ProbeFormat   `json:"format,omitempty"`   // 🔹 Метадані контейнера
    Streams []*ProbeStream `json:"streams,omitempty"`  // 🔹 Список потоків (відео/аудіо/субтитри)
}
```

> 💡 **Важливо**: `omitempty` дозволяє пропускати відсутні поля у JSON, що робить парсинг стійкішим до змін у виводі ffprobe.

### `ProbeFormat`: метадані контейнера

```go
type ProbeFormat struct {
    Filename         string      `json:"filename,omitempty"`        // 🎯 Шлях до файлу
    NBStreams        int         `json:"nb_streams,omitempty"`      // 🎯 Кількість потоків
    NBPrograms       int         `json:"nb_programs,omitempty"`     // 🎯 Кількість програм (для MPEG-TS)
    FormatName       string      `json:"format_name,omitempty"`     // 🎯 Формат: "mpegts", "matroska"...
    FormatLongName   string      `json:"format_long_name,omitempty"`// 🎯 Повна назва формату
    StartTimeSeconds string      `json:"start_time,omitempty"`      // 🎯 Час початку (строка)
    DurationSeconds  string      `json:"duration,omitempty"`        // 🎯 Тривалість (строка)
    Size             string      `json:"size,omitempty"`            // 🎯 Розмір файлу у байтах (строка)
    BitRate          string      `json:"bit_rate,omitempty"`        // 🎯 Середній бітрейт (строка)
    ProbeScore       int         `json:"probe_score,omitempty"`     // 🎯 Оцінка впевненості ffprobe (0-100)
    Tags             *FormatTags `json:"tags,omitempty"`            // 🎯 Метадані контейнера
}
```

### `ProbeStream`: метадані окремого потоку

```go
type ProbeStream struct {
    // 🔹 Ідентифікація
    Index              int    `json:"index"`              // 🎯 Індекс потоку (0, 1, 2...)
    CodecName          string `json:"codec_name"`         // 🎯 Кодек: "h264", "aac"...
    CodecLongName      string `json:"codec_long_name"`    // 🎯 Повна назва кодека
    CodecType          string `json:"codec_type"`         // 🎯 Тип: "video", "audio", "subtitle"
    
    // 🔹 Таймінги
    CodecTimeBase      string `json:"codec_time_base"`    // 🎯 Базовий таймінг кодека
    TimeBase           string `json:"time_base"`          // 🎯 Таймінг потоку
    StartPts           int    `json:"start_pts"`          // 🎯 Початкова точка у таймбейзах
    StartTime          string `json:"start_time"`         // 🎯 Початок у секундах (строка)
    DurationTs         uint64 `json:"duration_ts"`        // 🎯 Тривалість у таймбейзах
    Duration           float64 `json:"duration,string"`   // 🎯 Тривалість у секундах (парситься з строки!)
    
    // 🔹 Відео-специфічні поля
    Width              int    `json:"width"`              // 🎯 Ширина кадру
    Height             int    `json:"height"`             // 🎯 Висота кадру
    PixFmt             string `json:"pix_fmt,omitempty"`  // 🎯 Формат пікселів: "yuv420p"...
    SampleAspectRatio  string `json:"sample_aspect_ratio,omitempty"`
    DisplayAspectRatio string `json:"display_aspect_ratio,omitempty"`
    RFrameRate         string `json:"r_frame_rate"`       // 🎯 Real frame rate (напр., "30/1")
    AvgFrameRate       string `json:"avg_frame_rate"`     // 🎯 Середній frame rate
    HasBFrames         int    `json:"has_b_frames,omitempty"` // 🎯 Чи є B-frames
    Profile            string `json:"profile,omitempty"`  // 🎯 Профіль кодека: "high", "main"...
    Level              int    `json:"level,omitempty"`    // 🎯 Рівень кодека: 40, 41, 51...
    
    // 🔹 Аудіо-специфічні поля
    SampleFmt     string `json:"sample_fmt,omitempty"`    // 🎯 Формат семплів: "fltp"...
    SampleRate    string `json:"sample_rate,omitempty"`   // 🎯 Частота дискретизації
    Channels      int    `json:"channels,omitempty"`      // 🎯 Кількість каналів
    ChannelLayout string `json:"channel_layout,omitempty"`// 🎯 Розкладка каналів: "stereo", "5.1"...
    BitsPerSample int    `json:"bits_per_sample,omitempty"`
    
    // 🔹 Інші поля
    BitRate            int               `json:"bit_rate,string"`  // 🎯 Бітрейт потоку (парситься з строки!)
    NbFrames           string            `json:"nb_frames"`        // 🎯 Кількість кадрів (строка)
    Disposition        StreamDisposition `json:"disposition,omitempty"` // 🎯 Прапорці потоку
    Tags               StreamTags        `json:"tags,omitempty"`   // 🎯 Метадані потоку
    // ... та багато інших полів
}
```

### 🎯 Ключовий момент: парсинг чисел з рядків

```go
// У ProbeStream:
Duration float64 `json:"duration,string"`  // 🔹 Зверніть увагу на ",string"
BitRate  int     `json:"bit_rate,string"`

// Що це означає:
• ffprobe повертає числа як строки: "123.456", "1000000"
• ",string" інструктує json.Unmarshal спочатку парсити строку, потім конвертувати у число
• Без цього парсинг завершиться помилкою: "json: cannot unmarshal string into Go value of type float64"

Приклад:
  JSON: {"duration": "123.456"}
  Без ",string": ❌ помилка парсингу
  З ",string": ✅ Duration = 123.456 (float64)
```

---

## 🔍 Функція `Probe`: основна логіка прозонування

```go
func Probe(filename string) (*ProbeData, error) {
    // 🔹 1. Отримати метадані формату
    rf, errf := exec.Command("ffprobe", 
        "-show_format",      // 🔹 Показати інформацію про контейнер
        filename, 
        "-print_format", "json",  // 🔹 Вивід у JSON для парсингу
    ).Output()
    
    if errf != nil {
        return nil, errf
    }
    
    var v ProbeData
    // 🔹 2. Парсинг JSON формату
    err := json.Unmarshal(rf, &v)
    if err != nil {
        return &v, err  // 🔹 Повернути частково заповнену структуру
    }
    
    // 🔹 3. Отримати метадані потоків
    rs, errs := exec.Command("ffprobe", 
        "-show_streams",    // 🔹 Показати інформацію про кожен потік
        filename, 
        "-print_format", "json",
    ).Output()
    
    if errs != nil {
        return &v, errs
    }
    
    // 🔹 4. Парсинг JSON потоків (доповнює ту саму структуру v)
    err = json.Unmarshal(rs, &v)
    
    return &v, err
}
```

### 🎯 Ключові моменти реалізації

#### 🔹 Два окремих виклики ffprobe

```
Проблема:
• ffprobe має окремі опції для формату (-show_format) та потоків (-show_streams)
• Один виклик з обома опціями може дати неочікуваний JSON формат

Рішення:
• Викликати ffprobe двічі: спочатку для формату, потім для потоків
• Парсити обидва JSON у одну структуру `ProbeData`

Переваги:
✅ Простіший парсинг (окремі об'єкти для format/streams)
✅ Краща сумісність з різними версіями ffprobe
✅ Легше відлагоджувати окремі частини

Недоліки:
⚠️ Два процеси замість одного → трохи повільніше
⚠️ Можлива неконсистентність, якщо файл змінився між викликами (малоймовірно)
```

#### 🔹 Повернення часткових результатів при помилці

```go
if err != nil {
    return &v, err  // 🔹 Повернути структуру навіть при помилці парсингу
}
```

**Чому це важливо:**
```
Сценарій:
• Формат успішно пропарсено (v.Format != nil)
• Потоки не вдалося пропарсити (помилка у другому json.Unmarshal)

Якби ми повертали nil:
❌ Клієнтський код втратить доступ до валідних даних формату

З поверненням &v:
✅ Клієнт може перевірити v.Format та прийняти рішення
✅ Краща стійкість до часткових помилок
✅ Дозволяє "graceful degradation"
```

---

## 🔍 Функція `GetProbeData`: пакетна обробка кількох потоків

```go
func GetProbeData(streamURLs ...string) ([]*ProbeData, error) {
    for _, streamURL := range streamURLs {
        probeData, err := Probe(streamURL)
        if err != nil {
            // 🔹 Обгорнути помилку у структуровану YoutubeError
            return nil, jt_error.JoutubeError{
                ErrorType:       jt_error.ConversionError,
                Origin:          "probing file '" + streamURL + "'",
                AssociatedError: err,
            }
        }
        inputProbes = append(inputProbes, probeData)
    }
    return inputProbes, nil
}
```

### 🎯 Ключові аспекти:

#### 🔹 Варіадичні параметри (`...string`)

```go
func GetProbeData(streamURLs ...string)  // 🔹 Приймає будь-яку кількість аргументів

// Використання:
// Один потік:
probes, err := GetProbeData("input1.ts")

// Кілька потоків:
probes, err := GetProbeData("input1.ts", "input2.ts", "input3.ts")

// Масив:
urls := []string{"a.ts", "b.ts"}
probes, err := GetProbeData(urls...)  // 🔹 Розпаковка масиву
```

#### 🔹 Структурована обробка помилок через `jt_error.JoutubeError`

```go
// 🔹 YoutubeError надає контекст для відладки:
type YoutubeError struct {
    ErrorType       string  // 🔹 Категорія: "ConversionError", "NetworkError"...
    Origin          string  // 🔹 Де сталася помилка: "probing file 'input.ts'"
    AssociatedError error   // 🔹 Оригінальна помилка для деталей
}

// Переваги:
✅ Легше фільтрувати помилки за типом у логіці retry
✅ Зрозуміліші логи для операторів
✅ Можливість агрегувати метрики за ErrorType
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Валідація вхідного потоку перед енкодингом

```go
// У segmentAssembler — перевірка валідності вхідного файлу:
func validateInput(streamURL string) error {
    probeData, err := probe.Probe(streamURL)
    if err != nil {
        return fmt.Errorf("failed to probe %s: %w", streamURL, err)
    }
    
    // 🔹 Перевірити наявність відеопотоку
    var videoStream *probe.ProbeStream
    for _, s := range probeData.Streams {
        if s.CodecType == "video" {
            videoStream = s
            break
        }
    }
    if videoStream == nil {
        return fmt.Errorf("no video stream found in %s", streamURL)
    }
    
    // 🔹 Перевірити підтримку кодека
    supportedCodecs := map[string]bool{
        "h264": true, "libx264": true, "hevc": true, "libx265": true,
    }
    if !supportedCodecs[videoStream.CodecName] {
        return fmt.Errorf("unsupported video codec: %s", videoStream.CodecName)
    }
    
    // 🔹 Перевірити роздільну здатність
    if videoStream.Width < 320 || videoStream.Height < 240 {
        return fmt.Errorf("resolution too low: %dx%d", videoStream.Width, videoStream.Height)
    }
    
    // 🔹 Перевірити тривалість (для VOD)
    duration, _ := strconv.ParseFloat(probeData.Format.DurationSeconds, 64)
    if duration < 1.0 {
        return fmt.Errorf("duration too short: %.2fs", duration)
    }
    
    return nil
}
```

### ✅ 2: Автоматичне налаштування енкодера на основі метаданих

```go
// У converter — розрахунок параметрів енкодингу:
func suggestEncodingParams(probeData *probe.ProbeData) converter.EncodingConfig {
    config := converter.EncodingConfig{}
    
    // 🔹 Знайти відеопотік
    for _, s := range probeData.Streams {
        if s.CodecType != "video" { continue }
        
        // 🔹 Роздільна здатність → варіанти якості
        if s.Height >= 1080 {
            config.VideoVariants = append(config.VideoVariants,
                suggest.VideoVariant{ResolutionHeight: intPtr(1080), Bitrate: stringPtr("4000k")},
                suggest.VideoVariant{ResolutionHeight: intPtr(720), Bitrate: stringPtr("2000k")},
                suggest.VideoVariant{ResolutionHeight: intPtr(480), Bitrate: stringPtr("1000k")},
            )
        } else if s.Height >= 720 {
            config.VideoVariants = append(config.VideoVariants,
                suggest.VideoVariant{ResolutionHeight: intPtr(720), Bitrate: stringPtr("2000k")},
                suggest.VideoVariant{ResolutionHeight: intPtr(480), Bitrate: stringPtr("1000k")},
            )
        }
        
        // 🔹 Кодек → налаштування енкодера
        if s.CodecName == "hevc" || s.CodecName == "h265" {
            config.VideoVariants[0].Codec = "libx265"  // зберегти HEVC
            config.VideoVariants[0].AddHVC1Tag = true   // для сумісності з Apple
        }
        
        // 🔹 Інтерлейс → деінтерлейс
        if strings.Contains(s.PixFmt, "interlaced") || s.HasBFrames < 0 {
            config.VideoFilters = append(config.VideoFilters, "yadif")
        }
        
        break
    }
    
    // 🔹 Аудіопотік → налаштування аудіо
    for _, s := range probeData.Streams {
        if s.CodecType != "audio" { continue }
        
        config.AudioVariants = append(config.AudioVariants,
            suggest.AudioVariant{
                Codec: "aac",
                Bitrate: stringPtr("128k"),
                ConvertToStereo: s.Channels > 2,  // downmix 5.1→stereo
            },
        )
        break
    }
    
    return config
}

func intPtr(i int) *int { return &i }
func stringPtr(s string) *string { return &s }
```

### ✅ 3: Моніторинг якості вхідних потоків

```go
// monitoring.Monitor — метрики для прозонування:
type ProbeMetrics struct {
    ProbesTotal      *prometheus.CounterVec  // кількість прозон
    ProbesFailed     *prometheus.CounterVec  // помилки прозонування
    VideoCodecs      *prometheus.CounterVec  // розподіл відео-кодеків
    AudioCodecs      *prometheus.CounterVec  // розподіл аудіо-кодеків
    Resolutions      *prometheus.HistogramVec // розподіл роздільних здатностей
    Durations        *prometheus.HistogramVec // розподіл тривалостей
}

// У процесі прозонування:
func monitorProbe(streamURL string, probeData *probe.ProbeData, 
                 metrics *ProbeMetrics, err error) {
    
    metrics.ProbesTotal.WithLabelValues(streamURL).Inc()
    
    if err != nil {
        metrics.ProbesFailed.WithLabelValues(streamURL).Inc()
        return
    }
    
    // 🔹 Зібрати статистику по кодеках
    for _, s := range probeData.Streams {
        if s.CodecType == "video" {
            metrics.VideoCodecs.WithLabelValues(s.CodecName).Inc()
            metrics.Resolutions.WithLabelValues("width").Observe(float64(s.Width))
            metrics.Resolutions.WithLabelValues("height").Observe(float64(s.Height))
        }
        if s.CodecType == "audio" {
            metrics.AudioCodecs.WithLabelValues(s.CodecName).Inc()
        }
    }
    
    // 🔹 Тривалість
    if probeData.Format != nil {
        duration, _ := strconv.ParseFloat(probeData.Format.DurationSeconds, 64)
        metrics.Durations.WithLabelValues(streamURL).Observe(duration)
    }
}
```

### ✅ 4: Кешування результатів прозонування

```go
// Щоб не прозвонювати той самий файл багато разів:
type ProbeCache struct {
    mu    sync.RWMutex
    cache map[string]*probe.ProbeData  // key = filepath/URL, value = ProbeData
    ttl   time.Duration
}

func (c *ProbeCache) GetOrProbe(streamURL string) (*probe.ProbeData, error) {
    // 🔹 Спробувати отримати з кешу
    c.mu.RLock()
    if data, ok := c.cache[streamURL]; ok {
        c.mu.RUnlock()
        return data, nil
    }
    c.mu.RUnlock()
    
    // 🔹 Прозвонити файл
    data, err := probe.Probe(streamURL)
    if err != nil {
        return nil, err
    }
    
    // 🔹 Зберегти у кеш
    c.mu.Lock()
    c.cache[streamURL] = data
    c.mu.Unlock()
    
    return data, nil
}

// Використання:
probeCache := &ProbeCache{
    cache: make(map[string]*probe.ProbeData),
    ttl:   5 * time.Minute,  // оновлювати кеш кожні 5 хвилин
}

// При обробці вхідного потоку:
probeData, err := probeCache.GetOrProbe(inputURL)
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Інтеграційний тест на прозонування реального файлу

```go
func TestProbe_RealFile(t *testing.T) {
    // 🔹 Підготувати тестовий файл
    testFile := createTestVideoFile(t)  // ваша helper-функція
    
    // 🔹 Прозвонити
    data, err := Probe(testFile)
    assert.NoError(t, err)
    assert.NotNil(t, data)
    
    // 🔹 Перевірити формат
    assert.NotNil(t, data.Format)
    assert.NotEmpty(t, data.Format.FormatName)  // "mpegts", "matroska"...
    
    // 🔹 Перевірити наявність потоків
    assert.Greater(t, len(data.Streams), 0)
    
    // 🔹 Знайти відеопотік
    var videoStream *ProbeStream
    for _, s := range data.Streams {
        if s.CodecType == "video" {
            videoStream = s
            break
        }
    }
    assert.NotNil(t, videoStream, "no video stream found")
    
    // 🔹 Перевірити ключові поля відео
    assert.Greater(t, videoStream.Width, 0)
    assert.Greater(t, videoStream.Height, 0)
    assert.NotEmpty(t, videoStream.CodecName)
    assert.Greater(t, videoStream.Duration, 0.0)
}
```

### 🔹 Тест на обробку помилок

```go
func TestProbe_InvalidFile(t *testing.T) {
    // 🔹 Невірний шлях
    _, err := Probe("/nonexistent/file.ts")
    assert.Error(t, err)
    
    // 🔹 Пошкоджений файл
    corruptFile := createCorruptFile(t)
    _, err = Probe(corruptFile)
    // 🔹 Може повернути помилку або часткові дані — головне не панікувати
    // Перевіряємо, що функція завершилась коректно
}
```

### 🔹 Тест на парсинг чисел з рядків

```go
func TestProbe_StringNumbers(t *testing.T) {
    // 🔹 Створити файл з відомими метаданими
    testFile := createTestVideoWithDuration(t, 123.456)  // 123.456 секунд
    
    data, err := Probe(testFile)
    assert.NoError(t, err)
    
    // 🔹 Перевірити, що числа пропарсились коректно
    assert.InDelta(t, 123.456, data.Streams[0].Duration, 0.001)  // float64
    assert.Greater(t, data.Streams[0].BitRate, 0)  // int
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `",string"` не працює | Помилка парсингу: "json: cannot unmarshal string" | 🔹 Перевірити, що поле має `,string` у json-тезі; перевірити версію Go (підтримується з 1.1+) |
| Два виклики ffprobe | Повільне прозонування великих файлів | 🔹 Об'єднати виклики: `ffprobe -show_format -show_streams ...`; оновити парсинг |
| Часткові дані при помилці | Клієнт отримує nil замість часткових даних | 🔹 Завжди повертати `&v` навіть при помилці; документувати поведінку |
| Відсутні поля у JSON | Поля залишаються zero value, клієнт не розуміє чи це помилка | 🔹 Використовувати `omitempty` та перевіряти nil перед доступом |
| Різні версії ffprobe | Вихід JSON змінюється, парсинг ламається | 🔹 Фіксувати версію ffprobe у Docker; додати тести на сумісність |

### Приклад об'єднання викликів ffprobe:

```go
func ProbeOptimized(filename string) (*ProbeData, error) {
    // 🔹 Один виклик з обома опціями
    output, err := exec.Command("ffprobe", 
        "-show_format", 
        "-show_streams", 
        filename, 
        "-print_format", "json",
    ).Output()
    
    if err != nil {
        return nil, err
    }
    
    var v ProbeData
    // 🔹 ffprobe повертає один JSON з обома секціями
    err = json.Unmarshal(output, &v)
    
    return &v, err
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базове прозонування:
func getMediaMetadata(filePath string) (*probe.ProbeData, error) {
    return probe.Probe(filePath)
}

// 2: Пошук відеопотоку:
func findVideoStream(data *probe.ProbeData) *probe.ProbeStream {
    for _, s := range data.Streams {
        if s.CodecType == "video" {
            return s
        }
    }
    return nil
}

// 3: Перевірка підтримки кодека:
func isCodecSupported(codecName string) bool {
    supported := map[string]bool{
        "h264": true, "libx264": true, 
        "hevc": true, "libx265": true,
        "vp9": true, "av1": true,
    }
    return supported[codecName]
}

// 4: Конвертація тривалості:
func getDurationSeconds(data *probe.ProbeData) (float64, error) {
    if data.Format == nil {
        return 0, fmt.Errorf("no format data")
    }
    return strconv.ParseFloat(data.Format.DurationSeconds, 64)
}

// 5: Логування метаданих:
func logProbeResults(streamURL string, data *probe.ProbeData) {
    log.Infof("Probed %s:", streamURL)
    if data.Format != nil {
        log.Infof("  Format: %s (%s), duration: %ss", 
            data.Format.FormatName, data.Format.FormatLongName, 
            data.Format.DurationSeconds)
    }
    for i, s := range data.Streams {
        log.Infof("  Stream %d: type=%s, codec=%s", 
            i, s.CodecType, s.CodecName)
        if s.CodecType == "video" {
            log.Infof("    Resolution: %dx%d, fps: %s", 
                s.Width, s.Height, s.AvgFrameRate)
        }
        if s.CodecType == "audio" {
            log.Infof("    Channels: %d, sample_rate: %s", 
                s.Channels, s.SampleRate)
        }
    }
}
```

---

## 📊 Матриця корисних полів ProbeData для CCTV HLS

```
Поле                     | Тип       | Використання у пайплайні
─────────────────────────┼───────────┼─────────────────────────
Format.FormatName        | string    | ✅ Визначення формату входу (TS/MP4/MKV)
Format.DurationSeconds   | string    | ✅ Планування сегментів для VOD
Streams[].CodecType      | string    | ✅ Фільтрація відео/аудіо/субтитрів
Streams[].CodecName      | string    | ✅ Вибір енкодера (libx264/libx265)
Streams[].Width/Height   | int       | ✅ Розрахунок варіантів якості
Streams[].Duration       | float64   | ✅ Точна тривалість для таймінгів
Streams[].BitRate        | int       | ✅ Розрахунок вихідних бітрейтів
Streams[].PixFmt         | string    | ✅ Вибір filter графіків (deinterlace)
Streams[].Profile/Level  | string/int| ✅ Сумісність з цільовими пристроями
```

---

## 📚 Корисні посилання

- [FFmpeg ffprobe documentation](https://ffmpeg.org/ffprobe.html)
- [ffprobe JSON output format](https://ffmpeg.org/ffprobe.html#json)
- [Go encoding/json package](https://pkg.go.dev/encoding/json)
- [HLS codec compatibility guide](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices#3249247)

> 💡 **Ключова ідея**: Цей `probe` пакет — це "розвідник" вашого пайплайну. Він:
> - 🎯 Надає структурований доступ до метаданих будь-якого медіафайлу
> - 🔧 Дозволяє приймати розумні рішення про енкодинг на основі вхідних даних
> - ⚡ Масштабується через кешування та пакетну обробку
> - 🛡️ Граційно обробляє помилки без зупинки всього процесу

Якщо потрібно — можу допомогти:
- 🔄 Оптимізувати Probe() для зменшення кількості викликів ffprobe
- 🧪 Написати property-based тести для валідації парсингу різних форматів ffprobe
- 📈 Додати Prometheus-метрики для моніторингу успішності прозонування та розподілу кодеків по каналах

🛠️