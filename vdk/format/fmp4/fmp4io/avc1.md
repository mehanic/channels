# 📦 Глибокий розбір: `fmp4io.AVC1Desc` — Опис відео кодека H.264 у fMP4

Цей файл — **реалізація атому `avc1` (AVC Configuration)** для опису відео потоку H.264 у форматі Fragmented MP4 (fMP4). Він містить метадані відео (роздільна здатність, частота кадрів, тощо) та посилання на `avcC` атом з конфігурацією декодера.

---

## 🗺️ Архітектурна схема AVC1Desc

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.AVC1Desc — H.264 Video Desc │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • AVC1Desc — опис відео кодека        │
│  • AVC1Conf (avcC) — конфігурація декодера│
│  • PixelAspect (pasp) — співвідношення пікселів│
│  • Unknowns — невідомі дочірні атоми   │
│                                         │
│  🔄 Ієрархія атомів:                    │
│  avc1 (AVC1Desc)                       │
│  ├─ avcC (AVC1Conf) — AVCDecoderConfig │
│  ├─ pasp (PixelAspect) — pixel ratio  │
│  └─ Unknowns — інші атоми              │
│                                         │
│  📡 Використання:                       │
│  • fMP4 init segment для H.264        │
│  • HLS/DASH streaming з H.264         │
│  • Ініціалізація декодера на клієнті  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. AVC1Desc — структура опису відео кодека

### 🔧 Поля та їх призначення:

```go
type AVC1Desc struct {
    // 🗂️ Базові параметри
    DataRefIdx           int16   // індекс посилання на дані (зазвичай 1)
    Version              int16   // версія формату (зазвичай 0)
    Revision             int16   // ревізія кодека
    Vendor               int32   // ідентифікатор вендора
    
    // 🎬 Якість відео (рідко використовується)
    TemporalQuality      int32   // тимчасова якість (0 = не вказано)
    SpatialQuality       int32   // просторова якість (0 = не вказано)
    
    // 📐 Роздільна здатність
    Width                int16   // ширина кадру у пікселях
    Height               int16   // висота кадру у пікселях
    
    // 🔍 Роздільна здатність відображення (fixed-point 16.16)
    HorizontalResolution float64 // DPI по горизонталі (зазвичай 72.0)
    VorizontalResolution float64 // ⚠️ Опечатка: має бути VerticalResolution
    
    // 🎞️ Параметри відео
    FrameCount           int16   // кількість кадрів у семплі (зазвичай 1)
    CompressorName       [32]byte // назва кодека (null-terminated string)
    Depth                int16   // бітова глибина (24 = true color)
    ColorTableId         int16   // ID таблиці кольорів (-1 = немає)
    
    // 🔗 Дочірні атоми
    Conf                 *AVC1Conf   // ⭐ avcC: AVCDecoderConfigurationRecord
    PixelAspect          *PixelAspect // ⭐ pasp: pixel aspect ratio
    Unknowns             []Atom      // невідомі атоми для сумісності
    
    AtomPos  // offset/size у файлі
}
```

### 🔍 Призначення критичних полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `Width/Height` | `int16` | Роздільна здатність відео у пікселях | `1920, 1080` для Full HD |
| `HorizontalResolution` | `float64` | DPI для відображення (fixed-point 16.16) | `72.0` = стандарт для відео |
| `Conf` | `*AVC1Conf` | **Критично**: AVCDecoderConfigurationRecord для ініціалізації декодера | Містить SPS/PPS для H.264 |
| `PixelAspect` | `*PixelAspect` | Співвідношення сторін пікселя (для анаморфного відео) | `1:1` для квадратних пікселів |

### ⚠️ Критична проблема: опечатка у назві поля

```go
VorizontalResolution float64  // ← Має бути VerticalResolution!
```

**Наслідки**: Деякі плеєри можуть ігнорувати це поле, але інші (напр. старі версії QuickTime) можуть відмовитися відтворювати файл або показувати неправильне співвідношення сторін.

**✅ Виправлення**:
```go
VerticalResolution float64  // Правильна назва поля
```

---

## 🔑 2. AVC1Conf (avcC) — конфігурація декодера H.264

### 🔧 Структура та призначення:

```go
type AVC1Conf struct {
    Data []byte  // ⭐ AVCDecoderConfigurationRecord
    AtomPos
}
```

### 🔍 Формат AVCDecoderConfigurationRecord:

```
Це критична структура для ініціалізації H.264 декодера:

struct AVCDecoderConfigurationRecord {
    configurationVersion: 1          // завжди 1
    AVCProfileIndication: 1          // profile_idc (66=Baseline, 77=Main, 100=High)
    profile_compatibility: 1         // сумісність профілю
    AVCLevelIndication: 1            // level_idc (30=3.0, 40=4.0, тощо)
    lengthSizeMinusOne: 1            // зазвичай 3 (означає 4-байтові довжини NALU)
    
    numOfSequenceParameterSets: 1    // зазвичай 1
    sequenceParameterSetLength: 2    // довжина SPS (big-endian)
    sequenceParameterSetNALUnit: var // дані SPS
    
    numOfPictureParameterSets: 1     // зазвичай 1
    pictureParameterSetLength: 2     // довжина PPS (big-endian)
    pictureParameterSetNALUnit: var  // дані PPS
}
```

### ✅ Ваш use-case**: ініціалізація H.264 декодера

```go
// InitH264DecoderFromAVCC — створення CodecData з avcC атому
func InitH264DecoderFromAVCC(avcc *fmp4io.AVC1Conf) (av.CodecData, error) {
    if avcc == nil || len(avcc.Data) == 0 {
        return nil, fmt.Errorf("empty AVC config")
    }
    
    // h264parser.NewCodecDataFromAVCDecoderConfRecord очікує сирий AVCDecoderConfigurationRecord
    return h264parser.NewCodecDataFromAVCDecoderConfRecord(avcc.Data)
}

// Використання:
track := findVideoTrack(moov)
avcc := track.GetAVC1Conf()  // helper function з fmp4io package
if avcc == nil {
    return fmt.Errorf("no AVC config found")
}
codecData, err := InitH264DecoderFromAVCC(avcc)
if err != nil {
    return fmt.Errorf("init H.264 decoder: %w", err)
}
```

---

## 🔑 3. PixelAspect (pasp) — співвідношення пікселів

### 🔧 Структура та призначення:

```go
type PixelAspect struct {
    HorizontalSpacing uint32  // горизонтальний інтервал пікселя
    VerticalSpacing   uint32  // вертикальний інтервал пікселя
    AtomPos
}
```

### 🔍 Розрахунок співвідношення сторін:

```
Pixel Aspect Ratio (PAR) = HorizontalSpacing / VerticalSpacing

Display Aspect Ratio (DAR) = (Width / Height) * PAR

Приклади:
• Квадратні пікселі: PAR = 1/1 → DAR = Width/Height
• Анаморфне відео 16:9 на 4:3 сенсорі: PAR = 16/9 / (720/480) = 1.185

У MP4: pasp атом визначає PAR для коректного відображення відео.
```

### ✅ Ваш use-case**: розрахунок DAR для відео

```go
// CalculateDisplayAspectRatio — розрахунок DAR з PAR
func CalculateDisplayAspectRatio(width, height int, pasp *fmp4io.PixelAspect) float64 {
    if pasp == nil || pasp.VerticalSpacing == 0 {
        // Квадратні пікселі за замовчуванням
        return float64(width) / float64(height)
    }
    
    par := float64(pasp.HorizontalSpacing) / float64(pasp.VerticalSpacing)
    dar := (float64(width) / float64(height)) * par
    return dar
}

// Використання:
avc1 := findAVC1Desc(track)
dar := CalculateDisplayAspectRatio(
    int(avc1.Width), 
    int(avc1.Height), 
    avc1.PixelAspect,
)
log.Printf("Video DAR: %.3f (%.0f:%.0f)", dar, simplifyRatio(dar))
```

---

## 🔑 4. Marshal/Unmarshal — серіалізація атому

### 🔧 Основна логіка Marshal:

```go
func (a AVC1Desc) marshal(b []byte) (n int) {
    // 1. Пропуск зарезервованих байт (6 байт)
    n += 6
    
    // 2. Запис основних полів
    pio.PutI16BE(b[n:], a.DataRefIdx); n += 2
    pio.PutI16BE(b[n:], a.Version); n += 2
    // ... інші поля ...
    
    // 3. Запис fixed-point значень (16.16 формат)
    PutFixed32(b[n:], a.HorizontalResolution); n += 4
    PutFixed32(b[n:], a.VorizontalResolution); n += 4  // ⚠️ Опечатка!
    
    // 4. Пропуск зарезервованих байт (4 байти)
    n += 4
    
    // 5. Запис строкових/масивних полів
    pio.PutI16BE(b[n:], a.FrameCount); n += 2
    copy(b[n:], a.CompressorName[:]); n += len(a.CompressorName[:])
    
    // 6. Рекурсивна серіалізація дочірніх атомів
    if a.Conf != nil {
        n += a.Conf.Marshal(b[n:])
    }
    if a.PixelAspect != nil {
        n += a.PixelAspect.Marshal(b[n:])
    }
    for _, atom := range a.Unknowns {
        n += atom.Marshal(b[n:])
    }
    return
}
```

