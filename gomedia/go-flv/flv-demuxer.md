# 🎬 Глибокий розбір `flv/demuxer.go` — FLV-демультиплексор для CCTV HLS Processor

Це **архітектурний шар адаптації** між форматом FLV (Flash Video) та вашим HLS-пайплайном. Він приймає сирих відео/аудіо теги з RTMP/FLV-джерел, розпаковує їх у стандартні бітстріми (Annex-B для відео, ADTS для AAC) та передає у `segmentAssembler`. Розберемо детально:

---

## 🧱 1. Архітектура: інтерфейси та фабрики

### 🔑 Ключові абстракції:

```go
// Callback-тип для відправки розпакованих кадрів
type OnVideoFrameCallBack func(codecid codec.CodecID, frame []byte, cts int)
// cts = Composition Time Offset (для B-frames у H.264/HEVC)

type VideoTagDemuxer interface {
    Decode(data []byte) error           // Розпакувати сирий FLV-тег
    OnFrame(onframe OnVideoFrameCallBack) // Зареєструвати обробник кадрів
}

type AudioTagDemuxer interface {
    Decode(data []byte) error
    OnFrame(onframe OnAudioFrameCallBack)
}
```

### 🎯 Чому інтерфейси?

| Перевага | Практичне значення для вашого проекту |
|----------|---------------------------------------|
| **Поліморфізм** | Один `segmentAssembler` працює з H.264, H.265, AAC, G.711 через єдиний інтерфейс |
| **Тестування** | Легко мокати демуксери у юніт-тестах без реальних бітстрімів |
| **Розширюваність** | Додати VP8/Opus підтримку — реалізувати інтерфейс, не чіпаючи ядро |

### 🔧 Фабричні функції:

```go
func CreateFlvVideoTagHandle(cid FLV_VIDEO_CODEC_ID) VideoTagDemuxer {
    switch cid {
    case FLV_AVC:  return NewAVCTagDemuxer()   // H.264
    case FLV_HEVC: return NewHevcTagDemuxer()  // H.265
    default:       panic("unsupport video codec")
    }
}

func CreateAudioTagDemuxer(formats FLV_SOUND_FORMAT) AudioTagDemuxer {
    switch formats {
    case FLV_AAC:        return NewAACTagDemuxer()      // AAC
    case FLV_G711A, FLV_G711U, FLV_MP3:
                         return NewG711Demuxer(formats) // G.711/MP3
    default: panic("unsupport audio codec")
    }
}
```

> 💡 **Практичне застосування**: При підключенні RTMP-камери ви читаєте `CodecID` з метаданих потоку → викликаєте фабрику → отримуєте готовий демуксер для цього кодеку.

---

## 🎞️ 2. `AVCTagDemuxer` — розпаковка H.264 з FLV у Annex-B

### 🔍 Структура FLV відео-тегу для H.264:

```
[FLV Video Tag Header: 5 байт]
├─ FrameType (4 біти): 1=Key, 2=Inter
├─ CodecID (4 біти): 7=AVC (H.264)
├─ AVCPacketType (1 байт): 
   │  0=Sequence Header (SPS/PPS у AVCC форматі)
   │  1=NALU (кадр у AVCC форматі)
   │  2=End of Sequence
├─ CompositionTime (3 байти, signed): CTS для B-frames

[Payload: змінна довжина]
├─ Якщо AVCPacketType=0: AVCC extradata (SPS/PPS у ISO/IEC 14496-15 форматі)
└─ Якщо AVCPacketType=1: NAL units у AVCC форматі:
   [4-byte big-endian length][NALU without start code][next NALU]...
```

### 🔧 Логіка `Decode` для H.264:

#### 📦 Крок 1: Парсинг заголовку тега

```go
vtag := VideoTag{}
vtag.Decode(data[0:5])  // Розпарсити 5-байтовий заголовок
data = data[5:]         // Відсікти заголовок, залишити payload
```

#### 🗝️ Крок 2: Обробка Sequence Header (SPS/PPS)

