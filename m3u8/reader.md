# 🎬 `m3u8/parser.go`: Ядро парсингу HLS-плейлистів

Це **центральний модуль** бібліотеки `github.com/grafov/m3u8`, який реалізує повнофункціональний парсер для форматів **Master** та **Media** плейлистів HLS (HTTP Live Streaming) з підтримкою кастомних тегів, суворої валідації та гнучкого налаштування.

---

## 🎯 Коротка відповідь

> **Це "мозок" бібліотеки m3u8**: він перетворює текстові M3U8-файли у типобезпечні Go-структури, підтримує розширення через кастомні теги, обробляє обидва типи плейлистів (Master/Media) та надає контроль над суворістю парсингу — ідеально для інтеграції CCTV-метаданих у ваш HLS-конвеєр.

---

## 🧱 Архітектура парсингу: Огляд компонентів

### 🔹 Типи плейлистів

```go
type ListType int
const (
    MASTER ListType = iota  // 🔹 Master playlist: варіанти якості, аудіо-доріжки
    MEDIA                   // 🔹 Media playlist: послідовність сегментів .ts/.m4s
)
```

**🎯 Призначення**: Розрізняти логіку парсингу для двох різних форматів:
- ✅ **Master**: `#EXT-X-STREAM-INF`, `#EXT-X-MEDIA`, варіанти бітрейту
- ✅ **Media**: `#EXTINF`, `#EXT-X-KEY`, `#EXT-X-DISCONTINUITY`, сегменти

---

### 🔹 Інтерфейс `Playlist` та реалізації

```go
type Playlist interface {
    Encode() *bytes.Buffer
    String() string
    // ... інші методи
}

type MasterPlaylist struct { /* варіанти стріму, аудіо-доріжки */ }
type MediaPlaylist struct { /* сегменти, ключі, таймінги */ }
```

**🎯 Призначення**: Забезпечити **поліморфну обробку** плейлистів через єдиний інтерфейс.

---

## 🔍 Ключові функції парсингу

### 🔹 `DecodeWith` — універсальний вхід з підтримкою кастомних тегів

```go
func DecodeWith(input interface{}, strict bool, customDecoders []CustomDecoder) (Playlist, ListType, error)
```

| Параметр | Тип | Призначення |
|----------|-----|-------------|
| `input` | `bytes.Buffer` або `io.Reader` | Джерело даних (файл, мережа, буфер) |
| `strict` | `bool` | Чи зупинятися на першій помилці синтаксису |
| `customDecoders` | `[]CustomDecoder` | Список кастомних парсерів тегів |

**🔄 Потік даних:**
```
🔹 Вхід: io.Reader + customDecoders
│
▼
🔹 Читання всього вмісту у bytes.Buffer
│
▼
🔹 Виклик decode() з ініціалізацією стану:
   • decodingState{} — трекер поточного контексту парсингу
   • custom map — реєстр кастомних тегів
│
▼
🔹 Построчний парсинг:
   • decodeLineOfMasterPlaylist() → для MASTER
   • decodeLineOfMediaPlaylist() → для MEDIA
   • Перевірка кастомних тегів ПЕРЕД стандартними
│
▼
🔹 Визначення типу плейлиста через state.listType
│
▼
🔹 Повернення: Playlist, ListType, error
```

**🎯 Призначення**: Забезпечити **єдину точку входу** для парсингу з підтримкою розширень.

---

### 🔹 `decode()` — внутрішня логіка детекції типу

```go
func decode(buf *bytes.Buffer, strict bool, customDecoders []CustomDecoder) (Playlist, ListType, error)
```

**🔑 Ключові особливості:**
- ✅ **Паралельний парсинг**: кожен рядок перевіряється і для Master, і для Media (ефективно для невідомих типів)
- ✅ **Авто-детекція**: `state.listType` встановлюється при зустрічі першого специфічного тегу
- ✅ **Реєстрація кастомних декодерів**: `WithCustomDecoders()` ініціалізує `p.customDecoders`
- ✅ **Обробка порожніх рядків**: `if len(line) < 1 || line == "\r" { continue }`

**⚠️ Важливо**: Функція створює **обидва типи плейлистів** (`master` та `media`), але повертає тільки відповідний до `state.listType`.

---

