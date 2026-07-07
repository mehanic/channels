# 🔍 Глибокий розбір коду: парсинг PMT (Program Map Table) та дескрипторів для MPEG-2 TS

Цей код реалізує **детальний парсинг PMT (Program Map Table)** — критичної таблиці у MPEG-2 TS, яка описує склад програм: які PID містять відео, аудіо, субтитри, та які кодеки використовуються. Розберемо архітектурно, з критичним аналізом та практичними рекомендаціями.

---

## 📦 Контекст: що таке PMT і навіщо він потрібен?

### Контекст: ієрархія MPEG-2 TS
```
📦 Транспортний потік (TS)
   │
   ├─ PAT (PID 0x00) → "Програма #1 → шукай PMT на PID 0x100"
   │
   ├─ PMT (PID з PAT, напр. 0x100) → "Програма #1 містить:"
   │  ├─ Відео: PID 0x110, StreamType=0x1B (H.264/AVC)
   │  ├─ Аудіо: PID 0x111, StreamType=0x0F (AAC)
   │  ├─ Субтитри: PID 0x112, StreamType=0x06 (PES private data)
   │  └─ Дескриптори: мова, кодек-параметри, DRM, тощо
   │
   └─ Медіа-пакети на PID з PMT → фактичні відео/аудіо дані
```

### 🎯 Призначення `ParsePMT()`
```go
// 🎯 Вхід: TS пакет з payload, що містить PMT секцію
// 🎯 Вихід: структурована PMT таблиця з:
//   • PCR_PID: PID для Program Clock Reference (синхронізація)
//   • Streams: список елементарних потоків (відео/аудіо/субтитри)
//   • Дескриптори: метадані про кожен потік (мова, кодек, тощо)
// 🎯 Використання:
//   1. Отримати PMT PID з PAT
//   2. Викликати ParsePMT() → отримати список медіа-потоків
//   3. Налаштувати фільтрацію/транскодування для кожного PID
```

---

## 🔬 Детальний розбір структури PMT

### `PMT` — основна структура
```go
type PMT struct {
    // 🎯 Заголовок секції
    Pointer                byte     // pointer_field: зсув до початку таблиці
    TableID                byte     // завжди 0x02 для PMT
    SectionSyntaxIndicator bool     // завжди true для PMT
    SectionLength          uint16   // довжина секції після цього поля
    ProgramNumber          uint16   // номер програми (з PAT)
    Version                byte     // версія таблиці (0-31)
    CurrentNextIndicator   bool     // true = застосувати зараз
    SectionNumber          byte     // номер секції (для multi-section)
    LastSectionNumber      byte     // останній номер секції
    
    // 🎯 Критичні поля
    PCR_PID                PID      // ✅ PID для Program Clock Reference (синхронізація!)
    ProgramInfoLength      uint16   // довжина дескрипторів програми
    Descriptors            []ProgramElementDescriptor  // метадані програми
    
    // 🎯 Список елементарних потоків
    Streams                []StreamInfo  // ✅ Відео/аудіо/субтитри потоки
    CRC32                  uint     // ⚠️ має бути uint32, не uint!
}
```

#### ⚠️ Проблеми типів у `PMT`
| Поле | У коді | У специфікації | Наслідок |
|------|--------|---------------|----------|
| `CRC32` | `uint` | 32 біти | ⚠️ На 64-бітних системах — 8 байт замість 4 |
| `Reserved*` | `byte`/`int` | 2-4 біти | ⚠️ Зайва пам'ять, можлива плутанина |

#### ✅ Правильні типи
```go
type PMT struct {
    // ...
    CRC32 uint32  // ✅ Явний 32-бітний тип для CRC
    // Reserved поля можна видалити або зробити uint8 з масками
}
```

### `StreamInfo` — інформація про елементарний потік
```go
type StreamInfo struct {
    Type          StreamType  // ✅ Тип потоку: 0x1B=H.264, 0x0F=AAC, тощо
    Reserved1     byte        // ⚠️ 3 біти зарезервовано
    ElementaryPID PID         // ✅ PID цього потоку (для фільтрації!)
    Reserved2     byte        // ⚠️ 4 біти зарезервовано
    ESInfoLength  uint16      // ✅ Довжина дескрипторів цього потоку
    Descriptors   []ProgramElementDescriptor  // ✅ Метадані потоку
}
```

#### ✅ Корисний метод: `IsUserPrivateStream()`
```go
func (s *StreamInfo) IsUserPrivateStream() bool {
    return s.Type >= StreamTypeUserPrivateMin && s.Type <= StreamTypeUserPrivateMax
}
// 🎯 Дозволяє виявляти приватні потоки (0x80-0xFF) для спеціальної обробки
```

---

## 🔬 Детальний розбір `ParsePMT()` — крок за кроком

### Крок 1: Отримання payload та pointer_field
```go
func (p *Packet) ParsePMT(disableCRCcheck bool) (PMT, error) {
    pmt := PMT{}
    payload, err := p.GetPayload()
    if err != nil {
        return PMT{}, err
    }
    pmt.Pointer = payload[0]  // 🎯 pointer_field
    // ⚠️ Не перевіряється, чи Pointer в межах payload!
```

