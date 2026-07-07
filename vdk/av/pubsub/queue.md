# 📡 Глибокий розбір: pubsub.Queue — Publisher-Subscriber модель для медіа-стрімінгу

Цей файл — **реалізація потокобезпечної черги пакетів** з підтримкою одного видавця (publisher) та багатьох підписників (subscribers) для мультиканального стрімінгу. Вона дозволяє розподіляти медіа-пакети від одного джерела (напр. CCTV камери) до кількох обробників (HLS-сегментатори, WebSocket-клієнти, архіватори) без дублювання читання з джерела.

---

## 🗺️ Архітектурна схема pubsub.Queue

```
┌────────────────────────────────────────┐
│ 📦 pubsub.Queue — Pub/Sub Buffer       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • pktque.Buf — кільцевий буфер пакетів│
│  • sync.RWMutex + sync.Cond — синхронізація│
│  • QueueCursor — незалежні курсори для читання│
│                                         │
│  🔄 Потік даних:                        │
│  ┌─────────┐                           │
│  │Publisher│                           │
│  └────┬────┘                           │
│       │ WritePacket()                  │
│       ▼                                │
│  ┌─────────┐                           │
│  │  Queue  │ ← buf (ring buffer)      │
│  └────┬────┘                           │
│       │ ReadPacket()                   │
│  ┌────┴────┬────┬────┐                │
│  ▼         ▼    ▼    ▼                │
│ [Cursor1][C2][C3]...[CursorN]         │
│  HLS     WS  Arch  Analytics          │
│                                         │
│  ⚙️  Управління буфером:               │
│  • maxgopcount — ліміт GOP у буфері   │
│  • Auto-shrink: видалення старих GOP  │
│  • Condition variable — сигналізація  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Queue — ядро Pub/Sub системи

### Структура та поля:

```go
type Queue struct {
    buf        *pktque.Buf       // кільцевий буфер пакетів
    head, tail int              // індекси найстарішого/найновішого пакета (дублюють buf.Head/Tail?)
    lock       *sync.RWMutex    // R/W м'ютекс для синхронізації
    cond       *sync.Cond       // condition variable для сигналізації підписникам
    curgopcount, maxgopcount int // поточна/максимальна кількість GOP у буфері
    streams    []av.CodecData   // метадані потоків (кодеки)
    videoidx   int              // індекс відео-потоку
    closed     bool             // прапорець закриття черги
}
```

### 🔧 Ініціалізація:

```go
func NewQueue() *Queue {
    q := &Queue{}
    q.buf = pktque.NewBuf()           // буфер на 64 пакети (розширюваний)
    q.maxgopcount = 2                 // за замовчуванням тримати 2 GOP
    q.lock = &sync.RWMutex{}
    q.cond = sync.NewCond(q.lock.RLocker())  // RLocker для читання
    q.videoidx = -1                   // ще не визначено
    return q
}
```

> 💡 **Чому `sync.Cond` з `RLocker`?**  
> `WritePacket` потребує ексклюзивного доступу (Lock), а `ReadPacket` — тільки читання (RLock). `cond.Wait()` автоматично звільняє lock і блокує горутину до сигналу.

---

## ✍️ 2. WritePacket — публікація пакетів з авто-очищенням

```go
func (self *Queue) WritePacket(pkt av.Packet) error {
    self.lock.Lock()  // ексклюзивний доступ для запису
    
    // 1. Додавання пакету у буфер
    self.buf.Push(pkt)
    
    // 2. Підрахунок GOP (Group of Pictures)
    if pkt.Idx == int8(self.videoidx) && pkt.IsKeyFrame {
        self.curgopcount++  // новий GOP почався
    }
    
    // 3. Auto-shrink: видалення старих пакетів при перевищенні ліміту GOP
    for self.curgopcount >= self.maxgopcount && self.buf.Count > 1 {
        oldPkt := self.buf.Pop()  // видаляємо найстаріший пакет
        
        // Якщо видалений пакет був ключовим кадром — зменшуємо лічильник GOP
        if oldPkt.Idx == int8(self.videoidx) && oldPkt.IsKeyFrame {
            self.curgopcount--
        }
        
        // Зупиняємо видалення, якщо досягли ліміту
        if self.curgopcount < self.maxgopcount {
            break
        }
    }
    
    // 4. Сигналізуємо всім підписникам: "є нові дані!"
    self.cond.Broadcast()
    
    self.lock.Unlock()
    return nil
}
```

### 🔍 Логіка авто-очищення (GOP-based eviction):

```
Приклад: maxgopcount = 2

