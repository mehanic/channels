# 📦 Глибокий розбір: `rtmp` — RTMP клієнт/сервер для vdk

Цей файл — **повноцінна реалізація RTMP протоколу** для бібліотеки `vdk`, що підтримує підключення до серверів (напр. nginx-rtmp, OBS), публікацію та відтворення потоків, автентифікацію, та демуксинг/муксинг аудіо/відео (H.264, AAC) у уніфіковані `av.Packet` об'єкти.

---

## 🗺️ Архітектурна схема rtmp пакету

```
┌────────────────────────────────────────┐
│ 📦 rtmp — RTMP Client/Server Engine   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Conn — RTMP з'єднання (клієнт/сервер)│
│  • Server — RTMP сервер з callback'ами │
│  • chunkStream — обробка чанків        │
│  • handshakeClient/Server — RTMFP handshake│
│                                         │
│  🔄 Потік даних:                        │
│  TCP:1935 → handshake → connect        │
│  → createStream → publish/play         │
│  → AMF0 commands + FLV tags            │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (AVCC/Annex B)         │
│  • Аудіо: AAC (ADTS/raw)               │
│  • Команди: AMF0 encode/decode         │
│  • Chunking: Type 0/1/2/3              │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Conn — основна структура з'єднання

### Поля та їх призначення:

```go
type Conn struct {
    // 🌐 Мережа
    netconn     net.Conn          // базове TCP з'єднання
    bufr        *bufio.Reader     // буферизований reader
    bufw        *bufio.Writer     // буферизований writer
    txrxcount   *txrxcount        // лічильник байт (read/write)
    
    // 🔄 Стан машини
    stage       int               // stageHandshakeDone → ... → stageCodecDataDone
    isserver    bool              // чи це серверне з'єднання
    publishing, playing bool      // режим: публікація чи відтворення
    reading, writing bool         // напрямки потоку
    
    // 📦 Чанки (chunking)
    readMaxChunkSize, writeMaxChunkSize int
    readcsmap       map[uint32]*chunkStream  // активні чанк-стріми для читання
    chunkHeaderBuf, chunkHeaderBufExt []byte // буфери для заголовків
    
    // 🎛️ Команди (AMF0)
    gotcommand      bool
    commandname     string          // "connect", "publish", "play"...
    commandtransid  float64         // transaction ID для кореляції
    commandobj      flvio.AMFMap    // об'єкт команди
    commandparams   []interface{}   // параметри команди
    
    // 🎬 Медіа-дані
    avmsgsid        uint32          // stream ID для аудіо/відео
    msgtypeid       uint8           // тип повідомлення (відео/аудіо/команда)
    timestamp       uint32          // поточний timestamp
    avtag           flvio.Tag       // розпаршений FLV тег
    prober          *flv.Prober     // для авто-детекту кодеків
    
    // 📊 Метадані
    streams         []av.CodecData  // кешовані кодеки
    URL             *url.URL        // розпаршений URL
    OnPlayOrPublish func(string, flvio.AMFMap) error  // callback для валідації
}
```

### 🔧 Стани обробки (stage):

```go
const (
    stageHandshakeDone = iota + 1  // 1: handshake завершено
    stageCommandDone               // 2: connect/createStream/publish/play
    stageCodecDataDone             // 3: кодек-дані отримано, готовий до read/write
)
```

**Потік ініціалізації**:
```
0 → handshake → 1 → connect/createStream → 2 
→ publish/play → probe() → 3 → готовий
```

---

## 🔑 2. RTMP Handshake — C0C1C2/S0S1S2

### 🔧 handshakeClient():

```go
func (self *Conn) handshakeClient() (err error) {
    var random [(1 + 1536*2) * 2]byte
    C0C1C2 := random[:1536*2+1]
    C0 := C0C1C2[:1]      // версія (завжди 3)
    C1 := C0C1C2[1:1536+1]  // клієнтські дані (1536 байт)
    C2 := C0C1C2[1536+1:]   // відповідь серверу
    
    C0[0] = 3  // RTMP version
    
    // Відправка C0C1
    if _, err = self.bufw.Write(C0C1); err != nil { return }
    if err = self.bufw.Flush(); err != nil { return }
    
    // Отримання S0S1S2
    if _, err = io.ReadFull(self.bufr, S0S1S2); err != nil { return }
    
    // Формування C2 (копіюємо S1 або S2 залежно від версії)
    if ver := pio.U32BE(S1[4:8]); ver != 0 {
        C2 = S1  // "складний" handshake
    } else {
        C2 = S1  // "простий" handshake
    }
    
    // Відправка C2
    if _, err = self.bufw.Write(C2); err != nil { return }
    
    self.stage++
    return
}
```

### 🔍 Формат handshake пакетів:

```
C0/S0: 1 байт — версія протоколу (завжди 3)

