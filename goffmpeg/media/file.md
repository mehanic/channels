# 🎬 `media.File`: Конфігуратор команд FFmpeg у Go

Це **потужний об'єкт-конфігуратор** бібліотеки `go-ffmpeg` (або подібної), який дозволяє **декларативно налаштовувати параметри кодування відео/аудіо** та генерувати відповідну командну строку для FFmpeg — ідеально для інтеграції медіа-обробки у ваші Go-додатки.

---

## 🎯 Коротка відповідь

> **Це "будівельник команд" для FFmpeg**: він надає зручний Go API для встановлення параметрів кодування (кодеки, бітрейт, роздільність, фільтри, HLS-налаштування тощо) та автоматично генерує валідну командну строку для виклику FFmpeg — без необхідності ручного форматування аргументів.

---

## 🧱 Основні компоненти

### 🔹 Структура `File` — конфігурація завдання

```go
type File struct {
    // 🔹 Відео-параметри
    resolution            string  // Роздільність: "1920x1080"
    videoBitRate          string  // Бітрейт відео: "2000k"
    videoCodec            string  // Кодек відео: "libx264", "h264_nvenc"
    videoProfile          string  // Профіль кодека: "high", "main"
    pixFmt                string  // Формат пікселів: "yuv420p"
    frameRate             int     // Частота кадрів: 24, 30, 60
    vframes               int     // Кількість кадрів для обробки
    keyframeInterval      int     // Інтервал ключових кадрів (GOP)
    bframe                int     // Кількість B-фреймів
    videoFilter           string  // Відео-фільтр: "scale=1280:720"
    
    // 🔹 Аудіо-параметри
    audioCodec            string  // Кодек аудіо: "aac", "mp3"
    audioBitrate          string  // Бітрейт аудіо: "128k"
    audioChannels         int     // Кількість каналів: 1 (моно), 2 (стерео)
    audioRate             int     // Частота дискретизації: 44100, 48000
    audioProfile          string  // Профіль аудіо: "aac_low"
    audioFilter           string  // Аудіо-фільтр: "volume=1.5"
    audioVariableBitrate  bool    // Використовувати VBR для аудіо
    
    // 🔹 Параметри кодування
    preset                string  // Пресет швидкості: "ultrafast", "slow"
    tune                  string  // Налаштування: "film", "animation"
    crf                   uint32  // Constant Rate Factor (18-28 для x264)
    qscale                uint32  // Якість (1-31 для MPEG)
    threads               int     // Кількість потоків кодування
    bufferSize            int     // Розмір буфера бітрейту
    compressionLevel      int     // Рівень стиснення
    
    // 🔹 Вхід/вихід
    inputPath             string  // Шлях до вхідного файлу
    outputPath            string  // Шлях до вихідного файлу
    outputFormat          string  // Формат виводу: "mp4", "hls", "webm"
    duration              string  // Тривалість обробки: "00:01:30"
    seekTime              string  // Час початку обробки: "00:00:10"
    copyTs                bool    // Копіювати таймстемпи
    inputPipe             bool    // Читати вхід з pipe:0
    outputPipe            bool    // Писати вихід у pipe:1
    
    // 🔹 HLS-специфічні параметри
    hlsListSize           int     // Кількість сегментів у плейлисті
    hlsSegmentDuration    int     // Тривалість сегмента (секунди)
    hlsPlaylistType       string  // Тип плейлиста: "vod", "event"
    hlsMasterPlaylistName string  // Ім'я master-плейлиста
    hlsSegmentFilename    string  // Шаблон імен сегментів
    encryptionKey         string  // Шлях до файлу ключа шифрування
    
    // 🔹 Додаткові параметри
    metadata              Metadata   // Метадані файлу
    tags                  map[string]string  // Ключ-значення теги
    streamIds             map[int]string   // Мапінг ID потоків
    hwaccel               string    // Апаратне прискорення: "cuda", "vaapi"
    rawInputArgs          []string  // Сирі аргументи для входу
    rawOutputArgs         []string  // Сирі аргументи для виходу
    // ... та ще 20+ полів
}
```

**🎯 Призначення**: Інкапсулювати **всі можливі параметри кодування** у одному об'єкті з типобезпечним доступом.

---

### 🔹 Setters — типобезпечне встановлення параметрів

