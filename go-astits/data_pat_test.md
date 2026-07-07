# Глибоке роз'яснення: Тести PAT (Program Association Table) у astits — парсинг та серіалізація

Цей файл містить **комплексні тести парсингу та запису секції PAT (Program Association Table)** — фундаментальної таблиці MPEG-TS, що описує доступні програми та їхні PMT PID. Без валідного PAT плеєр не зможе знайти відео/аудіо потоки.

---

## 🎯 Навіщо PAT потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ PAT у контексті HLS-стрімінгу:         │
│                                         │
│ 🔹 Ідентифікація програм:               │
│   • Program Number → PMT PID           │
│   • Без PAT плеєр не знайде програми   │
│                                         │
│ 🔹 Маршрутизація потоків:               │
│   • PID 0x0000 завжди = PAT            │
│   • PMT PID вказує, де шукати опис     │
│     відео/аудіо потоків                │
│                                         │
│ 🔹 Для HLS:                             │
│   • PAT має бути першим у потоці       │
│   • Неправильний PAT → чорний екран    │
│   • Оновлення PAT → динамічні канали   │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `PATData` та тестові дані

### Тип даних

```go
type PATData struct {
    Programs    []*PATProgram  // 🎯 список програм у потоці
    TransportStreamID uint16   // 🎯 унікальний ID транспортного потоку
}

type PATProgram struct {
    ProgramNumber uint16  // 🎯 номер програми (1 = основна, 0 = NIT)
    ProgramMapID  uint16  // 🎯 PID таблиці PMT для цієї програми
}
```

### Тестові дані: `pat` та `patBytes()`

```go
// Глобальна змінна — еталонне значення для тесту
var pat = &PATData{
    TransportStreamID: 1,
    Programs: []*PATProgram{
        {ProgramNumber: 2, ProgramMapID: 3},   // Програма 2 → PMT на PID 3
        {ProgramNumber: 4, ProgramMapID: 5},   // Програма 4 → PMT на PID 5
    },
}

// Генератор "еталонних" байтів для PAT секції (payload без заголовка)
func patBytes() []byte {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // 🔹 Програма 1: ProgramNumber=2, ProgramMapID=3
    w.Write(uint16(2))              // Program number (2 байти)
    w.Write("111")                  // reserved = 0b111 (3 біти)
    w.Write("0000000000011")       // ProgramMapID = 3 (13 біт)
    
    // 🔹 Програма 2: ProgramNumber=4, ProgramMapID=5
    w.Write(uint16(4))              // Program number
    w.Write("111")                  // reserved
    w.Write("0000000000101")       // ProgramMapID = 5 (13 біт)
    
    // Результат: 8 байт = 2 програми × 4 байти кожна
    return buf.Bytes()
}
```

> 💡 **Важливо**: `patBytes()` генерує тільки **payload секції** (без PSI заголовка та CRC32). Функція `parsePATSection` очікує саме payload + довжину, бо заголовок парситься на вищому рівні (`parsePSISection`).

---

## 🔍 Тест `TestParsePATSection`

```go
func TestParsePATSection(t *testing.T) {
    // 🔹 1. Отримати тестові байти
    b := patBytes()
    
    // 🔹 2. Парсити секцію: ітератор + довжина + transport_stream_id
    d, err := parsePATSection(astikit.NewBytesIterator(b), len(b), uint16(1))
    
    // 🔹 3. Перевірити результат
    assert.Equal(t, d, pat)        // ✅ структурна рівність з еталоном
    assert.NoError(t, err)         // ✅ без помилок
}
```

### Що робить `parsePATSection` (гіпотетична реалізація)

