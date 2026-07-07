# 🔄 `read.go`: Ітеративне читання та обробка структури боксів у MP4

Це **ядро бібліотеки `go-mp4`**, яке відповідає за **ітеративне читання та обробку ієрархічної структури боксів** у файлах формату MP4/ISOBMFF.

---

## 🎯 Коротка відповідь

> **Це "двигун навігації" бібліотеки**: він дозволяє рекурсивно обходити дерево боксів, викликаючи користувацький обробник для кожного боксу, з підтримкою контексту, шляхів, динамічного розгортання вкладених структур та спеціальної логіки для QuickTime/iTunes metadata.

---

## 🧱 Основні типи даних

### 🔹 `BoxPath` — шлях до поточного боксу в ієрархії

```go
type BoxPath []BoxType
```

**🎯 Призначення**: Представляє **шлях від кореня до поточного боксу** у вигляді масиву типів.

**Приклад:**
```
📦 moov → trak → mdia → minf → stbl → stts
BoxPath = [BoxTypeMoov(), BoxTypeTrak(), BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStts()]
```

---

### 🔹 `compareWith` — порівняння шляхів для пошуку

```go
func (lhs BoxPath) compareWith(rhs BoxPath) (forwardMatch bool, match bool) {
    if len(lhs) > len(rhs) { return false, false }  // ❌ lhs довший за rhs → не може співпадати
    
    for i := 0; i < len(lhs); i++ {
        if !lhs[i].MatchWith(rhs[i]) { return false, false }  // ❌ Типи не співпадають
    }
    
    if len(lhs) < len(rhs) { return true, false }  // ✅ Префікс → forwardMatch=true
    return false, true  // ✅ Точне співпадіння → match=true
}
```

**🎯 Призначення**: Визначити, чи поточний шлях у файлі:
- `forwardMatch = true` → шлях є **префіксом** шуканого → треба заглиблюватися далі
- `match = true` → шлях **точно співпадає** з шуканим → знайдено ціль!

**🔢 Приклад:**
```
🔍 Шукаємо: ["moov", "trak", "mdia"]

📁 Поточний шлях у файлі:
• ["moov"] → forwardMatch=true, match=false → заглиблюємось у moov
• ["moov", "trak"] → forwardMatch=true, match=false → заглиблюємось у trak
• ["moov", "trak", "mdia"] → forwardMatch=false, match=true → ✅ знайдено!
• ["moov", "trak", "mdia", "minf"] → forwardMatch=false, match=false → ігноруємо
```

---

### 🔹 `ReadHandle` — дескриптор для обробки одного боксу

```go
type ReadHandle struct {
    Params      []interface{}           // 🔹 Користувацькі параметри для обробника
    BoxInfo     BoxInfo                 // 🔹 Метадані боксу: тип, розмір, офсет, контекст
    Path        BoxPath                 // 🔹 Шлях до цього боксу в ієрархії
    ReadPayload func() (IBox, uint64, error)  // 🔹 Читання та парсинг вмісту боксу
    ReadData    func(io.Writer) (uint64, error)  // 🔹 Читання сирих даних боксу
    Expand      func(params ...interface{}) ([]interface{}, error)  // 🔹 Рекурсивне розгортання вкладених боксів
}
```

**🎯 Призначення**: Надає **зручний інтерфейс** для користувацького обробника (`ReadHandler`) для роботи з поточним боксом.

**🔹 Ключові методи:**

| Метод | Призначення | Приклад використання |
|-------|-------------|---------------------|
| `ReadPayload()` | 🔹 Парсить вміст боксу у структуру | `box, n, err := h.ReadPayload()` |
| `ReadData(w)` | 🔹 Копіює сирі дані боксу у `io.Writer` | `h.ReadData(&buf)` для експорту |
| `Expand(params...)` | 🔹 Рекурсивно обробляє вкладені бокси | `children, err := h.Expand()` |

---