C1/S1: 1536 байт — дані клієнта/сервера:
  • Байти 0-3: час (unix timestamp)
  • Байти 4-7: версія (напр. 0x0d0e0a0d = 13.14.10.13)
  • Байти 8-1535: випадкові дані + digest (для RTMPE)

C2/S2: 1536 байт — відповідь (копія отриманих даних або оброблений digest)

Digest алгоритм (для RTMPE):
1. Обчислення позиції digest: pos = (sum of 4 bytes) % 728 + base + 4
2. HMAC-SHA256 з ключем (hsClientFullKey/hsServerFullKey)
3. Порівняння з отриманим digest для валідації
```

### ⚠️ Критичний момент: "простий" vs "складний" handshake

```
У вихідному коді:
    if ver := pio.U32BE(S1[4:8]); ver != 0 {
        C2 = S1
    } else {
        C2 = S1  // ← ОБИДВА ВИПАДКИ ОДНАКОВІ!
    }

Це означає, що код не реалізує справжній RTMPE handshake,
а лише імітує його для сумісності. Для production з шифруванням
потрібна повна реалізація hsParse1/hsCreate01/hsCreate2.
```

---

## 🔑 3. RTMP команди: AMF0 encode/decode

### 🔧 writeCommandMsg() — відправка команди:

```go
func (self *Conn) writeCommandMsg(csid, msgsid uint32, args ...interface{}) (err error) {
    return self.writeAMF0Msg(msgtypeidCommandMsgAMF0, csid, msgsid, args...)
}

func (self *Conn) writeAMF0Msg(msgtypeid uint8, csid, msgsid uint32, args ...interface{}) (err error) {
    // 1. Розрахунок загального розміру аргументів
    size := 0
    for _, arg := range args {
        size += flvio.LenAMF0Val(arg)
    }
    
    // 2. Заповнення чанк-заголовку + AMF0 даних
    b := self.tmpwbuf(chunkHeaderLength + size)
    n := self.fillChunkHeader(b, csid, 0, msgtypeid, msgsid, size)
    for _, arg := range args {
        n += flvio.FillAMF0Val(b[n:], arg)  // кодування в AMF0
    }
    
    // 3. Запис у буфер
    _, err = self.bufw.Write(b[:n])
    return
}
```

### 🔧 handleCommandMsgAMF0() — парсинг команди:

```go
func (self *Conn) handleCommandMsgAMF0(b []byte) (n int, err error) {
    // 1. Парсинг name (string), transid (number), obj (object)
    var name, transid, obj interface{}
    var size int
    
    if name, size, err = flvio.ParseAMF0Val(b[n:]); err != nil { return }
    n += size
    if transid, size, err = flvio.ParseAMF0Val(b[n:]); err != nil { return }
    n += size
    if obj, size, err = flvio.ParseAMF0Val(b[n:]); err != nil { return }
    n += size
    
    // 2. Збереження у поля Conn
    self.commandname, _ = name.(string)
    self.commandtransid, _ = transid.(float64)
    self.commandobj, _ = obj.(flvio.AMFMap)
    
    // 3. Парсинг додаткових параметрів
    self.commandparams = []interface{}{}
    for n < len(b) {
        if obj, size, err = flvio.ParseAMF0Val(b[n:]); err != nil { return }
        n += size
        self.commandparams = append(self.commandparams, obj)
    }
    
    self.gotcommand = true
    return
}
```

### 🔍 Формат AMF0:

```
AMF0 — Action Message Format version 0, використовується у RTMP:

Типи даних:
• 0x00: Number (8-byte BE double)
• 0x01: Boolean (1 byte)
• 0x02: String (2-byte length + UTF-8)
• 0x03: Object (key-value pairs, закінчується 0x00 0x00 0x09)
• 0x05: Null
• 0x06: Undefined
• 0x08: Mixed array (object + numeric indices)
• 0x0A: Strict array (length + elements)
• 0x0C: Date (number + timezone offset)
• 0x0D: Long string (4-byte length)

Приклад "connect" команди:
[0x02][0x00 0x07]["connect"]  // string: "connect"
[0x00][3F F0 00 00 00 00 00 00]  // number: 1.0 (transid)
[0x03]  // object start
  [0x00 0x03]["app"][0x02][0x00 0x04]["live"]  // app: "live"
  [0x00 0x06]["tcUrl"][0x02][0x00 0x1A]["rtmp://server/live"]  // tcUrl
  [0x00 0x00][0x09]  // object end
```

### ✅ Ваш use-case: валідація команд публікації

```go
// OnPlayOrPublish callback — перевірка прав доступу
func (s *RTMPServer) OnPlayOrPublish(cmd string, params flvio.AMFMap) error {
    app, _ := params["app"].(string)
    name, _ := params["name"].(string)
    
    // Приклад: дозволено тільки публікація у "live/*"
    if cmd == "publish" && !strings.HasPrefix(app, "live") {
        return fmt.Errorf("publishing to %q not allowed", app)
    }
    
    // Логування для аудиту
    log.Printf("RTMP %s: app=%q, name=%q, params=%v", 
        cmd, app, name, params)
    
    return nil
}

// Реєстрація у сервері:
server := &rtmp.Server{
    Addr: ":1935",
    HandlePublish: func(conn *rtmp.Conn) {
        // Обробка потоку...
    },
}
// При підключенні:
conn.OnPlayOrPublish = server.OnPlayOrPublish
```

---

## 🔑 4. Chunking — фрагментація повідомлень

### 🔍 Чому потрібні чанки?

```
RTMP передає дані фрагментами (чанками) для:
• Економії пропускної здатності (менше заголовків)
• Підтримки мультиплексування кількох потоків
• Контролю затримки (менші чанки = менша латентність)

Формат чанк-заголовку (4 типи):

Тип 0 (11+ байт): повний заголовок
  [7 bits: csid][2 bits: fmt=00][timestamp:3][length:3][type:1][stream id:4]
  [extended timestamp:4] (якщо timestamp >= 0xFFFFFF)

Тип 1 (7+ байт): дельта-заголовок (без stream id)
  [fmt=01][delta timestamp:3][length:3][type:1]

Тип 2 (3+ байт): тільки дельта часу
  [fmt=10][delta timestamp:3]

Тип 3 (0 байт): продовження даних (використовує попередній заголовок)
  [fmt=11] — тільки csid, решта з попереднього чанку
