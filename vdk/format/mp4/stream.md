# 📦 Глибокий розбір: `mp4.Stream` — Стан окремого треку у MP4 контейнері

Цей файл — **структура для представлення окремого медіа-треку** у межах `mp4.Muxer`/`mp4.Demuxer`. Вона містить стан для фрагментації, буфери для накопичення даних, таймінги, та посилання на батьківський муксер/демуксер. Ця структура є критичною для інкрементальної обробки `av.Packet` у стандартний формат `moov` + `mdat`.

---

## 🗺️ Архітектурна схема mp4.Stream

```
┌────────────────────────────────────────┐
│ 📦 mp4.Stream — MP4 Track State       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • av.CodecData — метадані кодека      │
│  • trackAtom *mp4io.Track — MP4 атоми  │
│  • sample *mp4io.SampleTable — таблиці │
│                                         │
│  ⏱️ Таймінги та індекси:               │
│  • timeScale — частота дискретизації   │
│  • dts/stts/ctts — decoding/composition│
│  • sampleIndex — лічильник семплів     │
│                                         │
│  🔄 Ролі:                               │
│  • Накопичення пакетів у буфер         │
│  • Генерація SampleTable Entries       │
│  • Синхронізація аудіо/відео через DTS │
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
func GetTrackInfo(stream *mp4.Stream) map[string]interface{} {
    info := map[string]interface{}{
        "codec": stream.Type().String(),
        "timeScale": stream.timeScale,
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

### Посилання на батьківський муксер/демуксер:

```go
muxer   *Muxer   // посилання на батьківський mp4 муксер
demuxer *Demuxer // посилання на батьківський mp4 демуксер
```

**Призначення**: дозволяє треку:
- Отримувати налаштування (`NegativeTsMakeZero`, `wpos`)
- Доступ до спільних ресурсів (буфери, метрики)
- Викликати методи батька для запису/читання

**⚠️ Увага**: Це посилання створює циклічну залежність. При копіюванні `Muxer`/`Demuxer` потрібно оновлювати `stream.muxer`/`stream.demuxer`.

---

### Поточний трек та таблиці семплів:

```go
trackAtom *mp4io.Track      // mp4 атом для цього треку (trak box)
sample    *mp4io.SampleTable // таблиці для навігації по семплах
```

**🔍 Життєвий цикл треку**:

```
1. newStream():
   • Створення trackAtom з базовими метаданими
   • Ініціалізація порожніх таблиць (stts, stsc, stsz, тощо)

2. fillTrackAtom() (для муксера):
   • Заповнення кодек-специфічних полів (AVC1Desc, MP4ADesc)
   • Встановлення timeScale, Duration, Handler

3. writePacket() (для муксера):
   • Додавання записів у таблиці (stts, stsz, stco, ctts, stss)
   • Оновлення dts, duration, sampleIndex

4. readPacket() (для демуксера):
   • Читання записів з таблиць для навігації
   • Розрахунок PTS/DTS для синхронізації
```

**✅ Ваш use-case**: моніторинг стану таблиць

```go
// GetTableStats — отримання статистики таблиць семплів
func (s *Stream) GetTableStats() TableStats {
    stats := TableStats{
        SampleCount: s.sampleIndex,
        Duration:    s.tsToTime(s.duration),
    }
    
    if s.sample.TimeToSample != nil {
        stats.STTSEntries = len(s.sample.TimeToSample.Entries)
    }
    if s.sample.SampleSize != nil {
        stats.STSZEntries = len(s.sample.SampleSize.Entries)
    }
    if s.sample.SyncSample != nil {
        stats.KeyFrameCount = len(s.sample.SyncSample.Entries)
    }
    
    return stats
}

type TableStats struct {
    SampleCount  int
    Duration     time.Duration
    STTSEntries  int  // time-to-sample entries
    STSZEntries  int  // sample size entries
    KeyFrameCount int // sync sample entries
}

// Використання для моніторингу якості запису:
stats := stream.GetTableStats()
if stats.KeyFrameCount == 0 {
    log.Printf("warning: no keyframes in stream %d", s.idx)
}
```

---

## 🔑 2. Таймінги: timeScale, DTS, конвертації

### 🔧 timeScale — частота дискретизації:

```go
timeScale int64  // ticks per second для цього треку
```

**🔍 Значення за стандартом**:
- Відео (H.264/H.265): зазвичай **90000** (сумісність з MPEG-TS)
- Аудіо (AAC): зазвичай **частота дискретизації** (напр. 48000)
- MP4: кожен трек може мати власний timeScale

**✅ Ваш use-case**: конвертація між time.Duration та ticks

```go
// Конвертація з time.Duration у ticks треку
ticks := stream.timeToTs(100 * time.Millisecond)  // напр. 9000 ticks @ 90kHz

