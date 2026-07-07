# 🔍 Глибокий розбір тесту: `SessionKeyItem` для HLS `#EXT-X-SESSION-KEY`

Цей файл містить **юніт-тест** для парсингу та серіалізації тега `#EXT-X-SESSION-KEY` — механізму оголошення ключів шифрування у **Master Playlist** HLS. Розберемо архітектурно та детально.

---

## 📦 Що таке `#EXT-X-SESSION-KEY` і навіщо він потрібен?

### Контекст: шифрування у Master Playlist
```m3u8
#EXTM3U
#EXT-X-VERSION:7

#EXT-X-SESSION-KEY:METHOD=AES-128,URI="https://keys.example.com/key.bin",IV=0xD512BBF
#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="skd://fairplay",KEYFORMAT="com.apple.streamingkeydelivery"

#EXT-X-STREAM-INF:BANDWIDTH=1280000,CODECS="avc1.640028,mp4a.40.2"
video/720p.m3u8
```

### Призначення атрибутів `Encryptable` (всередині `SessionKeyItem`)
| Атрибут | Тип | Обов'язковий? | Призначення |
|---------|-----|---------------|-------------|
| `METHOD` | `string` | ✅ Так | Алгоритм шифрування: `"AES-128"`, `"SAMPLE-AES"`, `"NONE"` |
| `URI` | `*string` | ⚠️ Умовно | Посилання на ключ (обов'язкове, якщо `METHOD != "NONE"`) |
| `IV` | `*string` | ❌ Ні | Вектор ініціалізації (16 байт hex), за замовчуванням генерується з PTS |
| `KEYFORMAT` | `*string` | ❌ Ні | Ідентифікатор формату ключа (DRM-система) |
| `KEYFORMATVERSIONS` | `*string` | ❌ Ні | Версії підтримуваного формату |

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

## 🔬 Детальний розбір тесту `TestSessionKeyItem_Parse`

```go
func TestSessionKeyItem_Parse(t *testing.T) {
    // 🎯 Вхідний рядок з усіма атрибутами шифрування
    line := `#EXT-X-SESSION-KEY:METHOD=AES-128,URI="http://test.key",IV=D512BBF,KEYFORMAT="identity",KEYFORMATVERSIONS="1/3"`
    
    // 🎯 Парсинг через конструктор
    ski, err := m3u8.NewSessionKeyItem(line)
    assert.Nil(t, err)
    
    // 🎯 Перевірка композиції: SessionKeyItem містить Encryptable
    assert.NotNil(t, ski.Encryptable)
    
    // 🎯 Перевірка обов'язкового поля (не-покажчик)
    assert.Equal(t, "AES-128", ski.Encryptable.Method)
    
    // 🎯 Перевірка опціональних полів-покажчиків через хелпер
    assertNotNilEqual(t, "http://test.key", ski.Encryptable.URI)           // *string
    assertNotNilEqual(t, "D512BBF", ski.Encryptable.IV)                    // *string (hex)
    assertNotNilEqual(t, "identity", ski.Encryptable.KeyFormat)            // *string
    assertNotNilEqual(t, "1/3", ski.Encryptable.KeyFormatVersions)         // *string
    
    // 🎯 Кругова перевірка: серіалізація має відтворити оригінал
    assertToString(t, line, ski)  // \n нормалізуються хелпером
}
```

### 🎯 Що тестує цей кейс?
| Аспект | Вхід у тесті | Чому це важливо |
|--------|-------------|----------------|
| **METHOD** | `AES-128` | Найпоширеніший алгоритм для HLS; має бути розпізнаний |
| **URI в лапках** | `"http://test.key"` | Рядкові значення у специфікації завжди в лапках |
| **IV без лапок** | `D512BBF` | Hex-значення можуть бути без лапок (специфікація дозволяє) |
| **KEYFORMAT** | `"identity"` | Вказує на простий формат ключа (не DRM) |
| **KEYFORMATVERSIONS** | `"1/3"` | Підтримка кількох версій через роздільник `/` |
| **Композиція** | `ski.Encryptable.Method` | Перевірка архітектурного патерну (не роздмухана структура) |

---

## 🏗️ Припустима структура `SessionKeyItem` та `Encryptable`

```go
// 🎯 SessionKeyItem — обгортка для поліморфізму (реалізує m3u8.Item)
type SessionKeyItem struct {
    Encryptable *Encryptable  // Композиція: всі атрибути шифрування тут
}

