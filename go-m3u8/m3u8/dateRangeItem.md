# Глибоке роз'яснення: `m3u8.DateRangeItem` — парсинг та серіалізація #EXT-X-DATERANGE тегів для HLS

Цей файл містить **реалізацію роботи з #EXT-X-DATERANGE тегами** — потужним інструментом у HLS для позначення часових діапазонів у контенті (реклама, події, маркери). Це критично для динамічної вставки контенту, аналітики та синхронізації з зовнішніми системами.

---

## 🎯 Навіщо `DateRangeItem` потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ DateRangeItem у контексті HLS:         │
│                                         │
│ 🔹 Маркування подій у стрімі:          │
│   • Рекламні вставки (SCTE-35 маркери) │
│   • Важливі моменти у CCTV записі      │
│     (тривога, рух, звук)               │
│   • Синхронізація з зовнішніми даними  │
│                                         │
│ 🔹 Динамічна вставка контенту:         │
│   • Server-side ad insertion (SSAI)    │
│   • Перемикання джерел сигналу         │
│   • Адаптація бітрейту на основі подій │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Позначення інцидентів у записі     │
│   • Інтеграція з системами відео-      │
│     аналітики (детекція руху, облич)   │
│   • Експорт маркерів для пошуку подій  │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `DateRangeItem`: представлення #EXT-X-DATERANGE

```go
type DateRangeItem struct {
    // 🔹 Обов'язкові поля
    ID        string  // 🎯 Унікальний ідентифікатор діапазону (обов'язковий)
    StartDate string  // 🎯 Час початку у форматі RFC3339 (обов'язковий)
    
    // 🔹 Опціональні часові параметри
    EndDate         *string   // 🎯 Час кінцю (RFC3339), nil = невизначений кінець
    Duration        *float64  // 🎯 Тривалість у секундах
    PlannedDuration *float64  // 🎯 Планована тривалість (для live-контенту)
    
    // 🔹 Класифікація
    Class *string  // 🎯 Категорія діапазону: "com.apple.hls.interstitial", "ad"...
    
    // 🔹 SCTE-35 маркери для динамічної вставки реклами
    Scte35Cmd *string  // 🎯 SCTE-35 splice_command() у base64
    Scte35Out *string  // 🎯 Маркер початку реклами
    Scte35In  *string  // 🎯 Маркер кінця реклами
    
    // 🔹 Логіка завершення
    EndOnNext bool  // 🎯 Завершити цей діапазон, коли почнеться інший з тією ж Class
    
    // 🔹 Користувацькі атрибути (X-*)
    ClientAttributes map[string]string  // 🎯 Довільні метадані: X-event_type="alarm"
}
```

### 🎯 Формат #EXT-X-DATERANGE у HLS:

```
#EXT-X-DATERANGE:ID="unique_id",START-DATE="2024-01-15T10:30:00Z",DURATION=30.5,CLASS="ad",X-event_type="preroll"

Розбір:
• ID="unique_id" → dri.ID = "unique_id"
• START-DATE="..." → dri.StartDate = "2024-01-15T10:30:00Z"
• DURATION=30.5 → dri.Duration = &30.5
• CLASS="ad" → dri.Class = &"ad"
• X-event_type="preroll" → dri.ClientAttributes["X-event_type"] = "preroll"
```

> 💡 **Важливо**: `StartDate` та `EndDate` використовують **рядок**, а не `time.Time`. Це дозволяє зберегти точний формат вхідних даних, але вимагає додаткового парсингу для порівнянь.

---

## 🔍 Функція `NewDateRangeItem`: парсинг текстового представлення

