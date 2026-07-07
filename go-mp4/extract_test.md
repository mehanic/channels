# 🧪 Тести утиліт пошуку боксів: `ExtractBox*` та `TestExtractDescendantBox`

Це **комплексний тест-сьют** для бібліотеки `go-mp4`, який перевіряє коректність роботи **функцій пошуку та витягування боксів** за шляхом (path-based navigation) у MP4-файлах.

---

## 🎯 Коротка відповідь

> **Ці тести гарантують, що ви можете швидко знайти будь-який бокс у складній ієрархії MP4** (напр. `moov → trak → mdia → hdlr`) без необхідності вручну ітерувати весь файл — критично для швидкої валідації, індексації та обробки fMP4-сегментів.

---

## 📋 Огляд тестових функцій

### 🔹 `TestExtractBoxWithPayload` — пошук з парсингом вмісту

```go
func TestExtractBoxWithPayload(t *testing.T) {
    // 🔹 Тестує: ExtractBoxWithPayload(r, parent, path)
    // Повертає: []*BoxInfoWithPayload {Info: метадані, Payload: розпаршена структура}
}
```

**📊 Тест-кейси:**

| Назва | Шлях | Очікуваний результат | Призначення |
|-------|------|---------------------|-------------|
| `empty box path` | `[]` | ❌ Помилка | Валідація: порожній шлях не дозволений |
| `invalid box path` | `{udta}` | `[]` (порожньо) | Бокс `udta` відсутній у тестовому файлі |
| `top level` | `{moov}` | ✅ 1 результат: `Moov{}` | Пошук боксу верхнього рівня |
| `multi hit` | `{moov, trak, mdia, hdlr}` | ✅ 2 результати: відео + аудіо `hdlr` | Знайти всі `hdlr` у різних треках |
| `multi hit (any)` | `{moov, trak, mdia, ANY}` | ✅ 6 результатів: всі нащадки `mdia` | Wildcard-пошук: `BoxTypeAny()` |

**🎯 Ключова перевірка**: Чи коректно парситься `Payload` і чи співпадають `Info` (offset, size, type).

---

### 🔹 `TestExtractBox` — пошук тільки метаданих

```go
func TestExtractBox(t *testing.T) {
    // 🔹 Тестує: ExtractBox(r, parent, path)
    // Повертає: []*BoxInfo (тільки метадані, без парсингу вмісту)
}
```

**📊 Тест-кейси:**

| Назва | Шлях | Очікуваний результат | Призначення |
|-------|------|---------------------|-------------|
| `empty box path` | `[]` | ❌ Помилка | Валідація вхідних даних |
| `invalid box path` | `{udta}` | `[]` | Бокс не знайдено — не помилка |
| `top level` | `{moov}` | ✅ 1 `BoxInfo` з офсетом/розміром | Швидкий доступ до метаданих |
| `multi hit` | `{moov, trak, tkhd}` | ✅ 2 `BoxInfo` (відео + аудіо треки) | Пошук кількох екземплярів |
| `any type` | `{moov, trak, ANY}` | ✅ 6 `BoxInfo` (всі нащадки `trak`) | Wildcard для індексації |

**🎯 Ключова перевірка**: Чи коректні `Offset`, `Size`, `HeaderSize`, `Type` без витрат на парсинг вмісту.

---

### 🔹 `TestExtractBoxes` — пошук за кількома шляхами одночасно

```go
func TestExtractBoxes(t *testing.T) {
    // 🔹 Тестує: ExtractBoxes(r, parent, []BoxPath)
    // Повертає: []*BoxInfo для всіх шляхів в одному проході
}
```

**📊 Тест-кейси:**

| Назва | Шляхи | Очікуваний результат | Призначення |
|-------|-------|---------------------|-------------|
| `empty path list` | `[]` | `[]` (без помилки) | Порожній список — допустимо |
| `contains empty path` | `[{moov}, {}]` | ❌ Помилка | Порожній шлях у списку — помилка |
| `single path` | `[{moov, udta}]` | ✅ 1 `BoxInfo` | Один шлях — як `ExtractBox` |
| `multi path` | `[{moov}, {moov, udta}]` | ✅ 2 `BoxInfo` (moov + udta) | Економія: один прохід файлу для кількох цілей |
| `multi hit` | `[{moov, trak, tkhd}]` | ✅ 2 `BoxInfo` | Кілька екземплярів одного шляху |

**🎯 Ключова перевага**: `ExtractBoxes` робить **один прохід файлу** для пошуку за кількома шляхами — ефективніше, ніж викликати `ExtractBox` кілька разів.

---

### 🔹 `TestExtractDescendantBox` — пошук з обмеженням по батьківському боксу

```go
func TestExtractDescendantBox(t *testing.T) {
    // 🔹 Тестує: пошук з параметром `parent` для обмеження області пошуку
}
```

