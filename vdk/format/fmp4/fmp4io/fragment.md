# 📦 Глибокий розбір: `fmp4io.MovieFrag` — Метадані фрагменту для fMP4 streaming

Цей файл — **реалізація атомів `moof` (Movie Fragment), `mfhd` (Movie Fragment Header), `traf` (Track Fragment), `trun` (Track Run), `tfdt` (Track Fragment Decode Time), та `tfhd` (Track Fragment Header)** для фрагментованого MP4 (fMP4). Ці атоми є критичними для low-latency streaming, дозволяючи кожному фрагменту бути самодостатнім.

---

## 🗺️ Архітектурна схема fMP4 фрагменту

```
┌────────────────────────────────────────┐
│ 📦 fMP4 Fragment Structure            │
├────────────────────────────────────────┤
│                                         │
│  🔑 Основні атоми фрагменту:           │
│  • moof (MovieFrag) — метадані фрагменту│
│  │  ├─ mfhd (MovieFragHeader) — номер фрагменту│
│  │  └─ traf × N (TrackFrag) — дані треку│
│  │      ├─ tfhd (TrackFragHeader) — default параметри│
│  │      ├─ tfdt (TrackFragDecodeTime) — базовий час│
│  │      └─ trun (TrackFragRun) — таблиця семплів│
│  • mdat — медіа-дані (окремий атом)    │
│                                         │
│  🔄 Потік streaming:                    │
│  [ftyp][moov] ← init segment          │
│  [styp][moof][mdat] ← фрагмент 1      │
│  [styp][moof][mdat] ← фрагмент 2      │
│  ...                                  │
│                                         │
│  📡 Ключові переваги:                   │
│  • Кожен фрагмент самодостатній       │
│  • Можливість seek до будь-якого фрагменту│
│  • Низька затримка для live streaming │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. MovieFrag (moof) — контейнер метаданих фрагменту

### 🔧 Структура та призначення:

```go
type MovieFrag struct {
    Header   *MovieFragHeader  // mfhd: номер фрагменту, прапорці
    Tracks   []*TrackFrag      // traf × N: дані кожного треку у фрагменті
    Unknowns []Atom            // невідомі дочірні атоми для сумісності
    AtomPos                   // offset/size у файлі
}
```

### 🔍 Призначення moof атому:

```
moof (Movie Fragment) містить метадані для одного фрагменту медіа:

• Дозволяє фрагменту бути самодостатнім (не потребує посилань на moov)
• Визначає таймінги, розміри, прапорці для семплів у цьому фрагменті
• Критичний для low-latency streaming (DASH, HLS fMP4, CMAF)

Структура:
  moof (MovieFrag)
  ├─ mfhd (MovieFragHeader) — sequence number, прапорці
  └─ traf × N (TrackFrag) — по одному на кожен трек у фрагменті
      ├─ tfhd (TrackFragHeader) — default параметри для треку
      ├─ tfdt (TrackFragDecodeTime) — базовий DTS для фрагменту
      └─ trun (TrackFragRun) — таблиця семплів з таймінгами/розмірами
```

### ✅ Ваш use-case**: пошук фрагменту за номером

```go
// FindFragmentBySeqnum — пошук moof атому за sequence number
func FindFragmentBySeqnum(atoms []fmp4io.Atom, seqnum uint32) (*fmp4io.MovieFrag, error) {
    for _, atom := range atoms {
        if atom.Tag() != fmp4io.MOOF {
            continue
        }
        
        moof, ok := atom.(*fmp4io.MovieFrag)
        if !ok {
            continue
        }
        
        // Перевірка sequence number у mfhd header
        if moof.Header != nil && moof.Header.Seqnum == seqnum {
            return moof, nil
        }
    }
    return nil, fmt.Errorf("fragment with seqnum %d not found", seqnum)
}

