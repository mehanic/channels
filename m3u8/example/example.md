# 🎬 `main.go`: Приклад використання кастомних тегів у HLS-плейлистах

Це **демонстраційна програма** бібліотеки `github.com/grafov/m3u8`, яка показує, як **парсити M3U8-плейлисти з кастомними тегами** — критично для інтеграції специфічних метаданих (камери, сповіщення, шифрування) у CCTV HLS-стрімінг без порушення сумісності зі стандартними плеєрами.

---

## 🎯 Коротка відповідь

> **Це "живий приклад" розширення HLS**: він демонструє реєстрацію кастомних декодерів, парсинг плейлиста з нестандартними тегами та типобезпечний доступ до метаданих — ідеальний шаблон для додавання CCTV-специфічної інформації у ваші плейлисти.

---

## 🧱 Структура програми: Крок за кроком

### 🔹 Крок 1: Визначення шляху до тестового файлу

```go
GOPATH := os.Getenv("GOPATH")
if GOPATH == "" {
    panic("$GOPATH is empty")  // 🔹 Перевірка змінної оточення
}

m3u8File := "github.com/grafov/m3u8/sample-playlists/media-playlist-with-custom-tags.m3u8"
f, err := os.Open(path.Join(GOPATH, "src", m3u8File))
if err != nil {
    panic(err)  // 🔹 Обробка помилок відкриття файлу
}
```

**🎯 Призначення**: Знайти та відкрити тестовий плейлист із кастомними тегами для демонстрації.

**⚠️ Застарілий підхід**: Сучасні Go-проєкти використовують модулі (`go.mod`), а не `GOPATH`. Для продакшену краще:
```go
// 🔹 Сучасний підхід з embed (Go 1.16+):
//go:embed sample-playlists/*.m3u8
var testFiles embed.FS

f, err := testFiles.Open("sample-playlists/media-playlist-with-custom-tags.m3u8")
```

---

### 🔹 Крок 2: Реєстрація кастомних декодерів

```go
customTags := []m3u8.CustomDecoder{
    &template.CustomPlaylistTag{},   // 🔹 Тег для всього плейлиста
    &template.CustomSegmentTag{},    // 🔹 Тег для окремих сегментів
}
```

**🎯 Призначення**: Повідомити бібліотеці `m3u8`, які кастомні теги вона має розпізнавати та парсити.

**🔑 Ключовий момент**: Порядок реєстрації не важливий — бібліотека автоматично визначає тип тегу через метод `SegmentTag()`.

---

### 🔹 Крок 3: Парсинг з підтримкою кастомних тегів

```go
p, listType, err := m3u8.DecodeWith(bufio.NewReader(f), true, customTags)
if err != nil {
    panic(err)
}
```

**🔄 Сигнатура `DecodeWith`:**
```go
func DecodeWith(r io.Reader, strict bool, custom ...CustomDecoder) (Playlist, ListType, error)
```

| Параметр | Призначення | Рекомендація для CCTV |
|----------|-------------|----------------------|
| `r` | Джерело даних (файл, мережа, буфер) | Використовуйте `bufio.Reader` для ефективності |
| `strict` | Чи відхиляти невідомі теги | `false` для сумісності зі старими плеєрами |
| `custom` | Список кастомних декодерів | Реєструйте всі теги, які плануєте використовувати |

**🎯 Призначення**: Розпарсити M3U8-файл у типобезпечну структуру (`MediaPlaylist` або `MasterPlaylist`) з підтримкою кастомних тегів.

---

### 🔹 Крок 4: Обробка результату парсингу

```go
switch listType {
case m3u8.MEDIA:
    mediapl := p.(*m3u8.MediaPlaylist)  // 🔹 Type assertion
    fmt.Printf("%+v\n", mediapl)        // 🔹 Вивід структури
case m3u8.MASTER:
    masterpl := p.(*m3u8.MasterPlaylist)
    fmt.Printf("%+v\n", masterpl)
}
```

