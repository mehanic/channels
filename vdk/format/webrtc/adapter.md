# 📦 Глибокий розбір: `webrtc.Muxer` — WebRTC стрімінг для vdk

Цей файл — **реалізація WebRTC муксера** для бібліотеки `vdk`, що дозволяє транслювати медіа-потоки (H.264 відео) через WebRTC за допомогою бібліотеки `pion/webrtc`. Він надає механізми для встановлення WebRTC з'єднання, генерації SDP, та відправки відео-пакетів у реальному часі.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема webrtc.Muxer

```
┌────────────────────────────────────────┐
│ 📦 webrtc.Muxer — WebRTC Stream Sender│
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Muxer — основний контролер          │
│  • Stream — окремий медіа-потік        │
│  • WriteHeader() — SDP negotiation     │
│  • WritePacket() — відправка відео     │
│  • WaitCloser() — авто-закриття        │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet → H.264 Annex B → RTP → WebRTC│
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (тільки)               │
│  • Аудіо: заплановано, не реалізовано  │
│  • STUN: stun.l.google.com:19302       │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Muxer — основна структура

### Поля та їх призначення:

```go
type Muxer struct {
    streams map[int8]*Stream  // потоки за індексом (відео=0, аудіо=1...)
    status  webrtc.ICEConnectionState  // стан WebRTC з'єднання
    stop    bool              // прапорець завершення
    pc      *webrtc.PeerConnection  // основне WebRTC з'єднання
    pt      *time.Timer       // таймер бездіяльності (20с)
    ps      chan bool         // канал сигналу закриття
}
```

### 🔧 Метод `NewMuxer()` — ініціалізація:

```go
func NewMuxer() *Muxer {
    tmp := Muxer{
        ps:      make(chan bool, 100),           // буферизований канал
        pt:      time.NewTimer(time.Second * 20), // 20с таймаут бездіяльності
        streams: make(map[int8]*Stream),          // порожня мапа потоків
    }
    go tmp.WaitCloser()  // фоновий монітор закриття
    return &tmp
}
```

### 🔍 Чому `WaitCloser()` у горутинах?

```
WebRTC з'єднання потребує асинхронного моніторингу:
• ICE connection state changes (connected/disconnected)
• Data channel messages (heartbeat)
• Таймаут бездіяльності (20с)

WaitCloser() слухає два джерела:
1. element.ps <- true  // сигнал від OnICEConnectionStateChange
2. element.pt.C        // таймаут 20с без повідомлень

Якщо будь-яка подія стається → stop=true → Close()
```

### ✅ Ваш use-case: створення муксера з кастомним таймаутом

```go
// NewMuxerWithTimeout — муксер з налаштовуваним таймаутом
func NewMuxerWithTimeout(timeout time.Duration) *webrtc.Muxer {
    m := webrtc.NewMuxer()
    // Перезапуск таймера з новим значенням
    m.PT().Reset(timeout)  // припускаємо, що є метод для доступу до pt
    return m
}

// Альтернатива: розширення структури
type ExtendedMuxer struct {
    *webrtc.Muxer
    customTimeout time.Duration
}

