# 🎬 Глибокий розбір: RTSP → FFmpeg → TS Pipeline

Цей файл — **практичний приклад обробки відео-потоку** з використанням бібліотеки `vdk`. Він демонструє повний цикл: підключення до RTSP камери, транскодування через FFmpeg, та обробку через MPEG-TS muxer/demuxer.

Розберемо архітектуру, критичні моменти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема потоку

```
┌────────────────────────────────────────┐
│ 📦 RTSP → FFmpeg → TS Pipeline         │
├────────────────────────────────────────┤
│                                         │
│  📥 Вхід:                               │
│  • RTSPClient (rtspv2.Dial)            │
│  • URL: "rtsp://url"                   │
│  • DisableAudio: true                  │
│                                         │
│  ⚙️  Обробка:                           │
│  ┌─────────────────┐                   │
│  │ RTSPClient      │                   │
│  │ • OutgoingPacketQueue │              │
│  │ • Signals channel   │                │
│  └────────┬────────┘                   │
│           │ packetAV                   │
│           ▼                            │
│  ┌─────────────────┐                   │
│  │ ts.Muxer        │                   │
│  │ • WriteHeader() │                   │
│  │ • WritePacket() │                   │
│  └────────┬────────┘                   │
│           │ pipe                       │
│           ▼                            │
│  ┌─────────────────┐                   │
│  │ FFmpeg (cmd)    │                   │
│  │ • libx264 encode│                   │
│  │ • ultrafast preset│                  │
│  │ • pipe:1 output │                   │
│  └────────┬────────┘                   │
│           │ pipe                       │
│           ▼                            │
│  ┌─────────────────┐                   │
│  │ ts.Demuxer      │                   │
│  │ • ReadPacket()  │                   │
│  │ • Streams()     │                   │
│  └─────────────────┘                   │
│                                         │
│  🔄 Потік даних:                        │
│  RTSP → RTSPClient → Muxer → FFmpeg  │
│  → Demuxer → Application logic        │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. RTSPClient — підключення до камери

### Налаштування клієнта:

```go
RTSPClient, err := rtspv2.Dial(rtspv2.RTSPClientOptions{
    URL: "rtsp://url",
    DisableAudio: true,              // тільки відео
    DialTimeout: 3 * time.Second,    // таймаут підключення
    ReadWriteTimeout: 5 * time.Second, // таймаут читання/запису
    Debug: true,                     // логування для відладки
    OutgoingProxy: false,            // без проксі
})
if err != nil {
    panic(err)
}
```

### 🔍 Ключові параметри:

| Параметр | Призначення | Рекомендація для CCTV |
|----------|-------------|---------------------|
| `DisableAudio` | Вимкнути аудіо-потік | ✅ Так, якщо обробляєте тільки відео |
| `DialTimeout` | Максимальний час підключення | 3-5с для швидкого виявлення недоступних камер |
| `ReadWriteTimeout` | Таймаут операцій читання/запису | 5-10с для стабільності при поганій мережі |
| `Debug` | Ввімкнути детальне логування | ✅ Для розробки, ❌ для production |
| `OutgoingProxy` | Використання проксі | ❌ Зазвичай не потрібно для локальних камер |

### ✅ Ваш use-case: надійне підключення з повторними спробами

```go
// DialWithRetry — підключення до RTSP з експоненційною затримкою
func DialWithRetry(url string, maxAttempts int) (*rtspv2.RTSPClient, error) {
    var client *rtspv2.RTSPClient
    var err error
    
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        client, err = rtspv2.Dial(rtspv2.RTSPClientOptions{
            URL: url,
            DisableAudio: true,
            DialTimeout: 3 * time.Second,
            ReadWriteTimeout: 5 * time.Second,
            Debug: false,  // вимкнути в production
        })
        if err == nil {
            log.Printf("Connected to %s after %d attempts", url, attempt)
            return client, nil
        }
        
        // Експоненційна затримка перед наступною спробою
        backoff := time.Duration(1<<uint(attempt)) * time.Second
        if backoff > 30*time.Second {
            backoff = 30 * time.Second
        }
        log.Printf("Attempt %d failed: %v, retrying in %v", attempt, err, backoff)
        time.Sleep(backoff)
    }
    
    return nil, fmt.Errorf("failed to connect after %d attempts: %w", maxAttempts, err)
}
```

---

## 🔑 2. FFmpeg pipeline — транскодування через pipe

### Конфігурація команди:

```go
cmd := exec.CommandContext(ctx, "ffmpeg",
    // Вхідні параметри для низької затримки
    "-flags", "low_delay",
    "-analyzeduration", "1",      // мінімальний аналіз потоку
    "-fflags", "-nobuffer",       // вимкнути буферизацію
    "-probesize", "1024k",        // розмір проби потоку
    
    // Вхідний формат
    "-f", "mpegts",
    "-i", "-",                    // читання з stdin (pipe)
    
    // Параметри кодування
    "-vcodec", "libx264",         // H.264 кодек
    "-preset", "ultrafast",       // швидке кодування (менша якість)
    "-bf", "0",                   // без B-frames (менша затримка)
    
    // Вихідний формат
    "-f", "mpegts",
    "-max_muxing_queue_size", "400",  // збільшено для стабільності
    "-pes_payload_size", "0",     // без обмеження розміру PES
    "pipe:1",                     // запис у stdout (pipe)
)
```

### 🔍 Пояснення ключових прапорців:

| Прапорець | Призначення | Вплив на затримку |
|-----------|-------------|------------------|
| `-flags low_delay` | Оптимізація для низької затримки | ✅ Зменшує |
| `-analyzeduration 1` | Мінімальний час аналізу потоку | ✅ Зменшує |
| `-fflags -nobuffer` | Вимкнути внутрішній буфер FFmpeg | ✅ Зменшує |
| `-preset ultrafast` | Швидке кодування (менша компресія) | ✅ Зменшує, ❌ Збільшує бітрейт |
| `-bf 0` | Без B-frames (не потребує майбутніх кадрів) | ✅ Зменшує затримку декодування |
| `-max_muxing_queue_size 400` | Збільшено чергу для уникнення переповнення | ✅ Збільшує стабільність |

### 🔧 Налаштування pipe:

```go
inPipe, _ := cmd.StdinPipe()   // FFmpeg читає з цього pipe
outPipe, _ := cmd.StdoutPipe() // FFmpeg пише у цей pipe