### 🔹 `decodeLineOfMasterPlaylist` — парсинг Master-плейлиста

```go
func decodeLineOfMasterPlaylist(p *MasterPlaylist, state *decodingState, line string, strict bool) error
```

**📋 Підтримувані теги:**

| Тег | Призначення | Приклад для CCTV |
|-----|-------------|-----------------|
| `#EXT-X-VERSION` | Версія специфікації | `#EXT-X-VERSION:7` |
| `#EXT-X-STREAM-INF` | Опис варіанту якості | `BANDWIDTH=2500000,RESOLUTION=1280x720` |
| `#EXT-X-MEDIA` | Аудіо/субтитри доріжки | `TYPE=AUDIO,GROUP-ID="audio",NAME="English"` |
| `#EXT-X-I-FRAME-STREAM-INF` | I-frame only варіанти | Для швидкого seek у записях |
| `#EXT-X-INDEPENDENT-SEGMENTS` | Незалежні сегменти | Для кращої сумісності |

**🔹 Приклад парсингу `#EXT-X-STREAM-INF`:**
```go
case !state.tagStreamInf && strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
    state.tagStreamInf = true
    state.listType = MASTER
    state.variant = new(Variant)
    p.Variants = append(p.Variants, state.variant)
    
    for k, v := range decodeParamsLine(line[18:]) {
        switch k {
        case "BANDWIDTH":
            val, _ := strconv.Atoi(v)
            state.variant.Bandwidth = uint32(val)
        case "CODECS":
            state.variant.Codecs = v  // "avc1.64001f,mp4a.40.2"
        case "RESOLUTION":
            state.variant.Resolution = v  // "1280x720"
        // ... інші атрибути
        }
    }
```

---

### 🔹 `decodeLineOfMediaPlaylist` — парсинг Media-плейлиста

```go
func decodeLineOfMediaPlaylist(p *MediaPlaylist, wv *WV, state *decodingState, line string, strict bool) error
```

**📋 Підтримувані теги:**

| Тег | Призначення | Приклад для CCTV |
|-----|-------------|-----------------|
| `#EXTINF` | Тривалість сегмента | `#EXTINF:4.000,Camera 1 - Motion` |
| `#EXT-X-KEY` | Шифрування сегментів | `METHOD=AES-128,URI="keys/key.bin"` |
| `#EXT-X-MAP` | Ініціалізаційний сегмент (fMP4) | `URI="init.mp4"` |
| `#EXT-X-DISCONTINUITY` | Розрив у послідовності | При зміні камери або налаштувань |
| `#EXT-X-PROGRAM-DATE-TIME` | Абсолютний час сегмента | Для синхронізації з подіями |
| `#EXT-X-BYTERANGE` | Часткове читання файлу | Для ефективного seek |
| `#EXT-SCTE35*` | Сповіщення про події | Інтеграція з кабельним ТВ/аналітикою |

**🔹 Приклад парсингу `#EXTINF` + сегмент:**
```go
case !state.tagInf && strings.HasPrefix(line, "#EXTINF:"):
    state.tagInf = true
    state.listType = MEDIA
    sepIndex := strings.Index(line, ",")
    duration := line[8:sepIndex]  // "4.000"
    state.duration, _ = strconv.ParseFloat(duration, 64)
    if len(line) > sepIndex {
        state.title = line[sepIndex+1:]  // "Camera 1 - Motion"
    }

case !strings.HasPrefix(line, "#"):  // ← URL сегмента
    if state.tagInf {
        p.Append(line, state.duration, state.title)  // "seg_001.ts", 4.0, "Camera 1..."
        state.tagInf = false
    }
    // 🔹 Прив'язка попередніх тегів до цього сегмента:
    if state.tagKey { p.Segments[p.last()].Key = state.xkey }
    if state.tagMap { p.Segments[p.last()].Map = state.xmap }
    if state.tagCustom { p.Segments[p.last()].Custom = state.custom }
```

**🎯 Ключова логіка**: Теги, що з'являються **перед** `#EXTINF`, автоматично прив'язуються до наступного сегмента.

---

## 🔐 Підтримка кастомних тегів

### 🔹 Реєстрація та парсинг

