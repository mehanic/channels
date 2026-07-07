# 🎬 Глибокий розбір: av — Базові інтерфейси та структури для медіа-обробки

Цей файл — **ядро бібліотеки `vdk` (Video Development Kit)**, що визначає фундаментальні типи, інтерфейси та константи для роботи з медіа-контейнерами, кодеками та пакетами. Він є основою для всіх інших компонентів: демуксингу, муксингу, транскодування та фільтрації.

Розберемо архітектуру, ключові абстракції та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема пакету av

```
┌────────────────────────────────────────┐
│ 📦 av — Core Media Abstractions         │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові абстракції:                 │
│  • CodecType — ідентифікація кодеків   │
│  • CodecData — метадані кодеків        │
│  • Packet — контейнер для стиснених даних│
│  • AudioFrame — сирий аудіо-фрейм      │
│                                         │
│  🎛️  Інтерфейси:                        │
│  • Demuxer/Muxer — читання/запис контейнерів│
│  • AudioEncoder/Decoder — кодування/декодування│
│  • PacketReader/Writer — потокова обробка│
│                                         │
│  🎵 Аудіо-специфічні типи:              │
│  • SampleFormat — формат семплів (S16, FLTP...)│
│  • ChannelLayout — розкладка каналів (STEREO, 5.1...)│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. CodecType — типізація кодеків

### Бітова магія для розрізнення аудіо/відео:

```go
const codecTypeAudioBit = 0x1  // біт 0: 1=аудіо, 0=відео

func (self CodecType) IsAudio() bool {
    return self&codecTypeAudioBit != 0  // перевірка біта 0
}

func (self CodecType) IsVideo() bool {
    return self&codecTypeAudioBit == 0
}
```

### Генерація типів:

```go
// Аудіо кодеки: base << 1 | 0x1
func MakeAudioCodecType(base uint32) CodecType {
    return CodecType(base)<<1 | 0x1
}

// Відео кодеки: base << 1 | 0x0
func MakeVideoCodecType(base uint32) CodecType {
    return CodecType(base) << 1
}

// Приклади:
AAC        = MakeAudioCodecType(avCodecTypeMagic + 1)  // (233334<<1)|1 = 466669
H264       = MakeVideoCodecType(avCodecTypeMagic + 1)  // (233334<<1)|0 = 466668
```

### ✅ Ваш use-case: фільтрація потоків за типом

```go
// FilterStreams — вибір тільки потрібних типів потоків
func FilterStreams(streams []av.CodecData, wantTypes []av.CodecType) []av.CodecData {
    var result []av.CodecData
    for _, s := range streams {
        for _, want := range wantTypes {
            if s.Type() == want {
                result = append(result, s)
                break
            }
        }
    }
    return result
}

// Приклад: тільки відео + AAC аудіо для HLS
hlsStreams := FilterStreams(allStreams, []av.CodecType{av.H264, av.AAC})
```

---

## 🎵 2. SampleFormat & ChannelLayout — аудіо-параметри

### SampleFormat — формат семплів:

```go
type SampleFormat uint8

const (
    U8   = SampleFormat(iota + 1) // 8-bit unsigned
    S16                           // 16-bit signed (найпоширеніший)
    S32                           // 32-bit signed
    FLT                           // 32-bit float
    DBL                           // 64-bit float
    // Planar версії (окремий масив на канал):
    U8P, S16P, S32P, FLTP, DBLP
)

// Корисні методи:
func (self SampleFormat) BytesPerSample() int {
    switch self {
    case U8, U8P: return 1
    case S16, S16P: return 2
    case FLT, FLTP, S32, S32P: return 4
    case DBL, DBLP: return 8
    }
    return 0
}

func (self SampleFormat) IsPlanar() bool {
    // Planar: кожен канал у окремому масиві (краще для обробки)
    return self == S16P || self == FLTP || ...
}
```

### ChannelLayout — розкладка аудіо-каналів:

```go
type ChannelLayout uint16  // бітова маска каналів

