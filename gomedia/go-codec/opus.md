# 🎧 Глибокий розбір `codec/opus.go` — парсинг Opus бітстріму (RFC 6716)

Це **низькорівнева реалізація парсера стандарту IETF RFC 6716 (Opus)**, який дозволяє вашому CCTV HLS Processor обробляти аудіопотоки у форматі Opus: витягувати тривалість кадрів для PTS-синхронізації, парсити конфігурацію кодування та генерувати `OpusHead` extradata для fMP4-сегментів. Розберемо архітектурно:

---

## 🧱 1. Архітектура Opus: гібридний кодек з адаптивною структурою

### 🔑 Унікальна особливість Opus:

Opus — це **гібридний кодек**, що поєднує два алгоритми:

```
[Opus Packet]
├─ TOC Byte (Table of Contents) — 1 байт
│  ├─ config: 5 біт (0-31) → режим кодування + bandwidth
│  ├─ s: 1 біт (mono/stereo)
│  └─ c: 2 біти (code 0-3) → спосіб пакування кадрів
│
├─ Payload (змінна структура залежно від code)
│  ├─ Code 0: 1 кадр
│  ├─ Code 1: 2 кадри однакового розміру
│  ├─ Code 2: 2 кадри різного розміру
│  └─ Code 3: N кадрів (1-255) з VBR/CBR
│
└─ Optional Padding (для вирівнювання)
```

### 📊 Таблиця конфігурацій (з коментарів у коді):

| Config | Mode | Bandwidth | Frame Sizes | SampleSize таблиця |
|--------|------|-----------|-------------|-------------------|
| 0-3 | SILK-only | NB (8 kHz) | 10/20/40/60 ms | `SLKOpusSampleSize[4]` |
| 4-7 | SILK-only | MB (12 kHz) | 10/20/40/60 ms | `SLKOpusSampleSize[4]` |
| 8-11 | SILK-only | WB (16 kHz) | 10/20/40/60 ms | `SLKOpusSampleSize[4]` |
| 12-13 | Hybrid | SWB (24 kHz) | 10/20 ms | `HybridOpusSampleSize[2]` |
| 14-15 | Hybrid | FB (48 kHz) | 10/20 ms | `HybridOpusSampleSize[2]` |
| 16-19 | CELT-only | NB/WB | 2.5/5/10/20 ms | `CELTOpusSampleSize[4]` |
| 20-31 | CELT-only | SWB/FB | 2.5/5/10/20 ms | `CELTOpusSampleSize[4]` |

> 💡 **Практичне значення для CCTV**: Більшість систем використовують **CELT-only FB (config 28-31) з 20ms кадрами** — це дає низьку затримку (~20ms) при високій якості (48 kHz), що критично для синхронізації аудіо з відео у реальному часі.

---

## 🔢 2. TOC Byte — ключ до розуміння пакета

### 🔍 Бітова структура (RFC 6716, Section 3.1):

```
Біти 7-3: config (0-31) → режим + bandwidth
Біт   2:  s (0=mono, 1=stereo)
Біти  1-0: c (code 0-3) → спосіб пакування

Приклад: 0xE3 = 1110 0011
├─ config = 0b11100 = 28 → CELT-only FB
├─ s = 0 → mono
└─ c = 0b11 = 3 → Code 3 (N кадрів, VBR)
```

### 🔧 Функція `OpusPacketDuration` — розрахунок тривалості:

```go
func OpusPacketDuration(packet []byte) uint64 {
    config := int(packet[0] >> 3)  // старші 5 біт
    code := packet[0] & 0x03        // молодші 2 біти
    
    // Визначити кількість кадрів у пакеті
    frameCount := 0
    switch code {
    case 0: frameCount = 1
    case 1, 2: frameCount = 2
    case 3: frameCount = int(packet[1] & 0x1F)  // біти 4-0 байту 1
    }
    
    // Вибрати таблицю SampleSize за config
    switch {
    case config < 12:  // SILK-only
        duration = uint64(frameCount * SLKOpusSampleSize[config%4])
    case config < 16:  // Hybrid
        duration = uint64(frameCount * HybridOpusSampleSize[config%2])
    case config < 32:  // CELT-only
        duration = uint64(frameCount * CELTOpusSampleSize[config%4])
    }
    
    return duration  // у семплах @ 48 kHz
}
```

