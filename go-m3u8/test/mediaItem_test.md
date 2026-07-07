# 🔍 Глибокий розбір тесту: `MediaItem` для HLS `#EXT-X-MEDIA`

Цей файл містить **комплексний юніт-тест** для парсингу та серіалізації тега `#EXT-X-MEDIA` — ключового елемента HLS Master Playlist для опису альтернативних аудіо-доріжок, субтитрів та закритих титрів. Розберемо архітектурно та детально.

---

## 📦 Що таке `#EXT-X-MEDIA` і навіщо він потрібен?

### Контекст: Master Playlist з багатомовними доріжками
```m3u8
#EXTM3U
#EXT-X-VERSION:7

#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="en",NAME="English",DEFAULT=YES,AUTOSELECT=YES,URI="audio/en.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",LANGUAGE="ar",NAME="العربية",AUTOSELECT=YES,URI="audio/ar.m3u8"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",LANGUAGE="uk",NAME="Українські",FORCED=NO,AUTOSELECT=YES,URI="subs/uk.m3u8"

#EXT-X-STREAM-INF:BANDWIDTH=1280000,AUDIO="audio",SUBTITLES="subs"
video/720p.m3u8
```

### Призначення атрибутів `MediaItem`
| Атрибут | Тип | Обов'язковий? | Призначення |
|---------|-----|---------------|-------------|
| `TYPE` | `string` | ✅ Так | Тип медіа: `"AUDIO"`, `"VIDEO"`, `"SUBTITLES"`, `"CLOSED-CAPTIONS"` |
| `GROUP-ID` | `string` | ✅ Так | Ідентифікатор групи для прив'язки до варіантів у `#EXT-X-STREAM-INF` |
| `NAME` | `string` | ✅ Так | Людиноподібне ім'я для відображення в інтерфейсі плеєра |
| `LANGUAGE` | `*string` | ❌ Ні | Код мови (RFC 5646) для автовибору за налаштуваннями пристрою |
| `ASSOC-LANGUAGE` | `*string` | ❌ Ні | Мова асоціації (напр. оригінал дубляжу) |
| `DEFAULT` | `*bool` | ❌ Ні | Обрати за замовчуванням, якщо немає переваг користувача |
| `AUTOSELECT` | `*bool` | ❌ Ні | Дозволити плеєру обирати автоматично |
| `FORCED` | `*bool` | ❌ Ні | Для субтитрів: показувати тільки іноземні частини |
| `URI` | `*string` | ⚠️ Умовно | Посилання на медіа-плейлист (не потрібно для `CLOSED-CAPTIONS`) |
| `IN-STREAM-ID` | `*string` | ❌ Ні | Ідентифікатор вбудованих субтитрів у відеопотоці (`"CC1"`, `"SERVICE3"`) |
| `CHARACTERISTICS` | `*string` | ❌ Ні | Accessibility характеристики (`"public.accessibility.describes-video"`) |
| `CHANNELS` | `*string` | ❌ Ні | Кількість аудіоканалів (`"2"`, `"6"`, `"JOC"`) |
| `STABLE-RENDITION-ID` | `*string` | ❌ Ні | Стабільний ID для аналітики/кешування |

### 🎯 Критичні сценарії використання у вашому проекті
```
🌐 Багатомовний CCTV для глобальної аудиторії:
• Арабська (основна) + англійська + українська аудіо-доріжки
• Автоматичний вибір мови за налаштуваннями пристрою користувача
• Ручне перемикання в інтерфейсі плеєра

♿ Доступність (accessibility):
• Аудіо-опис для слабозорих: CHARACTERISTICS="public.accessibility.describes-video"
• Субтитри для глухих: TYPE=SUBTITLES + CHARACTERISTICS="public.accessibility.transcribes-spoken-dialog"

📡 Вбудовані субтитри (CLOSED-CAPTIONS):
• TYPE=CLOSED-CAPTIONS + IN-STREAM-ID="SERVICE3"
• Дані вже у відеопотоці (CEA-608/708) → URI не потрібен
• Плеєр автоматично декодує з відео

🔗 Синхронізація з вашим WebSocketDistributor:
• Субтитри AR/EN/RU через WebSocket → динамічне додавання #EXT-X-MEDIA
• Клієнти отримують оновлений Master Playlist з новими мовами
```

