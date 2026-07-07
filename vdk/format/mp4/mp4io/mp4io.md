# 📦 Глибокий розбір: `mp4io` — Низькорівневі утиліти для парсингу атомів MP4

Цей файл — **бібліотека низькорівневих функцій** для парсингу, маршалінгу та навігації по атомах (boxes) формату ISO Base Media File Format (ISO BMFF), який лежить в основі контейнерів `.mp4`, `.mov`, `.m4a` тощо. Він надає інструменти для роботи з бінарною структурою файлів без залежності від високо-рівневих абстракцій.

---

## 🗺️ Архітектурна схема mp4io

```
┌────────────────────────────────────────┐
│ 📦 mp4io — Low-Level MP4 Box Parser   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Atom interface — уніфікований доступ│
│  • Tag (fourcc) — ідентифікація атомів │
│  • ParseError — деталізовані помилки   │
│  • ReadFileAtoms() — парсинг файлу     │
│  • Time/float конвертери для MP4       │
│                                         │
│  🔄 Потік парсингу:                     │
│  io.ReadSeeker → ReadFileAtoms()       │
│  → []Atom → навігація через FindChildren│
│                                         │
│  📡 Підтримка атомів:                   │
│  • MOOV/MOOF — метадані фільму         │
│  • ESDS — MPEG-4 Stream Descriptor     │
│  • TFHD/TRUN — фрагментовані треки     │
│  • Dummy — fallback для невідомих атомів│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Atom interface — уніфікований доступ до атомів

### 🔧 Інтерфейс:

```go
type Atom interface {
    Pos() (int, int)              // offset, size у файлі
    Tag() Tag                     // fourcc код (напр. 'moov')
    Marshal([]byte) int           // серіалізація у байти
    Unmarshal([]byte, int) (int, error)  // десеріалізація з байт
    Len() int                     // розмір атому у байтах
    Children() []Atom             // дочірні атоми (рекурсивна структура)
}
```

### 🔍 Призначення:
- **Уніфікація**: будь-який атом (moov, trak, stts, тощо) реалізує цей інтерфейс
- **Рекурсія**: `Children()` дозволяє навігацію по дереву атомів
- **Сериалізація**: `Marshal`/`Unmarshal` для запису/читання у бінарний формат

### ✅ Ваш use-case: пошук атому за тегом

```go
// FindAVCConfig — пошук AVCDecoderConfigurationRecord у треку
func FindAVCConfig(track *mp4io.Track) (*mp4io.AVC1Conf, error) {
    // Рекурсивний пошук атому 'avcC' (AVCC)
    atom := mp4io.FindChildren(track, mp4io.AVCC)
    if atom == nil {
        return nil, fmt.Errorf("AVC config not found")
    }
    
    conf, ok := atom.(*mp4io.AVC1Conf)
    if !ok {
        return nil, fmt.Errorf("unexpected atom type: %T", atom)
    }
    return conf, nil
}

// Використання:
conf, err := FindAVCConfig(trackAtom)
if err != nil { /* handle error */ }
// conf.Data містить AVCDecoderConfigurationRecord для ініціалізації декодера
```

---

## 🔑 2. Tag (fourcc) — ідентифікація атомів

### 🔧 Реалізація:

```go
type Tag uint32

func (self Tag) String() string {
    var b [4]byte
    pio.PutU32BE(b[:], uint32(self))  // Big-Endian конвертація
    for i := 0; i < 4; i++ {
        if b[i] == 0 { b[i] = ' ' }   // нулі → пробіли для читабельності
    }
    return string(b[:])
}

