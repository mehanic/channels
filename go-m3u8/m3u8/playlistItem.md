# 🔍 Глибокий розбір коду: `PlaylistItem` для HLS Master Playlist

Цей код реалізує роботу з тегами **`#EXT-X-STREAM-INF`** та **`#EXT-X-I-FRAME-STREAM-INF`** — ключовими елементами **Master Playlist**, які описують доступні варіанти якості відео для адаптивного стрімінгу. Розберемо детально.

---

## 📦 Що таке `PlaylistItem` і навіщо він потрібен?

### Контекст: Master Playlist
```m3u8
#EXTM3U
#EXT-X-VERSION:7

#EXT-X-STREAM-INF:BANDWIDTH=1280000,AVERAGE-BANDWIDTH=1000000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1280x720,FRAME-RATE=30.0,AUDIO="audio",SUBTITLES="subs"
video/720p.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=2560000,AVERAGE-BANDWIDTH=2000000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1920x1080,FRAME-RATE=30.0,AUDIO="audio",SUBTITLES="subs"
video/1080p.m3u8

#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=256000,CODECS="avc1.640028",URI="video/1080p_iframe.m3u8",RESOLUTION=1920x1080
```

### Призначення `PlaylistItem`
| Поле | Атрибут HLS | Призначення | Приклад |
|------|-------------|-------------|---------|
| `Bandwidth` | `BANDWIDTH` | ✅ Обов'язковий: пікова бітрейт у бітах/сек | `1280000` = 1.28 Mbps |
| `AverageBandwidth` | `AVERAGE-BANDWIDTH` | Середній бітрейт для точнішого ABR | `1000000` |
| `Resolution` | `RESOLUTION` | Роздільна здатність: Ширина×Висота | `1280x720` |
| `FrameRate` | `FRAME-RATE` | Частота кадрів (для плавності) | `30.0`, `59.94` |
| `Codecs` | `CODECS` | Кодеки відео/аудіо (RFC 6381) | `"avc1.640028,mp4a.40.2"` |
| `Audio`/`Subtitles` | `AUDIO`/`SUBTITLES` | Посилання на групи медіа (GROUP-ID) | `"audio"`, `"subs"` |
| `URI` | (після тега) | Посилання на media-плейлист цього варіанту | `video/720p.m3u8` |
| `IFrame` | (прапорець) | Чи це `#EXT-X-I-FRAME-STREAM-INF` | `true` = тільки iframe-сегменти |

### 🎯 Навіщо це критично для адаптивного стрімінгу?
```
Клієнтський плеєр (напр. hls.js, AVPlayer):
1️⃣ Завантажує Master Playlist
2️⃣ Аналізує PlaylistItem:
   • BANDWIDTH → порівнює з доступною пропускною здатністю
   • CODECS → перевіряє підтримку пристроєм
   • RESOLUTION → враховує розмір екрану
   • FRAME-RATE → обирає плавність
3️⃣ Обирає оптимальний варіант → завантажує відповідний media-плейлист
4️⃣ Динамічно перемикається при зміні мережі (ABR алгоритм)
```

---

## 🏗️ Struct `PlaylistItem` — повна карта атрибутів

