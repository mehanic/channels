# 📦 Глибокий розбір: `fmp4io.MP4ADesc` — Опис аудіо кодека AAC у fMP4

Цей файл — **реалізація атомів `mp4a` (MPEG-4 Audio Description) та `esds` (Elementary Stream Descriptor)** для опису аудіо потоку AAC у форматі Fragmented MP4 (fMP4). Він містить метадані аудіо (кількість каналів, частота дискретизації) та посилання на `esds` атом з конфігурацією декодера.

---

## 🗺️ Архітектурна схема MP4ADesc

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.MP4ADesc — AAC Audio Desc   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • MP4ADesc — опис аудіо кодека        │
│  • ElemStreamDesc (esds) — MPEG-4 ES Descriptor│
│  • esio.StreamDescriptor — конфігурація декодера│
│                                         │
│  🔄 Ієрархія атомів:                    │
│  mp4a (MP4ADesc)                       │
│  └─ esds (ElemStreamDesc) — ES Descriptor│
│      └─ esio.StreamDescriptor — конфігурація│
│          └─ DecoderConfigDescriptor    │
│              └─ DecoderSpecificInfo ⭐ │
│                                         │
│  📡 Використання:                       │
│  • fMP4 init segment для AAC           │
│  • HLS/DASH streaming з AAC            │
│  • Ініціалізація аудіо декодера        │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. MP4ADesc (mp4a) — опис аудіо кодека

### 🔧 Структура та призначення:

```go
type MP4ADesc struct {
    DataRefIdx       int16       // індекс посилання на дані (зазвичай 1)
    Version          int16       // версія формату (зазвичай 0)
    RevisionLevel    int16       // ревізія кодека
    Vendor           int32       // ідентифікатор вендора
    NumberOfChannels int16       // ⭐ кількість аудіо каналів
    SampleSize       int16       // ⭐ розмір семплу у бітах (напр. 16)
    CompressionId    int16       // ID компресії (зазвичай 0)
    SampleRate       float64     // ⭐ частота дискретизації у Hz (fixed-point 16.16)
    Conf             *ElemStreamDesc // ⭐ esds атом з конфігурацією декодера
    Unknowns         []Atom      // невідомі дочірні атоми для сумісності
    AtomPos                      // offset/size у файлі
}
```

### 🔍 Призначення критичних полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `NumberOfChannels` | `int16` | **Критично**: кількість аудіо каналів | `2` для стерео, `6` для 5.1 surround |
| `SampleSize` | `int16` | **Критично**: розмір семплу у бітах | `16` для 16-бітного аудіо |
| `SampleRate` | `float64` | **Критично**: частота дискретизації у Hz (fixed-point 16.16) | `48000.0` для 48 kHz |
| `Conf` | `*ElemStreamDesc` | **Критично**: esds атом з MPEG-4 Stream Descriptor | Містить AudioSpecificConfig для AAC |

### 🔍 Формат fixed-point 16.16 для SampleRate:

```
SampleRate зберігається у форматі 16.16 (фіксована крапка):
• Біти 0-15: дробова частина
• Біти 16-31: ціла частина

Конвертація:
    // Запис:
    func PutFixed32(b []byte, f float64) {
        intpart, fracpart := math.Modf(f)
        pio.PutU16BE(b[0:2], uint16(intpart))           // ціла частина
        pio.PutU16BE(b[2:4], uint16(fracpart*65536.0))  // дробова * 2^16
    }
    
    // Читання:
    func GetFixed32(b []byte) float64 {
        return float64(pio.U16BE(b[0:2])) + float64(pio.U16BE(b[2:4]))/65536.0
    }

Приклади:
• 48000.0 = 0x0000BB80 0000 (ціла=48000, дробова=0)
• 44100.0 = 0x0000AC44 0000
• 22050.5 = 0x00005622 8000 (дробова 0.5 = 32768/65536)
```

### ✅ Ваш use-case**: ініціалізація AAC декодера

