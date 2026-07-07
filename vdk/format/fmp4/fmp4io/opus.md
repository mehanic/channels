# 📦 Глибокий розбір: `fmp4io.OpusSampleEntry` — Опис аудіо кодека Opus у fMP4

Цей файл — **реалізація атомів `Opus` (sample entry) та `dOps` (OpusSpecificConfiguration)** для опису аудіо потоку Opus у форматі Fragmented MP4 (fMP4). Він містить метадані аудіо (кількість каналів, частота дискретизації) та специфічну конфігурацію декодера Opus.

---

## 🗺️ Архітектурна схема OpusSampleEntry

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.OpusSampleEntry — Opus Desc │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • OpusSampleEntry — опис аудіо кодека │
│  • OpusSpecificConfiguration (dOps) — конфігурація Opus│
│                                         │
│  🔄 Ієрархія атомів:                    │
│  Opus (OpusSampleEntry)                │
│  └─ dOps (OpusSpecificConfiguration) — конфігурація│
│                                         │
│  📡 Використання:                       │
│  • fMP4 init segment для Opus          │
│  • HLS/DASH streaming з Opus           │
│  • Ініціалізація аудіо декодера        │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. OpusSampleEntry (Opus) — опис аудіо кодека

### 🔧 Структура та призначення:

```go
type OpusSampleEntry struct {
    DataRefIdx       uint16  // індекс посилання на дані (зазвичай 1)
    NumberOfChannels uint16  // ⭐ кількість аудіо каналів
    SampleSize       uint16  // ⭐ розмір семплу у бітах (зазвичай 16)
    CompressionID    uint16  // ID компресії (зазвичай 0)
    SampleRate       float64 // ⭐ частота дискретизації у Hz (fixed-point 16.16)
    Conf             *OpusSpecificConfiguration // ⭐ dOps атом з конфігурацією
    AtomPos                    // offset/size у файлі
}
```

### 🔍 Призначення критичних полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `NumberOfChannels` | `uint16` | **Критично**: кількість аудіо каналів | `2` для стерео, `1` для моно |
| `SampleSize` | `uint16` | **Критично**: розмір семплу у бітах | `16` для 16-бітного аудіо |
| `SampleRate` | `float64` | **Критично**: частота дискретизації у Hz (fixed-point 16.16) | `48000.0` для 48 kHz |
| `Conf` | `*OpusSpecificConfiguration` | **Критично**: dOps атом з конфігурацією декодера | Містить параметри Opus декодера |

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

### ✅ Ваш use-case**: ініціалізація Opus декодера

```go
// InitOpusDecoderFromSampleEntry — створення CodecData з OpusSampleEntry
func InitOpusDecoderFromSampleEntry(opus *fmp4io.OpusSampleEntry) (av.CodecData, error) {
    if opus == nil || opus.Conf == nil {
        return nil, fmt.Errorf("missing Opus config in OpusSampleEntry")
    }
    
    conf := opus.Conf
    
    // Перевірка версії конфігурації
    if conf.Version != 0 {
        return nil, fmt.Errorf("unsupported Opus config version: %d", conf.Version)
    }
    
    // Перевірка ChannelMappingFamily (тільки 0 підтримується у цій реалізації)
    if conf.ChannelMappingFamily != 0 {
        return nil, fmt.Errorf("unsupported ChannelMappingFamily: %d", conf.ChannelMappingFamily)
    }
    
    // Створення конфігурації для декодера
    config := &OpusConfig{
        OutputChannelCount:   conf.OutputChannelCount,
        PreSkip:              conf.PreSkip,
        InputSampleRate:      conf.InputSampleRate,
        OutputGain:           conf.OutputGain,
        ChannelMappingFamily: conf.ChannelMappingFamily,
    }
    
    // Ініціалізація декодера (спрощено)
    return opusparser.NewCodecDataFromOpusConfig(config)
}

type OpusConfig struct {
    OutputChannelCount   uint8
    PreSkip              uint16
    InputSampleRate      uint32
    OutputGain           int16
    ChannelMappingFamily uint8
}

// Використання:
track := findAudioTrack(moov)
if track.Media != nil && track.Media.Info != nil {
    stsd := track.Media.Info.Sample.SampleDesc
    if stsd != nil && stsd.OpusSampleEntry != nil {
        codecData, err := InitOpusDecoderFromSampleEntry(stsd.OpusSampleEntry)
        if err != nil {
            return fmt.Errorf("init Opus decoder: %w", err)
        }
        // codecData готовий до використання у декодері
    }
}
```

