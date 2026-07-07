# Глибоке роз'яснення: `demuxer.go` — ядро парсингу MPEG-TS у astits

Цей файл містить **головний компонент бібліотеки** — `Demuxer`, який відповідає за читання, фільтрацію та агрегацію пакетів у логічні одиниці даних (PSI таблиці, PES-пакети). Це "вхідні двері" вашого пайплайну.

---

## 🎯 Архітектура `Demuxer`: що він робить?

```
┌─────────────────────────────────────────┐
│ Demuxer — головний інтерфейс astits:   │
│                                         │
│ Вхід: io.Reader (файл, WebSocket, UDP) │
│                                         │
│ 🔹 Рівень 1: NextPacket()              │
│    • Читає сирі 188-байтові пакети     │
│    • Парсить заголовки та адаптаційні  │
│      поля                               │
│    • Повертає *Packet структури        │
│                                         │
│ 🔹 Рівень 2: NextData()                │
│    • Агрегує пакети за PID через       │
│      packetPool                         │
│    • Збирає фрагментовані PSI/PES      │
│    • Повертає *DemuxerData з готовими  │
│      таблицями (PAT/PMT/EIT) або PES   │
│                                         │
│ 🔹 Додатково: Rewind()                 │
│    • Скидає стан для повторного читання│
│    • Очищає внутрішні буфери           │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `Demuxer`: поля та їх призначення

```go
type Demuxer struct {
    ctx        context.Context        // 🎯 для скасування та таймаутів
    dataBuffer []*DemuxerData         // 📦 черга готових даних (буферизація)
    l          astikit.CompleteLogger // 📝 структурований логер
    
    // 🔹 Опції (функціональний патерн)
    optPacketSize    int             // розмір пакету (188/192/204), 0=автодетекція
    optPacketsParser PacketsParser   // кастомний парсер для складних випадків
    optPacketSkipper PacketSkipper   // фільтр пакетів на ранньому етапі
    
    // 🔹 Внутрішні компоненти
    packetBuffer *packetBuffer  // 📦 низькорівневий читач пакетів
    packetPool   *packetPool    // 🗂️ агрегація пакетів за PID
    programMap   *programMap    // 🗺️ мапінг PID → program_number
    r            io.Reader      // 📥 джерело вхідних даних
}
```

### Ключові взаємодії компонентів

```
[io.Reader] → packetBuffer → packetPool → parseData() → DemuxerData
                    ↑              ↑
              автодетекція    агрегація за PID
              розміру         + фільтрація
              
programMap оновлюється при парсингу PAT:
  PAT: program_number=1 → PMT_PID=0x1000
  programMap[0x1000] = 1  // тепер знаємо: PID 0x1000 = PMT програми 1
```

---

## ⚙️ Functional Options Pattern: гнучка конфігурація

### Типи опцій

```go
// 🎯 PacketsParser: кастомна логіка агрегації пакетів
type PacketsParser func(ps []*Packet) (ds []*DemuxerData, skip bool, err error)
// • ps: набір пакетів одного логічного блоку (зібраний packetPool)
// • ds: готові до повернення дані
// • skip: чи пропустити стандартну обробку
// • err: помилка парсингу

// 🎯 PacketSkipper: фільтрація пакетів до парсингу
type PacketSkipper func(p *Packet) (skip bool)
// • p: пакет з парсеним заголовком та адаптаційним полем
// • skip: true = відкинути пакет, false = обробляти далі
```

### Реалізація опцій

```go
// 🔹 Опція для логера
func DemuxerOptLogger(l astikit.StdLogger) func(*Demuxer) {
    return func(d *Demuxer) {
        d.l = astikit.AdaptStdLogger(l)  // адаптація стандартного log.Logger
    }
}

// 🔹 Опція для розміру пакету
func DemuxerOptPacketSize(packetSize int) func(*Demuxer) {
    return func(d *Demuxer) {
        d.optPacketSize = packetSize  // 0 = автодетекція
    }
}

// 🔹 Опція для кастомного парсера
func DemuxerOptPacketsParser(p PacketsParser) func(*Demuxer) {
    return func(d *Demuxer) {
        d.optPacketsParser = p  // перевизначити логіку parseData()
    }
}

