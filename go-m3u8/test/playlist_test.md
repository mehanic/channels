# 🔍 Глибокий розбір тестів: `Playlist` — ядро пакету `m3u8`

Цей файл містить **комплексну батарею юніт-тестів** для структури `Playlist`, яка є центральним елементом пакету для роботи з HLS-плейлистами. Розберемо архітектурно та детально кожен аспект.

---

## 📦 Архітектура тестового файлу: матриця покриття

```
┌─────────────────────────────────────────────────┐
│ Тести Playlist: 8 функцій → 4 категорії         │
├─────────────────────────────────────────────────┤
│ 🔹 Конструктори та ініціалізація                │
│    • TestPlaylist_New                           │
│                                                 │
│ 🔹 Методи навігації та фільтрації               │
│    • TestPlaylist_PlaylistSize (PlaylistItem)   │
│    • TestPlaylist_Segments (SegmentItem)        │
│    • TestPlaylist_Master / IsLive               │
│                                                 │
│ 🔹 Валідація та бізнес-логіка                   │
│    • TestPlaylist_Valid (IsValid)               │
│    • TestPlaylist_Duration (сума тривалостей)   │
│                                                 │
│ 🔹 Серіалізація (Write → String)                │
│    • TestPlaylist_ToString (Master + Media)     │
└─────────────────────────────────────────────────┘
```

### 🎯 Навіщо таке розділення?
| Категорія | Призначення | Приклад у вашому проекті |
|-----------|-------------|-------------------------|
| **Конструктори** | Перевірка створення об'єктів | `NewPlaylist()` для нового каналу |
| **Навігація** | Фільтрація `[]Item` за типом | `Segments()` для отримання тільки сегментів |
| **Валідація** | Запобігання невалідним станам | `IsValid()` перед записом плейлиста |
| **Серіалізація** | Генерація M3U8-файлу | `String()` → запис у файл для CDN |

---

## 🔬 Детальний розбір кожного тесту

### 1️⃣ `TestPlaylist_New` — конструктори + читання з файлу

```go
func TestPlaylist_New(t *testing.T) {
    // 🎯 Тест 1: ручне створення Master Playlist
    p := &m3u8.Playlist{Master: pointer.ToBool(true)}
    assert.True(t, p.IsMaster())  // ✅ Прапорець Master = true → IsMaster() = true
    
    // 🎯 Тест 2: читання з файлу (інтеграційний аспект)
    p, err := m3u8.ReadFile("fixtures/master.m3u8")
    assert.Nil(t, err)            // ✅ Файл успішно прочитано
    assert.True(t, p.IsMaster())  // ✅ Парсер правильно визначив тип
    assert.Equal(t, len(p.Items), 8)  // ✅ Очікувана кількість елементів
}
```

#### 🎯 Що тестує цей кейс?
| Аспект | Перевірка | Чому це важливо |
|--------|-----------|----------------|
| **Ручне створення** | `Master: pointer.ToBool(true)` → `IsMaster()` | Прапорець має пріоритет над евристикою |
| **Читання з файлу** | `ReadFile()` → парсинг → валідація | Інтеграція з реальною файловою системою |
| **Визначення типу** | `IsMaster()` після парсингу | Евристика: PlaylistItem → Master, SegmentItem → Media |
| **Кількість елементів** | `len(p.Items) == 8` | Перевірка повноти парсингу файлу |

#### ⚠️ Потенційні проблеми
```go
// ❌ "fixtures/master.m3u8" — жорстко закодований шлях
// • Тест залежить від наявності файлу у конкретній директорії
// • При запуску з іншої директорії → файл не знайдено → false negative

// ✅ Рішення: використовувати embed або відносні шляхи через testing.T
func TestPlaylist_New(t *testing.T) {
    // 🎯 Варіант 1: embed файлів у бінар тесту (Go 1.16+)
    //go:embed fixtures/*.m3u8
    var testFiles embed.FS
    
    content, err := testFiles.ReadFile("fixtures/master.m3u8")
    assert.NoError(t, err)
    p, err := m3u8.ParseReader(bytes.NewReader(content))  // Парсинг з memory
    assert.NoError(t, err)
    
    // 🎯 Варіант 2: отримання абсолютного шляху через testing.T
    // absPath := filepath.Join(filepath.Dir(t.Name()), "fixtures", "master.m3u8")
}
```

---

### 2️⃣ `TestPlaylist_Duration` — сума тривалостей сегментів

