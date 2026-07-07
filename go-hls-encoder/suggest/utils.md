# Глибоке роз'яснення: `suggest.match*` функції — детекція мови та прапорців у субтитрах

Цей файл містить **логіку інтелектуального розпізнавання метаданих** для субтитрів: визначення мови за тегами та назвами, детекція `FORCED` та `HearingImpaired` прапорців. Це критично для коректного групування та відображення субтитрів у HLS-плеєрах.

---

## 🎯 Навіщо ці функції потрібні у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ match* функції у контексті HLS:        │
│                                         │
│ 🔹 Автоматична класифікація:           │
│   • Визначення мови субтитрів без      │
│     ручного втручання                  │
│   • Детекція спеціальних типів:        │
│     FORCED, HearingImpaired            │
│                                         │
│ 🔹 Підтримка багатомовності:           │
│   • Розрізнення стандартної французької│
│     (fra) та квебекської (vfq)         │
│   • Автовибір мови плеєром за налашту-│
│     ваннями користувача                │
│                                         │
│ 🔹 Accessibility:                      │
│   • Виявлення субтитрів для слабозорих │
│   • Підтримка FORCED для іноземних     │
│     реплік у фільмах                   │
└─────────────────────────────────────────┘
```

---

## 🔍 Функція `matchLanguage`: розпізнавання мови за метаданими

```go
func matchLanguage(stream *probe.ProbeStream) input.Language {
    currentGuess := input.Unknown  // 🔹 Початкове припущення
    
    // 🔹 Крок 1: Аналіз заголовка (Title tag)
    if len(stream.Tags.Title) > 0 {
        // 🔹 Регулярні вирази для різних мов
        matchVFF := regexp.MustCompile(`\b(vff|vfi|true(\b)*french)\b`)      // True French
        matchVFQ := regexp.MustCompile(`\bvfq\b|\bqu[eé]bec[a-z]*\b`)        // Quebec French
        matchFrench := regexp.MustCompile(`(fre|french|fran[cç]ais)`)         // French
        matchEnglish := regexp.MustCompile(`(ang|angl|eng|engl|anglais|english|vo)`)  // English
        
        titleString := strings.ToLower(stream.Tags.Title)
        
        switch {
        // 🔹 Пріоритет 1: Точні збіги для VFQ/VFF → повертаємо одразу
        case matchVFF.MatchString(titleString):
            return input.TrueFrench
        case matchVFQ.MatchString(titleString):
            return input.QuebecLanguage
            
        // 🔹 Пріоритет 2: Загальні збіги → запам'ятовуємо як припущення
        case matchFrench.MatchString(titleString):
            currentGuess = input.FrenchLanguage
        case matchEnglish.MatchString(titleString):
            currentGuess = input.EnglishLanguage
        }
    }
    
    // 🔹 Крок 2: Аналіз Language tag (має вищий пріоритет)
    if len(stream.Tags.Language) == 0 {
        return currentGuess  // 🔹 Повернути припущення з Title або Unknown
    }
    
    // 🔹 Строгі регулярні вирази для Language tag (^...$ = точний збіг)
    matchFrench := regexp.MustCompile(`^(fre|french|fran[cç]ais)$`)
    matchEnglish := regexp.MustCompile(`^(ang|angl|eng|engl|anglais|english)$`)
    
    languageString := strings.ToLower(stream.Tags.Language)
    switch {
    case matchFrench.MatchString(languageString):
        return input.FrenchLanguage
    case matchEnglish.MatchString(languageString):
        return input.EnglishLanguage
    default:
        return currentGuess  // 🔹 fallback на припущення з Title
    }
}
```

### 🎯 Ключові аспекти логіки

#### 🔹 Дворівнева стратегія: Title → Language

```
Пріоритет 1: Language tag (якщо є)
✅ Точні збіги через ^...$ (повний рядок)
✅ Строга валідація: "eng" = English, "eng-sub" = не збіг
✅ Повертає результат одразу при збігу

Пріоритет 2: Title tag (якщо Language відсутній)
✅ Гнучкі збіги через \b... (слово межа)
✅ Підтримка варіацій: "VO", "anglais", "English Audio"
✅ Запам'ятовує припущення, але не повертає одразу

Фінал:
• Якщо Language збігся → повертаємо його
• Якщо ні → повертаємо припущення з Title або Unknown
```

#### 🔹 Розрізнення французьких варіантів

```
Регулярні вирази:
• matchVFF: \b(vff|vfi|true(\b)*french)\b
  → "VFF", "VFI", "True French", "true french"
  
