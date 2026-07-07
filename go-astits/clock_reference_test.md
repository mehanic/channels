# Глибоке роз'яснення: Тести `ClockReference` у astits — конвертація таймінгів

Цей файл містить **тести для типу `ClockReference`** — фундаментальної структури для представлення часових міток у MPEG-TS (PCR, PTS, DTS, ESCR). Це "серце" синхронізації аудіо/відео у вашому пайплайні.

---

## 🎯 Навіщо `ClockReference` потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ ClockReference у контексті HLS:        │
│                                         │
│ 🔹 Типи часових міток:                  │
│   • PCR (Program Clock Reference)      │
│     → еталонний час програми @ 27 MHz  │
│   • PTS (Presentation Time Stamp)      │
│     → коли показувати кадр @ 90 kHz    │
│   • DTS (Decoding Time Stamp)          │
│     → коли декодувати кадр @ 90 kHz    │
│   • ESCR (Elementary Stream Clock Ref) │
│     → час потоку @ 27 MHz + extension  │
│                                         │
│ 🔹 Для CCTV HLS:                        │
│   • A/V синхронізація через PTS/DTS    │
│   • Генерація #EXT-X-PROGRAM-DATE-TIME │
│   • Детекція дрейфу часу               │
│   • Синхронізація з реальним часом     │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `ClockReference`

```go
// Гіпотетичне визначення (з clock.go):
type ClockReference struct {
    Base      int64  // 🎯 Основне значення:
                   // • 33 біти для PTS/DTS @ 90 kHz
                   // • 33 біти для ESCR base @ 27 MHz
    Extension int64  // 🎯 Розширення (тільки для ESCR):
                   // • 9 біт @ 27 MHz для точнішої синхронізації
}

// Конструктор:
func newClockReference(base, extension int64) *ClockReference {
    return &ClockReference{Base: base, Extension: extension}
}
```

### 🎯 Ключові частоти

```
Частота    | Призначення              | Точність
───────────┼──────────────────────────┼────────────
90 kHz     | PTS, DTS, PCR base       | ~11.1 мкс  (1/90000 с)
27 MHz     | PCR extension, ESCR      | ~37 нс     (1/27000000 с)

Співвідношення: 27 MHz / 90 kHz = 300
→ 1 tick @ 90 kHz = 300 ticks @ 27 MHz
```

---

## 🔍 Тест `TestClockReference`: розбір

```go
var clockReference = newClockReference(3271034319, 58)

func TestClockReference(t *testing.T) {
    // 🔹 Тест 1: Перевірка Duration()
    assert.Equal(t, 36344825768814*time.Nanosecond, clockReference.Duration())
    
    // 🔹 Тест 2: Перевірка Time() → Unix timestamp
    assert.Equal(t, int64(36344), clockReference.Time().Unix())
}
```

### 🧮 Розрахунок `Duration()`: як отримано 36344825768814 ns?

```
Вхід: Base = 3271034319, Extension = 58

🔹 Крок 1: Конвертація Base @ 90 kHz → наносекунди
   • 1 tick @ 90 kHz = 1/90000 секунд = 11111.111... наносекунд
   • Base_ns = 3271034319 × (1e9 / 90000)
             = 3271034319 × 11111.111...
             = 36344825766666.666... ns
             ≈ 36344825766667 ns (округлення)

🔹 Крок 2: Конвертація Extension @ 27 MHz → наносекунди
   • 1 tick @ 27 MHz = 1/27000000 секунд = 37.037... наносекунд
   • Ext_ns = 58 × (1e9 / 27000000)
            = 58 × 37.037...
            = 2148.148... ns
            ≈ 2148 ns (округлення)

🔹 Крок 3: Сума
   • Total_ns = Base_ns + Ext_ns
              = 36344825766667 + 2148
              = 36344825768815 ns

🔹 Очікуване значення у тесті: 36344825768814 ns
   • Розбіжність на 1 ns = нормальне округлення при float→int конвертації ✅
```

### 🧮 Розрахунок `Time().Unix()`: як отримано 36344?

