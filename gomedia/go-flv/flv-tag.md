# 🏷️ Глибокий розбір `flv/tag.go` — структури тегів FLV та їх кодування

Це **ядро бінарного формату FLV** у вашому CCTV HLS Processor. Файл реалізує специфікацію Adobe FLV (Section E.2-E.4) через type-safe структури з методами Encode/Decode для серіалізації/десеріалізації тегів. Розберемо архітектурно, побайтово:

---

## 📦 1. Загальна структура: `FlvTag` (11-байтовий заголовок)

### 🔍 Специфікація (Section E.2):

```
Байт 0:   [Reserved:2][Filter:1][TagType:5]
          ├─ Reserved: завжди 0 (для FMS)
          ├─ Filter: 0=немає фільтрації, 1=потрібна дешифровка
          └─ TagType: 8=audio, 9=video, 18=script

Байти 1-3: DataSize (UI24 BE) — розмір payload після цього заголовку
Байти 4-6: Timestamp (UI24 BE) — молодші 24 біти часу у мілісекундах
Байт   7:  TimestampExtended (UI8) — старші 8 біт → разом 32 біти
Байти 8-10: StreamID (UI24 BE) — завжди 0

[Payload: DataSize байт]
[PreviousTagSize: 4 байти = 11 + DataSize] ← back-pointer для seek
```

### 🔧 Структура Go:

```go
type FlvTag struct {
    TagType           uint8   // 8, 9, або 18
    DataSize          uint32  // розмір payload (макс. 16,777,215 байт)
    Timestamp         uint32  // молодші 24 біти часу
    TimestampExtended uint8   // старші 8 біт часу
    StreamID          uint32  // завжди 0
}
```

### 🔧 `Encode()`: серіалізація у байти

```go
func (ftag FlvTag) Encode() []byte {
    tag := make([]byte, 11)
    tag[0] = ftag.TagType                    // байт 0: TagType
    PutUint24(tag[1:], ftag.DataSize)        // байти 1-3: DataSize (UI24 BE)
    PutUint24(tag[4:], ftag.Timestamp)       // байти 4-6: Timestamp (молодші 24 біти)
    tag[7] = ftag.TimestampExtended          // байт 7: старші 8 біт
    PutUint24(tag[8:], ftag.StreamID)        // байти 8-10: StreamID (завжди 0)
    return tag
}
```

### 🔧 `Decode()`: десеріалізація з байтів

```go
func (ftag *FlvTag) Decode(data []byte) {
    ftag.TagType = data[0] & 0x1F            // маска 0x1F = 0b00011111 → виділяє 5 біт TagType
    ftag.DataSize = GetUint24(data[1:])      // байти 1-3 → 24-бітне число
    ftag.Timestamp = GetUint24(data[4:])     // байти 4-6 → молодші 24 біти часу
    ftag.TimestampExtended = data[7]         // байт 7 → старші 8 біт
    ftag.StreamID = GetUint24(data[8:])      // байти 8-10 → завжди 0
}
```

### 🎯 Чому `data[0] & 0x1F` для TagType?

```
Специфікація дозволяє майбутнє розширення через біти Reserved/Filter:
[Reserved:2][Filter:1][TagType:5] = 8 біт

Маска 0x1F = 0b00011111 виділяє тільки молодші 5 біт (TagType),
ігноруючи Reserved та Filter. Це забезпечує forward compatibility:
якщо майбутня версія використає біт Filter=1, ваш код все одно
коректно витягне TagType.

Приклад: 0x89 = 0b10001001
├─ Reserved:2 = 0b10 (не використовуємо)
├─ Filter:1 = 0b0 (немає фільтрації)
└─ TagType:5 = 0b01001 = 9 → VIDEO_TAG ✓

data[0] & 0x1F = 0x89 & 0x1F = 0x09 = 9 ✓
```

### 📐 Формула 32-бітного timestamp:

```
FLV зберігає час у двох частинах для економії місця:
• Timestamp (байти 4-6): молодші 24 біти (0..16,777,215 мс ≈ 4.66 годин)
• TimestampExtended (байт 7): старші 8 біт (0..255)

Розрахунок повного часу:
fullTimestamp = (TimestampExtended << 24) | Timestamp

Приклад:
Timestamp = 0x00F0B0 (15720 мс)
TimestampExtended = 0x01
fullTimestamp = (0x01 << 24) | 0x00F0B0 = 0x01000000 + 0xF0B0 = 16,843,952 мс ≈ 4.68 годин

Це дозволяє підтримувати потоки тривалістю до ~24 днів (2^32 мс) без переповнення.
```

> 💡 **Практичне значення**: У вашому `segmentAssembler` цей timestamp використовується для:
> 1. Розрахунку тривалості сегменту (#EXTINF у HLS)
> 2. Синхронізації аудіо/відео через PTS/DTS
> 3. Генерації #EXT-X-PROGRAM-DATE-TIME з абсолютним часом

---

## 🎞️ 2. Відео-тег: `VideoTag` — змінна структура для AVC/HEVC

### 🔍 Специфікація (Section E.4.3):

```
Байт 0: [FrameType:4][CodecID:4]
        ├─ FrameType: 1=key, 2=inter, 3=disposable, 4=generated, 5=info
        └─ CodecID: 2=H.263, 3=Screen, 4=VP6, 5=VP6-alpha, 6=ScreenV2, 7=AVC, 12=HEVC

Якщо CodecID == 7 (AVC) або 12 (HEVC):
  Байт 1: AVCPacketType
          ├─ 0 = Sequence Header (SPS/PPS/VPS у AVCC/hvcc форматі)
          ├─ 1 = NALU (відео-кадр у AVCC форматі)
          └─ 2 = End of Sequence
  Байти 2-4: CompositionTime (SI24 BE) — тільки для AVCPacketType=1
             ├─ signed 24-bit: PTS - DTS для B-frames
             └─ 0 для потоків без B-frames або для Sequence Header

[Payload]:
├─ Якщо AVCPacketType=0: AVCC/hvcc extradata
└─ Якщо AVCPacketType=1: NAL units у AVCC форматі:
   [4-byte length][NALU without start code][next NALU]...
```

### 🔧 Структура Go:

```go
type VideoTag struct {
    FrameType       uint8   // 1=key, 2=inter
    CodecId         uint8   // 7=AVC, 12=HEVC
    AVCPacketType   uint8   // 0=seq header, 1=NALU
    CompositionTime int32   // signed 24-bit CTS (PTS-DTS)
}
```

### 🔧 `Encode()`: серіалізація з урахуванням кодеку

```go
func (vtag VideoTag) Encode() (tag []byte) {
    // Визначити розмір заголовку за кодеком
    if vtag.CodecId == uint8(FLV_AVC) || vtag.CodecId == uint8(FLV_HEVC) {
        tag = make([]byte, 5)              // 1 байт базовий + 1 AVCPacketType + 3 CompositionTime
        tag[1] = vtag.AVCPacketType        // байт 1: тип пакету
        PutUint24(tag[2:], uint32(vtag.CompositionTime))  // байти 2-4: CTS (як unsigned для запису)
    } else {
        tag = make([]byte, 1)              // тільки базовий байт для старих кодеків
    }
    
    // Байт 0: об'єднати FrameType та CodecID
    tag[0] = (vtag.FrameType << 4) | (vtag.CodecId & 0x0F)
    // Приклад: FrameType=1 (key), CodecId=7 (AVC) → 0b0001_0111 = 0x17 ✓
    return
}
```

### 🔧 `Decode()`: десеріалізація з підтримкою Enhanced FLV

```go
func (vtag *VideoTag) Decode(data []byte) {
    // Перевірка Enhanced FLV flag: біт 7 першого байту = 1
    isExHeader := data[0] & 0x80
    
    if isExHeader != 0 {
        // === Enhanced FLV mode (Adobe extension для HEVC/AV1/VP9) ===
        
        // FrameType: біти 4-6 (3 біти замість 4 у legacy)
        vtag.FrameType = (data[0] >> 4) & 0x07
        
        // CodecId: молодші 4 біти → але в enhanced це AVCPacketType!
        vtag.AVCPacketType = data[0] & 0x0F
        
        // Детекція кодеку через FourCC (байти 1-4)
        // TODO av1 і VP9 — поки тільки HEVC
        if data[1] == 'h' && data[2] == 'v' && data[3] == 'c' && data[4] == '1' {
            vtag.CodecId = uint8(FLV_HEVC)  // "hvc1" → HEVC
            
            // CompositionTime тільки для PacketTypeCodedFrames (=1)
            if vtag.AVCPacketType == PacketTypeCodedFrames {
                vtag.CompositionTime = int32(GetUint24(data[5:]))  // байти 5-7
            }
        }
        
    } else {
        // === Legacy FLV mode (стандартна специфікація) ===
        
        // Розділити байт 0 на FrameType та CodecID
        vtag.FrameType = data[0] >> 4              // старші 4 біти
        vtag.CodecId = data[0] & 0x0F              // молодші 4 біти
        
        // Для AVC/HEVC: додаткові поля
        if vtag.CodecId == uint8(FLV_AVC) || vtag.CodecId == uint8(FLV_HEVC) {
            vtag.AVCPacketType = data[1]                    // байт 1
            vtag.CompositionTime = int32(GetUint24(data[2:])) // байти 2-4 як signed
        }
    }
}
```

### ⚠️ Критична проблема: обробка signed CompositionTime

```go
// У Decode():
vtag.CompositionTime = int32(GetUint24(data[2:]))

// GetUint24 повертає uint32, але CompositionTime — signed 24-bit integer!
// Якщо CTS негативний (PTS < DTS для B-frames), потрібен sign-extend:

// Неправильно:
cts := int32(GetUint24(data[2:]))  // 0x00FFFF → 65535, а має бути -1

// Правильно:
raw := GetUint24(data[2:])
if raw >= 1<<23 {  // якщо біт 23 = 1 → негативне число
    cts := int32(raw | ^((1<<24)-1))  // sign-extend до 32 біт
} else {
    cts := int32(raw)
}
```

### 🎯 Практичне застосування: B-frames та CTS

```
У H.264/HEVC з B-frames порядок декодування (DTS) ≠ порядок відтворення (PTS):

Кадр:   I0  B1  B2  P3  B4  B5  P6
DTS:    0   1   2   3   4   5   6   (порядок у потоці/файлі)
PTS:    0   3   4   6   7   8   9   (порядок відтворення)
CTS:    0   2   2   3   3   3   3   (PTS - DTS)

FLV зберігає:
• DTS у загальному заголовку тега (Timestamp + TimestampExtended)
• CTS у VideoTag (CompositionTime)

Розрахунок у вашому пайплайні:
pts := dts + uint32(cts)  // відновлення часу відтворення

Без коректної обробки signed CTS:
• Негативні значення перетворюються на великі позитивні
• PTS стає неправильним → розсинхронізація аудіо/відео
• Клієнти показують "стрибаюче" відео або розриви
```

---

## 🎵 3. Аудіо-тег: `AudioTag` — бітове пакування параметрів

### 🔍 Специфікація (Section E.4.2):

```
Байт 0: [SoundFormat:4][SoundRate:2][SoundSize:1][SoundType:1]
        ├─ SoundFormat: 2=MP3, 7=G711A, 8=G711U, 10=AAC
        ├─ SoundRate: 0=5.5kHz, 1=11kHz, 2=22kHz, 3=44kHz
        ├─ SoundSize: 0=8-bit, 1=16-bit (тільки для нестиснених форматів)
        └─ SoundType: 0=mono, 1=stereo

Якщо SoundFormat == 10 (AAC):
  Байт 1: AACPacketType
          ├─ 0 = Sequence Header (AudioSpecificConfig)
          └─ 1 = Raw AAC frame (без ADTS header)

[Payload]:
├─ Якщо AACPacketType=0: ASC (2+ байти)
└─ Якщо AACPacketType=1: AAC frame data (без 7-байтового ADTS header)
```

### 🔧 Структура Go:

```go
type AudioTag struct {
    SoundFormat   uint8  // 2, 7, 8, 10
    SoundRate     uint8  // 0-3 → 5.5/11/22/44 kHz
    SoundSize     uint8  // 0=8-bit, 1=16-bit
    SoundType     uint8  // 0=mono, 1=stereo
    AACPacketType uint8  // 0=seq header, 1=raw (тільки для AAC)
}
```

### 🔧 `Encode()`: бітове пакування у один байт

```go
func (atag AudioTag) Encode() (tag []byte) {
    // AAC потребує додаткового байту для AACPacketType
    if atag.SoundFormat == 10 {
        tag = make([]byte, 2)
        tag[1] = atag.AACPacketType  // байт 1: 0 або 1
    } else {
        tag = make([]byte, 1)  // інші кодеки: тільки один байт
    }
    
    // Байт 0: пакування 4 полів у 8 біт
    // [SoundFormat:4][SoundRate:2][SoundSize:1][SoundType:1]
    tag[0] = atag.SoundFormat<<4 | atag.SoundRate<<2 | atag.SoundSize<<1 | atag.SoundType
    
    // Приклад: AAC, 44kHz, 16-bit, stereo, raw frame
    // SoundFormat=10 (0b1010), SoundRate=3 (0b11), SoundSize=1, SoundType=1
    // tag[0] = 0b1010_1111 = 0xAF ✓
    return
}
```

### 🔧 `Decode()`: розпакування з валідацією

```go
func (atag *AudioTag) Decode(data []byte) error {
    if len(data) < 1 {
        return errors.New("audio tag header size < 1")
    }
    
    // Розпакувати 4 поля з одного байту
    atag.SoundFormat = data[0] >> 4              // старші 4 біти
    atag.SoundRate = (data[0] >> 2) & 0x03       // біти 2-3 (маска 0b11)
    atag.SoundSize = (data[0] >> 1) & 0x01       // біт 1 (маска 0b1)
    atag.SoundType = data[0] & 0x01              // молодший біт
    
    // AAC потребує другого байту
    if atag.SoundFormat == 10 {
        if len(data) < 2 {
            return errors.New("aac audio tag header size < 2")
        }
        atag.AACPacketType = data[1]
    }
    return nil
}
```

### 🎯 Чому `SoundSize` ігнорується для стиснених форматів?

```
Специфікація зазначає: "This parameter only pertains to uncompressed formats. 
Compressed formats always decode to 16 bits internally."

Це означає:
• Для AAC/MP3/G.711: SoundSize у заголовку може бути будь-яким, але декодер
  завжди виводить 16-бітні семпли.
• Ваш код коректно встановлює SoundSize=1 для AAC у WriteAudioTag(),
  але це фактично ігнорується декодером.

Практичний висновок: не покладайтеся на SoundSize для стиснених форматів —
завжди очікуйте 16-бітний вихід від декодера.
```

---

## 🚀 4. Enhanced FLV: підтримка сучасних кодеків

### 🔍 Чому потрібен Enhanced режим?

```
Legacy FLV обмежений 4 бітами для CodecID → максимум 16 кодеків.
Для підтримки AV1, VP9, майбутніх форматів — недостатньо.

Рішення (Adobe enhanced-rtmp spec):
• Біт 7 першого байту = 1 → enhanced mode
• Байти 1-4: FourCC код ('hvc1', 'av01', 'vp09')
• Розширена структура заголовку для нових можливостей
```

### 🔧 Детекція enhanced mode у `VideoTag.Decode()`:

```go
isExHeader := data[0] & 0x80  // перевірка біту 7

if isExHeader != 0 {
    // Enhanced mode: інша структура заголовку
    
    // FrameType: тепер 3 біти (біти 4-6), не 4
    vtag.FrameType = (data[0] >> 4) & 0x07  // маска 0b111
    
    // Молодші 4 біти → тепер AVCPacketType, не CodecID!
    vtag.AVCPacketType = data[0] & 0x0F
    
    // CodecID визначається через FourCC (байти 1-4)
    if data[1]=='h' && data[2]=='v' && data[3]=='c' && data[4]=='1' {
        vtag.CodecId = uint8(FLV_HEVC)  // "hvc1" → HEVC
    }
    // TODO: додати 'av01' → AV1, 'vp09' → VP9
}
```

### 📊 Порівняння заголовків:

| Поле | Legacy FLV (5 байт) | Enhanced FLV (8 байт) |
|------|-------------------|---------------------|
| Байт 0 | [FrameType:4][CodecID:4] | 0x80\|[FrameType:3][PacketType:4] |
| Байти 1-4 | AVCPacketType + CTS | FourCC ('hvc1', 'av01', ...) |
| Байт 5 | — | PacketType (0-5) |
| Байти 6-8 | — | CompositionTime (тільки для PacketType=1) |
| Макс. кодеків | 16 | необмежено (через FourCC) |

### 🎯 Практичне застосування:

```go
// У FlvReader для підтримки обох режимів:
func (f *FlvReader) handleVideoTag(data []byte) error {
    var vtag VideoTag
    vtag.Decode(data)  // автоматична детекція legacy/enhanced
    
    switch vtag.CodecId {
    case uint8(FLV_AVC):
        // Створити AVCTagDemuxer для H.264
    case uint8(FLV_HEVC):
        // Створити HevcTagDemuxer для H.265
    default:
        // Перевірити, чи це новий кодек через FourCC
        if data[0]&0x80 != 0 {
            fourcc := string(data[1:5])
            switch fourcc {
            case "av01": // AV1 підтримка
            case "vp09": // VP9 підтримка
            }
        }
        return fmt.Errorf("unsupported codec: %d", vtag.CodecId)
    }
    // ... подальша обробка
}
```

---

## 🐞 5. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Некоректна обробка signed CompositionTime**:
   ```go
   // У VideoTag.Decode():
   vtag.CompositionTime = int32(GetUint24(data[2:]))  // ← без sign-extend!
   
   // Якщо CTS = -1 (0x00FFFF у 24-бітному signed):
   // GetUint24 повертає 65535, int32(65535) = 65535, а не -1!
   
   // Виправлення:
   func decodeSigned24(data []byte) int32 {
       raw := GetUint24(data)
       if raw >= 1<<23 {  // біт 23 = 1 → негативне
           return int32(raw | ^((1<<24)-1))  // sign-extend
       }
       return int32(raw)
   }
   ```

2. **Відсутня валідація довжини буфера**:
   ```go
   // У VideoTag.Decode() для legacy mode:
   if vtag.CodecId == uint8(FLV_AVC) {
       vtag.AVCPacketType = data[1]  // ← panic якщо len(data) < 2!
       vtag.CompositionTime = int32(GetUint24(data[2:]))  // ← panic якщо len(data) < 5!
   }
   
   // Краще додати перевірку:
   if vtag.CodecId == uint8(FLV_AVC) || vtag.CodecId == uint8(FLV_HEVC) {
       if len(data) < 5 {
           return errors.New("video tag header too short for AVC/HEVC")
       }
       // ... існуюча логіка
   }
   ```

3. **Incomplete enhanced FLV support**:
   ```go
   // У коментарі: "TODO av1 і VP9", але не реалізовано!
   // Додати підтримку:
   if data[1]=='a' && data[2]=='v' && data[3]=='0' && data[4]=='1' {
       // Повернути новий тип: FLV_AV1 = 13 (наприклад)
   }
   ```

4. **`Encode()` не встановлює Filter/Reserved біти**:
   ```go
   // У FlvTag.Encode():
   tag[0] = ftag.TagType  // ← записує тільки TagType, інші біти = 0
   
   // Це правильно для більшості випадків, але якщо потрібно підтримувати
   // фільтрацію/шифрування — додати параметри для Filter/Reserved:
   func (ftag FlvTag) EncodeWithFlags(filter, reserved uint8) []byte {
       tag := make([]byte, 11)
       tag[0] = (reserved<<6) | (filter<<5) | (ftag.TagType&0x1F)
       // ...
   }
   ```

5. **Відсутність юніт-тестів для бітових операцій**:
   ```go
   // Ці методи критичні для коректності всього FLV-модуля!
   // Додати тести для:
   // • Encode/Decode roundtrip для всіх типів тегів
   // • Граничних значень (макс. DataSize, негативний CTS, enhanced flag)
   // • Бітових масок при кодуванні аудіо-заголовку
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечної обробки signed 24-bit
func DecodeSigned24(data []byte) (int32, error) {
    if len(data) < 3 {
        return 0, errors.New("buffer too short for signed 24-bit")
    }
    raw := GetUint24(data)
    if raw >= 1<<23 {
        return int32(raw | ^((1<<24)-1)), nil  // sign-extend
    }
    return int32(raw), nil
}

func EncodeSigned24(v int32) []byte {
    buf := make([]byte, 3)
    // Маска для 24 біт + обробка негативних значень
    PutUint24(buf, uint32(v&0xFFFFFF))
    return buf
}

// 2. Константи для бітових масок
const (
    FLV_TAG_TYPE_MASK = 0x1F
    FLV_FRAME_TYPE_MASK = 0xF0
    FLV_FRAME_TYPE_SHIFT = 4
    FLV_CODEC_ID_MASK = 0x0F
    FLV_ENHANCED_FLAG = 0x80
)

// 3. Методи для бітових операцій
func (ft FLV_VIDEO_FRAME_TYPE) Encode() uint8 {
    return uint8(ft) << FLV_FRAME_TYPE_SHIFT
}

func DecodeFrameType(b uint8) FLV_VIDEO_FRAME_TYPE {
    return FLV_VIDEO_FRAME_TYPE((b & FLV_FRAME_TYPE_MASK) >> FLV_FRAME_TYPE_SHIFT)
}

// 4. Юніт-тести для Encode/Decode roundtrip
func TestFlvTag_EncodeDecode(t *testing.T) {
    original := FlvTag{
        TagType: 9,  // VIDEO_TAG
        DataSize: 123456,
        Timestamp: 0x00F0B0,
        TimestampExtended: 0x01,
        StreamID: 0,
    }
    
    encoded := original.Encode()
    var decoded FlvTag
    decoded.Decode(encoded)
    
    if decoded.TagType != original.TagType {
        t.Errorf("TagType: got %d, want %d", decoded.TagType, original.TagType)
    }
    // ... перевірка всіх полів ...
}

func TestVideoTag_CompositionTime_Signed(t *testing.T) {
    // Тест негативного CTS (B-frame з PTS < DTS)
    tests := []struct{
        cts int32
        wantBytes []byte
    }{
        {0, []byte{0,0,0}},
        {1, []byte{0,0,1}},
        {-1, []byte{0xFF,0xFF,0xFF}},  // 24-бітне представлення -1
        {8388607, []byte{0x7F,0xFF,0xFF}},  // макс. позитивне
        {-8388608, []byte{0x80,0x00,0x00}}, // мін. негативне
    }
    for _, tt := range tests {
        got := EncodeSigned24(tt.cts)
        if !bytes.Equal(got, tt.wantBytes) {
            t.Errorf("EncodeSigned24(%d) = %x, want %x", tt.cts, got, tt.wantBytes)
        }
        decoded, _ := DecodeSigned24(got)
        if decoded != tt.cts {
            t.Errorf("DecodeSigned24(%x) = %d, want %d", got, decoded, tt.cts)
        }
    }
}
```

---

## 🎯 6. Інтеграція з вашим CCTV HLS Processor

### 📍 У `FlvReader` — парсинг вхідного потоку:

```go
func (f *FlvReader) handleTag(data []byte) error {
    // 1. Розпарсити загальний заголовок тега (11 байт)
    var flvTag FlvTag
    flvTag.Decode(data[:11])
    
    // 2. Витягнути payload
    payload := data[11 : 11+flvTag.DataSize]
    
    // 3. Обробка за типом тега
    switch TagType(flvTag.TagType) {
    case VIDEO_TAG:
        // Розпарсити VideoTag header
        var vtag VideoTag
        vtag.Decode(payload)
        
        // Розрахунок повного timestamp
        dts := uint32(flvTag.TimestampExtended)<<24 | flvTag.Timestamp
        pts := dts + uint32(vtag.CompositionTime)  // ← тут важлива коректна обробка signed CTS!
        
        // Витягнути відео-дані (пропустити VideoTag header)
        headerLen := 1  // базовий байт
        if vtag.CodecId == uint8(FLV_AVC) || vtag.CodecId == uint8(FLV_HEVC) {
            headerLen = 5  // + AVCPacketType + CTS
        }
        videoData := payload[headerLen:]
        
        // Передати у відео-демуксер
        return f.videoDemuxer.Decode(videoData)
        
    case AUDIO_TAG:
        // Аналогічно для аудіо...
    }
    return nil
}
```

### 📍 У `FlvMuxer` — генерація вихідного потоку:

```go
func (muxer *FlvMuxer) WriteVideo(frames []byte, pts, dts uint32) ([][]byte, error) {
    // 1. Розрахунок CTS
    cts := int32(pts - dts)  // ← може бути негативним!
    
    // 2. Делегувати пакування у AVCMuxer/HevcMuxer
    tags := muxer.videoMuxer.Write(frames, pts, dts)  // повертає [][]byte
    
    // 3. Упакувати кожен payload у повний FLV тег
    result := make([][]byte, 0, len(tags))
    for _, payload := range tags {
        var flvTag FlvTag
        flvTag.TagType = uint8(VIDEO_TAG)
        flvTag.DataSize = uint32(len(payload))
        flvTag.Timestamp = dts & 0x00FFFFFF           // молодші 24 біти
        flvTag.TimestampExtended = uint8(dts >> 24)   // старші 8 біт
        flvTag.StreamID = 0
        
        header := flvTag.Encode()  // 11 байт
        tag := append(header, payload...)  // заголовок + payload
        result = append(result, tag)
    }
    return result, nil
}
```

### 📍 У метриках — моніторинг тегів:

```go
func (f *FlvReader) recordTagMetrics(tagType TagType, dataSize uint32, timestamp uint32) {
    metrics.FLVTagsReceived.WithLabelValues(
        map[TagType]string{AUDIO_TAG:"audio", VIDEO_TAG:"video", SCRIPT_TAG:"script"}[tagType],
    ).Inc()
    
    metrics.FLVTagSize.Observe(float64(dataSize))
    metrics.FLVTimestamp.Observe(float64(timestamp) / 1000.0)  // конвертація у секунди
    
    // Детекція ключових кадрів для відео
    if tagType == VIDEO_TAG {
        // ... перевірка FrameType ...
        metrics.FLVKeyFramesReceived.Inc()
    }
}
```

---

## 🧭 Висновок: чому цей файл — фундамент бінарної сумісності

| Компонент | Роль у FLV-модулі | Вартість помилки без нього |
|-----------|------------------|---------------------------|
| **FlvTag.Encode/Decode** | Коректна серіалізація 11-байтового заголовку | Неправильний DataSize/Timestamp → пошкоджені теги, неможливість seek |
| **VideoTag з CTS** | Підтримка B-frames через signed CompositionTime | Розсинхронізація аудіо/відео, "стрибаюче" відео у клієнтів |
| **AudioTag бітове пакування** | Ефективне кодування 4 параметрів у 1 байт | Зайве споживання bandwidth, некоректні параметри декодування |
| **Enhanced FLV підтримка** | Майбутнє-доказність для AV1/VP9 | Неможливість підтримки нових камер без переписування ядра |
| **Бітові маски/зсуви** | Type-safe робота з бітовими полями специфікації | Плутанина між FrameType/CodecID → неправильний демуксер → падіння |

> 🔑 **Головна ідея**: Цей файл — **бінарний контракт** між вашим кодом та специфікацією FLV. Він гарантує, що:
> 1. Кожен байт заголовку кодується/декодується згідно специфікації
> 2. Підтримуються обидва режими: legacy та enhanced
> 3. Signed поля (CompositionTime) обробляються коректно
> 4. Бітові операції документовані та тестовані

Без нього кожен компонент пайплайну змушений був би реалізовувати власну логіку парсингу заголовків, що призвело б до дублювання коду, розсинхронізації та важкої підтримки.

💡 **Фінальна порада**: 
1. Виправте обробку signed CompositionTime через sign-extend
2. Додайте валідацію довжини буфера у Decode() методах
3. Реалізуйте підтримку AV1/VP9 у enhanced mode (FourCC 'av01'/'vp09')
4. Додайте юніт-тести для Encode/Decode roundtrip з граничними значеннями
5. Використовуйте константи (`FLV_TAG_TYPE_MASK`) замість "магічних чисел" (`0x1F`) у коді

Це перетворить цей файл з "робочої реалізації" на "гарантовано коректний бінарний шар" для всього вашого FLV-пайплайну.