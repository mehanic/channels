# Глибоке роз'яснення: `suggest.VideoVariant` — автоматична генерація відео-варіантів для HLS

Цей файл містить **логіку інтелектуального підбору відео-варіантів** на основі метаданих вхідних потоків. Він аналізує кодеки, роздільну здатність, бітрейт та генерує оптимальну конфігурацію для адаптивного HLS-стрімінгу.

---

## 🎯 Навіщо цей код потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ suggest.VideoVariant у контексті:      │
│                                         │
│ 🔹 Автоматизація конфігурації:         │
│   • Не потрібно вручну вказувати       │
│     параметри для кожного вхідного     │
│     потоку                              │
│   • Адаптація до різних кодеків/якостей│
│                                         │
│ 🔹 Адаптивний бітрейт (ABR):           │
│   • Генерація кількох варіантів якості │
│   • Автоматичний вибір плеєром за      │
│     мережевими умовами                 │
│   • Економія трафіку для клієнтів      │
│                                         │
│ 🔹 Сумісність з пристроями:            │
│   • H.264 baseline/main/high профілі   │
│   • HEVC/H.265 для 4K та економії      │
│   • hvc1 тег для Apple/Safari          │
└─────────────────────────────────────────┘
```

---

## 🔧 Типи даних: структура відео-варіантів

### `VideoVariant`: конфігурація одного відео-варіанту

```go
type VideoVariant struct {
    // 🔹 Вхідні параметри
    MapInput   string  // 🎯 FFmpeg map: "0:0" = вхід 0, потік 0
    Codec      string  // 🎯 "libx264", "copy", "libx265"...
    
    // 🔹 Параметри якості енкодингу
    CRF        *int    // 🎯 Constant Rate Factor (18-28: менше = краще)
    Profile    *string // 🎯 H.264 профіль: "baseline", "main", "high"
    Level      *string // 🎯 H.264 рівень: "3.0", "4.1", "5.1"...
    Bitrate    *string // 🎯 Цільовий бітрейт: "2000k", "5000k"...
    
    // 🔹 Спеціальні прапорці
    AddHVC1Tag bool    // 🎯 Додати -tag:v hvc1 для сумісності з Apple
    
    // 🔹 Групи медіа для HLS плейлиста
    AudioGroup    *string // 🎯 Група аудіо: "audio"
    SubtitleGroup *string // 🎯 Група субтитрів: "subtitles"
    
    // 🔹 HLS playlist метадані
    Resolution       string // 🎯 Роздільна здатність: "1280x720"
    Bandwidth        string // 🎯 Бітрейт для ABR: "2000000" (біт/сек)
    ResolutionHeight *int   // 🎯 Висота для масштабування: -filter:v scale=...:HEIGHT
}
```

### 🎯 Ключові поля для HLS сумісності

| Поле | Призначення | Приклад значення |
|------|-------------|-----------------|
| `MapInput` | Вказує FFmpeg який потік обробляти | `"0:0"` = перший вхід, перший потік |
| `Codec` | Кодек для енкодингу | `"copy"` (без переенкодингу), `"libx264"`, `"libx265"` |
| `CRF` | Якість для x264/x265 | `18` (висока), `23` (баланс), `28` (економія) |
| `Profile`/`Level` | Сумісність з пристроями | `"main"`/`"4.0"` для більшості плеєрів |
| `Resolution` | Для #EXT-X-STREAM-INF | `"1280x720"` |
| `Bandwidth` | Для адаптивного вибору плеєром | `"2000000"` (2 Mbps) |
| `AddHVC1Tag` | Сумісність HEVC з Apple | `true` для Safari/iOS |

---

## 🔍 Функція `masterVideo`: пошук основного відеопотоку

```go
func masterVideo(fileStreams []*probe.ProbeStream) (streamIndex int, err error) {
    for _, stream := range fileStreams {
        if stream.CodecType == "video" {
            streamIndex := stream.Index  // ⚠️ BUG: тіньова змінна!
            return streamIndex, nil
        }
    }
    err = errors.New("could not find a video stream to use as master")
    return -1, err
}
```

### ⚠️ BUG: тіньова змінна (`shadowing`)

```go
// ❌ Неправильно:
streamIndex := stream.Index  // 🔹 Оголошує НОВУ локальну змінну!

