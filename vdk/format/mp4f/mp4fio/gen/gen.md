# 📦 Глибокий розбір: `main.go` — Мета-генератор коду для атомів MP4

Цей файл — **потужний інструмент code generation**, що використовує пакет `go/ast` для парсингу DSL-опису атомів MP4 та автоматичної генерації повноцінного Go коду: структур даних, методів маршалінгу/анмаршалінгу, навігації та утиліт. Це дозволяє підтримувати складну бінарну специфікацію у компактному, людсько-читабельному форматі.

---

## 🗺️ Архітектурна схема генератора

```
┌────────────────────────────────────────┐
│ 📦 main.go — MP4 Atom Code Generator  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • DSL парсер — читання описів атомів  │
│  • AST трансформація — побудова коду  │
│  • Кодогенерація — Marshal/Unmarshal  │
│  • Типові мапінги — uint24→uint32 тощо│
│                                         │
│  🔄 Потік генерації:                    │
│  DSL функція → AST → аналіз →          │
│  генерація:                             │
│  • struct Type { fields... }           │
│  • func (Type) Marshal() []byte        │
│  • func (Type) Unmarshal() error      │
│  • func (Type) Len() int              │
│  • func (Type) Children() []Atom      │
│                                         │
│  📡 Підтримка типів:                    │
│  • Примітиви: uint8/16/24/32/64, time │
│  • Спеціальні: fixed16/32, bytes, atom│
│  • Колекції: slice, array, atoms      │
│  • Мета: _unknowns, _skip, _code      │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. DSL формат опису атомів

### 🔧 Приклад вхідного DSL:

```go
// Опис атому stts (TimeToSample) у DSL:
func stts_TimeToSample() {
    uint8("Version")
    uint24("Flags")
    uint32("_len_Entries")  // спеціальний префікс _len_ для довжини масиву
    slice("Entries", "TimeToSampleEntry")  // масив структур
}

// Опис атому trak (Track):
func trak_Track() {
    atom("Header", "TrackHeader")      // один дочірній атом
    atom("Media", "Media")             // один дочірній атом
    _unknowns()                        // підтримка невідомих атомів
}
```

### 🔍 Спеціальні директиви:

| Директива | Призначення | Приклад |
|-----------|-------------|---------|
| `uint8/16/24/32/64(name)` | Примітивні типи | `uint32("Duration")` |
| `time32/time64(name)` | Час у форматі 1904 епохи | `time64("CreateTime")` |
| `fixed16/fixed32(name)` | Фіксована крапка 16.16 | `fixed32("TrackWidth")` |
| `bytes(name, size)` | Фіксований масив байт | `bytes("CompressorName", "32")` |
| `bytesleft(name)` | Залишок байт до кінця | `bytesleft("Data")` |
| `slice(name, type)` | Масив елементів типу | `slice("Entries", "TimeToSampleEntry")` |
| `array(name, type, len)` | Фіксований масив | `array("Matrix", "int32", "9")` |
| `atom(name, type)` | Один дочірній атом | `atom("Header", "TrackHeader")` |
| `atoms(name, type)` | Масив дочірніх атомів | `atoms("Tracks", "Track")` |
| `_unknowns()` | Підтримка невідомих атомів | `_unknowns()` |
| `_skip(n)` | Пропуск n байт | `_skip("10")` |
| `_code(...)` | Вставка кастомного коду | `_code(mar: ..., len: ..., unmar: ...)` |

### ✅ Ваш use-case: додавання нового атому

```go
// Додавання опису нового атому 'styp' (Segment Type) у DSL:
func styp_SegmentType() {
    uint32("MajorBrand")
    uint32("MinorVersion")
    slice("CompatibleBrands", "uint32")  // масив fourcc кодів
    _unknowns()  // для сумісності з майбутніми розширеннями
}

// Після запуску генератора:
// 1. Створиться struct SegmentType з полями
// 2. Згенеруються методи Marshal/Unmarshal/Len/Children
// 3. Додасться константа STYP = Tag(0x73747970)
// 4. Додасться метод (SegmentType) Tag() Tag

