# 🔍 Глибокий розбір: типи даних EIT (Event Information Table) для MPEG-2 TS / DVB

Цей код визначає структури для парсингу **EIT (Event Information Table)** — таблиці програмної інформації у стандартах DVB SI (ETSI EN 300 468). Розберемо архітектурно, з критичним аналізом та рекомендаціями.

---

## 📦 Контекст: що таке EIT і навіщо він потрібен?

### Контекст: DVB Service Information (SI)
```
📺 DVB SI таблиці надають метадані про телевізійні послуги:
├─ PAT (Program Association Table) — список програм
├─ PMT (Program Map Table) — компоненти програми
├─ SDT (Service Description Table) — опис каналів
├─ EIT (Event Information Table) — 🎯 програми/події у каналах (EPG)
├─ TOT/TDT — час та дата
└─ CAT — умовний доступ (DRM)

🎯 EIT призначення:
• Електронний програмний гід (EPG) для користувача
• Автоматичне записування за розкладом
• Фільтрація контенту за жанром, рейтингом, мовою
• Синхронізація з зовнішніми системами (метадані, рекомендації)
```

### 📋 Структура EIT за специфікацією (ETSI EN 300 468)
```
📦 EIT Section (змінна довжина, макс. 1021 байт):
├─ table_id                    8 біт   (0x4E-0x6F)
├─ section_syntax_indicator    1 біт   (завжди 1 для EIT)
├─ '0'                         1 біт   (зарезервовано)
├─ reserved_future_use         2 біти
├─ section_length             12 біт   (до 1021 байт після цього поля)
├─ service_id                 16 біт   (ідентифікатор каналу)
├─ reserved_future_use         2 біти
├─ version_number              5 біт   (версія таблиці, 0-31)
├─ current_next_indicator      1 біт   (1 = актуальна, 0 = наступна)
├─ section_number              8 біт   (номер секції, 0-based)
├─ last_section_number         8 біт   (останній номер секції)
├─ transport_stream_id        16 біт   ⚠️ У коді: byte (помилка!)
├─ original_network_id        16 біт   ⚠️ У коді: byte (помилка!)
├─ segment_last_section_number 8 біт   (тільки для certain EIT types)
├─ last_table_id               8 біт   (останній table_id у сегменті)
├─ events[]                   змінна   (масив подій)
└─ CRC_32                     32 біти  ⚠️ У коді: uint (платформозалежний!)

📦 EIT Event (всередині EIT):
├─ event_id                   16 біт   (унікальний ID події)
├─ start_time                 40 біт   (MJD + 3 байти часу, BCD)
├─ duration                   24 біти  (3 байти, BCD: HH:MM:SS)
├─ running_status              3 біти  (0-4: undefined/running/paused/тощо)
├─ free_CA_mode                1 біт   (0 = free, 1 = scrambled)
├─ descriptors_loop_length    12 біт   (загальна довжина дескрипторів)
└─ descriptors[]              змінна   (масив дескрипторів)

📦 EIT Descriptor (загальна структура):
├─ descriptor_tag              8 біт   (тип дескриптора: 0x4D = short_event)
├─ descriptor_length           8 біт   (довжина payload)
└─ descriptor_data[]           змінна  (залежить від типу)
```

---

## 🔬 Детальний розбір типів у коді

### `EITTable` — основна структура таблиці

```go
type EITTable struct {
    TableID                  byte      // ✅ 8 біт — правильно
    SectionSyntaxIndicator   bool      // ✅ 1 біт — правильно
    SectionLength            uint16    // ⚠️ 12 біт у специфікації, uint16 = 16 біт
    ServiceID                uint16    // ✅ 16 біт — правильно
    Version                  byte      // ⚠️ 5 біт у специфікації, byte = 8 біт
    CurrentNextIndicator     bool      // ✅ 1 біт — правильно
    SectionNumber            byte      // ✅ 8 біт — правильно
    LastSectionNumber        byte      // ✅ 8 біт — правильно
    SegmentLastSectionNumber byte      // ✅ 8 біт — правильно (але опціонально)
    TransportStreamID        byte      // ❌ 16 біт у специфікації, byte = 8 біт!
    OriginalNetworkID        byte      // ❌ 16 біт у специфікації, byte = 8 біт!
    LastTableID              byte      // ✅ 8 біт — правильно
    CRC32                    uint      // ❌ uint платформозалежний, має бути uint32!
    Events                   []EITEvent // ✅ масив подій
}
```

