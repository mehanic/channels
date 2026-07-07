# 📦 Глибокий розбір: `fragment.Fragment` та `Fragmenter` — Інтерфейс для фрагментації медіа

Цей файл — **визначення інтерфейсу для фрагментації медіа-потоків** у форматі, сумісному з бібліотекою `vdk`. Він надає абстракцію для створення фрагментованих медіа-файлів (fMP4, WebM тощо) з підтримкою adaptive bitrate streaming.

---

## 🗺️ Архітектурна схема фрагментації

```
┌────────────────────────────────────────┐
│ 📦 fragment — Media Fragmentation API │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Fragment struct — один медіа-фрагмент│
│  • Fragmenter interface — інтерфейс фрагментатора│
│  • av.PacketWriter — стандартний інтерфейс запису│
│                                         │
│  🔄 Потік фрагментації:                 │
│  av.Packet → Fragmenter → Fragment    │
│  → HTTP/WebSocket → Client            │
│                                         │
│  📡 Використання:                       │
│  • HLS fMP4 сегменти                  │
│  • DASH сегменти                      │
│  • Low-latency streaming              │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Fragment — структура одного медіа-фрагменту

### 🔧 Структура та призначення:

```go
type Fragment struct {
    Bytes       []byte        // ⭐ сирий бінарний контент фрагменту
    Length      int           // ⭐ довжина контенту у байтах (може бути < len(Bytes))
    Independent bool          // ⭐ чи можна почати відтворення з цього фрагменту
    Duration    time.Duration // ⭐ тривалість фрагменту
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `Bytes` | `[]byte` | **Критично**: сирий бінарний контент фрагменту (fMP4 moof+mdat, WebM Cluster) | `[]byte{0x00, 0x00, 0x00, 0x18, 0x6d, 0x6f, 0x6f, 0x66, ...}` |
| `Length` | `int` | **Критично**: фактична довжина контенту (може бути < cap(Bytes) для оптимізації) | `1048576` = 1 MB |
| `Independent` | `bool` | **Критично**: чи можна почати відтворення з цього фрагменту (наявність ключового кадру) | `true` = фрагмент починається з IDR frame |
| `Duration` | `time.Duration` | **Критично**: тривалість фрагменту для клієнтського буферування | `4 * time.Second` = 4-секундний сегмент |

### 🔍 Чому `Length` окремо від `len(Bytes)`?

```
Оптимізація пам'яті через reuse буферів:

• Bytes може мати cap > Length для уникнення аллокацій
• Length вказує скільки байт дійсно валідні у Bytes
• Клієнт повинен використовувати Bytes[:Length] а не весь Bytes

Приклад:
    frag := Fragment{
        Bytes: make([]byte, 0, 2*1024*1024),  // cap = 2MB для reuse
        Length: 1048576,  // фактично використано 1MB
        // ...
    }
    
    // Правильне використання:
    writer.Write(frag.Bytes[:frag.Length])
    
    // Неправильне (може записати сміття):
    writer.Write(frag.Bytes)  // запише 2MB замість 1MB!
```

### ✅ Ваш use-case**: відправка фрагменту через WebSocket

```go
// SendFragmentOverWS — відправка фрагменту через WebSocket з коректною довжиною
func SendFragmentOverWS(conn *websocket.Conn, frag *fragment.Fragment) error {
    if frag == nil || frag.Length == 0 {
        return nil  // порожній фрагмент
    }
    
    // Використання тільки валідної частини буфера
    data := frag.Bytes[:frag.Length]
    
    // Опціонально: додавання метаданих у заголовок повідомлення
    header := FragmentHeader{
        Duration:    frag.Duration.Milliseconds(),
        Independent: frag.Independent,
        Timestamp:   time.Now().UnixMilli(),
    }
    
    // Серіалізація заголовку + даних
    headerBytes, err := header.Marshal()
    if err != nil {
        return fmt.Errorf("marshal header: %w", err)
    }
    
    // Відправка у одному повідомленні
    message := append(headerBytes, data...)
    return conn.WriteMessage(websocket.BinaryMessage, message)
}

type FragmentHeader struct {
    Duration    int64 // у мілісекундах
    Independent bool
    Timestamp   int64 // Unix milliseconds
}

func (h FragmentHeader) Marshal() ([]byte, error) {
    buf := make([]byte, 17)  // 8+1+8 байт
    binary.LittleEndian.PutUint64(buf[0:8], uint64(h.Duration))
    if h.Independent {
        buf[8] = 1
    }
    binary.LittleEndian.PutUint64(buf[9:17], uint64(h.Timestamp))
    return buf, nil
}
```

---

## 🔑 2. Fragmenter interface — інтерфейс фрагментатора

### 🔧 Інтерфейс та призначення:

```go
type Fragmenter interface {
    av.PacketWriter      // стандартний інтерфейс запису пакетів
    Fragment() (Fragment, error)  // отримання наступного готового фрагменту
    Duration() time.Duration      // загальна тривалість записаного контенту
    TimeScale() uint32           // timeScale для конвертації таймінгів
    MovieHeader() (filename, contentType string, contents []byte)  // init segment
    NewSegment()                 // початок нового фрагменту
}
```

### 🔍 Призначення методів:

| Метод | Повертає | Призначення | Приклад використання |
|-------|----------|-------------|---------------------|
| `WritePacket(av.Packet)` | `error` | Запис медіа-пакету у фрагментатор | Основний цикл демуксингу |
| `Fragment()` | `(Fragment, error)` | Отримання готового фрагменту для відправки | Після накопичення достатньо даних |
| `Duration()` | `time.Duration` | Загальна тривалість записаного контенту | Для логування, метрик, клієнтського прогресу |
| `TimeScale()` | `uint32` | TimeScale для конвертації ticks → time.Duration | `duration = time.Duration(ticks) * time.Second / time.Duration(timeScale)` |
| `MovieHeader()` | `(filename, contentType, contents)` | Генерація init segment для клієнта | Перше повідомлення у streaming сесії |
| `NewSegment()` | `void` | Сигнал про початок нового фрагменту | Для low-latency streaming або ручного контролю |

### 🔍 Життєвий цикл фрагментатора:

```
1. Ініціалізація:
   • Створення фрагментатора з параметрами (кодек, timeScale, duration)
   • Генерація MovieHeader() для клієнта (init segment)

2. Запис пакетів:
   • WritePacket() приймає av.Packet з демуксера
   • Внутрішня буферизація та фрагментація

3. Отримання фрагментів:
   • Коли накопичено достатньо даних → Fragment() повертає готовий фрагмент
   • Або виклик NewSegment() для примусового завершення поточного фрагменту

4. Повторення:
   • Продовження запису пакетів у новий фрагмент
   • Цикл до завершення потоку або помилки
```

### ✅ Ваш use-case**: основний цикл streaming сервера

```go
// StreamMedia — основний цикл медіа-стрімінгу через WebSocket
func StreamMedia(ctx context.Context, demuxer av.Demuxer, fragmenter fragment.Fragmenter, conn *websocket.Conn) error {
    // 1. Відправка init segment клієнту
    filename, contentType, initBytes := fragmenter.MovieHeader()
    initFrag := fragment.Fragment{
        Bytes:    initBytes,
        Length:   len(initBytes),
        Independent: true,  // init segment завжди незалежний
        Duration: 0,        // init segment не має тривалості
    }
    if err := SendFragmentOverWS(conn, &initFrag); err != nil {
        return fmt.Errorf("send init segment: %w", err)
    }
    
    // 2. Основний цикл запису пакетів
    ticker := time.NewTicker(100 * time.Millisecond)  // періодична перевірка фрагментів
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        default:
            // Читання пакету з демуксера
            pkt, err := demuxer.ReadPacket()
            if err == io.EOF {
                return nil  // нормальне завершення
            }
            if err != nil {
                return fmt.Errorf("read packet: %w", err)
            }
            
            // Запис пакету у фрагментатор
            if err := fragmenter.WritePacket(pkt); err != nil {
                return fmt.Errorf("write packet: %w", err)
            }
            
            // Періодична перевірка наявності готових фрагментів
            select {
            case <-ticker.C:
                for {
                    frag, err := fragmenter.Fragment()
                    if err != nil {
                        if err == io.EOF {
                            break  // немає готових фрагментів
                        }
                        return fmt.Errorf("get fragment: %w", err)
                    }
                    
                    // Відправка фрагменту клієнту
                    if err := SendFragmentOverWS(conn, &frag); err != nil {
                        return fmt.Errorf("send fragment: %w", err)
                    }
                    
                    // Логування метрик
                    log.Printf("Sent fragment: %d bytes, %v duration, independent=%v", 
                        frag.Length, frag.Duration, frag.Independent)
                }
            default:
                // Продовження циклу
            }
        }
    }
}
```

---

## 🔑 3. Інтеграція з av.PacketWriter

### 🔧 av.PacketWriter інтерфейс:

```go
// З бібліотеки vdk/av
type PacketWriter interface {
    WritePacket(pkt Packet) error
}
```

### 🔍 Призначення:

```
av.PacketWriter — стандартний інтерфейс для запису медіа-пакетів:

• Дозволяє уніфіковану обробку пакетів незалежно від формату контейнера
• Fragmenter реалізує цей інтерфейс для сумісності з існуючим кодом
• WritePacket() може буферизувати пакети до завершення фрагменту

Типовий av.Packet:
    type Packet struct {
        IsKeyFrame   bool          // чи це ключовий кадр
        Idx          int           // індекс треку
        Time         time.Duration // PTS у time.Duration
        Duration     time.Duration // тривалість семплу
        CompositionTime time.Duration // CTS для B-frames
        Data         []byte        // сирий медіа-контент (NALU, AAC frame, тощо)
        CodecData    CodecData     // метадані кодека (опціонально)
    }
```

### ✅ Ваш use-case**: фільтрація пакетів перед записом

```go
// FilteringFragmenter — обгортка фільтрації пакетів перед записом у фрагментатор
type FilteringFragmenter struct {
    base     fragment.Fragmenter
    videoIdx int
    minKeyFrameInterval time.Duration
    lastKeyFrameTime   time.Duration
}

func NewFilteringFragmenter(base fragment.Fragmenter, videoIdx int, minInterval time.Duration) *FilteringFragmenter {
    return &FilteringFragmenter{
        base:     base,
        videoIdx: videoIdx,
        minKeyFrameInterval: minInterval,
    }
}

func (f *FilteringFragmenter) WritePacket(pkt av.Packet) error {
    // Фільтрація відео пакетів: пропуск неключових кадрів занадто близько до попереднього ключового
    if pkt.Idx == f.videoIdx && !pkt.IsKeyFrame {
        if pkt.Time - f.lastKeyFrameTime < f.minKeyFrameInterval {
            // Пропускаємо пакет для зменшення бітрейту
            return nil
        }
    }
    
    // Оновлення часу останнього ключового кадру
    if pkt.IsKeyFrame {
        f.lastKeyFrameTime = pkt.Time
    }
    
    // Запис у базовий фрагментатор
    return f.base.WritePacket(pkt)
}

// Делегування інших методів інтерфейсу
func (f *FilteringFragmenter) Fragment() (fragment.Fragment, error) {
    return f.base.Fragment()
}
func (f *FilteringFragmenter) Duration() time.Duration {
    return f.base.Duration()
}
func (f *FilteringFragmenter) TimeScale() uint32 {
    return f.base.TimeScale()
}
func (f *FilteringFragmenter) MovieHeader() (string, string, []byte) {
    return f.base.MovieHeader()
}
func (f *FilteringFragmenter) NewSegment() {
    f.base.NewSegment()
}

// Використання для adaptive bitrate:
fragmenter := NewFilteringFragmenter(
    baseFragmenter,
    0,  // video track index
    2*time.Second,  // мінімум 2 секунди між ключовими кадрами
)
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Реалізація fMP4 фрагментатора

```go
// FMP4Fragmenter — реалізація Fragmenter для fMP4 формату
type FMP4Fragmenter struct {
    timeScale uint32
    duration  time.Duration
    
    // Внутрішній стан
    tracks      []*TrackState
    currentSeq  uint32
    buffer      *bytes.Buffer
    readyFrags  chan fragment.Fragment
}

type TrackState struct {
    idx         int
    codecData   av.CodecData
    lastDTS     time.Duration
    sampleCount int
}

func NewFMP4Fragmenter(timeScale uint32, tracks []av.CodecData) *FMP4Fragmenter {
    f := &FMP4Fragmenter{
        timeScale: timeScale,
        tracks:    make([]*TrackState, len(tracks)),
        buffer:    &bytes.Buffer{},
        readyFrags: make(chan fragment.Fragment, 2),  // буфер для асинхронної відправки
    }
    
    for i, codec := range tracks {
        f.tracks[i] = &TrackState{
            idx:       i,
            codecData: codec,
        }
    }
    
    return f
}

func (f *FMP4Fragmenter) WritePacket(pkt av.Packet) error {
    // Буферизація пакету у внутрішній формат
    // ... реалізація запису у fMP4 формат ...
    
    // Перевірка чи можна завершити поточний фрагмент
    if f.shouldFlushFragment(pkt) {
        frag, err := f.buildFragment()
        if err != nil {
            return err
        }
        f.readyFrags <- frag
    }
    
    return nil
}

func (f *FMP4Fragmenter) Fragment() (fragment.Fragment, error) {
    select {
    case frag := <-f.readyFrags:
        return frag, nil
    default:
        return fragment.Fragment{}, io.EOF  // немає готових фрагментів
    }
}

func (f *FMP4Fragmenter) MovieHeader() (string, string, []byte) {
    // Генерація fMP4 init segment (ftyp + moov)
    initBytes := generateFMP4InitSegment(f.timeScale, f.tracks)
    return "init.mp4", "video/mp4; codecs=\"avc1.42e01e,mp4a.40.2\"", initBytes
}

// Інші методи інтерфейсу...
```

### 🔧 Приклад: Low-latency streaming з ручним контролем фрагментів

```go
// LowLatencyStreamer — стрімінг з мінімальною затримкою через ручний контроль фрагментів
type LowLatencyStreamer struct {
    fragmenter fragment.Fragmenter
    conn       *websocket.Conn
    targetLatency time.Duration
}

func (s *LowLatencyStreamer) Stream(ctx context.Context, demuxer av.Demuxer) error {
    // Відправка init segment
    _, _, initBytes := s.fragmenter.MovieHeader()
    s.sendFragment(fragment.Fragment{
        Bytes:    initBytes,
        Length:   len(initBytes),
        Independent: true,
    })
    
    // Основний цикл з низькою затримкою
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF { break }
        if err != nil { return err }
        
        if err := s.fragmenter.WritePacket(pkt); err != nil {
            return err
        }
        
        // Примусове завершення фрагменту кожні targetLatency
        if pkt.Time%s.targetLatency < pkt.Duration {
            s.fragmenter.NewSegment()
            
            // Відправка всіх готових фрагментів
            for {
                frag, err := s.fragmenter.Fragment()
                if err != nil { break }
                s.sendFragment(frag)
            }
        }
    }
    
    return nil
}

