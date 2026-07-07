# 📦 Глибокий розбір: DSL файл для генерації атомів MP4

Цей файл — **вхідний DSL (Domain Specific Language) для кодогенератора**, що описує структуру атомів формату ISO BMFF (MP4) у компактному, людсько-читабельному форматі. Він використовується разом з `main.go` для автоматичної генерації повноцінного Go коду: структур даних, методів серіалізації/десеріалізації, та навігації.

---

## 🗺️ Архітектурна схема DSL

```
┌────────────────────────────────────────┐
│ 📦 atoms.dsl.go — MP4 Atom DSL        │
├────────────────────────────────────────┤
│                                         │
│  🔑 Синтаксис функцій:                  │
│  • tag_StructName() — опис атому       │
│  • tag = fourcc код (moov, trak...)    │
│  • StructName = Go тип (Movie, Track)  │
│                                         │
│  🔧 Директиви опису полів:              │
│  • Примітиви: uint8/16/24/32/64(name)  │
│  • Знакові: int16/32(name)             │
│  • Час: time32/64(name)                │
│  • Fixed-point: fixed16/32(name)       │
│  • Байти: bytes(name,N), bytesleft(name)│
│  • Колекції: slice(name,Type), array(...)│
│  • Атоми: atom(name,Type), atoms(...)  │
│  • Мета: _skip, _unknowns, _code...    │
│                                         │
│  🔄 Потік генерації:                    │
│  DSL функція → AST парсинг →           │
│  генерація: struct + Marshal/Unmarshal│
│  + Len + Children + Tag + константи   │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Синтаксис та конвенції іменування

### 🔧 Формат оголошення атому:

```go
// Загальний шаблон:
func <fourcc>_<StructName>() {
    // директиви полів...
}

// Приклади:
func moov_Movie() { ... }      // атом 'moov' → struct Movie
func mvhd_MovieHeader() { ... } // атом 'mvhd' → struct MovieHeader
func trak_Track() { ... }      // атом 'trak' → struct Track
```

### 🔍 Особливі випадки:

```go
// Атом з підкресленням у fourcc (напр. '©nam' → '_nam'):
func _nam_NameMetadata() { ... }

// Структура без відповідного fourcc (допоміжні типи):
func TimeToSampleEntry() { ... }  // не атом, а елемент таблиці
func SampleToChunkEntry() { ... }
```

### ✅ Ваш use-case: додавання нового атому

```go
// Опис нового атому 'udta' (User Data):
func udta_UserData() {
    _unknowns()  // підтримка будь-яких дочірніх атомів
}

// Опис атому 'keys' (Metadata Keys) зі складною логікою:
func keys_MetadataKeys() {
    uint8(Version)
    uint24(Flags)
    uint32(_len_Entries)  // спеціальний префікс _len_ для довжини масиву
    array(Namespace, uint32, 4)  // fourcc простору імен
    bytesleft(Keys)  // список ключів у форматі null-terminated strings
}

// Після запуску генератора:
// 1. Створиться struct MetadataKeys з полями
// 2. Згенеруються методи Marshal/Unmarshal/Len/Children
// 3. Додасться константа KEYS = Tag(0x6b657973)
// 4. Додасться метод (MetadataKeys) Tag() Tag
```

---

## 🔑 2. Директиви опису полів

### 🔧 Примітивні типи:

```go
uint8(Version)      // 1 байт, беззнаковий
uint16(Flags)       // 2 байти, беззнаковий (Big-Endian)
uint24(Flags)       // 3 байти → мапиться у uint32 у Go
uint32(Duration)    // 4 байти, беззнаковий
uint64(BaseOffset)  // 8 байт, беззнаковий

int16(Layer)        // 2 байти, знаковый
int32(TrackId)      // 4 байти, знаковый
```

### 🔧 Спеціальні типи:

```go
// Час у форматі 1904-01-01 епохи (MP4 стандарт):
time32(CreateTime)  // 4 байти → time.Time у Go
time64(ModifyTime)  // 8 байт → time.Time у Go

// Фіксована крапка 16.16 (ціла.дробова):
fixed16(Volume)     // 2 байти → float64 у Go (значення 1.0 = 0x00010000)
fixed32(TrackWidth) // 4 байти → float64 у Go

// Байтові масиви:
bytes(Type, 4)              // [4]byte — фіксований розмір
bytes(CompressorName, 32)   // [32]byte
bytesleft(Data)             // []byte — всі байти до кінця атому

