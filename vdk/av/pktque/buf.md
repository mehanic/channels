# 🔄 Глибокий розбір: pktque.Buf — Циклічний буфер для медіа-пакетів

Цей файл — **реалізація кільцевого буфера (ring buffer)** для зберігання `av.Packet` об'єктів у бібліотеці `vdk`. Він використовується для буферизації медіа-пакетів між демуксингом, фільтрацією та муксингом у медіа-пайплайнах.

Розберемо архітектуру, алгоритми та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема Buf

```
┌────────────────────────────────────────┐
│ 📦 pktque.Buf — Ring Buffer Design     │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • pkts[] — масив пакетів (потужність 2ⁿ)│
│  • Head/Tail — позиції читання/запису  │
│  • Size/Count — метрики буфера         │
│                                         │
│  🔄 Кільцева логіка:                    │
│  • Індексація: pos & (len-1)           │
│  • Авто-збільшення: grow() ×2          │
│  • Безпека: паніка при Pop() з пустої  │
│                                         │
│  📊 Метрики:                            │
│  • Count — кількість пакетів           │
│  • Size — загальний розмір даних (байт)│
│  • Capacity — len(pkts)                │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Структура даних: Buf та BufPos

### Основна структура:

```go
type Buf struct {
    Head, Tail BufPos    // позиції читання/запису
    pkts       []av.Packet // масив пакетів (потужність 2ⁿ)
    Size       int        // загальний розмір даних у байтах
    Count      int        // кількість пакетів у буфері
}
```

### 🎯 Поля та їх призначення:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `Head` | `BufPos` | Позиція наступного читання (Pop) | `Head=5` → читаємо з індексу 5 |
| `Tail` | `BufPos` | Позиція наступного запису (Push) | `Tail=10` → пишемо в індекс 10 |
| `pkts` | `[]av.Packet` | Циклічний масив даних | `make([]av.Packet, 64)` |
| `Size` | `int` | Сума `len(pkt.Data)` усіх пакетів | `10 пакетів × 1400 байт = 14000` |
| `Count` | `int` | Активна кількість пакетів | `Tail - Head = 5` |

### BufPos — тип-обгортка з операторами порівняння:

```go
type BufPos int

// Порівняння без переповнення (overflow-safe)
func (self BufPos) LT(pos BufPos) bool { return self-pos < 0 }  // <
func (self BufPos) GE(pos BufPos) bool { return self-pos >= 0 } // >=
func (self BufPos) GT(pos BufPos) bool { return self-pos > 0 }  // >
```

> 💡 **Чому `self-pos` замість `self < pos`?**  
> При переповненні `int` (напр. `Head` досягає `math.MaxInt`), пряме порівняння дасть хибний результат. Віднімання зберігає коректну відносну різницю.

---

## 🔄 2. Кільцева індексація: битові операції замість `%`

### Ключова оптимізація:

```go
// Замість повільного модуля:
index := pos % len(self.pkts)

// Швидка бітова маска (працює тільки якщо len = 2ⁿ):
index := int(pos) & (len(self.pkts) - 1)
```

### 🔢 Приклад роботи:

```
Масив: len=64 (2⁶), маска = 63 = 0b111111

pos=5   → 5 & 63 = 5   ✓
pos=63  → 63 & 63 = 63 ✓
pos=64  → 64 & 63 = 0  ✓ (перехід на початок!)
pos=127 → 127 & 63 = 63 ✓
pos=128 → 128 & 63 = 0 ✓ (знову початок)
```

### ✅ Ваш use-case: чому це важливо для CCTV:

```go
// У реальному часі ми обробляємо тисячі пакетів/секунду
// Бітова маска ~1-2 цикли CPU, модуль ~10-20 циклів
// Економія: 1000 пакетів/с × 18 циклів = 18000 циклів/с = ~5µs на ядро

