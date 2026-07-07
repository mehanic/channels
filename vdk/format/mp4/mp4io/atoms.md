# 📦 Глибокий розбір: `mp4io` — Згенеровані атоми для ISO BMFF (MP4)

Цей файл — **повна реалізація структур атомів (boxes)** для формату ISO Base Media File Format (ISO BMFF), що лежить в основі контейнерів `.mp4`, `.mov`, `.m4a`. Він був згенерований автоматично за допомогою DSL-генератора (`main.go`) і містить типи даних, маршалінг/анмаршалінг, та навігацію для всіх критичних атомів, необхідних для читання/запису медіа-файлів.

---

## 🗺️ Архітектурна схема mp4io

```
┌────────────────────────────────────────┐
│ 📦 mp4io — ISO BMFF Atom Definitions  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Atom interface — уніфікований доступ│
│  • Tag (fourcc) — ідентифікація атомів │
│  • Movie/Track/Media — ієрархія метаданих│
│  • SampleTable — таблиці семплів      │
│  • Codec descriptors (AVCC, ESDS)     │
│                                         │
│  🔄 Ієрархія атомів:                    │
│  MOOV (movie)                          │
│  ├─ MVHD (movie header)                │
│  ├─ MVEX (movie extensions)            │
│  └─ TRAK × N (tracks)                  │
│      ├─ TKHD (track header)           │
│      ├─ MDIA (media)                  │
│      │  ├─ MDHD (media header)        │
│      │  ├─ HDLR (handler reference)   │
│      │  └─ MINF (media info)          │
│      │      ├─ VMHD/SMHD (video/audio)│
│      │      ├─ DINF (data info)       │
│      │      └─ STBL (sample table)    │
│      │          ├─ STSD (sample desc) │
│      │          ├─ STTS (time-to-sample)│
│      │          ├─ STSC (sample-to-chunk)│
│      │          ├─ STSZ (sample size) │
│      │          ├─ STCO (chunk offset)│
│      │          ├─ STSS (sync samples)│
│      │          └─ CTTS (composition offset)│
│                                         │
│  📡 Підтримка кодеків:                  │
│  • H.264: AVC1Desc + AVC1Conf (avcC)  │
│  • H.265: HV1Desc + HV1Conf (hvcC)    │
│  • AAC: MP4ADesc + ElemStreamDesc (esds)│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Atom interface та Tag (fourcc)

### 🔧 Уніфікований інтерфейс:

```go
type Atom interface {
    Pos() (int, int)              // offset, size у файлі
    Tag() Tag                     // fourcc код (напр. 'moov')
    Marshal([]byte) int           // серіалізація у байти
    Unmarshal([]byte, int) (int, error)  // десеріалізація
    Len() int                     // розмір атому у байтах
    Children() []Atom             // дочірні атоми
}
```

### 🔧 Fourcc коди (Tag):

```go
type Tag uint32

const (
    MOOV = Tag(0x6d6f6f76)  // 'moov' — Movie atom
    TRAK = Tag(0x7472616b)  // 'trak' — Track atom
    MDIA = Tag(0x6d646961)  // 'mdia' — Media atom
    MINF = Tag(0x6d696e66)  // 'minf' — Media Info atom
    STBL = Tag(0x7374626c)  // 'stbl' — Sample Table atom
    // ... ще 30+ констант ...
)

func (self Tag) String() string {
    var b [4]byte
    pio.PutU32BE(b[:], uint32(self))
    for i := 0; i < 4; i++ {
        if b[i] == 0 { b[i] = ' ' }
    }
    return string(b[:])  // "moov", "trak", тощо
}
```

### ✅ Ваш use-case**: пошук атому за тегом

```go
// FindAtomByTag — рекурсивний пошук атому у дереві
func FindAtomByTag(root mp4io.Atom, tag mp4io.Tag) mp4io.Atom {
    if root.Tag() == tag {
        return root
    }
    for _, child := range root.Children() {
        if found := FindAtomByTag(child, tag); found != nil {
            return found
        }
    }
    return nil
}