```go
func TestPlaylist_Duration(t *testing.T) {
    p := &m3u8.Playlist{
        Items: []m3u8.Item{
            &m3u8.SegmentItem{Duration: 10.991, Segment: "test_01.ts"},
            &m3u8.SegmentItem{Duration: 9.891, Segment: "test_02.ts"},
            &m3u8.SegmentItem{Duration: 10.556, Segment: "test_03.ts"},
            &m3u8.SegmentItem{Duration: 8.790, Segment: "test_04.ts"},
        },
    }

    // 🎯 Очікувана сума: 10.991 + 9.891 + 10.556 + 8.790 = 40.228
    // 🎯 Форматування: "%.3f" → "40.228" (3 знаки після коми)
    assert.Equal(t, "40.228", fmt.Sprintf("%.3f", p.Duration()))
}
```

#### 🎯 Чому порівнюємо рядки, а не float64?
```go
// ❌ Пряме порівняння float64 ненадійне через похибки округлення:
// assert.Equal(t, 40.228, p.Duration())  // Може не пройти: 40.22799999999999

// ✅ Порівняння відформатованих рядків:
// • "%.3f" округлює до мілісекунд (достатньо для медіа)
// • Рядкове порівняння детерміноване та читабельне

// 🔄 Альтернатива: порівняння з допуском (epsilon)
// assert.InDelta(t, 40.228, p.Duration(), 0.001)  // Допуск ±1мс
```

#### ⚠️ Потенційні проблеми
```go
// ❌ Duration() ітерує ВСІ Items, включаючи не-SegmentItem
// • Якщо у плейлисті є PlaylistItem, MediaItem тощо → вони ігноруються (ок)
// • Але: O(n) прохід при кожному виклику → дорого при частому використанні

// ✅ Оптимізація: кешування сумарної тривалості
type CachedPlaylist struct {
    *m3u8.Playlist
    cachedDuration float64
    durationDirty  bool
}

func (cp *CachedPlaylist) Duration() float64 {
    if cp.durationDirty {
        cp.cachedDuration = cp.Playlist.Duration()  // Перерахунок
        cp.durationDirty = false
    }
    return cp.cachedDuration
}

func (cp *CachedPlaylist) AppendItem(item m3u8.Item) {
    cp.Playlist.AppendItem(item)
    if _, ok := item.(*m3u8.SegmentItem); ok {
        cp.durationDirty = true  // Позначити, що кеш застарів
    }
}
```

---

### 3️⃣ `TestPlaylist_Master` — логіка `IsMaster()`

```go
func TestPlaylist_Master(t *testing.T) {
    // 🎯 Сценарій 1: Master за вмістом (є PlaylistItem)
    p := &m3u8.Playlist{
        Items: []m3u8.Item{
            &m3u8.PlaylistItem{ProgramID: pointer.ToString("1"), URI: "playlist_url", Bandwidth: 6400},
        },
    }
    assert.True(t, p.IsMaster())  // ✅ PlaylistItem → Master
    
    // 🎯 Сценарій 2: Media за вмістом (є SegmentItem)
    p = &m3u8.Playlist{
        Items: []m3u8.Item{
            &m3u8.SegmentItem{Duration: 10.991, Segment: "test_01.ts"},
        },
    }
    assert.False(t, p.IsMaster())  // ✅ SegmentItem → Media
    
    // 🎯 Сценарій 3: Примусовий Master через прапорець
    p = &m3u8.Playlist{Master: pointer.ToBool(true)}
    assert.True(t, p.IsMaster())  // ✅ Прапорець має пріоритет
}
```

#### 🎯 Припустима реалізація `IsMaster()`
```go
func (pl *Playlist) IsMaster() bool {
    // 🎯 Пріоритет 1: явний прапорець
    if pl.Master != nil {
        return *pl.Master
    }
    
    // 🎯 Пріоритет 2: евристика за вмістом
    plSize := pl.PlaylistSize()   // Кількість PlaylistItem
    smSize := pl.SegmentSize()    // Кількість SegmentItem
    
    // • Master містить PlaylistItem, але НЕ містить SegmentItem
    // • Media містить SegmentItem, але НЕ містить PlaylistItem
    if plSize <= 0 && smSize <= 0 {
        return false  // Порожній плейлист → не Master
    }
    return plSize > 0  // Є PlaylistItem → Master
}
```