### 🔹 `ReadHandler` — функція-обробник для кожного боксу

```go
type ReadHandler func(handle *ReadHandle) (val interface{}, err error)
```

**🎯 Призначення**: Користувацька логіка, що викликається **для кожного боксу** під час обходу структури.

**Приклад:**
```go
handler := func(h *mp4.ReadHandle) (interface{}, error) {
    if h.BoxInfo.Type == mp4.BoxTypeTrun() {
        // 🔹 Знайшли trun → парсимо та обробляємо
        trun, _, err := h.ReadPayload()
        if err != nil { return nil, err }
        
        // 🔹 Доступ до полів через type assertion
        if t, ok := trun.(*mp4.Trun); ok {
            log.Printf("📦 Trun: %d samples", t.SampleCount)
        }
    }
    // 🔹 Рекурсивно обробити вкладені бокси
    return h.Expand()
}
```

---

## 🔍 Основні функції читання

### 🔹 `ReadBoxStructure` — вхідна точка для обходу файлу

```go
func ReadBoxStructure(r io.ReadSeeker, handler ReadHandler, params ...interface{}) ([]interface{}, error) {
    if _, err := r.Seek(0, io.SeekStart); err != nil {  // 🔹 Починаємо з початку файлу
        return nil, err
    }
    return readBoxStructure(r, 0, true, nil, Context{}, handler, params)  // 🔹 Виклик внутрішньої функції
}
```

**🎯 Призначення**: Запустити **рекурсивний обхід усієї структури боксів** у файлі, починаючи з кореня.

**🔄 Алгоритм:**
```
🔹 Вхід: io.ReadSeeker (файл, буфер, мережа), ReadHandler, params
│
▼
🔹 readBoxStructure(r, totalSize=0, isRoot=true, path=nil, ctx={}, handler, params):
   │
   ├── 🔹 Цикл: поки не кінець файлу (або totalSize=0 для вкладених)
   │   │
   │   ├── 🔹 Читання заголовка наступного боксу: ReadBoxInfo(r)
   │   │   • Тип (4 байти), Розмір (4/8 байт), [LargeSize], [UUID]
   │   │
   │   ├── 🔹 Перевірка розміру: чи не перевищує залишок буфера?
   │   │
   │   ├── 🔹 Оновлення контексту: QuickTime, Ilst, Wave, Udta прапорці
   │   │
   │   ├── 🔹 Побудова нового шляху: newPath = path + [currentType]
   │   │
   │   ├── 🔹 Створення ReadHandle з методами: ReadPayload, ReadData, Expand
   │   │
   │   ├── 🔹 Виклик користувацького handler(h):
   │   │   • Користувач може: парсити, читати дані, розгортати вкладені
   │   │   • Повертає val/interface{} для збору результатів
   │   │
   │   ├── 🔹 Оновлення контексту з результатів обробки (QuickTime, Keys)
   │   │
   │   └── 🔹 Додавання val до списку результатів
   │
   ▼
🔹 Вихід: []interface{} з результатами обробки всіх боксів
```

---

### 🔹 `readBoxStructureFromInternal` — обробка одного боксу з його метаданими

