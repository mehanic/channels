# 🔍 Глибокий розбір коду: `mpeg2ts` — низькорівневий парсер заголовків та адаптаційних полів

Цей код реалізує **детальний бітовий парсер** для заголовків MPEG-2 Transport Stream пакетів, включаючи складну обробку адаптаційних полів (Adaptation Field) з підтримкою PCR, OPCR, splicing та private data. Розберемо архітектурно, з критичним аналізом та практичними рекомендаціями.

---

## 📦 Архітектура модуля: огляд компонентів

```
┌─────────────────────────────────────┐
│ Модуль: mpeg2ts (парсер заголовків) │
│ Призначення: розбір бітових полів   │
│ Вхід: 188-байтовий TS пакет         │
│ Стандарт: ISO/IEC 13818-1 + ETSI EN 300 468 │
├─────────────────────────────────────┤
│ 🔹 Ключові константи:                │
│    • ScrambleControl_* (2 біти)     │
│    • AdaptationField_* (2 біти)     │
│    • TableID_* (8 біт, PSI/SI)      │
│                                      │
│ 🔹 Основні типи:                     │
│    • Packet — один TS пакет         │
│    • PacketList — колекція пакетів  │
│    • AdaptationField — метадані     │
│    • PCR — Program Clock Reference  │
│                                      │
│ 🔹 Критичні методи:                  │
│    • parseHeader() — бітовий парсинг│
│    • GetPayload() — отримання даних │
│    • HasAdaptationField() — перевірка│
└─────────────────────────────────────┘
```

### 🎯 Контекст: структура TS пакету (188 байт)
```
📦 Транспортний пакет:
├─ Заголовок (4 байти, ОБОВ'ЯЗКОВИЙ):
│  ├─ [0] sync_byte = 0x47 (завжди!)
│  ├─ [1] flags: [TEI:1][PUSI:1][TP:1][PID:5]
│  ├─ [2] PID[7:0] (разом 13 біт для PID)
│  └─ [3] [TSC:2][AFC:2][continuity:4]
│
├─ Adaptation Field (0-183 байти, ОПЦІЙНО):
│  ├─ [4] length (кількість наступних байт)
│  ├─ [5] flags: [DI:1][RAI:1][ESPI:1][PCR:1][OPCR:1][SP:1][TPD:1][EF:1]
│  ├─ PCR (6 байт, якщо PCRFlag) — 33-біт base @90kHz + 9-біт ext @27MHz
│  ├─ OPCR (6 байт, якщо OPCRFlag) — оригінальний PCR
│  ├─ SpliceCountdown (1 байт, якщо SplicingPointFlag)
│  ├─ PrivateData (змінна, якщо TransportPrivateDataFlag)
│  ├─ Extension (змінна, якщо ExtensionFlag) ← ❌ Не реалізовано!
│  └─ Stuffing (0xFF байти, заповнення до кінця)
│
└─ Payload (решта байт, ОПЦІЙНО):
   • Якщо AFC=1: тільки payload
   • Якщо AFC=2: тільки adaptation field  
   • Якщо AFC=3: adaptation field + payload
```

---

## 🔬 Детальний розбір ключових функцій

### 1️⃣ Константи: TableID та адаптаційні поля

```go
const (
    // 🎯 Управління скремблінгом (2 біти в заголовку)
    ScrambleControl_NotScrambled = 0  // ✅ Дані відкриті
    ScrambleControl_Userdefined1 = 1  // ⚠️ Рідко використовуються
    ScrambleControl_Userdefined2 = 2
    ScrambleControl_Userdefined3 = 3
    
    // 🎯 Контроль адаптаційного поля (2 біти)
    AdaptationField_Reserved                = 0  // ❌ ЗАБОРОНЕНО специфікацією!
    AdaptationField_PayloadOnly             = 1  // ✅ Тільки payload
    AdaptationField_AdaptationFieldOnly     = 2  // ✅ Тільки adaptation field
    AdaptationField_AdaptationFieldFollowed = 3  // ✅ Обидва
    
    // 🎯 TableID для PSI/SI таблиць (8 біт) — ETSI EN 300 468
    TableID_ProgramAssociationSection = 0x00  // PAT (Program Association)
    TableID_ProgramMapSection         = 0x02  // PMT (Program Map)
    TableID_EventInformationSection_ActualDVBTransportStreamPresentFollowing = 0x4E  // EIT p/f
    // ... ще 30+ констант для DVB SI таблиць
)
```

