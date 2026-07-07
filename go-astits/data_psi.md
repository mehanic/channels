# Глибоке роз'яснення: `psi.go` — ядро парсингу PSI (Program Specific Information) у astits

Цей файл містить **фундаментальну реалізацію парсингу та серіалізації PSI даних** — механізму MPEG-TS для передачі метаданих: PAT, PMT, EIT, NIT, SDT, TOT тощо. Це "серце" демуксера, що перетворює сирі байти на структуровані таблиці.

---

## 🎯 Навіщо PSI потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ PSI у контексті HLS-стрімінгу:         │
│                                         │
│ 🔹 PAT (Program Association Table):    │
│   • Список програм → PMT PID           │
│   • Без PAT плеєр не знайде програми   │
│                                         │
│ 🔹 PMT (Program Map Table):            │
│   • PID відео/аудіо потоків            │
│   • PCR PID для синхронізації          │
│   • Дескриптори кодеків                │
│                                         │
│ 🔹 EIT (Event Information Table):      │
│   • Розклад передач (EPG)              │
│   • Назви, час, опис подій             │
│                                         │
│ 🔹 SDT (Service Description Table):    │
│   • Назви каналів, провайдери          │
│   • Статус (running/off-air)           │
│                                         │
│ 🔹 TOT (Time Offset Table):            │
│   • Поточний час UTC + таймзони        │
│   • Синхронізація з реальним часом     │
│                                         │
│ 🔹 NIT (Network Information Table):    │
│   • Інформація про мережу (опціонально)│
└─────────────────────────────────────────┘
```

---

## 🔧 Архітектура: ієрархія PSI структур

```
┌─────────────────────────────────────────┐
│ PSIData (корінь)                        │
│ • PointerField: зміщення до першої секції│
│ • Sections[]: список секцій             │
│                                         │
│ └─► PSISection (одна таблиця)           │
│     • CRC32: валідація цілісності       │
│     • Header: загальний заголовок       │
│     • Syntax: специфічні дані таблиці   │
│                                         │
│     └─► PSISectionHeader                │
│         • TableID: тип таблиці (0=PAT)  │
│         • SectionLength: довжина даних  │
│         • SectionSyntaxIndicator: 1=є синтаксис│
│         • PrivateBit: 0 для PAT/PMT/CAT │
│                                         │
│     └─► PSISectionSyntax                │
│         • Header: syntax-specific заголовок│
│         • Data: PATData/PMTData/EITData...│
│                                         │
│         └─► PSISectionSyntaxHeader      │
│             • TableIDExtension: TS ID / program_number│
│             • VersionNumber: 0-31, інкремент при зміні│
│             • CurrentNextIndicator: 1=актуальна зараз│
│             • SectionNumber/LastSectionNumber: для багатосекційних таблиць│
└─────────────────────────────────────────┘
```

---

## 🔍 Функція `parsePSIData`: головний вхідний пункт

```go
func parsePSIData(i *astikit.BytesIterator) (*PSIData, error) {
    d := &PSIData{}
    
    // 🔹 1. PointerField: зміщення до першої секції
    b, _ := i.NextByte()
    d.PointerField = int(b)
    
    // 🔹 2. Пропустити filler bytes (вирівнювання)
    i.Skip(d.PointerField)
    
    // 🔹 3. Цикл парсингу секцій до кінця потоку або stop-умови
    for i.HasBytesLeft() && !stop {
        s, stop, err := parsePSISection(i)
        if err != nil {
            return nil, fmt.Errorf("astits: parsing PSI table failed: %w", err)
        }
        d.Sections = append(d.Sections, s)
    }
    
    return d, nil
}
```

### Ключовий момент: `PointerField`

```
PointerField — це 1 байт, що вказує, скільки байт пропустити перед першою секцією.

Призначення:
• Вирівнювання секцій на початку TS-пакету
• Пропуск приватних даних перед таблицею
• Забезпечення синхронізації при фрагментації

