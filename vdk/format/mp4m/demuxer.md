# 📦 Глибокий розбір: `mp4.Demuxer` — Читання стандартного MP4 контейнера

Цей файл — **повноцінна реалізація демуксера для MP4 (ISO BMFF) контейнера**, що перетворює файли у послідовність `av.Packet` для подальшої обробки. Він підтримує H.264 відео та AAC аудіо, пошук за часом, та оптимізований доступ до семплів через індекси.

---

## 🗺️ Архітектурна схема mp4.Demuxer

```
┌────────────────────────────────────────┐
│ 📦 mp4.Demuxer — ISO BMFF Reader      │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Demuxer — основний контролер        │
│  • Stream — обробка окремого треку     │
│  • probe() — парсинг moov атому       │
│  • ReadPacket() — читання з синхронізацією│
│  • SeekToTime() — пошук за часом       │
│                                         │
│  🔄 Потік даних:                        │
│  io.ReadSeeker → mp4io атоми → Stream  │
│  → av.Packet з коректними таймінгами  │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (AVCDecoderConfRecord)│
│  • Аудіо: AAC (MPEG4AudioConfig)      │
│  • Функції: seek, sync sample, ctts   │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Demuxer — основна структура

### Поля та їх призначення:

```go
type Demuxer struct {
    r         io.ReadSeeker  // вхідний потік (файл/буфер)
    streams   []*Stream      // масив треків (відео=0, аудіо=1...)
    movieAtom *mp4io.Movie   // кешований moov атом (метадані)
}
```

### 🔧 NewDemuxer() — ініціалізація:

```go
func NewDemuxer(r io.ReadSeeker) *Demuxer {
    return &Demuxer{
        r: r,
        // ⚠️ movieAtom = nil → lazy loading у probe()
    }
}
```

**✅ Ваш use-case**: відкриття файлу з перевіркою

```go
// OpenMP4File — безпечне відкриття з валідацією
func OpenMP4File(filename string) (*mp4.Demuxer, error) {
    f, err := os.Open(filename)
    if err != nil {
        return nil, fmt.Errorf("open file: %w", err)
    }
    
    demuxer := mp4.NewDemuxer(f)
    
    // Попередня перевірка чи файл валідний
    if err := demuxer.Probe(); err != nil {
        f.Close()
        return nil, fmt.Errorf("invalid MP4: %w", err)
    }
    
    return demuxer, nil
}
```

---

## 🔑 2. probe() — парсинг moov атому та ініціалізація треків

### 🔧 Основна логіка:

```go
func (self *Demuxer) probe() (err error) {
    // 1. Кешування: якщо вже пробовано — повертаємо
    if self.movieAtom != nil {
        return
    }
    
    // 2. Читання атомів з файлу
    var atoms []mp4io.Atom
    if atoms, err = mp4io.ReadFileAtoms(self.r); err != nil {
        return
    }
    
    // 3. Повернення на початок для подальшого читання даних
    if _, err = self.r.Seek(0, 0); err != nil {
        return
    }
    
    // 4. Пошук moov атому серед прочитаних
    var moov *mp4io.Movie
    for _, atom := range atoms {
        if atom.Tag() == mp4io.MOOV {
            moov = atom.(*mp4io.Movie)
        }
    }
    
    if moov == nil {
        err = fmt.Errorf("mp4: 'moov' atom not found")
        return
    }
    
    // 5. Ініціалізація треків
    self.streams = []*Stream{}
    for i, atrack := range moov.Tracks {
        stream := &Stream{
            trackAtom: atrack,
            demuxer:   self,
            idx:       i,
        }
        
        // Перевірка наявності таблиці семплів
        if atrack.Media != nil && atrack.Media.Info != nil && atrack.Media.Info.Sample != nil {
            stream.sample = atrack.Media.Info.Sample
            stream.timeScale = int64(atrack.Media.Header.TimeScale)
        } else {
            err = fmt.Errorf("mp4: sample table not found")
            return
        }
        
        // 6. Визначення кодека та ініціалізація CodecData
        if avc1 := atrack.GetAVC1Conf(); avc1 != nil {
            // H.264: парсинг AVCDecoderConfigurationRecord
            if stream.CodecData, err = h264parser.NewCodecDataFromAVCDecoderConfRecord(avc1.Data); err != nil {
                return
            }
            self.streams = append(self.streams, stream)
            
        } else if esds := atrack.GetElemStreamDesc(); esds != nil {
            // AAC: парсинг MPEG4AudioConfig
            if stream.CodecData, err = aacparser.NewCodecDataFromMPEG4AudioConfigBytes(esds.DecConfig); err != nil {
                return
            }
            self.streams = append(self.streams, stream)
        }
    }
    
    self.movieAtom = moov  // кешування для наступних викликів
    return
}
```

### 🔍 Чому lazy loading у probe()?

```
Переваги:
• Не парсимо файл якщо не потрібно (напр. тільки перевірка розширення)
• Економія пам'яті: movieAtom кешується тільки після першого виклику
• Гнучкість: можна відкрити файл, потім вирішити чи читати метадані

