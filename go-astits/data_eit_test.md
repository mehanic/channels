# Глибоке роз'яснення: Тест парсингу EIT (Event Information Table) у astits

Цей файл містить **тест парсингу секції EIT (Event Information Table)** — таблиці службової інформації стандарту DVB, що містить розклад передач: назви подій, час початку, тривалість, статус та дескриптори. Це фундамент для EPG (Electronic Program Guide) у вашому пайплайні.

---

## 🎯 Навіщо EIT потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ EIT у контексті HLS-стрімінгу:         │
│                                         │
│ 🔹 Розклад передач (EPG):              │
│   • EventID — унікальний ID події      │
│   • StartTime — коли починається       │
│   • Duration — тривалість передачі     │
│   • Назва, опис через дескриптори      │
│                                         │
│ 🔹 Статус та доступність:              │
│   • RunningStatus — чи йде зараз       │
│   • HasFreeCSAMode — чи без шифрування │
│                                         │
│ 🔹 Для CCTV HLS:                        │
│   • Генерація #EXT-X-PROGRAM-DATE-TIME │
│   • Відображення назв передач у плеєрі │
│   • Фільтрація активних подій          │
│   • Синхронізація з реальним часом     │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `EITData` та тестові дані

### Тип даних

```go
type EITData struct {
    Events                   []*EITDataEvent  // 🎯 список подій у секції
    LastTableID              uint8            // 🎯 ID останньої таблиці (для багатосекційних)
    OriginalNetworkID        uint16           // 🎯 ID мережі-джерела
    SegmentLastSectionNumber uint8            // 🎯 останній номер секції у сегменті
    ServiceID                uint16           // 🎯 ID сервісу (каналу)
    TransportStreamID        uint16           // 🎯 ID транспортного потоку
}

type EITDataEvent struct {
    Descriptors    []*Descriptor  // 🎯 метадані: назва, опис, категорія...
    Duration       time.Duration  // 🎯 тривалість події
    EventID        uint16         // 🎯 унікальний ID події
    HasFreeCSAMode bool           // 🎯 чи без шифрування
    RunningStatus  uint8          // 🎯 статус: 0=невизн, 1-4=running/pausing...
    StartTime      time.Time      // 🎯 час початку (UTC, парсений з DVB формату)
}
```

### Тестові дані: `eit` та `eitBytes()`

```go
// Глобальна змінна — еталонне значення для тесту
var eit = &EITData{
    Events: []*EITDataEvent{{
        Descriptors:    descriptors,  // посилання на тестові дескриптори
        Duration:       dvbDurationSeconds,  // 1:45:30 (з тестів DVB часу)
        EventID:        6,            // ID події
        HasFreeCSAMode: true,         // без шифрування
        RunningStatus:  7,            // reserved (тестове значення)
        StartTime:      dvbTime,      // 1993-10-13 12:45:00 UTC
    }},
    LastTableID:              5,   // остання таблиця = 5
    OriginalNetworkID:        3,   // мережа-джерело = 3
    SegmentLastSectionNumber: 4,   // остання секція сегменту = 4
    ServiceID:                1,   // сервіс/канал = 1
    TransportStreamID:        2,   // транспортний потік = 2
}

// Генератор "еталонних" байтів для EIT секції (payload без заголовка)
func eitBytes() []byte {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // 🔹 Загальні поля секції
    w.Write(uint16(2))               // TransportStreamID = 2
    w.Write(uint16(3))               // OriginalNetworkID = 3
    w.Write(uint8(4))                // SegmentLastSectionNumber = 4
    w.Write(uint8(5))                // LastTableID = 5
    
    // 🔹 Подія #1
    w.Write(uint16(6))               // EventID = 6
    w.Write(dvbTimeBytes)            // StartTime у DVB форматі (5 байт)
    w.Write(dvbDurationSecondsBytes) // Duration у DVB форматі (3 байти)
    
    // 🔹 Прапорці події
    w.Write("111")                   // RunningStatus = 7 (3 біти)
    w.Write("1")                     // FreeCA mode = true (1 біт)
    
    // 🔹 Дескриптори події
    descriptorsBytes(w)              // виклик з тестів дескрипторів
    
    // Результат: []byte з серіалізованим payload EIT секції
    return buf.Bytes()
}
```