### 🔧 Основна логіка Unmarshal:

```go
func (a *AVC1Desc) Unmarshal(b []byte, offset int) (n int, err error) {
    a.AtomPos.setPos(offset, len(b))
    n += 8  // пропуск заголовку атому (size+tag)
    n += 6  // пропуск зарезервованих байт
    
    // 1. Читання основних полів з перевіркою меж
    if len(b) < n+2 { err = parseErr("DataRefIdx", n+offset, err); return }
    a.DataRefIdx = pio.I16BE(b[n:]); n += 2
    // ... аналогічно для інших полів ...
    
    // 2. Читання fixed-point значень
    if len(b) < n+4 { err = parseErr("HorizontalResolution", n+offset, err); return }
    a.HorizontalResolution = GetFixed32(b[n:]); n += 4
    
    // 3. Пропуск зарезервованих байт
    n += 4
    
    // 4. Читання строкових/масивних полів
    if len(b) < n+len(a.CompressorName) { err = parseErr("CompressorName", n+offset, err); return }
    copy(a.CompressorName[:], b[n:]); n += len(a.CompressorName)
    
    // 5. Рекурсивний парсинг дочірніх атомів
    for n+8 < len(b) {
        tag := Tag(pio.U32BE(b[n+4:]))
        size := int(pio.U32BE(b[n:]))
        if len(b) < n+size { err = parseErr("TagSizeInvalid", n+offset, err); return }
        
        switch tag {
        case AVCC:  // avcC атом
            atom := &AVC1Conf{}
            if _, err = atom.Unmarshal(b[n:n+size], offset+n); err != nil { return }
            a.Conf = atom
        case PASP:  // pasp атом
            atom := &PixelAspect{}
            if _, err = atom.Unmarshal(b[n:n+size], offset+n); err != nil { return }
            a.PixelAspect = atom
        default:  // невідомі атоми
            atom := &Dummy{Tag_: tag, Data: b[n : n+size]}
            if _, err = atom.Unmarshal(b[n:n+size], offset+n); err != nil { return }
            a.Unknowns = append(a.Unknowns, atom)
        }
        n += size
    }
    return
}
```

### ⚠️ Критична проблема: відсутність перевірки меж для fixed-point полів

```
У поточному коді:
    a.HorizontalResolution = GetFixed32(b[n:])  // ← читає 4 байти
    n += 4

Проблема:
• Якщо len(b) < n+4 → GetFixed32 може читати за межами буфера
• Це може призвести до паніки або некоректних даних

✅ Виправлення: перевірка меж перед читанням
    if len(b) < n+4 {
        err = parseErr("HorizontalResolution", n+offset, err)
        return
    }
    a.HorizontalResolution = GetFixed32(b[n:])
    n += 4
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення fMP4 init segment для H.264

```go
// CreateH264InitSegment — генерація init segment для H.264 відео
func CreateH264InitSegment(width, height int, sps, pps []byte) ([]byte, error) {
    // 1. Створення AVCDecoderConfigurationRecord
    avcConfig, err := createAVCDecoderConfig(sps, pps)
    if err != nil {
        return nil, fmt.Errorf("create AVC config: %w", err)
    }
    
    // 2. Створення AVC1Conf атому
    avcc := &fmp4io.AVC1Conf{
        Data: avcConfig,
    }
    
    // 3. Створення AVC1Desc атому
    avc1 := &fmp4io.AVC1Desc{
        DataRefIdx:           1,
        Width:                int16(width),
        Height:               int16(height),
        HorizontalResolution: 72.0,  // стандартний DPI
        VerticalResolution:   72.0,  // ⚠️ Виправлена опечатка!
        FrameCount:           1,
        Depth:                24,
        ColorTableId:         -1,
        Conf:                 avcc,
        // PixelAspect опціонально
    }
    
    // 4. Створення SampleDesc з avc1
    stsd := &fmp4io.SampleDesc{
        AVC1Desc: avc1,
    }
    
    // 5. Створення повного moov атому (спрощено)
    moov := &fmp4io.Movie{
        Header: &fmp4io.MovieHeader{
            TimeScale: 90000,  // 90kHz для відео
            // ... інші параметри ...
        },
        Tracks: []*fmp4io.Track{
            &fmp4io.Track{
                Header: &fmp4io.TrackHeader{
                    TrackId:    1,
                    TrackWidth: float64(width),
                    TrackHeight: float64(height),
                },
                Media: &fmp4io.Media{
                    Header: &fmp4io.MediaHeader{
                        TimeScale: 90000,
                    },
                    Handler: &fmp4io.HandlerRefer{
                        SubType: [4]byte{'v', 'i', 'd', 'e'},
                    },
                    Info: &fmp4io.MediaInfo{
                        Sample: &fmp4io.SampleTable{
                            SampleDesc: stsd,
                        },
                    },
                },
            },
        },
    }
    
    // 6. Серіалізація moov + ftyp у один буфер
    ftyp := createFTYPAtom()  // helper function
    moovBytes := make([]byte, moov.Len())
    moov.Marshal(moovBytes)
    
    result := append(ftyp, moovBytes...)
    return result, nil
}