// ✅ Правильно:
streamIndex = stream.Index   // 🔹 Присвоює значення зовнішній змінній

// Наслідок бага:
• Функція завжди повертає 0 (початкове значення), навіть якщо відео на індексі 2
• Це призведе до неправильного `-map` у FFmpeg → енкодинг не того потоку

// Фікс:
func masterVideo(fileStreams []*probe.ProbeStream) (int, error) {
    for _, stream := range fileStreams {
        if stream.CodecType == "video" {
            return stream.Index, nil  // 🔹 Просто повернути, без проміжної змінної
        }
    }
    return -1, errors.New("could not find a video stream to use as master")
}
```

---

## 🔍 Функція `SuggestVideoVariants`: основна логіка генерації

```go
func SuggestVideoVariants(probeDataInputs []*probe.ProbeData) []VideoVariant {
    for inputIndex, probeData := range probeDataInputs {
        // 🔹 1. Знайти основний відеопотік
        masterVideoIndex, err := masterVideo(probeData.Streams)
        if err != nil { continue }  // 🔹 Пропустити входи без відео
        
        videoStream := probeData.Streams[masterVideoIndex]
        
        // 🔹 2. Оцінити бітрейт (FIXME: обробка unknown)
        bandwidth := 700000  // 🔹 Дефолт: 700 kbps
        if videoStream.BitRate > 0 {
            bandwidth = videoStream.BitRate  // 🔹 Використати реальний бітрейт
        }
        
        // 🔹 3. Генерація варіантів за кодеком
        switch videoStream.CodecName {
        
        // ── CASE 1: H.264 (найпоширеніший) ──
        case "h264":
            // 🔹 Опціонально: додати низькоякісний варіант для слабких мереж
            if videoStream.Height > 540 {
                // ... закоментований код для low-quality variant ...
            }
            
            // 🔹 Основний варіант: просто копіювати (без переенкодингу)
            variants = append(variants, VideoVariant{
                MapInput:   strconv.Itoa(inputIndex) + ":" + strconv.Itoa(masterVideoIndex),
                Codec:      "copy",  // ✅ Економія ресурсів!
                Resolution: fmt.Sprintf("%dx%d", videoStream.Width, videoStream.Height),
                Bandwidth:  strconv.Itoa(bandwidth),
            })
        
        // ── CASE 2: HEVC/H.265 (висока ефективність) ──
        case "h265", "hevc":
            log.Println("High efficiency stream detected. Copying...")
            
            // 🔹 Варіант 1: Копіювати HEVC (для сучасних пристроїв)
            variants = append(variants, VideoVariant{
                MapInput:   strconv.Itoa(inputIndex) + ":" + strconv.Itoa(masterVideoIndex),
                Codec:      "copy",
                Resolution: fmt.Sprintf("%dx%d", videoStream.Width, videoStream.Height),
                Bandwidth:  strconv.Itoa(bandwidth * 2),  // 🔹 ×2 для консервативної оцінки
                AddHVC1Tag: true,  // 🔹 Критично для Apple/Safari!
            })
            
            // 🔹 Варіант 2: Конвертувати в H.264 для широкої сумісності
            // ... закоментований код для x264 fallback ...
        
        // ── CASE 3: Інші кодеки (непідтримувані) ──
        default:
            // 🔹 Конвертувати в H.264 з обчисленням роздільної здатності
            h264Width, h264Height := computeNewRatio(videoStream, 1080)  // 🔹 Макс. висота 1080p
            crf := 18  // 🔹 Висока якість для конвертації
            
            variants = append(variants, VideoVariant{
                MapInput:         strconv.Itoa(inputIndex) + ":" + strconv.Itoa(masterVideoIndex),
                Codec:            "libx264",
                CRF:              &crf,
                ResolutionHeight: &h264Height,  // 🔹 Для -filter:v scale=...:HEIGHT
                Resolution:       fmt.Sprintf("%dx%d", h264Width, h264Height),
                Bandwidth:        strconv.Itoa(bandwidth),
            })
        }
    }
    return variants
}
```

### 🎯 Ключові рішення в логіці

#### 🔹 Стратегія "copy" vs переенкодинг

```
Коли використовується "copy":
✅ Вхідний кодек = H.264 або HEVC (підтримується у HLS)
✅ Немає потреби змінювати роздільну здатність/бітрейт
✅ Економія CPU та збереження якості