const (
    CH_FRONT_CENTER = 1 << iota  // 0x01
    CH_FRONT_LEFT                // 0x02
    CH_FRONT_RIGHT               // 0x04
    CH_BACK_CENTER               // 0x08
    // ... інші канали
    
    // Готові конфігурації:
    CH_MONO     = CH_FRONT_CENTER                    // 1 канал
    CH_STEREO   = CH_FRONT_LEFT | CH_FRONT_RIGHT     // 2 канали
    CH_5POINT1  = CH_STEREO | CH_FRONT_CENTER | CH_LOW_FREQ | CH_BACK_LEFT | CH_BACK_RIGHT  // 6 каналів
)

// Підрахунок кількості каналів (бітовий трюк):
func (self ChannelLayout) Count() (n int) {
    for self != 0 {
        n++
        self = (self - 1) & self  // видалення наймолодшого біта 1
    }
    return
}
```

### 🔍 Бітовий трюк `(self - 1) & self`:

```
Приклад: self = 0b10110 (22, 3 канали)

Ітерація 1:
  self = 0b10110
  self-1 = 0b10101
  (self-1)&self = 0b10100  ← видалено найправіший 1
  n = 1

Ітерація 2:
  self = 0b10100
  self-1 = 0b10011
  (self-1)&self = 0b10000
  n = 2

Ітерація 3:
  self = 0b10000
  self-1 = 0b01111
  (self-1)&self = 0b00000  ← готово
  n = 3 ✓

Ефективність: O(k) де k = кількість бітів 1, а не загальна кількість бітів
```

### ✅ Ваш use-case: нормалізація аудіо для HLS

```go
// NormalizeAudioForHLS — конвертація аудіо у формат, сумісний з HLS
func NormalizeAudioForHLS(frame av.AudioFrame) (av.AudioFrame, error) {
    // HLS вимагає: AAC, 44.1/48kHz, стерео, S16 або FLTP
    
    target := av.AudioFrame{
        SampleRate:    48000,
        ChannelLayout: av.CH_STEREO,
        SampleFormat:  av.S16P,  // planar для кращої обробки
    }
    
    // Якщо вже підходить — повертаємо як є
    if frame.HasSameFormat(target) {
        return frame, nil
    }
    
    // Інакше: використовуємо AudioResampler для конвертації
    resampler, err := ffmpeg.NewAudioResampler(frame, target)
    if err != nil {
        return av.AudioFrame{}, err
    }
    defer resampler.Close()
    
    return resampler.Resample(frame)
}
```

---

## 📦 3. Packet — контейнер для стиснених даних

```go
type Packet struct {
    IsKeyFrame      bool          // відео: чи це I-frame (точка входу для декодування)
    Idx             int8          // індекс потоку у контейнері (0=відео, 1=аудіо...)
    CompositionTime time.Duration // для B-frames: PTS - DTS (час композиції)
    Time            time.Duration // DTS (Decoding Time Stamp) — коли декодувати
    Duration        time.Duration // тривалість пакету у часі
    Data            []byte        // стиснені дані (H.264 NAL units, AAC ADTS тощо)
}
```

### 🔑 Ключові поля для синхронізації:

| Поле | Призначення | Приклад використання |
|------|-------------|---------------------|
| `Time` (DTS) | Коли декодувати пакет | Сортування пакетів перед декодуванням |
| `CompositionTime` | Різниця PTS-DTS для B-frames | `PTS = Time + CompositionTime` |
| `Duration` | Тривалість відтворення | Розрахунок загальної тривалості сегменту |
| `IsKeyFrame` | Чи це точка входу | Початок нового HLS-сегменту тільки з I-frame |

### ✅ Ваш use-case: детекція ключових кадрів для сегментації

```go
// ShouldStartNewSegment — чи починати новий HLS-сегмент з цього пакету?
func ShouldStartNewSegment(pkt av.Packet, lastKeyFrameTime time.Duration, minSegmentDur time.Duration) bool {
    // Умови для нового сегменту:
    // 1. Це ключовий кадр (I-frame)
    // 2. Прошло достатньо часу від останнього сегменту
    // 3. Потік відео (не аудіо/субтитри)
    
    if !pkt.IsKeyFrame {
        return false
    }
    if pkt.Time-lastKeyFrameTime < minSegmentDur {
        return false  // ще рано для нового сегменту
    }
    
    return true
}