func (s *LowLatencyStreamer) sendFragment(frag fragment.Fragment) {
    // Асинхронна відправка без блокування основного циклу
    go func() {
        if err := SendFragmentOverWS(s.conn, &frag); err != nil {
            log.Printf("warning: send fragment failed: %v", err)
        }
    }()
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Некоректне використання Length** | Клієнт отримує сміття після валідних даних | Завжди використовуйте `Bytes[:Length]` замість всього `Bytes` |
| **Відсутність Independent прапорця** | Клієнт не може почати відтворення з середини потоку | Встановлюйте `Independent=true` тільки для фрагментів з ключовими кадрами |
| **Невірний розрахунок Duration** | Клієнтський буфер переповнюється або спорожняється | Конвертуйте ticks у time.Duration через `TimeScale()`: `duration = time.Duration(ticks) * time.Second / time.Duration(timeScale)` |
| **Блокування у Fragment()** | Основний цикл зависає очікуючи фрагмент | Використовуйте non-blocking select або буферизований канал для readyFrags |
| **Відсутній MovieHeader** | Клієнт не може ініціалізувати декодер | Завжди відправляйте init segment перед першим медіа-фрагментом |

---

## ⚡ Оптимізації для high-performance streaming

### 1. Reuse буферів для Fragment.Bytes:

```go
var fragmentBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір фрагменту: 1-4 MB для відео
        buf := make([]byte, 0, 4*1024*1024)
        return &buf
    },
}

func GetFragmentBuffer() *[]byte {
    return fragmentBufferPool.Get().(*[]byte)
}

func PutFragmentBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення пам'яті
    fragmentBufferPool.Put(b)
}

// Використання у фрагментаторі:
func (f *FMP4Fragmenter) buildFragment() (fragment.Fragment, error) {
    buf := GetFragmentBuffer()
    defer PutFragmentBuffer(buf)
    
    // Запис даних у buf...
    
    return fragment.Fragment{
        Bytes: *buf,  // передаємо посилання на буфер
        Length: buf.Len(),  // фактична довжина
        // ...
    }, nil
}
```

### 2. Асинхронна генерація фрагментів:

```go
type AsyncFragmenter struct {
    base     fragment.Fragmenter
    workChan chan av.Packet
    fragChan chan fragment.Fragment
    errChan  chan error
    done     chan struct{}
}

func NewAsyncFragmenter(base fragment.Fragmenter, workerCount int) *AsyncFragmenter {
    f := &AsyncFragmenter{
        base:     base,
        workChan: make(chan av.Packet, 100),
        fragChan: make(chan fragment.Fragment, 10),
        errChan:  make(chan error, 1),
        done:     make(chan struct{}),
    }
    
    // Запуск воркерів
    for i := 0; i < workerCount; i++ {
        go f.worker()
    }
    
    return f
}

func (f *AsyncFragmenter) worker() {
    for {
        select {
        case pkt := <-f.workChan:
            if err := f.base.WritePacket(pkt); err != nil {
                f.errChan <- err
                return
            }
            // Перевірка готовності фрагменту...
        case <-f.done:
            return
        }
    }
}

func (f *AsyncFragmenter) WritePacket(pkt av.Packet) error {
    select {
    case f.workChan <- pkt:
        return nil
    case err := <-f.errChan:
        return err
    case <-f.done:
        return fmt.Errorf("fragmenter closed")
    }
}
```

### 3. Моніторинг продуктивності фрагментації:

```go
type FragmenterMetrics struct {
    PacketsWritten prometheus.CounterVec
    FragmentsGenerated prometheus.CounterVec
    FragmentLatency prometheus.HistogramVec
    FragmentSizes prometheus.HistogramVec
}

func (m *FragmenterMetrics) RecordPacket(trackIdx int, size int) {
    m.PacketsWritten.WithLabelValues(fmt.Sprintf("track_%d", trackIdx)).Inc()
}

func (m *FragmenterMetrics) RecordFragment(duration time.Duration, size int, independent bool) {
    m.FragmentsGenerated.Inc()
    m.FragmentLatency.Observe(duration.Seconds())
    m.FragmentSizes.Observe(float64(size))
    if independent {
        m.FragmentsGenerated.WithLabelValues("independent").Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання Fragmenter

```go
// ✅ 1. Завжди використовуйте Bytes[:Length] для доступу до даних
data := frag.Bytes[:frag.Length]  // ✅ правильно
// data := frag.Bytes             // ❌ може включати сміття

// ✅ 2. Перевірка Independent перед початком відтворення
if !frag.Independent {
    log.Printf("warning: fragment not independent, client may not start playback")
}

// ✅ 3. Конвертація часу через TimeScale()
durationTicks := uint64(frag.Duration * time.Duration(fragmenter.TimeScale()) / time.Second)

// ✅ 4. Non-blocking отримання фрагментів
select {
case frag := <-fragmenter.Fragment():
    // обробка фрагменту
default:
    // немає готових фрагментів, продовження циклу
}

// ✅ 5. Відправка MovieHeader перед першим медіа-фрагментом
filename, contentType, initBytes := fragmenter.MovieHeader()
sendInitSegment(conn, filename, contentType, initBytes)

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Generated fragment: %d bytes, %v duration, independent=%v", 
    frag.Length, frag.Duration, frag.Independent)

