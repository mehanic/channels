# Глибоке роз'яснення: Тести PSI (Program Specific Information) у astits — парсинг та серіалізація таблиць

Цей файл містить **комплексні тести парсингу та запису PSI даних** — фундаментального механізму MPEG-TS для передачі метаданих: PAT, PMT, EIT, NIT, SDT, TOT тощо. Це "серце" демуксера, що перетворює сирі байти на структуровані таблиці.

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

## 🔧 Архітектура тестів: стратегія валідації

### 📦 Глобальна змінна `psi` — еталонна структура

```go
var psi = &PSIData{
    PointerField: 4,
    Sections: []*PSISection{
        {CRC32: 0x7ffc6102, Header: &PSISectionHeader{TableID: 78, TableType: PSITableTypeEIT}, Syntax: &PSISectionSyntax{Data: &PSISectionSyntaxData{EIT: eit}, Header: psiSectionSyntaxHeader}},
        {CRC32: 0xfebaa941, Header: &PSISectionHeader{TableID: 64, TableType: PSITableTypeNIT}, Syntax: &PSISectionSyntax{Data: &PSISectionSyntaxData{NIT: nit}, Header: psiSectionSyntaxHeader}},
        {CRC32: 0x60739f61, Header: &PSISectionHeader{TableID: 0, TableType: PSITableTypePAT}, Syntax: &PSISectionSyntax{Data: &PSISectionSyntaxData{PAT: pat}, Header: psiSectionSyntaxHeader}},
        {CRC32: 0xc68442e8, Header: &PSISectionHeader{TableID: 2, TableType: PSITableTypePMT}, Syntax: &PSISectionSyntax{Data: &PSISectionSyntaxData{PMT: pmt}, Header: psiSectionSyntaxHeader}},
        {CRC32: 0xef3751d6, Header: &PSISectionHeader{TableID: 66, TableType: PSITableTypeSDT}, Syntax: &PSISectionSyntax{Data: &PSISectionSyntaxData{SDT: sdt}, Header: psiSectionSyntaxHeader}},
        {CRC32: 0x6969b13, Header: &PSISectionHeader{TableID: 115, TableType: PSITableTypeTOT}, Syntax: &PSISectionSyntax{Data: &PSISectionSyntaxData{TOT: tot}}},
        {Header: &PSISectionHeader{TableID: 254, TableType: PSITableTypeUnknown}},  // Unknown table
    },
}
```

**Ключові моменти:**
- `PointerField: 4` — зміщення до першої секції (4 байти "test" для вирівнювання)
- Кожна секція має: `Header` (метадані), `Syntax` (дані), `CRC32` (валідація)
- Остання секція з `TableID=254` тестує обробку невідомих типів

---

### 🔁 `psiBytes()` — генератор "еталонних" байтів

```go
func psiBytes() []byte {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // 🔹 Pointer field + padding
    w.Write(uint8(4))           // зміщення до першої секції
    w.Write([]byte("test"))     // 4 байти вирівнювання
    
    // 🔹 Цикл по всіх секціях (EIT, NIT, PAT, PMT, SDT, TOT, Unknown)
    // Для кожної:
    w.Write(uint8(tableID))                 // table_id
    w.Write("1")                            // syntax_section_indicator = 1
    w.Write("1")                            // private_bit = 1
    w.Write("11")                           // reserved = 0b11
    w.Write("000000011110")                // section_length (12 біт)
    w.Write(psiSectionSyntaxHeaderBytes()) // syntax header (version, current_next...)
    w.Write(eitBytes())                    // дані секції (напр., EIT)
    w.Write(uint32(0x7ffc6102))            // CRC32
    
    // ... повтор для інших типів ...
    
    return buf.Bytes()
}
```

**Формат однієї PSI секції:**
```
[8]  table_id                    ← визначає тип (0=PAT, 2=PMT, 66=SDT...)
[1]  section_syntax_indicator    ← 1 = syntax секція (PAT/PMT/EIT...), 0 = без синтаксису (TOT)
[1]  private_bit                 ← зазвичай 1
[2]  reserved                    ← 0b11
[12] section_length              ← довжина решти секції (байти)
[16] table_id_extension          ← transport_stream_id або program_number
[2]  reserved                    ← 0b11
[5]  version_number              ← 0-31, інкремент при зміні вмісту
[1]  current_next_indicator      ← 1 = актуальна зараз
[8]  section_number              ← 0 для односекційних таблиць
[8]  last_section_number         ← 0 для односекційних
[... data ...]                   ← залежить від table_id (PAT/PMT/EIT...)
[32] CRC32                       ← валідація цілісності
```

