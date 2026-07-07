# 🔧 `field.go`: Рефлексія та метадані полів у `go-mp4`

Це **ядро бібліотеки `go-mp4`** — модуль, який використовує **рефлексію (reflection)** Go для аналізу структур боксів, парсингу тегів `mp4:"..."` та динамічного визначення правил серіалізації/десеріалізації.

---

## 🎯 Коротка відповідь

> **Це "двигун" бібліотеки**: він перетворює декларації типу `mp4:"0,size=32,opt=0x000001"` у виконувану логіку для читання/запису бітів, обробки опціональних полів, версій та динамічних розмірів — без написання парсерів вручну.

---

## 🧱 Основні типи даних

### 🔹 `fieldFlag` — бітові прапорці для полів

```go
type fieldFlag uint16

const (
    fieldString        fieldFlag = 1 << iota  // 0: рядок (C-string)
    fieldExtend                               // 1: розширення батьківської структури
    fieldDec                                  // 2: вивід у десятковому форматі
    fieldHex                                  // 3: вивід у шістнадцятковому форматі
    fieldISO639_2                             // 4: кодування мови (5 біт/літера)
    fieldUUID                                 // 5: UUID (16 байт)
    fieldHidden                               // 6: поле тільки для читання
    fieldOptDynamic                           // 7: опціональне поле (динамічна логіка)
    fieldVarint                               // 8: змінна довжина цілого (MPEG-4 varint)
    fieldSizeDynamic                          // 9: розмір поля визначається динамічно
    fieldLengthDynamic                        // 10: довжина масиву визначається динамічно
    fieldBoxString                            // 11: рядок до кінця боксу (WebVTT)
)
```

**🎯 Призначення**: Компактне зберігання **12+ атрибутів** поля у одному `uint16` через бітові операції.

**Приклад використання**:
```go
f.set(fieldOptDynamic)  // 🔹 Встановити прапорець
if f.is(fieldHex) { ... }  // 🔹 Перевірити прапорець
```

---

### 🔹 `field` — метадані одного поля структури

```go
type field struct {
    children   []*field      // 🔹 Вкладені поля (для вкладених структур)
    name       string        // 🔹 Ім'я поля у структурі
    cnst       string        // 🔹 Очікуване значення `const=...`
    order      int           // 🔹 Порядок серіалізації (mp4:"0,...")
    optFlag    uint32        // 🔹 Прапорець для увімкнення (opt=0x000001)
    nOptFlag   uint32        // 🔹 Прапорець для вимкнення (nopt=0x000001)
    size       uint          // 🔹 Розмір у бітах (size=32)
    length     uint          // 🔹 Довжина масиву (len=dynamic)
    flags      fieldFlag     // 🔹 Бітові прапорці (див. вище)
    strType    stringType    // 🔹 Тип рядка: C-string / Pascal / boxstring
    version    uint8         // 🔹 Версія, для якої поле присутнє (ver=0)
    nVersion   uint8         // 🔹 Версія, для якої поле відсутнє (nver=0)
}
```

**🎯 Призначення**: Зберігати **всю інформацію про поле**, отриману з тегів, для подальшого використання під час парсингу/запису.

---

### 🔹 `fieldInstance` — екземпляр поля для конкретного боксу

```go
type fieldInstance struct {
    field          // 🔹 Базові метадані (з кешу)
    cfo ICustomFieldObject  // 🔹 Інтерфейс для динамічних обчислень
}
```

**🎯 Призначення**: Дозволяє **обчислювати динамічні властивості** (`size`, `length`) в контексті конкретного екземпляра боксу через інтерфейси `GetFieldSize()` / `GetFieldLength()`.

---

## 🔍 Ключові функції

### 🔹 `buildFields(box IImmutableBox) []*field` — побудова дерева полів

```go
func buildFields(box IImmutableBox) []*field {
    t := reflect.TypeOf(box).Elem()  // 🔹 Отримуємо тип структури через рефлексію
    return buildFieldsStruct(t)       // 🔹 Рекурсивно будуємо дерево полів
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: структура типу `Trun { FullBox; SampleCount uint32; Entries []TrunEntry }`
│
▼
🔹 buildFieldsStruct(t):
   │
   ├── 🔹 Для кожного поля структури:
   │   ├── 🔹 Читання тегу: `mp4:"0,size=32"`
   │   ├── 🔹 Виклик buildField() для парсингу тегу
   │   ├── 🔹 Рекурсивний виклик buildFieldsAny() для вкладених типів
   │   └── 🔹 Додавання у список `fs`
   │
   ├── 🔹 Сортування за `order`: fs[0].order < fs[1].order
   │
   ▼
🔹 Вихід: []*field у порядку серіалізації
```

