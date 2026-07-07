# 🎬 `vp08`/`vp09` та `vpcC`: VP8/VP9 Video Configuration у MP4

Це код з бібліотеки `go-mp4` для роботи з **відео-кодеками VP8/VP9** у форматі MP4/fMP4 (ISOBMFF) згідно зі специфікацією [VP9 in ISOBMFF](https://www.webmproject.org/vp9/mp4/).

VP8/VP9 — це відкриті відеокодеки від Google, що забезпечують високу якість при низькому бітрейті, ідеальні для веб-стрімінгу, YouTube та мобільних застосунків.

---

## 🎯 Коротка відповідь

> **`vp08`/`vp09` та `vpcC` — це "паспорт" VP8/VP9-відео**, який каже декодеру: *"Це відео закодовано в VP9, ось параметри: профіль, рівень, бітова глибина, кольоровий простір, ініціалізаційні дані"*.

---

## 🧱 Архітектура: Три типи боксів

### 🔹 `vp08` — VP8 Video Sample Entry

```go
func BoxTypeVp08() BoxType { return StrToBoxType("vp08") }

func init() {
	AddAnyTypeBoxDef(&VisualSampleEntry{}, BoxTypeVp08())
}
```

**Де зустрічається**: `moov → trak → mdia → minf → stbl → stsd → vp08`

**Призначення**: Оголошує, що ця відео-доріжка містить **відео в кодеку VP8**.

```
📦 stsd (Sample Description) для відео:
├── 📦 avc1  ← H.264
├── 📦 hvc1  ← HEVC/H.265
├── 📦 vp08  ← VP8 ✅ (цей бокс!)
├── 📦 vp09  ← VP9 ✅
├── 📦 av01  ← AV1
└── 📦 mp4v  ← MPEG-4 Visual
```

> 🎯 `VisualSampleEntry` — базова структура для всіх відео-кодеків. Бібліотека сама підставить специфічні поля для VP8/VP9.

---

### 🔹 `vp09` — VP9 Video Sample Entry

```go
func BoxTypeVp09() BoxType { return StrToBoxType("vp09") }

func init() {
	AddAnyTypeBoxDef(&VisualSampleEntry{}, BoxTypeVp09())
}
```

**Призначення**: Оголошує, що доріжка містить **відео в кодеку VP9** (новіший, ефективніший за VP8).

**Коли використовується**:
- ✅ YouTube, WebM, Chrome
- ✅ Веб-стрімінг з підтримкою VP9
- ✅ Економія бітрейту на 30-50% порівняно з H.264

---

### 🔹 `vpcC` — VP Codec Configuration Box ⭐ Найважливіший!

```go
func BoxTypeVpcC() BoxType { return StrToBoxType("vpcC") }

func init() {
	AddBoxDef(&VpcC{})
}
```

**Де зустрічається**: Зазвичай всередині `vp08`/`vp09` боксу як вкладений бокс конфігурації.

**Призначення**: Містить **детальні параметри декодера VP8/VP9**, необхідні для ініціалізації відтворення.

---

## 🔍 Детальний розбір структури `VpcC`

```go
type VpcC struct {
	FullBox                     `mp4:"0,extend"`  // 🔹 version + flags
	
	// 🔹 Основні параметри кодека (10 байт):
	Profile                     uint8   `mp4:"1,size=8"`   // 🔹 Профіль: 0=0, 1=1, 2=2, 3=3
	Level                       uint8   `mp4:"2,size=8"`   // 🔹 Рівень: 0-15 (10=4.0, 11=4.1...)
	BitDepth                    uint8   `mp4:"3,size=4"`   // 🔹 Бітова глибина: 8, 10, 12 біт
	ChromaSubsampling           uint8   `mp4:"4,size=3"`   // 🔹 Підвибірка кольору: 0=4:2:0, 1=4:2:2, 2=4:4:4
	VideoFullRangeFlag          uint8   `mp4:"5,size=1"`   // 🔹 Повний діапазон: 0=limited, 1=full
	
	// 🔹 Кольоровий простір (3 байти):
	ColourPrimaries             uint8   `mp4:"6,size=8"`   // 🔹 Примарії: 1=BT.709, 9=BT.2020...
	TransferCharacteristics     uint8   `mp4:"7,size=8"`   // 🔹 Transfer: 1=BT.709, 16=PQ, 18=HLG...
	MatrixCoefficients          uint8   `mp4:"8,size=8"`   // 🔹 Matrix: 1=BT.709, 9=BT.2020...
	
	// 🔹 Ініціалізаційні дані кодека:
	CodecInitializationDataSize uint16  `mp4:"9,size=16"`  // 🔹 Розмір даних ініціалізації
	CodecInitializationData     []uint8 `mp4:"10,size=8,len=dynamic"`  // 🔹 Сирі байти (Sequence Header OBU)
}
```

---

## 🔑 Розбір важливих полів

### 🔹 `Profile` (1 байт) — профіль кодека

| Значення | Профіль | Опис | Використання |
|----------|---------|------|-------------|
| `0` | 🔹 Profile 0 | 8-біт, 4:2:0 | ✅ Найпоширеніший для вебу |
| `1` | 🔹 Profile 1 | 8-біт, 4:2:0/4:2:2/4:4:4 | Професійне відео |
| `2` | 🔹 Profile 2 | 10/12-біт, 4:2:0 | ✅ HDR, висока якість |
| `3` | 🔹 Profile 3 | 10/12-біт, всі формати | Професійне/кіно |

**Для вашого CCTV**: Зазвичай `0` (Profile 0) для сумісності, або `2` для HDR-камер.

---

### 🔹 `Level` (1 байт) — рівень кодека

| Значення | Рівень | Макс. роздільність | Макс. бітрейт |
|----------|--------|-------------------|---------------|
| `0-9` | 1.0-1.9 | 480p | 1-5 Mbps |
| `10-14` | 🔹 4.0-4.4 | 1080p | 20-50 Mbps |
| `15` | 🔹 5.0 | 4K | 50+ Mbps |

**Для вашого CCTV**: Зазвичай `10-12` (4.0-4.2) для 1080p стріму.

---

### 🔹 `BitDepth` (4 біти) — бітова глибина кольору

| Значення | Біти | Динамічний діапазон | Використання |
|----------|------|---------------------|-------------|
| `8` | 🔹 8-біт | ~256 рівнів/канал | ✅ Стандарт для вебу |
| `10` | 🔹 10-біт | ~1024 рівнів/канал | ✅ HDR, професійне |
| `12` | 12-біт | ~4096 рівнів/канал | Кіно, архівування |

**Для вашого CCTV**: Зазвичай `8` для економії бітрейту, або `10` для HDR-камер.

---

### 🔹 `ChromaSubsampling` (3 біти) — підвибірка кольору

| Значення | Формат | Опис | Використання |
|----------|--------|------|-------------|
| `0` | 🔹 4:2:0 | Гориз.+верт. підвибірка | ✅ Стандарт для стрімінгу |
| `1` | 🔹 4:2:2 | Тільки горизонтальна підвибірка | Професійне відео |
| `2` | 🔹 4:4:4 | Без підвибірки | Графіка, скрінкасти |

**Для вашого CCTV**: Зазвичай `0` (4:2:0) — оптимальний баланс якості/бітрейту.

---

### 🔹 `VideoFullRangeFlag` (1 біт) — діапазон яскравості

| Значення | Діапазон | Опис |
|----------|----------|------|
| `0` | 🔹 Limited (16-235) | Стандарт для ТВ/стрімінгу |
| `1` | 🔹 Full (0-255) | Повний діапазон (ПК/ігри) |

> 🎯 **Важливо**: Неправильне значення → кольори виглядають "вимилими" або "пересиченими".

---

### 🔹 Кольоровий простір: `ColourPrimaries` / `Transfer` / `Matrix`

Ці три поля разом описують **колориметрію відео**:

#### `ColourPrimaries` (примарії кольору)
| Значення | Стандарт | Опис |
|----------|----------|------|
| `1` | 🔹 BT.709 | HD телебачення (стандарт) |
| `9` | 🔹 BT.2020 | UHD/HDR телебачення |
| `2` | BT.470 | Старий PAL/NTSC |

#### `TransferCharacteristics` (функція передачі)
| Значення | Стандарт | Опис |
|----------|----------|------|
| `1` | 🔹 BT.709 | Стандартна гамма |
| `16` | 🔹 SMPTE ST 2084 (PQ) | HDR10, Dolby Vision |
| `18` | 🔹 HLG | Hybrid Log-Gamma (HDR для ТВ) |

#### `MatrixCoefficients` (матриця конверсії)
| Значення | Стандарт | Опис |
|----------|----------|------|
| `1` | 🔹 BT.709 | HD телебачення |
| `9` | 🔹 BT.2020 | UHD/HDR телебачення |

**Для вашого CCTV**: Зазвичай всі три = `1` (BT.709) для стандартного HD-відео.

---

### 🔹 `CodecInitializationData` — ініціалізаційні дані кодека 🔥

```go
CodecInitializationDataSize uint16  `mp4:"9,size=16"`
CodecInitializationData     []uint8 `mp4:"10,size=8,len=dynamic"`
```

**Що це?** Сирі байти **Sequence Header** (для VP9) або **Frame Header** (для VP8), необхідні для ініціалізації декодера.

**Приклад для VP9**:
```
📦 Sequence Header OBU (спрощено):
[0x90][0x00][0x0C]...  ← маркери, профіль, рівень, розмір кадру...
```

> 🎯 **Це ключ до ініціалізації декодера!** Без цих байтів плеєр не знатиме, як декодувати потік.

---

### 🔹 `GetFieldLength` — динамічна довжина поля

```go
func (vpcc VpcC) GetFieldLength(name string, ctx Context) uint {
	switch name {
	case "CodecInitializationData":
		// 🔹 Довжина даних = значення CodecInitializationDataSize
		return uint(vpcc.CodecInitializationDataSize)
	}
	return 0
}
```

**🎯 Магія**: Бібліотека питає: "Яка довжина у `CodecInitializationData`?" → Ви відповідаєте: "Стільки, скільки в `CodecInitializationDataSize`!" → Бібліотека читає точно стільки байт.

---

## 🛠️ Практичне використання у вашому HLS-процесорі

### 🔹 Приклад 1: Читання VP9-конфігурації з fMP4

```go
import "github.com/abema/go-mp4"

func extractVP9Config(filePath string) (*VP9Config, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	var vp9Config *VP9Config
	
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeVpcC() {
			vpcc := &mp4.VpcC{}
			if _, err := h.ReadPayload(vpcc); err != nil {
				return nil, err
			}
			
			vp9Config = &VP9Config{
				Profile:          vpcc.Profile,
				Level:            vpcc.Level,
				BitDepth:         vpcc.BitDepth,
				ChromaSubsampling: vpcc.ChromaSubsampling,
				FullRange:        vpcc.VideoFullRangeFlag == 1,
				ColourPrimaries:  vpcc.ColourPrimaries,
				Transfer:         vpcc.TransferCharacteristics,
				Matrix:           vpcc.MatrixCoefficients,
				InitData:         vpcc.CodecInitializationData,
			}
		}
		return nil, nil
	})
	
	return vp9Config, err
}

type VP9Config struct {
	Profile           uint8
	Level             uint8
	BitDepth          uint8
	ChromaSubsampling uint8
	FullRange         bool
	ColourPrimaries   uint8
	Transfer          uint8
	Matrix            uint8
	InitData          []byte  // 🔥 Sequence Header байти
}
```

---

### 🔹 Приклад 2: Валідація VP9-потоку перед стрімінгом

```go
func validateVP9Stream(vpcc *mp4.VpcC) error {
	// 🔹 Перевірка профілю (для сумісності з вебом)
	if vpcc.Profile > 1 {
		log.Printf("⚠️  VP9 Profile %d may have limited browser support", vpcc.Profile)
	}
	
	// 🔹 Перевірка рівня (для 1080p)
	if vpcc.Level > 14 {  // >4.4
		log.Printf("⚠️  VP9 Level %d may cause buffering on slow connections", vpcc.Level)
	}
	
	// 🔹 Перевірка бітової глибини
	if vpcc.BitDepth == 10 || vpcc.BitDepth == 12 {
		log.Printf("ℹ️  HDR content detected (%d-bit)", vpcc.BitDepth)
	}
	
	// 🔹 Перевірка кольорового простору
	if vpcc.ColourPrimaries == 9 && vpcc.Transfer == 16 {
		log.Printf("🎨 HDR10 detected (BT.2020 + PQ)")
	}
	
	// 🔹 Перевірка ініціалізаційних даних
	if len(vpcc.CodecInitializationData) == 0 {
		return fmt.Errorf("missing CodecInitializationData — decoder cannot initialize")
	}
	
	// 🔹 Перевірка Sequence Header для VP9 (перші байти мають певну структуру)
	if len(vpcc.CodecInitializationData) >= 3 {
		header := vpcc.CodecInitializationData[0:3]
		// VP9 Sequence Header починається з певних маркерів
		// (детальна валідація залежить від специфікації)
	}
	
	return nil
}
```

---

### 🔹 Приклад 3: Генерація `vpcC` для нового VP9-потоку

```go
func createVpcC(profile, level, bitDepth uint8, chromaSubsampling uint8, 
                initHeader []byte) *mp4.VpcC {
	return &mp4.VpcC{
		FullBox:                     mp4.FullBox{Version: 0, Flags: [3]byte{0, 0, 0}},
		Profile:                     profile,           // 0=Profile 0
		Level:                       level,             // 10=Level 4.0
		BitDepth:                    bitDepth,          // 8 або 10
		ChromaSubsampling:           chromaSubsampling, // 0=4:2:0
		VideoFullRangeFlag:          0,                 // Limited range
		ColourPrimaries:             1,                 // BT.709
		TransferCharacteristics:     1,                 // BT.709
		MatrixCoefficients:          1,                 // BT.709
		CodecInitializationDataSize: uint16(len(initHeader)),
		CodecInitializationData:     initHeader,        // 🔥 Sequence Header байти
	}
}

// Використання при створенні fMP4:
func createVP9Segment(seq int, videoData []byte, initHeader []byte) error {
	f, err := os.Create(fmt.Sprintf("vp9_seg_%06d.m4s", seq))
	if err != nil { return err }
	defer f.Close()
	
	// 🔹 Створити VisualSampleEntry для VP9
	videoEntry := &mp4.VisualSampleEntry{
		SampleEntry: mp4.SampleEntry{
			DataReferenceIndex: 1,
		},
		Width:          1920,
		Height:         1080,
		Horizresolution: 0x00480000,  // 72 dpi у fixed-point 16.16
		Vertresolution:  0x00480000,
		FrameCount:     1,
		Depth:          0x0018,  // 24-біт колір
	}
	
	vpcc := createVpcC(0, 10, 8, 0, initHeader)  // Profile 0, Level 4.0, 8-bit, 4:2:0
	
	// 🔹 Записати у файл (спрощено)
	mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeVp09()})
	videoEntry.Marshal(f)
	mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeVpcC()})
	vpcc.Marshal(f)
	
	// 🔹 Записати сирі VP9-дані у mdat
	mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeMdat()})
	f.Write(videoData)
	
	return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `Profile` | Плеєр не підтримує профіль → помилка декодування | Використовуйте `0` (Profile 0) для максимальної сумісності |
| Неправильний `Level` | Високий бітрейт → буферизація на слабких пристроях | Для 1080p використовуйте `10-12` (4.0-4.2) |
| Невідповідність `BitDepth` та `ChromaSubsampling` | Кольори виглядають неправильно | Для 10-біт використовуйте 4:2:0 або 4:2:2, не 4:4:4 без потреби |
| Неправильний `VideoFullRangeFlag` | Кольори "вимилені" або "пересичені" | Для вебу використовуйте `0` (limited range) |
| Пустий `CodecInitializationData` | Декодер не ініціалізується → чорний екран | Завжди передавайте валідний Sequence Header з енкодера |
| Забути `len=dynamic` для `CodecInitializationData` | Читаєте не ту кількість байт → зсув даних | Завжди реалізуйте `GetFieldLength()` для масивів |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні VP9-відео:
    • Шукайте `vpcC` бокс всередині `vp09` у `stsd`
    • Перевіряйте `Profile <= 1` для сумісності з вебом
    • Логувайте `BitDepth` та `TransferCharacteristics` для HDR-детекції

[ ] Для сумісності з вебом:
    • Profile 0 (`0`) + 8-біт + 4:2:0 підтримується всюди
    • Рівень 4.0-4.2 (`10-12`) — оптимальний для 1080p
    • BT.709 колірний простір (всі три поля = `1`) — стандарт для вебу

[ ] При генерації нових сегментів:
    • Отримуйте `CodecInitializationData` з енкодера (не генеруйте вручну!)
    • Встановлюйте `CodecInitializationDataSize = len(CodecInitializationData)`
    • Узгоджуйте `BitDepth` з реальною глибиною у відео-потоці

[ ] Для дебагу:
    • Логуйте конфігурацію: log.Printf("🎬 VP9: Profile=%d, Level=%d, %d-bit", ...)
    • Перевіряйте ініціалізаційні дані: log.Printf("🔥 InitData: % x", initData[:16])
    • Використовуйте `Stringify()` для людського виводу

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: Chrome (VP9), Firefox, VLC
    • Перевірте відтворення з різними профілями: Profile 0, Profile 2 (HDR)
```

---

## 🎯 Інтеграція у ваш CCTV HLS Processor

```
📡 Ваш потік обробки VP9-відео:
1. Приймаєте VP9-потік з камери/енкодера
   │
   ▼
2. Валідуєте параметри:
   • Профіль: 0 для вебу, 2 для HDR
   • Рівень: 10-12 для 1080p, 15 для 4K
   • Колір: 8-біт/4:2:0 для економії, 10-біт для якості
   │
   ▼
3. Формуєте fMP4-сегмент:
   • Створюєте `vp09` VisualSampleEntry
   • Додаєте `vpcC` з конфігурацією
   • Записуєте сирі байти VP9 у `mdat`
   │
   ▼
4. Генеруєте HLS-плейлист:
   • Додаєте відео-доріжку з кодеком "vp09.0.XX.XX"
   • Вказуєте роздільність, бітрейт, HDR-прапорці
   │
   ▼
5. Клієнт відтворює високоякісне відео з економією бітрейту ✅
```

---

## ❓ Часті питання

**Q: Чому VP9, якщо є H.264?**  
A: VP9 має переваги для певних сценаріїв:
- 📉 На 30-50% менший бітрейт при тій самій якості
- 🆓 Відкритий стандарт без ліцензійних відрахувань
- 🌐 Нативна підтримка у Chrome, Firefox, Edge
- 🎨 Краща підтримка HDR (10-біт, BT.2020, PQ/HLG)

**Q: Чи підтримують браузери VP9 у HLS?**  
A: ✅ Chrome, Firefox, Edge — нативна підтримка через MSE  
⚠️ Safari — підтримка з macOS Big Sur/iOS 14+  
❌ Старі плеєри — можуть не підтримувати  
🎯 **Рекомендація**: Використовуйте адаптивний стрімінг: VP9 для сучасних клієнтів, H.264 як fallback.

**Q: Як отримати `CodecInitializationData`?**  
```go
// 🔹 З FFmpeg (через libvpx):
// 1. Енкодуєте відео з прапорцем -vp9params
// 2. Витягуєте Sequence Header з першого ключового кадру
// 3. Передаєте у vpcC.CodecInitializationData

// 🔹 Альтернатива: використати готову бібліотеку
// • github.com/klauspost/vp9
// • github.com/webmproject/libvpx (C bindings)
```

**Q: Як перевірити, чи коректний мій VP9-потік?**  
```bash
# ffprobe покаже деталі:
ffprobe -show_streams -select_streams v -print_format json segment.m4s

# Очікуйте:
{
  "codec_name": "vp9",
  "profile": "Profile 0",
  "width": 1920,
  "height": 1080,
  "color_space": "bt709",
  "color_transfer": "bt709",
  "color_primaries": "bt709"
}

# Або спеціалізовані інструменти:
# • mediainfo: mediainfo segment.m4s
# • vp9dec (з libvpx): для детальної валідації
```

---

## 🎯 Висновок

> **`vpcC` — це ключ до коректного відтворення VP8/VP9-відео у вашому HLS-стрімі**.  
> Він забезпечує:
> • ✅ Ініціалізацію декодера з правильними параметрами (профіль, рівень, колір)
> • ✅ Підтримку як SDR, так і HDR контенту (8/10-біт, BT.709/BT.2020)
> • ✅ Точну синхронізацію через ініціалізаційні дані
> • ✅ Сумісність зі стандартом VP9 in ISOBMFF

Для вашого **CCTV HLS Processor** це означає:
- 🎥 Високоякісне відео з економією бітрейту на 30-50% порівняно з H.264
- 🌐 Підтримка сучасних браузерів (Chrome, Firefox, Edge)
- 🎨 Гнучкість: від SD до HDR 4K з однією конфігурацією
- 🔧 Безпечна генерація нових сегментів з валідними параметрами

Потребуєте допомоги з інтеграцією VP9 у ваш конвеєр або з валідацією `CodecInitializationData`? Напишіть — покажу готовий код! 🚀🎬