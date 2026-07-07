# 🎬 Глибокий розбір: avconv — Transcoding Wrapper на базі vdk

Цей файл — **обгортка для бібліотеки `vdk` (Video Development Kit)**, яка надає функціонал транскодування медіа-потоків, подібний до `ffmpeg/avconv`. Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема avconv

```
┌────────────────────────────────────────┐
│ 📦 avconv Architecture                  │
├────────────────────────────────────────┤
│                                         │
│  📥 Вхід:                               │
│  • av.Demuxer — читання вхідного потоку│
│    (файл, RTSP, HTTP, memory buffer)   │
│                                         │
│  ⚙️  Транскодування:                    │
│  • transcode.Demuxer — ядро конвертації│
│  • Audio decoder/encoder pipeline      │
│  • pktque.Filters — фільтрація пакетів │
│  • Walltime — real-time режим (-re)    │
│                                         │
│  📤 Вихід:                              │
│  • av.Muxer — запис у цільовий формат │
│    (HLS, MP4, TS, WebM тощо)           │
│                                         │
│  🔧 CLI інтерфейс:                      │
│  • -i input, -o output, -t duration    │
│  • -re (real-time), -v (verbose)       │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔍 Детальний розбір ключових компонентів

### 1️⃣ `Demuxer` — транскодуючий демуксер

```go
type Demuxer struct {
    transdemux *transcode.Demuxer  // ядро транскодування
    streams    []av.CodecData      // кешовані кодеки вихідних потоків
    Options                        // налаштування вихідних кодеків
    Demuxer av.Demuxer            // вихідний демуксер (джерело)
}
```

### 🔁 Потік даних:

```
Вхідний потік (av.Demuxer)
         ↓
   [prepare()] — ініціалізація
         ↓
┌─────────────────────┐
│ transcode.Demuxer   │
│ • Виявлення кодеків │
│ • Вибір енкодера   │
│ • Створення декадера│
└────────┬────────────┘
         ↓
   ReadPacket() — читання вже транскодованих пакетів
         ↓
   pktque.FilterDemuxer — опціональні фільтри
         ↓
   av.Muxer — запис у вихідний файл/потік
```

### ✅ Ваш use-case: транскодування аудіо для HLS

```go
// У вашому pipeline: конвертація вхідного аудіо у AAC для HLS
func (p *HLSEncoder) transcodeAudioForHLS(inputDemuxer av.Demuxer) (*avconv.Demuxer, error) {
    // Вказуємо підтримувані вихідні кодеки (AAC для HLS)
    options := avconv.Options{
        OutputCodecTypes: []av.CodecType{
            av.AAC,      // основний вибір для HLS
            av.MP3,      // fallback
        },
    }
    
    convDemux := &avconv.Demuxer{
        Options: options,
        Demuxer: inputDemuxer,
    }
    
    // Ініціалізація (викликає prepare())
    streams, err := convDemux.Streams()
    if err != nil {
        return nil, fmt.Errorf("transcode init: %w", err)
    }
    
    // Логування результатів транскодування
    for _, s := range streams {
        log.Printf("Stream: type=%s, codec=%s", s.Type(), s.Codec())
    }
    
    return convDemux, nil
}
```

---

### 2️⃣ Транскодування аудіо: `FindAudioDecoderEncoder`

```go
transopts.FindAudioDecoderEncoder = func(codec av.AudioCodecData, i int) (
    ok bool, dec av.AudioDecoder, enc av.AudioEncoder, err error) {
    
    // 1. Перевірка чи кодек вже підтримується
    support := false
    for _, typ := range supports {
        if typ == codec.Type() {
            support = true
        }
    }
    if support {
        return  // не потрібно транскодувати
    }
    ok = true  // потрібно транскодувати
    
    // 2. Пошук доступного енкодера зі списку підтримуваних
    var enctype av.CodecType
    for _, typ := range supports {
        if typ.IsAudio() {
            if enc, _ = avutil.DefaultHandlers.NewAudioEncoder(typ); enc != nil {
                enctype = typ
                break
            }
        }
    }
    if enc == nil {
        err = fmt.Errorf("avconv: convert %s->%s failed", codec.Type(), enctype)
        return
    }
    
    // 3. Створення декадера для вхідного кодека
    if dec, err = avutil.DefaultHandlers.NewAudioDecoder(codec); err != nil {
        err = fmt.Errorf("avconv: decode %s failed", codec.Type())
        return
    }
    
    return  // повертаємо пару decoder+encoder
}
```

### 🔑 Ключові моменти:

| Етап | Що відбувається | Чому важливо |
|------|----------------|--------------|
| **Перевірка підтримки** | Якщо вхідний кодек вже у `OutputCodecTypes` — пропускаємо транскодування | Економія ресурсів, уникнення покоління поколінь |
| **Пошук енкодера** | Перебір `supports` для знаходження першого доступного енкодера | Гнучкість: можна вказати кілька варіантів з пріоритетом |
| **Створення декадера** | `NewAudioDecoder(codec)` готує вхідні дані до декодування | Без цього неможливо прочитати вхідний потік |

### ✅ Ваш use-case: підтримка мультикодеків для CCTV

```go
// Конфігурація для різних джерел відео
type TranscodeConfig struct {
    // Вхідні кодеки, які приймаємо без транскодування
    PassthroughCodecs []av.CodecType
    
    // Бажані вихідні кодеки (за пріоритетом)
    PreferredOutputCodecs []av.CodecType
    
    // Fallback кодеки якщо основні недоступні
    FallbackCodecs []av.CodecType
}