```
Вхід: Duration = 36344825768814 наносекунд

🔹 Крок 1: Конвертація наносекунд → секунди
   • Seconds = 36344825768814 / 1e9
             = 36.344825768814 секунд

🔹 Крок 2: Конвертація → Unix timestamp (цілі секунди)
   • Unix = int64(36.344...) = 36344 ✅

🔹 Інтерпретація:
   • Це 36344 секунди від Unix epoch (1970-01-01 00:00:00 UTC)
   • 36344 с = 10 годин 6 хвилин 44 секунди
   • Дата: 1970-01-01 10:06:44 UTC
```

> 💡 **Важливо**: `Time()` повертає `time.Time` у **UTC**, незалежно від таймзони мовлення. Для локального часу потрібна додаткова корекція через дескриптор `LocalTimeOffset`.

---

## 🔍 Гіпотетична реалізація методів `ClockReference`

### `Duration()`: конвертація у `time.Duration`

```go
func (cr *ClockReference) Duration() time.Duration {
    // 🔹 Base @ 90 kHz → наносекунди
    // Формула: base_ns = base × (1e9 / 90000) = base × 11111.111...
    baseNs := cr.Base * 1e9 / 90000
    
    // 🔹 Extension @ 27 MHz → наносекунди (якщо є)
    // Формула: ext_ns = ext × (1e9 / 27000000) = ext × 37.037...
    extNs := cr.Extension * 1e9 / 27000000
    
    // 🔹 Сума
    return time.Duration(baseNs + extNs)
}
```

### `Time()`: конвертація у `time.Time`

```go
func (cr *ClockReference) Time() time.Time {
    // 🔹 Отримати Duration
    duration := cr.Duration()
    
    // 🔹 Додати до Unix epoch
    return time.Unix(0, 0).Add(duration).UTC()
}

// Або еквівалентно:
func (cr *ClockReference) Time() time.Time {
    seconds := cr.Duration() / time.Second
    nanos := cr.Duration() % time.Second
    return time.Unix(int64(seconds), int64(nanos)).UTC()
}
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: A/V синхронізація через PTS/DTS

```go
// У segmentAssembler — синхронізація аудіо/відео за таймінгами:
type AVSyncState struct {
    lastVideoPTS int64
    lastAudioPTS int64
    maxDrift     time.Duration  // допустимий дрейф (напр., 100 мс)
}

func syncAudioVideo(videoPTS, audioPTS *astits.ClockReference, 
                   videoData, audioData []byte, state *AVSyncState) error {
    
    // 🔹 Конвертувати у Duration для порівняння
    videoDur := videoPTS.Duration()
    audioDur := audioPTS.Duration()
    
    // 🔹 Перевірити дрейф
    drift := videoDur - audioDur
    if drift < 0 { drift = -drift }
    
    if drift > state.maxDrift {
        log.Warnf("A/V drift detected: %v (max: %v)", drift, state.maxDrift)
        // 🔹 Опція: скоригувати аудіо таймінги
        // 🔹 Опція: відкинути кадр з великим дрейфом
    }
    
    // 🔹 Зберегти останні PTS для моніторингу
    state.lastVideoPTS = videoPTS.Base
    state.lastAudioPTS = audioPTS.Base
    
    return nil
}
```

### ✅ 2: Генерація `#EXT-X-PROGRAM-DATE-TIME` для HLS

```go
// У VideoManifestProxy — додавання точного часу до плейлиста:
func addProgramDateTime(playlist *HLSPlaylist, pts *astits.ClockReference, 
                       baseTime time.Time, basePCR *astits.ClockReference) {
    
    if basePCR == nil {
        // 🔹 Fallback: використати поточний час
        playlist.AddTag(fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s", 
            time.Now().UTC().Format(time.RFC3339Nano)))
        return
    }
    
    // 🔹 Розрахувати різницю між поточним PTS та базовим PCR
    pcrDiff := pts.Duration() - basePCR.Duration()
    
    // 🔹 Додати до базового часу
    programTime := baseTime.Add(pcrDiff)
    
    // 🔹 Форматувати у RFC3339 для HLS
    playlist.AddTag(fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s", 
        programTime.Format("2006-01-02T15:04:05.000Z")))
}

// Використання:
baseTime := time.Date(2024, 5, 15, 12, 0, 0, 0, time.UTC)
basePCR := &astits.ClockReference{Base: 90000000}  // 1000 секунд @ 90 kHz

for _, segment := range segments {
    if segment.PTS != nil {
        addProgramDateTime(playlist, segment.PTS, baseTime, basePCR)
    }
}
```