// Використання у коді:
var styp mp4io.SegmentType
err := styp.Unmarshal(data, 0)  // парсинг з байтів
if err != nil { /* handle error */ }
fmt.Printf("Brand: %s, Version: %d\n", 
    mp4io.Tag(styp.MajorBrand).String(), 
    styp.MinorVersion)
```

---

## 🔑 2. Типові мапінги та конвертації

### 🔧 Мапінг DSL типів у Go типи:

```go
func typegetvartype(typ string) string {
    switch typ {
    case "uint8": return "uint8"
    case "uint16": return "uint16"
    case "uint24": return "uint32"  // ⚠️ uint24 не існує у Go → uint32
    case "uint32": return "uint32"
    case "uint64": return "uint64"
    case "int16": return "int16"
    case "int32": return "int32"
    case "time32", "time64": return "time.Time"
    case "fixed16", "fixed32": return "float64"
    case "bytes": return "[N]byte"  // N з другого аргументу
    case "bytesleft": return "[]byte"
    case "slice": return "[]Type"   // Type з другого аргументу
    case "array": return "[N]Type"  // N з третього аргументу
    case "atom": return "*Type"     // Type з другого аргументу
    case "atoms": return "[]*Type"  // Type з другого аргументу
    }
    return ""
}
```

### 🔧 Мапінг функцій читання/запису:

```go
func typegetgetfn(typ string) string {
    switch typ {
    case "uint8": return "pio.U8"
    case "uint16": return "pio.U16BE"
    case "uint24": return "pio.U24BE"
    case "uint32": return "pio.U32BE"
    case "int16": return "pio.I16BE"
    case "int32": return "pio.I32BE"
    case "uint64": return "pio.U64BE"
    case "time32": return "GetTime32"
    case "time64": return "GetTime64"
    case "fixed16": return "GetFixed16"
    case "fixed32": return "GetFixed32"
    default: return "Get" + typ  // для кастомних типів
    }
}

func typegetputfn(typ string) string {
    // Аналогічно для запису: pio.PutU32BE, PutTime32, тощо
}
```

### ⚠️ Критичний момент: uint24 → uint32

```
У MP4 специфікації є 24-бітні поля (напр. у stts entry).
У Go немає uint24 типу → використовуємо uint32.

Наслідки:
• При записі: pio.PutU24BE(b, uint32(value)) — записує тільки 3 байти
• При читанні: value = pio.U24BE(b) — читає 3 байти у uint32

✅ Це коректно, але важливо пам'ятати:
• Значення > 0xFFFFFF (16777215) будуть обрізані при записі
• При читанні старший байт завжди 0
```

### ✅ Ваш use-case: безпечна робота з 24-бітними полями

```go
// Write24BitSafe — запис 24-бітного значення з перевіркою діапазону
func Write24BitSafe(b []byte, value uint32) error {
    if value > 0xFFFFFF {
        return fmt.Errorf("value 0x%X exceeds 24-bit range", value)
    }
    pio.PutU24BE(b, value)
    return nil
}

// Read24Bit — читання 24-бітного значення
func Read24Bit(b []byte) uint32 {
    return pio.U24BE(b)  // повертає uint32 з молодших 24 біт
}

// Використання у згенерованому коді:
func (self *TimeToSampleEntry) Marshal(b []byte) int {
    if err := Write24BitSafe(b, self.Count); err != nil {
        // У згенерованому коді помилки ігноруються для швидкодії
        // У production краще повертати error
    }
    // ...
}
```

---

## 🔑 3. Генерація методів Marshal/Unmarshal/Len/Children

### 🔧 Структура згенерованого коду:

```go
// Для DSL: func stts_TimeToSample() { uint8("Version"); slice("Entries", "TimeToSampleEntry") }

// 1. Оголошення структури:
type TimeToSample struct {
    Version uint8
    Flags   uint32  // з uint24
    Entries []TimeToSampleEntry
    AtomPos  // вбудоване поле для offset/size
}

// 2. Метод Marshal (публічний, з обгорткою):
func (self TimeToSample) Marshal(b []byte) (n int) {
    pio.PutU32BE(b[4:], uint32(STTS))  // запис tag
    n += self.marshal(b[8:]) + 8        // виклик приватного marshal
    pio.PutU32BE(b[0:], uint32(n))      // запис загального розміру
    return
}