```go
// InitAACDecoderFromMP4A — створення CodecData з MP4ADesc
func InitAACDecoderFromMP4A(mp4a *fmp4io.MP4ADesc) (av.CodecData, error) {
    if mp4a == nil || mp4a.Conf == nil || mp4a.Conf.StreamDescriptor == nil {
        return nil, fmt.Errorf("missing AAC config in MP4ADesc")
    }
    
    // Отримання DecoderSpecificInfo з esds
    desc := mp4a.Conf.StreamDescriptor
    if desc.DecoderConfig == nil || desc.DecoderConfig.DecSpecificInfo == nil {
        return nil, fmt.Errorf("missing DecoderSpecificInfo in esds")
    }
    
    // esds.DecoderConfig.DecSpecificInfo містить MPEG-4 AudioSpecificConfig
    // Формат: [audioObjectType:5][samplingFrequencyIndex:4][channelConfiguration:4]...
    return aacparser.NewCodecDataFromMPEG4AudioConfigBytes(
        desc.DecoderConfig.DecSpecificInfo,
    )
}

// Використання:
track := findAudioTrack(moov)
if track.Media != nil && track.Media.Info != nil {
    stsd := track.Media.Info.Sample.SampleDesc
    if stsd != nil && stsd.MP4ADesc != nil {
        codecData, err := InitAACDecoderFromMP4A(stsd.MP4ADesc)
        if err != nil {
            return fmt.Errorf("init AAC decoder: %w", err)
        }
        // codecData готовий до використання у декодері
    }
}
```

---

## 🔑 2. ElemStreamDesc (esds) — MPEG-4 Stream Descriptor

### 🔧 Структура та призначення:

```go
type ElemStreamDesc struct {
    StreamDescriptor *esio.StreamDescriptor  // ⭐ MPEG-4 ES Descriptor
    AtomPos
}
```

### 🔍 Призначення esds атому:

```
esds (Elementary Stream Descriptor) містить конфігурацію потоку MPEG-4:

• Використовується для опису аудіо (AAC) та відео (H.264) потоків
• Містить ES_Descriptor з параметрами декодера
• Критичний для ініціалізації декодера на клієнті

Структура:
  esds (ElemStreamDesc)
  ├─ version/flags: 4 байти (зазвичай 0)
  └─ ES_Descriptor (esio.StreamDescriptor)
     ├─ ES_ID: 2 bytes (ідентифікатор потоку)
     ├─ flags: 1 byte (опціональні поля)
     ├─ DecoderConfigDescriptor
     │  ├─ objectTypeIndication: 1 byte (0x40 = AAC)
     │  ├─ streamType: 1 byte (0x15 = AudioStream)
     │  ├─ bufferSizeDB: 3 bytes
     │  ├─ maxBitrate: 4 bytes
     │  ├─ avgBitrate: 4 bytes
     │  └─ DecoderSpecificInfo ⭐
     │     └─ AudioSpecificConfig: 2+ bytes ⭐
     └─ SLConfigDescriptor (опціонально)
```

### 🔍 AudioSpecificConfig для AAC:

```
AudioSpecificConfig — це бітова структура конфігурації AAC декодера:

Біти 0-4: audioObjectType (5 біт)
• 2 = AAC LC (Low Complexity) — найпоширеніший
• 5 = HE-AAC (SBR)
• 29 = HE-AAC v2 (SBR + PS)

Біти 5-8: samplingFrequencyIndex (4 біти)
• 3 = 48000 Hz, 4 = 44100 Hz, 5 = 32000 Hz, тощо
• 15 = explicit frequency (наступні 24 біти)

Біти 9-12: channelConfiguration (4 біти)
• 1 = mono, 2 = stereo, 6 = 5.1 surround

Біти 13+: додаткові параметри (залежать від audioObjectType)

Приклад для AAC-LC, 48kHz, stereo:
  0x1190 = [00010][0011][0010] = [2][3][2]
  • audioObjectType = 2 (AAC LC)
  • samplingFrequencyIndex = 3 (48000 Hz)
  • channelConfiguration = 2 (stereo)
```

### ✅ Ваш use-case**: генерація AudioSpecificConfig

