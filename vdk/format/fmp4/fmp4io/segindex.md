# 📦 Глибокий розбір: `fmp4io.SegmentIndex` — Індексація сегментів для DASH/HLS streaming

Цей файл — **реалізація атому `sidx` (Segment Index)** для опису індексів сегментів у форматі Fragmented MP4 (fMP4). Цей атом критичний для adaptive bitrate streaming (DASH, HLS), дозволяючи клієнтам швидко знаходити та завантажувати потрібні сегменти без повного сканування файлу.

---

## 🗺️ Архітектурна схема SegmentIndex

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.SegmentIndex — Segment Index│
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • SegmentIndex (sidx) — індекс сегментів│
│  • SegmentReference — посилання на сегмент│
│  • FullAtom — базовий клас з version/flags│
│                                         │
│  🔄 Формат атому:                       │
│  [size:4][tag:4][FullAtom header]     │
│  [ReferenceID:4][TimeScale:4]         │
│  [EarliestPTS:4/8][FirstOffset:4/8]   │
│  [reserved:2][ReferenceCount:2]       │
│  [SegmentReference × N:12 each]       │
│                                         │
│  📡 Використання:                       │
│  • DASH MPD маніфести                   │
│  • HLS fMP4 сегменти                   │
│  • Low-latency streaming               │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. SegmentIndex (sidx) — індекс сегментів

### 🔧 Структура та призначення:

```go
type SegmentIndex struct {
    FullAtom              // базовий клас: Version, Flags, AtomPos
    ReferenceID   uint32  // ⭐ ідентифікатор посилання (зазвичай 0)
    TimeScale     uint32  // ⭐ ticks per second для таймінгів
    EarliestPTS   uint64  // ⭐ найраніший PTS у цьому індексі
    FirstOffset   uint64  // ⭐ зміщення першого сегменту відносно початку атому
    References    []SegmentReference // ⭐ масив посилань на сегменти
}
```

### 🔍 Призначення sidx атому:

```
sidx (Segment Index) містить індекси для швидкого доступу до сегментів:

• Дозволяє клієнту знайти потрібний сегмент без сканування всього файлу
• Критичний для adaptive bitrate streaming: швидке перемикання між якостями
• Підтримує low-latency streaming: доступ до останніх сегментів у реальному часі

Структура:
  sidx (SegmentIndex)
  ├─ FullAtom header: Version (0/1), Flags
  ├─ ReferenceID: ідентифікатор посилання (зазвичай 0)
  ├─ TimeScale: ticks per second для таймінгів
  ├─ EarliestPTS: найраніший PTS у цьому індексі (4 або 8 байт залежно від Version)
  ├─ FirstOffset: зміщення першого сегменту (4 або 8 байт)
  ├─ reserved: 2 байти (завжди 0)
  ├─ ReferenceCount: кількість посилань у цьому індексі
  └─ SegmentReference × N: масив посилань (12 байт кожен)
```

### 🔍 Версії та формати часу:

```
Version 0 (32-бітний час):
• EarliestPTS та FirstOffset зберігаються як uint32
• Діапазон: 0..4,294,967,295 ticks
• При timeScale=90000: ~13 годин максимальної тривалості

Version 1 (64-бітний час):
• EarliestPTS та FirstOffset зберігаються як uint64
• Діапазон: практично необмежений
• Рекомендується для довгих потоків або high-precision таймінгів

Конвертація у time.Duration:
    duration := time.Duration(sidx.EarliestPTS) * time.Second / time.Duration(sidx.TimeScale)
```

### ✅ Ваш use-case**: пошук сегменту за часом

```go
// FindSegmentByTime — пошук індексу сегменту за цільовим часом
func FindSegmentByTime(sidx *fmp4io.SegmentIndex, targetTime time.Duration) (int, error) {
    if sidx == nil || len(sidx.References) == 0 {
        return -1, fmt.Errorf("empty segment index")
    }
    
    // Конвертація цільового часу у ticks
    targetTicks := uint64(targetTime * time.Duration(sidx.TimeScale) / time.Second)
    
    // Розрахунок абсолютного часу для кожного посилання
    accumulatedTime := sidx.EarliestPTS
    for i, ref := range sidx.References {
        segmentEnd := accumulatedTime + uint64(ref.SubsegmentDuration)
        
        if targetTicks >= accumulatedTime && targetTicks < segmentEnd {
            return i, nil  // знайдено сегмент
        }
        
        accumulatedTime = segmentEnd
    }
    
    // Цільовий час після останнього сегменту — повертаємо останній
    return len(sidx.References) - 1, nil
}

// Використання для HLS/DASH streaming:
targetTime := 120 * time.Second  // хочемо шукати до 2 хвилин
segmentIndex, err := FindSegmentByTime(sidx, targetTime)
if err != nil {
    return fmt.Errorf("find segment: %w", err)
}

ref := sidx.References[segmentIndex]
log.Printf("Found segment %d: size=%d, duration=%d ticks", 
    segmentIndex, ref.ReferencedSize, ref.SubsegmentDuration)
```