```go
type PlaylistItem struct {
    // 🔥 Обов'язкові поля
    Bandwidth int      // Піковий бітрейт (біт/сек)
    URI       string   // Посилання на media-плейлист
    IFrame    bool     // Прапорець: I-Frame variant
    
    // 🎨 Візуальні параметри
    Name             *string      // Людиноподібне ім'я варіанту
    Width            *int         // Ширина (застаріле, краще Resolution)
    Height           *int         // Висота (застаріле)
    Resolution       *Resolution  // Структура {Width, Height}
    
    // 📊 Бітрейт та продуктивність
    AverageBandwidth *int         // Середній бітрейт (точніший для ABR)
    FrameRate        *float64     // Кадри в секунду (30.0, 59.94)
    
    // 🔤 Кодеки та профілі
    ProgramID        *string      // Застарілий ідентифікатор програми
    Codecs           *string      // Повний рядок кодеків (RFC 6381)
    AudioCodec       *string      // Тільки аудіо-кодек (для авто-генерації)
    Profile          *string      // H.264 profile: "baseline", "main", "high"
    Level            *string      // H.264 level: "3.0", "4.1", "5.2"
    
    // 🔗 Групи медіа (посилання на #EXT-X-MEDIA)
    Video            *string      // GROUP-ID для відео-доріжок
    Audio            *string      // GROUP-ID для аудіо-доріжок
    Subtitles        *string      // GROUP-ID для субтитрів
    ClosedCaptions   *string      // "CC1", "NONE" — вбудовані субтитри
    
    // 🔐 DRM та стабільність
    HDCPLevel        *string      // Вимоги до захисту: "TYPE-0", "TYPE-1", "NONE"
    StableVariantID  *string      // Стабільний ID для аналітики/кешування
}
```

### 🎯 Чому так багато `*string` / `*int` / `*float64`?
```go
// Специфікація HLS (RFC 8216) вимагає:
// • Атрибут відсутній → nil → НЕ виводити у серіалізації
// • Атрибут присутній → виводити зі значенням

// Приклад для FrameRate:
// • FrameRate=nil  → не виводиться → плеєр використовує дефолт
// • FrameRate=&30.0 → виводиться "FRAME-RATE=30.0" → явна вказівка

// Це критично для:
// ✓ Сумісності зі старими плеєрами (які не підтримують нові атрибути)
// ✓ Мінімізації розміру Master Playlist (менше атрибутів = швидше завантаження)
// ✓ Гнучкості: можна додавати атрибути поступово
```

---

## 🔧 Конструктор `NewPlaylistItem` — парсинг з валідацією

```go
func NewPlaylistItem(text string) (*PlaylistItem, error) {
    // Крок 1: Парсинг атрибутів з рядка
    // Вхід: 'BANDWIDTH=1280000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1280x720'
    // Вихід: map[string]string{"BANDWIDTH": "1280000", "CODECS": "avc1.640028,mp4a.40.2", ...}
    attributes := ParseAttributes(text)

    // Крок 2: Парсинг Resolution (спеціальний формат "WxH")
    resolution, err := parseResolution(attributes, ResolutionTag)
    if err != nil {
        return nil, err  // Помилка: невірний формат, напр. "1280-720"
    }
    var width, height *int
    if resolution != nil {
        // 🔄 Зворотна сумісність: заповнюємо старі поля Width/Height
        width = &resolution.Width
        height = &resolution.Height
    }

    // Крок 3: Парсинг опціональних числових полів
    averageBandwidth, err := parseInt(attributes, AverageBandwidthTag)  // *int або nil
    if err != nil {
        return nil, err
    }

    frameRate, err := parseFloat(attributes, FrameRateTag)  // *float64 або nil
    if err != nil {
        return nil, err
    }
    // ✅ Валідація: FrameRate має бути > 0
    if frameRate != nil && *frameRate <= 0 {
        frameRate = nil  // Ігноруємо невалідне значення
    }

    // Крок 4: Парсинг обов'язкового Bandwidth
    bandwidth, err := parseBandwidth(attributes, BandwidthTag)
    if err != nil {
        return nil, err  // Помилка: відсутній або невірний формат
    }

    // Крок 5: Побудова об'єкта
    return &PlaylistItem{
        // Опціональні рядки: pointerTo повертає *string або nil
        ProgramID:        pointerTo(attributes, ProgramIDTag),
        Codecs:           pointerTo(attributes, CodecsTag),
        AudioCodec:       pointerTo(attributes, AudioCodecTag),
        Profile:          pointerTo(attributes, ProfileTag),
        Level:            pointerTo(attributes, LevelTag),
        Video:            pointerTo(attributes, VideoTag),
        Audio:            pointerTo(attributes, AudioTag),
        Subtitles:        pointerTo(attributes, SubtitlesTag),
        ClosedCaptions:   pointerTo(attributes, ClosedCaptionsTag),
        Name:             pointerTo(attributes, NameTag),
        HDCPLevel:        pointerTo(attributes, HDCPLevelTag),
        StableVariantID:  pointerTo(attributes, StableVariantIDTag),
        
        // Числові поля (вже розпарсені з валідацією)
        Width:            width,
        Height:           height,
        Bandwidth:        bandwidth,  // ✅ Обов'язковий
        AverageBandwidth: averageBandwidth,
        FrameRate:        frameRate,
        Resolution:       resolution,
        
        // URI: завжди string (може бути порожнім, але це валідується пізніше)
        URI: attributes[URITag],
    }, nil
}
```

