# 🎬 Глибокий розбір `flv/muxer.go` — FLV мультиплексор для CCTV HLS Processor

Це **архітектурне ядро** для пакування медіа-кадрів у формат FLV. Модуль приймає сирі бітстріми (Annex-B для відео, ADTS для AAC), конвертує їх у FLV-сумісний формат (AVCC/hvcc для параметрів, NAL units без start code для даних) та генерує готові теги для запису або ретрансляції. Розберемо детально:

---

## 🧱 1. Архітектура: інтерфейси та ієрархія

### 🔑 Ключова абстракція: `AVTagMuxer`

```go
type AVTagMuxer interface {
    Write(frames []byte, pts uint32, dts uint32) [][]byte
}
```

**Чому `[][]byte` на виході?** Один вхідний кадр може породити **кілька FLV-тегів**:

```
Вхід: [SPS][PPS][IDR-frame] (Annex-B)
Вихід: 
├─ Тег 1: Sequence Header (SPS+PPS у AVCC форматі)
└─ Тег 2: NALU (IDR кадр у AVCC форматі)
```

### 🗂️ Ієрархія муксерів:

```
FlvMuxer (оркестратор)
├─ videoMuxer: AVTagMuxer
│  ├─ AVCMuxer (H.264)
│  └─ HevcMuxer (H.265)
└─ audioMuxer: AVTagMuxer
   ├─ AACMuxer
   ├─ G711AMuxer / G711UMuxer
   └─ Mp3Muxer
```

> 💡 **Практичне значення**: `FlvMuxer` делегує специфічну логіку кодекам, залишаючи собі лише загальне пакування у теги. Це дозволяє додавати нові кодеки (VP8, Opus) без зміни ядра.

---

## 🎞️ 2. `WriteVideoTag` / `WriteAudioTag` — генерація payload тегів

### 🔧 `WriteVideoTag`: формування відео-заголовку

```go
func WriteVideoTag(data []byte, isKey bool, cid FLV_VIDEO_CODEC_ID, cts int32, isSequenceHeader bool) []byte {
    var vtag VideoTag
    vtag.CodecId = uint8(cid)
    vtag.CompositionTime = cts  // CTS для B-frames
    vtag.FrameType = uint8(KEY_FRAME)      // 1, якщо isKey
    vtag.FrameType = uint8(INTER_FRAME)    // 2, якщо ні
    vtag.AVCPacketType = uint8(AVC_SEQUENCE_HEADER)  // 0, якщо isSequenceHeader
    vtag.AVCPacketType = uint8(AVC_NALU)             // 1, якщо ні
    
    tagData := vtag.Encode()  // Кодує 5-байтовий VideoTag header
    tagData = append(tagData, data...)  // + payload
    return tagData
}
```

### 🔍 Структура VideoTag header (5 байт для AVC/HEVC):

```
Байт 0: [FrameType:4][CodecID:4]
        ├─ FrameType: 1=Key, 2=Inter
        └─ CodecID: 7=AVC, 12=HEVC
Байт 1: AVCPacketType
        ├─ 0 = Sequence Header (SPS/PPS/VPS)
        ├─ 1 = NALU (відео-кадр)
        └─ 2 = End of Sequence
Байти 2-4: CompositionTime (signed 24-bit)
           ├─ PTS - DTS для B-frames
           └─ 0 для потоків без B-frames
```

### 🎯 Приклад: запис IDR-кадру H.264

```go
// Вхід: Annex-B кадр з SPS+PPS+IDR
frames := []byte{0x00,0x00,0x00,0x01,0x67..., 0x00,0x00,0x00,0x01,0x68..., 0x00,0x00,0x00,0x01,0x65...}

// AVCMuxer.Write() розділить на:
// 1. Sequence Header тег:
//    [VideoTag header: 0x17 0x00 0x00 0x00 0x00][AVCC extradata: SPS+PPS]
// 2. NALU тег:
//    [VideoTag header: 0x17 0x01 0x00 0x00 0x00][AVCC NAL units: IDR]
```

---