// 🔹 Опція для фільтра пакетів
func DemuxerOptPacketSkipper(s PacketSkipper) func(*Demuxer) {
    return func(d *Demuxer) {
        d.optPacketSkipper = s  // фільтрувати на рівні packetBuffer
    }
}
```

### Застосування у конструкторі

```go
func NewDemuxer(ctx context.Context, r io.Reader, opts ...func(*Demuxer)) *Demuxer {
    d := &Demuxer{
        ctx:        ctx,
        l:          astikit.AdaptStdLogger(nil),  // default: stdout logger
        programMap: newProgramMap(),              // порожній мапінг
        r:          r,
    }
    d.packetPool = newPacketPool(d.programMap)    // пов'язати pool з programMap
    
    // Застосувати опції
    for _, opt := range opts {
        opt(d)
    }
    return d
}
```

> 💡 **Переваги патерну**:
> • Гнучкість: додавати нові опції без зміни сигнатури `NewDemuxer()`
> • Читабельність: `NewDemuxer(ctx, r, OptA(), OptB())` зрозуміліше ніж позиційні параметри
> • Типобезпека: кожна опція — типізована функція

---

## 📦 `NextPacket()`: низькорівневий доступ до пакетів

```go
func (dmx *Demuxer) NextPacket() (*Packet, error) {
    // 🔹 1. Перевірка контексту (скасування/таймаут)
    if err := dmx.ctx.Err(); err != nil {
        return nil, err  // context.Canceled або context.DeadlineExceeded
    }
    
    // 🔹 2. Ініціалізація packetBuffer (lazy init)
    if dmx.packetBuffer == nil {
        if dmx.packetBuffer, err = newPacketBuffer(
            dmx.r, 
            dmx.optPacketSize,    // 0 → автодетекція
            dmx.optPacketSkipper, // фільтр пакетів
        ); err != nil {
            return nil, fmt.Errorf("astits: creating packet buffer failed: %w", err)
        }
    }
    
    // 🔹 3. Читання наступного пакету
    p, err := dmx.packetBuffer.next()
    if err != nil && err != ErrNoMorePackets {
        err = fmt.Errorf("astits: fetching next packet from buffer failed: %w", err)
    }
    return p, err
}
```

### Коли використовувати `NextPacket()`?

```
✅ Низькорівнева діагностика: логування заголовків, детекція аномалій
✅ Орфан аудіо/відео merge: ручна агрегація за seqNum замість PID
✅ Кастомна обробка: коли стандартна логіка NextData() не підходить

