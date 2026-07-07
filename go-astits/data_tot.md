# Глибоке роз'яснення: `tot.go` — парсинг TOT (Time Offset Table) у astits

Цей файл містить **реалізацію парсингу секції TOT (Time Offset Table)** — таблиці службової інформації стандарту DVB, що передає поточний час UTC та інформацію про часові пояси. Це ключовий компонент для синхронізації розкладу передач (EIT) з реальним часом.

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
│ 🔹 Формат (ETSI EN 300 468, §5.2.6):   │
│ • table_id = 0x73                      │
│ • section_syntax_indicator = 0         │
│ • section_length: змінна довжина        │
│ • UTC_time: 40 біт (MJD + BCD час)     │
│ • reserved_future_use: 4 біти = 0      │
│ • descriptors_loop_length: 12 біт      │
│ • descriptors: цикл дескрипторів       │
│ • CRC32: валідація цілісності          │
│                                         │
│ 🔹 Для вашого CCTV HLS пайплайну:      │
│   • Синхронізація #EXT-X-PROGRAM-DATE-TIME│
│   • Корекція часових поясів для EPG    │
│   • Детекція дрейфу часу у потоці      │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `TOTData`

```go
type TOTData struct {
    Descriptors []*Descriptor  // 🎯 додаткові метадані (напр., LocalTimeOffset)
    UTCTime     time.Time      // 🎯 поточний час у UTC (парсений з DVB формату)
}
```

### Поля та їх значення

| Поле | Тип | Опис | Використання у вашому пайплайні |
|------|-----|------|--------------------------------|
| `UTCTime` | `time.Time` | Поточний час у UTC, парсений з DVB-формату (MJD+BCD) | 🔹 Базова точка для розрахунку `PROGRAM-DATE-TIME` |
| `Descriptors` | `[]*Descriptor` | Цикл дескрипторів, зокрема `LocalTimeOffset` (0x58) | 🔹 Отримання зсуву часового поясу для локалізації |

> 💡 **Важливо**: `UTCTime` завжди у **UTC**, незалежно від таймзони мовлення. Локальний час розраховується через дескриптор `LocalTimeOffset`.

---

## 🔍 Функція `parseTOTSection`: покроковий розбір

```go
func parseTOTSection(i *astikit.BytesIterator) (*TOTData, error) {
    // 🔹 1. Ініціалізація структури
    d := &TOTData{}
    
    // 🔹 2. Парсинг UTC часу (5 байт, DVB формат)
    if d.UTCTime, err = parseDVBTime(i); err != nil {
        err = fmt.Errorf("astits: parsing DVB time failed: %w", err)
        return
    }
    
    // 🔹 3. Парсинг дескрипторів (змінна довжина)
    if d.Descriptors, err = parseDescriptors(i); err != nil {
        err = fmt.Errorf("astits: parsing descriptors failed: %w", err)
        return
    }
    
    return d, nil
}
```

### Крок 1: Парсинг UTC часу через `parseDVBTime`

```go
// Виклик parseDVBTime(i) читає 5 байт:
// [2 байти] MJD (Modified Julian Date)
// [3 байти] Час у BCD: HH MM SS

// Приклад: []byte{0xc0, 0x79, 0x12, 0x45, 0x00}
// • MJD = 0xC079 = 49273 → 1993-10-13
// • Час: 0x12→12h, 0x45→45m, 0x00→00s
// Результат: time.Date(1993, 10, 13, 12, 45, 0, 0, time.UTC)
```

**Математика конвертації MJD → Unix time:**
```
MJD = кількість днів з 1858-11-17 00:00:00 UTC
Unix epoch = 1970-01-01 = MJD 40587

Формула:
  unix_seconds = (MJD - 40587) × 86400

Приклад для MJD=49273:
  (49273 - 40587) × 86400 = 8686 × 86400 = 750,470,400 секунд
  → 1993-10-13 00:00:00 UTC
```

### Крок 2: Парсинг дескрипторів