#### ⚠️ Потенційна проблема: вихід за межі слайсу
```go
// ❌ Якщо payload[0] = 10, а len(payload) = 5 → payload[10] → panic!
// ✅ Правильно: перевірити межі перед доступом:
if int(pmt.Pointer) >= len(payload) {
    return PMT{}, fmt.Errorf("pointer_field %d exceeds payload length %d", 
        pmt.Pointer, len(payload))
}
tableStart := 1 + int(pmt.Pointer)
if tableStart >= len(payload) {
    return PMT{}, fmt.Errorf("invalid pointer_field: no table data")
}
tableData := payload[tableStart:]
```

---

### Крок 2: Парсинг заголовка секції
```go
    pmt.TableID = payload[1]  // ⚠️ Має бути 0x02 для PMT!
    pmt.SectionSyntaxIndicator = ((payload[2] >> 7) & 0x01) == 1
    if ((payload[2] >> 6) & 0x01) == 1 {  // ⚠️ Завжди має бути 0!
        return PMT{}, fmt.Errorf("invalid format")
    }
    pmt.SectionLength = uint16(payload[2]&0x0F)<<8 | uint16(payload[3])
```

#### ✅ Правильні перевірки заголовка
```go
// ✅ Перевірка TableID:
if pmt.TableID != 0x02 {
    return PMT{}, fmt.Errorf("invalid TableID for PMT: 0x%02X != 0x02", pmt.TableID)
}

// ✅ Перевірка SectionSyntaxIndicator:
if !pmt.SectionSyntaxIndicator {
    return PMT{}, fmt.Errorf("section_syntax_indicator must be 1 for PMT")
}

// ✅ Перевірка зарезервованого біта:
if ((payload[2] >> 6) & 0x01) == 1 {
    return PMT{}, fmt.Errorf("reserved bit must be 0, got 1")
}

// ✅ Перевірка SectionLength:
if pmt.SectionLength < 9+4 {  // Мінімум: 9 байт заголовка + 4 байти CRC
    return PMT{}, fmt.Errorf("SectionLength too short: %d < 13", pmt.SectionLength)
}
if pmt.SectionLength > 1021 {  // Максимум за специфікацією
    return PMT{}, fmt.Errorf("SectionLength too long: %d > 1021", pmt.SectionLength)
}
// ✅ Перевірка, що таблиця вміщується у payload:
expectedEnd := tableStart + int(pmt.SectionLength) + 4  // +4 для CRC
if expectedEnd > len(payload) {
    return PMT{}, fmt.Errorf("PMT section extends beyond payload: %d > %d", 
        expectedEnd, len(payload))
}
```

---

### Крок 3: Парсинг основних полів
```go
    pmt.ProgramNumber = uint16(payload[4])<<8 | uint16(payload[5])
    pmt.Version = (payload[6] >> 1) & 0x1F  // ✅ Правильна маска для 5 біт
    pmt.CurrentNextIndicator = (payload[6] & 0x01) == 0x01
    pmt.SectionNumber = payload[7]
    pmt.LastSectionNumber = payload[8]
    pmt.PCR_PID = PID(uint16(payload[9]&0x1f)<<8 | uint16(payload[10]))  // ✅ 13-бітний PID
    pmt.ProgramInfoLength = uint16(payload[11]&0x0F)<<8 | uint16(payload[12])
```

#### ✅ Додаткові перевірки
```go
// ✅ Перевірка версії (0-31):
if pmt.Version > 31 {
    return PMT{}, fmt.Errorf("invalid version: %d > 31 (5 bits)", pmt.Version)
}

// ✅ Перевірка номерів секцій:
if pmt.SectionNumber > pmt.LastSectionNumber {
    return PMT{}, fmt.Errorf("SectionNumber %d > LastSectionNumber %d", 
        pmt.SectionNumber, pmt.LastSectionNumber)
}

// ✅ Перевірка PCR_PID:
if pmt.PCR_PID > 0x1FFF {
    return PMT{}, fmt.Errorf("invalid PCR_PID: 0x%X > 0x1FFF", pmt.PCR_PID)
}
```

---

### Крок 4: Парсинг дескрипторів програми
```go
    index := 13
    var diff int
    for i := 0; i < int(pmt.ProgramInfoLength); i += diff {
        pmt.Descriptors, diff, err = readDescriptor(payload, index, int(pmt.ProgramInfoLength))
        if err != nil {
            return PMT{}, err
        }
        index += diff
    }
```

