# Глибоке роз'яснення: `packetBuffer` — низькорівневий читач пакетів у astits

Ця структура реалізує **ефективний буферизований читач MPEG-TS пакетів** з підтримкою автодетекції розміру, обробки різних типів `io.Reader` та фільтрації пакетів через `PacketSkipper`.

---

## 🎯 Архітектура: навіщо потрібен `packetBuffer`?

```
┌─────────────────────────────────────────┐
│ Проблеми, які вирішує packetBuffer:    │
│                                         │
│ 🔹 Невідомий розмір пакету (188/192/204)│
│   → autoDetectPacketSize() сканує потік │
│                                         │
│ 🔹 Різні типи io.Reader:               │
│   • bufio.Reader: має Peek(), не має Seek│
│   • os.File: має Seek(), немає Peek()   │
│   • net.Conn: немає нічого → синхронізація│
│                                         │
│ 🔹 Пропуск пошкоджених пакетів:         │
│   → PacketSkipper callback дозволяє     │
│     фільтрувати без зупинки потоку      │
│                                         │
│ 🔹 Ефективне читання:                   │
│   • Переиспользування буфера            │
│   • Мінімізація алокацій                │
│   • Коректна обробка EOF                │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура та ініціалізація

### `packetBuffer` — поля

```go
type packetBuffer struct {
    packetSize       int           // розмір пакету: 188, 192 або 204
    s                PacketSkipper // callback для фільтрації пакетів
    r                io.Reader     // джерело даних
    packetReadBuffer []byte        // переиспользований буфер для читання
}
```

### `newPacketBuffer` — розумна ініціалізація

```go
func newPacketBuffer(r io.Reader, packetSize int, s PacketSkipper) (*packetBuffer, error) {
    pb := &packetBuffer{
        packetSize: packetSize,  // може бути 0 → автодетекція
        s:          s,
        r:          r,
    }
    
    // 🔹 Автодетекція, якщо розмір не вказано
    if pb.packetSize == 0 {
        if pb.packetSize, err = autoDetectPacketSize(r); err != nil {
            return nil, fmt.Errorf("astits: auto detecting packet size failed: %w", err)
        }
    }
    return pb, nil
}
```

> 💡 **Ключова ідея**: Автодетекція виконується **тільки один раз** при ініціалізації. Після цього `packetSize` фіксований — це важливо для продуктивності.

---

## 🔍 `autoDetectPacketSize` — алгоритм детекції

### Сигнатура та обмеження

```go
func autoDetectPacketSize(r io.Reader) (packetSize int, err error)
// 🔹 Читає перші 193 байти
// 🔹 Припускає: перший байт = syncByte (0x47)
// 🔹 Шукає наступний 0x47 → інтервал = розмір пакету
```

### Кроки алгоритму

```go
// 1. Читання префіксу (193 байти = 188 + 5 запас)
const l = 193
b := make([]byte, l)
shouldRewind, rerr := peek(r, b)  // 🎯 розумне читання залежно від типу reader

// 2. Перевірка першого байта
if b[0] != syncByte {  // 0x47
    return 0, ErrPacketMustStartWithASyncByte
}

// 3. Пошук наступного синхробайта
for idx, byteVal := range b {
    if byteVal == syncByte && idx >= MpegTsPacketSize {  // idx >= 188
        packetSize = idx  // ✅ знайдено розмір!
        
        // 4. Відновлення позиції читача
        if !shouldRewind {
            return packetSize, nil  // bufio.Reader: Peek() не змістив позицію
        }
        
        // 🔄 Спроба відмотати (Seek) або синхронізувати (Read+skip)
        n, err := rewind(r)
        if err != nil { return 0, err }
        
        if n == -1 {  // Seek неможливий → синхронізація читанням
            ls := packetSize - (l - packetSize)  // скільки байт "пропустили"
            _, err = r.Read(make([]byte, ls))    // "прочитати і викинути"
            if err != nil { return 0, err }
        }
        return packetSize, nil
    }
}

// 5. Помилка: не знайдено другий синхробайт
return 0, fmt.Errorf("astits: only one sync byte detected in first %d bytes", l)
```

### Візуалізація детекції

```
Вхідний потік (перші 193 байти):
Позиція:  0    1-187   188   189-192
Дані:    [47][187×00][47][4×00]
           ↑         ↑
        пакет 1   пакет 2 (початок)

