# Глибоке роз'яснення: `converter/subtitle.go` — асинхронна конвертація субтитрів для HLS

Цей файл містить **логіку асинхронної обробки субтитрів** — витягування, конвертацію у WebVTT та сегментацію для HLS. Це критичний компонент для підтримки багатомовності та доступності у вашому CCTV HLS пайплайні.

---

## 🎯 Навіщо цей код потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ Субтитри у контексті HLS-стрімінгу:    │
│                                         │
│ 🔹 Підтримка багатомовності:            │
│   • Конвертація вбудованих субтитрів   │
│     (DVB, Teletext, SRT) → WebVTT      │
│   • Додавання перекладених доріжок     │
│   • EXT-X-MEDIA інтеграція у master    │
│                                         │
│ 🔹 Доступність:                         │
│   • Субтитри для слабозорих/глухих     │
│   • Автоматична генерація через Whisper│
│   • Синхронізація з аудіо/відео        │
│                                         │
│ 🔹 Для CCTV HLS:                        │
│   • Асинхронна обробка → не блокує     │
│     основний енкодинг відео            │
│   • Паралельна конвертація кількох     │
│     мов для одного каналу              │
│   • Сегментація WebVTT під 6-секундні  │
│     сегменти HLS                       │
└─────────────────────────────────────────┘
```

---

## 🔧 Структури даних: архітектура обробки

### `subtitleConversionCommand`: контейнер для процесу конвертації

```go
type subtitleConversionCommand struct {
    EncoderCommand  *exec.Cmd   // 🎯 FFmpeg процес для витягування субтитрів
    OutputDir       string      // 🎯 Каталог для вихідних WebVTT файлів
    Name            string      // 🎯 Ідентифікатор субтитрів (напр., "ukr", "eng")
    Logfile         *os.File    // 🎯 Файл логу (або nil для stderr)
}
```

### `SubtitleVariantConversion`: публічний інтерфейс для керування

```go
type SubtitleVariantConversion struct {
    Variant  suggest.SubtitleVariant      // 🎯 Конфігурація варіанту
    commands *subtitleConversionCommand   // 🎯 Внутрішній процес (приватний)
}
```

> 💡 **Архітектурне рішення**: Розділення публічного (`SubtitleVariantConversion`) та приватного (`subtitleConversionCommand`) інтерфейсів дозволяє інкапсуляцію — клієнтський код працює тільки з конфігурацією, не маніпулюючи процесами напряму.

---

## 🔍 Метод `start()`: асинхронний пайплайн конвертації

```go
func (sCmds subtitleConversionCommand) start() error {
    // 🔹 1. Налаштування логування
    if sCmds.Logfile != nil {
        sCmds.EncoderCommand.Stderr = sCmds.Logfile  // у файл
    } else {
        sCmds.EncoderCommand.Stderr = os.Stderr      // у консоль
    }
    
    // 🔹 2. Створення pipe для stdout → WebVTT сегментер
    webvttPipe, err := sCmds.EncoderCommand.StdoutPipe()
    if err != nil { return err }
    
    // 🔹 3. Debug: вивід команди
    fmt.Println("\nDEBUG: FFMPEG Subtitle command:\n \"" + 
        strings.Join(sCmds.EncoderCommand.Args, "\" \""))
    
    // 🔹 4. Запуск сегментера у окремій горутині
    // 🎯 Ключовий момент: webvtt.Segment читає з pipe та пише файли
    go webvtt.Segment(webvttPipe, 6*time.Second, sCmds.OutputDir, sCmds.Name)
    
    // 🔹 5. Запуск FFmpeg процесу
    err = sCmds.EncoderCommand.Start()
    if err != nil { return err }
    
    // 🔹 6. Дублювання перевірки помилки (можливо, зайве)
    if err != nil { return err }  // ⚠️ Цей блок ніколи не виконається
    
    return nil
}
```

### 🎯 Ключовий момент: `webvtt.Segment` у горутині

```
Проблема:
• FFmpeg виводить WebVTT у stdout як потік
• Потрібно розбити цей потік на 6-секундні сегменти для HLS
• Блокуюча обробка зупинила б весь пайплайн

