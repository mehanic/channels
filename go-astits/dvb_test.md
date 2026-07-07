# Глибоке роз'яснення: Парсинг та запис DVB-часу у astits

Цей файл тестує **функції роботи з DVB-форматами часу**, які використовуються в таблицях SI (Service Information) стандарту DVB: EIT (Event Information Table), TOT (Time Offset Table), TDT (Time Date Table).

---

## 🎯 Навіщо це потрібно?

```
┌─────────────────────────────────────────┐
│ DVB Time у контексті телебачення:      │
│                                         │
│ 🔹 Формат часу в EIT/TOT/TDT таблицях  │
│   • Не Unix timestamp, не ISO 8601     │
│   • Спеціальний 40-бітний BCD-кодований│
│     формат, визначений в ETSI EN 300 468│
│                                         │
│ 🔹 Призначення:                         │
│   • Розклад передач (EIT)              │
│   • Поточний час мовлення (TOT/TDT)    │
│   • Тривалість подій (дескриптори)     │
│                                         │
│ 🔹 Для вашого CCTV HLS пайплайну:      │
│   • Конвертація DVB time → PROGRAM-DATE-TIME│
│   • Синхронізація розкладу з реальним часом│
│   • Коректна генерація HLS-плейлистів  │
└─────────────────────────────────────────┘
```

---

## 🔧 Формати даних: BCD-кодування

### 📅 DVB Time (40 біт = 5 байт)

```
Структура: [16 біт MJD][24 біт часу у BCD]

┌─────────────────┬─────────────────────────┐
│ MJD (16 біт)    │ Час у BCD (24 біти)    │
│ Modified Julian │ [8] HH [8] MM [8] SS   │
│ Date            │ BCD-кодовані значення  │
└─────────────────┴─────────────────────────┘

Приклад: "1993-10-13 12:45:00"
→ MJD = 49273 = 0xC079 (2 байти)
→ Час: 12:45:00 → BCD: 0x12 0x45 0x00 (3 байти)
→ Разом: [0xC0, 0x79, 0x12, 0x45, 0x00]
```

**Що таке BCD (Binary-Coded Decimal)?**
```
Звичайне двійкове:  45 = 0b00101101 = 0x2D
BCD-кодування:      45 = 0b01000101 = 0x45
                    ↑   ↑
                    4   5 (кожна цифра окремо у 4 бітах)

Переваги BCD для телебачення:
• Легко відображати на екрані без конвертації
• Людсько-читабельний у шістнадцятковому дампі
• Історичний стандарт для мовлення
```

### ⏱️ DVB Duration (16 або 24 біти)

```
Тривалість у хвилинах (16 біт = 2 байти):
[8] HH (BCD) [8] MM (BCD)
Приклад: 1 год 45 хв → 0x01 0x45

Тривалість у секундах (24 біти = 3 байти):
[8] HH (BCD) [8] MM (BCD) [8] SS (BCD)
Приклад: 1 год 45 хв 30 сек → 0x01 0x45 0x30
```

> ⚠️ **Важливо**: Значення > 59 у хвилинах/секундах **не валідні** у BCD! `0x60` ≠ 60, це помилка кодування.

---

## 🔍 Розбір тестів

### Тестові дані

```go
var (
    // Тривалість: 1 година 45 хвилин
    dvbDurationMinutes      = time.Hour + 45*time.Minute
    dvbDurationMinutesBytes = []byte{0x1, 0x45}  // BCD: 01 45
    
    // Тривалість: 1:45:30
    dvbDurationSeconds      = time.Hour + 45*time.Minute + 30*time.Second
    dvbDurationSecondsBytes = []byte{0x1, 0x45, 0x30}  // BCD: 01 45 30
    
    // Час: 1993-10-13 12:45:00
    dvbTime, _              = time.Parse("2006-01-02 15:04:05", "1993-10-13 12:45:00")
    dvbTimeBytes            = []byte{0xc0, 0x79, 0x12, 0x45, 0x0}  // MJD=0xC079, час=12:45:00 BCD
)
```

### ✅ Парсинг часу: `TestParseDVBTime`

```go
func TestParseDVBTime(t *testing.T) {
    d, err := parseDVBTime(astikit.NewBytesIterator(dvbTimeBytes))
    assert.Equal(t, dvbTime, d)      // ✅ отримали очікуваний time.Time
    assert.NoError(t, err)
}
```

