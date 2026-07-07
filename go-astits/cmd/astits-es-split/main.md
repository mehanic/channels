# 🔧 Глибокий розбір: TS Demuxer/Splitter CLI (astits)

Цей файл — **командно-рядковий інструмент для демуксингу MPEG-TS файлів** за допомогою бібліотеки `astits`. Він розділяє один `.ts` файл на окремі файли для кожного елементарного потоку (відео, аудіо, субтитри тощо). Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема інструменту

```
┌────────────────────────────────────────┐
│ 📦 TS Demuxer CLI — основні етапи       │
├────────────────────────────────────────┤
│                                         │
│  📥 Вхід:                               │
│  • INPUT.ts — MPEG Transport Stream    │
│                                         │
│  ⚙️  Обробка:                            │
│  1. Читання PAT (Program Association Table) │
│  2. Читання PMT (Program Map Tables)   │
│  3. Виявлення елементарних потоків (ES)│
│  4. Створення muxer для кожного PID    │
│  5. Перенаправлення пакетів за PID     │
│                                         │
│  📤 Вихід:                              │
│  • out/{PID}.ts — окремі файли потоків │
│  • Підтримка --discard для профілювання│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔍 Детальний розбір ключових компонентів

### 1️⃣ `muxerOut` — буферизований запис у файл

```go
type muxerOut struct {
    name   string      // ім'я файлу для логування
    closer io.Closer   // для закриття файлу
    *bufio.Writer      // буферизований запис (10MB буфер)
}

func newMuxerOut(name string, discard bool) (*muxerOut, error) {
    var w io.Writer
    var c io.Closer
    if !discard {
        // Створення реального файлу
        f, err := os.Create(name)
        // ...
        w = f
        c = f
    } else {
        // Режим профілювання: запис у /dev/null
        name += " --discard--"
        w = io.Discard
    }
    return &muxerOut{name, c, bufio.NewWriterSize(w, ioBufSize)}, nil
}
```

### ✅ Ваш use-case: стрімінговий запис без файлів

```go
// У вашому pipeline: запис у memory buffer або WebSocket
type StreamMuxerOut struct {
    channelID string
    wsSender  *WSSender
    buffer    *bytes.Buffer
}

func (s *StreamMuxerOut) Write(p []byte) (int, error) {
    // Відправка даних у реальний час через WebSocket
    msg := &StreamMessage{
        ChannelID: s.channelID,
        PID:       s.pid,
        Data:      p,
    }
    s.wsSender.Broadcast(s.channelID, msg)
    
    // Також зберігаємо у буфер для локальної обробки
    return s.buffer.Write(p)
}

func (s *StreamMuxerOut) Close() error {
    // Фіналізація: відправка EOF-маркера або метаданих
    return nil
}
```

---

### 2️⃣ PAT/PMT парсинг — виявлення потоків

```go
// PAT (Program Association Table) — карта програм → PMT PID
if d.PAT != nil {
    pat = d.PAT
    gotAllPMTs = false  // скидаємо прапорець, чекаємо нові PMT
    continue
}

// PMT (Program Map Table) — карта програми → елементарні потоки
if d.PMT != nil {
    pmts[d.PMT.ProgramNumber] = d.PMT
    
    // Перевірка чи отримали всі PMT з PAT
    gotAllPMTs = true
    for _, p := range pat.Programs {
        if _, ok := pmts[p.ProgramNumber]; !ok {
            gotAllPMTs = false
            break
        }
    }
    if !gotAllPMTs {
        continue  // чекаємо решту PMT
    }
    
    // Ініціалізація muxer для кожного елементарного потоку
    for _, es := range pmt.ElementaryStreams {
        if _, ok := muxers[es.ElementaryPID]; ok {
            continue  // вже створено
        }
        
        // Створення вихідного файлу/буфера
        esFilename := path.Join(*outDir, fmt.Sprintf("%d.ts", es.ElementaryPID))
        outWriter, _ := newMuxerOut(esFilename, *discard)
        
        // Створення astits.Muxer для цього PID
        mux := astits.NewMuxer(context.Background(), outWriter)
        mux.AddElementaryStream(*es)  // реєстрація формату потоку
        mux.SetPCRPID(es.ElementaryPID)  // синхронізація часу
        muxers[es.ElementaryPID] = mux
    }
    continue
}
```

### 📊 Типи елементарних потоків (StreamType):

```go
// З астис: astits.StreamType
0x02 → MPEG-2 Video
0x0F → AAC Audio
0x1B → H.264/AVC Video
0x24 → H.265/HEVC Video
0x06 → PES with private data (часто телетекст/субтитри)
0x05 → Private sections (часто телетекст)
```

### ✅ Ваш use-case: фільтрація телетекст-потоків

```go
// У вашому процесорі: виявлення телетекст/субтитрів потоків
func isTeletextStream(es *astits.ElementaryStream) bool {
    // Перевірка за StreamType
    if es.StreamType == 0x06 || es.StreamType == 0x05 {
        return true
    }
    
    // Перевірка за дескрипторами
    for _, desc := range es.ElementaryStreamDescriptors {
        if desc.Tag == astits.DescriptorTagTeletext || 
           desc.Tag == astits.DescriptorTagVBITeletext {
            return true
        }
    }
    return false
}

