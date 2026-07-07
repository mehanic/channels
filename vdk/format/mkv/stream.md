# 📦 Глибокий розбір: `mkv.Stream` — Стан окремого треку у MKV контейнері

Цей файл — **структура для представлення окремого медіа-треку** у межах `mkv.Demuxer`. Вона містить стан для обробки пакетів, таймінги, та посилання на батьківський демуксер. Ця структура є критичною для інкрементальної обробки `av.Packet` з контейнера Matroska/WebM.

---

## 🗺️ Архітектурна схема mkv.Stream

```
┌────────────────────────────────────────┐
│ 📦 mkv.Stream — MKV Track State       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • av.CodecData — метадані кодека      │
│  • demuxer *Demuxer — посилання на батька│
│  • pid/streamId/streamType — ідентифікатори│
│  • idx — індекс треку у масиві         │
│  • iskeyframe/pts/dts — таймінги пакетів│
│  • data/datalen — буфер даних          │
│                                         │
│  🔄 Ролі:                               │
│  • Накопичення даних пакету            │
│  • Розрахунок PTS/DTS для синхронізації│
│  • Визначення ключових кадрів          │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Базові поля структури

### Вбудований інтерфейс `av.CodecData`:

```go
av.CodecData  // вбудоване поле
```

**Призначення**: надає доступ до метаданих кодека:
- `Type()` → `av.H264`, `av.AAC`, тощо
- `SampleRate()`, `ChannelLayout()` → для аудіо
- `Width()`, `Height()`, `FPS()` → для відео

**✅ Ваш use-case**: отримання параметрів треку

```go
// GetTrackInfo — витягування метаданих для логування
func GetTrackInfo(stream *mkv.Stream) map[string]interface{} {
    info := map[string]interface{}{
        "codec": stream.Type().String(),
        "idx": stream.idx,
    }
    
    if stream.Type().IsVideo() {
        if vc, ok := stream.CodecData.(av.VideoCodecData); ok {
            info["resolution"] = fmt.Sprintf("%dx%d", vc.Width(), vc.Height())
            info["fps"] = vc.FPS()
        }
    }
    if stream.Type().IsAudio() {
        if ac, ok := stream.CodecData.(av.AudioCodecData); ok {
            info["sampleRate"] = ac.SampleRate()
            info["channels"] = ac.ChannelLayout().Count()
        }
    }
    return info
}
```

---

### Посилання на батьківський демуксер:

```go
demuxer *Demuxer  // посилання на батьківський mkv демуксер
```

**Призначення**: дозволяє треку:
- Отримувати налаштування демуксера
- Доступ до спільних ресурсів (буфери, метрики)
- Викликати методи батька для читання

**⚠️ Увага**: Це посилання створює циклічну залежність. При копіюванні `Demuxer` потрібно оновлювати `stream.demuxer`.

---

### Ідентифікатори потоку:

```go
pid        uint16  // Program ID (з MPEG-TS, не використовується у MKV!)
streamId   uint8   // Stream ID (застаріле, не використовується у EBML)
streamType uint8   // Тип потоку (застаріле, не використовується у EBML)
```

### ⚠️ Критична проблема: поля з MPEG-TS у MKV демуксері

```
Проблема:
• pid, streamId, streamType — це поля з формату MPEG Transport Stream
• MKV/WebM використовують EBML формат з іншою структурою:
  - TrackNumber (variable-length) замість pid
  - TrackEntry ID замість streamId
  - CodecID рядок замість streamType

Наслідки:
• Ці поля ніде не використовуються у mkv.Demuxer
• Витрата пам'яті на непотрібні дані
• Плутанина для розробників: які поля дійсно потрібні?

✅ Виправлення: видалити непотрібні поля або задокументувати їх призначення:

type Stream struct {
    av.CodecData
    demuxer *Demuxer
    idx     int
    // ... тільки поля, що дійсно використовуються ...
}
```

---

### Індекси та буфери:

```go
idx        int      // індекс треку у масиві demuxer.streams
iskeyframe bool     // прапорець ключового кадру для поточного пакету
pts, dts   time.Duration  // Presentation/Decoding Time Stamp
data       []byte   // буфер для накопичення даних пакету
datalen    int      // довжина валідних даних у data буфері
```

**🔍 Призначення**:
- `idx`: для ідентифікації треку у `av.Packet.Idx`
- `iskeyframe`: для визначення точок seek та ключових кадрів
- `pts/dts`: для синхронізації аудіо/відео та розрахунку duration
- `data/datalen`: для інкрементального накопичення даних перед створенням пакету

**✅ Ваш use-case**: моніторинг стану буфера

```go
// GetBufferStatus — отримання інформації про буфер пакету
func (s *Stream) GetBufferStatus() BufferStatus {
    return BufferStatus{
        DataLen:    s.datalen,
        BufferCap:  cap(s.data),
        PTS:        s.pts,
        DTS:        s.dts,
        IsKeyFrame: s.iskeyframe,
    }
}

type BufferStatus struct {
    DataLen    int
    BufferCap  int
    PTS        time.Duration
    DTS        time.Duration
    IsKeyFrame bool
}

// Використання для моніторингу затримки:
status := stream.GetBufferStatus()
if status.DataLen > maxBufferSize {
    log.Printf("warning: large buffer: %d bytes", status.DataLen)
}
```

---

## 🔑 2. Таймінги: PTS, DTS, синхронізація

### 🔧 Призначення полів:

```go
pts, dts time.Duration  // час відтворення/декодування від початку потоку
```

**🔍 Різниця між PTS та DTS**:

```
DTS (Decoding Time Stamp):
• Коли семпл має бути декодований
• Критичний для порядку декодування (особливо для B-frames)

PTS (Presentation Time Stamp):
• Коли семпл має бути показаний/відтворений
• Критичний для синхронізації аудіо/відео

У MKV/WebM:
• SimpleBlock містить relative timecode (відносно Cluster)
• Потрібно конвертувати у абсолютні PTS/DTS
• Для H.264: PTS = DTS + composition offset (з ctts таблиці)
```

### ✅ Ваш use-case**: розрахунок точного часу відтворення

```go
// GetPresentationTime — розрахунок PTS для поточного семплу
func (s *Stream) GetPresentationTime() time.Duration {
    // Для простих випадків (без B-frames): PTS = DTS
    if s.pts == 0 {
        return s.dts
    }
    return s.pts
}

// GetDuration — розрахунок тривалості семплу
func (s *Stream) GetDuration() time.Duration {
    // У MKV duration може бути у самому пакеті або розраховуватися
    // як різниця між поточним та наступним DTS
    return 0  // потребує додаткової логіки
}
```

---

## 🔑 3. Буферизація даних: data/datalen

### 🔧 Призначення:

```go
data    []byte  // буфер для накопичення даних пакету
datalen int     // кількість валідних байт у data
```

**🔍 Життєвий цикл буфера**:

```
1. Ініціалізація:
   • data = nil або make([]byte, initialCapacity)
   • datalen = 0

2. Накопичення даних:
   • При читанні частин пакету: data[datalen:] = newBytes
   • datalen += len(newBytes)

3. Створення пакету:
   • Коли достатньо даних: створити av.Packet з data[:datalen]
   • Скинути datalen = 0 (або виділити новий буфер)

4. Оптимізація:
   • Якщо datalen == 0: reuse буфера для наступного пакету
   • Якщо data cap < needed: allocate новий більший буфер
```

### ⚠️ Критична проблема: відсутність логіки управління буфером

```
У поточній реалізації:
• data/datalen оголошені, але не використовуються у mkv.Demuxer.ReadPacket()
• Замість цього: дані читаються одразу у av.Packet.Data

Наслідки:
• Неможливість обробки фрагментованих пакетів
• Витрата пам'яті на непотрібні поля
• Плутанина: чи потрібна буферизація?

✅ Виправлення: або реалізувати буферизацію, або видалити поля:

// Варіант 1: Реалізувати буферизацію для великих пакетів
func (s *Stream) appendData(b []byte) {
    needed := s.datalen + len(b)
    if cap(s.data) < needed {
        // Збільшення буфера
        newData := make([]byte, needed, needed*2)
        copy(newData, s.data[:s.datalen])
        s.data = newData
    }
    copy(s.data[s.datalen:], b)
    s.datalen = needed
}