func StringToTag(tag string) Tag {
    var b [4]byte
    copy(b[:], []byte(tag))  // доповнення нулями якщо tag < 4 символів
    return Tag(pio.U32BE(b[:]))
}
```

### 🔍 Приклади тегів:

```
• 'moov' (0x6D6F6F76) — Movie atom (метадані всього файлу)
• 'trak' (0x7472616B) — Track atom (метадані одного треку)
• 'mdat' (0x6D646174) — Media Data atom (сира медіа-інформація)
• 'stts' (0x73747473) — Time-To-Sample table
• 'avcC' (0x61766343) — AVCDecoderConfigurationRecord для H.264
• 'esds' (0x65736473) — MPEG-4 Stream Descriptor для AAC
```

### ✅ Ваш use-case: фільтрація атомів за типом

```go
// FilterAtomsByTag — отримання всіх атомів певного типу
func FilterAtomsByTag(atoms []mp4io.Atom, tag mp4io.Tag) []mp4io.Atom {
    result := make([]mp4io.Atom, 0)
    for _, atom := range atoms {
        if atom.Tag() == tag {
            result = append(result, atom)
        }
        // Рекурсивний пошук у дочірніх атомах
        for _, child := range atom.Children() {
            result = append(result, FilterAtomsByTag([]mp4io.Atom{child}, tag)...)
        }
    }
    return result
}

// Приклад: отримання всіх треків у файлі
tracks := FilterAtomsByTag(atoms, mp4io.TRACK)  // TRACK = StringToTag("trak")
```

---

## 🔑 3. ParseError — деталізовані помилки парсингу

### 🔧 Структура з ланцюжком помилок:

```go
type ParseError struct {
    Debug  string      // опис помилки (напр. "hdr", "datalen")
    Offset int         // позиція у файлі де сталася помилка
    prev   *ParseError // попередня помилка у ланцюжку (для контексту)
}

func (self *ParseError) Error() string {
    s := []string{}
    // Побудова ланцюжка: остання помилка → перша
    for p := self; p != nil; p = p.prev {
        s = append(s, fmt.Sprintf("%s:%d", p.Debug, p.Offset))
    }
    return "mp4io: parse error: " + strings.Join(s, ",")
}
```

### 🔍 Приклад виводу:

```
mp4io: parse error: hdr:128,datalen:256,MP4ESDescrTag:260
```

Це означає:
1. Помилка у заголовку на позиції 128
2. Потім помилка у довжині даних на 256
3. Потім помилка у MP4ESDescrTag на 260

### ✅ Ваш use-case: логування з контекстом

```go
// LogParseError — форматування помилки для логування
func LogParseError(err error, filename string) {
    if pe, ok := err.(*mp4io.ParseError); ok {
        log.Printf("File %s: parse error chain:", filename)
        for p := pe; p != nil; p = p.prev {
            log.Printf("  • %s at offset %d (0x%X)", p.Debug, p.Offset, p.Offset)
        }
    } else {
        log.Printf("File %s: error: %v", filename, err)
    }
}

// Використання:
atoms, err := mp4io.ReadFileAtoms(file)
if err != nil {
    LogParseError(err, "video.mp4")
    return err
}
```

---

## 🔑 4. Конвертери часу та чисел для MP4

### 🔧 Час у форматі 1904-01-01 (MP4 epoch):

```go
func GetTime32(b []byte) (t time.Time) {
    sec := pio.U32BE(b)  // секунди від 1904-01-01
    t = time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC)
    return t.Add(time.Second * time.Duration(sec))
}

func PutTime32(b []byte, t time.Time) {
    dur := t.Sub(time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC))
    sec := uint32(dur / time.Second)
    pio.PutU32BE(b, sec)
}
```

### 🔍 Чому 1904 рік?

```
MP4 використовує 1904-01-01 як епоху (на відміну від Unix 1970-01-01):
• Історична причина: сумісність з QuickTime (Apple)
• 32-бітне поле: діапазон 1904..2040 роки
• 64-бітне поле (GetTime64): діапазон до ~585 мільярдів років

⚠️ Увага: при конвертації з/у time.Time потрібно враховувати цю різницю!
```

### 🔧 Фіксована крапка для коефіцієнтів (16.16 формат):

```go
func PutFixed32(b []byte, f float64) {
    intpart, fracpart := math.Modf(f)
    pio.PutU16BE(b[0:2], uint16(intpart))      // ціла частина
    pio.PutU16BE(b[2:4], uint16(fracpart*65536.0))  // дробова * 2^16
}

