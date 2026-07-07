# Глибоке роз'яснення: `m3u8` помилки — централізоване визначення помилок для HLS плейлистів

Цей файл містить **набір попередньо визначених помилок** для валідації та обробки HLS плейлистів. Це критичний компонент для надійності пайплайну: чіткі, типобезпечні помилки дозволяють клієнтському коду точно реагувати на різні сценарії збоїв.

---

## 🎯 Навіщо централізовані помилки потрібні у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ Помилки m3u8 у контексті HLS:          │
│                                         │
│ 🔹 Чітка класифікація проблем:         │
│   • Розрізнення "невалідний плейлист"  │
│     від "відсутній бітрейт"            │
│   • Точна реакція на різні типи збоїв  │
│                                         │
│ 🔹 Безпечна обробка помилок:           │
│   • Порівняння через == замість        │
│     strings.Contains                   │
│   • Типобезпечна логіка retry/fallback │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Швидка діагностика проблем у       │
│     реальному часі                     │
│   • Автоматичне відновлення після      │
│     тимчасових збоїв                   │
│   • Аудит помилок для покращення       │
│     якості пайплайну                   │
└─────────────────────────────────────────┘
```

---

## 🔧 Перелік помилок: опис та призначення

### 🎯 `ErrPlaylistInvalid`: базова валідація формату

```go
ErrPlaylistInvalid = errors.New("invalid playlist, must start with #EXTM3U")
```

**Коли виникає:**
```
• Вхідний файл не починається з "#EXTM3U"
• Порожній файл або файл з іншим форматом
• Пошкоджені дані при мережевому завантаженні

Приклад коду:
  if !strings.HasPrefix(content, "#EXTM3U") {
      return nil, ErrPlaylistInvalid
  }
```

**Рекомендована реакція:**
```
❌ Не retry — це фундаментальна помилка формату
✅ Логувати джерело даних для відладки
✅ Повернути клієнту 400 Bad Request або 415 Unsupported Media Type
```

---

### 🎯 `ErrPlaylistInvalidType`: змішування типів плейлистів

```go
ErrPlaylistInvalidType = errors.New("invalid playlist, mixed master and media")
```

**Коли виникає:**
```
• Мастер-плейлист містить #EXTINF теги (має містити тільки #EXT-X-STREAM-INF)
• Медіа-плейлист містить #EXT-X-STREAM-INF теги (має містити тільки #EXTINF)
• Невірне вкладення плейлистів

Приклад:
  # ❌ Неправильно: медіа-плейлист з посиланнями на інші плейлисти
  #EXTM3U
  #EXT-X-STREAM-INF:BANDWIDTH=1000000
  other_playlist.m3u8  # ← Це має бути тільки у мастер-плейлисті!
```

**Рекомендована реакція:**
```
❌ Не retry — це логічна помилка структури
✅ Перевірити джерело генерації плейлиста
✅ Додати валідацію типу плейлиста перед обробкою
```

---

### 🎯 `ErrResolutionInvalid`: валідація роздільної здатності

```go
ErrResolutionInvalid = errors.New("invalid resolution")
```

**Коли виникає:**
```
• RESOLUTION атрибут має невірний формат: "1280" замість "1280x720"
• Нечислові значення: "HD" замість "1920x1080"
• Від'ємні або нульові розміри: "0x0", "-1x-1"

Приклад парсингу:
  func parseResolution(s string) (width, height int, err error) {
      parts := strings.Split(s, "x")
      if len(parts) != 2 {
          return 0, 0, ErrResolutionInvalid
      }
      width, err1 := strconv.Atoi(parts[0])
      height, err2 := strconv.Atoi(parts[1])
      if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
          return 0, 0, ErrResolutionInvalid
      }
      return width, height, nil
  }
