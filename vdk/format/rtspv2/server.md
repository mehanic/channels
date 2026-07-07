# 📦 Глибокий розбір: `rtspv2.Server` — RTSP сервер (скелетна реалізація)

Цей файл — **незавершена реалізація RTSP сервера** для бібліотеки `vdk`. Він містить базову структуру для прийому підключень, константи для MPEG Program Stream (PS), та хуки для обробки RTSP команд, але більшість функціоналу є заглушками (stubs).

---

## ⚠️ Критичні проблеми у вихідному коді

### ❌ 1. `Conn.Close()` не закриває з'єднання

```go
func (self *Conn) Close() (err error) {
    return nil  // ← НІЧОГО НЕ РОБИТЬ!
}
```

**Наслідки**: витоки файлових дескрипторів, пам'яті, завислі підключення.

**✅ Виправлення**:
```go
func (self *Conn) Close() error {
    if self.netconn != nil {
        return self.netconn.Close()
    }
    return nil
}
```

---

### ❌ 2. `defer conn.Close()` закоментовано

```go
go func() {
    err := self.handleConn(conn)
    //defer conn.Close()  ← ЗАКОМЕНТОВАНО!
}()
```

**Наслідки**: навіть при помилці з'єднання не закривається.

**✅ Виправлення**:
```go
go func() {
    defer conn.Close()  // гарантоване закриття
    err := self.handleConn(conn)
    if Debug {
        fmt.Println("rtsp: server: client closed err:", err)
    }
}()
```

---

### ❌ 3. Усі критичні методи — порожні заглушки

| Метод | Призначення | Стан |
|-------|-------------|------|
| `WritePacket()` | Відправка медіа-пакету клієнту | ❌ Пусто |
| `WriteHeader()` | Відправка кодек-метаданих (SDP) | ❌ Пусто |
| `prepare()` | Парсинг вхідних RTSP команд | ❌ Пусто |
| `handleConn()` | Основний цикл обробки з'єднання | ❌ Пусто |

**Наслідки**: сервер приймає підключення, але не може:
- Відповісти на RTSP команди (OPTIONS/DESCRIBE/SETUP/PLAY)
- Надіслати медіа-дані
- Генерувати SDP опис

---

### ❌ 4. MPEG-PS константи без реалізації

```go
const (
    StartCodePS        = 0x000001ba  // Pack header
    StartCodeSYS       = 0x000001bb  // System header
    StartCodeMAP       = 0x000001bc  // Program map
    StartCodeVideo     = 0x000001e0  // Video stream
    StartCodeAudio     = 0x000001c0  // Audio stream
    MEPGProgramEndCode = 0x000001b9  // End of program
)
```

**Проблема**: константи визначені, але немає коду для:
- Створення PS пакетів
- Мультиплексування аудіо/відео
- Розрахунку CRC32 для `encPSPacket`

---

## 🗺️ Архітектурна схема (поточний стан)

```
┌────────────────────────────────────────┐
│ 📦 rtspv2.Server — RTSP Server (Stub) │
├────────────────────────────────────────┤
│                                         │
│  🔹 Реалізовано:                        │
│  • ListenAndServe() — accept loop      │
│  • NewConn() — створення з'єднання     │
│  • Константи MPEG-PS                   │
│  • Callback хуки (Handle*)             │
│                                         │
│  🔹 НЕ реалізовано:                     │
│  • Парсинг RTSP команд                 │
│  • Генерація SDP                       │
│  • RTP/PS muxing                       │
│  • Відправка медіа                     │
│  • Закриття з'єднань                   │
│                                         │
│  🔄 Очікуваний потік:                   │
│  Client → OPTIONS → DESCRIBE → SETUP  │
│         → PLAY → RTP/PS stream → TEARDOWN│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔧 Як завершити реалізацію: покроковий план

### Крок 1: Виправити базові методи `Conn`

```go
// Close — гарантоване закриття з'єднання
func (self *Conn) Close() error {
    if self.netconn != nil {
        // Короткий дедлайн для швидкого закриття
        self.netconn.SetDeadline(time.Now().Add(time.Second))
        return self.netconn.Close()
    }
    return nil
}