func GetFixed32(b []byte) float64 {
    return float64(pio.U16BE(b[0:2])) + 
           float64(pio.U16BE(b[2:4]))/65536.0
}
```

### 🔍 Де використовується:

```
• Масштабування відео (track width/height у матриці трансформації)
• Гучність аудіо (volume у track header)
• Швидкість відтворення (preferred rate у movie header)

Приклад: масштаб 1.5 = 0x00018000 (ціла=1, дробова=0.5*65536=32768)
```

### ✅ Ваш use-case: коректна обробка часу

```go
// ConvertMP4TimeToUnix — конвертація часу з MP4 epoch у Unix epoch
func ConvertMP4TimeToUnix(mp4Time time.Time) int64 {
    // Різниця між епохами: 1904-01-01 та 1970-01-01 = 66 років
    epochDiff := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC).Sub(
                 time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC))
    return mp4Time.Add(-epochDiff).Unix()
}

// Використання для метаданих:
movieHeader := moov.Header
creationTime := mp4io.GetTime64(movieHeader.CreationTime[:])
unixTime := ConvertMP4TimeToUnix(creationTime)
log.Printf("File created at Unix time: %d", unixTime)
```

---

## 🔑 5. ReadFileAtoms() — парсинг файлу у список атомів

### 🔧 Основна логіка:

```go
func ReadFileAtoms(r io.ReadSeeker) (atoms []Atom, err error) {
    for {
        // 1. Читання заголовку атому (8 байт: size + tag)
        offset, _ := r.Seek(0, 1)  // поточна позиція
        taghdr := make([]byte, 8)
        if _, err = io.ReadFull(r, taghdr); err != nil {
            if err == io.EOF { err = nil }  // нормальне завершення
            return
        }
        
        size := pio.U32BE(taghdr[0:])  // розмір атому
        tag := Tag(pio.U32BE(taghdr[4:]))  // fourcc код
        
        // 2. Обробка special cases для розміру
        if size == 0 {
            // size=0 означає "до кінця файлу" — тільки для останнього атому
            err = fmt.Errorf("bad hdr size")
            return
        }
        // ⚠️ Не оброблено size=1 (64-бітний розмір) — критична проблема!
        
        // 3. Створення атому за типом
        var atom Atom
        switch tag {
        case MOOV: atom = &Movie{}      // метадані фільму
        case MOOF: atom = &MovieFrag{}  // фрагмент для streaming
        // Інші типи → Dummy (невідомий атом)
        }
        
        if atom != nil {
            // Читання всього атому у пам'ять
            b := make([]byte, int(size))
            if _, err = io.ReadFull(r, b[8:]); err != nil { return }
            copy(b, taghdr)
            
            // Десеріалізація
            if _, err = atom.Unmarshal(b, int(offset)); err != nil { return }
            atoms = append(atoms, atom)
        } else {
            // Невідомий атом: пропуск даних
            dummy := &Dummy{Tag_: tag}
            dummy.setPos(int(offset), int(size))
            if _, err = r.Seek(int64(size)-8, 1); err != nil { return }
            atoms = append(atoms, dummy)
        }
    }
}
```

### ⚠️ Критична проблема: не підтримка 64-бітних розмірів

```
У стандарті MP4:
• Якщо size == 1 → наступні 8 байт = 64-бітний розмір
• Це потрібно для файлів > 4 ГБ (2^32 байт)

У вихідному коді:
    if size == 0 {  // тільки перевірка на 0
        err = fmt.Errorf("bad hdr size")
        return
    }
    // ❌ Немає обробки size == 1 для 64-бітних розмірів!

Наслідки: Файли >4 ГБ не зможуть бути прочитані коректно.

✅ Виправлення:
    if size == 1 {
        // Читання 64-бітного розміру
        size64 := make([]byte, 8)
        if _, err = io.ReadFull(r, size64); err != nil { return }
        size = int(pio.U64BE(size64))
        // ⚠️ Потрібно перевірити чи size <= max int на цій архітектурі
    }