## 🎵 3. `WriteAudioTag` — формування аудіо-заголовку

### 🔧 Реалізація з урахуванням особливостей AAC:

```go
func WriteAudioTag(data []byte, cid FLV_SOUND_FORMAT, sampleRate int, channelCount int, isSequenceHeader bool) []byte {
    var atag AudioTag
    atag.SoundFormat = uint8(cid)  // 10=AAC, 8=G711A, 9=G711U, 2=MP3
    
    // AAC має фіксовані параметри у заголовку тега
    if cid == FLV_AAC {
        atag.SoundRate = uint8(FLV_SAMPLE_44000)  // Завжди 44.1kHz у заголовку!
        atag.SoundSize = 1   // 16-bit samples
        atag.SoundType = 1   // Stereo
    } else {
        // G.711/MP3: параметри з аргументів
        switch sampleRate {
        case 5500: atag.SoundRate = uint8(FLV_SAMPLE_5500)
        case 11025: atag.SoundRate = uint8(FLV_SAMPLE_11000)
        case 22050: atag.SoundRate = uint8(FLV_SAMPLE_22000)
        case 44100: atag.SoundRate = uint8(FLV_SAMPLE_44000)
        default: atag.SoundRate = uint8(FLV_SAMPLE_44000)  // fallback
        }
        atag.SoundSize = 1  // 16-bit
        atag.SoundType = 0  // Mono
        if channelCount > 1 { atag.SoundType = 1 }  // Stereo
    }

    // AACPacketType: 0=Sequence Header (ASC), 1=Raw AAC frame
    if isSequenceHeader {
        atag.AACPacketType = 0
    } else {
        atag.AACPacketType = 1
    }
    
    tagData := atag.Encode()  // 1-2 байти залежно від кодеку
    tagData = append(tagData, data...)  // + payload
    return tagData
}
```

### ⚠️ Важлива особливість AAC у FLV:

```
FLV заголовок для AAC завжди вказує 44.1kHz/stereo/16-bit,
незалежно від реального потоку!

Реальні параметри зберігаються у AudioSpecificConfig (ASC),
який передається у Sequence Header тегу.

Це означає, що плеєр має ігнорувати SoundRate/SoundType у заголовку
і читати параметри з ASC. Ваш код коректно це обробляє.
```

---

## 🎞️ 4. `AVCMuxer` — мультиплексинг H.264

### 🔑 Ключова логіка: конвертація Annex-B → AVCC + кешування параметрів

```go
type AVCMuxer struct {
    spsset map[uint64][]byte  // кеш SPS за ID
    ppsset map[uint64][]byte  // кеш PPS за ID
    cache  []byte             // буфер для NAL units поточного кадру
    first  bool               // прапорець: чи потрібен Sequence Header
}
```

### 🔧 Метод `Write`: крок за кроком

#### Крок 1: Розділення вхідного потоку на NAL units

```go
codec.SplitFrameWithStartCode(frames, func(nalu []byte) bool {
    naltype := codec.H264NaluType(nalu)
    
    switch naltype {
    case codec.H264_NAL_SPS:
        // 1. Витягнути ID параметра
        spsid := codec.GetSPSIdWithStartCode(nalu)
        
        // 2. Перевірити, чи новий/змінений
        s, found := muxer.spsset[spsid]
        if !found || !bytes.Equal(s, nalu) {
            // 3. Зберегти у кеш (clone для уникнення aliasing)
            naluCopy := make([]byte, len(nalu))
            copy(naluCopy, nalu)
            muxer.spsset[spsid] = naluCopy
            
            // 4. Конвертувати Annex-B → AVCC для кешу extradata
            muxer.cache = append(muxer.cache, codec.ConvertAnnexBToAVCC(nalu)...)
        }
        
    case codec.H264_NAL_PPS:
        // Аналогічно для PPS...
        
    default:
        // VCL NAL units (слайси)
        if naltype <= codec.H264_NAL_I_SLICE {
            vcl = true  // позначити, що є відео-дані
            if naltype == codec.H264_NAL_I_SLICE {
                isKey = true  // IDR frame
            }
        }
        // Конвертувати у AVCC формат для FLV
        muxer.cache = append(muxer.cache, codec.ConvertAnnexBToAVCC(nalu)...)
    }
    return true  // продовжити ітерацію
})
```

