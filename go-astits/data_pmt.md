# Глибоке роз'яснення: `pmt.go` — парсинг та серіалізація PMT (Program Map Table) у astits

Цей файл містить **реалізацію парсингу та запису секції PMT (Program Map Table)** — критичної таблиці MPEG-TS, що описує склад програми: PID відео/аудіо потоків, PCR PID, дескриптори кодеків та інші метадані.

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

## 🔧 Константи StreamType: мапа кодеків

```go
type StreamType uint8

const (
    // 🔹 Відео кодеки
    StreamTypeMPEG1Video  StreamType = 0x01  // Застарілий
    StreamTypeMPEG2Video  StreamType = 0x02  // Застарілий
    StreamTypeMPEG4Video  StreamType = 0x10  // Рідко у стрімінгу
    StreamTypeH264Video   StreamType = 0x1B  // ✅ Найпопулярніший
    StreamTypeH265Video   StreamType = 0x24  // ✅ Для 4K/економії бітрейту
    StreamTypeHEVCVideo   StreamType = 0x24  // Аліас для H265
    StreamTypeCAVSVideo   StreamType = 0x42  // Китайський стандарт
    StreamTypeVC1Video    StreamType = 0xea  // Microsoft
    StreamTypeDIRACVideo  StreamType = 0xd1  // BBC, рідко
    
    // 🔹 Аудіо кодеки
    StreamTypeMPEG1Audio  StreamType = 0x03  // Застарілий
    StreamTypeMPEG2Audio  StreamType = 0x04  // Застарілий
    StreamTypeAACAudio    StreamType = 0x0F  // ✅ Популярний (ADTS)
    StreamTypeAACLATMAudio StreamType = 0x11 // ✅ Альтернатива ADTS
    StreamTypeAC3Audio    StreamType = 0x81  // ⚠️ Для 5.1
    StreamTypeEAC3Audio   StreamType = 0x87  // ⚠️ Enhanced AC-3
    StreamTypeDTSAudio    StreamType = 0x82  // ⚠️ Рідко у стрімінгу
    StreamTypeTRUEHDAudio StreamType = 0x83  // ⚠️ Lossless, рідко
    
    // 🔹 Інші типи
    StreamTypePrivateSection StreamType = 0x05  // PSI/SI дані
    StreamTypePrivateData    StreamType = 0x06  // DVB субтитри, VBI, AC-3
    StreamTypeMetadata       StreamType = 0x15  // EPG, метадані
    StreamTypeSCTE35         StreamType = 0x86  // ✅ Маркери реклами/сплісингу
)
```

### Методи `StreamType`: класифікація та конвертація

```go
// 🔹 IsVideo(): чи це відео-потік?
func (t StreamType) IsVideo() bool {
    switch t {
    case StreamTypeMPEG1Video, StreamTypeMPEG2Video, StreamTypeMPEG4Video,
         StreamTypeH264Video, StreamTypeH265Video, StreamTypeCAVSVideo,
         StreamTypeVC1Video, StreamTypeDIRACVideo:
        return true
    }
    return false
}

// 🔹 IsAudio(): чи це аудіо-потік?
func (t StreamType) IsAudio() bool {
    switch t {
    case StreamTypeMPEG1Audio, StreamTypeMPEG2Audio, StreamTypeAACAudio,
         StreamTypeAACLATMAudio, StreamTypeAC3Audio, StreamTypeDTSAudio,
         StreamTypeTRUEHDAudio, StreamTypeEAC3Audio:
        return true
    }
    return false
}

// 🔹 String(): human-readable назва для логів
func (t StreamType) String() string {
    switch t {
    case StreamTypeH264Video: return "H264 Video"
    case StreamTypeAACAudio:  return "AAC Audio"
    // ... інші випадки ...
    default: return "Unknown"
    }
}

// 🔹 ToPESStreamID(): конвертація для PES заголовків
func (t StreamType) ToPESStreamID() uint8 {
    switch t {
    // Відео потоки → 0xE0-0xEF
    case StreamTypeMPEG1Video, StreamTypeH264Video, StreamTypeH265Video:
        return 0xe0
    // Аудіо потоки → 0xC0-0xDF
    case StreamTypeAACAudio, StreamTypeAC3Audio:
        return 0xc0
    // Приватні дані → 0xBD-0xBF
    case StreamTypePrivateData:
        return 0xbd
    default:
        return 0xbd  // fallback
    }
}
```