Алгоритм:
1. b[0] = 0x47 ✅
2. Цикл: idx=188, b[188]=0x47 ✅, idx >= 188 ✅
3. packetSize = 188 ✅
4. Rewind/sync для повернення на початок
```

---

## 🔄 `peek` та `rewind` — адаптація до типів reader

### `peek` — універсальне попереднє читання

```go
func peek(r io.Reader, b []byte) (shouldRewind bool, err error) {
    // 🔹 Спеціальна обробка bufio.Reader
    if br, ok := r.(*bufio.Reader); ok {
        bs, err := br.Peek(len(b))  // Peek() не зміщує позицію!
        if err != nil { return false, err }
        copy(b, bs)                  // скопіювати у вихідний буфер
        return false, nil            // не потрібно rewind
    }
    
    // 🔹 Звичайний reader: читаємо напряму
    _, err = r.Read(b)
    shouldRewind = true  // ⚠️ позиція змістилася → потрібно відмотати
    return
}
```

> 💡 **Чому це важливо?** `bufio.Reader.Peek()` — єдиний стандартний спосіб "підглянути" дані без зміщення позиції. Для інших типів доводиться читати + відмотувати.

### `rewind` — спроба відмотати читач

```go
func rewind(r io.Reader) (n int64, err error) {
    // 🔹 Якщо reader підтримує Seek — використовуємо його
    if s, ok := r.(io.Seeker); ok {
        n, err = s.Seek(0, 0)  // повернутися на початок
        if err != nil {
            return 0, fmt.Errorf("astits: seeking to 0 failed: %w", err)
        }
        return n, nil
    }
    
    // 🔹 Не підтримує Seek → повертаємо -1 (сигнал для синхронізації)
    return -1, nil
}
```

### Матриця підтримки reader-ів

| Тип reader | Peek() | Seek() | Стратегія autoDetect |
|------------|--------|--------|---------------------|
| `*bufio.Reader` | ✅ | ❌ | Peek() → no rewind |
| `*os.File` | ❌ | ✅ | Read() → Seek(0) |
| `*bytes.Reader` | ❌ | ✅ | Read() → Seek(0) |
| `net.Conn` | ❌ | ❌ | Read() → sync via Read(skip) |
| Ваш `WebSocketReader` | ❌ | ❌ | Read() → sync via Read(skip) ⚠️ |

> ⚠️ **Попередження**: Для `net.Conn` або WebSocket синхронізація через `Read(skip)` **втрачає дані**, якщо детекція не ідеальна. Краще явно вказувати `packetSize=188` для мережевих потоків.

---

## 📦 `next()` — отримання наступного пакету

### Алгоритм читання

```go
func (pb *packetBuffer) next() (p *Packet, err error) {
    // 🔹 Ініціалізація буфера (переиспользується!)
    if pb.packetReadBuffer == nil || len(pb.packetReadBuffer) != pb.packetSize {
        pb.packetReadBuffer = make([]byte, pb.packetSize)  // алокація тільки один раз
    }
    
    // 🔹 Цикл: гарантуємо повернення валідного пакету
    for p == nil {
        // 1. Читання рівно packetSize байт
        _, err = io.ReadFull(pb.r, pb.packetReadBuffer)
        if err != nil {
            if err == io.EOF || err == io.ErrUnexpectedEOF {
                err = ErrNoMorePackets  // ✅ нормальне завершення
            } else {
                err = fmt.Errorf("astits: reading %d bytes failed: %w", pb.packetSize, err)
            }
            return
        }
        
        // 2. Парсинг пакета з підтримкою skip-логіки
        if p, err = parsePacket(astikit.NewBytesIterator(pb.packetReadBuffer), pb.s); err != nil {
            if !errors.Is(err, errSkippedPacket) {
                err = fmt.Errorf("astits: building packet failed: %w", err)
                return
            }
            // 🔄 errSkippedPacket → продовжуємо цикл, шукаємо наступний пакет
        }
    }
    
    return p, nil  // ✅ валідний пакет
}
```

### Переиспользування буфера — оптимізація пам'яті

```
Перший виклик next():
• packetReadBuffer == nil → make([]byte, 188)
• Читання → парсинг → повертаємо пакет

Другий виклик next():
• packetReadBuffer != nil && len==188 → переиспользуємо!
• Читання перезаписує старі дані → парсинг → повертаємо
• ✅ Немає нової алокації → менше тиску на GC