```go
if vtag.AVCPacketType == AVC_SEQUENCE_HEADER {
    // 1. Конвертувати AVCC → масив SPS/PPS у Annex-B форматі
    tmpspss, tmpppss := codec.CovertExtradata(data)
    
    // 2. Зберегти у кеш за ID для подальшого використання
    for _, sps := range tmpspss {
        spsid := codec.GetSPSId(sps)  // Витягнути seq_parameter_set_id
        demuxer.spss[spsid] = clone(sps)  // clone для уникнення aliasing
    }
    for _, pps := range tmpppss {
        ppsid := codec.GetPPSId(pps)
        demuxer.ppss[ppsid] = clone(pps)
    }
    // Sequence header не передається далі — параметри вже в кеші
}
```

#### 🎬 Крок 3: Обробка NAL units (відео-кадри)

```go
else {
    // 1. Сканувати payload для детекції ключових кадрів та параметрів
    tmpdata := data
    for len(tmpdata) > 0 {
        naluSize := binary.BigEndian.Uint32(tmpdata)  // 4-byte length prefix
        codec.CovertAVCCToAnnexB(tmpdata)              // 0x00000001 замість length
        naluType := codec.H264NaluType(tmpdata)        // Витягнути тип NAL
        
        // Детекція важливих подій:
        if naluType == codec.H264_NAL_I_SLICE { idr = true }      // Ключовий кадр
        else if naluType == codec.H264_NAL_SPS { hassps = true }  // SPS у кадрі
        else if naluType == codec.H264_NAL_PPS { haspps = true }  // PPS у кадрі
        else if naluType < codec.H264_NAL_I_SLICE {
            // P/B-slice: парсити Slice Header для детекції "instantaneous decode refresh"
            sh := codec.SliceHeader{}
            sh.Decode(codec.NewBitStream(tmpdata[5:]))  // +5: пропустити заголовок NAL + length
            if sh.Slice_type == 2 || sh.Slice_type == 7 {  // I-slice за типом слайсу
                idr = true
            }
        }
        tmpdata = tmpdata[4+naluSize:]  // Перейти до наступного NAL
    }
    
    // 2. Логіка "вставки параметрів перед IDR":
    if idr && (!hassps || !haspps) {
        // Ключовий кадр без SPS/PPS → декодер не зможе його розпакувати!
        // Рішення: додати з кешу перед кадром
        var nalus []byte = make([]byte, 0, 2048)
        for _, sps := range demuxer.spss { nalus = append(nalus, sps...) }
        for _, pps := range demuxer.ppss { nalus = append(nalus, pps...) }
        nalus = append(nalus, data...)  // + оригінальний кадр
        demuxer.onframe(codec.CODECID_VIDEO_H264, nalus, int(vtag.CompositionTime))
    } else {
        // Нормальний випадок: параметри вже є або кадр не ключовий
        demuxer.onframe(codec.CODECID_VIDEO_H264, data, int(vtag.CompositionTime))
    }
}
```

### 🎯 Чому це критично для HLS:

| Сценарій | Без демуксера | З `AVCTagDemuxer` |
|----------|--------------|------------------|
| **IDR без SPS/PPS** | FFmpeg: `missing parameter sets` → сегмент відкидається | Автоматична вставка з кешу → валідний сегмент |
| **B-frames** | Неправильний PTS → розсинхронізація аудіо/відео | `CompositionTime` додається до DTS → коректний PTS |
| **Зміна параметрів** | Нові SPS ігноруються → артефакти декодування | Кеш оновлюється → плавний перехід між конфігураціями |

---

## 🌀 3. `HevcTagDemuxer` — підтримка H.265 з enhanced FLV

### 🔑 Відмінності H.265 у FLV:

```
Enhanced FLV (Adobe spec extension for HEVC):
├─ Extended header flag: bit 7 of first byte = 1
├─ Якщо встановлено:
   │  Заголовок тегу = 8 байт (замість 5)
   │  Додаткові поля: VideoFourCC, PacketType
└─ PacketType для HEVC:
   │  0 = Sequence Start (VPS/SPS/PPS у hvcc форматі)
   │  1 = Coded Frames (з CTS)
   │  2 = Coded Frames X (без CTS)
```