---

## 🔬 Детальний розбір тесту `TestMediaItem_Parse`

```go
func TestMediaItem_Parse(t *testing.T) {
    // 🎯 Вхідний рядок: багаторядковий формат з багатьма атрибутами
    // ⚠️ УВАГА: у вхідному рядку є синтаксична помилка!
    line := `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-lo",LANGUAGE="fre",
ASSOC-LANGUAGE="spoken",NAME="Francais",AUTOSELECT=YES,
INSTREAM-ID="SERVICE3",CHARACTERISTICS="public.html",
CHANNELS="6",
"DEFAULT=NO,URI="frelo/prog_index.m3u8",STABLE-RENDITION-ID="1234",FORCED=YES
"`
    // 🚨 Помилка у вхідному рядку: `"DEFAULT=NO,URI=...` — зайва лапка перед DEFAULT!
    // Це має призвести до помилки парсингу, але тест очікує успіх (err == nil)
    
    mi, err := m3u8.NewMediaItem(line)
    assert.Nil(t, err)  // ❌ Може не пройти, якщо парсер строгий до синтаксису
    
    // 🎯 Перевірка обов'язкових полів (звичайні ассерції)
    assert.Equal(t, "AUDIO", mi.Type)
    assert.Equal(t, "audio-lo", mi.GroupID)
    assert.Equal(t, "Francais", mi.Name)
    
    // 🎯 Перевірка опціональних полів-покажчиків через хелпер
    assertNotNilEqual(t, "1234", mi.StableRenditionId)      // *string
    assertNotNilEqual(t, "fre", mi.Language)                 // *string (RFC 5646)
    assertNotNilEqual(t, "spoken", mi.AssocLanguage)         // *string
    assertNotNilEqual(t, true, mi.AutoSelect)                // *bool (YES→true)
    assertNotNilEqual(t, false, mi.Default)                  // *bool (NO→false)
    assertNotNilEqual(t, "frelo/prog_index.m3u8", mi.URI)    // *string
    assertNotNilEqual(t, true, mi.Forced)                    // *bool (YES→true)
    assertNotNilEqual(t, "SERVICE3", mi.InStreamID)          // *string
    assertNotNilEqual(t, "public.html", mi.Characteristics)  // *string
    assertNotNilEqual(t, "6", mi.Channels)                   // *string
    
    // 🎯 Кругова перевірка: серіалізація має відтворити нормалізований формат
    // ⚠️ Зверніть увагу: порядок атрибутів у expected відрізняється від вхідного!
    expected := "#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio-lo\",LANGUAGE=\"fre\",ASSOC-LANGUAGE=\"spoken\",NAME=\"Francais\",AUTOSELECT=YES,DEFAULT=NO,URI=\"frelo/prog_index.m3u8\",FORCED=YES,INSTREAM-ID=\"SERVICE3\",CHARACTERISTICS=\"public.html\",CHANNELS=\"6\",STABLE-RENDITION-ID=\"1234\""
    assertToString(t, expected, mi)  // \n нормалізуються хелпером
}
```

### 🎯 Що тестує цей кейс?
| Аспект | Вхід у тесті | Чому це важливо |
|--------|-------------|----------------|
| **Багаторядковий формат** | Атрибути розбиті на кілька рядків | Парсер має об'єднувати рядки перед розбором |
| **Змішані типи значень** | Рядки в лапках, булеві без лапок (`YES`/`NO`) | Специфікація вимагає різного формату для різних типів |
| **Порядок атрибутів** | Вхідний порядок ≠ очікуваний вихідний | Серіалізація може впорядковувати атрибути логічно |
| **Булеві значення** | `AUTOSELECT=YES` → `true`, `DEFAULT=NO` → `false` | Парсинг `YES`/`NO` у `*bool` |
| **Усі опціональні поля** | Присутні всі можливі атрибути | Перевірка повноти підтримки специфікації |

---

## ⚠️ Критичні проблеми у тесті

### 1️⃣ Синтаксична помилка у вхідному рядку
```go
// ❌ У вхідному рядку є помилка:
"DEFAULT=NO,URI="frelo/prog_index.m3u8"
 ^
 |
 Зайва лапка перед DEFAULT!