#### ⚠️ Потенційні проблеми
```go
// ❌ Евристика крихка: якщо плейлист тимчасово порожній → неправильний результат
// • При створенні нового плейлиста: Items = [] → IsMaster() = false (хоча має бути Master)

// ✅ Рішення: вимагати явного встановлення Master при створенні
func NewMasterPlaylist() *Playlist {
    master := true
    return &Playlist{
        Target: 10,
        Master: &master,  // ✅ Явний прапорець
        Live:   false,
    }
}

func NewMediaPlaylist() *Playlist {
    return &Playlist{
        Target: 10,
        Live:   true,  // ✅ Media за замовчуванням live
    }
}
```

---

### 4️⃣ `TestPlaylist_Live` — логіка `IsLive()`

```go
func TestPlaylist_Live(t *testing.T) {
    // 🎯 Сценарій 1: Master playlist → завжди !IsLive()
    p := &m3u8.Playlist{
        Items: []m3u8.Item{
            &m3u8.PlaylistItem{...},
        },
    }
    assert.False(t, p.IsLive())  // ✅ Master не буває "live" у сенсі сегментів
    
    // 🎯 Сценарій 2: Media playlist з прапорцем Live=true
    p = &m3u8.Playlist{
        Items: []m3u8.Item{
            &m3u8.SegmentItem{Duration: 10.991, Segment: "test_01.ts"},
        },
        Live: true,  // ✅ Явний прапорець
    }
    assert.True(t, p.IsLive())  // ✅ Live=true → IsLive()=true
}
```

#### 🎯 Припустима реалізація `IsLive()`
```go
func (pl *Playlist) IsLive() bool {
    // 🎯 Master-плейлисти не бувають "live" у сенсі сегментів
    if pl.IsMaster() {
        return false
    }
    // 🎯 Для Media: делегуємо внутрішньому прапорцю
    return pl.Live
}
```

#### ⚠️ Потенційні проблеми
```go
// ❌ Прапорець Live не синхронізується з #EXT-X-PLAYLIST-TYPE
// • Якщо pl.Type = pointer("VOD"), але pl.Live = true → суперечність!

// ✅ Додати валідацію узгодженості:
func (pl *Playlist) Validate() error {
    if pl.Type != nil {
        if *pl.Type == "VOD" && pl.Live {
            return fmt.Errorf("conflict: Type=VOD but Live=true")
        }
        if *pl.Type == "EVENT" && !pl.Live {
            return fmt.Errorf("conflict: Type=EVENT but Live=false")
        }
    }
    return nil
}
```

---

### 5️⃣ `TestPlaylist_ToString` — серіалізація Master та Media

```go
func TestPlaylist_ToString(t *testing.T) {
    // 🎯 Сценарій 1: Master Playlist з двома варіантами якості
    p := &m3u8.Playlist{
        Items: []m3u8.Item{
            &m3u8.PlaylistItem{ProgramID: pointer.ToString("1"), URI: "playlist_url", Bandwidth: 6400, AudioCodec: pointer.ToString("mp3")},
            &m3u8.PlaylistItem{ProgramID: pointer.ToString("2"), URI: "playlist_url", Bandwidth: 50000, Width: pointer.ToInt(1920), Height: pointer.ToInt(1080), Profile: pointer.ToString("high"), Level: pointer.ToString("4.1"), AudioCodec: pointer.ToString("aac-lc")},
        },
    }
    
    expected := `#EXTM3U
#EXT-X-STREAM-INF:PROGRAM-ID=1,CODECS="mp4a.40.34",BANDWIDTH=6400
playlist_url
#EXT-X-STREAM-INF:PROGRAM-ID=2,RESOLUTION=1920x1080,CODECS="avc1.640029,mp4a.40.2",BANDWIDTH=50000
playlist_url
`
    assert.Equal(t, expected, p.String())  // ✅ Перевірка точного виводу
    
    // 🎯 Сценарій 2: Media Playlist з сегментами + ENDLIST
    p = m3u8.NewPlaylistWithItems(
        []m3u8.Item{
            &m3u8.SegmentItem{Duration: 11.344644, Segment: "1080-7mbps00000.ts"},
            &m3u8.SegmentItem{Duration: 11.261233, Segment: "1080-7mbps00001.ts"},
        },
    )
    expected = `#EXTM3U
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-TARGETDURATION:10
#EXTINF:11.344644,
1080-7mbps00000.ts
#EXTINF:11.261233,
1080-7mbps00001.ts
#EXT-X-ENDLIST
`
    assert.Equal(t, expected, p.String())  // ✅ VOD: додано #EXT-X-ENDLIST
}
```

