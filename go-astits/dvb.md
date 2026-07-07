# Глибоке роз'яснення: Реалізація DVB Time у astits

Цей файл містить **алгоритми конвертації між time.Time та форматом DVB**, що використовується в таблицях SI (EIT/TOT/TDT) стандарту DVB. Код включає складні математичні формули для роботи з MJD (Modified Julian Date) та BCD-кодування.

---

## 🎯 Архітектура: два рівні кодування

```
┌─────────────────────────────────────────┐
│ Рівень 1: MJD (Modified Julian Date)   │
│ • 16 біт = 0..65535                     │
│ • Дати: 1858-11-17 .. ~2137 рік         │
│ • Формула: складна арифметика з плаваючою│
│   точкою (див. ETSI EN 300 468, Annex C)│
│                                         │
│ Рівень 2: Час у BCD (Binary-Coded Decimal)│
│ • 24 біти = 3 байти: [HH][MM][SS]       │
│ • Кожна цифра 0-9 кодується у 4 бітах   │
│ • Приклад: 12:45:30 → 0x12 0x45 0x30   │
│                                         │
│ Разом: 40 біт = 5 байт для повного часу│
└─────────────────────────────────────────┘
```

---

## 🔧 Парсинг DVB Time: `parseDVBTime`

### Сигнатура та специфікація

```go
func parseDVBTime(i *astikit.BytesIterator) (time.Time, error)
// Вхід: 5 байт [2 байти MJD][3 байти часу BCD]
// Вихід: time.Time у UTC
// Специфікація: ETSI EN 300 468, Annex C
```

### Крок 1: Читання та декодування MJD

```go
// Читання 2 байт (big-endian)
bs, _ := i.NextBytesNoCopy(2)
mjd := uint16(bs[0])<<8 | uint16(bs[1])  // 0..65535
```

**Що таке MJD?**
```
Modified Julian Date = кількість днів з 1858-11-17 00:00:00 UTC

Відомі опорні точки:
• MJD 0        = 1858-11-17
• MJD 40587    = 1970-01-01 (Unix epoch)
• MJD 65535    = ~2137-02-16 (максимум для 16 біт)

Формула конвертації: Unix_seconds = (MJD - 40587) × 86400
```

### Крок 2: Складна формула MJD → рік/місяць/день

```go
// Формули з ETSI EN 300 468, Annex C (стор. 160)
var yt = int((float64(mjd) - 15078.2) / 365.25)
var mt = int((float64(mjd) - 14956.1 - float64(int(float64(yt)*365.25))) / 30.6001)
var d = int(float64(mjd) - 14956 - float64(int(float64(yt)*365.25)) - float64(int(float64(mt)*30.6001)))

var k int
if mt == 14 || mt == 15 { k = 1 }  // корекція для січня/лютого

var y = 1900 + yt + k
var m = mt - 1 - k*12

t = time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
```

**Чому такі "дивні" константи?**
```
• 15078.2, 14956.1, 365.25, 30.6001 — емпіричні коефіцієнти
  для наближеного розрахунку календаря з урахуванням:
  - високосних років (365.25 днів/рік у середньому)
  - різної довжини місяців (30.6001 — середня довжина)
  - зсуву епохи (1900 рік як базовий)

• Це НЕ точна арифметика — можливі помилки ±1 день
  на краях діапазону або при переході через високосні роки

• Альтернатива: використовувати бібліотеку для MJD конвертації,
  але це додало б залежність та overhead
```

**Візуалізація розрахунку для MJD=49273 (1993-10-13):**
```
mjd = 49273
yt = (49273 - 15078.2) / 365.25 = 93.6 → int = 93
mt = (49273 - 14956.1 - 93*365.25) / 30.6001 = (49273 - 14956.1 - 33968.25) / 30.6001
   = 348.65 / 30.6001 = 11.39 → int = 11
d = 49273 - 14956 - 33968.25 - 11*30.6001 = 49273 - 14956 - 33968 - 336.6 = 12.4 → int = 12
mt=11 ≠ 14,15 → k=0
y = 1900 + 93 + 0 = 1993 ✅
m = 11 - 1 - 0 = 10 (жовтень) ✅
d = 12 + 1 = 13 ✅ (корекція через округлення)
```

