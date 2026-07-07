# 📦 Глибокий розбір: `webrtc.Muxer` (v3) — Production-Ready WebRTC для vdk

Цей файл — **оновлена реалізація WebRTC муксера** з використанням `pion/webrtc/v3`, що включає підтримку аудіо (G.711, Opus), конфігурацію ICE/STUN/TURN, та покращену обробку помилок. Він значно стабільніший за попередню версію та готовий для використання у production.

Розберемо архітектуру, ключові покращення та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема webrtc.Muxer v3

```
┌────────────────────────────────────────┐
│ 📦 webrtc.Muxer (v3) — Production Ready│
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові покращення:                 │
│  • pion/webrtc/v3 замість v2           │
│  • Підтримка аудіо: PCMA/PCMU/Opus     │
│  • Конфігурація ICE: STUN/TURN, порти │
│  • RTCP handling для контролю якості   │
│  • GatheringCompletePromise для надійності│
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet → SplitNALUs → Annex B → RTP│
│                                         │
│  📡 Підтримка кодеків:                  │
│  • Відео: H.264 (обов'язково)          │
│  • Аудіо: PCMA (G.711 A-law), PCMU (μ-law), Opus│
│  • ❌ AAC не підтримується (TODO)       │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Options — гнучка конфігурація ICE

### Структура конфігурації:

```go
type Options struct {
    // STUN/TURN сервери
    ICEServers []string        // напр. ["stun:stun.l.google.com:19302", "turn:turn.example.com:3478"]
    ICEUsername string         // для аутентифікації TURN
    ICECredential string       // пароль для TURN
    
    // Мережеві налаштування
    ICECandidates []string     // зовнішні IP для 1:1 NAT (напр. ["203.0.113.1"])
    PortMin uint16            // мінімальний порт для UDP (напр. 10000)
    PortMax uint16            // максимальний порт для UDP (напр. 20000)
}
```

### 🔧 Метод `NewPeerConnection()` — створення з кастомними налаштуваннями:

```go
func (element *Muxer) NewPeerConnection(configuration webrtc.Configuration) (*webrtc.PeerConnection, error) {
    // 1. Налаштування ICE серверів
    if len(element.Options.ICEServers) > 0 {
        configuration.ICEServers = append(configuration.ICEServers, webrtc.ICEServer{
            URLs:           element.Options.ICEServers,
            Username:       element.Options.ICEUsername,
            Credential:     element.Options.ICECredential,
            CredentialType: webrtc.ICECredentialTypePassword,
        })
    } else {
        // Fallback на публічний STUN
        configuration.ICEServers = append(configuration.ICEServers, webrtc.ICEServer{
            URLs: []string{"stun:stun.l.google.com:19302"},
        })
    }
    
    // 2. Ініціалізація MediaEngine з дефолтними кодеками
    m := &webrtc.MediaEngine{}
    if err := m.RegisterDefaultCodecs(); err != nil {
        return nil, err
    }
    
    // 3. Реєстрація дефолтних інтерцепторів (RTCP, NACK, тощо)
    i := &interceptor.Registry{}
    if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
        return nil, err
    }
    
    // 4. Налаштування SettingEngine
    s := webrtc.SettingEngine{}
    
    // Обмеження діапазону портів для фаєрволів
    if element.Options.PortMin > 0 && element.Options.PortMax > 0 {
        s.SetEphemeralUDPPortRange(element.Options.PortMin, element.Options.PortMax)
    }
    
    // Налаштування зовнішніх IP для NAT traversal
    if len(element.Options.ICECandidates) > 0 {
        s.SetNAT1To1IPs(element.Options.ICECandidates, webrtc.ICECandidateTypeHost)
    }
    
    // 5. Створення API з усіма налаштуваннями
    api := webrtc.NewAPI(
        webrtc.WithMediaEngine(m),
        webrtc.WithInterceptorRegistry(i),
        webrtc.WithSettingEngine(s),
    )
    
    return api.NewPeerConnection(configuration)
}
```

### ✅ Ваш use-case: конфігурація для production

```go
// CreateProductionOptions — налаштування для реального розгортання
func CreateProductionOptions(externalIP string, turnURL, turnUser, turnPass string) webrtc.Options {
    return webrtc.Options{
        // TURN для надійного проходження через фаєрволи
        ICEServers:    []string{turnURL},
        ICEUsername:   turnUser,
        ICECredential: turnPass,
        
        // Зовнішній IP для коректного NAT traversal
        ICECandidates: []string{externalIP},
        
        // Обмеження портів для фаєрволу
        PortMin: 10000,
        PortMax: 20000,
    }
}

