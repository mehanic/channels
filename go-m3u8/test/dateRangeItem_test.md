# 🔍 Глибокий розбір тестів: `DateRangeItem` для HLS `#EXT-X-DATERANGE`

Цей файл містить **комплексні юніт-тести** для парсингу та серіалізації тега `#EXT-X-DATERANGE` — потужного механізму HLS для маркування подій у часовій шкалі (рекламні вставки, програмні сегменти, метадані). Розберемо архітектурно та детально.

---

## 📦 Що таке `#EXT-X-DATERANGE` і навіщо він потрібен?

### Контекст: маркування подій у HLS
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4

#EXT-X-DATERANGE:ID="ad-break-1",CLASS="com.example.ad",START-DATE="2024-01-15T14:30:00Z",DURATION=30.0,SCTE35-OUT=0xFC002F...
#EXTINF:4.0,
seg1000.ts
#EXTINF:4.0,
seg1001.ts
#EXT-X-DATERANGE:ID="ad-break-1",SCTE35-IN=0xFC002F...
#EXTINF:4.0,
seg1002.ts

#EXT-X-DATERANGE:ID="program-segment",CLASS="com.example.program",START-DATE="2024-01-15T14:31:00Z",END-DATE="2024-01-15T14:35:00Z"
```

### Призначення атрибутів `DateRangeItem`
| Атрибут | Тип | Обов'язковий? | Призначення |
|---------|-----|---------------|-------------|
| `ID` | `string` | ✅ Так | Унікальний ідентифікатор події в межах плейлиста |
| `CLASS` | `*string` | ❌ Ні | Категорія події: `"com.example.ad"`, `"com.example.program"` |
| `START-DATE` | `string` | ✅ Так | Початок події у форматі RFC 3339 (UTC) |
| `END-DATE` | `*string` | ❌ Ні | Кінець події (альтернатива `DURATION`) |
| `DURATION` | `*float64` | ❌ Ні | Тривалість у секундах (альтернатива `END-DATE`) |
| `PLANNED-DURATION` | `*float64` | ❌ Ні | Запланована тривалість (для попереднього маркування) |
| `SCTE35-CMD/OUT/IN` | `*string` | ❌ Ні | SCTE-35 команди для вставки реклами (hex-формат) |
| `END-ON-NEXT` | `bool` | ❌ Ні | Завершити подію на наступному `#EXT-X-DATERANGE` з тим же `ID` |
| `ClientAttributes` | `map[string]string` | ❌ Ні | Vendor-specific атрибути з префіксом `X-` |

### 🎯 Критичні сценарії використання у вашому проекті
```
📺 CCTV з рекламними вставками:
• #EXT-X-DATERANGE з SCTE35-OUT → початок рекламного блоку
• #EXT-X-DATERANGE з SCTE35-IN → кінець блоку, повернення до основного контенту
• Плеєр автоматично обробляє вставки без розриву відтворення

📊 Аналітика переглядів:
• Маркування програмних сегментів: "Новини", "Інтерв'ю", "Погода"
• Кореляція метрик перегляду з конкретними сегментами
• Пошук в архіві: "показати всі сегменти класу 'news'"

🔗 Інтеграція з зовнішніми системами:
• X-CUSTOM-ATTRIBUTES для передачі метадань у плеєр
• Синхронізація з EPG (Electronic Program Guide)
• Webhook-сповіщення при досягненні маркованої точки
```

---

## 🔬 Детальний розбір кожного тесту

### Тест 1: `TestDateRangeItem_Parse` — повний набір атрибутів

```go
func TestDateRangeItem_Parse(t *testing.T) {
    // 🎯 Вхідний рядок: багаторядковий формат з усіма опціональними атрибутами
    line := `#EXT-X-DATERANGE:ID="splice-6FFFFFF0",CLASS="test_class",