// Конвертація з ticks у time.Duration
duration := stream.tsToTime(9000)  // 100ms
```

### 🔧 Глобальні та методи конвертації:

```go
// Глобальні функції (з явним timeScale)
func timeToTs(tm time.Duration, timeScale int64) int64 {
    return int64(tm * time.Duration(timeScale) / time.Second)
}

func tsToTime(ts int64, timeScale int64) time.Duration {
    return time.Duration(ts) * time.Second / time.Duration(timeScale)
}

// Методи структури (використовують self.timeScale)
func (obj *Stream) timeToTs(tm time.Duration) int64 {
    return int64(tm * time.Duration(obj.timeScale) / time.Second)
}

func (obj *Stream) tsToTime(ts int64) time.Duration {
    return time.Duration(ts) * time.Second / time.Duration(obj.timeScale)
}
```

### ⚠️ Критичний момент: переповнення при конвертації

```
Проблема: 
  time.Duration — це int64 наносекунд
  timeScale — зазвичай 90000 (ticks/сек)
  
  При конвертації: tm * timeScale може переповнити int64!

Приклад:
  tm = 100 hours = 360,000,000,000,000 ns
  timeScale = 90000
  tm * timeScale = 3.24e19 > max int64 (9.22e18) → переповнення!

✅ Виправлення: використовувати float64 для проміжних обчислень
```

```go
// Безпечна конвертація без переповнення
func (s *Stream) safeTimeToTs(tm time.Duration) int64 {
    // Конвертація у секунди як float64
    seconds := float64(tm) / float64(time.Second)
    // Множення на timeScale
    ticks := seconds * float64(s.timeScale)
    return int64(ticks)
}

func (s *Stream) safeTsToTime(ts int64) time.Duration {
    seconds := float64(ts) / float64(s.timeScale)
    return time.Duration(seconds * float64(time.Second))
}
```

---

## 🔑 3. Індекси для навігації по таблицях семплів

### 🔧 Поля для стиснутих таблиць:

```go
// Для stts (Time-To-Sample):
sttsEntry              *mp4io.TimeToSampleEntry  // поточний запис
sttsEntryIndex         int                        // індекс запису у таблиці
sampleIndexInSttsEntry int                        // позиція у межах запису

// Для ctts (Composition Offset):
cttsEntry              *mp4io.CompositionOffsetEntry
cttsEntryIndex         int
sampleIndexInCttsEntry int

// Для stsc (Sample-To-Chunk) та інших:
chunkGroupIndex        int
chunkIndex             int
sampleIndexInChunk     int
sampleOffsetInChunk    int64
syncSampleIndex        int  // для stss (ключові кадри)
```

**🔍 Призначення**: Ці поля використовуються для:
- Ефективного розрахунку DTS/PTS без повного перебору таблиць
- Підтримки інкрементального додавання семплів у файл
- Синхронізації аудіо/відео через спільні таймінги

**✅ Ваш use-case**: розрахунок точного часу відтворення

```go
// GetPresentationTime — розрахунок PTS для поточного семплу
func (s *Stream) GetPresentationTime() (time.Duration, error) {
    // 1. Розрахунок DTS через stts
    dts := s.dts  // накопичений DTS
    
    // 2. Додавання composition offset через ctts
    var offset int64
    if s.cttsEntry != nil && s.cttsEntryIndex < len(s.sample.CompositionOffset.Entries) {
        offset = int64(s.cttsEntry.Offset)
    }
    
    pts := dts + offset
    return s.tsToTime(pts), nil
}
```

---

## 🔑 4. Інтеграція з mp4io таблицями

### 🔧 sample *mp4io.SampleTable — посилання на таблиці:

```go
sample *mp4io.SampleTable  // посилання на таблиці семплів з mp4io
```

**🔍 Структура SampleTable**:

```
SampleTable (stbl) містить:
• SampleDesc (stsd) — опис кодеків (AVC1Desc, MP4ADesc)
• TimeToSample (stts) — розрахунок DTS
• CompositionOffset (ctts) — розрахунок PTS = DTS + offset
• SampleToChunk (stsc) — мапінг семплів у чанки
• SyncSample (stss) — індекси ключових кадрів
• ChunkOffset (stco) — позиції чанків у файлі
• SampleSize (stsz) — розміри семплів
```

**⚠️ У муксері не всі таблиці використовуються однаково**:
- `stco`, `stsz` — заповнюються під час запису пакетів
- `stsc` — спрощено, бо кожен семпл — окремий "чанк" у цьому реалізації
- `stts`, `ctts`, `stss` — критичні для таймінгів та seek

**✅ Ваш use-case**: валідація наявності необхідних таблиць

```go
// ValidateSampleTable — перевірка наявності критичних таблиць
func ValidateSampleTable(st *mp4io.SampleTable) error {
    if st.SampleDesc == nil {
        return fmt.Errorf("missing SampleDesc (stsd)")
    }
    if st.TimeToSample == nil {
        return fmt.Errorf("missing TimeToSample (stts)")
    }
    // ctts опціональний для відео без B-frames
    // stss опціональний, але бажаний для seek
    return nil
}

