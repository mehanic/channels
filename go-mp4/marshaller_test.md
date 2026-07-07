# 🧪 Тести серіалізації `go-mp4`: Повний огляд

Це **комплексний тест-сьют** для ядра бібліотеки `go-mp4`, який перевіряє коректність роботи **маршалінгу/анмаршалінгу** (серіалізації/десеріалізації) структур боксів у бінарний формат MP4 та назад.

---

## 🎯 Коротка відповідь

> **Ці тести гарантують, що будь-яка структура, описана тегами `mp4:"..."`, коректно перетворюється у байти і назад** — з підтримкою бітових полів, динамічних розмірів, varint, опціональних полів, версій та спеціальних типів даних.

---

## 📋 Огляд тестових функцій

### 🔹 `TestMarshal` — головний інтеграційний тест

```go
func TestMarshal(t *testing.T) {
    // 🔹 Створюємо складну тестову структуру з 36+ полями різних типів
    type testBox struct {
        mockBox
        FullBox `mp4:"0,extend"`
        
        // 🔹 Цілі числа різних розмірів
        Int32  int32  `mp4:"1,size=32"`
        Uint32 uint32 `mp4:"2,size=32"`
        Int64  int64  `mp4:"3,size=64"`
        Uint64 uint64 `mp4:"4,size=64"`
        
        // 🔹 Вирівнювання зліва (ліво-виправдані)
        Int32l   int32  `mp4:"5,size=29"`  // ← 29 біт!
        Padding0 uint8  `mp4:"6,size=3,const=0"`  // ← доповнення до 32
        // ...
        
        // 🔹 Вирівнювання справа (право-виправдані)
        Padding4 uint8  `mp4:"13,size=3,const=0"`  // ← спочатку паддінг
        Int32r   int32  `mp4:"14,size=29"`         // ← потім дані
        
        // 🔹 Varint (MPEG-4 змінна довжина)
        Varint uint16 `mp4:"21,varint"`
        
        // 🔹 Рядки, слайси, вказівники, масиви
        String   string `mp4:"22,string"`      // C-string
        StringCP string `mp4:"23,string=c_p"`  // Pascal-string
        Bytes    []byte `mp4:"24,size=8,len=5"`  // Фіксована довжина
        Uints    []uint `mp4:"25,size=16,len=dynamic"`  // Динамічна довжина
        Ptr      *inner `mp4:"26,extend"`  // Вкладена структура
        
        // 🔹 Булеві значення
        Bool     bool  `mp4:"27,size=1"`  // ← 1 біт!
        Padding8 uint8 `mp4:"28,size=7,const=0"`  // ← доповнення до байта
        
        // 🔹 Динамічний розмір
        DynUint uint `mp4:"29,size=dynamic"`
        
        // 🔹 Опціональні поля за прапорцями
        OptUint1 uint `mp4:"30,size=8,opt=0x0100"`   // ✅ увімкнено (прапорець встановлено)
        OptUint2 uint `mp4:"31,size=8,opt=0x0200"`   // ❌ вимкнено
        OptUint3 uint `mp4:"32,size=8,nopt=0x0400"`  // ❌ вимкнено (nopt)
        OptUint4 uint `mp4:"33,size=8,nopt=0x0800"`  // ✅ увімкнено (nopt)
        
        // 🔹 Невідсортований порядок (перевірка сортування за `order`)
        NotSorted35 uint8 `mp4:"35,size=8,dec"`
        NotSorted36 uint8 `mp4:"36,size=8,dec"`
        NotSorted34 uint8 `mp4:"34,size=8,dec"`  // ← order=34 має бути перед 35,36
    }
    
    // 🔹 Налаштування динамічних параметрів через mockBox
    mb := mockBox{
        Type: boxType,
        DynSizeMap: map[string]uint{"DynUint": 24},  // ← DynUint має розмір 24 біти
        DynLenMap:  map[string]uint{"Uints": 5},     // ← Uints має 5 елементів
    }
    
    // 🔹 Вихідні дані для серіалізації
    src := testBox{
        mockBox: mb,
        FullBox: FullBox{Version: 0, Flags: [3]byte{0x00, 0x05, 0x00}},
        Int32:  -0x1234567,  // 🔹 Від'ємне число
        Uint32: 0x1234567,
        // ... ще 30+ полів ...
        Varint: 0x1234,  // 🔹 4660 десяткове → кодується як varint
        String: "abema.tv",
        Bool: true,  // 🔹 1 біт = 1
        DynUint: 0x123456,  // 🔹 24 біти
        OptUint1: 0x11,  // ✅ Прапорець 0x0100 встановлено у Flags
        OptUint4: 0x44,  // ✅ Прапорець 0x0800 НЕ встановлено → nopt=0x0800 → увімкнено
        NotSorted34: 34, NotSorted35: 35, NotSorted36: 36,  // 🔹 Перевірка сортування
    }
    
    // 🔹 Очікувані байти (еталон)
    bin := []byte{
        0,                // version
        0x00, 0x05, 0x00, // flags
        0xfe, 0xdc, 0xba, 0x99, // Int32 = -0x1234567 (доповнення до двійки)
        0x01, 0x23, 0x45, 0x67, // Uint32
        // ... ще 100+ байт ...
        0x80, 0x80, 0xa4, 0x34, // Varint: 0x1234 → MPEG-4 varint encoding
        // ...
        0x80,             // Bool=true (0x80 = 10000000b, старший біт=1)
        0x12, 0x34, 0x56, // DynUint (24 біти = 3 байти)
        0x11,       // OptUint1 (увімкнено)
        // ❌ OptUint2, OptUint3 відсутні (вимкнені прапорцями)
        0x44,       // OptUint4 (увімкнено через nopt)
        34, 35, 36, // NotSorted: записані у порядку 34→35→36, незалежно від порядку у коді
    }
    
    // 🔹 ТЕСТ 1: Marshal (структура → байти)
    buf := &bytes.Buffer{}
    n, err := Marshal(buf, &src, Context{})
    require.NoError(t, err)
    assert.Equal(t, uint64(len(bin)), n)  // 🔹 Перевірка розміру
    assert.Equal(t, bin, buf.Bytes())      // 🔹 Перевірка вмісту: байт в байт!
    
    // 🔹 ТЕСТ 2: Unmarshal (байти → структура) + Round-trip
    dst := testBox{mockBox: mb}
    n, err = Unmarshal(bytes.NewReader(bin), uint64(len(bin)+8), &dst, Context{})
    assert.NoError(t, err)
    assert.Equal(t, uint64(len(bin)), n)
    assert.Equal(t, src, dst)  // 🔹 КЛЮЧОВА ПЕРЕВІРКА: round-trip ідемпотентність!
}
```