```

### ✅ Ваш use-case**: безпечне читання великих файлів

```go
// ReadFileAtomsSafe — версія з підтримкою 64-бітних розмірів
func ReadFileAtomsSafe(r io.ReadSeeker) ([]mp4io.Atom, error) {
    var atoms []mp4io.Atom
    
    for {
        offset, _ := r.Seek(0, 1)
        taghdr := make([]byte, 8)
        if _, err := io.ReadFull(r, taghdr); err != nil {
            if err == io.EOF { return atoms, nil }
            return nil, err
        }
        
        size := int64(pio.U32BE(taghdr[0:]))
        tag := mp4io.Tag(pio.U32BE(taghdr[4:]))
        
        // Обробка 64-бітного розміру
        if size == 1 {
            size64 := make([]byte, 8)
            if _, err := io.ReadFull(r, size64); err != nil {
                return nil, fmt.Errorf("read 64-bit size: %w", err)
            }
            size = int64(pio.U64BE(size64))
            if size < 16 {  // мінімум: 8 (header) + 8 (size64)
                return nil, fmt.Errorf("invalid 64-bit size: %d", size)
            }
        } else if size == 0 {
            // size=0: атом до кінця файлу
            endPos, err := r.Seek(0, 2)  // кінець файлу
            if err != nil { return nil, err }
            size = endPos - offset
            if _, err := r.Seek(offset+8, 0); err != nil { return nil, err }  // повернення
        }
        
        // Перевірка розумності розміру
        if size < 8 || size > 1<<30 {  // 1GB ліміт для безпеки
            return nil, fmt.Errorf("invalid atom size: %d at offset %d", size, offset)
        }
        
        // ... решта логіки як у оригіналі ...
    }
}
```

---

## 🔑 6. ElemStreamDesc (esds) — парсинг MPEG-4 Stream Descriptor

### 🔍 Призначення:
Атом `esds` містить опис аудіо/відео потоку для кодеків MPEG-4 (напр. AAC). Він використовує вкладену структуру дескрипторів з variable-length кодуванням довжини.

### 🔧 Формат дескрипторів:

```
Кожен дескриптор має формат:
  [1-byte tag][variable-length size][payload]

Size кодується у форматі "MPEG-4 SL":
  • Кожен байт: 7 біт даних + 1 біт продовження (0x80)
  • Приклад: 300 = 0x81 0x2C (1*128 + 44 = 172? ні, це складніше)
  
Справжня логіка у parseLength():
  length = 0
  for each byte:
    length = (length << 7) | (byte & 0x7F)
    if byte & 0x80 == 0: break  // останній байт
```

### 🔧 Структура esds для AAC:

```
esds atom:
  [4-byte version/flags = 0]
  [ES_Descriptor]:
    tag=0x03 (MP4ESDescrTag)
    size=variable
    ES_ID=2 bytes (track ID)
    flags=1 byte
    [DecoderConfigDescriptor]:
      tag=0x04 (MP4DecConfigDescrTag)
      size=variable
      objectType=1 byte (0x40 = AAC)
      streamType=1 byte (0x15 = AudioStream)
      bufferSize=3 bytes
      maxBitrate=4 bytes
      avgBitrate=4 bytes
      [DecoderSpecificInfo]:
        tag=0x05 (MP4DecSpecificDescrTag)
        size=variable
        AudioSpecificConfig (2+ bytes)  // ← це DecConfig у коді!
    [SLConfigDescriptor] (опціонально)
      tag=0x06
      size=1
      value=0x02
```

### ✅ Ваш use-case: витягування AudioSpecificConfig для AAC

```go
// GetAACConfigFromESDS — отримання MPEG4AudioConfig з esds атому
func GetAACConfigFromESDS(esds *mp4io.ElemStreamDesc) ([]byte, error) {
    if esds == nil || len(esds.DecConfig) == 0 {
        return nil, fmt.Errorf("empty ES descriptor")
    }
    
    // esds.DecConfig вже містить AudioSpecificConfig без дескрипторних заголовків
    // (це робить Unmarshal у вихідному коді)
    return esds.DecConfig, nil
}