❌ Стандартна обробка потоків: використовуйте NextData() для агрегації PSI/PES
```

### Приклад використання для діагностики

```go
func debugStream(reader io.Reader, channelID string) error {
    dmx := astits.NewDemuxer(context.Background(), reader,
        astits.DemuxerOptPacketSize(188),
    )
    
    for i := 0; i < 100; i++ {  // обмежити сканування
        pkt, err := dmx.NextPacket()
        if errors.Is(err, astits.ErrNoMorePackets) {
            break
        }
        if err != nil {
            log.Errorf("Channel %s: packet read error: %v", channelID, err)
            continue
        }
        
        log.Infof("Packet %d: PID=%d, CC=%d, PUSI=%v, payload_len=%d",
            i,
            pkt.Header.PID,
            pkt.Header.ContinuityCounter,
            pkt.Header.PayloadUnitStartIndicator,
            len(pkt.Payload),
        )
        
        if pkt.AdaptationField != nil && pkt.AdaptationField.HasPCR {
            log.Infof("  PCR: base=%d, ext=%d", 
                pkt.AdaptationField.PCR.Base,
                pkt.AdaptationField.PCR.Extension,
            )
        }
    }
    return nil
}
```

---

## 🗂️ `NextData()`: високо рівнева агрегація даних

### Головний цикл агрегації

```go
func (dmx *Demuxer) NextData() (*DemuxerData, error) {
    // 🔹 1. Перевірити буфер готових даних
    if len(dmx.dataBuffer) > 0 {
        d := dmx.dataBuffer[0]
        dmx.dataBuffer = dmx.dataBuffer[1:]  // dequeue
        return d, nil
    }
    
    // 🔹 2. Цикл читання пакетів до отримання готових даних
    var ps []*Packet  // набір пакетів одного логічного блоку
    var ds []*DemuxerData  // результат парсингу
    
    for {
        // Читати наступний пакет
        p, err := dmx.NextPacket()
        if err != nil {
            // 🔹 EOF: злити залишки з packetPool
            if err == ErrNoMorePackets {
                return dmx.drainPool()  // допоміжна логіка для залишків
            }
            return nil, fmt.Errorf("astits: fetching next packet failed: %w", err)
        }
        
        // 🔹 Додати пакет у pool для агрегації
        ps = dmx.packetPool.addUnlocked(p)
        if len(ps) == 0 {
            continue  // ще збираємо фрагменти
        }
        
        // 🔹 Парсити зібрані пакети
        ds, err = parseData(ps, dmx.optPacketsParser, dmx.programMap)
        if err != nil {
            return nil, fmt.Errorf("astits: building new data failed: %w", err)
        }
        
        // 🔹 Оновити стан та повернути дані
        if d := dmx.updateData(ds); d != nil {
            return d, nil
        }
    }
}
```

### Логіка `drainPool()` при EOF

```go
// Коли потік закінчується — злити незавершені групи з packetPool
if err == ErrNoMorePackets {
    for {
        ps := dmx.packetPool.dumpUnlocked()  // повертає першу не-порожню групу
        if len(ps) == 0 {
            break  // всі групи оброблено
        }
        
        // Спробувати парсити навіть неповні дані
        ds, errParseData := parseData(ps, dmx.optPacketsParser, dmx.programMap)
        if errParseData != nil {
            dmx.l.Error(fmt.Errorf("astits: parsing data failed: %w", errParseData))
            continue  // не зупиняти обробку інших груп
        }
        
        if d := dmx.updateData(ds); d != nil {
            return d, nil  // повернути останні валідні дані
        }
    }
    return nil, ErrNoMorePackets  // дійсно кінець
}
```

> 💡 **Ключова ідея**: `NextData()` блокується доки не збере достатньо пакетів для формування валідної одиниці даних. Це дозволяє автоматично обробляти фрагментовані PSI/PES без ручного управління буферами.

---

## 🔄 `updateData()`: обробка парсених даних та оновлення стану

```go
func (dmx *Demuxer) updateData(ds []*DemuxerData) *DemuxerData {
    if len(ds) == 0 {
        return nil
    }
    
    // 🔹 1. Повернути перший елемент, решту — у буфер
    d := ds[0]
    dmx.dataBuffer = append(dmx.dataBuffer, ds[1:]...)  // enqueue решти
    
    // 🔹 2. Оновити programMap при парсингу PAT
    for _, v := range ds {
        if v.PAT != nil {
            for _, pgm := range v.PAT.Programs {
                // Program number 0 = NIT (Network Information Table), пропускаємо
                if pgm.ProgramNumber > 0 {
                    // 🎯 Ключове: PID PMT → program_number
                    dmx.programMap.setUnlocked(pgm.ProgramMapID, pgm.ProgramNumber)
                }
            }
        }
    }
    
    return d
}
```

### Чому оновлення `programMap` важливе?

```
Сценарій: потік містить PAT → PMT → відео/аудіо пакети

1. Читання PAT (PID=0x0000):
   • programMap[0x1000] = 1  // PMT програми 1 на PID 0x1000
   
2. Читання PMT (PID=0x1000):
   • packetPool знає: PID 0x1000 → programMap.exists(0x1000)=true → це PMT
   • parseData() розпізнає PMT структуру
   
3. Читання відео (PID=0x101):
   • programMap не містить 0x101 → це елементарний потік
   • parseData() розпізнає PES заголовок за payload

Без programMap: демуксер не зможе відрізнити PMT від елементарного потоку!
```

---

## 🔁 `Rewind()`: скидання стану демуксера

```go
func (dmx *Demuxer) Rewind() (int64, error) {
    // 🔹 Очищення буферів
    dmx.dataBuffer = []*DemuxerData{}           // порожня черга
    dmx.packetBuffer = nil                      // скинути читач пакетів
    dmx.packetPool = newPacketPool(dmx.programMap)  // новий пул (але programMap зберігаємо!)
    
    // 🔹 Скидання reader (якщо підтримує Seek)
    n, err := rewind(dmx.r)  // внутрішня функція: Seek(0) або -1
    if err != nil {
        err = fmt.Errorf("astits: rewinding reader failed: %w", err)
    }
    return n, err
}
```

### Коли використовувати `Rewind()`?

```
✅ Тестування: перечитати потік кілька разів з різними налаштуваннями
✅ Двопрохідний аналіз: 
   • Прохід 1: зібрати PAT/PMT для метаданих
   • Rewind()
   • Прохід 2: детальна обробка з контекстом метаданих
✅ Файлові джерела: os.File підтримує Seek(0)

