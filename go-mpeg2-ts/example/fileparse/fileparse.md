# 🔍 Глибокий розбір коду: MPEG-2 TS парсер на Go

Цей код реалізує **командний інструмент** для парсингу MPEG-2 Transport Stream (TS) файлів з використанням бібліотеки `github.com/misodengaku/go-mpeg2-ts`. Розберемо архітектурно, детально та з критичним аналізом.

---

## 📦 Архітектура програми: огляд компонентів

```
┌─────────────────────────────────────┐
│ Програма: main.go                   │
│ Призначення: парсинг MPEG-2 TS      │
│ Бібліотека: go-mpeg2-ts             │
├─────────────────────────────────────┤
│ 🔹 Етапи обробки:                    │
│    1. Завантаження TS файлу         │
│    2. Парсинг PAT → пошук програм   │
│    3. Парсинг PMT → пошук стрімів   │
│    4. Фільтрація AVC відео стріму   │
│    5. PES парсинг → витяг ES даних  │
│    6. Опціональний дамп у файли     │
│    7. Перевірка continuity errors   │
│                                      │
│ 🔹 Глобальні змінні:                 │
│    • enableESDump — дамп даних      │
│    • disableCRCcheck — пропуск CRC  │
│    • mpeg2 — глобальний парсер      │
└─────────────────────────────────────┘
```

### 🎯 Контекст: MPEG-2 TS структура
```
📦 Transport Stream (TS) = послідовність 188-байтових пакетів
   │
   ├── PAT (Program Association Table) — PID 0x00
   │   └─> список програм → PMT PID
   │
   ├── PMT (Program Map Table) — PID з PAT
   │   └─> список стрімів (відео/аудіо) → Elementary PID
   │
   ├── PES (Packetized Elementary Stream) — PID з PMT
   │   └─> витягнуті медіа-дані (H.264/AAC тощо)
   │
   └── Adaptation Field — метадані пакету (PCR, сплайсинг тощо)
```

---

## 🔬 Детальний розбір основного потоку `main()`

### Етап 1: Завантаження TS файлу
```go
mpeg2, err = mpeg2ts.LoadStandardTS("test.ts")
if err != nil {
    panic(err)  // ⚠️ Жорстка обробка помилок
}
```

#### ⚠️ Проблеми
```go
// ❌ panic() у продакшен-коді:
// • Неможливо обробити помилку на вищому рівні
// • Завантажує весь файл у пам'ять → проблеми з великими файлами

// ✅ Правильно:
func loadTSFile(path string) (*mpeg2ts.MPEG2TS, error) {
    info, err := os.Stat(path)
    if err != nil {
        return nil, fmt.Errorf("failed to stat file: %w", err)
    }
    if info.Size() > maxFileSize {  // Напр. 10GB
        return nil, fmt.Errorf("file too large: %d bytes", info.Size())
    }
    return mpeg2ts.LoadStandardTS(path)
}

// Використання:
mpeg2, err := loadTSFile("test.ts")
if err != nil {
    log.Fatalf("Failed to load TS: %v", err)  // Контрольований вихід
}
```

---

### Етап 2-3: Пошук відео стріму через PAT → PMT

```go
var elementaryPID mpeg2ts.PID
patPackets := mpeg2.FilterByPIDs(mpeg2ts.PID_PAT)

for _, p := range patPackets.PacketList.All() {
    patTable, _ := p.ParsePAT()  // ⚠️ Помилка ігнорується!
    
    for _, program := range patTable.Programs {
        if program.ProgramNumber != 0 {  // Пропускаємо NIT (program 0)
            programTable := mpeg2.FilterByPIDs(program.ProgramMapPID)
            for _, pmtPacket := range programTable.PacketList.All() {
                pmt, err := pmtPacket.ParsePMT(disableCRCcheck)
                if err != nil {
                    fmt.Printf("ParsePMT failed. %s\n", err.Error())  // ⚠️ Продовжуємо без помилки!
                }
                
                // Пошук першого AVC стріму
                for _, s := range pmt.Streams {
                    if s.Type == mpeg2ts.StreamTypeAVC {
                        elementaryPID = s.ElementaryPID
                        break
                    }
                }
            }
        }
    }
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Ігнорування помилок парсингу:
patTable, _ := p.ParsePAT()  // Якщо помилка → patTable = nil → паніка далі!
// ✅ Правильно:
patTable, err := p.ParsePAT()
if err != nil {
    return fmt.Errorf("failed to parse PAT: %w", err)
}

// ❌ "Тихий" провал парсингу PMT:
if err != nil {
    fmt.Printf("ParsePMT failed. %s\n", err.Error())  // Логуємо, але продовжуємо
}
// → pmt може бути nil → паніка при pmt.Streams
// ✅ Правильно:
if err != nil {
    return fmt.Errorf("PMT parse failed for PID 0x%X: %w", program.ProgramMapPID, err)
}

// ❌ Жорсткий вибір першого AVC стріму:
// • Якщо є кілька відео-стрімів (напр. основний + PiP) → вибирається перший
// • Якщо AVC не знайдено → elementaryPID = 0 → помилка далі
// ✅ Правильно: дозволити вибір через конфігурацію або повернути список
func findVideoStream(pmt *mpeg2ts.PMT, preferredType mpeg2ts.StreamType) (mpeg2ts.PID, error) {
    for _, s := range pmt.Streams {
        if s.Type == preferredType {
            return s.ElementaryPID, nil
        }
    }
    // Fallback: будь-який відео-стрім
    for _, s := range pmt.Streams {
        if isVideoStreamType(s.Type) {
            return s.ElementaryPID, nil
        }
    }
    return 0, fmt.Errorf("no video stream found in PMT")
}
```

