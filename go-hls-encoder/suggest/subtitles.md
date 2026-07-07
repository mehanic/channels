# Глибоке роз'яснення: `suggest.SubtitleVariant` — автоматична генерація варіантів субтитрів для HLS

Цей файл містить **логіку інтелектуального підбору субтитрів** на основі метаданих вхідних потоків та зовнішніх джерел. Він аналізує кодеки, мовні теги, accessibility прапорці та генерує оптимальну конфігурацію для адаптивного HLS-стрімінгу.

---

## 🎯 Навіщо цей код потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ suggest.SubtitleVariant у контексті:   │
│                                         │
│ 🔹 Автоматизація конфігурації:         │
│   • Не потрібно вручну вказувати       │
│     параметри для кожного вхідного     │
│     потоку                              │
│   • Адаптація до різних мов/тегів      │
│                                         │
│ 🔹 Підтримка багатомовності:           │
│   • Детекція мовних тегів (RFC 5646)   │
│   • Групування за SUBTITLES group ID   │
│   • Фільтрація регіональних варіантів  │
│                                         │
│ 🔹 Accessibility:                      │
│   • Виявлення субтитрів для слабозорих │
│   • Підтримка FORCED субтитрів для     │
│     іноземних діалогів                 │
│   • CHARACTERISTICS для HLS плеєрів    │
└─────────────────────────────────────────┘
```

---

## 🔧 Типи даних: структура субтитр-варіантів

### `SubtitleVariant`: конфігурація одного варіанту субтитрів

```go
type SubtitleVariant struct {
    // 🔹 Вхідні параметри
    InputURL    string  // 🎯 URL вхідного потоку/файлу
    StreamIndex uint    // 🎯 Індекс потоку субтитрів у вхідному файлі
    
    // 🔹 HLS playlist метадані (EXT-X-MEDIA)
    Name            string         // 🎯 Унікальна назва для вибору у плеєрі
    GroupID         *string        // 🎯 Група субтитрів: "subtitles" за замовчуванням
    HearingImpaired bool           // 🎯 Чи для слабозорих (accessibility)
    Forced          bool           // 🎯 Чи показувати автоматично для іноземної мови
    Language        input.Language // 🎯 Мова за RFC 5646: "eng", "ukr", "fra"...
    
    // 🔹 Вихідні параметри
    OutputIndex uint  // 🎯 Унікальний індекс для іменування вихідних файлів
}
```

### 🎯 Ключові поля для HLS сумісності

| Поле | Призначення | Приклад значення |
|------|-------------|-----------------|
| `InputURL` | Джерело субтитрів | `"input1.ts"`, `"srt://path/to/file.srt"` |
| `StreamIndex` | Який потік витягувати | `2` = третій потік у файлі (0-індексація) |
| `Name` | Назва для вибору у плеєрі | `"English Subtitles"`, `"Українські субтитри"` |
| `GroupID` | Група для EXT-X-MEDIA | `"subtitles"` — всі варіанти в одній групі |
| `Language` | Мова для автоматичного вибору | `input.English`, `input.Ukrainian` |
| `Forced` | Чи показувати автоматично | `true` для субтитрів іноземних реплік |
| `HearingImpaired` | Accessibility тег | `true` для SDH (Subtitles for the Deaf and Hard of Hearing) |

---

## 🔍 Функція `SuggestSubtitlesVariants`: основна логіка генерації

```go
func SuggestSubtitlesVariants(probeDataInputsURLs []string, 
                             probeDataInputs []*probe.ProbeData,
                             additionalSearcher func(languages []input.Language) map[input.Language][]input.SubtitleInput,
                             removeVFQ bool) []SubtitleVariant {
    
    // 🔹 1. Ініціалізація: мапа мов → варіанти
    languages := map[input.Language][]SubtitleVariant{
        input.EnglishLanguage: {}, 
        input.FrenchLanguage: {},
    }
    var outputIndex uint = 0
    
    // 🔹 2. Перший прохід: аналіз вбудованих субтитрів через ffprobe
    for inputIndex, probeData := range probeDataInputs {
        for streamIndex, stream := range probeData.Streams {
            if stream.CodecType == "subtitle" && stream.CodecName != "hdmv_pgs_subtitle" {
                outputIndex += 1
                
                // 🔹 Визначити метадані з потоків
                language := matchLanguage(stream)           // 🔹 Мова з тегів
                hearingImpaired := matchHearingImpairedTag(stream)  // 🔹 Accessibility
                forced := matchForcedTag(stream)            // 🔹 FORCED прапорець
                
                variant := SubtitleVariant{
                    InputURL:        probeDataInputsURLs[inputIndex],
                    StreamIndex:     uint(streamIndex),
                    Language:        language,
                    Name:            "Subtitle" + strconv.Itoa(streamIndex),
                    HearingImpaired: hearingImpaired,
                    Forced:          forced,
                    OutputIndex:     outputIndex,
                }
                
                // 🔹 Додати у мапу за мовою
                languages[language] = append(languages[language], variant)
            }
        }
    }
    
    // 🔹 3. Другий прохід: пошук зовнішніх субтитрів для відсутніх мов
    var languagesToSearch []input.Language
    for lang, variants := range languages {
        if len(variants) == 0 {
            languagesToSearch = append(languagesToSearch, lang)
        }
    }
    
    // 🔹 Виклик зовнішнього пошуку (напр., Whisper, зовнішні SRT файли)
    additionalInputs := additionalSearcher(languagesToSearch)
    for _, inputs := range additionalInputs {
        for _, subtitleInput := range inputs {
            outputIndex += 1
            variant := SubtitleVariant{
                InputURL:        subtitleInput.InputURL,
                StreamIndex:     subtitleInput.StreamIndex,
                Language:        subtitleInput.Language,
                Name:            subtitleInput.Name,
                HearingImpaired: subtitleInput.HearingImpaired,
                Forced:          subtitleInput.Forced,
                OutputIndex:     outputIndex,
            }
            languages[subtitleInput.Language] = append(languages[subtitleInput.Language], variant)
        }
    }
    
    // 🔹 4. Фінальна очистка: залишити по одному варіанту на мову
    variants := cleanVariants(languages, removeVFQ)
    return variants
}
```

### 🎯 Ключові рішення в логіці

#### 🔹 Фільтрація `hdmv_pgs_subtitle`

```go
if stream.CodecType == "subtitle" && stream.CodecName != "hdmv_pgs_subtitle" {
    // ... обробка ...
}
```

**Чому це важливо:**
```
• hdmv_pgs_subtitle = Blu-ray PGS (Presentation Graphics Stream)
• Це бітові зображення, не текст → не підтримується у WebVTT/HLS
• Ігнорування запобігає помилкам конвертації

