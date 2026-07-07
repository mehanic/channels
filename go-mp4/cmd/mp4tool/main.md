# 🛠️ `main.go`: Точка входу CLI-інструменту `mp4tool`

Це **головний файл** бібліотеки `go-mp4`, який реалізує **уніфікований CLI-інтерфейс** для всіх інструментів аналізу та обробки MP4-файлів: `dump`, `psshdump`, `probe`, `extract`, `edit`, `divide`.

---

## 🎯 Коротка відповідь

> **Це "командний центр" mp4tool**: він приймає команду користувача, делегує виконання відповідному під-інструменту та забезпечує консистентний інтерфейс, обробку помилок та довідку — подібно до `git`, `docker` або `kubectl`.

---

## 🗂️ Структура інструменту

```bash
# 🔹 Базовий синтаксис:
$ mp4tool COMMAND [OPTIONS] [ARGS]

# 🔹 Доступні команди:
📋 dump         # 🔹 Візуалізація структури боксів (людсько-читабельний формат)
🔐 psshdump     # 🔹 Витягування DRM-метаданих (PSSH бокси)
🔍 probe        # 🔹 Структурований звіт про метадані файлу (JSON/YAML)
📤 extract      # 🔹 Витягування сирих даних конкретного боксу
✏️  alpha edit   # 🔹 Редагування метаданих (таймстемпи, видалення боксів)
🎬 alpha divide # 🔹 Розділення на HLS-сегменти з генерацією плейлистів

# 🔹 Приклади:
$ mp4tool dump video.mp4                    # 🔹 Показати дерево боксів
$ mp4tool psshdump encrypted.mp4            # 🔹 Показати DRM-метадані
$ mp4tool probe -format=yaml video.mp4      # 🔹 Звіт у YAML
$ mp4tool extract moov video.mp4 > moov.bin # 🔹 Витягнути moov бокс
$ mp4tool alpha edit -drop=free input.mp4 output.mp4  # 🔹 Видалити free бокси
$ mp4tool alpha divide input.mp4 output/    # 🔹 Розділити на HLS-сегменти
```

---

## 🧱 Основні компоненти

### 🔹 Головний вхід: `main()`

```go
func main() {
    args := os.Args[1:]  // 🔹 Пропускаємо ім'я програми (mp4tool)
    
    // 🔹 Перевірка наявності команди
    if len(args) == 0 {
        printUsage()  // 🔹 Показати довідку
        os.Exit(1)    // 🔹 Код помилки
    }
    
    // 🔹 Маршрутизація за першим аргументом (ім'ям команди)
    switch args[0] {
    case "help":
        printUsage()  // 🔹 Явний запит довідки
        
    case "dump":
        os.Exit(dump.Main(args[1:]))  // 🔹 Делегування інструменту dump
        
    case "psshdump":
        os.Exit(psshdump.Main(args[1:]))  // 🔹 DRM-метадані
        
    case "probe":
        os.Exit(probe.Main(args[1:]))  // 🔹 Аналіз метаданих
        
    case "extract":
        os.Exit(extract.Main(args[1:]))  // 🔹 Витягування боксів
        
    case "alpha":
        os.Exit(alpha(args[1:]))  // 🔹 Експериментальні команди
        
    default:
        printUsage()  // 🔹 Невідома команда
        os.Exit(1)
    }
}
```

**🎯 Призначення**: Забезпечити **єдину точку входу** для всіх інструментів з:
- ✅ Консистентною обробкою аргументів
- ✅ Уніфікованою довідкою (`printUsage`)
- ✅ Стандартними кодами виходу (`os.Exit`)
- ✅ Чіткою маршрутизацією команд

---

### 🔹 Експериментальні команди: `alpha()`

```go
func alpha(args []string) int {
    if len(args) < 1 {
        printUsage()
        return 1
    }
    
    switch args[0] {
    case "edit":
        return edit.Main(args[1:])  // 🔹 Редагування метаданих
    case "divide":
        return divide.Main(args[1:])  // 🔹 Розділення на сегменти
    default:
        printUsage()
        return 1
    }
}
```