---

## 🧪 Тест `TestParsePSIData`: парсинг та валідація

### Кейс 1: Невірний CRC32

```go
func TestParsePSIData(t *testing.T) {
    // 🔹 Створити потік з неправильним CRC
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
}
```

**Чому CRC32 критичний?**
```
• Пошкоджені пакети в мережі/на диску → некоректні метадані
• Плеєр може відтворити "сміття" або впасти
• CRC32 (MPEG-2 поліном 0x04c11db7) гарантує цілісність
• astits обчислює CRC і порівнює з вказаним у секції
```

### Кейс 2: Валідний потік з кількома секціями

```go
// 🔹 Парсинг повного потоку з 7 секціями
d, err := parsePSIData(astikit.NewBytesIterator(psiBytes()))
assert.NoError(t, err)
assert.Equal(t, d, psi)  // ✅ структурна рівність з еталоном
```

**Що перевіряється:**
1. `PointerField` коректно пропускає вирівнюючі байти
2. Кожна секція парситься з правильним `table_id` → `TableType`
3. `Syntax` секції (PAT/PMT/EIT...) парсяться через специфічні функції
4. `CRC32` валідується для кожної секції
5. Невідомий `table_id=254` обробляється як `PSITableTypeUnknown` без помилки

---

## 🔍 Допоміжні тести: заголовки та типи

### `TestParsePSISectionHeader`: парсинг загального заголовка

```go
func TestParsePSISectionHeader(t *testing.T) {
    // 🔹 Кейс 1: Невідомий table_id
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    w.Write(uint8(254))  // table_id = 254 (не в стандарті)
    w.Write("1")         // syntax_section_indicator
    w.Write("0000000")   // решта байта
    
    d, _, _, _, _, err := parsePSISectionHeader(astikit.NewBytesIterator(buf.Bytes()))
    assert.Equal(t, d, &PSISectionHeader{
        TableID:   254,
        TableType: PSITableTypeUnknown,  // ✅ fallback для невідомих типів
    })
    assert.NoError(t, err)
    
    // 🔹 Кейс 2: Валідний заголовок з розрахунком офсетів
    d, offsetStart, offsetSectionsStart, offsetSectionsEnd, offsetEnd, err := 
        parsePSISectionHeader(astikit.NewBytesIterator(psiSectionHeaderBytes()))
    
    assert.Equal(t, d, psiSectionHeader)
    assert.Equal(t, 0, offsetStart)           // початок секції
    assert.Equal(t, 3, offsetSectionsStart)   // після заголовка (3 байти)
    assert.Equal(t, 2729, offsetSectionsEnd)  // перед CRC32
    assert.Equal(t, 2733, offsetEnd)          // кінець секції (включно з CRC)
    assert.NoError(t, err)
}
```

**Розрахунок офсетів:**
```
Вхід: section_length = 2730 (0b101010101010)

offsetStart = 0 (початок ітератора)
offsetSectionsStart = offsetStart + 3 = 3  // table_id(1) + flags(1) + section_length(2) = 4 байти, але враховуємо біти
offsetSectionsEnd = offsetSectionsStart + section_length - 4 = 3 + 2730 - 4 = 2729  // мінус CRC32(4)
offsetEnd = offsetSectionsEnd + 4 = 2733  // додаємо CRC32

Ці офсети використовуються для:
• Читання даних секції: i.Seek(offsetSectionsStart) ... i.Seek(offsetSectionsEnd)
• Валідації CRC32: обчислити для діапазону [offsetSectionsStart:offsetSectionsEnd]
```

### `TestPSITableType`: мапінг table_id → тип

