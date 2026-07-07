# 🔍 Глибокий розбір коду: `PlaybackStart` для HLS M3U8

Цей код реалізує роботу з тегом **`#EXT-X-START`** — директивою, яка вказує плеєру **з якої позиції почати відтворення** при завантаженні плейлиста. Розберемо детально.

---

## 📦 Що таке `#EXT-X-START` і навіщо він потрібен?

### Контекст: Live та VOD плейлисти
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:1500
#EXT-X-START:TIME-OFFSET=-30.0,PRECISE=YES

#EXTINF:4.0,
segment1500.ts
#EXTINF:4.0,
segment1501.ts
#EXTINF:4.0,
segment1502.ts
```

### Призначення `#EXT-X-START`
| Атрибут | Тип | Призначення | Приклад |
|---------|-----|-------------|---------|
| `TIME-OFFSET` | **float64** (обов'язковий) | Зміщення часу від початку плейлиста | `-30.0` = 30 секунд від кінця (live), `120.5` = з 2:00.5 (VOD) |
| `PRECISE` | `*bool` (опціональний) | Чи шукати точний ключовий кадр | `YES` = чекати на найближчий keyframe, `NO` = почати з будь-якого фрейму |

### Сценарії використання
```
🔴 Live-стрім (CCTV):
#EXT-X-START:TIME-OFFSET=-10.0
→ Клієнт підключається і бачить події останніх 10 секунд
→ Ідеально для моніторингу в реальному часі

🎬 VOD (архів):
#EXT-X-START:TIME-OFFSET=300.0,PRECISE=YES
→ Відтворення починається з 5:00 хвилини
→ PRECISE=YES гарантує початок з ключового кадру (без артефактів)

🔄 Перепідключення після розриву:
#EXT-X-START:TIME-OFFSET=0.0
→ Почати з самого початку доступного вікна (перший сегмент)
```

---

## 🏗️ Структура коду: детальний аналіз

### 1️⃣ Struct `PlaybackStart`
```go
type PlaybackStart struct {
    TimeOffset float64  // Зміщення в секундах: від'ємне = від кінця, додатне = від початку
    Precise    *bool    // Опціональний прапорець точності: nil = не вказано
}
```

**🎯 Чому `*bool` для `Precise`?**
```go
// Семантична різниця у специфікації HLS:
// • Precise=nil  → атрибут не виводиться → плеєр вирішує сам (за замовчуванням NO)
// • Precise=&false → виводиться "PRECISE=NO" → явно вказуємо: не чекати keyframe
// • Precise=&true  → виводиться "PRECISE=YES" → чекати на найближчий ключовий кадр

// Це критично для:
// ✓ Синхронізації аудіо/відео при старті
// ✓ Уникнення артефактів декодування (початок не з keyframe)
// ✓ Low-latency режимів (PRECISE=NO дозволяє швидший старт)
```

### 2️⃣ Конструктор `NewPlaybackStart` — парсинг з валідацією
```go
func NewPlaybackStart(text string) (*PlaybackStart, error) {
    // Крок 1: Парсинг атрибутів
    // Вхід: 'TIME-OFFSET=-30.0,PRECISE=YES'
    // Вихід: map[string]string{"TIME-OFFSET": "-30.0", "PRECISE": "YES"}
    attributes := ParseAttributes(text)

    // Крок 2: Парсинг TIME-OFFSET (обов'язковий, float64)
    timeOffset, err := strconv.ParseFloat(attributes[TimeOffsetTag], 64)
    if err != nil {
        return nil, err  // Помилка: невірний формат числа
    }

    // Крок 3: Парсинг PRECISE (опціональний, YES/NO → *bool)
    return &PlaybackStart{
        TimeOffset: timeOffset,
        Precise:    parseYesNo(attributes, PreciseTag),  // helper: "YES"→&true, "NO"→&false, ""→nil
    }, nil
}
```

**🔍 Helper `parseYesNo` (припустима реалізація):**
```go
func parseYesNo(attrs map[string]string, key string) *bool {
    v, ok := attrs[key]
    if !ok || v == "" {
        return nil  // Атрибут відсутній
    }
    b := strings.ToUpper(v) == "YES"
    return &b
}
```

### 3️⃣ Метод `String()` — серіалізація зі збереженням семантики
```go
func (ps *PlaybackStart) String() string {
    // TIME-OFFSET завжди виводиться (обов'язковий атрибут)
    slice := []string{
        fmt.Sprintf(formatString, TimeOffsetTag, ps.TimeOffset),
        // Результат: "TIME-OFFSET=-30.000000" (формат залежить від formatString)
    }
    
    // PRECISE виводиться ТІЛЬКИ якщо не-nil
    if ps.Precise != nil {
        // formatYesNo конвертує bool → "YES"/"NO"
        slice = append(slice, fmt.Sprintf(formatString, PreciseTag, formatYesNo(*ps.Precise)))
    }
    
    // Фінальна збірка: #EXT-X-START:ATTR1=val1,ATTR2=val2
    return fmt.Sprintf(`%s:%s`, PlaybackStartTag, strings.Join(slice, ","))
}
```

**🎯 Формат виводу:**
```m3u8
#EXT-X-START:TIME-OFFSET=-30.000000
#EXT-X-START:TIME-OFFSET=120.500000,PRECISE=YES
```

> ⚠️ **Примітка**: `formatString` має забезпечити коректне форматування float (напр. `"%s=%.6f"`), щоб уникнути втрати точності.

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **live-відеопотоком** та **синхронізацією аудіо/відео**:

### Сценарій 1: Live-моніторинг з "ковзним вікном"
```go
// У generateMediaPlaylist для live-каналу
func generateLivePlaylist(channelID string, segments []Segment) string {
    var buf strings.Builder
    buf.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-PLAYLIST-TYPE:EVENT\n")
    
    // 🎯 Ключове: починати з останніх 10 секунд для мінімальної затримки
    playbackStart := &m3u8.PlaybackStart{
        TimeOffset: -10.0,  // Останні 10 секунд
        Precise:    pointer(false),  // Швидкий старт без очікування keyframe
    }
    buf.WriteString(playbackStart.String() + "\n")
    
    // ... додавання сегментів ...
    return buf.String()
}
```

### Сценарій 2: VOD-архів з точним початком
```go
// Для архівного запису (наприклад, подія за seqNum)
func generateArchivePlaylist(channelID string, startSeq int, segments []Segment) string {
    var buf strings.Builder
    buf.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-PLAYLIST-TYPE:VOD\n")
    
    // 🎯 Почати з першого сегмента події
    playbackStart := &m3u8.PlaybackStart{
        TimeOffset: 0.0,  // Початок плейлиста
        Precise:    pointer(true),  // Гарантувати початок з keyframe
    }
    buf.WriteString(playbackStart.String() + "\n")
    
    // ... додавання сегментів ...
    return buf.String()
}
```

### Сценарій 3: Динамічна корекція при розривах
```go
// У VideoManifestProxy при виявленні розриву >1с
func (p *VideoManifestProxy) handleDiscontinuity(gapSeconds float64) {
    // Якщо розрив великий — оновити START для коректного відновлення
    if gapSeconds > 1.0 {
        p.currentPlaylist.PlaybackStart = &m3u8.PlaybackStart{
            TimeOffset: 0.0,  // Почати з нового сегмента після розриву
            Precise:    pointer(true),  // Уникнути артефактів
        }
    }
}
```

---

## ⚠️ Критичні моменти та покращення

### 1️⃣ Валідація `TIME-OFFSET`
```go
// Потенційна проблема: код не перевіряє діапазон значень
func NewPlaybackStart(text string) (*PlaybackStart, error) {
    attributes := ParseAttributes(text)
    
    timeOffset, err := strconv.ParseFloat(attributes[TimeOffsetTag], 64)
    if err != nil {
        return nil, fmt.Errorf("invalid TIME-OFFSET: %w", err)
    }
    
    // ✅ Додати валідацію діапазону
    // Для live: від'ємні значення не повинні перевищувати довжину вікна
    // Для VOD: додатні значення не повинні перевищувати тривалість
    if timeOffset < -300.0 {  // Наприклад, максимум 5 хвилин у минуле
        return nil, fmt.Errorf("TIME-OFFSET too negative: %f", timeOffset)
    }
    
    return &PlaybackStart{
        TimeOffset: timeOffset,
        Precise:    parseYesNo(attributes, PreciseTag),
    }, nil
}
```

### 2️⃣ Форматування float для сумісності
```go
// ⚠️ Проблема: fmt.Sprintf("%f", -30.0) → "-30.000000" (6 знаків після коми)
// Деякі плеєри можуть бути чутливі до формату

// ✅ Рішення: контролювати точність через formatString
const formatString = "%s=%.1f"  // Один знак після коми достатньо для секунд

// Або динамічно:
func formatTimeOffset(offset float64) string {
    if offset == float64(int64(offset)) {
        return fmt.Sprintf("%.1f", offset)  // 30.0 замість 30.000000
    }
    return fmt.Sprintf("%.3f", offset)  // 30.125 для мілісекундної точності
}
```

### 3️⃣ Обробка спеціальних значень float
```go
// ⚠️ ParseFloat приймає NaN, Inf, -Inf — але вони невалідні для TIME-OFFSET
timeOffset, err := strconv.ParseFloat(attributes[TimeOffsetTag], 64)
if err != nil || math.IsNaN(timeOffset) || math.IsInf(timeOffset, 0) {
    return nil, fmt.Errorf("invalid TIME-OFFSET value")
}
```

### 4️⃣ Thread-safety при спільному доступі
```go
// Якщо PlaybackStart змінюється динамічно (напр. при рекалібруванні часу):
type SafePlaybackStart struct {
    mu sync.RWMutex
    PlaybackStart
}

func (s *SafePlaybackStart) Update(offset float64, precise *bool) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.TimeOffset = offset
    s.Precise = precise
}

func (s *SafePlaybackStart) String() string {
    s.mu.RLock()
    defer s.mu.RUnlock()
    return s.PlaybackStart.String()
}
```

---

## 🧪 Приклад використання

```go
// ✅ Створення для live-стріму
liveStart := &m3u8.PlaybackStart{
    TimeOffset: -15.0,  // Останні 15 секунд
    Precise:    pointer(false),  // Швидкий старт
}
fmt.Println(liveStart.String())
// #EXT-X-START:TIME-OFFSET=-15.000000

// ✅ Створення для VOD з точною позицією
vodStart := &m3u8.PlaybackStart{
    TimeOffset: 125.75,  // 2:05.75
    Precise:    pointer(true),  // Чекати keyframe
}
fmt.Println(vodStart.String())
// #EXT-X-START:TIME-OFFSET=125.750000,PRECISE=YES

// ✅ Парсинг вхідного рядка
line := `TIME-OFFSET=-30.0,PRECISE=YES`
ps, err := m3u8.NewPlaybackStart(line)
if err != nil {
    log.Fatal(err)
}
fmt.Println(ps.TimeOffset)  // -30.0
fmt.Println(*ps.Precise)    // true

// ✅ Тільки обов'язковий атрибут
minimal := `TIME-OFFSET=0.0`
ps2, _ := m3u8.NewPlaybackStart(minimal)
fmt.Println(ps2.String())
// #EXT-X-START:TIME-OFFSET=0.000000  (PRECISE не виводиться, бо nil)
```

---

## 📋 Специфікація HLS (RFC 8216) — ключові вимоги

```
✅ #EXT-X-START може з'являтися тільки ОДИН раз у плейлисті
✅ Має розташовуватися ПЕРЕД першим #EXTINF (але після заголовків)
✅ TIME-OFFSET:
   • Від'ємне значення: відлік від кінця плейлиста (для live)
   • Додатне значення: відлік від початку плейлиста (для VOD)
   • Нуль: почати з першого доступного сегмента
✅ PRECISE:
   • YES: плеєр має знайти найближчий ключовий кадр до/після TIME-OFFSET
   • NO (або відсутній): плеєр може почати з будь-якого фрейму (швидше, але можливі артефакти)
✅ Для TYPE=EVENT або TYPE=VOD плейлистів — особливо важливо для коректного старту
```

---

## 🎯 Висновок

Цей код — **мінімалістичний, але потужний** інструмент для керування точкою старту відтворення:

✅ Підтримує live та VOD сценарії  
✅ Дозволяє баланс між швидкістю старту (`PRECISE=NO`) та якістю (`PRECISE=YES`)  
✅ Чіткий контракт: парсинг ↔ серіалізація  
✅ Легко інтегрується у ваш pipeline генерації плейлистів

**Для вашого CCTV-процесора**:
```go
// Рекомендація для low-latency моніторингу:
playbackStart := &m3u8.PlaybackStart{
    TimeOffset: -5.0,        // Останні 5 секунд = мінімальна затримка
    Precise:    pointer(false),  // Не чекати keyframe → швидший старт
}
// Це дасть: #EXT-X-START:TIME-OFFSET=-5.0
// → Клієнт підключається і миттєво бачить "зараз"
```

**Увага**: переконайтеся, що ваші сегменти мають достатньо часті ключові кадри (напр. кожні 2-4 секунди), щоб `PRECISE=NO` не призводив до тривалих артефактів декодування.

Потрібно допомогти з:
- 🔄 Динамічним оновленням `TIME-OFFSET` на основі `PROGRAM-DATE-TIME`?
- 🎯 Інтеграцією з вашою математичною синхронізацією часу?
- 🧪 Тестами для крайніх випадків (NaN, дуже великі від'ємні значення)?

Чекаю на ваші питання! 🛠️