// createAVCDecoderConfig — створення AVCDecoderConfigurationRecord
func createAVCDecoderConfig(sps, pps []byte) ([]byte, error) {
    // Спрощена реалізація: припускаємо Baseline profile, level 3.0
    config := make([]byte, 0, 20+len(sps)+len(pps))
    
    config = append(config, 1)  // configurationVersion
    config = append(config, 66) // AVCProfileIndication = Baseline
    config = append(config, 0)  // profile_compatibility
    config = append(config, 30) // AVCLevelIndication = 3.0
    config = append(config, 0xFF|3) // lengthSizeMinusOne = 3 (4-byte NALU lengths)
    
    // SPS
    config = append(config, 1)  // numOfSequenceParameterSets
    config = append(config, byte(len(sps)>>8), byte(len(sps)&0xFF))
    config = append(config, sps...)
    
    // PPS
    config = append(config, 1)  // numOfPictureParameterSets
    config = append(config, byte(len(pps)>>8), byte(len(pps)&0xFF))
    config = append(config, pps...)
    
    return config, nil
}
```

### 🔧 Приклад: Парсинг H.264 метаданих з fMP4 файлу

```go
// ParseH264Metadata — витягування метаданих відео з fMP4 файлу
func ParseH264Metadata(filename string) (*VideoMetadata, error) {
    f, err := os.Open(filename)
    if err != nil { return nil, err }
    defer f.Close()
    
    // Парсинг атомів
    atoms, err := fmp4io.ReadFileAtoms(f)
    if err != nil { return nil, fmt.Errorf("parse atoms: %w", err) }
    
    // Пошук moov атому
    moov := fmp4io.FindChildrenByName(atoms[0], "moov")
    if moov == nil { return nil, fmt.Errorf("moov atom not found") }
    
    movie, ok := moov.(*fmp4io.Movie)
    if !ok { return nil, fmt.Errorf("unexpected moov type") }
    
    // Пошук відео треку
    var videoTrack *fmp4io.Track
    for _, track := range movie.Tracks {
        if track.Media != nil && track.Media.Handler != nil {
            if string(track.Media.Handler.SubType[:]) == "vide" {
                videoTrack = track
                break
            }
        }
    }
    if videoTrack == nil { return nil, fmt.Errorf("no video track found") }
    
    // Пошук avc1 атому у SampleDesc
    stsd := videoTrack.Media.Info.Sample.SampleDesc
    if stsd == nil || stsd.AVC1Desc == nil {
        return nil, fmt.Errorf("no AVC1Desc found")
    }
    
    avc1 := stsd.AVC1Desc
    
    // Витягування метаданих
    meta := &VideoMetadata{
        Width:      int(avc1.Width),
        Height:     int(avc1.Height),
        FrameCount: int(avc1.FrameCount),
        Depth:      int(avc1.Depth),
    }
    
    // Розрахунок DAR
    meta.DAR = CalculateDisplayAspectRatio(
        meta.Width, meta.Height, avc1.PixelAspect,
    )
    
    // Парсинг AVC config для отримання profile/level
    if avc1.Conf != nil && len(avc1.Conf.Data) >= 4 {
        meta.Profile = avc1.Conf.Data[1]   // AVCProfileIndication
        meta.Level = avc1.Conf.Data[3]     // AVCLevelIndication
    }
    
    return meta, nil
}