**Гіпотетична реалізація `parseDVBTime`:**
```go
func parseDVBTime(i *astikit.BytesIterator) (time.Time, error) {
    // Читання 5 байт
    bs, err := i.NextBytesNoCopy(5)
    if err != nil { return time.Time{}, err }
    
    // 1. Парсинг MJD (16 біт, big-endian)
    mjd := int(uint16(bs[0])<<8 | uint16(bs[1]))
    
    // 2. Конвертація MJD → Unix time
    // MJD = Unix days since 1858-11-17
    // Unix epoch = 1970-01-01 = MJD 40587
    unixDays := mjd - 40587
    t := time.Unix(int64(unixDays)*86400, 0).UTC()
    
    // 3. Парсинг часу з BCD
    hours := int(bs[2]>>4)*10 + int(bs[2]&0x0F)
    minutes := int(bs[3]>>4)*10 + int(bs[3]&0x0F)
    seconds := int(bs[4]>>4)*10 + int(bs[4]&0x0F)
    
    return t.Add(time.Duration(hours)*time.Hour + 
                 time.Duration(minutes)*time.Minute + 
                 time.Duration(seconds)*time.Second), nil
}
```

### ✅ Парсинг тривалості: `TestParseDVBDurationMinutes/Seconds`

```go
func TestParseDVBDurationMinutes(t *testing.T) {
    d, err := parseDVBDurationMinutes(astikit.NewBytesIterator(dvbDurationMinutesBytes))
    assert.Equal(t, dvbDurationMinutes, d)  // 1h45m
    assert.NoError(t, err)
}
```

**Реалізація парсингу BCD-тривалості:**
```go
func parseDVBDurationMinutes(i *astikit.BytesIterator) (time.Duration, error) {
    bs, _ := i.NextBytesNoCopy(2)
    
    // BCD → десяткове
    hours := int(bs[0]>>4)*10 + int(bs[0]&0x0F)
    minutes := int(bs[1]>>4)*10 + int(bs[1]&0x0F)
    
    return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute, nil
}

func parseDVBDurationSeconds(i *astikit.BytesIterator) (time.Duration, error) {
    bs, _ := i.NextBytesNoCopy(3)
    
    hours := int(bs[0]>>4)*10 + int(bs[0]&0x0F)
    minutes := int(bs[1]>>4)*10 + int(bs[1]&0x0F)
    seconds := int(bs[2]>>4)*10 + int(bs[2]&0x0F)
    
    return time.Duration(hours)*time.Hour + 
           time.Duration(minutes)*time.Minute + 
           time.Duration(seconds)*time.Second, nil
}
```

### ✅ Запис: `TestWriteDVBTime` та інші

```go
func TestWriteDVBTime(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    n, err := writeDVBTime(w, dvbTime)
    
    assert.NoError(t, err)
    assert.Equal(t, 5, n)                    // ✅ рівно 5 байт
    assert.Equal(t, dvbTimeBytes, buf.Bytes())  // ✅ бінарна ідентичність
}
```

**Гіпотетична реалізація `writeDVBTime`:**
```go
func writeDVBTime(w *astikit.BitsWriter, t time.Time) (int, error) {
    // 1. Конвертація Unix time → MJD
    // MJD = days since 1858-11-17
    unixDays := t.Unix() / 86400
    mjd := uint16(unixDays + 40587)
    
    // 2. Запис MJD (big-endian)
    w.Write(uint8(mjd >> 8))
    w.Write(uint8(mjd & 0xFF))
    
    // 3. Витягнути час та закодувати у BCD
    hours := t.Hour()
    minutes := t.Minute()
    seconds := t.Second()
    
    w.Write(uint8(hours/10<<4 | hours%10))    // BCD для годин
    w.Write(uint8(minutes/10<<4 | minutes%10)) // BCD для хвилин
    w.Write(uint8(seconds/10<<4 | seconds%10)) // BCD для секунд
    
    return 5, nil
}
```

### ⚡ Бенчмарк: `BenchmarkDVBTime`

```go
func BenchmarkDVBTime(b *testing.B) {
    for i := 0; i < b.N; i++ {
        parseDVBTime(astikit.NewBytesIterator(dvbTimeBytes))
    }
}
```

