# 📦 Глибокий розбір: `fmp4.Track()` та `MovieHeader()` — Генерація метаданих для fMP4

Цей файл — **реалізація генерації init segment (init.mp4) для fMP4**: функція `Track()` створює TRAK атом для окремого кодека, а `MovieHeader()` об'єднує треки у повний moov атом з ftyp. Це критично для ініціалізації клієнтських декодерів перед початком streaming.

---

## 🗺️ Архітектурна схема генерації init segment

```
┌────────────────────────────────────────┐
│ 📦 fmp4 — Init Segment Generation     │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові функції:                    │
│  • Track() — створення TRAK атому     │
│  • MovieHeader() — генерація ftyp+moov│
│  • FragmentHeader() — styp для сегментів│
│                                         │
│  🔄 Потік даних:                        │
│  av.CodecData → Track()               │
│  → fmp4io.Track → MovieHeader()       │
│  → init.mp4 binary → Client           │
│                                         │
│  📡 Підтримувані кодеки:                │
│  • H.264 (avc1) — відео                │
│  • AAC (mp4a) — аудіо                  │
│  • Opus (Opus) — аудіо                 │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Track() — створення TRAK атому для кодека

### 🔧 Основна логіка:

```go
func (f *TrackFragmenter) Track() (*fmp4io.Track, error) {
    // 1. Ініціалізація SampleTable (stbl) з порожніми таблицями
    sample := &fmp4io.SampleTable{
        SampleDesc:    &fmp4io.SampleDesc{},
        TimeToSample:  &fmp4io.TimeToSample{},
        SampleToChunk: &fmp4io.SampleToChunk{},
        SampleSize:    &fmp4io.SampleSize{},
        ChunkOffset:   &fmp4io.ChunkOffset{},
    }
    
    // 2. Обробка за типом кодека
    switch cd := f.codecData.(type) {
    case h264parser.CodecData:
        // H.264 відео: налаштування AVC1Desc
        f.timeScale = 90000  // стандартна шкала для відео
        cd.RecordInfo.LengthSizeMinusOne = 3  // 4-байтові довжини NALU
        conf := make([]byte, cd.RecordInfo.Len())
        cd.RecordInfo.Marshal(conf)  // серіалізація AVCDecoderConfigurationRecord
        
        sample.SampleDesc.AVC1Desc = &fmp4io.AVC1Desc{
            DataRefIdx:           1,
            HorizontalResolution: 72,  // DPI
            VorizontalResolution: 72,  // ⚠️ Опечатка: має бути VerticalResolution
            Width:                int16(cd.Width()),
            Height:               int16(cd.Height()),
            FrameCount:           1,
            Depth:                24,  // 24-бітний колір
            ColorTableId:         -1,  // немає таблиці кольорів
            Conf:                 &fmp4io.AVC1Conf{Data: conf},  // avcC атом
        }
        
    case aacparser.CodecData:
        // AAC аудіо: налаштування MP4ADesc
        f.timeScale = 48000  // стандартна шкала для аудіо
        dc, err := esio.DecoderConfigFromCodecData(cd)
        if err != nil {
            return nil, fmt.Errorf("decoding AAC configuration: %w", err)
        }
        sample.SampleDesc.MP4ADesc = &fmp4io.MP4ADesc{
            DataRefIdx:       1,
            NumberOfChannels: int16(cd.ChannelLayout().Count()),
            SampleSize:       16,  // 16-бітне аудіо
            SampleRate:       float64(cd.SampleRate()),
            Conf: &fmp4io.ElemStreamDesc{
                StreamDescriptor: &esio.StreamDescriptor{
                    ESID:          uint16(f.trackID),
                    DecoderConfig: dc,  // MPEG-4 Stream Descriptor
                    SLConfig:      &esio.SLConfigDescriptor{Predefined: esio.SLConfigMP4},
                },
            },
        }
        
    case *opusparser.CodecData:
        // Opus аудіо: налаштування OpusSampleEntry
        f.timeScale = 48000
        sample.SampleDesc.OpusDesc = &fmp4io.OpusSampleEntry{
            DataRefIdx:       1,
            NumberOfChannels: uint16(cd.ChannelLayout().Count()),
            SampleSize:       16,
            SampleRate:       float64(cd.SampleRate()),
            Conf: &fmp4io.OpusSpecificConfiguration{
                OutputChannelCount: uint8(cd.ChannelLayout().Count()),
                PreSkip:            3840,  // 80ms @ 48kHz = 3840 семплів
            },
        }
        
    default:
        return nil, fmt.Errorf("mp4: codec type=%v is not supported", f.codecData.Type())
    }
    
    // 3. Створення Track атому з метаданими
    trackAtom := &fmp4io.Track{
        Header: &fmp4io.TrackHeader{
            Flags:   0x0003,  // Track enabled | Track in movie
            Matrix:  [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},  // identity matrix
            TrackID: f.trackID,
        },
        Media: &fmp4io.Media{
            Header: &fmp4io.MediaHeader{
                Language:  21956,  // 'und' = undefined (ISO 639-2/T packed format)
                TimeScale: f.timeScale,
            },
            Info: &fmp4io.MediaInfo{
                Sample: sample,
                Data: &fmp4io.DataInfo{
                    Refer: &fmp4io.DataRefer{
                        Url: &fmp4io.DataReferUrl{
                            Flags: 0x000001,  // Self reference (дані у тому ж файлі)
                        },
                    },
                },
            },
        },
    }
    
    // 4. Додаткові налаштування для відео/аудіо
    if f.codecData.Type().IsVideo() {
        vc := f.codecData.(av.VideoCodecData)
        trackAtom.Media.Handler = &fmp4io.HandlerRefer{
            Type: fmp4io.VideoHandler,  // 'vide'
            Name: "VideoHandler",
        }
        trackAtom.Media.Info.Video = &fmp4io.VideoMediaInfo{
            Flags: 0x000001,  // graphics mode present
        }
        trackAtom.Header.TrackWidth = float64(vc.Width())
        trackAtom.Header.TrackHeight = float64(vc.Height())
    } else {
        trackAtom.Header.Volume = 1  // максимальна гучність (8.8 fixed-point)
        trackAtom.Header.AlternateGroup = 1  // група для вибору мови
        trackAtom.Media.Handler = &fmp4io.HandlerRefer{
            Type: fmp4io.SoundHandler,  // 'soun'
            Name: "SoundHandler",
        }
        trackAtom.Media.Info.Sound = &fmp4io.SoundMediaInfo{}
    }
    
    return trackAtom, nil
}
```

### 🔍 Чому `LengthSizeMinusOne = 3` для H.264?

```
AVCDecoderConfigurationRecord містить поле lengthSizeMinusOne:
• Значення 0 = 1-байтові довжини NALU
• Значення 1 = 2-байтові довжини
• Значення 3 = 4-байтові довжини ⭐

