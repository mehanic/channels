# 🔍 Глибокий розбір: базові типи `mpeg2ts` — ядро парсера MPEG-2 Transport Stream

Цей код визначає **фундаментальні структури даних** для роботи з MPEG-2 Transport Stream у Go. Розберемо архітектурно, з критичним аналізом та практичними рекомендаціями.

---

## 📦 Архітектура модуля: ієрархія типів

```
┌─────────────────────────────────────┐
│ Пакет: mpeg2ts (core types)         │
│ Призначення: представлення TS даних │
│ Стандарт: ISO/IEC 13818-1           │
├─────────────────────────────────────┤
│ 🔹 Константи:                        │
│    • PacketSizeDefault = 188        │
│    • PacketSizeWithFEC = 204        │
│                                      │
│ 🔹 Основні типи:                     │
│    • MPEG2TS — контейнер потоку     │
│    • PacketList — колекція пакетів  │
│    • Packet — один TS пакет         │
│    • AdaptationField — метадані     │
│    • ProgramClockReference — PCR    │
│                                      │
│ 🔹 Допоміжні:                        │
│    • TransportPrivateData           │
│    • StreamCheckResult              │
└─────────────────────────────────────┘
```

### 🎯 Контекст: структура MPEG-2 TS пакету (188 байт)
```
📦 TS Packet Layout:
├─ Заголовок (4 байти, ОБОВ'ЯЗКОВИЙ)
│  ├─ [0] sync_byte = 0x47 (завжди!)
│  ├─ [1] flags: [TEI:1][PUSI:1][TP:1][PID:5]
│  ├─ [2] PID[7:0] (разом 13 біт для PID)
│  └─ [3] [TSC:2][AFC:2][continuity:4]
│
├─ Adaptation Field (0-183 байти, ОПЦІЙНО)
│  ├─ [4] length (кількість наступних байт)
│  ├─ [5] flags: [DI:1][RAI:1][ESPI:1][PCR:1][OPCR:1][SP:1][TPD:1][EF:1]
│  ├─ PCR/OPCR (6 байт кожен, якщо відповідні прапорці)
│  ├─ SpliceCountdown, PrivateData, Extension (опціонально)
│  └─ Stuffing (0xFF байти для заповнення)
│
└─ Payload (решта байт, ОПЦІЙНО)
   • Якщо AFC=1: тільки payload
   • Якщо AFC=2: тільки adaptation field  
   • Якщо AFC=3: adaptation field + payload
```

---

## 🔬 Детальний розбір ключових типів

### 1️⃣ Константи розмірів пакетів

```go
const PacketSizeDefault = 188      // ✅ Стандартний TS пакет
const PacketSizeWithFEC = 204      // ✅ TS + 16 байт Reed-Solomon FEC
```

#### ✅ Правильні аспекти
```go
// ✅ Чіткі імена констант з документацією
// ✅ Відповідність специфікації:
// • 188 байт: базовий розмір за ISO/IEC 13818-1
// • 204 байт: 188 + 16 байт FEC за DVB стандартом

// ✅ Використання у коді:
func LoadStandardTS(fname string) (*MPEG2TS, error) {
    return loadFile(fname, PacketSizeDefault)  // ✅ Читабельно
}
```

#### ⚠️ Потенційні покращення
```go
// ❌ Відсутність валідації chunkSize при ініціалізації:
// • Користувач може передати 100 → невалідний розмір
// ✅ Додати константу-валідатор або функцію перевірки:
func IsValidPacketSize(size int) bool {
    return size == PacketSizeDefault || size == PacketSizeWithFEC
}

// ✅ Або додати метод для отримання опису:
func (size int) PacketSizeDescription() string {
    switch size {
    case PacketSizeDefault:
        return "Standard TS (188 bytes)"
    case PacketSizeWithFEC:
        return "TS with FEC (204 bytes)"
    default:
        return "Invalid size"
    }
}
```

---

### 2️⃣ `MPEG2TS` — контейнер потоку

```go
type MPEG2TS struct {
    PacketList  // ✅ Вбудовування для композиції
    chunkSize int  // ⚠️ Не експортоване поле
}
```