```go
func parsePATSection(i *astikit.BytesIterator, sectionLength int, tableIDExtension uint16) (*PATData, error) {
    d := &PATData{TransportStreamID: tableIDExtension}
    
    // 🔹 Цикл по програмах до кінця секції
    offsetEnd := i.Offset() + sectionLength
    
    for i.Offset() < offsetEnd {
        prog := &PATProgram{}
        
        // ── Program Number (2 байти, big-endian) ──
        bs, _ := i.NextBytesNoCopy(2)
        prog.ProgramNumber = uint16(bs[0])<<8 | uint16(bs[1])
        
        // ── ProgramMapID (13 біт) + reserved (3 біти) ──
        bs, _ = i.NextBytesNoCopy(2)
        // Формат: [3 reserved][13 ProgramMapID]
        prog.ProgramMapID = uint16(bs[0]&0x1f)<<8 | uint16(bs[1])
        // bs[0]&0x1f = молодші 5 біт байта 0 (біти 4-0)
        // bs[1] = весь байт 1 (біти 7-0)
        // Разом: 5 + 8 = 13 біт для PID
        
        d.Programs = append(d.Programs, prog)
    }
    
    return d, nil
}
```

### 🎯 Ключовий момент: читання 13-бітного ProgramMapID

```
Формат: [3 reserved][13 ProgramMapID] розподілені у 2 байтах

Байт 0: [7-5]reserved [4-0]PID[12:8]
Байт 1: [7-0]PID[7:0]

Приклад: ProgramMapID = 3 = 0b0000000000011

Байт 0: 0b11100000 = 0xE0
  - reserved = 0b111 (біти 7-5) ✅
  - PID[12:8] = 0b00000 = 0 (біти 4-0)

Байт 1: 0b00000011 = 0x03 = 3
  - PID[7:0] = 3

Розрахунок:
  ProgramMapID = (0 << 8) | 3 = 3 ✅
```

> 💡 **Порада**: Завжди тестуйте бітові операції на відомих значеннях, щоб уникнути помилок зсуву.

---

## ✏️ Тест `TestWritePATSection`: серіалізація

```go
func TestWritePATSection(t *testing.T) {
    bw := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: bw})
    
    // 🔹 Записати PAT через writePATSection
    n, err := writePATSection(w, pat)
    
    // 🔹 Перевірити результат
    assert.NoError(t, err)
    assert.Equal(t, n, 8)              // ✅ 2 програми × 4 байти = 8 байт
    assert.Equal(t, n, bw.Len())       // ✅ кількість записаних байт
    assert.Equal(t, patBytes(), bw.Bytes())  // ✅ бінарна ідентичність з еталоном
}
```

**Чому round-trip тест важливий:**
```
• Ручне кодування (patBytes) = "джерело істини" за специфікацією
• writePATSection() = реалізація бібліотеки
• Бінарна ідентичність = гарантія сумісності з іншими декодерами

Якщо байти відрізняються:
→ Плеєри можуть відкинути PAT як невалідний
→ Програми не будуть знайдені
→ Чорний екран або помилка відтворення у клієнта
```

---

## ⚡ Бенчмарки: продуктивність парсингу та запису

### `BenchmarkParsePATSection`

```go
func BenchmarkParsePATSection(b *testing.B) {
    b.ReportAllocs()  // 📊 звітувати про алокації
    bs := patBytes()  // підготувати тестові дані
    
    for i := 0; i < b.N; i++ {
        parsePATSection(astikit.NewBytesIterator(bs), len(bs), uint16(1))
    }
}
```

**Очікувані результати:**
```
BenchmarkParsePATSection-8    500000    2000 ns/op    100 B/op    5 allocs/op
```

### `BenchmarkWritePATSection`

```go
func BenchmarkWritePATSection(b *testing.B) {
    b.ReportAllocs()
    
    bw := &bytes.Buffer{}
    bw.Grow(1024)  // 🔹 попереднє виділення пам'яті
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: bw})
    
    for i := 0; i < b.N; i++ {
        bw.Reset()  // 🔹 очищення без deallocation
        writePATSection(w, pat)
    }
}
```

**Що аналізувати:**
| Метрика | Ідеальне значення | Що означає відхилення |
|---------|-------------------|----------------------|
| `ns/op` | < 5 µs для 2 програм | Повільний парсинг → оптимізувати бітові операції |
| `B/op`  | < 200 B | Зайві алокації → використовувати bytesPool |
| `allocs/op` | < 10 | Кожна алокація = тиск на GC |

