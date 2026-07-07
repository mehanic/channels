# 🧪 Тести `field.go`: Перевірка рефлексії та метаданих полів

Це **тестовий файл** для ядра бібліотеки `go-mp4`, який перевіряє коректність роботи **парсингу тегів `mp4:"..."`**, **побудови дерева полів** та **логіки вибору полів** для серіалізації/десеріалізації.

---

## 🎯 Коротка відповідь

> **Ці тести гарантують, що бібліотека коректно розуміє ваші декларації типу** `mp4:"0,size=32,opt=0x000001"` **і перетворює їх у виконувану логіку** — без цього неможлива автоматична робота з бітовими полями, версіями, прапорцями та динамічними розмірами.

---

## 📋 Огляд тестових функцій

### 🔹 `TestBuildField` — тест парсингу тегів та побудови метаданих

```go
func TestBuildField(t *testing.T) {
    // 🔹 Створюємо анонімну структуру з 24 полями різних типів
    box := &struct {
        FullBox     `mp4:"0,extend"`
        Int32       int32    `mp4:"1,size=32"`
        Int17       int32    `mp4:"2,size=17"`      // 🔹 Нестандартний розмір!
        Uint15      uint16   `mp4:"3,size=15"`
        Const       byte     `mp4:"4,size=8,const=0"`
        String      []byte   `mp4:"5,size=8,string"`
        PString     []byte   `mp4:"6,size=8,string=c_p"`  // 🔹 Pascal-string
        Dec         byte     `mp4:"7,size=8,dec"`
        Hex         byte     `mp4:"8,size=8,hex"`
        ISO639_2    []byte   `mp4:"9,size=8,iso639-2"`
        UUID        [16]byte `mp4:"10,size=8,uuid"`
        Hidden      byte     `mp4:"11,size=8,hidden"`
        Opt         byte     `mp4:"12,size=8,opt=0x000010"`
        NOpt        byte     `mp4:"13,size=8,nopt=0x000010"`
        DynOpt      byte     `mp4:"14,size=8,opt=dynamic"`
        Varint      uint64   `mp4:"15,varint"`
        DynSize     uint64   `mp4:"16,size=dynamic"`
        FixedLen    []byte   `mp4:"17,size=8,len=5"`
        DynLen      []byte   `mp4:"18,size=8,len=dynamic"`
        Ver         byte     `mp4:"19,size=8,ver=1"`
        NVer        byte     `mp4:"20,size=8,nver=1"`
        NotSorted22 byte     `mp4:"22,size=8"`  // 🔹 Невідсортований порядок
        NotSorted23 byte     `mp4:"23,size=8"`
        NotSorted21 byte     `mp4:"21,size=8"`
    }{}
    
    // 🔹 Будуємо метадані полів
    fs := buildFields(box)
    
    // 🔹 Перевіряємо кожне поле на коректність
    assert.Equal(t, &field{...}, fs[0])  // FullBox з extend
    assert.Equal(t, &field{...}, fs[1])  // Int32 size=32
    // ... ще 22 перевірки ...
}
```

**📊 Що тестується:**

| Поле | Тег | Що перевіряємо |
|------|-----|---------------|
| `FullBox` | `mp4:"0,extend"` | Прапорець `fieldExtend`, вкладені поля `Version`/`Flags` |
| `Int17` | `mp4:"2,size=17"` | Нестандартний розмір (не кратний 8) |
| `Const` | `mp4:"4,size=8,const=0"` | Поле `cnst="0"` для валідації |
| `String` | `mp4:"5,size=8,string"` | Прапорець `fieldString`, тип `stringType_C` |
| `PString` | `mp4:"6,size=8,string=c_p"` | Тип `stringType_C_P` (Pascal-string) |
| `Dec`/`Hex` | `mp4:"7,size=8,dec"` | Прапорці формату виводу |
| `ISO639_2` | `mp4:"9,size=8,iso639-2"` | Прапорець `fieldISO639_2` для мов |
| `UUID` | `mp4:"10,size=8,uuid"` | Прапорець `fieldUUID` для 16-байтних ідентифікаторів |
| `Hidden` | `mp4:"11,size=8,hidden"` | Прапорець `fieldHidden` (тільки читання) |
| `Opt` | `mp4:"12,size=8,opt=0x000010"` | `optFlag=0x000010` для умовних полів |
| `NOpt` | `mp4:"13,size=8,nopt=0x000010"` | `nOptFlag=0x000010` для виключення за прапорцем |
| `DynOpt` | `mp4:"14,size=8,opt=dynamic"` | Прапорець `fieldOptDynamic` для динамічної логіки |
| `Varint` | `mp4:"15,varint"` | Прапорець `fieldVarint` для MPEG-4 varint |
| `DynSize` | `mp4:"16,size=dynamic"` | Прапорець `fieldSizeDynamic` |
| `DynLen` | `mp4:"18,size=8,len=dynamic"` | Прапорець `fieldLengthDynamic` |
| `Ver`/`NVer` | `mp4:"19,size=8,ver=1"` | Версійні поля: `version=1` / `nVersion=1` |
| `NotSorted*` | `mp4:"22", "23", "21"` | ✅ Сортування за `order`: 21→22→23 у результаті |