// 🎯 Encryptable — спільна логіка для #EXT-X-KEY та #EXT-X-SESSION-KEY
type Encryptable struct {
    Method            string   // ✅ Обов'язковий: "AES-128", "SAMPLE-AES", "NONE"
    URI               *string  // Посилання на ключ (обов'язкове якщо METHOD != "NONE")
    IV                *string  // Вектор ініціалізації (16 байт hex, опціонально)
    KeyFormat         *string  // DRM-система: "com.apple.streamingkeydelivery", тощо
    KeyFormatVersions *string  // Версії: "1", "1/2", "1/3"
}
```

### 🎯 Чому композиція `SessionKeyItem → Encryptable`?
```go
// ✅ Переваги патерну:
// • Єдине джерело правди для атрибутів шифрування
// • Повторне використання: Encryptable використовується і в #EXT-X-KEY
// • Легша підтримка: зміни в атрибутах — тільки в одному місці
// • Чистіший код: SessionKeyItem відповідає за інтерфейс Item, Encryptable — за логіку

// 🔄 Приклад використання у парсері:
func NewSessionKeyItem(text string) (*SessionKeyItem, error) {
    attrs := ParseAttributes(text)  // map[string]string
    return &SessionKeyItem{
        Encryptable: NewEncryptable(attrs),  // Делегування парсингу
    }, nil
}

func NewEncryptable(attrs map[string]string) *Encryptable {
    return &Encryptable{
        Method:            attrs[MethodTag],              // Обов'язковий
        URI:               pointerTo(attrs, URITag),      // *string або nil
        IV:                pointerTo(attrs, IVTag),
        KeyFormat:         pointerTo(attrs, KeyFormatTag),
        KeyFormatVersions: pointerTo(attrs, KeyFormatVersionsTag),
    }
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність валідації обов'язкового `METHOD`
```go
// ❌ Поточний тест не перевіряє помилки при відсутньому METHOD
// ✅ Додати тест на валідацію:

func TestSessionKeyItem_Parse_Invalid(t *testing.T) {
    cases := []struct{
        name  string
        input string
        wantErr bool
    }{
        {"missing_method", `#EXT-X-SESSION-KEY:URI="key.bin"`, true},
        {"invalid_method", `#EXT-X-SESSION-KEY:METHOD=INVALID`, true},
        {"missing_uri_with_method", `#EXT-X-SESSION-KEY:METHOD=AES-128`, true},
        {"valid_none_method", `#EXT-X-SESSION-KEY:METHOD=NONE`, false},  // URI не потрібен
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            ski, err := m3u8.NewSessionKeyItem(tc.input)
            if tc.wantErr {
                assert.Error(t, err)
                assert.Nil(t, ski)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, ski)
            }
        })
    }
}
```

### 2️⃣ Валідація формату `IV` (hex, 16 байт)
```go
// ❌ Тест використовує "D512BBF" — тільки 7 hex-символів (не 32!)
// ✅ Специфікація: IV має бути 16 байт = 32 hex-символи (або 0x + 32)

// 🎯 Додати перевірку валідного формату:
func TestSessionKeyItem_IV_Format(t *testing.T) {
    cases := []struct{
        name  string
        iv    string
        valid bool
    }{
        {"valid_32hex", "0123456789ABCDEF0123456789ABCDEF", true},
        {"valid_with_0x", "0x0123456789ABCDEF0123456789ABCDEF", true},
        {"too_short", "D512BBF", false},  // 7 символів ≠ 32
        {"invalid_chars", "GGGGHHHH", false},  // Не hex
        {"empty", "", false},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            line := fmt.Sprintf(`#EXT-X-SESSION-KEY:METHOD=AES-128,URI="k.bin",IV=%s`, tc.iv)
            ski, err := m3u8.NewSessionKeyItem(line)
            
            if tc.valid {
                assert.NoError(t, err)
                assertNotNilEqual(t, tc.iv, ski.Encryptable.IV)
            } else {
                // Залежить від реалізації: помилка валідації чи прийняття "як є"
                // Рекомендовано: валідувати на рівні парсера
                assert.Error(t, err, "invalid IV format should be rejected")
            }
        })
    }
}
```

### 3️⃣ Взаємовиключність `METHOD=NONE` та `URI`
```go
// ✅ Специфікація: якщо METHOD=NONE, URI не повинен бути вказаний
// ❌ Тест не перевіряє цю логіку

