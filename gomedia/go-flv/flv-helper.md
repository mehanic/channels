# 🛠️ Глибокий розбір `flv/utils.go` — утиліти для роботи з FLV форматом

Це **допоміжний шар** вашого FLV-модуля, який надає низькорівневі примітиви для кодування 24-бітних чисел, конвертації ідентифікаторів кодеків між різними стандартами та розрахунку розмірів заголовків. Ці функції використовуються повсюдно: від парсингу тегів до генерації вихідних файлів. Розберемо архітектурно:

---

## 🔢 1. 24-бітні цілі числа: `PutUint24` / `GetUint24`

### 🔍 Чому 24 біти? Формат FLV

У специфікації FLV багато критичних полів використовують **24-бітні unsigned integers** (UI24) замість стандартних 32 біт:

```
FLV Tag Header (11 байт):
├─ Байт 0:   TagType (8 біт)
├─ Байти 1-3: DataSize (UI24 BE) ← розмір payload, макс. 16,777,215 байт (~16MB)
├─ Байти 4-6: Timestamp (UI24 BE) ← молодші 24 біти часу, макс. ~4.6 годин
├─ Байт 7:   TimestampExtended (UI8) ← старші 8 біт → разом 32 біти
├─ Байти 8-10: StreamID (UI24 BE, завжди 0)
```

### 🔧 Реалізація `PutUint24` (запис):

```go
func PutUint24(b []byte, v uint32) {
    _ = b[2]  // ← "bounds check hint" для компілятора
    b[0] = byte(v >> 16)  // старший байт (біти 16-23)
    b[1] = byte(v >> 8)   // середній байт (біти 8-15)
    b[2] = byte(v)        // молодший байт (біти 0-7)
}
```

#### 📐 Приклад:

```
v = 0x00F0B0 (15720 у десятковій)

b[0] = 0x00F0B0 >> 16 = 0x00
b[1] = 0x00F0B0 >> 8  = 0xF0
b[2] = 0x00F0B0       = 0xB0

Результат: []byte{0x00, 0xF0, 0xB0} ✓
```

### 🔧 Реалізація `GetUint24` (читання):

```go
func GetUint24(b []byte) (v uint32) {
    _ = b[2]  // ← bounds check hint
    v = uint32(b[0])           // завантажити старший байт
    v = (v << 8) | uint32(b[1]) // зсунути + додати середній
    v = (v << 8) | uint32(b[2]) // зсунути + додати молодший
    return v
}
```

#### 📐 Приклад:

```
Вхід: []byte{0x00, 0xF0, 0xB0}

Крок 1: v = 0x00
Крок 2: v = (0x00 << 8) | 0xF0 = 0x00F0
Крок 3: v = (0x00F0 << 8) | 0xB0 = 0x00F0B0 = 15720 ✓
```

### 🎯 Чому `_ = b[2]` (bounds check hint)?

```go
// Без цього рядка компілятор Go має генерувати перевірку меж для кожного b[i]
// З цим рядком компілятор "бачить", що ми вже перевірили b[2], отже b[0] і b[1] теж валідні
// Результат: швидший код без зайвих перевірок у гарячому циклі парсингу

// Це мікро-оптимізація, але у потоковому парсингі тисяч тегів/сек вона дає відчутний приріст
```

### ⚠️ Потенційна проблема: відсутня валідація довжини буфера

```go
// Якщо викликати PutUint24([]byte{0}, 123) → panic: index out of range!
// Краще додати перевірку або документувати контракт:

// Варіант 1: повертати помилку
func PutUint24(b []byte, v uint32) error {
    if len(b) < 3 {
        return errors.New("buffer too small for uint24")
    }
    // ...
    return nil
}

// Варіант 2: документувати precondition (поточний підхід)
// "Caller must ensure len(b) >= 3"
```

### 🎯 Практичне застосування у вашому пайплайні:

```go
// У FlvTag.Decode для читання DataSize:
func (tag *FlvTag) Decode(data []byte) {
    tag.TagType = data[0]
    tag.DataSize = GetUint24(data[1:4])  // ← байти 1-3
    tag.Timestamp = GetUint24(data[4:7])  // ← байти 4-6 (молодші 24 біти)
    tag.TimestampExtended = data[7]       // ← старші 8 біт
    // ...
}

// У FlvTag.Encode для запису:
func (tag *FlvTag) Encode() []byte {
    buf := make([]byte, 11)
    buf[0] = tag.TagType
    PutUint24(buf[1:4], tag.DataSize)     // ← записати розмір
    PutUint24(buf[4:7], tag.Timestamp)     // ← записати timestamp
    buf[7] = tag.TimestampExtended
    // ...
    return buf
}
```