**🎯 Призначення**: Розрізнити тип плейлиста та отримати доступ до його вмісту.

**🔢 Приклад виводу для `MediaPlaylist`:**
```
&{Version:7 TargetDuration:4.0 Segments:[0xc000012340 0xc000012380] ... 
CustomTags:[#CUSTOM-PLAYLIST-TAG:42] ...}
```

---

## 🔍 Приклад вхідного файлу: `media-playlist-with-custom-tags.m3u8`

```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#CUSTOM-PLAYLIST-TAG:42          ← 🔹 Кастомний плейлист-тег
#EXTINF:4.000,
#CUSTOM-SEGMENT-TAG:NAME="alarm",JEDI=YES  ← 🔹 Кастомний сегмент-тег
seg_001.ts
#EXTINF:4.000,
seg_002.ts
#EXT-X-ENDLIST
```

**🔄 Що відбувається при парсингу:**

```
🔹 Рядок 4: "#CUSTOM-PLAYLIST-TAG:42"
   │
   ▼
   • Знайдено в customTags: &CustomPlaylistTag{}
   • Викликано: tag.Decode("#CUSTOM-PLAYLIST-TAG:42")
   • Результат: &CustomPlaylistTag{Number: 42}
   • Додано до: playlist.CustomTags

🔹 Рядок 6: "#CUSTOM-SEGMENT-TAG:NAME=\"alarm\",JEDI=YES"
   │
   ▼
   • Знайдено в customTags: &CustomSegmentTag{}
   • Викликано: tag.Decode("#CUSTOM-SEGMENT-TAG:NAME=\"alarm\",JEDI=YES")
   • Результат: &CustomSegmentTag{Name: "alarm", Jedi: true}
   • Додано до: playlist.Segments[0].CustomTags
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Парсинг плейлиста з подіями для аналітики

```go
// 🔹 Структура для зберігання розпаршених даних
type ParsedPlaylist struct {
    CameraID   string
    Events     []SegmentEvent  // Події для кожного сегмента
    Duration   float64
    SegmentCount int
}

type SegmentEvent struct {
    Index      int
    Type       string  // "motion", "face", "audio"
    Confidence float32
    Timestamp  int64
}