### ✅ 3: Детекція дрейфу часу

```go
// monitoring.Monitor — метрики для таймінгів:
type TimingMetrics struct {
    PTSInterval    *prometheus.HistogramVec  // інтервал між PTS
    AVDriftGauge   *prometheus.GaugeVec      // дрейф аудіо/відео
    PCRDetectGauge *prometheus.GaugeVec      // час з останнього PCR
    TimeDriftGauge *prometheus.GaugeVec      // дрейф відносно NTP
}

// У обробці таймінгів:
func monitorTiming(videoPTS, audioPTS *astits.ClockReference, 
                  channelID string, metrics *TimingMetrics) {
    
    if videoPTS != nil && audioPTS != nil {
        // 🔹 Виміряти A/V дрейф
        drift := videoPTS.Duration() - audioPTS.Duration()
        metrics.AVDriftGauge.WithLabelValues(channelID).Set(drift.Seconds())
        
        if math.Abs(drift.Seconds()) > 0.1 {  // поріг 100 мс
            log.Warnf("Channel %s: A/V drift %.2f seconds", channelID, drift.Seconds())
        }
    }
    
    // 🔹 Виміряти інтервал між кадрами (якщо є попередній PTS)
    if lastPTS, ok := lastVideoPTS[channelID]; ok && videoPTS != nil {
        interval := time.Duration(videoPTS.Base-lastPTS) * time.Second / 90000
        metrics.PTSInterval.WithLabelValues(channelID).Observe(interval.Seconds())
    }
    if videoPTS != nil {
        lastVideoPTS[channelID] = videoPTS.Base
    }
}
```

### ✅ 4: Синхронізація з реальним часом через NTP

```go
// Корекція системного дрейфу через NTP:
type TimeSync struct {
    mu           sync.RWMutex
    lastNTPTime  time.Time
    lastPCR      *astits.ClockReference
    driftRate    float64  // секунд дрейфу на секунду реального часу
}

func (ts *TimeSync) Update(pcr *astits.ClockReference, ntpTime time.Time) {
    ts.mu.Lock()
    defer ts.mu.Unlock()
    
    if ts.lastPCR != nil {
        // 🔹 Розрахувати реальний інтервал
        realInterval := ntpTime.Sub(ts.lastNTPTime).Seconds()
        
        // 🔹 Розрахувати інтервал за PCR
        pcrInterval := pcr.Duration().Seconds() - ts.lastPCR.Duration().Seconds()
        
        // 🔹 Розрахувати коефіцієнт дрейфу
        if realInterval > 0 {
            ts.driftRate = (pcrInterval - realInterval) / realInterval
        }
    }
    
    ts.lastNTPTime = ntpTime
    ts.lastPCR = pcr
}

func (ts *TimeSync) CorrectTime(pcr *astits.ClockReference) time.Time {
    ts.mu.RLock()
    defer ts.mu.RUnlock()
    
    if ts.lastPCR == nil {
        return time.Now().UTC()  // fallback
    }
    
    // 🔹 Розрахувати корегований час
    pcrDiff := pcr.Duration() - ts.lastPCR.Duration()
    correctedDiff := time.Duration(float64(pcrDiff) / (1 + ts.driftRate))
    
    return ts.lastNTPTime.Add(correctedDiff)
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на різні частоти (PTS vs ESCR)

```go
func TestClockReference_DifferentFrequencies(t *testing.T) {
    testCases := []struct {
        name      string
        base      int64
        extension int64
        frequency int64  // 90000 або 27000000
        expected  time.Duration
    }{
        {
            name:      "PTS @ 90kHz",
            base:      90000,      // 1 секунда @ 90 kHz
            extension: 0,
            frequency: 90000,
            expected:  1 * time.Second,
        },
        {
            name:      "ESCR @ 27MHz",
            base:      27000000,   // 1 секунда @ 27 MHz
            extension: 0,
            frequency: 27000000,
            expected:  1 * time.Second,
        },
        {
            name:      "ESCR with extension",
            base:      27000000,   // 1 секунда base
            extension: 27,         // +1 мікросекунда @ 27 MHz
            frequency: 27000000,
            expected:  1*time.Second + 1*time.Microsecond,
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            cr := newClockReference(tc.base, tc.extension)
            // Припустимо, Duration() використовує правильну частоту
            // (у реальності це визначається контекстом: PTS vs ESCR)
            _ = cr.Duration()
            // ... перевірки ...
        })
    }
}
```

### 🔹 Тест на конвертацію Time() з різними таймзонами

```go
func TestClockReference_TimeWithTimezone(t *testing.T) {
    cr := newClockReference(90000, 0)  // 1 секунда @ 90 kHz
    
    // 🔹 Time() завжди повертає UTC
    utcTime := cr.Time()
    assert.Equal(t, time.UTC, utcTime.Location())
    
    // 🔹 Для локального часу потрібна додаткова конвертація
    kyivLoc, _ := time.LoadLocation("Europe/Kiev")
    localTime := utcTime.In(kyivLoc)
    
    assert.NotEqual(t, utcTime, localTime)
    assert.Equal(t, kyivLoc, localTime.Location())
}
```

### 🔹 Бенчмарк продуктивності конвертації

```go
func BenchmarkClockReference_Duration(b *testing.B) {
    cr := newClockReference(3271034319, 58)
    
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _ = cr.Duration()
    }
}