// Створення muxer тільки для потрібних потоків
if isTeletextStream(es) {
    // Створюємо muxer для подальшої обробки субтитрів
    muxer := astits.NewMuxer(ctx, &SubtitleStreamHandler{
        channelID: channelID,
        processor: p,
    })
    muxers[es.ElementaryPID] = muxer
}
```

---

### 3️⃣ Перенаправлення PES-пакетів

```go
// Після отримання всіх PMT: обробка даних
if !gotAllPMTs {
    continue  // пропускаємо пакети до повної ініціалізації
}

if d.PES == nil {
    continue  // не PES-дані (напр. PSI/SI таблиці)
}

pid := d.FirstPacket.Header.PID
mux, ok := muxers[pid]
if !ok {
    log.Printf("Got payload for unknown PID %d", pid)
    continue  // невідомий потік — ігноруємо
}

// Обробка PCR (Program Clock Reference) для синхронізації
af := d.FirstPacket.AdaptationField
if af != nil && af.HasPCR {
    af.HasPCR = false  // видаляємо старий PCR, щоб уникнути дублікатів
}

// Перенос PTS/DTS у AdaptationField як PCR (для muxer)
var pcr *astits.ClockReference
switch d.PES.Header.OptionalHeader.PTSDTSIndicator {
case astits.PTSDTSIndicatorOnlyPTS:
    pcr = d.PES.Header.OptionalHeader.PTS
case astits.PTSDTSIndicatorBothPresent:
    pcr = d.PES.Header.OptionalHeader.DTS  // DTS має пріоритет для синхронізації
}

if pcr != nil {
    if af == nil {
        af = &astits.PacketAdaptationField{}
    }
    af.HasPCR = true
    af.PCR = pcr
}

// Запис у відповідний muxer
written, err := mux.WriteData(&astits.MuxerData{
    PID:             pid,
    AdaptationField: af,
    PES:             d.PES,
})
```

### 🔑 Ключові моменти синхронізації:

| Поле | Призначення | Важливість |
|------|-------------|------------|
| **PCR** | Program Clock Reference — основний таймінг програми | Критично для A/V синхронізації |
| **PTS** | Presentation Time Stamp — коли показувати кадр | Важливо для відтворення |
| **DTS** | Decoding Time Stamp — коли декодувати кадр | Важливо для B-frames у відео |

> 💡 **Порада**: У вашому HLS-процесорі використовуйте PTS для синхронізації субтитрів з відео, оскільки субтитри не потребують декодування.

---

### 4️⃣ Профілювання та метрики

```go
// CPU/Memory профілювання через pkg/profile
if *cpuProfiling {
    defer profile.Start(profile.CPUProfile, profile.ProfilePath(".")).Stop()
} else if *memoryProfiling {
    defer profile.Start(profile.MemProfile, profile.ProfilePath(".")).Stop()
}

// Метрики продуктивності
timeStarted := time.Now()
bytesWritten := 0
// ... у циклі обробки:
bytesWritten += written

// Фінальний звіт
timeDiff := time.Since(timeStarted)
log.Printf("%d bytes written at rate %.02f mb/s", 
    bytesWritten, 
    (float64(bytesWritten)/1024.0/1024.0)/timeDiff.Seconds())
```

### ✅ Ваш use-case: інтеграція з Prometheus

```go
// У вашому процесорі: експорт метрик для моніторингу
var (
    tsBytesProcessed = prometheus.NewCounterVec(
        prometheus.CounterOpts{Name: "ts_bytes_processed_total"},
        []string{"channel", "pid"},
    )
    tsProcessingRate = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{Name: "ts_processing_rate_mbps"},
        []string{"channel"},
    )
)