**🎯 Ключова перевірка**: Чи коректно парситься **кожен тип тегу** і чи зберігаються метадані у структурі `field`.

---

### 🔹 `TestResolveFieldInstance` — тест динамічного розв'язання полів

```go
func TestResolveFieldInstance(t *testing.T) {
    // 🔹 Тестові дані для динамічних розмірів/довжин
    dynSize1 := uint(16)
    dynLen1 := uint(2)
    dynSize2 := uint(32)
    dynLen2 := uint(4)
    
    // 🔹 Об'єкти з різними реалізаціями ICustomFieldObject
    cfo1 := struct { mockBox; Box }{  // Повертає dynSize1/dynLen1
        mockBox: mockBox{
            DynSizeMap: map[string]uint{"TestField": dynSize1},
            DynLenMap:  map[string]uint{"TestField": dynLen1},
        },
    }
    cfo2 := struct { mockBox; Box }{  // Повертає dynSize2/dynLen2
        mockBox: mockBox{
            DynSizeMap: map[string]uint{"TestField": dynSize2},
            DynLenMap:  map[string]uint{"TestField": dynLen2},
        },
    }
    
    // 🔹 Тест-кейси
    testCases := []struct {
        name     string
        f        *field          // Вхідне поле з метаданими
        box      IImmutableBox   // Бокс для контексту
        parent   interface{}     // Батьківський об'єкт (може реалізувати ICustomFieldObject)
        wantSize uint            // Очікуваний розмір після розв'язання
        wantLen  uint            // Очікувана довжина після розв'язання
        wantCFO  ICustomFieldObject  // Очікуваний об'єкт для динамічних викликів
    }{
        {
            name: "dynamic size with non CustomFieldObject",
            f: &field{name: "TestField", flags: fieldSizeDynamic, length: fixedLen},
            box: &cfo1,
            parent: &nonCFO,  // 🔹 Не реалізує ICustomFieldObject
            wantSize: dynSize1,  // ← Беремо з box, бо parent не підходить
            wantCFO:  &cfo1,
        },
        {
            name: "dynamic size with CustomFieldObject",
            f: &field{name: "TestField", flags: fieldSizeDynamic, length: fixedLen},
            box: &cfo1,
            parent: &cfo2,  // 🔹 Реалізує ICustomFieldObject!
            wantSize: dynSize2,  // ← Беремо з parent, бо він має пріоритет
            wantCFO:  &cfo2,
        },
        // ... ще 2 кейси для fieldLengthDynamic ...
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            fi := resolveFieldInstance(tc.f, tc.box, reflect.ValueOf(tc.parent).Elem(), Context{})
            assert.Equal(t, tc.wantSize, fi.size)  // 🔹 Чи коректно обчислено розмір?
            assert.Equal(t, tc.wantLen, fi.length)  // 🔹 Чи коректно обчислено довжину?
            assert.Same(t, tc.wantCFO, fi.cfo)  // 🔹 Чи правильний об'єкт для викликів?
        })
    }
}
```

**🎯 Що тестується:**

| Сценарій | Очікувана поведінка |
|----------|-------------------|
| `fieldSizeDynamic` + parent без інтерфейсу | Використовуємо `box.GetFieldSize()` |
| `fieldSizeDynamic` + parent з інтерфейсом | Використовуємо `parent.GetFieldSize()` (пріоритет!) |
| `fieldLengthDynamic` + parent без інтерфейсу | Використовуємо `box.GetFieldLength()` |
| `fieldLengthDynamic` + parent з інтерфейсом | Використовуємо `parent.GetFieldLength()` (пріоритет!) |