// Ініціалізація AAC декодера:
esds := track.GetElemStreamDesc()
if esds == nil {
    return fmt.Errorf("no esds atom for AAC track")
}
configBytes, err := GetAACConfigFromESDS(esds)
if err != nil { return err }

codecData, err := aacparser.NewCodecDataFromMPEG4AudioConfigBytes(configBytes)
if err != nil { return fmt.Errorf("parse AAC config: %w", err) }
```

---

## 🔑 7. Навігація по атомах: FindChildren

### 🔧 Рекурсивний пошук:

```go
func FindChildren(root Atom, tag Tag) Atom {
    if root.Tag() == tag {
        return root
    }
    for _, child := range root.Children() {
        if r := FindChildren(child, tag); r != nil {
            return r
        }
    }
    return nil
}
```

### ⚠️ Обмеження: повертає тільки перший знайдений атом

```
Якщо у файлі кілька атомів з однаковим тегом (напр. кілька 'trak'),
функція поверне тільки перший знайдений (у порядку обходу дерева).

✅ Для отримання всіх: реалізуйте FindChildrenAll
```

### ✅ Ваш use-case: пошук всіх треків

```go
// FindAllChildren — рекурсивний пошук всіх атомів з тегом
func FindAllChildren(root mp4io.Atom, tag mp4io.Tag) []mp4io.Atom {
    var results []mp4io.Atom
    
    if root.Tag() == tag {
        results = append(results, root)
    }
    
    for _, child := range root.Children() {
        results = append(results, FindAllChildren(child, tag)...)
    }
    
    return results
}

