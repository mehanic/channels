# 🏗️ `m3u8/types.go`: Ядро типів даних для HLS-плейлистів

Це **фундаментальний модуль** бібліотеки `github.com/grafov/m3u8`, який визначає **всі основні структури, інтерфейси та константи** для роботи з HLS-плейлистами (HTTP Live Streaming). Він слугує "моделлю даних" для парсингу, генерації та маніпуляції як Master, так і Media плейлистів.

---

## 🎯 Коротка відповідь

> **Це "скелет" бібліотеки m3u8**: він визначає типобезпечні структури для представлення всіх елементів HLS-специфікації (сегменти, варіанти, ключі шифрування, кастомні теги) та інтерфейси для розширення функціоналу — ідеально для інтеграції CCTV-метаданих у ваш HLS-конвеєр.

---

## 🧱 Ієрархія типів: Огляд

### 🔹 Константи та версії протоколу

```go
const (
    minver = uint8(3)  // 🔹 Мінімальна версія для підтримки float-тривалостей
    DATETIME = time.RFC3339Nano  // 🔹 Формат для #EXT-X-PROGRAM-DATE-TIME
)
```

**📋 Правила сумісності (згідно з розділом 7 специфікації):**

| Версія | Додані можливості | Значення для CCTV |
|--------|------------------|-------------------|
| **≥2** | `IV` атрибут у `#EXT-X-KEY` | Шифрування сегментів з індивідуальними векторами ініціалізації |
| **≥3** | Плаваючі значення тривалості в `#EXTINF` | Точні таймінги для сегментів <1 секунди |
| **≥4** | `#EXT-X-BYTERANGE`, `#EXT-X-I-FRAME-STREAM-INF`, `#EXT-X-MEDIA` | Часткове читання, I-frame only варіанти, аудіо-доріжки |

**🎯 Призначення**: Забезпечити **зворотну сумісність** та коректну генерацію плейлистів відповідно до версії протоколу.

---

### 🔹 Типи-перелічення (Enums)

```go
type ListType uint
const (
    MASTER ListType = iota + 1  // 🔹 Master playlist: варіанти якості
    MEDIA                       // 🔹 Media playlist: послідовність сегментів
)

type MediaType uint
const (
    EVENT MediaType = iota + 1  // 🔹 Live-подія (плейлист росте)
    VOD                         // 🔹 Video-on-Demand (закритий плейлист)
)

type SCTE35Syntax uint
const (
    SCTE35_67_2014  // 🔹 Стандартний синтаксис (SCTE-67)
    SCTE35_OATCLS   // 🔹 Поширений не-стандартний формат
)

type SCTE35CueType uint
const (
    SCTE35Cue_Start  // 🔹 Початок рекламної вставки
    SCTE35Cue_Mid    // 🔹 Середина вставки
    SCTE35Cue_End    // 🔹 Кінець вставки
)
```

**🎯 Призначення**: Типобезпечне представлення **станів та режимів** плейлистів.

---

## 🏗️ Основні структури

### 🔹 `MediaPlaylist` — плейлист медіа-сегментів

