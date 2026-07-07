# 🔍 Глибокий розбір: парсинг TDT/TOT (Time Date/Offset Table) для DVB SI

Цей код реалізує **парсинг таблиць часу** TDT (Time Date Table) та TOT (Time Offset Table) згідно зі стандартом **ETSI EN 300 468** для отримання абсолютного часу з DVB-потоків. Розберемо архітектурно, з критичним аналізом та практичними рекомендаціями.

---

## 📦 Контекст: навіщо потрібні TDT/TOT?

### Контекст: синхронізація часу у цифровому ТБ
```
📺 DVB SI таблиці часу:
├─ TDT (Table ID 0x70) — базова таблиця часу
│  ├─ Містить: MJD дату + BCD час (години:хвилини:секунди)
│  ├─ Без дескрипторів, без CRC
│  └─ Оновлюється кожні ~30 секунд для точності
│
├─ TOT (Table ID 0x73) — розширена таблиця часу
│  ├─ Містить: все з TDT + дескриптори (напр. time offset для часових зон)
│  ├─ Має CRC-32 для цілісності
│  └─ Використовується для локалізації часу (UTC + offset)
│
└─ Застосування:
   • Відображення точного часу в EPG
   • Синхронізація запису програм за розкладом
   • Кореляція подій між різними джерелами
```

### 🎯 Формат часової мітки (40 біт)
```
📋 MJD + BCD формат (ETSI EN 300 468 Annex B):

[40 біт загальних]
├─ [16 біт] MJD (Modified Julian Date) — дні з 1858-11-17
├─ [ 8 біт] Години у BCD (00-23)
├─ [ 8 біт] Хвилини у BCD (00-59)
└─ [ 8 біт] Секунди у BCD (00-59)

📋 BCD (Binary-Coded Decimal):
• 0x23 = 23 (десяткове), а не 0x17 = 23 (hex)
• Конвертація: (bcd >> 4) * 10 + (bcd & 0x0F)

🎯 Приклад:
RAW = 0x0000C3A5231530
• MJD = 0xC3A5 = 50085 → 2023-06-15
• Hour = 0x23 (BCD) = 23
• Min  = 0x15 (BCD) = 15
• Sec  = 0x30 (BCD) = 30
→ Результат: 2023-06-15 23:15:30 UTC
```

---

## 🔬 Детальний розбір структур даних

### `TDT` — базова структура часу
```go
type TDT struct {
    TableID                byte   // ✅ 8 біт: 0x70 для TDT, 0x73 для TOT
    SectionSyntaxIndicator byte   // ⚠️ 1 біт, але зберігається як byte
    ReservedFutureUse      byte   // ⚠️ 1 біт, але зберігається як byte
    Reserved1              byte   // ⚠️ 2 біти, але зберігається як byte
    SectionLength          uint16 // ✅ 12 біт, але зберігається як uint16
    RAWTimestamp           uint64 // ✅ 40 біт, uint64 достатньо
    
    Timestamp time.Time    // ✅ Зручний формат для використання
}
```

#### ⚠️ Проблеми типів
| Поле | У коді | У специфікації | Наслідок |
|------|--------|---------------|----------|
| `SectionSyntaxIndicator` | `byte` | 1 біт | ⚠️ Зайва пам'ять, але не критично |
| `Reserved*` | `byte` | 1-2 біти | ⚠️ Можна оптимізувати, але не обов'язково |
| `SectionLength` | `uint16` | 12 біт | ✅ Прийнятно, але варто валідувати ≤ 4095 |

#### ✅ Правильні типи (оптимізовані)
```go
type TDT struct {
    TableID                uint8   // ✅ Явний 8-бітний тип
    SectionSyntaxIndicator bool    // ✅ 1 біт як bool
    SectionLength          uint16  // ✅ Зберігаємо як uint16, але валідуємо
    RAWTimestamp           uint64  // ✅ 40 біт у 64-бітному контейнері
    Timestamp              time.Time
}
```

### `TOT` — розширена структура з дескрипторами
```go
type TOT struct {
    TDT  // ✅ Вбудовування TDT — правильний підхід
    Reserved2         byte   // ⚠️ 4 біти
    DescriptorsLength uint16 // ✅ 12 біт
    Descriptors       []struct{}  // ❌ Порожній struct — неможливо парсити!
    CRC32             uint   // ❌ Має бути uint32, не uint!
    
    Timestamp time.Time  // ⚠️ Дублює поле з TDT
}
```