Приклад:
[04][74][65][73][74]...  → PointerField=4, пропустити "test", далі table_id
```

> 💡 **Важливо**: `PayloadUnitStartIndicator=1` у заголовку пакету сигналізує, що payload починається з PointerField.

---

## 🔍 Функція `parsePSISection`: парсинг однієї таблиці

```go
func parsePSISection(i *astikit.BytesIterator) (*PSISection, bool, error) {
    s := &PSISection{}
    
    // 🔹 1. Парсинг загального заголовка
    h, offsetStart, _, offsetSectionsEnd, offsetEnd, err := parsePSISectionHeader(i)
    if err != nil { return nil, false, err }
    
    s.Header = h
    
    // 🔹 2. Перевірка stop-умови (Null/Unknown таблиці)
    if shouldStopPSIParsing(h.TableID) {
        return s, true, nil  // stop=true → припинити парсинг подальших секцій
    }
    
    // 🔹 3. Парсинг syntax секції (якщо є)
    if h.SectionLength > 0 {
        s.Syntax, err = parsePSISectionSyntax(i, h, offsetSectionsEnd)
        if err != nil { return nil, false, err }
        
        // 🔹 4. Валідація CRC32 (для таблиць, що його мають)
        if h.TableID.hasCRC32() {
            i.Seek(offsetSectionsEnd)  // перейти до CRC32
            s.CRC32, _ = parseCRC32(i)
            
            // Обчислити CRC для даних секції
            i.Seek(offsetStart)
            crc32Data, _ := i.NextBytesNoCopy(offsetSectionsEnd - offsetStart)
            computedCRC := computeCRC32(crc32Data)
            
            if computedCRC != s.CRC32 {
                return nil, false, fmt.Errorf("astits: Table CRC32 %x != computed CRC32 %x", s.CRC32, computedCRC)
            }
        }
    }
    
    // 🔹 5. Перейти до кінця секції
    i.Seek(offsetEnd)
    
    return s, false, nil
}
```

### Розрахунок офсетів у `parsePSISectionHeader`

```go
func parsePSISectionHeader(i *astikit.BytesIterator) (h *PSISectionHeader, offsetStart, offsetSectionsStart, offsetSectionsEnd, offsetEnd int, error) {
    offsetStart = i.Offset()  // початок секції
    
    // Читання table_id (1 байт)
    b, _ := i.NextByte()
    h.TableID = PSITableID(b)
    h.TableType = h.TableID.Type()
    
    // Читання прапорців + section_length (2 байти)
    bs, _ := i.NextBytesNoCopy(2)
    h.SectionSyntaxIndicator = bs[0]&0x80 > 0  // біт 7
    h.PrivateBit = bs[0]&0x40 > 0              // біт 6
    h.SectionLength = uint16(bs[0]&0xf)<<8 | uint16(bs[1])  // 12 біт
    
    // 🔹 Розрахунок офсетів:
    offsetSectionsStart = i.Offset()  // після заголовка (3 байти від offsetStart)
    offsetEnd = offsetSectionsStart + int(h.SectionLength)  // кінець секції (включно з даними + CRC)
    offsetSectionsEnd = offsetEnd
    if h.TableID.hasCRC32() {
        offsetSectionsEnd -= 4  // мінус CRC32 (4 байти)
    }
    
    return h, offsetStart, offsetSectionsStart, offsetSectionsEnd, offsetEnd, nil
}
```

**Візуалізація офсетів:**
```
Потік байтів:
[0] table_id
[1] flags + section_length[11:8]
[2] section_length[7:0]
[3...] дані секції (section_length байт)
[...-4...-1] CRC32 (якщо є)

offsetStart = 0
offsetSectionsStart = 3  // після 3-байтного заголовка
offsetEnd = 3 + section_length  // кінець секції
offsetSectionsEnd = offsetEnd - 4 (якщо є CRC32)  // перед CRC32
```

---

## 🔐 CRC32 валідація: чому це критично?

```go
// parseCRC32 читає 4 байти у big-endian форматі
func parseCRC32(i *astikit.BytesIterator) (uint32, error) {
    bs, _ := i.NextBytesNoCopy(4)
    return uint32(bs[0])<<24 | uint32(bs[1])<<16 | uint32(bs[2])<<8 | uint32(bs[3]), nil
}

