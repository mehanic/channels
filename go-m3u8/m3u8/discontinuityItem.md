# Глибоке роз'яснення: `m3u8.DiscontinuityItem` — представлення #EXT-X-DISCONTINUITY тегів для HLS

Цей файл містить **мінімалістичну реалізацію для #EXT-X-DISCONTINUITY тегів** — критичного маркера у HLS, що вказує на розрив у послідовності сегментів (зміна кодека, таймінгів, джерела). Це фундамент для підтримки live-стрімінгу, динамічної вставки контенту та обробки помилок.

---

## 🎯 Навіщо `DiscontinuityItem` потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ DiscontinuityItem у контексті HLS:     │
│                                         │
│ 🔹 Обробка розривів у потоці:          │
│   • Зміна джерела сигналу (камера →    │
│     резерв)                            │
│   • Перезапуск енкодера після збою     │
│   • Динамічна вставка реклами/подій    │
│                                         │
│ 🔹 Синхронізація таймінгів:            │
│   • Скидання PTS/DTS після розриву     │
│   • Запобігання десинхронізації аудіо/│
│     відео                              │
│   • Коректна обробка плеєром           │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Позначення моментів втрати сигналу│
│   • Інтеграція з системами моніторингу│
│   • Експорт логів для аналізу інцидентів│
└─────────────────────────────────────────┘
```

---

## 🔧 Структура `DiscontinuityItem`: чому вона порожня?

```go
type DiscontinuityItem struct{}
```

### 🎯 Чому немає полів?

```
#EXT-X-DISCONTINUITY — це **прапорець**, не контейнер даних:

• Не має атрибутів (на відміну від #EXT-X-DATERANGE)
• Просто маркер у плейлисті: "#EXT-X-DISCONTINUITY"
• Його наявність = сигнал плеєру: "тут розрив"

Аналогія:
• Це як <hr> у HTML — роздільник, не контейнер
• Або як "\n" у тексті — маркер нової секції

Тому struct порожній: немає даних для зберігання.
```

---

## 🔍 Функція `NewDiscontinuityItem`: конструктор

```go
func NewDiscontinuityItem() (*DiscontinuityItem, error) {
    return &DiscontinuityItem{}, nil
}
```

### 🎯 Чому повертає `error`, якщо ніколи не помиляється?

```
Причини:
✅ Консистентність з іншими "New*" функціями у пакеті
   • NewDateRangeItem() → може повернути помилку
   • NewDiscontinuityItem() → ніколи не повертає
   • Але однаковий сигнатурний контракт спрощує використання

✅ Майбутнє розширення
   • Якщо колись додадуть валідацію/парсинг — не потрібно міняти сигнатуру
   • Клієнтський код вже обробляє error

✅ Ідіоматичний Go
   • Функції, що створюють об'єкти, часто повертають (T, error)
   • Навіть якщо error завжди nil

Приклад використання:
  item, err := NewDiscontinuityItem()
  if err != nil {
      // ❌ Ніколи не виконається, але код коректний
      return err
  }
  // ✅ Використовуємо item
```

---

## 🔍 Метод `String()`: серіалізація у формат HLS

```go
func (di *DiscontinuityItem) String() string {
    return fmt.Sprintf("%s\n", DiscontinuityItemTag)
}
```

### 🎯 Що таке `DiscontinuityItemTag`?

```go
// Має бути оголошено десь у пакеті:
const DiscontinuityItemTag = "EXT-X-DISCONTINUITY"
```

### 🎯 Приклад виходу:

```
Виклик: (&DiscontinuityItem{}).String()
Вихід: "EXT-X-DISCONTINUITY\n"

У плейлисті:
  #EXTINF:6.000,
  segment0.ts
  #EXT-X-DISCONTINUITY
  #EXTINF:6.000,
  segment1.ts
```

### ⚠️ Потенційна проблема: зайвий `\n`

```go
return fmt.Sprintf("%s\n", DiscontinuityItemTag)
// Вихід: "EXT-X-DISCONTINUITY\n"
```

**Наслідок:**
```
• Якщо клієнтський код вже додає "\n" при записі → подвійний перенос рядка
• Може призвести до порожніх рядків у плейлисті

✅ Рішення: або прибрати "\n" тут, або документувати, що метод вже додає перенос
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Додавання розриву при зміні джерела сигналу

```go
// У VideoManifestProxy — позначення розриву при перемиканні камер:
func addDiscontinuityOnSourceSwitch(playlist *HLSPlaylist, oldSource, newSource string) {
    // 🔹 Додати #EXT-X-DISCONTINUITY перед новим сегментом
    discontinuity, _ := m3u8.NewDiscontinuityItem()
    playlist.AddTag(discontinuity.String())
    
    // 🔹 Опціонально: додати коментар для відладки
    playlist.AddComment(fmt.Sprintf("# Source switched: %s → %s", oldSource, newSource))
}
```

### ✅ 2: Обробка розривів при парсингі вхідного плейлиста

```go
// У segmentAssembler — реакція на #EXT-X-DISCONTINUITY:
func handleDiscontinuity(tag string, state *SegmentState) error {
    // 🔹 Скинути таймінги для нового сегмента
    state.ResetTimestamps()
    
    // 🔹 Збільшити лічильник розривів для моніторингу
    state.DiscontinuityCount++
    
    // 🔹 Логування для відладки
    log.Infof("Discontinuity detected at segment %d (total: %d)", 
        state.CurrentSegment, state.DiscontinuityCount)
    
    return nil
}
```

### ✅ 3: Валідація послідовності сегментів з урахуванням розривів

```go
// Перевірити, що таймінги коректні між сегментами:
func validateSegmentSequence(segments []SegmentItem) error {
    var lastPTS int64 = -1
    var discontinuityCount int = 0
    
    for i, seg := range segments {
        if seg.IsDiscontinuity {
            discontinuityCount++
            lastPTS = -1  // 🔹 Скинути після розриву
            continue
        }
        
        if lastPTS >= 0 {
            // 🔹 Перевірити монотонність PTS (з допуском)
            if seg.PTS < lastPTS-1000 {  // 1000 = допуск у таймбейзах
                return fmt.Errorf("segment %d: PTS regression after discontinuity %d", 
                    i, discontinuityCount)
            }
        }
        
        lastPTS = seg.PTS
    }
    
    return nil
}
```

### ✅ 4: Моніторинг розривів у потоці

```go
// monitoring.Monitor — метрики для discontinuity:
type DiscontinuityMetrics struct {
    DiscontinuitiesTotal *prometheus.CounterVec  // загальна кількість
    DiscontinuitiesByReason *prometheus.CounterVec  // за типом: "source_switch", "error"...
    AvgSegmentDuration *prometheus.HistogramVec  // для детекції аномалій
}

// У процесі обробки:
func monitorDiscontinuity(channelID string, reason string, 
                         metrics *DiscontinuityMetrics) {
    
    metrics.DiscontinuitiesTotal.WithLabelValues(channelID).Inc()
    metrics.DiscontinuitiesByReason.WithLabelValues(channelID, reason).Inc()
    
    log.Warnf("Channel %s: discontinuity detected (reason: %s)", channelID, reason)
}
```

### ✅ 5: Автоматичне вставлення розривів при помилках енкодингу

```go
// У segmentFinalizer — реакція на помилки:
func handleEncodingError(channelID string, err error, playlist *HLSPlaylist) {
    log.Errorf("Channel %s: encoding error: %v", channelID, err)
    
    // 🔹 Додати розрив перед наступним сегментом
    discontinuity, _ := m3u8.NewDiscontinuityItem()
    playlist.AddTag(discontinuity.String())
    
    // 🔹 Скинути стан енкодера
    resetEncoderState(channelID)
    
    // 🔹 Сповістити моніторинг
    monitorDiscontinuity(channelID, "encoding_error", metrics)
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на створення та серіалізацію

```go
func TestDiscontinuityItem_Basic(t *testing.T) {
    // 🔹 Створення
    item, err := NewDiscontinuityItem()
    assert.NoError(t, err)
    assert.NotNil(t, item)
    
    // 🔹 Серіалізація
    result := item.String()
    expected := "EXT-X-DISCONTINUITY\n"  // 🔹 Залежить від значення константи
    assert.Equal(t, expected, result)
}
```

### 🔹 Тест на використання у плейлисті

```go
func TestDiscontinuityItem_InPlaylist(t *testing.T) {
    var builder strings.Builder
    
    // 🔹 Імітація плейлиста з розривом
    builder.WriteString("#EXTM3U\n")
    builder.WriteString("#EXT-X-VERSION:3\n")
    builder.WriteString("#EXTINF:6.000,\nsegment0.ts\n")
    
    // 🔹 Додати розрив
    disc, _ := NewDiscontinuityItem()
    builder.WriteString(disc.String())
    
    builder.WriteString("#EXTINF:6.000,\nsegment1.ts\n")
    
    playlist := builder.String()
    
    // 🔹 Перевірити наявність тега
    assert.Contains(t, playlist, "#EXT-X-DISCONTINUITY")
    
    // 🔹 Перевірити, що сегменти розділені
    lines := strings.Split(playlist, "\n")
    var discIndex, seg0Index, seg1Index int
    for i, line := range lines {
        if line == "segment0.ts" { seg0Index = i }
        if line == "EXT-X-DISCONTINUITY" { discIndex = i }
        if line == "segment1.ts" { seg1Index = i }
    }
    
    assert.Less(t, seg0Index, discIndex)
    assert.Less(t, discIndex, seg1Index)
}
```

### 🔹 Тест на обробку помилки (майбутнє розширення)

```go
func TestNewDiscontinuityItem_ErrorHandling(t *testing.T) {
    // 🔹 Зараз завжди успіх, але тест готує код до майбутніх змін
    item, err := NewDiscontinuityItem()
    
    // ✅ Коректна обробка error, навіть якщо він завжди nil
    if err != nil {
        t.Fatalf("Unexpected error: %v", err)
    }
    if item == nil {
        t.Fatal("Expected non-nil item")
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Зайвий `\n` у `String()` | Подвійні порожні рядки у плейлисті | 🔹 Прибрати `\n` у методі або документувати поведінку |
| Відсутня валідація позиції розриву | #EXT-X-DISCONTINUITY між частинами одного сегмента | 🔹 Додати перевірку: розрив тільки між сегментами, не всередині |
| Не скидаються таймінги після розриву | Десинхронізація аудіо/відео у плеєрі | 🔹 Гарантувати, що `handleDiscontinuity` викликає `ResetTimestamps()` |
| Надмірне використання розривів | Плеєр показує "перезавантаження" при кожному сегменті | 🔹 Логувати частоту розривів; попереджати при >1/хвилину |
| Відсутній `DiscontinuityItemTag` у коді | Компіляційна помилка | 🔹 Перевірити, що константа оголошена: `const DiscontinuityItemTag = "EXT-X-DISCONTINUITY"` |

### Приклад валідації позиції розриву:

```go
func validateDiscontinuityPosition(playlist []string, discIndex int) error {
    // 🔹 Розрив має бути між #EXTINF тегами, не всередині сегмента
    if discIndex <= 0 || discIndex >= len(playlist)-1 {
        return fmt.Errorf("discontinuity at invalid position: %d", discIndex)
    }
    
    // 🔹 Попередній рядок має бути .ts файлом або іншим тегом
    prev := strings.TrimSpace(playlist[discIndex-1])
    if !strings.HasSuffix(prev, ".ts") && !strings.HasPrefix(prev, "#EXT-") {
        return fmt.Errorf("discontinuity not preceded by segment or tag: %q", prev)
    }
    
    return nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базове створення розриву:
func addDiscontinuity(playlist *strings.Builder) {
    disc, _ := m3u8.NewDiscontinuityItem()
    playlist.WriteString(disc.String())
}

// 2: Умовне додавання розриву:
func addDiscontinuityIfNeeded(playlist *strings.Builder, condition bool) {
    if condition {
        addDiscontinuity(playlist)
    }
}

// 3: Логування для відладки:
func logDiscontinuity(channelID, reason string) {
    log.Infof("Channel %s: adding EXT-X-DISCONTINUITY (reason: %s)", channelID, reason)
}

// 4: Підрахунок розривів у плейлисті:
func countDiscontinuities(playlistContent string) int {
    return strings.Count(playlistContent, "#EXT-X-DISCONTINUITY")
}

// 5: Перевірка наявності розривів перед обробкою:
func hasDiscontinuities(playlistPath string) (bool, error) {
    content, err := os.ReadFile(playlistPath)
    if err != nil {
        return false, err
    }
    return strings.Contains(string(content), "#EXT-X-DISCONTINUITY"), nil
}
```

---

## 📊 Матриця використання #EXT-X-DISCONTINUITY

```
Сценарій                     | Коли додавати розрив          | Наслідок для плеєра
─────────────────────────────┼───────────────────────────────┼─────────────────────────
Зміна джерела сигналу       | ✅ Перед першим сегментом нового джерела | 🔹 Плеєр скидає буфери, синхронізує таймінги
Перезапуск енкодера         | ✅ Після останнього сегмента старого сесії | 🔹 Уникає "залипання" на старих таймінгах
Динамічна вставка реклами   | ✅ Перед та після рекламного блоку | 🔹 Чітке відокремлення контенту/реклами
Помилка мережі/енкодингу    | ✅ Після пропущених сегментів  | 🔹 Плеєр не намагається "підтягнути" пропущене
Зміна кодека/роздільної здатності | ✅ Перед сегментом з новими параметрами | 🔹 Плеєр перезавантажує декодер
```

---

## 📚 Корисні посилання

- [HLS RFC Draft: EXT-X-DISCONTINUITY](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.2.3)
- [Apple HLS Authoring Specification: Discontinuities](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)
- [Understanding HLS discontinuities](https://www.brightcove.com/en/blog/2019/03/understanding-hls-discontinuities)
- [Go fmt package best practices](https://pkg.go.dev/fmt)

> 💡 **Ключова ідея**: `DiscontinuityItem` — це "роздільник секцій" у вашому HLS пайплайні. Він:
> - 🎯 Чітко позначає моменти розриву у послідовності сегментів
> - 🔧 Забезпечує коректну реакцію плеєрів на зміни у потоці
> - ⚡ Має мінімалістичну реалізацію, бо не потребує даних — тільки наявність
> - 🛡️ Готовий до розширення через консистентну сигнатуру (T, error)

Якщо потрібно — можу допомогти:
- 🔄 Додати валідацію позиції розриву у плейлисті (тільки між сегментами)
- 🧪 Написати integration-тест для перевірки коректної обробки розривів у реальному плеєрі (hls.js, AVPlayer)
- 📈 Додати Prometheus-метрики для моніторингу частоти та причин розривів по каналах

🛠️