#### ⚠️ Критичні проблеми типів

| Поле | У коді | У специфікації | Наслідок |
|------|--------|---------------|----------|
| `TransportStreamID` | `byte` (8 біт) | `uint16` (16 біт) | ❌ Обрізання значень >255, неможливо ідентифікувати потоки |
| `OriginalNetworkID` | `byte` (8 біт) | `uint16` (16 біт) | ❌ Неможливо розрізнити мережі >256 |
| `CRC32` | `uint` (32/64 біт) | `uint32` (32 біт) | ⚠️ На 64-бітних системах — зайва пам'ять, потенційні помилки порівняння |
| `SectionLength` | `uint16` (16 біт) | 12 біт | ⚠️ Може прийняти невалідні значення 1024-65535 |
| `Version` | `byte` (8 біт) | 5 біт | ⚠️ Може прийняти значення 32-255, які невалідні |

#### ✅ Правильні типи
```go
type EITTable struct {
    TableID                  uint8   // ✅ Явний 8-бітний тип
    SectionSyntaxIndicator   bool
    SectionLength            uint16  // ✅ Зберігати як uint16, але валідувати <= 1023 при парсингу
    ServiceID                uint16
    Version                  uint8   // ✅ Зберігати як uint8, але маскувати & 0x1F при парсингу
    CurrentNextIndicator     bool
    SectionNumber            uint8
    LastSectionNumber        uint8
    SegmentLastSectionNumber uint8
    TransportStreamID        uint16  // ✅ Виправлено: 16 біт!
    OriginalNetworkID        uint16  // ✅ Виправлено: 16 біт!
    LastTableID              uint8
    CRC32                    uint32  // ✅ Явний 32-бітний тип для CRC
    Events                   []EITEvent
}
```

---

### `EITEvent` — структура події

```go
type EITEvent struct {
    EventID           int            // ⚠️ У специфікації: 16 біт, int = платформозалежний
    StartTime         int            // ⚠️ У специфікації: 40 біт (MJD+time), int втрачає семантику
    Duration          int            // ⚠️ У специфікації: 24 біти (3 байти BCD), int втрачає формат
    RunningStatus     int            // ⚠️ У специфікації: 3 біти (0-4), int дозволяє невалідні значення
    FreeCAMode        int            // ⚠️ У специфікації: 1 біт, int дозволяє значення != 0/1
    DescriptorsLength int            // ⚠️ У специфікації: 12 біт, int дозволяє невалідні значення
    Descriptors       []EITDescriptor // ✅ масив дескрипторів
}
```

#### 🎯 Семантика полів: чому `int` — поганий вибір

```go
// ❌ StartTime як int:
// • Втрачається інформація про формат (MJD + BCD time)
// • Неможливо конвертувати у time.Time без додаткової логіки
// • Неможливо серіалізувати назад у бінарний формат

// ✅ Правильний підхід: зберегти сирий формат + методи конвертації
type DVBTime struct {
    MJD      uint16  // Modified Julian Date
    Hour     uint8   // BCD 0-23
    Minute   uint8   // BCD 0-59
    Second   uint8   // BCD 0-59
}

func (t DVBTime) ToTime() time.Time {
    // Конвертація MJD + BCD → time.Time
    // Реалізація за ETSI EN 300 468 Annex B
}

func ParseDVBTime(data []byte) (DVBTime, error) {
    // Парсинг 5 байт: [2 байти MJD][3 байти BCD time]
}

// ❌ Duration як int:
// • Втрачається BCD форматування (0x123456 = 12:34:56)
// • Неможливо перевірити валідність (напр. хвилини >59)

// ✅ Правильний підхід:
type DVBDuration struct {
    Hours   uint8  // BCD 0-99
    Minutes uint8  // BCD 0-59
    Seconds uint8  // BCD 0-59
}

func (d DVBDuration) ToSeconds() uint32 {
    return uint32(d.Hours)*3600 + uint32(d.Minutes)*60 + uint32(d.Seconds)
}

// ❌ RunningStatus як int:
// • Дозволяє значення 5-255, які невалідні за специфікацією

// ✅ Правильний підхід: enum-подібний тип
type RunningStatus uint8

const (
    RunningStatusUndefined RunningStatus = iota
    RunningStatusRunning
    RunningStatusPausing
    RunningStatusNotRunning
    RunningStatusAboutToStart
)

func (rs RunningStatus) IsValid() bool {
    return rs <= RunningStatusAboutToStart
}
```

