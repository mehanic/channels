# 🧱 Глибокий розбір `flv/consts.go` — типи та константи формату FLV

Це **фундаментальний шар абстракції** для роботи з форматом FLV (Flash Video) у вашому CCTV HLS Processor. Файл визначає типізовані константи, enum-подібні структури та helper-функції для безпечної роботи з бітовими полями специфікації Adobe FLV. Розберемо архітектурно:

---

## 📊 1. Ієрархія типів: від специфікації до type-safe Go

### 🔑 Чому окремі типи замість `int`?

```go
type FLV_SOUND_FORMAT int      // ← не просто int, а семантичний тип
type FLV_VIDEO_CODEC_ID int    // ← компілятор не дозволить плутати з аудіо
type TagType int               // ← ізоляція типів тегів
```

| Перевага | Практичне значення для вашого проекту |
|----------|---------------------------------------|
| **Type safety** | `func HandleAudio(format FLV_SOUND_FORMAT)` не прийме випадково `FLV_AVC` |
| **Документація через код** | Сигнатура функції сама пояснює, які значення очікуються |
| **IDE підтримка** | Автодоповнення показує тільки валідні константи для типу |
| **Рефакторинг** | Зміна внутрішнього представлення (int → string) не ламає зовнішній API |

---

## 🎵 2. Аудіо: `FLVSAMPLEINDEX` та `FLV_SOUND_FORMAT`

### 🔸 Частоти дискретизації (специфікація FLV, Section E.4.2):

```go
type FLVSAMPLEINDEX int
const (
    FLV_SAMPLE_5500  FLVSAMPLEINDEX = iota  // 0 = 5.5 kHz
    FLV_SAMPLE_11000                        // 1 = 11.025 kHz
    FLV_SAMPLE_22000                        // 2 = 22.05 kHz
    FLV_SAMPLE_44000                        // 3 = 44.1 kHz (найпоширеніший)
)
```

#### 🔍 Бітове кодування у заголовку аудіо-тега:

```
Аудіо тег (перший байт payload):
[SoundFormat:4][SoundRate:2][SoundSize:1][SoundType:1]
              ↑↑
              | 2 біти → 4 можливі значення (0-3) → наш FLVSAMPLEINDEX

Приклад: 0xAF = 0b1010_1111
├─ SoundFormat = 0b1010 = 10 → AAC ✓
├─ SoundRate = 0b11 = 3 → 44.1 kHz ✓
├─ SoundSize = 1 → 16-bit samples
└─ SoundType = 1 → Stereo
```

### 🔸 Кодеки аудіо: `FLV_SOUND_FORMAT`

```go
type FLV_SOUND_FORMAT int
const (
    FLV_MP3   FLV_SOUND_FORMAT = 2   // MPEG-1/2 Layer 3
    FLV_G711A FLV_SOUND_FORMAT = 7   // G.711 A-law (telephony)
    FLV_G711U FLV_SOUND_FORMAT = 8   // G.711 μ-law (telephony, US)
    FLV_AAC   FLV_SOUND_FORMAT = 10  // AAC-LC (Advanced Audio Coding)
)
```

#### 🎯 Практичне застосування у CCTV:

| Кодек | Сценарій використання | Переваги для відеоспостереження |
|-------|---------------------|--------------------------------|
| **AAC** | Основний аудіопотік | Висока якість, низький бітрейт, підтримка стерео |
| **G.711A/U** | Голосові повідомлення, двосторонній зв'язок | Низька затримка (<10ms), простота декодування |
| **MP3** | Legacy-камери, сумісність | Універсальна підтримка, але більший бітрейт |

### 🔸 Метод `ToMpegCodecId`: міст між стандартами

```go
func (format FLV_SOUND_FORMAT) ToMpegCodecId() codec.CodecID {
    switch {
    case format == FLV_G711A:
        return codec.CODECID_AUDIO_G711A  // 99
    case format == FLV_G711U:
        return codec.CODECID_AUDIO_G711U  // 100
    case format == FLV_AAC:
        return codec.CODECID_AUDIO_AAC    // 98
    case format == FLV_MP3:
        return codec.CODECID_AUDIO_MP3    // 102
    default:
        panic("unsupport sound format")  // ⚠️ ризик у продакшені!
    }
}
```

