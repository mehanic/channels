# 📦 `box.go`: Система реєстрації та пошуку типів боксів у `go-mp4`

Це **центральний модуль бібліотеки `go-mp4`**, який відповідає за **реєстрацію, зберігання метаданих та пошук** типів боксів (box types) стандарту MP4/ISOBMFF.

---

## 🎯 Коротка відповідь

> **Це "реєстр" бібліотеки**: він дозволяє зареєструвати будь-який тип боксу з його структурою, підтримуваними версіями та логікою вибору, а потім швидко знайти потрібну конфігурацію за 4-байтовим ідентифікатором (напр. `"moov"`, `"trak"`, `"trun"`).

---

## 🧱 Основні типи даних

### 🔹 `BoxType` — 4-байтовий ідентифікатор боксу

```go
type BoxType [4]byte
```

**🎯 Призначення**: Представляє **стандартний 4-символьний код** боксу у форматі MP4 (напр. `"moov"`, `"trak"`, `"trun"`).

**Приклади:**
```go
BoxType{'m', 'o', 'o', 'v'}  // "moov" — кореневий контейнер метаданих
BoxType{'t', 'r', 'a', 'k'}  // "trak" — опис доріжки
BoxType{'t', 'r', 'u', 'n'}  // "trun" — таймстемпи кадрів у фрагменті
BoxType{0xA9, 'n', 'a', 'm'} // "©nam" — назва (QuickTime legacy)
```

---

### 🔹 `boxDef` — внутрішні метадані зареєстрованого боксу

```go
type boxDef struct {
    dataType reflect.Type      // 🔹 Тип структури (напр. *Trun)
    versions []uint8           // 🔹 Підтримувані версії: [0, 1]
    isTarget func(Context) bool // 🔹 Умовна логіка вибору (опціонально)
    fields   []*field          // 🔹 Кешовані метадані полів (з field.go)
}
```

**🎯 Призначення**: Зберігати **всю інформацію**, необхідну для серіалізації/десеріалізації боксу, у компактному форматі для швидкого доступу.

---

### 🔹 `boxMap` — глобальний реєстр типів боксів

```go
var boxMap = make(map[BoxType][]boxDef, 64)
```

**🎯 Призначення**: `map[BoxType][]boxDef` дозволяє:
- ✅ Швидкий пошук за 4-байтовим ключем: `O(1)`
- ✅ Підтримку **кількох визначень** для одного типу (напр. різні структури для різних контекстів)
- ✅ Пріоритет останнього зареєстрованого визначення (пошук з кінця масиву)

---

## 🔑 Ключові функції реєстрації

### 🔹 `AddBoxDef` — базова реєстрація

```go
func AddBoxDef(payload IBox, versions ...uint8) {
    boxMap[payload.GetType()] = append(boxMap[payload.GetType()], boxDef{
        dataType: reflect.TypeOf(payload).Elem(),  // 🔹 Тип структури через рефлексію
        versions: versions,                         // 🔹 Підтримувані версії
        fields:   buildFields(payload),             // 🔹 Кеш метаданих полів
    })
}
```

**🔢 Приклад використання:**
```go
// 🔹 У файлі box_definitions.go:
func init() {
    AddBoxDef(&Trun{}, 0, 1)  // 🔹 Trun підтримує версії 0 та 1
}
```

**🎯 Коли використовувати?** Для більшості стандартних боксів, де логіка однакова для всіх контекстів.

---

### 🔹 `AddBoxDefEx` — реєстрація з умовною логікою

```go
func AddBoxDefEx(payload IBox, isTarget func(Context) bool, versions ...uint8) {
    boxMap[payload.GetType()] = append(boxMap[payload.GetType()], boxDef{
        dataType: reflect.TypeOf(payload).Elem(),
        versions: versions,
        isTarget: isTarget,  // 🔹 Функція вибору за контекстом!
        fields:   buildFields(payload),
    })
}
```

**🎯 Призначення**: Дозволяє **вибирати структуру боксу залежно від контексту** (напр. різні поля для QuickTime vs ISO).

**Приклад:**
```go
func isUnderIlstMeta(ctx Context) bool {
    return ctx.UnderIlstMeta  // 🔹 Тільки всередині iTunes metadata
}

func init() {
    AddBoxDefEx(&Data{}, isUnderIlstMeta, 0)  // 🔹 Data бокс тільки у певному контексті
}
```

