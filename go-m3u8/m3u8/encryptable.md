# Глибоке роз'яснення: `m3u8.Encryptable` — представлення атрибутів шифрування для HLS

Цей файл містить **універсальну структуру для роботи з атрибутами шифрування** у HLS плейлистах. Вона використовується як для `#EXT-X-KEY` (шифрування сегментів), так і для `#EXT-X-SESSION-KEY` (попередня домовленість про ключі сесії). Це критичний компонент для підтримки DRM, захисту контенту та безпечної доставки відео.

---

## 🎯 Навіщо `Encryptable` потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ Encryptable у контексті HLS:           │
│                                         │
│ 🔹 Захист контенту:                    │
│   • Шифрування сегментів через AES-128 │
│   • Інтеграція з DRM-системами         │
│     (FairPlay, Widevine, PlayReady)    │
│   • Контроль доступу до відеоархівів   │
│                                         │
│ 🔹 Гнучкість формату ключів:           │
│   • Підтримка різних KeyFormat:        │
│     "identity", "com.apple.streaming", │
│     "urn:uuid:edef8ba9-79d6-4ace..."   │
│   • Версіонування форматів ключів      │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Захист конфіденційних записів      │
│   • Відповідність вимогам безпеки      │
│     (GDPR, галузеві стандарти)         │
│   • Аудит доступу до зашифрованих      │
│     сегментів                          │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `Encryptable`: універсальне представлення

```go
type Encryptable struct {
    Method            string   // 🎯 Метод шифрування: "AES-128", "SAMPLE-AES", "NONE"
    URI               *string  // 🎯 URL для отримання ключа шифрування
    IV                *string  // 🎯 Вектор ініціалізації (16 байт у hex)
    KeyFormat         *string  // 🎯 Формат ключа: "identity", "com.apple.streaming"...
    KeyFormatVersions *string  // 🎯 Версія формату: "1", "1/2", "1/2/3"...
}
```

### 🎯 Опис полів за специфікацією HLS:

| Поле | Обов'язкове? | Формат | Приклад | Призначення |
|------|-------------|--------|---------|-------------|
| `METHOD` | ✅ Так | Рядок | `"AES-128"` | 🔹 Алгоритм шифрування |
| `URI` | ⚠️ Якщо METHOD ≠ NONE | URL | `"https://keys.example.com/key.bin"` | 🔹 Де отримати ключ |
| `IV` | ❌ Ні | 32 hex-символи (16 байт) | `"0x1234567890abcdef..."` | 🔹 Вектор ініціалізації для AES-CBC |
| `KEY-FORMAT` | ❌ Ні | Рядок | `"com.apple.streaming"` | 🔹 Ідентифікатор системи ключів |
| `KEY-FORMAT-VERSIONS` | ❌ Ні | Список через `/` | `"1/2"` | 🔹 Підтримувані версії формату |

> 💡 **Важливо**: Поля `URI`, `IV`, `KeyFormat`, `KeyFormatVersions` — покажчики, тому що вони **опціональні**. `nil` = атрибут відсутній, не плутати з порожнім рядком.

---

## 🔍 Функція `NewEncryptable`: парсинг атрибутів

```go
func NewEncryptable(attributes map[string]string) *Encryptable {
    return &Encryptable{
        Method:            attributes[MethodTag],           // 🔹 Порожній рядок, якщо ключ відсутній
        URI:               pointerTo(attributes, URITag),   // 🔹 nil, якщо ключ відсутній
        IV:                pointerTo(attributes, IVTag),
        KeyFormat:         pointerTo(attributes, KeyFormatTag),
        KeyFormatVersions: pointerTo(attributes, KeyFormatVersionsTag),
    }
}
```

### 🎯 Приклади використання:

```
Вхідна мапа атрибутів:
{
    "METHOD": "AES-128",
    "URI": "https://keys.example.com/key.bin",
    "IV": "0x1234567890abcdef1234567890abcdef",
    "KEY-FORMAT": "com.apple.streaming",
    "KEY-FORMAT-VERSIONS": "1"
}

Результат:
• Method = "AES-128"
• URI = &"https://keys.example.com/key.bin"
• IV = &"0x1234567890abcdef1234567890abcdef"
• KeyFormat = &"com.apple.streaming"
• KeyFormatVersions = &"1"
```

