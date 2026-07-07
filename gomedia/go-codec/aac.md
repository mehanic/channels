# 🔊 `codec`: Робота з аудіокодеками AAC та заголовками ADTS/ASC

Це **низькорівневий модуль** для обробки аудіокодеку AAC (Advanced Audio Coding) у бібліотеці медіа-обробки. Він реалізує парсинг та генерацію заголовків **ADTS** (Audio Data Transport Stream) та **AudioSpecificConfiguration** (ASC) — критично для правильного пакування/розпакування AAC-аудіо у форматах MP4, TS, HLS.

---

## 🎯 Коротка відповідь

> **Це "декодер заголовків AAC"**: він перетворює сирі біти заголовків ADTS/ASC у типобезпечні структури Go та навпаки, забезпечуючи коректне пакування аудіо для стрімінгу, транскодування та інтеграції з медіа-контейнерами.

---

## 🧱 Основні компоненти

### 🔹 Константи профілів та частот дискретизації

```go
type AAC_PROFILE int
const (
    MAIN AAC_PROFILE = iota  // 🔹 Основний профіль (рідко використовується)
    LC                       // 🔹 Low Complexity — найпоширеніший для стрімінгу
    SSR                      // 🔹 Scalable Sampling Rate (застарілий)
)

type AAC_SAMPLING_FREQUENCY int
const (
    AAC_SAMPLE_96000 AAC_SAMPLING_FREQUENCY = iota  // 🔹 96 кГц
    AAC_SAMPLE_88200                                 // 🔹 88.2 кГц
    AAC_SAMPLE_64000                                 // 🔹 64 кГц
    AAC_SAMPLE_48000                                 // 🔹 48 кГц — стандарт для відео
    AAC_SAMPLE_44100                                 // 🔹 44.1 кГц — стандарт для аудіо
    // ... ще 7 значень до 7350 Гц
)

// 🔹 Таблиця для швидкого пошуку індексу за частотою
var AAC_Sampling_Idx [13]int = [13]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
```

**🎯 Призначення**: Типобезпечне представлення параметрів AAC згідно зі стандартом ISO/IEC 13818-7.

---

### 🔹 `ADTS_Frame_Header` — заголовок транспортного потоку AAC

**📐 Структура згідно з таблицею 5 стандарту:**

```
📦 ADTS Fixed Header (28 біт):
├── syncword: 12 біт (0xFFF) — синхронізація кадру
├── ID: 1 біт (0=MPEG-4, 1=MPEG-2)
├── layer: 2 біти (завжди 0 для AAC)
├── protection_absent: 1 біт (1=без CRC, 0=з CRC)
├── profile: 2 біти (0=Main, 1=LC, 2=SSR)
├── sampling_frequency_index: 4 біти (індекс у таблиці частот)
├── private_bit: 1 біт (користувацький прапорець)
├── channel_configuration: 3 біти (0=за ASC, 1-7=кількість каналів)
├── original/copy: 1 біт
├── home: 1 біт

📦 ADTS Variable Header (28 біт):
├── copyright_identification_bit: 1 біт
├── copyright_identification_start: 1 біт
├── frame_length: 13 біт (загальна довжина кадру у байтах)
├── adts_buffer_fullness: 11 біт (0x7FF для VBR)
├── number_of_raw_data_blocks_in_frame: 2 біти (кількість блоків даних)
```

**🔢 Приклад кодування заголовка:**
```
🔹 Вхід: LC профіль, 48 кГц, 2 канали, frame_length=1024
🔹 Бітова маска:
   Байт 0: 11111111 = 0xFF (syncword high)
   Байт 1: 11110001 = 0xF1 (syncword low + ID=0 + layer=0 + protection_absent=1)
   Байт 2: 01011000 = 0x58 (profile=1<<6 + sampling_index=3<<2 + ...)
   Байт 3: 00000000 = 0x00 (channel_config=2<<6 + ...)
   Байт 4: 00000100 = 0x04 (frame_length high bits)
   Байт 5: 00000000 = 0x00 (frame_length low bits + buffer_fullness)
   Байт 6: 11111100 = 0xFC (buffer_fullness + raw_data_blocks=0)

🔹 Вихід: []byte{0xFF, 0xF1, 0x58, 0x00, 0x04, 0x00, 0xFC}
```

---

### 🔹 `AudioSpecificConfiguration` (ASC) — конфігурація декодера

**📐 Структура згідно з таблицею 4 стандарту:**