```go
func readBoxStructureFromInternal(r io.ReadSeeker, bi *BoxInfo, path BoxPath, handler ReadHandler, params []interface{}) (interface{}, error) {
    // 🔹 Крок 1: Позиціонування на початок даних боксу
    if _, err := bi.SeekToPayload(r); err != nil { return nil, err }
    
    // 🔹 Крок 2: Спеціальна обробка ftyp для QuickTime сумісності
    if len(path) == 0 && bi.Type == BoxTypeFtyp() {
        var ftyp Ftyp
        Unmarshal(r, bi.Size-bi.HeaderSize, &ftyp, bi.Context)
        if ftyp.HasCompatibleBrand(BrandQT()) {
            bi.IsQuickTimeCompatible = true  // 🔹 Позначка для подальшої обробки
        }
        bi.SeekToPayload(r)  // 🔹 Повертаємось назад для повторного читання
    }
    
    // 🔹 Крок 3: Спеціальна обробка keys для iTunes metadata
    if bi.Type == BoxTypeKeys() {
        var keys Keys
        Unmarshal(r, bi.Size-bi.HeaderSize, &keys, bi.Context)
        bi.QuickTimeKeysMetaEntryCount = int(keys.EntryCount)  // 🔹 Збереження для ilst item парсингу
        bi.SeekToPayload(r)
    }
    
    // 🔹 Крок 4: Оновлення контексту для спеціальних боксів
    ctx := bi.Context
    if bi.Type == BoxTypeWave() { ctx.UnderWave = true }
    else if bi.Type == BoxTypeIlst() { ctx.UnderIlst = true }
    else if bi.UnderIlst && !bi.UnderIlstMeta && IsIlstMetaBoxType(bi.Type) {
        ctx.UnderIlstMeta = true
        if bi.Type == StrToBoxType("----") { ctx.UnderIlstFreeMeta = true }
    }
    else if bi.Type == BoxTypeUdta() { ctx.UnderUdta = true }
    
    // 🔹 Крок 5: Побудова нового шляху
    newPath := make(BoxPath, len(path)+1)
    copy(newPath, path)
    newPath[len(path)] = bi.Type
    
    // 🔹 Крок 6: Створення ReadHandle з методами
    h := &ReadHandle{
        Params:  params,
        BoxInfo: *bi,
        Path:    newPath,
        
        ReadPayload: func() (IBox, uint64, error) {
            bi.SeekToPayload(r)
            box, n, err := UnmarshalAny(r, bi.Type, bi.Size-bi.HeaderSize, bi.Context)
            childrenOffset = bi.Offset + bi.HeaderSize + n  // 🔹 Запам'ятовуємо позицію після парсингу
            return box, n, err
        },
        
        ReadData: func(w io.Writer) (uint64, error) {
            bi.SeekToPayload(r)
            size := bi.Size - bi.HeaderSize
            return io.CopyN(w, r, int64(size))  // 🔹 Пряме копіювання сирих даних
        },
        
        Expand: func(params ...interface{}) ([]interface{}, error) {
            // 🔹 Якщо ще не парсили → парсимо для визначення childrenOffset
            if childrenOffset == 0 {
                bi.SeekToPayload(r)
                _, n, err := UnmarshalAny(r, bi.Type, bi.Size-bi.HeaderSize, bi.Context)
                childrenOffset = bi.Offset + bi.HeaderSize + n
            } else {
                r.Seek(int64(childrenOffset), io.SeekStart)  // 🔹 Позиціонування на початок дітей
            }
            // 🔹 Рекурсивний виклик для вкладених боксів
            childrenSize := bi.Offset + bi.Size - childrenOffset
            return readBoxStructure(r, childrenSize, false, newPath, ctx, handler, params)
        },
    }
    
    // 🔹 Крок 7: Виклик користувацького обробника
    if val, err := handler(h); err != nil { return nil, err }
    
    // 🔹 Крок 8: Позиціонування в кінець боксу (для продовження циклу)
    bi.SeekToEnd(r)
    return val, nil
}
```

**🎯 Ключова особливість**: **Lazy evaluation** — `ReadPayload()` та `Expand()` не виконуються автоматично, а викликаються користувачем тільки за потреби, що економить час/пам'ять.

---

## 🔑 Спеціальна логіка для контексту

### 🔹 QuickTime сумісність

```go
// У readBoxStructureFromInternal:
if len(path) == 0 && bi.Type == BoxTypeFtyp() {
    var ftyp Ftyp
    Unmarshal(r, bi.Size-bi.HeaderSize, &ftyp, bi.Context)
    if ftyp.HasCompatibleBrand(BrandQT()) {
        bi.IsQuickTimeCompatible = true  // 🔹 Позначка для подальшої обробки
    }
    bi.SeekToPayload(r)  // 🔹 Повертаємось для повторного читання
}

// У readBoxStructure (після обробки боксу):
if bi.IsQuickTimeCompatible {
    ctx.IsQuickTimeCompatible = true  // 🔹 Поширення контексту на вкладені бокси
}
```

