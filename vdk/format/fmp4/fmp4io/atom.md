# 📦 Глибокий розбір: `fmp4io` — Базові структури для Fragmented MP4 (fMP4)

Цей файл — **основа для парсингу та генерації фрагментованого MP4 (fMP4)**, що використовується для low-latency streaming (DASH, HLS fMP4, CMAF). Він надає уніфікований інтерфейс для роботи з атомами, підтримку variable-length encoding, та інструменти для навігації по ієрархічній структурі файлів.

---

## 🗺️ Архітектурна схема fmp4io

```
┌────────────────────────────────────────┐
│ 📦 fmp4io — Fragmented MP4 Core       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Atom interface — уніфікований доступ│
│  • Tag (fourcc) — ідентифікація атомів │
│  • FullAtom — базовий клас з version/flags│
│  • Dummy — fallback для невідомих атомів│
│  • ReadFileAtoms() — парсинг файлу     │
│                                         │
│  🔄 Ієрархія fMP4 атомів:              │
│  [ftyp][styp][moov][moof][mdat]...    │
│                ↑     ↑     ↑           │
│           init  segment  media data   │
│                                         │
│  📡 Підтримка атомів:                   │
│  • FTYP/STYP — File/Segment Type      │
│  • MOOV — Movie metadata (init)       │
│  • MOOF — Movie Fragment (metadata)   │
│  • SIDX — Segment Index (seek table)  │
│  • Dummy — невідомі атоми             │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Tag (fourcc) — ідентифікація атомів

### 🔧 Реалізація:

```go
type Tag uint32

func (a Tag) String() string {
    var b [4]byte
    pio.PutU32BE(b[:], uint32(a))  // Big-Endian конвертація
    for i := 0; i < 4; i++ {
        if b[i] == 0 { b[i] = ' ' }  // нулі → пробіли для читабельності
    }
    return string(b[:])
}

func StringToTag(tag string) Tag {
    var b [4]byte
    copy(b[:], []byte(tag))  // доповнення нулями якщо tag < 4 символів
    return Tag(pio.U32BE(b[:]))
}
```

### 🔍 Приклади тегів для fMP4:

```
• 'ftyp' (0x66747970) — File Type Box (ініціалізація)
• 'styp' (0x73747970) — Segment Type Box (для fMP4 сегментів)
• 'moov' (0x6D6F6F76) — Movie Box (метадані, init segment)
• 'moof' (0x6D6F6F66) — Movie Fragment Box (метадані фрагменту) ⭐
• 'mdat' (0x6D646174) — Media Data Box (сира медіа-інформація)
• 'sidx' (0x73696478) — Segment Index Box (таблиця seek) ⭐
```

### ✅ Ваш use-case: фільтрація атомів за типом

```go
// FilterFragmentAtoms — отримання тільки fMP4-специфічних атомів
func FilterFragmentAtoms(atoms []fmp4io.Atom) []fmp4io.Atom {
    fragmentTags := map[fmp4io.Tag]bool{
        fmp4io.StringToTag("moof"): true,  // Movie Fragment
        fmp4io.StringToTag("sidx"): true,  // Segment Index
        fmp4io.StringToTag("styp"): true,  // Segment Type
    }
    
    result := make([]fmp4io.Atom, 0)
    for _, atom := range atoms {
        if fragmentTags[atom.Tag()] {
            result = append(result, atom)
        }
        // Рекурсивний пошук у дочірніх атомах
        for _, child := range atom.Children() {
            result = append(result, FilterFragmentAtoms([]fmp4io.Atom{child})...)
        }
    }
    return result
}