Рішення:
• Запустити webvtt.Segment() у окремій горутині
• FFmpeg пише у pipe → горутина читає → пише файли

Архітектура:
[FFmpeg] --stdout(pipe)--> [webvtt.Segment горутина] --файли--> [outputDir]
                                ↓
                        Розбиває на сегменти по 6 секунд
                        Іменує: {Name}_00001.vtt, {Name}_00002.vtt...

Переваги:
✅ Повна асинхронність: основний потік не блокується
✅ Масштабованість: кожен варіант субтитрів у власній горутині
✅ Ізоляція помилок: збій у сегментері не зупиняє FFmpeg
```

> ⚠️ **BUG виявлено**: У коді є дублювання перевірки помилки:
> ```go
> err = sCmds.EncoderCommand.Start()
> if err != nil { return err }
> 
> if err != nil { return err }  // ❌ Цей блок недосяжний
> ```
> Другу перевірку можна видалити.

---

## 🔍 Функція `callSubtitleConversions`: оркестрація багатьох варіантів

```go
func callSubtitleConversions(variants []suggest.SubtitleVariant, outputDir string) []SubtitleVariantConversion {
    var conversions []SubtitleVariantConversion
    
    for _, v := range variants {
        // 🔹 1. Підготувати команду для цього варіанту
        cmds := convertSubtitle(v, outputDir)
        
        // 🔹 2. Запустити асинхронно
        err := cmds.start()
        if err != nil {
            log.Println("Cannot convert subtitle variant", v.Name, "\nError:", err)
            continue  // 🔹 Пропустити помилковий варіант, продовжити інші
        }
        
        // 🔹 3. Зберегти посилання для подальшого керування
        conversions = append(conversions, SubtitleVariantConversion{
            Variant:  v,
            commands: &cmds,
        })
    }
    
    return conversions
}
```

### 🎯 Стратегія "continue on error"

```
Чому не зупиняти весь процес при помилці одного варіанту?

Сценарій:
• Канал має 3 мови субтитрів: ukr, eng, rus
• Конвертація "rus" не вдається (немає потоку з цією мовою)

Якби ми повертали помилку:
❌ Всі 3 мови не будуть додані до плейлиста
❌ Користувачі втратять доступ до ukr/eng субтитрів

З "continue":
✅ ukr та eng успішно конвертовані та додані
✅ Помилка rus залогрована для відладки
✅ Користувачі отримують максимально можливу функціональність