// WritePacket — відправка av.Packet через RTP/PS
func (self *Conn) WritePacket(pkt *av.Packet) error {
    if !self.playing {
        return fmt.Errorf("not in PLAY state")
    }
    
    // TODO: Конвертація av.Packet → RTP або MPEG-PS
    // Залежить від self.protocol (UDP/TCP/PS)
    
    switch self.protocol {
    case TCPTransferPassive:
        // RTP over TCP interleaved: [0x24][channel][len:2][payload]
        return self.writeInterleavedRTP(pkt)
    case UDPTransfer:
        // RTP over UDP: відправка на клієнтський порт
        return self.writeUDPRTP(pkt)
    case LocalCache:
        // Запис у локальний файл/буфер
        return self.writeToCache(pkt)
    default:
        return fmt.Errorf("unsupported protocol: %d", self.protocol)
    }
}

// WriteHeader — генерація та відправка SDP
func (self *Conn) WriteHeader(codecs []av.CodecData) error {
    sdp := generateSDP(self.URL, codecs)
    
    response := fmt.Sprintf(
        "RTSP/1.0 200 OK\r\n" +
        "Content-Type: application/sdp\r\n" +
        "Content-Length: %d\r\n" +
        "CSeq: %d\r\n" +
        "Session: %s\r\n\r\n" +
        "%s",
        len(sdp), self.cseq, self.session, sdp,
    )
    
    _, err := self.netconn.Write([]byte(response))
    return err
}
```

---

### Крок 2: Реалізувати парсинг RTSP команд у `prepare()`

```go
func (self *Conn) prepare() error {
    // 1. Встановлення дедлайну
    err := self.netconn.SetDeadline(time.Now().Add(5 * time.Second))
    if err != nil { return err }
    
    // 2. Читання першого рядка з bufio
    reader := bufio.NewReader(self.netconn)
    line, err := reader.ReadString('\n')
    if err != nil { return err }
    
    parts := strings.Fields(strings.TrimSpace(line))
    if len(parts) < 2 {
        return fmt.Errorf("invalid request: %q", line)
    }
    
    method := parts[0]
    uri := parts[1]
    
    // 3. Читання заголовків
    headers := make(map[string]string)
    for {
        line, err := reader.ReadString('\n')
        if err != nil { return err }
        line = strings.TrimSpace(line)
        if line == "" { break }
        
        kv := strings.SplitN(line, ":", 2)
        if len(kv) == 2 {
            headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
        }
    }
    
    cseq := headers["CSeq"]
    if cseq == "" {
        return errors.New("missing CSeq header")
    }
    
    // 4. Обробка методу
    switch method {
    case OPTIONS:
        return self.handleOPTIONS(uri, cseq)
    case DESCRIBE:
        return self.handleDESCRIBE(uri, cseq, headers)
    case SETUP:
        return self.handleSETUP(uri, cseq, headers)
    case PLAY:
        return self.handlePLAY(uri, cseq)
    case TEARDOWN:
        return self.handleTEARDOWN(cseq)
    default:
        return fmt.Errorf("method not supported: %s", method)
    }
}
```

---

### Крок 3: Реалізувати обробники RTSP методів

```go
// handleOPTIONS — відповідь на OPTIONS
func (self *Conn) handleOPTIONS(uri, cseq string) error {
    response := fmt.Sprintf(
        "RTSP/1.0 200 OK\r\n" +
        "Public: OPTIONS, DESCRIBE, SETUP, PLAY, TEARDOWN\r\n" +
        "CSeq: %s\r\n\r\n",
        cseq,
    )
    _, err := self.netconn.Write([]byte(response))
    return err
}

// handleDESCRIBE — генерація SDP
func (self *Conn) handleDESCRIBE(uri, cseq string, headers map[string]string) error {
    // TODO: Отримати кодеки з джерела (callback або внутрішній стан)
    codecs := []av.CodecData{}  // заглушка
    
    sdp := generateSDP(self.URL, codecs)
    
    response := fmt.Sprintf(
        "RTSP/1.0 200 OK\r\n" +
        "Content-Type: application/sdp\r\n" +
        "Content-Length: %d\r\n" +
        "CSeq: %s\r\n\r\n" +
        "%s",
        len(sdp), cseq, sdp,
    )
    
    _, err := self.netconn.Write([]byte(response))
    return err
}