**🎯 Результат**: Кешоване дерево метаданих, яке використовується для швидкого доступу під час парсингу.

---

### 🔹 `buildField(fieldName, tag) *field` — парсинг одного тегу

```go
func buildField(fieldName string, tag string) *field {
    f := &field{name: fieldName}
    tagMap := parseFieldTag(tag)  // 🔹 "0,size=32,opt=0x000001" → map
    
    // 🔹 Визначення порядку серіалізації
    for key, val := range tagMap {
        if val == "" {  // Ключ без значення: "0", "extend", "hex"...
            if order, err := strconv.Atoi(key); err == nil {
                f.order = order  // 🔹 order = 0
                break
            }
        }
    }
    
    // 🔹 Обробка прапорців та атрибутів
    if _, contained := tagMap["hex"]; contained {
        f.set(fieldHex)  // 🔹 Виводити у шістнадцятковому форматі
    }
    
    if val, contained := tagMap["size"]; contained {
        if val == "dynamic" {
            f.set(fieldSizeDynamic)  // 🔹 Розмір обчислюється динамічно
        } else {
            f.size = uint(strconv.Atoi(val))  // 🔹 Фіксований розмір у бітах
        }
    }
    
    // 🔹 Обробка версій: ver=0, nver=0
    if val, contained := tagMap["ver"]; contained {
        f.version = uint8(strconv.Atoi(val))  // 🔹 Поле тільки для версії 0
    }
    
    return f
}
```

**🔢 Приклад парсингу тегу:**
```
🔤 Вхідний тег: `mp4:"2,size=32,opt=0x000001,hex"`

🔍 parseFieldTag() → map:
{
    "2": "",           // ← порядок
    "size": "32",      // ← розмір
    "opt": "0x000001", // ← прапорець увімкнення
    "hex": "",         // ← формат виводу
}

🔧 buildField() встановлює:
• f.order = 2
• f.size = 32
• f.optFlag = 0x000001
• f.set(fieldHex)
```

---

### 🔹 `parseFieldTag(str) map[string]string` — розбір рядка тегу

```go
func parseFieldTag(str string) map[string]string {
    tag := make(map[string]string, 8)
    list := strings.Split(str, ",")  // 🔹 "0,size=32,opt=0x01" → ["0", "size=32", "opt=0x01"]
    
    for _, e := range list {
        kv := strings.SplitN(e, "=", 2)  // 🔹 "size=32" → ["size", "32"]
        if len(kv) == 2 {
            tag[strings.Trim(kv[0], " ")] = strings.Trim(kv[1], " ")  // {"size": "32"}
        } else {
            tag[strings.Trim(kv[0], " ")] = ""  // {"0": ""} ← прапорець без значення
        }
    }
    return tag
}
```

**🎯 Призначення**: Перетворити рядок тегу у зручну для обробки `map[string]string`.

---

### 🔹 `resolveFieldInstance(f, box, parent, ctx) *fieldInstance` — динамічне розв'язання

```go
func resolveFieldInstance(f *field, box IImmutableBox, parent reflect.Value, ctx Context) *fieldInstance {
    fi := fieldInstance{field: *f}
    
    // 🔹 Визначення об'єкта для динамічних викликів
    cfo, ok := parent.Addr().Interface().(ICustomFieldObject)
    if ok {
        fi.cfo = cfo  // 🔹 Батьківський об'єкт реалізує інтерфейс
    } else {
        fi.cfo = box  // 🔹 fallback на сам бокс
    }
    
    // 🔹 Обчислення динамічного розміру
    if fi.is(fieldSizeDynamic) {
        fi.size = fi.cfo.GetFieldSize(f.name, ctx)  // 🔹 Виклик інтерфейсу!
    }
    
    // 🔹 Обчислення динамічної довжини
    if fi.is(fieldLengthDynamic) {
        fi.length = fi.cfo.GetFieldLength(f.name, ctx)  // 🔹 Виклик інтерфейсу!
    }
    
    return &fi
}
```

**🎯 Магія**: Ця функція **зв'язує статичні метадані з динамічною логікою** через інтерфейси `ICustomFieldObject`.

**Приклад:**
```
🔹 Поле: `Entries []TrunEntry \`mp4:"4,len=dynamic,size=dynamic"\``

