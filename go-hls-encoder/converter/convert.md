# Глибоке роз'яснення: `converter` пакет — запуск та управління FFmpeg для HLS-енкодингу

Цей файл містить **ядро управління процесами конвертації** — запуск FFmpeg, побудову командного рядка, генерацію master-плейлиста та обробку субтитрів. Це "диспетчер" вашого пайплайну, що координує всі етапи енкодингу.

---

## 🎯 Архітектура: що робить цей файл?

```
┌─────────────────────────────────────────┐
│ converter.LaunchConversion():          │
│                                         │
│ 🔹 Вхід:                               │
│   • inputs: []string (вхідні потоки)   │
│   • video/audio Variants: конфігурація │
│   • subtitleVariantsCh: канал субтитрів│
│   • outputDir, playlist names          │
│                                         │
│ 🔹 Етапи:                              │
│   1. Побудова аргументів FFmpeg        │
│   2. Запуск основного процесу енкодингу│
│   3. Асинхронна обробка субтитрів      │
│   4. Генерація master playlist (.m3u8) │
│   5. (опціонально) I-frame playlists  │
│                                         │
│ 🔹 Вихід:                              │
│   • *Conversion — керування процесами │
│   • Можливість сигналу/зупинки        │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `Conversion`: контейнер для процесів

```go
type Conversion struct {
    StreamURLs                 []string                          // 🎯 вхідні потоки
    mainCommand                *exec.Cmd                         // 🎯 основний FFmpeg процес
    SubtitleConversionCommands []SubtitleVariantConversion       // 🎯 процеси субтитрів
    OutputDirectory            string                            // 🎯 каталог виходу
}
```

### 🔹 Методи управління процесами

```go
// 🔹 do(f): застосувати функцію до всіх команд
func (c Conversion) do(f func(cmd *exec.Cmd)) {
    f(c.mainCommand)  // основний процес
    for _, subConv := range c.SubtitleConversionCommands {
        f(subConv.commands.EncoderCommand)  // кожен субтитр
    }
}

// 🔹 Signal(sig): надіслати сигнал усім процесам
func (c Conversion) Signal(sig syscall.Signal) {
    c.do(func(cmd *exec.Cmd) {
        cmd.Process.Signal(sig)  // SIGINT, SIGTERM тощо
    })
}

// 🔹 SigInt(): зручний метод для Ctrl+C
func (c Conversion) SigInt() {
    c.Signal(syscall.SIGINT)
}

// 🔹 Exit(): примусове завершення всіх процесів
func (c Conversion) Exit() {
    c.do(func(cmd *exec.Cmd) {
        if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
            cmd.Process.Kill()  // kill -9
        }
    })
}
```

> 💡 **Ключова ідея**: Патерн `do(f)` дозволяє централізовано керувати всіма процесами конвертації — корисно для graceful shutdown та обробки помилок.

---

## 🔧 Константи та налаштування HLS

```go
var GENERATE_IPLAYLIST = false  // 🔹 Флаг для I-frame playlist генерації
var FFMPEG_MASTER_PLAYLIST = "ffmpeg_playlist.m3u8"  // 🔹 Ім'я master playlist

