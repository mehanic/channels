# 🔍 Глибокий розбір тестів: `SessionDataItem` для HLS `#EXT-X-SESSION-DATA`

Цей файл містить **два юніт-тести** для парсингу та серіалізації тега `#EXT-X-SESSION-DATA` — механізму передачі довільних метаданих сесії у HLS Master Playlist. Розберемо архітектурно та детально.

---

## 📦 Що таке `#EXT-X-SESSION-DATA` і навіщо він потрібен?

### Контекст: метадані у Master Playlist
```m3u8
#EXTM3U
#EXT-X-VERSION:7

#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.title",VALUE="Live News",LANGUAGE="ar"
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.title",VALUE="Live News",LANGUAGE="en"
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.viewers",VALUE="15420"
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.alerts",URI="/api/alerts/ch1.json"

#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=1280x720
video/720p.m3u8
```

### Призначення атрибутів `SessionDataItem`
| Атрибут | Тип | Обов'язковий? | Призначення |
|---------|-----|---------------|-------------|
| `DATA-ID` | `string` | ✅ Так | Унікальний ідентифікатор даних (зворотний DNS-стиль) |
| `VALUE` | `*string` | ⚠️ Умовно | Вбудоване значення метаданих (або `URI`, не обидва) |
| `URI` | `*string` | ⚠️ Умовно | Посилання на зовнішній ресурс з даними (або `VALUE`) |
| `LANGUAGE` | `*string` | ❌ Ні | Код мови для локалізованих значень (RFC 5646) |

### 🎯 Критичні сценарії використання у вашому проекті
```
📊 Аналітика в реальному часі (CCTV):
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.viewers",VALUE="847"
→ Плеєр може показувати статистику переглядів у інтерфейсі

🌐 Локалізація інтерфейсу:
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.title",VALUE="Прямий ефір",LANGUAGE="uk"
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.title",VALUE="Live Feed",LANGUAGE="en"
→ Автоматичний вибір заголовка за мовою пристрою користувача

🔗 Динамічні метадані через URI:
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.alerts",URI="/api/alerts/ch1.json"
→ Плеєр завантажує актуальні попередження (пожежа, тривога) без перезавантаження плейлиста

🎯 A/B тестування:
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.experiment",VALUE="codec_test_v2"
→ Клієнт адаптує поведінку під експериментальну групу
```

### ⚠️ Критичне обмеження специфікації
```
✅ #EXT-X-SESSION-DATA може з'являтися ТІЛЬКИ у Master Playlist
❌ Заборонено у Media Playlist (сегменти)
✅ Один DATA-ID може мати кілька записів з різними LANGUAGE
✅ VALUE і URI — взаємовиключні (не можна вказувати обидва одночасно)
```

---

## 🔬 Детальний розбір кожного тесту

### Тест 1: `SessionDataItem` з `VALUE`

