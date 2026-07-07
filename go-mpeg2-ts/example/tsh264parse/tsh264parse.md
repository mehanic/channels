# 🔍 Глибокий розбір коду: MPEG-2 TS → H.264 NALU екстрактор

Цей код реалізує **інструмент для витягу H.264 NAL units** з MPEG-2 Transport Stream файлів, з подальшою фільтрацією та збереженням у чистий .h264 файл. Розберемо архітектурно, детально та з критичним аналізом.

---

## 📦 Архітектура програми: огляд компонентів

```
┌─────────────────────────────────────┐
│ Програма: main.go (TS → H.264 extractor)│
│ Вхід: test.ts (MPEG-2 TS файл)      │
│ Вихід: dump.h264 (чистий H.264 stream)│
│ Бібліотеки:                         │
│   • go-mpeg2-ts — TS парсинг        │
│   • go-h264-parse — H.264 NALU парсинг│
├─────────────────────────────────────┤
│ 🔹 Етапи обробки:                    │
│    1. Завантаження TS файлу         │
│    2. PAT → PMT → пошук AVC PID     │
│    3. PES парсинг → Elementary Stream│
│    4. H.264 NALU парсинг            │
│    5. Фільтрація NALU (видалення AUD/SEI)│
│    6. Маршалінг → dump.h264         │
│    7. Continuity check              │
│                                      │
│ 🔹 Глобальні змінні:                 │
│    • enableESDump — дамп ES у файли │
│    • disableCRCcheck — пропуск CRC  │
│    • mpeg2 — глобальний TS парсер   │
└─────────────────────────────────────┘
```

### 🎯 Контекст: H.264 у MPEG-2 TS
```
📦 MPEG-2 TS → PES → Elementary Stream → H.264 NAL units
   │
   ├── TS пакет (188 байт)
   │   └─> PES пакет (змінна довжина)
   │       └─> Elementary Stream (H.264 NAL units)
   │           ├─> NAL Unit Header (1 байт)
   │           ├─> NAL Unit Type (5 біт)
   │           └─> Payload (залежить від типу)
   │
   🎯 Ключові NAL types:
   • 1-5: Слайси (IDR, non-IDR) — основні відео-дані
   • 6: SEI (Supplemental Enhancement Info) — метадані
   • 7-8: SPS/PPS — параметри декодування
   • 9: AUD (Access Unit Delimiter) — маркер початку фрейму
   • 10-12: End of sequence/stream
```

---

## 🔬 Детальний розбір основного потоку `main()`

### Етап 1-3: Пошук AVC стріму (PAT → PMT → PID)

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

### Етап 4: PES парсинг та H.264 NALU екстракція

```go
ctx := context.Background()
pesPackets := mpeg2.FilterByPIDs(elementaryPID)
pesParser := mpeg2ts.NewPESParser(1500)  // ⚠️ Hardcoded буфер

c := pesParser.StartPESReadLoop(ctx)
nalus := h264parse.NALUs{}  // 🎯 Колекція для всіх NAL units
wg := sync.WaitGroup{}
wg.Add(1)

go func() {
    i := 0
    for p := range c {
        fmt.Printf("ES frame: %dbytes\n", len(p.ElementaryStream))
        
        // 🎯 Опціональний дамп Elementary Stream
        if enableESDump {
            fname := fmt.Sprintf("output/es_%04d.bin", i)
            os.WriteFile(fname, p.ElementaryStream, 0644)  // ⚠️ Помилка запису ігнорується!
        }
        
        // 🎯 Парсинг H.264 NAL units
        n, err := h264parse.Unmarshal(p.ElementaryStream)
        if err != nil {
            panic(err)  // ❌ Жорстка обробка помилок
        }
        nalus.Units = append(nalus.Units, n.Units...)  // 🎯 Накопичення всіх NALU
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
        panic(err)  // ❌ Знову panic!
    }
}
wg.Wait()  // 🎯 Чекаємо завершення горутини
```

