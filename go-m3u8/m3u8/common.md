# Глибоке роз'яснення: `m3u8` утиліти — парсинг та форматування атрибутів HLS плейлистів

Цей файл містить **набір допоміжних функцій** для роботи з атрибутами HLS плейлистів: парсинг текстових рядків типу `BANDWIDTH=2000000,RESOLUTION=1280x720`, перетворення значень у типи Go та зворотне форматування. Це фундамент для коректної генерації та читання `#EXT-X-STREAM-INF`, `#EXT-X-MEDIA` та інших тегів.

---

## 🎯 Навіщо ці утиліти потрібні у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ m3u8 утиліти у контексті HLS:          │
│                                         │
│ 🔹 Парсинг існуючих плейлистів:        │
│   • Читання мастер-плейлистів від      │
│     сторонніх джерел                   │
│   • Екстракція метаданих для аналізу   │
│   • Валідація коректності форматів     │
│                                         │
│ 🔹 Генерація валідних плейлистів:      │
│   • Форматування атрибутів за специ-   │
│     фікацією (лапки, роздільники...)   │
│   • Типобезпечне перетворення значень  │
│   • Уникнення помилок ручного конкат-  │
│     енування рядків                    │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Динамічне оновлення плейлистів     │
│     на основі метаданих потоку         │
│   • Адаптація параметрів енкодингу     │
│     під можливості клієнтів            │
└─────────────────────────────────────────┘
```

---

## 🔧 Константи та регулярний вираз: фундамент парсингу

### Форматні рядки для серіалізації

```go
const (
    quotedFormatString    = `%s="%v"`   // 🔹 Для значень у лапках: CODECS="avc1.640029"
    formatString          = `%s=%v`     // 🔹 Для числових значень: BANDWIDTH=2000000
    frameRateFormatString = `%s=%.3f`   // 🔹 Для дробових: FRAME-RATE=30.000
)
```

**Коли який використовувати:**
```
• quotedFormatString: CODECS, AUDIO, SUBTITLES, PATH (рядки з пробілами/спецсимволами)
• formatString: BANDWIDTH, RESOLUTION, GROUP-ID (числа, ідентифікатори)
• frameRateFormatString: FRAME-RATE (завжди 3 знаки після крапки за специфікацією)
```

### Регулярний вираз для парсингу атрибутів

```go
var parseRegex = regexp.MustCompile(`([A-z0-9-]+)\s*=\s*("[^"]*"|[^,]*)`)
```

**Розбір патерну:**
```
([A-z0-9-]+)     → 🔹 Група 1: ключ атрибута
                   • Букви (верхній/нижній регістр), цифри, дефіс
                   • Приклади: BANDWIDTH, FRAME-RATE, CODECS

\s*=\s*          → 🔹 Роздільник: = з опціональними пробілами
                   • Підтримує "BANDWIDTH=2000000" та "BANDWIDTH = 2000000"

("[^"]*"|[^,]*)  → 🔹 Група 2: значення атрибута
                   • Варіант 1: "[^"]*" — рядок у подвійних лапках (CODECS="...")
                   • Варіант 2: [^,]* — будь-що до коми (числа, ідентифікатори)
                   • Роздільник між атрибутами: кома
```

**Приклади збігів:**
```
Вхід: "BANDWIDTH=2000000,RESOLUTION=1280x720,CODECS=\"avc1.4d001f\""

Збіги:
1. Ключ="BANDWIDTH", Значення="2000000"
2. Ключ="RESOLUTION", Значення="1280x720"  
3. Ключ="CODECS", Значення=""avc1.4d001f"" (з лапками)

Після обробки (видалення лапок):
• CODECS → avc1.4d001f
```

> ⚠️ **Потенційна проблема**: `[A-z]` у регулярному виразі включає не тільки літери, а й символи `[\]^_` ` (ASCII 91-96). Краще використовувати `[A-Za-z]` для точності.

---

## 🔍 Функція `ParseAttributes`: основний парсер

