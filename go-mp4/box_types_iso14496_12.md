# 📦 MP4 Box Definitions: Повний огляд файлу

Це **великий файл з бібліотеки `go-mp4`**, який містить визначення **50+ типів боксів** стандарту ISO/IEC 14496-12 (MP4/ISOBMFF).

---

## 🎯 Коротка відповідь

> **Це "словник" всіх можливих боксів у MP4-файлі** — від `ftyp` (тип файлу) до `trun` (таймстемпи кадрів).  
> Кожен тип боксу має:
> • Структуру полів з тегами `mp4:"..."`
> • Методи для динамічних полів (`GetFieldLength`, `IsOptFieldEnabled`)
> • Реєстрацію в бібліотеці через `init()`

---

## 🗂️ Структура файлу: Шаблон для кожного боксу

Кожен бокс визначається за **однаковим шаблоном**:

```go
/*************************** ІМЯ_БОКСУ ****************************/

// 1. Функція для отримання типу боксу (4 ASCII-символи)
func BoxTypeImya() BoxType { return StrToBoxType("imya") }

// 2. Реєстрація в бібліотеці при завантаженні пакету
func init() {
	AddBoxDef(&Imya{}, 0)  // 0 = підтримувані версії боксу
}

// 3. Структура з полями та тегами
type Imya struct {
	Box  // або FullBox для боксів з version+flags
	Pole1 uint32 `mp4:"0,size=32"`
	Pole2 []byte `mp4:"1,size=8,len=dynamic"`
}

// 4. Метод GetType() для інтерфейсу
func (*Imya) GetType() BoxType {
	return BoxTypeImya()
}

// 5. (Опціонально) Методи для динамічних полів
func (i *Imya) GetFieldLength(name string, ctx Context) uint {
	switch name {
	case "Pole2":
		return uint(i.Pole1)  // довжина залежить від іншого поля
	}
	panic(fmt.Errorf("invalid field: %s", name))
}
```

---

## 🔑 Ключові концепції та теги `mp4:"..."`

### 🔹 Базові теги

| Тег | Значення | Приклад |
|-----|----------|---------|
| `size=N` | Розмір поля в бітах | `size=32` = uint32 |
| `len=dynamic` | Довжина слайсу залежить від іншого поля | `[]uint64` з `GetFieldLength()` |
| `opt=0xXXXX` | Опціональне поле (увімкнено, якщо прапорець у flags) | `opt=0x000001` |
| `ver=0` / `nver=0` | Поле для версії 0 / не для версії 0 | `SampleOffsetV0` vs `SampleOffsetV1` |
| `string` | Інтерпретувати байти як рядок | `[4]byte` → "avc1" |
| `const=N` | Поле має завжди дорівнювати N (валідація) | `Reserved uint8 \`mp4:"size=5,const=0"\`` |
| `hidden` | Поле читається, але не записується | `Pad bool \`mp4:"size=1,hidden"\`` |
| `iso639-2` | Спеціальне кодування мови (5 біт/літера) | `Language [3]byte` |
| `hex` / `dec` | Формат виводу у `Stringify()` | `Flags uint32 \`mp4:"size=24,hex"\`` |

---

### 🔹 Приклад: `trun` (Track Fragment Run) — найважливіший для HLS

```go
type Trun struct {
	FullBox     `mp4:"0,extend"`
	SampleCount uint32 `mp4:"1,size=32"`

	// 🔹 Опціональні поля (залежать від прапорців у flags)
	DataOffset       int32       `mp4:"2,size=32,opt=0x000001"`  // flag 0x01
	FirstSampleFlags uint32      `mp4:"3,size=32,opt=0x000004,hex"` // flag 0x04
	Entries          []TrunEntry `mp4:"4,len=dynamic,size=dynamic"` // динамічний масив
}

type TrunEntry struct {
	// 🔹 Кожне поле увімкнене окремим прапорцем:
	SampleDuration                uint32 `mp4:"0,size=32,opt=0x000100"`  // flag 0x100
	SampleSize                    uint32 `mp4:"1,size=32,opt=0x000200"`  // flag 0x200
	SampleFlags                   uint32 `mp4:"2,size=32,opt=0x000400,hex"` // flag 0x400
	SampleCompositionTimeOffsetV0 uint32 `mp4:"3,size=32,opt=0x000800,ver=0"` // flag 0x800, версія 0
	SampleCompositionTimeOffsetV1 int32  `mp4:"4,size=32,opt=0x000800,nver=0"` // flag 0x800, версія !=0
}
```

