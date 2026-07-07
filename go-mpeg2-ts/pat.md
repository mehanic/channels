# 🔍 Глибокий розбір: парсинг PAT (Program Association Table) для MPEG-2 TS

Цей код реалізує **парсинг PAT (Program Association Table)** — критичної таблиці у MPEG-2 TS, яка вказує, які програми (канали) доступні та де знайти їхні PMT (Program Map Tables). Розберемо архітектурно, з критичним аналізом та практичними рекомендаціями.

---

## 📦 Контекст: що таке PAT і навіщо він потрібен?

### Контекст: ієрархія MPEG-2 TS таблиць
```
📦 Транспортний потік (TS)
   │
   ├─ PAT (Program Association Table) — PID 0x00 (завжди!)
   │  ├─ Вказує: "Програма #1 → шукай PMT на PID 0x100"
   │  ├─ "Програма #2 → шукай PMT на PID 0x101"
   │  └─ "Програма 0x0000 → NIT (Network Information Table)"
   │
   ├─ PMT (Program Map Table) — PID з PAT
   │  ├─ Вказує: "Програма #1 містить:"
   │  │  ├─ Відео: PID 0x110 (H.264)
   │  │  ├─ Аудіо: PID 0x111 (AAC)
   │  │  └─ Субтитри: PID 0x112 (DVB)
   │  └─ ...
   │
   └─ PES/ES пакети — фактичні медіа-дані на PID з PMT
```

### 🎯 Призначення `ParsePAT()`
```go
// 🎯 Вхід: один TS пакет з payload, що містить PAT секцію
// 🎯 Вихід: структурована PAT таблиця з програмами та їхніми PMT PID
// 🎯 Використання: 
//   1. Знайти PAT пакет (PID == 0x00 && PUSI == 1)
//   2. Викликати ParsePAT() → отримати список програм
//   3. Для кожної програми: фільтрувати пакети з ProgramMapPID
//   4. Парсити PMT → отримати PID відео/аудіо/субтитрів
```

---

## 🔬 Детальний розбір структури PAT

### `PAT` — основна структура
```go
type PAT struct {
    Pointer                byte     // pointer_field: зсув до початку таблиці (зазвичай 0)
    TableID                byte     // завжди 0x00 для PAT
    SectionSyntaxIndicator bool     // завжди true для PAT
    Reserved1              byte     // 2 біти зарезервовано + 6 біт = section_length[11:10]
    SectionLength          uint16   // довжина секції після цього поля (макс. 1021)
    TransportStreamID      uint16   // унікальний ID транспортного потоку
    Reserved2              int      // ⚠️ 3 біти зарезервовано + 5 біт version_number
    Version                byte     // версія таблиці (0-31), зростає при зміні
    CurrentNextIndicator   bool     // true = застосувати зараз, false = наступна версія
    SectionNumber          byte     // номер секції (0-based, для розбиття великих таблиць)
    LastSectionNumber      byte     // номер останньої секції
    CRC32                  uint     // ⚠️ має бути uint32, не uint!
    Programs               []PATProgram  // масив програм
}
```

#### ⚠️ Проблеми типів у `PAT`
| Поле | У коді | У специфікації | Наслідок |
|------|--------|---------------|----------|
| `Reserved2` | `int` | 3 біти | ⚠️ Зайва пам'ять, можлива плутанина |
| `Version` | `byte` | 5 біт | ⚠️ Може прийняти значення 32-255 (невалідні) |
| `CRC32` | `uint` | 32 біти | ⚠️ На 64-бітних системах — 8 байт замість 4 |

#### ✅ Правильні типи
```go
type PAT struct {
    Pointer                uint8    // ✅ Явний 8-бітний тип
    TableID                uint8    // ✅ 0x00 для PAT
    SectionSyntaxIndicator bool     // ✅ 1 біт
    SectionLength          uint16   // ✅ 12 біт, але зберігаємо як uint16
    TransportStreamID      uint16   // ✅ 16 біт
    Version                uint8    // ✅ 5 біт, але зберігаємо як uint8
    CurrentNextIndicator   bool     // ✅ 1 біт
    SectionNumber          uint8    // ✅ 8 біт
    LastSectionNumber      uint8    // ✅ 8 біт
    CRC32                  uint32   // ✅ Явний 32-бітний тип для CRC
    Programs               []PATProgram
}
```