---

### 🔹 `AddAnyTypeBoxDef` / `AddAnyTypeBoxDefEx` — реєстрація для довільних типів

```go
func AddAnyTypeBoxDef(payload IAnyType, boxType BoxType, versions ...uint8) {
    boxMap[boxType] = append(boxMap[boxType], boxDef{...})
}
```

**🎯 Призначення**: Реєструвати **одну структуру для кількох типів боксів**.

**Приклад:**
```go
// 🔹 AudioSampleEntry використовується для багатьох аудіо-кодеків:
func init() {
    AddAnyTypeBoxDef(&AudioSampleEntry{}, BoxTypeOpus())   // "Opus"
    AddAnyTypeBoxDef(&AudioSampleEntry{}, BoxTypeMp4a())   // "mp4a"
    AddAnyTypeBoxDef(&AudioSampleEntry{}, BoxTypeAlac())   // "alac"
}
```

**🎯 Перевага**: Не потрібно створювати окремі структури для `Opus`, `mp4a`, `alac` — вони мають однакову базову структуру.

---

## 🔍 Функції пошуку та створення

### 🔹 `getBoxDef` — пошук визначення боксу за типом та контекстом

```go
func (boxType BoxType) getBoxDef(ctx Context) *boxDef {
    boxDefs := boxMap[boxType]  // 🔹 Отримуємо масив визначень для цього типу
    
    // 🔹 Пошук з кінця: останнє зареєстроване має пріоритет
    for i := len(boxDefs) - 1; i >= 0; i-- {
        boxDef := &boxDefs[i]
        if boxDef.isTarget == nil || boxDef.isTarget(ctx) {
            return boxDef  // ✅ Знайдено!
        }
    }
    
    // 🔹 Спеціальна логіка для iTunes metadata (Item)
    if ctx.UnderIlst {
        typeID := int(binary.BigEndian.Uint32(boxType[:]))
        if typeID >= 1 && typeID <= ctx.QuickTimeKeysMetaEntryCount {
            return &boxDef{
                dataType: reflect.TypeOf(Item{}),
                isTarget: isIlstMetaContainer,
                fields:   itemBoxFields,  // 🔹 Кешовані поля для Item
            }
        }
    }
    
    return nil  // ❌ Не знайдено
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: boxType="trun", ctx={Version:1, Flags:0x000101, ...}
│
▼
🔹 boxMap["trun"] → []boxDef{def0, def1}
│
▼
🔹 Ітерація з кінця:
   • i=1: boxDef.versions=[0,1], isTarget=nil → ✅ повертаємо def1
   • (якщо isTarget != nil → викликаємо isTarget(ctx))
│
▼
🔹 Якщо не знайдено у boxMap:
   • Спеціальна логіка для iTunes metadata (Item)
   • Повертаємо nil, якщо не підходить
│
▼
🔹 Вихід: *boxDef або nil
```

**🎯 Ключова особливість**: **Пріоритет останнього зареєстрованого** визначення дозволяє перевизначати стандартні бокси для специфічних контекстів.

---

### 🔹 `New` — створення екземпляра боксу

```go
func (boxType BoxType) New(ctx Context) (IBox, error) {
    boxDef := boxType.getBoxDef(ctx)  // 🔹 Знаходимо визначення
    if boxDef == nil {
        return nil, ErrBoxInfoNotFound
    }
    
    // 🔹 Створюємо екземпляр через рефлексію
    box, ok := reflect.New(boxDef.dataType).Interface().(IBox)
    if !ok {
        return nil, fmt.Errorf("box type not implements IBox interface: %s", boxType.String())
    }
    
    // 🔹 Для IAnyType: встановлюємо тип боксу
    anyTypeBox, ok := box.(IAnyType)
    if ok {
        anyTypeBox.SetType(boxType)
    }
    
    return box, nil
}
```

**🎯 Призначення**: Створити **порожній екземпляр** структури боксу для подальшого парсингу.