// Приклад: отримання всіх moof атомів для аналізу фрагментів
moofs := FilterFragmentAtoms(atoms)
for _, moof := range moofs {
    log.Printf("Found fragment at offset %d, size %d", moof.Pos())
}
```

---

## 🔑 2. Atom interface — уніфікований доступ до атомів

### 🔧 Інтерфейс:

```go
type Atom interface {
    Pos() (int, int)              // offset, size у файлі
    Tag() Tag                     // fourcc код (напр. 'moof')
    Marshal([]byte) int           // серіалізація у байти
    Unmarshal([]byte, int) (int, error)  // десеріалізація з байт
    Len() int                     // розмір атому у байтах
    Children() []Atom             // дочірні атоми (рекурсивна структура)
}
```

### 🔍 Призначення:
- **Уніфікація**: будь-який атом (moof, sidx, тощо) реалізує цей інтерфейс
- **Рекурсія**: `Children()` дозволяє навігацію по дереву атомів
- **Сериалізація**: `Marshal`/`Unmarshal` для запису/читання у бінарний формат

### ✅ Ваш use-case: пошук атому за тегом

```go
// FindFragmentBySeqnum — пошук фрагменту за послідовним номером
func FindFragmentBySeqnum(atoms []fmp4io.Atom, seqnum uint32) (*MovieFrag, error) {
    for _, atom := range atoms {
        if atom.Tag() != fmp4io.MOOF {
            continue
        }
        
        moof, ok := atom.(*MovieFrag)
        if !ok {
            continue
        }
        
        // Перевірка sequence number у mfhd header
        if moof.Header != nil && moof.Header.Seqnum == seqnum {
            return moof, nil
        }
    }
    return nil, fmt.Errorf("fragment with seqnum %d not found", seqnum)
}