Це приклад "graceful degradation" — система деградує граційно, не ламаючись повністю.
```

---

## 🔍 Функція `convertSubtitle`: побудова FFmpeg команди

```go
func convertSubtitle(variant suggest.SubtitleVariant, outputDir string) subtitleConversionCommand {
    // 🔹 1. Базові аргументи
    args := ffmpegDefaultArguments()  // ["-hide_banner", "-y", "-stats", "-loglevel", "warning"]
    
    // 🔹 2. Вхідний потік/файл
    args = append(args, "-i", variant.InputURL)
    
    // 🔹 3. Параметри витягування субтитрів
    args = append(args,
        "-map", fmt.Sprintf("0:%d", variant.StreamIndex),  // Який потік витягувати
        "-c:s:0", "webvtt",   // Кодек: конвертувати у WebVTT
        "-f", "webvtt",       // Формат виводу: WebVTT
        "-")                  // Вивід у stdout (pipe), не у файл
    
    // 🔹 4. Створити процес
    encode := exec.Command("ffmpeg", args...)
    
    // 🔹 5. Підготувати лог-файл
    logFilename := filepath.Join(outputDir, fmt.Sprintf("conversion-%s.log", variant.Name))
    logFile, err := os.Create(logFilename)
    if err != nil {
        log.Println("Cannot create logfile for subtitle conversion command:", err)
        logFile = nil  // 🔹 Fallback: логувати у stderr
    }
    
    // 🔹 6. Повернути структуру команди
    return subtitleConversionCommand{
        EncoderCommand: encode,
        OutputDir:      outputDir,
        Name:           variant.Name,
        Logfile:        logFile,
    }
}
```

### 🎯 Ключові параметри FFmpeg для субтитрів

| Параметр | Значення | Призначення |
|----------|----------|-------------|
| `-map 0:N` | `0:3`, `0:5`... | 🔹 Вибрати потік субтитрів за індексом з вхідного файлу |
| `-c:s:0 webvtt` | — | 🔹 Конвертувати субтитри у формат WebVTT |
| `-f webvtt` | — | 🔹 Вказати формат виводу (обов'язково для pipe) |
| `-` (останній аргумент) | — | 🔹 Вивід у stdout замість файлу (для pipe у сегментер) |

> 💡 **Важливо**: Вивід у `-` (stdout) дозволяє передавати дані напряму у `webvtt.Segment` без проміжних файлів — це економить дисковий простір та прискорює обробку.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Запуск конвертації субтитрів у `LaunchConversion`

```go
// У converter/converter.go — асинхронна обробка:
func LaunchConversion(..., subtitleVariantsCh <-chan []suggest.SubtitleVariant, ...) {
    // ... основна конвертація відео/аудіо ...
    
    // 🔹 Отримати варіанти субтитрів (блокує, доки не надійдуть дані)
    subtitleVariants := <-subtitleVariantsCh
    
    // 🔹 Запустити конвертацію асинхронно
    convertedSubtitles := callSubtitleConversions(subtitleVariants, outputDir)
    
    // 🔹 Додати субтитри у master playlist
    for _, c := range convertedSubtitles {
        f.WriteString(c.Variant.Stanza() + "\n")
    }
    
    // ... решта логіки ...
}
```

### ✅ 2: Генерація `suggest.SubtitleVariant` з метаданих каналу

```go
// У channel-менеджері — визначення доступних мов субтитрів:
func generateSubtitleVariants(channelID string, metadata *ChannelMetadata) []suggest.SubtitleVariant {
    var variants []suggest.SubtitleVariant
    
    // 🔹 Отримати список мов з метаданих (напр., з SDT/EIT дескрипторів)
    for _, lang := range metadata.SubtitleLanguages {
        variants = append(variants, suggest.SubtitleVariant{
            InputURL:    metadata.InputURL,      // той самий вхідний потік
            StreamIndex: metadata.SubtitlePIDs[lang],  // PID субтитрів для цієї мови
            Name:        lang,                   // код мови: "ukr", "eng"...
            Language:    lang,                   // для EXT-X-MEDIA
            GroupID:     &suggest.DefaultSubtitlesGroupID,
            Default:     lang == metadata.DefaultLanguage,
            AutoSelect:  true,
        })
    }
    
    return variants
}
```

### ✅ 3: Обробка автоматичних субтитрів через Whisper

```go
// Для каналів без вбудованих субтитрів — генерація через Whisper:
func generateAutoSubtitles(channelID string, audioStreamURL string, language string) suggest.SubtitleVariant {
    // 🔹 Запустити Whisper асинхронно
    whisperCh := make(chan string, 1)  // канал для готового SRT/WebVTT
    go func() {
        srt, err := whisper.Transcribe(audioStreamURL, language)
        if err != nil {
            log.Errorf("Channel %s: Whisper transcription failed: %v", channelID, err)
            whisperCh <- ""
            return
        }
        whisperCh <- srt
    }()
    
    // 🔹 Повернути варіант, який читатиме з каналу
    return suggest.SubtitleVariant{
        InputURL:    "pipe:0",  // спеціальний URL для читання з stdin
        StreamIndex: 0,         // не використовується для pipe
        Name:        fmt.Sprintf("%s_auto", language),
        Language:    language,
        // 🔹 Додаткові поля для обробки pipe...
    }
}
```

### ✅ 4: Моніторинг конвертації субтитрів

```go
// monitoring.Monitor — метрики для субтитрів:
type SubtitleMetrics struct {
    SubtitleConversionsStarted *prometheus.CounterVec  // кількість запущених конвертацій
    SubtitleConversionsCompleted *prometheus.CounterVec // кількість успішних
    SubtitleConversionsFailed *prometheus.CounterVec   // кількість помилок
    SubtitleLanguagesGauge *prometheus.GaugeVec       // кількість мов на канал
    SubtitleSegmentCount *prometheus.CounterVec       // кількість згенерованих сегментів
}