**🔢 Логіка тесту:**
```go
// 1. Знайти moov у корені файлу
boxes, _ := ExtractBox(f, nil, BoxPath{BoxTypeMoov()})  // ✅ 1 результат

// 2. Шукати нащадків ТІЛЬКИ всередині знайденого moov
descs, _ := ExtractBox(f, boxes[0], BoxPath{BoxTypeTrak(), BoxTypeMdia()})
// ✅ 2 результати: mdia у відео-треку + mdia у аудіо-треку
```

**🎯 Призначення**: Дозволяє **обмежити пошук конкретною гілкою ієрархії**, що:
- ⚡ Прискорює пошук (не сканує весь файл)
- 🔍 Запобігає хибним збігам (напр. `trak` в іншому контексті)
- 🧩 Дозволяє модульну обробку (спочатку знайти трек, потім його вміст)

---

## 🔍 Детальний розбір ключових концепцій

### 🔹 `BoxPath` — шлях до боксу

```go
type BoxPath []BoxType

// 🔹 Приклади:
BoxPath{BoxTypeMoov()}                          // moov на корені
BoxPath{BoxTypeMoov(), BoxTypeTrak()}           // moov → trak
BoxPath{BoxTypeMoov(), BoxTypeTrak(), BoxTypeAny()}  // moov → trak → * (всі нащадки)
```

> 🎯 **Важливо**: Шлях вказується **від кореня до цілі** (або від `parent` до цілі).

---

### 🔹 `BoxTypeAny()` — wildcard для пошуку

```go
BoxPath{BoxTypeMoov(), BoxTypeTrak(), BoxTypeAny()}
```

**🎯 Призначення**: Знайти **всі бокси**, що є безпосередніми нащадками `trak` всередині `moov`.

**Приклад результату**:
```
📁 moov
└── 📁 trak (відео)
    ├── 📦 tkhd ✅
    ├── 📦 edts ✅
    ├── 📁 mdia ✅
    └── ...
└── 📁 trak (аудіо)
    ├── 📦 tkhd ✅
    ├── 📦 edts ✅
    ├── 📁 mdia ✅
    └── ...
```
→ Повертає 6 боксів: `tkhd`, `edts`, `mdia` для кожного треку.

---

### 🔹 `BoxInfoWithPayload` — контейнер результату

```go
type BoxInfoWithPayload struct {
    Info    BoxInfo  // 🔹 Метадані: Offset, Size, HeaderSize, Type, Context
    Payload IBox     // 🔹 Розпаршена структура: *Trun, *Mdhd, *Hdlr...
}
```

