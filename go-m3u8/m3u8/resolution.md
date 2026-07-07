# 🔍 Глибокий розбір коду: `Resolution` для HLS PlaylistItem

Цей код реалізує структуру для роботи з **роздільною здатністю відео** у форматі `WxH` (наприклад, `1920x1080`), яка використовується в атрибуті `RESOLUTION` тега `#EXT-X-STREAM-INF`. Розберемо детально.

---

## 📦 Що таке `Resolution` і навіщо він потрібен?

### Контекст у HLS
```m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000,RESOLUTION=1920x1080,CODECS="avc1.640028"
video/1080p.m3u8
```

### Призначення
| Аспект | Пояснення |
|--------|-----------|
| **Формат** | Рядок `"ШиринаxВисота"` (наприклад, `"1280x720"`) |
| **Специфікація** | RFC 8216: `RESOLUTION=WidthxHeight` (обидва — десяткові цілі > 0) |
| **Використання** | Допомога плеєру у виборі варіанту: екран 720p → не завантажувати 4K |
| **Опціональність** | Атрибут не обов'язковий, але **рекомендований** для ABR |

### 🎯 Навіщо окремий тип `Resolution`?
```go
// ❌ Без типу:
type PlaylistItem struct {
    Resolution string  // "1920x1080" — але як валідувати? як отримати Width?
}

// ✅ З типом Resolution:
type Resolution struct {
    Width  int  // 1920 — зручно для порівнянь, обчислень
    Height int  // 1080
}
// • Type-safe: компілятор контролює типи
// • Зручні методи: .String(), .AspectRatio(), .Megapixels()
// • Легка валідація при парсингу
```

---

## 🏗️ Struct `Resolution` — мінімалізм і ефективність

```go
type Resolution struct {
    Width  int  // Ширина у пікселях (напр. 1920)
    Height int  // Висота у пікселях (напр. 1080)
}
```

### 🎯 Чому `int`, а не `*int`?
```go
// Resolution використовується ТІЛЬКИ коли атрибут присутній:
// • У PlaylistItem поле Resolution: *Resolution (pointer)
// • Якщо *Resolution == nil → атрибут відсутній у плейлисті
// • Якщо *Resolution != nil → значення Width/Height завжди валідні

// Це патерн "nullable struct":
// • Зовнішній шар (PlaylistItem) контролює наявність атрибута
// • Внутрішній шар (Resolution) контролює валідність значень
```

---

## 🔧 Метод `String()` — серіалізація у формат HLS

```go
func (r *Resolution) String() string {
    // 🎯 Захист від nil-паніки (хоча викликається через *Resolution)
    if r == nil {
        return ""
    }
    
    // 🎯 Формат специфікації: "WxH" без пробілів, без лапок
    return fmt.Sprintf("%dx%d", r.Width, r.Height)
}
```

### 🎯 Приклади виводу
```go
r := &Resolution{Width: 1920, Height: 1080}
fmt.Println(r.String())  // "1920x1080"

r2 := &Resolution{Width: 854, Height: 480}
fmt.Println(r2.String())  // "854x480"

var r3 *Resolution = nil
fmt.Println(r3.String())  // "" (безпечно, не панікує)
```

### ⚠️ Чому важливий `nil`-чек?
```go
// У PlaylistItem.String() може бути такий код:
if pi.Resolution != nil {
    // Але якщо хтось вручну встановить pi.Resolution = &Resolution{0, 0}?
    // Або якщо при парсингу сталася помилка?
    // → nil-чек у String() запобігає runtime panic
}
```

---

## 🔧 Конструктор `NewResolution` — парсинг з валідацією