❌ Мережеві потоки: net.Conn, WebSocket не підтримують Seek → rewind() поверне -1
❌ Production streaming: не використовувати на live-потоках
```

### Приклад двопрохідного аналізу

```go
func analyzeStreamWithMetadata(r io.ReadSeeker) (*StreamAnalysis, error) {
    dmx := astits.NewDemuxer(context.Background(), r)
    
    // 🔹 Прохід 1: зібрати метадані
    metadata := &StreamMetadata{}
    for {
        data, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) {
            break
        }
        if err != nil {
            return nil, err
        }
        
        if data.PAT != nil {
            metadata.TransportStreamID = data.PAT.TransportStreamID
        }
        if data.PMT != nil {
            metadata.PCRPID = data.PMT.PCRPID
            for _, es := range data.PMT.ElementaryStreams {
                metadata.Streams = append(metadata.Streams, StreamInfo{
                    PID:  es.ElementaryPID,
                    Type: es.StreamType,
                })
            }
        }
    }
    
    // 🔹 Rewind для другого проходу
    if _, err := r.Seek(0, io.SeekStart); err != nil {
        return nil, fmt.Errorf("seek failed: %w", err)
    }
    if _, err := dmx.Rewind(); err != nil {
        return nil, fmt.Errorf("rewind failed: %w", err)
    }
    
    // 🔹 Прохід 2: детальна обробка з контекстом метаданих
    return processWithMetadata(dmx, metadata)
}
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Ініціалізація з channel-aware фільтрацією

```go
// У вашому WebSocket-приймачі:
func handleChannelStream(conn *websocket.Conn, channelID string) error {
    // 🔹 Фільтр пакетів за PID каналу
    expectedPIDs := getPIDsForChannel(channelID)  // ваш мапінг
    skipper := func(p *astits.Packet) bool {
        return !expectedPIDs[p.Header.PID]
    }
    
    // 🔹 Створити демуксер з опціями
    dmx := astits.NewDemuxer(
        context.Background(),
        conn,
        astits.DemuxerOptPacketSize(188),  // явний розмір для мережі
        astits.DemuxerOptPacketSkipper(skipper),
        astits.DemuxerOptLogger(log.WithChannel(channelID)),  // структурований лог
    )
    
    // 🔹 Цикл читання даних
    for {
        data, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) {
            break  // нормальне завершення
        }
        if err != nil {
            log.Errorf("Channel %s: demux error: %v", channelID, err)
            metrics.DemuxErrors.WithLabelValues(channelID).Inc()
            continue  // не зупиняти весь потік через один пошкоджений пакет
        }
        
        // 🔹 Обробити готові дані
        if err := processDemuxerData(data, channelID); err != nil {
            log.Warnf("Channel %s: process error: %v", channelID, err)
        }
    }
    return nil
}
```

### ✅ 2. Кастомний `PacketsParser` для orphan audio merge

```go
// У вашому segmentAssembler — коли стандартна агрегація не підходить:
func createOrphanAudioParser(audioCache *AudioCache) astits.PacketsParser {
    return func(ps []*astits.Packet) ([]*astits.DemuxerData, bool, error) {
        // 🔹 Перевірити, чи це аудіо-потік з orphan логікою
        if len(ps) == 0 || !isAudioPID(ps[0].Header.PID) {
            return nil, false, nil  // стандартна обробка
        }
        
        // 🔹 Спробувати знайти відповідне відео за seqNum
        videoPackets := audioCache.FindMatchingVideo(ps)
        if videoPackets == nil {
            // Орфан: зберегти у кеш для пізнішого merge
            audioCache.StoreOrphan(ps)
            return nil, true, nil  // skip стандартну обробку
        }
        
        // 🔹 Merge аудіо+відео → створити демуксер-сумісні дані
        mergedData := mergeAudioVideoPackets(videoPackets, ps)
        return []*astits.DemuxerData{mergedData}, true, nil
    }
}

// Застосування:
dmx := astits.NewDemuxer(ctx, reader,
    astits.DemuxerOptPacketsParser(createOrphanAudioParser(audioCache)),
)
```

### ✅ 3. Обробка помилок та відновлення

