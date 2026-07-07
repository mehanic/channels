# 🔍 Глибокий розбір коду: MPEG-2 TS стрімінг-парсер з UDP/File підтримкою

Цей код реалізує **високопродуктивний стрімінг-парсер** для MPEG-2 Transport Stream з підтримкою входу через UDP або файл, конкурентною обробкою та статистикою в реальному часі. Розберемо архітектурно, детально та з критичним аналізом.

---

## 📦 Архітектура програми: огляд компонентів

```
┌─────────────────────────────────────┐
│ Програма: main.go (streaming parser)│
│ Входи: UDP :50000 або файл test.ts  │
│ Вихід: статистика + PES-потік       │
│ Бібліотека: go-mpeg2-ts             │
├─────────────────────────────────────┤
│ 🔹 Компоненти:                       │
│    • TSEngine — ядро TS-парсингу    │
│    • PESParser — витяг Elementary   │
│    • State Machine — PAT→PMT→PES    │
│    • Buffer Manager — inBuffer + mutex│
│    • Stats Collector — атомічні лічильники│
│                                      │
│ 🔹 Глобальний стан:                  │
│    • inBuffer []byte — спільний буфер│
│    • inBufferMutex — захист буфера  │
│    • udpInCount/pesOutCount — атоміки│
│    • continuityIndexes — map[PID]byte│
└─────────────────────────────────────┘
```

### 🎯 Потік даних (Data Flow)
```
[UDP:50000] или [файл test.ts]
        │
        ▼
┌─────────────────┐
│ startUDPSrc()   │
│ startFileSrc()  │
│ • Читання чанків │
│ • Запис у inBuffer│
│ • atomic.Add(udpInCount)│
└─────────────────┘
        │
        ▼ (кожні 27мс через bufTicker)
┌─────────────────┐
│ inBufferMutex.Lock()│
│ tse.Write(inBuffer) │ ← TSEngine парсить пакети
│ inBuffer = inBuffer[:0]│ ← очищення буфера
│ inBufferMutex.Unlock()│
└─────────────────┘
        │
        ▼ (через tsPacketChan)
┌─────────────────┐
│ State Machine   │
│ state=0: PAT → PID_PAT=0x00│
│ state=1: PMT → program.ProgramMapPID│
│ state=2: PES → elementaryPID (AVC/MPEG2)│
│ • continuity check │
│ • pesParser.EnqueueTSPacket()│
└─────────────────┘
        │
        ▼ (через pesChan)
┌─────────────────┐
│ PES Receiver    │
│ atomic.Add(pesOutCount, len(ES))│
│ (тут можна відправити у ваш pipeline)│
└─────────────────┘
        │
        ▼ (кожну 1с через statTicker)
┌─────────────────┐
│ Stats Logger    │
│ pesrate = pesOutCount*8/1024 Kbps│
│ incoming = udpInCount*8/1024 Kbps│
└─────────────────┘
```

---

## 🔬 Детальний розбір ключових компонентів

### 1️⃣ Глобальний стан та буферизація

```go
var (
    udpInCount      = uint32(0)           // ✅ Атомік для статистики
    pesOutCount     = uint32(0)           // ✅ Атомік для статистики
    inBuffer        []byte                // ❌ Глобальний буфер — джерело проблем!
    inBufferMutex   *sync.Mutex           // ❌ Покажчик на м'ютекс — незвично
    disableCRCcheck = false               // ❌ Глобальний прапорець
)

func main() {
    inBuffer = make([]byte, 0, 16*1048576)  // 🎯 16MB capacity, 0 length
    inBufferMutex = &sync.Mutex{}           // 🎯 Ініціалізація покажчика
    // ...
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Глобальний inBuffer []byte:
// • Зростає необмежено, якщо tse.Write() повільніший за вхід
// • Немає backpressure → OOM при великому навантаженні
// • Неможливо запустити кілька парсерів паралельно

// ✅ Рішення: обмежений канал або кільцевий буфер
type BoundedBuffer struct {
    ch     chan []byte
    maxLen int
}

func NewBoundedBuffer(maxBytes int) *BoundedBuffer {
    return &BoundedBuffer{
        ch:     make(chan []byte, 100),  // 100 чанків у черзі
        maxLen: maxBytes,
    }
}

func (b *BoundedBuffer) Write(data []byte) error {
    if len(data) > b.maxLen {
        return fmt.Errorf("chunk too large: %d > %d", len(data), b.maxLen)
    }
    select {
    case b.ch <- append([]byte(nil), data...):  // Копія для безпеки
        return nil
    default:
        return fmt.Errorf("buffer full, dropping chunk")  // Backpressure!
    }
}

// ❌ Покажчик на sync.Mutex:
inBufferMutex *sync.Mutex  // Нестандартно, може заплутати
// ✅ Правильно: значення
inBufferMutex sync.Mutex   // Простіше, безпечніше

// ❌ Глобальний disableCRCcheck:
// • Неможливо мати різні налаштування для різних потоків
// ✅ Правильно: конфігурація через структуру
type ParserConfig struct {
    DisableCRC bool
    BufferSize int
    // ...
}
```