func NewExtendedMuxer(timeout time.Duration) *ExtendedMuxer {
    return &ExtendedMuxer{
        Muxer:         webrtc.NewMuxer(),
        customTimeout: timeout,
    }
}
```

---

## 🔑 2. WriteHeader() — WebRTC SDP negotiation

### 🔧 Основна логіка:

```go
func (element *Muxer) WriteHeader(streams []av.CodecData, sdp64 string) (string, error) {
    // 1. Валідація вхідних даних
    if len(streams) == 0 {
        return "", ErrorNotFound
    }
    
    // 2. Декодування base64 SDP offer від клієнта
    sdpB, err := base64.StdEncoding.DecodeString(sdp64)
    if err != nil { return "", err }
    offer := webrtc.SessionDescription{
        Type: webrtc.SDPTypeOffer,
        SDP:  string(sdpB),
    }
    
    // 3. Ініціалізація MediaEngine з SDP
    mediaEngine := webrtc.MediaEngine{}
    if err = mediaEngine.PopulateFromSDP(offer); err != nil { return "", err }
    
    // 4. Створення API з кастомним MediaEngine
    api := webrtc.NewAPI(webrtc.WithMediaEngine(mediaEngine))
    
    // 5. Створення PeerConnection з STUN сервером
    peerConnection, err := api.NewPeerConnection(webrtc.Configuration{
        ICEServers: []webrtc.ICEServer{{
            URLs: []string{"stun:stun.l.google.com:19302"},
        }},
    })
    
    // 6. Створення WebRTC track для кожного потоку
    for i, codec := range streams {
        var track *webrtc.Track
        if codec.Type().IsVideo() {
            track, err = peerConnection.NewTrack(
                getPayloadType(mediaEngine, webrtc.RTPCodecTypeVideo, codec.Type().String()),
                rand.Uint32(),  // SSRC
                "video",        // ID
                Label,          // Label = "track_"
            )
        } else if codec.Type().IsAudio() {
            // Аудіо заплановано, але не реалізовано у WritePacket()
            track, err = peerConnection.NewTrack(...)
        }
        
        // Додавання track у PeerConnection
        peerConnection.AddTransceiverFromTrack(track, webrtc.RtpTransceiverInit{
            Direction: webrtc.RTPTransceiverDirectionSendonly,
        })
        peerConnection.AddTrack(track)
        
        // Збереження у мапу потоків
        element.streams[int8(i)] = &Stream{track: track, codec: codec}
    }
    
    // 7. Обробка подій з'єднання
    peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
        element.status = state
        if state == webrtc.ICEConnectionStateDisconnected {
            element.ps <- true  // сигнал закриття
        }
    })
    
    // 8. Heartbeat через DataChannel
    peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
        d.OnMessage(func(msg webrtc.DataChannelMessage) {
            element.pt.Reset(5 * time.Second)  // скидання таймера
        })
    })
    
    // 9. Встановлення віддаленого опису (offer)
    if err = peerConnection.SetRemoteDescription(offer); err != nil { return "", err }
    
    // 10. Генерація та встановлення локального опису (answer)
    answer, err := peerConnection.CreateAnswer(nil)
    if err != nil { return "", err }
    if err = peerConnection.SetLocalDescription(answer); err != nil { return "", err }
    
    // 11. Збереження та повернення SDP answer у base64
    element.pc = peerConnection
    return base64.StdEncoding.EncodeToString([]byte(answer.SDP)), nil
}
```

### 🔍 Функція `getPayloadType()`:

```go
func getPayloadType(m webrtc.MediaEngine, codecType webrtc.RTPCodecType, codecName string) uint8 {
    for _, codec := range m.GetCodecsByKind(codecType) {
        if codec.Name == codecName {
            return codec.PayloadType
        }
    }
    panic(fmt.Sprintf("Remote peer does not support %s", codecName))
}
```

### ⚠️ Критичні моменти:

```
❌ panic у getPayloadType() — небезпечно для production
✅ Рішення: повертати помилку замість panic

❌ Тільки H.264 підтримується у WritePacket()
✅ Рішення: додати обробку для AAC/Opus або валідувати на вході

❌ Жорстко закодований STUN сервер
✅ Рішення: винести у конфігурацію або підтримувати TURN

❌ Неточна обробка аудіо потоків (створюються tracks, але не відправляються)
✅ Рішення: або видалити створення аудіо tracks, або реалізувати WritePacket для аудіо
```

### ✅ Ваш use-case: безпечна генерація SDP

```go
// SafeWriteHeader — версія з обробкою помилок замість panic
func (m *Muxer) SafeWriteHeader(streams []av.CodecData, sdp64 string) (string, error) {
    // Валідація підтримуваних кодеків
    for _, s := range streams {
        if s.Type() != av.H264 {
            return "", fmt.Errorf("unsupported codec for WebRTC: %v", s.Type())
        }
    }
    
    // Виклик оригінального методу з recover
    defer func() {
        if r := recover(); r != nil {
            log.Printf("WebRTC SDP generation panic recovered: %v", r)
        }
    }()
    
    return m.WriteHeader(streams, sdp64)
}