```go
type MediaPlaylist struct {
    // 🔹 Глобальні параметри
    TargetDuration   float64           // 🔹 Макс. тривалість сегмента (#EXT-X-TARGETDURATION)
    SeqNo            uint64            // 🔹 Номер першого сегмента (#EXT-X-MEDIA-SEQUENCE)
    Iframe           bool              // 🔹 Тільки I-frame сегменти (#EXT-X-I-FRAMES-ONLY)
    Closed           bool              // 🔹 VOD (true) чи Live (false) плейлист
    MediaType        MediaType         // 🔹 EVENT або VOD
    DiscontinuitySeq uint64            // 🔹 Номер розриву (#EXT-X-DISCONTINUITY-SEQUENCE)
    
    // 🔹 Таймінги
    StartTime        float64           // 🔹 Зсув часу (#EXT-X-START:TIME-OFFSET)
    StartTimePrecise bool              // 🔹 Точність зсуву (#EXT-X-START:PRECISE)
    
    // 🔹 Внутрішня буферизація (FIFO для live-плейлистів)
    winsize          uint              // 🔹 Розмір вікна (0 = весь плейлист для VOD)
    capacity         uint              // 🔹 Загальна ємність масиву
    head, tail       uint              // 🔹 Покажчики черги
    count            uint              // 🔹 Кількість доданих сегментів
    
    // 🔹 Сегменти
    Segments         []*MediaSegment   // 🔹 Масив сегментів (циклічний буфер)
    
    // 🔹 Глобальні теги (застосовуються до всіх сегментів)
    Key              *Key              // 🔹 Ключ шифрування за замовчуванням
    Map              *Map              // 🔹 Ініціалізаційний сегмент (fMP4)
    WV               *WV               // 🔹 Widevine-специфічні метадані
    
    // 🔹 Кастомні теги
    Custom           map[string]CustomTag
    customDecoders   []CustomDecoder
    
    // 🔹 Внутрішні поля
    durationAsInt    bool              // 🔹 Формат виводу тривалостей
    keyformat        int               // 🔹 Формат ключа
    buf              bytes.Buffer      // 🔹 Буфер для генерації
    ver              uint8             // 🔹 Версія протоколу
    Args             string            // 🔹 Додаткові аргументи після URI
}
```

**🔄 Циклічний буфер для live-плейлистів:**
```
🔹 Live-плейлист (winsize=5):
   Segments: [seg0, seg1, seg2, seg3, seg4, seg5, ...]
              ↑                    ↑
            tail                 head
            
   • Додавання нового сегмента:
     - Запис у Segments[head % capacity]
     - head++
     - Якщо head-tail > winsize → tail++ (видалення найстарішого)
   
   • Кодування плейлиста:
     - Ітерація від tail до head (останні winsize сегментів)
```

**🎯 Призначення**: Ефективне керування **ковзним вікном** для live-стрімінгу без постійного виділення пам'яті.

---

### 🔹 `MasterPlaylist` — master-плейлист з варіантами якості

```go
type MasterPlaylist struct {
    Variants            []*Variant      // 🔹 Список варіантів (якість, аудіо, субтитри)
    Args                string          // 🔹 Додаткові аргументи після URI
    CypherVersion       string          // 🔹 Widevine: версія шифру
    buf                 bytes.Buffer    // 🔹 Буфер для генерації
    ver                 uint8           // 🔹 Версія протоколу
    independentSegments bool            // 🔹 #EXT-X-INDEPENDENT-SEGMENTS
    Custom              map[string]CustomTag
    customDecoders      []CustomDecoder
}
```

**🎯 Призначення**: Представлення **адаптивного стрімінгу** з кількома варіантами якості, аудіо-доріжками та субтитрами.

---

### 🔹 `Variant` — варіант якості у master-плейлисті

```go
type Variant struct {
    URI       string          // 🔹 Шлях до media-плейлиста цього варіанту
    Chunklist *MediaPlaylist  // 🔹 Опціонально: вкладений плейлист
    VariantParams             // 🔹 Параметри варіанту (вбудована структура)
}
```

---

### 🔹 `VariantParams` — параметри варіанту якості

