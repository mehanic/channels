# 📦 Глибокий розбір: `mp4.Stream` — Обробка окремого треку у MP4 контейнері

Цей файл — **структура для представлення окремого медіа-треку** (відео або аудіо) у межах MP4 контейнера. Вона містить стан для муксингу/демуксингу, таймінги, індекси для пошуку семплів, та посилання на батьківський контейнер.

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
│  • sample *mp4io.SampleTable — таблиця│
│  • muxer/demuxer — посилання на батька│
│                                         │
│  ⏱️ Таймінги та індекси:               │
│  • timeScale — частота дискретизації   │
│  • dts/stts/ctts — decoding/composition│
│  • chunk/sample індекси для пошуку     │
│                                         │
│  🔄 Ролі:                               │
│  • Muxer: av.Packet → MP4 атоми       │
│  • Demuxer: MP4 атоми → av.Packet     │
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
        "trackID": stream.trackAtom.TrackHeader.TrackID,
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

### Посилання на батьківський контейнер:

```go
muxer   *Muxer   // для запису у файл
demuxer *Demuxer // для читання з файлу
```

**Призначення**: дозволяє треку:
- Отримувати налаштування контейнера
- Доступ до спільних ресурсів (буфери, метрики)
- Сигналізувати про події (напр. завершення запису)

**⚠️ Увага**: Ці посилання взаємовиключні — у один момент часу активне тільки одне.

---

### MP4 атоми та структури:

```go
trackAtom *mp4io.Track  // кореневий атом треку (trak box)
sample    *mp4io.SampleTable  // таблиця семплів (stbl box)
```

**🔍 Структура MP4 треку**:

```
trak (Track Atom)
├─ tkhd (Track Header) — метадані треку (ID, duration, volume)
├─ mdia (Media Atom)
│  ├─ mdhd (Media Header) — timeScale, language, duration
│  ├─ hdlr (Handler Reference) — тип: vide/soun
│  └─ minf (Media Information)
│     ├─ vmif/smif (Video/Sound Media Info)
│     ├─ dinf (Data Information)
│     └─ stbl (Sample Table) ← sample поле
│        ├─ stsd (Sample Description) — кодек, параметри
│        ├─ stts (Time-To-Sample) — DTS розрахунок
│        ├─ ctts (Composition Offset) — PTS = DTS + offset
│        ├─ stsc (Sample-To-Chunk) — мапінг семплів у чанки
│        ├─ stsz (Sample Size) — розміри семплів
│        ├─ stco/co64 (Chunk Offset) — позиції чанків у файлі
│        └─ stss (Sync Sample) — індекси ключових кадрів
```

**✅ Ваш use-case**: навігація по атомах

```go
// FindSyncSample — пошук найближчого ключового кадру
func FindSyncSample(stream *mp4.Stream, targetDTS int64) (int64, error) {
    if stream.sample.SyncSample == nil {
        return 0, fmt.Errorf("no sync sample table")
    }
    
    // Бінарний пошук у stss таблиці
    entries := stream.sample.SyncSample.Entry
    idx := sort.Search(len(entries), func(i int) bool {
        return entries[i] >= targetDTS
    })
    
    if idx >= len(entries) {
        return 0, fmt.Errorf("no sync sample after DTS %d", targetDTS)
    }
    
    return int64(entries[idx]), nil
}
```

---

## 🔑 2. Таймінги: timeScale, DTS, STTS, CTTS

### 🔧 timeScale — частота дискретизації:

```go
timeScale int64  // ticks per second для цього треку
```

**🔍 Значення за стандартом**:
- Відео (H.264/H.265): зазвичай **90000** (сумісність з MPEG-TS)
- Аудіо (AAC): зазвичай **48000** або **44100** (частота дискретизації)
- Інші: можуть бути довільними, але кратними 1000 для зручності

**✅ Ваш use-case**: конвертація між time.Duration та ticks