**🎯 Для вашого HLS-процесора**: `trun` містить **PTS/DTS для кожного кадру** — це ключ до синхронізації аудіо/відео!

---

### 🔹 Динамічні поля: `GetFieldLength` / `IsOptFieldEnabled`

```go
// Приклад: ctts (Composition Time to Sample)
type Ctts struct {
	FullBox    `mp4:"0,extend"`
	EntryCount uint32      `mp4:"1,size=32"`
	Entries    []CttsEntry `mp4:"2,len=dynamic,size=64"`  // ← len=dynamic!
}

// Бібліотека питає: "Яка довжина у поля Entries?"
func (ctts *Ctts) GetFieldLength(name string, ctx Context) uint {
	switch name {
	case "Entries":
		return uint(ctts.EntryCount)  // ← відповідь: стільки, скільки вказано в EntryCount
	}
	panic(fmt.Errorf("invalid field: %s", name))
}

// Приклад: colr (Colour Information) — опціональні поля залежать від типу
func (colr *Colr) IsOptFieldEnabled(name string, ctx Context) bool {
	switch colr.ColourType {
	case [4]byte{'n', 'c', 'l', 'x'}:  // "nclx" = параметричний колір
		// Увімкнути поля кольорового простору
		return name == "ColourPrimaries" || name == "TransferCharacteristics" || ...
	case [4]byte{'r', 'I', 'C', 'C'}:  // "ICC " = ICC-профіль
		// Увімкнути тільки поле профілю
		return name == "Profile"
	default:
		// Невідомий тип — читати як сирі байти
		return name == "Unknown"
	}
}
```

> 🎯 **Магія**: Ви не пишете `if/else` для кожного боксу — бібліотека **автоматично** питає у структури, які поля читати!

---

### 🔹 Версійні поля: `ver=0` / `nver=0`

Деякі бокси мають **різну структуру для різних версій**:

```go
type Cslg struct {
	FullBox `mp4:"0,extend"`
	
	// 🔹 Поля для версії 0 (32-бітні значення)
	CompositionToDTSShiftV0        int32 `mp4:"1,size=32,ver=0"`
	LeastDecodeToDisplayDeltaV0    int32 `mp4:"2,size=32,ver=0"`
	
	// 🔹 Поля для версії 1 (64-бітні значення)
	CompositionToDTSShiftV1        int64 `mp4:"6,size=64,nver=0"`  // nver = not version 0
	LeastDecodeToDisplayDeltaV1    int64 `mp4:"7,size=64,nver=0"`
}

// Допоміжні методи для зручного доступу
func (cslg *Cslg) GetCompositionToDTSShift() int64 {
	switch cslg.GetVersion() {
	case 0: return int64(cslg.CompositionToDTSShiftV0)
	case 1: return cslg.CompositionToDTSShiftV1
	default: return 0
	}
}
```

> 🎯 **Перевага**: Один код працює з обома версіями боксу — бібліотека сама обирає потрібні поля.

---

## 🧩 Категорії боксів у файлі

### 🔹 📁 Метадані файлу
| Бокс | Призначення | Приклад використання |
|------|-------------|---------------------|
| `ftyp` | File Type — тип файлу, сумісні бренди | Перевірка: чи підтримує плеєр цей файл? |
| `moov` | Movie — метадані всього файлу | Читання треків, тривалості, кодеків |
| `mvhd` | Movie Header — загальна інформація | Тривалість, timescale, матриця трансформації |
| `udta` | User Data — користувацькі метадані | Назва передачі, автор, опис |