### 🔍 Helper-функції (припустима реалізація)

```go
// parseBandwidth: парсинг обов'язкового BANDWIDTH
func parseBandwidth(attributes map[string]string, key string) (int, error) {
    bw, ok := attributes[key]
    if !ok {
        return 0, ErrBandwidthMissing  // ✅ Чітка помилка для відсутнього обов'язкового поля
    }
    bandwidth, err := strconv.ParseInt(bw, 0, 0)  // Base 0 = авто-визначення (10, 16, 8)
    if err != nil {
        return 0, ErrBandwidthInvalid  // ✅ Чітка помилка для невалідного формату
    }
    return int(bandwidth), nil
}

// parseResolution: парсинг "WxH" → *Resolution
func parseResolution(attributes map[string]string, key string) (*Resolution, error) {
    resolution, ok := attributes[key]
    if !ok {
        return nil, nil  // ✅ Опціональний атрибут: відсутність = nil
    }
    return NewResolution(resolution)  // Делегуємо парсинг "1280x720" → Resolution{1280, 720}
}

// pointerTo: універсальний helper для опціональних рядків
func pointerTo(attrs map[string]string, key string) *string {
    if v, ok := attrs[key]; ok && v != "" {
        return &v
    }
    return nil
}
```

---

## 🔄 Метод `String()` — серіалізація з логікою

```go
func (pi *PlaylistItem) String() string {
    var slice []string
    
    // 🎯 Логіка 1: зворотна сумісність Resolution ↔ Width/Height
    if pi.Resolution == nil && pi.Width != nil && pi.Height != nil {
        r := &Resolution{Width: *pi.Width, Height: *pi.Height}
        pi.Resolution = r  // ⚠️ Побічний ефект: модифікує стан об'єкта!
    }
    
    // 🎯 Логіка 2: додавання атрибутів тільки якщо вони не-nil
    if pi.ProgramID != nil {
        slice = append(slice, fmt.Sprintf(formatString, ProgramIDTag, *pi.ProgramID))
    }
    if pi.Resolution != nil {
        // Resolution має власний String(): "1280x720"
        slice = append(slice, fmt.Sprintf(formatString, ResolutionTag, pi.Resolution.String()))
    }
    
    // 🎯 Логіка 3: складна генерація CODECS
    codecs := formatCodecs(pi)  // Об'єднує Codecs/Profile/Level/AudioCodec
    if codecs != nil {
        // CODECS завжди в лапках за специфікацією
        slice = append(slice, fmt.Sprintf(quotedFormatString, CodecsTag, *codecs))
    }
    
    // 🎯 Логіка 4: обов'язковий BANDWIDTH
    slice = append(slice, fmt.Sprintf(formatString, BandwidthTag, pi.Bandwidth))
    
    // 🎯 Логіка 5: опціональні числові поля
    if pi.AverageBandwidth != nil {
        slice = append(slice, fmt.Sprintf(formatString, AverageBandwidthTag, *pi.AverageBandwidth))
    }
    if pi.FrameRate != nil {
        // FRAME-RATE має спеціальний формат (може бути дробовим)
        slice = append(slice, fmt.Sprintf(frameRateFormatString, FrameRateTag, *pi.FrameRate))
    }
    
    // 🎯 Логіка 6: медіа-групи та інші опціональні атрибути
    if pi.Audio != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, AudioTag, *pi.Audio))
    }
    // ... аналогічно для Video, Subtitles, Name, тощо
    
    // 🎯 Логіка 7: спеціальна обробка ClosedCaptions
    if pi.ClosedCaptions != nil {
        cc := *pi.ClosedCaptions
        fs := quotedFormatString
        if cc == NoneValue {  // "NONE" не має лапок за специфікацією!
            fs = formatString
        }
        slice = append(slice, fmt.Sprintf(fs, ClosedCaptionsTag, cc))
    }
    
    // 🎯 Фінальна збірка атрибутів
    attributesString := strings.Join(slice, ",")
    
    // 🎯 Логіка 8: різний формат для I-Frame vs звичайний варіант
    if pi.IFrame {
        // #EXT-X-I-FRAME-STREAM-INF:ATTRS,URI="..."
        return fmt.Sprintf(`%s:%s,%s="%s"`, PlaylistIframeTag, attributesString, URITag, pi.URI)
    }
    // #EXT-X-STREAM-INF:ATTRS\nURI
    return fmt.Sprintf("%s:%s\n%s", PlaylistItemTag, attributesString, pi.URI)
}
```