mux := ts.NewMuxer(inPipe)      // запис у stdin FFmpeg
demuxer := ts.NewDemuxer(outPipe) // читання з stdout FFmpeg
```

### ✅ Ваш use-case: безпечний запуск та моніторинг FFmpeg

```go
// SafeFFmpegCommand — запуск FFmpeg з обробкою помилок
func SafeFFmpegCommand(ctx context.Context, args []string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
    cmd := exec.CommandContext(ctx, "ffmpeg", args...)
    
    // Налаштування pipe
    inPipe, err := cmd.StdinPipe()
    if err != nil {
        return nil, nil, nil, fmt.Errorf("stdin pipe: %w", err)
    }
    
    outPipe, err := cmd.StdoutPipe()
    if err != nil {
        inPipe.Close()
        return nil, nil, nil, fmt.Errorf("stdout pipe: %w", err)
    }
    
    // Логування stderr у реальний час
    stderr, err := cmd.StderrPipe()
    if err != nil {
        inPipe.Close()
        outPipe.Close()
        return nil, nil, nil, fmt.Errorf("stderr pipe: %w", err)
    }
    
    // Фонове читання stderr
    go func() {
        scanner := bufio.NewScanner(stderr)
        for scanner.Scan() {
            log.Printf("[FFmpeg stderr] %s", scanner.Text())
        }
    }()
    
    // Запуск команди
    if err := cmd.Start(); err != nil {
        inPipe.Close()
        outPipe.Close()
        return nil, nil, nil, fmt.Errorf("start ffmpeg: %w", err)
    }
    
    // Моніторинг завершення
    go func() {
        if err := cmd.Wait(); err != nil {
            log.Printf("FFmpeg exited with error: %v", err)
        } else {
            log.Printf("FFmpeg exited normally")
        }
    }()
    
    return cmd, inPipe, outPipe, nil
}
```

---

## 🔑 3. TS Muxer/Demuxer — обробка MPEG-TS потоків

### Ініціалізація:

```go
mux := ts.NewMuxer(inPipe)
demuxer := ts.NewDemuxer(outPipe)
codec := RTSPClient.CodecData
mux.WriteHeader(codec)
```

### 🔍 Фоновий обробник демуксера:

```go
go func() {
    // 1. Отримання нових метаданих потоків
    imNewCodec, err := demuxer.Streams()
    log.Println("new codec data", imNewCodec, err)
    for i, data := range imNewCodec {
        log.Println(i, data)
    }
    
    // 2. Основний цикл читання пакетів
    for {
        pkt, err := demuxer.ReadPacket()
        if err != nil {
            log.Panic(err)  // ⚠️ Паніка — небезпечно для production!
        }
        log.Println("im new pkt ===>", pkt.Idx, pkt.Time)
    }
}()
```

### ⚠️ Критичні проблеми у цьому коді:

```go
// ❌ ПРОБЛЕМА 1: log.Panic() зупиняє всю програму
if err != nil {
    log.Panic(err)  // Якщо демуксер помиляється — вся програма падає!
}

