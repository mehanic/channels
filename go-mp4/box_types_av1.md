# 🎬 `Av1C`: AV1 Codec Configuration Box у MP4

Це код для роботи з **AV1 відео-кодеком** у форматі MP4/fMP4 (ISOBMFF). AV1 — це сучасний відкритий кодек від AOMedia, який забезпечує кращу компресію за H.264/HEVC.

---

## 🎯 Коротка відповідь

> **`Av1C` — це "паспорт" AV1-відео**, який каже плеєру: *"Це відео закодовано в AV1, ось параметри для декодування: профіль, рівень, кольорова підвибірка, конфігураційні дані"*.

---

## 🧱 Архітектура: Два типи боксів

### 🔹 `av01` — AV1 Sample Entry (в `stsd`)

```go
func BoxTypeAv01() BoxType { return StrToBoxType("av01") }

func init() {
	AddAnyTypeBoxDef(&VisualSampleEntry{}, BoxTypeAv01())
}
```

**Де зустрічається**: `moov → trak → mdia → minf → stbl → stsd → av01`

**Призначення**: Оголошує, що ця доріжка містить **відео в кодеку AV1**.

```
📦 stsd (Sample Description)
├── 📦 avc1  ← H.264
├── 📦 hvc1  ← HEVC/H.265
├── 📦 av01  ← AV1 ✅ (цей бокс!)
└── 📦 mp4a  ← AAC Audio
```

> 🎯 `VisualSampleEntry` — базова структура для всіх відео-кодеків. Бібліотека сама підставить специфічні поля для AV1.

---

### 🔹 `av1C` — AV1 Codec Configuration Box

```go
func BoxTypeAv1C() BoxType { return StrToBoxType("av1C") }

func init() {
	AddBoxDef(&Av1C{})  // реєструємо структуру для парсингу
}
```

**Де зустрічається**: Зазвичай всередині `av01` боксу або як окрема конфігурація.

**Призначення**: Містить **детальні параметри кодека**, необхідні для ініціалізації декодера.

---

## 🔍 Детальний розбір структури `Av1C`

```go
type Av1C struct {
	Box  // ← базовий тип для будь-якого боксу
	
	// 🔹 Заголовок: 1 байт (8 біт)
	Marker                           uint8   `mp4:"0,size=1,const=1"`   // біт 7: завжди 1
	Version                          uint8   `mp4:"1,size=7,const=1"`   // біти 6-0: версія (зараз 1)
	
	// 🔹 Профіль та рівень: 1 байт (8 біт)
	SeqProfile                       uint8   `mp4:"2,size=3"`           // біти 7-5: профіль (0=Main, 1=High, 2=Professional)
	SeqLevelIdx0                     uint8   `mp4:"3,size=5"`           // біти 4-0: рівень кодека (0-31)
	
	// 🔹 Прапорці якості: 1 байт (8 біт)
	SeqTier0                         uint8   `mp4:"4,size=1"`           // біт 7: tier (0=Main, 1=High)
	HighBitdepth                     uint8   `mp4:"5,size=1"`           // біт 6: 10/12-бітний колір?
	TwelveBit                        uint8   `mp4:"6,size=1"`           // біт 5: 12-біт (якщо HighBitdepth=1)?
	Monochrome                       uint8   `mp4:"7,size=1"`           // біт 4: чорно-біле відео?
	ChromaSubsamplingX               uint8   `mp4:"8,size=1"`           // біт 3: горизонтальна підвибірка?
	ChromaSubsamplingY               uint8   `mp4:"9,size=1"`           // біт 2: вертикальна підвибірка?
	ChromaSamplePosition             uint8   `mp4:"10,size=2"`          // біти 1-0: позиція семплінгу (0-3)
	
	// 🔹 Зарезервовано: 3 біти
	Reserved                         uint8   `mp4:"11,size=3,const=0"`  // завжди 0
	
	// 🔹 Затримка презентації: 5 біт
	InitialPresentationDelayPresent  uint8   `mp4:"12,size=1"`          // біт 4: чи є поле затримки?
	InitialPresentationDelayMinusOne uint8   `mp4:"13,size=4"`          // біти 3-0: затримка-1 (0-15)
	
	// 🔹 Конфігураційні дані: змінна довжина
	ConfigOBUs                       []uint8 `mp4:"14,size=8"`          // сирі OBU (Open Bitstream Units)
}
```

---

## 📐 Візуалізація бітового формату