#### 🎯 Навіщо ця конвертація?

```
Ваш пайплайн має три шари ідентифікації кодеків:

1. FLV специфікація: FLV_AAC = 10
2. Внутрішній codec пакет: CODECID_AUDIO_AAC = 98  
3. MPEG-TS/ISO специфікація: 0x0F для AAC

ToMpegCodecId() — це адаптер між шаром 1 → 2.
Без нього FlvReader змушений був би знати внутрішні значення codec.CodecID,
що порушило б модульність архітектури.
```

#### ⚠️ Потенційна проблема: `panic` замість помилки

```go
// У продакшені невідомий формат → crash всього сервера!
// Краще повертати помилку або спеціальне значення:

func (format FLV_SOUND_FORMAT) ToMpegCodecId() (codec.CodecID, error) {
    switch format {
    case FLV_G711A: return codec.CODECID_AUDIO_G711A, nil
    case FLV_G711U: return codec.CODECID_AUDIO_G711U, nil
    case FLV_AAC:   return codec.CODECID_AUDIO_AAC, nil
    case FLV_MP3:   return codec.CODECID_AUDIO_MP3, nil
    default:
        return codec.CODECID_UNRECOGNIZED, fmt.Errorf("unsupported FLV sound format: %d", format)
    }
}
```

---

## 🎞️ 3. Відео: `FLV_VIDEO_CODEC_ID` та `FLV_VIDEO_FRAME_TYPE`

### 🔸 Типи кадрів:

```go
type FLV_VIDEO_FRAME_TYPE int
const (
    KEY_FRAME   FLV_VIDEO_FRAME_TYPE = 1  // I-frame / IDR: точка входу для декодування
    INTER_FRAME FLV_VIDEO_FRAME_TYPE = 2  // P/B-frame: залежить від попередніх кадрів
)
```

#### 🎯 Критичність для HLS:

```
HLS вимагає, щоб кожен сегмент (.ts файл) починався з ключового кадру.
Без цього:
• Браузери не зможуть почати відтворення з середини сегменту
• FFmpeg видасть помилку "non-keyframe where keyframe expected"
• VLC покаже чорний екран до наступного ключового кадру

Ваш segmentAssembler використовує KEY_FRAME детекцію для:
1. Визначення меж сегментів
2. Генерації #EXT-X-DISCONTINUITY при зміні параметрів
3. Коректного розрахунку PTS для першого кадру сегменту
```

### 🔸 Кодеки відео:

```go
type FLV_VIDEO_CODEC_ID int
const (
    FLV_AVC  FLV_VIDEO_CODEC_ID = 7   // H.264 / AVC (найпоширеніший)
    FLV_HEVC FLV_VIDEO_CODEC_ID = 12  // H.265 / HEVC (ефективніший, але новіший)
)
```

#### 🔍 Бітове кодування у заголовку відео-тега:

```
Відео тег (перший байт payload):
[FrameType:4][CodecID:4]

Приклад: 0x17 = 0b0001_0111
├─ FrameType = 0b0001 = 1 → KEY_FRAME ✓
└─ CodecID = 0b0111 = 7 → AVC (H.264) ✓

Приклад: 0x1C = 0b0001_1100  
├─ FrameType = 1 → KEY_FRAME
└─ CodecID = 12 → HEVC (H.265) ✓
```

---

## 📦 4. Типи тегів: `TagType`

```go
type TagType int
const (
    AUDIO_TAG  TagType = 8   // Аудіо-дані
    VIDEO_TAG  TagType = 9   // Відео-дані  
    SCRIPT_TAG TagType = 18  // Метадані (onMetaData, onCuePoint)
)
```

### 🔍 Структура загального заголовку тега (11 байт):

```
Байт 0:   TagType (8=audio, 9=video, 18=script)
Байти 1-3: DataSize (UI24 BE) — розмір payload
Байти 4-6: Timestamp (UI24 BE) — молодші 24 біти часу
Байт   7:  TimestampExtended (UI8) — старші 8 біт
Байти 8-10: StreamID (UI24 BE, завжди 0)

[Payload: DataSize байт]
[PreviousTagSize: 4 байти = 11 + DataSize] ← back-pointer для seek
```

