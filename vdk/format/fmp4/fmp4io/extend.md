# 📦 Глибокий розбір: `fmp4io.MovieExtend` та `TrackExtend` — Метадані для фрагментованого MP4

Цей файл — **реалізація атомів `mvex` (Movie Extend) та `trex` (Track Extend)** для фрагментованого MP4 (fMP4). Ці атоми визначають параметри за замовчуванням для треків у фрагментах, що дозволяє ефективне потокове відтворення без необхідності читати весь файл.

---

## 🗺️ Архітектурна схема fMP4 ієрархії

```
┌────────────────────────────────────────┐
│ 📦 fMP4 Structure — Fragmented MP4    │
├────────────────────────────────────────┤
│                                         │
│  🔑 Основні атоми:                      │
│  • ftyp — File Type Box                │
│  • moov — Movie Box (init metadata)    │
│  │  └─ mvex — Movie Extend ⭐         │
│  │      └─ trex × N — Track Extend ⭐ │
│  • moof × N — Movie Fragment (data)    │
│  │  └─ traf × N — Track Fragment      │
│  │      └─ trun — Track Run           │
│  • mdat × N — Media Data               │
│                                         │
│  🔄 Потік streaming:                    │
│  [ftyp][moov][moof][mdat][moof][mdat] │
│                ↑     ↑                 │
│           init  fragments             │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. MovieExtend (mvex) — розширення метаданих для фрагментів

### 🔧 Структура та призначення:

```go
type MovieExtend struct {
    Tracks   []*TrackExtend  // масив trex атомів для кожного треку
    Unknowns []Atom          // невідомі дочірні атоми для сумісності
    AtomPos                  // offset/size у файлі
}
```

### 🔍 Призначення mvex атому:

```
mvex (Movie Extend) містить параметри за замовчуванням для треків у фрагментах:

• Дозволяє фрагментам бути самодостатніми (не потребують посилань на moov)
• Визначає default параметри для TrackFragRun (trun) атомів
• Критичний для low-latency streaming (DASH, HLS fMP4)

Структура:
  mvex (MovieExtend)
  └─ trex × N (TrackExtend) — по одному на кожен трек
      ├─ TrackID — ідентифікатор треку
      ├─ DefaultSampleDescIdx — індекс опису кодека
      ├─ DefaultSampleDuration — тривалість семплу за замовчуванням
      ├─ DefaultSampleSize — розмір семплу за замовчуванням
      └─ DefaultSampleFlags — прапорці за замовчуванням
