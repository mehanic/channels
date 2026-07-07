# Глибоке роз'яснення: ClockReference у MPEG-TS та astits

Цей тип представляє **Program Clock Reference (PCR)** — механізм синхронізації часу в MPEG-TS потоках, критично важливий для A/V синхронізації, PTS нормалізації та корекції дрейфу.

---

## 🎯 Що таке PCR і навіщо він потрібен?

```
┌─────────────────────────────────────────┐
│ PCR (Program Clock Reference):          │
│ • "Еталонний годинник" програми в TS    │
│ • Передається в адаптаційному полі      │
│ • Частота: 27 MHz (точність ~37 ns)     │
│ • Використовується для:                 │
│   - Синхронізації декодерів             │
│   - Відновлення часової шкали PTS/DTS   │
│   - Виявлення дрейфу / розривів         │
└─────────────────────────────────────────┘
```

### Чому дві частини: Base + Extension?

| Компонент | Частота | Діапазон | Призначення |
|-----------|---------|----------|-------------|
| `Base` | 90 kHz | 33 біти (~26.5 год) | Сумісність з PTS/DTS (відео/аудіо таймстемпи) |
| `Extension` | 27 MHz | 9 біт (0-299) | Точна підгонка: 1 base tick = 300 extension ticks |

**Математика:**
```
27 MHz / 90 kHz = 300
→ 1 tick Base = 300 ticks Extension
→ 1 tick Base = 1/90000 sec ≈ 11.111 μs
→ 1 tick Extension = 1/27000000 sec ≈ 37.037 ns

PCR_value = Base × 300 + Extension  // у 27 MHz ticks
```

---

## 🔧 Розбір коду astits

### Структура
```go
type ClockReference struct {
    Base, Extension int64  // PCR_base (90kHz), PCR_ext (27MHz)
}
```

### Duration() — конвертація в time.Duration
```go
func (p ClockReference) Duration() time.Duration {
    return time.Duration(p.Base*1e9/90000) + 
           time.Duration(p.Extension*1e9/27000000)
}
```

**Розрахунок:**
```
Base: p.Base × (1e9 ns / 90000) = p.Base × 11111.111... ns
Ext:  p.Extension × (1e9 / 27e6) = p.Extension × 37.037... ns

Приклад: Base=90000, Extension=150
→ 90000×11111.111 + 150×37.037 = 1_000_000_000 + 5_555 ≈ 1.000005555 сек
```

> ⚠️ **Увага**: Цілочисельне ділення `1e9/90000 = 11111` (втрати ~0.111 нс на tick). Для тривалих потоків може накопичуватися дрейф.

### Time() — конвертація в time.Time
```go
func (p ClockReference) Time() time.Time {
    return time.Unix(0, p.Duration().Nanoseconds())
}
```

**Важливо**: Це **відносний час** від epoch (1970-01-01), а не абсолютний!  
PCR сам по собі не містить інформації про "реальний час" — лише відлік від початку програми.

---

## 🔄 Практичне використання у вашому пайплайні

### ✅ 1. Нормалізація PTS з урахуванням PCR дрейфу

```go
// segmentAssembler.go — корекція дрейфу між сегментами
func normalizePTS(pcr ClockReference, rawPTS int64, expectedPCR ClockReference) int64 {
    // Розрахувати очікуваний PTS на основі PCR
    pcrDuration := pcr.Duration()
    expectedDuration := expectedPCR.Duration()
    
    // Дрейф у наносекундах
    drift := pcrDuration - expectedDuration
    
    // Скоригувати PTS: врахувати дрейф + перевести в 90kHz ticks
    driftTicks := drift.Nanoseconds() * 90000 / 1e9
    
    return rawPTS + driftTicks
}
```

**Інтеграція з вашою логікою:**
```go
// Ваша формула: PTS = (segmentNum-1)*4s*90kHz + drift
func calculateExpectedPTS(segmentNum int, firstPCR ClockReference) int64 {
    baseTicks := int64(segmentNum-1) * 4 * 90000  // 4 секунди × 90 kHz
    return firstPCR.Base*300 + firstPCR.Extension + baseTicks
}
```

### ✅ 2. Синхронізація аудіо/відео через PCR

```go
// orphan audio sync — використання PCR як спільного репера
type SyncAnchor struct {
    PCR       ClockReference
    VideoPTS  int64  // у 90 kHz ticks
    AudioPTS  int64  // у 90 kHz ticks (AAC)
}

func (s SyncAnchor) AudioDelay() time.Duration {
    // Різниця між аудіо та відео у часовій області
    ptsDiff := s.AudioPTS - s.VideoPTS  // у 90 kHz ticks
    return time.Duration(ptsDiff * 1e9 / 90000)
}

// У segmentAssembler:
if orphanAudio, ok := audioCache[seqNum]; ok {
    anchor := SyncAnchor{
        PCR:      currentPCR,  // з адаптаційного поля
        VideoPTS: videoPTS,
        AudioPTS: orphanAudio.PTS,
    }
    
    delay := anchor.AudioDelay()
    if delay.Abs() > 100*time.Millisecond {
        log.Warnf("A/V skew detected: %v (seq=%d)", delay, seqNum)
        // Опція 1: вставити silence
        // Опція 2: скоригувати PTS аудіо
    }
}
```

