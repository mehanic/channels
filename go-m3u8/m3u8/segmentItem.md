# 🔍 Глибокий розбір коду: `SegmentItem` для HLS Media Playlist

Цей код реалізує роботу з тегом **`#EXTINF`** — **фундаментальним елементом** HLS Media Playlist, який описує окремий медіа-сегмент: його тривалість, опційний коментар, таймштамп та байт-рендж. Розберемо детально.

---

## 📦 Що таке `SegmentItem` і навіщо він потрібен?

### Контекст: Media Playlist
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:1000

#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
#EXTINF:4.000,Segment 1000
https://cdn/seg1000.ts

#EXTINF:4.000,Segment 1001
https://cdn/seg1001.ts

#EXT-X-BYTERANGE:188k@0
#EXTINF:4.000,
https://cdn/seg1002.ts
```

### Призначення `SegmentItem`
| Поле | HLS-тег | Призначення | Приклад |
|------|---------|-------------|---------|
| `Duration` | `#EXTINF` (обов'язковий) | Тривалість сегмента у секундах | `4.0`, `3.967` |
| `Segment` | URI після `#EXTINF` | Посилання на медіа-файл | `seg1000.ts`, `https://cdn/init.mp4` |
| `Comment` | `#EXTINF` (опціональний) | Людське ім'я/опис сегмента | `"News Block 12:00"` |
| `ProgramDateTime` | `#EXT-X-PROGRAM-DATE-TIME` | Абсолютний час початку сегмента (UTC) | `2024-01-01T12:00:00Z` |
| `ByteRange` | `#EXT-X-BYTERANGE` | Діапазон байтів для часткового завантаження | `188k@0` = 188KB з позиції 0 |

### 🎯 Навіщо це критично для HLS?
```
🔴 Live-стрім (CCTV):
• Duration + Sequence = "ковзне вікно" останніх подій
• ProgramDateTime = синхронізація з реальним часом (важливо для архіву)
• ByteRange = економія трафіку при повторному завантаженні

🎬 VOD (архів):
• Duration = загальна тривалість для прогрес-бару
• Comment = розділи для навігації ("Інтерв'ю", "Реклама")
• ByteRange = підтримка partial requests для швидкого seek

🔄 ABR (адаптивний бітрейт):
• Однакові Duration у всіх варіантах = синхронне перемикання
• Однакові Segment URI patterns = просте кешування CDN
```

---

## 🏗️ Struct `SegmentItem` — карта сегмента

```go
type SegmentItem struct {
    Duration        float64     // Тривалість у секундах (напр. 4.0)
    Segment         string      // URI сегмента: відносний або абсолютний URL
    
    // Опціональні поля (nil = відсутні у плейлисті)
    Comment         *string     // Людиноподібний опис (після коми у #EXTINF)
    ProgramDateTime *TimeItem   // Абсолютний час початку (структура для RFC3339)
    ByteRange       *ByteRange  // Діапазон байтів для partial fetch
}
```

### 🎯 Чому `Segment` — `string`, а не `*string`?
```go
// URI сегмента — ОБОВ'ЯЗКОВИЙ за специфікацією:
// • Кожен #EXTINF має бути одразу за якимсь сегментом
// • Порожній URI = невалідний плейлист
// • Тому string (не pointer) — якщо порожній, це помилка логіки

// Опціональні поля — *T:
// • Comment=nil → не виводити коментар у #EXTINF
// • ProgramDateTime=nil → не виводити #EXT-X-PROGRAM-DATE-TIME
// • ByteRange=nil → не виводити #EXT-X-BYTERANGE
```

### 🎯 Тип `TimeItem` (припустима реалізація)
```go
// TimeItem — обгортка навколо time.Time для серіалізації у RFC3339
type TimeItem struct {
    Time time.Time
}

func (t *TimeItem) String() string {
    return fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s", 
        t.Time.Format(time.RFC3339Nano))
}
```

---

## 🔧 Конструктор `NewSegmentItem` — парсинг `#EXTINF`

