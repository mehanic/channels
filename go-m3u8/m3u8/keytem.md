# Глибоке роз'яснення: `m3u8.KeyItem` — представлення #EXT-X-KEY тегів для шифрування в HLS

Цей файл містить **реалізацію для роботи з #EXT-X-KEY тегами** — критичним елементом у HLS для вказівки параметрів шифрування сегментів. Він використовує універсальну структуру `Encryptable` для парсингу та серіалізації атрибутів.

---

## 🎯 Навіщо `KeyItem` потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ KeyItem у контексті HLS:               │
│                                         │
│ 🔹 Захист контенту:                    │
│   • Вказівка ключа шифрування для     │
│     сегментів відео                    │
│   • Підтримка AES-128, SAMPLE-AES     │
│     та інших методів                   │
│                                         │
│ 🔹 Гнучкість конфігурації:             │
│   • Динамічна ротація ключів          │
│   • Інтеграція з DRM-системами        │
│     (FairPlay, Widevine)               │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Захист конфіденційних записів     │
│   • Відповідність вимогам безпеки     │
│   • Аудит доступу до зашифрованих     │
│     сегментів                         │
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `KeyItem`: обгортка для `Encryptable`

```go
type KeyItem struct {
    Encryptable *Encryptable  // 🔹 Універсальна структура атрибутів шифрування
}
```

### 🎯 Чому обгортка, а не пряме використання `Encryptable`?

```
Причини:
✅ Семантична чіткість: 
   • `KeyItem` = конкретний тег #EXT-X-KEY
   • `Encryptable` = абстрактний набір атрибутів (використовується також для #EXT-X-SESSION-KEY)

✅ Розширюваність:
   • У майбутньому можна додати специфічні для #EXT-X-KEY поля
   • Не ламає існуючий код, що використовує `Encryptable`

✅ Типобезпека:
   • Функції, що очікують `*KeyItem`, не приймуть `*SessionKeyItem` випадково
   • Компілятор допомагає уникнути логічних помилок
```

---

## 🔍 Функція `NewKeyItem`: парсинг текстового представлення

```go
func NewKeyItem(text string) (*KeyItem, error) {
    // 🔹 1. Парсинг загальних атрибутів
    attributes := ParseAttributes(text)
    
    // 🔹 2. Делегування до NewEncryptable
    return &KeyItem{
        Encryptable: NewEncryptable(attributes),
    }, nil
}
```

### 🎯 Приклади використання:

```
Вхід: `METHOD=AES-128,URI="https://keys.example.com/key.bin",IV=0x1234...`
Після ParseAttributes():
{
    "METHOD": "AES-128",
    "URI": "https://keys.example.com/key.bin",
    "IV": "0x1234..."
}

Після NewEncryptable():
Encryptable{
    Method: "AES-128",
    URI: &"https://keys.example.com/key.bin",
    IV: &"0x1234...",
    // ... інші поля
}

Фінальний результат:
&KeyItem{
    Encryptable: &Encryptable{...}
}
```

### ⚠️ Потенційна проблема: відсутність валідації

```
Поточна реалізація:
• Парсить будь-які атрибути без перевірки
• Не перевіряє обов'язковість METHOD
• Не валідує формат URI, IV тощо

Наслідок:
• Невалідні #EXT-X-KEY теги можуть пройти парсинг
• Помилки виявляться пізніше, при використанні

✅ Рішення: додати валідацію після парсингу:
  func NewKeyItemValidated(text string) (*KeyItem, error) {
      ki, err := NewKeyItem(text)
      if err != nil { return nil, err }
      if err := ki.Validate(); err != nil { return nil, err }
      return ki, nil
  }
```

---

## 🔍 Метод `String()`: серіалізація у формат #EXT-X-KEY

```go
func (ki *KeyItem) String() string {
    return fmt.Sprintf("%s:%v", KeyItemTag, ki.Encryptable.String())
}
```

### 🎯 Приклад серіалізації:

```
Вхід:
  &KeyItem{
      Encryptable: &Encryptable{
          Method: "AES-128",
          URI: stringPtr("https://keys.example.com/key.bin"),
          IV: stringPtr("0x1234567890abcdef1234567890abcdef"),
      },
  }

Після ki.Encryptable.String():
  "METHOD=AES-128,URI=\"https://keys.example.com/key.bin\",IV=0x1234567890abcdef1234567890abcdef"

Після fmt.Sprintf("%s:%v", KeyItemTag, ...):
  "EXT-X-KEY:METHOD=AES-128,URI=\"https://keys.example.com/key.bin\",IV=0x1234567890abcdef1234567890abcdef"

Для використання у плейлисті:
  "#" + ki.String()
  → "#EXT-X-KEY:METHOD=AES-128,URI=\"https://keys.example.com/key.bin\",IV=0x1234567890abcdef1234567890abcdef"
```

### ⚠️ Потенційна проблема: відсутність `#` префіксу

```
Поточна реалізація:
  return fmt.Sprintf("%s:%v", KeyItemTag, ki.Encryptable.String())
  // Вихід: "EXT-X-KEY:..."

Очікування у плейлисті:
  "#EXT-X-KEY:..."  // ← префікс # обов'язковий!

✅ Рішення: або додати `#` у методі, або документувати, що клієнт має додавати його:
  // Варіант 1: додати у методі
  return fmt.Sprintf("#%s:%v", KeyItemTag, ki.Encryptable.String())
  
  // Варіант 2: документувати та додавати при використанні
  playlist.WriteString("#" + keyItem.String() + "\n")
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Додавання ключа шифрування до плейлиста

```go
// У VideoManifestProxy — генерація #EXT-X-KEY перед зашифрованими сегментами:
func addEncryptionKey(playlist *strings.Builder, keyURL, iv string) error {
    // 🔹 Підготувати атрибути
    attrs := map[string]string{
        "METHOD": "AES-128",
        "URI":    keyURL,
        "IV":     iv,
    }
    
    // 🔹 Створити KeyItem
    keyItem, err := NewKeyItem(formatAttributes(attrs))
    if err != nil {
        return fmt.Errorf("failed to create KeyItem: %w", err)
    }
    
    // 🔹 Додати тег у плейлист (з префіксом #)
    playlist.WriteString("#" + keyItem.String() + "\n")
    return nil
}

func formatAttributes(attrs map[string]string) string {
    var parts []string
    for k, v := range attrs {
        // 🔹 Додати лапки для рядкових значень
        if strings.ContainsAny(v, " ,\"") {
            parts = append(parts, fmt.Sprintf(`%s="%s"`, k, v))
        } else {
            parts = append(parts, fmt.Sprintf("%s=%s", k, v))
        }
    }
    return strings.Join(parts, ",")
}
```

### ✅ 2: Валідація `KeyItem` перед використанням

```go
// Додати метод валідації до KeyItem:
func (ki *KeyItem) Validate() error {
    if ki.Encryptable == nil {
        return fmt.Errorf("Encryptable is nil")
    }
    
    // 🔹 METHOD обов'язковий
    if ki.Encryptable.Method == "" {
        return fmt.Errorf("METHOD attribute is required")
    }
    
    // 🔹 Якщо не NONE — потрібен URI
    if ki.Encryptable.Method != "NONE" {
        if ki.Encryptable.URI == nil || *ki.Encryptable.URI == "" {
            return fmt.Errorf("URI required for METHOD=%s", ki.Encryptable.Method)
        }
        // 🔹 Перевірити формат URL
        if _, err := url.Parse(*ki.Encryptable.URI); err != nil {
            return fmt.Errorf("invalid URI format: %w", err)
        }
    }
    
    // 🔹 IV: 32 hex-символи (16 байт) + опціональний "0x"
    if ki.Encryptable.IV != nil {
        iv := strings.TrimPrefix(*ki.Encryptable.IV, "0x")
        if len(iv) != 32 {
            return fmt.Errorf("IV must be 16 bytes (32 hex chars), got %d", len(iv))
        }
        if _, err := hex.DecodeString(iv); err != nil {
            return fmt.Errorf("invalid hex in IV: %w", err)
        }
    }
    
    return nil
}
```

### ✅ 3: Підтримка ротації ключів