---

## 🔑 2. OpusSpecificConfiguration (dOps) — конфігурація декодера Opus

### 🔧 Структура та призначення:

```go
type OpusSpecificConfiguration struct {
    Version              uint8   // ⭐ версія конфігурації (зазвичай 0)
    OutputChannelCount   uint8   // ⭐ кількість вихідних каналів
    PreSkip              uint16  // ⭐ кількість семплів для пропуску на початку
    InputSampleRate      uint32  // ⭐ вхідна частота дискретизації
    OutputGain           int16   // ⭐ вихідне посилення у Q7.8 форматі
    ChannelMappingFamily uint8   // ⭐ сімейство мапування каналів (0 = simple)
    AtomPos
}
```

### 🔍 Призначення критичних полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `Version` | `uint8` | **Критично**: версія формату конфігурації | `0` = єдина підтримувана версія |
| `OutputChannelCount` | `uint8` | **Критично**: кількість вихідних каналів | `2` для стерео, `1` для моно |
| `PreSkip` | `uint16` | **Критично**: кількість семплів для пропуску на початку (для синхронізації) | `312` для 48 kHz = 6.5 ms |
| `InputSampleRate` | `uint32` | **Критично**: вхідна частота дискретизації (може відрізнятися від вихідної) | `48000` для стандартного Opus |
| `OutputGain` | `int16` | **Критично**: вихідне посилення у форматі Q7.8 (fixed-point) | `0` = 0 dB, `256` = +1 dB |
| `ChannelMappingFamily` | `uint8` | **Критично**: сімейство мапування каналів | `0` = simple mapping (тільки підтримується) |

### 🔍 Формат OutputGain (Q7.8 fixed-point):

```
OutputGain зберігається у форматі Q7.8 (фіксована крапка зі знаком):
• Біт 15: знак (0 = позитивний, 1 = негативний)
• Біти 8-14: ціла частина (7 біт)
• Біти 0-7: дробова частина (8 біт)

Конвертація:
    // Читання:
    func GetOutputGain(gain int16) float64 {
        return float64(gain) / 256.0  // ділення на 2^8
    }
    
    // Запис:
    func SetOutputGain(db float64) int16 {
        return int16(db * 256.0)  // множення на 2^8
    }

Приклади:
• 0 = 0.0 dB (нейтральне посилення)
• 256 = +1.0 dB
• -256 = -1.0 dB
• 512 = +2.0 dB
```

### 🔍 ChannelMappingFamily значення:

```
ChannelMappingFamily визначає схему мапування каналів:

• 0 = Simple mapping:
  - 1 канал: моно
  - 2 канали: стерео (L, R)
  - >2 канали: не підтримується у цій реалізації

• 1-254 = Reserved (не використовуються)

• 255 = Vorbis-style channel mapping:
  - Підтримує довільну кількість каналів
  - Вимагає додаткових полів у конфігурації
  - НЕ підтримується у поточній реалізації (помилка при парсингу)

⚠️ Поточна реалізація підтримує ТОЛЬКИ ChannelMappingFamily = 0!
```

### ✅ Ваш use-case**: генерація Opus конфігурації

```go
// GenerateOpusConfig — створення OpusSpecificConfiguration для стандартного аудіо
func GenerateOpusConfig(channels int, sampleRate int, preSkipMs float64) (*fmp4io.OpusSpecificConfiguration, error) {
    if channels < 1 || channels > 2 {
        return nil, fmt.Errorf("unsupported channel count: %d (only 1-2 supported)", channels)
    }
    
    // Opus зазвичай використовує 48000 Hz вхідну частоту
    inputSampleRate := uint32(48000)
    
    // Розрахунок PreSkip у семплах (при 48 kHz)
    preSkip := uint16(preSkipMs * 48.0)  // ms * 48 samples/ms
    
    // OutputGain = 0 dB (нейтральне)
    outputGain := int16(0)
    
    return &fmp4io.OpusSpecificConfiguration{
        Version:              0,
        OutputChannelCount:   uint8(channels),
        PreSkip:              preSkip,
        InputSampleRate:      inputSampleRate,
        OutputGain:           outputGain,
        ChannelMappingFamily: 0,  // simple mapping
    }, nil
}

// Використання:
config, err := GenerateOpusConfig(2, 48000, 6.5)  // стерео, 48 kHz, 6.5 ms pre-skip
if err != nil { /* handle error */ }

// Створення OpusSampleEntry з цією конфігурацією
opus := &fmp4io.OpusSampleEntry{
    DataRefIdx:       1,
    NumberOfChannels: 2,
    SampleSize:       16,
    CompressionID:    0,
    SampleRate:       48000.0,  // буде конвертовано у fixed-point 16.16
    Conf:             config,   // ⭐ dOps атом
}
```