#### ⚠️ Проблеми констант
```go
// ❌ AdaptationField_Reserved = 0 — специфікація явно забороняє це значення!
// • Якщо зустрічається → пакет пошкоджений або невалідний
// ✅ Правильно: не визначати заборонені значення, або додати валідацію:

func (p *Packet) validateAdaptationFieldControl() error {
    switch p.AdaptationFieldControl {
    case AdaptationField_PayloadOnly, 
         AdaptationField_AdaptationFieldOnly, 
         AdaptationField_AdaptationFieldFollowed:
        return nil
    case AdaptationField_Reserved:
        return fmt.Errorf("invalid adaptation_field_control: reserved value 0")
    default:
        return fmt.Errorf("invalid adaptation_field_control: %d", p.AdaptationFieldControl)
    }
}

// ❌ TableID константи мають коментарі про зарезервовані значення, але не визначені:
// • Це може заплутати: чи можна використовувати 0x04, 0x05, тощо?
// ✅ Правильно: або визначити як `= iota` з коментарем, або прибрати коментарі

// ✅ Додати метод валідації для TableID:
func IsValidTableID(tid uint8) bool {
    // 📋 Дозволені значення за ETSI EN 300 468 V1.17.1
    switch tid {
    case 0x00, 0x01, 0x02, 0x03, // PSI
         0x40, 0x41, 0x42, 0x46, 0x4A, 0x4B, 0x4C, // DVB SI
         0x4E, 0x4F, // EIT present/following
         0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59, 0x5A, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, // EIT schedule actual
         0x60, 0x61, 0x62, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69, 0x6A, 0x6B, 0x6C, 0x6D, 0x6E, 0x6F, // EIT schedule other
         0x70, 0x71, 0x72, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79, 0x7A, 0x7B, 0x7C, 0x7E, 0x7F, // Time/Date, Running Status, тощо
         0x80, 0x81, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89, 0x8A, 0x8B, 0x8C, 0x8D, 0x8E, 0x8F,
         0x90, 0x91, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98, 0x99, 0x9A, 0x9B, 0x9C, 0x9D, 0x9E, 0x9F,
         0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5, 0xA6, 0xA7, 0xA8, 0xA9, 0xAA, 0xAB, 0xAC, 0xAD, 0xAE, 0xAF,
         0xB0, 0xB1, 0xB2, 0xB3, 0xB4, 0xB5, 0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xBB, 0xBC, 0xBD, 0xBE, 0xBF,
         0xC0, 0xC1, 0xC2, 0xC3, 0xC4, 0xC5, 0xC6, 0xC7, 0xC8, 0xC9, 0xCA, 0xCB, 0xCC, 0xCD, 0xCE, 0xCF,
         0xD0, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0xDA, 0xDB, 0xDC, 0xDD, 0xDE, 0xDF,
         0xE0, 0xE1, 0xE2, 0xE3, 0xE4, 0xE5, 0xE6, 0xE7, 0xE8, 0xE9, 0xEA, 0xEB, 0xEC, 0xED, 0xEE, 0xEF,
         0xF0, 0xF1, 0xF2, 0xF3, 0xF4, 0xF5, 0xF6, 0xF7, 0xF8, 0xF9, 0xFA, 0xFB, 0xFC, 0xFD, 0xFE:
        return true
    case 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
         0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F,
         0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F,
         0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x3F,
         0x43, 0x44, 0x45, 0x47, 0x48, 0x49, 0x4D, 0x7D, 0xFF:
        return false  // Reserved / Reserved for future use
    default:
        return false
    }
}
```

---

### 2️⃣ `NewPacketList()` — конструктор колекції

```go
func NewPacketList(chunkSize int) (PacketList, error) {
    pl := PacketList{}
    pl.mutex = &sync.Mutex{}  // ✅ Правильна ініціалізація
    pl.chunkSize = chunkSize
    pl.packets = make([]Packet, 0, 1024)  // ✅ Попереднє виділення capacity
    return pl, nil
}
```