Недоліки:
• Перший ReadPacket() буде повільнішим (парсинг moov)
• Потрібно перевіряти помилки probe() у кожному публічному методі

✅ Рекомендація: викликайте Streams() одразу після NewDemuxer() 
   для явного парсингу та ранньої обробки помилок.
```

### ⚠️ Критична проблема: ReadFileAtoms може читати багато даних

```
mp4io.ReadFileAtoms(self.r) читає ВСІ атоми файлу у пам'ять!

Для великих файлів (>1 ГБ) це може призвести до:
• Високого споживання пам'яті
• Повільного старту через читання всього файлу

✅ Виправлення: читати тільки moov атом, ігноруючи mdat

func (self *Demuxer) probeOptimized() (err error) {
    // Читання тільки заголовків атомів до знаходження moov
    for {
        atom, size, err := mp4io.ReadNextAtomHeader(self.r)
        if err == io.EOF { break }
        if err != nil { return err }
        
        if atom == mp4io.MOOV {
            // Читання тільки moov атому
            moovData := make([]byte, size)
            if _, err = io.ReadFull(self.r, moovData); err != nil { return err }
            // Парсинг moovData...
            break
        } else {
            // Пропуск атому
            if _, err = self.r.Seek(int64(size)-8, io.SeekCurrent); err != nil { return err }
        }
    }
    // Повернення на початок для читання даних
    _, err = self.r.Seek(0, 0)
    return err
}
```

---

## 🔑 3. Stream — структура для обробки треку

### 🔧 Ключові поля для навігації:

```go
type Stream struct {
    // 🗂️ Індекси для пошуку
    sampleIndex             int  // поточний семпл (0-based)
    chunkIndex              int  // поточний чанк у stco таблиці
    chunkGroupIndex         int  // поточна група у stsc таблиці
    sampleIndexInChunk      int  // позиція семплу у чанку
    
    // ⏱️ Таймінги
    dts                     int64  // поточний Decoding Time Stamp
    sttsEntryIndex          int    // поточний запис у stts таблиці
    sampleIndexInSttsEntry  int    // позиція у межах stts запису
    cttsEntryIndex          int    // аналогічно для ctts (Composition Offset)
    sampleIndexInCttsEntry  int
    
    // 🔑 Ключові кадри
    syncSampleIndex         int  // індекс у stss таблиці
    
    // 📊 Розміри та offset'и
    sampleOffsetInChunk     int64  // зміщення даних семплу у чанку
    
    // 🔗 Посилання
    trackAtom *mp4io.Track
    sample    *mp4io.SampleTable
    demuxer   *Demuxer
    idx       int
    timeScale int64
}
```

### 🔧 setSampleIndex() — навігація до конкретного семплу

```go
func (self *Stream) setSampleIndex(index int) (err error) {
    // 1. Пошук чанку через stsc таблицю
    found := false
    start := 0
    self.chunkGroupIndex = 0
    
    for self.chunkIndex = range self.sample.ChunkOffset.Entries {
        // Перехід до наступної групи stsc якщо потрібно
        if self.chunkGroupIndex+1 < len(self.sample.SampleToChunk.Entries) &&
            uint32(self.chunkIndex+1) == self.sample.SampleToChunk.Entries[self.chunkGroupIndex+1].FirstChunk {
            self.chunkGroupIndex++
        }
        
        n := int(self.sample.SampleToChunk.Entries[self.chunkGroupIndex].SamplesPerChunk)
        if index >= start && index < start+n {
            found = true
            self.sampleIndexInChunk = index - start
            break
        }
        start += n
    }
    if !found {
        err = fmt.Errorf("mp4: stream[%d]: cannot locate sample index in chunk", self.idx)
        return
    }
    
    // 2. Розрахунок offset'у даних у чанку
    if self.sample.SampleSize.SampleSize != 0 {
        // Фіксований розмір для всіх семплів
        self.sampleOffsetInChunk = int64(self.sampleIndexInChunk) * int64(self.sample.SampleSize.SampleSize)
    } else {
        // Змінний розмір: підсумовуємо розміри попередніх семплів
        if index >= len(self.sample.SampleSize.Entries) {
            err = fmt.Errorf("mp4: stream[%d]: sample index out of range", self.idx)
            return
        }
        self.sampleOffsetInChunk = int64(0)
        for i := index - self.sampleIndexInChunk; i < index; i++ {
            self.sampleOffsetInChunk += int64(self.sample.SampleSize.Entries[i])
        }
    }
    
    // 3. Розрахунок DTS через stts таблицю
    self.dts = int64(0)
    start = 0
    found = false
    self.sttsEntryIndex = 0
    for self.sttsEntryIndex < len(self.sample.TimeToSample.Entries) {
        entry := self.sample.TimeToSample.Entries[self.sttsEntryIndex]
        n := int(entry.Count)
        if index >= start && index < start+n {
            self.sampleIndexInSttsEntry = index - start
            self.dts += int64(index-start) * int64(entry.Duration)
            found = true
            break
        }
        start += n
        self.dts += int64(n) * int64(entry.Duration)
        self.sttsEntryIndex++
    }
    
    // 4. Аналогічно для ctts (Composition Offset) та sync samples...
    
    self.sampleIndex = index
    return
}
```

### 🔍 Чому такий складний пошук?

```
MP4 використовує стиснуті таблиці для економії місця:

stsc (Sample-To-Chunk):
  Замість зберігання запису для кожного чанку:
  [ {FirstChunk:1, SamplesPerChunk:4}, {FirstChunk:10, SamplesPerChunk:1} ]
  → Чанки 1-9: по 4 семпли, чанки 10+: по 1 семплу

stts (Time-To-Sample):
  Замість зберігання тривалості для кожного семплу:
  [ {Count:25, Duration:1000}, {Count:1, Duration:1001} ]
  → 25 семплів по 1000 ticks, потім 1 семпл 1001 ticks

Це економить пам'ять, але вимагає лінійного пошуку при навігації.
Для великих файлів можна оптимізувати бінарним пошуком або кешуванням.
```

### ✅ Ваш use-case**: швидкий пошук ключового кадру

```go
// FindNearestKeyFrame — пошук найближчого ключового кадру до часу
func (s *Stream) FindNearestKeyFrame(targetTime time.Duration) (int, error) {
    // 1. Конвертація часу у ticks
    targetTicks := s.timeToTs(targetTime)
    
    // 2. Пошук індексу семплу через stts
    sampleIndex := s.timeToSampleIndex(targetTime)
    
    // 3. Якщо це не ключовий кадр — шукаємо попередній у stss
    if s.sample.SyncSample != nil {
        entries := s.sample.SyncSample.Entries
        // Лінійний пошук (можна оптимізувати бінарним)
        for i := len(entries) - 1; i >= 0; i-- {
            if entries[i]-1 <= uint32(sampleIndex) {
                return int(entries[i] - 1), nil
            }
        }
    }
    
    // Якщо немає ключових кадрів — повертаємо знайдений індекс
    return sampleIndex, nil
}
```

---

## 🔑 4. ReadPacket() — читання з синхронізацією треків

### 🔧 Логіка вибору наступного семплу:

```go
func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
    if err = self.probe(); err != nil {
        return
    }
    if len(self.streams) == 0 {
        err = errors.New("mp4: no streams available while trying to read a packet")
        return
    }
    
    // 1. Вибір треку з найменшим DTS (синхронізація аудіо/відео)
    var chosen *Stream
    var chosenidx int
    for i, stream := range self.streams {
        if chosen == nil || stream.tsToTime(stream.dts) < chosen.tsToTime(chosen.dts) {
            chosen = stream
            chosenidx = i
        }
    }
    
    // 2. Читання пакету з обраного треку
    tm := chosen.tsToTime(chosen.dts)
    if pkt, err = chosen.readPacket(); err != nil {
        return
    }
    
    // 3. Встановлення метаданих пакету
    pkt.Time = tm
    pkt.Idx = int8(chosenidx)
    return
}
```

### 🔧 readPacket() — читання даних семплу:

```go
func (self *Stream) readPacket() (pkt av.Packet, err error) {
    // 1. Перевірка чи не дійшли до кінця
    if !self.isSampleValid() {
        err = io.EOF
        return
    }
    
    // 2. Розрахунок позиції даних у файлі
    chunkOffset := self.sample.ChunkOffset.Entries[self.chunkIndex]
    sampleSize := uint32(0)
    if self.sample.SampleSize.SampleSize != 0 {
        sampleSize = self.sample.SampleSize.SampleSize
    } else {
        sampleSize = self.sample.SampleSize.Entries[self.sampleIndex]
    }
    
    sampleOffset := int64(chunkOffset) + self.sampleOffsetInChunk
    
    // 3. Читання даних
    pkt.Data = make([]byte, sampleSize)
    if err = self.demuxer.readat(sampleOffset, pkt.Data); err != nil {
        return
    }
    
    // 4. Визначення ключового кадру через stss таблицю
    if self.sample.SyncSample != nil {
        if self.sample.SyncSample.Entries[self.syncSampleIndex]-1 == uint32(self.sampleIndex) {
            pkt.IsKeyFrame = true
        }
    }
    
    // 5. Розрахунок Composition Time (для B-frames)
    if self.sample.CompositionOffset != nil && len(self.sample.CompositionOffset.Entries) > 0 {
        cts := int64(self.sample.CompositionOffset.Entries[self.cttsEntryIndex].Offset)
        pkt.CompositionTime = self.tsToTime(cts)
    }
    
    // 6. Підготовка до наступного семплу
    self.incSampleIndex()
    
    return
}
```

### ⚠️ Критична проблема: аллокація буфера для кожного пакету

```go
pkt.Data = make([]byte, sampleSize)  // ← нова аллокація для кожного пакету!
```

**Наслідки**: Для high-FPS відео (30+ fps) це створює значне навантаження на GC.

**✅ Виправлення**: Використання пулу буферів:

```go
var packetBufferPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 64*1024)  // 64KB достатньо для більшості кадрів
        return &buf
    },
}

