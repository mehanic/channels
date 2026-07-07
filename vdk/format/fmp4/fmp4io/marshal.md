# 📦 Глибокий розбір: `fmp4io` — Утиліти для роботи з часом та фіксованою крапкою

Цей файл — **допоміжні функції для конвертації часу та чисел з фіксованою крапкою** у форматі MP4/fMP4. Він надає механізми для роботи з часовими мітками у специфічній епосі 1904 року та форматом фіксованої крапки 16.16, що є стандартом для контейнерів MP4.

---

## 🗺️ Архітектурна схема утиліт

```
┌────────────────────────────────────────┐
│ 📦 fmp4io — Time & Fixed-Point Utils  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • GetTime32/PutTime32 — 32-бітний час │
│  • GetTime64/PutTime64 — 64-бітний час │
│  • Фіксована крапка 16.16/8.8          │
│  • Епоха 1904-01-01 (MP4 standard)     │
│                                         │
│  🔄 Формати часу:                       │
│  • 32-бітний: секунди від 1904-01-01  │
│  • 64-бітний: те саме, але більший діапазон│
│                                         │
│  📡 Використання:                       │
│  • Метадані відео/аудіо у MP4         │
│  • Синхронізація таймінгів у fMP4     │
│  • Конвертація між time.Time та ticks │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Час у форматі MP4: епоха 1904 року

### 🔧 Функції роботи з 32-бітним часом:

```go
func GetTime32(b []byte) (t time.Time) {
    sec := pio.U32BE(b)  // читання 4 байт у big-endian форматі
    if sec != 0 {
        t = time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC)
        t = t.Add(time.Second * time.Duration(sec))
    }
    return
}

func PutTime32(b []byte, t time.Time) {
    var sec uint32
    if !t.IsZero() {
        dur := t.Sub(time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC))
        sec = uint32(dur / time.Second)
    }
    pio.PutU32BE(b, sec)  // запис у big-endian
}
```

### 🔧 Функції роботи з 64-бітним часом:

```go
func GetTime64(b []byte) (t time.Time) {
    sec := pio.U64BE(b)  // читання 8 байт у big-endian
    if sec != 0 {
        t = time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC)
        t = t.Add(time.Second * time.Duration(sec))
    }
    return
}

func PutTime64(b []byte, t time.Time) {
    var sec uint64
    if !t.IsZero() {
        dur := t.Sub(time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC))
        sec = uint64(dur / time.Second)
    }
    pio.PutU64BE(b, sec)
}
```

### 🔍 Чому епоха 1904 року?

```
MP4 використовує 1904-01-01 як епоху (на відміну від Unix 1970-01-01):

Історична причина:
• Сумісність з QuickTime (Apple, 1990-ті)
• 1904 рік був обраний як "безпечна" дата до ери комп'ютерів

Діапазони:
• 32-бітний час: 1904..2040 роки (2^32 секунд ≈ 136 років)
• 64-бітний час: практично необмежений (2^64 секунд ≈ 585 мільярдів років)

⚠️ Увага: при конвертації з/у time.Time потрібно враховувати цю різницю!
```

### ⚠️ Критична проблема: переповнення при конвертації

```
Проблема у PutTime32/PutTime64:
    dur := t.Sub(time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC))
    sec = uint32(dur / time.Second)  // ← dur/time.Second може переповнити!

Приклад:
    t = 2100-01-01  // дата за межами 32-бітного діапазону
    dur = ~6.2e9 секунд
    dur/time.Second = 6.2e9 (fits in int64)
    uint32(6.2e9) = 1904967296 (переповнення! максимум uint32 = 4.29e9)

Наслідки:
• Невірні часові мітки для дат після ~2040 року
• Неможливість коректної обробки довгих відеоархівів

✅ Виправлення: перевірка діапазону перед конвертацією
    func PutTime32Safe(b []byte, t time.Time) error {
        if t.IsZero() {
            pio.PutU32BE(b, 0)
            return nil
        }
        
        epoch := time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
        dur := t.Sub(epoch)
        
        // Перевірка чи значення поміщається у uint32
        sec := dur / time.Second
        if sec < 0 || sec > math.MaxUint32*time.Second {
            return fmt.Errorf("time %v out of 32-bit range", t)
        }
        
        pio.PutU32BE(b, uint32(sec/time.Second))
        return nil
    }