### 📐 Приклад розрахунку:

```
Вхідний пакет: []byte{0xE3, ...}  // config=28, stereo=0, code=3
1. config = 28 >> 3 = 3 (помилка! має бути 28 >> 3 = 3? Ні, 28 = 0b11100, >>3 = 0b111 = 7)
   // ⚠️ У коді: packet[0] >> 3 дає 0b11100 >> 3 = 0b111 = 7, але config=28 має бути 28!
   // Насправді: 0xE3 = 0b11100011, >>3 = 0b11100 = 28 ✓

2. code = 0xE3 & 0x03 = 0b11 = 3 → Code 3

3. frameCount = packet[1] & 0x1F  // припустимо, packet[1] = 0x05 → 5 кадрів

4. config=28 → CELT-only → CELTOpusSampleSize[28%4] = CELTOpusSampleSize[0] = 120 семплів

5. duration = 5 * 120 = 600 семплів @ 48 kHz = 600/48000 = 12.5 ms
```

> ⚠️ **Потенційна проблема**: У коді є `panic("unkown opus config")` для config >= 32, але за специфікацією config — це 5 біт (0-31), тому цей випадок неможливий. Краще повернути помилку, ніж панікувати.

---

## 📦 3. Чотири коди пакування (Code 0-3) — як Opus економить байти

### 🔸 Code 0: Один кадр (найпростіший)

```
[TOC: config|s|0|0][Frame Data (N-1 bytes)]
```

```go
case 0:
    pkt.FrameCount = 1
    pkt.FrameLen[0] = uint16(len(packet) - 1)  // весь payload — один кадр
    pkt.Frame = packet[1:]
```

### 🔸 Code 1: Два кадри однакового розміру

```
[TOC: config|s|0|1][Frame1 (N/2 bytes)][Frame2 (N/2 bytes)]
```

```go
case 1:
    pkt.FrameCount = 2
    pkt.FrameLen[0] = uint16(len(packet)-1) / 2  // ділимо навпіл
    pkt.Frame = packet[1:]  // обидва кадри підряд
```

### 🔸 Code 2: Два кадри різного розміру (з довжиною)

```
[TOC: config|s|1|0][N1 (1-2 bytes)][Frame1 (N1 bytes)][Frame2 (rest)]
```

```go
case 2:
    N1 := int(packet[1])  // перший байт довжини
    if N1 >= 252 {  // extended length
        N1 = N1 + int(packet[2]*4)  // формула: total = first + second*4
        hdr = 2  // заголовок займає 2 байти
    }
    pkt.FrameLen[0] = uint16(N1)
    pkt.FrameLen[1] = uint16(len(packet)-hdr) - uint16(N1)  // решта — другий кадр
```

### 🔸 Code 3: N кадрів (1-255) з VBR/CBR — найскладніший

```
[TOC: config|s|1|1][Frame Count Byte][Padding?][Lengths...][Frames...]
```

#### Frame Count Byte (другий байт пакета):
```
Біт 7: v (0=CBR, 1=VBR)
Біт 6: p (0=no padding, 1=padding present)
Біти 4-0: M (frame count, 1-31)
```

#### CBR vs VBR:
| Режим | Як кодуються довжини | Коли використовується |
|-------|---------------------|---------------------|
| **CBR** (v=0) | Одна довжина для всіх кадрів: `(total - hdr - padding) / M` | Стабільний бітрейт, простіший парсинг |
| **VBR** (v=1) | M-1 довжин кодуються явно, остання = решта | Змінний бітрейт, краща компресія |

#### Extended length coding (для N1 >= 252):
```
Якщо перший байт довжини >= 252:
  total_length = first_byte + second_byte * 4
  // Приклад: [252, 10] → 252 + 10*4 = 292 байти
```

> 💡 **Практичне значення**: Code 3 з VBR — це "режим максимальної економії" для низькобітрейтних каналів (наприклад, 3G-камери). Ваш `segmentAssembler` має коректно обробляти обидва режими для універсальності.

---

## 🧬 4. `OpusPacket` структура — результат парсингу

