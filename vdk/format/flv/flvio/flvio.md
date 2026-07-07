# 🎬 Глибокий розбір: flvio — FLV Tag Parser/Writer для RTMP/FLV

Цей файл — **реалізація парсингу та запису тегів формату FLV (Flash Video)**, що використовується у протоколі RTMP для передачі аудіо/відео даних. Він надає низькорівневі інструменти для роботи з бінарною структурою тегів, заголовків файлів та метаданих.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема flvio пакету

```
┌────────────────────────────────────────┐
│ 📦 flvio — FLV Tag Parser/Writer       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Tag struct — представлення тега     │
│  • ParseHeader/FillHeader — парсинг/запис│
│  • ReadTag/WriteTag — I/O операції     │
│  • ParseFileHeader/FillFileHeader — файл│
│                                         │
│  📊 Типи тегів:                         │
│  • TAG_AUDIO (8) — аудіо дані          │
│  • TAG_VIDEO (9) — відео дані          │
│  • TAG_SCRIPTDATA (18) — метадані      │
│                                         │
│  🎬 Формат тега (загальний):           │
│  [11-byte header][sub-header][payload][4-byte trailer]│
│                                         │
│  🔄 Потік даних:                        │
│  RTMP packet → Tag → av.Packet → HLS  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Tag — структура для представлення тегу

### Основні поля:

```go
type Tag struct {
    Type uint8  // тип тега: 8=аудіо, 9=відео, 18=метадані
    
    // Аудіо-специфічні поля (для Type=TAG_AUDIO):
    SoundFormat    uint8  // кодек: 2=MP3, 10=AAC, 7=ALaw, 8=μLaw
    SoundRate      uint8  // частота: 0=5.5kHz, 1=11kHz, 2=22kHz, 3=44kHz
    SoundSize      uint8  // розрядність: 0=8-bit, 1=16-bit
    SoundType      uint8  // канали: 0=mono, 1=stereo
    AACPacketType  uint8  // 0=sequence header, 1=raw data
    
    // Відео-специфічні поля (для Type=TAG_VIDEO):
    FrameType      uint8  // 1=keyframe, 2=interframe
    CodecID        uint8  // 7=H.264, 12=H.265
    AVCPacketType  uint8  // 0=seq header, 1=NALU, 2=EOS
    CompositionTime int32 // DTS-PTS для B-frames
    
    Data []byte  // корисне навантаження тега
}
```

### 🔍 Бітова структура заголовків:

#### Аудіо заголовок (1-2 байти):

```
Байт 0 (флаги):
  Біти 7-4: SoundFormat (кодек)
  Біти 3-2: SoundRate (частота)
  Біт 1:    SoundSize (8/16-bit)
  Біт 0:    SoundType (mono/stereo)

Якщо SoundFormat == AAC (10):
  Байт 1: AACPacketType (0=seq header, 1=raw)

Приклад: AAC, 44kHz, 16-bit, stereo, raw data
  Байт 0: (10<<4) | (3<<2) | (1<<1) | 1 = 0xAF
  Байт 1: 1 (AAC_RAW)
  Результат: [0xAF, 0x01]
```

#### Відео заголовок (5 байт для H.264/H.265):

```
Байт 0 (флаги):
  Біти 7-4: FrameType (1=key, 2=inter)
  Біти 3-0: CodecID (7=H.264, 12=H.265)

Байт 1: AVCPacketType (0=seq header, 1=NALU, 2=EOS)
Байти 2-4: CompositionTime (24-bit big-endian, у мілісекундах)

Приклад: H.264 keyframe, NALU, CompositionTime=0
  Байт 0: (1<<4) | 7 = 0x17
  Байт 1: 1 (AVC_NALU)
  Байти 2-4: 0x00 0x00 0x00
  Результат: [0x17, 0x01, 0x00, 0x00, 0x00]