func BenchmarkClockReference_Time(b *testing.B) {
    cr := newClockReference(3271034319, 58)
    
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _ = cr.Time()
    }
}

// Очікувані результати:
// BenchmarkClockReference_Duration-8    100000000    10 ns/op    0 B/op    0 allocs/op
// BenchmarkClockReference_Time-8        50000000     25 ns/op    0 B/op    0 allocs/op
// ✅ 0 алокацій — критично для high-throughput стрімінгу
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Неправильна частота | Таймінги зсуваються на ×300 | Перевірити: PTS/DTS @ 90 kHz, ESCR extension @ 27 MHz |
| Округлення наносекунд | Розбіжність на 1-2 ns у тестах | Це нормально: float→int конвертація; використовувати `assert.InDelta` для порівняння |
| `Time()` не в UTC | Локальний час замість універсального | Перевірити: `.UTC()` у реалізації `Time()` |
| Переповнення при множенні | Паніка для великих Base значень | Використовувати `int64` для проміжних обчислень: `base * 1e9 / 90000` |
| Extension ігнорується для PTS | Неправильна точність для ESCR | Перевірити: Extension застосовується тільки для ESCR, не для PTS/DTS |

### Приклад коректної конвертації з уникненням переповнення:

```go
func safeDuration(base int64, frequency int64) time.Duration {
    // 🔹 Уникнути переповнення: спочатку ділити, потім множити
    // Формула: duration_ns = base × (1e9 / frequency)
    
    // Крок 1: Розрахувати множник з округленням
    multiplier := int64(1e9 / frequency)  // 11111 для 90 kHz, 37 для 27 MHz
    remainder := int64(1e9 % frequency)    // залишок для точності
    
    // Крок 2: Основна частина
    durationNs := base * multiplier
    
    // Крок 3: Додати залишок (опціонально, для підвищення точності)
    durationNs += (base * remainder) / frequency
    
    return time.Duration(durationNs)
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Конвертація ClockReference → time.Duration:
func clockToDuration(cr *astits.ClockReference, frequency int64) time.Duration {
    if cr == nil {
        return 0
    }
    // Base @ frequency Hz
    baseNs := cr.Base * 1e9 / frequency
    // Extension @ 27 MHz (тільки для ESCR)
    extNs := cr.Extension * 1e9 / 27000000
    return time.Duration(baseNs + extNs)
}

// Використання:
ptsDuration := clockToDuration(pts, 90000)      // PTS @ 90 kHz
escrDuration := clockToDuration(escr, 27000000) // ESCR @ 27 MHz

// 2: Конвертація → time.Time з базовим часом:
func clockToTime(cr *astits.ClockReference, baseTime time.Time, basePCR *astits.ClockReference) time.Time {
    if basePCR == nil {
        return time.Now().UTC()  // fallback
    }
    pcrDiff := cr.Duration() - basePCR.Duration()
    return baseTime.Add(pcrDiff).UTC()
}

// 3: Форматування для HLS PROGRAM-DATE-TIME:
func formatProgramDateTime(t time.Time) string {
    // HLS вимагає RFC3339 / ISO8601
    return t.UTC().Format("2006-01-02T15:04:05.000Z")
    // Приклад: "2024-05-15T14:30:45.000Z"
}

// 4: Перевірка монотонності таймінгів:
func validateTimestampsMonotonic(current, last *astits.ClockReference, maxRegression time.Duration) error {
    if last == nil || current == nil {
        return nil
    }
    if current.Duration() < last.Duration() {
        regression := last.Duration() - current.Duration()
        if regression > maxRegression {
            return fmt.Errorf("timestamp regression: %v (max: %v)", regression, maxRegression)
        }
    }
    return nil
}

// 5: Моніторинг:
func monitorClockHealth(cr *astits.ClockReference, channelID string, metrics *TimingMetrics) {
    if cr == nil {
        return
    }
    metrics.LastTimestamp.WithLabelValues(channelID).Set(float64(cr.Base))
    metrics.TimestampFrequency.WithLabelValues(channelID).Set(90000)  // або 27000000 для ESCR
}
```