> 💡 **Важливо**: `eitBytes()` генерує тільки **payload секції** (без PSI заголовка та CRC32). Функція `parseEITSection` очікує саме payload + довжину, бо заголовок парситься на вищому рівні (`parsePSISection`).

---

## 🔍 Тест `TestParseEITSection`

```go
func TestParseEITSection(t *testing.T) {
    // 🔹 1. Отримати тестові байти
    b := eitBytes()
    
    // 🔹 2. Парсити секцію: ітератор + довжина + service_id
    d, err := parseEITSection(astikit.NewBytesIterator(b), len(b), uint16(1))
    
    // 🔹 3. Перевірити результат
    assert.Equal(t, d, eit)        // ✅ структурна рівність з еталоном
    assert.NoError(t, err)         // ✅ без помилок
}
```

### Що робить `parseEITSection` (гіпотетична реалізація)

```go
func parseEITSection(i *astikit.BytesIterator, sectionLength int, tableIDExtension uint16) (*EITData, error) {
    d := &EITData{
        ServiceID: tableIDExtension,  // передається зовні з PSI заголовка
    }
    
    // 🔹 1. Загальні поля секції
    bs, _ := i.NextBytesNoCopy(2)
    d.TransportStreamID = uint16(bs[0])<<8 | uint16(bs[1])
    
    bs, _ = i.NextBytesNoCopy(2)
    d.OriginalNetworkID = uint16(bs[0])<<8 | uint16(bs[1])
    
    d.SegmentLastSectionNumber, _ = i.NextByte()
    d.LastTableID, _ = i.NextByte()
    
    // 🔹 2. Цикл по подіях до кінця секції
    offsetEnd := i.Offset() + sectionLength - 8  // мінус вже прочитані 8 байт
    
    for i.Offset() < offsetEnd {
        event := &EITDataEvent{}
        
        // ── EventID (2 байти) ──
        bs, _ = i.NextBytesNoCopy(2)
        event.EventID = uint16(bs[0])<<8 | uint16(bs[1])
        
        // ── StartTime (5 байт, DVB формат) ──
        event.StartTime, _ = parseDVBTime(i)
        
        // ── Duration (3 байти, DVB формат) ──
        event.Duration, _ = parseDVBDurationSeconds(i)
        
        // ── Прапорці (1 байт: [3 running_status][1 free_ca][4 reserved]) ──
        b, _ := i.NextByte()
        event.RunningStatus = uint8(b) >> 5  // старші 3 біти
        event.HasFreeCSAMode = b&0x10 > 0    // біт 4
        
        // ── Дескриптори події ──
        event.Descriptors, _ = parseDescriptors(i)
        
        d.Events = append(d.Events, event)
    }
    
    return d, nil
}
```

> ⚠️ **Важливо**: Реальна реалізація може мати інший порядок читання — завжди звіряйтесь зі специфікацією ETSI EN 300 468.

---

## 🧮 Формат EIT секції у деталях

```
EIT Section Payload (без PSI заголовка та CRC):
┌─────────────────────────────────┐
│ [16] transport_stream_id       │
│ [16] original_network_id       │
│ [8]  segment_last_section_number│
│ [8]  last_table_id            │
├─────────────────────────────────┤
│ Event loop (повтор для кожної події):
│   [16] event_id                │
│   [40] start_time (DVB format) │ ← MJD(16) + BCD_час(24)
│   [24] duration (DVB format)   │ ← BCD: HH MM SS
│   [3]  running_status          │ ← 0=undefined, 1-4=running...
│   [1]  free_ca_mode            │ ← 1=без шифрування
│   [4]  reserved = 0            │
│   [12] descriptors_loop_length│
│   [N]  descriptors...          │ ← метадані події
└─────────────────────────────────┘

Повна PSI секція (додається на вищому рівні):
[8]  table_id = 0x4E-0x6F (EIT діапазон)
[12] section_length
[16] service_id (table_id_extension)
[16] reserved + version + current_next
[8]  section_number
[8]  last_section_number
[... EIT payload ...]
[32] CRC32
```

### Приклад розбору прапорців події