func TestSessionKeyItem_NoneMethod_NoURI(t *testing.T) {
    // ✅ Валідний випадок: METHOD=NONE без URI
    line := `#EXT-X-SESSION-KEY:METHOD=NONE`
    ski, err := m3u8.NewSessionKeyItem(line)
    assert.NoError(t, err)
    assert.Equal(t, "NONE", ski.Encryptable.Method)
    assert.Nil(t, ski.Encryptable.URI)  // URI має бути nil
    
    // ❌ Невалідний випадок: METHOD=NONE з URI
    lineInvalid := `#EXT-X-SESSION-KEY:METHOD=NONE,URI="should-not-be-here.key"`
    ski2, err2 := m3u8.NewSessionKeyItem(lineInvalid)
    assert.Error(t, err2, "URI should not be specified when METHOD=NONE")
}
```

### 4️⃣ Екранування спецсимволів у `URI`
```go
// ❌ Тест використовує простий URI без спецсимволів
// ✅ Реальні URI можуть містити лапки, &, ?, тощо

func TestSessionKeyItem_URI_WithSpecialChars(t *testing.T) {
    // 🎯 URI з query-параметрами
    line := `#EXT-X-SESSION-KEY:METHOD=AES-128,URI="https://keys.com/key?ch=1&exp=1234567890"`
    ski, err := m3u8.NewSessionKeyItem(line)
    assert.NoError(t, err)
    assertNotNilEqual(t, "https://keys.com/key?ch=1&exp=1234567890", ski.Encryptable.URI)
    
    // 🎯 URI з екранованими лапками (якщо специфікація дозволяє)
    // lineEscaped := `#EXT-X-SESSION-KEY:METHOD=AES-128,URI="https://keys.com/key\"name.bin"`
    // ... перевірка ...
}
```

### 5️⃣ Кругова перевірка: чи дійсно `String()` повертає оригінал?
```go
// ❌ assertToString(t, line, ski) порівнює після нормалізації \n
// ✅ Але: порядок атрибутів може змінитися при серіалізації!

// 🎯 Додати перевірку на порядок (якщо це важливо для сумісності):
func TestSessionKeyItem_String_AttributeOrder(t *testing.T) {
    line := `#EXT-X-SESSION-KEY:METHOD=AES-128,URI="k.bin",IV=ABC`
    ski, _ := m3u8.NewSessionKeyItem(line)
    output := ski.String()
    
    // 🎯 Перевірка, що ключові атрибути присутні (незалежно від порядку)
    assert.Contains(t, output, "METHOD=AES-128")
    assert.Contains(t, output, `URI="k.bin"`)
    assert.Contains(t, output, "IV=ABC")
    
    // 🎯 Якщо порядок критичний — перевірити точно:
    // assert.Equal(t, normalize(line), normalize(output))
}
```

### 6️⃣ Відсутність тесту на `KEYFORMAT` валідацію
```go
// ✅ KEYFORMAT має бути у форматі зворотного DNS або UUID
// ❌ Тест не перевіряє це

func TestSessionKeyItem_KeyFormat_Validation(t *testing.T) {
    cases := []struct{
        name  string
        format string
        valid bool
    }{
        {"reverse_dns", "com.apple.streamingkeydelivery", true},
        {"widevine_uuid", "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed", true},
        {"playready_uuid", "urn:uuid:9a04f079-9840-4286-ab92-e65be0885f95", true},
        {"invalid_format", "not-a-valid-format", false},
        {"empty", "", true},  // Опціональний атрибут
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            line := fmt.Sprintf(`#EXT-X-SESSION-KEY:METHOD=SAMPLE-AES,URI="k.bin",KEYFORMAT="%s"`, tc.format)
            ski, err := m3u8.NewSessionKeyItem(line)
            
            if tc.valid {
                assert.NoError(t, err)
                if tc.format != "" {
                    assertNotNilEqual(t, tc.format, ski.Encryptable.KeyFormat)
                }
            } else {
                assert.Error(t, err, "invalid KEYFORMAT should be rejected")
            }
        })
    }
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **захищеним стрімінгом**:

### 🎯 Сценарій: генерація `#EXT-X-SESSION-KEY` для Multi-DRM
```go
// У generateMasterPlaylist для підтримки різних платформ:
func addMultiDRMKeys(pl *m3u8.Playlist, drmConfig DRMConfig) {
    // 🎯 FairPlay для iOS/tvOS
    if drmConfig.FairPlay.Enabled {
        pl.AppendItem(&m3u8.SessionKeyItem{
            Encryptable: &m3u8.Encryptable{
                Method:            "SAMPLE-AES",
                URI:               pointer.ToString(drmConfig.FairPlay.LicenseURI),
                KeyFormat:         pointer.ToString("com.apple.streamingkeydelivery"),
                KeyFormatVersions: pointer.ToString("1"),
            },
        })
    }
    
    // 🎯 Widevine для Android/Chrome
    if drmConfig.Widevine.Enabled {
        pl.AppendItem(&m3u8.SessionKeyItem{
            Encryptable: &m3u8.Encryptable{
                Method:            "SAMPLE-AES",
                URI:               pointer.ToString(drmConfig.Widevine.LicenseURI),
                KeyFormat:         pointer.ToString("urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"),
                KeyFormatVersions: pointer.ToString("1"),
            },
        })
    }
    
    // 🎯 PlayReady для Edge/Xbox (опціонально)
    if drmConfig.PlayReady.Enabled {
        pl.AppendItem(&m3u8.SessionKeyItem{
            Encryptable: &m3u8.Encryptable{
                Method:            "SAMPLE-AES",
                URI:               pointer.ToString(drmConfig.PlayReady.LicenseURI),
                KeyFormat:         pointer.ToString("urn:uuid:9a04f079-9840-4286-ab92-e65be0885f95"),
                KeyFormatVersions: pointer.ToString("1"),
            },
        })
    }
}
```

### 🎯 Сценарій: key rotation для live-стріму
```go
// У KeyManager для періодичної зміни ключів:
type KeyRotator struct {
    mu           sync.Mutex
    currentKeyID string
    keyPeriod    time.Duration  // Напр. 1 година
    keyURIFormat string         // "https://keys.alarabiya.net/{key_id}.bin"
}

func (kr *KeyRotator) GetCurrentSessionKey() (*m3u8.SessionKeyItem, error) {
    kr.mu.Lock()
    defer kr.mu.Unlock()
    
    // 🎯 Генерація нового ключа, якщо минув період
    if time.Since(kr.lastRotation) > kr.keyPeriod {
        kr.currentKeyID = generateSecureKeyID()  // UUID або secure random
        kr.lastRotation = time.Now()
        
        // 📢 Тут можна відправити новий ключ на сервер ліцензій
        if err := kr.uploadKeyToServer(kr.currentKeyID); err != nil {
            return nil, fmt.Errorf("failed to upload key: %w", err)
        }
    }
    
    // 🎯 Генерація детермінованого IV з ключа + часу
    iv := kr.deriveIV(kr.currentKeyID, kr.lastRotation)
    
    // 🎯 Формування SessionKeyItem
    return &m3u8.SessionKeyItem{
        Encryptable: &m3u8.Encryptable{
            Method: "AES-128",
            URI:    pointer.ToString(fmt.Sprintf(kr.keyURIFormat, kr.currentKeyID)),
            IV:     pointer.ToString(hex.EncodeToString(iv)),
        },
    }, nil
}
```