```go
// 🔹 Приклад: встановлення основних параметрів відео
file := &media.File{}
file.SetVideoCodec("libx264")           // ✅ Кодек
file.SetResolution("1920x1080")         // ✅ Роздільність
file.SetVideoBitRate("5000k")           // ✅ Бітрейт
file.SetFrameRate(30)                   // ✅ FPS
file.SetPreset("medium")                // ✅ Пресет швидкості
file.SetCRF(23)                         // ✅ Якість (CRF)

// 🔹 Аудіо-параметри
file.SetAudioCodec("aac")
file.SetAudioBitRate("192k")
file.SetAudioChannels(2)
file.SetAudioRate(48000)

// 🔹 Вхід/вихід
file.SetInputPath("input.mp4")
file.SetOutputPath("output.mp4")
file.SetOutputFormat("mp4")
```

**🎯 Призначення**: Забезпечити **type-safe API** для налаштування — компілятор перевіряє типи, IDE пропонує автодоповнення.

---

### 🔹 Getters — отримання поточних значень

```go
// 🔹 Перевірка налаштувань перед кодуванням
if file.VideoCodec() == "" {
    return fmt.Errorf("video codec not set")
}
if file.InputPath() == "" {
    return fmt.Errorf("input path not set")
}

// 🔹 Логування конфігурації
log.Printf("🎬 Encoding: %s → %s @ %s, codec=%s",
    file.InputPath(),
    file.OutputPath(),
    file.Resolution(),
    file.VideoCodec())
```

**🎯 Призначення**: Дозволити **валідацію та логування** конфігурації перед виконанням.

---

### 🔹 `ToStrCommand()` — генерація командної строки

```go
func (m *File) ToStrCommand() []string {
    var strCommand []string
    
    // 🔹 Список параметрів у порядку пріоритету
    opts := []string{
        "SeekTimeInput", "InputPath", "VideoCodec", "Resolution",
        "VideoBitRate", "AudioCodec", "OutputPath", "HlsListSize",
        // ... ще 60+ опцій
    }
    
    // 🔹 Для кожної опції:
    for _, name := range opts {
        // 1. Знайти метод Obtain{Name} через рефлексію
        opt := reflect.ValueOf(m).MethodByName(fmt.Sprintf("Obtain%s", name))
        
        // 2. Викликати метод, якщо існує
        if (opt != reflect.Value{}) {
            result := opt.Call([]reflect.Value{})
            
            // 3. Додати повернуті аргументи до команди
            if val, ok := result[0].Interface().([]string); ok {
                strCommand = append(strCommand, val...)
            }
        }
    }
    
    return strCommand
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: конфігурація File з встановленими параметрами
│
▼
🔹 Для кожної опції у списку:
   │
   ├── 🔹 Шукати метод Obtain{Name} через reflect
   │   • Напр. "VideoCodec" → шукає ObtainVideoCodec()
   │
   ├── 🔹 Якщо метод знайдено → викликати його
   │   • ObtainVideoCodec() повертає []string{"-c:v", "libx264"} або nil
   │
   ├── 🔹 Якщо повернуто не порожній масив → додати до strCommand
   │
   ▼
🔹 Вихід: []string з аргументами для exec.Command("ffmpeg", ...)
```

**🎯 Призначення**: Автоматично **перетворити об'єктну конфігурацію у командну строку** без ручного форматування.

---

### 🔹 `Obtain*` методи — логіка перетворення параметрів у аргументи

Кожен `Obtain*` метод реалізує **логіку перетворення значення поля у аргументи FFmpeg**:

```go
// 🔹 Приклад: ObtainVideoCodec
func (m *File) ObtainVideoCodec() []string {
    if m.videoCodec != "" {
        return []string{"-c:v", m.videoCodec}  // ✅ "-c:v libx264"
    }
    return nil  // ❌ Не додавати аргумент, якщо не встановлено
}

// 🔹 Приклад: ObtainResolution
func (m *File) ObtainResolution() []string {
    if m.resolution != "" {
        return []string{"-s", m.resolution}  // ✅ "-s 1920x1080"
    }
    return nil
}

// 🔹 Приклад: ObtainAudioBitRate (складніша логіка)
func (m *File) ObtainAudioBitRate() []string {
    switch {
    case !m.audioVariableBitrate && m.audioBitrate != "":
        return []string{"-b:a", m.audioBitrate}  // ✅ CBR: "-b:a 128k"
    case m.audioVariableBitrate && m.audioBitrate != "":
        return []string{"-q:a", m.audioBitrate}  // ✅ VBR: "-q:a 2"
    case m.audioVariableBitrate:
        return []string{"-q:a", "0"}  // ✅ VBR без значення: "-q:a 0"
    default:
        return nil  // ❌ Не додавати аргумент
    }
}

// 🔹 Приклад: ObtainAspect (обчислення з роздільності)
func (m *File) ObtainAspect() []string {
    // 🔹 Якщо задано роздільність → обчислити aspect ratio
    if m.resolution != "" {
        resolution := strings.Split(m.resolution, "x")
        if len(resolution) == 2 {  // ✅ Перевірка на коректність
            width, _ := strconv.ParseFloat(resolution[0], 64)
            height, _ := strconv.ParseFloat(resolution[1], 64)
            return []string{"-aspect", fmt.Sprintf("%f", width/height)}
        }
    }
    // 🔹 Якщо задано aspect напряму → використати його
    if m.aspect != "" {
        return []string{"-aspect", m.aspect}
    }
    return nil
}
```

