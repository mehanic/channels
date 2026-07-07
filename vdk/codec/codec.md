# 🎵 Глибокий розбір: codec — Factory-функції для аудіо-кодеків у vdk

Цей файл — **набір factory-функцій та заглушок** для створення `av.AudioCodecData` об'єктів різних аудіо-кодеків у бібліотеці `vdk`. Він надає спрощений інтерфейс для ініціалізації метаданих кодеків без необхідності парсингу реальних потоків.

Розберемо архітектуру, use-case'и та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема пакету codec

```
┌────────────────────────────────────────┐
│ 📦 codec — Audio Codec Data Factories  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • OpusCodecData — метадані для Opus   │
│  • PCMUCodecData — для PCM μ-law/A-law │
│  • SpeexCodecData — заглушка для Speex │
│  • Factory-функції: New*CodecData()    │
│                                         │
│  🎯 Призначення:                        │
│  • Швидка ініціалізація тестових даних │
│  • Mock-об'єкти для юніт-тестів        │
│  • Спрощена конфігурація для реальних  │
│    пайплайнів без парсингу заголовків  │
│                                         │
│  🔄 Інтеграція:                         │
│  • Реалізує av.AudioCodecData інтерфейс│
│  • Сумісний з transcode, muxer, demuxer│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. OpusCodecData — спрощені метадані для Opus

### Структура та методи:

```go
type OpusCodecData struct {
    typ            av.CodecType        // завжди av.OPUS
    SampleRate_    int                 // частота дискретизації (зазвичай 48000)
    ChannelLayout_ av.ChannelLayout    // CH_MONO або CH_STEREO
}

// Реалізація av.AudioCodecData:
func (self OpusCodecData) Type() av.CodecType { return self.typ }
func (self OpusCodecData) SampleRate() int { return self.SampleRate_ }
func (self OpusCodecData) ChannelLayout() av.ChannelLayout { return self.ChannelLayout_ }
func (self OpusCodecData) SampleFormat() av.SampleFormat { return av.FLT }  // float!
func (self OpusCodecData) PacketDuration(data []byte) (time.Duration, error) {
    return 20 * time.Millisecond, nil  // фіксована тривалість!
}
```

### 🔍 Ключові відмінності від `opusparser.CodecData`:

| Характеристика | `codec.OpusCodecData` | `opusparser.CodecData` |
|---------------|----------------------|----------------------|
| **SampleFormat** | `av.FLT` (float) | `av.S16` (16-bit int) |
| **PacketDuration** | Фіксована 20ms | Динамічна з TOC байта |
| **Гнучкість** | Конфігурується вручну | Парситься з реального пакету |
| **Призначення** | Тести/заглушки | Реальна обробка потоків |

### ✅ Ваш use-case: швидка ініціалізація для тестів

```go
// TestHLSMuxer_Opus — юніт-тест з фейковими метаданими
func TestHLSMuxer_Opus(t *testing.T) {
    // Створення фейкового Opus codec data
    codecData := codec.NewOpusCodecData(48000, av.CH_STEREO)
    
    // Перевірка реалізації інтерфейсу
    assert.Equal(t, av.OPUS, codecData.Type())
    assert.Equal(t, 48000, codecData.SampleRate())
    assert.Equal(t, av.CH_STEREO, codecData.ChannelLayout())
    assert.Equal(t, av.FLT, codecData.SampleFormat())
    
    // Перевірка фіксованої тривалості
    dur, err := codecData.PacketDuration([]byte{0x48})
    assert.NoError(t, err)
    assert.Equal(t, 20*time.Millisecond, dur)
    
    // Використання у муксері
    muxer := NewTestHLSMuxer()
    err = muxer.WriteHeader([]av.CodecData{videoCodec, codecData})
    assert.NoError(t, err)
}
```

---

## 🔑 2. PCMUCodecData — універсальний для PCM μ-law/A-law/PCM

### Структура та factory-функції:

```go
type PCMUCodecData struct {
    typ av.CodecType  // av.PCM_MULAW, av.PCM_ALAW, або av.PCM
}