```

### ✅ Ваш use-case**: ініціалізація fMP4 streaming

```go
// CreateFMP4Init — генерація init segment для fMP4 streaming
func CreateFMP4Init(videoCodec, audioCodec string) ([]byte, error) {
    // 1. Створення ftyp атому
    ftyp := createFTYPAtom()
    
    // 2. Створення moov атому з mvex
    moov := &fmp4io.Movie{
        Header: &fmp4io.MovieHeader{
            TimeScale: 90000,  // 90kHz для відео
            // ... інші параметри ...
        },
        MovieExtend: &fmp4io.MovieExtend{
            Tracks: []*fmp4io.TrackExtend{
                // Відео трек
                &fmp4io.TrackExtend{
                    TrackID:              1,
                    DefaultSampleDescIdx: 1,
                    DefaultSampleDuration: 900,  // 10ms @ 90kHz
                    DefaultSampleSize:     0,     // змінний розмір
                    DefaultSampleFlags:    0x02000000,  // sample is sync
                },
                // Аудіо трек
                &fmp4io.TrackExtend{
                    TrackID:              2,
                    DefaultSampleDescIdx: 1,
                    DefaultSampleDuration: 1024,  // AAC frame @ 48kHz
                    DefaultSampleSize:     0,
                    DefaultSampleFlags:    0,
                },
            },
        },
        // ... треки з метаданими ...
    }
    
    // 3. Серіалізація
    moovBytes := make([]byte, moov.Len())
    moov.Marshal(moovBytes)
    
    return append(ftyp, moovBytes...), nil
}
```

---

## 🔑 2. TrackExtend (trex) — параметри треку за замовчуванням

### 🔧 Структура та призначення:

```go
type TrackExtend struct {
    Version               uint8   // версія формату (зазвичай 0)
    Flags                 uint32  // бітові прапорці (зазвичай 0)
    TrackID               uint32  // унікальний ідентифікатор треку
    DefaultSampleDescIdx  uint32  // індекс опису кодека у stsd таблиці
    DefaultSampleDuration uint32  // тривалість семплу за замовчуванням (у ticks)
    DefaultSampleSize     uint32  // розмір семплу за замовчуванням (0 = змінний)
    DefaultSampleFlags    uint32  // прапорці семплу за замовчуванням
    AtomPos
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `TrackID` | `uint32` | Унікальний ідентифікатор треку у межах файлу | `1` для відео, `2` для аудіо |
| `DefaultSampleDescIdx` | `uint32` | Індекс опису кодека у stsd таблиці | `1` = перший запис у SampleDesc |
| `DefaultSampleDuration` | `uint32` | Тривалість семплу за замовчуванням (у ticks) | `900` = 10ms @ 90kHz timeScale |
| `DefaultSampleSize` | `uint32` | Розмір семплу за замовчуванням (`0` = змінний) | `0` для відео, `1024` для фіксованого аудіо |
| `DefaultSampleFlags` | `uint32` | Прапорці семплу за замовчуванням | `0x02000000` = sample is sync (keyframe) |

### 🔍 DefaultSampleFlags — бітова маска:

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

### ✅ Ваш use-case**: налаштування прапорців для відео

```go
// SetDefaultSampleFlags — встановлення прапорців для відео треку
func SetDefaultSampleFlags(isKeyFrame bool) uint32 {
    if isKeyFrame {
        return 0x01000000  // sample is sync
    }
    return 0x02000000  // sample is NOT sync
}

// Використання при створенні trex:
trex := &fmp4io.TrackExtend{
    TrackID:              1,
    DefaultSampleFlags:   SetDefaultSampleFlags(true),  // припускаємо що перший кадр ключовий
    // ... інші поля ...
}
```

---

## 🔑 3. Marshal/Unmarshal — серіалізація атомів

### 🔧 Основна логіка Marshal для MovieExtend:

```go
func (a MovieExtend) marshal(b []byte) (n int) {
    // Рекурсивна серіалізація дочірніх атомів
    for _, atom := range a.Tracks {
        n += atom.Marshal(b[n:])  // серіалізація trex атому
    }
    for _, atom := range a.Unknowns {
        n += atom.Marshal(b[n:])  // серіалізація невідомих атомів
    }
    return
}
```

### 🔧 Основна логіка Unmarshal для TrackExtend:

```go
func (a *TrackExtend) Unmarshal(b []byte, offset int) (n int, err error) {
    (&a.AtomPos).setPos(offset, len(b))
    n += 8  // пропуск заголовку атому (size+tag)
    
    // Читання полів з перевіркою меж буфера
    if len(b) < n+1 { err = parseErr("Version", n+offset, err); return }
    a.Version = pio.U8(b[n:]); n += 1
    
    if len(b) < n+3 { err = parseErr("Flags", n+offset, err); return }
    a.Flags = pio.U24BE(b[n:]); n += 3  // 3-байтове читання big-endian
    
    // ... аналогічно для інших полів ...
    
    if len(b) < n+4 { err = parseErr("DefaultSampleFlags", n+offset, err); return }
    a.DefaultSampleFlags = pio.U32BE(b[n:]); n += 4
    
    return
}
```

### ⚠️ Критична проблема: відсутність перевірки меж для 3-байтових полів

```
У поточному коді:
    a.Flags = pio.U24BE(b[n:])  // ← читає 3 байти

Проблема:
• Якщо len(b) < n+3 → pio.U24BE може читати за межами буфера
• Це може призвести до паніки або некоректних даних

✅ Виправлення: перевірка меж перед читанням
    if len(b) < n+3 {
        err = parseErr("Flags", n+offset, err)
        return
    }
    a.Flags = pio.U24BE(b[n:])
    n += 3
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення fMP4 init segment для HLS/DASH

```go
// GenerateFMP4InitSegment — генерація init segment для streaming
func GenerateFMP4InitSegment(config *StreamConfig) ([]byte, error) {
    // 1. Створення ftyp атому
    ftyp := &fmp4io.FileType{
        MajorBrand:       fmp4io.StringToTag("iso6"),
        MinorVersion:     1,
        CompatibleBrands: []uint32{
            fmp4io.StringToTag("iso6"),
            fmp4io.StringToTag("dash"),  // підтримка DASH
        },
    }
    ftypBytes := make([]byte, ftyp.Len())
    ftyp.Marshal(ftypBytes)
    
    // 2. Створення moov атому з mvex
    moov := &fmp4io.Movie{
        Header: &fmp4io.MovieHeader{
            TimeScale:     config.VideoTimeScale,
            Duration:      0,  // 0 для fMP4 (тривалість у фрагментах)
            PreferredRate: 1,
            PreferredVolume: 1,
            Matrix:        [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
        },
        MovieExtend: &fmp4io.MovieExtend{
            Tracks: []*fmp4io.TrackExtend{
                // Відео трек
                &fmp4io.TrackExtend{
                    TrackID:              1,
                    DefaultSampleDescIdx: 1,
                    DefaultSampleDuration: config.VideoDefaultDuration,
                    DefaultSampleSize:     0,  // змінний розмір для відео
                    DefaultSampleFlags:    0x02000000,  // не sync за замовчуванням
                },
                // Аудіо трек (якщо є)
                &fmp4io.TrackExtend{
                    TrackID:              2,
                    DefaultSampleDescIdx: 1,
                    DefaultSampleDuration: config.AudioDefaultDuration,
                    DefaultSampleSize:     0,
                    DefaultSampleFlags:    0,
                },
            },
        },
        // ... треки з метаданими кодека ...
    }
    
    // 3. Серіалізація
    moovBytes := make([]byte, moov.Len())
    moov.Marshal(moovBytes)
    
    // 4. Об'єднання ftyp + moov
    return append(ftypBytes, moovBytes...), nil
}

type StreamConfig struct {
    VideoTimeScale      int32   // зазвичай 90000 для відео
    AudioTimeScale      int32   // зазвичай 48000 для аудіо
    VideoDefaultDuration uint32 // тривалість відео семплу у ticks
    AudioDefaultDuration uint32 // тривалість аудіо семплу у ticks
}
```

### 🔧 Приклад: Парсинг fMP4 init segment для валідації

```go
// ValidateFMP4Init — перевірка коректності init segment
func ValidateFMP4Init(data []byte) error {
    // 1. Парсинг атомів з буфера
    reader := bytes.NewReader(data)
    atoms, err := fmp4io.ReadFileAtoms(reader)
    if err != nil {
        return fmt.Errorf("parse atoms: %w", err)
    }
    
    // 2. Пошук moov атому
    var moov *fmp4io.Movie
    for _, atom := range atoms {
        if atom.Tag() == fmp4io.MOOV {
            moov, _ = atom.(*fmp4io.Movie)
            break
        }
    }
    if moov == nil {
        return fmt.Errorf("moov atom not found")
    }
    
    // 3. Перевірка наявності mvex
    if moov.MovieExtend == nil {
        return fmt.Errorf("mvex atom not found in moov")
    }
    
    // 4. Валідація trex атомів
    for i, trex := range moov.MovieExtend.Tracks {
        if trex.TrackID == 0 {
            return fmt.Errorf("track %d: invalid TrackID=0", i)
        }
        if trex.DefaultSampleDescIdx == 0 {
            return fmt.Errorf("track %d: invalid DefaultSampleDescIdx=0", i)
        }
        // Додаткові перевірки...
    }
    
    // 5. Перевірка ftyp атому
    var ftyp *fmp4io.FileType
    for _, atom := range atoms {
        if atom.Tag() == fmp4io.FTYP {
            ftyp, _ = atom.(*fmp4io.FileType)
            break
        }
    }
    if ftyp == nil {
        return fmt.Errorf("ftyp atom not found")
    }
    
    // Перевірка сумісних брендів для streaming
    hasDash := false
    for _, brand := range ftyp.CompatibleBrands {
        if brand == fmp4io.StringToTag("dash") {
            hasDash = true
            break
        }
    }
    if !hasDash {
        return fmt.Errorf("ftyp missing 'dash' compatible brand")
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при читанні 3-байтових полів** | Доступ за межами буфера у Unmarshal | Додайте перевірку `if len(b) < n+3` перед `pio.U24BE()` |
| **Невірний TrackID** | Треки не ідентифікуються коректно | Переконайтеся що TrackID унікальний та > 0 |
| **Некоректні DefaultSampleFlags** | Ключові кадри не розпізнаються | Використовуйте `0x01000000` для sync samples |
| **Відсутній mvex атом** | Фрагменти не можуть бути відтворені | Додайте `MovieExtend` при створенні moov |
| **Невідповідність timeScale** | Розсинхронізація аудіо/відео | Використовуйте однаковий timeScale для всіх треків або конвертуйте часи |

---

## ⚡ Оптимізації для high-performance streaming

### 1. Кешування серіалізованих trex атомів:

```go
type CachedTrackExtend struct {
    *fmp4io.TrackExtend
    serialized []byte
    dirty      bool
    mu         sync.RWMutex
}

func (c *CachedTrackExtend) Marshal(b []byte) int {
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
    n := c.TrackExtend.Marshal(b)
    c.serialized = make([]byte, n)
    copy(c.serialized, b[:n])
    c.dirty = false
    return n
}

func (c *CachedTrackExtend) MarkDirty() {
    c.mu.Lock()
    c.dirty = true
    c.serialized = nil
    c.mu.Unlock()
}
```

### 2. Попередня аллокація буферів для Marshal:

```go
// PreallocateMovieExtendBuffer — виділення місця для серіалізації заздалегідь
func PreallocateMovieExtendBuffer(mvex *fmp4io.MovieExtend) []byte {
    estimatedSize := mvex.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateMovieExtendBuffer(mvex)
n := mvex.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type MVEXMetrics struct {
    AtomsParsed prometheus.CounterVec
    ParseLatency prometheus.HistogramVec
    TrackCount prometheus.HistogramVec
    ParseErrors prometheus.CounterVec
}

func (m *MVEXMetrics) RecordParse(trackCount int, duration time.Duration, err error) {
    m.AtomsParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    m.TrackCount.Observe(float64(trackCount))
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання MovieExtend/TrackExtend

```go
// ✅ 1. Перевірка меж буфера перед читанням 3-байтових полів
if len(b) < n+3 {
    err = parseErr("Flags", n+offset, err)
    return
}
a.Flags = pio.U24BE(b[n:])
n += 3

// ✅ 2. Валідація TrackID та інших обов'язкових полів
if trex.TrackID == 0 {
    return fmt.Errorf("invalid TrackID: must be > 0")
}
if trex.DefaultSampleDescIdx == 0 {
    return fmt.Errorf("invalid DefaultSampleDescIdx: must be > 0")
}

// ✅ 3. Коректне встановлення DefaultSampleFlags
if isKeyFrame {
    trex.DefaultSampleFlags = 0x01000000  // sync sample
} else {
    trex.DefaultSampleFlags = 0x02000000  // not sync
}

// ✅ 4. Перевірка наявності mvex у moov для fMP4
if moov.MovieExtend == nil {
    return fmt.Errorf("mvex atom required for fragmented MP4")
}

// ✅ 5. Узгодження timeScale між треками
if videoTrack.TimeScale != audioTrack.TimeScale {
    log.Printf("warning: different timeScales: video=%d, audio=%d", 
        videoTrack.TimeScale, audioTrack.TimeScale)
    // Конвертація часів при необхідності
}

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Parsed mvex: %d tracks, first TrackID=%d", 
    len(mvex.Tracks), mvex.Tracks[0].TrackID)

// ✅ 7. Метрики для моніторингу
metrics.RecordParse(len(mvex.Tracks), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 23009-1 (DASH)](https://www.iso.org/standard/79329.html) — стандарт для fMP4 у streaming
- 📄 [CMAF Specification](https://www.iso.org/standard/74428.html) — Common Media Application Format
- 📄 [HLS fMP4 Guide](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 🧪 [Go encoding/binary Package](https://pkg.go.dev/encoding/binary) — робота з бінарними даними
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед `pio.U24BE()`** — уникнення панік при пошкоджених файлах.
> 2. **Валідуйте `TrackID` та `DefaultSampleDescIdx`** — забезпечення коректної ідентифікації треків.
> 3. **Використовуйте правильні `DefaultSampleFlags`** — `0x01000000` для ключових кадрів, `0x02000000` для звичайних.
> 4. **Додайте `mvex` атом при створенні fMP4 init segment** — без нього фрагменти не зможуть бути відтворені.
> 5. **Узгоджуйте `timeScale` між треками** — уникнення розсинхронізації аудіо/відео.

Потрібен приклад реалізації повного циклу створення/парсингу fMP4 init segment з підтримкою кількох треків, або інтеграція `fmp4io.MovieExtend` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