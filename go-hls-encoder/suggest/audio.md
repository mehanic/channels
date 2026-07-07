# Глибоке роз'яснення: `suggest` пакет — автоматична генерація аудіо-варіантів для HLS

Цей файл містить **логіку інтелектуального підбору аудіо-варіантів** на основі метаданих вхідних потоків. Він аналізує кодеки, кількість каналів, мовні теги та генерує оптимальну конфігурацію для адаптивного HLS-стрімінгу.

---

## 🎯 Навіщо цей код потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ suggest у контексті HLS-енкодингу:     │
│                                         │
│ 🔹 Автоматизація конфігурації:         │
│   • Не потрібно вручну вказувати       │
│     параметри для кожного вхідного     │
│     потоку                              │
│   • Адаптація до різних кодеків/мов   │
│                                         │
│ 🔹 Підтримка багатомовності:           │
│   • Детекція мовних тегів (RFC 5646)   │
│   • Групування за AUDIO group ID       │
│   • Фільтрація регіональних варіантів  │
│                                         │
│ 🔹 Оптимізація для сумісності:         │
│   • Конвертація несумісних кодеків     │
│     (DTS, TrueHD) → AAC/E-AC3          │
│   • Downmix 5.1/7.1 → stereo для       │
│     мобільних пристроїв                │
│   • Збереження оригіналу через "copy"  │
│     коли можливо                        │
└─────────────────────────────────────────┘
```

---

## 🔧 Типи даних: структура аудіо-варіантів

### `AudioVariantType`: класифікація за кількістю каналів

```go
type AudioVariantType int

const (
    StereoSound   AudioVariantType = 2  // 🔹 2 канали: L+R
    SurroundSound AudioVariantType = 6  // 🔹 5.1/7.1: багатоканальне аудіо
)
```

> 💡 **Важливо**: Цей тип використовується для іменування варіантів та логіки downmix, але не впливає на реальну обробку — важливіше поле `ConvertToStereo`.

### `AudioVariant`: конфігурація одного аудіо-варіанту

```go
type AudioVariant struct {
    // 🔹 Вхідні параметри
    MapInput        string           // 🎯 FFmpeg map: "0:1" = вхід 0, потік 1
    Codec           string           // 🎯 "aac", "eac3", "copy"...
    Type            AudioVariantType // 🎯 StereoSound/SurroundSound
    Bitrate         *string          // 🎯 "128k", "256k"... (nil = за замовчуванням)
    ConvertToStereo bool             // 🎯 Чи робити downmix 5.1→stereo
    
    // 🔹 HLS playlist метадані (EXT-X-MEDIA)
    GroupID        *string          // 🎯 Група аудіо: "audio" за замовчуванням
    Name           string           // 🎯 Унікальна назва для вибору у плеєрі
    Language       input.Language   // 🎯 Мова за RFC 5646: "eng", "ukr", "fra"...
    DescribesVideo *bool            // 🎯 Чи описує це аудіо відео (для accessibility)
}
```

### 🎯 Ключові поля для HLS сумісності

| Поле | Призначення | Приклад значення |
|------|-------------|-----------------|
| `MapInput` | Вказує FFmpeg який потік обробляти | `"0:1"` = перший вхід, другий потік |
| `Codec` | Кодек для енкодингу | `"copy"` (без переенкодингу), `"aac"`, `"eac3"` |
| `ConvertToStereo` | Чи робити downmix | `true` для мобільної сумісності |
| `GroupID` | Група для EXT-X-MEDIA | `"audio"` — всі варіанти в одній групі |
| `Name` | Назва для вибору у плеєрі | `"Audio 1 (AAC Stereo)"` |
| `Language` | Мова для автоматичного вибору | `input.English`, `input.Ukrainian` |

---

## 🔍 Функція `checkforAACsecondaryAudio`: пошук альтернативного stereo AAC

```go
func checkforAACsecondaryAudio(fileStreams []*probe.ProbeStream) (streamIndex int, err error) {
    // 🔹 Шукаємо 2-канальний AAC потік серед всіх аудіо-потоків
    for _, stream := range fileStreams {
        if stream.CodecType == "audio" {
            if stream.Channels == 2 {  // 🔹 Тільки stereo
                if stream.CodecName == "aac" {  // 🔹 Тільки AAC
                    streamIndex = stream.Index
                    fmt.Printf("Found a 2 channel aac stream")
                    return streamIndex, nil  // ✅ Знайдено!
                }
            }
        }
    }
    
    // 🔹 Не знайдено — повертаємо помилку
    err = jt_error.JoutubeError{
        ErrorType:       jt_error.ConversionError,
        Origin:          "looking for AAC audio",
        AssociatedError: errors.New("could not find secondary audio"),
    }
    return -1, err
}
```

### 🎯 Коли це використовується?

```
Сценарій: Вхідний потік має 5.1 surround sound (напр., AC3)

