# 🔍 Глибокий розбір коду: `Playlist` — ядро пакету m3u8

Цей код реалізує **центральну абстракцію** пакету — структуру `Playlist`, яка об'єднує всі окремі теги HLS (`#EXT-X-MAP`, `#EXT-X-MEDIA`, `#EXTINF` тощо) у єдиний документ. Розберемо архітектурно та детально.

---

## 📦 Архітектура пакету: поліморфізм через інтерфейс `Item`

```go
// Item — спільний контракт для всіх елементів плейлиста
type Item interface {
    String() string  // Серіалізація у рядок формату M3U8
}
```

### 🎯 Навіщо інтерфейс?
```
✅ Єдиний спосіб зберігати різнотипні теги в одному слайсі []Item
✅ Розширюваність: новий тип тега = новий struct + реалізація String()
✅ Clean code: логіка серіалізації інкапсульована в кожному типі

Приклад ієрархії:
Item (interface)
├─ SegmentItem      → #EXTINF:4.0,\nsegment0.ts
├─ MapItem          → #EXT-X-MAP:URI="init.mp4"
├─ MediaItem        → #EXT-X-MEDIA:TYPE=AUDIO,...
├─ PlaybackStart    → #EXT-X-START:TIME-OFFSET=-10.0
├─ Discontinuity    → #EXT-X-DISCONTINUITY
├─ ProgramDateTime  → #EXT-X-PROGRAM-DATE-TIME:2024-01-01T00:00:00Z
└─ ... інші теги
```

---

## 🏗️ Struct `Playlist` — повна карта стану HLS-плейлиста

```go
type Playlist struct {
    // 📋 Колекція елементів (поліморфний слайс)
    Items []Item
    
    // 🏷️ Мета-дані плейлиста (заголовки)
    Version               *int     // #EXT-X-VERSION:7 (nil = не вказано)
    Cache                 *bool    // #EXT-X-ALLOW-CACHE:YES/NO
    Type                  *string  // #EXT-X-PLAYLIST-TYPE:VOD|EVENT (nil = live)
    
    // ⏱️ Таймінги та послідовність
    Target                int      // #EXT-X-TARGETDURATION:10 (обов'язковий, секунд)
    Sequence              int      // #EXT-X-MEDIA-SEQUENCE:N (стартовий номер сегмента)
    DiscontinuitySequence *int     // #EXT-X-DISCONTINUITY-SEQUENCE:N
    
    // 🔄 Прапорці поведінки
    IFramesOnly           bool     // #EXT-X-I-FRAMES-ONLY:YES
    IndependentSegments   bool     // #EXT-X-INDEPENDENT-SEGMENTS
    Live                  bool     // Внутрішній прапорець: live vs VOD
    Master                *bool    // Внутрішній прапорець: master vs media playlist
}
```

### 🔍 Семантика полів у контексті HLS (RFC 8216)

| Поле | HLS-тег | Обов'язковий? | Примітки |
|------|---------|---------------|----------|
| `Target` | `#EXT-X-TARGETDURATION` | ✅ Так | Максимальна тривалість сегмента; має бути ≥ тривалості будь-якого `#EXTINF` |
| `Sequence` | `#EXT-X-MEDIA-SEQUENCE` | ✅ Для live | Номер першого сегмента в плейлисті; зростає при "ковзанні" вікна |
| `Version` | `#EXT-X-VERSION` | ⚠️ Рекомендовано | 7 = підтримка fMP4, BYTERANGE, дельта-оновлень |
| `Type` | `#EXT-X-PLAYLIST-TYPE` | ❌ Ні | `VOD` = весь контент доступний, `EVENT` = додаються тільки нові сегменти |
| `DiscontinuitySequence` | `#EXT-X-DISCONTINUITY-SEQUENCE` | ❌ Ні | Лічильник розривів; зростає при кожному `#EXT-X-DISCONTINUITY` |