---

## 🔑 2. SegmentReference — посилання на сегмент

### 🔧 Структура та призначення:

```go
type SegmentReference struct {
    ReferencesBox      bool     // ⭐ чи це посилання на інший sidx атом
    ReferencedSize     uint32   // ⭐ розмір сегменту у байтах
    SubsegmentDuration uint32   // ⭐ тривалість сегменту у ticks
    StartsWithSAP      bool     // ⭐ чи починається з точки доступу (SAP)
    SAPType            uint8    // ⭐ тип точки доступу (0-7)
    SAPDeltaTime       uint32   // ⭐ зміщення від початку сегменту до SAP
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `ReferencesBox` | `bool` | **Критично**: чи це посилання на інший sidx атом (для ієрархічних індексів) | `false` = звичайний сегмент, `true` = nested index |
| `ReferencedSize` | `uint32` | **Критично**: розмір сегменту у байтах (без заголовку атому) | `1048576` = 1 MB сегмент |
| `SubsegmentDuration` | `uint32` | **Критично**: тривалість сегменту у ticks | `90000` = 1 секунда @ 90kHz |
| `StartsWithSAP` | `bool` | **Критично**: чи починається сегмент з точки доступу (SAP = Stream Access Point) | `true` = можна почати відтворення з цього сегменту |
| `SAPType` | `uint8` | **Критично**: тип точки доступу (0-7) | `1` = IDR frame у H.264, `2` = ключовий кадр у VP9 |
| `SAPDeltaTime` | `uint32` | **Критично**: зміщення від початку сегменту до першої точки доступу | `0` = SAP на початку, `45000` = 0.5с @ 90kHz |

### 🔍 Stream Access Point (SAP) типи:

```
SAPType визначає тип точки доступу для початку відтворення:

• 0 = невизначений тип (не рекомендується)
• 1 = IDR frame у H.264/H.265 (найпоширеніший) ⭐
• 2 = ключовий кадр у VP8/VP9/AV1
• 3 = точка доступу для аудіо (всі семпли незалежні)
• 4-7 = зарезервовані для майбутніх кодеків

SAPDeltaTime:
• Зміщення у ticks від початку сегменту до першої точки доступу
• Якщо 0: сегмент починається з ключового кадру
• Якщо >0: потрібно пропустити перші семпли до досягнення SAP

Приклад для відео з B-frames:
• Сегмент може починатися з P-frame (не ключовий)
• SAPDeltaTime = 45000 (0.5с @ 90kHz)
• Клієнт повинен завантажити перші 0.5с даних, але почати відтворення з 0.5с
```

### ✅ Ваш use-case**: перевірка чи можна почати відтворення з сегменту

```go
// CanStartPlaybackFromSegment — перевірка чи сегмент підходить для початку відтворення
func CanStartPlaybackFromSegment(ref *fmp4io.SegmentReference) bool {
    // Сегмент підходить якщо:
    // 1. Він починається з точки доступу (SAP)
    // 2. Або SAPDeltaTime = 0 (SAP на початку)
    return ref.StartsWithSAP && ref.SAPDeltaTime == 0
}

// GetSAPTime — розрахунок часу першої точки доступу у сегменті
func GetSAPTime(ref *fmp4io.SegmentReference, timeScale int64) time.Duration {
    if !ref.StartsWithSAP {
        return 0  // немає точки доступу
    }
    return time.Duration(ref.SAPDeltaTime) * time.Second / time.Duration(timeScale)
}