```

**Рекомендована реакція:**
```
⚠️ Можна спробувати fallback на дефолтну роздільну здатність
✅ Логувати невалідне значення для відладки
✅ Для production: використовувати валідацію при генерації, не при парсингу
```

---

### 🎯 `ErrBandwidthMissing` / `ErrBandwidthInvalid`: критичні атрибути ABR

```go
ErrBandwidthMissing = errors.New("missing bandwidth")
ErrBandwidthInvalid = errors.New("invalid bandwidth")
```

**Коли виникають:**
```
• BANDWIDTH атрибут відсутній у #EXT-X-STREAM-INF (обов'язковий!)
• Нечислове значення: "high" замість "2000000"
• Від'ємне або нульове значення: "0", "-1000"

Приклад:
  # ❌ Неправильно: відсутній BANDWIDTH
  #EXT-X-STREAM-INF:RESOLUTION=1280x720
  stream_720p.m3u8
  
  # ✅ Правильно
  #EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720
  stream_720p.m3u8
```

**Рекомендована реакція:**
```
❌ Не retry для ErrBandwidthMissing — це порушення специфікації
⚠️ Для ErrBandwidthInvalid: спробувати виправити (напр., видалити нечислові символи)
✅ Завжди валідувати BANDWIDTH при генерації плейлистів
```

---

### 🎯 `ErrSegmentItemInvalid` / `ErrPlaylistItemInvalid`: валідація елементів

```go
ErrSegmentItemInvalid = errors.New("invalid segment item")
ErrPlaylistItemInvalid = errors.New("invalid playlist item")
```

**Коли виникають:**
```
• Сегмент має невалідний URI: порожній рядок, відносний шлях без базового URL
• #EXTINF має нечислову тривалість: "#EXTINF:abc,segment.ts"
• Відсутній обов'язковий атрибут у елементі плейлиста

Приклад:
  # ❌ Неправильно: порожній URI сегмента
  #EXTINF:6.000,
  
  # ✅ Правильно
  #EXTINF:6.000,segment_001.ts
```

**Рекомендована реакція:**
```
⚠️ Пропустити невалідний елемент, продовжити обробку решти
✅ Логувати номер рядка/сегмента для відладки
✅ Додати статистику помилок для моніторингу якості
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Типобезпечна обробка помилок

```go
// Замість рядкових порівнянь:
func handlePlaylistError(err error) {
    // ❌ Ненадійно: чутливе до змін тексту помилки
    if strings.Contains(err.Error(), "invalid playlist") {
        return BadRequest()
    }
    
    // ✅ Надійно: порівняння через ==
    switch err {
    case m3u8.ErrPlaylistInvalid:
        log.Error("Playlist format error")
        return BadRequest()
        
    case m3u8.ErrBandwidthMissing:
        log.Error("Missing BANDWIDTH attribute")
        return ValidationError()
        
    case m3u8.ErrResolutionInvalid:
        log.Warn("Invalid resolution, using fallback")
        return UseDefaultResolution()
        
    default:
        // 🔹 Для невідомих помилок — логувати та retry
        log.Errorf("Unexpected error: %v", err)
        return Retry()
    }
}
```

### ✅ 2: Валідація плейлиста з чіткими помилками

```go
func validatePlaylist(content string) error {
    // 🔹 Перевірка заголовка
    if !strings.HasPrefix(content, "#EXTM3U") {
        return m3u8.ErrPlaylistInvalid
    }
    
    // 🔹 Визначення типу плейлиста
    isMaster := strings.Contains(content, "#EXT-X-STREAM-INF")
    isMedia := strings.Contains(content, "#EXTINF")
    
    if isMaster && isMedia {
        return m3u8.ErrPlaylistInvalidType
    }
    
    // 🔹 Подальша валідація залежно від типу...
    return nil
}
```

### ✅ 3: Моніторинг помилок для проактивного виявлення проблем

