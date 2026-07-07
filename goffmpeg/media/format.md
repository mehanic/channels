# 📋 `media.Format`: Структура метаданих формату медіа-файлу

Це **допоміжна структура** бібліотеки для роботи з медіа-файлами, яка представляє **формат-рівневі метадані** з виходу FFprobe/FFmpeg у форматі JSON — фундамент для аналізу властивостей файлів перед кодуванням.

---

## 🎯 Коротка відповідь

> **Це "паспорт файлу"**: він містить базову інформацію про медіа-контейнер (формат, тривалість, розмір, бітрейт, теги) — ідеально для валідації вхідних файлів, логування та прийняття рішень про параметри кодування.

---

## 🧱 Основні структури

### 🔹 `Format` — метадані контейнера

```go
type Format struct {
    Filename       string  // 🔹 Ім'я файлу (шлях)
    NbStreams      int     `json:"nb_streams"`       // 🔹 Кількість потоків (відео+аудіо+субтитри)
    NbPrograms     int     `json:"nb_programs"`      // 🔹 Кількість програм (для MPEG-TS)
    FormatName     string  `json:"format_name"`      // 🔹 Коротка назва формату: "mov", "matroska", "mpegts"
    FormatLongName string  `json:"format_long_name"` // 🔹 Повна назва: "QuickTime / MOV", "Matroska / WebM"
    Duration       string  `json:"duration"`         // 🔹 Тривалість у секундах: "123.456000"
    Size           string  `json:"size"`             // 🔹 Розмір файлу у байтах: "1048576"
    BitRate        string  `json:"bit_rate"`         // 🔹 Середній бітрейт: "5000000" (5 Mbps)
    ProbeScore     int     `json:"probe_score"`      // 🔹 Оцінка достовірності детекції: 0-100
    Tags           Tags    `json:"tags"`             // 🔹 Метадані-теги (енкодер, назва, тощо)
}
```

**🎯 Призначення**: Інкапсулювати **формат-специфічну інформацію** для:
- ✅ Валідації вхідних файлів перед кодуванням
- ✅ Логування властивостей записів
- ✅ Прийняття рішень про параметри транскодування
- ✅ Генерації звітів про медіа-бібліотеку

---

### 🔹 `Tags` — ключ-значення метадані

```go
type Tags struct {
    Encoder string `json:"ENCODER"`  // 🔹 Назва енкодера: "Lavf58.29.100", "HandBrake 1.3.3"
}
```

**🎯 Призначення**: Представити **довільні текстові метадані** у типобезпечний спосіб.

**🔢 Розширений приклад (гіпотетичний):**
```go
type Tags struct {
    Encoder      string `json:"ENCODER"`        // "Lavf58.29.100"
    Title        string `json:"title"`          // "Camera 1 Recording"
    Artist       string `json:"artist"`         // "Security System"
    CreationTime string `json:"creation_time"`  // "2024-01-15T10:30:00.000000Z"
    Location     string `json:"location"`       // "Building A, Entrance"
}
```

**🎯 Призначення**: Дозволити доступ до **стандартних та кастомних тегів** без парсингу сирих JSON-полів.

---

## 🔍 Приклад вихідних даних FFprobe

```json
{
  "format": {
    "filename": "camera_20240115_103000.mp4",
    "nb_streams": 2,
    "nb_programs": 0,
    "format_name": "mov,mp4,m4a,3gp,3g2,mj2",
    "format_long_name": "QuickTime / MOV",
    "duration": "3600.123456",
    "size": "2147483648",
    "bit_rate": "4768000",
    "probe_score": 100,
    "tags": {
      "ENCODER": "Lavf58.29.100",
      "creation_time": "2024-01-15T10:30:00.000000Z",
      "location": "Building A, Floor 3"
    }
  }
}
```