#### ⚠️ Проблеми дизайну
```go
// ❌ Не експортоване поле chunkSize:
// • Неможливо отримати розмір пакету ззовні пакету
// • Ускладнює тестування та відладку
// ✅ Правильно: або експортувати, або додати геттер:
func (m *MPEG2TS) ChunkSize() int {
    return m.chunkSize
}

// ❌ Вбудовування без явного імені може заплутати:
m.PacketList.AddPacket(p)  // ✅ Явно
// vs
m.AddPacket(p)  // ❌ Неочевидно, що метод з PacketList
// ✅ Правильно: або використовувати явне ім'я, або документацію:
type MPEG2TS struct {
    packets PacketList  // ✅ Явне ім'я поля
    chunkSize int
}
// Доступ: m.packets.AddPacket(p) — зрозуміліше
```

---

### 3️⃣ `PacketList` — колекція пакетів

```go
type PacketList struct {
    packets   []Packet      // ✅ Слайс пакетів
    mutex     *sync.Mutex   // ✅ Покажчик на м'ютекс
    chunkSize int           // ⚠️ Не експортоване поле
}
```

#### ⚠️ Проблеми дизайну
```go
// ❌ Покажчик на sync.Mutex замість значення:
mutex *sync.Mutex  // ⚠️ Нестандартно, може заплутати
// • sync.Mutex не можна копіювати, тому покажчик — технічно правильно
// • Але: значення простіше та безпечніше для ініціалізації
// ✅ Правильно: або значення, або явна ініціалізація:
type PacketList struct {
    packets []Packet
    mu      sync.Mutex  // ✅ Значення (ініціалізується автоматично)
    chunkSize int
}
// Використання: pl.mu.Lock() замість pl.mutex.Lock()

// ❌ Не експортоване поле packets:
// • Неможливо ітерувати пакети ззовні без методу-гетера
// ✅ Правильно: додати метод для безпечного доступу:
func (pl *PacketList) All() []Packet {
    pl.mu.Lock()
    defer pl.mu.Unlock()
    // 🎯 Повертаємо копію для безпеки
    result := make([]Packet, len(pl.packets))
    copy(result, pl.packets)
    return result
}
```

---

### 4️⃣ `Packet` — основна одиниця даних

```go
type Packet struct {
    Index                      int     // ✅ Порядковий номер пакету
    Data                       []byte  // ✅ Сирі байти пакету
    SyncByte                   byte    // ✅ Завжди 0x47
    PID                        PID     // ✅ 13-бітний ідентифікатор
    TransportScrambleControl   byte    // ⚠️ 2 біти, але byte = 8 біт
    AdaptationFieldControl     byte    // ⚠️ 2 біти, але byte = 8 біт
    TransportErrorIndicator    bool    // ✅ 1 біт як bool
    PayloadUnitStartIndicator  bool    // ✅ 1 біт як bool
    TransportPriorityIndicator bool    // ✅ 1 біт як bool
    ContinuityCheckIndex       byte    // ⚠️ 4 біти, але byte = 8 біт
    AdaptationField            AdaptationField  // ✅ Вкладена структура
    isHeaderParsed             bool    // ❌ Не експортоване поле стану
}
```

#### ⚠️ Проблеми типів та доступу
| Поле | У коді | У специфікації | Наслідок |
|------|--------|---------------|----------|
| `TransportScrambleControl` | `byte` | 2 біти | ⚠️ Зайва пам'ять, можливі невалідні значення 2-3 |
| `AdaptationFieldControl` | `byte` | 2 біти | ⚠️ Може прийняти 0 (зарезервовано) |
| `ContinuityCheckIndex` | `byte` | 4 біти | ⚠️ Може прийняти значення 16-255 |
| `isHeaderParsed` | `bool` (не експорт.) | — | ❌ Неможливо перевірити стан ззовні |