### `PATProgram` — запис програми
```go
type PATProgram struct {
    ProgramNumber uint16  // ✅ 16 біт: 0x0000 = NIT, 0x0001-0xFFFF = програми
    Reserved      int     // ⚠️ 3 біти зарезервовано
    NetworkPID    PID     // ✅ Тільки якщо ProgramNumber == 0x0000
    ProgramMapPID PID     // ✅ Тільки якщо ProgramNumber != 0x0000
}
```

#### ⚠️ Проблема дизайну: взаємовиключні поля
```go
// ❌ NetworkPID та ProgramMapPID в одній структурі:
// • Якщо ProgramNumber != 0 → NetworkPID не використовується (марна пам'ять)
// • Якщо ProgramNumber == 0 → ProgramMapPID не використовується
// • Неможливо відрізнити "не встановлено" від "значення 0" без додаткової логіки

// ✅ Кращий дизайн: один типізований PID з контекстом
type PATProgram struct {
    ProgramNumber uint16
    PID           PID  // ✅ Єдине поле
}

// ✅ Методи для безпечного доступу:
func (p PATProgram) IsNetworkInfo() bool {
    return p.ProgramNumber == 0x0000
}

func (p PATProgram) GetProgramMapPID() (PID, error) {
    if p.IsNetworkInfo() {
        return 0, fmt.Errorf("program 0x0000 is NIT, not a program")
    }
    return p.PID, nil
}

func (p PATProgram) GetNetworkPID() (PID, error) {
    if !p.IsNetworkInfo() {
        return 0, fmt.Errorf("program 0x%X is not NIT", p.ProgramNumber)
    }
    return p.PID, nil
}
```

---

## 🔬 Детальний розбір `ParsePAT()` — крок за кроком

### Крок 1: Отримання payload та pointer_field
```go
func (p *Packet) ParsePAT() (PAT, error) {
    pat := PAT{}
    payload, err := p.GetPayload()
    if err != nil {
        return PAT{}, err
    }
    pat.Pointer = payload[0]  // 🎯 pointer_field: зазвичай 0, іноді >0 для вирівнювання
    // ⚠️ Не перевіряється, чи Pointer в межах payload!
```

#### ⚠️ Потенційна проблема: вихід за межі слайсу
```go
// ❌ Якщо payload[0] = 10, а len(payload) = 5 → payload[10] → panic!
// ✅ Правильно: перевірити межі перед доступом:
if int(pat.Pointer) >= len(payload) {
    return PAT{}, fmt.Errorf("pointer_field %d exceeds payload length %d", 
        pat.Pointer, len(payload))
}
// 🎯 Пропустити pointer_field байтів до початку таблиці
tableStart := 1 + int(pat.Pointer)
if tableStart >= len(payload) {
    return PAT{}, fmt.Errorf("invalid pointer_field: no table data")
}
tableData := payload[tableStart:]
```

---

### Крок 2: Парсинг заголовка секції
```go
    pat.TableID = payload[1]  // ⚠️ Має бути 0x00 для PAT!
    pat.SectionSyntaxIndicator = ((payload[2] >> 7) & 0x01) == 1
    if ((payload[2] >> 6) & 0x01) == 1 {  // ⚠️ Завжди має бути 0!
        return PAT{}, fmt.Errorf("invalid format")
    }
    pat.SectionLength = uint16(payload[2]&0x0F)<<8 | uint16(payload[3])
```