#### 🎯 Що тестує цей кейс?
| Аспект | Master Playlist | Media Playlist | Чому це важливо |
|--------|----------------|----------------|----------------|
| **Заголовки** | Тільки `#EXTM3U`, `#EXT-X-VERSION` | `#EXT-X-TARGETDURATION`, `#EXT-X-MEDIA-SEQUENCE` | Різні набори тегів для різних типів |
| **Елементи** | `#EXT-X-STREAM-INF` + URI | `#EXTINF` + URI | Різна структура елементів |
| **Авто-генерація кодеку** | `AudioCodec="mp3"` → `CODECS="mp4a.40.34"` | Не застосовується | Логіка `formatCodecs()` у PlaylistItem |
| **Футер** | Немає `#EXT-X-ENDLIST` | Є `#EXT-X-ENDLIST` (бо !Live) | Критично для поведінки плеєра |

#### ⚠️ Потенційні проблеми
```go
// ❌ Жорстке порівняння рядків чутливе до порядку атрибутів
// • Якщо Write() змінить порядок: BANDWIDTH,CODECS → CODECS,BANDWIDTH
// • Тест зламається, хоча функціонально все правильно

// ✅ Рішення: порівнювати семантично, а не текстуально
func assertPlaylistSemanticallyEqual(t *testing.T, expected, actual string) {
    // 🎯 Розбити на рядки, відсортувати атрибути, порівняти
    // Або: парсити обидва рядки → порівняти об'єкти, а не текст
}

// ❌ NewPlaylistWithItems() не встановлює TargetDuration динамічно
// • У тесті: сегменти по ~11с, але TargetDuration=10 (дефолт) → невалідний плейлист!
// • Специфікація: TargetDuration ≥ тривалості будь-якого сегмента

// ✅ Додати валідацію або авто-розрахунок:
func NewPlaylistWithItems(items []Item) *Playlist {
    pl := &Playlist{Target: 10, Items: items}
    
    // 🎯 Авто-розрахунок TargetDuration з максимальної тривалості сегмента
    maxDur := 10.0  // Дефолт
    for _, item := range items {
        if seg, ok := item.(*SegmentItem); ok {
            if seg.Duration > maxDur {
                maxDur = math.Ceil(seg.Duration)  // Округлення вгору до цілого
            }
        }
    }
    pl.Target = int(maxDur)
    return pl
}
```

---

### 6️⃣ `TestPlaylist_Valid` — валідація через `IsValid()`

```go
func TestPlaylist_Valid(t *testing.T) {
    p := m3u8.NewPlaylist()
    assert.True(t, p.IsValid())  // ✅ Порожній плейлист = валідний
    
    // 🎯 Додавання PlaylistItem (Master-елементи)
    p.AppendItem(&m3u8.PlaylistItem{...})
    p.AppendItem(&m3u8.PlaylistItem{...})
    assert.True(t, p.IsValid())  // ✅ Тільки PlaylistItem = валідний Master
    assert.Equal(t, 2, len(p.Items))
    
    // 🎯 Додавання SegmentItem до Master = ❌ НЕВАЛІДНО!
    p.AppendItem(&m3u8.SegmentItem{Duration: 10.991, Segment: "test.ts"})
    assert.False(t, p.IsValid())  // ❌ Змішаний тип = помилка
}
```

#### 🎯 Припустима реалізація `IsValid()`
```go
func (pl *Playlist) IsValid() bool {
    // ❌ Не можна мати одночасно PlaylistItem і SegmentItem
    // Це порушує специфікацію: master ≠ media playlist
    return !(pl.PlaylistSize() > 0 && pl.SegmentSize() > 0)
}
```

#### ⚠️ Потенційні проблеми
```go
// ❌ IsValid() перевіряє тільки "змішаність", але не інші правила:
// • TargetDuration ≥ тривалості сегментів
// • MEDIA-SEQUENCE монотонно зростає у live
// • #EXT-X-MAP перед першим сегментом для fMP4

// ✅ Додати розширену валідацію:
func (pl *Playlist) ValidateForWrite() error {
    if !pl.IsValid() {
        return fmt.Errorf("playlist contains mixed item types")
    }
    
    if !pl.IsMaster() {
        // 🎯 Перевірка TargetDuration
        for _, item := range pl.Items {
            if seg, ok := item.(*SegmentItem); ok {
                if seg.Duration > float64(pl.Target) {
                    return fmt.Errorf("segment duration %.3f exceeds TARGETDURATION %d", 
                        seg.Duration, pl.Target)
                }
            }
        }
        
        // 🎯 Перевірка #EXT-X-MAP для fMP4
        hasMap := false
        hasSegment := false
        for _, item := range pl.Items {
            if _, ok := item.(*MapItem); ok {
                hasMap = true
            }
            if _, ok := item.(*SegmentItem); ok {
                hasSegment = true
            }
        }
        if hasSegment && !hasMap && pl.UsesFMP4 {  // Припустимо, є прапорець
            return fmt.Errorf("fMP4 playlist requires #EXT-X-MAP before first segment")
        }
    }
    
    return nil
}
```