// 🎯 Наслідки:
// • Наївний парсер (strings.Split по ",") зламається
// • Строгий парсер поверне помилку → тест не пройде (assert.Nil(t, err) fail)
// • Якщо тест проходить → парсер надто толерантний (може приховати реальні помилки)

// ✅ Виправлення вхідного рядка:
line := `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-lo",LANGUAGE="fre",
ASSOC-LANGUAGE="spoken",NAME="Francais",AUTOSELECT=YES,
INSTREAM-ID="SERVICE3",CHARACTERISTICS="public.html",
CHANNELS="6",DEFAULT=NO,URI="frelo/prog_index.m3u8",STABLE-RENDITION-ID="1234",FORCED=YES
`
```

### 2️⃣ Невідповідність порядку атрибутів
```go
// 🎯 Вхідний порядок (приблизно):
// TYPE, GROUP-ID, LANGUAGE, ASSOC-LANGUAGE, NAME, AUTOSELECT, 
// INSTREAM-ID, CHARACTERISTICS, CHANNELS, DEFAULT, URI, STABLE-RENDITION-ID, FORCED

// 🎯 Очікуваний вихідний порядок:
// TYPE, GROUP-ID, LANGUAGE, ASSOC-LANGUAGE, NAME, AUTOSELECT, 
// DEFAULT, URI, FORCED, INSTREAM-ID, CHARACTERISTICS, CHANNELS, STABLE-RENDITION-ID

// ✅ Це нормально: специфікація не вимагає порядку атрибутів
// ✅ Але: тест має документувати, що порядок нормалізується
// ❌ Проблема: якщо парсер змінить порядок у майбутньому → тест зламається без причини

// ✅ Рішення: порівнювати атрибути незалежно від порядку:
func assertMediaItemAttributes(t *testing.T, expected map[string]string, actual *m3u8.MediaItem) {
    t.Helper()
    assert.Equal(t, expected["TYPE"], actual.Type)
    assert.Equal(t, expected["GROUP-ID"], actual.GroupID)
    // ... для кожного атрибута окремо ...
}
```

### 3️⃣ Відсутність тестів на помилки парсингу
```go
// ❌ Тест перевіряє тільки "щасливий шлях"
// ✅ Додати тести на невалідний ввід:

func TestMediaItem_Parse_Invalid(t *testing.T) {
    cases := []struct{
        name  string
        input string
        wantErr bool
    }{
        {"missing_type", `#EXT-X-MEDIA:GROUP-ID="x",NAME="Test"`, true},
        {"missing_group_id", `#EXT-X-MEDIA:TYPE=AUDIO,NAME="Test"`, true},
        {"missing_name", `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="x"`, true},
        {"invalid_type", `#EXT-X-MEDIA:TYPE=INVALID,GROUP-ID="x",NAME="Test"`, true},
        {"uri_required_for_audio", `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="x",NAME="Test"`, true},
        {"uri_not_allowed_for_cc", `#EXT-X-MEDIA:TYPE=CLOSED-CAPTIONS,GROUP-ID="cc",NAME="CC1",INSTREAM-ID="CC1",URI="should-not-be-here.m3u8"`, true},
        {"default_yes_multiple", `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="x",NAME="A",DEFAULT=YES
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="x",NAME="B",DEFAULT=YES`, true},  // Два DEFAULT=YES в одній групі
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            mi, err := m3u8.NewMediaItem(tc.input)
            if tc.wantErr {
                assert.Error(t, err)
                assert.Nil(t, mi)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, mi)
            }
        })
    }
}
```

### 4️⃣ Валідація специфічних правил специфікації
```go
// ✅ Специфікація має додаткові обмеження, які не тестуються:

func TestMediaItem_Validation_Rules(t *testing.T) {
    // 🎯 Правило 1: Якщо DEFAULT=YES, то максимум один елемент у групі
    t.Run("DefaultYes_OnlyOnePerGroup", func(t *testing.T) {
        // Це валідується на рівні Playlist, а не окремого MediaItem
        // Але можна перевірити, що парсер не відхиляє окремий запис з DEFAULT=YES
        line := `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="English",DEFAULT=YES,URI="en.m3u8"`
        mi, err := m3u8.NewMediaItem(line)
        assert.NoError(t, err)
        assertNotNilEqual(t, true, mi.Default)
    })
    
    // 🎯 Правило 2: URI не дозволено для TYPE=CLOSED-CAPTIONS
    t.Run("ClosedCaptions_NoURI", func(t *testing.T) {
        line := `#EXT-X-MEDIA:TYPE=CLOSED-CAPTIONS,GROUP-ID="cc",NAME="CC1",INSTREAM-ID="CC1"`
        mi, err := m3u8.NewMediaItem(line)
        assert.NoError(t, err)
        assert.Nil(t, mi.URI)  // URI має бути nil або не виводитися
        
        // ❌ Невалідний випадок: CLOSED-CAPTIONS з URI
        lineInvalid := `#EXT-X-MEDIA:TYPE=CLOSED-CAPTIONS,GROUP-ID="cc",NAME="CC1",INSTREAM-ID="CC1",URI="should-not-be-here.m3u8"`
        mi2, err2 := m3u8.NewMediaItem(lineInvalid)
        assert.Error(t, err2, "URI should not be specified for TYPE=CLOSED-CAPTIONS")
    })
    
    // 🎯 Правило 3: FORCED має сенс тільки для SUBTITLES
    t.Run("Forced_OnlyForSubtitles", func(t *testing.T) {
        // FORCED=YES для AUDIO — технічно допустимо, але логічно безглуздо
        // Парсер може прийняти, але валідатор плейлиста має попередити
        line := `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="x",NAME="Test",FORCED=YES,URI="test.m3u8"`
        mi, err := m3u8.NewMediaItem(line)
        // Залежить від реалізації: помилка, попередження, чи прийняття
        assert.NoError(t, err)  // або assert.Error, якщо валідація строга
    })
}
```

### 5️⃣ Назва тесту не відображає складність
```go
// ❌ TestMediaItem_Parse — надто загальна назва
// ✅ Кращі варіанти:
func TestMediaItem_Parse_FullAttributes(t *testing.T)           // Акцент на повному наборі
func TestMediaItem_Parse_And_Serialize(t *testing.T)            // Акцент на круговій перевірці
func TestMediaItem_RoundTrip_Complex(t *testing.T)              // Акцент на round-trip

