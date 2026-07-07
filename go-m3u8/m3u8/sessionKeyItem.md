# 🔍 Глибокий розбір коду: `SessionKeyItem` для HLS DRM/Encryption

Цей код реалізує роботу з тегом **`#EXT-X-SESSION-KEY`** — механізмом для оголошення **ключів шифрування** у HLS Master Playlist, що дозволяє клієнтам заздалегідь підготуватися до декодування зашифрованих медіа-сегментів. Розберемо детально.

---

## 📦 Що таке `#EXT-X-SESSION-KEY` і навіщо він потрібен?

### Контекст: Master Playlist з DRM
```m3u8
#EXTM3U
#EXT-X-VERSION:7

#EXT-X-SESSION-KEY:METHOD=AES-128,URI="https://keys.example.com/key.bin",IV=0x1234567890abcdef
#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="skd://fairplay-key",KEYFORMAT="com.apple.streamingkeydelivery",KEYFORMATVERSIONS="1"

#EXT-X-STREAM-INF:BANDWIDTH=1280000,CODECS="avc1.640028,mp4a.40.2"
video/720p.m3u8
```

### Призначення `#EXT-X-SESSION-KEY`
| Атрибут | Тип | Призначення | Приклад |
|---------|-----|-------------|---------|
| `METHOD` | **string** (обов'язковий) | Алгоритм шифрування | `"AES-128"`, `"SAMPLE-AES"`, `"NONE"` |
| `URI` | `*string` (умовно обов'язковий) | Посилання на ключ або ліцензійний сервер | `"https://keys.com/key.bin"`, `"skd://fairplay"` |
| `IV` | `*string` (опціональний) | Вектор ініціалізації (16 байт hex) | `"0x1234567890abcdef1234567890abcdef"` |
| `KEYFORMAT` | `*string` (опціональний) | Формат ключа (DRM-система) | `"com.apple.streamingkeydelivery"`, `"urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"` (Widevine) |
| `KEYFORMATVERSIONS` | `*string` (опціональний) | Версія формату ключа | `"1"` |

### 🎯 Ключова відмінність: `#EXT-X-SESSION-KEY` vs `#EXT-X-KEY`
```
🔐 #EXT-X-KEY (у Media Playlist):
• Застосовується до конкретних сегментів
• Може змінюватися між сегментами (key rotation)
• Клієнт дізнається про ключ тільки при завантаженні сегмента

🔐 #EXT-X-SESSION-KEY (у Master Playlist):
• Оголошує ключі ЗАВЧАСНО для всієї сесії
• Дозволяє клієнту підготувати DRM-контекст до початку відтворення
• Критично для:
  ✓ Швидкого старту (no key-fetch delay)
  ✓ Попередньої авторизації (license pre-fetch)
  ✓ Multi-DRM сценаріїв (FairPlay + Widevine + PlayReady)

✅ Специфікація: #EXT-X-SESSION-KEY може з'являтися ТІЛЬКИ у Master Playlist
```

### 🎯 Сценарії використання у вашому проекті
```
🔒 Захищений CCTV-стрім (платний доступ):
#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="skd://alarabiya/channel1",KEYFORMAT="com.apple.streamingkeydelivery"
→ iOS-клієнти автоматично ініціалізують FairPlay DRM

🌍 Multi-DRM підтримка:
#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="https://license.example.com/fp",KEYFORMAT="com.apple.streamingkeydelivery"
#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="https://license.example.com/wv",KEYFORMAT="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
→ Клієнт обирає підтримуваний KEYFORMAT автоматично

🔄 Key rotation для live-архіву:
#EXT-X-SESSION-KEY:METHOD=AES-128,URI="https://keys.example.com/key_{period}.bin",IV=0x{timestamp}
→ Клієнт може заздалегідь завантажити ключі для майбутніх періодів
```

---

## 🏗️ Struct `SessionKeyItem` — мінімалізм через композицію

```go
type SessionKeyItem struct {
    Encryptable *Encryptable  // Композиція: всі атрибути шифрування в одному полі
}
```

### 🎯 Чому композиція, а не розгорнута структура?
```go
// ✅ Переваги патерну композиції:
// • Єдине джерело правди для атрибутів шифрування
// • Повторне використання коду: Encryptable використовується також у #EXT-X-KEY
// • Легша підтримка: зміни в атрибутах шифрування — тільки в одному місці

// 📋 Припустима структура Encryptable:
type Encryptable struct {
    Method            string   // "AES-128", "SAMPLE-AES", "NONE"
    URI               *string  // Посилання на ключ/ліцензію
    IV                *string  // Вектор ініціалізації (hex)
    KeyFormat         *string  // DRM-система (reverse-DNS або UUID)
    KeyFormatVersions *string  // Версія формату
}

// 🎯 Методи Encryptable (припустимі):
func (e *Encryptable) String() string {
    // Серіалізація: "METHOD=AES-128,URI=\"...\",IV=0x..."
}
func NewEncryptable(attrs map[string]string) *Encryptable {
    // Парсинг атрибутів → *Encryptable
}
```

### 🎯 Семантика полів у контексті DRM
| Поле | Критичні нюанси |
|------|----------------|
| `Method` | `"NONE"` = відключити шифрування (рідко використовується) |
| `URI` | Для `SAMPLE-AES`: може бути схема `skd://`, `http://`, `https://` |
| `IV` | Має бути 32 hex-символи (16 байт) для AES-128; якщо nil — клієнт генерує з PTS |
| `KeyFormat` | Визначає, який DRM-плагін має обробити ключ (FairPlay/Widevine/PlayReady) |
| `KeyFormatVersions` | Зазвичай `"1"`; майбутні версії можуть змінити формат URI/IV |

---

## 🔧 Конструктор `NewSessionKeyItem` — делегування парсингу

```go
func NewSessionKeyItem(text string) (*SessionKeyItem, error) {
    // Крок 1: Парсинг атрибутів з рядка
    // Вхід: 'METHOD=AES-128,URI="https://keys.com/key.bin",IV=0x1234...'
    // Вихід: map[string]string{"METHOD": "AES-128", "URI": "https://...", "IV": "0x1234..."}
    attributes := ParseAttributes(text)
    
    // Крок 2: Делегування створення Encryptable
    // NewEncryptable відповідає за:
    // • Валідацію METHOD (обов'язковий, допустимі значення)
    // • Парсинг IV (hex-формат, довжина)
    // • Обробку опціональних полів через pointerTo()
    return &SessionKeyItem{
        Encryptable: NewEncryptable(attributes),
    }, nil
}
```

### 🔍 Припустима реалізація `NewEncryptable`
```go
func NewEncryptable(attributes map[string]string) *Encryptable {
    return &Encryptable{
        Method:            attributes[MethodTag],  // Обов'язковий
        URI:               pointerTo(attributes, URITag),
        IV:                pointerTo(attributes, IVTag),
        KeyFormat:         pointerTo(attributes, KeyFormatTag),
        KeyFormatVersions: pointerTo(attributes, KeyFormatVersionsTag),
    }
}

// pointerTo: універсальний helper для опціональних рядків
func pointerTo(attrs map[string]string, key string) *string {
    if v, ok := attrs[key]; ok && v != "" {
        return &v
    }
    return nil
}
```

### ⚠️ Потенційні проблеми конструктора
```go
// ❌ Проблема 1: Method не валідується
// Якщо attributes[MethodTag] == "" → невалідний об'єкт
// → Помилка виявиться тільки при використанні

// ✅ Рішення: валідація у NewEncryptable або на рівні бізнес-логіки
func (e *Encryptable) Validate() error {
    if e.Method == "" {
        return fmt.Errorf("METHOD is required for encryption tag")
    }
    validMethods := map[string]bool{
        "AES-128": true, 
        "SAMPLE-AES": true, 
        "NONE": true,
    }
    if !validMethods[e.Method] {
        return fmt.Errorf("invalid METHOD: %s", e.Method)
    }
    
    // URI обов'язковий, якщо METHOD != "NONE"
    if e.Method != "NONE" && e.URI == nil {
        return fmt.Errorf("URI is required when METHOD is %s", e.Method)
    }
    
    // IV має бути валідним hex, якщо вказаний
    if e.IV != nil && !isValidHex(*e.IV) {
        return fmt.Errorf("invalid IV format (expected hex): %s", *e.IV)
    }
    
    return nil
}
```

---

## 🔄 Метод `String()` — серіалізація через делегування

```go
func (ski *SessionKeyItem) String() string {
    // 🎯 Делегування серіалізації до Encryptable
    // Encryptable.String() повертає: "METHOD=AES-128,URI=\"...\",IV=0x..."
    return fmt.Sprintf("%s:%v", SessionKeyItemTag, ski.Encryptable.String())
}
```

### 🎯 Формат виводу (приклади)
```m3u8
#EXT-X-SESSION-KEY:METHOD=AES-128,URI="https://keys.example.com/key.bin",IV=0x1234567890abcdef1234567890abcdef

#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="skd://fairplay-key",KEYFORMAT="com.apple.streamingkeydelivery",KEYFORMATVERSIONS="1"

#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="https://license.example.com/wv",KEYFORMAT="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
```

### ⚠️ Чому `fmt.Sprintf("%v", ...)` працює для `Encryptable`?
```go
// У Go, якщо тип має метод String() string, він автоматично реалізує інтерфейс fmt.Stringer
// Тому %v викличе ski.Encryptable.String() автоматично

// ✅ Це чистий патерн: кожен компонент відповідає за свою серіалізацію
// ❌ Але: якщо Encryptable.String() поверне порожній рядок → невалідний тег!

// ✅ Рекомендація: додати nil-чек для безпеки
func (ski *SessionKeyItem) String() string {
    if ski.Encryptable == nil {
        return ""  // або повернути помилку, якщо це критично
    }
    return fmt.Sprintf("%s:%v", SessionKeyItemTag, ski.Encryptable.String())
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність валідації обов'язкових полів
```go
// ❌ Поточний код дозволяє створити невалідний об'єкт:
ski := &SessionKeyItem{
    Encryptable: &Encryptable{Method: ""},  // ❌ METHOD відсутній!
}

// ✅ Рішення: валідація у конструкторі
func NewSessionKeyItem(text string) (*SessionKeyItem, error) {
    attributes := ParseAttributes(text)
    
    method := attributes[MethodTag]
    if method == "" {
        return nil, fmt.Errorf("EXT-X-SESSION-KEY requires METHOD attribute")
    }
    
    validMethods := map[string]bool{"AES-128": true, "SAMPLE-AES": true, "NONE": true}
    if !validMethods[method] {
        return nil, fmt.Errorf("invalid METHOD: %s", method)
    }
    
    enc := NewEncryptable(attributes)
    
    // URI обов'язковий, якщо METHOD != "NONE"
    if method != "NONE" && enc.URI == nil {
        return nil, fmt.Errorf("URI is required when METHOD is %s", method)
    }
    
    return &SessionKeyItem{Encryptable: enc}, nil
}
```

### 2️⃣ Валідація формату `IV` (hex, 16 байт)
```go
// ❌ Поточний код приймає будь-який рядок як IV:
// IV="not-hex", IV="0x123" (замало), IV="0x" + 1000 символів (забагато)

// ✅ Валідація hex-формату:
func isValidHex(s string) bool {
    // Видалити опціональний префікс "0x"
    s = strings.TrimPrefix(strings.ToLower(s), "0x")
    
    // Має бути рівно 32 hex-символи (16 байт для AES-128)
    if len(s) != 32 {
        return false
    }
    
    // Перевірити, що всі символи — hex
    for _, r := range s {
        if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
            return false
        }
    }
    return true
}

// Використання:
if e.IV != nil && !isValidHex(*e.IV) {
    return fmt.Errorf("IV must be 32 hex characters (16 bytes), got: %s", *e.IV)
}
```

### 3️⃣ Екранування спецсимволів у `URI`
```go
// ❌ Якщо URI містить лапки або спецсимволи:
// URI="https://keys.com/key?param="value"" → зламе парсер!

// ✅ Специфікація: значення у лапках, тому лапки всередині мають бути екрановані
func escapeAttributeValue(s string) string {
    return strings.ReplaceAll(s, `"`, `\"`)
}

// Використання у Encryptable.String():
if e.URI != nil {
    escaped := escapeAttributeValue(*e.URI)
    slice = append(slice, fmt.Sprintf(quotedFormatString, URITag, escaped))
}
```

### 4️⃣ Підтримка кількох `#EXT-X-SESSION-KEY` з різними `KEYFORMAT`
```go
// ✅ Специфікація дозволяє кілька записів для multi-DRM:
// #EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="fp://...",KEYFORMAT="com.apple.streamingkeydelivery"
// #EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="wv://...",KEYFORMAT="urn:uuid:edef8ba9-..."

// ❌ Але поточний код не забороняє конфліктні записи:
// Два SESSION-KEY з однаковим KEYFORMAT → невизначена поведінка

// ✅ Рекомендація: валідація на рівні Playlist
func (pl *Playlist) ValidateSessionKeys() error {
    keyFormats := make(map[string]bool)
    for _, item := range pl.Items {
        if ski, ok := item.(*SessionKeyItem); ok {
            kf := ski.Encryptable.KeyFormat
            if kf != nil {
                if keyFormats[*kf] {
                    return fmt.Errorf("duplicate KEYFORMAT in EXT-X-SESSION-KEY: %s", *kf)
                }
                keyFormats[*kf] = true
            }
        }
    }
    return nil
}
```

### 5️⃣ Thread-safety при спільному доступі
```go
// ❌ У вашому pipeline (генерация Master Playlist + WebSocket broadcast):
ski := &SessionKeyItem{Encryptable: &Encryptable{Method: "AES-128"}}
pl.AppendItem(ski)  // Горутина 1: запис
s := ski.String()   // Горутина 2: читання → DATA RACE!

// ✅ Рішення: immutable патерн для конфігурації шифрування
// • Створювати новий SessionKeyItem при зміні ключів
// • Або додати sync.RWMutex якщо потрібні динамічні оновлення

type SafeSessionKeyItem struct {
    mu sync.RWMutex
    SessionKeyItem
}

func (ss *SafeSessionKeyItem) String() string {
    ss.mu.RLock()
    defer ss.mu.RUnlock()
    return ss.SessionKeyItem.String()
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **захищеним стрімінгом**:

### 🎯 Сценарій: генерація Master Playlist з Multi-DRM
```go
func generateSecureMasterPlaylist(channelID string, variants []Variant, drmConfig DRMConfig) *m3u8.Playlist {
    pl := m3u8.NewPlaylist()
    master := true
    pl.Master = &master
    pl.Version = pointer(7)
    
    // 🎯 Додавання SESSION-KEY для підтримуваних DRM-систем
    for _, drm := range drmConfig.SupportedDRMs {
        ski := &m3u8.SessionKeyItem{
            Encryptable: &m3u8.Encryptable{
                Method:            "SAMPLE-AES",  // Стандарт для HLS DRM
                URI:               pointer(drm.LicenseURI),
                KeyFormat:         pointer(drm.KeyFormat),  // FairPlay/Widevine/PlayReady ID
                KeyFormatVersions: pointer("1"),
                // IV зазвичай не вказується для SAMPLE-AES (генерується з PTS)
            },
        }
        pl.AppendItem(ski)
    }
    
    // 🎯 Додавання варіантів якості
    for _, v := range variants {
        pl.AppendItem(&m3u8.PlaylistItem{
            Bandwidth:  v.Bandwidth,
            URI:        v.URI,
            Resolution: &m3u8.Resolution{Width: v.Width, Height: v.Height},
            Codecs:     pointer(v.Codecs),
            Audio:      pointer("audio"),
        })
    }
    
    return pl
}

// 📋 Конфігурація DRM (приклад)
type DRMConfig struct {
    SupportedDRMs []DRMSystem
}

type DRMSystem struct {
    KeyFormat   string  // "com.apple.streamingkeydelivery" або UUID Widevine
    LicenseURI  string  // "https://license.example.com/fp" або "skd://..."
}
```

### 🎯 Сценарій: динамічна ротація ключів для live-архіву
```go
// У ключовому менеджері для періодичної зміни ключів:
type KeyRotator struct {
    mu           sync.Mutex
    currentKeyID string
    keyPeriod    time.Duration  // Напр. 1 година
}

func (kr *KeyRotator) GetCurrentSessionKey() *m3u8.SessionKeyItem {
    kr.mu.Lock()
    defer kr.mu.Unlock()
    
    // 🎯 Генерація нового ключа, якщо минув період
    if time.Since(kr.lastRotation) > kr.keyPeriod {
        kr.currentKeyID = generateNewKeyID()
        kr.lastRotation = time.Now()
        // 📢 Тут можна відправити ключ на сервер ліцензій
    }
    
    // 🎯 Формування SESSION-KEY з динамічним URI
    return &m3u8.SessionKeyItem{
        Encryptable: &m3u8.Encryptable{
            Method: "AES-128",
            URI:    pointer(fmt.Sprintf("https://keys.example.com/%s.bin", kr.currentKeyID)),
            IV:     pointer(generateIVFromTimestamp()),  // Детермінований IV з часу
        },
    }
}
```

### 🎯 Сценарій: валідація DRM-конфігурації перед запуском стріму
```go
// У ініціалізації каналу:
func (s *Server) setupSecureChannel(channelID string, config ChannelConfig) error {
    // 🎯 Валідація DRM-налаштувань
    for _, drm := range config.DRM.SupportedDRMs {
        if err := validateDRMSystem(drm); err != nil {
            return fmt.Errorf("invalid DRM config for %s: %w", channelID, err)
        }
    }
    
    // 🎯 Перевірка доступності ліцензійних серверів (health check)
    for _, drm := range config.DRM.SupportedDRMs {
        if err := checkLicenseServer(drm.LicenseURI); err != nil {
            s.logger.Warn("license server unreachable", 
                "channel", channelID, 
                "drm", drm.KeyFormat,
                "error", err)
            // Не блокуємо запуск, але логуємо для моніторингу
        }
    }
    
    return nil
}

func validateDRMSystem(drm DRMSystem) error {
    // ✅ Перевірка формату KeyFormat (reverse-DNS або UUID)
    if !isValidKeyFormat(drm.KeyFormat) {
        return fmt.Errorf("invalid KEYFORMAT: %s", drm.KeyFormat)
    }
    
    // ✅ Перевірка URI схеми
    if !strings.HasPrefix(drm.LicenseURI, "https://") && 
       !strings.HasPrefix(drm.LicenseURI, "skd://") {
        return fmt.Errorf("unsupported URI scheme in %s", drm.LicenseURI)
    }
    
    return nil
}
```

---

## 🧪 Приклад використання: повний цикл

```go
// ✅ Створення SESSION-KEY для AES-128
ski1 := &m3u8.SessionKeyItem{
    Encryptable: &m3u8.Encryptable{
        Method: "AES-128",
        URI:    pointer("https://keys.example.com/channel1.bin"),
        IV:     pointer("0x1234567890abcdef1234567890abcdef"),
    },
}
fmt.Println(ski1.String())
/*
#EXT-X-SESSION-KEY:METHOD=AES-128,URI="https://keys.example.com/channel1.bin",IV=0x1234567890abcdef1234567890abcdef
*/

// ✅ SESSION-KEY для FairPlay DRM
ski2 := &m3u8.SessionKeyItem{
    Encryptable: &m3u8.Encryptable{
        Method:            "SAMPLE-AES",
        URI:               pointer("skd://al-arabiya/channel1"),
        KeyFormat:         pointer("com.apple.streamingkeydelivery"),
        KeyFormatVersions: pointer("1"),
    },
}
fmt.Println(ski2.String())
/*
#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="skd://al-arabiya/channel1",KEYFORMAT="com.apple.streamingkeydelivery",KEYFORMATVERSIONS="1"
*/

// ✅ Парсинг вхідного рядка
line := `METHOD=SAMPLE-AES,URI="https://license.example.com/wv",KEYFORMAT="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"`
ski, err := m3u8.NewSessionKeyItem(line)
if err != nil {
    log.Fatal(err)
}
fmt.Println(ski.Encryptable.Method)           // "SAMPLE-AES"
fmt.Println(*ski.Encryptable.KeyFormat)       // "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"

// ✅ Обробка помилок валідації (після додавання перевірок)
_, err = m3u8.NewSessionKeyItem(`URI="https://keys.com/key.bin"`)  // Без METHOD
fmt.Println(err)  // "EXT-X-SESSION-KEY requires METHOD attribute"

_, err = m3u8.NewSessionKeyItem(`METHOD=AES-128`)  // Без URI
fmt.Println(err)  // "URI is required when METHOD is AES-128"

_, err = m3u8.NewSessionKeyItem(`METHOD=AES-128,URI="k.bin",IV="not-hex"`)
fmt.Println(err)  // "invalid IV format (expected hex): not-hex"
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги

```
✅ #EXT-X-SESSION-KEY дозволений ТІЛЬКИ у Master Playlist
✅ METHOD — обов'язковий, допустимі значення: "AES-128", "SAMPLE-AES", "NONE"
✅ Якщо METHOD != "NONE", URI — обов'язковий
✅ IV — опціональний, але якщо вказаний:
   • Має бути у hex-форматі: "0x" + 32 символи (16 байт)
   • Для AES-128-CBC: клієнт використовує цей IV замість генерованого з PTS
✅ KEYFORMAT — опціональний, але ОБОВ'ЯЗКОВИЙ для SAMPLE-AES:
   • FairPlay: "com.apple.streamingkeydelivery"
   • Widevine: "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
   • PlayReady: "urn:uuid:9a04f079-9840-4286-ab92-e65be0885f95"
✅ KEYFORMATVERSIONS — опціональний, зазвичай "1"
✅ Кілька #EXT-X-SESSION-KEY з різними KEYFORMAT дозволені для multi-DRM
✅ Клієнти МАЮТЬ ігнорувати SESSION-KEY з непідтримуваним KEYFORMAT
✅ SESSION-KEY застосовується до ВСІХ варіантів у Master Playlist (якщо не перевизначено у Media Playlist)
```

---

## 🎯 Висновок

Цей код — **мінімалістичний міст** між вашим Go-сервером та DRM-інфраструктурою:

✅ Чітке розділення відповідальності через композицію (`Encryptable`)  
✅ Гнучка підтримка multi-DRM через `KEYFORMAT`  
✅ Легка інтеграція у Master Playlist генерацію

**Для вашого CCTV-процесора — критичні рекомендації**:

1. ✅ Додати валідацію `METHOD` (обов'язковий, допустимі значення)
2. ✅ Забезпечити обов'язковість `URI` коли `METHOD != "NONE"`
3. ✅ Валідувати формат `IV` (hex, 32 символи)
4. ✅ Екранувати спецсимволи у `URI` перед серіалізацією
5. 🔒 Додати валідацію унікальності `KEYFORMAT` на рівні Playlist

**Приклад конфігурації для Al Arabiya CCTV**:
```go
// Multi-DRM підтримка для глобальної аудиторії:
drmConfig := DRMConfig{
    SupportedDRMs: []DRMSystem{
        {
            KeyFormat:  "com.apple.streamingkeydelivery",  // FairPlay для iOS
            LicenseURI: "skd://al-arabiya/live",
        },
        {
            KeyFormat:  "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed",  // Widevine для Android
            LicenseURI: "https://license.alarabiya.net/widevine",
        },
    },
}
// → Кожен клієнт автоматично обере підтримуваний DRM
```

Потрібно допомогти з:
- 🔗 Реалізацією `Encryptable` (парсинг/серіалізація атрибутів шифрування)?
- 🧠 Інтеграцією з license server для динамічної генерації ключів?
- 🧪 Юніт-тестами для валідації `IV`, `KEYFORMAT`, multi-DRM сценаріїв?

Чекаю на ваші питання! 🛠️🔐