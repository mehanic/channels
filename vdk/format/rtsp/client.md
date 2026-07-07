# 📦 Глибокий розбір: `rtsp.Client` — RTSP клієнт для vdk

Цей файл — **повноцінна реалізація RTSP клієнта** для бібліотеки `vdk`, що підтримує підключення до камер через TCP, парсинг SDP, автентифікацію (Basic/Digest), та демуксинг аудіо/відео потоків (H.264, AAC, Opus, G.711) у уніфіковані `av.Packet` об'єкти.

---

## 🗺️ Архітектурна схема rtsp.Client

```
┌────────────────────────────────────────┐
│ 📦 rtsp.Client — RTSP/TCP Client      │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Client — основний клієнт            │
│  • Stream — обробка окремого потоку    │
│  • Dial/Options/Describe/Setup/Play   │
│  • readPacket/handleBlock — демуксинг │
│                                         │
│  🔄 Потік даних:                        │
│  RTSP/TCP → SDP parse → SETUP/PLAY    │
│  → RTP blocks → av.Packet queue       │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (Single/FU-A/STAP-A)  │
│  • Аудіо: AAC, Opus, PCMA/PCMU        │
│  • Auth: Basic, Digest                │
│  • Redirect: 302 handling             │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Client — основна структура

### Поля та їх призначення:

```go
type Client struct {
    // 🎛️ Конфігурація
    DebugRtsp, DebugRtp     bool     // логування
    DisableAudio            bool     // ігнорувати аудіо потоки
    Headers                 []string // додаткові заголовки
    SkipErrRtpBlock         bool     // продовжувати при помилках RTP
    
    // ⏱️ Таймаути
    RtspTimeout             time.Duration
    RtpTimeout              time.Duration
    RtpKeepAliveTimeout     time.Duration
    rtpKeepaliveTimer       time.Time
    
    // 🔄 Стан машини
    stage                   int  // stageOptionsDone → ... → stageCodecDataDone
    setupIdx, setupMap      []int // мапінг індексів потоків
    session                 string // RTSP session ID
    cseq                    uint   // лічильник CSeq
    
    // 🔐 Автентифікація
    authHeaders             func(string) []string // callback для Authorization
    
    // 🌐 Мережа
    url                     *url.URL
    conn                    *connWithTimeout
    brconn                  *bufio.Reader
    requestUri              string
    
    // 🎬 Потоки
    streams                 []*Stream
    streamsintf             []av.CodecData  // кешовані кодеки
    body                    io.Reader
}
```

### 🔧 Стани обробки (stage):

```go
const (
    stageOptionsDone = iota + 1  // 1
    stageDescribeDone            // 2
    stageSetupDone               // 3
    stageWaitCodecData           // 4
    stageCodecDataDone           // 5 ← готовий до readPacket()
)
```

**Потік ініціалізації**:
```
0 → Options() → 1 → Describe() → 2 → SetupAll() → 3 
→ Play() → 4/5 → probe() → 5 → готовий
```

---

## 🔑 2. RTSP handshake: Options → Describe → Setup → Play

### 🔧 Options():

```go
func (self *Client) Options() (err error) {
    req := Request{Method: "OPTIONS", Uri: self.requestUri}
    if self.session != "" {
        req.Header = append(req.Header, "Session: "+self.session)
    }
    if err = self.WriteRequest(req); err != nil { return }
    if _, err = self.ReadResponse(); err != nil { return }
    self.stage = stageOptionsDone
    return
}
```

**Призначення**: Перевірка підтримки методів сервером. Відповідь має містити `Public: OPTIONS, DESCRIBE, SETUP, PLAY`.

---

### 🔧 Describe():

```go
func (self *Client) Describe() (streams []sdp.Media, err error) {
    // Повторні спроби при 401/302
    for i := 0; i < 2; i++ {
        req := Request{Method: "DESCRIBE", Uri: self.requestUri, 
                      Header: []string{"Accept: application/sdp"}}
        if err = self.WriteRequest(req); err != nil { return }
        if res, err = self.ReadResponse(); err != nil { return }
        if res.StatusCode == 200 { break }
    }
    
    // Парсинг SDP
    body := string(res.Body)
    _, medias := sdp.Parse(body)
    
    // Ініціалізація Stream об'єктів
    self.streams = []*Stream{}
    for _, media := range medias {
        stream := &Stream{Sdp: media, client: self}
        if err = stream.makeCodecData(); err != nil && DebugRtsp {
            fmt.Println("rtsp: makeCodecData error", err)
        }
        self.streams = append(self.streams, stream)
        streams = append(streams, media)
    }
    self.stage = stageDescribeDone
    return
}
```

**Ключові моменти**:
- Автоматична обробка `401 Unauthorized` та `302 Redirect` у `handleResp()`
- `makeCodecData()` витягує `SPS/PPS` для H.264, `AudioSpecificConfig` для AAC
- Якщо параметрів немає у SDP — `CodecData = nil`, очікування у потоці

---

### 🔧 SetupAll() / Setup():

```go
func (self *Client) Setup(idx []int) (err error) {
    if err = self.prepare(stageDescribeDone); err != nil { return }
    
    self.setupMap = make([]int, len(self.streams))
    for i := range self.setupMap { self.setupMap[i] = -1 }
    self.setupIdx = idx
    
    for i, si := range idx {
        self.setupMap[si] = i
        
        // Формування URI: абсолютний або відносний
        uri := self.streams[si].Sdp.Control
        if !strings.HasPrefix(uri, "rtsp://") {
            uri = self.requestUri + "/" + uri
        }
        
        req := Request{Method: "SETUP", Uri: uri}
        // TCP interleaved: channel = si*2 (data), si*2+1 (RTCP)
        req.Header = append(req.Header, 
            fmt.Sprintf("Transport: RTP/AVP/TCP;unicast;interleaved=%d-%d", si*2, si*2+1))
        if self.session != "" {
            req.Header = append(req.Header, "Session: "+self.session)
        }
        
        if err = self.WriteRequest(req); err != nil { return }
        if _, err = self.ReadResponse(); err != nil { return }
    }
    
    if self.stage == stageDescribeDone {
        self.stage = stageSetupDone
    }
    return
}
```

**🔍 Чому `interleaved=si*2, si*2+1`?**
- RTP over TCP використовує interleaved формат: `[$][channel][len:2][payload]`
- Парні канали = дані, непарні = RTCP
- Для потоку 0: channel 0=відео, 1=відео RTCP; для потоку 1: 2=аудіо, 3=аудіо RTCP

---

### 🔧 Play():

```go
func (self *Client) Play() (err error) {
    req := Request{Method: "PLAY", Uri: self.requestUri}
    req.Header = append(req.Header, "Range: npt=0.000-")  // з початку
    req.Header = append(req.Header, "Session: "+self.session)
    if err = self.WriteRequest(req); err != nil { return }
    
    if self.allCodecDataReady() {
        self.stage = stageCodecDataDone  // готовий одразу
    } else {
        self.stage = stageWaitCodecData  // потрібно probe()
    }
    return
}
```

---

## 🔑 3. Автентифікація: Basic та Digest

### 🔧 handle401():

```go
func (self *Client) handle401(res *Response) (err error) {
    authval := res.Headers.Get("WWW-Authenticate")
    hdrval := strings.SplitN(authval, " ", 2)
    
    if len(hdrval) == 2 {
        var realm, nonce string
        for _, field := range strings.Split(hdrval[1], ",") {
            field = strings.Trim(field, ", ")
            if keyval := strings.Split(field, "="); len(keyval) == 2 {
                key := keyval[0]
                val := strings.Trim(keyval[1], `"`)
                switch key {
                case "realm": realm = val
                case "nonce": nonce = val
                }
            }
        }
        
        if realm != "" {
            username := self.url.User.Username()
            password, _ := self.url.User.Password()
            
            self.authHeaders = func(method string) []string {
                var headers []string
                if nonce == "" {
                    // Basic auth
                    headers = []string{fmt.Sprintf(`Authorization: Basic %s`, 
                        base64.StdEncoding.EncodeToString([]byte(username+":"+password)))}
                } else {
                    // Digest auth
                    hs1 := md5hash(username + ":" + realm + ":" + password)
                    hs2 := md5hash(method + ":" + self.requestUri)
                    response := md5hash(hs1 + ":" + nonce + ":" + hs2)
                    headers = []string{fmt.Sprintf(
                        `Authorization: Digest username="%s", realm="%s", nonce="%s", uri="%s", response="%s"`,
                        username, realm, nonce, self.requestUri, response)}
                }
                return headers
            }
        }
    }
    return
}
```

**✅ Ваш use-case: безпечне зберігання паролів**

```go
// RTSPAuth — обгортка для очищення чутливих даних
type RTSPAuth struct {
    username, password string
}