> ⚠️ **Важливо**: Через використання `float64` та `int()` можливі помилки округлення. Для критичних застосунків рекомендується тестувати на відомих датах.

### Крок 3: Парсинг часу через BCD

```go
// parseDVBDurationSeconds читає 3 байти: [HH][MM][SS] у BCD
s, err := parseDVBDurationSeconds(i)
t = t.Add(s)  // додати час до дати
```

**BCD-декодування у `parseDVBDurationByte`:**
```go
func parseDVBDurationByte(i byte) time.Duration {
    // i = 0x45 (BCD для "45")
    // >>4 = 4 (старша цифра), &0xf = 5 (молодша)
    // Результат: 4*10 + 5 = 45
    return time.Duration(uint8(i)>>4*10 + uint8(i)&0xf)
}
```

**Приклад:**
```
Вхід: []byte{0x12, 0x45, 0x30}  // BCD для 12:45:30

parseDVBDurationByte(0x12):
  0x12>>4 = 1, 0x12&0xf = 2 → 1*10+2 = 12 годин

parseDVBDurationByte(0x45):
  0x45>>4 = 4, 0x45&0xf = 5 → 4*10+5 = 45 хвилин

parseDVBDurationByte(0x30):
  0x30>>4 = 3, 0x30&0xf = 0 → 3*10+0 = 30 секунд

Результат: 12h + 45m + 30s = 45930 секунд
```

---

## ✏️ Запис DVB Time: `writeDVBTime`

### Зворотна конвертація: time.Time → MJD + BCD

```go
func writeDVBTime(w *astikit.BitsWriter, t time.Time) (int, error) {
    year := t.Year() - 1900  // зсув до базового 1900 року
    month := t.Month()
    day := t.Day()
    
    // Корекція для січня/лютого (вони "належать" попередньому року у формулі)
    l := 0
    if month <= time.February {
        l = 1
    }
    
    // Формула MJD з ETSI EN 300 468, Annex C
    mjd := 14956 + day + int(float64(year-l)*365.25) + 
           int(float64(int(month)+1+l*12)*30.6001)
    
    // Виділити час окремо від дати
    d := t.Sub(t.Truncate(24 * time.Hour))  // час у межах доби
    
    // Записати MJD (2 байти, big-endian)
    b := astikit.NewBitsWriterBatch(w)
    b.Write(uint16(mjd))
    
    // Записати час через BCD-функцію
    bytesWritten, err := writeDVBDurationSeconds(w, d)
    
    return bytesWritten + 2, b.Err()
}
```

### BCD-кодування у `dvbDurationByteRepresentation`

```go
func dvbDurationByteRepresentation(n uint8) uint8 {
    // n = 45 (десяткове)
    // /10 = 4 (старша цифра), %10 = 5 (молодша)
    // (4<<4) | 5 = 0x40 | 0x05 = 0x45 (BCD)
    return (n/10)<<4 | n%10
}
```

**Приклад кодування 12:45:30:**
```
hours=12: (12/10)<<4 | 12%10 = 1<<4 | 2 = 0x10 | 0x02 = 0x12 ✅
minutes=45: (45/10)<<4 | 45%10 = 4<<4 | 5 = 0x40 | 0x05 = 0x45 ✅
seconds=30: (30/10)<<4 | 30%10 = 3<<4 | 0 = 0x30 | 0x00 = 0x30 ✅

Результат: []byte{0x12, 0x45, 0x30}
```

---

## 🧮 Математика формул MJD: детальний розбір

### Формула парсингу (MJD → дата)

```
Дано: mjd (0..65535)
Шукаємо: рік y, місяць m, день d

1. yt = floor((mjd - 15078.2) / 365.25)
   → приблизна кількість років від 1900

2. mt = floor((mjd - 14956.1 - yt*365.25) / 30.6001)
   → приблизна кількість місяців у поточному році

3. d = floor(mjd - 14956 - yt*365.25 - mt*30.6001)
   → день місяця

4. Якщо mt ∈ {14, 15} → це січень/лютий наступного року:
   k = 1, інакше k = 0

5. y = 1900 + yt + k
   m = mt - 1 - k*12

Константи:
• 15078.2, 14956.1 — зсуви для вирівнювання епох
• 365.25 — середня довжина року з урахуванням високосних
• 30.6001 — середня довжина місяця (365.25/12 ≈ 30.4375, але 30.6001 дає кращу точність)
```