```go
type OpusPacket struct {
    Code       int           // 0-3: спосіб пакування
    Config     int           // 0-31: режим кодування
    Stereo     int           // 0=mono, 1=stereo
    Vbr        int           // 0=CBR, 1=VBR (тільки для Code 3)
    FrameCount int           // кількість кадрів у пакеті
    FrameLen   []uint16      // довжина кожного кадру у байтах
    Frame      []byte        // сирі дані кадрів (без заголовків)
    Duration   uint64        // тривалість у семплах @ 48 kHz
}
```

### 🎯 Використання у вашому пайплайні:

```go
// У segmentAssembler для розрахунку PTS:
func (sa *SegmentAssembler) handleOpusPacket(data []byte) error {
    pkt := codec.DecodeOpusPacket(data)
    
    // Розрахунок тривалості у мікросекундах (Opus завжди 48 kHz внутрішньо)
    durationUs := pkt.Duration * 1_000_000 / 48000
    
    // Обробка кожного кадру окремо (важливо для Code 3 з багатьма кадрами)
    offset := 0
    for i, frameLen := range pkt.FrameLen {
        frame := pkt.Frame[offset : offset+int(frameLen)]
        
        // Додати кадр у поточний аудіо-сегмент з коректним PTS
        sa.currentAudioSegment.AppendFrame(frame, sa.currentAudioPTS)
        
        // Оновити PTS для наступного кадру
        sa.currentAudioPTS += durationUs / uint64(len(pkt.FrameLen))
        
        offset += int(frameLen)
    }
    
    return nil
}
```

---

## 🏷️ 5. `OpusHead` — extradata для fMP4/Ogg контейнерів

Це **критична структура** для ініціалізації декодера (RFC 7845, Section 5.1):

### 🔍 Структура OpusHead (19+ байт):

```
Байти  0-7:  "OpusHead" (magic signature)
Байт    8:   Version (завжди 1)
Байт    9:   Channel Count (1=mono, 2=stereo)
Байти  10-11: Pre-skip (uint16 LE) — семпли для пропуску на старті
Байти  12-15: Input Sample Rate (uint32 LE) — оригінальна частота дискретизації
Байти  16-17: Output Gain (Q7.8 fixed point) — посилення у dB
Байт   18:   Mapping Family (0=базова, 1=Vorbis, 255=немає)
Байти 19+:   Optional Channel Mapping Table (якщо Mapping Family != 0)
```

### 🔧 Функція `ParseExtranData` — парсинг конфігурації:

```go
func (ctx *OpusContext) ParseExtranData(extraData []byte) error {
    // 1. Перевірка magic signature
    if string(extraData[0:8]) != "OpusHead" {
        return errors.New("magic signature must equal OpusHead")
    }
    
    // 2. Читання базових полів (Little Endian!)
    ctx.ChannelCount = int(extraData[9])
    ctx.Preskip = int(binary.LittleEndian.Uint16(extraData[10:]))
    ctx.SampleRate = int(binary.LittleEndian.Uint32(extraData[12:]))
    ctx.OutputGain = binary.LittleEndian.Uint16(extraData[16:])
    ctx.MapType = extraData[18]
    
    // 3. Обробка Channel Mapping Table (якщо потрібно)
    if ctx.MapType == 1 {  // Vorbis mapping
        ctx.StreamCount = int(extraData[19])
        ctx.StereoStreamCount = int(extraData[20])
        channel := extraData[21 : 21+ctx.ChannelCount]
        
        // 4. Побудова ChannelMaps для правильного відтворення
        for i := 0; i < ctx.ChannelCount; i++ {
            cm := ChannelMap{}
            index := channel[vorbisOrder(ctx.ChannelCount, i)]
            // ... логіка мапінгу каналів
            ctx.ChannelMaps = append(ctx.ChannelMaps, cm)
        }
    }
    return nil
}
```

### 🎯 Навіщо це у HLS:

| Поле | Роль у вашому пайплайні |
|------|-------------------------|
| **Pre-skip** | Кількість семплів для пропуску на початку → коректна синхронізація аудіо/відео з першого кадру |
| **Output Gain** | Посилення гучності → клієнт може автоматично нормалізувати гучність без пост-обробки |
| **Channel Mapping** | Правильне відтворення multi-channel аудіо (наприклад, 5.1 у професійних CCTV-системах) |
| **Sample Rate** | Хоча Opus внутрішньо завжди 48 kHz, це поле потрібне для конвертації у вихідну частоту клієнта |