```go
func ParseAttributes(text string) map[string]string {
    // 🔹 1. Видалити нові рядки (захист від багаторядкових тегів)
    res := make(map[string]string)
    value := strings.Replace(text, "\n", "", -1)
    
    // 🔹 2. Знайти всі збіги регулярного виразу
    matches := parseRegex.FindAllStringSubmatch(value, -1)
    
    // 🔹 3. Обробити кожен збіг
    for _, match := range matches {
        if len(match) >= 3 {  // 🔹 match[0]=весь збіг, [1]=ключ, [2]=значення
            key := match[1]
            // 🔹 Видалити лапки зі значення (якщо є)
            value := strings.Replace(match[2], `"`, "", -1)
            res[key] = value
        }
    }
    
    return res
}
```

### 🎯 Приклади використання:

```
Вхід: "#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720"
Після ParseAttributes():
{
    "BANDWIDTH": "2000000",
    "RESOLUTION": "1280x720"
}

Вхід: "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio\",NAME=\"English\""
Після ParseAttributes():
{
    "TYPE": "AUDIO",
    "GROUP-ID": "audio",      // 🔹 Лапки видалено
    "NAME": "English"         // 🔹 Лапки видалено
}

Вхід: "" (порожній рядок)
Після ParseAttributes():
{}  // 🔹 Порожня мапа, не nil
```

### ⚠️ Обмеження поточного парсера:

| Обмеження | Наслідок | Рішення |
|-----------|----------|---------|
| Не підтримує вкладені лапки | `NAME="Say \"Hello\""` зламається | 🔹 Використовувати спеціалізований CSV-парсер або стан-машину |
| `[A-z]` замість `[A-Za-z]` | Може зловити не-літерні символи | 🔹 Виправити регулярний вираз |
| Не валідує типи значень | "BANDWIDTH=abc" пройде парсинг | 🔹 Додати типобезпечні хелпери (`parseInt`, `parseFloat`) |
| Чутливий до регістру ключів | "bandwidth" ≠ "BANDWIDTH" | 🔹 Нормалізувати ключі: `strings.ToUpper(key)` |

---

## 🔍 Хелпери для парсингу типів

### `parseFloat`: парсинг дробових чисел

```go
func parseFloat(attributes map[string]string, key string) (*float64, error) {
    stringValue, ok := attributes[key]
    if !ok {
        return nil, nil  // 🔹 Ключ відсутній → nil, nil (не помилка)
    }
    
    value, err := strconv.ParseFloat(stringValue, 64)
    if err != nil {
        return nil, err  // 🔹 Помилка парсингу → nil, error
    }
    
    return &value, nil  // 🔹 Успіх → покажчик на значення, nil
}
```

**Приклади:**
```
attributes = {"FRAME-RATE": "30.000", "BANDWIDTH": "2000000"}

parseFloat(attributes, "FRAME-RATE") → &30.0, nil
parseFloat(attributes, "BANDWIDTH")  → &2000000.0, nil  // 🔹 Парсить як float
parseFloat(attributes, "UNKNOWN")    → nil, nil          // 🔹 Не помилка
parseFloat(attributes, "FRAME-RATE") з "abc" → nil, error
```

> 💡 **Чому покажчик?** Дозволяє розрізняти "ключ відсутній" (nil, nil) та "ключ є, значення 0" (&0, nil).

### `parseInt`: парсинг цілих чисел

```go
func parseInt(attributes map[string]string, key string) (*int, error) {
    stringValue, ok := attributes[key]
    if !ok {
        return nil, nil
    }
    
    // 🔹 ParseInt з основою 0: автоматично визначає 10/16/8 систему
    int64Value, err := strconv.ParseInt(stringValue, 0, 0)
    if err != nil {
        return nil, err
    }
    
    // 🔹 Конвертація int64 → int (може бути втрата на 32-бітних системах)
    value := int(int64Value)
    return &value, nil
}
```

**Особливість `base=0`:**
```
• "123" → десяткове 123
• "0x7B" → шістнадцяткове 123
• "0173" → вісімкове 123

