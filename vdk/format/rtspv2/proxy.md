# 📦 Глибокий розбір: `rtspv2.Proxy` — RTSP проксі-сервер

Цей файл — **реалізація простого RTSP проксі-сервера**, що приймає підключення від клієнтів, імітує стандартний RTSP handshake (OPTIONS/DESCRIBE/SETUP/PLAY), та надає механізми для перенаправлення або обробки медіа-потоків.

---

## 🔧 Критичні проблеми у вихідному коді

### ❌ 1. `ProxyConn.Close()` не закриває з'єднання

```go
func (self *ProxyConn) Close() (err error) {
    return nil  // ← НІЧОГО НЕ РОБИТЬ!
}
```

**Наслідки**: витоки файлових дескрипторів, пам'яті, завислі з'єднання.

**✅ Виправлення**:
```go
func (self *ProxyConn) Close() error {
    if self.netconn != nil {
        return self.netconn.Close()
    }
    return nil
}
```

---

### ❌ 2. `defer conn.Close()` закоментовано у `handleConn`

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

### ❌ 3. Парсинг команд через `strings.Split` ненадійний

```go
allStringsSlice := strings.Split(string(self.readbuf[:n]), "\r\n")
fistStringsSlice := strings.Split(allStringsSlice[0], " ")
```

**Проблеми**:
- Не обробляє багаторядкові заголовки
- Чутливий до форматування (зайві пробіли, відсутність `\r\n`)
- `stringInBetween` може повернути порожній рядок → паніка

**✅ Виправлення**: використати `bufio.Reader` для надійного парсингу:

```go
func (self *ProxyConn) prepare() error {
    reader := bufio.NewReader(self.netconn)
    
    // Читання першого рядка: "METHOD URL RTSP/1.0"
    line, err := reader.ReadString('\n')
    if err != nil { return err }
    
    parts := strings.Fields(strings.TrimSpace(line))
    if len(parts) < 2 {
        return errors.New("invalid request line")
    }
    
    method := parts[0]
    uri := parts[1]
    
    // Читання заголовків до порожнього рядка
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
    
    // Обробка методу...
}
```

---

### ❌ 4. `cseq` інкрементується перед читанням

```go
self.cseq++  // ← інкремент ДО читання запиту!
cseq := strings.TrimSpace(stringInBetween(...))
```

**Проблема**: `cseq` у відповіді не співпадає з запитом клієнта → клієнт може відкинути відповідь.

**✅ Виправлення**: спочатку прочитати `CSeq` з запиту, потім використати його у відповіді:

```go
// Прочитати заголовки
headers := parseHeaders(reader)
cseq := headers["CSeq"]

// У відповіді:
fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: %s\r\n\r\n", cseq)
```

---

### ❌ 5. `in` лічильник interleaved каналів не скидається

```go
self.in = self.in + 2  // накопичується між клієнтами!
```

**Проблема**: після 10 клієнтів `in = 20`, що перевищує допустимий діапазон (0-255 для channel ID).

**✅ Виправлення**: ініціалізувати `in = 0` у `NewProxyConn`:

```go
func NewProxyConn(netconn net.Conn) *ProxyConn {
    conn := &ProxyConn{
        netconn:  netconn,
        writebuf: make([]byte, 4096),
        readbuf:  make([]byte, 4096),
        session:  uuid.New().String(),
        in:       0,  // ← скидання для кожного нового з'єднання
    }
    return conn
}
```

---

## 🗺️ Архітектурна схема Proxy