// Використання при ініціалізації треку:
if err := ValidateSampleTable(stream.sample); err != nil {
    return fmt.Errorf("invalid sample table: %w", err)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Моніторинг стану треків у реальному часі

```go
// StreamMonitor — моніторинг здоров'я треків у mp4 муксері/демуксері
type StreamMonitor struct {
    channelID string
    metrics   *StreamMetrics
}

type StreamMetrics struct {
    SamplesProcessed prometheus.CounterVec
    WriteLatency     prometheus.HistogramVec
    BufferHealth     prometheus.GaugeVec
    KeyFrameCount    prometheus.CounterVec
}

// CheckStreamHealth — оцінка стану треку
func (m *StreamMonitor) CheckStreamHealth(stream *mp4.Stream) (health StreamHealth, err error) {
    // 1. Перевірка наявності кодека
    if stream.CodecData == nil {
        health.Status = StatusWaitingCodec
        health.Issues = append(health.Issues, "codec not initialized")
        return health, nil
    }
    
    // 2. Перевірка таймінгів
    if stream.duration < 0 {
        health.Status = StatusTimeDrift
        health.Issues = append(health.Issues, "negative duration")
    }
    
    // 3. Статистика ключових кадрів (для відео)
    if stream.Type() == av.H264 || stream.Type() == av.H265 {
        if stream.sample.SyncSample != nil {
            keyframes := len(stream.sample.SyncSample.Entries)
            if keyframes == 0 && stream.sampleIndex > 30 {
                health.Issues = append(health.Issues, "no keyframes after 30 samples")
            }
            m.metrics.KeyFrameCount.WithLabelValues(m.channelID).Add(float64(keyframes))
        }
    }
    
    // 4. Статистика обробки
    m.metrics.SamplesProcessed.WithLabelValues(
        stream.Type().String(), 
        m.channelID,
    ).Inc()
    
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
    StatusTimeDrift      StreamStatus = "time_drift"
    StatusMissingKeyframes StreamStatus = "missing_keyframes"
)
```

### 🔧 Приклад: Обробка змін кодека у реальному часі

```go
// ProcessStreamWithCodecChange — цикл обробки з підтримкою змін
func ProcessStreamWithCodecChange(muxer *mp4.Muxer, handler PacketHandler) error {
    for {
        pkt, err := source.ReadPacket()
        if err == io.EOF { break }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Перевірка чи змінився кодек (напр. зміна роздільної здатності)
        if pkt.CodecData != nil && pkt.CodecData != muxer.Streams()[pkt.Idx].CodecData {
            // Оновлення кодека у треку
            stream := muxer.Streams()[pkt.Idx]
            stream.CodecData = pkt.CodecData
            if err := stream.fillTrackAtom(); err != nil {
                return fmt.Errorf("update codec: %w", err)
            }
            log.Printf("Codec updated for stream %d", pkt.Idx)
        }
        
        // Запис пакету
        if err := muxer.WritePacket(pkt); err != nil {
            return fmt.Errorf("write packet: %w", err)
        }
    }
    
    // Фіналізація файлу
    return muxer.WriteTrailer()
}

// PacketHandler — інтерфейс для обробки записаних даних
type PacketHandler interface {
    HandlePacket(av.Packet) error
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Переповнення при конвертації часу** | Неправильні таймінги для довгих потоків | Використовуйте `safeTimeToTs()` з float64 проміжними обчисленнями |
| **Невірний розрахунок PTS** | Розсинхронізація аудіо/відео | Перевірте наявність та коректність ctts таблиці |
| **Відсутні ключові кадри** | Неможливий seek, помилки декодування | Переконайтеся, що `pkt.IsKeyFrame` коректно встановлено для H.264 IDR кадрів |
| **Некоректний sampleOffsetInChunk** | Дані читаються з неправильної позиції | Перевірте логіку оновлення `sampleOffsetInChunk` у `writePacket()` |
| **Різні timeScale у треках** | Неточності у синхронізації | Нормалізуйте часи до спільної шкали перед порівнянням |

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

// Використання у writePacket:
buf := GetPacketBuffer()
defer PutPacketBuffer(buf)
// ... використання buf для накопичення даних ...
```

### 2. Попередня аллокація таблиць:

```go
// PreallocateTables — виділення місця для таблиць заздалегідь
func (s *Stream) PreallocateTables(expectedSamples int) {
    if s.sample.TimeToSample != nil {
        s.sample.TimeToSample.Entries = make([]mp4io.TimeToSampleEntry, 0, expectedSamples/100)
    }
    if s.sample.SampleSize != nil {
        s.sample.SampleSize.Entries = make([]uint32, 0, expectedSamples)
    }
    if s.sample.ChunkOffset != nil {
        s.sample.ChunkOffset.Entries = make([]uint32, 0, expectedSamples)
    }
    if s.sample.SyncSample != nil {
        s.sample.SyncSample.Entries = make([]uint32, 0, expectedSamples/30)  // ~1 keyframe per second @ 30fps
    }
}
```

### 3. Моніторинг продуктивності обробки:

```go
type StreamProcessingMetrics struct {
    SamplesPerSecond prometheus.GaugeVec
    ProcessingLatency prometheus.HistogramVec
    TableGrowthRate  prometheus.CounterVec
}

func (m *StreamProcessingMetrics) RecordSample(sampleIndex int, duration time.Duration, streamID string) {
    m.SamplesPerSecond.WithLabelValues(streamID).Set(float64(sampleIndex) / duration.Seconds())
    m.ProcessingLatency.WithLabelValues(streamID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції mp4.Stream

```go
// ✅ 1. Ініціалізація timeScale з метаданих кодека
stream.timeScale = int64(codec.SampleRate())  // для аудіо
// або 90000 для відео

// ✅ 2. Безпечна конвертація часу без переповнення
ticks := stream.safeTimeToTs(pkt.Time)

// ✅ 3. Перевірка наявності таблиць перед використанням
if stream.sample.TimeToSample == nil {
    return fmt.Errorf("stts table missing")
}

// ✅ 4. Оновлення індексів при додаванні семплів
stream.sampleIndex++
stream.dts += stream.timeToTs(pkt.Duration)

// ✅ 5. Обробка ключових кадрів для stss таблиці
if pkt.IsKeyFrame {
    // Додавання у syncSampleIndex або відповідну таблицю
}

// ✅ 6. Моніторинг розміру таблиць
if len(stream.sample.SampleSize.Entries) > maxEntries {
    // Логування або оптимізація
}

// ✅ 7. Метрики для моніторингу
metrics.RecordSample(stream.sampleIndex, time.Since(start), streamID)

// ✅ 8. Логування для дебагу
if debug {
    log.Printf("Stream %d: sample %d, DTS=%d, duration=%d", 
        stream.idx, stream.sampleIndex, stream.dts, stream.duration)
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk mp4 Package](https://pkg.go.dev/github.com/deepch/vdk/format/mp4) — GoDoc documentation
- 📄 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [MP4 Sample Tables Explained](https://wiki.multimedia.cx/index.php/MP4) — stts/ctts/stsc детальний опис
- 🧪 [Go time.Duration Best Practices](https://go.dev/doc/effective_go#time) — робота з часом у Go
- 📦 [Prometheus Metrics for Media](https://prometheus.io/docs/practices/instrumentation/) — моніторинг продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди використовуйте безпечну конвертацію часу** — переповнення int64 призведе до некоректних таймінгів.
> 2. **Кешуйте буфери через sync.Pool** — уникнення аллокацій критично для high-FPS потоків.
> 3. **Моніторьте розмір таблиць** — різке зростання може вказувати на проблеми з фрагментацією.
> 4. **Перевіряйте наявність ctts для відео з B-frames** — інакше PTS буде некоректним.
> 5. **Використовуйте GOP-базовану обробку для відео** — для ефективного seek та низької затримки.

Потрібен приклад реалізації `PacketHandler` для вашого `mse.Muxer`, що відправляє MP4 пакети через WebSocket для MSE відтворення у браузері? Готовий допомогти! 🚀