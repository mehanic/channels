# 🔍 Глибокий розбір коду: `MediaItem` для HLS M3U8

Цей код реалізує роботу з тегом **`#EXT-X-MEDIA`** — ключовим елементом для **мультиякісних плейлистів** (Multi-Variant Playlists) у HLS. Розберемо детально.

---

## 📦 Що таке `#EXT-X-MEDIA` і навіщо він потрібен?

### Контекст: Master Playlist
```m3u8
#EXTM3U
#EXT-X-VERSION:7

#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="English",LANGUAGE="en",DEFAULT=YES,AUTOSELECT=YES,URI="audio/en.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Arabic",LANGUAGE="ar",URI="audio/ar.m3u8"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="English",LANGUAGE="en",FORCED=NO,AUTOSELECT=YES,URI="subs/en.m3u8"

#EXT-X-STREAM-INF:BANDWIDTH=1280000,AUDIO="audio",SUBTITLES="subs"
video/720p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000,AUDIO="audio",SUBTITLES="subs"
video/1080p.m3u8
```

### Призначення `#EXT-X-MEDIA`
| Атрибут | Призначення | Обов'язковий? |
|---------|-------------|---------------|
| `TYPE` | Тип медіа: `AUDIO` \| `VIDEO` \| `SUBTITLES` \| `CLOSED-CAPTIONS` | ✅ Так |
| `GROUP-ID` | Група для прив'язки до `EXT-X-STREAM-INF` | ✅ Так |
| `NAME` | Людське ім'я для вибору в плеєрі | ✅ Так |
| `LANGUAGE` | Код мови (RFC 5646) для автовибору | ❌ Ні |
| `DEFAULT` | Чи обирати автоматично, якщо немає переваг користувача | ❌ Ні |
| `AUTOSELECT` | Чи дозволяти плеєру обирати автоматично | ❌ Ні |
| `FORCED` | Для субтитрів: показувати тільки іноземні частини | ❌ Ні |
| `URI` | Посилання на медіа-плейлист (крім CLOSED-CAPTIONS) | ❌ Ні* |
| `IN-STREAM-ID` | Ідентифікатор вбудованих субтитрів у відеопотоці | ❌ Ні |

> *`URI` не потрібен для `TYPE=CLOSED-CAPTIONS`, бо дані вже у відеопотоці.

---

## 🏗️ Структура коду: детальний аналіз

### 1️⃣ Struct `MediaItem` — повна підтримка атрибутів
```go
type MediaItem struct {
    Type              string   // AUDIO, VIDEO, SUBTITLES, CLOSED-CAPTIONS
    GroupID           string   // "audio", "subs" — для групування
    Name              string   // "English", "Arabic" — для UI плеєра
    
    // Опціональні рядкові поля (можуть бути nil)
    Language          *string  // "en", "ar", "ru"
    AssocLanguage     *string  // Мова асоціації (напр. для оригіналу)
    URI               *string  // Посилання на плейлист аудіо/субтитрів
    InStreamID        *string  // "CC1", "CC2" — для вбудованих субтитрів
    Characteristics   *string  // "public.accessibility.describes-video"
    Channels          *string  // "6", "2" — кількість аудіоканалів
    StableRenditionId *string  // Стабільний ID для аналітики
    
    // Булеві прапорці (YES/NO у специфікації)
    AutoSelect        *bool    // Дозволити автовибір плеєром
    Default           *bool    // Обрати за замовчуванням
    Forced            *bool    // Тільки для субтитрів: показувати іноземне
}
```

**🎯 Чому так багато `*string` і `*bool`?**
```
Специфікація HLS (RFC 8216) вимагає:
✓ Атрибути, яких немає в рядку — НЕ виводити при серіалізації
✓ Булеві значення: YES/NO, а не true/false
✓ Відсутність атрибута ≠ false (семантична різниця!)

Приклад:
- Default=nil  → атрибут не виводиться → плеєр вирішує сам
- Default=&false → виводиться "DEFAULT=NO" → явно забороняємо автовибір
```

