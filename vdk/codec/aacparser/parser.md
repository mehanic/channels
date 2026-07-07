# 🎵 Глибокий розбір: aacparser — Парсер AAC/MPEG-4 Audio Config

Цей файл — **реалізація парсингу та генерації AAC конфігурацій** згідно зі стандартом MPEG-4 Audio (ISO/IEC 14496-3). Він надає інструменти для роботи з двома основними форматами: **ADTS-заголовками** (для потокового передавання) та **AudioSpecificConfig** (для контейнерів на кшталт MP4/FLV).

Розберемо архітектуру, бітові формати та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема aacparser

```
┌────────────────────────────────────────┐
│ 📦 aacparser — AAC Config Handling     │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • MPEG4AudioConfig — універсальна структура│
│  • ADTS Header Parser/Filler — для потоку│
│  • AudioSpecificConfig Parser/Writer — для контейнерів│
│  • CodecData — інтеграція з av.CodecData│
│                                         │
│  📊 Таблиці:                            │
│  • sampleRateTable — 13 стандартних частот│
│  • chanConfigTable — 8 конфігурацій каналів│
│  • AOT_* — Audio Object Types (1-39+)  │
│                                         │
│  🔄 Потоки даних:                       │
│  Raw AAC → ParseADTSHeader() → config │
│  MP4 CodecData → ParseMPEG4AudioConfigBytes() → config│
│  config → FillADTSHeader() → ADTS packet│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. MPEG4AudioConfig — універсальна структура

```go
type MPEG4AudioConfig struct {
    SampleRate      int              // Частота дискретизації: 48000, 44100 Гц
    ChannelLayout   av.ChannelLayout // Розкладка каналів: STEREO, 5.1 тощо
    ObjectType      uint             // Audio Object Type: 2=AAC-LC, 5=SBR тощо
    SampleRateIndex uint             // Індекс у sampleRateTable (0-12) або 0xf+24-біт значення
    ChannelConfig   uint             // Конфігурація каналів: 1=mono, 2=stereo...7=7.1
}
```

### 🎯 Поля та їх значення:

| Поле | Тип | Діапазон | Приклад |
|------|-----|----------|---------|
| `ObjectType` | `uint` | 1-39+ | `2` = AAC-LC (найпоширеніший), `5` = AAC-LC+SBR (HE-AAC) |
| `SampleRateIndex` | `uint` | 0-12 або 0xf+24біт | `3` = 48000 Гц, `4` = 44100 Гц |
| `ChannelConfig` | `uint` | 0-7 | `2` = стерео, `6` = 5.1 |
| `SampleRate` | `int` | Обчислюється з індексу | `48000` |
| `ChannelLayout` | `av.ChannelLayout` | Обчислюється з конфігурації | `av.CH_STEREO` |

### 🔧 Метод `Complete()` — заповнення обчислюваних полів:

```go
func (self *MPEG4AudioConfig) Complete() {
    // SampleRate з індексу
    if int(self.SampleRateIndex) < len(sampleRateTable) {
        self.SampleRate = sampleRateTable[self.SampleRateIndex]
    }
    // ChannelLayout з конфігурації
    if int(self.ChannelConfig) < len(chanConfigTable) {
        self.ChannelLayout = chanConfigTable[self.ChannelConfig]
    }
}
```

### 📊 Таблиці:

```go
// Частоти дискретизації (стандартні для AAC)
var sampleRateTable = []int{
    96000, 88200, 64000, 48000, 44100, 32000,  // 0-5
    24000, 22050, 16000, 12000, 11025, 8000, 7350,  // 6-12
}

