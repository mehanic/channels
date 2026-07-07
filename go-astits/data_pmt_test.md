# Глибоке роз'яснення: Тести PMT (Program Map Table) у astits — парсинг та серіалізація

Цей файл містить **комплексні тести парсингу та запису секції PMT (Program Map Table)** — критичної таблиці MPEG-TS, що описує склад програми: PID відео/аудіо потоків, PCR PID, дескриптори кодеків та інші метадані.

---

## 🎯 Навіщо PMT потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ PMT у контексті HLS-стрімінгу:         │
│                                         │
│ 🔹 Ідентифікація потоків:               │
│   • ElementaryPID для відео/аудіо      │
│   • StreamType (H.264, AAC, HEVC...)   │
│   • Дескриптори кодеків (AVCVideo...)  │
│                                         │
│ 🔹 Синхронізація:                       │
│   • PCRPID — який потік містить PCR    │
│   • Критично для A/V синхронізації     │
│                                         │
│ 🔹 Метадані програми:                   │
│   • ProgramDescriptors — загальні      │
│   • ElementaryStreamDescriptors — per-stream│
│                                         │
│ 🔹 Для HLS:                             │
│   • Без валідного PMT плеєр не знайде  │
│     відео/аудіо потоки                 │
│   • Неправильні PID → чорний екран     │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `PMTData` та тестові дані

### Тип даних

```go
type PMTData struct {
    ElementaryStreams    []*PMTElementaryStream  // 🎯 список відео/аудіо потоків
    PCRPID               uint16                   // 🎯 PID потоку з PCR для синхронізації
    ProgramDescriptors   []*Descriptor            // 🎯 загальні дескриптори програми
    ProgramNumber        uint16                   // 🎯 номер програми (з PAT)
}

type PMTElementaryStream struct {
    ElementaryPID               uint16       // 🎯 PID цього потоку
    ElementaryStreamDescriptors []*Descriptor // 🎯 дескриптори потоку (кодек, мова...)
    StreamType                  uint8        // 🎯 тип кодека (0x1B=H.264, 0x0F=AAC...)
}
```

### Тестові дані: `pmt` та `pmtBytes()`

```go
// Глобальна змінна — еталонне значення для тесту
var pmt = &PMTData{
    ElementaryStreams: []*PMTElementaryStream{{
        ElementaryPID:               2730,  // 0x0AAA
        ElementaryStreamDescriptors: descriptors,  // посилання на тестові дескриптори
        StreamType:                  StreamTypeMPEG1Audio,  // 0x03
    }},
    PCRPID:             5461,  // 0x1555
    ProgramDescriptors: descriptors,
    ProgramNumber:      1,
}

// Генератор "еталонних" байтів для PMT секції (payload без заголовка)
func pmtBytes() []byte {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // 🔹 1. PCR PID (13 біт) + reserved (3 біти)
    w.Write("111")                          // reserved = 0b111
    w.Write("1010101010101")               // PCR_PID = 0x1555 = 5461 (13 біт)
    
    // 🔹 2. Program info length + descriptors
    w.Write("1111")                         // reserved = 0b1111 (верхні 4 біти program_info_length)
    descriptorsBytes(w)                     // Program descriptors (дескриптори програми)
    
    // 🔹 3. Elementary stream loop (один потік у тесті)
    w.Write(uint8(StreamTypeMPEG1Audio))   // stream_type = 0x03 (MPEG-1 Audio)
    w.Write("111")                          // reserved = 0b111 (верхні 3 біти elementary_PID)
    w.Write("0101010101010")               // ElementaryPID = 0x0AAA = 2730 (13 біт)
    w.Write("1111")                         // reserved = 0b1111 (верхні 4 біти ES_info_length)
    descriptorsBytes(w)                     // Elementary stream descriptors
    
    // Результат: []byte з серіалізованим payload PMT секції
    return buf.Bytes()
}
```

> 💡 **Важливо**: `pmtBytes()` генерує тільки **payload секції** (без PSI заголовка та CRC32). Функція `parsePMTSection` очікує саме payload + довжину, бо заголовок парситься на вищому рівні (`parsePSISection`).

---

## 🔍 Тест `TestParsePMTSection`