// У циклі обробки:
func (p *TSProcessor) recordMetrics(pid uint16, bytes int) {
    tsBytesProcessed.WithLabelValues(p.channelID, fmt.Sprintf("%d", pid)).Add(float64(bytes))
    
    // Розрахунок поточної швидкості (ковзне середнє)
    rate := calculateMovingAverage(p.channelID, bytes)
    tsProcessingRate.WithLabelValues(p.channelID).Set(rate)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// ts_subtitle_extractor.go — витягування субтитрів з TS потоку
type TSSubtitleExtractor struct {
    channelID    string
    teletextPID  uint16
    processor    *SubtitleProcessor
    demux        *astits.Demuxer
    muxers       map[uint16]*astits.Muxer
    gotAllPMTs   bool
    pat          *astits.PATData
    pmts         map[uint16]*astits.PMTData
}

func NewTSSubtitleExtractor(channelID string, pid uint16, processor *SubtitleProcessor) *TSSubtitleExtractor {
    return &TSSubtitleExtractor{
        channelID:   channelID,
        teletextPID: pid,
        processor:   processor,
        muxers:      make(map[uint16]*astits.Muxer),
        pmts:        make(map[uint16]*astits.PMTData),
    }
}

// ProcessStream — головна точка входу для обробки TS потоку
func (e *TSSubtitleExtractor) ProcessStream(ctx context.Context, r io.Reader) error {
    e.demux = astits.NewDemuxer(ctx, bufio.NewReaderSize(r, 10*1024*1024))
    
    var d *astits.DemuxerData
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        if d, err = e.demux.NextData(); err != nil {
            if errors.Is(err, astits.ErrNoMorePackets) {
                break
            }
            return fmt.Errorf("demux error: %w", err)
        }
        
        if err := e.handleData(d); err != nil {
            return fmt.Errorf("handle data error: %w", err)
        }
    }
    return nil
}

// handleData — обробка одного демуксованого елемента
func (e *TSSubtitleExtractor) handleData(d *astits.DemuxerData) error {
    // 1. Обробка PAT/PMT
    if d.PAT != nil {
        e.pat = d.PAT
        e.gotAllPMTs = false
        return nil
    }
    
    if d.PMT != nil {
        return e.handlePMT(d.PMT)
    }
    
    // 2. Пропуск даних до отримання всіх PMT
    if !e.gotAllPMTs {
        return nil
    }
    
    // 3. Обробка тільки телетекст-потоків
    if d.PES == nil {
        return nil
    }
    
    pid := d.FirstPacket.Header.PID
    if pid != e.teletextPID {
        return nil  // ігноруємо не-телетекст потоки
    }
    
    // 4. Відправка телетекст-даних у процесор субтитрів
    return e.processor.ProcessTeletextPES(d.PES, d.FirstPacket)
}