---

### `EITDescriptor` — порожня структура (критична проблема!)

```go
type EITDescriptor struct {
    // ❌ ПОРОЖНЯ! Дескриптори — це серце EIT, без них таблиця марна
}
```

#### 🎯 Чому дескриптори критично важливі?

```
📋 Типи дескрипторів у EIT (ETSI EN 300 468):
├─ 0x4D short_event          → назва події, короткий опис
├─ 0x4E extended_event       → детальний опис, розширена інформація
├─ 0x50 component            → тип компоненту (відео/аудіо/субтитри)
├─ 0x54 content              → жанр/категорія (спорт, новини, фільми)
├─ 0x55 parental_rating      → віковий рейтинг
├─ 0x5A teletext             → телетекст-сторінки
├─ 0x69 linkage              → посилання на пов'язані послуги
├─ 0x73 time_shifted_event   → посилання на time-shift версію
└─ ... ще 100+ типів

🎯 Без дескрипторів EIT містить тільки:
• Подія #1234, починається о [binary], триває [binary], статус: ?
• ❌ Немає назви! ❌ Немає опису! ❌ Немає жанру!

✅ Правильна структура дескриптора:
type EITDescriptor struct {
    Tag    uint8   // descriptor_tag (0x00-0xFF)
    Length uint8   // descriptor_length (0-255)
    Data   []byte  // payload (Length байт)
}

// ✅ Або типізований підхід для відомих дескрипторів:
type EITDescriptor struct {
    Tag uint8
    Payload DescriptorPayload  // interface{} або enum-based union
}

type DescriptorPayload interface {
    isDescriptorPayload()  // marker method for type safety
}

// Реалізації для конкретних типів:
type ShortEventDescriptor struct {
    LanguageCode string  // 3 байти, ISO 639-2
    EventName    string  // ISO/IEC 8859-5 тощо
    Text         string
}
func (ShortEventDescriptor) isDescriptorPayload() {}
```

---

## ⚠️ Загальні проблеми дизайну типів

### 1️⃣ Відсутність валідації на рівні типів
```go
// ❌ Типи дозволяють невалідні стани:
eit := EITTable{
    SectionLength: 2000,  // ❌ Максимум 1023 за специфікацією!
    Version: 50,          // ❌ Максимум 31 (5 біт)!
    RunningStatus: 10,    // ❌ Максимум 4!
}

// ✅ Додати методи валідації:
func (eit *EITTable) Validate() error {
    if eit.SectionLength > 1023 {
        return fmt.Errorf("SectionLength %d exceeds max 1023", eit.SectionLength)
    }
    if eit.Version > 31 {
        return fmt.Errorf("Version %d exceeds max 31 (5 bits)", eit.Version)
    }
    // ... інші перевірки ...
    return nil
}

// ✅ Або використовувати "неможливі стани" через типи:
type SectionLength uint16  // тип-обгортка

func NewSectionLength(v uint16) (SectionLength, error) {
    if v > 1023 {
        return 0, fmt.Errorf("SectionLength must be <= 1023")
    }
    return SectionLength(v), nil
}
```

