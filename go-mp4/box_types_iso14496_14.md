# 🎵 `esds`: ES Descriptor Box для MPEG-4 аудіо/відео

Це код для роботи з **`esds` боксом** (Elementary Stream Descriptor) у форматі MP4/fMP4. Цей бокс містить **конфігурацію MPEG-4 кодеків**, зокрема AAC аудіо — найпоширеніший формат звуку у HLS-стрімінгу.

---

## 🎯 Коротка відповідь

> **`esds` — це "паспорт" MPEG-4 потоку**, який каже декодеру: *"Це аудіо закодовано в AAC, ось параметри: тип об'єкта, бітрейт, конфігураційні дані кодека"*.

---

## 🧱 Архітектура: Ієрархія дескрипторів

```
📦 esds бокс
│
├── 🔹 FullBox (version + flags)
│
└── 🔹 Descriptors[] (масив вкладених дескрипторів)
    │
    ├── 📋 ESDescriptor (Tag=0x03) — опис потоку
    │   ├── ESID: ідентифікатор потоку
    │   ├── прапорці: StreamDependenceFlag, UrlFlag, OcrStreamFlag
    │   └── опціональні поля (залежать від прапорців)
    │
    ├── 📋 DecoderConfigDescriptor (Tag=0x04) — конфігурація декодера ⭐
    │   ├── ObjectTypeIndication: тип кодека (0x40=AAC, 0x20=AVC...)
    │   ├── StreamType: аудіо (0x05) / відео (0x04)
    │   ├── BufferSizeDB, MaxBitrate, AvgBitrate
    │   └── DecSpecificInfo (Tag=0x05) — сирі дані кодека!
    │
    ├── 📋 DecSpecificInfo (Tag=0x05) — 🔹 НАЙВАЖЛИВІШЕ!
    │   └── Data: сирі байти конфігурації AAC (AudioSpecificConfig)
    │
    └── 📋 SLConfigDescriptor (Tag=0x06) — синхронізація шару
```

---

## 🔑 Ключові константи: Теги дескрипторів

```go
const (
    ESDescrTag            = 0x03  // ES Descriptor — опис потоку
    DecoderConfigDescrTag = 0x04  // Decoder Config — параметри декодера ⭐
    DecSpecificInfoTag    = 0x05  // Decoder Specific Info — сирі дані кодека 🔥
    SLConfigDescrTag      = 0x06  // SL Config — синхронізація
)
```

> 🎯 **Важливо**: `DecSpecificInfoTag (0x05)` містить **AudioSpecificConfig** для AAC — це ключ до ініціалізації декодера!

---

## 🔍 Детальний розбір структури `Descriptor`

```go
type Descriptor struct {
    BaseCustomFieldObject
    Tag                     int8                     `mp4:"0,size=8"`  // тип дескриптора: 0x03, 0x04, 0x05...
    Size                    uint32                   `mp4:"1,varint"`  // 🔹 ЗМІННА довжина! (varint кодування)
    ESDescriptor            *ESDescriptor            `mp4:"2,extend,opt=dynamic"`
    DecoderConfigDescriptor *DecoderConfigDescriptor `mp4:"3,extend,opt=dynamic"`
    Data                    []byte                   `mp4:"4,size=8,opt=dynamic,len=dynamic"`
}
```

### 🔹 `varint` — змінна довжина поля `Size`

```
🔢 Формат varint у MPEG-4:
• Кожен байт: [продовження:1][дані:7]
• Якщо біт 7 = 1 → читаємо наступний байт
• Якщо біт 7 = 0 → це останній байт

📊 Приклади:
• 0x7F = 127 (1 байт)
• 0x80 0x01 = 128 (2 байти)
• 0xFF 0x7F = 16383 (2 байти)

🎯 Навіщо? Економія місця: малі значення займають 1 байт замість 4!
```

---

### 🔹 `opt=dynamic` — опціональні поля залежать від `Tag`