🔹 Під час парсингу:
1. resolveFieldInstance() викликає: fi.cfo.GetFieldLength("Entries", ctx)
2. Бокс `Trun` реалізує: 
   func (t *Trun) GetFieldLength(name string, ctx Context) uint {
       if name == "Entries" { return uint(t.SampleCount) }
   }
3. Результат: fi.length = t.SampleCount (напр. 42)
4. Парсер читає рівно 42 елементи масиву ✅
```

---

### 🔹 `isTargetField(box, fi, ctx) bool` — чи читати/писати це поле?

```go
func isTargetField(box IImmutableBox, fi *fieldInstance, ctx Context) bool {
    // 🔹 Перевірка версії: ver=0 / nver=0
    if box.GetVersion() != anyVersion {
        if fi.version != anyVersion && box.GetVersion() != fi.version {
            return false  // ❌ Поле не для цієї версії
        }
        if fi.nVersion != anyVersion && box.GetVersion() == fi.nVersion {
            return false  // ❌ Поле виключено для цієї версії
        }
    }
    
    // 🔹 Перевірка прапорців: opt=0x000001
    if fi.optFlag != 0 && box.GetFlags()&fi.optFlag == 0 {
        return false  // ❌ Прапорець не встановлено → поле відсутнє
    }
    
    // 🔹 Перевірка nopt=0x000001
    if fi.nOptFlag != 0 && box.GetFlags()&fi.nOptFlag != 0 {
        return false  // ❌ Прапорець встановлено → поле відсутнє
    }
    
    // 🔹 Динамічна перевірка: opt=dynamic
    if fi.is(fieldOptDynamic) && !fi.cfo.IsOptFieldEnabled(fi.name, ctx) {
        return false  // ❌ Метод повернув false → поле відсутнє
    }
    
    return true  // ✅ Поле має бути прочитане/записане
}
```

**🎯 Призначення**: Визначити, чи **потрібно обробляти це поле** в поточному контексті (версія, прапорці, динамічна логіка).

**Приклад для `trun` боксу:**
```
🔹 Поле: `SampleDuration uint32 \`mp4:"0,size=32,opt=0x000100"\``

🔹 Сценарій 1: flags = 0x000100 (біт 8 встановлено)
• fi.optFlag = 0x000100
• box.GetFlags() & 0x000100 = 0x000100 ≠ 0 → ✅ поле присутнє

🔹 Сценарій 2: flags = 0x000001 (біт 8 не встановлено)
• box.GetFlags() & 0x000100 = 0 → ❌ поле відсутнє, пропускаємо
```

---

## 🔑 Розбір важливих тегів та їх обробка

### 🔹 `order` — порядок серіалізації

```go
mp4:"0,size=32"  // ← "0" = порядок #0 (перше поле)
```

**🎯 Призначення**: Забезпечити правильний порядок читання/запису полів, незалежно від порядку у структурі.

```go
sort.SliceStable(fs, func(i, j int) bool {
    return fs[i].order < fs[j].order  // 🔹 Сортування за order
})
```

---

### 🔹 `size=N` / `size=dynamic` — розмір поля у бітах

```go
// 🔹 Фіксований розмір:
SampleCount uint32 `mp4:"1,size=32"`  // ← 32 біти = uint32

// 🔹 Динамічний розмір:
Data []byte `mp4:"2,size=8,len=dynamic"`  // ← size=8 = кожен елемент 8 біт
```

**🎯 Обробка**:
```
🔹 size=32 → f.size = 32 → читаємо 4 байти
🔹 size=dynamic → f.set(fieldSizeDynamic) → викликаємо GetFieldSize() під час парсингу
```

---

### 🔹 `len=N` / `len=dynamic` — довжина масиву

```go
// 🔹 Фіксована довжина:
Reserved [3]uint32 `mp4:"3,size=32,len=3"`  // ← рівно 3 елементи

// 🔹 Динамічна довжина:
Entries []TrunEntry `mp4:"4,len=dynamic,size=96"`  // ← довжина з GetFieldLength()
```

**🎯 Обробка**:
```
🔹 len=dynamic → f.set(fieldLengthDynamic)
🔹 Під час парсингу: fi.length = fi.cfo.GetFieldLength("Entries", ctx)
🔹 Результат: читаємо рівно стільки елементів, скільки повернув метод
```

---

### 🔹 `opt=0xXXXX` / `nopt=0xXXXX` — умовні поля за прапорцями

```go
// 🔹 Увімкнути, якщо прапорець встановлено:
BaseDataOffset uint64 `mp4:"2,size=64,opt=0x000001"`  // ← flag 0x01