START-DATE="2014-03-05T11:15:00Z",
END-DATE="2014-03-05T11:16:00Z",DURATION=60.1,
PLANNED-DURATION=59.993,
SCTE35-CMD=0xFC002F0000000000FF2,
SCTE35-OUT=0xFC002F0000000000FF0,
SCTE35-IN=0xFC002F0000000000FF1,
END-ON-NEXT=YES
`
    // 🎯 Парсинг (парсер має об'єднати рядки та розібрати атрибути)
    dri, err := m3u8.NewDateRangeItem(line)
    assert.Nil(t, err)
    
    // 🎯 Перевірка обов'язкових полів (звичайні ассерції)
    assert.Equal(t, "splice-6FFFFFF0", dri.ID)
    assert.Equal(t, "2014-03-05T11:15:00Z", dri.StartDate)
    
    // 🎯 Перевірка опціональних полів-покажчиків (через хелпер)
    assertNotNilEqual(t, "test_class", dri.Class)              // *string
    assertNotNilEqual(t, "2014-03-05T11:16:00Z", dri.EndDate)  // *string
    assertNotNilEqual(t, 60.1, dri.Duration)                   // *float64
    assertNotNilEqual(t, 59.993, dri.PlannedDuration)          // *float64
    assertNotNilEqual(t, "0xFC002F0000000000FF2", dri.Scte35Cmd)   // *string (hex)
    assertNotNilEqual(t, "0xFC002F0000000000FF0", dri.Scte35Out)   // *string (hex)
    assertNotNilEqual(t, "0xFC002F0000000000FF1", dri.Scte35In)    // *string (hex)
    
    // 🎯 Перевірка булевого прапорця
    assert.True(t, dri.EndOnNext)  // END-ON-NEXT=YES → true
    
    // 🎯 Перевірка відсутності client-атрибутів
    assert.Nil(t, dri.ClientAttributes)
    
    // 🎯 Кругова перевірка: серіалізація має відтворити оригінал
    assertToString(t, line, dri)  // \n будуть нормалізовані хелпером
}
```

### Тест 2: `TestDateRangeItem_Parse_2` — мінімальний набір (тільки обов'язкові)

```go
func TestDateRangeItem_Parse_2(t *testing.T) {
    // 🎯 Тільки ID + START-DATE (мінімум за специфікацією)
    line := `#EXT-X-DATERANGE:ID="splice-6FFFFFF0",
START-DATE="2014-03-05T11:15:00Z"
`
    dri, err := m3u8.NewDateRangeItem(line)
    assert.Nil(t, err)
    
    // ✅ Обов'язкові поля
    assert.Equal(t, "splice-6FFFFFF0", dri.ID)
    assert.Equal(t, "2014-03-05T11:15:00Z", dri.StartDate)
    
    // ✅ Всі опціональні поля = nil / false за замовчуванням
    assert.Nil(t, dri.Class)
    assert.Nil(t, dri.EndDate)
    assert.Nil(t, dri.Duration)
    assert.Nil(t, dri.PlannedDuration)
    assert.Nil(t, dri.Scte35In)
    assert.Nil(t, dri.Scte35Out)
    assert.Nil(t, dri.Scte35Cmd)
    assert.Nil(t, dri.ClientAttributes)
    assert.False(t, dri.EndOnNext)  // За замовчуванням = false
    
    assertToString(t, line, dri)
}
```

### Тест 3: `TestDateRangeItem_Parse_3` — vendor-specific атрибути (`X-*`)

```go
func TestDateRangeItem_Parse_3(t *testing.T) {
    // 🎯 Атрибути з префіксом "X-" → потрапляють у ClientAttributes map
    line := `#EXT-X-DATERANGE:ID="splice-6FFFFFF0",
START-DATE="2014-03-05T11:15:00Z",
X-CUSTOM-VALUE="test_value"
`
    dri, err := m3u8.NewDateRangeItem(line)
    assert.Nil(t, err)
    
    // ✅ ClientAttributes ініціалізується тільки якщо є X-* атрибути
    assert.NotNil(t, dri.ClientAttributes)
    
    // ✅ Доступ до кастомного атрибута через map
    assert.Equal(t, "test_value", dri.ClientAttributes["X-CUSTOM-VALUE"])
    
    assertToString(t, line, dri)
}
```

---

