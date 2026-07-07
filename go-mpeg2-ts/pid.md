# 🔍 Глибокий розбір: `PID` константи для MPEG-2 TS

Цей код визначає тип `PID` (Packet Identifier) та набір констант для ідентифікації потоків у MPEG-2 Transport Stream згідно з **ISO/IEC 13818-1** та **ARIB STD-B10**. Розберемо архітектурно, з критичним аналізом та практичними рекомендаціями.

---

## 📦 Контекст: що таке PID і навіщо він потрібен?

### Контекст: ідентифікація потоків у TS
```
📦 MPEG-2 TS пакет (188 байт):
   ├─ Заголовок (4 байти)
   │  └─ PID: 13 біт (0x0000-0x1FFF) ← ключове поле для фільтрації!
   └─ Payload

🎯 Призначення PID:
• Фільтрація пакетів: "мені потрібні тільки пакети з PID=0x100"
• Маршрутизація: відео → PID 0x110, аудіо → PID 0x111, субтитри → PID 0x112
• Ідентифікація системних таблиць: PAT=0x0000, PMT=змінний, NIT=0x0010, тощо

📋 Діапазони PID за специфікацією:
• 0x0000: PAT (Program Association Table) — завжди!
• 0x0001: CAT (Conditional Access Table)
• 0x0002: TSDT (Transport Stream Description Table)
• 0x0003-0x000F: Зарезервовано (не використовувати!)
• 0x0010-0x1FFE: Користувацькі програми/потоки
• 0x1FFF: Null packets (заповнення)
```

---

## 🔬 Детальний розбір констант

### ✅ Правильні визначення
```go
type PID uint16  // ✅ 16 біт достатньо для 13-бітного PID

// 🎯 Стандартні PSI таблиці (ISO/IEC 13818-1)
PID_PAT        = PID(0x0000)  // ✅ Завжди 0x0000
PID_CAT        = PID(0x0001)  // ✅ Conditional Access
PID_TSDT       = PID(0x0002)  // ✅ Transport Stream Description
PID_NullPacket = PID(0x1fff)  // ✅ Завжди 0x1FFF для заповнення

// 🎯 Зарезервовані PID (0x0003-0x000F)
PID_Reserved1  = PID(0x0003)
// ... до PID_Reserved13 = PID(0x000f)
// ✅ Правильно: визначено, але документація має забороняти використання
```

### ❌ Критичні проблеми: неправильні аліаси

```go
// ❌ КРИТИЧНА ПОМИЛКА: PID_PMT = PID_PAT
PID_PMT = PID_PAT  // ❌ PMT НЕ МОЖЕ дорівнювати PAT!

// 📋 Чому це неправильно:
// • PAT (0x0000) містить список програм → їхні PMT PID
// • PMT PID — змінний, визначається у PAT!
// • Приклад валідного PAT:
//   Program #1 → PMT PID = 0x100
//   Program #2 → PMT PID = 0x101
// • Якщо PMT = 0x0000 → нескінченний цикл парсингу!

// ✅ Правильно: НЕ визначати PID_PMT як константу!
// • PMT PID дізнаємося ДИНАМІЧНО з PAT таблиці
// • Якщо потрібна константа для прикладів — документація, не код:
//   // Example PMT PID (actual value comes from PAT): 0x0100

// ❌ Аналогічні помилки:
PID_ECM       = PID_PMT  // ❌ ECM має власні PID у діапазоні 0x0010-0x1FFE
PID_ECM_S     = PID_PMT  // ❌ Те саме
PID_EMM       = PID_CAT  // ❌ EMM ≠ CAT (0x0001)
PID_EMM_S     = PID_CAT  // ❌ Те саме
PID_LIT1      = PID_PMT  // ❌ Неправильний аліас
PID_ERT1      = PID_PMT  // ❌ Неправильний аліас
PID_ITT       = PID_PMT  // ❌ Неправильний аліас
PID_DSM_CC    = PID_PMT  // ❌ Неправильний аліас
PID_AIT       = PID_PMT  // ❌ Неправильний аліас

// ❌ SDT/BAT спільний PID (може бути правильно для ARIB, але заплутує):
PID_SDT = PID(0x0011)
PID_BAT = PID(0x0011)  // ⚠️ Однакове значення для різних таблиць!
// 📋 В ARIB STD-B10 це може бути навмисно, але варто коментувати:
// // ARIB: SDT і BAT можуть мати однаковий PID 0x0011
```

### ⚠️ Проблеми з зарезервованими PID