// 3. Метод marshal (приватний, основна логіка):
func (self TimeToSample) marshal(b []byte) (n int) {
    pio.PutU8(b[n:], self.Version); n += 1
    pio.PutU24BE(b[n:], self.Flags); n += 3
    pio.PutU32BE(b[n:], uint32(len(self.Entries))); n += 4
    for _, entry := range self.Entries {
        n += PutTimeToSampleEntry(b[n:], entry)  // виклик для елемента
    }
    return
}

// 4. Метод Len (розрахунок розміру):
func (self TimeToSample) Len() (n int) {
    n += 8  // заголовок атому
    n += 1 + 3 + 4  // Version + Flags + count
    n += LenTimeToSampleEntry * len(self.Entries)  // розмір масиву
    return
}

// 5. Метод Unmarshal (парсинг):
func (self *TimeToSample) Unmarshal(b []byte, offset int) (n int, err error) {
    (&self.AtomPos).setPos(offset, len(b))  // збереження позиції
    n += 8  // пропуск заголовку
    // ... читання полів з перевіркою довжини ...
    _len_Entries := pio.U32BE(b[n:]); n += 4
    self.Entries = make([]TimeToSampleEntry, _len_Entries)
    for i := range self.Entries {
        self.Entries[i] = GetTimeToSampleEntry(b[n:])
        n += LenTimeToSampleEntry
    }
    return
}

// 6. Метод Children (навігація):
func (self TimeToSample) Children() (r []Atom) {
    // Для простих атомів без дочірніх: порожній слайс
    return
}
```

### 🔧 Обробка складних випадків:

#### ✅ Атоми з дочірніми атомами (`atom`/`atoms`):

```go
// Для DSL: atom("Header", "TrackHeader")
// У marshal:
if self.Header != nil {
    n += self.Header.Marshal(b[n:])  // рекурсивний виклик
}

// У Unmarshal:
case TKHD:  // tag TrackHeader
    atom := &TrackHeader{}
    if _, err = atom.Unmarshal(b[n:n+size], offset+n); err != nil {
        return n, parseErr("tkhd", n+offset, err)
    }
    self.Header = atom  // збереження у поле

// У Children:
if self.Header != nil {
    r = append(r, self.Header)  // додавання у список дітей
}
```

#### ✅ Масиви атомів (`atoms`):

```go
// Для DSL: atoms("Tracks", "Track")
// У Unmarshal:
case TRAK:
    atom := &Track{}
    // ... парсинг ...
    if len(self.Tracks) > 100 {  // захист від зловмисних файлів
        return n, errors.New("too many tracks")
    }
    self.Tracks = append(self.Tracks, atom)

// У Children:
for _, atom := range self.Tracks {
    r = append(r, atom)  // додавання всіх треків у список дітей
}
```

#### ✅ Невідомі атоми (`_unknowns`):

```go
// Для DSL: _unknowns()
// У Unmarshal (default case у switch):
default:
    atom := &Dummy{Tag_: tag, Data: b[n:n+size]}
    // ... парсинг ...
    if len(self.Unknowns) > 100 {  // захист
        return n, errors.New("too many unknowns")
    }
    self.Unknowns = append(self.Unknowns, atom)

// У Children:
r = append(r, self.Unknowns...)  // додавання невідомих атомів
```

---

## 🔑 4. Обробка помилок та валідація

### 🔧 Патерн `parseErr` для контексту помилок:

```go
func parseErr(debug string, offset int, prev error) error {
    _prev, _ := prev.(*ParseError)
    return &ParseError{
        Debug:  debug,      // назва поля/атому де сталася помилка
        Offset: offset,     // позиція у файлі
        prev:   _prev,      // ланцюжок попередніх помилок
    }
}

// Приклад виводу:
// mp4io: parse error: TagSizeInvalid:256,stts:260,TimeToSampleEntry:268
// Це означає: помилка у розмірі атому на 256, потім у stts на 260, потім у записі на 268
```

### 🔧 Перевірка довжини буфера:

```go
// У згенерованому Unmarshal:
if len(b) < n+4 {  // перевірка чи вистачає байт для читання uint32
    err = parseErr("Duration", n+offset, err)
    return
}
self.Duration = pio.I32BE(b[n:])
n += 4
```

### ⚠️ Критична проблема: ігнорування помилок у маршалінгу

```
У згенерованому Marshal/putxx функціях помилки не повертаються:
    pio.PutU32BE(b[n:], self.Duration)  // ← ніякої перевірки!