```go
// parseDescriptors(i) читає дескриптори до кінця секції:
// Формат кожного дескриптора:
// [1 байт] descriptor_tag
// [1 байт] descriptor_length
// [N байт] descriptor_payload

// Для TOT найважливіший дескриптор:
// • Tag 0x58 = LocalTimeOffset (ETSI EN 300 468, §6.2.20)
//   Містить: country_code, region_id, time_offset, polarity, time_of_change
```

> ⚠️ **Важливо**: `parseTOTSection` очікує, що ітератор `i` вже позиціонований **після заголовка PSI секції** (після `table_id`, `section_length`, тощо). Заголовок парситься на вищому рівні у `parsePSISection`.

---

## 🧮 Формат TOT секції у деталях

```
Повна структура TOT секції (включаючи PSI заголовок):

┌─────────────────────────────────┐
│ [8]  table_id = 0x73            │ ← TOT ідентифікатор
├─────────────────────────────────┤
│ [1]  section_syntax_indicator=0 │ ← PSI без syntax (на відміну від PAT/PMT)
│ [1]  reserved_future_use        │
│ [10] section_length             │ ← довжина решти секції (байт 2-3)
├─────────────────────────────────┤
│ [40] UTC_time                   │ ← MJD(16) + BCD_час(24)
├─────────────────────────────────┤
│ [4]  reserved_future_use = 0    │
├─────────────────────────────────┤
│ [12] descriptors_loop_length    │ ← загальна довжина дескрипторів
├─────────────────────────────────┤
│ [N]  descriptors...             │ ← цикл дескрипторів
├─────────────────────────────────┤
│ [32] CRC32                      │ ← валідація цілісності
└─────────────────────────────────┘

parseTOTSection() отримує ітератор, позиціонований ПІСЛЯ:
• table_id (1 байт)
• section_length (2 байти)
→ Тобто прямо перед UTC_time
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Синхронізація `PROGRAM-DATE-TIME` через TOT + PCR

```go
// У VideoManifestProxy — збереження базової синхронізації:
type TimeSyncState struct {
    baseTOTTime      time.Time              // останній UTC час з TOT
    basePCR          *astits.ClockReference // відповідний PCR для цього часу
    timezoneOffset   time.Duration          // зсув з дескриптора
    lastUpdateTime   time.Time              // для детекції застарілих даних
}

func handleTOT(tot *astits.TOTData, pcr *astits.ClockReference, channelID string) {
    // 🔹 Зберегти базову синхронізацію
    syncState[channelID] = &TimeSyncState{
        baseTOTTime:    tot.UTCTime,
        basePCR:        pcr,
        lastUpdateTime: time.Now(),
    }
    
    // 🔹 Обробити дескриптори для зсуву часу
    for _, desc := range tot.Descriptors {
        if desc.Tag == astits.DescriptorTagLocalTimeOffset && desc.LocalTimeOffset != nil {
            for _, item := range desc.LocalTimeOffset.Items {
                polarity := 1
                if item.LocalTimeOffsetPolarity {
                    polarity = -1  // від'ємний зсув
                }
                offset := time.Duration(polarity * int(item.LocalTimeOffset/time.Minute)) * time.Minute
                syncState[channelID].timezoneOffset = offset
                
                log.Infof("Channel %s: timezone offset %+v (country=%s)", 
                    channelID, offset, item.CountryCode)
            }
        }
    }
}

// При генерації HLS плейлиста — розрахунок програмного часу:
func calculateProgramDateTime(pcr *astits.ClockReference, channelID string) time.Time {
    state := syncState[channelID]
    if state == nil || state.basePCR == nil {
        return time.Now().UTC()  // fallback при відсутності TOT
    }
    
    // 🔹 Розрахувати різницю між поточним PCR та базовим
    pcrDiff := pcr.Duration() - state.basePCR.Duration()
    
    // 🔹 Додати до базового TOT часу + таймзона
    return state.baseTOTTime.Add(pcrDiff).Add(state.timezoneOffset)
}
```

### ✅ 2. Детекція дрейфу часу та автоматична корекція

```go
// monitoring.Monitor — метрики для часової синхронізації:
type TimeSyncMetrics struct {
    TOTDriftGauge       *prometheus.GaugeVec  // різниця TOT time vs NTP
    LastTOTUpdateGauge  *prometheus.GaugeVec  // seconds since last TOT
    TimezoneOffsets     *prometheus.GaugeVec  // зсуви по каналах
    PCRSyncErrors       *prometheus.CounterVec // помилки синхронізації PCR↔TOT
}