// GetSupportedCodecs — перевірка сумісності перед викликом
func GetSupportedCodecs(sdp64 string) ([]av.CodecType, error) {
    sdpB, err := base64.StdEncoding.DecodeString(sdp64)
    if err != nil { return nil, err }
    
    offer := webrtc.SessionDescription{
        Type: webrtc.SDPTypeOffer,
        SDP:  string(sdpB),
    }
    
    var mediaEngine webrtc.MediaEngine
    if err := mediaEngine.PopulateFromSDP(offer); err != nil {
        return nil, err
    }
    
    var supported []av.CodecType
    for _, codec := range mediaEngine.GetCodecsByKind(webrtc.RTPCodecTypeVideo) {
        if codec.Name == "H264" {
            supported = append(supported, av.H264)
        }
    }
    // Додати аудіо якщо потрібно...
    
    return supported, nil
}
```

---

## 🔑 3. WritePacket() — відправка відео-пакетів

### 🔧 Основна логіка для H.264:

```go
func (element *Muxer) WritePacket(pkt av.Packet) (err error) {
    // 1. Перевірка стану
    if element.stop {
        return ErrorClientOffline
    }
    if element.status != webrtc.ICEConnectionStateConnected {
        return nil  // ігнорування якщо не підключено
    }
    
    // 2. Пошук потоку за індексом
    if tmp, ok := element.streams[pkt.Idx]; ok {
        switch tmp.codec.Type() {
        case av.H264:
            codec := tmp.codec.(h264parser.CodecData)
            
            // 3. Конвертація формату: AVCC → Annex B
            if pkt.IsKeyFrame {
                // Для ключових кадрів: додаємо SPS+PPS перед даними
                // pkt.Data у форматі AVCC: [4-byte length][NALU]...
                // Конвертація у Annex B: [0x00000001][NALU]...
                pkt.Data = append([]byte{0, 0, 0, 1}, 
                    bytes.Join([][]byte{
                        codec.SPS(),      // SPS NALU
                        codec.PPS(),      // PPS NALU
                        pkt.Data[4:],     // відео дані без 4-байтового префіксу
                    }, []byte{0, 0, 0, 1})...)  // start code separator
            } else {
                // Для не-ключових: просто видаляємо 4-байтовий префікс
                pkt.Data = pkt.Data[4:]
            }
            
            // 4. Відправка у WebRTC track
            // Samples: 90000 = 1 секунда @ 90kHz (стандарт для H.264 RTP)
            return tmp.track.WriteSample(media.Sample{
                Data:    pkt.Data,
                Samples: 90000,  // ⚠️ ФІКСОВАНЕ ЗНАЧЕННЯ — може бути неправильним!
            })
        default:
            return ErrorCodecNotSupported
        }
    }
    return ErrorNotFound
}
```

### 🔍 Конвертація AVCC → Annex B:

```
Вхід (av.Packet з FLV/TS демуксера):
  • Формат: AVCC (length-prefixed)
  • Структура: [4-byte big-endian length][NALU data]...
  • Приклад: [00 00 00 15][67 42 C0 1E...] (SPS NALU)

Вихід (WebRTC RTP):
  • Формат: Annex B (start code prefixed)
  • Структура: [0x00000001][NALU data][0x00000001][NALU data]...
  • Приклад: [00 00 00 01][67 42 C0 1E...][00 00 00 01][68 CE 38 80...]

Чому це потрібно:
• WebRTC/RTP очікує Annex B формат для H.264
• FLV/TS демуксери часто надають AVCC формат
• Конвертація забезпечує сумісність
```

### ⚠️ Критична проблема: фіксоване `Samples: 90000`

```go
// У коді:
return tmp.track.WriteSample(media.Sample{
    Data:    pkt.Data,
    Samples: 90000,  // ← ЗАВЖДИ 1 секунда, незалежно від реального duration!
})
```

**Наслідки**:
- Якщо реальний пакет триває 33ms (30fps), а ви вказуєте 1000ms → плеєр буде "зависати"
- Аудіо/відео розсинхронізація
- Неправильне відтворення швидкості

**✅ Правильний розрахунок**:

```go
// CalculateSamples — конвертація duration → samples @ 90kHz
func CalculateSamples(duration time.Duration) uint32 {
    // 90kHz = 90000 samples per second
    return uint32(duration * 90000 / time.Second)
}

