# 🔍 Глибокий розбір коду: `SessionDataItem` для HLS Master Playlist

Цей код реалізує роботу з тегом **`#EXT-X-SESSION-DATA`** — механізмом для передачі **довільних метаданих сесії** у HLS Master Playlist. Розберемо детально.

---

## 📦 Що таке `#EXT-X-SESSION-DATA` і навіщо він потрібен?

### Контекст: Master Playlist з метаданими
```m3u8
#EXTM3U
#EXT-X-VERSION:7

#EXT-X-SESSION-DATA:DATA-ID="com.example.viewer_count",VALUE="15420"
#EXT-X-SESSION-DATA:DATA-ID="com.example.content.genre",VALUE="news"
#EXT-X-SESSION-DATA:DATA-ID="com.example.ad_campaign",URI="/metadata/campaign_123.json"
#EXT-X-SESSION-DATA:DATA-ID="com.example.title",VALUE="Evening News",LANGUAGE="en"
#EXT-X-SESSION-DATA:DATA-ID="com.example.title",VALUE="Вечірні новини",LANGUAGE="uk"

#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=1280x720
video/720p.m3u8
```

### Призначення `#EXT-X-SESSION-DATA`
| Атрибут | Тип | Призначення | Приклад |
|---------|-----|-------------|---------|
| `DATA-ID` | **string** (обов'язковий) | Унікальний ідентифікатор даних (зворотний DNS-стиль) | `"com.example.viewer_count"` |
| `VALUE` | `*string` (опціональний) | Безпосереднє значення метаданих | `"15420"`, `"news"` |
| `URI` | `*string` (опціональний) | Посилання на зовнішній ресурс з даними (JSON, XML) | `"/metadata/campaign.json"` |
| `LANGUAGE` | `*string` (опціональний) | Код мови для локалізованих значень (RFC 5646) | `"en"`, `"uk"`, `"ar"` |

### 🎯 Сценарії використання у вашому проекті
```
📊 Аналітика в реальному часі (CCTV):
#EXT-X-SESSION-DATA:DATA-ID="com.cctv.viewers",VALUE="847"
#EXT-X-SESSION-DATA:DATA-ID="com.cctv.region",VALUE="Kyiv"
→ Клієнт може показувати статистику переглядів

🌐 Локалізація інтерфейсу:
#EXT-X-SESSION-DATA:DATA-ID="com.cctv.title",VALUE="Live Feed",LANGUAGE="en"
#EXT-X-SESSION-DATA:DATA-ID="com.cctv.title",VALUE="Прямий ефір",LANGUAGE="uk"
#EXT-X-SESSION-DATA:DATA-ID="com.cctv.title",VALUE="بث مباشر",LANGUAGE="ar"
→ Плеєр обирає значення за мовою користувача

🎯 A/B тестування та персоналізація:
#EXT-X-SESSION-DATA:DATA-ID="com.cctv.experiment",VALUE="codec_av1_test"
#EXT-X-SESSION-DATA:DATA-ID="com.cctv.user_tier",VALUE="premium"
→ Клієнт адаптує поведінку під групу користувача

🔗 Динамічні метадані через URI:
#EXT-X-SESSION-DATA:DATA-ID="com.cctv.alerts",URI="/api/alerts/channel_1.json"
→ Клієнт завантажує актуальні попередження (пожежа, тривога)
```

### ⚠️ Критичне обмеження специфікації
```
✅ #EXT-X-SESSION-DATA може з'являтися ТІЛЬКИ у Master Playlist
❌ Заборонено у Media Playlist (сегменти)
✅ Один DATA-ID може мати кілька записів з різними LANGUAGE
✅ VALUE і URI — взаємовиключні (не можна вказувати обидва одночасно)
```

---

## 🏗️ Struct `SessionDataItem` — карта сесійних даних

```go
type SessionDataItem struct {
    DataID   string   // ✅ Обов'язковий: унікальний ідентифікатор (зворотний DNS)
    Value    *string  // Опціональне значення: nil = не вказано
    URI      *string  // Опціональне посилання: nil = не вказано
    Language *string  // Опціональна мова: nil = значення універсальне
}
```

### 🎯 Чому `DataID` — `string`, а решта — `*T`?
```go
// DataID — ОБОВ'ЯЗКОВИЙ за специфікацією:
// • Без DATA-ID тег не має сенсу
// • Тому string: якщо порожній → помилка валідації

// Value/URI/Language — опціональні:
// • nil = атрибут відсутній у серіалізації
// • &"value" = атрибут виводиться зі значенням

// Це дозволяє гнучко формувати теги:
// • Тільки DATA-ID + VALUE
// • DATA-ID + URI (для великих даних)
// • Кілька записів з різними LANGUAGE
```

### 🎯 Патерн "VALUE або URI, але не обидва"
```go
// Специфікація вимагає:
// • АБО Value, АБО URI — не можна вказувати обидва
// • Це забезпечує однозначність: дані або вбудовані, або зовнішні

// ✅ Валідація має бути на рівні бізнес-логіки:
func (sdi *SessionDataItem) Validate() error {
    if sdi.DataID == "" {
        return fmt.Errorf("DATA-ID is required")
    }
    if sdi.Value != nil && sdi.URI != nil {
        return fmt.Errorf("VALUE and URI are mutually exclusive")
    }
    if sdi.Value == nil && sdi.URI == nil {
        return fmt.Errorf("either VALUE or URI must be specified")
    }
    return nil
}
```

---

## 🔧 Конструктор `NewSessionDataItem` — парсинг атрибутів

```go
func NewSessionDataItem(text string) (*SessionDataItem, error) {
    // Крок 1: Парсинг атрибутів з рядка
    // Вхід: 'DATA-ID="com.example.title",VALUE="News",LANGUAGE="en"'
    // Вихід: map[string]string{"DATA-ID": "com.example.title", "VALUE": "News", ...}
    attributes := ParseAttributes(text)

    // Крок 2: Побудова об'єкта
    return &SessionDataItem{
        // DataID — обов'язковий, але не валідується тут ⚠️
        DataID:   attributes[DataIDTag],
        
        // Опціональні поля: pointerTo повертає *string або nil
        Value:    pointerTo(attributes, ValueTag),      // nil якщо відсутній
        URI:      pointerTo(attributes, URITag),        // nil якщо відсутній
        Language: pointerTo(attributes, LanguageTag),   // nil якщо відсутній
    }, nil
}
```

### 🔍 Helper `pointerTo` (припустима реалізація)
```go
func pointerTo(attrs map[string]string, key string) *string {
    if v, ok := attrs[key]; ok && v != "" {
        return &v  // Повертаємо покажчик на значення з map
    }
    return nil  // Атрибут відсутній або порожній
}
```

### ⚠️ Потенційні проблеми конструктора
```go
// ❌ Проблема 1: DataID не валідується
// Якщо attributes[DataIDTag] == "" → сформується невалідний об'єкт
// → Помилка виявиться тільки при серіалізації або використанні

// ✅ Рішення: додати валідацію
func NewSessionDataItem(text string) (*SessionDataItem, error) {
    attributes := ParseAttributes(text)
    
    dataID := attributes[DataIDTag]
    if dataID == "" {
        return nil, fmt.Errorf("EXT-X-SESSION-DATA requires DATA-ID attribute")
    }
    
    // ✅ Валідація формату DATA-ID (зворотний DNS)
    if !isValidReverseDNS(dataID) {
        return nil, fmt.Errorf("invalid DATA-ID format: %s (expected reverse DNS)", dataID)
    }
    
    return &SessionDataItem{
        DataID:   dataID,
        Value:    pointerTo(attributes, ValueTag),
        URI:      pointerTo(attributes, URITag),
        Language: pointerTo(attributes, LanguageTag),
    }, nil
}

func isValidReverseDNS(id string) bool {
    // Проста перевірка: має містити крапку, не починатися з цифри
    parts := strings.Split(id, ".")
    return len(parts) >= 2 && parts[0] != "" && !unicode.IsDigit(rune(parts[0][0]))
}
```

---

## 🔄 Метод `String()` — серіалізація зі збереженням семантики

```go
func (sdi *SessionDataItem) String() string {
    // 🎯 DATA-ID завжди виводиться першим (обов'язковий, у лапках)
    slice := []string{
        fmt.Sprintf(quotedFormatString, DataIDTag, sdi.DataID),
        // Результат: `DATA-ID="com.example.title"`
    }
    
    // 🎯 Опціональні атрибути: додаємо ТІЛЬКИ якщо не-nil
    if sdi.Value != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, ValueTag, *sdi.Value))
    }
    if sdi.URI != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, URITag, *sdi.URI))
    }
    if sdi.Language != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, LanguageTag, *sdi.Language))
    }
    
    // 🎯 Фінальна збірка: #EXT-X-SESSION-DATA:ATTR1="val1",ATTR2="val2"
    return fmt.Sprintf(`%s:%s`, SessionDataItemTag, strings.Join(slice, ","))
}
```

### 🎯 Формат виводу (приклади)
```m3u8
#EXT-X-SESSION-DATA:DATA-ID="com.example.viewer_count",VALUE="15420"

#EXT-X-SESSION-DATA:DATA-ID="com.example.title",VALUE="Evening News",LANGUAGE="en"

#EXT-X-SESSION-DATA:DATA-ID="com.example.alerts",URI="/api/alerts/ch1.json"

#EXT-X-SESSION-DATA:DATA-ID="com.example.title",VALUE="Прямий ефір",LANGUAGE="uk"
```

### ⚠️ Порядок атрибутів у виводі
```go
// Поточний порядок: DATA-ID → Value → URI → Language
// ✅ Це логічно: обов'язковий атрибут першим, потім опціональні

// Але специфікація не вимагає порядку → плеєри мають бути толерантні
// ✅ Рекомендація: документувати порядок для читабельності
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність валідації взаємовиключності VALUE/URI
```go
// ❌ Поточний код дозволяє створити невалідний об'єкт:
sdi := &m3u8.SessionDataItem{
    DataID: "com.example.test",
    Value:  pointer("inline"),
    URI:    pointer("/external.json"),  // ⚠️ Обидва вказані!
}
// Серіалізація виведе обидва атрибута → невалідний HLS!

// ✅ Рішення 1: валідація у конструкторі
func NewSessionDataItem(text string) (*SessionDataItem, error) {
    // ... парсинг ...
    
    value := pointerTo(attributes, ValueTag)
    uri := pointerTo(attributes, URITag)
    
    if value != nil && uri != nil {
        return nil, fmt.Errorf("VALUE and URI are mutually exclusive in EXT-X-SESSION-DATA")
    }
    if value == nil && uri == nil {
        return nil, fmt.Errorf("either VALUE or URI must be specified")
    }
    
    return &SessionDataItem{
        DataID: attributes[DataIDTag],
        Value:  value,
        URI:    uri,
        Language: pointerTo(attributes, LanguageTag),
    }, nil
}

// ✅ Рішення 2: метод валідації для виклику перед використанням
func (sdi *SessionDataItem) Validate() error {
    if sdi.DataID == "" {
        return fmt.Errorf("DATA-ID is required")
    }
    if (sdi.Value != nil) == (sdi.URI != nil) {  // XOR перевірка
        return fmt.Errorf("exactly one of VALUE or URI must be specified")
    }
    return nil
}
```

### 2️⃣ Екранування спецсимволів у значеннях
```go
// ❌ Якщо Value містить лапки: "Title with "quotes""
// Вивід: DATA-ID="...",VALUE="Title with "quotes"" → зламе парсер!

// ✅ Специфікація: значення у лапках, тому лапки всередині мають бути екрановані
func escapeAttributeValue(s string) string {
    return strings.ReplaceAll(s, `"`, `\"`)
}

// Використання у String():
if sdi.Value != nil {
    escaped := escapeAttributeValue(*sdi.Value)
    slice = append(slice, fmt.Sprintf(quotedFormatString, ValueTag, escaped))
}
```

### 3️⃣ Валідація формату `DATA-ID`
```go
// ❌ Поточний код приймає будь-який рядок:
// "invalid", "123", "" → всі пройде

// ✅ Рекомендований формат: зворотний DNS (RFC 8216)
// "com.company.feature", "org.example.analytics.viewers"

func isValidDataID(id string) bool {
    if id == "" {
        return false
    }
    // Проста перевірка: має містити хоча б одну крапку
    // і не починатися з цифри/крапки
    if !strings.Contains(id, ".") {
        return false
    }
    if id[0] == '.' || id[len(id)-1] == '.' {
        return false
    }
    // Додатково: перевірка на допустимі символи
    for _, r := range id {
        if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '-' {
            return false
        }
    }
    return true
}
```

### 4️⃣ Обробка `LANGUAGE` без `VALUE`
```go
// ❌ Логічна помилка: LANGUAGE має сенс тільки разом з VALUE
// #EXT-X-SESSION-DATA:DATA-ID="x",LANGUAGE="en"  ← без значення?

// ✅ Валідація: якщо Language вказаний, Value теж має бути
func (sdi *SessionDataItem) Validate() error {
    // ... інші перевірки ...
    
    if sdi.Language != nil && sdi.Value == nil {
        return fmt.Errorf("LANGUAGE requires VALUE to be specified")
    }
    return nil
}
```

### 5️⃣ Thread-safety при спільному доступі
```go
// ❌ У вашому pipeline (WebSocketDistributor + playlist generation):
item := &SessionDataItem{DataID: "com.cctv.viewers", Value: pointer("100")}
pl.AppendItem(item)  // Горутина 1: запис
s := item.String()   // Горутина 2: читання → DATA RACE!

// ✅ Рішення: immutable патерн (найпростіший для метаданих)
// • Створювати новий об'єкт при зміні значень
// • Або додати sync.RWMutex якщо потрібні оновлення

type SafeSessionDataItem struct {
    mu sync.RWMutex
    SessionDataItem
}

func (ss *SafeSessionDataItem) String() string {
    ss.mu.RLock()
    defer ss.mu.RUnlock()
    return ss.SessionDataItem.String()
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **багатомовними субтитрами** та **WebSocket-розсилкою**:

### 🎯 Сценарій: додавання аналітики у Master Playlist
```go
func (s *Server) generateMasterPlaylist(channelID string, variants []Variant) *m3u8.Playlist {
    pl := m3u8.NewPlaylist()
    master := true
    pl.Master = &master
    
    // 🎯 Статистика переглядів (оновлюється періодично)
    viewers := s.metrics.GetViewerCount(channelID)
    pl.AppendItem(&m3u8.SessionDataItem{
        DataID: "com.cctv.viewers",
        Value:  pointer(strconv.Itoa(viewers)),
    })
    
    // 🎯 Регіон трансляції (з конфігурації каналу)
    if region := s.channels[channelID].Region; region != "" {
        pl.AppendItem(&m3u8.SessionDataItem{
            DataID: "com.cctv.region",
            Value:  pointer(region),
        })
    }
    
    // 🎯 Локалізовані назви (підтримка AR/EN/UK)
    titles := map[string]string{
        "ar": "بث مباشر",
        "en": "Live Feed", 
        "uk": "Прямий ефір",
    }
    for lang, title := range titles {
        pl.AppendItem(&m3u8.SessionDataItem{
            DataID:   "com.cctv.title",
            Value:    pointer(title),
            Language: pointer(lang),
        })
    }
    
    // 🎯 Посилання на динамічні метадані (попередження, розклад)
    pl.AppendItem(&m3u8.SessionDataItem{
        DataID: "com.cctv.alerts",
        URI:    pointer(fmt.Sprintf("/api/channels/%s/alerts.json", channelID)),
    })
    
    // ... додавання варіантів якості ...
    return pl
}
```

### 🎯 Сценарій: динамічне оновлення сесійних даних через WebSocket
```go
// У WebSocketDistributor при отриманні оновлення метаданих:
func (d *Distributor) onMetadataUpdate(msg MetadataMessage) {
    // 🎯 Оновлення viewer count у реальному часі
    if msg.Type == "viewers" {
        d.masterPlaylists[msg.ChannelID].updateSessionData(
            "com.cctv.viewers", 
            pointer(strconv.Itoa(msg.Value)),
            nil,  // Language не змінюється
        )
        // 📢 Повідомити клієнтів про оновлення Master Playlist
        d.broadcastPlaylistUpdate(msg.ChannelID)
    }
    
    // 🎯 Оновлення локалізованих назв
    if msg.Type == "title" {
        d.masterPlaylists[msg.ChannelID].updateSessionData(
            "com.cctv.title",
            pointer(msg.Value),
            pointer(msg.Language),  // Оновлюємо конкретну мову
        )
    }
}

// Helper для безпечного оновлення:
func (pl *Playlist) updateSessionData(dataID string, value, language *string) {
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
        .filter(i => i.dataId === 'com.cctv.title')
        .find(i => i.language === userLang)?.value 
        ?? sessionData.find(i => i.dataId === 'com.cctv.title')?.value;
    
    document.title = title || 'Live Stream';
    
    // 🎯 Відображення статистики переглядів
    const viewers = sessionData
        .find(i => i.dataId === 'com.cctv.viewers')?.value;
    if (viewers) {
        document.getElementById('viewer-count').textContent = `${viewers} watching`;
    }
    
    // 🎯 Завантаження динамічних попереджень
    const alertsURI = sessionData
        .find(i => i.dataId === 'com.cctv.alerts')?.uri;
    if (alertsURI) {
        fetch(alertsURI)
            .then(r => r.json())
            .then(data => showAlerts(data));
    }
}
```

---

## 🧪 Приклад використання: повний цикл

```go
// ✅ Створення простого запису з VALUE
item1 := &m3u8.SessionDataItem{
    DataID: "com.example.viewer_count",
    Value:  pointer("15420"),
}
fmt.Println(item1.String())
// #EXT-X-SESSION-DATA:DATA-ID="com.example.viewer_count",VALUE="15420"

// ✅ Локалізоване значення з LANGUAGE
item2 := &m3u8.SessionDataItem{
    DataID:   "com.example.title",
    Value:    pointer("Прямий ефір"),
    Language: pointer("uk"),
}
fmt.Println(item2.String())
// #EXT-X-SESSION-DATA:DATA-ID="com.example.title",VALUE="Прямий ефір",LANGUAGE="uk"

// ✅ Зовнішні дані через URI
item3 := &m3u8.SessionDataItem{
    DataID: "com.example.alerts",
    URI:    pointer("/api/alerts/channel_1.json"),
}
fmt.Println(item3.String())
// #EXT-X-SESSION-DATA:DATA-ID="com.example.alerts",URI="/api/alerts/channel_1.json"

// ✅ Парсинг вхідного рядка
line := `DATA-ID="com.example.genre",VALUE="news",LANGUAGE="en"`
item, err := m3u8.NewSessionDataItem(line)
if err != nil {
    log.Fatal(err)
}
fmt.Println(item.DataID)              // "com.example.genre"
fmt.Println(*item.Value)              // "news"
fmt.Println(*item.Language)           // "en"

// ✅ Обробка помилок валідації (після додавання перевірок)
_, err = m3u8.NewSessionDataItem(`VALUE="test"`)  // Без DATA-ID
fmt.Println(err)  // "EXT-X-SESSION-DATA requires DATA-ID attribute"

_, err = m3u8.NewSessionDataItem(`DATA-ID="x",VALUE="a",URI="/b.json"`)  // Обидва
fmt.Println(err)  // "VALUE and URI are mutually exclusive"
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги

```
✅ #EXT-X-SESSION-DATA дозволений ТІЛЬКИ у Master Playlist
✅ DATA-ID — обов'язковий, унікальний в межах плейлиста, формат зворотного DNS
✅ VALUE і URI — взаємовиключні: має бути вказано рівно один
✅ Якщо вказано LANGUAGE, то повинен бути вказаний VALUE
✅ Один DATA-ID може мати кілька записів з різними LANGUAGE для локалізації
✅ Клієнти МАЮТЬ ігнорувати невідомі DATA-ID (forward compatibility)
✅ Клієнти МОЖУТЬ кешувати дані з URI, але мають перевіряти актуальність
✅ Значення у лапках: спеціальні символи (лапки, зворотні слеші) мають бути екрановані
```

---

## 🎯 Висновок

Цей код — **гнучкий інструмент** для розширення функціоналу HLS через метадані:

✅ Підтримка локалізації через `LANGUAGE`  
✅ Можливість вбудованих (`VALUE`) та зовнішніх (`URI`) даних  
✅ Чіткий контракт: парсинг ↔ серіалізація  
✅ Легка інтеграція у Master Playlist генерацію

**Для вашого CCTV-процесора — критичні рекомендації**:

1. ✅ Додати валідацію `DATA-ID` формату (зворотний DNS)
2. ✅ Забезпечити взаємовиключність `VALUE`/`URI` у конструкторі
3. ✅ Екранувати спецсимволи у значеннях перед серіалізацією
4. ✅ Додати валідацію: `LANGUAGE` тільки разом з `VALUE`
5. 🔒 Забезпечити thread-safety при динамічному оновленні метаданих

**Приклад реалізації для багатомовного CCTV**:
```go
// Генерація локалізованих метаданих для Master Playlist:
func addLocalizedSessionData(pl *m3u8.Playlist, dataID, defaultValue string, translations map[string]string) {
    // Універсальне значення (fallback)
    pl.AppendItem(&m3u8.SessionDataItem{
        DataID: dataID,
        Value:  pointer(defaultValue),
    })
    
    // Локалізовані варіанти
    for lang, value := range translations {
        pl.AppendItem(&m3u8.SessionDataItem{
            DataID:   dataID,
            Value:    pointer(value),
            Language: pointer(lang),
        })
    }
}

// Використання:
addLocalizedSessionData(pl, "com.cctv.title", "Live Feed", map[string]string{
    "ar": "بث مباشر",
    "uk": "Прямий ефір", 
    "ru": "Прямой эфир",
})
```

Потрібно допомогти з:
- 🔗 Інтеграцією `SessionDataItem` у динамічне оновлення Master Playlist через WebSocket?
- 🧠 Реалізацією `isValidDataID()` та інших helper-функцій валідації?
- 🧪 Юніт-тестами для крайніх випадків (екранування, взаємовиключність, локалізація)?

Чекаю на ваші питання! 🛠️