// Це критично для low-latency HLS сегментації:
// • Менше затримка = менший буфер = швидший старт відтворення
// • Менше CPU = більше каналів на одному сервері
```

---

## ➕ 3. Push() — додавання пакету у буфер

```go
func (self *Buf) Push(pkt av.Packet) {
    // 1. Перевірка повноти → авто-збільшення
    if self.Count == len(self.pkts) {
        self.grow()  // ×2 розмір масиву
    }
    
    // 2. Запис за кільцевим індексом
    self.pkts[int(self.Tail)&(len(self.pkts)-1)] = pkt
    
    // 3. Оновлення метрик
    self.Tail++
    self.Count++
    self.Size += len(pkt.Data)  // важливо для backpressure!
}
```

### 🔍 Деталі `grow()`:

```go
func (self *Buf) grow() {
    // Новий масив ×2 більший
    newpkts := make([]av.Packet, len(self.pkts)*2)
    
    // Копіювання активних елементів (від Head до Tail)
    for i := self.Head; i.LT(self.Tail); i++ {
        // Кільцева індексація для старого та нового масивів
        oldIdx := int(i) & (len(self.pkts)-1)
        newIdx := int(i) & (len(newpkts)-1)
        newpkts[newIdx] = self.pkts[oldIdx]
    }
    
    // Перемикання посилання
    self.pkts = newpkts
    // Head/Tail залишаються незмінними (відносні позиції)
}
```

### ⚠️ Важливо: 
- `grow()` — O(n) операція, уникайте частих викликів
- Початковий розмір 64 обрано як компроміс між пам'яттю та частотою росту
- Для CCTV з високим бітрейтом можна збільшити до 256/512

### ✅ Ваш use-case: backpressure на основі `Size`:

```go
// У вашому pipeline: пауза полігу при переповненні буфера
func (p *VideoQueue) ShouldPause() bool {
    // Пауза якщо буфер > 50MB або > 1000 пакетів
    return p.buf.Size > 50*1024*1024 || p.buf.Count > 1000
}

// Інтеграція у цикл обробки:
for {
    if p.ShouldPause() {
        log.Debug("backpressure: pausing video poll", 
            "size_mb", p.buf.Size/1024/1024,
            "count", p.buf.Count)
        time.Sleep(100 * time.Millisecond)
        continue
    }
    
    pkt, err := p.demuxer.ReadPacket()
    if err != nil { /* ... */ }
    
    p.buf.Push(pkt)  // додаємо у буфер
}
```

---

## ➖ 4. Pop() — отримання пакету з буфера

```go
func (self *Buf) Pop() av.Packet {
    // 1. Перевірка порожнечі → паніка (програмістська помилка)
    if self.Count == 0 {
        panic("pktque.Buf: Pop() when count == 0")
    }
    
    // 2. Читання за кільцевим індексом
    i := int(self.Head) & (len(self.pkts) - 1)
    pkt := self.pkts[i]
    
    // 3. Очищення посилання (допомога GC)
    self.pkts[i] = av.Packet{}
    
    // 4. Оновлення метрик
    self.Size -= len(pkt.Data)
    self.Head++
    self.Count--
    
    return pkt
}
```

### 🔑 Ключові моменти:

| Операція | Навіщо | Наслідки якщо пропустити |
|----------|--------|-------------------------|
| `panic` при `Count==0` | Виявлення логічних помилок | Тихе повернення нульового пакету → складний дебаг |
| `self.pkts[i] = av.Packet{}` | Допомога garbage collector | Пам'ять не звільняється → memory leak при великих пакетах |
| `Size -= len(pkt.Data)` | Коректний backpressure | `ShouldPause()` завжди хибний → переповнення пам'яті |

### ✅ Ваш use-case: безпечний Pop з обробкою помилок:

```go
// SafePop — версія без паніки для production
func (p *VideoQueue) SafePop() (av.Packet, error) {
    if p.buf.Count == 0 {
        return av.Packet{}, io.EOF  // або ваш custom error
    }
    return p.buf.Pop(), nil
}

// Використання у основному циклі:
for {
    pkt, err := p.SafePop()
    if err == io.EOF {
        // Буфер порожній — можна заснути або перевірити нові дані
        time.Sleep(10 * time.Millisecond)
        continue
    }
    if err != nil {
        log.Error("pop failed", "err", err)
        continue
    }
    
    // Обробка пакету
    p.processPacket(pkt)
}
```

---

## 🔍 5. Get() та IsValidPos() — доступ без видалення

```go
// Get — читання пакету за позицією (без Pop)
func (self *Buf) Get(pos BufPos) av.Packet {
    return self.pkts[int(pos)&(len(self.pkts)-1)]
}

// IsValidPos — перевірка чи позиція у межах активних даних
func (self *Buf) IsValidPos(pos BufPos) bool {
    return pos.GE(self.Head) && pos.LT(self.Tail)
}
```

### ✅ Ваш use-case: пошук ключових кадрів у буфері

```go
// FindLastKeyFrame — пошук останнього I-frame у буфері
func (p *VideoQueue) FindLastKeyFrame() *av.Packet {
    // Ітеруємо від кінця до початку буфера
    for pos := p.buf.Tail - 1; pos.GE(p.buf.Head); pos-- {
        if !p.buf.IsValidPos(pos) {
            continue
        }
        pkt := p.buf.Get(pos)
        
        // Перевірка чи це відео-потік та ключовий кадр
        if pkt.Idx == p.videoStreamIdx && pkt.IsKeyFrame {
            return &pkt  // повертаємо посилання (копіювати не потрібно)
        }
    }
    return nil  // не знайдено
}