```

### ✅ Ваш use-case: створення відео-тегу для HLS

```go
// CreateVideoTag — створення FLV тега для H.264 відео
func CreateVideoTag(nalu []byte, isKeyFrame bool, dts, pts time.Duration) flvio.Tag {
    // Розрахунок CompositionTime = PTS - DTS
    compositionTime := flvio.TimeToTs(pts - dts)
    
    tag := flvio.Tag{
        Type:          flvio.TAG_VIDEO,
        FrameType:     flvio.FRAME_KEY,
        CodecID:       flvio.VIDEO_H264,
        AVCPacketType: flvio.AVC_NALU,
        CompositionTime: compositionTime,
    }
    
    if !isKeyFrame {
        tag.FrameType = flvio.FRAME_INTER
    }
    
    // Для AVC_NALU: дані = [4-byte length][NALU]
    tag.Data = make([]byte, 4+len(nalu))
    pio.PutU32BE(tag.Data, uint32(len(nalu)))
    copy(tag.Data[4:], nalu)
    
    return tag
}

// Використання для запису у файл/потік:
tag := CreateVideoTag(nalu, true, 100*time.Millisecond, 100*time.Millisecond)
ts := flvio.TimeToTs(100 * time.Millisecond)

// Буфер для заголовка + даних + трейлера
buf := make([]byte, flvio.TagHeaderLength+flvio.MaxTagSubHeaderLength+len(tag.Data)+flvio.TagTrailerLength)