**🎯 Призначення**: Ізолювати **нестабільні/експериментальні функції** у під-команду `alpha`, щоб:
- ✅ Не ламати стабільний API основних команд
- ✅ Чітко сигналізувати користувачам про експериментальний статус
- ✅ Легко додавати/видаляти функції без впливу на основний потік

**🔢 Приклади використання:**
```bash
# 🔹 Стабільні команди (без префіксу):
$ mp4tool dump video.mp4
$ mp4tool probe video.mp4

# 🔹 Експериментальні команди (з префіксом alpha):
$ mp4tool alpha edit -drop=free input.mp4 output.mp4
$ mp4tool alpha divide input.mp4 output/
```

---

### 🔹 Довідка: `printUsage()`

```go
func printUsage() {
    fmt.Fprintf(os.Stderr, "USAGE: mp4tool COMMAND_NAME [ARGS]\n")
    fmt.Fprintln(os.Stderr)
    fmt.Fprintln(os.Stderr, "COMMAND_NAME:")
    fmt.Fprintln(os.Stderr, "  dump         : display box tree as human readable format")
    fmt.Fprintln(os.Stderr, "  psshdump     : display pssh box attributes")
    fmt.Fprintln(os.Stderr, "  probe        : probe and summarize mp4 file status")
    fmt.Fprintln(os.Stderr, "  extract      : extract specific box")
    fmt.Fprintln(os.Stderr, "  alpha edit")
    fmt.Fprintln(os.Stderr, "  alpha divide")
}
```

**🎯 Призначення**: Надати користувачеві **швидку довідку** про доступні команди без необхідності читати документацію.

**🔑 Особливості:**
- ✅ Вивід у `stderr` (стандарт для довідки/помилок)
- ✅ Короткі, зрозумілі описи кожної команди
- ✅ Чітке розділення стабільних та експериментальних команд

**🔢 Приклад виводу:**
```
USAGE: mp4tool COMMAND_NAME [ARGS]

COMMAND_NAME:
  dump         : display box tree as human readable format
  psshdump     : display pssh box attributes
  probe        : probe and summarize mp4 file status
  extract      : extract specific box
  alpha edit
  alpha divide
```

---

## 🔍 Маршрутизація команд: Повний потік

```
🔹 Вхід: mp4tool probe -format=yaml video.mp4
│
▼
🔹 main():
   • args = ["probe", "-format=yaml", "video.mp4"]
   • args[0] = "probe" → case "probe":
   • Виклик: probe.Main(["-format=yaml", "video.mp4"])
│
▼
🔹 probe.Main():
   • Парсинг прапорців: -format=yaml
   • Відкриття файлу: video.mp4
   • Виклик buildReport() → аналіз метаданих
   • Вивід у YAML через os.Stdout
│
▼
🔹 Повернення коду виходу:
   • 0 = успіх → os.Exit(0)
   • 1 = помилка → os.Exit(1) + повідомлення у stderr
```

---

## 🔄 Порівняння з іншими CLI-інструментами

| Інструмент | Структура команд | Схожість з mp4tool |
|------------|-----------------|-------------------|
| **git** | `git commit`, `git push`, `git alpha-feature` | ✅ Під-команди + експериментальні через префікс |
| **docker** | `docker run`, `docker build`, `docker experimental` | ✅ Ізоляція експериментальних функцій |
| **kubectl** | `kubectl get`, `kubectl apply`, `kubectl alpha` | ✅ Явний префікс для нестабільних команд |
| **mp4tool** | `mp4tool dump`, `mp4tool alpha edit` | ✅ Та сама філософія: стабільність + гнучкість |

**🎯 Призначення**: Забезпечити **знайомий інтерфейс** для користувачів, які вже працювали з сучасними CLI-інструментами.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Інтеграція mp4tool як підпроцесу

