# 🛠️ Утиліти для пошуку та витягування боксів у MP4

Це **допоміжний модуль** бібліотеки `go-mp4`, який надає зручний API для **пошуку та витягування конкретних боксів** з MP4-файлів за шляхом (path) — без необхідності вручну ітерувати всю ієрархію.

---

## 🎯 Коротка відповідь

> **Це "пошуковий двигун" для MP4-файлів**: ви вказуєте шлях до боксу (напр. `["moov", "trak", "mdia"]`), а функція знаходить його, парсить вміст і повертає готову структуру — ідеально для швидкого доступу до метаданих, таймстемпів, конфігурацій кодеків.

---

## 🧱 Основні типи та функції

### 🔹 `BoxInfoWithPayload` — контейнер для результату пошуку

```go
type BoxInfoWithPayload struct {
	Info    BoxInfo  // 🔹 Метадані боксу: тип, розмір, зміщення, контекст
	Payload IBox     // 🔹 Розпаршений вміст: конкретна структура (Trun, Mdhd, Av1C...)
}
```

**🎯 Призначення**: Зручний спосіб отримати **і метадані, і дані** боксу одночасно.

**Приклад використання**:
```go
results, _ := ExtractBoxWithPayload(f, nil, mp4.BoxPath{"moov", "trak", "mdia", "minf", "stbl", "stts"})

for _, r := range results {
	log.Printf("📦 Знайдено stts: offset=%d, size=%d", r.Info.Offset, r.Info.Size)
	
	// 🔹 Доступ до розпаршених даних:
	if stts, ok := r.Payload.(*mp4.Stts); ok {
		log.Printf("⏱️  EntryCount=%d, перший запис: count=%d, delta=%d", 
			stts.EntryCount, stts.Entries[0].SampleCount, stts.Entries[0].SampleDelta)
	}
}
```

---

### 🔹 `ExtractBoxWithPayload` / `ExtractBoxesWithPayload` — пошук з парсингом

```go
// 🔹 Знайти один бокс за шляхом + розпарсити його вміст
func ExtractBoxWithPayload(r io.ReadSeeker, parent *BoxInfo, path BoxPath) ([]*BoxInfoWithPayload, error)

// 🔹 Знайти кілька боксів за кількома шляхами + розпарсити вміст
func ExtractBoxesWithPayload(r io.ReadSeeker, parent *BoxInfo, paths []BoxPath) ([]*BoxInfoWithPayload, error)
```

**📐 Параметри:**
| Параметр | Тип | Опис |
|----------|-----|------|
| `r` | `io.ReadSeeker` | 🔹 Вхідний потік (файл, буфер, мережа) |
| `parent` | `*BoxInfo` | 🔹 Батьківський бокс для пошуку (або `nil` для кореня) |
| `path` / `paths` | `BoxPath` / `[]BoxPath` | 🔹 Шлях до боксу: `[]BoxType{"moov", "trak", "mdia"}` |

**🔙 Повертає**: `[]*BoxInfoWithPayload` — масив знайдених боксів з розпаршеним вмістом.

---

### 🔹 `ExtractBox` / `ExtractBoxes` — пошук тільки метаданих

```go
// 🔹 Знайти один бокс за шляхом (тільки метадані, без парсингу вмісту)
func ExtractBox(r io.ReadSeeker, parent *BoxInfo, path BoxPath) ([]*BoxInfo, error)

// 🔹 Знайти кілька боксів за кількома шляхами (тільки метадані)
func ExtractBoxes(r io.ReadSeeker, parent *BoxInfo, paths []BoxPath) ([]*BoxInfo, error)
```

**🎯 Коли використовувати?**
- ✅ Коли вам потрібні **тільки офсети/розміри** боксів (напр. для швидкого індексування)
- ✅ Коли ви хочете **відкласти парсинг** для оптимізації продуктивності
- ❌ Не використовуйте, якщо потрібен доступ до полів боксу — тоді беріть `*WithPayload` версію

---

### 🔹 `matchPath` — внутрішня логіка зіставлення шляхів

```go
func matchPath(paths []BoxPath, path BoxPath) (forwardMatch bool, match bool)
```

**🎯 Призначення**: Визначає, чи поточний шлях у файлі:
- `forwardMatch = true` → шлях є **префіксом** одного з шуканих → треба заглиблюватися далі
- `match = true` → шлях **точно співпадає** з одним з шуканих → знайдено ціль!