// 🔹 Вимкнути, якщо прапорець встановлено:
SomeField uint32 `mp4:"3,size=32,nopt=0x000001"`  // ← відсутній, якщо flag 0x01
```

**🎯 Логіка**:
```
🔹 opt=0x000001:
   if box.GetFlags() & 0x000001 == 0 → ❌ пропускаємо поле
   
🔹 nopt=0x000001:
   if box.GetFlags() & 0x000001 != 0 → ❌ пропускаємо поле
```

---

### 🔹 `opt=dynamic` — динамічні умовні поля

```go
// 🔹 Логіка визначається методом структури:
SampleInfoSize []uint8 `mp4:"5,size=8,opt=dynamic,len=dynamic"`
```

**🎯 Обробка**:
```
🔹 f.set(fieldOptDynamic)
🔹 Під час перевірки: fi.cfo.IsOptFieldEnabled("SampleInfoSize", ctx)
🔹 Приклад реалізації у Saiz:
   func (s *Saiz) IsOptFieldEnabled(name string, ctx Context) bool {
       if name == "SampleInfoSize" {
           return s.DefaultSampleInfoSize == 0  // 🔹 Увімкнути, тільки якщо default=0
       }
       return false
   }
```

---

### 🔹 `ver=N` / `nver=N` — версійні поля

```go
// 🔹 Тільки для версії 0:
SampleOffsetV0 uint32 `mp4:"1,size=32,ver=0"`

// 🔹 Для всіх версій, КРІМ 0:
SampleOffsetV1 int32 `mp4:"2,size=32,nver=0"`
```

**🎯 Логіка**:
```
🔹 ver=0:
   if box.GetVersion() != 0 → ❌ пропускаємо поле
   
🔹 nver=0:
   if box.GetVersion() == 0 → ❌ пропускаємо поле
```

---

### 🔹 Спеціальні типи рядків

| Тег | Тип | Опис |
|-----|-----|------|
| `string` | C-string | Нуль-термінований рядок: `"foo\x00"` |
| `string=c_p` | Pascal-string | Довжина+дані: `"\x03foo"` (застарілий) |
| `boxstring` | Box-string | Рядок до кінця боксу (WebVTT) |

**🎯 Особливість `boxstring`**:
```go
if f.is(fieldBoxString) && i != t.NumField()-1 {
    fmt.Fprint(os.Stderr, "go-mp4: boxstring must be the last field!!\n")
}
```
> ⚠️ **Важливо**: `boxstring` має бути **останнім полем** у структурі, бо він читає всі байти до кінця боксу.

---

## 🛠️ Практичне використання: Як це працює разом

### 🔹 Приклад: Парсинг `trun` боксу

```
📦 Вхідні байти: [00 00 01 01 00 00 00 03 00 00 00 32 00 00 00 64 00 00 00 65 00 00 00 66]

🔹 Крок 1: buildFields(&Trun{}) → кеш метаданих:
   [
     {name:"SampleCount", order:1, size:32},
     {name:"DataOffset", order:2, size:32, optFlag:0x000001},
     {name:"Entries", order:4, size:dynamic, len:dynamic, optFlag:0x000100},
     ...
   ]

🔹 Крок 2: Читання FullBox → version=0, flags=0x000101

🔹 Крок 3: Для кожного поля:
   • SampleCount: isTargetField()=true → читаємо 4 байти → 3
   • DataOffset: flags&0x000001=1 → true → читаємо 4 байти → 50
   • Entries: flags&0x000100=1 → true → 
     - resolveFieldInstance(): fi.length = GetFieldLength("Entries") = 3
     - Читаємо 3 елементи по 4 байти → [100, 101, 102]

🔹 Результат: Trun{SampleCount:3, DataOffset:50, Entries:[{100},{101},{102}]}
```

---

### 🔹 Приклад: Динамічні поля у `Saiz`

```go
type Saiz struct {
    DefaultSampleInfoSize uint8   `mp4:"3,size=8"`
    SampleCount           uint32  `mp4:"4,size=32"`
    SampleInfoSize        []uint8 `mp4:"5,size=8,opt=dynamic,len=dynamic"`
}

func (s *Saiz) IsOptFieldEnabled(name string, ctx Context) bool {
    if name == "SampleInfoSize" {
        return s.DefaultSampleInfoSize == 0  // 🔹 Увімкнути, тільки якщо default=0
    }
    return false
}