// Колекції:
slice(Entries, TimeToSampleEntry)  // []TimeToSampleEntry — динамічний масив
array(Matrix, int32, 9)            // [9]int32 — фіксований масив
```

### 🔧 Атоми та навігація:

```go
// Один дочірній атом (вказівник):
atom(Header, TrackHeader)    // *TrackHeader
atom(Media, Media)           // *Media

// Масив дочірніх атомів:
atoms(Tracks, Track)         // []*Track
atoms(Unknowns, Atom)        // []Atom для невідомих атомів

// Мета-директиви:
_skip(10)                    // пропустити 10 байт (зарезервовано/паддінг)
_unknowns()                  // додати поле Unknowns []Atom для сумісності
_childrenNR                  // спеціальне поле для лічильника дітей у marshal
_len_Entries                 // спеціальний префікс для довжини слайсу
```

---

## 🔑 3. Складні випадки: `_code` директива

### 🔧 Призначення `_code`:

```
Директива `_code` дозволяє вставляти кастомний код для обробки:
• Умовних полів (залежних від прапорців)
• Версійно-залежної логіки
• Оптимізованих циклів для масивів

Синтаксис:
  _code(marshal_func, len_func, unmarshal_func)

Де кожна функція — це функціональний літерал, що буде вставлений
у відповідний метод згенерованого коду.
```

### 🔧 Приклад 1: Умовні поля у `TrackFragRun`:

```go
func trun_TrackFragRun() {
    uint8(Version)
    uint24(Flags)
    uint32(_len_Entries)

    // DataOffset — тільки якщо встановлено прапорець
    uint32(DataOffset, _code(func() {
        if self.Flags&TRUN_DATA_OFFSET != 0 {
            doit()  // "doit" буде замінено на стандартний код запису
        }
    }))

    // FirstSampleFlags — тільки якщо встановлено прапорець
    uint32(FirstSampleFlags, _code(func() {
        if self.Flags&TRUN_FIRST_SAMPLE_FLAGS != 0 {
            doit()
        }
    }))

    // Entries — складна логіка з різними прапорцями для кожного елемента
    slice(Entries, TrackFragRunEntry, _code(
        // Marshal логіка
        func() {
            for i, entry := range self.Entries {
                var flags uint32
                if i > 0 {
                    flags = self.Flags
                } else {
                    flags = self.FirstSampleFlags
                }
                if flags&TRUN_SAMPLE_DURATION != 0 {
                    pio.PutU32BE(b[n:], entry.Duration)
                    n += 4
                }
                // ... аналогічно для Size, Flags, Cts ...
            }
        },
        // Len логіка
        func() {
            for i := range self.Entries {
                var flags uint32
                if i > 0 { flags = self.Flags } else { flags = self.FirstSampleFlags }
                if flags&TRUN_SAMPLE_DURATION != 0 { n += 4 }
                // ... аналогічно ...
            }
        },
        // Unmarshal логіка
        func() {
            for i := 0; i < int(_len_Entries); i++ {
                var flags uint32
                if i > 0 { flags = self.Flags } else { flags = self.FirstSampleFlags }
                entry := &self.Entries[i]
                if flags&TRUN_SAMPLE_DURATION != 0 {
                    entry.Duration = pio.U32BE(b[n:])
                    n += 4
                }
                // ... аналогічно ...
            }
        },
    ))
}
```

### 🔧 Приклад 2: Версійно-залежний час у `TrackFragDecodeTime`:

```go
func tfdt_TrackFragDecodeTime() {
    uint8(Version)
    uint24(Flags)
    
    // Час: 64 біти якщо Version != 0, інакше 32 біти
    time64(Time, _code(
        // Marshal
        func() {
            if self.Version != 0 {
                PutTime64(b[n:], self.Time)  // 8 байт
                n += 8
            } else {
                PutTime32(b[n:], self.Time)  // 4 байти
                n += 4
            }
        },
        // Len
        func() {
            if self.Version != 0 { n += 8 } else { n += 4 }
        },
        // Unmarshal
        func() {
            if self.Version != 0 {
                self.Time = GetTime64(b[n:])
                n += 8
            } else {
                self.Time = GetTime32(b[n:])
                n += 4
            }
        },
    ))
}
```

### 🔧 Приклад 3: Раннє завершення у `SampleSize`:

```go
func stsz_SampleSize() {
    uint8(Version)
    uint24(Flags)
    uint32(SampleSize)  // якщо != 0, всі семпли одного розміру
    
    // Якщо SampleSize != 0, не читати масив Entries
    _code(func() {
        if self.SampleSize != 0 {
            return  // раннє завершення у marshal/unmarshal
        }
    })
    
    uint32(_len_Entries)  // тільки якщо SampleSize == 0
    slice(Entries, uint32)
}
```

---

## 🔑 4. Ієрархія атомів у DSL

### 🔧 Кореневі атоми:

```go
func moov_Movie() {
    atom(Header, MovieHeader)      // mvhd
    atom(MovieExtend, MovieExtend) // mvex (для fMP4)
    atoms(Tracks, Track)           // trak × N
    _unknowns()                    // невідомі атоми
}
```

### 🔧 Трек → Медіа → Таблиці:

```go
func trak_Track() {
    atom(Header, TrackHeader)  // tkhd
    atom(Media, Media)         // mdia
    _unknowns()
}

