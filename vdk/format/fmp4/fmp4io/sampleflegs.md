# 📦 Глибокий розбір: `fmp4io.SampleFlags` — Прапорці семплів для fMP4

Цей файл — **визначення бітових прапорців для медіа-семплів** у форматі Fragmented MP4 (fMP4). Ці прапорці використовуються у атомах `trun` (Track Run) та `tfhd` (Track Fragment Header) для опису властивостей окремих семплів, таких як ключові кадри, залежності між семплами, тощо.

---

## 🗺️ Архітектурна схема SampleFlags

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.SampleFlags — Sample Flags  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • SampleFlags type — 32-бітна маска  │
│  • Прапорці синхронізації (sync)      │
│  • Прапорці залежностей (dependencies)│
│  • Комбіновані константи               │
│                                         │
│  🔄 Використання у атомах:              │
│  • trun (TrackFragRun) — таблиця семплів│
│  • tfhd (TrackFragHeader) — default прапорці│
│                                         │
│  📡 Призначення:                        │
│  • Визначення ключових кадрів (seek)  │
│  • Оптимізація декодування (залежності)│
│  • Підтримка low-latency streaming    │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. SampleFlags — бітова маска властивостей семплу

### 🔧 Тип та призначення:

```go
type SampleFlags uint32  // 32-бітна бітова маска
```

### 🔍 Бітова структура прапорців:

```
Бітова маска (спрощено, лише важливі біти):

  Біт   Значення        Назва                 Призначення
  ───────────────────────────────────────────────────────
  16    0x00010000      SampleIsNonSync      ⭐ Не ключовий кадр
  24    0x01000000      SampleHasDependencies Семпл залежить від інших
  25    0x02000000      SampleNoDependencies  Семпл незалежний

Решта бітів: зарезервовані або використовуються для розширених функцій
```

### ✅ Ваш use-case**: перевірка властивостей семплу

```go
// IsKeyFrame — перевірка чи семпл є ключовим кадром
func IsKeyFrame(flags fmp4io.SampleFlags) bool {
    // Ключовий кадр = НЕ SampleIsNonSync
    return (flags & fmp4io.SampleIsNonSync) == 0
}

// HasDependencies — перевірка наявності залежностей
func HasDependencies(flags fmp4io.SampleFlags) bool {
    return (flags & fmp4io.SampleHasDependencies) != 0
}

// IsIndependent — перевірка чи семпл незалежний
func IsIndependent(flags fmp4io.SampleFlags) bool {
    return (flags & fmp4io.SampleNoDependencies) != 0
}

// Використання у демуксері:
for i, entry := range trun.Entries {
    sampleFlags := entry.Flags
    if trun.Flags&fmp4io.TrackRunFirstSampleFlags != 0 && i == 0 {
        sampleFlags = trun.FirstSampleFlags
    }
    
    if IsKeyFrame(sampleFlags) {
        // Обробка ключового кадру (можна почати декодування)
        log.Printf("Key frame at sample %d", i)
    }
    
    if HasDependencies(sampleFlags) {
        // Семпл залежить від попередніх — потрібен буфер референсів
        log.Printf("Dependent frame at sample %d", i)
    }
}
```

---

## 🔑 2. Константи прапорців — детальний розбір

### 🔧 SampleIsNonSync (0x00010000) — не ключовий кадр:

```
Призначення: Вказує чи семпл є "sync sample" (ключовим кадром)

• Якщо ПРАПОРЕЦЬ ВСТАНОВЛЕНО (1): 
  - Семпл НЕ є ключовим кадром (P-frame, B-frame у відео)
  - Семпл залежить від попередніх кадрів для декодування
  - Не можна почати декодування з цього семплу без референсів

• Якщо ПРАПОРЕЦЬ НЕ ВСТАНОВЛЕНО (0):
  - Семпл Є ключовим кадром (I-frame, IDR у H.264/H.265)
  - Семпл незалежний — можна почати декодування з цього місця
  - Критично для seek та low-latency streaming

Приклади для відео:
• H.264 IDR frame: SampleIsNonSync = 0 (ключовий)
• H.264 P-frame: SampleIsNonSync = 1 (не ключовий)
• H.264 B-frame: SampleIsNonSync = 1 (не ключовий)

Для аудіо (AAC/Opus):
• Аудіо семпли зазвичай незалежні
• SampleIsNonSync = 0 (всі семпли "ключові" для аудіо)
```

### 🔧 SampleHasDependencies (0x01000000) та SampleNoDependencies (0x02000000):