**🔄 Парсинг у Go:**
```go
var result struct {
    Format media.Format `json:"format"`
}
json.Unmarshal(ffprobeOutput, &result)

// 🔹 Доступ до полів:
fmt.Printf("📁 File: %s\n", result.Format.Filename)
fmt.Printf("🎬 Format: %s (%s)\n", result.Format.FormatName, result.Format.FormatLongName)
fmt.Printf("⏱️  Duration: %s seconds\n", result.Format.Duration)
fmt.Printf("💾 Size: %s bytes (%.2f GB)\n", result.Format.Size, 
    parseFloat(result.Format.Size) / 1e9)
fmt.Printf("📡 Bitrate: %s bps (%.2f Mbps)\n", result.Format.BitRate,
    parseFloat(result.Format.BitRate) / 1e6)
fmt.Printf("🔍 Probe Score: %d/100\n", result.Format.ProbeScore)
fmt.Printf("🏷️  Encoder: %s\n", result.Format.Tags.Encoder)
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Валідація вхідного файлу перед кодуванням

```go
// 🔹 Функція для перевірки придатності файлу для обробки
func validateInputFile(format *media.Format) error {
    // 🔹 Перевірка підтримуваних форматів
    supportedFormats := map[string]bool{
        "mov": true, "mp4": true, "avi": true, "mkv": true, "ts": true,
    }
    if !supportedFormats[format.FormatName] {
        return fmt.Errorf("unsupported format: %s", format.FormatName)
    }
    
    // 🔹 Перевірка наявності потоків
    if format.NbStreams == 0 {
        return fmt.Errorf("no streams found in file")
    }
    
    // 🔹 Перевірка тривалості
    duration, err := strconv.ParseFloat(format.Duration, 64)
    if err != nil || duration <= 0 {
        return fmt.Errorf("invalid duration: %s", format.Duration)
    }
    if duration > 86400 {  // 🔹 Максимум 24 години
        log.Printf("⚠️  Very long recording: %.1f hours", duration/3600)
    }
    
    // 🔹 Перевірка розміру
    size, err := strconv.ParseInt(format.Size, 10, 64)
    if err != nil {
        return fmt.Errorf("invalid file size: %s", format.Size)
    }
    if size > 10*1024*1024*1024 {  // 🔹 Максимум 10 ГБ
        return fmt.Errorf("file too large: %.2f GB > 10 GB", float64(size)/1e9)
    }
    
    // 🔹 Перевірка достовірності детекції
    if format.ProbeScore < 50 {
        log.Printf("⚠️  Low probe score (%d): file may be corrupted", format.ProbeScore)
    }
    
    return nil
}

// 🔹 Використання:
format := getFormatFromFFprobe("recording.mp4")
if err := validateInputFile(&format); err != nil {
    log.Printf("❌ Invalid file: %v", err)
    return err
}
log.Printf("✅ File validated: %s, %.1f min, %.2f GB", 
    format.Filename, 
    parseFloat(format.Duration)/60,
    parseFloat(format.Size)/1e9)
```

---

### 🔹 Приклад 2: Логування метаданих для аудиту

```go
// 🔹 Структура для логування властивостей запису
type RecordingLog struct {
    Filename   string    `json:"filename"`
    Format     string    `json:"format"`
    Duration   float64   `json:"duration_seconds"`
    SizeGB     float64   `json:"size_gb"`
    BitrateMbps float64  `json:"bitrate_mbps"`
    Encoder    string    `json:"encoder"`
    Timestamp  time.Time `json:"processed_at"`
}

// 🔹 Функція для створення логу з формату
func createRecordingLog(format *media.Format) (*RecordingLog, error) {
    duration, err := strconv.ParseFloat(format.Duration, 64)
    if err != nil {
        return nil, fmt.Errorf("invalid duration: %w", err)
    }
    
    size, err := strconv.ParseInt(format.Size, 10, 64)
    if err != nil {
        return nil, fmt.Errorf("invalid size: %w", err)
    }
    
    bitrate, err := strconv.ParseFloat(format.BitRate, 64)
    if err != nil {
        return nil, fmt.Errorf("invalid bitrate: %w", err)
    }
    
    return &RecordingLog{
        Filename:    format.Filename,
        Format:      format.FormatName,
        Duration:    duration,
        SizeGB:      float64(size) / 1e9,
        BitrateMbps: bitrate / 1e6,
        Encoder:     format.Tags.Encoder,
        Timestamp:   time.Now(),
    }, nil
}

// 🔹 Використання для аудиту:
logEntry, err := createRecordingLog(&format)
if err != nil {
    log.Printf("❌ Failed to create log: %v", err)
} else {
    // 🔹 Запис у базу даних або файл
    logJSON, _ := json.Marshal(logEntry)
    auditLog.Write(append(logJSON, '\n'))
    
    // 🔹 Консольний вивід
    log.Printf("📊 %s | %s | %.1f min | %.2f GB | %.1f Mbps | %s",
        logEntry.Filename,
        logEntry.Format,
        logEntry.Duration/60,
        logEntry.SizeGB,
        logEntry.BitrateMbps,
        logEntry.Encoder)
}
```

---

### 🔹 Приклад 3: Автоматичний вибір параметрів кодування на основі формату

```go
// 🔹 Функція для адаптивного налаштування кодування
func suggestEncodingParams(format *media.Format) (*media.File, error) {
    file := &media.File{}
    
    // 🔹 Базові налаштування
    file.SetInputPath(format.Filename)
    
    // 🔹 Адаптація бітрейту на основі вхідного
    inputBitrate, err := strconv.ParseFloat(format.BitRate, 64)
    if err == nil && inputBitrate > 0 {
        // 🔹 Зменшити бітрейт на 20% для економії місця
        targetBitrate := int(inputBitrate * 0.8 / 1000)  // у kbps
        file.SetVideoBitRate(fmt.Sprintf("%dk", targetBitrate))
    } else {
        // 🔹 Дефолтне значення
        file.SetVideoBitRate("2500k")
    }
    
    // 🔹 Адаптація роздільності на основі формату
    switch format.FormatName {
    case "mpegts", "ts":
        // 🔹 TS часто має 1080p → зменшити до 720p для економії
        file.SetResolution("1280x720")
    case "avi", "wmv":
        // 🔹 Старі формати → стандартна якість
        file.SetResolution("854x480")
    default:
        // 🔹 Сучасні формати → зберегти оригінал
        file.SetResolution("1920x1080")
    }
    
    // 🔹 Налаштування кодека
    if strings.Contains(format.FormatLongName, "QuickTime") {
        file.SetVideoCodec("libx264")  // ✅ Найкраща сумісність
    } else {
        file.SetVideoCodec("libx264")  // ✅ Універсальний вибір
    }
    
    // 🔹 Налаштування тривалості для обрізки
    duration, _ := strconv.ParseFloat(format.Duration, 64)
    if duration > 7200 {  // 🔹 > 2 години → обрізати до 2 годин
        file.SetDuration("7200")
        log.Printf("⚠️  Long recording truncated to 2 hours")
    }
    
    return file, nil
}