Для HLS атрибутів зазвичай очікується десяткове, тому base=10 було б безпечніше.
```

### `parseYesNo`: парсинг булевих значень

```go
func parseYesNo(attributes map[string]string, key string) *bool {
    stringValue, ok := attributes[key]
    if !ok {
        return nil  // 🔹 Ключ відсутній
    }
    
    val := false
    if stringValue == YesValue {  // 🔹 YesValue = "YES" (має бути оголошено десь)
        val = true
    }
    // 🔹 Будь-яке інше значення (включно з "NO") → false
    
    return &val
}
```

**Приклади:**
```
attributes = {"AUTOSELECT": "YES", "FORCED": "NO", "DEFAULT": "maybe"}

parseYesNo(attributes, "AUTOSELECT") → &true
parseYesNo(attributes, "FORCED")     → &false  // 🔹 "NO" → false
parseYesNo(attributes, "DEFAULT")    → &false  // 🔹 "maybe" → false (!)
parseYesNo(attributes, "UNKNOWN")    → nil     // 🔹 Ключ відсутній
```

> ⚠️ **Потенційна проблема**: Будь-яке значення, крім точного "YES", повертає `false`. Це може приховати помилки у вхідних даних ("Yes", "yes", "1" не розпізнаються).

---

## 🔍 Хелпери для форматування та перевірки

### `formatYesNo`: зворотне перетворення bool → рядок

```go
func formatYesNo(value bool) string {
    if value {
        return YesValue  // "YES"
    }
    return NoValue  // "NO" (має бути оголошено)
}
```

**Використання:**
```
• formatYesNo(true)  → "YES"
• formatYesNo(false) → "NO"

Для генерації тегів:
attrs = append(attrs, fmt.Sprintf(quotedFormatString, "AUTOSELECT", formatYesNo(true)))
// → AUTOSELECT="YES"
```

### `attributeExists`: перевірка наявності ключа

```go
func attributeExists(key string, attributes map[string]string) bool {
    _, ok := attributes[key]
    return ok
}
```

**Чому окрема функція?**
```
• Покращує читабельність: if attributeExists("CODECS", attrs) vs if _, ok := attrs["CODECS"]; ok
• Централізує логіку (можна додати логування, кешування...)
• Зручно для mock-тестування
```

### `pointerTo`: безпечне отримання покажчика на значення

```go
func pointerTo(attributes map[string]string, key string) *string {
    value, ok := attributes[key]
    if !ok {
        return nil
    }
    return &value  // 🔹 Повертаємо покажчик на локальну копію!
}
```

> ⚠️ **Критична проблема**: `&value` повертає покажчик на **локальну змінну циклу/функції**. Це працює у поточному коді, але може призвести до неочікуваної поведінки при зміні мапи після виклику.

**Безпечніша альтернатива:**
```go
func pointerTo(attributes map[string]string, key string) *string {
    value, ok := attributes[key]
    if !ok {
        return nil
    }
    // 🔹 Створити нову змінну в heap, не на стеку
    result := value
    return &result
}
// Або простіше: повертати значення, а не покажчик, де можливо
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Парсинг #EXT-X-STREAM-INF тегів

```go
// У VideoManifestProxy — читання мастер-плейлиста:
func parseStreamInfTag(line string) (*StreamVariant, error) {
    // 🔹 Видалити префікс тега
    if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
        return nil, fmt.Errorf("not a STREAM-INF tag: %s", line)
    }
    
    attrs := ParseAttributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
    
    // 🔹 Парсинг обов'язкових атрибутів
    bandwidth, err := parseInt(attrs, "BANDWIDTH")
    if err != nil || bandwidth == nil {
        return nil, fmt.Errorf("missing or invalid BANDWIDTH: %w", err)
    }
    
    resolution := pointerTo(attrs, "RESOLUTION")
    codecs := pointerTo(attrs, "CODECS")
    audioGroup := pointerTo(attrs, "AUDIO")
    
    // 🔹 Опціональні атрибути
    frameRate, _ := parseFloat(attrs, "FRAME-RATE")  // 🔹 Ігноруємо помилку, атрибут опціональний
    autoSelect := parseYesNo(attrs, "AUTOSELECT")
    
    return &StreamVariant{
        Bandwidth:  *bandwidth,
        Resolution: resolution,
        Codecs:     codecs,
        AudioGroup: audioGroup,
        FrameRate:  frameRate,
        AutoSelect: autoSelect,
    }, nil
}
```