// Конфігурації каналів (відповідність до av.ChannelLayout)
var chanConfigTable = []av.ChannelLayout{
    0,  // 0: undefined
    av.CH_FRONT_CENTER,  // 1: mono
    av.CH_FRONT_LEFT | av.CH_FRONT_RIGHT,  // 2: stereo
    av.CH_FRONT_CENTER | av.CH_FRONT_LEFT | av.CH_FRONT_RIGHT,  // 3: 3.0
    // ... до 7: 7.1
}
```

### ✅ Ваш use-case: валідація конфігурації перед транскодуванням

```go
// ValidateAACConfig — перевірка чи конфігурація сумісна з HLS
func ValidateAACConfig(config aacparser.MPEG4AudioConfig) error {
    // HLS вимагає: AAC-LC (ObjectType=2), 44.1/48kHz, stereo/mono
    if config.ObjectType != aacparser.AOT_AAC_LC {
        return fmt.Errorf("unsupported AAC object type: %d (need %d for LC)", 
            config.ObjectType, aacparser.AOT_AAC_LC)
    }
    
    validRates := map[int]bool{44100: true, 48000: true}
    if !validRates[config.SampleRate] {
        return fmt.Errorf("unsupported sample rate: %d Hz (need 44100 or 48000)", 
            config.SampleRate)
    }
    
    if config.ChannelConfig < 1 || config.ChannelConfig > 2 {
        return fmt.Errorf("unsupported channel config: %d (need 1=mono or 2=stereo)", 
            config.ChannelConfig)
    }
    
    return nil
}
```

---

## 📦 2. ADTS Header — парсинг та генерація

### Формат ADTS-заголовка (7 або 9 байт):

```
Бітова структура (7 байт, protection_absent=1):
  0-11:   Syncword (0xFFF)
  12:     ID (0=MPEG-4, 1=MPEG-2)
  13-14:  Layer (завжди 00 для AAC)
  15:     Protection absent (1=no CRC, 0=CRC)
  16-17:  Profile/ObjectType-1 (0=AAC Main, 1=AAC-LC...)
  18-21:  Sample rate index (4 біти)
  22:     Private bit
  23-25:  Channel config (3 біти)
  26-27:  Original/copy, home
  28-29:  Copyrighted ID bit, A-bit
  30:     P-frame indicator
  31-34:  Frame length (13 біт) — включаючи заголовок!
  35-36:  ADTS buffer fullness (11 біт)
  37-38:  Number of AAC frames in ADTS frame-2 (2 біти)

