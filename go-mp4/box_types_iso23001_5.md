# 🔊 `pcmC`: PCM Audio Configuration Box у MP4

Це код з бібліотеки `go-mp4` для роботи з **некомпресованим PCM-аудіо** (Pulse Code Modulation) у форматі MP4/fMP4 (ISOBMFF). PCM — це "сирий" аудіоформат без стиснення, що використовується для високоякісного запису, професійної обробки або як проміжний формат під час транскодування.

---

## 🎯 Коротка відповідь

> **`pcmC` — це "паспорт" PCM-аудіо**, який каже декодеру: *"Це аудіо у форматі PCM, ось параметри: ціле/плаваюче, 16/24/32 біти на семпл"*.

---

## 🧱 Архітектура: Три типи боксів

### 🔹 `ipcm` — Integer PCM Sample Entry

```go
func BoxTypeIpcm() BoxType { return StrToBoxType("ipcm") }

func init() {
	AddAnyTypeBoxDef(&AudioSampleEntry{}, BoxTypeIpcm())
}
```

**Де зустрічається**: `moov → trak → mdia → minf → stbl → stsd → ipcm`

**Призначення**: Оголошує, що ця аудіо-доріжка містить **цілочисельне PCM-аудіо** (напр. 16-біт, 24-біт).

```
📦 stsd (Sample Description) для аудіо:
├── 📦 mp4a  ← AAC (стиснене)
├── 📦 ipcm  ← Integer PCM ✅ (цей бокс!)
├── 📦 fpcm  ← Float PCM
└── 📦 sowt  ← Raw PCM (Little-endian, legacy)
```

> 🎯 `AudioSampleEntry` — базова структура для всіх аудіо-кодеків. Бібліотека сама підставить специфічні поля для PCM.

---

### 🔹 `fpcm` — Float PCM Sample Entry

```go
func BoxTypeFpcm() BoxType { return StrToBoxType("fpcm") }

func init() {
	AddAnyTypeBoxDef(&AudioSampleEntry{}, BoxTypeFpcm())
}
```

**Призначення**: Оголошує, що доріжка містить **аудіо з плаваючою комою** (32-біт float або 64-біт double).

**Коли використовується**:
- Професійна аудіо-обробка (DAW, mastering)
- Проміжний формат під час транскодування
- Наукові/аналітичні застосунки, де важлива точність

---

### 🔹 `pcmC` — PCM Configuration Box ⭐ Найважливіший!

```go
func BoxTypePcmC() BoxType { return StrToBoxType("pcmC") }

func init() {
	AddBoxDef(&PcmC{}, 0, 1)  // підтримуються версії 0 та 1
}

type PcmC struct {
	FullBox       `mp4:"0,extend"`
	FormatFlags   uint8 `mp4:"1,size=8"`   // 🔹 тип даних: ціле/плаваюче, endianess
	PCMSampleSize uint8 `mp4:"2,size=8"`   // 🔹 розмір семпла: 8/16/24/32/64 біти
}
```

> ⚠️ **Зверніть увагу**: У вашому коді обидва поля мають `mp4:"1,size=8"` — це, ймовірно, одруківка. Друге поле має бути `mp4:"2,size=8"`.

---

## 🔑 Розбір полів `PcmC`

### 🔹 `FormatFlags` (1 байт) — бітові прапорці формату

```
📐 Бітова структура (8 біт):
[7:4] — зарезервовано (завжди 0)
[3]   — Floating Point Flag: 0=Integer, 1=Float
[2]   — Endianness: 0=Little-endian, 1=Big-endian
[1:0] — зарезервовано (завжди 0)
```

| Значення | Опис | Приклад використання |
|----------|------|---------------------|
| `0x00` | 🔹 Integer, Little-endian | ✅ 16-біт PCM з ПК/мобільних пристроїв |
| `0x04` | 🔹 Integer, Big-endian | ✅ Legacy Mac/професійне обладнання |
| `0x08` | 🔹 Float, Little-endian | ✅ 32-біт float для обробки |
| `0x0C` | Float, Big-endian | Рідко, специфічні системи |