```
┌────────────────────────────────────────┐
│ 📦 rtspv2.Proxy — RTSP Proxy Server   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Proxy — сервер, слухає порт 554     │
│  • ProxyConn — обгортка з'єднання      │
│  • prepare() — парсинг RTSP команд     │
│  • HandleConn/Options/Play — callback'и│
│                                         │
│  🔄 Потік підключення:                  │
│  Client → OPTIONS → DESCRIBE → SETUP → PLAY│
│                                         │
│  📡 Підтримка:                          │
│  • Тільки TCP interleaved (RTP/AVP/TCP)│
│  • Відхиляє UDP (RTP/AVP/UDP)          │
│  • Генерація сесій через uuid          │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. ProxyConn — структура з'єднання

### Поля та їх призначення:

```go
type ProxyConn struct {
    URL      *url.URL        // розпаршений URL запиту
    netconn  net.Conn        // TCP з'єднання з клієнтом
    readbuf  []byte          // буфер для читання (4096 байт)
    writebuf []byte          // буфер для запису (4096 байт)
    sdp      []byte          // SDP для відповіді на DESCRIBE
    playing  bool            // чи пройшов PLAY
    options  bool            // чи пройшов OPTIONS
    cseq     int             // лічильник CSeq (⚠️ проблема!)
    session  string          // унікальний session ID (uuid)
    protocol int             // не використовується
    in       int             // наступний interleaved channel ID
}
```

### ✅ Ваш use-case: безпечне створення з'єднання

```go
// NewProxyConnSafe — виправлена версія
func NewProxyConnSafe(netconn net.Conn) *ProxyConn {
    return &ProxyConn{
        netconn:  netconn,
        readbuf:  make([]byte, 8192),   // збільшений буфер для безпеки
        writebuf: make([]byte, 8192),
        session:  uuid.New().String(),
        in:       0,                    // ✅ скидання лічильника
        cseq:     0,                    // ✅ явна ініціалізація
    }
}