err := flvio.WriteTag(writer, tag, ts, buf)
if err != nil { /* handle error */ }
```

---

## 🔑 2. ParseHeader/FillHeader — парсинг/запис підзаголовків

### 🔧 Аудіо парсинг:

```go
func (self *Tag) audioParseHeader(b []byte) (n int, err error) {
    // 1. Читання байта флагов
    if len(b) < n+1 {
        err = fmt.Errorf("audiodata: parse invalid")
        return
    }
    flags := b[n]
    n++
    
    // 2. Розпакування бітових полів
    self.SoundFormat = flags >> 4              // біти 7-4
    self.SoundRate = (flags >> 2) & 0x3       // біти 3-2
    self.SoundSize = (flags >> 1) & 0x1       // біт 1
    self.SoundType = flags & 0x1              // біт 0
    
    // 3. Додатковий байт для AAC
    switch self.SoundFormat {
    case SOUND_AAC:
        if len(b) < n+1 {
            err = fmt.Errorf("audiodata: parse invalid")
            return
        }
        self.AACPacketType = b[n]
        n++
    }
    return
}
```

### 🔧 Відео парсинг:

```go
func (self *Tag) videoParseHeader(b []byte) (n int, err error) {
    // 1. Читання байта флагов
    if len(b) < n+1 {
        err = fmt.Errorf("videodata: parse invalid")
        return
    }
    flags := b[n]
    self.FrameType = flags >> 4      // біти 7-4
    self.CodecID = flags & 0xf       // біти 3-0
    n++
    
    // 2. Додаткові 4 байти для H.264/H.265
    if self.FrameType == FRAME_INTER || self.FrameType == FRAME_KEY {
        if len(b) < n+4 {
            err = fmt.Errorf("videodata: parse invalid")
            return
        }
        self.AVCPacketType = b[n]    // тип AVC пакету
        n++
        self.CompositionTime = pio.I24BE(b[n:])  // 24-bit signed
        n += 3
    }
    return
}
```

### ✅ Ваш use-case: конвертація FLV Tag → av.Packet

```go
// TagToPacket — конвертація FLV тега у універсальний av.Packet
func TagToPacket(tag flvio.Tag, timestamp time.Duration) (*av.Packet, error) {
    pkt := &av.Packet{
        Time:     timestamp,
        Duration: 0,  // буде розраховано пізніше
        Idx:      0,  // 0=відео, 1=аудіо
        Data:     tag.Data,
    }
    
    switch tag.Type {
    case flvio.TAG_VIDEO:
        pkt.Idx = 0
        pkt.IsKeyFrame = (tag.FrameType == flvio.FRAME_KEY)
        
        // Для H.264: розрахунок Duration з FPS або метаданих
        if tag.CodecID == flvio.VIDEO_H264 {
            // CompositionTime = PTS - DTS, тому:
            // PTS = timestamp + CompositionTime
            pts := timestamp + flvio.TsToTime(tag.CompositionTime)
            // Duration можна отримати з метаданих або припустити
        }
        
    case flvio.TAG_AUDIO:
        pkt.Idx = 1
        
        // Для AAC: розрахунок Duration з кількості семплів
        if tag.SoundFormat == flvio.SOUND_AAC {
            // AAC-LC завжди 1024 семпли на фрейм
            sampleRate := []int{5500, 11000, 22000, 44000}[tag.SoundRate]
            pkt.Duration = 1024 * time.Second / time.Duration(sampleRate)
        }
    }
    
    return pkt, nil
}
```

---

## 🔑 3. ReadTag/WriteTag — I/O операції з тегами

### 🔧 ReadTag — читання тега з потоку:

```go
func ReadTag(r io.Reader, b []byte) (tag Tag, ts int32, err error) {
    // 1. Читання заголовку тега (11 байт)
    if _, err = io.ReadFull(r, b[:TagHeaderLength]); err != nil {
        return
    }
    
    // 2. Парсинг заголовку
    var datalen int
    if tag, ts, datalen, err = ParseTagHeader(b); err != nil {
        return
    }
    
    // 3. Читання даних тега
    data := make([]byte, datalen)
    if _, err = io.ReadFull(r, data); err != nil {
        return
    }
    
    // 4. Парсинг підзаголовку (аудіо/відео специфічний)
    var n int
    if n, err = (&tag).ParseHeader(data); err != nil {
        return
    }
    tag.Data = data[n:]  // збереження тільки корисного навантаження
    
    // 5. Читання трейлера (4 байти, розмір попереднього тега)
    if _, err = io.ReadFull(r, b[:4]); err != nil {
        return
    }
    
    return tag, ts, nil
}
```

### 🔧 WriteTag — запис тега у потік:

```go
func WriteTag(w io.Writer, tag Tag, ts int32, b []byte) (err error) {
    data := tag.Data
    
    // 1. Заповнення підзаголовку
    n := tag.FillHeader(b[TagHeaderLength:])
    datalen := len(data) + n  // загальна довжина = підзаголовок + дані
    
    // 2. Заповнення заголовку тега
    n += FillTagHeader(b, tag.Type, datalen, ts)
    
    // 3. Запис заголовку + підзаголовку
    if _, err = w.Write(b[:n]); err != nil {
        return
    }
    
    // 4. Запис корисного навантаження
    if _, err = w.Write(data); err != nil {
        return
    }
    
    // 5. Запис трейлера (розмір тега для зворотного читання)
    n = FillTagTrailer(b, datalen)
    if _, err = w.Write(b[:n]); err != nil {
        return
    }
    
    return nil
}
```

### 🔍 Формат заголовку тега (11 байт):

```
Байти 0:    Type (1 байт) — 8=аудіо, 9=відео, 18=метадані
Байти 1-3:  DataSize (3 байти, big-endian) — довжина даних + підзаголовку
Байти 4-6:  Timestamp (3 байти, big-endian) — нижні 24 біти
Байт 7:     TimestampExtended (1 байт) — верхні 8 біт (для >16.7с)
Байти 8-10: StreamID (3 байти) — завжди 0 для FLV

Трейлер (4 байти після даних):
  PreviousTagSize — розмір цього тега (заголовок+дані), big-endian