---

### Етап 4-5: PES парсинг та витяг Elementary Stream

```go
ctx := context.Background()
pesPackets := mpeg2.FilterByPIDs(elementaryPID)
pesParser := mpeg2ts.NewPESParser(1500)  // ⚠️ Hardcoded буфер

c := pesParser.StartPESReadLoop(ctx)  // 🎯 Запуск горутини для читання
wg := sync.WaitGroup{}
wg.Add(1)

go func() {
    i := 0
    for p := range c {
        fmt.Printf("%d: ES frame: %dbytes\n", i, len(p.ElementaryStream))
        if enableESDump {
            fname := fmt.Sprintf("output/es_%04d.bin", i)
            os.WriteFile(fname, p.ElementaryStream, 0644)  // ⚠️ Помилка запису ігнорується!
        }
        i++
    }
    wg.Done()
}()

// 🎯 Подача пакетів у парсер
packets := pesPackets.PacketList.All()
for i, p := range packets {
    if i < len(packets)-1 {
        err = pesParser.EnqueueTSPacket(p)
    } else {
        err = pesParser.EnqueueLastTSPacket(p)  // 🎯 Останній пакет — спеціальна обробка
    }
    if err != nil {
        panic(err)  // ⚠️ Знову panic!
    }
}
wg.Wait()  // 🎯 Чекаємо завершення горутини
```

#### ⚠️ Критичні проблеми
```go
// ❌ Hardcoded розмір буфера (1500):
pesParser := mpeg2ts.NewPESParser(1500)
// • Може бути замалим для великих PES-пакетів
// • Може бути завеликим → зайва пам'ять
// ✅ Правильно: конфігурувати або визначати динамічно
const DefaultPESBufferSize = 64 * 1024  // 64KB — розумний дефолт для відео
pesParser := mpeg2ts.NewPESParser(DefaultPESBufferSize)

// ❌ Ігнорування помилок запису файлу:
os.WriteFile(fname, p.ElementaryStream, 0644)  // Помилка → дані втрачені!
// ✅ Правильно:
if err := os.WriteFile(fname, p.ElementaryStream, 0644); err != nil {
    log.Printf("Failed to write ES dump %s: %v", fname, err)
    // Опціонально: зупинити дамп, але продовжити парсинг
}

// ❌ panic() при помилці Enqueue:
if err != nil {
    panic(err)  // Неможливо відновитися
}
// ✅ Правильно:
if err != nil {
    log.Printf("Failed to enqueue packet %d: %v", i, err)
    // Можна спробувати пропустити пошкоджений пакет або зупинитися
    continue  // або break, залежно від політики
}

// ❌ Відсутність обробки скасування через context:
ctx := context.Background()  // ❌ Не можна скасувати парсинг
// ✅ Правильно:
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
// Додати обробку сигналів для граціозного завершення:
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
go func() {
    <-sigCh
    log.Println("Received interrupt, stopping...")
    cancel()
}()
```

---

### Етап 6: Перевірка continuity