```go
// 🔹 У decodeLineOfMediaPlaylist (аналогічно для Master):
if p.Custom != nil {
    for _, v := range p.customDecoders {
        if strings.HasPrefix(line, v.TagName()) {
            t, err := v.Decode(line)
            if strict && err != nil { return err }
            
            if v.SegmentTag() {
                state.tagCustom = true
                state.custom[v.TagName()] = t  // 🔹 Тимчасове збереження
            } else {
                p.Custom[v.TagName()] = t      // 🔹 Глобальний тег плейлиста
            }
        }
    }
}
```

**🔄 Потік для сегмент-тегів:**
```
🔹 Рядок: "#CCTV-EVENT:TYPE=motion,CONFIDENCE=0.95"
│
▼
🔹 Перевірка: strings.HasPrefix(line, "#CCTV-EVENT:") → true
│
▼
🔹 Виклик: customDecoder.Decode(line) → &EventTag{Type:"motion", Confidence:0.95}
│
▼
🔹 Перевірка: customDecoder.SegmentTag() → true
│
▼
🔹 Збереження: state.custom["#CCTV-EVENT:"] = tag
│
▼
🔹 При наступному сегменті (після #EXTINF + URL):
   p.Segments[p.last()].Custom = state.custom  // 🔹 Прив'язка до сегмента
   state.custom = make(map[string]CustomTag)    // 🔹 Скидання для наступного
```

**🎯 Призначення**: Дозволити **розширення формату** без зміни ядра бібліотеки.

---

### 🔹 `DecodeAttributeList` — парсинг атрибутів

```go
func DecodeAttributeList(line string) map[string]string {
    return decodeParamsLine(line)
}

func decodeParamsLine(line string) map[string]string {
    out := make(map[string]string)
    // 🔹 Regex: KEY="value" або KEY=value
    for _, kv := range reKeyValue.FindAllStringSubmatch(line, -1) {
        k, v := kv[1], kv[2]
        out[k] = strings.Trim(v, ` "`)  // 🔹 Видалення лапок та пробілів
    }
    return out
}
```

**🔢 Приклад:**
```
🔹 Вхід: "NAME=\"alarm\",CONFIDENCE=0.95,TIMESTAMP=1705320000"
🔹 Regex match: 
   • ["NAME=\"alarm\"", "NAME", "\"alarm\""]
   • ["CONFIDENCE=0.95", "CONFIDENCE", "0.95"]
   • ["TIMESTAMP=1705320000", "TIMESTAMP", "1705320000"]
🔹 Вихід: map[string]string{
      "NAME": "alarm",
      "CONFIDENCE": "0.95",
      "TIMESTAMP": "1705320000",
   }
```

**🎯 Призначення**: Уніфікувати парсинг атрибутів для всіх тегів (`#EXT-X-STREAM-INF`, `#EXT-X-KEY`, кастомні теги).

---

## 🕐 Парсинг часу: `TimeParse`

```go
var TimeParse func(value string) (time.Time, error) = FullTimeParse

func FullTimeParse(value string) (time.Time, error) {
    layouts := []string{
        "2006-01-02T15:04:05.999999999Z0700",
        "2006-01-02T15:04:05.999999999Z07:00",
        "2006-01-02T15:04:05.999999999Z07",
    }
    // 🔹 Спроба парсингу за кожним форматом
    for _, layout := range layouts {
        if t, err := time.Parse(layout, value); err == nil {
            return t, nil
        }
    }
    return time.Time{}, err
}
```

**🎯 Призначення**: Підтримка **різних форматів дат** у `#EXT-X-PROGRAM-DATE-TIME`:
- ✅ `2024-01-15T10:30:00.123456789Z` (UTC з наносекундами)
- ✅ `2024-01-15T10:30:00+02:00` (часовий пояс)
- ✅ `2024-01-15T10:30:00Z` (UTC без дробової частини)

**🔧 Гнучкість**: Змінна `TimeParse` дозволяє перевизначити парсер глобально:
```go
// 🔹 Використання суворого RFC3339 парсера:
m3u8.TimeParse = m3u8.StrictTimeParse
```

---

## 🧠 State machine: `decodingState`

