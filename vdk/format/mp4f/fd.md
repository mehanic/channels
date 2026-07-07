# 📦 Глибокий розбір: `mp4f.FDummy` — Універсальний "заглушка" атом для fMP4

Цей файл — **мінімалістична реалізація інтерфейсу `mp4io.Atom`** для обробки невідомих, кастомних або пропущених атомів у фрагментованому MP4 (fMP4). Він дозволяє зберігати та передавати довільні бінарні дані без парсингу, забезпечуючи сумісність з розширеннями стандарту.

---

## 🗺️ Архітектурна схема FDummy

```
┌────────────────────────────────────────┐
│ 📦 mp4f.FDummy — Generic Atom Wrapper │
├────────────────────────────────────────┤
│                                         │
│  🔑 Призначення:                        │
│  • Обробка невідомих атомів у fMP4    │
│  • Збереження кастомних розширень      │
│  • "Прозорий" пропуск не підтримуваних│
│    атомів без втрати даних             │
│                                         │
│  🔄 Інтерфейс mp4io.Atom:              │
│  • Tag() → fourcc код атому           │
│  • Len() → розмір у байтах            │
│  • Marshal() → серіалізація у []byte  │
│  • Unmarshal() → парсинг з []byte     │
│  • Children() → дочірні атоми (пусто) │
│                                         │
│  📡 Використання:                       │
│  • Unknowns []mp4io.Atom у MovieFrag  │
│  • Forwarding невідомих атомів        │
│  • Дебаг/інспекція бінарної структури │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Структура та поля

```go
type FDummy struct {
    Data  []byte        // 🗄️ Сирий вміст атому (без заголовку size+tag)
    Tag_  mp4io.Tag     // 🏷️ Fourcc код атому (напр. 'udta', 'meta')
    mp4io.AtomPos       // 📍 offset, size у файлі для навігації
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення |
|------|-----|-------------|
| `Data` | `[]byte` | Сирий payload атому (тіло без 8-байтового заголовку) |
| `Tag_` | `mp4io.Tag` | Fourcc ідентифікатор (4 байти, наприклад `0x75647461` = 'udta') |
| `AtomPos` | `mp4io.AtomPos` | Вбудована структура з `Offset` та `Size` для відладки/навігації |

### ✅ Ваш use-case**: збереження невідомих атомів

```go
// ParseMovieFragWithUnknowns — парсинг moof із збереженням невідомих атомів
func ParseMovieFragWithUnknowns(b []byte, offset int) (*mp4fio.MovieFrag, error) {
    var moof mp4fio.MovieFrag
    n, err := moof.Unmarshal(b, offset)
    if err != nil {
        return nil, err
    }
    
    // Збереження невідомих атомів для подальшого аналізу
    for _, unknown := range moof.Unknowns {
        if dummy, ok := unknown.(*mp4f.FDummy); ok {
            log.Printf("Found unknown atom: %s at offset %d, size %d", 
                dummy.Tag().String(), dummy.Offset, dummy.Size)
            
            // Приклад: пошук кастомних метаданих
            if dummy.Tag() == mp4io.StringToTag("cprt") {  // copyright
                copyright := string(dummy.Data)
                log.Printf("Copyright: %s", copyright)
            }
        }
    }
    
    return &moof, nil
}
```

---

## 🔑 2. Реалізація інтерфейсу mp4io.Atom

### 🔧 Tag() — ідентифікація атому:

```go
func (self FDummy) Tag() mp4io.Tag {
    return self.Tag_
}
```

**Призначення**: Повертає fourcc код для ідентифікації типу атому.

**✅ Ваш use-case**: фільтрація атомів за типом

```go
// FilterAtomsByTag — отримання атомів певного типу з колекції
func FilterAtomsByTag(atoms []mp4io.Atom, tag mp4io.Tag) []mp4io.Atom {
    result := make([]mp4io.Atom, 0)
    for _, atom := range atoms {
        if atom.Tag() == tag {
            result = append(result, atom)
        }
    }
    return result
}

// Приклад: отримання всіх 'udta' атомів
udtaAtoms := FilterAtomsByTag(moof.Unknowns, mp4io.StringToTag("udta"))
```

### 🔧 Len() — розрахунок розміру:

```go
func (self FDummy) Len() int {
    return len(self.Data)
}
```

**⚠️ Важливо**: `Len()` повертає розмір **тільки даних**, без урахування 8-байтового заголовку атому (size + tag).

**Наслідки**: При серіалізації потрібно вручну додавати заголовок:

```go
// SerializeDummyWithHeader — правильна серіалізація з заголовком
func SerializeDummyWithHeader(dummy *mp4f.FDummy) []byte {
    totalSize := 8 + len(dummy.Data)  // header + data
    buf := make([]byte, totalSize)
    
    pio.PutU32BE(buf[0:], uint32(totalSize))      // size
    pio.PutU32BE(buf[4:], uint32(dummy.Tag()))    // tag
    copy(buf[8:], dummy.Data)                      // data
    
    return buf
}
```

### 🔧 Marshal() — серіалізація даних:

```go
func (self FDummy) Marshal(b []byte) int {
    copy(b, self.Data)
    return len(self.Data)
}
```

**Призначення**: Копіює сирий payload у буфер.

**⚠️ Обмеження**: Не перевіряє `len(b) >= len(self.Data)` → можлива паніка при недостатньому буфері.

**✅ Виправлення**: Додати перевірку меж:

```go
func (self FDummy) MarshalSafe(b []byte) (int, error) {
    if len(b) < len(self.Data) {
        return 0, fmt.Errorf("buffer too small: need %d, got %d", 
            len(self.Data), len(b))
    }
    n := copy(b, self.Data)
    return n, nil
}
```

### 🔧 Unmarshal() — парсинг даних:

```go
func (self FDummy) Unmarshal(b []byte, offset int) (n int, err error) {
    return  // ← ПОВЕРТАЄ (0, nil) БЕЗ ІНІЦІАЛІЗАЦІЇ!
}
```

**❌ Критична проблема**: Метод порожній → неможливо створити `FDummy` з байтів!

**Наслідки**: 
- Неможливо парсити невідомі атоми з файлу
- `Unknowns` залишаються порожніми навіть якщо атоми присутні
- Втрата даних при демуксингу

**✅ Виправлення**: Реалізувати базовий парсинг:

```go
func (self *FDummy) Unmarshal(b []byte, offset int) (n int, err error) {
    // Збереження позиції для навігації
    (&self.AtomPos).setPos(offset, len(b))
    
    // Копіювання даних (припускаємо, що b вже містить тільки payload)
    self.Data = make([]byte, len(b))
    copy(self.Data, b)
    
    return len(b), nil
}

// Альтернатива: парсинг з заголовком атому
func (self *FDummy) UnmarshalWithHeader(b []byte, offset int) (n int, err error) {
    if len(b) < 8 {
        return 0, fmt.Errorf("atom header too short")
    }
    
    (&self.AtomPos).setPos(offset, len(b))
    
    size := int(pio.U32BE(b[0:]))
    tag := mp4io.Tag(pio.U32BE(b[4:]))
    
    if size < 8 || size > len(b) {
        return 0, fmt.Errorf("invalid atom size: %d", size)
    }
    
    self.Tag_ = tag
    self.Data = make([]byte, size-8)
    copy(self.Data, b[8:size])
    
    return size, nil
}
```

### 🔧 Children() — дочірні атоми:

```go
func (self FDummy) Children() []mp4io.Atom {
    return nil
}
```

**Призначення**: `FDummy` — листяний атом, не має дітей.

**✅ Це правильно**: Більшість простих атомів (наприклад, `cprt`, `titl`) не мають вкладеної структури.

---

## 🔑 3. Практичне використання у fMP4 pipeline

### 🔧 Приклад: Прозорий forwarding невідомих атомів

```go
// ForwardUnknownAtoms — копіювання невідомих атомів у вихідний потік
func ForwardUnknownAtoms(w io.Writer, unknowns []mp4io.Atom) error {
    for _, atom := range unknowns {
        // Пропуск атомів, які не підтримують серіалізацію
        if atom.Len() == 0 {
            continue
        }
        
        // Створення буфера з заголовком
        totalSize := 8 + atom.Len()
        buf := make([]byte, totalSize)
        
        pio.PutU32BE(buf[0:], uint32(totalSize))
        pio.PutU32BE(buf[4:], uint32(atom.Tag()))
        
        // Серіалізація тіла
        if marshaler, ok := atom.(interface{ Marshal([]byte) int }); ok {
            marshaler.Marshal(buf[8:])
        } else {
            continue  // пропуск якщо не підтримує Marshal
        }
        
        // Запис у потік
        if _, err := w.Write(buf); err != nil {
            return err
        }
    }
    return nil
}
```

### 🔧 Приклад: Інспекція бінарної структури для дебагу

```go
// InspectAtom — вивід інформації про атом для відладки
func InspectAtom(atom mp4io.Atom, indent string) {
    fmt.Printf("%s%s (offset=%d, size=%d)\n", 
        indent, atom.Tag().String(), atom.Pos())
    
    // Спеціальна обробка FDummy
    if dummy, ok := atom.(*mp4f.FDummy); ok {
        fmt.Printf("%s  Data: %d bytes, hex: %x...\n", 
            indent, len(dummy.Data), dummy.Data[:min(16, len(dummy.Data))])
        
        // Спроба інтерпретації як тексту
        if isPrintable(dummy.Data) {
            fmt.Printf("%s  Text: %q\n", indent, string(dummy.Data))
        }
    }
    
    // Рекурсивний обхід дітей
    for _, child := range atom.Children() {
        InspectAtom(child, indent+"  ")
    }
}

func isPrintable(b []byte) bool {
    for _, c := range b {
        if c < 32 || c > 126 {
            return false
        }
    }
    return len(b) > 0
}

func min(a, b int) int {
    if a < b { return a }
    return b
}
```

### 🔧 Приклад: Створення кастомного атому для метаданих

```go
// CreateCustomMetadataAtom — створення FDummy з кастомними даними
func CreateCustomMetadataAtom(tagName string, data []byte) *mp4f.FDummy {
    return &mp4f.FDummy{
        Tag_:  mp4io.StringToTag(tagName),  // напр. "mytv" для custom TV metadata
        Data:  data,
        AtomPos: mp4io.AtomPos{
            Offset: 0,  // буде встановлено при записі
            Size:   8 + len(data),
        },
    }
}

// Використання: додавання кастомних метаданих у фрагмент
func AddCustomMetadataToFragment(moof *mp4fio.MovieFrag, metadata map[string]string) {
    for key, value := range metadata {
        if len(key) != 4 {
            log.Printf("warning: tag name must be 4 chars, got %q", key)
            continue
        }
        
        dummy := CreateCustomMetadataAtom(key, []byte(value))
        moof.Unknowns = append(moof.Unknowns, dummy)
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Порожній Unmarshal** | Неможливо створити FDummy з байтів | Реалізувати базовий парсинг з копіюванням даних |
| **Len() без заголовку** | Некоректний розрахунок розміру при серіалізації | Додавати +8 для заголовку при записі у файл |
| **Marshal без перевірки буфера** | Паніка при `len(b) < len(Data)` | Додати перевірку `if len(b) < len(self.Data)` |
| **Відсутність конструктора** | Користувачі забувають встановити `Tag_` | Додати `NewFDummy(tag, data)` helper функцію |
| **Неможливість парсингу вкладених структур** | `FDummy` не підходить для атомів з дітьми | Використовувати специфічні типи атомів замість `FDummy` для складних випадків |

---

## ⚡ Оптимізації для high-throughput обробки

### 1. Пул буферів для серіалізації:

```go
var dummyBufferPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 4096)  // типовий розмір для більшості атомів
        return &buf
    },
}