```go
// monitoring.Monitor — метрики для помилок плейлистів:
type PlaylistErrorMetrics struct {
    ErrorsTotal       *prometheus.CounterVec  // загальна кількість помилок
    ErrorsByType      *prometheus.CounterVec  // розподіл за типом помилки
    RecoveryAttempts  *prometheus.CounterVec  // спроби відновлення
    FatalErrors       *prometheus.CounterVec  // помилки, що зупиняють обробку
}

// У процесі обробки:
func monitorPlaylistError(channelID string, err error, metrics *PlaylistErrorMetrics) {
    metrics.ErrorsTotal.WithLabelValues(channelID).Inc()
    
    switch err {
    case m3u8.ErrPlaylistInvalid:
        metrics.ErrorsByType.WithLabelValues(channelID, "invalid_format").Inc()
        metrics.FatalErrors.WithLabelValues(channelID).Inc()
        
    case m3u8.ErrBandwidthMissing:
        metrics.ErrorsByType.WithLabelValues(channelID, "missing_bandwidth").Inc()
        // 🔹 Це може бути тимчасова помилка → спроба відновлення
        metrics.RecoveryAttempts.WithLabelValues(channelID).Inc()
        
    case m3u8.ErrResolutionInvalid:
        metrics.ErrorsByType.WithLabelValues(channelID, "invalid_resolution").Inc()
        // 🔹 Можна використати fallback
        metrics.RecoveryAttempts.WithLabelValues(channelID).Inc()
        
    default:
        metrics.ErrorsByType.WithLabelValues(channelID, "unknown").Inc()
    }
    
    log.Warnf("Channel %s: playlist error: %v", channelID, err)
}
```

### ✅ 4: Автоматичне відновлення після виправних помилок

```go
// Стратегія: retry тільки для "виправних" помилок
func processPlaylistWithRetry(channelID string, fetcher PlaylistFetcher, 
                           maxRetries int, metrics *PlaylistErrorMetrics) error {
    
    var lastErr error
    for attempt := 0; attempt < maxRetries; attempt++ {
        content, err := fetcher.Fetch(channelID)
        if err == nil {
            return validateAndProcess(content)
        }
        
        lastErr = err
        monitorPlaylistError(channelID, err, metrics)
        
        // 🔹 Визначити, чи варто retry
        if !isRecoverableError(err) {
            log.Errorf("Channel %s: unrecoverable error: %v", channelID, err)
            return err
        }
        
        // 🔹 Експоненційна затримка перед повтором
        delay := time.Duration(1<<uint(attempt)) * time.Second
        log.Infof("Channel %s: retrying in %v (attempt %d/%d)", 
            channelID, delay, attempt+1, maxRetries)
        time.Sleep(delay)
    }
    
    return fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func isRecoverableError(err error) bool {
    // 🔹 Невиправні помилки: фундаментальні проблеми формату
    if err == m3u8.ErrPlaylistInvalid || err == m3u8.ErrPlaylistInvalidType {
        return false
    }
    
    // 🔹 Виправні помилки: тимчасові проблеми з даними
    if err == m3u8.ErrBandwidthMissing || 
       err == m3u8.ErrResolutionInvalid ||
       err == m3u8.ErrSegmentItemInvalid {
        return true
    }
    
    // 🔹 Для невідомих помилок — консервативно: не retry
    return false
}
```

### ✅ 5: Логування з контекстом для швидкої відладки

