# 📦 Глибокий розбір: `mkv.Handler` — Реєстрація формату MKV у vdk

Цей файл — **реєстратор формату MKV** у системі `avutil.RegisterHandler` бібліотеки `vdk`. Він надає механізми авто-детекту формату, factory-функції для створення демуксерів, та список підтримуваних кодеків.

---

## 🗺️ Архітектурна схема реєстрації

```
┌────────────────────────────────────────┐
│ 📦 mkv.Handler — Format Registration   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • h.Ext = ".mkv" — розширення файлу  │
│  • h.Probe() — авто-детект за сигнатурою│
│  • h.ReaderDemuxer — factory для читання│
│  • h.WriterMuxer — factory для запису  │
│  • h.CodecTypes — підтримувані кодеки  │
│                                         │
│  🔄 Потік реєстрації:                  │
│  init() → Handler() → avutil.DefaultHandlers│
│                                         │
│  📡 Сигнатури для детекту:              │
│  • b[0] == 0x47 && b[188] == 0x47     │
│    → Це сигнатура MPEG-TS, НЕ MKV!    │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Реєстрація розширення файлу

```go
h.Ext = ".mkv"
```

### Призначення:
Вказує системі `avutil` яке розширення файлу асоціювати з цим форматом. Це дозволяє:
- Авто-відкриття файлів за розширенням: `avutil.Open("video.mkv")`
- Генерація правильних розширень при записі
- Фільтрація файлів у файлових діалогах

### ✅ Ваш use-case: авто-відкриття файлів

```go
// OpenMediaFile — універсальне відкриття медіа-файлів
func OpenMediaFile(filename string) (av.DemuxCloser, error) {
    // avutil.Open автоматично:
    // 1. Перевіряє розширення (.mkv → mkv.Demuxer)
    // 2. Якщо невідоме — читає перші байти та викликає Probe()
    // 3. Створює відповідний демуксер через ReaderDemuxer factory
    return avutil.Open(filename)
}

// Використання:
demuxer, err := OpenMediaFile("recording.mkv")
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

### ⚠️ КРИТИЧНА ПРОБЛЕМА: неправильна сигнатура для MKV!

```go
h.Probe = func(b []byte) bool {
    return b[0] == 0x47 && b[188] == 0x47
}
```

### 🔍 Що перевіряє цей код:

```
0x47 = символ 'G' у ASCII, але у контексті медіа:
• Це перший байт пакету MPEG Transport Stream (MPEG-TS)
• MPEG-TS пакети мають фіксований розмір 188 байт
• Кожен пакет починається з 0x47 (sync byte)

Перевірка:
• b[0] == 0x47 → перший байт = sync byte
• b[188] == 0x47 → байт 188 = початок наступного пакету

Висновок: ЦЯ ПЕРЕВІРКА ВИЗНАЧАЄ MPEG-TS, А НЕ MKV!
```

### 🔍 Правильна сигнатура для MKV/WebM:

```
MKV/WebM базуються на форматі EBML (Extensible Binary Meta Language).
Перші байти валідного файлу:

[0-3]   = розмір EBML header (зазвичай 0x1A45DFA3 у big-endian)
[4-7]   = EBML ID (0x1A45DFA3 = "EBML")
[8-11]  = розмір EBML header content
[12...] = EBML header content

Мінімальна перевірка для MKV:
• b[0] == 0x1A (перший байт EBML ID)
• b[1] == 0x45
• b[2] == 0xDF
• b[3] == 0xA3

АБО простіша перевірка за DocType:
• Пошук рядка "matroska" або "webm" у перших кілобайтах
```

### ✅ Виправлення: коректна Probe-функція для MKV

```go
h.Probe = func(b []byte) bool {
    if len(b) < 4 {
        return false
    }
    
    // Перевірка EBML ID (0x1A45DFA3)
    if b[0] == 0x1A && b[1] == 0x45 && b[2] == 0xDF && b[3] == 0xA3 {
        return true
    }
    
    // Додаткова перевірка: пошук DocType "matroska" або "webm"
    // у перших 4096 байтах (спрощено)
    if len(b) >= 20 {
        // Шукаємо рядок "matroska" або "webm" після EBML header
        for i := 4; i < len(b)-8; i++ {
            if string(b[i:i+8]) == "matroska" || string(b[i:i+5]) == "webm" {
                return true
            }
        }
    }
    
    return false
}
```

### ✅ Ваш use-case: валідація вхідного файлу