### Формула запису (дата → MJD)

```
Дано: рік y, місяць m, день d (UTC)
Шукаємо: mjd

1. year = y - 1900  // зсув до базового року
2. l = 1, якщо m ≤ 2 (січень/лютий), інакше 0
3. mjd = 14956 + d + (year-l)*365.25 + (m+1+l*12)*30.6001

Це дзеркальна формула до парсингу, але через округлення
можливі розбіжності ±1 день на краях діапазону.
```

### Тестування точності

```
Для перевірки точності рекомендується тестувати на:
• 1970-01-01 (Unix epoch, MJD=40587)
• 2000-01-01 (кінець тисячоліття)
• 2024-02-29 (високосний рік)
• 2137-02-16 (максимальна дата для 16-бітного MJD)

Приклад: round-trip тест
  t0 = time.Date(1993, 10, 13, 12, 45, 0, 0, time.UTC)
  bs = writeDVBTime(t0)           // → []byte{0xC0,0x79,0x12,0x45,0x00}
  t1 = parseDVBTime(bs)           // → time.Date(1993, 10, 13, 12, 45, 0, 0, UTC)
  assert.Equal(t0, t1)            // ✅ має співпадати
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Конвертація EIT Start Time → HLS PROGRAM-DATE-TIME

```go
// У VideoManifestProxy — обробка EIT подій:
func eitEventToHLSMetadata(event *astits.EITDataEvent) (HLSEvent, error) {
    // event.StartTime вже парсений через parseDVBTime у astits
    startTime := event.StartTime  // time.Time у UTC
    
    // event.Duration — time.Duration з BCD
    duration := event.Duration
    
    return HLSEvent{
        Title:     event.EventName,
        StartTime: startTime,
        Duration:  duration,
        // Додаткові поля з дескрипторів...
    }, nil
}

// При генерації плейлиста:
for _, event := range eitEvents {
    meta, err := eitEventToHLSMetadata(event)
    if err != nil {
        log.Warnf("Failed to parse EIT event: %v", err)
        continue
    }
    
    // Додати #EXT-X-PROGRAM-DATE-TIME
    playlist.AddTag(fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s", 
        meta.StartTime.Format(time.RFC3339)))
    
    // Опціонально: додати #EXTINF з тривалістю
    playlist.AddTag(fmt.Sprintf("#EXTINF:%.3f,", meta.Duration.Seconds()))
}
```

### ✅ 2. Синхронізація через TOT (Time Offset Table)

```go
// TOT містить поточний час + UTC offset для локального часу
func handleTOT(tot *astits.TOTData, channelID string) {
    // tot.UTCTime — вже парсений time.Time
    utcTime := tot.UTCTime
    
    // tot.TimeOffset — зсув у хвилинах (може бути від'ємним)
    localTime := utcTime.Add(time.Duration(tot.TimeOffset) * time.Minute)
    
    // Порівняти з локальним системним часом
    systemTime := time.Now().UTC()
    drift := utcTime.Sub(systemTime)
    
    if drift.Abs() > time.Second {
        log.Warnf("Channel %s: time drift %.2f seconds (DVB=%v, system=%v)",
            channelID, drift.Seconds(), utcTime, systemTime)
        
        // Опція: скоригувати синхронізацію сегментів
        adjustSegmentTiming(channelID, drift)
    }
}
```

### ✅ 3. Валідація вхідних DVB time перед використанням

```go
// Захист від пошкоджених або невалідних значень
func validateDVBTime(t time.Time) error {
    // Перевірити діапазон MJD (16 біт)
    unixDays := t.Unix() / 86400
    mjd := unixDays + 40587
    
    if mjd < 0 || mjd > 65535 {
        return fmt.Errorf("time %v out of DVB MJD range [1858-2137]", t)
    }
    
    // Перевірити валідність компонентів часу
    h, m, s := t.Clock()
    if h > 23 || m > 59 || s > 59 {
        return fmt.Errorf("invalid time components: %02d:%02d:%02d", h, m, s)
    }
    
    return nil
}