```
Ці два прапорці визначають залежності семплу:

• SampleHasDependencies (біт 24):
  - Семпл ЗАЛЕЖИТЬ від інших семплів для декодування
  - Типово для P/B-frames у відео
  - Декодер потребує буфер референсів

• SampleNoDependencies (біт 25):
  - Семпл НЕ ЗАЛЕЖИТЬ від інших семплів
  - Типово для I-frames/IDR та аудіо семплів
  - Можна декодувати незалежно

⚠️ Важливо: Ці прапорці НЕ є взаємовиключними!
• Теоретично можна встановити обидва (хоча це рідко має сенс)
• Практично: використовуйте один з них

Приклади:
• I-frame: SampleNoDependencies = 1, SampleHasDependencies = 0
• P-frame: SampleHasDependencies = 1, SampleNoDependencies = 0
• Аудіо: зазвичай SampleNoDependencies = 1
```

### 🔧 SampleNonKeyframe (комбінована константа):

```go
SampleNonKeyframe = SampleHasDependencies | SampleIsNonSync
// = 0x01000000 | 0x00010000 = 0x01010000
```

### 🔍 Призначення:

```
Зручна константа для не-ключових залежних кадрів:

• Еквівалентно: "не ключовий кадр" + "залежить від інших"
• Типове значення для P-frames у відео
• Спрощує код: замість ручної комбінації прапорців

Використання:
    if !sample.IsKeyFrame {
        entry.Flags = SampleNonKeyframe  // зручніше ніж |
    }
```

---

## 🔑 3. Використання у TrackFragRun (trun) атомі

### 🔧 Контекст використання прапорців:

```
SampleFlags використовується у двох місцях:

1. TrackFragHeader (tfhd) — DefaultSampleFlags:
   • Задає прапорці за замовчуванням для всіх семплів у фрагменті
   • Економить місце якщо всі семпли мають однакові властивості
   • Прапорець у tfhd: TrackFragDefaultFlags (0x20)

2. TrackFragRun (trun) — SampleFlags у Entries:
   • Прапорці для окремого семплу (якщо встановлено TrackRunSampleFlags)
   • Перевизначає DefaultSampleFlags для конкретного семплу
   • FirstSampleFlags — спеціальні прапорці для першого семплу

Приоритет визначення прапорців для семплу:
  1. FirstSampleFlags (якщо i==0 і TrackRunFirstSampleFlags встановлено)
  2. Entry.Flags (якщо TrackRunSampleFlags встановлено)  
  3. tfhd.DefaultSampleFlags (fallback)
```

### ✅ Ваш use-case**: генерація прапорців для відео семплів

```go
// GenerateVideoSampleFlags — генерація SampleFlags для відео семплу
func GenerateVideoSampleFlags(isKeyFrame bool) fmp4io.SampleFlags {
    if isKeyFrame {
        // Ключовий кадр: незалежний, sync sample
        return fmp4io.SampleNoDependencies  // 0x02000000
    } else {
        // Не ключовий кадр: залежний, non-sync
        return fmp4io.SampleNonKeyframe     // 0x01010000
    }
}

// Використання при створенні trun Entries:
for i, sample := range samples {
    entry := &trun.Entries[i]
    
    // Встановлення прапорців якщо потрібно
    if trun.Flags&fmp4io.TrackRunSampleFlags != 0 {
        entry.Flags = GenerateVideoSampleFlags(sample.IsKeyFrame)
    }
    
    // Інші поля (duration, size, CTS)...
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення fMP4 фрагменту з коректними прапорцями

```go
// CreateVideoFragmentWithFlags — генерація фрагменту з прапорцями семплів
func CreateVideoFragmentWithFlags(seqnum uint32, samples []VideoSample) ([]byte, error) {
    // 1. Створення MovieFragHeader
    mfhd := &fmp4io.MovieFragHeader{
        Seqnum: seqnum,
    }
    
    // 2. Створення TrackFrag з прапорцями
    traf := &fmp4io.TrackFrag{
        Header: &fmp4io.TrackFragHeader{
            TrackID:      1,
            DefaultFlags: fmp4io.SampleNonKeyframe,  // за замовчуванням не ключові
            Flags:        fmp4io.TrackFragDefaultFlags,
        },
        DecodeTime: &fmp4io.TrackFragDecodeTime{
            Time: uint64(samples[0].DTS),  // базовий DTS
        },
        Run: &fmp4io.TrackFragRun{
            Flags: fmp4io.TrackRunSampleDuration | fmp4io.TrackRunSampleSize | fmp4io.TrackRunSampleFlags,
            Entries: make([]fmp4io.TrackFragRunEntry, len(samples)),
        },
    }
    
    // 3. Заповнення Entries з прапорцями
    trun := traf.Run
    for i, sample := range samples {
        entry := &trun.Entries[i]
        entry.Duration = uint32(sample.Duration)
        entry.Size = uint32(len(sample.Data))
        
        // Генерація прапорців для цього семплу
        if sample.IsKeyFrame {
            entry.Flags = fmp4io.SampleNoDependencies  // 0x02000000
        } else {
            entry.Flags = fmp4io.SampleNonKeyframe     // 0x01010000
        }
        
        // CTS для B-frames (якщо потрібно)
        if sample.CTS != 0 {
            trun.Flags |= fmp4io.TrackRunSampleCTS
            entry.CTS = int32(sample.CTS)
        }
    }
    
    // 4. Створення повного moof атому
    moof := &fmp4io.MovieFrag{
        Header: mfhd,
        Tracks: []*fmp4io.TrackFrag{traf},
    }
    
    // 5. Серіалізація
    moofBytes := make([]byte, moof.Len())
    moof.Marshal(moofBytes)
    
    return moofBytes, nil
}