func (cfg *TranscodeConfig) GetOutputCodecTypes() []av.CodecType {
    // Об'єднуємо preferred + fallback
    return append(cfg.PreferredOutputCodecs, cfg.FallbackCodecs...)
}

// Приклад для Al Arabiya каналу:
config := TranscodeConfig{
    PassthroughCodecs: []av.CodecType{av.AAC, av.H264},  // вже підходить для HLS
    PreferredOutputCodecs: []av.CodecType{av.AAC, av.H264},  // бажаний формат
    FallbackCodecs: []av.CodecType{av.MP3, av.H264},  // якщо AAC недоступний
}

options := avconv.Options{
    OutputCodecTypes: config.GetOutputCodecTypes(),
}
```

---

### 3️⃣ `ConvertCmdline` — CLI інтерфейс (avconv-подібний)

```go
// Підтримувані прапорці:
// -i input.ts      — вхідний файл/потік
// -o output.m3u8   — вихідний файл (розширення визначає формат)
// -t 30.5          — обмеження тривалості (секунди)
// -re              — real-time режим (читання зі швидкістю відтворення)
// -v               — verbose режим (лог кожного пакету)

// Приклади використання:
// Конвертація TS → HLS:
avconv -i input.ts -o output.m3u8

// Real-time стрімінг з обмеженням 60 секунд:
avconv -re -i rtsp://camera/stream -t 60 -o hls/segment_%03d.ts

// Пере інформацію про потоки:
avconv -v -i input.mp4 -o /dev/null
```

### 🔍 Парсинг аргументів:

```go
for _, arg := range args {
    switch arg {
    case "-i": flagi = true          // наступний аргумент = input
    case "-t": flagt = true          // наступний аргумент = duration
    case "-re": flagre = true        // увімкнути Walltime фільтр
    case "-v": flagv = true          // увімкнути verbose лог
    
    default:
        if flagi { input = arg; flagi = false }
        if flagt { 
            var f float64
            fmt.Sscanf(arg, "%f", &f)
            duration = time.Duration(f * float64(time.Second))
            flagt = false
        }
        if !flagi && !flagt { output = arg }  // останній аргумент = output
    }
}
```

### ✅ Ваш use-case: інтеграція з вашим CLI

```go
// У вашому main.go: додавання avconv як підкоманди
func main() {
    cmd := astikit.FlagCmd()
    flag.Parse()
    
    switch cmd {
    case "transcode":
        // Виклик avconv.ConvertCmdline з аргументами
        args := flag.Args()
        if err := avconv.ConvertCmdline(args); err != nil {
            log.Fatalf("transcode failed: %v", err)
        }
        
    case "hls-encode":
        // Спеціалізована команда для HLS з вашими налаштуваннями
        config := loadChannelConfig(*channelID)
        if err := encodeToHLS(*inputPath, *outputDir, config); err != nil {
            log.Fatalf("hls encode failed: %v", err)
        }
    }
}