func (s *Saiz) GetFieldLength(name string, ctx Context) uint {
    if name == "SampleInfoSize" {
        return uint(s.SampleCount)  // 🔹 Довжина = кількість семплів
    }
    return 0
}
```

**🔄 Логіка парсингу:**
```
🔹 Сценарій 1: DefaultSampleInfoSize = 5
• IsOptFieldEnabled("SampleInfoSize") → false
• Поле пропускається → економимо місце

🔹 Сценарій 2: DefaultSampleInfoSize = 0
• IsOptFieldEnabled("SampleInfoSize") → true
• GetFieldLength("SampleInfoSize") → SampleCount (напр. 42)
• Читаємо 42 байти у масив ✅
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `order` | Поля читаються у неправильному порядку → зсув даних | Завжди вказуйте унікальні порядкові номери: `mp4:"0", mp4:"1", mp4:"2"` |
| `boxstring` не останнім полем | Помилка компіляції/парсингу: читає зайві байти | Завжди розміщуйте `boxstring` останнім у структурі |
| Забути `fieldSizeDynamic` | Динамічний розмір ігнорується → читаємо фіксовану кількість біт | Використовуйте `size=dynamic` + реалізуйте `GetFieldSize()` |
| Неправильна логіка `IsOptFieldEnabled` | Опціональні поля читаються завжди/ніколи → помилка парсингу | Перевіряйте логіку: повертати `true` тільки коли поле має бути присутнє |
| Конфлікт `ver`/`nver` | Поле читається для неправильної версії → помилка | Використовуйте `ver=0` для версії 0, `nver=0` для версій ≠0 |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні нових структур боксів:
    • Вказуйте `order` для кожного поля: `mp4:"0", mp4:"1", ...`
    • Використовуйте `size=N` для фіксованих розмірів, `size=dynamic` для змінних
    • Для масивів: `len=dynamic` + реалізуйте `GetFieldLength()`
    • Для опціональних полів: `opt=0xXXXX` або `opt=dynamic` + `IsOptFieldEnabled()`

[ ] Для динамічних полів:
    • Реалізуйте інтерфейс `ICustomFieldObject` (GetFieldSize, GetFieldLength, IsOptFieldEnabled)
    • Переконайтеся, що методи повертають коректні значення для поточного контексту
    • Тестуйте з різними версіями/прапорцями для перевірки логіки

[ ] Для рядкових полів:
    • Використовуйте `string` для C-string, `boxstring` для рядків до кінця боксу
    • Переконайтеся, що `boxstring` є останнім полем у структурі
    • Для мов: використовуйте `iso639-2` для автоматичного кодування/декодування

[ ] Для дебагу:
    • Логуйте метадані полів: log.Printf("🔧 Field %s: size=%d, flags=%b", f.name, f.size, f.flags)
    • Перевіряйте isTargetField() логіку: log.Printf("🎯 Field %s: target=%v", name, isTargetField(...))
    • Використовуйте `Stringify()` для перевірки виводу полів

[ ] Для тестування:
    • Створюйте тестові структури з різними комбінаціями тегів
    • Перевіряйте round-trip: структура → байти → структура
    • Тестуйте граничні випадки: порожні масиви, максимальні розміри, різні версії
```

---

## 🎯 Висновок

> **`field.go` — це "мізки" бібліотеки `go-mp4`**, які перетворюють декларації у код.  
> Він забезпечує:
> • ✅ Автоматичний парсинг тегів `mp4:"..."` через рефлексію
> • ✅ Гнучку підтримку динамічних полів через інтерфейси
> • ✅ Ефективну обробку версій, прапорців та умовних полів
> • ✅ Типобезпечний доступ до полів без ручного парсингу бітів
> • ✅ Розширюваність: додавайте нові типи боксів без зміни ядра бібліотеки

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Швидка розробка: описуйте структури боксів тегами, а не пишіть парсери
- 🔧 Гнучкість: легко додавати нові типи боксів або модифікувати існуючі
- 🛡️ Надійність: автоматична обробка краєвих випадків (версії, прапорці, динамічні розміри)
- 🧪 Тестованість: кожен бокс можна протестувати ізольовано через `Marshal`/`Unmarshal`

Потребуєте допомоги зі створенням нової структури боксу з динамічними полями або з дебагом тегів `mp4:"..."`? Напишіть — покажу готовий приклад! 🚀🔧