// Використання:
options := CreateProductionOptions("203.0.113.1", "turn:turn.example.com:3478", "user", "pass")
muxer := webrtc.NewMuxer(options)
```

---

## 🔑 2. WriteHeader() — покращена SDP negotiation

### 🔧 Ключові покращення:

```go
func (element *Muxer) WriteHeader(streams []av.CodecData, sdp64 string) (string, error) {
    var WriteHeaderSuccess bool
    defer func() {
        // Автоматичне закриття при помилці
        if !WriteHeaderSuccess {
            element.Close()
        }
    }()
    
    // 1. Декодування SDP offer
    sdpB, err := base64.StdEncoding.DecodeString(sdp64)
    // ...
    
    // 2. Створення PeerConnection з налаштуваннями
    peerConnection, err := element.NewPeerConnection(webrtc.Configuration{
        SDPSemantics: webrtc.SDPSemanticsUnifiedPlanWithFallback,  // краща сумісність
    })
    
    // 3. Створення tracks для кожного потоку
    for i, codec := range streams {
        if codec.Type().IsVideo() {
            // Тільки H.264 підтримується для відео
            if codec.Type() == av.H264 {
                track, err = webrtc.NewTrackLocalStaticSample(
                    webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
                    "pion-rtsp-video", "pion-video",
                )
                // Додавання RTCP reader для контролю якості
                if rtpSender, err := peerConnection.AddTrack(track); err == nil {
                    go func() {
                        rtcpBuf := make([]byte, 1500)
                        for {
                            if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
                                return  // з'єднання закрите
                            }
                            // Тут можна обробляти RTCP пакети (NACK, PLI, тощо)
                        }
                    }()
                }
            }
            
        } else if codec.Type().IsAudio() {
            // Підтримка аудіо кодеків
            var audioMimeType string
            switch codec.Type() {
            case av.PCM_ALAW:  // G.711 A-law
                audioMimeType = webrtc.MimeTypePCMA
            case av.PCM_MULAW:  // G.711 μ-law
                audioMimeType = webrtc.MimeTypePCMU
            case av.OPUS:
                audioMimeType = webrtc.MimeTypeOpus
            default:
                log.Println(ErrorIgnoreAudioTrack)
                continue  // пропуск непідтримуваних аудіо кодеків
            }
            
            audioCodec := codec.(av.AudioCodecData)
            track, err = webrtc.NewTrackLocalStaticSample(
                webrtc.RTPCodecCapability{
                    MimeType:  audioMimeType,
                    Channels:  uint16(audioCodec.ChannelLayout().Count()),
                    ClockRate: uint32(audioCodec.SampleRate()),
                },
                "pion-rtsp-audio", "pion-rtsp-audio",
            )
            // Аналогічно додавання RTCP reader...
        }
        element.streams[int8(i)] = &Stream{track: track, codec: codec}
    }
    
    // 4. Обробка подій з'єднання
    peerConnection.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
        element.status = state
        if state == webrtc.ICEConnectionStateDisconnected {
            element.Close()  // автоматичне закриття
        }
    })
    
    // 5. Heartbeat через DataChannel
    peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
        d.OnMessage(func(msg webrtc.DataChannelMessage) {
            element.ClientACK.Reset(5 * time.Second)  // скидання таймера
        })
    })
    
    // 6. Встановлення описів та очікування завершення збору кандидатів
    if err = peerConnection.SetRemoteDescription(offer); err != nil { return "", err }
    
    // ✅ Ключове покращення: Waiting for ICE gathering to complete
    gatherCompletePromise := webrtc.GatheringCompletePromise(peerConnection)
    answer, err := peerConnection.CreateAnswer(nil)
    if err != nil { return "", err }
    if err = peerConnection.SetLocalDescription(answer); err != nil { return "", err }
    
    element.pc = peerConnection
    
    // Очікування завершення збору кандидатів (до 10с)
    waitT := time.NewTimer(time.Second * 10)
    select {
    case <-waitT.C:
        return "", errors.New("gatherCompletePromise wait timeout")
    case <-gatherCompletePromise:
        // Успішно зібрано всі кандидати
    }
    
    WriteHeaderSuccess = true
    return base64.StdEncoding.EncodeToString([]byte(peerConnection.LocalDescription().SDP)), nil
}
```

### 🔍 Чому `GatheringCompletePromise` критично?

```
Без очікування завершення збору кандидатів:
• SDP answer може бути надісланий до того, як всі кандидати зібрані
• Клієнт може не отримати всі можливі шляхи для з'єднання
• З'єднання може не встановитися або бути нестабільним

