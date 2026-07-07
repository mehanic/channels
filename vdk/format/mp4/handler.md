# 📦 Глибокий розбір: `mp4.Handler` — Реєстрація формату MP4 у vdk

Цей файл — **реєстратор формату MP4** у системі `avutil.RegisterHandler` бібліотеки `vdk`. Він надає механізми авто-детекту формату, factory-функції для створення демуксерів/муксерів, та список підтримуваних кодеків.

---

## 🗺️ Архітектурна схема реєстрації

```
┌────────────────────────────────────────┐
│ 📦 mp4.Handler — Format Registration   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • h.Ext = ".mp4" — розширення файлу  │
│  • h.Probe() — авто-детект за сигнатурою│
│  • h.ReaderDemuxer — factory для читання│
│  • h.WriterMuxer — factory для запису  │
│  • h.CodecTypes — підтримувані кодеки  │
│                                         │
│  🔄 Потік реєстрації:                  │
│  init() → Handler() → avutil.DefaultHandlers│
│                                         │
│  📡 Сигнатури MP4 (ISO BMFF):           │
│  • 'ftyp' — File Type Box              │
│  • 'moov' — Movie Box (метадані)       │
│  • 'mdat' — Media Data Box (дані)      │
│  • 'moof' — Movie Fragment Box (fMP4)  │
│  • 'free' — Free Space Box             │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Реєстрація розширення файлу

```go
h.Ext = ".mp4"
```

### Призначення:
Вказує системі `avutil` яке розширення файлу асоціювати з цим форматом. Це дозволяє:
- Авто-відкриття файлів за розширенням: `avutil.Open("video.mp4")`
- Генерація правильних розширень при записі
- Фільтрація файлів у файлових діалогах

### ✅ Ваш use-case: авто-відкриття файлів

```go
// OpenMediaFile — універсальне відкриття медіа-файлів
func OpenMediaFile(filename string) (av.DemuxCloser, error) {
    // avutil.Open автоматично:
    // 1. Перевіряє розширення (.mp4 → mp4.Demuxer)
    // 2. Якщо невідоме — читає перші байти та викликає Probe()
    // 3. Створює відповідний демуксер через ReaderDemuxer factory
    return avutil.Open(filename)
}

// Використання:
demuxer, err := OpenMediaFile("recording.mp4")
if err != nil {
    return nil, fmt.Errorf("open file: %w", err)
}
defer demuxer.Close()

// Тепер можна читати пакети
for {
    pkt, err := demuxer.ReadPacket()
    if err == io.EOF { break }
    // обробка...
}
```

---

## 🔑 2. Probe-функція — авто-детект формату

```go
h.Probe = func(b []byte) bool {
    switch string(b[4:8]) {
    case "moov", "ftyp", "free", "mdat", "moof":
        return true
    }
    return false
}
```

### 🔍 Як працює детект:

```
MP4 (ISO BMFF) використовує "boxes" або "atoms" — структуровані блоки даних:

Кожен box має формат:
  [4-byte size][4-byte type][payload...]

Перші 4 байти = розмір (включаючи заголовок)
Наступні 4 байти = тип box (fourcc код)

Приклади box типів:
  • 'ftyp' — File Type Box (завжди на початку файлу)
  • 'moov' — Movie Box (метадані треку)
  • 'mdat' — Media Data Box (сира медіа-інформація)
  • 'moof' — Movie Fragment Box (для фрагментованого MP4)
  • 'free' — Free Space Box (зайве місце)

Probe перевіряє байти 4-7 (індексація з 0):
  • b[0:4] = розмір першого box
  • b[4:8] = тип першого box
  
Якщо тип співпадає з відомими → це майже напевно MP4 файл.
```

### ⚠️ Обмеження та ризики:

```
❌ Може дати хибно-позитивний результат якщо:
• Випадкові дані містять 'moov'/'ftyp' на позиції 4-7
• Файл пошкоджений або зміщений