func (a *RTSPAuth) Clear() {
    for i := range a.password { a.password[i] = 0 }
    a.password = ""
}

// Використання:
auth := &RTSPAuth{username: "admin", password: "secret"}
defer auth.Clear()

parsedURL, _ := url.Parse(fmt.Sprintf("rtsp://%s:%s@camera.local/stream", 
    auth.username, auth.password))
client, _ := rtsp.Dial(parsedURL.String())
```

---

## 🔑 4. Демуксинг: readPacket() → handleBlock() → Stream.handleRtpPacket()

### 🔧 Основний цикл readPacket():

```go
func (self *Client) readPacket() (pkt av.Packet, err error) {
    if err = self.SendRtpKeepalive(); err != nil { return }
    
    for {
        var res Response
        // poll() читає RTSP відповіді або RTP блоки
        for {
            if res, err = self.poll(); err != nil { return }
            if len(res.Block) > 0 { break }  // знайдено RTP блок
        }
        
        var ok bool
        if pkt, ok, err = self.handleBlock(res.Block); err != nil { return }
        if ok { return pkt, nil }  // готовий пакет
    }
}
```

### 🔧 poll() — розпізнавання форматів:

```go
func (self *Client) poll() (res Response, err error) {
    var block []byte
    var rtsp []byte
    
    self.conn.Timeout = self.RtspTimeout
    for {
        // 1. Спроба знайти RTP блок ($...) або RTSP заголовок (RTSP/1.0...)
        if block, rtsp, err = self.findRTSP(); err != nil { return }
        
        if len(block) > 0 {
            res.Block = block  // RTP блок
            return
        } else if len(rtsp) > 0 {
            // 2. Читання решти заголовків до \r\n\r\n
            if block, headers, err = self.readLFLF(); err != nil { return }
            if len(block) > 0 {
                res.Block = block
                return
            }
            // 3. Парсинг повної відповіді
            if res, err = self.readResp(append(rtsp, headers...)); err != nil { return }
        }
        return
    }
}
```

### 🔧 findRTSP() — пошук початку повідомлення:

```go
func (self *Client) findRTSP() (block []byte, data []byte, err error) {
    const ( R, T, S, Header, Dollar )  // стани автомату
    var peek [8]byte
    stat := 0
    
    for {
        b, err := self.brconn.ReadByte()
        if err != nil { return nil, nil, err }
        
        // Детект "RTSP/1.0" або "$" (початок RTP блоку)
        switch b {
        case 'R': if stat == 0 { stat = R }
        case 'T': if stat == R { stat = T }
        case 'S': if stat == T { stat = S }
        case 'P': if stat == S { stat = Header }
        case '$': 
            if stat != Dollar {
                stat = Dollar
                peek = peek[0:0]
            }
        default:
            if stat != Dollar { stat = 0; peek = peek[0:0] }
        }
        
        if stat != 0 { peek = append(peek, b) }
        
        if stat == Header {
            data = peek
            return  // знайдено RTSP заголовок
        }
        
        if stat == Dollar && len(peek) >= 12 {
            // Перевірка заголовку RTP блоку
            if blocklen, _, ok := self.parseBlockHeader(peek); ok {
                left := blocklen + 4 - len(peek)
                if left >= 0 {
                    block = append(peek, make([]byte, left)...)
                    if _, err = io.ReadFull(self.brconn, block[len(peek):]); err != nil { return }
                    return  // повний RTP блок
                }
            }
            stat = 0
            peek = peek[0:0]
        }
    }
}
```

### 🔧 parseBlockHeader() — валідація RTP блоку:

```go
func (self *Client) parseBlockHeader(h []byte) (length int, no int, valid bool) {
    length = int(h[2])<<8 + int(h[3])  // 2-byte big-endian length
    no = int(h[1])                      // channel ID
    
    if no/2 >= len(self.streams) { return }  // невідомий потік
    
    if no%2 == 0 {  // RTP (парний канал)
        if length < 8 { return }  // мінімум = RTP header
        
        // Перевірка RTP версії (біти 7-6 = 0b10)
        if h[4]&0xc0 != 0x80 { return }
        
        stream := self.streams[no/2]
        // Перевірка payload type
        if int(h[5]&0x7f) != stream.Sdp.PayloadType { return }
        
        // Перевірка timestamp (захист від переповнення/дрейфу)
        timestamp := binary.BigEndian.Uint32(h[8:12])
        if stream.firsttimestamp != 0 {
            timestamp -= stream.firsttimestamp
            if timestamp < stream.timestamp { return }  // назад у часі
            // Пропуск якщо дрейф > 1 година
            if timestamp-stream.timestamp > uint32(stream.timeScale()*60*60) { return }
        }
    }
    // no%2==1 → RTCP (ігноруємо)
    
    valid = true
    return
}
```

---

## 🔑 5. Stream.handleRtpPacket() — обробка H.264/AAC

### 🔧 H.264: handleH264Payload()

```go
func (self *Stream) handleH264Payload(timestamp uint32, packet []byte) (err error) {
    if len(packet) < 2 { return fmt.Errorf("rtp: h264 packet too short") }
    
    // Перевірка на "багований" Annex B (камери іноді надсилають start codes у RTP)
    if isBuggy, _ := self.handleBuggyAnnexbH264Packet(timestamp, packet); isBuggy {
        return
    }
    
    naluType := packet[0] & 0x1f
    
    switch {
    case naluType >= 1 && naluType <= 5:  // Single NALU
        if naluType == 5 { self.pkt.IsKeyFrame = true }  // IDR
        self.gotpkt = true
        // Конвертація raw NALU → AVCC (4-byte length prefix)
        b := make([]byte, 4+len(packet))
        pio.PutU32BE(b[0:4], uint32(len(packet)))
        copy(b[4:], packet)
        self.pkt.Data = b
        self.timestamp = timestamp
        
    case naluType == 7:  // SPS
        if len(self.sps) == 0 {
            self.sps = packet
            self.makeCodecData()  // ініціалізація кодека
        } else if bytes.Compare(self.sps, packet) != 0 {
            self.spsChanged = true  // сигнал про зміну
            self.sps = packet
        }
        
    case naluType == 8:  // PPS
        // Аналогічно до SPS...
        
    case naluType == 28:  // FU-A (фрагментація)
        fuHeader := packet[1]
        isStart := fuHeader&0x80 != 0
        isEnd := fuHeader&0x40 != 0
        
        if isStart {
            self.fuStarted = true
            // Відновлення оригінального заголовку NALU
            self.fuBuffer = []byte{packet[0]&0xe0 | fuHeader&0x1f}
        }
        if self.fuStarted {
            self.fuBuffer = append(self.fuBuffer, packet[2:]...)
            if isEnd {
                self.fuStarted = false
                // Рекурсивна обробка зібраного NALU
                if err = self.handleH264Payload(timestamp, self.fuBuffer); err != nil { return }
            }
        }
        
    case naluType == 24:  // STAP-A (агрегація)
        packet = packet[1:]  // пропуск заголовку
        for len(packet) >= 2 {
            size := int(packet[0])<<8 | int(packet[1])
            if size+2 > len(packet) { break }
            if err = self.handleH264Payload(timestamp, packet[2:size+2]); err != nil { return }
            packet = packet[size+2:]
        }
    }
    return
}
```

### 🔧 AAC: проста обробка (з "хаком")

```go
case av.AAC:
    if len(payload) < 4 { return fmt.Errorf("rtp: aac packet too short") }
    payload = payload[4:]  // ⚠️ "TODO: remove this hack" — викидаємо 4 байти (AU-headers?)
    self.gotpkt = true
    self.pkt.Data = payload
    self.timestamp = timestamp
