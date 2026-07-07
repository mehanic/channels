# 📦 Глибокий розбір: `fmp4io.FileType` та `SegmentType` — Ідентифікація форматів MP4/fMP4

Цей файл — **реалізація атомів `ftyp` (File Type) та `styp` (Segment Type)** для ідентифікації форматів контейнерів MP4 та фрагментованого MP4 (fMP4). Ці атоми критичні для сумісності з плеєрами та розуміння структури файлу.

---

## 🗺️ Архітектурна схема ftyp/styp атомів

```
┌────────────────────────────────────────┐
│ 📦 ftyp/styp — Format Identification  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • FileType (ftyp) — ідентифікація файлу│
│  • SegmentType (styp) — ідентифікація сегменту│
│  • MajorBrand/MinorVersion — версія формату│
│  • CompatibleBrands — список сумісних форматів│
│                                         │
│  🔄 Формат атому:                       │
│  [size:4][tag:4][MajorBrand:4]         │
│  [MinorVersion:4][CompatibleBrand×N:4] │
│                                         │
│  📡 Використання:                       │
│  • ftyp — на початку файлу (init)      │
│  • styp — на початку кожного сегменту  │
│  • Визначення підтримки кодеків/функцій│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. FileType (ftyp) — ідентифікація файлу

### 🔧 Структура та призначення:

```go
type FileType struct {
    MajorBrand       uint32   // основний бренд формату (напр. 'iso6', 'mp42')
    MinorVersion     uint32   // версія формату (зазвичай 0 або 1)
    CompatibleBrands []uint32 // список сумісних брендів для зворотньої сумісності
    AtomPos                   // offset/size у файлі
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `MajorBrand` | `uint32` | Основний ідентифікатор формату контейнера | `'iso6'` = ISO Base Media v6, `'mp42'` = MP4 v2 |
| `MinorVersion` | `uint32` | Версія специфікації (для зворотньої сумісності) | `0` або `1` |
| `CompatibleBrands` | `[]uint32` | Список додаткових форматів, з якими сумісний файл | `['iso6', 'dash', 'msdh']` для DASH streaming |

### 🔍 Популярні бренди для streaming:

```
• 'iso2' / 'iso4' / 'iso5' / 'iso6' — ISO Base Media File Format версії
• 'mp41' / 'mp42' — MPEG-4 Part 14 (стандартний MP4)
• 'dash' — підтримка MPEG-DASH streaming ⭐
• 'msdh' / 'mshx' — підтримка MSS (Microsoft Smooth Streaming)
• 'iso5' + 'dash' — комбінація для HLS fMP4 ⭐
• 'qt  ' — QuickTime (застарілий, але ще зустрічається)
```

### ✅ Ваш use-case**: перевірка сумісності з DASH

```go
// IsDASHCompatible — перевірка чи файл підтримує DASH streaming
func IsDASHCompatible(ftyp *fmp4io.FileType) bool {
    dashTag := fmp4io.StringToTag("dash")
    for _, brand := range ftyp.CompatibleBrands {
        if brand == dashTag {
            return true
        }
    }
    return false
}

// Використання:
atoms, _ := fmp4io.ReadFileAtoms(file)
for _, atom := range atoms {
    if atom.Tag() == fmp4io.FTYP {
        ftyp, _ := atom.(*fmp4io.FileType)
        if IsDASHCompatible(ftyp) {
            log.Printf("File supports DASH streaming")
            // Можна використовувати DASH-специфічні функції
        }
        break
    }
}
```

---

## 🔑 2. SegmentType (styp) — ідентифікація сегменту

### 🔧 Структура та призначення:

```go
type SegmentType struct {
    MajorBrand       uint32   // основний бренд сегменту
    MinorVersion     uint32   // версія сегменту
    CompatibleBrands []uint32 // сумісні бренди для цього сегменту
    AtomPos
}
```

### 🔍 Відмінності від FileType:

```
ftyp (File Type):
• Знаходиться тільки на початку файлу
• Описує весь файл/потік
• Обов'язковий для валідного MP4 файлу

styp (Segment Type):
• Знаходиться на початку КОЖНОГО сегменту у fMP4
• Описує тільки цей конкретний сегмент
• Опціональний, але рекомендований для fMP4 streaming

Призначення styp:
• Дозволяє кожному сегменту бути самодостатнім
• Клієнт може почати відтворення з будь-якого сегменту
• Критичний для low-latency streaming та live broadcast
```

### ✅ Ваш use-case**: генерація styp для fMP4 сегменту

```go
// CreateSegmentType — створення styp атому для нового сегменту
func CreateSegmentType() *fmp4io.SegmentType {
    return &fmp4io.SegmentType{
        MajorBrand:   fmp4io.StringToTag("iso6"),  // базовий формат
        MinorVersion: 1,
        CompatibleBrands: []uint32{
            fmp4io.StringToTag("iso6"),  // ISO Base Media v6
            fmp4io.StringToTag("dash"),  // підтримка DASH
            fmp4io.StringToTag("msdh"),  // підтримка MSS
        },
    }
}

// Використання при створенні сегменту:
styp := CreateSegmentType()
stypBytes := make([]byte, styp.Len())
styp.Marshal(stypBytes)

// Запис styp на початку сегменту перед moof+mdat
writer.Write(stypBytes)
writer.Write(moofBytes)  // метадані фрагменту
writer.Write(mdatBytes)  // медіа-дані
```

---

## 🔑 3. Marshal/Unmarshal — серіалізація атомів

### 🔧 Основна логіка Marshal:

```go
func (f FileType) Marshal(b []byte) (n int) {
    // 1. Розрахунок загальної довжини
    l := 16 + 4*len(f.CompatibleBrands)  // 8 (header) + 8 (major+minor) + 4*N (brands)
    
    // 2. Запис заголовку атому
    pio.PutU32BE(b, uint32(l))           // size (4 байти)
    pio.PutU32BE(b[4:], uint32(FTYP))    // tag (4 байти)
    
    // 3. Запис основних полів
    pio.PutU32BE(b[8:], f.MajorBrand)    // MajorBrand (4 байти)
    pio.PutU32BE(b[12:], f.MinorVersion) // MinorVersion (4 байти)
    
    // 4. Запис списку сумісних брендів
    for i, v := range f.CompatibleBrands {
        pio.PutU32BE(b[16+4*i:], v)      // кожен бренд = 4 байти
    }
    
    return l  // повертаємо загальну довжину
}
```

### 🔧 Основна логіка Unmarshal:

```go
func (f *FileType) Unmarshal(b []byte, offset int) (n int, err error) {
    f.AtomPos.setPos(offset, len(b))  // збереження позиції
    n = 8  // пропуск size+tag заголовку
    
    // 1. Перевірка мінімальної довжини (major+minor = 8 байт)
    if len(b) < n+8 {
        return 0, parseErr("MajorBrand", offset+n, nil)
    }
    
    // 2. Читання основних полів
    f.MajorBrand = pio.U32BE(b[n:]); n += 4
    f.MinorVersion = pio.U32BE(b[n:]); n += 4
    
    // 3. Читання списку сумісних брендів (залишок даних)
    for n < len(b)-3 {  // -3 бо читаємо 4 байти, потрібно мінімум 4 байти
        f.CompatibleBrands = append(f.CompatibleBrands, pio.U32BE(b[n:]))
        n += 4
    }
    
    return
}
```

### ⚠️ Критична проблема: відсутність перевірки переповнення при читанні брендів

```
У поточному коді:
    for n < len(b)-3 {
        f.CompatibleBrands = append(f.CompatibleBrands, pio.U32BE(b[n:]))
        n += 4
    }

Проблема:
• Якщо файл містить пошкоджені дані або зловмисно великий список брендів → пам'ять може вичерпатися
• Немає обмеження на максимальну кількість брендів

✅ Виправлення: додавання ліміту на кількість брендів
    const maxCompatibleBrands = 32  // розумний ліміт
    
    for n < len(b)-3 && len(f.CompatibleBrands) < maxCompatibleBrands {
        f.CompatibleBrands = append(f.CompatibleBrands, pio.U32BE(b[n:]))
        n += 4
    }
    
    if n < len(b)-3 {
        log.Printf("warning: truncated %d bytes of compatible brands", len(b)-n)
    }
```

---

## 🔑 4. Практичне використання у streaming

### 🔧 Приклад: Створення ftyp для HLS fMP4

```go
// CreateHLSFMP4FileType — генерація ftyp для HLS з підтримкою fMP4
func CreateHLSFMP4FileType() *fmp4io.FileType {
    return &fmp4io.FileType{
        MajorBrand:   fmp4io.StringToTag("iso6"),  // ISO Base Media v6
        MinorVersion: 1,
        CompatibleBrands: []uint32{
            fmp4io.StringToTag("iso6"),  // базова сумісність
            fmp4io.StringToTag("dash"),  // підтримка DASH (корисно для сумісності)
            fmp4io.StringToTag("msdh"),  // підтримка MSS
            fmp4io.StringToTag("iso5"),  // зворотня сумісність з ISO v5
        },
    }
}

// Використання при створенні init segment:
ftyp := CreateHLSFMP4FileType()
ftypBytes := make([]byte, ftyp.Len())
ftyp.Marshal(ftypBytes)

// ftypBytes тепер містить валідний ftyp атом для HLS fMP4
```

### 🔧 Приклад: Валідація вхідного файлу

```go
// ValidateMP4FileType — перевірка коректності ftyp атому
func ValidateMP4FileType(ftyp *fmp4io.FileType) error {
    // 1. Перевірка MajorBrand
    validMajors := map[uint32]bool{
        fmp4io.StringToTag("iso2"): true,
        fmp4io.StringToTag("iso4"): true,
        fmp4io.StringToTag("iso5"): true,
        fmp4io.StringToTag("iso6"): true,
        fmp4io.StringToTag("mp41"): true,
        fmp4io.StringToTag("mp42"): true,
    }
    
    if !validMajors[ftyp.MajorBrand] {
        return fmt.Errorf("unsupported MajorBrand: %s", Tag(ftyp.MajorBrand).String())
    }
    
    // 2. Перевірка MinorVersion (зазвичай 0 або 1)
    if ftyp.MinorVersion > 1 {
        log.Printf("warning: unusual MinorVersion: %d", ftyp.MinorVersion)
    }
    
    // 3. Перевірка сумісних брендів для streaming
    hasStreamingSupport := false
    streamingBrands := []uint32{
        fmp4io.StringToTag("dash"),
        fmp4io.StringToTag("msdh"),
        fmp4io.StringToTag("iso5"),  // часто використовується з fMP4
    }
    
    for _, brand := range ftyp.CompatibleBrands {
        for _, streamingBrand := range streamingBrands {
            if brand == streamingBrand {
                hasStreamingSupport = true
                break
            }
        }
    }
    
    if !hasStreamingSupport {
        log.Printf("warning: ftyp may not support streaming: brands=%v", 
            brandStrings(ftyp.CompatibleBrands))
    }
    
    return nil
}

func brandStrings(brands []uint32) []string {
    result := make([]string, len(brands))
    for i, b := range brands {
        result[i] = Tag(b).String()
    }
    return result
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Генерація повного fMP4 init segment

```go
// GenerateCompleteFMP4Init — створення повного init segment для streaming
func GenerateCompleteFMP4Init(config *StreamConfig) ([]byte, error) {
    // 1. Створення ftyp атому
    ftyp := &fmp4io.FileType{
        MajorBrand:   fmp4io.StringToTag("iso6"),
        MinorVersion: 1,
        CompatibleBrands: []uint32{
            fmp4io.StringToTag("iso6"),
            fmp4io.StringToTag("dash"),
            fmp4io.StringToTag("msdh"),
        },
    }
    ftypBytes := make([]byte, ftyp.Len())
    ftyp.Marshal(ftypBytes)
    
    // 2. Створення moov атому з mvex (див. попередній приклад)
    moov := createMoovWithMvex(config)
    moovBytes := make([]byte, moov.Len())
    moov.Marshal(moovBytes)
    
    // 3. Об'єднання у init segment
    initSegment := append(ftypBytes, moovBytes...)
    return initSegment, nil
}

// createMoovWithMvex — helper для створення moov з mvex (спрощено)
func createMoovWithMvex(config *StreamConfig) *fmp4io.Movie {
    return &fmp4io.Movie{
        Header: &fmp4io.MovieHeader{
            TimeScale: config.VideoTimeScale,
            // ... інші параметри ...
        },
        MovieExtend: &fmp4io.MovieExtend{
            Tracks: []*fmp4io.TrackExtend{
                // ... треки з налаштуваннями ...
            },
        },
        // ... треки з метаданими кодека ...
    }
}
```

### 🔧 Приклад: Парсинг сегменту зі styp

```go
// ParseFMP4Segment — парсинг fMP4 сегменту зі styp атомом
func ParseFMP4Segment(data []byte) (*SegmentInfo, error) {
    reader := bytes.NewReader(data)
    atoms, err := fmp4io.ReadFileAtoms(reader)
    if err != nil {
        return nil, fmt.Errorf("parse atoms: %w", err)
    }
    
    info := &SegmentInfo{}
    
    // 1. Пошук styp атому (якщо є)
    for _, atom := range atoms {
        if atom.Tag() == fmp4io.STYP {
            styp, _ := atom.(*fmp4io.SegmentType)
            info.SegmentType = styp
            info.HasSTYP = true
            break
        }
    }
    
    // 2. Пошук moof атому (обов'язковий для fMP4)
    for _, atom := range atoms {
        if atom.Tag() == fmp4io.MOOF {
            moof, _ := atom.(*fmp4io.MovieFrag)
            info.MovieFrag = moof
            if moof.Header != nil {
                info.SequenceNumber = moof.Header.Seqnum
            }
            break
        }
    }
    
    if info.MovieFrag == nil {
        return nil, fmt.Errorf("moof atom not found in segment")
    }
    
    // 3. Додаткова інформація
    info.HasFTYP = false  // ftyp зазвичай тільки у init segment
    info.TotalAtoms = len(atoms)
    
    return info, nil
}

type SegmentInfo struct {
    SegmentType    *fmp4io.SegmentType
    MovieFrag      *fmp4io.MovieFrag
    HasSTYP        bool
    HasFTYP        bool
    SequenceNumber uint32
    TotalAtoms     int
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при читанні брендів** | Доступ за межами буфера у Unmarshal | Додайте перевірку `if len(b) < n+4` перед `pio.U32BE()` |
| **Переповнення пам'яті** | Зловмисний файл з тисячами брендів | Додайте ліміт `maxCompatibleBrands` при читанні |
| **Невірний MajorBrand** | Плеєр не розпізнає формат файлу | Використовуйте стандартні бренди: 'iso6', 'mp42', тощо |
| **Відсутній styp у сегменті** | Клієнт не може почати з середини потоку | Додавайте styp на початку кожного fMP4 сегменту |
| **Несумісні бренди** | Помилки сумісності між клієнтом та сервером | Перевіряйте CompatibleBrands перед відправкою |

---

## ⚡ Оптимізації для high-performance streaming

### 1. Кешування серіалізованих ftyp/styp:

```go
var cachedFTYP = sync.Map{}  // map[string][]byte

func GetCachedFTYP(key string) []byte {
    if cached, ok := cachedFTYP.Load(key); ok {
        return cached.([]byte)
    }
    
    ftyp := CreateHLSFMP4FileType()  // або інша конфігурація
    bytes := make([]byte, ftyp.Len())
    ftyp.Marshal(bytes)
    
    cachedFTYP.Store(key, bytes)
    return bytes
}
```

### 2. Попередня аллокація буферів:

```go
// PreallocateFileTypeBuffer — виділення місця для серіалізації заздалегідь
func PreallocateFileTypeBuffer(ftyp *fmp4io.FileType) []byte {
    estimatedSize := ftyp.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateFileTypeBuffer(ftyp)
n := ftyp.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type FileTypeMetrics struct {
    AtomsParsed prometheus.CounterVec
    ParseLatency prometheus.HistogramVec
    BrandCount prometheus.HistogramVec
    ParseErrors prometheus.CounterVec
}

func (m *FileTypeMetrics) RecordParse(brandCount int, duration time.Duration, err error) {
    m.AtomsParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    m.BrandCount.Observe(float64(brandCount))
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання FileType/SegmentType

```go
// ✅ 1. Перевірка меж буфера перед читанням брендів
if len(b) < n+4 {
    err = parseErr("CompatibleBrand", n+offset, err)
    return
}
f.CompatibleBrands = append(f.CompatibleBrands, pio.U32BE(b[n:]))
n += 4

// ✅ 2. Обмеження кількості брендів для захисту від DoS
const maxCompatibleBrands = 32
for n < len(b)-3 && len(f.CompatibleBrands) < maxCompatibleBrands {
    // ... читання брендів ...
}

// ✅ 3. Валідація MajorBrand проти списку підтримуваних
validMajors := map[uint32]bool{
    fmp4io.StringToTag("iso6"): true,
    // ... інші ...
}
if !validMajors[ftyp.MajorBrand] {
    return fmt.Errorf("unsupported MajorBrand: %s", Tag(ftyp.MajorBrand).String())
}

// ✅ 4. Додавання styp до кожного fMP4 сегменту
styp := &fmp4io.SegmentType{
    MajorBrand: fmp4io.StringToTag("iso6"),
    CompatibleBrands: []uint32{fmp4io.StringToTag("dash")},
}
// ... серіалізація та запис ...

// ✅ 5. Логування з контекстом для дебагу
log.Printf("Parsed ftyp: MajorBrand=%s, CompatibleBrands=%v", 
    Tag(ftyp.MajorBrand).String(), brandStrings(ftyp.CompatibleBrands))

// ✅ 6. Метрики для моніторингу
metrics.RecordParse(len(ftyp.CompatibleBrands), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [MP4 Brand Registry](https://mp4ra.org/#/brands) — офіційний реєстр брендів
- 📄 [HLS fMP4 Specification](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 📄 [DASH MPD & fMP4](https://dashif.org/docs/DASH-IF-IOP-v4.3.pdf) — DASH Industry Forum
- 🧪 [Go encoding/binary Package](https://pkg.go.dev/encoding/binary) — робота з бінарними даними

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед `pio.U32BE()`** — уникнення панік при пошкоджених файлах.
> 2. **Обмежуйте кількість `CompatibleBrands`** — захист від DoS через зловмисні файли.
> 3. **Використовуйте стандартні `MajorBrand` значення** — забезпечення сумісності з плеєрами.
> 4. **Додавайте `styp` до кожного fMP4 сегменту** — для підтримки seek та low-latency streaming.
> 5. **Валідуйте `CompatibleBrands` перед відправкою** — уникнення помилок сумісності на клієнті.

Потрібен приклад реалізації повного циклу створення/парсингу fMP4 init segment з підтримкою кількох треків та брендів, або інтеграція `fmp4io.FileType` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