**Приклад використання:**
```go
// 🔹 У UnmarshalAny:
box, err := boxType.New(ctx)  // ✅ Створюємо *Trun{}
if err != nil { return nil, err }

// 🔹 Далі парсимо дані у цей екземпляр:
Unmarshal(r, size, box, ctx)  // ✅ box тепер містить розпаршені дані
```

---

### 🔹 `IsSupported` / `IsSupportedVersion` — перевірка підтримки

```go
func (boxType BoxType) IsSupported(ctx Context) bool {
    return boxType.getBoxDef(ctx) != nil  // ✅ true, якщо знайдено визначення
}

func (boxType BoxType) IsSupportedVersion(ver uint8, ctx Context) bool {
    boxDef := boxType.getBoxDef(ctx)
    if boxDef == nil { return false }
    
    // 🔹 Якщо versions порожній → підтримуються всі версії
    if len(boxDef.versions) == 0 { return true }
    
    // 🔹 Перевіряємо, чи є версія у списку підтримуваних
    for _, sver := range boxDef.versions {
        if ver == sver { return true }
    }
    return false
}
```

**🎯 Призначення**: Перевірити, чи може бібліотека обробити даний тип/версію боксу **перед спробою парсингу**.

**Приклад:**
```go
if !boxType.IsSupported(ctx) {
    log.Printf("⚠️  Unknown box type: %s — пропускаємо", boxType)
    return nil
}

if !boxType.IsSupportedVersion(version, ctx) {
    return fmt.Errorf("unsupported version %d for box %s", version, boxType)
}
```

---

## 🔤 Робота з рядковими представленнями

### 🔹 `StrToBoxType` — створення з рядка

```go
func StrToBoxType(code string) BoxType {
    if len(code) != 4 {
        panic(fmt.Errorf("invalid box type id length: [%s]", code))
    }
    return BoxType{code[0], code[1], code[2], code[3]}
}
```

**🔢 Приклад:**
```go
bt := StrToBoxType("moov")  // ✅ BoxType{'m','o','o','v'}
bt := StrToBoxType("trak")  // ✅ BoxType{'t','r','a','k'}
bt := StrToBoxType("©nam")  // ✅ BoxType{0xA9,'n','a','m'}
```

> ⚠️ **Важливо**: Функція **panic** при неправильній довжині — це свідомий вибір для виявлення помилок на етапі розробки.

---

### 🔹 `Uint32ToBoxType` — створення з uint32

```go
func Uint32ToBoxType(i uint32) BoxType {
    b := make([]byte, 4)
    binary.BigEndian.PutUint32(b, i)  // 🔹 Big-endian порядок байт!
    return BoxType{b[0], b[1], b[2], b[3]}
}
```

**🎯 Призначення**: Конвертувати числове представлення типу боксу (як у деяких заголовках файлів) у `BoxType`.

**Приклад:**
```go
// 🔹 0x6D6F6F76 = "moov" у big-endian
bt := Uint32ToBoxType(0x6D6F6F76)  // ✅ BoxType{'m','o','o','v'}
```

---

### 🔹 `String()` — людино-читабельне представлення

```go
func (boxType BoxType) String() string {
    // 🔹 Перевірка: чи всі байти друковані (ASCII або ©)
    if isPrintable(boxType[0]) && ... && isPrintable(boxType[3]) {
        s := string([]byte{boxType[0], boxType[1], boxType[2], boxType[3]})
        // 🔹 Спеціальна заміна © символу
        s = strings.ReplaceAll(s, string([]byte{0xa9}), "(c)")
        return s
    }
    // 🔹 Непридатні для друку → шістнадцяткове представлення
    return fmt.Sprintf("0x%02x%02x%02x%02x", boxType[0], boxType[1], boxType[2], boxType[3])
}
```

**🔢 Приклади виводу:**
```
BoxType{'m','o','o','v'} → "moov"
BoxType{0xA9,'n','a','m'} → "(c)nam"  // ← © замінено на (c)
BoxType{0x00,0x00,0x00,0x01} → "0x00000001"  // ← непридатні для друку
```

**🎯 Призначення**: Зручне логування та дебаг без необхідності ручного форматування.

---

### 🔹 `MatchWith` — порівняння з підтримкою wildcard