✅ Покращена версія для критичних застосунків:
h.Probe = func(b []byte) bool {
    if len(b) < 8 { return false }  // мінімум для box заголовку
    
    // Перевірка розміру: має бути >= 8 і <= довжини буфера
    size := int(binary.BigEndian.Uint32(b[0:4]))
    if size < 8 || size > len(b) {
        return false
    }
    
    boxType := string(b[4:8])
    switch boxType {
    case "ftyp", "moov", "mdat", "moof", "free":
        return true
    default:
        return false
    }
}
```

### ✅ Ваш use-case: валідація вхідного файлу

```go
// ValidateMP4File — перевірка чи файл дійсно MP4
func ValidateMP4File(r io.Reader) error {
    // Читання перших 16 байт для надійного детекту
    buf := make([]byte, 16)
    if _, err := io.ReadFull(r, buf); err != nil {
        return fmt.Errorf("read probe data: %w", err)
    }
    
    // Перевірка box заголовку
    size := int(binary.BigEndian.Uint32(buf[0:4]))
    boxType := string(buf[4:8])
    
    if size < 8 {
        return fmt.Errorf("invalid box size: %d", size)
    }
    
    validTypes := map[string]bool{
        "ftyp": true, "moov": true, "mdat": true, 
        "moof": true, "free": true,
    }
    
    if !validTypes[boxType] {
        return fmt.Errorf("invalid MP4 box type: %q", boxType)
    }
    
    // Додаткова перевірка для 'ftyp': має містити бренд 'isom' або 'mp42'
    if boxType == "ftyp" && len(buf) >= 16 {
        majorBrand := string(buf[8:12])
        if majorBrand != "isom" && majorBrand != "mp42" && majorBrand != "iso2" {
            return fmt.Errorf("unsupported MP4 brand: %q", majorBrand)
        }
    }
    
    log.Printf("Validated MP4 file, box type: %s, size: %d", boxType, size)
    return nil
}
```

---

## 🔑 3. Factory-функції для демуксера/муксера

### ReaderDemuxer — створення демуксера для читання:

```go
h.ReaderDemuxer = func(r io.Reader) av.Demuxer {
    return NewDemuxer(r.(io.ReadSeeker))
}
```

### WriterMuxer — створення муксера для запису:

```go
h.WriterMuxer = func(w io.Writer) av.Muxer {
    return NewMuxer(w.(io.WriteSeeker))
}
```

### ⚠️ Критична проблема: небезпечне type assertion

```go
r.(io.ReadSeeker)   // ← Паніка якщо r не реалізує io.ReadSeeker!
w.(io.WriteSeeker)  // ← Паніка якщо w не реалізує io.WriteSeeker!
```

**Наслідки**: Якщо користувач передає `io.Reader` без `Seek` (напр. `os.Stdin` або мережевий потік), програма впаде з панікою.

**✅ Виправлення**: Перевірка перед assertion або повернення помилки:

```go
h.ReaderDemuxer = func(r io.Reader) av.Demuxer {
    if rs, ok := r.(io.ReadSeeker); ok {
        return NewDemuxer(rs)
    }
    // Fallback: обгортка для підтримки seek через буферизацію
    // ⚠️ Це спрощений приклад — реальна реалізація може вимагати тимчасовий файл
    return NewDemuxerWithBuffer(r)
}

// NewDemuxerWithBuffer — обгортка для підтримки seek
func NewDemuxerWithBuffer(r io.Reader) av.Demuxer {
    // Створення тимчасового буфера для підтримки seek
    // У реальності: використання os.CreateTemp() або memory buffer
    // ... реалізація ...
}
```

### 🔍 Чому MP4 вимагає `io.ReadSeeker`/`io.WriteSeeker`?

```
MP4 формат має нелінійну структуру:
• Метадані (moov) можуть бути на початку або в кінці файлу
• Для читання: потрібно seek до moov для отримання таблиць семплів
• Для запису: потрібно seek назад для оновлення розмірів атомів