Буфер містить:
  [GOP1: I-frame + 10 P-frames] + [GOP2: I-frame + 8 P-frames] + [GOP3: I-frame...]

Коли приходить новий I-frame (початок GOP4):
  curgopcount = 3, що >= maxgopcount (2)

Цикл видалення:
  1. Pop() → видаляє найстаріший пакет (з GOP1)
  2. Якщо це був I-frame → curgopcount-- (тепер 2)
  3. Умова curgopcount < maxgopcount → вихід з циклу

Результат: Буфер містить тільки останні 2 GOP → економія пам'яті
```

### ✅ Ваш use-case: управління пам'яттю для CCTV

```go
// Налаштування maxgopcount залежно від бітрейту каналу
func (p *ChannelManager) configureQueue(channelID string, bitrate int) {
    queue := p.getQueue(channelID)
    
    // Вищий бітрейт → менше кадрів у секунду → можна тримати більше GOP
    if bitrate > 4000 {  // >4 Mbps
        queue.SetMaxGopCount(3)  // тримати 3 GOP (~6-9 секунд)
    } else if bitrate > 1000 {
        queue.SetMaxGopCount(2)  // 2 GOP (~4-6 секунд)
    } else {
        queue.SetMaxGopCount(1)  // 1 GOP (~2-3 секунди) для низького бітрейту
    }
    
    log.Printf("Channel %s: maxgopcount=%d for bitrate=%d kbps", 
        channelID, queue.maxgopcount, bitrate/1000)
}
```

---

## 🎯 3. QueueCursor — незалежні курсори для підписників

### Концепція:
Кожен підписник отримує власний `QueueCursor`, який:
- **Не споживає пакети** з буфера (тільки читає)
- **Має власну позицію** (`pos`) у кільцевому буфері
- **Може починати з різних точок**: найновіший, найстаріший, або затриманий пакет

### Типи курсорів:

```go
// Latest() — курсор на найновішому пакеті (для live-стрімінгу)
func (self *Queue) Latest() *QueueCursor {
    cursor := self.newCursor()
    cursor.init = func(buf *pktque.Buf, videoidx int) pktque.BufPos {
        return buf.Tail  // позиція "після" останнього пакету
    }
    return cursor
}

// Oldest() — курсор на найстарішому доступному пакеті (для архіву/перемотування)
func (self *Queue) Oldest() *QueueCursor {
    cursor := self.newCursor()
    cursor.init = func(buf *pktque.Buf, videoidx int) pktque.BufPos {
        return buf.Head  // позиція найстарішого пакету
    }
    return cursor
}

// DelayedTime(dur) — курсор затриманий на `dur` від найновішого (для буферизації)
func (self *Queue) DelayedTime(dur time.Duration) *QueueCursor {
    cursor := self.newCursor()
    cursor.init = func(buf *pktque.Buf, videoidx int) pktque.BufPos {
        i := buf.Tail - 1
        if buf.IsValidPos(i) {
            end := buf.Get(i)  // найновіший пакет
            // Рухаємось назад, доки не знайдемо пакет, старіший за `dur`
            for buf.IsValidPos(i) {
                if end.Time - buf.Get(i).Time > dur {
                    break
                }
                i--
            }
        }
        return i
    }
    return cursor
}