#### Крок 2: Генерація Sequence Header (тільки перший раз)

```go
if muxer.first && len(muxer.ppsset) > 0 && len(muxer.spsset) > 0 {
    // 1. Зібрати SPS/PPS у масиви
    spss := make([][]byte, len(muxer.spsset))
    idx := 0
    for _, sps := range muxer.spsset { spss[idx] = sps; idx++ }
    
    ppss := make([][]byte, len(muxer.ppsset))
    idx = 0
    for _, pps := range muxer.ppsset { ppss[idx] = pps; idx++ }
    
    // 2. Створити AVCC extradata (ISO/IEC 14496-15 формат)
    extraData, _ := codec.CreateH264AVCCExtradata(spss, ppss)
    
    // 3. Сформувати Sequence Header тег
    tags = append(tags, WriteVideoTag(extraData, true, FLV_AVC, 0, true))
    
    muxer.first = false  // більше не генерувати
}
```

#### Крок 3: Генерація NALU тега з відео-даними

```go
if vcl {
    // CTS = PTS - DTS (для B-frames)
    cts := int32(pts - dts)
    
    // Сформувати тег з кешованими NAL units у AVCC форматі
    tags = append(tags, WriteVideoTag(muxer.cache, isKey, FLV_AVC, cts, false))
    
    // Очистити кеш для наступного кадру
    muxer.cache = muxer.cache[:0]
}
return tags
```

### 🎯 Чому кешування параметрів критичне:

| Сценарій | Без кешування | З кешуванням у `AVCMuxer` |
|----------|--------------|--------------------------|
| **IDR без SPS/PPS** | FLV валідний, але декодер не може розпакувати → артефакти | Sequence Header тег додається автоматично перед першим IDR |
| **Зміна параметрів** | Нові SPS ігноруються → розсинхронізація | Нові SPS/PPS оновлюють кеш і extradata |
| **Повторні IDR** | SPS/PPS дублюються у кожному тегу → bandwidth waste | Sequence Header генерується тільки один раз |

---

## 🌀 5. `HevcMuxer` — мультиплексинг H.265

### 🔑 Відмінності від H.264:

```go
type HevcMuxer struct {
    hvcc  *codec.HEVCRecordConfiguration  // hvcc замість окремих SPS/PPS мап
    cache []byte
    first bool
}
```

### 🔧 Ключова логіка:

```go
func (muxer *HevcMuxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    codec.SplitFrameWithStartCode(frames, func(nalu []byte) bool {
        naltype := codec.H265NaluType(nalu)
        
        switch naltype {
        case codec.H265_NAL_SPS:
            // Інкрементальне оновлення hvcc (не заміна!)
            muxer.hvcc.UpdateSPS(nalu)
            muxer.cache = append(muxer.cache, codec.ConvertAnnexBToAVCC(nalu)...)
            
        case codec.H265_NAL_PPS:
            muxer.hvcc.UpdatePPS(nalu)
            muxer.cache = append(muxer.cache, codec.ConvertAnnexBToAVCC(nalu)...)
            
        case codec.H265_NAL_VPS:
            // VPS — новий параметр для H.265, відсутній у H.264
            muxer.hvcc.UpdateVPS(nalu)
            muxer.cache = append(muxer.cache, codec.ConvertAnnexBToAVCC(nalu)...)
            
        default:
            // Детекція IRAP frames (аналог IDR у H.264)
            if naltype >= 16 && naltype <= 21 {  // BLA/CRA/IDR types
                isKey = true
            }
            vcl = codec.IsH265VCLNaluType(naltype)
            muxer.cache = append(muxer.cache, codec.ConvertAnnexBToAVCC(nalu)...)
        }
        return true
    })
    
    // Sequence Header: hvcc → байти
    if muxer.first && len(muxer.hvcc.Arrays) > 0 {
        extraData, _ := muxer.hvcc.Encode()  // серіалізація hvcc структури
        tags = append(tags, WriteVideoTag(extraData, true, FLV_HEVC, 0, true))
        muxer.first = false
    }
    
    // NALU тег
    if vcl {
        tags = append(tags, WriteVideoTag(muxer.cache, isKey, FLV_HEVC, int32(pts-dts), false))
        muxer.cache = muxer.cache[:0]
    }
    return tags
}
```