```go
// GenerateAACConfig — створення 2-байтового AudioSpecificConfig для AAC-LC
func GenerateAACConfig(sampleRate int, channels int) ([]byte, error) {
    // Мапінг частоти дискретизації у index
    freqIndex := map[int]int{
        96000: 0, 88200: 1, 64000: 2, 48000: 3,
        44100: 4, 32000: 5, 24000: 6, 22050: 7,
        16000: 8, 12000: 9, 11025: 10, 8000: 11,
    }
    
    fi, ok := freqIndex[sampleRate]
    if !ok {
        return nil, fmt.Errorf("unsupported sample rate: %d", sampleRate)
    }
    
    if channels < 1 || channels > 8 {
        return nil, fmt.Errorf("invalid channel count: %d", channels)
    }
    
    // Бітова упаковка: [audioObjectType:5][freqIndex:4][channels:4]
    // AAC LC = objectType 2
    config := uint16(2<<11) | uint16(fi<<7) | uint16(channels<<3)
    
    return []byte{byte(config >> 8), byte(config & 0xFF)}, nil
}

// Використання:
config, err := GenerateAACConfig(48000, 2)  // AAC-LC, 48kHz, stereo
if err != nil { /* handle error */ }

// Створення esds атому з цією конфігурацією
esds := &fmp4io.ElemStreamDesc{
    StreamDescriptor: &esio.StreamDescriptor{
        ESID: 1,
        DecoderConfig: &esio.DecoderConfigDescriptor{
            ObjectType: 0x40,        // AAC
            StreamType: 0x15,        // AudioStream
            BufferSizeDB: 0,
            MaxBitrate: 200000,      // 200 kbps
            AvgBitrate: 128000,      // 128 kbps
            DecSpecificInfo: config, // AudioSpecificConfig
        },
        SLConfig: &esio.SLConfigDescriptor{
            Predefined: esio.SLConfigMP4,
        },
    },
}
```

---

## 🔑 3. Marshal/Unmarshal — серіалізація атомів

### 🔧 Основна логіка Marshal для MP4ADesc:

```go
func (a MP4ADesc) marshal(b []byte) (n int) {
    // 1. Пропуск зарезервованих байт (6 байт)
    n += 6
    
    // 2. Запис основних полів
    pio.PutI16BE(b[n:], a.DataRefIdx); n += 2
    pio.PutI16BE(b[n:], a.Version); n += 2
    pio.PutI16BE(b[n:], a.RevisionLevel); n += 2
    pio.PutI32BE(b[n:], a.Vendor); n += 4
    pio.PutI16BE(b[n:], a.NumberOfChannels); n += 2
    pio.PutI16BE(b[n:], a.SampleSize); n += 2
    pio.PutI16BE(b[n:], a.CompressionId); n += 2
    
    // 3. Пропуск зарезервованих байт (2 байти)
    n += 2
    
    // 4. Запис SampleRate у fixed-point 16.16 форматі
    PutFixed32(b[n:], a.SampleRate); n += 4
    
    // 5. Рекурсивна серіалізація дочірніх атомів
    if a.Conf != nil {
        n += a.Conf.Marshal(b[n:])  // esds атом
    }
    for _, atom := range a.Unknowns {
        n += atom.Marshal(b[n:])
    }
    return
}
```

### 🔧 Основна логіка Unmarshal для ElemStreamDesc:

```go
func (a *ElemStreamDesc) Unmarshal(b []byte, offset int) (n int, err error) {
    if len(b) < n+12 {  // 8 (header) + 4 (version/flags)
        err = parseErr("hdr", offset+n, err)
        return
    }
    a.AtomPos.setPos(offset, len(b))
    
    // Пропуск заголовку та version/flags
    var remainder []byte
    a.StreamDescriptor, remainder, err = esio.ParseStreamDescriptor(b[12:])
    
    // Розрахунок прочитаної кількості байт
    n += len(b) - len(remainder)
    return
}
```

### ⚠️ Критична проблема: panic при помилці маршалінгу