#### ⚠️ Критичні проблеми `TOT`
```go
// ❌ Порожній тип дескрипторів:
Descriptors []struct{}  // ❌ Це слайс порожніх структур — марна пам'ять!
// 📋 Специфікація: дескриптори мають формат [tag:8][length:8][data:N]
// ✅ Правильно: використати існуючий тип з PMT парсингу:
type TOT struct {
    // ...
    Descriptors []ProgramElementDescriptor  // ✅ Повторне використання типу
}

// ❌ Дублювання Timestamp:
type TOT struct {
    TDT  // Вже містить Timestamp
    // ...
    Timestamp time.Time  // ❌ Зайве поле!
}
// ✅ Правильно: використовувати tdt.Timestamp або додати метод:
func (tot TOT) GetTimestamp() time.Time {
    return tot.TDT.Timestamp
}

// ❌ Тип CRC32:
CRC32 uint  // ⚠️ На 64-бітних системах це 8 байт!
// ✅ Правильно:
CRC32 uint32  // ✅ Явний 32-бітний тип
```

---

## 🔬 Детальний розбір `ParseTDT()`

```go
func (p *Packet) ParseTDT(acceptTOT bool) (TDT, error) {
    tdt := TDT{}
    payload, err := p.GetPayload()
    if err != nil {
        return TDT{}, err
    }
    
    // 🎯 Перевірка TableID
    tdt.TableID = payload[1]
    if tdt.TableID != TableID_TimeDateSection && tdt.TableID != TableID_TimeOffsetSection {
        return TDT{}, fmt.Errorf("invalid TableID. expected: 0x70, actual: 0x%02x", tdt.TableID)
    }
    
    // 🎯 Обробка TOT/TDT розрізнення
    if tdt.TableID == TableID_TimeOffsetSection && !acceptTOT {
        return TDT{}, errors.New("This packet is TOT. Set the acceptTOT to true or use ParseTOT")
    }
    
    // 🎯 Парсинг заголовка
    tdt.SectionSyntaxIndicator = (payload[2] >> 7) & 0x01
    tdt.ReservedFutureUse = (payload[2] >> 6) & 0x01
    tdt.Reserved1 = (payload[2] >> 4) & 0x03
    tdt.SectionLength = uint16(payload[2]&0x0F)<<8 | uint16(payload[3])
    
    // 🎯 Парсинг 40-бітного таймштампу
    tdt.RAWTimestamp = uint64(payload[4])<<32 | uint64(payload[5])<<24 | uint64(payload[6])<<16 | uint64(payload[7])<<8 | uint64(payload[8])
    
    // 🎯 Конвертація MJD+BCD → time.Time
    tdt.Timestamp = getTimestampByMJD(tdt.RAWTimestamp)
    return tdt, nil
}
```

#### ✅ Правильні аспекти
```go
// ✅ Перевірка TableID з інформативною помилкою:
if tdt.TableID != TableID_TimeDateSection && ... {
    return TDT{}, fmt.Errorf("invalid TableID. expected: 0x70, actual: 0x%02x", tdt.TableID)
}

// ✅ Гнучкість через acceptTOT прапорець:
// • Дозволяє ParseTDT() обробляти обидва типи таблиць
// • Чітке повідомлення про помилку, якщо потрібен ParseTOT()

// ✅ Правильний парсинг 40-бітного значення:
tdt.RAWTimestamp = uint64(payload[4])<<32 | uint64(payload[5])<<24 | ...
// • 5 байт × 8 біт = 40 біт, правильно зібрані у uint64
```

