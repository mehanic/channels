# 🎬 `transcoder`: Головний клас для керування процесом транскодування

Це **ядро бібліотеки**, яке інкапсулює весь життєвий цикл роботи з `ffmpeg`: від ініціалізації та побудови команди до запуску процесу, моніторингу прогресу в реальному часі та коректного завершення. Клас надає типобезпечний API для роботи з медіа-файлами, підтримку pipe-вводу/виводу для стрімінгу та асинхронний моніторинг через парсинг `stderr`.

---

## 🎯 Коротка відповідь

> **Це "контролер транскодування"**: він об'єднує конфігурацію (`media.File`), бінарники (`ffmpeg`/`ffprobe`) та системний процес у єдиному інтерфейсі з підтримкою асинхронного прогресу, pipe-потоку та безпечного завершення — ідеально для інтеграції медіа-обробки у ваші Go-сервіси.

---

## 🧱 Архітектура класу `Transcoder`

```go
type Transcoder struct {
    // 🔹 Системні ресурси
    stdErrPipe   io.ReadCloser      // 🔹 Pipe для читання прогресу з stderr FFmpeg
    stdStdinPipe io.WriteCloser     // 🔹 Pipe для відправки команд (напр. "q" для зупинки)
    process      *exec.Cmd          // 🔹 Активний процес FFmpeg

    // 🔹 Конфігурація
    mediafile          *media.File          // 🔹 Параметри входу/виходу, кодеки, фільтри
    configuration      goffmpeg.Configuration // 🔹 Шляхи до бінарників, змінні оточення
    whiteListProtocols []string             // 🔹 Дозволені протоколи для RTSP/HTTP входу
}
```

**🎯 Призначення**: Інкапсулювати **всі аспекти транскодування** у одному об'єкті:
- ✅ Конфігурація через `media.File` (декларативний API)
- ✅ Пошук та валідація бінарників (`ffmpeg`/`ffprobe`)
- ✅ Запуск процесу з контролем `stdin`/`stdout`/`stderr`
- ✅ Асинхронний моніторинг прогресу через канал `Progress`
- ✅ Безпечне завершення через `Stop()` (відправка `q\n`)

---

## 🔍 Ключові методи: Огляд

| Метод | Призначення | Критичні деталі |
|-------|-------------|----------------|
| `Initialize()` | Ініціалізація з `ffprobe` + встановлення шляхів | Парсить JSON-вивід, створює `media.File`, валідує бінарники |
| `InitializeEmptyTranscoder()` | Створення "порожнього" транскодера для ручного налаштування | Не викликає `ffprobe` — для продвинутих сценаріїв |
| `GetCommand()` | Побудова фінальної команди для `exec.Command` | Додає `-y`, `-protocol_whitelist`, викликає `ToStrCommand()` |
| `Run(progress bool)` | Запуск процесу з опціональним моніторингом | Повертає `<-chan error`, запускає `Wait()` у горутині |
| `Output()` | Канал прогресу в реальному часі | Парсить `stderr` FFmpeg через `bufio.Scanner`, конвертує `time=` у секунди |
| `Stop()` | Коректне завершення процесу | Відправляє `q\n` у `stdin` (еквівалент натискання `q` в консолі) |
| `CreateInputPipe()` / `CreateOutputPipe()` | Налаштування pipe-вводу/виводу для стрімінгу | Використовує `io.Pipe()`, вимагає узгодженості з `InputPath`/`OutputPath` |

---

## 🔄 Потік даних: Від ініціалізації до завершення

```
🔹 Крок 1: Ініціалізація
   • transcoder.Initialize("input.mp4", "output.mp4")
   • → Виклик ffprobe для отримання метаданих
   • → Парсинг JSON у media.Metadata
   • → Створення media.File з вхідними/вихідними шляхами

🔹 Крок 2: Налаштування параметрів (опціонально)
   • file := transcoder.MediaFile()
   • file.SetVideoCodec("libx264")
   • file.SetResolution("1280x720")
   • file.SetHlsSegmentDuration(4)

🔹 Крок 3: Побудова команди
   • cmd := transcoder.GetCommand()
   • → ["-y", "-c:v", "libx264", "-s", "1280x720", ..., "-i", "input.mp4", "output.mp4"]

🔹 Крок 4: Запуск з моніторингом
   • done := transcoder.Run(true)  // progress=true
   • progressChan := transcoder.Output()
   • go func() {
         for p := range progressChan {
             log.Printf("📊 %s @ %.1fx speed", p.CurrentTime, p.Speed)
         }
     }()

🔹 Крок 5: Очікування завершення
   • if err := <-done; err != nil { log.Fatal(err) }

🔹 Крок 6: Аварійне завершення (за потреби)
   • transcoder.Stop()  // → відправка "q\n" у stdin
```

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Ігнорування помилок парсингу в `Output()`