**Очікувані результати:**
```
BenchmarkDVBTime-8    50000000    20-30 ns/op    0 B/op    0 allocs/op
```
→ **0 алокацій** завдяки `NextBytesNoCopy` та відсутності створення проміжних рядків.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Конвертація DVB Time → PROGRAM-DATE-TIME для HLS

```go
// У VideoManifestProxy — генерація #EXT-X-PROGRAM-DATE-TIME:
func dvbTimeToProgramDateTime(dvbBytes []byte) (time.Time, error) {
    if len(dvbBytes) != 5 {
        return time.Time{}, fmt.Errorf("invalid DVB time length: %d", len(dvbBytes))
    }
    
    // Парсити через astits (або ваша реалізація)
    t, err := parseDVBTime(astikit.NewBytesIterator(dvbBytes))
    if err != nil {
        return time.Time{}, err
    }
    
    // 🔹 Для HLS: формат RFC3339
    return t, nil  // time.Time вже у UTC
}

// Використання при генерації плейлиста:
for _, event := range eitEvents {
    startTime, _ := dvbTimeToProgramDateTime(event.StartTimeBytes)
    
    playlist.AddSegment(segment, 
        astits.ProgramDateTime(startTime.Format(time.RFC3339)),  // #EXT-X-PROGRAM-DATE-TIME
    )
}
```

### ✅ 2. Обробка EIT таблиць з розкладом передач

```go
// У обробці PSI/SI даних:
func handleEITEvent(event *astits.EITDataEvent, channelID string) {
    // event.StartTime — вже парсений time.Time з DVB формату
    // event.Duration — time.Duration з BCD
    
    log.Infof("Channel %s: event '%s' starts at %s, duration %v",
        channelID, event.EventName, event.StartTime, event.Duration)
    
    // 🔹 Для HLS: додати метадані про подію
    hlsMetadata := HLSEventMetadata{
        Title:     event.EventName,
        StartTime: event.StartTime,
        Duration:  event.Duration,
        // Можна додати опис з ExtendedEvent дескрипторів...
    }
    
    // Зберегти для відображення у клієнті або для EPG
    storeEventMetadata(channelID, hlsMetadata)
}
```

### ✅ 3. Синхронізація системного часу через TOT

```go
// TOT (Time Offset Table) містить поточний час + UTC offset
func handleTOT(tot *astits.TOTData) {
    // tot.UTCTime — парсений time.Time з DVB формату
    // tot.TimeOffset — зсув у хвилинах (signed)
    
    systemTime := tot.UTCTime.Add(time.Duration(tot.TimeOffset) * time.Minute)
    
    // 🔹 Порівняти з локальним часом для детекції дрейфу
    localNow := time.Now().UTC()
    drift := systemTime.Sub(localNow)
    
    if drift.Abs() > time.Second {
        log.Warnf("Time drift detected: system=%v, local=%v, diff=%v", 
            systemTime, localNow, drift)
        // Опція: скоригувати локальний годинник або залогити для моніторингу
    }
}
```

### ✅ 4. Валідація BCD-кодування перед записом

```go
// При генерації власних SI таблиць — валідація вхідних даних:
func validateBCDDuration(d time.Duration) error {
    hours := int(d.Hours())
    minutes := int(d.Minutes()) % 60
    seconds := int(d.Seconds()) % 60
    
    // BCD вимагає: кожна "цифра" 0-9
    if hours > 99 || minutes > 59 || seconds > 59 {
        return fmt.Errorf("duration %v exceeds BCD limits", d)
    }
    
    // Додаткова перевірка: кожна десяткова цифра має бути 0-9
    // (на випадок, якщо хтось передасть 0x60 замість 0x59)
    if hours/10 > 9 || hours%10 > 9 || minutes/10 > 9 || minutes%10 > 9 {
        return fmt.Errorf("invalid BCD digits in duration")
    }
    
    return nil
}
```

### ✅ 5. Моніторинг точності часу