---

## 🔄 2. Конвертація ідентифікаторів кодеків: FLV ↔ Internal

### 🔍 Проблема: різні стандарти, різні ID

| Стандарт | Відео кодеки | Аудіо кодеки |
|----------|-------------|-------------|
| **FLV** (Adobe spec) | `FLV_AVC=7`, `FLV_HEVC=12` | `FLV_AAC=10`, `FLV_G711A=8`, `FLV_G711U=9`, `FLV_MP3=2` |
| **Internal** (ваш `codec.CodecID`) | `CODECID_VIDEO_H264=0`, `CODECID_VIDEO_H265=1` | `CODECID_AUDIO_AAC=98`, `CODECID_AUDIO_G711A=99`, ... |
| **MPEG-TS/ISO** | `0x1B` (H.264), `0x24` (H.265) | `0x0F` (AAC), `0x10` (G.711) |

**Рішення**: функції-адаптери для прозорої конвертації.

### 🔧 Відео: FLV → Internal

```go
func CovertFlvVideoCodecId2MpegCodecId(cid FLV_VIDEO_CODEC_ID) codec.CodecID {
    if cid == FLV_AVC {      // 7
        return codec.CODECID_VIDEO_H264  // 0
    } else if cid == FLV_HEVC {  // 12
        return codec.CODECID_VIDEO_H265  // 1
    }
    return codec.CODECID_UNRECOGNIZED  // 999
}
```

### 🔧 Аудіо: FLV → Internal

```go
func CovertFlvAudioCodecId2MpegCodecId(cid FLV_SOUND_FORMAT) codec.CodecID {
    switch cid {
    case FLV_AAC:    return codec.CODECID_AUDIO_AAC    // 10 → 98
    case FLV_G711A:  return codec.CODECID_AUDIO_G711A  // 8  → 99
    case FLV_G711U:  return codec.CODECID_AUDIO_G711U  // 9  → 100
    case FLV_MP3:    return codec.CODECID_AUDIO_MP3    // 2  → 102
    }
    return codec.CODECID_UNRECOGNIZED
}
```

### 🔧 Зворотні конвертації: Internal → FLV

```go
func CovertCodecId2FlvVideoCodecId(cid codec.CodecID) FLV_VIDEO_CODEC_ID {
    if cid == codec.CODECID_VIDEO_H264 {
        return FLV_AVC
    } else if cid == codec.CODECID_VIDEO_H265 {
        return FLV_HEVC
    } else {
        panic("unsupport flv video codec")  // ⚠️ panic у продакшені!
    }
}

func CovertCodecId2SoundFromat(cid codec.CodecID) FLV_SOUND_FORMAT {  // ⚠️ опечатка: "Fromat" → "Format"
    if cid == codec.CODECID_AUDIO_AAC {
        return FLV_AAC
    } else if cid == codec.CODECID_AUDIO_G711A {
        return FLV_G711A
    } else if cid == codec.CODECID_AUDIO_G711U {
        return FLV_G711U
    } else {
        panic("unsupport flv audio codec")  // ⚠️ panic!
    }
}
```

### 🎯 Практичне застосування:

```go
// У FlvReader.createVideoTagDemuxer:
func (f *FlvReader) createVideoTagDemuxer(cid FLV_VIDEO_CODEC_ID) error {
    // 1. Конвертувати FLV ID → Internal ID
    internalCID := CovertFlvVideoCodecId2MpegCodecId(cid)
    
    // 2. Створити відповідний демуксер
    switch internalCID {
    case codec.CODECID_VIDEO_H264:
        f.videoDemuxer = NewAVCTagDemuxer()
    case codec.CODECID_VIDEO_H265:
        f.videoDemuxer = NewHevcTagDemuxer()
    }
    
    // 3. У callback передавати Internal ID для уніфікованої обробки
    f.videoDemuxer.OnFrame(func(codecid codec.CodecID, frame []byte, cts int) {
        f.OnFrame(codecid, frame, pts, dts)  // codecid = Internal ID
    })
    return nil
}

// У FlvWriter.WriteH264:
func (f *FlvWriter) WriteH264(data []byte, pts, dts uint32) error {
    // 1. Конвертувати Internal ID → FLV ID для заголовку тега
    flvCID := CovertCodecId2FlvVideoCodecId(codec.CODECID_VIDEO_H264)  // = FLV_AVC
    
    // 2. Сформувати заголовок відео-тега з правильним CodecID
    tag := VideoTag{
        FrameType: 1,      // Key frame
        CodecID:   flvCID, // 7 для AVC
        // ...
    }
    // ...
}
```