#### ⚠️ Потенційні проблеми
```go
// ❌ Відсутність перевірки довжини payload перед доступом:
tdt.TableID = payload[1]  // ❌ Якщо len(payload) < 2 → panic!
// ✅ Правильно: перевірити мінімальну довжину
if len(payload) < 9 {  // 1(pointer) + 1(TableID) + 2(flags+length) + 5(timestamp)
    return TDT{}, fmt.Errorf("payload too short for TDT: %d < 9", len(payload))
}

// ❌ Невалідований SectionLength:
tdt.SectionLength = uint16(payload[2]&0x0F)<<8 | uint16(payload[3])
// • Може прийняти значення > 4095 (12 біт максимум)
// ✅ Правильно: валідувати після парсингу
if tdt.SectionLength > 4095 {
    return TDT{}, fmt.Errorf("SectionLength %d exceeds 12-bit max 4095", tdt.SectionLength)
}

// ❌ Ігнорування pointer_field:
// • payload[0] = pointer_field, але код починає з payload[1]
// • Це правильно ТІЛЬКИ якщо pointer_field = 0
// ✅ Правильно: обробити pointer_field як у PAT/PMT парсингу
pointer := payload[0]
tableStart := 1 + int(pointer)
if tableStart+8 > len(payload) {  // 8 байт мінімум для TDT
    return TDT{}, fmt.Errorf("pointer_field %d causes overflow", pointer)
}
tdt.TableID = payload[tableStart]  // ✅ Правильний індекс
```

---

## 🔬 Детальний розбір `ParseTOT()`

```go
func (p *Packet) ParseTOT() (TOT, error) {
    tot := TOT{}
    payload, err := p.GetPayload()
    if err != nil {
        return TOT{}, err
    }
    
    // 🎯 Перевірка TableID для TOT
    tot.TableID = payload[1]
    if tot.TableID != TableID_TimeOffsetSection {
        return TOT{}, ErrPacketIsTDT
    }
    
    // 🎯 Повторний парсинг TDT частини (неефективно!)
    tot.TDT, err = p.ParseTDT(true)
    if err != nil {
        return TOT{}, err
    }
    
    // 🎯 Парсинг специфічних для TOT полів
    tot.Reserved2 = (payload[9] >> 4) & 0x0f
    tot.DescriptorsLength = uint16(payload[9]&0x0f)<<8 | uint16(payload[10])
    
    // ❌ FIXME: дескриптори не реалізовані!
    for i := 0; i < int(tot.DescriptorsLength); i++ {
        // FIXME: implement descriptor
    }
    
    // 🎯 Парсинг CRC-32
    tot.CRC32 = uint(payload[tot.SectionLength])<<24 | uint(payload[tot.SectionLength+1])<<16 | uint(payload[tot.SectionLength+2])<<8 | uint(payload[tot.SectionLength+3])
    
    // 🎯 Повторна конвертація таймштампу (зайва!)
    tot.Timestamp = getTimestampByMJD(tot.RAWTimestamp)
    
    // 🎯 Перевірка CRC
    crc := calculateCRC(payload[1:tot.SectionLength])  // ⚠️ Неправильний діапазон!
    if uint32(tot.CRC32) != crc {
        return TOT{}, errors.New("CRC32 mismatch")
    }
    return tot, nil
}
```

#### ⚠️ Критичні проблеми `ParseTOT()`
```go
// ❌ Неефективний повторний парсинг:
tot.TDT, err = p.ParseTDT(true)  // ❌ Парсить ті самі байти двічі!
// ✅ Правильно: парсити TDT частину один раз і використовувати результат:
// (або винести спільну логіку в окремий helper)

// ❌ Нереалізовані дескриптори:
for i := 0; i < int(tot.DescriptorsLength); i++ {
    // FIXME: implement descriptor  // ❌ Просто пропускаємо!
}
// 📋 TOT може містити важливі дескриптори:
// • time_offset_descriptor (tag 0x4D) — зміщення часової зони
// • local_time_offset_descriptor — локальні зміщення для літнього часу
// ✅ Правильно: або реалізувати, або логувати попередження:
logger.Warn("TOT descriptors not implemented, skipping %d bytes", tot.DescriptorsLength)

// ❌ Неправильний діапазон для CRC:
crc := calculateCRC(payload[1:tot.SectionLength])
// 📋 Як і в PMT: треба враховувати pointer_field!
// ✅ Правильно:
crcStart := 1 + int(payload[0])  // Після pointer_field
crcEnd := crcStart + int(tot.SectionLength)
crcData := payload[crcStart:crcEnd]
storedCRC := binary.BigEndian.Uint32(payload[crcEnd : crcEnd+4])
if storedCRC != calculateCRC(crcData) {
    return TOT{}, fmt.Errorf("CRC32 mismatch")
}

// ❌ Зайва конвертація таймштампу:
tot.Timestamp = getTimestampByMJD(tot.RAWTimestamp)  // ❌ Вже зроблено в ParseTDT!
// ✅ Правильно: використовувати успадковане поле:
// tot.Timestamp вже встановлено через tot.TDT.Timestamp
```