---

### 2️⃣ TSEngine та PESParser — конкурентна обробка

```go
tse, _ := mpeg2ts.InitTSEngine(mpeg2ts.PacketSizeDefault, 1024)  // ❌ Помилка ігнорується!
tsPacketChan := tse.StartPacketReadLoop(ctx)                      // 🎯 Channel для пакетів
pesParser := mpeg2ts.NewPESParser(1500)                           // ❌ Hardcoded буфер

// 🎯 PES receiver горутина
go func() {
    for {
        select {
        case pes := <-pesChan:
            atomic.AddUint32(&pesOutCount, uint32(len(pes.ElementaryStream)))  // 🎯 Тільки статистика!
        case <-ctx.Done():
            log.Println("PES receiver exit")
            return
        }
    }
}()
```

#### ⚠️ Критичні проблеми
```go
// ❌ Ігнорування помилки InitTSEngine:
tse, _ := mpeg2ts.InitTSEngine(...)  // Якщо err != nil → tse = nil → паніка!
// ✅ Правильно:
tse, err := mpeg2ts.InitTSEngine(...)
if err != nil {
    log.Fatalf("Failed to init TSEngine: %v", err)
}

// ❌ Hardcoded розмір PES-буфера (1500):
pesParser := mpeg2ts.NewPESParser(1500)
// • Може бути замалим для великих відео-фреймів
// ✅ Правильно: конфігурувати
const DefaultPESBufferSize = 64 * 1024  // 64KB
pesParser := mpeg2ts.NewPESParser(DefaultPESBufferSize)

// ❌ PES receiver тільки рахує байти — дані втрачаються!
atomic.AddUint32(&pesOutCount, uint32(len(pes.ElementaryStream)))
// • elementaryStream нікуди не відправляється → марна робота парсера!
// ✅ Правильно: інтегрувати з вашим pipeline
go func() {
    for {
        select {
        case pes := <-pesChan:
            // 🎯 Відправка у ваш segmentAssembler
            if err := assembler.ProcessFrame(pes.ElementaryStream); err != nil {
                log.Printf("Frame processing failed: %v", err)
            }
            atomic.AddUint32(&pesOutCount, uint32(len(pes.ElementaryStream)))
        case <-ctx.Done():
            return
        }
    }
}()
```

---

### 3️⃣ State Machine — PAT → PMT → PES