```go
// ❌ Визначення зарезервованих PID як констант може ввести в оману:
PID_Reserved1 = PID(0x0003)  // Може спонукати до використання!

// ✅ Правильно: або не визначати, або додати явну заборону:
// ⚠️ DO NOT USE: PIDs 0x0003-0x000F are reserved by ISO/IEC 13818-1
// If you need custom PIDs, use 0x0020-0x1FFE

// ✅ Або додати метод валідації:
func (p PID) IsValid() bool {
    // ✅ Дозволені: 0x0000-0x0002, 0x0010-0x1FFE, 0x1FFF
    // ❌ Заборонені: 0x0003-0x000F
    return (p <= 0x0002) || (p >= 0x0010 && p <= 0x1FFF)
}
```

---

## 🔬 Відсутність корисних методів

### ❌ Немає `String()` для дебагу
```go
// ❌ Неможливо легко вивести PID у лог:
pid := PID(0x100)
fmt.Println(pid)  // Виведе "256", а не "0x0100" або "Video PID"

// ✅ Додати метод для читабельного виводу:
func (p PID) String() string {
    switch p {
    case PID_PAT:
        return "PAT(0x0000)"
    case PID_CAT:
        return "CAT(0x0001)"
    case PID_NullPacket:
        return "Null(0x1FFF)"
    default:
        return fmt.Sprintf("0x%04X", uint16(p))
    }
}
```

### ❌ Немає методів класифікації PID
```go
// ❌ Важко перевірити тип PID без ручних порівнянь:
if pid == 0x0000 || pid == 0x0001 || pid == 0x0002 { ... }  // ❌ Не читабельно

// ✅ Додати методи-предикати:
// IsPSI повертає true для системних таблиць (PAT/CAT/TSDT)
func (p PID) IsPSI() bool {
    return p <= 0x0002
}

// IsReserved повертає true для зарезервованих 0x0003-0x000F
func (p PID) IsReserved() bool {
    return p >= 0x0003 && p <= 0x000F
}

// IsNull повертає true для null пакетів
func (p PID) IsNull() bool {
    return p == PID_NullPacket
}

// IsValid повертає true для дозволених PID
func (p PID) IsValid() bool {
    return !p.IsReserved()
}

// IsVideoPID / IsAudioPID — якщо відома конфігурація потоку
// (потребує зовнішньої інформації з PMT)
```

### ❌ Немає конструктора з валідацією
```go
// ❌ Можна створити невалідний PID:
invalid := PID(0x0005)  // Зарезервовано!

// ✅ Додати конструктор з валідацією:
func NewPID(value uint16) (PID, error) {
    p := PID(value)
    if p.IsReserved() {
        return 0, fmt.Errorf("PID 0x%04X is reserved by ISO/IEC 13818-1", value)
    }
    if value > 0x1FFF {
        return 0, fmt.Errorf("PID 0x%04X exceeds maximum 0x1FFF", value)
    }
    return p, nil
}
```

---

## ⚠️ Проблеми з документацією

### ❌ Відсутність посилань на специфікацію
```go
// ❌ Константи без контексту:
PID_NIT = PID(0x0010)  // ❌ Що це? Де використовується?

// ✅ Додати документацію з посиланнями:
// PID_NIT — Network Information Table (ARIB STD-B10, ISO/IEC 13818-1)
// Містить інформацію про мережу: частоти, модуляцію, тощо
// Зазвичай PID 0x0010 в ARIB, але може бути іншим в інших стандартах
PID_NIT = PID(0x0010)

// PID_EIT — Event Information Table (EPG дані)
// Містить програмний гід: назви подій, час, опис, жанр
// ARIB: зазвичай PID 0x0012 для terrestrial broadcast
PID_EIT = PID(0x0012)
```

### ❌ Закоментовані константи без пояснень
```go
// ❌ Загадковий закоментований рядок:
// PID_STExclude = PID(0x0000)

// ✅ Або видалити, або пояснити:
// PID_STExclude — застаріла константа, видалена в ARIB STD-B10 v2.0
// // PID_STExclude = PID(0x0000)  // Deprecated, do not use
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **фільтрацією потоків**:

### 🎯 Сценарій: безпечна фільтрація за PID
```go
// У PIDFilter для відбору потрібних пакетів:
type PIDFilter struct {
    allowed map[PID]bool
    logger  *log.Logger
}

func NewPIDFilter(pids ...PID) *PIDFilter {
    allowed := make(map[PID]bool, len(pids))
    for _, pid := range pids {
        // 🎯 Валідація вхідних PID
        if !pid.IsValid() {
            logger.Warn("invalid PID ignored", "pid", pid)
            continue
        }
        allowed[pid] = true
    }
    return &PIDFilter{allowed: allowed}
}

func (f *PIDFilter) Allow(pkt *mpeg2ts.Packet) bool {
    // 🎯 Швидка перевірка через map
    return f.allowed[pkt.PID]
}

