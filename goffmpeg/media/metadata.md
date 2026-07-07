# 📦 `media.Metadata`: Контейнер повних метаданих медіа-файлу

Це **коренева структура** бібліотеки для роботи з медіа-файлами, яка об'єднує **інформацію про потоки** (відео/аудіо/субтитри) та **формат-рівневі метадані** у єдиному об'єкті — ідеально для повного аналізу файлів перед кодуванням, валідації та генерації звітів.

---

## 🎯 Коротка відповідь

> **Це "повний паспорт файлу"**: він містить як детальну інформацію про кожен медіа-потік (кодек, роздільність, бітрейт, таймінги), так і загальні метадані контейнера (формат, тривалість, розмір, теги) — все, що потрібно для прийняття рішень про обробку медіа.

---

## 🧱 Структура `Metadata`

```go
type Metadata struct {
    Streams []Streams `json:"streams"`  // 🔹 Масив інформації про кожен потік
    Format  Format    `json:"format"`   // 🔹 Метадані контейнера/формату
}
```

**🎯 Призначення**: Представити **повний вихід FFprobe** у типобезпечному Go-форматі для:
- ✅ Комплексної валідації вхідних файлів
- ✅ Адаптивного вибору параметрів кодування
- ✅ Генерації детальних звітів про медіа-бібліотеку
- ✅ Інтеграції з системами моніторингу та аудиту

---

## 🔍 Детальний розбір компонентів

### 🔹 `Streams` — інформація про медіа-потоки

*(Примітка: структура `Streams` не показана у вашому коді, але зазвичай включає:)*

```go
type Streams struct {
    Index          int         `json:"index"`            // 🔹 Індексу потоку: 0, 1, 2...
    CodecName      string      `json:"codec_name"`       // 🔹 Кодек: "h264", "aac", "hevc"
    CodecLongName  string      `json:"codec_long_name"`  // 🔹 Повна назва кодека
    CodecType      string      `json:"codec_type"`       // 🔹 Тип: "video", "audio", "subtitle"
    SampleFmt      string      `json:"sample_fmt"`       // 🔹 Формат семплів (аудіо)
    
    // 🔹 Відео-специфічні поля
    Width          int         `json:"width"`            // 🔹 Ширина кадру
    Height         int         `json:"height"`           // 🔹 Висота кадру
    RFrameRate     string      `json:"r_frame_rate"`     // 🔹 Реальна частота кадрів: "30/1"
    AvgFrameRate   string      `json:"avg_frame_rate"`   // 🔹 Середня частота: "29.97"
    PixFmt         string      `json:"pix_fmt"`          // 🔹 Формат пікселів: "yuv420p"
    
    // 🔹 Аудіо-специфічні поля
    SampleRate     string      `json:"sample_rate"`      // 🔹 Частота дискретизації: "48000"
    Channels       int         `json:"channels"`         // 🔹 Кількість каналів: 2
    ChannelLayout  string      `json:"channel_layout"`   // 🔹 Розкладка: "stereo"
    
    // 🔹 Таймінги та бітрейт
    Duration       string      `json:"duration"`         // 🔹 Тривалість потоку
    BitRate        string      `json:"bit_rate"`         // 🔹 Бітрейт потоку
    NbFrames       string      `json:"nb_frames"`        // 🔹 Кількість кадрів/семплів
    
    // 🔹 Додаткові метадані
    Tags           Tags        `json:"tags"`             // 🔹 Теги потоку
    Disposition    Disposition `json:"disposition"`      // 🔹 Прапорці: default, forced, etc.
}
```

**🎯 Призначення**: Дозволити **детальний аналіз кожного потоку** для:
- ✅ Визначення основного відео/аудіо потоку
- ✅ Валідації сумісності кодеків
- ✅ Розрахунку параметрів транскодування

---

### 🔹 `Format` — метадані контейнера (повторення для контексту)

