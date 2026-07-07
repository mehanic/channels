# 🧪 Тести `box.go`: Перевірка реєстрації та пошуку типів боксів

Це **тестовий файл** для модуля `box.go` бібліотеки `go-mp4`, який перевіряє коректність роботи **системи реєстрації, пошуку та валідації** типів боксів.

---

## 🎯 Коротка відповідь

> **Ці тести гарантують, що бібліотека коректно розпізнає, реєструє та обробляє типи боксів** — від простих рядкових представлень до складної контекстної логіки вибору структур.

---

## 📋 Огляд тестових функцій

### 🔹 `TestBoxTypeString` — тест рядкового представлення `BoxType`

```go
func TestBoxTypeString(t *testing.T) {
    // 🔹 Друковані символи (ASCII)
    assert.Equal(t, "1234", BoxType{'1', '2', '3', '4'}.String())  // ✅ "1234"
    assert.Equal(t, "abcd", BoxType{'a', 'b', 'c', 'd'}.String())  // ✅ "abcd"
    
    // 🔹 Спеціальні друковані символи: пробіл, тильда
    assert.Equal(t, "xx x", BoxType{'x', 'x', ' ', 'x'}.String())  // ✅ "xx x" (пробіл дозволений)
    assert.Equal(t, "xx~x", BoxType{'x', 'x', '~', 'x'}.String())  // ✅ "xx~x" (~ дозволений)
    
    // 🔹 Спеціальний символ © (0xA9) → замінюється на "(c)"
    assert.Equal(t, "xx(c)x", BoxType{'x', 'x', 0xa9, 'x'}.String())  // ✅ "xx(c)x"
    
    // 🔹 Непридатні для друку байти → шістнадцяткове представлення
    assert.Equal(t, "0x7878ab78", BoxType{'x', 'x', 0xab, 'x'}.String())  // ✅ 0xab не друкований
}
```

**🎯 Що тестується:**

| Вхідний `BoxType` | Очікуваний `String()` | Чому? |
|------------------|----------------------|-------|
| `{'1','2','3','4'}` | `"1234"` | ✅ Всі байти друковані (ASCII) |
| `{'x','x',' ','x'}` | `"xx x"` | ✅ Пробіл (0x20) вважається друкованим |
| `{'x','x','~','x'}` | `"xx~x"` | ✅ Тильда (0x7E) — останній друкований ASCII |
| `{'x','x',0xA9,'x'}` | `"xx(c)x"` | ✅ 0xA9 = © → спеціальна заміна на "(c)" |
| `{'x','x',0xAB,'x'}` | `"0x7878ab78"` | ❌ 0xAB не друкований → hex-представлення |

**🔑 Ключова логіка `isPrintable()`**:
```go
func isPrintable(c byte) bool {
    return isASCII(c) || c == 0xa9  // ✅ ASCII 0x20-0x7E або ©
}
func isASCII(c byte) bool {
    return c >= 0x20 && c <= 0x7e  // ✅ Друковані ASCII символи
}
```

> 🎯 **Навіщо це?** Зручне логування: замість `0x6d6f6f76` бачимо `"moov"` у логах.

---

### 🔹 `TestIsSupported` — тест перевірки підтримки типу боксу

```go
func TestIsSupported(t *testing.T) {
    // 🔹 "pssh" зареєстровано у бібліотеці → ✅ підтримується
    assert.True(t, StrToBoxType("pssh").IsSupported(Context{}))
    
    // 🔹 "1234" не зареєстровано → ❌ не підтримується
    assert.False(t, StrToBoxType("1234").IsSupported(Context{}))
}
```

**🎯 Що тестується:**

| Вхід | Очікуваний результат | Чому? |
|------|---------------------|-------|
| `BoxType("pssh")` | `true` | ✅ Зареєстровано через `AddBoxDef(&Pssh{}, 0, 1)` |
| `BoxType("1234")` | `false` | ❌ Не зареєстровано у `boxMap` |

**🔑 Ключова логіка `IsSupported()`**:
```go
func (boxType BoxType) IsSupported(ctx Context) bool {
    return boxType.getBoxDef(ctx) != nil  // ✅ true, якщо знайдено визначення
}
```

> 🎯 **Навіщо це?** Безпечний парсинг: перевірити підтримку типу **перед** спробою читання, щоб уникнути помилок.

---

### 🔹 `TestGetSupportedVersions` — тест отримання списку підтримуваних версій

```go
func TestGetSupportedVersions(t *testing.T) {
    vers, err := BoxTypePssh().GetSupportedVersions(Context{})
    require.NoError(t, err)  // ✅ Не повинно бути помилки
    assert.Equal(t, []uint8{0, 1}, vers)  // ✅ Pssh підтримує версії 0 та 1
}
```