// Factory-функції для різних типів:
func NewPCMMulawCodecData() av.AudioCodecData {
    return PCMUCodecData{typ: av.PCM_MULAW}
}

func NewPCMAlawCodecData() av.AudioCodecData {
    return PCMUCodecData{typ: av.PCM_ALAW}
}

func NewPCMCodecData() av.AudioCodecData {
    return PCMUCodecData{typ: av.PCM}
}

// Реалізація av.AudioCodecData:
func (self PCMUCodecData) Type() av.CodecType { return self.typ }
func (self PCMUCodecData) SampleRate() int { return 8000 }  // фіксовано 8kHz!
func (self PCMUCodecData) ChannelLayout() av.ChannelLayout { return av.CH_MONO }  // фіксовано моно!
func (self PCMUCodecData) SampleFormat() av.SampleFormat { return av.S16 }
func (self PCMUCodecData) PacketDuration(data []byte) (time.Duration, error) {
    // Формула: (кількість байт / 8000) секунд
    return time.Duration(len(data)) * time.Second / 8000, nil
}
```

### 🔍 Чому фіксовані параметри?

```
PCM μ-law/A-law у телекомунікаціях (G.711):
• Завжди 8kHz sample rate
• Завжди моно (один канал)
• Завжди 8 біт на семпл (але зберігається як S16 для сумісності)

Тривалість пакету:
• Кожен байт = 1 семпл = 1/8000 секунди = 125 мкс
• Пакет 160 байт = 160/8000 = 20мс
• Пакет 320 байт = 320/8000 = 40мс

Це стандарт для VoIP, телефонії, CCTV аудіо.
```

### ✅ Ваш use-case: обробка аудіо з CCTV камер

```go
// ProcessG711Audio — обробка аудіо у форматі G.711 (μ-law/A-law)
func (p *AudioProcessor) ProcessG711Audio(pkt []byte, codecType av.CodecType) error {
    // 1. Створення codec data за типом
    var codecData av.AudioCodecData
    switch codecType {
    case av.PCM_MULAW:
        codecData = codec.NewPCMMulawCodecData()
    case av.PCM_ALAW:
        codecData = codec.NewPCMAlawCodecData()
    default:
        return fmt.Errorf("unsupported PCM type: %v", codecType)
    }
    
    // 2. Розрахунок тривалості
    duration, err := codecData.PacketDuration(pkt)
    if err != nil {
        return err
    }
    
    // 3. Логування для метрик
    p.metrics.G711PacketsProcessed.Inc()
    p.metrics.G711Duration.WithLabelValues(p.channelID).Observe(duration.Seconds())
    
    // 4. Конвертація у AAC для HLS (якщо потрібно)
    if p.targetFormat == "aac" {
        return p.convertG711ToAAC(pkt, duration, codecType)
    }
    
    // 5. Пряме записування (якщо плеєр підтримує PCM)
    return p.writePCMToHLS(pkt, duration, codecType)
}

// convertG711ToAAC — конвертація G.711 → AAC
func (p *AudioProcessor) convertG711ToAAC(g711Data []byte, duration time.Duration, codecType av.CodecType) error {
    // 1. Декодування G.711 → PCM (S16)
    pcm, err := p.g711Decoder.Decode(g711Data, codecType == av.PCM_ALAW)
    if err != nil {
        return fmt.Errorf("decode G.711: %w", err)
    }
    
    // 2. Ресемплінг 8kHz → 48kHz (для AAC)
    resampled, err := p.resampler.Resample(pcm, 8000, 48000)
    if err != nil {
        return err
    }
    
    // 3. Кодування у AAC
    aacPkts, err := p.aacEncoder.Encode(resampled)
    if err != nil {
        return err
    }
    
    // 4. Запис у HLS
    for _, aacPkt := range aacPkts {
        if err := p.hlsWriter.WriteAudioPacket(aacPkt, duration); err != nil {
            return err
        }
    }
    
    return nil
}
```

---

## 🔑 3. SpeexCodecData — заглушка на базі fake.CodecData

### Структура та наслідування:

```go
type SpeexCodecData struct {
    fake.CodecData  // вбудовуємо базову заглушку
}