Без seek підтримки неможливо:
• Ефективно читати великі файли (не завантажуючи все в пам'ять)
• Записувати "slow start" MP4 (mdat спочатку, moov в кінці)
• Підтримувати фрагментований MP4 (fMP4) для streaming

Альтернативи для streaming:
• Використовувати фрагментований MP4 (moof + mdat пари)
• Реалізувати буферизацію всього файлу в пам'яті (не для великих файлів)
• Використовувати тимчасовий файл на диску для seek
```

### ✅ Ваш use-case: універсальний процесор медіа

```go
// MediaProcessor — обробка будь-якого підтримуваного формату
type MediaProcessor struct {
    channelID    string
    demuxer      av.DemuxCloser
    handler      *avutil.RegisterHandler
}

func NewMediaProcessor(channelID, filename string) (*MediaProcessor, error) {
    // Авто-детект формату через avutil
    demuxer, err := avutil.Open(filename)
    if err != nil {
        return nil, fmt.Errorf("open media: %w", err)
    }
    
    return &MediaProcessor{
        channelID:   channelID,
        demuxer:     demuxer,
    }, nil
}

// Process — універсальна обробка незалежно від формату
func (p *MediaProcessor) Process(ctx context.Context) error {
    // Отримання метаданих (працює для будь-якого формату)
    streams, err := p.demuxer.Streams()
    if err != nil {
        return fmt.Errorf("get streams: %w", err)
    }
    
    log.Printf("Channel %s: processing %d streams", p.channelID, len(streams))
    
    // Читання пакетів (уніфікований інтерфейс)
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        pkt, err := p.demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Обробка пакету (універсальна логіка)
        if err := p.processPacket(pkt); err != nil {
            return err
        }
    }
    
    return nil
}
```

---

## 🔑 4. CodecTypes — список підтримуваних кодеків

```go
var CodecTypes = []av.CodecType{av.H264, av.AAC}
h.CodecTypes = CodecTypes
```

### Призначення:
- Інформує систему `avutil` які кодеки підтримує цей формат
- Дозволяє фільтрацію потоків за типом кодека
- Використовується для валідації сумісності при транскодуванні

### ✅ Ваш use-case: фільтрація за підтримуваними кодеками

```go
// FilterSupportedStreams — вибір тільки підтримуваних потоків
func FilterSupportedStreams(streams []av.CodecType) []av.CodecType {
    supported := map[av.CodecType]bool{
        av.H264: true,
        av.AAC:  true,
    }
    
    result := make([]av.CodecType, 0, len(streams))
    for _, st := range streams {
        if supported[st] {
            result = append(result, st)
        } else {
            log.Printf("warning: unsupported codec for MP4: %v", st)
        }
    }
    return result
}

