# 🎛️ Глибокий розбір: pktque — Фільтри пакетів для медіа-пайплайнів

Цей файл — **система фільтрації та трансформації медіа-пакетів** у бібліотеці `vdk`. Вона дозволяє модифікувати, відкидати або синхронізувати пакети "на льоту" під час читання з демуксера.

Розберемо архітектуру, готові фільтри та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема pktque

```
┌────────────────────────────────────────┐
│ 📦 pktque — Filter Pipeline System     │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові абстракції:                 │
│  • Filter interface — контракт фільтра │
│  • Filters []Filter — композиція       │
│  • FilterDemuxer — обгортка демуксера  │
│                                         │
│  🎛️  Готові фільтри:                    │
│  • WaitKeyFrame  — чекати на I-frame   │
│  • FixTime       — корекція таймінгів  │
│  • AVSync        — A/V синхронізація   │
│  • CalcDuration  — розрахунок тривалості│
│  • Walltime      — real-time режим     │
│                                         │
│  🔄 Потік даних:                        │
│  Demuxer → FilterDemuxer → [Filters]  │
│              ↓                          │
│  ReadPacket() → ModifyPacket() → pkt  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Filter Interface — контракт фільтрації

### Базовий інтерфейс:

```go
type Filter interface {
    // ModifyPacket — головний метод фільтра
    // Повертає: drop=true (відкинути пакет) або err (помилка)
    ModifyPacket(pkt *av.Packet, streams []av.CodecData, videoidx int, audioidx int) (drop bool, err error)
}
```

### 📋 Параметри `ModifyPacket`:

| Параметр | Тип | Призначення | Приклад використання |
|----------|-----|-------------|---------------------|
| `pkt` | `*av.Packet` | Пакет для модифікації | `pkt.Time += offset`, `pkt.IsKeyFrame = false` |
| `streams` | `[]av.CodecData` | Метадані всіх потоків | Визначення кодеків, бітрейту |
| `videoidx` | `int` | Індекс відео-потоку | `if pkt.Idx == videoidx { ... }` |
| `audioidx` | `int` | Індекс аудіо-потоку | `if pkt.Idx == audioidx { ... }` |

### ✅ Ваш use-case: кастомний фільтр для субтитрів

```go
// SubtitleFilter — фільтр для витягування телетекст-пакетів
type SubtitleFilter struct {
    channelID    string
    teletextPID  uint16
    callback     func(av.Packet)  // відправка у процесор субтитрів
}

func (f *SubtitleFilter) ModifyPacket(pkt *av.Packet, streams []av.CodecData, videoidx, audioidx int) (bool, error) {
    // 1. Якщо це телетекст-потік — обробити та відкинути (не передавати далі)
    if pkt.Idx == f.teletextPID {
        // Асинхронна обробка субтитрів
        go f.callback(*pkt)
        return true, nil  // drop=true: не передавати у основний пайплайн
    }
    
    // 2. Для відео: додавання метаданих про канал
    if pkt.Idx == int8(videoidx) {
        // Можна додати кастомні поля у pkt.Data або змінити таймінги
        // Напр.: синхронізація з серверним часом
    }
    
    // 3. Для аудіо: нормалізація гучності (якщо потрібно)
    // (реалізація залежить від кодеків)
    
    return false, nil  // drop=false: передати пакет далі
}
```

---

## 🔗 2. Filters — композиція фільтрів

### Ланцюжок обробки:

```go
type Filters []Filter

