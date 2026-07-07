# 🔍 Глибокий розбір: Константи пакету `m3u8` для HLS

Цей файл — **єдине джерело правди** для всіх тегів, атрибутів та значень формату HLS (RFC 8216). Він забезпечує type-safe роботу з плейлистами, уникнення магічних рядків та централізоване управління специфікацією.

---

## 📦 Архітектура файлу: логічне групування констант

```go
// 🗂️ Структура за категоріями:
const (
    // 1️⃣ ITEM TAGS — теги, що представляють окремі елементи плейлиста
    SessionKeyItemTag    = `#EXT-X-SESSION-KEY`    // DRM-ключі (Master)
    KeyItemTag           = `#EXT-X-KEY`            // DRM-ключі (Media)
    DiscontinuityItemTag = `#EXT-X-DISCONTINUITY`  // Розрив у потоці
    TimeItemTag          = `#EXT-X-PROGRAM-DATE-TIME` // Абсолютний час
    DateRangeItemTag     = `#EXT-X-DATERANGE`      // Діапазон дат (події)
    MapItemTag           = `#EXT-X-MAP`            // Init-файл для fMP4
    SessionDataItemTag   = `#EXT-X-SESSION-DATA`   // Метадані сесії
    SegmentItemTag       = `#EXTINF`               // Сегмент: тривалість+URI
    ByteRangeItemTag     = `#EXT-X-BYTERANGE`      // Часткове завантаження
    PlaybackStartTag     = `#EXT-X-START`          // Точка старту відтворення
    MediaItemTag         = `#EXT-X-MEDIA`          // Аудіо/субтитри групи
    PlaylistItemTag      = `#EXT-X-STREAM-INF`     // Варіант якості
    PlaylistIframeTag    = `#EXT-X-I-FRAME-STREAM-INF` // I-Frame варіант

    // 2️⃣ PLAYLIST TAGS — метадані всього плейлиста
    HeaderTag                = `#EXTM3U`                  // Обов'язковий заголовок
    FooterTag                = `#EXT-X-ENDLIST`           // Кінець VOD-плейлиста
    TargetDurationTag        = `#EXT-X-TARGETDURATION`    // Макс. тривалість сегмента
    CacheTag                 = `#EXT-X-ALLOW-CACHE`       // Дозвіл кешування
    DiscontinuitySequenceTag = `#EXT-X-DISCONTINUITY-SEQUENCE` // Лічильник розривів
    IndependentSegmentsTag   = `#EXT-X-INDEPENDENT-SEGMENTS`  // Незалежні сегменти
    PlaylistTypeTag          = `#EXT-X-PLAYLIST-TYPE`     // VOD/EVENT
    IFramesOnlyTag           = `#EXT-X-I-FRAMES-ONLY`     // Тільки iframe-сегменти
    MediaSequenceTag         = `#EXT-X-MEDIA-SEQUENCE`    // Номер першого сегмента
    VersionTag               = `#EXT-X-VERSION`           // Версія специфікації

    // 3️⃣ ATTRIBUTE TAGS — ключі для атрибутів у тегах
    // (використовуються всередині #EXT-X-...:ATTR1=val1,ATTR2=val2)
    
    // 🔐 Encryptable (для KEY/SESSION-KEY)
    MethodTag            = "METHOD"             // AES-128, SAMPLE-AES
    URITag               = "URI"                // Посилання на ресурс
    IVTag                = "IV"                 // Вектор ініціалізації (hex)
    KeyFormatTag         = "KEYFORMAT"          // DRM-система (FairPlay/Widevine)
    KeyFormatVersionsTag = "KEYFORMATVERSIONS"  // Версія формату ключа
    
    // 📅 DateRange (для подій/реклами)
    IDTag              = "ID"                   // Унікальний ідентифікатор
    ClassTag           = "CLASS"                // Категорія події
    StartDateTag       = "START-DATE"           // Початок (RFC3339)
    EndDateTag         = "END-DATE"             // Кінець (RFC3339)
    DurationTag        = "DURATION"             // Тривалість (секунди)
    PlannedDurationTag = "PLANNED-DURATION"     // Запланована тривалість
    Scte35CmdTag       = "SCTE35-CMD"           // SCTE-35 команда (hex)
    Scte35OutTag       = "SCTE35-OUT"           // Початок рекламного блоку
    Scte35InTag        = "SCTE35-IN"            // Кінець рекламного блоку
    EndOnNextTag       = "END-ON-NEXT"          // Завершити на наступному
    
    // 🎬 PlaybackStart
    TimeOffsetTag = "TIME-OFFSET"  // Зміщення старту (секунди)
    PreciseTag    = "PRECISE"      // Точність (YES/NO)
    
    // 📊 SessionData
    DataIDTag   = "DATA-ID"    // Унікальний ідентифікатор даних
    ValueTag    = "VALUE"      // Вбудоване значення
    LanguageTag = "LANGUAGE"   // Код мови (RFC5646)
    
    // 🎵 MediaItem (аудіо/субтитри)
    TypeTag              = "TYPE"              // AUDIO/VIDEO/SUBTITLES/CLOSED-CAPTIONS
    GroupIDTag           = "GROUP-ID"          // Ідентифікатор групи
    AssocLanguageTag     = "ASSOC-LANGUAGE"    // Мова асоціації
    NameTag              = "NAME"              // Людиноподібне ім'я
    AutoSelectTag        = "AUTOSELECT"        // Автовибір плеєром (YES/NO)
    DefaultTag           = "DEFAULT"           // Вибрати за замовчуванням (YES/NO)
    ForcedTag            = "FORCED"            // Показувати примусово (субтитри)
    InStreamIDTag        = "INSTREAM-ID"       // ID вбудованих субтитрів (CC1)
    CharacteristicsTag   = "CHARACTERISTICS"   // Accessibility характеристики
    ChannelsTag          = "CHANNELS"          // Кількість аудіоканалів ("2", "6")
    StableRenditionIDTag = "STABLE-RENDITION-ID" // Стабільний ID для аналітики
    
    // 🎞️ PlaylistItem (варіанти якості)
    ResolutionTag       = "RESOLUTION"        // "1920x1080"
    ProgramIDTag        = "PROGRAM-ID"        // Застарілий ідентифікатор
    CodecsTag           = "CODECS"            // "avc1.640028,mp4a.40.2"
    BandwidthTag        = "BANDWIDTH"         // Піковий бітрейт (біт/сек)
    AverageBandwidthTag = "AVERAGE-BANDWIDTH" // Середній бітрейт
    FrameRateTag        = "FRAME-RATE"        // Кадри в секунду
    VideoTag            = "VIDEO"             // GROUP-ID для відео
    AudioTag            = "AUDIO"             // GROUP-ID для аудіо
    SubtitlesTag        = "SUBTITLES"         // GROUP-ID для субтитрів
    ClosedCaptionsTag   = "CLOSED-CAPTIONS"   // Вбудовані субтитри
    HDCPLevelTag        = "HDCP-LEVEL"        // Вимоги до захисту
    StableVariantIDTag  = "STABLE-VARIANT-ID" // Стабільний ID варіанту

    // 4️⃣ VALUES — стандартні значення атрибутів
    NoneValue = "NONE"  // Для ClosedCaptions, METHOD
    YesValue  = "YES"   // Для булевих атрибутів
    NoValue   = "NO"    // Для булевих атрибутів
)
```

---

## 🎯 Навіщо виділяти константи? Переваги підходу

### ✅ 1. Type-safety та уникнення опечаток
```go
// ❌ Без констант (магічні рядки):
attrs["BANDWIDHT"] = "1280000"  // Опечатка! Плеєр проігнорує