### 🔹 🎥 Відео-доріжки
| Бокс | Призначення | Приклад використання |
|------|-------------|---------------------|
| `trak` | Track — опис однієї доріжки | Ітерація по відео/аудіо/субтитрах |
| `tkhd` | Track Header — параметри доріжки | Розмір кадру, тривалість, увімкнено/вимкнено |
| `mdia` | Media — медіа-інформація доріжки | Кодек, мова, таймскейл |
| `minf` | Media Info — специфіка медіа | Відео: `vmhd`, Аудіо: `smhd`, Субтитри: `null` |
| `stbl` | Sample Table — таблиця семплів | Пошук кадрів за часом |

### 🔹 ⏱️ Таймінги та синхронізація ⭐ Найважливіші для HLS!
| Бокс | Призначення | Приклад використання |
|------|-------------|---------------------|
| `stts` | Decoding Time to Sample | PTS для декодування (без урахування B-фреймів) |
| `ctts` | Composition Time to Sample | PTS для відображення (з урахуванням B-фреймів) |
| `stss` | Sync Samples — ключові кадри | Пошук I-фреймів для seek / сегментації |
| `trun` | Track Fragment Run | **PTS/DTS для кожного кадру в fMP4!** |
| `tfdt` | Track Fragment Decode Time | Базовий час декодування для фрагмента |

### 🔹 🔊 Аудіо-параметри
| Бокс | Призначення | Приклад використання |
|------|-------------|---------------------|
| `smhd` | Sound Media Header | Balance (стерео-панорама) |
| `dac3` | AC-3 config | Параметри Dolby Digital: канали, бітрейт |
| `esds` | ES Descriptor (MPEG-4) | Конфігурація AAC/MP3 кодеків |

### 🔹 🎨 Візуальні параметри
| Бокс | Призначення | Приклад використання |
|------|-------------|---------------------|
| `vmhd` | Video Media Header | Graphics mode, opcolor (для накладок) |
| `colr` | Colour Information | HDR, колірний простір (BT.709, BT.2020) |
| `pasp` | Pixel Aspect Ratio | Співвідношення пікселів (для анаморфного відео) |
| `fiel` | Field Information | Interlaced video: порядок полів |

### 🔹 📦 Фрагментований MP4 (fMP4) — основа HLS/DASH
| Бокс | Призначення | Приклад використання |
|------|-------------|---------------------|
| `moof` | Movie Fragment — метадані фрагмента | Кожен .m4s сегмент має свій `moof` |
| `mfhd` | Movie Fragment Header | Порядковий номер фрагмента |
| `traf` | Track Fragment — фрагмент доріжки | Відео/аудіо окремо в одному фрагменті |
| `tfhd` | Track Fragment Header | Базові параметри для семплів у фрагменті |
| `trun` | Track Fragment Run | **Таймстемпи, розміри, прапорці кадрів** |
| `sidx` | Segment Index — індекс під-сегментів | Швидкий seek у довгих файлах |

### 🔹 🔐 DRM та захист
| Бокс | Призначення | Приклад використання |
|------|-------------|---------------------|
| `sinf` | Protection Scheme Info | Опис системи захисту (FairPlay, Widevine) |
| `schm` | Scheme Type | Тип DRM: "cenc" (Common Encryption) |
| `schi` | Scheme Information | Ключі, ліцензії, специфіка DRM |
| `saio` / `saiz` | Sample Auxiliary Info | Офсети/розміри зашифрованих блоків |

### 🔹 📝 Субтитри та тексти
| Бокс | Призначення | Приклад використання |
|------|-------------|---------------------|
| `stpp` | XML Subtitle Sample Entry | TTML/IMSC субтитри |
| `sbtt` | Text Subtitle Sample Entry | SRT/VTT-подібні субтитри |
| `elst` | Edit List | Пропуск тиші, обрізка початку/кінця |