### 🎯 Формат виводу
```m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1280000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1280x720,FRAME-RATE=30.0,AUDIO="audio",SUBTITLES="subs"
video/720p.m3u8

#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=256000,CODECS="avc1.640028",RESOLUTION=1920x1080,URI="video/1080p_iframe.m3u8"
```

---

## 🧩 Helper: `formatCodecs` — розумна генерація рядка кодеків

```go
func formatCodecs(pi *PlaylistItem) *string {
    // Пріоритет 1: якщо Codecs вже вказаний явно — використовуємо його
    if pi.Codecs != nil {
        return pi.Codecs
    }

    // Пріоритет 2: авто-генерація з Profile/Level + AudioCodec
    videoCodecPtr := videoCodec(pi.Profile, pi.Level)  // "avc1.640028" з "high"/"4.1"
    
    // ❌ Якщо Profile/Level вказані, але не розпізнані → помилка конфігурації
    if !(pi.Profile == nil && pi.Level == nil) && videoCodecPtr == nil {
        return nil  // Не генеруємо CODECS, щоб уникнути невалідного значення
    }

    audioCodecPtr := audioCodec(pi.AudioCodec)  // "mp4a.40.2" з "aac"
    
    // ❌ Якщо AudioCodec вказаний, але не розпізнаний → помилка
    if !(pi.AudioCodec == nil) && audioCodecPtr == nil {
        return nil
    }

    // 🎯 Збірка фінального рядка
    var slice []string
    if videoCodecPtr != nil {
        slice = append(slice, *videoCodecPtr)
    }
    if audioCodecPtr != nil {
        slice = append(slice, *audioCodecPtr)
    }

    if len(slice) <= 0 {
        return nil  // Немає кодеків → не виводити атрибут
    }

    value := strings.Join(slice, ",")  // "avc1.640028,mp4a.40.2"
    return &value
}
```

