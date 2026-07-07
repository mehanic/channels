# 📦 Глибокий розбір: `rtsp.connWithTimeout` — Обгортка для таймаутів мережевих операцій

Цей файл — **мінімалістична обгортка над `net.Conn`**, що додає автоматичне встановлення дедлайнів для операцій `Read`/`Write`. Це критично важливо для RTSP клієнтів, щоб уникнути зависань при роботі з ненадійними мережевими джерелами (камери, нестабільний інтернет).

---

## 🗺️ Архітектурна схема connWithTimeout

```
┌────────────────────────────────────────┐
│ 📦 connWithTimeout — Timeout Wrapper  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Timeout time.Duration — ліміт часу  │
│  • net.Conn — вбудоване з'єднання      │
│  • Read/Write — авто-дедлайни          │
│                                         │
│  🔄 Потік даних:                        │
│  Read(p) → SetReadDeadline() → Conn.Read│
│  Write(p) → SetWriteDeadline() → Conn.Write│
│                                         │
│  📡 Використання:                       │
│  • RTSP команд (OPTIONS/DESCRIBE...)   │
│  • RTP пакети через TCP interleaved    │
│  • Захист від зависань у readPacket()  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Структура та вбудовування

```go
type connWithTimeout struct {
    Timeout time.Duration  // ліміт на одну операцію
    net.Conn               // вбудоване з'єднання (embedding)
}
```

### 🔍 Чому вбудовування (embedding)?

```
Вбудовування `net.Conn` дозволяє:
• Автоматично успадковувати всі методи інтерфейсу (Close, LocalAddr, SetDeadline тощо)
• Перевизначити тільки `Read`/`Write` для додавання таймаутів
• Зберегти сумісність з будь-яким кодом, що очікує `net.Conn`

Це патерн "decorator" у мінімалістичному вигляді.
```

---

## 🔑 2. Read() — читання з таймаутом

```go
func (self connWithTimeout) Read(p []byte) (n int, err error) {
    if self.Timeout > 0 {
        // Встановлення дедлайну: "зараз + Timeout"
        self.Conn.SetReadDeadline(time.Now().Add(self.Timeout))
    }
    return self.Conn.Read(p)
}
```

### 🔍 Як працює `SetReadDeadline`:

```
1. time.Now().Add(self.Timeout) → абсолютний час завершення
2. SetReadDeadline() → повідомляє ОС: "якщо дані не прийшли до X, поверни timeout error"
3. Conn.Read(p) → блокується до:
   • Приходу даних → повертає n>0, err=nil
   • Закриття з'єднання → повертає 0, io.EOF
   • Перевищення дедлайну → повертає 0, net.Error with Timeout()=true

Важливо: дедлайн встановлюється на КОЖНУ операцію Read, не на весь потік!
```

### ⚠️ Критичний момент: значення за замовчуванням

```
Якщо `Timeout == 0`:
• SetReadDeadline НЕ викликається
• Поведінка залежить від базового net.Conn:
  - TCP: за замовчуванням немає таймауту → може зависнути назавжди
  - Це може бути бажаним для "вічного" очікування, але небезпечним у production

✅ Рекомендація: завжди встановлюйте розумний Timeout (напр. 5-30 секунд)
```

---

## 🔑 3. Write() — запис з таймаутом

```go
func (self connWithTimeout) Write(p []byte) (n int, err error) {
    if self.Timeout > 0 {
        self.Conn.SetWriteDeadline(time.Now().Add(self.Timeout))
    }
    return self.Conn.Write(p)
}
```

### 🔍 Особливості запису:

```
• Таймаут застосовується до ВСЬОГО виклику Write(p), не до окремих байт
• Якщо буфер відправки переповнений або мережа повільна — Write може заблокуватися
• Після таймауту: повертається net.Error з Timeout()=true, з'єднання залишається відкритим

⚠️ Увага: частковий запис можливий!
Якщо Write повертає n < len(p), err == nil — це означає, що тільки n байт записано.
У такому разі потрібно повторити запис решти даних (але connWithTimeout цього не робить).
```

---

## 🔄 Інтеграція у RTSP Client

### 🔧 Як використовується у `rtsp.DialTimeout`:

```go
func DialTimeout(uri string, timeout time.Duration) (*Client, error) {
    // ... парсинг URL ...
    
    dailer := net.Dialer{Timeout: timeout}  // таймаут на встановлення з'єднання
    conn, err := dailer.Dial("tcp", URL.Host)
    if err != nil { return nil, err }
    
    // 🎯 Створення обгортки з таймаутом на операції
    connt := &connWithTimeout{Conn: conn}
    // ⚠️ Timeout не встановлено! Залишається 0 → немає авто-дедлайнів
    
    self := &Client{
        conn:   connt,
        // ... інші поля ...
    }
    return self, nil
}
```

### ⚠️ Проблема: `Timeout` не ініціалізується при створенні!

```
У вихідному коді:
    connt := &connWithTimeout{Conn: conn}
    // Timeout залишається 0 (zero value) → таймаути НЕ працюють!