```
📦 ASC (2 байти для базової конфігурації):
├── audio_object_type: 5 біт (профіль: 2=LC, 5=HE-AAC, тощо)
├── sampling_frequency_index: 4 біти
├── channel_configuration: 4 біти
├── GA_framelength_flag: 1 біт (1=1024 семпли, 0=960 семплів)
├── GA_depends_on_core_coder: 1 біт
├── GA_extension_flag: 1 біт
```

**🎯 Призначення**: Передавати параметри декодера у ініціалізаційному сегменті (fMP4) або ESDS боксі (MP4).

**🔢 Приклад кодування:**
```
🔹 Вхід: LC (2), 48 кГц (індекс 4), 2 канали
🔹 Бітова маска:
   Байт 0: (2 << 3) | (4 >> 1) = 00010000 | 00000010 = 00010010 = 0x12
   Байт 1: (4 & 0x01 << 7) | (2 << 3) = 00000000 | 00010000 = 00010000 = 0x10
🔹 Вихід: []byte{0x12, 0x10}
```

---

## 🔍 Ключові функції

### 🔹 `NewAdtsFrameHeader()` — конструктор заголовка

```go
func NewAdtsFrameHeader() *ADTS_Frame_Header {
    return &ADTS_Frame_Header{
        Fix_Header: ADTS_Fix_Header{
            ID:                       0,              // 🔹 MPEG-4
            Layer:                    0,              // 🔹 Завжди 0 для AAC
            Protection_absent:        1,              // 🔹 Без CRC (економія 2 байт)
            Profile:                  uint8(MAIN),    // 🔹 За замовчуванням Main
            Sampling_frequency_index: uint8(AAC_SAMPLE_44100), // 🔹 44.1 кГц
            Private_bit:              0,
            Channel_configuration:    0,  // 🔹 0 = взяти з ASC
            Originalorcopy:           0,
            Home:                     0,
        },
        Variable_Header: ADTS_Variable_Header{
            Frame_length:                       0,  // 🔹 Заповнюється при кодуванні
            Adts_buffer_fullness:               0,  // 🔹 0x7FF для VBR
            Number_of_raw_data_blocks_in_frame: 0,  // 🔹 Завжди 0 для AAC
        },
    }
}
```

**🎯 Призначення**: Створити порожній заголовок з безпечними значеннями за замовчуванням.

---

### 🔹 `Decode(aac []byte)` — парсинг сирих байт у структуру

```go
func (frame *ADTS_Frame_Header) Decode(aac []byte) {
    _ = aac[6]  // 🔹 Перевірка на паніку при короткому масиві
    
    // 🔹 Байт 1: ID, layer, protection_absent
    frame.Fix_Header.ID = aac[1] >> 3
    frame.Fix_Header.Layer = aac[1] >> 1 & 0x03
    frame.Fix_Header.Protection_absent = aac[1] & 0x01
    
    // 🔹 Байт 2: profile, sampling_index, private_bit, channel_config (частково)
    frame.Fix_Header.Profile = aac[2] >> 6 & 0x03
    frame.Fix_Header.Sampling_frequency_index = aac[2] >> 2 & 0x0F
    frame.Fix_Header.Private_bit = aac[2] >> 1 & 0x01
    frame.Fix_Header.Channel_configuration = (aac[2] & 0x01 << 2) | (aac[3] >> 6)
    
    // 🔹 Байт 3-6: решта полів
    // ... (бітові операції для frame_length, buffer_fullness тощо)
}
```

**🔄 Потік даних:**
```
🔹 Вхід: []byte{0xFF, 0xF1, 0x58, 0x00, 0x04, 0x00, 0xFC}
│
▼
🔹 Розпакування бітів:
   • aac[1] = 0xF1 = 0b11110001
   • ID = 0b11110001 >> 3 = 0b00011110 & 0x01 = 0
   • Layer = 0b11110001 >> 1 & 0x03 = 0b01111000 & 0x03 = 0
   • Protection_absent = 0b11110001 & 0x01 = 1
   • ...
│
▼
🔹 Вихід: заповнена структура *ADTS_Frame_Header
```

**⚠️ Важливо**: Функція не повертає помилку — перевірка довжини через `_ = aac[6]` викликає паніку при нестачі байт.

---

### 🔹 `Encode()` — серіалізація структури у сирі байти