```
Вхідний байт: 0b11110000 = 0xF0
              ↑    ↑
              │    └─ біт 4: free_ca_mode = 1 (без шифрування)
              └─ біти 7-5: running_status = 0b111 = 7 (reserved)

Розрахунок:
• RunningStatus = 0xF0 >> 5 = 0b111 = 7 ✅
• HasFreeCSAMode = 0xF0 & 0x10 = 0x10 > 0 → true ✅
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Генерація EPG для HLS плейлиста

```go
// У VideoManifestProxy — додавання метаданих подій до плейлиста:
func addEPGToPlaylist(eit *astits.EITData, playlist *HLSPlaylist, channelID string) {
    for _, event := range eit.Events {
        // 🔹 Форматувати час початку у RFC3339 для HLS
        startTime := event.StartTime.Format(time.RFC3339)
        
        // 🔹 Додати #EXT-X-PROGRAM-DATE-TIME
        playlist.AddTag(fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s", startTime))
        
        // 🔹 Витягнути назву події з дескрипторів
        eventName := extractEventName(event.Descriptors)
        if eventName != "" {
            playlist.AddComment(fmt.Sprintf("# %s", eventName))
        }
        
        // 🔹 Додати #EXTINF з тривалістю
        duration := event.Duration.Seconds()
        playlist.AddTag(fmt.Sprintf("#EXTINF:%.3f,", duration))
        
        // 🔹 Додати категорію (якщо є)
        category := extractEventCategory(event.Descriptors)
        if category != "" {
            playlist.AddTag(fmt.Sprintf("#EXT-X-GENRE:%s", category))
        }
    }
}

// Helper: витягнути назву з ShortEvent дескриптора
func extractEventName(descs []*astits.Descriptor) string {
    for _, desc := range descs {
        if desc.ShortEvent != nil {
            return string(desc.ShortEvent.EventName)
        }
    }
    return ""
}
```

### ✅ 2: Фільтрація активних подій за статусом

```go
// Показувати тільки "running" або майбутні події:
func getActiveEvents(eit *astits.EITData, now time.Time) []*astits.EITDataEvent {
    var active []*astits.EITDataEvent
    
    for _, event := range eit.Events {
        // 🔹 Пропустити завершені події
        if event.StartTime.Add(event.Duration).Before(now) {
            continue
        }
        
        // 🔹 Пропустити неактивні статуси (опціонально)
        if event.RunningStatus == astits.RunningStatusNotRunning {
            continue
        }
        
        active = append(active, event)
    }
    
    return active
}

// Використання:
now := time.Now().UTC()
activeEvents := getActiveEvents(eit, now)
for _, event := range activeEvents {
    log.Infof("Active event: ID=%d, start=%v, duration=%v, status=%d",
        event.EventID, event.StartTime, event.Duration, event.RunningStatus)
}
```

### ✅ 3: Синхронізація часу подій з реальним світом

```go
// Корекція часу подій через TOT/PCR синхронізацію:
type EventSyncState struct {
    baseEITTime   time.Time  // останній StartTime з EIT
    basePCR       *astits.ClockReference  // відповідний PCR
    timezoneOffset time.Duration  // зсув з дескриптора
}

func handleEITEvent(event *astits.EITDataEvent, pcr *astits.ClockReference, 
                   channelID string, syncState *EventSyncState) time.Time {
    // 🔹 Якщо є базова синхронізація — скоригувати час
    if syncState.basePCR != nil {
        pcrDiff := pcr.Duration() - syncState.basePCR.Duration()
        return event.StartTime.Add(pcrDiff).Add(syncState.timezoneOffset)
    }
    
    // 🔹 Fallback: використати час як є
    return event.StartTime
}

// Використання при генерації плейлиста:
for _, event := range eit.Events {
    correctedTime := handleEITEvent(event, currentPCR, channelID, syncState)
    playlist.AddTag(fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s", 
        correctedTime.Format(time.RFC3339)))
}
```

### ✅ 4: Моніторинг якості EPG даних

```go
// monitoring.Monitor — метрики для EIT:
type EITMetrics struct {
    EITParsed         *prometheus.CounterVec  // кількість парсингів EIT
    EventsDiscovered  *prometheus.CounterVec  // кількість знайдених подій
    ActiveEventsGauge *prometheus.GaugeVec    // скільки подій "running"
    EPGCoverageGauge  *prometheus.GaugeVec    // покриття розкладом (годин)
    DescriptorErrors  *prometheus.CounterVec  // помилки парсингу дескрипторів
}