```go
state := 0
pmtPID := -1
elementaryPID := mpeg2ts.PID(0)

Loop:
for {
    select {
    case v, ok := <-tsPacketChan:
        if !ok {
            log.Fatal("tsPacketChan is closed!")  // ❌ log.Fatal + os.Exit неявно
        }
        
        if state == 0 && v.PID == mpeg2ts.PID_PAT {
            // 🎯 Стан 0: пошук PAT
            pat, err := v.ParsePAT()
            if err != nil {
                log.Fatalln(err)  // ❌ Зупиняє всю програму
                os.Exit(1)         // ❌ Зайвий виклик після Fatalln
            }
            // ... знайшли PMT PID → state=1
            
        } else if state == 1 && v.PID == mpeg2ts.PID(pmtPID) {
            // 🎯 Стан 1: пошук PMT
            pmt, err := v.ParsePMT(disableCRCcheck)
            if err != nil {
                log.Println("invalid PMT!", err)  // ❌ Продовжуємо без PMT!
                // continue  // Закоментовано → потенційна помилка
            }
            // ... знайшли AVC → state=2
            
        } else if state == 2 && v.PID == elementaryPID {
            // 🎯 Стан 2: PES обробка
            pesParser.EnqueueTSPacket(v)  // ❌ Помилка Enqueue ігнорується!
        }
        // ...
    }
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ log.Fatal у головному циклі:
log.Fatalln(err)  // Зупиняє ВСЮ програму, навіть якщо це тимчасова помилка мережі
// ✅ Правильно: логувати та продовжувати або перезапускати парсинг
if err != nil {
    log.Printf("PAT parse error (recoverable): %v", err)
    continue  // Спробувати наступний пакет
}

// ❌ "Тихий" провал ParsePMT:
if err != nil {
    log.Println("invalid PMT!", err)
    // continue  // Закоментовано! → код продовжує виконання з nil pmt
}
if len(pmt.Streams) > 0 {  // ❌ pmt може бути nil → паніка!
    // ...
}
// ✅ Правильно:
if err != nil {
    log.Printf("PMT parse failed for PID 0x%X: %v", pmtPID, err)
    continue  // Пропустити цей пакет
}
// Тепер pmt гарантовано не nil

// ❌ Ігнорування помилки EnqueueTSPacket:
pesParser.EnqueueTSPacket(v)  // Якщо поверне error → дані втрачені!
// ✅ Правильно:
if err := pesParser.EnqueueTSPacket(v); err != nil {
    log.Printf("Failed to enqueue packet PID=0x%X: %v", v.PID, err)
    // Опціонально: скинути парсер або пропустити пошкоджений пакет
}
```

---

### 4️⃣ Буферний менеджер — inBuffer + ticker

```go
bufTicker := time.NewTicker(27 * time.Millisecond)  // 🎯 ~37 Гц оновлення
go func() {
    for {
        <-bufTicker.C
        inBufferMutex.Lock()
        tse.Write(inBuffer)      // 🎯 Парсинг накопичених даних
        inBuffer = inBuffer[:0]  // 🎯 Очищення без виділення нової пам'яті
        inBufferMutex.Unlock()
    }
}()
```

#### ⚠️ Критичні проблеми
```go
// ❌ Фіксований інтервал 27мс:
// • Не адаптується до швидкості вхідного потоку
// • Може створити затримку або перевантаження
// ✅ Правильно: динамічний інтервал або backpressure
type AdaptiveTicker struct {
    baseInterval time.Duration
    minInterval  time.Duration
    maxInterval  time.Duration
    // ... логіка адаптації за заповненням буфера ...
}

// ❌ inBuffer = inBuffer[:0] не звільняє пам'ять:
// • Capacity залишається 16MB → пам'ять не повертається GC
// • При великих спайках входу → високе споживання пам'яті
// ✅ Правильно: періодичне перевиділення або обмеження capacity
const MaxBufferCapacity = 8 * 1048576  // 8MB max
if cap(inBuffer) > MaxBufferCapacity {
    inBuffer = make([]byte, 0, MaxBufferCapacity)  // Перевиділення
} else {
    inBuffer = inBuffer[:0]  // Швидке очищення
}

// ❌ Немає backpressure:
// • Якщо tse.Write() повільний → inBuffer росте → OOM
// ✅ Правильно: обмеження розміру буфера
const MaxInBufferSize = 32 * 1048576  // 32MB
inBufferMutex.Lock()
if len(inBuffer) > MaxInBufferSize {
    log.Warn("Input buffer full, dropping incoming data")
    inBufferMutex.Unlock()
    continue  // Пропустити нові дані
}
inBuffer = append(inBuffer, buf[:n]...)
inBufferMutex.Unlock()
```

---

### 5️⃣ Вхідні джерела: UDP vs File

