# 🎬 Глибокий розбір: ffmpeg — CGO обгортка для аудіо-транскодування

Цей файл — **CGO-обгортка бібліотеки FFmpeg** для роботи з аудіо-кодеками у бібліотеці `vdk`. Він надає інтерфейси `AudioEncoder`, `AudioDecoder`, `Resampler` для транскодування, ресемплінгу та кодування/декодування аудіо-потоків.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема ffmpeg пакету

```
┌────────────────────────────────────────┐
│ 📦 ffmpeg — CGO FFmpeg Audio Wrapper   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • AudioEncoder — кодування аудіо      │
│  • AudioDecoder — декодування аудіо    │
│  • Resampler — ресемплінг/конвертація  │
│  • ffctx — внутрішній контекст FFmpeg  │
│                                         │
│  🔧 CGO інтеграція:                    │
│  • #include "ffmpeg.h" — C заголовки   │
│  • wrap_avcodec_decode_audio4() — обгортка│
│  • wrap_avresample_convert() — ресемплінг│
│                                         │
│  🔄 Потік даних:                        │
│  av.AudioFrame → Resampler → AudioEncoder → []byte│
│  []byte → AudioDecoder → av.AudioFrame │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Resampler — ресемплінг аудіо-фреймів

### Призначення:
Конвертація аудіо-фреймів між різними параметрами:
- **Sample Rate**: 8kHz → 48kHz (напр. G.711 → AAC)
- **Sample Format**: S16 → FLTP (напр. PCM → Opus)
- **Channel Layout**: MONO → STEREO (напр. downmix/upmix)

### Структура:

```go
type Resampler struct {
    // Вхідні параметри (оновлюються при зміні формату)
    inSampleFormat, inSampleRate int
    inChannelLayout av.ChannelLayout
    
    // Цільові параметри (фіксовані при створенні)
    OutSampleFormat, OutSampleRate int
    OutChannelLayout av.ChannelLayout
    
    // Внутрішній контекст FFmpeg AVAudioResampleContext
    avr *C.AVAudioResampleContext
}
```

### 🔧 Метод `Resample()`:

```go
func (self *Resampler) Resample(in av.AudioFrame) (out av.AudioFrame, err error) {
    // 1. Перевірка чи змінився вхідний формат
    formatChange := in.SampleRate != self.inSampleRate || 
                    in.SampleFormat != self.inSampleFormat || 
                    in.ChannelLayout != self.inChannelLayout
    
    if formatChange {
        // 2. Flush залишкових семплів зі старого контексту
        if self.avr != nil {
            // ... авдіо-флеш логіка ...
        }
        
        // 3. Створення нового AVAudioResampleContext
        C.avresample_free(&self.avr)
        self.inSampleFormat = in.SampleFormat
        self.inSampleRate = in.SampleRate
        self.inChannelLayout = in.ChannelLayout
        
        avr := C.avresample_alloc_context()
        // Налаштування параметрів через av_opt_set_int
        C.av_opt_set_int(unsafe.Pointer(avr), "in_channel_layout", channelLayoutAV2FF(self.inChannelLayout), 0)
        C.av_opt_set_int(unsafe.Pointer(avr), "out_channel_layout", channelLayoutAV2FF(self.OutChannelLayout), 0)
        // ... інші параметри ...
        C.avresample_open(avr)
        self.avr = avr
    }
    
    // 4. Підготовка буферів для вхідних/вихідних даних
    // Обробка planar/non-planar форматів
    // ... виділення пам'яті, unsafe.Pointer конвертації ...
    
    // 5. Виклик avresample_convert через CGO обгортку
    convertSamples := int(C.wrap_avresample_convert(
        self.avr,
        (*C.int)(unsafe.Pointer(&outData[0])), C.int(outLinesize), C.int(outSampleCount),
        (*C.int)(unsafe.Pointer(&inData[0])), C.int(inLinesize), C.int(inSampleCount),
    ))
    
    // 6. Обробка результату та обрізання буферів
    out.SampleCount = convertSamples
    // ... корекція розмірів даних ...
    
    // 7. Додавання flushed даних якщо є
    if flush.SampleCount > 0 {
        out = flush.Concat(out)
    }
    
    return out, nil
}
```

### 🔍 Планарні vs непланарні формати:

```
Планарний (Planar): кожен канал у окремому масиві
  • FLTP: float planar → Data[0] = канал0, Data[1] = канал1...
  • S16P: int16 planar → аналогічно
  