func GetDummyBuffer() *[]byte { return dummyBufferPool.Get().(*[]byte) }
func PutDummyBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    dummyBufferPool.Put(b)
}

// Використання у Marshal:
func (self FDummy) MarshalPooled() ([]byte, error) {
    bufPtr := GetDummyBuffer()
    defer PutDummyBuffer(bufPtr)
    
    totalSize := 8 + len(self.Data)
    if cap(*bufPtr) < totalSize {
        *bufPtr = make([]byte, totalSize)
    } else {
        *bufPtr = (*bufPtr)[:totalSize]
    }
    
    pio.PutU32BE((*bufPtr)[0:], uint32(totalSize))
    pio.PutU32BE((*bufPtr)[4:], uint32(self.Tag_))
    copy((*bufPtr)[8:], self.Data)
    
    result := make([]byte, totalSize)
    copy(result, *bufPtr)
    return result, nil
}
```

### 2. Кешування серіалізованого результату:

```go
type CachedFDummy struct {
    *FDummy
    serialized []byte
    dirty      bool
    mu         sync.RWMutex
}

func (c *CachedFDummy) Marshal(b []byte) int {
    c.mu.RLock()
    if !c.dirty && len(c.serialized) > 0 {
        n := copy(b, c.serialized)
        c.mu.RUnlock()
        return n
    }
    c.mu.RUnlock()
    
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Серіалізація якщо не в кеші
    n := c.FDummy.Marshal(b)
    c.serialized = make([]byte, n)
    copy(c.serialized, b[:n])
    c.dirty = false
    return n
}