> 💡 **Порада**: `bw.Grow(1024)` + `bw.Reset()` у бенчмарку запису імітує реальне використання — буфер переиспользується без нових алокацій.

---

## 🧮 Формат PAT секції у деталях

```
PAT Section Payload (без PSI заголовка та CRC):
┌─────────────────────────────────┐
│ Program loop (повтор для кожної програми):
│   [16] program_number           │ ← номер програми (0 = NIT, 1+ = програми)
│   [3]  reserved = 0b111         │
│   [13] program_map_PID          │ ← PID таблиці PMT для цієї програми
└─────────────────────────────────┘

Повна PSI секція (додається на вищому рівні):
[8]  table_id = 0x00 (PAT)
[12] section_length
[16] transport_stream_id
[16] reserved + version + current_next
[8]  section_number = 0
[8]  last_section_number = 0
[... program loop ...]
[32] CRC32
```

### Приклад розбору бітів для ProgramMapID

```
Вхідні байти для ProgramMapID=5: 0b11100000 0b00000101
                                 ↑         ↑
                                 │         └─ Байт 2: молодші 8 біт PID = 5
                                 └─ Байт 1: [7-5]reserved=0b111, [4-0]старші 5 біт PID=0

Розрахунок:
• Байт 1: 0b11100000 = 0xE0
  - reserved = 0b111 (біти 7-5) = 7 ✅
  - pidHigh = 0b00000 (біти 4-0) = 0

• Байт 2: 0b00000101 = 0x05 = 5

• ProgramMapID = (0 << 8) | 5 = 5 ✅
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Витягування PMT PID для основної програми

```go
// У VideoManifestProxy — отримання PMT PID для програми 1:
func getPMTForProgram(pat *astits.PATData, programNumber uint16) (uint16, error) {
    for _, prog := range pat.Programs {
        if prog.ProgramNumber == programNumber {
            return prog.ProgramMapID, nil
        }
    }
    return 0, fmt.Errorf("program %d not found in PAT", programNumber)
}

// Використання:
pmtPID, err := getPMTForProgram(pat, 1)  // основна програма
if err != nil {
    return fmt.Errorf("failed to find main program: %w", err)
}
// Тепер шукаємо PMT на PID pmtPID...
```

### ✅ 2: Побудова programMap для динамічної маршрутизації

```go
// У demuxer — створення мапінгу PID → program_number після парсингу PAT:
func buildProgramMap(pat *astits.PATData) map[uint16]uint16 {
    pm := make(map[uint16]uint16)
    
    for _, prog := range pat.Programs {
        // 🔹 ProgramNumber 0 = NIT (Network Information Table) — пропускаємо
        if prog.ProgramNumber > 0 {
            pm[prog.ProgramMapID] = prog.ProgramNumber
        }
    }
    
    return pm
}

// Використання:
programMap := buildProgramMap(pat)
// Тепер при зустрічі пакета з PID=3 знаємо: це PMT програми 2
if program, ok := programMap[packetPID]; ok {
    log.Debugf("PID %d belongs to program %d (PMT)", packetPID, program)
}
```

### ✅ 3: Валідація PAT перед використанням

```go
// Перевірити, що PAT валідний перед обробкою:
func validatePAT(pat *astits.PATData) error {
    // 🔹 Має бути хоча б одна програма (окрім NIT)
    hasProgram := false
    for _, prog := range pat.Programs {
        if prog.ProgramNumber > 0 {
            hasProgram = true
            break
        }
    }
    if !hasProgram {
        return fmt.Errorf("no programs found in PAT")
    }
    
    // 🔹 ProgramMapID не має бути 0x0000 або 0x1FFF (reserved)
    for _, prog := range pat.Programs {
        if prog.ProgramMapID == 0x0000 || prog.ProgramMapID == 0x1FFF {
            return fmt.Errorf("invalid ProgramMapID 0x%04X for program %d", 
                prog.ProgramMapID, prog.ProgramNumber)
        }
    }
    
    return nil
}
```

### ✅ 4: Моніторинг змін у PAT

```go
// monitoring.Monitor — метрики для PAT:
type PATMetrics struct {
    PATParsed        *prometheus.CounterVec  // кількість парсингів PAT
    ProgramsFound    *prometheus.GaugeVec    // кількість програм у потоці
    PMTPIDs          *prometheus.GaugeVec    // знайдені PMT PID
    PATVersionGauge  *prometheus.GaugeVec    // версія PAT (для детекції оновлень)
}