```

### ✅ Ваш use-case**: конвертація часу для метаданих

```go
// ConvertMP4TimeToUnix — конвертація часу з MP4 epoch у Unix epoch
func ConvertMP4TimeToUnix(mp4Time time.Time) int64 {
    if mp4Time.IsZero() {
        return 0
    }
    
    // Різниця між епохами: 1904-01-01 та 1970-01-01 = 66 років
    epochDiff := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC).Sub(
                 time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC))
    
    return mp4Time.Add(-epochDiff).Unix()
}

// ConvertUnixToMP4Time — зворотна конвертація
func ConvertUnixToMP4Time(unixTime int64) time.Time {
    if unixTime == 0 {
        return time.Time{}
    }
    
    epochDiff := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC).Sub(
                 time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC))
    
    return time.Unix(unixTime, 0).Add(epochDiff)
}

// Використання для метаданих:
movieHeader := moov.Header
creationTime := fmp4io.GetTime64(movieHeader.CreationTime[:])
unixTime := ConvertMP4TimeToUnix(creationTime)
log.Printf("File created at Unix time: %d (%v)", unixTime, creationTime)
```

---

## 🔑 2. Фіксована крапка (Fixed-point arithmetic)

### 🔧 Формат 8.8 (16 біт):

```go
func PutFixed16(b []byte, f float64) {
    intpart, fracpart := math.Modf(f)  // розділення на цілу та дробову частини
    b[0] = uint8(intpart)              // ціла частина (8 біт)
    b[1] = uint8(fracpart * 256.0)     // дробова частина * 2^8 (8 біт)
}

func GetFixed16(b []byte) float64 {
    return float64(b[0]) + float64(b[1])/256.0  // ціла + дробова/256
}
```

### 🔧 Формат 16.16 (32 біти):

```go
func PutFixed32(b []byte, f float64) {
    intpart, fracpart := math.Modf(f)
    pio.PutU16BE(b[0:2], uint16(intpart))           // ціла частина (16 біт)
    pio.PutU16BE(b[2:4], uint16(fracpart*65536.0))  // дробова * 2^16 (16 біт)
}

func GetFixed32(b []byte) float64 {
    return float64(pio.U16BE(b[0:2])) + float64(pio.U16BE(b[2:4]))/65536.0
}
```

### 🔍 Де використовується фіксована крапка:

```
У MP4/fMP4 форматі фіксована крапка використовується для:

• Масштабування відео (track width/height у матриці трансформації)
• Гучність аудіо (volume у track header)
• Швидкість відтворення (preferred rate у movie header)
• Співвідношення пікселів (PixelAspect ratio)

Приклади:
• Масштаб 1.5 = 0x00018000 (ціла=1, дробова=0.5*65536=32768)
• Гучність 0.75 = 0x0000C000 (ціла=0, дробова=0.75*65536=49152)
• DPI 72.0 = 0x00480000 (ціла=72, дробова=0)
```

### ⚠️ Критична проблема: втрата точності при конвертації

```
Проблема у конвертації фіксованої крапки:
    • Дробова частина обмежена 8 або 16 бітами
    • При конвертації float64 → fixed → float64 можлива втрата точності

Приклад для 16.16:
    original = 1.123456789
    fixed = 1 + (0.123456789 * 65536) / 65536
          = 1 + 8087/65536
          = 1.123458862  // втрата точності у 7-му знаку після коми

✅ Виправлення: документація обмежень та валідація діапазону
    // PutFixed32WithValidation — безпечна конвертація з перевіркою
    func PutFixed32WithValidation(b []byte, f float64) error {
        // Перевірка діапазону: ціла частина має поміститися у 16 біт
        intpart, fracpart := math.Modf(f)
        if intpart < -32768 || intpart > 32767 {
            return fmt.Errorf("fixed-point value %f out of 16.16 range", f)
        }
        
        // Перевірка дробової частини
        if fracpart < 0 || fracpart >= 1 {
            return fmt.Errorf("invalid fractional part: %f", fracpart)
        }
        
        PutFixed32(b, f)
        return nil
    }