```go
func (frame *ADTS_Frame_Header) Encode() []byte {
    var hdr []byte
    if frame.Fix_Header.Protection_absent == 1 {
        hdr = make([]byte, 7)  // 🔹 Без CRC: 7 байт заголовка
    } else {
        hdr = make([]byte, 9)  // 🔹 З CRC: 9 байт (додаткові 2 байти CRC)
    }
    
    // 🔹 Синхронізація: завжди 0xFFF
    hdr[0] = 0xFF
    hdr[1] = 0xF0
    
    // 🔹 Формування байт 1-6 через бітові операції
    hdr[1] = hdr[1] | (frame.Fix_Header.ID << 3) | ...
    // ...
    
    return hdr
}
```

**🎯 Призначення**: Підготувати заголовок для запису у потік або файл.

---

### 🔹 `ConvertADTSToASC()` / `ConvertASCToADTS()` — конвертація між форматами

```go
func ConvertADTSToASC(frame []byte) (*AudioSpecificConfiguration, error) {
    if len(frame) < 7 { return nil, errors.New("len of frame < 7") }
    
    adts := NewAdtsFrameHeader()
    adts.Decode(frame)
    
    asc := NewAudioSpecificConfiguration()
    asc.Audio_object_type = adts.Fix_Header.Profile + 1  // 🔹 Профіль у ASC = profile+1
    asc.Channel_configuration = adts.Fix_Header.Channel_configuration
    asc.Sample_freq_index = adts.Fix_Header.Sampling_frequency_index
    
    return asc, nil
}

func ConvertASCToADTS(asc []byte, aacbytes int) (*ADTS_Frame_Header, error) {
    aac_asc := NewAudioSpecificConfiguration()
    err := aac_asc.Decode(asc)
    if err != nil { return nil, err }
    
    aac_adts := NewAdtsFrameHeader()
    aac_adts.Fix_Header.Profile = aac_asc.Audio_object_type - 1  // 🔹 Зворотне перетворення
    aac_adts.Fix_Header.Channel_configuration = aac_asc.Channel_configuration
    aac_adts.Fix_Header.Sampling_frequency_index = aac_asc.Sample_freq_index
    aac_adts.Fix_Header.Protection_absent = 1  // 🔹 Без CRC для ефективності
    aac_adts.Variable_Header.Adts_buffer_fullness = 0x3F  // 🔹 Стандартне значення
    aac_adts.Variable_Header.Frame_length = uint16(aacbytes)  // 🔹 Загальна довжина кадру
    
    return aac_adts, nil
}
```

**🎯 Призначення**: Конвертувати параметри між **транспортним форматом** (ADTS, для потокової передачі) та **конфігураційним форматом** (ASC, для ініціалізації декодера у MP4/fMP4).

**🔄 Коли це потрібно:**
- ✅ При пакуванні AAC у MP4: ASC → ESDS бокс
- ✅ При розпакуванні AAC з TS: ADTS → ASC для ініціалізації декодера
- ✅ При транскодуванні між форматами: ADTS ↔ ASC

---

### 🔹 Допоміжні функції для частот дискретизації

```go
func SampleToAACSampleIndex(sampling int) int {
    for i, v := range AAC_Sampling_Idx {
        if v == sampling {
            return i
        }
    }
    panic("not Found AAC Sample Index")  // 🔹 Паніка при невідомій частоті
}

func AACSampleIdxToSample(idx int) int {
    return AAC_Sampling_Idx[idx]  // 🔹 Без перевірки меж — обережно!
}
```

**⚠️ Ризик**: `panic` у продакшені — краще повертати `(int, error)`.

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Паніка замість помилок

```go
func SampleToAACSampleIndex(sampling int) int {
    // ...
    panic("not Found AAC Sample Index")  // ❌ Небезпечно для продакшену
}

func (frame *ADTS_Frame_Header) Decode(aac []byte) {
    _ = aac[6]  // ❌ Паніка при короткому масиві замість помилки
}
```

**✅ Рішення**: Повертати `(value, error)`:
```go
func SampleToAACSampleIndex(sampling int) (int, error) {
    for i, v := range AAC_Sampling_Idx {
        if v == sampling {
            return i, nil
        }
    }
    return 0, fmt.Errorf("unknown sampling frequency: %d Hz", sampling)
}

func (frame *ADTS_Frame_Header) Decode(aac []byte) error {
    if len(aac) < 7 {
        return fmt.Errorf("ADTS header too short: got %d, need >=7", len(aac))
    }
    // ... парсинг
    return nil
}
```

---