---

## 🔬 Детальний розбір `getTimestampByMJD()` та `bcdToDec()`

```go
func getTimestampByMJD(mjd uint64) time.Time {
    rawDate := mjd >> 24  // ✅ Верхні 16 біт = MJD дата
    mjdOrigin := time.Date(1858, 11, 17, 0, 00, 00, 00, time.UTC)
    mjdDate := mjdOrigin.Add(time.Duration(rawDate) * time.Hour * 24)
    
    // 🎯 Витяг BCD компонентів часу
    hour := bcdToDec(byte((mjd >> 16) & 0xff))  // ✅ Біти [23:16]
    min  := bcdToDec(byte((mjd >> 8) & 0xff))   // ✅ Біти [15:8]
    sec  := bcdToDec(byte((mjd) & 0xff))        // ✅ Біти [7:0]
    
    return mjdDate.Add(time.Duration(hour) * time.Hour).
                 Add(time.Duration(min) * time.Minute).
                 Add(time.Duration(sec) * time.Second)
}

func bcdToDec(bcd byte) byte {
    return bcd>>4*10 + bcd&0x0f  // ✅ Правильна BCD→decimal конвертація
}
```

#### ✅ Правильні аспекти
```go
// ✅ MJD обробка:
// • MJD 0 = 1858-11-17 (початок модифікованого юліанського календаря)
// • rawDate = mjd >> 24 правильно витягує 16-бітну дату
// • time.Duration(rawDate) * 24h правильно конвертує дні

// ✅ BCD парсинг:
// • (mjd >> 16) & 0xff = години (біти 23-16)
// • (mjd >> 8) & 0xff = хвилини (біти 15-8)
// • (mjd) & 0xff = секунди (біти 7-0)
// • bcdToDec правильно конвертує: 0x23 → 23

// ✅ Часова зона:
// • Всі обчислення в UTC (time.UTC) — правильно для DVB
```

#### ⚠️ Потенційні покращення
```go
// ❌ Багаторазові Add() виклики:
return mjdDate.Add(...).Add(...).Add(...)
// • Кожен Add() створює новий time.Time → зайві аллокації
// ✅ Правильно: один Add з сумарною тривалістю:
totalSecs := int64(hour)*3600 + int64(min)*60 + int64(sec)
return mjdDate.Add(time.Duration(totalSecs) * time.Second)

// ❌ Відсутність валідації BCD значень:
// • Що якщо hour = 0x25 (BCD) = 25 (невалідно, максимум 23)?
// ✅ Правильно: додати перевірку:
if hour > 23 || min > 59 || sec > 59 {
    logger.Warn("invalid BCD time values", "h", hour, "m", min, "s", sec)
    // Опціонально: повернути помилку або скоригувати
}

// ❌ Відсутність обробки переповнення:
// • time.Duration(rawDate) * 24h може переповнитися для дуже великих MJD
// ✅ Правильно: використовувати time.AddDate для великих інтервалів:
years := int(rawDate / 365)
days := rawDate % 365
mjdDate := mjdOrigin.AddDate(0, 0, int(rawDate))  // ✅ Безпечніше
```

---

## ⚠️ Загальні проблеми модуля

### 1️⃣ Відсутність валідації вхідного пакету
```go
// ❌ ParseTDT/TOT не перевіряють, чи пакет дійсно містить часову таблицю:
// • Чи PID відповідає очікуваному (зазвичай 0x0014 для TDT/TOT)?
// • Чи PayloadUnitStartIndicator == 1?

// ✅ Додати перевірки:
func (p *Packet) ParseTDT(acceptTOT bool) (TDT, error) {
    // 🎯 Перевірка PID (опціонально, але рекомендовано)
    if p.PID != 0x0014 {  // Стандартний PID для TDT/TOT в ARIB
        logger.Debug("non-standard PID for time table", "pid", p.PID)
    }
    
    payload, err := p.GetPayload()
    if err != nil {
        return TDT{}, fmt.Errorf("failed to get payload: %w", err)
    }
    
    // 🎯 Перевірка мінімальної довжини
    if len(payload) < 9 {
        return TDT{}, fmt.Errorf("payload too short for TDT: %d < 9", len(payload))
    }
    // ...
}
```

