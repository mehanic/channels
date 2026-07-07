# 🎵 Глибокий розбір: opusparser — Парсер Opus аудіо-потоків

Цей файл — **реалізація парсингу та обробки аудіо-пакетів Opus** згідно зі стандартом RFC 6716. Opus — це сучасний аудіо-кодек з низькою затримкою, високою якістю та адаптивним бітрейтом, ідеальний для VoIP, стрімінгу та real-time комунікацій.

Розберемо архітектуру, бітові формати та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема opusparser

```
┌────────────────────────────────────────┐
│ 📦 opusparser — Opus Audio Handling    │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • CodecData — метадані Opus потоку    │
│  • PacketDuration() — розрахунок тривалості│
│  • Channels() — детекція кількості каналів│
│  • opusFrameTimes — таблиця тривалостей│
│                                         │
│  📊 Opus TOC (Table of Contents) байт: │
│  • Біти 7-3: конфігурація (кодек/частота)│
│  • Біт 2: stereo/mono                  │
│  • Біти 1-0: тип пакування фреймів     │
│                                         │
│  🔄 Типи пакування:                     │
│  • code=0: один фрейм                  │
│  • code=1,2: два фрейми                │
│  • code=3: N фреймів (з лічильником)   │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Opus TOC (Table of Contents) — бітова структура

### Формат першого байта пакету:

```
Біти:  7   6   5   4   3   2   1   0
       └─── conf ───┘ s  └─code─┘

де:
  • conf (біти 7-3): 5-бітна конфігурація (кодек + частота)
  • s (біт 2): stereo flag (1=стерео, 0=моно)
  • code (біти 1-0): тип пакування фреймів (0-3)
```

### 🔍 Деталі полів:

| Поле | Біти | Значення | Приклад |
|------|------|----------|---------|
| `config` | 7-3 | 0-31: індекс у `opusFrameTimes[]` | `config=4` → SILK MB, 20ms |
| `stereo` | 2 | 0=mono, 1=stereo | `(toc & 0x4) != 0` → стерео |
| `code` | 1-0 | 0=1 фрейм, 1-2=2 фрейми, 3=N фреймів | `code=0` → один фрейм |

### ✅ Ваш use-case: детекція параметрів пакету

```go
// ParseOpusHeader — витягування параметрів з першого байта
func ParseOpusHeader(pkt []byte) (config uint, stereo bool, code uint, err error) {
    if len(pkt) < 1 {
        return 0, false, 0, errors.New("empty opus packet")
    }
    
    toc := pkt[0]
    config = uint(toc >> 3)           // біти 7-3
    stereo = (toc & 0x4) != 0         // біт 2
    code = uint(toc & 0x3)            // біти 1-0
    
    return config, stereo, code, nil
}

// Використання для логування/метрик:
config, stereo, code, _ := ParseOpusHeader(packet)
log.Printf("Opus packet: config=%d, stereo=%v, code=%d", config, stereo, code)
```

---

## 📊 2. opusFrameTimes — таблиця тривалостей фреймів

### Структура таблиці (32 елементи):

```go
var opusFrameTimes = []time.Duration{
    // SILK NB (Narrowband, 8kHz) — індекси 0-3
    10 * time.Millisecond,
    20 * time.Millisecond,
    40 * time.Millisecond,
    60 * time.Millisecond,
    
    // SILK MB (Medium-band, 12kHz) — індекси 4-7
    10 * time.Millisecond,
    20 * time.Millisecond,
    40 * time.Millisecond,
    60 * time.Millisecond,
    
    // SILK WB (Wideband, 16kHz) — індекси 8-11
    10 * time.Millisecond,
    20 * time.Millisecond,
    40 * time.Millisecond,
    60 * time.Millisecond,
    
    // Hybrid SWB/FB (Super/Wideband, 32/48kHz) — індекси 12-15
    10 * time.Millisecond,  // SWB
    20 * time.Millisecond,  // SWB
    10 * time.Millisecond,  // FB
    20 * time.Millisecond,  // FB
    
    // CELT NB/MB/WB/SWB/FB — індекси 16-31
    // (коротші фрейми для низької затримки)
    2500 * time.Microsecond,  // 2.5ms
    5 * time.Millisecond,
    10 * time.Millisecond,
    20 * time.Millisecond,
    // ... повтор для різних смуг
}
```

### 🔍 Як використовувати `config` як індекс:

```
Приклад: toc = 0x48 (бінарно: 01001000)
  • config = toc >> 3 = 01001 = 9 (десяткове)
  • opusFrameTimes[9] = 20ms (SILK WB)
  
Приклад: toc = 0x1A (бінарно: 00011010)
  • config = toc >> 3 = 00011 = 3 (десяткове)
  • opusFrameTimes[3] = 60ms (SILK NB)
