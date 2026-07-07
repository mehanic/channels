# 🎬 Глибокий розбір: dvrip — Пропрієтарний протокол для DVR/IP камер

Цей файл — **реалізація клієнта для пропрієтарного протоколу DVRIp**, який часто використовується у китайських системах відеоспостереження (XMeye, VStarcam, тощо). Він надає механізми для підключення, автентифікації, отримання відео/аудіо потоків та їх обробки у форматі `vdk`.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема dvrip клієнта

```
┌────────────────────────────────────────┐
│ 📦 dvrip.Client — Proprietary Protocol │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Client — основна структура          │
│  • Dial() — підключення та ініціалізація│
│  • Monitor() — фоновий цикл отримання даних│
│  • Command()/send()/recv() — мережевий протокол│
│                                         │
│  🔄 Потік даних:                        │
│  TCP → parseURL → Login → SetTime → Monitor│
│           ↓                              │
│  Binary frames → NALU split → av.Packet queue│
│                                         │
│  📡 Протокол:                           │
│  • Заголовок: 20 байт (Payload struct) │
│  • Тіло: JSON або бінарні дані         │
│  • Магічні числа: 0xFF, 0x1FC, 0x1FD тощо│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Client — основна структура

### Поля та їх призначення:

```go
type Client struct {
    conn                net.Conn              // TCP з'єднання
    login, password     string                // облікові дані
    host, stream        string                // хост:порт та шлях потоку
    sequenceNumber      int32                 // лічильник послідовності пакетів
    session             int32                 // ID сесії після логіну
    aliveInterval       time.Duration         // інтервал KeepAlive
    CodecData           []av.CodecData        // метадані кодеків (оновлюються динамічно)
    OutgoingPacketQueue chan *av.Packet       // черга вихідних пакетів для обробки
    Signals             chan int              // сигнали подій (Stop, CodecUpdate)
    options             ClientOptions         // налаштування клієнта
    sps, pps            []byte                // кешовані SPS/PPS для H.264
}
```

### 🔧 ClientOptions — конфігурація:

```go
type ClientOptions struct {
    Debug            bool           // логування для відладки
    URL              string         // "dvrip://user:pass@host:port/stream"
    DialTimeout      time.Duration  // таймаут підключення
    ReadWriteTimeout time.Duration  // таймаут операцій
    DisableAudio     bool           // вимкнути аудіо-потік
}
```

### ✅ Ваш use-case: створення клієнта з валідацією

```go
// NewDVRIpClient — безпечне створення клієнта з перевіркою параметрів
func NewDVRIpClient(rawURL string, disableAudio bool) (*dvrip.Client, error) {
    // 1. Парсинг та валідація URL
    parsed, err := url.Parse(rawURL)
    if err != nil {
        return nil, fmt.Errorf("invalid URL: %w", err)
    }
    
    // 2. Перевірка обов'язкових полів
    if parsed.Host == "" {
        return nil, fmt.Errorf("missing host in URL")
    }
    
    // 3. Налаштування опцій
    opts := dvrip.ClientOptions{
        URL:              rawURL,
        DialTimeout:      5 * time.Second,
        ReadWriteTimeout: 10 * time.Second,
        DisableAudio:     disableAudio,
        Debug:            false,  // вимкнути в production
    }
    
    // 4. Підключення
    client, err := dvrip.Dial(opts)
    if err != nil {
        return nil, fmt.Errorf("dial failed: %w", err)
    }
    
    // 5. Реєстрація фіналізатора для очищення
    runtime.SetFinalizer(client, func(c *dvrip.Client) {
        log.Printf("warning: DVRIp client not explicitly closed")
        c.Close()
    })
    
    return client, nil
}
```

---

## 🔑 2. Dial() — підключення та ініціалізація

### Потік ініціалізації:

```go
func Dial(options ClientOptions) (*Client, error) {
    client := &Client{
        Signals:             make(chan int, 100),
        OutgoingPacketQueue: make(chan *av.Packet, 3000),  // великий буфер!
        options:             options,
    }
    
    // 1. Парсинг URL
    err := client.parseURL(html.UnescapeString(client.options.URL))
    if err != nil { return nil, err }
    
    // 2. TCP підключення
    client.conn, err = net.DialTimeout("tcp", client.host, 2*time.Second)
    if err != nil { return nil, err }
    
    // 3. Встановлення дедлайну для ініціалізації
    err = client.conn.SetDeadline(time.Now().Add(5 * time.Second))
    
    // 4. Логін
    err = client.Login()
    
    // 5. Синхронізація часу
    err = client.SetTime()
    
    // 6. Запуск фонового моніторингу
    go client.Monitor()
    
    return client, nil
}
```

### 🔍 Критичні моменти:

| Етап | Призначення | Ризики |
|------|-------------|--------|
| `parseURL()` | Витягування login/pass/host/stream з URL | Необроблений HTML-encoding може зламати парсинг |
| `net.DialTimeout` | Підключення з таймаутом | Короткий таймаут (2с) може бути недостатнім для поганої мережі |
| `SetDeadline` | Обмеження часу на ініціалізацію | Якщо Login/SetTime займають довше 5с — помилка |
| `Login()` | Автентифікація з MD5 хешем пароля | `sofiaHash()` має бути реалізована коректно |
| `SetTime()` | Синхронізація часу з камерою | Може бути необов'язковим, але деякі камери вимагають |
| `go client.Monitor()` | Запуск фонового циклу отримання даних | Помилки у горутинах не повертаються — потрібен `Signals` канал |

### ✅ Ваш use-case: надійне підключення з повторними спробами

```go
// DialWithRetry — підключення з експоненційною затримкою
func DialWithRetry(opts dvrip.ClientOptions, maxAttempts int) (*dvrip.Client, error) {
    var client *dvrip.Client
    var err error
    
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        client, err = dvrip.Dial(opts)
        if err == nil {
            log.Printf("Connected after %d attempts", attempt)
            return client, nil
        }
        
        // Експоненційна затримка
        backoff := time.Duration(1<<uint(attempt)) * time.Second
        if backoff > 30*time.Second {
            backoff = 30 * time.Second
        }
        log.Printf("Attempt %d failed: %v, retrying in %v", attempt, err, backoff)
        time.Sleep(backoff)
    }
    
    return nil, fmt.Errorf("failed after %d attempts: %w", maxAttempts, err)
}
```

---

## 🔑 3. Monitor() — фоновий цикл отримання даних

### Основна логіка:

```go
func (client *Client) Monitor() {
    defer func() {
        client.Signals <- SignalStreamStop  // сигнал про завершення
    }()
    
    // 1. Claim моніторингу
    client.Command(codeOPMonitor, map[string]interface{}{
        "Action": "Claim",
        "Parameter": map[string]interface{}{
            "Channel": 0, "CombinMode": "NONE",
            "StreamType": client.stream, "TransMode": "TCP",
        },
    })
    
    // 2. Старт потоку
    payload, _ := json.Marshal(map[string]interface{}{
        "Name": "OPMonitor", "SessionID": fmt.Sprintf("0x%08X", client.session),
        "OPMonitor": map[string]interface{}{ /* параметри */ },
    })
    client.send(1410, payload)
    
    // 3. Основний цикл
    var length uint32
    var dataType uint32
    timer := time.Now()
    var fps int
    
    for {
        // KeepAlive
        if time.Since(timer) > client.aliveInterval {
            client.SetKeepAlive()
            timer = time.Now()
        }
        
        // Отримання відповіді
        _, body, err := client.recv(false)
        if err != nil { return }
        
        buf := bytes.NewReader(body)
        binary.Read(buf, binary.BigEndian, &dataType)
        
        switch dataType {
        case 0x1FC, 0x1FE:  // Відео фрейми
            // Парсинг заголовка фрейму
            // Отримання даних (можливо частинами через recvSize)
            // Split NALUs через h264parser
            // Відправка у OutgoingPacketQueue
            
        case 0x1FD:  // Інший тип відео даних
            // Аналогічна обробка
            
        case 0x1FA, 0x1F9:  // Аудіо (PCM A-law)
            if client.options.DisableAudio { continue }
            // Парсинг аудіо фрейму
            // Відправка у чергу якщо кодек ініціалізовано
            
        default:
            continue  // ігнорування невідомих типів
        }
    }
}
```

### 🔍 Обробка відео-пакетів (0x1FC/0x1FE):

```go
// 1. Парсинг заголовка фрейму
frame := struct {
    Media    byte   // тип медіа (0=відео, 1=аудіо)
    FPS      byte   // кадри в секунду
    Width    byte   // ширина (не завжди використовується)
    Height   byte   // висота
    DateTime uint32 // timestamp
    Length   uint32 // довжина даних
}{}
binary.Read(buf, binary.LittleEndian, &frame)