func (self Filters) ModifyPacket(pkt *av.Packet, streams []av.CodecData, videoidx, audioidx int) (bool, error) {
    for _, filter := range self {
        // Кожен фільтр може:
        // 1. Модифікувати pkt (Time, Data, IsKeyFrame тощо)
        // 2. Повернути drop=true (зупинити обробку, відкинути пакет)
        // 3. Повернути err (зупинити весь пайплайн)
        if drop, err := filter.ModifyPacket(pkt, streams, videoidx, audioidx); err != nil {
            return drop, err  // помилка зупиняє все
        } else if drop {
            return true, nil  // пакет відкинуто
        }
    }
    return false, nil  // пакет пройшов всі фільтри
}
```

### ✅ Ваш use-case: побудова пайплайну для CCTV

```go
// BuildCCTVPipeline — створення ланцюжка фільтрів для каналу
func (p *CCTVProcessor) BuildCCTVPipeline(channelID string) pktque.Filters {
    return pktque.Filters{
        // 1. Чекати на ключовий кадр перед початком відтворення
        &pktque.WaitKeyFrame{},
        
        // 2. Корекція таймінгів: почати з 0, забезпечити монотонність
        &pktque.FixTime{
            StartFromZero: true,
            MakeIncrement: true,
        },
        
        // 3. A/V синхронізація: відкидати пакети з великим розривом
        &pktque.AVSync{
            MaxTimeDiff: 500 * time.Millisecond,
        },
        
        // 4. Розрахунок тривалості пакетів (для HLS сегментації)
        &pktque.CalcDuration{
            LastTime: make(map[int8]time.Duration),
        },
        
        // 5. Кастомний фільтр для субтитрів
        &SubtitleFilter{
            channelID:   channelID,
            teletextPID: p.getChannelConfig(channelID).TeletextPID,
            callback:    p.processSubtitlePacket,
        },
        
        // 6. (Опціонально) Real-time режим для тестування
        // &pktque.Walltime{},  // розкоментуйте для -re поведінки
    }
}
```

---

## 🎛️ 3. FilterDemuxer — обгортка для прозорої фільтрації

### Як це працює:

```go
type FilterDemuxer struct {
    av.Demuxer  // базовий демуксер (джерело)
    Filter      Filter  // ланцюжок фільтрів
    streams     []av.CodecData  // кешовані метадані
    videoidx    int  // індекс відео
    audioidx    int  // індекс аудіо
}

func (self *FilterDemuxer) ReadPacket() (pkt av.Packet, err error) {
    // 1. Ініціалізація: отримання інформації про потоки
    if self.streams == nil {
        self.streams, _ = self.Demuxer.Streams()
        // Визначення індексів відео/аудіо
        for i, stream := range self.streams {
            if stream.Type().IsVideo() { self.videoidx = i }
            if stream.Type().IsAudio() { self.audioidx = i }
        }
    }
    
    // 2. Цикл читання з фільтрацією
    for {
        // Читання сирого пакету з джерела
        pkt, err = self.Demuxer.ReadPacket()
        if err != nil { return }  // EOF або помилка
        
        // Застосування фільтрів
        drop, err := self.Filter.ModifyPacket(&pkt, self.streams, self.videoidx, self.audioidx)
        if err != nil { return err }  // помилка фільтра
        if drop { continue }          // пакет відкинуто, читаємо наступний
        
        // Пакет пройшов фільтрацію — повертаємо клієнту
        return pkt, nil
    }
}
```

### ✅ Ваш use-case: інтеграція з вашим пайплайном

```go
// StartFilteredStream — запуск фільтрованого читання для каналу
func (p *CCTVProcessor) StartFilteredStream(ctx context.Context, channelID string, inputURI string) error {
    // 1. Відкриття вхідного потоку
    demuxer, err := avutil.Open(inputURI)
    if err != nil {
        return fmt.Errorf("open %s: %w", inputURI, err)
    }
    
    // 2. Побудова фільтрів
    filters := p.BuildCCTVPipeline(channelID)
    
    // 3. Створення обгорнутого демуксера
    filterDemux := &pktque.FilterDemuxer{
        Demuxer: demuxer,
        Filter:  filters,
    }
    
    // 4. Основний цикл читання (тепер з фільтрацією!)
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        pkt, err := filterDemux.ReadPacket()
        if err == io.EOF {
            log.Printf("Channel %s: stream ended", channelID)
            break
        }
        if err != nil {
            log.Warn("read filtered packet failed", "err", err)
            continue
        }
        
        // Пакет вже пройшов всі фільтри — можна відправляти у HLS
        if err := p.writeToHLSSegment(channelID, pkt); err != nil {
            log.Error("write to HLS failed", "err", err)
        }
    }
    
    return demuxer.Close()
}
```

---

## 🎛️ 4. Готові фільтри — детальний розбір

### 🔑 WaitKeyFrame — чекати на I-frame

```go
type WaitKeyFrame struct {
    ok bool  // прапорець: чи вже отримано ключовий кадр
}