### ✅ 2: Генерація валідних тегів

```go
// У suggest.VideoVariant.Stanza() — побудова #EXT-X-STREAM-INF:
func (v VideoVariant) buildStreamInfTag(playlistFilename string) string {
    var attrs []string
    
    // 🔹 Обов'язкові атрибути
    attrs = append(attrs, fmt.Sprintf(formatString, "BANDWIDTH", v.Bandwidth))
    attrs = append(attrs, fmt.Sprintf(formatString, "RESOLUTION", v.Resolution))
    
    // 🔹 CODECS (якщо є)
    if v.Codecs != "" {
        attrs = append(attrs, fmt.Sprintf(quotedFormatString, "CODECS", v.Codecs))
    }
    
    // 🔹 Групи медіа
    if v.AudioGroup != nil {
        attrs = append(attrs, fmt.Sprintf(quotedFormatString, "AUDIO", *v.AudioGroup))
    }
    if v.SubtitleGroup != nil {
        attrs = append(attrs, fmt.Sprintf(quotedFormatString, "SUBTITLES", *v.SubtitleGroup))
    }
    
    // 🔹 FRAME-RATE (якщо вказано)
    if v.FrameRate != nil {
        attrs = append(attrs, fmt.Sprintf(frameRateFormatString, "FRAME-RATE", *v.FrameRate))
    }
    
    return fmt.Sprintf("#EXT-X-STREAM-INF:%s\n%s", 
        strings.Join(attrs, ","), playlistFilename)
}
```

### ✅ 3: Валідація атрибутів перед записом

```go
// Перевірити, що атрибуты валідні перед генерацією плейлиста:
func validateStreamInfAttributes(attrs map[string]string) error {
    // 🔹 BANDWIDTH: обов'язковий, позитивне ціле
    bw, err := parseInt(attrs, "BANDWIDTH")
    if err != nil {
        return fmt.Errorf("invalid BANDWIDTH: %w", err)
    }
    if bw == nil || *bw <= 0 {
        return fmt.Errorf("BANDWIDTH must be positive")
    }
    
    // 🔹 RESOLUTION: формат WxH
    if res, ok := attrs["RESOLUTION"]; ok {
        if !regexp.MustCompile(`^\d+x\d+$`).MatchString(res) {
            return fmt.Errorf("invalid RESOLUTION format: %s (expected WxH)", res)
        }
    }
    
    // 🔹 CODECS: список через кому, без пробілів
    if codecs, ok := attrs["CODECS"]; ok {
        for _, codec := range strings.Split(codecs, ",") {
            if !regexp.MustCompile(`^[a-z0-9.]+$`).MatchString(strings.ToLower(codec)) {
                return fmt.Errorf("invalid CODEC format: %s", codec)
            }
        }
    }
    
    // 🔹 FRAME-RATE: позитивне дробове
    if fr, ok := attrs["FRAME-RATE"]; ok {
        if val, err := strconv.ParseFloat(fr, 64); err != nil || val <= 0 {
            return fmt.Errorf("invalid FRAME-RATE: %s", fr)
        }
    }
    
    return nil
}
```

### ✅ 4: Моніторинг якості плейлистів