```go
type decodingState struct {
    m3u                bool  // 🔹 Чи зустрінуто #EXTM3U
    listType           ListType  // 🔹 MASTER або MEDIA
    tagStreamInf       bool  // 🔹 Чи в середині парсингу #EXT-X-STREAM-INF
    tagInf             bool  // 🔹 Чи очікуємо URL сегмента після #EXTINF
    tagKey             bool  // 🔹 Чи є активний #EXT-X-KEY
    tagMap             bool  // 🔹 Чи є активний #EXT-X-MAP
    tagCustom          bool  // 🔹 Чи є кастомні теги для поточного сегмента
    duration           float64  // 🔹 Тривалість з #EXTINF
    title              string   // 🔹 Назва сегмента з #EXTINF
    xkey               *Key     // 🔹 Поточний ключ шифрування
    xmap               *Map     // 🔹 Поточна ініціалізаційна карта
    custom             map[string]CustomTag  // 🔹 Кастомні теги для сегмента
    alternatives       []*Alternative  // 🔹 Аудіо/субтитри доріжки
    variant            *Variant  // 🔹 Поточний варіант якості
    // ... інші поля
}
```

**🎯 Призначення**: Керувати **контекстом парсингу** між рядками — критично для коректної прив'язки тегів до сегментів.

**🔄 Приклад життєвого циклу:**
```
🔹 Рядок 1: "#EXTM3U" → state.m3u = true
🔹 Рядок 2: "#EXT-X-VERSION:7" → state.listType = MEDIA
🔹 Рядок 3: "#EXT-X-TARGETDURATION:4" → p.TargetDuration = 4.0
🔹 Рядок 4: "#EXTINF:4.000,Motion detected" → state.tagInf=true, duration=4.0, title="Motion..."
🔹 Рядок 5: "seg_001.ts" → p.Append("seg_001.ts", 4.0, "Motion..."), state.tagInf=false
🔹 Рядок 6: "#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"" → state.xkey=..., state.tagKey=true
🔹 Рядок 7: "#EXTINF:4.000" → state.tagInf=true
🔹 Рядок 8: "seg_002.ts" → p.Append(...), p.Segments[last].Key = state.xkey, state.tagKey=false
```

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Паралельний парсинг Master/Media

```go
// 🔹 Поточний код:
err = decodeLineOfMasterPlaylist(master, state, line, strict)
err = decodeLineOfMediaPlaylist(media, wv, state, line, strict)
```

**🎯 Ризик**: Кожен рядок парситься **двічі**, що збільшує накладні витрати на ~2×.

**✅ Рішення**: Детектувати тип плейлиста на основі перших рядків, потім парсити тільки відповідну функцію:
```go
if state.listType == UNKNOWN && isMasterIndicator(line) {
    state.listType = MASTER
}
if state.listType == MASTER {
    err = decodeLineOfMasterPlaylist(...)
} else {
    err = decodeLineOfMediaPlaylist(...)
}
```

---

### 🔴 Проблема 2: Ігнорування помилок `strconv` у non-strict режимі

```go
// 🔹 Поточний код:
val, err := strconv.Atoi(v)
if strict && err != nil { return err }  // ← Помилка ігнорується, якщо !strict
// val = 0 при помилці → некоректні дані
```

**🎯 Ризик**: Невалідні значення (`BANDWIDTH=abc`) призводять до `0` без попередження → некоректна логіка вибору якості.

**✅ Рішення**: Логувати попередження навіть у non-strict режимі:
```go
val, err := strconv.Atoi(v)
if err != nil {
    if strict { return err }
    log.Printf("⚠️  Invalid BANDWIDTH value %q: %v", v, err)
    continue  // ← Пропустити невалідний атрибут
}
```

---

### 🟡 Проблема 3: Хардкод індексів у `decodeParamsLine`

```go
// 🔹 Поточний код:
for k, v := range decodeParamsLine(line[18:])  // ← 18 = len("#EXT-X-STREAM-INF:")
```

**🎯 Ризик**: Зміна формату тегу (напр., додання пробілу) ламає парсинг.

**✅ Рішення**: Використовувати `strings.TrimPrefix` або константи:
```go
const streamInfPrefix = "#EXT-X-STREAM-INF:"
for k, v := range decodeParamsLine(strings.TrimPrefix(line, streamInfPrefix))
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Парсинг плейлиста з кастомними тегами подій

```go
// 🔹 Структура для зберігання розпаршених даних
type ParsedMediaPlaylist struct {
    CameraID   string
    Events     []SegmentEvent
    Duration   float64
    Closed     bool
}