Непланарний (Packed/Interleaved): канали інтерліс у одному масиві
  • FLT: float interleaved → Data[0] = [L0,R0,L1,R1,...]
  • S16: int16 interleaved → аналогічно

Обробка у Resampler:
  if !self.inSampleFormat.IsPlanar() {
      inChannels = 1  // один масив для всіх каналів
      inLinesize = inSampleCount * bytesPerSample * channelCount
  } else {
      inChannels = channelCount  // окремий масив на канал
      inLinesize = inSampleCount * bytesPerSample
  }
```

### ✅ Ваш use-case: конвертація G.711 → AAC для HLS

```go
// ConvertG711ToAAC — ресемплінг + кодування для HLS сумісності
func ConvertG711ToAAC(g711Data []byte, codecType av.CodecType) ([]byte, error) {
    // 1. Створення декодера для G.711
    decoder, err := ffmpeg.NewAudioDecoder(codec.NewPCMMulawCodecData())
    if err != nil {
        return nil, fmt.Errorf("create G.711 decoder: %w", err)
    }
    defer decoder.Close()
    
    // 2. Декодування G.711 → PCM (S16)
    gotFrame, pcmFrame, err := decoder.Decode(g711Data)
    if err != nil || !gotFrame {
        return nil, fmt.Errorf("decode G.711: %w", err)
    }
    
    // 3. Створення ресемплера: 8kHz S16 MONO → 48kHz FLTP STEREO
    resampler := &ffmpeg.Resampler{
        OutSampleFormat:  av.FLTP,
        OutSampleRate:    48000,
        OutChannelLayout: av.CH_STEREO,
    }
    
    // 4. Ресемплінг
    resampled, err := resampler.Resample(pcmFrame)
    if err != nil {
        return nil, fmt.Errorf("resample: %w", err)
    }
    
    // 5. Створення AAC енкодера
    encoder, err := ffmpeg.NewAudioEncoderByCodecType(av.AAC)
    if err != nil {
        return nil, err
    }
    defer encoder.Close()
    
    // Налаштування параметрів енкодера
    encoder.SetSampleRate(48000)
    encoder.SetChannelLayout(av.CH_STEREO)
    encoder.SetSampleFormat(av.FLTP)
    encoder.SetBitrate(128000)
    
    // 6. Кодування у AAC
    aacPkts, err := encoder.Encode(resampled)
    if err != nil {
        return nil, fmt.Errorf("encode AAC: %w", err)
    }
    
    // 7. Об'єднання пакетів (якщо їх кілька)
    var result []byte
    for _, pkt := range aacPkts {
        result = append(result, pkt...)
    }
    
    return result, nil
}
```

---

## 🔑 2. AudioEncoder — кодування аудіо у стиснений формат

### Структура та налаштування:

```go
type AudioEncoder struct {
    ff               *ffctx              // внутрішній FFmpeg контекст
    SampleRate       int                 // цільова частота дискретизації
    Bitrate          int                 // цільовий бітрейт (біт/с)
    ChannelLayout    av.ChannelLayout    // цільова розкладка каналів
    SampleFormat     av.SampleFormat     // цільовий формат семплів
    FrameSampleCount int                 // розмір фрейму для енкодера (з codecCtx.frame_size)
    framebuf         av.AudioFrame       // буфер для накопичення семплів
    codecData        av.AudioCodecData   // метадані для заголовка контейнера
    resampler        *Resampler          // внутрішній ресемплер для авто-конвертації
}
```

### 🔧 Методи налаштування:

```go
// SetSampleFormat — встановлення цільового формату семплів
func (self *AudioEncoder) SetSampleFormat(fmt av.SampleFormat) error {
    self.SampleFormat = fmt
    return nil
}

// SetSampleRate — встановлення цільової частоти дискретизації
func (self *AudioEncoder) SetSampleRate(rate int) error {
    self.SampleRate = rate
    return nil
}

// SetChannelLayout — встановлення цільової розкладки каналів
func (self *AudioEncoder) SetChannelLayout(ch av.ChannelLayout) error {
    self.ChannelLayout = ch
    return nil
}