```go
// 🔹 Поточний код:
timesec := duration.DurToSec(currentTime)  // ← повертає 0 при помилці
dursec, _ := strconv.ParseFloat(t.MediaFile().Metadata().Format.Duration, 64)  // ← ігнорує err
```

**🎯 Ризик**: Невалідні `time=` або `Duration` призводять до `progress = 0` або паніки при діленні на 0.

**✅ Рішення**:
```go
timesec, err := duration.ParseDurationFlex(currentTime)
if err != nil {
    log.Printf("⚠️  Invalid time %q: %v", currentTime, err)
    continue
}
durStr := t.MediaFile().Metadata().Format.Duration
dursec, err := strconv.ParseFloat(durStr, 64)
if err != nil || dursec <= 0 {
    // 🔹 Пропускаємо розрахунок прогресу для live-стрімів
    out <- *Progress  // без поля Progress
    continue
}
progress := (timesec * 100) / dursec
Progress.Progress = progress
```

---

### 🔴 Проблема 2: Відсутність контексту для скасування

```go
// 🔹 Run() створює процес без context
proc := exec.Command(t.configuration.FFmpegBinPath(), command...)
```

**🎯 Ризик**: Неможливо скасувати транскодування при перезавантаженні сервісу або таймауті.

**✅ Рішення**: Додати `context.Context` у сигнатуру:
```go
func (t *Transcoder) Run(ctx context.Context, progress bool) <-chan error {
    done := make(chan error)
    proc := exec.CommandContext(ctx, t.configuration.FFmpegBinPath(), command...)
    // ... решта логіки
}
```

---

### 🟡 Проблема 3: Жорсткий парсинг `stderr`

```go
// 🔹 Поточний підхід: регулярні вирази + strings.Fields
if strings.Contains(line, "frame=") && strings.Contains(line, "time=") && strings.Contains(line, "bitrate=") {
    var re = regexp.MustCompile(`=\s+`)
    st := re.ReplaceAllString(line, `=`)
    f := strings.Fields(st)
    // ... парсинг полів
}
```

**🎯 Ризик**: Зміни у форматі виводу FFmpeg (напр. нові версії) ламають парсинг.

**✅ Рішення**: Використовувати офіційний `-progress` флаг FFmpeg:
```go
// 🔹 Додаємо у GetCommand():
if progress {
    command = append(command, "-progress", "pipe:1")  // Вивід прогресу у stdout у машино-читабельному форматі
}

// 🔹 Парсинг у Output():
for scanner.Scan() {
    line := scanner.Text()
    if strings.HasPrefix(line, "out_time_ms=") {
        // Парсинг часу у мікросекундах → конвертація у секунди
    }
    // ... інші ключі: progress=continue/end, speed=1.2x
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Базове транскодування запису у HLS

```go
func TranscodeToHLS(ctx context.Context, inputPath, outputDir string) error {
    // 🔹 Створення транскодера
    trans := &transcoder.Transcoder{}
    if err := trans.Initialize(inputPath, ""); err != nil {
        return fmt.Errorf("init failed: %w", err)
    }

    // 🔹 Налаштування параметрів
    file := trans.MediaFile()
    file.SetOutputPath(outputDir)
    file.SetOutputFormat("hls")
    file.SetVideoCodec("libx264")
    file.SetAudioCodec("aac")
    file.SetResolution("1280x720")
    file.SetVideoBitRate("2500k")
    file.SetHlsSegmentDuration(4)
    file.SetHlsListSize(10)
    file.SetHlsPlaylistType("vod")

    // 🔹 Запуск з моніторингом
    done := trans.Run(ctx, true)
    progressChan := trans.Output()

    // 🔹 Моніторинг прогресу
    go func() {
        for p := range progressChan {
            if p.Progress > 0 {
                log.Printf("🔄 %.1f%% @ %s (%.1fx)", p.Progress, p.CurrentTime, p.Speed)
            }
        }
    }()

    // 🔹 Очікування завершення
    if err := <-done; err != nil {
        return fmt.Errorf("transcoding failed: %w", err)
    }

    log.Printf("✅ HLS generated in %s", outputDir)
    return nil
}
```

---

### 🔹 Приклад 2: Стрімінг з камери у реальному часі через pipe

```go
func StreamFromCamera(ctx context.Context, rtspURL string, hlsOutputDir string) error {
    trans := &transcoder.Transcoder{}
    if err := trans.InitializeEmptyTranscoder(); err != nil {
        return err
    }

    file := trans.MediaFile()
    file.SetInputPath(rtspURL)  // RTSP вхід
    file.SetRtmpLive("live")    // Оптимізація для live
    file.SetCopyTs(true)        // Збереження оригінальних таймстемпів

    // 🔹 Низька затримка
    file.SetVideoCodec("libx264")
    file.SetPreset("ultrafast")
    file.SetTune("zerolatency")
    file.SetKeyframeInterval(30)  // Ключовий кадр кожну секунду @ 30fps

    // 🔹 HLS для live
    file.SetOutputFormat("hls")
    file.SetHlsListSize(3)        // Мінімум буфера
    file.SetHlsSegmentDuration(1) // 1 секунда на сегмент
    file.SetHlsPlaylistType("event")

    // 🔹 Створення output pipe для інтеграції з веб-сервером
    outputReader, err := trans.CreateOutputPipe("hls")
    if err != nil {
        return err
    }

    // 🔹 Запуск у фоні
    done := trans.Run(ctx, false)  // progress=false для мінімізації накладних витрат

    // 🔹 Читання з pipe у веб-сервер (приклад)
    go func() {
        http.HandleFunc("/live/", func(w http.ResponseWriter, r *http.Request) {
            io.Copy(w, outputReader)  // Проксі сегментів плейлиста
        })
    }()

    // 🔹 Очікування завершення (або скасування через ctx)
    return <-done
}
```

---

### 🔹 Приклад 3: Аварійне завершення при перезавантаженні сервісу

```go
type RecordingJob struct {
    transcoder *transcoder.Transcoder
    cancel     context.CancelFunc
}

