# Глибоке роз'яснення: `m3u8.ByteRange` — обробка байтових діапазонів для HLS

Цей файл містить **реалізацію парсингу та серіалізації байтових діапазонів** для HLS плейлистів. Це критичний компонент для підтримки `#EXT-X-BYTERANGE` тегів, які використовуються у I-frame-only плейлистах та для ефективного завантаження сегментів.

---

## 🎯 Навіщо `ByteRange` потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ ByteRange у контексті HLS:             │
│                                         │
│ 🔹 I-frame-only плейлисти:             │
│   • #EXT-X-BYTERANGE вказує позицію    │
│     та розмір I-frame у файлі          │
│   • Плеєр завантажує тільки потрібні   │
│     байти для швидкого seek            │
│                                         │
│ 🔹 Ефективність завантаження:          │
│   • Не завантажувати весь сегмент,     │
│     якщо потрібна тільки частина       │
│   • Економія трафіку для клієнтів      │
│   • Швидший старт відтворення          │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Швидкий пошук подій у довгих       │
│     записах через I-frame навігацію    │
│   • Підтримка прев'ю таймлайну         │
│   • Ефективне використання bandwidth   │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `ByteRange`: представлення діапазону

```go
type ByteRange struct {
    Length *int  // 🔹 Розмір діапазону у байтах (обов'язковий)
    Start  *int  // 🔹 Початкова позиція у файлі (опціональний)
}
```

### 🎯 Формат у HLS специфікації:

```
#EXT-X-BYTERANGE: n[@o]

де:
• n = Length (обов'язково) — кількість байт
• o = Start (опціонально) — позиція початку від початку файлу

Приклади:
• "376" → Length=376, Start=nil (початок з 0 або продовження попереднього)
• "376@3008" → Length=376, Start=3008 (явна позиція)
```

### 🎯 Коли Start є опціональним:

```
Сценарій 1: Перший I-frame у файлі
  #EXT-X-BYTERANGE:376@3008
  • Start=3008: I-frame починається з байта 3008
  • Length=376: I-frame займає 376 байт

Сценарій 2: Послідовні I-frames у тому ж файлі
  #EXT-X-BYTERANGE:376
  #EXT-X-BYTERANGE:400
  • Другий діапазон починається одразу після першого
  • Start не вказується → плеєр обчислює автоматично

Важливо:
✅ Якщо Start=nil, плеєр використовує кінець попереднього діапазону
✅ Якщо це перший діапазон у файлі і Start=nil → початок з 0
```

---

## 🔍 Функція `NewByteRange`: парсинг текстового представлення

```go
func NewByteRange(text string) (*ByteRange, error) {
    // 🔹 1. Обробка порожнього рядка
    if text == "" {
        return nil, nil  // 🔹 nil = немає байтового діапазону
    }
    
    // 🔹 2. Розділити за "@" на Length та Start
    values := strings.Split(text, "@")
    
    // 🔹 3. Парсинг Length (обов'язковий)
    lengthValue, err := strconv.Atoi(values[0])
    if err != nil {
        return nil, err  // 🔹 Помилка: нечислове значення
    }
    
    br := ByteRange{Length: &lengthValue}
    
    // 🔹 4. Парсинг Start (опціональний)
    if len(values) >= 2 {
        startValue, err := strconv.Atoi(values[1])
        if err != nil {
            return &br, err  // 🔹 Часткова помилка: Length валідний, Start — ні
        }
        br.Start = &startValue
    }
    
    return &br, nil
}
```

### 🎯 Ключові аспекти парсингу

#### 🔹 Обробка порожнього рядка

```go
if text == "" {
    return nil, nil
}
```

**Чому це важливо:**
```
• Не всі сегменти мають #EXT-X-BYTERANGE
• nil повертається для звичайних сегментів (повний файл)
• Клієнтський код має перевіряти: `if byteRange != nil { ... }`
```

#### 🔹 Часткова помилка при парсингу Start

```go
if len(values) >= 2 {
    startValue, err := strconv.Atoi(values[1])
    if err != nil {
        return &br, err  // 🔹 Повертаємо частково валідний об'єкт + помилку
    }
    br.Start = &startValue
}
```