func (self *WaitKeyFrame) ModifyPacket(pkt *av.Packet, streams []av.CodecData, videoidx, audioidx int) (bool, error) {
    // Якщо ще не отримали ключовий кадр І це відео-потік І це ключовий кадр
    if !self.ok && pkt.Idx == int8(videoidx) && pkt.IsKeyFrame {
        self.ok = true  // тепер пропускаємо всі пакети
    }
    // Відкидати всі пакети поки self.ok == false
    drop := !self.ok
    return drop, nil
}
```

#### ✅ Ваш use-case: швидкий старт відтворення

```
Проблема: Якщо почати HLS-сегмент з P/B-frame, плеєр не зможе декодувати до наступного I-frame.

Рішення: WaitKeyFrame відкидає всі пакети до першого I-frame.

Результат: Перший сегмент завжди починається з ключового кадру → миттєвий старт відтворення.
```

---

### ⏱️ FixTime — корекція таймінгів

```go
type FixTime struct {
    zerobase      time.Duration  // базовий час для StartFromZero
    incrbase      time.Duration  // корекція для MakeIncrement
    lasttime      time.Duration  // останній коректний таймінг
    StartFromZero bool          // нормалізувати час до 0
    MakeIncrement bool          // забезпечити монотонне зростання
}
```

#### Логіка `StartFromZero`:
```go
if self.StartFromZero {
    if self.zerobase == 0 {
        self.zerobase = pkt.Time  // запам'ятати перший таймінг
    }
    pkt.Time -= self.zerobase  // зсунути всі таймінги
}
// Результат: перший пакет має Time=0, решта — відносні значення
```

#### Логіка `MakeIncrement`:
```go
if self.MakeIncrement {
    pkt.Time -= self.incrbase  // застосувати накопичену корекцію
    
    // Якщо таймінг "зламався" (менше попереднього або стрибок >500мс)
    if pkt.Time < self.lasttime || pkt.Time > self.lasttime+500*time.Millisecond {
        // Коригуємо базу, щоб "вирівняти" час
        self.incrbase += pkt.Time - self.lasttime
        pkt.Time = self.lasttime  // використати попередній коректний час
    }
    self.lasttime = pkt.Time  // оновити останній коректний час
}
```

#### ✅ Ваш use-case: виправлення розривів у потоці

```
Проблема: Камера перезапустилася → таймінги "перестрибнули" на +1 годину.

До корекції:
  Пакет 100: Time=3600s
  Пакет 101: Time=7200s  ← розрив 3600с!

Після FixTime{MakeIncrement: true}:
  Пакет 100: Time=3600s
  Пакет 101: Time=3600.04s  ← вирівняно до монотонного зростання

Результат: HLS-плеєр не "зависає" при розривах потоку.
```

---

### 🎵 AVSync — синхронізація аудіо/відео

```go
type AVSync struct {
    MaxTimeDiff time.Duration      // допустимий розрив (дефолт: 500мс)
    time        []time.Duration    // останній таймінг для кожного потоку
}
```

#### Алгоритм синхронізації:

```
1. Знайти min/max таймінги серед всіх потоків
2. Визначити "еталонний" час (min)
3. Для кожного пакету:
   ├─ Якщо Time у межах [min, min+MaxTimeDiff] → OK
   ├─ Якщо пакет "відстає" і це не max-потік → скоригувати Time
   └─ Якщо пакет "випереджає" і це max-потік → відкинути (drop=true)