```go
func startUDPSrc(ctx context.Context) {
    udpConn, err := net.ListenPacket("udp", "0.0.0.0:50000")  // 🎯 Сервер на 50000
    // ...
    udpSenderConn, err := net.DialUDP("udp", nil, udpAddr)    // 🎯 Клієнт на 5000?
    // ...
    go func() {
        buf := [1500]byte{}  // 🎯 Фіксований буфер 1500 байт
        for {
            n, _, err := udpConn.ReadFrom(buf[:])
            // ... запис у inBuffer ...
        }
    }()
}

func startFileSrc(_ctx context.Context, filename string, byterateLimit int) error {
    // ...
    go func() {
        buf := [1500]byte{}
        for {
            n, err = reader.Read(buf[:])  // 🎯 Читання з файлу
            // ... запис у inBuffer ...
            time.Sleep(1 * time.Microsecond)  // 🎯 Штучна затримка для лімітування?
        }
    }()
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Невідповідність портів у startUDPSrc:
// • ListenPacket на :50000 (сервер)
// • DialUDP на :5000 (клієнт) → навіщо відправляти дані назад?
// ✅ Правильно: або тільки сервер, або тільки клієнт
func startUDPSrc(ctx context.Context, listenAddr string) error {
    conn, err := net.ListenPacket("udp", listenAddr)  // Напр. ":50000"
    if err != nil {
        return fmt.Errorf("failed to listen UDP: %w", err)
    }
    // ... тільки ReadFrom, без DialUDP ...
}

// ❌ Фіксований буфер 1500 байт для UDP:
// • TS пакети = 188 байт, але UDP може приходити чанками >1500
// • Можлива фрагментація або втрата даних
// ✅ Правильно: буфер ≥ MTU (1500) або динамічний
const MaxUDPChunk = 65535  // Максимальний UDP пакет
buf := make([]byte, MaxUDPChunk)

// ❌ Штучна затримка 1µs у file src:
time.Sleep(1 * time.Microsecond)  // Не надійний спосіб лімітування швидкості
// ✅ Правильно: token bucket або time.Ticker для точного rate limiting
type RateLimiter struct {
    ticker  *time.Ticker
    maxBytesPerTick int
}

func (rl *RateLimiter) Allow(n int) bool {
    <-rl.ticker.C  // Чекаємо наступного "тик"
    return n <= rl.maxBytesPerTick
}
```

---

### 6️⃣ Статистика та моніторинг

```go
statTicker := time.NewTicker(1 * time.Second)
go func() {
    for {
        <-statTicker.C
        log.Println("-------------------")
        old := atomic.SwapUint32(&pesOutCount, 0)  // 🎯 Атомік swap + reset
        log.Printf("pesrate %dKbps frame:%d\n", old/1024*8, frameIndex)
        old = atomic.SwapUint32(&udpInCount, 0)
        log.Printf("incoming rate %dKbps\n", old/1024*8)
    }
}()
```

#### ⚠️ Потенційні покращення
```go
// ✅ Атомічні операції для статистики — правильно!
// • atomic.SwapUint32 — атомарне читання + скидання
// • Без mutex → швидше для лічильників

// ❌ Розрахунок Kbps не враховує інтервал точно:
old/1024*8  // Припускає рівно 1 секунду між тиками
// ✅ Правильно: вимірювати реальний інтервал
lastTime := time.Now()
go func() {
    for range statTicker.C {
        now := time.Now()
        interval := now.Sub(lastTime).Seconds()
        lastTime = now
        
        pesBytes := atomic.SwapUint32(&pesOutCount, 0)
        pesKbps := float64(pesBytes) * 8 / 1024 / interval
        log.Printf("pesrate %.2fKbps frame:%d\n", pesKbps, frameIndex)
    }
}()

// ❌ Тільки логування — немає інтеграції з Prometheus/Grafana
// ✅ Додати метрики для production-моніторингу:
import "github.com/prometheus/client_golang/prometheus"

var (
    tsPacketsTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{Name: "ts_packets_total", Help: "Total TS packets processed"},
        []string{"pid", "type"},
    )
    continuityErrors = prometheus.NewCounter(
        prometheus.CounterOpts{Name: "ts_continuity_errors_total", Help: "Continuity check failures"},
    )
    pesBytesProcessed = prometheus.NewCounter(
        prometheus.CounterOpts{Name: "pes_bytes_processed_total", Help: "Elementary stream bytes"},
    )
)

func init() {
    prometheus.MustRegister(tsPacketsTotal, continuityErrors, pesBytesProcessed)
}
```