// ✅ З константами (помилка виявиться на етапі компіляції):
attrs[m3u8.BandwidthTag] = "1280000"  // Компілятор підкаже, якщо BandwidthTag не існує
```

### ✅ 2. Централізоване оновлення специфікації
```go
// Якщо RFC 8216 змінить назву атрибута:
// ❌ Треба шукати по всьому коду: grep -r "BANDWIDTH"
// ✅ Змінити в одному місці:
const BandwidthTag = "NEW-BANDWIDTH-NAME"  // Всі використання оновляться автоматично
```

### ✅ 3. Self-documenting код
```go
// Читач одразу розуміє призначення:
if method == m3u8.NoneValue {  // "NONE" = шифрування вимкнено
    // ...
}

// Замість:
if method == "NONE" {  // Що означає "NONE"? Де це документовано?
    // ...
}
```

### ✅ 4. Легкий рефакторинг та пошук
```bash
# Знайти всі використання атрибута CODECS:
grep -r "CodecsTag" .  # → 15 файлів

# Знайти всі магічні рядки "CODECS" (важче, можна пропустити):
grep -r '"CODECS"' .   # → Може бути в коментарях, тестах, логах...
```

---

## 🔗 Як константи використовуються у коді: приклади

### Приклад 1: Парсинг атрибутів у `NewPlaylistItem`
```go
func NewPlaylistItem(text string) (*PlaylistItem, error) {
    attributes := ParseAttributes(text)  // map[string]string
    
    // ✅ Використання констант для доступу до атрибутів
    bandwidth, err := parseBandwidth(attributes, BandwidthTag)  // "BANDWIDTH"
    if err != nil {
        return nil, err
    }
    
    resolution, err := parseResolution(attributes, ResolutionTag)  // "RESOLUTION"
    if err != nil {
        return nil, err
    }
    
    return &PlaylistItem{
        Bandwidth:  bandwidth,
        Resolution: resolution,
        Codecs:     pointerTo(attributes, CodecsTag),  // "CODECS"
        // ...
    }, nil
}
```

### Приклад 2: Серіалізація у `String()` методах
```go
func (pi *PlaylistItem) String() string {
    var slice []string
    
    // ✅ Використання констант для форматування
    slice = append(slice, fmt.Sprintf(formatString, BandwidthTag, pi.Bandwidth))
    // Результат: "BANDWIDTH=1280000"
    
    if pi.Resolution != nil {
        slice = append(slice, fmt.Sprintf(formatString, ResolutionTag, pi.Resolution.String()))
        // Результат: "RESOLUTION=1920x1080"
    }
    
    if pi.Codecs != nil {
        // ✅ Quoted формат для рядкових значень
        slice = append(slice, fmt.Sprintf(quotedFormatString, CodecsTag, *pi.Codecs))
        // Результат: `CODECS="avc1.640028,mp4a.40.2"`
    }
    
    return fmt.Sprintf("%s:%s", PlaylistItemTag, strings.Join(slice, ","))
    // Результат: #EXT-X-STREAM-INF:BANDWIDTH=1280000,...
}
```

### Приклад 3: Валідація булевих значень
```go
func parseYesNo(attrs map[string]string, key string) *bool {
    v, ok := attrs[key]
    if !ok {
        return nil
    }
    
    // ✅ Порівняння зі стандартними значеннями
    switch strings.ToUpper(v) {
    case YesValue:  // "YES"
        b := true
        return &b
    case NoValue:  // "NO"
        b := false
        return &b
    default:
        return nil  // Невалідне значення
    }
}
```

### Приклад 4: Фільтрація за типом медіа
```go
func filterAudioTracks(items []*MediaItem, lang string) []*MediaItem {
    var result []*MediaItem
    for _, item := range items {
        // ✅ Порівняння типів через константи
        if item.Type == TypeTag &&  // "TYPE" атрибут
           *item.Language == lang {
            result = append(result, item)
        }
    }
    return result
}
```

---

## 🗂️ Детальний огляд категорій констант

### 🔹 1. Item Tags — теги елементів плейлиста
| Константа | Тег | Де використовується | Призначення |
|-----------|-----|---------------------|-------------|
| `SegmentItemTag` | `#EXTINF` | Media Playlist | Опис сегмента: тривалість + URI |
| `KeyItemTag` | `#EXT-X-KEY` | Media Playlist | Ключ шифрування для сегментів |
| `SessionKeyItemTag` | `#EXT-X-SESSION-KEY` | **Master Playlist** | Ключ шифрування для всієї сесії |
| `MapItemTag` | `#EXT-X-MAP` | Media Playlist (fMP4) | Init-файл з метаданими контейнера |
| `DiscontinuityItemTag` | `#EXT-X-DISCONTINUITY` | Media Playlist | Позначає розрив у таймлайні (зміна кодека, часу) |
| `TimeItemTag` | `#EXT-X-PROGRAM-DATE-TIME` | Media Playlist | Абсолютний UTC-час початку сегмента |
| `MediaItemTag` | `#EXT-X-MEDIA` | **Master Playlist** | Опис аудіо/субтитр доріжок |
| `PlaylistItemTag` | `#EXT-X-STREAM-INF` | **Master Playlist** | Опис варіанту якості відео |