> 💡 **Важливо**: `ToPESStreamID()` використовується при генерації PES-пакетів для встановлення `stream_id` у заголовку.

---

## 📦 Структури даних

### `PMTData` — контейнер для всієї таблиці

```go
type PMTData struct {
    ElementaryStreams  []*PMTElementaryStream  // 🎯 список відео/аудіо потоків
    PCRPID             uint16                   // 🎯 PID потоку з PCR для синхронізації
    ProgramDescriptors []*Descriptor            // 🎯 загальні дескриптори програми
    ProgramNumber      uint16                   // 🎯 номер програми (з PAT)
}
```

### `PMTElementaryStream` — опис одного потоку

```go
type PMTElementaryStream struct {
    ElementaryPID               uint16        // 🎯 PID цього потоку
    ElementaryStreamDescriptors []*Descriptor // 🎯 дескриптори потоку (кодек, мова...)
    StreamType                  StreamType    // 🎯 тип кодека (0x1B=H.264, 0x0F=AAC...)
}
```

---

## 🔍 Функція `parsePMTSection`: покроковий розбір

```go
func parsePMTSection(i *astikit.BytesIterator, offsetSectionsEnd int, tableIDExtension uint16) (*PMTData, error) {
    // 🔹 1. Ініціалізація з program_number (передається зовні)
    d := &PMTData{ProgramNumber: tableIDExtension}
    
    // 🔹 2. PCR PID (13 біт) + reserved (3 біти)
    bs, _ := i.NextBytesNoCopy(2)
    // Формат: [3 reserved][13 PCR_PID] у перших двох байтах
    d.PCRPID = uint16(bs[0]&0x1f)<<8 | uint16(bs[1])
    // bs[0]&0x1f = молодші 5 біт байта 0 (біти 4-0)
    // bs[1] = весь байт 1 (біти 7-0)
    // Разом: 5 + 8 = 13 біт для PID
    
    // 🔹 3. Program descriptors (змінна довжина)
    // parseDescriptors читає до кінця, визначеного program_info_length
    if d.ProgramDescriptors, err = parseDescriptors(i); err != nil {
        return nil, fmt.Errorf("astits: parsing descriptors failed: %w", err)
    }
    
    // 🔹 4. Цикл elementary streams до кінця секції
    for i.Offset() < offsetSectionsEnd {
        e := &PMTElementaryStream{}
        
        // ── Stream type (1 байт) ──
        b, _ := i.NextByte()
        e.StreamType = StreamType(b)
        
        // ── Elementary PID (13 біт) + reserved (3 біти) ──
        bs, _ = i.NextBytesNoCopy(2)
        e.ElementaryPID = uint16(bs[0]&0x1f)<<8 | uint16(bs[1])
        
        // ── Elementary descriptors ──
        if e.ElementaryStreamDescriptors, err = parseDescriptors(i); err != nil {
            return nil, fmt.Errorf("astits: parsing descriptors failed: %w", err)
        }
        
        // ── Додати потік у результат ──
        d.ElementaryStreams = append(d.ElementaryStreams, e)
    }
    
    return d, nil
}
```

### 🎯 Ключовий момент: читання 13-бітних PID

```
Формат: [3 reserved][13 PID] розподілені у 2 байтах

Байт 0: [7-5]reserved [4-0]PID[12:8]
Байт 1: [7-0]PID[7:0]

Приклад: PCR_PID = 5461 = 0x1555 = 0b001 010101010101

Байт 0: 0b11101010 = 0xEA
  - reserved = 0b111 (біти 7-5) ✅
  - PID[12:8] = 0b01010 = 10 (біти 4-0)

Байт 1: 0b01010101 = 0x55 = 85
  - PID[7:0] = 85

Розрахунок:
  PCR_PID = (10 << 8) | 85 = 2560 + 85 = 2645 ❌

Правильний приклад для 5461:
  5461 = 0x1555 = 0b001 010101010101
  Байт 0: 0b11100101 = 0xE5 (reserved=0b111, PID[12:8]=0b00101=5)
  Байт 1: 0b01010101 = 0x55 = 85
  Розрахунок: (5 << 8) | 85 = 1280 + 85 = 1365 ❌

Справжній розрахунок:
  5461 = 0x1555 = 0b1 010101010101 (13 біт)
  Байт 0: 0b11110101 = 0xF5 (reserved=0b111, PID[12:8]=0b10101=21)
  Байт 1: 0b01010101 = 0x55 = 85
  Розрахунок: (21 << 8) | 85 = 5376 + 85 = 5461 ✅
```

