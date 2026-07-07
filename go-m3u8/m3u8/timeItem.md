# 🔍 Глибокий розбір коду: `TimeItem` для HLS `#EXT-X-PROGRAM-DATE-TIME`

Цей код реалізує роботу з тегом **`#EXT-X-PROGRAM-DATE-TIME`** — механізмом для прив'язки **абсолютного часу UTC** до медіа-сегментів у HLS-плейлистах. Критично важливий для синхронізації, архівування та кореляції подій.

---

## 📦 Що таке `#EXT-X-PROGRAM-DATE-TIME` і навіщо він потрібен?

### Контекст: Media Playlist з таймштампами
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:1000

#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00.000Z
#EXTINF:4.000,
seg1000.ts

#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:04.000Z
#EXTINF:4.000,
seg1001.ts

#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:08.123Z
#EXTINF:4.000,
seg1002.ts
```

### Призначення тега
| Аспект | Пояснення |
|--------|-----------|
| **Формат** | RFC 3339 / ISO 8601: `YYYY-MM-DDTHH:MM:SS.sssZ` |
| **Часова зона** | Завжди **UTC** (індикатор `Z` або `+00:00`) |
| **Точність** | До наносекунд (9 знаків після коми), але зазвичай мілісекунди |
| **Частота** | Рекомендується перед **кожним** сегментом для точної синхронізації |

### 🎯 Критичні сценарії використання
```
🔴 Live CCTV моніторинг:
• Клієнт бачить точний час події: "14:30:08" → кореляція з іншими камерами
• Архівування: пошук за часом "знайти інцидент о 14:30"
• Юридична валідність: таймштампи як доказ у реальному часі

🔄 Синхронізація аудіо/відео/субтитрів:
• Ваш pipeline: відео 10с = 2× аудіо-чанки по 4с + субтитри
• PROGRAM-DATE-TIME = єдиний "якір" для вирівнювання всіх доріжок
• PTS нормалізація: розрахунок drift відносно абсолютного часу

📊 Аналітика та метрики:
• Затримка стріму = server_time - program_date_time
• Виявлення розривів: gap > 1с між послідовними таймштампами
• Кореляція з зовнішніми подіями (тривоги, новини)

🎬 VOD навігація:
• Прогрес-бар показує реальний час, не просто "00:05:30"
• Швидкий перехід до моменту: "перейти до 14:30:00"
```

---

## 🏗️ Struct `TimeItem` — обгортка навколо `time.Time`

```go
type TimeItem struct {
    Time time.Time  // Абсолютний час у форматі UTC
}
```

### 🎯 Чому окрема структура, а не просто `time.Time`?
```go
// ✅ Інтерфейс Item: TimeItem реалізує String() для поліморфізму
// ✅ Інкапсуляція: логіка форматування ізольована в одному місці
// ✅ Розширюваність: можна додати методи без зміни API:
//   • ti.IsZero() — чи встановлено час
//   • ti.DurationSince(other) — різниця між таймштампами
//   • ti.WithOffset(d) — зсув для корекції drift

// ✅ Сумісність з []Item у Playlist:
pl.AppendItem(&TimeItem{Time: segStartTime})  // Поліморфне додавання
```

---

## 🔧 Конструктор `NewTimeItem` — парсинг таймштампу

```go
func NewTimeItem(text string) (*TimeItem, error) {
    // Крок 1: Видалення префікса тегу "#EXT-X-PROGRAM-DATE-TIME:"
    // Вхід: "#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00.000Z"
    // Після: "2024-01-15T14:30:00.000Z"
    timeString := strings.Replace(text, TimeItemTag+":", "", -1)

    // Крок 2: Парсинг рядка у time.Time через гнучкий парсер
    t, err := ParseTime(timeString)
    if err != nil {
        return nil, err  // Помилка: невідповідність формату
    }

    // Крок 3: Побудова об'єкта
    return &TimeItem{Time: t}, nil
}
```

### ⚠️ Потенційна проблема: `strings.Replace` з `-1`
```go
// ❌ Ризик: якщо час містить префікс тегу (малоймовірно, але теоретично):
// "#EXT-X-PROGRAM-DATE-TIME:2024-01-15T#EXT-X-PROGRAM-DATE-TIME:00Z"
// → Видалить ВСІ входження → пошкодить дані!