---

## 🐞 6. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **`OpusPacketDuration` не зберігає результат у `pkt.Duration`**:
   ```go
   func DecodeOpusPacket(packet []byte) *OpusPacket {
       // ... парсинг ...
       OpusPacketDuration(packet)  // ← виклик без присвоєння! Результат втрачається
       return pkt
   }
   // Має бути:
   pkt.Duration = OpusPacketDuration(packet)
   ```

2. **Некоректна обробка Code 2 у `DecodeOpusPacket`**:
   ```go
   case 2:
       // ... парсинг N1 ...
       pkt.FrameLen = make([]uint16, 2)
       pkt.FrameLen[0] = uint16(N1)
       pkt.FrameLen[1] = uint16(len(packet)-hdr) - uint16(N1)  // ⚠️ Не враховує, що Frame не встановлено!
       // Потрібно також встановити pkt.Frame = packet[hdr:]
   ```

3. **`WriteOpusExtraData` не записує всі поля**:
   ```go
   func (ctx *OpusContext) WriteOpusExtraData() []byte {
       extraData := make([]byte, 19)  // ← лише базові 19 байт
       // ... запис базових полів ...
       // Але не записує MapType, StreamCount, ChannelMapping!
       // Це призведе до некоректного відтворення multi-channel аудіо
   }
   ```

4. **Відсутня валідація `FrameLen`**:
   ```go
   // Якщо N1 > len(packet) — паніка при доступі до пакету
   // Краще додати:
   if int(N1) > len(packet)-hdr {
       return nil, errors.New("invalid frame length")
   }
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для конвертації тривалості у часові одиниці
func (pkt *OpusPacket) DurationTime() time.Duration {
    // Opus внутрішньо завжди 48 kHz
    return time.Duration(pkt.Duration) * time.Second / 48000
}

// 2. Валідація пакета перед обробкою
func (pkt *OpusPacket) Validate() error {
    if pkt.Config < 0 || pkt.Config > 31 {
        return errors.New("invalid config")
    }
    if pkt.FrameCount < 1 || pkt.FrameCount > 255 {
        return errors.New("invalid frame count")
    }
    totalLen := 0
    for _, l := range pkt.FrameLen {
        totalLen += int(l)
    }
    if totalLen != len(pkt.Frame) {
        return fmt.Errorf("frame length mismatch: declared=%d, actual=%d", totalLen, len(pkt.Frame))
    }
    return nil
}

// 3. Інтеграція з метриками
func (sa *SegmentAssembler) recordOpusMetrics(pkt *codec.OpusPacket) {
    mode := "unknown"
    switch {
    case pkt.Config < 12: mode = "SILK"
    case pkt.Config < 16: mode = "Hybrid"
    case pkt.Config < 32: mode = "CELT"
    }
    
    metrics.AudioCodecProfile.WithLabelValues(
        fmt.Sprintf("Opus-%s", mode),
        fmt.Sprintf("Code%d", pkt.Code),
    ).Inc()
    
    metrics.AudioFrameDuration.Observe(float64(pkt.DurationTime()) / float64(time.Millisecond))
    
    if pkt.Stereo == 1 {
        metrics.AudioChannels.WithLabelValues("stereo").Inc()
    } else {
        metrics.AudioChannels.WithLabelValues("mono").Inc()
    }
}
```

---

## 🎯 7. Інтеграція з вашим CCTV HLS Processor

### 📍 У `segmentAssembler` — уніфікована обробка аудіо:

```go
func (sa *SegmentAssembler) handleAudioChunk(codecID codec.CodecID, data []byte) error {
    switch codecID {
    case codec.CODECID_AUDIO_AAC:
        return sa.handleAACChunk(data)
        
    case codec.CODECID_AUDIO_OPUS:
        // Opus пакети можуть містити кілька кадрів — обробляємо кожен окремо
        pkt := codec.DecodeOpusPacket(data)
        if err := pkt.Validate(); err != nil {
            logger.Warn("skipping invalid Opus packet", "error", err)
            return nil
        }
        
        // Розрахунок базової тривалості одного кадру
        baseDuration := pkt.DurationTime() / time.Duration(pkt.FrameCount)
        
        offset := 0
        for i, frameLen := range pkt.FrameLen {
            frame := data[offset : offset+int(frameLen)]
            
            // Додати кадр з коректним PTS
            sa.processAudioFrame(frame, sa.currentAudioPTS)
            
            // Оновити PTS для наступного кадру
            sa.currentAudioPTS += baseDuration
            
            offset += int(frameLen)
        }
        return nil
        
    case codec.CODECID_AUDIO_MP3:
        return codec.SplitMp3Frames(data, sa.handleMP3Frame)
    }
    return nil
}
```