// encodeToHLS — обгортка над avconv з CCTV-специфічними налаштуваннями
func encodeToHLS(input, outputDir string, config *ChannelConfig) error {
    // Підготовка аргументів для avconv
    args := []string{
        "-i", input,
        "-re",  // real-time для live-стрімінгу
    }
    
    // Додаємо транскодування аудіо якщо потрібно
    if config.TranscodeAudio {
        args = append(args, "-acodec", "aac")
    }
    
    // Вихід у HLS формат
    args = append(args, "-f", "hls", 
        "-hls_time", "10",  // 10-секундні сегменти
        "-hls_list_size", "5",  // тримати 5 сегментів у плейлисті
        filepath.Join(outputDir, "stream.m3u8"))
    
    return avconv.ConvertCmdline(args)
}
```

---

### 4️⃣ Фільтри пакетів: `pktque.Filters`

```go
filters := pktque.Filters{}
if flagre {
    // Walltime фільтр: читає пакети з реальною швидкістю
    // Корисно для симуляції live-стрімінгу з файлу
    filters = append(filters, &pktque.Walltime{})
}
filterdemux := &pktque.FilterDemuxer{
    Demuxer: convdemux,
    Filter:  filters,
}
```

### 🔍 Як працює `Walltime`:

```
Без -re:
• Читає пакети максимально швидко
• Корисно для офлайн-конвертації

З -re:
• Чекає стільки, скільки триває пакет за PTS
• Наприклад: пакет з тривалістю 40мс → sleep(40ms) перед наступним
• Корисно для тестування HLS-плеєрів у real-time
```

### ✅ Ваш use-case: симуляція live-потоків для тестування

```go
// TestLiveStream — тестування HLS-плеєра з записаного файлу
func (p *TestRunner) SimulateLiveStream(recordedFile string, duration time.Duration) error {
    // Використовуємо -re для real-time відтворення
    args := []string{
        "-re",
        "-i", recordedFile,
        "-t", fmt.Sprintf("%.1f", duration.Seconds()),
        "-f", "hls",
        "-hls_time", "4",  // 4-секундні сегменти як у продакшені
        "/tmp/test_stream/stream.m3u8",
    }
    
    // Запуск у окремій горутині
    done := make(chan error, 1)
    go func() {
        done <- avconv.ConvertCmdline(args)
    }()
    
    // Паралельно запускаємо тест плеєра
    go p.testHLSPlayer("http://localhost:8080/test_stream/stream.m3u8")
    
    return <-done
}
```

---

### 5️⃣ Обробка пакетів: основний цикл

```go
for {
    var pkt av.Packet
    if pkt, err = filterdemux.ReadPacket(); err != nil {
        if err == io.EOF {
            err = nil
            break
        }
        return err
    }
    
    // Verbose лог
    if flagv {
        fmt.Println(pkt.Idx, pkt.Time, len(pkt.Data), pkt.IsKeyFrame)
    }
    
    // Обмеження за тривалістю
    if duration != 0 && pkt.Time > duration {
        break
    }
    
    // Запис у вихідний muxer
    if err = muxer.WritePacket(pkt); err != nil {
        return err
    }
}
```

### 🔑 Поля `av.Packet`:

| Поле | Тип | Призначення |
|------|-----|-------------|
| `Idx` | `int` | Індекс потоку (0=відео, 1=аудіо, 2=субтитри) |
| `Time` | `time.Duration` | PTS пакета відносно початку потоку |
| `Data` | `[]byte` | Сирі дані кадру/фрейму |
| `IsKeyFrame` | `bool` | Чи є відео-кадр ключовим (I-frame) |

### ✅ Ваш use-case: фільтрація пакетів для субтитрів

```go
// ProcessPacket — обробка одного пакету з можливістю фільтрації
func (p *SubtitleExtractor) ProcessPacket(pkt av.Packet) error {
    // Пропускаємо не-субтитр потоки
    if pkt.Idx != p.subtitleStreamIdx {
        return nil
    }
    
    // Логування ключових кадрів для синхронізації
    if pkt.IsKeyFrame {
        log.Printf("Key frame at %v, size=%d", pkt.Time, len(pkt.Data))
    }
    
    // Відправка у процесор телетексту
    return p.processor.ProcessTeletextData(pkt.Data, pkt.Time)
}