// computeCRC32 використовує MPEG-2 поліном 0x04c11db7
func computeCRC32(data []byte) uint32 {
    crc := crc32.New(crc32.MakeTable(0x04c11db7))  // ⚠️ Не IEEE 0xEDB88320!
    crc.Write(data)
    return crc.Sum32()
}
```

> ⚠️ **Критично**: MPEG-2 використовує поліном `0x04c11db7`, а не стандартний IEEE `0xEDB88320`. Неправильний поліном → всі CRC перевірки проваляться.

### Коли CRC32 обов'язковий?

```go
func (t PSITableID) hasCRC32() bool {
    return t == PSITableIDPAT ||  // ✅
           t == PSITableIDPMT ||  // ✅
           t == PSITableIDTOT ||  // ✅
           t == PSITableIDNITVariant1 || t == PSITableIDNITVariant2 ||  // ✅
           t == PSITableIDSDTVariant1 || t == PSITableIDSDTVariant2 ||  // ✅
           (t >= PSITableIDEITStart && t <= PSITableIDEITEnd)  // ✅ EIT діапазон
    // ❌ TDT, DIT, RST, SIT, ST, BAT — без CRC
}
```

---

## 🗂️ Типи таблиць: мапінг table_id → тип

```go
type PSITableID uint16

const (
    PSITableIDPAT  PSITableID = 0x00  // Program Association Table
    PSITableIDPMT  PSITableID = 0x02  // Program Map Table
    PSITableIDBAT  PSITableID = 0x4a  // Bouquet Association Table
    PSITableIDDIT  PSITableID = 0x7e  // Discontinuity Information Table
    PSITableIDRST  PSITableID = 0x71  // Running Status Table
    PSITableIDSIT  PSITableID = 0x7f  // Selection Information Table
    PSITableIDST   PSITableID = 0x72  // Stuffing Table
    PSITableIDTDT  PSITableID = 0x70  // Time Date Table
    PSITableIDTOT  PSITableID = 0x73  // Time Offset Table
    PSITableIDNull PSITableID = 0xff  // Null table (заповнення)
    
    // Діапазони:
    PSITableIDEITStart    PSITableID = 0x4e  // Event Information Table (actual)
    PSITableIDEITEnd      PSITableID = 0x6f  // Event Information Table (other)
    PSITableIDSDTVariant1 PSITableID = 0x42  // Service Description Table (actual)
    PSITableIDSDTVariant2 PSITableID = 0x46  // Service Description Table (other)
    PSITableIDNITVariant1 PSITableID = 0x40  // Network Information Table (actual)
    PSITableIDNITVariant2 PSITableID = 0x41  // Network Information Table (other)
)
```

### Метод `Type()`: table_id → human-readable тип

```go
func (t PSITableID) Type() string {
    switch {
    case t == PSITableIDPAT: return "PAT"
    case t == PSITableIDPMT: return "PMT"
    case t >= PSITableIDEITStart && t <= PSITableIDEITEnd: return "EIT"
    case t == PSITableIDSDTVariant1, t == PSITableIDSDTVariant2: return "SDT"
    case t == PSITableIDTOT: return "TOT"
    // ... інші випадки ...
    default: return "Unknown"
    }
}
```

---

## 🔄 `toData()`: конвертація PSI → DemuxerData

```go
func (d *PSIData) toData(firstPacket *Packet, pid uint16) []*DemuxerData {
    ds := make([]*DemuxerData, 0, len(d.Sections))
    
    for _, s := range d.Sections {
        // Пропустити секції без даних
        if s.Syntax == nil || s.Syntax.Data == nil {
            continue
        }
        
        // 🔹 Створити DemuxerData для кожного типу таблиці
        switch s.Header.TableID {
        case PSITableIDPAT:
            ds = append(ds, &DemuxerData{FirstPacket: firstPacket, PAT: s.Syntax.Data.PAT, PID: pid})
        case PSITableIDPMT:
            ds = append(ds, &DemuxerData{FirstPacket: firstPacket, PID: pid, PMT: s.Syntax.Data.PMT})
        case PSITableIDSDTVariant1, PSITableIDSDTVariant2:
            ds = append(ds, &DemuxerData{FirstPacket: firstPacket, PID: pid, SDT: s.Syntax.Data.SDT})
        case PSITableIDTOT:
            ds = append(ds, &DemuxerData{FirstPacket: firstPacket, PID: pid, TOT: s.Syntax.Data.TOT})
        }
        
        // 🔹 EIT діапазон
        if s.Header.TableID >= PSITableIDEITStart && s.Header.TableID <= PSITableIDEITEnd {
            ds = append(ds, &DemuxerData{EIT: s.Syntax.Data.EIT, FirstPacket: firstPacket, PID: pid})
        }
    }
    
    return ds
}
```

**Призначення `DemuxerData`:**
```
• FirstPacket: посилання на перший пакет секції (для метаданих заголовка)
• PID: PID потоку, з якого отримано дані
• PAT/PMT/EIT/SDT/TOT: парсена структура таблиці

