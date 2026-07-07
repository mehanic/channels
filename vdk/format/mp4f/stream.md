# 📦 Глибокий розбір: `mp4f.Stream` — Стан окремого треку у фрагментованому MP4 (fMP4)

Цей файл — **структура для представлення окремого медіа-треку** у межах fMP4 муксера. Вона містить стан для фрагментації, буфери для накопичення даних, таймінги, та посилання на батьківський муксер. Ця структура є критичною для інкрементальної генерації `moof` + `mdat` пар для low-latency streaming.

---

## 🗺️ Архітектурна схема mp4f.Stream

```
┌────────────────────────────────────────┐
│ 📦 mp4f.Stream — fMP4 Track State     │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • av.CodecData — метадані кодека      │
│  • trackAtom *mp4io.Track — MP4 атоми  │
│  • sample *mp4io.SampleTable — таблиці │
│  • moof mp4fio.MovieFrag — поточний фрагмент│
│  • buffer []byte — буфер для mdat даних│
│                                         │
│  ⏱️ Таймінги та індекси:               │
│  • timeScale — частота дискретизації   │
│  • dts/stts/ctts — decoding/composition│
│  • sampleIndex — лічильник семплів     │
│                                         │
│  🔄 Ролі:                               │
│  • Накопичення пакетів у буфер         │
│  • Генерація TrackFragRun Entries      │
│  • Фрагментація на moof+mdat пари      │
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
func GetTrackInfo(stream *mp4f.Stream) map[string]interface{} {
    info := map[string]interface{}{
        "codec": stream.Type().String(),
        "codecString": stream.codecString,  // напр. "avc1.42001e"
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

### Посилання на батьківський муксер:

```go
muxer *Muxer  // посилання на батьківський fMP4 муксер
```

**Призначення**: дозволяє треку:
- Отримувати налаштування муксера (`maxFrames`, `fragmentIndex`)
- Оновлювати глобальний лічильник фрагментів
- Доступ до спільних ресурсів (буфери, метрики)

**⚠️ Увага**: Це посилання створює циклічну залежність. При копіюванні `Muxer` потрібно оновлювати `stream.muxer`.

---

### Поточний фрагмент та буфер:

```go
moof   mp4fio.MovieFrag  // поточний moof атом (метадані фрагменту)
buffer []byte            // буфер для mdat даних (сира медіа-інформація)
```

**🔍 Життєвий цикл фрагменту**:

```
1. writePacketV2/V3/V4():
   • Якщо sampleIndex == 0 → ініціалізація нового moof
   • Додавання TrackFragRun Entry для семплу
   • Апенд даних у buffer (mdat payload)

2. При досягненні ліміту (maxFrames або GOP):
   • Встановлення DataOffset у TrackFragRun
   • Серіалізація moof у байти
   • Оновлення розміру mdat у buffer
   • Об'єднання moof + mdat у один буфер
   • Скидання sampleIndex для наступного фрагменту

3. Finalize():
   • Примусова фіналізація накопичених даних
   • Повернення останнього фрагменту
```

**✅ Ваш use-case**: моніторинг стану буфера

```go
// GetBufferStatus — отримання інформації про буфер фрагменту
func (s *Stream) GetBufferStatus() BufferStatus {
    return BufferStatus{
        SampleCount:   s.sampleIndex,
        BufferSize:    len(s.buffer),
        DTS:           s.dts,
        FragmentIndex: s.muxer.fragmentIndex,
        MoofSize:      s.moof.Len(),
    }
}

type BufferStatus struct {
    SampleCount   int
    BufferSize    int
    DTS           int64
    FragmentIndex int
    MoofSize      int
}