```go
// 🔹 Універсальна функція для виклику будь-якої команди mp4tool
func runMp4tool(command string, args ...string) (string, error) {
    cmdArgs := append([]string{command}, args...)
    cmd := exec.Command("mp4tool", cmdArgs...)
    
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    
    if err := cmd.Run(); err != nil {
        return "", fmt.Errorf("mp4tool %s failed: %w\nstderr: %s", 
            command, err, stderr.String())
    }
    
    return stdout.String(), nil
}

// 🔹 Використання для валідації сегмента:
func validateSegment(filePath string) error {
    // 🔹 Швидкий аналіз структури через probe
    output, err := runMp4tool("probe", "-format=json", filePath)
    if err != nil {
        return fmt.Errorf("probe failed: %w", err)
    }
    
    // 🔹 Парсинг JSON-відповіді
    var report struct {
        Tracks []*struct {
            Codec   string `json:"codec"`
            Width   uint16 `json:"width"`
            Height  uint16 `json:"height"`
            Bitrate uint64 `json:"bitrate"`
        } `json:"tracks"`
    }
    
    if err := json.Unmarshal([]byte(output), &report); err != nil {
        return fmt.Errorf("json parse failed: %w", err)
    }
    
    // 🔹 Валідація параметрів
    for _, tr := range report.Tracks {
        if tr.Width > 0 {  // 🔹 Відео-доріжка
            if tr.Codec != "avc1.64001f" {
                return fmt.Errorf("unsupported codec: %s", tr.Codec)
            }
            if tr.Bitrate > 5_000_000 {
                log.Printf("⚠️  High bitrate: %d bps", tr.Bitrate)
            }
        }
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Автоматизація розділення на сегменти

```go
// 🔹 Функція для підготовки запису для стрімінгу
func prepareForStreaming(inputPath, outputDir string) error {
    // 🔹 Крок 1: Розділення на сегменти через alpha divide
    _, err := runMp4tool("alpha", "divide", inputPath, outputDir)
    if err != nil {
        return fmt.Errorf("divide failed: %w", err)
    }
    
    // 🔹 Крок 2: Валідація згенерованих плейлистів
    playlistPath := filepath.Join(outputDir, "playlist.m3u8")
    if _, err := os.Stat(playlistPath); os.IsNotExist(err) {
        return fmt.Errorf("playlist not generated: %s", playlistPath)
    }
    
    // 🔹 Крок 3: Додаткова обробка (напр. додавання метаданих)
    return addAnalyticsMetadata(outputDir)
}