// Перевизначення тільки PacketDuration:
func (self SpeexCodecData) PacketDuration(data []byte) (time.Duration, error) {
    return 20 * time.Millisecond, nil  // фіксована тривалість
}

// Factory-функція:
func NewSpeexCodecData(sr int, cl av.ChannelLayout) SpeexCodecData {
    codec := SpeexCodecData{}
    codec.CodecType_ = av.SPEEX
    codec.SampleFormat_ = av.S16
    codec.SampleRate_ = sr
    codec.ChannelLayout_ = cl
    return codec
}
```

### 🔍 Чому наслідування від `fake.CodecData`?

```
• Speex — застарілий кодек, рідко використовується у нових системах
• Реальний парсинг Speex складний (змінна тривалість, різні режими)
• Для більшості use-case'ів достатньо фіксованої тривалості 20мс
• Вбудовування fake.CodecData дає всі необхідні методи інтерфейсу
• Можна легко розширити у майбутньому якщо знадобиться
```

### ✅ Ваш use-case: підтримка застарілих камер

```go
// ProcessLegacyAudio — обробка аудіо зі старих камер (Speex/G.711)
func (p *LegacyProcessor) ProcessLegacyAudio(pkt []byte, codecHint string) error {
    var codecData av.AudioCodecData
    
    switch codecHint {
    case "speex":
        // Створення фейкового Speex codec data
        codecData = codec.NewSpeexCodecData(16000, av.CH_MONO)
        
    case "pcmu", "g711u":
        codecData = codec.NewPCMMulawCodecData()
        
    case "pcma", "g711a":
        codecData = codec.NewPCMAlawCodecData()
        
    default:
        return fmt.Errorf("unknown legacy codec: %s", codecHint)
    }
    
    // Розрахунок тривалості
    duration, err := codecData.PacketDuration(pkt)
    if err != nil {
        return err
    }
    
    // Логування для моніторингу застарілих форматів
    p.metrics.LegacyCodecUsed.WithLabelValues(codecHint, p.channelID).Inc()
    
    // Конвертація у сучасний формат (AAC) для сумісності
    return p.convertToModernFormat(pkt, duration, codecData)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// audio_codec_factory.go — фабрика кодеків для CCTV аудіо
type AudioCodecFactory struct {
    channelConfigs map[string]*ChannelAudioConfig
}

type ChannelAudioConfig struct {
    CodecType      av.CodecType
    SampleRate     int
    Channels       int
    TargetFormat   string  // "aac", "opus", "passthrough"
}

func NewAudioCodecFactory(configs map[string]*ChannelAudioConfig) *AudioCodecFactory {
    return &AudioCodecFactory{channelConfigs: configs}
}

// GetCodecData — отримання або створення codec data для каналу
func (f *AudioCodecFactory) GetCodecData(channelID string, hintData []byte) (av.AudioCodecData, error) {
    config, ok := f.channelConfigs[channelID]
    if !ok {
        return nil, fmt.Errorf("unknown channel: %s", channelID)
    }
    
    // Якщо є підказка з реального пакету — використовуємо її
    if hintData != nil && len(hintData) > 0 {
        return f.parseFromHint(config.CodecType, hintData)
    }
    
    // Інакше — створюємо з конфігурації
    return f.createFromConfig(config)
}

// parseFromHint — спроба парсингу з реального пакету
func (f *AudioCodecFactory) parseFromHint(codecType av.CodecType, data []byte) (av.AudioCodecData, error) {
    switch codecType {
    case av.OPUS:
        // Спроба парсингу через opusparser
        if len(data) >= 1 {
            channels := opusparser.Channels(data)
            return opusparser.NewCodecData(channels), nil
        }
        // Fallback на factory
        return codec.NewOpusCodecData(48000, av.CH_STEREO), nil
        
    case av.PCM_MULAW, av.PCM_ALAW:
        // G.711 не потребує парсингу — використовуємо factory
        if codecType == av.PCM_MULAW {
            return codec.NewPCMMulawCodecData(), nil
        }
        return codec.NewPCMAlawCodecData(), nil
        
    case av.SPEEX:
        // Speex — тільки factory
        return codec.NewSpeexCodecData(16000, av.CH_MONO), nil
        
    default:
        return nil, fmt.Errorf("unsupported codec for hint parsing: %v", codecType)
    }
}

// createFromConfig — створення з конфігурації каналу
func (f *AudioCodecFactory) createFromConfig(config *ChannelAudioConfig) (av.AudioCodecData, error) {
    layout := av.CH_MONO
    if config.Channels == 2 {
        layout = av.CH_STEREO
    }
    
    switch config.CodecType {
    case av.OPUS:
        return codec.NewOpusCodecData(config.SampleRate, layout), nil
        
    case av.PCM_MULAW:
        return codec.NewPCMMulawCodecData(), nil
        
    case av.PCM_ALAW:
        return codec.NewPCMAlawCodecData(), nil
        
    case av.SPEEX:
        return codec.NewSpeexCodecData(config.SampleRate, layout), nil
        
    case av.AAC:
        // AAC потребує реального парсингу — повертаємо помилку
        return nil, fmt.Errorf("AAC requires real SPS/PPS parsing, use opusparser/aacparser")
        
    default:
        return nil, fmt.Errorf("unsupported codec type: %v", config.CodecType)
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Неправильна тривалість Opus** | Фіксована 20ms замість динамічної з TOC | Використовуйте `opusparser.CodecData` для реальної обробки; `codec.OpusCodecData` тільки для тестів |
| **G.711 не моно** | `ChannelLayout()` завжди повертає `CH_MONO` | Це очікувана поведінка для G.711; якщо потрібно стерео — використовуйте інший кодек або ресемплінг |
| **SampleFormat не співпадає** | `OpusCodecData` повертає `FLT`, а декодер очікує `S16` | Переконайтеся, що ваш декодер підтримує float; або використовуйте `opusparser.CodecData` з `S16` |
| **Speex не парситься** | `PacketDuration()` завжди 20ms | Це обмеження заглушки; для точної обробки реалізуйте повний Speex parser або конвертуйте у AAC на вході |
| **Конфлікт типів** | `codec.OpusCodecData` vs `opusparser.CodecData` | Використовуйте `codec.*` для тестів/заглушок, `*parser.*` для реальної обробки; не змішуйте |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування codec data на рівні каналу:

```go
type CodecDataCache struct {
    mu    sync.RWMutex
    cache map[string]av.AudioCodecData  // channelID → codecData
}

func (c *CodecDataCache) Get(channelID string) (av.AudioCodecData, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    codec, ok := c.cache[channelID]
    return codec, ok
}

func (c *CodecDataCache) Set(channelID string, codec av.AudioCodecData) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.cache[channelID] = codec
}

// Використання: уникати повторного створення однакових об'єктів
if codec, ok := cache.Get(channelID); ok {
    return codec, nil
}
codec, err := factory.GetCodecData(channelID, hint)
if err == nil {
    cache.Set(channelID, codec)
}
return codec, err
```

### 2. Пакетна обробка тривалостей:

```go
// BatchPacketDuration — розрахунок для кількох пакетів одного кодека
func BatchPacketDuration(codec av.AudioCodecData, packets [][]byte) ([]time.Duration, error) {
    results := make([]time.Duration, 0, len(packets))
    
    for _, pkt := range packets {
        dur, err := codec.PacketDuration(pkt)
        if err != nil {
            return results, err
        }
        results = append(results, dur)
    }
    return results, nil
}
```

### 3. Моніторинг використання кодеків:

```go
type CodecMetrics struct {
    PacketsByCodec prometheus.CounterVec
    AvgDuration    prometheus.GaugeVec
    FormatConversions prometheus.CounterVec
}

func (m *CodecMetrics) RecordPacket(codecType av.CodecType, duration time.Duration, channelID string) {
    m.PacketsByCodec.WithLabelValues(codecType.String(), channelID).Inc()
    m.AvgDuration.WithLabelValues(channelID).Set(duration.Seconds())
}

func (m *CodecMetrics) RecordConversion(from, to av.CodecType, channelID string) {
    m.FormatConversions.WithLabelValues(from.String(), to.String(), channelID).Inc()
}
```

---

## 📋 Чек-лист інтеграції codec factory

```go
// ✅ 1. Вибір між factory та parser залежно від use-case
if isUnitTest {
    // Використовуємо codec.* для швидкості та простоти
    codecData := codec.NewOpusCodecData(48000, av.CH_STEREO)
} else {
    // Використовуємо *parser.* для точності
    codecData, err := opusparser.NewCodecDataFromPacket(firstPacket)
}

// ✅ 2. Валідація параметрів перед створенням
if channels < 1 || channels > 2 {
    return nil, fmt.Errorf("unsupported channels: %d", channels)
}
if sampleRate != 48000 && codecType == av.OPUS {
    log.Printf("warning: Opus internal rate is always 48kHz, got %d", sampleRate)
}

// ✅ 3. Обробка помилок від factory-функцій
codecData, err := factory.GetCodecData(channelID, hint)
if err != nil {
    // Fallback на дефолтні значення або помилка
    log.Printf("fallback to defaults for channel %s: %v", channelID, err)
    codecData = codec.NewOpusCodecData(48000, av.CH_MONO)
}

// ✅ 4. Кешування результатів для уникнення повторних обчислень
if cached, ok := codecCache.Get(channelID); ok {
    return cached, nil
}
codecData, err := createCodecData(...)
if err == nil {
    codecCache.Set(channelID, codecData)
}

// ✅ 5. Логування для дебагу та моніторингу
log.Printf("Channel %s: using codec %v, sampleRate=%d, channels=%d", 
    channelID, codecData.Type(), codecData.SampleRate(), codecData.ChannelLayout().Count())
```

---

## 🔗 Корисні посилання

- 💻 [vdk codec Package](https://pkg.go.dev/github.com/deepch/vdk/codec) — GoDoc documentation
- 📄 [G.711 μ-law/A-law Spec](https://www.itu.int/rec/T-REC-G.711) — стандарт для PCM кодеків
- 📄 [Opus RFC 6716](https://datatracker.ietf.org/doc/html/rfc6716) — специфікація Opus
- 📄 [Speex Documentation](https://www.speex.org/docs/) — документація застарілого кодека
- 🧪 [vdk fake Package](https://pkg.go.dev/github.com/deepch/vdk/codec/fake) — базові заглушки для тестів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **різними аудіо-кодеками у CCTV**:
> 1. **Використовуйте `codec.*` factory-функції тільки для тестів/заглушок** — для реальної обробки віддавайте перевагу `*parser.*` пакетам з динамічним парсингом.
> 2. **Валідуйте `SampleFormat`** — `OpusCodecData` повертає `FLT`, але багато декодерів очікують `S16`; переконайтеся у сумісності.
> 3. **Кешуйте `AudioCodecData` на рівні каналу** — створення нових об'єктів для кожного пакету неефективне.
> 4. **Обробляйте G.711 (PCMU/PCMA) окремо** — це стандарт для телефонії, але вимагає конвертації у AAC для HLS сумісності.
> 5. **Логувайте використання застарілих кодеків** (Speex) — це допоможе планувати міграцію на сучасні формати.

Потрібен приклад реалізації `convertG711ToAAC()` для конвертації телефонного аудіо у формат, сумісний з HLS? Готовий допомогти! 🚀