```

### 🔧 readChunk() — парсинг чанків:

```go
func (self *Conn) readChunk() (err error) {
    // 1. Читання першого байта (fmt + csid)
    b := self.readbuf
    if _, err = io.ReadFull(self.bufr, b[:1]); err != nil { return }
    header := b[0]
    
    msghdrtype := header >> 6  // fmt: 0..3
    csid := uint32(header) & 0x3f
    
    // 2. Розширений csid (якщо 0 або 1)
    switch csid {
    case 0:  // csid у наступному байті + 64
        if _, err = io.ReadFull(self.bufr, b[:1]); err != nil { return }
        csid = uint32(b[0]) + 64
    case 1:  // csid у наступних 2 байтах + 64
        if _, err = io.ReadFull(self.bufr, b[:2]); err != nil { return }
        csid = uint32(pio.U16BE(b)) + 64
    }
    
    // 3. Отримання або створення chunkStream
    cs := self.readcsmap[csid]
    if cs == nil {
        cs = &chunkStream{}
        self.readcsmap[csid] = cs
    }
    
    // 4. Парсинг заголовку залежно від типу
    switch msghdrtype {
    case 0:  // повний заголовок
        // Читання 11 байт: timestamp, length, type, stream id
        // + extended timestamp якщо потрібно
        // Ініціалізація cs.Start()
        
    case 1:  // дельта-заголовок
        // Читання 7 байт: delta timestamp, length, type
        // Оновлення cs.timenow += delta
        
    case 2:  // тільки дельта часу
        // Читання 3 байт: delta timestamp
        
    case 3:  // продовження даних
        // Якщо msgdataleft == 0 → нове повідомлення, інакше продовження
    }
    
    // 5. Читання даних чанку
    size := int(cs.msgdataleft)
    if size > self.readMaxChunkSize {
        size = self.readMaxChunkSize
    }
    off := cs.msgdatalen - cs.msgdataleft
    buf := cs.msgdata[off : off+size]
    if _, err = io.ReadFull(self.bufr, buf); err != nil { return }
    cs.msgdataleft -= uint32(size)
    
    // 6. Обробка завершеного повідомлення
    if cs.msgdataleft == 0 {
        if err = self.handleMsg(cs.timenow, cs.msgsid, cs.msgtypeid, cs.msgdata); err != nil {
            return
        }
    }
    
    return
}
```

### ⚠️ Критичний момент: обробка extended timestamp

```
Якщо 24-бітне поле timestamp == 0xFFFFFF, наступні 4 байти містять повний 32-бітний timestamp.

У вихідному коді:
    if timestamp == 0xffffff {
        if _, err = io.ReadFull(self.bufr, b[:4]); err != nil { return }
        timestamp = pio.U32BE(b)  // ← читаємо 4 байти
        cs.hastimeext = true
    }

Але пізніше у fillChunkHeader():
    if uint32(timestamp) <= FlvTimestampMax {  // 0xFFFFFF
        pio.PutU24BE(b[n:], uint32(timestamp))
    } else {
        pio.PutU24BE(b[n:], FlvTimestampMax)  // ← записуємо 0xFFFFFF
        pio.PutU32BE(b[n:], uint32(timestamp))  // ← потім 4 байти
    }

Це коректно, але важливо пам'ятати: для timestamp > 16777215 (24 біти)
завжди використовується extended формат.
```

---

## 🔑 5. Обробка медіа-повідомлень: handleMsg()

### 🔧 Відео/аудіо теги:

```go
case msgtypeidVideoMsg:  // 9
    if len(msgdata) == 0 { return }
    tag := flvio.Tag{Type: flvio.TAG_VIDEO}
    var n int
    if n, err = (&tag).ParseHeader(msgdata); err != nil { return }
    if !(tag.FrameType == flvio.FRAME_INTER || tag.FrameType == flvio.FRAME_KEY) {
        return  // ігноруємо невідомиі типи кадрів
    }
    tag.Data = msgdata[n:]  // дані без заголовку
    self.avtag = tag

case msgtypeidAudioMsg:  // 8
    // Аналогічно для аудіо...
```

### 🔧 parseHeader() у flvio.Tag:

```
Формат FLV відео-тегу (перший байт даних):
  Біти 7-4: FrameType (1=key, 2=inter, тощо)
  Біти 3-0: CodecID (7=H.264, 10=H.265)

Для H.264:
  Другий байт: AVCPacketType (0=seq header, 1=NALU, 2=end of seq)
  Третій байт: CompositionTime (для B-frames)

Функція ParseHeader() витягує ці поля у struct Tag:
  type Tag struct {
      Type uint8  // TAG_VIDEO/TAG_AUDIO
      FrameType, CodecID uint8
      AVCPacketType uint8  // для H.264/H.265
      Data []byte  // корисне навантаження
  }