### 🔧 Логіка `Decode` для H.265:

```go
func (demuxer *HevcTagDemuxer) Decode(data []byte) error {
    isExHeader := data[0] & 0x80  // Перевірка enhanced flag
    
    if isExHeader != 0 {
        // Enhanced FLV: 8-байтовий заголовок
        vtag.Decode(data[0:8])
        
        switch vtag.AVCPacketType {
        case PacketTypeSequenceStart:  // hvcc extradata
            data = data[5:]  // ⚠️ Підозріло: має бути data[8:]?
            hvcc := codec.NewHEVCRecordConfiguration()
            hvcc.Decode(data)  // Парсинг hvcc
            demuxer.SpsPpsVps = hvcc.ToNalus()  // Конвертація у Annex-B
            
        case PacketTypeCodedFrames:  // Кадр з CTS
            data = data[8:]  // Пропустити повний заголовок
            demuxer.decodeNalus(data, vtag.CompositionTime)
            
        case PacketTypeCodedFramesX:  // Кадр без CTS
            data = data[5:]  // Тільки базовий заголовок
            demuxer.decodeNalus(data, vtag.CompositionTime)
        }
    } else {
        // Legacy FLV: 5-байтовий заголовок (як H.264)
        vtag.Decode(data[0:5])
        data = data[5:]
        
        if vtag.AVCPacketType == AVC_SEQUENCE_HEADER {
            hvcc := codec.NewHEVCRecordConfiguration()
            hvcc.Decode(data)
            demuxer.SpsPpsVps = hvcc.ToNalus()
        } else {
            demuxer.decodeNalus(data, vtag.CompositionTime)
        }
    }
    return nil
}
```

### 🔧 Helper `decodeNalus` — спільна логіка для H.264/H.265:

```go
func (demuxer *HevcTagDemuxer) decodeNalus(data []byte, CompositionTime int32) error {
    // 1. Сканування NAL units для детекції ключових кадрів та параметрів
    var hassps, haspps, hasvps, idr bool
    tmpdata := data
    for len(tmpdata) > 0 {
        naluSize := binary.BigEndian.Uint32(tmpdata)
        codec.CovertAVCCToAnnexB(tmpdata)  // Конвертація length → start code
        naluType := codec.H265NaluType(tmpdata)
        
        // Детекція IRAP frames (H.265 аналог IDR): types 16-21
        if naluType >= 16 && naluType <= 21 { idr = true }
        else if naluType == codec.H265_NAL_SPS { hassps = true }
        else if naluType == codec.H265_NAL_PPS { haspps = true }
        else if naluType == codec.H265_NAL_VPS { hasvps = true }
        
        tmpdata = tmpdata[4+naluSize:]
    }
    
    // 2. Логіка "вставки параметрів перед IRAP":
    if idr && (!hassps || !haspps || !hasvps) {
        var nalus []byte = make([]byte, 0, 2048)
        nalus = append(demuxer.SpsPpsVps, data...)  // VPS+SPS+PPS + кадр
        demuxer.onframe(codec.CODECID_VIDEO_H265, nalus, int(CompositionTime))
    } else {
        demuxer.onframe(codec.CODECID_VIDEO_H265, data, int(CompositionTime))
    }
    return nil
}
```

### ⚠️ Потенційна проблема:

```go
// У обробці enhanced FLV:
case PacketTypeSequenceStart:
    data = data[5:]  // ← Підозріло: enhanced header = 8 байт, а не 5!
    // Має бути: data = data[8:] для пропуску повного заголовку
    
// Це може призвести до зсуву при парсингу hvcc → невалідні параметри
```

---

## 🎵 4. `AACTagDemuxer` — конвертація AAC: ASC → ADTS

### 🔍 Проблема: FLV vs HLS формати AAC