### 🔹 2. Playlist Tags — метадані плейлиста
| Константа | Тег | Обов'язковий? | Призначення |
|-----------|-----|---------------|-------------|
| `HeaderTag` | `#EXTM3U` | ✅ Так | Ідентифікує файл як M3U8 плейлист |
| `VersionTag` | `#EXT-X-VERSION` | ⚠️ Рекомендовано | Версія специфікації (3-7+) |
| `TargetDurationTag` | `#EXT-X-TARGETDURATION` | ✅ Так | Макс. тривалість сегмента (для ABR) |
| `MediaSequenceTag` | `#EXT-X-MEDIA-SEQUENCE` | ✅ Для live | Номер першого сегмента у "ковзному вікні" |
| `PlaylistTypeTag` | `#EXT-X-PLAYLIST-TYPE` | ❌ Ні | `VOD` = весь контент доступний, `EVENT` = тільки додавання |
| `FooterTag` | `#EXT-X-ENDLIST` | ❌ Ні | Ознака завершення плейлиста (VOD) |

### 🔹 3. Attribute Tags — ключі атрибутів
#### 🔐 Шифрування (Encryptable)
```go
// Використовується в #EXT-X-KEY та #EXT-X-SESSION-KEY
MethodTag            // "METHOD=AES-128" — алгоритм шифрування
URITag               // "URI=\"https://keys.com/key.bin\"" — де взяти ключ
IVTag                // "IV=0x1234..." — вектор ініціалізації (опціонально)
KeyFormatTag         // "KEYFORMAT=\"com.apple.streamingkeydelivery\"" — DRM-система
KeyFormatVersionsTag // "KEYFORMATVERSIONS=\"1\"" — версія формату
```