#### ⚠️ Проблеми `readDescriptor()`
```go
// ❌ Складна логіка з багатьма switch-кейсами
// • Багато дескрипторів тільки логується "[WARN] not implemented"
// • Немає валідації довжини дескриптора перед доступом до даних

// ✅ Правильно: додати перевірку меж у readDescriptor:
func readDescriptor(payload []byte, startIndex, length int) ([]ProgramElementDescriptor, int, error) {
    endIndex := startIndex + length
    if endIndex > len(payload) {
        return nil, 0, fmt.Errorf("descriptor extends beyond payload: %d > %d", 
            endIndex, len(payload))
    }
    
    for index := startIndex; index < endIndex; {
        if index+2 > endIndex {  // Мінімум: tag(1) + length(1)
            return nil, 0, fmt.Errorf("descriptor header truncated")
        }
        
        tag := payload[index]
        descLen := payload[index+1]
        
        if index+2+int(descLen) > endIndex {
            return nil, 0, fmt.Errorf("descriptor %d length %d exceeds bounds", 
                tag, descLen)
        }
        
        // ... парсинг конкретного дескриптора ...
    }
    // ...
}
```

---

### Крок 5: Парсинг список потоків (Streams)
```go
    // Stream Descriptor
    for index < int(pmt.SectionLength) {
        si := StreamInfo{}
        si.Type = StreamType(payload[index])  // ✅ 8-бітний тип потоку
        si.ElementaryPID = PID(uint16(payload[index+1]&0x1f)<<8 | uint16(payload[index+2]))  // ✅ 13-бітний PID
        si.ESInfoLength = uint16(payload[index+3]&0x0f)<<8 | uint16(payload[index+4])  // ✅ 12-бітна довжина
        index += 5
        
        // 🎯 Парсинг дескрипторів цього потоку
        si.Descriptors, diff, err = readDescriptor(payload, index, int(si.ESInfoLength))
        if err != nil {
            return PMT{}, err
        }
        pmt.Streams = append(pmt.Streams, si)
        index += diff
    }
```

#### ✅ Перевірки для потоків
```go
// ✅ Перевірка валідності StreamType:
if si.Type == StreamTypeReserved {
    return PMT{}, fmt.Errorf("reserved StreamType 0x00 in program")
}
if si.Type >= StreamTypeUserPrivateMin && si.Type <= StreamTypeUserPrivateMax {
    // 🎯 Приватний потік → може потребувати спеціальної обробки
    logger.Debug("user-private stream detected", "type", si.Type)
}

// ✅ Перевірка валідності ElementaryPID:
if si.ElementaryPID > 0x1FFF {
    return PMT{}, fmt.Errorf("invalid ElementaryPID: 0x%X > 0x1FFF", si.ElementaryPID)
}

// ✅ Перевірка узгодженості: якщо StreamType=0x1B (AVC), очікуємо відео-дескриптори
if si.Type == StreamTypeAVC {
    hasAVCDescriptor := false
    for _, desc := range si.Descriptors {
        if desc.Tag == 40 {  // AVC video descriptor
            hasAVCDescriptor = true
            break
        }
    }
    if !hasAVCDescriptor {
        logger.Warn("AVC stream without AVC descriptor", "pid", si.ElementaryPID)
    }
}
```

---

### Крок 6: Парсинг та перевірка CRC-32
```go
    pmt.CRC32 = uint(payload[index])<<24 | uint(payload[index+1])<<16 | uint(payload[index+2])<<8 | uint(payload[index+3])
    
    if disableCRCcheck {
        return pmt, nil
    }
    
    crc := calculateCRC(payload[1:pmt.SectionLength])  // ⚠️ Неправильний діапазон!
    if uint32(pmt.CRC32) != crc {
        return PMT{}, fmt.Errorf("CRC32 mismatch")
    }
    return pmt, nil
```

#### ⚠️ Критичні помилки у CRC обробці
```go
// ❌ Неправильний діапазон для CRC розрахунку:
crc := calculateCRC(payload[1:pmt.SectionLength])
// 📋 Специфікація: CRC обчислюється на даних секції БЕЗ:
// • pointer_field
// • TableID (але ВКЛЮЧАЮЧИ TableID у розрахунок!)
// • ... до кінця секції, НЕ ВКЛЮЧАЮЧИ сам CRC
// 📋 Правильний діапазон:
// • Початок: tableStart (після pointer_field)
// • Кінець: tableStart + pmt.SectionLength (виключаючи 4 байти CRC)
crcStart := tableStart
crcEnd := crcStart + int(pmt.SectionLength)  // Вказує на перший байт CRC
crcData := payload[crcStart:crcEnd]  // Дані для CRC розрахунку

storedCRC := binary.BigEndian.Uint32(payload[crcEnd : crcEnd+4])
computedCRC := calculateCRC(crcData)  // Ваша функція calculateCRC з попереднього огляду
if storedCRC != computedCRC {
    return PMT{}, fmt.Errorf("CRC32 mismatch: stored=0x%08X, computed=0x%08X", 
        storedCRC, computedCRC)
}

// ❌ Тип CRC32: uint замість uint32
pmt.CRC32 = uint(...)  // ⚠️ На 64-бітних системах це 8 байт!
// ✅ Правильно:
pmt.CRC32 = uint32(payload[crcStart])<<24 | 
            uint32(payload[crcStart+1])<<16 | 
            uint32(payload[crcStart+2])<<8 | 
            uint32(payload[crcStart+3])
```

---

## 🔬 Детальний розбір `readDescriptor()` — парсинг дескрипторів