```
Вхідна мапа (мінімальна):
{
    "METHOD": "NONE"
}

Результат:
• Method = "NONE"
• URI = nil  // 🔹 Не потрібно для METHOD=NONE
• IV = nil
• KeyFormat = nil
• KeyFormatVersions = nil
```

> ⚠️ **Потенційна проблема**: `Method` — не покажчик, тому `METHOD=""` і `METHOD` відсутній обидва дають `Method=""`. Це може ускладнити валідацію.

---

## 🔍 Метод `String()`: серіалізація у формат атрибутів

```go
func (e *Encryptable) String() string {
    var slice []string
    
    // 🔹 METHOD: завжди присутній, без лапок
    slice = append(slice, fmt.Sprintf(formatString, MethodTag, e.Method))
    
    // 🔹 Опціональні поля: тільки якщо не nil
    if e.URI != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, URITag, *e.URI))
    }
    if e.IV != nil {
        slice = append(slice, fmt.Sprintf(formatString, IVTag, *e.IV))
    }
    if e.KeyFormat != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, KeyFormatTag, *e.KeyFormat))
    }
    if e.KeyFormatVersions != nil {
        slice = append(slice, fmt.Sprintf(quotedFormatString, KeyFormatVersionsTag, *e.KeyFormatVersions))
    }
    
    // 🔹 Об'єднати через кому (без префіксу тега!)
    return strings.Join(slice, ",")
}
```

### 🎯 Приклади серіалізації:

```
Вхід:
  Encryptable{
      Method: "AES-128",
      URI: stringPtr("https://keys.example.com/key.bin"),
      IV: nil,
      KeyFormat: stringPtr("identity"),
      KeyFormatVersions: nil,
  }

Вихід:
  "METHOD=AES-128,URI=\"https://keys.example.com/key.bin\",KEY-FORMAT=\"identity\""

Для використання у тегу:
  "#EXT-X-KEY:" + encryptable.String()
  → "#EXT-X-KEY:METHOD=AES-128,URI=\"https://keys.example.com/key.bin\",KEY-FORMAT=\"identity\""
```

### 🔹 Чому різні формати для різних полів?

```
• formatString = `%s=%v` → для чисел, ідентифікаторів: METHOD=AES-128, IV=0x1234...
• quotedFormatString = `%s="%v"` → для рядків з можливістю спецсимволів: URI="...", KEY-FORMAT="..."

Це відповідає специфікації:
✅ URI, KEY-FORMAT, KEY-FORMAT-VERSIONS завжди у лапках
✅ METHOD, IV можуть бути без лапок (але лапки не заборонені)
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Додавання шифрування до сегментів

```go
// У VideoManifestProxy — генерація #EXT-X-KEY перед зашифрованими сегментами:
func addEncryptionKey(playlist *HLSPlaylist, keyURL string, iv string, keyFormat string) {
    attrs := map[string]string{
        "METHOD": "AES-128",
        "URI": keyURL,
        "IV": iv,
    }
    if keyFormat != "" {
        attrs["KEY-FORMAT"] = keyFormat
    }
    
    encryptable := NewEncryptable(attrs)
    playlist.AddTag(fmt.Sprintf("#EXT-X-KEY:%s", encryptable.String()))
}

// Використання:
addEncryptionKey(playlist, 
    "https://keys.cctv.local/key.bin", 
    "0x1234567890abcdef1234567890abcdef",
    "identity")
```

### ✅ 2: Підтримка FairPlay DRM для Apple пристроїв

```go
// Для iOS/Safari — використання FairPlay Streaming:
func addFairPlayKey(playlist *HLSPlaylist, fpsURL, fpsCert string) {
    encryptable := &Encryptable{
        Method:            "SAMPLE-AES",  // 🔹 FairPlay вимагає SAMPLE-AES
        URI:               &fpsURL,
        KeyFormat:         stringPtr("com.apple.streaming"),  // 🔹 Ідентифікатор FairPlay
        KeyFormatVersions: stringPtr("1"),
        // 🔹 IV генерується динамічно для кожного сегмента
    }
    
    playlist.AddTag(fmt.Sprintf("#EXT-X-SESSION-KEY:%s", encryptable.String()))
}

