# 📦 Глибокий розбір: `rtspv2.RTSPClient` — RTSP клієнт для vdk

Цей файл — **повноцінна реалізація RTSP клієнта** для бібліотеки `vdk`, що підтримує підключення до камер через RTSP/RTP over TCP, парсинг SDP, автентифікацію (Basic/Digest), та демуксинг аудіо/відео потоків у уніфіковані `av.Packet` об'єкти.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема rtspv2.RTSPClient

```
┌────────────────────────────────────────┐
│ 📦 rtspv2.RTSPClient — RTSP/TCP Client│
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • RTSPClient — основний клієнт        │
│  • Dial/ReplayDial — підключення       │
│  • request() — RTSP метод виклики      │
│  • startStream() — RTP демуксинг       │
│  • RTPDemuxer() — парсинг RTP пакетів  │
│                                         │
│  🔄 Потік даних:                        │
│  RTSP/RTP (TCP) → parse SDP → SETUP/PLAY│
│  → RTP packets → av.Packet queue       │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264, H.265                 │
│  • Аудіо: AAC, Opus, PCM A/μ-law       │
│  • Автентифікація: Basic, Digest       │
│  • RTSP over TLS (rtsps://)            │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. RTSPClient — основна структура клієнта

### Поля та їх призначення:

```go
type RTSPClient struct {
    // 📡 Мережеві параметри
    control             string           // базовий URL для команд
    pURL                *url.URL         // розпаршений URL
    conn                net.Conn         // TCP/TLS з'єднання
    connRW              *bufio.ReadWriter// буферизований reader/writer
    
    // 🔐 Автентифікація
    username, password  string
    realm, nonce        string           // для Digest auth
    clientDigest        bool             // чи використовується Digest
    clientBasic         bool             // чи використовується Basic
    
    // 📊 Сесія та послідовність
    seq                 int              // CSeq лічильник
    session             string           // RTSP session ID
    
    // 🎬 Медіа-потоки
    mediaSDP            []sdp.Media      // розпаршені медіа з SDP
    SDPRaw              []byte           // сирий SDP текст
    videoID, audioID    int              // interleaved channel IDs
    videoIDX, audioIDX  int8             // індекси у CodecData slice
    videoCodec, audioCodec av.CodecType  // типи кодеків
    
    // 🧬 Кодеки та параметри
    CodecData           []av.CodecData   // метадані кодеків
    sps, pps, vps       []byte           // параметр-сети для H.264/H.265
    FPS                 int              // FPS з SDP (якщо вказано)
    WaitCodec           bool             // чи очікуємо SPS/PPS у потоці
    
    // ⏱️ Таймінги
    startVideoTS, startAudioTS int64    // початкові RTP timestamp
    PreVideoTS, PreAudioTS    int64    // попередні timestamp для розрахунку duration
    AudioTimeScale            int64    // частота дискретизації аудіо
    AudioTimeLine             time.Duration
    
    // 📦 Буфери та черги
    BufferRtpPacket     *bytes.Buffer    // для збірки фрагментованих NALU
    OutgoingPacketQueue chan *av.Packet  // черга готових пакетів для обробки
    OutgoingProxyQueue  chan *[]byte     // для проксі режиму (сирий RTP)
    Signals             chan int         // сигнали подій (Stop, CodecUpdate)
    
    // ⚙️ Стан демуксингу
    fuStarted           bool             // стан FU-A фрагментації
    PreSequenceNumber   int              // попередній RTP sequence number
    sequenceNumber      int              // поточний sequence number
    timestamp           int64            // поточний RTP timestamp
    offset, end         int              // для парсингу NALU
    chTMP               int              // тимчасовий лічильник interleaved channels
    
    // 🔧 Опції
    options             RTSPClientOptions
}
```

### ✅ Ваш use-case: створення клієнта з валідацією

```go
// NewRTSPClient — безпечне створення клієнта з перевіркою параметрів
func NewRTSPClient(rawURL string, disableAudio bool, timeout time.Duration) (*rtspv2.RTSPClient, error) {
    // Валідація URL
    parsed, err := url.Parse(rawURL)
    if err != nil {
        return nil, fmt.Errorf("invalid URL: %w", err)
    }
    if parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps" {
        return nil, fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
    }
    
    // Налаштування опцій
    opts := rtspv2.RTSPClientOptions{
        URL:              rawURL,
        DialTimeout:      timeout,
        ReadWriteTimeout: timeout * 2,
        DisableAudio:     disableAudio,
        Debug:            false,  // вимкнути в production
    }
    
    // Підключення
    client, err := rtspv2.Dial(opts)
    if err != nil {
        return nil, fmt.Errorf("dial failed: %w", err)
    }
    
    // Реєстрація фіналізатора для очищення
    runtime.SetFinalizer(client, func(c *rtspv2.RTSPClient) {
        log.Printf("warning: RTSPClient not explicitly closed")
        c.Close()
    })
    
    return client, nil
}
```

---

## 🔑 2. Dial() — підключення та ініціалізація сесії

### 🔧 Потік ініціалізації:

```go
func Dial(options RTSPClientOptions) (*RTSPClient, error) {
    client := &RTSPClient{
        // Ініціалізація полів з дефолтними значеннями
        headers:             make(map[string]string),
        Signals:             make(chan int, 100),
        OutgoingProxyQueue:  make(chan *[]byte, 3000),
        OutgoingPacketQueue: make(chan *av.Packet, 3000),
        BufferRtpPacket:     bytes.NewBuffer([]byte{}),
        videoID: -1, audioID: -2,  // "не знайдено" значення
        videoIDX: -1, audioIDX: -2,
        options: options,
        AudioTimeScale: 8000,  // дефолт для G.711
    }
    client.headers["User-Agent"] = "Lavf58.76.100"  // ідентифікація як FFmpeg
    
    // 1. Парсинг URL
    err := client.parseURL(html.UnescapeString(client.options.URL))
    
    // 2. TCP/TLS підключення
    conn, err := net.DialTimeout("tcp", client.pURL.Host, client.options.DialTimeout)
    if client.pURL.Scheme == "rtsps" {
        // TLS handshake для rtsps://
        tlsConn := tls.Client(conn, &tls.Config{
            InsecureSkipVerify: options.InsecureSkipVerify,
            ServerName: client.pURL.Hostname(),
        })
        err = tlsConn.Handshake()
        conn = tlsConn
    }
    client.conn = conn
    client.connRW = bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
    
    // 3. RTSP handshake: OPTIONS → DESCRIBE → SETUP × N → PLAY
    client.request(OPTIONS, nil, client.pURL.String(), false, false)
    client.request(DESCRIBE, map[string]string{"Accept": "application/sdp"}, client.pURL.String(), false, false)
    
    // 4. Парсинг SDP та SETUP для кожного медіа
    for _, media := range client.mediaSDP {
        if media.AVType == VIDEO || (media.AVType == AUDIO && !options.DisableAudio) {
            // Розрахунок interleaved каналів: video=0-1, audio=2-3, тощо
            transport := fmt.Sprintf("RTP/AVP/TCP;unicast;interleaved=%d-%d", client.chTMP, client.chTMP+1)
            client.request(SETUP, map[string]string{"Transport": transport}, client.ControlTrack(media.Control), false, false)
            
            // Обробка відео кодека
            if media.AVType == VIDEO {
                switch media.Type {
                case av.H264:
                    if len(media.SpropParameterSets) >= 2 {
                        // SPS+PPS у SDP — можна створити CodecData одразу
                        codecData, _ := h264parser.NewCodecDataFromSPSAndPPS(media.SpropParameterSets[0], media.SpropParameterSets[1])
                        client.CodecData = append(client.CodecData, codecData)
                        client.sps = media.SpropParameterSets[0]
                        client.pps = media.SpropParameterSets[1]
                    } else {
                        // SPS/PPS очікуються у потоці
                        client.CodecData = append(client.CodecData, h264parser.CodecData{})
                        client.WaitCodec = true
                    }
                    client.videoCodec = av.H264
                    
                case av.H265:
                    // Аналогічно для H.265 з VPS+SPS+PPS
                    if len(media.SpropVPS) > 0 && len(media.SpropSPS) > 0 && len(media.SpropPPS) > 0 {
                        codecData, _ := h265parser.NewCodecDataFromVPSAndSPSAndPPS(media.SpropVPS, media.SpropSPS, media.SpropPPS)
                        client.CodecData = append(client.CodecData, codecData)
                    }
                    client.videoCodec = av.H265
                }
                client.videoIDX = int8(len(client.CodecData) - 1)
                client.videoID = client.chTMP
            }
            
            // Обробка аудіо кодека
            if media.AVType == AUDIO {
                var codecData av.AudioCodecData
                switch media.Type {
                case av.AAC:
                    codecData, _ = aacparser.NewCodecDataFromMPEG4AudioConfigBytes(media.Config)
                case av.OPUS:
                    layout := av.CH_MONO
                    if media.ChannelCount == 2 { layout = av.CH_STEREO }
                    codecData = codec.NewOpusCodecData(media.TimeScale, layout)
                case av.PCM_MULAW, av.PCM_ALAW:
                    // G.711 не потребує додаткових параметрів
                    if media.Type == av.PCM_MULAW {
                        codecData = codec.NewPCMMulawCodecData()
                    } else {
                        codecData = codec.NewPCMAlawCodecData()
                    }
                }
                if codecData != nil {
                    client.CodecData = append(client.CodecData, codecData)
                    client.audioIDX = int8(len(client.CodecData) - 1)
                    client.audioCodec = codecData.Type()
                    if media.TimeScale != 0 {
                        client.AudioTimeScale = int64(media.TimeScale)
                    }
                }
            }
            client.chTMP += 2  // наступна пара каналів
        }
    }
    
    // 5. Запуск потоку: PLAY
    err = client.request(PLAY, nil, client.control, false, false)
    
    // 6. Запуск фонового демуксингу
    go client.startStream()
    
    return client, nil
}
```

### 🔍 Чому `interleaved=0-1, 2-3`?

```
RTSP over TCP використовує interleaved режим для мультиплексування:
• Кожен медіа-потік отримує пару каналів: data channel, RTCP channel
• Формат пакету: [0x24][channel][length:2][payload]
• Приклад:
  • Відео: interleaved=0-1 → channel 0=відео дані, channel 1=відео RTCP
  • Аудіо: interleaved=2-3 → channel 2=аудіо дані, channel 3=аудіо RTCP