Це припустимо для маршалінгу, бо:
• Буфер b заздалегідь аллокований з правильним розміром (через Len())
• Індексація b[n:] завжди валідна якщо Len() обчислено коректно

Але для безпеки у production краще додавати перевірки:
    if n+4 > len(b) {
        return n, fmt.Errorf("buffer too small for Duration")
    }
```

### ✅ Ваш use-case: безпечний маршалінг з перевіркою

```go
// PutU32BESafe — версія з перевіркою меж буфера
func PutU32BESafe(b []byte, offset int, value uint32) error {
    if offset+4 > len(b) {
        return fmt.Errorf("buffer overflow at offset %d", offset)
    }
    pio.PutU32BE(b[offset:], value)
    return nil
}

// Використання у згенерованому коді (якщо потрібна безпека):
func (self TimeToSample) marshalSafe(b []byte) (n int, err error) {
    if err := PutU32BESafe(b, n, uint32(STTS)); err != nil { return n, err }; n += 4
    // ... інші поля ...
    return n, nil
}
```

---

## 🔑 5. Робота з AST: парсинг та трансформація

### 🔧 Основні функції роботи з AST:

```go
// getexprs — отримання значення з ast.Expr
func getexprs(e ast.Expr) string {
    if lit, ok := e.(*ast.BasicLit); ok {
        return lit.Value  // константи: "32", "0x1234"
    }
    if ident, ok := e.(*ast.Ident); ok {
        return ident.Name  // ідентифікатори: "Duration", "TrackHeader"
    }
    return ""
}

// simplecall — створення виклику функції
func simplecall(fun string, args ...string) *ast.ExprStmt {
    _args := []ast.Expr{}
    for _, s := range args {
        _args = append(_args, ast.NewIdent(s))
    }
    return &ast.ExprStmt{
        X: &ast.CallExpr{
            Fun:  ast.NewIdent(fun),
            Args: _args,
        },
    }
}

// newdecl — створення оголошення функції
func newdecl(recv, name string, params, res []*ast.Field, stmts []ast.Stmt) *ast.FuncDecl {
    return &ast.FuncDecl{
        Recv: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{ast.NewIdent("self")}, Type: ast.NewIdent(recv)}}},
        Name: ast.NewIdent(name),
        Type: &ast.FuncType{Params: &ast.FieldList{List: params}, Results: &ast.FieldList{List: res}},
        Body: &ast.BlockStmt{List: stmts},
    }
}
```

### 🔧 Кодова клонування для `_code` директиви:

```go
// codeclonereplace — заміна викликів doit на кастомний код
func codeclonereplace(stmts []ast.Stmt, doit []ast.Stmt) (out []ast.Stmt) {
    out = append([]ast.Stmt(nil), stmts...)  // глибоке копіювання
    for i := range out {
        if ifstmt, ok := out[i].(*ast.IfStmt); ok {
            // Рекурсивна обробка тіла if/else
            newifstmt := &ast.IfStmt{
                Cond: ifstmt.Cond,
                Body: &ast.BlockStmt{List: codeclonereplace(ifstmt.Body.List, doit)},
            }
            if ifstmt.Else != nil {
                newifstmt.Else = &ast.BlockStmt{List: codeclonereplace(ifstmt.Else.(*ast.BlockStmt).List, doit)}
            }
            out[i] = newifstmt
        } else if exprstmt, ok := out[i].(*ast.ExprStmt); ok {
            if callexpr, ok := exprstmt.X.(*ast.CallExpr); ok {
                if getexprs(callexpr.Fun) == "doit" {
                    out[i] = &ast.BlockStmt{List: doit}  // заміна виклику на код
                }
            }
        }
    }
    return
}
```

### ✅ Ваш use-case: додавання кастомної логіки через `_code`

```go
// DSL з кастомним кодом для обробки прапорців у TrackFragRun:
func trun_TrackFragRun() {
    uint8("Version")
    uint24("Flags")
    uint32("_len_Entries")
    
    // Кастомна логіка для опціональних полів залежно від прапорців
    _code(
        mar: func() {
            if self.Flags&TRUN_DATA_OFFSET != 0 {
                pio.PutU32BE(b[n:], self.DataOffset); n += 4
            }
        },
        len: func() {
            if self.Flags&TRUN_DATA_OFFSET != 0 {
                n += 4
            }
        },
        unmar: func() {
            if self.Flags&TRUN_DATA_OFFSET != 0 {
                self.DataOffset = pio.U32BE(b[n:]); n += 4
            }
        },
    )
    
    slice("Entries", "TrackFragRunEntry")
}