#### 📅 DateRange (події/реклама)
```go
// Використовується в #EXT-X-DATERANGE для маркування подій
IDTag              // Унікальний ідентифікатор події
ClassTag           // Категорія: "com.example.ad", "com.example.program"
StartDateTag       // Початок події: "2024-01-01T12:00:00Z"
EndDateTag         // Кінець події (опціонально)
DurationTag        // Тривалість у секундах (опціонально)
Scte35OutTag       // Маркер початку рекламного блоку (SCTE-35)
Scte35InTag        // Маркер кінця рекламного блоку
```

#### 🎵 MediaItem атрибути
```go
// Для #EXT-X-MEDIA: опис аудіо/субтитр доріжок
TypeTag              // "AUDIO", "SUBTITLES" — тип медіа
GroupIDTag           // "audio", "subs" — для прив'язки до варіантів
NameTag              // "English", "Arabic" — відображення в плеєрі
LanguageTag          // "en", "ar" — для автовибору за мовою пристрою
DefaultTag           // "YES" — обрати за замовчуванням
AutoSelectTag        // "YES" — дозволити плеєру обирати автоматично
ForcedTag            // "YES" — показувати субтитри для іноземних частин
ChannelsTag          // "2", "6" — кількість аудіоканалів
```

#### 🎞️ PlaylistItem атрибути
```go
// Для #EXT-X-STREAM-INF: опис варіантів якості
BandwidthTag         // Піковий бітрейт (обов'язковий для ABR)
ResolutionTag        // "1920x1080" — роздільна здатність
CodecsTag            // "avc1.640028,mp4a.40.2" — підтримка кодеками
FrameRateTag         // "30.0", "59.94" — частота кадрів
AudioTag/SubtitlesTag // Посилання на GROUP-ID медіа-груп
```