З `GatheringCompletePromise`:
• Гарантується, що всі кандидати (host, srflx, relay) зібрані
• SDP answer містить повний список можливих шляхів
• З'єднання встановлюється надійніше, особливо за фаєрволом
```

### ✅ Ваш use-case: обробка аудіо потоків

```go
// ValidateAudioCodec — перевірка підтримки аудіо кодека
func ValidateAudioCodec(codec av.CodecData) error {
    switch codec.Type() {
    case av.PCM_ALAW, av.PCM_MULAW, av.OPUS:
        return nil  // підтримується
    case av.AAC:
        return fmt.Errorf("AAC not supported for WebRTC; convert to Opus or G.711 first")
    default:
        return fmt.Errorf("unsupported audio codec: %v", codec.Type())
    }
}

// ConvertAACtoOpus — приклад конвертації (псевдокод)
func ConvertAACtoOpus(aacData []byte, codec aacparser.CodecData) ([]byte, error) {
    // 1. Декодування AAC → PCM
    pcm, err := aacDecoder.Decode(aacData)
    if err != nil { return nil, err }
    
    // 2. Ресемплінг до 48kHz якщо потрібно
    if codec.SampleRate() != 48000 {
        pcm = resample(pcm, codec.SampleRate(), 48000)
    }
    
    // 3. Кодування у Opus
    opusData, err := opusEncoder.Encode(pcm)
    if err != nil { return nil, err }
    
    return opusData, nil
}
```

---

## 🔑 3. WritePacket() — покращена обробка відео та аудіо

### 🔧 Обробка H.264 з розбиттям на NALU:

```go
case av.H264:
    // 1. Розбиття на окремі NALU
    nalus, _ := h264parser.SplitNALUs(pkt.Data)
    
    for _, nalu := range nalus {
        naltype := nalu[0] & 0x1f
        
        if naltype == 5 {  // IDR frame (ключовий кадр)
            // Для ключових кадрів: додаємо SPS+PPS перед NALU
            codec := tmp.codec.(h264parser.CodecData)
            err = tmp.track.WriteSample(media.Sample{
                Data: append([]byte{0, 0, 0, 1}, 
                    bytes.Join([][]byte{
                        codec.SPS(), 
                        codec.PPS(), 
                        nalu,  // сам IDR NALU
                    }, []byte{0, 0, 0, 1})...),
                Duration: pkt.Duration,  // ✅ Використання реального duration!
            })
        } else {
            // Для звичайних NALU: просто додаємо start code
            err = tmp.track.WriteSample(media.Sample{
                Data:     append([]byte{0, 0, 0, 1}, nalu...),
                Duration: pkt.Duration,  // ✅ Реальний duration!
            })
        }
        if err != nil {
            return err
        }
    }
    WritePacketSuccess = true
    return
```

### 🔍 Чому розбиття на NALU краще?

```
Оригінальна версія (v2):
• Відправляла весь пакет як один Sample
• Якщо пакет містив кілька NALU — вони об'єднувались
• Плеєр міг некоректно обробляти такі "злиті" фрейми

Покращена версія (v3):
• Кожен NALU відправляється окремим Sample
• Ключові кадри (IDR) отримують SPS+PPS автоматично
• Краща сумісність з різними плеєрами
• Можливість обробки NALU індивідуально (напр. фільтрація)
```

### 🔧 Обробка аудіо (заглушки для майбутньої реалізації):

```go
case av.PCM_ALAW, av.PCM_MULAW, av.OPUS:
    // Ці кодеки підтримуються напряму
    // Потрібно лише переконатися, що дані у правильному форматі
    err = tmp.track.WriteSample(media.Sample{
        Data:     pkt.Data,
        Duration: pkt.Duration,
    })
    
case av.AAC:
    // TODO: Потрібен декодер AAC → PCM + енкодер PCM → Opus
    return ErrorCodecNotSupported
    
case av.PCM:
    // TODO: Потрібен енкодер PCM → Opus
    return ErrorCodecNotSupported