// У обробці EIT:
func monitorEIT(eit *astits.EITData, channelID string, metrics *EITMetrics, now time.Time) {
    metrics.EITParsed.WithLabelValues(channelID).Inc()
    metrics.EventsDiscovered.WithLabelValues(channelID).Add(float64(len(eit.Events)))
    
    // 🔹 Підрахувати активні події
    activeCount := 0
    totalDuration := time.Duration(0)
    
    for _, event := range eit.Events {
        if event.RunningStatus == astits.RunningStatusRunning {
            activeCount++
        }
        // 🔹 Порахувати загальну тривалість розкладу
        if event.StartTime.After(now) {
            totalDuration += event.Duration
        }
    }
    
    metrics.ActiveEventsGauge.WithLabelValues(channelID).Set(float64(activeCount))
    metrics.EPGCoverageGauge.WithLabelValues(channelID).Set(totalDuration.Hours())
}
```

### ✅ 5: Кешування EIT для fallback при втраті сигналу

```go
// EIT передається періодично — кешувати для використання між оновленнями:
type EITCache struct {
    mu         sync.RWMutex
    lastValid  *astits.EITData
    lastUpdate time.Time
    ttl        time.Duration
}

func (c *EITCache) Update(eit *astits.EITData) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.lastValid = eit
    c.lastUpdate = time.Now()
}

func (c *EITCache) Get() (*astits.EITData, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    if c.lastValid == nil {
        return nil, false
    }
    if time.Since(c.lastUpdate) > c.ttl {
        return nil, false  // застарілі дані
    }
    return c.lastValid, true
}

// Використання:
eitCache := &EITCache{ttl: 10 * time.Minute}  // EIT оновлюється кожні ~хвилини

// При отриманні нової EIT:
if data.EIT != nil {
    eitCache.Update(data.EIT)
}