Проблема:
• Не всі плеєри підтримують AC3/E-AC3
• Мобільні пристрої часто мають тільки stereo динаміки

Рішення:
1. Спочатку шукаємо вже наявний stereo AAC потік у тому ж файлі
2. Якщо знайдено → копіюємо його без переенкодингу ("copy")
3. Якщо не знайдено → конвертуємо surround → stereo AAC

Переваги:
✅ Економія ресурсів: "copy" не вимагає переенкодингу
✅ Краща якість: оригінальний stereo AAC кращий за downmix
✅ Сумісність: AAC підтримується всіма HLS-плеєрами
```

---

## 🔍 Функція `SuggestAudioVariants`: основна логика генерації

```go
func SuggestAudioVariants(probeDataInputs []*probe.ProbeData, 
                         createAlternateStereo bool, 
                         removeVFQ bool) []AudioVariant {
    
    for inputIndex, probeData := range probeDataInputs {  // 🔹 Кожен вхідний файл
        for streamIndex, stream := range probeData.Streams {  // 🔹 Кожен потік
            
            // 🔹 Визначити мову з тегів
            language := matchLanguage(stream)  // ваша функція парсингу тегів
            mapInput := strconv.Itoa(inputIndex) + ":" + strconv.Itoa(streamIndex)
            
            // 🔹 Обробляємо тільки аудіо-потоки
            if stream.CodecType == "audio" {
                
                // ── CASE 1: Stereo або Mono (≤2 канали) ──
                if stream.Channels <= 2 {
                    audioType := StereoSound
                    
                    switch stream.CodecName {
                    case "aac":
                        // ✅ Вже AAC stereo → просто копіюємо
                        variants = append(variants, AudioVariant{
                            MapInput:        mapInput,
                            Type:            audioType,
                            Codec:           "copy",  // 🔹 Без переенкодингу!
                            Name:            "Audio " + strconv.Itoa(streamIndex) + " (AAC Stereo)",
                            Language:        language,
                            ConvertToStereo: false,
                        })
                        
                    default:
                        // ❌ Інший кодек (mp3, ac3...) → конвертуємо в AAC
                        bitrate := "256k"
                        variants = append(variants, AudioVariant{
                            MapInput:        mapInput,
                            Type:            audioType,
                            Codec:           "aac",
                            Bitrate:         &bitrate,
                            Name:            "Audio " + strconv.Itoa(streamIndex),
                            Language:        language,
                            ConvertToStereo: false,
                        })
                    }
                }
                
                // ── CASE 2: Surround sound (>2 канали) ──
                else {
                    audioType := SurroundSound
                    log.Println("Surround sound detected. Format:", stream.CodecName)
                    
                    switch stream.CodecName {
                    // 🔹 Підтримувані surround кодеки
                    case "aac", "ac3", "eac3":
                        // 🔹 Спробувати знайти альтернативний stereo AAC
                        idx, err := checkforAACsecondaryAudio(probeData.Streams)
                        
                        if err != nil {
                            // ❌ Не знайдено stereo AAC → створюємо варіанти
                            
                            // 🔹 Варіант 1: Оригінальний surround (копіюємо або переенкодуємо)
                            variants = append(variants, AudioVariant{
                                MapInput: mapInput,
                                Type:     audioType,
                                Codec:    "eac3",  // 🔹 E-AC3 для кращої сумісності
                                Name:     fmt.Sprintf("Audio %d (%s Surround)", 
                                    streamIndex, strings.ToUpper(stream.CodecName)),
                                Language:        language,
                                ConvertToStereo: false,
                            })
                            
                            // 🔹 Варіант 2: Stereo downmix (якщо увімкнено)
                            if createAlternateStereo {
                                bitrate := "256k"
                                variants = append(variants, AudioVariant{
                                    MapInput:        mapInput,
                                    Type:            StereoSound,
                                    Codec:           "aac",
                                    Bitrate:         &bitrate,
                                    Name:            "Audio " + strconv.Itoa(streamIndex) + " (AAC Stereo)",
                                    Language:        language,
                                    ConvertToStereo: true,  // 🔹 Ключовий прапорець!
                                })
                            }
                            
                        } else {
                            // ✅ Знайдено stereo AAC → копіюємо обидва
                            variants = append(variants, AudioVariant{
                                MapInput: mapInput,
                                Type:     audioType,
                                Codec:    "copy",  // 🔹 Оригінальний surround
                                Name:     fmt.Sprintf("Audio %d&%d (%s Surround Version)", 
                                    streamIndex, idx, strings.ToUpper(stream.CodecName)),
                                Language:        language,
                                ConvertToStereo: false,
                            })
                            // 🔹 AAC stereo вже буде додано окремо в іншій ітерації циклу
                        }
                        
                    // 🔹 Непідтримувані surround кодеки (TrueHD, DTS...)
                    case "truehd", "dca", "dts":
                        fallthrough  // 🔹 Обробляємо як default
                    default:
                        // 🔹 Конвертуємо в E-AC3 для сумісності
                        bitrate1 := "384k"
                        variants = append(variants, AudioVariant{
                            MapInput: mapInput,
                            Type:     audioType,
                            Codec:    "eac3",
                            Bitrate:  &bitrate1,
                            Name:     "Audio " + strconv.Itoa(streamIndex) + " (eAC3 Surround)",
                            Language: language,
                        })
                        
                        // 🔹 Додатково stereo AAC (якщо увімкнено)
                        if createAlternateStereo {
                            bitrate2 := "256k"
                            variants = append(variants, AudioVariant{
                                MapInput:        mapInput,
                                Type:            StereoSound,
                                Codec:           "aac",
                                Bitrate:         &bitrate2,
                                Name:            "Audio " + strconv.Itoa(streamIndex) + " (AAC Stereo)",
                                Language:        language,
                                ConvertToStereo: true,
                            })
                        }
                    }
                }
            }
        }
    }
    
    // 🔹 Фільтрація VFQ (Very French Quebec) якщо потрібно
    if removeVFQ {
        variants = removeVFQAudio(variants)
    }
    
    return variants
}
```

### 🎯 Ключові рішення в логіці

#### 🔹 Пріоритет "copy" над переенкодингом

```
Коли використовується "copy":
✅ Вхідний кодек = вихідний кодек (напр., AAC→AAC)
✅ Немає потреби змінювати бітрейт/частоту дискретизації
✅ Економія CPU та збереження якості

