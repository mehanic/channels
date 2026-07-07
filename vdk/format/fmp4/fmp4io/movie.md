# 📦 Глибокий розбір: `fmp4io.Movie` — Кореневий контейнер для MP4/fMP4

Цей файл — **реалізація атомів `moov` (Movie), `mvhd` (Movie Header), `trak` (Track), та `tkhd` (Track Header)** для опису медіа-файлів у форматі MP4. Ці атоми є кореневими для метаданих файлу і містять критичну інформацію для відтворення відео та аудіо потоків.

---

## 🗺️ Архітектурна схема Movie ієрархії

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.Movie — Root Metadata       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Основні атоми:                      │
│  • moov (Movie) — кореневий контейнер  │
│  │  ├─ mvhd (MovieHeader) — глобальні параметри│
│  │  ├─ mvex (MovieExtend) — fMP4 розширення│
│  │  └─ trak × N (Track) — окремі треки│
│  │      ├─ tkhd (TrackHeader) — параметри треку│
│  │      └─ mdia (Media) — медіа-специфіка│
│                                         │
│  🔄 Ієрархія файлу:                     │
│  [ftyp][moov][mdat] ← стандартний MP4 │
│  [ftyp][moov][moof][mdat]... ← fMP4   │
│                                         │
│  📡 Призначення:                        │
│  • Метадані всього файлу              │
│  • Опис треків (відео/аудіо/субтитри) │
│  • Синхронізація таймінгів            │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Movie (moov) — кореневий контейнер метаданих

### 🔧 Структура та призначення:

```go
type Movie struct {
    Header      *MovieHeader   // mvhd: глобальні параметри файлу
    MovieExtend *MovieExtend   // mvex: розширення для fMP4 (опціонально)
    Tracks      []*Track       // trak × N: масив треків (відео/аудіо/субтитри)
    Unknowns    []Atom         // невідомі дочірні атоми для сумісності
    AtomPos                    // offset/size у файлі
}
```

### 🔍 Призначення moov атому:

```
moov (Movie) містить ВСІ метадані для медіа-файлу:

• Глобальні параметри: timeScale, duration, creation time
• Опис кожного треку: кодек, роздільна здатність, таймінги
• Посилання на таблиці семплів для навігації по даних
• Критичний для відтворення: без moov файл неможливо відтворити

Структура:
  moov (Movie)
  ├─ mvhd (MovieHeader) — глобальні налаштування
  │  ├─ TimeScale — ticks per second для всього файлу
  │  ├─ Duration — загальна тривалість у ticks
  │  ├─ CreateTime/ModifyTime — часові мітки
  │  ├─ PreferredRate/Volume — налаштування відтворення
  │  ├─ Matrix — трансформація координат
  │  └─ NextTrackID — наступний доступний ID треку
  ├─ mvex (MovieExtend) — ⭐ для fMP4 streaming
  │  └─ trex × N — default параметри для фрагментів
  └─ trak × N (Track) — окремі медіа-потоки
     ├─ tkhd (TrackHeader) — параметри треку
     └─ mdia (Media) — медіа-специфічна інформація
```

### ✅ Ваш use-case**: пошук треків за типом

```go
// FindTracksByType — пошук треків певного типу (відео/аудіо)
func FindTracksByType(movie *fmp4io.Movie, trackType TrackType) []*fmp4io.Track {
    var result []*fmp4io.Track
    
    for _, track := range movie.Tracks {
        if track.Media != nil && track.Media.Handler != nil {
            subType := string(track.Media.Handler.SubType[:])
            switch trackType {
            case TrackTypeVideo:
                if subType == "vide" {
                    result = append(result, track)
                }
            case TrackTypeAudio:
                if subType == "soun" {
                    result = append(result, track)
                }
            case TrackTypeSubtitle:
                if subType == "subt" || subType == "text" {
                    result = append(result, track)
                }
            }
        }
    }
    
    return result
}

type TrackType int
const (
    TrackTypeVideo TrackType = iota
    TrackTypeAudio
    TrackTypeSubtitle
)

// Використання:
videoTracks := FindTracksByType(moov, TrackTypeVideo)
if len(videoTracks) == 0 {
    return fmt.Errorf("no video track found")
}
log.Printf("Found %d video tracks", len(videoTracks))
```