func (c *CachedFDummy) MarkDirty() {
    c.mu.Lock()
    c.dirty = true
    c.serialized = nil
    c.mu.Unlock()
}
```

### 3. Моніторинг використання невідомих атомів:

```go
type UnknownAtomMetrics struct {
    UnknownAtomsFound prometheus.CounterVec
    UnknownAtomSize   prometheus.HistogramVec
    KnownTags         prometheus.CounterVec
}

func (m *UnknownAtomMetrics) RecordUnknownAtom(tag mp4io.Tag, size int, streamID string) {
    m.UnknownAtomsFound.WithLabelValues(streamID).Inc()
    m.UnknownAtomSize.WithLabelValues(streamID).Observe(float64(size))
    
    // Відстеження "відомих невідомих" атомів
    knownCustomTags := map[mp4io.Tag]bool{
        mp4io.StringToTag("cprt"): true,  // copyright
        mp4io.StringToTag("titl"): true,  // title
        mp4io.StringToTag("desc"): true,  // description
    }
    if knownCustomTags[tag] {
        m.KnownTags.WithLabelValues(tag.String()).Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання FDummy

```go
// ✅ 1. Завжди встановлюйте Tag_ при створенні
dummy := &mp4f.FDummy{
    Tag_: mp4io.StringToTag("udta"),
    Data: []byte("custom data"),
}

// ✅ 2. Перевіряйте розмір буфера перед Marshal
if len(buf) < dummy.Len() {
    buf = make([]byte, dummy.Len())
}
n := dummy.Marshal(buf)

// ✅ 3. Додавайте заголовок при записі у файл
totalSize := 8 + dummy.Len()
header := make([]byte, 8)
pio.PutU32BE(header[0:], uint32(totalSize))
pio.PutU32BE(header[4:], uint32(dummy.Tag()))
w.Write(header)
w.Write(dummy.Data)

// ✅ 4. Реалізуйте Unmarshal для парсингу з байтів
// ✅ 5. Використовуйте helper-функції для створення
func NewFDummy(tag string, data []byte) *mp4f.FDummy {
    return &mp4f.FDummy{
        Tag_: mp4io.StringToTag(tag),
        Data: data,
    }
}

// ✅ 6. Логування невідомих атомів для дебагу
if debug {
    log.Printf("Unknown atom: %s, %d bytes", dummy.Tag().String(), len(dummy.Data))
}

// ✅ 7. Метрики для моніторингу
metrics.RecordUnknownAtom(dummy.Tag(), len(dummy.Data), streamID)
```

---

## 🔗 Корисні посилання

- 💻 [mp4io.Atom Interface](https://pkg.go.dev/github.com/deepch/vdk/format/mp4/mp4io#Atom) — інтерфейс для атомів
- 📄 [ISO/IEC 14496-12 Box Structure](https://www.iso.org/standard/74428.html) — офіційна специфікація атомів
- 📄 [FourCC Codes Reference](https://www.ftyps.com/) — довідник fourcc кодів для MP4
- 🧪 [Go sync.Pool Best Practices](https://go.dev/blog/pool) — ефективне управління пам'яттю
- 📦 [Prometheus Metrics for Media](https://prometheus.io/docs/practices/instrumentation/) — моніторинг продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Реалізуйте `Unmarshal`** — без нього неможливо парсити невідомі атоми з файлів.
> 2. **Пам'ятайте про заголовок атому** — `Len()` повертає тільки розмір даних, додавайте +8 при серіалізації.
> 3. **Використовуйте helper-функції** — `NewFDummy(tag, data)` зменшує ризик помилок при створенні.
> 4. **Логування невідомих атомів** — допомагає виявляти нові розширення стандарту або помилки у парсингу.
> 5. **Моніторьте `UnknownAtomSize`** — різке зростання може вказувати на пошкоджені файли або атаки.

Потрібен приклад інтеграції `FDummy` з вашим `mp4f.Muxer` для автоматичного збереження та forwarding невідомих атомів у fMP4 потоці? Готовий допомогти! 🚀