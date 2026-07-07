# Глибоке роз'яснення: Тест парсингу TOT (Time Offset Table) у astits

Цей файл тестує **парсинг секції TOT (Time Offset Table)** — таблиці службової інформації DVB, що містить поточний час UTC та зсув для локального часу. Це критично для синхронізації розкладу передач (EIT) з реальним часом.

---

## 🎯 Що таке TOT і навіщо він потрібен?

```
┌─────────────────────────────────────────┐
│ TOT (Time Offset Table) у DVB:         │
│                                         │
│ 🔹 Призначення:                         │
│   • Передача поточного часу UTC        │
│   • Зсув для локального часу (таймзони)│
│   • Дескриптори для розширеної інформації│
│                                         │
│ 🔹 Формат (ETSI EN 300 468, §6.2.42):  │
│ • table_id = 0x73                      │
│ • section_length: змінна довжина        │
│ • UTC_time: 40 біт (MJD + BCD час)     │
│ • time_offset: 16 біт (знак + хвилини) │
│ • descriptors: цикл дескрипторів       │
│ • CRC32: валідація цілісності          │
│                                         │
│ 🔹 Для вашого CCTV HLS пайплайну:      │
│   • Синхронізація PROGRAM-DATE-TIME    │
│   • Корекція часових поясів для EPG    │
│   • Детекція дрейфу часу у потоці      │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `TOTData` та тестові дані

### Тип даних

```go
// TOTData представляє секцію Time Offset Table
type TOTData struct {
    Descriptors []*Descriptor  // 🎯 додаткові метадані (напр., LocalTimeOffset)
    UTCTime     time.Time      // 🎯 поточний час у UTC (парсений з DVB формату)
}
```

### Тестові дані: `tot` та `totBytes()`

```go
// Глобальна змінна для тестів — еталонне значення
var tot = &TOTData{
    Descriptors: descriptors,  // посилання на тестові дескриптори з іншого файлу
    UTCTime:     dvbTime,      // time.Time: 1993-10-13 12:45:00 UTC
}

// Генератор "еталонних" байтів для TOT секції
func totBytes() []byte {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // 🔹 1. UTC час у DVB форматі (5 байт: 2 байти MJD + 3 байти BCD часу)
    w.Write(dvbTimeBytes)  // []byte{0xc0, 0x79, 0x12, 0x45, 0x00}
    
    // 🔹 2. Reserved біти (4 біти = 0)
    w.Write("0000")
    
    // 🔹 3. Дескриптори (змінна довжина)
    descriptorsBytes(w)  // виклик з тестів дескрипторів
    
    // Результат: []byte з серіалізованою TOT секцією (без заголовка таблиці та CRC)
    return buf.Bytes()
}
```

> 💡 **Важливо**: `totBytes()` генерує тільки **payload секції** (без PSI заголовка та CRC32). Функція `parseTOTSection` очікує саме payload, бо заголовок парситься на вищому рівні (`parsePSISection`).

---

## 🔍 Тест `TestParseTOTSection`

```go
func TestParseTOTSection(t *testing.T) {
    // 🔹 1. Створити ітератор на тестових байтах
    iter := astikit.NewBytesIterator(totBytes())
    
    // 🔹 2. Парсити TOT секцію
    d, err := parseTOTSection(iter)
    
    // 🔹 3. Перевірити результат
    assert.Equal(t, d, tot)        // ✅ структурна рівність з еталоном
    assert.NoError(t, err)         // ✅ без помилок
}
```

### Що робить `parseTOTSection` (гіпотетична реалізація)

```go
func parseTOTSection(i *astikit.BytesIterator) (*TOTData, error) {
    // 🔹 1. Парсинг UTC часу (5 байт, DVB формат)
    utcTime, err := parseDVBTime(i)  // використовувати функцію з dvb.go
    if err != nil {
        return nil, fmt.Errorf("astits: parsing UTC time failed: %w", err)
    }
    
    // 🔹 2. Пропустити reserved біти (4 біти)
    // У astikit: біти читаються побайтово, тому пропускаємо частину байта
    // (реальна реалізація може використовувати бітовий ітератор)
    
    // 🔹 3. Парсинг дескрипторів (змінна довжина до кінця секції)
    // Дескриптори мають формат: [tag][length][payload...]
    descriptors, err := parseDescriptors(i)
    if err != nil {
        return nil, fmt.Errorf("astits: parsing TOT descriptors failed: %w", err)
    }
    
    return &TOTData{
        UTCTime:     utcTime,
        Descriptors: descriptors,
    }, nil
}
```

---

## 🧮 Формат TOT секції у деталях

```
TOT Section Payload (без заголовка та CRC):
┌─────────────────────────────────┐
│ [40 біт] UTC_time               │
│   • [16] MJD (Modified Julian Date)│
│   • [24] Час у BCD: HH MM SS   │
├─────────────────────────────────┤
│ [4 біти] Reserved = 0b0000      │
├─────────────────────────────────┤
│ [N байт] Descriptors loop       │
│   • [8] descriptor_tag          │
│   • [8] descriptor_length       │
│   • [N] descriptor_payload      │
│   • ... повтор для кожного дескриптора │
└─────────────────────────────────┘