// ✅ Або subtests для покриття всіх аспектів:
func TestMediaItem(t *testing.T) {
    t.Run("Parse/FullAttributes", func(t *testing.T) { ... })
    t.Run("Parse/MinimalRequired", func(t *testing.T) { ... })
    t.Run("Parse/ClosedCaptions_NoURI", func(t *testing.T) { ... })
    t.Run("Serialize/AttributeOrder", func(t *testing.T) { ... })
    t.Run("Validate/InvalidInputs", func(t *testing.T) { ... })
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **багатомовними субтитрами** та **WebSocket-оновленнями**:

### 🎯 Сценарій: генерація Master Playlist з багатомовними аудіо-доріжками
```go
// У generateMasterPlaylist для каналу Al Arabiya:
func generateMasterPlaylist(channelID string, variants []VideoVariant, audioTracks []AudioTrack) *m3u8.Playlist {
    pl := m3u8.NewPlaylist()
    master := true
    pl.Master = &master
    pl.Version = pointer(7)
    
    // 🎯 Додавання аудіо-доріжок (AR/EN/UK)
    for i, track := range audioTracks {
        media := &m3u8.MediaItem{
            Type:       "AUDIO",
            GroupID:    "audio",
            Name:       track.Name,              // "العربية", "English", "Українська"
            Language:   pointer(track.Language), // "ar", "en", "uk"
            AutoSelect: pointer(true),
            URI:        pointer(fmt.Sprintf("/channels/%s/audio/%s.m3u8", channelID, track.Language)),
        }
        
        // 🎯 Перша доріжка (арабська) — за замовчуванням
        if track.Language == "ar" {
            media.Default = pointer(true)
        }
        
        // 🎯 Додаткові метадані для доступності
        if track.IsDescriptive {
            media.Characteristics = pointer("public.accessibility.describes-video")
        }
        if track.Channels != "" {
            media.Channels = pointer(track.Channels)  // "2", "6"
        }
        
        pl.AppendItem(media)
    }
    
    // 🎯 Додавання субтитрів (динамічно через WebSocketDistributor)
    for _, lang := range []string{"en", "uk", "ru"} {
        media := &m3u8.MediaItem{
            Type:       "SUBTITLES",
            GroupID:    "subs",
            Name:       fmt.Sprintf("%s Subtitles", languageName(lang)),
            Language:   pointer(lang),
            AutoSelect: pointer(true),
            Forced:     pointer(false),  // Показувати тільки при виборі користувачем
            URI:        pointer(fmt.Sprintf("/channels/%s/subs/%s.m3u8", channelID, lang)),
        }
        pl.AppendItem(media)
    }
    
    // 🎯 Додавання варіантів якості відео
    for _, v := range variants {
        pl.AppendItem(&m3u8.PlaylistItem{
            Bandwidth:  v.Bandwidth,
            URI:        v.URI,
            Resolution: &m3u8.Resolution{Width: v.Width, Height: v.Height},
            Codecs:     pointer(v.Codecs),
            Audio:      pointer("audio"),  // 🔗 Посилання на групу "audio"
            Subtitles:  pointer("subs"),   // 🔗 Посилання на групу "subs"
        })
    }
    
    return pl
}
```

### 🎯 Сценарій: динамічне додавання нових мов субтитрів через WebSocket
```go
// У WebSocketDistributor при отриманні нової мови:
func (d *Distributor) onNewSubtitleLanguage(channelID, language, name string) {
    // 🎯 Перевірка, чи мова вже додана
    if d.hasSubtitleTrack(channelID, language) {
        return  // Вже існує → не додавати дублікат
    }
    
    // 🎯 Створення нового #EXT-X-MEDIA для субтитрів
    media := &m3u8.MediaItem{
        Type:       "SUBTITLES",
        GroupID:    "subs",
        Name:       name,  // "Українські субтитри"
        Language:   pointer(language),
        AutoSelect: pointer(true),
        URI:        pointer(fmt.Sprintf("/channels/%s/subs/%s.m3u8", channelID, language)),
    }
    
    // 🎯 Додавання у Master Playlist
    pl := d.masterPlaylists[channelID]
    pl.AppendItem(media)
    
    // 🎯 Інвалідація кешу + сповіщення клієнтів
    d.invalidatePlaylistCache(channelID)
    d.broadcastPlaylistUpdate(channelID)
    
    d.logger.Info("added new subtitle language", 
        "channel", channelID, 
        "language", language, 
        "name", name)
}
```

### 🎯 Сценарій: валідація MediaItem перед додаванням у плейлист
```go
// У segmentFinalizer або playlist generator:
func validateMediaItem(mi *m3u8.MediaItem) error {
    // ✅ Обов'язкові поля
    if mi.Type == "" {
        return fmt.Errorf("TYPE is required in EXT-X-MEDIA")
    }
    if mi.GroupID == "" {
        return fmt.Errorf("GROUP-ID is required in EXT-X-MEDIA")
    }
    if mi.Name == "" {
        return fmt.Errorf("NAME is required in EXT-X-MEDIA")
    }
    
    // ✅ Валідація TYPE
    validTypes := map[string]bool{
        "AUDIO": true, "VIDEO": true, "SUBTITLES": true, "CLOSED-CAPTIONS": true,
    }
    if !validTypes[mi.Type] {
        return fmt.Errorf("invalid TYPE: %s", mi.Type)
    }
    
    // ✅ URI правила
    if mi.Type == "CLOSED-CAPTIONS" && mi.URI != nil {
        return fmt.Errorf("URI is not allowed for TYPE=CLOSED-CAPTIONS")
    }
    if mi.Type != "CLOSED-CAPTIONS" && mi.URI == nil {
        return fmt.Errorf("URI is required for TYPE=%s", mi.Type)
    }
    
    // ✅ IN-STREAM-ID тільки для CLOSED-CAPTIONS
    if mi.InStreamID != nil && mi.Type != "CLOSED-CAPTIONS" {
        return fmt.Errorf("IN-STREAM-ID is only allowed for TYPE=CLOSED-CAPTIONS")
    }
    
    // ✅ FORCED тільки для SUBTITLES (попередження, не помилка)
    if mi.Forced != nil && *mi.Forced && mi.Type != "SUBTITLES" {
        log.Warn("FORCED=YES only makes sense for SUBTITLES", 
            "type", mi.Type, "name", mi.Name)
    }
    
    // ✅ LANGUAGE формат (RFC 5646)
    if mi.Language != nil && !isValidLanguageCode(*mi.Language) {
        return fmt.Errorf("invalid LANGUAGE format (expected RFC 5646): %s", *mi.Language)
    }
    
    return nil
}

func isValidLanguageCode(code string) bool {
    // Проста перевірка: 2-3 літери, опціонально з регіоном після дефісу
    // "en", "ar", "uk", "en-US", "ar-SA"
    matched, _ := regexp.MatchString(`^[a-zA-Z]{2,3}(-[a-zA-Z0-9]{2,4})?$`, code)
    return matched
}
```

### 🎯 Сценарій: фільтрація доріжок за можливостями клієнта
```go
// У WebSocketDistributor при підключенні нового клієнта:
func (d *Distributor) filterMediaTracks(clientCaps ClientCapabilities, items []*m3u8.MediaItem) []*m3u8.MediaItem {
    var filtered []*m3u8.MediaItem
    
    for _, item := range items {
        // 🎯 Фільтр за типом (клієнт не підтримує субтитри)
        if item.Type == "SUBTITLES" && !clientCaps.SupportsSubtitles {
            continue
        }
        
        // 🎯 Фільтр за мовою (клієнт хоче тільки арабську + англійську)
        if item.Language != nil {
            if !clientCaps.PreferredLanguages[*item.Language] && 
               *item.Language != "ar" {  // Завжди залишати основну мову
                continue
            }
        }
        
        // 🎯 Фільтр за каналами (клієнт не підтримує 5.1)
        if item.Channels != nil && *item.Channels == "6" && !clientCaps.Supports51Audio {
            continue
        }
        
        filtered = append(filtered, item)
    }
    
    return filtered
}
```

---

## 🧪 Приклад: розширений набір тестів для `MediaItem`

```go
// ✅ Повний набір тестів з subtests та валідацією:
func TestMediaItem(t *testing.T) {
    t.Parallel()
    
    t.Run("Parse/FullAttributes", func(t *testing.T) {
        t.Parallel()
        // ✅ Виправлений вхідний рядок (без синтаксичної помилки)
        line := `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-lo",LANGUAGE="fre",ASSOC-LANGUAGE="spoken",NAME="Francais",AUTOSELECT=YES,INSTREAM-ID="SERVICE3",CHARACTERISTICS="public.html",CHANNELS="6",DEFAULT=NO,URI="frelo/prog_index.m3u8",STABLE-RENDITION-ID="1234",FORCED=YES`
        
        mi, err := m3u8.NewMediaItem(line)
        assert.NoError(t, err)
        
        // Обов'язкові поля
        assert.Equal(t, "AUDIO", mi.Type)
        assert.Equal(t, "audio-lo", mi.GroupID)
        assert.Equal(t, "Francais", mi.Name)
        
        // Опціональні поля
        assertNotNilEqual(t, "fre", mi.Language)
        assertNotNilEqual(t, true, mi.AutoSelect)
        assertNotNilEqual(t, false, mi.Default)
        // ... інші поля ...
    })
    
    t.Run("Parse/MinimalRequired", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="English",URI="en.m3u8"`
        mi, err := m3u8.NewMediaItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "AUDIO", mi.Type)
        assert.Nil(t, mi.Language)  // Опціональні = nil
    })
    
    t.Run("Parse/ClosedCaptions_NoURI", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-MEDIA:TYPE=CLOSED-CAPTIONS,GROUP-ID="cc",NAME="CC1",INSTREAM-ID="CC1"`
        mi, err := m3u8.NewMediaItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "CLOSED-CAPTIONS", mi.Type)
        assert.Nil(t, mi.URI)  // URI не повинен бути вказаний
        assertNotNilEqual(t, "CC1", mi.InStreamID)
    })
    
    t.Run("Parse/Invalid/MissingRequired", func(t *testing.T) {
        t.Parallel()
        cases := []string{
            `#EXT-X-MEDIA:GROUP-ID="x",NAME="Test"`,  // Без TYPE
            `#EXT-X-MEDIA:TYPE=AUDIO,NAME="Test"`,     // Без GROUP-ID
            `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="x"`,    // Без NAME
        }
        for _, input := range cases {
            t.Run(input, func(t *testing.T) {
                _, err := m3u8.NewMediaItem(input)
                assert.Error(t, err, "missing required attribute should fail")
            })
        }
    })
    
    t.Run("Parse/Invalid/ClosedCaptionsWithURI", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-MEDIA:TYPE=CLOSED-CAPTIONS,GROUP-ID="cc",NAME="CC1",INSTREAM-ID="CC1",URI="should-not-be-here.m3u8"`
        _, err := m3u8.NewMediaItem(line)
        assert.Error(t, err, "URI should not be allowed for CLOSED-CAPTIONS")
    })
    
    t.Run("Serialize/AttributeOrder", func(t *testing.T) {
        t.Parallel()
        // 🎯 Перевірка, що порядок атрибутів логічний (не обов'язково як у вхідному)
        mi := &m3u8.MediaItem{
            Type:       "AUDIO",
            GroupID:    "audio",
            Name:       "English",
            Language:   pointer("en"),
            Default:    pointer(true),
            URI:        pointer("en.m3u8"),
        }
        output := mi.String()
        
        // 🎯 Перевірка наявності ключових атрибутів (незалежно від порядку)
        assert.Contains(t, output, "TYPE=AUDIO")
        assert.Contains(t, output, `GROUP-ID="audio"`)
        assert.Contains(t, output, `NAME="English"`)
        assert.Contains(t, output, `LANGUAGE="en"`)
        assert.Contains(t, output, "DEFAULT=YES")
        assert.Contains(t, output, `URI="en.m3u8"`)
    })
    
    t.Run("RoundTrip/WithAllAttributes", func(t *testing.T) {
        t.Parallel()
        original := `#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",LANGUAGE="uk",NAME="Українські",FORCED=NO,AUTOSELECT=YES,URI="subs/uk.m3u8"`
        mi, err := m3u8.NewMediaItem(original)
        assert.NoError(t, err)
        
        output := mi.String()
        // 🎯 Порівняння з нормалізацією (порядок може змінитися)
        assert.Equal(t, normalizeM3U8(original), normalizeM3U8(output))
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до `#EXT-X-MEDIA`

```
✅ TYPE — обов'язковий, допустимі значення:
   • "AUDIO": альтернативні аудіо-доріжки
   • "VIDEO": альтернативні відео-доріжки (рідко використовується)
   • "SUBTITLES": субтитри у окремому плейлисті
   • "CLOSED-CAPTIONS": вбудовані субтитри у відеопотоці (CEA-608/708)

✅ GROUP-ID — обов'язковий, унікальний в межах плейлиста для групування:
   • Використовується у #EXT-X-STREAM-INF: AUDIO="audio", SUBTITLES="subs"
   • Всі елементи з однаковим GROUP-ID мають однаковий TYPE

✅ NAME — обов'язковий, людиноподібне ім'я для відображення в інтерфейсі плеєра
✅ ЯКЩО DEFAULT=YES, то максимум ОДИН елемент у групі може мати DEFAULT=YES
✅ URI:
   • Обов'язковий для AUDIO/VIDEO/SUBTITLES
   • ЗАБОРОНЕНИЙ для CLOSED-CAPTIONS (дані вже у відео)
   • Має вказувати на валідний M3U8 плейлист

✅ LANGUAGE — опціональний, формат RFC 5646 ("en", "ar", "uk", "en-US")
✅ FORCED — має сенс тільки для SUBTITLES:
   • YES = показувати автоматично, якщо мова контенту ≠ мова користувача
   • Використовується для "іноземних" частин фільму

✅ IN-STREAM-ID — тільки для CLOSED-CAPTIONS:
   • "CC1", "CC2", "SERVICE1", "SERVICE3" — ідентифікатори у відеопотоці
   • Плеєр автоматично декодує CEA-608/708 дані з відео

✅ CHARACTERISTICS — опціональний, формат зворотного DNS:
   • "public.accessibility.describes-video" (аудіо-опис)
   • "public.accessibility.transcribes-spoken-dialog" (субтитри)

✅ CHANNELS — опціональний:
   • "2" = стерео, "6" = 5.1, "JOC" = Dolby Atmos (Joint Object Coding)

✅ Клієнти МАЮТЬ ігнорувати невідомі атрибути (forward compatibility)
✅ Клієнти МОЖУТЬ фільтрувати доріжки за можливостями пристрою (мова, канали, DRM)
```

---

## 🎯 Висновок

Цей тест — **потужна перевірка** підтримки складних атрибутів `#EXT-X-MEDIA`:

✅ Покриття всіх опціональних полів специфікації  
✅ Перевірка парсингу булевих значень (`YES`/`NO` → `*bool`)  
✅ Кругова перевірка серіалізації з нормалізацією порядку

**Критичні виправлення для цього тесту**:

1. ✅ **Виправити синтаксичну помилку** у вхідному рядку (`"DEFAULT=NO` → `DEFAULT=NO`)
2. ✅ Додати тести на **невалідний ввід** (відсутні обов'язкові поля, конфлікти URI/TYPE)
3. ✅ Додати валідацію **специфічних правил** (CLOSED-CAPTIONS без URI, DEFAULT=YES тільки один)
4. ✅ Документувати, що **порядок атрибутів нормалізується** при серіалізації
5. ✅ Перейменувати тест або використати subtests для кращої організації

**Приклад інтеграції для Al Arabiya CCTV**:
```go
// Динамічна генерація багатомовного Master Playlist:
audioTracks := []AudioTrack{
    {Language: "ar", Name: "العربية", Default: true, Channels: "2"},
    {Language: "en", Name: "English", Default: false, Channels: "2"},
    {Language: "uk", Name: "Українська", Default: false, Channels: "2", IsDescriptive: true},
}

subtitleTracks := []SubtitleTrack{
    {Language: "en", Name: "English Subtitles"},
    {Language: "uk", Name: "Українські субтитри"},
    {Language: "ru", Name: "Русские субтитры"},
}

master := generateMasterPlaylist("alarabiya-live", videoVariants, audioTracks)
// → Клієнти автоматично обирають мову, плеєр показує доступні субтитри
```

Потрібно допомогти з:
- 🔗 Інтеграцією динамічного оновлення `#EXT-X-MEDIA` через ваш `WebSocketDistributor`?
- 🧠 Реалізацією валідатора `MediaItem` з перевіркою специфічних правил специфікації?
- 🧪 Написанням інтеграційних тестів для повного циклу: парсинг → модифікація → серіалізація → клієнтське відтворення?

Чекаю на ваші питання! 🛠️🌐🔊