```go
func NewDateRangeItem(text string) (*DateRangeItem, error) {
    // 🔹 1. Парсинг загальних атрибутів
    attributes := ParseAttributes(text)
    
    // 🔹 2. Парсинг числових полів з обробкою помилок
    duration, err := parseFloat(attributes, DurationTag)
    if err != nil { return nil, err }
    
    plannedDuration, err := parseFloat(attributes, PlannedDurationTag)  // 🔹 Опечатка: plannedDuartion
    if err != nil { return nil, err }
    
    // 🔹 3. Побудова структури
    return &DateRangeItem{
        ID:               attributes[IDTag],  // 🔹 Порожній рядок, якщо відсутній
        Class:            pointerTo(attributes, ClassTag),
        StartDate:        attributes[StartDateTag],  // 🔹 Порожній = помилка валідації пізніше
        EndDate:          pointerTo(attributes, EndDateTag),
        Duration:         duration,
        PlannedDuration:  plannedDuration,  // 🔹 Використовуємо виправлену змінну
        Scte35Cmd:        pointerTo(attributes, Scte35CmdTag),
        Scte35Out:        pointerTo(attributes, Scte35OutTag),
        Scte35In:         pointerTo(attributes, Scte35InTag),
        EndOnNext:        attributeExists(EndOnNextTag, attributes),  // 🔹 true якщо ключ є
        ClientAttributes: parseClientAttributes(attributes),  // 🔹 Фільтрація X-* атрибутів
    }, nil
}
```

### ⚠️ Критичні моменти:

#### 🔹 Опечатка у змінній: `plannedDuartion`

```go
plannedDuartion, err := parseFloat(attributes, PlannedDurationTag)  // ❌ Duartion
// ...
PlannedDuration: plannedDuartion,  // 🔹 Використовуємо ту саму опечатку
```

**Наслідок:**
```
• Код працює, бо змінна використовується консистентно
• Але це вводить в оману при читанні коду
• Може призвести до помилок при майбутньому рефакторингу

✅ Рішення: виправити на `plannedDuration`
```

#### 🔹 Порожній `ID` або `StartDate` не є помилкою парсингу

```go
ID:        attributes[IDTag],        // "" якщо ключ відсутній
StartDate: attributes[StartDateTag], // "" якщо ключ відсутній
```

**Проблема:**
```
• Специфікація вимагає, щоб ID та START-DATE були обов'язковими
• Але парсер не повертає помилку, якщо вони відсутні
• Це може призвести до невалідних плейлистів

✅ Рішення: додати валідацію після парсингу:
  if dri.ID == "" { return nil, fmt.Errorf("ID is required") }
  if dri.StartDate == "" { return nil, fmt.Errorf("START-DATE is required") }
```

#### 🔹 `EndOnNext` через `attributeExists`

```go
EndOnNext: attributeExists(EndOnNextTag, attributes)
```

**Логіка:**
```
• Якщо атрибут END-ON-NEXT є (будь-яке значення) → true
• Якщо відсутній → false
• Значення "YES"/"NO" ігноруються (за специфікацією достатньо наявності)

Це відповідає специфікації: END-ON-NEXT — це прапорець, не булеве значення.
```

---

## 🔍 Метод `String()`: серіалізація у формат HLS

```go
func (dri *DateRangeItem) String() string {
    var slice []string
    
    // 🔹 Обов'язкові атрибути (завжди в лапках)
    slice = append(slice, fmt.Sprintf(quotedFormatString, IDTag, dri.ID))
    slice = append(slice, fmt.Sprintf(quotedFormatString, StartDateTag, dri.StartDate))
    
    // 🔹 Опціональні рядкові атрибути (тільки якщо не nil)
    if dri.Class != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, ClassTag, *dri.Class))
    }
    if dri.EndDate != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, EndDateTag, *dri.EndDate))
    }
    
    // 🔹 Числові атрибути (без лапок)
    if dri.Duration != nil {
        slice = append(slice, fmt.Sprintf(formatString, DurationTag, *dri.Duration))
    }
    if dri.PlannedDuration != nil {
        slice = append(slice, fmt.Sprintf(formatString, PlannedDurationTag, *dri.PlannedDuration))
    }
    
    // 🔹 Користувацькі атрибути (X-*)
    clientAttributes := formatClientAttributes(dri.ClientAttributes)
    slice = append(slice, clientAttributes...)
    
    // 🔹 SCTE-35 атрибути
    if dri.Scte35Cmd != nil {
        slice = append(slice, fmt.Sprintf(formatString, Scte35CmdTag, *dri.Scte35Cmd))
    }
    // ... аналогічно для Scte35Out, Scte35In
    
    // 🔹 Прапорець EndOnNext
    if dri.EndOnNext {
        slice = append(slice, fmt.Sprintf(`%s=YES`, EndOnNextTag))
    }
    
    // 🔹 Фінальне форматування
    return fmt.Sprintf("%s:%s", DateRangeItemTag, strings.Join(slice, ","))
}
```