Чому 4 байти?
• Більшість MP4 muxer'ів використовують 4-байтові довжини для сумісності
• Дозволяє розміри до 4GB для одного NALU (на практиці не потрібно, але стандарт)
• Сумісність з існуючими плеєрами та інструментами

Приклад формату:
  [4-byte length][NALU data][4-byte length][NALU data]...
  • 0x0000001C = 28 байт (наступний NALU має 28 байт)
  • 0x0000000A = 10 байт (наступний NALU має 10 байт)
```

### ⚠️ Критична проблема: опечатка `VorizontalResolution`

```go
VorizontalResolution: 72,  // ← Має бути VerticalResolution!
```

**Наслідки**:
• Деякі плеєри можуть ігнорувати це поле або показувати неправильне співвідношення сторін
• Старі версії QuickTime можуть відмовитися відтворювати файл

**✅ Виправлення**:
```go
VerticalResolution: 72,  // Правильна назва поля
```

### ✅ Ваш use-case**: розширення підтримки кодеків

```go
// Додавання підтримки HEVC (H.265) у Track()
case hevcparser.CodecData:  // припустимо такий тип існує
    f.timeScale = 90000
    // HEVC використовує hvcC замість avcC
    conf := make([]byte, cd.RecordInfo.Len())
    cd.RecordInfo.Marshal(conf)
    
    sample.SampleDesc.HEVC1Desc = &fmp4io.HEVC1Desc{  // припустимо такий тип існує
        DataRefIdx:           1,
        Width:                int16(cd.Width()),
        Height:               int16(cd.Height()),
        Conf:                 &fmp4io.HEVC1Conf{Data: conf},  // hvcC атом
        // ... інші поля ...
    }