// Використання для low-latency streaming:
for i, ref := range sidx.References {
    if CanStartPlaybackFromSegment(&ref) {
        log.Printf("Segment %d: can start playback immediately", i)
    } else if ref.StartsWithSAP {
        sapTime := GetSAPTime(&ref, int64(sidx.TimeScale))
        log.Printf("Segment %d: SAP at %.3fs", i, sapTime.Seconds())
    } else {
        log.Printf("Segment %d: no SAP, cannot start playback", i)
    }
}
```

---

## 🔑 3. Marshal/Unmarshal — серіалізація атому

### 🔧 Основна логіка Marshal:

```go
func (s SegmentIndex) Marshal(b []byte) (n int) {
    // 1. Запис заголовку FullAtom (tag + version + flags)
    n = s.FullAtom.marshalAtom(b, SIDX)
    
    // 2. Запис основних полів
    pio.PutU32BE(b[n:], s.ReferenceID); n += 4
    pio.PutU32BE(b[n:], s.TimeScale); n += 4
    
    // 3. Запис EarliestPTS та FirstOffset (4 або 8 байт залежно від Version)
    if s.Version == 0 {
        pio.PutU32BE(b[n:], uint32(s.EarliestPTS)); n += 4
        pio.PutU32BE(b[n:], uint32(s.FirstOffset)); n += 4
    } else {
        pio.PutU64BE(b[n:], s.EarliestPTS); n += 8
        pio.PutU64BE(b[n:], s.FirstOffset); n += 8
    }
    
    // 4. Запис reserved та ReferenceCount
    n += 2  // reserved (завжди 0)
    pio.PutU16BE(b[n:], uint16(len(s.References))); n += 2
    
    // 5. Запис масиву SegmentReference (12 байт кожен)
    for _, ref := range s.References {
        // ReferencedSize з бітом ReferencesBox
        v := ref.ReferencedSize
        if ref.ReferencesBox {
            v |= 1 << 31  // встановлення біту 31
        }
        pio.PutU32BE(b[n:], v); n += 4
        
        pio.PutU32BE(b[n:], ref.SubsegmentDuration); n += 4
        
        // SAP flags та SAPDeltaTime у одному uint32
        v = (uint32(ref.SAPType) << 28) | ref.SAPDeltaTime
        if ref.StartsWithSAP {
            v |= 1 << 31  // встановлення біту 31
        }
        pio.PutU32BE(b[n:], v); n += 4
    }
    
    // 6. Запис загального розміру атому на початок
    pio.PutU32BE(b, uint32(n))
    return
}
```

### 🔧 Основна логіка Unmarshal:

```go
func (s *SegmentIndex) Unmarshal(b []byte, offset int) (n int, err error) {
    // 1. Читання заголовку FullAtom
    n, err = s.FullAtom.unmarshalAtom(b, offset)
    if err != nil { return }
    
    // 2. Читання основних полів
    if len(b) < n+8 { err = parseErr("ReferenceID", n+offset, nil); return }
    s.ReferenceID = pio.U32BE(b[n:]); n += 4
    s.TimeScale = pio.U32BE(b[n:]); n += 4
    
    // 3. Читання EarliestPTS та FirstOffset (4 або 8 байт)
    if s.Version == 0 {
        if len(b) < n+8 { err = parseErr("EarliestPTS", n+offset, nil); return }
        s.EarliestPTS = uint64(pio.U32BE(b[n:])); n += 4
        s.FirstOffset = uint64(pio.U32BE(b[n:])); n += 4
    } else {
        if len(b) < n+16 { err = parseErr("EarliestPTS", n+offset, nil); return }
        s.EarliestPTS = pio.U64BE(b[n:]); n += 8
        s.FirstOffset = pio.U64BE(b[n:]); n += 8
    }
    
    // 4. Читання ReferenceCount
    if len(b) < n+4 { err = parseErr("ReferenceCount", n+offset, nil); return }
    n += 2  // пропуск reserved
    refCount := int(pio.U16BE(b[n:])); n += 2
    
    // 5. Читання масиву SegmentReference
    if len(b) < n+(12*refCount) { err = parseErr("SegmentReference", n+offset, nil); return }
    s.References = make([]SegmentReference, refCount)
    
    for i := range s.References {
        ref := &s.References[i]
        
        // ReferencedSize з бітом ReferencesBox
        refSize := pio.U32BE(b[n:]); n += 4
        if refSize&(1<<31) != 0 {
            ref.ReferencesBox = true
        }
        ref.ReferencedSize = refSize &^ ((1 << 31) - 1)  // очищення біту 31
        
        ref.SubsegmentDuration = pio.U32BE(b[n:]); n += 4
        
        // SAP flags та SAPDeltaTime у одному uint32
        sapDelta := pio.U32BE(b[n:]); n += 4
        if sapDelta&(1<<31) != 0 {
            ref.StartsWithSAP = true
        }
        ref.SAPType = uint8(0x7 & (sapDelta >> 28))  // біти 28-30
        ref.SAPDeltaTime = sapDelta &^ ((1 << 28) - 1)  // очищення біт 28-31
    }
    
    return
}
```

### ⚠️ Критична проблема: бітові операції для SAP flags

```
У поточному коді для SAP:
    v = (uint32(ref.SAPType) << 28) | ref.SAPDeltaTime
    if ref.StartsWithSAP {
        v |= 1 << 31  // встановлення біту 31
    }

