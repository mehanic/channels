# 📦 Глибокий розбір: `timescale` — Утиліти для точної конвертації медіа-таймінгів

Цей файл — **допоміжні функції для безпечної конвертації часу** між `time.Duration` та довільною шкалою часу (timescale) у медіа-контейнерах. Він надає механізми для перетворення без втрати точності, з підтримкою округлення та обробки від'ємних значень для relative time.

---

## 🗺️ Архітектурна схема timescale утиліт

```
┌────────────────────────────────────────┐
│ 📦 timescale — Time Conversion Utils  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • ToScale() — Duration → ticks (uint64)│
│  • Relative() — relative Duration → int32│
│  • 64-бітна арифметика для точності   │
│  • Округлення до найближчого ticks    │
│                                         │
│  🔄 Формули конвертації:                │
│  • ticks = duration * scale / 1s      │
│  • rounding: rem >= 0.5s → round up   │
│  • relative: підтримка від'ємних значень│
│                                         │
│  📡 Використання:                       │
│  • DTS/PTS розрахунок у MP4/WebM      │
│  • Синхронізація аудіо/відео          │
│  • Low-latency streaming таймінги     │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. ToScale() — конвертація Duration → ticks

### 🔧 Реалізація:

```go
func ToScale(t time.Duration, scale uint32) uint64 {
    hi, lo := bits.Mul64(uint64(t), uint64(scale))
    dts, rem := bits.Div64(hi, lo, uint64(time.Second))
    if rem >= uint64(time.Second/2) {
        // round up
        dts++
    }
    return dts
}
```

### 🔍 Як працює:

```
Формула: ticks = duration * scale / time.Second

Проблема прямого обчислення:
    • duration (int64 наносекунд) * scale (uint32) може переповнити int64
    • Приклад: 1 годину * 90000 = 3.6e9 * 9e4 = 3.24e14 > max int64

Рішення: 64-бітна арифметика через bits.Mul64/Div64:

1. bits.Mul64(uint64(t), uint64(scale)):
   • Повертає 128-бітний результат як (hi:64, lo:64)
   • hi = старші 64 біти добутку, lo = молодші 64 біти

2. bits.Div64(hi, lo, uint64(time.Second)):
   • Ділить 128-бітне число (hi:lo) на 1_000_000_000 (1 секунда у наносекундах)
   • Повертає частку (dts) та остачу (rem)

3. Округлення:
   • Якщо остача >= 0.5 секунди → округлюємо вгору
   • rem >= uint64(time.Second/2) = 500_000_000

Приклад:
    t = 1_500_000_000 ns (1.5 секунди)
    scale = 90_000 (90kHz для відео)
    
    1.5 * 90000 = 135000 ticks (точне значення)
    
    Якщо t = 1_500_000_001 ns (1.500000001 секунди):
    • 1.500000001 * 90000 = 135000.00009
    • Залишок = 90 наносекунд * 90000 = 8_100_000 < 500_000_000
    • Округлення вниз → 135000 ticks
    
    Якщо t = 1_500_500_000 ns (1.5005 секунди):
    • Залишок = 500_000_000 * 90000 = 45_000_000_000 >= 500_000_000
    • Округлення вгору → 135001 ticks
```

### ⚠️ Критична проблема: переповнення при дуже великих значеннях

```
У поточній реалізації:
    hi, lo := bits.Mul64(uint64(t), uint64(scale))