• matchVFQ: \bvfq\b|\bqu[eé]bec[a-z]*\b
  → "VFQ", "Quebec", "Québec", "quebecois"
  
• matchFrench: (fre|french|fran[cç]ais)
  → "fre", "french", "français", "francais"

Пріоритет повернення:
1. VFF/VFI → input.TrueFrench ✅
2. VFQ/Quebec → input.QuebecLanguage ✅
3. Інші французькі → input.FrenchLanguage (припущення)

Чому це важливо:
• Квебекська французька має відмінності у лексиці/вимові
• Для міжнародного контенту краще показувати стандартну французьку
• Але якщо це єдина французька доріжка — краще показати її, ніж нічого
```

#### 🔹 Гнучкість регулярних виразів

```
Для Title (гнучкі збіги):
• \b = межа слова → "english" збігається, "englishness" — ні
• [cç] = підтримка "français" та "francais"
• strings.ToLower() → нечутливість до регістру

Для Language (строгі збіги):
• ^...$ = тільки повний рядок
• "eng" ✅, "english" ✅, "eng-sub" ❌

Це забезпечує баланс:
✅ Гнучкість для людських назв (Title)
✅ Точність для машинних тегів (Language)
```

---

## 🔍 Функція `matchForcedTag`: детекція FORCED субтитрів

```go
func matchForcedTag(stream *probe.ProbeStream) bool {
    return stream.Disposition.Forced == 1
}
```

### 🎯 Що таке FORCED субтитри?

```
FORCED = субтитри, які мають показуватися автоматично, 
коли в аудіо є іноземна мова, навіть якщо користувач 
вимкнув субтитри загалом.

Приклади використання:
• Російські репліки в англомовному фільмі → показати англійські субтитри
• Іноземні написи у кадрі → перекласти їх
• Культурні посилання, які важливо зрозуміти

HLS реалізація:
#EXT-X-MEDIA:TYPE=SUBTITLES,...,FORCED=YES,...

Поведінка плеєра:
• Якщо аудіо = English, а в кадрі російська мова → показати FORCED English subs
• Якщо користувач увімкнув повні субтитри → показувати всі, включаючи FORCED
```

### 🔹 Як визначається у ffprobe?

```
ffprobe вивід для субтитрів:
"disposition": {
    "default": 0,
    "dub": 0,
    "original": 0,
    "comment": 0,
    "lyrics": 0,
    "karaoke": 0,
    "forced": 1,        // 🔹 Це поле ми читаємо
    "hearing_impaired": 0,
    "visual_impaired": 0,
    "clean_effects": 0,
    "attached_pic": 0
}

У Go структурі:
type StreamDisposition struct {
    Forced int `json:"forced"`  // 1 = true, 0 = false
    // ...
}
```

---

## 🔍 Функція `matchHearingImpairedTag`: детекція accessibility субтитрів

```go
func matchHearingImpairedTag(stream *probe.ProbeStream) bool {
    return stream.Disposition.HearingImpaired == 1
}
```

### 🎯 Що таке HearingImpaired (SDH) субтитри?

```
SDH = Subtitles for the Deaf and Hard of Hearing

Відмінності від звичайних субтитрів:
✅ Описують не тільки діалоги, а й:
   • Звукові ефекти: [door creaks], [music swells]
   • Ідентифікація мовця: [John]: Hello
   • Емоційні підказки: [whispering], [sarcastically]
✅ Часто мають більший шрифт, контрастний колір
✅ Можуть включати опис візуальних елементів

HLS реалізація:
#EXT-X-MEDIA:TYPE=SUBTITLES,...,CHARACTERISTICS="public.accessibility.transcribes-spoken-dialog,public.accessibility.describes-music-and-sound",...

Переваги для користувачів:
• Доступність для слабозорих/глухих
• Краще розуміння контексту в шумних умовах
• Відповідність законодавству про accessibility (напр., ADA у США)
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Розширення підтримки мов

```go
// 🔹 Додати підтримку української мови у matchLanguage:
func matchLanguage(stream *probe.ProbeStream) input.Language {
    // ... існуючий код ...
    
    // 🔹 Додати українські регулярні вирази
    matchUkrainian := regexp.MustCompile(`(ukr|ukrainian|україн[сьс]ка|укр)`)
    
    // У аналізі Title:
    case matchUkrainian.MatchString(titleString):
        currentGuess = input.UkrainianLanguage
    
    // У аналізі Language tag:
    case matchUkrainian.MatchString(languageString):
        return input.UkrainianLanguage
        
    // ... решта коду ...
}
```