```

---

## 🔑 2. MovieHeader() — генерація init.mp4

### 🔧 Основна логіка:

```go
func MovieHeader(tracks []*fmp4io.Track) ([]byte, error) {
    // 1. Створення ftyp атому (File Type Box)
    ftyp := fmp4io.FileType{
        MajorBrand: 0x69736f36,  // 'iso6' = ISO Base Media File Format v6
        CompatibleBrands: []uint32{
            0x69736f35,  // 'iso5' = зворотня сумісність з v5
            0x6d703431,  // 'mp41' = MPEG-4 Part 14 v1
        },
    }
    
    // 2. Створення moov атому (Movie Box)
    moov := &fmp4io.Movie{
        Header: &fmp4io.MovieHeader{
            PreferredRate:   1,      // нормальна швидкість відтворення (16.16 fixed-point)
            PreferredVolume: 1,      // максимальна гучність (8.8 fixed-point)
            Matrix:          [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},  // identity
            TimeScale:       1000,   // глобальна шкала часу: 1000 ticks = 1 секунда
        },
        Tracks:      tracks,          // масив треків згенерованих через Track()
        MovieExtend: &fmp4io.MovieExtend{},  // mvex для fMP4 streaming
    }
    
    // 3. Налаштування MovieExtend для fMP4
    for _, track := range tracks {
        // Оновлення NextTrackID для унікальності
        if track.Header.TrackID >= moov.Header.NextTrackID {
            moov.Header.NextTrackID = track.Header.TrackID + 1
        }
        
        // Додавання TrackExtend для кожного треку
        moov.MovieExtend.Tracks = append(moov.MovieExtend.Tracks,
            &fmp4io.TrackExtend{
                TrackID: track.Header.TrackID,
                DefaultSampleDescIdx: 1,  // перший запис у SampleDesc
            })
    }
    
    // 4. Серіалізація у binary формат
    fhdr := make([]byte, ftyp.Len()+moov.Len())  // алокація буфера
    n := ftyp.Marshal(fhdr)                       // запис ftyp
    moov.Marshal(fhdr[n:])                        // запис moov після ftyp
    
    return fhdr, nil
}
```

### 🔍 Чому `TimeScale = 1000` для moov?

```
Глобальна шкала часу у MovieHeader:
• 1000 ticks = 1 секунда (мілісекундна точність)
• Використовується для загальних таймінгів файлу (duration, тощо)

Але кожен трек має свій TimeScale:
• Відео: 90000 ticks/second (сумісність з MPEG-TS)
• Аудіо: 48000 ticks/second (стандарт для аудіо)

Конвертація між шкалами:
  • Клієнт використовує track.TimeScale для розрахунку таймінгів семплів
  • Movie.TimeScale використовується для загальної тривалості файлу
  • Конвертація: track_ticks = movie_ticks * track_scale / movie_scale

Приклад:
  • Movie duration = 4000 ticks @ 1000 Hz = 4 секунди
  • Відео трек: 4 секунди * 90000 = 360000 ticks
  • Аудіо трек: 4 секунди * 48000 = 192000 ticks
```

### ⚠️ Критична проблема: відсутність обробки помилок у Marshal

```go
// У поточному коді:
n := ftyp.Marshal(fhdr)           // ← ігнорування поверненого n?
moov.Marshal(fhdr[n:])            // ← ігнорування помилок!

Проблема:
• Marshal() може повернути помилку (напр. при некоректних даних)
• Ігнорування помилок може призвести до пошкодженого init segment
• Клієнт не зможе ініціалізувати декодер

✅ Виправлення: обробка помилок
    n, err := ftyp.Marshal(fhdr)
    if err != nil {
        return nil, fmt.Errorf("marshal ftyp: %w", err)
    }
    _, err = moov.Marshal(fhdr[n:])
    if err != nil {
        return nil, fmt.Errorf("marshal moov: %w", err)
    }