```

### ✅ Ваш use-case: читання FLV файлу для аналізу

```go
// AnalyzeFLVFile — читання та логування метаданих з FLV файлу
func AnalyzeFLVFile(filename string) error {
    f, err := os.Open(filename)
    if err != nil {
        return fmt.Errorf("open file: %w", err)
    }
    defer f.Close()
    
    // 1. Читання заголовку файлу
    fileHeader := make([]byte, flvio.FileHeaderLength)
    if _, err := io.ReadFull(f, fileHeader); err != nil {
        return fmt.Errorf("read file header: %w", err)
    }
    
    flags, skip, err := flvio.ParseFileHeader(fileHeader)
    if err != nil {
        return err
    }
    
    log.Printf("FLV file: hasAudio=%v, hasVideo=%v, skip=%d bytes",
        flags&flvio.FILE_HAS_AUDIO != 0,
        flags&flvio.FILE_HAS_VIDEO != 0,
        skip)
    
    // Пропуск додаткових байтів заголовку
    if skip > 0 {
        if _, err := io.CopyN(io.Discard, f, int64(skip)); err != nil {
            return err
        }
    }
    
    // 2. Читання тегів
    tagBuf := make([]byte, flvio.TagHeaderLength+flvio.MaxTagSubHeaderLength)
    tagCount := 0
    videoCount := 0
    audioCount := 0
    
    for {
        tag, ts, err := flvio.ReadTag(f, tagBuf)
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read tag %d: %w", tagCount, err)
        }
        
        tagCount++
        switch tag.Type {
        case flvio.TAG_VIDEO:
            videoCount++
            if tagCount <= 5 {  // логувати перші 5 відео тегів
                log.Printf("Video tag %d: ts=%dms, key=%v, codec=%d, avcType=%d",
                    tagCount, ts, tag.FrameType == flvio.FRAME_KEY,
                    tag.CodecID, tag.AVCPacketType)
            }
        case flvio.TAG_AUDIO:
            audioCount++
            if tagCount <= 5 {
                log.Printf("Audio tag %d: ts=%dms, codec=%d, rate=%d, type=%d",
                    tagCount, ts, tag.SoundFormat, tag.SoundRate, tag.SoundType)
            }
        case flvio.TAG_SCRIPTDATA:
            log.Printf("Metadata tag: %d bytes", len(tag.Data))
            // Тут можна викликати ParseAMF0Val для парсингу метаданих
        }
    }
    
    log.Printf("Total: %d tags (%d video, %d audio)", tagCount, videoCount, audioCount)
    return nil
}
```

---

## 🔑 4. FileHeader — заголовок FLV файлу

### Формат заголовку файлу (9 байт):

```
Байти 0-2:  Signature — "FLV" (0x46 0x4C 0x56)
Байт 3:     Version — завжди 1
Байт 4:     Flags — бітова маска:
              Біт 2 (0x4): FILE_HAS_AUDIO — є аудіо теги
              Біт 0 (0x1): FILE_HAS_VIDEO — є відео теги
Байти 5-8:  DataOffset — зміщення до першого тега (зазвичай 9)
Байти 9-12: PreviousTagSize0 — завжди 0 (для сумісності)
```

### 🔧 FillFileHeader/ParseFileHeader:

```go
func FillFileHeader(b []byte, flags uint8) (n int) {
    // 'FLV', version 1
    pio.PutU32BE(b[n:], 0x464c5601)  // 4 байти: "FLV" + version
    n += 4
    
    b[n] = flags  // flags: 0x5 = audio+video, 0x4 = audio only, 0x1 = video only
    n++
    
    // DataOffset: завжди 9 для FLV v1
    pio.PutU32BE(b[n:], 9)
    n += 4
    
    // PreviousTagSize0: завжди 0
    pio.PutU32BE(b[n:], 0)
    n += 4
    
    return
}

func ParseFileHeader(b []byte) (flags uint8, skip int, err error) {
    // Перевірка signature
    flv := pio.U24BE(b[0:3])
    if flv != 0x464c56 {  // 'FLV'
        err = fmt.Errorf("flvio: file header cc3 invalid")
        return
    }
    
    flags = b[4]
    
    // Розрахунок skip: DataOffset - 9 + 4
    // DataOffset зазвичай 9, тому skip = 4 (PreviousTagSize0)
    skip = int(pio.U32BE(b[5:9])) - 9 + 4
    if skip < 0 {
        err = fmt.Errorf("flvio: file header datasize invalid")
        return
    }
    
    return
}
```

### ✅ Ваш use-case: створення нового FLV файлу для запису

```go
// CreateFLVWriter — ініціалізація запису у новий FLV файл
func CreateFLVWriter(filename string, hasAudio, hasVideo bool) (*os.File, error) {
    f, err := os.Create(filename)
    if err != nil {
        return nil, err
    }
    
    // Розрахунок flags
    var flags uint8
    if hasAudio {
        flags |= flvio.FILE_HAS_AUDIO
    }
    if hasVideo {
        flags |= flvio.FILE_HAS_VIDEO
    }
    
    // Запис заголовку файлу
    header := make([]byte, flvio.FileHeaderLength)
    flvio.FillFileHeader(header, flags)
    
    if _, err := f.Write(header); err != nil {
        f.Close()
        return nil, err
    }
    
    return f, nil
}