---

## 🛠️ Практичне використання: Приклади для вашого HLS-процесора

### 🔹 Приклад 1: Читання таймстемпів з `trun` (найважливіше!)

```go
import "github.com/abema/go-mp4"

func extractFrameTimestamps(filePath string) ([]FrameInfo, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	var frames []FrameInfo
	
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeTrun() {
			trun := &mp4.Trun{}
			if _, err := h.ReadPayload(trun); err != nil {
				return nil, err
			}
			
			baseTime := trun.DataOffset  // базове зміщення даних
			for i := uint32(0); i < trun.SampleCount; i++ {
				entry := trun.Entries[i]
				
				// 🔹 Отримуємо composition time offset (різниця між DTS та PTS)
				cto := trun.GetSampleCompositionTimeOffset(int(i))
				
				frames = append(frames, FrameInfo{
					Index:     int(i),
					Duration:  entry.SampleDuration,
					Size:      entry.SampleSize,
					IsKeyFrame: (entry.SampleFlags & 0x00010000) != 0, // sap_type
					PTSOffset: cto,  // додайте до base decode time
				})
			}
		}
		return nil, nil
	})
	
	return frames, err
}
```

---

### 🔹 Приклад 2: Пошук ключових кадрів через `stss`

```go
func findKeyFrames(filePath string) ([]uint32, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	var keyFrames []uint32
	
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeStss() {
			stss := &mp4.Stss{}
			if _, err := h.ReadPayload(stss); err != nil {
				return nil, err
			}
			// stss.SampleNumber містить номери ключових кадрів (1-based!)
			keyFrames = append(keyFrames, stss.SampleNumber...)
		}
		return nil, nil
	})
	
	return keyFrames, err
}

// Використання для сегментації:
keyFrames, _ := findKeyFrames("video.mp4")
for _, kf := range keyFrames {
	if kf >= startSample && kf <= endSample {
		// Це ключовий кадр у потрібному діапазоні — можна робити сегмент тут!
	}
}
```

---

### 🔹 Приклад 3: Валідація кодека через `avcC` / `hvcC` / `Av1C`

```go
func validateCodec(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil { return err }
	defer f.Close()
	
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		switch h.BoxInfo.Type {
		case mp4.BoxTypeAvcC():  // H.264
			avcc := &mp4.AVCDecoderConfiguration{}
			h.ReadPayload(avcc)
			
			// Перевірка профілю для сумісності з вебом
			if avcc.Profile != mp4.AVCHighProfile && avcc.Profile != mp4.AVCMainProfile {
				return nil, fmt.Errorf("unsupported AVC profile: %d", avcc.Profile)
			}
			
		case mp4.BoxTypeHvcC():  // H.265/HEVC
			hvcc := &mp4.HvcC{}
			h.ReadPayload(hvcc)
			
			// Перевірка рівня для 1080p
			if hvcc.GeneralLevelIdc > 120 {  // Level 4.0
				log.Printf("⚠️  HEVC level %d may not be supported by all browsers", 
					hvcc.GeneralLevelIdc)
			}
			
		case mp4.BoxTypeAv1C():  // AV1
			av1c := &mp4.Av1C{}
			h.ReadPayload(av1c)
			
			// Перевірка профілю
			if av1c.SeqProfile > 1 {  // Main or High
				log.Printf("⚠️  AV1 profile %d may have limited browser support", 
					av1c.SeqProfile)
			}
		}
		return nil, nil
	})
	
	return err
}
```

---

### 🔹 Приклад 4: Читання метаданих з `udta` + 3GPP-рядків