### 🐞 Потенційні проблеми:

1. **`panic` замість повернення помилки**:
   ```go
   // У зворотних конвертаціях:
   panic("unsupport flv video codec")  // ← crash сервера при невідомому кодеку!
   
   // Краще:
   return FLV_VIDEO_CODEC_ID(-1), errors.New("unsupported codec for FLV")
   // Або повернути спеціальне значення та перевіряти його у викликаючому коді
   ```

2. **Опечатка у назві функції**:
   ```go
   func CovertCodecId2SoundFromat(...)  // ← "Fromat" замість "Format"
   // Це ламає консистентність API, ускладнює пошук та автодоповнення
   ```

3. **Неповна підтримка кодеків**:
   ```go
   // У CovertFlvAudioCodecId2MpegCodecId відсутній FLV_MP3 → CODECID_AUDIO_MP3 мапінг?
   // Перевірити, чи всі підтримувані кодеки мають двосторонню конвертацію
   ```

---

## 📏 3. Розрахунок розмірів заголовків: `GetTagLenBy*Codec`

### 🔍 Проблема: різні кодеки → різні заголовки тегів

У FLV заголовок аудіо/відео тега має **змінну довжину** залежно від кодеку:

```
Аудіо тег:
├─ Загальний заголовок тега: 11 байт
├─ AudioTag header:
   │  Якщо AAC: 2 байти (SoundFormat + AACPacketType)
   │  Якщо G.711/MP3: 1 байт (тільки SoundFormat)
└─ Payload: змінна довжина

Відео тег:
├─ Загальний заголовок тега: 11 байт  
├─ VideoTag header:
   │  Якщо AVC/HEVC: 5 байт (FrameType+CodecID + AVCPacketType + CompositionTime)
   │  Якщо інше: 1 байт (тільки FrameType+CodecID)
└─ Payload: змінна довжина
```

### 🔧 Реалізація:

```go
func GetTagLenByAudioCodec(cid FLV_SOUND_FORMAT) int {
    if cid == FLV_AAC {
        return 2  // AAC має додатковий AACPacketType байт
    } else {
        return 1  // G.711/MP3: тільки SoundFormat
    }
}

func GetTagLenByVideoCodec(cid FLV_VIDEO_CODEC_ID) int {
    if cid == FLV_AVC || cid == FLV_HEVC {
        return 5  // FrameType+CodecID(1) + AVCPacketType(1) + CompositionTime(3)
    } else {
        return 1  // Тільки FrameType+CodecID
    }
}
```

### 🎯 Практичне застосування:

```go
// У FlvReader для пропуску заголовку тега:
func (f *FlvReader) handleAudioTag(data []byte) error {
    // 1. Визначити кодек з перших 4 біт
    soundFormat := FLV_SOUND_FORMAT((data[0] >> 4) & 0x0F)
    
    // 2. Розрахувати розмір заголовку
    headerLen := GetTagLenByAudioCodec(soundFormat)  // 1 або 2
    
    // 3. Пропустити заголовок, залишити payload
    payload := data[headerLen:]
    
    // 4. Передати у демуксер
    return f.audioDemuxer.Decode(payload)
}

// У FlvMuxer для формування заголовку при записі:
func (m *AudioMuxer) WriteHeader(buf *bytes.Buffer, cid FLV_SOUND_FORMAT) {
    headerLen := GetTagLenByAudioCodec(cid)
    // Виділити буфер потрібного розміру
    header := make([]byte, headerLen)
    
    if cid == FLV_AAC {
        header[0] = 0xAF  // SoundFormat=10 (AAC), Stereo, 44kHz, 16-bit
        header[1] = AAC_PACKET_TYPE_RAW  // 1 = raw AAC frame
    } else {
        header[0] = byte(cid << 4)  // SoundFormat у старших 4 бітах
    }
    buf.Write(header)
}
```

---