---

### 7️⃣ `TestPlaylist_PlaylistSize` — фільтрація PlaylistItem

```go
func TestPlaylist_PlaylistSize(t *testing.T) {
    p := m3u8.NewPlaylist()
    
    p.AppendItem(&m3u8.PlaylistItem{URI: "playlist0_url", ...})
    p.AppendItem(&m3u8.PlaylistItem{URI: "playlist1_url", ...})
    
    assert.Equal(t, 2, p.PlaylistSize())  // ✅ Кількість PlaylistItem
    
    pi := p.Playlists()  // ✅ Фільтрований слайс []*PlaylistItem
    assert.Equal(t, "playlist0_url", pi[0].URI)
    assert.Equal(t, "playlist1_url", pi[1].URI)
}
```

#### 🎯 Припустима реалізація
```go
func (pl *Playlist) PlaylistSize() int {
    result := 0
    for _, item := range pl.Items {
        if _, ok := item.(*PlaylistItem); ok {
            result++
        }
    }
    return result
}

func (pl *Playlist) Playlists() []*PlaylistItem {
    var p []*PlaylistItem
    for _, i := range pl.Items {
        if pi, ok := i.(*PlaylistItem); ok {
            p = append(p, pi)
        }
    }
    return p
}
```

#### ⚠️ Потенційні проблеми
```go
// ❌ O(n) прохід при кожному виклику → дорого при частому використанні
// • У live-плейлистах: PlaylistSize() може викликатися при кожному оновленні

// ✅ Оптимізація: кешування розмірів
type CachedPlaylist struct {
    *Playlist
    mu sync.RWMutex
    cachedPlaylistSize int
    cachedSegmentSize  int
    sizesDirty         bool
}

func (cp *CachedPlaylist) PlaylistSize() int {
    cp.mu.RLock()
    if !cp.sizesDirty {
        defer cp.mu.RUnlock()
        return cp.cachedPlaylistSize
    }
    cp.mu.RUnlock()
    
    cp.mu.Lock()
    defer cp.mu.Unlock()
    cp.cachedPlaylistSize = cp.Playlist.PlaylistSize()
    cp.cachedSegmentSize = cp.Playlist.SegmentSize()
    cp.sizesDirty = false
    return cp.cachedPlaylistSize
}

func (cp *CachedPlaylist) AppendItem(item Item) {
    cp.Playlist.AppendItem(item)
    cp.sizesDirty = true  // Позначити, що кеш застарів
}
```

---

### 8️⃣ `TestPlaylist_Segments` — фільтрація SegmentItem

```go
func TestPlaylist_Segments(t *testing.T) {
    p := &m3u8.Playlist{
        Items: []m3u8.Item{
            &m3u8.SegmentItem{Duration: 10.991, Segment: "test_01.ts"},
            &m3u8.SegmentItem{Duration: 9.891, Segment: "test_02.ts"},
            &m3u8.SegmentItem{Duration: 10.556, Segment: "test_03.ts"},
            &m3u8.SegmentItem{Duration: 8.790, Segment: "test_04.ts"},
        },
    }

    assert.Equal(t, 4, p.SegmentSize())  // ✅ Кількість сегментів
    si := p.Segments()                   // ✅ Фільтрований слайс []*SegmentItem
    
    assert.Equal(t, "test_01.ts", si[0].Segment)
    assert.Equal(t, "test_02.ts", si[1].Segment)
    assert.Equal(t, 10.556, si[2].Duration)
    assert.Equal(t, 8.790, si[3].Duration)
}
```