```go
type Format struct {
    Filename       string  `json:"filename"`         // 🔹 Шлях до файлу
    NbStreams      int     `json:"nb_streams"`       // 🔹 Загальна кількість потоків
    NbPrograms     int     `json:"nb_programs"`      // 🔹 Кількість програм (MPEG-TS)
    FormatName     string  `json:"format_name"`      // 🔹 "mov", "matroska", "mpegts"
    FormatLongName string  `json:"format_long_name"` // 🔹 "QuickTime / MOV"
    Duration       string  `json:"duration"`         // 🔹 Загальна тривалість
    Size           string  `json:"size"`             // 🔹 Розмір у байтах
    BitRate        string  `json:"bit_rate"`         // 🔹 Середній бітрейт файлу
    ProbeScore     int     `json:"probe_score"`      // 🔹 Оцінка достовірності: 0-100
    Tags           Tags    `json:"tags"`             // 🔹 Глобальні теги файлу
}
```

---

## 🔍 Приклад вихідних даних FFprobe

```json
{
  "streams": [
    {
      "index": 0,
      "codec_name": "h264",
      "codec_type": "video",
      "width": 1920,
      "height": 1080,
      "r_frame_rate": "30/1",
      "pix_fmt": "yuv420p",
      "bit_rate": "4500000",
      "nb_frames": "108000",
      "tags": {
        "handler_name": "VideoHandler"
      }
    },
    {
      "index": 1,
      "codec_name": "aac",
      "codec_type": "audio",
      "sample_rate": "48000",
      "channels": 2,
      "channel_layout": "stereo",
      "bit_rate": "128000",
      "tags": {
        "handler_name": "SoundHandler"
      }
    }
  ],
  "format": {
    "filename": "camera_20240115.mp4",
    "nb_streams": 2,
    "format_name": "mov,mp4,m4a,3gp,3g2,mj2",
    "format_long_name": "QuickTime / MOV",
    "duration": "3600.123456",
    "size": "2147483648",
    "bit_rate": "4768000",
    "probe_score": 100,
    "tags": {
      "ENCODER": "Lavf58.29.100",
      "creation_time": "2024-01-15T10:30:00.000000Z"
    }
  }
}
```

**🔄 Парсинг у Go:**
```go
var metadata media.Metadata
json.Unmarshal(ffprobeOutput, &metadata)

// 🔹 Доступ до формат-метаданих:
fmt.Printf("📁 %s | %s | %.1f min\n", 
    metadata.Format.Filename,
    metadata.Format.FormatName,
    parseFloat(metadata.Format.Duration)/60)

// 🔹 Ітерація по потоках:
for _, stream := range metadata.Streams {
    if stream.CodecType == "video" {
        fmt.Printf("🎬 Video: %dx%d @ %s, codec=%s\n",
            stream.Width, stream.Height,
            stream.RFrameRate, stream.CodecName)
    } else if stream.CodecType == "audio" {
        fmt.Printf("🔊 Audio: %s, %d ch, %s Hz\n",
            stream.CodecName, stream.Channels, stream.SampleRate)
    }
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Комплексна валідація запису камери

```go
// 🔹 Функція для повної перевірки придатності файлу
func validateRecording(metadata *media.Metadata) error {
    // 🔹 Перевірка формату контейнера
    supportedFormats := map[string]bool{
        "mov": true, "mp4": true, "ts": true, "mkv": true,
    }
    if !supportedFormats[metadata.Format.FormatName] {
        return fmt.Errorf("unsupported container: %s", metadata.Format.FormatName)
    }
    
    // 🔹 Пошук відео та аудіо потоків
    var videoStream, audioStream *media.Streams
    for i := range metadata.Streams {
        switch metadata.Streams[i].CodecType {
        case "video":
            if videoStream == nil {
                videoStream = &metadata.Streams[i]
            }
        case "audio":
            if audioStream == nil {
                audioStream = &metadata.Streams[i]
            }
        }
    }
    
    // 🔹 Обов'язковий відео-потік
    if videoStream == nil {
        return fmt.Errorf("no video stream found")
    }
    
    // 🔹 Валідація відео-параметрів
    if videoStream.Width < 640 || videoStream.Height < 360 {
        return fmt.Errorf("resolution too low: %dx%d", videoStream.Width, videoStream.Height)
    }
    if !isSupportedCodec(videoStream.CodecName) {
        return fmt.Errorf("unsupported video codec: %s", videoStream.CodecName)
    }
    
    // 🔹 Валідація аудіо (якщо є)
    if audioStream != nil {
        if audioStream.Channels == 0 || audioStream.SampleRate == "" {
            log.Printf("⚠️  Invalid audio stream parameters")
        }
    }
    
    // 🔹 Перевірка тривалості
    duration, _ := strconv.ParseFloat(metadata.Format.Duration, 64)
    if duration < 1 {
        return fmt.Errorf("duration too short: %.2fs", duration)
    }
    if duration > 86400 {  // 24 години
        log.Printf("⚠️  Very long recording: %.1f hours", duration/3600)
    }
    
    // 🔹 Перевірка достовірності
    if metadata.Format.ProbeScore < 50 {
        return fmt.Errorf("low probe score (%d): file may be corrupted", 
            metadata.Format.ProbeScore)
    }
    
    return nil
}