```go
func (ds *Descriptor) IsOptFieldEnabled(name string, ctx Context) bool {
    switch ds.Tag {
    case ESDescrTag:  // 0x03
        return name == "ESDescriptor"  // увімкнути ESDescriptor
    case DecoderConfigDescrTag:  // 0x04
        return name == "DecoderConfigDescriptor"  // увімкнути DecoderConfig
    default:
        return name == "Data"  // для інших тегів — сирі дані
    }
}
```

**🎯 Приклад:**
```
📦 Дескриптор з Tag=0x04 (DecoderConfig):
• Tag: 0x04
• Size: 0x0F (15 байт)
• 🔹 Увімкнено: DecoderConfigDescriptor (бо Tag=0x04)
• 🔹 Вимкнено: ESDescriptor, Data (бо не підходять)

📦 Дескриптор з Tag=0x05 (DecSpecificInfo):
• Tag: 0x05
• Size: 0x02 (2 байти)
• 🔹 Увімкнено: Data (бо default case)
• 🔹 Data: []byte{0x12, 0x34} ← сирі байти конфігурації AAC!
```

---

### 🔹 `len=dynamic` — довжина `Data` залежить від `Size`

```go
func (ds *Descriptor) GetFieldLength(name string, ctx Context) uint {
    switch name {
    case "Data":
        return uint(ds.Size)  // ← довжина = значення поля Size!
    }
    panic(fmt.Errorf("invalid field: %s", name))
}
```

> 🎯 **Магія**: Бібліотека питає: "Яка довжина у поля Data?" → Ви відповідаєте: "Стільки, скільки вказано в `Size`!" → Бібліотека читає точно стільки байт.

---

## 🔍 Розбір `ESDescriptor` — опис потоку

```go
type ESDescriptor struct {
    BaseCustomFieldObject
    ESID                 uint16 `mp4:"0,size=16"`  // ідентифікатор потоку
    StreamDependenceFlag bool   `mp4:"1,size=1"`    // чи залежить від іншого потоку?
    UrlFlag              bool   `mp4:"2,size=1"`    // чи є URL зовнішніх даних?
    OcrStreamFlag        bool   `mp4:"3,size=1"`    // чи є OCR-потік (для субтитрів)?
    StreamPriority       int8   `mp4:"4,size=5"`    // пріоритет потоку (0-31)
    
    // 🔹 Опціональні поля (увімкнені прапорцями вище)
    DependsOnESID        uint16 `mp4:"5,size=16,opt=dynamic"`      // якщо StreamDependenceFlag=1
    URLLength            uint8  `mp4:"6,size=8,opt=dynamic"`       // якщо UrlFlag=1
    URLString            []byte `mp4:"7,size=8,len=dynamic,opt=dynamic,string"`  // якщо UrlFlag=1
    OCRESID              uint16 `mp4:"8,size=16,opt=dynamic"`      // якщо OcrStreamFlag=1
}
```

### 🔹 Прапорці та опціональні поля

```go
func (esds *ESDescriptor) IsOptFieldEnabled(name string, ctx Context) bool {
    switch name {
    case "DependsOnESID":
        return esds.StreamDependenceFlag  // увімкнути, якщо прапорець=1
    case "URLLength", "URLString":
        return esds.UrlFlag               // увімкнути, якщо прапорець=1
    case "OCRESID":
        return esds.OcrStreamFlag         // увімкнути, якщо прапорець=1
    default:
        return false
    }
}
```

**🎯 Для вашого CCTV**: Зазвичай всі прапорці = 0 → опціональні поля відсутні → простіша структура.

---

## 🔍 Розбір `DecoderConfigDescriptor` — конфігурація декодера ⭐

```go
type DecoderConfigDescriptor struct {
    BaseCustomFieldObject
    ObjectTypeIndication byte   `mp4:"0,size=8"`   // 🔹 Тип кодека! (0x40=AAC, 0x20=AVC...)
    StreamType           int8   `mp4:"1,size=6"`   // 🔹 Тип потоку: 0x05=аудіо, 0x04=відео
    UpStream             bool   `mp4:"2,size=1"`   // чи потік upload (рідко)
    Reserved             bool   `mp4:"3,size=1"`   // завжди 1
    BufferSizeDB         uint32 `mp4:"4,size=24"`  // розмір буфера декодера (3 байти!)
    MaxBitrate           uint32 `mp4:"5,size=32"`  // максимальний бітрейт
    AvgBitrate           uint32 `mp4:"6,size=32"`  // середній бітрейт
    // 🔹 Далі йде DecSpecificInfo (Tag=0x05) з сирими даними кодека!
}
```