#### ⚠️ Проблеми дизайну
```go
// ❌ Повернення error, який завжди nil:
return pl, nil  // Якщо ніколи не повертає помилку → навіщо error?
// ✅ Правильно: або видалити error, або додати валідацію:
func NewPacketList(chunkSize int) (*PacketList, error) {
    if chunkSize != PacketSizeDefault && chunkSize != PacketSizeWithFEC {
        return nil, fmt.Errorf("invalid chunkSize: %d (expected %d or %d)", 
            chunkSize, PacketSizeDefault, PacketSizeWithFEC)
    }
    return &PacketList{
        mutex:     &sync.Mutex{},
        chunkSize: chunkSize,
        packets:   make([]Packet, 0, 1024),
    }, nil
}

// ❌ Value receiver vs pointer receiver інконсистентність:
// • Методи використовують *PacketList (✅ правильно для мутацій)
// • Але PacketList іноді передається за значенням → дорогі копії!
// ✅ Правильно: завжди використовувати *PacketList для мутабельних операцій
```

---

### 3️⃣ `AddBytes()` — парсинг пакету з байтів

```go
func (ps *PacketList) AddBytes(packetBytes []byte, packetSize int) error {
    ps.mutex.Lock()
    defer ps.mutex.Unlock()
    
    // 🎯 Перевірка розміру
    if len(packetBytes) != packetSize {
        return fmt.Errorf("packetBytes length and packetSize is not match. len(packetBytes) is %d", len(packetBytes))
    }
    
    index := len(ps.packets)
    p := Packet{}
    p.Data = make([]byte, PacketSizeDefault)  // ❌ Hardcoded!
    copy(p.Data, packetBytes)
    p.Index = index
    
    // 🎯 Парсинг заголовка
    err := p.parseHeader()
    if err != nil {
        return err
    }
    
    ps.packets = append(ps.packets, p)
    return nil
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Hardcoded PacketSizeDefault замість packetSize параметра:
p.Data = make([]byte, PacketSizeDefault)  // ❌ Завжди 188, навіть якщо packetSize=204!
// • При завантаженні TS з FEC (204 байти) → обрізання останніх 16 байт!
// ✅ Правильно:
p.Data = make([]byte, packetSize)  // ✅ Використовувати переданий розмір
copy(p.Data, packetBytes)

// ❌ Зайве копіювання даних:
p.Data = make([]byte, PacketSizeDefault)
copy(p.Data, packetBytes)  // Копія 188 байт
// ✅ Оптимізація: якщо packetBytes не буде змінюватися ззовні → можна уникнути копії:
// (але це вимагає обережності з життєвим циклом пам'яті)

// ❌ Відсутність перевірки синхробайта ДО парсингу:
err := p.parseHeader()  // parseHeader перевіряє 0x47, але після копіювання
// ✅ Правильно: швидка перевірка перед дорогими операціями:
if packetBytes[0] != 0x47 {
    return fmt.Errorf("invalid sync byte: 0x%02X at packet %d", packetBytes[0], index)
}
```

---

### 4️⃣ `parseHeader()` — серце парсера (бітовий розбір)

Це **найскладніша функція** модуля. Розберемо її поетапно.

#### 🎯 Етап 1: Перевірка синхробайта та розбір заголовка
```go
func (p *Packet) parseHeader() error {
    // 🎯 Перевірка sync_byte — критично важливо!
    if p.Data[0] != 0x47 {
        return fmt.Errorf("invalid magic number %02X", p.Data[0])  // ✅ Правильна помилка
    }
    
    // 🎯 Бітове розпакування заголовка (4 байти):
    p.SyncByte = p.Data[0]  // 0x47
    
    // Byte 1: [TEI:1][PUSI:1][TP:1][PID:5]
    p.TransportErrorIndicator = ((p.Data[1] >> 7) & 0x01) == 1  // ✅ Правильна маска
    p.PayloadUnitStartIndicator = ((p.Data[1] >> 6) & 0x01) == 1
    p.TransportPriorityIndicator = ((p.Data[1] >> 5) & 0x01) == 1
    
    // PID: 13 біт = [5 біт з byte1][8 біт з byte2]
    p.PID = PID((uint16(p.Data[1])&0x1F)<<8 | uint16(p.Data[2]))  // ✅ Правильна збірка
    
    // Byte 3: [TSC:2][AFC:2][continuity:4]
    p.TransportScrambleControl = (p.Data[3] >> 6) & 0x03  // ✅ 2 біти
    p.AdaptationFieldControl = (p.Data[3] >> 4) & 0x03
    p.ContinuityCheckIndex = (p.Data[3] & 0x0F)  // ✅ Нижні 4 біти
    
    // 🎯 Обробка адаптаційного поля
    if p.HasAdaptationField() {
        // ... розбір adaptation field ...
    }
    
    p.isHeaderParsed = true
    return nil
}
```