#### ✅ Правильні аспекти
```go
// ✅ Використання bool для 1-бітних прапорців:
TransportErrorIndicator bool  // ✅ Читабельно та безпечно
// • Неможливо присвоїти небулеве значення
// • Автоматична ініціалізація false

// ✅ PID як окремий тип:
PID PID  // ✅ Тип-обгортка для uint16 з можливістю додавання методів
// • Дозволяє додати валідацію: func (p PID) IsValid() bool
// • Читабельність: PID(0x100) замість uint16(256)
```

#### ⚠️ Потенційні покращення
```go
// ✅ Додати методи валідації для бітових полів:
func (p *Packet) validateHeader() error {
    if p.SyncByte != 0x47 {
        return fmt.Errorf("invalid sync byte: 0x%02X", p.SyncByte)
    }
    if p.AdaptationFieldControl == 0 {
        return fmt.Errorf("reserved adaptation_field_control value 0")
    }
    if p.ContinuityCheckIndex > 15 {
        return fmt.Errorf("continuity_counter exceeds 4 bits: %d", p.ContinuityCheckIndex)
    }
    return nil
}

// ✅ Додати публічний метод для перевірки стану парсингу:
func (p *Packet) IsHeaderParsed() bool {
    return p.isHeaderParsed
}
// Або: зробити поле експортованим, якщо це внутрішній стан бібліотеки

// ✅ Додати методи для зручного доступу до даних:
func (p *Packet) HasPayload() bool {
    return p.AdaptationFieldControl == 1 || p.AdaptationFieldControl == 3
}

func (p *Packet) HasAdaptationField() bool {
    return p.AdaptationFieldControl == 2 || p.AdaptationFieldControl == 3
}

func (p *Packet) GetPayload() ([]byte, error) {
    if !p.IsHeaderParsed() {
        return nil, fmt.Errorf("header not parsed")
    }
    if !p.HasPayload() {
        return nil, nil  // Немає payload
    }
    offset := 4  // Заголовок
    if p.HasAdaptationField() {
        offset += 1 + int(p.AdaptationField.Length)  // length byte + data
    }
    if offset >= len(p.Data) {
        return nil, fmt.Errorf("payload offset exceeds packet size")
    }
    return p.Data[offset:], nil
}
```

---

### 5️⃣ `AdaptationField` — метадані пакету

```go
type AdaptationField struct {
    Length                        byte  // ✅ 8 біт, 0-183
    DiscontinuityIndicator        bool  // ✅ 1 біт
    RandomAccessIndicator         bool  // ✅ 1 біт (важливо для seek)
    ESPriorityIndicator           bool  // ✅ 1 біт
    PCRFlag                       bool  // ✅ 1 біт (критично для синхронізації)
    OPCRFlag                      bool  // ✅ 1 біт
    SplicingPointFlag             bool  // ✅ 1 біт
    TransportPrivateDataFlag      bool  // ✅ 1 біт
    ExtensionFlag                 bool  // ✅ 1 біт
    ProgramClockReference         ProgramClockReference  // ✅ Вкладена структура
    OriginalProgramClockReference ProgramClockReference  // ✅ Для сплайсингу
    SpliceCountdown               byte  // ⚠️ 8 біт, але тільки якщо SplicingPointFlag
    TransportPrivateData          TransportPrivateData   // ✅ Вкладена структура
    ExtensionLength               byte  // ⚠️ Не використовується?
    Stuffing                      []byte  // ✅ Заповнення 0xFF
}
```

#### ⚠️ Проблеми структури
```go
// ❌ Поля, що залежать від прапорців, завжди присутні:
// • SpliceCountdown існує, навіть якщо SplicingPointFlag=false
// • ProgramClockReference існує, навіть якщо PCRFlag=false
// → Зайва пам'ять для пакетів без цих полів

// ✅ Правильно: використовувати покажчики для опціональних полів:
type AdaptationField struct {
    Length byte
    // ... прапорці ...
    PCRFlag bool
    ProgramClockReference *ProgramClockReference  // ✅ nil, якщо PCRFlag=false
    // ...
}

// ✅ Або додати методи для безпечного доступу:
func (af *AdaptationField) GetPCR() (*ProgramClockReference, bool) {
    if !af.PCRFlag {
        return nil, false
    }
    return &af.ProgramClockReference, true
}

// ❌ ExtensionLength не використовується:
// • Якщо ExtensionFlag=true, парсинг розширення не реалізовано
// • Поле залишається 0, що вводить в оману
// ✅ Правильно: або реалізувати парсинг, або видалити поле, або додати коментар:
// ExtensionLength byte  // Reserved for future use (ExtensionFlag parsing not implemented)
```