// Використання у основному циклі:
var lastKeyFrameTime time.Duration
for {
    pkt, err := demuxer.ReadPacket()
    if err != nil { break }
    
    if ShouldStartNewSegment(pkt, lastKeyFrameTime, 10*time.Second) {
        finalizeCurrentSegment()
        startNewSegment()
        lastKeyFrameTime = pkt.Time
    }
    
    writePacketToCurrentSegment(pkt)
}
```

---

## 🎵 4. AudioFrame — сирий аудіо-фрейм

```go
type AudioFrame struct {
    SampleFormat  SampleFormat  // формат семплів: S16, FLTP тощо
    ChannelLayout ChannelLayout // розкладка каналів: STEREO, 5.1 тощо
    SampleCount   int           // кількість семплів у цьому фреймі
    SampleRate    int           // частота дискретизації: 44100, 48000 Гц
    Data          [][]byte      // дані: [канал0][канал1]... для planar, або [інтерліс] для packed
}
```

### 🔧 Корисні методи:

```go
// Duration — тривалість фрейму у часі
func (self AudioFrame) Duration() time.Duration {
    // Формула: (кількість семплів / частота) секунд
    return time.Second * time.Duration(self.SampleCount) / time.Duration(self.SampleRate)
}

// HasSameFormat — перевірка сумісності форматів
func (self AudioFrame) HasSameFormat(other AudioFrame) bool {
    return self.SampleRate == other.SampleRate &&
           self.ChannelLayout == other.ChannelLayout &&
           self.SampleFormat == other.SampleFormat
}

// Slice — вирізання піддіапазону семплів
func (self AudioFrame) Slice(start, end int) AudioFrame {
    out := self
    out.SampleCount = end - start
    size := self.SampleFormat.BytesPerSample()
    
    // Для кожного каналу: вирізаємо відповідний діапазон байтів
    for i := range out.Data {
        out.Data[i] = out.Data[i][start*size : end*size]
    }
    return out
}

// Concat — об'єднання двох фреймів
func (self AudioFrame) Concat(in AudioFrame) AudioFrame {
    out := self
    out.SampleCount += in.SampleCount
    
    // Для кожного каналу: дописуємо дані
    for i := range out.Data {
        out.Data[i] = append(out.Data[i], in.Data[i]...)
    }
    return out
}
```

### ✅ Ваш use-case: буферизація аудіо для точної сегментації

```go
// AudioBuffer — буфер для накопичення аудіо до точної межі сегменту
type AudioBuffer struct {
    frames     []av.AudioFrame
    totalDur   time.Duration
    targetDur  time.Duration  // наприклад, 10 секунд для HLS
}

func (b *AudioBuffer) AddFrame(frame av.AudioFrame) {
    b.frames = append(b.frames, frame)
    b.totalDur += frame.Duration()
}

func (b *AudioBuffer) GetSegment() ([]av.AudioFrame, bool) {
    if b.totalDur < b.targetDur {
        return nil, false  // ще не набралось достатньо
    }
    
    // Витягуємо фрейми доки не перевищимо targetDur
    var segment []av.AudioFrame
    var accumulated time.Duration
    
    for _, frame := range b.frames {
        if accumulated+frame.Duration() > b.targetDur {
            // Цей фрейм частково потрапляє у сегмент — вирізаємо потрібну частину
            needed := b.targetDur - accumulated
            samplesNeeded := int(needed * time.Duration(frame.SampleRate) / time.Second)
            
            partial := frame.Slice(0, samplesNeeded)
            segment = append(segment, partial)
            
            // Залишок залишаємо у буфері для наступного сегменту
            remainder := frame.Slice(samplesNeeded, frame.SampleCount)
            b.frames = append([]av.AudioFrame{remainder}, b.frames[len(segment):]...)
            break
        }
        
        segment = append(segment, frame)
        accumulated += frame.Duration()
    }
    
    // Видаляємо використані фрейми з буфера
    b.frames = b.frames[len(segment):]
    b.totalDur -= accumulated
    
    return segment, true
}
```

---

## 🎛️ 5. Інтерфейси: Demuxer, Muxer, Encoder, Decoder

### Demuxer — читання з контейнера:

```go
type Demuxer interface {
    PacketReader                   // ReadPacket() → av.Packet
    Streams() ([]CodecData, error) // метадані потоків (кодеки, роздільна здатність тощо)
}