#### ✅ Правильні перевірки заголовка
```go
// ✅ Перевірка TableID:
if pat.TableID != 0x00 {
    return PAT{}, fmt.Errorf("invalid TableID for PAT: 0x%02X != 0x00", pat.TableID)
}

// ✅ Перевірка SectionSyntaxIndicator:
if !pat.SectionSyntaxIndicator {
    return PAT{}, fmt.Errorf("section_syntax_indicator must be 1 for PAT")
}

// ✅ Перевірка зарезервованого біта (має бути 0):
if ((payload[2] >> 6) & 0x01) == 1 {
    return PAT{}, fmt.Errorf("reserved bit must be 0, got 1")
}

// ✅ Перевірка SectionLength:
if pat.SectionLength < 5+4 {  // Мінімум: 5 байт заголовка + 4 байти CRC
    return PAT{}, fmt.Errorf("SectionLength too short: %d < 9", pat.SectionLength)
}
if pat.SectionLength > 1021 {  // Максимум за специфікацією
    return PAT{}, fmt.Errorf("SectionLength too long: %d > 1021", pat.SectionLength)
}
// ✅ Перевірка, що таблиця вміщується у payload:
expectedEnd := tableStart + int(pat.SectionLength) + 4  // +4 для CRC
if expectedEnd > len(payload) {
    return PAT{}, fmt.Errorf("PAT section extends beyond payload: %d > %d", 
        expectedEnd, len(payload))
}
```

---

### Крок 3: Парсинг основних полів
```go
    pat.TransportStreamID = uint16(payload[4])<<8 | uint16(payload[5])
    pat.Version = (payload[6] >> 1) & 0x1F  // ✅ Правильна маска для 5 біт
    pat.CurrentNextIndicator = (payload[6] & 0x01) == 0x01
    pat.SectionNumber = payload[7]
    pat.LastSectionNumber = payload[8]
```

#### ✅ Додаткові перевірки
```go
// ✅ Перевірка версії (0-31):
if pat.Version > 31 {
    return PAT{}, fmt.Errorf("invalid version: %d > 31 (5 bits)", pat.Version)
}

// ✅ Перевірка номерів секцій:
if pat.SectionNumber > pat.LastSectionNumber {
    return PAT{}, fmt.Errorf("SectionNumber %d > LastSectionNumber %d", 
        pat.SectionNumber, pat.LastSectionNumber)
}
```

---

### Крок 4: Парсинг циклу програм
```go
    pat.Programs = make([]PATProgram, (pat.SectionLength-5-4)/4)
    for i := uint16(0); i < (pat.SectionLength-5-4)/4; i++ {
        base := 9 + i*4  // ⚠️ "9" = tableStart(1+pointer) + 8 байт заголовка
        pat.Programs[i].ProgramNumber = uint16(payload[base])<<8 | uint16(payload[base+1])
        if pat.Programs[i].ProgramNumber == 0x0000 {
            pat.Programs[i].NetworkPID = PID(uint16(payload[base+2]&0x1f)<<8 | uint16(payload[base+3])&0x1fff)
        } else {
            pat.Programs[i].ProgramMapPID = PID(uint16(payload[base+2]&0x1f)<<8 | uint16(payload[base+3])&0x1fff)
        }
    }
```

#### ⚠️ Критичні проблеми
```go
// ❌ Hardcoded "9" для base:
base := 9 + i*4  // ❌ Не враховує pointer_field!
// 📋 Правильна формула:
// • pointer_field = payload[0]
// • table_start = 1 + pointer_field
// • заголовок секції = 8 байт (TableID...LastSectionNumber)
// • початок циклу програм = table_start + 8
base := tableStart + 8 + int(i)*4  // ✅ Правильно

// ❌ Неправильна маска для PID:
PID(uint16(payload[base+2]&0x1f)<<8 | uint16(payload[base+3])&0x1fff)
// 📋 PID = 13 біт: [5 біт з byte2][8 біт з byte3]
// • payload[base+2] & 0x1F = нижні 5 біт (правильно)
// • <<8 = зсув на 8 біт вліво (правильно)
// • | (payload[base+3] & 0x1FFF) = ❌ Неправильно!
//   • 0x1FFF = 13 біт, але payload[base+3] — тільки 8 біт!
//   • & 0x1FFF на байті = & 0xFF (зайва операція)

// ✅ Правильний парсинг 13-бітного PID:
pid := PID((uint16(payload[base+2]&0x1F) << 8) | uint16(payload[base+3]))
// • payload[base+2] & 0x1F = 5 біт
// • <<8 = зсув на 8 позицій → біти [12:8]
// • | payload[base+3] = додає біти [7:0]
// • Результат: 13-бітний PID у діапазоні 0x0000-0x1FFF

// ✅ Перевірка валідності PID:
if pid > 0x1FFF {
    return PAT{}, fmt.Errorf("invalid PID: 0x%X > 0x1FFF", pid)
}
```