Це **найскладніша функція** модуля. Розберемо ключові аспекти.

### 🎯 Структура дескриптора (загальна)
```
📦 Descriptor:
├─ descriptor_tag: 8 біт (тип дескриптора: 0-255)
├─ descriptor_length: 8 біт (довжина payload: 0-255)
└─ descriptor_data[]: descriptor_length байт (залежить від tag)
```

### 🎯 Реалізовані дескриптори
| Tag | Назва | Реалізація | Статус |
|-----|-------|-----------|--------|
| 2 | video_stream_descriptor | ✅ Повна | Готово |
| 5 | registration_descriptor | ✅ Часткова | Формат identifier + додаткові дані |
| 10 | ISO_639_language_descriptor | ✅ Повна | Мова + тип аудіо |
| 27 | MPEG-4_video_descriptor | ✅ Базова | Тільки profile_and_level |
| 28 | MPEG-4_audio_descriptor | ✅ Базова | Тільки profile_and_level |
| 40 | AVC_video_descriptor | ✅ Повна | Всі прапорці H.264 |
| 0x80-0xFF | User private | ✅ Базова | Збереження сирих даних |

### ⚠️ Нереалізовані дескриптори (попередження)
```go
// ❌ Багато дескрипторів тільки логується:
case ped.Tag == 3: //audio_stream_descriptor
    fmt.Println("[WARN] not implemented", ped.Tag)
    diff += int(ped.Length)  // ⚠️ Просто пропускаємо!
```

#### Проблеми такого підходу
```go
// ❌ Втрата важливих метаданих:
// • audio_stream_descriptor (tag=3) містить:
//   - free_format_flag, sampling_frequency, bitrate, тощо
// • Без цього: неможливо правильно налаштувати аудіо декодер!

// ✅ Правильно: або реалізувати, або явно ігнорувати з логуванням:
case ped.Tag == 3: //audio_stream_descriptor
    logger.Warn("audio_stream_descriptor not implemented, skipping", 
        "tag", ped.Tag, "length", ped.Length)
    // 🎯 Зберегти сирі дані для майбутньої обробки
    ped.RawData = payload[index+2 : index+2+int(ped.Length)]
    diff += int(ped.Length)
```

### 🎯 Приклад: парсинг AVC video descriptor (tag=40)
```go
case ped.Tag == 40: // AVC video descriptor
    ped.AVCVideoDescriptor.ProfileIDC = payload[index+2]
    ped.AVCVideoDescriptor.ConstraintSet0Flag = ((payload[index+3] >> 7) & 0x01) == 1
    // ... ще 10+ прапорців ...
    ped.AVCVideoDescriptor.LevelIDC = payload[index+4]
    // ...
    diff += 4  // ✅ Правильно: заголовок(2) + дані(4) = 6 байт
```

#### ✅ Правильний бітовий парсинг
```go
// 📋 AVC video descriptor структура (ITU-T H.222.0 Table 2-40):
// [2]: profile_idc (8 біт)
// [3]: constraint_set0_flag(1) + constraint_set1_flag(1) + ... + reserved(2)
// [4]: level_idc (8 біт)
// [5]: AVC_still_present(1) + AVC_24_hour_picture_flag(1) + ... + reserved(5)

// ✅ Код правильно парсить бітові поля через маски та зсуви:
ConstraintSet0Flag = ((payload[index+3] >> 7) & 0x01) == 1  // ✅ Найстарший біт
ConstraintSet1Flag = ((payload[index+3] >> 6) & 0x01) == 1  // ✅ Наступний біт
// ...
AVCCompatibleFlags = (payload[index+3] & 0x03)  // ✅ Наймолодші 2 біти
```

---

## ⚠️ Загальні проблеми модуля

### 1️⃣ Відсутність валідації вхідного пакету
```go
// ❌ ParsePMT() не перевіряє, чи пакет дійсно містить PMT:
// • Чи PID відповідає очікуваному (з PAT)?
// • Чи PayloadUnitStartIndicator == 1?
// • Чи TableID == 0x02?

// ✅ Додати перевірки на початку:
func (p *Packet) ParsePMT(disableCRCcheck bool) (PMT, error) {
    // 🎯 Перевірка, що це PMT пакет
    if p.TableID != 0x02 {  // Якщо є таке поле в Packet
        return PMT{}, fmt.Errorf("not a PMT packet: TableID=0x%X", p.TableID)
    }
    if !p.PayloadUnitStartIndicator {
        return PMT{}, fmt.Errorf("PMT must start at payload boundary: PUSI=0")
    }
    
    payload, err := p.GetPayload()
    if err != nil {
        return PMT{}, fmt.Errorf("failed to get payload: %w", err)
    }
    // ... решта коду ...
}
```

### 2️⃣ Жорстке припущення про розташування таблиці
```go
// ❌ Код припускає, що PMT починається з payload[1]:
pmt.TableID = payload[1]  // ❌ Ігнорує pointer_field!
// 📋 pointer_field може бути >0, якщо перед таблицею є вирівнюючі байти
// ✅ Правильно: використовувати tableStart = 1 + pointer_field
```