Коли використовується переенкодинг:
❌ Непідтримуваний кодек (DTS, TrueHD)
❌ Потрібен downmix (5.1→stereo)
❌ Потрібна зміна бітрейту для адаптивного стрімінгу
```

#### 🔹 `ConvertToStereo`: прапорець для downmix

```go
ConvertToStereo: true  // 🔹 Вказує конвертеру робити downmix
```

**Що це означає на практиці:**
```
• У converter/audioConversionArgs() цей прапорець додає:
  -ac:a:N 2  // встановити 2 канали
  -filter:a:N "pan=stereo|FL < 1.0*FL + 0.707*FC + 0.707*BL|FR < 1.0*FR + 0.707*FC + 0.707*BR"
  
• Результат: коректний downmix 5.1→stereo з збереженням діалогів
```

#### 🔹 Матриця рішень за кодеком

| Вхідний кодек | Канали | Стратегія | Вихідний варіант(и) |
|--------------|--------|-----------|-------------------|
| `aac` | ≤2 | ✅ Copy | AAC stereo (copy) |
| `aac` | >2 | 🔍 Шукаємо stereo AAC | Surround (copy) + stereo (copy) |
| `ac3`/`eac3` | >2 | 🔄 Конвертуємо + шукаємо stereo | E-AC3 surround + AAC stereo (опціонально) |
| `truehd`/`dts` | будь-які | 🔄 Конвертуємо в E-AC3 | E-AC3 surround + AAC stereo (опціонально) |
| інший | ≤2 | 🔄 Конвертуємо в AAC | AAC stereo |

---

## 🔍 Функція `removeVFQAudio`: фільтрація регіональних варіантів

```go
func removeVFQAudio(variants []AudioVariant) []AudioVariant {
    // 🔹 Перевіряємо наявність французької мови
    hasFrench := false
    filteredVariants := make([]AudioVariant, 0)
    
    for _, variant := range variants {
        // 🔹 Запам'ятовуємо, чи є французька (стандартна або "true")
        hasFrench = hasFrench || variant.Language == input.FrenchLanguage || variant.Language == input.TrueFrench
        
        // 🔹 Фільтруємо Quebec French (VFQ)
        if variant.Language != input.QuebecLanguage {
            filteredVariants = append(filteredVariants, variant)
        }
    }
    
    // 🔹 Ключова логіка: видаляємо VFQ тільки якщо є інша французька
    if hasFrench {
        return filteredVariants  // ✅ VFQ видалено, є інша французька
    } else {
        return variants  // ❌ Залишаємо VFQ, бо це єдина французька
    }
}
```

### 🎯 Навіщо це потрібно?

```
Сценарій: Канадський контент з двома французькими доріжками:
• "fra" = стандартна французька (Франція)
• "fr-CA" = Quebec French (VFQ)