---

### 6️⃣ `ProgramClockReference` — синхронізація часу

```go
type ProgramClockReference struct {
    Base      uint64  // ✅ 33 біти @ 90 kHz
    Extension uint16  // ✅ 9 біт @ 27 MHz
}
```

#### ✅ Правильні аспекти
```go
// ✅ Розділення Base та Extension:
// • Base: 33 біти для грубої синхронізації (90 kHz = 11.111... мкс)
// • Extension: 9 біт для точної синхронізації (27 MHz = 37.037... нс)
// • Разом: 42 біти точності для A/V синхронізації

// ✅ Достатні типи:
// • uint64 для 33-бітного Base → без втрати точності
// • uint16 для 9-бітного Extension → без втрати точності
```

#### ⚠️ Потенційні покращення
```go
// ✅ Додати методи конвертації у time.Duration:
func (pcr *ProgramClockReference) Duration() time.Duration {
    // 📋 PCR частота: 27 MHz = 1 tick = 37.037... нс
    // • Base: 90 kHz → 1 tick = 11.111... мкс
    // • Extension: 27 MHz → 1 tick = 37.037... нс
    baseNs := pcr.Base * 11111  // 90 kHz → нс (приблизно)
    extNs := uint64(pcr.Extension) * 37  // 27 MHz → нс (приблизно)
    return time.Duration(baseNs + extNs)
}

// ✅ Додати метод порівняння для виявлення drift:
func (pcr *ProgramClockReference) Diff(other *ProgramClockReference) time.Duration {
    // 🎯 Обчислити різницю між двома PCR у наносекундах
    // 🎯 Повернути додатне/від'ємне значення для корекції
}

// ⚠️ Відсутність валідації діапазону:
// • Base має бути ≤ 2^33-1, Extension ≤ 2^9-1
// ✅ Додати метод валідації:
func (pcr *ProgramClockReference) IsValid() bool {
    return pcr.Base <= 0x1FFFFFFFF && pcr.Extension <= 0x1FF
}
```

---

### 7️⃣ `StreamCheckResult` — результат перевірки цілісності

```go
type StreamCheckResult struct {
    DropCount int
    DropList  []struct {
        Description string
        Index       int
    }
}
```

#### ⚠️ Проблеми дизайну
```go
// ❌ Анонімний struct у слайсі:
DropList []struct { Description string; Index int }
// • Неможливо розширити без зміни сигнатури
// • Не інтуїтивно для користувачів бібліотеки
// • Неможливо додати методи

// ✅ Правильно: окремий іменований тип:
type DropEntry struct {
    Description string  // Опис помилки
    PacketIndex int     // Індекс пакету у потоці
    PID         PID     // 🎯 Додати PID для зручності дебагу
    ExpectedCC  byte    // 🎯 Очікуваний continuity_counter
    ActualCC    byte    // 🎯 Фактичний continuity_counter
}

type StreamCheckResult struct {
    DropCount int
    DropList  []DropEntry  // ✅ Іменований тип
}

// ✅ Додати методи для зручного доступу:
func (scr StreamCheckResult) HasErrors() bool {
    return scr.DropCount > 0
}

func (scr StreamCheckResult) ErrorSummary() string {
    if scr.DropCount == 0 {
        return "OK"
    }
    return fmt.Sprintf("%d continuity errors detected", scr.DropCount)
}
```

---

## ⚠️ Загальні проблеми модуля