// ✅ РІШЕННЯ: обробка помилок та перезапуск
if err != nil {
    log.Printf("demuxer error: %v, attempting reconnect", err)
    // Сигнал про помилку, спроба відновлення
    errorChan <- err
    return
}

// ❌ ПРОБЛЕМА 2: Нескінченний цикл без виходу
for {
    pkt, err := demuxer.ReadPacket()
    // ...
}
// Ніколи не завершується, навіть при ctx.Done()

// ✅ РІШЕННЯ: перевірка контексту
for {
    select {
    case <-ctx.Done():
        return  // коректне завершення
    default:
        pkt, err := demuxer.ReadPacket()
        if err != nil {
            return
        }
        // обробка пакету
    }
}

// ❌ ПРОБЛЕМА 3: Ігнорування результатів StdinPipe/StdoutPipe
inPipe, _ := cmd.StdinPipe()  // помилка ігнорується!
outPipe, _ := cmd.StdoutPipe()

// ✅ РІШЕННЯ: обробка помилок
inPipe, err := cmd.StdinPipe()
if err != nil {
    return fmt.Errorf("stdin pipe: %w", err)
}
```

---

## 🔑 4. Основний цикл обробки пакетів

### Логіка фільтрації та запису:

```go
var start bool  // прапорець: чи почати обробку

for {
    select {
    case signals := <-RTSPClient.Signals:
        switch signals {
        case rtspv2.SignalCodecUpdate:
            // ? — порожня обробка!
        case rtspv2.SignalStreamRTPStop:
            return  // коректне завершення при зупинці потоку
        }
        
    case packetAV := <-RTSPClient.OutgoingPacketQueue:
        // 1. Чекаємо на ключовий кадр перед початком обробки
        if packetAV.IsKeyFrame {
            start = true
        }
        if !start {
            continue  // пропускаємо пакети до першого I-frame
        }
        
        // 2. Запис пакету у muxer (stdin FFmpeg)
        if err = mux.WritePacket(*packetAV); err != nil {
            return  // ⚠️ Тихе завершення без логування помилки!
        }
    }
}
```

### 🔍 Чому чекаємо на ключовий кадр?

```
H.264 потік може починатися з P/B-кадрів, які залежать від попередніх.
Якщо почати кодування з не-ключового кадру:
• Декодер не зможе відтворити зображення
• Виникнуть артефакти або чорний екран
• Плеєр може "зависнути" до наступного ключового кадру