// 🔹 Допоміжна функція для перевірки кодеків
func isSupportedCodec(codec string) bool {
    supported := map[string]bool{
        "h264": true, "hevc": true, "vp9": true,  // 🔹 Відео
        "aac": true, "mp3": true, "opus": true,   // 🔹 Аудіо
    }
    return supported[codec]
}
```

---

### 🔹 Приклад 2: Адаптивний вибір параметрів кодування

```go
// 🔹 Функція для генерації конфігурації кодування на основі метаданих
func generateEncodingConfig(metadata *media.Metadata) (*media.File, error) {
    file := &media.File{}
    
    // 🔹 Базові налаштування
    file.SetInputPath(metadata.Format.Filename)
    
    // 🔹 Пошук основного відео-потоку
    var videoStream *media.Streams
    for i := range metadata.Streams {
        if metadata.Streams[i].CodecType == "video" {
            videoStream = &metadata.Streams[i]
            break
        }
    }
    if videoStream == nil {
        return nil, fmt.Errorf("no video stream found")
    }
    
    // 🔹 Адаптація роздільності
    if videoStream.Width > 1920 {
        file.SetResolution("1920x1080")  // 🔹 Downscale 4K → 1080p
    } else if videoStream.Width >= 1280 {
        file.SetResolution(fmt.Sprintf("%dx%d", videoStream.Width, videoStream.Height))
    } else {
        file.SetResolution("1280x720")  // 🔹 Upscale низької якості
    }
    
    // 🔹 Адаптація бітрейту
    inputBitrate, _ := strconv.ParseFloat(videoStream.BitRate, 64)
    if inputBitrate > 0 {
        // 🔹 Зберегти ~80% вхідного бітрейту для балансу якості/розміру
        targetBitrate := int(inputBitrate * 0.8 / 1000)
        file.SetVideoBitRate(fmt.Sprintf("%dk", targetBitrate))
    } else {
        file.SetVideoBitRate("2500k")  // 🔹 Дефолтне значення
    }
    
    // 🔹 Налаштування кодека
    switch videoStream.CodecName {
    case "hevc", "h265":
        file.SetVideoCodec("libx265")  // 🔹 Зберегти HEVC
    case "vp9":
        file.SetVideoCodec("libvpx-vp9")  // 🔹 Зберегти VP9
    default:
        file.SetVideoCodec("libx264")  // 🔹 Універсальний H.264
    }
    
    // 🔹 Налаштування частоти кадрів
    if fps := parseFrameRate(videoStream.RFrameRate); fps > 0 {
        file.SetFrameRate(fps)
    }
    
    // 🔹 Аудіо-налаштування
    for _, stream := range metadata.Streams {
        if stream.CodecType == "audio" {
            file.SetAudioCodec("aac")  // 🔹 Універсальний кодек
            file.SetAudioBitRate("128k")
            if stream.Channels > 0 {
                file.SetAudioChannels(stream.Channels)
            }
            if stream.SampleRate != "" {
                rate, _ := strconv.Atoi(stream.SampleRate)
                file.SetAudioRate(rate)
            }
            break
        }
    }
    
    // 🔹 HLS-специфічні налаштування
    file.SetOutputFormat("hls")
    file.SetHlsSegmentDuration(4)
    file.SetHlsListSize(10)
    file.SetHlsPlaylistType("vod")
    
    return file, nil
}

