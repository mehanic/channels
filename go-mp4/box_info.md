# 📦 `Context` та `BoxInfo`: Ядро парсингу MP4

Це **фундаментальні типи** бібліотеки `go-mp4`, які відповідають за:
1. **`Context`** — *"Де ми знаходимось у ієрархії боксів?"*
2. **`BoxInfo`** — *"Де і який цей бокс у файлі?"*

---

## 🧭 `Context`: Навігація по ієрархії боксів

MP4 — це **дерево боксів**, і одні й ті самі дані можуть інтерпретуватися по-різному залежно від контексту.

```go
type Context struct {
	IsQuickTimeCompatible bool  // Чи це .mov від QuickTime?
	
	// iTunes/QuickTime metadata навігація:
	QuickTimeKeysMetaEntryCount int
	UnderWave       bool  // Чи ми в "wave" боксі?
	UnderIlst       bool  // Чи ми в "ilst" (iTunes metadata)?
	UnderIlstMeta   bool  // Чи ми в метаданих всередині ilst?
	UnderIlstFreeMeta bool // Чи ми в "----" (free-form metadata)?
	
	// 3GPP/HLS metadata:
	UnderUdta bool  // Чи ми в "udta" (user data)?
}
```

### 🔍 Навіщо це потрібно?

**Приклад**: Бокс `data` може означати:

| Контекст | Інтерпретація `data` |
|----------|---------------------|
| `UnderIlstMeta=true` | 📝 UTF-8 текст метаданих (назва, автор) |
| `UnderUdta=true` | 🔤 3GPP-рядок з кодом мови |
| `UnderWave=true` | 🎵 Аудіо-семпли у хвильовому форматі |
| `false` (за замовчуванням) | 🔢 Сирі байти для подальшого парсингу |

**Без `Context`** парсер не зможе відрізнити текст від бінарних даних!

---

## 📋 `BoxInfo`: Паспорт кожного боксу

```go
type BoxInfo struct {
	Offset     uint64  // Зміщення боксу у файлі (байти)
	Size       uint64  // Повний розмір боксу (заголовок + дані)
	HeaderSize uint64  // Розмір заголовка (8 або 16 байт)
	Type       BoxType // Тип боксу: "moof", "traf", "trun"...
	ExtendToEOF bool   // Чи бокс тягнеться до кінця файлу?
	Context           // ← вбудований контекст!
}
```

### 📐 Розмір заголовка: 8 vs 16 байт

```
🔹 Small header (8 байт) — для боксів < 4 ГБ:
   [4B: size][4B: type]

🔹 Large header (16 байт) — для боксів ≥ 4 ГБ:
   [4B: 1][4B: type][8B: size64]
   ↑
   size=1 означає "читай 64-бітний розмір далі"
```

---

## ⚙️ Функції роботи з `BoxInfo`

### 🔹 `EncodeBoxInfo(bi *BoxInfo) []byte` — серіалізація заголовка

```go
// Перетворює BoxInfo → []byte для запису у файл
func EncodeBoxInfo(bi *BoxInfo) []byte {
	var data []byte
	
	// Випадок 1: бокс до кінця файлу (ExtendToEOF)
	if bi.ExtendToEOF {
		data = make([]byte, 8)  // size=0, type
	}
	// Випадок 2: малий заголовок (розмір < 4 ГБ)
	else if bi.Size <= math.MaxUint32 && bi.HeaderSize != 16 {
		data = make([]byte, 8)
		binary.BigEndian.PutUint32(data, uint32(bi.Size))
	}
	// Випадок 3: великий заголовок (розмір ≥ 4 ГБ)
	else {
		data = make([]byte, 16)
		binary.BigEndian.PutUint32(data, 1)  // маркер "читай далі"
		binary.BigEndian.PutUint64(data[8:], bi.Size)  // 64-бітний розмір
	}
	
	// Запис типу боксу (4 ASCII-символи)
	data[4] = bi.Type[0]
	data[5] = bi.Type[1]
	data[6] = bi.Type[2]
	data[7] = bi.Type[3]
	
	return data
}
```

> 🎯 **Навіщо?** Ви не думаєте про бінарний формат — просто заповнюєте `BoxInfo`, а функція сама обирає правильний encoding.

---

### 🔹 `WriteBoxInfo(w io.WriteSeeker, bi *BoxInfo)` — запис у файл