Проблема:
• Біт 31 використовується для двох різних прапорців:
  - У ReferencedSize: біт 31 = ReferencesBox
  - У SAP field: біт 31 = StartsWithSAP
• Це може призвести до плутанини при читанні/записі

✅ Виправлення: чітка документація та валідація
    // Для ReferencedSize:
    //   Біт 31 = ReferencesBox (1 = nested sidx, 0 = звичайний сегмент)
    //   Біти 0-30 = ReferencedSize (максимум 2^31-1 = 2,147,483,647 байт)
    
    // Для SAP field:
    //   Біт 31 = StartsWithSAP (1 = має SAP, 0 = немає)
    //   Біти 28-30 = SAPType (0-7)
    //   Біти 0-27 = SAPDeltaTime (максимум 2^28-1 = 268,435,455 ticks)
    
    // Валідація діапазонів перед записом:
    if ref.ReferencedSize > (1<<31)-1 {
        return fmt.Errorf("ReferencedSize too large: %d", ref.ReferencedSize)
    }
    if ref.SAPDeltaTime > (1<<28)-1 {
        return fmt.Errorf("SAPDeltaTime too large: %d", ref.SAPDeltaTime)
    }
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення sidx атому для HLS сегменту

```go
// CreateSegmentIndexForHLS — генерація sidx атому для HLS fMP4 сегменту
func CreateSegmentIndexForHLS(segmentStartTime time.Duration, segmentDuration time.Duration, 
                             segmentSize uint32, hasKeyFrame bool, timeScale int64) (*fmp4io.SegmentIndex, error) {
    // Конвертація часу у ticks
    earliestPTS := uint64(segmentStartTime * time.Duration(timeScale) / time.Second)
    subsegmentDuration := uint32(segmentDuration * time.Duration(timeScale) / time.Second)
    
    // Розрахунок SAP параметрів
    var sapType uint8
    var sapDeltaTime uint32
    if hasKeyFrame {
        sapType = 1  // IDR frame для H.264
        sapDeltaTime = 0  // ключовий кадр на початку сегменту
    } else {
        // Якщо немає ключового кадру на початку — не можна почати відтворення
        // Встановлюємо StartsWithSAP = false
        sapType = 0
        sapDeltaTime = 0
    }
    
    ref := fmp4io.SegmentReference{
        ReferencesBox:      false,  // звичайний сегмент, не nested index
        ReferencedSize:     segmentSize,
        SubsegmentDuration: subsegmentDuration,
        StartsWithSAP:      hasKeyFrame,
        SAPType:            sapType,
        SAPDeltaTime:       sapDeltaTime,
    }
    
    return &fmp4io.SegmentIndex{
        FullAtom: fmp4io.FullAtom{
            Version: 0,  // 32-бітний час (достатньо для більшості випадків)
            Flags:   0,
        },
        ReferenceID: 0,  // зазвичай 0 для простих випадків
        TimeScale:   uint32(timeScale),
        EarliestPTS: earliestPTS,
        FirstOffset: 0,  // сегмент починається одразу після sidx атому
        References:  []fmp4io.SegmentReference{ref},
    }, nil
}

// Використання для генерації HLS сегменту:
sidx, err := CreateSegmentIndexForHLS(
    120*time.Second,  // початок сегменту на 2 хвилині
    4*time.Second,    // тривалість сегменту 4 секунди
    1048576,          // розмір сегменту 1 MB
    true,             // сегмент починається з ключового кадру
    90000,            // timeScale 90kHz для відео
)
if err != nil { /* handle error */ }

// Серіалізація sidx атому
sidxBytes := make([]byte, sidx.Len())
sidx.Marshal(sidxBytes)
```

### 🔧 Приклад: Парсинг sidx для adaptive bitrate streaming