```go
func TestParsePMTSection(t *testing.T) {
    // 🔹 1. Отримати тестові байти
    b := pmtBytes()
    
    // 🔹 2. Парсити секцію: ітератор + довжина + program_number
    d, err := parsePMTSection(astikit.NewBytesIterator(b), len(b), uint16(1))
    
    // 🔹 3. Перевірити результат
    assert.Equal(t, d, pmt)        // ✅ структурна рівність з еталоном
    assert.NoError(t, err)         // ✅ без помилок
}
```

### Що робить `parsePMTSection` (гіпотетична реалізація)

```go
func parsePMTSection(i *astikit.BytesIterator, sectionLength int, programNumber uint16) (*PMTData, error) {
    d := &PMTData{ProgramNumber: programNumber}
    
    // 🔹 1. PCR PID (13 біт) + reserved (3 біти)
    b, _ := i.NextByte()
    reserved := b >> 5  // старші 3 біти
    pcrPIDHigh := b & 0x1F  // молодші 5 біт
    
    b, _ = i.NextByte()
    pcrPIDLow := b  // 8 біт
    d.PCRPID = uint16(pcrPIDHigh)<<8 | uint16(pcrPIDLow)
    
    // 🔹 2. Program info length (12 біт) + дескриптори
    bs, _ := i.NextBytesNoCopy(2)
    programInfoLength := uint16(bs[0]&0x0F)<<8 | uint16(bs[1])
    
    if programInfoLength > 0 {
        d.ProgramDescriptors, _ = parseDescriptors(i)
        // Пропустити залишок, якщо дескриптори коротші за programInfoLength
        i.Seek(i.Offset() + int(programInfoLength) - descriptorsBytesRead)
    }
    
    // 🔹 3. Цикл elementary streams до кінця секції
    offsetEnd := i.Offset() + sectionLength - 5  // мінус вже прочитані байти
    
    for i.Offset() < offsetEnd {
        es := &PMTElementaryStream{}
        
        // Stream type (1 байт)
        es.StreamType, _ = i.NextByte()
        
        // Elementary PID (13 біт) + reserved
        bs, _ = i.NextBytesNoCopy(2)
        es.ElementaryPID = uint16(bs[0]&0x1F)<<8 | uint16(bs[1])
        
        // ES info length (12 біт) + дескриптори
        esInfoLength := uint16(bs[1]&0x0F)<<8  // продовження з наступного байта...
        if esInfoLength > 0 {
            es.ElementaryStreamDescriptors, _ = parseDescriptors(i)
        }
        
        d.ElementaryStreams = append(d.ElementaryStreams, es)
    }
    
    return d, nil
}
```

> ⚠️ **Важливо**: Бітова структура PMT складна через змішані 13-бітні PID та 12-бітні довжини. Завжди звіряйтесь зі специфікацією ISO/IEC 13818-1.

---

## ✏️ Тест `TestWritePMTSection`: серіалізація

```go
func TestWritePMTSection(t *testing.T) {
    buf := bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &buf})
    
    // 🔹 Записати PMT через writePMTSection
    n, err := writePMTSection(w, pmt)
    
    // 🔹 Перевірити результат
    assert.NoError(t, err)
    assert.Equal(t, n, buf.Len())              // ✅ кількість записаних байт
    assert.Equal(t, pmtBytes(), buf.Bytes())   // ✅ бінарна ідентичність з еталоном
}
```

**Чому round-trip тест важливий:**
```
• Ручне кодування (pmtBytes) = "джерело істини" за специфікацією
• writePMTSection() = реалізація бібліотеки
• Бінарна ідентичність = гарантія сумісності з іншими декодерами

Якщо байти відрізняються:
→ Плеєри можуть відкинути PMT як невалідний
→ Відео/аудіо потоки не будуть знайдені
→ Чорний екран або помилка відтворення у клієнта
```

---

## ⚡ Бенчмарки: продуктивність парсингу та запису

### `BenchmarkParsePMTSection`

```go
func BenchmarkParsePMTSection(b *testing.B) {
    b.ReportAllocs()  // 📊 звітувати про алокації
    bs := pmtBytes()  // підготувати тестові дані
    
    for i := 0; i < b.N; i++ {
        parsePMTSection(astikit.NewBytesIterator(bs), len(bs), uint16(1))
    }
}
```

