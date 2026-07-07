# 🔍 Глибокий розбір тестів: `TimeItem` для HLS `#EXT-X-PROGRAM-DATE-TIME`

Цей файл містить **два мінімалістичні юніт-тести** для парсингу та серіалізації тега `#EXT-X-PROGRAM-DATE-TIME` — механізму прив'язки абсолютного часу UTC до медіа-сегментів у HLS. Розберемо архітектурно та детально.

---

## 📦 Що таке `#EXT-X-PROGRAM-DATE-TIME` і навіщо він потрібен?

### Контекст: синхронізація часу у HLS
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4

#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00.000Z
#EXTINF:4.0,
seg1000.ts

#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:04.000Z
#EXTINF:4.0,
seg1001.ts
```

### Призначення `TimeItem`
| Аспект | Пояснення |
|--------|-----------|
| **Формат** | Рядок у форматі RFC 3339 / ISO 8601: `YYYY-MM-DDTHH:MM:SS.sssZ` |
| **Часова зона** | Завжди **UTC** (індикатор `Z` або `+00:00`) |
| **Точність** | До наносекунд (9 знаків після коми), але зазвичай мілісекунди |
| **Частота** | Рекомендується перед **кожним** сегментом для точної синхронізації |

### 🎯 Критичні сценарії використання у вашому проекті
```
🔴 Live CCTV моніторинг:
• Клієнт бачить точний час події: "14:30:04" → кореляція з іншими камерами
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

## 🔬 Детальний розбір кожного тесту

### Тест 1: `TestTimeItem_New` — створення + серіалізація

```go
func TestTimeItem_New(t *testing.T) {
    // 🎯 Парсинг часу через helper-функцію
    timeVar, err := m3u8.ParseTime("2010-02-19T14:54:23.031Z")
    assert.Nil(t, err)
    
    // 🎯 Створення TimeItem з розпаршеним часом
    ti := &m3u8.TimeItem{
        Time: timeVar,
    }
    
    // 🎯 Перевірка серіалізації: має повернути точний формат тега
    assert.Equal(t, "#EXT-X-PROGRAM-DATE-TIME:2010-02-19T14:54:23.031Z", ti.String())
}
```

#### 🎯 Що тестує цей кейс?
| Аспект | Перевірка | Чому це важливо |
|--------|-----------|----------------|
| **ParseTime** | `"2010-02-19T14:54:23.031Z"` → `time.Time` | Гнучкий парсер для різних форматів часових зон |
| **Структура TimeItem** | `Time: timeVar` | Проста обгортка навколо `time.Time` |
| **String() серіалізація** | `time.Time` → `"#EXT-X-PROGRAM-DATE-TIME:..."` | Точний формат для сумісності з плеєрами |
| **Точність часу** | `.031` (мілісекунди) зберігається | Критично для синхронізації субпіксельної точності |

#### 🎯 Припустима реалізація `TimeItem.String()`
```go
const dateTimeFormat = time.RFC3339Nano  // "2006-01-02T15:04:05.999999999Z07:00"

func (ti *TimeItem) String() string {
    // ✅ Нормалізація до UTC перед серіалізацією
    return fmt.Sprintf("%s:%s", TimeItemTag, ti.Time.UTC().Format(dateTimeFormat))
}
```

---

### Тест 2: `TestTimeItem_Parse` — парсинг з рядка

```go
func TestTimeItem_Parse(t *testing.T) {
    // 🎯 Парсинг повного рядка тегу
    ti, err := m3u8.NewTimeItem("#EXT-X-PROGRAM-DATE-TIME:2010-02-19T14:54:23.031Z")
    assert.Nil(t, err)
    
    // 🎯 Очікуваний результат: time.Time у форматі RFC3339Nano
    expected, err := time.Parse(time.RFC3339Nano, "2010-02-19T14:54:23.031Z")
    assert.Nil(t, err)
    
    // 🎯 Порівняння розпаршених часів
    assert.Equal(t, expected, ti.Time)
}
```

#### 🎯 Що тестує цей кейс?
| Аспект | Перевірка | Чому це важливо |
|--------|-----------|----------------|
| **NewTimeItem парсинг** | `#EXT-X-PROGRAM-DATE-TIME:...` → `*TimeItem` | Видалення префіксу тегу + парсинг часу |
| **ParseTime гнучкість** | Підтримка різних форматів часових зон | Сумісність з різними генераторами плейлистів |
| **Точність порівняння** | `time.Time == time.Time` | Go порівнює часи за значенням, включаючи наносекунди |