```go
// ParseSegmentIndexForABR — аналіз sidx атому для adaptive bitrate логіки
func ParseSegmentIndexForABR(sidxData []byte, targetBitrate int) (*SegmentAnalysis, error) {
    var sidx fmp4io.SegmentIndex
    if _, err := sidx.Unmarshal(sidxData, 0); err != nil {
        return nil, fmt.Errorf("parse sidx: %w", err)
    }
    
    analysis := &SegmentAnalysis{
        TimeScale:   int64(sidx.TimeScale),
        EarliestPTS: sidx.EarliestPTS,
        Segments:    make([]SegmentInfo, len(sidx.References)),
    }
    
    for i, ref := range sidx.References {
        // Розрахунок бітрейту сегменту
        durationSec := float64(ref.SubsegmentDuration) / float64(sidx.TimeScale)
        bitrate := int(float64(ref.ReferencedSize) * 8 / durationSec)
        
        // Перевірка чи підходить сегмент для цільового бітрейту
        suitable := bitrate <= targetBitrate*110/100  // дозволяємо +10% відхилення
        
        analysis.Segments[i] = SegmentInfo{
            Index:          i,
            Size:           ref.ReferencedSize,
            Duration:       time.Duration(ref.SubsegmentDuration) * time.Second / time.Duration(sidx.TimeScale),
            Bitrate:        bitrate,
            HasSAP:         ref.StartsWithSAP,
            SAPType:        ref.SAPType,
            SAPTime:        time.Duration(ref.SAPDeltaTime) * time.Second / time.Duration(sidx.TimeScale),
            SuitableForABR: suitable,
        }
    }
    
    return analysis, nil
}

type SegmentAnalysis struct {
    TimeScale   int64
    EarliestPTS uint64
    Segments    []SegmentInfo
}

type SegmentInfo struct {
    Index          int
    Size           uint32
    Duration       time.Duration
    Bitrate        int
    HasSAP         bool
    SAPType        uint8
    SAPTime        time.Duration
    SuitableForABR bool
}

// Використання для adaptive bitrate логіки:
analysis, err := ParseSegmentIndexForABR(sidxBytes, 2000000)  // цільовий бітрейт 2 Mbps
if err != nil { /* handle error */ }

// Вибір найкращого сегменту для поточного бітрейту
var bestSegment *SegmentInfo
for i := range analysis.Segments {
    seg := &analysis.Segments[i]
    if seg.SuitableForABR && (bestSegment == nil || seg.Bitrate > bestSegment.Bitrate) {
        bestSegment = seg
    }
}
if bestSegment != nil {
    log.Printf("Selected segment %d: %d kbps, %v duration", 
        bestSegment.Index, bestSegment.Bitrate/1000, bestSegment.Duration)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Переповнення 32-бітного часу** | Невірні таймінги для довгих потоків | Використовуйте `Version=1` для 64-бітного часу у довгих потоках |
| **Невірне декодування бітових прапорців** | Не коректні значення ReferencedSize або SAPDeltaTime | Переконайтеся що бітові маски коректні: `&^ ((1 << 31) - 1)` для очищення бітів |
| **Непідтримка nested sidx** | Помилки при читанні ієрархічних індексів | Документуйте обмеження або реалізуйте рекурсивний парсинг для `ReferencesBox=true` |
| **Невірний розрахунок бітрейту** | Неправильний вибір сегменту у ABR | Перевіряйте ділення на нуль: `if durationSec == 0 { bitrate = 0 }` |
| **Відсутній sidx атом** | Неможливість швидкого пошуку сегментів | Генеруйте sidx для кожного fMP4 сегменту у streaming pipeline |

---

## ⚡ Оптимізації для adaptive bitrate streaming

### 1. Кешування розпарсених sidx атомів:

```go
var sidxCache = sync.Map{}  // map[string]*fmp4io.SegmentIndex

func GetCachedSegmentIndex(key string, data []byte) (*fmp4io.SegmentIndex, error) {
    if cached, ok := sidxCache.Load(key); ok {
        return cached.(*fmp4io.SegmentIndex), nil
    }
    
    var sidx fmp4io.SegmentIndex
    if _, err := sidx.Unmarshal(data, 0); err != nil {
        return nil, err
    }
    
    sidxCache.Store(key, &sidx)
    return &sidx, nil
}
```

### 2. Попередній розрахунок кумулятивних часів:

```go
// PrecomputeSegmentTimes — попередній розрахунок кумулятивних часів для швидкого пошуку
type PrecomputedSegmentIndex struct {
    *fmp4io.SegmentIndex
    CumulativeTimes []uint64  // кумулятивні ticks для кожного сегменту
}

