# 🏷️ `template`: Створення кастомних тегів для HLS-плейлистів

Це **приклад реалізації** кастомного тегу для бібліотеки `github.com/grafov/m3u8`, який демонструє, як розширити стандартний формат M3U8 власними метаданими — критично для додавання специфічної інформації у CCTV HLS-стрімінг (наприклад, `#CCTV-CAMERA-ID:1`, `#CCTV-ALERT:motion`, `#CCTV-ENCRYPTION:key_id`).

---

## 🎯 Коротка відповідь

> **Це "шаблон для розширення HLS"**: він показує, як створити власний тег формату `#CUSTOM-PLAYLIST-TAG:<number>`, що реалізує інтерфейси `CustomTag` та `CustomDecoder` бібліотеки `m3u8` — дозволяючи додавати кастомні метадані у плейлисти без порушення сумісності зі стандартними плеєрами.

---

## 🧱 Архітектура кастомного тегу

### 🔹 Структура `CustomPlaylistTag`

```go
type CustomPlaylistTag struct {
    Number int  // 🔹 Корисне навантаження тегу
}
```

**🎯 Призначення**: Інкапсулювати **дані тегу** у типобезпечний спосіб — замість роботи з сирими рядками.

---

### 🔹 Інтерфейси `m3u8.CustomTag` / `CustomDecoder`

```go
// 🔹 TagName: ідентифікатор тегу (з '#' та ':')
func (tag *CustomPlaylistTag) TagName() string {
    return "#CUSTOM-PLAYLIST-TAG:"
}

// 🔹 Decode: парсинг рядка у структуру
func (tag *CustomPlaylistTag) Decode(line string) (m3u8.CustomTag, error) {
    _, err := fmt.Sscanf(line, "#CUSTOM-PLAYLIST-TAG:%d", &tag.Number)
    return tag, err
}

// 🔹 SegmentTag: чи є тег прив'язаним до сегмента?
func (tag *CustomPlaylistTag) SegmentTag() bool {
    return false  // 🔹 Це плейлист-тег, не сегмент-тег
}

// 🔹 Encode: серіалізація структури у рядок
func (tag *CustomPlaylistTag) Encode() *bytes.Buffer {
    buf := new(bytes.Buffer)
    buf.WriteString(tag.TagName())              // "#CUSTOM-PLAYLIST-TAG:"
    buf.WriteString(strconv.Itoa(tag.Number))   // "123"
    return buf
}

// 🔹 String: зручний вивід для дебагу
func (tag *CustomPlaylistTag) String() string {
    return tag.Encode().String()  // "#CUSTOM-PLAYLIST-TAG:123"
}
```

**🔄 Потік даних:**
```
🔹 Парсинг (Decode):
   Вхід: "#CUSTOM-PLAYLIST-TAG:42"
   │
   ▼
   fmt.Sscanf(line, "#CUSTOM-PLAYLIST-TAG:%d", &tag.Number)
   │
   ▼
   Вихід: &CustomPlaylistTag{Number: 42}

🔹 Серіалізація (Encode):
   Вхід: &CustomPlaylistTag{Number: 42}
   │
   ▼
   buf.WriteString("#CUSTOM-PLAYLIST-TAG:")
   buf.WriteString("42")
   │
   ▼
   Вихід: "#CUSTOM-PLAYLIST-TAG:42"
```

---

## 🔍 Чому це важливо для CCTV HLS?

### 🔹 Проблема: Стандартні теги недостатні

Стандарт HLS (RFC 8216) визначає фіксований набір тегів (`#EXT-X-VERSION`, `#EXTINF`, `#EXT-X-KEY` тощо), але для систем відеоспостереження часто потрібні:

| Потреба | Приклад кастомного тегу | Призначення |
|---------|-------------------------|-------------|
| 🔹 Ідентифікація камери | `#CCTV-CAMERA-ID:CAM-001` | Маркування плейлиста для конкретного пристрою |
| 🔹 Сповіщення про події | `#CCTV-ALERT:motion,detected_at=1705320000` | Інтеграція з системами аналітики |
| 🔹 Шифрування/ключі | `#CCTV-ENCRYPTION:key_id=abc123,algo=AES-128` | Керування DRM-ключами |
| 🔹 Геолокація | `#CCTV-LOCATION:lat=50.4501,lon=30.5234` | Прив'язка запису до місця |
| 🔹 Версія конфігурації | `#CCTV-CONFIG:v2.1.0` | Відстеження змін у налаштуваннях кодування |