```

### ✅ Ваш use-case: відправка аудіо з коректними параметрами

```go
// WriteAudioPacket — допоміжна функція для відправки аудіо
func WriteAudioPacket(track *webrtc.TrackLocalStaticSample, pkt av.Packet, codec av.AudioCodecData) error {
    // Перевірка чи дані у правильному форматі
    switch codec.Type() {
    case av.PCM_ALAW, av.PCM_MULAW:
        // G.711: 8kHz, mono, 8-bit
        if codec.SampleRate() != 8000 || codec.ChannelLayout().Count() != 1 {
            return fmt.Errorf("G.711 requires 8kHz mono")
        }
        
    case av.OPUS:
        // Opus: зазвичай 48kHz, stereo/mono
        // Перевірка параметрів...
    }
    
    // Відправка з реальним duration
    return track.WriteSample(media.Sample{
        Data:     pkt.Data,
        Duration: pkt.Duration,
    })
}
```

---

## 🔑 4. Таймери та моніторинг стану

### 🔧 Два незалежних таймери:

```go
type Muxer struct {
    // ...
    ClientACK *time.Timer  // таймер активності клієнта (5с)
    StreamACK *time.Timer  // таймер активності потоку (10с)
}
```

### 🔍 Як вони працюють:

```
1. ClientACK (скидається при повідомленні у DataChannel):
   • Клієнт може надсилати періодичні "сердцебиття" через DataChannel
   • Якщо 5с без повідомлень → з'єднання вважається неактивним
   • Захист від "завислих" клієнтів

2. StreamACK (скидається при кожному WritePacket):
   • Якщо 10с без відео/аудіо пакетів → потік неактивний
   • Захист від витоків ресурсів при зупинці джерела

3. Обидва таймери запускаються у `NewMuxer()`:
   tmp := Muxer{
       ClientACK: time.NewTimer(time.Second * 20),  // початковий таймаут 20с
       StreamACK: time.NewTimer(time.Second * 20),
       // ...
   }
```

### ⚠️ Важливе зауваження: таймери не зупиняються!

```
У поточному коді:
• `ClientACK.Reset()` та `StreamACK.Reset()` викликаються
• Але `time.Timer` не зупиняється автоматично при Reset()
• Це може призвести до витоку ресурсів (таймери продовжують працювати у фоні)

✅ Виправлення:
// Перед Reset() завжди зупиняйте таймер
if !element.ClientACK.Stop() {
    select {
    case <-element.ClientACK.C:  // зчитування якщо таймер вже спрацював
    default:
    }
}
element.ClientACK.Reset(5 * time.Second)
```

### ✅ Ваш use-case: надійний моніторинг активності

```go
// SafeResetTimer — безпечне скидання таймера
func SafeResetTimer(t *time.Timer, d time.Duration) {
    if !t.Stop() {
        select {
        case <-t.C:  // зчитування якщо таймер вже спрацював
        default:
        }
    }
    t.Reset(d)
}

// Використання у WritePacket():
if tmp, ok := element.streams[pkt.Idx]; ok {
    SafeResetTimer(element.StreamACK, 10*time.Second)
    // ... обробка пакету ...
}

// Використання у OnDataChannel:
peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
    d.OnMessage(func(msg webrtc.DataChannelMessage) {
        SafeResetTimer(element.ClientACK, 5*time.Second)
    })
})
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// webrtc_cctv_handler.go — обробка WebRTC для CCTV з підтримкою аудіо
type WebRTCCCTVHandler struct {
    channelID    string
    muxer        *webrtc.Muxer
    videoQueue   chan av.Packet
    audioQueue   chan av.Packet
    metrics      *WebRTCMetrics
    ctx          context.Context
    cancel       context.CancelFunc
}

func NewWebRTCCCTVHandler(channelID string, options webrtc.Options) (*WebRTCCCTVHandler, error) {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &WebRTCCCTVHandler{
        channelID:  channelID,
        muxer:      webrtc.NewMuxer(options),
        videoQueue: make(chan av.Packet, 1000),
        audioQueue: make(chan av.Packet, 500),
        metrics:    NewWebRTCMetrics(channelID),
        ctx:        ctx,
        cancel:     cancel,
    }, nil
}