// Варіант 2: Видалити непотрібні поля
type Stream struct {
    av.CodecData
    demuxer *Demuxer
    idx     int
    // ... тільки використовувані поля ...
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Моніторинг стану треків у реальному часі

```go
// StreamMonitor — моніторинг здоров'я треків у mkv демуксері
type StreamMonitor struct {
    channelID string
    metrics   *StreamMetrics
}

type StreamMetrics struct {
    PacketsProcessed prometheus.CounterVec
    BufferHealth     prometheus.GaugeVec
    KeyFrameCount    prometheus.CounterVec
    SyncErrors       prometheus.CounterVec
}

// CheckStreamHealth — оцінка стану треку
func (m *StreamMonitor) CheckStreamHealth(stream *mkv.Stream) (health StreamHealth, err error) {
    // 1. Перевірка наявності кодека
    if stream.CodecData == nil {
        health.Status = StatusWaitingCodec
        health.Issues = append(health.Issues, "codec not initialized")
        return health, nil
    }
    
    // 2. Перевірка буфера
    if stream.datalen > maxBufferSize {
        health.Status = StatusBufferOverflow
        health.Issues = append(health.Issues, 
            fmt.Sprintf("large buffer: %d bytes", stream.datalen))
    }
    
    // 3. Перевірка таймінгів
    if stream.pts < stream.dts {
        health.Status = StatusTimeDrift
        health.Issues = append(health.Issues, "PTS < DTS")
        m.metrics.SyncErrors.WithLabelValues(m.channelID).Inc()
    }
    
    // 4. Статистика ключових кадрів
    if stream.iskeyframe {
        m.metrics.KeyFrameCount.WithLabelValues(m.channelID).Inc()
    }
    
    if len(health.Issues) == 0 {
        health.Status = StatusHealthy
    }
    return health, nil
}

type StreamHealth struct {
    Status StreamStatus
    Issues []string
}

type StreamStatus string

const (
    StatusHealthy        StreamStatus = "healthy"
    StatusWaitingCodec   StreamStatus = "waiting_codec"
    StatusBufferOverflow StreamStatus = "buffer_overflow"
    StatusTimeDrift      StreamStatus = "time_drift"
)
```

### 🔧 Приклад: Обробка змін кодека у реальному часі

```go
// ProcessStreamWithCodecChange — цикл обробки з підтримкою змін
func ProcessStreamWithCodecChange(demuxer *mkv.Demuxer, handler PacketHandler) error {
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF { break }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Перевірка чи змінився кодек (напр. зміна роздільної здатності)
        stream := demuxer.Streams()[pkt.Idx]
        if pkt.CodecData != nil && pkt.CodecData != stream.CodecData {
            // Оновлення кодека у треку
            stream.CodecData = pkt.CodecData
            log.Printf("Codec updated for stream %d", pkt.Idx)
        }
        
        // Обробка пакету
        if err := handler.HandlePacket(pkt); err != nil {
            return fmt.Errorf("handle packet: %w", err)
        }
    }
    
    return nil
}

// PacketHandler — інтерфейс для обробки прочитаних пакетів
type PacketHandler interface {
    HandlePacket(av.Packet) error
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Невикористані поля (pid, streamId)** | Витрата пам'яті, плутанина | Видалити поля або задокументувати їх призначення |
| **Буфер data не використовується** | Неможливість обробки фрагментованих пакетів | Реалізувати логіку буферизації або видалити поля |
| **PTS/DTS не синхронізовані** | Розсинхронізація аудіо/відео | Перевіряти `if pts < dts` та логувати попередження |
| **iskeyframe не оновлюється** | Неможливий seek, помилки декодування | Оновлювати прапорець при парсингу NALU type 5 (IDR) |
| **datalen > len(data)** | Паніка при доступі до data[datalen] | Завжди перевіряти `if datalen > len(data)` перед використанням |

---

## ⚡ Оптимізації для high-throughput обробки

### 1. Кешування буферів для даних:

```go
var packetBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір пакету: 1 кадр відео @ 1080p = ~2MB
        buf := make([]byte, 0, 2*1024*1024)
        return &buf
    },
}