---

### 7️⃣ Continuity Check — виявлення втрат пакетів

```go
continuityIndexes := map[mpeg2ts.PID]byte{}  // 🎯 Останній counter для кожного PID

// У головному циклі:
ci, ok := continuityIndexes[v.PID]
if ok {
    if (ci+1)%16 != v.ContinuityCheckIndex {
        // log.Printf("drop frame detected! ...")  // Закоментовано!
    }
}
continuityIndexes[v.PID] = v.ContinuityCheckIndex
```

#### 🎯 Як працює continuity counter
```
📋 MPEG-2 TS специфікація:
• Кожен пакет має 4-бітовий continuity_counter (0-15)
• Зростає по модулю 16 для кожного PID
• Пропуск значення = втрата пакету (drop)
• Повтор значення = дублікат (можливо при retransmit)

🔍 Приклад:
PID=0x100: counter=5 → 6 → 7 → 9  ← ❌ Пропущено 8 = втрата пакету!
PID=0x100: counter=5 → 6 → 6 → 7  ← ⚠️ Дублікат 6 = можливо retransmit

⚠️ Обмеження:
• Не виявляє помилки всередині пакету (тільки втрату цілого пакету)
• Може хибно спрацювати при splicing/re-muxing (легальні розриви)
• Потрібно скидати при зміні програми (new PMT)
```

#### ⚠️ Проблеми реалізації
```go
// ❌ Закоментоване логування втрат:
// log.Printf("drop frame detected! ...")  // Не бачимо проблем!
// ✅ Правильно: логувати та збирати метрики
if (ci+1)%16 != v.ContinuityCheckIndex {
    log.Printf("Continuity error: PID=0x%X expected=%d actual=%d", 
        v.PID, (ci+1)%16, v.ContinuityCheckIndex)
    continuityErrors.Inc()  // Prometheus метрика
}

// ❌ continuityIndexes росте необмежено:
// • Нові PID додаються, старі не видаляються → витік пам'яті
// ✅ Правильно: періодичне очищення або LRU-кеш
const MaxTrackedPIDs = 1000  // Розумний ліміт
if len(continuityIndexes) > MaxTrackedPIDs {
    // Видалити найстаріші записи (потрібен timestamp)
    cleanupStalePIDs(continuityIndexes, 5*time.Minute)
}
```

---

## ⚠️ Загальні проблеми програми

### 1️⃣ Відсутність граціозного завершення
```go
// ❌ При ctx.Done() — просто вихід з циклу:
case <-ctx.Done():
    break Loop  // Але горутини можуть ще працювати!

// Проблеми:
// • tse.Write() може блокуватися
// • PES receiver може чекати на канал
// • Файли/сокети не закриваються → витік ресурсів

// ✅ Правильно: WaitGroup + defer close
var wg sync.WaitGroup
wg.Add(3)  // TSEngine writer, PES receiver, stats logger

go func() {
    defer wg.Done()
    // ... TSEngine writer ...
}()

// При завершенні:
cancel()  // Скасувати контекст
wg.Wait() // Дочекатися всіх горутин
tse.Close()  // Закрити ресурси парсера
udpConn.Close()  // Закрити сокет
```

### 2️⃣ Пам'ять та продуктивність
```go
// ❌ inBuffer може рости необмежено:
inBuffer = append(inBuffer, buf[:n]...)  // Без перевірки capacity
// → При швидкому вході → OOM

// ✅ Правильно: обмеження + backpressure
const MaxInputBuffer = 64 * 1024 * 1024  // 64MB
inBufferMutex.Lock()
if len(inBuffer)+n > MaxInputBuffer {
    log.Warn("Input buffer limit reached, dropping chunk")
    inBufferMutex.Unlock()
    continue  // Backpressure: відкидаємо нові дані
}
inBuffer = append(inBuffer, buf[:n]...)
inBufferMutex.Unlock()

// ❌ Часте виділення пам'яті у PES receiver:
atomic.AddUint32(&pesOutCount, uint32(len(pes.ElementaryStream)))
// • pes.ElementaryStream — новий slice кожного разу
// ✅ Оптимізація: reuse buffer або zero-copy якщо можливо
```