---

## 🔑 3. Marshal/Unmarshal — серіалізація атомів

### 🔧 Основна логіка Marshal для OpusSampleEntry:

```go
func (a OpusSampleEntry) marshal(b []byte) (n int) {
    // 1. Пропуск зарезервованих байт (6 байт)
    n += 6
    
    // 2. Запис основних полів
    pio.PutU16BE(b[n:], a.DataRefIdx); n += 2
    
    // 3. Пропуск зарезервованих байт (8 байт)
    n += 8
    
    // 4. Запис аудіо параметрів
    pio.PutU16BE(b[n:], a.NumberOfChannels); n += 2
    pio.PutU16BE(b[n:], a.SampleSize); n += 2
    
    // 5. Пропуск зарезервованих байт (4 байти)
    n += 4
    
    // 6. Запис SampleRate у fixed-point 16.16 форматі
    PutFixed32(b[n:], a.SampleRate); n += 4
    
    // 7. Рекурсивна серіалізація дочірніх атомів
    if a.Conf != nil {
        n += a.Conf.Marshal(b[n:])  // dOps атом
    }
    return
}
```

### 🔧 Основна логіка Unmarshal для OpusSpecificConfiguration:

```go
func (a *OpusSpecificConfiguration) Unmarshal(b []byte, offset int) (n int, err error) {
    a.setPos(offset, len(b))
    n += 8  // пропуск заголовку атому (size+tag)
    
    // Перевірка мінімальної довжини (11 байт даних)
    if len(b) < 8+11 {
        err = parseErr("OpusSpecificConfiguration", offset, nil)
        return
    }
    
    // Читання полів
    a.Version = b[n]; n++
    
    // ⚠️ Перевірка версії — тільки 0 підтримується
    if a.Version != 0 {
        err = parseErr("unknown version", offset, nil)
        return
    }
    
    a.OutputChannelCount = b[n]; n++
    a.PreSkip = pio.U16BE(b[n:]); n += 2
    a.InputSampleRate = pio.U32BE(b[n:]); n += 4
    a.OutputGain = pio.I16BE(b[n:]); n += 2
    a.ChannelMappingFamily = b[n]; n++
    
    // ⚠️ Перевірка ChannelMappingFamily — тільки 0 підтримується
    if a.ChannelMappingFamily != 0 {
        err = parseErr("ChannelMappingFamily", offset+n, nil)
        return
    }
    
    return
}
```

### ⚠️ Критична проблема: обмежена підтримка конфігурацій