**Наслідок:**
```
Вхід: "376@invalid"
Результат: 
• *ByteRange{Length: 376, Start: nil} + error
• Клієнт може використати Length, але має обробити помилку

Це приклад "graceful degradation" — не втрачати валідні дані через часткову помилку.
```

#### 🔹 Використання покажчиків (*int)

```go
Length *int  // не int!
Start  *int
```

**Чому покажчики:**
```
✅ Дозволяє розрізняти "не вказано" (nil) та "0"
• Start=nil → початок обчислюється автоматично
• Start=0 → явний початок з байта 0 (рідкісний випадок)

✅ Економія пам'яті при серіалізації
• При String(): якщо Start=nil, не додаємо "@0"

✅ Сумісність з JSON маршалінгом
• omitempty працює з покажчиками: "Start": null vs "Start": 0
```

---

## 🔍 Метод `String()`: серіалізація у формат HLS

```go
func (br *ByteRange) String() string {
    if br.Start == nil {
        return fmt.Sprintf("%d", *br.Length)  // 🔹 Тільки Length
    }
    return fmt.Sprintf("%d@%d", *br.Length, *br.Start)  // 🔹 Length@Start
}
```

### 🎯 Приклади серіалізації:

```
Вхід: ByteRange{Length: 376, Start: nil}
Вихід: "376"
HLS: #EXT-X-BYTERANGE:376

Вхід: ByteRange{Length: 376, Start: 3008}
Вихід: "376@3008"
HLS: #EXT-X-BYTERANGE:376@3008

Вхід: ByteRange{Length: 0, Start: 1000}  // ⚠️ Edge case
Вихід: "0@1000"
HLS: #EXT-X-BYTERANGE:0@1000  // ❌ Може бути невалідним у деяких плеєрах
```

> ⚠️ **Потенційна проблема**: `Length=0` може бути невалідним у деяких плеєрах. Рекомендується додати валідацію: `if *br.Length <= 0 { return "" }`.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Використання у I-frame playlist генерації

```go
// У iframe_playlist_generator — створення #EXT-X-BYTERANGE:
func createIFramePlaylistEntry(entry *IFrameEntry) string {
    byteRange := &m3u8.ByteRange{
        Length: intPtr(int(entry.PacketSize)),
        Start:  intPtr(int(entry.PacketPosition)),
    }
    
    return fmt.Sprintf("#EXT-X-BYTERANGE:%s\n%s", 
        byteRange.String(), entry.SegmentURI)
}

func intPtr(i int) *int { return &i }
```

### ✅ 2: Валідація байтових діапазонів перед записом

```go
// Перевірити, що діапазон валідний перед додаванням у плейлист:
func validateByteRange(br *m3u8.ByteRange, fileSize int64) error {
    if br == nil {
        return nil  // ✅ Немає діапазону = повний файл
    }
    
    if br.Length == nil {
        return fmt.Errorf("ByteRange.Length is required")
    }
    
    if *br.Length <= 0 {
        return fmt.Errorf("ByteRange.Length must be positive: %d", *br.Length)
    }
    
    if br.Start != nil {
        if *br.Start < 0 {
            return fmt.Errorf("ByteRange.Start cannot be negative: %d", *br.Start)
        }
        if int64(*br.Start)+int64(*br.Length) > fileSize {
            return fmt.Errorf("ByteRange [%d, %d) exceeds file size %d", 
                *br.Start, *br.Start+*br.Length, fileSize)
        }
    }
    
    return nil
}
```

### ✅ 3: Обчислення наступного діапазону для послідовних I-frames

```go
// Для I-frame playlist: якщо Start=nil, обчислити з попереднього діапазону
func computeNextByteRange(prev *m3u8.ByteRange, length int) *m3u8.ByteRange {
    var start *int
    
    if prev != nil && prev.Start != nil && prev.Length != nil {
        // 🔹 Наступний діапазон починається після попереднього
        nextStart := *prev.Start + *prev.Length
        start = &nextStart
    }
    // 🔹 Якщо prev=nil або Start=nil → start залишається nil (початок з 0)
    
    return &m3u8.ByteRange{
        Length: &length,
        Start:  start,
    }
}

// Використання:
var prevRange *m3u8.ByteRange
for _, entry := range iframeEntries {
    br := computeNextByteRange(prevRange, int(entry.PacketSize))
    playlist.WriteString(fmt.Sprintf("#EXT-X-BYTERANGE:%s\n", br.String()))
    prevRange = br
}
```