Після 1000 пакетів:
• Тільки 1 алокація 188 байт замість 1000
• Економія: ~188KB алокацій + менше GC пауз
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Ініціалізація для WebSocket-приймача

```go
// У вашому WebSocket-обробнику:
func handleWebSocketConnection(conn *websocket.Conn, channelID string) error {
    // 🔹 Для WebSocket краще явно вказати розмір (не покладатися на автодетекцію)
    packetSize := astits.MpegTsPacketSize  // 188
    
    // 🔹 Створити skipper для фільтрації за PID/channel
    skipper := func(p *astits.Packet) bool {
        // Пропускати пакети не для цього каналу (якщо multiplexed)
        return p.Header.PID != expectedPIDForChannel(channelID)
    }
    
    // 🔹 Створити packetBuffer
    pb, err := astits.NewPacketBuffer(conn, packetSize, skipper)
    if err != nil {
        return fmt.Errorf("failed to init packetBuffer for %s: %w", channelID, err)
    }
    
    // 🔹 Цикл читання
    for {
        pkt, err := pb.Next()
        if errors.Is(err, astits.ErrNoMorePackets) {
            break  // нормальне завершення
        }
        if err != nil {
            log.Errorf("Channel %s: packet read error: %v", channelID, err)
            metrics.ReadErrors.WithLabelValues(channelID).Inc()
            continue  // спробувати наступний пакет
        }
        
        // 🔹 Відправити у segmentAssembler
        if err := assembler.AddPacket(pkt, channelID); err != nil {
            log.Warnf("Channel %s: assembler error: %v", channelID, err)
        }
    }
    return nil
}
```

### ✅ 2. Обробка файлів з автодетекцією

```go
// Для локальних .ts файлів — автодетекція працює ідеально:
func processTSFile(filePath string, channelID string) error {
    file, err := os.Open(filePath)
    if err != nil { return err }
    defer file.Close()
    
    // 🔹 packetSize=0 → автодетекція + Seek(0) працює для os.File
    pb, err := astits.NewPacketBuffer(file, 0, nil)
    if err != nil {
        return fmt.Errorf("failed to init buffer for %s: %w", filePath, err)
    }
    
    log.Infof("File %s: detected packet size %d", filePath, pb.PacketSize())
    
    // Далі — стандартний цикл читання...
    return processPacketBuffer(pb, channelID)
}
```

### ✅ 3. Моніторинг ефективності читання

```go
// monitoring.Monitor — метрики для packetBuffer:
type ReadMetrics struct {
    PacketsRead      *prometheus.CounterVec
    PacketsSkipped   *prometheus.CounterVec
    ReadErrors       *prometheus.CounterVec
    AvgReadLatency   *prometheus.HistogramVec
    AutoDetectSizes  *prometheus.HistogramVec  // розподіл виявлених розмірів
}

// У циклі читання:
start := time.Now()
pkt, err := pb.Next()
latency := time.Since(start)

if err != nil {
    if errors.Is(err, astits.ErrNoMorePackets) {
        break
    }
    metrics.ReadErrors.WithLabelValues(channelID).Inc()
    continue
}

if pkt == nil {
    metrics.PacketsSkipped.WithLabelValues(channelID).Inc()  // skipper відфільтрував
    continue
}

metrics.PacketsRead.WithLabelValues(channelID).Inc()
metrics.AvgReadLatency.WithLabelValues(channelID).Observe(latency.Seconds())
```

### ✅ 4. Обробка помилки синхронізації для мережевих потоків