**📊 Що тестується у цьому кейсі:**

| Категорія | Поле | Що перевіряємо |
|-----------|------|---------------|
| **Цілі числа** | `Int32`, `Uint64` | Коректне кодування знакових/беззнакових, big-endian порядок |
| **Нестандартні розміри** | `Int32l size=29` | Запис 29 біт у 4 байти, з паддінгом до 32 |
| **Вирівнювання** | `Int32l` (ліво) / `Int32r` (право) | Правильне позиціонування даних у байті |
| **Varint** | `Varint uint16` | MPEG-4 varint кодування: `0x1234` → `[0x80,0x80,0xa4,0x34]` |
| **Рядки** | `String` (C) / `StringCP` (Pascal) | Нуль-термінація vs довжина+дані |
| **Масиви** | `Bytes len=5` / `Uints len=dynamic` | Фіксована та динамічна довжина |
| **Вкладені структури** | `Ptr *inner` | Рекурсивний маршалінг |
| **Булеві значення** | `Bool size=1` | Запис 1 біта (0x80 для true) + паддінг до байта |
| **Динамічний розмір** | `DynUint size=dynamic` | Виклик `GetFieldSize()` через mockBox |
| **Опціональні поля** | `OptUint1-4` | Логіка `opt`/`nopt` за прапорцями `Flags=0x000500` |
| **Сортування** | `NotSorted34-36` | Поля записуються у порядку `order`, а не у порядку у коді |