```go
// Конвертація з time.Duration у ticks треку
ticks := stream.timeToTs(100 * time.Millisecond)  // напр. 9000 ticks @ 90kHz

// Конвертація з ticks у time.Duration
duration := stream.tsToTime(9000)  // 100ms
```

### 🔧 DTS/PTS розрахунок через STTS/CTTS:

```go
dts                    int64  // поточний Decoding Time Stamp
sttsEntryIndex         int    // індекс поточного запису у stts таблиці
sampleIndexInSttsEntry int    // позиція у межах поточного stts запису

cttsEntryIndex         int    // аналогічно для ctts (Composition Offset)
sampleIndexInCttsEntry int
```

**🔍 Як працює STTS (Time-To-Sample)**:

```
stts таблиця зберігає стиснуту інформацію про тривалість семплів:

struct TimeToSampleEntry {
    Count uint32  // кількість послідовних семплів з цією тривалістю
    Delta uint32  // тривалість у ticks
}

Приклад:
  [ {Count: 25, Delta: 1000}, {Count: 1, Delta: 1001} ]
  → 25 семплів по 1000 ticks, потім 1 семпл 1001 ticks
  → компенсує дрібні відхилення частоти кадрів

Розрахунок DTS для семплу #42:
1. Знайти запис у stts: entryIndex = 0 (бо 42 < 25)
2. DTS = entryIndex * Delta = 42 * 1000 = 42000 ticks
```

**🔍 Як працює CTTS (Composition Offset)**:

```
ctts таблиця зберігає зсув між DTS та PTS:

PTS = DTS + CompositionOffset

Це потрібно для:
• B-frames у відео (декодуються не в порядку відтворення)
• Аудіо з затримкою (напр. через буферизацію)

struct CompositionOffsetEntry {
    Count  uint32  // кількість семплів з цим зсувом
    Offset int32   // зсув у ticks (може бути від'ємним!)
}

Приклад для відео з B-frames:
  Семпл 0 (I-frame): DTS=0, Offset=0 → PTS=0
  Семпл 1 (B-frame): DTS=2, Offset=-1 → PTS=1 (відтворюється раніше)
  Семпл 2 (P-frame): DTS=1, Offset=0 → PTS=1
```

**✅ Ваш use-case**: розрахунок точного часу відтворення

```go
// GetPresentationTime — розрахунок PTS для семплу
func (s *Stream) GetPresentationTime(sampleIndex int) (time.Duration, error) {
    // 1. Розрахунок DTS через stts
    dts, err := s.calculateDTS(sampleIndex)
    if err != nil { return 0, err }
    
    // 2. Додавання composition offset через ctts
    var offset int64
    if s.sample.CompositionOffset != nil && len(s.sample.CompositionOffset.Entry) > 0 {
        offset = s.getCompositionOffset(sampleIndex)
    }
    
    pts := dts + offset
    return s.tsToTime(pts), nil
}

func (s *Stream) calculateDTS(sampleIndex int) (int64, error) {
    if s.sample.TimeToSample == nil {
        return 0, fmt.Errorf("no stts table")
    }
    
    remaining := sampleIndex
    var totalTicks int64
    
    for _, entry := range s.sample.TimeToSample.Entry {
        if remaining < int(entry.Count) {
            totalTicks += int64(remaining) * int64(entry.Delta)
            return totalTicks, nil
        }
        totalTicks += int64(entry.Count) * int64(entry.Delta)
        remaining -= int(entry.Count)
    }
    
    return 0, fmt.Errorf("sample index %d out of range", sampleIndex)
}
```

---

## 🔑 3. Навігація по чанках та семплах

### 🔧 Індекси для пошуку:

```go
chunkGroupIndex    int  // індекс групи чанків у stsc таблиці
chunkIndex         int  // поточний чанк у межах групи
sampleIndexInChunk int  // позиція семплу у чанку

sampleOffsetInChunk int64  // зміщення даних семплу у чанку
syncSampleIndex     int    // індекс у stss таблиці (ключові кадри)
```

**🔍 Як працює STSC (Sample-To-Chunk)**:

```
stsc таблиця мапить семпли на чанки:

struct SampleToChunkEntry {
    FirstChunk             uint32  // номер першого чанку в групі
    SamplesPerChunk        uint32  // скільки семплів у кожному чанку групи
    SampleDescriptionIndex uint32  // індекс кодек-опису (зазвичай 1)
}

Приклад:
  [ {FirstChunk:1, SamplesPerChunk:4}, {FirstChunk:10, SamplesPerChunk:1} ]
  → Чанки 1-9: по 4 семпли у кожному
  → Чанки 10+: по 1 семплу у кожному (напр. для ключових кадрів)

Пошук чанку для семплу #42:
1. Група 0: чанки 1-9, 4 семпли/чанк → 9*4 = 36 семплів
2. Семпл 42 > 36 → переходимо до групи 1
3. Група 1: чанки 10+, 1 семпл/чанк → семпл 42 = чанк 10 + (42-36) = чанк 16
```

**✅ Ваш use-case**: пошук даних семплу у файлі

```go
// GetSampleDataOffset — знайти позицію даних семплу у файлі
func (s *Stream) GetSampleDataOffset(sampleIndex int) (int64, int, error) {
    // 1. Знайти чанк через stsc
    chunkIndex, sampleInChunk, err := s.findChunkForSample(sampleIndex)
    if err != nil { return 0, 0, err }
    
    // 2. Знайти зміщення чанку через stco/co64
    chunkOffset, err := s.getChunkOffset(chunkIndex)
    if err != nil { return 0, 0, err }
    
    // 3. Знайти розміри семплів у чанку через stsz
    sizes, err := s.getSampleSizesInChunk(chunkIndex, sampleInChunk)
    if err != nil { return 0, 0, err }
    
    // 4. Розрахунок зміщення конкретного семплу
    var offset int64
    for i := 0; i < sampleInChunk; i++ {
        offset += int64(sizes[i])
    }
    
    return chunkOffset + offset, sizes[sampleInChunk], nil
}
```

---

## 🔑 4. Конвертація часу: helper функції

### 🔧 Глобальні функції:

```go
func timeToTs(tm time.Duration, timeScale int64) int64 {
    return int64(tm * time.Duration(timeScale) / time.Second)
}

func tsToTime(ts int64, timeScale int64) time.Duration {
    return time.Duration(ts) * time.Second / time.Duration(timeScale)
}
```

### 🔧 Методи структури (з використанням self.timeScale):

```go
func (self *Stream) timeToTs(tm time.Duration) int64 {
    return int64(tm * time.Duration(self.timeScale) / time.Second)
}

func (self *Stream) tsToTime(ts int64) time.Duration {
    return time.Duration(ts) * time.Second / time.Duration(self.timeScale)
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

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Демуксинг семплів для відтворення

```go
// ReadSamplesInRange — читання семплів у часовому діапазоні
func ReadSamplesInRange(stream *mp4.Stream, start, end time.Duration) ([]av.Packet, error) {
    // 1. Конвертація часу у ticks
    startTicks := stream.timeToTs(start)
    endTicks := stream.timeToTs(end)
    
    // 2. Пошук індексів першого та останнього семплу
    startIdx, err := stream.findSampleByDTS(startTicks)
    if err != nil { return nil, err }
    
    endIdx, err := stream.findSampleByDTS(endTicks)
    if err != nil { return nil, err }
    
    // 3. Читання даних семплів
    var packets []av.Packet
    for i := startIdx; i <= endIdx; i++ {
        pkt, err := stream.readSample(i)
        if err != nil { return nil, err }
        
        // Конвертація часу для av.Packet
        pkt.Time = stream.tsToTime(pkt.DTS)
        if pkt.CTS != pkt.DTS {
            pkt.CompositionTime = pkt.CTS - pkt.DTS
        }
        
        packets = append(packets, pkt)
    }
    
    return packets, nil
}