#### ⚠️ Критичні проблеми
```go
// ❌ Hardcoded розмір PES-буфера (1500):
pesParser := mpeg2ts.NewPESParser(1500)
// • Може бути замалим для великих PES-пакетів
// • Може бути завеликим → зайва пам'ять
// ✅ Правильно: конфігурувати або визначати динамічно
const DefaultPESBufferSize = 64 * 1024  // 64KB — розумний дефолт для відео
pesParser := mpeg2ts.NewPESParser(DefaultPESBufferSize)

// ❌ Накопичення ВСІХ NAL units у пам'яті:
nalus.Units = append(nalus.Units, n.Units...)  // Для довгого відео → OOM!
// ✅ Правильно: обробляти потоково або обмежувати розмір
const MaxNALUnits = 10000  // Розумний ліміт для тестування
if len(nalus.Units) < MaxNALUnits {
    nalus.Units = append(nalus.Units, n.Units...)
} else {
    log.Printf("NALU limit reached, dropping further units")
}

// ❌ panic() при помилці Unmarshal:
if err != nil {
    panic(err)  // Неможливо відновитися
}
// ✅ Правильно:
if err != nil {
    log.Printf("H.264 parse failed for frame %d: %v", i, err)
    continue  // Пропустити пошкоджений фрейм
}

// ❌ Ігнорування помилок запису файлу:
os.WriteFile(fname, p.ElementaryStream, 0644)  // Помилка → дані втрачені!
// ✅ Правильно:
if err := os.WriteFile(fname, p.ElementaryStream, 0644); err != nil {
    log.Printf("Failed to write ES dump %s: %v", fname, err)
}
```

---

### Етап 5: Фільтрація NAL units

```go
filteredNALUs := make([]h264parse.NAL, 0, len(nalus.Units))
for n, nal := range nalus.Units {
    fmt.Printf("%d:\t%s (%d)\n", n, nal.UnitType.String(), nal.UnitType)
    
    // 🎯 Видалення небажаних NAL types
    if nal.UnitType == h264parse.AccessUnitDelimiter {
        continue  // AUD — маркери початку фрейму, не потрібні для декодування
    }
    if nal.UnitType == h264parse.SupplementalEnhancementInformation {
        continue  // SEI — метадані, часто великі, не потрібні для базового декодування
    }
    filteredNALUs = append(filteredNALUs, nal)
}
nalus.Units = filteredNALUs
```

#### 🎯 Навіщо фільтрувати AUD та SEI?
```
📋 Access Unit Delimiter (AUD, type 9):
• Маркер початку нового access unit (фрейму)
• Корисний для синхронізації, але не обов'язковий для декодування
• Видалення зменшує розмір файлу без втрати якості

📋 Supplemental Enhancement Information (SEI, type 6):
• Додаткові метадані: таймштампи, інформація про контент, тощо
• Може бути великим (кілобайти) → значно збільшує розмір
• Не впливає на візуальну якість при видаленні

⚠️ Застереження:
• Деякі SEI можуть бути важливі (напр. для HDR, 3D)
• AUD може бути потрібен для певних плеєрів/декодерів
• Фільтрація має бути конфігурованою, не жорсткою
```

#### ⚠️ Потенційні покращення
```go
// ❌ Жорстке видалення без конфігурації:
if nal.UnitType == h264parse.AccessUnitDelimiter { continue }
// ✅ Правильно: дозволити налаштування через прапорці
type NALUFilterConfig struct {
    RemoveAUD bool
    RemoveSEI bool
    KeepSPSPPS bool  // Важливо: SPS/PPS потрібні для ініціалізації декодера
}

func shouldKeepNALU(nal h264parse.NAL, cfg NALUFilterConfig) bool {
    switch nal.UnitType {
    case h264parse.AccessUnitDelimiter:
        return !cfg.RemoveAUD
    case h264parse.SupplementalEnhancementInformation:
        return !cfg.RemoveSEI
    case h264parse.SPS, h264parse.PPS:
        return cfg.KeepSPSPPS  // Зазвичай true!
    default:
        return true  // Зберігати всі слайси та інші типи
    }
}
```

---

### Етап 6: Маршалінг та збереження

```go
nb, _ := h264parse.Marshal(nalus)  // ⚠️ Помилка маршалінгу ігнорується!
os.WriteFile("dump.h264", nb, 0755)  // 🎯 Збереження чистого H.264 stream
```

#### ⚠️ Критичні проблеми
```go
// ❌ Ігнорування помилки Marshal:
nb, _ := h264parse.Marshal(nalus)  // Якщо помилка → nb = nil → запис порожнього файлу!
// ✅ Правильно:
nb, err := h264parse.Marshal(nalus)
if err != nil {
    log.Fatalf("Failed to marshal H.264 NALUs: %v", err)
}

// ❌ Hardcoded ім'я файлу "dump.h264":
// • Неможливо використати з іншими файлами без зміни коду
// • Може перезаписати існуючий файл
// ✅ Правильно: CLI аргументи або конфігурація
outputPath := flag.String("output", "dump.h264", "Output H.264 file path")
flag.Parse()
if err := os.WriteFile(*outputPath, nb, 0644); err != nil {
    log.Fatalf("Failed to write output file: %v", err)
}

// ❌ Права доступу 0755 для файлу даних:
os.WriteFile("dump.h264", nb, 0755)  // 755 = rwxr-xr-x (виконуваний!)
// ✅ Правильно: 0644 = rw-r--r-- (тільки читання/запис для власника)
os.WriteFile("dump.h264", nb, 0644)
```