### 2️⃣ Неправильна обробка pointer_field
```go
// ❌ Код припускає, що таблиця починається з payload[1]:
tdt.TableID = payload[1]  // ❌ Ігнорує pointer_field!
// 📋 pointer_field може бути >0 для вирівнювання
// ✅ Правильно: як у PAT/PMT парсингу:
pointer := payload[0]
tableStart := 1 + int(pointer)
if tableStart+8 > len(payload) {
    return TDT{}, fmt.Errorf("pointer_field causes overflow")
}
tdt.TableID = payload[tableStart]  // ✅ Правильний індекс
```

### 3️⃣ Відсутність підтримки дескрипторів у TOT
```go
// ❌ Дескриптори TOT критично важливі для локалізації часу:
// • time_offset_descriptor (tag 0x4D) містить:
//   - time_offset (16 біт зі знаком) — зміщення в хвилинах від UTC
//   - future_use біти
// • Без цього: неможливо відобразити локальний час!

// ✅ Реалізувати базовий парсинг time_offset_descriptor:
type TimeOffsetDescriptor struct {
    CountryCode [3]byte  // 24 біти, ISO 3166-1 alpha-3
    TimeOffset  int16    // 16 біт зі знаком, хвилини від UTC
}

func parseTimeOffsetDescriptor(data []byte) (TimeOffsetDescriptor, error) {
    if len(data) < 5 {  // 3 байти country + 2 байти offset
        return TimeOffsetDescriptor{}, fmt.Errorf("descriptor too short")
    }
    return TimeOffsetDescriptor{
        CountryCode: [3]byte{data[0], data[1], data[2]},
        TimeOffset:  int16(uint16(data[3])<<8 | uint16(data[4])),  // Зі знаком!
    }, nil
}
```

### 4️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу
// • Неможливо перевірити коректність MJD конвертації
// • Неможливо покрити edge cases (невалідний BCD, переповнення)

// ✅ Додати мінімальні тести:
func TestGetTimestampByMJD_Valid(t *testing.T) {
    // 🎯 Відомий вектор: 2023-06-15 23:15:30 UTC
    // MJD for 2023-06-15 = 60105 = 0xEA99
    // BCD: 23=0x23, 15=0x15, 30=0x30
    raw := uint64(0xEA99)<<24 | uint64(0x23)<<16 | uint64(0x15)<<8 | uint64(0x30)
    
    result := getTimestampByMJD(raw)
    expected := time.Date(2023, 6, 15, 23, 15, 30, 0, time.UTC)
    
    assert.Equal(t, expected, result)
}

func TestBCDToDec_Valid(t *testing.T) {
    tests := []struct{ bcd, dec byte }{
        {0x00, 0}, {0x09, 9}, {0x10, 10}, {0x23, 23}, {0x59, 59},
    }
    for _, tt := range tests {
        assert.Equal(t, tt.dec, bcdToDec(tt.bcd))
    }
}

func TestBCDToDec_Invalid(t *testing.T) {
    // Невалідний BCD: 0xA5 = 10*10 + 5 = 105 (не існує в реальному часі)
    result := bcdToDec(0xA5)
    assert.Equal(t, byte(105), result)  // Функція не валідує, тільки конвертує
    // ✅ Додати окрему валідацію: isValidBCD(bcd byte) bool
}
```

### 5️⃣ Проблеми з обробкою помилок
```go
// ❌ Загальні помилки без контексту:
return errors.New("CRC32 mismatch")  // ❌ Не зрозуміло, де саме помилка
// ✅ Правильно: додати контекст:
return fmt.Errorf("TOT CRC32 mismatch: stored=0x%08X, computed=0x%08X", 
    tot.CRC32, crc)

