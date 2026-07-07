# 🎵 `Opus` та `dOps`: Opus Audio Configuration у MP4

Це код з бібліотеки `go-mp4` для роботи з **аудіо-кодеком Opus** у форматі MP4/fMP4 (ISOBMFF) згідно зі специфікацією [Opus in ISOBMFF](https://opus-codec.org/docs/opus_in_isobmff.html).

Opus — це сучасний відкритий аудіокодек, що забезпечує високу якість при низькому бітрейті, ідеальний для стрімінгу, відеодзвінків та мобільних застосунків.

---

## 🎯 Коротка відповідь

> **`Opus` та `dOps` — це "паспорт" Opus-аудіо**, який каже декодеру: *"Це аудіо закодовано в Opus, ось параметри: кількість каналів, затримка, мапінг каналів, посилення"*.

---

## 🧱 Архітектура: Два типи боксів

### 🔹 `Opus` — Opus Audio Sample Entry

```go
func BoxTypeOpus() BoxType { return StrToBoxType("Opus") }

func init() {
	AddAnyTypeBoxDef(&AudioSampleEntry{}, BoxTypeOpus())
}
```

**Де зустрічається**: `moov → trak → mdia → minf → stbl → stsd → Opus`

**Призначення**: Оголошує, що ця аудіо-доріжка містить **аудіо в кодеку Opus**.

```
📦 stsd (Sample Description) для аудіо:
├── 📦 mp4a  ← AAC (найпоширеніший)
├── 📦 Opus  ← Opus ✅ (цей бокс!)
├── 📦 flac  ← FLAC
└── 📦 alac  ← Apple Lossless
```

> 🎯 `AudioSampleEntry` — базова структура для всіх аудіо-кодеків. Бібліотека сама підставить специфічні поля для Opus.

---

### 🔹 `dOps` — Opus Decoder Configuration Box ⭐ Найважливіший!

```go
func BoxTypeDOps() BoxType { return StrToBoxType("dOps") }

func init() {
	AddBoxDef(&DOps{})
}
```

**Де зустрічається**: Зазвичай всередині `Opus` боксу як вкладений бокс конфігурації.

**Призначення**: Містить **детальні параметри декодера Opus**, необхідні для ініціалізації відтворення.

---

## 🔍 Детальний розбір структури `DOps`

```go
type DOps struct {
	Box
	Version              uint8   `mp4:"0,size=8"`   // 🔹 Версія формату (завжди 0)
	OutputChannelCount   uint8   `mp4:"1,size=8"`   // 🔹 Кількість вихідних каналів (1=моно, 2=стерео...)
	PreSkip              uint16  `mp4:"2,size=16"`  // 🔹 Затримка у семплах (для синхронізації)
	InputSampleRate      uint32  `mp4:"3,size=32"`  // 🔹 Вхідна частота дискретизації (зазвичай 48000)
	OutputGain           int16   `mp4:"4,size=16"`  // 🔹 Посилення виводу у 1/256 dB
	ChannelMappingFamily uint8   `mp4:"5,size=8"`   // 🔹 Сімейство мапінгу каналів (0=стандартне, 1-255=користувацьке)
	
	// 🔹 Опціональні поля (тільки якщо ChannelMappingFamily != 0):
	StreamCount          uint8   `mp4:"6,opt=dynamic,size=8"`      // 🔹 Кількість потоків Opus
	CoupledCount         uint8   `mp4:"7,opt=dynamic,size=8"`      // 🔹 Кількість спарених каналів
	ChannelMapping       []uint8 `mp4:"8,opt=dynamic,size=8,len=dynamic"`  // 🔹 Мапа каналів
}
```

---

## 🔑 Розбір важливих полів

### 🔹 `Version` (1 байт) — версія формату

| Значення | Опис |
|----------|------|
| `0` | ✅ Поточна версія (єдина підтримувана) |
| `1-255` | Зарезервовано для майбутніх версій |

> 🎯 **Важливо**: Завжди встановлюйте `0`. Інші значення можуть не підтримуватися декодерами.

---

### 🔹 `OutputChannelCount` (1 байт) — кількість вихідних каналів

| Значення | Конфігурація | Використання |
|----------|-------------|-------------|
| `1` | 🔹 Моно | Голосові повідомлення, подкасти |
| `2` | 🔹 Стерео (L, R) | ✅ Музика, відео, стандарт для стрімінгу |
| `3-8` | Багатоканальне (5.1, 7.1) | Професійне аудіо, кіно |
| `>8` | Екзотичні конфігурації | Рідко, спеціальні застосунки |

**Для вашого CCTV**: Зазвичай `1` (моно) для голосових коментарів, або `2` (стерео) для якісного звуку.

---

### 🔹 `PreSkip` (2 байти) — затримка у семплах

```
🎯 Призначення: Кількість семплів, які потрібно пропустити на початку декодування.

🔹 Навіщо це?
• Opus має внутрішню затримку кодека (~2.5-13.5 мс)
• Для точної синхронізації аудіо/відео потрібно "відрізати" ці семпли
• Значення вказується у семплах @ 48000 Hz (незалежно від InputSampleRate!)

🔢 Приклад:
• PreSkip = 312 → пропустити 312 семпли @ 48000 Hz = 6.5 мс
• Це забезпечує ідеальну синхронізацію з відео-кадрами
```

**Для вашого CCTV**: Зазвичай `0` або значення, рекомендоване енкодером (напр. FFmpeg встановлює автоматично).

---

### 🔹 `InputSampleRate` (4 байти) — вхідна частота дискретизації

| Значення | Опис |
|----------|------|
| `0` | 🔹 Невідома/не вказана (декодер використає стандартну) |
| `8000` | Телефонна якість |
| `16000` | Широкополосна мова |
| `24000` | ✅ Оптимум для мови |
| `48000` | ✅ Стандарт для музики/відео (рекомендовано) |

> 🎯 **Важливо**: Opus внутрішньо працює @ 48000 Hz, але може декодувати в будь-яку частоту. Це поле інформативне.

**Для вашого CCTV**: Зазвичай `48000` для максимальної сумісності.

---

### 🔹 `OutputGain` (2 байти, знакове) — посилення виводу

```
📐 Формат: 16-біт знакове ціле, одиниця = 1/256 dB

🔢 Приклади:
• 0 → 0 dB (без змін)
• 256 → +1.0 dB (посилення)
• -256 → -1.0 dB (затухання)
• 1280 → +5.0 dB (сильне посилення)

🎯 Призначення: Компенсація різниці гучності між джерелами.
```

**Для вашого CCTV**: Зазвичай `0` (без змін), або невелике посилення для тихих джерел.

---

### 🔹 `ChannelMappingFamily` (1 байт) — сімейство мапінгу каналів

| Значення | Опис | Використання |
|----------|------|-------------|
| `0` | 🔹 Стандартне мапінгу (ITU-T) | ✅ Моно/стерео — більшість випадків |
| `1` | 🔹 Вальс-мапінгу (WAV-style) | Для сумісності з існуючими файлами |
| `2-255` | Користувацьке мапінгу | Професійні/екзотичні конфігурації |

**Як працює мапінгу:**
```
🔹 ChannelMappingFamily = 0 (стандартне):
• 1 канал: моно
• 2 канали: стерео (ліво, право)
• 3+ канали: за стандартом ITU-T (L, R, C, LFE, Ls, Rs...)

🔹 ChannelMappingFamily != 0 (користувацьке):
• Потрібні додаткові поля: StreamCount, CoupledCount, ChannelMapping[]
• ChannelMapping[] визначає, який вихідний канал відповідає якому потоку Opus
```

---

### 🔹 Опціональні поля (тільки якщо `ChannelMappingFamily != 0`)

#### ✅ `StreamCount` (1 байт) — кількість потоків Opus

```
🎯 Opus підтримує декілька незалежних потоків у одному файлі.
• StreamCount = 1 → один потік (найпоширеніший випадок)
• StreamCount = 2 → два потоки (напр. для 5.1: 1 потік для основних каналів, 1 для LFE)
```

#### ✅ `CoupledCount` (1 байт) — кількість спарених каналів

```
🎯 Спарені канали (coupled) кодуються разом для кращої компресії стерео-сигналу.
• 0 ≤ CoupledCount ≤ StreamCount
• Загальна кількість каналів = CoupledCount × 2 + (StreamCount - CoupledCount)

🔢 Приклад для стерео:
• StreamCount = 1, CoupledCount = 1 → 1×2 + (1-1) = 2 канали ✅
```

#### ✅ `ChannelMapping` ([]uint8) — мапа каналів

```
📐 Довжина масиву = OutputChannelCount
📐 Кожен елемент: індекс потоку/каналу для цього вихідного каналу

🔢 Приклад для 5.1 (6 каналів) з нестандартним мапінгу:
ChannelMapping = [0, 1, 2, 3, 4, 5]
• Вихідний канал 0 (L) ← потік 0, канал 0
• Вихідний канал 1 (R) ← потік 0, канал 1
• Вихідний канал 2 (C) ← потік 0, канал 2
• ...

🎯 Для стандартного мапінгу (ChannelMappingFamily=0) це поле відсутнє!
```

---

## 🔍 Динамічна логіка: `IsOptFieldEnabled` та `GetFieldLength`

```go
func (dops DOps) IsOptFieldEnabled(name string, ctx Context) bool {
	switch name {
	case "StreamCount", "CoupledCount", "ChannelMapping":
		// 🔹 Увімкнути опціональні поля, тільки якщо мапінгу нестандартне
		return dops.ChannelMappingFamily != 0
	}
	return false
}

func (ops DOps) GetFieldLength(name string, ctx Context) uint {
	switch name {
	case "ChannelMapping":
		// 🔹 Довжина масиву = кількість вихідних каналів
		return uint(ops.OutputChannelCount)
	}
	return 0
}
```

**🎯 Магія**: Бібліотека питає: "Чи читати поле `ChannelMapping`?" → Ви відповідаєте: "Тільки якщо `ChannelMappingFamily != 0`!" → Бібліотека читає точно стільки байт, скільки потрібно.

---

## 🛠️ Практичне використання у вашому HLS-процесорі

### 🔹 Приклад 1: Читання Opus-конфігурації з fMP4

```go
import "github.com/abema/go-mp4"

func extractOpusConfig(filePath string) (*OpusConfig, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	var opusConfig *OpusConfig
	
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeDOps() {
			dops := &mp4.DOps{}
			if _, err := h.ReadPayload(dops); err != nil {
				return nil, err
			}
			
			opusConfig = &OpusConfig{
				ChannelCount:   dops.OutputChannelCount,
				PreSkip:        dops.PreSkip,
				SampleRate:     dops.InputSampleRate,
				OutputGain:     float32(dops.OutputGain) / 256.0,  // конвертація у dB
				MappingFamily:  dops.ChannelMappingFamily,
			}
			
			// 🔹 Якщо нестандартне мапінгу — читаємо додаткові поля
			if dops.ChannelMappingFamily != 0 {
				opusConfig.StreamCount = dops.StreamCount
				opusConfig.CoupledCount = dops.CoupledCount
				opusConfig.ChannelMapping = dops.ChannelMapping
			}
		}
		return nil, nil
	})
	
	return opusConfig, err
}

type OpusConfig struct {
	ChannelCount    uint8
	PreSkip         uint16
	SampleRate      uint32
	OutputGain      float32  // у dB
	MappingFamily   uint8
	StreamCount     uint8    // опціонально
	CoupledCount    uint8    // опціонально
	ChannelMapping  []uint8  // опціонально
}
```

---

### 🔹 Приклад 2: Валідація Opus-потоку перед стрімінгом

```go
func validateOpusStream(dops *mp4.DOps) error {
	// 🔹 Перевірка версії
	if dops.Version != 0 {
		return fmt.Errorf("unsupported Opus version: %d", dops.Version)
	}
	
	// 🔹 Перевірка кількості каналів
	if dops.OutputChannelCount == 0 || dops.OutputChannelCount > 8 {
		return fmt.Errorf("invalid channel count: %d", dops.OutputChannelCount)
	}
	
	// 🔹 Перевірка PreSkip (розумні межі)
	if dops.PreSkip > 15360 {  // >320 мс @ 48000 Hz — підозріло багато
		log.Printf("⚠️  Large PreSkip value: %d samples (%.1f ms)", 
			dops.PreSkip, float64(dops.PreSkip)/48.0)
	}
	
	// 🔹 Перевірка мапінгу
	if dops.ChannelMappingFamily != 0 {
		// Для нестандартного мапінгу перевіряємо узгодженість полів
		expectedChannels := int(dops.CoupledCount)*2 + int(dops.StreamCount - dops.CoupledCount)
		if int(dops.OutputChannelCount) != expectedChannels {
			return fmt.Errorf("channel count mismatch: declared=%d, calculated=%d", 
				dops.OutputChannelCount, expectedChannels)
		}
		if len(dops.ChannelMapping) != int(dops.OutputChannelCount) {
			return fmt.Errorf("channel mapping length mismatch")
		}
	}
	
	// 🔹 Логування для моніторингу
	log.Printf("🎵 Opus config: %d ch, %d Hz, gain=%.1f dB, mapping=%d", 
		dops.OutputChannelCount, dops.InputSampleRate, 
		float32(dops.OutputGain)/256.0, dops.ChannelMappingFamily)
	
	return nil
}
```

---

### 🔹 Приклад 3: Генерація `dOps` для нового Opus-потоку

```go
func createDOps(channels int, sampleRate uint32, preSkip uint16) *mp4.DOps {
	// 🔹 Стандартне мапінгу (ChannelMappingFamily = 0)
	return &mp4.DOps{
		Version:              0,
		OutputChannelCount:   uint8(channels),
		PreSkip:              preSkip,
		InputSampleRate:      sampleRate,
		OutputGain:           0,  // без посилення
		ChannelMappingFamily: 0,  // стандартне мапінгу
		// 🔹 Опціональні поля відсутні, бо ChannelMappingFamily = 0
	}
}

// Використання при створенні fMP4:
func createOpusSegment(seq int, audioData []byte, channels int, sampleRate int) error {
	f, err := os.Create(fmt.Sprintf("opus_seg_%06d.m4s", seq))
	if err != nil { return err }
	defer f.Close()
	
	// 🔹 Створити AudioSampleEntry для Opus
	audioEntry := &mp4.AudioSampleEntry{
		SampleEntry: mp4.SampleEntry{
			DataReferenceIndex: 1,
		},
		ChannelCount: uint16(channels),
		SampleSize:   16,  // біти на семпл (інформативно для Opus)
		SampleRate:   uint32(sampleRate) << 16,  // fixed-point 16.16
	}
	
	dops := createDOps(channels, uint32(sampleRate), 312)  // 312 семпли = 6.5 мс
	
	// 🔹 Записати у файл (спрощено)
	mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeOpus()})
	audioEntry.Marshal(f)
	mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeDOps()})
	dops.Marshal(f)
	
	// 🔹 Записати сирі Opus-дані у mdat
	mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeMdat()})
	f.Write(audioData)
	
	return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `PreSkip` | Десинхронізація аудіо/відео на початку сегмента | Використовуйте значення, рекомендоване енкодером (напр. FFmpeg: 312 для 6.5 мс) |
| Ігнорування `ChannelMappingFamily` | Нестандартне мапінгу читається неправильно → канали переплутані | Завжди перевіряйте `ChannelMappingFamily` перед доступом до опціональних полів |
| Неправильний `OutputGain` | Звук занадто тихий або гучний | Пам'ятайте: одиниця = 1/256 dB; 0 = без змін |
| Невідповідність `StreamCount`/`CoupledCount` | Декодер не може ініціалізуватися → помилка | Для стандартного мапінгу (`ChannelMappingFamily=0`) не встановлюйте ці поля |
| Забути `len=dynamic` для `ChannelMapping` | Читаєте не ту кількість байт → зсув даних | Завжди реалізуйте `GetFieldLength()` для масивів |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні Opus-аудіо:
    • Шукайте `dOps` бокс всередині `Opus` у `stsd`
    • Перевіряйте `Version == 0` для сумісності
    • Логувайте `OutputChannelCount` та `InputSampleRate` для моніторингу

[ ] Для сумісності з вебом:
    • Моно (1 канал) або стерео (2 канали) підтримується всюди
    • `InputSampleRate = 48000` — оптимальний вибір
    • Уникайте нестандартного мапінгу (`ChannelMappingFamily != 0`) для широкого розповсюдження

[ ] При генерації нових сегментів:
    • Встановлюйте `PreSkip` відповідно до затримки кодека (зазвичай 312 для 6.5 мс)
    • Для стандартного мапінгу: `ChannelMappingFamily = 0`, опціональні поля відсутні
    • Узгоджуйте `OutputChannelCount` з реальною кількістю каналів у потоці

[ ] Для дебагу:
    • Логуйте конфігурацію: log.Printf("🎵 Opus: %d ch, %d Hz, gain=%.1f dB", ...)
    • Перевіряйте мапінгу: if mappingFamily != 0 { log.Printf("Custom mapping: %v", channelMapping) }
    • Використовуйте `Stringify()` для людського виводу

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: Chrome (WebM/Opus), Firefox, VLC
    • Перевірте синхронізацію: аудіо має починатися точно з відео
```

---

## 🎯 Інтеграція у ваш CCTV HLS Processor

```
📡 Ваш потік обробки Opus-аудіо:
1. Приймаєте Opus-потік з камери/мікрофона
   │
   ▼
2. Валідуєте параметри:
   • Кількість каналів: 1 (моно) для голосу, 2 (стерео) для якісного звуку
   • Частота: 48000 Hz для максимальної сумісності
   • PreSkip: для синхронізації з відео
   │
   ▼
3. Формуєте fMP4-сегмент:
   • Створюєте `Opus` AudioSampleEntry
   • Додаєте `dOps` з конфігурацією
   • Записуєте сирі байти Opus у `mdat`
   │
   ▼
4. Генеруєте HLS-плейлист:
   • Додаєте аудіо-доріжку з кодеком "opus"
   • Вказуєте бітрейт та канали для адаптивного стрімінгу
   │
   ▼
5. Клієнт відтворює високоякісне аудіо з низькою затримкою ✅
```

---

## ❓ Часті питання

**Q: Чому Opus, якщо є AAC?**  
A: Opus має переваги для певних сценаріїв:
- ⚡ Нижча затримка (2.5-13.5 мс vs 20-100 мс у AAC) — ідеально для live/відеодзвінків
- 🎯 Краща якість при низьких бітрейтах (<64 kbps) — економія трафіку
- 🔄 Адаптивна частота: один потік може декодуватися в 8-48 kHz
- 🆓 Відкритий стандарт без ліцензійних обмежень

**Q: Чи підтримують браузери Opus у HLS?**  
A: ✅ Chrome, Firefox, Edge — нативна підтримка через MSE/EME  
⚠️ Safari — підтримка з iOS 15+/macOS Monterey  
❌ Старі плеєри — можуть не підтримувати  
🎯 **Рекомендація**: Для широкого розповсюдження використовуйте AAC як fallback, Opus — для сучасних клієнтів.

**Q: Як конвертувати Opus → AAC на льоту?**  
```go
// 1. Прочитати Opus-сегмент
opusData, config := extractOpusData(segmentPath)

// 2. Декодувати Opus → PCM (напр. через libopus)
pcmData := decodeOpusToPCM(opusData, config)

// 3. Закодувати PCM → AAC (напр. через FFmpeg bindings)
aacData, aacConfig := encodeToAAC(pcmData, config)

// 4. Створити новий fMP4 з AAC
createAACSegment(seq, aacData, aacConfig)
```

**Q: Як перевірити, чи коректний мій Opus-потік?**  
```bash
# ffprobe покаже деталі:
ffprobe -show_streams -select_streams a -print_format json segment.m4s

# Очікуйте:
{
  "codec_name": "opus",
  "sample_rate": 48000,
  "channels": 2,
  "channel_layout": "stereo"
}

# Або спеціалізовані інструменти:
# • opusinfo (з opus-tools): opusinfo segment.m4s
# • mediainfo: mediainfo segment.m4s
```

---

## 🎯 Висновок

> **`dOps` — це ключ до коректного відтворення Opus-аудіо у вашому HLS-стрімі**.  
> Він забезпечує:
> • ✅ Ініціалізацію декодера з правильними параметрами (канали, затримка, мапінгу)
> • ✅ Підтримку як стандартного, так і користувацького мапінгу каналів
> • ✅ Точну синхронізацію аудіо/відео через `PreSkip`
> • ✅ Сумісність зі стандартом Opus in ISOBMFF

Для вашого **CCTV HLS Processor** це означає:
- 🔊 Високоякісне аудіо з низькою затримкою для live-трансляцій
- ⚡ Економія бітрейту без втрати якості (ідеально для мобільних мереж)
- 🔄 Гнучкість: легко адаптувати параметри під різні сценарії (голос/музика)
- 🌐 Підтримка сучасних браузерів та плеєрів

Потребуєте допомоги з інтеграцією Opus у ваш конвеєр або з конвертацією Opus ↔ AAC на льоту? Напишіть — покажу готовий код! 🚀🎵