// У процесі конвертації:
func monitorSubtitleConversion(channelID string, variants []suggest.SubtitleVariant, 
                            metrics *SubtitleMetrics) {
    
    metrics.SubtitleConversionsStarted.WithLabelValues(channelID).Add(float64(len(variants)))
    metrics.SubtitleLanguagesGauge.WithLabelValues(channelID).Set(float64(len(variants)))
    
    // 🔹 Горутина для очікування завершення кожного варіанту
    for _, v := range variants {
        go func(lang string) {
            // Періодично перевіряти статус (спрощено)
            // У реальності: інтегрувати з webvtt.Segment для підрахунку сегментів
            time.Sleep(30 * time.Second)  // припустимо, конвертація завершилась
            metrics.SubtitleConversionsCompleted.WithLabelValues(channelID).Inc()
        }(v.Name)
    }
}
```

### ✅ 5: Graceful shutdown для субтитрів

```go
// У Conversion.Exit() — коректна зупинка субтитрів:
func (c Conversion) Exit() {
    c.do(func(cmd *exec.Cmd) {
        if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
            // 🔹 Спочатку спробувати SIGTERM для коректного завершення
            cmd.Process.Signal(syscall.SIGTERM)
            
            // 🔹 Дати час на завершення (напр., 5 секунд)
            done := make(chan bool, 1)
            go func() {
                cmd.Wait()
                done <- true
            }()
            
            select {
            case <-done:
                // ✅ Завершилось коректно
            case <-time.After(5 * time.Second):
                // ❌ Примусово вбити
                cmd.Process.Kill()
            }
        }
    })
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Інтеграційний тест на конвертацію субтитрів

```go
func TestCallSubtitleConversions_Integration(t *testing.T) {
    // 🔹 Підготувати тестові дані
    tempDir := t.TempDir()
    inputFile := createTestInputWithSubtitles(t)  // ваша helper-функція
    
    variants := []suggest.SubtitleVariant{
        {
            InputURL:    inputFile,
            StreamIndex: 3,  // індекс потоку субтитрів
            Name:        "ukr",
            Language:    "ukr",
        },
        {
            InputURL:    inputFile,
            StreamIndex: 4,
            Name:        "eng",
            Language:    "eng",
        },
    }
    
    // 🔹 Запустити конвертацію
    conversions := callSubtitleConversions(variants, tempDir)
    assert.Len(t, conversions, 2)
    
    // 🔹 Дочекатися завершення (або таймаут)
    done := make(chan bool, 1)
    go func() {
        // Періодично перевіряти, чи створено сегменти
        for i := 0; i < 30; i++ {  // 30 секунд таймаут
            files, _ := filepath.Glob(filepath.Join(tempDir, "ukr_*.vtt"))
            if len(files) >= 2 {  // хоча б 2 сегменти
                done <- true
                return
            }
            time.Sleep(1 * time.Second)
        }
        done <- false
    }()
    
    select {
    case success := <-done:
        assert.True(t, success, "Subtitle conversion timed out")
        
        // 🔹 Перевірити вихідні файли
        ukrFiles, _ := filepath.Glob(filepath.Join(tempDir, "ukr_*.vtt"))
        assert.Greater(t, len(ukrFiles), 0)
        
        // 🔹 Перевірити формат WebVTT
        content, _ := os.ReadFile(ukrFiles[0])
        assert.Contains(t, string(content), "WEBVTT")
        assert.Contains(t, string(content), "00:00:00.000")  // таймкоди
        
    case <-time.After(30 * time.Second):
        t.Fatal("Subtitle conversion timed out")
    }
    
    // 🔹 Прибрати процеси
    for _, conv := range conversions {
        if conv.commands.EncoderCommand.ProcessState == nil {
            conv.commands.EncoderCommand.Process.Kill()
        }
    }
}
```