// 🔹 Використання у конвеєрі:
format := probeFile("camera_recording.mp4")
encodingConfig, err := suggestEncodingParams(&format)
if err != nil {
    log.Printf("❌ Failed to suggest params: %v", err)
} else {
    // 🔹 Запуск кодування з рекомендованими параметрами
    runFFmpeg(encodingConfig)
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Парсинг `Duration`/`Size` як `int` замість `string` | Помилка `json: cannot unmarshal number into Go struct field` | Залишайте поля як `string` у структурі, парсіть при використанні |
| Ігнорування `ProbeScore` | Обробка пошкоджених файлів без попередження | Перевіряйте `ProbeScore < 50` для виявлення проблемних файлів |
| Неправильна конвертація одиниць | "5000000" сприймається як 5 байт замість 5 Mbps | Завжди діліть на 1000 для kbps, на 1e6 для Mbps |
| Відсутність обробки порожніх тегів | `Tags.Encoder` порожній → помилки у логіці | Перевіряйте `if format.Tags.Encoder != ""` перед використанням |
| Ігнорування `NbStreams` | Спроба кодувати файл без відео/аудіо потоків | Перевіряйте `NbStreams > 0` та аналізуйте потоки окремо |

---

## 📋 Чекліст для вашого проекту

```
[ ] При парсингу метаданих:
    • Залишайте числові поля як string у структурі (як у FFprobe JSON)
    • Парсіть значення при використанні з обробкою помилок
    • Перевіряйте ProbeScore для оцінки достовірності детекції

[ ] Для валідації файлів:
    • Перевіряйте підтримку формату через FormatName
    • Валідуйте тривалість та розмір проти політик зберігання
    • Логувайте попередження для файлів з низьким ProbeScore

[ ] Для логування та аудиту:
    • Конвертуйте одиниці у зручний формат: секунди → хвилини, байти → ГБ
    • Додавайте мітку часу обробки для трасування
    • Зберігайте JSON-лог для подальшого аналізу

[ ] Для адаптивного кодування:
    • Використовуйте вхідний бітрейт для розрахунку цільового
    • Адаптуйте роздільність на основі формату та джерела
    • Обрізайте надто довгі записи відповідно до політик

[ ] Для дебагу:
    • Логувайте сирі значення перед парсингом: log.Printf("🔢 Duration raw: %q", format.Duration)
    • Перевіряйте помилки парсингу: if err != nil { log.Printf("❌ Parse error: %v", err) }
    • Тестуйте з різними форматами: MP4, MKV, TS, AVI

[ ] Для тестування:
    • Створюйте тестові JSON-відповіді з різними значеннями полів
    • Перевіряйте обробку крайніх випадків: порожні рядки, від'ємні значення, дуже великі числа
    • Тестуйте помилкові сценарії: невалідний JSON, відсутні обов'язкові поля
```

---

## 🎯 Висновок

> **`media.Format` — це "інтерфейс до метаданих"**, який забезпечує:
> • ✅ Типобезпечний доступ до формат-рівневих метаданих з FFprobe
> • ✅ Зручне представлення числових значень як string для сумісності з JSON
> • ✅ Розширюваність через теги для кастомних метаданих
> • ✅ Інтеграцію з логікою валідації, логування та адаптивного кодування
> • ✅ Простоту використання: мінімум полів, максимум корисної інформації

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Миттєва валідація вхідних записів перед додаванням у конвеєр обробки
- 📊 Автоматичне логування властивостей файлів для аудиту та аналітики
- 🎯 Розумний вибір параметрів кодування на основі вхідних метаданих
- 🛡️ Захист від пошкоджених або невалідних файлів через ProbeScore та валідацію
- 🔄 Легке масштабування: додавання нових полів тегів без зміни основної логіки

Потребуєте допомоги з інтеграцією парсингу метаданих у ваш конвеєр валідації або з налаштуванням адаптивного вибору параметрів кодування? Напишіть — покажу готовий код для вашого сценарію! 🚀📋