// ❌ Використання errors.New замість fmt.Errorf:
return errors.New("This packet is TOT...")  // ❌ Неможливо обгорнути
// ✅ Правильно:
return fmt.Errorf("packet is TOT, use ParseTOT or set acceptTOT=true")
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **синхронізацією аудіо/відео/субтитрів**:

### 🎯 Сценарій: синхронізація системного часу з DVB TDT
```go
// У TimeSync модулі для корекції системного годинника:
type TimeSync struct {
    lastKnownTime time.Time
    maxDrift      time.Duration
    onTimeUpdate  func(old, new time.Time)
    logger        *log.Logger
}

func (ts *TimeSync) ProcessTDT(tdt mpeg2ts.TDT) error {
    // 🎯 Перевірка розумності часу (захист від пошкоджених даних)
    if tdt.Timestamp.Year() < 2000 || tdt.Timestamp.Year() > 2100 {
        return fmt.Errorf("suspicious timestamp: %v", tdt.Timestamp)
    }
    
    // 🎯 Порівняння з локальним часом
    now := time.Now().UTC()
    drift := tdt.Timestamp.Sub(now)
    
    if drift.Abs() > ts.maxDrift {
        ts.logger.Warn("significant time drift detected", 
            "drift", drift,
            "dvb_time", tdt.Timestamp,
            "system_time", now)
        
        // 🎯 Опціонально: скоригувати системний час (потребує прав)
        // або: відправити алерт адміну
        if ts.onTimeUpdate != nil {
            ts.onTimeUpdate(now, tdt.Timestamp)
        }
    }
    
    ts.lastKnownTime = tdt.Timestamp
    return nil
}
```

### 🎯 Сценарій: локалізація часу через TOT дескриптори
```go
// У EPGGenerator для відображення часу в локальній зоні:
type TimeLocale struct {
    utcOffset time.Duration  // Зміщення від UTC
    dstActive bool          // Чи активний літній час
}

func (tl *TimeLocale) ConvertUTC(utc time.Time) time.Time {
    local := utc.Add(tl.utcOffset)
    if tl.dstActive {
        local = local.Add(time.Hour)  // +1 година для літнього часу
    }
    return local
}

// Парсинг time_offset_descriptor з TOT:
func parseTOTWithLocale(payload []byte) (time.Time, TimeLocale, error) {
    tot, err := parseTOT(payload)  // Ваша функція
    if err != nil {
        return time.Time{}, TimeLocale{}, err
    }
    
    locale := TimeLocale{}
    
    // 🎯 Пошук time_offset_descriptor (tag 0x4D)
    for _, desc := range tot.Descriptors {
        if desc.Tag == 0x4D && desc.Length >= 5 {
            // 📋 Формат: [3 байти country][2 байти offset зі знаком]
            offset := int16(uint16(desc.Data[3])<<8 | uint16(desc.Data[4]))
            locale.utcOffset = time.Duration(offset) * time.Minute
            break
        }
    }
    
    return tot.Timestamp, locale, nil
}
```

### 🎯 Сценарій: моніторинг якості часових таблиць
```go
// У monitoring.Monitor для аналізу стабільності часу:
type TimeQualityReport struct {
    LastUpdateTime    time.Time
    TimeDrift         time.Duration
    ConsecutiveErrors int
    Status            string  // "ok", "drifting", "failed"
}

func (m *Monitor) AnalyzeTimeQuality(tdt mpeg2ts.TDT) TimeQualityReport {
    report := TimeQualityReport{
        LastUpdateTime: time.Now(),
    }
    
    // 🎯 Перевірка розумності часу
    if tdt.Timestamp.Year() < 2000 {
        report.ConsecutiveErrors++
        report.Status = "failed"
        m.alerts["time_invalid_year"].Inc()
        return report
    }
    
    // 🎯 Порівняння з попереднім значенням
    if !m.lastTime.IsZero() {
        expected := m.lastTime.Add(time.Since(m.lastUpdateTime))
        drift := tdt.Timestamp.Sub(expected)
        report.TimeDrift = drift
        
        if drift.Abs() > 5*time.Second {
            report.ConsecutiveErrors++
            report.Status = "drifting"
            m.alerts["time_drift"].Inc()
        } else {
            report.ConsecutiveErrors = 0
            report.Status = "ok"
        }
    }
    
    m.lastTime = tdt.Timestamp
    return report
}
```

