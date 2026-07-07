# 📦 Глибокий розбір: ts.Handler — Реєстрація MPEG-TS формату у vdk

Цей файл — **реєстратор формату MPEG-TS** у системі `avutil.RegisterHandler` бібліотеки `vdk`. Він надає механізми авто-детекту формату, factory-функції для створення демуксерів/муксерів, та список підтримуваних кодеків.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема реєстрації

```
┌────────────────────────────────────────┐
│ 📦 ts.Handler — Format Registration    │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • h.Ext = ".ts" — розширення файлу   │
│  • h.Probe() — авто-детект за сигнатурою│
│  • h.ReaderDemuxer — factory для читання│
│  • h.WriterMuxer — factory для запису  │
│  • h.CodecTypes — підтримувані кодеки  │
│                                         │
│  🔄 Потік реєстрації:                  │
│  init() → Handler() → avutil.DefaultHandlers│
│                                         │
│  📡 Сигнатура MPEG-TS:                  │
│  • Sync byte 0x47 кожні 188 байт       │
│  • Probe: b[0]==0x47 && b[188]==0x47  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Реєстрація розширення файлу

```go
h.Ext = ".ts"
```

### Призначення:
Вказує системі `avutil` яке розширення файлу асоціювати з цим форматом. Це дозволяє:
- Авто-відкриття файлів за розширенням: `avutil.Open("stream.ts")`
- Генерація правильних розширень при записі
- Фільтрація файлів у файлових діалогах

### ✅ Ваш use-case: авто-відкриття файлів

```go
// OpenMediaFile — універсальне відкриття медіа-файлів
func OpenMediaFile(filename string) (av.DemuxCloser, error) {
    // avutil.Open автоматично:
    // 1. Перевіряє розширення (.ts → ts.Demuxer)
    // 2. Якщо невідоме — читає перші байти та викликає Probe()
    // 3. Створює відповідний демуксер через ReaderDemuxer factory
    return avutil.Open(filename)
}

// Використання:
demuxer, err := OpenMediaFile("camera_stream.ts")
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
    return b[0] == 0x47 && b[188] == 0x47
}
```

### 🔍 Як працює детект:

```
MPEG-TS має фіксований розмір пакету: 188 байт
Кожен пакет починається з синхро-байта: 0x47 ('G')

Логіка Probe():
1. Перевіряємо b[0] == 0x47 (початок першого пакету)
2. Перевіряємо b[188] == 0x47 (початок другого пакету)
3. Якщо обидва співпадають → це майже напевно MPEG-TS

Чому це надійно:
• Ймовірність випадкового співпадіння: 1/256 × 1/256 = 1/65536
• Додаткова перевірка: можна перевірити b[376] для ще більшої впевненості
• Швидко: лише 2 порівняння байт, без парсингу
```

### ⚠️ Обмеження та ризики:

```
❌ Може дати хибно-позитивний результат якщо:
• Дані випадково містять 0x47 на позиціях 0 та 188
• Потік пошкоджений або зміщений

✅ Покращена версія для критичних застосунків:
h.Probe = func(b []byte) bool {
    if len(b) < 376 { return false }  // мінімум 2 повних пакети
    if b[0] != 0x47 || b[188] != 0x47 { return false }
    // Додаткова перевірка третього пакету для надійності
    return b[376] == 0x47
}
```

### ✅ Ваш use-case: валідація вхідного потоку

```go
// ValidateTSStream — перевірка чи потік дійсно MPEG-TS
func ValidateTSStream(r io.Reader) error {
    // Читання перших 376 байт для надійного детекту
    buf := make([]byte, 376)
    if _, err := io.ReadFull(r, buf); err != nil {
        return fmt.Errorf("read probe data: %w", err)
    }
    
    // Перевірка сигнатури
    if buf[0] != 0x47 || buf[188] != 0x47 || buf[376] != 0x47 {
        return fmt.Errorf("invalid TS signature: not MPEG-TS stream")
    }
    
    // Додаткова перевірка: парсинг першого заголовку
    pid := uint16((buf[1]&0x1f))<<8 | uint16(buf[2])
    if pid > 0x1FFF {  // PID має бути 13 біт
        return fmt.Errorf("invalid PID in TS header")
    }
    
    log.Printf("Validated MPEG-TS stream, first PID: 0x%X", pid)
    return nil
}
```

---

## 🔑 3. Factory-функції для демуксера/муксера

### ReaderDemuxer — створення демуксера для читання:

```go
h.ReaderDemuxer = func(r io.Reader) av.Demuxer {
    return NewDemuxer(r)
}
```

### WriterMuxer — створення муксера для запису:

```go
h.WriterMuxer = func(w io.Writer) av.Muxer {
    return NewMuxer(w)
}
```

### 🔍 Як це працює у avutil:

```
1. Користувач викликає: avutil.Open("file.ts")
2. avutil визначає розширення ".ts"
3. Шукає зареєстрований handler для ".ts"
4. Викликає handler.ReaderDemuxer(fileReader)
5. Отримує *ts.Demuxer реалізацію
6. Повертає як av.DemuxCloser інтерфейс