// 2. Отримання даних (можливо частинами)
var packet bytes.Buffer
if frame.Length > uint32(buf.Len()) {
    // Потрібно дочитати решту
    need := frame.Length - uint32(buf.Len())
    buf.WriteTo(&packet)
    client.recvSize(&packet, need)  // рекурсивне читання
} else {
    buf.WriteTo(&packet)
}

// 3. Розбиття на NALU
packets, _ := h264parser.SplitNALUs(packet.Bytes())

// 4. Обробка кожного NALU
for _, nalu := range packets {
    naluType := nalu[0] & 0x1f
    switch {
    case naluType >= 1 && naluType <= 5:  // VCL NALU (відео дані)
        client.OutgoingPacketQueue <- &av.Packet{
            Duration:   time.Duration(1000/fps) * time.Millisecond,
            Idx:        0,  // відео потік
            IsKeyFrame: naluType == 5,  // IDR = ключовий кадр
            Data:       append(binSize(len(nalu)), nalu...),  // додаємо 4-байтову довжину для AVCC
        }
    case naluType == 7:  // SPS
        client.CodecUpdateSPS(nalu)
    case naluType == 8:  // PPS
        client.CodecUpdatePPS(nalu)
    }
}
```

### 🔍 Чому `binSize(len(nalu))`?

```
Функція `binSize` (не показана у коді, але ймовірно):
  func binSize(n int) []byte {
      return []byte{byte(n>>24), byte(n>>16), byte(n>>8), byte(n)}
  }