type VideoSample struct {
    Data       []byte
    DTS        int64
    Duration   int64
    CTS        int64      // composition time offset (для B-frames)
    IsKeyFrame bool
}
```

### 🔧 Приклад: Фільтрація ключових кадрів для seek

```go
// FindKeyFrames — пошук індексів ключових кадрів у фрагменті
func FindKeyFrames(trun *fmp4io.TrackFragRun, tfhd *fmp4io.TrackFragHeader) []int {
    var keyFrameIndices []int
    
    for i := 0; i < len(trun.Entries); i++ {
        // Визначення прапорців для цього семплу
        var sampleFlags fmp4io.SampleFlags
        
        if i == 0 && (trun.Flags&fmp4io.TrackRunFirstSampleFlags != 0) {
            sampleFlags = trun.FirstSampleFlags
        } else if trun.Flags&fmp4io.TrackRunSampleFlags != 0 {
            sampleFlags = trun.Entries[i].Flags
        } else if tfhd != nil && (tfhd.Flags&fmp4io.TrackFragDefaultFlags != 0) {
            sampleFlags = tfhd.DefaultFlags
        } else {
            // За замовчуванням припускаємо ключовий кадр
            sampleFlags = fmp4io.SampleNoDependencies
        }
        
        if IsKeyFrame(sampleFlags) {
            keyFrameIndices = append(keyFrameIndices, i)
        }
    }
    
    return keyFrameIndices
}