---

## 🔑 2. MovieHeader (mvhd) — глобальні параметри файлу

### 🔧 Структура та призначення:

```go
type MovieHeader struct {
    Version         uint8       // версія формату (0=32-бітний час, 1=64-бітний)
    Flags           uint32      // бітові прапорці (зазвичай 0)
    CreateTime      time.Time   // час створення файлу (епоха 1904)
    ModifyTime      time.Time   // час останньої модифікації
    TimeScale       uint32      // ⭐ ticks per second для всього файлу
    Duration        uint32      // ⭐ загальна тривалість у ticks
    PreferredRate   float64     // швидкість відтворення за замовчуванням (fixed-point 16.16)
    PreferredVolume float64     // гучність за замовчуванням (fixed-point 8.8)
    Matrix          [9]int32    // ⭐ 3x3 матриця трансформації координат
    NextTrackID     uint32      // наступний доступний ID для нового треку
    AtomPos
}
```

### 🔍 Призначення критичних полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `TimeScale` | `uint32` | **Критично**: кількість "тіків" за секунду для всього файлу | `90000` для відео (MPEG), `44100` для аудіо |
| `Duration` | `uint32` | **Критично**: загальна тривалість файлу у ticks | `360000` = 4 секунди @ 90kHz |
| `Matrix` | `[9]int32` | **Критично**: 3x3 матриця для трансформації координат відео | Identity matrix для без трансформації |

### 🔍 Матриця трансформації:

```
Matrix — це 3x3 матриця у row-major order для 2D трансформацій:

[ a  b  u ]
[ c  d  v ]
[ x  y  w ]

Де:
• [a b; c d] — лінійна трансформація (масштаб, обертання, skew)
• [u v] — трансляція (зсув)
• [x y w] — перспективні перетворення (зазвичай [0 0 1])

Identity matrix (без трансформації):
[ 0x00010000, 0, 0,
  0, 0x00010000, 0,
  0, 0, 0x40000000 ]

Де 0x00010000 = 1.0 у fixed-point 16.16, 0x40000000 = 16384.0

Приклад масштабування 2x:
[ 0x00020000, 0, 0,
  0, 0x00020000, 0,
  0, 0, 0x40000000 ]
```

### ✅ Ваш use-case**: конвертація тривалості у time.Duration

```go
// GetMovieDuration — отримання тривалості файлу у time.Duration
func GetMovieDuration(header *fmp4io.MovieHeader) time.Duration {
    if header == nil || header.TimeScale == 0 {
        return 0
    }
    
    // Конвертація ticks у секунди
    return time.Duration(header.Duration) * time.Second / time.Duration(header.TimeScale)
}

// GetMovieTimeScale — отримання global timeScale для конвертації таймінгів
func GetMovieTimeScale(header *fmp4io.MovieHeader) int64 {
    if header == nil {
        return 90000  // default для відео
    }
    return int64(header.TimeScale)
}

// Використання для синхронізації:
movieScale := GetMovieTimeScale(moov.Header)
videoDuration := GetMovieDuration(moov.Header)
log.Printf("Movie duration: %v (%d ticks @ %d Hz)", 
    videoDuration, moov.Header.Duration, movieScale)
```

---

## 🔑 3. Track (trak) — окремий медіа-потік

### 🔧 Структура та призначення:

```go
type Track struct {
    Header   *TrackHeader  // tkhd: параметри треку (ID, duration, розмір)
    Media    *Media        // mdia: медіа-специфічна інформація (кодек, таблиці)
    Unknowns []Atom        // невідомі дочірні атоми для сумісності
    AtomPos                // offset/size у файлі
}
```

### 🔍 Призначення trak атому:

```
trak (Track) містить всі метадані для одного медіа-потоку:

• TrackHeader: ідентифікатор, тривалість, розмір, прапорці
• Media: тип треку (відео/аудіо), кодек, таблиці семплів
• Критичний для демуксингу: без trak неможливо знайти дані треку

Структура:
  trak (Track)
  ├─ tkhd (TrackHeader) — параметри треку
  │  ├─ TrackID — унікальний ідентифікатор треку
  │  ├─ Duration — тривалість треку у ticks
  │  ├─ TrackWidth/Height — розмір відео у fixed-point 16.16
  │  ├─ Volume — гучність аудіо у fixed-point 8.8
  │  └─ Matrix — трансформація координат треку
  └─ mdia (Media) — медіа-специфіка
     ├─ mdhd — час, timeScale, мова
     ├─ hdlr — тип треку ('vide'/'soun')
     └─ minf — специфічна інформація
        ├─ vmhd/smhd — відео/аудіо специфіка
        ├─ dinf — посилання на дані
        └─ ⭐ stbl — таблиці семплів (найважливіше!)
```