#### ✅ Правильні аспекти бітового парсингу
```
✅ Використання бітових операцій:
• (data >> n) & mask — стандартний підхід для виділення бітових полів
• Правильний порядок байт (big-endian, як вимагає MPEG-2 TS)

✅ Валідація вхідних даних:
• Перевірка sync_byte перед будь-яким парсингом
• Повернення інформативних помилок з контекстом

✅ Читабельність:
• Коментарі пояснюють структуру бітових полів
• Імена змінних відповідають специфікації (TEI, PUSI, тощо)
```

#### ⚠️ Проблеми розбору адаптаційного поля

##### Проблема 1: Неправильна перевірка довжини
```go
// ❌ Неправильна перевірка довжини adaptation field:
if p.AdaptationFieldControl == AdaptationField_AdaptationFieldFollowed {
    if af.Length > 183 {  // ❌ Максимум 182, бо 1 байт = length field!
        return fmt.Errorf("AdaptationField.Length should not exceed 182bytes")
    }
} else if p.AdaptationFieldControl == AdaptationField_AdaptationFieldOnly {
    if af.Length != 183 {  // ❌ Має бути 182, не 183!
        return fmt.Errorf("AdaptationField.Length must be 182bytes")
    }
}

// 📋 Специфікація: adaptation_field_length — 8 біт, значення 0-183
// • Але: загальна довжина adaptation field = 1 (length byte) + af.Length
// • Максимальний розмір пакета = 188 байт
// • Заголовок = 4 байти → залишок = 184 байти
// • Тому: af.Length може бути 0-183, але 1+af.Length ≤ 184 → af.Length ≤ 183 ✅

// ✅ Правильна перевірка:
const MaxAdaptationFieldLength = 183  // 184-1 (заголовок вже врахований)

if p.AdaptationFieldControl == AdaptationField_AdaptationFieldOnly {
    if af.Length != MaxAdaptationFieldLength {
        return fmt.Errorf("adaptation_field_only: length must be %d, got %d", 
            MaxAdaptationFieldLength, af.Length)
    }
} else if af.Length > MaxAdaptationFieldLength {
    return fmt.Errorf("adaptation field too long: %d > %d", af.Length, MaxAdaptationFieldLength)
}
```

##### Проблема 2: Неповний парсинг PCR
```go
// ❌ Неправильний парсинг PCR:
af.ProgramClockReference.Base = uint64(p.Data[fieldIndex])<<25 | uint64(p.Data[fieldIndex+1])<<17 | uint64(p.Data[fieldIndex+2])<<9 | uint64(p.Data[fieldIndex+3])<<1 | uint64(p.Data[fieldIndex+4])>>7&0x01
// 📋 PCR структура (6 байт):
// [0-4]: program_clock_reference_base (33 біти) + reserved (6 біт) + extension (9 біт)
// [5]: program_clock_reference_extension (9 біт, молодші біти)
// ❌ Код не враховує reserved 6 біт між base та extension!

// ✅ Правильний парсинг PCR:
func parsePCR(data []byte) (base uint64, extension uint16, err error) {
    if len(data) < 6 {
        return 0, 0, fmt.Errorf("PCR too short: %d < 6", len(data))
    }
    // Base: 33 біти = [byte0:8][byte1:8][byte2:8][byte3:8][byte4:1]
    base = uint64(data[0])<<25 | uint64(data[1])<<17 | uint64(data[2])<<9 | uint64(data[3])<<1 | uint64(data[4])>>7
    // Extension: 9 біт = [byte4:7 LSB][byte5:8]
    extension = (uint16(data[4]&0x01) << 8) | uint16(data[5])
    return base, extension, nil
}
```

##### Проблема 3: Незавершений парсинг ExtensionFlag
```go
// ❌ Незавершений парсинг ExtensionFlag:
if af.ExtensionFlag {
    fmt.Println("[BUG] AdaptationFieldExtension parsing is not implemented")
    // ... код пропущено ...
}

// 📋 Adaptation Field Extension може містити:
// • LTW (Legal Time Window) для синхронізації
// • Piecewise rate для CBR-стрімінгу  
// • Seamless splice info для ads insertion
// • Дескриптори адаптаційного поля

// ✅ Правильно: або реалізувати, або явно ігнорувати з логуванням:
if af.ExtensionFlag {
    // 🎯 Пропустити extension, але записати в лог для дебагу
    logger.Debug("skipping adaptation field extension", "pid", p.PID, "index", p.Index)
    // 🎯 Або: додати поле ExtensionData []byte для подальшої обробки
    // af.ExtensionData = p.Data[fieldIndex : fieldIndex+int(af.ExtensionLength)]
}
```