**Очікувані результати:**
```
BenchmarkParsePMTSection-8    100000    12000 ns/op    500 B/op    20 allocs/op
```

### `BenchmarkWritePMTSection`

```go
func BenchmarkWritePMTSection(b *testing.B) {
    b.ReportAllocs()
    
    bw := &bytes.Buffer{}
    bw.Grow(1024)  // 🔹 попереднє виділення пам'яті
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: bw})
    
    for i := 0; i < b.N; i++ {
        bw.Reset()  // 🔹 очищення без deallocation
        writePMTSection(w, pmt)
    }
}
```

**Що аналізувати:**
| Метрика | Ідеальне значення | Що означає відхилення |
|---------|-------------------|----------------------|
| `ns/op` | < 20 µs для 1 потоку | Повільний парсинг → оптимізувати бітові операції |
| `B/op`  | < 1 KB | Зайві алокації → використовувати bytesPool |
| `allocs/op` | < 30 | Кожна алокація = тиск на GC |

> 💡 **Порада**: `bw.Grow(1024)` + `bw.Reset()` у бенчмарку запису імітує реальне використання — буфер переиспользується без нових алокацій.

---

## 🧮 Формат PMT секції у деталях

```
PMT Section Payload (без PSI заголовка та CRC):
┌─────────────────────────────────┐
│ [3]  reserved = 0b111           │
│ [13] PCR_PID                    │ ← який потік містить PCR
├─────────────────────────────────┤
│ [4]  reserved = 0b1111          │
│ [12] program_info_length        │ ← довжина дескрипторів програми
│ [N]  program_descriptors...     │ ← цикл дескрипторів
├─────────────────────────────────┤
│ Elementary stream loop (повтор):│
│   [8]  stream_type              │ ← тип кодека (0x1B=H.264...)
│   [3]  reserved = 0b111         │
│   [13] elementary_PID           │ ← PID цього потоку
│   [4]  reserved = 0b1111        │
│   [12] ES_info_length           │ ← довжина дескрипторів потоку
│   [N]  elementary_descriptors...│ ← цикл дескрипторів
└─────────────────────────────────┘

Повна PSI секція (додається на вищому рівні):
[8]  table_id = 0x02 (PMT)
[12] section_length
[16] program_number
[16] reserved + version + current_next
[8]  section_number = 0
[8]  last_section_number = 0
[... PMT payload ...]
[32] CRC32
```

### Приклад розбору бітів для PCR PID

```
Вхідні байти: 0b11110101 0b01010101
              ↑         ↑
              │         └─ Байт 2: молодші 8 біт PID
              └─ Байт 1: [7-5]reserved=0b111, [4-0]старші 5 біт PID

Розрахунок:
• Байт 1: 0b11110101 = 0xF5
  - reserved = 0b111 (біти 7-5) = 7 ✅
  - pcrPIDHigh = 0b10101 (біти 4-0) = 21

• Байт 2: 0b01010101 = 0x55 = 85

• PCR_PID = (21 << 8) | 85 = 5376 + 85 = 5461 = 0x1555 ✅
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Витягування відео/аудіо PID з PMT

```go
// У VideoManifestProxy — отримання PID для обробки:
func extractStreamPIDs(pmt *astits.PMTData) (videoPID, audioPID, pcrPID uint16, err error) {
    pcrPID = pmt.PCRPID  // 🔹 PCR завжди з одного потоку
    
    // 🔹 Шукаємо відео та аудіо за StreamType
    for _, es := range pmt.ElementaryStreams {
        switch es.StreamType {
        case astits.StreamTypeH264Video, astits.StreamTypeHEVCVideo, astits.StreamTypeMPEG2Video:
            if videoPID == 0 {  // взяти перший знайдений відео
                videoPID = es.ElementaryPID
            }
        case astits.StreamTypeADTS, astits.StreamTypeAACAudio, astits.StreamTypeMPEG1Audio, astits.StreamTypeAC3:
            if audioPID == 0 {  // взяти перший знайдений аудіо
                audioPID = es.ElementaryPID
            }
        }
    }
    
    if videoPID == 0 {
        return 0, 0, 0, fmt.Errorf("no video stream found in PMT")
    }
    // Аудіо може бути опціональним
    
    return videoPID, audioPID, pcrPID, nil
}
```

### ✅ 2: Фільтрація потоків за дескрипторами

```go
// У channel-aware архітектурі — обробляти тільки потрібні кодеки:
func isStreamSupported(es *astits.PMTElementaryStream) bool {
    // 🔹 Підтримувані StreamType
    supportedTypes := map[uint8]bool{
        astits.StreamTypeH264Video: true,
        astits.StreamTypeHEVCVideo: true,
        astits.StreamTypeADTS:      true,  // AAC
        astits.StreamTypeAACAudio:  true,
    }
    
    if !supportedTypes[es.StreamType] {
        return false
    }
    
    // 🔹 Додаткова фільтрація за дескрипторами (опціонально)
    for _, desc := range es.ElementaryStreamDescriptors {
        if desc.Tag == astits.DescriptorTagAVCVideo {
            // Перевірити profile/level для сумісності
            if desc.AVCVideo != nil && desc.AVCVideo.LevelIDC > 51 {
                return false  // занадто високий level для цільових пристроїв
            }
        }
    }
    
    return true
}