```go
func WriteBoxInfo(w io.WriteSeeker, bi *BoxInfo) (*BoxInfo, error) {
	// 1. Дізнаємось поточну позицію у файлі
	offset, _ := w.Seek(0, io.SeekCurrent)
	
	// 2. Серіалізуємо заголовок
	data := EncodeBoxInfo(bi)
	w.Write(data)
	
	// 3. Повертаємо ОНОВЛЕНИЙ BoxInfo з реальними значеннями
	return &BoxInfo{
		Offset:      uint64(offset),           // реальне зміщення
		Size:        bi.Size - bi.HeaderSize + uint64(len(data)), // скоригований розмір
		HeaderSize:  uint64(len(data)),        // 8 або 16
		Type:        bi.Type,
		ExtendToEOF: bi.ExtendToEOF,
	}, nil
}
```

> ✅ **Ключова фішка**: Функція **автоматично коригує** `Size` та `HeaderSize`, тому вам не треба вручну рахувати байти.

---

### 🔹 `ReadBoxInfo(r io.ReadSeeker)` — читання з файлу

```go
func ReadBoxInfo(r io.ReadSeeker) (*BoxInfo, error) {
	offset, _ := r.Seek(0, io.SeekCurrent)  // запам'ятали позицію
	bi := &BoxInfo{Offset: uint64(offset)}
	
	// Крок 1: читаємо 8 байт (мінімальний заголовок)
	buf := make([]byte, 8)
	r.Read(buf)
	bi.HeaderSize = 8
	
	// Крок 2: парсимо size та type
	bi.Size = uint64(binary.BigEndian.Uint32(buf[0:4]))
	bi.Type = BoxType{buf[4], buf[5], buf[6], buf[7]}
	
	// Крок 3: обробляємо special cases
	if bi.Size == 0 {
		// 🎯 Бокс тягнеться до кінця файлу (останній бокс)
		eof, _ := r.Seek(0, io.SeekEnd)
		bi.Size = uint64(eof) - bi.Offset
		bi.ExtendToEOF = true
		bi.SeekToPayload(r)  // позиціонуємось на дані
	}
	
	if bi.Size == 1 {
		// 🎯 Великий заголовок: читаємо ще 8 байт для 64-бітного size
		buf64 := make([]byte, 8)
		r.Read(buf64)
		bi.HeaderSize = 16
		bi.Size = binary.BigEndian.Uint64(buf64)
	}
	
	return bi, nil
}
```

> 🎯 **Навіщо це?** Ви читаєте **будь-який** бокс, не знаючи наперед його тип чи розмір.

---

### 🔹 Методи навігації: `SeekToStart/Payload/End`

```go
// Перейти на початок боксу (заголовок)
func (bi *BoxInfo) SeekToStart(s io.Seeker) (int64, error) {
	return s.Seek(int64(bi.Offset), io.SeekStart)
}

// Перейти на початок ДАНИХ боксу (після заголовка)
func (bi *BoxInfo) SeekToPayload(s io.Seeker) (int64, error) {
	return s.Seek(int64(bi.Offset+bi.HeaderSize), io.SeekStart)
}

// Перейти в кінець боксу
func (bi *BoxInfo) SeekToEnd(s io.Seeker) (int64, error) {
	return s.Seek(int64(bi.Offset+bi.Size), io.SeekStart)
}
```

> 🎯 **Навіщо?** Після `ReadBoxInfo` ви знаходитесь **після заголовка**. Щоб прочитати дані — викликайте `SeekToPayload`.

---

## 🛠️ Практичні приклади для вашого HLS-процесора

### 🔹 Приклад 1: Рекурсивний парсинг fMP4-сегмента