Якщо protection_absent=0: додається 2 байти CRC → заголовок 9 байт
```

### 🔍 Парсинг `ParseADTSHeader()`:

```go
func ParseADTSHeader(frame []byte) (config MPEG4AudioConfig, hdrlen, framelen, samples int, err error) {
    // 1. Перевірка syncword: 0xFFF + ID+Layer+protection
    if frame[0] != 0xff || frame[1]&0xf6 != 0xf0 {
        err = fmt.Errorf("aacparser: not adts header")
        return
    }
    
    // 2. Витягування основних полів
    config.ObjectType = uint(frame[2]>>6) + 1  // біти 16-17 + 1
    config.SampleRateIndex = uint(frame[2] >> 2 & 0xf)  // біти 18-21
    config.ChannelConfig = uint(frame[2]<<2&0x4 | frame[3]>>6&0x3)  // біти 23-25
    
    // 3. Перевірка валідності
    if config.ChannelConfig == 0 {
        err = fmt.Errorf("aacparser: adts channel count invalid")
        return
    }
    config.Complete()  // заповнення SampleRate/ChannelLayout
    
    // 4. Розрахунок довжини фрейму (біти 31-43)
    framelen = int(frame[3]&0x3)<<11 | int(frame[4])<<3 | int(frame[5]>>5)
    
    // 5. Кількість семплів: (frame_count+1) × 1024
    samples = (int(frame[6]&0x3) + 1) * 1024
    
    // 6. Довжина заголовка: 7 байт (без CRC) або 9 байт (з CRC)
    hdrlen = 7
    if frame[1]&0x1 == 0 {  // protection_absent=0
        hdrlen = 9
    }
    
    // 7. Валідація: довжина фрейму >= довжини заголовка
    if framelen < hdrlen {
        err = fmt.Errorf("aacparser: adts framelen < hdrlen")
        return
    }
    return
}
```

### ✍️ Генерація `FillADTSHeader()`:

```go
func FillADTSHeader(header []byte, config MPEG4AudioConfig, samples, payloadLength int) {
    payloadLength += 7  // загальна довжина = payload + заголовок
    
    // Базові байти (синхронізація + фіксовані біти)
    header[0] = 0xff
    header[1] = 0xf1  // ID=0 (MPEG-4), Layer=00, protection_absent=1
    header[2] = 0x50  // шаблон, буде перезаписано
    header[3] = 0x80
    header[4] = 0x43
    header[5] = 0xff
    header[6] = 0xcd
    
    // Перезапис змінних полів:
    // Біти 16-25: ObjectType-1, SampleRateIndex, верхній біт ChannelConfig
    header[2] = (byte(config.ObjectType-1)&0x3)<<6 | 
                (byte(config.SampleRateIndex)&0xf)<<2 | 
                byte(config.ChannelConfig>>2)&0x1
    
    // Біти 26-34: нижні 2 біти ChannelConfig + верхні 2 біти довжини
    header[3] = header[3]&0x3f | byte(config.ChannelConfig&0x3)<<6
    header[3] = header[3]&0xfc | byte(payloadLength>>11)&0x3
    
    // Біти 35-43: середні 8 біт довжини
    header[4] = byte(payloadLength >> 3)
    
    // Біти 44-51: нижні 3 біти довжини + верхні 2 біти frame_count-1
    header[5] = header[5]&0x1f | (byte(payloadLength)&0x7)<<5
    header[6] = header[6]&0xfc | byte(samples/1024-1)  // frame_count-1 у бітах 52-53
}
```

### ✅ Ваш use-case: додавання ADTS-заголовків для HLS

```go
// WrapAACWithADTS — обгортка сирих AAC пакетів у ADTS для HLS-сегментів
func WrapAACWithADTS(rawData []byte, config aacparser.MPEG4AudioConfig) ([]byte, error) {
    // 1. Розрахунок кількості семплів (AAC завжди 1024 семпли на фрейм для LC)
    samples := 1024
    
    // 2. Створення буфера для заголовка + даних
    header := make([]byte, aacparser.ADTSHeaderLength)
    aacparser.FillADTSHeader(header, config, samples, len(rawData))
    
    // 3. Об'єднання заголовка та даних
    result := make([]byte, len(header)+len(rawData))
    copy(result, header)
    copy(result[len(header):], rawData)
    
    return result, nil
}

// Використання у транскодері:
func (t *Transcoder) encodeAACFrame(frame av.AudioFrame) ([]byte, error) {
    // Кодування у сирий AAC (без заголовка)
    rawPackets, err := t.aacEncoder.Encode(frame)
    if err != nil {
        return nil, err
    }
    
    // Отримання конфігурації енкодера
    codecData, _ := t.aacEncoder.CodecData()
    aacCodec := codecData.(aacparser.CodecData)
    config := aacCodec.Config
    
    // Обгортка кожного пакету у ADTS
    var result []byte
    for _, pkt := range rawPackets {
        adtsPkt, err := WrapAACWithADTS(pkt, config)
        if err != nil {
            return nil, err
        }
        result = append(result, adtsPkt...)
    }
    return result, nil
}
```

---

## 🔧 3. AudioSpecificConfig — бітовий парсинг для контейнерів

### Формат AudioSpecificConfig (бітовий потік):

```
5 біт: audioObjectType (1-31, або 31+6 біт для escape)
4 біти: samplingFrequencyIndex (0-12, або 15+24 біт для explicit rate)
4 біти: channelConfiguration (0-7)
[опціонально: GASpecificConfig, SBR/PSSignaling тощо]
```

### 🔍 Парсинг `ParseMPEG4AudioConfigBytes()`:

```go
func ParseMPEG4AudioConfigBytes(data []byte) (config MPEG4AudioConfig, err error) {
    r := bytes.NewReader(data)
    br := &bits.Reader{R: r}  // бітовий читач
    
    // 1. ObjectType з підтримкою escape-значення
    if config.ObjectType, err = readObjectType(br); err != nil {
        return
    }
    
    // 2. SampleRateIndex з підтримкою explicit rate
    if config.SampleRateIndex, err = readSampleRateIndex(br); err != nil {
        return
    }
    
    // 3. ChannelConfig (4 біти)
    if config.ChannelConfig, err = br.ReadBits(4); err != nil {
        return
    }
    
    // 4. Заповнення обчислюваних полів
    config.Complete()
    return
}