### 2️⃣ Конструктор `NewMediaItem` — парсинг з абстракціями
```go
func NewMediaItem(text string) (*MediaItem, error) {
    attributes := ParseAttributes(text)
    // Вхід: 'TYPE=AUDIO,NAME="English",LANGUAGE="en",DEFAULT=YES'
    // Вихід: map[string]string{"TYPE": "AUDIO", "NAME": "English", ...}

    return &MediaItem{
        // Обов'язкові поля — завжди string (навіть якщо порожні)
        Type:    attributes[TypeTag],
        GroupID: attributes[GroupIDTag],
        Name:    attributes[NameTag],
        
        // Опціональні рядки: helper повертає *string (nil якщо немає)
        Language:          pointerTo(attributes, LanguageTag),
        AssocLanguage:     pointerTo(attributes, AssocLanguageTag),
        URI:               pointerTo(attributes, URITag),
        InStreamID:        pointerTo(attributes, InStreamIDTag),
        Characteristics:   pointerTo(attributes, CharacteristicsTag),
        Channels:          pointerTo(attributes, ChannelsTag),
        StableRenditionId: pointerTo(attributes, StableRenditionIDTag),
        
        // Булеві поля: парсинг YES/NO → *bool
        AutoSelect: parseYesNo(attributes, AutoSelectTag),
        Default:    parseYesNo(attributes, DefaultTag),
        Forced:     parseYesNo(attributes, ForcedTag),
    }, nil
}
```

**🔍 Helper-функції (припустима реалізація):**
```go
// pointerTo: повертає *string або nil
func pointerTo(attrs map[string]string, key string) *string {
    if v, ok := attrs[key]; ok && v != "" {
        return &v
    }
    return nil
}

// parseYesNo: парсить "YES"/"NO" → *bool
func parseYesNo(attrs map[string]string, key string) *bool {
    v, ok := attrs[key]
    if !ok {
        return nil
    }
    b := strings.ToUpper(v) == "YES"
    return &b
}
```

### 3️⃣ Метод `String()` — серіалізація зі збереженням семантики
```go
func (mi *MediaItem) String() string {
    slice := []string{
        // Type: без лапок (ENUM)
        fmt.Sprintf(formatString, TypeTag, mi.Type),  // TYPE=AUDIO
        
        // GroupID і Name: завжди в лапках (обов'язкові)
        fmt.Sprintf(quotedFormatString, GroupIDTag, mi.GroupID),  // GROUP-ID="audio"
        fmt.Sprintf(quotedFormatString, NameTag, mi.Name),        // NAME="English"
    }

    // Опціональні поля: додаємо ТІЛЬКИ якщо не-nil
    if mi.Language != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, LanguageTag, *mi.Language))
    }
    if mi.AutoSelect != nil {
        // Булеві: конвертуємо bool → "YES"/"NO"
        slice = append(slice, fmt.Sprintf(formatString, AutoSelectTag, formatYesNo(*mi.AutoSelect)))
    }
    // ... аналогічно для інших полів

    // Фінальна збірка: #EXT-X-MEDIA:ATTR1="val1",ATTR2=val2
    return fmt.Sprintf("%s:%s", MediaItemTag, strings.Join(slice, ","))
}
```