**🔢 Приклад:**
```
🔍 Шукаємо: ["moov", "trak", "mdia"]

📁 Поточний шлях у файлі:
• ["moov"] → forwardMatch=true, match=false → заглиблюємось у moov
• ["moov", "trak"] → forwardMatch=true, match=false → заглиблюємось у trak
• ["moov", "trak", "mdia"] → forwardMatch=false, match=true → ✅ знайдено!
• ["moov", "trak", "mdia", "minf"] → forwardMatch=false, match=false → ігноруємо
```

---

## 🔍 Як працює алгоритм пошуку (під капотом)

```
🔹 Крок 1: Виклик ReadBoxStructure / ReadBoxStructureFromInternal
   │
   ▼
🔹 Крок 2: Для кожного боксу у файлі:
   │
   ├── 🔹 Побудова поточного шляху: handle.Path
   │
   ├── 🔹 Якщо вказано parent → обрізаємо перший елемент шляху
   │
   ├── 🔹 Виклик matchPath(paths, currentPath):
   │   ├── ✅ forwardMatch=true → рекурсивно заглиблюємось (handle.Expand())
   │   ├── ✅ match=true → додаємо бокс у результат
   │   └── ❌ нічого не співпадає → пропускаємо бокс
   │
   ▼
🔹 Крок 3: Для *WithPayload версій:
   │
   ├── 🔹 bi.SeekToPayload(r) → перехід на початок даних боксу
   │
   ├── 🔹 UnmarshalAny(r, bi.Type, size, ctx) → парсинг вмісту
   │
   ├── 🔹 Створення BoxInfoWithPayload{Info: bi, Payload: box}
   │
   ▼
🔹 Крок 4: Повернення масиву результатів
```

> 🎯 **Ключова оптимізація**: Алгоритм **не читає весь файл** — він зупиняється, як тільки знаходить всі потрібні бокси (завдяки `forwardMatch` логіці).

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Швидке отримання таймстемпів з `trun`

```go
func getFrameTimestamps(filePath string) ([]FrameTimestamp, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	// 🔹 Шукаємо всі trun бокси всередині moof → traf
	results, err := mp4.ExtractBoxesWithPayload(f, nil, 
		[]mp4.BoxPath{
			{mp4.BoxTypeMoof(), mp4.BoxTypeTraf(), mp4.BoxTypeTrun()},
		})
	if err != nil { return nil, err }
	
	var timestamps []FrameTimestamp
	
	for _, r := range results {
		trun, ok := r.Payload.(*mp4.Trun)
		if !ok { continue }
		
		baseTime := trun.DataOffset
		for i := uint32(0); i < trun.SampleCount; i++ {
			entry := trun.Entries[i]
			ctsOffset := trun.GetSampleCompositionTimeOffset(int(i))
			
			timestamps = append(timestamps, FrameTimestamp{
				Index:     int(i),
				Duration:  entry.SampleDuration,
				PTSOffset: ctsOffset,
				Size:      entry.SampleSize,
			})
		}
	}
	
	return timestamps, nil
}
```

---

### 🔹 Приклад 2: Отримання конфігурації кодека без повного парсингу

```go
func getCodecConfig(filePath string) (*CodecConfig, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	// 🔹 Шукаємо avcC/hvcC/Av1C всередині stsd
	paths := []mp4.BoxPath{
		{mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), 
		 mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeAvcC()},
		{mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), 
		 mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeHvcC()},
		{mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), 
		 mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeAv1C()},
	}
	
	results, err := mp4.ExtractBoxesWithPayload(f, nil, paths)
	if err != nil { return nil, err }
	
	for _, r := range results {
		switch payload := r.Payload.(type) {
		case *mp4.AVCDecoderConfiguration:
			return &CodecConfig{
				Type:    "avc1",
				Profile: payload.Profile,
				Level:   payload.Level,
			}, nil
		case *mp4.HvcC:
			return &CodecConfig{
				Type:    "hvc1",
				Profile: payload.GeneralProfileIdc,
				Level:   payload.GeneralLevelIdc,
			}, nil
		case *mp4.Av1C:
			return &CodecConfig{
				Type:    "av01",
				Profile: payload.SeqProfile,
				Level:   payload.SeqLevelIdx0,
			}, nil
		}
	}
	
	return nil, fmt.Errorf("codec configuration not found")
}

type CodecConfig struct {
	Type    string
	Profile uint8
	Level   uint8
}
```

---

### 🔹 Приклад 3: Оптимізований пошук тільки метаданих (без парсингу)