// SetBitrate — встановлення цільового бітрейту
func (self *AudioEncoder) SetBitrate(bitrate int) error {
    self.Bitrate = bitrate
    return nil
}

// SetOption — встановлення довільних опцій FFmpeg
func (self *AudioEncoder) SetOption(key string, val interface{}) error {
    ff := &self.ff.ff
    sval := fmt.Sprint(val)
    
    // Спеціальна обробка profile
    if key == "profile" {
        ff.profile = C.avcodec_profile_name_to_int(ff.codec, C.CString(sval))
        if ff.profile == C.FF_PROFILE_UNKNOWN {
            return fmt.Errorf("ffmpeg: profile `%s` invalid", sval)
        }
        return nil
    }
    
    // Загальні опції через av_dict_set
    C.av_dict_set(&ff.options, C.CString(key), C.CString(sval), 0)
    return nil
}
```

### 🔧 Метод `Setup()` — ініціалізація енкодера:

```go
func (self *AudioEncoder) Setup() (err error) {
    ff := &self.ff.ff
    
    // 1. Виділення AVFrame
    ff.frame = C.av_frame_alloc()
    
    // 2. Встановлення дефолтних значень якщо не задано
    if self.SampleFormat == av.SampleFormat(0) {
        self.SampleFormat = sampleFormatFF2AV(*ff.codec.sample_fmts)
    }
    if self.SampleRate == 0 {
        self.SampleRate = 44100  // дефолт 44.1kHz
    }
    if self.ChannelLayout == av.ChannelLayout(0) {
        self.ChannelLayout = av.CH_STEREO  // дефолт стерео
    }
    
    // 3. Налаштування AVCodecContext
    ff.codecCtx.sample_fmt = sampleFormatAV2FF(self.SampleFormat)
    ff.codecCtx.sample_rate = C.int(self.SampleRate)
    ff.codecCtx.bit_rate = C.int64_t(self.Bitrate)
    ff.codecCtx.channel_layout = channelLayoutAV2FF(self.ChannelLayout)
    ff.codecCtx.strict_std_compliance = C.FF_COMPLIANCE_EXPERIMENTAL
    ff.codecCtx.flags = C.AV_CODEC_FLAG_GLOBAL_HEADER  // важливо для HLS!
    ff.codecCtx.profile = ff.profile
    
    // 4. Відкриття кодека
    if C.avcodec_open2(ff.codecCtx, ff.codec, nil) != 0 {
        return fmt.Errorf("ffmpeg: encoder: avcodec_open2 failed")
    }
    
    // 5. Оновлення параметрів з реального кодека
    self.SampleFormat = sampleFormatFF2AV(ff.codecCtx.sample_fmt)
    self.FrameSampleCount = int(ff.codecCtx.frame_size)  // критично для AAC!
    
    // 6. Отримання extradata для заголовка контейнера
    extradata := C.GoBytes(unsafe.Pointer(ff.codecCtx.extradata), ff.codecCtx.extradata_size)
    
    // 7. Створення CodecData залежно від типу кодека
    switch ff.codecCtx.codec_id {
    case C.AV_CODEC_ID_AAC:
        self.codecData, err = aacparser.NewCodecDataFromMPEG4AudioConfigBytes(extradata)
    default:
        self.codecData = audioCodecData{
            channelLayout: self.ChannelLayout,
            sampleFormat:  self.SampleFormat,
            sampleRate:    self.SampleRate,
            codecId:       ff.codecCtx.codec_id,
            extradata:     extradata,
        }
    }
    
    return err
}
```

### 🔧 Метод `Encode()` — основна логіка кодування:

```go
func (self *AudioEncoder) Encode(frame av.AudioFrame) (pkts [][]byte, err error) {
    // 1. Авто-ресемплінг якщо параметри не співпадають
    if frame.SampleFormat != self.SampleFormat || 
       frame.ChannelLayout != self.ChannelLayout || 
       frame.SampleRate != self.SampleRate {
        if frame, err = self.resample(frame); err != nil {
            return nil, err
        }
    }
    
    // 2. Обробка фреймів різного розміру (буферизація)
    if self.FrameSampleCount != 0 {
        // AAC вимагає фіксованого розміру фрейму (напр. 1024 семпли)
        if self.framebuf.SampleCount == 0 {
            self.framebuf = frame  // перший фрейм
        } else {
            self.framebuf = self.framebuf.Concat(frame)  // накопичення
        }
        
        // Кодування поки є достатньо даних
        for self.framebuf.SampleCount >= self.FrameSampleCount {
            // Витягування фрейму потрібного розміру
            frame := self.framebuf.Slice(0, self.FrameSampleCount)
            
            // Кодування одного фрейму
            gotpkt, pkt, err := self.encodeOne(frame)
            if err != nil {
                return nil, err
            }
            if gotpkt {
                pkts = append(pkts, pkt)
            }
            
            // Видалення оброблених даних з буфера
            self.framebuf = self.framebuf.Slice(self.FrameSampleCount, self.framebuf.SampleCount)
        }
    } else {
        // Кодеки зі змінним розміром фрейму (напр. Opus)
        gotpkt, pkt, err := self.encodeOne(frame)
        if err != nil {
            return nil, err
        }
        if gotpkt {
            pkts = append(pkts, pkt)
        }
    }
    
    return pkts, nil
}
```

### ✅ Ваш use-case: створення AAC енкодера для HLS

```go
// CreateAACForHLS — налаштування AAC енкодера для HLS сумісності
func CreateAACForHLS(sampleRate int, channels int, bitrate int) (*ffmpeg.AudioEncoder, error) {
    // 1. Створення енкодера за типом кодека
    encoder, err := ffmpeg.NewAudioEncoderByCodecType(av.AAC)
    if err != nil {
        return nil, fmt.Errorf("create AAC encoder: %w", err)
    }
    
    // 2. Налаштування параметрів для HLS
    encoder.SetSampleRate(sampleRate)  // 44100 або 48000
    encoder.SetChannelLayout(func() av.ChannelLayout {
        if channels == 1 { return av.CH_MONO }
        return av.CH_STEREO
    }())
    encoder.SetSampleFormat(av.FLTP)  // AAC працює з float planar
    encoder.SetBitrate(bitrate)        // 128000 для 128 kbps
    
    // 3. Налаштування профілю для сумісності
    encoder.SetOption("profile", "aac_low")  // AAC-LC для широкої сумісності
    
    // 4. Ініціалізація (Setup викликається автоматично при першому Encode)
    return encoder, nil
}