// 🔹 Використання у конвеєрі:
go func() {
    for recording := range recordingQueue {
        if err := prepareForStreaming(recording.Input, recording.OutputDir); err != nil {
            log.Printf("❌ Failed to prepare %s: %v", recording.Input, err)
            continue
        }
        log.Printf("✅ Ready for streaming: %s", recording.OutputDir)
    }
}()
```

---

### 🔹 Приклад 3: Динамічне виклик команд на основі типу файлу

```go
// 🔹 Розумний вибір команди для обробки файлу
func processFile(filePath string) error {
    // 🔹 Спочатку швидкий аналіз через probe
    report, err := runMp4tool("probe", "-format=json", filePath)
    if err != nil {
        return fmt.Errorf("initial probe failed: %w", err)
    }
    
    var meta struct {
        FastStart bool `json:"fast_start"`
        Tracks    []*struct {
            Codec     string `json:"codec"`
            Encrypted bool   `json:"encrypted"`
        } `json:"tracks"`
    }
    
    if err := json.Unmarshal([]byte(report), &meta); err != nil {
        return err
    }
    
    // 🔹 Логіка обробки на основі метаданих
    if !meta.FastStart {
        log.Printf("⚠️  File not optimized for web — consider re-encoding")
    }
    
    for _, tr := range meta.Tracks {
        if tr.Encrypted {
            // 🔹 Зашифрований контент: витягнути PSSH для ліцензій
            pssh, err := runMp4tool("psshdump", filePath)
            if err != nil {
                return fmt.Errorf("psshdump failed: %w", err)
            }
            log.Printf("🔐 DRM config extracted:\n%s", pssh)
        }
        
        if strings.HasPrefix(tr.Codec, "avc1") {
            // 🔹 H.264 відео: перевірити роздільність
            if err := validateResolution(filePath); err != nil {
                return err
            }
        }
    }
    
    return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний порядок аргументів | Команда не розпізнається, показується довідка | Завжди передавайте команду першим аргументом: `mp4tool dump file.mp4`, не `mp4tool file.mp4 dump` |
| Забути префікс `alpha` для експериментальних команд | "unknown command" для `edit`/`divide` | Використовуйте `mp4tool alpha edit`, не `mp4tool edit` |
| Ігнорування коду виходу (`os.Exit`) | Скрипти продовжують виконання після помилки | Завжди перевіряйте код виходу: `if cmd.Run() != nil { ... }` |
| Неправильна обробка `stderr` | Повідомлення про помилки губляться | Перенаправляйте `stderr` для логування: `cmd.Stderr = &stderrBuf` |
| Виклик `main()` без ініціалізації | Panic через неініціалізовані залежності | Використовуйте `exec.Command` для ізольованого виклику, не імпортуйте `main` пакети |

---

## 📋 Чекліст для вашого проекту

```
[ ] При інтеграції mp4tool:
    • Використовуйте exec.Command для ізольованого виклику
    • Перехоплюйте stdout/stderr для обробки виводу та помилок
    • Перевіряйте код виходу для визначення успіху/невдачі

[ ] Для автоматизації:
    • Створюйте обгортки для часто використовуваних команд (probe, extract)
    • Кешуйте результати probe для уникнення повторного аналізу
    • Використовуйте таймаути для запобігання зависань на великих файлах

[ ] Для дебагу:
    • Логувайте повні команди: log.Printf("🔧 Running: mp4tool %v", args)
    • Зберігайте stderr для аналізу помилок
    • Тестуйте з різними типами файлів: звичайні, фрагментовані, DRM

[ ] Для безпеки:
    • Валідуйте вхідні шляхи до файлів перед передачею у mp4tool
    • Обмежуйте розмір файлів для обробки (напр. макс. 10 ГБ)
    • Не передавайте чутливі дані через аргументи командного рядка

[ ] Для масштабування:
    • Використовуйте пул воркерів для паралельної обробки файлів
    • Кешуйте бінарний файл mp4tool для уникнення повторних компіляцій
    • Моніторьте використання CPU/пам'яті під час масової обробки
```

---

## 🎯 Висновок

> **`main.go` — це "диригент" оркестру інструментів mp4tool**, який забезпечує:
> • ✅ Уніфікований CLI-інтерфейс для всіх функцій бібліотеки
> • ✅ Чітку маршрутизацію команд з підтримкою експериментальних функцій
> • ✅ Консистентну обробку помилок, довідки та кодів виходу
> • ✅ Знайомий патерн для користувачів (`git`/`docker`-подібний інтерфейс)
> • ✅ Легку розширюваність: додавання нових команд без зміни основної логіки

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Швидка інтеграція всіх інструментів аналізу через єдиний інтерфейс
- 🔧 Гнучка автоматизація: виклик будь-якої команди з ваших скриптів/сервісів
- 🛡️ Надійна обробка помилок: чіткі коди виходу та повідомлення у stderr
- 🔄 Легке масштабування: паралельна обробка тисяч файлів через exec.Command
- 📚 Зручна довідка: `mp4tool help` завжди під рукою для розробників

Потребуєте допомоги з інтеграцією mp4tool у ваш конвеєр обробки записів або з налаштуванням автоматизації через CLI? Напишіть — покажу готовий код для вашого сценарію! 🚀🛠️