func mdia_Media() {
    atom(Header, MediaHeader)  // mdhd
    atom(Handler, HandlerRefer) // hdlr
    atom(Info, MediaInfo)      // minf
    _unknowns()
}

func minf_MediaInfo() {
    atom(Sound, SoundMediaInfo)   // smhd (аудіо)
    atom(Video, VideoMediaInfo)   // vmhd (відео)
    atom(Data, DataInfo)          // dinf
    atom(Sample, SampleTable)     // stbl ← критично для демуксингу
    _unknowns()
}
```

### 🔧 Sample Table — серце демуксингу:

```go
func stbl_SampleTable() {
    atom(SampleDesc, SampleDesc)        // stsd: кодек-описи
    atom(TimeToSample, TimeToSample)    // stts: DTS розрахунок
    atom(CompositionOffset, CompositionOffset) // ctts: PTS = DTS + offset
    atom(SampleToChunk, SampleToChunk)  // stsc: мапінг семплів у чанки
    atom(SyncSample, SyncSample)        // stss: ключові кадри
    atom(ChunkOffset, ChunkOffset)      // stco: позиції чанків
    atom(SampleSize, SampleSize)        // stsz: розміри семплів
}
```

### 🔧 Фрагментований MP4 (fMP4):

```go
func moof_MovieFrag() {
    atom(Header, MovieFragHeader)  // mfhd
    atoms(Tracks, TrackFrag)       // traf × N
    _unknowns()
}

func traf_TrackFrag() {
    atom(Header, TrackFragHeader)  // tfhd
    atom(DecodeTime, TrackFragDecodeTime) // tfdt
    atom(Run, TrackFragRun)        // trun ← таблиця семплів у фрагменті
    _unknowns()
}
```

---

## 🔑 5. Генерація коду: приклад перетворення

### 🔧 Вхідний DSL:

```go
func stts_TimeToSample() {
    uint8(Version)
    uint24(Flags)
    uint32(_len_Entries)
    slice(Entries, TimeToSampleEntry)
}

func TimeToSampleEntry() {
    uint32(Count)
    uint32(Duration)
}
```

### 🔧 Згенерований код (скорочено):

```go
// 1. Константи:
const STTS = Tag(0x73747473)

// 2. Структури:
type TimeToSample struct {
    Version uint8
    Flags   uint32  // з uint24
    Entries []TimeToSampleEntry
    AtomPos
}

type TimeToSampleEntry struct {
    Count    uint32
    Duration uint32
}

// 3. Методи:
func (self TimeToSample) Tag() Tag { return STTS }

func (self TimeToSample) Marshal(b []byte) (n int) {
    pio.PutU32BE(b[4:], uint32(STTS))
    n += self.marshal(b[8:]) + 8
    pio.PutU32BE(b[0:], uint32(n))
    return
}

func (self TimeToSample) marshal(b []byte) (n int) {
    pio.PutU8(b[n:], self.Version); n += 1
    pio.PutU24BE(b[n:], self.Flags); n += 3
    pio.PutU32BE(b[n:], uint32(len(self.Entries))); n += 4
    for _, entry := range self.Entries {
        PutTimeToSampleEntry(b[n:], entry)
        n += LenTimeToSampleEntry
    }
    return
}

func (self TimeToSample) Len() (n int) {
    n += 8
    n += 1 + 3 + 4
    n += LenTimeToSampleEntry * len(self.Entries)
    return
}

func (self *TimeToSample) Unmarshal(b []byte, offset int) (n int, err error) {
    (&self.AtomPos).setPos(offset, len(b))
    n += 8
    self.Version = pio.U8(b[n:]); n += 1
    self.Flags = pio.U24BE(b[n:]); n += 3
    _len_Entries := pio.U32BE(b[n:]); n += 4
    self.Entries = make([]TimeToSampleEntry, _len_Entries)
    for i := range self.Entries {
        self.Entries[i] = GetTimeToSampleEntry(b[n:])
        n += LenTimeToSampleEntry
    }
    return
}