```go
// Для підвищення безпеки — періодична зміна ключів:
type KeyRotationPolicy struct {
    KeyURLPattern   string  // "https://keys.example.com/key-{index}.bin"
    IVGenerator     func(int) string  // Генерація IV для індексу
    KeyFormat       *string
    SegmentInterval int  // Змінювати ключ кожні N сегментів
}

func generateRotatedKeys(policy KeyRotationPolicy, totalSegments int) ([]*KeyItem, error) {
    var keys []*KeyItem
    
    for i := 0; i < totalSegments; i += policy.SegmentInterval {
        // 🔹 Підставити індекс у URL
        keyURL := strings.Replace(policy.KeyURLPattern, "{index}", strconv.Itoa(i), 1)
        
        // 🔹 Згенерувати унікальний IV
        iv := policy.IVGenerator(i)
        
        // 🔹 Створити KeyItem
        attrs := map[string]string{
            "METHOD": "AES-128",
            "URI":    keyURL,
            "IV":     iv,
        }
        if policy.KeyFormat != nil {
            attrs["KEY-FORMAT"] = *policy.KeyFormat
        }
        
        keyItem, err := NewKeyItem(formatAttributes(attrs))
        if err != nil {
            return nil, fmt.Errorf("failed to create key for segment %d: %w", i, err)
        }
        
        // 🔹 Валідація
        if err := keyItem.Validate(); err != nil {
            return nil, fmt.Errorf("invalid key for segment %d: %w", i, err)
        }
        
        keys = append(keys, keyItem)
    }
    
    return keys, nil
}
```

### ✅ 4: Моніторинг використання ключів шифрування

```go
// monitoring.Monitor — метрики для KeyItem:
type KeyMetrics struct {
    KeysGenerated   *prometheus.CounterVec  // кількість згенерованих ключів
    KeyMethods      *prometheus.CounterVec  // розподіл за METHOD
    KeyFormats      *prometheus.CounterVec  // розподіл за KEY-FORMAT
    ValidationErrors *prometheus.CounterVec  // помилки валідації
}

// У процесі створення ключа:
func monitorKeyCreation(channelID string, ki *KeyItem, metrics *KeyMetrics, err error) {
    if err != nil {
        metrics.ValidationErrors.WithLabelValues(channelID).Inc()
        log.Warnf("Channel %s: invalid KeyItem: %v", channelID, err)
        return
    }
    
    metrics.KeysGenerated.WithLabelValues(channelID).Inc()
    metrics.KeyMethods.WithLabelValues(channelID, ki.Encryptable.Method).Inc()
    
    if ki.Encryptable.KeyFormat != nil {
        metrics.KeyFormats.WithLabelValues(channelID, *ki.Encryptable.KeyFormat).Inc()
    }
}
```

### ✅ 5: Обробка помилок отримання ключа