## 🐞 4. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **`panic` у конвертаціях**:
   ```go
   // У продакшені невідомий кодек → crash всього сервера!
   // Краще повертати помилку або спеціальне значення:
   
   func CovertCodecId2FlvVideoCodecId(cid codec.CodecID) (FLV_VIDEO_CODEC_ID, error) {
       switch cid {
       case codec.CODECID_VIDEO_H264: return FLV_AVC, nil
       case codec.CODECID_VIDEO_H265: return FLV_HEVC, nil
       default: return -1, fmt.Errorf("unsupported video codec for FLV: %d", cid)
       }
   }
   ```

2. **Опечатка "Fromat"**:
   ```go
   // Пошук/заміна в усьому проекті:
   // "CovertCodecId2SoundFromat" → "CovertCodecId2SoundFormat"
   ```

3. **Відсутність юніт-тестів**:
   ```go
   // Ці функції критичні для коректності всього FLV-модуля, але не покриті тестами!
   // Додати тести для:
   // • PutUint24/GetUint24 roundtrip
   // • Всіх мапінгів кодеків (прямо і зворотно)
   // • Граничних значень (0, 0xFFFFFF для uint24)
   ```

4. **Необроблені кодеки**:
   ```go
   // У CovertFlvAudioCodecId2MpegCodecId відсутній мапінг для FLV_MP3?
   // Перевірити консистентність усіх конвертацій
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечної конвертації з fallback
func SafeConvertFlvVideoCodec(cid FLV_VIDEO_CODEC_ID, fallback codec.CodecID) codec.CodecID {
    result := CovertFlvVideoCodecId2MpegCodecId(cid)
    if result == codec.CODECID_UNRECOGNIZED {
        return fallback  // або логувати попередження
    }
    return result
}

// 2. Константи для "магічних чисел"
const (
    FLV_VIDEO_HEADER_LEN_AVC = 5
    FLV_VIDEO_HEADER_LEN_OTHER = 1
    FLV_AUDIO_HEADER_LEN_AAC = 2
    FLV_AUDIO_HEADER_LEN_OTHER = 1
)

func GetTagLenByVideoCodec(cid FLV_VIDEO_CODEC_ID) int {
    switch cid {
    case FLV_AVC, FLV_HEVC:
        return FLV_VIDEO_HEADER_LEN_AVC
    default:
        return FLV_VIDEO_HEADER_LEN_OTHER
    }
}

// 3. Юніт-тести для утиліт
func TestPutGetUint24(t *testing.T) {
    tests := []struct{
        input uint32
        want  []byte
    }{
        {0, []byte{0,0,0}},
        {1, []byte{0,0,1}},
        {0x00F0B0, []byte{0x00,0xF0,0xB0}},
        {0xFFFFFF, []byte{0xFF,0xFF,0xFF}},  // макс. значення
    }
    for _, tt := range tests {
        buf := make([]byte, 3)
        PutUint24(buf, tt.input)
        if !bytes.Equal(buf, tt.want) {
            t.Errorf("PutUint24(%d) = %x, want %x", tt.input, buf, tt.want)
        }
        got := GetUint24(buf)
        if got != tt.input {
            t.Errorf("GetUint24(%x) = %d, want %d", buf, got, tt.input)
        }
    }
}

func TestCodecConversions(t *testing.T) {
    // Перевірити двосторонню конвертацію для всіх підтримуваних кодеків
    videoCodecs := []struct{ flv FLV_VIDEO_CODEC_ID; internal codec.CodecID }{
        {FLV_AVC, codec.CODECID_VIDEO_H264},
        {FLV_HEVC, codec.CODECID_VIDEO_H265},
    }
    for _, tc := range videoCodecs {
        got := CovertFlvVideoCodecId2MpegCodecId(tc.flv)
        if got != tc.internal {
            t.Errorf("FLV→Internal: %v → %v, want %v", tc.flv, got, tc.internal)
        }
        back := CovertCodecId2FlvVideoCodecId(tc.internal)
        if back != tc.flv {
            t.Errorf("Internal→FLV: %v → %v, want %v", tc.internal, back, tc.flv)
        }
    }
    // Аналогічно для аудіо...
}
```

---

## 🎯 5. Інтеграція з вашим CCTV HLS Processor

### 📍 У `FlvReader` — парсинг вхідного потоку:

```go
func (f *FlvReader) handleVideoTag(data []byte) error {
    // 1. Витягнути CodecID з перших 4 біт
    codecID := FLV_VIDEO_CODEC_ID(data[0] & 0x0F)
    
    // 2. Конвертувати у Internal ID для уніфікованої обробки
    internalCID := CovertFlvVideoCodecId2MpegCodecId(codecID)
    if internalCID == codec.CODECID_UNRECOGNIZED {
        return fmt.Errorf("unsupported video codec in FLV: %d", codecID)
    }
    
    // 3. Розрахувати зміщення payload
    headerLen := GetTagLenByVideoCodec(codecID)
    payload := data[headerLen:]
    
    // 4. Передати у відповідний демуксер
    return f.videoDemuxer.Decode(payload)
}
```

### 📍 У `FlvWriter` — генерація вихідного потоку:

```go
func (f *FlvWriter) writeVideoFrame(cid codec.CodecID, frame []byte, pts, dts uint32) error {
    // 1. Конвертувати Internal ID → FLV ID для заголовку
    flvCID, err := CovertCodecId2FlvVideoCodecId(cid)
    if err != nil {
        return err
    }
    
    // 2. Розрахувати розмір заголовку
    headerLen := GetTagLenByVideoCodec(flvCID)
    
    // 3. Сформувати заголовок тега
    tagHeader := make([]byte, 11 + headerLen)  // 11 = базовий FLV tag header
    tagHeader[0] = VIDEO_TAG  // TagType = 9
    PutUint24(tagHeader[1:4], uint32(headerLen + len(frame)))  // DataSize
    PutUint24(tagHeader[4:7], dts & 0xFFFFFF)  // Timestamp (молодші 24 біти)
    tagHeader[7] = byte(dts >> 24)  // TimestampExtended
    
    // 4. Записати VideoTag header
    tagHeader[11] = byte(1<<4 | flvCID)  // FrameType=1 (key) + CodecID
    if flvCID == FLV_AVC || flvCID == FLV_HEVC {
        tagHeader[12] = AVC_NALU  // AVCPacketType = 1 (NAL unit)
        PutUint24(tagHeader[13:16], uint32(pts - dts))  // CompositionTime
    }
    
    // 5. Записати у вихід
    f.writer.Write(tagHeader)
    f.writer.Write(frame)
    return nil
}
```

### 📍 У метриках — моніторинг кодеків:

```go
func (f *FlvReader) recordCodecMetrics(flvcid interface{}, direction string) {
    var internalCID codec.CodecID
    switch cid := flvcid.(type) {
    case FLV_VIDEO_CODEC_ID:
        internalCID = CovertFlvVideoCodecId2MpegCodecId(cid)
    case FLV_SOUND_FORMAT:
        internalCID = CovertFlvAudioCodecId2MpegCodecId(cid)
    }
    
    if internalCID != codec.CODECID_UNRECOGNIZED {
        metrics.FLVCodecUsage.WithLabelValues(
            direction,  // "read" or "write"
            codec.CodecString(internalCID),
        ).Inc()
    }
}
```

---

## 🧭 Висновок: чому ці утиліти — фундамент коректності

| Функція | Роль у FLV-модулі | Вартість помилки без неї |
|---------|------------------|-------------------------|
| `PutUint24`/`GetUint24` | Коректне кодування 24-бітних полів | Неправильний DataSize/Timestamp → пошкоджені теги, неможливість seek |
| Конвертації кодеків | Прозора робота з різними стандартами | Невідповідність CodecID → демуксер не створиться, кадр відкидається |
| `GetTagLenBy*Codec` | Точне визначення меж payload | Зсув при читанні → парсинг "з'їдає" дані наступного тега |

> 🔑 **Головна ідея**: Ці функції — **інфраструктурний шар**, який абстрагує специфіку формату FLV від бізнес-логіки. Без них кожен компонент пайплайну змушений був би знати деталі бітового представлення FLV-тегів, що призвело б до дублювання коду, помилок та важкої підтримки.

💡 **Фінальна порада**: 
1. Виправте опечатку: `CovertCodecId2SoundFromat` → `CovertCodecId2SoundFormat`
2. Замініть `panic` на повернення помилок у зворотних конвертаціях
3. Додайте юніт-тести для `PutUint24`/`GetUint24` з граничними значеннями (0, 1, 0xFFFFFF)
4. Додайте тест на двосторонню конвертацію всіх підтримуваних кодеків

Це перетворить ці утиліти з "допоміжного коду" на "гарантовано коректний фундамент" для всього FLV-модуля.