// DelayedGopCount(n) — курсор затриманий на `n` GOP від найновішого
func (self *Queue) DelayedGopCount(n int) *QueueCursor {
    cursor := self.newCursor()
    cursor.init = func(buf *pktque.Buf, videoidx int) pktque.BufPos {
        i := buf.Tail - 1
        if videoidx != -1 {
            for gop := 0; buf.IsValidPos(i) && gop < n; i-- {
                pkt := buf.Get(i)
                // Рахуємо ключові кадри (початок GOP)
                if pkt.Idx == int8(self.videoidx) && pkt.IsKeyFrame {
                    gop++
                }
            }
        }
        return i
    }
    return cursor
}
```

### 🔍 Як працює `ReadPacket()` курсору:

```go
func (self *QueueCursor) ReadPacket() (pkt av.Packet, err error) {
    self.que.cond.L.Lock()  // блокуємо через RLocker (читання)
    
    // 1. Ініціалізація позиції при першому виклику
    if !self.gotpos {
        self.pos = self.init(buf, self.que.videoidx)
        self.gotpos = true
    }
    
    // 2. Основний цикл читання
    for {
        // Корекція позиції, якщо вона вийшла за межі буфера
        if self.pos.LT(buf.Head) {
            self.pos = buf.Head  // "відсталий" курсор → стрибок до голови
        } else if self.pos.GT(buf.Tail) {
            self.pos = buf.Tail  // "випереджаючий" курсор → стрибок до хвоста
        }
        
        // Якщо позиція валідна — читаємо пакет
        if buf.IsValidPos(self.pos) {
            pkt = buf.Get(self.pos)
            self.pos++  // просуваємо курсор
            break
        }
        
        // Якщо черга закрита — кінець потоку
        if self.que.closed {
            err = io.EOF
            break
        }
        
        // Інакше — чекаємо на нові дані (cond.Wait() звільняє lock)
        self.que.cond.Wait()
    }
    
    self.que.cond.L.Unlock()
    return
}
```

### ✅ Ваш use-case: різні стратегії підписки

```go
// Створення курсорів для різних цілей у CCTV HLS Processor
func (p *CCTVProcessor) createSubscribers(channelID string, queue *pubsub.Queue) {
    // 1. Live HLS генератор (мінімальна затримка)
    liveCursor := queue.Latest()
    go p.generateHLSSegments(channelID, liveCursor, "live")
    
    // 2. Затриманий HLS для буферизації (10 секунд)
    delayedCursor := queue.DelayedTime(10 * time.Second)
    go p.generateHLSSegments(channelID, delayedCursor, "delayed_10s")
    
    // 3. WebSocket для real-time клієнтів (з можливістю перемотування)
    wsCursor := queue.Oldest()  // починаємо з найстарішого доступного
    go p.handleWebSocketClients(channelID, wsCursor)
    
    // 4. Архіватор (запис всіх пакетів у довгострокове сховище)
    archiveCursor := queue.Oldest()
    go p.archiveStream(channelID, archiveCursor)
    
    // 5. Аналітика (підрахунок метрик без затримки)
    analyticsCursor := queue.Latest()
    go p.collectMetrics(channelID, analyticsCursor)
}
```

---

## 🔐 4. Синхронізація та потокобезпека

### Механізми синхронізації:

| Компонент | Призначення | Приклад використання |
|-----------|-------------|---------------------|
| `sync.RWMutex` | Дозволяє багатьом читачам або одному писателю | `WritePacket` → `Lock()`, `ReadPacket` → `RLock()` |
| `sync.Cond` | Сигналізація між горутинами | `WritePacket` → `Broadcast()`, `ReadPacket` → `Wait()` |
| `pktque.Buf` | Потокобезпечний кільцевий буфер | `Push()`/`Pop()` вже синхронізовані внутрішньо |

### 🔄 Потік сигналізації:

```
Writer (Publisher):
  1. Lock()
  2. buf.Push(pkt)
  3. cond.Broadcast()  ← "є нові дані!"
  4. Unlock()

Readers (Subscribers):
  1. RLock() (через cond.L)
  2. if no data: cond.Wait()  ← звільняє lock і блокується
  3. ... (прокидається при Broadcast)
  4. buf.Get(pos)
  5. RUnlock()
```

### ✅ Ваш use-case: уникнення deadlock

```go
// ❌ НЕПРАВИЛЬНО: виклик WritePacket з утриманням lock
func (p *Processor) badWrite(queue *pubsub.Queue, pkt av.Packet) {
    p.lock.Lock()
    // ... обробка ...
    queue.WritePacket(pkt)  // WritePacket також викликає Lock() → deadlock!
    p.lock.Unlock()
}