### 🔹 Рішення: Кастомні теги через `m3u8.CustomTag`

```go
// 🔹 Приклад: тег для ідентифікації камери
type CameraIDTag struct {
    ID string
}

func (t *CameraIDTag) TagName() string { return "#CCTV-CAMERA-ID:" }
func (t *CameraIDTag) Decode(line string) (m3u8.CustomTag, error) {
    _, err := fmt.Sscanf(line, "#CCTV-CAMERA-ID:%s", &t.ID)
    return t, err
}
func (t *CameraIDTag) SegmentTag() bool { return false }
func (t *CameraIDTag) Encode() *bytes.Buffer {
    buf := new(bytes.Buffer)
    buf.WriteString(t.TagName())
    buf.WriteString(t.ID)
    return buf
}
func (t *CameraIDTag) String() string { return t.Encode().String() }
```

**✅ Переваги:**
- 🎯 Типобезпечний доступ до даних (не парсинг рядків у рантаймі)
- 🔄 Автоматична серіалізація/десеріалізація через бібліотеку `m3u8`
- 🌐 Сумісність: стандартні плеєри ігнорують невідомі теги (за специфікацією)
- 🔧 Розширюваність: легке додавання нових тегів без зміни ядра

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Додавання кастомних тегів при генерації плейлиста

```go
func GenerateCCTVPlaylist(cameraID string, alertType string, segments []Segment) (string, error) {
    // 🔹 Створення плейлиста
    p, err := m3u8.NewMediaPlaylist(uint(len(segments)), uint(len(segments)))
    if err != nil {
        return "", err
    }
    
    // 🔹 Додавання стандартних заголовків
    p.SetVersion(7)
    p.SetTargetDuration(4)
    p.SetPlaylistType("vod")
    
    // 🔹 Додавання кастомних тегів через RegisterCustomTag
    p.RegisterCustomTag(&CameraIDTag{})
    p.RegisterCustomTag(&AlertTag{})
    
    // 🔹 Додавання тегів у плейлист
    p.AddCustomTag(&CameraIDTag{ID: cameraID})
    if alertType != "" {
        p.AddCustomTag(&AlertTag{Type: alertType, Timestamp: time.Now().Unix()})
    }
    
    // 🔹 Додавання сегментів
    for _, seg := range segments {
        p.Append(seg.Filename, seg.Duration, "")
    }
    
    p.Close()
    return p.String(), nil
}

// 🔹 Приклад виводу:
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-PLAYLIST-TYPE:VOD
#CCTV-CAMERA-ID:CAM-001
#CCTV-ALERT:motion,detected_at=1705320000
#EXTINF:4.000,
seg_001.ts
#EXTINF:4.000,
seg_002.ts
#EXT-X-ENDLIST
```

---

### 🔹 Приклад 2: Парсинг кастомних тегів з вхідного плейлиста

```go
func ParseCCTVPlaylist(playlistContent string) (*PlaylistMetadata, error) {
    // 🔹 Створення парсера з реєстрацією кастомних тегів
    p, err := m3u8.DecodeFrom(strings.NewReader(playlistContent), true)
    if err != nil {
        return nil, err
    }
    
    media, ok := p.(*m3u8.MediaPlaylist)
    if !ok {
        return nil, fmt.Errorf("expected MediaPlaylist")
    }
    
    meta := &PlaylistMetadata{}
    
    // 🔹 Ітерація по кастомних тегах
    for _, tag := range media.CustomTags {
        switch t := tag.(type) {
        case *CameraIDTag:
            meta.CameraID = t.ID
        case *AlertTag:
            meta.AlertType = t.Type
            meta.AlertTimestamp = t.Timestamp
        }
    }
    
    return meta, nil
}

// 🔹 Використання:
meta, err := ParseCCTVPlaylist(playlistContent)
if err != nil {
    log.Printf("❌ Parse failed: %v", err)
} else {
    log.Printf("📹 Camera: %s, Alert: %s @ %d", 
        meta.CameraID, meta.AlertType, meta.AlertTimestamp)
}
```

---

### 🔹 Приклад 3: Фільтрація плейлистів за кастомними тегами