---

## 🧪 Приклад: рефакторинг `ParseTDT()` з кращою безпекою

```go
// ✅ Безпечний парсинг з повною валідацією:
func (p *Packet) ParseTDT(acceptTOT bool) (TDT, error) {
    payload, err := p.GetPayload()
    if err != nil {
        return TDT{}, fmt.Errorf("failed to get payload: %w", err)
    }
    
    // 🎯 Перевірка мінімальної довжини
    const MinTDTSize = 9  // pointer(1) + TableID(1) + flags+length(2) + timestamp(5)
    if len(payload) < MinTDTSize {
        return TDT{}, fmt.Errorf("payload too short for TDT: %d < %d", len(payload), MinTDTSize)
    }
    
    // 🎯 Обробка pointer_field
    pointer := payload[0]
    tableStart := 1 + int(pointer)
    if tableStart+8 > len(payload) {  // 8 байт після pointer_field
        return TDT{}, fmt.Errorf("pointer_field %d causes overflow", pointer)
    }
    
    tdt := TDT{}
    
    // 🎯 Парсинг заголовка
    tdt.TableID = payload[tableStart]
    if tdt.TableID != TableID_TimeDateSection && tdt.TableID != TableID_TimeOffsetSection {
        return TDT{}, fmt.Errorf("invalid TableID for time table: 0x%02X (expected 0x70 or 0x73)", tdt.TableID)
    }
    
    if tdt.TableID == TableID_TimeOffsetSection && !acceptTOT {
        return TDT{}, fmt.Errorf("packet is TOT (0x73), use ParseTOT() or set acceptTOT=true")
    }
    
    flags := payload[tableStart+1]
    tdt.SectionSyntaxIndicator = (flags >> 7) & 0x01 == 1
    // Reserved біти можна ігнорувати або логувати при невідповідності
    
    tdt.SectionLength = uint16(flags&0x0F)<<8 | uint16(payload[tableStart+2])
    if tdt.SectionLength > 4095 {  // 12 біт максимум
        return TDT{}, fmt.Errorf("SectionLength %d exceeds 12-bit max 4095", tdt.SectionLength)
    }
    
    // 🎯 Парсинг 40-бітного таймштампу
    tsStart := tableStart + 3  // Після заголовка
    if tsStart+5 > len(payload) {
        return TDT{}, fmt.Errorf("timestamp extends beyond payload")
    }
    
    tdt.RAWTimestamp = uint64(payload[tsStart])<<32 | 
                       uint64(payload[tsStart+1])<<24 | 
                       uint64(payload[tsStart+2])<<16 | 
                       uint64(payload[tsStart+3])<<8 | 
                       uint64(payload[tsStart+4])
    
    // 🎯 Конвертація з валідацією
    timestamp, err := convertMJDToTime(tdt.RAWTimestamp)
    if err != nil {
        return TDT{}, fmt.Errorf("invalid timestamp format: %w", err)
    }
    tdt.Timestamp = timestamp
    
    return tdt, nil
}

// ✅ Окрема функція для конвертації з валідацією:
func convertMJDToTime(mjd uint64) (time.Time, error) {
    rawDate := mjd >> 24
    if rawDate < 37685 {  // 1973-01-01 (мінімум для DVB)
        return time.Time{}, fmt.Errorf("MJD %d too old (< 1973)", rawDate)
    }
    
    mjdOrigin := time.Date(1858, 11, 17, 0, 0, 0, 0, time.UTC)
    mjdDate := mjdOrigin.AddDate(0, 0, int(rawDate))
    
    hour := bcdToDec(byte((mjd >> 16) & 0xff))
    min  := bcdToDec(byte((mjd >> 8) & 0xff))
    sec  := bcdToDec(byte(mjd & 0xff))
    
    // 🎯 Валідація часових компонентів
    if hour > 23 || min > 59 || sec > 59 {
        return time.Time{}, fmt.Errorf("invalid BCD time: %02d:%02d:%02d", hour, min, sec)
    }
    
    totalSecs := int64(hour)*3600 + int64(min)*60 + int64(sec)
    return mjdDate.Add(time.Duration(totalSecs) * time.Second), nil
}
```

---