### 🎯 Практичне застосування у `FlvReader`:

```go
func (f *FlvReader) handleTagHeader(data []byte) error {
    tagType := TagType(data[0])
    
    switch tagType {
    case VIDEO_TAG:
        // 1. Витягнути CodecID з перших 4 біт payload
        cid := GetFLVVideoCodecId(data[11:])  // +11: пропустити заголовок тега
        
        // 2. Створити відповідний демуксер
        if f.videoDemuxer == nil {
            f.videoDemuxer = CreateFlvVideoTagHandle(cid)
        }
        
        // 3. Передати payload у демуксер
        return f.videoDemuxer.Decode(data[11:])
        
    case AUDIO_TAG:
        // Аналогічно для аудіо...
        
    case SCRIPT_TAG:
        // Пропустити метадані (або розпарсити для HLS-плейлиста)
        return f.skipScriptData(data)
        
    default:
        return fmt.Errorf("unknown tag type: %d", tagType)
    }
}
```

---

## 🔄 5. AVC/HEVC packet types: `AVC_SEQUENCE_HEADER` / `AVC_NALU`

```go
const (
    AVC_SEQUENCE_HEADER = 0  // Містить SPS/PPS у AVCC/hvcc форматі
    AVC_NALU            = 1  // Містить відео-кадр (NAL units у AVCC форматі)
)
```

### 🔍 Структура VideoTag payload для AVC/HEVC:

```
Байт 0: [FrameType:4][CodecID:4]  ← вже розглянули вище
Байт 1: AVCPacketType
        ├─ 0 = Sequence Header (тільки для першого тега потоку)
        ├─ 1 = NALU (відео-кадр)
        └─ 2 = End of Sequence (рідко використовується)
Байти 2-4: CompositionTime (signed 24-bit) ← тільки для AVCPacketType=1
           ├─ PTS - DTS для B-frames
           └─ 0 для потоків без B-frames

[Payload]:
├─ Якщо AVCPacketType=0: AVCC extradata (SPS+PPS для H.264, VPS+SPS+PPS для H.265)
└─ Якщо AVCPacketType=1: NAL units у AVCC форматі:
   [4-byte length][NALU without start code][next NALU]...
```

### 🎯 Чому Sequence Header окремий?

```
FLV розділяє параметри кодування та відео-дані для:

1. Ефективності: параметри передаються один раз, не дублюються у кожному кадрі
2. Гнучкості: можна змінити SPS/PPS "на льоту" без перезапуску потоку
3. Сумісності: плеєри ініціалізують декодер до отримання першого кадру

Ваш AVCMuxer реалізує це через:
• Кешування SPS/PPS у map[uint64][]byte
• Авто-генерація Sequence Header тега перед першим IDR-кадром
• Оновлення кешу при зміні параметрів потоку
```

---

## 🎵 6. AAC packet types: `AAC_SEQUENCE_HEADER` / `AAC_RAW`

```go
const (
    AAC_SEQUENCE_HEADER = 0  // Містить AudioSpecificConfig (ASC)
    AAC_RAW             = 1  // Містить "сирий" AAC frame без ADTS заголовку
)
```

### 🔍 Структура AudioTag payload для AAC:

```
Байт 0: [SoundFormat:4][SoundRate:2][SoundSize:1][SoundType:1]
Байт 1: AACPacketType (тільки для FLV_AAC)
        ├─ 0 = Sequence Header: payload = AudioSpecificConfig (2+ байти)
        └─ 1 = Raw AAC frame: payload = AAC frame data без ADTS header

[Payload]:
├─ Якщо AACPacketType=0: ASC (AudioSpecificConfig)
│  ├─ audioObjectType (5 біт): 2 = AAC-LC
│  ├─ samplingFrequencyIndex (4 біти): 3 = 44.1 kHz
│  ├─ channelConfiguration (4 біти): 2 = stereo
│  └─ ... інші параметри
│
└─ Якщо AACPacketType=1: AAC frame data (без 7-байтового ADTS header)
```

### 🎯 Практичне застосування у `AACMuxer`:

```go
func (muxer *AACMuxer) Write(frames []byte, pts, dts uint32) [][]byte {
    var tags [][]byte
    
    codec.SplitAACFrame(frames, func(aac []byte) {
        // 1. Розпарсити ADTS header для отримання параметрів
        hdr := codec.NewAdtsFrameHeader()
        hdr.Decode(aac)
        
        // 2. При першому кадрі: згенерувати Sequence Header з ASC
        if muxer.updateSequence {
            asc, _ := codec.ConvertADTSToASC(aac)  // ADTS → ASC конвертація
            // Sequence Header тег: AACPacketType=0
            tags = append(tags, WriteAudioTag(asc.Encode(), FLV_AAC, 0, 0, true))
            muxer.updateSequence = false
        }
        
        // 3. Raw AAC frame: пропустити 7-байтовий ADTS header
        // FLV вже зберігає параметри у заголовку тега + ASC
        tags = append(tags, WriteAudioTag(aac[7:], FLV_AAC, 0, 0, false))
    })
    return tags
}
```

---

## 🚀 7. Enhanced RTMP: нові packet types для сучасних кодеків

```go
// enhanced-rtmp Table 4 (Adobe extension for HEVC/AV1/VP9)
const (
    PacketTypeSequenceStart        = 0  // hvcc/av1C екстрадата
    PacketTypeCodedFrames          = 1  // Кадр з CTS (як у legacy)
    PacketTypeSequenceEnd          = 2  // Кінець послідовності (рідко)
    PacketTypeCodedFramesX         = 3  // Кадр без CTS (економія 3 байти)
    PacketTypeMetadata             = 4  // Метадані (кодування, HDR info)
    PacketTypeMPEG2TSSequenceStart = 5  // TS-потік у вкладеному форматі
)
```

### 🔍 Enhanced FLV header (8 байт замість 5):

```
Legacy FLV Video Tag header (5 байт):
[0]: [FrameType:4][CodecID:4]
[1]: AVCPacketType
[2-4]: CompositionTime (3 байти)

Enhanced FLV Video Tag header (8 байт):
[0]: 0x80 | [FrameType:4][CodecID:4]  ← біт 7 = 1 → enhanced mode
[1-4]: FourCC ('hvc1' для HEVC, 'av01' для AV1)
[5]: PacketType (0-5 з констант вище)
[6-8]: CompositionTime (тільки для PacketTypeCodedFrames)
```

### 🔧 Helper `GetFLVVideoCodecId`: детекція кодеку з legacy/enhanced заголовків

```go
func GetFLVVideoCodecId(data []byte) (cid FLV_VIDEO_CODEC_ID) {
    // Перевірка enhanced flag: біт 7 першого байту = 1
    isExHeader := data[0] & 0x80
    
    if isExHeader != 0 {
        // Enhanced mode: читати FourCC з байтів 1-4
        // 'h' 'v' 'c' '1' = 0x68 0x76 0x63 0x31 = "hvc1" → HEVC
        if data[1] == 'h' && data[2] == 'v' && data[3] == 'c' && data[4] == '1' {
            cid = FLV_HEVC
        }
        // TODO: додати підтримку AV1 ('a','v','0','1') та VP9
    } else {
        // Legacy mode: CodecID у молодших 4 бітах
        cid = FLV_VIDEO_CODEC_ID(data[0] & 0x0F)
    }
    return cid
}
```

### 🎯 Навіщо Enhanced RTMP?

| Перевага | Опис | Вигода для CCTV |
|----------|------|----------------|
| **Підтримка нових кодеків** | FourCC замість 4-бітного CodecID → не обмежені 16 значеннями | Можливість додавати AV1, VP9 без зміни специфікації |
| **Економія bandwidth** | PacketTypeCodedFramesX без CTS → -3 байти на кадр | Важливо для низькобітрейтних каналів (3G/4G камери) |
| **HDR metadata** | PacketTypeMetadata для передачі HDR info | Підтримка нічного бачення з широким динамічним діапазоном |
| **Backward compatibility** | Legacy плеєри ігнорують enhanced теги | Плавний перехід на нові кодеки без ламання старих клієнтів |

---