// ✅ 7. Метрики для моніторингу
metrics.RecordFragment(frag.Duration, frag.Length, frag.Independent)
```

---

## 🔗 Корисні посилання

- 💻 [vdk av Package](https://pkg.go.dev/github.com/deepch/vdk/av) — інтерфейси для медіа-пакетів
- 📄 [ISO/IEC 23009-1:2022 (DASH)](https://www.iso.org/standard/79329.html) — стандарт для adaptive streaming
- 📄 [HLS fMP4 Specification](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 🧪 [Go sync.Pool Documentation](https://pkg.go.dev/sync#Pool) — ефективне управління пам'яттю
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди використовуйте `Bytes[:Length]`** — уникнення відправки сміттєвих даних клієнту.
> 2. **Коректно встановлюйте `Independent` прапорець** — забезпечення можливості seek та low-latency старту.
> 3. **Конвертуйте час через `TimeScale()`** — уникнення розсинхронізації таймінгів між сервером та клієнтом.
> 4. **Використовуйте non-blocking отримання фрагментів** — уникнення блокування основного циклу streaming.
> 5. **Відправляйте `MovieHeader` перед медіа-даними** — забезпечення коректної ініціалізації декодера на клієнті.

Потрібен приклад реалізації повного циклу фрагментації для HLS/DASH з підтримкою adaptive bitrate, або інтеграція `fragment.Fragmenter` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