**🎯 Призначення**: Дозволити **різну логіку парсингу** для файлів, сумісних з QuickTime (напр. інший формат рядків у metadata).

---

### 🔹 iTunes metadata (ilst) контекст

```go
// Оновлення контексту для ilst:
if bi.Type == BoxTypeIlst() {
    ctx.UnderIlst = true  // 🔹 Ми всередині контейнера metadata
} else if bi.UnderIlst && !bi.UnderIlstMeta && IsIlstMetaBoxType(bi.Type) {
    ctx.UnderIlstMeta = true  // 🔹 Ми всередині конкретного метаданого (©nam, covr...)
    if bi.Type == StrToBoxType("----") {
        ctx.UnderIlstFreeMeta = true  // 🔹 Free-form metadata з mean/name/data
    }
}

// Спеціальна обробка keys боксу:
if bi.Type == BoxTypeKeys() {
    var keys Keys
    Unmarshal(r, bi.Size-bi.HeaderSize, &keys, bi.Context)
    bi.QuickTimeKeysMetaEntryCount = int(keys.EntryCount)  // 🔹 Збереження для ilst item парсингу
}
```

**🎯 Призначення**: Дозволити **правильний парсинг нумерованих metadata item** у стилі QuickTime, де тип боксу — це числовий ID, а не 4-символьний код.

---

## 🔄 Потік даних: Повний приклад обходу

```
🔹 Вхід: файл "video.mp4", handler для пошуку trun боксів
│
▼
🔹 ReadBoxStructure(r, handler):
   │
   ├── 🔹 Цикл 1: Читання ftyp боксу
   │   • bi.Type = "ftyp", bi.Size = 24
   │   • newPath = ["ftyp"]
   │   • handler(h): користувач ігнорує ftyp → return h.Expand()
   │   • Expand(): ftyp не має дітей → повертає порожній список
   │
   ├── 🔹 Цикл 2: Читання moov боксу
   │   • bi.Type = "moov", bi.Size = 10000
   │   • newPath = ["moov"]
   │   • handler(h): користувач ігнорує moov → return h.Expand()
   │   • Expand(): рекурсивний виклик readBoxStructure для дітей moov
   │   │
   │   ├── 🔹 Вкладений цикл: Читання trak боксу
   │   │   • newPath = ["moov", "trak"]
   │   │   • handler(h): ігнорує → Expand()
   │   │   │
   │   │   ├── 🔹 Вкладений цикл: Читання mdia → minf → stbl → trun
   │   │   │   • newPath = ["moov","trak","mdia","minf","stbl","trun"]
   │   │   │   • handler(h): 
   │   │   │     • if h.BoxInfo.Type == BoxTypeTrun() {
   │   │   │         trun, _, _ := h.ReadPayload()  // 🔹 Парсимо вміст!
   │   │   │         results = append(results, trun)  // 🔹 Зберігаємо результат
   │   │   │       }
   │   │   │     • return nil  // 🔹 Не розгортаємо дітей trun
   │   │   │
   │   │   └── 🔹 Повернення з вкладеного циклу
   │   │
   │   └── 🔹 Повернення з Expand() moov
   │
   ├── 🔹 Цикл 3: Читання mdat боксу (великі дані)
   │   • handler(h): користувач пропускає mdat → return nil
   │   • ⚡ Економія: не парсимо великі дані, тільки пропускаємо
   │
   ▼
🔹 Вихід: []interface{} з знайденими Trun структурами
```