### 🎯 Приклади роботи `formatCodecs`
| Вхідні дані | Результат | Пояснення |
|-------------|-----------|-----------|
| `Codecs="avc1.640028"` | `"avc1.640028"` | Явний Codecs має пріоритет |
| `Profile="high", Level="4.1"` | `"avc1.640028"` | Авто-генерація з профілю |
| `Profile="invalid"` | `nil` | Невідомий профіль → не генеруємо |
| `AudioCodec="aac"` | `"mp4a.40.2"` | Авто-генерація аудіо-кодеку |
| `Profile="high", AudioCodec="aac"` | `"avc1.640028,mp4a.40.2"` | Об'єднання відео + аудіо |

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Побічний ефект у `String()`: модифікація стану
```go
// ❌ Поточний код:
if pi.Resolution == nil && pi.Width != nil && pi.Height != nil {
    r := &Resolution{Width: *pi.Width, Height: *pi.Height}
    pi.Resolution = r  // ⚠️ Змінює об'єкт під час серіалізації!
}

// Проблема:
// • String() має бути "читанням", а не "записом"
// • При конкурентному доступі → race condition
// • Несподівана поведінка: після String() об'єкт змінюється

// ✅ Рішення: не модифікувати стан, використовувати локальну змінну
func (pi *PlaylistItem) String() string {
    resolution := pi.Resolution
    if resolution == nil && pi.Width != nil && pi.Height != nil {
        resolution = &Resolution{Width: *pi.Width, Height: *pi.Height}
    }
    // ... використовувати resolution замість pi.Resolution
}
```

### 2️⃣ Відсутність валідації URI
```go
// ❌ URI може бути порожнім → невалідний плейлист
return &PlaylistItem{
    URI: attributes[URITag],  // "" якщо атрибут відсутній
    // ...
}

// ✅ Додати валідацію
if uri := attributes[URITag]; uri == "" {
    return nil, fmt.Errorf("EXT-X-STREAM-INF requires URI")
}
```

### 3️⃣ Магічні константи у формат-рядках
```go
// ❌ Непрозорі змінні:
// formatString = "%s=%d"
// quotedFormatString = `%s="%s"`
// frameRateFormatString = "%s=%.3f"

// ✅ Винести в константи з документацією:
const (
    // formatString: для числових/енум атрибутів без лапок
    // Приклад: "BANDWIDTH=1280000"
    formatString = "%s=%d"
    
    // quotedFormatString: для рядкових атрибутів у лапках
    // Приклад: 'CODECS="avc1.640028"'
    quotedFormatString = `%s="%s"`
    
    // frameRateFormatString: для FRAME-RATE з фіксованою точністю
    // Приклад: "FRAME-RATE=30.000"
    frameRateFormatString = "%s=%.3f"
)
```

### 4️⃣ Обробка помилок у `parseInt`/`parseFloat`
```go
// ❌ Якщо helper повертає nil при помилці парсингу — важко відлагодити
averageBandwidth, err := parseInt(attributes, AverageBandwidthTag)
if err != nil {
    return nil, err  // ✅ Добре: помилка прокидається вгору
}

// ✅ Рекомендація: додавати контекст до помилок
if err != nil {
    return nil, fmt.Errorf("failed to parse %s: %w", AverageBandwidthTag, err)
}
```