// ✅ Безпечніше: strings.TrimPrefix (видаляє тільки перше входження)
timeString := strings.TrimPrefix(text, TimeItemTag+":")

// ✅ Ще краще: валідація, що префікс дійсно був
if !strings.HasPrefix(text, TimeItemTag+":") {
    return nil, fmt.Errorf("invalid TimeItem format: missing %s prefix", TimeItemTag)
}
```

---

## 🔄 Метод `String()` — серіалізація у RFC 3339 Nano

```go
func (ti *TimeItem) String() string {
    // dateTimeFormat = time.RFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
    return fmt.Sprintf("%s:%s", TimeItemTag, ti.Time.Format(dateTimeFormat))
}
```

### 🎯 Формат виводу (приклади)
```m3u8
#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00.000Z
#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:04.123456789Z
#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:08+02:00  // Якщо час не UTC!
```

### ⚠️ Критичний нюанс: `RFC3339Nano` та часові зони
```go
// time.RFC3339Nano завжди включає часову зону:
// • UTC: "Z" або "+00:00"
// • Інша зона: "+02:00", "-05:00"

// ❌ Проблема: якщо ti.Time не в UTC, вивід буде некоректним для HLS!
// Специфікація вимагає UTC для PROGRAM-DATE-TIME

// ✅ Рішення: нормалізувати час до UTC перед серіалізацією
func (ti *TimeItem) String() string {
    return fmt.Sprintf("%s:%s", TimeItemTag, 
        ti.Time.UTC().Format(dateTimeFormat))  // ✅ Примусовий UTC
}
```

---

## 🧩 Helper-функції: `ParseTime` та `FormatTime`

### `FormatTime` — простий делегат
```go
func FormatTime(t time.Time) string {
    return t.Format(dateTimeFormat)  // RFC3339Nano
}
// ✅ Корисно для форматування часу поза контекстом тега
// ⚠️ Не нормалізує до UTC — клієнт має робити це сам
```

### `ParseTime` — гнучкий парсер з fallback-логікою
```go
func ParseTime(value string) (time.Time, error) {
    // 🎯 Три варіанти формату для сумісності з різними генераторами:
    layouts := []string{
        "2006-01-02T15:04:05.999999999Z0700",   // Без двокрапки в зоні: +0000
        "2006-01-02T15:04:05.999999999Z07:00",  // Стандарт RFC3339: +00:00
        "2006-01-02T15:04:05.999999999Z07",     // Коротка зона: +00
    }
    
    var err error
    var t time.Time
    
    // 🎯 Спроба парсингу по черзі: перший успіх = результат
    for _, layout := range layouts {
        if t, err = time.Parse(layout, value); err == nil {
            return t, nil  // ✅ Успіх: повертаємо розпаршений час
        }
    }
    
    // ❌ Всі варіанти не вдалися: повертаємо останню помилку
    return t, err
}
```

### 🔍 Чому три layout? Приклади вхідних даних
| Формат | Приклад | Походження |
|--------|---------|------------|
| `Z0700` | `2024-01-15T14:30:00.000Z+0200` | Старіші генератори, FFmpeg <4.x |
| `Z07:00` | `2024-01-15T14:30:00.000Z+02:00` | Стандарт RFC 3339, Go `time.RFC3339Nano` |
| `Z07` | `2024-01-15T14:30:00.000Z+02` | Спрощені реалізації, вбудовані пристрої |

### ⚠️ Проблема: `return t, err` при невдачі
```go
// ❌ Якщо всі layout не спрацювали:
// • t = zero value (0001-01-01 00:00:00 +0000 UTC)
// • err = помилка останнього Parse()
// • Клієнт може не перевірити err → працює з нульовим часом!