---

### Етап 7: Continuity Check

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

## ⚠️ Загальні проблеми програми

### 1️⃣ Глобальні змінні та стан
```go
// ❌ Глобальні змінні ускладнюють тестування та повторне використання:
var enableESDump = false
var disableCRCcheck = false
var mpeg2 *mpeg2ts.MPEG2TS  // Глобальний парсер

// Проблеми:
// • Неможливо запустити кілька парсерів паралельно
// • Тести змінюють глобальний стан → взаємний вплив
// • Важко ін'єктувати залежності (mocking)

// ✅ Рішення: структура конфігурації + локальний стан
type H264ExtractorConfig struct {
    InputPath      string
    OutputPath     string
    EnableESDump   bool
    DisableCRC     bool
    FilterConfig   NALUFilterConfig
}

type H264Extractor struct {
    cfg    H264ExtractorConfig
    mpeg2  *mpeg2ts.MPEG2TS
    logger *log.Logger
}

func NewH264Extractor(cfg H264ExtractorConfig) (*H264Extractor, error) {
    mpeg2, err := mpeg2ts.LoadStandardTS(cfg.InputPath)
    if err != nil {
        return nil, err
    }
    return &H264Extractor{cfg: cfg, mpeg2: mpeg2, logger: log.Default()}, nil
}

func (e *H264Extractor) Extract() error {
    // Використовувати e.cfg, e.mpeg2, e.logger
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
func (e *H264Extractor) findVideoStreamPID() (mpeg2ts.PID, error) {
    patPackets := e.mpeg2.FilterByPIDs(mpeg2ts.PID_PAT)
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
    extractor := &H264Extractor{
        mpeg2: createMockMPEG2TS(),  // Helper для тестів
        cfg: H264ExtractorConfig{FilterConfig: NALUFilterConfig{RemoveAUD: true}},
    }
    
    pid, err := extractor.findVideoStreamPID()
    require.NoError(t, err)
    assert.Equal(t, mpeg2ts.PID(0x100), pid)  // Очікуваний PID
}
```

### 5️⃣ Жорстко закодовані шляхи та налаштування
```go
// ❌ Hardcoded "test.ts", "dump.h264", "output/es_%04d.bin":
// • Неможливо використати з іншими файлами без зміни коду
// • Шлях "output/" може не існувати

// ✅ Правильно: CLI аргументи або конфігураційний файл
func main() {
    inputPath := flag.String("input", "test.ts", "Input TS file path")
    outputPath := flag.String("output", "dump.h264", "Output H.264 file path")
    enableDump := flag.Bool("dump", false, "Enable elementary stream dumping")
    flag.Parse()
    
    // 🎯 Створення директорії якщо треба
    if *enableDump {
        if err := os.MkdirAll("output", 0755); err != nil {
            log.Fatalf("Failed to create output dir: %v", err)
        }
    }
    
    cfg := H264ExtractorConfig{
        InputPath:    *inputPath,
        OutputPath:   *outputPath,
        EnableESDump: *enableDump,
    }
    // ...
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **WebSocket-приймачем fMP4-фрагментів**:

### 🎯 Сценарій: валідація вхідного H.264 перед конвертацією у HLS
```go
// У вашому pipeline при отриманні H.264 NAL units:
func (p *H264Processor) ValidateAndExtract(input []byte) (*VideoMetadata, error) {
    // 🎯 Парсинг H.264 NAL units
    nalus, err := h264parse.Unmarshal(input)
    if err != nil {
        return nil, fmt.Errorf("H.264 parse failed: %w", err)
    }
    
    // 🎯 Витяг метаданих з SPS/PPS
    var metadata VideoMetadata
    for _, nal := range nalus.Units {
        switch nal.UnitType {
        case h264parse.SPS:
            sps, err := h264parse.ParseSPS(nal.Payload)
            if err != nil {
                return nil, fmt.Errorf("SPS parse failed: %w", err)
            }
            metadata.Width = sps.Width
            metadata.Height = sps.Height
            metadata.Codec = fmt.Sprintf("avc1.%02x%02x%02x", 
                sps.Profile, sps.Compatibility, sps.Level)
            
        case h264parse.PPS:
            // PPS може містити додаткові параметри
        }
    }
    
    // 🎯 Фільтрація за конфігурацією
    filtered := filterNALUs(nalus.Units, p.cfg.FilterConfig)
    
    return &metadata, nil
}
```

### 🎯 Сценарій: генерація fMP4 сегментів з H.264 NAL units
```go
// У segmentFinalizer для створення fMP4:
func (sf *SegmentFinalizer) createFMP4Segment(nalus []h264parse.NAL, seqNum int) ([]byte, error) {
    // 🎯 Ініціалізація fMP4 (якщо перший сегмент)
    if seqNum == 1 {
        initSegment, err := sf.generateInitSegment(nalus)  // SPS/PPS у moov
        if err != nil {
            return nil, err
        }
        sf.initSegment = initSegment
    }
    
    // 🎯 Генерація media сегмента з NAL units
    mediaSegment, err := sf.generateMediaSegment(nalus, seqNum)
    if err != nil {
        return nil, fmt.Errorf("media segment generation failed: %w", err)
    }
    
    return mediaSegment, nil
}