Наслідки:
• Read/Write можуть зависнути назавжди при втраті мережі
• Клієнт не реагуватиме на `RtspTimeout`/`RtpTimeout` налаштування

✅ Виправлення:
    connt := &connWithTimeout{
        Conn:    conn,
        Timeout: timeout,  // ← встановити значення!
    }
```

### 🔧 Як використовується у `Client.ReadPacket`:

```go
func (self *Client) readPacket() (pkt av.Packet, err error) {
    // Перед кожною операцією читання:
    self.conn.Timeout = self.RtspTimeout  // ← встановлення таймауту "на льоту"
    
    for {
        var res Response
        for {
            // poll() → findRTSP() → brconn.ReadByte() → conn.Read()
            if res, err = self.poll(); err != nil { return }
            if len(res.Block) > 0 { break }
        }
        // ... обробка ...
    }
}
```

**✅ Правильний підхід**: динамічне встановлення `Timeout` перед критичними операціями.

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **`Timeout == 0` за замовчуванням** | Read/Write зависають назавжди | Ініціалізуйте `Timeout` при створенні: `&connWithTimeout{Conn: c, Timeout: 10*time.Second}` |
| **Частковий запис не обробляється** | `Write(p)` повертає `n < len(p)` без помилки | Додайте цикл повторного запису: `for n < len(p) { written, err := Write(p[n:]); n += written }` |
| **Дедлайн "перетікає" між операціями** | Один повільний Read "краде" час у наступних | Встановлюйте дедлайн безпосередньо перед кожною операцією (як у вихідному коді) |
| **`net.Error` не перевіряється** | Таймаути обробляються як звичайні помилки | Використовуйте `if ne, ok := err.(net.Error); ok && ne.Timeout() { /* retry or reconnect */ }` |
| **Неможливо скинути дедлайн** | Після таймауту з'єднання залишається у "таймаут-стані" | Викликайте `SetReadDeadline(time.Time{})` для скидання (нульовий time.Time = без дедлайну) |

---

## ⚡ Оптимізації для high-performance I/O

### 1. Уникнення зайвих викликів `SetReadDeadline`:

```go
// Оптимізований Read — встановлює дедлайн тільки якщо змінився
func (self *connWithTimeout) Read(p []byte) (int, error) {
    if self.Timeout > 0 {
        deadline := time.Now().Add(self.Timeout)
        // Перевірка чи дедлайн вже встановлено (можна додати кешування)
        self.Conn.SetReadDeadline(deadline)
    }
    return self.Conn.Read(p)
}
```

> 💡 На практиці `SetReadDeadline` — дуже швидка операція (системний виклик не завжди потрібен), тому оптимізація рідко потрібна.

### 2. Підтримка `io.ReaderFrom`/`io.WriterTo`:

```go
// Додати методи для zero-copy передачі даних
func (self *connWithTimeout) ReadFrom(r io.Reader) (int64, error) {
    if self.Timeout > 0 {
        self.Conn.SetReadDeadline(time.Now().Add(self.Timeout))
    }
    // ⚠️ Увага: ReadFrom може читати багато разів — дедлайн застаріє!
    // Краще встановлювати дедлайн на весь процес, а не на кожну ітерацію
    return io.Copy(self.Conn, r)
}
```

### 3. Моніторинг таймаутів:

```go
type TimeoutMetrics struct {
    ReadTimeouts  prometheus.CounterVec
    WriteTimeouts prometheus.CounterVec
    AvgReadTime   prometheus.HistogramVec
}