```go
type VariantParams struct {
    // 🔹 Обов'язкові поля
    ProgramId        uint32   // 🔹 Застаріле, але підтримується для сумісності
    Bandwidth        uint32   // 🔹 Середній бітрейт у бітах/сек (#EXT-X-STREAM-INF)
    
    // 🔹 Опціональні поля
    AverageBandwidth uint32   // 🔹 Середній бітрейт (точніший за Bandwidth)
    Codecs           string   // 🔹 Кодеки: "avc1.64001f,mp4a.40.2"
    Resolution       string   // 🔹 Роздільність: "1280x720"
    
    // 🔹 Групи медіа (#EXT-X-MEDIA)
    Audio            string   // 🔹 GROUP-ID для аудіо-доріжок
    Video            string   // 🔹 GROUP-ID для відео-доріжок
    Subtitles        string   // 🔹 GROUP-ID для субтитрів
    Captions         string   // 🔹 GROUP-ID для закритих субтитрів
    
    // 🔹 Розширені параметри
    Name             string   // 🔹 Назва варіанту (Wowza/JWPlayer extension)
    Iframe           bool     // 🔹 I-frame only варіант (#EXT-X-I-FRAME-STREAM-INF)
    VideoRange       string   // 🔹 Динамічний діапазон: "SDR", "PQ", "HLG"
    HDCPLevel        string   // 🔹 Рівень захисту: "NONE", "TYPE-0", "TYPE-1"
    FrameRate        float64  // 🔹 Частота кадрів: 23.976, 30, 60
    
    // 🔹 Альтернативні доріжки
    Alternatives     []*Alternative  // 🔹 Аудіо/субтитри для цього варіанту
}
```

**🔢 Приклад використання:**
```go
variant := &Variant{
    URI: "720p/index.m3u8",
    VariantParams: VariantParams{
        Bandwidth:  2500000,  // 2.5 Mbps
        Codecs:     "avc1.64001f,mp4a.40.2",
        Resolution: "1280x720",
        Audio:      "audio-stereo",  // 🔹 Посилання на групу аудіо
        FrameRate:  30.0,
    },
}
```

---

### 🔹 `Alternative` — альтернативна медіа-доріжка (#EXT-X-MEDIA)

```go
type Alternative struct {
    GroupId         string  // 🔹 Група, до якої належить доріжка
    URI             string  // 🔹 Шлях до плейлиста доріжки
    Type            string  // 🔹 "AUDIO", "SUBTITLES", "CLOSED-CAPTIONS", "VIDEO"
    Language        string  // 🔹 Код мови: "en", "uk", "ru"
    Name            string  // 🔹 Назва для відображення: "English", "Українська"
    Default         bool    // 🔹 Чи обирати за замовчуванням
    Autoselect      string  // 🔹 "YES"/"NO": чи обирати автоматично
    Forced          string  // 🔹 "YES"/"NO": чи показувати примусово (для субтитрів)
    Characteristics string  // 🔹 Характеристики: "public.accessibility.describes-video"
    Subtitles       string  // 🔹 Посилання на групу субтитрів (для аудіо)
}
```

**🎯 Призначення**: Підтримка **мульти-мовності та доступності** у HLS-стрімінгу.

**🔢 Приклад для CCTV:**
```go
// 🔹 Аудіо-доріжка з описом для слабозорих
audioDesc := &Alternative{
    GroupId:  "audio",
    Type:     "AUDIO",
    Language: "uk",
    Name:     "Українська (аудіо-опис)",
    Default:  false,
    Characteristics: "public.accessibility.describes-video",
    URI:      "audio-description/uk/index.m3u8",
}
```

---

### 🔹 `MediaSegment` — окремий медіа-сегмент

```go
type MediaSegment struct {
    // 🔹 Ідентифікація
    SeqId    uint64   // 🔹 Послідовний номер сегмента
    URI      string   // 🔹 Шлях до файлу сегмента (.ts, .m4s)
    Title    string   // 🔹 Опційна назва (#EXTINF:duration,title)
    
    // 🔹 Таймінги
    Duration float64  // 🔹 Тривалість у секундах (#EXTINF)
    
    // 🔹 Часткове читання (#EXT-X-BYTERANGE)
    Limit    int64    // 🔹 Довжина в байтах (<n>)
    Offset   int64    // 🔹 Зсув від початку файлу (@<o>)
    
    // 🔹 Шифрування та ініціалізація
    Key      *Key     // 🔹 Ключ шифрування для цього сегмента
    Map      *Map     // 🔹 Ініціалізаційний сегмент (для fMP4)
    
    // 🔹 Контрольні прапорці
    Discontinuity bool  // 🔹 Розрив у кодуванні (#EXT-X-DISCONTINUITY)
    
    // 🔹 SCTE-35 сповіщення (реклама/події)
    SCTE     *SCTE
    
    // 🔹 Абсолютний час
    ProgramDateTime time.Time  // 🔹 Час першого семплу (#EXT-X-PROGRAM-DATE-TIME)
    
    // 🔹 Кастомні теги
    Custom   map[string]CustomTag
}
```