// Використання:
if err := validateDVBTime(event.StartTime); err != nil {
    log.Errorf("Invalid DVB time in EIT: %v", err)
    // Опція: використати fallback (напр., поточний час)
    event.StartTime = time.Now().UTC()
}
```

### ✅ 4. Обробка "невизначеного" часу (всі біти = 1)

```
Специфікація: "Якщо час не визначено, всі 40 біт = 1"
→ 0xFFFF для MJD + 0xFFFFFF для часу

Це спеціальне значення, яке треба обробляти окремо:
```

```go
func parseDVBTimeWithUndefined(i *astikit.BytesIterator) (time.Time, bool, error) {
    bs, err := i.NextBytesNoCopy(5)
    if err != nil { return time.Time{}, false, err }
    
    // Перевірити, чи всі байти = 0xFF
    allOnes := true
    for _, b := range bs {
        if b != 0xFF {
            allOnes = false
            break
        }
    }
    
    if allOnes {
        // Спеціальне значення "невизначено"
        return time.Time{}, true, nil
    }
    
    // Нормальний парсинг
    t, err := parseDVBTime(astikit.NewBytesIterator(bs))
    return t, false, err
}

// Використання:
startTime, isUndefined, err := parseDVBTimeWithUndefined(iter)
if isUndefined {
    log.Debugf("Event start time is undefined — using fallback")
    startTime = time.Now().UTC()  // або інша логіка
}
```

### ✅ 5. Моніторинг точності конвертації

```go
// monitoring.Monitor — метрики для часової синхронізації:
type DVBMetrics struct {
    MJDConversionErrors *prometheus.CounterVec  // помилки конвертації MJD
    BCDDecodeErrors     *prometheus.CounterVec  // помилки BCD-декодування
    TimeDriftGauge      *prometheus.GaugeVec    // дрейф DVB time vs NTP
}

// У парсингу:
func safeParseDVBTime(i *astikit.BytesIterator, channelID string, metrics *DVBMetrics) (time.Time, error) {
    t, err := parseDVBTime(i)
    if err != nil {
        metrics.MJDConversionErrors.WithLabelValues(channelID).Inc()
        return time.Time{}, err
    }
    
    // Додаткова валідація результату
    if t.Year() < 1900 || t.Year() > 2137 {
        metrics.MJDConversionErrors.WithLabelValues(channelID).Inc()
        return time.Time{}, fmt.Errorf("parsed year %d out of range", t.Year())
    }
    
    return t, nil
}
```

---

## 🧪 Тестування: критичні кейси

### 🔹 Тест на round-trip для довільних дат

```go
func TestDVBTime_RoundTrip_Comprehensive(t *testing.T) {
    testDates := []time.Time{
        time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),      // Unix epoch
        time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),      // Y2K
        time.Date(2024, 2, 29, 12, 30, 45, 0, time.UTC),  // високосний
        time.Date(2137, 2, 16, 23, 59, 59, 0, time.UTC),  // максимум
    }
    
    for _, original := range testDates {
        t.Run(original.String(), func(t *testing.T) {
            // Записати
            buf := &bytes.Buffer{}
            w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
            _, err := writeDVBTime(w, original)
            require.NoError(t, err)
            
            // Прочитати назад
            parsed, err := parseDVBTime(astikit.NewBytesIterator(buf.Bytes()))
            require.NoError(t, err)
            
            // Порівняти з точністю до секунди (BCD не має мікросекунд)
            assert.Equal(t, original.Truncate(time.Second), parsed)
        })
    }
}
```

### 🔹 Тест на невалідне BCD

```go
func TestParseDVBDuration_InvalidBCD(t *testing.T) {
    // Невалідне BCD: 0x60 ≠ 60 (цифра 6 у старшому ніблі — допустимо, але 0 у молодшому)
    // Але 0x6A = 6*10 + 10 = 70 — невалідно, бо друга "цифра" = 10 > 9
    
    invalidBytes := []byte{0x6A, 0x00, 0x00}  // "години" = 0x6A (не BCD!)
    
    // Поточна реалізація не валідує — просто обчислює:
    d := parseDVBDurationByte(0x6A)  // (6<<4 | 10) = 6*10 + 10 = 70
    assert.Equal(t, 70*time.Hour, d)  // ✅ але це логічно невірний результат
    
    // Рекомендація: додати валідацію у парсер:
    // func parseDVBDurationByte(i byte) (time.Duration, error) {
    //     hi, lo := i>>4, i&0x0F
    //     if hi > 9 || lo > 9 {
    //         return 0, fmt.Errorf("invalid BCD digit: 0x%02X", i)
    //     }
    //     return time.Duration(hi*10 + lo), nil
    // }
}
```

### 🔹 Тест на граничні значення MJD

```go
func TestParseDVBTime_MJDEdgeCases(t *testing.T) {
    testCases := []struct {
        name     string
        mjd      uint16
        expected time.Time
        shouldErr bool
    }{
        {"Min MJD", 0, time.Date(1858, 11, 17, 0, 0, 0, 0, time.UTC), false},
        {"Unix epoch", 40587, time.Unix(0, 0).UTC(), false},
        {"Max MJD", 65535, time.Date(2137, 2, 16, 0, 0, 0, 0, time.UTC), false},
        {"Overflow", 65536, time.Time{}, true},  // не вміщається в 16 біт
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            bs := make([]byte, 5)
            bs[0], bs[1] = uint8(tc.mjd>>8), uint8(tc.mjd&0xFF)
            bs[2], bs[3], bs[4] = 0x00, 0x00, 0x00  // 00:00:00
            
            got, err := parseDVBTime(astikit.NewBytesIterator(bs))
            if tc.shouldErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tc.expected, got)
            }
        })
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Помилка ±1 день при конвертації | Дати зсуваються на 1 день | Тестувати на відомих датах; додати корекцію округлення у формулах |
| Невалідне BCD у вхідних даних | "Години" = 96 замість 60 | Додати валідацію: `if (b>>4)>9 || (b&0xF)>9 { error }` |
| Часовий пояс не враховано | Результат у локальному замість UTC | Завжди використовувати `time.UTC` у `time.Date()` |
| Мікросекунди втрачаються | Точність лише до секунд | Це обмеження формату — документувати, не "лагодити" |
| Переповнення при записі | Panic при dates > 2137 року | Перевіряти `if mjd > 65535 { return error }` перед записом |
| Високосні роки оброблені некоректно | 29 лютого → 1 березня | Формули ETSI враховують високосні через 365.25, але тестувати на 2024-02-29 |