**🎯 Що тестується:**

| Бокс | Очікувані версії | Чому? |
|------|-----------------|-------|
| `pssh` | `[0, 1]` | ✅ Зареєстровано як `AddBoxDef(&Pssh{}, 0, 1)` |

**🔑 Ключова логіка `GetSupportedVersions()`**:
```go
func (boxType BoxType) GetSupportedVersions(ctx Context) ([]uint8, error) {
    boxDef := boxType.getBoxDef(ctx)
    if boxDef == nil {
        return nil, ErrBoxInfoNotFound  // ❌ Помилка, якщо не знайдено
    }
    return boxDef.versions, nil  // ✅ Повертаємо масив версій
}
```

> 🎯 **Навіщо це?** Дізнатися, які версії боксу підтримуються, для валідації вхідних даних або генерації сумісного виводу.

---

### 🔹 `TestIsSupportedVersion` — тест перевірки підтримки конкретної версії

```go
func TestIsSupportedVersion(t *testing.T) {
    // 🔹 Версії 0 та 1 підтримуються для pssh
    assert.True(t, BoxTypePssh().IsSupportedVersion(0, Context{}))  // ✅
    assert.True(t, BoxTypePssh().IsSupportedVersion(1, Context{}))  // ✅
    
    // 🔹 Версія 2 не підтримується
    assert.False(t, BoxTypePssh().IsSupportedVersion(2, Context{}))  // ❌
}
```

**🎯 Що тестується:**

| Версія | Очікуваний результат | Чому? |
|--------|---------------------|-------|
| `0` | `true` | ✅ У списку `[0, 1]` |
| `1` | `true` | ✅ У списку `[0, 1]` |
| `2` | `false` | ❌ Не у списку |

**🔑 Ключова логіка `IsSupportedVersion()`**:
```go
func (boxType BoxType) IsSupportedVersion(ver uint8, ctx Context) bool {
    boxDef := boxType.getBoxDef(ctx)
    if boxDef == nil { return false }
    
    // 🔹 Порожній список версій = підтримуються всі
    if len(boxDef.versions) == 0 { return true }
    
    // 🔹 Пошук версії у списку
    for _, sver := range boxDef.versions {
        if ver == sver { return true }
    }
    return false
}
```

> 🎯 **Навіщо це?** Перевірити конкретну версію боксу перед парсингом, щоб відхилити непідтримувані формати.

---

## 🔍 Як це працює разом: Повний потік