**🎯 Ключові принципи:**
- ✅ Повертати `nil`, якщо параметр не встановлено (не додавати зайвих аргументів)
- ✅ Форматувати значення у правильному форматі для FFmpeg (`"5000k"`, `"1920x1080"`)
- ✅ Обробляти складну логіку (VBR/CBR для аудіо, обчислення aspect ratio)
- ✅ Валідувати вхідні дані (напр. перевірка `len(resolution) == 2`)

---

## 🔍 Повний приклад: Генерація команди для HLS

```go
// 🔹 Створення конфігурації
file := &media.File{}

// 🔹 Вхід/вихід
file.SetInputPath("camera_feed.mp4")
file.SetOutputPath("hls_output/")
file.SetOutputFormat("hls")

// 🔹 Відео-параметри
file.SetVideoCodec("libx264")
file.SetResolution("1280x720")
file.SetVideoBitRate("2500k")
file.SetFrameRate(30)
file.SetPreset("fast")
file.SetCRF(23)
file.SetKeyframeInterval(90)  // 🔹 Ключовий кадр кожні 3 секунди @ 30fps

// 🔹 Аудіо-параметри
file.SetAudioCodec("aac")
file.SetAudioBitRate("128k")
file.SetAudioChannels(2)

// 🔹 HLS-специфічні налаштування
file.SetHlsListSize(5)                    // 🔹 5 сегментів у плейлисті
file.SetHlsSegmentDuration(3)             // 🔹 3 секунди на сегмент
file.SetHlsPlaylistType("vod")            // 🔹 Video on Demand
file.SetHlsSegmentFilename("seg_%03d.ts") // 🔹 Імена сегментів
file.SetHlsMasterPlaylistName("master.m3u8")

// 🔹 Додаткові параметри
file.SetThreads(4)        // 🔹 4 потоки кодування
file.SetMovFlags("+faststart")  // 🔹 Оптимізація для web

// 🔹 Генерація команди
args := file.ToStrCommand()

// 🔹 Результат (спрощено):
[
  "-c:v", "libx264",
  "-s", "1280x720",
  "-b:v", "2500k",
  "-r", "30",
  "-preset", "fast",
  "-crf", "23",
  "-g", "90",
  "-c:a", "aac",
  "-b:a", "128k",
  "-ac", "2",
  "-threads", "4",
  "-movflags", "+faststart",
  "-f", "hls",
  "-hls_list_size", "5",
  "-hls_time", "3",
  "-hls_playlist_type", "vod",
  "-hls_segment_filename", "seg_%03d.ts",
  "-master_pl_name", "master.m3u8",
  "-i", "camera_feed.mp4",
  "hls_output/"
]

// 🔹 Виконання:
cmd := exec.Command("ffmpeg", args...)
cmd.Run()
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Конвертація запису камери у HLS

```go
// 🔹 Функція для підготовки запису для стрімінгу
func prepareRecordingForHLS(inputPath, outputDir string) error {
    file := &media.File{}
    
    // 🔹 Основні параметри
    file.SetInputPath(inputPath)
    file.SetOutputPath(outputDir)
    file.SetOutputFormat("hls")
    
    // 🔹 Відео: H.264, 720p, 2.5 Mbps
    file.SetVideoCodec("libx264")
    file.SetResolution("1280x720")
    file.SetVideoBitRate("2500k")
    file.SetFrameRate(25)  // 🔹 PAL стандарт для камер
    file.SetPreset("medium")
    file.SetCRF(23)
    
    // 🔹 Ключові кадри кожні 2 секунди для кращого seek
    file.SetKeyframeInterval(50)  // 25 fps × 2s = 50
    
    // 🔹 Аудіо: AAC, 128 kbps, стерео
    file.SetAudioCodec("aac")
    file.SetAudioBitRate("128k")
    file.SetAudioChannels(2)
    
    // 🔹 HLS налаштування
    file.SetHlsListSize(10)  // 🔹 10 сегментів = 30 секунд буфера
    file.SetHlsSegmentDuration(3)  // 🔹 3 секунди на сегмент
    file.SetHlsPlaylistType("vod")
    file.SetHlsSegmentFilename("seg_%04d.ts")
    
    // 🔹 Оптимізація
    file.SetThreads(0)  // 🔹 Авто-визначення потоків
    file.SetMovFlags("+faststart")
    
    // 🔹 Генерація та виконання команди
    args := file.ToStrCommand()
    cmd := exec.Command("ffmpeg", args...)
    
    // 🔹 Логування для дебагу
    log.Printf("🎬 Running: ffmpeg %v", args)
    
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("ffmpeg failed: %w", err)
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Адаптивний стрімінг з кількома якостями

```go
// 🔹 Конфігурація для кількох варіантів якості
type HLSProfile struct {
    Name        string
    Resolution  string
    VideoBitrate string
    AudioBitrate string
}

func createAdaptiveHLS(inputPath, outputDir string, profiles []HLSProfile) error {
    var variantArgs []string
    
    for i, profile := range profiles {
        file := &media.File{}
        
        // 🔹 Спільні параметри
        file.SetInputPath(inputPath)
        file.SetOutputFormat("hls")
        file.SetVideoCodec("libx264")
        file.SetAudioCodec("aac")
        file.SetPreset("fast")
        file.SetCRF(23)
        file.SetKeyframeInterval(60)  // 🔹 Синхронізація ключових кадрів
        
        // 🔹 Профіль-специфічні параметри
        file.SetResolution(profile.Resolution)
        file.SetVideoBitRate(profile.VideoBitrate)
        file.SetAudioBitRate(profile.AudioBitrate)
        
        // 🔹 HLS параметри
        file.SetHlsListSize(5)
        file.SetHlsSegmentDuration(4)
        file.SetHlsPlaylistType("vod")
        
        // 🔹 Унікальні імена для кожного варіанту
        variantName := fmt.Sprintf("var_%d", i)
        file.SetHlsSegmentFilename(fmt.Sprintf("%s/seg_%%03d.ts", variantName))
        file.SetOutputPath(fmt.Sprintf("%s/%s/index.m3u8", outputDir, variantName))
        
        // 🔹 Додавання до master-плейлиста
        variantArgs = append(variantArgs, file.ToStrCommand()...)
    }
    
    // 🔹 Генерація master-плейлиста
    masterFile := &media.File{}
    masterFile.SetHlsMasterPlaylistName("master.m3u8")
    // ... додавання варіантів до master ...
    
    // 🔹 Виконання всіх команд
    // (у реальному коді: паралельне виконання або пакетна обробка)
    
    return nil
}

// 🔹 Використання:
profiles := []HLSProfile{
    {"low", "640x360", "800k", "96k"},
    {"medium", "1280x720", "2500k", "128k"},
    {"high", "1920x1080", "5000k", "192k"},
}
createAdaptiveHLS("recording.mp4", "/hls/output", profiles)
```

---

### 🔹 Приклад 3: Обробка в реальному часі через pipe

```go
// 🔹 Функція для стрімінгу з камери у реальному часі
func streamFromCamera(cameraURL, hlsOutputDir string) error {
    file := &media.File{}
    
    // 🔹 Вхід: RTSP потік з камери
    file.SetInputPath(cameraURL)  // "rtsp://camera-ip/stream"
    file.SetRtmpLive("live")      // 🔹 Оптимізація для live-потоків
    file.SetCopyTs(true)          // 🔹 Копіювати оригінальні таймстемпи
    
    // 🔹 Кодування з низькою затримкою
    file.SetVideoCodec("libx264")
    file.SetPreset("ultrafast")   // 🔹 Мінімальна затримка
    file.SetTune("zerolatency")   // 🔹 Оптимізація для стрімінгу
    file.SetResolution("1280x720")
    file.SetVideoBitRate("2000k")
    file.SetKeyframeInterval(30)  // 🔹 Ключовий кадр кожну секунду @ 30fps
    
    // 🔹 Аудіо
    file.SetAudioCodec("aac")
    file.SetAudioBitRate("96k")
    
    // 🔹 HLS для live
    file.SetOutputFormat("hls")
    file.SetOutputPath(hlsOutputDir)
    file.SetHlsListSize(3)        // 🔹 Тільки 3 сегменти для мінімальної затримки
    file.SetHlsSegmentDuration(1) // 🔹 1 секунда на сегмент
    file.SetHlsPlaylistType("event")  // 🔹 Live-плейлист
    
    // 🔹 Виконання у фоновому режимі
    args := file.ToStrCommand()
    cmd := exec.Command("ffmpeg", args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    
    if err := cmd.Start(); err != nil {
        return fmt.Errorf("failed to start ffmpeg: %w", err)
    }
    
    // 🔹 Моніторинг процесу
    go func() {
        if err := cmd.Wait(); err != nil {
            log.Printf("❌ FFmpeg process failed: %v", err)
        } else {
            log.Printf("✅ Streaming completed")
        }
    }()
    
    return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний порядок аргументів | FFmpeg ігнорує параметри або видає помилку | Дотримуйтесь порядку: вхідні опції → `-i` → вихідні опції → вихідний файл |
| Відсутність валідації вхідних даних | Падіння при парсингу роздільності "1920" замість "1920x1080" | Перевіряйте `strings.Split(resolution, "x")` на довжину 2 |
| Ігнорування сумісності параметрів | `-crf` та `-b:v` разом → конфлікт | Використовуйте або CRF (якість), або бітрейт, не обидва одночасно |
| Неправильне форматування значень | `"2500"` замість `"2500k"` → бітрейт у 1000 разів менший | Завжди додавайте одиниці: `"2500k"`, `"128k"`, `"48000"` для частоти |
| Забути встановити вихідний формат | HLS не працює без `-f hls` | Завжди встановлюйте `SetOutputFormat()` для нестандартних форматів |

---

## 📋 Чекліст для вашого проекту

```
[ ] При налаштуванні кодування:
    • Встановлюйте videoCodec та audioCodec обов'язково
    • Використовуйте CRF (18-28) для якості або бітрейт для контролю розміру
    • Налаштовуйте keyframeInterval для кращого seek (2-4 секунди)
    • Перевіряйте сумісність параметрів: preset + tune + profile

[ ] Для HLS-стрімінгу:
    • Встановлюйте outputFormat="hls" та hlsPlaylistType ("vod"/"event")
    • Налаштовуйте hlsSegmentDuration (2-4 секунди для live, 4-6 для VOD)
    • Використовуйте hlsListSize для контролю буфера (3-10 сегментів)
    • Додавайте encryptionKey для DRM-захисту записів

[ ] Для обробки в реальному часі:
    • Використовуйте preset="ultrafast" + tune="zerolatency" для мінімальної затримки
    • Налаштовуйте keyframeInterval=частота_кадрів для ключового кадру кожну секунду
    • Використовуйте copyTs=true для збереження оригінальних таймстемпів
    • Моніторьте процес FFmpeg через cmd.Wait() у горутині

[ ] Для дебагу:
    • Логувайте згенеровану команду: log.Printf("🎬 ffmpeg %v", file.ToStrCommand())
    • Перехоплюйте stdout/stderr FFmpeg для аналізу помилок
    • Тестуйте з короткими відео перед запуском на повних запис

[ ] Для тестування:
    • Створюйте юніт-тести для ToStrCommand() з різними конфігураціями
    • Перевіряйте, що необов'язкові параметри не додають зайвих аргументів
    • Тестуйте крайні випадки: порожні значення, неправильні формати, конфлікти
```

---

## 🎯 Висновок

> **`media.File` — це "універсальний пульт" для керування FFmpeg**, який забезпечує:
> • ✅ Типобезпечний API для налаштування всіх параметрів кодування
> • ✅ Автоматичну генерацію валідної командної строки без ручного форматування
> • ✅ Підтримку складних сценаріїв: HLS, live-стрімінг, адаптивне кодування
> • ✅ Гнучкість: поєднання декларативних setter'ів та сирих аргументів
> • ✅ Безпеку: валідація вхідних даних та обробка крайніх випадків

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Швидка інтеграція кодування записів у HLS без вивчення всіх аргументів FFmpeg
- 🔧 Гнучке налаштування якості, затримки, сумісності для різних сценаріїв
- 📡 Підтримка live-стрімінгу з камер у реальному часі з мінімальною затримкою
- 🛡️ Надійність: типобезпечний API запобігає помилкам у командних рядках
- 🔄 Легке масштабування: додавання нових профілів кодування без зміни основної логіки

Потребуєте допомоги з налаштуванням специфічних параметрів кодування для ваших камер або з інтеграцією `media.File` у ваш конвеєр обробки записів? Напишіть — покажу готовий код для вашого сценарію! 🎬🚀