// 🔹 Допоміжна функція для парсингу частоти кадрів
func parseFrameRate(rateStr string) int {
    // 🔹 Обробка формату "30/1", "29.97", тощо
    parts := strings.Split(rateStr, "/")
    if len(parts) == 2 {
        num, _ := strconv.ParseFloat(parts[0], 64)
        den, _ := strconv.ParseFloat(parts[1], 64)
        if den > 0 {
            return int(num / den)
        }
    }
    fps, _ := strconv.ParseFloat(rateStr, 64)
    return int(fps)
}
```

---

### 🔹 Приклад 3: Генерація звіту для системи моніторингу

```go
// 🔹 Структура для звіту про оброблений запис
type ProcessingReport struct {
    FileID         string    `json:"file_id"`
    Filename       string    `json:"filename"`
    Format         string    `json:"format"`
    DurationSec    float64   `json:"duration_seconds"`
    SizeGB         float64   `json:"size_gb"`
    VideoCodec     string    `json:"video_codec"`
    VideoResolution string   `json:"video_resolution"`
    AudioCodec     string    `json:"audio_codec"`
    ProcessingTime time.Duration `json:"processing_time"`
    Status         string    `json:"status"`  // "success", "failed", "skipped"
    Error          string    `json:"error,omitempty"`
    Timestamp      time.Time `json:"processed_at"`
}

// 🔹 Функція для створення звіту з метаданих
func createProcessingReport(fileID string, metadata *media.Metadata, 
    procTime time.Duration, status string, err error) *ProcessingReport {
    
    report := &ProcessingReport{
        FileID:         fileID,
        Filename:       metadata.Format.Filename,
        Format:         metadata.Format.FormatName,
        ProcessingTime: procTime,
        Status:         status,
        Timestamp:      time.Now(),
    }
    
    // 🔹 Парсинг числових значень
    if duration, e := strconv.ParseFloat(metadata.Format.Duration, 64); e == nil {
        report.DurationSec = duration
    }
    if size, e := strconv.ParseInt(metadata.Format.Size, 10, 64); e == nil {
        report.SizeGB = float64(size) / 1e9
    }
    
    // 🔹 Пошук відео/аудіо інформації
    for _, stream := range metadata.Streams {
        if stream.CodecType == "video" && report.VideoCodec == "" {
            report.VideoCodec = stream.CodecName
            report.VideoResolution = fmt.Sprintf("%dx%d", stream.Width, stream.Height)
        }
        if stream.CodecType == "audio" && report.AudioCodec == "" {
            report.AudioCodec = stream.CodecName
        }
    }
    
    // 🔹 Обробка помилок
    if err != nil {
        report.Error = err.Error()
    }
    
    return report
}