#### 🎯 Логіка `HasAdaptationField()`
```go
func (p *Packet) HasAdaptationField() bool {
    c := p.AdaptationFieldControl
    if c == AdaptationField_AdaptationFieldOnly || c == AdaptationField_AdaptationFieldFollowed {
        return true
    }
    return false
}
// ✅ Правильна реалізація: перевіряє AFC ∈ {2, 3}
```

---

### 5️⃣ `GetPayload()` — отримання корисного навантаження

```go
func (p *Packet) GetPayload() ([]byte, error) {
    if !p.isHeaderParsed {
        return nil, fmt.Errorf("execute parseHeader() first")  // ✅ Правильна перевірка
    }
    
    if len(p.Data) != PacketSizeDefault {
        return nil, fmt.Errorf("invalid data size")
    }
    
    if p.HasAdaptationField() {
        // 🎯 Пропустити заголовок (4) + length byte (1) + adaptation field (af.Length)
        return p.Data[4+p.AdaptationField.Length+1:], nil  // ⚠️ Потенційна паніка!
    }
    return p.Data[4:], nil  // ✅ Тільки заголовок пропущено
}
```

#### ⚠️ Проблеми безпеки
```go
// ❌ Потенційна паніка при out-of-bounds slice:
return p.Data[4+p.AdaptationField.Length+1:], nil
// • Якщо 4+1+af.Length > len(p.Data) → runtime panic: slice bounds out of range
// ✅ Правильно: перевірити межі перед доступом:
offset := 4 + 1 + int(p.AdaptationField.Length)
if offset > len(p.Data) {
    return nil, fmt.Errorf("adaptation field too long: offset %d > packet size %d", 
        offset, len(p.Data))
}
return p.Data[offset:], nil

// ❌ Повернення слайсу, що посилається на внутрішній буфер пакета:
// • Клієнт може модифікувати payload → пошкодження стану пакета!
// ✅ Правильно: повертати копію, якщо мутація можлива:
payload := p.Data[offset:]
return append([]byte(nil), payload...), nil  // Копія
// Або: документувати, що повернений слайс read-only
```

---

## ⚠️ Загальні проблеми модуля

### 1️⃣ Відсутність валідації після парсингу
```go
// ❌ parseHeader() не перевіряє узгодженість полів:
// • Чи PID у допустимому діапазоні (0x0000-0x1FFF)?
// • Чи continuity_counter ∈ [0,15]?
// • Чи adaptation_field_length узгоджений з AFC?

// ✅ Додати пост-парсинг валідацію:
func (p *Packet) Validate() error {
    if p.PID > 0x1FFF {
        return fmt.Errorf("invalid PID: 0x%X > 0x1FFF", p.PID)
    }
    if p.ContinuityCheckIndex > 15 {
        return fmt.Errorf("invalid continuity_counter: %d > 15", p.ContinuityCheckIndex)
    }
    if p.AdaptationFieldControl > 3 {
        return fmt.Errorf("invalid adaptation_field_control: %d", p.AdaptationFieldControl)
    }
    // ... інші перевірки ...
    return nil
}
```

### 2️⃣ Відсутність підтримки скремблінгу
```go
// ❌ TransportScrambleControl парситься, але не обробляється:
p.TransportScrambleControl = (p.Data[3] >> 6) & 0x03
// • Якщо TSC != 0 → дані зашифровані → payload неможливо парсити без ключа
// ✅ Правильно: додати перевірку та обробку:
func (p *Packet) IsScrambled() bool {
    return p.TransportScrambleControl != ScrambleControl_NotScrambled
}

func (p *Packet) GetPayload() ([]byte, error) {
    if p.IsScrambled() {
        return nil, fmt.Errorf("packet is scrambled (TSC=%d), decryption not supported", 
            p.TransportScrambleControl)
    }
    // ... решта коду ...
}
```

### 3️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу
// • Неможливо перевірити коректність бітового парсингу
// • Неможливо покрити edge cases (пошкоджені пакети, незвичні AFC)