```go
func (lhs BoxType) MatchWith(rhs BoxType) bool {
    if lhs == boxTypeAny || rhs == boxTypeAny {
        return true  // ✅ Wildcard: будь-який тип співпадає
    }
    return lhs == rhs  // ✅ Точне співпадіння
}
```

**🎯 Призначення**: Дозволити пошук боксів за типом з підтримкою **`BoxTypeAny()`** як універсального шаблону.

**Приклад:**
```go
// 🔹 Пошук всіх нащадків "trak":
path := BoxPath{BoxTypeMoov(), BoxTypeTrak(), BoxTypeAny()}
// ✅ Співпаде з: moov→trak→tkhd, moov→trak→edts, moov→trak→mdia...
```

---

## 🎯 Спеціальна логіка для iTunes metadata (Item)

```go
var itemBoxFields = buildFields(&Item{})  // 🔹 Кешовані поля для Item

// У getBoxDef():
if ctx.UnderIlst {
    typeID := int(binary.BigEndian.Uint32(boxType[:]))
    if typeID >= 1 && typeID <= ctx.QuickTimeKeysMetaEntryCount {
        return &boxDef{
            dataType: reflect.TypeOf(Item{}),
            isTarget: isIlstMetaContainer,
            fields:   itemBoxFields,
        }
    }
}
```

**🎯 Призначення**: Підтримка **нумерованих метаданих** у стилі QuickTime, де тип боксу — це числовий ID (1, 2, 3...), а не 4-символьний код.

**Контекст:**
```
📦 moov → udta → ilst
├── 📦 0x00000001 (ID=1) → Item{ItemName="data", Data=...}
├── 📦 0x00000002 (ID=2) → Item{ItemName="desc", Data=...}
└── ...
```

**🔑 Ключова умова**: `ctx.QuickTimeKeysMetaEntryCount` має бути встановлено з боксу `keys`, щоб знати діапазон валідних ID.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Реєстрація кастомного боксу

```go
// 🔹 Оголошення структури
type CustomCCTVMeta struct {
    Box
    CameraID  uint32 `mp4:"0,size=32"`
    Timestamp uint64 `mp4:"1,size=64"`
    Flags     uint8  `mp4:"2,size=8"`
}

func (CustomCCTVMeta) GetType() BoxType {
    return StrToBoxType("cctv")  // 🔹 Наш 4-символьний код
}

// 🔹 Реєстрація у init()
func init() {
    AddBoxDef(&CustomCCTVMeta{}, 0)  // ✅ Версія 0
}

// 🔹 Використання:
box := &CustomCCTVMeta{CameraID: 12345, Timestamp: 1678901234567, Flags: 0x01}
buf := &bytes.Buffer{}
Marshal(buf, box, Context{})  // ✅ Автоматична серіалізація!
```

---

### 🔹 Приклад 2: Динамічний вибір структури за контекстом

```go
// 🔹 Різні структури для QuickTime vs ISO
type QuickTimeData struct {
    Box
    PascalString []byte `mp4:"0,string=c_p"`  // ← Pascal-style
}

type ISOData struct {
    Box
    CString []byte `mp4:"0,string"`  // ← C-style
}

func isQuickTime(ctx Context) bool {
    return ctx.IsQuickTimeCompatible
}

func init() {
    // 🔹 Реєструємо обидва варіанти для одного типу "data"
    AddAnyTypeBoxDefEx(&QuickTimeData{}, BoxTypeData(), isQuickTime, 0)
    AddAnyTypeBoxDef(&ISOData{}, BoxTypeData(), 0)  // ← default, реєструємо останнім
}

// 🔹 При парсингу:
// • Якщо ctx.IsQuickTimeCompatible=true → використається QuickTimeData
// • Інакше → ISOData (останнє зареєстроване має пріоритет, але isTarget=false для QuickTime)
```

---

### 🔹 Приклад 3: Перевірка підтримки перед парсингом