```go
// Структуроване логування помилок:
func logPlaylistError(ctx context.Context, channelID string, 
                     playlistURL string, err error) {
    
    logger := log.WithContext(ctx).
        With("channel_id", channelID).
        With("playlist_url", playlistURL).
        With("error_type", getErrorType(err))
    
    switch err {
    case m3u8.ErrPlaylistInvalid:
        logger.Error("Playlist does not start with #EXTM3U")
        
    case m3u8.ErrBandwidthMissing:
        logger.Warn("Segment missing BANDWIDTH attribute")
        
    case m3u8.ErrResolutionInvalid:
        logger.Warn("Invalid RESOLUTION format")
        
    default:
        logger.Errorf("Unexpected playlist error: %v", err)
    }
}

func getErrorType(err error) string {
    switch err {
    case m3u8.ErrPlaylistInvalid:
        return "invalid_format"
    case m3u8.ErrPlaylistInvalidType:
        return "mixed_type"
    case m3u8.ErrBandwidthMissing:
        return "missing_bandwidth"
    case m3u8.ErrBandwidthInvalid:
        return "invalid_bandwidth"
    case m3u8.ErrResolutionInvalid:
        return "invalid_resolution"
    case m3u8.ErrSegmentItemInvalid:
        return "invalid_segment"
    case m3u8.ErrPlaylistItemInvalid:
        return "invalid_playlist_item"
    default:
        return "unknown"
    }
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на порівняння помилок

```go
func TestErrorComparison(t *testing.T) {
    // 🔹 Порівняння через == працює для errors.New
    err := m3u8.ErrPlaylistInvalid
    
    assert.True(t, err == m3u8.ErrPlaylistInvalid)
    assert.False(t, err == m3u8.ErrBandwidthMissing)
    
    // 🔹 Порівняння через errors.Is для сумісності з wrapped errors
    wrapped := fmt.Errorf("parsing failed: %w", m3u8.ErrPlaylistInvalid)
    assert.True(t, errors.Is(wrapped, m3u8.ErrPlaylistInvalid))
    assert.False(t, errors.Is(wrapped, m3u8.ErrBandwidthMissing))
}
```

### 🔹 Тест на обробку помилок у валідаторі

```go
func TestValidatePlaylist_Errors(t *testing.T) {
    testCases := []struct {
        name     string
        content  string
        expected error
    }{
        {
            "missing_extm3u",
            "#EXT-X-VERSION:3\n",
            m3u8.ErrPlaylistInvalid,
        },
        {
            "mixed_types",
            "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\n#EXTINF:6.0,seg.ts\n",
            m3u8.ErrPlaylistInvalidType,
        },
        {
            "valid_master",
            "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000000\nstream.m3u8\n",
            nil,
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            err := validatePlaylist(tc.content)
            if tc.expected == nil {
                assert.NoError(t, err)
            } else {
                assert.Equal(t, tc.expected, err)
            }
        })
    }
}
```

### 🔹 Тест на recoverable vs fatal помилки

```go
func TestIsRecoverableError(t *testing.T) {
    assert.False(t, isRecoverableError(m3u8.ErrPlaylistInvalid))
    assert.False(t, isRecoverableError(m3u8.ErrPlaylistInvalidType))
    
    assert.True(t, isRecoverableError(m3u8.ErrBandwidthMissing))
    assert.True(t, isRecoverableError(m3u8.ErrResolutionInvalid))
    assert.True(t, isRecoverableError(m3u8.ErrSegmentItemInvalid))
    
    // 🔹 Невідома помилка → не recoverable (консервативно)
    assert.False(t, isRecoverableError(errors.New("unknown error")))
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Порівняння через `strings.Contains` | Помилки не розпізнаються після змін тексту | 🔹 Завжди використовувати `==` або `errors.Is()` для порівняння |
| Відсутність контексту у помилці | Важко відлагоджувати в production | 🔹 Додати `fmt.Errorf("context: %w", err)` при поверненні |
| Надмірне retry для fatal помилок | Зайве навантаження, затримки | 🔹 Чітко розділити recoverable/fatal помилки у `isRecoverableError` |
| Невідповідність помилок специфікації | Плеєри відкидають плейлисти | 🔹 Додати валідацію відповідності специфікації при генерації |
| Відсутність метрик для помилок | Неможливо виявити тренди проблем | 🔹 Інтегрувати `monitorPlaylistError` у всі точки обробки |

### Приклад додавання контексту до помилок:

```go
func parseBandwidth(s string) (int, error) {
    value, err := strconv.Atoi(s)
    if err != nil {
        // 🔹 Додати контекст: яке значення не вдалося парсити
        return 0, fmt.Errorf("parsing BANDWIDTH=%q: %w", s, m3u8.ErrBandwidthInvalid)
    }
    if value <= 0 {
        return 0, fmt.Errorf("BANDWIDTH=%d must be positive: %w", value, m3u8.ErrBandwidthInvalid)
    }
    return value, nil
}

// Клієнтський код може витягнути оригінальну помилку:
err := parseBandwidth("abc")
if errors.Is(err, m3u8.ErrBandwidthInvalid) {
    // 🔹 Обробити як невалідний бітрейт
}
// Повне повідомлення: "parsing BANDWIDTH=\"abc\": invalid bandwidth"
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базова перевірка типу помилки:
func handleError(err error) {
    if err == m3u8.ErrPlaylistInvalid {
        // 🔹 Фатальна помилка формату
        return Fatal("Invalid playlist format")
    }
    if errors.Is(err, m3u8.ErrBandwidthMissing) {
        // 🔹 Спробувати відновити
        return RetryWithFallback()
    }
}

// 2: Логування з контекстом:
func logAndReturn(ctx context.Context, err error) error {
    log.WithContext(ctx).Errorf("Playlist error: %v", err)
    return err  // 🔹 Повертаємо оригінальну помилку для порівняння
}

// 3: Створення нової помилки з контекстом:
func wrapError(err error, context string) error {
    if err == nil {
        return nil
    }
    return fmt.Errorf("%s: %w", context, err)
}

// Використання:
// • return wrapError(m3u8.ErrBandwidthMissing, "segment #42")
// → "segment #42: missing bandwidth"

// 4: Перевірка на recoverable помилки:
func shouldRetry(err error) bool {
    return err != m3u8.ErrPlaylistInvalid && 
           err != m3u8.ErrPlaylistInvalidType
}

// 5: Статистика помилок для моніторингу:
type ErrorStats struct {
    Total       int
    ByType      map[error]int
    Recoverable int
}

func trackError(stats *ErrorStats, err error) {
    stats.Total++
    stats.ByType[err]++
    if shouldRetry(err) {
        stats.Recoverable++
    }
}
```

---

## 📊 Матриця помилок: recoverable vs fatal

```
Помилка                    | Тип     | Рекомендована дія        | Приклад реакції
───────────────────────────┼─────────┼──────────────────────────┼─────────────────────────
ErrPlaylistInvalid         | 🔴 Fatal| Не retry, логувати       | Повернути 400 Bad Request
ErrPlaylistInvalidType     | 🔴 Fatal| Не retry, перевірити джерело | Логувати для відладки генерації
ErrBandwidthMissing        | 🟡 Recoverable | Retry або fallback | Використати дефолтний бітрейт
ErrBandwidthInvalid        | 🟡 Recoverable | Спробувати виправити | Видалити нечислові символи
ErrResolutionInvalid       | 🟡 Recoverable | Fallback на дефолт | Використати 1280x720
ErrSegmentItemInvalid      | 🟡 Recoverable | Пропустити сегмент | Продовжити обробку решти
ErrPlaylistItemInvalid     | 🟡 Recoverable | Пропустити елемент | Логувати для аналізу
```

---

## 📚 Корисні посилання

- [Go errors package best practices](https://pkg.go.dev/errors)
- [Error handling patterns in Go](https://go.dev/blog/error-handling-and-go)
- [HLS RFC Draft: Error conditions](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis)
- [Prometheus error metrics guide](https://prometheus.io/docs/practices/instrumentation/#errors)

> 💡 **Ключова ідея**: Ці попередньо визначені помилки — це "мова спілкування" між компонентами вашого пайплайну. Вони:
> - 🎯 Дозволяють точно класифікувати проблеми без парсингу тексту помилок
> - 🔧 Забезпечують консистентну обробку через порівняння `==` або `errors.Is()`
> - ⚡ Прискорюють відладку через чіткі, само-документовані повідомлення
> - 🛡️ Запобігають каскадним збоям через розділення recoverable/fatal помилок

Якщо потрібно — можу допомогти:
- 🔄 Додати нові типи помилок для специфічних сценаріїв CCTV (напр., `ErrTimestampRegression`, `ErrSegmentGap`)
- 🧪 Написати integration-тест для перевірки коректної обробки помилок у реальному пайплайні
- 📈 Додати Prometheus-метрики для моніторингу розподілу помилок по каналах та типах

🛠️