```
🔹 Крок 1: Реєстрація боксу (у init())
   │
   ├── 🔹 AddBoxDef(&Pssh{}, 0, 1)
   │   ├── dataType = reflect.TypeOf(&Pssh{}).Elem()
   │   ├── versions = [0, 1]
   │   ├── fields = buildFields(&Pssh{})  // кеш метаданих полів
   │   └── boxMap["pssh"] = append(boxMap["pssh"], boxDef)
   │
   ▼
🔹 Крок 2: Перевірка підтримки (у коді користувача)
   │
   ├── 🔹 boxType := StrToBoxType("pssh")
   ├── 🔹 if !boxType.IsSupported(ctx) { ... }  // ✅ true
   ├── 🔹 if !boxType.IsSupportedVersion(version, ctx) { ... }  // перевірка версії
   │
   ▼
🔹 Крок 3: Створення екземпляра для парсингу
   │
   ├── 🔹 box, err := boxType.New(ctx)  // ✅ *Pssh{}
   ├── 🔹 Unmarshal(r, size, box, ctx)  // парсинг даних
   │
   ▼
🔹 Крок 4: Використання розпаршених даних
   │
   ├── 🔹 pssh := box.(*Pssh)
   ├── 🔹 log.Printf("🔐 SystemID: %s", uuid.UUID(pssh.SystemID[:]).String())
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Безпечний парсинг невідомих боксів

```go
func parseBoxSafely(r io.ReadSeeker, boxType BoxType, size uint64, ctx Context) (IBox, error) {
    // 🔹 Крок 1: Чи підтримується тип?
    if !boxType.IsSupported(ctx) {
        log.Printf("⚠️  Unknown box type: %s (0x%08x) — пропускаємо %d байт", 
            boxType.String(), binary.BigEndian.Uint32(boxType[:]), size)
        // 🔹 Пропускаємо дані боксу
        _, err := r.Seek(int64(size), io.SeekCurrent)
        return nil, err
    }
    
    // 🔹 Крок 2: Читаємо заголовок FullBox (якщо є)
    var version uint8
    var flags [3]byte
    if boxType.IsFullBox(ctx) {  // 🔹 Перевірка: чи має бокс FullBox заголовок?
        if err := binary.Read(r, binary.BigEndian, &version); err != nil {
            return nil, err
        }
        if err := binary.Read(r, binary.BigEndian, &flags); err != nil {
            return nil, err
        }
        
        // 🔹 Крок 3: Чи підтримується версія?
        if !boxType.IsSupportedVersion(version, ctx) {
            return nil, fmt.Errorf("unsupported version %d for box %s", version, boxType)
        }
    }
    
    // 🔹 Крок 4: Створюємо та парсимо
    box, err := boxType.New(ctx)
    if err != nil { return nil, err }
    
    // 🔹 Встановлюємо версію/прапорці для FullBox
    if fb, ok := box.(interface{ SetVersion(uint8); SetFlags([3]byte) }); ok {
        fb.SetVersion(version)
        fb.SetFlags(flags)
    }
    
    _, err = Unmarshal(r, size, box, ctx)
    return box, err
}
```

---

### 🔹 Приклад 2: Динамічна реєстрація кастомних боксів

```go
// 🔹 Реєстрація боксів для різних типів камер
func RegisterCCTVBoxes() {
    // 🔹 Бокс для метаданих камери
    type CameraMeta struct {
        Box
        CameraID  uint32 `mp4:"0,size=32"`
        Location  string `mp4:"1,string"`
        Timestamp uint64 `mp4:"2,size=64"`
    }
    func (CameraMeta) GetType() BoxType { return StrToBoxType("cmet") }
    AddBoxDef(&CameraMeta{}, 0)
    
    // 🔹 Бокс для подій (рух, звук)
    type Event struct {
        Box
        EventType uint8  `mp4:"0,size=8"`  // 1=рух, 2=звук, 3=обидва
        Confidence uint8 `mp4:"1,size=8"`  // 0-100%
        Region    []uint8 `mp4:"2,size=8,len=dynamic"`  // координати ROI
    }
    func (Event) GetType() BoxType { return StrToBoxType("cevt") }
    func (e *Event) GetFieldLength(name string, ctx Context) uint {
        if name == "Region" { return uint(len(e.Region)) }
        return 0
    }
    AddBoxDef(&Event{}, 0)
    
    log.Printf("✅ Зареєстровано кастомні CCTV бокси: cmet, cevt")
}
```

---

### 🔹 Приклад 3: Контекстна логіка для QuickTime vs ISO

```go
// 🔹 Різні структури для iTunes metadata залежно від контексту
type QuickTimeMeta struct {
    Box
    PascalName []byte `mp4:"0,string=c_p"`  // ← Pascal-style
}

type ISOMeta struct {
    Box
    CString []byte `mp4:"0,string"`  // ← C-style
}

func isQuickTimeContext(ctx Context) bool {
    return ctx.IsQuickTimeCompatible
}

func init() {
    // 🔹 Реєструємо обидва варіанти для типу "©nam"
    AddAnyTypeBoxDefEx(&QuickTimeMeta{}, StrToBoxType("©nam"), isQuickTimeContext, 0)
    AddAnyTypeBoxDef(&ISOMeta{}, StrToBoxType("©nam"), 0)  // ← default (останній)
}

// 🔹 При парсингу:
// • Якщо ctx.IsQuickTimeCompatible=true → використається QuickTimeMeta
// • Інакше → ISOMeta (останнє зареєстроване, але isTarget=false для QuickTime)
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Реєстрація після першого використання | `ErrBoxInfoNotFound` при парсингу | Завжди реєструйте бокси у `init()` або перед будь-яким парсингом |
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

> **Ці тести — ваш "страховий поліс" для надійної роботи з типами боксів**.  
> Вони гарантують:
> • ✅ Коректне рядкове представлення для дебагу (друковані символи, ©, hex)
> • ✅ Надійну перевірку підтримки типу перед парсингом
> • ✅ Точне визначення підтримуваних версій для валідації
> • ✅ Гнучку систему реєстрації з пріоритетом останнього визначення
> • ✅ Спеціальну підтримку iTunes metadata через нумеровані типи

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Впевненість у коректній обробці як стандартних, так і кастомних боксів
- 🔧 Легке додавання нових типів метаданих для камер, подій, аналітики
- 🛡️ Безпека: перевірка підтримки типу/версії запобігає крашам на невідомих даних
- 🧪 Тестованість: кожен новий бокс можна протестувати за цим шаблоном

Потребуєте допомоги з реєстрацією кастомного боксу для вашого процесора або з налаштуванням контекстної логіки? Напишіть — покажу готовий приклад! 🚀📦