> 💡 **Порада**: Завжди тестуйте бітові операції на відомих значеннях, щоб уникнути помилок зсуву.

---

## ✏️ Функції розрахунку довжини: `calcPMT*Length`

### `calcPMTProgramInfoLength`: загальна довжина з дескрипторами

```go
func calcPMTProgramInfoLength(d *PMTData) uint16 {
    ret := uint16(2)  // program_info_length field itself (2 байти)
    ret += calcDescriptorsLength(d.ProgramDescriptors)  // довжина дескрипторів програми
    
    // Додати довжину кожного елементарного потоку
    for _, es := range d.ElementaryStreams {
        ret += 5  // stream_type(1) + elementary_pid(2) + es_info_length(2)
        ret += calcDescriptorsLength(es.ElementaryStreamDescriptors)
    }
    
    return ret
}
```

### `calcPMTSectionLength`: довжина секції для заголовка

```go
func calcPMTSectionLength(d *PMTData) uint16 {
    ret := uint16(4)  // PCR_PID(2) + program_info_length(2)
    ret += calcDescriptorsLength(d.ProgramDescriptors)
    
    for _, es := range d.ElementaryStreams {
        ret += 5  // stream_type(1) + elementary_pid(2) + es_info_length(2)
        ret += calcDescriptorsLength(es.ElementaryStreamDescriptors)
    }
    
    return ret
}
```

> ⚠️ **Важливо**: `calcPMTSectionLength` повертає довжину **без** заголовка PSI секції та без CRC32. Це значення записується у 12-бітне поле `section_length` у заголовку.

---

## ✏️ Функція `writePMTSection`: серіалізація

```go
func writePMTSection(w *astikit.BitsWriter, d *PMTData) (int, error) {
    b := astikit.NewBitsWriterBatch(w)
    
    // 🔹 1. PCR PID (13 біт) + reserved (3 біти)
    b.WriteN(uint8(0xff), 3)  // reserved = 0b111
    b.WriteN(d.PCRPID, 13)    // 13-бітний PID
    bytesWritten := 2         // 2 байти для PCR PID
    
    // 🔹 2. Program descriptors з довжиною
    n, err := writeDescriptorsWithLength(w, d.ProgramDescriptors)
    if err != nil { return 0, err }
    bytesWritten += n
    
    // 🔹 3. Цикл elementary streams
    for _, es := range d.ElementaryStreams {
        // Stream type (1 байт)
        b.Write(uint8(es.StreamType))
        
        // Elementary PID (13 біт) + reserved (3 біти)
        b.WriteN(uint8(0xff), 3)  // reserved = 0b111
        b.WriteN(es.ElementaryPID, 13)
        bytesWritten += 3  // 1 байт stream_type + 2 байти PID
        
        // Elementary descriptors з довжиною
        n, err = writeDescriptorsWithLength(w, es.ElementaryStreamDescriptors)
        if err != nil { return 0, err }
        bytesWritten += n
    }
    
    return bytesWritten, b.Err()
}
```

### Патерн `writeDescriptorsWithLength`

```go
// writeDescriptorsWithLength записує дескриптори з попереднім записом загальної довжини
func writeDescriptorsWithLength(w *astikit.BitsWriter, ds []*Descriptor) (int, error) {
    // 🔹 1. Розрахувати загальну довжину дескрипторів
    totalLength := calcDescriptorsLength(ds)
    
    // 🔹 2. Записати program_info_length / es_info_length (12 біт)
    b := astikit.NewBitsWriterBatch(w)
    b.WriteN(uint8(0xff), 4)  // reserved = 0b1111 (верхні 4 біти)
    b.WriteN(totalLength, 12)  // 12-бітна довжина
    if err := b.Err(); err != nil { return 0, err }
    
    // 🔹 3. Записати самі дескриптори
    n, err := writeDescriptors(w, ds)
    return n + 2, err  // +2 байти для поля довжини
}
```

