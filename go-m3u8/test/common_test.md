# 🔍 Глибокий розбір тесту: `ParseAttributes` для HLS атрибутів

Цей файл містить **юніт-тест** для функції `ParseAttributes`, яка відповідає за парсинг рядків атрибутів у форматі HLS (наприклад, `KEY="value",NUM=123`). Розберемо архітектурно та детально.

---

## 📦 Контекст: навіщо потрібен `ParseAttributes`?

### Контекст: атрибути у HLS-тегах
```m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1280000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1920x1080
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="English",LANGUAGE="en",DEFAULT=YES
#EXT-X-KEY:METHOD=AES-128,URI="https://keys.com/key.bin",IV=0x1234...
```

### Призначення `ParseAttributes`
| Вхід | Вихід |
|------|--------|
| `BANDWIDTH=1280000,CODECS="avc1.640028"` | `map[string]string{"BANDWIDTH": "1280000", "CODECS": "avc1.640028"}` |
| `TYPE=AUDIO,NAME="Test, with comma"` | `map[string]string{"TYPE": "AUDIO", "NAME": "Test, with comma"}` |
| `URI="http://example.com/path?x=1&y=2"` | `map[string]string{"URI": "http://example.com/path?x=1&y=2"}` |

### 🎯 Чому це критично?
```
✅ Універсальний парсер: один код для всіх тегів (#EXT-X-STREAM-INF, #EXT-X-MEDIA, тощо)
✅ Обробка складних випадків: лапки, коми всередині значень, спецсимволи в URI
✅ Type-safe подальша обробка: після парсингу → валідація типу значення (int, bool, string)
```

---

## 🔬 Детальний розбір тесту `TestParseAttributes`

```go
func TestParseAttributes(t *testing.T) {
    // 🎯 Вхідний рядок з різними форматами значень
    line := "TEST-ID=\"Help\",URI=\"http://test\",ID=33\n"
    
    // 🎯 Виклик парсера
    mapAttr := m3u8.ParseAttributes(line)

    // 🎯 Крок 1: Перевірка, що результат не nil
    assert.NotNil(t, mapAttr)
    
    // 🎯 Крок 2: Перевірка розпаршених ключ-значень
    // ✅ Ключ з дефісом + значення в лапках
    assert.Equal(t, "Help", mapAttr["TEST-ID"])
    
    // ✅ URI-подібне значення в лапках
    assert.Equal(t, "http://test", mapAttr["URI"])
    
    // ✅ Числове значення БЕЗ лапок → повертається як string "33"
    assert.Equal(t, "33", mapAttr["ID"])
}
```

### 🎯 Що тестує цей кейс?
| Аспект | Вхід у тесті | Чому це важливо |
|--------|-------------|----------------|
| **Ключ з дефісом** | `TEST-ID` | HLS атрибути часто мають дефіси: `GROUP-ID`, `KEYFORMAT`, `TIME-OFFSET` |
| **Значення в лапках** | `"Help"`, `"http://test"` | Рядкові значення у специфікації завжди в лапках |
| **Значення без лапок** | `33` | Числові/булеві значення (`BANDWIDTH=1280000`, `DEFAULT=YES`) |
| **Роздільник атрибутів** | `,` | Атрибути розділені комами без пробілів |
| **Завершення рядка** | `\n` у кінці | Парсер має ігнорувати/обробляти переноси рядків |
| **Повернення map[string]string** | `mapAttr["KEY"]` → `string` | Усі значення парсяться як рядки; типізація — на наступному етапі |

---

## ⚠️ Критичний аналіз: що покрито, а що — ні

### ✅ Покриті сценарії
```
✅ Змішані формати значень: "строка в лапках" + число без лапок
✅ Ключі з дефісами: TEST-ID → коректна обробка
✅ Наявність \n у кінці → парсер не ламається
✅ Повернення map (не nil) при валідному вводі
```

### ❌ Непокриті критичні сценарії

#### 1️⃣ Значення з комою всередині лапок
```go
// ❌ Не тестується:
line := `NAME="Title, with comma",ID=123`
// ✅ Очікуваний результат: map["NAME"] = "Title, with comma" (кома НЕ розділяє атрибути)
// ❌ Якщо парсер просто сплітить по "," → помилка: ["NAME=\"Title", " with comma\"", "ID=123"]
```

#### 2️⃣ Екрановані лапки всередині значення
```go
// ❌ Не тестується:
line := `NAME="Title with \"quotes\"",ID=1`
// ✅ Очікуваний результат: map["NAME"] = `Title with "quotes"`
// ❌ Наївний парсер зламається на першій зустрічній \"
```