### 1️⃣ Відсутність методів валідації
```go
// ❌ Типи не мають методів для самоперевірки:
// • Чи валідний синхробайт?
// • Чи PID у діапазоні 0x0000-0x1FFF?
// • Чи continuity_counter ∈ [0,15]?

// ✅ Додати інтерфейс Valider:
type Valider interface {
    Validate() error
}

// Реалізація для Packet:
func (p *Packet) Validate() error {
    if p.SyncByte != 0x47 {
        return fmt.Errorf("invalid sync byte: 0x%02X", p.SyncByte)
    }
    if p.PID > 0x1FFF {
        return fmt.Errorf("invalid PID: 0x%X > 0x1FFF", p.PID)
    }
    if p.ContinuityCheckIndex > 15 {
        return fmt.Errorf("continuity_counter exceeds 4 bits: %d", p.ContinuityCheckIndex)
    }
    if p.AdaptationFieldControl == 0 {
        return fmt.Errorf("reserved adaptation_field_control value 0")
    }
    return nil
}
```

### 2️⃣ Пам'ять та продуктивність
```go
// ❌ Кожен Packet містить []byte Data (188 байт) + окремі поля:
// • 188 байт Data + ~50 байт полів = ~238 байт на пакет
// • При 1000 пакетів/сек → 238 KB/s тільки на структури
// ✅ Оптимізації:
// • Використовувати sync.Pool для повторного використання Packet
// • Зберігати тільки посилання на буфер замість копіювання даних
// • Видалити дублюючі поля (напр. SyncByte, якщо завжди 0x47)

// Приклад sync.Pool:
var packetPool = sync.Pool{
    New: func() interface{} {
        return &Packet{Data: make([]byte, PacketSizeDefault)}
    },
}

func GetPacket() *Packet {
    return packetPool.Get().(*Packet)
}

func PutPacket(p *Packet) {
    // 🎯 Скинути стан перед поверненням у pool
    p.isHeaderParsed = false
    p.AdaptationField = AdaptationField{}
    packetPool.Put(p)
}
```

### 3️⃣ Документація та читабельність
```go
// ❌ Відсутність коментарів для бітових полів:
TransportScrambleControl byte  // ❌ Що означають значення 0-3?
// ✅ Правильно: додати документацію за специфікацією:
// TransportScrambleControl: 2 біти, значення:
//   00 = not scrambled
//   01, 10, 11 = user-defined scrambling modes
TransportScrambleControl byte

// ❌ Неочевидні взаємозв'язки полів:
// • Як AdaptationFieldControl впливає на наявність payload?
// ✅ Правильно: додати методи-помічники:
// HasPayload() повертає true, якщо пакет містить корисне навантаження
func (p *Packet) HasPayload() bool {
    return p.AdaptationFieldControl == 1 || p.AdaptationFieldControl == 3
}
```

### 4️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу для типів
// • Неможливо перевірити коректність валідації
// • Неможливо покрити edge cases (невалідні PID, пошкоджені заголовки)

// ✅ Додати мінімальні тести:
func TestPacket_Validate_Valid(t *testing.T) {
    pkt := &Packet{
        SyncByte: 0x47,
        PID: 0x100,
        ContinuityCheckIndex: 5,
        AdaptationFieldControl: 1,
    }
    err := pkt.Validate()
    assert.NoError(t, err)
}

func TestPacket_Validate_InvalidSyncByte(t *testing.T) {
    pkt := &Packet{SyncByte: 0x48}  // ❌ Не 0x47!
    err := pkt.Validate()
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "invalid sync byte")
}