// Допоміжні функції для escape-значень:
func readObjectType(r *bits.Reader) (uint, error) {
    objectType, err := r.ReadBits(5)
    if err != nil { return 0, err }
    if objectType == AOT_ESCAPE {  // 31
        ext, err := r.ReadBits(6)
        if err != nil { return 0, err }
        return 32 + ext, nil
    }
    return objectType, nil
}
```

### ✍️ Генерація `WriteMPEG4AudioConfig()`:

```go
func WriteMPEG4AudioConfig(w io.Writer, config MPEG4AudioConfig) error {
    bw := &bits.Writer{W: w}  // бітовий записувач
    
    // 1. Запис ObjectType з escape-обробкою
    if err := writeObjectType(bw, config.ObjectType); err != nil {
        return err
    }
    
    // 2. Автоматичне визначення SampleRateIndex якщо не вказано
    if config.SampleRateIndex == 0 {
        for i, rate := range sampleRateTable {
            if rate == config.SampleRate {
                config.SampleRateIndex = uint(i)
                break
            }
        }
    }
    if err := writeSampleRateIndex(bw, config.SampleRateIndex); err != nil {
        return err
    }
    
    // 3. Автоматичне визначення ChannelConfig якщо не вказано
    if config.ChannelConfig == 0 {
        for i, layout := range chanConfigTable {
            if layout == config.ChannelLayout {
                config.ChannelConfig = uint(i)
                break
            }
        }
    }
    if err := bw.WriteBits(config.ChannelConfig, 4); err != nil {
        return err
    }
    
    // 4. Флеш залишкових біт (якщо потрібно)
    return bw.FlushBits()
}
```

### ✅ Ваш use-case: створення CodecData для HLS муксера

```go
// CreateAACCodecData — створення av.CodecData з параметрів для заголовка MP4/HLS
func CreateAACCodecData(sampleRate int, channelLayout av.ChannelLayout, objectType uint) (av.CodecData, error) {
    config := aacparser.MPEG4AudioConfig{
        SampleRate:    sampleRate,
        ChannelLayout: channelLayout,
        ObjectType:    objectType,
    }
    
    // Автоматичне заповнення індексів
    config.Complete()
    
    // Генерація AudioSpecificConfig байтів
    var buf bytes.Buffer
    if err := aacparser.WriteMPEG4AudioConfig(&buf, config); err != nil {
        return nil, err
    }
    
    // Створення CodecData для використання у WriteHeader()
    return aacparser.NewCodecDataFromMPEG4AudioConfigBytes(buf.Bytes())
}

// Використання при ініціалізації HLS:
func (h *HLSMuxer) initAudioCodec(sampleRate int, channels int) error {
    layout := av.CH_MONO
    if channels == 2 {
        layout = av.CH_STEREO
    }
    
    codecData, err := CreateAACCodecData(48000, layout, aacparser.AOT_AAC_LC)
    if err != nil {
        return err
    }
    
    // Додавання у заголовок муксера
    return h.WriteHeader([]av.CodecData{videoCodecData, codecData})
}
```

---

## 📦 4. CodecData — інтеграція з av.CodecData

```go
type CodecData struct {
    ConfigBytes []byte           // сирі байти AudioSpecificConfig
    Config      MPEG4AudioConfig // розпарсена структура
}