// Використання: отримання всіх аудіо треків
tracks := FindAllChildren(moov, mp4io.TRACK)
var audioTracks []*mp4io.Track
for _, t := range tracks {
    track, ok := t.(*mp4io.Track)
    if !ok { continue }
    
    // Перевірка типу треку через handler
    if track.Media != nil && track.Media.Handler != nil {
        if string(track.Media.Handler.SubType[:]) == "soun" {
            audioTracks = append(audioTracks, track)
        }
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Аналіз метаданих MP4 файлу

```go
// AnalyzeMP4Metadata — витягування технічних метаданих
func AnalyzeMP4Metadata(filename string) (*MediaInfo, error) {
    f, err := os.Open(filename)
    if err != nil { return nil, err }
    defer f.Close()
    
    // Парсинг атомів
    atoms, err := mp4io.ReadFileAtoms(f)
    if err != nil { return nil, fmt.Errorf("parse atoms: %w", err) }
    
    // Пошук moov атому
    moovAtom := mp4io.FindChildrenByName(atoms[0], "moov")
    if moovAtom == nil {
        return nil, fmt.Errorf("moov atom not found")
    }
    moov, ok := moovAtom.(*mp4io.Movie)
    if !ok { return nil, fmt.Errorf("unexpected moov type: %T", moovAtom) }
    
    info := &MediaInfo{
        Duration: time.Duration(moov.Header.Duration) * time.Second / time.Duration(moov.Header.TimeScale),
        CreationTime: mp4io.GetTime64(moov.Header.CreationTime[:]),
    }
    
    // Аналіз треків
    for _, trackAtom := range moov.Tracks {
        trackInfo := &TrackInfo{
            ID:        trackAtom.Header.TrackID,
            Duration:  time.Duration(trackAtom.Header.Duration) * time.Second / time.Duration(trackAtom.Media.Header.TimeScale),
            TimeScale: int64(trackAtom.Media.Header.TimeScale),
        }
        
        // Визначення типу треку
        if trackAtom.Media.Handler != nil {
            handlerType := string(trackAtom.Media.Handler.SubType[:])
            switch handlerType {
            case "vide": trackInfo.Type = "video"
            case "soun": trackInfo.Type = "audio"
            case "subt": trackInfo.Type = "subtitle"
            }
        }
        
        // Витягування кодек-інформації
        if avc1 := trackAtom.GetAVC1Conf(); avc1 != nil {
            trackInfo.Codec = "H.264"
            // Парсинг SPS для роздільної здатності...
        } else if esds := trackAtom.GetElemStreamDesc(); esds != nil {
            trackInfo.Codec = "AAC"
            trackInfo.AACConfig = esds.DecConfig
        }
        
        info.Tracks = append(info.Tracks, trackInfo)
    }
    
    return info, nil
}

type MediaInfo struct {
    Duration     time.Duration
    CreationTime time.Time
    Tracks       []*TrackInfo
}

type TrackInfo struct {
    ID        int32
    Type      string
    Codec     string
    Duration  time.Duration
    TimeScale int64
    AACConfig []byte  // для AAC
}
```

### 🔧 Приклад: Перевірка цілісності файлу

```go
// ValidateMP4Structure — базова валідація структури файлу
func ValidateMP4Structure(r io.ReadSeeker) error {
    atoms, err := mp4io.ReadFileAtoms(r)
    if err != nil {
        return fmt.Errorf("parse error: %w", err)
    }
    
    // Перевірка наявності обов'язкових атомів
    hasFtyp := false
    hasMoov := false
    
    for _, atom := range atoms {
        switch atom.Tag() {
        case mp4io.StringToTag("ftyp"):
            hasFtyp = true
        case mp4io.StringToTag("moov"):
            hasMoov = true
        }
    }
    
    if !hasFtyp {
        return fmt.Errorf("missing 'ftyp' atom")
    }
    if !hasMoov {
        return fmt.Errorf("missing 'moov' atom")
    }
    
    // Перевірка що moov містить треки
    moov := mp4io.FindChildrenByName(atoms[0], "moov")
    if movie, ok := moov.(*mp4io.Movie); ok {
        if len(movie.Tracks) == 0 {
            return fmt.Errorf("moov atom has no tracks")
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"bad hdr size" для файлів >4 ГБ** | Помилка при парсингу великих файлів | Реалізуйте обробку `size == 1` для 64-бітних розмірів |
| **FindChildren повертає не той атом** | Коли є кілька атомів з однаковим тегом | Використовуйте `FindAllChildren` або перевіряйте контекст батьків |
| **Невірний час у метаданих** | Час зміщений на 66 років | Враховуйте різницю між 1904 та 1970 епохами при конвертації |
| **Паніка при type assertion** | `atom.(*Movie)` не співпадає | Завжди перевіряйте `ok` після type assertion |
| **Некоректний парсинг esds** | AAC config не витягується | Переконайтеся, що `DecConfig` містить тільки AudioSpecificConfig без заголовків дескрипторів |

---

## ⚡ Оптимізації для великих файлів

### 1. Lazy reading атомів:

```go
// ReadAtomHeaderOnly — читання тільки заголовків для швидкого сканування
func ReadAtomHeaders(r io.ReadSeeker) ([]AtomHeader, error) {
    var headers []AtomHeader
    
    for {
        offset, _ := r.Seek(0, 1)
        taghdr := make([]byte, 8)
        if _, err := io.ReadFull(r, taghdr); err != nil {
            if err == io.EOF { break }
            return nil, err
        }
        
        size := int64(pio.U32BE(taghdr[0:]))
        tag := mp4io.Tag(pio.U32BE(taghdr[4:]))
        
        // Обробка 64-бітного розміру
        if size == 1 {
            size64 := make([]byte, 8)
            if _, err := io.ReadFull(r, size64); err != nil { return nil, err }
            size = int64(pio.U64BE(size64))
            if size < 16 { return nil, fmt.Errorf("invalid 64-bit size") }
        } else if size == 0 {
            endPos, _ := r.Seek(0, 2)
            size = endPos - offset
            r.Seek(offset+8, 0)  // повернення
        }
        
        headers = append(headers, AtomHeader{
            Offset: offset,
            Size:   size,
            Tag:    tag,
        })
        
        // Пропуск даних атому
        if _, err := r.Seek(offset+size, 0); err != nil { return nil, err }
    }
    
    return headers, nil
}

type AtomHeader struct {
    Offset int64
    Size   int64
    Tag    mp4io.Tag
}
```

### 2. Кешування результатів пошуку:

```go
type AtomCache struct {
    mu    sync.RWMutex
    cache map[mp4io.Tag][]mp4io.Atom  // tag → атоми
}

func (c *AtomCache) FindAll(root mp4io.Atom, tag mp4io.Tag) []mp4io.Atom {
    c.mu.RLock()
    if atoms, ok := c.cache[tag]; ok {
        c.mu.RUnlock()
        return atoms
    }
    c.mu.RUnlock()
    
    // Пошук якщо не в кеші
    atoms := FindAllChildren(root, tag)
    
    c.mu.Lock()
    if c.cache == nil { c.cache = make(map[mp4io.Tag][]mp4io.Atom) }
    c.cache[tag] = atoms
    c.mu.Unlock()
    
    return atoms
}
```

### 3. Моніторинг продуктивності парсингу:

```go
type ParserMetrics struct {
    AtomsParsed   prometheus.CounterVec
    ParseLatency  prometheus.HistogramVec
    LargeAtomCount prometheus.CounterVec  // атоми >1MB
}

func (m *ParserMetrics) RecordAtom(tag mp4io.Tag, size int, duration time.Duration) {
    m.AtomsParsed.WithLabelValues(tag.String()).Inc()
    m.ParseLatency.WithLabelValues(tag.String()).Observe(duration.Seconds())
    if size > 1<<20 {  // >1MB
        m.LargeAtomCount.WithLabelValues(tag.String()).Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання mp4io

```go
// ✅ 1. Обробка 64-бітних розмірів атомів
if size == 1 {
    // read 64-bit size
}

// ✅ 2. Перевірка type assertion з ok
if movie, ok := atom.(*mp4io.Movie); ok {
    // use movie
} else {
    return fmt.Errorf("unexpected atom type: %T", atom)
}

// ✅ 3. Валідація часу перед конвертацією
if mp4Time.Year() < 1904 || mp4Time.Year() > 2100 {
    log.Printf("warning: suspicious MP4 time: %v", mp4Time)
}

// ✅ 4. Обмеження максимального розміру атому для безпеки
if size > 1<<30 {  // 1GB
    return fmt.Errorf("atom too large: %d bytes", size)
}

// ✅ 5. Логування з контекстом для помилок
if err != nil {
    LogParseError(err, filename)  // функція з прикладу вище
}

// ✅ 6. Метрики для моніторингу
metrics.RecordAtom(tag, size, time.Since(start))
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [MP4 Box Structure](https://wiki.multimedia.cx/index.php/MP4) — візуальна схема атомів
- 📄 [MPEG-4 Systems Descriptor Syntax](https://standards.iso.org/ittf/PubliclyAvailableStandards/c041828_ISO_IEC_14496-1_2010(E).zip) — формат esds
- 🧪 [Go encoding/binary](https://pkg.go.dev/encoding/binary) — бітова робота з числами
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди обробляйте `size == 1` для 64-бітних розмірів** — інакше файли >4 ГБ не зможуть бути прочитані.
> 2. **Перевіряйте `ok` після type assertion** — уникнення панік при несподіваних типах атомів.
> 3. **Враховуйте різницю епох (1904 vs 1970)** при конвертації часу — інакше метадані будуть зміщені на 66 років.
> 4. **Обмежуйте максимальний розмір атому** — захист від malformed файлів або атак.
> 5. **Кешуйте результати пошуку атомів** — прискорення повторних операцій навігації.

Потрібен приклад реалізації `WriteFileAtoms` для створення власних MP4 файлів з низького рівня? Готовий допомогти! 🚀