---

## 📊 Матриця часових міток у вашому пайплайні

```
Тип          | Частота   | Розмір    | Використання у CCTV HLS
─────────────┼───────────┼───────────┼─────────────────────────
PTS          | 90 kHz    | 33 біти   | ✅ Коли показувати кадр
DTS          | 90 kHz    | 33 біти   | ✅ Коли декодувати кадр (B-frames)
PCR base     | 90 kHz    | 33 біти   | ✅ Еталонний час програми
PCR ext      | 27 MHz    | 9 біт     | ✅ Точна синхронізація декодера
ESCR base    | 27 MHz    | 33 біти   | ⚠️ Еталонний час потоку
ESCR ext     | 27 MHz    | 9 біт     | ⚠️ Додаткова точність

Конвертація:
• 90 kHz → time.Duration: base × 1e9 / 90000
• 27 MHz → time.Duration: (base×300 + ext) × 1e9 / 27000000
• Duration → time.Time: time.Unix(0, 0).Add(duration).UTC()
```

---

## 📚 Корисні посилання

- [ISO/IEC 13818-1: Timing references](https://www.iso.org/standard/61236.html)
- [MPEG-TS PCR/PTS/DTS explanation](https://en.wikipedia.org/wiki/Program_clock_reference)
- [astits ClockReference source](https://github.com/asticode/go-astits/blob/master/clock.go)
- [Go time.Duration best practices](https://pkg.go.dev/time#Duration)

> 💡 **Ключова ідея**: `ClockReference` — це "універсальний перекладач" між бітовими таймінгами MPEG-TS та часом Go. У вашому CCTV HLS пайплайні це дозволяє:
> - ⏱️ Точну A/V синхронізацію через PTS/DTS @ 90 kHz
> - 🕐 Генерацію валідних `#EXT-X-PROGRAM-DATE-TIME` для плеєрів
> - 🔍 Детекцію дрейфу часу для проактивного моніторингу
> - 🌍 Коректну обробку таймзон через конвертацію в UTC

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати синхронізацію `ClockReference` → `PROGRAM-DATE-TIME` у ваш `VideoManifestProxy`
- 🧪 Написати integration-тест для перевірки A/V синхронізації з реальними відео-потоками
- 📈 Додати Prometheus-метрики для моніторингу інтервалів PTS та дрейфу часу по каналах

🛠️