---

### Крок 5: Парсинг та перевірка CRC-32
```go
    pat.CRC32 = uint(payload[pat.SectionLength])<<24 | uint(payload[pat.SectionLength+1])<<16 | uint(payload[pat.SectionLength+1])<<8 | uint(payload[pat.SectionLength+3])
    // ⚠️ Помилка: payload[pat.SectionLength+1] повторюється двічі!

    crc := calculateCRC(payload[1:pat.SectionLength])  // ⚠️ Неправильний діапазон!
    if uint32(pat.CRC32) != crc {
        return PAT{}, fmt.Errorf("CRC32 mismatch")
    }
```

#### ⚠️ Критичні помилки у CRC обробці
```go
// ❌ Помилка у збірці CRC32:
pat.CRC32 = uint(payload[pat.SectionLength])<<24 | 
            uint(payload[pat.SectionLength+1])<<16 | 
            uint(payload[pat.SectionLength+1])<<8 |  // ❌ Повторення індексу +1!
            uint(payload[pat.SectionLength+3])
// 📋 Правильно:
pat.CRC32 = uint32(payload[crcStart])<<24 | 
            uint32(payload[crcStart+1])<<16 | 
            uint32(payload[crcStart+2])<<8 | 
            uint32(payload[crcStart+3])

// ❌ Неправильний діапазон для CRC розрахунку:
crc := calculateCRC(payload[1:pat.SectionLength])
// 📋 Специфікація: CRC обчислюється на даних секції БЕЗ:
// • pointer_field
// • TableID (але ВКЛЮЧАЮЧИ TableID у розрахунок!)
// • ... до кінця секції, НЕ ВКЛЮЧАЮЧИ сам CRC
// 📋 Правильний діапазон:
// • Початок: tableStart (після pointer_field)
// • Кінець: tableStart + pat.SectionLength (виключаючи 4 байти CRC)
crcStart := tableStart
crcEnd := crcStart + int(pat.SectionLength)  // Вказує на перший байт CRC
crcData := payload[crcStart:crcEnd]  // Дані для CRC розрахунку

storedCRC := binary.BigEndian.Uint32(payload[crcEnd : crcEnd+4])
computedCRC := calculateCRC(crcData)  // Ваша функція calculateCRC з попереднього огляду
if storedCRC != computedCRC {
    return PAT{}, fmt.Errorf("CRC32 mismatch: stored=0x%08X, computed=0x%08X", 
        storedCRC, computedCRC)
}
```

---

## ⚠️ Загальні проблеми функції `ParsePAT`

### 1️⃣ Відсутність валідації вхідного пакету
```go
// ❌ ParsePAT() не перевіряє, чи пакет дійсно містить PAT:
// • Чи PID == 0x00?
// • Чи PayloadUnitStartIndicator == 1?
// • Чи TableID == 0x00?

// ✅ Додати перевірки на початку:
func (p *Packet) ParsePAT() (PAT, error) {
    // 🎯 Перевірка, що це PAT пакет
    if p.PID != 0x00 {
        return PAT{}, fmt.Errorf("not a PAT packet: PID=0x%X", p.PID)
    }
    if !p.PayloadUnitStartIndicator {
        return PAT{}, fmt.Errorf("PAT must start at payload boundary: PUSI=0")
    }
    
    payload, err := p.GetPayload()
    if err != nil {
        return PAT{}, fmt.Errorf("failed to get payload: %w", err)
    }
    // ... решта коду ...
}
```

