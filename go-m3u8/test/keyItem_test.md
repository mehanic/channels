# 🔍 Глибокий розбір тесту: `KeyItem` для HLS `#EXT-X-KEY`

Цей файл містить **юніт-тест** для парсингу та серіалізації тега `#EXT-X-KEY` — механізму шифрування медіа-сегментів у HLS. Розберемо архітектурно та детально.

---

## 📦 Що таке `#EXT-X-KEY` і навіщо він потрібен?

### Контекст: шифрування у HLS
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4

#EXT-X-KEY:METHOD=AES-128,URI="https://keys.example.com/key.bin",IV=0xD512BBF0000000000000000000000000
#EXTINF:4.0,
seg_encrypted_001.ts
#EXTINF:4.0,
seg_encrypted_002.ts

#EXT-X-KEY:METHOD=NONE  ← Вимкнення шифрування
#EXTINF:4.0,
seg_plain_003.ts
```

### Призначення атрибутів `Encryptable` (всередині `KeyItem`)
| Атрибут | Тип | Обов'язковий? | Призначення |
|---------|-----|---------------|-------------|
| `METHOD` | `string` | ✅ Так | Алгоритм шифрування: `"AES-128"`, `"SAMPLE-AES"`, `"NONE"` |
| `URI` | `*string` | ⚠️ Умовно | Посилання на ключ (обов'язкове, якщо `METHOD != "NONE"`) |
| `IV` | `*string` | ❌ Ні | Вектор ініціалізації (16 байт hex), за замовчуванням генерується з PTS |
| `KEYFORMAT` | `*string` | ❌ Ні | Ідентифікатор формату ключа (DRM-система) |
| `KEYFORMATVERSIONS` | `*string` | ❌ Ні | Версії підтримуваного формату |

### 🎯 Критичні сценарії використання у вашому проекті
```
🔒 Захищений CCTV-стрім (платний доступ):
#EXT-X-KEY:METHOD=AES-128,URI="https://keys.alarabiya.net/live/ch1.bin"
→ Тільки авторизовані клієнти отримують ключ → дешифрують потік

🔄 Key rotation для безпеки:
#EXT-X-KEY:METHOD=AES-128,URI="https://keys.../key_v1.bin",IV=0x...
#EXTINF:4.0, seg_v1_001.ts
#EXT-X-DISCONTINUITY
#EXT-X-KEY:METHOD=AES-128,URI="https://keys.../key_v2.bin",IV=0x...
#EXTINF:4.0, seg_v2_001.ts  ← Новий ключ, новий IV
→ Періодична зміна ключів обмежує збитки при компрометації