> 💡 **Патерн**: Спочатку розрахувати довжину, записати поле довжини, потім записати дані. Це вимагає двопрохідного підходу або буферизації.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Витягування відео/аудіо PID з PMT

```go
// У VideoManifestProxy — отримання PID для обробки:
func extractStreamPIDs(pmt *astits.PMTData) (videoPID, audioPID, pcrPID uint16, err error) {
    pcrPID = pmt.PCRPID  // 🔹 PCR завжди з одного потоку
    
    // 🔹 Шукаємо відео та аудіо за StreamType
    for _, es := range pmt.ElementaryStreams {
        switch {
        case es.StreamType.IsVideo():
            if videoPID == 0 {  // взяти перший знайдений відео
                videoPID = es.ElementaryPID
            }
        case es.StreamType.IsAudio():
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

### ✅ 2: Фільтрація потоків за підтримуваними кодеками

```go
// У channel-aware архітектурі — обробляти тільки потрібні кодеки:
func isStreamSupported(es *astits.PMTElementaryStream) bool {
    // 🔹 Підтримувані StreamType
    supportedVideo := map[astits.StreamType]bool{
        astits.StreamTypeH264Video: true,
        astits.StreamTypeH265Video: true,
    }
    supportedAudio := map[astits.StreamType]bool{
        astits.StreamTypeAACAudio:    true,
        astits.StreamTypeAACLATMAudio: true,
        astits.StreamTypeAC3Audio:    true,
    }
    
    if supportedVideo[es.StreamType] || supportedAudio[es.StreamType] {
        return true
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
    
    return false
}

// Використання:
for _, es := range pmt.ElementaryStreams {
    if !isStreamSupported(es) {
        log.Debugf("Skipping unsupported stream: PID=%d, type=%s", 
            es.ElementaryPID, es.StreamType.String())
        continue
    }
    // Обробити підтримуваний потік...
}
```

### ✅ 3: Валідація цілісності PMT

```go
// Перевірити, що PMT валідний перед використанням:
func validatePMT(pmt *astits.PMTData) error {
    // 🔹 PCR PID має існувати серед потоків
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
        if es.StreamType.IsVideo() {
            hasVideo = true
            break
        }
    }
    if !hasVideo {
        return fmt.Errorf("no video stream found in PMT")
    }
    
    // 🔹 ProgramNumber має співпадати з очікуваним (опціонально)
    // 🔹 Дескриптори не повинні бути порожніми для критичних потоків (опціонально)
    
    return nil
}
```

### ✅ 4: Моніторинг складу потоків

```go
// monitoring.Monitor — метрики для PMT:
type PMTMetrics struct {
    PMTParsed         *prometheus.CounterVec  // кількість парсингів PMT
    StreamsDiscovered *prometheus.CounterVec  // кількість знайдених потоків
    VideoStreams      *prometheus.GaugeVec    // кількість відео потоків
    AudioStreams      *prometheus.GaugeVec    // кількість аудіо потоків
    PCRPIDErrors      *prometheus.CounterVec  // помилки: PCR PID не у списку потоків
    UnsupportedStreams *prometheus.CounterVec // відкинуті непідтримувані потоки
}