### 🔴 Проблема 2: Відсутність валідації полів при кодуванні

```go
func (asc *AudioSpecificConfiguration) Encode() []byte {
    buf := make([]byte, 2)
    // 🔹 Бітові операції без перевірки діапазонів
    buf[0] = (asc.Audio_object_type & 0x1f << 3) | ...
    return buf
}
```

**🎯 Ризик**: Некоректні значення (напр., `Audio_object_type=31`) призведуть до невалідного заголовка.

**✅ Рішення**: Додати валідацію:
```go
func (asc *AudioSpecificConfiguration) Validate() error {
    if asc.Audio_object_type > 31 {
        return fmt.Errorf("invalid audio_object_type: %d", asc.Audio_object_type)
    }
    if asc.Sample_freq_index > 12 {
        return fmt.Errorf("invalid sample_freq_index: %d", asc.Sample_freq_index)
    }
    if asc.Channel_configuration > 15 {
        return fmt.Errorf("invalid channel_configuration: %d", asc.Channel_configuration)
    }
    return nil
}

func (asc *AudioSpecificConfiguration) Encode() ([]byte, error) {
    if err := asc.Validate(); err != nil {
        return nil, err
    }
    // ... кодування
    return buf, nil
}
```

---

### 🟡 Проблема 3: Неочевидна логіка конвертації профілів

```go
// 🔹 У ConvertADTSToASC:
asc.Audio_object_type = adts.Fix_Header.Profile + 1

// 🔹 У ConvertASCToADTS:
aac_adts.Fix_Header.Profile = aac_asc.Audio_object_type - 1
```

**🎯 Пояснення**: У ADTS профіль кодується як 0=Main, 1=LC, 2=SSR, а у ASC — як 1=Main, 2=LC, 3=SSR (згідно з ISO/IEC 14496-3). Тому потрібне зміщення на 1.

**✅ Рішення**: Додати коментар або константи для ясності:
```go
// 🔹 Профіль у ADTS (2 біти): 0=Main, 1=LC, 2=SSR
// 🔹 Профіль у ASC (5 біт): 1=Main, 2=LC, 3=SSR (ISO/IEC 14496-3 Table 1.13)
const (
    ADTS_PROFILE_MAIN = iota  // 0
    ADTS_PROFILE_LC           // 1
    ADTS_PROFILE_SSR          // 2
)

const (
    ASC_PROFILE_MAIN = 1 + iota  // 1
    ASC_PROFILE_LC               // 2
    ASC_PROFILE_SSR              // 3
)

func ConvertADTSToASC(...) {
    asc.Audio_object_type = adts.Fix_Header.Profile + ASC_PROFILE_MAIN  // 🔹 Явне зміщення
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Парсинг аудіо з TS-потоку камери

```go
func ParseAACFromTS(tsPacket []byte) (*codec.AudioSpecificConfiguration, error) {
    // 🔹 Пошук синхрослова 0xFFF у пакеті
    for i := 0; i < len(tsPacket)-6; i++ {
        if tsPacket[i] == 0xFF && (tsPacket[i+1]&0xF0) == 0xF0 {
            // 🔹 Знайдено ADTS-заголовок
            adts := codec.NewAdtsFrameHeader()
            adts.Decode(tsPacket[i:])
            
            // 🔹 Конвертація у ASC для ініціалізації декодера
            asc, err := codec.ConvertADTSToASC(tsPacket[i:])
            if err != nil {
                return nil, fmt.Errorf("ADTS→ASC conversion failed: %w", err)
            }
            
            return asc, nil
        }
    }
    return nil, fmt.Errorf("no ADTS header found in TS packet")
}
```

---

### 🔹 Приклад 2: Генерація ініціалізаційного сегмента для fMP4

```go
func GenerateAACInitSegment(asc *codec.AudioSpecificConfiguration) ([]byte, error) {
    // 🔹 Кодування ASC у 2 байти
    ascBytes := asc.Encode()
    
    // 🔹 Створення ES Descriptor (спрощено)
    esds := &mp4.Esds{
        FullBox: mp4.FullBox{Version: 0, Flags: 0},
        ESDescriptor: &mp4.ESDescriptor{
            Tag: 0x03,
            Size: 0x19,
            ESID: 1,
            StreamPriority: 0,
            DecoderConfigDescriptor: &mp4.DecoderConfigDescriptor{
                Tag: 0x04,
                Size: 0x12,
                ObjectType: 0x40,  // 🔹 AAC
                StreamType: 0x15,  // 🔹 Audio
                BufferSizeDB: 0,
                MaxBitrate: 128000,
                AvgBitrate: 128000,
                DecoderSpecificInfo: &mp4.DecoderSpecificInfo{
                    Tag: 0x05,
                    Size: 2,
                    Data: ascBytes,  // 🔹 ASC у Data
                },
            },
        },
    }
    
    // 🔹 Серіалізація у байти (використовуючи бібліотеку mp4)
    var buf bytes.Buffer
    if _, err := mp4.Marshal(&buf, esds, mp4.Context{}); err != nil {
        return nil, err
    }
    
    return buf.Bytes(), nil
}
```

---

### 🔹 Приклад 3: Додавання ADTS-заголовків до сирих AAC-фреймів для TS

```go
func AddADTSHeaders(rawAAC []byte, asc *codec.AudioSpecificConfiguration) ([]byte, error) {
    // 🔹 Конвертація ASC → ADTS
    adts, err := codec.ConvertASCToADTS(asc.Encode(), len(rawAAC)+7)
    if err != nil {
        return nil, err
    }
    
    // 🔹 Кодування ADTS-заголовка
    hdr := adts.Encode()
    
    // 🔹 Об'єднання заголовка + аудіо-даних
    result := make([]byte, len(hdr)+len(rawAAC))
    copy(result, hdr)
    copy(result[len(hdr):], rawAAC)
    
    return result, nil
}