### 3️⃣ Неповна реалізація дескрипторів
```go
// ❌ Багато важливих дескрипторів не реалізовані:
// • audio_stream_descriptor (tag=3) — критично для аудіо налаштувань
// • CA_descriptor (tag=9) — для DRM/умовного доступу
// • Maximum_bitrate_descriptor (tag=14) — для ABR логіки
// • HEVC_video_descriptor (tag=56) — для H.265 підтримки

// ✅ Правильно: або реалізувати, або документувати обмеження:
// 📋 Підтримка дескрипторів:
// ✅ Реалізовано: video_stream(2), registration(5), ISO639(10), AVC(40)
// ❌ Не реалізовано: audio_stream(3), CA(9), HEVC(56), тощо
// Якщо потрібен нереалізований дескриптор → повертати помилку або логувати
```

### 4️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу для PMT парсингу
// • Неможливо перевірити коректність бітового парсингу
// • Неможливо покрити edge cases (пошкоджені CRC, невалідні PID)

// ✅ Додати мінімальні тести:
func TestParsePMT_Valid(t *testing.T) {
    // 🎯 Створити моковий PMT пакет з відомими даними
    pkt := createMockPMTPacket([]StreamInfo{
        {Type: StreamTypeAVC, ElementaryPID: 0x110},
        {Type: StreamTypeISO13818_7_AudioWithADTS, ElementaryPID: 0x111},
    })
    
    pmt, err := pkt.ParsePMT(false)  // ✅ З перевіркою CRC
    require.NoError(t, err)
    assert.Equal(t, uint16(1), pmt.ProgramNumber)
    assert.Len(t, pmt.Streams, 2)
    assert.Equal(t, PID(0x110), pmt.Streams[0].ElementaryPID)
}