### 🎯 Приклад серіалізації:

```
Вхід:
  DateRangeItem{
      ID: "ad-break-1",
      StartDate: "2024-01-15T10:30:00Z",
      Duration: float64Ptr(30.5),
      Class: stringPtr("ad"),
      ClientAttributes: {"X-campaign_id": "summer2024"},
      EndOnNext: false,
  }

Вихід:
  #EXT-X-DATERANGE:ID="ad-break-1",START-DATE="2024-01-15T10:30:00Z",CLASS="ad",DURATION=30.5,X-campaign_id="summer2024"
```

### 🔹 Форматування клієнтських атрибутів: автоматичне визначення лапок

```go
func formatClientAttributes(ca map[string]string) []string {
    if ca == nil { return nil }
    
    var slice []string
    for key, value := range ca {
        formatString := `%s=%s`  // 🔹 Дефолт: без лапок
        
        // 🔹 Спробувати парсити як число
        _, err := strconv.ParseFloat(value, 64)
        if err != nil {
            // 🔹 Не число → додати лапки
            formatString = `%s="%s"`
        }
        
        slice = append(slice, fmt.Sprintf(formatString, key, value))
    }
    return slice
}
```

**Логіка:**
```
• "X-count=42" → ParseFloat("42") succeeds → формат "%s=%s" → X-count=42
• "X-name=John" → ParseFloat("John") fails → формат "%s=\"%s\"" → X-name="John"
• "X-price=19.99" → ParseFloat succeeds → X-price=19.99

Це забезпечує сумісність: числа без лапок, рядки — у лапках.
```

> ⚠️ **Потенційна проблема**: "123abc" не парситься як float → отримає лапки, хоча це не валідне число. Краще використовувати явну типізацію або валідацію.

---

## 🔍 Функції роботи з клієнтськими атрибутами

### `parseClientAttributes`: фільтрація `X-*` атрибутів

```go
func parseClientAttributes(attributes map[string]string) map[string]string {
    result := make(map[string]string)
    hasCA := false
    
    for key, value := range attributes {
        if strings.HasPrefix(key, "X-") {  // 🔹 Тільки атрибути, що починаються з "X-"
            result[key] = value
            hasCA = true
        }
    }
    
    if hasCA {
        return result
    }
    return nil  // 🔹 nil замість порожньої мапи для економії пам'яті
}
```

**Призначення:**
```
• Виділити користувацькі метадані зі стандартних атрибутів
• nil повертається, якщо немає X-* атрибутів → менше алокацій
• Клієнтський код може перевіряти: `if dri.ClientAttributes != nil { ... }`
```

### `formatClientAttributes`: зворотне перетворення у рядки

```go
func formatClientAttributes(ca map[string]string) []string {
    if ca == nil { return nil }
    
    var slice []string
    for key, value := range ca {
        // 🔹 Автоматичне визначення формату (див. вище)
        // ...
    }
    return slice
}
```

**Особливість:**
```
• Повертає []string, а не один рядок → гнучкість у побудові фінального тега
• Порядок атрибутів не гарантований (мапа в Go не впорядкована)
• Для детермінованого виходу можна сортувати ключі перед ітерацією
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Додавання маркерів подій у плейлист

```go
// У VideoManifestProxy — позначення важливих моментів у записі:
func addEventMarker(playlist *HLSPlaylist, eventType string, timestamp time.Time, metadata map[string]string) {
    dri := &m3u8.DateRangeItem{
        ID:        fmt.Sprintf("event-%s-%d", eventType, timestamp.Unix()),
        StartDate: timestamp.Format(time.RFC3339),
        Duration:  float64Ptr(1.0),  // 🔹 Миттєва подія = 1 секунда
        Class:     stringPtr("cctv-event"),
        ClientAttributes: map[string]string{
            "X-event_type": eventType,
            "X-severity":   "high",
        },
    }
    
    // 🔹 Додати клієнтські метадані
    for k, v := range metadata {
        dri.ClientAttributes["X-"+k] = v
    }
    
    // 🔹 Додати тег у плейлист
    playlist.AddTag(dri.String())
}