// ✅ Краще: явна перевірка та інформативна помилка
func ParseTime(value string) (time.Time, error) {
    layouts := []string{...}
    
    for _, layout := range layouts {
        if t, err := time.Parse(layout, value); err == nil {
            return t, nil
        }
    }
    
    // ❌ Всі спроби не вдалися
    return time.Time{}, fmt.Errorf("failed to parse time %q: unsupported format", value)
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність нормалізації до UTC
```go
// ❌ Поточний код зберігає будь-яку часову зону:
t, _ := time.Parse(time.RFC3339, "2024-01-15T16:30:00+02:00")
ti := &TimeItem{Time: t}
fmt.Println(ti.String())  
// #EXT-X-PROGRAM-DATE-TIME:2024-01-15T16:30:00+02:00  ← ❌ Не UTC!

// ✅ Специфікація HLS вимагає UTC:
// RFC 8216 §4.3.2.3: "The date and time SHOULD be in UTC"

// ✅ Рішення: нормалізувати при створенні
func NewTimeItem(text string) (*TimeItem, error) {
    // ... парсинг ...
    return &TimeItem{Time: t.UTC()}, nil  // ✅ Примусовий UTC
}

// ✅ Або при серіалізації:
func (ti *TimeItem) String() string {
    return fmt.Sprintf("%s:%s", TimeItemTag, ti.Time.UTC().Format(dateTimeFormat))
}
```

### 2️⃣ Обробка нульового часу (`time.IsZero()`)
```go
// ❌ Можна створити TimeItem з нульовим часом:
ti := &TimeItem{Time: time.Time{}}  // 0001-01-01 00:00:00 +0000 UTC
fmt.Println(ti.String())
// #EXT-X-PROGRAM-DATE-TIME:0001-01-01T00:00:00Z  ← ❌ Некоректно для HLS!

// ✅ Додати валідацію:
func (ti *TimeItem) Validate() error {
    if ti.Time.IsZero() {
        return fmt.Errorf("TimeItem cannot have zero time value")
    }
    return nil
}

// ✅ Або повертати помилку у конструкторі при парсингі "порожнього" рядка
```

### 3️⃣ Точність наносекунд: чи потрібна?
```go
// time.RFC3339Nano підтримує 9 знаків після коми (наносекунди)
// Але:
// • Більшість медіа-сегментів мають точність ~1-10 мс
// • Плеєри часто ігнорують частини < мілісекунд
// • Зайві цифри збільшують розмір плейлиста

// ✅ Опція: форматувати тільки до мілісекунд для економії
const milliFormat = "2006-01-02T15:04:05.000Z07:00"

func (ti *TimeItem) String() string {
    // Якщо наносекунди = 0, використовувати коротший формат
    if ti.Time.Nanosecond()%1e6 == 0 {
        return fmt.Sprintf("%s:%s", TimeItemTag, ti.Time.UTC().Format(milliFormat))
    }
    return fmt.Sprintf("%s:%s", TimeItemTag, ti.Time.UTC().Format(dateTimeFormat))
}
// Результат: "2024-01-15T14:30:00.000Z" замість "2024-01-15T14:30:00.000000000Z"
```

### 4️⃣ Thread-safety при спільному доступі
```go
// ❌ time.Time immutable, але якщо TimeItem змінюється:
ti := &TimeItem{Time: startTime}
// Горутина 1: ti.Time = ti.Time.Add(4*time.Second)  // Оновлення
// Горутина 2: s := ti.String()  // Читання → DATA RACE!

// ✅ Рішення: immutable патерн (найпростіший для таймштампів)
// • Не змінювати існуючий TimeItem, а створювати новий:
ti2 := &TimeItem{Time: ti.Time.Add(4 * time.Second)}  // Безпечно

// ✅ Або додати sync.RWMutex якщо потрібні оновлення:
type SafeTimeItem struct {
    mu sync.RWMutex
    TimeItem
}
```

### 5️⃣ Кешування результату `String()`
```go
// ✅ Якщо TimeItem серіалізується багато разів (напр. у розсилці клієнтам):
type CachedTimeItem struct {
    mu        sync.RWMutex
    Time      time.Time
    cachedStr string
    dirty     bool
}

func (cti *CachedTimeItem) String() string {
    cti.mu.RLock()
    if !cti.dirty && cti.cachedStr != "" {
        defer cti.mu.RUnlock()
        return cti.cachedStr
    }
    cti.mu.RUnlock()
    
    cti.mu.Lock()
    defer cti.mu.Unlock()
    cti.cachedStr = fmt.Sprintf("%s:%s", TimeItemTag, cti.Time.UTC().Format(dateTimeFormat))
    cti.dirty = false
    return cti.cachedStr
}

func (cti *CachedTimeItem) Update(t time.Time) {
    cti.mu.Lock()
    defer cti.mu.Unlock()
    cti.Time = t.UTC()
    cti.dirty = true  // Позначити, що кеш застарів
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **синхронізацією аудіо/відео/субтитрів**:

### 🎯 Сценарій: генерація PROGRAM-DATE-TIME у `segmentFinalizer`
```go
func (sf *SegmentFinalizer) createTimeItem(seqNum int) *m3u8.TimeItem {
    // 🎯 Розрахунок абсолютного часу сегмента:
    // startTime = час першого сегмента каналу
    // segmentDuration = 4.0 секунди (конфігурується)
    segmentTime := sf.startTime.Add(
        time.Duration(seqNum) * sf.segmentDuration,
    )
    
    // ✅ Нормалізація до UTC + створення TimeItem
    return &m3u8.TimeItem{Time: segmentTime.UTC()}
}

// Використання при додаванні сегмента:
func (sf *SegmentFinalizer) addSegment(seqNum int, uri string) {
    seg := &m3u8.SegmentItem{
        Duration: sf.segmentDuration,
        Segment:  uri,
        ProgramDateTime: sf.createTimeItem(seqNum),  // 🔗 Прив'язка часу
    }
    sf.playlist.AppendItem(seg)
}
```

### 🎯 Сценарій: корекція drift на основі `PROGRAM-DATE-TIME`
```go
// У VideoManifestProxy для синхронізації з серверним часом:
func (p *VideoManifestProxy) correctTimeDrift(serverTime time.Time, items []Item) {
    var lastTimeItem *m3u8.TimeItem
    
    // 🎯 Знайти останній PROGRAM-DATE-TIME у плейлисті
    for i := len(items) - 1; i >= 0; i-- {
        if ti, ok := items[i].(*m3u8.TimeItem); ok {
            lastTimeItem = ti
            break
        }
    }
    
    if lastTimeItem == nil {
        return  // Немає таймштампів → неможливо корегувати
    }
    
    // 🎯 Розрахунок drift: серверний час - час з плейлиста
    expectedTime := lastTimeItem.Time.Add(p.lastSegmentDuration)
    drift := serverTime.Sub(expectedTime)
    
    // 🎯 Якщо drift > поріг (напр. 1с) → корегувати наступні таймштампи
    if drift.Abs() > time.Second {
        p.logger.Warn("time drift detected", 
            "drift", drift, 
            "expected", expectedTime, 
            "actual", serverTime)
        
        p.timeOffset += drift  // Накопичувальна корекція
    }
}

// Застосування корекції при створенні нового TimeItem:
func (p *VideoManifestProxy) createTimeItem(seqNum int) *m3u8.TimeItem {
    baseTime := p.startTime.Add(time.Duration(seqNum) * p.segmentDuration)
    return &m3u8.TimeItem{Time: baseTime.Add(p.timeOffset).UTC()}  // ✅ З корекцією
}
```

### 🎯 Сценарій: синхронізація субтитрів з відео через `PROGRAM-DATE-TIME`
```go
// У WebSocketDistributor при отриманні субтитрів:
func (d *Distributor) onSubtitleMessage(msg SubtitleMessage) {
    // 🎯 msg.start_time_utc = абсолютний час початку субтитру (UTC)
    
    // 🎯 Знайти відео-сегмент, що містить цей час:
    targetTime := msg.start_time_utc
    matchingSegment := d.findSegmentByTime(targetTime)  // Binary search по PROGRAM-DATE-TIME
    
    if matchingSegment != nil {
        // 🎯 Прив'язка субтитру до відео через спільний таймштамп:
        subtitle := &Subtitle{
            Time:      msg.start_time_utc,
            Text:      msg.Arabic,  // + переклади
            VideoSeq:  matchingSegment.SeqNum,  // 🔗 Посилання на відео
            Duration:  msg.time_end - msg.time_start,
        }
        d.broadcastSubtitle(subtitle)
    }
}

// Helper: пошук сегмента за часом (O(log n) завдяки сортуванню)
func (d *Distributor) findSegmentByTime(target time.Time) *SegmentItem {
    segments := d.playlist.Segments()  // Вже відсортовані за часом
    
    // 🎯 Binary search: перший сегмент з ProgramDateTime >= target
    idx := sort.Search(len(segments), func(i int) bool {
        if segments[i].ProgramDateTime == nil {
            return false
        }
        return !segments[i].ProgramDateTime.Time.Before(target)
    })
    
    if idx < len(segments) {
        return segments[idx]
    }
    return nil  // Час поза межами доступних сегментів
}
```

### 🎯 Сценарій: валідація послідовності таймштампів
```go
// У monitoring.Monitor для виявлення розривів:
func (m *Monitor) validateTimestamps(items []Item) []TimeGap {
    var gaps []TimeGap
    var prevTime *time.Time
    
    for _, item := range items {
        if ti, ok := item.(*m3u8.TimeItem); ok {
            if prevTime != nil {
                gap := ti.Time.Sub(*prevTime)
                
                // 🎯 Виявлення аномалій:
                if gap < 0 {
                    m.alerts["time_regression"].Inc()  // Час "повернувся назад"
                } else if gap > 2*time.Second {
                    // 🎯 Розрив >2с: можлива втрата сегментів
                    gaps = append(gaps, TimeGap{
                        Start: *prevTime,
                        End:   ti.Time,
                        Duration: gap,
                    })
                    m.alerts["time_gap"].Inc()
                }
            }
            t := ti.Time  // Копія для безпеки
            prevTime = &t
        }
    }
    return gaps
}
```

---

## 🧪 Приклад використання: повний цикл

```go
// ✅ Створення TimeItem з поточним часом
now := time.Now().UTC()
ti := &m3u8.TimeItem{Time: now}
fmt.Println(ti.String())
// #EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00.123456789Z

// ✅ Парсинг вхідного рядка (стандартний формат)
line := "#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00.000Z"
ti1, err := m3u8.NewTimeItem(line)
if err != nil {
    log.Fatal(err)
}
fmt.Println(ti1.Time.Format(time.RFC3339))  // 2024-01-15T14:30:00Z

// ✅ Парсинг з альтернативним форматом (без двокрапки в зоні)
line2 := "#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00.000+0200"
ti2, err := m3u8.NewTimeItem(line2)
if err != nil {
    log.Fatal(err)
}
fmt.Println(ti2.Time.UTC())  // 2024-01-15 12:30:00 +0000 UTC ✅

// ✅ Форматування часу поза контекстом тега
custom := m3u8.FormatTime(time.Date(2024, 1, 15, 14, 30, 0, 123000000, time.UTC))
fmt.Println(custom)  // 2024-01-15T14:30:00.123000000Z

// ✅ Обробка помилок парсингу
_, err = m3u8.NewTimeItem("#EXT-X-PROGRAM-DATE-TIME:invalid-time")
fmt.Println(err)  // failed to parse time "invalid-time": unsupported format

// ✅ Використання у плейлисті
pl := m3u8.NewPlaylist()
pl.AppendItem(&m3u8.TimeItem{Time: time.Now().UTC()})
pl.AppendItem(&m3u8.SegmentItem{
    Duration: 4.0,
    Segment:  "seg1.ts",
})
fmt.Println(pl.String())
/*
#EXTM3U
#EXT-X-TARGETDURATION:10
#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00.123456789Z
#EXTINF:4.000,
seg1.ts
*/
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги

```
✅ #EXT-X-PROGRAM-DATE-TIME — опціональний, але РЕКОМЕНДОВАНИЙ для:
   • Live-плейлистів (синхронізація з реальним часом)
   • Архівування та пошуку за часом
   • Кореляції з іншими джерелами даних

✅ Формат: рядок у форматі RFC 3339 / ISO 8601
   • Обов'язково: дата, час, часова зона
   • Рекомендується: наносекундна точність (але достатньо мілісекунд)

✅ Часова зона:
   • ПОВИНЕН бути UTC (індикатор "Z" або "+00:00")
   • Інші зони допускаються, але можуть бути некоректно оброблені плеєрами

✅ Частота:
   • Може з'являтися перед будь-яким #EXTINF
   • Рекомендується: перед КОЖНИМ сегментом для максимальної точності
   • Якщо пропущено: клієнт екстраполює час на основі попереднього + Duration

✅ Послідовність:
   • Часи мають бути монотонно зростаючими (або рівними при розривах)
   • Розрив > цільової тривалості сегмента може вказувати на втрату даних

✅ Сумісність:
   • Клієнти МАЮТЬ підтримувати формати з різними варіантами часової зони
   • Клієнти МОЖУТЬ ігнорувати частини < мілісекунд для продуктивності
```

---

## 🎯 Висновок

Цей код — **критичний компонент** для точної синхронізації у вашому CCTV HLS Processor:

✅ Гнучкий парсинг різних форматів часових зон  
✅ Чіткий контракт: парсинг ↔ серіалізація через інтерфейс `Item`  
✅ Інтеграція з `time.Time` для зручних обчислень

**Для вашого проекту — критичні рекомендації**:

1. ✅ Нормалізувати час до UTC у `String()` та `NewTimeItem()`
2. ✅ Додати валідацію `!Time.IsZero()` для уникнення некоректних виводів
3. ✅ Розглянути формат до мілісекунд для економії трафіку (якщо наносекунди не потрібні)
4. ✅ Додати метод `DurationSince(other *TimeItem)` для зручного розрахунку drift
5. 🔒 Забезпечити immutable патерн або mutex при конкурентному доступі

**Приклад оптимізації для low-latency CCTV**:
```go
// Для мінімальної затримки: точні таймштампи + корекція в реальному часі
const (
    MaxAllowedDrift = 500 * time.Millisecond  // Поріг для тривоги
    SyncInterval    = 10 * time.Second         // Частота перевірки синхронізації
)

func (sf *SegmentFinalizer) syncWithServerTime(serverTime time.Time) {
    lastTi := sf.getLastProgramDateTime()
    if lastTi == nil {
        return
    }
    
    expected := lastTi.Time.Add(sf.segmentDuration)
    drift := serverTime.Sub(expected)
    
    if drift.Abs() > MaxAllowedDrift {
        sf.logger.Warn("significant time drift", 
            "drift_ms", drift.Milliseconds(),
            "action", "adjusting future timestamps")
        sf.timeOffset += drift  // Плавна корекція
    }
}
```

Потрібно допомогти з:
- 🧠 Реалізацією `DurationSince()` та інших helper-методів для `TimeItem`?
- 🔗 Інтеграцією корекції drift у ваш `VideoManifestProxy`?
- 🧪 Юніт-тестами для `ParseTime` з різними форматами часових зон?

Чекаю на ваші питання! 🛠️⏱️