### 🎯 Чому так багато `*int` / `*string` / `*bool`?
```go
// Специфікація HLS розрізняє:
// • Атрибут відсутній → nil → не виводити у серіалізації
// • Атрибут присутній зі значенням → виводити

// Приклад для Type:
// • Type=nil  → плейлист live (немає #EXT-X-PLAYLIST-TYPE)
// • Type=&"VOD" → виводиться #EXT-X-PLAYLIST-TYPE:VOD
// • Type=&"EVENT" → виводиться #EXT-X-PLAYLIST-TYPE:EVENT

// Це критично для сумісності з плеєрами!
```

---

## 🛠️ Конструктори та базові методи

### 1️⃣ `NewPlaylist()` — дефолтний live-плейлист
```go
func NewPlaylist() *Playlist {
    return &Playlist{
        Target: 10,  // Дефолт: сегменти ≤10 секунд (стандарт для low-latency)
        Live:   true, // За замовчуванням — live-режим
    }
}
```

### 2️⃣ `NewPlaylistWithItems()` — ініціалізація з елементами
```go
func NewPlaylistWithItems(items []Item) *Playlist {
    return &Playlist{
        Target: 10,
        Items:  items,  // Гнучкість: можна одразу додати #EXT-X-MAP, сегменти тощо
    }
}
```

### 3️⃣ `AppendItem()` — потокове додавання
```go
func (pl *Playlist) AppendItem(item Item) {
    pl.Items = append(pl.Items, item)
}
// ✅ Використовується у pipeline: segmentFinalizer → AppendItem(newSegment)
```

### 4️⃣ `String()` — серіалізація через делегування
```go
func (pl *Playlist) String() string {
    s, err := Write(pl)  // Write() — окремий серіалізатор (не показаний у коді)
    if err != nil {
        return ""  // ⚠️ Потенційна проблема: помилка "проковтується"
    }
    return s
}
```

---

## 🔍 Навігаційні методи: фільтрація за типом через type assertion

### Патерн: "Безпечне приведення типу"
```go
// SegmentSize: підрахунок тільки сегментів (#EXTINF)
func (pl *Playlist) SegmentSize() int {
    result := 0
    for _, item := range pl.Items {
        if _, ok := item.(*SegmentItem); ok {  // type assertion
            result++
        }
    }
    return result
}

// Segments: повертає тільки сегменти як зручний слайс
func (pl *Playlist) Segments() []*SegmentItem {
    var s []*SegmentItem
    for _, i := range pl.Items {
        if si, ok := i.(*SegmentItem); ok {  // одночасно перевірка + приведення
            s = append(s, si)
        }
    }
    return s
}
```

### 📊 Метрики плейлиста
| Метод | Призначення | Використання у вашому проекті |
|-------|-------------|------------------------------|
| `PlaylistSize()` | Кількість *плейлистів* у master-плейлисті | Перевірка: чи це master-плейлист? |
| `SegmentSize()` | Кількість медіа-сегментів | Контроль довжини "ковзного вікна" |
| `ItemSize()` | Загальна кількість елементів | Дебаг, метрики |
| `Duration()` | Сумарна тривалість сегментів | Розрахунок затримки, синхронізація |

---

## 🧠 Логіка визначення типу плейлиста

### `IsMaster()` — чи це master-плейлист?
```go
func (pl *Playlist) IsMaster() bool {
    // Пріоритет 1: явний прапорець
    if pl.Master != nil {
        return *pl.Master
    }

    // Пріоритет 2: евристичний аналіз вмісту
    plSize := pl.PlaylistSize()   // Кількість *PlaylistItem* (варіанти якості)
    smSize := pl.SegmentSize()    // Кількість *SegmentItem* (медіа-сегменти)
    
    // Логіка:
    // • Master містить PlaylistItem (варіанти), але НЕ містить SegmentItem
    // • Media містить SegmentItem, але НЕ містить PlaylistItem
    if plSize <= 0 && smSize <= 0 {
        return false  // Порожній плейлист → не master
    }
    return plSize > 0  // Є PlaylistItem → master
}
```