### 2️⃣ Відсутність методів парсингу/серіалізації
```go
// ❌ Тільки структури даних, без логіки:
// • Як парсити бінарний потік у EITTable?
// • Як серіалізувати назад у байти для передачі?

// ✅ Додати інтерфейси:
type EITTable interface {
    UnmarshalBinary(data []byte) error  // парсинг з байтів
    MarshalBinary() ([]byte, error)     // серіалізація у байти
    Validate() error                    // валідація
}

// ✅ Або окремі функції-помічники:
func ParseEITTable(data []byte) (*EITTable, error) {
    // Реалізація парсингу за специфікацією
}

func (eit *EITTable) AppendEvent(event EITEvent) error {
    // Додавання події з перевіркою секції не переповненої
}
```

### 3️⃣ Відсутність документації
```go
// ❌ Немає коментарів, що пояснюють семантику полів:
type EITEvent struct {
    EventID int  // ❌ Що це? Де унікальність гарантується?
}

// ✅ Додати документацію за специфікацією:
type EITEvent struct {
    // EventID — унікальний ідентифікатор події в межах service_id.
    // Значення 0x0000 зарезервовано.
    EventID uint16
    
    // StartTime — час початку події у форматі DVB:
    // [16 біт MJD][24 біти BCD time HH:MM:SS]
    // MJD = Modified Julian Date (days since 1858-11-17)
    StartTime DVBTime
    
    // Duration — тривалість у форматі BCD: 0xHHMMSS
    // Приклад: 0x013000 = 1 година 30 хвилин 0 секунд
    Duration DVBDuration
    
    // RunningStatus — статус виконання події:
    // 0=undefined, 1=running, 2=pausing, 3=not running, 4=about to start
    RunningStatus RunningStatus
    
    // FreeCAMode — 0 = free-to-air, 1 = controlled by CA system
    FreeCAMode bool  // ✅ bool краще за int для булевих полів!
    
    // Descriptors — цикл дескрипторів (загальна довжина <= 4095 байт)
    Descriptors []EITDescriptor
}
```

### 4️⃣ Неоптимальне використання пам'яті
```go
// ❌ Поля типу `int` на 64-бітних системах займають 8 байт замість 1-4:
type EITEvent struct {
    EventID           int  // 8 байт на amd64, хоча потрібно 2
    StartTime         int  // 8 байт, хоча потрібно 5
    // ... → ~40 байт замість ~20 на подію
}

// ✅ Використовувати мінімально достатні типи:
type EITEvent struct {
    EventID           uint16  // 2 байти
    StartTime         DVBTime // 5 байт (структура з полями)
    Duration          DVBDuration // 3 байти
    RunningStatus     uint8   // 1 байт (з валідацією)
    FreeCAMode        bool    // 1 байт
    DescriptorsLength uint16  // 2 байти (12 біт + вирівнювання)
    Descriptors       []EITDescriptor
}
// → ~14 байт + дескриптори → значна економія для тисяч подій
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **EPG-синхронізацією** та **багатомовними субтитрами**:

### 🎯 Сценарій: парсинг EIT для програмного гіда
```go
// У EITProcessor при отриманні SI-пакетів:
func (p *EITProcessor) ProcessEITPacket(pkt *TSPacket) error {
    // 🎯 Парсинг PSI секції
    section, err := ParsePSISection(pkt.Payload())
    if err != nil {
        return fmt.Errorf("PSI parse failed: %w", err)
    }
    
    // 🎯 Перевірка table_id діапазону для EIT (0x4E-0x6F)
    if section.TableID < 0x4E || section.TableID > 0x6F {
        return nil  // Не EIT, ігноруємо
    }
    
    // 🎯 Парсинг EIT таблиці
    eit, err := ParseEITTable(section.Data)
    if err != nil {
        return fmt.Errorf("EIT parse failed: %w", err)
    }
    
    // 🎯 Збереження у кеш EPG
    for _, event := range eit.Events {
        p.epgCache.AddEvent(eit.ServiceID, event)
        
        // 🎯 Синхронізація з субтитрами: якщо подія має мовні дескриптори
        if langs := event.GetLanguages(); len(langs) > 0 {
            p.subtitleSync.UpdateLanguages(eit.ServiceID, langs)
        }
    }
    
    return nil
}
```

### 🎯 Сценарій: фільтрація подій за мовою для багатомовного EPG
```go
// У EPGGenerator для формування EPG для конкретного користувача:
func (g *EPGGenerator) GenerateForUser(serviceID uint16, preferredLangs []string) ([]EPGEvent, error) {
    events := g.cache.GetEvents(serviceID)
    var result []EPGEvent
    
    for _, eitEvent := range events {
        // 🎯 Витяг назви/опису з дескрипторів
        shortEvent := eitEvent.FindDescriptor(0x4D) // short_event
        if shortEvent == nil {
            continue  // Подія без назви → пропускаємо
        }
        
        // 🎯 Вибір мови за пріоритетом користувача
        var title, description string
        for _, lang := range preferredLangs {
            if name := shortEvent.GetNameForLanguage(lang); name != "" {
                title = name
                description = shortEvent.GetTextForLanguage(lang)
                break
            }
        }
        if title == "" {
            // Fallback: будь-яка доступна мова
            title = shortEvent.GetAnyName()
        }
        
        // 🎯 Конвертація часу у UTC для клієнта
        startTime := eitEvent.StartTime.ToTime().UTC()
        
        result = append(result, EPGEvent{
            ID:          eitEvent.EventID,
            Title:       title,
            Description: description,
            StartTime:   startTime,
            Duration:    eitEvent.Duration.ToSeconds(),
            Genre:       eitEvent.GetGenre(),  // з content descriptor
            Rating:      eitEvent.GetRating(), // з parental_rating descriptor
        })
    }
    
    return result, nil
}
```

### 🎯 Сценарій: моніторинг цілісності EIT даних
```go
// У monitoring.Monitor для аналізу якості SI-даних:
type EITQualityReport struct {
    ServiceID         uint16
    TotalEvents       int
    EventsMissingTitle int  // Події без short_event descriptor
    EventsMissingTime  int  // Події з невалідним start_time
    CRCErrors         int   // Помилки CRC у секціях
    LastUpdateTime    time.Time
}