```
У поточному коді:
    blob, err := a.StreamDescriptor.Marshal()
    if err != nil {
        panic(err)  // ← Паніка при помилці!
    }

Проблема:
• Паніка у виробничому коді неприпустима
• Неможливість коректної обробки помилок
• Може призвести до crash сервера

✅ Виправлення: повернення помилки замість panic
    func (a ElemStreamDesc) Marshal(b []byte) (n int) {
        pio.PutU32BE(b[4:], uint32(ESDS))
        n += 8
        pio.PutU32BE(b[n:], 0) // Version
        n += 4
        
        blob, err := a.StreamDescriptor.Marshal()
        if err != nil {
            // Логування помилки та повернення 0 (або інша обробка)
            log.Printf("error marshaling StreamDescriptor: %v", err)
            return 0
        }
        copy(b[n:], blob)
        n += len(blob)
        pio.PutU32BE(b[0:], uint32(n))
        return n
    }
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення AAC треку для fMP4 init segment

```go
// CreateAACTrack — генерація аудіо треку для fMP4 streaming
func CreateAACTrack(trackID uint32, sampleRate int, channels int, bitrate int) (*fmp4io.Track, error) {
    // 1. Генерація AudioSpecificConfig
    audioConfig, err := GenerateAACConfig(sampleRate, channels)
    if err != nil {
        return nil, fmt.Errorf("generate AAC config: %w", err)
    }
    
    // 2. Створення esds атому
    esds := &fmp4io.ElemStreamDesc{
        StreamDescriptor: &esio.StreamDescriptor{
            ESID: uint16(trackID),
            DecoderConfig: &esio.DecoderConfigDescriptor{
                ObjectType: 0x40,        // AAC
                StreamType: 0x15,        // AudioStream
                BufferSizeDB: 0,
                MaxBitrate: uint32(bitrate * 2),  // peak bitrate
                AvgBitrate: uint32(bitrate),      // average bitrate
                DecSpecificInfo: audioConfig,     // ⭐ AudioSpecificConfig
            },
            SLConfig: &esio.SLConfigDescriptor{
                Predefined: esio.SLConfigMP4,  // стандартна конфігурація
            },
        },
    }
    
    // 3. Створення MP4ADesc атому
    mp4a := &fmp4io.MP4ADesc{
        DataRefIdx:       1,
        Version:          0,
        RevisionLevel:    0,
        Vendor:           0,
        NumberOfChannels: int16(channels),
        SampleSize:       16,  // 16-бітне аудіо
        CompressionId:    0,
        SampleRate:       float64(sampleRate),  // буде конвертовано у fixed-point 16.16
        Conf:             esds,                 // ⭐ esds атом
    }
    
    // 4. Створення SampleTable з MP4ADesc
    stsd := &fmp4io.SampleDesc{
        MP4ADesc: mp4a,
    }
    
    stbl := &fmp4io.SampleTable{
        SampleDesc:   stsd,
        TimeToSample: &fmp4io.TimeToSample{},  // буде заповнено при записі
        SampleToChunk: &fmp4io.SampleToChunk{
            Entries: []fmp4io.SampleToChunkEntry{
                {FirstChunk: 1, SamplesPerChunk: 1, SampleDescId: 1},
            },
        },
        SampleSize:  &fmp4io.SampleSize{},
        ChunkOffset: &fmp4io.ChunkOffset{},
    }
    
    // 5. Створення MediaInfo
    minf := &fmp4io.MediaInfo{
        Sound: &fmp4io.SoundMediaInfo{
            Version: 0,
            Flags:   0,
            Balance: 0,  // центр (0.0 у 8.8 fixed-point)
        },
        Data: &fmp4io.DataInfo{
            Refer: &fmp4io.DataRefer{
                Version: 0,
                Flags:   0x000001,  // self-reference
                Url: &fmp4io.DataReferUrl{
                    Version: 0,
                    Flags:   0x000001,
                },
            },
        },
        Sample: stbl,
    }
    
    // 6. Створення Media
    media := &fmp4io.Media{
        Header: &fmp4io.MediaHeader{
            Version:   0,
            Flags:     0,
            TimeScale: uint32(sampleRate),  // timeScale = sampleRate для аудіо
            Duration:  0,  // буде оновлено
            Language:  21956,  // 'und' = undefined
            Quality:   0,
        },
        Handler: &fmp4io.HandlerRefer{
            Version: 0,
            Flags:   0,
            SubType: [4]byte{'s', 'o', 'u', 'n'},  // 'soun' = audio track
            Name:    []byte("Sound Handler\x00"),
        },
        Info: minf,
    }
    
    // 7. Створення повного треку
    track := &fmp4io.Track{
        Header: &fmp4io.TrackHeader{
            Version:     0,
            Flags:       0x0003,  // enabled | in-movie
            TrackID:     trackID,
            Duration:    0,  // буде оновлено
            Layer:       0,
            AlternateGroup: 0,
            Volume:      1.0,  // максимальна гучність
            Matrix:      [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
        },
        Media: media,
    }
    
    return track, nil
}
```

### 🔧 Приклад: Парсинг AAC конфігурації для валідації

```go
// ParseAACConfigFromTrack — витягування параметрів AAC з треку
func ParseAACConfigFromTrack(track *fmp4io.Track) (*AACConfig, error) {
    if track == nil || track.Media == nil || track.Media.Info == nil {
        return nil, fmt.Errorf("invalid track structure")
    }
    
    stsd := track.Media.Info.Sample.SampleDesc
    if stsd == nil || stsd.MP4ADesc == nil {
        return nil, fmt.Errorf("no MP4ADesc found")
    }
    
    mp4a := stsd.MP4ADesc
    config := &AACConfig{
        Channels:   int(mp4a.NumberOfChannels),
        SampleSize: int(mp4a.SampleSize),
        SampleRate: mp4a.SampleRate,
    }
    
    // Парсинг AudioSpecificConfig з esds
    if mp4a.Conf != nil && mp4a.Conf.StreamDescriptor != nil {
        desc := mp4a.Conf.StreamDescriptor
        if desc.DecoderConfig != nil && desc.DecoderConfig.DecSpecificInfo != nil {
            asc := desc.DecoderConfig.DecSpecificInfo
            if len(asc) >= 2 {
                // Декодування перших 2 байт AudioSpecificConfig
                config.AudioObjectType = (asc[0] >> 3) & 0x1F
                config.SamplingFrequencyIndex = ((asc[0] & 0x07) << 1) | (asc[1] >> 7)
                config.ChannelConfiguration = (asc[1] >> 3) & 0x0F
                
                // Мапінг index у частоту
                freqMap := []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000}
                if config.SamplingFrequencyIndex < len(freqMap) {
                    config.SamplingFrequency = freqMap[config.SamplingFrequencyIndex]
                }
            }
        }
    }
    
    return config, nil
}