```go
func NewResolution(text string) (*Resolution, error) {
    // Крок 1: Розбиття рядка за роздільником "x"
    // Вхід: "1920x1080" → values: ["1920", "1080"]
    // Вхід: "1920"      → values: ["1920"] → помилка!
    values := strings.Split(text, "x")
    if len(values) <= 1 {
        return nil, ErrResolutionInvalid  // ✅ Чітка помилка для невалідного формату
    }

    // Крок 2: Парсинг ширини
    // strconv.ParseInt з base=0: авто-визначення системи числення
    // "1920" → 1920 (decimal), "0x780" → 1920 (hex), "03600" → 1920 (octal)
    width, err := strconv.ParseInt(values[0], 0, 0)
    if err != nil {
        return nil, err  // ✅ Прокидаємо помилку парсингу вгору
    }

    // Крок 3: Парсинг висоти (аналогічно)
    height, err := strconv.ParseInt(values[1], 0, 0)
    if err != nil {
        return nil, err
    }

    // Крок 4: Побудова об'єкта з конвертацією int64 → int
    return &Resolution{
        Width:  int(width),   // ⚠️ Може бути втрата даних, якщо width > max(int)
        Height: int(height),
    }, nil
}
```

### 🔍 Деталі `strconv.ParseInt(values[0], 0, 0)`
```go
// Підпис: ParseInt(s string, base int, bitSize int)
// • base=0 → авто-визначення:
//   "123"   → decimal (10)
//   "0x7B"  → hexadecimal (16)
//   "0173"  → octal (8)
// • bitSize=0 → результат типу int64

// ✅ Перевага: гнучкість вводу
// ❌ Ризик: користувач може випадково вказати "0x780" замість "1920"

// ✅ Рекомендація для продакшену: використовувати base=10 для суворості
width, err := strconv.ParseInt(values[0], 10, 0)  // Тільки десяткові числа
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність валідації діапазону значень
```go
// ❌ Поточний код приймає будь-які int:
r, _ := NewResolution("999999999x999999999")  // Нереальна роздільна здатність
r2, _ := NewResolution("0x0")                   // Нульова роздільна здатність
r3, _ := NewResolution("-1920x-1080")           // Від'ємні значення!

// ✅ Додати валідацію після парсингу:
func NewResolution(text string) (*Resolution, error) {
    // ... парсинг ...
    
    // ✅ Валідація: роздільна здатність має бути розумною
    const (
        MinResolution = 160      // Мінімум: 160x120 (QCIF)
        MaxResolution = 16384    // Максимум: 16K (16384x16384)
    )
    
    if width < MinResolution || width > MaxResolution {
        return nil, fmt.Errorf("invalid width %d: must be [%d, %d]", 
            width, MinResolution, MaxResolution)
    }
    if height < MinResolution || height > MaxResolution {
        return nil, fmt.Errorf("invalid height %d: must be [%d, %d]", 
            height, MinResolution, MaxResolution)
    }
    
    return &Resolution{Width: int(width), Height: int(height)}, nil
}
```

### 2️⃣ Втрата точності при конвертації `int64 → int`
```go
// ❌ На 32-бітних системах:
// int64(3000000000) → int(3000000000) = переповнення!

// ✅ Безпечна конвертація з перевіркою:
func toIntSafe(val int64, fieldName string) (int, error) {
    if val < math.MinInt || val > math.MaxInt {
        return 0, fmt.Errorf("%s overflow: %d", fieldName, val)
    }
    return int(val), nil
}

// Використання:
widthInt, err := toIntSafe(width, "width")
if err != nil {
    return nil, err
}
```

### 3️⃣ Чутливість до регістру роздільника
```go
// ❌ strings.Split(text, "x") розрізняє "x" та "X":
NewResolution("1920x1080")  // ✅ Працює
NewResolution("1920X1080")  // ❌ ErrResolutionInvalid

// ✅ Нормалізація вводу:
text = strings.ToLower(text)  // "1920X1080" → "1920x1080"
values := strings.Split(text, "x")
```

### 4️⃣ Зайві пробіли у вхідному рядку
```go
// ❌ "1920 x 1080" → values: ["1920 ", " 1080"] → ParseInt помилка!