func GetPacketBuffer() *[]byte { return packetBufferPool.Get().(*[]byte) }
func PutPacketBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    packetBufferPool.Put(b)
}

// Використання у обробці пакетів:
buf := GetPacketBuffer()
defer PutPacketBuffer(buf)
// ... використання buf для накопичення даних ...
```

### 2. Попередня аллокація буферів:

```go
// PreallocateBuffer — виділення місця для даних заздалегідь
func (s *Stream) PreallocateBuffer(expectedSize int) {
    if cap(s.data) < expectedSize {
        s.data = make([]byte, expectedSize, expectedSize*2)
    }
    s.datalen = 0  // скидання лічильника
}
```

### 3. Моніторинг продуктивності обробки:

```go
type StreamProcessingMetrics struct {
    PacketsPerSecond prometheus.GaugeVec
    ProcessingLatency prometheus.HistogramVec
    BufferGrowthRate prometheus.CounterVec
}

func (m *StreamProcessingMetrics) RecordPacket(packetSize int, duration time.Duration, streamID string) {
    m.PacketsPerSecond.WithLabelValues(streamID).Set(float64(packetSize) / duration.Seconds())
    m.ProcessingLatency.WithLabelValues(streamID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції mkv.Stream

```go
// ✅ 1. Ініціалізація кодека перед використанням
if stream.CodecData == nil {
    return fmt.Errorf("codec not initialized for stream %d", stream.idx)
}

// ✅ 2. Перевірка буфера перед записом
if stream.datalen+len(newData) > cap(stream.data) {
    // Збільшення буфера
    newDataSlice := make([]byte, stream.datalen+len(newData), (stream.datalen+len(newData))*2)
    copy(newDataSlice, stream.data[:stream.datalen])
    stream.data = newDataSlice
}

// ✅ 3. Оновлення таймінгів при читанні пакетів
stream.dts = calculatedDTS
stream.pts = calculatedPTS  // або stream.dts якщо немає composition offset

// ✅ 4. Встановлення iskeyframe для ключових кадрів
if naluType == 5 {  // H.264 IDR frame
    stream.iskeyframe = true
}

// ✅ 5. Скидання буфера після створення пакету
pkt := av.Packet{
    Data: append([]byte(nil), stream.data[:stream.datalen]...),  // копіювання даних
    // ... інші поля ...
}
stream.datalen = 0  // готовий до наступного пакету

// ✅ 6. Метрики для моніторингу
metrics.RecordPacket(len(pkt.Data), time.Since(start), streamID)

// ✅ 7. Логування для дебагу
if debug {
    log.Printf("Stream %d: PTS=%v, DTS=%v, keyframe=%v, datalen=%d", 
        stream.idx, stream.pts, stream.dts, stream.iskeyframe, stream.datalen)
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk mkv Package](https://pkg.go.dev/github.com/deepch/vdk/format/mkv) — GoDoc documentation
- 📄 [Matroska Specification](https://matroska.org/technical/specs/index.html) — офіційна специфікація формату
- 📄 [H.264/AVC NALU Structure](https://wiki.videolan.org/NAL/) — структура Network Abstraction Layer Units
- 🧪 [Go sync.Pool Best Practices](https://go.dev/blog/pool) — ефективне управління пам'яттю
- 📦 [Prometheus Metrics for Media](https://prometheus.io/docs/practices/instrumentation/) — моніторинг продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Видаліть непотрібні поля (pid, streamId, streamType)** — вони з MPEG-TS і не використовуються у MKV.
> 2. **Реалізуйте логіку буферизації або видаліть data/datalen** — уникнення плутанини та витрати пам'яті.
> 3. **Завжди оновлюйте iskeyframe при парсингу NALU** — критично для seek та коректного декодування.
> 4. **Перевіряйте PTS >= DTS** — уникнення розсинхронізації аудіо/відео.
> 5. **Кешуйте буфери через sync.Pool** — уникнення аллокацій критично для high-FPS потоків.

Потрібен приклад реалізації повноцінної буферизації даних у `mkv.Stream` для обробки великих або фрагментованих пакетів? Готовий допомогти! 🚀