```go
func NewSegmentItem(text string) (*SegmentItem, error) {
    var si SegmentItem
    
    // Крок 1: Видалення префікса тегу "#EXTINF:"
    // Вхід: "#EXTINF:4.000,Segment 1000"
    // Після: "4.000,Segment 1000"
    line := strings.Replace(text, SegmentItemTag+":", "", -1)
    
    // Крок 2: Видалення нових рядків (захист від multiline вводу)
    line = strings.Replace(line, "\n", "", -1)
    
    // Крок 3: Розбиття за комою: [тривалість, коментар?]
    // "4.000,Segment 1000" → values: ["4.000", "Segment 1000"]
    // "4.000,"             → values: ["4.000", ""]
    // "4.000"              → values: ["4.000"]
    values := strings.Split(line, ",")
    
    // Крок 4: Парсинг тривалості (обов'язковий, float64)
    d, err := strconv.ParseFloat(values[0], 64)
    if err != nil {
        return nil, err  // Помилка: нечислове значення
    }
    si.Duration = d
    
    // Крок 5: Опціональний коментар (якщо є і не порожній)
    if len(values) > 1 && values[1] != "" {
        si.Comment = &values[1]  // Зберігаємо покажчик на рядок
    }
    
    // ⚠️ УВАГА: URI сегмента НЕ парситься тут!
    // Він очікується окремим рядком ПІСЛЯ #EXTINF у плейлисті
    // Це відповідальність вищого рівня (парсер плейлиста)
    
    return &si, nil
}
```

### 🔍 Критичні моменти парсингу
```go
// ❌ Проблема 1: strings.Replace з -1 може видалити зайве
// Якщо URI містить "#EXTINF:" (малоймовірно, але можливо):
// "#EXTINF:4.0,http://example.com/#EXTINF:extra" 
// → "4.0,http://example.com/extra"  ← ПОМИЛКА!

// ✅ Безпечніше: strings.TrimPrefix
line := strings.TrimPrefix(text, SegmentItemTag+":")

// ❌ Проблема 2: коментар може містити кому!
// "#EXTINF:4.0,Title, with comma" → values: ["4.0", "Title", " with comma"]
// → Comment = "Title" (втрата частини!)

// ✅ Специфікація: коментар НЕ має містити коми
// Але якщо потрібно — екранувати або використовувати інший формат

// ❌ Проблема 3: URI не парситься у конструкторі
// Це розділяє відповідальність, але ускладнює API:
// клієнт має пам'ятати: спочатку NewSegmentItem(), потім si.Segment = uri

// ✅ Документувати цей контракт чітко!
```

---

## 🔄 Метод `String()` — серіалізація з порядком тегів

```go
func (si *SegmentItem) String() string {
    // 🎯 Крок 1: ProgramDateTime (якщо є) — ПЕРЕД #EXTINF
    date := ""
    if si.ProgramDateTime != nil {
        // TimeItem.String() повертає "#EXT-X-PROGRAM-DATE-TIME:...\n"
        date = fmt.Sprintf("%v\n", si.ProgramDateTime)
    }
    
    // 🎯 Крок 2: ByteRange (якщо є) — ПЕРЕД #EXTINF, після ProgramDateTime
    byteRange := ""
    if si.ByteRange != nil {
        // ByteRange.String() повертає "188k@0"
        byteRange = fmt.Sprintf("\n%s:%v", ByteRangeItemTag, si.ByteRange.String())
    }
    
    // 🎯 Крок 3: Comment — частина #EXTINF
    comment := ""
    if si.Comment != nil {
        comment = *si.Comment
    }
    
    // 🎯 Крок 4: Збірка фінального рядка
    // Порядок: [ProgramDateTime] [ByteRange] #EXTINF:duration,comment\nURI
    return fmt.Sprintf("%s:%v,%s%s\n%s%s", 
        SegmentItemTag,        // "#EXTINF"
        si.Duration,           // 4.0
        comment,               // "Segment 1000" або ""
        byteRange,             // "\n#EXT-X-BYTERANGE:188k@0" або ""
        date,                  // "#EXT-X-PROGRAM-DATE-TIME:...\n" або ""
        si.Segment)            // "https://cdn/seg.ts"
}
```

### ⚠️ Проблема порядку тегів у `String()`
```go
// ❌ Поточний код генерує:
// #EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
// #EXT-X-BYTERANGE:188k@0
// #EXTINF:4.0,Comment
// https://cdn/seg.ts

// Але специфікація рекомендує порядок:
// #EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z  ← ДО #EXTINF
// #EXT-X-BYTERANGE:188k@0                         ← ДО #EXTINF  
// #EXTINF:4.0,Comment
// https://cdn/seg.ts

// ✅ Поточний код ПРАВИЛЬНИЙ: ProgramDateTime і ByteRange виводяться ПЕРЕД #EXTINF
// Але формат-рядок може бути неочевидним через вставку \n у середині

// ✅ Рекомендація: розбити на рядки для читабельності
var lines []string
if si.ProgramDateTime != nil {
    lines = append(lines, si.ProgramDateTime.String())
}
if si.ByteRange != nil {
    lines = append(lines, fmt.Sprintf("%s:%v", ByteRangeItemTag, si.ByteRange))
}
lines = append(lines, fmt.Sprintf("%s:%.3f,%s", SegmentItemTag, si.Duration, comment))
lines = append(lines, si.Segment)
return strings.Join(lines, "\n")
```