// Примітка: #EXT-X-SESSION-KEY використовується для попередньої домовленості,
// а #EXT-X-KEY — для безпосереднього шифрування сегментів
```

### ✅ 3: Валідація атрибутів шифрування

```go
// Перевірити, що конфігурація шифрування валідна:
func validateEncryptable(e *Encryptable) error {
    // 🔹 METHOD обов'язковий
    if e.Method == "" {
        return fmt.Errorf("METHOD attribute is required")
    }
    
    // 🔹 Якщо не NONE — потрібен URI
    if e.Method != "NONE" && e.Method != "SAMPLE-AES" {
        if e.URI == nil || *e.URI == "" {
            return fmt.Errorf("URI required for METHOD=%s", e.Method)
        }
        // 🔹 Перевірити формат URL
        if _, err := url.Parse(*e.URI); err != nil {
            return fmt.Errorf("invalid URI format: %w", err)
        }
    }
    
    // 🔹 IV: 32 hex-символи (16 байт) + опціональний префікс "0x"
    if e.IV != nil {
        iv := strings.TrimPrefix(*e.IV, "0x")
        if len(iv) != 32 {
            return fmt.Errorf("IV must be 16 bytes (32 hex chars), got %d", len(iv))
        }
        if _, err := hex.DecodeString(iv); err != nil {
            return fmt.Errorf("invalid hex in IV: %w", err)
        }
    }
    
    // 🔹 KEY-FORMAT-VERSIONS: список через /
    if e.KeyFormatVersions != nil {
        parts := strings.Split(*e.KeyFormatVersions, "/")
        for _, p := range parts {
            if _, err := strconv.Atoi(p); err != nil {
                return fmt.Errorf("invalid version in KEY-FORMAT-VERSIONS: %s", p)
            }
        }
    }
    
    return nil
}
```

### ✅ 4: Моніторинг використання шифрування

```go
// monitoring.Monitor — метрики для шифрування:
type EncryptionMetrics struct {
    EncryptedSegments *prometheus.CounterVec  // кількість зашифрованих сегментів
    EncryptionMethods *prometheus.CounterVec  // розподіл за METHOD
    KeyFormats        *prometheus.CounterVec  // розподіл за KEY-FORMAT
    KeyFetchErrors    *prometheus.CounterVec  // помилки отримання ключів
}

// У процесі додавання ключа:
func monitorEncryption(channelID string, e *Encryptable, metrics *EncryptionMetrics) {
    if e.Method != "NONE" {
        metrics.EncryptedSegments.WithLabelValues(channelID).Inc()
        metrics.EncryptionMethods.WithLabelValues(channelID, e.Method).Inc()
        
        if e.KeyFormat != nil {
            metrics.KeyFormats.WithLabelValues(channelID, *e.KeyFormat).Inc()
        }
    }
}
```

### ✅ 5: Ротація ключів шифрування

```go
// Для підвищення безпеки — періодична зміна ключів:
type KeyRotation struct {
    KeyURL        string
    IV            string
    KeyFormat     string
    SegmentInterval int  // 🔹 Змінювати ключ кожні N сегментів
}

func addRotatedKeys(playlist *HLSPlaylist, rotation KeyRotation, totalSegments int) {
    for i := 0; i < totalSegments; i++ {
        // 🔹 Новий ключ кожні SegmentInterval сегментів
        if i%rotation.SegmentInterval == 0 {
            // 🔹 Генерувати новий IV (на основі номера сегмента або випадково)
            iv := generateIV(i)
            
            encryptable := &Encryptable{
                Method:    "AES-128",
                URI:       &rotation.KeyURL,
                IV:        &iv,
                KeyFormat: &rotation.KeyFormat,
            }
            playlist.AddTag(fmt.Sprintf("#EXT-X-KEY:%s", encryptable.String()))
        }
        // 🔹 Додати сегмент...
    }
}