Коли використовується переенкодинг:
❌ Непідтримуваний кодек (MPEG-2, VP9, AV1...)
❌ Потрібне масштабування (напр., 4K → 1080p)
❌ Потрібна зміна профілю/рівня для сумісності

Переваги "copy":
• ⚡ Швидше: немає витрат на енкодинг
• 🎬 Краща якість: немає поколінь стиснення
• 💰 Економія ресурсів сервера
```

#### 🔹 Обробка HEVC: подвійний варіант

```
Проблема:
• HEVC/H.265 має кращу компресію (~50% економія бітрейту)
• Але не всі плеєри підтримують HEVC (особливо старі браузери)

Рішення:
1. Варіант 1: Копіювати HEVC + AddHVC1Tag=true
   • Для сучасних пристроїв (iOS 11+, Safari, Android 5+)
   • hvc1 тег вирішує проблему сумісності з Apple

2. Варіант 2: Конвертувати в H.264 (закоментовано)
   • Для широкої сумісності
   • Більші витрати на енкодинг

Рекомендація:
• Розкоментувати варіант 2 для production-систем
• Використовувати ABR: плеєр сам вибере підтримуваний варіант
```

#### 🔹 Оцінка бітрейту: FIXME для unknown

```go
bandwidth := 700000  // 🔹 Дефолт: 700 kbps
if videoStream.BitRate > 0 {
    bandwidth = videoStream.BitRate
}
```

**Проблема:**
```
• ffprobe може повернути 0 для бітрейту (напр., для VBR або потокових джерел)
• Дефолт 700 kbps може бути занадто низьким для HD контенту
• Неправильний BANDWIDTH → плеєр вибере неоптимальний варіант

Рішення:
• Оцінити бітрейт за розміром файлу / тривалістю
• Використати середній бітрейт для подібного контенту
• Додати метрику для моніторингу "unknown bandwidth" випадків
```

---

## 🔍 Функція `computeNewRatio`: збереження aspect ratio при масштабуванні

```go
func computeNewRatio(videoStream *probe.ProbeStream, maximumHeight int) (int, int) {
    h264Height := videoStream.Height
    
    // 🔹 Якщо висота вже в межах → не масштабувати
    if h264Height > maximumHeight {
        h264Height = maximumHeight  // 🔹 Обмежити висоту
        
        // 🔹 Розрахувати aspect ratio
        ratio := 1.777778  // 🔹 Дефолт: 16:9
        ratioStrings := strings.Split(videoStream.DisplayAspectRatio, ":")
        
        if len(ratioStrings) == 2 {
            a, err1 := strconv.ParseFloat(ratioStrings[0], 64)
            b, err2 := strconv.ParseFloat(ratioStrings[1], 64)
            if err1 == nil && err2 == nil {
                ratio = a / b  // 🔹 Використати реальний aspect ratio
            } else {
                log.Println("WARNING: Cannot parse aspect ratio... Defaulting to 16/9")
            }
        } else {
            log.Println("WARNING: Unexpected aspect ratio format... Defaulting to 16/9")
        }
        
        // 🔹 Розрахувати ширину: width = height × ratio
        // 🔹 trunc(oh*a/2)*2 гарантує парну ширину (вимога кодеків)
        return int(float64(h264Height) * ratio), h264Height
    }
    
    // 🔹 Не потрібно масштабувати → повернути оригінал
    return videoStream.Width, videoStream.Height
}
```

### 🎯 Ключовий момент: гарантія парної ширини

```
Чому парна ширина важлива:
• Кодеки H.264/H.265 використовують макроблоки 2×2 пікселі
• Непарна ширина → помилка енкодингу або автоматичне обрізання

Як гарантується парність:
• У converter/videoConversionArgs():
  fmt.Sprintf("scale=trunc(oh*a/2)*2:%d", *variant.ResolutionHeight)
  
• Формула: trunc(висота × aspect_ratio / 2) × 2
  • Ділення на 2 → округлення вниз до цілого
  • Множення на 2 → гарантія парного результату

Приклад:
• Вхід: 1920×1080 (16:9), target height = 720
• Теоретична ширина: 720 × 1.777... = 1280
• Перевірка парності: 1280 вже парне → залишається 1280
• Вихід: 1280×720 ✅
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Генерація відео-варіантів для каналу