### 🎯 Чому `hvcc` складніший за окремі SPS/PPS:

```
H.265 hvcc (HEVCDecoderConfigurationRecord) містить:
├─ ProfileTierLevel (профіль/рівень кодування)
├─ ChromaFormat, BitDepth (колірна підвибірка, бітова глибина)
├─ MinSpatialSegmentationIdc (для паралельного декодування)
├─ Arrays of NAL units: VPS + SPS + PPS (у порядку пріоритету)
└─ Temporal layer info (для adaptive streaming)

Це дозволяє одному hvcc описувати складні конфігурації:
• Multi-layer coding (SVC)
• HDR metadata (10/12-bit)
• Multi-profile streams (адаптивний бітрейт)
```

---

## 🎵 6. Аудіо-муксери: AAC, G.711, MP3

### 🔸 `AACMuxer` — конвертація ADTS → ASC + Raw AAC

```go
type AACMuxer struct {
    updateSequence bool  // прапорець: чи потрібно надіслати ASC
}

func (muxer *AACMuxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    var tags [][]byte
    
    // Розділити вхідний потік на окремі ADTS-кадри
    codec.SplitAACFrame(frames, func(aac []byte) {
        // 1. Розпарсити заголовок для отримання параметрів
        hdr := codec.NewAdtsFrameHeader()
        hdr.Decode(aac)
        
        // 2. При першому кадрі: згенерувати Sequence Header з ASC
        if muxer.updateSequence {
            // ConvertADTSToASC: витягнути AudioSpecificConfig з ADTS
            asc, _ := codec.ConvertADTSToASC(aac)
            
            // Sequence Header тег: ASC у payload, AACPacketType=0
            tags = append(tags, WriteAudioTag(asc.Encode(), FLV_AAC, 0, 0, true))
            muxer.updateSequence = false  // більше не генерувати
        }
        
        // 3. Raw AAC frame тег: тільки дані, без заголовку ADTS
        // aac[7:] — пропустити 7-байтовий ADTS header
        tags = append(tags, WriteAudioTag(aac[7:], FLV_AAC, 0, 0, false))
    })
    return tags
}
```

### 🔍 Чому `aac[7:]`?

```
ADTS header = 7 байт (без CRC) або 9 байт (з CRC):
[Syncword:12][ID:1][Layer:2][Protection:1][Profile:2][SampleRate:4]
[Private:1][ChannelConfig:3][Original:1][Home:1][Copyright:1][CopyrightStart:1]
[FrameLength:13][BufferFullness:11][Frames:2]

FLV вже зберігає ці параметри у заголовку тега + ASC,
тому повторювати їх у payload — зайве.
```

### 🔸 `G711AMuxer` / `G711UMuxer` — простий проксі

```go
func (muxer *G711AMuxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    // G.711 не має sequence header — параметри фіксовані
    // Один вхідний кадр = один FLV тег
    tags := make([][]byte, 1)
    tags[0] = WriteAudioTag(frames, FLV_G711A, muxer.sampleRate, muxer.channelCount, true)
    return tags
}
```

> 💡 **Особливість**: `isSequenceHeader=true` для G.711, хоча це не sequence header. Це "костиль" для сумісності: деякі плеєри очікують хоча б один тег з `AACPacketType=0` для ініціалізації декодера.

### 🔸 `Mp3Muxer` — аналогічно G.711

