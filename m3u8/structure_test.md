# 🧪 `m3u8/types_test.go`: Тестування структур даних для HLS-плейлистів

Це **допоміжний тест-файл** бібліотеки `github.com/grafov/m3u8`, який перевіряє коректність створення основних структур даних (`MediaPlaylist`, `MasterPlaylist`) та надає інструменти для валідації реалізації інтерфейсів.

---

## 🎯 Коротка відповідь

> **Це "перевірка фундаменту" бібліотеки**: він гарантує, що базові структури плейлистів створюються коректно, реалізують потрібні інтерфейси, а механізм кастомних тегів працює як очікується — критично для надійності вашого CCTV HLS-конвеєра.

---

## 🧱 Основні компоненти тесту

### 🔹 `CheckType` — валідація реалізації інтерфейсу `Playlist`

```go
func CheckType(t *testing.T, p Playlist) {
    t.Logf("%T implements Playlist interface OK\n", p)
}
```

**🎯 Призначення**: Перевірити через type assertion, що переданий об'єкт реалізує інтерфейс `Playlist`.

**🔄 Як це працює:**
```
🔹 Виклик: CheckType(t, &MediaPlaylist{})
│
▼
🔹 Параметр функції: p Playlist (інтерфейс)
│
▼
🔹 %T у t.Logf() виводить конкретний тип: *m3u8.MediaPlaylist
│
▼
🔹 Якщо компіляція пройшла → тип реалізує Playlist ✅
🔹 Якщо ні → помилка компіляції ще до запуску тесту
```

**🎯 Призначення у тестах:**
```go
func TestDecodeMediaPlaylistWithAutodetection(t *testing.T) {
    p, listType, err := DecodeFrom(bufio.NewReader(f), true)
    if err != nil { t.Fatal(err) }
    
    pp := p.(*MediaPlaylist)  // 🔹 Type assertion
    CheckType(t, pp)          // 🔹 Логування: "*m3u8.MediaPlaylist implements Playlist interface OK"
    // ... подальші перевірки
}
```

---

### 🔹 `TestNewMediaPlaylist` — тест конструктора MediaPlaylist

```go
func TestNewMediaPlaylist(t *testing.T) {
    _, e := NewMediaPlaylist(1, 2)  // 🔹 winsize=1, capacity=2
    if e != nil {
        t.Fatalf("Create media playlist failed: %s", e)
    }
}
```

**🎯 Призначення**: Перевірити, що конструктор `NewMediaPlaylist` не повертає помилок при коректних параметрах.

**🔍 Що перевіряється (неявно):**
- ✅ Виділення пам'яті для масиву сегментів (`capacity=2`)
- ✅ Ініціалізація внутрішніх покажчиків (`head=0`, `tail=0`, `count=0`)
- ✅ Налаштування параметрів за замовчуванням (`ver=3`, `Closed=false`, тощо)

**⚠️ Обмеження тесту**: Не перевіряє логіку роботи з ковзним вікном — тільки створення.

---

### 🔹 `MockCustomTag` — тестовий мок для кастомних тегів

```go
type MockCustomTag struct {
    name          string  // 🔹 Ідентифікатор тегу: "#CUSTOM-TAG:"
    err           error   // 🔹 Помилка для симуляції збою парсингу
    segment       bool    // 🔹 Чи прив'язаний до сегмента?
    encodedString string  // 🔹 Рядок для Encode()/String()
}
```

**🔄 Реалізація інтерфейсів:**

```go
// 🔹 CustomDecoder
func (t *MockCustomTag) TagName() string { return t.name }
func (t *MockCustomTag) Decode(line string) (CustomTag, error) { return t, t.err }
func (t *MockCustomTag) SegmentTag() bool { return t.segment }

// 🔹 CustomTag
func (t *MockCustomTag) Encode() *bytes.Buffer {
    if t.encodedString == "" { return nil }
    buf := new(bytes.Buffer)
    buf.WriteString(t.encodedString)
    return buf
}
func (t *MockCustomTag) String() string { return t.encodedString }
```

**🎯 Призначення**: Симулювати поведінку кастомних тегів у тестах без реалізації повної логіки.

**🔢 Приклади використання:**

```go
// 🔹 Кейс 1: Успішний парсинг плейлист-тегу
mock := &MockCustomTag{
    name: "#CUSTOM-PLAYLIST-TAG:",
    err: nil,
    segment: false,
    encodedString: "#CUSTOM-PLAYLIST-TAG:42",
}

// 🔹 Кейс 2: Симуляція помилки парсингу
mockErr := &MockCustomTag{
    name: "#BROKEN-TAG:",
    err: errors.New("parse failed"),
    segment: true,
}

// 🔹 Кейс 3: Порожній Encode() → тег не додається у вивід
mockEmpty := &MockCustomTag{
    name: "#EMPTY-TAG:",
    encodedString: "",  // ← Encode() поверне nil
}
```