**🎯 Приклад використання**:
```go
results, _ := ExtractBoxWithPayload(f, nil, BoxPath{"moov", "trak", "mdia", "hdlr"})

for _, r := range results {
    // 🔹 Доступ до метаданих:
    log.Printf("📦 %s @ offset=%d, size=%d", 
        r.Info.Type, r.Info.Offset, r.Info.Size)
    
    // 🔹 Доступ до розпаршених даних:
    if hdlr, ok := r.Payload.(*mp4.Hdlr); ok {
        log.Printf("🔤 Handler: %s, name=%q", 
            string(hdlr.HandlerType[:]), hdlr.Name)
    }
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Швидка валідація структури fMP4-сегмента

```go
func validateFragment(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 Перевірка: чи є moof (фрагментований формат)?
    moofs, err := mp4.ExtractBox(f, nil, mp4.BoxPath{mp4.BoxTypeMoof()})
    if err != nil { return fmt.Errorf("error reading moof: %w", err) }
    if len(moofs) == 0 { return fmt.Errorf("not a fragmented MP4: missing moof") }
    
    // 🔹 Перевірка: чи є trun з таймстемпами?
    truns, err := mp4.ExtractBoxWithPayload(f, nil, 
        mp4.BoxPath{mp4.BoxTypeMoof(), mp4.BoxTypeTraf(), mp4.BoxTypeTrun()})
    if err != nil { return fmt.Errorf("error reading trun: %w", err) }
    if len(truns) == 0 { return fmt.Errorf("missing trun: no frame timestamps") }
    
    // 🔹 Валідація таймстемпів
    for _, tr := range truns {
        if trun, ok := tr.Payload.(*mp4.Trun); ok {
            if trun.SampleCount == 0 {
                return fmt.Errorf("trun has zero samples")
            }
            // 🔹 Додаткові перевірки...
        }
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Отримання конфігурації кодека для всіх треків

```go
func getCodecConfigs(filePath string) (map[uint32]*CodecInfo, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    configs := make(map[uint32]*CodecInfo)
    
    // 🔹 Шляхи до різних типів кодеків
    codecPaths := []mp4.BoxPath{
        {mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), 
         mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeAvcC()},
        {mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), 
         mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeHvcC()},
        {mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), 
         mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeAv1C()},
    }
    
    results, err := mp4.ExtractBoxesWithPayload(f, nil, codecPaths)
    if err != nil { return nil, err }
    
    for _, r := range results {
        // 🔹 Визначаємо trackID з контексту (спрощено)
        trackID := extractTrackIDFromPath(r.Info.Path)
        
        switch payload := r.Payload.(type) {
        case *mp4.AVCDecoderConfiguration:
            configs[trackID] = &CodecInfo{
                Type: "avc1",
                Profile: payload.Profile,
                Level: payload.Level,
            }
        case *mp4.HvcC:
            configs[trackID] = &CodecInfo{
                Type: "hvc1",
                Profile: payload.GeneralProfileIdc,
                Level: payload.GeneralLevelIdc,
            }
        case *mp4.Av1C:
            configs[trackID] = &CodecInfo{
                Type: "av01",
                Profile: payload.SeqProfile,
                Level: payload.SeqLevelIdx0,
            }
        }
    }
    
    return configs, nil
}

type CodecInfo struct {
    Type    string
    Profile uint8
    Level   uint8
}
```

---

### 🔹 Приклад 3: Індексація сегментів для швидкого seek

```go
type SegmentIndex struct {
    Offset uint64
    Size   uint64
    Duration uint64
    KeyFrames []uint32  // номери семплів
}

func buildSegmentIndex(filePath string) ([]SegmentIndex, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    var indexes []SegmentIndex
    
    // 🔹 Шукаємо всі sidx бокси (індекси сегментів)
    sidxs, err := mp4.ExtractBoxesWithPayload(f, nil, 
        []mp4.BoxPath{{mp4.BoxTypeSidx()}})
    if err != nil { return nil, err }
    
    for _, sidxResult := range sidxs {
        if sidx, ok := sidxResult.Payload.(*mp4.Sidx); ok {
            idx := SegmentIndex{
                Offset: sidxResult.Info.Offset,
                Size:   sidxResult.Info.Size,
                Duration: sidx.GetEarliestPresentationTime(), // спрощено
            }
            
            // 🔹 Додати ключові кадри зі stss (опціонально)
            stssResults, _ := mp4.ExtractBoxWithPayload(f, &sidxResult.Info,
                []mp4.BoxPath{{mp4.BoxTypeStss()}})
            if len(stssResults) > 0 {
                if stss, ok := stssResults[0].Payload.(*mp4.Stss); ok {
                    idx.KeyFrames = stss.SampleNumber
                }
            }
            
            indexes = append(indexes, idx)
        }
    }
    
    return indexes, nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний порядок у `BoxPath` | Функція не знаходить бокси → порожній результат | Завжди вказуйте шлях від кореня до цілі: `{"moov", "trak", "mdia"}` |
| Використання `ExtractBox` замість `*WithPayload` | Отримуєте тільки метадані, не можете доступитися до полів | Якщо потрібен доступ до полів — використовуйте `ExtractBoxWithPayload` |
| Ігнорування `parent` параметра | Пошук починається з кореня файлу замість вкладеного боксу | Передавайте `parent` для обмеження пошуку конкретною гілкою |
| Не перевіряти тип `Payload` через type assertion | `panic` при неправильному приведенні типу | Завжди: `if box, ok := payload.(*mp4.Trun); ok { ... }` |
| Використання `BoxTypeAny()` без потреби | Повертається забагато результатів → зайва обробка | Використовуйте `Any` тільки коли дійсно потрібні всі нащадки |

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

## 🎯 Висновок

> **Ці тести — ваш "золотий стандарт" для надійного пошуку боксів у MP4**.  
> Вони гарантують:
> • ✅ Коректну обробку порожніх/невалідних шляхів
> • ✅ Точний пошук за ієрархією (від кореня до нащадків)
> • ✅ Ефективну підтримку wildcard (`BoxTypeAny()`)
> • ✅ Безпечне обмеження пошуку через `parent` контекст
> • ✅ Надійну роботу з кількома шляхами одночасно (`ExtractBoxes`)

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Швидка валідація структури fMP4-сегментів при прийомі через WebSocket
- 🔍 Точний доступ до таймстемпів (`trun`), конфігурацій кодеків (`avcC`/`hvcC`/`Av1C`), метаданих
- 🔄 Гнучкість: легко додавати нові типи боксів для обробки без переписування парсерів
- 🛡️ Надійність: коректна обробка відсутніх, пошкоджених або нестандартних боксів

Потребуєте допомоги з інтеграцією цих утиліт у ваш конвеєр пошуку/обробки боксів? Напишіть — покажу готовий код для вашого сценарію! 🚀🔍