### 🎯 Сценарій: валідація SessionKeyItem перед додаванням у плейлист
```go
// У segmentFinalizer для забезпечення валідності:
func (sf *SegmentFinalizer) validateSessionKey(ski *m3u8.SessionKeyItem) error {
    enc := ski.Encryptable
    if enc == nil {
        return fmt.Errorf("Encryptable is required")
    }
    
    // ✅ Обов'язковий METHOD
    if enc.Method == "" {
        return fmt.Errorf("METHOD is required")
    }
    
    // ✅ Валідація допустимих значень METHOD
    validMethods := map[string]bool{"AES-128": true, "SAMPLE-AES": true, "NONE": true}
    if !validMethods[enc.Method] {
        return fmt.Errorf("invalid METHOD: %s", enc.Method)
    }
    
    // ✅ URI обов'язковий, якщо METHOD != "NONE"
    if enc.Method != "NONE" && enc.URI == nil {
        return fmt.Errorf("URI is required when METHOD is %s", enc.Method)
    }
    
    // ✅ Валідація IV формату (якщо вказано)
    if enc.IV != nil && !isValidHex(*enc.IV, 32) {
        return fmt.Errorf("IV must be 32 hex characters, got: %s", *enc.IV)
    }
    
    // ✅ Валідація KEYFORMAT (якщо вказано)
    if enc.KeyFormat != nil && !isValidKeyFormat(*enc.KeyFormat) {
        return fmt.Errorf("invalid KEYFORMAT: %s", *enc.KeyFormat)
    }
    
    return nil
}

func isValidHex(s string, expectedLen int) bool {
    s = strings.TrimPrefix(strings.ToLower(s), "0x")
    if len(s) != expectedLen {
        return false
    }
    for _, r := range s {
        if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
            return false
        }
    }
    return true
}

func isValidKeyFormat(format string) bool {
    // 🎯 Перевірка зворотного DNS або UUID формату
    if strings.HasPrefix(format, "urn:uuid:") {
        // UUID формат
        _, err := uuid.Parse(format)
        return err == nil
    }
    // 🎯 Зворотний DNS формат
    parts := strings.Split(format, ".")
    return len(parts) >= 2 && parts[0] != "" && !unicode.IsDigit(rune(parts[0][0]))
}
```

---

## 🧪 Приклад: розширений набір тестів для `SessionKeyItem`