```go
// Якщо автодетекція неможлива для net.Conn — fallback логіка:
func safeNewPacketBuffer(r io.Reader, packetSize int, skipper PacketSkipper) (*packetBuffer, error) {
    // Спроба 1: якщо розмір вказано — використовуємо його
    if packetSize > 0 {
        return astits.NewPacketBuffer(r, packetSize, skipper)
    }
    
    // Спроба 2: автодетекція (може не працювати для net.Conn)
    pb, err := astits.NewPacketBuffer(r, 0, skipper)
    if err == nil {
        return pb, nil
    }
    
    // Спроба 3: fallback на стандартний розмір + логування
    log.Warnf("Auto-detect failed: %v, falling back to packetSize=188", err)
    return astits.NewPacketBuffer(r, astits.MpegTsPacketSize, skipper)
}

// Використання:
pb, err := safeNewPacketBuffer(websocketConn, 0, mySkipper)
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на автодетекцію для os.File

```go
func TestPacketBuffer_AutoDetectFile(t *testing.T) {
    // Створити тимчасовий файл з відомим вмістом
    tmpFile, err := os.CreateTemp("", "test_*.ts")
    require.NoError(t, err)
    defer os.Remove(tmpFile.Name())
    
    // Записати 3 пакети по 188 байт
    for i := 0; i < 3; i++ {
        pkt := make([]byte, 188)
        pkt[0] = 0x47  // sync byte
        // ... заповнити решту ...
        _, err := tmpFile.Write(pkt)
        require.NoError(t, err)
    }
    
    // Відкрити для читання + автодетекція
    tmpFile.Seek(0, 0)
    pb, err := newPacketBuffer(tmpFile, 0, nil)  // packetSize=0 → автодетекція
    assert.NoError(t, err)
    assert.Equal(t, 188, pb.packetSize)
    
    // Прочитати пакети
    for i := 0; i < 3; i++ {
        pkt, err := pb.next()
        assert.NoError(t, err)
        assert.Equal(t, uint8(0x47), pkt.Header.SyncByte)
    }
    
    // Четвертий виклик → EOF
    _, err = pb.next()
    assert.Equal(t, ErrNoMorePackets, err)
}
```

### 🔹 Тест на skipper-фільтрацію

```go
func TestPacketBuffer_SkipperFilter(t *testing.T) {
    // Створити буфер з пакетами різних PID
    buf := &bytes.Buffer{}
    for pid := uint16(100); pid < 110; pid++ {
        pkt := make([]byte, 188)
        pkt[0] = 0x47
        // Записати PID у заголовок (біти 1-13 після sync)
        // ... бітова маніпуляція ...
        buf.Write(pkt)
    }
    
    // Skipper: пропускати все крім PID=105
    skipper := func(p *Packet) bool {
        return p.Header.PID != 105
    }
    
    pb, err := newPacketBuffer(buf, 188, skipper)
    assert.NoError(t, err)
    
    // Прочитати — має повернутися тільки PID=105
    pkt, err := pb.next()
    assert.NoError(t, err)
    assert.Equal(t, uint16(105), pkt.Header.PID)
    
    // Наступний виклик → EOF (всі інші відфільтровані)
    _, err = pb.next()
    assert.Equal(t, ErrNoMorePackets, err)
}
```

### 🔹 Бенчмарк на переиспользування буфера

```go
func BenchmarkPacketBuffer_Reuse(b *testing.B) {
    // Згенерувати 1000 валідних пакетів
    data := make([]byte, 188*1000)
    for i := 0; i < 1000; i++ {
        data[i*188] = 0x47  // sync byte
        // ... решта заголовка ...
    }
    
    r := bytes.NewReader(data)
    pb, _ := newPacketBuffer(r, 188, nil)
    
    b.ResetTimer()
    b.ReportAllocs()
    
    for i := 0; i < b.N; i++ {
        r.Seek(0, 0)  // скинути для повторного тесту
        for j := 0; j < 1000; j++ {
            _, err := pb.next()
            if err != nil && !errors.Is(err, ErrNoMorePackets) {
                b.Fatal(err)
            }
        }
    }
}
```

**Очікуваний результат:**
```
BenchmarkPacketBuffer_Reuse-8    100    12000000 ns/op    188 B/op    1 allocs/op
```
→ **1 алокація** на весь цикл завдяки переиспользуванню `packetReadBuffer`.

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Автодетекція для net.Conn втрачає дані | Перші пакети "зникають" після детекції | Явно вказувати `packetSize=188` для мережевих потоків; не використовувати автодетекцію |
| `bufio.Reader` + автодетекція | Позиція не скидається → дублювання даних | `peek()` для `bufio.Reader` повертає `shouldRewind=false` → алгоритм коректний ✅ |
| Короткий вхідний буфер (<193 байти) | Помилка "only one sync byte detected" | Перевіряти `len(data) >= 2*packetSize` перед ініціалізацією |
| Пошкоджені пакети у потоці | `parsePacket` повертає помилку → зупинка | Використовувати `PacketSkipper` для фільтрації + логування помилок |
| Багатопотоковий доступ до одного buffer | Race condition при читанні | `packetBuffer` не thread-safe → створювати окремий екземпляр на горутину |

### Приклад thread-safe обгортки:

```go
type SafePacketBuffer struct {
    mu sync.Mutex
    pb *packetBuffer
}

