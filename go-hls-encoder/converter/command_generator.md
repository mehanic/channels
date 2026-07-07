# Глибоке роз'яснення: `converter` пакет — генерація аргументів FFmpeg для HLS-енкодингу

Цей файл містить **логіку побудови командного рядка FFmpeg** для конвертації відео/аудіо у HLS-сумісні варіанти. Це "міст" між вашим пайплайном та FFmpeg, що перетворює конфігурацію на конкретні параметри енкодингу.

---

## 🎯 Навіщо цей код потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ converter у контексті HLS-стрімінгу:   │
│                                         │
│ 🔹 Адаптивний бітрейт (ABR):            │
│   • Генерація кількох варіантів якості  │
│   • Автоматичний вибір плеєром         │
│   • Економія трафіку для клієнтів      │
│                                         │
│ 🔹 Сумісність з пристроями:             │
│   • H.264 baseline/main/high профілі   │
│   • HEVC/H.265 для 4K та економії      │
│   • AAC аудіо з підтримкою різних бітрейтів│
│                                         │
│ 🔹 Оптимізація для CCTV:                │
│   • Фіксований GOP (-g 60) для швидкого│
│     seek у записі                      │
│   • Конвертація 5.1 → stereo для       │
│     сумісності з плеєрами              │
│   • Асинхронна обробка аудіо для      │
│     запобігання десинхронізації        │
└─────────────────────────────────────────┘
```

---

## 🔧 Функція `videoConversionArgs`: побудова відео-параметрів

### 📦 Вхідні дані: `suggest.VideoVariant`

```go
// Гіпотетична структура (з suggest пакету):
type VideoVariant struct {
    MapInput          string   // 🎯 FFmpeg map: "0:v:0", "0:v:1"...
    Codec             string   // 🎯 Кодек: "libx264", "libx265", "copy"...
    AddHVC1Tag        bool     // 🎯 Додати -tag:v hvc1 для сумісності з Apple
    ResolutionHeight  *int     // 🎯 Висота вихідного відео (nil = без зміни)
    Bitrate           *string  // 🎯 Цільовий бітрейт: "2000k", "5000k"...
    CRF               *int     // 🎯 Constant Rate Factor (якість для x264/x265)
    Profile           *string  // 🎯 H.264 профіль: "baseline", "main", "high"
    Level             *string  // 🎯 H.264 рівень: "3.0", "4.1", "5.1"...
}
```

### 🔍 Покроковий розбір генерації аргументів

```go
func videoConversionArgs(variants []suggest.VideoVariant) []string {
    for outputIndex, variant := range variants {
        indexS := strconv.Itoa(outputIndex)  // "0", "1", "2"...
        
        // 🔹 1. Базові параметри: map + codec + GOP
        args = append(args, 
            "-map", variant.MapInput,        // Який вхідний потік використовувати
            "-c:v:"+indexS, variant.Codec,   // Кодек для цього виходу
            "-g", "60")                      // 🔹 Ключовий момент: фіксований GOP
        
        // 🔹 2. Специфічні налаштування для H.264
        if variant.Codec == "libx264" {
            args = append(args,
                "-bsf:v:"+indexS, "h264_mp4toannexb",  // Конвертація MP4 → Annex B для HLS
                "-pix_fmt", "yuv420p")                  // Сумісний формат пікселів
        }
        
        // 🔹 3. Tag для HEVC/H.265 (сумісність з Apple/Safari)
        if variant.AddHVC1Tag {
            args = append(args, "-tag:v:"+indexS, "hvc1")
        }
        
        // 🔹 4. Масштабування (якщо вказано висоту)
        if variant.ResolutionHeight != nil {
            // Формула: ширина = trunc(висота × aspect_ratio / 2) × 2
            // 🔹 "trunc(.../2)*2" гарантує парну ширину (вимога багатьох кодеків)
            args = append(args, "-filter:v:"+indexS,
                fmt.Sprintf("scale=trunc(oh*a/2)*2:%d", *variant.ResolutionHeight))
        }
        
        // 🔹 5. Бітрейт (CBR/VBR контроль)
        if variant.Bitrate != nil {
            args = append(args, "-b:v:"+indexS, *variant.Bitrate)
        }
        
        // 🔹 6. CRF (якість для x264/x265)
        if variant.CRF != nil {
            args = append(args, "-crf", strconv.Itoa(*variant.CRF))
        }
        
        // 🔹 7. Профіль та рівень (сумісність з пристроями)
        if variant.Profile != nil && variant.Level != nil {
            args = append(args,
                "-profile:v:"+indexS, *variant.Profile,
                "-level", *variant.Level,
            )
        }
    }
    return args
}
```

### 🎯 Ключові моменти реалізації

#### 🔹 Фіксований GOP (`-g 60`)

```
• GOP (Group of Pictures) = інтервал між ключовими кадрами (I-frames)
• -g 60 = ключовий кадр кожні 60 кадрів
• При 30 fps → ключовий кадр кожні 2 секунди