#### 3️⃣ Порожні ключі або значення
```go
// ❌ Не тестується:
cases := []string{
    `=value`,           // Порожній ключ
    `KEY=`,             // Порожнє значення
    `KEY=""`,           // Порожнє значення в лапках
    `,KEY=value`,       // Зайва кома на початку
    `KEY=value,`,       // Зайва кома в кінці
}
```

#### 4️⃣ Пробіли навколо `=` та `,`
```go
// ❌ Не тестується:
line := `KEY = "value" , ID = 123`  // Пробіли навколо = та ,
// ✅ Специфікація не забороняє пробіли, парсер має бути толерантним
```

#### 5️⃣ Дублікати ключів
```go
// ❌ Не тестується:
line := `KEY=first,KEY=second`
// ✅ Що має повернути парсер? Останнє значення? Помилку? Перше?
```

#### 6️⃣ Невалідні формати
```go
// ❌ Не тестується:
cases := []struct{
    name  string
    input string
}{
    {"unclosed_quote", `KEY="value`},
    {"missing_value", `KEY=`},
    {"just_comma", `,`},
    {"empty_string", ``},
    {"no_equals", `KEYVALUE`},
}
```

---

## 🛠️ Припустима реалізація `ParseAttributes` (для контексту)

```go
// 🎯 Як МОЖЕ виглядати парсер (спрощено):
func ParseAttributes(text string) map[string]string {
    result := make(map[string]string)
    
    // 🎯 Видалення зайвих символів
    text = strings.TrimSpace(text)
    text = strings.TrimSuffix(text, "\n")
    
    // 🎯 Розбиття на атрибути (НАЙСКЛАДНІША ЧАСТИНА)
    // ❌ Наївний підхід (зламається на комах у лапках):
    // parts := strings.Split(text, ",")
    
    // ✅ Правильний підхід: парсинг з урахуванням лапок
    var key, value string
    var inQuotes bool
    var current strings.Builder
    
    for _, r := range text {
        switch {
        case r == '"' && !inQuotes:
            inQuotes = true
        case r == '"' && inQuotes:
            inQuotes = false
        case r == ',' && !inQuotes:
            // Кінець атрибута → парсинг key=value
            if parts := strings.SplitN(current.String(), "=", 2); len(parts) == 2 {
                result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
            }
            current.Reset()
        default:
            current.WriteRune(r)
        }
    }
    // 🎯 Обробка останнього атрибута
    if current.Len() > 0 {
        if parts := strings.SplitN(current.String(), "=", 2); len(parts) == 2 {
            result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
        }
    }
    
    return result
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **парсингом HLS-тегів**:

### 🎯 Сценарій: парсинг `#EXT-X-STREAM-INF` у Master Playlist
```go
// У generateMasterPlaylist при обробці вхідного плейлиста:
func parseStreamInf(line string) (*m3u8.PlaylistItem, error) {
    // 🎯 Видалення префікса тегу
    attrsLine := strings.TrimPrefix(line, m3u8.PlaylistItemTag + ":")
    
    // 🎯 Парсинг атрибутів через універсальну функцію
    attrs := m3u8.ParseAttributes(attrsLine)
    
    // 🎯 Валідація обов'язкових полів
    if attrs[m3u8.BandwidthTag] == "" {
        return nil, fmt.Errorf("BANDWIDTH is required in EXT-X-STREAM-INF")
    }
    
    // 🎯 Типізація значень
    bandwidth, err := strconv.Atoi(attrs[m3u8.BandwidthTag])
    if err != nil {
        return nil, fmt.Errorf("invalid BANDWIDTH: %w", err)
    }
    
    // 🎯 Побудова об'єкта
    return &m3u8.PlaylistItem{
        Bandwidth:  bandwidth,
        Codecs:     pointerTo(attrs, m3u8.CodecsTag),  // helper: map → *string
        Resolution: parseResolution(attrs),            // "1920x1080" → *Resolution
        // ... інші поля ...
    }, nil
}
```

### 🎯 Сценарій: парсинг `#EXT-X-MEDIA` для багатомовних доріжок
```go
// У WebSocketDistributor при отриманні оновлень плейлиста:
func parseMediaTrack(line string) (*m3u8.MediaItem, error) {
    attrsLine := strings.TrimPrefix(line, m3u8.MediaItemTag + ":")
    attrs := m3u8.ParseAttributes(attrsLine)
    
    // 🎯 Валідація через хелпер
    if err := validateMediaAttributes(attrs); err != nil {
        return nil, err
    }
    
    return &m3u8.MediaItem{
        Type:       attrs[m3u8.TypeTag],
        GroupID:    attrs[m3u8.GroupIDTag],
        Name:       attrs[m3u8.NameTag],
        Language:   pointerTo(attrs, m3u8.LanguageTag),
        Default:    parseYesNo(attrs, m3u8.DefaultTag),  // "YES"→true, "NO"→false
        AutoSelect: parseYesNo(attrs, m3u8.AutoSelectTag),
        URI:        pointerTo(attrs, m3u8.URITag),
    }, nil
}

// 🎯 Helper для безпечного отримання *string з map
func pointerTo(attrs map[string]string, key string) *string {
    if v, ok := attrs[key]; ok && v != "" {
        return &v
    }
    return nil
}
```

### 🎯 Сценарій: тестування парсера на реальних даних Al Arabiya
```go
// ✅ Інтеграційний тест з реальними атрибутами з продакшену:
func TestParseAttributes_AlArabiya_RealCases(t *testing.T) {
    cases := []struct{
        name     string
        input    string
        expected map[string]string
    }{
        {
            name: "StreamInf with codecs and resolution",
            input: `BANDWIDTH=2560000,CODECS="avc1.640028,mp4a.40.2",RESOLUTION=1920x1080,FRAME-RATE=30.0,AUDIO="audio",SUBTITLES="subs"`,
            expected: map[string]string{
                "BANDWIDTH": "2560000",
                "CODECS":    "avc1.640028,mp4a.40.2",  // ✅ Кома всередині лапок!
                "RESOLUTION": "1920x1080",
                "FRAME-RATE": "30.0",
                "AUDIO":     "audio",
                "SUBTITLES": "subs",
            },
        },
        {
            name: "MediaItem with Arabic name",
            input: `TYPE=AUDIO,GROUP-ID="audio",NAME="العربية",LANGUAGE="ar",DEFAULT=YES`,
            expected: map[string]string{
                "TYPE": "AUDIO",
                "GROUP-ID": "audio",
                "NAME": "العربية",  // ✅ Юнікод у лапках
                "LANGUAGE": "ar",
                "DEFAULT": "YES",
            },
        },
        {
            name: "Key with HTTPS URI containing query params",
            input: `METHOD=SAMPLE-AES,URI="https://license.alarabiya.net/key?ch=1&exp=1234567890",KEYFORMAT="com.apple.streamingkeydelivery"`,
            expected: map[string]string{
                "METHOD": "SAMPLE-AES",
                "URI": "https://license.alarabiya.net/key?ch=1&exp=1234567890",  // ✅ & не розділяє атрибути!
                "KEYFORMAT": "com.apple.streamingkeydelivery",
            },
        },
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            result := m3u8.ParseAttributes(tc.input)
            assert.Equal(t, tc.expected, result, "parsed attributes mismatch")
        })
    }
}
```

---

## 🧪 Приклад: розширений набір тестів для `ParseAttributes`

```go
// ✅ Повний набір тестів з покриттям крайніх випадків:
func TestParseAttributes(t *testing.T) {
    t.Parallel()
    
    t.Run("Basic/MixedFormats", func(t *testing.T) {
        line := "TEST-ID=\"Help\",URI=\"http://test\",ID=33\n"
        result := m3u8.ParseAttributes(line)
        
        assert.NotNil(t, result)
        assert.Equal(t, "Help", result["TEST-ID"])
        assert.Equal(t, "http://test", result["URI"])
        assert.Equal(t, "33", result["ID"])
    })
    
    t.Run("ValueWithCommaInsideQuotes", func(t *testing.T) {
        line := `NAME="Title, with comma",ID=123`
        result := m3u8.ParseAttributes(line)
        
        assert.Equal(t, "Title, with comma", result["NAME"])  // ✅ Кома не розділяє!
        assert.Equal(t, "123", result["ID"])
    })
    
    t.Run("ValueWithEscapedQuotes", func(t *testing.T) {
        line := `NAME="Title with \"quotes\"",ID=1`
        result := m3u8.ParseAttributes(line)
        
        // Залежить від реалізації: чи розпарсить екрановані лапки?
        // Очікуємо: "Title with \"quotes\"" або "Title with "quotes""
        assert.Contains(t, result["NAME"], "quotes")
    })
    
    t.Run("URIWithQueryParams", func(t *testing.T) {
        line := `URI="https://example.com/path?x=1&y=2",BANDWIDTH=1000`
        result := m3u8.ParseAttributes(line)
        
        assert.Equal(t, "https://example.com/path?x=1&y=2", result["URI"])  // ✅ & не ламає парсинг
        assert.Equal(t, "1000", result["BANDWIDTH"])
    })
    
    t.Run("WhitespaceTolerance", func(t *testing.T) {
        line := `KEY = "value" , ID = 123`  // Пробіли навколо = та ,
        result := m3u8.ParseAttributes(line)
        
        // Парсер має бути толерантним до пробілів
        assert.Equal(t, "value", strings.TrimSpace(result["KEY"]))
        assert.Equal(t, "123", strings.TrimSpace(result["ID"]))
    })
    
    t.Run("EmptyValues", func(t *testing.T) {
        cases := []struct{
            input    string
            expected map[string]string
        }{
            {`KEY=`, map[string]string{"KEY": ""}},
            {`KEY=""`, map[string]string{"KEY": ""}},
            {`A=1,,B=2`, map[string]string{"A": "1", "B": "2"}},  // Зайва кома
        }
        
        for _, tc := range cases {
            t.Run(tc.input, func(t *testing.T) {
                result := m3u8.ParseAttributes(tc.input)
                assert.Equal(t, tc.expected, result)
            })
        }
    })
    
    t.Run("InvalidFormats", func(t *testing.T) {
        // 🎯 Парсер має бути стійким до невалідного вводу
        // (не панікувати, повертати частковий результат або порожній map)
        cases := []string{
            ``,                    // Порожній рядок
            `KEY`,                 // Без =
            `="value"`,            // Без ключа
            `KEY="unclosed`,       // Незакрита лапка
        }
        
        for _, input := range cases {
            t.Run(input, func(t *testing.T) {
                result := m3u8.ParseAttributes(input)
                // 🎯 Очікуємо: не nil, але можливо порожній або частковий map
                assert.NotNil(t, result)
                // Детальні перевірки залежать від специфікації парсера
            })
        }
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — вимоги до парсингу атрибутів