// ✅ Очищення від пробілів:
values := strings.Split(text, "x")
if len(values) != 2 {
    return nil, ErrResolutionInvalid
}
// TrimSpace видаляє пробіли з обох кінців
widthStr := strings.TrimSpace(values[0])
heightStr := strings.TrimSpace(values[1])
width, err := strconv.ParseInt(widthStr, 10, 0)
```

### 5️⃣ Відсутність корисних методів для роботи з Resolution
```go
// ✅ Додати helper-методи:

// Співвідношення сторін (aspect ratio)
func (r *Resolution) AspectRatio() float64 {
    if r.Height == 0 {
        return 0
    }
    return float64(r.Width) / float64(r.Height)
}
// 1920x1080 → 1.777... (16:9)

// Перевірка чи "портретна" орієнтація
func (r *Resolution) IsPortrait() bool {
    return r.Height > r.Width
}

// Порівняння за площею (для вибору "найбільшого" варіанту)
func (r *Resolution) Megapixels() float64 {
    return float64(r.Width * r.Height) / 1_000_000
}
// 1920x1080 → ~2.07 MP

// Чи підходить для екрану клієнта
func (r *Resolution) FitsWithin(maxW, maxH int) bool {
    return r.Width <= maxW && r.Height <= maxH
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **адаптивним стрімінгом**:

### 🎯 Сценарій: фільтрація варіантів за роздільною здатністю
```go
// У WebSocketDistributor при підключенні мобільного клієнта:
func (d *Distributor) selectVariant(clientMaxRes *m3u8.Resolution, items []*m3u8.PlaylistItem) *m3u8.PlaylistItem {
    var best *m3u8.PlaylistItem
    var bestPixels int
    
    for _, item := range items {
        if item.Resolution == nil {
            continue  // Пропускаємо варіанти без вказаної роздільної здатності
        }
        
        // 🎯 Фільтр: не пропонувати варіанти більші за екран клієнта
        if !item.Resolution.FitsWithin(clientMaxRes.Width, clientMaxRes.Height) {
            continue
        }
        
        // 🎯 Вибір "найкращого" варіанту: максимальна площа в межах ліміту
        pixels := item.Resolution.Width * item.Resolution.Height
        if pixels > bestPixels {
            bestPixels = pixels
            best = item
        }
    }
    return best
}
```

### 🎯 Сценарій: логування метрик якості
```go
// У monitoring.Monitor для аналітики:
func (m *Monitor) recordVariantSelected(item *m3u8.PlaylistItem, clientID string) {
    if item.Resolution != nil {
        m.histograms["resolution_width"].Observe(float64(item.Resolution.Width))
        m.histograms["resolution_height"].Observe(float64(item.Resolution.Height))
        
        // 🎯 Групування за стандартними роздільними здатностями
        standard := m.standardizeResolution(item.Resolution)
        m.counters["variant_by_resolution_"+standard].Inc()
    }
}

func (m *Monitor) standardizeResolution(r *m3u8.Resolution) string {
    // 🎯 Нормалізація до стандартних назв
    switch {
    case r.Width >= 3840: return "4K"
    case r.Width >= 1920: return "1080p"
    case r.Width >= 1280: return "720p"
    case r.Width >= 854:  return "480p"
    default:             return "low"
    }
}
```

### 🎯 Сценарій: валідація вихідних параметрів FFmpeg
```go
// У segmentFinalizer після отримання параметрів сегмента:
func (sf *SegmentFinalizer) validateOutputParams(width, height int) error {
    // 🎯 Створення Resolution для валідації
    res := &m3u8.Resolution{Width: width, Height: height}
    
    // 🎯 Перевірка на стандартні співвідношення сторін (для CCTV)
    ratio := res.AspectRatio()
    const (
        Ratio16_9 = 1.777  // 16:9
        Ratio4_3  = 1.333  // 4:3
        Tolerance = 0.01   // Допустима похибка
    )
    
    if math.Abs(ratio-Ratio16_9) > Tolerance && 
       math.Abs(ratio-Ratio4_3) > Tolerance {
        sf.logger.Warn("non-standard aspect ratio", 
            "resolution", res.String(), 
            "ratio", ratio)
        // Не блокуємо, але логуємо для аналізу
    }
    
    return nil
}
```

---

## 🧪 Приклад використання: повний цикл

```go
// ✅ Парсинг вхідного рядка
res, err := m3u8.NewResolution("1920x1080")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Width: %d, Height: %d\n", res.Width, res.Height)
// Width: 1920, Height: 1080

// ✅ Серіалізація
fmt.Println(res.String())  // "1920x1080"

// ✅ Використання у PlaylistItem
item := &m3u8.PlaylistItem{
    Bandwidth:  2560000,
    URI:        "video/1080p.m3u8",
    Resolution: res,  // *Resolution
    Codecs:     pointer("avc1.640028"),
}
fmt.Println(item.String())
/*
#EXT-X-STREAM-INF:BANDWIDTH=2560000,CODECS="avc1.640028",RESOLUTION=1920x1080
video/1080p.m3u8
*/

// ✅ Обробка помилок
_, err = m3u8.NewResolution("invalid")
fmt.Println(err)  // ErrResolutionInvalid

_, err = m3u8.NewResolution("1920")  // Відсутня висота
fmt.Println(err)  // ErrResolutionInvalid

_, err = m3u8.NewResolution("1920xabc")  // Невалідна висота
fmt.Println(err)  // strconv.ParseInt: parsing "abc": invalid syntax
```

---

## 📋 Специфікація HLS (RFC 8216) — вимоги до RESOLUTION

```
✅ Формат: "DecimalInteger[x]DecimalInteger" (без пробілів)
✅ Обидва значення мають бути > 0
✅ Роздільник: літера "x" (маленька, чутливо до регістру у парсері)
✅ Опціональний атрибут, але РЕКОМЕНДОВАНИЙ для:
   • Адаптивного бітрейту (ABR)
   • Фільтрації за можливостями пристрою
   • Економії трафіку (не завантажувати 4K на телефон)
✅ Не має бути вказаний для I-Frame варіантів без відео-доріжки
```

---

## 🎯 Висновок

Цей код — **мінімалістичний, але критично важливий** компонент:

✅ Чіткий контракт: парсинг ↔ серіалізація  
✅ Type-safe: уникнення помилок з рядками "1920x1080"  
✅ Легка інтеграція: використовується в PlaylistItem через `*Resolution`

**Для вашого проекту — рекомендовані покращення**:

1. ✅ Додати валідацію діапазону `[160, 16384]` для Width/Height
2. ✅ Нормалізувати ввід: `strings.ToLower()`, `strings.TrimSpace()`
3. ✅ Використовувати `base=10` у `ParseInt` для суворості
4. 🎯 Додати helper-методи: `AspectRatio()`, `FitsWithin()`, `Megapixels()`
5. 🔒 Додати перевірку переповнення при `int64 → int`

**Приклад оптимізації для CCTV**:
```go
// Для моніторингу: достатньо 480p для економії трафіку
lowResVariant := &m3u8.PlaylistItem{
    Bandwidth:  500000,
    URI:        "video/480p.m3u8",
    Resolution: &m3u8.Resolution{Width: 854, Height: 480},  // 16:9
    // ... інші поля
}
// → Клієнти з обмеженим трафіком автоматично оберуть цей варіант
```

Потрібно допомогти з:
- 🧠 Реалізацією helper-методів для `Resolution`?
- 🧪 Юніт-тестами для крайніх випадків парсингу?
- 🔗 Інтеграцією валідації роздільної здатності у ваш pipeline?

Чекаю на ваші питання! 🛠️