// 🔹 Використання у конвеєрі:
rawAAC := getRawAACFromEncoder()  // 🔹 Сирі дані без заголовка
asc := getAudioSpecificConfig()    // 🔹 З ініціалізації декодера

packet, err := AddADTSHeaders(rawAAC, asc)
if err != nil {
    log.Printf("❌ Failed to add ADTS header: %v", err)
} else {
    writeTSPacket(packet)  // 🔹 Запис у TS-потік
}
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При парсингу ADTS:
    • Перевіряйте довжину вхідного масиву перед Decode()
    • Валідуйте sampling_frequency_index у діапазоні [0, 12]
    • Обробляйте channel_configuration=0 (взяти з ASC) окремо

[ ] При кодуванні ASC:
    • Використовуйте Validate() перед Encode()
    • Для LC профілю: Audio_object_type=2 (ASC) ↔ Profile=1 (ADTS)
    • Зберігайте ASC у ініціалізаційному сегменті для fMP4

[ ] Для конвертації ADTS↔ASC:
    • Пам'ятайте про зміщення профілю на 1 між форматами
    • Для VBR встановлюйте Adts_buffer_fullness=0x7FF
    • Frame_length у ADTS = 7 (заголовок) + довжина аудіо-даних

[ ] Для безпеки:
    • Замініть panic на повернення помилок у публічних функціях
    • Валідуйте вхідні дані перед бітовими операціями
    • Обмежуйте максимальну довжину frame_length (напр., 10 КБ для AAC)

[ ] Для тестування:
    • Створюйте тестові вектори з відомими заголовками (0xFFF1...)
    • Перевіряйте round-trip: Decode → Encode → порівняння байт
    • Тестуйте крайні випадки: мінімальна/максимальна частота, різні профілі
```

---

## 🎯 Висновок

> **Цей модуль — "міст" між сирими бітами AAC та типобезпечним кодом Go**, який забезпечує:
> • ✅ Коректний парсинг та генерацію заголовків ADTS/ASC згідно зі стандартом
> • ✅ Конвертацію між транспортним (ADTS) та конфігураційним (ASC) форматами
> • ✅ Типобезпечне представлення профілів, частот дискретизації, конфігурацій каналів
> • ✅ Інтеграцію з медіа-контейнерами (MP4, TS, HLS) через правильні заголовки

Для вашого **CCTV HLS Processor** це означає:
- 🔊 Надійне пакування аудіо з камер у AAC/ADTS для стрімінгу
- 📦 Коректна генерація ініціалізаційних сегментів для fMP4/HLS
- 🔄 Прозора конвертація між форматами при транскодуванні
- 🛡️ Захист від невалідних заголовків через валідацію полів
- 🧪 Легке тестування через контрольовані бітові вектори

Потребуєте допомоги з інтеграцією цього модуля у ваш конвеєр обробки аудіо або з налаштуванням валідації заголовків для різних кодеків? Напишіть — покажу готовий код для вашого сценарію! 🚀🔊