```
У поточній реалізації:
    // Перевірка версії
    if a.Version != 0 {
        err = parseErr("unknown version", offset, nil)
        return
    }
    
    // Перевірка ChannelMappingFamily
    if a.ChannelMappingFamily != 0 {
        err = parseErr("ChannelMappingFamily", offset+n, nil)
        return
    }

Проблема:
• Підтримується ТОЛЬКИ Version = 0 та ChannelMappingFamily = 0
• Файли з іншими конфігураціями не зможуть бути прочитані
• Це обмежує підтримку multi-channel Opus та майбутніх версій формату

✅ Виправлення: документація обмежень або розширення підтримки
    // Документувати обмеження у коментарях
    // АБО реалізувати підтримку додаткових конфігурацій:
    if a.ChannelMappingFamily != 0 {
        // Реалізувати парсинг Vorbis-style mapping
        // ... додатковий код ...
    }
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення Opus треку для fMP4 init segment

```go
// CreateOpusTrack — генерація аудіо треку Opus для fMP4 streaming
func CreateOpusTrack(trackID uint32, channels int, sampleRate int, preSkipMs float64) (*fmp4io.Track, error) {
    // 1. Генерація OpusSpecificConfiguration
    opusConfig, err := GenerateOpusConfig(channels, sampleRate, preSkipMs)
    if err != nil {
        return nil, fmt.Errorf("generate Opus config: %w", err)
    }
    
    // 2. Створення OpusSampleEntry атому
    opus := &fmp4io.OpusSampleEntry{
        DataRefIdx:       1,
        NumberOfChannels: uint16(channels),
        SampleSize:       16,  // 16-бітне аудіо
        CompressionID:    0,
        SampleRate:       float64(sampleRate),  // буде конвертовано у fixed-point 16.16
        Conf:             opusConfig,           // ⭐ dOps атом
    }
    
    // 3. Створення SampleTable з OpusSampleEntry
    stsd := &fmp4io.SampleDesc{
        OpusSampleEntry: opus,
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
    
    // 4. Створення MediaInfo
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
    
    // 5. Створення Media
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
    
    // 6. Створення повного треку
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

### 🔧 Приклад: Парсинг Opus конфігурації для валідації

```go
// ParseOpusConfigFromTrack — витягування параметрів Opus з треку
func ParseOpusConfigFromTrack(track *fmp4io.Track) (*OpusConfig, error) {
    if track == nil || track.Media == nil || track.Media.Info == nil {
        return nil, fmt.Errorf("invalid track structure")
    }
    
    stsd := track.Media.Info.Sample.SampleDesc
    if stsd == nil || stsd.OpusSampleEntry == nil {
        return nil, fmt.Errorf("no OpusSampleEntry found")
    }
    
    opus := stsd.OpusSampleEntry
    if opus.Conf == nil {
        return nil, fmt.Errorf("missing dOps configuration")
    }
    
    conf := opus.Conf
    
    // Перевірка підтримуваних значень
    if conf.Version != 0 {
        return nil, fmt.Errorf("unsupported Opus config version: %d", conf.Version)
    }
    if conf.ChannelMappingFamily != 0 {
        return nil, fmt.Errorf("unsupported ChannelMappingFamily: %d", conf.ChannelMappingFamily)
    }
    
    // Конвертація OutputGain у dB
    outputGainDB := float64(conf.OutputGain) / 256.0
    
    return &OpusConfig{
        Channels:             int(conf.OutputChannelCount),
        PreSkip:              conf.PreSkip,
        InputSampleRate:      conf.InputSampleRate,
        OutputGainDB:         outputGainDB,
        ChannelMappingFamily: conf.ChannelMappingFamily,
    }, nil
}

type OpusConfig struct {
    Channels             int
    PreSkip              uint16
    InputSampleRate      uint32
    OutputGainDB         float64
    ChannelMappingFamily uint8
}

// Використання:
for _, track := range moov.Tracks {
    if track.Media != nil && track.Media.Handler != nil {
        if string(track.Media.Handler.SubType[:]) == "soun" {
            config, err := ParseOpusConfigFromTrack(track)
            if err != nil {
                log.Printf("warning: parse Opus config: %v", err)
                continue
            }
            log.Printf("Opus track: %d channels, %d Hz input, %.1f dB gain", 
                config.Channels, config.InputSampleRate, config.OutputGainDB)
        }
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Помилка при парсингу невідомих версій** | "unknown version" для файлів з Version != 0 | Документуйте обмеження або реалізуйте підтримку нових версій |
| **Непідтримка multi-channel мапування** | "ChannelMappingFamily" помилка для >2 каналів | Обмежте підтримку 1-2 каналами або реалізуйте Vorbis-style mapping |
| **Невірне декодування OutputGain** | Неправильна гучність аудіо | Переконайтеся що конвертація Q7.8 → float коректна: `gain/256.0` |
| **Переповнення fixed-point 16.16** | Невірні значення частоти дискретизації | Перевіряйте діапазон перед конвертацією: `if f < 0 || f > 65535` |
| **Відсутній dOps атом** | Неможливість ініціалізації декодера | Перевіряйте `if opus.Conf != nil` перед використанням |

---

## ⚡ Оптимізації для high-performance streaming

### 1. Кешування серіалізованих dOps атомів:

```go
var dopsCache = sync.Map{}  // map[string][]byte

func GetCachedDOPS(key string) ([]byte, error) {
    if cached, ok := dopsCache.Load(key); ok {
        return cached.([]byte), nil
    }
    
    // Генерація dOps (спрощено)
    dops := createDOPS(key)  // helper function
    blob, err := dops.Marshal()
    if err != nil {
        return nil, err
    }
    
    dopsCache.Store(key, blob)
    return blob, nil
}
```

### 2. Попередня аллокація буферів для Marshal:

```go
// PreallocateOpusBuffer — виділення місця для серіалізації заздалегідь
func PreallocateOpusBuffer(opus *fmp4io.OpusSampleEntry) []byte {
    estimatedSize := opus.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateOpusBuffer(opus)
n := opus.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type OpusMetrics struct {
    TracksParsed prometheus.CounterVec
    ParseLatency prometheus.HistogramVec
    ConfigSizes  prometheus.HistogramVec
    ParseErrors  prometheus.CounterVec
}

func (m *OpusMetrics) RecordParse(configSize int, duration time.Duration, err error) {
    m.TracksParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    m.ConfigSizes.Observe(float64(configSize))
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання OpusSampleEntry/OpusSpecificConfiguration

```go
// ✅ 1. Перевірка меж буфера перед доступом
if len(b) < n+2 {
    err = parseErr("NumberOfChannels", n+offset, err)
    return
}
a.NumberOfChannels = pio.U16BE(b[n:])
n += 2

// ✅ 2. Валідація діапазону для fixed-point конвертації
if sampleRate < 8000 || sampleRate > 192000 {
    return fmt.Errorf("unsupported sample rate: %f", sampleRate)
}

// ✅ 3. Перевірка підтримуваних значень версії та ChannelMappingFamily
if conf.Version != 0 {
    return fmt.Errorf("unsupported Opus config version: %d", conf.Version)
}
if conf.ChannelMappingFamily != 0 {
    return fmt.Errorf("unsupported ChannelMappingFamily: %d (only 0 supported)", conf.ChannelMappingFamily)
}

// ✅ 4. Безпечне декодування OutputGain (Q7.8 format)
outputGainDB := float64(conf.OutputGain) / 256.0
if outputGainDB < -128 || outputGainDB > 127 {  // діапазон 7-бітного знакового цілого
    log.Printf("warning: unusual OutputGain: %.1f dB", outputGainDB)
}

// ✅ 5. Логування з контекстом для дебагу
log.Printf("Parsed OpusSampleEntry: channels=%d, sampleRate=%.0f, dOps=%v", 
    opus.NumberOfChannels, opus.SampleRate, opus.Conf != nil)

// ✅ 6. Метрики для моніторингу
metrics.RecordParse(len(confData), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [RFC 7845: Ogg Encapsulation for the Opus Audio Codec](https://datatracker.ietf.org/doc/html/rfc7845) — офіційний стандарт для Opus
- 📄 [Opus in MP4 Specification](https://opus-codec.org/docs/opus_in_isobmff.html) — специфікація Opus у ISO BMFF
- 📄 [OpusSpecificConfiguration Format](https://wiki.xiph.org/Opus_in_MPEG#OpusSpecificConfiguration) — детальний опис структури dOps
- 🧪 [Fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) — теорія форматів 16.16 та Q7.8
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Документуйте обмеження підтримки конфігурацій** — тільки Version=0 та ChannelMappingFamily=0.
> 2. **Валідуйте діапазон `SampleRate` перед конвертацією** — уникнення невірних значень частоти дискретизації.
> 3. **Перевіряйте наявність `dOps` атому перед використанням** — без нього неможливо ініціалізувати декодер.
> 4. **Безпечно декодуйте `OutputGain` у форматі Q7.8** — уникнення невірної гучності аудіо.
> 5. **Кешуйте серіалізовані dOps для повторного використання** — прискорення генерації init segment.

Потрібен приклад реалізації повного циклу створення/парсингу Opus треку з підтримкою різних конфігурацій каналів, або інтеграція `fmp4io.OpusSampleEntry` з вашим `mse.Muxer` для стрімінгу аудіо через WebSocket? Готовий допомогти! 🚀