### ✅ 4: Моніторинг використання ByteRange

```go
// monitoring.Monitor — метрики для байтових діапазонів:
type ByteRangeMetrics struct {
    RangesParsed      *prometheus.CounterVec  // кількість розпарсених діапазонів
    RangesWithStart   *prometheus.CounterVec  // скільки мають явний Start
    RangeSizeDistribution *prometheus.HistogramVec  // розподіл розмірів
    ValidationErrors  *prometheus.CounterVec  // помилки валідації
}

// У процесі парсингу:
func monitorByteRange(channelID string, br *m3u8.ByteRange, 
                     metrics *ByteRangeMetrics, err error) {
    
    if br == nil {
        return  // ✅ Немає діапазону = нормальний випадок
    }
    
    metrics.RangesParsed.WithLabelValues(channelID).Inc()
    
    if br.Start != nil {
        metrics.RangesWithStart.WithLabelValues(channelID).Inc()
    }
    
    if br.Length != nil {
        metrics.RangeSizeDistribution.WithLabelValues(channelID).
            Observe(float64(*br.Length))
    }
    
    if err != nil {
        metrics.ValidationErrors.WithLabelValues(channelID).Inc()
        log.Warnf("Channel %s: ByteRange parse error: %v", channelID, err)
    }
}
```

### ✅ 5: Обробка помилок парсингу з retry

```go
// Стратегія: спробувати виправити поширені помилки формату
func parseByteRangeWithRecovery(text string) (*m3u8.ByteRange, error) {
    // 🔹 Спробувати стандартний парсинг
    br, err := m3u8.NewByteRange(text)
    if err == nil {
        return br, nil
    }
    
    // 🔹 Спроба 1: Видалити пробіли
    cleaned := strings.TrimSpace(text)
    if cleaned != text {
        if br, err := m3u8.NewByteRange(cleaned); err == nil {
            log.Debugf("Recovered ByteRange by trimming: %q → %q", text, cleaned)
            return br, nil
        }
    }
    
    // 🔹 Спроба 2: Замінити кому на крапку (для локалей)
    fixed := strings.Replace(text, ",", ".", -1)
    if fixed != text {
        if br, err := m3u8.NewByteRange(fixed); err == nil {
            log.Debugf("Recovered ByteRange by fixing locale: %q → %q", text, fixed)
            return br, nil
        }
    }
    
    // 🔹 Всі спроби вичерпано → повернути оригінальну помилку
    return nil, fmt.Errorf("failed to parse ByteRange %q: %w", text, err)
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на парсинг валідних діапазонів

```go
func TestNewByteRange_Valid(t *testing.T) {
    testCases := []struct {
        input    string
        expected *ByteRange
        hasError bool
    }{
        {"", nil, false},  // ✅ Порожній = nil
        {"376", &ByteRange{Length: intPtr(376)}, false},  // ✅ Тільки Length
        {"376@3008", &ByteRange{Length: intPtr(376), Start: intPtr(3008)}, false},  // ✅ Повний
        {"0@1000", &ByteRange{Length: intPtr(0), Start: intPtr(1000)}, false},  // ⚠️ Edge case
    }
    
    for _, tc := range testCases {
        t.Run(tc.input, func(t *testing.T) {
            result, err := NewByteRange(tc.input)
            
            if tc.hasError {
                assert.Error(t, err)
                return
            }
            
            assert.NoError(t, err)
            
            if tc.expected == nil {
                assert.Nil(t, result)
                return
            }
            
            assert.NotNil(t, result)
            assert.Equal(t, *tc.expected.Length, *result.Length)
            
            if tc.expected.Start == nil {
                assert.Nil(t, result.Start)
            } else {
                assert.NotNil(t, result.Start)
                assert.Equal(t, *tc.expected.Start, *result.Start)
            }
        })
    }
}