```
📦 Av1C бокс (мінімум 4 байти + ConfigOBUs)
┌─────────────────────────────────┐
│ Байт 0: [Marker:1][Version:7]   │
│         [1][000 0001] = 0x81    │ ← завжди 0x81 для версії 1
├─────────────────────────────────┤
│ Байт 1: [Profile:3][Level:5]    │
│         [000][1 0101] = 0x15    │ ← Profile=0 (Main), Level=21
├─────────────────────────────────┤
│ Байт 2: [Tier:1][HDR:1][12b:1]  │
│         [Mono:1][SubX:1][SubY:1]│
│         [ChrPos:2]              │
│         [0][0][0][0][0][0][01]  │ ← ChromaSamplePosition=1
├─────────────────────────────────┤
│ Байт 3: [Reserved:3][Delay?:1]  │
│         [Delay-1:4]             │
│         [000][1][0101] = 0x15   │ ← DelayPresent=1, Delay=6
├─────────────────────────────────┤
│ Байти 4+: ConfigOBUs [...]      │ ← сирі OBU-дані кодека
└─────────────────────────────────┘
```

> 🎯 **Ключове**: Багато полів займають **менше 1 байта** — бібліотека автоматично пакує/розпаковує їх за тегами `mp4:"...,size=N"`.

---

## 🔑 Розбір важливих полів

### 🔹 `SeqProfile` (3 біти) — профіль кодека

| Значення | Профіль | Опис |
|----------|---------|------|
| `0` | Main | 8/10-біт, 4:2:0, найпоширеніший |
| `1` | High | 8/10-біт, 4:2:0/4:2:2/4:4:4 |
| `2` | Professional | 10/12-біт, всі формати |
| `3-7` | Reserved | Майбутнє використання |

**Для вашого CCTV**: Зазвичай `0` (Main), якщо камера не спеціалізована.

---

### 🔹 `SeqLevelIdx0` (5 біт) — рівень кодека

| Значення | Рівень | Макс. роздільність | Макс. бітрейт |
|----------|--------|-------------------|---------------|
| `0-7` | 2.0-2.3 | 480p | 1-5 Mbps |
| `8-15` | 3.0-3.3 | 720p | 5-20 Mbps |
| `16-23` | 4.0-4.3 | 1080p | 20-50 Mbps |
| `24-31` | 5.0+ | 4K+ | 50+ Mbps |

**Для вашого CCTV**: Зазвичай `16-20` для 1080p стріму.

---

### 🔹 `ChromaSubsamplingX/Y` (по 1 біту) — кольорова підвибірка

```
🎨 Формати пікселів:
• [0,0] = 4:4:4 (без підвибірки, професійне)
• [1,0] = 4:2:2 (горизонтальна підвибірка)
• [1,1] = 4:2:0 (вертикальна+горизонтальна) ← ✅ найпоширеніший для стрімінгу
• [0,1] = незвичний формат (рідко)
```

**Для вашого CCTV**: Зазвичай `[1,1]` (4:2:0) — оптимально для бітрейту.

---

### 🔹 `ChromaSamplePosition` (2 біти) — позиція семплінгу

| Значення | Опис |
|----------|------|
| `0` | Vertical (ITU-R BT.601) |
| `1` | Co-located (ITU-R BT.709) ← ✅ для HD |
| `2` | Co-located (ITU-R BT.2020) ← для HDR/UHD |
| `3` | Reserved |

---

### 🔹 `ConfigOBUs` — сирі дані кодека

```go
ConfigOBUs []uint8 `mp4:"14,size=8"`
```

**Що це?** Масив байтів, що містить **OBU (Open Bitstream Units)** — конфігураційні пакети AV1:

```
📦 Типи OBU в ConfigOBUs:
├── OBU_SEQUENCE_HEADER    ← найважливіший! параметри послідовності
├── OBU_METADATA           ← метадані (колір, HDR)
├── OBU_TEMPORAL_DELIMITER ← роздільник кадрів (опціонально)
└── ... інші ...
```

> 🎯 **Важливо**: Це **сирі біти кодека** — не парсіть їх вручну, просто передавайте декодеру як є.

---

## 🛠️ Практичне використання у вашому HLS-процесорі

### 🔹 Приклад 1: Читання AV1-конфігурації з fMP4-сегмента