func (m *Monitor) AnalyzeEITQuality(serviceID uint16, eitTables []*EITTable) EITQualityReport {
    report := EITQualityReport{
        ServiceID:      serviceID,
        LastUpdateTime: time.Now(),
    }
    
    for _, eit := range eitTables {
        // 🎯 Перевірка CRC
        if err := eit.ValidateCRC(); err != nil {
            report.CRCErrors++
            m.alerts["eit_crc_errors"].Inc()
        }
        
        for _, event := range eit.Events {
            report.TotalEvents++
            
            // 🎯 Перевірка наявності обов'язкових дескрипторів
            if event.FindDescriptor(0x4D) == nil { // short_event
                report.EventsMissingTitle++
            }
            
            // 🎯 Перевірка валідності часу
            if !event.StartTime.IsValid() {
                report.EventsMissingTime++
            }
        }
    }
    
    // 🎯 Алерти при поганій якості
    if report.CRCErrors > 0 {
        m.alerts["eit_data_corruption"].Inc()
    }
    if float64(report.EventsMissingTitle)/float64(report.TotalEvents) > 0.1 {
        m.alerts["eit_incomplete_metadata"].Inc()
    }
    
    return report
}
```

---

## 🧪 Приклад: виправлені типи з методами

```go
// ✅ Виправлені типи з валідацією та корисними методами:

// EITTable — таблиця подій для одного сервісу
type EITTable struct {
    TableID                  uint8   // 0x4E-0x6F для EIT
    SectionSyntaxIndicator   bool    // завжди true для EIT
    SectionLength            uint16  // 0-1023 (12 біт)
    ServiceID                uint16  // ідентифікатор сервісу (каналу)
    Version                  uint8   // 0-31 (5 біт)
    CurrentNextIndicator     bool    // true = актуальна, false = наступна
    SectionNumber            uint8   // 0-based номер секції
    LastSectionNumber        uint8   // номер останньої секції
    SegmentLastSectionNumber uint8   // остання секція у сегменті (опціонально)
    TransportStreamID        uint16  // ✅ Виправлено: 16 біт!
    OriginalNetworkID        uint16  // ✅ Виправлено: 16 біт!
    LastTableID              uint8   // останній table_id у сегменті
    CRC32                    uint32  // ✅ Виправлено: явний uint32
    Events                   []EITEvent
}