// 🔹 Використання у конвеєрі обробки:
func processRecording(fileID, inputPath string) error {
    startTime := time.Now()
    
    // 🔹 Отримання метаданих
    metadata, err := probeFile(inputPath)
    if err != nil {
        report := createProcessingReport(fileID, &media.Metadata{
            Format: media.Format{Filename: inputPath},
        }, time.Since(startTime), "failed", err)
        return sendReport(report)
    }
    
    // 🔹 Валідація
    if err := validateRecording(metadata); err != nil {
        report := createProcessingReport(fileID, metadata, 
            time.Since(startTime), "failed", err)
        return sendReport(report)
    }
    
    // 🔹 Кодування
    config, err := generateEncodingConfig(metadata)
    if err != nil {
        report := createProcessingReport(fileID, metadata, 
            time.Since(startTime), "failed", err)
        return sendReport(report)
    }
    
    if err := runFFmpeg(config); err != nil {
        report := createProcessingReport(fileID, metadata, 
            time.Since(startTime), "failed", err)
        return sendReport(report)
    }
    
    // 🔹 Успіх
    report := createProcessingReport(fileID, metadata, 
        time.Since(startTime), "success", nil)
    return sendReport(report)
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Парсинг числових полів як `int` замість `string` | `json: cannot unmarshal number into Go struct field` | Залишайте поля як `string` у структурі, парсіть при використанні |
| Ігнорування `NbStreams` у `Format` | Невідповідність між заявленою та фактичною кількістю потоків | Порівнюйте `Format.NbStreams` з `len(Streams)` для валідації |
| Неправильна обробка `r_frame_rate` | Частота кадрів "30/1" парситься як 30.0 замість 30 | Використовуйте спеціальну функцію парсингу дробових значень |
| Відсутність перевірки `CodecType` | Спроба отримати `Width` з аудіо-потоку → 0 | Завжди перевіряйте `stream.CodecType == "video"` перед доступом до відео-полів |
| Ігнорування `ProbeScore` | Обробка пошкоджених файлів без попередження | Перевіряйте `ProbeScore < 50` для виявлення проблемних файлів |

---

## 📋 Чекліст для вашого проекту

```
[ ] При парсингу метаданих:
    • Залишайте числові поля як string у структурі (сумісність з FFprobe JSON)
    • Парсіть значення при використанні з обробкою помилок
    • Перевіряйте ProbeScore для оцінки достовірності детекції

[ ] Для валідації файлів:
    • Шукайте обов'язковий відео-потік (CodecType="video")
    • Валідуйте роздільність, кодек, частоту кадрів проти політик
    • Перевіряйте узгодженість Format.NbStreams та len(Streams)

[ ] Для адаптивного кодування:
    • Використовуйте параметри вхідного відео для розрахунку цільових
    • Адаптуйте роздільність та бітрейт на основі джерела
    • Зберігайте аудіо-параметри або конвертуйте у стандартні

[ ] Для логування та аудиту:
    • Конвертуйте одиниці у зручний формат: секунди → хвилини, байти → ГБ
    • Додавайте мітку часу та ID файлу для трасування
    • Зберігайте повний звіт у JSON для подальшого аналізу

[ ] Для дебагу:
    • Логувайте сирі значення перед парсингом
    • Перевіряйте помилки парсингу для кожного поля
    • Тестуйте з різними форматами: MP4, MKV, TS, AVI, FLV

[ ] Для тестування:
    • Створюйте тестові JSON-відповіді з різними конфігураціями потоків
    • Перевіряйте обробку крайніх випадків: порожні масиви, відсутні поля
    • Тестуйте помилкові сценарії: невалідний JSON, пошкоджені метадані
```

---

## 🎯 Висновок

> **`media.Metadata` — це "єдине джерело правди" про медіа-файл**, який забезпечує:
> • ✅ Комплексне представлення як потоків, так і контейнера у одному об'єкті
> • ✅ Типобезпечний доступ до всіх метаданих з виходу FFprobe
> • ✅ Інтеграцію з логікою валідації, кодування та звітності
> • ✅ Розширюваність: легке додавання нових полів без зміни основної логіки
> • ✅ Сумісність: пряме відображення структури JSON FFprobe у Go-типи

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Повна валідація вхідних записів перед додаванням у конвеєр обробки
- 🎯 Розумний вибір параметрів кодування на основі реальних характеристик файлу
- 📊 Детальне логування та звітність для аудиту та аналітики
- 🛡️ Захист від пошкоджених або невалідних файлів через комплексну перевірку
- 🔄 Легке масштабування: підтримка нових форматів та кодеків через розширення структури

Потребуєте допомоги з інтеграцією парсингу повних метаданих у ваш конвеєр або з налаштуванням адаптивної логіки кодування на основі `media.Metadata`? Напишіть — покажу готовий код для вашого сценарію! 🚀📦