type SegmentEvent struct {
    Index      int
    Type       string  // "motion", "face", "audio"
    Confidence float32
    Timestamp  int64
}

// 🔹 Функція парсингу
func ParseCCTVPlaylist(r io.Reader) (*ParsedMediaPlaylist, error) {
    // 🔹 Реєстрація кастомних декодерів
    customTags := []m3u8.CustomDecoder{
        &CameraIDTag{},   // #CCTV-CAMERA-ID:CAM-001
        &EventTag{},      // #CCTV-EVENT:TYPE=motion,CONFIDENCE=0.95
    }
    
    playlist, listType, err := m3u8.DecodeWith(r, false, customTags)
    if err != nil {
        return nil, fmt.Errorf("decode failed: %w", err)
    }
    if listType != m3u8.MEDIA {
        return nil, fmt.Errorf("expected MEDIA playlist, got %v", listType)
    }
    
    media := playlist.(*m3u8.MediaPlaylist)
    result := &ParsedMediaPlaylist{
        Duration: media.TargetDuration,
        Closed:   media.Closed,
        Events:   make([]SegmentEvent, 0),
    }
    
    // 🔹 Пошук CameraID у плейлист-тегах
    for _, tag := range media.Custom {
        if camTag, ok := tag.(*CameraIDTag); ok {
            result.CameraID = camTag.ID
            break
        }
    }
    
    // 🔹 Збір подій з сегмент-тегів
    for i, seg := range media.Segments {
        if seg == nil { continue }
        for _, tag := range seg.Custom {
            if eventTag, ok := tag.(*EventTag); ok {
                result.Events = append(result.Events, SegmentEvent{
                    Index:      i,
                    Type:       eventTag.Type,
                    Confidence: eventTag.Confidence,
                    Timestamp:  eventTag.Timestamp,
                })
            }
        }
    }
    
    return result, nil
}
```

---

### 🔹 Приклад 2: Генерація Master-плейлиста з адаптивними варіантами

```go
func GenerateAdaptiveMaster(variants []StreamVariant) (string, error) {
    p := m3u8.NewMasterPlaylist()
    
    for _, v := range variants {
        // 🔹 Додавання варіанту якості
        err := p.Append(v.URI, v.Bandwidth, v.Resolution, v.Codecs)
        if err != nil { return "", err }
        
        // 🔹 Додавання аудіо-доріжки, якщо є
        if v.AudioGroupID != "" {
            p.AppendAlternate(v.AudioGroupID, "AUDIO", v.AudioName, v.AudioURI, true, true)
        }
    }
    
    p.SetIndependentSegments(true)  // 🔹 Краща сумісність
    return p.String(), nil
}

// 🔹 Структура варіанту
type StreamVariant struct {
    URI         string
    Bandwidth   uint32
    Resolution  string  // "1280x720"
    Codecs      string  // "avc1.64001f,mp4a.40.2"
    AudioGroupID string
    AudioName   string
    AudioURI    string
}

// 🔹 Використання:
variants := []StreamVariant{
    {URI: "720p/index.m3u8", Bandwidth: 2500000, Resolution: "1280x720", Codecs: "avc1.64001f,mp4a.40.2"},
    {URI: "480p/index.m3u8", Bandwidth: 1200000, Resolution: "854x480", Codecs: "avc1.64001f,mp4a.40.2"},
    {URI: "360p/index.m3u8", Bandwidth: 600000, Resolution: "640x360", Codecs: "avc1.64001f,mp4a.40.2"},
}

master, err := GenerateAdaptiveMaster(variants)
if err != nil {
    log.Printf("❌ Generate failed: %v", err)
} else {
    os.WriteFile("master.m3u8", []byte(master), 0644)
    log.Printf("✅ Master playlist generated with %d variants", len(variants))
}
```

---

### 🔹 Приклад 3: Обробка `#EXT-X-PROGRAM-DATE-TIME` для синхронізації з подіями