Проблема:
• Для міжнародної аудиторії VFQ може бути незрозумілим
• Але якщо це єдина французька доріжка — видаляти її не можна

Рішення:
1. Якщо є і "fra", і "fr-CA" → залишаємо тільки "fra"
2. Якщо тільки "fr-CA" → залишаємо її (краще ніж нічого)

Це приклад "graceful degradation" для мультимовного контенту.
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Генерація аудіо-варіантів для каналу

```go
// У channel-менеджері — автоматична конфігурація:
func generateAudioConfig(channelID string, probeData []*probe.ProbeData) []converter.AudioVariant {
    // 🔹 Параметри: створювати stereo альтернативи, видаляти VFQ
    variants := suggest.SuggestAudioVariants(probeData, 
        true,   // createAlternateStereo = true для мобільної сумісності
        false,  // removeVFQ = false (CCTV зазвичай не має регіональних варіантів)
    )
    
    // 🔹 Логування для відладки
    log.Infof("Channel %s: generated %d audio variants", channelID, len(variants))
    for _, v := range variants {
        log.Debugf("  - %s: codec=%s, channels=%d, bitrate=%s, language=%s",
            v.Name, v.Codec, v.Type, 
            derefString(v.Bitrate, "default"), 
            v.Language)
    }
    
    return variants
}

func derefString(ptr *string, fallback string) string {
    if ptr != nil { return *ptr }
    return fallback
}
```

### ✅ 2: Валідація згенерованих варіантів

```go
// Перевірити, що варіанти валідні перед передачею у конвертер:
func validateAudioVariants(variants []suggest.AudioVariant) error {
    for i, v := range variants {
        // 🔹 Обов'язкові поля
        if v.MapInput == "" {
            return fmt.Errorf("variant %d: missing MapInput", i)
        }
        if v.Codec == "" {
            return fmt.Errorf("variant %d: missing Codec", i)
        }
        if v.Name == "" {
            return fmt.Errorf("variant %d: missing Name", i)
        }
        
        // 🔹 Валідація бітрейту
        if v.Bitrate != nil {
            if !isValidBitrate(*v.Bitrate) {
                return fmt.Errorf("variant %d: invalid bitrate: %s", i, *v.Bitrate)
            }
        }
        
        // 🔹 Перевірка сумісності Codec + Type
        if v.Type == suggest.SurroundSound && v.Codec == "aac" && !v.ConvertToStereo {
            // AAC без downmix може не підтримуватися деякими плеєрами
            log.Warnf("variant %d: AAC surround without downmix may have limited compatibility", i)
        }
    }
    return nil
}

func isValidBitrate(s string) bool {
    // 🔹 Проста перевірка формату: "128k", "256k", "1m"...
    return strings.HasSuffix(s, "k") || strings.HasSuffix(s, "m")
}
```