Чому це важливо для CCTV HLS:
✅ Швидкий seek у записі (плеєр може стрибати по I-frames)
✅ Краща стійкість до втрати пакетів (кожен GOP автономний)
✅ Сумісність з низькопотужними пристроями

⚠️ Компроміс:
• Менший GOP = більший розмір файлу (більше I-frames)
• Більший GOP = краща компресія, але гірший seek
```

#### 🔹 Bitstream Filter для H.264: `h264_mp4toannexb`

```
Проблема:
• FFmpeg за замовчуванням генерує H.264 у форматі MP4 (AVCC)
• HLS вимагає формат Annex B (start codes 0x000001)

Рішення:
• -bsf:v h264_mp4toannexb конвертує AVCC → Annex B "на льоту"

Наслідок без цього фільтра:
❌ Плеєри не зможуть декодувати сегменти
❌ Помилки "invalid NAL unit" або "no start code"
```

#### 🔹 Масштабування з гарантією парної ширини

```go
fmt.Sprintf("scale=trunc(oh*a/2)*2:%d", *variant.ResolutionHeight)
```

**Розбір формули:**
```
• oh = output height (задана висота)
• a = aspect ratio вхідного відео (width/height)
• oh*a = теоретична ширина
• /2)*2 = округлення до парного числа вниз

Приклад:
• Вхід: 1920×1080 (16:9), цільова висота = 720
• Теоретична ширина: 720 × (1920/1080) = 1280
• Парна перевірка: 1280 вже парне → залишається 1280
• Вихід: 1280×720 ✅

Чому парна ширина важлива:
• Багато кодеків (H.264, H.265) вимагають парні розміри через макроблоки 2×2
• Непарні розміри → помилка енкодингу або автоматичне обрізання
```

---

## 🔧 Функція `audioConversionArgs`: побудова аудіо-параметрів

### 📦 Вхідні дані: `suggest.AudioVariant`

```go
// Гіпотетична структура:
type AudioVariant struct {
    MapInput           string   // 🎯 FFmpeg map: "0:a:0", "0:a:1"...
    Codec              string   // 🎯 Кодек: "aac", "mp3", "copy"...
    Bitrate            *string  // 🎯 Цільовий бітрейт: "128k", "256k"...
    ConvertToStereo    bool     // 🎯 Конвертувати 5.1/7.1 → stereo
}
```

### 🔍 Покроковий розбір

```go
func audioConversionArgs(variants []suggest.AudioVariant) []string {
    for outputIndex, variant := range variants {
        indexS := strconv.Itoa(outputIndex)
        
        // 🔹 1. Базові параметри: map + codec + GOP
        args = append(args, 
            "-map", variant.MapInput,
            "-c:a:"+indexS, variant.Codec,
            "-g", "60")  // 🔹 Консистентність з відео: той самий GOP
        
        // 🔹 2. Асинхронна обробка аудіо (тільки якщо не "copy")
        if variant.Codec != "copy" {
            // 🔹 Ключовий момент: запобігання десинхронізації
            args = append(args, "-af", "aresample=async=1:first_pts=0")
        }
        
        // 🔹 3. Бітрейт аудіо
        if variant.Bitrate != nil {
            args = append(args, "-b:a:"+indexS, *variant.Bitrate)
        }
        
        // 🔹 4. Конвертація багатоканального аудіо → stereo
        if variant.ConvertToStereo {
            args = append(args,
                "-ac:a:"+indexS, "2",  // Встановити 2 канали
                // 🔹 Складний фільтр панорамування для коректного downmix
                "-filter:a:"+indexS, 
                "pan=stereo|FL < 1.0*FL + 0.707*FC + 0.707*BL|FR < 1.0*FR + 0.707*FC + 0.707*BR",
            )
        }
    }
    return args
}
```

### 🎯 Ключові моменти реалізації

#### 🔹 Асинхронна обробка аудіо: `aresample=async=1:first_pts=0`

```
Проблема:
• Аудіо та відео можуть мати різні таймінги на вході
• Без синхронізації → десинхронізація у виході (аудіо випереджає/відстає)