Це додає 4-байтову довжину перед кожним NALU,
перетворюючи Annex B формат (start codes) у AVCC формат (length-prefixed).

Це потрібно для сумісності з контейнерами на кшталт MP4/FLV,
де NALU зберігаються з префіксом довжини, а не start codes.
```

### ✅ Ваш use-case: обробка пакетів з черги

```go
// StartPacketProcessor — запуск обробника пакетів з черги
func (p *DVRIpProcessor) StartPacketProcessor(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
                
            case pkt := <-p.client.OutgoingPacketQueue:
                // Обробка відео/аудіо пакету
                if err := p.processPacket(pkt); err != nil {
                    log.Printf("process packet failed: %v", err)
                    p.metrics.ProcessErrors.Inc()
                }
                
            case signal := <-p.client.Signals:
                switch signal {
                case dvrip.SignalStreamStop:
                    log.Printf("Stream stopped, shutting down")
                    return
                case dvrip.SignalCodecUpdate:
                    // Оновлення кодека для muxer
                    if err := p.updateCodecData(p.client.CodecData); err != nil {
                        log.Printf("codec update failed: %v", err)
                    }
                }
            }
        }
    }()
}
```

---

## 🔑 4. Мережевий протокол: send/recv/Command

### Структура заголовка (Payload):

```go
type Payload struct {
    Head           byte   // завжди 0xFF
    Version        byte   // версія протоколу (0)
    Session        int32  // ID сесії
    SequenceNumber int32  // лічильник пакетів
    MsgID          int16  // тип повідомлення (requestCode)
    BodyLength     int32  // довжина тіла + 2 (для магії в кінці)
}
```

### 🔧 Метод `send()`:

```go
func (client *Client) send(msgID requestCode, data []byte) error {
    var buf bytes.Buffer
    
    // 1. Запис заголовка
    binary.Write(&buf, binary.LittleEndian, Payload{
        Head:           255,  // 0xFF
        Version:        0,
        Session:        client.session,
        SequenceNumber: client.sequenceNumber,
        MsgID:          int16(msgID),
        BodyLength:     int32(len(data)) + 2,  // +2 для магії в кінці
    })
    
    // 2. Встановлення дедлайну
    client.conn.SetDeadline(time.Now().Add(5 * time.Second))
    
    // 3. Запис тіла
    binary.Write(&buf, binary.LittleEndian, data)
    
    // 4. Магічні байти в кінці (0x0A, 0x00)
    binary.Write(&buf, binary.LittleEndian, magicEnd)  // []byte{10, 0}
    
    // 5. Відправка
    _, err := client.conn.Write(buf.Bytes())
    client.sequenceNumber++  // інкремент для наступного пакету
    return err
}
```

### 🔧 Метод `recv()`:

```go
func (client *Client) recv(text bool) (*Payload, []byte, error) {
    // 1. Читання заголовка (20 байт)
    var p Payload
    b := make([]byte, 20)
    client.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
    _, err := client.conn.Read(b)
    binary.Read(bytes.NewReader(b), binary.LittleEndian, &p)
    
    // 2. Валідація довжини тіла
    if p.BodyLength <= 0 || p.BodyLength >= 100000 {
        return nil, nil, fmt.Errorf("invalid bodylength: %v", p.BodyLength)
    }
    
    // 3. Читання тіла
    body := make([]byte, p.BodyLength)
    binary.Read(client.conn, binary.LittleEndian, &body)
    
    // 4. Видалення магії в кінці для текстових відповідей
    if text && len(body) > 2 && bytes.Compare(body[len(body)-2:], []byte{10, 0}) == 0 {
        body = body[:len(body)-2]
    }
    
    client.sequenceNumber++  // інкремент навіть при читанні
    return &p, body, nil
}
```

### 🔍 Чому `sequenceNumber++` у recv()?

```
Протокол вимагає, щоб кожен обмін (відправка+отримання) мав унікальний номер.
Це допомагає:
• Відстежувати втрачені пакети
• Уникати повторної обробки дублікатів
• Синхронізувати стан клієнта та сервера