### ✅ Ваш use-case**: отримання роздільної здатності відео

```go
// GetVideoResolution — отримання ширини/висоти відео треку
func GetVideoResolution(track *fmp4io.Track) (width, height int, err error) {
    if track == nil || track.Header == nil {
        return 0, 0, fmt.Errorf("nil track or header")
    }
    
    // TrackWidth/Height зберігаються у fixed-point 16.16 форматі
    // Конвертація: ціла_частина = значення >> 16
    width = int(track.Header.TrackWidth)
    height = int(track.Header.TrackHeight)
    
    // Валідація розумних значень
    if width < 16 || width > 16384 || height < 16 || height > 16384 {
        return 0, 0, fmt.Errorf("suspicious resolution: %dx%d", width, height)
    }
    
    return width, height, nil
}

// GetTrackVolume — отримання гучності аудіо треку
func GetTrackVolume(track *fmp4io.Track) float64 {
    if track == nil || track.Header == nil {
        return 1.0  // default гучність
    }
    // Volume зберігається у fixed-point 8.8 форматі
    return track.Header.Volume
}

// Використання:
for _, track := range moov.Tracks {
    if track.Header != nil {
        if track.Media != nil && track.Media.Handler != nil {
            subType := string(track.Media.Handler.SubType[:])
            if subType == "vide" {
                width, height, _ := GetVideoResolution(track)
                log.Printf("Video track %d: %dx%d", track.Header.TrackID, width, height)
            } else if subType == "soun" {
                volume := GetTrackVolume(track)
                log.Printf("Audio track %d: volume=%.2f", track.Header.TrackID, volume)
            }
        }
    }
}
```

---

## 🔑 4. TrackHeader (tkhd) — параметри треку

### 🔧 Структура та призначення:

```go
type TrackHeader struct {
    Version        uint8       // версія формату (0=32-бітний час, 1=64-бітний)
    Flags          uint32      // бітові прапорці (0x0001=enabled, 0x0002=in-movie)
    CreateTime     time.Time   // час створення треку (епоха 1904)
    ModifyTime     time.Time   // час останньої модифікації
    TrackID        uint32      // ⭐ унікальний ідентифікатор треку
    Duration       uint32      // ⭐ тривалість треку у ticks
    Layer          int16       // шар відображення (для накладання відео)
    AlternateGroup int16       // група альтернативних треків (для вибору мови)
    Volume         float64     // гучність треку (fixed-point 8.8, для аудіо)
    Matrix         [9]int32    // трансформація координат треку
    TrackWidth     float64     // ⭐ ширина відео у fixed-point 16.16
    TrackHeight    float64     // ⭐ висота відео у fixed-point 16.16
    AtomPos
}
```

### 🔍 Призначення критичних полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `TrackID` | `uint32` | **Критично**: унікальний ідентифікатор треку у межах файлу | `1` для першого відео, `2` для аудіо |
| `Duration` | `uint32` | **Критично**: тривалість треку у ticks (може відрізнятися від movie duration) | `360000` = 4 секунди @ 90kHz |
| `TrackWidth/Height` | `float64` | **Критично**: розмір відео у fixed-point 16.16 | `1920.0`, `1080.0` для Full HD |

### 🔍 Прапорці треку:

```
Flags бітова маска:
• 0x0001 — track enabled (трек активний)
• 0x0002 — track in movie (трек є частиною фільму)
• 0x0004 — track in preview (трек у прев'ю)
• 0x0008 — track in poster (трек у постері)

Зазвичай встановлюються 0x0003 (enabled | in-movie)
```

### ✅ Ваш use-case**: фільтрація активних треків