### `IsLive()` — чи це live-потік?
```go
func (pl *Playlist) IsLive() bool {
    if pl.IsMaster() {
        return false  // Master-плейлисти не бувають "live" у сенсі сегментів
    }
    return pl.Live  // Делегуємо внутрішньому прапорцю
}
```

### `IsValid()` — базова валідація структури
```go
func (pl *Playlist) IsValid() bool {
    // ❌ Не можна одночасно мати і PlaylistItem, і SegmentItem
    // Це порушує специфікацію: master ≠ media playlist
    return !(pl.PlaylistSize() > 0 && pl.SegmentSize() > 0)
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ `String()` "проковтує" помилки
```go
// ❌ Поточна реалізація:
func (pl *Playlist) String() string {
    s, err := Write(pl)
    if err != nil {
        return ""  // ⚠️ Клієнт не дізнається про причину помилки!
    }
    return s
}

// ✅ Рекомендація: додати метод з поверненням помилки
func (pl *Playlist) Marshal() (string, error) {
    return Write(pl)  // Write має повертати (string, error)
}

// Або логувати помилку:
if err != nil {
    log.Printf("failed to serialize playlist: %v", err)
    return ""
}
```

### 2️⃣ Відсутність м'ютексів → race condition у конкурентному доступі
```go
// ❌ У вашому pipeline (8x FFmpeg workers + WebSocket broadcast):
pl.AppendItem(segment)  // Горутина 1
items := pl.Segments()  // Горутина 2: читання того ж слайсу → DATA RACE!

// ✅ Рішення: додати sync.RWMutex
type Playlist struct {
    mu sync.RWMutex  // ← Додати
    Items []Item
    // ... інші поля
}

func (pl *Playlist) AppendItem(item Item) {
    pl.mu.Lock()
    defer pl.mu.Unlock()
    pl.Items = append(pl.Items, item)
}

func (pl *Playlist) Segments() []*SegmentItem {
    pl.mu.RLock()
    defer pl.mu.RUnlock()
    // ... фільтрація
}
```

### 3️⃣ `IsMaster()` — крихка евристика
```go
// ❌ Проблема: якщо master-плейлист тимчасово порожній (плSize=0, smSize=0),
// функція поверне false, хоча прапорець Master=nil

// ✅ Рішення: вимагати явного встановлення Master при створенні
func NewMasterPlaylist() *Playlist {
    master := true
    return &Playlist{
        Target: 10,
        Master: &master,  // Явний прапорець
        Live:   false,
    }
}
```

### 4️⃣ `Duration()` — O(n) при кожному виклику
```go
// ❌ Якщо викликається часто (напр. у метриках), це дорого
func (pl *Playlist) Duration() float64 {
    duration := 0.0
    for _, item := range pl.Items {  // Лінійний прохід по всіх елементах
        if segmentItem, ok := item.(*SegmentItem); ok {
            duration += segmentItem.Duration
        }
    }
    return duration
}

// ✅ Оптимізація: кешувати сумарну тривалість
type Playlist struct {
    // ... інші поля
    cachedDuration float64
    durationDirty  bool
}

func (pl *Playlist) AppendItem(item Item) {
    // ... додавання
    if _, ok := item.(*SegmentItem); ok {
        pl.durationDirty = true  // Позначити, що кеш застарів
    }
}

func (pl *Playlist) Duration() float64 {
    if pl.durationDirty {
        pl.recalculateDuration()  // Перерахувати тільки коли потрібно
        pl.durationDirty = false
    }
    return pl.cachedDuration
}
```

### 5️⃣ Відсутність валідації `Target`
```go
// ❌ Можна встановити Target=1, а додати сегменти по 10 секунд → невалідний плейлист