func float64Ptr(f float64) *float64 { return &f }
func stringPtr(s string) *string { return &s }
```

### ✅ 2: Обробка SCTE-35 маркерів для динамічної реклами

```go
// У segmentAssembler — інтеграція з SSAI системами:
func handleSCTE35Marker(scte35Cmd string, scte35Out string) *m3u8.DateRangeItem {
    return &m3u8.DateRangeItem{
        ID:        fmt.Sprintf("scte35-%d", time.Now().UnixNano()),
        StartDate: time.Now().UTC().Format(time.RFC3339),
        Class:     stringPtr("com.apple.hls.interstitial"),  // 🔹 Стандартний клас для реклами
        Scte35Cmd: &scte35Cmd,
        Scte35Out: &scte35Out,
        EndOnNext: true,  // 🔹 Завершити, коли почнеться наступна реклама
    }
}

// Використання:
marker := handleSCTE35Marker(base64Cmd, outMarker)
playlist.WriteString("#" + marker.String() + "\n")
```

### ✅ 3: Валідація `DateRangeItem` перед записом

```go
// Перевірити, що діапазон валідний перед додаванням у плейлист:
func validateDateRange(dri *m3u8.DateRangeItem) error {
    // 🔹 Обов'язкові поля
    if dri.ID == "" {
        return fmt.Errorf("DateRange ID is required")
    }
    if dri.StartDate == "" {
        return fmt.Errorf("DateRange START-DATE is required")
    }
    
    // 🔹 Перевірити формат дат (RFC3339)
    if _, err := time.Parse(time.RFC3339, dri.StartDate); err != nil {
        return fmt.Errorf("invalid START-DATE format: %w", err)
    }
    if dri.EndDate != nil {
        if _, err := time.Parse(time.RFC3339, *dri.EndDate); err != nil {
            return fmt.Errorf("invalid END-DATE format: %w", err)
        }
    }
    
    // 🔹 Логічні перевірки
    if dri.Duration != nil && *dri.Duration < 0 {
        return fmt.Errorf("DURATION cannot be negative: %f", *dri.Duration)
    }
    if dri.PlannedDuration != nil && *dri.PlannedDuration < 0 {
        return fmt.Errorf("PLANNED-DURATION cannot be negative: %f", *dri.PlannedDuration)
    }
    
    // 🔹 EndOnNext вимагає Class
    if dri.EndOnNext && dri.Class == nil {
        return fmt.Errorf("END-ON-NEXT requires CLASS to be set")
    }
    
    return nil
}
```

### ✅ 4: Моніторинг використання DateRange

```go
// monitoring.Monitor — метрики для маркерів подій:
type DateRangeMetrics struct {
    MarkersAdded      *prometheus.CounterVec  // кількість доданих маркерів
    MarkerTypes       *prometheus.CounterVec  // розподіл за Class/X-event_type
    DurationDistribution *prometheus.HistogramVec  // розподіл тривалостей
    ValidationErrors  *prometheus.CounterVec  // помилки валідації
}