```go
// IsTrackEnabled — перевірка чи трек активний та у фільмі
func IsTrackEnabled(header *fmp4io.TrackHeader) bool {
    if header == nil {
        return false
    }
    // Перевірка прапорців enabled (0x0001) та in-movie (0x0002)
    return (header.Flags & 0x0003) == 0x0003
}

// GetActiveTracks — отримання тільки активних треків
func GetActiveTracks(movie *fmp4io.Movie) []*fmp4io.Track {
    var result []*fmp4io.Track
    for _, track := range movie.Tracks {
        if track.Header != nil && IsTrackEnabled(track.Header) {
            result = append(result, track)
        }
    }
    return result
}

// Використання:
activeTracks := GetActiveTracks(moov)
log.Printf("Found %d active tracks out of %d total", 
    len(activeTracks), len(moov.Tracks))
```

---

## 🔑 5. Helper-функції для навігації

### 🔧 GetAVC1Conf/GetElemStreamDesc — пошук конфігурації кодека:

```go
func (a *Track) GetAVC1Conf() (conf *AVC1Conf) {
    atom := FindChildren(a, AVCC)  // пошук avcC атому рекурсивно
    conf, _ = atom.(*AVC1Conf)     // type assertion
    return
}

func (a *Track) GetElemStreamDesc() (esds *ElemStreamDesc) {
    atom := FindChildren(a, ESDS)  // пошук esds атому рекурсивно
    esds, _ = atom.(*ElemStreamDesc)
    return
}
```

### 🔍 Призначення:

```
Ці helper-функції спрощують пошук конфігурації кодека у треку:

• GetAVC1Conf() — для H.264 відео: повертає AVCDecoderConfigurationRecord
• GetElemStreamDesc() — для AAC аудіо: повертає MPEG-4 Stream Descriptor

Використовують FindChildren() для рекурсивного пошуку по дереву атомів.
```

### ✅ Ваш use-case**: ініціалізація декодерів