// Генератор вставить цей код у відповідні методи,
// забезпечуючи гнучку обробку опціональних полів.
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Крок 1: Створення DSL файлу з описом атомів

```go
// atoms.dsl.go — DSL опис нових атомів
package mp4io_dsl

import "time"

// Опис атому 'udta' (User Data) з підтримкою метаданих
func udta_UserData() {
    _unknowns()  // підтримка будь-яких дочірніх атомів
}

// Опис атому '©nam' (Name metadata) у udta
func _nam_NameMetadata() {
    bytesleft("Data")  // сирий текст у UTF-8
}

// Опис атому 'keys' (Metadata Keys) для ISO BMFF
func keys_MetadataKeys() {
    uint8("Version")
    uint24("Flags")
    uint32("_len_Entries")
    array("Namespace", "uint32", "4")  // fourcc код простору імен
    bytesleft("Keys")  // список ключів у форматі null-terminated strings
}
```

### 🔧 Крок 2: Запуск генератора

```bash
# Генерація коду з DSL:
go run main.go gen atoms.dsl.go mp4io_generated.go

# Результат: mp4io_generated.go містить:
# • struct UserData, NameMetadata, MetadataKeys
# • Методи Marshal/Unmarshal/Len/Children для кожного
# • Константи UDTA, _nam, KEYS
# • Інтеграцію з існуючим кодом mp4io
```

### 🔧 Крок 3: Використання згенерованого коду

```go
// main.go — використання згенерованих атомів
package main

import (
    "os"
    "github.com/deepch/vdk/format/mp4/mp4io"
)

func main() {
    // Читання файлу
    data, _ := os.ReadFile("video.mp4")
    
    // Парсинг атому udta
    var udta mp4io.UserData
    n, err := udta.Unmarshal(data, 0)
    if err != nil {
        panic(err)
    }
    
    // Пошук метаданих імені
    for _, child := range udta.Children() {
        if child.Tag() == mp4io._nam {  // fourcc '©nam'
            nameMeta := child.(*mp4io.NameMetadata)
            fmt.Printf("Video name: %s\n", string(nameMeta.Data))
        }
    }
    
    // Створення нового атому keys
    keys := mp4io.MetadataKeys{
        Version:   0,
        Flags:     0,
        Namespace: [4]uint32{mp4io.StringToTag("mdir")},
        Keys:      []byte("title\x00artist\x00"),
    }
    
    // Серіалізація
    buf := make([]byte, keys.Len())
    keys.Marshal(buf)
    
    // Запис у файл або мережу...
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Невірний розмір у Len()** | Marshal пише за межі буфера | Перевірте чи typegetlen повертає правильне значення для всіх типів |
| **Паніка при type assertion** | `atom.(*TrackHeader)` не співпадає | Завжди перевіряйте `ok` після type assertion у ручному коді |
| **Переповнення буфера** | Index out of range у Marshal | Додайте перевірку `if n+size > len(b)` перед записом |
| **Некоректний парсинг масивів** | `slice` читає неправильну кількість елементів | Переконайтеся, що `_len_Entries` читається перед створенням слайсу |
| **Втрата невідомих атомів** | `_unknowns` не зберігає атоми | Переконайтеся, що default case у switch додає атоми у `self.Unknowns` |

---

## ⚡ Оптимізації генерації

### 1. Кешування результатів парсингу:

```go
// Додавання кешування у згенерований Unmarshal:
type AtomCache struct {
    mu    sync.RWMutex
    cache map[Tag][]Atom
}