func generateIV(segmentIndex int) string {
    // 🔹 Простий приклад: IV на основі індексу сегмента
    // У production: використовувати криптографічно безпечний RNG
    data := fmt.Sprintf("%d", segmentIndex)
    hash := sha256.Sum256([]byte(data))
    return "0x" + hex.EncodeToString(hash[:16])  // 🔹 16 байт = 32 hex-символи
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на парсинг атрибутів

```go
func TestNewEncryptable_Basic(t *testing.T) {
    attrs := map[string]string{
        "METHOD": "AES-128",
        "URI": "https://keys.example.com/key.bin",
        "IV": "0x1234567890abcdef1234567890abcdef",
        "KEY-FORMAT": "identity",
        "KEY-FORMAT-VERSIONS": "1",
    }
    
    e := NewEncryptable(attrs)
    
    assert.Equal(t, "AES-128", e.Method)
    assert.NotNil(t, e.URI)
    assert.Equal(t, "https://keys.example.com/key.bin", *e.URI)
    assert.NotNil(t, e.IV)
    assert.Equal(t, "0x1234567890abcdef1234567890abcdef", *e.IV)
    assert.NotNil(t, e.KeyFormat)
    assert.Equal(t, "identity", *e.KeyFormat)
    assert.NotNil(t, e.KeyFormatVersions)
    assert.Equal(t, "1", *e.KeyFormatVersions)
}
```

### 🔹 Тест на опціональні поля

```go
func TestNewEncryptable_OptionalFields(t *testing.T) {
    // 🔹 Тільки обов'язковий METHOD
    attrs := map[string]string{"METHOD": "NONE"}
    e := NewEncryptable(attrs)
    
    assert.Equal(t, "NONE", e.Method)
    assert.Nil(t, e.URI)   // 🔹 nil, не порожній рядок
    assert.Nil(t, e.IV)
    assert.Nil(t, e.KeyFormat)
    assert.Nil(t, e.KeyFormatVersions)
}
```

### 🔹 Тест на серіалізацію

```go
func TestEncryptable_String(t *testing.T) {
    e := &Encryptable{
        Method:    "AES-128",
        URI:       stringPtr("https://keys.example.com/key.bin"),
        IV:        stringPtr("0x1234"),
        KeyFormat: stringPtr("identity"),
    }
    
    result := e.String()
    
    // 🔹 Перевірити порядок та формат
    assert.Contains(t, result, "METHOD=AES-128")           // 🔹 Без лапок
    assert.Contains(t, result, `URI="https://keys.example.com/key.bin"`)  // 🔹 З лапками
    assert.Contains(t, result, "IV=0x1234")                // 🔹 Без лапок (hex)
    assert.Contains(t, result, `KEY-FORMAT="identity"`)    // 🔹 З лапками
    
    // 🔹 Перевірити роздільник
    assert.Contains(t, result, ",")
    
    // 🔹 Перевірити відсутність непотрібних полів
    assert.NotContains(t, result, "KEY-FORMAT-VERSIONS")  // 🔹 nil → не включається
}

func stringPtr(s string) *string { return &s }
```

### 🔹 Тест на валідацію

```go
func TestValidateEncryptable(t *testing.T) {
    // 🔹 Валідна конфігурація
    valid := &Encryptable{
        Method: "AES-128",
        URI: stringPtr("https://keys.example.com/key.bin"),
        IV: stringPtr("0x1234567890abcdef1234567890abcdef"),
    }
    assert.NoError(t, validateEncryptable(valid))
    
    // 🔹 Відсутній METHOD
    invalid1 := &Encryptable{}
    assert.Error(t, validateEncryptable(invalid1))
    
    // 🔹 METHOD ≠ NONE, але немає URI
    invalid2 := &Encryptable{Method: "AES-128"}
    assert.Error(t, validateEncryptable(invalid2))
    
    // 🔹 Невалідний IV (не парне число hex-символів)
    invalid3 := &Encryptable{
        Method: "AES-128",
        URI: stringPtr("https://keys.example.com/key.bin"),
        IV: stringPtr("0x123"),  // 🔹 3 символи замість 32
    }
    assert.Error(t, validateEncryptable(invalid3))
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `Method` не покажчик | Не можна розрізнити "" і відсутність | 🔹 Змінити на `*string` або додати валідацію `if e.Method == ""` |
| Неправильний формат `IV` | Помилки декодування у плеєрі | 🔹 Додати валідацію: 32 hex-символи, опціональний "0x" префікс |
| Відсутні лапки для `URI` | Помилки парсингу у плеєрі | 🔹 Завжди використовувати `quotedFormatString` для `URI` ✅ |
| `KEY-FORMAT-VERSIONS` без валідації | "abc/def" приймається як валідне | 🔹 Додати перевірку: кожна версія — ціле число |
| `nil` покажчики у `String()` | Паніка при `*e.URI` якщо `URI=nil` | 🔹 Перевірка `if e.URI != nil` перед доступом ✅ |

### Приклад покращеної валідації `IV`:

```go
func validateIV(iv *string) error {
    if iv == nil {
        return nil  // ✅ Опціональне поле
    }
    
    // 🔹 Видалити опціональний префікс "0x"
    value := strings.TrimPrefix(*iv, "0x")
    
    // 🔹 Перевірити довжину: 16 байт = 32 hex-символи
    if len(value) != 32 {
        return fmt.Errorf("IV must be 16 bytes (32 hex chars), got %d chars", len(value))
    }
    
    // 🔹 Перевірити, що це валідний hex
    if _, err := hex.DecodeString(value); err != nil {
        return fmt.Errorf("invalid hex in IV: %w", err)
    }
    
    return nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Створення базового ключа шифрування:
func makeAES128Key(keyURL, iv string) *Encryptable {
    return &Encryptable{
        Method: "AES-128",
        URI:    &keyURL,
        IV:     &iv,
    }
}

// 2: Створення ключа для DRM:
func makeDRMKey(method, uri, keyFormat, versions string) *Encryptable {
    return &Encryptable{
        Method:            method,
        URI:               &uri,
        KeyFormat:         &keyFormat,
        KeyFormatVersions: &versions,
    }
}

// 3: Форматування повного тега:
func formatKeyTag(tagType string, e *Encryptable) string {
    return fmt.Sprintf("#%s:%s", tagType, e.String())
}

// Використання:
// • formatKeyTag("EXT-X-KEY", encryptable) → "#EXT-X-KEY:METHOD=..."
// • formatKeyTag("EXT-X-SESSION-KEY", encryptable) → "#EXT-X-SESSION-KEY:..."

// 4: Перевірка, чи потрібне шифрування:
func needsEncryption(e *Encryptable) bool {
    return e != nil && e.Method != "" && e.Method != "NONE"
}

// 5: Логування для відладки:
func logEncryptionConfig(e *Encryptable) {
    log.Debugf("Encryption: METHOD=%s, URI=%s, IV=%s, KeyFormat=%s",
        e.Method,
        deref(e.URI),
        deref(e.IV),
        deref(e.KeyFormat))
}

func deref(s *string) string { if s != nil { return *s }; return "<nil>" }
```

---

## 📊 Матриця підтримуваних методів шифрування

```
METHOD         | KEY-FORMAT           | Призначення                     | Сумісність
───────────────┼──────────────────────┼─────────────────────────────────┼────────────
AES-128        | identity (дефолт)    | 🔹 Стандартне шифрування сегментів | ✅ Всі плеєри
AES-128        | com.apple.streaming  | 🔹 FairPlay DRM для Apple         | ✅ iOS, Safari
SAMPLE-AES     | com.apple.streaming  | 🔹 FairPlay з семпл-шифруванням   | ✅ iOS 4+, Safari
AES-128        | urn:uuid:edef8ba9... | 🔹 Widevine DRM                   | ✅ Android, Chrome
NONE           | (будь-який)          | 🔹 Відключити шифрування          | ✅ Всі плеєри
```

---

## 📚 Корисні посилання

- [HLS RFC Draft: EXT-X-KEY](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.2.4)
- [HLS RFC Draft: EXT-X-SESSION-KEY](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.2.5)
- [FairPlay Streaming Server SDK](https://developer.apple.com/documentation/fairplay_streaming)
- [AES-128 encryption in HLS](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)

> 💡 **Ключова ідея**: `Encryptable` — це "універсальний ключ" для шифрування у вашому HLS пайплайні. Він:
> - 🎯 Забезпечує єдиний інтерфейс для `#EXT-X-KEY` та `#EXT-X-SESSION-KEY`
> - 🔧 Підтримує різні методи шифрування та DRM-системи через `KeyFormat`
> - ⚡ Граційно обробляє опціональні поля через покажчики (`nil` = відсутній)
> - 🛡️ Готовий до розширення: нові `METHOD`/`KEY-FORMAT` не ламають існуючий код

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку нових DRM-систем (PlayReady, ClearKey) через розширення мапи `KeyFormat`
- 🧪 Написати integration-тест для перевірки коректності шифрування у реальному плеєрі
- 📈 Додати Prometheus-метрики для моніторингу використання шифрування та помилок отримання ключів

🛠️