### ✅ 3. Виявлення розривів у потоці (для HLS playlist)

```go
// VideoManifestProxy.go — детекція розривів >1с
func detectDiscontinuity(prevPCR, currPCR ClockReference, expectedInterval time.Duration) bool {
    actualInterval := currPCR.Duration() - prevPCR.Duration()
    return actualInterval > expectedInterval + time.Second  // поріг 1с
}

// У циклі сканування сегментів:
var lastPCR *ClockReference
for _, seg := range segments {
    pcr := extractPCR(seg)  // парсинг адаптаційного поля
    if lastPCR != nil {
        if detectDiscontinuity(*lastPCR, pcr, 4*time.Second) {
            // Вставити #EXT-X-DISCONTINUITY у playlist
            playlist.AddDiscontinuity()
            // Скинути дрейф-корекцію
            driftAccumulator = 0
        }
    }
    lastPCR = &pcr
}
```

### ✅ 4. Конвертація в PROGRAM-DATE-TIME для HLS

```go
// Якщо відомий "реальний час" першого PCR (наприклад, з NTP/PTP):
func pcrToProgramDateTime(pcr ClockReference, baseRealTime time.Time, basePCR ClockReference) time.Time {
    // Різниця між поточним і базовим PCR
    delta := pcr.Duration() - basePCR.Duration()
    return baseRealTime.Add(delta)
}

// У генерації HLS:
baseTime := segmentZeroStartTime  // з вашого time sync алгоритму
basePCR := firstSegmentPCR

for _, seg := range segments {
    pcr := extractPCR(seg)
    programDateTime := pcrToProgramDateTime(pcr, baseTime, basePCR)
    
    playlist.AddSegment(seg, 
        astits.ProgramDateTime(programDateTime),  // #EXT-X-PROGRAM-DATE-TIME
    )
}
```

---

## 🧮 Точність та обмеження

### Проблема цілочисельного ділення в Duration()

```go
// Поточна реалізація:
time.Duration(p.Base*1e9/90000)  // 1e9/90000 = 11111 (втрати 0.111...)

// Наслідки:
// За 1 годину (3600 сек × 90000 ticks = 324_000_000 ticks):
// Втрата: 324_000_000 × 0.111 нс ≈ 36 мс дрейфу

// Більш точна альтернатива:
func (p ClockReference) DurationPrecise() time.Duration {
    // Використовуємо float для проміжних обчислень
    baseNs := float64(p.Base) * (1e9 / 90000.0)
    extNs := float64(p.Extension) * (1e9 / 27000000.0)
    return time.Duration(baseNs + extNs)
}
```

### Wrap-around PCR (33-бітний Base)

```go
// PCR Base — 33 біти, максимальне значення: 2^33 - 1 = 8_589_934_591
// Це ≈ 26.5 годин при 90 kHz

func handlePCRWraparound(prev, curr ClockReference) int64 {
    const maxBase = (1 << 33) - 1
    
    if curr.Base < prev.Base && prev.Base - curr.Base > maxBase/2 {
        // Виявлено wrap-around: curr "перестрибнув" через максимум
        return curr.Base + maxBase + 1
    }
    return curr.Base
}
```

> 💡 **Порада**: Для 24/7 CCTV потоків обов'язково обробляйте wrap-around, інакше через ~26 год синхронізація "зламається".

---

## 🛠️ Інтеграція з вашою архітектурою

### У segmentFinalizer — валідація через ffprobe + PCR

```go
func validateSegmentWithPCR(tsData []byte, expectedPCR ClockReference) error {
    // 1. Парсинг першого PCR з TS
    dmx := astits.NewDemuxer(ctx, bytes.NewReader(tsData))
    pkt, err := dmx.NextPacket()
    if err != nil { return err }
    
    if pkt.AdaptationField != nil && pkt.AdaptationField.HasPCR {
        actualPCR := pkt.AdaptationField.PCR
        drift := actualPCR.Duration() - expectedPCR.Duration()
        
        if drift.Abs() > 50*time.Millisecond {
            return fmt.Errorf("PCR drift too large: %v", drift)
        }
    }
    
    // 2. Додаткова перевірка через ffprobe (як у вас)
    return validateWithFFprobe(tsData)
}
```