Альтернативи для PGS:
❌ Не конвертувати (втрачаються субтитри)
✅ Використовувати OCR (напр., ggsub) для розпізнавання тексту
✅ Попередньо екстрагувати SRT з PGS перед запуском пайплайну
```

#### 🔹 Дворівнева стратегія пошуку субтитрів

```
Рівень 1: Вбудовані субтитри (ffprobe)
✅ Швидко, не вимагає зовнішніх залежностей
✅ Точні таймінги, синхронізація з відео
❌ Обмежено тим, що є у вхідному файлі

Рівень 2: Зовнішній пошук (additionalSearcher)
✅ Може знайти субтитри, яких немає у вхідному файлі
✅ Підтримка автоматичної генерації (Whisper)
❌ Вимагає додаткової логіки/сервісів
❌ Може мати розсинхронізацію таймінгів

Архітектура:
1. Спочатку шукаємо вбудовані субтитри
2. Для мов, яких не знайдено → викликаємо additionalSearcher
3. Об'єднуємо результати, видаляємо дублікати

Це приклад "layered fallback" стратегії.
```

#### 🔹 `cleanVariants`: вибір оптимального варіанту на мову

```go
func cleanVariants(languages map[input.Language][]SubtitleVariant, removeVFQ bool) []SubtitleVariant {
    var variants []SubtitleVariant
    
    for language, subtitleVariants := range languages {
        // 🔹 Пропустити VFQ якщо потрібно
        if removeVFQ && language == input.QuebecLanguage {
            continue
        }
        
        if len(subtitleVariants) > 0 {
            gotForced := false
            gotFull := false
            
            for _, subVariant := range subtitleVariants {
                // 🔹 Пріоритет 1: Взяти один FORCED варіант
                if !gotForced && subVariant.Forced {
                    variants = append(variants, subVariant)
                    gotForced = true
                }
                // 🔹 Пріоритет 2: Взяти один повний варіант
                if !gotFull && !subVariant.Forced {
                    variants = append(variants, subVariant)
                    gotFull = true
                }
                // 🔹 Якщо знайшли обидва → далі не шукаємо
                if gotForced && gotFull {
                    break
                }
            }
        }
    }
    return variants
}
```

**Логіка вибору:**
```
Для кожної мови:
1. Спочатку шукаємо FORCED варіант (для іноземних реплік)
2. Потім шукаємо повний варіант (для всіх діалогів)
3. Якщо знайшли обидва → зупиняємо пошук для цієї мови
4. Якщо тільки один тип → беремо його