```

### ✅ Ваш use-case**: валідація init segment перед відправкою

```go
// ValidateInitSegment — перевірка коректності згенерованого init.mp4
func ValidateInitSegment(data []byte) error {
    if len(data) < 16 {
        return fmt.Errorf("init segment too short: %d bytes", len(data))
    }
    
    // Перевірка ftyp атому
    if string(data[4:8]) != "ftyp" {
        return fmt.Errorf("missing ftyp atom at offset 4")
    }
    
    // Перевірка major brand
    majorBrand := string(data[8:12])
    validBrands := map[string]bool{
        "iso6": true, "iso5": true, "mp41": true, "mp42": true,
    }
    if !validBrands[majorBrand] {
        log.Printf("warning: unusual major brand: %s", majorBrand)
    }
    
    // Перевірка наявності moov атому (спрощено)
    moovOffset := bytes.Index(data, []byte("moov"))
    if moovOffset < 0 {
        return fmt.Errorf("missing moov atom")
    }
    
    // Перевірка що moov містить треки
    moovData := data[moovOffset:]
    if !bytes.Contains(moovData, []byte("trak")) {
        return fmt.Errorf("moov atom missing trak children")
    }
    
    return nil
}

// Використання:
initBytes, err := MovieHeader(tracks)
if err != nil {
    return fmt.Errorf("generate init: %w", err)
}
if err := ValidateInitSegment(initBytes); err != nil {
    log.Printf("warning: invalid init segment: %v", err)
    // Можна спробувати відновитися або повернути помилку
}
```

---

## 🔑 3. FragmentHeader() — генерація styp для сегментів

### 🔧 Реалізація:

```go
func FragmentHeader() []byte {
    styp := fmp4io.SegmentType{
        MajorBrand:       0x6d736468,           // 'msdh' = Microsoft Smooth Streaming
        CompatibleBrands: []uint32{0x6d736978}, // 'msix' = MSS extension
    }
    shdr := make([]byte, styp.Len())
    styp.Marshal(shdr)
    return shdr
}
```

### 🔍 Призначення styp атому:

```
styp (Segment Type) — аналог ftyp для окремих сегментів:

• Використовується на початку кожного fMP4 сегменту (не тільки init)
• Дозволяє кожному сегменту бути самодостатнім (не потребує init.mp4)
• Критично для low-latency streaming та live broadcast

Бренди у прикладі:
• 'msdh' = Microsoft Smooth Streaming (історичний бренд)
• 'msix' = MSS extension для fMP4

Сучасна практика:
• Для HLS fMP4: використовувати 'iso6' + 'dash'
• Для DASH: використовувати 'iso6' + 'dash' + 'cmfc' (CMAF)

Приклад оновлення для HLS:
    styp := fmp4io.SegmentType{
        MajorBrand:       0x69736f36,  // 'iso6'
        CompatibleBrands: []uint32{
            0x69736f36,  // 'iso6' — базова сумісність
            0x64617368,  // 'dash' — підтримка DASH/HLS
        },
    }
```

### ✅ Ваш use-case**: генерація styp для HLS fMP4

```go
// HLSFragmentHeader — генерація styp для HLS fMP4 сегментів
func HLSFragmentHeader() []byte {
    styp := fmp4io.SegmentType{
        MajorBrand: 0x69736f36,  // 'iso6'
        CompatibleBrands: []uint32{
            0x69736f36,  // 'iso6' — базова сумісність
            0x64617368,  // 'dash' — підтримка DASH/HLS
            0x636d6663,  // 'cmfc' — CMAF для low-latency
        },
    }
    shdr := make([]byte, styp.Len())
    styp.Marshal(shdr)
    return shdr
}