## 🐞 8. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **`panic` у `ToMpegCodecId`**:
   ```go
   // У продакшені невідомий формат → crash сервера!
   // Краще повертати помилку:
   func (format FLV_SOUND_FORMAT) ToMpegCodecId() (codec.CodecID, error) {
       // ... switch ...
       default:
           return codec.CODECID_UNRECOGNIZED, fmt.Errorf("unsupported: %d", format)
   }
   ```

2. **Відсутня валідація вхідних даних у `GetFLVVideoCodecId`**:
   ```go
   // Якщо len(data) < 5 для enhanced mode → panic: index out of range!
   // Краще додати перевірку:
   func GetFLVVideoCodecId(data []byte) (FLV_VIDEO_CODEC_ID, error) {
       if len(data) == 0 {
           return 0, errors.New("empty data")
       }
       if data[0]&0x80 != 0 && len(data) < 5 {
           return 0, errors.New("enhanced header too short")
       }
       // ... існуюча логіка ...
       return cid, nil
   }
   ```

3. **Неповна підтримка Enhanced RTMP**:
   ```go
   // TODO av1 і VP9 у коментарі, але не реалізовано!
   // Додати підтримку:
   if data[1]=='a' && data[2]=='v' && data[3]=='0' && data[4]=='1' {
       // Повернути новий тип: FLV_AV1 = 13 (наприклад)
   }
   ```