```go
// 🔹 Структура для фільтрації
type PlaylistFilter struct {
    CameraIDs []string
    AlertTypes []string
    MinTimestamp int64
}

func FilterPlaylists(playlists []string, filter PlaylistFilter) ([]string, error) {
    var result []string
    
    for _, pl := range playlists {
        meta, err := ParseCCTVPlaylist(pl)
        if err != nil {
            log.Printf("⚠️  Skipping invalid playlist: %v", err)
            continue
        }
        
        // 🔹 Фільтрація за camera_id
        if len(filter.CameraIDs) > 0 && !contains(filter.CameraIDs, meta.CameraID) {
            continue
        }
        
        // 🔹 Фільтрація за типом сповіщення
        if len(filter.AlertTypes) > 0 && !contains(filter.AlertTypes, meta.AlertType) {
            continue
        }
        
        // 🔹 Фільтрація за часом
        if filter.MinTimestamp > 0 && meta.AlertTimestamp < filter.MinTimestamp {
            continue
        }
        
        result = append(result, pl)
    }
    
    return result, nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути `SegmentTag()` | Тег додається не в те місце плейлиста | Повертайте `true` для сегмент-тегів, `false` для плейлист-тегів |
| Неправильний формат `TagName()` | Парсинг не знаходить тег | Завжди повертайте `#TAG_NAME:` з двокрапкою, якщо є значення |
| Ігнорування реєстрації тегів | `Decode` не викликається, тег ігнорується | Викликайте `RegisterCustomTag()` перед парсингом/генерацією |
| Неправильний парсинг у `Decode` | Помилки при читанні складних значень | Використовуйте `fmt.Sscanf` або регулярні вирази для надійності |
| Відсутність `String()` | Складний дебаг кастомних тегів | Реалізуйте `String()` через `Encode().String()` для зручного виводу |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні кастомних тегів:
    • Реалізуйте всі 5 методів інтерфейсу: TagName, Decode, SegmentTag, Encode, String
    • Використовуйте унікальні префікси: #CCTV-*, #MYAPP-* для уникнення конфліктів
    • Документуйте формат значень: "#CCTV-ALERT:type=value,time=unix_ts"

[ ] Для реєстрації тегів:
    • Викликайте RegisterCustomTag() перед DecodeFrom() або генерацією плейлиста
    • Реєструйте всі типи, які плануєте використовувати
    • Тестуйте round-trip: Encode → Decode → порівняння значень

[ ] Для сумісності:
    • Не змінюйте стандартні теги (#EXT-X-*) — тільки додавайте кастомні
    • Переконайтеся, що кастомні теги ігноруються стандартними плеєрами
    • Тестуйте з популярними плеєрами: hls.js, Video.js, Safari, ExoPlayer

[ ] Для безпеки:
    • Валідуйте вхідні дані у Decode() (напр., обмеження довжини ID)
    • Не довіряйте кастомним тегам з ненадійних джерел
    • Логувайте невідомі теги для моніторингу аномалій

[ ] Для тестування:
    • Покрийте кейси: порожнє значення, спеціальні символи, великі числа
    • Тестуйте парсинг з різними форматами: пробіли, відсутність двокрапки
    • Перевіряйте, що Encode() → Decode() дає ідентичний результат
```

---

## 🎯 Висновок

> **Цей шаблон — ваш "ключ до розширення HLS"**, який забезпечує:
> • ✅ Типобезпечне додавання кастомних метаданих у плейлисти
> • ✅ Автоматичну серіалізацію/десеріалізацію через інтерфейси `m3u8`
> • ✅ Сумісність зі стандартом: невідомі теги ігноруються плеєрами
> • ✅ Розширюваність: легке додавання нових тегів без зміни ядра
> • ✅ Прозорий дебаг через реалізацію `String()`

Для вашого **CCTV HLS Processor** це означає:
- 🎯 Додавання ідентифікаторів камер, сповіщень, геолокації у плейлисти
- 🔍 Автоматичний парсинг кастомних тегів для фільтрації та аналітики
- 🌐 Сумісність зі стандартними плеєрами (вони ігнорують невідомі теги)
- 🔧 Гнучкість: швидке додавання нових типів метаданих без рефакторингу
- 🛡️ Безпека: валідація вхідних даних у `Decode()` запобігає ін'єкціям

Потребуєте допомоги з реалізацією конкретних кастомних тегів для ваших сценаріїв (шифрування, геолокація, інтеграція з аналітикою)? Напишіть — покажу готовий код для вашого випадку! 🚀🏷️