**🎯 Формат виводу:**
```m3u8
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="English",LANGUAGE="en",DEFAULT=YES,AUTOSELECT=YES,URI="audio/en.m3u8"
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **багатомовними субтитрами (AR/EN/RU)**:

### Сценарій 1: Мастер-плейлист з аудіо-доріжками
```go
func generateMasterPlaylist(channelID string, variants []Variant) string {
    var buf strings.Builder
    buf.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
    
    // Аудіо-доріжки
    for _, lang := range []string{"ar", "en", "ru"} {
        media := &m3u8.MediaItem{
            Type:       "AUDIO",
            GroupID:    "audio",
            Name:       languageName(lang),  // "Arabic", "English", "Russian"
            Language:   pointer(lang),
            Default:    pointer(lang == "ar"),  // Арабська — за замовчуванням
            AutoSelect: pointer(true),
            URI:        pointer(fmt.Sprintf("/channels/%s/audio/%s.m3u8", channelID, lang)),
        }
        buf.WriteString(media.String() + "\n")
    }
    
    // Субтитри (з вашого WebSocketDistributor)
    for _, lang := range []string{"en", "ru"} {
        media := &m3u8.MediaItem{
            Type:       "SUBTITLES",
            GroupID:    "subs",
            Name:       languageName(lang) + " Subtitles",
            Language:   pointer(lang),
            AutoSelect: pointer(true),
            Forced:     pointer(false),
            URI:        pointer(fmt.Sprintf("/channels/%s/subs/%s.m3u8", channelID, lang)),
        }
        buf.WriteString(media.String() + "\n")
    }
    
    // Відео-варіанти
    for _, v := range variants {
        buf.WriteString(fmt.Sprintf(
            "#EXT-X-STREAM-INF:BANDWIDTH=%d,AUDIO=\"audio\",SUBTITLES=\"subs\"\n%s\n",
            v.Bandwidth, v.URI,
        ))
    }
    return buf.String()
}
```

### Сценарій 2: Динамічне оновлення при зміні мови
```go
// При отриманні нової мови через WebSocket:
func (s *Server) onSubtitleMessage(msg SubtitleMessage) {
    // ... обробка субтитрів ...
    
    // Якщо з'явилася нова мока субтитрів — оновлюємо Master Playlist
    if !s.hasSubtitleTrack(msg.ChannelID, msg.Language) {
        s.addSubtitleTrack(msg.ChannelID, msg.Language)
        s.regenerateMasterPlaylist(msg.ChannelID)  // Перезаписує master.m3u8
    }
}
```

---

## ⚠️ Критичні моменти та покращення

### 1️⃣ Валідація обов'язкових полів
```go
// Потенційна проблема: код не перевіряє наявність Type/GroupID/Name
func NewMediaItem(text string) (*MediaItem, error) {
    attributes := ParseAttributes(text)
    
    // ✅ Додати валідацію
    required := []string{TypeTag, GroupIDTag, NameTag}
    for _, key := range required {
        if attributes[key] == "" {
            return nil, fmt.Errorf("EXT-X-MEDIA requires %s attribute", key)
        }
    }
    
    // ✅ Валідація Type
    validTypes := map[string]bool{"AUDIO": true, "VIDEO": true, "SUBTITLES": true, "CLOSED-CAPTIONS": true}
    if !validTypes[attributes[TypeTag]] {
        return nil, fmt.Errorf("invalid TYPE: %s", attributes[TypeTag])
    }
    
    // ... решта коду
}
```

### 2️⃣ Екранування лапок у значеннях
```go
// За специфікацією, лапки в атрибутах мають бути екрановані
func escapeAttributeValue(s string) string {
    return strings.ReplaceAll(s, `"`, `\"`)
}

// Використання у String():
fmt.Sprintf(quotedFormatString, NameTag, escapeAttributeValue(mi.Name))
```

### 3️⃣ Порядок атрибутів (best practice)
```go
// Специфікація не вимагає порядку, але для читабельності:
// Рекомендується: TYPE, GROUP-ID, NAME, LANGUAGE, ... URI
slice := []string{
    fmt.Sprintf(formatString, TypeTag, mi.Type),
    fmt.Sprintf(quotedFormatString, GroupIDTag, mi.GroupID),
    fmt.Sprintf(quotedFormatString, NameTag, mi.Name),
}
// Потім опціональні в логічному порядку...
```

### 4️⃣ Кешування результату `String()`
```go
// Якщо MediaItem використовується багаторазово:
type CachedMediaItem struct {
    MediaItem
    mu        sync.RWMutex
    cachedStr string
    dirty     bool
}