```go
func (muxer *Mp3Muxer) Write(frames []byte, pts uint32, dts uint32) [][]byte {
    tags := make([][]byte, 1)
    // Розпарсити заголовок для отримання sampleRate/channelCount
    codec.SplitMp3Frames(frames, func(head *codec.MP3FrameHead, frame []byte) {
        tags = append(tags, WriteAudioTag(frames, FLV_MP3, 
            head.GetSampleRate(), head.GetChannelCount(), true))
    })
    return tags
}
```

---

## 🏗️ 7. `FlvMuxer` — оркестратор

### 🔧 Ініціалізація:

```go
func NewFlvMuxer(vid FLV_VIDEO_CODEC_ID, aid FLV_SOUND_FORMAT) *FlvMuxer {
    return &FlvMuxer{
        videoMuxer: CreateVideoMuxer(vid),  // фабрика відео-муксера
        audioMuxer: CreateAudioMuxer(aid),  // фабрика аудіо-муксера
    }
}
```

### 🔧 Динамічна зміна кодеку:

```go
func (muxer *FlvMuxer) SetVideoCodeId(cid FLV_VIDEO_CODEC_ID) {
    muxer.videoMuxer = CreateVideoMuxer(cid)  // замінити муксер "на льоту"
}
```

> ⚠️ **Ризик**: Якщо замінити муксер під час обробки потоку, кеш параметрів (SPS/PPS) втратиться → наступний IDR може не мати параметрів. Краще ініціалізувати один раз на початку сесії.

### 🔧 Основний метод `WriteFrames`:

```go
func (muxer *FlvMuxer) WriteFrames(frameType TagType, frames []byte, pts uint32, dts uint32) ([][]byte, error) {
    var ftag FlvTag
    var tags [][]byte
    
    // 1. Делегувати специфічну логіку кодеку
    if frameType == AUDIO_TAG {
        ftag.TagType = uint8(AUDIO_TAG)
        tags = muxer.audioMuxer.Write(frames, pts, dts)
    } else if frameType == VIDEO_TAG {
        ftag.TagType = uint8(VIDEO_TAG)
        tags = muxer.videoMuxer.Write(frames, pts, dts)
    } else {
        return nil, errors.New("unsupport Frame Type")
    }
    
    // 2. Встановити загальні поля заголовку тега
    ftag.Timestamp = dts & 0x00FFFFFF           // молодші 24 біти
    ftag.TimestampExtended = uint8(dts >> 24)   // старші 8 біт
    
    // 3. Упакувати кожен payload у повний FLV тег
    tmptags := make([][]byte, 0, 1)
    for _, tag := range tags {
        ftag.DataSize = uint32(len(tag))  // розмір payload
        vtag := ftag.Encode()             // 11-байтовий заголовок тега
        vtag = append(vtag, tag...)       // + payload
        tmptags = append(tmptags, vtag)
    }
    return tmptags, nil
}
```

### 🎯 Чому повертається `[][]byte`, а не `[]byte`?

```
Один вхідний кадр → кілька вихідних тегів:

Приклад для H.264 IDR без попередніх параметрів:
Вхід: [SPS][PPS][IDR] (Annex-B)

Вихід:
[
  [FLV tag header:11][VideoTag header:5][AVCC extradata:SPS+PPS],  // Sequence Header
  [FLV tag header:11][VideoTag header:5][AVCC NAL units:IDR]       // NALU frame
]

Клієнт (FlvWriter) записує кожен тег окремо з PreviousTagSize між ними.
```

---

## 🐞 8. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Ігнорування помилок у критичних функціях**:
   ```go
   extraData, _ := codec.CreateH264AVCCExtradata(spss, ppss)  // ← помилка ігнорується!
   // Якщо extradata не створиться → Sequence Header тег буде порожнім → декодер не ініціалізується
   
   // Краще:
   extraData, err := codec.CreateH264AVCCExtradata(spss, ppss)
   if err != nil {
       logger.Error("failed to create AVCC extradata", "error", err)
       return nil  // або повернути помилку наверх
   }
   ```