// Інтеграція у основний цикл avconv:
for {
    pkt, err := filterdemux.ReadPacket()
    if err != nil { /* ... */ }
    
    // Ваша кастомна обробка
    if err := p.ProcessPacket(pkt); err != nil {
        log.Warn("packet processing failed", "err", err)
        // Не перериваємо цикл — продовжуємо обробку інших пакетів
    }
    
    // Стандартний запис у muxer
    if err = muxer.WritePacket(pkt); err != nil {
        return err
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// hls_transcoder.go — транскодування CCTV потоків у HLS
type HLSTranscoder struct {
    channelID      string
    inputDemuxer   av.Demuxer
    outputMuxer    av.Muxer
    convDemux      *avconv.Demuxer
    config         *ChannelConfig
    segmentDuration time.Duration
}

func NewHLSTranscoder(channelID string, inputURL string, config *ChannelConfig) (*HLSTranscoder, error) {
    // 1. Відкриття вхідного потоку (RTSP/HTTP/файл)
    demuxer, err := avutil.Open(inputURL)
    if err != nil {
        return nil, fmt.Errorf("open input: %w", err)
    }
    
    // 2. Налаштування транскодування
    options := avconv.Options{
        OutputCodecTypes: []av.CodecType{
            av.H264,  // відео
            av.AAC,   // аудіо
        },
    }
    
    // 3. Створення транскодуючого демуксера
    convDemux := &avconv.Demuxer{
        Options: options,
        Demuxer: demuxer,
    }
    
    return &HLSTranscoder{
        channelID:      channelID,
        inputDemuxer:   demuxer,
        convDemux:      convDemux,
        config:         config,
        segmentDuration: 10 * time.Second,
    }, nil
}

// Start — запуск транскодування у HLS
func (t *HLSTranscoder) Start(ctx context.Context, outputDir string) error {
    // 1. Отримання транскодованих потоків
    streams, err := t.convDemux.Streams()
    if err != nil {
        return fmt.Errorf("get streams: %w", err)
    }
    
    // 2. Створення HLS muxer
    hlsPath := filepath.Join(outputDir, t.channelID, "stream.m3u8")
    muxer, err := avutil.Create(hlsPath)
    if err != nil {
        return fmt.Errorf("create HLS muxer: %w", err)
    }
    t.outputMuxer = muxer
    
    // 3. Запис заголовка HLS
    if err = muxer.WriteHeader(streams); err != nil {
        return err
    }
    
    // 4. Основний цикл читання/запису
    ticker := time.NewTicker(t.segmentDuration)
    defer ticker.Stop()
    
    segmentNum := 0
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-ticker.C:
            // Ротація сегментів (спрощено)
            segmentNum++
            log.Printf("Channel %s: starting segment %d", t.channelID, segmentNum)
        }
        
        // Читання пакету
        pkt, err := t.convDemux.ReadPacket()
        if err != nil {
            if err == io.EOF {
                break
            }
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Кастомна обробка (субтитри, метрики тощо)
        t.processPacket(pkt)
        
        // Запис у HLS
        if err = muxer.WritePacket(pkt); err != nil {
            return fmt.Errorf("write packet: %w", err)
        }
    }
    
    // 5. Завершення
    return muxer.WriteTrailer()
}

// processPacket — кастомна обробка пакетів
func (t *HLSTranscoder) processPacket(pkt av.Packet) {
    // Експорт метрик
    monitoring.PacketsProcessed.WithLabelValues(t.channelID).Inc()
    monitoring.PacketSize.WithLabelValues(t.channelID).Observe(float64(len(pkt.Data)))
    
    // Обробка субтитрів якщо це потрібний потік
    if pkt.Idx == t.config.SubtitleStreamIdx {
        _ = t.extractSubtitles(pkt)  // ваша логіка
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"convert X->Y failed"** | Енкодер для цільового кодека не знайдено | Переконайтеся, що `vdk` зібрано з підтримкою потрібних кодеків (AAC, H.264) |
| **Аудіо розсинхронізоване** | Неправильна обробка PTS/DTS | Перевірте чи `Walltime` фільтр не ламає таймінги; вимкніть `-re` для офлайн-конвертації |
| **Повільне транскодування** | Програмне декодування замість апаратного | Використовуйте `vdk` з підтримкою FFmpeg backend або апаратних енкодерів (NVENC, QSV) |
| **HLS плейлист не оновлюється** | `WriteTrailer()` не викликано або сегменти не закриваються | Для live HLS використовуйте спеціальний muxer з авто-ротацією сегментів |
| **Пам'ять росте** | Буфери не очищаються між сегментами | Викликайте `Close()` на старих muxers, використовуйте `sync.Pool` для буферів |

---

## ⚡ Оптимізації для real-time обробки

### 1. Апаратне транскодування (якщо доступно):

```go
// Перевірка доступності апаратних енкодерів
func getPreferredEncoder(codec av.CodecType) av.AudioEncoder {
    // Пріоритет: NVENC > QSV > VA-API > software
    hardwareEncoders := []string{"nvenc", "qsv", "vaapi"}
    
    for _, name := range hardwareEncoders {
        if enc, _ := avutil.DefaultHandlers.NewAudioEncoderByName(codec, name); enc != nil {
            log.Printf("Using hardware encoder: %s", name)
            return enc
        }
    }
    
    // Fallback на програмний енкодер
    log.Printf("Using software encoder for %s", codec)
    enc, _ := avutil.DefaultHandlers.NewAudioEncoder(codec)
    return enc
}
```

### 2. Пакетна обробка для зменшення накладних витрат:

```go
// Замість WritePacket для кожного пакету:
func (t *HLSTranscoder) writePacketBatch(packets []av.Packet) error {
    for _, pkt := range packets {
        if err := t.outputMuxer.WritePacket(pkt); err != nil {
            return err
        }
    }
    // Один Flush замість багатьох системних викликів
    if flusher, ok := t.outputMuxer.(interface{ Flush() error }); ok {
        return flusher.Flush()
    }
    return nil
}
```

### 3. Кешування енкодерів/декодерів:

```go
type CodecCache struct {
    decoders map[av.CodecType]av.AudioDecoder
    encoders map[av.CodecType]av.AudioEncoder
    mu       sync.RWMutex
}

func (c *CodecCache) GetDecoder(codec av.AudioCodecData) (av.AudioDecoder, error) {
    c.mu.RLock()
    if dec, ok := c.decoders[codec.Type()]; ok {
        c.mu.RUnlock()
        return dec, nil
    }
    c.mu.RUnlock()
    
    // Створення нового декадера
    dec, err := avutil.DefaultHandlers.NewAudioDecoder(codec)
    if err != nil {
        return nil, err
    }
    
    c.mu.Lock()
    c.decoders[codec.Type()] = dec
    c.mu.Unlock()
    
    return dec, nil
}
```

---

## 📋 Чек-лист інтеграції

```go
// ✅ 1. Відкриття вхідного потоку
demuxer, err := avutil.Open(inputURL)  // підтримує file://, rtsp://, http://

// ✅ 2. Налаштування транскодування
options := avconv.Options{
    OutputCodecTypes: []av.CodecType{av.H264, av.AAC},
}

// ✅ 3. Створення транскодуючого демуксера
convDemux := &avconv.Demuxer{
    Options: options,
    Demuxer: demuxer,
}
streams, _ := convDemux.Streams()  // ініціалізація

// ✅ 4. Створення вихідного muxer (HLS/MP4/TS)
muxer, _ := avutil.Create(outputPath)
muxer.WriteHeader(streams)

// ✅ 5. Основний цикл: ReadPacket → Process → WritePacket
for {
    pkt, err := convDemux.ReadPacket()
    if err == io.EOF { break }
    
    // Кастомна обробка (субтитри, метрики)
    processPacket(pkt)
    
    // Запис у вихід
    muxer.WritePacket(pkt)
}

// ✅ 6. Завершення
muxer.WriteTrailer()
convDemux.Close()
demuxer.Close()

// ✅ 7. Метрики
monitoring.TranscodeLatency.Observe(time.Since(start).Seconds())
monitoring.BytesTranscoded.Add(float64(totalBytes))
```

---

## 🔗 Корисні посилання

- 💻 [vdk GitHub](https://github.com/deepch/vdk) — основна бібліотека для роботи з медіа
- 📄 [vdk Documentation](https://pkg.go.dev/github.com/deepch/vdk) — GoDoc API reference
- 🎬 [HLS Spec (RFC 8216)](https://datatracker.ietf.org/doc/html/rfc8216) — специфікація HLS для правильного форматування виходу
- 🧪 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади використання демуксингу/транскодування
- 🔧 [FFmpeg Codec Support](https://ffmpeg.org/general.html) — список кодеків, які можуть бути доступні через vdk backend

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Використовуйте `-re` тільки для тестування** — у продакшені краще читати максимально швидко та керувати швидкістю на рівні HLS-плейлиста.
> 2. **Фільтруйте потоки на ранньому етапі** — не транскодуйте відео якщо потрібні тільки субтитри.
> 3. **Кешуйте енкодери/декодери** — створення нового енкодера для кожного каналу дорого.
> 4. **Моніторьте розмір пакетів та затримки** — великі пакети можуть лагати HLS-сегментацію.
> 5. **Тестуйте з різними вхідними кодеками** — CCTV камери часто використовують H.264 Main profile, який може вимагати транскодування для HLS compatibility.

Потрібен приклад інтеграції `avconv.Demuxer` з вашим `segmentAssembler` для синхронізації транскодованого аудіо з відео та субтитрами? Готовий допомогти! 🚀