// Використання:
// 🎯 Отримати PMT PID з PAT, потім створити фільтр
pat, _ := patPacket.ParsePAT()
var videoPID, audioPID mpeg2ts.PID
for _, prog := range pat.Programs {
    if prog.ProgramNumber == 1 {  // Основна програма
        pmt := parsePMT(prog.ProgramMapPID)  // Парсинг PMT
        videoPID = pmt.VideoPID()
        audioPID = pmt.AudioPID()
        break
    }
}
filter := NewPIDFilter(videoPID, audioPID, mpeg2ts.PID_PAT)  // + PAT для оновлень
```

### 🎯 Сценарій: моніторинг використання PID
```go
// У monitoring.Monitor для аналізу розподілу потоків:
type PIDUsageReport struct {
    TotalPackets int64
    ByPID        map[mpeg2ts.PID]int64  // PID → кількість пакетів
    UnknownPIDs  []mpeg2ts.PID          // PID, що не розпізнані
}

func (m *Monitor) AnalyzePIDUsage(packets []mpeg2ts.Packet) PIDUsageReport {
    report := PIDUsageReport{
        ByPID: make(map[mpeg2ts.PID]int64),
    }
    
    knownPIDs := map[mpeg2ts.PID]string{
        mpeg2ts.PID_PAT:        "PAT",
        mpeg2ts.PID_NIT:        "NIT",
        mpeg2ts.PID_SDT:        "SDT",
        mpeg2ts.PID_EIT:        "EIT",
        mpeg2ts.PID_NullPacket: "Null",
        // ... додати відомі PID з вашої конфігурації ...
    }
    
    for _, pkt := range packets {
        report.TotalPackets++
        report.ByPID[pkt.PID]++
        
        if _, known := knownPIDs[pkt.PID]; !known && !pkt.PID.IsReserved() {
            // 🎯 Невідомий, але валідний PID → можливо, новий потік
            report.UnknownPIDs = append(report.UnknownPIDs, pkt.PID)
        }
    }
    
    return report
}
```

### 🎯 Сценарій: динамічне оновлення фільтрів через PAT/PMT
```go
// У PATMonitor для відстеження змін у конфігурації потоків:
type StreamConfig struct {
    VideoPID mpeg2ts.PID
    AudioPID mpeg2ts.PID
    SubtitlePID mpeg2ts.PID  // Опціонально
}

type PATMonitor struct {
    currentConfig map[uint16]StreamConfig  // ProgramNumber → конфігурація
    onChange      func(old, new StreamConfig)
}

func (m *PATMonitor) OnPATUpdate(pat mpeg2ts.PAT) {
    for _, prog := range pat.Programs {
        if prog.ProgramNumber == 0 {
            continue  // Пропускаємо NIT запис
        }
        
        // 🎯 Парсинг PMT для отримання PID відео/аудіо
        pmt := parsePMTFromPID(prog.ProgramMapPID)  // Ваша функція
        config := StreamConfig{
            VideoPID: pmt.FindVideoPID(),
            AudioPID: pmt.FindAudioPID(),
        }
        
        // 🎯 Сповіщення про зміни
        if old, exists := m.currentConfig[prog.ProgramNumber]; exists {
            if old != config && m.onChange != nil {
                m.onChange(old, config)  // 🎯 Оновити фільтри, транскодери, тощо
            }
        }
        m.currentConfig[prog.ProgramNumber] = config
    }
}
```

---

## 🧪 Приклад: рефакторинг з кращою безпекою

```go
// ✅ Виправлені константи з валідацією:
type PID uint16

const (
    // 🎯 ISO/IEC 13818-1: обов'язкові PSI таблиці
    PID_PAT        = PID(0x0000)  // Program Association Table
    PID_CAT        = PID(0x0001)  // Conditional Access Table
    PID_TSDT       = PID(0x0002)  // Transport Stream Description Table
    
    // ⚠️ 0x0003-0x000F: RESERVED — DO NOT USE
    // Визначено тільки для документації, не для використання
    PID_ReservedStart = PID(0x0003)
    PID_ReservedEnd   = PID(0x000F)
    
    // 🎯 ARIB STD-B10: системні таблиці
    PID_NIT        = PID(0x0010)  // Network Information Table
    PID_SDT_BAT    = PID(0x0011)  // SDT/BAT (спільний PID в ARIB)
    PID_EIT        = PID(0x0012)  // Event Information Table
    PID_TDT_TOT    = PID(0x0014)  // Time/Date Table
    
    // 🎯 Null packets (завжди 0x1FFF)
    PID_NullPacket = PID(0x1FFF)
)

// ❌ НЕ визначати PID_PMT як константу!
// PMT PID дізнаємося динамічно з PAT:
// func GetPMTPID(pat PAT, programNumber uint16) (PID, error)

// ✅ Методи валідації:
func (p PID) IsValid() bool {
    return p <= 0x0002 || (p >= 0x0010 && p <= 0x1FFF)
}

