# 📦 MP4 Library: Повне пояснення + Практичні приклади

Це код з бібліотеки **`github.com/abema/go-mp4`** — низькорівневий інструмент для роботи з **MP4/ISOBMFF** файлами (формат, який використовується в HLS/fMP4 стрімінгу).

---

## 🎯 Коротка відповідь

> **Це "конструктор LEGO" для MP4-файлів**.  
> Ви не працюєте з бітами вручну — просто створюєте структури з тегами `mp4:"..."`, а бібліотека сама серіалізує/десеріалізує їх у правильний бінарний формат.

---

## 🧱 Архітектура: Ієрархія типів

```
🔹 ICustomFieldObject  ← Інтерфейс для ДИНАМІЧНИХ полів
     │
     ├── BaseCustomFieldObject  ← "заглушка" з panic-реалізаціями
     │
🔹 IImmutableBox       ← Бокс тільки для читання
     │  ├── GetVersion(), GetFlags(), GetType()
     │
🔹 IBox                ← Бокс для читання + запису
     │  ├── Додає: SetVersion(), SetFlags(), AddFlag()...
     │
🔹 Box                 ← Базова реалізація (пусті методи)
     │
🔹 FullBox             ← Box + стандартні поля ISO: version + flags
```

### 📊 Візуалізація спадкування:
```
┌─────────────────────────────┐
│ ICustomFieldObject          │ ← "Вмієш працювати з динамічними полями?"
└────────┬────────────────────┘
         │
┌────────▼────────┐  ┌─────────────────────┐
│ BaseCustomField │  │ IImmutableBox       │
│ (panic-методи)  │  │ (тільки читання)    │
└────────┬────────┘  └────────┬────────────┘
         │                    │
         │ ┌──────────────────▼──────────┐
         └─► IBox                        │
             (читання + запис)           │
               │                         │
    ┌──────────▼─────────┐              │
    │ Box                │              │
    │ (базова реалізація)│              │
    └──────────┬─────────┘              │
               │                        │
    ┌──────────▼─────────┐              │
    │ FullBox            │◄─────────────┘
    │ (version + flags)  │
    └────────────────────┘
```

---

## 🔑 Ключові концепції

### 1. **Бокси (Boxes/Atoms) — будівельні блоки MP4**

Кожен бокс має стандартний заголовок:
```
[4 байти: розмір] + [4 байти: тип] + [корисне навантаження]
```

**Приклад ієрархії для fMP4 (ваш випадок):**
```
📦 moof (Movie Fragment)
├── 📦 mfhd (Movie Fragment Header)
├── 📦 traf (Track Fragment)
│   ├── 📦 tfhd (Track Fragment Header)
│   ├── 📦 trun (Track Fragment Run) ← таймстемпи кадрів!
│   └── 📦 sdtp (Sample Dependency)
└── 📦 mdat (Media Data) ← сирі відео/аудіо байти
```

---

### 2. **Теги `mp4:"..."` — мова опису структури**

```go
type FullBox struct {
	BaseCustomFieldObject
	Version uint8   `mp4:"0,size=8"`           // поле #0, 8 біт = 1 байт
	Flags   [3]byte `mp4:"1,size=8"`           // поле #1, три байти по 8 біт
}

type Av1C struct {
	Box
	SeqProfile   uint8 `mp4:"2,size=3"`        // 3 біти для профілю
	SeqLevelIdx0 uint8 `mp4:"3,size=5"`        // 5 біт для рівня
	// 3+5 = 8 біт = 1 байт → автоматичне вирівнювання!
}
```

| Тег | Значення |
|-----|----------|
| `mp4:"0,size=8"` | Поле #0, розмір 8 біт |
| `mp4:"1,size=5,iso639-2"` | Поле #1, 5 біт, кодування мови |
| `mp4:"2,size=8,string"` | Поле #2, байти, інтерпретувати як рядок |
| `mp4:"3,size=8,var"` | Поле #3, змінна довжина (читати до кінця боксу) |
| `mp4:"4,extend"` | Розширити заголовок (для `FullBox`) |
| `mp4:"5,hidden"` | Поле є в структурі, але не записується у файл |