**🎯 Призначення**: Представлення **окремого сегмента** з усіма можливими метаданими.

---

## 🔐 Структури для шифрування та метаданих

### 🔹 `Key` — інформація про шифрування (#EXT-X-KEY)

```go
type Key struct {
    Method            string  // 🔹 Метод: "AES-128", "SAMPLE-AES", "NONE"
    URI               string  // 🔹 Шлях до ключа або ключового сервера
    IV                string  // 🔹 Вектор ініціалізації (опціонально, hex)
    Keyformat         string  // 🔹 Формат ключа: "urn:uuid:...", "identity"
    Keyformatversions string  // 🔹 Версії формату: "1"
}
```

**🔢 Приклад для CCTV з DRM:**
```go
key := &Key{
    Method:  "SAMPLE-AES",
    URI:     "https://license.server/key?id=camera_001",
    Keyformat: "urn:uuid:eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1",  // Widevine
}
```

---

### 🔹 `Map` — ініціалізаційний сегмент (#EXT-X-MAP)

```go
type Map struct {
    URI    string  // 🔹 Шлях до init-файлу (fMP4)
    Limit  int64   // 🔹 Довжина для часткового читання
    Offset int64   // 🔹 Зсув для часткового читання
}
```

**🎯 Призначення**: Вказівка на **файл ініціалізації** для фрагментованого MP4 (fMP4), який містить moov-бокс.

---

### 🔹 `SCTE` — SCTE-35 сповіщення для реклами/подій

```go
type SCTE struct {
    Syntax  SCTE35Syntax   // 🔹 Формат синтаксису
    CueType SCTE35CueType  // 🔹 Тип точки: Start/Mid/End
    Cue     string         // 🔹 Base64-кодований SCTE-35 payload
    ID      string         // 🔹 Ідентифікатор події
    Time    float64        // 🔹 Тривалість вставки у секундах
    Elapsed float64        // 🔹 Пройдений час у вставці (для Mid)
}
```

**🎯 Призначення**: Інтеграція з **кабельним ТВ/аналітикою** через стандартизовані сповіщення про події.

---

### 🔹 `WV` — Widevine-специфічні метадані

```go
type WV struct {
    AudioChannels          uint
    AudioFormat            uint
    AudioProfileIDC        uint
    AudioSampleSize        uint
    AudioSamplingFrequency uint
    CypherVersion          string
    ECM                    string  // 🔹 ECM-повідомлення для отримання ключа
    VideoFormat            uint
    VideoFrameRate         uint
    VideoLevelIDC          uint
    VideoProfileIDC        uint
    VideoResolution        string
    VideoSAR               string
}
```

**🎯 Призначення**: Підтримка **не-стандартних тегів** Google Widevine Live Packager з префіксом `#WV-*`.

---

## 🔌 Інтерфейси: Розширюваність бібліотеки

### 🔹 `Playlist` — базовий інтерфейс для всіх плейлистів

```go
type Playlist interface {
    Encode() *bytes.Buffer                    // 🔹 Генерація M3U8-рядка
    Decode(bytes.Buffer, bool) error          // 🔹 Парсинг з буфера
    DecodeFrom(reader io.Reader, bool) error  // 🔹 Парсинг з io.Reader
    WithCustomDecoders([]CustomDecoder) Playlist  // 🔹 Реєстрація кастомних декодерів
    String() string                           // 🔹 Зручний вивід
}
```

**🎯 Призначення**: Забезпечити **поліморфну обробку** Master та Media плейлистів через єдиний інтерфейс.

---

### 🔹 `CustomDecoder` / `CustomTag` — розширення через кастомні теги