// У WritePacket():
samples := CalculateSamples(pkt.Duration)
if samples == 0 {
    // Fallback: розрахунок з FPS якщо duration не вказано
    if cd, ok := tmp.codec.(h264parser.CodecData); ok && cd.FPS() > 0 {
        samples = 90000 / uint32(cd.FPS())
    } else {
        samples = 3000  // дефолт: 30fps → 3000 samples
    }
}
return tmp.track.WriteSample(media.Sample{
    Data:    pkt.Data,
    Samples: samples,
})
```

### ✅ Ваш use-case: відправка відео з коректними таймінгами

```go
// WritePacketWithTiming — виправлена версія з розрахунком samples
func (m *Muxer) WritePacketWithTiming(pkt av.Packet) error {
    if m.stop || m.status != webrtc.ICEConnectionStateConnected {
        return nil
    }
    
    stream, ok := m.streams[pkt.Idx]
    if !ok || stream.codec.Type() != av.H264 {
        return ErrorNotFound  // або ErrorCodecNotSupported
    }
    
    codec := stream.codec.(h264parser.CodecData)
    
    // Конвертація AVCC → Annex B
    var data []byte
    if pkt.IsKeyFrame {
        data = append([]byte{0, 0, 0, 1}, 
            bytes.Join([][]byte{codec.SPS(), codec.PPS(), pkt.Data[4:]}, 
            []byte{0, 0, 0, 1})...)
    } else {
        data = pkt.Data[4:]
    }
    
    // Розрахунок samples
    samples := uint32(3000)  // дефолт 30fps
    if pkt.Duration > 0 {
        samples = uint32(pkt.Duration * 90000 / time.Second)
    } else if codec.FPS() > 0 {
        samples = 90000 / uint32(codec.FPS())
    }
    
    return stream.track.WriteSample(media.Sample{
        Data:    data,
        Samples: samples,
    })
}
```

---

## 🔑 4. WaitCloser() — авто-закриття з'єднання

### 🔧 Логіка моніторингу:

```go
func (element *Muxer) WaitCloser() {
    select {
    case <-element.ps:
        // Сигнал від OnICEConnectionStateChange (disconnected)
        element.stop = true
        element.Close()
        
    case <-element.pt.C:
        // Таймаут 20с без активності
        element.stop = true
        element.Close()
    }
}
```

### 🔍 Чому два джерела сигналу?

```
1. element.ps (канал):
   • Встановлюється у OnICEConnectionStateChange при disconnected
   • Миттєва реакція на розрив з'єднання

2. element.pt.C (таймер):
   • Скидається при кожному повідомленні у DataChannel
   • Захист від "завислих" з'єднань без явного disconnect
   • 20с дефолт, 5с при отриманні heartbeat

Це забезпечує:
• Швидке закриття при реальних помилках мережі
• Захист від витоків ресурсів при "тихому" відключенні клієнта
```

### ✅ Ваш use-case: кастомна логіка закриття

```go
// ExtendedWaitCloser — розширена версія з логуванням та метриками
func (m *Muxer) ExtendedWaitCloser(metrics *WebRTCMetrics, channelID string) {
    select {
    case reason := <-m.ps:
        log.Printf("Channel %s: WebRTC disconnected (signal: %v)", channelID, reason)
        metrics.Disconnects.WithLabelValues("ice_failure", channelID).Inc()
        m.Close()
        
    case <-m.pt.C:
        log.Printf("Channel %s: WebRTC timeout (no heartbeat for 20s)", channelID)
        metrics.Disconnects.WithLabelValues("timeout", channelID).Inc()
        m.Close()
        
    case <-m.customStopChan:  // додатковий канал для зовнішнього закриття
        log.Printf("Channel %s: WebRTC stopped externally", channelID)
        metrics.Disconnects.WithLabelValues("external", channelID).Inc()
        m.Close()
    }
}

// Використання:
muxer := webrtc.NewMuxer()
go muxer.ExtendedWaitCloser(metrics, channelID)
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// webrtc_stream_handler.go — обробка WebRTC стрімінгу для CCTV
type WebRTCStreamHandler struct {
    channelID    string
    muxer        *webrtc.Muxer
    packetQueue  chan av.Packet
    metrics      *WebRTCMetrics
    ctx          context.Context
    cancel       context.CancelFunc
}