func intPtr(i int) *int { return &i }
```

### 🔹 Тест на помилки парсингу

```go
func TestNewByteRange_Errors(t *testing.T) {
    errorCases := []struct {
        input string
        desc  string
    }{
        {"abc", "non-numeric length"},
        {"376@xyz", "non-numeric start"},
        {"376@1000@extra", "too many parts"},  // strings.Split дасть ["376", "1000", "extra"]
    }
    
    for _, tc := range errorCases {
        t.Run(tc.desc, func(t *testing.T) {
            result, err := NewByteRange(tc.input)
            
            // 🔹 Для "376@xyz": має повернути частковий результат + помилку
            if tc.input == "376@xyz" {
                assert.NotNil(t, result)
                assert.Equal(t, 376, *result.Length)
                assert.Nil(t, result.Start)
                assert.Error(t, err)
                return
            }
            
            // 🔹 Інші випадки: повна помилка
            assert.Error(t, err)
            assert.Nil(t, result)
        })
    }
}
```

### 🔹 Тест на серіалізацію (String())

```go
func TestByteRange_String(t *testing.T) {
    testCases := []struct {
        input    *ByteRange
        expected string
    }{
        {&ByteRange{Length: intPtr(376), Start: nil}, "376"},
        {&ByteRange{Length: intPtr(376), Start: intPtr(3008)}, "376@3008"},
        {&ByteRange{Length: intPtr(0), Start: intPtr(1000)}, "0@1000"},  // ⚠️ Edge case
    }
    
    for _, tc := range testCases {
        t.Run(tc.expected, func(t *testing.T) {
            result := tc.input.String()
            assert.Equal(t, tc.expected, result)
        })
    }
}
```

### 🔹 Інтеграційний тест на використання у плейлисті

```go
func TestByteRange_InPlaylist(t *testing.T) {
    // 🔹 Створити тестовий I-frame playlist
    var builder strings.Builder
    builder.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-I-FRAMES-ONLY\n")
    
    entries := []struct {
        size uint
        pos  uint
    }{
        {376, 3008},
        {400, 0},  // 🔹 Start=0 → має серіалізуватися як "400@0"
        {500, 0},  // 🔹 Start=nil → має серіалізуватися як "500"
    }
    
    for i, e := range entries {
        var br *ByteRange
        if i == 0 {
            br = &ByteRange{Length: intPtr(int(e.size)), Start: intPtr(int(e.pos))}
        } else if i == 1 {
            br = &ByteRange{Length: intPtr(int(e.size)), Start: intPtr(0)}
        } else {
            br = &ByteRange{Length: intPtr(int(e.size))}  // Start=nil
        }
        
        builder.WriteString(fmt.Sprintf("#EXT-X-BYTERANGE:%s\nsegment%d.ts\n", 
            br.String(), i))
    }
    
    playlist := builder.String()
    
    // 🔹 Перевірити формат
    assert.Contains(t, playlist, "#EXT-X-BYTERANGE:376@3008")
    assert.Contains(t, playlist, "#EXT-X-BYTERANGE:400@0")
    assert.Contains(t, playlist, "#EXT-X-BYTERANGE:500")  // ✅ Без "@0"
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `Length=0` у серіалізації | Плеєр ігнорує діапазон або показує помилку | 🔹 Додати валідацію: `if *br.Length <= 0 { return "", fmt.Errorf(...) }` |
| `Start=nil` інтерпретується як 0 | Неправильне завантаження сегмента | 🔹 Документувати поведінку: nil = обчислювати автоматично, 0 = явний початок |
| Парсинг "376@1000@extra" | Часткова помилка, Start=nil | 🔹 Додати перевірку: `if len(values) > 2 { return nil, fmt.Errorf("too many @") }` |
| Переповнення при обчисленні next Start | Паніка при великих файлах | 🔹 Використовувати `int64` для проміжних обчислень: `int64(*prev.Start) + int64(*prev.Length)` |
| Локаль-залежний парсинг чисел | "376,5" не парситься у деяких регіонах | 🔹 Замінити кому на крапку перед парсингом: `strings.Replace(text, ",", ".", -1)` |

### Приклад покращеної валідації:

```go
func NewByteRangeValidated(text string, fileSize int64) (*ByteRange, error) {
    br, err := NewByteRange(text)
    if err != nil || br == nil {
        return br, err
    }
    
    // 🔹 Перевірити Length
    if br.Length == nil || *br.Length <= 0 {
        return nil, fmt.Errorf("ByteRange.Length must be positive")
    }
    
    // 🔹 Перевірити Start + Length проти розміру файлу
    if br.Start != nil {
        if *br.Start < 0 {
            return nil, fmt.Errorf("ByteRange.Start cannot be negative")
        }
        if int64(*br.Start)+int64(*br.Length) > fileSize {
            return nil, fmt.Errorf("ByteRange exceeds file size")
        }
    }
    
    return br, nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базове створення ByteRange:
func makeByteRange(length, start int) *m3u8.ByteRange {
    br := &m3u8.ByteRange{Length: &length}
    if start >= 0 {
        br.Start = &start
    }
    return br
}

// 2: Парсинг з логуванням:
func parseByteRangeWithLog(text, context string) (*m3u8.ByteRange, error) {
    br, err := m3u8.NewByteRange(text)
    if err != nil {
        log.Warnf("%s: failed to parse ByteRange %q: %v", context, text, err)
    } else if br != nil {
        log.Debugf("%s: parsed ByteRange: %s", context, br.String())
    }
    return br, err
}

// 3: Обчислення діапазону для I-frame:
func computeIFrameByteRange(position, size uint64) *m3u8.ByteRange {
    length := int(size)
    start := int(position)
    return &m3u8.ByteRange{
        Length: &length,
        Start:  &start,
    }
}

// 4: Форматування для HLS плейлиста:
func formatByteRangeTag(br *m3u8.ByteRange) string {
    if br == nil {
        return ""  // ✅ Немає діапазону = повний файл
    }
    return fmt.Sprintf("#EXT-X-BYTERANGE:%s", br.String())
}

// 5: Валідація перед записом у файл:
func safeWriteByteRange(f *os.File, br *m3u8.ByteRange, fileSize int64) error {
    if br == nil {
        return nil  // ✅ Нормальний випадок
    }
    
    if err := validateByteRange(br, fileSize); err != nil {
        return fmt.Errorf("invalid ByteRange: %w", err)
    }
    
    _, err := f.WriteString(formatByteRangeTag(br) + "\n")
    return err
}
```

---

## 📊 Матриця використання ByteRange у вашому пайплайні

```
Сценарій                     | Формат ByteRange      | Примітки
─────────────────────────────┼───────────────────────┼─────────────────────────
Перший I-frame у файлі      | "376@3008"           | ✅ Явна позиція
Послідовні I-frames         | "400", "500"...      | ✅ Start обчислюється автоматично
Повний сегмент (не I-frame) | nil                  | ✅ Немає #EXT-X-BYTERANGE тегу
Edge case: Length=0         | "0@1000"             | ⚠️ Може бути невалідним у плеєрах
Edge case: Start=0          | "400@0"              | ✅ Явний початок з 0 (рідкісно)
```

---

## 📚 Корисні посилання

- [HLS RFC Draft: EXT-X-BYTERANGE](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.2.2)
- [Apple HLS Authoring Specification](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [Go strconv package](https://pkg.go.dev/strconv)
- [Understanding HTTP byte ranges](https://developer.mozilla.org/en-US/docs/Web/HTTP/Range_requests)

> 💡 **Ключова ідея**: Цей `ByteRange` тип — це "міст" між текстовим форматом HLS та бінарними даними файлів. Він:
> - 🎯 Дозволяє точно вказувати позицію та розмір даних для ефективного завантаження
> - 🔧 Підтримує опціональний Start для гнучкості у послідовних діапазонах
> - ⚡ Економить трафік через завантаження тільки потрібних байтів
> - 🛡️ Граційно обробляє помилки парсингу без втрати валідних даних

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку 64-бітних значень для файлів >2GB (замість int використовувати int64)
- 🧪 Написати property-based тести для валідації інваріантів серіалізації/десеріалізації
- 📈 Додати Prometheus-метрики для моніторингу розподілу розмірів байтових діапазонів по каналах

🛠️