**Для вашого CCTV**: Зазвичай `0x00` (Integer, Little-endian) — стандарт для більшості пристроїв.

---

### 🔹 `PCMSampleSize` (1 байт) — розмір одного семпла

| Значення | Розмір | Динамічний діапазон | Використання |
|----------|--------|---------------------|-------------|
| `8` | 8 біт | ~48 dB | Рідко, низька якість |
| `16` | 🔹 16 біт | ~96 dB | ✅ CD-якість, стандарт для стрімінгу |
| `24` | 🔹 24 біт | ~144 dB | ✅ Професійний запис, студійна якість |
| `32` | 32 біт (integer) | ~192 dB | Рідко, спеціальні застосунки |
| `32` | 🔹 32 біт (float) | ~1500 dB* | ✅ Професійна обробка, HDR-аудіо |
| `64` | 64 біт (float) | практично необмежений | Наукові розрахунки |

> *🎯 Плаваюча кома має іншу логіку динамічного діапазону — важлива точність, а не "гучність".

**Для вашого CCTV**: Зазвичай `16` (16-біт) для економії бітрейту, або `24` для високоякісних трансляцій.

---

## 🔍 Як це пов'язано з `AudioSampleEntry`?

`PcmC` — це **додатковий бокс**, що вкладений у `AudioSampleEntry`:

```
📦 stsd
└── 📦 ipcm (AudioSampleEntry)
    ├── 🔹 Базові поля: ChannelCount, SampleSize, SampleRate...
    └── 🔹 Вкладений бокс: pcmC (PcmC)
        ├── FormatFlags: 0x00 (Integer, LE)
        └── PCMSampleSize: 16 (16-біт)
```

**Приклад ієрархії у файлі:**
```
moov
└── trak
    └── mdia
        └── minf
            └── stbl
                └── stsd
                    └── ipcm ← AudioSampleEntry для PCM
                        └── pcmC ← PcmC з деталями формату
```

---

## 🛠️ Практичне використання у вашому HLS-процесорі

### 🔹 Приклад 1: Читання PCM-конфігурації з fMP4

```go
import "github.com/abema/go-mp4"

func extractPCMConfig(filePath string) (*PCMConfig, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    var pcmConfig *PCMConfig
    
    _, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Шукаємо ipcm або fpcm у stsd
        if h.BoxInfo.Type == mp4.BoxTypeIpcm() || h.BoxInfo.Type == mp4.BoxTypeFpcm() {
            // 🔹 Шукаємо вкладений pcmC бокс
            mp4.ReadBoxStructure(h.Reader(), func(inner *mp4.ReadHandle) (interface{}, error) {
                if inner.BoxInfo.Type == mp4.BoxTypePcmC() {
                    pcmC := &mp4.PcmC{}
                    if _, err := inner.ReadPayload(pcmC); err != nil {
                        return nil, err
                    }
                    
                    pcmConfig = &PCMConfig{
                        IsFloat:      pcmC.FormatFlags & 0x08 != 0,
                        IsBigEndian:  pcmC.FormatFlags & 0x04 != 0,
                        SampleSize:   pcmC.PCMSampleSize,
                    }
                }
                return nil, nil
            })
        }
        return nil, nil
    })
    
    return pcmConfig, err
}

type PCMConfig struct {
    IsFloat      bool  // true = float PCM, false = integer PCM
    IsBigEndian  bool  // true = big-endian, false = little-endian
    SampleSize   uint8 // 8, 16, 24, 32, 64 біт
}
```

---

### 🔹 Приклад 2: Валідація PCM-потоку перед стрімінгом