```go
func checkContinuity() {
    fmt.Print("Continuity check->")
    if cr := mpeg2.CheckStream(); cr.DropCount > 0 {
        fmt.Println("frame drop detected!!")
        for _, v := range cr.DropList {
            fmt.Printf("frame index: %d\n", v.Index)
        }
    } else {
        fmt.Println("OK")
    }
}
```

#### 🎯 Що таке continuity check?
```
📋 MPEG-2 TS пакети мають 4-бітовий continuity_counter (0-15)
• Зростає по модулю 16 для кожного PID
• Пропуск значення = втрата пакету (drop)
• Повтор значення = дублікат пакету

🔍 Навіщо перевіряти:
• Виявлення мережевих втрат при стрімінгу
• Діагностика пошкоджених файлів
• Моніторинг якості транспорту

⚠️ Обмеження:
• Не виявляє помилки всередині пакету (тільки втрату)
• Може хибно спрацювати при splicing/re-muxing
```

---

### Допоміжна функція `dumpPackets()` — відладка

```go
func dumpPackets(count int) {
    for i, p := range mpeg2.PacketList.All() {
        // 🎯 Вивід заголовка TS пакету
        fmt.Printf("%d sync:%x tei:%t pusi:%t tpi:%t pid:%x tsc:%d afc:%d cci:%d\r\n",
            i,
            p.SyncByte,  // Завжди 0x47
            p.TransportErrorIndicator,
            p.PayloadUnitStartIndicator,  // Важливо: початок PES
            p.TransportPriorityIndicator,
            p.PID,
            p.TransportScrambleControl,  // 0 = не зашифровано
            p.AdaptationFieldControl,
            p.ContinuityCheckIndex)
        
        // 🎯 Вивід Adaptation Field якщо є
        if p.HasAdaptationField() {
            fmt.Printf("\tAdaptationField dump: size:%d di:%t rai:%t espi:%t pcr:%t opcr:%t spf:%t tpdf:%t ef:%t\r\n",
                p.AdaptationField.Length,
                p.AdaptationField.DiscontinuityIndicator,
                p.AdaptationField.RandomAccessIndicator,  // Важливо для seek
                p.AdaptationField.ESPriorityIndicator,
                p.AdaptationField.PCRFlag,  // Program Clock Reference — синхронізація
                p.AdaptationField.OPCRFlag,
                p.AdaptationField.SplicingPointFlag,
                p.AdaptationField.TransportPrivateDataFlag,
                p.AdaptationField.ExtensionFlag)
        }
        if count > 0 && count-1 == i {
            break
        }
    }
}
```

#### 🎯 Ключові поля для відладки
| Поле | Призначення | Коли важливо |
|------|-------------|--------------|
| `PayloadUnitStartIndicator` | Початок нового PES/PSI | Парсинг структури |
| `AdaptationField.PCRFlag` | Наявність PCR для синхронізації | A/V синхронізація |
| `ContinuityCheckIndex` | Лічильник для continuity check | Виявлення втрат |
| `TransportScrambleControl` | 0 = не зашифровано | Перевірка DRM |

---

## ⚠️ Загальні проблеми програми

### 1️⃣ Глобальні змінні та стан
```go
// ❌ Глобальні змінні ускладнюють тестування та повторне використання:
var enableESDump = false
var disableCRCcheck = true
var mpeg2 *mpeg2ts.MPEG2TS  // Глобальний парсер

// Проблеми:
// • Неможливо запустити кілька парсерів паралельно
// • Тести змінюють глобальний стан → взаємний вплив
// • Важко ін'єктувати залежності (mocking)

// ✅ Рішення: структура конфігурації + локальний стан
type TSParserConfig struct {
    EnableESDump   bool
    DisableCRC     bool
    InputPath      string
    OutputDir      string
    PreferredCodec mpeg2ts.StreamType  // Напр. StreamTypeAVC
}

type TSParser struct {
    cfg    TSParserConfig
    mpeg2  *mpeg2ts.MPEG2TS
    logger *log.Logger
}

func NewTSParser(cfg TSParserConfig) (*TSParser, error) {
    mpeg2, err := mpeg2ts.LoadStandardTS(cfg.InputPath)
    if err != nil {
        return nil, err
    }
    return &TSParser{cfg: cfg, mpeg2: mpeg2, logger: log.Default()}, nil
}

func (p *TSParser) Parse() error {
    // Використовувати p.cfg, p.mpeg2, p.logger
}
```