```go
// 🔹 Інтерфейс для парсингу кастомних тегів
type CustomDecoder interface {
    TagName() string                              // 🔹 Ідентифікатор: "#CCTV-EVENT:"
    Decode(line string) (CustomTag, error)        // 🔹 Парсинг рядка у структуру
    SegmentTag() bool                             // 🔹 Чи прив'язаний до сегмента?
}

// 🔹 Інтерфейс для генерації кастомних тегів
type CustomTag interface {
    TagName() string              // 🔹 Ідентифікатор
    Encode() *bytes.Buffer        // 🔹 Серіалізація у рядок
    String() string               // 🔹 Зручний вивід
}
```

**🔄 Потік розширення:**
```
🔹 Користувач створює тип:
   type EventTag struct { Type string; Confidence float32 }
   
🔹 Реалізує інтерфейси:
   func (t *EventTag) TagName() string { return "#CCTV-EVENT:" }
   func (t *EventTag) Decode(line string) (CustomTag, error) { ... }
   func (t *EventTag) SegmentTag() bool { return true }
   func (t *EventTag) Encode() *bytes.Buffer { ... }
   func (t *EventTag) String() string { return t.Encode().String() }
   
🔹 Реєструє у плейлисті:
   playlist.WithCustomDecoders([]CustomDecoder{&EventTag{}})
   
🔹 Бібліотека автоматично:
   • Викликає Decode() при парсингу "#CCTV-EVENT:..."
   • Викликає Encode() при генерації плейлиста
   • Прив'язує до сегмента, якщо SegmentTag()=true
```

**🎯 Призначення**: Дозволити **розширення формату** без зміни ядра бібліотеки.

---

## 🧠 Внутрішній стан: `decodingState`

```go
type decodingState struct {
    // 🔹 Детекція типу
    listType ListType
    
    // 🔹 Прапорці поточного контексту
    m3u, tagWV, tagStreamInf, tagInf, tagSCTE35 bool
    tagRange, tagDiscontinuity, tagProgramDateTime bool
    tagKey, tagMap, tagCustom bool
    
    // 🔹 Тимчасові дані для прив'язки до сегментів
    programDateTime time.Time
    limit, offset   int64
    duration        float64
    title           string
    variant         *Variant
    alternatives    []*Alternative
    xkey            *Key
    xmap            *Map
    scte            *SCTE
    custom          map[string]CustomTag
}
```

**🎯 Призначення**: Керувати **контекстом парсингу** між рядками — критично для коректної прив'язки тегів до сегментів.

**🔄 Приклад життєвого циклу:**
```
🔹 Рядок: "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\""
   → state.xkey = &Key{Method:"AES-128", URI:"key.bin"}
   → state.tagKey = true  // 🔹 Очікуємо сегмент для прив'язки

🔹 Рядок: "#EXTINF:4.000,Motion detected"
   → state.duration = 4.0, state.title = "Motion detected"
   → state.tagInf = true  // 🔹 Очікуємо URI сегмента

🔹 Рядок: "seg_001.ts"
   → p.Append("seg_001.ts", 4.0, "Motion detected")
   → Якщо state.tagKey: p.Segments[last].Key = state.xkey
   → Скидання прапорців: state.tagInf=false, state.tagKey=false
```

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Циклічний буфер може бути складним для розуміння

```go
// 🔹 Поля для управління чергою:
head, tail, count, capacity, winsize uint
```

**🎯 Ризик**: Помилки у логіці додавання/видалення сегментів можуть призвести до втрати даних або паніки.

**✅ Рішення**: Додати методи-хелпери з документацією:
```go
// AppendSegment додає сегмент у live-плейлист з ковзним вікном
func (p *MediaPlaylist) AppendSegment(seg *MediaSegment) error {
    if p.count >= p.capacity {
        // 🔹 Розширення масиву
        newSegments := make([]*MediaSegment, p.capacity*2)
        copy(newSegments, p.Segments)
        p.Segments = newSegments
        p.capacity *= 2
    }
    
    p.Segments[p.head%p.capacity] = seg
    p.head++
    p.count++
    
    if p.winsize > 0 && p.head-p.tail > p.winsize {
        p.tail++  // 🔹 Видалення найстарішого
    }
    return nil
}
```