func (j *RecordingJob) Start(ctx context.Context) error {
    jobCtx, cancel := context.WithCancel(ctx)
    j.cancel = cancel

    j.transcoder = &transcoder.Transcoder{}
    if err := j.transcoder.Initialize("recording.mp4", "output/"); err != nil {
        return err
    }

    // ... налаштування параметрів ...

    done := j.transcoder.Run(jobCtx, true)

    // 🔹 Моніторинг скасування
    go func() {
        <-jobCtx.Done()
        log.Printf("🛑 Cancelling transcoding job...")
        j.transcoder.Stop()  // → відправка "q\n"
    }()

    return <-done
}

func (j *RecordingJob) Stop() {
    if j.cancel != nil {
        j.cancel()  // → скасування context → Stop() → "q\n"
    }
}
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При ініціалізації:
    • Використовуйте Initialize() для автоматичного парсингу метаданих
    • Для стрімінгу: InitializeEmptyTranscoder() + ручне налаштування
    • Завжди перевіряйте наявність ffmpeg/ffprobe через configuration

[ ] Для налаштування параметрів:
    • Використовуйте media.File API (SetVideoCodec, SetResolution, тощо)
    • Для HLS: обов'язково встановлюйте OutputFormat="hls" + hls_* параметри
    • Для live: preset="ultrafast" + tune="zerolatency" + keyframeInterval=частота_кадрів

[ ] Для запуску та моніторингу:
    • Передавайте context з таймаутом для контролю життєвого циклу
    • Використовуйте Output() тільки якщо потрібен прогрес (додаткові накладні витрати)
    • Обробляйте помилки з <-done: розрізняйте помилки запуску та завершення

[ ] Для роботи з pipe:
    • CreateInputPipe() для читання з мережі/камери
    • CreateOutputPipe() для інтеграції з веб-сервером або іншим процесом
    • Закривайте pipe після завершення через closePipes()

[ ] Для дебагу:
    • Логувайте повну команду: log.Printf("🔧 Executing: ffmpeg %v", trans.GetCommand())
    • Перехоплюйте stderr при прогресі: log.Printf("📡 FFmpeg: %s", stderrLine)
    • Тестуйте з різними вхідними форматами: MP4, MKV, TS, RTSP

[ ] Для тестування:
    • Мокайте exec.Command через інтерфейс configuration
    • Тестуйте сценарії: успішне завершення, помилка парсингу, скасування через context
    • Перевіряйте коректність pipe: запис → читання → закриття
```

---

## 🎯 Висновок

> **`transcoder.Transcoder` — це "диригент" медіа-конвеєра**, який забезпечує:
> • ✅ Типобезпечний декларативний API для налаштування кодування
> • ✅ Інтеграцію з системними процесами через `exec.Command` з контролем ресурсів
> • ✅ Асинхронний моніторинг прогресу в реальному часі через парсинг `stderr`
> • ✅ Підтримку pipe-вводу/виводу для стрімінгу та інтеграції
> • ✅ Безпечне завершення через `Stop()` (відправка `q\n`)

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Швидка інтеграція транскодування записів у HLS без вивчення всіх аргументів FFmpeg
- 📡 Підтримка live-стрімінгу з камер через pipe та оптимізацію низької затримки
- 🛡️ Надійне керування життєвим циклом: запуск, моніторинг, скасування через context
- 🔍 Прозорий дебаг через парсинг прогресу та логування помилок
- 🔄 Гнучкість: підтримка як файлових записів, так і мережевих потоків (RTSP/HTTP)

Потребуєте допомоги з інтеграцією `Transcoder` у ваш конвеєр обробки записів або з налаштуванням адаптивного моніторингу прогресу? Напишіть — покажу готовий код для вашого сценарію! 🎬🚀