func (self *connWithTimeout) Read(p []byte) (int, error) {
    start := time.Now()
    if self.Timeout > 0 {
        self.Conn.SetReadDeadline(time.Now().Add(self.Timeout))
    }
    n, err := self.Conn.Read(p)
    duration := time.Since(start)
    
    if err != nil {
        if ne, ok := err.(net.Error); ok && ne.Timeout() {
            metrics.ReadTimeouts.WithLabelValues("rtsp").Inc()
        }
    }
    metrics.AvgReadTime.WithLabelValues("rtsp").Observe(duration.Seconds())
    return n, err
}
```

---

## ✅ Production-ready версія

```go
// connWithTimeout — безпечна обгортка з таймаутами
type connWithTimeout struct {
    Timeout time.Duration
    net.Conn
}

// NewConnWithTimeout — конструктор з валідацією
func NewConnWithTimeout(conn net.Conn, timeout time.Duration) *connWithTimeout {
    if timeout < 0 {
        timeout = 0  // від'ємний = без таймауту
    }
    return &connWithTimeout{
        Conn:    conn,
        Timeout: timeout,
    }
}

// Read — з обробкою часткових читань та таймаутів
func (c *connWithTimeout) Read(p []byte) (int, error) {
    if c.Timeout > 0 {
        if err := c.Conn.SetReadDeadline(time.Now().Add(c.Timeout)); err != nil {
            return 0, fmt.Errorf("set read deadline: %w", err)
        }
    }
    return c.Conn.Read(p)
}

// Write — з циклом для повного запису
func (c *connWithTimeout) Write(p []byte) (int, error) {
    if c.Timeout > 0 {
        if err := c.Conn.SetWriteDeadline(time.Now().Add(c.Timeout)); err != nil {
            return 0, fmt.Errorf("set write deadline: %w", err)
        }
    }
    // Забезпечення повного запису
    for n < len(p) {
        written, err := c.Conn.Write(p[n:])
        n += written
        if err != nil {
            return n, err
        }
    }
    return n, nil
}

// ResetTimeout — динамічна зміна таймауту
func (c *connWithTimeout) ResetTimeout(timeout time.Duration) {
    c.Timeout = timeout
}

// ClearDeadline — скидання дедлайнів (для "вічного" очікування)
func (c *connWithTimeout) ClearDeadline() error {
    zero := time.Time{}
    if err := c.Conn.SetReadDeadline(zero); err != nil {
        return err
    }
    return c.Conn.SetWriteDeadline(zero)
}
```

---

## 📋 Чек-лист використання connWithTimeout

```go
// ✅ 1. Ініціалізуйте Timeout при створенні
conn := rtsp.NewConnWithTimeout(tcpConn, 10*time.Second)

// ✅ 2. Динамічно змінюйте таймаут для різних операцій
client.conn.ResetTimeout(client.RtspTimeout)  // для RTSP команд
client.conn.ResetTimeout(client.RtpTimeout)   // для RTP пакетів

// ✅ 3. Обробляйте net.Error для таймаутів
_, err := conn.Read(buf)
if err != nil {
    if ne, ok := err.(net.Error); ok && ne.Timeout() {
        log.Printf("read timeout, reconnecting...")
        // логіка перепідключення
    }
}

// ✅ 4. Скидайте дедлайни для довгих операцій (якщо потрібно)
if needInfiniteWait {
    conn.ClearDeadline()
}

// ✅ 5. Закривайте з'єднання при завершенні
defer conn.Close()  // викликає conn.Conn.Close() через embedding
```

---

## 🔗 Корисні посилання

- 💻 [Go net.Conn Documentation](https://pkg.go.dev/net#Conn)
- 📄 [SetReadDeadline Behavior](https://pkg.go.dev/net#Conn.SetReadDeadline)
- 🧪 [Handling Partial Writes in Go](https://stackoverflow.com/questions/20376446/golang-net-connection-write-partial-write)
- 📦 [Go net.Error Interface](https://pkg.go.dev/net#Error)

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди ініціалізуйте `Timeout`** при створенні `connWithTimeout` — інакше таймаути не працюватимуть.
> 2. **Встановлюйте розумні значення**: 5-10с для команд, 1-5с для RTP пакетів.
> 3. **Обробляйте `net.Error.Timeout()`** — це дозволить відрізнити таймаут від інших помилок мережі.
> 4. **Не покладайтеся на `Write(p)` як на атомарну операцію** — завжди перевіряйте `n == len(p)`.
> 5. **Використовуйте `ClearDeadline()`** для довгих операцій (напр. запис великого файлу), щоб уникнути несподіваних таймаутів.

Потрібен приклад інтеграції `connWithTimeout` з вашим `rtsp.Client` для надійної обробки таймаутів у production? Готовий допомогти! 🚀