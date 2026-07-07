# 📦 Глибокий розбір: `mp4.Muxer` — Запис стандартного MP4 контейнера

Цей файл — **повноцінна реалізація муксера для MP4 (ISO BMFF) контейнера**, що перетворює `av.Packet` у стандартизований файл з атомами `moov` та `mdat`. Він підтримує H.264/H.265 відео та AAC аудіо, оптимізує запис через буферизацію, та генерує всі необхідні метадані для сумісності з плеєрами.

---

## 🗺️ Архітектурна схема mp4.Muxer

```
┌────────────────────────────────────────┐
│ 📦 mp4.Muxer — ISO BMFF Writer        │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Muxer — основний контролер          │
│  • Stream — обробка окремого треку     │
│  • WriteHeader/WritePacket/WriteTrailer│
│  • fillTrackAtom — генерація метаданих │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet → буферизація → MP4 атоми  │
│  → io.WriteSeeker (файл/мережа)       │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (AVC1Desc + AVC1Conf) │
│  • Відео: H.265 (HV1Desc + HV1Conf)   │
│  • Аудіо: AAC (MP4ADesc + ElemStreamDesc)│
│  • Формат: ISO BMFF (moov + mdat)     │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Muxer — основна структура

### Поля та їх призначення:

```go
type Muxer struct {
    w                  io.WriteSeeker  // вихідний потік (файл/буфер)
    bufw               *bufio.Writer   // буферизований writer для ефективності
    wpos               int64           // поточна позиція запису (для offset)
    streams            []*Stream       // масив треків (відео=0, аудіо=1...)
    NegativeTsMakeZero bool            // прапорець обробки від'ємних duration
}
```

### 🔧 NewMuxer() — ініціалізація з буферизацією:

```go
func NewMuxer(w io.WriteSeeker) *Muxer {
    return &Muxer{
        w:    w,
        bufw: bufio.NewWriterSize(w, pio.RecommendBufioSize),  // оптимальний розмір буфера
    }
}
```

**✅ Ваш use-case**: запис у файл з автоматичним флешем

```go
// WriteMP4File — зручна обгортка для запису у файл
func WriteMP4File(filename string, packets []av.Packet, streams []av.CodecData) error {
    f, err := os.Create(filename)
    if err != nil { return err }
    defer f.Close()
    
    muxer := mp4.NewMuxer(f)
    if err := muxer.WriteHeader(streams); err != nil { return err }
    
    for _, pkt := range packets {
        if err := muxer.WritePacket(pkt); err != nil { return err }
    }
    
    return muxer.WriteTrailer()  // автоматичний флеш + запис moov
}
```

---

## 🔑 2. newStream() — створення треку з метаданими

### 🔧 Ініціалізація SampleTable:

```go
stream.sample = &mp4io.SampleTable{
    SampleDesc:   &mp4io.SampleDesc{},           // stsd: опис кодека
    TimeToSample: &mp4io.TimeToSample{},         // stts: DTS розрахунок
    SampleToChunk: &mp4io.SampleToChunk{         // stsc: мапінг семплів у чанки
        Entries: []mp4io.SampleToChunkEntry{
            {FirstChunk: 1, SampleDescId: 1, SamplesPerChunk: 1},
        },
    },
    SampleSize:  &mp4io.SampleSize{},            // stsz: розміри семплів
    ChunkOffset: &mp4io.ChunkOffset{},           // stco: позиції чанків у файлі
}
```

### 🔧 Ініціалізація TrackAtom (trak box):

```go
stream.trackAtom = &mp4io.Track{
    Header: &mp4io.TrackHeader{
        TrackId:  int32(len(self.streams) + 1),  // унікальний ID треку
        Flags:    0x0003,  // Track enabled | Track in movie
        Duration: 0,       // заповнюється пізніше у WriteTrailer
        Matrix:   [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},  // identity matrix
    },
    Media: &mp4io.Media{
        Header: &mp4io.MediaHeader{
            TimeScale: 0,  // заповнюється пізніше
            Duration:  0,  // заповнюється пізніше
            Language:  21956,  // 'und' (undefined) у MP4 format
        },
        Info: &mp4io.MediaInfo{
            Sample: stream.sample,  // посилання на таблицю семплів
            Data: &mp4io.DataInfo{
                Refer: &mp4io.DataRefer{
                    Url: &mp4io.DataReferUrl{Flags: 0x000001},  // self-reference
                },
            },
        },
    },
}
```

### 🔧 Специфіка для відео: SyncSample таблиця

```go
switch codec.Type() {
case av.H264, av.H265:
    stream.sample.SyncSample = &mp4io.SyncSample{}  // stss: індекси ключових кадрів
}
```

**🔍 Чому тільки для відео?**
- Аудіо (AAC) не має поняття "ключових кадрів" — кожен фрейм незалежний
- Для відео stss таблиця критична для:
  • Seek operations (пошук точки відтворення)
  • Low-latency стрімінг (початок з ключового кадру)
  • Редагування (вирізання сегментів)

### ✅ Ваш use-case**: валідація кодеків перед створенням треку

```go
// ValidateCodecForMP4 — перевірка сумісності з MP4 форматом
func ValidateCodecForMP4(codec av.CodecData) error {
    switch codec.Type() {
    case av.H264:
        // Перевірка наявності SPS/PPS
        h264, ok := codec.(h264parser.CodecData)
        if !ok {
            return fmt.Errorf("invalid H.264 codec data")
        }
        if len(h264.SPS()) == 0 || len(h264.PPS()) == 0 {
            return fmt.Errorf("H.264 codec missing SPS/PPS")
        }
        
    case av.H265:
        // Перевірка наявності VPS/SPS/PPS
        h265, ok := codec.(h265parser.CodecData)
        if !ok {
            return fmt.Errorf("invalid H.265 codec data")
        }
        if len(h265.VPS()) == 0 || len(h265.SPS()) == 0 || len(h265.PPS()) == 0 {
            return fmt.Errorf("H.265 codec missing VPS/SPS/PPS")
        }
        
    case av.AAC:
        // Перевірка AudioSpecificConfig
        aac, ok := codec.(aacparser.CodecData)
        if !ok {
            return fmt.Errorf("invalid AAC codec data")
        }
        if len(aac.MPEG4AudioConfigBytes()) == 0 {
            return fmt.Errorf("AAC codec missing config")
        }
        
    default:
        return fmt.Errorf("unsupported codec for MP4: %v", codec.Type())
    }
    return nil
}
```

---

## 🔑 3. fillTrackAtom() — генерація кодек-специфічних метаданих

### 🔧 H.264: AVC1Desc + AVC1Conf

```go
case av.H264:
    codec := self.CodecData.(h264parser.CodecData)
    width, height := codec.Width(), codec.Height()
    
    self.sample.SampleDesc.AVC1Desc = &mp4io.AVC1Desc{
        DataRefIdx:           1,
        HorizontalResolution: 72,  // DPI: 72 = стандарт для відео
        VorizontalResolution: 72,  // ⚠️ Опечатка: має бути VerticalResolution
        Width:                int16(width),
        Height:               int16(height),
        FrameCount:           1,   // кількість кадрів у семплі (зазвичай 1)
        Depth:                24,  // біт на піксель
        ColorTableId:         -1,  // без палітри
        Conf: &mp4io.AVC1Conf{
            Data: codec.AVCDecoderConfRecordBytes(),  // ⚠️ Критично: AVCDecoderConfigurationRecord
        },
    }
    
    self.trackAtom.Media.Handler = &mp4io.HandlerRefer{
        SubType: [4]byte{'v', 'i', 'd', 'e'},  // 'vide' = video track
        Name:    []byte("Video Media Handler"),
    }
    self.trackAtom.Media.Info.Video = &mp4io.VideoMediaInfo{Flags: 0x000001}
    self.trackAtom.Header.TrackWidth = float64(width)
    self.trackAtom.Header.TrackHeight = float64(height)