// Використання у транскодері:
aacEnc, err := CreateAACForHLS(48000, 2, 128000)
if err != nil { /* handle error */ }
defer aacEnc.Close()

// Кодування аудіо-фрейму
aacPackets, err := aacEnc.Encode(pcmFrame)
if err != nil { /* handle error */ }

// Отримання метаданих для заголовка HLS
codecData, err := aacEnc.CodecData()
if err != nil { /* handle error */ }
// codecData містить AudioSpecificConfig для WriteHeader()
```

---

## 🔑 3. AudioDecoder — декодування стисненого аудіо у PCM

### Структура та ініціалізація:

```go
type AudioDecoder struct {
    ff            *ffctx              // внутрішній FFmpeg контекст
    ChannelLayout av.ChannelLayout    // вихідна розкладка каналів
    SampleFormat  av.SampleFormat     // вихідний формат семплів
    SampleRate    int                 // вихідна частота дискретизації
    Extradata     []byte              // extradata для ініціалізації (SPS/PPS для AAC)
}
```

### 🔧 Метод `NewAudioDecoder()` — фабрика декодерів:

```go
func NewAudioDecoder(codec av.AudioCodecData) (dec *AudioDecoder, err error) {
    _dec := &AudioDecoder{}
    var id uint32
    
    // Визначення FFmpeg codec_id за типом з av.CodecType
    switch codec.Type() {
    case av.AAC:
        // AAC вимагає extradata (AudioSpecificConfig)
        if aaccodec, ok := codec.(aacparser.CodecData); ok {
            _dec.Extradata = aaccodec.MPEG4AudioConfigBytes()
            id = C.AV_CODEC_ID_AAC
        } else {
            return nil, fmt.Errorf("ffmpeg: aac CodecData must be aacparser.CodecData")
        }
    case av.SPEEX:
        id = C.AV_CODEC_ID_SPEEX
    case av.PCM_MULAW:
        id = C.AV_CODEC_ID_PCM_MULAW
    case av.PCM_ALAW:
        id = C.AV_CODEC_ID_PCM_ALAW
    default:
        // Fallback для кастомних codecData
        if ffcodec, ok := codec.(audioCodecData); ok {
            _dec.Extradata = ffcodec.extradata
            id = ffcodec.codecId
        } else {
            return nil, fmt.Errorf("ffmpeg: invalid CodecData for ffmpeg to decode")
        }
    }
    
    // Пошук декодера у FFmpeg
    c := C.avcodec_find_decoder(id)
    if c == nil || C.avcodec_get_type(c.id) != C.AVMEDIA_TYPE_AUDIO {
        return nil, fmt.Errorf("ffmpeg: cannot find audio decoder id=%d", id)
    }
    
    // Створення внутрішнього контексту
    if _dec.ff, err = newFFCtxByCodec(c); err != nil {
        return nil, err
    }
    
    // Збереження параметрів з codecData
    _dec.SampleFormat = codec.SampleFormat()
    _dec.SampleRate = codec.SampleRate()
    _dec.ChannelLayout = codec.ChannelLayout()
    
    // Ініціалізація декодера
    if err = _dec.Setup(); err != nil {
        return nil, err
    }
    
    return _dec, nil
}
```

### 🔧 Метод `Decode()` — декодування пакету у фрейм:

```go
func (self *AudioDecoder) Decode(pkt []byte) (gotframe bool, frame av.AudioFrame, err error) {
    ff := &self.ff.ff
    
    // 1. Підготовка змінних для CGO виклику
    cgotframe := C.int(0)
    
    // 2. Виклик обгорнутої функції avcodec_decode_audio4
    cerr := C.wrap_avcodec_decode_audio4(
        ff.codecCtx,           // AVCodecContext
        ff.frame,              // AVFrame для виходу
        unsafe.Pointer(&pkt[0]), // вхідні дані
        C.int(len(pkt)),       // розмір вхідних даних
        &cgotframe,            // прапорець: чи отримано фрейм
    )
    
    if cerr < C.int(0) {
        return false, av.AudioFrame{}, fmt.Errorf("ffmpeg: avcodec_decode_audio4 failed: %d", cerr)
    }
    
    // 3. Обробка результату
    if cgotframe != C.int(0) {
        gotframe = true
        // Конвертація C.AVFrame → av.AudioFrame
        audioFrameAssignToAV(ff.frame, &frame)
        frame.SampleRate = self.SampleRate  // збереження вихідної частоти
        
        if debug {
            fmt.Println("ffmpeg: Decode", frame.SampleCount, frame.SampleRate, 
                       frame.ChannelLayout, frame.SampleFormat)
        }
    }
    
    return gotframe, frame, nil
}
```

### ✅ Ваш use-case: декодування AAC для обробки

```go
// DecodeAACForProcessing — декодування AAC → PCM для аналізу/транскодування
func DecodeAACForProcessing(aacData []byte, codecData av.AudioCodecData) (av.AudioFrame, error) {
    // 1. Створення декодера
    decoder, err := ffmpeg.NewAudioDecoder(codecData)
    if err != nil {
        return av.AudioFrame{}, fmt.Errorf("create decoder: %w", err)
    }
    defer decoder.Close()
    
    // 2. Декодування пакету
    gotFrame, pcmFrame, err := decoder.Decode(aacData)
    if err != nil {
        return av.AudioFrame{}, fmt.Errorf("decode AAC: %w", err)
    }
    if !gotFrame {
        return av.AudioFrame{}, fmt.Errorf("no frame decoded from AAC packet")
    }
    
    // 3. PCM фрейм готовий для подальшої обробки
    // Напр.: аналіз гучності, детекція мови, ресемплінг тощо
    return pcmFrame, nil
}
```

---

## 🔧 4. Допоміжні функції конвертації

### Конвертація SampleFormat між av та FFmpeg:

```go
// sampleFormatAV2FF — конвертація з av.SampleFormat у FFmpeg константи
func sampleFormatAV2FF(sampleFormat av.SampleFormat) (ffsamplefmt int32) {
    switch sampleFormat {
    case av.U8:   return C.AV_SAMPLE_FMT_U8
    case av.S16:  return C.AV_SAMPLE_FMT_S16
    case av.S32:  return C.AV_SAMPLE_FMT_S32
    case av.FLT:  return C.AV_SAMPLE_FMT_FLT
    case av.DBL:  return C.AV_SAMPLE_FMT_DBL
    case av.U8P:  return C.AV_SAMPLE_FMT_U8P
    case av.S16P: return C.AV_SAMPLE_FMT_S16P
    case av.S32P: return C.AV_SAMPLE_FMT_S32P
    case av.FLTP: return C.AV_SAMPLE_FMT_FLTP
    case av.DBLP: return C.AV_SAMPLE_FMT_DBLP
    }
    return 0
}

