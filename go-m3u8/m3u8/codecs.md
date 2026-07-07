# Глибоке роз'яснення: `m3u8.CodecMap` — відображення кодеків для HLS плейлистів

Цей файл містить **мапи відображення кодеків** для аудіо та відео у форматі, який вимагається специфікацією HLS для атрибуту `CODECS` у тегах `#EXT-X-STREAM-INF`. Це критичний компонент для сумісності з плеєрами, особливо Apple Safari/iOS.

---

## 🎯 Навіщо ці мапи потрібні у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ CodecMap у контексті HLS:              │
│                                         │
│ 🔹 Сумісність з плеєрами:              │
│   • Apple Safari/iOS вимагає технічні  │
│     кодеки у форматі "avc1.XXXXXX"     │
│   • Без CODECS атрибуту плеєр може     │
│     відмовитися відтворювати потік     │
│                                         │
│ 🔹 Адаптивний вибір якості:            │
│   • Плеєр використовує CODECS для      │
│     фільтрації підтримуваних варіантів │
│   • Уникає спроб завантажити           │
│     непідтримуваний кодек              │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Гарантія відтворення на різних     │
│     пристроях (мобільні, десктоп, ТВ)  │
│   • Підтримка різних профілів для      │
│     різних рівнів апаратного забезпечення│
└─────────────────────────────────────────┘
```

---

## 🔧 Мапи кодеків: структура та призначення

### 🎵 Аудіо кодеки: `AudioCodecMap`

```go
var AudioCodecMap = map[string]string{
    "aac-lc": "mp4a.40.2",   // 🔹 AAC Low Complexity — найпоширеніший
    "he-aac": "mp4a.40.5",   // 🔹 High Efficiency AAC — краща компресія
    "mp3":    "mp4a.40.34",  // 🔹 MP3 — застарілий, але підтримується
}
```

**Формат технічних кодеків для аудіо:**
```
mp4a.<object_type>.<sample_rate_index>

Приклади:
• mp4a.40.2  = AAC-LC, будь-яка частота дискретизації
• mp4a.40.5  = HE-AAC (SBR), будь-яка частота
• mp4a.40.34 = MP3