// 🔹 Функція парсингу з кастомними тегами
func ParseCCTVPlaylist(playlistPath string) (*ParsedPlaylist, error) {
    f, err := os.Open(playlistPath)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    
    // 🔹 Реєстрація кастомних декодерів
    customTags := []m3u8.CustomDecoder{
        &CameraIDTag{},   // #CCTV-CAMERA-ID:CAM-001
        &EventTag{},      // #CCTV-EVENT:TYPE=motion,CONFIDENCE=0.95
    }
    
    p, listType, err := m3u8.DecodeWith(bufio.NewReader(f), false, customTags)
    if err != nil {
        return nil, fmt.Errorf("decode failed: %w", err)
    }
    
    if listType != m3u8.MEDIA {
        return nil, fmt.Errorf("expected MEDIA playlist, got %v", listType)
    }
    
    media := p.(*m3u8.MediaPlaylist)
    result := &ParsedPlaylist{
        Duration:     media.TargetDuration,
        SegmentCount: len(media.Segments),
        Events:       make([]SegmentEvent, 0),
    }
    
    // 🔹 Пошук CameraID у плейлист-тегах
    for _, tag := range media.CustomTags {
        if camTag, ok := tag.(*CameraIDTag); ok {
            result.CameraID = camTag.ID
            break
        }
    }
    
    // 🔹 Збір подій з сегмент-тегів
    for i, seg := range media.Segments {
        if seg == nil { continue }
        
        for _, tag := range seg.CustomTags {
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

// 🔹 Використання:
parsed, err := ParseCCTVPlaylist("camera_001/playlist.m3u8")
if err != nil {
    log.Printf("❌ Parse failed: %v", err)
} else {
    log.Printf("📹 Camera: %s, Events: %d, Segments: %d", 
        parsed.CameraID, len(parsed.Events), parsed.SegmentCount)
}
```

---

### 🔹 Приклад 2: Генерація плейлиста з кастомними тегами в реальному часі

```go
// 🔹 Функція для створення плейлиста з подіями
func GenerateCCTVPlaylist(cameraID string, segments []Segment, events map[int]Event) (string, error) {
    // 🔹 Створення MediaPlaylist
    p, err := m3u8.NewMediaPlaylist(uint(len(segments)), uint(len(segments)))
    if err != nil {
        return "", err
    }
    
    // 🔹 Реєстрація кастомних тегів
    p.RegisterCustomTag(&CameraIDTag{})
    p.RegisterCustomTag(&EventTag{})
    
    // 🔹 Стандартні налаштування
    p.SetVersion(7)
    p.SetTargetDuration(4)
    p.SetPlaylistType("vod")
    
    // 🔹 Додавання плейлист-тегу (камера)
    p.AddCustomTag(&CameraIDTag{ID: cameraID})
    
    // 🔹 Додавання сегментів з подіями
    for i, seg := range segments {
        // 🔹 Додавання сегмент-тегу, якщо є подія
        if event, exists := events[i]; exists {
            p.AddCustomTagForSegment(i, &EventTag{
                Type:       event.Type,
                Confidence: event.Confidence,
                Timestamp:  event.Timestamp,
            })
        }
        
        // 🔹 Додавання самого сегмента
        p.Append(seg.Filename, seg.Duration, "")
    }
    
    p.Close()
    return p.String(), nil
}

// 🔹 Використання у конвеєрі:
events := map[int]Event{
    5: {Type: "motion", Confidence: 0.95, Timestamp: 1705320000},
    12: {Type: "face", Confidence: 1.0, Timestamp: 1705320048},
}

playlist, err := GenerateCCTVPlaylist("CAM-001", segments, events)
if err != nil {
    log.Printf("❌ Generate failed: %v", err)
} else {
    // 🔹 Запис плейлиста на диск
    os.WriteFile("output/playlist.m3u8", []byte(playlist), 0644)
    log.Printf("✅ Playlist generated with %d custom tags", 
        strings.Count(playlist, "#CCTV-"))
}
```

---

### 🔹 Приклад 3: Фільтрація плейлистів за кастомними тегами для пошуку

```go
// 🔹 Структура для пошукових запитів
type PlaylistQuery struct {
    CameraIDs    []string
    EventTypes   []string
    MinConfidence float32
    TimeRange    struct{ Start, End int64 }
}

// 🔹 Пошук плейлистів за критеріями
func SearchPlaylists(directory string, query PlaylistQuery) ([]string, error) {
    var results []string
    
    // 🔹 Пошук всіх .m3u8 файлів
    files, err := filepath.Glob(filepath.Join(directory, "**/*.m3u8"))
    if err != nil {
        return nil, err
    }
    
    for _, path := range files {
        parsed, err := ParseCCTVPlaylist(path)
        if err != nil {
            log.Printf("⚠️  Skipping %s: %v", path, err)
            continue
        }
        
        // 🔹 Фільтрація за camera_id
        if len(query.CameraIDs) > 0 && !contains(query.CameraIDs, parsed.CameraID) {
            continue
        }
        
        // 🔹 Фільтрація за подіями
        match := false
        for _, event := range parsed.Events {
            if len(query.EventTypes) > 0 && !contains(query.EventTypes, event.Type) {
                continue
            }
            if event.Confidence < query.MinConfidence {
                continue
            }
            if query.TimeRange.Start > 0 && event.Timestamp < query.TimeRange.Start {
                continue
            }
            if query.TimeRange.End > 0 && event.Timestamp > query.TimeRange.End {
                continue
            }
            match = true
            break
        }
        
        if match || len(query.EventTypes) == 0 {
            results = append(results, path)
        }
    }
    
    return results, nil
}

// 🔹 Використання:
results, err := SearchPlaylists("/recordings", PlaylistQuery{
    CameraIDs: []string{"CAM-001", "CAM-003"},
    EventTypes: []string{"motion", "face"},
    MinConfidence: 0.9,
    TimeRange: struct{ Start, End int64 }{
        Start: time.Now().Add(-24 * time.Hour).Unix(),
        End: time.Now().Unix(),
    },
})
if err != nil {
    log.Printf("❌ Search failed: %v", err)
} else {
    log.Printf("✅ Found %d matching playlists", len(results))
    for _, path := range results {
        fmt.Println("  -", path)
    }
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути `RegisterCustomTag()` | Кастомні теги ігноруються, повертаються як сирі рядки | Завжди реєструйте теги перед `DecodeWith` або додаванням у плейлист |
| Неправильний `SegmentTag()` | Теги додаються не в те місце (плейлист замість сегмента) | Повертайте `true` для сегмент-тегів, `false` для плейлист-тегів |
| Reuse екземпляра у `Decode` | Дані одного сегмента "просочуються" в інший | Завжди створюйте `new(TagType)` у `Decode()` |
| Ігнорування `listType` | Паніка при type assertion для неправильного типу плейлиста | Завжди перевіряйте `listType` перед `p.(*Type)` |
| Хардкод індексу у `line[20:]` | Парсинг ламається при зміні довжини ідентифікатора | Використовуйте `line[len(TagName()):]` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При парсингу плейлистів:
    • Реєструйте всі кастомні теги через DecodeWith()
    • Перевіряйте listType перед type assertion
    • Обробляйте помилки парсингу з контекстом (шлях файлу, номер рядка)

[ ] При генерації плейлистів:
    • Реєструйте теги через RegisterCustomTag() перед додаванням
    • Використовуйте AddCustomTag() для плейлист-тегів
    • Використовуйте AddCustomTagForSegment() для сегмент-тегів
    • Завжди викликайте Close() після завершення додавання сегментів

[ ] Для сумісності:
    • Не змінюйте стандартні теги — тільки додавайте кастомні
    • Тестуйте з різними плеєрами: hls.js, Video.js, Safari, ExoPlayer
    • Документуйте формат кастомних тегів для інших розробників

[ ] Для безпеки:
    • Валідуйте атрибути кастомних тегів у Decode()
    • Обмежуйте довжину рядкових значень (напр., max 255 символів)
    • Не довіряйте кастомним тегам з ненадійних джерел

[ ] Для тестування:
    • Створюйте тестові плейлисти з різними комбінаціями тегів
    • Перевіряйте round-trip: Generate → Parse → порівняння даних
    • Тестуйте крайні випадки: порожні атрибути, спеціальні символи, великі числа
```

---

## 🎯 Висновок

> **Цей приклад — ваш "стартовий майданчик" для розширення HLS**, який демонструє:
> • ✅ Реєстрацію кастомних декодерів через `DecodeWith()`
> • ✅ Типобезпечний парсинг плейлист- та сегмент-тегів
> • ✅ Автоматичне розрізнення типів плейлистів через `ListType`
> • ✅ Інтеграцію з існуючим кодом без порушення сумісності
> • ✅ Простий шаблон для додавання нових типів метаданих

Для вашого **CCTV HLS Processor** це означає:
- 🎯 Легке додавання ідентифікаторів камер, подій, геолокації у плейлисти
- 🔍 Автоматичний парсинг кастомних тегів для фільтрації та аналітики
- 🌐 Сумісність зі стандартними плеєрами (вони ігнорують невідомі теги)
- 🔧 Гнучкість: швидке додавання нових типів метаданих без рефакторингу
- 🛡️ Безпека: валідація вхідних даних у `Decode()` запобігає ін'єкціям

Потребуєте допомоги з інтеграцією кастомних тегів у ваш конвеєр генерації плейлистів або з налаштуванням пошуку за метаданими? Напишіть — покажу готовий код для вашого сценарію! 🚀🏷️