func TestParsePMT_InvalidCRC(t *testing.T) {
    pkt := createMockPMTPacketWithBadCRC()
    _, err := pkt.ParsePMT(false)  // ✅ З перевіркою CRC
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "CRC32 mismatch")
}
```

### 5️⃣ Проблеми з обробкою `disableCRCcheck`
```go
// ❌ Прапорець disableCRCcheck може приховати критичні помилки:
if disableCRCcheck {
    return pmt, nil  // ✅ Повертаємо без перевірки цілісності!
}
// 📋 У продакшені: CRC має перевірятися завжди!
// ✅ Правильно: або видалити прапорець, або логувати попередження:
if disableCRCcheck {
    logger.Warn("CRC check disabled for PMT parsing", "program", pmt.ProgramNumber)
    return pmt, nil
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **WebSocket-приймачем TS-фрагментів**:

### 🎯 Сценарій: ініціалізація каналу через PAT/PMT
```go
// У ChannelInitializer при підключенні нового CCTV потоку:
func (ci *ChannelInitializer) DiscoverStreams(patPID, pmtPID mpeg2ts.PID) error {
    // 🎯 Парсинг PAT для отримання PMT PID (якщо не відомо заздалегідь)
    if pmtPID == 0 {
        patPacket := getPacketByPID(patPID)  // Ваша функція
        pat, err := patPacket.ParsePAT()
        if err != nil {
            return fmt.Errorf("PAT parse failed: %w", err)
        }
        // 🎯 Знайти основну програму (ProgramNumber == 1)
        for _, prog := range pat.Programs {
            if prog.ProgramNumber == 1 {
                pmtPID = prog.ProgramMapPID
                break
            }
        }
        if pmtPID == 0 {
            return fmt.Errorf("main program not found in PAT")
        }
    }
    
    // 🎯 Парсинг PMT для отримання медіа-потоків
    pmtPacket := getPacketByPID(pmtPID)
    pmt, err := pmtPacket.ParsePMT(false)  // ✅ Завжди з CRC перевіркою!
    if err != nil {
        return fmt.Errorf("PMT parse failed: %w", err)
    }
    
    // 🎯 Налаштування фільтрації для кожного потоку
    for _, stream := range pmt.Streams {
        switch stream.Type {
        case mpeg2ts.StreamTypeAVC:  // H.264 відео
            ci.videoPID = stream.ElementaryPID
            ci.videoCodec = "h264"
            // 🎯 Парсинг AVC descriptor для profile/level
            for _, desc := range stream.Descriptors {
                if desc.Tag == 40 {  // AVC video descriptor
                    ci.h264Profile = desc.AVCVideoDescriptor.ProfileIDC
                    ci.h264Level = desc.AVCVideoDescriptor.LevelIDC
                    break
                }
            }
            
        case mpeg2ts.StreamTypeISO13818_7_AudioWithADTS:  // AAC аудіо
            ci.audioPID = stream.ElementaryPID
            ci.audioCodec = "aac"
            // 🎯 Парсинг мови з ISO639 descriptor
            for _, desc := range stream.Descriptors {
                if desc.Tag == 10 {  // ISO639 language descriptor
                    for _, lang := range desc.ISO639LanguageDescriptor.Languages {
                        ci.audioLanguage = iso639CodeToString(lang.ISO639LanguageCode)
                        break
                    }
                }
            }
        }
    }
    
    ci.logger.Info("channel initialized", 
        "video_pid", ci.videoPID, "audio_pid", ci.audioPID,
        "video_codec", ci.videoCodec, "audio_codec", ci.audioCodec)
    
    return nil
}
```

### 🎯 Сценарій: фільтрація потоків за типом
```go
// У StreamRouter для розділення відео/аудіо/субтитрів:
type StreamRouter struct {
    videoPID   mpeg2ts.PID
    audioPID   mpeg2ts.PID
    subtitlePID mpeg2ts.PID  // Опціонально
    pcrPID     mpeg2ts.PID  // Для синхронізації
}

func (r *StreamRouter) RoutePacket(pkt *mpeg2ts.Packet) StreamType {
    switch pkt.PID {
    case r.videoPID:
        return StreamVideo
    case r.audioPID:
        return StreamAudio
    case r.subtitlePID:
        return StreamSubtitle
    case r.pcrPID:
        return StreamPCR  // Для A/V синхронізації
    default:
        return StreamOther
    }
}

// Використання з PMT:
func (r *StreamRouter) ConfigureFromPMT(pmt mpeg2ts.PMT) {
    for _, stream := range pmt.Streams {
        switch stream.Type {
        case mpeg2ts.StreamTypeAVC, mpeg2ts.StreamTypeISO13818_2_Video:
            r.videoPID = stream.ElementaryPID
        case mpeg2ts.StreamTypeISO13818_7_AudioWithADTS, mpeg2ts.StreamTypeISO14496_3_Audio:
            r.audioPID = stream.ElementaryPID
        case mpeg2ts.StreamTypeISO13818_1_PES:  // Приватні дані → субтитри
            r.subtitlePID = stream.ElementaryPID
        }
    }
    r.pcrPID = pmt.PCR_PID  // ✅ Важливо для синхронізації!
}
```

### 🎯 Сценарій: моніторинг якості метаданих
```go
// У monitoring.Monitor для аналізу якості PMT даних:
type PMTQualityReport struct {
    ProgramNumber      uint16
    TotalStreams       int
    VideoStreams       int
    AudioStreams       int
    MissingDescriptors int  // Потоки без важливих дескрипторів
    CRCErrors          int
    Status             string  // "ok", "degraded", "failed"
}

func (m *Monitor) AnalyzePMTQuality(pmt mpeg2ts.PMT) PMTQualityReport {
    report := PMTQualityReport{
        ProgramNumber: pmt.ProgramNumber,
        TotalStreams:  len(pmt.Streams),
    }
    
    for _, stream := range pmt.Streams {
        // 🎯 Класифікація потоків
        switch {
        case isVideoStreamType(stream.Type):
            report.VideoStreams++
            // 🎯 Перевірка наявності відео-дескрипторів
            if stream.Type == mpeg2ts.StreamTypeAVC {
                hasAVC := false
                for _, desc := range stream.Descriptors {
                    if desc.Tag == 40 {  // AVC video descriptor
                        hasAVC = true
                        break
                    }
                }
                if !hasAVC {
                    report.MissingDescriptors++
                    m.alerts["pmt_missing_avc_descriptor"].Inc()
                }
            }
        case isAudioStreamType(stream.Type):
            report.AudioStreams++
            // 🎯 Перевірка наявності мовних дескрипторів
            hasLanguage := false
            for _, desc := range stream.Descriptors {
                if desc.Tag == 10 {  // ISO639 language descriptor
                    hasLanguage = true
                    break
                }
            }
            if !hasLanguage {
                report.MissingDescriptors++
                m.alerts["pmt_missing_language_descriptor"].Inc()
            }
        }
    }
    
    // 🎯 Визначення статусу
    if report.MissingDescriptors > report.TotalStreams/2 {
        report.Status = "degraded"
        m.alerts["pmt_incomplete_metadata"].Inc()
    } else {
        report.Status = "ok"
    }
    
    return report
}
```

---

## 🧪 Приклад: рефакторинг `ParsePMT()` з кращою безпекою

```go
// ✅ Безпечний парсинг з повною валідацією:
func (p *Packet) ParsePMT(disableCRCcheck bool) (PMT, error) {
    // 🎯 Перевірка, що це потенційно PMT пакет
    if p.PID == 0x00 {  // PAT PID
        return PMT{}, fmt.Errorf("PID 0x00 is PAT, not PMT")
    }
    
    payload, err := p.GetPayload()
    if err != nil {
        return PMT{}, fmt.Errorf("failed to get payload: %w", err)
    }
    if len(payload) < 13 {  // Мінімум: pointer(1) + заголовок(12)
        return PMT{}, fmt.Errorf("payload too short for PMT: %d < 13", len(payload))
    }
    
    pmt := PMT{}
    
    // 🎯 Обробка pointer_field
    pointer := payload[0]
    tableStart := 1 + int(pointer)
    if tableStart >= len(payload) {
        return PMT{}, fmt.Errorf("pointer_field %d exceeds payload length %d", 
            pointer, len(payload))
    }
    pmt.Pointer = pointer
    
    // 🎯 Парсинг заголовка секції
    tableData := payload[tableStart:]
    if len(tableData) < 12 {  // Заголовок без pointer_field
        return PMT{}, fmt.Errorf("table data too short: %d < 12", len(tableData))
    }
    
    pmt.TableID = tableData[0]
    if pmt.TableID != 0x02 {
        return PMT{}, fmt.Errorf("invalid TableID for PMT: 0x%02X != 0x02", pmt.TableID)
    }
    
    flags := tableData[1]
    pmt.SectionSyntaxIndicator = (flags >> 7) & 0x01 == 1
    if !pmt.SectionSyntaxIndicator {
        return PMT{}, fmt.Errorf("section_syntax_indicator must be 1 for PMT")
    }
    if (flags >> 6) & 0x01 == 1 {
        return PMT{}, fmt.Errorf("reserved bit must be 0, got 1")
    }
    
    pmt.SectionLength = uint16(flags&0x0F)<<8 | uint16(tableData[2])
    if pmt.SectionLength < 9+4 {  // 9 байт заголовка + 4 байти CRC
        return PMT{}, fmt.Errorf("SectionLength too short: %d < 13", pmt.SectionLength)
    }
    if pmt.SectionLength > 1021 {
        return PMT{}, fmt.Errorf("SectionLength too long: %d > 1021", pmt.SectionLength)
    }
    
    // 🎯 Перевірка, що таблиця вміщується у payload
    expectedEnd := tableStart + int(pmt.SectionLength) + 4  // +4 для CRC
    if expectedEnd > len(payload) {
        return PMT{}, fmt.Errorf("PMT section extends beyond payload: %d > %d", 
            expectedEnd, len(payload))
    }
    
    // 🎯 Парсинг основних полів
    pmt.ProgramNumber = uint16(tableData[3])<<8 | uint16(tableData[4])
    pmt.Version = (tableData[5] >> 1) & 0x1F
    if pmt.Version > 31 {
        return PMT{}, fmt.Errorf("invalid version: %d > 31", pmt.Version)
    }
    pmt.CurrentNextIndicator = (tableData[5] & 0x01) == 0x01
    pmt.SectionNumber = tableData[6]
    pmt.LastSectionNumber = tableData[7]
    if pmt.SectionNumber > pmt.LastSectionNumber {
        return PMT{}, fmt.Errorf("SectionNumber %d > LastSectionNumber %d", 
            pmt.SectionNumber, pmt.LastSectionNumber)
    }
    
    pmt.PCR_PID = PID((uint16(tableData[8]&0x1F) << 8) | uint16(tableData[9]))
    if pmt.PCR_PID > 0x1FFF {
        return PMT{}, fmt.Errorf("invalid PCR_PID: 0x%X > 0x1FFF", pmt.PCR_PID)
    }
    
    pmt.ProgramInfoLength = uint16(tableData[10]&0x0F)<<8 | uint16(tableData[11])
    
    // 🎯 Парсинг дескрипторів програми
    index := 12  // Після заголовка
    for i := uint16(0); i < pmt.ProgramInfoLength; {
        if index >= len(tableData) {
            return PMT{}, fmt.Errorf("program descriptor extends beyond table")
        }
        desc, diff, err := readDescriptor(tableData, index, int(pmt.ProgramInfoLength)-int(i))
        if err != nil {
            return PMT{}, fmt.Errorf("program descriptor parse failed: %w", err)
        }
        pmt.Descriptors = append(pmt.Descriptors, desc...)
        index += diff
        i += uint16(diff)
    }
    
    // 🎯 Парсинг список потоків
    for index < int(pmt.SectionLength)-4 {  // -4 для CRC
        if index+5 > len(tableData) {
            return PMT{}, fmt.Errorf("stream info header truncated")
        }
        
        si := StreamInfo{}
        si.Type = StreamType(tableData[index])
        if si.Type == StreamTypeReserved {
            return PMT{}, fmt.Errorf("reserved StreamType 0x00 in program")
        }
        
        si.ElementaryPID = PID((uint16(tableData[index+1]&0x1F) << 8) | uint16(tableData[index+2]))
        if si.ElementaryPID > 0x1FFF {
            return PMT{}, fmt.Errorf("invalid ElementaryPID: 0x%X > 0x1FFF", si.ElementaryPID)
        }
        
        si.ESInfoLength = uint16(tableData[index+3]&0x0F)<<8 | uint16(tableData[index+4])
        index += 5
        
        // 🎯 Парсинг дескрипторів потоку
        for i := uint16(0); i < si.ESInfoLength; {
            if index >= len(tableData) {
                return PMT{}, fmt.Errorf("stream descriptor extends beyond table")
            }
            desc, diff, err := readDescriptor(tableData, index, int(si.ESInfoLength)-int(i))
            if err != nil {
                return PMT{}, fmt.Errorf("stream descriptor parse failed: %w", err)
            }
            si.Descriptors = append(si.Descriptors, desc...)
            index += diff
            i += uint16(diff)
        }
        
        pmt.Streams = append(pmt.Streams, si)
    }
    
    // 🎯 Парсинг та перевірка CRC-32
    crcStart := tableStart + int(pmt.SectionLength)
    if crcStart+4 > len(payload) {
        return PMT{}, fmt.Errorf("CRC32 extends beyond payload")
    }
    
    storedCRC := binary.BigEndian.Uint32(payload[crcStart : crcStart+4])
    pmt.CRC32 = storedCRC  // ✅ Зберігаємо як uint32
    
    if !disableCRCcheck {
        crcData := payload[tableStart : crcStart]  // Дані для CRC розрахунку
        computedCRC := calculateCRC(crcData)
        if storedCRC != computedCRC {
            return PMT{}, fmt.Errorf("CRC32 mismatch: stored=0x%08X, computed=0x%08X", 
                storedCRC, computedCRC)
        }
    }
    
    return pmt, nil
}
```

---

## 📋 Специфікація MPEG-2 TS — критичні вимоги до PMT

```
✅ Table ID: завжди 0x02 для PMT
✅ PID: змінний, визначається в PAT таблиці
✅ SectionSyntaxIndicator: завжди 1 для PMT
✅ SectionLength: 12 біт, максимум 1021 (після цього поля до кінця секції, не включаючи CRC)
✅ ProgramNumber: 16 біт, ідентифікатор програми (з PAT)
✅ Version_number: 5 біт, зростає при зміні вмісту таблиці (модуль 32)
✅ Current_next_indicator: 1 = застосувати зараз, 0 = наступна версія
✅ PCR_PID: 13 біт, PID для Program Clock Reference (синхронізація)
✅ Program_info_length: 12 біт, загальна довжина дескрипторів програми
✅ Stream loop: кожен запис = 5 байт заголовка + N байт дескрипторів
✅ Stream_type: 8 біт, визначає тип елементарного потоку (0x1B=H.264, 0x0F=AAC, тощо)
✅ Elementary_PID: 13 біт, PID цього елементарного потоку
✅ ES_info_length: 12 біт, довжина дескрипторів цього потоку
✅ CRC-32: polynomial 0x04C11DB7, initial 0xFFFFFFFF, final XOR 0xFFFFFFFF, MSB-first
✅ pointer_field: 0-255, вказує зсув до початку таблиці від payload[1]
```

---

## 🎯 Висновок

Цей `ParsePMT()` — **детальна реалізація** парсингу PMT з підтримкою багатьох дескрипторів:

✅ Правильна загальна структура парсингу за специфікацією ITU-T H.222.0  
✅ Підтримка критичних дескрипторів: AVC video, ISO639 language, registration  
✅ Валідація CRC-32 для цілісності даних

**Критичні виправлення перед продакшеном**:

1. ✅ **Виправити діапазон для CRC розрахунку** (виключати сам CRC)
2. ✅ **Замінити `uint` на `uint32` для `CRC32` поля**
3. ✅ **Додати перевірку меж** перед доступом до `payload[...]`
4. ✅ **Виправити hardcoded індекси** → використовувати `tableStart + offset`
5. ✅ **Додати валідацію вхідного пакету** (PID, PUSI, TableID)
6. ✅ **Покращити обробку нереалізованих дескрипторів** (збереження сирих даних)
7. ✅ **Додати тести** для валідних/невалідних PMT з відомими векторами

**Приклад інтеграції у ваш pipeline**:
```go
// 🎯 PMTProcessor для вашого WebSocket-сервера:
type PMTProcessor struct {
    channelID string
    onStreamDiscovered func(streamType StreamType, pid PID, descriptors []Descriptor)
    logger *log.Logger
}

func (p *PMTProcessor) ProcessPacket(pkt *mpeg2ts.Packet) error {
    // 🎯 Перевірка, що це потенційно PMT (за PID з PAT)
    if !p.isExpectedPMTPID(pkt.PID) {
        return nil
    }
    
    pmt, err := pkt.ParsePMT(false)  // ✅ Завжди з CRC перевіркою!
    if err != nil {
        p.logger.Warn("PMT parse failed", 
            "channel", p.channelID, 
            "error", err)
        return nil  // Не зупиняти потік через помилку метаданих
    }
    
    // 🎯 Сповіщення про знайдені потоки
    for _, stream := range pmt.Streams {
        if p.onStreamDiscovered != nil {
            p.onStreamDiscovered(stream.Type, stream.ElementaryPID, stream.Descriptors)
        }
        p.logger.Info("discovered stream", 
            "channel", p.channelID,
            "type", stream.Type.String(),  // Потребує методу String()
            "pid", stream.ElementaryPID)
    }
    
    return nil
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом `ParsePMT()` з повною валідацією та безпечним доступом до слайсів?
- 🧠 Інтеграцією дескрипторного парсингу для отримання кодек-параметрів (H.264 profile/level)?
- 🧪 Написанням тестів з відомими векторами з специфікації та fuzz-тестів для країв?

Чекаю на ваші питання! 🛠️📡🎬