// Приклад: отримання Sample Table для треку
stbl := FindAtomByTag(trackAtom, mp4io.STBL)
if stbl == nil {
    return fmt.Errorf("sample table not found")
}
sampleTable, ok := stbl.(*mp4io.SampleTable)
if !ok {
    return fmt.Errorf("unexpected atom type: %T", stbl)
}
```

---

## 🔑 2. Movie/Track/Media — ієрархія метаданих

### 🔧 Movie (moov) — кореневий атом:

```go
type Movie struct {
    Header      *MovieHeader   // mvhd: загальні метадані фільму
    MovieExtend *MovieExtend   // mvex: розширення для фрагментованого MP4
    Tracks      []*Track       // trak × N: масив треків
    Unknowns    []Atom         // невідомі атоми для сумісності
    AtomPos
}
```

### 🔧 Track (trak) — окремий медіа-потік:

```go
type Track struct {
    Header   *TrackHeader  // tkhd: метадані треку (тривалість, розмір)
    Media    *Media        // mdia: медіа-специфічні дані
    Unknowns []Atom
    AtomPos
}
```

### 🔧 Media (mdia) — кодек та таймінги:

```go
type Media struct {
    Header   *MediaHeader   // mdhd: частота дискретизації, мова
    Handler  *HandlerRefer  // hdlr: тип медіа ('vide'/'soun')
    Info     *MediaInfo     // minf: інформація про медіа
    Unknowns []Atom
    AtomPos
}
```

### 🔍 Приклад навігації:

```
moov (Movie)
├─ mvhd (MovieHeader)
│  ├─ Duration: 360000 ticks @ 90000 Hz = 4 секунди
│  ├─ TimeScale: 90000
│  └─ NextTrackId: 2
├─ trak (Track #1)
│  ├─ tkhd (TrackHeader)
│  │  ├─ TrackId: 1
│  │  ├─ Duration: 360000
│  │  └─ TrackWidth/Height: 1920x1080
│  └─ mdia (Media)
│     ├─ mdhd (MediaHeader)
│     │  ├─ TimeScale: 90000 (відео)
│     │  └─ Language: 21956 ('und')
│     ├─ hdlr (HandlerRefer)
│     │  └─ SubType: 'vide' (відео трек)
│     └─ minf (MediaInfo)
│        ├─ vmhd (VideoMediaInfo)
│        ├─ dinf (DataInfo)
│        └─ stbl (SampleTable) ← критично для демуксингу
```

### ✅ Ваш use-case**: отримання роздільної здатності відео

```go
// GetVideoResolution — витягування width/height з треку
func GetVideoResolution(track *mp4io.Track) (width, height int, err error) {
    if track.Header == nil {
        return 0, 0, fmt.Errorf("track header missing")
    }
    
    // TrackHeader зберігає розмір у форматі фіксованої крапки 16.16
    width = int(track.Header.TrackWidth)   // ⚠️ Потрібна конвертація!
    height = int(track.Header.TrackHeight) // ⚠️ Потрібна конвертація!
    
    // Конвертація з фіксованої крапки 16.16 у ціле число
    // Значення = ціла_частина + дробова/65536
    // Для роздільної здатності зазвичай дробова = 0
    return int(track.Header.TrackWidth), int(track.Header.TrackHeight), nil
}

// Безпечніша версія з перевіркою:
func GetVideoResolutionSafe(track *mp4io.Track) (int, int, error) {
    if track.Header == nil {
        return 0, 0, fmt.Errorf("track header missing")
    }
    
    // Конвертація з fixed-point 16.16
    width := int(track.Header.TrackWidth)
    height := int(track.Header.TrackHeight)
    
    // Валідація розумних значень
    if width < 16 || width > 16384 || height < 16 || height > 16384 {
        return 0, 0, fmt.Errorf("suspicious resolution: %dx%d", width, height)
    }
    
    return width, height, nil
}
```

---

## 🔑 3. SampleTable — таблиці для навігації по семплах

### 🔧 Структура та призначення:

```go
type SampleTable struct {
    SampleDesc        *SampleDesc        // stsd: опис кодеків
    TimeToSample      *TimeToSample      // stts: DTS розрахунок
    CompositionOffset *CompositionOffset // ctts: PTS = DTS + offset
    SampleToChunk     *SampleToChunk     // stsc: мапінг семплів у чанки
    SyncSample        *SyncSample        // stss: індекси ключових кадрів
    ChunkOffset       *ChunkOffset       // stco: позиції чанків у файлі
    SampleSize        *SampleSize        // stsz: розміри семплів
    AtomPos
}
```

### 🔍 Призначення кожної таблиці:

| Таблиця | Тег | Призначення | Приклад використання |
|---------|-----|-------------|---------------------|
| **stsd** | SampleDesc | Опис кодеків (AVC1Desc, MP4ADesc) | Ініціалізація декодера |
| **stts** | TimeToSample | Розрахунок DTS: `DTS[n] = DTS[n-1] + Duration` | Синхронізація аудіо/відео |
| **ctts** | CompositionOffset | Розрахунок PTS: `PTS = DTS + Offset` | Обробка B-frames |
| **stsc** | SampleToChunk | Мапінг: "чанк X містить Y семплів" | Пошук даних у файлі |
| **stco** | ChunkOffset | Позиція чанку у файлі (32-біт) | Seek до даних |
| **stsz** | SampleSize | Розмір кожного семплу у байтах | Виділення буфера |
| **stss** | SyncSample | Індекси ключових кадрів (1-based) | Seek до найближчого keyframe |

### ✅ Ваш use-case**: пошук даних семплу у файлі

```go
// GetSampleDataOffset — знайти позицію та розмір даних семплу
func GetSampleDataOffset(st *mp4io.SampleTable, sampleIndex int) (offset int64, size uint32, err error) {
    // 1. Знайти чанк через stsc таблицю
    chunkIndex, sampleInChunk, err := findChunkForSample(st.SampleToChunk, sampleIndex)
    if err != nil { return 0, 0, err }
    
    // 2. Знайти зміщення чанку через stco
    if chunkIndex >= len(st.ChunkOffset.Entries) {
        return 0, 0, fmt.Errorf("chunk index %d out of range", chunkIndex)
    }
    chunkOffset := int64(st.ChunkOffset.Entries[chunkIndex])
    
    // 3. Знайти розміри семплів у чанку через stsz
    var sampleOffsetInChunk int64
    if st.SampleSize.SampleSize != 0 {
        // Фіксований розмір для всіх семплів
        sampleOffsetInChunk = int64(sampleInChunk) * int64(st.SampleSize.SampleSize)
        size = st.SampleSize.SampleSize
    } else {
        // Змінний розмір: підсумовуємо розміри попередніх семплів
        startIndex := getSampleIndexInFile(st.SampleToChunk, chunkIndex)
        for i := 0; i < sampleInChunk; i++ {
            idx := startIndex + i
            if idx >= len(st.SampleSize.Entries) {
                return 0, 0, fmt.Errorf("sample size index %d out of range", idx)
            }
            sampleOffsetInChunk += int64(st.SampleSize.Entries[idx])
        }
        if sampleIndex >= len(st.SampleSize.Entries) {
            return 0, 0, fmt.Errorf("sample index %d out of range", sampleIndex)
        }
        size = st.SampleSize.Entries[sampleIndex]
    }
    
    offset = chunkOffset + sampleOffsetInChunk
    return offset, size, nil
}

// Допоміжна функція: знайти чанк для семплу
func findChunkForSample(stsc *mp4io.SampleToChunk, sampleIndex int) (chunkIndex, sampleInChunk int, err error) {
    start := 0
    groupIndex := 0
    
    for chunkIdx := 0; chunkIdx < len(stsc.Entries); chunkIdx++ {
        // Перехід до наступної групи stsc якщо потрібно
        if groupIndex+1 < len(stsc.Entries) && 
           uint32(chunkIdx+1) == stsc.Entries[groupIndex+1].FirstChunk {
            groupIndex++
        }
        
        samplesPerChunk := int(stsc.Entries[groupIndex].SamplesPerChunk)
        if sampleIndex >= start && sampleIndex < start+samplesPerChunk {
            return chunkIdx, sampleIndex - start, nil
        }
        start += samplesPerChunk
    }
    
    return 0, 0, fmt.Errorf("sample index %d not found in stsc", sampleIndex)
}
```

---

## 🔑 4. Codec descriptors — AVC1Conf, ElemStreamDesc

### 🔧 H.264: AVC1Conf (avcC атом)

```go
type AVC1Conf struct {
    Data []byte  // AVCDecoderConfigurationRecord
    AtomPos
}
```

**🔍 Формат AVCDecoderConfigurationRecord**:

```
Байти 0-3: configurationVersion(1), AVCProfileIndication(1), profile_compatibility(1), AVCLevelIndication(1)
Байт 4: lengthSizeMinusOne (зазвичай 3 = 4-байтові довжини)
Байт 5: numOfSequenceParameterSets (зазвичай 1)
Байти 6-7: sequenceParameterSetLength (big-endian)
Байти 8...: sequenceParameterSetNALUnit (SPS дані)
... (аналогічно для PPS)
```

### ✅ Ваш use-case**: ініціалізація H.264 декодера

```go
// InitH264DecoderFromAVCC — створення CodecData з avcC атому
func InitH264DecoderFromAVCC(avcc *mp4io.AVC1Conf) (av.CodecData, error) {
    if avcc == nil || len(avcc.Data) == 0 {
        return nil, fmt.Errorf("empty AVC config")
    }
    
    // h264parser.NewCodecDataFromAVCDecoderConfRecord очікує сирий AVCDecoderConfigurationRecord
    return h264parser.NewCodecDataFromAVCDecoderConfRecord(avcc.Data)
}

// Використання:
track := findVideoTrack(moov)
avcc := track.GetAVC1Conf()  // helper function з mp4 package
if avcc == nil {
    return fmt.Errorf("no AVC config found")
}
codecData, err := InitH264DecoderFromAVCC(avcc)
if err != nil {
    return fmt.Errorf("init H.264 decoder: %w", err)
}
```

### 🔧 AAC: ElemStreamDesc (esds атом)

```go
type ElemStreamDesc struct {
    DecConfig []byte  // MPEG4AudioConfig (AudioSpecificConfig)
    TrackId   uint16
    AtomPos
}
```

**🔍 Формат AudioSpecificConfig**:

```
Байти 0-1: audioObjectType (5 біт) + samplingFrequencyIndex (4 біти) + channelConfiguration (4 біти)
Якщо samplingFrequencyIndex == 0xF: наступні 24 біти = explicit sampling frequency
```

### ✅ Ваш use-case**: ініціалізація AAC декодера

```go
// InitAACDecoderFromESDS — створення CodecData з esds атому
func InitAACDecoderFromESDS(esds *mp4io.ElemStreamDesc) (av.CodecData, error) {
    if esds == nil || len(esds.DecConfig) == 0 {
        return nil, fmt.Errorf("empty AAC config")
    }
    
    // aacparser.NewCodecDataFromMPEG4AudioConfigBytes очікує AudioSpecificConfig
    return aacparser.NewCodecDataFromMPEG4AudioConfigBytes(esds.DecConfig)
}

// Використання:
track := findAudioTrack(moov)
esds := track.GetElemStreamDesc()  // helper function
if esds == nil {
    return fmt.Errorf("no ESDS config found")
}
codecData, err := InitAACDecoderFromESDS(esds)
if err != nil {
    return fmt.Errorf("init AAC decoder: %w", err)
}
```

---

## 🔑 5. Fragmented MP4 атоми (MOOF, TRAF, TRUN)

### 🔧 Призначення для streaming:

```
Фрагментований MP4 (fMP4) використовується для:
• Low-latency streaming (DASH, HLS fMP4)
• Live broadcasting
• Progressive download з можливістю seek

Структура:
  [ftyp][moov][moof][mdat][moof][mdat]...
                ↑       ↑
           метадані  дані фрагменту

moof (Movie Fragment):
├─ mfhd (MovieFragHeader) — номер фрагменту
└─ traf × N (TrackFrag) — дані треку у фрагменті
    ├─ tfhd (TrackFragHeader) — default параметри
    ├─ tfdt (TrackFragDecodeTime) — базовий DTS
    └─ trun (TrackFragRun) — таблиця семплів у фрагменті
```

### 🔧 TrackFragRun (trun) — гнучка таблиця семплів:

```go
type TrackFragRun struct {
    Version          uint8
    Flags            uint32  // бітові прапорці для опціональних полів
    DataOffset       uint32  // зміщення даних відносно базового
    FirstSampleFlags uint32  // прапорці для першого семплу
    Entries          []TrackFragRunEntry  // масив семплів
    AtomPos
}

type TrackFragRunEntry struct {
    Duration uint32  // тривалість семплу (опціонально)
    Size     uint32  // розмір даних (опціонально)
    Flags    uint32  // прапорці семплу (опціонально)
    Cts      uint32  // composition offset (опціонально)
}
```

**🔍 Прапорці (Flags)**:

```go
const (
    TRUN_DATA_OFFSET        = 0x01  // присутній DataOffset
    TRUN_FIRST_SAMPLE_FLAGS = 0x04  // присутній FirstSampleFlags
    TRUN_SAMPLE_DURATION    = 0x100 // Duration у кожному Entry
    TRUN_SAMPLE_SIZE        = 0x200 // Size у кожному Entry
    TRUN_SAMPLE_FLAGS       = 0x400 // Flags у кожному Entry
    TRUN_SAMPLE_CTS         = 0x800 // Cts у кожному Entry
)
```

### ✅ Ваш use-case**: парсинг fMP4 фрагменту

```go
// ParseFragmentSamples — отримання семплів з moof атому
func ParseFragmentSamples(moof *mp4io.MovieFrag, trackId uint32) ([]SampleInfo, error) {
    var samples []SampleInfo
    
    for _, traf := range moof.Tracks {
        if traf.Header == nil || traf.Header.TrackId != trackId {
            continue
        }
        
        // Базовий DTS з tfdt
        baseDTS := int64(0)
        if traf.DecodeTime != nil {
            baseDTS = int64(traf.DecodeTime.Time.UnixNano())  // ⚠️ Потрібна конвертація!
        }
        
        // Базові параметри з tfhd
        var defaultDuration, defaultSize uint32
        if traf.Header != nil {
            defaultDuration = traf.Header.DefaultDuration
            defaultSize = traf.Header.DefaultSize
        }
        
        // Семпли з trun
        if traf.Run != nil {
            run := traf.Run
            currentDTS := baseDTS
            
            for i, entry := range run.Entries {
                // Визначення прапорців для цього семплу
                flags := run.Flags
                if i == 0 && (run.Flags&TRUN_FIRST_SAMPLE_FLAGS != 0) {
                    flags = run.FirstSampleFlags
                }
                
                // Розрахунок параметрів
                duration := defaultDuration
                if flags&TRUN_SAMPLE_DURATION != 0 {
                    duration = entry.Duration
                }
                
                size := defaultSize
                if flags&TRUN_SAMPLE_SIZE != 0 {
                    size = entry.Size
                }
                
                cts := int64(0)
                if flags&TRUN_SAMPLE_CTS != 0 {
                    cts = int64(entry.Cts)
                }
                
                samples = append(samples, SampleInfo{
                    DTS: currentDTS,
                    PTS: currentDTS + cts,
                    Duration: int64(duration),
                    Size: size,
                })
                
                currentDTS += int64(duration)
            }
        }
    }
    
    return samples, nil
}

type SampleInfo struct {
    DTS, PTS, Duration int64
    Size uint32
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Невірний розмір у TrackHeader** | Роздільна здатність = 125829120 замість 1920 | Конвертуйте з fixed-point 16.16: `int(value)` або `int(value >> 16)` |
| **Переповнення стко** | Файли >4 ГБ не читаються коректно | Використовуйте `co64` атом замість `stco` для 64-бітних offset'ів |
| **Невірний час у tfdt** | Час зміщений на 66 років | Враховуйте різницю між 1904 та 1970 епохами при конвертації |
| **Паніка при type assertion** | `atom.(*SampleTable)` не співпадає | Завжди перевіряйте `ok` після type assertion |
| **Некоректний parсинг esds** | AAC config не витягується | Переконайтеся, що `DecConfig` містить тільки AudioSpecificConfig без заголовків дескрипторів |

---

## ⚡ Оптимізації для великих файлів

### 1. Кешування індексів пошуку:

```go
type SampleIndexCache struct {
    mu        sync.RWMutex
    timeToIdx map[time.Duration]int  // час → sample index
    maxSize   int
}

func (c *SampleIndexCache) Get(tm time.Duration) (int, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    idx, ok := c.timeToIdx[tm]
    return idx, ok
}

func (c *SampleIndexCache) Set(tm time.Duration, idx int) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if len(c.timeToIdx) >= c.maxSize {
        // Видалення найстарішого запису (спрощено)
        for k := range c.timeToIdx {
            delete(c.timeToIdx, k)
            break
        }
    }
    c.timeToIdx[tm] = idx
}
```

### 2. Пакетне читання чанків:

```go
// ReadChunkBatch — читання кількох чанків за один системний виклик
func (s *Stream) ReadChunkBatch(chunkIndices []int) ([][]byte, error) {
    if len(chunkIndices) == 0 { return nil, nil }
    
    // Сортування для послідовного читання
    sort.Ints(chunkIndices)
    
    var results [][]byte
    var lastOffset int64 = -1
    
    for _, chunkIdx := range chunkIndices {
        offset := int64(s.sample.ChunkOffset.Entries[chunkIdx])
        size := s.getChunkSize(chunkIdx)  // допоміжна функція
        
        // Оптимізація: якщо чанки послідовні, читати одним викликом
        if lastOffset+size == offset && len(results) > 0 {
            continue
        }
        
        data := make([]byte, size)
        if _, err := s.demuxer.r.ReadAt(data, offset); err != nil {
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
type ParserMetrics struct {
    PacketReadLatency prometheus.HistogramVec
    SeekLatency       prometheus.HistogramVec
    CacheHitRatio     prometheus.GaugeVec
}

func (m *ParserMetrics) RecordPacketRead(duration time.Duration, streamID string) {
    m.PacketReadLatency.WithLabelValues(streamID).Observe(duration.Seconds())
}

func (m *ParserMetrics) RecordSeek(duration time.Duration, streamID string) {
    m.SeekLatency.WithLabelValues(streamID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист безпечного використання mp4io

```go
// ✅ 1. Обробка 64-бітних розмірів атомів
if size == 1 {
    // read 64-bit size
}

// ✅ 2. Перевірка type assertion з ok
if sampleTable, ok := atom.(*mp4io.SampleTable); ok {
    // use sampleTable
} else {
    return fmt.Errorf("unexpected atom type: %T", atom)
}

// ✅ 3. Конвертація fixed-point 16.16 для розмірів
width := int(trackHeader.TrackWidth)  // або (int)(trackHeader.TrackWidth >> 16)

// ✅ 4. Валідація часу перед конвертацією
if mp4Time.Year() < 1904 || mp4Time.Year() > 2100 {
    log.Printf("warning: suspicious MP4 time: %v", mp4Time)
}

// ✅ 5. Обмеження максимального розміру атому для безпеки
if size > 1<<30 {  // 1GB
    return fmt.Errorf("atom too large: %d bytes", size)
}

// ✅ 6. Логування з контекстом для помилок
if err != nil {
    LogParseError(err, filename)  // функція з попереднього прикладу
}

// ✅ 7. Метрики для моніторингу
metrics.RecordAtom(tag, size, time.Since(start))
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [MP4 Box Structure](https://wiki.multimedia.cx/index.php/MP4) — візуальна схема атомів
- 📄 [AVCDecoderConfigurationRecord](https://wiki.multimedia.cx/index.php/AVCDecoderConfigurationRecord) — формат avcC
- 📄 [AudioSpecificConfig for AAC](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — формат esds
- 🧪 [Fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) — теорія формату 16.16

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди конвертуйте fixed-point 16.16** для роздільної здатності та коефіцієнтів — інакше отримаєте неправильні значення.
> 2. **Перевіряйте `ok` після type assertion** — уникнення панік при несподіваних типах атомів.
> 3. **Обробляйте 64-бітні розміри атомів** — для підтримки файлів >4 ГБ.
> 4. **Враховуйте різницю епох (1904 vs 1970)** при конвертації часу — інакше метадані будуть зміщені на 66 років.
> 5. **Кешуйте результати пошуку атомів** — прискорення повторних операцій навігації.

Потрібен приклад реалізації `WriteAtom` для створення власних MP4 файлів з низького рівня, або інтеграція `mp4io` з вашим `mp4.Muxer` для генерації фрагментованого MP4? Готовий допомогти! 🚀