```

### ✅ Ваш use-case: розрахунок тривалості пакету

```go
// PacketDuration — основна функція розрахунку
func PacketDuration(pkt []byte) (time.Duration, error) {
    if len(pkt) < 1 {
        return 0, errors.New("empty opus packet")
    }
    
    toc := pkt[0]
    config := toc >> 3      // індекс у таблиці
    code := toc & 0x3       // тип пакування
    
    numFr := 0
    switch code {
    case 0:  // один фрейм
        if len(pkt) > 1 {
            numFr = 1
        }
    case 1, 2:  // два фрейми
        if len(pkt) > 2 {
            numFr = 2
        }
    case 3:  // N фреймів (лічильник у другому байті)
        if len(pkt) < 2 {
            return 0, errors.New("invalid opus packet")
        }
        numFr = int(pkt[1] & 0x3f)  // біти 0-5 другого байта
    }
    
    // Тривалість = кількість фреймів × тривалість одного фрейму
    return time.Duration(numFr) * opusFrameTimes[config], nil
}
```

### 🔢 Приклади розрахунку:

```
Приклад 1: Один фрейм, SILK WB 20ms
  pkt[0] = 0x48 (config=9, stereo=0, code=0)
  numFr = 1
  duration = 1 × opusFrameTimes[9] = 1 × 20ms = 20ms

Приклад 2: Два фрейми, CELT FB 10ms
  pkt[0] = 0x39 (config=7, stereo=1, code=1)
  numFr = 2
  duration = 2 × opusFrameTimes[7] = 2 × 10ms = 20ms

Приклад 3: 5 фреймів, SILK NB 40ms (code=3)
  pkt[0] = 0x03 (config=0, stereo=0, code=3)
  pkt[1] = 0x05 (numFr = 5 & 0x3f = 5)
  duration = 5 × opusFrameTimes[0] = 5 × 40ms = 200ms
```

---

## 📦 3. CodecData — інтеграція з av.CodecData

### Структура та методи:

```go
type CodecData struct {
    Channels int  // 1=mono, 2=stereo
}

// Реалізація інтерфейсу av.AudioCodecData:
func (d CodecData) Type() av.CodecType {
    return av.OPUS  // завжди Opus
}

func (d CodecData) SampleRate() int {
    return 48000  // Opus завжди 48kHz internally
}

func (d CodecData) ChannelLayout() av.ChannelLayout {
    switch d.Channels {
    case 1: return av.CH_MONO
    case 2: return av.CH_STEREO
    default: panic("not implemented")  // Opus підтримує до 255 каналів, але тут тільки 1-2
    }
}

func (d CodecData) SampleFormat() av.SampleFormat {
    return av.S16  // Opus декодує у 16-bit signed integer
}

func (d CodecData) PacketDuration(pkt []byte) (time.Duration, error) {
    return PacketDuration(pkt)  // делегування до головної функції
}
```

### 🔑 Ключові моменти:

| Метод | Повертає | Чому так |
|-------|----------|----------|
| `SampleRate()` | `48000` | Opus internal sample rate завжди 48kHz, навіть якщо вхід/вихід інший |
| `SampleFormat()` | `av.S16` | Opus декодує у 16-bit PCM; для float потрібно ресемплінг |
| `ChannelLayout()` | `CH_MONO/STEREO` | Бібліотека обмежена 1-2 каналами; для більше — розширити `panic` |

### ✅ Ваш use-case: створення CodecData для HLS муксера

```go
// CreateOpusCodecData — створення av.CodecData для заголовка контейнера
func CreateOpusCodecData(channels int) (av.CodecData, error) {
    if channels < 1 || channels > 2 {
        return nil, fmt.Errorf("unsupported channels: %d (Opus parser supports 1-2)", channels)
    }
    
    return opusparser.NewCodecData(channels), nil
}