type AACConfig struct {
    Channels              int
    SampleSize            int
    SampleRate            float64
    AudioObjectType       uint8
    SamplingFrequencyIndex uint8
    SamplingFrequency     int
    ChannelConfiguration  uint8
}

// Використання:
for _, track := range moov.Tracks {
    if track.Media != nil && track.Media.Handler != nil {
        if string(track.Media.Handler.SubType[:]) == "soun" {
            config, err := ParseAACConfigFromTrack(track)
            if err != nil {
                log.Printf("warning: parse AAC config: %v", err)
                continue
            }
            log.Printf("AAC track: %d channels, %.0f Hz, %d-bit", 
                config.Channels, config.SampleRate, config.SampleSize)
        }
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при помилці маршалінгу** | Crash сервера при некоректній конфігурації | Замініть `panic(err)` на повернення помилки або логування |
| **Невірне декодування AudioSpecificConfig** | Неправильні параметри декодера | Переконайтеся що бітові операції коректні для 5+4+4 біт формату |
| **Переповнення fixed-point 16.16** | Невірні значення частоти дискретизації | Перевіряйте діапазон перед конвертацією: `if f < 0 || f > 65535` |
| **Відсутній esds атом** | Неможливість ініціалізації декодера | Перевіряйте `if mp4a.Conf != nil` перед використанням |
| **Некоректний SampleRate** | Аудіо відтворюється з неправильною швидкістю | Переконайтеся що SampleRate конвертовано у fixed-point 16.16 коректно |

---

## ⚡ Оптимізації для high-performance streaming

### 1. Кешування серіалізованих esds атомів:

```go
var esdsCache = sync.Map{}  // map[string][]byte

func GetCachedESDS(key string) ([]byte, error) {
    if cached, ok := esdsCache.Load(key); ok {
        return cached.([]byte), nil
    }
    
    // Генерація esds (спрощено)
    esds := createESDS(key)  // helper function
    blob, err := esds.Marshal()
    if err != nil {
        return nil, err
    }
    
    esdsCache.Store(key, blob)
    return blob, nil
}
```

### 2. Попередня аллокація буферів для Marshal:

```go
// PreallocateMP4ABuffer — виділення місця для серіалізації заздалегідь
func PreallocateMP4ABuffer(mp4a *fmp4io.MP4ADesc) []byte {
    estimatedSize := mp4a.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateMP4ABuffer(mp4a)
n := mp4a.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type AACMetrics struct {
    TracksParsed prometheus.CounterVec
    ParseLatency prometheus.HistogramVec
    ConfigSizes  prometheus.HistogramVec
    ParseErrors  prometheus.CounterVec
}

func (m *AACMetrics) RecordParse(configSize int, duration time.Duration, err error) {
    m.TracksParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    m.ConfigSizes.Observe(float64(configSize))
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання MP4ADesc/ElemStreamDesc

```go
// ✅ 1. Перевірка меж буфера перед доступом
if len(b) < n+2 {
    err = parseErr("NumberOfChannels", n+offset, err)
    return
}
a.NumberOfChannels = pio.I16BE(b[n:])
n += 2

// ✅ 2. Валідація діапазону для fixed-point конвертації
if sampleRate < 8000 || sampleRate > 192000 {
    return fmt.Errorf("unsupported sample rate: %f", sampleRate)
}

// ✅ 3. Перевірка наявності esds перед використанням
if mp4a.Conf == nil || mp4a.Conf.StreamDescriptor == nil {
    return fmt.Errorf("missing esds atom in MP4ADesc")
}

// ✅ 4. Безпечне декодування AudioSpecificConfig
if len(asc) < 2 {
    return fmt.Errorf("AudioSpecificConfig too short: %d bytes", len(asc))
}
audioObjectType := (asc[0] >> 3) & 0x1F
if audioObjectType != 2 {  // AAC LC
    log.Printf("warning: unsupported audio object type: %d", audioObjectType)
}

// ✅ 5. Логування з контекстом для дебагу
log.Printf("Parsed MP4ADesc: channels=%d, sampleRate=%.0f, esds=%v", 
    mp4a.NumberOfChannels, mp4a.SampleRate, mp4a.Conf != nil)

// ✅ 6. Метрики для моніторингу
metrics.RecordParse(len(asc), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-3:2020 (MPEG-4 Audio)](https://www.iso.org/standard/79428.html) — офіційний стандарт для AAC
- 📄 [MPEG-4 AudioSpecificConfig Format](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — детальний опис структури
- 📄 [ESDS Atom Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap3/qtff3.html#//apple_ref/doc/uid/TP40000939-CH205-SW1) — Apple documentation
- 🧪 [Fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) — теорія формату 16.16
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Замініть `panic(err)` на повернення помилки** — уникнення crash сервера при некоректній конфігурації.
> 2. **Валідуйте діапазон `SampleRate` перед конвертацією** — уникнення невірних значень частоти дискретизації.
> 3. **Перевіряйте наявність `esds` атому перед використанням** — без нього неможливо ініціалізувати декодер.
> 4. **Безпечно декодуйте `AudioSpecificConfig`** — уникнення невірних параметрів декодера.
> 5. **Кешуйте серіалізовані esds для повторного використання** — прискорення генерації init segment.

Потрібен приклад реалізації повного циклу створення/парсингу AAC треку з підтримкою різних конфігурацій (LC, HE-AAC, тощо), або інтеграція `fmp4io.MP4ADesc` з вашим `mse.Muxer` для стрімінгу аудіо через WebSocket? Готовий допомогти! 🚀