// Validate перевіряє відповідність специфікації
func (eit *EITTable) Validate() error {
    if eit.TableID < 0x4E || eit.TableID > 0x6F {
        return fmt.Errorf("invalid EIT table_id: 0x%02X", eit.TableID)
    }
    if eit.SectionLength > 1023 {
        return fmt.Errorf("SectionLength %d exceeds max 1023", eit.SectionLength)
    }
    if eit.Version > 31 {
        return fmt.Errorf("Version %d exceeds 5-bit max 31", eit.Version)
    }
    if eit.SectionNumber > eit.LastSectionNumber {
        return fmt.Errorf("SectionNumber %d > LastSectionNumber %d", 
            eit.SectionNumber, eit.LastSectionNumber)
    }
    return nil
}

// EITEvent — одна подія у програмному гіді
type EITEvent struct {
    EventID           uint16        // унікальний ID в межах service_id
    StartTime         DVBTime       // час початку у DVB форматі
    Duration          DVBDuration   // тривалість у форматі HH:MM:SS BCD
    RunningStatus     RunningStatus // статус виконання (0-4)
    FreeCAMode        bool          // true = закодовано, false = free-to-air
    DescriptorsLength uint16        // загальна довжина дескрипторів (12 біт)
    Descriptors       []EITDescriptor
}

// GetTitle повертає назву події з short_event descriptor
func (e *EITEvent) GetTitle(lang string) string {
    for _, d := range e.Descriptors {
        if d.Tag == 0x4D { // short_event
            return d.ShortEvent().GetNameForLanguage(lang)
        }
    }
    return ""
}

// DVBTime — представлення часу у форматі DVB
type DVBTime struct {
    MJD      uint16 // Modified Julian Date
    Hour     uint8  // BCD 0-23
    Minute   uint8  // BCD 0-59
    Second   uint8  // BCD 0-59
}

// ToTime конвертує у стандартний time.Time (UTC)
func (t DVBTime) ToTime() time.Time {
    // Реалізація за ETSI EN 300 468 Annex B:
    // MJD → year, month, day + BCD time → time.Time
    // (спрощена версія для прикладу)
    return time.Date(1858, 11, 17, 0, 0, 0, 0, time.UTC).
        AddDate(0, 0, int(t.MJD)).
        Add(time.Duration(t.Hour)*time.Hour + 
            time.Duration(t.Minute)*time.Minute + 
            time.Duration(t.Second)*time.Second)
}

// IsValid перевіряє валідність BCD значень
func (t DVBTime) IsValid() bool {
    return t.Hour <= 23 && t.Minute <= 59 && t.Second <= 59
}

// EITDescriptor — базова структура дескриптора
type EITDescriptor struct {
    Tag    uint8   // descriptor_tag (0x00-0xFF)
    Length uint8   // descriptor_length (0-255)
    Data   []byte  // payload
}

// ShortEvent повертає типізовану структуру для descriptor 0x4D
func (d EITDescriptor) ShortEvent() *ShortEventDescriptor {
    if d.Tag != 0x4D || len(d.Data) < 5 {
        return nil
    }
    // Парсинг: [3 байти language][N байт event_name_length + name][M байт text]
    // (реалізація залежить від специфікації кодування тексту)
    return &ShortEventDescriptor{
        LanguageCode: string(d.Data[0:3]),
        // ... парсинг назви та тексту ...
    }
}
```

---

## 📋 Специфікація DVB SI — критичні вимоги до EIT

```
✅ Table ID діапазон:
   • 0x4E: present/following для current TS
   • 0x4F-0x5F: schedule для current TS (до 7 днів)
   • 0x60-0x6F: present/following/schedule для other TS

✅ ServiceID:
   • 16-бітний унікальний ідентифікатор сервісу в межах network
   • 0x0000 зарезервовано, 0xFFFF = всі сервіси