### 🔹 `ObjectTypeIndication` — типи кодеків

| Значення | Кодек | Опис |
|----------|-------|------|
| `0x40` | 🔹 AAC LC | AAC Low Complexity — ✅ найпоширеніший для стрімінгу |
| `0x41` | AAC Main | AAC Main Profile |
| `0x42` | AAC SSR | AAC Scalable Sample Rate |
| `0x43` | AAC LTP | AAC Long Term Prediction |
| `0x44` | 🔹 AAC SBR | AAC + Spectral Band Replication (HE-AAC) |
| `0x45` | 🔹 AAC PS | AAC + Parametric Stereo (HE-AAC v2) |
| `0x20` | 🔹 AVC/H.264 | Відео H.264 |
| `0x21` | 🔹 HEVC/H.265 | Відео H.265 |

**Для вашого CCTV**: Зазвичай `0x40` (AAC LC) для аудіо, `0x20` (AVC) або `0x21` (HEVC) для відео.

### 🔹 `StreamType` — тип медіа

| Значення | Тип | Опис |
|----------|-----|------|
| `0x04` | 🔹 Visual | Відео-потік |
| `0x05` | 🔹 Audio | Аудіо-потік |
| `0x06` | Scene Description | MPEG-4 сцени |
| `0x07` | Visual Object | 2D/3D об'єкти |

---

## 🔥 `DecSpecificInfo` — сирі дані кодека (найважливіше!)

Це **вкладений дескриптор з `Tag=0x05`**, який містить **бінарну конфігурацію кодека**.

### 🔹 Для AAC: `AudioSpecificConfig` (2-7 байт)

```
📦 Приклад: AAC LC, 48kHz, стерео
Байти: [0x12, 0x10]

🔢 Бітова розбивка:
0001 0010  0001 0000
││││ ││││  ││││ ││││
││││ │││└──┘│││ └┴┴┴┴→ ChannelConfig: 2 = стерео
││││ ││└───────┘→ SamplingFrequencyIndex: 4 = 48000 Hz
││││ └──────────→ ObjectType: 2 = AAC LC
│││└────────────→ залежить від розширень...
```

**🎯 Це ключ до ініціалізації декодера!** Без цих байтів плеєр не знатиме, як декодувати потік.

---

## 🛠️ Практичне використання у вашому HLS-процесорі

### 🔹 Приклад 1: Читання AAC-конфігурації з fMP4-сегмента

```go
import "github.com/abema/go-mp4"

func extractAACConfig(filePath string) (*AACConfig, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    var aacConfig *AACConfig
    
    _, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeEsds() {
            esds := &mp4.Esds{}
            if _, err := h.ReadPayload(esds); err != nil {
                return nil, err
            }
            
            // 🔍 Шукаємо DecoderConfigDescriptor (Tag=0x04)
            for _, desc := range esds.Descriptors {
                if desc.Tag == mp4.DecoderConfigDescrTag && desc.DecoderConfigDescriptor != nil {
                    dcd := desc.DecoderConfigDescriptor
                    
                    // 🔍 Шукаємо DecSpecificInfo (Tag=0x05) всередині
                    // (це вкладений дескриптор — треба парсити Data)
                    if dcd.ObjectTypeIndication == 0x40 {  // AAC
                        // 🔥 Тут має бути логіка парсингу AudioSpecificConfig з Data
                        // Для простоти: повертаємо сирі байти
                        aacConfig = &AACConfig{
                            ObjectType:   dcd.ObjectTypeIndication,
                            StreamType:   dcd.StreamType,
                            MaxBitrate:   dcd.MaxBitrate,
                            AvgBitrate:   dcd.AvgBitrate,
                            // ConfigData: parseAudioSpecificConfig(desc.Data),
                        }
                    }
                }
            }
        }
        return nil, nil
    })
    
    return aacConfig, err
}

type AACConfig struct {
    ObjectType   byte
    StreamType   int8
    MaxBitrate   uint32
    AvgBitrate   uint32
    // ConfigData []byte  // сирі байти AudioSpecificConfig
}
```