// Використання у marshalFragment():
if initial {
    shdrOnce.Do(func() {
        shdr = HLSFragmentHeader()  // заміна FragmentHeader()
    })
    // ... використання shdr ...
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення init segment для multi-codec streaming

```go
// CreateMultiCodecInit — генерація init.mp4 для відео + аудіо
func CreateMultiCodecInit(videoCodec av.CodecData, audioCodec av.CodecData) ([]byte, error) {
    // 1. Створення фрагментаторів для кожного кодека
    videoFrag, err := NewTrack(videoCodec)
    if err != nil {
        return nil, fmt.Errorf("video track: %w", err)
    }
    
    audioFrag, err := NewTrack(audioCodec)
    if err != nil {
        return nil, fmt.Errorf("audio track: %w", err)
    }
    
    // 2. Генерація TRAK атомів
    videoTrack, err := videoFrag.Track()
    if err != nil {
        return nil, fmt.Errorf("generate video track: %w", err)
    }
    
    audioTrack, err := audioFrag.Track()
    if err != nil {
        return nil, fmt.Errorf("generate audio track: %w", err)
    }
    
    // 3. Генерація init segment
    initBytes, err := MovieHeader([]*fmp4io.Track{videoTrack, audioTrack})
    if err != nil {
        return nil, fmt.Errorf("marshal init: %w", err)
    }
    
    // 4. Валідація перед відправкою
    if err := ValidateInitSegment(initBytes); err != nil {
        return nil, fmt.Errorf("invalid init segment: %w", err)
    }
    
    return initBytes, nil
}

// Використання:
initBytes, err := CreateMultiCodecInit(h264Codec, aacCodec)
if err != nil {
    log.Printf("error creating init: %v", err)
    return
}

// Відправка клієнту
conn.Write(initBytes)
```

### 🔧 Приклад: Динамічне додавання треків (напр. субтитри)

```go
// DynamicTrackAdder — додавання нових треків до існуючого init segment
type DynamicTrackAdder struct {
    existingTracks []*fmp4io.Track
    nextTrackID    uint32
}

func NewDynamicTrackAdder(initBytes []byte) (*DynamicTrackAdder, error) {
    // Парсинг існуючого init segment для отримання треків
    // (спрощено — припускаємо що треки вже відомі)
    return &DynamicTrackAdder{
        existingTracks: nil,  // заповнити при парсингу
        nextTrackID:    1,    // або макс. існуючий ID + 1
    }, nil
}

func (d *DynamicTrackAdder) AddSubtitleTrack(codec av.CodecData) (*fmp4io.Track, error) {
    // Створення фрагментатора для субтитрів
    frag, err := NewTrack(codec)
    if err != nil {
        return nil, err
    }
    
    // Налаштування унікального TrackID
    frag.trackID = d.nextTrackID
    d.nextTrackID++
    
    // Генерація TRAK атому
    track, err := frag.Track()
    if err != nil {
        return nil, err
    }
    
    // Додавання до списку
    d.existingTracks = append(d.existingTracks, track)
    return track, nil
}

func (d *DynamicTrackAdder) RegenerateInit() ([]byte, error) {
    // Перегенерація init.mp4 з усіма треками
    return MovieHeader(d.existingTracks)
}

// Використання:
adder, _ := NewDynamicTrackAdder(initBytes)
subTrack, _ := adder.AddSubtitleTrack(webvttCodec)
newInit, _ := adder.RegenerateInit()
// Відправка newInit клієнтам для оновлення
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Опечатка `VorizontalResolution`** | Невірне співвідношення сторін у старих плеєрах | Виправити на `VerticalResolution` у всіх місцях |
| **Ігнорування помилок у Marshal** | Пошкоджені init segment без явних помилок | Додати обробку помилок для всіх викликів Marshal() |
| **Некоректний TimeScale для треку** | Розсинхронізація аудіо/відео | Переконайтеся що video=90000, audio=48000, а не навпаки |
| **Відсутній MovieExtend для fMP4** | Неможливість фрагментації у streaming | Переконайтеся що `moov.MovieExtend` ініціалізовано та заповнено |
| **Неунікальні TrackID** | Конфлікти при обробці треків на клієнті | Використовуйте інкрементальний `NextTrackID` при додаванні треків |

---

## ⚡ Оптимізації для high-performance генерації

### 1. Кешування серіалізованих Track атомів:

```go
var trackCache = sync.Map{}  // map[codecHash][]byte

func GetCachedTrack(codec av.CodecData, trackID uint32) (*fmp4io.Track, error) {
    hash := codecHash(codec)  // helper function
    if cached, ok := trackCache.Load(hash); ok {
        // Десеріалізація кешованого Track (спрощено)
        return deserializeTrack(cached.([]byte), trackID)
    }
    
    // Генерація нового
    frag, _ := NewTrack(codec)
    frag.trackID = trackID
    track, err := frag.Track()
    if err != nil {
        return nil, err
    }
    
    // Кешування
    serialized, _ := serializeTrack(track)  // helper function
    trackCache.Store(hash, serialized)
    
    return track, nil
}
```

### 2. Попередня аллокація буферів для MovieHeader:

```go
// PreallocateInitBuffer — оцінка розміру init segment перед алокацією
func PreallocateInitBuffer(tracks []*fmp4io.Track) []byte {
    // Оцінка розміру: ftyp (~24 байт) + moov header (~100 байт) + треки
    size := 124  // базовий розмір
    for _, track := range tracks {
        size += estimateTrackSize(track)  // helper function
    }
    
    return make([]byte, 0, size)  // cap = size для уникнення realloc
}

// Використання:
buf := PreallocateInitBuffer(tracks)
// ... використання buf для серіалізації ...
```

### 3. Моніторинг продуктивності генерації init segment:

```go
type InitGenMetrics struct {
    TracksGenerated prometheus.CounterVec
    GenLatency      prometheus.HistogramVec
    InitSizes       prometheus.HistogramVec
    GenErrors       prometheus.CounterVec
}

func (m *InitGenMetrics) RecordGeneration(trackCount int, duration time.Duration, size int, err error) {
    m.TracksGenerated.WithLabelValues(fmt.Sprintf("count_%d", trackCount)).Inc()
    m.GenLatency.Observe(duration.Seconds())
    m.InitSizes.Observe(float64(size))
    if err != nil {
        m.GenErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання Track/MovieHeader

```go
// ✅ 1. Виправлення опечатки у назві поля
type AVC1Desc struct {
    // ...
    VerticalResolution float64  // ✅ Правильна назва
    // VorizontalResolution float64  // ❌ Видалити або задепрекейтити
}

// ✅ 2. Обробка помилок у Marshal
n, err := ftyp.Marshal(fhdr)
if err != nil {
    return nil, fmt.Errorf("marshal ftyp: %w", err)
}
_, err = moov.Marshal(fhdr[n:])
if err != nil {
    return nil, fmt.Errorf("marshal moov: %w", err)
}

// ✅ 3. Валідація TimeScale для кожного треку
if track.Media.Header.TimeScale == 0 {
    return fmt.Errorf("track %d: invalid TimeScale", track.Header.TrackID)
}

// ✅ 4. Унікальність TrackID
usedIDs := make(map[uint32]bool)
for _, track := range tracks {
    if usedIDs[track.Header.TrackID] {
        return fmt.Errorf("duplicate TrackID: %d", track.Header.TrackID)
    }
    usedIDs[track.Header.TrackID] = true
}

// ✅ 5. Наявність MovieExtend для fMP4
if moov.MovieExtend == nil {
    return fmt.Errorf("missing MovieExtend for fMP4 streaming")
}

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Generated init: %d tracks, %d bytes, brands=%v", 
    len(tracks), len(fhdr), ftyp.CompatibleBrands)

// ✅ 7. Метрики для моніторингу
metrics.RecordGeneration(len(tracks), time.Since(start), len(fhdr), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [MP4 Brand Registry](https://mp4ra.org/#/brands) — офіційний реєстр брендів для ftyp/styp
- 📄 [HLS fMP4 Specification](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 📄 [CMAF Specification](https://www.iso.org/standard/74428.html) — Common Media Application Format для low-latency
- 🧪 [Go encoding/binary Package](https://pkg.go.dev/encoding/binary) — робота з бінарними даними

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Виправте опечатку `VorizontalResolution` → `VerticalResolution`** — забезпечення сумісності з усіма плеєрами.
> 2. **Додайте обробку помилок для всіх викликів Marshal()** — уникнення пошкоджених init segment.
> 3. **Валідуйте унікальність TrackID** — уникнення конфліктів при обробці треків на клієнті.
> 4. **Використовуйте сучасні бренди для styp** (`iso6` + `dash`) замість застарілих `msdh`/`msix`.
> 5. **Моніторьте `GenErrors` метрику** — різке зростання може вказувати на проблеми з новими кодеками.

Потрібен приклад реалізації повного циклу генерації init segment з підтримкою динамічного додавання треків, або інтеграція `fmp4.MovieHeader` з вашим `mse.Muxer` для WebSocket стрімінгу? Готовий допомогти! 🚀