### 3️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу
// • Неможливо перевірити коректність state machine
// • Неможливо покрити edge cases (пошкоджені пакети, незвичні PID)

// ✅ Додати мінімальні тести:
func TestStateMachine_PATPMTDetection(t *testing.T) {
    // 🎯 Mock TS потік з відомою структурою
    parser := newTestParser()
    
    // 🎯 Подати PAT пакет
    parser.processPacket(mockPATPacket)
    assert.Equal(t, 1, parser.state)
    assert.Equal(t, expectedPMT_PID, parser.pmtPID)
    
    // 🎯 Подати PMT пакет
    parser.processPacket(mockPMTPacket)
    assert.Equal(t, 2, parser.state)
    assert.Equal(t, expectedVideoPID, parser.elementaryPID)
}
```

### 4️⃣ Конфігурація через глобальні змінні
```go
// ❌ Глобальні прапорці:
var (
    fromUDP = false
    disableCRCcheck = false
)
// • Неможливо мати різні налаштування для різних потоків
// • Важко тестувати (змінює глобальний стан)

// ✅ Правильно: структура конфігурації
type Config struct {
    InputType   string  // "udp" або "file"
    InputPath   string  // ":50000" або "test.ts"
    DisableCRC  bool
    BufferSize  int
    RateLimit   int  // bytes/sec
}