```

### 🔍 AVCDecoderConfigurationRecord (AVCC):

```
Це критична структура для H.264 у MP4:

struct AVCDecoderConfigurationRecord {
    configurationVersion: 1
    AVCProfileIndication: profile_idc (напр. 66=Baseline, 77=Main, 100=High)
    profile_compatibility: 1 байт
    AVCLevelIndication: level_idc (напр. 30=3.0, 40=4.0)
    lengthSizeMinusOne: 3 (означає 4-байтові довжини NALU)
    numOfSequenceParameterSets: 1
    sequenceParameterSetLength: len(SPS)
    sequenceParameterSetNALUnit: SPS дані
    numOfPictureParameterSets: 1
    pictureParameterSetLength: len(PPS)
    pictureParameterSetNALUnit: PPS дані
}

Без цієї структури плеєр не зможе ініціалізувати декодер!
```

### 🔧 H.265: HV1Desc + HV1Conf

```go
case av.H265:
    codec := self.CodecData.(h265parser.CodecData)
    width, height := codec.Width(), codec.Height()
    
    self.sample.SampleDesc.HV1Desc = &mp4io.HV1Desc{
        DataRefIdx:           1,
        HorizontalResolution: 72,
        VorizontalResolution: 72,  // ⚠️ Опечатка: має бути VerticalResolution
        Width:                int16(width),
        Height:               int16(height),
        FrameCount:           1,
        Depth:                24,
        ColorTableId:         -1,
        Conf: &mp4io.HV1Conf{
            Data: codec.AVCDecoderConfRecordBytes(),  // ⚠️ Критично: HVCC для H.265
        },
    }
    // ... решта аналогічно H.264 ...