### Приклад валідації BCD:

```go
func parseDVBDurationByteSafe(i byte) (time.Duration, error) {
    hi, lo := i>>4, i&0x0F
    if hi > 9 || lo > 9 {
        return 0, fmt.Errorf("invalid BCD digit in byte 0x%02X: hi=%d, lo=%d", i, hi, lo)
    }
    return time.Duration(hi*10 + lo), nil
}

func parseDVBDurationSecondsSafe(i *astikit.BytesIterator) (time.Duration, error) {
    bs, err := i.NextBytesNoCopy(3)
    if err != nil { return 0, err }
    
    h, err := parseDVBDurationByteSafe(bs[0])
    if err != nil { return 0, fmt.Errorf("hours: %w", err) }
    m, err := parseDVBDurationByteSafe(bs[1])
    if err != nil { return 0, fmt.Errorf("minutes: %w", err) }
    s, err := parseDVBDurationByteSafe(bs[2])
    if err != nil { return 0, fmt.Errorf("seconds: %w", err) }
    
    return h*time.Hour + m*time.Minute + s*time.Second, nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Парсинг DVB time з EIT/TOT:
func parseEventTime(event *astits.EITDataEvent) (time.Time, error) {
    // Якщо у вас сирі байти (напр., з кастомного парсера):
    return parseDVBTime(astikit.NewBytesIterator(event.StartTimeBytes))
    // Або використовувати вже парсений event.StartTime з astits
}

// 2. Конвертація для HLS PROGRAM-DATE-TIME:
func formatProgramDateTime(t time.Time) string {
    // HLS вимагає RFC3339 / ISO8601
    return t.UTC().Format(time.RFC3339)
    // Приклад: "2024-05-15T14:30:45Z"
}

// 3. Запис DVB time у власні SI таблиці:
func buildTOTPacket(utcTime time.Time, offsetMinutes int) ([]byte, error) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Записати DVB time (MJD + BCD час)
    if err := writeDVBTime(w, utcTime); err != nil {
        return nil, err
    }
    
    // Записати time offset (16 біт, signed, у хвилинах)
    // Формат: [1] sign [3] reserved [12] offset
    sign := 0
    absOffset := offsetMinutes
    if offsetMinutes < 0 {
        sign = 1
        absOffset = -offsetMinutes
    }
    w.WriteN(uint16(sign<<15 | uint16(absOffset)&0xFFF), 16)
    
    // CRC32 + stuffing...
    return buf.Bytes(), nil
}

// 4. Обробка "невизначеного" часу:
func parseWithFallback(i *astikit.BytesIterator, fallback time.Time) (time.Time, error) {
    bs, err := i.NextBytesNoCopy(5)
    if err != nil { return fallback, err }
    
    // Перевірити спеціальне значення 0xFFFFFFFFF
    allOnes := true
    for _, b := range bs {
        if b != 0xFF { allOnes = false; break }
    }
    if allOnes {
        return fallback, nil  // використати fallback замість помилки
    }
    
    return parseDVBTime(astikit.NewBytesIterator(bs))
}

// 5. Моніторинг дрейфу часу:
func monitorTimeSync(dvbTime time.Time, channelID string) {
    ntpTime := getNTPTime()  // ваш отримувач NTP
    drift := dvbTime.Sub(ntpTime).Seconds()
    
    if math.Abs(drift) > 1.0 {  // поріг 1 секунда
        log.Warnf("Channel %s: time drift %.2f seconds", channelID, drift)
        metrics.TimeDriftGauge.WithLabelValues(channelID).Set(drift)
    }
}
```