// У процесі додавання маркера:
func monitorDateRange(channelID string, dri *m3u8.DateRangeItem, 
                     metrics *DateRangeMetrics, err error) {
    
    if err != nil {
        metrics.ValidationErrors.WithLabelValues(channelID).Inc()
        log.Warnf("Channel %s: invalid DateRange: %v", channelID, err)
        return
    }
    
    metrics.MarkersAdded.WithLabelValues(channelID).Inc()
    
    // 🔹 Тип маркера
    if dri.Class != nil {
        metrics.MarkerTypes.WithLabelValues(channelID, *dri.Class).Inc()
    }
    if eventType, ok := dri.ClientAttributes["X-event_type"]; ok {
        metrics.MarkerTypes.WithLabelValues(channelID, "X-"+eventType).Inc()
    }
    
    // 🔹 Тривалість
    if dri.Duration != nil {
        metrics.DurationDistribution.WithLabelValues(channelID).Observe(*dri.Duration)
    }
}
```

### ✅ 5: Пошук подій за часовим діапазоном

```go
// Знайти всі маркери у певному інтервалі часу:
func findDateRangesInInterval(ranges []*m3u8.DateRangeItem, 
                             start, end time.Time) []*m3u8.DateRangeItem {
    
    var result []*m3u8.DateRangeItem
    
    for _, dr := range ranges {
        // 🔹 Парсити StartDate
        startTime, err := time.Parse(time.RFC3339, dr.StartDate)
        if err != nil { continue }  // 🔹 Пропустити невалідні
        
        // 🔹 Визначити endTime
        endTime := startTime
        if dr.EndDate != nil {
            if t, err := time.Parse(time.RFC3339, *dr.EndDate); err == nil {
                endTime = t
            }
        } else if dr.Duration != nil {
            endTime = startTime.Add(time.Duration(*dr.Duration * float64(time.Second)))
        }
        
        // 🔹 Перевірити перетин з [start, end]
        if endTime.After(start) && startTime.Before(end) {
            result = append(result, dr)
        }
    }
    
    return result
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на парсинг базового DateRange

```go
func TestNewDateRangeItem_Basic(t *testing.T) {
    input := `ID="test-1",START-DATE="2024-01-15T10:30:00Z",DURATION=30.5,CLASS="ad"`
    
    dri, err := NewDateRangeItem(input)
    assert.NoError(t, err)
    assert.NotNil(t, dri)
    
    assert.Equal(t, "test-1", dri.ID)
    assert.Equal(t, "2024-01-15T10:30:00Z", dri.StartDate)
    assert.NotNil(t, dri.Duration)
    assert.InDelta(t, 30.5, *dri.Duration, 0.001)
    assert.NotNil(t, dri.Class)
    assert.Equal(t, "ad", *dri.Class)
    assert.Nil(t, dri.EndDate)  // 🔹 Опціональне поле відсутнє
}
```

### 🔹 Тест на клієнтські атрибути

```go
func TestNewDateRangeItem_ClientAttributes(t *testing.T) {
    input := `ID="event-1",START-DATE="2024-01-15T10:30:00Z",X-type="alarm",X-severity=5`
    
    dri, err := NewDateRangeItem(input)
    assert.NoError(t, err)
    
    // 🔹 Перевірити фільтрацію X-* атрибутів
    assert.NotNil(t, dri.ClientAttributes)
    assert.Equal(t, "alarm", dri.ClientAttributes["X-type"])
    assert.Equal(t, "5", dri.ClientAttributes["X-severity"])  // 🔹 Значення залишається рядком
    
    // 🔹 Стандартні атрибути не потрапляють у ClientAttributes
    assert.NotContains(t, dri.ClientAttributes, "ID")
    assert.NotContains(t, dri.ClientAttributes, "START-DATE")
}
```

### 🔹 Тест на серіалізацію

```go
func TestDateRangeItem_String(t *testing.T) {
    dri := &DateRangeItem{
        ID:        "test-1",
        StartDate: "2024-01-15T10:30:00Z",
        Duration:  float64Ptr(30.5),
        Class:     stringPtr("ad"),
        ClientAttributes: map[string]string{
            "X-campaign": "summer",
            "X-count":    "42",  // 🔹 Число без лапок
        },
        EndOnNext: true,
    }
    
    result := dri.String()
    
    // 🔹 Перевірити обов'язкові атрибути
    assert.Contains(t, result, `ID="test-1"`)
    assert.Contains(t, result, `START-DATE="2024-01-15T10:30:00Z"`)
    
    // 🔹 Опціональні атрибути
    assert.Contains(t, result, `CLASS="ad"`)
    assert.Contains(t, result, `DURATION=30.5`)  // 🔹 Без лапок
    
    // 🔹 Клієнтські атрибути
    assert.Contains(t, result, `X-campaign="summer"`)  // 🔹 Рядок → лапки
    assert.Contains(t, result, `X-count=42`)           // 🔹 Число → без лапок
    
    // 🔹 Прапорець
    assert.Contains(t, result, `END-ON-NEXT=YES`)
    
    // 🔹 Префікс тега
    assert.Contains(t, result, `#EXT-X-DATERANGE:`)
}
```

### 🔹 Тест на валідацію обов'язкових полів

```go
func TestNewDateRangeItem_Validation(t *testing.T) {
    // 🔹 Відсутній ID
    _, err := NewDateRangeItem(`START-DATE="2024-01-15T10:30:00Z"`)
    // ❌ Поточний код НЕ повертає помилку — це проблема!
    // ✅ Потрібно додати валідацію після парсингу
    
    // 🔹 Відсутній StartDate
    _, err = NewDateRangeItem(`ID="test-1"`)
    // ❌ Також не повертає помилку
    
    // 🔹 Невалідний формат числа
    _, err = NewDateRangeItem(`ID="test-1",START-DATE="2024-01-15T10:30:00Z",DURATION=abc`)
    assert.Error(t, err)  // ✅ parseFloat повертає помилку
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Опечатка `plannedDuartion` | Плутанина при читанні коду, ризик помилок при рефакторингу | 🔹 Виправити на `plannedDuration` |
| Відсутня валідація обов'язкових полів | Невалідні плейлисти з порожнім ID/StartDate | 🔹 Додати `validateDateRange()` після парсингу |
| `StartDate` як рядок, не `time.Time` | Неможливість порівняння дат без додаткового парсингу | 🔹 Додати метод `StartTime() time.Time` з кешуванням |
| Недетермінований порядок клієнтських атрибутів | Різний вихід при однакових вхідних даних | 🔹 Сортувати ключі мапи перед ітерацією у `formatClientAttributes` |
| Автоматичне визначення лапок для X-* атрибутів | "123abc" отримує лапки, хоча не валідне число | 🔹 Використовувати явну типізацію або валідацію формату |

### Приклад додавання валідації:

```go
func NewDateRangeItemValidated(text string) (*DateRangeItem, error) {
    dri, err := NewDateRangeItem(text)
    if err != nil {
        return nil, err
    }
    
    if err := validateDateRange(dri); err != nil {
        return nil, fmt.Errorf("invalid DateRange: %w", err)
    }
    
    return dri, nil
}
```

### Приклад методу для парсингу часу:

```go
func (dri *DateRangeItem) StartTime() (time.Time, error) {
    return time.Parse(time.RFC3339, dri.StartDate)
}

func (dri *DateRangeItem) EndTime() (*time.Time, error) {
    if dri.EndDate != nil {
        t, err := time.Parse(time.RFC3339, *dri.EndDate)
        if err != nil {
            return nil, err
        }
        return &t, nil
    }
    if dri.Duration != nil {
        start, err := dri.StartTime()
        if err != nil {
            return nil, err
        }
        t := start.Add(time.Duration(*dri.Duration * float64(time.Second)))
        return &t, nil
    }
    return nil, nil  // 🔹 Невизначений кінець
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Створення маркера події:
func makeEventMarker(id, eventType, startDate string, duration float64) *m3u8.DateRangeItem {
    return &m3u8.DateRangeItem{
        ID:        id,
        StartDate: startDate,
        Duration:  &duration,
        Class:     stringPtr("cctv-event"),
        ClientAttributes: map[string]string{
            "X-event_type": eventType,
        },
    }
}

// 2: Парсинг з валідацією:
func safeParseDateRange(line string) (*m3u8.DateRangeItem, error) {
    if !strings.HasPrefix(line, "#EXT-X-DATERANGE:") {
        return nil, fmt.Errorf("not a DATERANGE tag")
    }
    
    attrs := strings.TrimPrefix(line, "#EXT-X-DATERANGE:")
    dri, err := NewDateRangeItem(attrs)
    if err != nil {
        return nil, err
    }
    
    return dri, validateDateRange(dri)
}

// 3: Форматування для запису у файл:
func writeDateRangeTag(w io.Writer, dri *m3u8.DateRangeItem) error {
    _, err := w.WriteString(dri.String() + "\n")
    return err
}

// 4: Логування для відладки:
func logDateRange(dri *m3u8.DateRangeItem) {
    log.Debugf("DateRange: ID=%s, Start=%s, Duration=%v, Class=%s", 
        dri.ID, dri.StartDate, dri.Duration, deref(dri.Class))
    
    for k, v := range dri.ClientAttributes {
        log.Debugf("  %s = %q", k, v)
    }
}

// 5: Фільтрація за типом події:
func filterByEventType(ranges []*m3u8.DateRangeItem, eventType string) []*m3u8.DateRangeItem {
    var result []*m3u8.DateRangeItem
    for _, dr := range ranges {
        if dr.ClientAttributes["X-event_type"] == eventType {
            result = append(result, dr)
        }
    }
    return result
}

func deref(s *string) string { if s != nil { return *s }; return "" }
```

---

## 📊 Матриця атрибутів #EXT-X-DATERANGE

```
Атрибут          | Тип       | Обов'язковий? | Приклад значення       | Призначення
─────────────────┼───────────┼───────────────┼────────────────────────┼─────────────────────────
ID               | string    | ✅ Так        | "ad-break-1"           | 🔹 Унікальний ідентифікатор
START-DATE       | string    | ✅ Так        | "2024-01-15T10:30:00Z" | 🔹 Час початку (RFC3339)
END-DATE         | string    | ❌ Ні         | "2024-01-15T10:31:00Z" | 🔹 Час кінцю (альтернатива DURATION)
DURATION         | float64   | ❌ Ні         | 30.5                   | 🔹 Тривалість у секундах
PLANNED-DURATION | float64   | ❌ Ні         | 30.0                   | 🔹 Планована тривалість для live
CLASS            | string    | ❌ Ні         | "ad", "cctv-event"     | 🔹 Категорія для групування
SCTE35-CMD       | string    | ❌ Ні         | "base64..."            | 🔹 SCTE-35 команда для SSAI
SCTE35-OUT       | string    | ❌ Ні         | "base64..."            | 🔹 Маркер початку реклами
SCTE35-IN        | string    | ❌ Ні         | "base64..."            | 🔹 Маркер кінця реклами
END-ON-NEXT      | bool      | ❌ Ні         | YES                    | 🔹 Автозавершення при наступному діапазоні
X-*              | string    | ❌ Ні         | X-event_type="alarm"   | 🔹 Користувацькі метадані
```

---

## 📚 Корисні посилання

- [HLS RFC Draft: EXT-X-DATERANGE](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.2.7)
- [SCTE-35 specification for ad insertion](https://www.scte.org/standards/scte-35/)
- [Apple HLS Authoring Specification: Date Range Tags](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [Go time package: RFC3339 format](https://pkg.go.dev/time#RFC3339)

> 💡 **Ключова ідея**: `DateRangeItem` — це "маркер подій" у вашому HLS пайплайні. Він:
> - 🎯 Дозволяє точно позначати часові діапазони для реклами, інцидентів, синхронізації
> - 🔧 Підтримує як стандартні атрибути, так і довільні користувацькі метадані (X-*)
> - ⚡ Забезпечує двосторонню конвертацію між текстовим форматом та типобезпечною структурою
> - 🛡️ Граційно обробляє опціональні поля через покажчики та окрему валідацію

Якщо потрібно — можу допомогти:
- 🔄 Виправити опечатку `plannedDuartion` та додати валідацію обов'язкових полів
- 🧪 Написати integration-тест для перевірки коректності серіалізації/десеріалізації DateRangeItem
- 📈 Додати Prometheus-метрики для моніторингу кількості та типів маркерів подій по каналах

🛠️