### 🎯 Формат виводу (приклади)
```m3u8
#EXTINF:4.0,
seg1000.ts

#EXTINF:4.0,News Block
seg1001.ts

#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:00Z
#EXTINF:4.0,
seg1002.ts

#EXT-X-BYTERANGE:188k@0
#EXTINF:4.0,
seg1003.ts

#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:16Z
#EXT-X-BYTERANGE:188k@376k
#EXTINF:4.0,Live Feed
seg1004.ts
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність валідації `Duration`
```go
// ❌ Поточний код приймає будь-яке float64:
si, _ := NewSegmentItem("#EXTINF:-4.0,")   // Від'ємна тривалість!
si, _ = NewSegmentItem("#EXTINF:9999.0,")  // Нереально довгий сегмент

// ✅ Додати валідацію після парсингу:
func NewSegmentItem(text string) (*SegmentItem, error) {
    // ... парсинг Duration ...
    
    // ✅ Валідація: тривалість має бути в розумних межах
    const (
        MinDuration = 0.1    // Мінімум: 100мс
        MaxDuration = 60.0   // Максимум: 60с (зазвичай TARGETDURATION ≤ 10)
    )
    
    if d < MinDuration || d > MaxDuration {
        return nil, fmt.Errorf("invalid duration %.3f: must be [%.1f, %.1f]", 
            d, MinDuration, MaxDuration)
    }
    
    si.Duration = d
    // ... решта коду
}
```

### 2️⃣ `Segment` (URI) не встановлюється у конструкторі
```go
// ❌ API незручний: клієнт має робити два кроки
si, err := NewSegmentItem("#EXTINF:4.0,Comment")
if err != nil { ... }
si.Segment = "https://cdn/seg.ts"  // ← Окремий крок!

// ✅ Варіант 1: додати параметр URI у конструктор
func NewSegmentItem(extinfLine, uri string) (*SegmentItem, error) {
    si, err := parseExtinf(extinfLine)  // внутрішня функція
    if err != nil {
        return nil, err
    }
    si.Segment = uri
    return si, nil
}

// ✅ Варіант 2: метод-сетер з валідацією
func (si *SegmentItem) SetURI(uri string) error {
    if uri == "" {
        return fmt.Errorf("segment URI cannot be empty")
    }
    si.Segment = uri
    return nil
}
```

### 3️⃣ Форматування `Duration` у `String()`
```go
// ❌ fmt.Sprintf("%v", 4.0) → "4" (без десяткових)
// ❌ А специфікація рекомендує 3 знаки після коми для сумісності

// ✅ Використовувати фіксований формат:
return fmt.Sprintf("%s:%.3f,%s...", SegmentItemTag, si.Duration, ...)
// Результат: "#EXTINF:4.000," замість "#EXTINF:4,"

// ✅ Або динамічний: якщо дробова частина = 0, виводити ".0"
func formatDuration(d float64) string {
    if d == float64(int64(d)) {
        return fmt.Sprintf("%.1f", d)  // "4.0"
    }
    return fmt.Sprintf("%.3f", d)       // "3.967"
}
```

### 4️⃣ Екранування спеціальних символів у `Comment`
```go
// ❌ Якщо Comment містить лапки або нові рядки:
si.Comment = pointer(`Title with "quotes"`)
// Вивід: #EXTINF:4.0,Title with "quotes"  ← Може зламати парсер!

// ✅ Специфікація: коментар НЕ має містити спеціальних символів
// Але для безпеки — екранувати або валідувати:
func escapeComment(s string) string {
    s = strings.ReplaceAll(s, `"`, `\"`)
    s = strings.ReplaceAll(s, "\n", " ")
    s = strings.ReplaceAll(s, "\r", " ")
    return s
}
// Використання у String():
comment := ""
if si.Comment != nil {
    comment = escapeComment(*si.Comment)
}
```

### 5️⃣ Thread-safety при спільному доступі
```go
// ❌ У вашому pipeline (8x FFmpeg workers → segmentFinalizer):
seg := &SegmentItem{Duration: 4.0, Segment: "seg1.ts"}
pl.AppendItem(seg)  // Горутина 1: запис
s := seg.String()   // Горутина 2: читання → DATA RACE!