```go
func validatePCMStream(pcmConfig *PCMConfig) error {
    // 🔹 Перевірка розміру семпла (підтримка плеєрами)
    if pcmConfig.SampleSize != 16 && pcmConfig.SampleSize != 24 {
        return fmt.Errorf("unsupported PCM sample size: %d bits", pcmConfig.SampleSize)
    }
    
    // 🔹 Перевірка endianess (більшість плеєрів очікують little-endian)
    if pcmConfig.IsBigEndian {
        log.Printf("⚠️  Big-endian PCM may not be supported by all browsers")
    }
    
    // 🔹 Float PCM потребує спеціальної підтримки
    if pcmConfig.IsFloat {
        if pcmConfig.SampleSize != 32 {
            return fmt.Errorf("float PCM must be 32-bit, got %d", pcmConfig.SampleSize)
        }
        log.Printf("ℹ️  Float PCM detected — ensure client supports Web Audio API")
    }
    
    return nil
}
```

---

### 🔹 Приклад 3: Генерація `pcmC` для нового PCM-потоку

```go
func createPcmC(isFloat bool, isBigEndian bool, sampleSize uint8) *mp4.PcmC {
    // 🔹 Формуємо FormatFlags
    var formatFlags uint8 = 0
    if isFloat {
        formatFlags |= 0x08  // біт 3 = Float
    }
    if isBigEndian {
        formatFlags |= 0x04  // біт 2 = Big-endian
    }
    
    return &mp4.PcmC{
        FullBox:       mp4.FullBox{Version: 0, Flags: [3]byte{0, 0, 0}},
        FormatFlags:   formatFlags,
        PCMSampleSize: sampleSize,  // 16, 24, або 32
    }
}

// Використання при створенні fMP4:
func createPCMSegment(seq int, audioData []byte, sampleRate int, channels int) error {
    f, err := os.Create(fmt.Sprintf("pcm_seg_%06d.m4s", seq))
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 Створити AudioSampleEntry з вкладеним pcmC
    audioEntry := &mp4.AudioSampleEntry{
        SampleEntry: mp4.SampleEntry{
            DataReferenceIndex: 1,
        },
        ChannelCount: uint16(channels),
        SampleSize:   16,  // біти на семпл
        SampleRate:   uint32(sampleRate) << 16,  // fixed-point 16.16
    }
    
    pcmC := createPcmC(false, false, 16)  // Integer, LE, 16-біт
    
    // 🔹 Записати у файл (спрощено)
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeIpcm()})
    audioEntry.Marshal(f)
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypePcmC()})
    pcmC.Marshal(f)
    
    // 🔹 Записати сирі PCM-дані у mdat
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeMdat()})
    f.Write(audioData)  // сирі байти PCM
    
    return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `FormatFlags` | Плеєр інтерпретує integer як float → шум/артефакти | Завжди встановлюйте біт 3 = 0 для integer PCM |
| Неправильна `PCMSampleSize` | Зсув даних на 1-2 байти → десинхронізація | Перевіряйте, що `SampleSize` у `AudioSampleEntry` співпадає з `PCMSampleSize` |
| Ігнорування endianess | Big-endian дані читаються як little-endian → "кракозябри" звуку | Для вебу завжди використовуйте little-endian (`FormatFlags & 0x04 = 0`) |
| Float PCM без підтримки | Старі плеєри не відтворюють 32-біт float | Використовуйте `IsFloat` для перевірки сумісності перед відправкою |
| Одруківка в тегах `mp4:"..."` | Обидва поля читають один байт → дані зсуваються | Перевірте: `FormatFlags` має `mp4:"1,size=8"`, `PCMSampleSize` має `mp4:"2,size=8"` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні PCM-аудіо:
    • Шукайте `ipcm`/`fpcm` у `stsd` + вкладений `pcmC`
    • Перевіряйте `PCMSampleSize`: 16 або 24 для максимальної сумісності
    • Логувайте `FormatFlags` для дебагу endianess/float

[ ] Для сумісності з вебом:
    • Integer PCM (`FormatFlags & 0x08 = 0`) підтримується всюди
    • Little-endian (`FormatFlags & 0x04 = 0`) очікується більшістю плеєрів
    • Уникайте float PCM для широкого розповсюдження

[ ] При генерації нових сегментів:
    • Узгоджуйте `SampleSize` у `AudioSampleEntry` з `PCMSampleSize` у `pcmC`
    • Встановлюйте `SampleRate` у fixed-point 16.16 форматі: `rate << 16`
    • Додавайте `pcmC` як вкладений бокс у `ipcm`/`fpcm`

[ ] Для дебагу:
    • Логуйте сирий вміст: log.Printf("🔊 PCM: flags=0x%02x, size=%d", flags, size)
    • Перевіряйте відповідність: if audioEntry.SampleSize != pcmC.PCMSampleSize { ... }
    • Використовуйте `Stringify()` для людського виводу

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: VLC, hls.js, Safari, ExoPlayer
    • Перевірте відтворення з різними конфігураціями: 16-біт LE, 24-біт LE, float
```