**🔑 Ключова логіка**:
```
🔹 resolveFieldInstance() визначає, хто реалізує ICustomFieldObject:
   1. Спочатку перевіряємо parent.Addr().Interface().(ICustomFieldObject)
   2. Якщо не вдалося → fallback на box
   3. Викликаємо GetFieldSize/GetFieldLength на обраному об'єкті

🎯 Це дозволяє вкладеним структурам перевизначати динамічні параметри батьків!
```

---

### 🔹 `TestIsTargetField` — тест логіки вибору полів

```go
func TestIsTargetField(t *testing.T) {
    // 🔹 Тестовий бокс з Version=1, Flags=0x000006 (біти 1 і 2 встановлені)
    box := &struct { AnyTypeBox; FullBox }{
        FullBox: FullBox{
            Version: 1,
            Flags:   [3]byte{0x00, 0x00, 0x06},  // 0x06 = 00000110
        },
    }
    
    // 🔹 CFO з динамічною логікою опціональних полів
    cfo := struct { mockBox; Box }{
        mockBox: mockBox{
            DynOptMap: map[string]bool{
                "DynEnabledField":  true,   // ✅ Увімкнути
                "DynDisabledField": false,  // ❌ Вимкнути
            },
        },
    }
    
    // 🔹 Тест-кейси для різних сценаріїв
    testCases := []struct {
        name  string
        fi    *fieldInstance  // Поле з метаданими
        wants bool            // Очікуваний результат: чи читати це поле?
    }{
        // 🔹 Базовий випадок: немає обмежень → завжди true
        {name: "normal", fi: &fieldInstance{field: field{version: anyVersion, nVersion: anyVersion}}, wants: true},
        
        // 🔹 Версійні поля: ver=0 / ver=1 / nver=0 / nver=1
        {name: "ver=0", fi: &fieldInstance{field: field{version: 0}}, wants: false},  // ❌ box.Version=1 ≠ 0
        {name: "ver=1", fi: &fieldInstance{field: field{version: 1}}, wants: true},   // ✅ box.Version=1 == 1
        {name: "nver=0", fi: &fieldInstance{field: field{nVersion: 0}}, wants: true}, // ✅ box.Version=1 ≠ 0
        {name: "nver=1", fi: &fieldInstance{field: field{nVersion: 1}}, wants: false},// ❌ box.Version=1 == 1
        
        // 🔹 Прапорці opt=0xXXXX: читаємо, тільки якщо біт встановлено
        {name: "opt=0x000001", fi: &fieldInstance{field: field{optFlag: 0x000001}}, wants: false},  // ❌ Flags&0x01=0
        {name: "opt=0x000002", fi: &fieldInstance{field: field{optFlag: 0x000002}}, wants: true},   // ✅ Flags&0x02=1
        {name: "opt=0x000004", fi: &fieldInstance{field: field{optFlag: 0x000004}}, wants: true},   // ✅ Flags&0x04=1
        {name: "opt=0x000008", fi: &fieldInstance{field: field{optFlag: 0x000008}}, wants: false},  // ❌ Flags&0x08=0
        
        // 🔹 Прапорці nopt=0xXXXX: читаємо, тільки якщо біт НЕ встановлено
        {name: "nopt=0x000001", fi: &fieldInstance{field: field{nOptFlag: 0x000001}}, wants: true}, // ✅ Flags&0x01=0
        {name: "nopt=0x000002", fi: &fieldInstance{field: field{nOptFlag: 0x000002}}, wants: false},// ❌ Flags&0x02=1
        
        // 🔹 Динамічні опціональні поля: opt=dynamic
        {name: "opt=dynamic enabled", fi: &fieldInstance{field: field{name: "DynEnabledField", flags: fieldOptDynamic}}, wants: true},  // ✅ IsOptFieldEnabled=true
        {name: "opt=dynamic disabled", fi: &fieldInstance{field: field{name: "DynDisabledField", flags: fieldOptDynamic}}, wants: false}, // ❌ IsOptFieldEnabled=false
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            assert.Equal(t, tc.wants, isTargetField(box, tc.fi, Context{}))
        })
    }
}
```

**🎯 Що тестується:**

| Категорія | Логіка | Приклад |
|-----------|--------|---------|
| **Версія** | `ver=N` → тільки для версії N; `nver=N` → для всіх, крім N | `ver=1` + `box.Version=1` → ✅ |
| **opt=0xXXXX** | Читаємо, якщо `Flags & optFlag != 0` | `opt=0x000002` + `Flags=0x06` → ✅ (0x06&0x02=0x02) |
| **nopt=0xXXXX** | Читаємо, якщо `Flags & nOptFlag == 0` | `nopt=0x000001` + `Flags=0x06` → ✅ (0x06&0x01=0) |
| **opt=dynamic** | Читаємо, якщо `IsOptFieldEnabled(name, ctx) == true` | `DynEnabledField` → ✅, `DynDisabledField` → ❌ |