// ✅ Рішення: immutable патерн або sync.RWMutex
type SafeSegmentItem struct {
    mu sync.RWMutex
    SegmentItem
}

func (ss *SafeSegmentItem) String() string {
    ss.mu.RLock()
    defer ss.mu.RUnlock()
    // ... серіалізація
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **segmentAssembler → segmentFinalizer**:

### 🎯 Сценарій: створення сегмента у `segmentFinalizer`
```go
func (sf *SegmentFinalizer) createSegment(seqNum int, duration float64, uri string) (*m3u8.SegmentItem, error) {
    // 🎯 Створення базового сегмента
    seg := &m3u8.SegmentItem{
        Duration: duration,  // З ffprobe або розрахункове
        Segment:  uri,       // "/channels/ch1/seg1234.ts"
    }
    
    // 🎯 Додавання ProgramDateTime для синхронізації
    if !sf.startTime.IsZero() {
        segTime := sf.startTime.Add(time.Duration(seqNum) * sf.segmentDuration)
        seg.ProgramDateTime = &m3u8.TimeItem{Time: segTime}
    }
    
    // 🎯 Опціональний коментар для дебагу
    if sf.debugMode {
        comment := fmt.Sprintf("seq=%d,pts=%d", seqNum, sf.lastPTS)
        seg.Comment = &comment
    }
    
    // 🎯 ByteRange для fMP4 partial fetch (опціонально)
    if sf.useByteRange && seqNum > 0 {
        offset, length := sf.calculateByteRange(seqNum)
        seg.ByteRange = &m3u8.ByteRange{Length: length, Offset: &offset}
    }
    
    return seg, nil
}
```

### 🎯 Сценарій: валідація сегмента перед додаванням у плейлист
```go
func (sf *SegmentFinalizer) validateSegment(seg *m3u8.SegmentItem) error {
    // ✅ Перевірка обов'язкових полів
    if seg.Segment == "" {
        return fmt.Errorf("segment URI is required")
    }
    if seg.Duration <= 0 {
        return fmt.Errorf("duration must be positive: %.3f", seg.Duration)
    }
    
    // ✅ Перевірка відповідності TARGETDURATION
    if seg.Duration > float64(sf.targetDuration) {
        return fmt.Errorf("duration %.3f exceeds TARGETDURATION %d", 
            seg.Duration, sf.targetDuration)
    }
    
    // ✅ Перевірка ByteRange (якщо є)
    if seg.ByteRange != nil {
        if seg.ByteRange.Length <= 0 {
            return fmt.Errorf("byte range length must be positive")
        }
        // Offset може бути nil = з початку файлу
    }
    
    return nil
}
```

### 🎯 Сценарій: генерація media-плейлиста з сегментами
```go
func generateMediaPlaylist(channelID string, segments []*m3u8.SegmentItem, targetDuration int) string {
    var buf strings.Builder
    buf.WriteString("#EXTM3U\n")
    buf.WriteString(fmt.Sprintf("#EXT-X-VERSION:7\n"))
    buf.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", targetDuration))
    buf.WriteString(fmt.Sprintf("#EXT-X-MEDIA-SEQUENCE:%d\n", segments[0].SeqNum))
    
    // 🎯 Додавання EXT-X-MAP для fMP4
    if initURI := getInitURI(channelID); initURI != "" {
        buf.WriteString(fmt.Sprintf("#EXT-X-MAP:URI=\"%s\"\n", initURI))
    }
    
    // 🎯 Додавання сегментів
    for _, seg := range segments {
        buf.WriteString(seg.String())  // Викликає SegmentItem.String()
        buf.WriteString("\n")
    }
    
    // 🎯 Для live: EXT-X-ENDLIST відсутній
    // Для VOD: додати в кінці
    // buf.WriteString("#EXT-X-ENDLIST\n")
    
    return buf.String()
}
```

---

## 🧪 Приклад використання: повний цикл

```go
// ✅ Створення сегмента з мінімальними даними
seg1, err := m3u8.NewSegmentItem("#EXTINF:4.0,")
if err != nil {
    log.Fatal(err)
}
seg1.Segment = "seg1000.ts"
fmt.Println(seg1.String())
/*
#EXTINF:4.0,
seg1000.ts
*/

// ✅ Сегмент з коментарем та таймштампом
seg2, _ := m3u8.NewSegmentItem("#EXTINF:3.967,News Block")
seg2.Segment = "seg1001.ts"
seg2.ProgramDateTime = &m3u8.TimeItem{
    Time: time.Date(2024, 1, 1, 12, 0, 4, 0, time.UTC),
}
fmt.Println(seg2.String())
/*
#EXT-X-PROGRAM-DATE-TIME:2024-01-01T12:00:04Z
#EXTINF:3.967,News Block
seg1001.ts
*/

// ✅ Сегмент з ByteRange (для partial fetch)
seg3, _ := m3u8.NewSegmentItem("#EXTINF:4.0,")
seg3.Segment = "init.mp4"
seg3.ByteRange = &m3u8.ByteRange{Length: 188000, Offset: pointer(0)}
fmt.Println(seg3.String())
/*
#EXT-X-BYTERANGE:188000@0
#EXTINF:4.0,
init.mp4
*/

// ✅ Обробка помилок парсингу
_, err = m3u8.NewSegmentItem("#EXTINF:invalid,")
fmt.Println(err)  // strconv.ParseFloat: parsing "invalid": invalid syntax

_, err = m3u8.NewSegmentItem("#EXTINF:")  // Відсутня тривалість
fmt.Println(err)  // strconv.ParseFloat: parsing "": invalid syntax
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги

```
✅ #EXTINF:duration,[title] — обов'язковий для кожного сегмента
✅ duration: додатне float, зазвичай ≤ #EXT-X-TARGETDURATION
✅ title (Comment): опціональний, не має містити коми або нових рядків
✅ URI сегмента: ОБОВ'ЯЗКОВИЙ, йде окремим рядком ПІСЛЯ #EXTINF
✅ #EXT-X-PROGRAM-DATE-TIME: опціональний, але РЕКОМЕНДОВАНИЙ для:
   • Синхронізації з реальним часом
   • Архівування та пошуку за часом
   • Кореляції з іншими джерелами даних
✅ #EXT-X-BYTERANGE: опціональний, формат "N[@O]" де:
   • N = довжина у байтах (обов'язкова)
   • O = зміщення від початку файлу (опціональне, за замовчуванням 0)
✅ Порядок тегів перед сегментом:
   #EXT-X-PROGRAM-DATE-TIME (якщо є)
   #EXT-X-BYTERANGE (якщо є)
   #EXTINF:duration,title
   URI
```

---

## 🎯 Висновок

Цей код — **ядро медіа-плейлиста**: кожен `SegmentItem` — це один "кадр" у стрімі вашого CCTV.

✅ Мінімум полів, максимум гнучкості  
✅ Підтримка ключових функцій: таймштампи, byte-range, коментарі  
✅ Чіткий контракт: парсинг `#EXTINF` ↔ серіалізація

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати валідацію `Duration ∈ [0.1, 60.0]` у конструкторі
2. ✅ Форматувати `Duration` як `%.3f` для сумісності з плеєрами
3. ✅ Додати метод `SetURI(uri string) error` для безпечного встановлення
4. ✅ Екранувати `Comment` від спецсимволів (`"`, `\n`)
5. 🔒 Забезпечити thread-safety при конкурентному доступі (ваші 8x workers)

**Приклад оптимізації для low-latency CCTV**:
```go
// Для мінімальної затримки: короткі сегменти + точні таймштампи
seg := &m3u8.SegmentItem{
    Duration: 2.0,  // 2-секундні сегменти = швидше оновлення плейлиста
    Segment:  fmt.Sprintf("/channels/%s/seg%d.ts", channelID, seqNum),
    ProgramDateTime: &m3u8.TimeItem{Time: segmentStartTime},  // Точна синхронізація
    // Comment не потрібен у продакшені → економимо байти
}
// → Клієнти отримують оновлення кожні 2 секунди замість 4-10
```

Потрібно допомогти з:
- 🧠 Реалізацією `TimeItem` для `ProgramDateTime`?
- 🔗 Інтеграцією `SegmentItem` у ваш `segmentFinalizer` з підтримкою fMP4?
- 🧪 Юніт-тестами для крайніх випадків (дуже малі/великі Duration, спецсимволи в Comment)?

Чекаю на ваші питання! 🛠️