### У monitoring — метрики дрейфу для Prometheus

```go
// monitoring.Monitor
type Metrics struct {
    PCRDriftGauge *prometheus.GaugeVec      // поточний дрейф у мс
    PCRWrapCounter *prometheus.CounterVec   // кількість wrap-around
}

// У обробці сегмента:
driftMs := (actualPCR.Duration() - expectedPCR.Duration()).Seconds() * 1000
metrics.PCRDriftGauge.WithLabelValues(channelID).Set(driftMs)

if handlePCRWraparound(lastPCR, actualPCR) != actualPCR.Base {
    metrics.PCRWrapCounter.WithLabelValues(channelID).Inc()
}
```

### У WebSocketDistributor — синхронізація субтитрів з відео

```go
// SubtitleMessage тепер може містити PCR-репер:
type SubtitleMessage struct {
    Seq        int64  `json:"seq"`
    TimeStart  int64  `json:"time_start"`   // у 90 kHz ticks від початку сегмента
    PCRBase    int64  `json:"pcr_base,omitempty"`    // абсолютний PCR для синхронізації
    PCRExtension int64 `json:"pcr_ext,omitempty"`
    // ... інші поля
}

// На клієнті: відтворення субтитрів відносно PCR
func scheduleSubtitle(msg SubtitleMessage, streamPCR ClockReference) {
    subtitleTime := time.Duration(msg.TimeStart * 1e9 / 90000)
    pcrTime := streamPCR.Duration()
    
    delay := subtitleTime - pcrTime
    time.AfterFunc(delay, func() {
        renderSubtitle(msg.Text)
    })
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|-----------|---------|
| Накопичення дрейфу | Аудіо відстає на секунди через годину | Використовувати `DurationPrecise()` + періодична ре-синхронізація по ключових кадрах |
| Невірний Time() | PROGRAM-DATE-TIME показує 1970 рік | Пам'ятати: `PCR.Time()` — відносний час! Додавати baseRealTime |
| Пропуск PCR | Адаптаційне поле без PCR у більшості пакетів | Шукати PCR тільки в пакетах з PID = PMT.PCR_PID (зберегти при парсингу PMT) |
| Wrap-around не оброблено | Через 26 год синхронізація "стрибає" | Реалізувати `handlePCRWraparound()` з порогом `maxBase/2` |

---

## 📊 Порівняння: PCR vs PTS vs PROGRAM-DATE-TIME

```
┌─────────────────┬──────────────┬────────────────────────────┐
│ Тип часу        │ Частота      │ Призначення                │
├─────────────────┼──────────────┼────────────────────────────┤
│ PCR             │ 27 MHz       │ Синхронізація декодера    │
│ PTS/DTS         │ 90 kHz       │ Таймінг відтворення кадрів│
│ PROGRAM-DATE-TIME│ 1 сек       │ Абсолютний час для HLS    │
└─────────────────┴──────────────┴────────────────────────────┘

Workflow:
1. PCR → відновлення часової шкали декодера
2. PTS/DTS + PCR → синхронізація A/V відтворення  
3. Програмний mapping: PCR → PROGRAM-DATE-TIME → HLS playlist
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Отримати PCR з пакета
if pkt.AdaptationField != nil && pkt.AdaptationField.HasPCR {
    pcr := pkt.AdaptationField.PCR  // *astits.ClockReference
    
    // 2. Конвертувати для порівняння
    duration := pcr.Duration()           // time.Duration
    // або точніше:
    precise := time.Duration(
        float64(pcr.Base)*1e9/90000 + 
        float64(pcr.Extension)*1e9/27000000,
    )
    
    // 3. Використати для синхронізації
    if lastPCR != nil {
        drift := duration - lastPCR.Duration()
        if drift > maxAllowedDrift {
            // корекція або DISCONTINUITY
        }
    }
    lastPCR = pcr
}

// 4. Для HLS PROGRAM-DATE-TIME:
baseRealTime := segmentZeroUTC  // ваш time-sync алгоритм
basePCR := firstSegmentPCR
currentTime := baseRealTime.Add(pcr.Duration() - basePCR.Duration())
```

---

> 💡 **Ключова ідея**: PCR — це не "час доби", а **відносний годинник програми**. Ваша задача — прив'язати його до реального часу (NTP/PTP/UTC) один раз на початку сесії, а далі використовувати для точної A/V синхронізації та детекції аномалій.

Якщо потрібно — можу допомогти реалізувати:
- 🔄 Автоматичну ре-синхронізацію по ключових кадрах з урахуванням PCR
- 📡 Інтеграцію PTP/NTP для ініціалізації `baseRealTime`
- 🧪 Unit-тести для `handlePCRWraparound()` та дрейф-корекції

🛠️