Це дозволяє демуксеру повертати однорідний тип для всіх таблиць,
а клієнтський код використовує type switch або перевірку полів.
```

---

## ✏️ Серіалізація: `writePSIData` та допоміжні функції

### `writePSIData`: запис усіх секцій

```go
func writePSIData(w *astikit.BitsWriter, d *PSIData) (int, error) {
    b := astikit.NewBitsWriterBatch(w)
    
    // 🔹 PointerField + filler bytes
    b.Write(uint8(d.PointerField))
    for i := 0; i < d.PointerField; i++ {
        b.Write(uint8(0x00))  // заповнення нулями
    }
    
    bytesWritten := 1 + d.PointerField
    
    // 🔹 Записати кожну секцію
    for _, s := range d.Sections {
        n, err := writePSISection(w, s)
        if err != nil { return 0, err }
        bytesWritten += n
    }
    
    return bytesWritten, b.Err()
}
```

### `writePSISection`: запис однієї секції з CRC32

```go
func writePSISection(w *astikit.BitsWriter, s *PSISection) (int, error) {
    // 🔹 Підтримуються тільки PAT/PMT (TODO: інші типи)
    if s.Header.TableID != PSITableIDPAT && s.Header.TableID != PSITableIDPMT {
        return 0, fmt.Errorf("writePSISection: table %s is not implemented", s.Header.TableID.Type())
    }
    
    b := astikit.NewBitsWriterBatch(w)
    
    // 🔹 Розрахувати довжину секції
    sectionLength := calcPSISectionLength(s)
    
    // 🔹 Callback для обчислення CRC32 "на льоту"
    sectionCRC32 := crc32Polynomial  // початкове значення
    if s.Header.TableID.hasCRC32() {
        w.SetWriteCallback(func(bs []byte) {
            sectionCRC32 = updateCRC32(sectionCRC32, bs)  // оновлювати при кожному записі
        })
        defer w.SetWriteCallback(nil)  // скинути callback після запису
    }
    
    // 🔹 Записати заголовок секції
    b.Write(uint8(s.Header.TableID))
    b.Write(s.Header.SectionSyntaxIndicator)
    b.Write(s.Header.PrivateBit)
    b.WriteN(uint8(0xff), 2)  // reserved = 0b11
    b.WriteN(sectionLength, 12)  // 12-бітна довжина
    bytesWritten := 3
    
    // 🔹 Записати syntax секцію (дані + заголовок)
    if s.Header.SectionLength > 0 {
        n, err := writePSISectionSyntax(w, s)
        if err != nil { return 0, err }
        bytesWritten += n
        
        // 🔹 Додати CRC32 в кінці
        if s.Header.TableID.hasCRC32() {
            b.Write(sectionCRC32)  // записати обчислений CRC
            bytesWritten += 4
        }
    }
    
    return bytesWritten, b.Err()
}
```

> 💡 **Патерн `SetWriteCallback`**: дозволяє обчислювати CRC32 під час запису даних, без необхідності буферизувати всю секцію в пам'яті.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Обробка PAT/PMT для ідентифікації потоків

```go
// У VideoManifestProxy — отримання PID відео/аудіо:
func extractStreamPIDs(data *astits.DemuxerData) (videoPID, audioPID, pcrPID uint16, err error) {
    if data.PAT == nil {
        return 0, 0, 0, fmt.Errorf("no PAT data")
    }
    
    // 🔹 Знайти PMT PID для програми 1 (основна)
    var pmtPID uint16
    for _, prog := range data.PAT.Programs {
        if prog.ProgramNumber == 1 {  // основна програма
            pmtPID = prog.ProgramMapID
            break
        }
    }
    if pmtPID == 0 {
        return 0, 0, 0, fmt.Errorf("program 1 not found in PAT")
    }
    
    // 🔹 Читати PMT (має бути в тому ж потокі або окремо)
    if data.PMT == nil {
        return 0, 0, 0, fmt.Errorf("no PMT data for program 1")
    }
    
    pcrPID = data.PMT.PCRPID
    
    // 🔹 Знайти відео та аудіо потоки
    for _, es := range data.PMT.ElementaryStreams {
        switch es.StreamType {
        case astits.StreamTypeH264Video, astits.StreamTypeHEVCVideo:
            videoPID = es.ElementaryPID
        case astits.StreamTypeADTS, astits.StreamTypeAACAudio:
            audioPID = es.ElementaryPID
        }
    }
    
    return videoPID, audioPID, pcrPID, nil
}
```

### ✅ 2: Генерація EPG через EIT

```go
// У VideoManifestProxy — додавання метаданих подій до плейлиста:
func addEPGToPlaylist(eit *astits.EITData, playlist *HLSPlaylist) {
    for _, event := range eit.Events {
        // 🔹 Форматувати час початку
        startTime := event.StartTime.Format(time.RFC3339)
        
        // 🔹 Додати #EXT-X-PROGRAM-DATE-TIME
        playlist.AddTag(fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s", startTime))
        
        // 🔹 Додати назву події як коментар (опціонально)
        if event.EventName != "" {
            playlist.AddComment(fmt.Sprintf("# %s", event.EventName))
        }
        
        // 🔹 Додати #EXTINF з тривалістю
        duration := event.Duration.Seconds()
        playlist.AddTag(fmt.Sprintf("#EXTINF:%.3f,", duration))
    }
}
```

### ✅ 3: Синхронізація часу через TOT

```go
// У VideoManifestProxy — розрахунок точного часу для сегментів:
type TimeSyncState struct {
    baseTOTTime   time.Time
    basePCR       *astits.ClockReference
    timezoneOffset time.Duration
}

func handleTOT(tot *astits.TOTData, pcr *astits.ClockReference, channelID string) {
    // 🔹 Зберегти базову синхронізацію
    syncState[channelID] = &TimeSyncState{
        baseTOTTime: tot.UTCTime,
        basePCR:     pcr,
    }
    
    // 🔹 Обробити дескриптори для зсуву часу
    for _, desc := range tot.Descriptors {
        if desc.Tag == astits.DescriptorTagLocalTimeOffset && desc.LocalTimeOffset != nil {
            for _, item := range desc.LocalTimeOffset.Items {
                polarity := 1
                if item.LocalTimeOffsetPolarity {
                    polarity = -1
                }
                offset := time.Duration(polarity * int(item.LocalTimeOffset/time.Minute)) * time.Minute
                syncState[channelID].timezoneOffset = offset
            }
        }
    }
}

func calculateProgramDateTime(pcr *astits.ClockReference, channelID string) time.Time {
    state := syncState[channelID]
    if state == nil || state.basePCR == nil {
        return time.Now().UTC()  // fallback
    }
    
    // 🔹 Розрахувати різницю між поточним PCR та базовим
    pcrDiff := pcr.Duration() - state.basePCR.Duration()
    
    // 🔹 Додати до базового TOT часу + таймзона
    return state.baseTOTTime.Add(pcrDiff).Add(state.timezoneOffset)
}
```

### ✅ 4: Фільтрація активних каналів через SDT

```go
// У генерації плейлиста — показувати тільки "running" канали:
func generateChannelPlaylist(sdt *astits.SDTData, activeServiceID uint16) *HLSPlaylist {
    playlist := NewHLSPlaylist()
    
    for _, svc := range sdt.Services {
        // 🔹 Пропустити неактивні канали
        if svc.RunningStatus != astits.RunningStatusRunning {
            log.Debugf("Skipping service %d: status=%d", svc.ServiceID, svc.RunningStatus)
            continue
        }
        
        // 🔹 Додати канал у плейлист
        meta := extractServiceMetadata(svc)
        playlist.AddChannel(meta.ServiceID, meta.Name, meta.Provider)
        
        // 🔹 Якщо це активний канал — додати сегменти
        if svc.ServiceID == activeServiceID {
            addSegmentsToPlaylist(playlist, activeServiceID)
        }
    }
    
    return playlist
}
```

### ✅ 5: Моніторинг цілісності таблиць

```go
// monitoring.Monitor — метрики для PSI:
type PSIMetrics struct {
    TablesParsed    *prometheus.CounterVec  // кількість парсингів по типу
    CRCFailures     *prometheus.CounterVec  // помилки валідації CRC
    UnknownTables   *prometheus.CounterVec  // невідомі table_id
    SectionLatency  *prometheus.HistogramVec  // латентність парсингу секції
}

// У парсингу:
func parsePSIWithMetrics(iter *astikit.BytesIterator, channelID string, metrics *PSIMetrics) (*astits.PSIData, error) {
    start := time.Now()
    data, err := parsePSIData(iter)
    latency := time.Since(start)
    
    if err != nil {
        if strings.Contains(err.Error(), "CRC32") {
            metrics.CRCFailures.WithLabelValues(channelID).Inc()
        }
        return nil, err
    }
    
    for _, sec := range data.Sections {
        metrics.TablesParsed.WithLabelValues(
            channelID,
            psiTableTypeName(sec.Header.TableType),  // human-readable name
        ).Inc()
        
        if sec.Header.TableType == astits.PSITableTypeUnknown {
            metrics.UnknownTables.WithLabelValues(channelID).Inc()
        }
    }
    
    metrics.SectionLatency.WithLabelValues(channelID).Observe(latency.Seconds())
    return data, nil
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Базовий тест на парсинг з валідацією CRC

```go
func TestParsePSIData_CRCValidation(t *testing.T) {
    // 🔹 Кейс 1: Невірний CRC32
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    w.Write(uint8(0))       // pointer_field = 0
    w.Write(uint8(115))     // table_id = 115 (TOT)
    w.Write("1")            // syntax_section_indicator
    w.Write("1")            // private_bit
    w.Write("11")           // reserved
    w.Write("000000001110") // section_length = 14
    w.Write(totBytes())     // дані TOT
    w.Write(uint32(32))     // ❌ неправильний CRC32 (має бути 0x6969b13)
    
    _, err := parsePSIData(astikit.NewBytesIterator(buf.Bytes()))
    // ✅ Очікуємо помилку валідації CRC
    assert.EqualError(t, err, "astits: parsing PSI table failed: astits: Table CRC32 20 != computed CRC32 6969b13")
    
    // 🔹 Кейс 2: Валідний потік
    _, err = parsePSIData(astikit.NewBytesIterator(psiBytes()))
    assert.NoError(t, err)  // ✅ всі CRC валідні
}
```

### 🔹 Тест на обробку PointerField

```go
func TestParsePSIData_PointerField(t *testing.T) {
    // Створити потік з PointerField=4 + "test" + валідна секція
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.Write(uint8(4))           // PointerField = 4
    w.Write([]byte("test"))     // 4 байти вирівнювання
    // Далі — валідна PAT секція...
    writePATSection(w, testPAT)
    
    data, err := parsePSIData(astikit.NewBytesIterator(buf.Bytes()))
    assert.NoError(t, err)
    assert.Equal(t, 4, data.PointerField)
    assert.Len(t, data.Sections, 1)
    assert.NotNil(t, data.Sections[0].PAT)
}
```

### 🔹 Тест на round-trip (парсинг ↔ запис)

```go
func TestPSI_RoundTrip(t *testing.T) {
    // 🔹 1. Створити еталонну PSIData
    original := &astits.PSIData{
        PointerField: 0,
        Sections: []*astits.PSISection{
            {
                Header: &astits.PSISectionHeader{
                    TableID: astits.PSITableIDPAT,
                    SectionSyntaxIndicator: true,
                    PrivateBit: false,
                    SectionLength: 17,  // розраховується
                },
                Syntax: &astits.PSISectionSyntax{
                    Header: &astits.PSISectionSyntaxHeader{
                        TableIDExtension: 1,
                        VersionNumber: 0,
                        CurrentNextIndicator: true,
                        SectionNumber: 0,
                        LastSectionNumber: 0,
                    },
                    Data: &astits.PSISectionSyntaxData{
                        PAT: &astits.PATData{
                            TransportStreamID: 1,
                            Programs: []*astits.PATProgram{
                                {ProgramNumber: 1, ProgramMapID: 0x1000},
                            },
                        },
                    },
                },
                // CRC32 буде обчислений при записі
            },
        },
    }
    
    // 🔹 2. Серіалізувати
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    _, err := writePSIData(w, original)
    assert.NoError(t, err)
    
    // 🔹 3. Парсити назад
    parsed, err := parsePSIData(astikit.NewBytesIterator(buf.Bytes()))
    assert.NoError(t, err)
    
    // 🔹 4. Порівняти ключові поля (CRC може відрізнятися через обчислення)
    assert.Equal(t, original.PointerField, parsed.PointerField)
    assert.Len(t, parsed.Sections, 1)
    assert.Equal(t, original.Sections[0].Header.TableID, parsed.Sections[0].Header.TableID)
    assert.NotNil(t, parsed.Sections[0].Syntax.Data.PAT)
    assert.Equal(t, original.Sections[0].Syntax.Data.PAT.TransportStreamID, 
                 parsed.Sections[0].Syntax.Data.PAT.TransportStreamID)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірний CRC32 | Помилка "Table CRC32 X != computed CRC32 Y" | Перевірити, що обчислення CRC використовує поліном 0x04c11db7 (MPEG-2); перевірити діапазон даних для CRC (без заголовка та без самого CRC) |
| PointerField не враховано | Парсинг починається не з table_id | Перевірити, що `i.Skip(int(pointerField))` викликається перед читанням першої секції |
| section_length неправильний | Цикл читання даних зупиняється завчасно або читає зайве | Перевірити розрахунок: `section_length` = довжина решти секції після байта 2 (включно з даними та CRC, але без заголовка) |
| Невідомий table_id панікує | `PSITableTypeUnknown` не обробляється | Додати fallback у `parsePSISection`: `if tableType == PSITableTypeUnknown { skip section data }` |
| Версія таблиці не інкрементується | Клієнти не бачать оновлень метаданих | Переконатися, що при зміні вмісту PAT/PMT інкрементується `version_number` перед серіалізацією |
| `writePSISection` не підтримує EIT/SDT | Помилка "table EIT is not implemented" | Реалізувати `writeEITSection`/`writeSDTSection` або використовувати тільки PAT/PMT для базового функціонування |

### Приклад коректного розрахунку CRC32:

```go
func computePSICRC(data []byte, offsetStart, offsetEnd int) uint32 {
    // CRC обчислюється тільки для даних секції (без заголовка та без самого CRC)
    // offsetStart = після заголовка (4 байти: table_id + flags + section_length)
    // offsetEnd = перед CRC32 (останні 4 байти секції)
    
    // ⚠️ Використовувати MPEG-2 поліном, не IEEE!
    crcTable := crc32.MakeTable(0x04c11db7)
    crc := crc32.New(crcTable)
    
    crc.Write(data[offsetStart:offsetEnd])
    return crc.Sum32()
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Парсинг PSI з вхідного потоку:
func parsePSIStream(reader io.Reader) (*astits.PSIData, error) {
    data, err := io.ReadAll(reader)
    if err != nil {
        return nil, err
    }
    return parsePSIData(astikit.NewBytesIterator(data))
}

// 2: Витягування PID з PAT/PMT:
func extractPIDsFromPSI(psi *astits.PSIData) (map[uint16]StreamInfo, error) {
    streams := make(map[uint16]StreamInfo)
    
    // 🔹 Знайти PAT
    var pat *astits.PATData
    for _, sec := range psi.Sections {
        if sec.PAT != nil {
            pat = sec.PAT
            break
        }
    }
    if pat == nil {
        return nil, fmt.Errorf("no PAT found")
    }
    
    // 🔹 Для кожної програми знайти PMT
    for _, prog := range pat.Programs {
        if prog.ProgramNumber == 0 {
            continue  // NIT, пропускаємо
        }
        
        // Шукаємо PMT з program_map_pid = prog.ProgramMapID
        for _, sec := range psi.Sections {
            if sec.PMT != nil && sec.Header.TableIDExtension == prog.ProgramMapID {
                pmt := sec.PMT
                for _, es := range pmt.ElementaryStreams {
                    streams[es.ElementaryPID] = StreamInfo{
                        ProgramNumber: prog.ProgramNumber,
                        StreamType:    es.StreamType,
                        PCR:           pmt.PCRPID == es.ElementaryPID,
                    }
                }
                break
            }
        }
    }
    
    return streams, nil
}

// 3: Форматування PROGRAM-DATE-TIME для HLS:
func formatProgramDateTime(t time.Time) string {
    // HLS вимагає RFC3339 / ISO8601
    return t.UTC().Format("2006-01-02T15:04:05.000Z")
    // Приклад: "2024-05-15T14:30:45.000Z"
}

// 4: Моніторинг цілісності:
func validatePSIIntegrity(psi *astits.PSIData, channelID string, metrics *PSIMetrics) error {
    for _, sec := range psi.Sections {
        if sec.Header.TableType == astits.PSITableTypeUnknown {
            log.Warnf("Channel %s: unknown PSI table_id=%d", channelID, sec.Header.TableID)
            metrics.UnknownTables.WithLabelValues(channelID).Inc()
        }
        
        // 🔹 Перевірити version_number на монотонність (опціонально)
        // 🔹 Перевірити current_next_indicator
    }
    return nil
}
```

---

## 📊 Матриця PSI таблиць для вашого пайплайну

```
Таблиця | table_id | Обов'язкова? | Використання у CCTV HLS
────────┼──────────┼──────────────┼─────────────────────────
PAT     | 0x00     | ✅ Так       | ✅ Ідентифікація програм → PMT PID
PMT     | 0x02     | ✅ Для програми | ✅ Список відео/аудіо потоків, PCR PID
EIT     | 0x4E-0x6F| ⚠️ Опціонально | ✅ EPG: назви, час, опис подій
SDT     | 0x42/0x46| ⚠️ Опціонально | ✅ Назви каналів, провайдери, статус
TOT     | 0x73     | ⚠️ Опціонально | ✅ Синхронізація часу з реальним світом
NIT     | 0x40/0x41| ❌ Ні        | ⚠️ Мережеві дані (рідко використовується)
CAT     | 0x01     | ❌ Ні        | ❌ Умовний доступ (шифрування) — ігноруємо
```

---

## 📚 Корисні посилання

- [ETSI EN 300 468: PSI/SI specification](https://dvb.org/wp-content/uploads/2019/12/a038_tm1217r37_en300468v1_17_1_-_rev-134_-_si_specification.pdf)
- [MPEG-TS PSI tables overview](https://en.wikipedia.org/wiki/Program-specific_information)
- [astits PSI parsing source](https://github.com/asticode/go-astits/blob/master/data.go)
- [CRC32 MPEG-2 polynomial](https://reveng.sourceforge.net/crc-catalogue/all.htm#crc.cat.crc-32-mpeg-2)

> 💡 **Ключова ідея**: PSI — це "мова метаданих" MPEG-TS. Кожна таблиця — стандартизований спосіб передати інформацію про потік без зміни основного формату. У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Автоматично ідентифікувати відео/аудіо потоки через PAT/PMT
> - 📺 Збагачувати HLS-плейлисти EPG даними через EIT/SDT
> - 🕐 Синхронізувати таймінги з реальним часом через TOT
> - 🔍 Відлагоджувати нові формати через логування `Unknown` таблиць

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати PSI-парсинг у ваш `segmentAssembler` для динамічного оновлення метаданих
- 🧪 Написати integration-тест для перевірки сумісності з реальними енкодерами
- 📈 Додати Prometheus-метрики для моніторингу цілісності PSI таблиць по каналах

🛠️