#### 🎯 Припустима реалізація `NewTimeItem`
```go
func NewTimeItem(text string) (*TimeItem, error) {
    // 🎯 Видалення префіксу тегу
    timeString := strings.TrimPrefix(text, TimeItemTag+":")
    
    // 🎯 Парсинг через гнучкий парсер
    t, err := ParseTime(timeString)
    if err != nil {
        return nil, err
    }
    
    return &TimeItem{Time: t}, nil
}
```

#### 🎯 Припустима реалізація `ParseTime` (з тесту раніше)
```go
func ParseTime(value string) (time.Time, error) {
    // 🎯 Три варіанти формату для сумісності:
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
            return t, nil
        }
    }
    
    return t, err  // Повертаємо останню помилку
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність нормалізації до UTC
```go
// ❌ Поточний код може зберігати будь-яку часову зону:
t, _ := time.Parse(time.RFC3339, "2024-01-15T16:30:00+02:00")
ti := &TimeItem{Time: t}
fmt.Println(ti.String())  
// #EXT-X-PROGRAM-DATE-TIME:2024-01-15T16:30:00+02:00  ← ❌ Не UTC!

// ✅ Специфікація HLS вимагає UTC:
// RFC 8216 §4.3.2.3: "The date and time SHOULD be in UTC"

// ✅ Рішення: нормалізувати при створенні або серіалізації
func (ti *TimeItem) String() string {
    return fmt.Sprintf("%s:%s", TimeItemTag, ti.Time.UTC().Format(dateTimeFormat))  // ✅ Примусовий UTC
}