```go
// InitTrackDecoders — ініціалізація кодеків для треку
func InitTrackDecoders(track *fmp4io.Track) (videoCodec av.CodecData, audioCodec av.CodecData, err error) {
    if track.Media == nil {
        return nil, nil, fmt.Errorf("no media in track")
    }
    
    // Визначення типу треку
    if track.Media.Handler != nil {
        subType := string(track.Media.Handler.SubType[:])
        
        if subType == "vide" {
            // Пошук H.264 конфігурації
            avcc := track.GetAVC1Conf()
            if avcc != nil && len(avcc.Data) > 0 {
                videoCodec, err = h264parser.NewCodecDataFromAVCDecoderConfRecord(avcc.Data)
                if err != nil {
                    return nil, nil, fmt.Errorf("init H.264 decoder: %w", err)
                }
            }
        } else if subType == "soun" {
            // Пошук AAC конфігурації
            esds := track.GetElemStreamDesc()
            if esds != nil && len(esds.DecConfig) > 0 {
                audioCodec, err = aacparser.NewCodecDataFromMPEG4AudioConfigBytes(esds.DecConfig)
                if err != nil {
                    return nil, nil, fmt.Errorf("init AAC decoder: %w", err)
                }
            }
        }
    }
    
    return videoCodec, audioCodec, nil
}

// Використання:
for _, track := range moov.Tracks {
    videoCodec, audioCodec, err := InitTrackDecoders(track)
    if err != nil {
        log.Printf("warning: init track %d: %v", track.Header.TrackID, err)
        continue
    }
    
    if videoCodec != nil {
        log.Printf("Track %d: H.264 decoder initialized", track.Header.TrackID)
    }
    if audioCodec != nil {
        log.Printf("Track %d: AAC decoder initialized", track.Header.TrackID)
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення повного MP4 файлу з метаданими

```go
// CreateMP4File — генерація валідного MP4 файлу з метаданими
func CreateMP4File(filename string, config *FileConfig) error {
    f, err := os.Create(filename)
    if err != nil {
        return fmt.Errorf("create file: %w", err)
    }
    defer f.Close()
    
    // 1. Створення ftyp атому
    ftyp := &fmp4io.FileType{
        MajorBrand:   fmp4io.StringToTag("iso6"),
        MinorVersion: 1,
        CompatibleBrands: []uint32{
            fmp4io.StringToTag("iso6"),
            fmp4io.StringToTag("mp41"),
        },
    }
    ftypBytes := make([]byte, ftyp.Len())
    ftyp.Marshal(ftypBytes)
    f.Write(ftypBytes)
    
    // 2. Створення moov атому
    now := time.Now()
    moov := &fmp4io.Movie{
        Header: &fmp4io.MovieHeader{
            Version:         0,
            Flags:           0,
            CreateTime:      now,
            ModifyTime:      now,
            TimeScale:       90000,  // 90kHz для відео
            Duration:        0,      // буде оновлено при записі
            PreferredRate:   1.0,
            PreferredVolume: 1.0,
            Matrix:          [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
            NextTrackID:     2,  // після одного треку
        },
        Tracks: []*fmp4io.Track{
            // Відео трек
            &fmp4io.Track{
                Header: &fmp4io.TrackHeader{
                    Version:     0,
                    Flags:       0x0003,  // enabled | in-movie
                    CreateTime:  now,
                    ModifyTime:  now,
                    TrackID:     1,
                    Duration:    0,  // буде оновлено
                    Layer:       0,
                    AlternateGroup: 0,
                    Volume:      1.0,
                    Matrix:      [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
                    TrackWidth:  float64(config.Width),
                    TrackHeight: float64(config.Height),
                },
                Media: createVideoMedia(config),  // helper function
            },
        },
    }
    
    // 3. Серіалізація moov
    moovBytes := make([]byte, moov.Len())
    moov.Marshal(moovBytes)
    f.Write(moovBytes)
    
    // 4. Запис медіа-даних (mdat атом) — спрощено
    // ... запис реальних даних відео/аудіо ...
    
    return nil
}

type FileConfig struct {
    Width, Height int
    Duration      time.Duration
    // ... інші параметри ...
}

// createVideoMedia — helper для створення Media для відео треку
func createVideoMedia(config *FileConfig) *fmp4io.Media {
    // ... реалізація створення mdia атому ...
    // Див. попередній приклад у розділі про Media
    return &fmp4io.Media{
        // ... заповнені поля ...
    }
}
```

### 🔧 Приклад: Парсинг метаданих для аналізу файлу

```go
// AnalyzeMP4Metadata — витягування повної інформації про файл
func AnalyzeMP4Metadata(filename string) (*FileAnalysis, error) {
    f, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    
    // Парсинг атомів
    atoms, err := fmp4io.ReadFileAtoms(f)
    if err != nil {
        return nil, fmt.Errorf("parse atoms: %w", err)
    }
    
    // Пошук moov атому
    var moov *fmp4io.Movie
    for _, atom := range atoms {
        if atom.Tag() == fmp4io.MOOV {
            moov, _ = atom.(*fmp4io.Movie)
            break
        }
    }
    if moov == nil {
        return nil, fmt.Errorf("moov atom not found")
    }
    
    analysis := &FileAnalysis{
        Filename: filename,
    }
    
    // Аналіз MovieHeader
    if moov.Header != nil {
        header := moov.Header
        analysis.TimeScale = int64(header.TimeScale)
        analysis.Duration = time.Duration(header.Duration) * time.Second / time.Duration(header.TimeScale)
        analysis.CreateTime = header.CreateTime
        analysis.ModifyTime = header.ModifyTime
    }
    
    // Аналіз треків
    for _, track := range moov.Tracks {
        trackAnalysis, err := AnalyzeTrackMetadata(track)
        if err != nil {
            log.Printf("warning: analyze track: %v", err)
            continue
        }
        analysis.Tracks = append(analysis.Tracks, trackAnalysis)
    }
    
    return analysis, nil
}

type FileAnalysis struct {
    Filename    string
    TimeScale   int64
    Duration    time.Duration
    CreateTime  time.Time
    ModifyTime  time.Time
    Tracks      []*TrackAnalysis
}

// Використання:
analysis, err := AnalyzeMP4Metadata("video.mp4")
if err != nil {
    log.Printf("error: %v", err)
    return
}

log.Printf("File: %s", analysis.Filename)
log.Printf("Duration: %v", analysis.Duration)
log.Printf("Tracks: %d", len(analysis.Tracks))
for _, track := range analysis.Tracks {
    log.Printf("  Track %d: %s, %dx%d, codec=%s", 
        track.TrackID, track.Type, track.Width, track.Height, track.Codec)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при читанні 3-байтових полів** | Доступ за межами буфера у Unmarshal | Додайте перевірку `if len(b) < n+3` перед `pio.U24BE()` |
| **Невірний розрахунок тривалості** | Duration не співпадає з реальним часом | Переконайтеся що використовуєте правильний `TimeScale` для конвертації |
| **Некоректна конвертація fixed-point** | Роздільна здатність = 125829120 замість 1920 | Конвертуйте з 16.16 формату: `int(value)` або `int(value >> 16)` |
| **Відсутній moov атом** | Неможливо відтворити файл | Перевіряйте наявність `moov` перед використанням метаданих |
| **Невірний TrackID** | Треки не ідентифікуються коректно | Переконайтеся що TrackID унікальний та > 0 |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування конвертацій часу:

```go
var durationCache = sync.Map{}  // map[uint64]time.Duration

func CachedGetDuration(duration, timeScale uint32) time.Duration {
    key := uint64(duration)<<32 | uint64(timeScale)
    
    if cached, ok := durationCache.Load(key); ok {
        return cached.(time.Duration)
    }
    
    result := time.Duration(duration) * time.Second / time.Duration(timeScale)
    durationCache.Store(key, result)
    return result
}
```

### 2. Попередня аллокація буферів для Marshal:

```go
// PreallocateMovieBuffer — виділення місця для серіалізації заздалегідь
func PreallocateMovieBuffer(movie *fmp4io.Movie) []byte {
    estimatedSize := movie.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateMovieBuffer(moov)
n := moov.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type MovieMetrics struct {
    MoviesParsed prometheus.CounterVec
    ParseLatency prometheus.HistogramVec
    TrackCount   prometheus.HistogramVec
    ParseErrors  prometheus.CounterVec
}

func (m *MovieMetrics) RecordParse(trackCount int, duration time.Duration, err error) {
    m.MoviesParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    m.TrackCount.Observe(float64(trackCount))
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання Movie атомів

```go
// ✅ 1. Перевірка меж буфера перед читанням 3-байтових полів
if len(b) < n+3 {
    err = parseErr("Flags", n+offset, err)
    return
}
a.Flags = pio.U24BE(b[n:])
n += 3

// ✅ 2. Валідація TimeScale перед діленням
if header.TimeScale == 0 {
    return fmt.Errorf("invalid TimeScale: cannot be zero")
}
duration := time.Duration(header.Duration) * time.Second / time.Duration(header.TimeScale)

// ✅ 3. Конвертація fixed-point 16.16 для роздільної здатності
width := int(trackHeader.TrackWidth)  // або (int)(trackHeader.TrackWidth) для точності
height := int(trackHeader.TrackHeight)

// ✅ 4. Перевірка наявності moov перед використанням
if moov == nil {
    return fmt.Errorf("moov atom not found in file")
}

// ✅ 5. Фільтрація активних треків за прапорцями
for _, track := range moov.Tracks {
    if track.Header != nil && (track.Header.Flags&0x0003) == 0x0003 {
        // обробка активного треку
    }
}

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Parsed movie: timeScale=%d, duration=%v, tracks=%d", 
    header.TimeScale, duration, len(moov.Tracks))

// ✅ 7. Метрики для моніторингу
metrics.RecordParse(len(moov.Tracks), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [MP4 Matrix Transformation](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25688) — Apple documentation про матриці
- 🧪 [Fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) — теорія формату 16.16
- 💻 [Go time Package Documentation](https://pkg.go.dev/time) — робота з часом у Go
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед `pio.U24BE()`** — уникнення панік при пошкоджених файлах.
> 2. **Валідуйте `TimeScale != 0` перед діленням** — уникнення панік при некоректних метаданих.
> 3. **Коректно конвертуйте fixed-point 16.16 для роздільної здатності** — уникнення невірних значень розміру відео.
> 4. **Перевіряйте наявність `moov` атому** — без нього неможливо відтворити файл.
> 5. **Фільтруйте активні треки за прапорцями** — уникнення обробки неактивних або прихованих треків.

Потрібен приклад реалізації повного циклу створення/парсингу MP4 файлу з підтримкою кількох треків та fMP4 розширень, або інтеграція `fmp4io.Movie` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