```

#### ✅ Ваш use-case: виправлення десинхронізації

```
Проблема: Аудіо відстає від відео на 2 секунди через мережеві затримки.

До AVSync:
  Відео: Time=10.0s, Аудіо: Time=8.0s  ← розрив 2с

Після AVSync{MaxTimeDiff: 500ms}:
  Відео: Time=10.0s (еталон)
  Аудіо: Time=10.04s (скориговано)  ← синхронізовано!

Результат: Губи співпадають зі звуком у відтворенні.
```

---

### 📏 CalcDuration — розрахунок тривалості пакетів

```go
type CalcDuration struct {
    LastTime map[int8]time.Duration  // останній таймінг для кожного потоку
}

func (self *CalcDuration) ModifyPacket(pkt *av.Packet, ...) (bool, error) {
    // Duration = поточний Time - попередній Time
    if tmp, ok := self.LastTime[pkt.Idx]; ok && tmp != 0 {
        pkt.Duration = pkt.Time - self.LastTime[pkt.Idx]
    } else if pkt.Time < 100*time.Millisecond {
        // Для перших пакетів: Duration ≈ Time
        pkt.Duration = pkt.Time
    }
    self.LastTime[pkt.Idx] = pkt.Time  // запам'ятати для наступного
    return false, nil
}
```

#### ✅ Ваш use-case: точна сегментація HLS

```
Проблема: Без Duration неможливо точно визначити тривалість сегменту.

Рішення: CalcDuration додає тривалість кожному пакету.

Використання у HLS:
  segmentDuration = sum(pkt.Duration for pkt in segment)
  
Результат: Сегменти точно 10.0с, не 9.8с або 10.3с → стабільний плейлист.
```

---

### ⏱️ Walltime — real-time режим (як ffmpeg -re)

```go
type Walltime struct {
    firsttime time.Time  // час старту відтворення
}

func (self *Walltime) ModifyPacket(pkt *av.Packet, ...) (bool, error) {
    // Тільки для першого потоку (зазвичай відео)
    if pkt.Idx == 0 {
        if self.firsttime.IsZero() {
            self.firsttime = time.Now()  // запам'ятати старт
        }
        
        // Розрахувати "ідеальний" час відтворення цього пакету
        pkttime := self.firsttime.Add(pkt.Time)
        
        // Якщо ще не час — спати
        delta := pkttime.Sub(time.Now())
        if delta > 0 {
            time.Sleep(delta)
        }
    }
    return false, nil
}
```

#### ✅ Ваш use-case: тестування плеєрів у real-time

```bash
# Без Walltime: конвертація 1-годинного запису за 2 хвилини
./converter -i recorded.ts -o output.m3u8  # швидко, але не real-time

# З Walltime: відтворення зі швидкістю 1×
./converter -i recorded.ts -o output.m3u8 -re  # 1 година за 1 годину

# Навіщо це потрібно:
# • Тестування буферизації плеєра
# • Симуляція live-стрімінгу для навантажувальних тестів
# • Відладка синхронізації субтитрів у реальному часі
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// cctv_filter_pipeline.go — повний приклад використання pktque
type FilteredStream struct {
    channelID    string
    filterDemux  *pktque.FilterDemuxer
    hlsWriter    *HLSWriter
    metrics      *StreamMetrics
}

func NewFilteredStream(channelID string, inputURI string) (*FilteredStream, error) {
    // 1. Відкриття джерела
    demuxer, err := avutil.Open(inputURI)
    if err != nil {
        return nil, err
    }
    
    // 2. Побудова фільтрів
    filters := pktque.Filters{
        &pktque.WaitKeyFrame{},  // старт з I-frame
        &pktque.FixTime{
            StartFromZero: true,
            MakeIncrement: true,
        },
        &pktque.AVSync{MaxTimeDiff: 300 * time.Millisecond},
        &pktque.CalcDuration{LastTime: make(map[int8]time.Duration)},
        // Кастомний фільтр для субтитрів
        &SubtitleExtractor{
            channelID: channelID,
            onSubtitle: func(sub SubtitleData) {
                // Відправка у WebSocket
                websocketSender.Broadcast(channelID, sub)
            },
        },
    }
    
    // 3. Створення обгорнутого демуксера
    filterDemux := &pktque.FilterDemuxer{
        Demuxer: demuxer,
        Filter:  filters,
    }
    
    return &FilteredStream{
        channelID:   channelID,
        filterDemux: filterDemux,
        hlsWriter:   NewHLSWriter(channelID),
        metrics:     NewStreamMetrics(channelID),
    }, nil
}