---

## 🔍 Детальний розбір `NewMediaPlaylist`

### 🔹 Сигнатура та параметри

```go
func NewMediaPlaylist(winsize, capacity uint) (*MediaPlaylist, error)
```

| Параметр | Призначення | Рекомендація для CCTV |
|----------|-------------|----------------------|
| `winsize` | Розмір ковзного вікна для live-плейлистів | `4-10` для low-latency, `0` для VOD |
| `capacity` | Початкова ємність масиву сегментів | `2×winsize` для уникнення частих realloc |

**🔄 Логіка ініціалізації (спрощено):**
```go
func NewMediaPlaylist(winsize, capacity uint) (*MediaPlaylist, error) {
    if capacity < winsize {
        return nil, errors.New("capacity must be >= winsize")
    }
    
    return &MediaPlaylist{
        Segments: make([]*MediaSegment, capacity),  // 🔹 Виділення масиву
        capacity: capacity,
        winsize:  winsize,
        head:     0,
        tail:     0,
        count:    0,
        ver:      minver,  // 🔹 Версія 3 за замовчуванням
        // ... інші поля за замовчуванням
    }, nil
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Створення live-плейлиста для камери

```go
func NewCameraLivePlaylist(cameraID string, latencySeconds int) *m3u8.MediaPlaylist {
    // 🔹 Розрахунок параметрів:
    // • Сегмент = 4 секунди
    // • Ковзне вікно = latencySeconds / 4
    winsize := uint(latencySeconds / 4)
    if winsize < 3 { winsize = 3 }  // 🔹 Мінімум 3 сегменти для стабільності
    
    // 🔹 Ємність = 2×вікно для уникнення частих розширень
    capacity := winsize * 2
    
    p, err := m3u8.NewMediaPlaylist(winsize, capacity)
    if err != nil {
        log.Fatalf("Failed to create playlist: %v", err)
    }
    
    // 🔹 Базові налаштування
    p.SetVersion(7)
    p.SetTargetDuration(4)
    p.SetPlaylistType("event")  // 🔹 Live-подія
    p.SetIndependentSegments(true)
    
    // 🔹 Реєстрація кастомних тегів
    p.WithCustomDecoders([]m3u8.CustomDecoder{
        &CameraIDTag{ID: cameraID},
        &EventTag{},
    })
    
    // 🔹 Додавання плейлист-тегу
    p.AddCustomTag(&CameraIDTag{ID: cameraID})
    
    return p
}

// 🔹 Використання:
playlist := NewCameraLivePlaylist("CAM-001", 12)  // 🔹 12-секундна затримка
```

---

### 🔹 Приклад 2: Тестування власного кастомного тегу

```go
// 🔹 Ваш кастомний тег
type CameraIDTag struct {
    ID string
}

func (t *CameraIDTag) TagName() string { return "#CCTV-CAMERA-ID:" }
func (t *CameraIDTag) Decode(line string) (m3u8.CustomTag, error) {
    newTag := new(CameraIDTag)
    _, err := fmt.Sscanf(line, "#CCTV-CAMERA-ID:%s", &newTag.ID)
    return newTag, err
}
func (t *CameraIDTag) SegmentTag() bool { return false }
func (t *CameraIDTag) Encode() *bytes.Buffer {
    buf := new(bytes.Buffer)
    buf.WriteString(t.TagName())
    buf.WriteString(t.ID)
    return buf
}
func (t *CameraIDTag) String() string { return t.Encode().String() }

// 🔹 Юніт-тест з використанням MockCustomTag як шаблону
func TestCameraIDTag_EncodeDecode(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        wantID   string
        wantErr  bool
    }{
        {"valid", "#CCTV-CAMERA-ID:CAM-001", "CAM-001", false},
        {"empty", "#CCTV-CAMERA-ID:", "", false},
        {"invalid", "#CCTV-CAMERA-ID", "", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tag := &CameraIDTag{}
            result, err := tag.Decode(tt.input)
            
            if (err != nil) != tt.wantErr {
                t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr {
                got := result.(*CameraIDTag).ID
                if got != tt.wantID {
                    t.Errorf("Decode() ID = %v, want %v", got, tt.wantID)
                }
            }
            
            // 🔹 Round-trip тест
            if !tt.wantErr {
                encoded := tag.Encode().String()
                if encoded != tt.input {
                    t.Errorf("Encode() = %v, want %v", encoded, tt.input)
                }
            }
        })
    }
}
```

---

### 🔹 Приклад 3: Валідація типу плейлиста у конвеєрі

```go
func ProcessPlaylist(r io.Reader) error {
    playlist, listType, err := m3u8.DecodeFrom(r, false)
    if err != nil {
        return fmt.Errorf("decode failed: %w", err)
    }
    
    // 🔹 Перевірка типу через type assertion (як у тестах)
    switch listType {
    case m3u8.MASTER:
        master := playlist.(*m3u8.MasterPlaylist)
        CheckType(nil, master)  // 🔹 Опціонально для логування
        return processMasterPlaylist(master)
        
    case m3u8.MEDIA:
        media := playlist.(*m3u8.MediaPlaylist)
        CheckType(nil, media)
        return processMediaPlaylist(media)
        
    default:
        return fmt.Errorf("unknown playlist type: %v", listType)
    }
}