```go
func safeUnmarshal(r io.ReadSeeker, boxType BoxType, size uint64, ctx Context) (IBox, error) {
    // 🔹 Крок 1: Чи підтримується тип?
    if !boxType.IsSupported(ctx) {
        log.Printf("⚠️  Unknown box type: %s — пропускаємо", boxType)
        // 🔹 Пропускаємо байти боксу
        _, err := r.Seek(int64(size), io.SeekCurrent)
        return nil, err
    }
    
    // 🔹 Крок 2: Чи підтримується версія?
    version, err := readVersion(r)  // 🔹 Читаємо перший байт
    if err != nil { return nil, err }
    
    if !boxType.IsSupportedVersion(version, ctx) {
        return nil, fmt.Errorf("unsupported version %d for box %s", version, boxType)
    }
    
    // 🔹 Крок 3: Створюємо та парсимо
    box, err := boxType.New(ctx)
    if err != nil { return nil, err }
    
    _, err = Unmarshal(r, size, box, ctx)
    return box, err
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Реєстрація без `init()` | Бокс не знайдено → `ErrBoxInfoNotFound` | Завжди реєструйте бокси у `init()` або перед використанням |
| Неправильна довжина коду у `StrToBoxType` | Panic: "invalid box type id length" | Завжди передавайте рівно 4 символи: `"moov"`, не `"mo"` |
| Конфлікт `isTarget` функцій | Неправильна структура обрана для контексту | Перевіряйте логіку `isTarget`: вона має повертати `true` тільки для відповідного контексту |
| Забути `SetType` для `IAnyType` | Бокс має неправильний тип у виводі | Завжди викликайте `anyTypeBox.SetType(boxType)` у `New()` |
| Неправильний порядок реєстрації | Перевизначення не працює | Пам'ятайте: **останнє зареєстроване має пріоритет** (пошук з кінця масиву) |

---

## 📋 Чекліст для вашого проекту

```
[ ] При реєстрації нових типів боксів:
    • Використовуйте AddBoxDef для стандартних випадків
    • Використовуйте AddBoxDefEx, якщо потрібна логіка за контекстом
    • Використовуйте AddAnyTypeBoxDef, якщо одна структура для кількох типів
    • Завжди реєструйте у init() або перед першим використанням

[ ] Для кастомних боксів:
    • Реалізуйте GetType() BoxType, що повертає 4-байтовий код
    • Дотримуйтесь стандарту: малі літери для стандартних боксів, великі для кастомних
    • Уникайте конфліктів з існуючими типами: перевірте boxMap перед реєстрацією

[ ] Для контекстної логіки:
    • Реалізуйте isTarget(ctx) чітко: повертати true тільки для відповідного контексту
    • Тестуйте з різними Context: UnderIlst, IsQuickTimeCompatible, тощо
    • Пам'ятайте про пріоритет: останнє зареєстроване з isTarget=true перемагає

[ ] Для дебагу:
    • Використовуйте boxType.String() для логування: log.Printf("📦 %s", boxType)
    • Перевіряйте підтримку перед парсингом: if !boxType.IsSupported(ctx) { ... }
    • Логувайте версії: log.Printf("🔢 Version %d supported: %v", ver, boxType.IsSupportedVersion(ver, ctx))

[ ] Для тестування:
    • Створюйте тестові бокси з різними тегами та перевіряйте round-trip
    • Тестуйте контекстну логіку: різні Context → різні структури
    • Перевіряйте граничні випадки: невідомі типи, непідтримувані версії
```

---

## 🎯 Висновок

> **`box.go` — це "мозок" системи типів `go-mp4`**, який забезпечує:
> • ✅ Гнучку реєстрацію боксів з підтримкою версій та контекстної логіки
> • ✅ Швидкий пошук за 4-байтовим ключем через `map[BoxType][]boxDef`
> • ✅ Пріоритет останнього зареєстрованого визначення для перевизначення
> • ✅ Спеціальну підтримку iTunes metadata через нумеровані типи (Item)
> • ✅ Безпечне створення екземплярів через рефлексію з перевіркою інтерфейсів

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Легке додавання кастомних боксів для метаданих камери, подій, аналітики
- 🔧 Гнучкість: різні структури для різних стандартів (ISO vs QuickTime) через `isTarget`
- 🛡️ Надійність: перевірка підтримки типу/версії перед парсингом запобігає крашам
- 🧪 Тестованість: кожен зареєстрований бокс можна протестувати ізольовано

Потребуєте допомоги з реєстрацією кастомного боксу для вашого процесора або з налаштуванням контекстної логіки? Напишіть — покажу готовий приклад! 🚀📦