Результат: максимум 2 варіанти на мову (FORCED + Full)
Це запобігає дублюванню та перевантаженню плеєра.
```

---

## 🔍 Метод `SubtitleVariant.PlaylistName()`: генерація імені вихідного файлу

```go
func (v SubtitleVariant) PlaylistName(outputDir string) string {
    if len(outputDir) > 0 {
        // 🔹 З каталогом: join шляхів
        return filepath.Join(outputDir, v.PlaylistName(""))
    } else {
        // 🔹 Без каталогу: просто Name + розширення
        return v.Name + ".m3u8"
    }
}
```

### 🎯 Приклади виходу:

```
v.Name = "English Subtitles"

✅ Без outputDir:
  "English Subtitles.m3u8"

✅ З outputDir = "/output/hls":
  "/output/hls/English Subtitles.m3u8"
```

> ⚠️ **Увага**: Ім'я файлу містить пробіли! Це може призвести до проблем у деяких системах. Рекомендується санітизувати `Name` перед використанням.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Генерація субтитр-варіантів для каналу

```go
// У channel-менеджері — автоматична конфігурація:
func generateSubtitleConfig(channelID string, 
                           probeURLs []string, 
                           probeData []*probe.ProbeData) []suggest.SubtitleVariant {
    
    // 🔹 Зовнішній пошуковик: наприклад, Whisper для автоматичних субтитрів
    additionalSearcher := func(languages []input.Language) map[input.Language][]input.SubtitleInput {
        result := make(map[input.Language][]input.SubtitleInput)
        
        for _, lang := range languages {
            // 🔹 Запустити Whisper для цієї мови
            srtPath, err := whisper.GenerateSubtitles(channelID, lang)
            if err != nil {
                log.Warnf("Channel %s: Whisper failed for %s: %v", channelID, lang, err)
                continue
            }
            
            result[lang] = append(result[lang], input.SubtitleInput{
                InputURL:    srtPath,
                StreamIndex: 0,  // SRT має один потік
                Language:    lang,
                Name:        fmt.Sprintf("%s (Auto)", lang.String()),
                Forced:      false,
                HearingImpaired: false,
            })
        }
        return result
    }
    
    // 🔹 Параметри: не видаляти VFQ для CCTV (може бути корисним)
    variants := suggest.SuggestSubtitlesVariants(
        probeURLs, 
        probeData, 
        additionalSearcher,
        false,  // removeVFQ = false
    )
    
    // 🔹 Логування для відладки
    log.Infof("Channel %s: generated %d subtitle variants", channelID, len(variants))
    for _, v := range variants {
        log.Debugf("  - %s: lang=%s, forced=%v, hearing_impaired=%v, url=%s",
            v.Name, v.Language, v.Forced, v.HearingImpaired, v.InputURL)
    }
    
    return variants
}
```

### ✅ 2: Валідація згенерованих варіантів

```go
// Перевірити, що варіанти валідні перед передачею у конвертер:
func validateSubtitleVariants(variants []suggest.SubtitleVariant) error {
    for i, v := range variants {
        // 🔹 Обов'язкові поля
        if v.InputURL == "" {
            return fmt.Errorf("variant %d: missing InputURL", i)
        }
        if v.Name == "" {
            return fmt.Errorf("variant %d: missing Name", i)
        }
        
        // 🔹 Санітизація імені файлу (видалити пробіли, спецсимволи)
        sanitizedName := sanitizeFilename(v.Name)
        if sanitizedName != v.Name {
            log.Warnf("variant %d: sanitized name from '%s' to '%s'", i, v.Name, sanitizedName)
            v.Name = sanitizedName
        }
        
        // 🔹 Перевірка доступності вхідного файлу
        if _, err := os.Stat(v.InputURL); os.IsNotExist(err) {
            return fmt.Errorf("variant %d: input file not found: %s", i, v.InputURL)
        }
    }
    return nil
}