```
FLV AAC Payload:
├─ AudioTag header (2 байти)
└─ "Raw" AAC frames без заголовків (тільки основні дані)

HLS/TS AAC Payload:
├─ ADTS header (7-9 байт) перед кожним кадром
│  ├─ Syncword: 0xFFF
│  ├─ Sampling frequency, channel config, frame length...
└─ AAC frame data

Рішення: конвертувати AudioSpecificConfig (ASC) → ADTS header "на льоту"
```

### 🔧 Логіка `Decode` для AAC:

```go
func (demuxer *AACTagDemuxer) Decode(data []byte) error {
    if len(data) < 2 { return errors.New("aac tag size < 2") }
    
    // 1. Парсинг аудіо-заголовку
    atag := AudioTag{}
    atag.Decode(data[0:2])  // CodecID, SampleRate, Channels, AACPacketType
    data = data[2:]         // Відсікти заголовок
    
    // 2. Sequence Header: зберегти ASC для подальшої конвертації
    if atag.AACPacketType == AAC_SEQUENCE_HEADER {
        demuxer.asc = clone(data)  // ASC = AudioSpecificConfig
        return nil  // Не передавати далі — це метадані
    }
    
    // 3. Raw AAC frame: конвертувати ASC → ADTS + додати frame
    else {
        // codec.ConvertASCToADTS:
        // 1. Розпарсити ASC (profile, sampleRateIndex, channelConfig)
        // 2. Заповнити ADTS header згідно специфікації
        // 3. Встановити frame length = 7 + len(data)
        adts, err := codec.ConvertASCToADTS(demuxer.asc, len(data)+7)
        if err != nil { return err }
        
        // 4. З'єднати ADTS header + raw AAC data
        adts_frame := append(adts.Encode(), data...)
        
        // 5. Відправити у пайплайн
        if demuxer.onframe != nil {
            demuxer.onframe(codec.CODECID_AUDIO_AAC, adts_frame)
        }
    }
    return nil
}
```

### 📐 Формула ADTS header (спрощено):

```
ADTS Header (7 байт, без CRC):
Біти 0-11:   Syncword = 0xFFF
Біти 12-13:  MPEG version = 0 (MPEG-4)
Біти 14-16:  Layer = 0 (reserved)
Біт   17:    Protection absent = 1 (no CRC)
Біти 18-19:  Profile = ASC.profile - 1
Біти 20-23:  Sample rate index = ASC.sampleRateIndex
Біт   24:    Private bit = 0
Біти 25-27:  Channel config = ASC.channelConfig
Біти 28-29:  Original/copy = 0, home = 0
Біти 30-31:  Copyright ID bit = 0, Copyright ID start = 0
Біти 32-34:  AACL = 0, AACLF = 0
Біти 35-45:  Frame length = (7 + len(aacData)) у бітах
Біти 46-51:  ADTS buffer fullness = 0x7FF (VBR)
Біти 52-53:  Number of AAC frames = 0 (1 frame per ADTS)
```

> 💡 **Практичне значення**: Без ADTS-заголовків браузер/плеєр не зможе визначити розмір кадру → буфер переповнення або недоповнення → артефакти аудіо.

---

## 🔊 5. `G711Demuxer` — простий проксі для PCM-кодеків

### 🔍 Чому G.711/MP3 простіші за AAC:

```
G.711 (A-law/μ-law) та MP3 у FLV:
├─ Не мають "sequence header" — параметри фіксовані
├─ Кожен теґ = один готовий кадр для відтворення
├─ Не потрібна конвертація заголовків
```

### 🔧 Логіка `Decode`:

```go
func (demuxer *G711Demuxer) Decode(data []byte) error {
    if len(data) < 1 { return errors.New("audio tag size < 1") }
    
    // 1. Прочитати заголовок (1 байт для G.711/MP3)
    atag := AudioTag{}
    atag.Decode(data[0:1])
    data = data[1:]  // Відсікти заголовок
    
    // 2. Без обробки — відправити "як є"
    if demuxer.onframe != nil {
        // demuxer.format.ToMpegCodecId() мапить FLV_G711A → CODECID_AUDIO_G711A
        demuxer.onframe(demuxer.format.ToMpegCodecId(), data)
    }
    return nil
}
```