// ✅ ПРАВИЛЬНО: мінімізувати час утримання lock
func (p *Processor) goodWrite(queue *pubsub.Queue, pkt av.Packet) {
    // Обробка без lock
    processedPkt := p.processPacket(pkt)
    
    // Тільки запис у чергу з lock
    if err := queue.WritePacket(processedPkt); err != nil {
        log.Error("write to queue failed", "err", err)
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// cctv_pubsub.go — інтеграція pubsub.Queue у CCTV HLS Processor
type ChannelPipeline struct {
    channelID  string
    queue      *pubsub.Queue
    demuxer    av.DemuxCloser
    cursors    map[string]*pubsub.QueueCursor  // subscriber ID → cursor
    metrics    *ChannelMetrics
}

func NewChannelPipeline(channelID string, inputURI string) (*ChannelPipeline, error) {
    // 1. Відкриття джерела
    demuxer, err := avutil.Open(inputURI)
    if err != nil {
        return nil, fmt.Errorf("open %s: %w", inputURI, err)
    }
    
    // 2. Створення черги
    queue := pubsub.NewQueue()
    queue.SetMaxGopCount(2)  // налаштування за замовчуванням
    
    // 3. Ініціалізація метаданих потоків
    streams, err := demuxer.Streams()
    if err != nil {
        return nil, err
    }
    queue.WriteHeader(streams)
    
    return &ChannelPipeline{
        channelID: channelID,
        queue:     queue,
        demuxer:   demuxer,
        cursors:   make(map[string]*pubsub.QueueCursor),
        metrics:   NewChannelMetrics(channelID),
    }, nil
}

// StartPublisher — запуск публікації пакетів у чергу
func (p *ChannelPipeline) StartPublisher(ctx context.Context) error {
    go func() {
        defer p.queue.Close()
        defer p.demuxer.Close()
        
        for {
            select {
            case <-ctx.Done():
                return
            default:
            }
            
            pkt, err := p.demuxer.ReadPacket()
            if err == io.EOF {
                log.Printf("Channel %s: EOF reached", p.channelID)
                break
            }
            if err != nil {
                p.metrics.ReadErrors.Inc()
                log.Warn("read packet failed", "err", err)
                continue
            }
            
            // Публікація у чергу
            if err := p.queue.WritePacket(pkt); err != nil {
                p.metrics.WriteErrors.Inc()
                log.Error("write to queue failed", "err", err)
                continue
            }
            
            p.metrics.PacketsPublished.Inc()
        }
    }()
    return nil
}

// Subscribe — реєстрація нового підписника
func (p *ChannelPipeline) Subscribe(subscriberID string, strategy string) (*pubsub.QueueCursor, error) {
    p.metrics.Subscribers.Inc()
    
    var cursor *pubsub.QueueCursor
    switch strategy {
    case "live":
        cursor = p.queue.Latest()
    case "delayed_5s":
        cursor = p.queue.DelayedTime(5 * time.Second)
    case "delayed_10s":
        cursor = p.queue.DelayedTime(10 * time.Second)
    case "archive":
        cursor = p.queue.Oldest()
    default:
        return nil, fmt.Errorf("unknown strategy: %s", strategy)
    }
    
    p.cursors[subscriberID] = cursor
    log.Printf("Channel %s: subscribed %s with strategy %s", 
        p.channelID, subscriberID, strategy)
    
    return cursor, nil
}

// Unsubscribe — видалення підписника
func (p *ChannelPipeline) Unsubscribe(subscriberID string) {
    if cursor, ok := p.cursors[subscriberID]; ok {
        // Курсор не потребує явного закриття — він просто перестає читати
        delete(p.cursors, subscriberID)
        p.metrics.Subscribers.Dec()
        log.Printf("Channel %s: unsubscribed %s", p.channelID, subscriberID)
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Курсор "відстає" і пропускає пакети** | `pos < buf.Head` після видалення старих пакетів | Курсор автоматично стрибає до `buf.Head` — це очікувана поведінка; логувайте такі випадки для моніторингу |
| **Deadlock при WritePacket** | Горутина зависає на `cond.Wait()` | Переконайтеся, що `WritePacket` не викликається з утриманням іншого lock; використовуйте окремі м'ютекси для різних ресурсів |
| **Пам'ять росте через buf** | `maxgopcount` занадто великий або ключові кадри не детектуються | Налаштуйте `maxgopcount` відповідно до бітрейту; переконайтеся, що `videoidx` визначено правильно |
| **Підписник не отримує дані** | `cond.Broadcast()` не викликається або курсор не ініціалізований | Перевірте, що `WriteHeader()` викликано перед першим `WritePacket()`; додайте логування при `cond.Wait()` |
| **EOF не обробляється коректно** | Підписники не отримують `io.EOF` після `Close()` | Переконайтеся, що `Close()` викликається і `cond.Broadcast()` сигналізує всім очікуючим курсорам |

---

## ⚡ Оптимізації для real-time обробки

### 1. Пакетна публікація для зменшення накладних витрат:

```go
// WritePacketBatch — публікація кількох пакетів за один lock
func (q *Queue) WritePacketBatch(packets []av.Packet) error {
    q.lock.Lock()
    defer q.lock.Unlock()
    
    for _, pkt := range packets {
        q.buf.Push(pkt)
        if pkt.Idx == int8(q.videoidx) && pkt.IsKeyFrame {
            q.curgopcount++
        }
    }
    
    // Один Broadcast замість багатьох
    q.cond.Broadcast()
    
    // Auto-shrink (той самий логіка, але для пакетів)
    q.shrinkBuffer()
    
    return nil
}
```

### 2. Моніторинг стану черги:

```go
type QueueMetrics struct {
    BufferSizeBytes   prometheus.Gauge
    BufferCount       prometheus.Gauge
    GOPCount          prometheus.Gauge
    SubscriberCount   prometheus.Gauge
    PublishLatency    prometheus.Histogram
}

func (q *Queue) ReportMetrics(m *QueueMetrics) {
    q.lock.RLock()
    defer q.lock.RUnlock()
    
    m.BufferSizeBytes.Set(float64(q.buf.Size))
    m.BufferCount.Set(float64(q.buf.Count))
    m.GOPCount.Set(float64(q.curgopcount))
    m.SubscriberCount.Set(float64(len(q.cursors)))
}
```

### 3. Асинхронна обробка для повільних підписників:

```go
// AsyncCursor — обгортка курсору з чергою для повільних обробників
type AsyncCursor struct {
    cursor   *pubsub.QueueCursor
    queue    chan av.Packet
    done     chan struct{}
    dropped  prometheus.Counter
}

func NewAsyncCursor(base *pubsub.QueueCursor, queueSize int) *AsyncCursor {
    ac := &AsyncCursor{
        cursor:  base,
        queue:   make(chan av.Packet, queueSize),
        done:    make(chan struct{}),
        dropped: monitoring.DroppedPackets,
    }
    
    // Background reader goroutine
    go func() {
        for {
            pkt, err := base.ReadPacket()
            if err == io.EOF {
                close(ac.queue)
                return
            }
            if err != nil {
                log.Warn("async cursor read error", "err", err)
                continue
            }
            
            select {
            case ac.queue <- pkt:
                // Успішно відправлено
            case <-time.After(10 * time.Millisecond):
                // Черга переповнена — пропускаємо пакет
                ac.dropped.Inc()
            case <-ac.done:
                return
            }
        }
    }()
    
    return ac
}

func (ac *AsyncCursor) ReadPacket() (av.Packet, error) {
    pkt, ok := <-ac.queue
    if !ok {
        return av.Packet{}, io.EOF
    }
    return pkt, nil
}
```

---

## 📋 Чек-лист інтеграції pubsub.Queue

```go
// ✅ 1. Створення черги з адекватним maxgopcount
queue := pubsub.NewQueue()
queue.SetMaxGopCount(2)  // налаштувати за бітрейтом каналу

// ✅ 2. Ініціалізація метаданих перед публікацією
streams, _ := demuxer.Streams()
queue.WriteHeader(streams)

// ✅ 3. Запуск публікатора у окремій горутині
go func() {
    for pkt := range packetStream {
        queue.WritePacket(pkt)  // блокується тільки на час запису
    }
    queue.Close()  // сигналізує всім підписникам
}()

// ✅ 4. Створення курсорів з потрібними стратегіями
liveCursor := queue.Latest()           // для live HLS
delayedCursor := queue.DelayedTime(10*time.Second)  // для буферизації
archiveCursor := queue.Oldest()        // для архіву

// ✅ 5. Обробка io.EOF у підписниках
for {
    pkt, err := cursor.ReadPacket()
    if err == io.EOF {
        log.Info("stream ended")
        break
    }
    if err != nil {
        log.Warn("read error", "err", err)
        continue
    }
    processPacket(pkt)
}

// ✅ 6. Моніторинг метрик
monitoring.QueueSize.Set(float64(queue.buf.Size))
monitoring.Subscribers.Set(float64(len(queue.cursors)))

// ✅ 7. Очищення ресурсів
queue.Close()  // якщо потрібно примусово закрити
```

---

## 🔗 Корисні посилання

- 💻 [vdk pubsub Package](https://pkg.go.dev/github.com/deepch/vdk/av/pubsub) — GoDoc documentation (якщо доступна)
- 📄 [Pub/Sub Pattern Wikipedia](https://en.wikipedia.org/wiki/Publish%E2%80%93subscribe_pattern) — теоретична основа
- 🎬 [HLS Low-Latency Spec](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis) — як затримка впливає на HLS
- 🧪 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади використання pubsub

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Налаштуйте `maxgopcount` відповідно до бітрейту** — високий бітрейт потребує менше GOP у буфері для економії пам'яті.
> 2. **Використовуйте `DelayedTime()` для буферизації** — це дозволяє клієнтам з поганою мережею отримувати стабільний потік.
> 3. **Моніторьте `buf.Size` та `curgopcount`** — різке зростання може вказувати на проблему з обробкою підписниками.
> 4. **Обробляйте `io.EOF` коректно** — при закритті черги всі курсори повинні отримати EOF для чистого завершення.
> 5. **Тестуйте з різними стратегіями підписки** — переконайтеся, що `Latest()`, `Oldest()`, `DelayedTime()` працюють як очікується для вашого use-case.

Потрібен приклад інтеграції `pubsub.Queue` з вашим `WebSocketDistributorService` для розсилки субтитрів та відео-метаданих у реальному часі? Готовий допомогти! 🚀