✅ Час у форматі DVB:
   • 5 байт: [2 байти MJD][3 байти BCD HH:MM:SS]
   • MJD = дні з 1858-11-17 (початок модифікованого юліанського календаря)
   • BCD = Binary-Coded Decimal: 0x23 = 23 (десяткове), не 0x17 = 23 (hex)

✅ Дескриптори:
   • Кожен дескриптор: [1 байт tag][1 байт length][N байт data]
   • Максимальна довжина одного дескриптора: 255 байт
   • Загальна довжина циклу дескрипторів у події: ≤ 4095 байт

✅ CRC-32:
   • Polynomial: 0x04C11DB7, Initial: 0xFFFFFFFF, Final XOR: 0xFFFFFFFF
   • Обчислюється на даних секції БЕЗ заголовка PSI та самого CRC
   • Розташовується в кінці секції

✅ Версіонування:
   • Version_number: 5 біт, зростає при зміні вмісту таблиці
   • Current_next_indicator: 1 = застосувати зараз, 0 = застосувати пізніше
   • Клієнти мають відслідковувати версії для уникнення повторної обробки
```

---

## 🎯 Висновок

Ці типи — **початкова основа** для роботи з EIT, але потребують критичних виправлень:

✅ Правильна загальна структура таблиці/події/дескриптора  
✅ Наявність ключових полів (ServiceID, StartTime, Descriptors)

**Критичні виправлення перед використанням**:

1. ✅ **Виправити типи полів**: `TransportStreamID`/`OriginalNetworkID` → `uint16`, `CRC32` → `uint32`
2. ✅ **Додати семантичні типи**: `DVBTime`, `DVBDuration`, `RunningStatus` замість `int`
3. ✅ **Реалізувати `EITDescriptor`** з підтримкою хоча б базових типів (short_event, extended_event)
4. ✅ **Додати методи валідації** (`Validate()`) для перевірки відповідності специфікації
5. ✅ **Додати документацію** з посиланнями на ETSI EN 300 468 для кожного поля
6. ✅ **Реалізувати парсинг/серіалізацію** (`UnmarshalBinary`/`MarshalBinary`)

**Приклад інтеграції у ваш EPG-сервіс**:
```go
// 🎯 EITCache для зберігання та пошуку подій:
type EITCache struct {
    mu sync.RWMutex
    // service_id → список подій, відсортованих за часом
    events map[uint16][]*CachedEITEvent
}

type CachedEITEvent struct {
    EventID   uint16
    StartTime time.Time  // ✅ Вже конвертовано з DVBTime
    Duration  time.Duration  // ✅ Вже конвертовано з DVBDuration
    Title     string     // ✅ Вже витягнуто з дескрипторів
    Genre     string     // ✅ Вже витягнуто з content descriptor
    Languages []string   // ✅ Вже витягнуто з мовних дескрипторів
}

// GetEventsInRange повертає події для EPG у заданому часовому вікні
func (c *EITCache) GetEventsInRange(serviceID uint16, start, end time.Time, langs []string) []EPGEntry {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    events := c.events[serviceID]
    var result []EPGEntry
    
    for _, e := range events {
        if e.StartTime.Before(end) && e.StartTime.Add(e.Duration).After(start) {
            // 🎯 Фільтрація за мовою
            title := e.Title
            for _, lang := range langs {
                if localized := e.GetTitleForLanguage(lang); localized != "" {
                    title = localized
                    break
                }
            }
            
            result = append(result, EPGEntry{
                ID:        e.EventID,
                Title:     title,
                StartTime: e.StartTime,
                Duration:  e.Duration,
                Genre:     e.Genre,
            })
        }
    }
    
    return result
}
```

Потрібно допомогти з:
- 🔗 Реалізацією парсингу EIT з бінарного потоку (PSI секції)?
- 🧠 Типізацією дескрипторів для short_event/extended_event/content?
- 🧪 Написанням тестів з відомими векторами з ETSI EN 300 468?

Чекаю на ваші питання! 🛠️📺🗓️