2. **Race condition у кешах параметрів**:
   ```go
   // AVCMuxer.spsset/ppss — map без mutex!
   // Якщо Write() викликається з кількох горутин → data race!
   
   // Рішення: додати sync.Mutex
   type AVCMuxer struct {
       mu sync.Mutex
       spsset map[uint64][]byte
       // ...
   }
   func (m *AVCMuxer) Write(...) {
       m.mu.Lock()
       defer m.mu.Unlock()
       // ... існуюча логіка
   }
   ```

3. **Некоректна обробка CTS для негативних значень**:
   ```go
   cts := int32(pts - dts)  // ← якщо pts < dts, переповнення!
   
   // FLV зберігає CTS як signed 24-bit integer
   // Безпечніше:
   cts := int32(int64(pts) - int64(dts))
   if cts < -(1<<23) || cts >= (1<<23) {
       logger.Warn("CTS out of range for FLV", "cts", cts)
       cts = 0  // fallback
   }
   ```

4. **`Mp3Muxer.Write` дублює вхідні дані**:
   ```go
   codec.SplitMp3Frames(frames, func(head *codec.MP3FrameHead, frame []byte) {
       // ⚠️ frames передається, а не frame!
       tags = append(tags, WriteAudioTag(frames, FLV_MP3, ...))
   })
   // Якщо у вхідному буфері кілька MP3 кадрів — кожен тег міститиме ВСІ кадри!
   
   // Має бути:
   tags = append(tags, WriteAudioTag(frame, FLV_MP3, ...))
   ```

5. **Відсутня валідація вхідних даних**:
   ```go
   // Якщо frames порожній або містить невалідні NAL units — паніка у парсері
   // Краще додати перевірку на початку Write():
   if len(frames) == 0 {
       return nil  // або logger.Warn + return
   }
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для безпечної конвертації CTS
func encodeFlvCTS(pts, dts uint32) (int32, error) {
    diff := int64(pts) - int64(dts)
    if diff < -(1<<23) || diff >= (1<<23) {
        return 0, fmt.Errorf("CTS out of FLV range: %d", diff)
    }
    return int32(diff), nil
}

// 2. Метрики для моніторингу мультиплексингу
func (muxer *AVCMuxer) recordMetrics(vclCount int, hasIDR bool, spsCount int) {
    metrics.FLVVideoFramesMuxed.Inc()
    if hasIDR { metrics.FLVKeyFramesMuxed.Inc() }
    metrics.FLVNalusPerFrame.Observe(float64(vclCount))
    metrics.FLVSPSCacheSize.Observe(float64(spsCount))
}

// 3. Кеш з TTL для параметрів (захист від пам'яті при довгих сесіях)
type ParamCache struct {
    items map[uint64]cachedParam
    mu    sync.RWMutex
    ttl   time.Duration
}

func (c *ParamCache) GetOrSet(id uint64, data []byte) []byte {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Перевірити наявність + валідність за TTL
    if item, ok := c.items[id]; ok && time.Now().Before(item.exp) {
        return item.data
    }
    
    // Зберегти нове значення
    c.items[id] = cachedParam{
        data: append([]byte(nil), data...),  // clone
        exp:  time.Now().Add(c.ttl),
    }
    return data
}
```

---

## 🎯 9. Інтеграція з вашим CCTV HLS Processor

### 📍 У `FlvWriter` — використання муксерів:

```go
func (f *FlvWriter) WriteH264(data []byte, pts uint32, dts uint32) error {
    // 1. Делегувати пакування у FlvMuxer
    tags, err := f.muxer.WriteVideo(data, pts, dts)
    if err != nil { return err }
    
    // 2. Записати кожен згенерований тег + PreviousTagSize
    for _, tag := range tags {
        f.writer.Write(tag)  // сам тег (заголовок + payload)
        f.writePreviousTagSize(uint32(len(tag)))  // 4 байти back-pointer
    }
    return nil
}
```

### 📍 У `RTMPRelay` — транскодування на льоту:

```go
type RTMPRelay struct {
    reader *flv.FlvReader
    writer *flv.FlvWriter
}

func (r *RTMPRelay) Start(input, output net.Conn) {
    // 1. Ініціалізувати reader/writer
    r.reader = flv.CreateFlvReader()
    r.writer = flv.CreateFlvWriter(output)
    r.writer.WriteFlvHeader()
    
    // 2. Налаштувати callback: reader → muxer → writer
    r.reader.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
        switch {
        case cid == codec.CODECID_VIDEO_H264:
            r.writer.WriteH264(frame, pts, dts)
        case cid == codec.CODECID_AUDIO_AAC:
            r.writer.WriteAAC(frame, pts, dts)
        }
    }
    
    // 3. Читати з входу → парсити → записувати у вихід
    buf := make([]byte, 64*1024)
    for {
        n, err := input.Read(buf)
        if err != nil { break }
        r.reader.Input(buf[:n])  // інкрементальний парсинг
    }
}
```

### 📍 У `HLSArchiver` — запис сегментів у FLV для архіву:

```go
type HLSArchiver struct {
    muxer *flv.FlvMuxer
    file  *os.File
}

func (a *HLSArchiver) Start(segmentPath string, videoCID, audioCID interface{}) error {
    file, _ := os.Create(segmentPath)
    a.file = file
    
    // 1. Створити муксер з потрібними кодеками
    a.muxer = flv.NewFlvMuxer(
        flv.CovertCodecId2FlvVideoCodecId(videoCID),
        flv.CovertCodecId2SoundFormat(audioCID),  // ← виправити опечатку!
    )
    
    // 2. Записати FLV заголовок
    writer := flv.CreateFlvWriter(file)
    writer.WriteFlvHeader()
    
    return nil
}

func (a *HLSArchiver) WriteFrame(cid codec.CodecID, frame []byte, pts, dts uint32) error {
    var tags [][]byte
    var err error
    
    // 3. Використати відповідний муксер
    if cid.IsVideo() {
        tags, err = a.muxer.WriteVideo(frame, pts, dts)
    } else {
        tags, err = a.muxer.WriteAudio(frame, pts, dts)
    }
    if err != nil { return err }
    
    // 4. Записати теги у файл
    for _, tag := range tags {
        a.file.Write(tag)
        // PreviousTagSize записується окремо у реальному коді
    }
    return nil
}
```

---

## 🧭 Висновок: чому цей модуль — міст між форматами

| Компонент | Роль у CCTV HLS Processor |
|-----------|---------------------------|
| **AVCMuxer/HevcMuxer** | Конвертація Annex-B → AVCC/hvcc + авто-генерація Sequence Header → валідні FLV теги |
| **AACMuxer** | Конвертація ADTS → ASC + Raw AAC → сумісність з браузерними плеєрами |
| **FlvMuxer** | Оркестрація відео/аудіо потоків + пакування у 11-байтові теги → готовий до запису FLV |
| **Кешування параметрів** | Уникнення дублікатів SPS/PPS + гарантія, що IDR завжди має параметри → стабільне відтворення |
| **CTS handling** | Коректна обробка B-frames → точна синхронізація аудіо/відео у вихідному файлі |

> 🔑 **Головна ідея**: Цей код — **форматний адаптер**, який перетворює "сирі" бітстріми на структуровані FLV теги, готові для запису, ретрансляції або подальшої конвертації у HLS. Без нього кожен компонент пайплайну змушений був би знати деталі формату FLV, AVCC, hvcc, ADTS — це порушило б принцип єдиної відповідальності.

💡 **Фінальна порада**: 
1. Виправте опечатку: `CovertCodecId2SoundFromat` → `CovertCodecId2SoundFormat` у всьому проекті
2. Додайте обробку помилок замість `_` у критичних місцях (`CreateH264AVCCExtradata`, `hvcc.Encode`)
3. Додайте `sync.Mutex` до муксерів для потокобезпеки
4. Виправте баг у `Mp3Muxer.Write`: використовувати `frame`, а не `frames` у циклі
5. Додайте інтеграційний тест, який перевіряє roundtrip: Annex-B → FLV → Annex-B через муксер/дему

Це перетворить цей модуль з "робочого коду" на "гарантовано надійний фундамент" для вашого FLV-пайплайну.