### 🔹 4. Стандартні значення (Values)
```go
NoneValue = "NONE"  // • ClosedCaptions="NONE" = немає вбудованих субтитрів
                    // • METHOD="NONE" = шифрування вимкнено
                    
YesValue  = "YES"   // • DEFAULT=YES, AUTOSELECT=YES, FORCED=YES
NoValue   = "NO"    // • DEFAULT=NO, AUTOSELECT=NO, FORCED=NO
```

---

## ⚠️ Потенційні покращення файлу констант

### 1️⃣ Додати константи для форматів значень
```go
// ✅ Зараз формати "закодовані" у коді:
fmt.Sprintf("%.3f", duration)  // Чому 3 знаки? Де це документовано?

// ✅ Рішення: додати константи форматів
const (
    DurationFormat      = "%.3f"   // Тривалість: 3 знаки після коми
    FrameRateFormat     = "%.3f"   // FrameRate: 3 знаки
    BandwidthFormat     = "%d"     // Bandwidth: ціле число
    ResolutionFormat    = "%dx%d"  // Resolution: "WxH"
    HexIVFormat         = "0x%032s" // IV: 32 hex-символи з префіксом
)
```

### 2️⃣ Додати константи для допустимих значень ENUM
```go
// ✅ Для валідації METHOD у шифруванні:
const (
    MethodAES128    = "AES-128"
    MethodSampleAES = "SAMPLE-AES"
    MethodNone      = "NONE"
)

// ✅ Для валідації TYPE у MediaItem:
const (
    TypeAudio           = "AUDIO"
    TypeVideo           = "VIDEO"
    TypeSubtitles       = "SUBTITLES"
    TypeClosedCaptions  = "CLOSED-CAPTIONS"
)

// ✅ Для валідації PlaylistType:
const (
    PlaylistTypeVOD   = "VOD"
    PlaylistTypeEvent = "EVENT"
)

// Використання:
func validateMethod(method string) error {
    switch method {
    case MethodAES128, MethodSampleAES, MethodNone:
        return nil
    default:
        return fmt.Errorf("invalid METHOD: %s", method)
    }
}
```

### 3️⃣ Додати константи для помилок
```go
// ✅ Зараз помилки створюються "на льоту":
return nil, fmt.Errorf("invalid BANDWIDTH value")

// ✅ Рішення: централізовані помилки для кращого логування/моніторингу:
var (
    ErrBandwidthMissing   = errors.New("BANDWIDTH attribute is required")
    ErrBandwidthInvalid   = errors.New("BANDWIDTH must be a positive integer")
    ErrResolutionInvalid  = errors.New("RESOLUTION must be WxH format")
    ErrMethodInvalid      = errors.New("METHOD must be AES-128, SAMPLE-AES, or NONE")
    // ...
)

// Використання:
if bw, ok := attrs[BandwidthTag]; !ok {
    return nil, ErrBandwidthMissing
}
```

