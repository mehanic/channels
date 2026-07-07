# 🎬 Глибокий розбір: flv — FLV Muxer/Demuxer для vdk

Цей файл — **високорівнева реалізація muxer/demuxer для формату FLV (Flash Video)**, що використовується у протоколі RTMP. Він надає інтеграцію з бібліотекою `vdk` через інтерфейси `av.Muxer`/`av.Demuxer`, автоматичне визначення кодеків, та конвертацію між `av.Packet` та `flvio.Tag`.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема flv пакету

```
┌────────────────────────────────────────┐
│ 📦 flv — High-Level FLV Muxer/Demuxer │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Prober — авто-детект кодеків       │
│  • Muxer — запис FLV потоків          │
│  • Demuxer — читання FLV потоків      │
│  • CodecData↔Tag конвертери           │
│  • avutil.RegisterHandler інтеграція  │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet ↔ PacketToTag ↔ flvio.Tag │
│  flvio.Tag ↔ TagToPacket ↔ av.Packet │
│                                         │
│  📊 Підтримувані кодеки:                │
│  • Відео: H.264, H.265                 │
│  • Аудіо: AAC, Speex, NellyMoser       │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Prober — авто-детект кодеків при читанні

### Призначення:
`Prober` аналізує перші теги FLV потоку для автоматичного визначення кодеків та їх параметрів (SPS/PPS для H.264, AudioSpecificConfig для AAC тощо).

### Структура:

```go
type Prober struct {
    HasAudio, HasVideo             bool  // очікувані потоки з заголовку файлу
    GotAudio, GotVideo             bool  // чи вже знайдені під час проби
    VideoStreamIdx, AudioStreamIdx int   // індекси потоків у Streams[]
    PushedCount                    int   // кількість оброблених тегів
    Streams                        []av.CodecData  // виявлені кодеки
    CachedPkts                     []av.Packet     // закешовані пакети для подальшого читання
}
```

### 🔧 Метод `PushTag()` — основна логіка проби:

```go
func (self *Prober) PushTag(tag flvio.Tag, timestamp int32) (err error) {
    self.PushedCount++
    
    // Обмеження кількості тегів для проби
    if self.PushedCount > MaxProbePacketCount {  // за замовчуванням 20
        err = fmt.Errorf("flv: max probe packet count reached")
        return
    }
    
    switch tag.Type {
    case flvio.TAG_VIDEO:
        switch tag.AVCPacketType {
        case flvio.AVC_SEQHDR:  // Sequence Header з параметрами кодека
            if !self.GotVideo {
                // Спроба парсингу як H.264
                var stream av.CodecData
                if stream, err = h264parser.NewCodecDataFromAVCDecoderConfRecord(tag.Data); err != nil {
                    // Якщо не H.264 — спроба H.265
                    if stream, err = h265parser.NewCodecDataFromAVCDecoderConfRecord(tag.Data); err != nil {
                        err = fmt.Errorf("flv: h264 seqhdr invalid")
                        return
                    }
                }
                self.VideoStreamIdx = len(self.Streams)
                self.Streams = append(self.Streams, stream)
                self.GotVideo = true
            }
            
        case flvio.AVC_NALU:  // Звичайні відео дані
            self.CacheTag(tag, timestamp)  // кешування для подальшого читання
        }
        
    case flvio.TAG_AUDIO:
        switch tag.SoundFormat {
        case flvio.SOUND_AAC:
            switch tag.AACPacketType {
            case flvio.AAC_SEQHDR:  // AudioSpecificConfig
                if !self.GotAudio {
                    stream, err := aacparser.NewCodecDataFromMPEG4AudioConfigBytes(tag.Data)
                    // ... збереження у Streams ...
                }
            case flvio.AAC_RAW:
                self.CacheTag(tag, timestamp)
            }
        // ... інші аудіо кодеки ...
        }
    }
    return nil
}
```

### 🔍 Коли проба вважається завершеною?

```go
func (self *Prober) Probed() (ok bool) {
    if self.HasAudio || self.HasVideo {
        // Якщо заголовок файлу вказує на наявність потоків — 
        // чекаємо поки всі вони будуть знайдені
        if self.HasAudio == self.GotAudio && self.HasVideo == self.GotVideo {
            return true
        }
    } else {
        // Якщо заголовок не вказує — чекаємо макс. кількість тегів
        if self.PushedCount == MaxProbePacketCount {
            return true
        }
    }
    return false
}
```

### ✅ Ваш use-case: обробка потоку з невідомими кодеками

```go
// AutoDetectCodecs — визначення кодеків з початку потоку
func AutoDetectCodecs(r io.Reader) ([]av.CodecData, error) {
    demuxer := flv.NewDemuxer(r)
    
    // Streams() автоматично запустить пробу
    streams, err := demuxer.Streams()
    if err != nil {
        return nil, fmt.Errorf("probe failed: %w", err)
    }
    
    log.Printf("Detected %d streams:", len(streams))
    for i, s := range streams {
        log.Printf("  [%d] Type: %v", i, s.Type())
    }
    
    return streams, nil
}
```

---

## 🔑 2. Muxer — запис FLV потоків

### Структура:

```go
type Muxer struct {
    bufw    writeFlusher  // буферизований writer (для ефективності)
    b       []byte        // буфер для заголовків (256 байт)
    streams []av.CodecData // метадані потоків
}
```

### 🔧 Метод `WriteHeader()` — ініціалізація файлу:

```go
func (self *Muxer) WriteHeader(streams []av.CodecData) (err error) {
    // 1. Розрахунок flags для заголовку файлу
    var flags uint8
    for _, stream := range streams {
        if stream.Type().IsVideo() {
            flags |= flvio.FILE_HAS_VIDEO
        } else if stream.Type().IsAudio() {
            flags |= flvio.FILE_HAS_AUDIO
        }
    }
    
    // 2. Запис заголовку файлу
    n := flvio.FillFileHeader(self.b, flags)
    if _, err = self.bufw.Write(self.b[:n]); err != nil {
        return
    }
    
    // 3. Запис sequence headers для кожного потоку
    for _, stream := range streams {
        var tag flvio.Tag
        var ok bool
        if tag, ok, err = CodecDataToTag(stream); err != nil {
            return
        }
        if ok {
            // Запис тега з timestamp=0 (sequence headers завжди на початку)
            if err = flvio.WriteTag(self.bufw, tag, 0, self.b); err != nil {
                return
            }
        }
    }
    
    self.streams = streams
    return nil
}
```

### 🔧 Конвертер `CodecDataToTag()` — створення sequence header:

```go
func CodecDataToTag(stream av.CodecData) (_tag flvio.Tag, ok bool, err error) {
    switch stream.Type() {
    case av.H264:
        h264 := stream.(h264parser.CodecData)
        tag := flvio.Tag{
            Type:          flvio.TAG_VIDEO,
            AVCPacketType: flvio.AVC_SEQHDR,  // sequence header
            CodecID:       flvio.VIDEO_H264,
            Data:          h264.AVCDecoderConfRecordBytes(),  // SPS+PPS
            FrameType:     flvio.FRAME_KEY,
        }
        ok = true
        _tag = tag
        
    case av.AAC:
        aac := stream.(aacparser.CodecData)
        tag := flvio.Tag{
            Type:          flvio.TAG_AUDIO,
            SoundFormat:   flvio.SOUND_AAC,
            SoundRate:     flvio.SOUND_44Khz,  // за замовчуванням
            AACPacketType: flvio.AAC_SEQHDR,   // sequence header
            Data:          aac.MPEG4AudioConfigBytes(),  // AudioSpecificConfig
        }
        // Налаштування SoundSize/SoundType з метаданих
        switch aac.SampleFormat().BytesPerSample() {
        case 1: tag.SoundSize = flvio.SOUND_8BIT
        default: tag.SoundSize = flvio.SOUND_16BIT
        }
        switch aac.ChannelLayout().Count() {
        case 1: tag.SoundType = flvio.SOUND_MONO
        case 2: tag.SoundType = flvio.SOUND_STEREO
        }
        ok = true
        _tag = tag
    }
    return
}
```

### 🔧 Метод `WritePacket()` — запис даних:

```go
func (self *Muxer) WritePacket(pkt av.Packet) (err error) {
    // 1. Отримання метаданих потоку за індексом
    stream := self.streams[pkt.Idx]
    
    // 2. Конвертація av.Packet → flvio.Tag
    tag, timestamp := PacketToTag(pkt, stream)
    
    // 3. Запис тега у потік
    if err = flvio.WriteTag(self.bufw, tag, timestamp, self.b); err != nil {
        return
    }
    return nil
}
```

### 🔧 Конвертер `PacketToTag()` — av.Packet → flvio.Tag:

```go
func PacketToTag(pkt av.Packet, stream av.CodecData) (tag flvio.Tag, timestamp int32) {
    switch stream.Type() {
    case av.H264, av.H265:
        tag = flvio.Tag{
            Type:            flvio.TAG_VIDEO,
            AVCPacketType:   flvio.AVC_NALU,  // звичайні дані (не sequence header)
            CodecID:         flvio.VIDEO_H264,  // або H265
            Data:            pkt.Data,
            CompositionTime: flvio.TimeToTs(pkt.CompositionTime),  // DTS→PTS
        }
        if pkt.IsKeyFrame {
            tag.FrameType = flvio.FRAME_KEY
        } else {
            tag.FrameType = flvio.FRAME_INTER
        }
        
    case av.AAC:
        tag = flvio.Tag{
            Type:          flvio.TAG_AUDIO,
            SoundFormat:   flvio.SOUND_AAC,
            SoundRate:     flvio.SOUND_44Khz,  // за замовчуванням
            AACPacketType: flvio.AAC_RAW,      // звичайні дані
            Data:          pkt.Data,
        }
        // Налаштування SoundSize/SoundType з метаданих
        astream := stream.(av.AudioCodecData)
        switch astream.SampleFormat().BytesPerSample() {
        case 1: tag.SoundSize = flvio.SOUND_8BIT
        default: tag.SoundSize = flvio.SOUND_16BIT
        }
        switch astream.ChannelLayout().Count() {
        case 1: tag.SoundType = flvio.SOUND_MONO
        case 2: tag.SoundType = flvio.SOUND_STEREO
        }
    }
    
    // Конвертація часу: time.Duration → milliseconds int32
    timestamp = flvio.TimeToTs(pkt.Time)
    return
}
```

### ✅ Ваш use-case: запис відео/аудіо у FLV файл

```go
// WriteFLVFile — запис пакетів у новий FLV файл
func WriteFLVFile(filename string, packets []av.Packet, videoCodec, audioCodec av.CodecData) error {
    f, err := os.Create(filename)
    if err != nil {
        return fmt.Errorf("create file: %w", err)
    }
    defer f.Close()
    
    // Створення muxer
    muxer := flv.NewMuxer(f)
    
    // Підготовка метаданих
    streams := []av.CodecData{videoCodec}
    if audioCodec != nil {
        streams = append(streams, audioCodec)
    }
    
    // Запис заголовка
    if err := muxer.WriteHeader(streams); err != nil {
        return fmt.Errorf("write header: %w", err)
    }
    
    // Запис кожного пакету
    for _, pkt := range packets {
        if err := muxer.WritePacket(pkt); err != nil {
            return fmt.Errorf("write packet: %w", err)
        }
    }
    
    // Фіналізація (flush буфера)
    return muxer.WriteTrailer()
}
```

---

## 🔑 3. Demuxer — читання FLV потоків

### Структура:

```go
type Demuxer struct {
    prober *Prober           // для авто-детекту кодеків
    bufr   *bufio.Reader    // буферизований reader
    b      []byte           // буфер для заголовків (256 байт)
    stage  int              // стан ініціалізації: 0=не готовий, 1=заголовок прочитано, 2=готовий
}
```

### 🔧 Метод `prepare()` — ініціалізація перед читанням:

```go
func (self *Demuxer) prepare() (err error) {
    for self.stage < 2 {
        switch self.stage {
        case 0:  // Читання заголовку файлу
            if _, err = io.ReadFull(self.bufr, self.b[:flvio.FileHeaderLength]); err != nil {
                return
            }
            var flags uint8
            var skip int
            if flags, skip, err = flvio.ParseFileHeader(self.b); err != nil {
                return
            }
            // Пропуск додаткових байтів заголовку
            if _, err = self.bufr.Discard(skip); err != nil {
                return
            }
            // Збереження очікуваних потоків
            if flags&flvio.FILE_HAS_AUDIO != 0 {
                self.prober.HasAudio = true
            }
            if flags&flvio.FILE_HAS_VIDEO != 0 {
                self.prober.HasVideo = true
            }
            self.stage++
            
        case 1:  // Проба тегів для визначення кодеків
            for !self.prober.Probed() {
                var tag flvio.Tag
                var timestamp int32
                if tag, timestamp, err = flvio.ReadTag(self.bufr, self.b); err != nil {
                    return
                }
                if err = self.prober.PushTag(tag, timestamp); err != nil {
                    return
                }
            }
            self.stage++  // проба завершена, готовий до читання
        }
    }
    return nil
}
```

### 🔧 Метод `ReadPacket()` — читання пакетів:

```go
func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
    // 1. Ініціалізація якщо потрібно
    if err = self.prepare(); err != nil {
        return
    }
    
    // 2. Спочатку віддати закешовані пакети з проби
    if !self.prober.Empty() {
        pkt = self.prober.PopPacket()
        return
    }
    
    // 3. Читання нових тегів
    for {
        var tag flvio.Tag
        var timestamp int32
        if tag, timestamp, err = flvio.ReadTag(self.bufr, self.b); err != nil {
            return
        }
        
        // 4. Конвертація Tag → Packet
        var ok bool
        if pkt, ok = self.prober.TagToPacket(tag, timestamp); ok {
            return  // успішна конвертація
        }
        // Якщо не ok — тег не містить корисних даних (напр. sequence header), читаємо далі
    }
}
```

### 🔧 Метод `TagToPacket()` — конвертація flvio.Tag → av.Packet:

```go
func (self *Prober) TagToPacket(tag flvio.Tag, timestamp int32) (pkt av.Packet, ok bool) {
    switch tag.Type {
    case flvio.TAG_VIDEO:
        pkt.Idx = int8(self.VideoStreamIdx)
        switch tag.AVCPacketType {
        case flvio.AVC_NALU:  // тільки звичайні дані, не sequence header
            ok = true
            pkt.Data = tag.Data
            pkt.CompositionTime = flvio.TsToTime(tag.CompositionTime)
            pkt.IsKeyFrame = tag.FrameType == flvio.FRAME_KEY
        }
        
    case flvio.TAG_AUDIO:
        pkt.Idx = int8(self.AudioStreamIdx)
        switch tag.SoundFormat {
        case flvio.SOUND_AAC:
            switch tag.AACPacketType {
            case flvio.AAC_RAW:  // тільки raw дані, не sequence header
                ok = true
                pkt.Data = tag.Data
            }
        // ... інші аудіо кодеки ...
        }
    }
    
    // Конвертація часу: milliseconds int32 → time.Duration
    pkt.Time = flvio.TsToTime(timestamp)
    return
}
```

### ✅ Ваш use-case: читання та аналіз FLV файлу

```go
// AnalyzeFLVFile — читання та логування метаданих з FLV файлу
func AnalyzeFLVFile(filename string) error {
    f, err := os.Open(filename)
    if err != nil {
        return fmt.Errorf("open file: %w", err)
    }
    defer f.Close()
    
    demuxer := flv.NewDemuxer(f)
    
    // Отримання метаданих потоків (автоматично запустить пробу)
    streams, err := demuxer.Streams()
    if err != nil {
        return fmt.Errorf("get streams: %w", err)
    }
    
    log.Printf("File: %s", filename)
    log.Printf("Found %d streams:", len(streams))
    for i, s := range streams {
        log.Printf("  [%d] Type: %v", i, s.Type())
        switch v := s.(type) {
        case h264parser.CodecData:
            log.Printf("      Resolution: %dx%d, FPS: %d", v.Width(), v.Height(), v.FPS())
        case aacparser.CodecData:
            log.Printf("      SampleRate: %d Hz, Channels: %d", 
                v.SampleRate(), v.ChannelLayout().Count())
        }
    }
    
    // Читання перших 10 пакетів для дебагу
    for i := 0; i < 10; i++ {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read packet %d: %w", i, err)
        }
        
        log.Printf("Packet %d: idx=%d, time=%v, key=%v, size=%d", 
            i, pkt.Idx, pkt.Time, pkt.IsKeyFrame, len(pkt.Data))
    }
    
    return nil
}
```

---

## 🔑 4. NewMetadataByStreams — генерація метаданих для FLV

### Призначення:
Створення `onMetaData` об'єкта для запису у скрипт-тег (TAG_SCRIPTDATA), що містить інформацію про кодек, роздільну здатність, тривалість тощо.

### Реалізація:

```go
func NewMetadataByStreams(streams []av.CodecData) (metadata flvio.AMFMap, err error) {
    metadata = flvio.AMFMap{}
    
    for _, _stream := range streams {
        typ := _stream.Type()
        switch {
        case typ.IsVideo():
            stream := _stream.(av.VideoCodecData)
            switch typ {
            case av.H264:
                metadata["videocodecid"] = flvio.VIDEO_H264
            case av.H265:
                metadata["videocodecid"] = flvio.VIDEO_H265
            default:
                err = fmt.Errorf("flv: metadata: unsupported video codecType=%v", stream.Type())
                return
            }
            metadata["width"] = stream.Width()
            metadata["height"] = stream.Height()
            metadata["displayWidth"] = stream.Width()
            metadata["displayHeight"] = stream.Height()
            
        case typ.IsAudio():
            stream := _stream.(av.AudioCodecData)
            switch typ {
            case av.AAC:
                metadata["audiocodecid"] = flvio.SOUND_AAC
            case av.SPEEX:
                metadata["audiocodecid"] = flvio.SOUND_SPEEX
            default:
                err = fmt.Errorf("flv: metadata: unsupported audio codecType=%v", stream.Type())
                return
            }
            metadata["audiosamplerate"] = stream.SampleRate()
        }
    }
    return
}
```

### ✅ Ваш use-case: створення метаданих для RTMP потоку

```go
// CreateRTMPMetadata — генерація onMetaData для відправки у RTMP
func CreateRTMPMetadata(streams []av.CodecData, duration time.Duration) ([]byte, error) {
    metadata, err := flv.NewMetadataByStreams(streams)
    if err != nil {
        return nil, err
    }
    
    // Додавання додаткових полів
    metadata["duration"] = duration.Seconds()
    metadata["framerate"] = 30.0  // можна отримати з метаданих кодека
    metadata["creationdate"] = time.Now().Format(time.RFC3339)
    
    // Серіалізація у AMF0 формат
    size := flvio.LenAMF0Val(metadata)
    buf := make([]byte, size)
    n := flvio.FillAMF0Val(buf, metadata)
    
    return buf[:n], nil
}
```

---

## 🔑 5. Handler — реєстрація у avutil

### Функція `Handler()`:

```go
func Handler(h *avutil.RegisterHandler) {
    // 1. Probe-функція для авто-детекту формату
    h.Probe = func(b []byte) bool {
        return b[0] == 'F' && b[1] == 'L' && b[2] == 'V'  // перевірка "FLV" signature
    }
    
    // 2. Розширення файлу
    h.Ext = ".flv"
    
    // 3. Factory-функції
    h.ReaderDemuxer = func(r io.Reader) av.Demuxer {
        return NewDemuxer(r)
    }
    h.WriterMuxer = func(w io.Writer) av.Muxer {
        return NewMuxer(w)
    }
    
    // 4. Підтримувані типи кодеків
    h.CodecTypes = CodecTypes  // []av.CodecType{av.H264, av.AAC, av.SPEEX, av.H265}
}
```

### ✅ Ваш use-case: реєстрація handler у вашому проекті

```go
// init.go — реєстрація всіх підтримуваних форматів
func init() {
    // Реєстрація FLV handler
    flv.Handler(avutil.DefaultHandlers)
    
    // Реєстрація інших форматів...
    // ts.Handler(avutil.DefaultHandlers)
    // aac.Handler(avutil.DefaultHandlers)
    // тощо
}