func (c *AtomCache) Parse(b []byte, offset int, tag Tag) ([]Atom, error) {
    c.mu.RLock()
    if atoms, ok := c.cache[tag]; ok {
        c.mu.RUnlock()
        return atoms, nil
    }
    c.mu.RUnlock()
    
    // Парсинг якщо не в кеші
    atoms, err := parseAtoms(b, offset, tag)
    
    c.mu.Lock()
    if c.cache == nil { c.cache = make(map[Tag][]Atom) }
    c.cache[tag] = atoms
    c.mu.Unlock()
    
    return atoms, nil
}
```

### 2. Генерація з підтримкою пулу буферів:

```go
// Додавання опції у генератор для пулінгу:
func genatomdeclWithPool(origfn *ast.FuncDecl, usePool bool) {
    if usePool {
        // Додавання отримання/повернення буфера з sync.Pool
        // у початок/кінець Marshal/Unmarshal
    }
    // ... решта логіки ...
}
```

### 3. Моніторинг продуктивності генерації:

```go
type GeneratorMetrics struct {
    AtomsGenerated prometheus.CounterVec
    GenLatency    prometheus.HistogramVec
    CodeSize      prometheus.GaugeVec
}

func (m *GeneratorMetrics) RecordGeneration(atomName string, duration time.Duration, codeSize int) {
    m.AtomsGenerated.WithLabelValues(atomName).Inc()
    m.GenLatency.WithLabelValues(atomName).Observe(duration.Seconds())
    m.CodeSize.WithLabelValues(atomName).Set(float64(codeSize))
}
```

---

## 📋 Чек-лист безпечного використання генератора

```go
// ✅ 1. Перевірка діапазону для 24-бітних полів
if value > 0xFFFFFF {
    return fmt.Errorf("value exceeds 24-bit range")
}

// ✅ 2. Валідація довжини буфера перед записом
if n+4 > len(b) {
    return n, fmt.Errorf("buffer too small")
}

// ✅ 3. Перевірка type assertion з ok
if header, ok := atom.(*TrackHeader); ok {
    // use header
} else {
    return fmt.Errorf("unexpected atom type: %T", atom)
}

// ✅ 4. Обмеження максимальної кількості атомів для захисту
if len(self.Tracks) > 100 {
    return errors.New("too many tracks")
}

// ✅ 5. Логування з контекстом для помилок
if err != nil {
    LogParseError(err, filename)  // функція з контекстом
}

// ✅ 6. Тестування згенерованого коду
// • Юніт-тести для Marshal/Unmarshal round-trip
// • Fuzzing для перевірки стійкості до некоректних вхідних даних
// • Бенчмарки для перевірки продуктивності
```

---

## 🔗 Корисні посилання

- 💻 [Go go/ast Package](https://pkg.go.dev/go/ast) — робота з AST Go коду
- 💻 [Go go/parser Package](https://pkg.go.dev/go/parser) — парсинг вихідного коду
- 📄 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [Code Generation in Go](https://go.dev/blog/generate) — офіційний гайд по генераторам
- 🧪 [Go testing Package](https://pkg.go.dev/testing) — написання тестів для згенерованого коду

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди тестуйте згенерований код** — юніт-тести на round-trip (Marshal → Unmarshal → порівняння) виявлять помилки у генерації.
> 2. **Додайте fuzzing для Unmarshal** — генератор випадкових бінарних даних допоможе знайти edge cases та вразливості.
> 3. **Використовуйте `_code` для складної логіки** — але документуйте кастомний код, щоб уникнути плутанини при підтримці.
> 4. **Моніторьте розмір згенерованого коду** — надмірна генерація може призвести до великих бінарників та повільної компіляції.
> 5. **Версіонуйте DSL формат** — зміни у синтаксисі DSL можуть зламати існуючі описи атомів.

Потрібен приклад fuzz-тесту для згенерованого `Unmarshal` методу, або інтеграція цього генератора у ваш CI/CD pipeline для автоматичної перевірки змін у специфікації MP4? Готовий допомогти! 🚀