Рішення: пропускати всі пакети до першого `IsKeyFrame == true`
```

### ✅ Ваш use-case: надійна обробка з логуванням та метриками

```go
// ProcessRTSPPackets — надійна обробка пакетів з метриками
func (p *RTSPProcessor) ProcessRTSPPackets(ctx context.Context) error {
    var start bool
    var lastKeyFrameTime time.Time
    var packetCount int64
    
    for {
        select {
        case <-ctx.Done():
            log.Printf("Channel %s: stopping packet processing", p.channelID)
            return ctx.Err()
            
        case signals := <-p.client.Signals:
            switch signals {
            case rtspv2.SignalCodecUpdate:
                // Оновлення кодеків — можна оновити muxer заголовок
                log.Printf("Channel %s: codec update detected", p.channelID)
                // p.mux.WriteHeader(p.client.CodecData)  // якщо підтримується
                
            case rtspv2.SignalStreamRTPStop:
                log.Printf("Channel %s: stream stopped", p.channelID)
                return fmt.Errorf("stream stopped")
            }
            
        case packetAV := <-p.client.OutgoingPacketQueue:
            packetCount++
            
            // Детекція ключових кадрів
            if packetAV.IsKeyFrame {
                start = true
                lastKeyFrameTime = time.Now()
                p.metrics.KeyFramesReceived.Inc()
                
                // Логування інтервалу між ключовими кадрами
                if p.lastKeyFrameTime != nil {
                    interval := time.Since(*p.lastKeyFrameTime)
                    p.metrics.KeyFrameInterval.Observe(interval.Seconds())
                }
                p.lastKeyFrameTime = &lastKeyFrameTime
            }
            
            // Пропуск пакетів до першого ключового кадру
            if !start {
                p.metrics.PacketsSkippedBeforeKeyFrame.Inc()
                continue
            }
            
            // Запис пакету у muxer
            startWrite := time.Now()
            if err := p.mux.WritePacket(*packetAV); err != nil {
                p.metrics.WriteErrors.Inc()
                log.Printf("Channel %s: write packet failed: %v", p.channelID, err)
                return fmt.Errorf("write packet: %w", err)
            }
            p.metrics.WriteLatency.Observe(time.Since(startWrite).Seconds())
            p.metrics.PacketsWritten.Inc()
        }
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// cctv_rtsp_processor.go — інтеграція RTSP → FFmpeg → TS у CCTV HLS Processor
type RTSPProcessor struct {
    channelID    string
    rtspURL      string
    client       *rtspv2.RTSPClient
    ffmpegCmd    *exec.Cmd
    mux          *ts.Muxer
    demuxer      *ts.Demuxer
    packetQueue  chan *av.Packet  // черга для подальшої обробки
    metrics      *RTSPMetrics
    ctx          context.Context
    cancel       context.CancelFunc
}

func NewRTSPProcessor(channelID, rtspURL string) (*RTSPProcessor, error) {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &RTSPProcessor{
        channelID:   channelID,
        rtspURL:     rtspURL,
        packetQueue: make(chan *av.Packet, 100),  // буфер на 100 пакетів
        metrics:     NewRTSPMetrics(channelID),
        ctx:         ctx,
        cancel:      cancel,
    }, nil
}

// Start — запуск обробки потоку
func (p *RTSPProcessor) Start() error {
    // 1. Підключення до RTSP з повторними спробами
    client, err := DialWithRetry(p.rtspURL, 5)
    if err != nil {
        return fmt.Errorf("connect RTSP: %w", err)
    }
    p.client = client
    
    // 2. Запуск FFmpeg pipeline
    ffmpegArgs := []string{
        "-flags", "low_delay",
        "-analyzeduration", "1",
        "-fflags", "-nobuffer",
        "-probesize", "1024k",
        "-f", "mpegts",
        "-i", "-",
        "-vcodec", "libx264",
        "-preset", "ultrafast",
        "-bf", "0",
        "-f", "mpegts",
        "-max_muxing_queue_size", "400",
        "-pes_payload_size", "0",
        "pipe:1",
    }
    
    cmd, inPipe, outPipe, err := SafeFFmpegCommand(p.ctx, ffmpegArgs)
    if err != nil {
        p.client.Close()
        return fmt.Errorf("start FFmpeg: %w", err)
    }
    p.ffmpegCmd = cmd
    
    // 3. Ініціалізація TS muxer/demuxer
    p.mux = ts.NewMuxer(inPipe)
    p.demuxer = ts.NewDemuxer(outPipe)
    
    // Запис заголовка з метаданими кодеків
    if err := p.mux.WriteHeader(p.client.CodecData); err != nil {
        return fmt.Errorf("write header: %w", err)
    }
    
    // 4. Запуск фонового читання з демуксера
    go p.readDemuxerLoop()
    
    // 5. Запуск основної обробки пакетів
    go p.processPacketsLoop()
    
    log.Printf("Channel %s: RTSP processing started", p.channelID)
    return nil
}

// readDemuxerLoop — фонове читання пакетів з демуксера
func (p *RTSPProcessor) readDemuxerLoop() {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Channel %s: demuxer panic recovered: %v", p.channelID, r)
        }
    }()
    
    // Отримання метаданих потоків
    streams, err := p.demuxer.Streams()
    if err != nil {
        log.Printf("Channel %s: get streams failed: %v", p.channelID, err)
        return
    }
    
    for i, stream := range streams {
        log.Printf("Channel %s: stream %d: type=%v", p.channelID, i, stream.Type())
    }
    
    // Основний цикл читання
    for {
        select {
        case <-p.ctx.Done():
            return
            
        default:
            pkt, err := p.demuxer.ReadPacket()
            if err != nil {
                if err == io.EOF {
                    log.Printf("Channel %s: demuxer EOF", p.channelID)
                    return
                }
                log.Printf("Channel %s: read packet failed: %v", p.channelID, err)
                p.metrics.ReadErrors.Inc()
                // Спроба відновлення або сигнал про помилку
                return
            }
            
            // Відправка пакету у чергу для подальшої обробки
            select {
            case p.packetQueue <- &pkt:
                p.metrics.PacketsDemuxed.Inc()
            default:
                // Черга переповнена — пропускаємо пакет
                p.metrics.DroppedPackets.Inc()
            }
        }
    }
}