```

### ✅ Ваш use-case: фільтрація кадрів перед обробкою

```go
// ShouldProcessFrame — вирішення чи обробляти кадр
func ShouldProcessFrame(tag flvio.Tag) bool {
    if tag.Type != flvio.TAG_VIDEO {
        return true  // аудіо завжди обробляємо
    }
    
    // Ігноруємо неключові кадри при низькому бітрейті
    if tag.FrameType != flvio.FRAME_KEY && lowBitrateMode {
        return false
    }
    
    // Ігноруємо SEI/SPS/PPS якщо вже отримані
    if tag.CodecID == flvio.CODEC_H264 && tag.AVCPacketType == flvio.AVC_SEQHDR {
        return !alreadyHaveSPSPPS
    }
    
    return true
}

// Використання у handleMsg():
case msgtypeidVideoMsg:
    // ... парсинг ...
    if !ShouldProcessFrame(tag) {
        return  // пропускаємо кадр
    }
    self.avtag = tag
```

---

## 🔑 6. Server — RTMP сервер з callback'ами

### 🔧 ListenAndServe():

```go
func (self *Server) ListenAndServe() (err error) {
    addr := self.Addr
    if addr == "" { addr = ":1935" }
    
    tcpaddr, _ := net.ResolveTCPAddr("tcp", addr)
    listener, _ := net.ListenTCP("tcp", tcpaddr)
    
    for {
        netconn, err := listener.Accept()
        if err != nil { return err }
        
        conn := NewConn(netconn)
        conn.isserver = true
        go func() {
            err := self.handleConn(conn)
            if Debug { fmt.Println("rtmp: server: client closed err:", err) }
        }()
    }
}
```

### 🔧 handleConn() — обробка підключення:

```go
func (self *Server) handleConn(conn *Conn) (err error) {
    if self.HandleConn != nil {
        self.HandleConn(conn)  // кастомна логіка
    } else {
        // Стандартний потік: handshake → connect → publish/play
        if err = conn.prepare(stageCommandDone, 0); err != nil { return }
        
        if conn.playing {
            if self.HandlePlay != nil { self.HandlePlay(conn) }
        } else if conn.publishing {
            if self.HandlePublish != nil { self.HandlePublish(conn) }
        }
    }
    return
}
```

### ✅ Ваш use-case: кастомна обробка публікації

```go
// RTMPServer — розширений сервер з логуванням та метриками
type RTMPServer struct {
    *rtmp.Server
    metrics *RTMPMetrics
    auth    AuthProvider
}