// ✅ Додати валідацію при AppendItem:
func (pl *Playlist) AppendItem(item Item) error {
    if seg, ok := item.(*SegmentItem); ok {
        if seg.Duration > float64(pl.Target) {
            return fmt.Errorf("segment duration %.2f exceeds TARGETDURATION %d", 
                seg.Duration, pl.Target)
        }
    }
    pl.Items = append(pl.Items, item)
    return nil
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури:

### 🎯 Сценарій: генерація media-плейлиста у `segmentFinalizer`
```go
func (sf *SegmentFinalizer) generatePlaylist(channelID string, segments []Segment) (*m3u8.Playlist, error) {
    pl := m3u8.NewPlaylist()
    pl.Live = true
    pl.Target = 4  // Ваші сегменти по 4 секунди
    
    // Додати EXT-X-MAP (fMP4 ініціалізація)
    if sf.initURI != "" {
        pl.AppendItem(&m3u8.MapItem{URI: sf.initURI})
    }
    
    // Додати EXT-X-PROGRAM-DATE-TIME для синхронізації
    if !segments[0].StartTime.IsZero() {
        pl.AppendItem(&m3u8.ProgramDateTime{Time: segments[0].StartTime})
    }
    
    // Додати сегменти
    for _, seg := range segments {
        pl.AppendItem(&m3u8.SegmentItem{
            URI:      seg.URI,
            Duration: seg.Duration,
            Title:    seg.Title,
        })
    }
    
    // Валідація
    if !pl.IsValid() {
        return nil, fmt.Errorf("invalid playlist structure")
    }
    
    return pl, nil
}
```

### 🎯 Сценарій: динамічне оновлення "ковзного вікна"
```go
// У VideoManifestProxy при додаванні нового сегмента:
func (p *VideoManifestProxy) addSegment(seg *SegmentItem) {
    p.playlist.mu.Lock()  // Thread-safe
    defer p.playlist.mu.Unlock()
    
    // Додати новий сегмент
    p.playlist.AppendItem(seg)
    
    // Видалити старі сегменти, якщо перевищено вікно (напр. 60 сегментів = 4 хв)
    const maxSegments = 60
    if p.playlist.SegmentSize() > maxSegments {
        // Знайти індекс першого сегмента
        firstSegIdx := -1
        for i, item := range p.playlist.Items {
            if _, ok := item.(*m3u8.SegmentItem); ok {
                firstSegIdx = i
                break
            }
        }
        if firstSegIdx >= 0 {
            // Видалити перший сегмент + оновити MEDIA-SEQUENCE
            p.playlist.Items = append(p.playlist.Items[:firstSegIdx], p.playlist.Items[firstSegIdx+1:]...)
            p.playlist.Sequence++  // Критично: збільшити MEDIA-SEQUENCE!
        }
    }
}
```

### 🎯 Сценарій: master-плейлист з багатомовними доріжками
```go
func generateMasterPlaylist(channelID string, variants []VideoVariant) *m3u8.Playlist {
    pl := m3u8.NewPlaylist()
    master := true
    pl.Master = &master
    pl.Version = pointer(7)  // fMP4 support
    
    // Аудіо-доріжки (AR/EN/RU)
    for _, lang := range []string{"ar", "en", "ru"} {
        pl.AppendItem(&m3u8.MediaItem{
            Type:       "AUDIO",
            GroupID:    "audio",
            Name:       langName(lang),
            Language:   pointer(lang),
            Default:    pointer(lang == "ar"),
            AutoSelect: pointer(true),
            URI:        pointer(fmt.Sprintf("/channels/%s/audio/%s.m3u8", channelID, lang)),
        })
    }
    
    // Субтитри (з WebSocketDistributor)
    for _, lang := range []string{"en", "ru"} {
        pl.AppendItem(&m3u8.MediaItem{
            Type:       "SUBTITLES",
            GroupID:    "subs",
            Name:       langName(lang) + " Subs",
            Language:   pointer(lang),
            AutoSelect: pointer(true),
            URI:        pointer(fmt.Sprintf("/channels/%s/subs/%s.m3u8", channelID, lang)),
        })
    }
    
    // Відео-варіанти
    for _, v := range variants {
        pl.AppendItem(&m3u8.PlaylistItem{  // PlaylistItem — варіант якості
            URI:      v.URI,
            Bandwidth: v.Bandwidth,
            Resolution: v.Resolution,
            Audio:     "audio",
            Subtitles: "subs",
        })
    }
    
    return pl
}
```

---

## 🧪 Приклад використання: повний цикл

```go
// ✅ Створення live-плейлиста
pl := m3u8.NewPlaylist()
pl.Target = 4
pl.Sequence = 1000
pl.Version = pointer(7)

// ✅ Додавання заголовків
pl.AppendItem(&m3u8.MapItem{URI: "https://cdn/init.mp4"})
pl.AppendItem(&m3u8.PlaybackStart{TimeOffset: -10.0, Precise: pointer(false)})

// ✅ Додавання сегментів
for i := 0; i < 10; i++ {
    pl.AppendItem(&m3u8.SegmentItem{
        URI:      fmt.Sprintf("seg%d.ts", 1000+i),
        Duration: 4.0,
    })
}

// ✅ Серіалізація
output := pl.String()
fmt.Println(output)
/*
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:1000
#EXT-X-MAP:URI="https://cdn/init.mp4"
#EXT-X-START:TIME-OFFSET=-10.000000
#EXTINF:4.0,
seg1000.ts
#EXTINF:4.0,
seg1001.ts
...
*/

// ✅ Навігація
segments := pl.Segments()
fmt.Printf("Total segments: %d, Duration: %.1fs\n", 
    len(segments), pl.Duration())  // 10, 40.0s
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до Playlist

```
✅ #EXT-X-VERSION має бути ≥3 для базового HLS, ≥7 для fMP4
✅ #EXT-X-TARGETDURATION має бути ≥ тривалості будь-якого #EXTINF
✅ #EXT-X-MEDIA-SEQUENCE має зростати монотонно при оновленні live-плейлиста
✅ Master-плейлист НЕ повинен містити #EXTINF / сегменти
✅ Media-плейлист НЕ повинен містити #EXT-X-STREAM-INF / варіанти
✅ #EXT-X-MAP має з'являтися ПЕРЕД першим сегментом (для fMP4)
✅ При видаленні сегментів з live-плейлиста: 
   • Збільшити #EXT-X-MEDIA-SEQUENCE
   • Не змінювати номери в URI (вони вже закешировані клієнтами)
```

---

## 🎯 Висновок

Цей код — **гнучкий фундамент** для роботи з HLS-плейлистами:

✅ Поліморфна архітектура через інтерфейс `Item`  
✅ Чітке розділення master/media плейлистів  
✅ Зручні навігаційні методи (`Segments()`, `Duration()`)  
✅ Легка інтеграція у pipeline генерації

**Для вашого проекту — критичні рекомендації**:

1. 🔒 Додати `sync.RWMutex` для thread-safety у конкурентному середовищі (8x workers + WebSocket)
2. 🚫 Не "проковтувати" помилки у `String()` — додати `Marshal() error`
3. ✅ Валідувати `Target` при додаванні сегментів
4. 🔄 Кешувати `Duration()` для уникнення O(n) при частому виклику
5. 🎯 Явно встановлювати прапорець `Master` при створенні master-плейлистів

Потрібно допомогти з:
- 🔗 Інтеграцією цього `Playlist` у ваш `segmentFinalizer`?
- 🧵 Реалізацією thread-safe версії з `sync.RWMutex`?
- 🧪 Юніт-тестами для `IsValid()`, `IsMaster()`, `Duration()`?

Чекаю на ваші питання! 🛠️