```go
import "github.com/abema/go-mp4"

func parseFragment(f *os.File) error {
	// ReadBoxStructure автоматично:
	// 1. Викликає ReadBoxInfo для кожного боксу
	// 2. Передає вам BoxInfo + контекст
	// 3. Рекурсивно заходить у вкладені бокси
	_, err := mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		bi := h.BoxInfo
		
		// Логування для дебагу
		indent := strings.Repeat("  ", h.Depth)
		log.Printf("%s📦 %s @ offset=%d, size=%d, header=%d", 
			indent, bi.Type.String(), bi.Offset, bi.Size, bi.HeaderSize)
		
		switch bi.Type {
		case mp4.BoxTypeMoof():  // Movie Fragment
			log.Printf("  → Це fMP4-фрагмент!")
			
		case mp4.BoxTypeTrun():  // Track Fragment Run → таймстемпи!
			// Позиціонуємось на дані боксу
			bi.SeekToPayload(f)
			
			// Читаємо trun структуру
			trun := &mp4.Trun{}
			h.ReadPayload(trun)
			
			log.Printf("  → %d семплів, first-flags=0x%x", 
				trun.SampleCount, trun.FirstSampleFlags)
			// trun.Samples містить PTS/DTS для кожного кадру!
			
		case mp4.BoxTypeMdat():  // Media Data → сирі відео/аудіо
			// Не парсимо mdat вручну — це просто байти
			// Але можемо перевірити розмір для валідації
			if bi.Size > 100*1024*1024 { // >100MB?
				log.Printf("⚠️  Великий mdat: %d байт", bi.Size)
			}
		}
		
		return nil, nil  // продовжуємо парсинг
	})
	
	return err
}
```

---

### 🔹 Приклад 2: Створення нового fMP4-сегмента

```go
func writeFragment(seq int, payload []byte) error {
	f, _ := os.Create(fmt.Sprintf("seg_%06d.m4s", seq))
	defer f.Close()
	
	// 1. Створити BoxInfo для moof
	moofInfo := &mp4.BoxInfo{
		Type: mp4.BoxTypeMoof(),
		Size: 1024,  // тимчасове значення, буде перераховано
	}
	
	// 2. Записати заголовок (WriteBoxInfo сам обчислить правильний розмір!)
	updated, err := mp4.WriteBoxInfo(f, moofInfo)
	if err != nil { return err }
	
	log.Printf("📝 Записано moof: offset=%d, header=%d байт", 
		updated.Offset, updated.HeaderSize)
	
	// 3. Записати дані (наприклад, вкладені бокси traf/trun)
	// ... ваш код для запису вкладених боксів ...
	
	// 4. Записати mdat з сирими даними
	mdatInfo := &mp4.BoxInfo{
		Type: mp4.BoxTypeMdat(),
		Size: uint64(len(payload)) + 8,  // +8 для заголовка
	}
	mp4.WriteBoxInfo(f, mdatInfo)
	f.Write(payload)  // сирі відео/аудіо байти
	
	return nil
}
```

> ✅ **Магія**: `WriteBoxInfo` автоматично:
> - Обирає 8- або 16-байтний заголовок
> - Коригує `Size` з урахуванням реального розміру заголовка
> - Повертає оновлений `BoxInfo` для подальшого використання

---

### 🔹 Приклад 3: Обробка `ExtendToEOF` (останній бокс у файлі)

```go
func handleLastBox(r io.ReadSeeker) error {
	bi, err := mp4.ReadBoxInfo(r)
	if err != nil { return err }
	
	if bi.ExtendToEOF {
		// 🎯 Це останній бокс у файлі — його розмір = до кінця файлу
		log.Printf("🔚 Останній бокс %s: size=%d (до EOF)", 
			bi.Type, bi.Size)
		
		// Читати дані до кінця файлу
		data := make([]byte, bi.Size - bi.HeaderSize)
		r.Read(data)
		
		// Обробити data...
	}
	
	return nil
}
```

> 🎯 **Коли це важливо?** У HLS-сегментах останній `mdat` часто має `size=0`, тому що стрімінг може обірватися.

---

## 🔍 Як `Context` передається при парсингу

```
📁 Файл: segment.m4s
│
▼ ReadBoxStructure(f, callback)
│
├── 📦 ftyp → Context{IsQuickTimeCompatible: false}
│   ▼ callback(bi, ctx)
│
├── 📦 moov → Context{...}
│   ├── 📦 trak → Context{...}
│   │   ├── 📦 mdia → Context{...}
│   │   │   ├── 📦 minf → Context{...}
│   │   │   │   ├── 📦 stbl → Context{...}
│   │   │   │   │   ├── 📦 stts → Context{...}
│   │   │   │   │   └── 📦 stss → Context{...}  ← ключові кадри!
│
├── 📦 moof → Context{...}
│   ├── 📦 traf → Context{...}
│   │   ├── 📦 tfhd → Context{...}
│   │   └── 📦 trun → Context{...}  ← таймстемпи кадрів!
│
└── 📦 mdat → Context{...}  ← сирі дані
```

