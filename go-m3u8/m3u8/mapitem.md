# 🔍 Глибокий розбір коду: `MapItem` для HLS M3U8

Цей код реалізує роботу з тегом **`#EXT-X-MAP`** у форматі HLS-плейлистів. Розберемо детально кожен аспект.

---

## 📦 Що таке HLS і навіщо потрібен `EXT-X-MAP`?

### Контекст: HLS (HTTP Live Streaming)
```
┌─────────────────────────────────────┐
│ Master Playlist (.m3u8)             │
│ ├─ Variant 1: 480p/low.m3u8         │
│ ├─ Variant 2: 720p/medium.m3u8      │
│ └─ Variant 3: 1080p/high.m3u8       │
└─────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────┐
│ Media Playlist (low.m3u8)           │
│ #EXTM3U                             │
│ #EXT-X-VERSION:6                    │
│ #EXT-X-MAP:URI="init.mp4" ◄─── ЦЕ   │
│ #EXTINF:4.0,                        │
│ segment0.ts                         │
│ #EXTINF:4.0,                        │
│ segment1.ts                         │
└─────────────────────────────────────┘
```

### Призначення `#EXT-X-MAP`
```
#EXT-X-MAP:URI="init.mp4",BYTERANGE="1000@0"
```

| Аспект | Пояснення |
|--------|-----------|
| **Для fMP4 (CMAF)** | Вказує на файл ініціалізації з метаданими: кодеки, timescale, track IDs |
| **Initialization Section** | Містить `moov` box — "словник" для декодування всіх наступних сегментів |
| **ByteRange (опціонально)** | Дозволяє брати init-дані з частини файлу (економія трафіку) |
| **Ключова вимога** | У fMP4-плейлистах `#EXT-X-MAP` **обов'язковий** перед першим сегментом |

---

## 🏗️ Структура коду: детальний аналіз

### 1️⃣ Struct `MapItem`
```go
type MapItem struct {
    URI       string      // URL до init-файлу (напр. "https://cdn/init.mp4")
    ByteRange *ByteRange  // Опціональний діапазон байтів: nil = весь файл
}
```

**Чому `*ByteRange` (pointer)?**
- `nil` = атрибут відсутній → завантажувати весь файл
- `&ByteRange{...}` = атрибут є → завантажувати тільки вказану частину
- Це відповідає специфікації HLS: `BYTERANGE` — опціональний атрибут

### 2️⃣ Конструктор `NewMapItem`
```go
func NewMapItem(text string) (*MapItem, error) {
    // Крок 1: Парсинг атрибутів з рядка
    // Вхід: 'URI="init.mp4",BYTERANGE="1000@0"'
    // Вихід: map[string]string{"URI": "init.mp4", "BYTERANGE": "1000@0"}
    attributes := ParseAttributes(text)

    // Крок 2: Спроба розпарсити BYTERANGE (якщо є)
    br, err := NewByteRange(attributes[ByteRangeTag])
    if err != nil {
        return nil, err  // Помилка валідації формату байт-ренджу
    }

    // Крок 3: Побудова об'єкта
    return &MapItem{
        URI:       attributes[URITag],       // Обов'язковий атрибут
        ByteRange: br,                        // Може бути nil
    }, nil
}
```

**🔍 Критичні моменти:**
```go
// ⚠️ Потенційна проблема: якщо URI відсутній в attributes,
// mi.URI буде порожнім рядком "", а не помилкою!
// Рекомендація: додати валідацію
if attributes[URITag] == "" {
    return nil, fmt.Errorf("missing required attribute: URI")
}
```

### 3️⃣ Метод `String()` — серіалізація
```go
func (mi *MapItem) String() string {
    if mi.ByteRange == nil {
        // Простий випадок: тільки URI
        return fmt.Sprintf(`%s:%s="%s"`, MapItemTag, URITag, mi.URI)
        // Результат: #EXT-X-MAP:URI="init.mp4"
    }

    // Повний випадок: URI + BYTERANGE
    return fmt.Sprintf(`%s:%s="%s",%s="%v"`, 
        MapItemTag, URITag, mi.URI, ByteRangeTag, mi.ByteRange)
    // Результат: #EXT-X-MAP:URI="init.mp4",BYTERANGE="1000@0"
}
```