```go
import "github.com/abema/go-mp4"

func extractAV1Config(filePath string) (*mp4.Av1C, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	var av1c *mp4.Av1C
	
	// Рекурсивний парсинг всіх боксів
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeAv1C() {
			// Знайшли av1C — розпарсити його
			av1c = &mp4.Av1C{}
			_, err := h.ReadPayload(av1c)
			return av1c, err
		}
		return nil, nil
	})
	
	return av1c, err
}

// Використання:
config, _ := extractAV1Config("segment_000123.m4s")
if config != nil {
	log.Printf("🎬 AV1: Profile=%d, Level=%d, 4:2:0=%v", 
		config.SeqProfile, 
		config.SeqLevelIdx0,
		config.ChromaSubsamplingX == 1 && config.ChromaSubsamplingY == 1)
	
	// Перевірка сумісності з плеєром:
	if config.SeqProfile > 0 {
		log.Printf("⚠️  High/Professional profile may not be supported by all clients")
	}
}
```

---

### 🔹 Приклад 2: Валідація вхідного AV1-стріму

```go
func validateAV1Stream(av1c *mp4.Av1C) error {
	// 🔹 Перевірка заголовка
	if av1c.Marker != 1 || av1c.Version != 1 {
		return fmt.Errorf("invalid Av1C header: marker=%d, version=%d", 
			av1c.Marker, av1c.Version)
	}
	
	// 🔹 Перевірка профілю (для сумісності)
	if av1c.SeqProfile > 1 {
		return fmt.Errorf("profile %d may not be supported by web clients", 
			av1c.SeqProfile)
	}
	
	// 🔹 Перевірка рівня (для бітрейту)
	if av1c.SeqLevelIdx0 > 23 {
		log.Printf("⚠️  High level %d may cause buffering on slow connections", 
			av1c.SeqLevelIdx0)
	}
	
	// 🔹 Перевірка кольорового формату
	if av1c.HighBitdepth == 1 && av1c.TwelveBit == 1 {
		log.Printf("🎨 12-bit HDR content detected")
	}
	
	// 🔹 Перевірка ConfigOBUs
	if len(av1c.ConfigOBUs) == 0 {
		return fmt.Errorf("missing ConfigOBUs — decoder cannot initialize")
	}
	
	// 🔹 Перевірка наявності Sequence Header OBU (перші 2 байти = тип+розмір)
	if len(av1c.ConfigOBUs) >= 2 {
		obuType := av1c.ConfigOBUs[0] & 0x3F  // нижні 6 біт = тип
		if obuType != 1 {  // OBU_SEQUENCE_HEADER = 1
			log.Printf("⚠️  First OBU is not Sequence Header (type=%d)", obuType)
		}
	}
	
	return nil
}
```

---

### 🔹 Приклад 3: Створення нового fMP4 з AV1-конфігурацією

```go
func createAV1Segment(seq int, videoData []byte, config *mp4.Av1C) error {
	f, err := os.Create(fmt.Sprintf("av1_seg_%06d.m4s", seq))
	if err != nil { return err }
	defer f.Close()
	
	// 1. Записати moof (Movie Fragment)
	moof := &mp4.Moof{ /* ... */ }
	mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeMoof()})
	moof.Marshal(f)
	
	// 2. Записати mdat з відео-даними
	// (ConfigOBUs зазвичай вже включені у перший ключовий кадр)
	mdatInfo := &mp4.BoxInfo{
		Type: mp4.BoxTypeMdat(),
		Size: uint64(len(videoData)) + 8,
	}
	mp4.WriteBoxInfo(f, mdatInfo)
	f.Write(videoData)
	
	return nil
}
```

---

## 🔍 Як це пов'язано з вашим CCTV HLS Processor?

```
📡 Ваш потік обробки AV1-відео:
1. Приймаєте fMP4-фрагмент через WebSocket
   │
   ▼
2. Парсите av1C бокс для:
   • Валідації профілю/рівня (чи підтримує клієнт?)
   • Витягування кольорових параметрів (HDR? 4:2:0?)
   • Перевірки ConfigOBUs (чи є Sequence Header?)
   │
   ▼
3. При необхідності:
   • Транскодуєте в інший профіль (якщо клієнт старий)
   • Додаєте метадані про кодек у HLS-плейлист
   • Логуєте параметри для моніторингу якості
   │
   ▼
4. Генеруєте оновлений .m3u8 з кодеком "av01":
   #EXT-X-STREAM-INF:CODECS="av01.0.05M.08"
   segment_000123.m4s
```

---

## 🧪 Приклад тесту для `Av1C`