---

## 🎯 Інтеграція у ваш CCTV HLS Processor

```
📡 Ваш потік обробки PCM-аудіо:
1. Приймаєте сирі PCM-дані з камери/мікрофона
   │
   ▼
2. Валідуєте параметри:
   • Розмір семпла: 16/24 біти?
   • Формат: integer чи float?
   • Endianess: little чи big?
   │
   ▼
3. Формуєте fMP4-сегмент:
   • Створюєте `ipcm` AudioSampleEntry
   • Додаєте `pcmC` з конфігурацією
   • Записуєте сирі байти у `mdat`
   │
   ▼
4. Генеруєте HLS-плейлист:
   • Додаєте аудіо-доріжку з кодеком "pcm"
   • Вказуєте бітрейт: sampleRate × channels × sampleSize/8
   │
   ▼
5. Клієнт відтворює високоякісне аудио без артефактів ✅
```

---

## ❓ Часті питання

**Q: Чому PCM, якщо є AAC/MP3?**  
A: PCM не має стиснення → нульова затримка кодування, ідеальна якість. Використовується для:
- Професійних трансляцій (новини, спорт)
- Проміжної обробки перед транскодуванням
- Систем, де важлива точність (медичні, наукові)

**Q: Чи підтримують браузери PCM у HLS?**  
A: ✅ Safari (iOS/macOS) — повна підтримка  
⚠️ Chrome/Firefox — підтримка через Web Audio API, але не всі версії  
❌ Старі плеєри — можуть не підтримувати взагалі  
🎯 **Рекомендація**: Для широкого розповсюдження використовуйте AAC, PCM залиште для професійних сценаріїв.

**Q: Як конвертувати PCM → AAC на льоту?**  
```go
// 1. Прочитати PCM-сегмент
pcmData, config := extractPCMData(segmentPath)

// 2. Закодувати в AAC (напр. через FFmpeg bindings)
aacData, aacConfig := encodeToAAC(pcmData, config)

// 3. Створити новий fMP4 з AAC
createAACSegment(seq, aacData, aacConfig)
```

**Q: Як перевірити, чи коректний мій PCM-потік?**  
```bash
# ffprobe покаже деталі:
ffprobe -show_streams -select_streams a -print_format json segment.m4s

# Очікуйте:
{
  "codec_name": "pcm_s16le",  // або pcm_f32le, pcm_s24be...
  "bits_per_sample": 16,
  "sample_rate": 48000,
  "channels": 2
}
```

---

## 🎯 Висновок

> **`pcmC` — це ключ до коректного відтворення некомпресованого аудіо у вашому HLS-стрімі**.  
> Він забезпечує:
> • ✅ Ініціалізацію декодера з правильними параметрами (бітність, endianess)
> • ✅ Підтримку як integer, так і float PCM форматів
> • ✅ Сумісність зі стандартом ISO/IEC 14496-3 та професійним обладнанням

Для вашого **CCTV HLS Processor** це означає:
- 🔊 Високоякісне аудіо без артефактів стиснення
- ⚡ Нульова затримка кодування для live-трансляцій
- 🔧 Гнучкість: легко конвертувати PCM → AAC для різних клієнтів
- 🎚️ Професійна обробка: збереження повного динамічного діапазону

Потребуєте допомоги з інтеграцією PCM у ваш конвеєр або з конвертацією PCM → AAC на льоту? Напишіть — покажу готовий код! 🚀🔊