Повна PSI секція (додається на вищому рівні):
[8] table_id = 0x73
[12] section_length
[16] reserved + version_number + current_next
[8] section_number = 0
[8] last_section_number = 0
[40] UTC_time
[4] reserved
[N] descriptors
[32] CRC32
```

### Приклад розрахунку UTC часу

```
Вхідні байти: []byte{0xc0, 0x79, 0x12, 0x45, 0x00}

1. MJD = 0xC079 = 49273 (десяткове)
   • Конвертація: 49273 - 40587 = 8686 днів від Unix epoch
   • 8686 × 86400 = 750,470,400 секунд → 1993-10-13

2. Час у BCD: 0x12 0x45 0x00
   • 0x12 → 1×10 + 2 = 12 годин
   • 0x45 → 4×10 + 5 = 45 хвилин
   • 0x00 → 0×10 + 0 = 0 секунд

Результат: time.Date(1993, 10, 13, 12, 45, 0, 0, time.UTC) ✅
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Синхронізація PROGRAM-DATE-TIME через TOT

```go
// У VideoManifestProxy — використання TOT для точного часу:
type TimeSyncState struct {
    baseTOTTime   time.Time  // останній отриманий UTC час з TOT
    basePCR       *astits.ClockReference  // відповідний PCR
    timezoneOffset time.Duration  // зсув з дескриптора LocalTimeOffset
}

func handleTOT(tot *astits.TOTData, pcr *astits.ClockReference, channelID string) {
    // 🔹 Зберегти базову синхронізацію
    syncState[channelID] = &TimeSyncState{
        baseTOTTime: tot.UTCTime,
        basePCR:     pcr,
    }
    
    // 🔹 Обробити дескриптори для зсуву часу
    for _, desc := range tot.Descriptors {
        if desc.LocalTimeOffset != nil {
            for _, item := range desc.LocalTimeOffset.Items {
                polarity := 1
                if item.LocalTimeOffsetPolarity {
                    polarity = -1
                }
                syncState[channelID].timezoneOffset = 
                    time.Duration(polarity * int(item.LocalTimeOffset.Minutes())) * time.Minute
                
                log.Infof("Channel %s: timezone offset %+v", channelID, 
                    syncState[channelID].timezoneOffset)
            }
        }
    }
}

// При генерації HLS плейлиста:
func calculateProgramDateTime(pcr *astits.ClockReference, channelID string) time.Time {
    state := syncState[channelID]
    if state == nil || state.basePCR == nil {
        return time.Now().UTC()  // fallback
    }
    
    // Розрахувати різницю між поточним PCR та базовим
    pcrDiff := pcr.Duration() - state.basePCR.Duration()
    
    // Додати до базового TOT часу
    return state.baseTOTTime.Add(pcrDiff).Add(state.timezoneOffset)
}
```