type DemuxCloser interface {
    Demuxer
    Close() error  // закриття ресурсів (файл, мережеве з'єднання)
}
```

### Muxer — запис у контейнер:

```go
type Muxer interface {
    WriteHeader([]CodecData) error  // запис заголовка з метаданими
    PacketWriter                    // WritePacket(av.Packet) error
    WriteTrailer() error            // фіналізація файлу (індекси, метадані)
}

type MuxCloser interface {
    Muxer
    Close() error  // автоматично викликає WriteTrailer() якщо потрібно
}
```

### AudioEncoder/Decoder — кодування/декодування:

```go
type AudioEncoder interface {
    CodecData() (AudioCodecData, error)   // метадані енкодера для заголовка
    Encode(AudioFrame) ([][]byte, error)  // сирий фрейм → стиснені пакети (1→N)
    Close()                               // звільнення ресурсів (CGO контексти)
    
    // Налаштування параметрів:
    SetSampleRate(int) error
    SetChannelLayout(ChannelLayout) error
    SetSampleFormat(SampleFormat) error
    SetBitrate(int) error
    SetOption(string, interface{}) error  // довільні опції (напр. ffmpeg av_opt_set)
}

type AudioDecoder interface {
    Decode([]byte) (bool, AudioFrame, error)  // стиснений пакет → сирий фрейм
    Close()
}
```

### ✅ Ваш use-case: абстракція над різними джерелами

```go
// MediaSource — уніфікований інтерфейс для різних джерел медіа
type MediaSource interface {
    av.DemuxCloser
    SourceInfo() SourceInfo  // метадані джерела (камера, файл, мережа)
}

// Реалізації:
type RTSPSource struct {
    av.DemuxCloser
    url string
    // ... RTSP-специфічні поля
}

type FileSource struct {
    av.DemuxCloser
    path string
}

type MemorySource struct {
    av.DemuxCloser
    data []byte
    pos  int
}

// Factory для створення джерела за URI:
func OpenMediaSource(uri string) (MediaSource, error) {
    switch {
    case strings.HasPrefix(uri, "rtsp://"):
        return NewRTSPSource(uri)
    case strings.HasPrefix(uri, "file://"):
        return NewFileSource(strings.TrimPrefix(uri, "file://"))
    case strings.HasPrefix(uri, "memory://"):
        return NewMemorySource(parseMemoryURI(uri))
    default:
        // Спроба через avutil.Open (авто-детект)
        demuxer, err := avutil.Open(uri)
        if err != nil {
            return nil, err
        }
        return &GenericSource{DemuxCloser: demuxer, uri: uri}, nil
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// cctv_media_processor.go — обробка медіа з використанням av абстракцій
type MediaProcessor struct {
    channelID    string
    source       av.DemuxCloser
    encoder      av.AudioEncoder  // для транскодування аудіо
    hlsMuxer     av.MuxCloser
    packetQueue  *pubsub.Queue    // для розподілу між підписниками
}

func NewMediaProcessor(channelID, inputURI string) (*MediaProcessor, error) {
    // 1. Відкриття джерела
    source, err := avutil.Open(inputURI)
    if err != nil {
        return nil, fmt.Errorf("open source: %w", err)
    }
    
    // 2. Отримання метаданих потоків
    streams, err := source.Streams()
    if err != nil {
        source.Close()
        return nil, err
    }
    
    // 3. Фільтрація потрібних потоків (відео + аудіо)
    var videoStream, audioStream av.CodecData
    for _, s := range streams {
        if s.Type().IsVideo() && videoStream == nil {
            videoStream = s
        }
        if s.Type().IsAudio() && audioStream == nil {
            audioStream = s
        }
    }
    
    // 4. Налаштування енкодера якщо потрібно транскодування
    var encoder av.AudioEncoder
    if audioStream != nil && audioStream.Type() != av.AAC {
        encoder, err = avutil.DefaultHandlers.NewAudioEncoder(av.AAC)
        if err != nil {
            source.Close()
            return nil, fmt.Errorf("create AAC encoder: %w", err)
        }
        // Налаштування параметрів енкодера
        encoder.SetSampleRate(48000)
        encoder.SetChannelLayout(av.CH_STEREO)
        encoder.SetSampleFormat(av.S16P)
    }
    
    return &MediaProcessor{
        channelID: channelID,
        source:    source,
        encoder:   encoder,
        // hlsMuxer ініціалізується пізніше
    }, nil
}

// Process — основний цикл обробки
func (p *MediaProcessor) Process(ctx context.Context) error {
    // 1. Ініціалізація HLS муксера
    if err := p.initHLSMuxer(); err != nil {
        return err
    }
    defer p.hlsMuxer.Close()
    
    // 2. Запис заголовка з транскодованими метаданими
    streams, _ := p.source.Streams()
    if p.encoder != nil {
        // Замінюємо аудіо кодек на AAC у метаданих
        for i, s := range streams {
            if s.Type().IsAudio() {
                streams[i], _ = p.encoder.CodecData()
                break
            }
        }
    }
    p.hlsMuxer.WriteHeader(streams)
    
    // 3. Основний цикл читання/обробки/запису
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        pkt, err := p.source.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            log.Warn("read packet failed", "err", err)
            continue
        }
        
        // Обробка пакету
        processedPkts, err := p.processPacket(pkt)
        if err != nil {
            log.Error("process packet failed", "err", err)
            continue
        }
        
        // Запис у HLS та розсилка підписникам
        for _, outPkt := range processedPkts {
            if err := p.hlsMuxer.WritePacket(outPkt); err != nil {
                return err
            }
            if p.packetQueue != nil {
                p.packetQueue.WritePacket(outPkt)
            }
        }
    }
    
    // 4. Фіналізація
    return p.hlsMuxer.WriteTrailer()
}