```go
// monitoring.Monitor — метрики для часової синхронізації:
type TimeSyncMetrics struct {
    DVBTimeDriftGauge    *prometheus.GaugeVec  // різниця DVB time vs NTP
    EITEventCount        *prometheus.CounterVec  // кількість подій з EIT
    BCDParseErrors       *prometheus.CounterVec  // помилки парсингу BCD
}

// У обробці TOT:
func monitorTimeSync(tot *astits.TOTData, channelID string, metrics *TimeSyncMetrics) {
    ntpTime := getNTPTime()  // ваш отримувач NTP
    dvbTime := tot.UTCTime
    
    drift := dvbTime.Sub(ntpTime).Seconds()
    metrics.DVBTimeDriftGauge.WithLabelValues(channelID).Set(drift)
    
    if math.Abs(drift) > 1.0 {  // поріг 1 секунда
        log.Warnf("Channel %s: time drift %.2f seconds", channelID, drift)
    }
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на крайні значення MJD

```go
func TestParseDVBTime_EdgeCases(t *testing.T) {
    // MJD діапазон: 0..65535 (16 біт)
    // Відповідає датам: 1858-11-17 .. ~2137 рік
    
    testCases := []struct {
        name     string
        mjd      uint16
        expected time.Time
    }{
        {"Unix epoch", 40587, time.Unix(0, 0).UTC()},  // 1970-01-01
        {"Min MJD", 0, time.Date(1858, 11, 17, 0, 0, 0, 0, time.UTC)},
        {"Future", 65535, time.Date(2137, 2, 16, 0, 0, 0, 0, time.UTC)},
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            bs := []byte{
                uint8(tc.mjd >> 8), uint8(tc.mjd & 0xFF),  // MJD
                0x00, 0x00, 0x00,  // 00:00:00 BCD
            }
            got, err := parseDVBTime(astikit.NewBytesIterator(bs))
            assert.NoError(t, err)
            assert.Equal(t, tc.expected, got)
        })
    }
}
```

### 🔹 Тест на невалідне BCD

```go
func TestParseDVBDuration_InvalidBCD(t *testing.T) {
    // Невалідне BCD: 0x60 ≠ 60, це помилка кодування
    invalidBytes := []byte{0x60, 0x00}  // "години" = 0x60 (не BCD!)
    
    // Поточна реалізація може не валідувати — додайте перевірку:
    d, err := parseDVBDurationMinutes(astikit.NewBytesIterator(invalidBytes))
    
    // Опція 1: дозволити (просто обчислити 6*10+0 = 60 годин)
    // assert.Equal(t, 60*time.Hour, d)
    
    // Опція 2: повернути помилку (більш безпечно)
    // assert.Error(t, err)
    
    // Рекомендація: додати валідацію у парсер
}
```

### 🔹 Тест на round-trip для довільного часу

```go
func TestDVBTime_RoundTrip(t *testing.T) {
    // Згенерувати випадковий час у допустимому діапазоні
    testTime := time.Date(2024, 5, 15, 14, 30, 45, 0, time.UTC)
    
    // Записати у DVB формат
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    _, err := writeDVBTime(w, testTime)
    assert.NoError(t, err)
    
    // Прочитати назад
    parsed, err := parseDVBTime(astikit.NewBytesIterator(buf.Bytes()))
    assert.NoError(t, err)
    
    // Порівняти (з точністю до секунди, бо DVB не має мікросекунд)
    assert.Equal(t, testTime.Truncate(time.Second), parsed)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Неправильна конвертація MJD | Дати зсуваються на дні/місяці | Перевірити константу: Unix epoch = MJD 40587 (не 40586!) |
| Невалідне BCD у вхідних даних | "Години" = 96 замість 60 | Додати валідацію: `if digit > 9 { return error }` |
| Часовий пояс не враховано | PROGRAM-DATE-TIME у локальному замість UTC | Завжди використовувати `.UTC()` при роботі з DVB time |
| Мікросекунди втрачаються | Точність лише до секунд | Це обмеження формату — документувати, не "лагодити" |
| Переповнення MJD при записі | Дати після ~2137 року не кодуються | Перевіряти діапазон перед `writeDVBTime`: `if year > 2137 { error }` |

### Приклад валідації перед записом:

```go
func safeWriteDVBTime(w *astikit.BitsWriter, t time.Time) error {
    // Перевірити діапазон MJD (16 біт = 0..65535)
    unixDays := t.Unix() / 86400
    mjd := int(unixDays + 40587)
    
    if mjd < 0 || mjd > 65535 {
        return fmt.Errorf("time %v out of DVB MJD range [1858-2137]", t)
    }
    
    // Перевірити BCD-валідність часу
    h, m, s := t.Clock()
    if h > 23 || m > 59 || s > 59 {
        return fmt.Errorf("invalid time components: %02d:%02d:%02d", h, m, s)
    }
    
    return writeDVBTime(w, t)
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Парсинг DVB time з EIT/TOT:
func parseEventStartTime(event *astits.EITDataEvent) (time.Time, error) {
    // event.StartTime вже парсений astits, але якщо у вас сирі байти:
    return parseDVBTime(astikit.NewBytesIterator(event.StartTimeBytes))
}

// 2. Конвертація для HLS PROGRAM-DATE-TIME:
func formatProgramDateTime(t time.Time) string {
    // HLS вимагає RFC3339 / ISO8601 формат
    return t.UTC().Format(time.RFC3339)
    // Приклад: "2024-05-15T14:30:45Z"
}

// 3. Запис DVB time у власні SI таблиці:
func buildTOTPacket(utcTime time.Time, offsetMinutes int) ([]byte, error) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Заголовок пакету...
    
    // Записати DVB time
    if err := writeDVBTime(w, utcTime); err != nil {
        return nil, err
    }
    
    // Записати time offset (16 біт, signed, у хвилинах)
    // Формат: [1] sign [3] reserved [12] offset
    sign := 0
    if offsetMinutes < 0 {
        sign = 1
        offsetMinutes = -offsetMinutes
    }
    w.WriteN(uint16(sign<<15 | uint16(offsetMinutes)&0xFFF), 16)
    
    // CRC32 + stuffing...
    
    return buf.Bytes(), nil
}

// 4. Валідація вхідних даних:
func validateDVBTimeBytes(bs []byte) error {
    if len(bs) != 5 {
        return fmt.Errorf("expected 5 bytes, got %d", len(bs))
    }
    
    // Перевірити BCD-валідність часу
    for i := 2; i < 5; i++ {
        hi, lo := bs[i]>>4, bs[i]&0x0F
        if hi > 9 || lo > 9 {
            return fmt.Errorf("invalid BCD digit at byte %d: 0x%02X", i, bs[i])
        }
    }
    return nil
}
```

---

## 📊 Матриця форматів часу у MPEG-TS/DVB

```
Формат          | Розмір   | Кодування | Використання
────────────────┼──────────┼───────────┼─────────────────────────
PCR Base        | 33 біти  | Бінарне   | Синхронізація декодера @ 90 kHz
PTS/DTS         | 33 біти  | Бінарне   | Таймінг кадрів @ 90 kHz
DVB Time (MJD)  | 40 біт   | BCD       | EIT/TOT/TDT: абсолютний час
DVB Duration    | 16/24 біт| BCD       | Тривалість подій у дескрипторах
Unix timestamp  | 32/64 біт| Бінарне   | Ваш пайплайн, API, логи
```

> 💡 **Ключова ідея**: DVB time — це "мовний" формат для розкладів, а PCR/PTS — "виконавчий" формат для синхронізації відтворення. У вашому HLS пайплайні:
> - 📅 DVB time → `#EXT-X-PROGRAM-DATE-TIME` (для EPG/розкладу)
> - ⏱️ PCR/PTS → синхронізація аудіо/відео у плеєрі

---

## 📚 Корисні посилання

- [ETSI EN 300 468: DVB SI specification](https://www.etsi.org/deliver/etsi_en/300400_300499/300468/)
- [Modified Julian Date converter](https://www.timeanddate.com/date/julian-day.html)
- [BCD encoding explanation](https://en.wikipedia.org/wiki/Binary-coded_decimal)
- [astits DVB time functions](https://github.com/asticode/go-astits/blob/master/dvb.go)

> 💡 **Порада**: Якщо ваші вхідні потоки містять EIT — використовуйте `parseDVBTime` для витягування розкладу і додавайте `#EXT-X-PROGRAM-DATE-TIME` у HLS-плейлисти. Це покращить сумісність з плеєрами, що підтримують EPG, та дозволить користувачам бачити назви передач у таймлайн.

Якщо потрібно — можу допомогти:
- 🔄 Реалізувати конвертер DVB time ↔ Unix time з обробкою часових поясів
- 🧩 Інтегрувати парсинг EIT у ваш `segmentAssembler` для збагачення метаданими
- 🧪 Написати fuzz-тест для стійкості до пошкоджених BCD-значень

🛠️