**🎯 Формат виводу:**
```
#EXT-X-MAP:URI="init.mp4"
#EXT-X-MAP:URI="init.mp4",BYTERANGE="1000@0"
```

---

## 🔗 Як це інтегрується у ваш pipeline

З урахуванням вашої архітектури **CCTV HLS Processor**:

```go
// Приклад використання у segmentFinalizer або playlist generator
func generateMediaPlaylist(segments []Segment, initURI string) string {
    var buf strings.Builder
    buf.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
    
    // Додаємо EXT-X-MAP на початку
    mapItem := &m3u8.MapItem{URI: initURI}
    buf.WriteString(mapItem.String() + "\n")
    
    // Далі сегменти...
    for _, seg := range segments {
        buf.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n%s\n", seg.Duration, seg.URI))
    }
    return buf.String()
}
```

### ⚙️ Особливості для вашого use-case:
| Вимога | Рішення через MapItem |
|--------|----------------------|
| **Динамічний init-файл** | Оновлюйте `URI` при зміні кодека/роздільної здатності |
| **ByteRange для економії** | Використовуйте `ByteRange`, якщо init-дані в середині великого файлу |
| **DISCONTINUITY** | При зміні init-файлу додавайте `#EXT-X-DISCONTINUITY` перед новим `#EXT-X-MAP` |
| **Кешування** | Клієнти кешують init-файл за URI — уникайте змін URI без потреби |

---

## 🧪 Приклад парсингу та генерації

```go
// ✅ Парсинг вхідного рядка
line := `URI="https://cdn/init.mp4",BYTERANGE="800@0"`
mapItem, err := m3u8.NewMapItem(line)
if err != nil {
    log.Fatal(err)
}
fmt.Println(mapItem.URI)        // "https://cdn/init.mp4"
fmt.Println(mapItem.ByteRange)  // &ByteRange{Length: 800, Offset: 0}

// ✅ Генерація вихідного рядка
output := mapItem.String()
// "#EXT-X-MAP:URI=\"https://cdn/init.mp4\",BYTERANGE=\"800@0\""
```

---

## ⚠️ Потенційні покращення коду

1. **Валідація обов'язкового URI**:
```go
if uri := attributes[URITag]; uri == "" {
    return nil, fmt.Errorf("EXT-X-MAP requires URI attribute")
}
```

2. **Екранування спеціальних символів у URI**:
```go
// За специфікацією, лапки в URI мають бути екрановані
escapedURI := strings.ReplaceAll(mi.URI, `"`, `\"`)
```

3. **Thread-safety при спільному доступі**:
```go
// Якщо MapItem використовується в кількох горутинах:
type SafeMapItem struct {
    mu sync.RWMutex
    MapItem
}
```

4. **Кешування парсингу ByteRange**:
```go
// Якщо один і той же MapItem серіалізується багато разів,
// можна кешувати результат String() після першого виклику
```

---

## 📋 Специфікація HLS (RFC 8216) — ключові вимоги

```
#EXT-X-MAP — обов'язковий для:
✓ fMP4 (Fragmented MP4) плейлистів
✓ Плейлистів з TYPE=IMAGE

#EXT-X-MAP має:
✓ З'являтися ПЕРЕД першим медіа-сегментом
✓ Мати стабільний URI (клієнти кешують)
✓ Не змінюватися всередині плейлиста (окрім випадків з DISCONTINUITY)

Формат атрибутів:
✓ URI="..." — обов'язковий, у лапках
✓ BYTERANGE="N@O" — опціональний, N=довжина, O=зміщення (обидва десяткові)
```

---

## 🎯 Висновок

Цей код — **атомарний будівельний блок** для роботи з HLS-плейлистами. Він:

✅ Відповідає специфікації RFC 8216  
✅ Підтримує опціональний `BYTERANGE`  
✅ Має чіткий контракт: парсинг ↔ серіалізація  
✅ Легко інтегрується у ваш pipeline сегментації

**Для вашого CCTV-процесора**: переконайтеся, що `init.mp4` генерується коректно (з `moov` на початку) і доступний за стабільним URI — це критично для відтворення на клієнтських пристроях.

Потрібно допомогти з інтеграцією цього коду у ваш `segmentFinalizer` або з генерацією `init-файлів` через FFmpeg? 🛠️