// У обробці PMT:
func monitorPMT(pmt *astits.PMTData, channelID string, metrics *PMTMetrics) {
    metrics.PMTParsed.WithLabelValues(channelID).Inc()
    metrics.StreamsDiscovered.WithLabelValues(channelID).Add(float64(len(pmt.ElementaryStreams)))
    
    // 🔹 Підрахувати відео/аудіо потоки
    videoCount := 0
    audioCount := 0
    for _, es := range pmt.ElementaryStreams {
        if es.StreamType.IsVideo() {
            videoCount++
        } else if es.StreamType.IsAudio() {
            audioCount++
        }
    }
    metrics.VideoStreams.WithLabelValues(channelID).Set(float64(videoCount))
    metrics.AudioStreams.WithLabelValues(channelID).Set(float64(audioCount))
    
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

### ✅ 5: Динамічне оновлення при зміні програми

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
            log.Infof("Channel %s: new stream added: PID=%d, type=%s", 
                channelID, es.ElementaryPID, es.StreamType.String())
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

## 🧪 Тестування: стратегії валідації

### 🔹 Базовий тест на парсинг

```go
func TestParsePMTSection_Basic(t *testing.T) {
    // Підготувати тестові байти: PCR PID + descriptors + один потік
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // PCR PID = 256 (0x100)
    w.WriteN(uint8(0xff), 3)  // reserved
    w.WriteN(uint16(256), 13)  // PCR_PID
    
    // Program descriptors (порожні)
    w.WriteN(uint16(0), 12)  // program_info_length = 0
    
    // Один елементарний потік: H.264 video, PID=257
    w.Write(uint8(astits.StreamTypeH264Video))  // stream_type
    w.WriteN(uint8(0xff), 3)  // reserved
    w.WriteN(uint16(257), 13)  // ElementaryPID
    w.WriteN(uint16(0), 12)  // ES_info_length = 0
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    pmt, err := parsePMTSection(iter, buf.Len(), uint16(1))
    
    assert.NoError(t, err)
    assert.Equal(t, uint16(256), pmt.PCRPID)
    assert.Len(t, pmt.ElementaryStreams, 1)
    
    es := pmt.ElementaryStreams[0]
    assert.Equal(t, uint16(257), es.ElementaryPID)
    assert.Equal(t, astits.StreamTypeH264Video, es.StreamType)
}
```

### 🔹 Тест на валідацію `IsVideo`/`IsAudio`

```go
func TestStreamType_Classification(t *testing.T) {
    testCases := []struct {
        streamType astits.StreamType
        isVideo    bool
        isAudio    bool
        pesID      uint8
    }{
        {astits.StreamTypeH264Video, true, false, 0xe0},
        {astits.StreamTypeH265Video, true, false, 0xe0},
        {astits.StreamTypeAACAudio, false, true, 0xc0},
        {astits.StreamTypeAC3Audio, false, true, 0xfd},
        {astits.StreamTypePrivateData, false, false, 0xbd},
        {astits.StreamType(0xFF), false, false, 0xbd},  // unknown
    }
    
    for _, tc := range testCases {
        t.Run(tc.streamType.String(), func(t *testing.T) {
            assert.Equal(t, tc.isVideo, tc.streamType.IsVideo())
            assert.Equal(t, tc.isAudio, tc.streamType.IsAudio())
            assert.Equal(t, tc.pesID, tc.streamType.ToPESStreamID())
        })
    }
}
```

### 🔹 Тест на round-trip (парсинг ↔ запис)

```go
func TestPMTSection_RoundTrip(t *testing.T) {
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
func filterStreamsByType(pmt *astits.PMTData, types ...astits.StreamType) []*astits.PMTElementaryStream {
    var filtered []*astits.PMTElementaryStream
    typeSet := make(map[astits.StreamType]bool)
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
    astits.StreamTypeH265Video,
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
        if es.StreamType.IsVideo() {
            hasVideo = true
            break
        }
    }
    if !hasVideo {
        return fmt.Errorf("no video stream found in PMT")
    }
    
    return nil
}

// 4: Моніторинг:
func monitorPMTHealth(pmt *astits.PMTData, channelID string, metrics *PMTMetrics) {
    metrics.StreamCount.WithLabelValues(channelID).Set(float64(len(pmt.ElementaryStreams)))
    
    videoCount := 0
    audioCount := 0
    for _, es := range pmt.ElementaryStreams {
        if es.StreamType.IsVideo() {
            videoCount++
        } else if es.StreamType.IsAudio() {
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
0x01             | MPEG-1 Video       | ⚠️ Застарілий, рідко
0x02             | MPEG-2 Video       | ⚠️ Застарілий
0x03             | MPEG-1 Audio       | ⚠️ Застарілий
0x04             | MPEG-2 Audio       | ⚠️ Застарілий
0x0F             | AAC (ADTS)         | ✅ Популярний для аудіо
0x11             | AAC (LATM)         | ✅ Альтернатива ADTS
0x1B             | H.264/AVC          | ✅ Найпопулярніший відео
0x24             | HEVC/H.265         | ✅ Для 4K/економії бітрейту
0x06             | DVB Subtitles      | ⚠️ Якщо потрібні субтитри
0x81             | AC-3               | ⚠️ Для 5.1 аудіо
0x87             | E-AC-3             | ⚠️ Enhanced AC-3
0x86             | SCTE-35            | ✅ Маркери реклами/сплісингу
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