## 🏗️ Припустима структура `DateRangeItem`

```go
type DateRangeItem struct {
    ID               string             // ✅ Обов'язковий: унікальний ідентифікатор
    StartDate        string             // ✅ Обов'язковий: RFC 3339 формат
    Class            *string            // Категорія події
    EndDate          *string            // Кінець події (альтернатива Duration)
    Duration         *float64           // Тривалість у секундах
    PlannedDuration  *float64           // Запланована тривалість
    Scte35Cmd        *string            // SCTE-35 команда (hex)
    Scte35Out        *string            // Маркер початку реклами (hex)
    Scte35In         *string            // Маркер кінця реклами (hex)
    EndOnNext        bool               // Прапорець завершення на наступному
    ClientAttributes map[string]string // Vendor-specific атрибути (X-*)
}
```

### 🎯 Чому `ClientAttributes` — окремий map?
```go
// Специфікація HLS дозволяє розширення через атрибути з префіксом "X-":
// • X-CUSTOM-FIELD="value"
// • X-ANALYTICS-ID="abc123"
// • X-AD-CAMPAIGN="winter_sale"

// ✅ Рішення: збирати всі X-* атрибути в окремий map
// • Не засмічувати основну структуру невідомими полями
// • Дозволити гнучке розширення без змін коду парсера
// • Клієнт сам вирішує, як обробляти кастомні атрибути

// Приклад парсингу:
if strings.HasPrefix(key, "X-") {
    if dri.ClientAttributes == nil {
        dri.ClientAttributes = make(map[string]string)
    }
    dri.ClientAttributes[key] = value
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Багаторядковий вхід: чи коректно обробляє парсер?
```go
// ❌ Вхідний рядок у тестах містить переноси:
line := `#EXT-X-DATERANGE:ID="x",
START-DATE="2024-01-01T00:00:00Z"
`

// 🎯 Питання: чи видаляє парсер \n перед ParseAttributes?
// ✅ Очікувана поведінка: об'єднати рядки → один рядок атрибутів

// ❌ Якщо парсер не обробляє \n → помилка парсингу
// ✅ Тест повинен явно перевіряти стійкість до форматування:
func TestDateRangeItem_MultilineTolerance(t *testing.T) {
    cases := []string{
        `#EXT-X-DATERANGE:ID="x",START-DATE="2024-01-01T00:00:00Z"`,  // Один рядок
        `#EXT-X-DATERANGE:ID="x",` + "\n" + `START-DATE="2024-01-01T00:00:00Z"`,  // \n
        `#EXT-X-DATERANGE:ID="x",` + "\r\n" + `START-DATE="2024-01-01T00:00:00Z"`, // \r\n
    }
    
    for _, input := range cases {
        t.Run("format", func(t *testing.T) {
            dri, err := m3u8.NewDateRangeItem(input)
            assert.NoError(t, err, "parser should handle multiline input")
            assert.Equal(t, "x", dri.ID)
        })
    }
}
```

### 2️⃣ Валідація взаємовиключності `END-DATE` / `DURATION`
```go
// ✅ Специфікація: можна вказати АБО EndDate, АБО Duration (не обидва)
// ❌ Поточні тести не перевіряють цю валідацію

// ✅ Додати тест на конфліктні атрибути:
func TestDateRangeItem_Validate_MutuallyExclusive(t *testing.T) {
    // ❌ Обидва вказані → помилка валідації
    line := `#EXT-X-DATERANGE:ID="x",START-DATE="2024-01-01T00:00:00Z",END-DATE="2024-01-01T00:01:00Z",DURATION=60.0`
    dri, err := m3u8.NewDateRangeItem(line)
    
    // Залежить від реалізації:
    // • Варіант А: помилка валідації
    assert.Error(t, err, "END-DATE and DURATION are mutually exclusive")
    
    // • Варіант Б: пріоритет EndDate, ігнорування Duration (документувати!)
    // assert.NoError(t, err)
    // assert.NotNil(t, dri.EndDate)
    // assert.Nil(t, dri.Duration)  // або значення, але з попередженням
}
```

### 3️⃣ Формат `START-DATE` / `END-DATE`: валідація RFC 3339
```go
// ❌ Тести використовують тільки валідні дати
// ✅ Додати перевірки на невалідні формати:

func TestDateRangeItem_InvalidDateFormats(t *testing.T) {
    cases := []struct{
        name  string
        date  string
        wantErr bool
    }{
        {"valid_rfc3339", "2024-01-15T14:30:00Z", false},
        {"valid_with_offset", "2024-01-15T14:30:00+02:00", false},
        {"invalid_no_time", "2024-01-15", true},  // ❌ Тільки дата
        {"invalid_spaces", "2024-01-15 14:30:00", true},  // ❌ Пробіл замість T
        {"empty", "", true},  // ❌ Порожній рядок
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            line := fmt.Sprintf(`#EXT-X-DATERANGE:ID="x",START-DATE="%s"`, tc.date)
            _, err := m3u8.NewDateRangeItem(line)
            if tc.wantErr {
                assert.Error(t, err, "expected error for invalid date: %s", tc.date)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### 4️⃣ Парсинг hex-значень `SCTE35-*`
```go
// ✅ Тести перевіряють збереження рядка, але не валідацію формату
// ❌ Що якщо SCTE35-CMD="not-hex"? Парсер має прийняти чи відхилити?

// ✅ Додати валідацію hex-формату (опціонально, але корисно):
func isValidSCTE35Hex(s string) bool {
    if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
        return false  // SCTE-35 має починатися з 0x
    }
    hexPart := strings.TrimPrefix(strings.ToLower(s), "0x")
    for _, r := range hexPart {
        if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
            return false
        }
    }
    return len(hexPart) > 0
}

// Використання у парсері:
if scte35Cmd := attrs["SCTE35-CMD"]; scte35Cmd != "" {
    if !isValidSCTE35Hex(scte35Cmd) {
        return nil, fmt.Errorf("invalid SCTE35-CMD format (expected hex): %s", scte35Cmd)
    }
    dri.Scte35Cmd = &scte35Cmd
}
```

### 5️⃣ Обробка `ClientAttributes`: регістр ключів
```go
// ✅ Специфікація: ключі чутливі до регістру
// ❌ Тест не перевіряє, чи зберігається регістр:

func TestDateRangeItem_ClientAttributes_CaseSensitive(t *testing.T) {
    line := `#EXT-X-DATERANGE:ID="x",START-DATE="2024-01-01T00:00:00Z",X-MyAttr="val1",x-myattr="val2"`
    dri, err := m3u8.NewDateRangeItem(line)
    assert.NoError(t, err)
    
    // ✅ Ключі мають бути чутливі до регістру:
    assert.Equal(t, "val1", dri.ClientAttributes["X-MyAttr"])
    assert.Equal(t, "val2", dri.ClientAttributes["x-myattr"])  // Окремий ключ!
}
```

### 6️⃣ Назви тестів: нумерація замість опису
```go
// ❌ Поточні назви:
TestDateRangeItem_Parse_2      // Що саме тестується?
TestDateRangeItem_Parse_3      // Чим відрізняється від _2?

// ✅ Рекомендовані описові назви:
func TestDateRangeItem_Parse_FullAttributes(t *testing.T)      // Тест 1
func TestDateRangeItem_Parse_MinimalRequired(t *testing.T)     // Тест 2  
func TestDateRangeItem_Parse_ClientAttributes(t *testing.T)    // Тест 3

// ✅ Або використання subtests:
func TestDateRangeItem(t *testing.T) {
    t.Run("Parse/FullAttributes", func(t *testing.T) { ... })
    t.Run("Parse/MinimalRequired", func(t *testing.T) { ... })
    t.Run("Parse/ClientAttributes", func(t *testing.T) { ... })
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **синхронізацією подій та субтитрів**:

### 🎯 Сценарій: маркування рекламних вставок через SCTE-35
```go
// У segmentFinalizer при отриманні SCTE-35 маркерів:
func (sf *SegmentFinalizer) addAdBreakMarker(scte35Out, scte35In string, startTime time.Time, duration float64) {
    // 🎯 Створення DateRangeItem для початку реклами
    adStart := &m3u8.DateRangeItem{
        ID:          fmt.Sprintf("ad-%d", time.Now().UnixNano()),
        StartDate:   startTime.UTC().Format(time.RFC3339),
        Class:       pointer.ToString("com.alarabiya.ad"),
        Duration:    pointer.ToFloat64(duration),
        Scte35Out:   pointer.ToString(scte35Out),  // Hex-команда
        EndOnNext:   false,
    }
    sf.playlist.AppendItem(adStart)
    
    // 🎯 Якщо є кінець реклами (SCTE35-IN) — окремий маркер
    if scte35In != "" {
        adEnd := &m3u8.DateRangeItem{
            ID:        adStart.ID,  // ✅ Той самий ID = продовження тієї ж події
            StartDate: startTime.Add(time.Duration(duration) * time.Second).UTC().Format(time.RFC3339),
            Scte35In:  pointer.ToString(scte35In),
            EndOnNext: true,  // ✅ Завершити на наступному маркері
        }
        sf.playlist.AppendItem(adEnd)
    }
}
```

### 🎯 Сценарій: синхронізація субтитрів з програмними сегментами
```go
// У WebSocketDistributor при отриманні субтитрів з метаданими:
func (d *Distributor) onSubtitleWithSegment(msg SubtitleMessage) {
    if msg.SegmentClass != "" {
        // 🎯 Створення DateRangeItem для програмного сегмента
        segment := &m3u8.DateRangeItem{
            ID:          fmt.Sprintf("segment-%s-%d", msg.SegmentClass, msg.Seq),
            Class:       pointer.ToString(msg.SegmentClass),  // "news", "interview", "weather"
            StartDate:   msg.start_time_utc.Format(time.RFC3339),
            Duration:    pointer.ToFloat64(msg.Duration),
            // 🎯 Кастомні атрибути для плеєра
            ClientAttributes: map[string]string{
                "X-SUBTITLE-LANG": msg.Language,
                "X-TOPIC":         msg.Topic,
            },
        }
        d.playlist.AppendItem(segment)
        
        // 🎯 Синхронізація: субтитри прив'язані до сегмента через ID
        msg.DateRangeID = segment.ID  // 🔗 Зв'язок для клієнта
    }
}
```

### 🎯 Сценарій: фільтрація плейлиста за класом подій
```go
// У клієнтському плеєрі або проксі для аналітики:
func filterDateRangesByClass(items []m3u8.Item, class string) []*m3u8.DateRangeItem {
    var result []*m3u8.DateRangeItem
    for _, item := range items {
        if dri, ok := item.(*m3u8.DateRangeItem); ok {
            if dri.Class != nil && *dri.Class == class {
                result = append(result, dri)
            }
        }
    }
    return result
}

// Використання: отримати всі рекламні вставки
adBreaks := filterDateRangesByClass(pl.Items, "com.alarabiya.ad")
for _, ad := range adBreaks {
    log.Printf("Ad break %s: %s (+%.1fs)", ad.ID, ad.StartDate, *ad.Duration)
}
```

### 🎯 Сценарій: валідація DateRangeItem перед додаванням у плейлист
```go
// У segmentFinalizer для забезпечення валідності:
func (sf *SegmentFinalizer) validateDateRange(dri *m3u8.DateRangeItem) error {
    // ✅ Обов'язкові поля
    if dri.ID == "" {
        return fmt.Errorf("DateRangeItem.ID is required")
    }
    if dri.StartDate == "" {
        return fmt.Errorf("DateRangeItem.StartDate is required")
    }
    
    // ✅ Валідація формату дати
    if _, err := time.Parse(time.RFC3339, dri.StartDate); err != nil {
        return fmt.Errorf("invalid StartDate format: %w", err)
    }
    if dri.EndDate != nil {
        if _, err := time.Parse(time.RFC3339, *dri.EndDate); err != nil {
            return fmt.Errorf("invalid EndDate format: %w", err)
        }
    }
    
    // ✅ Взаємовиключність EndDate/Duration
    if dri.EndDate != nil && dri.Duration != nil {
        return fmt.Errorf("EndDate and Duration are mutually exclusive")
    }
    
    // ✅ Валідація SCTE-35 hex-формату
    for name, value := range map[string]*string{
        "SCTE35-CMD": dri.Scte35Cmd,
        "SCTE35-OUT": dri.Scte35Out,
        "SCTE35-IN":  dri.Scte35In,
    } {
        if value != nil && !isValidSCTE35Hex(*value) {
            return fmt.Errorf("invalid %s format (expected hex): %s", name, *value)
        }
    }
    
    return nil
}
```

---

## 🧪 Приклад: розширені тести для `DateRangeItem`

```go
// ✅ Повний набір тестів з subtests та валідацією:
func TestDateRangeItem(t *testing.T) {
    t.Parallel()
    
    t.Run("Parse/FullAttributes", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-DATERANGE:ID="x",CLASS="ad",START-DATE="2024-01-01T00:00:00Z",END-DATE="2024-01-01T00:01:00Z",DURATION=60.0,SCTE35-OUT=0xABCD,END-ON-NEXT=YES`
        dri, err := m3u8.NewDateRangeItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "x", dri.ID)
        assertNotNilEqual(t, "ad", dri.Class)
        assertNotNilEqual(t, 60.0, dri.Duration)
        assert.True(t, dri.EndOnNext)
        assertToString(t, line, dri)
    })
    
    t.Run("Parse/MinimalRequired", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-DATERANGE:ID="min",START-DATE="2024-01-01T00:00:00Z"`
        dri, err := m3u8.NewDateRangeItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "min", dri.ID)
        assert.Nil(t, dri.Class)  // Опціональні = nil
        assert.False(t, dri.EndOnNext)  // Булеві = false за замовчуванням
    })
    
    t.Run("Parse/ClientAttributes", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-DATERANGE:ID="x",START-DATE="2024-01-01T00:00:00Z",X-CUSTOM="val",X-ANOTHER=123`
        dri, err := m3u8.NewDateRangeItem(line)
        
        assert.NoError(t, err)
        assert.NotNil(t, dri.ClientAttributes)
        assert.Equal(t, "val", dri.ClientAttributes["X-CUSTOM"])
        assert.Equal(t, "123", dri.ClientAttributes["X-ANOTHER"])  // Усі значення як string
    })
    
    t.Run("Parse/Invalid/MissingID", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-DATERANGE:START-DATE="2024-01-01T00:00:00Z"`  // ❌ Без ID
        _, err := m3u8.NewDateRangeItem(line)
        assert.Error(t, err, "ID is required")
    })
    
    t.Run("Parse/Invalid/MissingStartDate", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-DATERANGE:ID="x"`  // ❌ Без START-DATE
        _, err := m3u8.NewDateRangeItem(line)
        assert.Error(t, err, "START-DATE is required")
    })
    
    t.Run("Parse/Invalid/ConflictingEndAndDuration", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-DATERANGE:ID="x",START-DATE="2024-01-01T00:00:00Z",END-DATE="2024-01-01T00:01:00Z",DURATION=60.0`
        _, err := m3u8.NewDateRangeItem(line)
        // Залежить від реалізації: помилка або попередження
        assert.Error(t, err, "EndDate and Duration are mutually exclusive")
    })
    
    t.Run("Parse/MultilineTolerance", func(t *testing.T) {
        t.Parallel()
        // ✅ Парсер має обробляти переноси рядків у атрибутах
        line := "#EXT-X-DATERANGE:ID=\"x\",\nSTART-DATE=\"2024-01-01T00:00:00Z\"\n"
        dri, err := m3u8.NewDateRangeItem(line)
        assert.NoError(t, err)
        assert.Equal(t, "x", dri.ID)
    })
    
    t.Run("RoundTrip/WithClientAttributes", func(t *testing.T) {
        t.Parallel()
        original := `#EXT-X-DATERANGE:ID="x",START-DATE="2024-01-01T00:00:00Z",X-CUSTOM="val"`
        dri, err := m3u8.NewDateRangeItem(original)
        assert.NoError(t, err)
        
        output := dri.String()
        // Нормалізувати для порівняння (видалити \n)
        assert.Equal(t, normalizeM3U8(original), normalizeM3U8(output))
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до `#EXT-X-DATERANGE`

```
✅ ID — обов'язковий, унікальний в межах плейлиста, рядок без пробілів
✅ START-DATE — обов'язковий, формат RFC 3339 (ISO 8601), UTC рекомендовано
✅ Класи подій (CLASS):
   • Рекомендується зворотний DNS-формат: "com.example.ad", "org.example.program"
   • Клієнти можуть фільтрувати/ігнорувати за класом
✅ END-DATE та DURATION — взаємовиключні:
   • Можна вказати тільки один з них
   • Якщо жоден не вказаний: подія триває до наступного #EXT-X-DATERANGE з тим же ID
✅ SCTE35-* атрибути:
   • Формат: hex-рядок, опціонально з префіксом "0x"
   • SCTE35-OUT: початок рекламного блоку
   • SCTE35-IN: кінець рекламного блоку
   • SCTE35-CMD: команда для вставки (рідше використовується)
✅ END-ON-NEXT=YES:
   • Завершити поточну подію з тим же ID при наступному #EXT-X-DATERANGE
   • Корисно для розділення події на кілька сегментів плейлиста
✅ Client Attributes (X-*):
   • Будь-які атрибути з префіксом "X-" вважаються vendor-specific
   • Клієнти МАЮТЬ ігнорувати невідомі X-* атрибути (forward compatibility)
   • Ключі чутливі до регістру: "X-MyAttr" ≠ "x-myattr"
✅ Порядок #EXT-X-DATERANGE:
   • Має з'являтися ПЕРЕД першим сегментом, до якого відноситься
   • Може з'являтися кілька разів для однієї події (через END-ON-NEXT)
```

---

## 🎯 Висновок

Ці тести — **солідна основа** для валідації `DateRangeItem`:

✅ Покриття повного, мінімального та розширеного (X-*) наборів атрибутів  
✅ Перевірка булевих, числових та рядкових типів  
✅ Кругова перевірка серіалізації через `assertToString`

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати валідацію обов'язкових полів (`ID`, `START-DATE`) у конструкторі
2. ✅ Перевірити стійкість парсера до багаторядкового формату атрибутів
3. ✅ Додати тести на взаємовиключність `END-DATE`/`DURATION`
4. ✅ Валідувати формат `RFC3339` для дат та `hex` для `SCTE35-*`
5. ✅ Перейменувати тести за описовим патерном або використати subtests

**Приклад інтеграції для Al Arabiya CCTV**:
```go
// Маркування новинних сегментів з підтримкою багатомовних субтитрів:
func (sf *SegmentFinalizer) markNewsSegment(startTime time.Time, duration float64, topic string) {
    segment := &m3u8.DateRangeItem{
        ID:          fmt.Sprintf("news-%d", startTime.Unix()),
        Class:       pointer.ToString("com.alarabiya.program.news"),
        StartDate:   startTime.UTC().Format(time.RFC3339),
        Duration:    pointer.ToFloat64(duration),
        ClientAttributes: map[string]string{
            "X-TOPIC-AR": topic,  // Арабська назва теми
            "X-TOPIC-EN": translateToEnglish(topic),
            "X-TOPIC-UK": translateToUkrainian(topic),
        },
    }
    sf.playlist.AppendItem(segment)
    
    // 📢 Синхронізувати з WebSocketDistributor для субтитрів
    sf.distributor.NotifySegmentStart(segment.ID, topic)
}
```

Потрібно допомогти з:
- 🧠 Реалізацією валідації `DateRangeItem` у вашому `segmentFinalizer`?
- 🔗 Інтеграцією `ClientAttributes` для передачі багатомовних метадань?
- 🧪 Написанням fuzz-тестів для пошуку крайніх випадків парсингу?

Чекаю на ваші питання! 🛠️📅