func main() {
    cfg := parseConfig()  // CLI args або config file
    run(cfg)
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **WebSocket-приймачем fMP4-фрагментів**:

### 🎯 Сценарій: TS → fMP4 конвертація у реальному часі
```go
// У вашому pipeline при отриманні TS-чанку:
type TSStreamProcessor struct {
    cfg        Config
    tse        *mpeg2ts.TSEngine
    pesParser  *mpeg2ts.PESParser
    assembler  *SegmentAssembler  // Ваш існуючий компонент
    metrics    *StreamMetrics
}

func (p *TSStreamProcessor) Start(ctx context.Context) error {
    // 🎯 Ініціалізація компонентів
    tse, err := mpeg2ts.InitTSEngine(mpeg2ts.PacketSizeDefault, 1024)
    if err != nil {
        return fmt.Errorf("TSEngine init failed: %w", err)
    }
    
    pesParser := mpeg2ts.NewPESParser(p.cfg.PESBufferSize)
    pesChan := pesParser.StartPESReadLoop(ctx)
    
    // 🎯 Обробник PES → ваш segmentAssembler
    go func() {
        for {
            select {
            case pes := <-pesChan:
                if err := p.assembler.ProcessElementaryStream(
                    pes.ElementaryStream, 
                    pes.PTS,  // Якщо доступно
                ); err != nil {
                    p.metrics.Errors.Inc()
                    log.Printf("AS processing failed: %v", err)
                }
                p.metrics.BytesProcessed.Add(uint64(len(pes.ElementaryStream)))
                
            case <-ctx.Done():
                return
            }
        }
    }()
    
    // 🎯 Головний цикл парсингу (аналогічно вашому коду, але з обробкою помилок)
    return p.runParserLoop(ctx, tse, pesParser)
}

// Використання у WebSocket хендлері:
func (h *WSHandler) onTSFragment(channelID string, data []byte) {
    processor := h.getOrCreateProcessor(channelID)
    
    // 🎯 Backpressure: перевірка буфера перед прийомом
    if !processor.CanAcceptMore() {
        h.metrics.DroppedChunks.Inc()
        return  // Відкидаємо, щоб уникнути OOM
    }
    
    if err := processor.EnqueueTSData(data); err != nil {
        h.metrics.Errors.Inc()
        log.Printf("TS enqueue failed for %s: %v", channelID, err)
    }
}
```

### 🎯 Сценарій: моніторинг якості стріму
```go
// У monitoring.Monitor для аналізу TS-якості:
type StreamQualityReport struct {
    ChannelID         string
    ContinuityErrors  int
    PacketLossRate    float64  // %
    PCRJitter         time.Duration
    AvgBitrateKbps    float64
    Status            string  // "ok", "degraded", "failed"
}

func (m *Monitor) AnalyzeTSQuality(channelID string, stats *StreamStats) StreamQualityReport {
    report := StreamQualityReport{ChannelID: channelID}
    
    // 🎯 Розрахунок метрик
    report.ContinuityErrors = stats.ContinuityErrors
    report.PacketLossRate = float64(stats.DroppedPackets) / float64(stats.TotalPackets) * 100
    report.PCRJitter = stats.CalculatePCRJitter()
    report.AvgBitrateKbps = stats.AvgBitrateKbps
    
    // 🎯 Визначення статусу
    if report.PacketLossRate > 5.0 || report.ContinuityErrors > 10 {
        report.Status = "degraded"
        m.alerts["stream_degraded"].Inc()
    }
    if report.PacketLossRate > 20.0 {
        report.Status = "failed"
        m.alerts["stream_failed"].Inc()
    }
    
    return report
}
```

### 🎯 Сценарій: адаптивне буферування
```go
// Для стабільності при змінному навантаженні:
type AdaptiveBuffer struct {
    mu           sync.Mutex
    data         []byte
    targetSize   int
    maxSize      int
    fillRatio    float64  // 0.0-1.0
}

func (ab *AdaptiveBuffer) Write(chunk []byte) error {
    ab.mu.Lock()
    defer ab.mu.Unlock()
    
    // 🎯 Динамічне обмеження за fillRatio
    if len(ab.data)+len(chunk) > int(float64(ab.maxSize)*ab.fillRatio) {
        return fmt.Errorf("buffer near capacity, applying backpressure")
    }
    
    ab.data = append(ab.data, chunk...)
    
    // 🎯 Адаптація: якщо буфер часто переповнюється → збільшити targetSize
    if len(ab.data) > ab.targetSize {
        ab.targetSize = min(ab.targetSize*2, ab.maxSize)
    }
    
    return nil
}

func (ab *AdaptiveBuffer) ReadAndClear() []byte {
    ab.mu.Lock()
    defer ab.mu.Unlock()
    
    result := append([]byte(nil), ab.data...)  // Копія
    ab.data = ab.data[:0]  // Швидке очищення
    
    // 🎯 Адаптація: якщо буфер часто порожній → зменшити targetSize
    if len(result) < ab.targetSize/4 {
        ab.targetSize = max(ab.targetSize/2, 1024*1024)  // Мін. 1MB
    }
    
    return result
}
```

---

## 🧪 Приклад: рефакторинг з кращою структурою

```go
// ✅ Рефакторинг головного циклу з обробкою помилок:
func (p *TSStreamProcessor) runParserLoop(ctx context.Context) error {
    state := StateInitial
    var pmtPID mpeg2ts.PID
    var elementaryPID mpeg2ts.PID
    continuity := make(map[mpeg2ts.PID]byte)
    
    for {
        select {
        case pkt, ok := <-p.tsPacketChan:
            if !ok {
                return fmt.Errorf("TS packet channel closed unexpectedly")
            }
            
            // 🎯 State machine з явною обробкою помилок
            switch state {
            case StateInitial:
                if pkt.PID == mpeg2ts.PID_PAT {
                    pat, err := pkt.ParsePAT()
                    if err != nil {
                        p.logger.Printf("PAT parse error (recoverable): %v", err)
                        continue
                    }
                    if pid := findProgramMapPID(pat); pid != 0 {
                        pmtPID = pid
                        state = StateWaitingPMT
                        p.logger.Infof("Found PMT PID: 0x%04X", pid)
                    }
                }
                
            case StateWaitingPMT:
                if pkt.PID == pmtPID {
                    pmt, err := pkt.ParsePMT(p.cfg.DisableCRC)
                    if err != nil {
                        p.logger.Printf("PMT parse error: %v", err)
                        continue
                    }
                    if pid := findVideoElementaryPID(pmt, p.cfg.PreferredCodec); pid != 0 {
                        elementaryPID = pid
                        state = StateProcessingPES
                        p.logger.Infof("Found video PID: 0x%04X", pid)
                    }
                }
                
            case StateProcessingPES:
                if pkt.PID == elementaryPID {
                    if err := p.pesParser.EnqueueTSPacket(pkt); err != nil {
                        p.logger.Printf("PES enqueue error: %v", err)
                        // Опціонально: скинути парсер при критичних помилках
                    }
                }
            }
            
            // 🎯 Continuity check з метриками
            if err := checkContinuity(continuity, pkt); err != nil {
                p.metrics.ContinuityErrors.Inc()
                p.logger.Debugf("Continuity error: %v", err)
            }
            
        case <-ctx.Done():
            p.logger.Info("Parser loop terminated by context")
            return nil
        }
    }
}
```

---

## 📋 Best Practices для TS-парсингу у production

```
✅ Обробка помилок:
   • Ніколи не ігнорувати помилки парсингу PAT/PMT/PES
   • Дозволити відновлення після тимчасових помилок
   • Логувати з достатнім контекстом для дебагу

✅ Управління пам'яттю:
   • Обмежувати розмір inBuffer з backpressure
   • Використовувати sync.Pool для reuse буферів
   • Моніторити memory usage у Prometheus

✅ Конкурентність:
   • Використовувати WaitGroup для граціозного завершення
   • Уникати глобальних змінних — ін'єктувати залежності
   • Тестувати race condition: go test -race

✅ Моніторинг:
   • Збирати метрики: continuity errors, bitrate, buffer fill
   • Інтегрувати з Prometheus/Grafana для alerting
   • Логувати аномалії для подальшого аналізу

✅ Конфігурація:
   • Винести налаштування у CLI args або config file
   • Дозволити гаряче перезавантаження конфігурації
   • Документувати вплив параметрів на продуктивність

✅ Тестування:
   • Додати юніт-тести для state machine
   • Покрити edge cases: пошкоджені пакети, незвичні PID
   • Додати інтеграційні тести з реальними фікстурами
```

---

## 🎯 Висновок

Ця програма — **потужний прототип** для стрімінг-парсингу MPEG-2 TS:

✅ Правильна архітектура: TSEngine → State Machine → PESParser  
✅ Використання context для скасування та атоміків для статистики  
✅ Підтримка двох джерел входу (UDP/file)

**Критичні виправлення перед продакшеном**:

1. ✅ **Додати backpressure** для inBuffer (обмеження розміру + відкидання)
2. ✅ **Обробляти помилки** ParsePAT/ParsePMT/EnqueueTSPacket, а не ігнорувати
3. ✅ **Замінити глобальні змінні** на структуру конфігурації + ін'єкцію залежностей
4. ✅ **Додати граціозне завершення** з WaitGroup та закриттям ресурсів
5. ✅ **Інтегрувати PES-вихід** у ваш існуючий pipeline, а не тільки рахувати байти
6. ✅ **Додати метрики для Prometheus** замість тільки логування

**Приклад інтеграції у ваш CCTV pipeline**:
```go
// 🎯 TSReceiver для вашого WebSocket-сервера:
type TSReceiver struct {
    channelID  string
    processor  *TSStreamProcessor
    wsSender   *WSSender  // Ваш існуючий компонент для розсилки
}

func (r *TSReceiver) ProcessTSChunk(data []byte) error {
    // 🎯 Backpressure перевірка
    if !r.processor.CanAcceptMore() {
        return fmt.Errorf("receiver buffer full")
    }
    
    // 🎯 Енкодинг у inBuffer (аналогічно вашому startFileSrc)
    if err := r.processor.EnqueueTSData(data); err != nil {
        return fmt.Errorf("failed to enqueue TS data: %w", err)
    }
    
    return nil
}

// Використання:
func (h *WSHandler) onBinaryMessage(channelID string, data []byte) {
    receiver := h.getOrCreateTSReceiver(channelID)
    
    if err := receiver.ProcessTSChunk(data); err != nil {
        h.logger.Warn("TS processing failed", 
            "channel", channelID, 
            "error", err,
            "action", "dropping chunk")
        h.metrics.DroppedChunks.Inc()
    }
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом коду з backpressure та кращою обробкою помилок?
- 🧠 Інтеграцією PES-виходу у ваш `segmentAssembler` для fMP4-генерації?
- 🧪 Написанням тестів для state machine та continuity check?

Чекаю на ваші питання! 🛠️📡🎬