```go
// 🔹 Функція для прив'язки сегментів до часової шкали подій
func SyncWithEvents(playlistPath string, events []Event) ([]SyncedSegment, error) {
    f, err := os.Open(playlistPath)
    if err != nil { return nil, err }
    defer f.Close()
    
    p, _, err := m3u8.DecodeFrom(f, false)
    if err != nil { return nil, err }
    
    media := p.(*m3u8.MediaPlaylist)
    var result []SyncedSegment
    
    for i, seg := range media.Segments {
        if seg == nil { continue }
        
        synced := SyncedSegment{
            Index:    i,
            Filename: seg.URI,
            Duration: seg.Duration,
        }
        
        // 🔹 Прив'язка до абсолютного часу, якщо є
        if seg.ProgramDateTime != nil {
            synced.StartTime = *seg.ProgramDateTime
            synced.EndTime = synced.StartTime.Add(time.Duration(seg.Duration * float64(time.Second)))
        }
        
        // 🔹 Прив'язка подій, що потрапляють у часовий діапазон сегмента
        for _, event := range events {
            if event.Time.After(synced.StartTime) && event.Time.Before(synced.EndTime) {
                synced.Events = append(synced.Events, event)
            }
        }
        
        result = append(result, synced)
    }
    
    return result, nil
}

// 🔹 Використання:
events := []Event{
    {Time: time.Unix(1705320015, 0), Type: "motion", CameraID: "CAM-001"},
    {Time: time.Unix(1705320048, 0), Type: "face", CameraID: "CAM-001"},
}

synced, err := SyncWithEvents("camera_001/playlist.m3u8", events)
if err != nil {
    log.Printf("❌ Sync failed: %v", err)
} else {
    for _, seg := range synced {
        if len(seg.Events) > 0 {
            log.Printf("🎬 Segment %d (%s): %d events", 
                seg.Index, seg.Filename, len(seg.Events))
        }
    }
}
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При парсингу плейлистів:
    • Використовуйте DecodeWith() для реєстрації кастомних тегів
    • Завжди перевіряйте ListType перед type assertion
    • Обробляйте помилки з контекстом (шлях файлу, номер рядка)

[ ] Для кастомних тегів:
    • Реалізуйте всі 5 методів інтерфейсу: TagName, Decode, SegmentTag, Encode, String
    • Використовуйте унікальні префікси: #CCTV-*, #MYAPP-* для уникнення конфліктів
    • Валідуйте атрибути у Decode() з чіткими помилками

[ ] Для генерації плейлистів:
    • Викликайте Close() після завершення додавання сегментів
    • Встановлюйте SetIndependentSegments(true) для кращої сумісності
    • Додавайте #EXT-X-PROGRAM-DATE-TIME для синхронізації з подіями

[ ] Для безпеки:
    • Валідуйте вхідні URI сегментів (заборона `file://`, `../`)
    • Обмежуйте довжину кастомних атрибутів (напр., max 255 символів)
    • Не довіряйте кастомним тегам з ненадійних джерел

[ ] Для тестування:
    • Створюйте тестові плейлисти з різними комбінаціями тегів
    • Перевіряйте round-trip: Generate → Parse → порівняння даних
    • Тестуйте крайні випадки: порожні атрибути, спеціальні символи, великі числа
```

---

## 🎯 Висновок

> **Цей модуль — "серце" бібліотеки m3u8**, яке забезпечує:
> • ✅ Надійний парсинг Master та Media плейлистів з підтримкою всіх стандартних тегів
> • ✅ Гнучке розширення через кастомні декодери без зміни ядра
> • ✅ Типобезпечний доступ до метаданих через структури Go
> • ✅ Контроль суворості парсингу через `strict` режим
> • ✅ Підтримку різних форматів дат через налаштовуваний `TimeParse`

Для вашого **CCTV HLS Processor** це означає:
- 🎯 Легка інтеграція кастомних тегів для маркування подій, камер, шифрування
- 🔍 Автоматичний парсинг `#EXT-X-PROGRAM-DATE-TIME` для синхронізації з аналітикою
- 🌐 Сумісність зі стандартними плеєрами (кастомні теги ігноруються)
- 🛡️ Безпека: валідація вхідних даних та контроль над суворістю парсингу
- 🔄 Гнучкість: підтримка як VoD, так і live-плейлистів з адаптивними варіантами

Потребуєте допомоги з інтеграцією парсингу плейлистів у ваш конвеєр або з реалізацією специфічних кастомних тегів для ваших сценаріїв? Напишіть — покажу готовий код для вашого випадку! 🚀🎬