---

### 3. **Динамічні поля: `ICustomFieldObject`**

Деякі поля мають розмір, який **залежить від інших полів**:

```go
// Приклад: бокс з опціональним полем
type TrafBox struct {
	FullBox
	TrackID uint32 `mp4:"0,size=32"`
	// Поле default_sample_duration є ТІЛЬКИ якщо flags & 0x000008 != 0
	DefaultSampleDuration uint32 `mp4:"1,size=32,opt=0x000008"`
}

// Реалізація інтерфейсу для обчислення розміру "на льоту":
func (t *TrafBox) IsOptFieldEnabled(name string, ctx Context) bool {
	if name == "DefaultSampleDuration" {
		return t.Flags[2] & 0x08 != 0  // перевірка біта
	}
	return false
}
```

> 🎯 **Навіщо це?** Без цього довелося б писати `if/else` для кожного боксу. З інтерфейсом — бібліотека сама питає: *"Чи треба читати це поле?"*.

---

### 4. **Контекст парсингу: `Context`**

MP4 має **контекстно-залежні правила**:

```go
type Context struct {
	UnderUdta     bool  // Чи ми в udta? → парсити метадані інакше
	UnderIlst     bool  // Чи ми в iTunes metadata?
	IsQuickTimeCompatible bool // Чи це .mov від QuickTime?
}
```

**Приклад**: Бокс `data` може бути:
- 🎵 Сирими аудіо-даними (якщо в `mdat`)
- 📝 UTF-8 текстом (якщо в `ilst` → метадані)
- 🔤 Псевдо-рядком з довжиною (якщо `IsPString=true`)

Без `Context` парсер не зможе правильно інтерпретувати одні й ті самі байти.

---

## 🛠️ Практичне використання: Приклади для вашого проекту

### 🔹 Приклад 1: Читання `tfhd` боксу (таймстемпи фрагмента)

```go
import (
	"os"
	"github.com/abema/go-mp4"
)

func readFragmentTimestamps(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil { return err }
	defer f.Close()

	// Рекурсивний парсинг всіх боксів
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		switch h.BoxInfo.Type {
		case mp4.BoxTypeTfhd(): // Track Fragment Header
			tfhd := &mp4.Tfhd{}
			if _, err := h.ReadPayload(tfhd); err != nil {
				return nil, err
			}
			log.Printf("📦 tfhd: TrackID=%d, BaseDataOffset=%d", 
				tfhd.TrackID, tfhd.BaseDataOffset)
			
		case mp4.BoxTypeTrun(): // Track Fragment Run → таймстемпи!
			trun := &mp4.Trun{}
			if _, err := h.ReadPayload(trun); err != nil {
				return nil, err
			}
			log.Printf("📦 trun: %d samples, first-sample-flags=0x%x", 
				trun.SampleCount, trun.FirstSampleFlags)
			// trun.Samples містить PTS/DTS для кожного кадру!
		}
		return nil, nil
	})
	return err
}
```

---

### 🔹 Приклад 2: Створення нового fMP4-сегмента з вашими даними

```go
func createSegment(seq int, videoData []byte, audioData []byte) error {
	f, err := os.Create(fmt.Sprintf("segment_%06d.m4s", seq))
	if err != nil { return err }
	defer f.Close()

	// 1. Створити moof (Movie Fragment)
	moof := &mp4.Moof{
		// ... заповнити поля ...
	}
	
	// 2. Записати заголовок боксу
	_, err = mp4.WriteBoxInfo(f, &mp4.BoxInfo{
		Type: mp4.BoxTypeMoof(),
	})
	if err != nil { return err }
	
	// 3. Серіалізувати бокс (бібліотека сама обчислить розмір!)
	_, err = moof.Marshal(f)
	if err != nil { return err }
	
	// 4. Записати sирі дані в mdat
	_, err = mp4.WriteBoxInfo(f, &mp4.BoxInfo{
		Type: mp4.BoxTypeMdat(),
		Size: uint64(len(videoData) + len(audioData) + 8), // +8 для заголовка
	})
	if err != nil { return err }
	f.Write(videoData)
	f.Write(audioData)
	
	return nil
}
```