> 🎯 **Ключове**: `Context` **автоматично оновлюється** при вході/виході з боксів. Вам не треба керувати ним вручну!

---

## 🧪 Тестування: Перевірка заголовків

```go
func TestBoxInfoEncoding(t *testing.T) {
	// Тест 1: малий заголовок (< 4 ГБ)
	bi := &mp4.BoxInfo{
		Size: 1024,
		Type: mp4.StrToBoxType("test"),
	}
	data := mp4.EncodeBoxInfo(bi)
	
	assert.Equal(t, uint8(0), data[0])  // size=1024 = 0x00000400
	assert.Equal(t, uint8(4), data[1])
	assert.Equal(t, byte('t'), data[4]) // type[0]
	assert.Equal(t, byte('e'), data[5]) // type[1]
	
	// Тест 2: великий заголовок (≥ 4 ГБ)
	biLarge := &mp4.BoxInfo{
		Size: 5_000_000_000,  // > 2^32
		Type: mp4.StrToBoxType("huge"),
	}
	dataLarge := mp4.EncodeBoxInfo(biLarge)
	
	assert.Equal(t, uint32(1), binary.BigEndian.Uint32(dataLarge[0:4])) // маркер
	assert.Equal(t, uint64(5_000_000_000), binary.BigEndian.Uint64(dataLarge[8:16]))
}
```

---

## ⚠️ Поширені помилки

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Не викликати `SeekToPayload` після `ReadBoxInfo` | Читаєте заголовок замість даних | Завжди `bi.SeekToPayload(r)` перед читанням даних |
| Ігнорувати `ExtendToEOF` | Помилка при читанні останнього боксу | Перевіряйте `if bi.ExtendToEOF` перед `Seek()` |
| Неправильний `Size` при записі | Файл пошкоджений, плеєр не читає | Дозвольте `WriteBoxInfo` самому коригувати розмір |
| Не передавати `Context` у вкладені бокси | Неправильна інтерпретація `data` | Використовуйте `ReadBoxStructure`, який робить це автоматично |

---

## 📋 Чекліст для вашого проекту

```
[ ] Для парсингу: використовуйте mp4.ReadBoxStructure(f, callback)
    • Не викликайте ReadBoxInfo вручну — це робить бібліотека
    • У callback перевіряйте bi.Type для фільтрації потрібних боксів

[ ] Для запису: 
    1. Створіть BoxInfo з типом та приблизним розміром
    2. Викличіть mp4.WriteBoxInfo(w, bi) — отримаєте оновлений bi
    3. Запишіть дані (вкладені бокси або сирі байти)

[ ] Для навігації:
    • bi.SeekToPayload(r) — перейти до даних боксу
    • bi.SeekToEnd(r) — пропустити бокс, якщо не потрібен

[ ] Для дебагу:
    • Логуйте bi.Offset, bi.Size, bi.HeaderSize
    • Перевіряйте bi.ExtendToEOF для останніх боксів

[ ] Для контексту:
    • Не змінюйте ctx вручну — бібліотека робить це автоматично
    • Використовуйте ctx.UnderUdta, ctx.UnderIlst для умовної логіки
```

---

## 🎯 Висновок

> **`BoxInfo` + `Context` = ваш навігатор по лабіринту MP4**.  
> Вони беруть на себе:
> • 📐 Обчислення розмірів заголовків (8 vs 16 байт)
> • 🗺️ Відстеження позиції у файлі (`Offset`)
> • 🧭 Контекстну інтерпретацію даних (`Context`)
> • 🔄 Обробку крайніх випадків (`ExtendToEOF`)

Для вашого **CCTV HLS Processor** це означає:
- ✅ Коректне читання `trun` для синхронізації таймстемпів
- ✅ Безпечний запис нових fMP4-сегментів без пошкодження формату
- ✅ Фільтрація метаданих (`udta`, `ilst`) для додавання субтитрів

Потребуєте допомоги з парсингом конкретного боксу (`trun` для PTS, `stss` для ключових кадрів) або з модифікацією `mdat` для вставки субтитрів? Напишіть — покажу готовий код! 🎬🚀