// Використання для реалізації seek:
keyFrames := FindKeyFrames(trun, tfhd)
if len(keyFrames) > 0 {
    // Знаходження найближчого ключового кадру до цільового часу
    targetIndex := findNearestKeyFrame(keyFrames, targetTime)
    log.Printf("Seek to key frame at index %d", targetIndex)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Невірне визначення ключового кадру** | Seek не працює, помилки декодування | Переконайтеся що `SampleIsNonSync` коректно встановлено: 0 для ключових, 1 для не-ключових |
| **Конфлікт прапорців залежності** | Неможливість декодування через неправильні залежності | Використовуйте `SampleHasDependencies` для залежних кадрів, `SampleNoDependencies` для незалежних |
| **Неврахування FirstSampleFlags** | Перший семпл має невірні прапорці | Перевіряйте `TrackRunFirstSampleFlags` прапорець у trun перед використанням `FirstSampleFlags` |
| **Відсутність DefaultSampleFlags** | Всі семпли отримують неправильні прапорці за замовчуванням | Встановлюйте `tfhd.DefaultFlags` якщо більшість семплів мають однакові властивості |
| **Некоректна комбінація прапорців** | Неможливість інтерпретації властивостей семплу | Використовуйте готові константи (`SampleNonKeyframe`) замість ручної комбінації |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування результатів перевірки прапорців:

```go
var flagCache = sync.Map{}  // map[fmp4io.SampleFlags]FlagAnalysis

type FlagAnalysis struct {
    IsKeyFrame       bool
    HasDependencies bool
    IsIndependent   bool
}

func AnalyzeFlagsCached(flags fmp4io.SampleFlags) FlagAnalysis {
    if cached, ok := flagCache.Load(flags); ok {
        return cached.(FlagAnalysis)
    }
    
    analysis := FlagAnalysis{
        IsKeyFrame:       IsKeyFrame(flags),
        HasDependencies: HasDependencies(flags),
        IsIndependent:   (flags&fmp4io.SampleHasDependencies) == 0 && (flags&fmp4io.SampleIsNonSync) == 0,
    }
    
    flagCache.Store(flags, analysis)
    return analysis
}
```

### 2. Попередній розрахунок комбінованих прапорців:

```go
// Precomputed flag combinations for common cases
const (
    // Video key frame (I-frame/IDR)
    VideoKeyFrameFlags = fmp4io.SampleNoDependencies  // 0x02000000
    
    // Video dependent frame (P-frame)
    VideoDependentFlags = fmp4io.SampleNonKeyframe  // 0x01010000
    
    // Audio frame (no concept of key frames)
    AudioFrameFlags = fmp4io.SampleNoDependencies  // 0x02000000 or 0x00000000
)

// Використання:
if sample.IsKeyFrame {
    entry.Flags = VideoKeyFrameFlags
} else {
    entry.Flags = VideoDependentFlags
}
```

### 3. Моніторинг продуктивності обробки прапорців:

```go
type FlagMetrics struct {
    FlagsProcessed prometheus.CounterVec
    KeyFrameRatio  prometheus.GaugeVec
    DependencyRatio prometheus.GaugeVec
}

func (m *FlagMetrics) RecordFlags(keyFrameCount, totalCount int, streamID string) {
    m.FlagsProcessed.WithLabelValues(streamID).Add(float64(totalCount))
    if totalCount > 0 {
        m.KeyFrameRatio.WithLabelValues(streamID).Set(float64(keyFrameCount) / float64(totalCount))
    }
}
```

---

## 📋 Чек-лист безпечного використання SampleFlags

```go
// ✅ 1. Коректне визначення ключового кадру
func IsKeyFrame(flags fmp4io.SampleFlags) bool {
    return (flags & fmp4io.SampleIsNonSync) == 0  // NOT SampleIsNonSync
}

// ✅ 2. Перевірка залежностей перед декодуванням
if HasDependencies(flags) {
    // Потрібен буфер референсів для декодування
    decoder.WaitForReferences()
}

// ✅ 3. Використання готових комбінацій прапорців
// Замість ручної комбінації:
//   flags := fmp4io.SampleHasDependencies | fmp4io.SampleIsNonSync
// Використовуйте готову константу:
   flags := fmp4io.SampleNonKeyframe

// ✅ 4. Обробка FirstSampleFlags у trun
if i == 0 && (trun.Flags&fmp4io.TrackRunFirstSampleFlags != 0) {
    sampleFlags = trun.FirstSampleFlags
} else if trun.Flags&fmp4io.TrackRunSampleFlags != 0 {
    sampleFlags = entry.Flags
} else {
    sampleFlags = tfhd.DefaultFlags  // fallback
}

// ✅ 5. Валідація прапорців перед записом
if (flags & (fmp4io.SampleHasDependencies | fmp4io.SampleNoDependencies)) == 
   (fmp4io.SampleHasDependencies | fmp4io.SampleNoDependencies) {
    log.Printf("warning: conflicting dependency flags: 0x%X", flags)
}

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Sample %d: flags=0x%X, key=%v, deps=%v", 
    i, flags, IsKeyFrame(flags), HasDependencies(flags))

// ✅ 7. Метрики для моніторингу
metrics.RecordFlags(keyFrameCount, totalCount, streamID)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [MP4 Sample Flags Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25688) — Apple documentation про прапорці семплів
- 📄 [H.264/AVC NALU Types](https://wiki.videolan.org/NAL/) — типи кадрів у H.264
- 🧪 [Bitwise operations in Go](https://go.dev/ref/spec#Operators) — бітові операції у Go
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте `SampleIsNonSync` для визначення ключових кадрів** — критично для seek та коректного декодування.
> 2. **Коректно встановлюйте прапорці залежності** — уникнення помилок декодування через неправильні референси.
> 3. **Використовуйте готові константи (`SampleNonKeyframe`)** — уникнення помилок при ручній комбінації прапорців.
> 4. **Обробляйте `FirstSampleFlags` у trun окремо** — перший семпл може мати спеціальні властивості.
> 5. **Валідуйте конфліктні прапорці** — уникнення неможливих комбінацій (`HasDependencies` + `NoDependencies`).

Потрібен приклад реалізації повного циклу обробки прапорців семплів для HLS/DASH streaming, або інтеграція `fmp4io.SampleFlags` з вашим `mse.Muxer` для коректної синхронізації ключових кадрів у WebSocket стрімінгу? Готовий допомогти! 🚀