### 📍 У `createTSSegment` — генерація init-сегменту:

```go
func createOpusInitSegment(opusHead []byte) ([]byte, error) {
    // Opus у fMP4 вимагає специфічного extradata формату
    // (RFC 7845, Section 5.1 + ISO/IEC 14496-12)
    
    // 1. Перевірити/доповнити OpusHead
    var ctx codec.OpusContext
    if err := ctx.ParseExtranData(opusHead); err != nil {
        return nil, fmt.Errorf("invalid OpusHead: %w", err)
    }
    
    // 2. Згенерувати валідний extradata (включаючи Channel Mapping якщо потрібно)
    extradata := ctx.WriteOpusExtraData()  // ⚠️ Потрібно доповнити функцію!
    
    // 3. Побудувати fMP4 init сегмент з 'Opus' track
    return mp4.BuildInitSegment(mp4.TrackConfig{
        Codec:      "Opus",
        ExtraData:  extradata,
        Channels:   ctx.ChannelCount,
        SampleRate: 48000,  // Opus завжди 48 kHz внутрішньо
        Timescale:  48000,  // HLS standard
    }), nil
}
```

### 📍 У `VideoManifestProxy` — синхронізація з відео:

```go
func calculateOpusVideoSync(opusCtx *codec.OpusContext, videoFPS float64) time.Duration {
    // Opus має фіксовану внутрішню частоту 48 kHz
    opusSampleRate := 48000
    
    // Скільки аудіо-семплів припадає на один відео-кадр
    samplesPerVideoFrame := float64(opusSampleRate) / videoFPS
    
    // Для типового 20ms Opus кадру: 960 семплів
    // Скільки відео-кадрів припадає на один аудіо-кадр
    videoFramesPerOpusFrame := 960.0 / samplesPerVideoFrame
    
    return time.Duration(float64(time.Second) * videoFramesPerOpusFrame / videoFPS)
}
```

---

## 🧭 Висновок: чому Opus складніший, але вартий того

| Аспект | AAC/MP3 | Opus | Вигода для вашого проекту |
|--------|---------|------|---------------------------|
| **Затримка** | 20-100 ms | **5-20 ms** | Краща синхронізація аудіо/відео у реальному часі |
| **Бітрейт** | Фіксований | **Адаптивний (6-510 kbps)** | Економія bandwidth при збереженні якості |
| **Частоти** | Одна на потік | **Ресемплінг на льоту** | Підтримка камер з різною частотою дискретизації |
| **Пакування** | Один кадр на пакет | **1-255 кадрів на пакет** | Менше overhead, краща ефективність мережі |
| **Парсинг** | Простий | **Складний (TOC + Code 0-3)** | Більше метаданих для розумної обробки |

> 🔑 **Головна ідея**: Цей код — **аудіо-аналог** ваших H.264/H.265 парсерів, але з додатковою складністю через адаптивну структуру Opus. Він дає пайплайну:
> 1. Точний розрахунок тривалості кадрів → коректна PTS-синхронізація
> 2. Розуміння режиму кодування → адаптивна обробка (наприклад, пріоритезація SILK для voice, CELT для music)
> 3. Валідний `OpusHead` → сумісність з браузерами (MSE) та FFmpeg

💡 **Фінальна порада**: Додайте інтеграційний тест, який:
1. Генерує Opus пакети з різними `config`/`code` комбінаціями
2. Перевіряє коректність `OpusPacketDuration` проти еталонних значень
3. Валідує згенерований fMP4 init-сегмент через `ffprobe -show_streams`

Це вбереже від тонких багів, які проявляються тільки на реальних потоках з IoT-камер, де Opus використовується через його низьку затримку.