```go
// ✅ Повний набір тестів з subtests та валідацією:
func TestSessionKeyItem(t *testing.T) {
    t.Parallel()
    
    t.Run("Parse/FullAttributes", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-KEY:METHOD=AES-128,URI="http://test.key",IV=D512BBF0000000000000000000000000,KEYFORMAT="identity"`
        ski, err := m3u8.NewSessionKeyItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "AES-128", ski.Encryptable.Method)
        assertNotNilEqual(t, "http://test.key", ski.Encryptable.URI)
        assertNotNilEqual(t, "D512BBF0000000000000000000000000", ski.Encryptable.IV)
        assertNotNilEqual(t, "identity", ski.Encryptable.KeyFormat)
        assertToString(t, line, ski)
    })
    
    t.Run("Parse/MinimalAES128", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-KEY:METHOD=AES-128,URI="key.bin"`
        ski, err := m3u8.NewSessionKeyItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "AES-128", ski.Encryptable.Method)
        assertNotNilEqual(t, "key.bin", ski.Encryptable.URI)
        assert.Nil(t, ski.Encryptable.IV)  // Опціональний
    })
    
    t.Run("Parse/MethodNone", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-KEY:METHOD=NONE`
        ski, err := m3u8.NewSessionKeyItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "NONE", ski.Encryptable.Method)
        assert.Nil(t, ski.Encryptable.URI)  // Не повинен бути вказаний
    })
    
    t.Run("Parse/Invalid/MissingMethod", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-KEY:URI="key.bin"`
        _, err := m3u8.NewSessionKeyItem(line)
        assert.Error(t, err, "METHOD is required")
    })
    
    t.Run("Parse/Invalid/NoneWithURI", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-SESSION-KEY:METHOD=NONE,URI="should-not-be-here.bin"`
        _, err := m3u8.NewSessionKeyItem(line)
        assert.Error(t, err, "URI should not be specified with METHOD=NONE")
    })
    
    t.Run("Parse/Invalid/ShortIV", func(t *testing.T) {
        t.Parallel()
        // IV має бути 32 hex-символи, а не 7
        line := `#EXT-X-SESSION-KEY:METHOD=AES-128,URI="k.bin",IV=ABC`
        _, err := m3u8.NewSessionKeyItem(line)
        // Залежить від реалізації: валідувати чи ні
        // Рекомендовано: валідувати
        assert.Error(t, err, "IV should be 32 hex characters")
    })
    
    t.Run("RoundTrip/WithAllAttributes", func(t *testing.T) {
        t.Parallel()
        original := `#EXT-X-SESSION-KEY:METHOD=AES-128,URI="https://keys.com/key.bin",IV=0123456789ABCDEF0123456789ABCDEF,KEYFORMAT="com.example.drm",KEYFORMATVERSIONS="1/2"`
        ski, err := m3u8.NewSessionKeyItem(original)
        assert.NoError(t, err)
        
        output := ski.String()
        assert.Equal(t, normalizeM3U8(original), normalizeM3U8(output))
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до `#EXT-X-SESSION-KEY`

```
✅ #EXT-X-SESSION-KEY дозволений ТІЛЬКИ у Master Playlist
✅ METHOD — обов'язковий, допустимі значення:
   • "AES-128": стандартне шифрування, ключ завантажується з URI
   • "SAMPLE-AES": шифрування на рівні семплів (для DRM)
   • "NONE": вимкнути шифрування (для переходу між зашифрованими/незашифрованими сегментами)
✅ URI — обов'язковий, якщо METHOD != "NONE":
   • Абсолютний або відносний URL до файлу з 16-байтовим ключем
   • Для AES-128: файл має містити рівно 16 байт (128 біт)
   • Для SAMPLE-AES: може бути схема "skd://", "http://", "https://"
✅ IV — опціональний, але якщо вказаний:
   • Має бути 16 байт, представлені як 32 hex-символи (опціонально з префіксом "0x")
   • Якщо відсутній: плеєр генерує IV з медіа-сегмента (PTS)
✅ KEYFORMAT — опціональний, але ОБОВ'ЯЗКОВИЙ для SAMPLE-AES:
   • "identity": простий формат (ключ = 16 байт з URI)
   • "com.apple.streamingkeydelivery": FairPlay DRM
   • "urn:uuid:edef8ba9-...": Widevine DRM
   • "urn:uuid:9a04f079-...": PlayReady DRM
✅ KEYFORMATVERSIONS — опціональний:
   • Перелік підтримуваних версій через "/": "1", "1/2", "1/2/3"
   • За замовчуванням: "1"
✅ Кілька #EXT-X-SESSION-KEY з різними KEYFORMAT дозволені для multi-DRM
✅ Клієнти МАЮТЬ ігнорувати SESSION-KEY з непідтримуваним KEYFORMAT
✅ SESSION-KEY застосовується до ВСІХ варіантів у Master Playlist (якщо не перевизначено у Media Playlist)
```

---

## 🎯 Висновок

Цей тест — **солідна основа** для валідації `SessionKeyItem`:

✅ Покриття повного набору атрибутів шифрування  
✅ Перевірка композиції `SessionKeyItem → Encryptable`  
✅ Кругова перевірка серіалізації

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати валідацію `METHOD` (обов'язковий, допустимі значення)
2. ✅ Забезпечити обов'язковість `URI` коли `METHOD != "NONE"`
3. ✅ Валідувати формат `IV` (32 hex-символи = 16 байт)
4. ✅ Додати тести на `METHOD=NONE` (без URI)
5. ✅ Перевірити стійкість до спецсимволів у `URI`

**Приклад конфігурації для Al Arabiya CCTV**:
```go
// Захищений live-стрім з multi-DRM підтримкою:
drmConfig := DRMConfig{
    FairPlay: DRMSystem{
        Enabled:    true,
        LicenseURI: "skd://al-arabiya/live",
        KeyFormat:  "com.apple.streamingkeydelivery",
    },
    Widevine: DRMSystem{
        Enabled:    true,
        LicenseURI: "https://license.alarabiya.net/widevine",
        KeyFormat:  "urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed",
    },
    PlayReady: DRMSystem{
        Enabled:    true,
        LicenseURI: "https://license.alarabiya.net/playready",
        KeyFormat:  "urn:uuid:9a04f079-9840-4286-ab92-e65be0885f95",
    },
}
// → Кожен клієнт автоматично обере підтримуваний метод шифрування
```

Потрібно допомогти з:
- 🔗 Реалізацією `KeyRotator` для автоматичної ротації ключів?
- 🧠 Інтеграцією multi-DRM (`SessionKeyItem`) у ваш Master Playlist генератор?
- 🧪 Написанням інтеграційних тестів для шифрування + відтворення у тестовому плеєрі?

Чекаю на ваші питання! 🛠️🔐🎬