---

### 🔹 `TestUnsupportedBoxVersionErr` — тест перевірки версій

```go
func TestUnsupportedBoxVersionErr(t *testing.T) {
    type testBox struct {
        mockBox
        FullBox `mp4:"0,extend"`
    }
    
    // 🔹 Реєструємо бокс з підтримкою версій 0, 1, 2
    AddBoxDef(&testBox{mockBox: mb}, 0, 1, 2)
    
    // 🔹 Тест-кейси для різних версій
    for _, e := range []struct {
        version byte
        enabled bool  // 🔹 Чи очікуємо успіх?
    }{
        {version: 0, enabled: true},   // ✅ Підтримується
        {version: 1, enabled: true},   // ✅ Підтримується
        {version: 2, enabled: true},   // ✅ Підтримується
        {version: 3, enabled: false},  // ❌ Не підтримується
        {version: 4, enabled: false},  // ❌ Не підтримується
    } {
        bin := []byte{e.version, 0x00, 0x00, 0x00}  // version + flags
        
        dst := testBox{mockBox: mb}
        n, err := Unmarshal(bytes.NewReader(bin), uint64(len(bin)+8), &dst, Context{})
        
        if e.enabled {
            assert.NoError(t, err)  // ✅ Успішний парсинг
            assert.Equal(t, expected, dst)
        } else {
            assert.Error(t, err)  // ❌ Помилка: ErrUnsupportedBoxVersion
        }
    }
}
```

**🎯 Призначення**: Перевірити, що бібліотека коректно відхиляє непідтримувані версії боксів, повертаючи `ErrUnsupportedBoxVersion`.

---

### 🔹 `TestReadPString` — тест парсингу рядків: C-style vs Pascal-style

```go
func TestReadPString(t *testing.T) {
    type testBox struct {
        mockBox
        Box
        String   string `mp4:"1,string"`      // ← C-string (null-terminated)
        StringCP string `mp4:"2,string=c_p"`  // ← Pascal-string (length-prefixed)
        Uint32   uint32 `mp4:"3,size=32"`
    }
    
    testCases := []struct {
        name      string
        src       []byte      // 🔹 Вхідні байти для StringCP
        isPString bool        // 🔹 Чи вважати це Pascal-string?
        wants     string      // 🔹 Очікуваний результат
    }{
        {
            name:      "c style string",
            src:       []byte{0x05, 'a', 'b', 'e', 'm', 'a'},  // ← 0x05 = довжина
            isPString: true,  // 🔹 tryReadPString() поверне true
            wants:     "abema",
        }, {
            name:      "pascal style string",
            src:       []byte{'a', 'b', 'e', 'm', 'a', 0x00},  // ← null-термінатор
            isPString: true,  // 🔹 tryReadPString() поверне false → fallback на C-string
            wants:     "abema",
        }, {
            name:      "pascal style string isPString=true",
            src:       []byte{0x0a, 'a', 'b', 'e', 'm', 'a', '1', '2', '3', '4', '5'},
            isPString: true,
            wants:     "abema12345",  // ← 0x0a = 10 байт даних
        }, {
            name:      "pascal style string isPString=false",
            src:       []byte{0x0a, 'a', 'b', 'e', 'm', 'a', '1', '2', '3', '4', '5', 0x00},
            isPString: false,  // ← Не вважаємо Pascal → читаємо як C-string
            wants:     "\nabema12345",  // ← 0x0a = '\n' + решта до 0x00
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // 🔹 Налаштування mockBox з логікою IsPString
            mb := mockBox{
                Type: boxType,
                IsPStringMap: map[string]bool{"StringCP": tc.isPString},
            }
            
            // 🔹 Вхідні дані: String (C-style) + StringCP (тестові) + Uint32
            src := append(append([]byte{
                'h', 'e', 'l', 'l', 'o', 0x00,  // ← "hello" + null
            }, tc.src...),  // ← тестові дані для StringCP
                0x01, 0x23, 0x45, 0x67,  // ← Uint32
            )
            
            dst := testBox{mockBox: mb}
            n, err := Unmarshal(bytes.NewReader(src), uint64(len(src)), &dst, Context{})
            
            require.NoError(t, err)
            assert.Equal(t, "hello", dst.String)  // ← C-string завжди працює
            assert.Equal(t, tc.wants, dst.StringCP)  // ← Залежить від isPString
            assert.Equal(t, uint32(0x01234567), dst.Uint32)
        })
    }
}
```