// Реалізація інтерфейсу av.CodecData:
func (self CodecData) Type() av.CodecType {
    return av.AAC  // завжди AAC
}

func (self CodecData) SampleRate() int {
    return self.Config.SampleRate
}

func (self CodecData) ChannelLayout() av.ChannelLayout {
    return self.Config.ChannelLayout
}

func (self CodecData) SampleFormat() av.SampleFormat {
    return av.FLTP  // AAC завжди використовує float planar internally
}

func (self CodecData) PacketDuration(data []byte) (time.Duration, error) {
    // AAC-LC завжди 1024 семпли на фрейм
    return time.Duration(1024) * time.Second / time.Duration(self.Config.SampleRate), nil
}

func (self CodecData) Tag() string {
    // FourCC для MP4: "mp4a.40.X" де X = ObjectType
    return fmt.Sprintf("mp4a.40.%d", self.Config.ObjectType)
}
```

### 🔑 Метод `PacketDuration()` — критичний для синхронізації:

```go
func (self CodecData) PacketDuration(data []byte) (dur time.Duration, err error) {
    // Формула: (кількість семплів / частота) секунд
    // Для AAC-LC: завжди 1024 семпли на фрейм
    dur = time.Duration(1024) * time.Second / time.Duration(self.Config.SampleRate)
    return
}
```

### ✅ Ваш use-case: розрахунок тривалості сегменту

```go
// CalculateSegmentDuration — точний розрахунок тривалості аудіо-сегменту
func CalculateSegmentDuration(packets []av.Packet, codecData aacparser.CodecData) (time.Duration, error) {
    var totalDur time.Duration
    for _, pkt := range packets {
        dur, err := codecData.PacketDuration(pkt.Data)
        if err != nil {
            return 0, err
        }
        totalDur += dur
    }
    return totalDur, nil
}

// Використання для перевірки чи сегмент досяг цільової тривалості:
targetDur := 10 * time.Second
actualDur, _ := CalculateSegmentDuration(currentAudioPackets, aacCodecData)