```go
func indexSegments(filePath string) ([]SegmentInfo, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	// 🔹 Шукаємо тільки moof бокси (тільки метадані, без парсингу вмісту)
	// Це швидше, бо не викликаємо Unmarshal для кожного боксу
	moofs, err := mp4.ExtractBoxes(f, nil, []mp4.BoxPath{
		{mp4.BoxTypeMoof()},
	})
	if err != nil { return nil, err }
	
	var segments []SegmentInfo
	
	for _, moof := range moofs {
		// 🔹 Отримуємо тільки офсет та розмір — достатньо для індексації
		segments = append(segments, SegmentInfo{
			Offset: moof.Offset,
			Size:   moof.Size,
			// 🔹 Payload не парсимо — економимо час/пам'ять
		})
	}
	
	return segments, nil
}

type SegmentInfo struct {
	Offset uint64
	Size   uint64
	// Payload не потрібен для індексації
}
```

---

### 🔹 Приклад 4: Пошук з обмеженням по батьківському боксу

```go
func getTrackMetadata(filePath string, trackID uint32) (*TrackMetadata, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	// 🔹 Спочатку знаходимо потрібний trak за trackID
	traks, err := mp4.ExtractBoxesWithPayload(f, nil, 
		[]mp4.BoxPath{{mp4.BoxTypeMoov(), mp4.BoxTypeTrak()}})
	if err != nil { return nil, err }
	
	var targetTrak *mp4.BoxInfoWithPayload
	for _, t := range traks {
		if trak, ok := t.Payload.(*mp4.Trak); ok {
			// 🔹 Шукаємо tkhd всередині цього trak для отримання trackID
			tkhdResults, _ := mp4.ExtractBoxesWithPayload(f, &t.Info, 
				[]mp4.BoxPath{{mp4.BoxTypeTkhd()}})
			if len(tkhdResults) > 0 {
				if tkhd, ok := tkhdResults[0].Payload.(*mp4.Tkhd); ok {
					if tkhd.TrackID == trackID {
						targetTrak = t
						break
					}
				}
			}
		}
	}
	
	if targetTrak == nil {
		return nil, fmt.Errorf("track %d not found", trackID)
	}
	
	// 🔹 Тепер шукаємо mdhd тільки всередині знайденого trak
	mdhdResults, err := mp4.ExtractBoxesWithPayload(f, &targetTrak.Info,
		[]mp4.BoxPath{{mp4.BoxTypeMdia(), mp4.BoxTypeMdhd()}})
	if err != nil { return nil, err }
	
	if len(mdhdResults) == 0 {
		return nil, fmt.Errorf("mdhd not found in track %d", trackID)
	}
	
	mdhd := mdhdResults[0].Payload.(*mp4.Mdhd)
	
	return &TrackMetadata{
		TrackID:    trackID,
		Timescale:  mdhd.Timescale,
		Duration:   mdhd.GetDuration(),
		Language:   string(mdhd.Language[:]),
	}, nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `BoxPath` | Функція не знаходить бокси → порожній результат | Перевіряйте порядок типів: `{"moov", "trak", "mdia"}` — від кореня до цілі |
| Використання `ExtractBox` замість `*WithPayload` | Отримуєте тільки метадані, не можете доступитися до полів | Якщо потрібен доступ до полів — використовуйте `ExtractBoxWithPayload` |
| Ігнорування `parent` параметра | Пошук починається з кореня файлу замість вкладеного боксу | Передавайте `parent` для обмеження пошуку конкретною гілкою ієрархії |
| Неправильне оброблення `io.ReadSeeker` | Помилки позиціонування → зсув даних | Завжди використовуйте `SeekToPayload()` перед `UnmarshalAny` |
| Не перевіряти тип `Payload` через type assertion | `panic` при неправильному приведенні типу | Завжди: `if box, ok := payload.(*mp4.Trun); ok { ... }` |

---

## 📋 Чекліст для вашого проекту

```
[ ] Для пошуку боксів:
    • Використовуйте BoxPath від кореня до цілі: {"moov", "trak", "mdia", ...}
    • Для кількох можливих типів: передавайте масив paths у ExtractBoxes
    • Для вкладеного пошуку: передавайте parent для обмеження області

[ ] Для оптимізації продуктивності:
    • Використовуйте ExtractBox (без Payload), якщо потрібні тільки офсети/розміри
    • Уникайте повного парсингу файлу — алгоритм зупиняється після знаходження цілей
    • Кешуйте результати пошуку, якщо файл читається багаторазово