```

### 🔧 AAC: MP4ADesc + ElemStreamDesc

```go
case av.AAC:
    codec := self.CodecData.(aacparser.CodecData)
    
    self.sample.SampleDesc.MP4ADesc = &mp4io.MP4ADesc{
        DataRefIdx:       1,
        NumberOfChannels: int16(codec.ChannelLayout().Count()),
        SampleSize:       int16(codec.SampleFormat().BytesPerSample()),
        SampleRate:       float64(codec.SampleRate()),
        Conf: &mp4io.ElemStreamDesc{
            DecConfig: codec.MPEG4AudioConfigBytes(),  // ⚠️ Критично: AudioSpecificConfig
        },
    }
    
    self.trackAtom.Header.Volume = 1  // гучність (1.0 = максимум)
    self.trackAtom.Header.AlternateGroup = 1  // для синхронізації аудіо/відео
    self.trackAtom.Media.Handler = &mp4io.HandlerRefer{
        SubType: [4]byte{'s', 'o', 'u', 'n'},  // 'soun' = audio track
        Name:    []byte("Sound Handler"),
    }
    self.trackAtom.Media.Info.Sound = &mp4io.SoundMediaInfo{}
```

### ⚠️ Критична проблема: опечатка у назві поля

```go
VorizontalResolution: 72,  // ← Має бути VerticalResolution!
```

**Наслідки**: Деякі плеєри можуть ігнорувати це поле, але інші (напр. старі версії QuickTime) можуть відмовитися відтворювати файл.

**✅ Виправлення**:
```go
VerticalResolution: 72,  // Правильна назва поля
```

---

## 🔑 4. WriteHeader() — ініціалізація файлу

### 🔧 Логіка запису заголовку:

```go
func (self *Muxer) WriteHeader(streams []av.CodecData) (err error) {
    // 1. Створення треків для кожного кодека
    self.streams = []*Stream{}
    for _, stream := range streams {
        if err = self.newStream(stream); err != nil { return }
    }
    
    // 2. Запис заголовку mdat атому (поки з нульовим розміром)
    taghdr := make([]byte, 8)
    pio.PutU32BE(taghdr[4:], uint32(mp4io.MDAT))  // 'mdat' = media data
    if _, err = self.w.Write(taghdr); err != nil { return }
    self.wpos += 8  // оновлення позиції запису
    
    // 3. Ініціалізація CompositionOffset для відео треків
    for _, stream := range self.streams {
        if stream.Type().IsVideo() {
            stream.sample.CompositionOffset = &mp4io.CompositionOffset{}  // ctts таблиця
        }
    }
    return
}
```

### 🔍 Чому mdat записується першим?

```
Стандартний порядок атомів у MP4:
  [ftyp][moov][mdat]  ← "fast start" (moov на початку)
  [ftyp][mdat][moov]  ← "slow start" (mdat на початку, як у цьому коді)

Цей муксер використовує "slow start":
  1. Записуємо mdat заголовок з нульовим розміром
  2. Записуємо медіа-дані у mdat
  3. У WriteTrailer():
     • Записуємо moov атом у кінець файлу
     • Повертаємось на початок і оновлюємо розмір mdat

Переваги "slow start":
• Не потрібно буферизувати всі дані в пам'яті
• Підходить для потокового запису великих файлів