func PrecomputeSegmentTimes(sidx *fmp4io.SegmentIndex) *PrecomputedSegmentIndex {
    p := &PrecomputedSegmentIndex{
        SegmentIndex:    sidx,
        CumulativeTimes: make([]uint64, len(sidx.References)),
    }
    
    accumulated := sidx.EarliestPTS
    for i, ref := range sidx.References {
        p.CumulativeTimes[i] = accumulated
        accumulated += uint64(ref.SubsegmentDuration)
    }
    
    return p
}

// Використання для бінарного пошуку замість лінійного
func FindSegmentFast(p *PrecomputedSegmentIndex, targetTicks uint64) int {
    // Бінарний пошук у CumulativeTimes...
    // O(log n) замість O(n)
}
```

### 3. Моніторинг продуктивності парсингу:

```go
type SIDXMetrics struct {
    IndexesParsed prometheus.CounterVec
    ParseLatency  prometheus.HistogramVec
    SegmentCount  prometheus.HistogramVec
    ParseErrors   prometheus.CounterVec
}

func (m *SIDXMetrics) RecordParse(segmentCount int, duration time.Duration, err error) {
    m.IndexesParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    m.SegmentCount.Observe(float64(segmentCount))
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання SegmentIndex

```go
// ✅ 1. Перевірка версії перед читанням часу
if sidx.Version == 0 {
    // 32-бітний час: перевірка діапазону
    if sidx.EarliestPTS > math.MaxUint32 {
        return fmt.Errorf("EarliestPTS overflow for 32-bit version")
    }
}

// ✅ 2. Валідація бітових прапорців при читанні
refSize := pio.U32BE(b[n:])
if refSize&(1<<31) != 0 {
    ref.ReferencesBox = true
}
ref.ReferencedSize = refSize &^ ((1 << 31) - 1)  // очищення біту 31

// ✅ 3. Перевірка діапазонів перед записом
if ref.ReferencedSize > (1<<31)-1 {
    return fmt.Errorf("ReferencedSize too large: %d", ref.ReferencedSize)
}
if ref.SAPDeltaTime > (1<<28)-1 {
    return fmt.Errorf("SAPDeltaTime too large: %d", ref.SAPDeltaTime)
}

// ✅ 4. Обробка nested sidx (ReferencesBox=true)
if ref.ReferencesBox {
    // Рекурсивний парсинг nested sidx атому
    nestedSidxData := data[offset+ref.ReferencedSize : offset+ref.ReferencedSize+refSize]
    // ... парсинг nestedSidxData ...
}

// ✅ 5. Логування з контекстом для дебагу
log.Printf("Parsed sidx: version=%d, timeScale=%d, references=%d, earliestPTS=%d", 
    sidx.Version, sidx.TimeScale, len(sidx.References), sidx.EarliestPTS)

// ✅ 6. Метрики для моніторингу
metrics.RecordParse(len(sidx.References), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 23009-1:2022 (DASH)](https://www.iso.org/standard/79329.html) — офіційний стандарт для adaptive streaming
- 📄 [HLS fMP4 Specification](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation про fMP4 у HLS
- 📄 [Segment Index Atom Format](https://wiki.multimedia.cx/index.php/MP4#sidx) — детальний опис sidx атому
- 🧪 [Bitwise operations in Go](https://go.dev/ref/spec#Operators) — бітові операції у Go
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Використовуйте `Version=1` для довгих потоків** — уникнення переповнення 32-бітного часу.
> 2. **Валідуйте бітові маски при читанні/записі** — уникнення невірних значень ReferencedSize та SAPDeltaTime.
> 3. **Документуйте обмеження nested sidx** — уникнення плутанини при роботі з ієрархічними індексами.
> 4. **Перевіряйте ділення на нуль при розрахунку бітрейту** — уникнення панік у adaptive bitrate логіці.
> 5. **Генеруйте sidx для кожного fMP4 сегменту** — забезпечення швидкого пошуку у streaming pipeline.

Потрібен приклад реалізації повного циклу створення/парсингу sidx атому для HLS/DASH streaming, або інтеграція `fmp4io.SegmentIndex` з вашим `mse.Muxer` для adaptive bitrate стрімінгу через WebSocket? Готовий допомогти! 🚀