// 🔹 Допоміжна функція для логування (адаптація CheckType)
func CheckType(logger func(string, ...interface{}), p m3u8.Playlist) {
    if logger != nil {
        logger("%T implements Playlist interface OK\n", p)
    }
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| `capacity < winsize` | Паніка або некоректна поведінка ковзного вікна | Завжди передавайте `capacity >= winsize` у `NewMediaPlaylist` |
| Забути `SetPlaylistType("event")` для live | Плейлист кодується як VOD (`#EXT-X-ENDLIST`) | Встановлюйте MediaType перед додаванням сегментів |
| Reuse екземпляра кастомного тегу | Дані одного сегмента "просочуються" в інший | Створюйте новий екземпляр у `Decode()`: `newTag := new(YourTag)` |
| Порожній `encodedString` у моку | Тег не з'являється у виводі без попередження | Перевіряйте `if t.encodedString == "" { return nil }` у тестах |
| Ігнорування `SegmentTag()` | Теги додаються не в те місце (плейлист замість сегмента) | Повертайте `true` для сегмент-тегів, `false` для плейлист-тегів |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні плейлистів:
    • Використовуйте winsize > 0 для live, winsize = 0 для VOD
    • Встановлюйте capacity >= 2×winsize для уникнення частих розширень масиву
    • Реєструйте кастомні теги через WithCustomDecoders() перед парсингом/генерацією

[ ] Для кастомних тегів:
    • Реалізуйте всі методи інтерфейсів: TagName, Decode/Encode, SegmentTag, String
    • Використовуйте унікальні префікси: #CCTV-*, #MYAPP-* для уникнення конфліктів
    • Валідуйте атрибути у Decode() з чіткими помилками
    • Тестуйте round-trip: Encode → Decode → порівняння даних

[ ] Для тестування:
    • Використовуйте MockCustomTag як шаблон для створення тестових тегів
    • Покрийте кейси: успішний парсинг, помилка, порожній Encode()
    • Перевіряйте тип плейлиста через type assertion + CheckType()

[ ] Для дебагу:
    • Логувайте тип плейлиста: log.Printf("📋 Type: %T", playlist)
    • Виводьте закодований плейлист для перевірки: log.Printf("📝 Output:\n%s", playlist.String())
    • Тестуйте з різними розмірами вікна: 3, 10, 100 сегментів

[ ] Для безпеки:
    • Валідуйте вхідні URI сегментів (заборона `file://`, `../`)
    • Обмежуйте довжину кастомних атрибутів (напр., max 255 символів)
    • Не довіряйте кастомним тегам з ненадійних джерел
```

---

## 🎯 Висновок

> **Цей тест-файл — "страж цілісності" бібліотеки**, який забезпечує:
> • ✅ Коректне створення основних структур через конструктори
> • ✅ Реалізацію інтерфейсу `Playlist` для поліморфної обробки
> • ✅ Шаблон для тестування кастомних тегів через `MockCustomTag`
> • ✅ Просту валідацію типів через `CheckType()` для дебагу
> • ✅ Базову перевірку без зайвої складності

Для вашого **CCTV HLS Processor** це означає:
- 🛡️ Надійне створення live/VOD плейлистів з правильними параметрами
- 🔍 Легка валідація типів плейлистів у конвеєрі обробки
- 🧪 Швидке тестування власних кастомних тегів за шаблоном `MockCustomTag`
- 🔄 Безпечне розширення функціоналу без ризику зламати базову логіку
- 📋 Чіткі орієнтири для написання власних тестів структур даних

Потребуєте допомоги з реалізацією специфічних тестів для ваших кастомних тегів або з налаштуванням параметрів ковзного вікна для live-камер? Напишіть — покажу готовий код для вашого сценарію! 🚀🧪