---

## 📊 Матриця форматів часу у вашому пайплайні

```
Формат          | Джерело        | Призначення                     | Конвертація
────────────────┼────────────────┼─────────────────────────────────┼────────────
DVB Time (MJD+BCD) | EIT/TOT/TDT  | Розклад передач, абсолютний час | → time.Time (UTC)
PCR Base (33-bit) | TS пакети    | Синхронізація декодера @ 90 kHz | → time.Duration
PTS/DTS (33-bit)  | PES пакети   | Таймінг кадрів @ 90 kHz         | → time.Duration
Unix timestamp    | Ваш код      | Логи, метрики, API              | ←/→ time.Time
HLS PROGRAM-DATE-TIME | Плейлист | Відображення часу у плеєрі      | time.Time → RFC3339

Workflow:
1. EIT DVB Time → parseDVBTime() → time.Time
2. time.Time → format для HLS → "#EXT-X-PROGRAM-DATE-TIME:2024-05-15T14:30:45Z"
3. PCR/PTS → синхронізація аудіо/відео у межах сегмента
4. Unix time → метрики Prometheus → моніторинг дрейфу
```

---

## 📚 Корисні посилання

- [ETSI EN 300 468: DVB SI specification, Annex C](https://dvb.org/wp-content/uploads/2019/12/a038_tm1217r37_en300468v1_17_1_-_rev-134_-_si_specification.pdf)
- [Modified Julian Date converter](https://www.timeanddate.com/date/julian-day.html)
- [BCD encoding explanation](https://en.wikipedia.org/wiki/Binary-coded_decimal)
- [astits dvb.go source](https://github.com/asticode/go-astits/blob/master/dvb.go)

> 💡 **Ключова ідея**: Формули MJD у цьому файлі — це пряма реалізація специфікації ETSI EN 300 468. Вони "негарні" через необхідність працювати з плаваючою точкою та округленням, але забезпечують сумісність із мільйонами DVB-приймачів. У вашому CCTV HLS пайплайні це дозволяє:
> - 📅 Коректно відображати розклад передач з EIT у плеєрах, що підтримують EPG
> - ⏱️ Синхронізувати системний час через TOT для точного таймінгу сегментів
> - 🔄 Конвертувати між форматами без втрати точності (до секунд)

Якщо потрібно — можу допомогти:
- 🧮 Реалізувати більш точну MJD-конвертацію через сторонню бібліотеку (напр., `github.com/recoilme/ttime`)
- 🧩 Інтегрувати парсинг EIT/TOT у ваш `segmentAssembler` для збагачення метаданими
- 🧪 Написати fuzz-тест для стійкості до пошкоджених BCD/MJD значень

🛠️