// sampleFormatFF2AV — зворотна конвертація
func sampleFormatFF2AV(ffsamplefmt int32) (sampleFormat av.SampleFormat) {
    switch ffsamplefmt {
    case C.AV_SAMPLE_FMT_U8:  return av.U8
    case C.AV_SAMPLE_FMT_S16: return av.S16
    case C.AV_SAMPLE_FMT_S32: return av.S32
    case C.AV_SAMPLE_FMT_FLT: return av.FLT
    case C.AV_SAMPLE_FMT_DBL: return av.DBL
    case C.AV_SAMPLE_FMT_U8P: return av.U8P
    case C.AV_SAMPLE_FMT_S16P: return av.S16P
    case C.AV_SAMPLE_FMT_S32P: return av.S32P
    case C.AV_SAMPLE_FMT_FLTP: return av.FLTP
    case C.AV_SAMPLE_FMT_DBLP: return av.DBLP
    }
    return 0
}
```

### Конвертація ChannelLayout між av та FFmpeg:

```go
// channelLayoutAV2FF — конвертація з av.ChannelLayout у FFmpeg бітову маску
func channelLayoutAV2FF(channelLayout av.ChannelLayout) (layout C.uint64_t) {
    if channelLayout&av.CH_FRONT_CENTER != 0 { layout |= C.AV_CH_FRONT_CENTER }
    if channelLayout&av.CH_FRONT_LEFT != 0  { layout |= C.AV_CH_FRONT_LEFT }
    if channelLayout&av.CH_FRONT_RIGHT != 0 { layout |= C.AV_CH_FRONT_RIGHT }
    if channelLayout&av.CH_BACK_CENTER != 0 { layout |= C.AV_CH_BACK_CENTER }
    if channelLayout&av.CH_BACK_LEFT != 0   { layout |= C.AV_CH_BACK_LEFT }
    if channelLayout&av.CH_BACK_RIGHT != 0  { layout |= C.AV_CH_BACK_RIGHT }
    if channelLayout&av.CH_SIDE_LEFT != 0   { layout |= C.AV_CH_SIDE_LEFT }
    if channelLayout&av.CH_SIDE_RIGHT != 0  { layout |= C.AV_CH_SIDE_RIGHT }
    if channelLayout&av.CH_LOW_FREQ != 0    { layout |= C.AV_CH_LOW_FREQUENCY }
    return layout
}