---

### 🔹 Приклад 3: Додавання метаданих (назва передачі) у `udta`

```go
func addMetadata(filePath, title, language string) error {
	// Створити бокс 3GPP string (стандарт для метаданих)
	langBytes := [3]byte{language[0], language[1], language[2]}
	meta := &mp4.Udta3GppString{
		FullBox: mp4.FullBox{Version: 0, Flags: [3]byte{0,0,0}},
		Language: langBytes,  // "ukr", "eng", "rus"...
		Data:     []byte(title),
	}
	
	f, err := os.OpenFile(filePath, os.O_RDWR, 0644)
	if err != nil { return err }
	defer f.Close()
	
	// Знайти або створити udta бокс
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeUdta() {
			// Записати метадані всередину udta
			mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.StrToBoxType("titl")})
			meta.Marshal(f)
		}
		return nil, nil
	})
	return err
}
```

---

## 🔍 Як це працює "під капотом"

### Серіалізація (запис у файл):
```
Ваша структура (Go)
       │
       ▼
mp4.Marshal(w)  ← бібліотека
       │
       ├── 1. Обчислити розмір кожного поля за тегами `mp4:"..."`
       ├── 2. Записати заголовок: [розмір][тип]
       ├── 3. Записати поля у бінарному форматі (big-endian!)
       ├── 4. Для динамічних полів: викликати GetFieldSize() / IsOptFieldEnabled()
       │
       ▼
Бінарний MP4-файл ✅
```

### Десеріалізація (читання з файлу):
```
Бінарний MP4-файл
       │
       ▼
mp4.ReadBoxStructure(r, callback)
       │
       ├── 1. Прочитати заголовок: [розмір][тип]
       ├── 2. Створити екземпляр структури за типом боксу
       ├── 3. Читати поля за тегами `mp4:"..."`
       ├── 4. Для динамічних полів: викликати методи інтерфейсу
       ├── 5. Викликати ваш callback для кожного боксу
       │
       ▼
Ваша структура (Go) + callback-логіка ✅
```

---

## 🚀 Інтеграція у ваш CCTV HLS Processor

```
📡 Ваш потік обробки:
1. Приймаєте fMP4-фрагмент через WebSocket
   │
   ▼
2. Парсите mp4.ReadBoxStructure() для:
   • Витягнути PTS/DTS з `trun` → нормалізація часу
   • Знайти ключові кадри в `stss` → сегментація
   • Прочитати кодек-конфіг (`avcC`/`hvcC`/`Av1C`) → валідація
   │
   ▼
3. Модифікуєте структуру:
   • Оновлюєте таймстемпи з урахуванням drift
   • Додаєте субтитри як окрему доріжку (`stbl` + `mdat`)
   │
   ▼
4. Записуєте новий сегмент через mp4.Marshal()
   │
   ▼
5. Оновлюєте HLS-плейлист (.m3u8)
```

---

## 🧪 Тестування: Приклад з вашого коду

```go
// mp4/box_test.go
func TestFullBoxFlags(t *testing.T) {
	box := FullBox{}
	
	// Встановити flags = 0x35ac68
	box.SetFlags(0x35ac68)
	
	// Перевірити, що байти розставлені правильно (big-endian)
	assert.Equal(t, byte(0x35), box.Flags[0])  // старший байт
	assert.Equal(t, byte(0xac), box.Flags[1])
	assert.Equal(t, byte(0x68), box.Flags[2])  // молодший байт
	
	// Зворотне перетворення
	assert.Equal(t, uint32(0x35ac68), box.GetFlags())
	
	// Робота з бітами: додати/видалити прапори
	box.AddFlag(0x030000)     // 0x35ac68 | 0x030000 = 0x37ac68
	assert.Equal(t, uint32(0x37ac68), box.GetFlags())
	
	box.RemoveFlag(0x000900)  // 0x37ac68 & ~0x000900 = 0x37a468
	assert.Equal(t, uint32(0x37a468), box.GetFlags())
}
```