```go
// У production-коді — стійка обробка помилок:
func robustDemuxLoop(dmx *astits.Demuxer, channelID string) error {
    for {
        data, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) {
            return nil  // нормальне завершення
        }
        if err != nil {
            // 🔹 Класифікувати помилку
            if isRecoverableError(err) {
                log.Warnf("Channel %s: recoverable demux error: %v", channelID, err)
                metrics.RecoverableErrors.WithLabelValues(channelID).Inc()
                continue  // спробувати наступний пакет
            }
            
            // 🔹 Критична помилка: можливо, втрата синхронізації
            log.Errorf("Channel %s: fatal demux error: %v", channelID, err)
            metrics.FatalErrors.WithLabelValues(channelID).Inc()
            
            // 🔹 Опція: спробувати відновити через Rewind (тільки для файлів)
            if canRewind(channelID) {
                if _, err := dmx.Rewind(); err == nil {
                    log.Infof("Channel %s: rewound for recovery", channelID)
                    continue
                }
            }
            
            return fmt.Errorf("demux failed: %w", err)
        }
        
        // 🔹 Обробити валідні дані
        if err := processDemuxerData(data, channelID); err != nil {
            log.Warnf("Channel %s: process error: %v", channelID, err)
            // Не зупиняти демуксинг через помилку обробки
        }
    }
}

func isRecoverableError(err error) bool {
    // Помилки, які можна пропустити без втрати синхронізації
    return strings.Contains(err.Error(), "corrupted") ||
           strings.Contains(err.Error(), "skip") ||
           strings.Contains(err.Error(), "unknown descriptor")
}
```

### ✅ 4. Моніторинг продуктивності демуксингу

```go
// monitoring.Monitor — метрики для Demuxer:
type DemuxMetrics struct {
    PacketsRead     *prometheus.CounterVec  // кількість прочитаних пакетів
    DataAggregated  *prometheus.CounterVec  // кількість DemuxerData
    ParseErrors     *prometheus.CounterVec  // помилки парсингу
    BytesProcessed  *prometheus.CounterVec  // загальний обсяг даних
    Latency         *prometheus.HistogramVec  // латентність NextData()
    ProgramMapSize  *prometheus.GaugeVec    // розмір programMap (активні програми)
}

// У циклі читання:
func demuxWithMetrics(dmx *astits.Demuxer, channelID string, metrics *DemuxMetrics) error {
    startTime := time.Now()
    packetCount := 0
    
    for {
        data, err := dmx.NextData()
        
        if errors.Is(err, astits.ErrNoMorePackets) {
            duration := time.Since(startTime)
            metrics.Latency.WithLabelValues(channelID).Observe(duration.Seconds())
            metrics.BytesProcessed.WithLabelValues(channelID).Add(float64(packetCount * 188))
            return nil
        }
        if err != nil {
            metrics.ParseErrors.WithLabelValues(channelID).Inc()
            continue
        }
        
        packetCount++
        metrics.DataAggregated.WithLabelValues(
            channelID,
            dataTypeToString(data),  // "PAT", "PMT", "PES", etc.
        ).Inc()
        
        // Оновити gauge розміру programMap
        metrics.ProgramMapSize.WithLabelValues(channelID).Set(
            float64(len(dmx.programMap)),  // гіпотетичний метод
        )
        
        // Обробити data...
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `NextData()` блокується назавжди | Потік не передає дані або мережева затримка | Використовувати `context.WithTimeout()` при створенні `Demuxer` |
| `programMap` не оновлюється | Клієнти не бачать нових програм | Перевірити, що PAT парситься коректно; додати логування після `updateData()` |
| Фрагментовані PSI не збираються | `NextData()` пропускає таблиці | Перевірити, що `PayloadUnitStartIndicator` встановлений на першому пакеті фрагмента |
| Високе споживання пам'яті | `dataBuffer` росте без обмежень | Додати ліміт черги: `if len(dmx.dataBuffer) > max { dropOldest() }` |
| Rewind() не працює на мережі | `rewind()` повертає -1, позиція не скидається | Не використовувати `Rewind()` на `net.Conn`; для тестів використовувати `bytes.Reader` |

### Приклад таймауту для мережевих потоків

```go
func newDemuxerWithTimeout(reader io.Reader, timeout time.Duration) *astits.Demuxer {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    
    // 🔹 Важливо: запустити горутину для скасування контексту при завершенні
    go func() {
        // Коли reader закривається — скасувати контекст
        if closer, ok := reader.(io.Closer); ok {
            defer closer.Close()
        }
        // Тут можна додати логіку очікування завершення читання
    }()
    
    return astits.NewDemuxer(ctx, reader,
        astits.DemuxerOptPacketSize(188),
    )
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Ініціалізація з усіма опціями:
func newChannelDemuxer(channelID string, reader io.Reader) (*astits.Demuxer, error) {
    ctx, cancel := context.WithCancel(context.Background())
    
    dmx := astits.NewDemuxer(
        ctx,
        reader,
        astits.DemuxerOptPacketSize(188),  // явний розмір для мережі
        astits.DemuxerOptPacketSkipper(func(p *astits.Packet) bool {
            // Фільтрувати пакети не для цього каналу
            return !isPIDForChannel(p.Header.PID, channelID)
        }),
        astits.DemuxerOptLogger(log.WithChannel(channelID)),
    )
    
    // 🔹 Зберегти cancel для коректного закриття
    storeCancelFunc(channelID, cancel)
    
    return dmx, nil
}

// 2. Читання з обробкою помилок:
func readStreamData(dmx *astits.Demuxer, channelID string) error {
    for {
        data, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) {
            return nil
        }
        if err != nil {
            if isRecoverableError(err) {
                continue
            }
            return fmt.Errorf("demux error on %s: %w", channelID, err)
        }
        
        // Обробити дані
        switch {
        case data.PAT != nil:
            handlePAT(data.PAT, channelID)
        case data.PMT != nil:
            handlePMT(data.PMT, channelID)
        case data.PES != nil:
            handlePES(data.PES, channelID)
        case data.EIT != nil:
            handleEIT(data.EIT, channelID)
        }
    }
}