**🔢 Бітова арифметика на прикладі:**
```
🔹 Flags = 0x06 = 0000 0110 (біти 1 і 2 встановлені)

🔹 opt=0x000001 (біт 0):
   0000 0110 & 0000 0001 = 0000 0000 = 0 → ❌ не читаємо

🔹 opt=0x000002 (біт 1):
   0000 0110 & 0000 0010 = 0000 0010 = 2 → ✅ читаємо

🔹 opt=0x000004 (біт 2):
   0000 0110 & 0000 0100 = 0000 0100 = 4 → ✅ читаємо

🔹 opt=0x000008 (біт 3):
   0000 0110 & 0000 1000 = 0000 0000 = 0 → ❌ не читаємо
```

---

## 🔍 Як це працює разом: Повний потік

```
🔹 Крок 1: buildFields(box) → кеш метаданих
   │
   ├── 🔹 Рефлексія: reflect.TypeOf(box).Elem()
   ├── 🔹 Для кожного поля: парсинг тегу mp4:"..."
   ├── 🔹 Побудова дерева: вкладені структури (FullBox → Version/Flags)
   ├── 🔹 Сортування за order: [0,1,2,...] незалежно від порядку у коді
   │
   ▼
🔹 Крок 2: resolveFieldInstance(f, box, parent, ctx) → екземпляр поля
   │
   ├── 🔹 Визначення CFO: parent або box
   ├── 🔹 Якщо fieldSizeDynamic → fi.size = cfo.GetFieldSize(name, ctx)
   ├── 🔹 Якщо fieldLengthDynamic → fi.length = cfo.GetFieldLength(name, ctx)
   │
   ▼
🔹 Крок 3: isTargetField(box, fi, ctx) → чи обробляти це поле?
   │
   ├── 🔹 Перевірка версії: ver/nver
   ├── 🔹 Перевірка прапорців: opt/nopt
   ├── 🔹 Динамічна перевірка: opt=dynamic → IsOptFieldEnabled()
   │
   ▼
🔹 Крок 4: Якщо isTargetField=true → читаємо/пишемо поле
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Створення нового боксу з динамічними полями

```go
// 🔹 Опис структури з тегами
type CustomCCTVMeta struct {
    Box
    CameraID    uint32   `mp4:"0,size=32"`                    // 🔹 Фіксований розмір
    Timestamp   uint64   `mp4:"1,size=64"`                    // 🔹 64-бітний час
    Flags       uint8    `mp4:"2,size=8"`                     // 🔹 Прапорці
    Metadata    []byte   `mp4:"3,size=8,len=dynamic"`         // 🔹 Динамічна довжина!
    Extended    []byte   `mp4:"4,size=8,opt=dynamic,len=dynamic"` // 🔹 Умовне поле
}

// 🔹 Реалізація інтерфейсу для динаміки
func (c *CustomCCTVMeta) GetFieldLength(name string, ctx Context) uint {
    switch name {
    case "Metadata":
        return uint(len(c.Metadata))  // 🔹 Довжина = реальна довжина слайсу
    case "Extended":
        if c.Flags&0x01 != 0 {  // 🔹 Тільки якщо біт 0 встановлено
            return uint(len(c.Extended))
        }
        return 0
    }
    return 0
}

func (c *CustomCCTVMeta) IsOptFieldEnabled(name string, ctx Context) bool {
    if name == "Extended" {
        return c.Flags&0x01 != 0  // 🔹 Увімкнути, тільки якщо прапорець встановлено
    }
    return false
}

// 🔹 Використання: бібліотека сама викличе ці методи під час парсингу!
```

---

### 🔹 Приклад 2: Валідація `const` полів

```go
type ValidatedBox struct {
    Box
    Magic       byte  `mp4:"0,size=8,const=0xAB"`  // 🔹 Очікуємо 0xAB
    Version     byte  `mp4:"1,size=8,const=1"`     // 🔹 Тільки версія 1
    Reserved    byte  `mp4:"2,size=8,const=0"`     // 🔹 Завжди 0
}