// У обробці TOT:
func monitorTOTSync(tot *astits.TOTData, pcr *astits.ClockReference, channelID string, metrics *TimeSyncMetrics) {
    // 🔹 Порівняти з локальним системним часом (через NTP)
    ntpTime := getNTPTime()  // ваш отримувач NTP
    drift := tot.UTCTime.Sub(ntpTime).Seconds()
    
    metrics.TOTDriftGauge.WithLabelValues(channelID).Set(drift)
    metrics.LastTOTUpdateGauge.WithLabelValues(channelID).Set(float64(time.Now().Unix()))
    
    if math.Abs(drift) > 1.0 {  // поріг 1 секунда
        log.Warnf("Channel %s: TOT time drift %.2f seconds (TOT=%v, NTP=%v, PCR_base=%d)", 
            channelID, drift, tot.UTCTime, ntpTime, pcr.Base)
        
        // 🔹 Опція: автоматична корекція базової синхронізації
        if state := syncState[channelID]; state != nil {
            // Скоригувати baseTOTTime на величину дрейфу
            state.baseTOTTime = state.baseTOTTime.Add(-time.Duration(drift * float64(time.Second)))
            log.Infof("Channel %s: auto-corrected baseTOTTime by %.2f seconds", channelID, drift)
        }
    }
}
```

### ✅ 3: Обробка дескриптора `LocalTimeOffset`

```go
// LocalTimeOffset дескриптор (0x58) — детальна структура:
type DescriptorLocalTimeOffsetItem struct {
    CountryCode             []byte      // 3 байти, напр. "UA ", "GBR"
    CountryRegionID         uint8       // 6 біт: ідентифікатор регіону
    LocalTimeOffset         time.Duration // зсув у хвилинах
    LocalTimeOffsetPolarity bool        // true = від'ємний зсув
    TimeOfChange            time.Time   // коли змінюється зсув (DST)
    NextTimeOffset          time.Duration // наступний зсув (після DST)
}

// Helper-функція для витягування зсуву:
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
        programDateTime := calculateProgramDateTime(pcr, channelID).Add(offset)
    }
}
```

### ✅ 4: Fallback логіка при відсутності TOT

```go
// Якщо потік не містить TOT — використати альтернативні джерела часу:
func getFallbackTime(channelID string, pcr *astits.ClockReference) time.Time {
    // 🔹 Спроба 1: час з останньої EIT події
    if lastEIT, ok := eitCache[channelID]; ok && lastEIT.StartTime.IsZero() == false {
        return lastEIT.StartTime
    }
    
    // 🔹 Спроба 2: системний час з корекцією на відому таймзону
    if tz, ok := channelTimezone[channelID]; ok {
        return time.Now().In(tz)
    }
    
    // 🔹 Спроба 3: розрахунок відносно останнього відомого PCR
    if lastPCR, ok := lastPCRCache[channelID]; ok {
        pcrDiff := pcr.Duration() - lastPCR.Duration()
        return lastKnownTime[channelID].Add(pcrDiff)
    }
    
    // 🔹 Fallback: поточний UTC час
    return time.Now().UTC()
}