### 5️⃣ Thread-safety при спільному доступі
```go
// ❌ У вашому pipeline (генерация Master Playlist + WebSocket broadcast):
item := &PlaylistItem{Bandwidth: 1280000}
pl.AppendItem(item)  // Горутина 1
s := item.String()   // Горутина 2: читання тих самих полів → DATA RACE!

// ✅ Рішення: додати sync.RWMutex або використовувати immutable патерн
type SafePlaylistItem struct {
    mu sync.RWMutex
    PlaylistItem
}

func (si *SafePlaylistItem) String() string {
    si.mu.RLock()
    defer si.mu.RUnlock()
    // ... серіалізація
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **багатоваріантним стрімінгом**:

### 🎯 Сценарій: генерація Master Playlist з адаптивними варіантами
```go
func generateMasterPlaylist(channelID string, variants []VideoVariant) *m3u8.Playlist {
    pl := m3u8.NewPlaylist()
    master := true
    pl.Master = &master
    pl.Version = pointer(7)  // fMP4 support
    
    // 🎯 Додавання варіантів якості
    for _, v := range variants {
        item := &m3u8.PlaylistItem{
            Bandwidth:  v.Bandwidth,  // ✅ Обов'язковий
            URI:        v.URI,        // ✅ Обов'язковий
            Resolution: &m3u8.Resolution{Width: v.Width, Height: v.Height},
            FrameRate:  pointer(v.FrameRate),
            Codecs:     pointer(v.Codecs),  // "avc1.640028,mp4a.40.2"
            Audio:      pointer("audio"),   // Посилання на групу аудіо
            Subtitles:  pointer("subs"),    // Посилання на групу субтитрів
        }
        pl.AppendItem(item)
    }
    
    // 🎯 Додавання I-Frame варіантів (для швидкого перемотування)
    for _, v := range variants {
        if v.SupportsIFrames {
            item := &m3u8.PlaylistItem{
                Bandwidth:  v.IFrameBandwidth,
                URI:        v.IFrameURI,
                Resolution: &m3u8.Resolution{Width: v.Width, Height: v.Height},
                Codecs:     pointer(strings.Split(v.Codecs, ",")[0]),  // Тільки відео-кодек
                IFrame:     true,  // ✅ Ключовий прапорець!
            }
            pl.AppendItem(item)
        }
    }
    
    return pl
}
```

### 🎯 Сценарій: динамічне оновлення при зміні кодека
```go
// У segmentFinalizer при виявленні зміни кодека:
func (sf *SegmentFinalizer) handleCodecChange(newCodec string) {
    // 🔄 Оновити Master Playlist з новим CODECS
    for i, item := range sf.masterPlaylist.Items {
        if pi, ok := item.(*m3u8.PlaylistItem); ok {
            sf.masterPlaylist.Items[i] = &m3u8.PlaylistItem{
                Bandwidth:  pi.Bandwidth,
                URI:        pi.URI,
                Resolution: pi.Resolution,
                Codecs:     pointer(newCodec),  // ✅ Оновлений кодек
                // ... інші поля зберігаємо
            }
        }
    }
    // 📢 Повідомити клієнтів про оновлення (WebSocket)
    sf.broadcastMasterUpdate()
}
```

### 🎯 Сценарій: фільтрація варіантів за можливостями клієнта
```go
// У WebSocketDistributor при підключенні нового клієнта:
func (d *Distributor) filterVariants(clientCaps ClientCapabilities, items []*m3u8.PlaylistItem) []*m3u8.PlaylistItem {
    var filtered []*m3u8.PlaylistItem
    for _, item := range items {
        // 🎯 Фільтр за кодеками
        if item.Codecs != nil && !clientCaps.SupportsCodecs(*item.Codecs) {
            continue
        }
        // 🎯 Фільтр за роздільною здатністю
        if item.Resolution != nil {
            if item.Resolution.Width > clientCaps.MaxWidth || 
               item.Resolution.Height > clientCaps.MaxHeight {
                continue
            }
        }
        // 🎯 Фільтр за бітрейтом (для мобільних мереж)
        if item.Bandwidth > clientCaps.MaxBandwidth {
            continue
        }
        filtered = append(filtered, item)
    }
    return filtered
}
```

---

## 🧪 Приклад використання: повний цикл

```go
// ✅ Створення PlaylistItem для 720p варіанту
item720 := &m3u8.PlaylistItem{
    Bandwidth:  1280000,
    URI:        "video/720p.m3u8",
    Resolution: &m3u8.Resolution{Width: 1280, Height: 720},
    FrameRate:  pointer(30.0),
    Codecs:     pointer("avc1.640028,mp4a.40.2"),
    Audio:      pointer("audio"),
    Subtitles:  pointer("subs"),
}
fmt.Println(item720.String())
/*
#EXT-X-STREAM-INF:BANDWIDTH=1280000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1280x720,FRAME-RATE=30.000,AUDIO="audio",SUBTITLES="subs"
video/720p.m3u8
*/