### 🔹 Тест на обробку помилок

```go
func TestCallSubtitleConversions_InvalidStreamIndex(t *testing.T) {
    tempDir := t.TempDir()
    inputFile := createTestInputWithSubtitles(t)
    
    // 🔹 Невірний індекс потоку (не існує)
    variants := []suggest.SubtitleVariant{
        {
            InputURL:    inputFile,
            StreamIndex: 99,  // ❌ не існує
            Name:        "invalid",
        },
    }
    
    // 🔹 Запустити конвертацію
    conversions := callSubtitleConversions(variants, tempDir)
    
    // 🔹 Очікуємо: помилковий варіант пропущено, conversions порожній
    assert.Empty(t, conversions)
    
    // 🔹 Перевірити, що помилка залогрована
    // (у реальності: mock log.Logger для перевірки)
}
```

### 🔹 Тест на асинхронність

```go
func TestSubtitleConversion_AsyncBehavior(t *testing.T) {
    // 🔹 Перевірити, що start() не блокує викликаючий потік
    cmds := createTestSubtitleCommand(t)
    
    startTime := time.Now()
    err := cmds.start()
    elapsed := time.Since(startTime)
    
    assert.NoError(t, err)
    // 🔹 start() має повернутися майже миттєво (< 100ms)
    //    а не чекати завершення конвертації
    assert.Less(t, elapsed, 100*time.Millisecond)
    
    // 🔹 Дочекатися реального завершення для очищення
    cmds.EncoderCommand.Wait()
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `if err != nil` дублювання | Зайвий код, можлива плутанина | 🔹 Видалити другий блок перевірки помилки після `Start()` |
| Формат імені лог-файлу | `"conversion-%s.log"` замість `"conversion-%s.log"` | 🔹 Виправити: `fmt.Sprintf("conversion-%s.log", variant.Name)` |
| Pipe не закривається | Горутина `webvtt.Segment` зависає | 🔹 Переконатися, що FFmpeg закриває stdout при завершенні; додати `cmd.Wait()` у горутині |
| Субтитри не синхронізовані з відео | Таймкоди у WebVTT не співпадають з сегментами | 🔹 Перевірити, що `webvtt.Segment` використовує той самий `hls_time` (6 секунд), що і основний енкодер |
| Помилка "Stream index out of range" | Невірний `StreamIndex` у варіанті | 🔹 Додати валідацію: перевірити доступні потоки вхідного файлу через `ffprobe` перед запуском |

### Приклад валідації StreamIndex:

```go
func validateSubtitleVariant(variant suggest.SubtitleVariant) error {
    // 🔹 Перевірити вхідний файл через ffprobe
    cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", 
        "-show_streams", variant.InputURL)
    output, err := cmd.Output()
    if err != nil {
        return fmt.Errorf("ffprobe failed: %w", err)
    }
    
    // 🔹 Парсити JSON та перевірити наявність потоку з індексом
    var probe ffprobe.Output
    json.Unmarshal(output, &probe)
    
    found := false
    for _, stream := range probe.Streams {
        if stream.Index == variant.StreamIndex && stream.CodecType == "subtitle" {
            found = true
            break
        }
    }
    
    if !found {
        return fmt.Errorf("subtitle stream index %d not found in %s", 
            variant.StreamIndex, variant.InputURL)
    }
    
    return nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Запуск конвертації субтитрів з валідацією:
func safeLaunchSubtitles(channelID string, variants []suggest.SubtitleVariant, 
                        outputDir string) []SubtitleVariantConversion {
    
    // 🔹 Валідувати варіанти перед запуском
    validVariants := make([]suggest.SubtitleVariant, 0, len(variants))
    for _, v := range variants {
        if err := validateSubtitleVariant(v); err != nil {
            log.Warnf("Channel %s: skipping invalid subtitle variant %s: %v", 
                channelID, v.Name, err)
            continue
        }
        validVariants = append(validVariants, v)
    }
    
    // 🔹 Запустити конвертацію
    return callSubtitleConversions(validVariants, outputDir)
}