// Використання при ініціалізації:
streams, _ := demuxer.Streams()
supported := FilterSupportedStreams(streams)
if len(supported) == 0 {
    return fmt.Errorf("no supported codecs found for MP4 muxing")
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// init.go — реєстрація всіх форматів на старті програми
func init() {
    // Реєстрація MP4 handler
    mp4.Handler(avutil.DefaultHandlers)
    
    // Реєстрація інших форматів...
    // ts.Handler(avutil.DefaultHandlers)
    // flv.Handler(avutil.DefaultHandlers)
    // rtsp.Handler(avutil.DefaultHandlers)
    
    log.Printf("Registered media handlers: MP4, TS, FLV, RTSP")
}

// mp4_channel_processor.go — обробка каналу з авто-детектом формату
type MP4ChannelProcessor struct {
    channelID    string
    sourceFile   string
    demuxer      av.DemuxCloser
    transcoder   *Transcoder
    hlsWriter    *HLSWriter
    metrics      *ChannelMetrics
}

func NewMP4ChannelProcessor(channelID, sourceFile string) (*MP4ChannelProcessor, error) {
    // 1. Авто-відкриття джерела з детектом формату
    demuxer, err := avutil.Open(sourceFile)
    if err != nil {
        return nil, fmt.Errorf("open source: %w", err)
    }
    
    // 2. Отримання метаданих
    streams, err := demuxer.Streams()
    if err != nil {
        demuxer.Close()
        return nil, fmt.Errorf("probe streams: %w", err)
    }
    
    // 3. Валідація підтримуваних кодеків
    if err := validateStreams(streams); err != nil {
        demuxer.Close()
        return nil, err
    }
    
    // 4. Ініціалізація компонентів
    return &MP4ChannelProcessor{
        channelID:  channelID,
        sourceFile: sourceFile,
        demuxer:    demuxer,
        transcoder: NewTranscoder(streams),
        hlsWriter:  NewHLSWriter(channelID),
        metrics:    NewChannelMetrics(channelID),
    }, nil
}

// validateStreams — перевірка сумісності потоків
func validateStreams(streams []av.CodecData) error {
    var hasVideo, hasAudio bool
    
    for _, s := range streams {
        switch s.Type() {
        case av.H264:
            hasVideo = true
        case av.AAC:
            hasAudio = true
        default:
            log.Printf("warning: unknown codec type %v for MP4", s.Type())
        }
    }
    
    if !hasVideo {
        return fmt.Errorf("no supported video codec found for MP4 (expected H.264)")
    }
    // Аудіо опціональне для MP4
    
    return nil
}

// Run — основний цикл обробки каналу
func (p *MP4ChannelProcessor) Run(ctx context.Context) error {
    log.Printf("Channel %s: starting MP4 processing", p.channelID)
    
    // Ініціалізація HLS з метаданими
    streams, _ := p.demuxer.Streams()
    if err := p.hlsWriter.WriteHeader(streams); err != nil {
        return fmt.Errorf("init HLS: %w", err)
    }
    
    // Основний цикл
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        pkt, err := p.demuxer.ReadPacket()
        if err == io.EOF {
            log.Printf("Channel %s: file ended", p.channelID)
            break
        }
        if err != nil {
            p.metrics.ReadErrors.Inc()
            log.Printf("Channel %s: read error: %v", p.channelID, err)
            // Спроба відновлення або перезапуску
            continue
        }
        
        // Транскодування якщо потрібно
        if p.transcoder.NeedsTranscoding(pkt) {
            pkt, err = p.transcoder.Transcode(pkt)
            if err != nil {
                p.metrics.TranscodeErrors.Inc()
                continue
            }
        }
        
        // Запис у HLS
        if err := p.hlsWriter.WritePacket(pkt); err != nil {
            p.metrics.WriteErrors.Inc()
            return fmt.Errorf("write HLS: %w", err)
        }
        
        p.metrics.PacketsProcessed.Inc()
    }
    
    // Фіналізація
    return p.hlsWriter.WriteTrailer()
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Probe не спрацьовує** | Файл .mp4 не розпізнається як MP4 | Переконайтеся, що файл не пошкоджений; перевірте чи box type на позиції 4-7 співпадає |
| **Паніка при type assertion** | `r.(io.ReadSeeker)` падає для stdin/pipe | Перевіряйте тип перед assertion; використовуйте `io.ReadSeeker` інтерфейс |
| **Невідомий кодек** | `CodecTypes` не містить потрібний тип | Додайте новий тип у `CodecTypes` slice; переконайтеся, що демуксер підтримує його парсинг |
| **Factory повертає nil** | `ReaderDemuxer` не створює демуксер | Перевірте чи `NewDemuxer(r)` не повертає помилку; додайте логування у factory |
| **Конфлікт розширень** | Два формати претендують на ".mp4" | Переконайтеся, що кожен формат має унікальне розширення; використовуйте Probe для додаткової перевірки |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування результатів Probe:

```go
// FormatCache — кешування результатів детекту формату
type FormatCache struct {
    mu    sync.RWMutex
    cache map[string]string  // hash(first 8 bytes) → format name
}

func (c *FormatCache) DetectFormat(data []byte) string {
    if len(data) < 8 {
        return ""
    }
    
    // Простий хеш для ключа: перші 8 байт
    key := fmt.Sprintf("%x", data[:8])
    
    c.mu.RLock()
    if format, ok := c.cache[key]; ok {
        c.mu.RUnlock()
        return format
    }
    c.mu.RUnlock()
    
    // Детект якщо не в кеші
    var format string
    boxType := string(data[4:8])
    switch boxType {
    case "moov", "ftyp", "free", "mdat", "moof":
        format = "mp4"
    }
    
    c.mu.Lock()
    if c.cache == nil {
        c.cache = make(map[string]string)
    }
    c.cache[key] = format
    c.mu.Unlock()
    
    return format
}
```

### 2. Попередня реєстрація всіх handler'ів:

```go
// RegisterAllFormats — централізована реєстрація на старті
func RegisterAllFormats() {
    // MP4
    mp4.Handler(avutil.DefaultHandlers)
    
    // Інші формати...
    // ts.Handler(avutil.DefaultHandlers)
    // flv.Handler(avutil.DefaultHandlers)
    
    log.Printf("Registered %d media formats", len(avutil.DefaultHandlers.Handlers))
}

// Використання у main():
func main() {
    RegisterAllFormats()
    // ... решта ініціалізації ...
}
```

### 3. Моніторинг використання форматів:

```go
type FormatMetrics struct {
    FormatsDetected prometheus.CounterVec
    ProbeLatency    prometheus.HistogramVec
    OpenErrors      prometheus.CounterVec
}

func (m *FormatMetrics) RecordDetection(format string, duration time.Duration, err error) {
    if err != nil {
        m.OpenErrors.WithLabelValues(format).Inc()
        return
    }
    m.FormatsDetected.WithLabelValues(format).Inc()
    m.ProbeLatency.WithLabelValues(format).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції mp4.Handler

```go
// ✅ 1. Реєстрація handler у init()
func init() {
    mp4.Handler(avutil.DefaultHandlers)
}

// ✅ 2. Авто-відкриття файлів з детектом формату
demuxer, err := avutil.Open("video.mp4")
if err != nil { /* handle error */ }
defer demuxer.Close()

// ✅ 3. Отримання метаданих перед читанням
streams, err := demuxer.Streams()
if err != nil { /* handle error */ }

// ✅ 4. Фільтрація за підтримуваними кодеками
for _, s := range streams {
    if s.Type() != av.H264 && s.Type() != av.AAC {
        log.Printf("skipping unsupported codec: %v", s.Type())
        continue
    }
    // обробка...
}

// ✅ 5. Обробка помилок читання з відновленням
for {
    pkt, err := demuxer.ReadPacket()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Printf("read error: %v, attempting recovery", err)
        // логіка відновлення...
        continue
    }
    // обробка пакету...
}