// findSampleByDTS — бінарний пошук семплу за DTS
func (s *Stream) findSampleByDTS(targetDTS int64) (int, error) {
    if s.sample.TimeToSample == nil {
        return 0, fmt.Errorf("no stts table")
    }
    
    // Простий лінійний пошук (можна оптимізувати бінарним)
    var currentDTS int64
    var sampleIndex int
    
    for _, entry := range s.sample.TimeToSample.Entry {
        for i := uint32(0); i < entry.Count; i++ {
            if currentDTS >= targetDTS {
                return sampleIndex, nil
            }
            currentDTS += int64(entry.Delta)
            sampleIndex++
        }
    }
    
    return sampleIndex - 1, nil  // останній семпл
}
```

### 🔧 Приклад: Муксинг з оптимізацією ключових кадрів

```go
// WritePacketWithKeyframeOptimization — запис з групуванням ключових кадрів
func (s *Stream) WritePacketWithKeyframeOptimization(pkt av.Packet) error {
    // 1. Конвертація часу у ticks
    dts := s.timeToTs(pkt.Time)
    cts := dts + s.timeToTs(pkt.CompositionTime)
    
    // 2. Перевірка чи це ключовий кадр для створення нового чанку
    if pkt.IsKeyFrame {
        // Примусове завершення поточного чанку
        if err := s.flushCurrentChunk(); err != nil {
            return err
        }
        // Додавання у stss таблицю
        s.addSyncSample(s.sampleIndex)
    }
    
    // 3. Додавання семплу у поточний чанк
    if err := s.appendSampleToChunk(pkt.Data, dts, cts); err != nil {
        return err
    }
    
    // 4. Оновлення індексів
    s.sampleIndex++
    s.updateSTTSEntry(pkt.Duration)
    s.updateCTTSEntry(pkt.CompositionTime)
    
    return nil
}