```go
func TestSessionDataItem_Parse(t *testing.T) {
    // 🎯 Вхідний рядок: дані вбудовані через VALUE
    line := `#EXT-X-SESSION-DATA:DATA-ID="com.test.movie.title",VALUE="Test",LANGUAGE="en"`
    
    // 🎯 Парсинг через конструктор
    sdi, err := m3u8.NewSessionDataItem(line)
    assert.Nil(t, err)
    
    // 🎯 Перевірка обов'язкового DATA-ID
    assert.Equal(t, "com.test.movie.title", sdi.DataID)
    
    // 🎯 Перевірка опціональних полів-покажчиків
    assertNotNilEqual(t, "Test", sdi.Value)      // ✅ VALUE розпаршено
    assert.Nil(t, sdi.URI)                        // ✅ URI = nil (не вказано)
    assertNotNilEqual(t, "en", sdi.Language)      // ✅ LANGUAGE розпаршено
    
    // 🎯 Кругова перевірка: серіалізація має відтворити оригінал
    assertToString(t, line, sdi)  // \n нормалізуються хелпером
}
```

### Тест 2: `SessionDataItem` з `URI`

```go
func TestSessionDataItem_Parse(t *testing.T) {
    // ... перший тест ...
    
    // 🎯 Вхідний рядок: дані через зовнішній URI
    line = `#EXT-X-SESSION-DATA:DATA-ID="com.test.movie.title",URI="http://test",LANGUAGE="en"`
    sdi, err = m3u8.NewSessionDataItem(line)
    assert.Nil(t, err)
    
    // 🎯 Перевірка: VALUE=nil, URI встановлено
    assert.Equal(t, "com.test.movie.title", sdi.DataID)
    assert.Nil(t, sdi.Value)                        // ✅ VALUE = nil (не вказано)
    assertNotNilEqual(t, "http://test", sdi.URI)    // ✅ URI розпаршено
    assertNotNilEqual(t, "en", sdi.Language)
    
    assertToString(t, line, sdi)
}
```

### 🎯 Що тестують ці кейси?
| Аспект | Тест 1 (VALUE) | Тест 2 (URI) | Чому це важливо |
|--------|---------------|--------------|----------------|
| **DATA-ID парсинг** | ✅ `"com.test.movie.title"` | ✅ `"com.test.movie.title"` | Обов'язкове поле, основа ідентифікації |
| **Взаємовиключність** | `Value!=nil, URI=nil` | `Value=nil, URI!=nil` | Специфікація вимагає тільки одне з двох |
| **LANGUAGE парсинг** | ✅ `"en"` → `*string` | ✅ `"en"` → `*string` | Підтримка локалізації метаданих |
| **Кругова перевірка** | `Parse → String() == original` | `Parse → String() == original` | Гарантія консистентності парсингу/серіалізації |

---

## 🏗️ Припустима структура `SessionDataItem`

```go
// 🎯 SessionDataItem — реалізує m3u8.Item для поліморфізму
type SessionDataItem struct {
    DataID   string            // ✅ Обов'язковий: унікальний ідентифікатор
    Value    *string           // Опціональне вбудоване значення
    URI      *string           // Опціональне посилання на зовнішні дані
    Language *string           // Опціональний код мови (RFC 5646)
}

// 🎯 Конструктор: парсинг атрибутів
func NewSessionDataItem(text string) (*SessionDataItem, error) {
    attrs := ParseAttributes(text)  // map[string]string
    
    // 🎯 DATA-ID — обов'язковий
    dataID := attrs[DataIDTag]
    if dataID == "" {
        return nil, fmt.Errorf("EXT-X-SESSION-DATA requires DATA-ID attribute")
    }
    
    // 🎯 VALUE та URI — опціональні, взаємовиключні
    value := pointerTo(attrs, ValueTag)
    uri := pointerTo(attrs, URITag)
    
    // ⚠️ Валідація взаємовиключності (може бути у конструкторі або пізніше)
    if value != nil && uri != nil {
        return nil, fmt.Errorf("VALUE and URI are mutually exclusive in EXT-X-SESSION-DATA")
    }
    
    return &SessionDataItem{
        DataID:   dataID,
        Value:    value,
        URI:      uri,
        Language: pointerTo(attrs, LanguageTag),
    }, nil
}

// 🎯 Серіалізація
func (sdi *SessionDataItem) String() string {
    var attrs []string
    attrs = append(attrs, fmt.Sprintf(`%s="%s"`, DataIDTag, sdi.DataID))
    
    if sdi.Value != nil {
        attrs = append(attrs, fmt.Sprintf(`%s="%s"`, ValueTag, *sdi.Value))
    }
    if sdi.URI != nil {
        attrs = append(attrs, fmt.Sprintf(`%s="%s"`, URITag, *sdi.URI))
    }
    if sdi.Language != nil {
        attrs = append(attrs, fmt.Sprintf(`%s="%s"`, LanguageTag, *sdi.Language))
    }
    
    return fmt.Sprintf("%s:%s", SessionDataItemTag, strings.Join(attrs, ","))
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність тестів на взаємовиключність `VALUE`/`URI`
```go
// ❌ Тести перевіряють тільки "щасливий шлях" (одне з двох)
// ✅ Додати тест на конфліктні атрибути:

func TestSessionDataItem_Parse_Invalid(t *testing.T) {
    // ❌ Обидва вказані → має бути помилка
    line := `#EXT-X-SESSION-DATA:DATA-ID="x",VALUE="a",URI="b"`
    sdi, err := m3u8.NewSessionDataItem(line)
    
    // Залежить від реалізації:
    // • Варіант А: помилка валідації
    assert.Error(t, err, "VALUE and URI are mutually exclusive")
    
    // • Варіант Б: пріоритет одного над іншим (документувати!)
    // assert.NoError(t, err)
    // assert.NotNil(t, sdi.Value)  // або URI
}
```

### 2️⃣ Відсутність валідації формату `DATA-ID`
```go
// ✅ Специфікація рекомендує зворотний DNS-формат:
// • "com.example.feature", "org.alarabiya.analytics"
// ❌ Тест не перевіряє це