func (c *CachedMediaItem) String() string {
    c.mu.RLock()
    if !c.dirty && c.cachedStr != "" {
        defer c.mu.RUnlock()
        return c.cachedStr
    }
    c.mu.RUnlock()
    
    c.mu.Lock()
    defer c.mu.Unlock()
    c.cachedStr = c.MediaItem.String()
    c.dirty = false
    return c.cachedStr
}
```

---

## 🧪 Приклад використання

```go
// ✅ Створення аудіо-доріжки
enAudio := &m3u8.MediaItem{
    Type:       "AUDIO",
    GroupID:    "audio",
    Name:       "English",
    Language:   pointer("en"),
    Default:    pointer(false),
    AutoSelect: pointer(true),
    URI:        pointer("/audio/en.m3u8"),
}
fmt.Println(enAudio.String())
// #EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="English",LANGUAGE="en",AUTOSELECT=YES,URI="/audio/en.m3u8"

// ✅ Парсинг вхідного рядка
line := `TYPE=SUBTITLES,GROUP-ID="subs",NAME="Arabic",LANGUAGE="ar",FORCED=NO,URI="subs/ar.m3u8"`
media, err := m3u8.NewMediaItem(line)
if err != nil {
    log.Fatal(err)
}
fmt.Println(*media.Language)  // "ar"
fmt.Println(*media.Forced)    // false

// ✅ CLOSED-CAPTIONS (без URI)
cc := &m3u8.MediaItem{
    Type:       "CLOSED-CAPTIONS",
    GroupID:    "cc",
    Name:       "English CC",
    Language:   pointer("en"),
    InStreamID: pointer("CC1"),
}
fmt.Println(cc.String())
// #EXT-X-MEDIA:TYPE=CLOSED-CAPTIONS,GROUP-ID="cc",NAME="English CC",LANGUAGE="en",IN-STREAM-ID="CC1"
```

---

## 📋 Специфікація HLS (RFC 8216) — ключові вимоги для `#EXT-X-MEDIA`

```
✅ TYPE має бути одним з: AUDIO, VIDEO, SUBTITLES, CLOSED-CAPTIONS
✅ GROUP-ID має збігатися з атрибутом у EXT-X-STREAM-INF
✅ NAME має бути унікальним в межах групи
✅ Якщо DEFAULT=YES, то максимум один елемент у групі може мати DEFAULT=YES
✅ Якщо TYPE=CLOSED-CAPTIONS, URI НЕ повинен бути вказаний
✅ Якщо TYPE!=CLOSED-CAPTIONS, URI повинен бути вказаний (і вказувати на медіа-плейлист)
✅ LANGUAGE має бути у форматі RFC 5646 (напр. "en", "ar-SA")
✅ FORCED має сенс тільки для SUBTITLES
```

---

## 🎯 Висновок

Цей код — **потужний інструмент** для побудови професійних HLS-плейлистів з підтримкою:

✅ Багатомовного аудіо (критично для Al Arabiya)  
✅ Субтитрів у кількох мовах (AR/EN/RU — ваш use-case)  
✅ Адаптивного вибору доріжок клієнтськими плеєрами  
✅ Відповідності специфікації RFC 8216

**Для вашого проекту**: цей код ідеально інтегрується з `WebSocketDistributorService` — ви можете динамічно додавати нові мовні доріжки субтитрів у Master Playlist при отриманні нових даних через WebSocket.

Потрібно допомогти з:
- 🔗 Інтеграцією `MediaItem` у генерацію Master Playlist?
- 🔄 Динамічним оновленням плейлиста при зміні мов?
- 🧪 Юніт-тестами для `NewMediaItem`/`String()`?

Чекаю на ваші питання! 🛠️