```go
// Стратегія: retry при тимчасових помилках завантаження ключа
func fetchKeyWithRetry(keyURL string, maxRetries int) ([]byte, error) {
    var lastErr error
    for attempt := 0; attempt < maxRetries; attempt++ {
        resp, err := http.Get(keyURL)
        if err == nil && resp.StatusCode == http.StatusOK {
            defer resp.Body.Close()
            return io.ReadAll(resp.Body)
        }
        
        lastErr = err
        if resp != nil {
            lastErr = fmt.Errorf("HTTP %d: %w", resp.StatusCode, err)
        }
        
        // 🔹 Не retry для 4xx помилок (клієнтська помилка)
        if resp != nil && resp.StatusCode >= 400 && resp.StatusCode < 500 {
            break
        }
        
        // 🔹 Експоненційна затримка
        delay := time.Duration(1<<uint(attempt)) * time.Second
        log.Warnf("Retry %d/%d for key %s in %v: %v", 
            attempt+1, maxRetries, keyURL, delay, lastErr)
        time.Sleep(delay)
    }
    
    return nil, fmt.Errorf("failed to fetch key after %d attempts: %w", maxRetries, lastErr)
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на парсинг базового #EXT-X-KEY

```go
func TestNewKeyItem_Basic(t *testing.T) {
    input := `METHOD=AES-128,URI="https://keys.example.com/key.bin",IV=0x1234567890abcdef1234567890abcdef`
    
    ki, err := NewKeyItem(input)
    assert.NoError(t, err)
    assert.NotNil(t, ki)
    assert.NotNil(t, ki.Encryptable)
    
    assert.Equal(t, "AES-128", ki.Encryptable.Method)
    assert.NotNil(t, ki.Encryptable.URI)
    assert.Equal(t, "https://keys.example.com/key.bin", *ki.Encryptable.URI)
    assert.NotNil(t, ki.Encryptable.IV)
    assert.Equal(t, "0x1234567890abcdef1234567890abcdef", *ki.Encryptable.IV)
}
```

### 🔹 Тест на серіалізацію

```go
func TestKeyItem_String(t *testing.T) {
    ki := &KeyItem{
        Encryptable: &Encryptable{
            Method: "AES-128",
            URI:    stringPtr("https://keys.example.com/key.bin"),
            IV:     stringPtr("0x1234"),
        },
    }
    
    result := ki.String()
    
    // 🔹 Перевірити префікс тега
    assert.Contains(t, result, "EXT-X-KEY:")
    
    // 🔹 Перевірити атрибути
    assert.Contains(t, result, "METHOD=AES-128")
    assert.Contains(t, result, `URI="https://keys.example.com/key.bin"`)
    assert.Contains(t, result, "IV=0x1234")
    
    // 🔹 Перевірити роздільник
    assert.Contains(t, result, ",")
}
```

### 🔹 Тест на валідацію

```go
func TestKeyItem_Validate(t *testing.T) {
    // 🔹 Валідна конфігурація
    valid := &KeyItem{
        Encryptable: &Encryptable{
            Method: "AES-128",
            URI:    stringPtr("https://keys.example.com/key.bin"),
            IV:     stringPtr("0x1234567890abcdef1234567890abcdef"),
        },
    }
    assert.NoError(t, valid.Validate())
    
    // 🔹 Відсутній METHOD
    invalid1 := &KeyItem{Encryptable: &Encryptable{}}
    assert.Error(t, invalid1.Validate())
    
    // 🔹 METHOD ≠ NONE, але немає URI
    invalid2 := &KeyItem{
        Encryptable: &Encryptable{Method: "AES-128"},
    }
    assert.Error(t, invalid2.Validate())
    
    // 🔹 Невалідний IV
    invalid3 := &KeyItem{
        Encryptable: &Encryptable{
            Method: "AES-128",
            URI:    stringPtr("https://keys.example.com/key.bin"),
            IV:     stringPtr("0x123"),  // 🔹 Замало символів
        },
    }
    assert.Error(t, invalid3.Validate())
}
```

### 🔹 Тест на ротацію ключів

```go
func TestGenerateRotatedKeys(t *testing.T) {
    policy := KeyRotationPolicy{
        KeyURLPattern:   "https://keys.example.com/key-{index}.bin",
        IVGenerator:     func(i int) string { return fmt.Sprintf("0x%032d", i) },
        SegmentInterval: 10,
    }
    
    keys, err := generateRotatedKeys(policy, 25)
    assert.NoError(t, err)
    assert.Len(t, keys, 3)  // 🔹 0-9, 10-19, 20-24 → 3 ключі
    
    // 🔹 Перевірити перший ключ
    assert.Contains(t, *keys[0].Encryptable.URI, "key-0.bin")
    assert.Equal(t, "0x00000000000000000000000000000000", *keys[0].Encryptable.IV)
    
    // 🔹 Перевірити останній ключ
    assert.Contains(t, *keys[2].Encryptable.URI, "key-20.bin")
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Відсутній `#` префікс у `String()` | Плеєр не розпізнає тег | 🔹 Додати `#` у методі або документувати необхідність додавання клієнтом |
| Відсутня валідація обов'язкових полів | Невалідні плейлисти проходять парсинг | 🔹 Додати `Validate()` метод та використовувати його після `NewKeyItem` |
| Неправильний формат `IV` | Помилки декодування у плеєрі | 🔹 Додати валідацію: 32 hex-символи, опціональний "0x" префікс |
| `nil` `Encryptable` у `String()` | Паніка при `ki.Encryptable.String()` | 🔹 Додати перевірку: `if ki.Encryptable == nil { return "" }` |
| Необроблені помилки парсингу атрибутів | Тихе ігнорування невалідних атрибутів | 🔹 Логувати попередження при невідомих атрибутах у `ParseAttributes` |

### Приклад покращеного `String()` з перевіркою:

```go
func (ki *KeyItem) String() string {
    if ki.Encryptable == nil {
        // 🔹 Безпечний fallback: порожній рядок або логування
        log.Warn("KeyItem.Encryptable is nil")
        return ""
    }
    // 🔹 Додати # префікс для сумісності з форматом плейлиста
    return fmt.Sprintf("#%s:%v", KeyItemTag, ki.Encryptable.String())
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базове створення ключа:
func makeKeyItem(method, uri, iv string) (*KeyItem, error) {
    attrs := map[string]string{
        "METHOD": method,
        "URI":    uri,
        "IV":     iv,
    }
    return NewKeyItem(formatAttributes(attrs))
}

// 2: Створення ключа з валідацією:
func makeValidatedKeyItem(method, uri, iv string) (*KeyItem, error) {
    ki, err := makeKeyItem(method, uri, iv)
    if err != nil {
        return nil, err
    }
    if err := ki.Validate(); err != nil {
        return nil, fmt.Errorf("invalid key: %w", err)
    }
    return ki, nil
}

// 3: Додавання ключа у плейлист:
func writeKeyTag(w io.Writer, ki *KeyItem) error {
    // 🔹 Ki.String() вже включає # префікс (якщо виправлено)
    _, err := w.WriteString(ki.String() + "\n")
    return err
}

// 4: Логування для відладки:
func logKeyItem(ki *KeyItem) {
    if ki == nil || ki.Encryptable == nil {
        log.Debug("KeyItem is nil")
        return
    }
    log.Debugf("KeyItem: METHOD=%s, URI=%s, IV=%s",
        ki.Encryptable.Method,
        deref(ki.Encryptable.URI),
        deref(ki.Encryptable.IV))
}

// 5: Порівняння ключів (для детекції змін):
func keysEqual(a, b *KeyItem) bool {
    if a == nil || b == nil || a.Encryptable == nil || b.Encryptable == nil {
        return a == b  // 🔹 Обидва nil = рівні
    }
    return a.Encryptable.Method == b.Encryptable.Method &&
           deref(a.Encryptable.URI) == deref(b.Encryptable.URI) &&
           deref(a.Encryptable.IV) == deref(b.Encryptable.IV)
}

func deref(s *string) string { if s != nil { return *s }; return "" }
```

---

## 📊 Матриця атрибутів #EXT-X-KEY

```
Атрибут          | Тип       | Обов'язковий? | Приклад значення       | Призначення
─────────────────┼───────────┼───────────────┼────────────────────────┼─────────────────────────
METHOD           | string    | ✅ Так        | "AES-128", "NONE"      | 🔹 Алгоритм шифрування
URI              | string    | ⚠️ Якщо ≠ NONE| "https://keys.../key.bin" | 🔹 URL для отримання ключа
IV               | string    | ❌ Ні         | "0x1234...abcdef"      | 🔹 Вектор ініціалізації (16 байт)
KEY-FORMAT       | string    | ❌ Ні         | "identity", "com.apple.streaming" | 🔹 Ідентифікатор системи ключів
KEY-FORMAT-VERSIONS | string | ❌ Ні         | "1", "1/2"             | 🔹 Підтримувані версії формату
```

---

## 📚 Корисні посилання

- [HLS RFC Draft: EXT-X-KEY](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.2.4)
- [AES-128 encryption in HLS](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [FairPlay Streaming Server SDK](https://developer.apple.com/documentation/fairplay_streaming)
- [Go crypto/aes package](https://pkg.go.dev/crypto/aes)

> 💡 **Ключова ідея**: `KeyItem` — це "ключ до безпеки" у вашому HLS пайплайні. Він:
> - 🎯 Забезпечує типобезпечне представлення #EXT-X-KEY тегів
> - 🔧 Використовує універсальну `Encryptable` для гнучкості та повторного використання
> - ⚡ Готовий до розширення: нові атрибути шифрування не ламають існуючий код
> - 🛡️ Вимагає явної валідації для запобігання невалідним плейлистам

Якщо потрібно — можу допомогти:
- 🔄 Додати метод `Validate()` до `KeyItem` для автоматичної перевірки обов'язкових атрибутів
- 🧪 Написати integration-тест для перевірки коректності шифрування у реальному плеєрі
- 📈 Додати Prometheus-метрики для моніторингу використання ключів шифрування та помилок валідації

🛠️