// Використання при розриві потоку:
if p.detectedStreamGap() {
    if keyframe := p.FindLastKeyFrame(); keyframe != nil {
        // Відправляємо ключовий кадр для швидкого відновлення плеєра
        p.sendKeyFrameToClients(*keyframe)
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// video_queue.go — буферизація відео-пакетів для HLS сегментації
type VideoQueue struct {
    channelID      string
    buf            *pktque.Buf
    demuxer        av.Demuxer
    videoStreamIdx int
    audioStreamIdx int
    segmentDuration time.Duration
    metrics        *QueueMetrics
}

func NewVideoQueue(channelID string, demuxer av.Demuxer) *VideoQueue {
    return &VideoQueue{
        channelID:      channelID,
        buf:            pktque.NewBuf(),  // початковий розмір 64
        demuxer:        demuxer,
        videoStreamIdx: -1,  // визначається при Streams()
        segmentDuration: 10 * time.Second,
        metrics:        NewQueueMetrics(channelID),
    }
}

// StartPolling — фонове читання пакетів з демуксера
func (q *VideoQueue) StartPolling(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(10 * time.Millisecond)
        defer ticker.Stop()
        
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                // Backpressure: пауза якщо буфер переповнений
                if q.buf.Size > 100*1024*1024 {  // 100MB ліміт
                    q.metrics.BackpressureTriggered.Inc()
                    continue
                }
                
                // Читання пакету
                pkt, err := q.demuxer.ReadPacket()
                if err == io.EOF {
                    log.Printf("Channel %s: EOF reached", q.channelID)
                    return
                }
                if err != nil {
                    log.Warn("read packet failed", "err", err)
                    continue
                }
                
                // Визначення індексів потоків при першому пакеті
                if q.videoStreamIdx == -1 {
                    q.discoverStreamIndices()
                }
                
                // Додавання у буфер
                q.buf.Push(pkt)
                q.metrics.PacketsReceived.Inc()
                q.metrics.BufferSizeBytes.Set(float64(q.buf.Size))
            }
        }
    }()
}

// GetNextSegment — отримання пакетів для одного HLS-сегменту
func (q *VideoQueue) GetNextSegment() ([]av.Packet, error) {
    var packets []av.Packet
    segmentStart := time.Now()
    
    // Збираємо пакети протягом segmentDuration
    for time.Since(segmentStart) < q.segmentDuration {
        if q.buf.Count == 0 {
            // Буфер порожній — чекаємо трохи
            time.Sleep(5 * time.Millisecond)
            continue
        }
        
        pkt := q.buf.Pop()
        packets = append(packets, pkt)
        
        // Якщо це ключовий кадр і ми вже маємо достатньо даних — можна завершити
        if pkt.IsKeyFrame && len(packets) > 10 {
            break
        }
    }
    
    q.metrics.SegmentsGenerated.Inc()
    return packets, nil
}