// 3. Коректне закриття:
func closeChannelDemuxer(channelID string) {
    if cancel, ok := getCancelFunc(channelID); ok {
        cancel()  // скасувати контекст → зупинити блокування в NextPacket()
    }
}

// 4. Діагностика стану:
func debugDemuxerState(dmx *astits.Demuxer, channelID string) {
    log.Infof("Channel %s demuxer state:", channelID)
    // 🔹 programMap розмір
    log.Infof("  programMap entries: %d", len(dmx.programMap))  // гіпотетичний доступ
    // 🔹 dataBuffer черга
    log.Infof("  dataBuffer size: %d", len(dmx.dataBuffer))
    // 🔹 packetPool стан (якщо є публічний метод)
}
```

---

## 📊 Матриця методів `Demuxer`

```
Метод            | Рівень абстракції | Повертає          | Коли використовувати
─────────────────┼───────────────────┼───────────────────┼─────────────────────
NextPacket()     | Низький (сирі пакети) | *Packet        | • Низькорівнева діагностика
                 |                   |                   | • Орфан аудіо/відео merge
                 |                   |                   | • Кастомна агрегація
─────────────────┼───────────────────┼───────────────────┼─────────────────────
NextData()       | Високий (логічні одиниці) | *DemuxerData | • Отримання PAT/PMT/EIT
                 |                   |                   | • Парсинг PES-потоків
                 |                   |                   | • Стандартна обробка потоків
─────────────────┼───────────────────┼───────────────────┼─────────────────────
Rewind()         | Управління станом | int64 (позиція)  | • Тестування
                 |                   |                   | • Двопрохідний аналіз
                 |                   |                   | • НЕ для network streams
─────────────────┼───────────────────┼───────────────────┼─────────────────────
NewDemuxer()     | Ініціалізація     | *Demuxer         | • Створення з опціями
                 |                   |                   | • PacketSize, Skipper, Parser
```

---

## 📚 Корисні посилання

- [astits Demuxer API docs](https://pkg.go.dev/github.com/asticode/go-astits#Demuxer)
- [MPEG-TS PSI specification](https://www.iso.org/standard/61236.html)
- [Functional Options Pattern in Go](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis)
- [Context package best practices](https://go.dev/blog/context)

> 💡 **Ключова ідея**: `Demuxer` — це "міст" між сирими бітами та структурованими даними. У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Отримувати готові PSI таблиці для генерації валідних HLS-плейлистів
> - 🧩 Агрегувати фрагментовані PES-пакети для подальшої обробки (transcode, TTS, субтитри)
> - 🔍 Діагностувати проблеми потоку через низькорівневий доступ до пакетів
> - 📊 Збирати метрики про якість демуксингу для моніторингу стабільності

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати `Demuxer` у ваш `segmentAssembler` з підтримкою orphan audio merge
- 🧪 Написати integration-тест для перевірки сумісності з реальними енкодерами
- 📈 Додати Prometheus-метрики для моніторингу латентності та помилок демуксингу по каналах

🛠️