func NewWebRTCStreamHandler(channelID string) (*WebRTCStreamHandler, error) {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &WebRTCStreamHandler{
        channelID:   channelID,
        muxer:       webrtc.NewMuxer(),
        packetQueue: make(chan av.Packet, 500),  // буфер на 500 пакетів
        metrics:     NewWebRTCMetrics(channelID),
        ctx:         ctx,
        cancel:      cancel,
    }, nil
}

// StartHandshake — початок WebRTC handshake з клієнтом
func (h *WebRTCStreamHandler) StartHandshake(offerSDP64 string, streams []av.CodecData) (string, error) {
    // Валідація кодеків
    for _, s := range streams {
        if s.Type() != av.H264 {
            return "", fmt.Errorf("unsupported codec for WebRTC: %v", s.Type())
        }
    }
    
    // Генерація answer SDP
    answerSDP64, err := h.muxer.WriteHeader(streams, offerSDP64)
    if err != nil {
        return "", fmt.Errorf("generate SDP answer: %w", err)
    }
    
    h.metrics.HandshakesCompleted.Inc()
    log.Printf("Channel %s: WebRTC handshake completed", h.channelID)
    return answerSDP64, nil
}

// SendPacket — відправка відео-пакету у WebRTC потік
func (h *WebRTCStreamHandler) SendPacket(pkt av.Packet) error {
    select {
    case h.packetQueue <- pkt:
        return nil
    default:
        // Черга переповнена — пропускаємо пакет
        h.metrics.DroppedPackets.Inc()
        return nil
    }
}

// StartPacketSender — фоновий відправник пакетів
func (h *WebRTCStreamHandler) StartPacketSender() {
    go func() {
        for {
            select {
            case <-h.ctx.Done():
                return
                
            case pkt := <-h.packetQueue:
                start := time.Now()
                
                // Використання виправленої версії з коректними таймінгами
                if err := h.muxer.WritePacketWithTiming(pkt); err != nil {
                    if err == webrtc.ErrorClientOffline {
                        log.Printf("Channel %s: client offline, stopping", h.channelID)
                        h.cancel()
                        return
                    }
                    h.metrics.SendErrors.Inc()
                    log.Printf("Channel %s: send error: %v", h.channelID, err)
                    continue
                }
                
                h.metrics.SendLatency.Observe(time.Since(start).Seconds())
                h.metrics.PacketsSent.Inc()
            }
        }
    }()
}