// Використання: авто-відкриття файлу за розширенням
func OpenMediaFile(filename string) (av.DemuxCloser, error) {
    // avutil.Open автоматично визначить формат за розширенням або Probe()
    return avutil.Open(filename)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// flv_to_hls_transcoder.go — транскодування FLV/RTMP у HLS
type FLVToHLSTranscoder struct {
    channelID    string
    input        io.Reader
    outputDir    string
    demuxer      *flv.Demuxer
    hlsWriter    *HLSWriter
    metrics      *TranscodeMetrics
}

func NewFLVToHLSTranscoder(channelID string, input io.Reader, outputDir string) (*FLVToHLSTranscoder, error) {
    return &FLVToHLSTranscoder{
        channelID:  channelID,
        input:      input,
        outputDir:  outputDir,
        demuxer:    flv.NewDemuxer(input),
        hlsWriter:  NewHLSWriter(channelID, outputDir),
        metrics:    NewTranscodeMetrics(channelID),
    }, nil
}

// Transcode — основний цикл транскодування
func (t *FLVToHLSTranscoder) Transcode(ctx context.Context) error {
    // 1. Отримання метаданих потоків (автоматична проба)
    streams, err := t.demuxer.Streams()
    if err != nil {
        return fmt.Errorf("probe streams: %w", err)
    }
    
    // 2. Ініціалізація HLS writer з метаданими
    if err := t.hlsWriter.WriteHeader(streams); err != nil {
        return fmt.Errorf("init HLS: %w", err)
    }
    
    // 3. Стан для сегментації
    var currentSegment *HLSSegment
    var lastKeyFrameTime time.Duration
    
    // 4. Основний цикл читання/запису
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        // Читання пакету
        pkt, err := t.demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Оновлення метрик
        t.metrics.PacketsProcessed.Inc()
        
        // Детекція ключових кадрів для сегментації
        if pkt.IsKeyFrame && pkt.Idx == 0 {  // відео ключовий кадр
            if currentSegment != nil && pkt.Time-lastKeyFrameTime >= 10*time.Second {
                // Завершити поточний сегмент
                if err := t.hlsWriter.WriteSegment(currentSegment); err != nil {
                    return err
                }
                currentSegment = nil
            }
            if currentSegment == nil {
                currentSegment = t.hlsWriter.StartNewSegment(pkt.Time)
                lastKeyFrameTime = pkt.Time
            }
        }
        
        // Додавання пакету у поточний сегмент
        if currentSegment != nil {
            if err := currentSegment.AddPacket(pkt); err != nil {
                return err
            }
        }
    }
    
    // Фіналізація останнього сегменту
    if currentSegment != nil {
        if err := t.hlsWriter.WriteSegment(currentSegment); err != nil {
            return err
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"flv: max probe packet count reached"** | Проба не знайшла sequence headers за 20 тегів | Збільште `MaxProbePacketCount`; перевірте чи потік дійсно містить sequence headers на початку |
| **"flv: h264 seqhdr invalid"** | Дані не парсяться як H.264 SPS/PPS | Переконайтеся, що `tag.Data` містить AVCDecoderConfRecord, а не сирий NALU; перевірте цілісність даних |
| **"flv: aac seqhdr invalid"** | Дані не парсяться як AudioSpecificConfig | Переконайтеся, що `tag.Data` містить MPEG4AudioConfigBytes; для AAC-LC це зазвичай 2-5 байт |
| **Неправильний timestamp** | Конвертація `TsToTime`/`TimeToTs` дає зсув | Переконайтеся, що використовуєте однакові одиниці (мілісекунди) по всьому пайплайну |
| **Буфер не флешиться** | Дані не записуються у файл/мережу | Викликайте `WriteTrailer()` або `Flush()` після запису останнього пакету |

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

// Використання у Muxer.WritePacket:
buf := GetTagBuffer()
defer PutTagBuffer(buf)
err := flvio.WriteTag(self.bufw, tag, timestamp, *buf)
```

### 2. Пакетне читання для зменшення системних викликів:

```go
// BatchReadPackets — читання кількох пакетів за один виклик
func (d *Demuxer) BatchReadPackets(count int) ([]av.Packet, error) {
    packets := make([]av.Packet, 0, count)
    
    for i := 0; i < count; i++ {
        pkt, err := d.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return packets, err
        }
        packets = append(packets, pkt)
    }
    return packets, nil
}
```

### 3. Моніторинг продуктивності транскодування:

```go
type TranscodeMetrics struct {
    PacketsProcessed prometheus.CounterVec
    ProcessLatency   prometheus.HistogramVec
    SegmentDuration  prometheus.HistogramVec
    KeyFrameInterval prometheus.HistogramVec
}

func (m *TranscodeMetrics) RecordPacket(duration time.Duration, channelID string) {
    m.ProcessLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    m.PacketsProcessed.WithLabelValues(channelID).Inc()
}

func (m *TranscodeMetrics) RecordSegment(duration time.Duration, channelID string) {
    m.SegmentDuration.WithLabelValues(channelID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції flv пакету

```go
// ✅ 1. Реєстрація handler у init()
func init() {
    flv.Handler(avutil.DefaultHandlers)
}

// ✅ 2. Створення muxer з буферизацією
muxer := flv.NewMuxer(writer)  // автоматично використовує bufio.Writer

// ✅ 3. Запис заголовка з правильними метаданими
streams := []av.CodecData{videoCodec, audioCodec}
if err := muxer.WriteHeader(streams); err != nil { /* handle */ }

// ✅ 4. Конвертація пакетів перед записом
tag, timestamp := flv.PacketToTag(pkt, stream)
// або просто: muxer.WritePacket(pkt)  // автоматична конвертація

// ✅ 5. Фіналізація запису
if err := muxer.WriteTrailer(); err != nil { /* handle */ }

// ✅ 6. Читання з авто-пробою
demuxer := flv.NewDemuxer(reader)
streams, err := demuxer.Streams()  // автоматично запустить пробу

// ✅ 7. Обробка закешованих пакетів з проби
for !demuxer.prober.Empty() {
    pkt := demuxer.prober.PopPacket()
    // обробка
}

// ✅ 8. Метрики для моніторингу
metrics.PacketsProcessed.Inc()
metrics.ProcessLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [vdk flv Package](https://pkg.go.dev/github.com/deepch/vdk/format/flv) — GoDoc documentation
- 💻 [vdk flvio Package](https://pkg.go.dev/github.com/deepch/vdk/format/flv/flvio) — низькорівневі FLV утиліти
- 📄 [FLV File Format Specification (Adobe)](https://download.macromedia.com/f4v/video_file_format_spec_v10_1.pdf) — офіційна специфікація
- 📄 [RTMP Specification](https://www.adobe.com/devnet/rtmp.html) — використання FLV у RTMP
- 🧪 [Go bufio Package](https://pkg.go.dev/bufio) — буферизований I/O для ефективності

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **медіа потоками у реальному часі**:
> 1. **Завжди викликайте `WriteTrailer()`** — без цього буферизовані дані можуть не записатися у файл/мережу.
> 2. **Кешуйте буфери через `sync.Pool`** — це значно зменшує навантаження на GC при обробці тисяч пакетів на секунду.
> 3. **Моніторьте `Probed()` стан** — якщо проба не завершується, потік може бути пошкоджений або використовувати непідтримувані кодеки.
> 4. **Обробляйте `CompositionTime` коректно** — це критично для синхронізації аудіо/відео при наявності B-frames.
> 5. **Тестуйте з різними клієнтами** — OBS, FFmpeg, власні клієнти можуть надсилати трохи різні формати тегів.

Потрібен приклад інтеграції `FLVToHLSTranscoder` з вашим `pubsub.Queue` для розподілу вже транскодованих пакетів між підписниками (WebSocket, архів, аналітика)? Готовий допомогти! 🚀