### 4️⃣ Додати коментарі з посиланнями на RFC
```go
// ✅ Покращена документація:
const (
    // #EXT-X-VERSION: вказує сумісну версію специфікації HLS
    // RFC 8216 §4.3.1.2: https://datatracker.ietf.org/doc/html/rfc8216#section-4.3.1.2
    // Допустимі значення: 1-7+
    VersionTag = `#EXT-X-VERSION`
    
    // #EXT-X-TARGETDURATION: максимальна тривалість будь-якого медіа-сегмента
    // RFC 8216 §4.3.2.1: https://datatracker.ietf.org/doc/html/rfc8216#section-4.3.2.1
    // Має бути цілим числом ≥ тривалості найдовшого сегмента
    TargetDurationTag = `#EXT-X-TARGETDURATION`
)
```

### 5️⃣ Групування через iota для пов'язаних констант
```go
// ✅ Для ENUM-подібних значень можна використати iota:
type MediaType string
const (
    TypeAudio MediaType = "AUDIO"
    TypeVideo           = "VIDEO"       // "VIDEO"
    TypeSubtitles       = "SUBTITLES"   // "SUBTITLES"
    TypeClosedCaptions  = "CLOSED-CAPTIONS"
)

// ✅ Але для рядкових тегів краще явні значення (як зараз):
// • Читабельність при дебазі
// • Безпека при рефакторингу
// • Сумісність зі специфікацією
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури:

### 🎯 Сценарій: генерація Media Playlist з правильними тегами
```go
func (sf *SegmentFinalizer) generatePlaylist(channelID string, segments []Segment) string {
    var buf strings.Builder
    
    // ✅ Обов'язковий заголовок
    buf.WriteString(m3u8.HeaderTag + "\n")
    buf.WriteString(fmt.Sprintf("%s:%d\n", m3u8.VersionTag, 7))  // fMP4 support
    
    // ✅ TargetDuration: максимум з тривалостей сегментів
    buf.WriteString(fmt.Sprintf("%s:%d\n", m3u8.TargetDurationTag, sf.targetDuration))
    
    // ✅ MediaSequence для live-ковзного вікна
    buf.WriteString(fmt.Sprintf("%s:%d\n", m3u8.MediaSequenceTag, sf.sequence))
    
    // ✅ EXT-X-MAP для fMP4 ініціалізації
    if sf.initURI != "" {
        buf.WriteString(fmt.Sprintf("%s:%s=\"%s\"\n", 
            m3u8.MapItemTag, m3u8.URITag, sf.initURI))
    }
    
    // ✅ Додавання сегментів
    for _, seg := range segments {
        // ProgramDateTime для синхронізації
        if !seg.StartTime.IsZero() {
            buf.WriteString(fmt.Sprintf("%s:%s\n", 
                m3u8.TimeItemTag, seg.StartTime.Format(time.RFC3339Nano)))
        }
        
        // EXTINF: тривалість + опціональний коментар
        comment := ""
        if seg.Title != "" {
            comment = "," + seg.Title
        }
        buf.WriteString(fmt.Sprintf("%s:%.3f%s\n%s\n",
            m3u8.SegmentItemTag, seg.Duration, comment, seg.URI))
    }
    
    // ✅ Для VOD: додати ENDLIST
    if !sf.isLive {
        buf.WriteString(m3u8.FooterTag + "\n")
    }
    
    return buf.String()
}
```