// У обробці PAT:
func monitorPAT(pat *astits.PATData, channelID string, metrics *PATMetrics, lastVersion *uint8) {
    metrics.PATParsed.WithLabelValues(channelID).Inc()
    metrics.ProgramsFound.WithLabelValues(channelID).Set(float64(len(pat.Programs)))
    
    // 🔹 Відстежувати PMT PID для кожного програми
    for _, prog := range pat.Programs {
        if prog.ProgramNumber > 0 {
            metrics.PMTPIDs.WithLabelValues(
                channelID, 
                fmt.Sprintf("program_%d", prog.ProgramNumber),
            ).Set(float64(prog.ProgramMapID))
        }
    }
    
    // 🔹 Детекція оновлення PAT за версією (якщо доступно з заголовка)
    // (версія зберігається у PSISectionSyntaxHeader, не у PATData)
}
```

### ✅ 5: Обробка динамічних змін програми

```go
// При зміні вмісту PAT (напр., додано нову програму):
func handlePATUpdate(oldPAT, newPAT *astits.PATData, channelID string) {
    // 🔹 Порівняти версії (якщо доступно)
    if oldPAT == nil {
        log.Infof("Channel %s: new PAT detected with %d programs", 
            channelID, len(newPAT.Programs))
        return
    }
    
    // 🔹 Знайти додані програми
    oldPrograms := make(map[uint16]uint16)  // program_number → PMT_PID
    for _, prog := range oldPAT.Programs {
        oldPrograms[prog.ProgramNumber] = prog.ProgramMapID
    }
    
    for _, prog := range newPAT.Programs {
        if oldPID, exists := oldPrograms[prog.ProgramNumber]; !exists {
            log.Infof("Channel %s: new program added: number=%d, PMT_PID=%d", 
                channelID, prog.ProgramNumber, prog.ProgramMapID)
            // Ініціалізувати обробку для нової програми...
        } else if oldPID != prog.ProgramMapID {
            log.Infof("Channel %s: program %d PMT PID changed: %d → %d", 
                channelID, prog.ProgramNumber, oldPID, prog.ProgramMapID)
            // Оновити маршрутизацію для програми...
        }
    }
    
    // 🔹 Знайти видалені програми
    newPrograms := make(map[uint16]bool)
    for _, prog := range newPAT.Programs {
        newPrograms[prog.ProgramNumber] = true
    }
    
    for progNum := range oldPrograms {
        if !newPrograms[progNum] {
            log.Infof("Channel %s: program %d removed", channelID, progNum)
            // Зупинити обробку для видаленої програми...
        }
    }
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на кілька програм у PAT

```go
func TestParsePATSection_MultiplePrograms(t *testing.T) {
    // Створити PAT з 3 програмами
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Програма 1: number=1, PMT_PID=0x100
    w.Write(uint16(1))
    w.Write("111")
    w.Write("0000001000000")  // 0x100 = 256
    
    // Програма 2: number=2, PMT_PID=0x101
    w.Write(uint16(2))
    w.Write("111")
    w.Write("0000001000001")  // 0x101 = 257
    
    // Програма 3: number=3, PMT_PID=0x102
    w.Write(uint16(3))
    w.Write("111")
    w.Write("0000001000010")  // 0x102 = 258
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    pat, err := parsePATSection(iter, buf.Len(), uint16(123))
    
    assert.NoError(t, err)
    assert.Equal(t, uint16(123), pat.TransportStreamID)
    assert.Len(t, pat.Programs, 3)
    
    // Перевірити кожну програму
    assert.Equal(t, uint16(1), pat.Programs[0].ProgramNumber)
    assert.Equal(t, uint16(256), pat.Programs[0].ProgramMapID)
    
    assert.Equal(t, uint16(2), pat.Programs[1].ProgramNumber)
    assert.Equal(t, uint16(257), pat.Programs[1].ProgramMapID)
    
    assert.Equal(t, uint16(3), pat.Programs[2].ProgramNumber)
    assert.Equal(t, uint16(258), pat.Programs[2].ProgramMapID)
}
```

### 🔹 Тест на NIT (program_number = 0)

```go
func TestParsePATSection_WithNIT(t *testing.T) {
    // PAT з NIT (program_number=0) + одна програма
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // NIT: program_number=0, network_PID=0x10
    w.Write(uint16(0))  // program_number = 0 → NIT
    w.Write("111")
    w.Write("0000000010000")  // network_PID = 0x10 = 16
    
    // Програма 1: number=1, PMT_PID=0x100
    w.Write(uint16(1))
    w.Write("111")
    w.Write("0000001000000")  // 0x100 = 256
    
    iter := astikit.NewBytesIterator(buf.Bytes())
    pat, err := parsePATSection(iter, buf.Len(), uint16(1))
    
    assert.NoError(t, err)
    assert.Len(t, pat.Programs, 2)
    
    // Перевірити NIT
    assert.Equal(t, uint16(0), pat.Programs[0].ProgramNumber)
    assert.Equal(t, uint16(16), pat.Programs[0].ProgramMapID)  // network_PID
    
    // Перевірити програму
    assert.Equal(t, uint16(1), pat.Programs[1].ProgramNumber)
    assert.Equal(t, uint16(256), pat.Programs[1].ProgramMapID)
}
```

### 🔹 Тест на round-trip (парсинг ↔ запис)

```go
func TestPATSection_RoundTrip(t *testing.T) {
    original := &astits.PATData{
        TransportStreamID: 456,
        Programs: []*astits.PATProgram{
            {ProgramNumber: 1, ProgramMapID: 0x100},
            {ProgramNumber: 2, ProgramMapID: 0x101},
            {ProgramNumber: 0, ProgramMapID: 0x10},  // NIT
        },
    }
    
    // Серіалізувати
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    writePATSection(w, original)
    
    // Парсити назад
    parsed, err := parsePATSection(astikit.NewBytesIterator(buf.Bytes()), buf.Len(), original.TransportStreamID)
    assert.NoError(t, err)
    
    // Порівняти ключові поля
    assert.Equal(t, original.TransportStreamID, parsed.TransportStreamID)
    assert.Len(t, parsed.Programs, 3)
    
    for i, origProg := range original.Programs {
        parsedProg := parsed.Programs[i]
        assert.Equal(t, origProg.ProgramNumber, parsedProg.ProgramNumber)
        assert.Equal(t, origProg.ProgramMapID, parsedProg.ProgramMapID)
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання 13-бітних PID | ProgramMapID зміщено на 1-2 біти | Перевірити бітові маски: `(b0&0x1F)<<8 \| b1` для PID, де b0 містить 5 старших біт |
| ProgramNumber 0 обробляється як програма | NIT помилково трактується як програма | Додати перевірку: `if prog.ProgramNumber == 0 { continue }` при побудові programMap |
| PAT не містить програм | Помилка "no programs found" при валідації | Перевірити вхідний потік: чи дійсно передається PAT, чи це інша таблиця |
| Неправильний TransportStreamID | Плутанина між потоками у багатоканальній системі | Переконатися, що `tableIDExtension` передається коректно з `parsePSISection` |
| Великий PAT не парситься | Цикл зупиняється завчасно | Перевірити розрахунок `offsetEnd = i.Offset() + sectionLength` |

### Приклад коректного читання 13-бітного ProgramMapID:

```go
func read13BitProgramMapID(i *astikit.BytesIterator) (uint16, error) {
    bs, err := i.NextBytesNoCopy(2)
    if err != nil { return 0, err }
    
    // Формат: [3 reserved][13 ProgramMapID] у перших двох байтах
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
// 1: Витягування PMT PID для програми:
func getPMTForProgram(pat *astits.PATData, programNumber uint16) (uint16, error) {
    for _, prog := range pat.Programs {
        if prog.ProgramNumber == programNumber {
            return prog.ProgramMapID, nil
        }
    }
    return 0, fmt.Errorf("program %d not found in PAT", programNumber)
}

// 2: Побудова programMap для динамічної маршрутизації:
func buildProgramMap(pat *astits.PATData) map[uint16]uint16 {
    pm := make(map[uint16]uint16)
    for _, prog := range pat.Programs {
        if prog.ProgramNumber > 0 {  // пропускаємо NIT (program_number=0)
            pm[prog.ProgramMapID] = prog.ProgramNumber
        }
    }
    return pm
}

// 3: Валідація PAT перед використанням:
func validatePAT(pat *astits.PATData) error {
    hasProgram := false
    for _, prog := range pat.Programs {
        if prog.ProgramNumber > 0 {
            hasProgram = true
            break
        }
        if prog.ProgramMapID == 0x0000 || prog.ProgramMapID == 0x1FFF {
            return fmt.Errorf("invalid ProgramMapID 0x%04X", prog.ProgramMapID)
        }
    }
    if !hasProgram {
        return fmt.Errorf("no programs found in PAT")
    }
    return nil
}

// 4: Моніторинг:
func monitorPATHealth(pat *astits.PATData, channelID string, metrics *PATMetrics) {
    metrics.ProgramCount.WithLabelValues(channelID).Set(float64(len(pat.Programs)))
    
    programCount := 0
    for _, prog := range pat.Programs {
        if prog.ProgramNumber > 0 {
            programCount++
        }
    }
    metrics.ActiveProgramCount.WithLabelValues(channelID).Set(float64(programCount))
}

// 5: Helper для логування:
func logPAT(pat *astits.PATData, channelID string) {
    log.Infof("Channel %s: PAT (TS_ID=%d) with %d entries:", 
        channelID, pat.TransportStreamID, len(pat.Programs))
    
    for _, prog := range pat.Programs {
        if prog.ProgramNumber == 0 {
            log.Infof("  NIT: network_PID=0x%04X", prog.ProgramMapID)
        } else {
            log.Infof("  Program %d: PMT_PID=0x%04X", 
                prog.ProgramNumber, prog.ProgramMapID)
        }
    }
}
```

---

## 📊 Матриця полів PAT для вашого пайплайну

```
Поле PAT               | Тип       | Використання у CCTV HLS
───────────────────────┼───────────┼─────────────────────────
TransportStreamID      | uint16    | ✅ Ідентифікація потоку для плейлиста
ProgramNumber          | uint16    | ✅ Номер програми (1 = основна)
ProgramMapID           | uint16    | ✅ PID таблиці PMT для цієї програми
ProgramNumber = 0      | special   | ⚠️ NIT (мережева інформація) — зазвичай ігноруємо
```

---

## 📚 Корисні посилання

- [ISO/IEC 13818-1: PAT specification](https://www.iso.org/standard/61236.html)
- [MPEG-TS PAT overview](https://en.wikipedia.org/wiki/Program-specific_information#Program_Association_Table)
- [astits PAT parsing source](https://github.com/asticode/go-astits/blob/master/data.go)

> 💡 **Ключова ідея**: PAT — це "зміст" вашого MPEG-TS потоку. У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Автоматично ідентифікувати PMT PID для подальшого парсингу відео/аудіо
> - 🔍 Відстежувати динамічні зміни у складі програм (додавання/видалення каналів)
> - 📊 Збирати метрики про кількість програм для моніторингу якості потоку
> - 🧩 Фільтрувати непотрібні програми на ранньому етапі (економія ресурсів)

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати PAT-обробку у ваш `segmentAssembler` для динамічного оновлення programMap
- 🧪 Написати integration-тест для перевірки сумісності з реальними енкодерами
- 📈 Додати Prometheus-метрики для моніторингу змін у PAT по каналах

🛠️