#### 🎯 Навіщо окремі методи `Segments()` / `Playlists()`?
```go
// ✅ Type-safe доступ: не потрібно робити type assertion у клієнтському коді
// ❌ Без методів:
for _, item := range pl.Items {
    if seg, ok := item.(*SegmentItem); ok {
        // Працюємо з seg...
    }
}

// ✅ З методами:
for _, seg := range pl.Segments() {
    // seg вже типу *SegmentItem → чистий код
    processSegment(seg)
}

// ✅ Підтримка поліморфізму: []Item зберігає різні типи, 
// але методи надають зручний доступ до конкретного типу
```

---

## ⚠️ Загальні проблеми та покращення для всього файлу

### 1️⃣ Відсутність `t.Parallel()` для прискорення тестів
```go
// ✅ Додати t.Parallel() у кожен тест для паралельного виконання:
func TestPlaylist_New(t *testing.T) {
    t.Parallel()  // ✅ Дозволяє виконувати паралельно з іншими тестами
    // ... код ...
}

// 📊 Ефект: 8 тестів × ~10мс кожен → 80мс послідовно → ~15мс паралельно
```

### 2️⃣ Жорсткі залежності від зовнішніх файлів
```go
// ❌ ReadFile("fixtures/master.m3u8") → залежить від ФС
// ✅ Використовувати embed або bytes.Reader для ізольованості:

//go:embed fixtures/*.m3u8
var testFixtures embed.FS

func TestPlaylist_New(t *testing.T) {
    content, err := testFixtures.ReadFile("fixtures/master.m3u8")
    assert.NoError(t, err)
    p, err := m3u8.ParseReader(bytes.NewReader(content))  // Парсинг з memory
    assert.NoError(t, err)
    // ...
}
```

### 3️⃣ Відсутність тестів на помилки
```go
// ✅ Додати негативні тести:
func TestPlaylist_InvalidInputs(t *testing.T) {
    t.Run("MixedItemTypes", func(t *testing.T) {
        p := m3u8.NewPlaylist()
        p.AppendItem(&m3u8.PlaylistItem{URI: "x", Bandwidth: 100})
        p.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg.ts"})
        
        assert.False(t, p.IsValid())  // ✅ Змішані типи = невалідно
    })
    
    t.Run("TargetDurationTooSmall", func(t *testing.T) {
        p := m3u8.NewPlaylist()
        p.Target = 4  // Максимум 4 секунди
        p.AppendItem(&m3u8.SegmentItem{Duration: 10.0, Segment: "seg.ts"})  // ❌ 10 > 4
        
        err := p.ValidateForWrite()  // Припустимо, такий метод є
        assert.Error(t, err)
    })
}
```

### 4️⃣ Відсутність бенчмарків для продуктивності
```go
// ✅ Додати бенчмарки для критичних методів:
func BenchmarkPlaylist_Duration(b *testing.B) {
    p := &m3u8.Playlist{}
    for i := 0; i < 1000; i++ {
        p.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: fmt.Sprintf("seg%d.ts", i)})
    }
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = p.Duration()
    }
}

// 🚀 Запуск: go test -bench=. -benchmem
// Результат покаже, чи потрібна оптимізація кешуванням
```