Рішення (з https://stackoverflow.com/a/63995029):
• aresample=async=1: дозволити FFmpeg коригувати семпли для синхронізації
• first_pts=0: встановити початкову точку відліку для уникнення дрейфу

Ефект:
✅ Аудіо автоматично "підтягується" до відео таймінгів
✅ Запобігання накопиченню дрейфу у довгих записів
✅ Краща A/V синхронізація у фінальному HLS

⚠️ Коли не використовувати:
• Codec = "copy" (немає переенкодингу → не можна змінювати семпли)
```

#### 🔹 Downmix 5.1 → Stereo: фільтр `pan`

```
Проблема:
• Багато плеєрів (особливо веб) не підтримують 5.1/7.1 аудіо
• Потрібно конвертувати у stereo для широкої сумісності

Наївне рішення (неправильне):
• -ac 2 → просто викидає задні канали, втрачається просторовість

Правильне рішення (панорамування):
pan=stereo|
  FL < 1.0*FL + 0.707*FC + 0.707*BL|
  FR < 1.0*FR + 0.707*FC + 0.707*BR

Розбір коефіцієнтів:
• FL/FR (Front Left/Right): 1.0 × оригінал (повна гучність)
• FC (Front Center): 0.707 × для обох каналів (√2/2 ≈ -3dB)
  → центральний діалог рівномірно розподіляється
• BL/BR (Back Left/Right): 0.707 × для відповідних каналів
  → задні ефекти додаються з пом'якшенням

Чому 0.707?
• Це √2/2 — коефіцієнт для збереження загальної потужності сигналу
• Без нього: сума каналів могла б перевищити 0 dB → кліппінг

Результат:
✅ Збереження просторовості при конвертації
✅ Діалоги залишаються чіткими (центр не втрачається)
✅ Сумісність з будь-яким плеєром
```

---

## 🔧 Функція `variantsMapArg`: генерація `-map` аргументу

```go
func variantsMapArg(videoVariants []suggest.VideoVariant, audioVariants []suggest.AudioVariant) string {
    mapArray := make([]string, 0, len(videoVariants)+len(audioVariants))
    
    // 🔹 Додати всі відео-варіанти
    for variantIndex := range videoVariants {
        mapArray = append(mapArray, "v:"+strconv.Itoa(variantIndex))
    }
    
    // 🔹 Додати всі аудіо-варіанти
    for variantIndex := range audioVariants {
        mapArray = append(mapArray, "a:"+strconv.Itoa(variantIndex))
    }
    
    // 🔹 Об'єднати у рядок: "v:0 v:1 a:0 a:1"
    return strings.Join(mapArray, " ")
}
```

### 🎯 Використання результату у FFmpeg

```bash
# Приклад виходу для 2 відео + 2 аудіо варіантів:
# variantsMapArg(...) → "v:0 v:1 a:0 a:1"

# Повна команда FFmpeg:
ffmpeg -i input.ts \
  -map 0:v:0 -map 0:v:1 -map 0:a:0 -map 0:a:1 \
  [відео-аргументи для v:0] \
  [відео-аргументи для v:1] \
  [аудіо-аргументи для a:0] \
  [аудіо-аргументи для a:1] \
  -f hls output.m3u8

# 🔹 -map вказує, які вхідні потоки використовувати
# 🔹 Індекси (0,1...) мають співпадати з індексами у -c:v:0, -b:v:1 тощо
```

> 💡 **Важливо**: Порядок `-map` має відповідати порядку інших аргументів з індексами. FFmpeg прив'язує параметри за індексом виходу, не за вхідним потоком.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Побудова повної команди FFmpeg

```go
// У segmentFinalizer — генерація команди для енкодингу сегмента:
func buildFFmpegCommand(inputPath string, videoVars []suggest.VideoVariant, 
                       audioVars []suggest.AudioVariant, outputPath string) []string {
    
    args := []string{"-i", inputPath}
    
    // 🔹 Додати map аргумент
    mapArg := variantsMapArg(videoVars, audioVars)
    for _, stream := range strings.Fields(mapArg) {
        args = append(args, "-map", "0:"+stream)
    }
    
    // 🔹 Додати відео-параметри
    args = append(args, videoConversionArgs(videoVars)...)
    
    // 🔹 Додати аудіо-параметри
    args = append(args, audioConversionArgs(audioVars)...)
    
    // 🔹 HLS-специфічні параметри
    args = append(args,
        "-f", "hls",
        "-hls_time", "4",           // Довжина сегмента: 4 секунди
        "-hls_playlist_type", "vod", // Для VOD-контенту
        "-hls_segment_filename", outputPath+"_seg%d.ts",
        outputPath+".m3u8",
    )
    
    return args
}

// Використання:
cmd := exec.Command("ffmpeg", buildFFmpegCommand(input, videoVars, audioVars, output)...)
err := cmd.Run()
```

### ✅ 2: Адаптивний бітрейт на основі мережевих умов

```go
// У channel-aware архітектурі — динамічна генерація варіантів:
func suggestVariantsForChannel(channelID string, networkBandwidthMbps float64) 
    ([]suggest.VideoVariant, []suggest.AudioVariant) {
    
    var videoVars []suggest.VideoVariant
    var audioVars []suggest.AudioVariant
    
    // 🔹 Визначити доступні бітрейти на основі пропускної здатності
    maxVideoBitrate := int(networkBandwidthMbps * 0.8 * 1000) // 80% для відео, kbps
    
    // 🔹 Додати варіанти від низького до високого
    bitrates := []int{500, 1000, 2000, 4000, 8000}
    for _, br := range bitrates {
        if br > maxVideoBitrate {
            break
        }
        
        height := calculateHeightForBitrate(br)  // ваша логіка
        videoVars = append(videoVars, suggest.VideoVariant{
            MapInput:         "0:v:0",
            Codec:            "libx264",
            ResolutionHeight: &height,
            Bitrate:          stringPtr(fmt.Sprintf("%dk", br)),
            CRF:              intPtr(23),  // баланс якість/розмір
            Profile:          stringPtr("main"),
            Level:            stringPtr("4.0"),
        })
    }
    
    // 🔹 Аудіо: фіксований стерео AAC
    audioVars = append(audioVars, suggest.AudioVariant{
        MapInput:        "0:a:0",
        Codec:           "aac",
        Bitrate:         stringPtr("128k"),
        ConvertToStereo: true,  // гарантувати сумісність
    })
    
    return videoVars, audioVars
}

func stringPtr(s string) *string { return &s }
func intPtr(i int) *int          { return &i }
```

### ✅ 3: Моніторинг параметрів енкодингу

```go
// monitoring.Monitor — метрики для конвертації:
type EncoderMetrics struct {
    VariantsGenerated *prometheus.CounterVec  // кількість варіантів на канал
    AvgBitrate        *prometheus.GaugeVec    // середній бітрейт виходу
    EncodingLatency   *prometheus.HistogramVec  // час енкодингу сегмента
    CodecUsage        *prometheus.CounterVec  // розподіл кодеків
}

// У процесі енкодингу:
func monitorEncoding(channelID string, videoVars []suggest.VideoVariant, 
                    audioVars []suggest.AudioVariant, metrics *EncoderMetrics, 
                    duration time.Duration) {
    
    metrics.VariantsGenerated.WithLabelValues(channelID).Add(
        float64(len(videoVars) + len(audioVars)))
    
    // 🔹 Розрахувати середній бітрейт
    totalBitrate := 0
    count := 0
    for _, v := range videoVars {
        if v.Bitrate != nil {
            if br, err := parseBitrate(*v.Bitrate); err == nil {
                totalBitrate += br
                count++
            }
        }
    }
    if count > 0 {
        metrics.AvgBitrate.WithLabelValues(channelID).Set(float64(totalBitrate / count))
    }
    
    metrics.EncodingLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    
    for _, v := range videoVars {
        metrics.CodecUsage.WithLabelValues(channelID, v.Codec).Inc()
    }
}

func parseBitrate(s string) (int, error) {
    // "2000k" → 2000, "5M" → 5000 тощо
    s = strings.ToLower(s)
    multiplier := 1
    if strings.HasSuffix(s, "k") {
        multiplier = 1
        s = s[:len(s)-1]
    } else if strings.HasSuffix(s, "m") {
        multiplier = 1000
        s = s[:len(s)-1]
    }
    return strconv.Atoi(s) * multiplier
}
```

### ✅ 4: Валідація параметрів перед енкодингом

```go
// Перевірити сумісність параметрів перед запуском FFmpeg:
func validateVariants(videoVars []suggest.VideoVariant, audioVars []suggest.AudioVariant) error {
    for i, v := range videoVars {
        // 🔹 Кодек має підтримувати вказаний профіль
        if v.Codec == "libx264" && v.Profile != nil {
            validProfiles := []string{"baseline", "main", "high"}
            if !contains(validProfiles, *v.Profile) {
                return fmt.Errorf("variant %d: invalid H.264 profile: %s", i, *v.Profile)
            }
        }
        
        // 🔹 CRF має бути у діапазоні 0-51 для x264/x265
        if v.CRF != nil && (v.Codec == "libx264" || v.Codec == "libx265") {
            if *v.CRF < 0 || *v.CRF > 51 {
                return fmt.Errorf("variant %d: CRF out of range [0-51]: %d", i, *v.CRF)
            }
        }
        
        // 🔹 Висота має бути парною (вимога кодеків)
        if v.ResolutionHeight != nil && *v.ResolutionHeight%2 != 0 {
            return fmt.Errorf("variant %d: height must be even: %d", i, *v.ResolutionHeight)
        }
    }
    
    for i, a := range audioVars {
        // 🔹 Бітрейт аудіо має бути розумним
        if a.Bitrate != nil {
            br, _ := parseBitrate(*a.Bitrate)
            if br < 32 || br > 320 {  // 32-320 kbps для AAC
                return fmt.Errorf("variant %d: audio bitrate out of range [32-320]k: %s", i, *a.Bitrate)
            }
        }
    }
    
    return nil
}

func contains(slice []string, item string) bool {
    for _, s := range slice {
        if s == item {
            return true
        }
    }
    return false
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на генерацію аргументів для H.264

```go
func TestVideoConversionArgs_H264(t *testing.T) {
    variants := []suggest.VideoVariant{
        {
            MapInput:         "0:v:0",
            Codec:            "libx264",
            ResolutionHeight: intPtr(720),
            Bitrate:          stringPtr("2000k"),
            CRF:              intPtr(23),
            Profile:          stringPtr("main"),
            Level:            stringPtr("4.0"),
        },
    }
    
    args := videoConversionArgs(variants)
    
    // 🔹 Перевірити наявність ключових аргументів
    assert.Contains(t, args, "-map")
    assert.Contains(t, args, "0:v:0")
    assert.Contains(t, args, "-c:v:0")
    assert.Contains(t, args, "libx264")
    assert.Contains(t, args, "-g")
    assert.Contains(t, args, "60")
    
    // 🔹 H.264-специфічні аргументи
    assert.Contains(t, args, "-bsf:v:0")
    assert.Contains(t, args, "h264_mp4toannexb")
    assert.Contains(t, args, "-pix_fmt")
    assert.Contains(t, args, "yuv420p")
    
    // 🔹 Масштабування
    assert.Contains(t, args, "-filter:v:0")
    assert.Contains(t, args, "scale=trunc(oh*a/2)*2:720")
    
    // 🔹 Бітрейт та якість
    assert.Contains(t, args, "-b:v:0")
    assert.Contains(t, args, "2000k")
    assert.Contains(t, args, "-crf")
    assert.Contains(t, args, "23")
    
    // 🔹 Профіль та рівень
    assert.Contains(t, args, "-profile:v:0")
    assert.Contains(t, args, "main")
    assert.Contains(t, args, "-level")
    assert.Contains(t, args, "4.0")
}
```

### 🔹 Тест на аудіо downmix

```go
func TestAudioConversionArgs_StereoDownmix(t *testing.T) {
    variants := []suggest.AudioVariant{
        {
            MapInput:        "0:a:0",
            Codec:           "aac",
            Bitrate:         stringPtr("128k"),
            ConvertToStereo: true,
        },
    }
    
    args := audioConversionArgs(variants)
    
    // 🔹 Базові аргументи
    assert.Contains(t, args, "-map")
    assert.Contains(t, args, "0:a:0")
    assert.Contains(t, args, "-c:a:0")
    assert.Contains(t, args, "aac")
    
    // 🔹 Асинхронна обробка
    assert.Contains(t, args, "-af")
    assert.Contains(t, args, "aresample=async=1:first_pts=0")
    
    // 🔹 Конвертація у stereo
    assert.Contains(t, args, "-ac:a:0")
    assert.Contains(t, args, "2")
    assert.Contains(t, args, "-filter:a:0")
    assert.Contains(t, args, "pan=stereo|FL < 1.0*FL + 0.707*FC + 0.707*BL|FR < 1.0*FR + 0.707*FC + 0.707*BR")
}
```

### 🔹 Тест на variantsMapArg

```go
func TestVariantsMapArg(t *testing.T) {
    videoVars := make([]suggest.VideoVariant, 3)  // 3 відео варіанти
    audioVars := make([]suggest.AudioVariant, 2)  // 2 аудіо варіанти
    
    result := variantsMapArg(videoVars, audioVars)
    
    // 🔹 Очікуваний формат: "v:0 v:1 v:2 a:0 a:1"
    expected := "v:0 v:1 v:2 a:0 a:1"
    assert.Equal(t, expected, result)
    
    // 🔹 Перевірити порядок: спочатку відео, потім аудіо
    parts := strings.Fields(result)
    assert.Equal(t, "v:0", parts[0])
    assert.Equal(t, "a:0", parts[3])  // перший аудіо після трьох відео
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Непарна висота відео | Помилка енкодингу "width/height not divisible by 2" | Перевірити формулу масштабування: `trunc(oh*a/2)*2` гарантує парність |
| Десинхронізація аудіо | Аудіо випереджає/відстає від відео у виході | Додати `-af aresample=async=1:first_pts=0` для всіх варіантів окрім "copy" |
| Неправильний порядок `-map` | FFmpeg застосовує параметри не до тих потоків | Переконатися, що індекси у `-map`, `-c:v:N`, `-b:v:N` співпадають |
| CRF + бітрейт конфлікт | Непередбачувана якість/розмір виходу | Використовувати або CRF (якість), або бітрейт (розмір), не обидва одночасно |
| Missing h264_mp4toannexb | Сегменти не відтворюються у плеєрах | Додати `-bsf:v h264_mp4toannexb` для всіх H.264 варіантів |

### Приклад валідації парності висоти:

```go
func ensureEvenHeight(height int) int {
    if height%2 != 0 {
        return height - 1  // округлити вниз до парного
    }
    return height
}

// Використання при побудові варіантів:
targetHeight := 721  // непарне!
safeHeight := ensureEvenHeight(targetHeight)  // → 720
variant.ResolutionHeight = &safeHeight
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Побудова повної команди для FFmpeg:
func buildFFmpegArgs(input string, videoVars []suggest.VideoVariant, 
                    audioVars []suggest.AudioVariant, outputM3U8 string) []string {
    
    args := []string{"-i", input}
    
    // Map streams
    for _, stream := range strings.Fields(variantsMapArg(videoVars, audioVars)) {
        args = append(args, "-map", "0:"+stream)
    }
    
    // Video params
    args = append(args, videoConversionArgs(videoVars)...)
    
    // Audio params
    args = append(args, audioConversionArgs(audioVars)...)
    
    // HLS output
    args = append(args,
        "-f", "hls",
        "-hls_time", "4",
        "-hls_playlist_type", "vod",
        "-hls_segment_filename", strings.TrimSuffix(outputM3U8, ".m3u8")+"_seg%d.ts",
        outputM3U8,
    )
    
    return args
}

// 2: Валідація перед енкодингом:
func safeConvert(input string, videoVars []suggest.VideoVariant, 
                audioVars []suggest.AudioVariant, output string) error {
    
    if err := validateVariants(videoVars, audioVars); err != nil {
        return fmt.Errorf("invalid variants: %w", err)
    }
    
    args := buildFFmpegArgs(input, videoVars, audioVars, output)
    cmd := exec.Command("ffmpeg", args...)
    
    // 🔹 Логування команди для відладки
    log.Debugf("Running ffmpeg: %s", strings.Join(args, " "))
    
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("ffmpeg failed: %w", err)
    }
    
    return nil
}

// 3: Helper для створення варіантів:
func NewVideoVariant(mapInput, codec string, height, bitrateKbps, crf int, 
                    profile, level string, addHVC1Tag bool) suggest.VideoVariant {
    return suggest.VideoVariant{
        MapInput:         mapInput,
        Codec:            codec,
        ResolutionHeight: &height,
        Bitrate:          stringPtr(fmt.Sprintf("%dk", bitrateKbps)),
        CRF:              &crf,
        Profile:          stringPtr(profile),
        Level:            stringPtr(level),
        AddHVC1Tag:       addHVC1Tag,
    }
}

func NewAudioVariant(mapInput, codec string, bitrateKbps int, stereo bool) suggest.AudioVariant {
    return suggest.AudioVariant{
        MapInput:        mapInput,
        Codec:           codec,
        Bitrate:         stringPtr(fmt.Sprintf("%dk", bitrateKbps)),
        ConvertToStereo: stereo,
    }
}

// 4: Моніторинг:
func logEncodingStart(channelID string, videoVars, audioVars int) {
    log.Infof("Channel %s: starting encoding with %d video + %d audio variants", 
        channelID, videoVars, audioVars)
}

func logEncodingComplete(channelID string, duration time.Duration, outputSize int64) {
    log.Infof("Channel %s: encoding complete in %v, output size: %d bytes", 
        channelID, duration, outputSize)
}
```

---

## 📊 Матриця параметрів енкодингу для CCTV HLS

```
Параметр          | Тип       | Рекомендоване значення      | Призначення
──────────────────┼───────────┼─────────────────────────────┼─────────────────────────
-g (GOP)          | int       | 60 (2 секунди @ 30 fps)     | ✅ Швидкий seek, стійкість
-pix_fmt          | string    | yuv420p                     | ✅ Сумісність з усіма плеєрами
-bsf:v h264_mp4toannexb | -  | (для H.264)                 | ✅ Конвертація формату для HLS
-tag:v hvc1       | bool      | true для HEVC               | ✅ Сумісність з Apple/Safari
-af aresample     | string    | async=1:first_pts=0         | ✅ A/V синхронізація
-pan (downmix)    | string    | 0.707 коефіцієнти           | ✅ Коректний 5.1→stereo
-scale formula    | string    | trunc(oh*a/2)*2             | ✅ Гарантія парної ширини
-CRF              | int       | 20-28 (менше = краще)       | ⚠️ Якість для x264/x265
-profile/-level   | string    | main/4.0 для більшості      | ✅ Сумісність з пристроями
```

---

## 📚 Корисні посилання

- [FFmpeg HLS muxer documentation](https://ffmpeg.org/ffmpeg-formats.html#hls)
- [H.264 bitstream filter docs](https://ffmpeg.org/ffmpeg-bitstream-filters.html#h264_mp4toannexb)
- [AAC audio encoding guide](https://trac.ffmpeg.org/wiki/Encode/AAC)
- [pan filter for downmixing](https://ffmpeg.org/ffmpeg-filters.html#pan)
- [CRF rate control explanation](https://slhck.info/video/2017/02/24/crf-guide.html)

> 💡 **Ключова ідея**: Цей `converter` — це "перекладач" між вашою бізнес-логікою та командним рядком FFmpeg. У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Автоматично генерувати адаптивні варіанти для різних мережевих умов
> - 🔧 Гарантувати сумісність виходу з широким спектром плеєрів та пристроїв
> - ⚡ Оптимізувати параметри енкодингу для балансу якість/розмір/швидкість
> - 🛡️ Запобігати поширеним помилкам через валідацію параметрів

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку нових кодеків (AV1, VP9) або параметрів енкодингу
- 🧪 Написати integration-тест для перевірки сумісності вихідних HLS-файлів з реальними плеєрами
- 📈 Додати Prometheus-метрики для моніторингу якості енкодингу та продуктивності по каналах

🛠️