Недоліки:
• Плеєр має прочитати весь файл, щоб знайти moov
• Не підходить для HTTP range requests без додаткової обробки
```

---

## 🔑 5. WritePacket() — буферизація та запис семплів

### 🔧 Логіка відкладеного запису:

```go
func (self *Muxer) WritePacket(pkt av.Packet) (err error) {
    stream := self.streams[pkt.Idx]
    
    // Запис попереднього пакету з розрахунком duration
    if stream.lastpkt != nil {
        if err = stream.writePacket(*stream.lastpkt, pkt.Time - stream.lastpkt.Time); err != nil {
            return
        }
    }
    
    // Збереження поточного пакету для наступного виклику
    stream.lastpkt = &pkt
    return
}
```

**🔍 Чому відкладений запис?**
- `duration` семплу = різниця часу між поточним та наступним пакетом
- Для останнього пакету у потоці `duration` невідомий → обробляється у `WriteTrailer()`

### 🔧 writePacket() — основна логіка:

```go
func (self *Stream) writePacket(pkt av.Packet, rawdur time.Duration) (err error) {
    // 1. Обробка від'ємних duration
    if rawdur < 0 {
        if self.muxer.NegativeTsMakeZero {
            rawdur = 0  // force non-negative
        } else {
            err = fmt.Errorf("mp4: stream#%d time=%v < lasttime=%v", 
                pkt.Idx, pkt.Time, self.lastpkt.Time)
            return
        }
    }
    
    // 2. Запис сирих даних у буфер
    if _, err = self.muxer.bufw.Write(pkt.Data); err != nil { return }
    
    // 3. Оновлення SyncSample таблиці для ключових кадрів
    if pkt.IsKeyFrame && self.sample.SyncSample != nil {
        self.sample.SyncSample.Entries = append(
            self.sample.SyncSample.Entries, 
            uint32(self.sampleIndex+1))  // 1-based indexing у MP4
    }
    
    // 4. Оновлення TimeToSample (stts) таблиці
    duration := uint32(self.timeToTs(rawdur))
    if self.sttsEntry == nil || duration != self.sttsEntry.Duration {
        // Новий запис у stts: змінена тривалість
        self.sample.TimeToSample.Entries = append(
            self.sample.TimeToSample.Entries, 
            mp4io.TimeToSampleEntry{Duration: duration})
        self.sttsEntry = &self.sample.TimeToSample.Entries[len(self.sample.TimeToSample.Entries)-1]
    }
    self.sttsEntry.Count++  // інкремент кількості семплів з цією тривалістю
    
    // 5. Оновлення CompositionOffset (ctts) для відео
    if self.sample.CompositionOffset != nil {
        offset := uint32(self.timeToTs(pkt.CompositionTime))
        if self.cttsEntry == nil || offset != self.cttsEntry.Offset {
            table := self.sample.CompositionOffset
            table.Entries = append(table.Entries, 
                mp4io.CompositionOffsetEntry{Offset: offset})
            self.cttsEntry = &table.Entries[len(table.Entries)-1]
        }
        self.cttsEntry.Count++
    }
    
    // 6. Оновлення глобальних лічильників
    self.duration += int64(duration)
    self.sampleIndex++
    
    // 7. Оновлення ChunkOffset (stco) та SampleSize (stsz)
    self.sample.ChunkOffset.Entries = append(
        self.sample.ChunkOffset.Entries, 
        uint32(self.muxer.wpos))  // позиція даних у файлі
    self.sample.SampleSize.Entries = append(
        self.sample.SampleSize.Entries, 
        uint32(len(pkt.Data)))  // розмір семплу
    
    self.muxer.wpos += int64(len(pkt.Data))  // оновлення позиції запису
    return
}
```

### ⚠️ Критична проблема: 32-бітні offset у ChunkOffset

```go
self.sample.ChunkOffset.Entries = append(
    self.sample.ChunkOffset.Entries, 
    uint32(self.muxer.wpos))  // ← Перетворення int64 → uint32!