// Використання при ініціалізації:
func (h *HLSMuxer) initAudioCodec(channels int) error {
    codecData, err := CreateOpusCodecData(channels)
    if err != nil {
        return err
    }
    
    // Додавання у заголовок муксера
    return h.WriteHeader([]av.CodecData{videoCodecData, codecData})
}
```

---

## 🎯 4. Детекція каналів: `Channels()` функція

```go
func Channels(pkt []byte) int {
    if len(pkt) > 0 && (pkt[0]&0x4) == 0 {
        return 1  // mono
    }
    return 2  // stereo
}
```

### 🔍 Логіка:
- `(pkt[0] & 0x4) == 0` → біт 2 = 0 → mono
- `(pkt[0] & 0x4) != 0` → біт 2 = 1 → stereo

### ✅ Ваш use-case: адаптивна обробка за кількістю каналів

```go
// ProcessOpusPacket — обробка з урахуванням моно/стерео
func (p *AudioProcessor) ProcessOpusPacket(pkt []byte) error {
    channels := opusparser.Channels(pkt)
    duration, err := opusparser.PacketDuration(pkt)
    if err != nil {
        return err
    }
    
    // Логування для метрик
    p.metrics.OpusPacketsProcessed.Inc()
    p.metrics.OpusChannels.WithLabelValues(p.channelID).Set(float64(channels))
    p.metrics.OpusDuration.WithLabelValues(p.channelID).Observe(duration.Seconds())
    
    // Адаптивна обробка: наприклад, downmix стерео → моно для низького бітрейту
    if channels == 2 && p.targetChannels == 1 {
        return p.downmixToMono(pkt)
    }
    
    // Стандартна обробка
    return p.encodeForHLS(pkt, duration)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// opus_processor.go — обробка Opus аудіо для HLS
type OpusProcessor struct {
    channelID    string
    codecData    av.CodecData
    lastPacketTime time.Duration
    metrics      *AudioMetrics
}

func NewOpusProcessor(channelID string, channels int) (*OpusProcessor, error) {
    codecData, err := opusparser.NewCodecData(channels)
    if err != nil {
        return nil, err
    }
    
    return &OpusProcessor{
        channelID:  channelID,
        codecData:  codecData,
        metrics:    NewAudioMetrics(channelID),
    }, nil
}

// ProcessPacket — обробка одного Opus пакету
func (p *OpusProcessor) ProcessPacket(pkt []byte, timestamp time.Duration) error {
    // 1. Валідація та розрахунок тривалості
    duration, err := opusparser.PacketDuration(pkt)
    if err != nil {
        return fmt.Errorf("invalid Opus packet: %w", err)
    }
    
    // 2. Оновлення метрик
    p.metrics.RecordPacket(duration, len(pkt), p.channelID)
    
    // 3. Перевірка послідовності таймінгів (детекція розривів)
    if p.lastPacketTime > 0 {
        expectedGap := duration
        actualGap := timestamp - p.lastPacketTime
        
        if actualGap > expectedGap*2 {  // розрив > 2× тривалості
            log.Printf("Channel %s: Opus time gap detected: expected %v, got %v", 
                p.channelID, expectedGap, actualGap)
            p.metrics.TimeGaps.Inc()
        }
    }
    p.lastPacketTime = timestamp
    
    // 4. Підготовка для HLS: Opus → AAC конвертація (якщо потрібно)
    if p.needsTranscoding() {
        return p.transcodeToAAC(pkt, duration, timestamp)
    }
    
    // 5. Пряме записування у HLS (якщо плеєр підтримує Opus)
    return p.writeOpusToHLS(pkt, duration, timestamp)
}

// transcodeToAAC — конвертація Opus → AAC для сумісності з більшістю плеєрів
func (p *OpusProcessor) transcodeToAAC(opusPkt []byte, duration time.Duration, timestamp time.Duration) error {
    // 1. Декодування Opus → PCM
    pcm, err := p.opusDecoder.Decode(opusPkt)
    if err != nil {
        return fmt.Errorf("decode Opus: %w", err)
    }
    
    // 2. Ресемплінг якщо потрібно (Opus 48kHz → AAC 44.1/48kHz)
    resampled, err := p.resampler.Resample(pcm)
    if err != nil {
        return err
    }
    
    // 3. Кодування у AAC
    aacPkts, err := p.aacEncoder.Encode(resampled)
    if err != nil {
        return err
    }
    
    // 4. Запис у HLS з коректними таймінгами
    for i, aacPkt := range aacPkts {
        pktTimestamp := timestamp + time.Duration(i)*duration/time.Duration(len(aacPkts))
        if err := p.hlsWriter.WriteAudioPacket(aacPkt, pktTimestamp); err != nil {
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
| **"empty opus packet"** | `len(pkt) < 1` у `PacketDuration()` | Переконайтеся, що передаєте непорожні пакети; перевірте мережеві втрати |
| **"invalid opus packet"** | `code=3` але `len(pkt) < 2` | Для `code=3` другий байт обов'язковий (лічильник фреймів); перевірте цілісність даних |
| **Неправильна тривалість** | `config` виходить за межі `opusFrameTimes[]` | Переконайтеся, що `config = toc >> 3` дає значення 0-31; валідуйте вхідні дані |
| **Panic у `ChannelLayout()`** | `Channels > 2` | Opus підтримує до 255 каналів, але цей парсер обмежений 1-2; розширте `switch` або обріжте до стерео |
| **Розсинхронізація аудіо/відео** | Неправильний розрахунок `PacketDuration()` | Використовуйте `PacketDuration()` для кожного пакету; не припускайте фіксовану тривалість |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування розрахунку тривалості:

```go
type DurationCache struct {
    mu    sync.RWMutex
    cache map[uint32]time.Duration  // hash(pkt[0:2]) → duration
}

func (c *DurationCache) Get(pkt []byte) (time.Duration, bool) {
    if len(pkt) < 2 {
        return 0, false
    }
    
    // Простий хеш перших 2 байт (достатньо для більшості випадків)
    key := uint32(pkt[0])<<8 | uint32(pkt[1])
    
    c.mu.RLock()
    dur, ok := c.cache[key]
    c.mu.RUnlock()
    
    if ok {
        return dur, true
    }
    
    // Розрахунок якщо не в кеші
    dur, err := PacketDuration(pkt)
    if err != nil {
        return 0, false
    }
    
    c.mu.Lock()
    c.cache[key] = dur
    c.mu.Unlock()
    
    return dur, true
}
```

### 2. Пакетна обробка для зменшення накладних витрат:

```go
// BatchPacketDuration — розрахунок тривалості для кількох пакетів
func BatchPacketDuration(packets [][]byte) ([]time.Duration, error) {
    results := make([]time.Duration, 0, len(packets))
    
    for _, pkt := range packets {
        dur, err := PacketDuration(pkt)
        if err != nil {
            return results, err
        }
        results = append(results, dur)
    }
    return results, nil
}
```

### 3. Моніторинг параметрів потоку:

```go
type OpusMetrics struct {
    PacketsProcessed prometheus.Counter
    AvgDuration      prometheus.Gauge
    ChannelMode      prometheus.GaugeVec  // mono/stereo
    ConfigDistribution prometheus.HistogramVec  // розподіл config значень
}

func (m *OpusMetrics) RecordPacket(duration time.Duration, pkt []byte, channelID string) {
    m.PacketsProcessed.Inc()
    m.AvgDuration.Set(duration.Seconds())
    
    channels := Channels(pkt)
    m.ChannelMode.WithLabelValues(channelID).Set(float64(channels))
    
    config := pkt[0] >> 3
    m.ConfigDistribution.WithLabelValues(channelID).Observe(float64(config))
}
```

---

## 📋 Чек-лист інтеграції opusparser

```go
// ✅ 1. Створення CodecData з правильною кількістю каналів
codecData, err := opusparser.NewCodecData(channels)
if err != nil { /* handle error */ }

// ✅ 2. Розрахунок тривалості кожного пакету
duration, err := opusparser.PacketDuration(pkt)
if err != nil { /* handle error */ }

// ✅ 3. Детекція моно/стерео для адаптивної обробки
channels := opusparser.Channels(pkt)
if channels == 2 && targetChannels == 1 {
    // downmix to mono
}

// ✅ 4. Валідація вхідних даних
if len(pkt) < 1 {
    return errors.New("empty packet")
}
if (pkt[0] & 0x3) == 3 && len(pkt) < 2 {
    return errors.New("invalid packet: code=3 requires 2nd byte")
}

// ✅ 5. Інтеграція з HLS муксером
hlsMuxer.WriteHeader([]av.CodecData{videoCodec, codecData})
hlsMuxer.WritePacket(av.Packet{
    Data: pkt,
    Time: timestamp,
    Duration: duration,
})

// ✅ 6. Метрики
monitoring.OpusDuration.Observe(duration.Seconds())
monitoring.OpusChannels.Set(float64(channels))
```

---

## 🔗 Корисні посилання

- 📄 [RFC 6716: Opus Codec](https://datatracker.ietf.org/doc/html/rfc6716) — офіційна специфікація
- 📄 [Opus TOC Byte Format](https://www.opus-codec.org/docs/opus_rfc6716.html#section-3.1) — детальний опис бітової структури
- 📄 [Opus Frame Durations](https://www.opus-codec.org/docs/opus_rfc6716.html#section-2.1.5) — таблиця тривалостей фреймів
- 💻 [vdk opusparser Package](https://pkg.go.dev/github.com/deepch/vdk/codec/opusparser) — GoDoc documentation
- 🎬 [HLS Audio Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до аудіо у HLS (Opus підтримується не всіма плеєрами)

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV аудіо в реальному часі**:
> 1. **Завжди використовуйте `PacketDuration()`** для кожного пакету — Opus має змінну тривалість фреймів, не припускайте фіксоване значення.
> 2. **Валідуйте `code=3` пакети** — вони вимагають другого байта з лічильником; неправильні дані можуть зламати парсинг.
> 3. **Моніторьте `config` розподіл** — різкі зміни можуть вказувати на адаптацію бітрейту або проблеми з джерелом.
> 4. **Обробляйте моно/стерео адаптивно** — якщо цільовий формат не підтримує стерео, реалізуйте downmix.
> 5. **Перевірте сумісність плеєра з Opus** — не всі HLS-плеєри підтримують Opus; можливо, знадобиться транскодування у AAC.

Потрібен приклад реалізації `downmixToMono()` для конвертації стерео Opus у моно без втрати якості? Готовий допомогти! 🚀