// Close — гарантоване закриття
func (self *ProxyConn) Close() error {
    if self.netconn != nil {
        // Встановлення короткого дедлайну для швидкого закриття
        self.netconn.SetDeadline(time.Now().Add(time.Second))
        return self.netconn.Close()
    }
    return nil
}
```

---

## 🔑 2. prepare() — парсинг RTSP команд (виправлена версія)

### 🔧 Надійний парсинг:

```go
func (self *ProxyConn) prepare() error {
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
        if line == "" { break }  // кінець заголовків
        
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
        return self.handleDESCRIBE(uri, cseq)
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

### 🔧 Обробка SETUP з валідацією:

```go
func (self *ProxyConn) handleSETUP(uri, cseq string, headers map[string]string) error {
    transport := headers["Transport"]
    
    // ✅ Відхилення UDP — тільки TCP interleaved
    if strings.Contains(transport, "RTP/AVP/UDP") {
        response := fmt.Sprintf(
            "RTSP/1.0 461 Unsupported transport\r\n" +
            "CSeq: %s\r\n" +
            "Session: %s\r\n\r\n",
            cseq, self.session,
        )
        _, err := self.netconn.Write([]byte(response))
        return err
    }
    
    // ✅ Генерація interleaved каналів
    channel := self.in
    self.in += 2  // наступна пара: data, RTCP
    
    response := fmt.Sprintf(
        "RTSP/1.0 200 OK\r\n" +
        "CSeq: %s\r\n" +
        "Session: %s\r\n" +
        "Transport: RTP/AVP/TCP;unicast;interleaved=%d-%d\r\n\r\n",
        cseq, self.session, channel, channel+1,
    )
    
    _, err := self.netconn.Write([]byte(response))
    return err
}
```

### ✅ Ваш use-case: логування запитів

```go
// LogRequest — допоміжна функція для дебагу
func (self *ProxyConn) LogRequest(method, uri, cseq string) {
    if Debug {
        log.Printf("[%s] %s %s (CSeq: %s)", 
            self.session[:8], method, uri, cseq)
    }
}

// Використання у handleOPTIONS:
func (self *ProxyConn) handleOPTIONS(uri, cseq string) error {
    self.LogRequest("OPTIONS", uri, cseq)
    // ... решта логіки ...
}
```

---

## 🔑 3. Proxy.ListenAndServe() — запуск сервера

### 🔧 Виправлена версія з graceful shutdown:

```go
func (self *Proxy) ListenAndServe() error {
    addr := self.Addr
    if addr == "" {
        addr = ":554"
    }
    
    tcpaddr, err := net.ResolveTCPAddr("tcp", addr)
    if err != nil {
        return fmt.Errorf("resolve address: %w", err)
    }
    
    listener, err := net.ListenTCP("tcp", tcpaddr)
    if err != nil {
        return fmt.Errorf("listen: %w", err)
    }
    
    if Debug {
        log.Printf("rtsp: server: listening on %s", addr)
    }
    
    // Graceful shutdown через канал
    stop := make(chan struct{})
    defer close(stop)
    
    for {
        select {
        case <-stop:
            listener.Close()
            return nil
            
        default:
            // Приймання з'єднань з таймаутом
            listener.SetDeadline(time.Now().Add(time.Second))
            netconn, err := listener.Accept()
            if err != nil {
                if ne, ok := err.(net.Error); ok && ne.Timeout() {
                    continue  // таймаут — продовжуємо слухати
                }
                return fmt.Errorf("accept: %w", err)
            }
            
            if Debug {
                log.Printf("rtsp: server: accepted from %s", netconn.RemoteAddr())
            }
            
            conn := NewProxyConnSafe(netconn)
            go func() {
                defer conn.Close()  // ✅ гарантоване закриття
                err := self.handleConn(conn)
                if Debug && err != nil {
                    log.Printf("rtsp: server: client closed err: %v", err)
                }
            }()
        }
    }
}
```

### ✅ Ваш use-case: зупинка сервера

```go
// ProxyWithShutdown — розширення з підтримкою зупинки
type ProxyWithShutdown struct {
    *Proxy
    stop chan struct{}
}

func NewProxyWithShutdown(addr string) *ProxyWithShutdown {
    return &ProxyWithShutdown{
        Proxy: &Proxy{Addr: addr},
        stop:  make(chan struct{}),
    }
}

func (p *ProxyWithShutdown) Start() error {
    return p.ListenAndServe()
}

func (p *ProxyWithShutdown) Stop() {
    close(p.stop)
}

// Використання:
proxy := NewProxyWithShutdown(":554")
go proxy.Start()

// При завершенні програми:
defer proxy.Stop()
```

---

## 🔑 4. Callback'и: HandleConn/Options/Play

### Призначення:
Дозволяють вбудовувати кастомну логіку обробки на різних етапах:

```go
type Proxy struct {
    HandleConn    func(*ProxyConn)  // після підключення
    HandleOptions func(*ProxyConn)  // після OPTIONS
    HandlePlay    func(*ProxyConn)  // після PLAY — початок стрімінгу
}
```

### ✅ Ваш use-case: перенаправлення потоку у внутрішній обробник

```go
// CreateProxyWithBackend — проксі з перенаправленням на backend RTSP
func CreateProxyWithBackend(proxyAddr, backendURL string) *rtspv2.Proxy {
    return &rtspv2.Proxy{
        Addr: proxyAddr,
        HandlePlay: func(conn *rtspv2.ProxyConn) {
            // 1. Підключення до backend камери
            backendClient, err := rtspv2.Dial(rtspv2.RTSPClientOptions{
                URL: backendURL,
                // ... налаштування ...
            })
            if err != nil {
                log.Printf("backend connect failed: %v", err)
                conn.Close()
                return
            }
            
            // 2. Запуск forwarding у зворотному напрямку
            go func() {
                defer backendClient.Close()
                for pkt := range backendClient.OutgoingPacketQueue {
                    // Конвертація av.Packet → RTP → interleaved format
                    rtpPkt := convertToRTP(pkt)
                    interleaved := []byte{0x24, byte(conn.in), 0, 0} // placeholder
                    binary.BigEndian.PutUint16(interleaved[2:], uint16(len(rtpPkt)))
                    interleaved = append(interleaved, rtpPkt...)
                    
                    if err := conn.WritePacket(&interleaved); err != nil {
                        log.Printf("forward failed: %v", err)
                        break
                    }
                }
            }()
        },
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **З'єднання не закривається** | `netstat` показує `CLOSE_WAIT` | Розкоментуйте `defer conn.Close()`; виправте `ProxyConn.Close()` |
| **Клієнт відкидає відповідь** | `CSeq mismatch` у логах | Не інкрементуйте `cseq` перед читанням; використовуйте `CSeq` з запиту |
| **Interleaved channel collision** | Помилки парсингу на клієнті | Скидайте `in = 0` у `NewProxyConn` для кожного з'єднання |
| **Паніка при парсингу** | `index out of range` | Використовуйте `bufio.Reader` замість `strings.Split`; валідуйте довжину |
| **UDP клієнти не відхиляються** | Спроби UDP підключення | Додайте перевірку `strings.Contains(transport, "UDP")` у SETUP |

---

## ⚡ Оптимізації для production

### 1. Пул буферів для зменшення аллокацій:

```go
var proxyBufferPool = sync.Pool{
    New: func() interface{} {
        return new([8192]byte)
    },
}

func getBuffer() *[8192]byte { return proxyBufferPool.Get().(*[8192]byte) }
func putBuffer(b *[8192]byte) { proxyBufferPool.Put(b) }

// Використання у ProxyConn:
func NewProxyConn(netconn net.Conn) *ProxyConn {
    return &ProxyConn{
        netconn: netconn,
        // readbuf/writebuf більше не потрібні — використовуємо пул
        session: uuid.New().String(),
        in:      0,
    }
}

func (self *ProxyConn) Read() ([]byte, error) {
    buf := getBuffer()
    defer putBuffer(buf)
    
    n, err := self.netconn.Read(buf[:])
    if err != nil {
        return nil, err
    }
    // Копіювання результату, бо буфер повертається у пул
    result := make([]byte, n)
    copy(result, buf[:n])
    return result, nil
}
```

### 2. Моніторинг підключень:

```go
type ProxyMetrics struct {
    ConnectionsActive prometheus.Gauge
    ConnectionsTotal  prometheus.Counter
    RequestsByMethod  *prometheus.CounterVec
    ErrorsTotal       prometheus.Counter
}

func (m *ProxyMetrics) RecordConnection(opened bool) {
    if opened {
        m.ConnectionsActive.Inc()
        m.ConnectionsTotal.Inc()
    } else {
        m.ConnectionsActive.Dec()
    }
}

func (m *ProxyMetrics) RecordRequest(method string) {
    m.RequestsByMethod.WithLabelValues(method).Inc()
}
```

### 3. Rate limiting для захисту від DDoS:

```go
// RateLimitedProxy — проксі з обмеженням підключень
type RateLimitedProxy struct {
    *Proxy
    limiter *rate.Limiter
}

func NewRateLimitedProxy(addr string, r rate.Limit, burst int) *RateLimitedProxy {
    return &RateLimitedProxy{
        Proxy:   &Proxy{Addr: addr},
        limiter: rate.NewLimiter(r, burst),
    }
}

func (p *RateLimitedProxy) ListenAndServe() error {
    // ... як у ListenAndServe, але з перевіркою:
    if !p.limiter.Allow() {
        netconn.Close()  // відхилення надлишкових підключень
        continue
    }
    // ... обробка ...
}
```

---

## 📋 Чек-лист виправлень

```go
// ✅ 1. Виправити ProxyConn.Close()
func (self *ProxyConn) Close() error {
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

// ✅ 3. Скидати self.in = 0 у NewProxyConn
conn := &ProxyConn{
    // ...
    in: 0,
}

// ✅ 4. Не інкрементувати cseq перед читанням
// Видалити: self.cseq++
// Використовувати: cseq := headers["CSeq"]

// ✅ 5. Використовувати bufio.Reader для парсингу
reader := bufio.NewReader(self.netconn)
line, _ := reader.ReadString('\n')

// ✅ 6. Валідувати Transport у SETUP
if strings.Contains(transport, "UDP") {
    // відхилити
}

// ✅ 7. Метрики для моніторингу
metrics.RecordConnection(true)
metrics.RecordRequest(method)
```

---

## 🔗 Корисні посилання

- 📄 [RTSP Specification (RFC 2326)](https://datatracker.ietf.org/doc/html/rfc2326)
- 📄 [RTP over TCP Interleaved (RFC 4571)](https://datatracker.ietf.org/doc/html/rfc4571)
- 💻 [Go net Package](https://pkg.go.dev/net)
- 🧪 [Go bufio.Reader](https://pkg.go.dev/bufio#Reader)

---

> 💡 **Ключові рекомендації**:
> 1. **Завжди закривайте з'єднання** — навіть при помилках; використовуйте `defer conn.Close()`.
> 2. **Використовуйте `CSeq` з запиту** — не інкрементуйте його заздалегідь.
> 3. **Скидайте `in = 0` для кожного з'єднання** — уникнення конфліктів interleaved каналів.
> 4. **Валідуйте Transport** — відхиляйте UDP, якщо підтримуєте тільки TCP.
> 5. **Моніторьте активні підключення** — для виявлення витоків та атак.

Потрібен приклад інтеграції `RateLimitedProxy` з вашим `pubsub.Queue` для розподілу медіа-потоку між підписниками? Готовий допомогти! 🚀