// StartHandshake — початок WebRTC handshake
func (h *WebRTCCCTVHandler) StartHandshake(offerSDP64 string, streams []av.CodecData) (string, error) {
    // Валідація потоків
    var hasVideo bool
    for _, s := range streams {
        if s.Type().IsVideo() {
            if s.Type() != av.H264 {
                return "", fmt.Errorf("WebRTC requires H.264 video, got %v", s.Type())
            }
            hasVideo = true
        } else if s.Type().IsAudio() {
            if err := ValidateAudioCodec(s); err != nil {
                log.Printf("warning: skipping audio stream: %v", err)
            }
        }
    }
    if !hasVideo {
        return "", fmt.Errorf("at least one H.264 video stream required")
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

// SendVideoPacket — відправка відео пакету
func (h *WebRTCCCTVHandler) SendVideoPacket(pkt av.Packet) error {
    select {
    case h.videoQueue <- pkt:
        return nil
    default:
        h.metrics.VideoDropped.Inc()
        return nil  // не блокувати відправника
    }
}

// SendAudioPacket — відправка аудіо пакету
func (h *WebRTCCCTVHandler) SendAudioPacket(pkt av.Packet) error {
    select {
    case h.audioQueue <- pkt:
        return nil
    default:
        h.metrics.AudioDropped.Inc()
        return nil
    }
}

// StartSenders — запуск фонових відправників
func (h *WebRTCCCTVHandler) StartSenders() {
    // Відео відправник
    go func() {
        for {
            select {
            case <-h.ctx.Done():
                return
            case pkt := <-h.videoQueue:
                if err := h.muxer.WritePacket(pkt); err != nil {
                    if err == webrtc.ErrorClientOffline {
                        h.cancel()
                        return
                    }
                    h.metrics.VideoSendErrors.Inc()
                } else {
                    h.metrics.VideoPacketsSent.Inc()
                }
            }
        }
    }()
    
    // Аудіо відправник (якщо підтримується)
    go func() {
        for {
            select {
            case <-h.ctx.Done():
                return
            case pkt := <-h.audioQueue:
                if err := h.muxer.WritePacket(pkt); err != nil {
                    if err == webrtc.ErrorClientOffline {
                        h.cancel()
                        return
                    }
                    // Ігноруємо помилки для аудіо (менш критично)
                    h.metrics.AudioSendErrors.Inc()
                } else {
                    h.metrics.AudioPacketsSent.Inc()
                }
            }
        }
    }()
}

// Close — зупинка обробника
func (h *WebRTCCCTVHandler) Close() {
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
| **"gatherCompletePromise wait timeout"** | SDP answer не генерується вчасно | Перевірте мережеві налаштування; збільште таймаут; перевірте доступність STUN/TURN |
| **Аудіо не працює** | Пакети відправляються, але немає звуку | Переконайтеся, що `ChannelLayout` та `SampleRate` коректні; перевірте чи клієнт підтримує цей аудіо кодек |
| **Відео "зависає" на ключових кадрах** | Неправильна конвертація AVCC → Annex B | Переконайтеся, що `SplitNALUs()` працює коректно; перевірте чи SPS/PPS дійсно присутні у `CodecData` |
| **Таймери "витікають"** | Зростання використання пам'яті з часом | Використовуйте `SafeResetTimer()` замість прямого `Reset()` |
| **З'єднання не встановлюється за фаєрволом** | Статус `checking` → `disconnected` | Додайте TURN сервер у `ICEServers`; перевірте налаштування портів |

---

## ⚡ Оптимізації для real-time

### 1. Пакетна відправка NALU:

```go
// BatchWriteNALUs — відправка кількох NALU за один виклик
func (s *Stream) BatchWriteNALUs(nalus [][]byte, isKeyFrame bool, duration time.Duration, codec h264parser.CodecData) error {
    for i, nalu := range nalus {
        naltype := nalu[0] & 0x1f
        var data []byte
        
        if isKeyFrame && i == 0 {
            // Перший NALU у ключовому кадрі: додаємо SPS+PPS
            data = append([]byte{0,0,0,1}, 
                bytes.Join([][]byte{codec.SPS(), codec.PPS(), nalu}, []byte{0,0,0,1})...)
        } else {
            data = append([]byte{0,0,0,1}, nalu...)
        }
        
        if err := s.track.WriteSample(media.Sample{Data: data, Duration: duration}); err != nil {
            return err
        }
    }
    return nil
}
```

### 2. Кешування SPS/PPS префіксу:

```go
// SPSPPSPrefixCache — кешування префіксу для ключових кадрів
type SPSPPSPrefixCache struct {
    mu     sync.RWMutex
    sps, pps []byte
    prefix []byte  // [0,0,0,1] + SPS + [0,0,0,1] + PPS + [0,0,0,1]
}

func (c *SPSPPSPrefixCache) Update(sps, pps []byte) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if bytes.Equal(c.sps, sps) && bytes.Equal(c.pps, pps) {
        return
    }
    
    c.sps = append([]byte(nil), sps...)
    c.pps = append([]byte(nil), pps...)
    c.prefix = append([]byte{0,0,0,1}, 
        bytes.Join([][]byte{c.sps, c.pps}, []byte{0,0,0,1})...)
    c.prefix = append(c.prefix, []byte{0,0,0,1}...)
}

func (c *SPSPPSPrefixCache) Get() []byte {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return append([]byte(nil), c.prefix...)  // копія для безпеки
}
```

### 3. Моніторинг якості через RTCP:

```go
// RTCPHandler — обробка RTCP пакетів для контролю якості
func (s *Stream) RTCPHandler(rtpSender *webrtc.RTPSender, metrics *WebRTCMetrics) {
    go func() {
        rtcpBuf := make([]byte, 1500)
        for {
            _, rtcpPackets, err := rtpSender.Read(rtcpBuf)
            if err != nil {
                return
            }
            
            for _, pkt := range rtcpPackets {
                switch rtcp := pkt.(type) {
                case *rtcp.PictureLossIndication:
                    // Клієнт втратив кадр — можна відправити ключовий
                    metrics.KeyFrameRequests.Inc()
                    
                case *rtcp.ReceiverReport:
                    // Статистика отримання: втрати, jitter, тощо
                    for _, report := range rtcp.Reports {
                        metrics.PacketLoss.WithLabelValues(s.track.ID()).Set(float64(report.Lost) / float64(report.PacketsReceived + report.Lost))
                    }
                }
            }
        }
    }()
}
```

---

## 📋 Чек-лист інтеграції webrtc.Muxer v3

```go
// ✅ 1. Налаштування ICE для production
options := webrtc.Options{
    ICEServers: []string{"turn:your-turn-server.com:3478"},
    ICEUsername: "user",
    ICECredential: "pass",
    ICECandidates: []string{"your.external.ip"},
    PortMin: 10000,
    PortMax: 20000,
}
muxer := webrtc.NewMuxer(options)

// ✅ 2. Валідація потоків перед handshake
for _, s := range streams {
    if s.Type().IsVideo() && s.Type() != av.H264 {
        return fmt.Errorf("H.264 required")
    }
    if s.Type().IsAudio() {
        if err := ValidateAudioCodec(s); err != nil {
            log.Printf("warning: %v", err)
        }
    }
}

// ✅ 3. Безпечне скидання таймерів
SafeResetTimer(element.StreamACK, 10*time.Second)

// ✅ 4. Відправка з реальним duration
track.WriteSample(media.Sample{
    Data:     convertAVCCtoAnnexB(pkt),
    Duration: pkt.Duration,  // не фіксоване значення!
})

// ✅ 5. Обробка закриття
defer muxer.Close()

// ✅ 6. Моніторинг якості
metrics.PacketLoss.WithLabelValues(trackID).Set(lossRate)
```

---

## 🔗 Корисні посилання

- 💻 [pion/webrtc/v3 Documentation](https://pkg.go.dev/github.com/pion/webrtc/v3)
- 📄 [WebRTC H.264 RTP Payload Format (RFC 6184)](https://datatracker.ietf.org/doc/html/rfc6184)
- 📄 [G.711 in WebRTC](https://webrtc.googlesource.com/src/+/refs/heads/main/docs/native-code/audio/pcm.md)
- 🧪 [STUN/TURN Setup Guide](https://github.com/pion/turn)
- 🎬 [WebRTC for CCTV Use Cases](https://webrtc.github.io/samples/src/content/peerconnection/multiple/)

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **медіа в реальному часі**:
> 1. **Завжди використовуйте `GatheringCompletePromise`** — це гарантує надійне встановлення з'єднання.
> 2. **Розбивайте H.264 на окремі NALU** — це покращує сумісність з різними плеєрами.
> 3. **Використовуйте `pkt.Duration` замість фіксованих значень** — це критично для коректної синхронізації.
> 4. **Налаштуйте TURN для production** — STUN недостатній для клієнтів за симетричним NAT.
> 5. **Моніторьте втрати пакетів через RTCP** — це дозволяє адаптувати бітрейт або відправляти ключові кадри за потреби.

Потрібен приклад реалізації `ConvertAACtoOpus()` для підтримки аудіо AAC у WebRTC? Готовий допомогти! 🚀