### 5️⃣ Thread-safety не тестується
```go
// ❌ У вашому pipeline (8x workers + WebSocket) Playlist мутається конкурентно
// ✅ Додати тести на race condition:

func TestPlaylist_ConcurrentAppend(t *testing.T) {
    p := m3u8.NewPlaylist()
    var wg sync.WaitGroup
    
    // 🎯 10 горутин додають елементи одночасно
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                p.AppendItem(&m3u8.SegmentItem{
                    Duration: 4.0,
                    Segment:  fmt.Sprintf("seg_%d_%d.ts", id, j),
                })
            }
        }(i)
    }
    
    wg.Wait()
    assert.Equal(t, 1000, len(p.Items))  // 10 goroutines × 100 items
}

// 🚀 Запуск з race detector: go test -race -run TestPlaylist_ConcurrentAppend
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **live-ковзним вікном** та **WebSocket-оновленнями**:

### 🎯 Сценарій: створення live-плейлиста з валідацією
```go
// У segmentFinalizer при генерації нового плейлиста:
func (sf *SegmentFinalizer) generateLivePlaylist() error {
    pl := m3u8.NewPlaylist()
    pl.Target = sf.targetDuration  // Напр. 4 секунди
    pl.Live = true
    pl.Sequence = sf.currentSequence
    
    // 🎯 Додавання #EXT-X-MAP для fMP4
    if sf.initURI != "" {
        pl.AppendItem(&m3u8.MapItem{URI: sf.initURI})
    }
    
    // 🎯 Додавання сегментів (ковзне вікно)
    for _, seg := range sf.activeSegments {
        pl.AppendItem(&m3u8.SegmentItem{
            Duration: seg.Duration,
            Segment:  seg.URI,
        })
    }
    
    // 🎯 Валідація перед серіалізацією
    if err := pl.ValidateForWrite(); err != nil {
        return fmt.Errorf("invalid playlist: %w", err)
    }
    
    // 🎯 Серіалізація та атомарний запис
    content := pl.String()
    return sf.atomicWritePlaylist(content)
}
```

### 🎯 Сценарій: фільтрація сегментів для метрик
```go
// У monitoring.Monitor для збору статистики:
func (m *Monitor) collectSegmentMetrics(pl *m3u8.Playlist) {
    segments := pl.Segments()  // ✅ Type-safe фільтрація
    
    if len(segments) == 0 {
        return
    }
    
    // 🎯 Розрахунок середньої тривалості
    var totalDur float64
    for _, seg := range segments {
        totalDur += seg.Duration
    }
    avgDur := totalDur / float64(len(segments))
    
    m.metrics["segment_avg_duration"].Set(avgDur)
    m.metrics["segment_count"].Set(float64(len(segments)))
    
    // 🎯 Виявлення аномалій (сегменти > TARGETDURATION)
    for _, seg := range segments {
        if seg.Duration > float64(pl.Target) {
            m.alerts["oversized_segment"].Inc()
            m.logger.Warn("segment exceeds TARGETDURATION",
                "duration", seg.Duration, "target", pl.Target)
        }
    }
}
```

### 🎯 Сценарій: динамічне оновлення Master Playlist
```go
// У WebSocketDistributor при додаванні нової аудіо-доріжки:
func (d *Distributor) addAudioTrack(channelID, language, name, uri string) {
    pl := d.masterPlaylists[channelID]
    
    // 🎯 Створення #EXT-X-MEDIA для нової доріжки
    media := &m3u8.MediaItem{
        Type:       "AUDIO",
        GroupID:    "audio",
        Name:       name,
        Language:   pointer.ToString(language),
        AutoSelect: pointer.ToBool(true),
        URI:        pointer.ToString(uri),
    }
    pl.AppendItem(media)
    
    // 🎯 Перевірка валідності після модифікації
    if !pl.IsValid() {
        d.logger.Error("playlist became invalid after modification", 
            "channel", channelID)
        return
    }
    
    // 🎯 Серіалізація + розсилка клієнтам
    content := pl.String()
    d.broadcast(channelID, content)
}
```

---

## 🧪 Приклад: розширений набір тестів для `Playlist`

```go
// ✅ Додати комплексні тести з subtests та валідацією:
func TestPlaylist(t *testing.T) {
    t.Parallel()
    
    t.Run("New/MasterPlaylist", func(t *testing.T) {
        t.Parallel()
        pl := m3u8.NewPlaylist()
        pl.Master = pointer.ToBool(true)
        assert.True(t, pl.IsMaster())
        assert.False(t, pl.IsLive())  // Master не live
    })
    
    t.Run("New/MediaPlaylist_Live", func(t *testing.T) {
        t.Parallel()
        pl := m3u8.NewPlaylist()
        pl.Live = true
        assert.False(t, pl.IsMaster())
        assert.True(t, pl.IsLive())
    })
    
    t.Run("Duration/WithMixedItems", func(t *testing.T) {
        t.Parallel()
        pl := &m3u8.Playlist{
            Items: []m3u8.Item{
                &m3u8.SegmentItem{Duration: 4.0, Segment: "seg1.ts"},
                &m3u8.MapItem{URI: "init.mp4"},  // Не впливає на Duration()
                &m3u8.SegmentItem{Duration: 4.0, Segment: "seg2.ts"},
            },
        }
        assert.Equal(t, 8.0, pl.Duration())  // Тільки сегменти враховуються
    })
    
    t.Run("IsValid/MixedTypes_Invalid", func(t *testing.T) {
        t.Parallel()
        pl := m3u8.NewPlaylist()
        pl.AppendItem(&m3u8.PlaylistItem{URI: "x", Bandwidth: 100})
        pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg.ts"})
        
        assert.False(t, pl.IsValid())  // ❌ Змішані типи
    })
    
    t.Run("ValidateForWrite/TargetDuration", func(t *testing.T) {
        t.Parallel()
        pl := m3u8.NewPlaylist()
        pl.Target = 4
        pl.AppendItem(&m3u8.SegmentItem{Duration: 10.0, Segment: "seg.ts"})
        
        err := pl.ValidateForWrite()  // Припустимо, метод існує
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "exceeds TARGETDURATION")
    })
    
    t.Run("Concurrency/Append_Safe", func(t *testing.T) {
        t.Parallel()
        pl := m3u8.NewPlaylist()
        var wg sync.WaitGroup
        
        for i := 0; i < 10; i++ {
            wg.Add(1)
            go func(id int) {
                defer wg.Done()
                for j := 0; j < 100; j++ {
                    pl.AppendItem(&m3u8.SegmentItem{
                        Duration: 4.0,
                        Segment:  fmt.Sprintf("seg_%d_%d.ts", id, j),
                    })
                }
            }(i)
        }
        wg.Wait()
        
        assert.Equal(t, 1000, len(pl.Items))
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до Playlist

```
✅ #EXTM3U — перший рядок будь-якого M3U8 файлу
✅ #EXT-X-VERSION — має бути ≥3 для базового HLS, ≥7 для fMP4
✅ Master Playlist:
   • Містить #EXT-X-STREAM-INF або #EXT-X-I-FRAME-STREAM-INF
   • НЕ містить #EXTINF або сегменти
   • Може містити #EXT-X-MEDIA для аудіо/субтитр
✅ Media Playlist:
   • Містить #EXTINF + URI сегментів
   • НЕ містить #EXT-X-STREAM-INF
   • Обов'язкові: #EXT-X-TARGETDURATION, #EXT-X-MEDIA-SEQUENCE (для live)
✅ #EXT-X-TARGETDURATION ≥ тривалості будь-якого #EXTINF
✅ #EXT-X-MEDIA-SEQUENCE — монотонно зростає у live при оновленні
✅ #EXT-X-ENDLIST — тільки для VOD, ніколи для live/master
✅ #EXT-X-MAP — обов'язковий для fMP4, перед першим сегментом
✅ Клієнти МАЮТЬ ігнорувати невідомі теги (forward compatibility)
```

---

## 🎯 Висновок

Ці тести — **потужна основа** для валідації `Playlist`:

✅ Покриття основних сценаріїв: створення, навігація, валідація, серіалізація  
✅ Перевірка поліморфізму через `[]Item` + type assertion  
✅ Інтеграційний аспект: читання з файлу

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати `t.Parallel()` для прискорення прогону тестів
2. ✅ Замінити `ReadFile` на `embed` для ізольованості тестів
3. ✅ Додати `ValidateForWrite()` з перевіркою `TargetDuration`, `#EXT-X-MAP` тощо
4. ✅ Додати тести на конкурентний доступ (`-race` flag)
5. ✅ Додати бенчмарки для `Duration()`, `Segments()` при великих плейлистах

**Приклад оптимізації для CCTV live-стріму**:
```go
// Для high-throughput генерації плейлистів:
type OptimizedPlaylist struct {
    *m3u8.Playlist
    mu sync.RWMutex
    // Кеш для частих операцій
    cachedSegments []*m3u8.SegmentItem
    segmentsDirty  bool
}

func (op *OptimizedPlaylist) Segments() []*m3u8.SegmentItem {
    op.mu.RLock()
    if !op.segmentsDirty && op.cachedSegments != nil {
        defer op.mu.RUnlock()
        return op.cachedSegments
    }
    op.mu.RUnlock()
    
    op.mu.Lock()
    defer op.mu.Unlock()
    op.cachedSegments = op.Playlist.Segments()  // Перерахунок
    op.segmentsDirty = false
    return op.cachedSegments
}

func (op *OptimizedPlaylist) AppendItem(item m3u8.Item) {
    op.mu.Lock()
    defer op.mu.Unlock()
    op.Playlist.AppendItem(item)
    if _, ok := item.(*m3u8.SegmentItem); ok {
        op.segmentsDirty = true  // Позначити, що кеш застарів
    }
}
// → При 1000 сегментах: Segments() з кешем ~10× швидше
```

Потрібно допомогти з:
- 🔗 Реалізацією `ValidateForWrite()` для вашого `segmentFinalizer`?
- 🧠 Інтеграцією кешування для `Duration()`/`Segments()` у high-load сценаріях?
- 🧪 Написанням fuzz-тестів для пошуку крайніх випадків у парсингу/серіалізації?

Чекаю на ваші питання! 🛠️🎬📋