**🎯 Ключова логіка `tryReadPString`**:
```
🔹 Алгоритм:
1. Читаємо перший байт → plen (довжина рядка)
2. Перевіряємо: чи вистачає байт у потоці для plen символів?
3. Читаємо plen байт → candidate string
4. Викликаємо fi.cfo.IsPString(name, buf, remainingSize, ctx)
   • Якщо true → це Pascal-string → повертаємо результат
   • Якщо false → це не Pascal → відкатуємо позицію → fallback на C-string

🔹 Навіщо це? Деякі бокси (напр. у QuickTime) використовують гібридний формат:
   • Якщо перший байт ≤ залишок і виглядає як довжина → Pascal
   • Інакше → C-string
```

---

### 🔹 `TestReadVarint` / `TestWriteVarint` — тест MPEG-4 varint

```go
func TestReadVarint(t *testing.T) {
    testCases := []struct {
        name     string
        input    []byte      // 🔹 Вхідні байти varint
        err      bool        // 🔹 Чи очікуємо помилку?
        expected uint64      // 🔹 Очікуване значення
    }{
        {name: "0", input: []byte{0x0}, expected: 0},
        {name: "1 byte", input: []byte{0x6c}, expected: 0x6c},  // 108
        {name: "2 bytes", input: []byte{0xac, 0x52}, expected: 0x1652},  // 5714
        {name: "3 bytes", input: []byte{0xac, 0xd2, 0x43}, expected: 0xb2943},  // 731459
        {name: "overrun", input: []byte{0xac, 0xd2, 0xef}, err: true},  // ❌ Невірне кодування
        {name: "1 byte padded", input: []byte{0x80, 0x80, 0x80, 0x6c}, expected: 0x6c},
        {name: "2 byte padded", input: []byte{0x80, 0x80, 0x81, 0x0c}, expected: 0x8c},
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            u := &unmarshaller{
                reader: bitio.NewReadSeeker(bytes.NewReader(tc.input)),
                size:   uint64(len(tc.input)),
            }
            val, err := u.readUvarint()
            if tc.err {
                require.Error(t, err)  // ✅ Очікуємо помилку
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tc.expected, val)  // ✅ Перевірка значення
        })
    }
}

func TestWriteVarint(t *testing.T) {
    testCases := []struct {
        name     string
        input    uint64    // 🔹 Вхідне значення
        expected []byte    // 🔹 Очікуване кодування varint
    }{
        {name: "0", input: 0x0, expected: []byte{0x80, 0x80, 0x80, 0x00}},
        {name: "1 byte", input: 0x6c, expected: []byte{0x80, 0x80, 0x80, 0x6c}},
        {name: "1 byte into 2 bytes", input: 0x8c, expected: []byte{0x80, 0x80, 0x81, 0x0c}},
        {name: "2 bytes", input: 0x1652, expected: []byte{0x80, 0x80, 0xac, 0x52}},
        {name: "3 bytes", input: 0xb2943, expected: []byte{0x80, 0xac, 0xd2, 0x43}},
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            var b bytes.Buffer
            m := &marshaller{writer: bitio.NewWriter(&b)}
            err := m.writeUvarint(tc.input)
            require.NoError(t, err)
            assert.Equal(t, tc.expected, b.Bytes())  // ✅ Перевірка кодування
        })
    }
}
```

**🔢 Як працює MPEG-4 varint:**