Проблема:
• time.Duration — це int64 наносекунд
• Якщо t < 0 (від'ємний час, наприклад для CTS/B-frames) → uint64(t) дасть велике додатне число
• Це призведе до некоректного розрахунку

✅ Виправлення: обробка від'ємних значень
    func ToScaleSigned(t time.Duration, scale uint32) (int64, error) {
        if t >= 0 {
            return int64(ToScale(t, scale)), nil
        }
        
        // Для від'ємних значень: конвертуємо абсолютне значення та змінюємо знак
        absTicks := ToScale(-t, scale)
        if absTicks > math.MaxInt64 {
            return 0, fmt.Errorf("overflow for negative time: %v", t)
        }
        return -int64(absTicks), nil
    }
```

### ✅ Ваш use-case**: розрахунок DTS для відео семплу

```go
// CalculateDTS — розрахунок Decoding Time Stamp у ticks
func CalculateDTS(pkt av.Packet, timeScale uint32) uint64 {
    // pkt.Time — це PTS у time.Duration
    // Для відео без B-frames: DTS = PTS
    return timescale.ToScale(pkt.Time, timeScale)
}

// CalculatePTSWithCTS — розрахунок PTS з урахуванням Composition Time
func CalculatePTSWithCTS(pkt av.Packet, timeScale uint32) uint64 {
    // PTS = DTS + CTS (Composition Time Shift)
    dts := timescale.ToScale(pkt.Time, timeScale)
    cts := timescale.Relative(pkt.CompositionTime, timeScale)  // може бути від'ємним
    return uint64(int64(dts) + int64(cts))
}

// Використання у фрагментаторі:
for _, pkt := range packets {
    dts := CalculateDTS(pkt, 90000)  // 90kHz для відео
    pts := CalculatePTSWithCTS(pkt, 90000)
    
    entry := TrackFragRunEntry{
        Duration: timescale.ToScale(pkt.Duration, 90000),
        // ... інші поля ...
    }
    // Додавання entry у trun таблицю...
}
```

---

## 🔑 2. Relative() — конвертація relative Duration → int32

### 🔧 Реалізація:

```go
func Relative(t time.Duration, scale uint32) int32 {
    rel := int64(t) * int64(scale) / int64(time.Second/2)
    if (rel&1 != 0) == (t > 0) {
        // round up
        rel++
    }
    return int32(rel >> 1)
}
```

### 🔍 Як працює:

```
Призначення: конвертація відносного часу (може бути від'ємним) у ticks з округленням.

Формула: result = (t * scale / (time.Second/2) + rounding) >> 1

Кроки:
1. rel = int64(t) * int64(scale) / int64(time.Second/2)
   • Ділення на time.Second/2 замість time.Second дає подвійну точність
   • Це дозволяє округлити до найближчого ticks після зсуву вправо

2. Округлення:
   • (rel&1 != 0) == (t > 0) перевіряє чи потрібно округлити вгору
   • Якщо rel непарний (rel&1 != 0) І t додатний → округлюємо вгору
   • Якщо rel непарний І t від'ємний → округлюємо вниз (для симетрії)
   • rel++ для округлення вгору

3. rel >> 1: зсув вправо на 1 біт = ділення на 2
   • Повертає фінальне значення у ticks

Приклад для додатного t:
    t = 500_000_000 ns (0.5 секунди)
    scale = 90_000
    
    1. rel = 500_000_000 * 90_000 / 500_000_000 = 90_000
    2. rel&1 = 0 (парне) → не округлюємо
    3. rel >> 1 = 45_000 ticks ✓

Приклад для від'ємного t:
    t = -500_000_000 ns (-0.5 секунди)
    scale = 90_000
    
    1. rel = -500_000_000 * 90_000 / 500_000_000 = -90_000
    2. rel&1 = 0 (парне) → не округлюємо
    3. rel >> 1 = -45_000 ticks ✓

Приклад округлення:
    t = 500_000_001 ns (трохи більше 0.5 секунди)
    1. rel = 500_000_001 * 90_000 / 500_000_000 = 90_000.00018 → 90_000 (ціле ділення)
    2. rel&1 = 0 → не округлюємо
    3. Результат: 45_000 ticks
    
    t = 500_500_000 ns (0.5005 секунди)
    1. rel = 500_500_000 * 90_000 / 500_000_000 = 90_090
    2. rel&1 = 0 (парне) → не округлюємо
    3. rel >> 1 = 45_045 ticks
```

### ⚠️ Критична проблема: переповнення при великих значеннях

```
У поточній реалізації:
    rel := int64(t) * int64(scale) / int64(time.Second/2)

Проблема:
• int64(t) * int64(scale) може переповнити int64
• Приклад: t = 1 годину = 3.6e12 ns, scale = 90_000
• 3.6e12 * 9e4 = 3.24e17 > max int64 (9.22e18) — ще поміщається, але близько

✅ Виправлення: перевірка переповнення або використання 128-бітної арифметики
    func RelativeSafe(t time.Duration, scale uint32) (int32, error) {
        // Перевірка чи значення поміщається у int32 після конвертації
        maxTicks := int64(math.MaxInt32)
        maxDuration := time.Duration(maxTicks) * time.Second / time.Duration(scale)
        
        if t > maxDuration || t < -maxDuration {
            return 0, fmt.Errorf("time %v out of int32 range for scale %d", t, scale)
        }
        
        // Безпечне обчислення
        rel := int64(t) * int64(scale) / int64(time.Second/2)
        if (rel&1 != 0) == (t > 0) {
            rel++
        }
        result := int32(rel >> 1)
        
        // Перевірка переповнення при конвертації у int32
        if int64(result) != (rel >> 1) {
            return 0, fmt.Errorf("overflow converting to int32: %d", rel>>1)
        }
        
        return result, nil
    }
```

### ✅ Ваш use-case**: розрахунок Composition Time Offset для B-frames

```go
// CalculateCTSTicks — розрахунок Composition Time Shift у ticks (може бути від'ємним)
func CalculateCTSTicks(cts time.Duration, timeScale uint32) int32 {
    return timescale.Relative(cts, timeScale)
}

// Використання у TrackFragRun для відео з B-frames:
for i, pkt := range videoPackets {
    entry := TrackFragRunEntry{
        Duration: timescale.ToScale(pkt.Duration, timeScale),
        Size:     uint32(len(pkt.Data)),
    }
    
    // Встановлення прапорців
    if pkt.IsKeyFrame {
        entry.Flags = SampleNoDependencies
    } else {
        entry.Flags = SampleNonKeyframe
    }
    
    // Додавання CTS якщо потрібно
    if pkt.CompositionTime != 0 {
        ctsTicks := CalculateCTSTicks(pkt.CompositionTime, timeScale)
        if ctsTicks != 0 {
            trun.Flags |= TrackRunSampleCTS
            if trun.Version > 0 {
                entry.CTS = ctsTicks  // signed для Version > 0
            } else {
                entry.CTS = int32(uint32(ctsTicks))  // unsigned для Version = 0
            }
        }
    }
    
    trun.Entries = append(trun.Entries, entry)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення fMP4 фрагменту з коректними таймінгами

```go
// CreateFragmentWithTimescales — генерація фрагменту з точними таймінгами
func CreateFragmentWithTimescales(packets []av.Packet, timeScale uint32) ([]byte, error) {
    if len(packets) == 0 {
        return nil, fmt.Errorf("no packets to fragment")
    }
    
    // 1. Розрахунок базового DTS для фрагменту
    baseDTS := timescale.ToScale(packets[0].Time, timeScale)
    
    // 2. Створення TrackFragDecodeTime (tfdt)
    tfdt := &fmp4io.TrackFragDecodeTime{
        Version: 0,  // 32-бітний час
        Time:    baseDTS,
    }
    
    // 3. Створення TrackFragRun (trun) з таймінгами
    trun := &fmp4io.TrackFragRun{
        Version: 0,
        Flags:   TrackRunSampleDuration | TrackRunSampleSize | TrackRunSampleFlags,
        Entries: make([]TrackFragRunEntry, len(packets)),
    }
    
    for i, pkt := range packets {
        entry := &trun.Entries[i]
        
        // Конвертація тривалості
        entry.Duration = uint32(timescale.ToScale(pkt.Duration, timeScale))
        entry.Size = uint32(len(pkt.Data))
        
        // Прапорці
        if pkt.IsKeyFrame {
            entry.Flags = SampleNoDependencies
        } else {
            entry.Flags = SampleNonKeyframe
        }
        
        // CTS для B-frames
        if pkt.CompositionTime != 0 {
            ctsTicks := timescale.Relative(pkt.CompositionTime, timeScale)
            if ctsTicks != 0 {
                trun.Flags |= TrackRunSampleCTS
                entry.CTS = ctsTicks
            }
        }
    }
    
    // 4. Серіалізація у fMP4 формат (спрощено)
    // ... реалізація запису moof + mdat ...
    
    return fragmentBytes, nil
}
```

### 🔧 Приклад: Синхронізація аудіо/відео через спільну шкалу часу

```go
// SyncAudioVideo — синхронізація аудіо та відео таймінгів
func SyncAudioVideo(videoPackets, audioPackets []av.Packet, 
                   videoScale, audioScale uint32) ([]SyncedPacket, error) {
    // Конвертація всіх таймінгів у спільну шкалу (напр. 90000)
    commonScale := uint32(90000)
    
    type TimedPacket struct {
        pkt  av.Packet
        dts  uint64  // у commonScale ticks
        pts  uint64  // у commonScale ticks
        isVideo bool
    }
    
    var allPackets []TimedPacket
    
    // Конвертація відео пакетів
    for _, pkt := range videoPackets {
        dts := timescale.ToScale(pkt.Time, videoScale)
        // Конвертація з videoScale → commonScale
        dtsCommon := dts * uint64(commonScale) / uint64(videoScale)
        
        pts := dts
        if pkt.CompositionTime != 0 {
            cts := timescale.Relative(pkt.CompositionTime, videoScale)
            pts = uint64(int64(dts) + int64(cts))
            ptsCommon := pts * uint64(commonScale) / uint64(videoScale)
            pts = ptsCommon
        }
        
        allPackets = append(allPackets, TimedPacket{
            pkt: pkt, dts: dtsCommon, pts: pts, isVideo: true,
        })
    }
    
    // Аналогічно для аудіо...
    
    // Сортування за DTS для коректного порядку декодування
    sort.Slice(allPackets, func(i, j int) bool {
        return allPackets[i].dts < allPackets[j].dts
    })
    
    // Групування синхронізованих пакетів
    var synced []SyncedPacket
    var currentVideo *TimedPacket
    var currentAudio *TimedPacket
    
    for _, tp := range allPackets {
        if tp.isVideo {
            currentVideo = &tp
        } else {
            currentAudio = &tp
        }
        
        if currentVideo != nil && currentAudio != nil {
            // Перевірка синхронізації: різниця PTS <= tolerance
            diff := int64(currentVideo.pts) - int64(currentAudio.pts)
            tolerance := uint64(commonScale) / 100  // 10ms tolerance
            
            if uint64(abs(diff)) <= tolerance {
                synced = append(synced, SyncedPacket{
                    Video: currentVideo.pkt,
                    Audio: currentAudio.pkt,
                    PTS:   time.Duration(currentVideo.pts) * time.Second / time.Duration(commonScale),
                })
            }
        }
    }
    
    return synced, nil
}

func abs(x int64) int64 {
    if x < 0 { return -x }
    return x
}

type SyncedPacket struct {
    Video, Audio av.Packet
    PTS          time.Duration
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Переповнення при великих duration** | Невірні ticks для довгих відео | Використовуйте `bits.Mul64/Div64` у `ToScale()` для 128-бітної арифметики |
| **Некоректна обробка від'ємного часу** | Від'ємний CTS стає великим додатним числом | Використовуйте `Relative()` для signed конвертації, не `ToScale()` |
| **Неточне округлення** | Накопичення помилок у довгих потоках | Перевірте логіку округлення: `rem >= time.Second/2` для `ToScale()` |
| **Втрата точності при конвертації шкал** | Розсинхронізація при перерахунку timeScale | Конвертуйте через спільну шкалу або використовуйте 64-бітну арифметику |
| **Переповнення int32 у Relative()** | Паніка при великих relative times | Додайте перевірку діапазону перед конвертацією у int32 |

---

## ⚡ Оптимізації для high-performance конвертації

### 1. Кешування конвертацій для частих значень:

```go
var timeScaleCache = sync.Map{}  // map[uint64]uint64 (key = duration<<32 | scale)

func CachedToScale(t time.Duration, scale uint32) uint64 {
    key := uint64(t)<<32 | uint64(scale)
    
    if cached, ok := timeScaleCache.Load(key); ok {
        return cached.(uint64)
    }
    
    result := ToScale(t, scale)
    timeScaleCache.Store(key, result)
    return result
}
```

### 2. Попередній розрахунок множників:

```go
// PrecomputedScale — попередньо розраховані множники для швидкої конвертації
type PrecomputedScale struct {
    scale      uint32
    invScale   float64  // 1.0 / scale для зворотньої конвертації
    roundHalf  uint64   // time.Second / 2 для округлення
}

func NewPrecomputedScale(scale uint32) *PrecomputedScale {
    return &PrecomputedScale{
        scale:     scale,
        invScale:  1.0 / float64(scale),
        roundHalf: uint64(time.Second / 2),
    }
}

func (p *PrecomputedScale) ToScaleFast(t time.Duration) uint64 {
    // Швидка конвертація з плаваючою крапкою (менш точна, але швидша)
    ticks := uint64(float64(t) * p.invScale + 0.5)
    return ticks
}

// Використання для non-critical таймінгів де не потрібна ідеальна точність
```

### 3. Моніторинг продуктивності конвертації:

```go
type TimeScaleMetrics struct {
    Conversions prometheus.CounterVec
    Latency     prometheus.HistogramVec
    RoundingUp  prometheus.CounterVec
    Errors      prometheus.CounterVec
}

func (m *TimeScaleMetrics) RecordConversion(scale uint32, duration time.Duration, roundedUp bool, err error) {
    m.Conversions.WithLabelValues(fmt.Sprintf("scale_%d", scale)).Inc()
    if err != nil {
        m.Errors.Inc()
    } else if roundedUp {
        m.RoundingUp.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання timescale утиліт

```go
// ✅ 1. Перевірка діапазону перед конвертацією
if t > maxDurationForScale(scale) {
    return fmt.Errorf("duration %v too large for scale %d", t, scale)
}

// ✅ 2. Використання ToScale() для абсолютного часу, Relative() для relative
dts := timescale.ToScale(pkt.Time, timeScale)           // абсолютний час
cts := timescale.Relative(pkt.CompositionTime, timeScale) // relative offset

// ✅ 3. Обробка від'ємних значень для CTS
if cts < 0 && trun.Version == 0 {
    // Version 0 trun вимагає unsigned CTS — конвертуємо або логуємо попередження
    log.Printf("warning: negative CTS %d with trun version 0", cts)
}

// ✅ 4. Округлення: перевірка логіки
// ToScale: rem >= time.Second/2 → round up
// Relative: (rel&1 != 0) == (t > 0) → round up

// ✅ 5. Логування з контекстом для дебагу
log.Printf("Converted time: %v → %d ticks @ scale %d (rounded: %v)", 
    t, ticks, scale, rem >= uint64(time.Second/2))

// ✅ 6. Метрики для моніторингу
metrics.RecordConversion(scale, t, roundedUp, err)
```

---

## 🔗 Корисні посилання

- 💻 [Go math/bits Package](https://pkg.go.dev/math/bits) — низькорівнева бітова арифметика
- 📄 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [MP4 TimeScale Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25688) — Apple documentation про timeScale
- 🧪 [Fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) — теорія точних обчислень
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди використовуйте `ToScale()` для абсолютного часу, `Relative()` для relative** — уникнення некоректної обробки від'ємних значень.
> 2. **Перевіряйте діапазон перед конвертацією** — уникнення переповнення при великих duration або scale.
> 3. **Документуйте логіку округлення** — уникнення плутанини при дебагу таймінгів.
> 4. **Використовуйте спільну шкалу для синхронізації аудіо/відео** — уникнення розсинхронізації через різні timeScale.
> 5. **Моніторьте `RoundingUp` метрику** — різке зростання може вказувати на проблеми з точністю таймінгів.

Потрібен приклад реалізації повного циклу синхронізації аудіо/відео з використанням `timescale` утиліт, або інтеграція цих функцій з вашим `mse.Muxer` для коректної конвертації таймінгів у WebSocket стрімінгу? Готовий допомогти! 🚀