// 2: Отримання URL сегментів для плейлиста:
func getSubtitleSegmentURLs(outputDir, baseURL, name string, segmentDuration time.Duration) []string {
    var urls []string
    // 🔹 Знайти всі .vtt файли для цієї мови
    pattern := filepath.Join(outputDir, fmt.Sprintf("%s_*.vtt", name))
    files, _ := filepath.Glob(pattern)
    
    // 🔹 Сортувати за іменем (за номером сегмента)
    sort.Strings(files)
    
    for _, file := range files {
        relPath, _ := filepath.Rel(outputDir, file)
        urls = append(urls, baseURL+"/"+relPath)
    }
    return urls
}

// 3: Моніторинг прогресу:
func logSubtitleProgress(channelID string, conversions []SubtitleVariantConversion) {
    for _, conv := range conversions {
        // 🔹 Перевірити, чи процес ще працює
        if conv.commands.EncoderCommand.ProcessState == nil {
            log.Debugf("Channel %s: subtitle %s still converting...", channelID, conv.Variant.Name)
        } else {
            log.Debugf("Channel %s: subtitle %s completed", channelID, conv.Variant.Name)
        }
    }
}

// 4: Обробка помилок з retry:
func launchSubtitleWithRetry(variant suggest.SubtitleVariant, outputDir string, 
                            maxRetries int) (*subtitleConversionCommand, error) {
    
    var lastErr error
    for attempt := 0; attempt < maxRetries; attempt++ {
        cmds := convertSubtitle(variant, outputDir)
        if err := cmds.start(); err == nil {
            return &cmds, nil
        } else {
            lastErr = err
            log.Warnf("Subtitle %s attempt %d failed: %v", variant.Name, attempt+1, err)
            time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
        }
    }
    return nil, fmt.Errorf("subtitle %s: all %d attempts failed: %w", 
        variant.Name, maxRetries, lastErr)
}
```

---

## 📊 Матриця параметрів субтитрів для CCTV HLS

```
Параметр              | Тип       | Рекомендоване значення      | Призначення
──────────────────────┼───────────┼─────────────────────────────┼─────────────────────────
-c:s:0 webvtt         | string    | webvtt                      | ✅ Конвертація у HLS-сумісний формат
-f webvtt             | string    | webvtt                      | ✅ Вказати формат для pipe
-hls_time (сегментер) | duration  | 6 секунд                    | 🔹 Синхронізація з відео-сегментами
OUTPUT via pipe (-)   | -         | stdout                      | ✅ Без проміжних файлів, швидше
Logfile per variant   | *os.File  | conversion-{lang}.log       | 🔹 Ізоляція логів для відладки
Async via goroutine   | bool      | true                        | ✅ Не блокує основний енкодинг
```

---

## 📚 Корисні посилання

- [FFmpeg subtitle encoding documentation](https://ffmpeg.org/ffmpeg.html#Subtitle-options)
- [WebVTT specification](https://www.w3.org/TR/webvtt1/)
- [HLS subtitle support (EXT-X-MEDIA)](https://developer.apple.com/documentation/http_live_streaming/example_playlists_for_http_live_streaming#2949262)
- [Go exec.Command pipe best practices](https://pkg.go.dev/os/exec#Cmd.StdoutPipe)

> 💡 **Ключова ідея**: Цей код — це "конвеєр" для субтитрів у вашому HLS пайплайні. Він:
> - 🎯 Асинхронно конвертує вбудовані субтитри у WebVTT
> - 🔧 Сегментує під 6-секундні інтервали для сумісності з відео
> - ⚡ Не блокує основний енкодинг завдяки горутинам та pipe
> - 🛡️ Граційно деградує при помилках одного варіанту

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку автоматичної генерації субтитрів через Whisper/NLLB
- 🧪 Написати integration-тест для перевірки синхронізації субтитрів з відео-сегментами
- 📈 Додати Prometheus-метрики для моніторингу успішності конвертації субтитрів по мовах та каналах

🛠️