// processPacketsLoop — основна обробка пакетів з RTSP
func (p *RTSPProcessor) processPacketsLoop() {
    var start bool
    var lastKeyFrameTime time.Time
    
    for {
        select {
        case <-p.ctx.Done():
            return
            
        case signals := <-p.client.Signals:
            p.handleSignal(signals)
            
        case packetAV := <-p.client.OutgoingPacketQueue:
            // Детекція ключових кадрів
            if packetAV.IsKeyFrame {
                start = true
                lastKeyFrameTime = time.Now()
                p.metrics.KeyFramesReceived.Inc()
            }
            
            if !start {
                p.metrics.PacketsSkippedBeforeKeyFrame.Inc()
                continue
            }
            
            // Запис у muxer
            if err := p.mux.WritePacket(*packetAV); err != nil {
                log.Printf("Channel %s: mux write failed: %v", p.channelID, err)
                p.metrics.WriteErrors.Inc()
                return
            }
            
            p.metrics.PacketsMuxed.Inc()
        }
    }
}

// handleSignal — обробка сигналів від RTSP клієнта
func (p *RTSPProcessor) handleSignal(signal rtspv2.Signal) {
    switch signal {
    case rtspv2.SignalCodecUpdate:
        log.Printf("Channel %s: codec update", p.channelID)
        // Можна оновити заголовок якщо muxer підтримує
    case rtspv2.SignalStreamRTPStop:
        log.Printf("Channel %s: stream stopped", p.channelID)
        p.cancel()  // коректне завершення
    }
}