## 📋 Специфікація ETSI EN 300 468 — критичні вимоги

```
✅ Table ID:
   • 0x70: TDT (Time Date Table)
   • 0x73: TOT (Time Offset Table)

✅ Формат часу (40 біт):
   • [16 біт] MJD: дні з 1858-11-17 (Modified Julian Date)
   • [ 8 біт] Години у BCD (00-23)
   • [ 8 біт] Хвилини у BCD (00-59)
   • [ 8 біт] Секунди у BCD (00-59)

✅ BCD кодування:
   • Кожен байт: старші 4 біти = десятки, молодші 4 біти = одиниці
   • Приклад: 0x23 = 2*10 + 3 = 23 (десяткове)

✅ TOT розширення:
   • Після 40-бітного часу: 4 біти reserved + 12 біт descriptors_length
   • Дескриптори: стандартний формат [tag:8][length:8][data:N]
   • CRC-32 в кінці секції

✅ Часова зона:
   • TDT/TDT завжди в UTC
   • Локальний час отримується через time_offset_descriptor у TOT

✅ Оновлення:
   • Рекомендується оновлювати кожні ~30 секунд для точності
   • Клієнти мають відслідковувати version_number для змін
```

---

## 🎯 Висновок

Цей код — **функціональна основа** для парсингу часових таблиць:

✅ Правильна реалізація MJD+BCD конвертації у time.Time  
✅ Гнучкість через acceptTOT прапорець для обробки TDT/TOT  
✅ Валідація TableID для запобігання неправильному парсингу

**Критичні виправлення перед продакшеном**:

1. ✅ **Додати перевірку меж** перед доступом до `payload[...]`
2. ✅ **Обробити pointer_field** правильно (як у PAT/PMT)
3. ✅ **Замінити `uint` на `uint32` для `CRC32`** у TOT
4. ✅ **Реалізувати парсинг time_offset_descriptor** для локалізації часу
5. ✅ **Додати валідацію BCD значень** (години ≤23, хвилини/секунди ≤59)
6. ✅ **Оптимізувати конвертацію часу** (один Add замість трьох)
7. ✅ **Додати тести** для MJD конвертації та edge cases

**Приклад інтеграції у ваш pipeline**:
```go
// 🎯 TimeSync модуль для вашого CCTV процесора:
type TimeSync struct {
    channelID    string
    lastDVBTime  time.Time
    maxDrift     time.Duration
    onDriftAlert func(drift time.Duration, dvbTime, systemTime time.Time)
}

func (ts *TimeSync) ProcessTimePacket(pkt *mpeg2ts.Packet) error {
    // 🎯 Спроба парсингу TDT/TOT
    tdt, err := pkt.ParseTDT(true)  // ✅ Приймаємо обидва типи
    if err != nil {
        // Може бути TOT → спробувати ParseTOT
        if errors.Is(err, mpeg2ts.ErrPacketIsTDT) {
            tot, err := pkt.ParseTOT()
            if err != nil {
                return fmt.Errorf("failed to parse time table: %w", err)
            }
            return ts.processTimestamp(tot.Timestamp, tot.Descriptors)
        }
        return err
    }
    
    return ts.processTimestamp(tdt.Timestamp, nil)  // TDT не має дескрипторів
}

func (ts *TimeSync) processTimestamp(dvbTime time.Time, descriptors []Descriptor) error {
    now := time.Now().UTC()
    drift := dvbTime.Sub(ts.lastDVBTime)
    
    // 🎯 Виявлення аномалій
    if drift.Abs() > ts.maxDrift {
        if ts.onDriftAlert != nil {
            ts.onDriftAlert(drift, dvbTime, now)
        }
        ts.logger.Warn("time drift detected", 
            "channel", ts.channelID,
            "drift", drift,
            "dvb_time", dvbTime,
            "system_time", now)
    }
    
    ts.lastDVBTime = dvbTime
    return nil
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом `ParseTDT()`/`ParseTOT()` з повною валідацією та обробкою pointer_field?
- 🧠 Інтеграцією парсингу time_offset_descriptor для локалізації часу?
- 🧪 Написанням тестів для MJD конвертації з відомими векторами та валідацією країв?

Чекаю на ваші питання! 🛠️⏰📡