var hlsSettings = []string{
    "-f", "hls",                    // Формат виводу: HLS
    "-hls_flags", "+split_by_time", // Розбивати за часом, не за розміром
    "-hls_time", "6",              // 🔹 Довжина сегмента: 6 секунд
    "-hls_list_size", "0",         // 0 = необмежена кількість сегментів у плейлисті
    //"-hls_playlist_type", "event", // Розкоментувати для live-стрімінгу
    "-hls_segment_type", "fmp4",   // 🔹 Використовувати fMP4 (фрагментований MP4)
    "-movflags", "+frag_keyframe", // Фрагментувати на ключових кадрах
    //"-flags", "+cgop",           // Closed GOP (опціонально)
    "-g", "60",                    // 🔹 Фіксований GOP: 60 кадрів
    "-master_pl_name", FFMPEG_MASTER_PLAYLIST,  // Ім'я master playlist
    //"-hls_flags", "+single_file", // Всі сегменти в одному файлі (не рекомендується)
    //"-hls_flags", "+independent_segments", // Кожен сегмент самодостатній
}
```

### 🎯 Ключові параметри HLS

| Параметр | Значення | Призначення |
|----------|----------|-------------|
| `-hls_time` | `6` | 🔹 Довжина сегмента: 6 секунд (баланс між latency та overhead) |
| `-hls_segment_type` | `fmp4` | 🔹 Фрагментований MP4: краща сумісність з сучасними плеєрами |
| `-movflags +frag_keyframe` | — | 🔹 Фрагментувати тільки на I-frames: гарантує незалежність сегментів |
| `-g 60` | `60` | 🔹 Фіксований GOP: ключовий кадр кожні 60 кадрів (~2 сек @ 30 fps) |
| `-hls_list_size 0` | `0` | 🔹 Зберігати всі сегменти у плейлисті (для VOD) |

> 💡 **Порада**: Для live-стрімінгу розкоментуйте `-hls_playlist_type event` та встановіть `-hls_list_size` у 5-10 для ковзного вікна.

---

## 🔍 Функція `LaunchConversion`: головний вхідний пункт

### 🔹 Етап 1: Побудова аргументів FFmpeg

```go
func LaunchConversion(outputDir, masterPlaylistName, streamPlaylistName string,
    videoVariants []suggest.VideoVariant, audioVariants []suggest.AudioVariant, 
    subtitleVariantsCh <-chan []suggest.SubtitleVariant,
    inputs ...string) (*Conversion, error) {

    // 🔹 1. Базові аргументи: приховати банер, перезаписати файли, лог рівень
    args := ffmpegDefaultArguments()  // ["-hide_banner", "-y", "-stats", "-loglevel", "warning"]
    
    // 🔹 2. Додати вхідні файли/потоки
    for _, input := range inputs {
        args = append(args, "-i", input)
    }
    
    // 🔹 3. Додати параметри відео/аудіо варіантів
    args = append(args, videoConversionArgs(videoVariants)...)  // з converter/args.go
    args = append(args, audioConversionArgs(audioVariants)...)
    
    // 🔹 4. Додати HLS-налаштування
    args = append(args, hlsSettings...)
    
    // 🔹 5. Додати mapping потоків: "v:0 v:1 a:0 a:1"
    args = append(args, "-var_stream_map", variantsMapArg(videoVariants, audioVariants))
```

### 🔹 Етап 2: Підготовка вихідного каталогу

```go
    // 🔹 Створити каталог виходу (якщо не існує)
    if err := os.MkdirAll(outputDir, 0700); err != nil {
        log.Println("Cannot create conversion dir at path '"+outputDir+"':", err)
        return nil, err
    }
    
    // 🔹 Шаблон імені плейлиста для кожного stream: stream_0.m3u8, stream_1.m3u8...
    outputFile := filepath.Join(outputDir, streamPlaylistName+"_%v.m3u8")
    
    // 🔹 Додати параметри черги та вихідний файл
    args = append(args, "-max_muxing_queue_size", "1024", outputFile)
```

> 💡 **Важливо**: `-max_muxing_queue_size 1024` запобігає помилкам "Too many packets buffered" при обробці складних потоків.

### 🔹 Етап 3: Запуск основного процесу FFmpeg

```go
    // 🔹 Канал для передачі імені master playlist у горутину
    masterCh := make(chan string)
    
    // 🔹 Запустити FFmpeg асинхронно
    cmd, err := callFFmpeg(filepath.Join(outputDir, "conversion.log"), args, masterCh)
    if err != nil {
        close(masterCh)
        return nil, err
    }
```

### 🔹 Етап 4: Асинхронна обробка субтитрів

```go
    // 🔹 Отримати варіанти субтитрів з каналу (блокує, доки не надійдуть дані)
    subtitleVariants := <-subtitleVariantsCh
    
    // 🔹 Запустити конвертацію субтитрів (окремі процеси)
    convertedSubtitles := callSubtitleConversions(subtitleVariants, outputDir)
```

> 💡 **Архітектурне рішення**: Субтитри обробляються окремо від основного відео/аудіо, що дозволяє:
> • Паралельне виконання → швидша загальна конвертація
> • Незалежне масштабування → можна додати більше воркерів для субтитрів
> • Гнучкість → субтитри можуть мати інші параметри енкодингу

### 🔹 Етап 5: Генерація master playlist

```go
    // 🔹 Створити master playlist файл
    masterFilename := filepath.Join(outputDir, masterPlaylistName+".m3u8")
    masterCh <- masterFilename  // передати ім'я у горутину callFFmpeg
    close(masterCh)
    
    f, err := os.OpenFile(masterFilename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
    if err != nil { panic(err) }  // FIXME: краща обробка помилок
    
    // 🔹 Записати заголовок HLS
    f.WriteString("#EXTM3U\n" + "#EXT-X-VERSION:7\n")
    
    // 🔹 Визначити групи аудіо/субтитрів
    var audioGroup *string = nil
    var subtitlesGroup *string = nil
    if len(audioVariants) > 0 {
        audioGroup = &suggest.DefaultAudioGroupID
        if audioVariants[0].GroupID != nil {
            audioGroup = audioVariants[0].GroupID
        }
    }
    if len(convertedSubtitles) > 0 {
        subtitlesGroup = &suggest.DefaultSubtitlesGroupID
        if convertedSubtitles[0].Variant.GroupID != nil {
            subtitlesGroup = convertedSubtitles[0].Variant.GroupID
        }
    }
    
    // 🔹 Записати EXT-X-MEDIA для аудіо
    streamIndex := len(videoVariants)  // аудіо плейлисти йдуть після відео
    for _, variant := range audioVariants {
        f.WriteString(variant.Stanza(playlistFilenameForStream(streamPlaylistName, streamIndex)) + "\n")
        streamIndex += 1
    }
    f.WriteString("\n")
    
    // 🔹 Записати EXT-X-MEDIA для субтитрів
    streamIndex = 0  // субтитри починаються з 0
    for _, c := range convertedSubtitles {
        fmt.Printf("DEBUG: Adding subtitle %q to Master\n", c.Variant.Name)
        f.WriteString(c.Variant.Stanza() + "\n")
        streamIndex += 1
    }
    f.WriteString("\n\n")
    
    // 🔹 Записати EXT-X-STREAM-INF для відео варіантів
    streamIndex = 0  // відео варіанти починаються з 0
    for _, variant := range videoVariants {
        vAudioGroup := audioGroup
        if variant.AudioGroup != nil {
            vAudioGroup = variant.AudioGroup
        }
        vSubtitlesGroup := subtitlesGroup
        if variant.SubtitleGroup != nil {
            vSubtitlesGroup = variant.SubtitleGroup  // 🔹 BUG: має бути variant.SubtitleGroup, не vAudioGroup
        }
        f.WriteString(variant.Stanza(playlistFilenameForStream(streamPlaylistName, streamIndex), 
            vAudioGroup, vSubtitlesGroup) + "\n")
        streamIndex += 1
    }
    f.Close()
```

> ⚠️ **BUG виявлено**: У рядку `vSubtitlesGroup := subtitlesGroup` є помилка копіювання:
> ```go
> if variant.SubtitleGroup != nil {
>     vAudioGroup = variant.SubtitleGroup  // ❌ має бути vSubtitlesGroup
> }
> ```
> Це призведе до неправильного присвоєння групи субтитрів.

### 🔹 Етап 6: Повернення результату

```go
    return &Conversion{
        StreamURLs:                 inputs,
        mainCommand:                cmd,
        SubtitleConversionCommands: convertedSubtitles,
        OutputDirectory:            outputDir,
    }, nil
}
```

---

## 🔍 Функція `callFFmpeg`: запуск процесу з логуванням

```go
func callFFmpeg(logFilename string, args []string, masterCh <-chan string) (*exec.Cmd, error) {
    // 🔹 Створити файл логу
    logFile, err := os.Create(logFilename)
    if err != nil {
        log.Println("Cannot create logfile:", err)
        return nil, err
    }
    
    // 🔹 Створити команду FFmpeg
    cmd := exec.Command("ffmpeg", args...)
    
    // 🔹 Debug: вивести команду у консоль
    fmt.Println("\nDEBUG: Running FFMPEG command:\n \"" + strings.Join(cmd.Args, "\" \"") + "\"")
    fmt.Println("DEBUG:\tUse \n\t\ttail -f " + logFilename + "\n\n\tto see output.")
    
    // 🔹 Перенаправити вивід у лог-файл
    cmd.Stdout = logFile
    cmd.Stderr = logFile  // TODO: можна додати buffer для парсингу помилок
    
    // 🔹 Запустити процес (не чекати завершення)
    err = cmd.Start()
    if err != nil {
        log.Println("FFmpeg execution had the following error:", err)
        return cmd, err
    }
    
    // 🔹 Горутина для пост-обробки після завершення FFmpeg
    go func() {
        masterFilename, ok := <-masterCh
        if !ok { return }  // канал закрито
        
        // 🔹 Дочекатися завершення FFmpeg
        err := cmd.Wait()
        if err != nil {
            log.Println("Error running FFMPEG:", err)
            return
        }
        logFile.Close()
        
        // 🔹 Post-processing: генерація I-frame playlist
        dir, filename := filepath.Split(masterFilename)
        
        fmt.Printf("DEBUG: Everything is fine. \n"+
            "DEBUG: Generating I-FRAME-ONLY playlists on master in directory \"%v\"\n", dir)
        
        // 🔹 EnrichPlaylist: додати I-frame info до існуючого master playlist
        _, err = iframe_playlist_generator.EnrichPlaylist(dir, filename, dir, FFMPEG_MASTER_PLAYLIST, filename)
        if err != nil {
            log.Println("An error happened enriching playlist:", err)
        }
        
        // 🔹 GeneratePlaylist: створити окремий I-frame-only playlist (опціонально)
        if GENERATE_IPLAYLIST {
            err = iframe_playlist_generator.GeneratePlaylist(dir, filename)
            if err != nil {
                log.Println("An error happened generating I-FRAME-ONLY playlist:", err)
            }
        } else {
            fmt.Printf("DEBUG: Everything is fine, but we're not generating iFrame Playlist...")
        }
    }()
    
    return cmd, nil
}
```

### 🎯 Ключові моменти реалізації

#### 🔹 Асинхронна пост-обробка через горутину

```
Проблема:
• Генерація I-frame playlist вимагає читання всіх сегментів
• Це може зайняти значний час після завершення основного енкодингу

Рішення:
• Запустити пост-обробку у окремій горутині після cmd.Wait()
• Основний потік може продовжити роботу (напр., обробляти наступний сегмент)

Переваги:
✅ Не блокує основний потік конвертації
✅ Дозволяє паралельну обробку кількох каналів
✅ Гнучкість: можна вимкнути через GENERATE_IPLAYLIST

Ризики:
⚠️ Горутина може "втекти", якщо cmd.Wait() ніколи не завершиться
⚠️ Помилки у пост-обробці не впливають на основний процес (логірування)
```

#### 🔹 Логування та відладка

```go
// 🔹 Вивід команди у консоль для відладки:
fmt.Println("\nDEBUG: Running FFMPEG command:\n \"" + strings.Join(cmd.Args, "\" \"") + "\"")

// 🔹 Рекомендація для користувача:
fmt.Println("DEBUG:\tUse \n\t\ttail -f " + logFilename + "\n\n\tto see output.")

// 🔹 Логування помилок:
if err != nil {
    log.Println("FFmpeg execution had the following error:", err)
}
```

> 💡 **Порада**: У production-режимі замініть `fmt.Println` на структуроване логування з рівнями (INFO/WARN/ERROR) та метаданими (channel_id, segment_num).

---

## 🔧 Допоміжні функції

### `playlistFilenameForStream`: генерація імен плейлистів

```go
func playlistFilenameForStream(streamPlaylistName string, index int) string {
    return streamPlaylistName + "_" + strconv.Itoa(index) + ".m3u8"
}
```

**Приклад:**
```
streamPlaylistName = "channel1"
index = 0 → "channel1_0.m3u8"  // відео 720p
index = 1 → "channel1_1.m3u8"  // відео 480p
index = 2 → "channel1_2.m3u8"  // аудіо 128k
```

### `ffmpegDefaultArguments`: базові прапорці

```go
func ffmpegDefaultArguments() []string {
    return []string{"-hide_banner", "-y", "-stats", "-loglevel", "warning"}
}
```

| Прапорець | Призначення |
|-----------|-------------|
| `-hide_banner` | Приховати версію FFmpeg та конфігурацію (менше шуму в логах) |
| `-y` | Автоматично перезаписувати вихідні файли (без запиту) |
| `-stats` | Показувати прогрес енкодингу у stderr (корисно для моніторингу) |
| `-loglevel warning` | Показувати тільки попередження та помилки (зменшує обсяг логів) |

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Запуск конвертації з channel-aware налаштуваннями

```go
// У segmentFinalizer — запуск енкодингу для каналу:
func launchChannelConversion(channelID string, inputPath string, 
                          outputDir string, bandwidthMbps float64) (*converter.Conversion, error) {
    
    // 🔹 Сгенерувати варіанти на основі пропускної здатності
    videoVars, audioVars := suggestVariantsForChannel(channelID, bandwidthMbps)
    
    // 🔹 Канал для субтитрів (асинхронна передача)
    subtitleCh := make(chan []suggest.SubtitleVariant, 1)
    go func() {
        subs := generateSubtitleVariants(channelID)  // ваша логіка
        subtitleCh <- subs
        close(subtitleCh)
    }()
    
    // 🔹 Запустити конвертацію
    conv, err := converter.LaunchConversion(
        outputDir,
        "master",                    // master playlist name
        channelID,                   // stream playlist base name
        videoVars, audioVars,        // варіанти
        subtitleCh,                  // канал субтитрів
        inputPath,                   // вхідний файл/потік
    )
    
    if err != nil {
        log.Errorf("Channel %s: conversion launch failed: %v", channelID, err)
        return nil, err
    }
    
    log.Infof("Channel %s: conversion started, output dir: %s", channelID, outputDir)
    return conv, nil
}
```

### ✅ 2: Graceful shutdown при зупинці каналу

```go
// У канал-менеджері — коректна зупинка конвертації:
func stopChannelConversion(conv *converter.Conversion, channelID string) {
    log.Infof("Channel %s: stopping conversion...", channelID)
    
    // 🔹 Спочатку спробувати м'яку зупинку (SIGINT)
    conv.SigInt()
    
    // 🔹 Дати час на завершення (напр., 10 секунд)
    done := make(chan bool, 1)
    go func() {
        // Періодично перевіряти статус процесів
        for i := 0; i < 10; i++ {
            if allProcessesExited(conv) {
                done <- true
                return
            }
            time.Sleep(1 * time.Second)
        }
        done <- false
    }()
    
    select {
    case <-done:
        log.Infof("Channel %s: conversion stopped gracefully", channelID)
    case <-time.After(10 * time.Second):
        // 🔹 Якщо не завершилось — примусово вбити
        log.Warnf("Channel %s: forcing conversion termination", channelID)
        conv.Exit()
    }
}

func allProcessesExited(conv *converter.Conversion) bool {
    // Перевірити основний процес
    if conv.mainCommand.ProcessState == nil || !conv.mainCommand.ProcessState.Exited() {
        return false
    }
    // Перевірити процеси субтитрів
    for _, sub := range conv.SubtitleConversionCommands {
        cmd := sub.commands.EncoderCommand
        if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
            return false
        }
    }
    return true
}
```

### ✅ 3: Моніторинг прогресу енкодингу

```go
// monitoring.Monitor — метрики для конвертації:
type ConversionMetrics struct {
    ConversionsStarted  *prometheus.CounterVec  // кількість запущених конвертацій
    ConversionsCompleted *prometheus.CounterVec // кількість успішних
    ConversionsFailed   *prometheus.CounterVec  // кількість помилок
    EncodingDuration    *prometheus.HistogramVec // час енкодингу
    OutputSizeGauge     *prometheus.GaugeVec    // розмір вихідних файлів
}