```go
// monitoring.Monitor — метрики для атрибутів плейлистів:
type PlaylistMetrics struct {
    TagsParsed        *prometheus.CounterVec  // кількість розпарсених тегів
    AttributeErrors   *prometheus.CounterVec  // помилки парсингу атрибутів
    BandwidthDistribution *prometheus.HistogramVec  // розподіл бітрейтів
    ResolutionDistribution *prometheus.HistogramVec  // розподіл роздільних здатностей
    MissingRequiredAttrs *prometheus.CounterVec  // відсутні обов'язкові атрибути
}

// У процесі парсингу:
func monitorPlaylistParsing(channelID string, tagType string, 
                           attrs map[string]string, metrics *PlaylistMetrics, err error) {
    
    metrics.TagsParsed.WithLabelValues(channelID, tagType).Inc()
    
    if err != nil {
        metrics.AttributeErrors.WithLabelValues(channelID, tagType).Inc()
        log.Warnf("Channel %s: error parsing %s attributes: %v", channelID, tagType, err)
        return
    }
    
    // 🔹 Зібрати статистику по атрибутах
    if bw, err := parseInt(attrs, "BANDWIDTH"); err == nil && bw != nil {
        metrics.BandwidthDistribution.WithLabelValues(channelID).Observe(float64(*bw))
    }
    
    if res, ok := attrs["RESOLUTION"]; ok {
        if parts := strings.Split(res, "x"); len(parts) == 2 {
            if height, err := strconv.Atoi(parts[1]); err == nil {
                metrics.ResolutionDistribution.WithLabelValues(channelID).Observe(float64(height))
            }
        }
    }
    
    // 🔹 Перевірити наявність обов'язкових атрибутів
    if tagType == "EXT-X-STREAM-INF" {
        if !attributeExists("BANDWIDTH", attrs) {
            metrics.MissingRequiredAttrs.WithLabelValues(channelID, "BANDWIDTH").Inc()
        }
    }
}
```

### ✅ 5: Обробка помилок парсингу з fallback