### 🎯 Коли використовувати G.711 у CCTV:

| Перевага | Сценарій використання |
|----------|---------------------|
| **Низька затримка** | Голосові повідомлення, двосторонній зв'язок |
| **Простота декодування** | Старі плеєри, embedded-пристрої без AAC підтримки |
| **Детермінований бітрейт** | Розрахунок bandwidth для мережевого планування |

---

## 🐞 6. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Некоректний зсув у enhanced FLV для HEVC**:
   ```go
   // У HevcTagDemuxer.Decode:
   case PacketTypeSequenceStart:
       data = data[5:]  // ← enhanced header = 8 байт, а не 5!
       // Має бути:
       data = data[8:]  // пропустити повний заголовок
   ```

2. **Відсутня валідація ASC перед конвертацією**:
   ```go
   // У AACTagDemuxer:
   adts, err := codec.ConvertASCToADTS(demuxer.asc, len(data)+7)
   // Якщо asc порожній або невалідний → паніка або некоректний ADTS!
   
   // Краще додати:
   if len(demuxer.asc) < 2 {
       return errors.New("ASC not initialized")
   }
   ```

3. **Race condition у кешах SPS/PPS**:
   ```go
   // AVCTagDemuxer.spss/ppss — map без mutex!
   // Якщо Decode() викликається з кількох горутин → data race!
   
   // Рішення: додати sync.RWMutex
   type AVCTagDemuxer struct {
       mu      sync.RWMutex
       spss    map[uint64][]byte
       ppss    map[uint64][]byte
       // ...
   }
   
   func (d *AVCTagDemuxer) Decode(data []byte) error {
       d.mu.Lock()
       defer d.mu.Unlock()
       // ... обробка
   }
   ```

4. **Необроблений `CompositionTime` для негативних значень**:
   ```go
   // CompositionTime у FLV — signed 24-bit integer
   // При конвертації у int: int(vtag.CompositionTime) може дати неправильний знак!
   
   // Безпечніше:
   cts := int32(vtag.CompositionTime)
   if cts >= 1<<23 { cts -= 1<<24 }  // sign-extend для 24-бітного числа
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для безпечної конвертації 24-бітного signed CTS
func decodeCompositionTime(b []byte) int32 {
    if len(b) < 3 { return 0 }
    val := int32(b[0])<<16 | int32(b[1])<<8 | int32(b[2])
    // Sign-extend: якщо біт 23 = 1, число негативне
    if val >= 1<<23 {
        val -= 1 << 24
    }
    return val
}

// 2. Метрики для моніторингу демуксингу
func (demuxer *AVCTagDemuxer) recordMetrics(packetType uint8, naluCount int, hasIDR bool) {
    metrics.FLVPacketsReceived.WithLabelValues("H264", fmt.Sprintf("type_%d", packetType)).Inc()
    metrics.FLVNalusPerPacket.Observe(float64(naluCount))
    if hasIDR {
        metrics.FLVKeyFramesReceived.Inc()
    }
}

// 3. Кеш з TTL для параметрів (захист від пам'яті при довгих сесіях)
type ParamCache struct {
    items map[uint64]cachedParam
    mu    sync.RWMutex
    ttl   time.Duration
}

type cachedParam struct {
    data []byte
    exp  time.Time
}

func (c *ParamCache) Get(id uint64) ([]byte, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    param, ok := c.items[id]
    if !ok || time.Now().After(param.exp) {
        return nil, false
    }
    return param.data, true
}
```

---

## 🎯 7. Інтеграція з вашим CCTV HLS Processor

### 📍 У `RTMPReceiver` — ініціалізація демуксерів:

```go
func (r *RTMPReceiver) onMetaData(metadata flv.MetaData) error {
    // 1. Визначити кодеки з метаданих
    videoCodec := flv.FLV_VIDEO_CODEC_ID(metadata.VideoCodecID)
    audioCodec := flv.FLV_SOUND_FORMAT(metadata.AudioFormat)
    
    // 2. Створити демуксери через фабрику
    r.videoDemuxer = flv.CreateFlvVideoTagHandle(videoCodec)
    r.audioDemuxer = flv.CreateAudioTagDemuxer(audioCodec)
    
    // 3. Зареєструвати callback у segmentAssembler
    r.videoDemuxer.OnFrame(func(codecid codec.CodecID, frame []byte, cts int) {
        r.assembler.HandleVideoFrame(codecid, frame, cts)
    })
    r.audioDemuxer.OnFrame(func(codecid codec.CodecID, frame []byte) {
        r.assembler.HandleAudioFrame(codecid, frame)
    })
    
    return nil
}
```

### 📍 У `segmentAssembler` — обробка кадрів з CTS:

```go
func (sa *SegmentAssembler) HandleVideoFrame(codecid codec.CodecID, frame []byte, cts int) {
    // 1. Розрахунок PTS: DTS + CTS
    // DTS = поточний час сегменту, CTS = offset для B-frames
    pts := sa.currentVideoDTS + time.Duration(cts)*time.Millisecond
    
    // 2. Додати кадр у поточний сегмент
    sa.currentVideoSegment.AppendFrame(frame, pts)
    
    // 3. Оновити DTS для наступного кадру
    // (спрощено: припустимо фіксований frame duration)
    sa.currentVideoDTS += sa.frameDuration
}
```

### 📍 У `createTSSegment` — валідація вихідного потоку:

```go
func validateAnnexBStream(data []byte, codecID codec.CodecID) error {
    // Перевірити, що всі NAL units мають валідні start codes
    offset := 0
    for offset < len(data) {
        pos, typ := codec.FindStartCode(data, offset)
        if pos == -1 { break }
        
        nalu := data[pos+int(typ):]
        switch codecID {
        case codec.CODECID_VIDEO_H264:
            if codec.H264NaluTypeWithoutStartCode(nalu) > codec.H264_NAL_AUD {
                return fmt.Errorf("invalid H.264 NAL type at offset %d", pos)
            }
        case codec.CODECID_VIDEO_H265:
            if codec.H265NaluTypeWithoutStartCode(nalu) > 63 {
                return fmt.Errorf("invalid H.265 NAL type at offset %d", pos)
            }
        }
        offset = pos + int(typ) + 1  // +1 для пропуску першого байту заголовку
    }
    return nil
}
```

---

## 🧭 Висновок: чому цей демуксер — міст між світами

| Компонент | Роль у CCTV HLS Processor |
|-----------|---------------------------|
| **AVCTagDemuxer** | Конвертація FLV/H.264 → Annex-B + авто-вставка SPS/PPS перед IDR → валідні HLS-сегменти |
| **HevcTagDemuxer** | Підтримка enhanced FLV для H.265 + обробка VPS/SPS/PPS → майбутнє 4K/8K камер |
| **AACTagDemuxer** | Конвертація "raw" AAC → ADTS → сумісність з браузерами та плеєрами |
| **G711Demuxer** | Простий проксі для legacy-аудіо → підтримка старих камер без перевкодування |
| **Фабрики** | Єдина точка створення демуксерів → легке додавання нових кодеків (VP8, Opus) |

> 🔑 **Головна ідея**: Цей код — **адаптер**, який перетворює "брудний" RTMP/FLV потік на "чисті" бітстріми, готові для сегментації у HLS. Без нього ваш `segmentAssembler` змушений був би знати деталі формату FLV, AVCC, ADTS — це порушило б принцип єдиної відповідальності та ускладнило тестування.

💡 **Фінальна порада**: Додайте інтеграційний тест, який:
1. Генерує сирий FLV-тег з H.264 IDR-кадром без SPS/PPS
2. Пропускає його через `AVCTagDemuxer`
3. Перевіряє, що вихідний кадр містить SPS+PPS+IDR у правильному порядку
4. Валідує результат через `ffprobe -show_packets`

Це гарантує, що демуксер коректно обробляє найкритичніший сценарій — початок відтворення з ключового кадру.