// У процесі конвертації:
func monitorConversion(channelID string, conv *converter.Conversion, 
                      metrics *ConversionMetrics, startTime time.Time) {
    
    metrics.ConversionsStarted.WithLabelValues(channelID).Inc()
    
    // 🔹 Горутина для очікування завершення
    go func() {
        // Періодично перевіряти статус
        ticker := time.NewTicker(5 * time.Second)
        defer ticker.Stop()
        
        for range ticker.C {
            if allProcessesExited(conv) {
                duration := time.Since(startTime)
                outputSize := calculateOutputSize(conv.OutputDirectory)
                
                metrics.ConversionsCompleted.WithLabelValues(channelID).Inc()
                metrics.EncodingDuration.WithLabelValues(channelID).Observe(duration.Seconds())
                metrics.OutputSizeGauge.WithLabelValues(channelID).Set(float64(outputSize))
                
                log.Infof("Channel %s: conversion completed in %v, size: %d bytes", 
                    channelID, duration, outputSize)
                return
            }
        }
    }()
}

func calculateOutputSize(dir string) int64 {
    var total int64
    filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
        if err == nil && !info.IsDir() {
            total += info.Size()
        }
        return nil
    })
    return total
}
```

### ✅ 4: Обробка помилок та відновлення

```go
// Стратегія retry при помилці FFmpeg:
func launchWithRetry(channelID string, maxRetries int, 
                    launchFunc func() (*converter.Conversion, error)) (*converter.Conversion, error) {
    
    var lastErr error
    for attempt := 0; attempt < maxRetries; attempt++ {
        conv, err := launchFunc()
        if err == nil {
            return conv, nil
        }
        
        lastErr = err
        log.Warnf("Channel %s: conversion attempt %d failed: %v", 
            channelID, attempt+1, err)
        
        // 🔹 Експоненційна затримка перед повтором
        delay := time.Duration(1<<uint(attempt)) * time.Second
        log.Infof("Channel %s: retrying in %v...", channelID, delay)
        time.Sleep(delay)
    }
    
    return nil, fmt.Errorf("channel %s: all %d conversion attempts failed: %w", 
        channelID, maxRetries, lastErr)
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Інтеграційний тест на запуск конвертації

```go
func TestLaunchConversion_Integration(t *testing.T) {
    // 🔹 Підготувати тестові дані
    tempDir := t.TempDir()
    inputFile := createTestInputVideo(t)  // ваша helper-функція
    
    videoVars := []suggest.VideoVariant{
        {
            MapInput:         "0:v:0",
            Codec:            "libx264",
            ResolutionHeight: intPtr(480),
            Bitrate:          stringPtr("1000k"),
            Profile:          stringPtr("main"),
            Level:            stringPtr("4.0"),
        },
    }
    audioVars := []suggest.AudioVariant{
        {
            MapInput:  "0:a:0",
            Codec:     "aac",
            Bitrate:   stringPtr("128k"),
            GroupID:   stringPtr("audio_stereo"),
        },
    }
    
    subtitleCh := make(chan []suggest.SubtitleVariant, 1)
    subtitleCh <- []suggest.SubtitleVariant{}  // порожні субтитри для тесту
    close(subtitleCh)
    
    // 🔹 Запустити конвертацію
    conv, err := LaunchConversion(
        tempDir, "master", "stream",
        videoVars, audioVars, subtitleCh,
        inputFile,
    )
    assert.NoError(t, err)
    assert.NotNil(t, conv)
    
    // 🔹 Дочекатися завершення (або таймаут)
    done := make(chan bool, 1)
    go func() {
        conv.mainCommand.Wait()  // дочекатися FFmpeg
        done <- true
    }()
    
    select {
    case <-done:
        // 🔹 Перевірити вихідні файли
        assert.FileExists(t, filepath.Join(tempDir, "master.m3u8"))
        assert.FileExists(t, filepath.Join(tempDir, "stream_0.m3u8"))  // відео
        assert.FileExists(t, filepath.Join(tempDir, "stream_1.m3u8"))  // аудіо
        
        // 🔹 Перевірити вміст master playlist
        masterContent, _ := os.ReadFile(filepath.Join(tempDir, "master.m3u8"))
        assert.Contains(t, string(masterContent), "#EXT-X-STREAM-INF")
        assert.Contains(t, string(masterContent), "BANDWIDTH=")
        
    case <-time.After(30 * time.Second):
        t.Fatal("Conversion timed out")
    }
    
    // 🔹 Прибрати процеси
    conv.Exit()
}
```

### 🔹 Тест на graceful shutdown

```go
func TestConversion_SigInt(t *testing.T) {
    // 🔹 Запустити довгу конвертацію (наприклад, 10-секундне відео)
    conv, err := launchTestConversion(t)
    assert.NoError(t, err)
    
    // 🔹 Надіслати SIGINT
    conv.SigInt()
    
    // 🔹 Перевірити, що процеси завершилися
    done := make(chan bool, 1)
    go func() {
        conv.mainCommand.Wait()
        done <- true
    }()
    
    select {
    case <-done:
        // ✅ Процес завершився коректно
        assert.NotNil(t, conv.mainCommand.ProcessState)
    case <-time.After(10 * time.Second):
        t.Fatal("Process did not terminate after SIGINT")
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `vAudioGroup = variant.SubtitleGroup` | Субтитри прив'язуються до аудіо-групи | 🔹 Виправити помилку: `vSubtitlesGroup = variant.SubtitleGroup` |
| FFmpeg не знаходить вхідний файл | Помилка "No such file or directory" | 🔹 Перевірити, що `inputs` містить валідні шляхи; додати валідацію перед запуском |
| Переповнення черги muxing | Помилка "Too many packets buffered" | 🔹 Збільшити `-max_muxing_queue_size` (за замовчуванням 1024) |
| I-frame генерація зависає | Горутина не завершується | 🔹 Додати таймаут у пост-обробці; логувати прогрес |
| Субтитри не з'являються у master playlist | Плейлист не містить EXT-X-MEDIA для субтитрів | 🔹 Перевірити, що `subtitleVariantsCh` не заблокований; додати логування отримання даних |

### Приклад валідації вхідних файлів:

```go
func validateInputs(inputs []string) error {
    for _, input := range inputs {
        if _, err := os.Stat(input); os.IsNotExist(err) {
            // 🔹 Спробувати як URL (для мережевих потоків)
            if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "rtmp://") {
                return fmt.Errorf("input not found: %s", input)
            }
        }
    }
    return nil
}