### 2️⃣ Жорстке припущення про розташування таблиці
```go
// ❌ Код припускає, що PAT починається з payload[1]:
pat.TableID = payload[1]  // ❌ Ігнорує pointer_field!
// 📋 pointer_field може бути >0, якщо перед таблицею є вирівнюючі байти
// ✅ Правильно: використовувати tableStart = 1 + pointer_field
```

### 3️⃣ Відсутність підтримки multi-section PAT
```go
// ❌ Код обробляє тільки одну секцію:
// • Якщо PAT великий (>1021 байт), він розбивається на кілька секцій
// • SectionNumber/LastSectionNumber парсяться, але не використовуються
// ✅ Правильно: або документувати обмеження, або реалізувати збірку багатосекційних PAT

type PATParser struct {
    sections map[uint8][]byte  // section_number → дані секції
    lastSection uint8
}

func (pp *PATParser) AddSection(payload []byte) error {
    // 🎯 Парсинг заголовка секції
    // 🎯 Збереження у map за section_number
    // 🎯 Коли section_number == lastSection → збірка повної таблиці
}

func (pp *PATParser) Assemble() (PAT, error) {
    // 🎯 Конкатенація секцій у правильному порядку
    // 🎯 Парсинг повної таблиці
}
```

### 4️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу для PAT парсингу
// • Неможливо перевірити коректність бітового парсингу
// • Неможливо покрити edge cases (пошкоджені CRC, невалідні PID)

// ✅ Додати мінімальні тести:
func TestParsePAT_Valid(t *testing.T) {
    // 🎯 Створити моковий PAT пакет з відомими даними
    pkt := createMockPATPacket([]PATProgram{
        {ProgramNumber: 1, ProgramMapPID: 0x100},
        {ProgramNumber: 2, ProgramMapPID: 0x101},
    })
    
    pat, err := pkt.ParsePAT()
    require.NoError(t, err)
    assert.Equal(t, uint16(1), pat.Programs[0].ProgramNumber)
    assert.Equal(t, PID(0x100), pat.Programs[0].ProgramMapPID)
}

func TestParsePAT_InvalidCRC(t *testing.T) {
    pkt := createMockPATPacketWithBadCRC()
    _, err := pkt.ParsePAT()
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "CRC32 mismatch")
}
```

### 5️⃣ Проблеми з обробкою NIT (ProgramNumber == 0x0000)
```go
// ❌ Код обробляє NetworkPID, але не повертає його у зручному форматі:
if pat.Programs[i].ProgramNumber == 0x0000 {
    pat.Programs[i].NetworkPID = PID(...)  // ✅ Зберігає
}
// 🎯 Але: як дізнатися, чи це NIT запис, без перевірки ProgramNumber?
// ✅ Правильно: додати метод IsNetworkInfo() або типізувати записи:

type PATEntry struct {
    ProgramNumber uint16
    PID           PID
    Type          PATEntryType  // NetworkInfo або ProgramMap
}

type PATEntryType uint8
const (
    PATEntryNetworkInfo PATEntryType = iota
    PATEntryProgramMap
)
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **WebSocket-приймачем TS-фрагментів**:

### 🎯 Сценарій: ініціалізація каналу через PAT/PMT
```go
// У ChannelInitializer при підключенні нового CCTV потоку:
func (ci *ChannelInitializer) DiscoverStreams(pkt *mpeg2ts.Packet) error {
    // 🎯 Перевірка, що це PAT пакет
    if pkt.PID != 0x00 || !pkt.PayloadUnitStartIndicator {
        return nil  // Не PAT, ігноруємо
    }
    
    // 🎯 Парсинг PAT
    pat, err := pkt.ParsePAT()
    if err != nil {
        return fmt.Errorf("PAT parse failed: %w", err)
    }
    
    // 🎯 Пошук основної програми (зазвичай ProgramNumber == 1)
    var mainProgram *mpeg2ts.PATProgram
    for i := range pat.Programs {
        if pat.Programs[i].ProgramNumber == 1 {  // Або конфігурований номер
            mainProgram = &pat.Programs[i]
            break
        }
    }
    if mainProgram == nil {
        return fmt.Errorf("main program not found in PAT")
    }
    
    // 🎯 Збереження ProgramMapPID для подальшого парсингу PMT
    ci.pmtPID = mainProgram.ProgramMapPID
    ci.logger.Info("discovered main program", 
        "program_number", mainProgram.ProgramNumber,
        "pmt_pid", ci.pmtPID)
    
    return nil
}

// 🎯 Далі: фільтрувати пакети з ci.pmtPID → парсити PMT → отримати відео/аудіо PID
```