// Close — зупинка стрімінгу
func (h *WebRTCStreamHandler) Close() {
    h.cancel()
    if h.muxer != nil {
        h.muxer.Close()
    }
    log.Printf("Channel %s: WebRTC handler closed", h.channelID)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"Remote peer does not support H264" panic** | Клієнт не підтримує H.264 у своєму SDP | Валідуйте SDP перед викликом `WriteHeader()`; повертайте помилку замість panic |
| **Відео "зависає" або прискорюється** | Фіксоване `Samples: 90000` не відповідає реальному duration | Використовуйте `CalculateSamples(pkt.Duration)` замість жорсткого значення |
| **З'єднання не закривається** | `WaitCloser()` не отримує сигнали | Переконайтеся, що `OnICEConnectionStateChange` та `OnDataChannel` зареєстровані коректно |
| **Аудіо не працює** | Аудіо tracks створюються, але `WritePacket()` не обробляє аудіо | Або видаліть створення аудіо tracks, або реалізуйте обробку для AAC/Opus |
| **STUN не працює за фаєрволом** | З'єднання не встановлюється | Додайте підтримку TURN серверів у конфігурацію `ICEServers` |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування конвертації AVCC → Annex B:

```go
// AnnexBConverter — кешування SPS/PPS для уникнення повторного append
type AnnexBConverter struct {
    mu       sync.RWMutex
    sps, pps []byte
    prefix   []byte  // префікс [0,0,0,1] + SPS + [0,0,0,1] + PPS + [0,0,0,1]
}

func (c *AnnexBConverter) Update(sps, pps []byte) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if bytes.Equal(c.sps, sps) && bytes.Equal(c.pps, pps) {
        return  // без змін
    }
    
    c.sps = append([]byte(nil), sps...)
    c.pps = append([]byte(nil), pps...)
    c.prefix = append([]byte{0,0,0,1}, 
        bytes.Join([][]byte{c.sps, c.pps}, []byte{0,0,0,1})...)
    c.prefix = append(c.prefix, []byte{0,0,0,1}...)
}

func (c *AnnexBConverter) Convert(isKeyFrame bool, avccData []byte) []byte {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    if isKeyFrame {
        // Використання кешованого префіксу
        result := make([]byte, len(c.prefix)+len(avccData)-4)
        copy(result, c.prefix)
        copy(result[len(c.prefix):], avccData[4:])
        return result
    }
    // Не-ключовий: просто видалити 4-байтовий префікс
    return avccData[4:]
}
```

### 2. Пакетна відправка для зменшення накладних витрат:

```go
// BatchWriteSamples — відправка кількох пакетів за один виклик
func (s *Stream) BatchWriteSamples(packets []av.Packet) error {
    for _, pkt := range packets {
        // Конвертація та розрахунок samples...
        samples := CalculateSamples(pkt.Duration)
        if err := s.track.WriteSample(media.Sample{
            Data:    convertAVCCtoAnnexB(pkt),
            Samples: samples,
        }); err != nil {
            return err
        }
    }
    return nil
}
```

### 3. Моніторинг продуктивності WebRTC:

```go
type WebRTCMetrics struct {
    HandshakesCompleted prometheus.CounterVec
    PacketsSent         prometheus.CounterVec
    SendLatency         prometheus.HistogramVec
    Disconnects         prometheus.CounterVec  // by reason: ice_failure, timeout, external
    DroppedPackets      prometheus.CounterVec
    SendErrors          prometheus.CounterVec
}

func (m *WebRTCMetrics) RecordSend(duration time.Duration, channelID string) {
    m.SendLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    m.PacketsSent.WithLabelValues(channelID).Inc()
}
```

---

## 📋 Чек-лист інтеграції webrtc.Muxer

```go
// ✅ 1. Валідація кодеків перед handshake
for _, s := range streams {
    if s.Type() != av.H264 {
        return fmt.Errorf("WebRTC requires H.264, got %v", s.Type())
    }
}

// ✅ 2. Безпечна генерація SDP (без panic)
answer, err := muxer.SafeWriteHeader(streams, offerSDP64)
if err != nil { /* handle error */ }

// ✅ 3. Відправка пакетів з коректними таймінгами
samples := CalculateSamples(pkt.Duration)  // не фіксоване 90000!
stream.track.WriteSample(media.Sample{
    Data:    convertAVCCtoAnnexB(pkt),
    Samples: samples,
})

// ✅ 4. Моніторинг стану з'єднання
if muxer.Status() != webrtc.ICEConnectionStateConnected {
    // Не відправляти пакети, або буферизувати
    continue
}

// ✅ 5. Обробка закриття
defer muxer.Close()  // гарантоване звільнення ресурсів

// ✅ 6. Метрики для моніторингу
metrics.RecordSend(time.Since(start), channelID)
```

---

## 🔗 Корисні посилання

- 💻 [pion/webrtc Documentation](https://pkg.go.dev/github.com/pion/webrtc/v2) — офіційна довідка
- 💻 [vdk webrtc Package](https://pkg.go.dev/github.com/deepch/vdk/format/webrtc) — GoDoc (якщо доступно)
- 📄 [WebRTC H.264 RTP Payload Format](https://datatracker.ietf.org/doc/html/rfc6184) — специфікація кодування
- 📄 [Annex B vs AVCC](https://wiki.multimedia.cx/index.php/H.264#Annex_B) — порівняння форматів
- 🧪 [STUN/TURN Setup Guide](https://github.com/pion/webrtc/wiki/Getting-Started#turn-server) — налаштування серверів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **відео в реальному часі**:
> 1. **Замініть `panic` на помилку у `getPayloadType()`** — паніка у production може "вбити" весь процес.
> 2. **Розраховуйте `Samples` динамічно** — фіксоване значення 90000 зламає таймінги для будь-якого іншого FPS.
> 3. **Валідуйте SDP перед handshake** — перевірка підтримки H.264 на стороні клієнта уникне помилок.
> 4. **Додайте підтримку TURN** — STUN недостатній для клієнтів за симетричним NAT/фаєрволом.
> 5. **Моніторьте `SendLatency`** — різке зростання може вказувати на перевантаження мережі або процесора.

Потрібен приклад реалізації `convertAVCCtoAnnexB()` з оптимізацією для великих потоків? Готовий допомогти! 🚀