```go
// ValidateMKVFile — перевірка чи файл дійсно MKV
func ValidateMKVFile(r io.Reader) error {
    // Читання перших 16 байт для надійного детекту
    buf := make([]byte, 16)
    if _, err := io.ReadFull(r, buf); err != nil {
        return fmt.Errorf("read probe  %w", err)
    }
    
    // Перевірка EBML ID
    ebmlID := binary.BigEndian.Uint32(buf[0:4])
    if ebmlID != 0x1A45DFA3 {
        return fmt.Errorf("invalid EBML ID: 0x%X", ebmlID)
    }
    
    // Додаткова перевірка: читання та парсинг EBML header
    // ... реалізація ...
    
    log.Printf("Validated MKV file, EBML ID: 0x%X", ebmlID)
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
    //return NewMuxer(w)
    return nil  // ← ЗАПИС НЕ ПІДТРИМУЄТЬСЯ!
}
```

### ⚠️ Критична проблема: запис не реалізовано

```
Поточна реалізація:
    return nil  // ← завжди повертає nil!

Наслідки:
• avutil.NewMuxer("output.mkv") поверне помилку
• Неможливо створювати нові MKV файли через цю бібліотеку
• Тільки читання підтримується

✅ Якщо потрібен запис:
    // 1. Реалізувати mkv.NewMuxer(w io.Writer)
    // 2. Повернути екземпляр замість nil
    return NewMuxer(w)
    
// АБО явно документувати обмеження:
    return nil, fmt.Errorf("MKV writing not supported in this version")
```

### 🔍 Чому MKV запис складний:

```
MKV/WebM мають складну ієрархічну структуру:
• Потрібно записувати елементи у правильному порядку
• Багато метаданих мають бути відомі заздалегідь (тривалість, кодеки)
• SimpleBlock/BlockGroup вимагають коректних таймінгів
• SeekHead/Cues індекси бажані для seek підтримки

Альтернативи для запису:
• Використовувати фрагментований MP4 (fMP4) для streaming
• Використовувати TS для live-стрімінгу
• Реалізувати повний MKV muxer (значні зусилля)
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

### ⚠️ Обмеження поточної реалізації:

```
Поточний список: [H.264, AAC]

Проблеми:
• MKV/WebM підтримують набагато більше кодеків:
  - Відео: VP8, VP9, AV1, HEVC, тощо
  - Аудіо: Opus, Vorbis, FLAC, AC3, тощо
• Поточний mkv.Demuxer реалізує тільки H.264 парсинг
• AAC підтримка не реалізована у mkv.Demuxer.ReadPacket()

✅ Рекомендації:
1. Видалити AAC з CodecTypes поки не реалізовано підтримку
2. Додати підтримку інших відео кодеків у mkv.Demuxer
3. Документувати обмеження підтримки кодеків
```

### ✅ Ваш use-case: фільтрація за підтримуваними кодеками

```go
// FilterSupportedStreams — вибір тільки підтримуваних потоків
func FilterSupportedStreams(streams []av.CodecType) []av.CodecType {
    supported := map[av.CodecType]bool{
        av.H264: true,
        // av.AAC: true,  // видалено поки не реалізовано
    }
    
    result := make([]av.CodecType, 0, len(streams))
    for _, st := range streams {
        if supported[st] {
            result = append(result, st)
        } else {
            log.Printf("warning: unsupported codec for MKV demuxer: %v", st)
        }
    }
    return result
}