4. **Відсутність юніт-тестів для констант**:
   ```go
   // Ці константи критичні для коректності всього FLV-модуля!
   // Додати тести для:
   // • GetFLVVideoCodecId з legacy/enhanced заголовками
   // • ToMpegCodecId для всіх підтримуваних форматів
   // • Бітових масок при кодуванні/декодуванні заголовків
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечної конвертації з fallback
func SafeConvertSoundFormat(format FLV_SOUND_FORMAT, fallback codec.CodecID) codec.CodecID {
    cid, err := format.ToMpegCodecId()
    if err != nil {
        logger.Warn("unsupported sound format, using fallback", "format", format, "fallback", fallback)
        return fallback
    }
    return cid
}

// 2. Константи для "магічних чисел"
const (
    FLV_ENHANCED_FLAG = 0x80
    FLV_CODEC_ID_MASK = 0x0F
    FLV_FRAME_TYPE_MASK = 0xF0
    FLV_FRAME_TYPE_SHIFT = 4
)

// 3. Методи для бітових операцій
func (frameType FLV_VIDEO_FRAME_TYPE) Encode() uint8 {
    return uint8(frameType) << FLV_FRAME_TYPE_SHIFT
}

func DecodeFrameType(b uint8) FLV_VIDEO_FRAME_TYPE {
    return FLV_VIDEO_FRAME_TYPE((b & FLV_FRAME_TYPE_MASK) >> FLV_FRAME_TYPE_SHIFT)
}

// 4. Юніт-тести для констант
func TestGetFLVVideoCodecId(t *testing.T) {
    tests := []struct{
        name string
        data []byte
        want FLV_VIDEO_CODEC_ID
        wantErr bool
    }{
        {"legacy AVC key frame", []byte{0x17}, FLV_AVC, false},  // 0x17 = KEY|AVC
        {"legacy HEVC inter frame", []byte{0x2C}, FLV_HEVC, false}, // 0x2C = INTER|HEVC
        {"enhanced HEVC", []byte{0x80, 'h','v','c','1'}, FLV_HEVC, false},
        {"empty data", []byte{}, 0, true},
        {"enhanced too short", []byte{0x80, 'h'}, 0, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := GetFLVVideoCodecId(tt.data)
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("got = %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

## 🎯 9. Інтеграція з вашим CCTV HLS Processor

### 📍 У `FlvReader` — детекція кодеку при першому тегі:

```go
func (f *FlvReader) handleVideoTag(data []byte) error {
    // 1. Витягнути CodecID з заголовку (підтримка legacy + enhanced)
    cid, err := GetFLVVideoCodecId(data)
    if err != nil {
        return fmt.Errorf("detect video codec: %w", err)
    }
    
    // 2. Конвертувати у Internal ID для уніфікованої обробки
    internalCID := CovertFlvVideoCodecId2MpegCodecId(cid)
    if internalCID == codec.CODECID_UNRECOGNIZED {
        return fmt.Errorf("unsupported video codec: %d", cid)
    }
    
    // 3. Створити демуксер при першому тегі
    if f.videoDemuxer == nil {
        f.videoDemuxer = CreateFlvVideoTagHandle(cid)
    }
    
    // 4. Розрахувати зміщення payload (5 байт для AVC/HEVC)
    headerLen := GetTagLenByVideoCodec(cid)
    payload := data[headerLen:]
    
    // 5. Передати у демуксер
    return f.videoDemuxer.Decode(payload)
}
```

### 📍 У `FlvMuxer` — генерація заголовків:

```go
func (muxer *AVCMuxer) encodeVideoTagHeader(isKey bool, cts int32) []byte {
    header := make([]byte, 5)
    
    // Байт 0: [FrameType:4][CodecID:4]
    frameType := KEY_FRAME
    if !isKey { frameType = INTER_FRAME }
    header[0] = byte(frameType)<<4 | byte(FLV_AVC)
    
    // Байт 1: AVCPacketType
    if muxer.first {
        header[1] = AVC_SEQUENCE_HEADER  // 0
    } else {
        header[1] = AVC_NALU  // 1
    }
    
    // Байти 2-4: CompositionTime (signed 24-bit BE)
    if cts != 0 {
        // Sign-extend для 24-бітного числа
        if cts < 0 { cts |= ^((1<<24)-1) }
        PutUint24(header[2:5], uint32(cts&0xFFFFFF))
    }
    
    return header
}
```

### 📍 У метриках — моніторинг кодеків:

```go
func (f *FlvReader) recordCodecMetrics(tagType TagType, flvCID interface{}) {
    var internalCID codec.CodecID
    
    switch tagType {
    case VIDEO_TAG:
        if cid, ok := flvCID.(FLV_VIDEO_CODEC_ID); ok {
            internalCID = CovertFlvVideoCodecId2MpegCodecId(cid)
        }
    case AUDIO_TAG:
        if format, ok := flvCID.(FLV_SOUND_FORMAT); ok {
            internalCID, _ = format.ToMpegCodecId()  // ігноруємо помилку для метрик
        }
    }
    
    if internalCID != codec.CODECID_UNRECOGNIZED {
        metrics.FLVCodecUsage.WithLabelValues(
            map[TagType]string{AUDIO_TAG:"audio", VIDEO_TAG:"video"}[tagType],
            codec.CodecString(internalCID),
        ).Inc()
    }
}
```

---

## 🧭 Висновок: чому ці константи — фундамент коректності

| Компонент | Роль у FLV-модулі | Вартість помилки без нього |
|-----------|------------------|---------------------------|
| **Типізовані константи** | Запобігання плутанини між аудіо/відео/тегами | Неправильний парсинг → пошкоджені кадри → артефакти відтворення |
| **Бітові маски/зсуви** | Коректне кодування 4-бітних полів у байтах | Неправильний CodecID/FrameType → демуксер не створиться |
| **ToMpegCodecId()** | Прозора конвертація між стандартами | Внутрішній пайплайн не зрозуміє вхідний потік → кадри відкидаються |
| **Enhanced RTMP підтримка** | Майбутнє-доказність для AV1/VP9 | Неможливість підтримки нових камер без переписування ядра |

> 🔑 **Головна ідея**: Цей файл — **контракт між модулями**, який гарантує, що всі компоненти пайплайну "говорять однією мовою" про формати, кодеки та структури даних. Без нього кожен модуль змушений був би дублювати знання про специфікацію FLV, що призвело б до розсинхронізації, помилок та важкої підтримки.

💡 **Фінальна порада**: 
1. Виправте `panic` на повернення помилок у `ToMpegCodecId`
2. Додайте валідацію довжини буфера у `GetFLVVideoCodecId`
3. Реалізуйте підтримку AV1/VP9 у enhanced mode (FourCC 'av01'/'vp09')
4. Додайте юніт-тести для всіх helper-функцій з граничними випадками
5. Використовуйте константи (`FLV_ENHANCED_FLAG`) замість "магічних чисел" (`0x80`) у коді

Це перетворить цей файл з "набору констант" на "гарантовано коректний контракт" для всього FLV-модуля.