// Використання:
f, err := CreateFLVWriter("output.flv", true, true)
if err != nil { /* handle error */ }
defer f.Close()

// Тепер можна записувати теги через flvio.WriteTag(f, tag, ts, buf)
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// flv_to_hls_converter.go — конвертація FLV/RTMP потоку у HLS сегменти
type FLVToHLSConverter struct {
    channelID    string
    inputFile    io.Reader
    hlsWriter    *HLSWriter
    videoCodec   *h264parser.CodecData
    audioCodec   *aacparser.CodecData
    metrics      *ConversionMetrics
}

func NewFLVToHLSConverter(channelID string, input io.Reader) *FLVToHLSConverter {
    return &FLVToHLSConverter{
        channelID:  channelID,
        inputFile:  input,
        hlsWriter:  NewHLSWriter(channelID),
        metrics:    NewConversionMetrics(channelID),
    }
}

// Convert — основний цикл конвертації
func (c *FLVToHLSConverter) Convert(ctx context.Context) error {
    // 1. Читання заголовку файлу
    fileHeader := make([]byte, flvio.FileHeaderLength)
    if _, err := io.ReadFull(c.inputFile, fileHeader); err != nil {
        return fmt.Errorf("read file header: %w", err)
    }
    
    flags, skip, err := flvio.ParseFileHeader(fileHeader)
    if err != nil {
        return err
    }
    
    if skip > 0 {
        if _, err := io.CopyN(io.Discard, c.inputFile, int64(skip)); err != nil {
            return err
        }
    }
    
    // 2. Буфер для читання тегів
    tagBuf := make([]byte, flvio.TagHeaderLength+flvio.MaxTagSubHeaderLength)
    
    // 3. Стан для сегментації
    var currentSegment *HLSSegment
    var lastVideoTS, lastAudioTS time.Duration
    
    // 4. Основний цикл
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        // Читання тега
        tag, ts, err := flvio.ReadTag(c.inputFile, tagBuf)
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read tag: %w", err)
        }
        
        timestamp := flvio.TsToTime(ts)
        
        // Обробка за типом тега
        switch tag.Type {
        case flvio.TAG_VIDEO:
            if err := c.processVideoTag(tag, timestamp, &currentSegment, &lastVideoTS); err != nil {
                return err
            }
            
        case flvio.TAG_AUDIO:
            if err := c.processAudioTag(tag, timestamp, &currentSegment, &lastAudioTS); err != nil {
                return err
            }
            
        case flvio.TAG_SCRIPTDATA:
            if err := c.processMetadataTag(tag); err != nil {
                log.Printf("warning: metadata parse error: %v", err)
            }
        }
        
        // Перевірка чи потрібно завершити поточний сегмент
        if currentSegment != nil && currentSegment.Duration() >= 10*time.Second {
            if err := c.finalizeSegment(currentSegment); err != nil {
                return err
            }
            currentSegment = nil
        }
    }
    
    // Фіналізація останнього сегменту
    if currentSegment != nil {
        if err := c.finalizeSegment(currentSegment); err != nil {
            return err
        }
    }
    
    return nil
}