**🎯 Ключова оптимізація**: Користувач **сам вирішує**, які бокси парсити, які пропускати, які розгортати — це дозволяє уникнути зайвої роботи.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Пошук всіх trun боксів для отримання таймстемпів

```go
func extractFrameTimestamps(filePath string) ([]FrameTimestamp, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    var timestamps []FrameTimestamp
    
    // 🔹 Обробник для пошуку trun боксів
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeTrun() {
            // 🔹 Парсимо вміст trun
            box, _, err := h.ReadPayload()
            if err != nil { return nil, err }
            
            if trun, ok := box.(*mp4.Trun); ok {
                // 🔹 Обробляємо кожен семпл
                for i := uint32(0); i < trun.SampleCount; i++ {
                    entry := trun.Entries[i]
                    ctsOffset := trun.GetSampleCompositionTimeOffset(int(i))
                    
                    timestamps = append(timestamps, FrameTimestamp{
                        Index:     int(i),
                        Duration:  entry.SampleDuration,
                        PTSOffset: ctsOffset,
                        Size:      entry.SampleSize,
                    })
                }
            }
        }
        // 🔹 Рекурсивно обробити вкладені бокси
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(f, handler)
    return timestamps, err
}

type FrameTimestamp struct {
    Index     int
    Duration  uint32
    PTSOffset int64
    Size      uint32
}
```

---

### 🔹 Приклад 2: Експорт сирих даних конкретного боксу

```go
func extractBoxData(filePath string, targetPath mp4.BoxPath) ([]byte, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    var result []byte
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Перевірка: чи співпадає поточний шлях з цільовим?
        if h.Path.compareWith(targetPath) == (false, true) {  // ✅ точне співпадіння
            var buf bytes.Buffer
            // 🔹 Копіюємо сирі дані боксу у буфер
            _, err := h.ReadData(&buf)
            if err != nil { return nil, err }
            result = buf.Bytes()
        }
        // 🔹 Продовжуємо обхід для пошуку інших екземплярів
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(f, handler)
    return result, err
}

// 🔹 Використання: експорт даних аватарки з iTunes metadata
avatarData, err := extractBoxData("video.mp4", 
    mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeUdta(), mp4.BoxTypeIlst(), 
                mp4.StrToBoxType("covr"), mp4.BoxTypeData()})
```

---

### 🔹 Приклад 3: Аналіз структури файлу для дебагу