// Stop — зупинка обробки
func (p *RTSPProcessor) Stop() {
    p.cancel()
    
    if p.client != nil {
        p.client.Close()
    }
    if p.ffmpegCmd != nil && p.ffmpegCmd.Process != nil {
        p.ffmpegCmd.Process.Kill()  // примусове завершення
    }
    
    log.Printf("Channel %s: processing stopped", p.channelID)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **FFmpeg не запускається** | "exec: ffmpeg: not found" | Встановіть FFmpeg: `sudo apt install ffmpeg`; перевірте PATH у середовищі виконання |
| **Pipe переповнюється** | Запис у `inPipe` блокується | Збільшіть `-max_muxing_queue_size`; використовуйте неблокуючий запис з таймаутом |
| **Затримка зростає** | Відео відстає від реального часу | Використовуйте `-preset ultrafast`, `-bf 0`; перевірте мережеву затримку до камери |
| **Паніка при `log.Panic()`** | Програма падає при помилці демуксера | Замініть на логування + коректне завершення; додайте `recover()` у горутинах |
| **Витік пам'яті** | `runtime.SetFinalizer` не звільняє ресурси | Завжди викликайте `Close()`/`Free()` явно; не покладайтеся на фіналізатори |

---

## ⚡ Оптимізації для real-time обробки

### 1. Неблокуючий запис у pipe:

```go
// NonBlockingWrite — запис у pipe з таймаутом
func NonBlockingWrite(w io.Writer, data []byte, timeout time.Duration) error {
    done := make(chan error, 1)
    
    go func() {
        _, err := w.Write(data)
        done <- err
    }()
    
    select {
    case err := <-done:
        return err
    case <-time.After(timeout):
        return fmt.Errorf("write timeout after %v", timeout)
    }
}

// Використання:
if err := NonBlockingWrite(inPipe, packetData, 100*time.Millisecond); err != nil {
    log.Printf("write timeout, dropping packet")
    metrics.DroppedPackets.Inc()
}
```

### 2. Буферизація пакетів перед ключовим кадром:

```go
// KeyFrameBuffer — буферизація пакетів до першого ключового кадру
type KeyFrameBuffer struct {
    packets []av.Packet
    maxSize int
}

func (b *KeyFrameBuffer) Add(pkt av.Packet) bool {
    if len(b.packets) >= b.maxSize {
        // Видаляємо найстаріший пакет якщо буфер повний
        b.packets = b.packets[1:]
    }
    b.packets = append(b.packets, pkt)
    return true
}

func (b *KeyFrameBuffer) Flush() []av.Packet {
    pkts := b.packets
    b.packets = nil
    return pkts
}

// Використання:
buffer := &KeyFrameBuffer{maxSize: 50}  // буфер на 50 пакетів

if packetAV.IsKeyFrame {
    // Записуємо всі буферизовані пакети + поточний
    for _, pkt := range buffer.Flush() {
        mux.WritePacket(pkt)
    }
    mux.WritePacket(*packetAV)
} else {
    // Буферизуємо до ключового кадру
    buffer.Add(*packetAV)
}
```

### 3. Моніторинг затримки потоку:

```go
type StreamLatencyMetrics struct {
    IngestTime    prometheus.GaugeVec  // час отримання пакету з камери
    ProcessTime   prometheus.GaugeVec  // час обробки через FFmpeg
    EndToEndDelay prometheus.HistogramVec  // загальна затримка
}

func (m *StreamLatencyMetrics) RecordPacket(channelID string, ingestTime time.Time) {
    now := time.Now()
    m.IngestTime.WithLabelValues(channelID).Set(float64(ingestTime.UnixNano()))
    m.EndToEndDelay.WithLabelValues(channelID).Observe(now.Sub(ingestTime).Seconds())
}
```

---

## 📋 Чек-лист інтеграції

```go
// ✅ 1. Підключення до RTSP з обробкою помилок
client, err := DialWithRetry(rtspURL, 5)
if err != nil {
    return fmt.Errorf("connect: %w", err)
}
defer client.Close()

// ✅ 2. Запуск FFmpeg з моніторингом
cmd, inPipe, outPipe, err := SafeFFmpegCommand(ctx, args)
if err != nil {
    return err
}
// Не забути: defer cmd.Wait() або моніторинг у горутині

// ✅ 3. Ініціалізація muxer/demuxer з перевіркою
mux := ts.NewMuxer(inPipe)
if err := mux.WriteHeader(client.CodecData); err != nil {
    return err
}

// ✅ 4. Обробка сигналів та пакетів з перевіркою контексту
for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case pkt := <-client.OutgoingPacketQueue:
        // обробка
    }
}

// ✅ 5. Логування помилок замість panic
if err != nil {
    log.Printf("error: %v", err)
    return err  // або спроба відновлення
}

// ✅ 6. Метрики для моніторингу
metrics.PacketsProcessed.Inc()
metrics.ProcessLatency.Observe(duration.Seconds())

// ✅ 7. Graceful shutdown
func shutdown() {
    cancel()  // скасування контексту
    if client != nil { client.Close() }
    if cmd != nil && cmd.Process != nil { cmd.Process.Kill() }
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk rtspv2 Package](https://pkg.go.dev/github.com/deepch/vdk/format/rtspv2) — GoDoc documentation
- 💻 [vdk ts Package](https://pkg.go.dev/github.com/deepch/vdk/format/ts) — MPEG-TS muxer/demuxer
- 📄 [FFmpeg Streaming Guide](https://trac.ffmpeg.org/wiki/StreamingGuide) — практичні поради для стрімінгу
- 📄 [H.264 Low-Latency Encoding](https://trac.ffmpeg.org/wiki/Encode/H.264#Lowlatency) — оптимізація для низької затримки
- 🧪 [Go exec.Command Documentation](https://pkg.go.dev/os/exec#Command) — робота з зовнішніми процесами

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV в реальному часі**:
> 1. **Замініть `log.Panic()` на обробку помилок** — паніка зупиняє всю програму; для production потрібна стійкість.
> 2. **Додайте перевірку `ctx.Done()` у всі цикли** — це забезпечить коректне завершення при зупинці каналу.
> 3. **Використовуйте буферизацію до ключового кадру** — це уникне втрати початку відео після підключення.
> 4. **Моніторьте затримку на кожному етапі** — вхід (RTSP) → обробка (FFmpeg) → вихід (TS) — щоб виявляти вузькі місця.
> 5. **Тестуйте з різними камерами** — різні виробники можуть надсилати різні формати/таймінги, які потребують налаштування.

Потрібен приклад інтеграції цього `RTSPProcessor` з вашим `pubsub.Queue` для розподілу вже оброблених відео-пакетів між підписниками (HLS-генератор, WebSocket, архів)? Готовий допомогти! 🚀