func (s *RTMPServer) HandlePublish(conn *rtmp.Conn) {
    start := time.Now()
    
    // Автентифікація
    if err := s.auth.AuthorizePublish(conn.URL.Path, conn.commandobj); err != nil {
        log.Printf("publish auth failed: %v", err)
        conn.Close()
        return
    }
    
    // Ініціалізація обробника потоку
    handler := NewStreamHandler(conn.URL.Path)
    
    // Основний цикл читання пакетів
    for {
        pkt, err := conn.ReadPacket()
        if err == io.EOF { break }
        if err != nil {
            log.Printf("read error: %v", err)
            break
        }
        
        // Обробка пакету
        if err := handler.HandlePacket(pkt); err != nil {
            log.Printf("handle error: %v", err)
            break
        }
        
        // Метрики
        s.metrics.RecordPacket(pkt.Idx, len(pkt.Data), time.Since(start))
    }
    
    // Завершення
    handler.Close()
    s.metrics.RecordSessionEnd(conn.URL.Path, time.Since(start))
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"rtmp: first command is not connect"** | Клієнт надсилає не "connect" першим | Перевірте чи клієнт дотримується RTMP специфікації; деякі реалізації пропускають connect |
| **"chunk msgdataleft invalid"** | Невідповідність розмірів чанків | Перевірте чи `readMaxChunkSize` співпадає між клієнтом та сервером; додайте логування чанків |
| **Handshake не проходить** | З'єднання закривається після C0C1 | Переконайтеся, що буфери достатньо великі (1536+ байт); перевірте RTMPE підтримку |
| **AMF0 парсинг падає** | "CommandMsgAMF0 command is not string" | Перевірте чи клієнт надсилає коректний AMF0; додайте валідацію типів перед type assertion |
| **Timestamp переповнення** | Час "стрибає" після ~4.5 годин | Додайте обробку 32-бітного переповнення: `if ts < prev { ts += 1<<32 }` |

---

## ⚡ Оптимізації для high-throughput

### 1. Кешування буферів для чанків:

```go
var chunkBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір чанку: 128-4096 байт
        buf := make([]byte, 4096)
        return &buf
    },
}

func getChunkBuffer() *[]byte { return chunkBufferPool.Get().(*[]byte) }
func putChunkBuffer(b *[]byte) { chunkBufferPool.Put(b) }

// У readChunk():
buf := getChunkBuffer()
defer putChunkBuffer(buf)
// ... використання buf замість self.readbuf ...
```

### 2. Пакетна обробка чанків:

```go
// ReadChunksBatch — читання кількох чанків за один виклик
func (c *Conn) ReadChunksBatch(count int) ([]flvio.Tag, error) {
    tags := make([]flvio.Tag, 0, count)
    for i := 0; i < count; i++ {
        tag, err := c.pollAVTag()
        if err == io.EOF { break }
        if err != nil { return tags, err }
        tags = append(tags, tag)
    }
    return tags, nil
}
```

### 3. Моніторинг продуктивності:

```go
type RTMPMetrics struct {
    ChunksRead    prometheus.CounterVec
    CommandsHandled prometheus.CounterVec
    ChunkLatency  prometheus.HistogramVec
    HandshakeErrors prometheus.CounterVec
}

func (m *RTMPMetrics) RecordChunk(size int, duration time.Duration, connID string) {
    m.ChunksRead.WithLabelValues(connID).Inc()
    m.ChunkLatency.WithLabelValues(connID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції rtmp.Conn

```go
// ✅ 1. Підключення з таймаутом
conn, err := rtmp.DialTimeout("rtmp://server/live/stream", 5*time.Second)
if err != nil { /* handle */ }

// ✅ 2. Отримання метаданих перед читанням
streams, err := conn.Streams()
if err != nil { /* handle */ }

// ✅ 3. Обробка пакетів з перевіркою помилок
for {
    pkt, err := conn.ReadPacket()
    if err == io.EOF { break }
    if err != nil { /* handle */ }
    // обробка pkt...
}

// ✅ 4. Закриття ресурсів
defer conn.Close()

// ✅ 5. Для сервера: реєстрація callback'ів
server := &rtmp.Server{
    Addr: ":1935",
    HandlePublish: handlePublish,
    HandlePlay: handlePlay,
}
go server.ListenAndServe()

// ✅ 6. Метрики для моніторингу
metrics.RecordChunk(len(pkt.Data), time.Since(start), connID)
```

---

## 🔗 Корисні посилання

- 💻 [vdk rtmp Package](https://pkg.go.dev/github.com/deepch/vdk/format/rtmp) — GoDoc documentation
- 📄 [RTMP Specification (Adobe)](https://www.adobe.com/devnet/rtmp.html) — офіційна специфікація
- 📄 [AMF0 Format](https://github.com/mifi/lossless-cut/blob/master/amf0.md) — детальний опис
- 📄 [RTMP Chunking](https://rtmp.veriskope.com/pdf/rtmp_specification_1.0.pdf#page=10) — розділ 5.3
- 🧪 [Go bufio Package](https://pkg.go.dev/bufio) — буферизований I/O для ефективності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте `stage` перед read/write** — ігнорування призведе до "call WriteHeader() before WritePacket()".
> 2. **Налаштуйте `readMaxChunkSize`** — занадто малі чанки збільшують overhead, занадто великі — затримку.
> 3. **Обробляйте 32-бітне переповнення timestamp** — додайте логіку `if ts < prev { ts += 1<<32 }`.
> 4. **Валідуйте AMF0 типи перед type assertion** — уникнення панік при некоректних командах.
> 5. **Тестуйте з різними клієнтами** — OBS, FFmpeg, власні клієнти можуть надсилати різні варіанти handshake.

Потрібен приклад інтеграції `rtmp.Server` з вашим `pubsub.Queue` для розподілу опублікованих пакетів між підписниками (HLS muxer, WebSocket, архів)? Готовий допомогти! 🚀