[ ] Для обробки результатів:
    • Завжди перевіряйте len(results) > 0 перед доступом до елементів
    • Використовуйте type assertion для доступу до конкретних полів: 
      if trun, ok := r.Payload.(*mp4.Trun); ok { ... }
    • Логувайте не знайдені бокси: log.Printf("⚠️  %s not found", boxType)

[ ] Для дебагу:
    • Логуйте шляхи пошуку: log.Printf("🔍 Searching: %v", path)
    • Виводьте знайдені бокси: log.Printf("✅ Found %s @ offset=%d", bi.Type, bi.Offset)
    • Перевіряйте контекст: log.Printf("📦 Context: UnderIlst=%v", ctx.UnderIlst)

[ ] Для тестування:
    • Створюйте тестові MP4-файли з відомою структурою
    • Перевіряйте, що ExtractBox знаходить правильні офсети
    • Тестуйте ExtractBoxWithPayload на коректність парсингу вмісту
```

---

## 🎯 Інтеграція у ваш CCTV HLS Processor

```
📡 Ваш потік обробки з пошуком боксів:
1. Приймаєте fMP4-сегмент через WebSocket
   │
   ▼
2. Швидка валідація структури:
   • ExtractBox(f, nil, {"moof"}) → чи є фрагмент?
   • ExtractBox(f, nil, {"moof", "traf", "trun"}) → чи є таймстемпи?
   │
   ▼
3. Отримання метаданих для обробки:
   • ExtractBoxWithPayload(...) → розпаршити trun для синхронізації
   • ExtractBoxWithPayload(...) → отримати av1C/hvcC для валідації кодека
   │
   ▼
4. Модифікація/аналіз:
   • Доступ до полів через payload: trun.Entries[i].SampleDuration
   • Логування/моніторинг на основі розпаршених даних
   │
   ▼
5. Генерація оновленого сегмента або передача клієнту ✅
```

---

## ❓ Часті питання

**Q: Чому `ExtractBoxes` повертає масив, а не один елемент?**  
A: У MP4-файлі може бути **кілька боксів одного типу** на різних рівнях ієрархії. Напр., кілька `trak` боксів для відео/аудіо/субтитрів. Масив дозволяє обробити всі знайдені екземпляри.

**Q: Чи можна шукати бокси за типом без повного шляху?**  
A: Так! Використовуйте `BoxPath{mp4.BoxTypeTrun()}` для пошуку всіх `trun` боксів у файлі, незалежно від їхнього розташування. Але це менш ефективно, ніж вказання повного шляху.

**Q: Як обробити помилку "box not found"?**  
```go
results, err := mp4.ExtractBox(f, nil, mp4.BoxPath{"moov", "trak", "mdia", "minf", "stbl", "stss"})
if err != nil {
    return fmt.Errorf("error searching for stss: %w", err)
}
if len(results) == 0 {
    // 🔹 Бокс не знайдено — це не помилка, а відсутність даних
    log.Printf("ℹ️  stss box not found — no keyframe list in this file")
    return nil, nil
}
// ✅ Бокс знайдено — обробляємо results[0]
```

**Q: Чи безпечно використовувати ці функції у конвеєрі реального часу?**  
A: ✅ Так, але з обережністю:
- Використовуйте `ExtractBox` (без Payload) для швидкої перевірки структури
- Уникайте повного парсингу великих боксів (напр. `mdat`) у реальному часі
- Кешуйте результати пошуку, якщо файл обробляється багаторазово

---

## 🎯 Висновок

> **Ці утиліти — ваш "швидкий доступ" до будь-якого боксу у MP4-файлі**.  
> Вони забезпечують:
> • ✅ Зручний API для пошуку за шляхом (path-based navigation)
> • ✅ Оптимізацію: зупинка після знаходження цілей, без читання всього файлу
> • ✅ Гнучкість: пошук тільки метаданих або з повним парсингом вмісту
> • ✅ Безпека: обробка помилок позиціонування та парсингу

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Швидка валідація структури fMP4-сегментів при прийомі
- 🔍 Точний доступ до таймстемпів, конфігурацій кодеків, метаданих
- 🔄 Гнучкість: легко додавати нові типи боксів для обробки
- 🛡️ Надійність: коректна обробка відсутніх або пошкоджених боксів

Потребуєте допомоги з інтеграцією цих утиліт у ваш конвеєр пошуку/обробки боксів? Напишіть — покажу готовий код для вашого сценарію! 🚀🔍