```go
// Стратегія: продовжити з дефолтними значеннями при помилках
func safeParseAttributes(line string, defaults map[string]string) map[string]string {
    attrs := ParseAttributes(line)
    
    // 🔹 Заповнити відсутні значення дефолтами
    for key, defaultValue := range defaults {
        if _, ok := attrs[key]; !ok {
            attrs[key] = defaultValue
            log.Debugf("Using default for %s: %s", key, defaultValue)
        }
    }
    
    return attrs
}

// Використання:
defaults := map[string]string{
    "BANDWIDTH": "1000000",      // дефолт 1 Mbps
    "RESOLUTION": "1280x720",    // дефолт 720p
    "CODECS": "avc1.4d001f",     // дефолт H.264 Main@4.1
}
attrs := safeParseAttributes(tagLine, defaults)
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на парсинг атрибутів

```go
func TestParseAttributes_Basic(t *testing.T) {
    testCases := []struct {
        input    string
        expected map[string]string
    }{
        {
            "BANDWIDTH=2000000,RESOLUTION=1280x720",
            map[string]string{"BANDWIDTH": "2000000", "RESOLUTION": "1280x720"},
        },
        {
            `CODECS="avc1.4d001f,mp4a.40.2",AUDIO="audio"`,
            map[string]string{"CODECS": "avc1.4d001f,mp4a.40.2", "AUDIO": "audio"},
        },
        {
            "FRAME-RATE=30.000, CLOSED-CAPTIONS=NONE",
            map[string]string{"FRAME-RATE": "30.000", "CLOSED-CAPTIONS": "NONE"},
        },
        {
            "",  // порожній вхід
            map[string]string{},
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.input, func(t *testing.T) {
            result := ParseAttributes(tc.input)
            assert.Equal(t, tc.expected, result)
        })
    }
}
```

### 🔹 Тест на парсинг типів

```go
func TestParseHelpers(t *testing.T) {
    attrs := map[string]string{
        "BANDWIDTH": "2000000",
        "FRAME-RATE": "30.000",
        "AUTOSELECT": "YES",
        "FORCED": "NO",
        "UNKNOWN": "value",
    }
    
    // 🔹 parseInt
    bw, err := parseInt(attrs, "BANDWIDTH")
    assert.NoError(t, err)
    assert.NotNil(t, bw)
    assert.Equal(t, 2000000, *bw)
    
    // 🔹 parseFloat
    fr, err := parseFloat(attrs, "FRAME-RATE")
    assert.NoError(t, err)
    assert.NotNil(t, fr)
    assert.InDelta(t, 30.0, *fr, 0.001)
    
    // 🔹 parseYesNo
    assert.True(t, *parseYesNo(attrs, "AUTOSELECT"))
    assert.False(t, *parseYesNo(attrs, "FORCED"))
    assert.Nil(t, parseYesNo(attrs, "MISSING"))
    
    // 🔹 pointerTo
    assert.NotNil(t, pointerTo(attrs, "UNKNOWN"))
    assert.Equal(t, "value", *pointerTo(attrs, "UNKNOWN"))
    assert.Nil(t, pointerTo(attrs, "NOTEXIST"))
}
```

### 🔹 Тест на edge cases парсингу

```go
func TestParseAttributes_EdgeCases(t *testing.T) {
    // 🔹 Пробіли навколо =
    result := ParseAttributes("BANDWIDTH = 2000000 , RESOLUTION = 1280x720")
    assert.Equal(t, "2000000", result["BANDWIDTH"])
    
    // 🔹 Лапки у значенні
    result = ParseAttributes(`NAME="Subtitle Track (English)"`)
    assert.Equal(t, "Subtitle Track (English)", result["NAME"])
    
    // 🔹 Порожнє значення
    result = ParseAttributes("EMPTY=,BANDWIDTH=2000000")
    assert.Equal(t, "", result["EMPTY"])
    assert.Equal(t, "2000000", result["BANDWIDTH"])
    
    // 🔹 Спецсимволи у ключі (дефіс)
    result = ParseAttributes("CLOSED-CAPTIONS=NONE")
    assert.Equal(t, "NONE", result["CLOSED-CAPTIONS"])
}
```

### 🔹 Тест на помилки парсингу

```go
func TestParseHelpers_Errors(t *testing.T) {
    attrs := map[string]string{
        "BANDWIDTH": "not-a-number",
        "FRAME-RATE": "abc.def",
    }
    
    // 🔹 parseInt з нечисловим значенням
    _, err := parseInt(attrs, "BANDWIDTH")
    assert.Error(t, err)
    
    // 🔹 parseFloat з нечисловим значенням
    _, err = parseFloat(attrs, "FRAME-RATE")
    assert.Error(t, err)
    
    // 🔹 Відсутній ключ → не помилка
    result, err := parseInt(attrs, "MISSING")
    assert.NoError(t, err)
    assert.Nil(t, result)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `[A-z]` замість `[A-Za-z]` | Ключі з символами `[\]^_` ` парсяться некоректно | 🔹 Виправити регулярний вираз: `regexp.MustCompile([A-Za-z0-9-]+...)` |
| Покажчик на локальну змінну у `pointerTo` | Неочікувана поведінка при зміні мапи | 🔹 Повертати копію значення: `result := value; return &result` |
| `parseYesNo` не розпізнає "yes"/"1" | Булеві атрибути завжди false | 🔹 Додати нормалізацію: `strings.ToUpper(stringValue) == "YES"` |
| `parseInt` з base=0 парсить шістнадцяткові | "0x10" → 16 замість помилки | 🔹 Використовувати base=10 для десяткових значень |
| Відсутня валідація формату значень | "RESOLUTION=abc" пройде парсинг | 🔹 Додати `validateStreamInfAttributes` перед використанням |

### Приклад покращеного `pointerTo`:

```go
func pointerTo(attributes map[string]string, key string) *string {
    value, ok := attributes[key]
    if !ok {
        return nil
    }
    // 🔹 Створити нову змінну, щоб уникнути покажчика на локальну
    result := value
    return &result
}

// Або ще краще: повертати значення, де можливо, щоб уникнути покажчиків взагалі
func stringValue(attributes map[string]string, key string) (string, bool) {
    value, ok := attributes[key]
    return value, ok
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базовий парсинг тега:
func parseHLSLine(line string) (tagType string, attrs map[string]string, err error) {
    if !strings.HasPrefix(line, "#EXT") {
        return "", nil, fmt.Errorf("not an HLS tag")
    }
    
    parts := strings.SplitN(line, ":", 2)
    tagType = strings.TrimPrefix(parts[0], "#")
    
    if len(parts) < 2 {
        return tagType, map[string]string{}, nil
    }
    
    attrs = ParseAttributes(parts[1])
    return tagType, attrs, nil
}

// 2: Безпечне отримання обов'язкового атрибуту:
func requireString(attrs map[string]string, key string) (string, error) {
    value, ok := attrs[key]
    if !ok {
        return "", fmt.Errorf("missing required attribute: %s", key)
    }
    if value == "" {
        return "", fmt.Errorf("empty value for required attribute: %s", key)
    }
    return value, nil
}

// 3: Форматування повного тега:
func formatHLSTag(tagType string, attrs map[string]string, value string) string {
    var parts []string
    for key, val := range attrs {
        // 🔹 Визначити, чи потрібні лапки
        if strings.ContainsAny(val, " ,\"") {
            parts = append(parts, fmt.Sprintf(quotedFormatString, key, val))
        } else {
            parts = append(parts, fmt.Sprintf(formatString, key, val))
        }
    }
    
    if value != "" {
        return fmt.Sprintf("#%s:%s\n%s", tagType, strings.Join(parts, ","), value)
    }
    return fmt.Sprintf("#%s:%s", tagType, strings.Join(parts, ","))
}

// 4: Логування для відладки:
func logParsedTag(tagType string, attrs map[string]string) {
    log.Debugf("Parsed %s tag with %d attributes:", tagType, len(attrs))
    for k, v := range attrs {
        log.Debugf("  %s = %q", k, v)
    }
}

// 5: Кешування результатів парсингу (для великих плейлистів):
var attrCache = sync.Map{}  // key: "tag:attrs_string", value: map[string]string

func cachedParseAttributes(tagType, attrStr string) map[string]string {
    key := tagType + ":" + attrStr
    if cached, ok := attrCache.Load(key); ok {
        if m, ok := cached.(map[string]string); ok {
            return m
        }
    }
    
    result := ParseAttributes(attrStr)
    attrCache.Store(key, result)
    return result
}
```

---

## 📊 Матриця атрибутів HLS для вашого пайплайну

```
Атрибут          | Тип       | Обов'язковий? | Приклад значення    | Парсер
─────────────────┼───────────┼───────────────┼─────────────────────┼──────────
BANDWIDTH        | int       | ✅ Так        | 2000000             | parseInt
RESOLUTION       | string    | ⚠️ Рекомендовано | 1280x720         | pointerTo + валідація
CODECS           | string    | ⚠️ Рекомендовано | "avc1.4d001f"    | pointerTo
AUDIO            | string    | ❌ Ні         | "audio"             | pointerTo
SUBTITLES        | string    | ❌ Ні         | "subs"              | pointerTo
FRAME-RATE       | float64   | ❌ Ні         | 30.000              | parseFloat
AUTOSELECT       | bool      | ❌ Ні         | YES/NO              | parseYesNo
FORCED           | bool      | ❌ Ні         | YES/NO              | parseYesNo
DEFAULT          | bool      | ❌ Ні         | YES/NO              | parseYesNo
```

---

## 📚 Корисні посилання

- [HLS RFC Draft: Attribute syntax](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.2)
- [Go regexp package](https://pkg.go.dev/regexp)
- [Apple HLS Authoring Specification](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [strconv package best practices](https://pkg.go.dev/strconv)

> 💡 **Ключова ідея**: Ці утиліти — це "інструментарій" для роботи з текстовим представленням HLS плейлистів. Вони:
> - 🎯 Перетворюють сирий текст тегів у типобезпечні структури Go
> - 🔧 Забезпечують консистентне форматування при генерації плейлистів
> - ⚡ Прискорюють розробку через централізовану логіку замість дублювання
> - 🛡️ Граційно обробляють помилки через nil-повернення та окремі error-значення

Якщо потрібно — можу допомогти:
- 🔄 Виправити регулярний вираз для точнішого парсингу ключів ([A-Za-z] замість [A-z])
- 🧪 Написати fuzz-тести для пошуку крашів на випадкових вхідних рядках атрибутів
- 📈 Додати Prometheus-метрики для моніторингу успішності парсингу атрибутів по каналах

🛠️