```

### ✅ Ваш use-case**: розрахунок співвідношення пікселів

```go
// CalculatePixelAspectRatio — розрахунок PAR з fixed-point значень
func CalculatePixelAspectRatio(hSpacing, vSpacing uint32) float64 {
    if vSpacing == 0 {
        return 1.0  // уникнення ділення на нуль
    }
    
    // Конвертація з 16.16 fixed-point у float64
    h := float64(hSpacing) / 65536.0
    v := float64(vSpacing) / 65536.0
    
    return h / v
}

// Використання з PixelAspect атомом:
pasp := findPixelAspect(track)  // helper function
if pasp != nil {
    par := CalculatePixelAspectRatio(pasp.HorizontalSpacing, pasp.VerticalSpacing)
    dar := (float64(width) / float64(height)) * par
    log.Printf("Video: %dx%d, PAR=%.3f, DAR=%.3f", width, height, par, dar)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Конвертація метаданих часу для HLS

```go
// ConvertMovieHeaderTimes — конвертація часових міток для HLS маніфесту
func ConvertMovieHeaderTimes(header *fmp4io.MovieHeader) (*HLSTimes, error) {
    if header == nil {
        return nil, fmt.Errorf("nil movie header")
    }
    
    // Конвертація часу створення
    creationTime := fmp4io.GetTime64(header.CreationTime[:])
    creationUnix := ConvertMP4TimeToUnix(creationTime)
    
    // Конвертація тривалості
    duration := time.Duration(header.Duration) * time.Second / time.Duration(header.TimeScale)
    
    // Конвертація preferred rate (fixed-point 16.16)
    preferredRate := fmp4io.GetFixed32(header.PreferredRate[:])
    
    return &HLSTimes{
        CreationUnix:   creationUnix,
        Duration:       duration,
        TimeScale:      int64(header.TimeScale),
        PreferredRate:  preferredRate,
    }, nil
}

type HLSTimes struct {
    CreationUnix  int64
    Duration      time.Duration
    TimeScale     int64
    PreferredRate float64
}

// Використання для генерації HLS маніфесту:
times, err := ConvertMovieHeaderTimes(moov.Header)
if err != nil { /* handle error */ }

manifest := fmt.Sprintf(`#EXTM3U
#EXT-X-VERSION:7
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-TARGETDURATION:%d
#EXTINF:%.3f,
segment_00001.mp4
#EXT-X-ENDLIST`, 
    int(times.Duration.Seconds())+1,  // target duration
    times.Duration.Seconds(),          // last segment duration
)
```

### 🔧 Приклад: Валідація часових міток у фрагментах

```go
// ValidateFragmentTimes — перевірка коректності таймінгів у fMP4 фрагменті
func ValidateFragmentTimes(moof *fmp4io.MovieFrag, timeScale int64) error {
    if moof == nil || len(moof.Tracks) == 0 {
        return fmt.Errorf("invalid fragment: no tracks")
    }
    
    track := moof.Tracks[0]
    
    // 1. Перевірка tfdt (базовий час декодування)
    if track.DecodeTime != nil {
        tfdt := track.DecodeTime
        if tfdt.Version == 0 && tfdt.Time > math.MaxUint32 {
            return fmt.Errorf("32-bit time overflow in tfdt: %d", tfdt.Time)
        }
        
        // Перевірка розумності часу (не в майбутньому на >1 день)
        baseTime := time.Duration(tfdt.Time) * time.Second / time.Duration(timeScale)
        if baseTime > 24*time.Hour {
            log.Printf("warning: unusually large base time: %v", baseTime)
        }
    }
    
    // 2. Перевірка trun (таблиця семплів)
    if track.Run != nil {
        trun := track.Run
        
        // Перевірка DataOffset
        if trun.Flags&fmp4io.TrackRunDataOffset != 0 {
            if trun.DataOffset > 1<<30 {  // 1GB ліміт
                return fmt.Errorf("suspicious DataOffset: %d", trun.DataOffset)
            }
        }
        
        // Перевірка CTS для B-frames
        if trun.Flags&fmp4io.TrackRunSampleCTS != 0 {
            for i, entry := range trun.Entries {
                if entry.CTS < -10000 || entry.CTS > 10000 {  // ±10 секунд @ 1000Hz
                    log.Printf("warning: unusual CTS at sample %d: %d", i, entry.CTS)
                }
            }
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Переповнення 32-бітного часу** | Невірні дати для файлів після 2040 року | Використовуйте 64-бітний час (Version=1) для довгих потоків |
| **Втрата точності фіксованої крапки** | Невірні значення масштабу/гучності | Документуйте обмеження точності; використовуйте float де можливо |
| **Невірне ділення на time.Second** | Помилки при конвертації duration | Переконайтеся що timeScale коректний та не нульовий |
| **Паніка при доступі до буфера** | Доступ за межами масиву у Put/Get функціях | Додайте перевірку `if len(b) < 4` перед читанням/записом |
| **Некоректна обробка zero time** | Час 1904-01-01 замість нульового значення | Перевіряйте `if sec != 0` перед додаванням до епохи |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування конвертацій часу:

```go
var timeCache = sync.Map{}  // map[time.Time]uint64

func CachedPutTime64(t time.Time) uint64 {
    if t.IsZero() {
        return 0
    }
    
    if cached, ok := timeCache.Load(t); ok {
        return cached.(uint64)
    }
    
    epoch := time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
    sec := uint64(t.Sub(epoch) / time.Second)
    
    timeCache.Store(t, sec)
    return sec
}
```

### 2. Попередній розрахунок констант:

```go
// Precomputed epoch difference for Unix conversion
const mp4EpochOffset = 2082844800  // seconds between 1904-01-01 and 1970-01-01

func FastConvertMP4ToUnix(mp4Sec uint64) int64 {
    return int64(mp4Sec) - mp4EpochOffset
}

func FastConvertUnixToMP4(unixSec int64) uint64 {
    return uint64(unixSec + mp4EpochOffset)
}
```

### 3. Моніторинг продуктивності конвертацій:

```go
type TimeConversionMetrics struct {
    Conversions prometheus.CounterVec
    Latency     prometheus.HistogramVec
    Errors      prometheus.CounterVec
}

func (m *TimeConversionMetrics) RecordConversion(duration time.Duration, err error) {
    m.Conversions.Inc()
    m.Latency.Observe(duration.Seconds())
    if err != nil {
        m.Errors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання часових утиліт

```go
// ✅ 1. Перевірка меж буфера перед доступом
if len(b) < 4 {
    return fmt.Errorf("buffer too short for 32-bit time")
}
sec := pio.U32BE(b)

// ✅ 2. Валідація діапазону перед конвертацією
if sec > math.MaxInt64/time.Second.Nanoseconds() {
    return fmt.Errorf("time value %d out of range", sec)
}

// ✅ 3. Обробка нульових значень часу
if sec == 0 {
    return time.Time{}  // або обробити як special case
}

// ✅ 4. Перевірка діапазону для фіксованої крапки
if f < -32768 || f > 32767 {
    return fmt.Errorf("fixed-point value %f out of 16.16 range", f)
}

// ✅ 5. Уникнення ділення на нуль при конвертації duration
if timeScale == 0 {
    return fmt.Errorf("invalid timeScale: cannot be zero")
}
duration := time.Duration(ticks) * time.Second / time.Duration(timeScale)

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Converted time: MP4=%v, Unix=%d, duration=%v", 
    mp4Time, unixTime, duration)

// ✅ 7. Метрики для моніторингу
metrics.RecordConversion(time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 📄 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [QuickTime File Format Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html) — Apple documentation про епоху 1904
- 🧪 [Fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) — теорія формату 16.16
- 💻 [Go time Package Documentation](https://pkg.go.dev/time) — робота з часом у Go
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед `pio.U32BE/U64BE`** — уникнення панік при пошкоджених файлах.
> 2. **Використовуйте 64-бітний час для довгих потоків** — уникнення переповнення для дат після 2040 року.
> 3. **Документуйте обмеження точності фіксованої крапки** — уникнення невірних очікувань щодо точності обчислень.
> 4. **Перевіряйте `timeScale != 0` перед діленням** — уникнення панік при некоректних метаданих.
> 5. **Кешуйте часті конвертації часу** — прискорення обробки великих кількостей метаданих.

Потрібен приклад реалізації повного циклу конвертації метаданих часу для HLS/DASH маніфестів, або інтеграція цих утиліт з вашим `mse.Muxer` для коректної синхронізації таймінгів у streaming? Готовий допомогти! 🚀