// flushCurrentChunk — завершення чанку та запис у файл
func (s *Stream) flushCurrentChunk() error {
    if len(s.currentChunkSamples) == 0 {
        return nil
    }
    
    // Запис даних чанку
    offset, err := s.muxer.writeData(s.currentChunkData)
    if err != nil { return err }
    
    // Оновлення stco таблиці
    s.sample.ChunkOffset.Entry = append(s.sample.ChunkOffset.Entry, 
        mp4io.ChunkOffsetEntry{Offset: uint64(offset)})
    
    // Скидання буфера чанку
    s.currentChunkData = nil
    s.currentChunkSamples = 0
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Переповнення при конвертації часу** | Неправильні таймінги для довгих відео | Використовуйте `safeTimeToTs()` з float64 проміжними обчисленнями |
| **Невірний пошук семплу** | `findSampleByDTS` повертає не той індекс | Переконайтеся, що stts таблиця відсортована; використовуйте бінарний пошук для великих файлів |
| **Відсутні ключові кадри** | `syncSampleIndex` не оновлюється | Перевірте чи `pkt.IsKeyFrame` коректно встановлено для H.264 IDR кадрів |
| **Розсинхронізація аудіо/відео** | Різні timeScale для треків | Нормалізуйте часи до спільної шкали перед порівнянням |
| **Повільний пошук у великих файлах** | Лінійний пошук у stts/stsc | Реалізуйте кешування індексів або бінарний пошук |

---

## ⚡ Оптимізації для великих файлів

### 1. Кешування індексів пошуку:

```go
type SampleIndexCache struct {
    mu        sync.RWMutex
    dtsToIdx  map[int64]int  // DTS → sample index
    idxToDTS  map[int]64     // sample index → DTS
    maxSize   int
}

func (c *SampleIndexCache) GetByDTS(dts int64) (int, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    idx, ok := c.dtsToIdx[dts]
    return idx, ok
}

func (c *SampleIndexCache) Set(dts int64, idx int) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Видалення старих записів якщо перевищено ліміт
    if len(c.dtsToIdx) >= c.maxSize {
        // Проста стратегія: видалити найстаріші
        for k := range c.dtsToIdx {
            delete(c.dtsToIdx, k)
            break
        }
    }
    
    c.dtsToIdx[dts] = idx
    c.idxToDTS[idx] = dts
}
```

### 2. Пакетне читання чанків:

```go
// ReadChunkBatch — читання кількох чанків за один системний виклик
func (s *Stream) ReadChunkBatch(chunkIndices []int) ([][]byte, error) {
    if len(chunkIndices) == 0 {
        return nil, nil
    }
    
    // Сортування індексів для послідовного читання
    sort.Ints(chunkIndices)
    
    var results [][]byte
    var lastOffset int64 = -1
    
    for _, chunkIdx := range chunkIndices {
        offset, size, err := s.getChunkInfo(chunkIdx)
        if err != nil { return nil, err }
        
        // Оптимізація: якщо чанки послідовні, читати одним викликом
        if lastOffset+size == offset && len(results) > 0 {
            // Об'єднання з попереднім читанням
            continue
        }
        
        // Читання чанку
        data := make([]byte, size)
        if _, err := s.demuxer.file.ReadAt(data, offset); err != nil {
            return nil, err
        }
        
        results = append(results, data)
        lastOffset = offset + size
    }
    
    return results, nil
}
```

### 3. Моніторинг продуктивності:

```go
type StreamMetrics struct {
    SampleReadLatency prometheus.HistogramVec
    ChunkSeekTime     prometheus.HistogramVec
    CacheHitRatio     prometheus.GaugeVec
}

func (m *StreamMetrics) RecordSampleRead(sampleIndex int, duration time.Duration, streamID string) {
    m.SampleReadLatency.WithLabelValues(streamID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист використання mp4.Stream

```go
// ✅ 1. Ініціалізація timeScale з метаданих треку
stream.timeScale = stream.trackAtom.Media.Header.TimeScale

// ✅ 2. Безпечна конвертація часу без переповнення
ticks := stream.safeTimeToTs(pkt.Time)

// ✅ 3. Перевірка наявності таблиць перед пошуком
if stream.sample.TimeToSample == nil {
    return fmt.Errorf("stts table missing")
}

// ✅ 4. Оновлення індексів при додаванні семплів
stream.sampleIndex++
stream.updateSTTSEntry(pkt.Duration)

// ✅ 5. Обробка ключових кадрів для stss таблиці
if pkt.IsKeyFrame {
    stream.addSyncSample(stream.sampleIndex)
}

// ✅ 6. Метрики для моніторингу
metrics.RecordSampleRead(stream.sampleIndex, time.Since(start), streamID)

// ✅ 7. Логування для дебагу
if Debug {
    log.Printf("Stream %d: sample %d, DTS=%d, PTS=%d", 
        stream.trackAtom.TrackHeader.TrackID, 
        stream.sampleIndex, dts, cts)
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk mp4 Package](https://pkg.go.dev/github.com/deepch/vdk/format/mp4) — GoDoc documentation
- 📄 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт MP4
- 📄 [MP4 Sample Tables Explained](https://wiki.multimedia.cx/index.php/MP4) — детальний опис stts/ctts/stsc
- 🧪 [Go time.Duration Best Practices](https://go.dev/doc/effective_go#time) — робота з часом у Go
- 📦 [Prometheus Metrics for Media](https://prometheus.io/docs/practices/instrumentation/) — моніторинг продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди використовуйте безпечну конвертацію часу** — переповнення int64 призведе до некоректних таймінгів.
> 2. **Кешуйте індекси пошуку для великих файлів** — лінійний пошук у stts/stsc може бути повільним.
> 3. **Перевіряйте наявність таблиць перед доступом** — уникнення панік при пошкоджених файлах.
> 4. **Оновлюйте stss таблицю для ключових кадрів** — це критично для seek та low-latency стрімінгу.
> 5. **Моніторьте `SampleReadLatency`** — різке зростання може вказувати на фрагментацію файлу або проблеми з диском.

Потрібен приклад реалізації бінарного пошуку у stts таблиці для прискорення пошуку семплів у великих файлах? Готовий допомогти! 🚀