// Використання для моніторингу затримки:
status := stream.GetBufferStatus()
if status.BufferSize > maxBufferSize {
    log.Printf("warning: large buffer: %d bytes, %d samples", 
        status.BufferSize, status.SampleCount)
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
- fMP4: може бути довільним, але має співпадати з треком у moov

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
- Підтримки інкрементального додавання семплів у фрагмент
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

**⚠️ У fMP4 не всі таблиці використовуються**:
- `stco`, `stsz` — не потрібні, бо дані у mdat, а не у файлі
- `stsc` — спрощено, бо кожен фрагмент — окремий "чанк"
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
// StreamMonitor — моніторинг здоров'я треків у fMP4 муксері
type StreamMonitor struct {
    channelID string
    metrics   *StreamMetrics
}

type StreamMetrics struct {
    SamplesProcessed prometheus.CounterVec
    FragmentLatency  prometheus.HistogramVec
    BufferHealth     prometheus.GaugeVec
    KeyFrameCount    prometheus.CounterVec
}

// CheckStreamHealth — оцінка стану треку
func (m *StreamMonitor) CheckStreamHealth(stream *mp4f.Stream) (health StreamHealth, err error) {
    // 1. Перевірка наявності кодека
    if stream.CodecData == nil {
        health.Status = StatusWaitingCodec
        health.Issues = append(health.Issues, "codec not initialized")
        return health, nil
    }
    
    // 2. Перевірка буфера фрагменту
    status := stream.GetBufferStatus()
    if status.BufferSize > 10*1024*1024 {  // 10MB ліміт
        health.Status = StatusBufferOverflow
        health.Issues = append(health.Issues, 
            fmt.Sprintf("large buffer: %d bytes", status.BufferSize))
    }
    
    // 3. Перевірка таймінгів
    if status.DTS < 0 {
        health.Status = StatusTimeDrift
        health.Issues = append(health.Issues, "negative DTS")
    }
    
    // 4. Статистика
    if stream.Type() == av.H264 {
        // Підрахунок ключових кадрів (спрощено)
        // У реальності: відстеження через syncSampleIndex
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
    StatusHealthy         StreamStatus = "healthy"
    StatusWaitingCodec    StreamStatus = "waiting_codec"
    StatusBufferOverflow  StreamStatus = "buffer_overflow"
    StatusTimeDrift       StreamStatus = "time_drift"
)
```

### 🔧 Приклад: Обробка змін кодека у реальному часі

```go
// ProcessStreamWithCodecChange — цикл обробки з підтримкою змін
func ProcessStreamWithCodecChange(muxer *mp4f.Muxer, handler PacketHandler) error {
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
        
        // Фрагментація з підтримкою GOP
        gotFragment, fragment, err := muxer.WritePacket(pkt, pkt.IsKeyFrame)
        if err != nil {
            return fmt.Errorf("write packet: %w", err)
        }
        
        if gotFragment {
            // Обробка готового фрагменту
            if err := handler.HandleFragment(fragment); err != nil {
                return fmt.Errorf("handle fragment: %w", err)
            }
        }
    }
    
    // Фіналізація останнього фрагменту
    if finalFragment := muxer.Finalize(); len(finalFragment) > 0 {
        handler.HandleFragment(finalFragment)
    }
    
    return nil
}

// PacketHandler — інтерфейс для обробки фрагментів
type PacketHandler interface {
    HandleFragment([]byte) error
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Переповнення при конвертації часу** | Неправильні таймінги для довгих потоків | Використовуйте `safeTimeToTs()` з float64 проміжними обчисленнями |
| **Невірний розрахунок PTS** | Розсинхронізація аудіо/відео | Перевірте наявність та коректність ctts таблиці |
| **Буфер переповнюється** | Високе споживання пам'яті, затримки | Додайте ліміт розміру буфера та примусову фіналізацію |
| **Відсутні ключові кадри** | Неможливий seek, помилки декодування | Переконайтеся, що `pkt.IsKeyFrame` коректно встановлено для H.264 IDR кадрів |
| **Некоректний DataOffset** | Плеєр не може знайти дані у mdat | Перевірте розрахунок `moof.Len() + 8` при фіналізації |

---

## ⚡ Оптимізації для high-throughput фрагментації

### 1. Кешування буферів для mdat:

```go
var mdatBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір mdat: 1с відео @ 2Mbps = 250KB
        buf := make([]byte, 0, 256*1024)
        return &buf
    },
}

func GetMdatBuffer() *[]byte { return mdatBufferPool.Get().(*[]byte) }
func PutMdatBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    mdatBufferPool.Put(b)
}

// Використання у writePacketV2/V3:
if stream.buffer == nil {
    stream.buffer = *GetMdatBuffer()
}
// ... додавання даних ...
// Після фіналізації:
PutMdatBuffer(&stream.buffer)
```

### 2. Попередня аллокація TrackFragRun Entries:

```go
// PreallocateRunEntries — виділення місця для Entries заздалегідь
func (s *Stream) PreallocateRunEntries(maxSamples int) {
    if s.moof.Tracks == nil || len(s.moof.Tracks) == 0 {
        return
    }
    run := s.moof.Tracks[0].Run
    if run.Entries == nil || cap(run.Entries) < maxSamples {
        run.Entries = make([]mp4io.TrackFragRunEntry, 0, maxSamples)
    }
}
```

### 3. Моніторинг продуктивності фрагментації:

```go
type FragmentationMetrics struct {
    SamplesPerFragment prometheus.HistogramVec
    FragmentSize       prometheus.HistogramVec
    GOPAlignment       prometheus.CounterVec
    BufferAllocations  prometheus.CounterVec
}

func (m *FragmentationMetrics) RecordFragment(sampleCount int, size int, aligned bool, streamID string) {
    m.SamplesPerFragment.WithLabelValues(streamID).Observe(float64(sampleCount))
    m.FragmentSize.WithLabelValues(streamID).Observe(float64(size))
    if aligned {
        m.GOPAlignment.WithLabelValues(streamID).Inc()
    }
}
```

---

## 📋 Чек-лист інтеграції mp4f.Stream

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

// ✅ 6. Моніторинг розміру буфера
if len(stream.buffer) > maxBufferSize {
    // Примусова фіналізація або логування
}

// ✅ 7. Метрики для моніторингу
metrics.RecordFragment(stream.sampleIndex, len(stream.buffer), pkt.IsKeyFrame, streamID)

// ✅ 8. Логування для дебагу
if debug {
    log.Printf("Stream %d: sample %d, DTS=%d, buffer=%d bytes", 
        stream.idx, stream.sampleIndex, stream.dts, len(stream.buffer))
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk mp4f Package](https://pkg.go.dev/github.com/deepch/vdk/format/mp4f) — GoDoc documentation
- 📄 [ISO/IEC 23009-1 (DASH)](https://www.iso.org/standard/79329.html) — стандарт для fMP4 у streaming
- 📄 [CMAF Specification](https://www.iso.org/standard/74428.html) — Common Media Application Format
- 🧪 [Go time.Duration Best Practices](https://go.dev/doc/effective_go#time) — робота з часом у Go
- 📦 [Prometheus Metrics for Media](https://prometheus.io/docs/practices/instrumentation/) — моніторинг продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди використовуйте безпечну конвертацію часу** — переповнення int64 призведе до некоректних таймінгів.
> 2. **Кешуйте буфери через sync.Pool** — уникнення аллокацій критично для high-FPS потоків.
> 3. **Моніторьте розмір буфера** — різке зростання може вказувати на проблеми з фіналізацією фрагментів.
> 4. **Перевіряйте наявність ctts для відео з B-frames** — інакше PTS буде некоректним.
> 5. **Використовуйте GOP-базовану фрагментацію (V3)** — для low-latency streaming та ефективного seek.

Потрібен приклад реалізації `PacketHandler` для вашого `mse.Muxer`, що відправляє fMP4 фрагменти через WebSocket для MSE відтворення у браузері? Готовий допомогти! 🚀