type VideoMetadata struct {
    Width      int
    Height     int
    FrameCount int
    Depth      int
    DAR        float64
    Profile    uint8  // H.264 profile id
    Level      uint8  // H.264 level id
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Опечатка VorizontalResolution** | Неправильне відображення відео у старих плеєрах | Виправити назву поля на `VerticalResolution` |
| **Паніка при читанні fixed-point полів** | Доступ за межами буфера у Unmarshal | Додати перевірку `if len(b) < n+4` перед `GetFixed32()` |
| **Невірний розрахунок DAR** | Відео розтягнуте або стиснуте | Перевірити наявність та коректність `PixelAspect` атому |
| **Відсутній avcC атом** | Неможливість ініціалізації декодера | Перевірити `if avc1.Conf != nil` перед використанням |
| **Некоректний CompressorName** | Назва кодека містить сміття | Переконатися що рядок null-terminated у межах 32 байт |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування серіалізованого avcC:

```go
type CachedAVC1Conf struct {
    *fmp4io.AVC1Conf
    serialized []byte
    dirty      bool
    mu         sync.RWMutex
}

func (c *CachedAVC1Conf) Marshal(b []byte) int {
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
    n := c.AVC1Conf.Marshal(b)
    c.serialized = make([]byte, n)
    copy(c.serialized, b[:n])
    c.dirty = false
    return n
}

func (c *CachedAVC1Conf) MarkDirty() {
    c.mu.Lock()
    c.dirty = true
    c.serialized = nil
    c.mu.Unlock()
}
```

### 2. Попередня аллокація буферів для Marshal:

```go
// PreallocateAVC1Buffer — виділення місця для серіалізації заздалегідь
func PreallocateAVC1Buffer(avc1 *fmp4io.AVC1Desc) []byte {
    estimatedSize := avc1.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateAVC1Buffer(avc1)
n := avc1.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type AVC1Metrics struct {
    AtomsParsed prometheus.CounterVec
    ParseLatency prometheus.HistogramVec
    ConfigSizes prometheus.HistogramVec
    ParseErrors prometheus.CounterVec
}

func (m *AVC1Metrics) RecordParse(size int, duration time.Duration, err error) {
    m.AtomsParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    m.ConfigSizes.Observe(float64(size))
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання AVC1Desc

```go
// ✅ 1. Виправлення опечатки у назві поля
type AVC1Desc struct {
    // ...
    VerticalResolution float64  // ✅ Правильна назва
    // VorizontalResolution float64  // ❌ Видалити або задепрекейтити
}

// ✅ 2. Перевірка меж буфера перед читанням fixed-point полів
if len(b) < n+4 {
    err = parseErr("HorizontalResolution", n+offset, err)
    return
}
a.HorizontalResolution = GetFixed32(b[n:])
n += 4

// ✅ 3. Валідація AVCDecoderConfigurationRecord перед використанням
if avc1.Conf == nil || len(avc1.Conf.Data) < 7 {
    return fmt.Errorf("invalid AVC config: too short")
}
if avc1.Conf.Data[0] != 1 {
    return fmt.Errorf("unsupported AVC config version: %d", avc1.Conf.Data[0])
}

// ✅ 4. Обробка PixelAspect для коректного розрахунку DAR
dar := CalculateDisplayAspectRatio(width, height, avc1.PixelAspect)
if dar < 1.0 || dar > 4.0 {
    log.Printf("warning: unusual DAR: %.3f", dar)
}

// ✅ 5. Логування з контекстом для дебагу
log.Printf("Parsed AVC1: %dx%d, profile=%d, level=%d, DAR=%.3f", 
    avc1.Width, avc1.Height, profile, level, dar)

// ✅ 6. Метрики для моніторингу
metrics.RecordParse(len(avc1.Conf.Data), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-15:2014 (AVC in MP4)](https://www.iso.org/standard/69445.html) — офіційний стандарт для H.264 у MP4
- 📄 [AVCDecoderConfigurationRecord Format](https://wiki.multimedia.cx/index.php/AVCDecoderConfigurationRecord) — детальний опис структури
- 📄 [Pixel Aspect Ratio in MP4](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25688) — Apple documentation
- 🧪 [Fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) — теорія формату 16.16
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Виправте опечатку `VorizontalResolution` → `VerticalResolution`** — забезпечення сумісності з усіма плеєрами.
> 2. **Завжди перевіряйте межі буфера перед `GetFixed32()`** — уникнення панік при пошкоджених файлах.
> 3. **Валідуйте `AVCDecoderConfigurationRecord` перед ініціалізацією декодера** — уникнення помилок при некоректних SPS/PPS.
> 4. **Використовуйте `PixelAspect` для коректного розрахунку DAR** — уникнення розтягнутого або стиснутого відео.
> 5. **Кешуйте серіалізовані avcC для повторного використання** — прискорення генерації init segment.

Потрібен приклад реалізації повного циклу створення/парсингу fMP4 init segment для H.264, або інтеграція `fmp4io.AVC1Desc` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