// processPacket — обробка одного пакету (транскодування, фільтрація тощо)
func (p *MediaProcessor) processPacket(pkt av.Packet) ([]av.Packet, error) {
    // Якщо це аудіо і потрібне транскодування
    if p.encoder != nil && pkt.Idx == p.getAudioStreamIdx() {
        // 1. Декодування у сирий фрейм
        decoder, err := avutil.DefaultHandlers.NewAudioDecoder(pkt.(av.AudioCodecData))
        if err != nil {
            return nil, err
        }
        defer decoder.Close()
        
        ok, frame, err := decoder.Decode(pkt.Data)
        if err != nil || !ok {
            return nil, err
        }
        
        // 2. Кодування у цільовий формат (AAC)
        encodedPkts, err := p.encoder.Encode(frame)
        if err != nil {
            return nil, err
        }
        
        // 3. Створення нових пакетів з корегованими таймінгами
        var result []av.Packet
        for _, data := range encodedPkts {
            result = append(result, av.Packet{
                Idx:        pkt.Idx,
                Time:       pkt.Time,  // спрощено: потрібна корекція через Timeline
                Duration:   pkt.Duration,
                Data:       data,
                IsKeyFrame: false,  // аудіо не має ключових кадрів
            })
        }
        return result, nil
    }
    
    // Для відео або аудіо без транскодування: passthrough
    return []av.Packet{pkt}, nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"invalid CodecType"** | Неправильне використання `MakeAudioCodecType` | Переконайтеся, що `base` унікальний і не конфліктує з іншими типами |
| **Аудіо розсинхронізоване** | Неправильний розрахунок `Duration` або `CompositionTime` | Використовуйте `PacketDuration()` з `AudioCodecData` замість ручного розрахунку |
| **Planar/packed плутанина** | `Data [][]byte` інтерпретується неправильно | Перевіряйте `SampleFormat.IsPlanar()` перед обробкою `Data` |
| **B-frames ламають таймінги** | `CompositionTime` не враховується при розрахунку PTS | `PTS = pkt.Time + pkt.CompositionTime` для відео з B-frames |
| **Закриття ресурсів** | `Close()` не викликається → memory leak | Використовуйте `defer source.Close()` одразу після успішного відкриття |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування CodecData:

```go
type CodecCache struct {
    mu    sync.RWMutex
    cache map[av.CodecType]av.CodecData
}

func (c *CodecCache) Get(codecType av.CodecType, factory func() (av.CodecData, error)) (av.CodecData, error) {
    c.mu.RLock()
    if data, ok := c.cache[codecType]; ok {
        c.mu.RUnlock()
        return data, nil
    }
    c.mu.RUnlock()
    
    data, err := factory()
    if err != nil {
        return nil, err
    }
    
    c.mu.Lock()
    c.cache[codecType] = data
    c.mu.Unlock()
    
    return data, nil
}
```

### 2. Пакетна обробка аудіо-фреймів:

```go
// BatchEncode — кодування кількох фреймів за один виклик
func BatchEncode(encoder av.AudioEncoder, frames []av.AudioFrame) ([][]byte, error) {
    var result [][]byte
    for _, frame := range frames {
        pkts, err := encoder.Encode(frame)
        if err != nil {
            return result, err
        }
        result = append(result, pkts...)
    }
    return result, nil
}
```

### 3. Моніторинг параметрів аудіо:

```go
type AudioMetrics struct {
    SampleRate    prometheus.GaugeVec
    ChannelCount  prometheus.GaugeVec
    Format        prometheus.GaugeVec
    ProcessingTime prometheus.Histogram
}

func (m *AudioMetrics) RecordFrame(frame av.AudioFrame, channelID string, duration time.Duration) {
    m.SampleRate.WithLabelValues(channelID).Set(float64(frame.SampleRate))
    m.ChannelCount.WithLabelValues(channelID).Set(float64(frame.ChannelLayout.Count()))
    m.Format.WithLabelValues(channelID).Set(float64(frame.SampleFormat))
    m.ProcessingTime.WithLabelValues(channelID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції пакету av

```go
// ✅ 1. Визначення типів потоків
for _, stream := range streams {
    if stream.Type().IsVideo() {
        // Обробка відео
    }
    if stream.Type().IsAudio() {
        audioData := stream.(av.AudioCodecData)  // type assertion
        // Доступ до аудіо-параметрів:
        _ = audioData.SampleRate()
        _ = audioData.ChannelLayout()
        _ = audioData.SampleFormat()
    }
}

// ✅ 2. Обробка пакетів з урахуванням таймінгів
pts := pkt.Time + pkt.CompositionTime  // для B-frames
duration := pkt.Duration               // тривалість відтворення

// ✅ 3. Робота з аудіо-фреймами
if frame.SampleFormat.IsPlanar() {
    // Planar: Data[0] = канал0, Data[1] = канал1...
    for i, channelData := range frame.Data {
        processChannel(i, channelData)
    }
} else {
    // Packed: інтерліс [L0,R0,L1,R1,...]
    processInterleaved(frame.Data[0])
}

// ✅ 4. Закриття ресурсів
defer func() {
    if encoder != nil { encoder.Close() }
    if decoder != nil { decoder.Close() }
    if muxer != nil { muxer.Close() }
    if demuxer != nil { demuxer.Close() }
}()

// ✅ 5. Метрики
monitoring.PacketsProcessed.Inc()
monitoring.AudioSampleRate.Set(float64(audioData.SampleRate()))
```

---

## 🔗 Корисні посилання

- 💻 [vdk av Package](https://pkg.go.dev/github.com/deepch/vdk/av) — GoDoc documentation
- 📄 [FFmpeg Codec Parameters](https://ffmpeg.org/doxygen/trunk/structAVCodecParameters.html) — аналогічні концепції у FFmpeg
- 🎬 [HLS Audio Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до аудіо у HLS
- 🧪 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади використання av інтерфейсів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Завжди перевіряйте `pkt.IsKeyFrame`** перед початком нового HLS-сегменту — це гарантує коректне відтворення.
> 2. **Використовуйте `CompositionTime` для B-frames** — інакше аудіо/відео розсинхронізуються.
> 3. **Кешуйте `AudioCodecData`** — створення нового енкодера для кожного пакету дороге.
> 4. **Моніторьте `SampleRate` та `ChannelLayout`** — зміни цих параметрів "на льоту" можуть зламати плеєри.
> 5. **Тестуйте з різними `SampleFormat`** — CCTV камери часто використовують PCM_MULAW/ALAW, які потребують конвертації у S16/FLTP для обробки.

Потрібен приклад реалізації `AudioResampler` для конвертації між різними `SampleFormat`/`ChannelLayout`/`SampleRate` у реальному часі? Готовий допомогти! 🚀