### ✅ 2. Детекція дрейфу часу

```go
// monitoring.Monitor — метрики для часової синхронізації:
type TimeSyncMetrics struct {
    TOTDriftGauge    *prometheus.GaugeVec  // різниця TOT time vs NTP
    LastTOTUpdate    *prometheus.GaugeVec  // timestamp останнього TOT
    TimezoneOffsets  *prometheus.GaugeVec  // зсуви по каналах
}

// У обробці TOT:
func monitorTOTSync(tot *astits.TOTData, channelID string, metrics *TimeSyncMetrics) {
    // Порівняти з локальним системним часом (через NTP)
    ntpTime := getNTPTime()  // ваш отримувач NTP
    drift := tot.UTCTime.Sub(ntpTime).Seconds()
    
    metrics.TOTDriftGauge.WithLabelValues(channelID).Set(drift)
    metrics.LastTOTUpdate.WithLabelValues(channelID).Set(float64(time.Now().Unix()))
    
    if math.Abs(drift) > 1.0 {  // поріг 1 секунда
        log.Warnf("Channel %s: TOT time drift %.2f seconds (TOT=%v, NTP=%v)", 
            channelID, drift, tot.UTCTime, ntpTime)
    }
}
```

### ✅ 3. Обробка дескриптора LocalTimeOffset

```go
// LocalTimeOffset дескриптор (0x58) містить інформацію про таймзону:
func extractTimezoneOffset(desc *astits.Descriptor) (time.Duration, error) {
    if desc.Tag != astits.DescriptorTagLocalTimeOffset {
        return 0, fmt.Errorf("wrong descriptor tag: 0x%02X", desc.Tag)
    }
    
    if desc.LocalTimeOffset == nil || len(desc.LocalTimeOffset.Items) == 0 {
        return 0, fmt.Errorf("no LocalTimeOffset items")
    }
    
    item := desc.LocalTimeOffset.Items[0]  // зазвичай один елемент
    
    // Конвертувати хвилини у time.Duration
    offsetMinutes := int(item.LocalTimeOffset / time.Minute)
    if item.LocalTimeOffsetPolarity {
        offsetMinutes = -offsetMinutes  // від'ємний зсув
    }
    
    return time.Duration(offsetMinutes) * time.Minute, nil
}

// Використання:
for _, desc := range tot.Descriptors {
    if offset, err := extractTimezoneOffset(desc); err == nil {
        log.Infof("Timezone offset: %+v", offset)
        // Застосувати до PROGRAM-DATE-TIME
    }
}
```

### ✅ 4. Fallback логіка при відсутності TOT