---

### 🔴 Проблема 2: Відсутність валідації полів при створенні структур

```go
// 🔹 Користувач може створити невалідний Key:
key := &Key{Method: "INVALID", URI: ""}  // ❌ Без валідації
```

**✅ Рішення**: Додати конструктори з валідацією:
```go
func NewKey(method, uri string) (*Key, error) {
    validMethods := map[string]bool{"AES-128": true, "SAMPLE-AES": true, "NONE": true}
    if !validMethods[method] {
        return nil, fmt.Errorf("invalid encryption method: %s", method)
    }
    if method != "NONE" && uri == "" {
        return nil, fmt.Errorf("URI required for method %s", method)
    }
    return &Key{Method: method, URI: uri}, nil
}
```

---

### 🟡 Проблема 3: `Custom` map може призвести до витоків пам'яті

```go
// 🔹 Якщо не очищати custom теги після прив'язки до сегмента:
state.custom = make(map[string]CustomTag)  // ✅ Скидання після використання
```

**✅ Рішення**: Завжди скидати `state.custom` після прив'язки до сегмента (як зроблено у `decodeLineOfMediaPlaylist`).

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Створення MediaPlaylist для live-камери

```go
func NewLiveCameraPlaylist(cameraID string, windowSize uint) *m3u8.MediaPlaylist {
    p, _ := m3u8.NewMediaPlaylist(windowSize, windowSize*2)  // 🔹winsize, capacity
    
    // 🔹 Базові налаштування
    p.SetVersion(7)
    p.SetTargetDuration(4)
    p.SetPlaylistType("event")  // 🔹 Live-подія
    p.SetIndependentSegments(true)
    
    // 🔹 Реєстрація кастомних тегів
    p.WithCustomDecoders([]m3u8.CustomDecoder{
        &CameraIDTag{ID: cameraID},
        &EventTag{},
    })
    
    // 🔹 Додавання плейлист-тегу
    p.AddCustomTag(&CameraIDTag{ID: cameraID})
    
    return p
}

// 🔹 Додавання сегмента в реальному часі
func AddSegment(p *m3u8.MediaPlaylist, uri string, duration float64, event *Event) error {
    // 🔹 Додавання сегмента
    if err := p.Append(uri, duration, ""); err != nil {
        return err
    }
    
    // 🔹 Додавання кастомного тегу події, якщо є
    if event != nil {
        p.AddCustomTagForSegment(p.Count()-1, &EventTag{
            Type:       event.Type,
            Confidence: event.Confidence,
            Timestamp:  event.Timestamp,
        })
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Генерація Master-плейлиста з адаптивними варіантами

```go
func GenerateAdaptiveMaster(cameraID string, variants []StreamVariant) (*m3u8.MasterPlaylist, error) {
    p := m3u8.NewMasterPlaylist()
    p.SetVersion(7)
    p.SetIndependentSegments(true)
    
    // 🔹 Додавання кастомного тегу камери
    p.AddCustomTag(&CameraIDTag{ID: cameraID})
    
    for _, v := range variants {
        // 🔹 Створення варіанту
        variant := &m3u8.Variant{
            URI: v.URI,
            VariantParams: m3u8.VariantParams{
                Bandwidth:  v.Bandwidth,
                Codecs:     v.Codecs,
                Resolution: v.Resolution,
                Audio:      "audio-stereo",  // 🔹 Посилання на групу аудіо
                FrameRate:  v.FrameRate,
            },
        }
        p.Variants = append(p.Variants, variant)
    }
    
    // 🔹 Додавання аудіо-доріжки
    p.AppendAlternate("audio-stereo", "AUDIO", "Українська", "audio/uk/index.m3u8", true, true)
    
    return p, nil
}
```

---

### 🔹 Приклад 3: Парсинг плейлиста з кастомними тегами

```go
func ParseCCTVPlaylist(r io.Reader) (*ParsedData, error) {
    // 🔹 Реєстрація кастомних декодерів
    customTags := []m3u8.CustomDecoder{
        &CameraIDTag{},
        &EventTag{},
        &EncryptionTag{},
    }
    
    playlist, listType, err := m3u8.DecodeWith(r, false, customTags)
    if err != nil {
        return nil, fmt.Errorf("decode failed: %w", err)
    }
    
    if listType != m3u8.MEDIA {
        return nil, fmt.Errorf("expected MEDIA playlist")
    }
    
    media := playlist.(*m3u8.MediaPlaylist)
    result := &ParsedData{
        CameraID: extractCameraID(media.Custom),
        Events:   extractEvents(media.Segments),
    }
    
    return result, nil
}