### 🎯 Сценарій: валідація вхідних даних через константи
```go
func validateMediaItem(attrs map[string]string) error {
    // ✅ Перевірка обов'язкових атрибутів
    if attrs[m3u8.TypeTag] == "" {
        return fmt.Errorf("%s attribute is required", m3u8.TypeTag)
    }
    if attrs[m3u8.GroupIDTag] == "" {
        return fmt.Errorf("%s attribute is required", m3u8.GroupIDTag)
    }
    if attrs[m3u8.NameTag] == "" {
        return fmt.Errorf("%s attribute is required", m3u8.NameTag)
    }
    
    // ✅ Валідація TYPE
    validTypes := map[string]bool{
        m3u8.TypeAudio: true, m3u8.TypeVideo: true,
        m3u8.TypeSubtitles: true, m3u8.TypeClosedCaptions: true,
    }
    if !validTypes[attrs[m3u8.TypeTag]] {
        return fmt.Errorf("invalid %s: %s", m3u8.TypeTag, attrs[m3u8.TypeTag])
    }
    
    // ✅ Валідація булевих атрибутів
    for _, key := range []string{m3u8.DefaultTag, m3u8.AutoSelectTag, m3u8.ForcedTag} {
        if v, ok := attrs[key]; ok && v != "" {
            if v != m3u8.YesValue && v != m3u8.NoValue {
                return fmt.Errorf("%s must be %s or %s, got: %s", 
                    key, m3u8.YesValue, m3u8.NoValue, v)
            }
        }
    }
    
    return nil
}
```

### 🎯 Сценарій: фільтрація варіантів за підтримкою кодеків
```go
func selectCompatibleVariant(clientCodecs []string, items []*PlaylistItem) *PlaylistItem {
    for _, item := range items {
        if item.Codecs == nil {
            continue  // Пропускаємо варіанти без вказаних кодеків
        }
        
        // ✅ Розбиття CODECS за комою та перевірка сумісності
        requiredCodecs := strings.Split(*item.Codecs, ",")
        if isSubset(clientCodecs, requiredCodecs) {
            return item  // Перший сумісний варіант
        }
    }
    return nil  // Жоден варіант не підходить
}

// Helper: чи є required підмножиною available
func isSubset(available, required []string) bool {
    set := make(map[string]bool)
    for _, c := range available {
        set[strings.TrimSpace(c)] = true
    }
    for _, c := range required {
        if !set[strings.TrimSpace(c)] {
            return false
        }
    }
    return true
}
```

---

## 🧪 Приклад: повний цикл використання констант

```go
// ✅ Створення Master Playlist з багатомовними доріжками
func generateMaster(channelID string) string {
    var buf strings.Builder
    buf.WriteString(m3u8.HeaderTag + "\n")
    buf.WriteString(fmt.Sprintf("%s:%d\n", m3u8.VersionTag, 7))
    
    // 🎵 Аудіо-доріжки (AR/EN/UK)
    for _, lang := range []struct{code, name string}{
        {"ar", "Arabic"}, {"en", "English"}, {"uk", "Ukrainian"},
    } {
        attrs := []string{
            fmt.Sprintf("%s=%s", m3u8.TypeTag, m3u8.TypeAudio),
            fmt.Sprintf("%s=\"%s\"", m3u8.GroupIDTag, "audio"),
            fmt.Sprintf("%s=\"%s\"", m3u8.NameTag, lang.name),
            fmt.Sprintf("%s=\"%s\"", m3u8.LanguageTag, lang.code),
            fmt.Sprintf("%s=%s", m3u8.DefaultTag, 
                map[bool]string{true: m3u8.YesValue, false: m3u8.NoValue}[lang.code == "ar"]),
            fmt.Sprintf("%s=%s", m3u8.AutoSelectTag, m3u8.YesValue),
            fmt.Sprintf("%s=\"/channels/%s/audio/%s.m3u8\"", m3u8.URITag, channelID, lang.code),
        }
        buf.WriteString(fmt.Sprintf("%s:%s\n", m3u8.MediaItemTag, strings.Join(attrs, ",")))
    }
    
    // 🎞️ Варіанти якості
    variants := []struct{bw int; res string; uri string}{
        {800000, "854x480", "video/480p.m3u8"},
        {2500000, "1280x720", "video/720p.m3u8"},
        {5000000, "1920x1080", "video/1080p.m3u8"},
    }
    for _, v := range variants {
        attrs := []string{
            fmt.Sprintf("%s=%d", m3u8.BandwidthTag, v.bw),
            fmt.Sprintf("%s=\"%s\"", m3u8.ResolutionTag, v.res),
            fmt.Sprintf("%s=\"%s,%s\"", m3u8.CodecsTag, "avc1.64001f", "mp4a.40.2"),
            fmt.Sprintf("%s=\"%s\"", m3u8.AudioTag, "audio"),
        }
        buf.WriteString(fmt.Sprintf("%s:%s\n%s\n", 
            m3u8.PlaylistItemTag, strings.Join(attrs, ","), v.uri))
    }
    
    return buf.String()
}
```