func TestSessionDataItem_DataID_Format(t *testing.T) {
    cases := []struct{
        name  string
        id    string
        valid bool
    }{
        {"valid_reverse_dns", "com.example.feature", true},
        {"valid_with_hyphen", "org.alarabiya.live-stats", true},
        {"invalid_no_dot", "invalid", false},
        {"invalid_starts_digit", "123.example", false},
        {"empty", "", false},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            line := fmt.Sprintf(`#EXT-X-SESSION-DATA:DATA-ID="%s",VALUE="x"`, tc.id)
            sdi, err := m3u8.NewSessionDataItem(line)
            
            if tc.valid {
                assert.NoError(t, err)
                assert.Equal(t, tc.id, sdi.DataID)
            } else {
                // Залежить від реалізації: валідувати чи приймати будь-який рядок
                // Рекомендовано: валідувати
                assert.Error(t, err, "invalid DATA-ID format")
            }
        })
    }
}
```

### 3️⃣ Відсутність тестів на `LANGUAGE` без `VALUE`
```go
// ✅ Специфікація: LANGUAGE має сенс тільки разом з VALUE
// ❌ Тест не перевіряє цю логіку

func TestSessionDataItem_Language_Requires_Value(t *testing.T) {
    // ❌ LANGUAGE без VALUE — логічно безглуздо
    line := `#EXT-X-SESSION-DATA:DATA-ID="x",LANGUAGE="en"`
    sdi, err := m3u8.NewSessionDataItem(line)
    
    // Залежить від реалізації:
    // • Варіант А: помилка валідації
    assert.Error(t, err, "LANGUAGE requires VALUE to be specified")
    
    // • Варіант Б: прийняти, але ігнорувати LANGUAGE (документувати!)
}
```

### 4️⃣ Екранування спецсимволів у значеннях
```go
// ❌ Якщо Value містить лапки: "Title with "quotes""
// Вивід: DATA-ID="...",VALUE="Title with "quotes"" → зламе парсер!

// ✅ Специфікація: значення у лапках, тому лапки всередині мають бути екрановані
func escapeAttributeValue(s string) string {
    return strings.ReplaceAll(s, `"`, `\"`)
}

// Використання у серіалізації:
if sdi.Value != nil {
    escaped := escapeAttributeValue(*sdi.Value)
    attrs = append(attrs, fmt.Sprintf(`%s="%s"`, ValueTag, escaped))
}

// ✅ Додати тест на екранування:
func TestSessionDataItem_Value_WithQuotes(t *testing.T) {
    sdi := &m3u8.SessionDataItem{
        DataID: "com.test.title",
        Value:  pointer.ToString(`Title with "quotes"`),
    }
    output := sdi.String()
    assert.Contains(t, output, `VALUE="Title with \"quotes\""`)
}
```

### 5️⃣ Назви тестів: нумерація замість опису
```go
// ❌ Поточна назва:
TestSessionDataItem_Parse  // Що саме тестується?

// ✅ Рекомендовані описові назви:
func TestSessionDataItem_Parse_WithValue(t *testing.T)      // Тест 1
func TestSessionDataItem_Parse_WithURI(t *testing.T)        // Тест 2