// Використання при ініціалізації:
streams, _ := demuxer.Streams()
supported := FilterSupportedStreams(streams)
if len(supported) == 0 {
    return fmt.Errorf("no supported codecs found for MKV demuxing")
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// init.go — реєстрація всіх форматів на старті програми
func init() {
    // Реєстрація MKV handler
    mkv.Handler(avutil.DefaultHandlers)
    
    // Реєстрація інших форматів...
    // mp4.Handler(avutil.DefaultHandlers)
    // ts.Handler(avutil.DefaultHandlers)
    // flv.Handler(avutil.DefaultHandlers)
    
    log.Printf("Registered media handlers: MKV, MP4, TS, FLV")
}

// mkv_channel_processor.go — обробка каналу з авто-детектом формату
type MKVChannelProcessor struct {
    channelID    string
    sourceFile   string
    demuxer      av.DemuxCloser
    transcoder   *Transcoder
    hlsWriter    *HLSWriter
    metrics      *ChannelMetrics
}

func NewMKVChannelProcessor(channelID, sourceFile string) (*MKVChannelProcessor, error) {
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
    return &MKVChannelProcessor{
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
    var hasVideo bool
    
    for _, s := range streams {
        switch s.Type() {
        case av.H264:
            hasVideo = true
            // Додаткова перевірка: наявність SPS/PPS
        default:
            log.Printf("warning: unknown codec type %v for MKV demuxer", s.Type())
        }
    }
    
    if !hasVideo {
        return fmt.Errorf("no supported video codec found for MKV demuxer (expected H.264)")
    }
    
    return nil
}

// Run — основний цикл обробки каналу
func (p *MKVChannelProcessor) Run(ctx context.Context) error {
    log.Printf("Channel %s: starting MKV processing", p.channelID)
    
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
| **Probe не спрацьовує для справжніх MKV** | Файли .mkv не розпізнаються як MKV | Замінити перевірку на EBML ID (0x1A45DFA3) замість MPEG-TS сигнатури |
| **Помилка при створенні муксера** | `avutil.NewMuxer("out.mkv")` повертає nil | Реалізувати mkv.NewMuxer() або документувати обмеження |
| **Непідтримувані кодеки** | AAC потік ігнорується або викликає помилку | Видалити AAC з CodecTypes або реалізувати підтримку у mkv.Demuxer |
| **Конфлікт розширень** | Інший формат також претендує на ".mkv" | Перевірити реєстрацію інших handler'ів; використовуйте Probe для додаткової перевірки |
| **Нескінченний цикл у ReadPacket** | mkv.Demuxer.ReadPacket() не повертає | Обробляти помилки від mkvio.ParseElement() та SplitNALUs() |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування результатів Probe:

```go
// FormatCache — кешування результатів детекту формату
type FormatCache struct {
    mu    sync.RWMutex
    cache map[string]string  // hash(first 16 bytes) → format name
}

func (c *FormatCache) DetectFormat(data []byte) string {
    if len(data) < 16 {
        return ""
    }
    
    // Простий хеш для ключа: перші 16 байт
    key := fmt.Sprintf("%x", data[:16])
    
    c.mu.RLock()
    if format, ok := c.cache[key]; ok {
        c.mu.RUnlock()
        return format
    }
    c.mu.RUnlock()
    
    // Детект якщо не в кеші
    var format string
    if data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 {
        format = "mkv"
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
    // MKV
    mkv.Handler(avutil.DefaultHandlers)
    
    // Інші формати...
    // mp4.Handler(avutil.DefaultHandlers)
    // ts.Handler(avutil.DefaultHandlers)
    
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

## 📋 Чек-лист інтеграції mkv.Handler

```go
// ✅ 1. Реєстрація handler у init()
func init() {
    mkv.Handler(avutil.DefaultHandlers)
}

// ✅ 2. Авто-відкриття файлів з детектом формату
demuxer, err := avutil.Open("video.mkv")
if err != nil { /* handle error */ }
defer demuxer.Close()

// ✅ 3. Отримання метаданих перед читанням
streams, err := demuxer.Streams()
if err != nil { /* handle error */ }

// ✅ 4. Фільтрація за підтримуваними кодеками
for _, s := range streams {
    if s.Type() != av.H264 {
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
metrics.RecordDetection("mkv", time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [vdk mkv Package](https://pkg.go.dev/github.com/deepch/vdk/format/mkv) — GoDoc documentation
- 💻 [vdk avutil Package](https://pkg.go.dev/github.com/deepch/vdk/av/avutil) — система реєстрації форматів
- 📄 [Matroska Specification](https://matroska.org/technical/specs/index.html) — офіційна специфікація формату
- 📄 [EBML Specification (RFC 8794)](https://datatracker.ietf.org/doc/html/rfc8794) — стандарт Extensible Binary Meta Language
- 🧪 [MPEG-TS vs MKV Detection](https://wiki.multimedia.cx/index.php/Container_formats) — порівняння форматів

---

> 💡 **Ключова рекомендація для вашого проекту**: **НЕГАЙНО ВИПРАВТЕ Probe-функцію!**
> 1. **Замініть MPEG-TS сигнатуру на EBML ID** — поточна перевірка визначає TS, а не MKV.
> 2. **Документуйте обмеження запису** — повернення `nil` з WriterMuxer може заплутати користувачів.
> 3. **Видаліть AAC з CodecTypes** поки не реалізовано підтримку у mkv.Demuxer.
> 4. **Додайте валідацію вхідних даних** — перевірка довжини буфера перед доступом до b[188].
> 5. **Моніторьте `ProbeLatency`** — різке зростання може вказувати на пошкоджені файли або проблеми з мережею.

Потрібен приклад реалізації коректної Probe-функції для всіх підтримуваних форматів, або інтеграція `mkv.Handler` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