Але: інкремент у recv() може призвести до розсинхронізації,
якщо відправка та отримання не парні. Краще інкрементити тільки при send().
```

---

## 🔑 5. CodecUpdate* — динамічне оновлення метаданих

### Сценарій: оновлення SPS/PPS

```go
func (client *Client) CodecUpdateSPS(val []byte) {
    // 1. Перевірка чи змінився SPS
    if bytes.Compare(val, client.sps) == 0 { return }
    client.sps = val
    
    // 2. Якщо є PPS — створюємо codecData
    if len(client.pps) == 0 { return }
    
    codecData, err := h264parser.NewCodecDataFromSPSAndPPS(val, client.pps)
    if err != nil { return }
    
    // 3. Оновлення або додавання у CodecData slice
    if len(client.CodecData) > 0 {
        for i, cd := range client.CodecData {
            if cd.Type().IsVideo() {
                client.CodecData[i] = codecData
                break
            }
        }
    } else {
        client.CodecData = append(client.CodecData, codecData)
    }
    
    // 4. Сигнал про оновлення
    client.Signals <- SignalCodecUpdate
}
```

### ✅ Ваш use-case: обробка SignalCodecUpdate

```go
// updateCodecData — оновлення muxer заголовка при зміні кодека
func (p *DVRIpProcessor) updateCodecData(codecData []av.CodecData) error {
    // 1. Перевірка чи змінився відео кодек
    var videoCodec av.CodecData
    for _, cd := range codecData {
        if cd.Type().IsVideo() {
            videoCodec = cd
            break
        }
    }
    if videoCodec == nil {
        return fmt.Errorf("no video codec found")
    }
    
    // 2. Оновлення muxer заголовка (якщо підтримується)
    if p.muxer != nil {
        if err := p.muxer.WriteHeader(codecData); err != nil {
            return fmt.Errorf("update header: %w", err)
        }
    }
    
    // 3. Логування для моніторингу
    if h264, ok := videoCodec.(h264parser.CodecData); ok {
        log.Printf("Codec updated: resolution=%dx%d, fps=%d",
            h264.Width(), h264.Height(), h264.FPS())
        p.metrics.Resolution.Set(float64(h264.Width() * h264.Height()))
    }
    
    return nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// dvrip_processor.go — інтеграція DVRIp клієнта у CCTV HLS Processor
type DVRIpProcessor struct {
    channelID    string
    client       *dvrip.Client
    muxer        av.MuxCloser
    packetQueue  chan *av.Packet
    metrics      *StreamMetrics
    ctx          context.Context
    cancel       context.CancelFunc
}

func NewDVRIpProcessor(channelID, rtspURL string) (*DVRIpProcessor, error) {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &DVRIpProcessor{
        channelID:   channelID,
        packetQueue: make(chan *av.Packet, 500),
        metrics:     NewStreamMetrics(channelID),
        ctx:         ctx,
        cancel:      cancel,
    }, nil
}

// Start — запуск обробки потоку
func (p *DVRIpProcessor) Start() error {
    // 1. Підключення до камери
    client, err := NewDVRIpClient(p.rtspURL, false)  // false = з аудіо
    if err != nil {
        return fmt.Errorf("connect: %w", err)
    }
    p.client = client
    
    // 2. Ініціалізація muxer (напр. для TS або HLS)
    p.muxer, err = p.initMuxer()
    if err != nil {
        p.client.Close()
        return fmt.Errorf("init muxer: %w", err)
    }
    
    // 3. Запуск обробника пакетів
    go p.StartPacketProcessor(p.ctx)
    
    // 4. Запуск моніторингу метрик
    go p.monitorMetrics(p.ctx)
    
    log.Printf("Channel %s: DVRIp processing started", p.channelID)
    return nil
}

// StartPacketProcessor — обробка пакетів з черги
func (p *DVRIpProcessor) StartPacketProcessor(ctx context.Context) {
    var lastKeyFrameTime time.Time
    var start bool
    
    for {
        select {
        case <-ctx.Done():
            return
            
        case pkt := <-p.client.OutgoingPacketQueue:
            // Детекція ключових кадрів для сегментації
            if pkt.IsKeyFrame {
                start = true
                lastKeyFrameTime = time.Now()
                p.metrics.KeyFrames.Inc()
            }
            
            if !start {
                p.metrics.PacketsSkipped.Inc()
                continue
            }
            
            // Запис у muxer
            startWrite := time.Now()
            if err := p.muxer.WritePacket(*pkt); err != nil {
                log.Printf("mux write failed: %v", err)
                p.metrics.WriteErrors.Inc()
                continue
            }
            p.metrics.WriteLatency.Observe(time.Since(startWrite).Seconds())
            p.metrics.PacketsWritten.Inc()
            
        case signal := <-p.client.Signals:
            switch signal {
            case dvrip.SignalCodecUpdate:
                if err := p.updateCodecData(p.client.CodecData); err != nil {
                    log.Printf("codec update failed: %v", err)
                }
            case dvrip.SignalStreamStop:
                log.Printf("Channel %s: stream stopped", p.channelID)
                p.cancel()
                return
            }
        }
    }
}

// Stop — зупинка обробки
func (p *DVRIpProcessor) Stop() {
    p.cancel()
    
    if p.client != nil {
        p.client.Close()
    }
    if p.muxer != nil {
        p.muxer.Close()
    }
    
    log.Printf("Channel %s: processing stopped", p.channelID)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"invalid bodylength"** | Помилка парсингу заголовка | Перевірте чи камера повертає очікуваний формат; додайте логування сирих байтів для дебагу |
| **Пакети не надходять у чергу** | `OutgoingPacketQueue` переповнена або не читається | Збільшіть розмір буферу (3000); переконайтеся, що `StartPacketProcessor` запущено |
| **Кодек не оновлюється** | `SignalCodecUpdate` не обробляється | Переконайтеся, що SPS та PPS отримані обидва; перевірте логи `CodecUpdateSPS/PPS` |
| **З'єднання обривається** | Таймаути або помилки мережі | Збільште `DialTimeout`/`ReadWriteTimeout`; реалізуйте автоматичне перепідключення |
| **Аудіо не працює** | `DisableAudio=true` або неправильний тип | Переконайтеся, що камера підтримує PCM A-law; перевірте `CodecUpdatePCMAlaw()` |

---

## ⚡ Оптимізації для real-time обробки

### 1. Неблокуюча відправка у чергу:

```go
// NonBlockingSend — відправка у чергу з таймаутом
func NonBlockingSend(queue chan *av.Packet, pkt *av.Packet, timeout time.Duration) bool {
    select {
    case queue <- pkt:
        return true
    case <-time.After(timeout):
        return false
    }
}

// Використання у Monitor():
if !NonBlockingSend(client.OutgoingPacketQueue, pkt, 10*time.Millisecond) {
    // Черга переповнена — пропускаємо пакет або логуємо
    log.Printf("queue full, dropping packet")
}
```

### 2. Кешування SPS/PPS для уникнення повторного парсингу:

```go
type CodecCache struct {
    mu    sync.RWMutex
    sps   []byte
    pps   []byte
    codec av.CodecData
}

func (c *CodecCache) UpdateSPS(val []byte) bool {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if bytes.Equal(val, c.sps) {
        return false  // без змін
    }
    c.sps = make([]byte, len(val))
    copy(c.sps, val)
    
    if c.pps != nil {
        // Оновлення codecData
        codecData, err := h264parser.NewCodecDataFromSPSAndPPS(c.sps, c.pps)
        if err == nil {
            c.codec = codecData
            return true
        }
    }
    return false
}
```

### 3. Моніторинг затримки потоку:

```go
type StreamLatencyMetrics struct {
    IngestTime    prometheus.GaugeVec
    ProcessTime   prometheus.GaugeVec
    EndToEndDelay prometheus.HistogramVec
}

func (m *StreamLatencyMetrics) RecordPacket(channelID string, ingestTime time.Time) {
    now := time.Now()
    m.EndToEndDelay.WithLabelValues(channelID).Observe(now.Sub(ingestTime).Seconds())
}

// Використання у Monitor():
frameTime := time.Unix(int64(frame.DateTime), 0)  // якщо DateTime — Unix timestamp
metrics.RecordPacket(channelID, frameTime)
```

---

## 📋 Чек-лист інтеграції dvrip

```go
// ✅ 1. Валідація вхідного URL
parsed, err := url.Parse(rawURL)
if err != nil || parsed.Host == "" {
    return fmt.Errorf("invalid URL")
}

// ✅ 2. Підключення з таймаутами
opts := dvrip.ClientOptions{
    URL: rawURL,
    DialTimeout: 5 * time.Second,
    ReadWriteTimeout: 10 * time.Second,
}
client, err := dvrip.Dial(opts)

// ✅ 3. Обробка сигналів у окремій горутині
go func() {
    for signal := range client.Signals {
        switch signal {
        case dvrip.SignalCodecUpdate:
            // оновити muxer
        case dvrip.SignalStreamStop:
            // завершити обробку
        }
    }
}()

// ✅ 4. Читання пакетів з черги з перевіркою контексту
for {
    select {
    case <-ctx.Done():
        return
    case pkt := <-client.OutgoingPacketQueue:
        // обробка пакету
    }
}

// ✅ 5. Закриття ресурсів при завершенні
defer func() {
    if client != nil { client.Close() }
    if muxer != nil { muxer.Close() }
}()

// ✅ 6. Метрики для моніторингу
metrics.PacketsReceived.Inc()
metrics.ProcessLatency.Observe(duration.Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [vdk dvrip Package](https://pkg.go.dev/github.com/deepch/vdk/format/dvrip) — GoDoc documentation (якщо доступна)
- 📄 [XMeye/DVRIP Protocol Analysis](https://github.com/bluenviron/mediamtx/issues/123) — спільнотний аналіз пропрієтарного протоколу
- 📄 [H.264 NALU Structure](https://wiki.multimedia.cx/index.php/H.264) — для розуміння обробки SPS/PPS
- 🧪 [Go net.Conn Documentation](https://pkg.go.dev/net#Conn) — робота з мережевими з'єднаннями

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **пропрієтарними DVR камерами**:
> 1. **Завжди обробляйте `SignalStreamStop`** — це єдиний спосіб дізнатися про обрив з'єднання з камери.
> 2. **Кешуйте SPS/PPS окремо** — деякі камери надсилають їх нерегулярно; без кешування відео не декодується.
> 3. **Збільшуйте буфер `OutgoingPacketQueue`** для високобітрейтних потоків — 3000 пакетів можуть заповнитися за секунди.
> 4. **Моніторьте `aliveInterval`** — якщо камера не відповідає на KeepAlive, ініціюйте перепідключення.
> 5. **Тестуйте з різними моделями камер** — реалізація DVRIp може відрізнятися між виробниками (XMeye, VStarcam, тощо).

Потрібен приклад реалізації автоматичного перепідключення при обриві з'єднання з DVR камерою? Готовий допомогти! 🚀