// ✅ Парсинг вхідного рядка
line := `BANDWIDTH=2560000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1920x1080,FRAME-RATE=30.0`
item, err := m3u8.NewPlaylistItem(line)
if err != nil {
    log.Fatal(err)
}
fmt.Println(item.Bandwidth)           // 2560000
fmt.Println(item.Resolution.Width)    // 1920
fmt.Println(*item.FrameRate)          // 30.0

// ✅ Авто-генерація CODECS з Profile/Level
itemAuto := &m3u8.PlaylistItem{
    Bandwidth:  512000,
    URI:        "video/480p.m3u8",
    Profile:    pointer("main"),
    Level:      pointer("3.1"),
    AudioCodec: pointer("aac"),
}
fmt.Println(itemAuto.String())
/*
#EXT-X-STREAM-INF:BANDWIDTH=512000,CODECS="avc1.4d401f,mp4a.40.2"
video/480p.m3u8
*/
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги

```
✅ BANDWIDTH — обов'язковий, ціле число > 0, у бітах за секунду
✅ URI — обов'язковий, відносний або абсолютний URL до media-плейлиста
✅ CODECS — має відповідати RFC 6381, у лапках, розділені комами
✅ RESOLUTION — формат "WxH", обидва цілі числа > 0
✅ FRAME-RATE — додатне число, може бути дробовим (59.94)
✅ AUDIO/SUBTITLES — мають збігатися з GROUP-ID у #EXT-X-MEDIA
✅ CLOSED-CAPTIONS — "NONE" (без лапок) або ідентифікатор ("CC1")
✅ I-Frame варіанти:
   • Мають окремий тег #EXT-X-I-FRAME-STREAM-INF
   • CODECS має містити ТІЛЬКИ відео-кодек (без аудіо)
   • Містять тільки сегменти з ключовими кадрами
✅ Порядок атрибутів не регламентований, але рекомендується:
   BANDWIDTH, AVERAGE-BANDWIDTH, CODECS, RESOLUTION, FRAME-RATE, ...
```

---

## 🎯 Висновок

Цей код — **потужний інструмент** для побудови професійних HLS Master Playlist:

✅ Повна підтримка атрибутів RFC 8216  
✅ Розумна авто-генерація `CODECS` з `Profile/Level`  
✅ Гнучка робота з опціональними полями через `*T`  
✅ Чітке розділення звичайних та I-Frame варіантів

**Для вашого CCTV-процесора — критичні рекомендації**:

1. 🔒 Уникати побічних ефектів у `String()` → не модифікувати стан об'єкта
2. ✅ Додати валідацію `URI` у конструкторі
3. 🎯 Використовувати `AverageBandwidth` для точнішого ABR у нестабільних мережах
4. 🔄 Кешувати результат `formatCodecs()` при частій серіалізації
5. 📊 Додати метрики: які варіанти обирають клієнти (для оптимізації бітрейтів)

**Приклад оптимізації для low-latency CCTV**:
```go
// Для моніторингу в реальному часі:
item := &m3u8.PlaylistItem{
    Bandwidth:        800000,        // Нижчий бітрейт = швидше завантаження
    AverageBandwidth: pointer(600000), // Точніша оцінка для ABR
    Resolution:       &m3u8.Resolution{Width: 854, Height: 480}, // 480p достатньо для моніторингу
    FrameRate:        pointer(15.0), // Нижча частота = менший бітрейт
    Codecs:           pointer("avc1.64001f,mp4a.40.2"),
    // ... інші поля
}
// → Клієнти швидше перемикаються на цей варіант при поганій мережі
```

Потрібно допомогти з:
- 🔗 Інтеграцією `PlaylistItem` у динамічну генерацію Master Playlist?
- 🧠 Реалізацією `videoCodec()`/`audioCodec()` helper-функцій?
- 🧪 Юніт-тестами для `formatCodecs()` з різними комбінаціями входу?

Чекаю на ваші питання! 🛠️