func sanitizeFilename(name string) string {
    // 🔹 Замінити пробіли та спецсимволи на підкреслення
    return strings.Map(func(r rune) rune {
        if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
            return r
        }
        return '_'
    }, name)
}
```

### ✅ 3: Моніторинг розподілу субтитр-варіантів

```go
// monitoring.Monitor — метрики для субтитрів:
type SubtitleVariantMetrics struct {
    VariantsGenerated *prometheus.CounterVec  // кількість згенерованих варіантів
    LanguagesDistribution *prometheus.CounterVec  // розподіл за мовами
    ForcedCount       *prometheus.CounterVec  // кількість FORCED варіантів
    AccessibilityCount *prometheus.CounterVec  // кількість accessibility варіантів
}

// У процесі генерації:
func monitorSubtitleVariants(channelID string, variants []suggest.SubtitleVariant, 
                            metrics *SubtitleVariantMetrics) {
    
    metrics.VariantsGenerated.WithLabelValues(channelID).Add(float64(len(variants)))
    
    for _, v := range variants {
        metrics.LanguagesDistribution.WithLabelValues(channelID, string(v.Language)).Inc()
        
        if v.Forced {
            metrics.ForcedCount.WithLabelValues(channelID).Inc()
        }
        if v.HearingImpaired {
            metrics.AccessibilityCount.WithLabelValues(channelID).Inc()
        }
    }
}
```

### ✅ 4: Кешування результатів пошуку субтитрів

```go
// Щоб не шукати ті самі субтитри багато разів:
type SubtitleVariantCache struct {
    mu    sync.RWMutex
    cache map[string][]suggest.SubtitleVariant  // key = fileHash, value = variants
    ttl   time.Duration
}