// handleSETUP — узгодження транспорту
func (self *Conn) handleSETUP(uri, cseq string, headers map[string]string) error {
    transport := headers["Transport"]
    
    // Визначення протоколу
    if strings.Contains(transport, "RTP/AVP/UDP") {
        self.protocol = UDPTransfer
    } else if strings.Contains(transport, "RTP/AVP/TCP") {
        self.protocol = TCPTransferPassive
    } else {
        return fmt.Errorf("unsupported transport: %s", transport)
    }
    
    // Генерація session ID якщо ще немає
    if self.session == "" {
        self.session = uuid.New().String()
    }
    
    // Формування відповіді
    response := fmt.Sprintf(
        "RTSP/1.0 200 OK\r\n" +
        "CSeq: %s\r\n" +
        "Session: %s\r\n" +
        "Transport: %s\r\n\r\n",
        cseq, self.session, transport,
    )
    
    _, err := self.netconn.Write([]byte(response))
    return err
}

// handlePLAY — початок стрімінгу
func (self *Conn) handlePLAY(uri, cseq string) error {
    self.playing = true
    
    response := fmt.Sprintf(
        "RTSP/1.0 200 OK\r\n" +
        "Session: %s;timeout=60\r\n" +
        "RTP-Info: url=%s;seq=0;rtptime=0\r\n" +
        "CSeq: %s\r\n\r\n",
        self.session, uri, cseq,
    )
    
    _, err := self.netconn.Write([]byte(response))
    return err
}

// handleTEARDOWN — завершення сесії
func (self *Conn) handleTEARDOWN(cseq string) error {
    response := fmt.Sprintf(
        "RTSP/1.0 200 OK\r\n" +
        "CSeq: %s\r\n" +
        "Session: %s\r\n\r\n",
        cseq, self.session,
    )
    self.netconn.Write([]byte(response))
    return self.Close()
}
```

---

### Крок 4: Реалізувати `handleConn()` — основний цикл

```go
func (self *Server) handleConn(conn *Conn) error {
    // Callback для кастомної логіки
    if self.HandleConn != nil {
        self.HandleConn(conn)
    }
    
    // Основний цикл обробки команд
    for {
        err := conn.prepare()
        if err != nil {
            return err  // помилка читання або TEARDOWN
        }
        
        // Callback'и після кожної команди
        if conn.options && self.HandleOptions != nil {
            self.HandleOptions(conn)
        }
        if conn.playing && self.HandlePlay != nil {
            self.HandlePlay(conn)
        }
    }
}
```

---

### Крок 5: Реалізувати MPEG-PS muxing (опціонально)

Якщо ви хочете використовувати MPEG Program Stream замість RTP:

```go
// writeMPEGPS — створення PS пакету з авідео/аудіо даними
func (self *Conn) writeMPEGPS(pkt *av.Packet, streamID byte) error {
    var buf bytes.Buffer
    
    // Pack header (start code + header)
    buf.Write([]byte{0x00, 0x00, 0x01, 0xba})
    buf.WriteByte(0x44)  // marker bits + SCR base
    // ... SCR (System Clock Reference) ...
    
    // System header (опціонально)
    buf.Write([]byte{0x00, 0x00, 0x01, 0xbb})
    buf.Write([]byte{0x00, 0x06})  // header length
    // ... system header fields ...
    
    // Packet header для конкретного stream
    buf.Write([]byte{0x00, 0x00, 0x01, streamID})  // stream ID
    buf.Write([]byte{0x80, 0x80})  // flags + length
    // ... PTS/DTS якщо потрібно ...
    
    // Payload: конвертація av.Packet.Data у відповідний формат
    // • H.264: Annex B або AVCC → byte stream
    // • AAC: ADTS або raw → byte stream
    buf.Write(pkt.Data)
    
    // Запис у з'єднання
    _, err := self.netconn.Write(buf.Bytes())
    return err
}
```

---

## ✅ Ваш use-case: мінімальний робочий сервер

```go
// MinimalRTSPServer — простий сервер для тестування
type MinimalRTSPServer struct {
    *rtspv2.Server
    codecs []av.CodecData
    packets chan *av.Packet
}