---

## 📋 RFC 8216: критичні вимоги до тегів

```
✅ #EXTM3U — перший рядок будь-якого M3U8 файлу
✅ #EXT-X-VERSION — має бути ≥3 для базового HLS, ≥7 для fMP4/DATERANGE
✅ #EXT-X-TARGETDURATION — має бути ≥ тривалості будь-якого #EXTINF
✅ #EXT-X-MEDIA-SEQUENCE — має зростати монотонно у live-плейлистах
✅ #EXT-X-KEY / #EXT-X-SESSION-KEY:
   • METHOD — обов'язковий (AES-128, SAMPLE-AES, NONE)
   • URI — обов'язковий якщо METHOD != NONE
   • KEYFORMAT — обов'язковий для SAMPLE-AES (DRM)
✅ #EXT-X-MEDIA:
   • TYPE, GROUP-ID, NAME — обов'язкові
   • Якщо DEFAULT=YES, то максимум один елемент у групі
✅ #EXT-X-STREAM-INF:
   • BANDWIDTH — обов'язковий
   • CODECS, RESOLUTION — рекомендовані для ABR
✅ #EXT-X-DATERANGE:
   • ID — обов'язковий, унікальний в межах плейлиста
   • START-DATE — обов'язковий, RFC3339 формат
✅ Значення булевих атрибутів: ТІЛЬКИ "YES" або "NO" (чутливо до регістру)
```

---

## 🎯 Висновок

Цей файл констант — **фундамент type-safe роботи з HLS**:

✅ Уникнення магічних рядків та опечаток  
✅ Централізоване управління специфікацією  
✅ Self-documenting код для нових розробників  
✅ Легкий рефакторинг при оновленні RFC 8216

**Для вашого проекту — рекомендовані покращення**:

1. ✅ Додати ENUM-константи для `METHOD`, `TYPE`, `PlaylistType`
2. ✅ Додати константи форматів (`DurationFormat`, `HexIVFormat`)
3. ✅ Централізувати помилки валідації (`ErrBandwidthMissing` тощо)
4. ✅ Додати коментарі з посиланнями на відповідні секції RFC 8216
5. ✅ Розглянути генерацію констант зі специфікації (code generation)

**Приклад оптимізації для CCTV-моніторингу**:
```go
// Low-latency профіль: короткі сегменти + точні таймштампи
const (
    CCTVTargetDuration = 4      // 4-секундні сегменти
    CCTVSegmentFormat  = "%.3f" // Мілісекундна точність для синхронізації
    CCTVTimeFormat     = time.RFC3339Nano  // Найвища точність часу
)

// Використання:
buf.WriteString(fmt.Sprintf("%s:%d\n", m3u8.TargetDurationTag, CCTVTargetDuration))
buf.WriteString(fmt.Sprintf("%s:%s\n", m3u8.TimeItemTag, 
    seg.StartTime.Format(CCTVTimeFormat)))
buf.WriteString(fmt.Sprintf("%s:"+CCTVSegmentFormat+",\n", 
    m3u8.SegmentItemTag, seg.Duration))
```

Потрібно допомогти з:
- 🧠 Генерацією констант автоматично зі специфікації RFC 8216?
- 🔗 Інтеграцією валідації через ENUM-константи у ваш парсер?
- 🧪 Юніт-тестами для перевірки сумісності констант зі специфікацією?

Чекаю на ваші питання! 🛠️📋