---

### 🔹 Приклад 2: Валідація аудіо-потоку перед стрімінгом

```go
func validateAACStream(esds *mp4.Esds) error {
    for _, desc := range esds.Descriptors {
        if desc.Tag != mp4.DecoderConfigDescrTag {
            continue
        }
        dcd := desc.DecoderConfigDescriptor
        if dcd == nil {
            continue
        }
        
        // 🔹 Перевірка типу кодека
        if dcd.ObjectTypeIndication != 0x40 {  // AAC LC
            return fmt.Errorf("unsupported audio codec: 0x%02x", dcd.ObjectTypeIndication)
        }
        
        // 🔹 Перевірка типу потоку
        if dcd.StreamType != 0x05 {  // Audio
            return fmt.Errorf("expected audio stream, got type 0x%02x", dcd.StreamType)
        }
        
        // 🔹 Перевірка бітрейту (для мобільних мереж)
        if dcd.AvgBitrate > 256000 {  // >256 kbps
            log.Printf("⚠️  High audio bitrate %d bps may cause buffering", dcd.AvgBitrate)
        }
        
        // 🔹 Перевірка наявності DecSpecificInfo (Tag=0x05)
        // (тут потрібен додатковий парсинг вкладених дескрипторів)
        
        return nil
    }
    return fmt.Errorf("DecoderConfigDescriptor not found in esds")
}
```

---

### 🔹 Приклад 3: Генерація `esds` для нового AAC-потоку