// discoverStreamIndices — визначення індексів відео/аудіо потоків
func (q *VideoQueue) discoverStreamIndices() {
    streams, err := q.demuxer.Streams()
    if err != nil {
        log.Warn("failed to get streams", "err", err)
        return
    }
    
    for i, s := range streams {
        switch s.Type() {
        case av.H264, av.H265:
            q.videoStreamIdx = i
            log.Printf("Channel %s: video stream at idx %d", q.channelID, i)
        case av.AAC, av.MP3:
            q.audioStreamIdx = i
            log.Printf("Channel %s: audio stream at idx %d", q.channelID, i)
        }
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка "Pop() when count == 0"** | Логічна помилка: Pop без попередньої перевірки | Використовуйте `SafePop()` або перевіряйте `buf.Count > 0` перед Pop |
| **Пам'ять росте без обмежень** | Великі пакети + відсутній backpressure | Моніторьте `buf.Size` та паузуйте полігу при перевищенні ліміту |
| **Затримка зростає з часом** | `grow()` викликається занадто часто | Збільште початковий розмір буфера: `pkts: make([]av.Packet, 256)` |
| **GC тиск через великі пакети** | `av.Packet.Data` — великі []byte | Очищуйте посилання після Pop: `self.pkts[i] = av.Packet{}` (вже реалізовано) |
| **Переповнення BufPos** | Дуже довга робота без перезапуску (>2³¹ пакетів) | Періодично скидайте буфер або використовуйте `int64` для `BufPos` |

---

## ⚡ Оптимізації для real-time обробки

### 1. Попереднє виділення буфера:

```go
// Замість NewBuf() з 64:
func NewPreallocatedBuf(capacity int) *pktque.Buf {
    // Округлення до найближчої степені 2 для ефективної маски
    capacity = 1 << (32 - bits.LeadingZeros32(uint32(capacity-1)))
    
    return &pktque.Buf{
        pkts: make([]av.Packet, capacity),
    }
}

// Для CCTV з високим бітрейтом:
q.buf = NewPreallocatedBuf(512)  // 512 пакетів замість 64
```

### 2. Пакетний Pop для зменшення накладних витрат:

```go
// PopBatch — отримання кількох пакетів за один виклик
func (q *VideoQueue) PopBatch(count int) []av.Packet {
    available := q.buf.Count
    if count > available {
        count = available
    }
    
    packets := make([]av.Packet, 0, count)
    for i := 0; i < count; i++ {
        packets = append(packets, q.buf.Pop())
    }
    return packets
}
```

### 3. Моніторинг стану буфера:

```go
type QueueMetrics struct {
    BufferSizeBytes   prometheus.Gauge
    BufferCount       prometheus.Gauge
    BackpressureCount prometheus.Counter
    // ...
}

func (q *VideoQueue) ReportMetrics() {
    q.metrics.BufferSizeBytes.Set(float64(q.buf.Size))
    q.metrics.BufferCount.Set(float64(q.buf.Count))
    
    // Попередження при високому завантаженні
    if q.buf.Count > 400 {  // 400/512 = 78%
        q.metrics.BufferHighWatermark.Inc()
    }
}
```

---

## 📋 Чек-лист інтеграції pktque.Buf

```go
// ✅ 1. Ініціалізація буфера з адекватним розміром
buf := pktque.NewBuf()  // 64 за замовчуванням
// або для високого навантаження:
buf := &pktque.Buf{pkts: make([]av.Packet, 256)}

// ✅ 2. Backpressure на основі Size/Count
if buf.Size > 50*1024*1024 || buf.Count > 500 {
    // Пауза або скидання старих пакетів
    time.Sleep(100 * time.Millisecond)
}

// ✅ 3. Безпечний Pop з перевіркою
if buf.Count > 0 {
    pkt := buf.Pop()
    processPacket(pkt)
}

// ✅ 4. Очищення посилань після Pop (вже реалізовано в бібліотеці)
// Але якщо використовуєте кастомний Pop — не забудьте:
// self.pkts[i] = av.Packet{}

// ✅ 5. Моніторинг метрик
monitoring.BufferSize.Set(float64(buf.Size))
monitoring.BufferCount.Set(float64(buf.Count))

// ✅ 6. Обробка розривів: пошук ключових кадрів
if detectedGap {
    if keyframe := findLastKeyFrame(buf); keyframe != nil {
        sendKeyFrame(*keyframe)
    }
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk pktque Package](https://pkg.go.dev/github.com/deepch/vdk/av/pktque) — GoDoc documentation
- 📄 [Ring Buffer Wikipedia](https://en.wikipedia.org/wiki/Circular_buffer) — теоретична основа
- 🎬 [HLS Segment Buffering](https://developer.apple.com/documentation/http_live_streaming/about_the_http_live_streaming_protocol) — як буферизація впливає на HLS
- 🧪 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади використання pktque

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Моніторьте `buf.Size` а не тільки `Count`** — один великий відео-кадр може бути 100KB+, тому `Count=100` не завжди означає "мало даних".
> 2. **Налаштуйте backpressure на рівні каналу** — різні камери мають різний бітрейт, універсальні ліміти не працюють.
> 3. **Використовуйте `FindLastKeyFrame()` при розривах** — це прискорює відновлення відтворення у клієнтів.
> 4. **Збільште початковий розмір буфера для 4K потоків** — 64 пакети можуть заповнитися за <100мс при високому бітрейті.
> 5. **Додайте метрики для `grow()` викликів** — часте збільшення буфера вказує на неправильний початковий розмір.

Потрібен приклад інтеграції `VideoQueue` з вашим `segmentAssembler` для синхронізації відео/аудіо/субтитрів перед створенням HLS-сегментів? Готовий допомогти! 🚀