// processVideoTag — обробка відео тега
func (c *FLVToHLSConverter) processVideoTag(
    tag flvio.Tag, 
    timestamp time.Duration,
    segment **HLSSegment,
    lastTS *time.Duration,
) error {
    // 1. Оновлення метрик
    c.metrics.VideoTagsProcessed.Inc()
    
    // 2. Обробка sequence header (SPS/PPS)
    if tag.AVCPacketType == flvio.AVC_SEQHDR {
        codecData, err := h264parser.NewCodecDataFromAVCDecoderConfRecord(tag.Data)
        if err != nil {
            return fmt.Errorf("parse SPS/PPS: %w", err)
        }
        c.videoCodec = &codecData
        c.metrics.CodecUpdated.Inc()
        return nil
    }
    
    // 3. Пропуск якщо немає кодека
    if c.videoCodec == nil {
        return nil
    }
    
    // 4. Конвертація у av.Packet
    pkt, err := TagToPacket(tag, timestamp)
    if err != nil {
        return err
    }
    
    // 5. Додавання у поточний сегмент
    if *segment == nil {
        *segment = c.startNewSegment(timestamp)
    }
    (*segment).AddVideoPacket(*pkt)
    
    *lastTS = timestamp
    return nil
}

// processAudioTag — аналогічно для аудіо
func (c *FLVToHLSConverter) processAudioTag(
    tag flvio.Tag,
    timestamp time.Duration,
    segment **HLSSegment,
    lastTS *time.Duration,
) error {
    // Обробка AAC sequence header
    if tag.SoundFormat == flvio.SOUND_AAC && tag.AACPacketType == flvio.AAC_SEQHDR {
        codecData, err := aacparser.NewCodecDataFromMPEG4AudioConfigBytes(tag.Data)
        if err != nil {
            return fmt.Errorf("parse AAC config: %w", err)
        }
        c.audioCodec = &codecData
        return nil
    }
    
    // Конвертація та додавання у сегмент...
    // (аналогічно до video)
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"flvio: file header cc3 invalid"** | Файл не починається з "FLV" | Переконайтеся, що вхідні дані дійсно у форматі FLV; перевірте чи не змішані з іншими форматами |
| **"audiodata: parse invalid"** | Недостатньо даних для парсингу заголовку | Перевірте цілісність потоку; можливе обрізання пакету при мережевих помилках |
| **CompositionTime неправильний** | Неправильне перетворення 24-bit signed | Використовуйте `pio.I24BE()` для читання, не `U24BE`; пам'ятайте про знакове перетворення |
| **Timestamp > 16.7с не працює** | Верхні 8 біт часу не обробляються | Переконайтеся, що `ts` у `WriteTag` — це 32-bit значення, а не тільки нижні 24 біти |
| **Трейлер не збігається** | `PreviousTagSize` не співпадає з реальним розміром | Переконайтеся, що `datalen` у `FillTagTrailer` включає заголовок + дані, а не тільки дані |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування буферів для тегів:

```go
// TagBufferPool — пул буферів для уникнення аллокацій
var TagBufferPool = sync.Pool{
    New: func() interface{} {
        // Максимальний розмір: заголовок(11) + підзаголовок(16) + дані(варіюється) + трейлер(4)
        buf := make([]byte, flvio.TagHeaderLength+flvio.MaxTagSubHeaderLength+65536+flvio.TagTrailerLength)
        return &buf
    },
}

func GetTagBuffer() *[]byte {
    return TagBufferPool.Get().(*[]byte)
}

func PutTagBuffer(buf *[]byte) {
    // Очищення чутливих даних перед поверненням у пул
    for i := range *buf {
        (*buf)[i] = 0
    }
    TagBufferPool.Put(buf)
}

// Використання:
buf := GetTagBuffer()
defer PutTagBuffer(buf)

err := flvio.WriteTag(writer, tag, ts, *buf)
```

### 2. Пакетне читання для зменшення системних викликів:

```go
// BatchReadTags — читання кількох тегів за один виклик
func BatchReadTags(r io.Reader, count int, buf []byte) ([]flvio.Tag, []int32, error) {
    tags := make([]flvio.Tag, 0, count)
    timestamps := make([]int32, 0, count)
    
    for i := 0; i < count; i++ {
        tag, ts, err := flvio.ReadTag(r, buf)
        if err == io.EOF {
            break
        }
        if err != nil {
            return tags, timestamps, err
        }
        tags = append(tags, tag)
        timestamps = append(timestamps, ts)
    }
    return tags, timestamps, nil
}
```

### 3. Моніторинг продуктивності парсингу:

```go
type FLVMetrics struct {
    TagsProcessed   prometheus.CounterVec
    ParseLatency    prometheus.HistogramVec
    BytesProcessed  prometheus.CounterVec
    CodecUpdates    prometheus.CounterVec
}

func (m *FLVMetrics) RecordTag(tagType uint8, bytes int, duration time.Duration, channelID string) {
    m.TagsProcessed.WithLabelValues(fmt.Sprintf("%d", tagType), channelID).Inc()
    m.ParseLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    m.BytesProcessed.WithLabelValues(channelID).Add(float64(bytes))
}
```

---

## 📋 Чек-лист інтеграції flvio

```go
// ✅ 1. Перевірка заголовку файлу перед обробкою
fileHeader := make([]byte, flvio.FileHeaderLength)
if _, err := io.ReadFull(r, fileHeader); err != nil { /* handle */ }
flags, skip, err := flvio.ParseFileHeader(fileHeader)

// ✅ 2. Пропуск додаткових байтів заголовку
if skip > 0 {
    io.CopyN(io.Discard, r, int64(skip))
}

// ✅ 3. Виділення достатнього буфера для тегів
buf := make([]byte, flvio.TagHeaderLength+flvio.MaxTagSubHeaderLength)

// ✅ 4. Обробка помилок читання
tag, ts, err := flvio.ReadTag(r, buf)
if err == io.EOF {
    break  // нормальне завершення
}
if err != nil {
    return fmt.Errorf("read tag: %w", err)
}

// ✅ 5. Конвертація timestamp
timestamp := flvio.TsToTime(ts)

// ✅ 6. Парсинг підзаголовку перед доступом до даних
var n int
if n, err = tag.ParseHeader(tag.Data); err != nil { /* handle */ }
payload := tag.Data[n:]  // тільки корисне навантаження

// ✅ 7. Закриття ресурсів
defer func() {
    if r, ok := r.(io.Closer); ok {
        r.Close()
    }
}()
```

---

## 🔗 Корисні посилання

- 💻 [vdk flvio Package](https://pkg.go.dev/github.com/deepch/vdk/format/flvio) — GoDoc documentation
- 📄 [FLV File Format Specification (Adobe)](https://download.macromedia.com/f4v/video_file_format_spec_v10_1.pdf) — офіційна специфікація
- 📄 [RTMP Specification](https://www.adobe.com/devnet/rtmp.html) — використання тегів у RTMP
- 📄 [H.264 in FLV](https://github.com/mifi/lossless-cut/issues/113) — особливості кодування H.264 у FLV
- 🧪 [Go io.ReadFull Documentation](https://pkg.go.dev/io#ReadFull) — надійне читання фіксованої кількості байт

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **медіа потоками у реальному часі**:
> 1. **Завжди перевіряйте заголовок файлу** перед обробкою — це уникне помилок парсингу не-FLV даних.
> 2. **Використовуйте `io.ReadFull` замість `io.Read`** — гарантує читання точної кількості байт, що критично для бінарних форматів.
> 3. **Кешуйте буфери через `sync.Pool`** — це значно зменшує навантаження на GC при обробці тисяч тегів на секунду.
> 4. **Обробляйте `CompositionTime` коректно** — це 24-бітове знакове значення; неправильне перетворення зламає синхронізацію аудіо/відео.
> 5. **Моніторьте `TagsProcessed` та `ParseLatency`** — різке зростання латентності може вказувати на перевантаження або проблеми з джерелом.

Потрібен приклад інтеграції `FLVToHLSConverter` з вашим `pubsub.Queue` для розподілу вже конвертованих пакетів між підписниками (транскодер, WebSocket, архів)? Готовий допомогти! 🚀