// ✅ Додати мінімальні тести:
func TestParseHeader_Valid(t *testing.T) {
    // 🎯 Створити валідний пакет з відомими полями
    pkt := createValidPacket(PID(0x100), AdaptationField_PayloadOnly, 5)
    err := pkt.parseHeader()
    require.NoError(t, err)
    assert.Equal(t, PID(0x100), pkt.PID)
    assert.Equal(t, byte(5), pkt.ContinuityCheckIndex)
}

func TestParseHeader_InvalidSyncByte(t *testing.T) {
    pkt := Packet{Data: make([]byte, PacketSizeDefault)}
    pkt.Data[0] = 0x48  // ❌ Не 0x47!
    err := pkt.parseHeader()
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "invalid magic number")
}

func TestGetPayload_WithAdaptationField(t *testing.T) {
    pkt := createPacketWithAdaptationField(10)  // 10 байт adaptation field
    payload, err := pkt.GetPayload()
    require.NoError(t, err)
    expectedLen := PacketSizeDefault - 4 - 1 - 10  // заголовок + length + af
    assert.Len(t, payload, expectedLen)
}
```

### 4️⃣ Проблеми з адаптаційним полем
```go
// ❌ Stuffing перевірка надто сувора:
for i, v := range af.Stuffing {
    if v != 0xff {
        return fmt.Errorf("[BUG] stuffing bytes contains non-0xff byte. data:0x%02x index:%d", v, i)
    }
}
// 📋 Специфікація: stuffing bytes SHOULD бути 0xFF, але не MUST
// • Деякі енкодери використовують інші значення для відладки
// ✅ Правильно: логувати попередження, але не повертати помилку:
for i, v := range af.Stuffing {
    if v != 0xFF {
        logger.Warn("non-0xFF stuffing byte", "pid", p.PID, "index", i, "value", v)
        // Не повертати error — продовжити парсинг
    }
}

// ❌ Незавершений парсинг private data:
af.TransportPrivateData.Data = p.Data[fieldIndex+1 : fieldIndex+1+int(af.TransportPrivateData.Length)]
// • Не перевіряється, чи fieldIndex+1+Length ≤ len(p.Data)
// ✅ Правильно: перевірити межі:
endIndex := fieldIndex + 1 + int(af.TransportPrivateData.Length)
if endIndex > len(p.Data) {
    return fmt.Errorf("private data extends beyond packet: %d > %d", endIndex, len(p.Data))
}
af.TransportPrivateData.Data = p.Data[fieldIndex+1 : endIndex]
```

### 5️⃣ Інтернаціоналізація та логування
```go
// ❌ Змішані мови в помилках та коментарях:
return fmt.Errorf("sirikire %d", n)  // Японська в іншому файлі
fmt.Println("[BUG] AdaptationFieldExtension parsing is not implemented")  // Англійська
// ✅ Правильно: єдиний стиль — англійські помилки з контекстом:
return fmt.Errorf("incomplete packet: read %d bytes, expected %d", n, packetSize)
logger.Debug("adaptation field extension parsing not implemented", "pid", p.PID)
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