// handlePMT — ініціалізація після отримання PMT
func (e *TSSubtitleExtractor) handlePMT(pmt *astits.PMTData) error {
    e.pmts[pmt.ProgramNumber] = pmt
    
    // Перевірка чи отримали всі PMT
    e.gotAllPMTs = true
    for _, p := range e.pat.Programs {
        if _, ok := e.pmts[p.ProgramNumber]; !ok {
            e.gotAllPMTs = false
            break
        }
    }
    if !e.gotAllPMTs {
        return nil
    }
    
    // Логування знайдених потоків
    log.Printf("Channel %s: found %d elementary streams", e.channelID, len(pmt.ElementaryStreams))
    for _, es := range pmt.ElementaryStreams {
        log.Printf("  PID %d: type=%s, descriptors=%d", 
            es.ElementaryPID, es.StreamType.String(), len(es.ElementaryStreamDescriptors))
        
        // Якщо це телетекст — створюємо muxer для подальшої обробки
        if isTeletextStream(es) {
            mux := astits.NewMuxer(context.Background(), &SubtitleStreamHandler{
                channelID: e.channelID,
                processor: e.processor,
            })
            mux.AddElementaryStream(*es)
            mux.SetPCRPID(es.ElementaryPID)
            e.muxers[es.ElementaryPID] = mux
            log.Printf("  → Registered teletext handler for PID %d", es.ElementaryPID)
        }
    }
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"unknown PID" у логах** | Потік не зареєстрований у PMT | Переконайтеся, що телетекст PID вказано правильно або авто-детект через дескриптори |
| **Субтитри не синхронізовані** | Відсутній/некоректний PCR/PTS | Переносьте PTS у AdaptationField як PCR перед записом у muxer |
| **Повільна обробка** | Великі буфери або блокуючий I/O | Використовуйте `bufio.NewReaderSize` з 10MB буфером, асинхронну обробку |
| **Пам'ять росте** | Не закриті muxers або буфери | Використовуйте `defer outWriter.Close()` та контекст для скасування |
| **Пропуск пакетів при старті** | Обробка до отримання всіх PMT | Пропускайте дані поки `!gotAllPMTs`, як у прикладі |

---

## ⚡ Оптимізації для real-time обробки

### 1. Асинхронна обробка потоків:

```go
// Замість послідовної обробки:
func (e *TSSubtitleExtractor) handleDataAsync(d *astits.DemuxerData) {
    go func(data *astits.DemuxerData) {
        _ = e.handleData(data)  // обробка у окремій горутині
    }(d)
}
```

### 2. Кешування muxers за PID:

```go
// muxers вже кешуються у map[uint16]*astits.Muxer
// Додайте TTL для автоматичного очищення неактивних потоків
type CachedMuxer struct {
    mux       *astits.Muxer
    lastUsed  time.Time
}

func (e *TSSubtitleExtractor) getMuxer(pid uint16) *astits.Muxer {
    if cached, ok := e.muxers[pid]; ok {
        cached.lastUsed = time.Now()
        return cached.mux
    }
    return nil
}

// Періодичне очищення (у окремій горутині)
func (e *TSSubtitleExtractor) startCleanupTicker(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            cutoff := time.Now().Add(-10 * time.Minute)
            for pid, cached := range e.muxers {
                if cached.lastUsed.Before(cutoff) {
                    delete(e.muxers, pid)
                    log.Printf("Cleaned up inactive muxer for PID %d", pid)
                }
            }
        }
    }
}
```

### 3. Пакетна обробка PES-даних:

```go
// Замість індивідуального WriteData для кожного пакета:
func (e *TSSubtitleExtractor) batchWriteData(pid uint16, packets []*astits.DemuxerData) error {
    mux, ok := e.muxers[pid]
    if !ok {
        return fmt.Errorf("unknown PID %d", pid)
    }
    
    for _, d := range packets {
        // Оптимізована підготовка даних
        af := prepareAdaptationField(d)
        
        if _, err := mux.WriteData(&astits.MuxerData{
            PID:             pid,
            AdaptationField: af,
            PES:             d.PES,
        }); err != nil {
            return err
        }
    }
    return nil
}
```

---

## 📋 Чек-лист інтеграції

```go
// ✅ 1. Ініціалізація демуксера з великим буфером
demux := astits.NewDemuxer(ctx, bufio.NewReaderSize(reader, 10*1024*1024))

// ✅ 2. Обробка PAT/PMT для виявлення телетекст PID
// (див. handlePMT вище)

// ✅ 3. Фільтрація тільки потрібних потоків
if pid != teletextPID {
    continue  // ігноруємо відео/аудіо потоки
}

// ✅ 4. Синхронізація часу: перенос PTS у PCR
if pcr := extractPCR(d); pcr != nil {
    af.HasPCR = true
    af.PCR = pcr
}

// ✅ 5. Відправка у процесор субтитрів
err := processor.ProcessTeletextPES(d.PES, d.FirstPacket)

// ✅ 6. Метрики
monitoring.TSPacketsProcessed.Inc()
monitoring.TSProcessingLatency.Observe(time.Since(start).Seconds())

// ✅ 7. Graceful shutdown
defer func() {
    for _, mux := range muxers {
        mux.Close()  // якщо є такий метод
    }
}()
```

---

## 🔗 Корисні посилання

- 💻 [astits GitHub](https://github.com/asticode/go-astits) — бібліотека для роботи з MPEG-TS
- 📄 [MPEG-TS Spec (ISO/IEC 13818-1)](https://www.iso.org/standard/61246.html) — офіційна специфікація
- 📄 [DVB Subtitles (ETSI EN 300 743)](https://www.etsi.org/deliver/etsi_en/300700_300799/300743/02.01.01_60/en_300743v020101p.pdf) — стандарт субтитрів у DVB
- 🎬 [pkg/profile](https://github.com/pkg/profile) — проста бібліотека для CPU/Memory профілювання
- 🧪 [astits examples](https://github.com/asticode/go-astits/tree/master/cmd) — приклади CLI інструментів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **реальним часом** та **CCTV потоками**:
> 1. **Використовуйте великі буфери** (`bufio.NewReaderSize` з 10MB) — зменшує системні виклики та покращує пропускну здатність.
> 2. **Фільтруйте потоки на ранньому етапі** — не створюйте muxers для відео/аудіо, якщо потрібні тільки субтитри.
> 3. **Переносьте PTS у PCR** перед записом — це забезпечує коректну синхронізацію при подальшому ремуксингу.
> 4. **Додайте контекст для скасування** — дозволяє коректно зупинити обробку при закритті WebSocket-з'єднання.
> 5. **Експортуйте метрики у Prometheus** — моніторинг пропускної здатності допомагає виявляти вузькі місця.

Потрібен приклад функції `extractPCR()` для витягування таймінгів з PES-пакетів або інтеграція з вашим `VideoManifestProxy` для синхронізації субтитрів з відео? Готовий допомогти! 🚀