func TestProgramClockReference_Duration(t *testing.T) {
    pcr := &ProgramClockReference{
        Base: 90000,  // 1 секунда @ 90 kHz
        Extension: 0,
    }
    dur := pcr.Duration()
    assert.Equal(t, time.Second, dur)
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **WebSocket-приймачем TS-фрагментів**:

### 🎯 Сценарій: валідація вхідних пакетів
```go
// У TSValidator для перевірки якості потоку:
type TSValidator struct {
    lastContinuity map[mpeg2ts.PID]byte
    logger         *log.Logger
}

func (v *TSValidator) ValidatePacket(pkt *mpeg2ts.Packet) error {
    // 🎯 Базова валідація структури
    if err := pkt.Validate(); err != nil {
        return fmt.Errorf("packet validation failed: %w", err)
    }
    
    // 🎯 Перевірка скремблінгу
    if pkt.TransportScrambleControl != 0 {
        return fmt.Errorf("scrambled packet (TSC=%d), decryption not supported", 
            pkt.TransportScrambleControl)
    }
    
    // 🎯 Continuity check
    if pkt.HasPayload() {
        last, exists := v.lastContinuity[pkt.PID]
        if !exists {
            v.lastContinuity[pkt.PID] = pkt.ContinuityCheckIndex
        } else {
            expected := (last + 1) % 16
            if expected != pkt.ContinuityCheckIndex {
                v.logger.Warn("continuity error", 
                    "pid", pkt.PID,
                    "expected", expected,
                    "actual", pkt.ContinuityCheckIndex)
                // Синхронізувати та продовжити
                v.lastContinuity[pkt.PID] = pkt.ContinuityCheckIndex
            } else {
                v.lastContinuity[pkt.PID] = pkt.ContinuityCheckIndex
            }
        }
    }
    
    return nil
}
```

### 🎯 Сценарій: витяг PCR для A/V синхронізації
```go
// У A/V Sync модулі для корекції drift:
func extractPCR(pkt *mpeg2ts.Packet) (*time.Time, error) {
    if !pkt.AdaptationField.PCRFlag {
        return nil, nil  // Немає PCR у цьому пакеті
    }
    
    pcr := &pkt.AdaptationField.ProgramClockReference
    // 🎯 Конвертація PCR (27 MHz clock) у time.Time
    // PCR base: 33 біти @ 90 kHz → секунди
    baseSeconds := float64(pcr.Base) / 90000.0
    // PCR extension: 9 біт @ 27 MHz → суб-мілісекунди
    extensionSeconds := float64(pcr.Extension) / 27000000.0
    pcrTime := time.Unix(0, int64((baseSeconds+extensionSeconds)*1e9))
    
    return &pcrTime, nil
}

// Використання у синхронізаторі:
func (s *AVSync) CorrectDrift(pkt *mpeg2ts.Packet, serverTime time.Time) error {
    pcrTime, err := extractPCR(pkt)
    if err != nil || pcrTime == nil {
        return nil  // Немає PCR → не можна корегувати
    }
    
    drift := serverTime.Sub(*pcrTime)
    if drift.Abs() > s.maxAllowedDrift {
        s.logger.Warn("significant A/V drift detected", 
            "drift_ms", drift.Milliseconds(),
            "pid", pkt.PID)
        // 🎯 Тут можна скоригувати PTS/DTS у payload
    }
    
    return nil
}
```

### 🎯 Сценарій: моніторинг якості транспорту
```go
// У monitoring.Monitor для агрегації метрик:
type TransportQualityMetrics struct {
    TotalPackets      int64
    ContinuityErrors  int64
    SyncErrors        int64
    DropRate          float64  // %
    ActivePIDs        int      // Кількість активних PID
    AvgPCRDrift       time.Duration
}

func (m *Monitor) AnalyzeTransportQuality(packets []*mpeg2ts.Packet) TransportQualityMetrics {
    metrics := TransportQualityMetrics{
        TotalPackets: int64(len(packets)),
    }
    
    pidSet := make(map[mpeg2ts.PID]bool)
    var totalDrift time.Duration
    driftCount := 0
    
    for _, pkt := range packets {
        pidSet[pkt.PID] = true
        
        // 🎯 Підрахунок помилок
        if pkt.SyncByte != 0x47 {
            metrics.SyncErrors++
        }
        if pkt.AdaptationFieldControl == 0 {
            metrics.ContinuityErrors++  // Reserved value
        }
        
        // 🎯 PCR drift аналіз
        if pkt.AdaptationField.PCRFlag {
            // 🎯 Порівняння з попереднім PCR того ж PID
            // (реалізація залежить від вашої логіки)
        }
    }
    
    metrics.ActivePIDs = len(pidSet)
    if metrics.TotalPackets > 0 {
        metrics.DropRate = float64(metrics.ContinuityErrors) / float64(metrics.TotalPackets) * 100
    }
    
    return metrics
}
```

---

## 🧪 Приклад: рефакторинг типів з кращою безпекою

```go
// ✅ Оптимізовані типи з валідацією та методами:

// 🎯 PID тип з методами валідації
type PID uint16

const (
    PIDMax = 0x1FFF  // 13 біт максимум
)

func (p PID) IsValid() bool {
    return p <= PIDMax
}

func (p PID) IsReserved() bool {
    return p >= 0x0003 && p <= 0x000F
}

func (p PID) IsPSI() bool {
    return p <= 0x0002  // PAT/CAT/TSDT
}

func (p PID) String() string {
    switch p {
    case 0x0000:
        return "PAT"
    case 0x0001:
        return "CAT"
    case 0x1FFF:
        return "Null"
    default:
        return fmt.Sprintf("0x%04X", uint16(p))
    }
}

// 🎯 Packet з методами валідації та доступу
type Packet struct {
    Index                      int
    Data                       []byte
    SyncByte                   byte
    PID                        PID
    TransportScrambleControl   uint8  // ✅ Явний тип для 2 біт
    AdaptationFieldControl     uint8  // ✅ Явний тип для 2 біт
    TransportErrorIndicator    bool
    PayloadUnitStartIndicator  bool
    TransportPriorityIndicator bool
    ContinuityCheckIndex       uint8  // ✅ Явний тип для 4 біт
    AdaptationField            AdaptationField
    isHeaderParsed             bool
}

// ✅ Методи валідації
func (p *Packet) Validate() error {
    if p.SyncByte != 0x47 {
        return fmt.Errorf("invalid sync byte: 0x%02X", p.SyncByte)
    }
    if !p.PID.IsValid() {
        return fmt.Errorf("invalid PID: 0x%X", p.PID)
    }
    if p.ContinuityCheckIndex > 15 {
        return fmt.Errorf("continuity_counter exceeds 4 bits: %d", p.ContinuityCheckIndex)
    }
    if p.AdaptationFieldControl == 0 {
        return fmt.Errorf("reserved adaptation_field_control value 0")
    }
    if p.TransportScrambleControl > 3 {
        return fmt.Errorf("invalid transport_scramble_control: %d", p.TransportScrambleControl)
    }
    return nil
}

// ✅ Методи доступу до даних
func (p *Packet) HasPayload() bool {
    return p.AdaptationFieldControl == 1 || p.AdaptationFieldControl == 3
}

func (p *Packet) HasAdaptationField() bool {
    return p.AdaptationFieldControl == 2 || p.AdaptationFieldControl == 3
}

func (p *Packet) GetPayload() ([]byte, error) {
    if !p.isHeaderParsed {
        return nil, fmt.Errorf("header not parsed")
    }
    if !p.HasPayload() {
        return nil, nil
    }
    
    offset := 4  // Заголовок
    if p.HasAdaptationField() {
        if int(p.AdaptationField.Length) > len(p.Data)-5 {
            return nil, fmt.Errorf("adaptation field too long")
        }
        offset += 1 + int(p.AdaptationField.Length)
    }
    
    if offset >= len(p.Data) {
        return nil, fmt.Errorf("payload offset exceeds packet size")
    }
    
    return p.Data[offset:], nil
}

// 🎯 AdaptationField з покажчиками для опціональних полів
type AdaptationField struct {
    Length uint8
    DiscontinuityIndicator   bool
    RandomAccessIndicator    bool
    ESPriorityIndicator      bool
    PCRFlag                  bool
    OPCRFlag                 bool
    SplicingPointFlag        bool
    TransportPrivateDataFlag bool
    ExtensionFlag            bool
    
    // ✅ Опціональні поля як покажчики
    ProgramClockReference         *ProgramClockReference
    OriginalProgramClockReference *ProgramClockReference
    SpliceCountdown               *uint8
    TransportPrivateData          *TransportPrivateData
    // Extension не реалізовано → коментар
    // Extension *AdaptationFieldExtension  // Not implemented per spec
    
    Stuffing []byte  // 0xFF байти заповнення
}

// ✅ Методи безпечного доступу
func (af *AdaptationField) GetPCR() (*ProgramClockReference, bool) {
    if !af.PCRFlag || af.ProgramClockReference == nil {
        return nil, false
    }
    return af.ProgramClockReference, true
}

// 🎯 StreamCheckResult з іменованим типом
type DropEntry struct {
    Description string
    PacketIndex int
    PID         PID
    ExpectedCC  uint8
    ActualCC    uint8
}

type StreamCheckResult struct {
    DropCount int
    DropList  []DropEntry
}

func (scr StreamCheckResult) HasErrors() bool {
    return scr.DropCount > 0
}

func (scr StreamCheckResult) ErrorSummary() string {
    if scr.DropCount == 0 {
        return "OK"
    }
    return fmt.Sprintf("%d continuity errors", scr.DropCount)
}
```

---

## 📋 Специфікація MPEG-2 TS — критичні вимоги до типів

```
✅ Синхробайт: завжди 0x47, перший байт кожного пакету
✅ PID: 13 біт, діапазон 0x0000-0x1FFF, 0x1FFF = null packet
✅ Adaptation field control (AFC):
   • 0 = reserved (заборонено)
   • 1 = тільки payload
   • 2 = тільки adaptation field
   • 3 = adaptation field + payload
✅ Continuity counter: 4 біти, зростає по модулю 16 для кожного PID
✅ Transport scramble control: 0 = не зашифровано, 1-3 = зашифровано
✅ PCR: 33 біти base @ 90 kHz + 9 біт extension @ 27 MHz
✅ Stuffing bytes: SHOULD бути 0xFF, але не MUST
✅ Payload unit start indicator (PUSI): 1 = початок нової PES/PSI секції
```

---

## 🎯 Висновок

Ці типи — **солідна основа** для роботи з MPEG-2 TS:

✅ Правильна відповідність специфікації для основних полів  
✅ Використання bool для 1-бітних прапорців — читабельно та безпечно  
✅ Композиція через вбудовування для гнучкості

**Критичні покращення перед продакшеном**:

1. ✅ **Додати методи валідації** (`Validate()`) для всіх основних типів
2. ✅ **Замінити byte на uint8/конкретні типи** для бітових полів з документацією
3. ✅ **Додати методи доступу** (`HasPayload()`, `GetPayload()`, `GetPCR()`) для зручності
4. ✅ **Замінити анонімний struct** у `DropList` на іменований тип `DropEntry`
5. ✅ **Додати документацію** з посиланнями на специфікацію для кожного поля
6. ✅ **Розглянути оптимізації пам'яті**: sync.Pool, zero-copy, покажчики для опціональних полів
7. ✅ **Додати тести** для валідації типів та edge cases

**Приклад інтеграції у ваш pipeline**:
```go
// 🎯 PacketProcessor для вашого WebSocket-сервера:
type PacketProcessor struct {
    channelID string
    validator *TSValidator
    syncer    *AVSync
    assembler *SegmentAssembler  // Ваш існуючий компонент
}

func (p *PacketProcessor) ProcessPacket(pkt *mpeg2ts.Packet) error {
    // 🎯 Валідація структури
    if err := pkt.Validate(); err != nil {
        return fmt.Errorf("invalid packet: %w", err)
    }
    
    // 🎯 Перевірка скремблінгу
    if pkt.TransportScrambleControl != 0 {
        return fmt.Errorf("scrambled packet not supported")
    }
    
    // 🎯 PCR синхронізація
    if err := p.syncer.CorrectDrift(pkt, time.Now()); err != nil {
        p.logger.Warn("drift correction failed", "error", err)
    }
    
    // 🎯 Отримання payload для подальшої обробки
    payload, err := pkt.GetPayload()
    if err != nil {
        return fmt.Errorf("payload extraction failed: %w", err)
    }
    if payload == nil {
        return nil  // Немає даних для обробки
    }
    
    // 🎯 Відправка у ваш segmentAssembler
    return p.assembler.ProcessPayload(pkt.PID, payload)
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом типів з додаванням методів валідації та доступу?
- 🧠 Оптимізацією пам'яті через sync.Pool та zero-copy для high-throughput сценаріїв?
- 🧪 Написанням тестів для валідації типів та edge cases з невалідними даними?

Чекаю на ваші питання! 🛠️📡🎬