### 2️⃣ Відсутність обробки помилок
```go
// ❌ Патерн, що повторюється:
patTable, _ := p.ParsePAT()  // Ігноруємо помилку
// ...
if err != nil {
    fmt.Printf("Error\n")  // Логуємо, але продовжуємо
}
// ...
panic(err)  // Або панікуємо

// ✅ Правильний патерн:
func (p *TSParser) findVideoStreamPID() (mpeg2ts.PID, error) {
    patPackets := p.mpeg2.FilterByPIDs(mpeg2ts.PID_PAT)
    for _, pkt := range patPackets.PacketList.All() {
        pat, err := pkt.ParsePAT()
        if err != nil {
            return 0, fmt.Errorf("PAT parse failed: %w", err)
        }
        // ... обробка ...
    }
    return 0, fmt.Errorf("video stream not found")
}
```

### 3️⃣ Пам'ять та продуктивність
```go
// ❌ LoadStandardTS завантажує ВЕСЬ файл у пам'ять:
// • "test.ts" 1GB → 1GB RAM
// • Великі файли → OOM

// ✅ Альтернативи:
// Варіант А: Stream-парсинг (якщо бібліотека підтримує)
mpeg2, err := mpeg2ts.LoadStandardTSStream(os.Open("test.ts"))  // Псевдокод

// Варіант Б: Обробка чанками
const chunkSize = 10 * 1024 * 1024  // 10MB
for offset := 0; offset < fileSize; offset += chunkSize {
    chunk := readChunk(file, offset, chunkSize)
    processChunk(chunk)
}

// Варіант В: Використовувати mmap для великих файлів
// (потребує змін у бібліотеці go-mpeg2-ts)
```

### 4️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу
// • Неможливо перевірити коректність парсингу
// • Неможливо покрити edge cases (пошкоджені пакети, незвичні PID тощо)

// ✅ Додати мінімальні тести:
func TestFindVideoStreamPID(t *testing.T) {
    // 🎯 Mock TS файл з відомою структурою
    parser := &TSParser{
        mpeg2: createMockMPEG2TS(),  // Helper для тестів
        cfg: TSParserConfig{PreferredCodec: mpeg2ts.StreamTypeAVC},
    }
    
    pid, err := parser.findVideoStreamPID()
    require.NoError(t, err)
    assert.Equal(t, mpeg2ts.PID(0x100), pid)  // Очікуваний PID
}
```

### 5️⃣ Жорстко закодовані шляхи та налаштування
```go
// ❌ Hardcoded "test.ts", "output/es_%04d.bin":
// • Неможливо використати з іншими файлами без зміни коду
// • Шлях "output/" може не існувати