func (c *SubtitleVariantCache) GetOrSuggest(probeURLs []string, 
                                           probeData []*probe.ProbeData,
                                           searcher func([]input.Language) map[input.Language][]input.SubtitleInput,
                                           removeVFQ bool,
                                           fileHash string) []suggest.SubtitleVariant {
    // 🔹 Спробувати отримати з кешу
    c.mu.RLock()
    if variants, ok := c.cache[fileHash]; ok {
        c.mu.RUnlock()
        return variants
    }
    c.mu.RUnlock()
    
    // 🔹 Згенерувати варіанти
    variants := suggest.SuggestSubtitlesVariants(probeURLs, probeData, searcher, removeVFQ)
    
    // 🔹 Зберегти у кеш
    c.mu.Lock()
    c.cache[fileHash] = variants
    c.mu.Unlock()
    
    return variants
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на генерацію з вбудованих субтитрів

```go
func TestSuggestSubtitlesVariants_Embedded(t *testing.T) {
    // 🔹 Підготувати тестові дані: вбудовані субтитри eng/ukr
    probeData := &probe.ProbeData{
        Streams: []*probe.ProbeStream{
            {
                Index:      2,
                CodecType:  "subtitle",
                CodecName:  "webvtt",  // ✅ Підтримуваний формат
                Tags: probe.StreamTags{Language: "eng"},
            },
            {
                Index:      3,
                CodecType:  "subtitle",
                CodecName:  "webvtt",
                Tags: probe.StreamTags{Language: "ukr"},
            },
        },
    }
    
    variants := SuggestSubtitlesVariants(
        []string{"input.ts"}, 
        []*probe.ProbeData{probeData},
        nil,  // additionalSearcher = nil
        false,
    )
    
    // 🔹 Перевірити результат
    assert.Len(t, variants, 2)  // eng + ukr
    
    // 🔹 Перевірити англійські субтитри
    eng := findVariantByLanguage(variants, input.English)
    assert.NotNil(t, eng)
    assert.Equal(t, "input.ts", eng.InputURL)
    assert.Equal(t, uint(2), eng.StreamIndex)
    assert.False(t, eng.Forced)  // за замовчуванням
    
    // 🔹 Перевірити українські субтитри
    ukr := findVariantByLanguage(variants, input.Ukrainian)
    assert.NotNil(t, ukr)
    assert.Equal(t, uint(3), ukr.StreamIndex)
}

func findVariantByLanguage(variants []suggest.SubtitleVariant, lang input.Language) *suggest.SubtitleVariant {
    for _, v := range variants {
        if v.Language == lang {
            return &v
        }
    }
    return nil
}
```

### 🔹 Тест на фільтрацію PGS субтитрів

```go
func TestSuggestSubtitlesVariants_FilterPGS(t *testing.T) {
    // 🔹 Підготувати дані: PGS субтитри (не підтримуються)
    probeData := &probe.ProbeData{
        Streams: []*probe.ProbeStream{
            {
                Index:      2,
                CodecType:  "subtitle",
                CodecName:  "hdmv_pgs_subtitle",  // ❌ Не підтримується
                Tags: probe.StreamTags{Language: "eng"},
            },
        },
    }
    
    variants := SuggestSubtitlesVariants(
        []string{"input.ts"}, 
        []*probe.ProbeData{probeData},
        nil, false,
    )
    
    // 🔹 PGS має бути проігноровано
    assert.Empty(t, variants, "PGS subtitles should be filtered out")
}
```

### 🔹 Тест на cleanVariants логіку

```go
func TestCleanVariants_ForcedAndFull(t *testing.T) {
    languages := map[input.Language][]suggest.SubtitleVariant{
        input.English: {
            {Name: "English Forced", Language: input.English, Forced: true},
            {Name: "English Full", Language: input.English, Forced: false},
            {Name: "English Duplicate", Language: input.English, Forced: false},  // дублікат
        },
    }
    
    variants := cleanVariants(languages, false)
    
    // 🔹 Має бути рівно 2 варіанти: один FORCED, один повний
    assert.Len(t, variants, 2)
    
    // 🔹 Перевірити наявність обох типів
    hasForced := false
    hasFull := false
    for _, v := range variants {
        if v.Forced { hasForced = true }
        if !v.Forced { hasFull = true }
    }
    assert.True(t, hasForced)
    assert.True(t, hasFull)
}

func TestCleanVariants_OnlyForced(t *testing.T) {
    languages := map[input.Language][]suggest.SubtitleVariant{
        input.French: {
            {Name: "French Forced", Language: input.French, Forced: true},
        },
    }
    
    variants := cleanVariants(languages, false)
    
    // 🔹 Має бути 1 варіант (тільки FORCED)
    assert.Len(t, variants, 1)
    assert.True(t, variants[0].Forced)
}
```

### 🔹 Тест на фільтрацію VFQ

```go
func TestCleanVariants_RemoveVFQ(t *testing.T) {
    languages := map[input.Language][]suggest.SubtitleVariant{
        input.FrenchLanguage: {
            {Name: "French", Language: input.FrenchLanguage, Forced: false},
        },
        input.QuebecLanguage: {
            {Name: "Quebec French", Language: input.QuebecLanguage, Forced: false},
        },
    }
    
    // 🔹 З removeVFQ = true
    variants := cleanVariants(languages, true)
    
    // 🔹 VFQ має бути видалено, стандартна французька — залишена
    assert.Len(t, variants, 1)
    assert.Equal(t, input.FrenchLanguage, variants[0].Language)
    
    // 🔹 З removeVFQ = false
    variants = cleanVariants(languages, false)
    
    // 🔹 Обидва мають бути залишені
    assert.Len(t, variants, 2)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Пробіли у `Name` → проблеми з шляхами | Помилки запису файлів, 404 у плеєрі | 🔹 Санітизувати `Name` перед використанням у `PlaylistName()` |
| `hdmv_pgs_subtitle` не фільтрується | Помилка конвертації у WebVTT | 🔹 Перевірити умову `stream.CodecName != "hdmv_pgs_subtitle"` |
| Дублювання варіантів однієї мови | Плеєр показує однакові субтитри кілька разів | 🔹 Перевірити логіку `cleanVariants`: має бути максимум 2 на мову |
| `additionalSearcher` повертає невалідні дані | Помилки при обробці зовнішніх субтитрів | 🔹 Додати валідацію вхідних даних у `additionalSearcher` |
| Мова не розпізнається (`Unknown`) | Субтитри не групуються коректно | 🔹 Перевірити `matchLanguage()`: додати fallback на `Language: "und"` |

### Приклад санітизації імені файлу:

```go
func sanitizeForFilename(s string) string {
    // 🔹 Замінити недопустимі символи на підкреслення
    return strings.Map(func(r rune) rune {
        switch {
        case unicode.IsLetter(r), unicode.IsDigit(r):
            return r
        case r == '.', '_', '-':
            return r  // допустимі роздільники
        default:
            return '_'  // все інше → підкреслення
        }
    }, s)
}

// Використання у PlaylistName():
func (v SubtitleVariant) PlaylistName(outputDir string) string {
    sanitizedName := sanitizeForFilename(v.Name)
    filename := sanitizedName + ".m3u8"
    
    if len(outputDir) > 0 {
        return filepath.Join(outputDir, filename)
    }
    return filename
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базова генерація субтитр-варіантів:
func generateSubtitleConfig(probeURLs []string, probeData []*probe.ProbeData) []suggest.SubtitleVariant {
    return suggest.SuggestSubtitlesVariants(probeURLs, probeData, nil, false)
}

// 2: Фільтрація за підтримуваними форматами:
func filterSupportedFormats(variants []suggest.SubtitleVariant) []suggest.SubtitleVariant {
    supported := map[string]bool{
        "webvtt": true, "srt": true, "vtt": true, "ass": false,  // ass не підтримується
    }
    result := make([]suggest.SubtitleVariant, 0)
    for _, v := range variants {
        // 🔹 Перевірити розширення або кодек
        ext := strings.ToLower(filepath.Ext(v.InputURL))
        if supported[ext[1:]] {  // видалити крапку
            result = append(result, v)
        } else {
            log.Warnf("Skipping unsupported subtitle format: %s", v.InputURL)
        }
    }
    return result
}

// 3: Сортування варіантів для пріоритету у плеєрі:
func sortVariantsByPriority(variants []suggest.SubtitleVariant) []suggest.SubtitleVariant {
    sort.Slice(variants, func(i, j int) bool {
        a, b := variants[i], variants[j]
        
        // 🔹 Спочатку FORCED (для іноземних реплік)
        if a.Forced && !b.Forced {
            return true
        }
        if b.Forced && !a.Forced {
            return false
        }
        
        // 🔹 Потім accessibility для слабозорих
        if a.HearingImpaired && !b.HearingImpaired {
            return true
        }
        if b.HearingImpaired && !a.HearingImpaired {
            return false
        }
        
        // 🔹 Потім за мовою (англійська перша)
        if a.Language == input.English && b.Language != input.English {
            return true
        }
        
        return false
    })
    return variants
}

// 4: Логування для відладки:
func logSubtitleVariants(channelID string, variants []suggest.SubtitleVariant) {
    log.Infof("Channel %s: %d subtitle variants generated", channelID, len(variants))
    for i, v := range variants {
        log.Debugf("  [%d] %s: lang=%s, forced=%v, hearing_impaired=%v, url=%s",
            i, v.Name, v.Language, v.Forced, v.HearingImpaired, v.InputURL)
    }
}

// 5: Конвертація у конвертер-сумісний формат:
func toConverterVariants(suggestVariants []suggest.SubtitleVariant) []converter.SubtitleVariant {
    result := make([]converter.SubtitleVariant, len(suggestVariants))
    for i, v := range suggestVariants {
        result[i] = converter.SubtitleVariant{
            InputURL:    v.InputURL,
            StreamIndex: v.StreamIndex,
            Name:        v.Name,
            Language:    v.Language,
            Forced:      v.Forced,
            // ... інші поля ...
        }
    }
    return result
}
```

---

## 📊 Матриця рішень для різних сценаріїв субтитрів

```
Вхідний потік          | Стратегія                      | Вихідні варіанти
───────────────────────┼────────────────────────────────┼─────────────────────────
WebVTT stereo          | ✅ Copy без змін               | 1× WebVTT (copy)
SRT embedded           | 🔄 Конвертація у WebVTT        | 1× WebVTT (конвертований)
PGS (Blu-ray)          | ❌ Ігнорується                  | 0 (потрібен зовнішній пошук)
Немає субтитрів        | 🔍 additionalSearcher          | 0 або N (залежить від пошуку)
FORCED + Full однієї мови| ✅ Залишити обидва           | 2× варіанти (FORCED + Full)
Кілька варіантів однієї мови| 🔹 Взяти по одному типу  | Максимум 2 на мову
```

---

## 📚 Корисні посилання

- [RFC 5646: Language Tags](https://datatracker.ietf.org/doc/html/rfc5646)
- [HLS Subtitles specification](https://developer.apple.com/documentation/http_live_streaming/about_the_radio_stream_format)
- [WebVTT specification](https://www.w3.org/TR/webvtt1/)
- [FFmpeg subtitle encoding guide](https://trac.ffmpeg.org/wiki/Subtitles)

> 💡 **Ключова ідея**: Цей `suggest.SubtitleVariant` — це "радник" для субтитрів у вашому пайплайні. Він:
> - 🎯 Автоматично визначає оптимальні варіанти на основі вхідних метаданих
> - 🔧 Балансує між якістю (вбудовані субтитри) та доступністю (зовнішній пошук)
> - 🌍 Підтримує багатомовність через мовні теги та групування
> - ♿ Забезпечує accessibility через FORCED та HearingImpaired прапорці

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку нових форматів субтитрів (ASS, SSA) або інтеграцію з OCR для PGS
- 🧪 Написати property-based тести для генерації варіантів з випадковими вхідними даними
- 📈 Додати Prometheus-метрики для моніторингу розподілу субтитр-варіантів по каналах та мовах

🛠️