// Використання у LaunchConversion:
if err := validateInputs(inputs); err != nil {
    return nil, fmt.Errorf("invalid inputs: %w", err)
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Запуск конвертації з обробкою помилок:
func safeLaunchConversion(channelID string, config ConversionConfig) (*converter.Conversion, error) {
    if err := validateConversionConfig(config); err != nil {
        return nil, fmt.Errorf("invalid config for %s: %w", channelID, err)
    }
    
    conv, err := converter.LaunchConversion(
        config.OutputDir,
        config.MasterPlaylistName,
        config.StreamPlaylistName,
        config.VideoVariants,
        config.AudioVariants,
        config.SubtitleChannel,
        config.Inputs...,
    )
    
    if err != nil {
        log.Errorf("Channel %s: conversion launch failed: %v", channelID, err)
        return nil, err
    }
    
    log.Infof("Channel %s: conversion started", channelID)
    return conv, nil
}

// 2: Моніторинг стану конвертації:
func isConversionActive(conv *converter.Conversion) bool {
    if conv.mainCommand.ProcessState != nil && conv.mainCommand.ProcessState.Exited() {
        return false
    }
    for _, sub := range conv.SubtitleConversionCommands {
        cmd := sub.commands.EncoderCommand
        if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
            return true  // хоча б один процес ще працює
        }
    }
    return false
}