func (self *Stream) readPacketPooled() (pkt av.Packet, err error) {
    // ... розрахунок sampleSize ...
    
    // Отримання буфера з пулу
    bufPtr := packetBufferPool.Get().(*[]byte)
    defer packetBufferPool.Put(bufPtr)
    
    if sampleSize > uint32(cap(*bufPtr)) {
        // Якщо буфер замалий — створюємо новий
        *bufPtr = make([]byte, sampleSize)
    } else {
        *bufPtr = (*bufPtr)[:sampleSize]  // встановлення довжини
    }
    
    if err = self.demuxer.readat(sampleOffset, *bufPtr); err != nil {
        return
    }
    
    // Копіювання даних у pkt.Data (щоб уникнути проблем з пулом)
    pkt.Data = make([]byte, sampleSize)
    copy(pkt.Data, *bufPtr)
    
    // ... решта логіки ...
    return
}
```

---

## 🔑 5. SeekToTime() — пошук за часом

### 🔧 Логіка синхронізованого seek:

```go
func (self *Demuxer) SeekToTime(tm time.Duration) (err error) {
    // 1. Спочатку seek відео треку (якщо є)
    for _, stream := range self.streams {
        if stream.Type().IsVideo() {
            if err = stream.seekToTime(tm); err != nil {
                return
            }
            // Використовуємо фактичний час відео для синхронізації аудіо
            tm = stream.tsToTime(stream.dts)
            break
        }
    }
    
    // 2. Потім seek аудіо треків до того ж часу
    for _, stream := range self.streams {
        if !stream.Type().IsVideo() {
            if err = stream.seekToTime(tm); err != nil {
                return
            }
        }
    }
    
    return
}
```

### 🔧 timeToSampleIndex() — пошук індексу за часом:

```go
func (self *Stream) timeToSampleIndex(tm time.Duration) int {
    targetTs := self.timeToTs(tm)
    targetIndex := 0
    
    // Лінійний пошук у stts таблиці
    startTs := int64(0)
    endTs := int64(0)
    startIndex := 0
    endIndex := 0
    found := false
    
    for _, entry := range self.sample.TimeToSample.Entries {
        endTs = startTs + int64(entry.Count*entry.Duration)
        endIndex = startIndex + int(entry.Count)
        
        if targetTs >= startTs && targetTs < endTs {
            // Знайдено запис: розрахунок точного індексу
            targetIndex = startIndex + int((targetTs-startTs)/int64(entry.Duration))
            found = true
        }
        startTs = endTs
        startIndex = endIndex
    }
    
    // Обробка крайніх випадків
    if !found {
        if targetTs < 0 {
            targetIndex = 0
        } else {
            targetIndex = endIndex - 1  // останній семпл
        }
    }
    
    // Якщо є таблиця ключових кадрів — округлюємо до попереднього keyframe
    if self.sample.SyncSample != nil {
        entries := self.sample.SyncSample.Entries
        for i := len(entries) - 1; i >= 0; i-- {
            if entries[i]-1 < uint32(targetIndex) {
                targetIndex = int(entries[i] - 1)
                break
            }
        }
    }
    
    return targetIndex
}
```

### ✅ Ваш use-case**: плавний seek з буферизацією

```go
// SeekWithBuffer — seek з попередньою буферизацією для плавності
func (d *mp4.Demuxer) SeekWithBuffer(targetTime time.Duration, bufferDuration time.Duration) error {
    // 1. Основний seek
    if err := d.SeekToTime(targetTime); err != nil {
        return err
    }
    
    // 2. Попереднє читання буфера пакетів
    buffer := make([]av.Packet, 0, 30)  // ~1 секунда при 30fps
    endTime := targetTime.Add(bufferDuration)
    
    for {
        pkt, err := d.ReadPacket()
        if err == io.EOF { break }
        if err != nil { return err }
        
        if pkt.Time.After(endTime) {
            // Повертаємо пакет назад у чергу (спрощено)
            break
        }
        buffer = append(buffer, pkt)
    }
    
    // 3. Збереження буфера для наступних ReadPacket() викликів
    // (у реальній реалізації: використання internal queue)
    
    return nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Транскодування MP4 → HLS

```go
// TranscodeMP4ToHLS — конвертація локального MP4 у HLS сегменти
func TranscodeMP4ToHLS(inputFile, outputDir string, segmentDuration time.Duration) error {
    // 1. Відкриття вхідного файлу
    demuxer, err := mp4.OpenMP4File(inputFile)
    if err != nil {
        return fmt.Errorf("open input: %w", err)
    }
    defer demuxer.Close()
    
    // 2. Отримання метаданих
    streams, err := demuxer.Streams()
    if err != nil {
        return fmt.Errorf("probe streams: %w", err)
    }
    
    // 3. Ініціалізація HLS муксера
    hlsMuxer, err := hls.NewMuxer(outputDir, streams)
    if err != nil {
        return fmt.Errorf("init HLS: %w", err)
    }
    
    // 4. Основний цикл транскодування
    var segmentStart time.Duration
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF { break }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Запис у HLS
        if err := hlsMuxer.WritePacket(pkt); err != nil {
            return fmt.Errorf("write HLS: %w", err)
        }
        
        // Ротація сегменту за часом
        if pkt.Time-segmentStart >= segmentDuration {
            if err := hlsMuxer.StartNewSegment(); err != nil {
                return fmt.Errorf("new segment: %w", err)
            }
            segmentStart = pkt.Time
        }
    }
    
    // 5. Фіналізація
    return hlsMuxer.WriteTrailer()
}
```

### 🔧 Приклад: Відео-прев'ю з seek

```go
// GenerateVideoPreview — створення прев'ю з довільного моменту
func GenerateVideoPreview(inputFile string, previewTime time.Duration, duration time.Duration) ([]byte, error) {
    demuxer, err := mp4.OpenMP4File(inputFile)
    if err != nil { return nil, err }
    defer demuxer.Close()
    
    // Seek до потрібного часу
    if err := demuxer.SeekToTime(previewTime); err != nil {
        return nil, fmt.Errorf("seek: %w", err)
    }
    
    // Буфер для прев'ю даних
    var previewData bytes.Buffer
    endTime := previewTime.Add(duration)
    
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF || pkt.Time.After(endTime) { break }
        if err != nil { return nil, err }
        
        // Копіювання даних у буфер (спрощено)
        previewData.Write(pkt.Data)
    }
    
    return previewData.Bytes(), nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"moov atom not found"** | Помилка при probe() для "fast start" файлів | Переконайтеся, що файл не пошкоджений; moov має бути на початку або в кінці |
| **Повільний seek у великих файлах** | Лінійний пошук у stts/stsc займає час | Реалізуйте кешування індексів або бінарний пошук |
| **Розсинхронізація аудіо/відео** | Різні timeScale у треках | Нормалізуйте часи до спільної шкали перед порівнянням |
| **Високе споживання пам'яті** | Аллокація буфера для кожного пакету | Використовуйте `sync.Pool` для буферів |
| **Невірний ключовий кадр** | seek не потрапляє у keyframe | Переконайтеся, що stss таблиця коректна; округлюйте до попереднього keyframe |

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
type DemuxerMetrics struct {
    PacketReadLatency prometheus.HistogramVec
    SeekLatency       prometheus.HistogramVec
    CacheHitRatio     prometheus.GaugeVec
}

func (m *DemuxerMetrics) RecordPacketRead(duration time.Duration, streamID string) {
    m.PacketReadLatency.WithLabelValues(streamID).Observe(duration.Seconds())
}

func (m *DemuxerMetrics) RecordSeek(duration time.Duration, streamID string) {
    m.SeekLatency.WithLabelValues(streamID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист використання mp4.Demuxer

```go
// ✅ 1. Відкриття файлу з перевіркою
demuxer, err := mp4.OpenMP4File("video.mp4")
if err != nil { /* handle error */ }
defer demuxer.Close()

// ✅ 2. Отримання метаданих перед читанням
streams, err := demuxer.Streams()
if err != nil { /* handle error */ }

// ✅ 3. Seek тільки після успішного probe()
if err := demuxer.SeekToTime(10 * time.Second); err != nil { /* handle */ }

// ✅ 4. Обробка помилок читання з відновленням
for {
    pkt, err := demuxer.ReadPacket()
    if err == io.EOF { break }
    if err != nil {
        log.Printf("read error: %v, attempting recovery", err)
        // логіка відновлення...
        continue
    }
    // обробка пакету...
}

// ✅ 5. Синхронізація аудіо/відео через Time поле
if pkt.Time < lastAudioTime && pkt.Idx == videoIdx {
    // Відео відстає — можна пропустити кадр або буферизувати
}

// ✅ 6. Метрики для моніторингу
metrics.RecordPacketRead(time.Since(start), streamID)
```

---

## 🔗 Корисні посилання

- 💻 [vdk mp4 Package](https://pkg.go.dev/github.com/deepch/vdk/format/mp4) — GoDoc documentation
- 📄 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [MP4 Sample Tables Explained](https://wiki.multimedia.cx/index.php/MP4) — stts/ctts/stsc детальний опис
- 🧪 [Go sync.Pool Best Practices](https://go.dev/blog/pool) — ефективне управління пам'яттю
- 📦 [Prometheus Metrics for Media](https://prometheus.io/docs/practices/instrumentation/) — моніторинг продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди викликайте `Streams()` після `NewDemuxer()`** — це гарантує, що `probe()` виконано та помилки оброблені.
> 2. **Кешуйте індекси пошуку для великих файлів** — лінійний пошук у stts/stsc може бути повільним.
> 3. **Використовуйте `sync.Pool` для буферів пакетів** — уникнення аллокацій критично для high-FPS відео.
> 4. **Округлюйте seek до ключових кадрів** — це забезпечує коректне декодування після seek.
> 5. **Моніторьте `SeekLatency`** — різке зростання може вказувати на фрагментацію файлу або проблеми з диском.

Потрібен приклад реалізації бінарного пошуку у stts таблиці для прискорення `timeToSampleIndex()` у великих файлах? Готовий допомогти! 🚀