```go
// Якщо потік не містить TOT — використати альтернативні джерела часу:
func getFallbackTime(channelID string) time.Time {
    // 🔹 Спроба 1: час з EIT подій
    if lastEITTime, ok := eitTimeCache[channelID]; ok {
        return lastEITTime
    }
    
    // 🔹 Спроба 2: системний час з корекцією на відому таймзону
    if tz, ok := channelTimezone[channelID]; ok {
        return time.Now().In(tz)
    }
    
    // 🔹 Fallback: UTC
    return time.Now().UTC()
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на різні часові пояси

```go
func TestParseTOTSection_Timezones(t *testing.T) {
    testCases := []struct {
        name           string
        offsetMinutes  int
        polarity       bool
        expectedOffset time.Duration
    }{
        {"UTC", 0, false, 0},
        {"Kyiv +2h", 120, false, 2 * time.Hour},
        {"New York -5h", 300, true, -5 * time.Hour},
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // Створити LocalTimeOffset дескриптор
            desc := &astits.Descriptor{
                Tag: astits.DescriptorTagLocalTimeOffset,
                LocalTimeOffset: &astits.DescriptorLocalTimeOffset{
                    Items: []*astits.DescriptorLocalTimeOffsetItem{
                        {
                            CountryCode:             []byte("UA "),
                            LocalTimeOffset:         time.Duration(tc.offsetMinutes) * time.Minute,
                            LocalTimeOffsetPolarity: tc.polarity,
                        },
                    },
                },
            }
            
            // Серіалізувати та парсити
            buf := &bytes.Buffer{}
            w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
            writeDescriptor(w, desc)
            
            parsed, err := parseDescriptors(astikit.NewBytesIterator(buf.Bytes()))
            assert.NoError(t, err)
            
            offset, _ := extractTimezoneOffset(parsed[0])
            assert.Equal(t, tc.expectedOffset, offset)
        })
    }
}
```

### 🔹 Тест на round-trip TOT секції

```go
func TestTOTSection_RoundTrip(t *testing.T) {
    original := &astits.TOTData{
        UTCTime: time.Date(2024, 5, 15, 14, 30, 45, 0, time.UTC),
        Descriptors: []*astits.Descriptor{
            {
                Tag: astits.DescriptorTagLocalTimeOffset,
                LocalTimeOffset: &astits.DescriptorLocalTimeOffset{
                    Items: []*astits.DescriptorLocalTimeOffsetItem{
                        {
                            CountryCode:             []byte("UA "),
                            LocalTimeOffset:         2 * time.Hour,
                            LocalTimeOffsetPolarity: false,
                        },
                    },
                },
            },
        },
    }
    
    // Серіалізувати
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    w.Write(dvbTimeBytesFor(original.UTCTime))  // ваша helper-функція
    writeDescriptors(w, original.Descriptors)
    
    // Парсити назад
    parsed, err := parseTOTSection(astikit.NewBytesIterator(buf.Bytes()))
    assert.NoError(t, err)
    
    // Порівняти (з точністю до секунди)
    assert.Equal(t, original.UTCTime.Truncate(time.Second), parsed.UTCTime)
    assert.Len(t, parsed.Descriptors, 1)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірний парсинг MJD | Дати зсуваються на дні/місяці | Перевірити константу: Unix epoch = MJD 40587 (не 40586!) |
| Невалідне BCD у часі | "Години" = 96 замість 60 | Додати валідацію: `if (b>>4)>9 || (b&0xF)>9 { return error }` |
| Часовий пояс не враховано | PROGRAM-DATE-TIME у локальному замість UTC | Завжди використовувати `.UTC()` при роботі з DVB time, застосовувати зсув окремо |
| Дескриптори не парсяться | `desc.LocalTimeOffset == nil` | Перевірити, що `parseDescriptors` викликається з правильним `offsetEnd` |
| TOT не надходить у потоці | `NextData()` ніколи не повертає TOT | Це нормально: TOT передається рідко (раз на кілька хвилин); реалізувати fallback |

### Приклад валідації DVB time перед використанням:

```go
func validateDVBTime(t time.Time) error {
    // Перевірити діапазон MJD (16 біт = 0..65535)
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
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Парсинг TOT з вхідного потоку:
func handleTOTData(data *astits.DemuxerData, channelID string) error {
    if data.TOT == nil {
        return nil  // не TOT
    }
    
    tot := data.TOT
    log.Infof("Channel %s: TOT UTC time=%v, descriptors=%d", 
        channelID, tot.UTCTime, len(tot.Descriptors))
    
    // Синхронізувати стан часу
    updateTimeSyncState(channelID, tot.UTCTime, data.FirstPacket)
    
    // Обробити дескриптори
    for _, desc := range tot.Descriptors {
        if desc.Tag == astits.DescriptorTagLocalTimeOffset {
            offset, err := extractTimezoneOffset(desc)
            if err == nil {
                setTimezoneOffset(channelID, offset)
            }
        }
    }
    
    return nil
}

// 2. Розрахунок PROGRAM-DATE-TIME для HLS:
func calculateProgramDateTime(pcr *astits.ClockReference, channelID string) time.Time {
    state := getTimeSyncState(channelID)
    if state == nil {
        return time.Now().UTC()  // fallback
    }
    
    // Розрахувати різницю у часовій області
    pcrDiff := pcr.Duration() - state.basePCR.Duration()
    
    // Застосувати до базового часу + таймзона
    return state.baseTOTTime.Add(pcrDiff).Add(state.timezoneOffset)
}

// 3. Форматування для HLS плейлиста:
func formatHLSProgramDateTime(t time.Time) string {
    // HLS вимагає RFC3339 / ISO8601
    return t.Format("2006-01-02T15:04:05.000Z")
    // Приклад: "2024-05-15T14:30:45.000Z"
}

// 4. Моніторинг дрейфу:
func monitorTimeDrift(totTime time.Time, channelID string) {
    ntpTime := getNTPTime()
    drift := totTime.Sub(ntpTime).Seconds()
    
    if math.Abs(drift) > 1.0 {
        log.Warnf("Channel %s: time drift %.2f seconds", channelID, drift)
        metrics.TOTDriftGauge.WithLabelValues(channelID).Set(drift)
    }
}
```

---

## 📊 Матриця часових форматів у вашому пайплайні

```
Формат          | Джерело        | Призначення                     | Конвертація
────────────────┼────────────────┼─────────────────────────────────┼────────────
DVB Time (MJD+BCD) | TOT/EIT     | Абсолютний час у метаданих      | → time.Time (UTC)
PCR Base (33-bit) | TS пакети    | Синхронізація декодера @ 90 kHz | → time.Duration
PTS/DTS (33-bit)  | PES пакети   | Таймінг кадрів @ 90 kHz         | → time.Duration
Unix timestamp    | Ваш код      | Логи, метрики, API              | ←/→ time.Time
HLS PROGRAM-DATE-TIME | Плейлист | Відображення часу у плеєрі      | time.Time → RFC3339

Workflow для синхронізації:
1. TOT DVB Time → parseDVBTime() → time.Time (UTC)
2. Зберегти базову синхронізацію: (baseTOTTime, basePCR)
3. Для кожного сегмента: programDateTime = baseTOTTime + (currentPCR - basePCR).Duration()
4. Додати таймзону з дескриптора LocalTimeOffset
5. Форматувати у RFC3339 для #EXT-X-PROGRAM-DATE-TIME
```

---

## 📚 Корисні посилання

- [ETSI EN 300 468: TOT specification (§6.2.42)](https://www.etsi.org/deliver/etsi_en/300400_300499/300468/)
- [Modified Julian Date converter](https://www.timeanddate.com/date/julian-day.html)
- [BCD encoding explanation](https://en.wikipedia.org/wiki/Binary-coded_decimal)
- [astits TOT parsing source](https://github.com/asticode/go-astits/blob/master/data.go)

> 💡 **Ключова ідея**: TOT — це "годинник" вашого DVB потоку. У вашому CCTV HLS пайплайні це дозволяє:
> - 🕐 Генерувати точні `#EXT-X-PROGRAM-DATE-TIME` теги для сумісності з плеєрами
> - 🌍 Коректно обробляти часові пояси для міжнародної аудиторії
> - 🔍 Детектувати дрейф часу між джерелом та вашим сервером
> - 📊 Збагачувати метадані сегментів для EPG та аналітики

Якщо потрібно — можу допомогти:
- 🔄 Реалізувати повну синхронізацію TOT → PCR → PROGRAM-DATE-TIME у вашому `VideoManifestProxy`
- 🧩 Додати підтримку кешування останнього TOT для fallback при втраті сигналу
- 🧪 Написати integration-тест для перевірки коректності генерації плейлистів з таймінгами

🛠️