```go
func createEsdsForAAC(sampleRate int, channels int, bitrate int) *mp4.Esds {
    // 🔹 Формуємо AudioSpecificConfig (спрощено)
    // ObjectType: 2 (AAC LC), SamplingFrequencyIndex, ChannelConfig
    configData := buildAudioSpecificConfig(sampleRate, channels)
    
    return &mp4.Esds{
        FullBox: mp4.FullBox{Version: 0, Flags: [3]byte{0, 0, 0}},
        Descriptors: []mp4.Descriptor{
            // 🔹 ESDescriptor (Tag=0x03)
            {
                Tag:  mp4.ESDescrTag,
                Size: 25,  // приблизна довжина
                ESDescriptor: &mp4.ESDescriptor{
                    ESID:           1,
                    StreamPriority: 15,
                },
            },
            // 🔹 DecoderConfigDescriptor (Tag=0x04)
            {
                Tag:  mp4.DecoderConfigDescrTag,
                Size: 15 + uint32(len(configData)),
                DecoderConfigDescriptor: &mp4.DecoderConfigDescriptor{
                    ObjectTypeIndication: 0x40,  // AAC LC
                    StreamType:           0x05,  // Audio
                    BufferSizeDB:         1536,  // типовий буфер
                    MaxBitrate:           uint32(bitrate * 1.2),
                    AvgBitrate:           uint32(bitrate),
                },
            },
            // 🔹 DecSpecificInfo (Tag=0x05) — сирі дані!
            {
                Tag:  mp4.DecSpecificInfoTag,
                Size: uint32(len(configData)),
                Data: configData,  // 🔥 AudioSpecificConfig байти
            },
            // 🔹 SLConfigDescriptor (Tag=0x06)
            {
                Tag:  mp4.SLConfigDescrTag,
                Size: 1,
                Data: []byte{0x02},  // стандартне значення
            },
        },
    }
}

// Допоміжна функція: формування AudioSpecificConfig
func buildAudioSpecificConfig(sampleRate, channels int) []byte {
    // 🔹 Спрощена реалізація — для продакшену використовуйте бібліотеку!
    // ObjectType: 2 (AAC LC) → біти 11-7: 00010
    // SamplingFrequencyIndex: див. таблицю нижче
    // ChannelConfig: 1=моно, 2=стерео, 3=3.0, 4=4.0, 5=5.0, 6=5.1
    
    freqIndex := getSamplingFrequencyIndex(sampleRate)
    channelConfig := channels  // спрощено: 2 = стерео
    
    // Формуємо 2 байти: [ObjectType:5][FreqIdx:4][ChannelCfg:4][останні біти]
    byte0 := byte((2 << 3) | (freqIndex >> 1))        // 00010xxx
    byte1 := byte((freqIndex & 1) << 7 | channelConfig << 3)  // x000cccc000
    
    return []byte{byte0, byte1}
}

func getSamplingFrequencyIndex(rate int) int {
    // Таблиця з MPEG-4 стандарту
    rates := []int{
        96000, 88200, 64000, 48000, 44100, 32000,
        24000, 22050, 16000, 12000, 11025, 8000, 7350,
    }
    for i, r := range rates {
        if rate == r {
            return i
        }
    }
    return 4  // fallback: 48000 Hz
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний парсинг `varint` | `Size` читається неправильно → зсув даних | Використовуйте бібліотечний парсер, не пишіть свій |
| Ігнорування вкладених дескрипторів | `DecSpecificInfo` не знайдено → декодер не ініціалізується | Рекурсивно парсіть `Descriptors[]` у `DecoderConfigDescriptor` |
| Неправильне кодування `AudioSpecificConfig` | AAC не декодується, артефакти звуку | Використовуйте перевірену бібліотеку (напр. `github.com/asticode/go-astits`) |
| Невірний `ObjectTypeIndication` | Плеєр відмовляє відтворювати | Завжди `0x40` для AAC LC, `0x20` для H.264 |
| Забути `opt=dynamic` логіку | Читаєте "зайві" поля → помилка парсингу | Завжди перевіряйте `IsOptFieldEnabled()` перед доступом до опціональних полів |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні fMP4 з аудіо:
    • Шукайте `esds` бокс у `stsd` → `mp4a` → `esds`
    • Перевіряйте `ObjectTypeIndication == 0x40` для AAC
    • Витягуйте `DecSpecificInfo` (Tag=0x05) для ініціалізації декодера

[ ] Для валідації аудіо-потоку:
    • Перевіряйте `StreamType == 0x05` (аудіо)
    • Логувайте `AvgBitrate` для моніторингу якості
    • Відхиляйте невідомі `ObjectTypeIndication`

[ ] При генерації нових сегментів:
    • Формуйте правильний `AudioSpecificConfig` для вашої конфігурації
    • Встановлюйте `BufferSizeDB` адекватно до бітрейту
    • Додавайте `SLConfigDescriptor` (Tag=0x06) для сумісності

[ ] Для дебагу:
    • Логуйте сирий вміст `DecSpecificInfo`: 
      log.Printf("🎵 AAC config: % x", configData)
    • Використовуйте `Stringify()` для людського виводу: 
      log.Printf("📦 %s", Stringify(esds, ctx))

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: VLC, hls.js, ExoPlayer, Safari
    • Перевірте відтворення на різних пристроях (ТВ, телефон, ПК)
```

---

## 🎯 Висновок

> **`esds` — це ключ до коректного відтворення MPEG-4 аудіо (AAC) у вашому HLS-стрімі**.  
> Він забезпечує:
> • ✅ Ініціалізацію декодера з правильними параметрами
> • ✅ Підтримку різних профілів AAC (LC, HE-AAC, HE-AAC v2)
> • ✅ Ефективне кодування через `varint` та опціональні поля
> • ✅ Сумісність зі стандартом MPEG-4 Part 1

Для вашого **CCTV HLS Processor** це означає:
- 🔊 Клієнти чують чистий звук без артефактів
- 🌐 Підтримка всіх сучасних плеєрів (Safari, Chrome, VLC)
- 📉 Економія бітрейту завдяки правильній конфігурації AAC
- 🔧 Безпечна генерація нових сегментів з валідними параметрами

Потребуєте допомоги з парсингом `AudioSpecificConfig` або з інтеграцією `esds` у ваш конвеєр обробки аудіо? Напишіть — покажу готовий код! 🚀🔊