// Використання:
moofs := extractMOOFAtoms(file)  // helper function
fragment, err := FindFragmentBySeqnum(moofs, 42)
if err != nil { /* handle error */ }
// fragment містить метадані для фрагменту #42
```

---

## 🔑 2. MovieFragHeader (mfhd) — заголовок фрагменту

### 🔧 Структура та призначення:

```go
type MovieFragHeader struct {
    Version uint8   // версія формату (зазвичай 0)
    Flags   uint32  // бітові прапорці (зазвичай 0)
    Seqnum  uint32  // ⭐ послідовний номер фрагменту (критично для streaming)
    AtomPos
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `Version` | `uint8` | Версія формату (для зворотньої сумісності) | `0` = базовий формат |
| `Flags` | `uint32` | Бітові прапорці (зазвичай 0 для mfhd) | `0` = без додаткових прапорців |
| `Seqnum` | `uint32` | **Критично**: послідовний номер фрагменту | `1, 2, 3, ...` для порядку відтворення |

### 🔍 Чому Seqnum критичний:

```
Seqnum (sequence number) використовується для:
• Визначення порядку фрагментів при відтворенні
• Виявлення втрачених фрагментів у мережі
• Синхронізації клієнта та сервера у live streaming
• Реалізації buffer management на клієнті

Приклади використання:
• HLS: сегменти нумеруються послідовно (#1, #2, #3...)
• DASH: segment numbering у MPD маніфесті
• Low-latency: клієнт запитує фрагменти за номером
```

### ✅ Ваш use-case**: валідація послідовності фрагментів

```go
// ValidateFragmentSequence — перевірка послідовності seqnum у фрагментах
func ValidateFragmentSequence(moofs []*fmp4io.MovieFrag) error {
    if len(moofs) == 0 {
        return nil
    }
    
    expectedSeq := moofs[0].Header.Seqnum
    for i, moof := range moofs {
        if moof.Header == nil {
            return fmt.Errorf("fragment %d: missing mfhd header", i)
        }
        
        if moof.Header.Seqnum != expectedSeq {
            return fmt.Errorf("fragment %d: expected seqnum %d, got %d", 
                i, expectedSeq, moof.Header.Seqnum)
        }
        expectedSeq++
    }
    
    return nil
}

// Використання:
moofs := extractMOOFAtoms(file)
if err := ValidateFragmentSequence(moofs); err != nil {
    log.Printf("warning: fragment sequence error: %v", err)
    // Можна спробувати відновитися або пропустити пошкоджені фрагменти
}
```

---

## 🔑 3. TrackFragRun (trun) — таблиця семплів у фрагменті

### 🔧 Структура та призначення:

```go
type TrackFragRun struct {
    Version          uint8              // версія (0 або 1 для 64-бітних CTS)
    Flags            TrackRunFlags      // ⭐ бітові прапорці для опціональних полів
    DataOffset       uint32             // зміщення даних відносно базового
    FirstSampleFlags SampleFlags        // прапорці для першого семплу
    Entries          []TrackFragRunEntry // ⭐ масив записів для кожного семплу
    AtomPos
}
```

### 🔍 TrackRunFlags — бітова маска опціональних полів:

```go
const (
    TrackRunDataOffset       TrackRunFlags = 0x01   // присутній DataOffset
    TrackRunFirstSampleFlags TrackRunFlags = 0x04   // присутній FirstSampleFlags
    TrackRunSampleDuration   TrackRunFlags = 0x100  // Duration у кожному Entry
    TrackRunSampleSize       TrackRunFlags = 0x200  // Size у кожному Entry
    TrackRunSampleFlags      TrackRunFlags = 0x400  // Flags у кожному Entry
    TrackRunSampleCTS        TrackRunFlags = 0x800  // CTS у кожному Entry
)
```

### 🔍 TrackFragRunEntry — запис для одного семплу:

```go
type TrackFragRunEntry struct {
    Duration uint32     // тривалість семплу у ticks (опціонально)
    Size     uint32     // розмір даних семплу у байтах (опціонально)
    Flags    SampleFlags // прапорці семплу (опціонально)
    CTS      int32      // composition time offset (опціонально, signed)
}
```

### 🔍 Як працюють прапорці:

```
Приклад: Flags = 0x00000301 (біти 0, 8, 9 встановлені)

Біт 0 (0x01): TrackRunDataOffset → присутній DataOffset поле
Біт 2 (0x04): TrackRunFirstSampleFlags → присутній FirstSampleFlags
Біт 8 (0x100): TrackRunSampleDuration → Duration у кожному Entry
Біт 9 (0x200): TrackRunSampleSize → Size у кожному Entry
Біт 10 (0x400): TrackRunSampleFlags → Flags у кожному Entry
Біт 11 (0x800): TrackRunSampleCTS → CTS у кожному Entry

Це дозволяє економити місце: якщо всі семпли мають однаковий розмір,
можна вказати DefaultSampleSize у tfhd і не включати Size у кожен Entry.
```

### ✅ Ваш use-case**: парсинг семплів з trun атому

```go
// ParseTrackRunSamples — витягування інформації про семпли з trun атому
func ParseTrackRunSamples(trun *fmp4io.TrackFragRun, baseDTS uint64) ([]SampleInfo, error) {
    var samples []SampleInfo
    currentDTS := baseDTS
    
    for i, entry := range trun.Entries {
        // Визначення прапорців для цього семплу
        flags := trun.Flags
        if i == 0 && (trun.Flags&fmp4io.TrackRunFirstSampleFlags != 0) {
            flags = fmp4io.TrackRunFlags(trun.FirstSampleFlags)
        }
        
        // Розрахунок параметрів
        duration := uint32(0)
        if flags&fmp4io.TrackRunSampleDuration != 0 {
            duration = entry.Duration
        }
        
        size := uint32(0)
        if flags&fmp4io.TrackRunSampleSize != 0 {
            size = entry.Size
        }
        
        cts := int64(0)
        if flags&fmp4io.TrackRunSampleCTS != 0 {
            cts = int64(entry.CTS)
        }
        
        samples = append(samples, SampleInfo{
            DTS:      currentDTS,
            PTS:      currentDTS + uint64(cts),
            Duration: uint64(duration),
            Size:     size,
            IsKeyFrame: (flags&fmp4io.TrackRunSampleFlags != 0) && 
                       (entry.Flags&SampleFlagIsNonSync == 0),
        })
        
        currentDTS += uint64(duration)
    }
    
    return samples, nil
}

type SampleInfo struct {
    DTS, PTS, Duration uint64
    Size               uint32
    IsKeyFrame         bool
}
```

---

## 🔑 4. TrackFragDecodeTime (tfdt) — базовий час декодування

### 🔧 Структура та призначення:

```go
type TrackFragDecodeTime struct {
    Version uint8   // 0 = 32-бітний час, 1 = 64-бітний час
    Flags   uint32  // прапорці (зазвичай 0)
    Time    uint64  // ⭐ базовий DTS для першого семплу у фрагменті
    AtomPos
}
```

### 🔍 Версії та формати часу:

```
Version 0 (32-бітний час):
• Time зберігається як uint32 у Marshal/Unmarshal
• Діапазон: 0..4,294,967,295 ticks
• При timeScale=90000: ~13 годин максимальної тривалості

Version 1 (64-бітний час):
• Time зберігається як uint64
• Діапазон: практично необмежений
• Рекомендується для довгих потоків або high-precision таймінгів

Конвертація у time.Duration:
    duration := time.Duration(tfdt.Time) * time.Second / time.Duration(timeScale)
```

### ✅ Ваш use-case**: розрахунок абсолютного часу семплу

```go
// CalculateAbsoluteTime — розрахунок абсолютного DTS/PTS для семплу
func CalculateAbsoluteTime(tfdt *fmp4io.TrackFragDecodeTime, sampleIndex int, 
                          trun *fmp4io.TrackFragRun, timeScale int64) (dts, pts time.Duration) {
    // 1. Базовий DTS з tfdt
    baseDTS := time.Duration(tfdt.Time) * time.Second / time.Duration(timeScale)
    
    // 2. Додавання тривалостей попередніх семплів
    accumulatedDuration := time.Duration(0)
    for i := 0; i < sampleIndex && i < len(trun.Entries); i++ {
        entry := trun.Entries[i]
        if trun.Flags&fmp4io.TrackRunSampleDuration != 0 {
            accumulatedDuration += time.Duration(entry.Duration) * time.Second / time.Duration(timeScale)
        }
    }
    
    dts = baseDTS + accumulatedDuration
    
    // 3. Додавання composition offset для PTS
    if sampleIndex < len(trun.Entries) && trun.Flags&fmp4io.TrackRunSampleCTS != 0 {
        cts := time.Duration(trun.Entries[sampleIndex].CTS) * time.Second / time.Duration(timeScale)
        pts = dts + cts
    } else {
        pts = dts
    }
    
    return dts, pts
}
```

---

## 🔑 5. TrackFragHeader (tfhd) — параметри треку за замовчуванням

### 🔧 Структура та призначення:

```go
type TrackFragHeader struct {
    Version         uint8
    Flags           TrackFragFlags  // ⭐ прапорці для опціональних полів
    TrackID         uint32          // ідентифікатор треку
    BaseDataOffset  uint64          // базове зміщення даних (опціонально)
    StsdID          uint32          // індекс опису кодека (опціонально)
    DefaultDuration uint32          // тривалість за замовчуванням (опціонально)
    DefaultSize     uint32          // розмір за замовчуванням (опціонально)
    DefaultFlags    SampleFlags     // прапорці за замовчуванням (опціонально)
    AtomPos
}
```

### 🔍 TrackFragFlags — бітова маска:

```go
const (
    TrackFragBaseDataOffset    TrackFragFlags = 0x01   // присутній BaseDataOffset
    TrackFragStsdID            TrackFragFlags = 0x02   // присутній StsdID
    TrackFragDefaultDuration   TrackFragFlags = 0x08   // присутній DefaultDuration
    TrackFragDefaultSize       TrackFragFlags = 0x10   // присутній DefaultSize
    TrackFragDefaultFlags      TrackFragFlags = 0x20   // присутній DefaultFlags
    TrackFragDurationIsEmpty   TrackFragFlags = 0x010000  // тривалість фрагменту = 0
    TrackFragDefaultBaseIsMOOF TrackFragFlags = 0x020000  // BaseDataOffset відносно moof
)
```

### 🔍 SampleFlags — прапорці семплу:

```
Біти прапорців (ISO/IEC 14496-12):
• 0x00000001 — sample depends on other samples
• 0x00000002 — sample is depended on by other samples  
• 0x00000004 — sample has redundant coding
• 0x01000000 — sample is a sync sample (keyframe) ⭐
• 0x02000000 — sample is NOT a sync sample ⭐

Приклади:
• H.264 IDR frame: 0x01000000 (sync sample)
• H.264 P/B frame: 0x02000000 (not sync)
• AAC frame: 0x00000000 (аудіо не має поняття keyframe)
```

### ✅ Ваш use-case**: визначення ключових кадрів у фрагменті

```go
// IsKeyFrame — перевірка чи семпл є ключовим кадром
func IsKeyFrame(trun *fmp4io.TrackFragRun, tfhd *fmp4io.TrackFragHeader, sampleIndex int) bool {
    if sampleIndex >= len(trun.Entries) {
        return false
    }
    
    // Визначення прапорців для цього семплу
    flags := trun.Flags
    if sampleIndex == 0 && (trun.Flags&fmp4io.TrackRunFirstSampleFlags != 0) {
        flags = fmp4io.TrackRunFlags(trun.FirstSampleFlags)
    }
    
    // Якщо прапорець SampleFlags присутній — читаємо з Entry
    if flags&fmp4io.TrackRunSampleFlags != 0 {
        entryFlags := trun.Entries[sampleIndex].Flags
        return (entryFlags & SampleFlagIsNonSync) == 0
    }
    
    // Інакше використовуємо DefaultFlags з tfhd
    if tfhd != nil && (tfhd.Flags&fmp4io.TrackFragDefaultFlags != 0) {
        return (tfhd.DefaultFlags & SampleFlagIsNonSync) == 0
    }
    
    // За замовчуванням припускаємо що не ключовий
    return false
}

const SampleFlagIsNonSync SampleFlags = 0x02000000
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення fMP4 фрагменту для streaming

```go
// CreateFMP4Fragment — генерація одного фрагменту для HLS/DASH
func CreateFMP4Fragment(seqnum uint32, trackID uint32, samples []MediaSample, 
                       timeScale int64) ([]byte, []byte, error) {
    // 1. Створення moof атому
    moof := &fmp4io.MovieFrag{
        Header: &fmp4io.MovieFragHeader{
            Seqnum: seqnum,
        },
        Tracks: []*fmp4io.TrackFrag{
            &fmp4io.TrackFrag{
                Header: &fmp4io.TrackFragHeader{
                    TrackID: trackID,
                    Flags:   fmp4io.TrackFragDefaultDuration | fmp4io.TrackFragDefaultFlags,
                    DefaultDuration: 900,  // 10ms @ 90kHz
                    DefaultFlags:    0x02000000,  // not sync за замовчуванням
                },
                DecodeTime: &fmp4io.TrackFragDecodeTime{
                    Version: 0,  // 32-бітний час
                    Time:    uint64(samples[0].DTS),  // базовий DTS
                },
                Run: &fmp4io.TrackFragRun{
                    Flags: fmp4io.TrackRunSampleDuration | fmp4io.TrackRunSampleSize | fmp4io.TrackRunSampleFlags,
                    Entries: make([]fmp4io.TrackFragRunEntry, len(samples)),
                },
            },
        },
    }
    
    // 2. Заповнення Entries у trun
    trun := moof.Tracks[0].Run
    for i, sample := range samples {
        entry := &trun.Entries[i]
        entry.Duration = uint32(sample.Duration)
        entry.Size = uint32(len(sample.Data))
        
        // Встановлення прапорця ключового кадру
        if sample.IsKeyFrame {
            entry.Flags = 0x01000000  // sync sample
        } else {
            entry.Flags = 0x02000000  // not sync
        }
        
        // CTS для B-frames (якщо потрібно)
        if sample.CTS != 0 {
            trun.Flags |= fmp4io.TrackRunSampleCTS
            entry.CTS = int32(sample.CTS)
        }
    }
    
    // 3. Серіалізація moof
    moofBytes := make([]byte, moof.Len())
    moof.Marshal(moofBytes)
    
    // 4. Створення mdat з медіа-даними
    mdat := createMDATAtom(samples)  // helper function
    
    return moofBytes, mdat, nil
}

type MediaSample struct {
    Data       []byte
    DTS        int64  // decoding time stamp у ticks
    Duration   int64  // тривалість у ticks
    CTS        int64  // composition time offset (для B-frames)
    IsKeyFrame bool
}
```

### 🔧 Приклад: Парсинг fMP4 фрагменту для відтворення

```go
// ParseFMP4FragmentForPlayback — підготовка фрагменту для відтворення
func ParseFMP4FragmentForPlayback(moofData, mdatData []byte, timeScale int64) ([]PlaybackSample, error) {
    // 1. Парсинг moof атому
    var moof fmp4io.MovieFrag
    if _, err := moof.Unmarshal(moofData, 0); err != nil {
        return nil, fmt.Errorf("parse moof: %w", err)
    }
    
    if len(moof.Tracks) == 0 || moof.Tracks[0].Run == nil {
        return nil, fmt.Errorf("invalid fragment: no track run data")
    }
    
    track := moof.Tracks[0]
    trun := track.Run
    tfhd := track.Header
    tfdt := track.DecodeTime
    
    // 2. Отримання базового DTS
    baseDTS := int64(0)
    if tfdt != nil {
        baseDTS = int64(tfdt.Time)
    }
    
    // 3. Парсинг семплів
    var samples []PlaybackSample
    currentDTS := baseDTS
    
    for i, entry := range trun.Entries {
        // Визначення прапорців
        flags := trun.Flags
        if i == 0 && (trun.Flags&fmp4io.TrackRunFirstSampleFlags != 0) {
            flags = fmp4io.TrackRunFlags(trun.FirstSampleFlags)
        }
        
        // Розрахунок параметрів
        duration := int64(0)
        if flags&fmp4io.TrackRunSampleDuration != 0 {
            duration = int64(entry.Duration)
        } else if tfhd != nil && (tfhd.Flags&fmp4io.TrackFragDefaultDuration != 0) {
            duration = int64(tfhd.DefaultDuration)
        }
        
        size := int64(0)
        if flags&fmp4io.TrackRunSampleSize != 0 {
            size = int64(entry.Size)
        } else if tfhd != nil && (tfhd.Flags&fmp4io.TrackFragDefaultSize != 0) {
            size = int64(tfhd.DefaultSize)
        }
        
        cts := int64(0)
        if flags&fmp4io.TrackRunSampleCTS != 0 {
            cts = int64(entry.CTS)
        }
        
        // Визначення ключового кадру
        isKeyFrame := IsKeyFrame(trun, tfhd, i)
        
        // Витягування даних з mdat (спрощено)
        dataStart := calculateDataOffset(track, i, mdatData)  // helper function
        data := mdatData[dataStart : dataStart+size]
        
        samples = append(samples, PlaybackSample{
            Data:       data,
            DTS:        time.Duration(currentDTS) * time.Second / time.Duration(timeScale),
            PTS:        time.Duration(currentDTS+cts) * time.Second / time.Duration(timeScale),
            Duration:   time.Duration(duration) * time.Second / time.Duration(timeScale),
            IsKeyFrame: isKeyFrame,
        })
        
        currentDTS += duration
    }
    
    return samples, nil
}

type PlaybackSample struct {
    Data       []byte
    DTS, PTS, Duration time.Duration
    IsKeyFrame bool
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при читанні 3-байтових полів** | Доступ за межами буфера у Unmarshal | Додайте перевірку `if len(b) < n+3` перед `pio.U24BE()` |
| **Невірний розрахунок PTS** | Розсинхронізація аудіо/відео | Перевірте наявність та коректність `TrackRunSampleCTS` прапорця |
| **Втрачені ключові кадри** | Неможливий seek, помилки декодування | Переконайтеся що `SampleFlagIsNonSync` встановлено коректно |
| **Некоректний DataOffset** | Дані читаються з неправильної позиції | Перевірте `TrackFragBaseDataOffset` прапорець та розрахунок офсету |
| **Переповнення 32-бітного часу** | Помилки для довгих потоків | Використовуйте `Version=1` для 64-бітного часу у `TrackFragDecodeTime` |

---

## ⚡ Оптимізації для low-latency streaming

### 1. Кешування серіалізованих trun атомів:

```go
type CachedTrackFragRun struct {
    *fmp4io.TrackFragRun
    serialized []byte
    dirty      bool
    mu         sync.RWMutex
}

func (c *CachedTrackFragRun) Marshal(b []byte) int {
    c.mu.RLock()
    if !c.dirty && len(c.serialized) > 0 {
        n := copy(b, c.serialized)
        c.mu.RUnlock()
        return n
    }
    c.mu.RUnlock()
    
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Серіалізація якщо не в кеші
    n := c.TrackFragRun.Marshal(b)
    c.serialized = make([]byte, n)
    copy(c.serialized, b[:n])
    c.dirty = false
    return n
}

func (c *CachedTrackFragRun) MarkDirty() {
    c.mu.Lock()
    c.dirty = true
    c.serialized = nil
    c.mu.Unlock()
}
```

### 2. Попередня аллокація буферів для Marshal:

```go
// PreallocateMOOFBuffer — виділення місця для серіалізації заздалегідь
func PreallocateMOOFBuffer(moof *fmp4io.MovieFrag) []byte {
    estimatedSize := moof.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateMOOFBuffer(moof)
n := moof.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type FragmentMetrics struct {
    FragmentsParsed prometheus.CounterVec
    ParseLatency    prometheus.HistogramVec
    SampleCount     prometheus.HistogramVec
    ParseErrors     prometheus.CounterVec
}

func (m *FragmentMetrics) RecordParse(sampleCount int, duration time.Duration, err error) {
    m.FragmentsParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    m.SampleCount.Observe(float64(sampleCount))
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання MovieFrag/TrackFragRun

```go
// ✅ 1. Перевірка меж буфера перед читанням 3-байтових полів
if len(b) < n+3 {
    err = parseErr("Flags", n+offset, err)
    return
}
a.Flags = TrackRunFlags(pio.U24BE(b[n:]))
n += 3

// ✅ 2. Валідація Seqnum для уникнення дублікатів
if moof.Header.Seqnum <= lastProcessedSeqnum {
    log.Printf("warning: duplicate fragment seqnum %d", moof.Header.Seqnum)
    return nil  // або пропустити
}

// ✅ 3. Коректне встановлення прапорців у trun
if sample.IsKeyFrame {
    entry.Flags = 0x01000000  // sync sample
} else {
    entry.Flags = 0x02000000  // not sync
}

// ✅ 4. Використання 64-бітного часу для довгих потоків
if expectedDuration > 4*time.Hour {  // межа для 32-бітного часу @ 90kHz
    tfdt.Version = 1  // 64-бітний час
    tfdt.Time = uint64(baseDTS)
}

// ✅ 5. Перевірка DataOffset перед доступом до даних
if trun.Flags&fmp4io.TrackRunDataOffset != 0 {
    if trun.DataOffset > uint32(len(mdatData)) {
        return fmt.Errorf("invalid DataOffset: %d > %d", trun.DataOffset, len(mdatData))
    }
}

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Parsed moof: seqnum=%d, tracks=%d, samples=%d", 
    moof.Header.Seqnum, len(moof.Tracks), len(trun.Entries))

// ✅ 7. Метрики для моніторингу
metrics.RecordParse(len(trun.Entries), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 23009-1 (DASH)](https://www.iso.org/standard/79329.html) — стандарт для fMP4 у streaming
- 📄 [CMAF Specification](https://www.iso.org/standard/74428.html) — Common Media Application Format
- 📄 [HLS fMP4 Guide](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 📄 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 🧪 [Go encoding/binary Package](https://pkg.go.dev/encoding/binary) — робота з бінарними даними

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед `pio.U24BE()`** — уникнення панік при пошкоджених файлах.
> 2. **Використовуйте `Seqnum` для уникнення дублікатів** — критично для надійного streaming.
> 3. **Коректно встановлюйте `SampleFlags` для ключових кадрів** — забезпечення можливості seek.
> 4. **Використовуйте 64-бітний час для довгих потоків** — уникнення переповнення таймінгів.
> 5. **Валідуйте `DataOffset` перед доступом до даних** — запобігання читанню за межами буфера.

Потрібен приклад реалізації повного циклу створення/парсингу fMP4 фрагментів з підтримкою кількох треків та low-latency оптимізацій, або інтеграція `fmp4io.MovieFrag` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