```
✅ Формат атрибута: KEY=VALUE або KEY="VALUE"
✅ Роздільник атрибутів: кома (`,`) без обов'язкових пробілів
✅ Значення в лапках:
   • Якщо значення містить кому, пробіл, або спецсимвол — ОБОВ'ЯЗКОВО в лапках
   • Лапки всередині значення мають бути екрановані: `\"`
✅ Ключі: чутливі до регістру, можуть містити дефіс (`KEY-FORMAT`)
✅ Значення без лапок:
   • Тільки для чисел (`BANDWIDTH=1280000`) та ENUM (`METHOD=AES-128`)
   • Не можуть містити пробіли, коми, лапки
✅ Порядок атрибутів: не регламентований, плеєри мають бути толерантні
✅ Невідомі атрибути: клієнти МАЮТЬ ігнорувати (forward compatibility)
```

---

## 🎯 Висновок

Цей тест — **мінімальна, але важлива перевірка** базового функціоналу парсера:

✅ Покриття змішаних форматів значень (лапки/без лапок)  
✅ Перевірка стійкості до `\n` у кінці рядка  
✅ Валідація повернення map (не nil)

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати тести на **коми всередині лапок** (найчастіша помилка парсерів)
2. ✅ Перевірити обробку **екранованих лапок** `\"` у значеннях
3. ✅ Додати тести на **URI з query-параметрами** (`?x=1&y=2`)
4. ✅ Перевірити толерантність до **пробілів** навколо `=` та `,`
5. ✅ Додати тести на **невалідний ввід** (парсер не має панікувати)

**Приклад інтеграції у ваш pipeline**:
```go
// У парсері HLS-плейлистів:
func parseTagLine(line string) (Item, error) {
    // 🎯 Визначення типу тегу за префіксом
    switch {
    case strings.HasPrefix(line, m3u8.PlaylistItemTag):
        attrs := m3u8.ParseAttributes(strings.TrimPrefix(line, m3u8.PlaylistItemTag+":"))
        return m3u8.NewPlaylistItemFromMap(attrs)  // Типізація та валідація
        
    case strings.HasPrefix(line, m3u8.MediaItemTag):
        attrs := m3u8.ParseAttributes(strings.TrimPrefix(line, m3u8.MediaItemTag+":"))
        return m3u8.NewMediaItemFromMap(attrs)
        
    // ... інші теги ...
    default:
        return nil, fmt.Errorf("unknown tag: %s", line)
    }
}
```

Потрібно допомогти з:
- 🧠 Реалізацією стійкого `ParseAttributes` з підтримкою екранування?
- 🔗 Написанням інтеграційних тестів з реальними даними Al Arabiya?
- 🧪 Бенчмарками для оцінки продуктивності парсера на великих плейлистах?

Чекаю на ваші питання! 🛠️🧪