```go
// У channel-менеджері — автоматична конфігурація:
func generateVideoConfig(channelID string, probeData []*probe.ProbeData) []suggest.VideoVariant {
    variants := suggest.SuggestVideoVariants(probeData)
    
    // 🔹 Додати групи аудіо/субтитрів для HLS плейлиста
    audioGroup := "audio"
    subtitleGroup := "subtitles"
    
    for i := range variants {
        variants[i].AudioGroup = &audioGroup
        variants[i].SubtitleGroup = &subtitleGroup
    }
    
    // 🔹 Логування для відладки
    log.Infof("Channel %s: generated %d video variants", channelID, len(variants))
    for _, v := range variants {
        log.Debugf("  - %s: codec=%s, resolution=%s, bandwidth=%s",
            v.Resolution, v.Codec, v.Resolution, v.Bandwidth)
    }
    
    return variants
}
```

### ✅ 2: Валідація згенерованих варіантів

```go
// Перевірити, що варіанти валідні перед передачею у конвертер:
func validateVideoVariants(variants []suggest.VideoVariant) error {
    for i, v := range variants {
        // 🔹 Обов'язкові поля
        if v.MapInput == "" {
            return fmt.Errorf("variant %d: missing MapInput", i)
        }
        if v.Codec == "" {
            return fmt.Errorf("variant %d: missing Codec", i)
        }
        if v.Resolution == "" {
            return fmt.Errorf("variant %d: missing Resolution", i)
        }
        if v.Bandwidth == "" {
            return fmt.Errorf("variant %d: missing Bandwidth", i)
        }
        
        // 🔹 Валідація бітрейту (число)
        if _, err := strconv.Atoi(v.Bandwidth); err != nil {
            return fmt.Errorf("variant %d: invalid bandwidth format: %s", i, v.Bandwidth)
        }
        
        // 🔹 Перевірка сумісності Profile/Level
        if v.Profile != nil && v.Level == nil {
            return fmt.Errorf("variant %d: Level required when Profile is set", i)
        }
        
        // 🔹 Перевірка CRF діапазону (для x264/x265)
        if v.CRF != nil && (v.Codec == "libx264" || v.Codec == "libx265") {
            if *v.CRF < 0 || *v.CRF > 51 {
                return fmt.Errorf("variant %d: CRF out of range [0-51]: %d", i, *v.CRF)
            }
        }
    }
    return nil
}
```

### ✅ 3: Моніторинг розподілу відео-варіантів

```go
// monitoring.Monitor — метрики для відео-варіантів:
type VideoVariantMetrics struct {
    VariantsGenerated *prometheus.CounterVec  // кількість згенерованих варіантів
    CodecDistribution *prometheus.CounterVec  // розподіл за кодеками
    ResolutionDistribution *prometheus.HistogramVec  // розподіл роздільних здатностей
    BandwidthDistribution *prometheus.HistogramVec  // розподіл бітрейтів
    CopyVsEncodeRatio *prometheus.GaugeVec  // співвідношення copy/encode
}

// У процесі генерації:
func monitorVideoVariants(channelID string, variants []suggest.VideoVariant, 
                         metrics *VideoVariantMetrics) {
    
    metrics.VariantsGenerated.WithLabelValues(channelID).Add(float64(len(variants)))
    
    copyCount := 0
    for _, v := range variants {
        metrics.CodecDistribution.WithLabelValues(channelID, v.Codec).Inc()
        
        // 🔹 Роздільна здатність
        if parts := strings.Split(v.Resolution, "x"); len(parts) == 2 {
            if height, err := strconv.Atoi(parts[1]); err == nil {
                metrics.ResolutionDistribution.WithLabelValues(channelID).Observe(float64(height))
            }
        }
        
        // 🔹 Бітрейт
        if bw, err := strconv.Atoi(v.Bandwidth); err == nil {
            metrics.BandwidthDistribution.WithLabelValues(channelID).Observe(float64(bw))
        }
        
        if v.Codec == "copy" {
            copyCount++
        }
    }
    
    // 🔹 Співвідношення copy/encode
    if len(variants) > 0 {
        ratio := float64(copyCount) / float64(len(variants))
        metrics.CopyVsEncodeRatio.WithLabelValues(channelID).Set(ratio)
    }
}
```