// Використання у pipeline:
func (p *H264Processor) ProcessNALUs(channelID string, nalus []h264parse.NAL) error {
    finalizer := p.getSegmentFinalizer(channelID)
    
    segment, err := finalizer.createFMP4Segment(nalus, p.nextSeqNum)
    if err != nil {
        return fmt.Errorf("segment creation failed: %w", err)
    }
    
    // 🎯 Відправка у ваш WebSocketDistributor
    return p.distributor.SendSegment(channelID, segment, p.nextSeqNum)
}
```

### 🎯 Сценарій: моніторинг якості H.264 потоку
```go
// У monitoring.Monitor для аналізу H.264 якості:
type H264QualityReport struct {
    ChannelID      string
    TotalNALUs     int
    IDRCount       int  // Кількість ключових кадрів
    SPSPPSPresent  bool  // Наявність параметрів декодування
    AvgNALUSize    int
    Status         string  // "ok", "degraded", "failed"
}

func (m *Monitor) AnalyzeH264Quality(channelID string, nalus []h264parse.NAL) H264QualityReport {
    report := H264QualityReport{ChannelID: channelID, TotalNALUs: len(nalus)}
    
    var totalSize int
    for _, nal := range nalus {
        totalSize += len(nal.Payload)
        switch nal.UnitType {
        case h264parse.IDR:
            report.IDRCount++
        case h264parse.SPS, h264parse.PPS:
            report.SPSPPSPresent = true
        }
    }
    
    if report.TotalNALUs > 0 {
        report.AvgNALUSize = totalSize / report.TotalNALUs
    }
    
    // 🎯 Визначення статусу
    if !report.SPSPPSPresent {
        report.Status = "failed"  // Неможливо декодувати без SPS/PPS
        m.alerts["h264_missing_spspps"].Inc()
    } else if report.IDRCount == 0 && report.TotalNALUs > 100 {
        report.Status = "degraded"  // Довгий потік без ключових кадрів
        m.alerts["h264_no_idr"].Inc()
    } else {
        report.Status = "ok"
    }
    
    return report
}
```

---

## 🧪 Приклад: рефакторинг з кращою обробкою помилок

```go
// ✅ Рефакторинг main() з структурованою обробкою:
func run(cfg Config) error {
    // 🎯 Ініціалізація екстрактора
    extractor, err := NewH264Extractor(cfg)
    if err != nil {
        return fmt.Errorf("failed to init extractor: %w", err)
    }
    
    // 🎯 Пошук відео стріму
    videoPID, err := extractor.FindVideoStreamPID(mpeg2ts.StreamTypeAVC)
    if err != nil {
        return fmt.Errorf("video stream detection failed: %w", err)
    }
    extractor.logger.Printf("Found video stream at PID 0x%04X", videoPID)
    
    // 🎯 Екстракція та фільтрація NAL units
    nalus, err := extractor.ExtractNALUs(videoPID)
    if err != nil {
        return fmt.Errorf("NALU extraction failed: %w", err)
    }
    
    // 🎯 Фільтрація за конфігурацією
    filtered := filterNALUs(nalus, cfg.FilterConfig)
    
    // 🎯 Маршалінг та збереження
    output, err := h264parse.Marshal(h264parse.NALUs{Units: filtered})
    if err != nil {
        return fmt.Errorf("H.264 marshal failed: %w", err)
    }
    
    if err := os.WriteFile(cfg.OutputPath, output, 0644); err != nil {
        return fmt.Errorf("failed to write output: %w", err)
    }
    
    // 🎯 Фінальна перевірка
    report := extractor.CheckContinuity()
    if report.DropCount > 0 {
        extractor.logger.Warnf("Continuity errors detected: %d drops", report.DropCount)
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

## 📋 Best Practices для H.264 екстракції

```
✅ Обробка помилок:
   • Ніколи не ігнорувати помилки парсингу PAT/PMT/PES/H.264
   • Використовувати error wrapping для контексту
   • Дозволити часткове відновлення (пропуск пошкоджених NALU)

✅ Пам'ять та продуктивність:
   • Уникати накопичення всіх NALU у пам'яті для великих файлів
   • Використовувати stream-обробку або chunked парсинг
   • Налаштовувати розміри буферів під конкретні сценарії

✅ Фільтрація NALU:
   • Зробити фільтрацію конфігурованою, не жорсткою
   • Зберігати SPS/PPS обов'язково (потрібні для декодування)
   • Документувати вплив видалення AUD/SEI на сумісність

✅ Конфігурація:
   • Винести hardcoded значення у CLI args або config file
   • Дозволити вибір кодеків, PID, шляхів виводу
   • Додати прапорці для відладки (verbose, dump, skip-crc)

✅ Тестування:
   • Додати юніт-тести з mock TS файлами
   • Покрити edge cases: пошкоджені NALU, незвичні PID
   • Додати інтеграційні тести з реальними фікстурами

✅ Безпека:
   • Валідувати вхідні дані перед парсингом
   • Обмежувати розмір оброблюваних файлів
   • Уникати path traversal у шляхах виводу
```

---

## 🎯 Висновок

Ця програма — **функціональний інструмент** для екстракції H.264 з MPEG-2 TS:

✅ Правильна послідовність: TS → PES → ES → H.264 NALU  
✅ Фільтрація небажаних NAL types для зменшення розміру  
✅ Continuity check для моніторингу якості

**Критичні виправлення перед продакшеном**:

1. ✅ **Замінити `panic()` на контрольовану обробку помилок**
2. ✅ **Додати CLI аргументи** замість hardcoded шляхів
3. ✅ **Уникати глобальних змінних** — використовувати структури конфігурації
4. ✅ **Додати перевірку помилок** для `ParsePAT`, `ParsePMT`, `Unmarshal`, `Marshal`
5. ✅ **Підтримувати stream-обробку** для великих файлів (не накопичувати всі NALU)
6. ✅ **Зробити фільтрацію NALU конфігурованою**, не жорсткою
7. ✅ **Додати тести** для ключових функцій парсингу

**Приклад інтеграції у ваш CCTV pipeline**:
```go
// 🎯 H264Processor для вашого WebSocket-приймача:
type H264Processor struct {
    channelID string
    extractor *H264Extractor
    assembler *SegmentAssembler  // Ваш існуючий компонент
}

func (p *H264Processor) ProcessH264Chunk(data []byte) error {
    // 🎯 Парсинг та фільтрація NAL units
    nalus, err := h264parse.Unmarshal(data)
    if err != nil {
        return fmt.Errorf("H.264 parse failed: %w", err)
    }
    
    // 🎯 Фільтрація за конфігурацією
    filtered := filterNALUs(nalus.Units, p.cfg.FilterConfig)
    
    // 🎯 Відправка у ваш існуючий pipeline
    return p.assembler.ProcessNALUnits(filtered)
}

// Використання у WebSocket хендлері:
func (h *WSHandler) onH264Fragment(channelID string, data []byte) {
    processor := h.getProcessor(channelID)
    if err := processor.ProcessH264Chunk(data); err != nil {
        h.logger.Error("H.264 processing failed", 
            "channel", channelID, "error", err)
        // Опціонально: відправити алерт у monitoring
    }
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом коду з кращою обробкою помилок та конфігурацією?
- 🧠 Інтеграцією H.264 NALU обробки у ваш існуючий `segmentAssembler` pipeline?
- 🧪 Написанням тестів для парсингу PAT/PMT/PES та H.264 NALU з mock даними?

Чекаю на ваші питання! 🛠️🎬📡