### 🎯 Сценарій: моніторинг змін у PAT (версіонування)
```go
// У PATMonitor для відстеження оновлень таблиці:
type PATMonitor struct {
    currentVersion uint8
    currentPrograms map[uint16]mpeg2ts.PID  // ProgramNumber → ProgramMapPID
    onChange func(old, new PAT)
}

func (m *PATMonitor) ProcessPacket(pkt *mpeg2ts.Packet) error {
    if pkt.PID != 0x00 {
        return nil
    }
    
    pat, err := pkt.ParsePAT()
    if err != nil {
        return fmt.Errorf("PAT parse failed: %w", err)
    }
    
    // 🎯 Перевірка версії: якщо змінилася → оновити стан
    if pat.Version != m.currentVersion || pat.CurrentNextIndicator {
        m.logger.Info("PAT version updated", 
            "old_version", m.currentVersion,
            "new_version", pat.Version,
            "current_next", pat.CurrentNextIndicator)
        
        oldPrograms := m.currentPrograms
        m.currentPrograms = make(map[uint16]mpeg2ts.PID)
        for _, prog := range pat.Programs {
            if prog.ProgramNumber != 0x0000 {  // Ігноруємо NIT
                m.currentPrograms[prog.ProgramNumber] = prog.ProgramMapPID
            }
        }
        m.currentVersion = pat.Version
        
        // 🎯 Сповістити про зміни (напр., для перепідключення до нових потоків)
        if m.onChange != nil {
            m.onChange(PAT{Programs: oldPrograms}, pat)
        }
    }
    
    return nil
}
```

### 🎯 Сценарій: валідація цілісності PAT через CRC
```go
// У StreamValidator для перевірки якості метаданих:
func (v *StreamValidator) ValidatePAT(pkt *mpeg2ts.Packet) error {
    if pkt.PID != 0x00 {
        return nil  // Не PAT
    }
    
    pat, err := pkt.ParsePAT()
    if err != nil {
        v.metrics.PATParseErrors.Inc()
        return fmt.Errorf("PAT validation failed: %w", err)
    }
    
    // 🎯 Додаткові перевірки якості
    if len(pat.Programs) == 0 {
        v.metrics.PATEmptyPrograms.Inc()
        v.logger.Warn("PAT contains no programs", "ts_id", pat.TransportStreamID)
    }
    
    for _, prog := range pat.Programs {
        if prog.ProgramNumber != 0x0000 && prog.ProgramMapPID == 0 {
            v.metrics.PATInvalidPID.Inc()
            v.logger.Warn("program with invalid PMT PID", 
                "program", prog.ProgramNumber, "pid", prog.ProgramMapPID)
        }
    }
    
    return nil
}
```

---

## 🧪 Приклад: рефакторинг `ParsePAT()` з кращою безпекою