// Інтеграція у розрахунок програмного часу:
func safeCalculateProgramDateTime(pcr *astits.ClockReference, channelID string) time.Time {
    state := syncState[channelID]
    if state == nil || state.basePCR == nil {
        return getFallbackTime(channelID, pcr)
    }
    
    // Перевірити актуальність даних (TOT може бути застарілим)
    if time.Since(state.lastUpdateTime) > 5*time.Minute {
        log.Debugf("Channel %s: TOT data stale (%v ago), using fallback", 
            channelID, time.Since(state.lastUpdateTime))
        return getFallbackTime(channelID, pcr)
    }
    
    // Нормальний розрахунок
    pcrDiff := pcr.Duration() - state.basePCR.Duration()
    return state.baseTOTTime.Add(pcrDiff).Add(state.timezoneOffset)
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Базовий тест на парсинг

```go
func TestParseTOTSection_Basic(t *testing.T) {
    // Підготувати тестові байти: UTC time + дескриптори
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // UTC time: 2024-05-15 14:30:45 UTC
    writeDVBTime(w, time.Date(2024, 5, 15, 14, 30, 45, 0, time.UTC))
    
    // Reserved bits: 4 біти = 0
    w.WriteN(uint8(0), 4)
    
    // Дескриптори (напр., LocalTimeOffset)
    writeDescriptors(w, []*astits.Descriptor{
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
    })
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    tot, err := parseTOTSection(iter)
    
    assert.NoError(t, err)
    assert.Equal(t, time.Date(2024, 5, 15, 14, 30, 45, 0, time.UTC), tot.UTCTime)
    assert.Len(t, tot.Descriptors, 1)
    assert.Equal(t, astits.DescriptorTagLocalTimeOffset, tot.Descriptors[0].Tag)
}
```

### 🔹 Тест на round-trip (серіалізація ↔ парсинг)

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
    writeDVBTime(w, original.UTCTime)
    w.WriteN(uint8(0), 4)  // reserved
    writeDescriptors(w, original.Descriptors)
    
    // Парсити назад
    parsed, err := parseTOTSection(astikit.NewBytesIterator(buf.Bytes()))
    assert.NoError(t, err)
    
    // Порівняти (з точністю до секунди, бо DVB time не має мікросекунд)
    assert.Equal(t, original.UTCTime.Truncate(time.Second), parsed.UTCTime)
    assert.Len(t, parsed.Descriptors, 1)
}
```

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
        {"Kathmandu +5:45", 345, false, 5*time.Hour + 45*time.Minute},
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // Створити LocalTimeOffset дескриптор
            desc := &astits.Descriptor{
                Tag: astits.DescriptorTagLocalTimeOffset,
                LocalTimeOffset: &astits.DescriptorLocalTimeOffset{
                    Items: []*astits.DescriptorLocalTimeOffsetItem{
                        {
                            CountryCode:             []byte("TEST"),
                            LocalTimeOffset:         time.Duration(tc.offsetMinutes) * time.Minute,
                            LocalTimeOffsetPolarity: tc.polarity,
                        },
                    },
                },
            }
            
            // Серіалізувати та парсити
            buf := &bytes.Buffer{}
            w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
            writeDVBTime(w, time.Now().UTC())
            w.WriteN(uint8(0), 4)
            writeDescriptors(w, []*astits.Descriptor{desc})
            
            parsed, err := parseTOTSection(astikit.NewBytesIterator(buf.Bytes()))
            assert.NoError(t, err)
            
            offset, _ := extractTimezoneOffset(parsed.Descriptors[0])
            assert.Equal(t, tc.expectedOffset, offset)
        })
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірний парсинг MJD | Дати зсуваються на дні/місяці | Перевірити константу: Unix epoch = MJD 40587 (не 40586!) |
| Невалідне BCD у часі | "Години" = 96 замість 60 | Додати валідацію: `if (b>>4)>9 \|\| (b&0xF)>9 { return error }` |
| Часовий пояс не враховано | `PROGRAM-DATE-TIME` у локальному замість UTC | Завжди використовувати `.UTC()` при роботі з DVB time, застосовувати зсув окремо |
| Дескриптори не парсяться | `desc.LocalTimeOffset == nil` | Перевірити, що `parseDescriptors` викликається з правильним `offsetEnd` |
| TOT не надходить у потоці | `NextData()` ніколи не повертає `*DemuxerData` з `TOT` | Це нормально: TOT передається рідко (раз на кілька хвилин); реалізувати fallback |
| DST (літній час) не обробляється | Зсув не змінюється при переході на літній час | Відстежувати `TimeOfChange` у дескрипторі та оновлювати `timezoneOffset` автоматично |

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

- [ETSI EN 300 468: TOT specification (§5.2.6)](https://dvb.org/wp-content/uploads/2019/12/a038_tm1217r37_en300468v1_17_1_-_rev-134_-_si_specification.pdf)
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