```

**Наслідки**: Для файлів > 4 ГБ (2^32 байт) offset переповниться → некоректні посилання на дані.

**✅ Виправлення**: Використовувати `ChunkOffset64` для великих файлів:

```go
// Перевірка чи потрібен 64-бітний offset
if self.muxer.wpos > 0xFFFFFFFF {
    // Використання co64 замість stco
    if self.sample.ChunkOffset64 == nil {
        self.sample.ChunkOffset64 = &mp4io.ChunkOffset64{}
    }
    self.sample.ChunkOffset64.Entries = append(
        self.sample.ChunkOffset64.Entries, 
        uint64(self.muxer.wpos))
} else {
    // Стандартний 32-бітний offset
    self.sample.ChunkOffset.Entries = append(
        self.sample.ChunkOffset.Entries, 
        uint32(self.muxer.wpos))
}
```

---

## 🔑 6. WriteTrailer() — фіналізація файлу

### 🔧 Логіка завершення:

```go
func (self *Muxer) WriteTrailer() (err error) {
    // 1. Запис останнього пакету з duration=0
    for _, stream := range self.streams {
        if stream.lastpkt != nil {
            if err = stream.writePacket(*stream.lastpkt, 0); err != nil { return }
            stream.lastpkt = nil
        }
    }
    
    // 2. Створення moov атому
    moov := &mp4io.Movie{}
    moov.Header = &mp4io.MovieHeader{
        PreferredRate:   1,
        PreferredVolume: 1,
        Matrix:          [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
        NextTrackId:     2,  // ⚠️ Має бути len(streams)+1 для кількох треків!
    }
    
    // 3. Заповнення метаданих треків
    maxDur := time.Duration(0)
    timeScale := int64(10000)  // ⚠️ Фіксована шкала часу для moov, не для треків!
    
    for _, stream := range self.streams {
        if err = stream.fillTrackAtom(); err != nil { return }
        
        // Конвертація тривалості треку у moov timeScale
        dur := stream.tsToTime(stream.duration)
        stream.trackAtom.Header.Duration = int32(timeToTs(dur, timeScale))
        
        if dur > maxDur { maxDur = dur }
        moov.Tracks = append(moov.Tracks, stream.trackAtom)
    }
    
    moov.Header.TimeScale = int32(timeScale)
    moov.Header.Duration = int32(timeToTs(maxDur, timeScale))
    
    // 4. Флеш буфера даних
    if err = self.bufw.Flush(); err != nil { return }
    
    // 5. Оновлення розміру mdat атому на початку файлу
    var mdatsize int64
    if mdatsize, err = self.w.Seek(0, 1); err != nil { return }  // поточна позиція
    if _, err = self.w.Seek(0, 0); err != nil { return }         // початок файлу
    
    taghdr := make([]byte, 4)
    pio.PutU32BE(taghdr, uint32(mdatsize))  // ⚠️ Перетворення int64 → uint32!
    if _, err = self.w.Write(taghdr); err != nil { return }
    
    // 6. Запис moov атому у кінець файлу
    if _, err = self.w.Seek(0, 2); err != nil { return }  // кінець файлу
    b := make([]byte, moov.Len())
    moov.Marshal(b)
    if _, err = self.w.Write(b); err != nil { return }
    
    return
}
```

### ⚠️ Критичні проблеми у WriteTrailer():

#### ❌ 1. NextTrackId завжди = 2

```go
NextTrackId: 2,  // ← Має бути len(self.streams) + 1!
```

**Наслідки**: Для файлів з >1 треком це порушує специфікацію, хоча більшість плеєрів ігнорують це поле.

**✅ Виправлення**:
```go
NextTrackId: int32(len(self.streams) + 1),
```

#### ❌ 2. Фіксована timeScale для moov

```go
timeScale := int64(10000)  // ← Фіксоване значення, не залежить від треків!
```

**Проблема**: Якщо треки мають різні timeScale (напр. відео=90000, аудіо=48000), конвертація у спільну шкалу може призвести до втрати точності.

**✅ Виправлення**: Використовувати найбільший timeScale серед треків:

```go
timeScale := int64(0)
for _, stream := range self.streams {
    if stream.timeScale > timeScale {
        timeScale = stream.timeScale
    }
}
if timeScale == 0 { timeScale = 10000 }  // fallback
```

#### ❌ 3. Переповнення розміру mdat

```go
pio.PutU32BE(taghdr, uint32(mdatsize))  // ← int64 → uint32!
```

**Наслідки**: Для файлів > 4 ГБ розмір mdat переповниться → некоректний файл.

**✅ Виправлення**: Використовувати "large size" формат атомів:

```go
if mdatsize > 0xFFFFFFFF {
    // Великий атом: size=1, потім 64-бітний розмір
    pio.PutU32BE(taghdr, 1)  // special value for large size
    pio.PutU64BE(taghdr[4:], uint64(mdatsize))
    if _, err = self.w.Write(taghdr[:12]); err != nil { return }
} else {
    // Стандартний атом
    pio.PutU32BE(taghdr, uint32(mdatsize))
    if _, err = self.w.Write(taghdr[:4]); err != nil { return }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Запис камери у локальний MP4 файл

```go
// RecordCameraToMP4 — запис потоку камери у файл
func RecordCameraToMP4(cameraSource av.Demuxer, outputPath string, duration time.Duration) error {
    // 1. Відкриття файлу
    f, err := os.Create(outputPath)
    if err != nil { return err }
    defer f.Close()
    
    // 2. Створення муксера
    muxer := mp4.NewMuxer(f)
    
    // 3. Отримання метаданих потоків
    streams, err := cameraSource.Streams()
    if err != nil { return fmt.Errorf("get streams: %w", err) }
    
    // 4. Валідація кодеків
    for _, s := range streams {
        if err := ValidateCodecForMP4(s); err != nil {
            return fmt.Errorf("invalid codec: %w", err)
        }
    }
    
    // 5. Запис заголовку
    if err := muxer.WriteHeader(streams); err != nil { return err }
    
    // 6. Основний цикл запису
    deadline := time.Now().Add(duration)
    for time.Now().Before(deadline) {
        pkt, err := cameraSource.ReadPacket()
        if err == io.EOF { break }
        if err != nil {
            log.Printf("read error: %v", err)
            continue
        }
        
        if err := muxer.WritePacket(pkt); err != nil {
            log.Printf("write error: %v", err)
            break
        }
    }
    
    // 7. Фіналізація файлу
    return muxer.WriteTrailer()
}
```

### 🔧 Приклад: Стрімінг у мережу з буферизацією

```go
// StreamMP4ToNetwork — запис у мережевий потік з контролем буфера
func StreamMP4ToNetwork(conn net.Conn, packets <-chan av.Packet, streams []av.CodecData) error {
    // Буферизований writer для мережі
    bufConn := bufio.NewWriterSize(conn, 64*1024)  // 64KB буфер
    
    muxer := mp4.NewMuxer(bufConn)
    if err := muxer.WriteHeader(streams); err != nil { return err }
    
    // Фоновий флеш буфера
    done := make(chan error, 1)
    go func() {
        ticker := time.NewTicker(100 * time.Millisecond)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                if err := bufConn.Flush(); err != nil {
                    done <- err
                    return
                }
            case err := <-done:
                return
            }
        }
    }()
    
    // Запис пакетів
    for pkt := range packets {
        if err := muxer.WritePacket(pkt); err != nil {
            close(done)
            return err
        }
    }
    
    // Фіналізація
    if err := muxer.WriteTrailer(); err != nil {
        close(done)
        return err
    }
    close(done)
    
    return bufConn.Flush()
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"codec type=X is not supported"** | Спроба запису непідтримуваного кодека | Використовуйте `ValidateCodecForMP4()` перед `WriteHeader()` |
| **Файл >4 ГБ не відтворюється** | Переповнення 32-бітних offset/size | Реалізуйте підтримку `ChunkOffset64` та "large size" атомів |
| **Від'ємні duration** | Помилка "time < lasttime" | Встановіть `muxer.NegativeTsMakeZero = true` або виправте джерело таймінгів |
| **moov не знайдено плеєром** | "slow start" формат не підтримується | Додайте опцію для "fast start" (moov на початку) через буферизацію |
| **Розсинхронізація аудіо/відео** | Різні timeScale у треках | Нормалізуйте часи до спільної шкали перед записом |

---

## ⚡ Оптимізації для high-throughput запису

### 1. Динамічний розмір буфера:

```go
// NewMuxerBuffered — муксер з налаштовуваним буфером
func NewMuxerBuffered(w io.WriteSeeker, bufferSize int) *Muxer {
    return &Muxer{
        w:    w,
        bufw: bufio.NewWriterSize(w, bufferSize),
    }
}

// AutoFlush — автоматичний флеш при досягненні порогу
func (m *Muxer) WritePacketWithAutoFlush(pkt av.Packet, flushThreshold int) error {
    if err := m.WritePacket(pkt); err != nil {
        return err
    }
    
    // Флеш якщо буфер майже повний
    if m.bufw.Available() < flushThreshold {
        return m.bufw.Flush()
    }
    return nil
}
```

### 2. Паралельна обробка треків:

```go
// WritePacketsConcurrent — запис кількох треків паралельно
func (m *Muxer) WritePacketsConcurrent(videoChan, audioChan <-chan av.Packet) error {
    var wg sync.WaitGroup
    errChan := make(chan error, 2)
    
    wg.Add(2)
    
    // Відео потік
    go func() {
        defer wg.Done()
        for pkt := range videoChan {
            if pkt.Idx != 0 { continue }  // тільки відео
            if err := m.WritePacket(pkt); err != nil {
                errChan <- err
                return
            }
        }
    }()
    
    // Аудіо потік
    go func() {
        defer wg.Done()
        for pkt := range audioChan {
            if pkt.Idx != 1 { continue }  // тільки аудіо
            if err := m.WritePacket(pkt); err != nil {
                errChan <- err
                return
            }
        }
    }()
    
    wg.Wait()
    close(errChan)
    
    if err, ok := <-errChan; ok {
        return err
    }
    return nil
}
```

### 3. Моніторинг продуктивності запису:

```go
type MuxerMetrics struct {
    PacketsWritten prometheus.CounterVec
    WriteLatency   prometheus.HistogramVec
    BufferSize     prometheus.GaugeVec
    FileSize       prometheus.CounterVec
}

func (m *MuxerMetrics) RecordPacket(codec av.CodecType, size int, duration time.Duration, streamID string) {
    m.PacketsWritten.WithLabelValues(codec.String(), streamID).Inc()
    m.WriteLatency.WithLabelValues(streamID).Observe(duration.Seconds())
    m.FileSize.WithLabelValues(streamID).Add(float64(size))
}
```

---

## 📋 Чек-лист безпечного використання mp4.Muxer

```go
// ✅ 1. Валідація кодеків перед ініціалізацією
for _, s := range streams {
    if err := ValidateCodecForMP4(s); err != nil {
        return fmt.Errorf("invalid codec: %w", err)
    }
}

// ✅ 2. Обробка від'ємних duration
muxer.NegativeTsMakeZero = true  // або виправте джерело таймінгів

// ✅ 3. Перевірка розміру файлу для 64-бітних offset
if expectedSize > 4*GB {
    // Реалізуйте підтримку ChunkOffset64
}

// ✅ 4. Регулярний флеш буфера для мережевих потоків
ticker := time.NewTicker(100 * time.Millisecond)
go func() {
    for range ticker.C {
        muxer.bufw.Flush()
    }
}()

// ✅ 5. Закриття ресурсів при помилці
defer func() {
    if err != nil {
        muxer.bufw.Flush()  // спроба зберегти дані
    }
}()

// ✅ 6. Метрики для моніторингу
metrics.RecordPacket(pkt.Idx, len(pkt.Data), time.Since(start), streamID)
```

---

## 🔗 Корисні посилання

- 💻 [vdk mp4 Package](https://pkg.go.dev/github.com/deepch/vdk/format/mp4) — GoDoc documentation
- 📄 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [AVCDecoderConfigurationRecord](https://wiki.multimedia.cx/index.php/AVCDecoderConfigurationRecord) — структура для H.264
- 📄 [HVCC for H.265](https://wiki.multimedia.cx/index.php/HEVC) — структура для H.265
- 📄 [AudioSpecificConfig for AAC](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — структура для AAC
- 🧪 [Go bufio Package](https://pkg.go.dev/bufio) — буферизований I/O для ефективності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди валідуйте кодеки перед `WriteHeader()`** — уникнення помилок під час запису.
> 2. **Обробляйте файли >4 ГБ через 64-бітні offset** — інакше дані стануть недоступними.
> 3. **Виправте опечатку `VorizontalResolution` → `VerticalResolution`** — забезпечення сумісності з усіма плеєрами.
> 4. **Встановіть `NegativeTsMakeZero = true` для live-потоків** — уникнення помилок при неідеальних таймінгах.
> 5. **Флеште буфер регулярно для мережевих потоків** — зменшення затримки передачі.
> 6. **Моніторьте `WriteLatency`** — різке зростання може вказувати на повільний диск або мережу.

Потрібен приклад реалізації "fast start" режиму (moov на початку файлу) для покращення сумісності з HTTP streaming? Готовий допомогти! 🚀