func (v *TSValidator) ValidatePacket(data []byte) error {
    if len(data) != mpeg2ts.PacketSizeDefault {
        return fmt.Errorf("invalid packet size: %d", len(data))
    }
    
    // 🎯 Швидка перевірка синхробайта
    if data[0] != 0x47 {
        return fmt.Errorf("invalid sync byte: 0x%02X", data[0])
    }
    
    // 🎯 Парсинг заголовка
    pkt := &mpeg2ts.Packet{Data: make([]byte, len(data))}
    copy(pkt.Data, data)
    if err := pkt.parseHeader(); err != nil {
        return fmt.Errorf("header parse failed: %w", err)
    }
    
    // 🎯 Перевірка скремблінгу
    if pkt.IsScrambled() {
        return fmt.Errorf("scrambled packet (TSC=%d), decryption not supported", 
            pkt.TransportScrambleControl)
    }
    
    // 🎯 Continuity check (спрощено)
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
    
    // 🎯 Конвертація PCR (27 MHz clock) у time.Time
    pcr := pkt.AdaptationField.ProgramClockReference
    // PCR base: 33 біти @ 90 kHz → секунди
    // PCR extension: 9 біт @ 27 MHz → суб-мілісекунди
    baseSeconds := float64(pcr.Base) / 90000.0
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

### 🎯 Сценарій: фільтрація PSI-пакетів для EPG
```go
// У EITProcessor для обробки програмної інформації:
func (p *EITProcessor) IsPSIPacket(pkt *mpeg2ts.Packet) bool {
    // 🎯 PSI таблиці мають PID 0x00 (PAT) або з PMT
    // 🎯 SI таблиці (DVB) мають PID з NIT/SDT/EIT
    return pkt.PID == 0x00 ||  // PAT
           pkt.PID == 0x10 ||  // Приклад: SDT
           pkt.PID == 0x12 ||  // Приклад: EIT
           pkt.PayloadUnitStartIndicator  // PUSI=1 → початок нової секції
}

func (p *EITProcessor) ParsePSISection(pkt *mpeg2ts.Packet) (*PSISection, error) {
    if !pkt.PayloadUnitStartIndicator {
        return nil, fmt.Errorf("not start of section: PUSI=0")
    }
    
    payload, err := pkt.GetPayload()
    if err != nil {
        return nil, err
    }
    
    // 🎯 Парсинг PSI заголовка: [pointer_field][table_id][section_syntax_indicator]...
    if payload[0] != 0x00 {  // pointer_field має бути 0 для початку секції
        return nil, fmt.Errorf("unexpected pointer_field: 0x%02X", payload[0])
    }
    
    tableID := payload[1]
    if !IsValidTableID(tableID) {
        return nil, fmt.Errorf("invalid table_id: 0x%02X", tableID)
    }
    
    // 🎯 Подальший парсинг залежить від tableID...
    return parseSectionByType(tableID, payload[2:])
}
```

---

## 🧪 Приклад: рефакторинг `parseHeader()` з кращою безпекою

```go
// ✅ Безпечний парсинг адаптаційного поля:
func (p *Packet) parseHeader() error {
    // ... перевірка sync_byte та розбір заголовка ...
    
    if p.HasAdaptationField() {
        // 🎯 Перевірка меж перед доступом до даних
        if len(p.Data) < 5 {  // 4 заголовок + 1 length byte
            return fmt.Errorf("packet too short for adaptation field: %d < 5", len(p.Data))
        }
        
        af := AdaptationField{}
        af.Length = p.Data[4]
        
        // 🎯 Валідація довжини
        maxAFLength := len(p.Data) - 5  // Заголовок (4) + length byte (1)
        if int(af.Length) > maxAFLength {
            return fmt.Errorf("adaptation field too long: %d > %d", af.Length, maxAFLength)
        }
        
        // 🎯 Перевірка узгодженості з AFC
        if p.AdaptationFieldControl == AdaptationField_AdaptationFieldOnly {
            if int(af.Length) != maxAFLength {
                return fmt.Errorf("adaptation_field_only: length mismatch: %d != %d", 
                    af.Length, maxAFLength)
            }
        }
        
        if af.Length == 0 {
            p.AdaptationField = af
            p.isHeaderParsed = true
            return nil
        }
        
        // 🎯 Безпечний доступ до байтів адаптаційного поля
        afData := p.Data[5 : 5+int(af.Length)]
        
        // Розбір флагів (byte 5 відносно початку adaptation field = p.Data[5])
        flags := afData[0]
        af.DiscontinuityIndicator = (flags >> 7) & 0x01 == 1
        // ... інші прапорці ...
        
        fieldIndex := 1  // Індекс всередині afData, не p.Data
        
        // 🎯 Безпечний парсинг PCR
        if af.PCRFlag {
            if fieldIndex+6 > len(afData) {
                return fmt.Errorf("PCR field extends beyond adaptation field")
            }
            pcrBase, pcrExt, err := parsePCR(afData[fieldIndex:])
            if err != nil {
                return fmt.Errorf("PCR parse failed: %w", err)
            }
            af.ProgramClockReference.Base = pcrBase
            af.ProgramClockReference.Extension = pcrExt
            fieldIndex += 6
        }
        
        // ... аналогічно для OPCR, SpliceCountdown, PrivateData ...
        
        // 🎯 Обробка stuffing з попередженням замість помилки
        remaining := int(af.Length) - fieldIndex
        if remaining > 0 {
            af.Stuffing = afData[fieldIndex:]
            for i, b := range af.Stuffing {
                if b != 0xFF {
                    logger.Warn("non-0xFF stuffing byte", 
                        "pid", p.PID, 
                        "offset", 5+fieldIndex+i, 
                        "value", fmt.Sprintf("0x%02X", b))
                }
            }
        }
        
        p.AdaptationField = af
    }
    
    p.isHeaderParsed = true
    return nil
}

// 🎯 Helper для безпечного парсингу PCR:
func parsePCR(data []byte) (base uint64, extension uint16, err error) {
    if len(data) < 6 {
        return 0, 0, fmt.Errorf("PCR too short: %d < 6", len(data))
    }
    // Base: 33 біти
    base = uint64(data[0])<<25 | uint64(data[1])<<17 | uint64(data[2])<<9 | uint64(data[3])<<1 | uint64(data[4])>>7
    // Extension: 9 біт
    extension = (uint16(data[4]&0x01) << 8) | uint16(data[5])
    return base, extension, nil
}
```

---

## 📋 Специфікація MPEG-2 TS — критичні вимоги до заголовків

```
✅ Sync byte: завжди 0x47, перший байт кожного пакету
✅ PID: 13 біт, діапазон 0x0000-0x1FFF, 0x1FFF = null packet
✅ Adaptation field control (AFC):
   • 0 = reserved (заборонено)
   • 1 = тільки payload
   • 2 = тільки adaptation field
   • 3 = adaptation field + payload
✅ Continuity counter: 4 біти, зростає по модулю 16 для кожного PID
✅ Adaptation field length: 8 біт, 0-183, загальна довжина = 1 + length
✅ PCR: 6 байт, 33-біт base @ 90 kHz + 9-біт extension @ 27 MHz
✅ Stuffing bytes: SHOULD бути 0xFF, але не MUST (попередження, не помилка)
✅ Payload unit start indicator (PUSI): 1 = початок нової PES/PSI секції
✅ Transport scramble control (TSC): 0 = не зашифровано, 1-3 = зашифровано
```

---

## 🎯 Висновок

Цей код — **детальна реалізація низькорівневого парсингу** MPEG-2 TS заголовків:

✅ Правильне бітове розпакування полів за специфікацією  
✅ Підтримка адаптаційних полів з PCR/OPCR/splicing  
✅ Валідація sync_byte та базових обмежень

**Критичні виправлення перед продакшеном**:

1. ✅ **Виправити hardcoded PacketSizeDefault** у `AddBytes()` → використовувати `packetSize` параметр
2. ✅ **Додати перевірку меж** перед доступом до слайсів у `GetPayload()` та `parseHeader()`
3. ✅ **Реалізувати безпечний парсинг PCR** з урахуванням reserved біт
4. ✅ **Замінити помилку на попередження** для non-0xFF stuffing bytes
5. ✅ **Додати валідацію після парсингу** (`Validate()` метод для пакета)
6. ✅ **Обробити скремблінг** — повертати помилку, якщо TSC != 0
7. ✅ **Додати тести** для бітового парсингу та edge cases

**Приклад інтеграції у ваш pipeline**:
```go
// 🎯 TSPacketProcessor для вашого WebSocket-сервера:
type TSPacketProcessor struct {
    validator *TSValidator
    pcrSync   *PCRSync
    psiParser *PSIParser
}

func (p *TSPacketProcessor) ProcessPacket(data []byte) error {
    // 🎯 Валідація та парсинг
    if err := p.validator.ValidatePacket(data); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    
    pkt := &mpeg2ts.Packet{Data: make([]byte, len(data))}
    copy(pkt.Data, data)
    if err := pkt.parseHeader(); err != nil {
        return fmt.Errorf("header parse failed: %w", err)
    }
    
    // 🎯 PCR синхронізація
    if err := p.pcrSync.Process(pkt); err != nil {
        // Логувати, але не зупиняти потік
        log.Printf("PCR sync warning: %v", err)
    }
    
    // 🎯 PSI обробка
    if pkt.PID == 0x00 || pkt.PayloadUnitStartIndicator {
        if section, err := p.psiParser.ParseSection(pkt); err == nil {
            p.handlePSISection(section)
        }
    }
    
    // 🎯 Відправка payload у ваш segmentAssembler
    if payload, err := pkt.GetPayload(); err == nil && len(payload) > 0 {
        return p.assembler.ProcessPayload(pkt.PID, payload)
    }
    
    return nil
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом `parseHeader()` з безпечним доступом до слайсів та повним парсингом PCR?
- 🧠 Інтеграцією PCR-синхронізації у ваш A/V sync модуль?
- 🧪 Написанням тестів для бітового парсингу з моковими даними та валідацією меж?

Чекаю на ваші питання! 🛠️📡🎬