func (p PID) IsReserved() bool {
    return p >= PID_ReservedStart && p <= PID_ReservedEnd
}

func (p PID) IsPSI() bool {
    return p <= 0x0002
}

func (p PID) IsNull() bool {
    return p == PID_NullPacket
}

// ✅ String() для дебагу:
func (p PID) String() string {
    switch p {
    case PID_PAT:
        return "PAT"
    case PID_CAT:
        return "CAT"
    case PID_TSDT:
        return "TSDT"
    case PID_NIT:
        return "NIT"
    case PID_SDT_BAT:
        return "SDT/BAT"
    case PID_EIT:
        return "EIT"
    case PID_NullPacket:
        return "Null"
    default:
        if p.IsReserved() {
            return fmt.Sprintf("Reserved(0x%04X)", uint16(p))
        }
        return fmt.Sprintf("0x%04X", uint16(p))
    }
}

// ✅ Конструктор з валідацією:
func NewPID(value uint16) (PID, error) {
    if value > 0x1FFF {
        return 0, fmt.Errorf("PID 0x%04X exceeds maximum 0x1FFF", value)
    }
    p := PID(value)
    if p.IsReserved() {
        return 0, fmt.Errorf("PID 0x%04X is reserved by ISO/IEC 13818-1", value)
    }
    return p, nil
}

// ✅ Helper для отримання PMT PID з PAT (замість неправильної константи):
func GetProgramMapPID(pat PAT, programNumber uint16) (PID, error) {
    for _, prog := range pat.Programs {
        if prog.ProgramNumber == programNumber {
            return prog.ProgramMapPID, nil
        }
    }
    return 0, fmt.Errorf("program %d not found in PAT", programNumber)
}
```

---

## 📋 Специфікація — критичні вимоги до PID

```
✅ Діапазон: 13 біт → 0x0000-0x1FFF (8192 можливих значень)
✅ Обов'язкові значення:
   • 0x0000: PAT (завжди!)
   • 0x0001: CAT (якщо є умовний доступ)
   • 0x0002: TSDT (опціонально)
   • 0x1FFF: Null packets (для заповнення пропускної здатності)
✅ Зарезервовано: 0x0003-0x000F (не використовувати!)
✅ Користувацькі: 0x0010-0x1FFE (для програм, відео, аудіо, тощо)
✅ PMT PID: НЕ фіксований! Визначається динамічно в PAT таблиці
✅ Кілька програм: кожна має свій PMT PID, перелічені в PAT
✅ Null packets: завжди 0x1FFF, ігноруються парсерами
✅ Фільтрація: клієнти мають фільтрувати за PID до парсингу payload
```

---

## 🎯 Висновок

Цей файл — **корисна основа** для роботи з PID, але має критичні проблеми:

✅ Правильний тип `uint16` для 13-бітного PID  
✅ Визначення стандартних констант (PAT, CAT, Null)  
✅ Підтримка як ISO, так і ARIB стандартів

**Критичні виправлення перед продакшеном**:

1. ✅ **ВИДАЛИТИ або виправити `PID_PMT = PID_PAT`** — це критична помилка!
2. ✅ **Видалити неправильні аліаси**: `PID_ECM = PID_PMT`, `PID_EMM = PID_CAT`, тощо
3. ✅ **Додати методи валідації**: `IsValid()`, `IsReserved()`, `IsPSI()`
4. ✅ **Додати `String()` метод** для читабельного логування
5. ✅ **Документувати константи** з посиланнями на специфікацію
6. ✅ **Додати конструктор `NewPID()`** з валідацією вхідних значень
7. ✅ **Пояснити або видалити закоментовані константи**

**Приклад правильного використання у вашому коді**:
```go
// 🎯 Отримання та валідація PID:
func processPacket(pkt *mpeg2ts.Packet) error {
    // 🎯 Перевірка валідності PID
    if !pkt.PID.IsValid() {
        return fmt.Errorf("invalid PID: 0x%04X", pkt.PID)
    }
    
    // 🎯 Спеціальна обробка системних таблиць
    switch pkt.PID {
    case mpeg2ts.PID_PAT:
        return handlePAT(pkt)
    case mpeg2ts.PID_NullPacket:
        return nil  // Ігноруємо null пакети
    }
    
    // 🎯 Для програмних потоків: фільтрація за конфігурацією
    if !streamConfig.HasPID(pkt.PID) {
        return nil  // Не наш потік → ігноруємо
    }
    
    // 🎯 Обробка медіа-даних...
    return processMediaPacket(pkt)
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом констант з виправленням критичних аліасів?
- 🧠 Інтеграцією динамічного отримання PMT PID з PAT таблиці?
- 🧪 Написанням тестів для валідації PID та методів класифікації?

Чекаю на ваші питання! 🛠️📡🎬