// Run — основний цикл обробки
func (fs *FilteredStream) Run(ctx context.Context) error {
    segmentTicker := time.NewTicker(10 * time.Second)
    defer segmentTicker.Stop()
    
    var currentSegment []av.Packet
    segmentStart := time.Now()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-segmentTicker.C:
            // Фіналізація поточного сегменту
            if len(currentSegment) > 0 {
                if err := fs.hlsWriter.WriteSegment(fs.channelID, currentSegment); err != nil {
                    log.Error("write segment failed", "err", err)
                }
                currentSegment = currentSegment[:0]  // очистити
                segmentStart = time.Now()
            }
        }
        
        // Читання фільтрованого пакету
        pkt, err := fs.filterDemux.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            fs.metrics.ReadErrors.Inc()
            continue
        }
        
        // Додавання у поточний сегмент
        currentSegment = append(currentSegment, pkt)
        
        // Оновлення метрик
        fs.metrics.PacketsProcessed.Inc()
        fs.metrics.LastPacketTime.Set(float64(pkt.Time.Milliseconds()))
        
        // Моніторинг розривів
        if len(currentSegment) > 1 {
            gap := pkt.Time - currentSegment[len(currentSegment)-2].Time
            if gap > 2*time.Second {
                fs.metrics.TimeGaps.Inc()
                log.Warn("time gap detected", "gap", gap, "channel", fs.channelID)
            }
        }
    }
    
    // Фінальний сегмент
    if len(currentSegment) > 0 {
        fs.hlsWriter.WriteSegment(fs.channelID, currentSegment)
    }
    
    return fs.filterDemux.Close()
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Фільтр "з'їдає" всі пакети** | `WaitKeyFrame` не бачить ключових кадрів | Переконайтеся, що `videoidx` визначено правильно; деякі кодеки не встановлюють `IsKeyFrame` |
| **Таймінги "скачуть" після корекції** | `FixTime.MakeIncrement` надто агресивний | Збільште поріг `500*time.Millisecond` або вимкніть `MakeIncrement` для стабільних потоків |
| **Аудіо/відео десинхронізація посилюється** | `AVSync` відкидає занадто багато пакетів | Збільште `MaxTimeDiff` або додайте логіку "плавного" вирівнювання замість різких стрибків |
| **Duration = 0 для перших пакетів** | `CalcDuration` не ініціалізований | Переконайтеся, що `LastTime` map створено до першого `ModifyPacket` виклику |
| **Walltime "зависає" на початку** | `firsttime` не скидається між перезапусками | Додайте `Reset()` метод або створюйте новий `Walltime` для кожного сеансу |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування індексів потоків:

```go
// Замість пошуку videoidx/audioidx при кожному ReadPacket:
type CachedStreamIndices struct {
    once     sync.Once
    videoIdx int
    audioIdx int
    err      error
}

func (c *CachedStreamIndices) Get(demuxer av.Demuxer) (video, audio int, err error) {
    c.once.Do(func() {
        streams, err := demuxer.Streams()
        if err != nil {
            c.err = err
            return
        }
        for i, s := range streams {
            if s.Type().IsVideo() { c.videoIdx = i }
            if s.Type().IsAudio() { c.audioIdx = i }
        }
    })
    return c.videoIdx, c.audioIdx, c.err
}
```

### 2. Async фільтрація для не-критичних пакетів:

```go
// Для субтитрів: не блокувати основний потік
type AsyncSubtitleFilter struct {
    queue chan av.Packet
    done  chan struct{}
}

func (f *AsyncSubtitleFilter) ModifyPacket(pkt *av.Packet, ...) (bool, error) {
    if pkt.Idx == subtitleIdx {
        // Копіювати пакет (бо pkt може бути перевикористаний)
        pktCopy := *pkt
        pktCopy.Data = make([]byte, len(pkt.Data))
        copy(pktCopy.Data, pkt.Data)
        
        select {
        case f.queue <- pktCopy:
            // Відправлено у чергу для асинхронної обробки
        case <-time.After(10 * time.Millisecond):
            // Черга переповнена — пропускаємо цей субтитр
        }
        return true, nil  // відкинути з основного потоку
    }
    return false, nil
}
```

### 3. Моніторинг ефективності фільтрів:

```go
type FilterMetrics struct {
    PacketsProcessed prometheus.Counter
    PacketsDropped   prometheus.CounterVec  // by filter name
    ProcessingTime   prometheus.Histogram
}

func (m *FilterMetrics) Record(filterName string, dropped bool, duration time.Duration) {
    m.PacketsProcessed.Inc()
    if dropped {
        m.PacketsDropped.WithLabelValues(filterName).Inc()
    }
    m.ProcessingTime.Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції pktque

```go
// ✅ 1. Визначення індексів потоків
streams, _ := demuxer.Streams()
var videoIdx, audioIdx int
for i, s := range streams {
    if s.Type().IsVideo() { videoIdx = i }
    if s.Type().IsAudio() { audioIdx = i }
}

// ✅ 2. Побудова ланцюжка фільтрів
filters := pktque.Filters{
    &pktque.WaitKeyFrame{},
    &pktque.FixTime{StartFromZero: true, MakeIncrement: true},
    &pktque.AVSync{MaxTimeDiff: 300 * time.Millisecond},
    // ... кастомні фільтри
}

// ✅ 3. Створення FilterDemuxer
filterDemux := &pktque.FilterDemuxer{
    Demuxer: demuxer,
    Filter:  filters,
}

// ✅ 4. Читання з фільтрацією
for {
    pkt, err := filterDemux.ReadPacket()
    if err == io.EOF { break }
    if err != nil { log.Warn("read error", "err", err); continue }
    
    // Пакет вже відфільтрований — можна використовувати
    processPacket(pkt)
}

// ✅ 5. Метрики
monitoring.FilteredPackets.Inc()
monitoring.FilterLatency.Observe(time.Since(start).Seconds())

// ✅ 6. Очищення ресурсів
filterDemux.Close()  // якщо реалізовано
demuxer.Close()
```

---

## 🔗 Корисні посилання

- 💻 [vdk pktque Package](https://pkg.go.dev/github.com/deepch/vdk/av/pktque) — GoDoc documentation
- 📄 [FFmpeg Filter Graph](https://ffmpeg.org/ffmpeg-filters.html) — концептуальний аналог для порівняння
- 🎬 [HLS Timing Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4.2) — вимоги до таймінгів у HLS
- 🧪 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади використання фільтрів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Завжди використовуйте `WaitKeyFrame`** на початку пайплайну — це гарантує коректний старт HLS-сегментів.
> 2. **Налаштуйте `FixTime.MakeIncrement` обережно** — надто агресивна корекція може "зламати" синхронізацію з субтитрами.
> 3. **Моніторьте `AVSync` drop rate** — якщо фільтр відкидає >5% пакетів, збільште `MaxTimeDiff`.
> 4. **Використовуйте `CalcDuration` для точної сегментації** — це критично для стабільного HLS плейлиста.
> 5. **Тестуйте з `Walltime` для відладки** — дозволяє симулювати live-потік з записаного файлу.

Потрібен приклад кастомного фільтра для **синхронізації субтитрів з відео** на основі PTS або для **детекції розривів у потоці**? Готовий допомогти! 🚀