### ✅ 3: Моніторинг розподілу аудіо-варіантів

```go
// monitoring.Monitor — метрики для аудіо-варіантів:
type AudioVariantMetrics struct {
    VariantsGenerated *prometheus.CounterVec  // кількість згенерованих варіантів
    CodecDistribution *prometheus.CounterVec  // розподіл за кодеками
    LanguageDistribution *prometheus.CounterVec  // розподіл за мовами
    StereoDownmixCount *prometheus.CounterVec  // кількість downmix варіантів
}

// У процесі генерації:
func monitorAudioVariants(channelID string, variants []suggest.AudioVariant, 
                         metrics *AudioVariantMetrics) {
    
    metrics.VariantsGenerated.WithLabelValues(channelID).Add(float64(len(variants)))
    
    for _, v := range variants {
        metrics.CodecDistribution.WithLabelValues(channelID, v.Codec).Inc()
        metrics.LanguageDistribution.WithLabelValues(channelID, string(v.Language)).Inc()
        
        if v.ConvertToStereo {
            metrics.StereoDownmixCount.WithLabelValues(channelID).Inc()
        }
    }
}
```

### ✅ 4: Кешування результатів прозонування для швидкої генерації

```go
// Щоб не прозвонювати той самий файл багато разів:
type AudioVariantCache struct {
    mu    sync.RWMutex
    cache map[string][]suggest.AudioVariant  // key = fileHash, value = variants
    ttl   time.Duration
}

func (c *AudioVariantCache) GetOrSuggest(probeData []*probe.ProbeData, 
                                        createStereo, removeVFQ bool, 
                                        fileHash string) []suggest.AudioVariant {
    // 🔹 Спробувати отримати з кешу
    c.mu.RLock()
    if variants, ok := c.cache[fileHash]; ok {
        c.mu.RUnlock()
        return variants
    }
    c.mu.RUnlock()
    
    // 🔹 Згенерувати варіанти
    variants := suggest.SuggestAudioVariants(probeData, createStereo, removeVFQ)
    
    // 🔹 Зберегти у кеш
    c.mu.Lock()
    c.cache[fileHash] = variants
    c.mu.Unlock()
    
    return variants
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на генерацію для stereo AAC

```go
func TestSuggestAudioVariants_StereoAAC(t *testing.T) {
    // 🔹 Підготувати тестові дані: stereo AAC потік
    probeData := &probe.ProbeData{
        Streams: []*probe.ProbeStream{
            {
                Index:      1,
                CodecType:  "audio",
                CodecName:  "aac",
                Channels:   2,
                Tags: probe.StreamTags{Language: "eng"},
            },
        },
    }
    
    variants := SuggestAudioVariants([]*probe.ProbeData{probeData}, true, false)
    
    // 🔹 Перевірити результат
    assert.Len(t, variants, 1)
    assert.Equal(t, "copy", variants[0].Codec)  // ✅ Копіюємо, не переенкодуємо
    assert.Equal(t, suggest.StereoSound, variants[0].Type)
    assert.False(t, variants[0].ConvertToStereo)  // ✅ Вже stereo
    assert.Equal(t, input.English, variants[0].Language)
}
```

### 🔹 Тест на surround sound з альтернативою

```go
func TestSuggestAudioVariants_SurroundWithAlternate(t *testing.T) {
    // 🔹 Підготувати дані: AC3 5.1 + stereo AAC
    probeData := &probe.ProbeData{
        Streams: []*probe.ProbeStream{
            {
                Index:      1,
                CodecType:  "audio",
                CodecName:  "ac3",
                Channels:   6,  // 5.1 surround
                Tags: probe.StreamTags{Language: "eng"},
            },
            {
                Index:      2,
                CodecType:  "audio",
                CodecName:  "aac",
                Channels:   2,  // stereo альтернатива
                Tags: probe.StreamTags{Language: "eng"},
            },
        },
    }
    
    variants := SuggestAudioVariants([]*probe.ProbeData{probeData}, true, false)
    
    // 🔹 Очікуємо 2 варіанти: surround (copy) + stereo (copy)
    assert.Len(t, variants, 2)
    
    // 🔹 Перевірити surround варіант
    surround := findVariantByName(variants, "Audio 1&2")
    assert.NotNil(t, surround)
    assert.Equal(t, "copy", surround.Codec)
    assert.Equal(t, suggest.SurroundSound, surround.Type)
    
    // 🔹 Перевірити stereo варіант
    stereo := findVariantByName(variants, "AAC Stereo")
    assert.NotNil(t, stereo)
    assert.Equal(t, "copy", stereo.Codec)  // ✅ Теж copy, бо вже є AAC
    assert.True(t, stereo.ConvertToStereo)  // ✅ Це downmix варіант
}