```go
// ✅ Безпечний парсинг з повною валідацією:
func (p *Packet) ParsePAT() (PAT, error) {
    // 🎯 Перевірка, що це PAT пакет
    if p.PID != 0x00 {
        return PAT{}, fmt.Errorf("not a PAT packet: PID=0x%X", p.PID)
    }
    if !p.PayloadUnitStartIndicator {
        return PAT{}, fmt.Errorf("PAT must start at payload boundary: PUSI=0")
    }
    
    payload, err := p.GetPayload()
    if err != nil {
        return PAT{}, fmt.Errorf("failed to get payload: %w", err)
    }
    if len(payload) < 9 {  // Мінімум: pointer(1) + заголовок(8)
        return PAT{}, fmt.Errorf("payload too short for PAT: %d < 9", len(payload))
    }
    
    pat := PAT{}
    
    // 🎯 Обробка pointer_field
    pointer := payload[0]
    tableStart := 1 + int(pointer)
    if tableStart >= len(payload) {
        return PAT{}, fmt.Errorf("pointer_field %d exceeds payload length %d", 
            pointer, len(payload))
    }
    pat.Pointer = pointer
    
    // 🎯 Парсинг заголовка секції
    tableData := payload[tableStart:]
    if len(tableData) < 8 {
        return PAT{}, fmt.Errorf("table data too short: %d < 8", len(tableData))
    }
    
    pat.TableID = tableData[0]
    if pat.TableID != 0x00 {
        return PAT{}, fmt.Errorf("invalid TableID for PAT: 0x%02X != 0x00", pat.TableID)
    }
    
    flags := tableData[1]
    pat.SectionSyntaxIndicator = (flags >> 7) & 0x01 == 1
    if !pat.SectionSyntaxIndicator {
        return PAT{}, fmt.Errorf("section_syntax_indicator must be 1 for PAT")
    }
    if (flags >> 6) & 0x01 == 1 {
        return PAT{}, fmt.Errorf("reserved bit must be 0, got 1")
    }
    
    pat.SectionLength = uint16(flags&0x0F)<<8 | uint16(tableData[2])
    if pat.SectionLength < 5+4 {  // 5 байт заголовка + 4 байти CRC
        return PAT{}, fmt.Errorf("SectionLength too short: %d < 9", pat.SectionLength)
    }
    if pat.SectionLength > 1021 {
        return PAT{}, fmt.Errorf("SectionLength too long: %d > 1021", pat.SectionLength)
    }
    
    // 🎯 Перевірка, що таблиця вміщується у payload
    expectedEnd := tableStart + int(pat.SectionLength) + 4  // +4 для CRC
    if expectedEnd > len(payload) {
        return PAT{}, fmt.Errorf("PAT section extends beyond payload: %d > %d", 
            expectedEnd, len(payload))
    }
    
    // 🎯 Парсинг основних полів
    pat.TransportStreamID = uint16(tableData[3])<<8 | uint16(tableData[4])
    pat.Version = (tableData[5] >> 1) & 0x1F
    if pat.Version > 31 {
        return PAT{}, fmt.Errorf("invalid version: %d > 31", pat.Version)
    }
    pat.CurrentNextIndicator = (tableData[5] & 0x01) == 0x01
    pat.SectionNumber = tableData[6]
    pat.LastSectionNumber = tableData[7]
    if pat.SectionNumber > pat.LastSectionNumber {
        return PAT{}, fmt.Errorf("SectionNumber %d > LastSectionNumber %d", 
            pat.SectionNumber, pat.LastSectionNumber)
    }
    
    // 🎯 Парсинг циклу програм
    programCount := (pat.SectionLength - 5 - 4) / 4  // -5 заголовок, -4 CRC
    pat.Programs = make([]PATProgram, programCount)
    
    for i := uint16(0); i < programCount; i++ {
        base := 8 + int(i)*4  // 8 байт заголовка секції
        if base+4 > len(tableData) {
            return PAT{}, fmt.Errorf("program %d extends beyond table data", i)
        }
        
        programNumber := uint16(tableData[base])<<8 | uint16(tableData[base+1])
        pid := PID((uint16(tableData[base+2]&0x1F) << 8) | uint16(tableData[base+3]))
        
        if pid > 0x1FFF {
            return PAT{}, fmt.Errorf("invalid PID in program %d: 0x%X > 0x1FFF", i, pid)
        }
        
        pat.Programs[i] = PATProgram{
            ProgramNumber: programNumber,
            PID:           pid,  // ✅ Єдине поле, інтерпретація залежить від ProgramNumber
        }
    }
    
    // 🎯 Парсинг та перевірка CRC-32
    crcStart := tableStart + 8 + int(programCount)*4  // Після циклу програм
    storedCRC := binary.BigEndian.Uint32(payload[crcStart : crcStart+4])
    
    crcData := payload[tableStart : crcStart]  // Дані для CRC розрахунку
    computedCRC := calculateCRC(crcData)
    
    if storedCRC != computedCRC {
        return PAT{}, fmt.Errorf("CRC32 mismatch: stored=0x%08X, computed=0x%08X", 
            storedCRC, computedCRC)
    }
    pat.CRC32 = storedCRC
    
    return pat, nil
}
```