```
📐 Формат: кожен байт = [продовження:1][дані:7]
• Біт 7 = 1 → є наступний байт
• Біт 7 = 0 → останній байт
• Дані збираються від старших до молодших груп

🔢 Приклад: 0x1234 (4660 десяткове)
Бінарно: 0001 0010 0011 0100

Групуємо по 7 біт (від старших):
• Група 3: 0000000 → байт 3: 10000000 = 0x80 (біт 7=1 → продовження)
• Група 2: 0000000 → байт 2: 10000000 = 0x80 (біт 7=1 → продовження)  
• Група 1: 0010100 → байт 1: 10100100 = 0xa4 (біт 7=1 → продовження)
• Група 0: 0110100 → байт 0: 00110100 = 0x34 (біт 7=0 → кінець)

✅ Результат: [0x80, 0x80, 0xa4, 0x34]

🔢 Декодування:
• 0x80 = 10000000 → дані=0000000, продовження=1
• 0x80 = 10000000 → дані=0000000, продовження=1
• 0xa4 = 10100100 → дані=0100100, продовження=1
• 0x34 = 00110100 → дані=0110100, продовження=0 ← кінець

Збірка: ((0<<7 + 0)<<7 + 0100100)<<7 + 0110100 = 0001 0010 0011 0100 = 0x1234 ✅
```

> ⚠️ **Увага**: У коді бібліотеки є потенційна помилка в `readUvarint` — він збирає значення від старших груп до молодших, але MPEG-4 varint зазвичай кодує від молодших до старших. Тести проходять, бо `writeUvarint`/`readUvarint` симетричні, але можуть бути несумісні з іншими реалізаціями.

---

## 🔍 Ключові перевірки у `TestMarshal`

### 🔹 Round-trip ідемпотентність

```go
// 🔹 Крок 1: структура → байти
Marshal(buf, &src, ctx) → bin

// 🔹 Крок 2: байти → структура
Unmarshal(bin, &dst, ctx) → dst

// 🔹 Крок 3: порівняння
assert.Equal(t, src, dst)  // ✅ Повна ідентичність!
```

**🎯 Навіщо це?** Гарантує, що серіалізація не втрачає дані і може бути відновлена точно.

---

### 🔹 Опціональні поля за прапорцями

```
🔹 Flags = 0x000500 = 0000 0000 0000 0101 0000 0000

🔹 OptUint1: opt=0x0100
   Flags & 0x0100 = 0x0100 ≠ 0 → ✅ увімкнено → записуємо 0x11

🔹 OptUint2: opt=0x0200
   Flags & 0x0200 = 0x0000 = 0 → ❌ вимкнено → пропускаємо

🔹 OptUint3: nopt=0x0400
   Flags & 0x0400 = 0x0400 ≠ 0 → ❌ вимкнено (nopt: вимкнути, якщо біт=1)

🔹 OptUint4: nopt=0x0800
   Flags & 0x0800 = 0x0000 = 0 → ✅ увімкнено (nopt: увімкнути, якщо біт=0) → записуємо 0x44

✅ Результат у байтах: [..., 0x11, 0x44, ...] ← тільки два опціональні поля
```

---

### 🔹 Сортування за `order`

```go
// 🔹 У коді структури поля у порядку:
NotSorted35 `mp4:"35,size=8"`  // order=35
NotSorted36 `mp4:"36,size=8"`  // order=36
NotSorted34 `mp4:"34,size=8"`  // order=34 ← має бути першим!

// 🔹 Після buildFields() + сортування:
// Поля у порядку: 34 → 35 → 36

// 🔹 У вихідних байтах:
..., 34, 35, 36  // ✅ Записані у порядку order, а не у порядку у коді
```

**🎯 Навіщо це?** Забезпечує коректний порядок серіалізації незалежно від порядку оголошення полів у структурі.

---

### 🔹 Нестандартні розміри: 29 біт, 59 біт

```
🔹 Int32l: size=29 → записується у 4 байти (32 біти), але використовуються тільки 29

📐 Формат: [дані:29][паддінг:3]
• Значення: -0x123456 = -1193046 десяткове
• Доповнення до двійки: 0xFFEDCA8 (29 біт)
• Запис: 1111 1111 1110 1101 1100 1010 1000 + 000 (паддінг)
• Байти: 0xFF 0x6E 0x5D 0x50 ✅

🔹 Padding0: size=3, const=0 → завжди 000
• Перевірка при парсингу: якщо прочитане ≠ 0 → помилка!
```