```go
func printBoxTree(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Відступ залежно від глибини
        indent := strings.Repeat("  ", len(h.Path)-1)
        
        // 🔹 Форматування інформації про бокс
        info := fmt.Sprintf("%s📦 %s @ offset=%d, size=%d", 
            indent, 
            h.BoxInfo.Type.String(), 
            h.BoxInfo.Offset, 
            h.BoxInfo.Size)
        
        // 🔹 Додаткова інформація для відомих типів
        if h.BoxInfo.Type == mp4.BoxTypeTrun() {
            box, _, _ := h.ReadPayload()
            if trun, ok := box.(*mp4.Trun); ok {
                info += fmt.Sprintf(" (samples=%d)", trun.SampleCount)
            }
        }
        
        fmt.Println(info)
        
        // 🔹 Рекурсивно обробити дітей
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(f, handler)
    return err
}

// 🔹 Приклад виводу:
// 📦 ftyp @ offset=0, size=24
// 📦 moov @ offset=24, size=10240
//   📦 trak @ offset=100, size=2048
//     📦 tkhd @ offset=108, size=92
//     📦 mdia @ offset=200, size=1848
//       📦 mdhd @ offset=208, size=32
//       📦 hdlr @ offset=240, size=44
//       📦 minf @ offset=284, size=1764
//         📦 stbl @ offset=300, size=1748
//           📦 stts @ offset=308, size=28
//           📦 stss @ offset=336, size=20
//           📦 trun @ offset=356, size=120 (samples=10)  ← додаткова інформація!
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути викликати `h.Expand()` | Вкладені бокси не обробляються → пропуск даних | Завжди викликайте `return h.Expand()` у кінці обробника, якщо потрібна рекурсія |
| Неправильне використання `ReadPayload()` | Подвійне читання → зсув позиції → помилки парсингу | Викликайте `ReadPayload()` тільки один раз на бокс, або використовуйте `ReadData()` для сирих даних |
| Ігнорування контексту (`UnderIlst`, `IsQuickTimeCompatible`) | Неправильний парсинг metadata → "кракозябри" в текстах | Завжди передавайте `bi.Context` у `UnmarshalAny()` та перевіряйте контекстні прапорці |
| Неправильна обробка `totalSize` у вкладених циклах | Переповнення буфера → помилка "too large box size" | Передавайте `totalSize` у рекурсивні виклики та оновлюйте його після кожного боксу |
| Забути `SeekToEnd()` після обробки | Наступний бокс читається з неправильної позиції → пошкодження даних | Завжди викликайте `bi.SeekToEnd(r)` у кінці `readBoxStructureFromInternal` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні ReadHandler:
    • Завжди перевіряйте h.BoxInfo.Type для фільтрації потрібних боксів
    • Викликайте h.ReadPayload() тільки для боксів, вміст яких потрібен
    • Викликайте h.Expand() для рекурсивної обробки вкладених структур
    • Повертайте val/interface{} для збору результатів, якщо потрібно

[ ] Для оптимізації продуктивності:
    • Уникайте парсингу великих боксів (mdat) через ReadPayload() — використовуйте ReadData() або пропускайте
    • Фільтруйте бокси за типом на ранніх етапах, щоб уникнути зайвих викликів
    • Використовуйте BoxPath.compareWith() для швидкого пошуку за шляхом

[ ] Для роботи з контекстом:
    • Передавайте bi.Context у UnmarshalAny() для коректної обробки версій/прапорців
    • Перевіряйте ctx.UnderIlst, ctx.IsQuickTimeCompatible для спеціальної логіки
    • Оновлюйте контекст при обробці ftyp/keys для iTunes metadata

[ ] Для дебагу:
    • Логуйте шлях: log.Printf("🔍 Path: %v", h.Path)
    • Виводьте метадані: log.Printf("📦 %s @ %d+%d", h.BoxInfo.Type, h.BoxInfo.Offset, h.BoxInfo.Size)
    • Використовуйте printBoxTree() для візуалізації структури файлу

[ ] Для тестування:
    • Створюйте тестові файли з відомою структурою боксів
    • Перевіряйте, що handler викликається для очікуваних типів/шляхів
    • Тестуйте edge cases: порожні файли, пошкоджені заголовки, вкладеність >10 рівнів
```

---

## 🎯 Висновок

> **`read.go` — це "мозок" навігації по структурі MP4-файлів**, який забезпечує:
> • ✅ Гнучкий ітеративний обхід ієрархії боксів з користувацькою логікою
> • ✅ Lazy evaluation: парсинг/читання тільки за потреби через ReadPayload/ReadData/Expand
> • ✅ Контекстну обробку: QuickTime сумісність, iTunes metadata, спеціальні бокси
> • ✅ Ефективну роботу з великими файлами: мінімальне копіювання, пряме позиціонування
> • ✅ Безпечну рекурсію: контроль розмірів, перевірка переповнень, коректне позиціонування

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Швидкий аналіз структури вхідних fMP4-сегментів без повного парсингу
- 🔍 Точний пошук ключових боксів (trun, stts, avcC) для синхронізації та валідації
- 🔄 Гнучкість: легко додавати нові типи обробки без зміни ядра бібліотеки
- 🛡️ Надійність: коректна обробка краєвих випадків, пошкоджених файлів, різних стандартів

Потребуєте допомоги зі створенням ReadHandler для вашого сценарію (пошук таймстемпів, експорт даних, аналіз структури)? Напишіть — покажу готовий код! 🚀🔍