```go
func TestPSITableType(t *testing.T) {
    // 🔹 EIT діапазон: 0x4E-0x6F
    for i := PSITableIDEITStart; i <= PSITableIDEITEnd; i++ {
        assert.Equal(t, PSITableTypeEIT, i.Type())
    }
    
    // 🔹 Окремі table_id
    assert.Equal(t, PSITableTypeDIT, PSITableIDDIT.Type())           // 0x7E
    assert.Equal(t, PSITableTypeNIT, PSITableIDNITVariant1.Type())   // 0x40
    assert.Equal(t, PSITableTypeNIT, PSITableIDNITVariant2.Type())   // 0x41
    assert.Equal(t, PSITableTypeSDT, PSITableIDSDTVariant1.Type())   // 0x42 (actual)
    assert.Equal(t, PSITableTypeSDT, PSITableIDSDTVariant2.Type())   // 0x46 (other)
    assert.Equal(t, PSITableTypePAT, PSITableIDPAT.Type())           // 0x00
    assert.Equal(t, PSITableTypePMT, PSITableIDPMT.Type())           // 0x02
    assert.Equal(t, PSITableTypeTOT, PSITableIDTOT.Type())           // 0x73
    
    // 🔹 Невідомий тип
    assert.Equal(t, PSITableTypeUnknown, PSITableID(1).Type())       // 0x01 = CAT (не парситься)
}
```

**Матриця table_id у DVB:**
```
table_id | Тип   | Опис
─────────┼───────┼─────────────────────────────────
0x00     | PAT   | Program Association Table (обов'язкова)
0x01     | CAT   | Conditional Access Table (шифрування)
0x02     | PMT   | Program Map Table (обов'язкова для програми)
0x40-0x41| NIT   | Network Information Table (мережеві дані)
0x42/0x46| SDT   | Service Description Table (опис каналів)
0x4E-0x6F| EIT   | Event Information Table (розклад передач)
0x70     | TDT   | Time Date Table (поточний час)
0x73     | TOT   | Time Offset Table (час + таймзони)
0x7E     | DIT   | Discontinuity Information Table (розриви)
0x7F     | SIT   | Selection Information Table (вибір)
0x80-0xFE| -     | Приватні / зарезервовані
0xFF     | Null  | Заповнення
```

---

## ✏️ Тест серіалізації: `TestWritePSIData`

```go
func TestWritePSIData(t *testing.T) {
    for _, tc := range psiDataTestCases {  // PAT та PMT кейси
        t.Run(tc.name, func(t *testing.T) {
            // 🔹 1. Згенерувати "еталонні" байти вручну
            bufExpected := bytes.Buffer{}
            wExpected := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &bufExpected})
            tc.bytesFunc(wExpected)  // ручне кодування за специфікацією
            
            // 🔹 2. Згенерувати байти через writePSIData()
            bufActual := bytes.Buffer{}
            wActual := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &bufActual})
            n, err := writePSIData(wActual, tc.data)
            
            // 🔹 3. Порівняти бінарно
            assert.NoError(t, err)
            assert.Equal(t, bufExpected.Len(), n)
            assert.Equal(t, n, bufActual.Len())
            assert.Equal(t, bufExpected.Bytes(), bufActual.Bytes())  // ✅ ідентичність
        })
    }
}
```

**Чому round-trip тест важливий:**
```
• Ручне кодування (bytesFunc) = "джерело істини" за специфікацією
• writePSIData() = реалізація бібліотеки
• Бінарна ідентичність = гарантія сумісності з іншими декодерами

Якщо байти відрізняються:
→ Плеєри можуть відкинути таблицю як невалідну
→ Метадані не відобразяться у клієнта
→ Можливі помилки парсингу на стороні приймача
```

---

## ⚡ Бенчмарк: `BenchmarkParsePSIData`

```go
func BenchmarkParsePSIData(b *testing.B) {
    pb := psiBytes()  // підготувати тестові дані
    b.ReportAllocs()  // 📊 звітувати про алокації
    
    for i := 0; i < b.N; i++ {
        parsePSIData(astikit.NewBytesIterator(pb))
    }
}
```

**Очікувані результати:**
```
BenchmarkParsePSIData-8    50000    25000 ns/op    2000 B/op    50 allocs/op
```

**Що аналізувати:**
| Метрика | Ідеальне значення | Що означає відхилення |
|---------|-------------------|----------------------|
| `ns/op` | < 50 µs для 7 секцій | Повільний парсинг → оптимізувати бітові операції |
| `B/op`  | < 5 KB | Зайві алокації → використовувати bytesPool |
| `allocs/op` | < 100 | Кожна алокація = тиск на GC |

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

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на фрагментацію великої PSI секції