### ✅ 2: Логування для відладки розпізнавання мов

```go
// 🔹 Додати детальне логування у matchLanguage:
func matchLanguageWithLogging(stream *probe.ProbeStream, streamID string) input.Language {
    result := matchLanguage(stream)
    
    log.Debugf("Stream %s: language detection", streamID)
    log.Debugf("  Title: '%s'", stream.Tags.Title)
    log.Debugf("  Language tag: '%s'", stream.Tags.Language)
    log.Debugf("  Result: %s", result)
    
    return result
}

// Використання у SuggestSubtitlesVariants:
language := matchLanguageWithLogging(stream, fmt.Sprintf("%s:%d", inputURL, streamIndex))
```

### ✅ 3: Валідація розпізнаних прапорців

```go
// 🔹 Перевірити консистентність Forced/HearingImpaired з іншими метаданими:
func validateSubtitleFlags(stream *probe.ProbeStream, language input.Language) error {
    // 🔹 FORCED має сенс тільки для іноземних мов
    if matchForcedTag(stream) && language == input.English {
        log.Warnf("FORCED flag on English subtitles may be redundant")
    }
    
    // 🔹 HearingImpaired має мати відповідні CHARACTERISTICS у HLS
    if matchHearingImpairedTag(stream) {
        // Перевірити, що конвертер додасть accessibility теги
        // ... логіка перевірки ...
    }
    
    return nil
}
```

### ✅ 4: Моніторинг розподілу мов та прапорців

```go
// monitoring.Monitor — метрики для детекції метаданих:
type MetadataDetectionMetrics struct {
    LanguageDetected *prometheus.CounterVec  // розподіл виявлених мов
    ForcedDetected   *prometheus.CounterVec  // кількість FORCED субтитрів
    AccessibilityDetected *prometheus.CounterVec  // кількість SDH субтитрів
    DetectionConfidence *prometheus.HistogramVec  // впевненість детекції (Title vs Language tag)
}

// У процесі детекції:
func monitorMetadataDetection(streamID string, stream *probe.ProbeStream, 
                             language input.Language, metrics *MetadataDetectionMetrics) {
    
    metrics.LanguageDetected.WithLabelValues(string(language)).Inc()
    
    if matchForcedTag(stream) {
        metrics.ForcedDetected.WithLabelValues(streamID).Inc()
    }
    if matchHearingImpairedTag(stream) {
        metrics.AccessibilityDetected.WithLabelValues(streamID).Inc()
    }
    
    // 🔹 Впевненість: Language tag = висока, Title = середня, Unknown = низька
    confidence := 0.0
    if stream.Tags.Language != "" {
        confidence = 1.0  // висока впевненість
    } else if stream.Tags.Title != "" {
        confidence = 0.5  // середня впевненість
    }
    metrics.DetectionConfidence.WithLabelValues(streamID).Observe(confidence)
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на розпізнавання мов за Title

```go
func TestMatchLanguage_Title(t *testing.T) {
    testCases := []struct {
        name     string
        title    string
        expected input.Language
    }{
        {"VFF", "English Audio / VFF", input.TrueFrench},
        {"VFQ", "Quebec French Track", input.QuebecLanguage},
        {"French", "French Subtitles", input.FrenchLanguage},
        {"English", "English VO Track", input.EnglishLanguage},
        {"Unknown", "Some Random Title", input.Unknown},
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            stream := &probe.ProbeStream{
                Tags: probe.StreamTags{
                    Title: tc.title,
                    // Language порожній → використовуємо Title
                },
            }
            result := matchLanguage(stream)
            assert.Equal(t, tc.expected, result)
        })
    }
}
```

### 🔹 Тест на пріоритет Language tag над Title

```go
func TestMatchLanguage_Priority(t *testing.T) {
    // 🔹 Title каже "French", але Language tag = "eng" → має повернути English
    stream := &probe.ProbeStream{
        Tags: probe.StreamTags{
            Title:    "French Subtitles",  // вводить в оману
            Language: "eng",               // правильний тег
        },
    }
    
    result := matchLanguage(stream)
    assert.Equal(t, input.EnglishLanguage, result)  // ✅ Language tag має пріоритет
}
```

### 🔹 Тест на регістро-нечутливість та варіації

```go
func TestMatchLanguage_CaseVariations(t *testing.T) {
    variations := []string{
        "ENG", "eng", "Eng", "EnGlIsH",  // English варіації
        "FRENCH", "french", "Français", "FRANCAIS",  // French варіації
        "VFQ", "vfq", "Québec", "quebecois",  // Quebec French
    }
    
    for _, variant := range variations {
        t.Run(variant, func(t *testing.T) {
            stream := &probe.ProbeStream{
                Tags: probe.StreamTags{
                    Language: variant,
                },
            }
            result := matchLanguage(stream)
            // 🔹 Перевірити, що хоча б щось розпізналось (не Unknown)
            assert.NotEqual(t, input.Unknown, result, "Failed to recognize: %s", variant)
        })
    }
}
```

### 🔹 Тест на Forced/HearingImpaired прапорці

```go
func TestMatchForcedTag(t *testing.T) {
    stream := &probe.ProbeStream{
        Disposition: probe.StreamDisposition{
            Forced: 1,  // 🔹 Встановлено
        },
    }
    assert.True(t, matchForcedTag(stream))
    
    stream.Disposition.Forced = 0
    assert.False(t, matchForcedTag(stream))
}