```go
func readMetadata(filePath string) (map[string]string, error) {
	result := make(map[string]string)
	
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		// Перевіряємо контекст: чи ми всередині udta?
		if h.BoxInfo.Type.IsSupported(h.BoxInfo.Context) && 
		   h.BoxInfo.Context.UnderUdta {
			
			boxType := h.BoxInfo.Type.String()
			switch boxType {
			case "titl", "dscp", "cprt", "perf", "auth", "gnre":
				meta := &mp4.Udta3GppString{}
				if _, err := h.ReadPayload(meta); err != nil {
					return nil, err
				}
				
				// Декодуємо мову з 5-бітного формату
				lang := decodeISO639_2(meta.Language)
				result[boxType] = fmt.Sprintf("[%s] %s", lang, string(meta.Data))
			}
		}
		return nil, nil
	})
	
	return result, err
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Ігнорування `opt=...` у `trun` | Читаєте "зайві" поля → зсув таймстемпів | Завжди перевіряйте `trun.GetFlags() & 0x100` перед читанням `SampleDuration` |
| Неправильна обробка `ver=0` / `nver=0` | 32-бітне значення читається як 64-бітне → помилка | Використовуйте допоміжні методи: `trun.GetSampleCompositionTimeOffset()` |
| Забути `const=N` валідацію | Пошкоджені файли не відхиляються → артефакти | Додайте перевірку: `if box.Reserved != 0 { return err }` |
| Неправильне кодування `iso639-2` | Мова відображається як "und" | Використовуйте формулу: `код = (літера - 'a' + 1)` |
| Ігнорування `len=dynamic` | Читаєте не ту кількість елементів → краш | Завжди реалізуйте `GetFieldLength()` для слайсів |

---

## 📋 Чекліст для вашого проекту

```
[ ] Для парсингу fMP4-сегментів:
    • Завжди шукайте `trun` бокси для таймстемпів
    • Перевіряйте прапорці перед читанням опціональних полів
    • Використовуйте `GetSampleCompositionTimeOffset()` для CTS

[ ] Для сегментації на ключових кадрах:
    • Читайте `stss` для пошуку I-фреймів
    • Або перевіряйте `SampleFlags & 0x00010000` у `trun.Entries`

[ ] Для валідації кодеків:
    • Читайте `avcC` / `hvcC` / `Av1C` з `stsd`
    • Перевіряйте профіль/рівень для сумісності з вебом

[ ] Для метаданих:
    • Шукайте бокси всередині `udta` з `Context.UnderUdta=true`
    • Декодуйте мову з 5-бітного формату для 3GPP-рядків

[ ] Для дебагу:
    • Логуйте тип боксу та розмір: 
      log.Printf("📦 %s @ offset=%d, size=%d", 
          bi.Type, bi.Offset, bi.Size)
    • Використовуйте `Stringify()` для людського виводу структур

[ ] Для тестування:
    • Напишіть тести з `Marshal` → `Unmarshal` для ваших боксів
    • Перевіряйте round-trip: структура → байти → структура
```

---

## 🎯 Висновок

> **Цей файл — "енциклопедія" форматів MP4 для бібліотеки `go-mp4`**.  
> Він дозволяє:
> • ✅ Парсити будь-який стандартний бокс без написання парсерів з нуля
> • ✅ Працювати з динамічними полями через інтерфейси
> • ✅ Підтримувати різні версії боксів (`ver=0` / `nver=0`)
> • ✅ Валідувати дані через `const=N` теги

Для вашого **CCTV HLS Processor** це означає:
- 🎥 Коректна синхронізація аудіо/відео через `trun` / `ctts`
- 🔍 Швидка сегментація на ключових кадрах через `stss`
- 🌐 Сумісність з різними кодеками (H.264, HEVC, AV1) через `avcC` / `hvcC` / `Av1C`
- 📝 Підтримка метаданих (назви, описи, мови) через `udta` + 3GPP-рядки

Потребуєте допомоги з парсингом конкретного боксу (`trun` для таймстемпів, `stss` для ключових кадрів) або з інтеграцією цих структур у ваш конвеєр? Напишіть — покажу готовий код! 🚀🎬