func (self TimeToSample) Children() (r []Atom) { return }

// 4. Утиліти для елементів:
func GetTimeToSampleEntry(b []byte) (self TimeToSampleEntry) {
    self.Count = pio.U32BE(b[0:])
    self.Duration = pio.U32BE(b[4:])
    return
}
func PutTimeToSampleEntry(b []byte, self TimeToSampleEntry) {
    pio.PutU32BE(b[0:], self.Count)
    pio.PutU32BE(b[4:], self.Duration)
}
const LenTimeToSampleEntry = 8
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Невірний мапінг uint24** | Значення > 0xFFFFFF обрізаються | Додайте валідацію діапазону у кастомному `_code` |
| **Переповнення буфера** | Index out of range у Marshal | Переконайтеся, що `Len()` обчислює розмір коректно |
| **Паніка при type assertion** | `atom.(*TrackHeader)` не співпадає | Завжди перевіряйте `ok` після type assertion у ручному коді |
| **Некоректний парсинг масивів** | `slice` читає неправильну кількість | Переконайтеся, що `_len_Entries` читається перед `make()` |
| **Втрата невідомих атомів** | `_unknowns` не зберігає атоми | Переконайтеся, що default case у switch додає атоми у `self.Unknowns` |

---

## ⚡ Оптимізації DSL

### 1. Додавання підтримки 64-бітних offset'ів:

```go
// DSL розширення для co64 атому:
func co64_ChunkOffset64() {
    uint8(Version)
    uint24(Flags)
    uint32(_len_Entries)
    slice(Entries, uint64)  // 64-бітні offset'и для файлів >4 ГБ
}
```

### 2. Підтримка великих розмірів атомів (size=1):

```go
// Додавання у генератор обробки 64-бітного розміру:
// У ReadFileAtoms():
if size == 1 {
    size64 := make([]byte, 8)
    io.ReadFull(r, size64)
    size = int64(pio.U64BE(size64))
}
```

### 3. Кешування результатів парсингу:

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
    
    atoms, err := parseAtoms(b, offset, tag)
    
    c.mu.Lock()
    if c.cache == nil { c.cache = make(map[Tag][]Atom) }
    c.cache[tag] = atoms
    c.mu.Unlock()
    
    return atoms, nil
}
```

---

## 📋 Чек-лист використання DSL

```go
// ✅ 1. Дотримуйтесь конвенції іменування: fourcc_StructName()
// ✅ 2. Використовуйте _len_ префікс для довжин масивів
// ✅ 3. Додавайте _unknowns() для сумісності з майбутніми розширеннями
// ✅ 4. Використовуйте _code для складної умовної логіки
// ✅ 5. Перевіряйте діапазони для 24-бітних полів
// ✅ 6. Тестуйте згенерований код: Marshal → Unmarshal → порівняння
// ✅ 7. Додавайте fuzz-тести для Unmarshal методів
// ✅ 8. Документуйте кастомний код у _code директивах
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12 (ISO BMFF)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [MP4 Box Structure](https://wiki.multimedia.cx/index.php/MP4) — візуальна схема атомів
- 📄 [Go Code Generation](https://go.dev/blog/generate) — офіційний гайд
- 🧪 [Go testing Package](https://pkg.go.dev/testing) — написання тестів для згенерованого коду
- 📦 [Fuzzing in Go](https://go.dev/doc/fuzz/) — fuzz-тестування для стійкості

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди тестуйте round-trip** — Marshal → Unmarshal → порівняння оригіналу з результатом виявить помилки у генерації.
> 2. **Додайте fuzzing для Unmarshal** — генератор випадкових бінарних даних допоможе знайти edge cases та вразливості.
> 3. **Документуйте `_code` блоки** — кастомний код важко підтримувати без коментарів.
> 4. **Версіонуйте DSL формат** — зміни у синтаксисі можуть зламати існуючі описи атомів.
> 5. **Моніторьте розмір згенерованого коду** — надмірна генерація може призвести до великих бінарників.

Потрібен приклад fuzz-тесту для згенерованого `Unmarshal` методу, або інтеграція цього генератора у ваш CI/CD pipeline для автоматичної перевірки змін у специфікації MP4? Готовий допомогти! 🚀