// 🔹 Під час парсингу бібліотека перевірить:
// • якщо прочитане значення != cnst → помилка!
// • Це запобігає пошкодженим або невалідним файлам
```

---

### 🔹 Приклад 3: Версійні поля для сумісності

```go
type VersionedConfig struct {
    FullBox `mp4:"0,extend"`
    
    // 🔹 Поля для версії 0 (застарілі)
    OldParamV0 uint32 `mp4:"1,size=32,ver=0"`
    
    // 🔹 Поля для версії 1+ (нові)
    NewParamV1 uint64 `mp4:"2,size=64,nver=0"`  // ← nver=0 = "not version 0"
    
    // 🔹 Поле для всіх версій
    CommonParam uint16 `mp4:"3,size=16"`  // ← без ver/nver = завжди присутнє
}

// 🔹 Логіка isTargetField():
// • ver=0 + box.Version=1 → ❌ пропускаємо OldParamV0
// • nver=0 + box.Version=1 → ✅ читаємо NewParamV1
// • CommonParam → ✅ завжди читаємо
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `order` | Поля читаються у неправильному порядку → зсув даних | Завжди вказуйте унікальні порядкові номери: `mp4:"0", mp4:"1", mp4:"2"` |
| `boxstring` не останнім полем | Помилка: читає зайві байти або не вистачає даних | Завжди розміщуйте `boxstring` останнім у структурі |
| Забути `fieldSizeDynamic` | Динамічний розмір ігнорується → читаємо фіксовану кількість біт | Використовуйте `size=dynamic` + реалізуйте `GetFieldSize()` |
| Неправильна логіка `IsOptFieldEnabled` | Опціональні поля читаються завжди/ніколи → помилка парсингу | Перевіряйте логіку: повертати `true` тільки коли поле має бути присутнє |
| Конфлікт `ver`/`nver` | Поле читається для неправильної версії → помилка | Використовуйте `ver=0` для версії 0, `nver=0` для версій ≠0 |
| Неправильний тип для `string=c_p` | Рядок читається неправильно → "кракозябри" | Пам'ятайте: `c_p` = Pascal-string (довжина+дані), не використовуйте без потреби |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні нових структур боксів:
    • Вказуйте `order` для кожного поля: `mp4:"0", mp4:"1", ...`
    • Використовуйте `size=N` для фіксованих розмірів, `size=dynamic` для змінних
    • Для масивів: `len=dynamic` + реалізуйте `GetFieldLength()`
    • Для опціональних полів: `opt=0xXXXX` або `opt=dynamic` + `IsOptFieldEnabled()`
    • Для валідації: `const=...` для очікуваних значень

[ ] Для динамічних полів:
    • Реалізуйте інтерфейс `ICustomFieldObject` (GetFieldSize, GetFieldLength, IsOptFieldEnabled)
    • Переконайтеся, що методи повертають коректні значення для поточного контексту
    • Тестуйте з різними версіями/прапорцями для перевірки логіки

[ ] Для рядкових полів:
    • Використовуйте `string` для C-string, `boxstring` для рядків до кінця боксу
    • Переконайтеся, що `boxstring` є останнім полем у структурі
    • Для мов: використовуйте `iso639-2` для автоматичного кодування/декодування

[ ] Для версійних полів:
    • `ver=N` → поле тільки для версії N
    • `nver=N` → поле для всіх версій, крім N
    • Без `ver`/`nver` → поле для всіх версій

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

> **Ці тести — ваш "золотий стандарт" для надійної роботи з тегами `mp4:"..."`**.  
> Вони гарантують:
> • ✅ Коректний парсинг усіх типів тегів: `size`, `len`, `opt`, `ver`, `const`, `string`, `uuid`...
> • ✅ Правильне сортування полів за `order`, незалежно від порядку у коді
> • ✅ Надійну обробку динамічних розмірів/довжин через інтерфейси
> • ✅ Точну логіку вибору полів за версією, прапорцями та динамічними умовами
> • ✅ Типобезпечну роботу з нестандартними розмірами (17 біт, 15 біт тощо)

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Швидка розробка: описуйте структури боксів тегами, а не пишіть парсери вручну
- 🔧 Гнучкість: легко додавати нові типи боксів або модифікувати існуючі
- 🛡️ Надійність: автоматична валідація `const` полів, обробка краєвих випадків
- 🧪 Тестованість: кожен бокс можна протестувати ізольовано через `Marshal`/`Unmarshal`

Потребуєте допомоги зі створенням нової структури боксу з динамічними полями або з дебагом тегів `mp4:"..."`? Напишіть — покажу готовий приклад для вашого сценарію! 🚀🔧