🌍 Multi-DRM підтримка (через KEYFORMAT):
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="skd://fairplay",KEYFORMAT="com.apple.streamingkeydelivery"
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="https://widevine...",KEYFORMAT="urn:uuid:edef8ba9-..."
→ iOS-клієнти використовують FairPlay, Android — Widevine автоматично
```

---

## 🔬 Детальний розбір тесту `TestKeyItem_Parse`

```go
func TestKeyItem_Parse(t *testing.T) {
    // 🎯 Вхідний рядок з усіма атрибутами шифрування
    line := `#EXT-X-KEY:METHOD=AES-128,URI="http://test.key",IV=D512BBF,KEYFORMAT="identity",KEYFORMATVERSIONS="1/3"`

    // 🎯 Парсинг через конструктор
    ki, err := m3u8.NewKeyItem(line)
    assert.Nil(t, err)
    
    // 🎯 Перевірка композиції: KeyItem містить Encryptable
    assert.NotNil(t, ki.Encryptable)
    
    // 🎯 Перевірка обов'язкового поля (не-покажчик)
    assert.Equal(t, "AES-128", ki.Encryptable.Method)
    
    // 🎯 Перевірка опціональних полів-покажчиків через хелпер
    assertNotNilEqual(t, "http://test.key", ki.Encryptable.URI)           // *string
    assertNotNilEqual(t, "D512BBF", ki.Encryptable.IV)                    // *string (hex)
    assertNotNilEqual(t, "identity", ki.Encryptable.KeyFormat)            // *string
    assertNotNilEqual(t, "1/3", ki.Encryptable.KeyFormatVersions)         // *string
    
    // 🎯 Кругова перевірка: серіалізація має відтворити оригінал
    assertToString(t, line, ki)  // \n нормалізуються хелпером
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
| **Композиція** | `ki.Encryptable.Method` | Перевірка архітектурного патерну (не роздмухана структура) |

---

## 🏗️ Припустима структура `KeyItem` та `Encryptable`

```go
// 🎯 KeyItem — обгортка для поліморфізму (реалізує m3u8.Item)
type KeyItem struct {
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

### 🎯 Чому композиція `KeyItem → Encryptable`?
```go
// ✅ Переваги патерну:
// • Єдине джерело правди для атрибутів шифрування
// • Повторне використання: Encryptable використовується і в #EXT-X-SESSION-KEY
// • Легша підтримка: зміни в атрибутах — тільки в одному місці
// • Чистіший код: KeyItem відповідає за інтерфейс Item, Encryptable — за логіку

// 🔄 Приклад використання у парсері:
func NewKeyItem(text string) (*KeyItem, error) {
    attrs := ParseAttributes(text)  // map[string]string
    return &KeyItem{
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

### 1️⃣ Валідація обов'язкового `METHOD`
```go
// ❌ Поточний тест не перевіряє помилки при відсутньому METHOD
// ✅ Додати тест на валідацію:

func TestKeyItem_Parse_Invalid(t *testing.T) {
    cases := []struct{
        name  string
        input string
        wantErr bool
    }{
        {"missing_method", `#EXT-X-KEY:URI="key.bin"`, true},
        {"invalid_method", `#EXT-X-KEY:METHOD=INVALID`, true},
        {"missing_uri_with_method", `#EXT-X-KEY:METHOD=AES-128`, true},
        {"valid_none_method", `#EXT-X-KEY:METHOD=NONE`, false},  // URI не потрібен
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            ki, err := m3u8.NewKeyItem(tc.input)
            if tc.wantErr {
                assert.Error(t, err)
                assert.Nil(t, ki)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, ki)
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
func TestKeyItem_IV_Format(t *testing.T) {
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
            line := fmt.Sprintf(`#EXT-X-KEY:METHOD=AES-128,URI="k.bin",IV=%s`, tc.iv)
            ki, err := m3u8.NewKeyItem(line)
            
            if tc.valid {
                assert.NoError(t, err)
                assertNotNilEqual(t, tc.iv, ki.Encryptable.IV)
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

func TestKeyItem_NoneMethod_NoURI(t *testing.T) {
    // ✅ Валідний випадок: METHOD=NONE без URI
    line := `#EXT-X-KEY:METHOD=NONE`
    ki, err := m3u8.NewKeyItem(line)
    assert.NoError(t, err)
    assert.Equal(t, "NONE", ki.Encryptable.Method)
    assert.Nil(t, ki.Encryptable.URI)  // URI має бути nil
    
    // ❌ Невалідний випадок: METHOD=NONE з URI
    lineInvalid := `#EXT-X-KEY:METHOD=NONE,URI="should-not-be-here.key"`
    ki2, err2 := m3u8.NewKeyItem(lineInvalid)
    assert.Error(t, err2, "URI should not be specified when METHOD=NONE")
}
```

### 4️⃣ Екранування спецсимволів у `URI`
```go
// ❌ Тест використовує простий URI без спецсимволів
// ✅ Реальні URI можуть містити лапки, &, ?, тощо

func TestKeyItem_URI_WithSpecialChars(t *testing.T) {
    // 🎯 URI з query-параметрами
    line := `#EXT-X-KEY:METHOD=AES-128,URI="https://keys.com/key?ch=1&exp=1234567890"`
    ki, err := m3u8.NewKeyItem(line)
    assert.NoError(t, err)
    assertNotNilEqual(t, "https://keys.com/key?ch=1&exp=1234567890", ki.Encryptable.URI)
    
    // 🎯 URI з екранованими лапками (якщо специфікація дозволяє)
    // lineEscaped := `#EXT-X-KEY:METHOD=AES-128,URI="https://keys.com/key\"name.bin"`
    // ... перевірка ...
}
```

### 5️⃣ Кругова перевірка: чи дійсно `String()` повертає оригінал?
```go
// ❌ assertToString(t, line, ki) порівнює після нормалізації \n
// ✅ Але: порядок атрибутів може змінитися при серіалізації!

// 🎯 Додати перевірку на порядок (якщо це важливо для сумісності):
func TestKeyItem_String_AttributeOrder(t *testing.T) {
    line := `#EXT-X-KEY:METHOD=AES-128,URI="k.bin",IV=ABC`
    ki, _ := m3u8.NewKeyItem(line)
    output := ki.String()
    
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

func TestKeyItem_KeyFormat_Validation(t *testing.T) {
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
            line := fmt.Sprintf(`#EXT-X-KEY:METHOD=SAMPLE-AES,URI="k.bin",KEYFORMAT="%s"`, tc.format)
            ki, err := m3u8.NewKeyItem(line)
            
            if tc.valid {
                assert.NoError(t, err)
                if tc.format != "" {
                    assertNotNilEqual(t, tc.format, ki.Encryptable.KeyFormat)
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

### 🎯 Сценарій: генерація `#EXT-X-KEY` для AES-128 шифрування
```go
// У segmentFinalizer при створенні зашифрованого плейлиста:
func (sf *SegmentFinalizer) addEncryptionKey(keyID string, keyURI string, iv []byte) error {
    // 🎯 Форматування IV як hex-рядок (32 символи)
    ivHex := hex.EncodeToString(iv)  // []byte{0xD5, 0x12, ...} → "D512..."
    
    // 🎯 Створення KeyItem
    keyItem := &m3u8.KeyItem{
        Encryptable: &m3u8.Encryptable{
            Method: "AES-128",
            URI:    pointer.ToString(keyURI),
            IV:     pointer.ToString(ivHex),
            // KEYFORMAT не вказуємо → простий AES-128 без DRM
        },
    }
    
    // 🎯 Валідація перед додаванням
    if err := sf.validateKeyItem(keyItem); err != nil {
        return fmt.Errorf("invalid encryption config: %w", err)
    }
    
    // 🎯 Додавання у плейлист (перед першим зашифрованим сегментом)
    sf.playlist.AppendItem(keyItem)
    
    sf.logger.Info("added encryption key", "key_id", keyID, "uri", keyURI)
    return nil
}

// 🎯 Helper для валідації
func (sf *SegmentFinalizer) validateKeyItem(ki *m3u8.KeyItem) error {
    if ki.Encryptable.Method == "" {
        return fmt.Errorf("METHOD is required")
    }
    
    if ki.Encryptable.Method != "NONE" && ki.Encryptable.URI == nil {
        return fmt.Errorf("URI is required when METHOD is %s", ki.Encryptable.Method)
    }
    
    if ki.Encryptable.IV != nil {
        if !isValidHex(*ki.Encryptable.IV, 32) {  // 32 hex = 16 байт
            return fmt.Errorf("IV must be 32 hex characters, got: %s", *ki.Encryptable.IV)
        }
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

func (kr *KeyRotator) GetCurrentKeyItem() (*m3u8.KeyItem, error) {
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
    
    // 🎯 Формування KeyItem
    return &m3u8.KeyItem{
        Encryptable: &m3u8.Encryptable{
            Method: "AES-128",
            URI:    pointer.ToString(fmt.Sprintf(kr.keyURIFormat, kr.currentKeyID)),
            IV:     pointer.ToString(hex.EncodeToString(iv)),
        },
    }, nil
}

// 🎯 Використання у segmentFinalizer:
func (sf *SegmentFinalizer) rotateKeyIfNeeded(seqNum int) error {
    if seqNum%sf.keyRotationInterval == 0 {  // Напр. кожні 900 сегментів = 1 година
        keyItem, err := sf.keyRotator.GetCurrentKeyItem()
        if err != nil {
            return err
        }
        
        // 🎯 Вставка розриву + нового ключа
        disc, _ := m3u8.NewDiscontinuityItem()
        sf.playlist.AppendItem(disc)
        sf.playlist.AppendItem(keyItem)
        
        sf.discontinuitySequence++
        sf.logger.Info("rotated encryption key", "seq", seqNum)
    }
    return nil
}
```

### 🎯 Сценарій: multi-DRM підтримка через `#EXT-X-SESSION-KEY`
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

---

## 🧪 Приклад: розширений набір тестів для `KeyItem`

```go
// ✅ Повний набір тестів з subtests та валідацією:
func TestKeyItem(t *testing.T) {
    t.Parallel()
    
    t.Run("Parse/FullAttributes", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-KEY:METHOD=AES-128,URI="http://test.key",IV=D512BBF0000000000000000000000000,KEYFORMAT="identity"`
        ki, err := m3u8.NewKeyItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "AES-128", ki.Encryptable.Method)
        assertNotNilEqual(t, "http://test.key", ki.Encryptable.URI)
        assertNotNilEqual(t, "D512BBF0000000000000000000000000", ki.Encryptable.IV)
        assertNotNilEqual(t, "identity", ki.Encryptable.KeyFormat)
        assertToString(t, line, ki)
    })
    
    t.Run("Parse/MinimalAES128", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-KEY:METHOD=AES-128,URI="key.bin"`
        ki, err := m3u8.NewKeyItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "AES-128", ki.Encryptable.Method)
        assertNotNilEqual(t, "key.bin", ki.Encryptable.URI)
        assert.Nil(t, ki.Encryptable.IV)  // Опціональний
    })
    
    t.Run("Parse/MethodNone", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-KEY:METHOD=NONE`
        ki, err := m3u8.NewKeyItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "NONE", ki.Encryptable.Method)
        assert.Nil(t, ki.Encryptable.URI)  // Не повинен бути вказаний
    })
    
    t.Run("Parse/Invalid/MissingMethod", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-KEY:URI="key.bin"`
        _, err := m3u8.NewKeyItem(line)
        assert.Error(t, err, "METHOD is required")
    })
    
    t.Run("Parse/Invalid/NoneWithURI", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-KEY:METHOD=NONE,URI="should-not-be-here.bin"`
        _, err := m3u8.NewKeyItem(line)
        assert.Error(t, err, "URI should not be specified with METHOD=NONE")
    })
    
    t.Run("Parse/Invalid/ShortIV", func(t *testing.T) {
        t.Parallel()
        // IV має бути 32 hex-символи, а не 7
        line := `#EXT-X-KEY:METHOD=AES-128,URI="k.bin",IV=ABC`
        _, err := m3u8.NewKeyItem(line)
        // Залежить від реалізації: валідувати чи ні
        // Рекомендовано: валідувати
        assert.Error(t, err, "IV should be 32 hex characters")
    })
    
    t.Run("RoundTrip/WithAllAttributes", func(t *testing.T) {
        t.Parallel()
        original := `#EXT-X-KEY:METHOD=AES-128,URI="https://keys.com/key.bin",IV=0123456789ABCDEF0123456789ABCDEF,KEYFORMAT="com.example.drm",KEYFORMATVERSIONS="1/2"`
        ki, err := m3u8.NewKeyItem(original)
        assert.NoError(t, err)
        
        output := ki.String()
        assert.Equal(t, normalizeM3U8(original), normalizeM3U8(output))
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до `#EXT-X-KEY`

```
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

✅ Розташування:
   • #EXT-X-KEY має з'являтися ПЕРЕД першим сегментом, до якого застосовується
   • Може з'являтися кілька разів у плейлисті (key rotation)
   • При зміні ключа: рекомендується #EXT-X-DISCONTINUITY перед новим #EXT-X-KEY

✅ Безпека:
   • URI має використовувати HTTPS для захисту ключа при передачі
   • Ключі мають бути унікальними та періодично змінюватися (key rotation)
   • Для production: використовувати DRM (FairPlay/Widevine) замість простого AES-128
```

---

## 🎯 Висновок

Цей тест — **солідна основа** для валідації `KeyItem`:

✅ Покриття повного набору атрибутів шифрування  
✅ Перевірка композиції `KeyItem → Encryptable`  
✅ Кругова перевірка серіалізації

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати валідацію `METHOD` (обов'язковий, допустимі значення)
2. ✅ Забезпечити обов'язковість `URI` коли `METHOD != "NONE"`
3. ✅ Валідувати формат `IV` (32 hex-символи = 16 байт)
4. ✅ Додати тести на `METHOD=NONE` (без URI)
5. ✅ Перевірити стійкість до спецсимволів у `URI`

**Приклад конфігурації для Al Arabiya CCTV**:
```go
// Захищений live-стрім з key rotation кожну годину:
drmConfig := DRMConfig{
    AES128: AES128Config{
        Enabled:        true,
        KeyRotationSec: 3600,  // Кожну годину
        KeyURIFormat:   "https://keys.alarabiya.net/live/{channel}/{key_id}.bin",
    },
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
}
// → Кожен клієнт автоматично обере підтримуваний метод шифрування
```

Потрібно допомогти з:
- 🔗 Реалізацією `KeyRotator` для автоматичної ротації ключів?
- 🧠 Інтеграцією multi-DRM (`SessionKeyItem`) у ваш Master Playlist генератор?
- 🧪 Написанням інтеграційних тестів для шифрування + відтворення у тестовому плеєрі?

Чекаю на ваші питання! 🛠️🔐