---

## 📋 Специфікація MPEG-2 TS — критичні вимоги до PAT

```
✅ PID: завжди 0x00 для PAT пакетів
✅ PayloadUnitStartIndicator: завжди 1 для початку нової секції
✅ TableID: завжди 0x00 для PAT
✅ SectionSyntaxIndicator: завжди 1 для PAT
✅ SectionLength: 12 біт, максимум 1021 (після цього поля до кінця секції, не включаючи CRC)
✅ TransportStreamID: 16-бітний унікальний ідентифікатор потоку
✅ Version_number: 5 біт, зростає при зміні вмісту таблиці (модуль 32)
✅ Current_next_indicator: 1 = застосувати зараз, 0 = наступна версія
✅ ProgramNumber: 16 біт, 0x0000 = NIT, 0x0001-0xFFFF = програми
✅ PID у програмі: 13 біт (0x0000-0x1FFF), NetworkPID або ProgramMapPID залежно від ProgramNumber
✅ CRC-32: polynomial 0x04C11DB7, initial 0xFFFFFFFF, final XOR 0xFFFFFFFF, MSB-first
✅ pointer_field: 0-255, вказує зсув до початку таблиці від payload[1]
✅ Multi-section PAT: якщо SectionLength > доступного місця в одному пакеті → розбиття на секції
```

---

## 🎯 Висновок

Цей `ParsePAT()` — **функціональна реалізація** парсингу PAT, але має критичні проблеми:

✅ Правильна загальна структура парсингу за специфікацією  
✅ Валідація CRC-32 для цілісності даних  
✅ Обробка pointer_field та циклу програм

**Критичні виправлення перед продакшеном**:

1. ✅ **Виправити помилку у збірці CRC32** (повторення індексу +1)
2. ✅ **Виправити діапазон для CRC розрахунку** (виключати сам CRC)
3. ✅ **Додати перевірку меж** перед доступом до `payload[...]`
4. ✅ **Виправити hardcoded "9"** → використовувати `tableStart + 8`
5. ✅ **Виправити маску для PID** → прибрати зайве `&0x1FFF` на байті
6. ✅ **Додати валідацію вхідного пакету** (PID==0x00, PUSI==1)
7. ✅ **Замінити `uint` на `uint32` для `CRC32` поля**
8. ✅ **Додати тести** для валідних/невалідних PAT з відомими векторами

**Приклад інтеграції у ваш pipeline**:
```go
// 🎯 PATProcessor для вашого WebSocket-сервера:
type PATProcessor struct {
    channelID string
    onProgramDiscovered func(programNumber uint16, pmtPID mpeg2ts.PID)
    logger *log.Logger
}

func (p *PATProcessor) ProcessPacket(pkt *mpeg2ts.Packet) error {
    if pkt.PID != 0x00 {
        return nil  // Не PAT
    }
    
    pat, err := pkt.ParsePAT()
    if err != nil {
        p.logger.Warn("PAT parse failed", 
            "channel", p.channelID, 
            "error", err)
        return nil  // Не зупиняти потік через помилку метаданих
    }
    
    for _, prog := range pat.Programs {
        if prog.ProgramNumber == 0x0000 {
            continue  // Ігноруємо NIT
        }
        if p.onProgramDiscovered != nil {
            p.onProgramDiscovered(prog.ProgramNumber, prog.PID)
        }
        p.logger.Info("discovered program", 
            "channel", p.channelID,
            "program", prog.ProgramNumber,
            "pmt_pid", prog.PID)
    }
    
    return nil
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом `ParsePAT()` з повною валідацією та безпечним доступом до слайсів?
- 🧠 Інтеграцією PAT-моніторингу для відстеження змін у конфігурації каналів?
- 🧪 Написанням тестів з відомими векторами з специфікації та fuzz-тестів для країв?

Чекаю на ваші питання! 🛠️📡🎬