// ✅ 6. Метрики для моніторингу
metrics.RecordDetection("mp4", time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [vdk mp4 Package](https://pkg.go.dev/github.com/deepch/vdk/format/mp4) — GoDoc documentation
- 💻 [vdk avutil Package](https://pkg.go.dev/github.com/deepch/vdk/av/avutil) — система реєстрації форматів
- 📄 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [MP4 Box Structure](https://wiki.multimedia.cx/index.php/MP4) — візуальна схема атомів
- 🧪 [Go io.ReaderSeeker Documentation](https://pkg.go.dev/io#ReadSeeker) — інтерфейси для потокового читання

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **медіа файлами у реальному часі**:
> 1. **Завжди реєструйте handler'и у `init()`** — це гарантує, що формати доступні до початку обробки.
> 2. **Використовуйте `avutil.Open()` замість прямих конструкторів** — це забезпечує авто-детект та уніфікований інтерфейс.
> 3. **Валідуйте `CodecTypes` перед обробкою** — уникнення помилок при зустрічі з невідомими кодеками.
> 4. **Моніторьте `ProbeLatency`** — різке зростання може вказувати на пошкоджені файли або проблеми з мережею.
> 5. **Завжди викликайте `Close()` на демуксерах** — уникнення витоку файлових дескрипторів та пам'яті.

Потрібен приклад реалізації `NewDemuxer` з підтримкою `io.Reader` (без seek) для streaming use cases? Готовий допомогти! 🚀