> 🎯 **Навіщо цей тест?** Прапори в MP4 — це 24 біти, які керують поведінкою боксу. Помилка в біті → некоректний парсинг у плеєрі.

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний порядок полів у структурі | Дані зсуваються, парсинг ламається | Поля мають йти у порядку `mp4:"0,...", mp4:"1,...", ...` |
| Забули `BaseCustomFieldObject` | `panic: GetFieldSize not implemented` | Завжди вбудовуйте базовий тип: `type MyBox struct { Box }` |
| Неправильний `size` у тегах | Читання "з'їдає" зайві байти | 1 байт = 8 біт; `[3]byte` з `size=8` = три окремих байти |
| Не обробили `ExtendToEOF` | Помилка при читанні останнього боксу | Перевіряйте `bi.ExtendToEOF` перед `Seek()` |
| Ігнорування `Context` | Неправильна інтерпретація `data` боксів | Завжди передавайте `ctx` у рекурсивні виклики |

---

## 📋 Чекліст для початку роботи

```
[ ] Додати залежність: go get github.com/abema/go-mp4@latest

[ ] Для парсингу: використовуйте mp4.ReadBoxStructure(r, callback)
    • callback викликається для КОЖНОГО боксу
    • повертайте (ні, nil) для пропуску, або (дані, nil) для збору

[ ] Для запису: 
    1. Створіть структуру з тегами mp4:"..."
    2. Викличіть mp4.WriteBoxInfo() для заголовка
    3. Викличіть box.Marshal(w) для даних

[ ] Для динамічних полів: реалізуйте ICustomFieldObject
    • IsOptFieldEnabled() — чи читати опціональне поле?
    • GetFieldSize() — який розмір у динамічного поля?

[ ] Дебаг: додайте логування в callback:
    log.Printf("📦 %s @ offset=%d, size=%d", 
        bi.Type.String(), bi.Offset, bi.Size)

[ ] Тестуйте на реальних fMP4 з вашого стріму перед продакшеном
```

---

## ❓ Часті питання

**Q: Чи можу я модифікувати існуючий MP4-файл "на місці"?**  
A: Технічно — так, але це складно: зміна розміру одного боксу зсуває офсети всіх наступних.  
✅ **Краща практика**: створюйте новий файл, копіюючи потрібні бокси + ваші зміни.

**Q: Чи підтримує бібліотека фрагментований MP4 (fMP4)?**  
A: Так! Бокси `moof`, `mdat`, `traf`, `tfhd`, `trun` — повністю підтримуються. Саме це потрібно для HLS.

**Q: Як додати свій власний тип боксу?**  
```go
// 1. Оголосіть структуру
type MyBox struct {
	mp4.FullBox
	MyField uint32 `mp4:"0,size=32"`
}

// 2. Реалізуйте GetType()
func (MyBox) GetType() mp4.BoxType { return mp4.StrToBoxType("mybx") }

// 3. Зареєструйте в init()
func init() {
	mp4.AddBoxDef(&MyBox{}, 0) // 0 = версія боксу
}
```

**Q: Як обробити великі файли (>4 ГБ)?**  
A: Бібліотека автоматично використовує "large header" (16 байт замість 8), якщо `Size > 2^32`.  
Просто переконайтеся, що використовуєте `uint64` для розмірів.

---

## 🎯 Висновок

> **`go-mp4` — це ваш "шлюз" до бітів MP4 без болю**.  
> Ви описуєте структуру даних на високому рівні (теги `mp4:"..."`), а бібліотека бере на себе:  
> • Бінарну серіалізацію (big-endian, вирівнювання)  
> • Динамічні поля (через інтерфейси)  
> • Контекстну логіку (через `Context`)  
> • Обробку edge-cases (`ExtendToEOF`, large-size)

Для вашого HLS-процесора це означає: **менше коду, менше багів, більше часу на бізнес-логіку** (синхронізація, субтитри, метрики).

Потребуєте допомоги з парсингом конкретного боксу (`trun` для PTS, `stbl` для ключових кадрів) або з додаванням субтитрів як окремої доріжки? Напишіть — покажу готовий код! 🎬🚀