// Використання:
for _, es := range pmt.ElementaryStreams {
    if !isStreamSupported(es) {
        log.Debugf("Skipping unsupported stream: PID=%d, type=0x%02X", 
            es.ElementaryPID, es.StreamType)
        continue
    }
    // Обробити підтримуваний потік...
}
```

### ✅ 3: Моніторинг цілісності PMT

```go
// monitoring.Monitor — метрики для PMT:
type PMTMetrics struct {
    PMTParsed         *prometheus.CounterVec  // кількість парсингів PMT
    StreamsDiscovered *prometheus.CounterVec  // кількість знайдених потоків
    PCRPIDErrors      *prometheus.CounterVec  // помилки: PCR PID не у списку потоків
    UnsupportedStreams *prometheus.CounterVec // відкинуті непідтримувані потоки
}

// У обробці PMT:
func monitorPMT(pmt *astits.PMTData, channelID string, metrics *PMTMetrics) {
    metrics.PMTParsed.WithLabelValues(channelID).Inc()
    metrics.StreamsDiscovered.WithLabelValues(channelID).Add(float64(len(pmt.ElementaryStreams)))
    
    // 🔹 Перевірити, що PCR PID існує серед потоків
    pcrFound := false
    for _, es := range pmt.ElementaryStreams {
        if es.ElementaryPID == pmt.PCRPID {
            pcrFound = true
            break
        }
    }
    if !pcrFound {
        metrics.PCRPIDErrors.WithLabelValues(channelID).Inc()
        log.Warnf("Channel %s: PCR PID %d not found in elementary streams", 
            channelID, pmt.PCRPID)
    }
    
    // 🔹 Підрахувати непідтримувані потоки
    for _, es := range pmt.ElementaryStreams {
        if !isStreamSupported(es) {
            metrics.UnsupportedStreams.WithLabelValues(channelID).Inc()
        }
    }
}
```

### ✅ 4: Динамічне оновлення при зміні програми

```go
// При зміні вмісту програми (напр., додано новий аудіо-потік):
func handlePMTPMTUpdate(oldPMT, newPMT *astits.PMTData, channelID string) {
    // 🔹 Порівняти версії (якщо доступно)
    if oldPMT == nil {
        log.Infof("Channel %s: new PMT detected with %d streams", 
            channelID, len(newPMT.ElementaryStreams))
        return
    }
    
    // 🔹 Знайти додані потоки
    oldPIDs := make(map[uint16]bool)
    for _, es := range oldPMT.ElementaryStreams {
        oldPIDs[es.ElementaryPID] = true
    }
    
    for _, es := range newPMT.ElementaryStreams {
        if !oldPIDs[es.ElementaryPID] {
            log.Infof("Channel %s: new stream added: PID=%d, type=0x%02X", 
                channelID, es.ElementaryPID, es.StreamType)
            // Ініціалізувати обробку для нового потоку...
        }
    }
    
    // 🔹 Знайти видалені потоки
    newPIDs := make(map[uint16]bool)
    for _, es := range newPMT.ElementaryStreams {
        newPIDs[es.ElementaryPID] = true
    }
    
    for pid := range oldPIDs {
        if !newPIDs[pid] {
            log.Infof("Channel %s: stream removed: PID=%d", channelID, pid)
            // Зупинити обробку для видаленого потоку...
        }
    }
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на кілька елементарних потоків

```go
func TestParsePMTSection_MultipleStreams(t *testing.T) {
    // Створити PMT з відео + аудіо + субтитри
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // PCR PID
    w.Write("111")                          // reserved
    w.Write("0000001000000")               // PCR_PID = 0x100 = 256
    
    // Program descriptors (порожні)
    w.Write("1111")                         // reserved + program_info_length[11:8] = 0
    w.Write(uint8(0))                       // program_info_length[7:0] = 0
    
    // Stream 1: H.264 video
    w.Write(uint8(astits.StreamTypeH264Video))  // 0x1B
    w.Write("111")                          // reserved
    w.Write("0000001000001")               // PID = 0x101 = 257
    w.Write("111100000000")                // ES_info_length = 0
    // (без дескрипторів)
    
    // Stream 2: AAC audio
    w.Write(uint8(astits.StreamTypeADTS))  // 0x0F
    w.Write("111")
    w.Write("0000001000010")               // PID = 0x102 = 258
    w.Write("111100000000")
    
    // Stream 3: DVB subtitles
    w.Write(uint8(astits.StreamTypeDVBSubtitles))  // 0x06
    w.Write("111")
    w.Write("0000001000011")               // PID = 0x103 = 259
    w.Write("111100000010")                // ES_info_length = 2
    // Дескриптор субтитрів (спрощено)
    w.Write(uint8(astits.DescriptorTagSubtitling))
    w.Write(uint8(0))  // length = 0 для тесту
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    pmt, err := parsePMTSection(iter, buf.Len(), uint16(1))
    
    assert.NoError(t, err)
    assert.Equal(t, uint16(256), pmt.PCRPID)
    assert.Len(t, pmt.ElementaryStreams, 3)
    
    // Перевірити відео
    assert.Equal(t, uint16(257), pmt.ElementaryStreams[0].ElementaryPID)
    assert.Equal(t, astits.StreamTypeH264Video, pmt.ElementaryStreams[0].StreamType)
    
    // Перевірити аудіо
    assert.Equal(t, uint16(258), pmt.ElementaryStreams[1].ElementaryPID)
    assert.Equal(t, astits.StreamTypeADTS, pmt.ElementaryStreams[1].StreamType)
}
```

### 🔹 Тест на валідацію PCR PID

```go
func TestPMT_PCRPIDValidation(t *testing.T) {
    // Створити PMT, де PCR PID не збігається з жодним elementary_PID
    pmtInvalid := &astits.PMTData{
        PCRPID: 999,  // ❌ не існує серед потоків
        ElementaryStreams: []*astits.PMTElementaryStream{
            {ElementaryPID: 100, StreamType: astits.StreamTypeH264Video},
            {ElementaryPID: 101, StreamType: astits.StreamTypeADTS},
        },
        ProgramNumber: 1,
    }
    
    // Серіалізувати та парсити (парсинг пройде, але валідація має виявити помилку)
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    writePMTSection(w, pmtInvalid)
    
    parsed, err := parsePMTSection(astikit.NewBytesIterator(buf.Bytes()), buf.Len(), 1)
    assert.NoError(t, err)  // парсинг успішний
    
    // 🔹 Валідація на рівні додатку:
    pcrFound := false
    for _, es := range parsed.ElementaryStreams {
        if es.ElementaryPID == parsed.PCRPID {
            pcrFound = true
            break
        }
    }
    assert.False(t, pcrFound)  // ✅ помилка виявлена
    
    // Логування для відладки:
    if !pcrFound {
        log.Warnf("Invalid PMT: PCR PID %d not found in elementary streams", parsed.PCRPID)
    }
}
```

### 🔹 Тест на round-trip з дескрипторами

```go
func TestPMTSection_RoundTrip_WithDescriptors(t *testing.T) {
    original := &astits.PMTData{
        ProgramNumber: 1,
        PCRPID:        256,
        ProgramDescriptors: []*astits.Descriptor{
            {
                Tag: astits.DescriptorTagService,
                Service: &astits.DescriptorService{
                    Name:     []byte("Test Channel"),
                    Provider: []byte("Test Provider"),
                    Type:     astits.ServiceTypeDigitalTelevisionService,
                },
            },
        },
        ElementaryStreams: []*astits.PMTElementaryStream{
            {
                ElementaryPID: 257,
                StreamType:    astits.StreamTypeH264Video,
                ElementaryStreamDescriptors: []*astits.Descriptor{
                    {
                        Tag: astits.DescriptorTagAVCVideo,
                        AVCVideo: &astits.DescriptorAVCVideo{
                            ProfileIDC: 100,  // High profile
                            LevelIDC:   41,   // Level 4.1
                        },
                    },
                },
            },
        },
    }
    
    // Серіалізувати
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    writePMTSection(w, original)
    
    // Парсити назад
    parsed, err := parsePMTSection(astikit.NewBytesIterator(buf.Bytes()), buf.Len(), original.ProgramNumber)
    assert.NoError(t, err)
    
    // Порівняти ключові поля
    assert.Equal(t, original.PCRPID, parsed.PCRPID)
    assert.Len(t, parsed.ElementaryStreams, 1)
    assert.Equal(t, original.ElementaryStreams[0].ElementaryPID, 
                 parsed.ElementaryStreams[0].ElementaryPID)
    assert.NotNil(t, parsed.ElementaryStreams[0].ElementaryStreamDescriptors[0].AVCVideo)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання 13-бітних PID | PID зміщено на 1-2 біти | Перевірити бітові маски: `(b0&0x1F)<<8 \| b1` для PID, де b0 містить 5 старших біт |
| program_info_length не враховано | Дескриптори програми пропускаються або читаються зайві байти | Перевірити розрахунок: `uint16(bs[0]&0x0F)<<8 \| uint16(bs[1])` (12 біт) |
| PCR PID не знайдено серед потоків | Помилка синхронізації у плеєрі | Додати валідацію після парсингу: перевірити, що `pmt.PCRPID` існує в `ElementaryStreams` |
| Дескриптори не парсяться | `ElementaryStreamDescriptors` порожній | Перевірити, що `parseDescriptors` викликається з правильним `offsetEnd`; перевірити `ES_info_length` |
| Непідтримувані StreamType | Потік ігнорується, чорний екран | Додати логування невідомих типів: `log.Warnf("Unknown stream type 0x%02X", es.StreamType)` |

### Приклад коректного читання 13-бітного PID:

```go
func read13BitPID(i *astikit.BytesIterator) (uint16, error) {
    bs, err := i.NextBytesNoCopy(2)
    if err != nil { return 0, err }
    
    // Формат: [3 reserved][13 PID] у перших двох байтах
    // Байт 0: [7-5]reserved, [4-0]PID[12:8]
    // Байт 1: [7-0]PID[7:0]
    
    pidHigh := uint16(bs[0] & 0x1F)  // молодші 5 біт байта 0
    pidLow := uint16(bs[1])          // весь байт 1
    
    return (pidHigh << 8) | pidLow, nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Витягування основних параметрів з PMT:
func extractPMTInfo(pmt *astits.PMTData) PMTInfo {
    info := PMTInfo{
        ProgramNumber: pmt.ProgramNumber,
        PCRPID:        pmt.PCRPID,
        Streams:       make([]StreamInfo, 0, len(pmt.ElementaryStreams)),
    }
    
    for _, es := range pmt.ElementaryStreams {
        info.Streams = append(info.Streams, StreamInfo{
            PID:         es.ElementaryPID,
            StreamType:  es.StreamType,
            HasDescriptors: len(es.ElementaryStreamDescriptors) > 0,
        })
    }
    
    return info
}

// 2: Фільтрація за типом кодека:
func filterStreamsByType(pmt *astits.PMTData, types ...uint8) []*astits.PMTElementaryStream {
    var filtered []*astits.PMTElementaryStream
    typeSet := make(map[uint8]bool)
    for _, t := range types {
        typeSet[t] = true
    }
    
    for _, es := range pmt.ElementaryStreams {
        if typeSet[es.StreamType] {
            filtered = append(filtered, es)
        }
    }
    return filtered
}

// Використання:
videoStreams := filterStreamsByType(pmt, 
    astits.StreamTypeH264Video, 
    astits.StreamTypeHEVCVideo,
)

// 3: Валідація PMT перед використанням:
func validatePMT(pmt *astits.PMTData) error {
    // 🔹 PCR PID має існувати
    pcrFound := false
    for _, es := range pmt.ElementaryStreams {
        if es.ElementaryPID == pmt.PCRPID {
            pcrFound = true
            break
        }
    }
    if !pcrFound {
        return fmt.Errorf("PCR PID %d not found in elementary streams", pmt.PCRPID)
    }
    
    // 🔹 Має бути хоча б один відео-потік
    hasVideo := false
    for _, es := range pmt.ElementaryStreams {
        if isVideoStreamType(es.StreamType) {
            hasVideo = true
            break
        }
    }
    if !hasVideo {
        return fmt.Errorf("no video stream found in PMT")
    }
    
    return nil
}

func isVideoStreamType(streamType uint8) bool {
    switch streamType {
    case astits.StreamTypeMPEG1Video, astits.StreamTypeMPEG2Video,
         astits.StreamTypeH264Video, astits.StreamTypeHEVCVideo:
        return true
    default:
        return false
    }
}

// 4: Моніторинг:
func monitorPMTHealth(pmt *astits.PMTData, channelID string, metrics *PMTMetrics) {
    metrics.StreamCount.WithLabelValues(channelID).Set(float64(len(pmt.ElementaryStreams)))
    
    videoCount := 0
    audioCount := 0
    for _, es := range pmt.ElementaryStreams {
        if isVideoStreamType(es.StreamType) {
            videoCount++
        } else if isAudioStreamType(es.StreamType) {
            audioCount++
        }
    }
    metrics.VideoStreamCount.WithLabelValues(channelID).Set(float64(videoCount))
    metrics.AudioStreamCount.WithLabelValues(channelID).Set(float64(audioCount))
}
```

---

## 📊 Матриця StreamType для вашого пайплайну

```
StreamType (hex) | Кодек              | Використання у CCTV HLS
─────────────────┼────────────────────┼─────────────────────────
0x02             | MPEG-1 Video       | ⚠️ Застарілий, рідко
0x03             | MPEG-1 Audio       | ⚠️ Застарілий
0x04             | MPEG-2 Video       | ⚠️ Застарілий
0x0F             | AAC (ADTS)         | ✅ Популярний для аудіо
0x10             | MPEG-2 Audio       | ⚠️ Рідко
0x11             | AAC (LATM)         | ✅ Альтернатива ADTS
0x1B             | H.264/AVC          | ✅ Найпопулярніший відео
0x24             | HEVC/H.265         | ✅ Для 4K/економії бітрейту
0x06             | DVB Subtitles      | ⚠️ Якщо потрібні субтитри
0x81             | AC-3               | ⚠️ Для 5.1 аудіо
0x82             | DTS                | ⚠️ Рідко у стрімінгу
```

---

## 📚 Корисні посилання

- [ISO/IEC 13818-1: MPEG-2 Systems](https://www.iso.org/standard/61236.html)
- [Stream Type registry](https://www.atsc.org/standards/a_53-6-2018/)
- [astits PMT parsing source](https://github.com/asticode/go-astits/blob/master/data.go)

> 💡 **Ключова ідея**: PMT — це "карта потоків" вашої програми. У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Автоматично ідентифікувати відео/аудіо потоки для обробки
> - 🔍 Валідувати цілісність програми (PCR PID, наявність відео)
> - 🧩 Фільтрувати непідтримувані кодеки на ранньому етапі
> - 📊 Збирати метрики про склад потоків для моніторингу

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати PMT-валідацію у ваш `segmentAssembler` для раннього виявлення помилок
- 🧪 Написати integration-тест для перевірки сумісності з реальними енкодерами
- 📈 Додати Prometheus-метрики для моніторингу складу потоків по каналах

🛠️