// ✅ Або використання subtests:
func TestSessionDataItem(t *testing.T) {
    t.Run("Parse/WithValue", func(t *testing.T) { ... })
    t.Run("Parse/WithURI", func(t *testing.T) { ... })
    t.Run("Parse/Invalid/BothValueAndURI", func(t *testing.T) { ... })
    t.Run("Parse/Invalid/InvalidDataID", func(t *testing.T) { ... })
}
```

### 6️⃣ Відсутність інтеграційного тесту з Master Playlist
```go
// ✅ Додати тест, що показує використання у реальному плейлисті:
func TestSessionDataItem_InMasterPlaylist(t *testing.T) {
    pl := m3u8.NewPlaylist()
    master := true
    pl.Master = &master
    
    // 🎯 Додавання сесійних даних
    sdi1 := &m3u8.SessionDataItem{
        DataID:   "com.alarabiya.title",
        Value:    pointer.ToString("Live News"),
        Language: pointer.ToString("ar"),
    }
    sdi2 := &m3u8.SessionDataItem{
        DataID:   "com.alarabiya.title",
        Value:    pointer.ToString("Live News"),
        Language: pointer.ToString("en"),
    }
    pl.AppendItem(sdi1)
    pl.AppendItem(sdi2)
    
    // 🎯 Додавання варіантів якості
    pl.AppendItem(&m3u8.PlaylistItem{
        Bandwidth: 1280000,
        URI:       "video/720p.m3u8",
    })
    
    output, err := m3u8.Write(pl)
    assert.NoError(t, err)
    
    // 🎯 Перевірка наявності сесійних даних у виводі
    assert.Contains(t, output, `#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.title"`)
    assert.Contains(t, output, `VALUE="Live News"`)
    assert.Contains(t, output, `LANGUAGE="ar"`)
    assert.Contains(t, output, `LANGUAGE="en"`)
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **багатомовними субтитрами** та **WebSocket-оновленнями**:

### 🎯 Сценарій: додавання локалізованих метаданих у Master Playlist
```go
// У generateMasterPlaylist для каналу Al Arabiya:
func generateMasterPlaylist(channelID string, variants []VideoVariant) *m3u8.Playlist {
    pl := m3u8.NewPlaylist()
    master := true
    pl.Master = &master
    pl.Version = pointer(7)
    
    // 🎯 Локалізовані заголовки (AR/EN/UK)
    titles := map[string]string{
        "ar": "بث مباشر",
        "en": "Live Feed",
        "uk": "Прямий ефір",
    }
    for lang, title := range titles {
        pl.AppendItem(&m3u8.SessionDataItem{
            DataID:   "com.alarabiya.title",
            Value:    pointer.ToString(title),
            Language: pointer.ToString(lang),
        })
    }
    
    // 🎯 Статистика переглядів (оновлюється періодично)
    viewers := getViewerCount(channelID)  // Напр. з Redis
    pl.AppendItem(&m3u8.SessionDataItem{
        DataID: "com.alarabiya.viewers",
        Value:  pointer.ToString(strconv.Itoa(viewers)),
    })
    
    // 🎯 Посилання на динамічні метадані (попередження, розклад)
    pl.AppendItem(&m3u8.SessionDataItem{
        DataID: "com.alarabiya.alerts",
        URI:    pointer.ToString(fmt.Sprintf("/api/channels/%s/alerts.json", channelID)),
    })
    
    // 🎯 Додавання варіантів якості
    for _, v := range variants {
        pl.AppendItem(&m3u8.PlaylistItem{
            Bandwidth:  v.Bandwidth,
            URI:        v.URI,
            Resolution: &m3u8.Resolution{Width: v.Width, Height: v.Height},
        })
    }
    
    return pl
}
```

### 🎯 Сценарій: динамічне оновлення сесійних даних через WebSocket
```go
// У WebSocketDistributor при отриманні оновлення метаданих:
func (d *Distributor) onMetadataUpdate(msg MetadataMessage) {
    switch msg.Type {
    case "viewers":
        // 🎯 Оновлення лічильника переглядів
        d.masterPlaylists[msg.ChannelID].updateSessionData(
            "com.alarabiya.viewers",
            pointer.ToString(strconv.Itoa(msg.Value)),
            nil,  // Language не змінюється
        )
        
    case "title":
        // 🎯 Оновлення локалізованої назви
        d.masterPlaylists[msg.ChannelID].updateSessionData(
            "com.alarabiya.title",
            pointer.ToString(msg.Value),
            pointer.ToString(msg.Language),  // Оновлюємо конкретну мову
        )
        
    case "alerts":
        // 🎯 Оновлення посилання на попередження
        d.masterPlaylists[msg.ChannelID].updateSessionData(
            "com.alarabiya.alerts",
            nil,  // Видаляємо VALUE
            pointer.ToString(msg.URI),  // Встановлюємо новий URI
        )
    }
    
    // 📢 Повідомити клієнтів про оновлення Master Playlist
    d.broadcastPlaylistUpdate(msg.ChannelID)
}

// Helper для безпечного оновлення:
func (pl *Playlist) updateSessionData(dataID string, value, uri, language *string) {
    pl.mu.Lock()
    defer pl.mu.Unlock()
    
    // 🎯 Видалити старі записи з тим же DATA-ID (+ Language якщо є)
    filtered := pl.Items[:0]
    for _, item := range pl.Items {
        if sdi, ok := item.(*m3u8.SessionDataItem); ok {
            if sdi.DataID == dataID {
                if language == nil || (sdi.Language != nil && *sdi.Language == *language) {
                    continue  // Пропускаємо старий запис
                }
            }
        }
        filtered = append(filtered, item)
    }
    pl.Items = filtered
    
    // 🎯 Додати оновлений запис
    pl.AppendItem(&m3u8.SessionDataItem{
        DataID:   dataID,
        Value:    value,
        URI:      uri,
        Language: language,
    })
}
```

### 🎯 Сценарій: клієнтська обробка сесійних даних
```go
// На стороні клієнта (JavaScript/TypeScript плеєр):
function processSessionData(playlist: HLSPlaylist) {
    const sessionData = playlist.items.filter(i => i.type === 'session-data');
    
    // 🎯 Вибір заголовка за мовою користувача
    const userLang = navigator.language.split('-')[0];  // "uk", "en", "ar"
    const title = sessionData
        .filter(i => i.dataId === 'com.alarabiya.title')
        .find(i => i.language === userLang)?.value 
        ?? sessionData.find(i => i.dataId === 'com.alarabiya.title')?.value;
    
    document.title = title || 'Live Stream';
    
    // 🎯 Відображення статистики переглядів
    const viewers = sessionData
        .find(i => i.dataId === 'com.alarabiya.viewers')?.value;
    if (viewers) {
        document.getElementById('viewer-count').textContent = `${viewers} watching`;
    }
    
    // 🎯 Завантаження динамічних попереджень
    const alertsURI = sessionData
        .find(i => i.dataId === 'com.alarabiya.alerts')?.uri;
    if (alertsURI) {
        fetch(alertsURI)
            .then(r => r.json())
            .then(data => showAlerts(data));
    }
}
```

---

## 🧪 Приклад: розширений набір тестів для `SessionDataItem`

```go
// ✅ Повний набір тестів з subtests та валідацією:
func TestSessionDataItem(t *testing.T) {
    t.Parallel()
    
    t.Run("Parse/WithValue", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-DATA:DATA-ID="com.test.title",VALUE="Test",LANGUAGE="en"`
        sdi, err := m3u8.NewSessionDataItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "com.test.title", sdi.DataID)
        assertNotNilEqual(t, "Test", sdi.Value)
        assert.Nil(t, sdi.URI)
        assertNotNilEqual(t, "en", sdi.Language)
        assertToString(t, line, sdi)
    })
    
    t.Run("Parse/WithURI", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-DATA:DATA-ID="com.test.alerts",URI="http://test/alerts.json"`
        sdi, err := m3u8.NewSessionDataItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "com.test.alerts", sdi.DataID)
        assert.Nil(t, sdi.Value)
        assertNotNilEqual(t, "http://test/alerts.json", sdi.URI)
    })
    
    t.Run("Parse/Invalid/BothValueAndURI", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-DATA:DATA-ID="x",VALUE="a",URI="b"`
        _, err := m3u8.NewSessionDataItem(line)
        assert.Error(t, err, "VALUE and URI are mutually exclusive")
    })
    
    t.Run("Parse/Invalid/MissingDataID", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-DATA:VALUE="test"`  // ❌ Без DATA-ID
        _, err := m3u8.NewSessionDataItem(line)
        assert.Error(t, err, "DATA-ID is required")
    })
    
    t.Run("Parse/Invalid/InvalidDataIDFormat", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-DATA:DATA-ID="invalid",VALUE="x"`  // ❌ Без крапки
        _, err := m3u8.NewSessionDataItem(line)
        // Залежить від реалізації: валідувати формат чи ні
        // Рекомендовано: валідувати
        assert.Error(t, err, "invalid DATA-ID format")
    })
    
    t.Run("Serialize/ValueWithQuotes", func(t *testing.T) {
        t.Parallel()
        sdi := &m3u8.SessionDataItem{
            DataID: "com.test.title",
            Value:  pointer.ToString(`Title with "quotes"`),
        }
        output := sdi.String()
        // 🎯 Лапки всередині мають бути екрановані
        assert.Contains(t, output, `VALUE="Title with \"quotes\""`)
    })
    
    t.Run("Integration/InMasterPlaylist", func(t *testing.T) {
        t.Parallel()
        pl := m3u8.NewPlaylist()
        pl.Master = pointer.ToBool(true)
        
        // 🎯 Додавання локалізованих заголовків
        for _, lang := range []string{"ar", "en", "uk"} {
            pl.AppendItem(&m3u8.SessionDataItem{
                DataID:   "com.alarabiya.title",
                Value:    pointer.ToString("Live"),
                Language: pointer.ToString(lang),
            })
        }
        
        output, err := m3u8.Write(pl)
        assert.NoError(t, err)
        
        // 🎯 Перевірка наявності всіх мов
        assert.Contains(t, output, `LANGUAGE="ar"`)
        assert.Contains(t, output, `LANGUAGE="en"`)
        assert.Contains(t, output, `LANGUAGE="uk"`)
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги

```
✅ #EXT-X-SESSION-DATA дозволений ТІЛЬКИ у Master Playlist
✅ DATA-ID — обов'язковий, унікальний в межах плейлиста, формат зворотного DNS рекомендовано
✅ VALUE і URI — взаємовиключні: має бути вказано рівно один
✅ Якщо вказано LANGUAGE, то повинен бути вказаний VALUE (логічна вимога)
✅ Один DATA-ID може мати кілька записів з різними LANGUAGE для локалізації
✅ Клієнти МАЮТЬ ігнорувати невідомі DATA-ID (forward compatibility)
✅ Клієнти МОЖУТЬ кешувати дані з URI, але мають перевіряти актуальність
✅ Значення у лапках: спеціальні символи (лапки, зворотні слеші) мають бути екрановані
✅ Порядок атрибутів: не регламентований, але DATA-ID рекомендується першим
```

---

## 🎯 Висновок

Ці тести — **солідна основа** для валідації `SessionDataItem`:

✅ Покриття обох режимів: `VALUE` (вбудовані дані) та `URI` (зовнішні дані)  
✅ Перевірка локалізації через `LANGUAGE`  
✅ Кругова перевірка серіалізації

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати валідацію взаємовиключності `VALUE`/`URI` у конструкторі
2. ✅ Додати валідацію формату `DATA-ID` (зворотний DNS)
3. ✅ Екранувати спецсимволи у значеннях перед серіалізацією
4. ✅ Додати тести на невалідний ввід (відсутній DATA-ID, обидва VALUE+URI)
5. ✅ Перейменувати тести за описовим патерном або використати subtests

**Приклад реалізації для багатомовного CCTV**:
```go
// Генерація локалізованих метаданих для Master Playlist:
func addLocalizedSessionData(pl *m3u8.Playlist, dataID, defaultValue string, translations map[string]string) {
    // 🎯 Універсальне значення (fallback)
    pl.AppendItem(&m3u8.SessionDataItem{
        DataID: dataID,
        Value:  pointer.ToString(defaultValue),
    })
    
    // 🎯 Локалізовані варіанти
    for lang, value := range translations {
        pl.AppendItem(&m3u8.SessionDataItem{
            DataID:   dataID,
            Value:    pointer.ToString(value),
            Language: pointer.ToString(lang),
        })
    }
}

// Використання для Al Arabiya:
addLocalizedSessionData(pl, "com.alarabiya.title", "Live Feed", map[string]string{
    "ar": "بث مباشر",
    "en": "Live Feed", 
    "uk": "Прямий ефір",
    "ru": "Прямой эфир",
})
// → Клієнт автоматично обирає заголовок за мовою пристрою
```

Потрібно допомогти з:
- 🔗 Інтеграцією динамічного оновлення `SessionDataItem` через ваш `WebSocketDistributor`?
- 🧠 Реалізацією валідації `DATA-ID` формату та взаємовиключності `VALUE`/`URI`?
- 🧪 Написанням інтеграційних тестів: Master Playlist → клієнтська обробка → відображення метаданих?

Чекаю на ваші питання! 🛠️📊🌐