// channelLayoutFF2AV — зворотна конвертація
func channelLayoutFF2AV(layout C.uint64_t) (channelLayout av.ChannelLayout) {
    if layout&C.AV_CH_FRONT_CENTER != 0 { channelLayout |= av.CH_FRONT_CENTER }
    if layout&C.AV_CH_FRONT_LEFT != 0  { channelLayout |= av.CH_FRONT_LEFT }
    if layout&C.AV_CH_FRONT_RIGHT != 0 { channelLayout |= av.CH_FRONT_RIGHT }
    // ... інші канали ...
    return channelLayout
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// audio_transcoder.go — транскодування аудіо для HLS з використанням ffmpeg
type AudioTranscoder struct {
    channelID    string
    inputCodec   av.AudioCodecData
    outputCodec  av.CodecType  // цільовий кодек: av.AAC, av.OPUS тощо
    decoder      *ffmpeg.AudioDecoder
    encoder      *ffmpeg.AudioEncoder
    resampler    *ffmpeg.Resampler
    metrics      *AudioMetrics
}

func NewAudioTranscoder(channelID string, inputCodec av.AudioCodecData, outputCodec av.CodecType) (*AudioTranscoder, error) {
    // 1. Створення декодера для вхідного кодека
    decoder, err := ffmpeg.NewAudioDecoder(inputCodec)
    if err != nil {
        return nil, fmt.Errorf("create decoder: %w", err)
    }
    
    // 2. Створення енкодера для цільового кодека
    var encoder *ffmpeg.AudioEncoder
    switch outputCodec {
    case av.AAC:
        encoder, err = ffmpeg.NewAudioEncoderByCodecType(av.AAC)
        if err != nil {
            decoder.Close()
            return nil, fmt.Errorf("create AAC encoder: %w", err)
        }
        // Налаштування для HLS
        encoder.SetSampleRate(48000)
        encoder.SetChannelLayout(av.CH_STEREO)
        encoder.SetSampleFormat(av.FLTP)
        encoder.SetBitrate(128000)
        encoder.SetOption("profile", "aac_low")
        
    case av.OPUS:
        encoder, err = ffmpeg.NewAudioEncoderByName("libopus")
        if err != nil {
            decoder.Close()
            return nil, fmt.Errorf("create Opus encoder: %w", err)
        }
        // Налаштування для Opus
        encoder.SetSampleRate(48000)
        encoder.SetChannelLayout(av.CH_STEREO)
        encoder.SetSampleFormat(av.FLTP)
        encoder.SetBitrate(64000)
        
    default:
        decoder.Close()
        return nil, fmt.Errorf("unsupported output codec: %v", outputCodec)
    }
    
    return &AudioTranscoder{
        channelID:   channelID,
        inputCodec:  inputCodec,
        outputCodec: outputCodec,
        decoder:     decoder,
        encoder:     encoder,
        metrics:     NewAudioMetrics(channelID),
    }, nil
}

// TranscodePacket — транскодування одного аудіо-пакету
func (t *AudioTranscoder) TranscodePacket(inputPkt []byte) ([][]byte, error) {
    start := time.Now()
    
    // 1. Декодування вхідного пакету у PCM
    gotFrame, pcmFrame, err := t.decoder.Decode(inputPkt)
    if err != nil {
        return nil, fmt.Errorf("decode: %w", err)
    }
    if !gotFrame {
        return nil, nil  // немає фрейму для кодування
    }
    
    t.metrics.DecodeLatency.Observe(time.Since(start).Seconds())
    
    // 2. Кодування у цільовий формат
    outputPkts, err := t.encoder.Encode(pcmFrame)
    if err != nil {
        return nil, fmt.Errorf("encode: %w", err)
    }
    
    t.metrics.EncodeLatency.Observe(time.Since(start).Seconds())
    t.metrics.PacketsTranscoded.Inc()
    
    return outputPkts, nil
}

// GetOutputCodecData — отримання метаданих для заголовка контейнера
func (t *AudioTranscoder) GetOutputCodecData() (av.AudioCodecData, error) {
    return t.encoder.CodecData()
}

// Close — закриття ресурсів
func (t *AudioTranscoder) Close() {
    if t.decoder != nil {
        t.decoder.Close()
    }
    if t.encoder != nil {
        t.encoder.Close()
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"avcodec_open2 failed"** | Неправильні параметри енкодера або відсутній кодек у FFmpeg | Переконайтеся, що FFmpeg зібрано з підтримкою потрібних кодеків (libfdk-aac для AAC, libopus для Opus); перевірте параметри через `SetOption()` |
| **"aac CodecData must be aacparser.CodecData"** | Неправильний тип `codecData` для AAC декодера | Завжди використовуйте `aacparser.CodecData` для AAC; не передавайте `fake.CodecData` у реальний декодер |
| **Ресемплінг не працює** | Планарні/непланарні формати не обробляються коректно | Переконайтеся, що `IsPlanar()` перевірка коректна; для interleaved форматів `inChannels=1`, для planar `inChannels=channelCount` |
| **Пам'ять не звільняється** | `Close()` не викликається для енкодерів/декодерів | Завжди використовуйте `defer encoder.Close()` після успішного створення; FFmpeg контексти вимагають явного закриття |
| **CGO помилки компіляції** | Відсутні заголовні файли FFmpeg або неправильні шляхи | Встановіть `libavcodec-dev`, `libavresample-dev` (або `libswresample-dev` для нових версій); налаштуйте `CGO_CFLAGS`/`CGO_LDFLAGS` |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування енкодерів/декодерів на рівні каналу:

```go
type CodecCache struct {
    mu       sync.RWMutex
    encoders map[string]*ffmpeg.AudioEncoder  // channelID → encoder
    decoders map[string]*ffmpeg.AudioDecoder  // channelID → decoder
}

func (c *CodecCache) GetEncoder(channelID string) (*ffmpeg.AudioEncoder, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    enc, ok := c.encoders[channelID]
    return enc, ok
}

func (c *CodecCache) SetEncoder(channelID string, enc *ffmpeg.AudioEncoder) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.encoders[channelID] = enc
}
```

### 2. Пакетне кодування для зменшення накладних витрат:

```go
// BatchEncode — кодування кількох фреймів за один виклик
func (e *AudioEncoder) BatchEncode(frames []av.AudioFrame) ([][]byte, error) {
    var allPkts [][]byte
    
    for _, frame := range frames {
        pkts, err := e.Encode(frame)
        if err != nil {
            return allPkts, err
        }
        allPkts = append(allPkts, pkts...)
    }
    return allPkts, nil
}
```

### 3. Моніторинг продуктивності кодування:

```go
type FFmpegMetrics struct {
    EncodeLatency   prometheus.HistogramVec
    DecodeLatency   prometheus.HistogramVec
    ResampleLatency prometheus.HistogramVec
    PacketsProcessed prometheus.CounterVec
}

func (m *FFmpegMetrics) RecordEncode(duration time.Duration, channelID string) {
    m.EncodeLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    m.PacketsProcessed.WithLabelValues("encode", channelID).Inc()
}
```

---

## 📋 Чек-лист інтеграції ffmpeg пакету

```go
// ✅ 1. Перевірка наявності FFmpeg бібліотек
// Перед компіляцією:
// sudo apt-get install libavcodec-dev libavresample-dev (або libswresample-dev)

// ✅ 2. Створення енкодера/декодера з правильними параметрами
encoder, err := ffmpeg.NewAudioEncoderByCodecType(av.AAC)
if err != nil { /* handle error */ }
encoder.SetSampleRate(48000)
encoder.SetChannelLayout(av.CH_STEREO)
encoder.SetSampleFormat(av.FLTP)
encoder.SetBitrate(128000)

// ✅ 3. Ініціалізація перед використанням
// Setup() викликається автоматично при першому Encode(), 
// але можна викликати вручну для ранньої перевірки помилок
if err := encoder.Setup(); err != nil { /* handle error */ }

// ✅ 4. Обробка помилок кодування/декодування
pkts, err := encoder.Encode(pcmFrame)
if err != nil {
    log.Printf("encode failed: %v", err)
    // Спроба відновлення: скидання енкодера, логування метрик
    return nil, err
}

// ✅ 5. Закриття ресурсів
defer encoder.Close()
defer decoder.Close()

// ✅ 6. Метрики для моніторингу
start := time.Now()
// ... кодування/декодування ...
metrics.EncodeLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [vdk ffmpeg Package](https://pkg.go.dev/github.com/deepch/vdk/codec/ffmpeg) — GoDoc documentation
- 📄 [FFmpeg Libavcodec Documentation](https://ffmpeg.org/doxygen/trunk/group__lavc.html) — API довідник
- 📄 [AAC Encoding Guide](https://trac.ffmpeg.org/wiki/Encode/AAC) — практичні поради для AAC
- 📄 [Opus Encoding Guide](https://trac.ffmpeg.org/wiki/Encode/Opus) — практичні поради для Opus
- 🧪 [CGO Documentation](https://golang.org/cmd/cgo/) — довідка по інтеграції C/Go

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **аудіо в реальному часі**:
> 1. **Кешуйте енкодери/декодери на рівні каналу** — створення нових об'єктів для кожного пакету дороге через CGO виклики.
> 2. **Використовуйте `AV_CODEC_FLAG_GLOBAL_HEADER`** для AAC — це критично для коректного запису заголовків у HLS.
> 3. **Моніторьте латентність `Encode()`/`Decode()`** — різке зростання може вказувати на перевантаження або проблеми з FFmpeg.
> 4. **Обробляйте планарні/непланарні формати коректно** — помилки у `IsPlanar()` перевірці можуть призвести до пошкодження даних.
> 5. **Завжди викликайте `Close()`** — FFmpeg контексти займають нативну пам'ять, яка не збирається GC Go.

Потрібен приклад інтеграції `AudioTranscoder` з вашим `pubsub.Queue` для розподілу вже транскодованих аудіо-пакетів між підписниками (HLS, WebSocket, архів)? Готовий допомогти! 🚀