func NewMinimalRTSPServer(addr string, codecs []av.CodecData) *MinimalRTSPServer {
    s := &MinimalRTSPServer{
        Server: &rtspv2.Server{Addr: addr},
        codecs: codecs,
        packets: make(chan *av.Packet, 1000),
    }
    
    // Реєстрація callback'ів
    s.HandleDescribe = func(conn *rtspv2.Conn) {
        conn.WriteHeader(s.codecs)
    }
    
    s.HandlePlay = func(conn *rtspv2.Conn) {
        // Запуск відправки пакетів
        go func() {
            for pkt := range s.packets {
                if err := conn.WritePacket(pkt); err != nil {
                    break
                }
            }
        }()
    }
    
    return s
}

// SendPacket — додавання пакету у чергу відправки
func (s *MinimalRTSPServer) SendPacket(pkt *av.Packet) {
    select {
    case s.packets <- pkt:
        // успішно
    default:
        // черга переповнена — пропускаємо
    }
}

// Використання:
server := NewMinimalRTSPServer(":554", []av.CodecData{
    h264parser.CodecData{...},
    aacparser.CodecData{...},
})
go server.ListenAndServe()

// Відправка відео:
server.SendPacket(&av.Packet{
    Idx: 0,
    Data: h264NALU,
    IsKeyFrame: true,
    Time: time.Now(),
})
```

---

## 📋 Чек-лист завершення реалізації

```go
// ✅ 1. Виправити Conn.Close()
func (self *Conn) Close() error {
    if self.netconn != nil {
        return self.netconn.Close()
    }
    return nil
}

// ✅ 2. Розкоментувати defer conn.Close()
go func() {
    defer conn.Close()
    err := self.handleConn(conn)
    // ...
}()

// ✅ 3. Реалізувати prepare() з парсингом команд
// ✅ 4. Реалізувати handleOPTIONS/DESCRIBE/SETUP/PLAY/TEARDOWN
// ✅ 5. Реалізувати WritePacket() для відправки медіа
// ✅ 6. Реалізувати WriteHeader() для генерації SDP
// ✅ 7. Додати валідацію Transport у SETUP
// ✅ 8. Реалізувати generateSDP() для опису потоків
// ✅ 9. Додати метрики для моніторингу підключень
// ✅ 10. Протестувати з різними клієнтами (VLC, FFmpeg, браузер)
```

---

## 🔗 Корисні посилання

- 📄 [RTSP Specification (RFC 2326)](https://datatracker.ietf.org/doc/html/rfc2326)
- 📄 [SDP: Session Description Protocol (RFC 4566)](https://datatracker.ietf.org/doc/html/rfc4566)
- 📄 [MPEG-Systems (ISO/IEC 13818-1)](https://www.iso.org/standard/23088.html) — Program Stream формат
- 📄 [RTP Payload Format for H.264 (RFC 6184)](https://datatracker.ietf.org/doc/html/rfc6184)
- 💻 [Go net Package](https://pkg.go.dev/net)

---

> 💡 **Ключові рекомендації**:
> 1. **Почніть з виправлення `Close()` та `defer conn.Close()`** — це запобігає витокам ресурсів.
> 2. **Реалізуйте мінімальний RTSP handshake** (OPTIONS → DESCRIBE → SETUP → PLAY) перед додаванням медіа.
> 3. **Використовуйте `bufio.Reader` для парсингу** — надійніше ніж `strings.Split`.
> 4. **Тестуйте з VLC/FFmpeg** — вони покажуть помилки у SDP або RTSP відповідях.
> 5. **Додайте логування у Debug режимі** — для відладки складних сценаріїв.

Потрібен приклад реалізації `generateSDP()` для опису H.264+AAC потоків? Готовий допомогти! 🚀