**🎯 Навіщо це?** Деякі стандарти (напр. MPEG-4, QuickTime) використовують поля, не кратні 8 бітам.

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний порядок `order` | Поля записуються не в тому порядку → зсув даних | Завжди вказуйте унікальні `order`: `mp4:"0", mp4:"1", ...` |
| Забути `const=0` для паддінгу | Паддінг читається як дані → помилка валідації | Завжди вказуйте `const=0` для полів-заповнювачів |
| Неправильна логіка `IsPString` | Pascal-string читається як C-string → "кракозябри" | Реалізуйте `IsPString()` для точного визначення формату |
| Помилка у varint кодуванні | Числа читаються неправильно → помилки в метаданих | Перевіряйте симетрію `writeUvarint`/`readUvarint` |
| Ігнорування вирівнювання булевих полів | `Bool size=1` займає 1 біт, але треба доповнити до байта | Додавайте `Padding size=7, const=0` після булевих полів |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні нових структур боксів:
    • Вказуйте `order` для кожного поля: `mp4:"0", mp4:"1", ...`
    • Для полів, не кратних 8 бітам: додавайте `Padding size=N, const=0`
    • Для опціональних полів: тестуйте з різними комбінаціями прапорців
    • Для рядків: обирайте `string` (C) або `string=c_p` (Pascal) залежно від стандарту

[ ] Для динамічних полів:
    • Реалізуйте `GetFieldSize()` / `GetFieldLength()` у mockBox для тестів
    • Перевіряйте, що методи повертають коректні значення для всіх сценаріїв
    • Тестуйте граничні випадки: порожні масиви, максимальні розміри

[ ] Для varint полів:
    • Перевіряйте симетрію кодування/декодування: write→read→порівняння
    • Тестуйте з різними розмірами: 1 байт, 2 байти, 3 байти, паддінг
    • Перевіряйте обробку помилок: переповнення, невірне кодування

[ ] Для опціональних полів:
    • Тестуйте всі комбінації прапорців: opt=0x01, opt=0x02, nopt=0x04...
    • Перевіряйте, що вимкнені поля дійсно не записуються/не читаються
    • Логувайте стан прапорців для дебагу: log.Printf("Flags: 0x%06x", flags)

[ ] Для тестування round-trip:
    • Завжди перевіряйте: Marshal → Unmarshal → порівняння з оригіналом
    • Тестуйте з різними значеннями: 0, максимум, від'ємні, граничні
    • Перевіряйте вирівнювання: `wbits % 8 == 0` після маршалінгу
```

---

## 🎯 Висновок

> **Ці тести — ваш "золотий стандарт" для надійної серіалізації у `go-mp4`**.  
> Вони гарантують:
> • ✅ Коректну обробку полів довільного розміру (1 біт, 29 біт, 59 біт...)
> • ✅ Правильне кодування MPEG-4 varint
> • ✅ Надійну логіку опціональних полів за прапорцями `opt`/`nopt`
> • ✅ Автоматичне сортування полів за `order`, незалежно від порядку у коді
> • ✅ Гнучку обробку рядків: C-string vs Pascal-string через `IsPString`
> • ✅ Ідемпотентність round-trip: структура → байти → структура

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Впевненість у коректності серіалізації/десеріалізації кастомних боксів
- 🔧 Легке додавання нових типів полів без ризику зламати існуючий код
- ⚡ Оптимізація: бібліотека сама обирає швидкі шляхи для масивів байт
- 🛡️ Надійність: автоматична валідація `const`, вирівнювання, переповнення
- 🧪 Тестованість: кожен новий бокс можна протестувати за цим шаблоном

Потребуєте допомоги зі створенням тестів для вашого кастомного боксу або з дебагом серіалізації? Напишіть — покажу готовий приклад для вашого сценарію! 🚀🔧