Де:
• 40 = MPEG-4 Audio (об'єктний тип)
• 2/5/34 = конкретний підтип кодека
```

### 🎬 Відео кодеки: профілі + рівні → технічні рядки

```go
// Baseline profile (для старих/слабких пристроїв)
var BaselineCodecMap = map[string]string{
    "3.0": "avc1.66.30",  // 🔹 Level 3.0: до 720p @ 30fps
    "3.1": "avc1.42001f", // 🔹 Level 3.1: до 720p @ 60fps / 1080p @ 30fps
}

// Main profile (баланс сумісності/якості)
var MainCodecMap = map[string]string{
    "3.0": "avc1.77.30",
    "3.1": "avc1.4d001f",
    "4.0": "avc1.4d0028",  // 🔹 Level 4.0: до 1080p @ 60fps
    "4.1": "avc1.4d0029",  // 🔹 Level 4.1: до 1080p @ 60fps, 4K @ 30fps
}

// High profile (найкраща якість, сучасні пристрої)
var HighCodecMap = map[string]string{
    "3.0": "avc1.64001e",
    "3.1": "avc1.64001f",
    "3.2": "avc1.640020",
    "4.0": "avc1.640028",
    "4.1": "avc1.640029",  // 🔹 Найпоширеніший для 1080p HLS
    "4.2": "avc1.64002a",
    "5.0": "avc1.640032",  // 🔹 Level 5.0: 4K @ 30fps
    "5.1": "avc1.640033",  // 🔹 Level 5.1: 4K @ 60fps
    "5.2": "avc1.640034",  // 🔹 Level 5.2: 4K @ 120fps / 8K @ 30fps
}
```

**Формат технічних кодеків для відео (H.264/AVC):**
```
avc1.<profile_hex><level_hex>

Розбір "avc1.64001f":
• avc1 = H.264/AVC кодек
• 64 = High profile (0x64 = 100 десяткове)
• 00 = обмеження (constraint set flags)
• 1f = Level 3.1 (0x1f = 31 десяткове)

Таблиця профілів:
• 42 (0x42) = Baseline
• 4D (0x4D) = Main  
• 64 (0x64) = High

Таблиця рівнів (десяткове → hex):
• 3.0 → 30 (0x1E)
• 3.1 → 31 (0x1F)
• 4.0 → 40 (0x28)
• 4.1 → 41 (0x29)
• 5.0 → 50 (0x32)
• 5.1 → 51 (0x33)
```

> 💡 **Важливо**: Ці значення жорстко закодовані у специфікації ISO/IEC 14496-15. Неправильне значення → плеєр може відмовитися відтворювати потік.

---

## 🔍 Функція `audioCodec`: перетворення аудіо кодека

```go
func audioCodec(codec *string) *string {
    // 🔹 1. Обробка nil входу
    if codec == nil {
        return nil
    }
    
    // 🔹 2. Нормалізація регістру для пошуку
    key := strings.ToLower(*codec)
    
    // 🔹 3. Пошук у мапі
    value, ok := AudioCodecMap[key]
    
    // 🔹 4. Повернення результату
    if !ok {
        return nil  // 🔹 Невідомий кодек → nil
    }
    return &value  // 🔹 Повертаємо покажчик на знайдене значення
}
```

### 🎯 Приклади використання:

```
Вхід: nil
Вихід: nil

Вхід: "AAC-LC" (верхній регістр)
Процес: strings.ToLower → "aac-lc" → знайдено в AudioCodecMap
Вихід: &"mp4a.40.2"

Вхід: "unknown_codec"
Процес: не знайдено в мапі
Вихід: nil
```

> 💡 **Чому покажчики?** Це дозволяє розрізняти "не вказано" (nil) та "відомий, але не підтримується" (також nil, але з іншою семантикою). Клієнтський код може логувати різні випадки.

---

## 🔍 Функція `videoCodec`: перетворення відео профілю+рівня

```go
func videoCodec(profile *string, level *string) *string {
    // 🔹 1. Обидва параметри мають бути вказані
    if profile == nil || level == nil {
        return nil
    }
    
    var value string
    var ok bool
    
    // 🔹 2. Вибір мапи за профілем
    switch *profile {
    case "baseline":
        value, ok = BaselineCodecMap[*level]
    case "main":
        value, ok = MainCodecMap[*level]
    case "high":
        value, ok = HighCodecMap[*level]
    }
    
    // 🔹 3. Перевірка результату
    if !ok {
        return nil  // 🔹 Невідомий профіль/рівень
    }
    
    return &value
}
```

### 🎯 Приклади використання:

```
Вхід: profile="high", level="4.1"
Процес: 
  • *profile = "high" → вибір HighCodecMap
  • HighCodecMap["4.1"] = "avc1.640029" → знайдено
Вихід: &"avc1.640029"

Вхід: profile="high", level="6.0"
Процес:
  • "6.0" не існує у HighCodecMap → ok = false
Вихід: nil

Вхід: profile="baseline", level=nil
Процес: level == nil → раннє повернення
Вихід: nil
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Генерація CODECS атрибуту для #EXT-X-STREAM-INF

```go
// У suggest.VideoVariant.Stanza() — додавання CODECS:
func (v VideoVariant) Stanza(streamPlaylistFilename string, audioGroup *string, subtitleGroup *string) string {
    var optionsList []string
    
    // 🔹 Обов'язкові атрибути
    optionsList = append(optionsList,
        fmt.Sprintf("BANDWIDTH=%v", v.Bandwidth),
        fmt.Sprintf("RESOLUTION=%v", v.Resolution))
    
    // 🔹 Додати CODECS (якщо можливо)
    if codecs := buildCodecsAttribute(v.Codec, v.Profile, v.Level, audioGroup); codecs != "" {
        optionsList = append(optionsList, fmt.Sprintf("CODECS=\"%s\"", codecs))
    }
    
    // ... решта логіки ...
}

func buildCodecsAttribute(videoCodecName, profile, level *string, audioGroup *string) string {
    var codecs []string
    
    // 🔹 Відео кодек
    if videoCodecName != nil {
        if *videoCodecName == "libx264" || *videoCodecName == "h264" {
            if tech := videoCodec(profile, level); tech != nil {
                codecs = append(codecs, *tech)
            }
        } else if *videoCodecName == "libx265" || *videoCodecName == "hevc" {
            // 🔹 HEVC має інший формат: hvc1.<profile>.<level>
            codecs = append(codecs, "hvc1.1.6.L120.90")  // 🔹 Приклад, потребує динамічного розрахунку
        }
    }
    
    // 🔹 Аудіо кодек (якщо є група аудіо)
    if audioGroup != nil {
        // 🔹 Припустимо AAC-LC за замовчуванням
        if tech := audioCodec(stringPtr("aac-lc")); tech != nil {
            codecs = append(codecs, *tech)
        }
    }
    
    if len(codecs) == 0 {
        return ""
    }
    return strings.Join(codecs, ",")
}

func stringPtr(s string) *string { return &s }
```

### ✅ 2: Валідація профілю/рівня перед генерацією

```go
// Перевірити, що профіль/рівень підтримуються:
func validateVideoProfile(profile, level string) error {
    validProfiles := map[string]bool{
        "baseline": true, "main": true, "high": true,
    }
    if !validProfiles[profile] {
        return fmt.Errorf("unsupported profile: %s", profile)
    }
    
    // 🔹 Перевірити наявність рівня у відповідній мапі
    var levelMap map[string]string
    switch profile {
    case "baseline": levelMap = BaselineCodecMap
    case "main": levelMap = MainCodecMap
    case "high": levelMap = HighCodecMap
    }
    
    if _, ok := levelMap[level]; !ok {
        return fmt.Errorf("unsupported level %s for profile %s", level, profile)
    }
    
    return nil
}
```

### ✅ 3: Моніторинг використання кодеків

```go
// monitoring.Monitor — метрики для кодеків:
type CodecMetrics struct {
    CodecUsage        *prometheus.CounterVec  // розподіл технічних кодеків
    UnknownCodecs     *prometheus.CounterVec  // кількість невпізнаних кодеків
    ProfileDistribution *prometheus.CounterVec  // розподіл профілів
    LevelDistribution *prometheus.CounterVec  // розподіл рівнів
}

// У процесі генерації:
func monitorCodecMapping(channelID string, profile, level *string, 
                        metrics *CodecMetrics) {
    
    if profile != nil {
        metrics.ProfileDistribution.WithLabelValues(channelID, *profile).Inc()
    }
    if level != nil {
        metrics.LevelDistribution.WithLabelValues(channelID, *level).Inc()
    }
    
    if tech := videoCodec(profile, level); tech != nil {
        metrics.CodecUsage.WithLabelValues(channelID, *tech).Inc()
    } else if profile != nil || level != nil {
        metrics.UnknownCodecs.WithLabelValues(channelID).Inc()
        log.Warnf("Channel %s: unknown codec mapping for profile=%s, level=%s", 
            channelID, deref(profile), deref(level))
    }
}

func deref(s *string) string { if s != nil { return *s }; return "<nil>" }
```

### ✅ 4: Розширення мап для нових кодеків

```go
// 🔹 Додати підтримку HEVC/H.265:
var HEVCCodecMap = map[string]string{
    // Формат: hvc1.<profile_space><profile><tier><level>
    // Спрощено для прикладу:
    "4.0": "hvc1.1.6.L120.90",  // Main profile, Level 4.0
    "4.1": "hvc1.1.6.L123.90",  // Main profile, Level 4.1
    "5.0": "hvc1.1.6.L150.90",  // Main profile, Level 5.0 (4K)
    "5.1": "hvc1.1.6.L153.90",  // Main profile, Level 5.1 (4K @ 60fps)
}

func hevcCodec(level *string) *string {
    if level == nil {
        return nil
    }
    value, ok := HEVCCodecMap[*level]
    if !ok {
        return nil
    }
    return &value
}

// 🔹 Додати підтримку AV1 (майбутнє):
var AV1CodecMap = map[string]string{
    "4.0": "av01.0.04M.08",  // Main profile, Level 4.0
    "4.1": "av01.0.05M.08",  // Main profile, Level 4.1
    "5.0": "av01.0.08M.08",  // Main profile, Level 5.0 (4K)
}
```

### ✅ 5: Fallback для невідомих кодеків

```go
// Стратегія: якщо кодек невідомий → використати безпечний дефолт
func safeVideoCodec(profile, level *string) string {
    if tech := videoCodec(profile, level); tech != nil {
        return *tech
    }
    
    // 🔹 Fallback: Main profile, Level 4.1 — найширша сумісність
    log.Warnf("Unknown profile/level (%s/%s), using fallback avc1.4d001f", 
        deref(profile), deref(level))
    return "avc1.4d001f"  // Main@4.1
}

// Використання:
codecsAttr := fmt.Sprintf("CODECS=\"%s,%s\"", 
    safeVideoCodec(v.Profile, v.Level),
    deref(audioCodec(stringPtr("aac-lc"))))
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на аудіо кодеки

```go
func TestAudioCodec(t *testing.T) {
    testCases := []struct {
        input    *string
        expected *string
    }{
        {nil, nil},
        {stringPtr("aac-lc"), stringPtr("mp4a.40.2")},
        {stringPtr("AAC-LC"), stringPtr("mp4a.40.2")},  // 🔹 регістр нечутливий
        {stringPtr("he-aac"), stringPtr("mp4a.40.5")},
        {stringPtr("mp3"), stringPtr("mp4a.40.34")},
        {stringPtr("unknown"), nil},  // 🔹 Невідомий → nil
    }
    
    for _, tc := range testCases {
        t.Run(deref(tc.input), func(t *testing.T) {
            result := audioCodec(tc.input)
            if tc.expected == nil {
                assert.Nil(t, result)
            } else {
                assert.NotNil(t, result)
                assert.Equal(t, *tc.expected, *result)
            }
        })
    }
}
```

### 🔹 Тест на відео кодеки

```go
func TestVideoCodec(t *testing.T) {
    testCases := []struct {
        profile  *string
        level    *string
        expected *string
    }{
        // 🔹 Baseline
        {stringPtr("baseline"), stringPtr("3.0"), stringPtr("avc1.66.30")},
        {stringPtr("baseline"), stringPtr("3.1"), stringPtr("avc1.42001f")},
        
        // 🔹 Main
        {stringPtr("main"), stringPtr("4.1"), stringPtr("avc1.4d0029")},
        
        // 🔹 High
        {stringPtr("high"), stringPtr("4.1"), stringPtr("avc1.640029")},
        {stringPtr("high"), stringPtr("5.1"), stringPtr("avc1.640033")},
        
        // 🔹 Невідомі комбінації
        {stringPtr("high"), stringPtr("6.0"), nil},  // Level не існує
        {stringPtr("unknown"), stringPtr("4.1"), nil},  // Профіль не існує
        {nil, stringPtr("4.1"), nil},  // nil profile
        {stringPtr("high"), nil, nil},  // nil level
    }
    
    for _, tc := range testCases {
        name := fmt.Sprintf("%s/%s", deref(tc.profile), deref(tc.level))
        t.Run(name, func(t *testing.T) {
            result := videoCodec(tc.profile, tc.level)
            if tc.expected == nil {
                assert.Nil(t, result)
            } else {
                assert.NotNil(t, result)
                assert.Equal(t, *tc.expected, *result)
            }
        })
    }
}

func stringPtr(s string) *string { return &s }
func deref(s *string) string { if s != nil { return *s }; return "<nil>" }
```

### 🔹 Інтеграційний тест на генерацію CODECS атрибуту

```go
func TestBuildCodecsAttribute_Integration(t *testing.T) {
    // 🔹 Підготувати варіант з підтримуваним кодеком
    variant := suggest.VideoVariant{
        Codec:   "libx264",
        Profile: stringPtr("high"),
        Level:   stringPtr("4.1"),
    }
    
    codecs := buildCodecsAttribute(&variant.Codec, variant.Profile, variant.Level, stringPtr("audio"))
    
    // 🔹 Очікуємо: avc1.640029 (відео) + mp4a.40.2 (аудіо)
    assert.Contains(t, codecs, "avc1.640029")
    assert.Contains(t, codecs, "mp4a.40.2")
    assert.Contains(t, codecs, ",")  // 🔹 Роздільник між кодеками
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невідомий профіль/рівень | `videoCodec` повертає nil, CODECS відсутній | 🔹 Додати fallback на безпечний дефолт (Main@4.1) |
| Регістрозалежність пошуку | "HIGH" не знаходиться у мапі | 🔹 Завжди використовувати `strings.ToLower()` перед пошуком ✅ |
| Відсутність CODECS у плейлисті | Плеєр не відтворює потік на iOS | 🔹 Завжди додавати CODECS атрибут для відео-варіантів |
| Неправильний формат технічного кодека | Помилка валідації плеєром | 🔹 Перевіряти значення за специфікацією; додати юніт-тести |
| HEVC кодек не підтримується | Відсутність мапи для hvc1 | 🔹 Додати HEVCCodecMap з правильними значеннями |

### Приклад валідації технічного кодека:

```go
// 🔹 Перевірити формат avc1.XXXXXX
func validateAVC1Codec(codec string) error {
    if !strings.HasPrefix(codec, "avc1.") {
        return fmt.Errorf("invalid AVC1 codec prefix: %s", codec)
    }
    
    parts := strings.Split(codec, ".")
    if len(parts) != 2 {
        return fmt.Errorf("invalid AVC1 codec format: %s", codec)
    }
    
    // 🔹 Перевірити, що після "avc1." — 6 hex цифр
    hexPart := parts[1]
    if len(hexPart) != 6 {
        return fmt.Errorf("AVC1 codec must have 6 hex digits: %s", hexPart)
    }
    if _, err := strconv.ParseUint(hexPart, 16, 32); err != nil {
        return fmt.Errorf("invalid hex in AVC1 codec: %s", hexPart)
    }
    
    return nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базове перетворення кодека:
func getTechnicalCodec(profile, level string) string {
    if tech := videoCodec(&profile, &level); tech != nil {
        return *tech
    }
    return "avc1.4d001f"  // fallback
}

// 2: Побудова CODECS атрибуту:
func buildCodecsAttr(videoProfile, videoLevel, audioCodec string) string {
    var parts []string
    
    if videoTech := videoCodec(&videoProfile, &videoLevel); videoTech != nil {
        parts = append(parts, *videoTech)
    }
    if audioTech := audioCodec(&audioCodec); audioTech != nil {
        parts = append(parts, *audioTech)
    }
    
    if len(parts) == 0 {
        return ""
    }
    return strings.Join(parts, ",")
}

// 3: Форматування для HLS тега:
func formatStreamInfTag(bandwidth, resolution, codecs, audioGroup string) string {
    attrs := []string{
        fmt.Sprintf("BANDWIDTH=%s", bandwidth),
        fmt.Sprintf("RESOLUTION=%s", resolution),
    }
    if codecs != "" {
        attrs = append(attrs, fmt.Sprintf("CODECS=\"%s\"", codecs))
    }
    if audioGroup != "" {
        attrs = append(attrs, fmt.Sprintf("AUDIO=\"%s\"", audioGroup))
    }
    return fmt.Sprintf("#EXT-X-STREAM-INF:%s", strings.Join(attrs, ","))
}

// 4: Логування для відладки:
func logCodecMapping(profile, level *string, result *string) {
    if result != nil {
        log.Debugf("Mapped %s/%s → %s", deref(profile), deref(level), *result)
    } else {
        log.Warnf("Failed to map codec: profile=%s, level=%s", deref(profile), deref(level))
    }
}

// 5: Кешування результатів для продуктивності:
var codecCache = sync.Map{}  // key: "profile:level", value: string

func cachedVideoCodec(profile, level *string) *string {
    if profile == nil || level == nil {
        return nil
    }
    
    key := *profile + ":" + *level
    if cached, ok := codecCache.Load(key); ok {
        if s, ok := cached.(string); ok && s != "" {
            return &s
        }
        return nil  // кешовано як nil
    }
    
    result := videoCodec(profile, level)
    
    // 🔹 Зберегти у кеш
    if result != nil {
        codecCache.Store(key, *result)
    } else {
        codecCache.Store(key, "")  // маркер "не знайдено"
    }
    
    return result
}
```

---

## 📊 Матриця підтримуваних кодеків

```
Профіль   | Рівень | Технічний кодек | Макс. роздільна здатність | Призначення
──────────┼────────┼─────────────────┼───────────────────────────┼─────────────────────────
baseline  | 3.0    | avc1.66.30      | 720p @ 30fps              | 🔹 Старі пристрої, низька складність
baseline  | 3.1    | avc1.42001f     | 720p @ 60fps / 1080p @ 30fps | 🔹 Базова сумісність
main      | 4.1    | avc1.4d0029     | 1080p @ 60fps / 4K @ 30fps   | 🔹 Баланс сумісності/якості
high      | 4.1    | avc1.640029     | 1080p @ 60fps / 4K @ 30fps   | ✅ Найпоширеніший для HLS
high      | 5.1    | avc1.640033     | 4K @ 60fps                   | 🔹 4K контент, сучасні пристрої
```

---

## 📚 Корисні посилання

- [HLS RFC Draft: CODECS attribute](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.4.2)
- [ISO/IEC 14496-15: AVC/H.264 codec strings](https://www.iso.org/standard/75467.html)
- [Apple HLS Authoring Specification](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [FourCC codes for H.264](https://fourcc.org/codecs.php)

> 💡 **Ключова ідея**: Ці мапи кодеків — це "перекладач" між людсько-читабельними налаштуваннями енкодера та технічними рядками, які розуміють плеєри. Вони:
> - 🎯 Гарантують сумісність з широким спектром пристроїв через стандартизовані кодеки
> - 🔧 Дозволяють гнучко підтримувати різні профілі для різних рівнів апаратного забезпечення
> - ⚡ Прискорюють розробку через централізоване відображення замість дублювання логіки
> - 🛡️ Граційно обробляють невідомі кодеки через nil-повернення та fallback стратегії

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку HEVC/H.265 та AV1 кодеків з правильними технічними рядками
- 🧪 Написати property-based тести для валідації всіх комбінацій профіль/рівень
- 📈 Додати Prometheus-метрики для моніторингу розподілу використаних кодеків по каналах та пристроях

🛠️