// ✅ Правильно: CLI аргументи або конфігураційний файл
func main() {
    inputPath := flag.String("input", "test.ts", "Input TS file path")
    outputDir := flag.String("output", "output", "Output directory for ES dumps")
    enableDump := flag.Bool("dump", false, "Enable elementary stream dumping")
    flag.Parse()
    
    // 🎯 Створення директорії якщо треба
    if *enableDump {
        if err := os.MkdirAll(*outputDir, 0755); err != nil {
            log.Fatalf("Failed to create output dir: %v", err)
        }
    }
    
    cfg := TSParserConfig{
        InputPath:    *inputPath,
        OutputDir:    *outputDir,
        EnableESDump: *enableDump,
    }
    // ...
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **WebSocket-приймачем fMP4-фрагментів**:

### 🎯 Сценарій: валідація вхідного TS перед конвертацією у HLS
```go
// У вашому pipeline при отриманні TS-чанку:
func (p *TSProcessor) ValidateAndExtract(input []byte) (*VideoMetadata, error) {
    // 🎯 Тимчасовий файл для парсингу (або stream-парсинг якщо підтримується)
    tmpFile, err := os.CreateTemp("", "ts_*.ts")
    if err != nil {
        return nil, fmt.Errorf("failed to create temp file: %w", err)
    }
    defer os.Remove(tmpFile.Name())
    
    if _, err := tmpFile.Write(input); err != nil {
        return nil, fmt.Errorf("failed to write temp file: %w", err)
    }
    tmpFile.Close()
    
    // 🎯 Парсинг через TSParser
    parser, err := NewTSParser(TSParserConfig{
        InputPath:      tmpFile.Name(),
        PreferredCodec: mpeg2ts.StreamTypeAVC,
        DisableCRC:     true,  // Швидше, але менш надійно
    })
    if err != nil {
        return nil, fmt.Errorf("failed to init parser: %w", err)
    }
    
    // 🎯 Витяг метаданих
    metadata, err := parser.ExtractMetadata()
    if err != nil {
        return nil, fmt.Errorf("metadata extraction failed: %w", err)
    }
    
    // 🎯 Перевірка continuity для моніторингу якості
    continuity := parser.CheckContinuity()
    if continuity.DropCount > 0 {
        p.logger.Warn("TS packet drops detected", 
            "drops", continuity.DropCount,
            "channel", p.channelID)
        // Опціонально: відхилити пошкоджений чанк
    }
    
    return metadata, nil
}
```

### 🎯 Сценарій: моніторинг якості TS-транспорту
```go
// У monitoring.Monitor для аналізу вхідного TS:
func (m *Monitor) AnalyzeTSQuality(channelID string, tsData []byte) TSQualityReport {
    report := TSQualityReport{ChannelID: channelID}
    
    // 🎯 Швидкий парсинг заголовків без витягу ES
    parser, err := NewTSParser(TSParserConfig{
        InputPath:      "memory://",  // Псевдо-шлях для stream-парсингу
        DisableCRC:     true,
        SkipESExtraction: true,  // Новий прапорець для швидкого аналізу
    })
    if err != nil {
        report.Error = err.Error()
        return report
    }
    
    // 🎯 Збір статистики
    stats := parser.CollectStats()
    report.TotalPackets = stats.TotalPackets
    report.VideoPackets = stats.VideoPackets
    report.AudioPackets = stats.AudioPackets
    report.ContinuityErrors = stats.ContinuityErrors
    report.PCRJitter = stats.PCRJitter  // Важливо для A/V синхронізації
    
    // 🎯 Виявлення аномалій
    if report.ContinuityErrors > continuityErrorThreshold {
        m.alerts["ts_continuity_errors"].Inc()
        report.Status = "degraded"
    }
    if report.PCRJitter > pcrJitterThreshold {
        m.alerts["ts_pcr_jitter"].Inc()
        report.Status = "degraded"
    }
    
    return report
}
```

### 🎯 Сценарій: екстракція H.264 для подальшої транскодації
```go
// У транскодері для підготовки сегментів HLS:
func (t *Transcoder) ExtractH264Frames(tsData []byte) ([]H264Frame, error) {
    // 🎯 Парсинг TS → PES → H.264 NAL units
    parser, err := NewTSParser(TSParserConfig{
        InputPath:      "memory://",
        PreferredCodec: mpeg2ts.StreamTypeAVC,
        EnableESDump:   false,  // Не пишемо на диск, працюємо в пам'яті
    })
    if err != nil {
        return nil, err
    }
    
    frames, err := parser.ParseToH264Frames()
    if err != nil {
        return nil, fmt.Errorf("H.264 extraction failed: %w", err)
    }
    
    // 🎯 Фільтрація/обробка NAL units перед транскодацією
    var cleanFrames []H264Frame
    for _, frame := range frames {
        // Видалити SEI/NAL units, що не потрібні для транскодації
        if frame.Type != H264NAL_SEI {
            cleanFrames = append(cleanFrames, frame)
        }
    }
    
    return cleanFrames, nil
}
```

---

## 🧪 Приклад: рефакторинг з кращою обробкою помилок

```go
// ✅ Рефакторинг main() з структурованою обробкою:
func run(cfg Config) error {
    // 🎯 Ініціалізація парсера
    parser, err := NewTSParser(cfg)
    if err != nil {
        return fmt.Errorf("failed to init parser: %w", err)
    }
    defer parser.Close()  // Ресурси, якщо треба
    
    // 🎯 Пошук відео стріму
    videoPID, err := parser.FindVideoStreamPID(mpeg2ts.StreamTypeAVC)
    if err != nil {
        return fmt.Errorf("video stream detection failed: %w", err)
    }
    parser.logger.Printf("Found video stream at PID 0x%04X", videoPID)
    
    // 🎯 Налаштування PES парсера
    pesParser := mpeg2ts.NewPESParser(cfg.PESBufferSize)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // 🎯 Запуск обробника в окремій горутині
    resultCh := make(chan ParseResult, cfg.BufferSize)
    go parser.ProcessPESStream(ctx, videoPID, pesParser, resultCh)
    
    // 🎯 Обробка результатів
    for result := range resultCh {
        if result.Error != nil {
            parser.logger.Printf("Frame %d error: %v", result.Index, result.Error)
            continue
        }
        
        if cfg.EnableESDump {
            if err := saveElementaryStream(cfg.OutputDir, result.Index, result.Data); err != nil {
                parser.logger.Printf("Dump failed for frame %d: %v", result.Index, err)
            }
        }
        
        // 🎯 Тут можна відправити у ваш pipeline обробки
        // p.segmentAssembler.ProcessFrame(result.Data)
    }
    
    // 🎯 Фінальна перевірка
    report := parser.CheckContinuity()
    if report.DropCount > 0 {
        parser.logger.Warnf("Continuity errors detected: %d drops", report.DropCount)
    }
    
    return nil
}

func main() {
    cfg := parseConfig()  // CLI args або config file
    
    if err := run(cfg); err != nil {
        log.Fatalf("Execution failed: %v", err)
    }
}
```

---

## 📋 Best Practices для MPEG-2 TS парсингу

```
✅ Обробка помилок:
   • Ніколи не ігнорувати помилки парсингу PAT/PMT/PES
   • Використовувати error wrapping для контексту
   • Дозволити часткове відновлення (пропуск пошкоджених пакетів)

✅ Пам'ять та продуктивність:
   • Уникати завантаження всього файлу у пам'ять для великих TS
   • Використовувати stream-парсинг або chunked обробку
   • Налаштовувати розміри буферів під конкретні сценарії

✅ Конфігурація:
   • Винести hardcoded значення у CLI args або config file
   • Дозволити вибір кодеків, PID, шляхів виводу
   • Додати прапорці для відладки (verbose, dump, skip-crc)

✅ Моніторинг:
   • Збирати статистику: пакети, drops, PCR jitter
   • Логувати аномалії для подальшого аналізу
   • Інтегрувати з Prometheus/Grafana для production

✅ Тестування:
   • Додати юніт-тести з mock TS файлами
   • Покрити edge cases: пошкоджені пакети, незвичні PID
   • Додати інтеграційні тести з реальними фікстурами

✅ Безпека:
   • Валідувати вхідні дані перед парсингом
   • Обмежувати розмір оброблюваних файлів
   • Уникати path traversal у шляхах виводу
```

---

## 🎯 Висновок

Ця програма — **функціональний прототип** для парсингу MPEG-2 TS:

✅ Правильна послідовність: PAT → PMT → PES → ES  
✅ Використання `context` та `sync.WaitGroup` для конкурентності  
✅ Перевірка continuity для моніторингу якості

**Критичні виправлення перед продакшеном**:

1. ✅ **Замінити `panic()` на контрольовану обробку помилок**
2. ✅ **Додати CLI аргументи** замість hardcoded шляхів
3. ✅ **Уникати глобальних змінних** — використовувати структури конфігурації
4. ✅ **Додати перевірку помилок** для `ParsePAT`, `ParsePMT`, `WriteFile`
5. ✅ **Підтримувати stream-парсинг** для великих файлів
6. ✅ **Додати тести** для ключових функцій парсингу

**Приклад інтеграції у ваш CCTV pipeline**:
```go
// 🎯 TSProcessor для вашого WebSocket-приймача:
type TSProcessor struct {
    channelID string
    parser    *TSParser
    assembler *SegmentAssembler  // Ваш існуючий компонент
}

func (p *TSProcessor) ProcessTSChunk(data []byte) error {
    // 🎯 Швидка валідація та витяг метаданих
    metadata, err := p.parser.ExtractMetadata(data)
    if err != nil {
        return fmt.Errorf("metadata extraction failed: %w", err)
    }
    
    // 🎯 Якщо це новий стрім — ініціалізувати контекст
    if metadata.IsNewStream {
        p.assembler.InitStream(metadata.Codec, metadata.Timescale)
    }
    
    // 🎯 Відправка у ваш існуючий pipeline
    return p.assembler.ProcessElementaryStream(metadata.Data)
}

// Використання у WebSocket хендлері:
func (h *WSHandler) onTSFragment(channelID string, data []byte) {
    processor := h.getProcessor(channelID)
    if err := processor.ProcessTSChunk(data); err != nil {
        h.logger.Error("TS processing failed", 
            "channel", channelID, "error", err)
        // Опціонально: відправити алерт у monitoring
    }
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом коду з кращою обробкою помилок та конфігурацією?
- 🧠 Інтеграцією TS-парсингу у ваш існуючий `segmentAssembler` pipeline?
- 🧪 Написанням тестів для парсингу PAT/PMT/PES з mock даними?

Чекаю на ваші питання! 🛠️📡🎬