func extractCameraID(custom map[string]m3u8.CustomTag) string {
    if tag, ok := custom["#CCTV-CAMERA-ID:"]; ok {
        return tag.(*CameraIDTag).ID
    }
    return ""
}

func extractEvents(segments []*m3u8.MediaSegment) []SegmentEvent {
    var events []SegmentEvent
    for i, seg := range segments {
        if seg == nil { continue }
        if tag, ok := seg.Custom["#CCTV-EVENT:"]; ok {
            e := tag.(*EventTag)
            events = append(events, SegmentEvent{
                Index: i, Type: e.Type, Confidence: e.Confidence,
            })
        }
    }
    return events
}
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні плейлистів:
    • Використовуйте NewMediaPlaylist(winsize, capacity) для live, (0, N) для VOD
    • Встановлюйте SetIndependentSegments(true) для кращої сумісності
    • Реєструйте кастомні теги через WithCustomDecoders() перед парсингом/генерацією

[ ] Для кастомних тегів:
    • Реалізуйте всі методи інтерфейсів: TagName, Decode/Encode, SegmentTag, String
    • Використовуйте унікальні префікси: #CCTV-*, #MYAPP-* для уникнення конфліктів
    • Валідуйте атрибути у Decode() з чіткими помилками

[ ] Для шифрування:
    • Використовуйте конструктор NewKey() з валідацією методу та URI
    • Для Widevine: заповнюйте Keyformat та Keyformatversions
    • Для fMP4: додавайте Map з URI init-файлу

[ ] Для live-стрімінгу:
    • Налаштовуйте winsize > 0 для ковзного вікна
    • Використовуйте SetPlaylistType("event") для live
    • Очищайте state.custom після прив'язки до сегмента

[ ] Для тестування:
    • Тестуйте round-trip: Generate → Parse → порівняння даних
    • Перевіряйте поведінку циклічного буфера при додаванні > capacity сегментів
    • Тестуйте кастомні теги з різними форматами атрибутів
```

---

## 🎯 Висновок

> **Цей модуль — "ДНК" бібліотеки m3u8**, який забезпечує:
> • ✅ Типобезпечне представлення всіх елементів HLS-специфікації
> • ✅ Ефективне керування пам'яттю через циклічні буфери для live-плейлистів
> • ✅ Гнучке розширення через інтерфейси CustomDecoder/CustomTag
> • ✅ Підтримку шифрування, SCTE-35, Widevine та інших розширень
> • ✅ Чітке розділення Master/Media логіки через інтерфейс Playlist

Для вашого **CCTV HLS Processor** це означає:
- 🎯 Типобезпечне створення та маніпуляція плейлистами для камер
- 📡 Ефективна підтримка live-стрімінгу з ковзним вікном сегментів
- 🔐 Надійне керування шифруванням та DRM-метаданими
- 🔄 Легке розширення через кастомні теги для маркування подій, камер, аналітики
- 🛡️ Безпека: валідація полів та чіткі інтерфейси запобігають помилкам

Потребуєте допомоги з реалізацією специфічних структур для ваших сценаріїв або з оптимізацією керування пам'яттю для великих live-плейлистів? Напишіть — покажу готовий код для вашого випадку! 🚀🏗️