### ✅ 4: Розкоментування додаткових варіантів для ABR

```go
// 🔹 Розкоментувати low-quality варіант для H.264:
if videoStream.Height > 540 {
    // Додати низькоякісний варіант для слабких мереж
    h264Width, h264Height := computeNewRatio(videoStream, 420)  // 🔹 Макс. 420p
    crf := 28  // 🔹 Нижча якість для економії бітрейту
    
    variants = append(variants, VideoVariant{
        MapInput:         strconv.Itoa(inputIndex) + ":" + strconv.Itoa(masterVideoIndex),
        Codec:            "libx264",
        CRF:              &crf,
        ResolutionHeight: &h264Height,
        Resolution:       fmt.Sprintf("%dx%d", h264Width, h264Height),
        Bandwidth:        strconv.Itoa(bandwidth / 10),  // 🔹 ~10% від оригіналу
    })
}

// 🔹 Розкоментувати H.264 fallback для HEVC:
if videoStream.CodecName == "h265" || videoStream.CodecName == "hevc" {
    // ... HEVC copy variant ...
    
    // Додати H.264 варіант для широкої сумісності
    h264Width, h264Height := computeNewRatio(videoStream, 360)  // 🔹 360p fallback
    crf := 18  // 🔹 Висока якість для конвертації
    
    variants = append(variants, VideoVariant{
        MapInput:         strconv.Itoa(inputIndex) + ":" + strconv.Itoa(masterVideoIndex),
        Codec:            "libx264",
        CRF:              &crf,
        ResolutionHeight: &h264Height,
        Resolution:       fmt.Sprintf("%dx%d", h264Width, h264Height),
        Bandwidth:        strconv.Itoa(730000),  // 🔹 Фіксований бітрейт для 360p
    })
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на генерацію для H.264 входу

```go
func TestSuggestVideoVariants_H264(t *testing.T) {
    // 🔹 Підготувати тестові дані: H.264 1080p
    probeData := &probe.ProbeData{
        Streams: []*probe.ProbeStream{
            {
                Index:      0,
                CodecType:  "video",
                CodecName:  "h264",
                Width:      1920,
                Height:     1080,
                BitRate:    5000000,  // 5 Mbps
            },
        },
    }
    
    variants := SuggestVideoVariants([]*probe.ProbeData{probeData})
    
    // 🔹 Перевірити результат
    assert.Len(t, variants, 1)  // ✅ Тільки один варіант (copy)
    
    v := variants[0]
    assert.Equal(t, "copy", v.Codec)  // ✅ Копіюємо, не переенкодуємо
    assert.Equal(t, "1920x1080", v.Resolution)
    assert.Equal(t, "5000000", v.Bandwidth)  // ✅ Використано реальний бітрейт
    assert.Equal(t, "0:0", v.MapInput)  // ✅ Перший вхід, перший потік
}
```

### 🔹 Тест на HEVC з AddHVC1Tag

```go
func TestSuggestVideoVariants_HEVC(t *testing.T) {
    probeData := &probe.ProbeData{
        Streams: []*probe.ProbeStream{
            {
                Index:      0,
                CodecType:  "video",
                CodecName:  "hevc",
                Width:      3840,
                Height:     2160,  // 4K
                BitRate:    15000000,  // 15 Mbps
            },
        },
    }
    
    variants := SuggestVideoVariants([]*probe.ProbeData{probeData})
    
    // 🔹 Очікуємо 1 варіант (HEVC copy)
    assert.Len(t, variants, 1)
    
    v := variants[0]
    assert.Equal(t, "copy", v.Codec)
    assert.Equal(t, "3840x2160", v.Resolution)
    assert.Equal(t, "30000000", v.Bandwidth)  // ✅ ×2 для консервативної оцінки
    assert.True(t, v.AddHVC1Tag)  // ✅ Критично для Apple!
}
```

### 🔹 Тест на конвертацію невідомого кодека

```go
func TestSuggestVideoVariants_UnknownCodec(t *testing.T) {
    probeData := &probe.ProbeData{
        Streams: []*probe.ProbeStream{
            {
                Index:      0,
                CodecType:  "video",
                CodecName:  "mpeg2video",  // ❌ Непідтримуваний
                Width:      1920,
                Height:     1080,
                DisplayAspectRatio: "16:9",
            },
        },
    }
    
    variants := SuggestVideoVariants([]*probe.ProbeData{probeData})
    
    // 🔹 Очікуємо 1 варіант: конвертація в H.264
    assert.Len(t, variants, 1)
    
    v := variants[0]
    assert.Equal(t, "libx264", v.Codec)  // ✅ Конвертуємо в H.264
    assert.NotNil(t, v.CRF)
    assert.Equal(t, 18, *v.CRF)  // ✅ Висока якість
    assert.NotNil(t, v.ResolutionHeight)
    assert.Equal(t, 1080, *v.ResolutionHeight)  // ✅ Не масштабуємо, бо вже 1080p
}
```

### 🔹 Тест на computeNewRatio з aspect ratio

```go
func TestComputeNewRatio(t *testing.T) {
    // 🔹 Вхід: 1920×1080 (16:9), максимум 720p
    videoStream := &probe.ProbeStream{
        Width:  1920,
        Height: 1080,
        DisplayAspectRatio: "16:9",
    }
    
    width, height := computeNewRatio(videoStream, 720)
    
    // 🔹 Очікуємо: 1280×720 (збереження 16:9)
    assert.Equal(t, 1280, width)
    assert.Equal(t, 720, height)
    
    // 🔹 Тест на нестандартний aspect ratio: 4:3
    videoStream.DisplayAspectRatio = "4:3"
    width, height = computeNewRatio(videoStream, 720)
    
    // 🔹 Очікуємо: 960×720 (720 × 4/3 = 960)
    assert.Equal(t, 960, width)
    assert.Equal(t, 720, height)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `masterVideo` тіньова змінна | Завжди повертає індекс 0, енкодинг не того потоку | 🔹 Виправити: `return stream.Index, nil` без проміжної змінної |
| `bandwidth = 700000` дефолт | Неправильний ABR вибір плеєром | 🔹 Оцінити бітрейт за розмір/тривалість; додати метрику для unknown |
| Відсутній `hvc1` тег для HEVC | Помилки відтворення на Safari/iOS | 🔹 Завжди встановлювати `AddHVC1Tag: true` для HEVC варіантів |
| Непарна ширина після масштабування | Помилка енкодингу "width not divisible by 2" | 🔹 Використовувати `trunc(oh*a/2)*2` у фільтрі масштабування |
| `Profile` без `Level` | Помилка валідації, несумісність з пристроями | 🔹 Додати валідацію: якщо `Profile != nil`, то `Level` обов'язковий |

### Приклад оцінки бітрейту за розміром файлу:

```go
func estimateBitrate(format *probe.ProbeFormat) int {
    if format == nil || format.DurationSeconds == "" || format.Size == "" {
        return 700000  // fallback
    }
    
    // 🔹 Парсити розмір (байти) та тривалість (секунди)
    size, err1 := strconv.ParseInt(format.Size, 10, 64)
    duration, err2 := strconv.ParseFloat(format.DurationSeconds, 64)
    
    if err1 != nil || err2 != nil || duration <= 0 {
        return 700000
    }
    
    // 🔹 Розрахувати бітрейт: біти/секунду
    bitrate := int(float64(size) * 8 / duration)
    
    // 🔹 Обмежити розумними межами
    if bitrate < 100000 { return 100000 }   // мінімум 100 kbps
    if bitrate > 50000000 { return 50000000 } // максимум 50 Mbps
    
    return bitrate
}

// Використання у SuggestVideoVariants:
if videoStream.BitRate <= 0 && probeData.Format != nil {
    bandwidth = estimateBitrate(probeData.Format)
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базова генерація відео-варіантів:
func generateVideoConfig(probeData []*probe.ProbeData) []suggest.VideoVariant {
    return suggest.SuggestVideoVariants(probeData)
}

// 2: Додавання груп для HLS плейлиста:
func withHLSGroups(variants []suggest.VideoVariant, audioGroup, subtitleGroup string) []suggest.VideoVariant {
    for i := range variants {
        variants[i].AudioGroup = &audioGroup
        variants[i].SubtitleGroup = &subtitleGroup
    }
    return variants
}

// 3: Фільтрація за підтримуваними кодеками:
func filterSupportedCodecs(variants []suggest.VideoVariant) []suggest.VideoVariant {
    supported := map[string]bool{
        "copy": true, "libx264": true, "libx265": true,
    }
    result := make([]suggest.VideoVariant, 0)
    for _, v := range variants {
        if supported[v.Codec] {
            result = append(result, v)
        } else {
            log.Warnf("Skipping unsupported codec: %s", v.Codec)
        }
    }
    return result
}

// 4: Сортування варіантів для пріоритету у плеєрі:
func sortVariantsByPriority(variants []suggest.VideoVariant) []suggest.VideoVariant {
    sort.Slice(variants, func(i, j int) bool {
        a, b := variants[i], variants[j]
        
        // 🔹 Спочатку copy (найвища якість, найменші витрати)
        if a.Codec == "copy" && b.Codec != "copy" {
            return true
        }
        if b.Codec == "copy" && a.Codec != "copy" {
            return false
        }
        
        // 🔹 Потім за роздільною здатністю (вища перша)
        aParts := strings.Split(a.Resolution, "x")
        bParts := strings.Split(b.Resolution, "x")
        if len(aParts) == 2 && len(bParts) == 2 {
            aHeight, _ := strconv.Atoi(aParts[1])
            bHeight, _ := strconv.Atoi(bParts[1])
            return aHeight > bHeight
        }
        
        return false
    })
    return variants
}

// 5: Логування для відладки:
func logVideoVariants(channelID string, variants []suggest.VideoVariant) {
    log.Infof("Channel %s: %d video variants generated", channelID, len(variants))
    for i, v := range variants {
        log.Debugf("  [%d] %s: codec=%s, crf=%v, bandwidth=%s",
            i, v.Resolution, v.Codec, v.CRF, v.Bandwidth)
    }
}

// 6: Конвертація у converter-сумісний формат:
func toConverterVariants(suggestVariants []suggest.VideoVariant) []converter.VideoVariant {
    result := make([]converter.VideoVariant, len(suggestVariants))
    for i, v := range suggestVariants {
        result[i] = converter.VideoVariant{
            MapInput:   v.MapInput,
            Codec:      v.Codec,
            CRF:        v.CRF,
            Profile:    v.Profile,
            Level:      v.Level,
            Bitrate:    v.Bitrate,
            AddHVC1Tag: v.AddHVC1Tag,
            // ... інші поля ...
        }
    }
    return result
}
```

---

## 📊 Матриця рішень для різних сценаріїв відео

```
Вхідний кодек          | Стратегія                      | Вихідні варіанти
───────────────────────┼────────────────────────────────┼─────────────────────────
H.264 (будь-яка якість)| ✅ Copy без змін               | 1× H.264 (copy)
HEVC/H.265             | 🔍 Copy + hvc1 тег            | 1× HEVC (copy) + [опціонально 1× H.264 fallback]
MPEG-2, VP9, AV1...    | 🔄 Конвертація в H.264        | 1× H.264 (CRF 18, масштабування до 1080p)
Невідомий/відсутній    | ⚠️ Fallback на H.264 720p     | 1× H.264 (CRF 23, дефолт бітрейт)
```

---

## 📚 Корисні посилання

- [HLS Authoring Specification](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [x264/x265 CRF guide](https://slhck.info/video/2017/02/24/crf-guide.html)
- [FFmpeg scaling filter](https://ffmpeg.org/ffmpeg-filters.html#scale)
- [HEVC compatibility with HLS](https://developer.apple.com/documentation/http_live_streaming/about_apple_s_http_live_streaming_tools)

> 💡 **Ключова ідея**: Цей `suggest.VideoVariant` — це "радник" для відео у вашому пайплайні. Він:
> - 🎯 Автоматично визначає оптимальні варіанти на основі вхідних метаданих
> - 🔧 Балансує між якістю ("copy") та сумісністю (конвертація в H.264)
> - ⚡ Економить ресурси через пріоритет "copy" коли можливо
> - 📱 Забезпечує сумісність з широким спектром пристроїв через Profile/Level/hvc1

Якщо потрібно — можу допомогти:
- 🔄 Розкоментувати та налаштувати додаткові варіанти для повного ABR підтримки
- 🧪 Написати property-based тести для генерації варіантів з випадковими вхідними даними
- 📈 Додати Prometheus-метрики для моніторингу розподілу відео-варіантів по каналах та якостях

🛠️