// 3: Отримання вихідних URL для клієнтів:
func getOutputURLs(conv *converter.Conversion, baseURL string) []string {
    var urls []string
    masterPath := filepath.Join(conv.OutputDirectory, "master.m3u8")
    if relPath, err := filepath.Rel(conv.OutputDirectory, masterPath); err == nil {
        urls = append(urls, baseURL+"/"+relPath)
    }
    // 🔹 Додати URL для кожного stream playlist...
    return urls
}

// 4: Логування прогресу для відладки:
func logConversionProgress(channelID string, conv *converter.Conversion) {
    log.Debugf("Channel %s: conversion state:", channelID)
    log.Debugf("  Main process: pid=%d, exited=%v", 
        conv.mainCommand.Process.Pid,
        conv.mainCommand.ProcessState != nil && conv.mainCommand.ProcessState.Exited())
    
    for i, sub := range conv.SubtitleConversionCommands {
        cmd := sub.commands.EncoderCommand
        log.Debugf("  Subtitle %d: pid=%d, exited=%v", 
            i, cmd.Process.Pid,
            cmd.ProcessState != nil && cmd.ProcessState.Exited())
    }
}
```

---

## 📊 Матриця параметрів конвертації для CCTV HLS

```
Параметр                | Тип       | Рекомендоване значення      | Призначення
────────────────────────┼───────────┼─────────────────────────────┼─────────────────────────
-hls_time               | int       | 4-6 секунд                  | ✅ Баланс latency/overhead
-hls_segment_type       | string    | fmp4                        | ✅ Сучасна сумісність
-g (GOP)                | int       | 60 (2 сек @ 30 fps)         | ✅ Швидкий seek, стійкість
-max_muxing_queue_size  | int       | 1024                        | ✅ Запобігання помилок черги
-hls_list_size          | int       | 0 (VOD) / 5-10 (live)       | 🔹 Керування розміром плейлиста
-hls_playlist_type      | string    | (не вказано) / "event"      | 🔹 VOD vs live поведінка
GENERATE_IPLAYLIST      | bool      | false (за замовчуванням)    | ⚠️ Додатковий overhead для I-frame playlists
```

---

## 📚 Корисні посилання

- [FFmpeg HLS muxer documentation](https://ffmpeg.org/ffmpeg-formats.html#hls)
- [HLS Authoring Specification for Apple Devices](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [fMP4 vs MPEG-TS for HLS](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls)
- [Go exec.Command best practices](https://pkg.go.dev/os/exec#Cmd)

> 💡 **Ключова ідея**: Цей `converter` — це "оркестратор" вашого HLS-пайплайну. Він не просто запускає FFmpeg, а:
> - 🎯 Координує паралельну обробку відео/аудіо/субтитрів
> - 🔧 Гарантує коректну структуру вихідних плейлистів (master + stream)
> - ⚡ Дозволяє graceful shutdown та моніторинг процесів
> - 🛡️ Ізолює помилки (пост-обробка у горутині не ламає основний потік)

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку динамічного оновлення master playlist для live-стрімінгу
- 🧪 Написати integration-тест для перевірки сумісності вихідних HLS-файлів з реальними плеєрами (VLC, hls.js, AVPlayer)
- 📈 Додати Prometheus-метрики для моніторингу продуктивності енкодингу та використання ресурсів по каналах

🛠️