func findVariantByName(variants []suggest.AudioVariant, namePart string) *suggest.AudioVariant {
    for _, v := range variants {
        if strings.Contains(v.Name, namePart) {
            return &v
        }
    }
    return nil
}
```

### 🔹 Тест на фільтрацію VFQ

```go
func TestRemoveVFQAudio_WithStandardFrench(t *testing.T) {
    variants := []suggest.AudioVariant{
        {Language: input.FrenchLanguage, Name: "French"},      // стандартна
        {Language: input.QuebecLanguage, Name: "Quebec French"}, // VFQ
        {Language: input.English, Name: "English"},
    }
    
    filtered := removeVFQAudio(variants)
    
    // 🔹 VFQ видалено, бо є стандартна французька
    assert.Len(t, filtered, 2)
    assert.Contains(t, filtered, variants[0])  // French залишено
    assert.NotContains(t, filtered, variants[1])  // Quebec French видалено
    assert.Contains(t, filtered, variants[2])  // English залишено
}

func TestRemoveVFQAudio_OnlyVFQ(t *testing.T) {
    variants := []suggest.AudioVariant{
        {Language: input.QuebecLanguage, Name: "Quebec French"},
        {Language: input.English, Name: "English"},
    }
    
    filtered := removeVFQAudio(variants)
    
    // 🔹 VFQ залишено, бо це єдина французька
    assert.Len(t, filtered, 2)  // нічого не видалено
    assert.Contains(t, filtered, variants[0])  // Quebec French залишено
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `matchLanguage` не розпізнає теги | Мова залишається "unknown" | 🔹 Перевірити парсинг тегів у `probe`; додати fallback на `Language: "und"` |
| `ConvertToStereo` ігнорується | Downmix не застосовується | 🔹 Перевірити, що `audioConversionArgs` читає цей прапорець і додає `-ac` та `pan` фільтр |
| Дублювання варіантів | Одна мова/кодек з'являється кілька разів | 🔹 Додати дедуплікацію за `(Language, Codec, Type)` перед поверненням |
| Неправильний `MapInput` | FFmpeg не знаходить потік | 🔹 Перевірити нумерацію: `inputIndex:streamIndex` має співпадати з `-i` аргументами |
| Помилка при `removeVFQ` | Видаляє всі французькі варіанти | 🔹 Перевірити логіку: `hasFrench` має враховувати і `FrenchLanguage`, і `TrueFrench` |

### Приклад дедуплікації варіантів:

```go
func deduplicateVariants(variants []suggest.AudioVariant) []suggest.AudioVariant {
    seen := make(map[string]bool)
    result := make([]suggest.AudioVariant, 0)
    
    for _, v := range variants {
        // 🔹 Ключ: мова + кодек + тип + stereo flag
        key := fmt.Sprintf("%s:%s:%d:%v", 
            v.Language, v.Codec, v.Type, v.ConvertToStereo)
        
        if !seen[key] {
            seen[key] = true
            result = append(result, v)
        } else {
            log.Debugf("Skipping duplicate variant: %s", key)
        }
    }
    return result
}

// Використання у SuggestAudioVariants:
// ... після генерації ...
return deduplicateVariants(variants)
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базова генерація аудіо-варіантів:
func generateAudioConfig(probeData []*probe.ProbeData) []suggest.AudioVariant {
    return suggest.SuggestAudioVariants(probeData, 
        true,   // створювати stereo альтернативи
        false,  // не видаляти VFQ для CCTV
    )
}

// 2: Фільтрація за підтримуваними кодеками:
func filterSupportedCodecs(variants []suggest.AudioVariant) []suggest.AudioVariant {
    supported := map[string]bool{
        "aac": true, "eac3": true, "copy": true,
    }
    result := make([]suggest.AudioVariant, 0)
    for _, v := range variants {
        if supported[v.Codec] {
            result = append(result, v)
        } else {
            log.Warnf("Skipping unsupported codec: %s", v.Codec)
        }
    }
    return result
}

// 3: Сортування варіантів для пріоритету у плеєрі:
func sortVariantsByPriority(variants []suggest.AudioVariant) []suggest.AudioVariant {
    // 🔹 Пріоритет: AAC stereo > E-AC3 surround > інші
    sort.Slice(variants, func(i, j int) bool {
        a, b := variants[i], variants[j]
        
        // 🔹 Спочатку stereo AAC
        if a.Codec == "aac" && !a.ConvertToStereo {
            return true
        }
        if b.Codec == "aac" && !b.ConvertToStereo {
            return false
        }
        
        // 🔹 Потім surround з downmix альтернативою
        if a.ConvertToStereo {
            return true
        }
        if b.ConvertToStereo {
            return false
        }
        
        return false
    })
    return variants
}

// 4: Логування для відладки:
func logAudioVariants(channelID string, variants []suggest.AudioVariant) {
    log.Infof("Channel %s: %d audio variants generated", channelID, len(variants))
    for i, v := range variants {
        log.Debugf("  [%d] %s: codec=%s, type=%d, bitrate=%s, lang=%s, downmix=%v",
            i, v.Name, v.Codec, v.Type,
            derefString(v.Bitrate, "default"),
            v.Language, v.ConvertToStereo)
    }
}

// 5: Конвертація у converter.AudioVariant:
func toConverterVariants(suggestVariants []suggest.AudioVariant) []converter.AudioVariant {
    result := make([]converter.AudioVariant, len(suggestVariants))
    for i, v := range suggestVariants {
        result[i] = converter.AudioVariant{
            MapInput:        v.MapInput,
            Codec:           v.Codec,
            Bitrate:         v.Bitrate,
            ConvertToStereo: v.ConvertToStereo,
            // ... інші поля ...
        }
    }
    return result
}
```

---

## 📊 Матриця рішень для різних сценаріїв аудіо

```
Вхідний потік          | Стратегія                      | Вихідні варіанти
───────────────────────┼────────────────────────────────┼─────────────────────────
AAC stereo             | ✅ Copy без змін               | 1× AAC stereo (copy)
AAC 5.1 + stereo AAC   | 🔍 Copy обидва                 | 1× AAC surround (copy) + 1× AAC stereo (copy)
AC3 5.1 без stereo     | 🔄 E-AC3 surround + AAC stereo | 1× E-AC3 surround + 1× AAC stereo (опціонально)
DTS/TrueHD             | 🔄 Конвертація в E-AC3         | 1× E-AC3 surround + 1× AAC stereo (опціонально)
MP3 stereo             | 🔄 Конвертація в AAC           | 1× AAC stereo (256k)
Mono аудіо             | ⚠️ Потрібна обробка (TODO)     | (зараз ігнорується)
```

---

## 📚 Корисні посилання

- [RFC 5646: Language Tags](https://datatracker.ietf.org/doc/html/rfc5646)
- [HLS Audio Groups specification](https://developer.apple.com/documentation/http_live_streaming/about_the_radio_stream_format)
- [FFmpeg audio encoding guide](https://trac.ffmpeg.org/wiki/Encode/AAC)
- [E-AC3 compatibility with HLS](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)

> 💡 **Ключова ідея**: Цей `suggest` пакет — це "радник" вашого пайплайну. Він:
> - 🎯 Автоматично визначає оптимальні аудіо-варіанти на основі вхідних метаданих
> - 🔧 Балансує між якістю ("copy") та сумісністю (конвертація в AAC/E-AC3)
> - 🌍 Підтримує багатомовність через мовні теги та групування
> - 🛡️ Граційно деградує при відсутності ідеальних варіантів

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку нових аудіо-кодеків (Opus, FLAC) або мовних тегів
- 🧪 Написати property-based тести для генерації варіантів з випадковими вхідними даними
- 📈 Додати Prometheus-метрики для моніторингу розподілу аудіо-варіантів по каналах та мовах

🛠️