// Використання:
fragment, err := FindFragmentBySeqnum(atoms, 42)
if err != nil { /* handle error */ }
// fragment містить метадані для фрагменту #42
```

---

## 🔑 3. FullAtom — базовий клас для атомів з version/flags

### 🔧 Структура та призначення:

```go
type FullAtom struct {
    Version uint8   // версія формату (зазвичай 0 або 1)
    Flags   uint32  // бітові прапорці для опціональних полів
    AtomPos         // вбудована структура з offset/size
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `Version` | `uint8` | Версія формату атому (для зворотньої сумісності) | `0` = базовий, `1` = 64-бітні часи |
| `Flags` | `uint32` | Бітові прапорці для опціональних полів | `0x000001` = self-reference у dref |
| `AtomPos` | вбудований | Позиція та розмір у файлі для навігації | `Offset=1024, Size=256` |

### 🔧 Методи FullAtom:

```go
// marshalAtom — запис заголовку FullAtom
func (f FullAtom) marshalAtom(b []byte, tag Tag) (n int) {
    pio.PutU32BE(b[4:], uint32(tag))    // tag (4 байти)
    pio.PutU8(b[8:], f.Version)          // version (1 байт)
    pio.PutU24BE(b[9:], f.Flags)         // flags (3 байти, big-endian)
    return 12  // загальний розмір заголовку: 4+4+1+3 = 12
}

// atomLen — розрахунок розміру заголовку
func (f FullAtom) atomLen() int {
    return 12  // фіксований розмір для FullAtom
}

// unmarshalAtom — читання заголовку FullAtom
func (f *FullAtom) unmarshalAtom(b []byte, offset int) (n int, err error) {
    f.AtomPos.setPos(offset, len(b))  // збереження позиції
    n = 8  // пропуск size+tag
    if len(b) < n+4 {  // перевірка чи вистачає байт для version+flags
        return 0, parseErr("fullAtom", offset, nil)
    }
    f.Version = pio.U8(b[n:])      // читання version (1 байт)
    f.Flags = pio.U24BE(b[n+1:])   // читання flags (3 байти)
    n += 4  // загальний розмір version+flags
    return
}
```

### ⚠️ Критична проблема: відсутність перевірки меж у unmarshalAtom

```
У поточному коді:
    f.Flags = pio.U24BE(b[n+1:])  // ← читає 3 байти з позиції n+1

Проблема:
• Якщо len(b) < n+4 → pio.U24BE може читати за межами буфера
• Це може призвести до паніки або некоректних даних

✅ Виправлення: перевірка меж перед читанням
    func (f *FullAtom) unmarshalAtomSafe(b []byte, offset int) (n int, err error) {
        f.AtomPos.setPos(offset, len(b))
        n = 8
        if len(b) < n+4 {
            return 0, parseErr("fullAtom", offset, nil)
        }
        f.Version = pio.U8(b[n:])
        // Додаткова перевірка для U24BE
        if len(b) < n+4 {
            return 0, parseErr("fullAtom flags", offset, nil)
        }
        f.Flags = pio.U24BE(b[n+1:])
        n += 4
        return
    }
```

### ✅ Ваш use-case**: обробка прапорців у moof атомі

```go
// ParseMoofFlags — аналіз прапорців Movie Fragment
func ParseMoofFlags(moof *MovieFrag) MoofFlags {
    flags := MoofFlags{}
    
    if moof.Header != nil {
        // Приклади прапорців для mfhd (Movie Fragment Header)
        if moof.Header.Flags&0x000001 != 0 {
            flags.BaseDataOffsetPresent = true
        }
        // ... інші прапорці ...
    }
    
    // Аналіз прапорців у TrackFrag
    for _, traf := range moof.Tracks {
        if traf.Run != nil {
            runFlags := traf.Run.Flags
            if runFlags&0x01 != 0 {
                flags.DataOffsetPresent = true
            }
            if runFlags&0x04 != 0 {
                flags.FirstSampleFlagsPresent = true
            }
            // ... інші прапорці trun ...
        }
    }
    
    return flags
}

type MoofFlags struct {
    BaseDataOffsetPresent    bool
    DataOffsetPresent        bool
    FirstSampleFlagsPresent  bool
    // ... інші прапорці ...
}
```

---

## 🔑 4. Dummy — fallback для невідомих атомів

### 🔧 Структура та призначення:

```go
type Dummy struct {
    Data []byte   // сирий вміст атому (без заголовку size+tag)
    Tag_ Tag      // fourcc ідентифікатор
    AtomPos        // offset, size для навігації
}
```

### 🔍 Призначення:
- **Обробка невідомих атомів**: збереження даних атомів, які не підтримуються бібліотекою
- **Прозорий forwarding**: можливість запису невідомих атомів без парсингу
- **Дебаг/інспекція**: аналіз бінарної структури файлу

### ⚠️ Критична проблема: Unmarshal копіює весь буфер

```
У поточному коді:
    func (a *Dummy) Unmarshal(b []byte, offset int) (n int, err error) {
        (&a.AtomPos).setPos(offset, len(b))
        a.Data = b  // ← посилання на вхідний буфер, не копія!
        n = len(b)
        return
    }

Проблема:
• a.Data = b створює посилання, а не копію даних
• Якщо вхідний буфер b буде перезапписаний → a.Data також зміниться
• Це може призвести до важко відтворюваних багів

✅ Виправлення: створення копії даних
    func (a *Dummy) Unmarshal(b []byte, offset int) (n int, err error) {
        (&a.AtomPos).setPos(offset, len(b))
        a.Data = make([]byte, len(b))  // аллокація нового буфера
        copy(a.Data, b)                 // копіювання даних
        n = len(b)
        return
    }
```

### ✅ Ваш use-case**: інспекція невідомих атомів

```go
// InspectUnknownAtoms — вивід інформації про невідомі атоми
func InspectUnknownAtoms(atoms []fmp4io.Atom) {
    for _, atom := range atoms {
        if dummy, ok := atom.(*fmp4io.Dummy); ok {
            fmt.Printf("Unknown atom: %s at offset %d, size %d\n", 
                dummy.Tag().String(), dummy.Offset, dummy.Size)
            
            // Вивід перших 16 байт даних у hex
            if len(dummy.Data) > 0 {
                fmt.Printf("  First 16 bytes: %x\n", 
                    dummy.Data[:min(16, len(dummy.Data))])
            }
            
            // Спроба інтерпретації як тексту
            if isPrintable(dummy.Data) {
                fmt.Printf("  As text: %q\n", string(dummy.Data))
            }
        }
        
        // Рекурсивна обробка дітей
        for _, child := range atom.Children() {
            InspectUnknownAtoms([]fmp4io.Atom{child})
        }
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

---

## 🔑 5. ReadFileAtoms() — парсинг файлу у список атомів

### 🔧 Основна логіка:

```go
func ReadFileAtoms(r io.ReadSeeker) (atoms []Atom, err error) {
    for {
        // 1. Читання заголовку атому (8 байт: size + tag)
        offset, _ := r.Seek(0, 1)  // поточна позиція
        taghdr := make([]byte, 8)
        if _, err = io.ReadFull(r, taghdr); err != nil {
            if err == io.EOF { err = nil }  // нормальне завершення
            return
        }
        
        size := pio.U32BE(taghdr[0:])  // розмір атому
        tag := Tag(pio.U32BE(taghdr[4:]))  // fourcc код
        
        // 2. Створення атому за типом
        var atom Atom
        switch tag {
        case FTYP: atom = &FileType{}      // File Type
        case STYP: atom = &SegmentType{}   // Segment Type
        case MOOV: atom = &Movie{}         // Movie metadata
        case MOOF: atom = &MovieFrag{}     // Movie Fragment ⭐
        case SIDX: atom = &SegmentIndex{}  // Segment Index ⭐
        }
        
        if atom != nil {
            // Читання всього атому у пам'ять
            b := make([]byte, int(size))
            if _, err = io.ReadFull(r, b[8:]); err != nil { return }
            copy(b, taghdr)
            
            // Десеріалізація
            if _, err = atom.Unmarshal(b, int(offset)); err != nil { return }
            atoms = append(atoms, atom)
        } else {
            // Невідомий атом: пропуск даних
            dummy := &Dummy{Tag_: tag}
            dummy.setPos(int(offset), int(size))
            if _, err = r.Seek(int64(size)-8, 1); err != nil { return }
            atoms = append(atoms, dummy)
        }
    }
}
```

### ⚠️ Критична проблема: не підтримка 64-бітних розмірів

```
У стандарті MP4:
• Якщо size == 1 → наступні 8 байт = 64-бітний розмір
• Це потрібно для файлів > 4 ГБ (2^32 байт)

У вихідному коді:
    size := pio.U32BE(taghdr[0:])  // ← тільки 32-бітне читання!
    // ❌ Немає обробки size == 1 для 64-бітних розмірів!

Наслідки: Файли >4 ГБ не зможуть бути прочитані коректно.

✅ Виправлення:
    size := int64(pio.U32BE(taghdr[0:]))
    if size == 1 {
        // Читання 64-бітного розміру
        size64 := make([]byte, 8)
        if _, err = io.ReadFull(r, size64); err != nil { return }
        size = int64(pio.U64BE(size64))
        if size < 16 {  // мінімум: 8 (header) + 8 (size64)
            err = fmt.Errorf("invalid 64-bit size: %d", size)
            return
        }
    } else if size == 0 {
        // size=0: атом до кінця файлу
        endPos, err := r.Seek(0, 2)  // кінець файлу
        if err != nil { return }
        size = endPos - offset
        if _, err := r.Seek(offset+8, 0); err != nil { return }  // повернення
    }
```

### ✅ Ваш use-case**: безпечне читання великих fMP4 файлів

```go
// ReadFileAtomsSafe — версія з підтримкою 64-бітних розмірів
func ReadFileAtomsSafe(r io.ReadSeeker) ([]fmp4io.Atom, error) {
    var atoms []fmp4io.Atom
    
    for {
        offset, _ := r.Seek(0, 1)
        taghdr := make([]byte, 8)
        if _, err := io.ReadFull(r, taghdr); err != nil {
            if err == io.EOF { return atoms, nil }
            return nil, err
        }
        
        size := int64(pio.U32BE(taghdr[0:]))
        tag := fmp4io.Tag(pio.U32BE(taghdr[4:]))
        
        // Обробка 64-бітного розміру
        if size == 1 {
            size64 := make([]byte, 8)
            if _, err := io.ReadFull(r, size64); err != nil {
                return nil, fmt.Errorf("read 64-bit size: %w", err)
            }
            size = int64(pio.U64BE(size64))
            if size < 16 {
                return nil, fmt.Errorf("invalid 64-bit size: %d", size)
            }
        } else if size == 0 {
            endPos, err := r.Seek(0, 2)
            if err != nil { return nil, err }
            size = endPos - offset
            if _, err := r.Seek(offset+8, 0); err != nil { return nil, err }
        }
        
        // Перевірка розумності розміру
        if size < 8 || size > 1<<30 {  // 1GB ліміт для безпеки
            return nil, fmt.Errorf("invalid atom size: %d at offset %d", size, offset)
        }
        
        // ... решта логіки як у оригіналі ...
    }
}
```

---

## 🔑 6. Навігація по атомах: FindChildren

### 🔧 Рекурсивний пошук:

```go
func FindChildren(root Atom, tag Tag) Atom {
    if root.Tag() == tag {
        return root
    }
    for _, child := range root.Children() {
        if r := FindChildren(child, tag); r != nil {
            return r
        }
    }
    return nil
}
```

### ⚠️ Обмеження: повертає тільки перший знайдений атом

```
Якщо у файлі кілька атомів з однаковим тегом (напр. кілька 'moof'),
функція поверне тільки перший знайдений (у порядку обходу дерева).

✅ Для отримання всіх: реалізуйте FindChildrenAll
```

### ✅ Ваш use-case: пошук всіх фрагментів

```go
// FindAllFragments — рекурсивний пошук всіх moof атомів
func FindAllFragments(root fmp4io.Atom) []*MovieFrag {
    var fragments []*MovieFrag
    
    if root.Tag() == fmp4io.MOOF {
        if moof, ok := root.(*MovieFrag); ok {
            fragments = append(fragments, moof)
        }
    }
    
    for _, child := range root.Children() {
        fragments = append(fragments, FindAllFragments(child)...)
    }
    
    return fragments
}

// Використання: отримання всіх фрагментів для аналізу
moofs := FindAllFragments(rootAtom)
log.Printf("Found %d fragments in file", len(moofs))

for i, moof := range moofs {
    if moof.Header != nil {
        log.Printf("Fragment %d: seqnum=%d, offset=%d", 
            i, moof.Header.Seqnum, moof.Offset)
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Аналіз fMP4 файлу для streaming

```go
// AnalyzeFMP4ForStreaming — витягування метаданих для HLS/DASH
func AnalyzeFMP4ForStreaming(filename string) (*StreamingMetadata, error) {
    f, err := os.Open(filename)
    if err != nil { return nil, err }
    defer f.Close()
    
    // Парсинг атомів
    atoms, err := fmp4io.ReadFileAtoms(f)
    if err != nil { return nil, fmt.Errorf("parse atoms: %w", err) }
    
    meta := &StreamingMetadata{}
    
    // Пошук init segment (moov)
    moov := fmp4io.FindChildrenByName(atoms[0], "moov")
    if moov != nil {
        if movie, ok := moov.(*Movie); ok {
            meta.Duration = time.Duration(movie.Header.Duration) * time.Second / time.Duration(movie.Header.TimeScale)
            meta.Tracks = len(movie.Tracks)
        }
    }
    
    // Пошук фрагментів (moof)
    fragments := FindAllFragments(atoms[0])
    meta.FragmentCount = len(fragments)
    
    // Аналіз першого фрагменту для таймінгів
    if len(fragments) > 0 && fragments[0].Header != nil {
        meta.FirstSeqnum = fragments[0].Header.Seqnum
    }
    
    // Пошук sidx для seek таблиці
    sidx := fmp4io.FindChildrenByName(atoms[0], "sidx")
    if sidx != nil {
        meta.HasSeekTable = true
        // ... парсинг sidx для отримання точних offset'ів ...
    }
    
    return meta, nil
}

type StreamingMetadata struct {
    Duration      time.Duration
    Tracks        int
    FragmentCount int
    FirstSeqnum   uint32
    HasSeekTable  bool
}
```

### 🔧 Приклад: Перевірка цілісності fMP4 файлу

```go
// ValidateFMP4Structure — базова валідація структури fMP4 файлу
func ValidateFMP4Structure(r io.ReadSeeker) error {
    atoms, err := fmp4io.ReadFileAtoms(r)
    if err != nil {
        return fmt.Errorf("parse error: %w", err)
    }
    
    // Перевірка наявності обов'язкових атомів для fMP4
    hasFtyp := false
    hasStyp := false
    hasMoov := false
    hasMoof := false
    
    for _, atom := range atoms {
        switch atom.Tag() {
        case fmp4io.StringToTag("ftyp"):
            hasFtyp = true
        case fmp4io.StringToTag("styp"):
            hasStyp = true  // styp обов'язковий для fMP4 сегментів
        case fmp4io.StringToTag("moov"):
            hasMoov = true
        case fmp4io.StringToTag("moof"):
            hasMoof = true
        }
    }
    
    if !hasFtyp {
        return fmt.Errorf("missing 'ftyp' atom")
    }
    
    // Для fMP4: або moov (init), або styp+moof (сегменти)
    if !hasMoov && (!hasStyp || !hasMoof) {
        return fmt.Errorf("invalid fMP4 structure: need moov OR (styp+moof)")
    }
    
    // Перевірка що moov містить треки
    if hasMoov {
        moov := fmp4io.FindChildrenByName(atoms[0], "moov")
        if movie, ok := moov.(*Movie); ok {
            if len(movie.Tracks) == 0 {
                return fmt.Errorf("moov atom has no tracks")
            }
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"invalid atom size" для файлів >4 ГБ** | Помилка при парсингу великих файлів | Реалізуйте обробку `size == 1` для 64-бітних розмірів |
| **FindChildren повертає не той атом** | Коли є кілька атомів з однаковим тегом | Використовуйте `FindAllFragments` або перевіряйте контекст батьків |
| **Паніка при type assertion** | `atom.(*MovieFrag)` не співпадає | Завжди перевіряйте `ok` після type assertion |
| **Dummy.Data змінюється** | Посилання на вхідний буфер замість копії | Створюйте копію даних у `Dummy.Unmarshal()` |
| **Некоректний парсинг FullAtom** | Flags читаються некоректно | Додайте перевірку меж буфера перед `pio.U24BE()` |

---

## ⚡ Оптимізації для великих файлів

### 1. Lazy reading атомів:

```go
// ReadAtomHeadersOnly — читання тільки заголовків для швидкого сканування
func ReadAtomHeadersOnly(r io.ReadSeeker) ([]AtomHeader, error) {
    var headers []AtomHeader
    
    for {
        offset, _ := r.Seek(0, 1)
        taghdr := make([]byte, 8)
        if _, err := io.ReadFull(r, taghdr); err != nil {
            if err == io.EOF { break }
            return nil, err
        }
        
        size := int64(pio.U32BE(taghdr[0:]))
        tag := fmp4io.Tag(pio.U32BE(taghdr[4:]))
        
        // Обробка 64-бітного розміру
        if size == 1 {
            size64 := make([]byte, 8)
            if _, err := io.ReadFull(r, size64); err != nil { return nil, err }
            size = int64(pio.U64BE(size64))
            if size < 16 { return nil, fmt.Errorf("invalid 64-bit size") }
        } else if size == 0 {
            endPos, _ := r.Seek(0, 2)
            size = endPos - offset
            r.Seek(offset+8, 0)  // повернення
        }
        
        headers = append(headers, AtomHeader{
            Offset: offset,
            Size:   size,
            Tag:    tag,
        })
        
        // Пропуск даних атому
        if _, err := r.Seek(offset+size, 0); err != nil { return nil, err }
    }
    
    return headers, nil
}

type AtomHeader struct {
    Offset int64
    Size   int64
    Tag    fmp4io.Tag
}
```

### 2. Кешування результатів пошуку:

```go
type AtomCache struct {
    mu    sync.RWMutex
    cache map[fmp4io.Tag][]fmp4io.Atom  // tag → атоми
}

func (c *AtomCache) FindAll(root fmp4io.Atom, tag fmp4io.Tag) []fmp4io.Atom {
    c.mu.RLock()
    if atoms, ok := c.cache[tag]; ok {
        c.mu.RUnlock()
        return atoms
    }
    c.mu.RUnlock()
    
    // Пошук якщо не в кеші
    atoms := FindAllFragments(root)  // або інша функція пошуку
    
    c.mu.Lock()
    if c.cache == nil { c.cache = make(map[fmp4io.Tag][]fmp4io.Atom) }
    c.cache[tag] = atoms
    c.mu.Unlock()
    
    return atoms
}
```

### 3. Моніторинг продуктивності парсингу:

```go
type ParserMetrics struct {
    AtomsParsed   prometheus.CounterVec
    ParseLatency  prometheus.HistogramVec
    LargeAtomCount prometheus.CounterVec  // атоми >1MB
}

func (m *ParserMetrics) RecordAtom(tag fmp4io.Tag, size int, duration time.Duration) {
    m.AtomsParsed.WithLabelValues(tag.String()).Inc()
    m.ParseLatency.WithLabelValues(tag.String()).Observe(duration.Seconds())
    if size > 1<<20 {  // >1MB
        m.LargeAtomCount.WithLabelValues(tag.String()).Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання fmp4io

```go
// ✅ 1. Обробка 64-бітних розмірів атомів
if size == 1 {
    // read 64-bit size
}

// ✅ 2. Перевірка type assertion з ok
if moof, ok := atom.(*fmp4io.MovieFrag); ok {
    // use moof
} else {
    return fmt.Errorf("unexpected atom type: %T", atom)
}

// ✅ 3. Валідація часу перед конвертацією
if mp4Time.Year() < 1904 || mp4Time.Year() > 2100 {
    log.Printf("warning: suspicious MP4 time: %v", mp4Time)
}

// ✅ 4. Обмеження максимального розміру атому для безпеки
if size > 1<<30 {  // 1GB
    return fmt.Errorf("atom too large: %d bytes", size)
}

// ✅ 5. Логування з контекстом для помилок
if err != nil {
    LogParseError(err, filename)  // функція з контекстом
}

// ✅ 6. Метрики для моніторингу
metrics.RecordAtom(tag, size, time.Since(start))
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 23009-1 (DASH)](https://www.iso.org/standard/79329.html) — стандарт для fMP4 у streaming
- 📄 [CMAF Specification](https://www.iso.org/standard/74428.html) — Common Media Application Format
- 📄 [HLS fMP4 Guide](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 🧪 [Go io.ReadSeeker Documentation](https://pkg.go.dev/io#ReadSeeker) — інтерфейси для потокового читання
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди обробляйте `size == 1` для 64-бітних розмірів** — інакше файли >4 ГБ не зможуть бути прочитані.
> 2. **Перевіряйте `ok` після type assertion** — уникнення панік при несподіваних типах атомів.
> 3. **Створюйте копії даних у `Dummy.Unmarshal()`** — уникнення проблем з посиланнями на тимчасові буфери.
> 4. **Використовуйте `FindAllFragments` замість `FindChildren`** — для отримання всіх moof атомів у файлі.
> 5. **Моніторьте `LargeAtomCount`** — різке зростання може вказувати на фрагментацію файлу або проблеми з мережею.

Потрібен приклад реалізації `WriteFileAtoms` для створення власних fMP4 файлів з низького рівня, або інтеграція `fmp4io` з вашим `mse.Muxer` для генерації fMP4 фрагментів у реальному часі? Готовий допомогти! 🚀