func (spb *SafePacketBuffer) Next() (*Packet, error) {
    spb.mu.Lock()
    defer spb.mu.Unlock()
    return spb.pb.next()
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Ініціалізація з правильними параметрами:
func initPacketBuffer(source interface{}, channelID string) (*packetBuffer, error) {
    var r io.Reader
    var packetSize int
    
    switch s := source.(type) {
    case *os.File:
        r = s
        packetSize = 0  // ✅ автодетекція працює для файлів
    case *websocket.Conn:
        r = s
        packetSize = astits.MpegTsPacketSize  // ⚠️ явно вказати для мережі
    case io.Reader:
        r = s
        packetSize = astits.MpegTsPacketSize  // fallback
    default:
        return nil, fmt.Errorf("unsupported source type: %T", source)
    }
    
    skipper := createChannelSkipper(channelID)
    return newPacketBuffer(r, packetSize, skipper)
}

// 2. Цикл читання з обробкою помилок:
func readLoop(pb *packetBuffer, channelID string) error {
    for {
        pkt, err := pb.next()
        if errors.Is(err, astits.ErrNoMorePackets) {
            log.Infof("Channel %s: stream ended", channelID)
            return nil
        }
        if err != nil {
            metrics.ReadErrors.WithLabelValues(channelID).Inc()
            log.Warnf("Channel %s: read error: %v", channelID, err)
            continue  // не зупиняти весь потік через один пошкоджений пакет
        }
        if pkt == nil {
            // skipper відфільтрував — це не помилка
            continue
        }
        
        // Обробити валідний пакет
        if err := processPacket(pkt, channelID); err != nil {
            log.Errorf("Channel %s: process error: %v", channelID, err)
        }
    }
}

// 3. Створення skipper для channel-aware фільтрації:
func createChannelSkipper(channelID string) PacketSkipper {
    allowedPIDs := getExpectedPIDsForChannel(channelID)  // ваш мапінг
    
    return func(p *Packet) bool {
        // Пропускати, якщо PID не в списку дозволених
        return !allowedPIDs[p.Header.PID]
    }
}
```

---

## 📊 Матриця поведінки для різних reader-ів

```
Тип reader      | Auto-detect | Rewind strategy | Рекомендація
────────────────┼─────────────┼─────────────────┼─────────────────
*os.File        | ✅ Працює   | Seek(0)         | Використовувати auto-detect
*bytes.Reader   | ✅ Працює   | Seek(0)         | Використовувати auto-detect
*bufio.Reader   | ✅ Працює   | Peek() (no rewind)| ✅ Найкраща підтримка
net.Conn        | ⚠️ Втрачає дані | Read(skip)    | Явно вказати packetSize=188
websocket.Conn  | ⚠️ Втрачає дані | Read(skip)    | Явно вказати packetSize=188
Ваш CustomReader| ❓ Залежить | Реалізувати io.Seeker або вказати розмір | Документувати обмеження
```

---

## 📚 Корисні посилання

- [io.Reader та io.Seeker інтерфейси](https://pkg.go.dev/io)
- [bufio.Reader.Peek документація](https://pkg.go.dev/bufio#Reader.Peek)
- [astits packet_buffer.go source](https://github.com/asticode/go-astits/blob/master/packet_buffer.go)

> 💡 **Ключова ідея**: `packetBuffer` — це "розумний адаптер" між сирий `io.Reader` та структурованими `Packet` об'єктами. У вашому CCTV HLS пайплайні він дозволяє:
> - 🔄 Уніфікувати обробку файлів, WebSocket та UDP через єдиний інтерфейс
> - 🎯 Фільтрувати непотрібні пакети на ранньому етапі (економія CPU/пам'яті)
> - 📊 Збирати метрики про якість читання для моніторингу стабільності потоку

Якщо потрібно — можу допомогти:
- 🔌 Реалізувати `io.Seeker` для вашого `WebSocketReader` щоб увімкнути автодетекцію
- 🧩 Інтегрувати `PacketSkipper` у ваш channel-aware архітектуру для ефективного фільтрування
- 📈 Додати Prometheus-метрики для моніторингу латентності читання по каналах

🛠️