```

> ⚠️ **Увага**: Цей "хак" може зламати парсинг, якщо сервер надсилає AU-headers. Краще реалізувати повний парсинг згідно з [RFC 3640](https://datatracker.ietf.org/doc/html/rfc3640).

---

## 🔑 6. Обробка змін кодеків: HandleCodecDataChange()

### Проблема:
Деякі камери можуть змінювати `SPS/PPS` "на льоту" (напр. при зміні роздільної здатності). Це вимагає перезавантаження кодека.

### Рішення:

```go
// Stream.isCodecDataChange() — детекція змін
func (self *Stream) isCodecDataChange() bool {
    return self.spsChanged && self.ppsChanged  // обидва змінилися
}

// Client.HandleCodecDataChange() — створення нового клієнта з оновленими кодеками
func (self *Client) HandleCodecDataChange() (_newcli *Client, err error) {
    newcli := &Client{}
    *newcli = *self  // shallow copy
    
    newcli.streams = []*Stream{}
    for _, stream := range self.streams {
        newstream := &Stream{}
        *newstream = *stream
        newstream.client = newcli
        
        if newstream.isCodecDataChange() {
            if err = newstream.makeCodecData(); err != nil { return }
            newstream.clearCodecDataChange()
        }
        newcli.streams = append(newcli.streams, newstream)
    }
    _newcli = newcli
    return
}
```

### ✅ Ваш use-case: обробка змін у циклі читання

```go
// ReadPacketWithCodecChange — обробка ErrCodecDataChange
func (c *rtsp.Client) ReadPacketWithCodecChange() (pkt av.Packet, err error) {
    for {
        pkt, err = c.ReadPacket()
        if err == rtsp.ErrCodecDataChange {
            // Перестворення клієнта з оновленими кодеками
            if c, err = c.HandleCodecDataChange(); err != nil {
                return pkt, fmt.Errorf("codec change failed: %w", err)
            }
            continue  // повторна спроба
        }
        return pkt, err
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"rtsp: missing h264 sps or pps"** | `makeCodecData()` не знаходить параметри | Перевірте чи камера надсилає `sprop-parameter-sets` у SDP; якщо ні — очікуйте у потоці |
| **"rtp: time invalid"** | Timestamp дрейфує або переповнюється | Перевірте чи `firsttimestamp` ініціалізовано; додайте обробку переповнення 32-бітного timestamp |
| **Аудіо розсинхронізоване** | `timestamp` не конвертується коректно | Переконайтеся, що `timeScale()` повертає правильну частоту (90000 для відео, 8000/16000 для аудіо) |
| **FU-A фрагменти не збираються** | `fuStarted` не скидається при помилці | Додайте `defer` або перевірку `isEnd` у всіх гілках обробки |
| **"TODO: remove this hack" для AAC** | Втрачаються перші 4 байти аудіо | Реалізуйте повний парсинг AU-headers згідно з RFC 3640 |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування буферів для RTP пакетів:

```go
var rtpBufferPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 1500)  // типовий MTU
        return &buf
    },
}

func getRTPBuffer() *[]byte { return rtpBufferPool.Get().(*[]byte) }
func putRTPBuffer(b *[]byte) { rtpBufferPool.Put(b) }

// У handleBlock():
buf := getRTPBuffer()
defer putRTPBuffer(buf)
// ... копіювання даних у *buf ...
```

### 2. Пакетне читання для зменшення системних викликів:

```go
// ReadPacketsBatch — читання кількох пакетів за один виклик
func (c *Client) ReadPacketsBatch(count int) ([]av.Packet, error) {
    packets := make([]av.Packet, 0, count)
    for i := 0; i < count; i++ {
        pkt, err := c.ReadPacket()
        if err == io.EOF { break }
        if err != nil { return packets, err }
        packets = append(packets, pkt)
    }
    return packets, nil
}
```

### 3. Моніторинг продуктивності демуксингу:

```go
type RTSPMetrics struct {
    PacketsRead    prometheus.CounterVec
    ReadLatency    prometheus.HistogramVec
    CodecChanges   prometheus.CounterVec
    RTPBlocksDropped prometheus.CounterVec
}

func (m *RTSPMetrics) RecordPacket(codec av.CodecType, duration time.Duration, channelID string) {
    m.PacketsRead.WithLabelValues(codec.String(), channelID).Inc()
    m.ReadLatency.WithLabelValues(channelID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції rtsp.Client

```go
// ✅ 1. Підключення з таймаутами
client, err := rtsp.DialTimeout("rtsp://camera/stream", 5*time.Second)
if err != nil { /* handle */ }

// ✅ 2. Отримання метаданих перед читанням
streams, err := client.Streams()
if err != nil { /* handle */ }

// ✅ 3. Обробка змін кодеків
for {
    pkt, err := client.ReadPacket()
    if err == rtsp.ErrCodecDataChange {
        client, err = client.HandleCodecDataChange()
        if err != nil { /* handle */ }
        continue
    }
    if err == io.EOF { break }
    // обробка pkt...
}

// ✅ 4. Закриття ресурсів
defer client.Close()

// ✅ 5. Keep-alive для довгих сесій
client.RtpKeepAliveTimeout = 30 * time.Second

// ✅ 6. Метрики для моніторингу
metrics.RecordPacket(pkt.Idx, time.Since(start), channelID)
```

---

## 🔗 Корисні посилання

- 💻 [vdk rtsp Package](https://pkg.go.dev/github.com/deepch/vdk/format/rtsp) — GoDoc documentation
- 📄 [RTSP Specification (RFC 2326)](https://datatracker.ietf.org/doc/html/rfc2326)
- 📄 [RTP Payload Format for H.264 (RFC 6184)](https://datatracker.ietf.org/doc/html/rfc6184)
- 📄 [RTP Payload Format for AAC (RFC 3640)](https://datatracker.ietf.org/doc/html/rfc3640)
- 🧪 [Go net/url Package](https://pkg.go.dev/net/url) — парсинг та нормалізація URL

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди обробляйте `ErrCodecDataChange`** — ігнорування призведе до некоректного декодування після зміни `SPS/PPS`.
> 2. **Використовуйте `DialTimeout`** — уникнення зависань при підключенні до недоступних камер.
> 3. **Налаштуйте `RtpKeepAliveTimeout`** — деякі сервери закривають сесію після 60с бездіяльності.
> 4. **Валідуйте `timestamp`** — додайте обробку переповнення 32-бітного RTP timestamp.
> 5. **Тестуйте з різними камерами** — реалізація RTSP може відрізнятися (LIVE555, Hikvision, Dahua, Axis).

Потрібен приклад інтеграції `rtsp.Client` з вашим `pubsub.Queue` для розподілу прочитаних пакетів між підписниками (HLS muxer, WebSocket, архів)? Готовий допомогти! 🚀