Це дозволяє передавати кілька потоків через одне TCP з'єднання без конфліктів.
```

### ✅ Ваш use-case: підключення з повторними спробами

```go
// DialWithRetry — підключення з експоненційною затримкою
func DialWithRetry(opts rtspv2.RTSPClientOptions, maxAttempts int) (*rtspv2.RTSPClient, error) {
    var client *rtspv2.RTSPClient
    var err error
    
    for attempt := 1; attempt <= maxAttempts; attempt++ {
        client, err = rtspv2.Dial(opts)
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

## 🔑 3. startStream() — фоновий демуксинг RTP пакетів

### 🔧 Основний цикл читання:

```go
func (client *RTSPClient) startStream() {
    defer func() {
        client.Signals <- SignalStreamRTPStop  // сигнал про завершення
    }()
    
    timer := time.Now()
    oneb := make([]byte, 1)
    header := make([]byte, 4)  // буфер для заголовку RTP пакету
    var fixed bool
    
    for {
        // Оновлення дедлайну для кожної операції читання
        err := client.conn.SetDeadline(time.Now().Add(client.options.ReadWriteTimeout))
        
        // Keep-alive: OPTIONS кожні 25 секунд
        if int(time.Now().Sub(timer).Seconds()) > 25 {
            err := client.request(OPTIONS, map[string]string{"Require": "implicit-play"}, client.control, false, true)
            timer = time.Now()
        }
        
        // Читання 4-байтового заголовку: [0x24][channel][length:2]
        if !fixed {
            nb, err := io.ReadFull(client.connRW, header)
            if err != nil || nb != 4 {
                return  // помилка читання
            }
        }
        fixed = false
        
        switch header[0] {
        case 0x24:  // RTP/RTCP пакет у interleaved режимі
            length := int32(binary.BigEndian.Uint16(header[2:]))
            if length > 65535 || length < 12 {  // мінімум = RTP header (12 байт)
                return  // некоректний розмір
            }
            
            // Читання всього пакету: заголовок + payload
            content := make([]byte, length+4)
            copy(content[:4], header)
            n, rerr := io.ReadFull(client.connRW, content[4:length+4])
            if rerr != nil || n != int(length) {
                return
            }
            
            // Проксі режим: відправка сирих байт у чергу
            if client.options.OutgoingProxy {
                if len(client.OutgoingProxyQueue) < 2000 {
                    client.OutgoingProxyQueue <- &content
                } else {
                    return  // черга переповнена
                }
            }
            
            // Демуксинг: RTP → av.Packet
            pkt, got := client.RTPDemuxer(&content)
            if !got {
                continue  // пропуск некоректних пакетів
            }
            
            // Відправка готових пакетів у чергу
            for _, p := range pkt {
                if len(client.OutgoingPacketQueue) > 2000 {
                    return  // черга переповнена
                }
                client.OutgoingPacketQueue <- p
            }
            
        case 0x52:  // 'R' — початок текстової відповіді (keep-alive)
            // Читання до кінця HTTP-подібної відповіді
            var responseTmp []byte
            for {
                n, rerr := io.ReadFull(client.connRW, oneb)
                if rerr != nil { return }
                responseTmp = append(responseTmp, oneb...)
                // Кінець заголовків: \r\n\r\n або перевищення буфера
                if (len(responseTmp) > 4 && bytes.Compare(responseTmp[len(responseTmp)-4:], []byte("\r\n\r\n")) == 0) || len(responseTmp) > 768 {
                    // Обробка Content-Length якщо є
                    if strings.Contains(string(responseTmp), "Content-Length:") {
                        si, _ := strconv.Atoi(stringInBetween(string(responseTmp), "Content-Length: ", "\r\n"))
                        cont := make([]byte, si)
                        io.ReadFull(client.connRW, cont)  // читання тіла
                    }
                    break
                }
            }
            
        default:  // Десинхронізація: не 0x24 і не 'R'
            return
        }
    }
}
```

### 🔍 Чому `header[0] == 0x24`?

```
У RTSP over TCP interleaved режимі, кожен пакет має формат:
  [0][1][2-3][4...]
   ↑  ↑   ↑     ↑
   |  |   |     └─ payload (length байт)
   |  |   └─ 2-byte big-endian length
   |  └─ channel ID (0,1,2,3...)
   └─ magic byte 0x24 ('$')

Це дозволяє мультиплексувати кілька потоків через одне з'єднання.
```

### ✅ Ваш use-case: обробка пакетів з черги

```go
// StartPacketProcessor — запуск обробника пакетів з черги
func (p *RTSPProcessor) StartPacketProcessor(ctx context.Context) {
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
                case rtspv2.SignalStreamRTPStop:
                    log.Printf("Stream stopped, shutting down")
                    return
                case rtspv2.SignalCodecUpdate:
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

## 🔑 4. request() — RTSP метод виклики з автентифікацією

### 🔧 Логіка запиту:

```go
func (client *RTSPClient) request(method string, customHeaders map[string]string, uri string, one bool, nores bool) (err error) {
    client.seq++  // інкремент CSeq
    
    // Побудова запиту
    builder := bytes.Buffer{}
    builder.WriteString(fmt.Sprintf("%s %s RTSP/1.0\r\n", method, uri))
    builder.WriteString(fmt.Sprintf("CSeq: %d\r\n", client.seq))
    
    // Автентифікація
    if client.clientDigest {
        builder.WriteString(fmt.Sprintf("Authorization: %s\r\n", client.createDigest(method, uri)))
    }
    
    // Додаткові заголовки
    for k, v := range customHeaders {
        builder.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
    }
    for k, v := range client.headers {
        builder.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
    }
    builder.WriteString("\r\n")
    
    // Відправка
    _, err = client.connRW.WriteString(builder.String())
    err = client.connRW.Flush()
    
    if nores {
        return  // не очікуємо відповіді
    }
    
    // Читання відповіді
    var isPrefix bool
    var line []byte
    var contentLen int
    res := make(map[string]string)
    
    for {
        line, isPrefix, err = client.connRW.ReadLine()
        if err != nil { return }
        
        // Перевірка статусу
        if strings.Contains(string(line), "RTSP/1.0") && (!strings.Contains(string(line), "200") && !strings.Contains(string(line), "401")) {
            return errors.New("Camera send status " + string(line))
        }
        
        // Парсинг заголовків
        if len(line) == 0 { break }  // кінець заголовків
        splits := strings.SplitN(string(line), ":", 2)
        if len(splits) == 2 {
            res[strings.TrimSpace(splits[0])] = strings.TrimSpace(splits[1])
        }
    }
    
    // Обробка 401 Unauthorized
    if val, ok := res["WWW-Authenticate"]; ok {
        if strings.Contains(val, "Digest") {
            // Парсинг realm/nonce для Digest auth
            client.realm = stringInBetween(val, "realm=\"", "\"")
            client.nonce = stringInBetween(val, "nonce=\"", "\"")
            client.clientDigest = true
        } else if strings.Contains(val, "Basic") {
            // Basic auth: base64(username:password)
            client.headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(client.username+":"+client.password))
            client.clientBasic = true
        }
        // Повторний запит з автентифікацією
        if !one {
            return client.request(method, customHeaders, uri, true, false)
        }
        return errors.New("RTSP Client Unauthorized 401")
    }
    
    // Збереження session ID
    if val, ok := res["Session"]; ok {
        client.session = strings.TrimSpace(strings.Split(val, ";")[0])
        client.headers["Session"] = client.session
    }
    
    // Парсинг SDP для DESCRIBE
    if method == DESCRIBE {
        if val, ok := res["Content-Length"]; ok {
            contentLen, _ = strconv.Atoi(strings.TrimSpace(val))
            client.SDPRaw = make([]byte, contentLen)
            io.ReadFull(client.connRW, client.SDPRaw)
            _, client.mediaSDP = sdp.Parse(string(client.SDPRaw))
        }
    }
    
    // Парсинг interleaved каналів для SETUP
    if method == SETUP {
        if val, ok := res["Transport"]; ok {
            // Пошук interleaved=0-1
            for _, part := range strings.Split(val, ";") {
                if strings.Contains(part, "interleaved=") {
                    splits := strings.Split(strings.Split(part, "=")[1], "-")
                    if len(splits) == 2 {
                        client.chTMP, _ = strconv.Atoi(splits[0])
                    }
                }
            }
        }
    }
    
    return nil
}
```

### 🔧 createDigest() — Digest автентифікація:

```go
func (client *RTSPClient) createDigest(method string, uri string) string {
    // HA1 = MD5(username:realm:password)
    md5UserRealmPwd := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s:%s:%s", client.username, client.realm, client.password))))
    
    // HA2 = MD5(method:uri)
    md5MethodURL := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s:%s", method, uri))))
    
    // response = MD5(HA1:nonce:HA2)
    response := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s:%s:%s", md5UserRealmPwd, client.nonce, md5MethodURL))))
    
    return fmt.Sprintf("Digest username=\"%s\", realm=\"%s\", nonce=\"%s\", uri=\"%s\", response=\"%s\"", 
        client.username, client.realm, client.nonce, uri, response)
}
```

### ✅ Ваш use-case: безпечна автентифікація

```go
// RTSPAuth — безпечна обгортка для автентифікації
type RTSPAuth struct {
    username string
    password string  // зберігаємо у відкритому вигляді тільки в пам'яті!
}

// Clear — очищення чутливих даних
func (a *RTSPAuth) Clear() {
    for i := range a.password {
        a.password[i] = 0  // перезапис нулями
    }
    a.password = ""
}

// Використання:
auth := &RTSPAuth{username: "admin", password: "secret123"}
defer auth.Clear()  // гарантоване очищення

// У Dial():
opts := rtspv2.RTSPClientOptions{
    URL: fmt.Sprintf("rtsp://%s:%s@camera.local/stream", auth.username, auth.password),
    // ...
}
```

---

## 🔑 5. RTPDemuxer() — парсинг RTP пакетів у av.Packet

> ⚠️ Ця функція не показана у наданому коді, але критично важлива. Ось її ймовірна логіка:

```go
// RTPDemuxer — парсинг RTP пакету у av.Packet (спрощена версія)
func (client *RTSPClient) RTPDemuxer(content *[]byte) ([]*av.Packet, bool) {
    channel := (*content)[1]
    payload := (*content)[4:]
    
    // Пропуск RTCP пакетів
    if isRTCPPacket(payload) {
        return nil, false
    }
    
    // Визначення потоку за channel ID
    var idx int8
    var isVideo bool
    if int(channel) == client.videoID {
        idx = client.videoIDX
        isVideo = true
    } else if int(channel) == client.audioID {
        idx = client.audioIDX
        isVideo = false
    } else {
        return nil, false  // невідомий канал
    }
    
    // Парсинг RTP заголовку (12 байт)
    if len(payload) < RTPHeaderSize {
        return nil, false
    }
    
    // Витягування полів
    // [0]: V=2, P, X, CC
    // [1]: M, PT
    // [2-3]: sequence number
    // [4-7]: timestamp
    // [8-11]: SSRC
    
    sequenceNumber := int(binary.BigEndian.Uint16(payload[2:4]))
    timestamp := int64(binary.BigEndian.Uint32(payload[4:8]))
    
    // Пропуск RTP header + CSRC + extension
    headerSize := RTPHeaderSize + 4*int(payload[0]&0x0F)  // CC field
    if (payload[0] & 0x10) != 0 {  // extension flag
        extLen := int(binary.BigEndian.Uint16(payload[headerSize+2:headerSize+4]))
        headerSize += 4 + extLen*4
    }
    
    rtpPayload := payload[headerSize:]
    
    // Обробка за типом кодека
    if isVideo {
        switch client.videoCodec {
        case av.H264:
            return client.demuxH264(rtpPayload, timestamp, sequenceNumber, idx)
        case av.H265:
            return client.demuxH265(rtpPayload, timestamp, sequenceNumber, idx)
        }
    } else {
        switch client.audioCodec {
        case av.AAC:
            return client.demuxAAC(rtpPayload, timestamp, idx)
        case av.OPUS, av.PCM_MULAW, av.PCM_ALAW:
            return client.demuxAudio(rtpPayload, timestamp, idx)
        }
    }
    
    return nil, false
}
```

### 🔧 Приклад: demuxH264() — обробка H.264 RTP:

```go
func (client *RTSPClient) demuxH264(payload []byte, timestamp int64, seq int, idx int8) ([]*av.Packet, bool) {
    if len(payload) < 1 {
        return nil, false
    }
    
    naluHeader := payload[0] & 0x1F
    var nalus [][]byte
    
    switch {
    case naluHeader >= 1 && naluHeader <= 23:  // Single NALU
        nalus = [][]byte{payload}
        
    case naluHeader == 24:  // STAP-A (aggregation)
        // Розбиття на кілька NALU
        offset := 1
        for offset < len(payload) {
            if offset+2 > len(payload) { break }
            naluLen := int(binary.BigEndian.Uint16(payload[offset:offset+2]))
            offset += 2
            if offset+naluLen > len(payload) { break }
            nalus = append(nalus, payload[offset:offset+naluLen])
            offset += naluLen
        }
        
    case naluHeader == 28:  // FU-A (fragmentation)
        fuHeader := payload[1]
        start := (fuHeader & 0x80) != 0
        end := (fuHeader & 0x40) != 0
        naluType := fuHeader & 0x1F
        
        if start {
            // Початок фрагмента: зберігаємо у буфері
            client.BufferRtpPacket.Reset()
            client.BufferRtpPacket.WriteByte((payload[0] & 0xE0) | naluType)
            client.BufferRtpPacket.Write(payload[2:])
            client.fuStarted = true
        } else if client.fuStarted {
            // Продовження: додаємо у буфер
            client.BufferRtpPacket.Write(payload[2:])
            if end {
                // Кінець фрагмента: формуємо повний NALU
                nalus = [][]byte{client.BufferRtpPacket.Bytes()}
                client.fuStarted = false
            }
        }
        
    default:
        return nil, false  // непідтримуваний тип
    }
    
    // Конвертація у av.Packet
    var packets []*av.Packet
    for _, nalu := range nalus {
        if len(nalu) == 0 { continue }
        
        nalType := nalu[0] & 0x1F
        isKeyFrame := nalType == 5  // IDR
        
        // Оновлення SPS/PPS якщо знайдено
        if nalType == 7 {
            client.CodecUpdateSPS(nalu)
        } else if nalType == 8 {
            client.CodecUpdatePPS(nalu)
        }
        
        // Розрахунок duration
        var duration time.Duration
        if client.PreVideoTS != 0 {
            delta := timestamp - client.PreVideoTS
            if client.FPS > 0 {
                duration = time.Second / time.Duration(client.FPS)
            } else if delta > 0 {
                duration = time.Duration(delta * 1e9 / 90000)  // припускаємо 90kHz clock
            }
        }
        client.PreVideoTS = timestamp
        
        // Додавання 4-байтового префіксу для AVCC формату
        data := append(binSize(len(nalu)), nalu...)
        
        packets = append(packets, &av.Packet{
            Idx:        idx,
            Time:       time.Duration(timestamp * 1e9 / 90000),  // конвертація у time.Duration
            Duration:   duration,
            IsKeyFrame: isKeyFrame,
            Data:       data,
        })
    }
    
    return packets, len(packets) > 0
}
```

### ✅ Ваш use-case: обробка фрагментованих NALU

```go
// HandleFragmentedNALU — валідація FU-A фрагментації
func (c *RTSPClient) HandleFragmentedNALU(payload []byte) error {
    if len(payload) < 2 {
        return fmt.Errorf("FU-A payload too short")
    }
    
    naluHeader := payload[0] & 0x1F
    if naluHeader != 28 {  // FU-A
        return nil  // не FU-A, стандартна обробка
    }
    
    fuHeader := payload[1]
    start := (fuHeader & 0x80) != 0
    end := (fuHeader & 0x40) != 0
    
    if start && end {
        // ⚠️ Це помилка: start і end не можуть бути одночасно
        return fmt.Errorf("invalid FU-A: start and end both set")
    }
    
    if start {
        // Початок нового фрагмента: очищення буфера
        c.BufferRtpPacket.Reset()
    }
    
    return nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// rtsp_cctv_processor.go — обробка RTSP потоку для CCTV HLS Processor
type RTSPCCTVProcessor struct {
    channelID    string
    client       *rtspv2.RTSPClient
    packetQueue  chan *av.Packet
    metrics      *RTSPMetrics
    ctx          context.Context
    cancel       context.CancelFunc
}

func NewRTSPCCTVProcessor(channelID, rtspURL string) (*RTSPCCTVProcessor, error) {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &RTSPCCTVProcessor{
        channelID:   channelID,
        packetQueue: make(chan *av.Packet, 1000),
        metrics:     NewRTSPMetrics(channelID),
        ctx:         ctx,
        cancel:      cancel,
    }, nil
}

// Start — запуск обробки потоку
func (p *RTSPCCTVProcessor) Start() error {
    // 1. Підключення до RTSP з повторними спробами
    opts := rtspv2.RTSPClientOptions{
        URL:              p.rtspURL,
        DialTimeout:      5 * time.Second,
        ReadWriteTimeout: 10 * time.Second,
        DisableAudio:     false,
        Debug:            false,
    }
    
    client, err := DialWithRetry(opts, 3)
    if err != nil {
        return fmt.Errorf("connect RTSP: %w", err)
    }
    p.client = client
    
    // 2. Запуск обробника пакетів
    go p.StartPacketProcessor(p.ctx)
    
    // 3. Запуск моніторингу метрик
    go p.monitorMetrics(p.ctx)
    
    log.Printf("Channel %s: RTSP processing started", p.channelID)
    return nil
}

// StartPacketProcessor — обробка пакетів з черги
func (p *RTSPCCTVProcessor) StartPacketProcessor(ctx context.Context) {
    var start bool
    var lastKeyFrameTime time.Duration
    
    for {
        select {
        case <-ctx.Done():
            return
            
        case pkt := <-p.client.OutgoingPacketQueue:
            // Детекція ключових кадрів
            if pkt.IsKeyFrame {
                start = true
                lastKeyFrameTime = pkt.Time
                p.metrics.KeyFramesReceived.Inc()
            }
            
            if !start {
                p.metrics.PacketsSkippedBeforeKeyFrame.Inc()
                continue
            }
            
            // Відправка у чергу для подальшої обробки (напр. HLS muxing)
            select {
            case p.packetQueue <- pkt:
                p.metrics.PacketsForwarded.Inc()
            default:
                p.metrics.DroppedPackets.Inc()
            }
            
        case signal := <-p.client.Signals:
            switch signal {
            case rtspv2.SignalStreamRTPStop:
                log.Printf("Channel %s: stream stopped", p.channelID)
                p.cancel()
                return
            case rtspv2.SignalCodecUpdate:
                // Оновлення кодека для muxer
                if err := p.updateCodecData(p.client.CodecData); err != nil {
                    log.Printf("codec update failed: %v", err)
                }
            }
        }
    }
}

// Stop — зупинка обробки
func (p *RTSPCCTVProcessor) Stop() {
    p.cancel()
    
    if p.client != nil {
        p.client.Close()
    }
    
    log.Printf("Channel %s: processing stopped", p.channelID)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"RTSP Client Unauthorized 401"** | Неправильні облікові дані або метод автентифікації | Перевірте username/password; переконайтеся, що камера підтримує Basic/Digest; спробуйте `InsecureSkipVerify` для rtsps |
| **"Incorrect Packet Size"** | RTP пакет має некоректну довжину | Перевірте мережеву стабільність; можливе обрізання пакетів через MTU або фаєрвол |
| **SPS/PPS не оновлюються** | `WaitCodec = true` залишається, відео не декодується | Переконайтеся, що камера надсилає SPS/PPS у потоці; перевірте `CodecUpdateSPS/PPS` логи |
| **Аудіо розсинхронізоване** | `AudioTimeScale` не співпадає з реальним | Перевірте `TimeScale` у SDP; для AAC зазвичай 90000, для G.711 — 8000 |
| **Черга переповнена** | `OutgoingPacketQueue full` | Збільшіть розмір черги; оптимізуйте обробника пакетів; перевірте чи не зависла обробка |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування буферів для RTP демуксингу:

```go
// RTPBufferPool — пул буферів для уникнення аллокацій
var RTPBufferPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 2048)  // типовий розмір RTP пакету
        return &buf
    },
}

func GetRTPBuffer() *[]byte { return RTPBufferPool.Get().(*[]byte) }
func PutRTPBuffer(buf *[]byte) { RTPBufferPool.Put(buf) }

// Використання у startStream():
buf := GetRTPBuffer()
defer PutRTPBuffer(buf)
// ... читання у buf ...
```

### 2. Пакетна обробка для зменшення накладних витрат:

```go
// BatchProcessPackets — обробка кількох пакетів за один виклик
func (p *RTSPCCTVProcessor) BatchProcessPackets(packets []*av.Packet) error {
    for _, pkt := range packets {
        if err := p.processPacket(pkt); err != nil {
            return err
        }
    }
    return nil
}
```

### 3. Моніторинг продуктивності демуксингу:

```go
type RTSPMetrics struct {
    PacketsReceived prometheus.CounterVec
    DemuxLatency    prometheus.HistogramVec
    KeyFrames       prometheus.CounterVec
    DecodeErrors    prometheus.CounterVec
}

func (m *RTSPMetrics) RecordPacket(duration time.Duration, channelID string) {
    m.PacketsReceived.WithLabelValues(channelID).Inc()
    m.DemuxLatency.WithLabelValues(channelID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції rtspv2.RTSPClient

```go
// ✅ 1. Валідація вхідного URL
parsed, err := url.Parse(rawURL)
if err != nil || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") {
    return fmt.Errorf("invalid RTSP URL")
}

// ✅ 2. Підключення з таймаутами
opts := rtspv2.RTSPClientOptions{
    URL: rawURL,
    DialTimeout: 5 * time.Second,
    ReadWriteTimeout: 10 * time.Second,
}
client, err := rtspv2.Dial(opts)

// ✅ 3. Обробка сигналів у окремій горутині
go func() {
    for signal := range client.Signals {
        switch signal {
        case rtspv2.SignalCodecUpdate:
            // оновити muxer
        case rtspv2.SignalStreamRTPStop:
            // завершити обробку
        }
    }
}()

// ✅ 4. Читання пакетів з перевіркою контексту
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
}()

// ✅ 6. Метрики для моніторингу
metrics.RecordPacket(time.Since(start), channelID)
```

---

## 🔗 Корисні посилання

- 💻 [vdk rtspv2 Package](https://pkg.go.dev/github.com/deepch/vdk/format/rtspv2) — GoDoc documentation
- 📄 [RTSP Specification (RFC 2326)](https://datatracker.ietf.org/doc/html/rfc2326) — офіційний стандарт
- 📄 [RTP Payload Format for H.264 (RFC 6184)](https://datatracker.ietf.org/doc/html/rfc6184) — деталі кодування
- 📄 [SDP: Session Description Protocol (RFC 4566)](https://datatracker.ietf.org/doc/html/rfc4566) — формат опису сесій
- 🧪 [Go net.Dial Documentation](https://pkg.go.dev/net#DialTimeout) — робота з мережевими з'єднаннями

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **камерами в реальному часі**:
> 1. **Завжди обробляйте `SignalStreamRTPStop`** — це єдиний спосіб дізнатися про обрив з'єднання з камери.
> 2. **Кешуйте SPS/PPS окремо** — деякі камери надсилають їх нерегулярно; без кешування відео не декодується.
> 3. **Збільшуйте буфер `OutgoingPacketQueue`** для високобітрейтних потоків — 3000 пакетів можуть заповнитися за секунди.
> 4. **Моніторьте `AudioTimeScale`** — неправильне значення призведе до розсинхронізації аудіо/відео.
> 5. **Тестуйте з різними камерами** — реалізація RTSP може відрізнятися між виробниками (ONVIF, Hikvision, Dahua, тощо).

Потрібен приклад реалізації `demuxAAC()` для коректної обробки аудіо пакетів з RTSP? Готовий допомогти! 🚀