func TestMatchHearingImpairedTag(t *testing.T) {
    stream := &probe.ProbeStream{
        Disposition: probe.StreamDisposition{
            HearingImpaired: 1,  // 🔹 Встановлено
        },
    }
    assert.True(t, matchHearingImpairedTag(stream))
    
    stream.Disposition.HearingImpaired = 0
    assert.False(t, matchHearingImpairedTag(stream))
}
```

### 🔹 Інтеграційний тест на повний пайплайн детекції

```go
func TestMatchLanguage_Integration(t *testing.T) {
    // 🔹 Підготувати реалістичні дані з ffprobe
    stream := &probe.ProbeStream{
        Tags: probe.StreamTags{
            Title:    "Ukrainian Subtitles [SDH]",
            Language: "ukr",
        },
        Disposition: probe.StreamDisposition{
            HearingImpaired: 1,  // SDH
            Forced:          0,
        },
    }
    
    // 🔹 Перевірити мову
    language := matchLanguage(stream)
    assert.Equal(t, input.UkrainianLanguage, language)  // ✅ Розпізнано українську
    
    // 🔹 Перевірити прапорці
    assert.True(t, matchHearingImpairedTag(stream))   // ✅ SDH детектовано
    assert.False(t, matchForcedTag(stream))           // ✅ Not forced
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Нерозпізнана мова → `Unknown` | Субтитри не групуються, плеєр не показує автовибір | 🔹 Додати більше регулярних виразів для рідкісних мов; додати fallback на `Language: "und"` |
| False positive у Title | "English" у назві не-англійських субтитрів → неправильна мова | 🔹 Підвищити пріоритет Language tag; додати валідацію за контекстом |
| Регістрозалежність | "ENG" не розпізнається як English | 🔹 Завжди використовувати `strings.ToLower()` перед порівнянням |
| Часткові збіги у Language tag | "eng-sub" збігається з "eng" → неправильна мова | 🔹 Використовувати `^...$` для точних збігів у Language tag |
| Відсутність `Disposition` у ffprobe | `Forced`/`HearingImpaired` завжди `false` | 🔹 Перевірити версію ffprobe; додати `-show_entries stream=disposition` у виклик |

### Приклад розширення підтримки мов:

```go
// 🔹 Додати підтримку іспанської, німецької, італійської:
func matchLanguage(stream *probe.ProbeStream) input.Language {
    // ... існуючий код ...
    
    // 🔹 Додати нові регулярні вирази
    matchSpanish := regexp.MustCompile(`(spa|spanish|español|castellano)`)
    matchGerman := regexp.MustCompile(`(ger|deu|german|deutsch)`)
    matchItalian := regexp.MustCompile(`(ita|italian|italiano)`)
    
    titleString := strings.ToLower(stream.Tags.Title)
    languageString := strings.ToLower(stream.Tags.Language)
    
    // 🔹 У Title (гнучкі збіги)
    switch {
    // ... існуючі case ...
    case matchSpanish.MatchString(titleString):
        currentGuess = input.SpanishLanguage
    case matchGerman.MatchString(titleString):
        currentGuess = input.GermanLanguage
    case matchItalian.MatchString(titleString):
        currentGuess = input.ItalianLanguage
    }
    
    // 🔹 У Language tag (строгі збіги)
    switch {
    // ... існуючі case ...
    case matchSpanish.MatchString(languageString):
        return input.SpanishLanguage
    case matchGerman.MatchString(languageString):
        return input.GermanLanguage
    case matchItalian.MatchString(languageString):
        return input.ItalianLanguage
    }
    
    return currentGuess
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базове розпізнавання мови:
func detectSubtitleLanguage(stream *probe.ProbeStream) input.Language {
    return matchLanguage(stream)
}

// 2: Перевірка прапорців:
func isForcedSubtitle(stream *probe.ProbeStream) bool {
    return matchForcedTag(stream)
}

func isAccessibilitySubtitle(stream *probe.ProbeStream) bool {
    return matchHearingImpairedTag(stream)
}

// 3: Комплексна перевірка метаданих:
type SubtitleMetadata struct {
    Language        input.Language
    IsForced        bool
    IsAccessibility bool
    Confidence      float64  // 0.0-1.0: впевненість у розпізнаванні
}

func extractSubtitleMetadata(stream *probe.ProbeStream) SubtitleMetadata {
    lang := matchLanguage(stream)
    
    // 🔹 Впевненість: Language tag = 1.0, Title = 0.5, Unknown = 0.0
    confidence := 0.0
    if stream.Tags.Language != "" {
        confidence = 1.0
    } else if stream.Tags.Title != "" {
        confidence = 0.5
    }
    
    return SubtitleMetadata{
        Language:        lang,
        IsForced:        matchForcedTag(stream),
        IsAccessibility: matchHearingImpairedTag(stream),
        Confidence:      confidence,
    }
}

// 4: Логування для відладки:
func logSubtitleMetadata(streamID string, stream *probe.ProbeStream) {
    meta := extractSubtitleMetadata(stream)
    log.Infof("Subtitle %s: lang=%s (conf=%.1f), forced=%v, accessibility=%v",
        streamID, meta.Language, meta.Confidence, meta.IsForced, meta.IsAccessibility)
}

// 5: Фільтрація за критеріями:
func filterSubtitlesByCriteria(variants []SubtitleVariant, 
                              requiredLangs []input.Language,
                              includeForced, includeAccessibility bool) []SubtitleVariant {
    
    result := make([]SubtitleVariant, 0)
    langSet := make(map[input.Language]bool)
    for _, lang := range requiredLangs {
        langSet[lang] = true
    }
    
    for _, v := range variants {
        // 🔹 Фільтр за мовою
        if len(requiredLangs) > 0 && !langSet[v.Language] {
            continue
        }
        // 🔹 Фільтр за Forced
        if !includeForced && v.Forced {
            continue
        }
        // 🔹 Фільтр за Accessibility
        if !includeAccessibility && v.HearingImpaired {
            continue
        }
        result = append(result, v)
    }
    return result
}
```

---

## 📊 Матриця розпізнавання мов

```
Джерело метаданих   | Приклад значень                     | Пріоритет | Впевненість
────────────────────┼─────────────────────────────────────┼───────────┼────────────
Language tag        | "eng", "fre", "ukr", "fra"          | ✅ Високий | 1.0
Title tag           | "English Audio", "VFQ", "VO"        | ⚠️ Середній| 0.5
Disposition.Forced  | 0 або 1                             | ✅ Точний  | 1.0
Disposition.HearingImpaired | 0 або 1                     | ✅ Точний  | 1.0
Відсутність даних   | —                                   | ❌ Низький | 0.0 → Unknown
```

---

## 📚 Корисні посилання

- [RFC 5646: Language Tags](https://datatracker.ietf.org/doc/html/rfc5646)
- [FFmpeg stream disposition documentation](https://ffmpeg.org/ffprobe.html)
- [HLS accessibility characteristics](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [Go regexp package](https://pkg.go.dev/regexp)

> 💡 **Ключова ідея**: Ці `match*` функції — це "перекладачі" між сирими метаданими ffprobe та бізнес-логікою вашого пайплайну. Вони:
> - 🎯 Автоматично класифікують субтитри за мовою та типом
> - 🔧 Балансують між гнучкістю (Title) та точністю (Language tag)
> - ♿ Підтримують accessibility через детекцію спеціальних прапорців
> - 🌍 Дозволяють розширення для нових мов через регулярні вирази

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку нових мов (українська, іспанська, німецька) через розширення регулярних виразів
- 🧪 Написати property-based тести для валідації розпізнавання на випадкових вхідних даних
- 📈 Додати Prometheus-метрики для моніторингу впевненості детекції мов та розподілу прапорців по каналах

🛠️