```go
func TestParsePSIData_FragmentedSection(t *testing.T) {
    // Створити PAT з багатьма програмами → велика секція
    pat := &astits.PATData{
        TransportStreamID: 1,
        Programs:          []*astits.PATProgram{},
    }
    for i := 0; i < 100; i++ {
        pat.Programs = append(pat.Programs, &astits.PATProgram{
            ProgramNumber: uint16(i + 1),
            ProgramMapID:  uint16(0x1000 + i),
        })
    }
    
    // Серіалізувати пат (спрощено)
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    writePATSection(w, pat)  // ваша helper-функція
    
    // Розбити на кілька "пакетів" для імітації фрагментації
    data := buf.Bytes()
    chunks := make([][]byte, 0)
    for i := 0; i < len(data); i += 100 {
        end := i + 100
        if end > len(data) {
            end = len(data)
        }
        chunks = append(chunks, data[i:end])
    }
    
    // З'єднати та парсити
    var combined []byte
    for _, chunk := range chunks {
        combined = append(combined, chunk...)
    }
    
    parsed, err := parsePSIData(astikit.NewBytesIterator(combined))
    assert.NoError(t, err)
    assert.NotNil(t, parsed.Sections[0].PAT)
    assert.Len(t, parsed.Sections[0].PAT.Programs, 100)
}
```

### 🔹 Тест на обробку невідомих table_id

```go
func TestParsePSIData_UnknownTableID(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Створити секцію з table_id=254 (приватний)
    w.Write(uint8(0))       // pointer_field
    w.Write(uint8(254))     // table_id = unknown
    w.Write("0")            // syntax_section_indicator = 0 (без синтаксису)
    w.Write("1")            // private_bit
    w.Write("11")           // reserved
    w.Write("000000000100") // section_length = 4
    w.Write([]byte("test")) // приватні дані
    // ❌ Без CRC32, бо syntax_section_indicator=0
    
    data, err := parsePSIData(astikit.NewBytesIterator(buf.Bytes()))
    assert.NoError(t, err)
    assert.Len(t, data.Sections, 1)
    assert.Equal(t, astits.PSITableTypeUnknown, data.Sections[0].Header.TableType)
    // Дані секції не парсяться, бо тип невідомий
}
```

### 🔹 Тест на валідацію version_number при оновленні таблиць

```go
func TestPSI_VersionNumberIncrement(t *testing.T) {
    // Створити дві версії PAT з різними version_number
    patV0 := createPATWithVersion(0)
    patV1 := createPATWithVersion(1)  // та сама структура, інша версія
    
    // Серіалізувати обидві
    bufV0 := serializePAT(patV0)
    bufV1 := serializePAT(patV1)
    
    // Парсити обидві
    dataV0, _ := parsePSIData(astikit.NewBytesIterator(bufV0))
    dataV1, _ := parsePSIData(astikit.NewBytesIterator(bufV1))
    
    // Перевірити, що version_number збережено
    assert.Equal(t, uint8(0), dataV0.Sections[0].Syntax.Header.VersionNumber)
    assert.Equal(t, uint8(1), dataV1.Sections[0].Syntax.Header.VersionNumber)
    
    // 🔹 Ключове: плеєри детектують оновлення за зміною version_number
    // при тому ж current_next_indicator=1
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

### Приклад коректного розрахунку CRC32:

```go
func computePSICRC(data []byte, offsetStart, offsetEnd int) uint32 {
    // CRC обчислюється тільки для даних секції (без заголовка та без самого CRC)
    // offsetStart = після заголовка (4 байти: table_id + flags + section_length)
    // offsetEnd = перед CRC32 (останні 4 байти секції)
    
    crc := crc32.NewIEEE()  // ⚠️ Але MPEG-2 використовує інший поліном!
    // Правильно:
    crc := astits.NewCRC32MPEG2()  // гіпотетична функція з поліномом 0x04c11db7
    
    crc.Write(data[offsetStart:offsetEnd])
    return crc.Sum32()
}
```

> ⚠️ **Важливо**: MPEG-2 CRC32 використовує поліном `0x04c11db7`, а не стандартний IEEE `0xEDB88320`. Переконайтеся, що ваша реалізація використовує правильний поліном.

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