func NewTimeItem(text string) (*TimeItem, error) {
    // ... парсинг ...
    return &TimeItem{Time: t.UTC()}, nil  // ✅ Нормалізація одразу
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

// ✅ Або повертати помилку у конструкторі при парсингу "порожнього" рядка
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

### 4️⃣ Відсутність тестів на помилки парсингу
```go
// ✅ Додати перевірки невалідного вводу:
func TestTimeItem_Parse_Invalid(t *testing.T) {
    cases := []struct{
        name  string
        input string
        wantErr bool
    }{
        {"invalid_format", "#EXT-X-PROGRAM-DATE-TIME:2024-01-15", true},  // ❌ Тільки дата
        {"invalid_spaces", "#EXT-X-PROGRAM-DATE-TIME:2024-01-15 14:30:00", true},  // ❌ Пробіл замість T
        {"empty_time", "#EXT-X-PROGRAM-DATE-TIME:", true},  // ❌ Порожній час
        {"valid_with_offset", "#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00+02:00", false},  // ✅ Автоматична конвертація в UTC
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            ti, err := m3u8.NewTimeItem(tc.input)
            if tc.wantErr {
                assert.Error(t, err)
                assert.Nil(t, ti)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, ti)
                // ✅ Перевірка, що час нормалізовано до UTC
                assert.Equal(t, "UTC", ti.Time.Location().String())
            }
        })
    }
}
```

### 5️⃣ Назви тестів: `New` vs `Parse` — що саме тестується?
```go
// ❌ Поточні назви можуть бути неочевидними:
TestTimeItem_New      // Тестує створення + серіалізацію?
TestTimeItem_Parse    // Тестує парсинг?

// ✅ Рекомендовані описові назви:
func TestTimeItem_Serialize_FromTime(t *testing.T)      // Тест 1
func TestTimeItem_Parse_FromString(t *testing.T)        // Тест 2

// ✅ Або використання subtests:
func TestTimeItem(t *testing.T) {
    t.Run("Serialize/WithMilliseconds", func(t *testing.T) { ... })
    t.Run("Parse/WithRFC3339Nano", func(t *testing.T) { ... })
    t.Run("Parse/Invalid/EmptyString", func(t *testing.T) { ... })
    t.Run("RoundTrip/Consistency", func(t *testing.T) { ... })
}
```

### 6️⃣ Відсутність кругової перевірки (round-trip)
```go
// ✅ Додати тест, що парсинг + серіалізація = оригінал:
func TestTimeItem_RoundTrip(t *testing.T) {
    original := "#EXT-X-PROGRAM-DATE-TIME:2010-02-19T14:54:23.031Z"
    
    // 🎯 Парсинг
    ti, err := m3u8.NewTimeItem(original)
    assert.NoError(t, err)
    
    // 🎯 Серіалізація
    output := ti.String()
    
    // 🎯 Порівняння (з нормалізацією, якщо потрібно)
    assert.Equal(t, original, output)
    
    // 🎯 Додатково: перевірка, що time.Time однаковий
    expected, _ := time.Parse(time.RFC3339Nano, "2010-02-19T14:54:23.031Z")
    assert.Equal(t, expected, ti.Time)
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **синхронізацією аудіо/відео/субтитрів**:

### 🎯 Сценарій: генерація `#EXT-X-PROGRAM-DATE-TIME` у `segmentFinalizer`
```go
// У segmentFinalizer при створенні нового сегмента:
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
func (p *VideoManifestProxy) correctTimeDrift(serverTime time.Time, items []m3u8.Item) {
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
func (d *Distributor) findSegmentByTime(target time.Time) *m3u8.SegmentItem {
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

---

## 🧪 Приклад: розширений набір тестів для `TimeItem`

```go
// ✅ Повний набір тестів з subtests та валідацією:
func TestTimeItem(t *testing.T) {
    t.Parallel()
    
    t.Run("Serialize/WithMilliseconds", func(t *testing.T) {
        t.Parallel()
        timeVar, _ := m3u8.ParseTime("2010-02-19T14:54:23.031Z")
        ti := &m3u8.TimeItem{Time: timeVar}
        
        expected := "#EXT-X-PROGRAM-DATE-TIME:2010-02-19T14:54:23.031Z"
        assert.Equal(t, expected, ti.String())
    })
    
    t.Run("Parse/WithRFC3339Nano", func(t *testing.T) {
        t.Parallel()
        ti, err := m3u8.NewTimeItem("#EXT-X-PROGRAM-DATE-TIME:2010-02-19T14:54:23.031Z")
        assert.NoError(t, err)
        
        expected, _ := time.Parse(time.RFC3339Nano, "2010-02-19T14:54:23.031Z")
        assert.Equal(t, expected, ti.Time)
    })
    
    t.Run("Parse/WithTimezoneOffset", func(t *testing.T) {
        t.Parallel()
        // 🎯 Час з офсетом має бути нормалізований до UTC
        ti, err := m3u8.NewTimeItem("#EXT-X-PROGRAM-DATE-TIME:2024-01-15T16:30:00+02:00")
        assert.NoError(t, err)
        
        // 🎯 Очікуємо конвертацію в UTC: 16:30+02:00 = 14:30Z
        expected, _ := time.Parse(time.RFC3339Nano, "2024-01-15T14:30:00Z")
        assert.Equal(t, expected, ti.Time)
        assert.Equal(t, "UTC", ti.Time.Location().String())
    })
    
    t.Run("Parse/Invalid/EmptyTime", func(t *testing.T) {
        t.Parallel()
        _, err := m3u8.NewTimeItem("#EXT-X-PROGRAM-DATE-TIME:")
        assert.Error(t, err)
    })
    
    t.Run("RoundTrip/Consistency", func(t *testing.T) {
        t.Parallel()
        original := "#EXT-X-PROGRAM-DATE-TIME:2010-02-19T14:54:23.031Z"
        
        ti, err := m3u8.NewTimeItem(original)
        assert.NoError(t, err)
        
        output := ti.String()
        assert.Equal(t, original, output)
    })
    
    t.Run("Validate/ZeroTime", func(t *testing.T) {
        t.Parallel()
        ti := &m3u8.TimeItem{Time: time.Time{}}  // Zero value
        err := ti.Validate()  // ✅ Припустимо, метод існує
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "zero time")
    })
}
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
   • Інші зони допускаються, але мають бути конвертовані клієнтом

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

Ці тести — **мінімальна, але важлива перевірка** для `TimeItem`:

✅ Покриття базового парсингу та серіалізації  
✅ Перевірка точності до мілісекунд  
✅ Використання стандартного RFC3339Nano формату

**Для вашого проекту — критичні рекомендації**:

1. ✅ Нормалізувати час до UTC у `String()` та `NewTimeItem()`
2. ✅ Додати валідацію `!Time.IsZero()` для уникнення некоректних виводів
3. ✅ Розглянути формат до мілісекунд для економії трафіку (якщо наносекунди не потрібні)
4. ✅ Додати тести на невалідний ввід (порожній час, неправильний формат)
5. ✅ Додати кругову перевірку (round-trip) для гарантії консистентності

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
- 🧠 Реалізацією `Validate()` та інших helper-методів для `TimeItem`?
- 🔗 Інтеграцією корекції drift у ваш `VideoManifestProxy`?
- 🧪 Написанням бенчмарків для оцінки продуктивності парсингу часу?

Чекаю на ваші питання! 🛠️⏱️🎬