// При генерації плейлиста:
if eit, ok := eitCache.Get(); ok {
    addEPGToPlaylist(eit, playlist, channelID)
} else {
    // Fallback: мінімальні метадані
    log.Warnf("Channel %s: no EIT data, using minimal metadata", channelID)
    addMinimalMetadata(playlist, channelID)
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на кілька подій у EIT

```go
func TestParseEITSection_MultipleEvents(t *testing.T) {
    // Створити EIT з 3 подіями
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Загальні поля
    w.Write(uint16(1))  // TransportStreamID
    w.Write(uint16(1))  // OriginalNetworkID
    w.Write(uint8(0))   // SegmentLastSectionNumber
    w.Write(uint8(0))   // LastTableID
    
    // Подія 1: ID=100, running
    w.Write(uint16(100))
    w.Write(dvbTimeBytes)  // StartTime
    w.Write(dvbDurationSecondsBytes)  // Duration
    w.Write("0110000")  // running_status=3 (running), free_ca=0
    w.WriteN(uint16(0), 12)  // descriptors_length = 0
    
    // Подія 2: ID=101, not running
    w.Write(uint16(101))
    w.Write(dvbTimeBytes)
    w.Write(dvbDurationSecondsBytes)
    w.Write("0010000")  // running_status=1 (not running)
    w.WriteN(uint16(0), 12)
    
    // Подія 3: ID=102, pausing
    w.Write(uint16(102))
    w.Write(dvbTimeBytes)
    w.Write(dvbDurationSecondsBytes)
    w.Write("0100000")  // running_status=2 (pausing)
    w.WriteN(uint16(0), 12)
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    eit, err := parseEITSection(iter, buf.Len(), uint16(1))
    
    assert.NoError(t, err)
    assert.Len(t, eit.Events, 3)
    
    // Перевірити статуси
    assert.Equal(t, uint8(3), eit.Events[0].RunningStatus)  // running
    assert.Equal(t, uint8(1), eit.Events[1].RunningStatus)  // not running
    assert.Equal(t, uint8(2), eit.Events[2].RunningStatus)  // pausing
}
```

### 🔹 Тест на дескриптори подій (ShortEvent)

```go
func TestParseEITSection_WithEventDescriptors(t *testing.T) {
    // Створити EIT з ShortEvent дескриптором
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Загальні поля
    w.Write(uint16(1))
    w.Write(uint16(1))
    w.Write(uint8(0))
    w.Write(uint8(0))
    
    // Подія з назвою
    w.Write(uint16(1))  // EventID
    w.Write(dvbTimeBytes)
    w.Write(dvbDurationSecondsBytes)
    w.Write("0110000")  // running=3, free_ca=0
    
    // ShortEvent дескриптор: "Новости"
    w.Write(uint8(astits.DescriptorTagShortEvent))
    w.Write(uint8(12))  // length = 3 (lang) + 1 (name_len) + 8 (name)
    w.Write([]byte("ukr"))  // мова
    w.Write(uint8(8))  // назва довжина
    w.Write([]byte("Новости"))  // назва події
    w.Write(uint8(0))  // текст довжина = 0
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    eit, err := parseEITSection(iter, buf.Len(), uint16(1))
    
    assert.NoError(t, err)
    assert.Len(t, eit.Events, 1)
    
    event := eit.Events[0]
    assert.Len(t, event.Descriptors, 1)
    
    desc := event.Descriptors[0]
    assert.Equal(t, astits.DescriptorTagShortEvent, desc.Tag)
    assert.NotNil(t, desc.ShortEvent)
    assert.Equal(t, []byte("Новости"), desc.ShortEvent.EventName)
}
```

### 🔹 Тест на round-trip EIT секції

```go
func TestEITSection_RoundTrip(t *testing.T) {
    original := &astits.EITData{
        ServiceID:         1,
        TransportStreamID: 2,
        OriginalNetworkID: 3,
        Events: []*astits.EITDataEvent{
            {
                EventID:        10,
                StartTime:      time.Date(2024, 5, 15, 14, 30, 0, 0, time.UTC),
                Duration:       30 * time.Minute,
                RunningStatus:  astits.RunningStatusRunning,
                HasFreeCSAMode: true,
                Descriptors: []*astits.Descriptor{
                    {
                        Tag: astits.DescriptorTagShortEvent,
                        ShortEvent: &astits.DescriptorShortEvent{
                            Language:  []byte("ukr"),
                            EventName: []byte("Test Event"),
                            Text:      []byte("Description"),
                        },
                    },
                },
            },
        },
    }
    
    // Серіалізувати (спрощено — тільки payload)
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    // ... записати EIT payload ...
    
    // Парсити назад
    parsed, err := parseEITSection(astikit.NewBytesIterator(buf.Bytes()), buf.Len(), original.ServiceID)
    assert.NoError(t, err)
    
    // Порівняти ключові поля
    assert.Equal(t, original.ServiceID, parsed.ServiceID)
    assert.Len(t, parsed.Events, 1)
    assert.Equal(t, original.Events[0].EventID, parsed.Events[0].EventID)
    assert.Equal(t, original.Events[0].StartTime.Truncate(time.Second), 
                 parsed.Events[0].StartTime.Truncate(time.Second))
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання DVB часу | `StartTime` зсувається на дні/місяці | Перевірити константу MJD: Unix epoch = MJD 40587; перевірити BCD-декодування |
| `RunningStatus` не співпадає | Статус інтерпретується неправильно | Перевірити бітовий зсув: `b >> 5` для старших 3 біт |
| Дескриптори не парсяться | `event.Descriptors` порожній | Перевірити, що `parseDescriptors` викликається з правильним `offsetEnd` |
| EIT не надходить у потоці | `NextData()` ніколи не повертає `*DemuxerData` з `EIT` | Це нормально: EIT передається періодично; реалізувати кешування з TTL |
| `table_id` діапазон не обробляється | Події з table_id 0x4E-0x6F ігноруються | Перевірити `parsePSISectionSyntaxData`: `if tableID >= 0x4E && tableID <= 0x6F` |

### Приклад коректного читання RunningStatus:

```go
func parseEventFlags(b byte) (runningStatus uint8, freeCAMode bool) {
    // Формат: [3 running_status][1 free_ca][4 reserved]
    runningStatus = b >> 5  // старші 3 біти (0-7)
    freeCAMode = b&0x10 > 0  // біт 4
    
    // Валідація: running_status має бути 0-4, 5-7 = reserved
    if runningStatus > 4 {
        log.Debugf("Reserved running_status value: %d", runningStatus)
    }
    
    return runningStatus, freeCAMode
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Парсинг EIT з вхідного потоку:
func handleEITData(data *astits.DemuxerData, channelID string) error {
    if data.EIT == nil {
        return nil  // не EIT
    }
    
    eit := data.EIT
    log.Infof("Channel %s: EIT with %d events (Service_ID=%d)", 
        channelID, len(eit.Events), eit.ServiceID)
    
    // Оновити кеш EPG
    updateEPGCache(channelID, eit)
    
    return nil
}

// 2: Витягування назви події для HLS:
func getEventName(event *astits.EITDataEvent) string {
    for _, desc := range event.Descriptors {
        if desc.ShortEvent != nil {
            return string(desc.ShortEvent.EventName)
        }
        if desc.ExtendedEvent != nil {
            // Спробувати знайти назву у items
            for _, item := range desc.ExtendedEvent.Items {
                if string(item.Description) == "Title" || string(item.Description) == "Назва" {
                    return string(item.Content)
                }
            }
        }
    }
    return fmt.Sprintf("Event %d", event.EventID)
}

// 3: Фільтрація за статусом:
func getRunningEvents(eit *astits.EITData) []*astits.EITDataEvent {
    var running []*astits.EITDataEvent
    for _, event := range eit.Events {
        if event.RunningStatus == astits.RunningStatusRunning {
            running = append(running, event)
        }
    }
    return running
}

// 4: Форматування для HLS PROGRAM-DATE-TIME:
func formatEventStartTime(event *astits.EITDataEvent) string {
    // HLS вимагає RFC3339 / ISO8601
    return event.StartTime.UTC().Format("2006-01-02T15:04:05.000Z")
}

// 5: Моніторинг:
func monitorEITHealth(eit *astits.EITData, channelID string, metrics *EITMetrics) {
    if eit == nil {
        metrics.LastEITUpdate.WithLabelValues(channelID).Set(0)
        return
    }
    
    metrics.LastEITUpdate.WithLabelValues(channelID).Set(float64(time.Now().Unix()))
    metrics.EventCount.WithLabelValues(channelID).Set(float64(len(eit.Events)))
    
    // 🔹 Підрахувати події з назвами (якість EPG)
    namedCount := 0
    for _, event := range eit.Events {
        if getEventName(event) != fmt.Sprintf("Event %d", event.EventID) {
            namedCount++
        }
    }
    metrics.NamedEventRatio.WithLabelValues(channelID).Set(float64(namedCount) / float64(len(eit.Events)))
}
```

---

## 📊 Матриця полів EIT для вашого пайплайну

```
Поле EIT                   | Тип       | Використання у CCTV HLS
───────────────────────────┼───────────┼─────────────────────────
ServiceID                  | uint16    | ✅ Ідентифікація каналу
TransportStreamID          | uint16    | ✅ Ідентифікація потоку
OriginalNetworkID          | uint16    | ✅ Розрізнення джерел
EventID                    | uint16    | ✅ Унікальний ID події
StartTime                  | time.Time | ✅ Час початку для #EXT-X-PROGRAM-DATE-TIME
Duration                   | time.Duration| ✅ Тривалість для #EXTINF
RunningStatus              | uint8     | ✅ Фільтрація активних подій
HasFreeCSAMode             | bool      | ⚠️ Інформація про шифрування
Descriptors[ShortEvent]    | []byte    | ✅ Назва, опис для EPG
Descriptors[Content]       | []byte    | ⚠️ Категорія для фільтрації
```

---

## 📚 Корисні посилання

- [ETSI EN 300 468: EIT specification (§6.2.13)](https://dvb.org/wp-content/uploads/2019/12/a038_tm1217r37_en300468v1_17_1_-_rev-134_-_si_specification.pdf)
- [DVB EPG poster](http://seidl.cs.vsb.cz/download/dvb/DVB_Poster.pdf)
- [astits EIT parsing source](https://github.com/asticode/go-astits/blob/master/data.go)

> 💡 **Ключова ідея**: EIT — це "розклад передач" вашого DVB-сигналу. У вашому CCTV HLS пайплайні це дозволяє:
> - 📺 Автоматично генерувати EPG для плеєрів через #EXT-X-PROGRAM-DATE-TIME
> - 🎯 Фільтрувати активні події для відображення "зараз в ефірі"
> - 🔍 Відстежувати якість метаданих (наявність назв, описів)
> - 🧩 Підтримувати мультимовність через дескриптори з різними мовами

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати EIT-обробку у ваш `VideoManifestProxy` для динамічного оновлення EPG
- 🧩 Додати підтримку кешування EPG з TTL для fallback при втраті EIT
- 🧪 Написати integration-тест для перевірки коректності генерації HLS-плейлистів з EPG

🛠️