Переваги:
• Абстракція: користувач не знає про конкретну реалізацію
• Розширюваність: можна додати нові формати без зміни коду користувача
• Тестування: легко мокувати демуксери у тестах
```

### ✅ Ваш use-case: універсальний процесор медіа

```go
// MediaProcessor — обробка будь-якого підтримуваного формату
type MediaProcessor struct {
    channelID string
    demuxer   av.DemuxCloser
    handler   *avutil.RegisterHandler
}

func NewMediaProcessor(channelID, filename string) (*MediaProcessor, error) {
    // Авто-детект формату через avutil
    demuxer, err := avutil.Open(filename)
    if err != nil {
        return nil, fmt.Errorf("open media: %w", err)
    }
    
    return &MediaProcessor{
        channelID: channelID,
        demuxer:   demuxer,
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
h.CodecTypes = CodecTypes
```

### Де визначено `CodecTypes` (у іншому файлі пакету `ts`):

```go
var CodecTypes = []av.CodecType{
    av.H264,      // H.264/AVC відео
    av.AAC,       // AAC аудіо
    av.SPEEX,     // Speex аудіо (застарілий)
    av.H265,      // H.265/HEVC відео
    // Можливо інші: av.MJPEG, av.PCM_MULAW, тощо
}
```

### Призначення:
- Інформує систему `avutil` які кодеки підтримує цей формат
- Дозволяє фільтрацію потоків за типом кодека
- Використовується для валідації сумісності при транскодуванні

### ✅ Ваш use-case: фільтрація за підтримуваними кодеками

```go
// FilterSupportedStreams — вибір тільки підтримуваних потоків
func FilterSupportedStreams(streams []av.CodecType) []av.CodecType {
    supported := make(map[av.CodecType]bool)
    for _, ct := range ts.CodecTypes {
        supported[ct] = true
    }
    
    result := make([]av.CodecType, 0, len(streams))
    for _, st := range streams {
        if supported[st] {
            result = append(result, st)
        } else {
            log.Printf("warning: unsupported codec %v, skipping", st)
        }
    }
    return result
}

// Використання при ініціалізації:
streams, _ := demuxer.Streams()
supported := FilterSupportedStreams(streams)
if len(supported) == 0 {
    return fmt.Errorf("no supported codecs found in stream")
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// init.go — реєстрація всіх форматів на старті програми
func init() {
    // Реєстрація MPEG-TS handler
    ts.Handler(avutil.DefaultHandlers)
    
    // Реєстрація інших форматів...
    // flv.Handler(avutil.DefaultHandlers)
    // mp4.Handler(avutil.DefaultHandlers)
    // rtsp.Handler(avutil.DefaultHandlers)
    
    log.Printf("Registered media handlers: TS, FLV, MP4, RTSP")
}

// cctv_channel_processor.go — обробка каналу з авто-детектом формату
type CCTVChannelProcessor struct {
    channelID    string
    sourceURL    string
    demuxer      av.DemuxCloser
    transcoder   *Transcoder
    hlsWriter    *HLSWriter
    metrics      *ChannelMetrics
}

func NewCCTVChannelProcessor(channelID, sourceURL string) (*CCTVChannelProcessor, error) {
    // 1. Авто-відкриття джерела з детектом формату
    demuxer, err := avutil.Open(sourceURL)
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
    return &CCTVChannelProcessor{
        channelID:  channelID,
        sourceURL:  sourceURL,
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
        case av.H264, av.H265:
            hasVideo = true
        case av.AAC:
            hasAudio = true
        default:
            log.Printf("warning: unknown codec type %v", s.Type())
        }
    }
    
    if !hasVideo {
        return fmt.Errorf("no supported video codec found")
    }
    // Аудіо опціональне для CCTV
    
    return nil
}

// Run — основний цикл обробки каналу
func (p *CCTVChannelProcessor) Run(ctx context.Context) error {
    log.Printf("Channel %s: starting processing", p.channelID)
    
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
            log.Printf("Channel %s: stream ended", p.channelID)
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
| **Probe не спрацьовує** | Файл .ts не розпізнається як MPEG-TS | Переконайтеся, що файл не пошкоджений; перевірте чи синхро-байт 0x47 на позиціях 0, 188, 376 |
| **Невідомий кодек** | `CodecTypes` не містить потрібний тип | Додайте новий тип у `CodecTypes` slice; переконайтеся, що демуксер підтримує його парсинг |
| **Factory повертає nil** | `ReaderDemuxer` не створює демуксер | Перевірте чи `NewDemuxer(r)` не повертає помилку; додайте логування у factory |
| **Конфлікт розширень** | Два формати претендують на ".ts" | Переконайтеся, що кожен формат має унікальне розширення; використовуйте Probe для додаткової перевірки |
| **Memory leak у демуксері** | `Close()` не викликається | Завжди використовуйте `defer demuxer.Close()` після успішного відкриття |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування результатів Probe:

```go
// FormatCache — кешування результатів детекту формату
type FormatCache struct {
    mu    sync.RWMutex
    cache map[string]string  // hash(first 376 bytes) → format name
}

func (c *FormatCache) DetectFormat(data []byte) string {
    if len(data) < 376 {
        return ""
    }
    
    // Простий хеш для ключа
    key := fmt.Sprintf("%x", data[:376])
    
    c.mu.RLock()
    if format, ok := c.cache[key]; ok {
        c.mu.RUnlock()
        return format
    }
    c.mu.RUnlock()
    
    // Детект якщо не в кеші
    var format string
    if data[0] == 0x47 && data[188] == 0x47 {
        format = "mpegts"
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
    // TS
    ts.Handler(avutil.DefaultHandlers)
    
    // Інші формати...
    // flv.Handler(avutil.DefaultHandlers)
    // mp4.Handler(avutil.DefaultHandlers)
    
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

## 📋 Чек-лист інтеграції ts.Handler

```go
// ✅ 1. Реєстрація handler у init()
func init() {
    ts.Handler(avutil.DefaultHandlers)
}

// ✅ 2. Авто-відкриття файлів з детектом формату
demuxer, err := avutil.Open("stream.ts")
if err != nil { /* handle error */ }
defer demuxer.Close()

// ✅ 3. Отримання метаданих перед читанням
streams, err := demuxer.Streams()
if err != nil { /* handle error */ }

// ✅ 4. Фільтрація за підтримуваними кодеками
for _, s := range streams {
    if !isSupportedCodec(s.Type()) {
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
metrics.RecordDetection("mpegts", time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [vdk ts Package](https://pkg.go.dev/github.com/deepch/vdk/format/ts) — GoDoc documentation
- 💻 [vdk avutil Package](https://pkg.go.dev/github.com/deepch/vdk/av/avutil) — система реєстрації форматів
- 📄 [MPEG-TS Sync Byte Specification](https://en.wikipedia.org/wiki/MPEG_transport_stream#Packet) — опис синхронізації
- 📄 [HLS TS Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до TS у HLS
- 🧪 [Go io.Reader Documentation](https://pkg.go.dev/io#Reader) — інтерфейси для потокового читання

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **різними джерелами медіа у реальному часі**:
> 1. **Завжди реєструйте handler'и у `init()`** — це гарантує, що формати доступні до початку обробки.
> 2. **Використовуйте `avutil.Open()` замість прямих конструкторів** — це забезпечує авто-детект та уніфікований інтерфейс.
> 3. **Валідуйте `CodecTypes` перед обробкою** — уникнення помилок при зустрічі з невідомими кодеками.
> 4. **Моніторьте `ProbeLatency`** — різке зростання може вказувати на пошкоджені файли або проблеми з мережею.
> 5. **Завжди викликайте `Close()` на демуксерах** — уникнення витоку файлових дескрипторів та пам'яті.

Потрібен приклад реалізації `isSupportedCodec()` з розширенням підтримки нових кодеків (напр. AV1, Opus)? Готовий допомогти! 🚀