if actualDur >= targetDur {
    finalizeSegment()
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// aac_transcoder.go — транскодування аудіо у AAC для HLS
type AACTranscoder struct {
    encoder      av.AudioEncoder
    codecData    aacparser.CodecData
    sampleRate   int
    channelLayout av.ChannelLayout
}

func NewAACTranscoder(targetSampleRate int, targetChannels int) (*AACTranscoder, error) {
    // 1. Створення енкодера
    encoder, err := ffmpeg.NewAudioEncoder(av.AAC)
    if err != nil {
        return nil, err
    }
    
    // 2. Налаштування параметрів
    encoder.SetSampleRate(targetSampleRate)
    encoder.SetChannelLayout(av.CH_STEREO)  // або CH_MONO
    encoder.SetSampleFormat(av.FLTP)        // float planar для кращої якості
    encoder.SetBitrate(128000)              // 128 kbps
    
    // 3. Отримання конфігурації енкодера
    codecDataRaw, err := encoder.CodecData()
    if err != nil {
        encoder.Close()
        return nil, err
    }
    
    codecData := codecDataRaw.(aacparser.CodecData)
    
    return &AACTranscoder{
        encoder:       encoder,
        codecData:     codecData,
        sampleRate:    targetSampleRate,
        channelLayout: av.CH_STEREO,
    }, nil
}

// TranscodePacket — транскодування одного аудіо-пакету
func (t *AACTranscoder) TranscodePacket(pkt av.Packet) ([]av.Packet, error) {
    // 1. Декодування вхідного пакету у сирий фрейм
    decoder, err := ffmpeg.NewAudioDecoder(pkt.(av.AudioCodecData))
    if err != nil {
        return nil, err
    }
    defer decoder.Close()
    
    ok, frame, err := decoder.Decode(pkt.Data)
    if err != nil || !ok {
        return nil, err
    }
    
    // 2. Конвертація формату якщо потрібно (resampling)
    if !frame.HasSameFormat(av.AudioFrame{
        SampleRate:    t.sampleRate,
        ChannelLayout: t.channelLayout,
        SampleFormat:  av.FLTP,
    }) {
        resampler, err := ffmpeg.NewAudioResampler(frame, av.AudioFrame{
            SampleRate:    t.sampleRate,
            ChannelLayout: t.channelLayout,
            SampleFormat:  av.FLTP,
        })
        if err != nil {
            return nil, err
        }
        frame, err = resampler.Resample(frame)
        if err != nil {
            resampler.Close()
            return nil, err
        }
        resampler.Close()
    }
    
    // 3. Кодування у AAC
    rawPackets, err := t.encoder.Encode(frame)
    if err != nil {
        return nil, err
    }
    
    // 4. Створення вихідних пакетів з коректними таймінгами
    var result []av.Packet
    baseTime := pkt.Time
    for i, data := range rawPackets {
        dur, _ := t.codecData.PacketDuration(data)
        result = append(result, av.Packet{
            Idx:        pkt.Idx,
            Time:       baseTime + time.Duration(i)*dur,
            Duration:   dur,
            Data:       data,
            IsKeyFrame: false,  // аудіо не має ключових кадрів
        })
    }
    
    return result, nil
}

// GetCodecData — повернення метаданих для заголовка контейнера
func (t *AACTranscoder) GetCodecData() av.CodecData {
    return t.codecData
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"not adts header"** | Неправильний syncword або біти у заголовку | Переконайтеся, що вхідні дані дійсно у форматі ADTS (не raw AAC або інший контейнер) |
| **"channel count invalid"** | `ChannelConfig=0` у заголовку | Перевірте чи конфігурація каналів коректна; для mono використовуйте `ChannelConfig=1` |
| **Таймінги не збігаються** | `PacketDuration()` повертає неправильне значення | Переконайтеся, що використовується правильний `SampleRate` з `MPEG4AudioConfig` |
| **HE-AAC не розпізнається** | `ObjectType=5` (SBR) не обробляється | Додайте підтримку `AOT_SBR` у вашому енкодері/декодері; не всі бібліотеки підтримують HE-AAC |
| **Бітові помилки при записі** | `FlushBits()` не викликано після `WriteBits()` | Завжди викликайте `bw.FlushBits()` після запису бітових полів |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування ADTS-заголовків:

```go
type ADTSCache struct {
    mu      sync.RWMutex
    headers map[string][]byte  // key: "rate_channels_objectType" → pre-filled header
}

func (c *ADTSCache) GetHeader(config aacparser.MPEG4AudioConfig, samples, payloadLen int) []byte {
    key := fmt.Sprintf("%d_%d_%d", config.SampleRate, config.ChannelConfig, config.ObjectType)
    
    c.mu.RLock()
    if header, ok := c.headers[key]; ok {
        // Копіюємо щоб уникнути race condition при модифікації
        result := make([]byte, len(header))
        copy(result, header)
        // Оновлюємо змінні поля (довжина, samples)
        FillADTSHeader(result, config, samples, payloadLen)
        c.mu.RUnlock()
        return result
    }
    c.mu.RUnlock()
    
    // Створення нового заголовка
    header := make([]byte, ADTSHeaderLength)
    FillADTSHeader(header, config, samples, payloadLen)
    
    c.mu.Lock()
    c.headers[key] = header
    c.mu.Unlock()
    
    return header
}
```

### 2. Пакетне кодування для зменшення накладних витрат:

```go
// BatchEncodeAAC — кодування кількох фреймів за один виклик
func BatchEncodeAAC(encoder av.AudioEncoder, frames []av.AudioFrame) ([][]byte, error) {
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

### 3. Моніторинг параметрів транскодування:

```go
type AACMetrics struct {
    ObjectType    prometheus.GaugeVec
    SampleRate    prometheus.GaugeVec
    EncodingTime  prometheus.Histogram
    CompressionRatio prometheus.GaugeVec
}

func (m *AACMetrics) RecordEncoding(inputSize, outputSize int, duration time.Duration, config aacparser.MPEG4AudioConfig, channelID string) {
    m.ObjectType.WithLabelValues(channelID).Set(float64(config.ObjectType))
    m.SampleRate.WithLabelValues(channelID).Set(float64(config.SampleRate))
    m.EncodingTime.WithLabelValues(channelID).Observe(duration.Seconds())
    if inputSize > 0 {
        m.CompressionRatio.WithLabelValues(channelID).Set(float64(outputSize) / float64(inputSize))
    }
}
```

---

## 📋 Чек-лист інтеграції aacparser

```go
// ✅ 1. Парсинг вхідного потоку (ADTS або AudioSpecificConfig)
if isADTSStream {
    config, hdrlen, framelen, samples, err := aacparser.ParseADTSHeader(frame)
    if err != nil { /* handle error */ }
} else {
    config, err := aacparser.ParseMPEG4AudioConfigBytes(configBytes)
    if err != nil { /* handle error */ }
}

// ✅ 2. Валідація конфігурації для HLS
if err := ValidateAACConfig(config); err != nil {
    // Транскодування або помилка
}

// ✅ 3. Створення CodecData для заголовка контейнера
codecData, err := aacparser.NewCodecDataFromMPEG4AudioConfig(config)
if err != nil { /* handle error */ }

// ✅ 4. Розрахунок тривалості пакетів для синхронізації
dur, err := codecData.PacketDuration(packetData)
if err != nil { /* handle error */ }

// ✅ 5. Генерація ADTS-заголовків для вихідного потоку
header := make([]byte, aacparser.ADTSHeaderLength)
aacparser.FillADTSHeader(header, config, 1024, len(rawAACData))
output := append(header, rawAACData...)

// ✅ 6. Закриття ресурсів
if encoder != nil { encoder.Close() }
if decoder != nil { decoder.Close() }

// ✅ 7. Метрики
monitoring.AACSampleRate.Set(float64(config.SampleRate))
monitoring.AACEncodingTime.Observe(encodingDuration.Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [MPEG-4 Audio Standard (ISO/IEC 14496-3)](https://www.iso.org/standard/61246.html) — офіційна специфікація
- 📄 [ADTS Format Details](https://wiki.multimedia.cx/index.php/ADTS) — детальний опис бітової структури
- 📄 [AAC Object Types](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#Audio_Object_Types) — список AOT_* значень
- 💻 [vdk aacparser Package](https://pkg.go.dev/github.com/deepch/vdk/codec/aacparser) — GoDoc documentation
- 🎬 [HLS Audio Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до аудіо у HLS

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Завжди валідуйте `MPEG4AudioConfig` перед використанням** — неправильні параметри можуть зламати HLS-плеєри.
> 2. **Використовуйте `PacketDuration()` для точної синхронізації** — це критично для A/V синхронізації у сегментах.
> 3. **Кешуйте ADTS-заголовки** — генерація бітових полів "на льоту" дорога для високобітрейтних потоків.
> 4. **Тестуйте з різними `ObjectType`** — CCTV камери часто використовують AAC-LC, але іноді HE-AAC (ObjectType=5), який потребує спеціальної підтримки.
> 5. **Моніторьте `CompressionRatio`** — різкі зміни можуть вказувати на проблеми з кодуванням або вхідним сигналом.

Потрібен приклад інтеграції `aacparser.CodecData` з вашим `pubsub.Queue` для розподілу вже транскодованих AAC-пакетів між підписниками? Готовий допомогти! 🚀