```go
func TestAv1C_MarshalUnmarshal(t *testing.T) {
	// Створити тестову конфігурацію
	src := &mp4.Av1C{
		Marker:              1,
		Version:             1,
		SeqProfile:          0,  // Main
		SeqLevelIdx0:        16, // Level 4.0
		SeqTier0:            0,
		HighBitdepth:        0,
		TwelveBit:           0,
		Monochrome:          0,
		ChromaSubsamplingX:  1,  // 4:2:0
		ChromaSubsamplingY:  1,
		ChromaSamplePosition: 1, // BT.709
		Reserved:            0,
		InitialPresentationDelayPresent: 1,
		InitialPresentationDelayMinusOne: 5, // delay = 6
		ConfigOBUs: []byte{0x08, 0x00, 0x00, 0x00, 0x2A}, // приклад OBU
	}
	
	// 🔹 Marshal: структура → байти
	buf := bytes.NewBuffer(nil)
	n, err := mp4.Marshal(buf, src, mp4.Context{})
	require.NoError(t, err)
	
	// 🔹 Unmarshal: байти → структура
	dst := &mp4.Av1C{}
	r := bytes.NewReader(buf.Bytes())
	_, err = mp4.Unmarshal(r, uint64(buf.Len()), dst, mp4.Context{})
	require.NoError(t, err)
	
	// 🔹 Перевірка round-trip
	assert.Equal(t, src, dst)
	assert.Equal(t, uint8(1), dst.Marker)
	assert.Equal(t, uint8(0), dst.SeqProfile)
	assert.Equal(t, uint8(16), dst.SeqLevelIdx0)
	assert.Equal(t, []byte{0x08, 0x00, 0x00, 0x00, 0x2A}, dst.ConfigOBUs)
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильне вирівнювання бітів | Поля "з'їжджають", парсинг ламається | Дотримуйтесь порядку полів у структурі (`mp4:"0", mp4:"1", ...`) |
| Ігнорування `const=1` | Запис невалідного заголовка → плеєр відмовляє | Завжди встановлюйте `Marker=1, Version=1` |
| Порожній `ConfigOBUs` | Декодер не може ініціалізуватися → чорний екран | Переконайтеся, що ConfigOBUs містить OBU_SEQUENCE_HEADER |
| Неправильний `ChromaSamplePosition` | Кольори відображаються зі зсувом | Використовуйте `1` для HD (BT.709), `2` для UHD (BT.2020) |
| Рівень > 23 для вебу | Буферизація на слабких пристроях | Для HLS у браузері обмежте `SeqLevelIdx0 <= 23` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні AV1-сегмента:
    • Витягніть av1C бокс через ReadBoxStructure
    • Перевірте Marker=1, Version=1
    • Залогуйте профіль/рівень для моніторингу

[ ] Для сумісності з вебом:
    • Відхиляйте профілі >1 (High/Professional)
    • Обмежте рівень <=23 (4.3) для 1080p
    • Переконайтеся, що 4:2:0 (ChromaSubsamplingX=Y=1)

[ ] При записі нових сегментів:
    • Скопіюйте ConfigOBUs з вхідного потоку (не генеруйте нові!)
    • Збережіть оригінальні параметри кольору
    • Додайте CODECS="av01.0.XX.YZ" у HLS-плейлист

[ ] Для дебагу:
    • Логуйте сирий ConfigOBUs: log.Printf("📦 OBU: % x", av1c.ConfigOBUs[:10])
    • Перевіряйте тип першого OBU: (ConfigOBUs[0] & 0x3F) == 1?
    • Використовуйте Stringify() для людського виводу структури

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: VLC, hls.js, ExoPlayer
    • Перевірте відтворення на слабких пристроях (старі смартфони)
```

---

## 🎯 Висновок

> **`Av1C` — це ключ до коректного відтворення AV1-відео у вашому HLS-стрімі**.  
> Він забезпечує:
> • ✅ Ініціалізацію декодера з правильними параметрами
> • ✅ Сумісність з різними плеєрами та пристроями
> • ✅ Ефективну компресію завдяки точному опису профілю/рівня
> • ✅ Підтримку HDR, 10/12-бітного кольору, 4:2:0

Для вашого **CCTV HLS Processor** це означає:
- 🎥 Клієнти коректно декодують AV1-потік без артефактів
- 🌐 Підтримка сучасних браузерів (Chrome, Firefox з AV1)
- 📉 Економія бітрейту на 30-50% порівняно з H.264 при тій самій якості

Потребуєте допомоги з парсингом `ConfigOBUs`, валідацією профілів або інтеграцією автотестів для AV1? Напишіть — покажу готовий код! 🚀🎬