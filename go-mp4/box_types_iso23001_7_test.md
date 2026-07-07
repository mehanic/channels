# 🧪 Тест `TestBoxTypesISO23001_7`: DRM бокси `pssh` та `tenc`

Це **інтеграційний тест** для бібліотеки `go-mp4`, який перевіряє коректну роботу **серіалізації/десеріалізації** боксів стандарту **ISO/IEC 23001-7 (Common Encryption, CENC)**: `pssh` (Protection System Specific Header) та `tenc` (Track Encryption).

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що DRM-параметри (SystemID, KIDs, IV, ключі) коректно перетворюються між структурою Go та бінарним форматом** — критично для відтворення захищеного контенту у плеєрах з підтримкою Widevine/PlayReady/FairPlay.

---

## 📋 Структура тесту

```go
func TestBoxTypesISO23001_7(t *testing.T) {
    // 1. Масив тест-кейсів (5 кейсів: 2 для pssh, 3 для tenc)
    testCases := []struct {
        name string           // Назва тесту (напр. "pssh: version 1: with KIDs")
        src  IImmutableBox    // Вихідна структура (для Marshal)
        dst  IBox             // Порожня структура (для Unmarshal)
        bin  []byte           // Очікувані байти (еталон)
        str  string           // Очікуваний рядок для Stringify()
        ctx  Context          // Контекст парсингу
    }{ ... }

    // 2. Запуск кожного кейсу
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // Тестуємо 4 операції: Marshal → Unmarshal → UnmarshalAny → Stringify
        })
    }
}
```

---

## 🔍 Детальний розбір тест-кейсів

### 🔹 Кейс 1: `pssh: version 0: no KIDs`

```go
{
    name: "pssh: version 0: no KIDs",
    src: &Pssh{
        FullBox: FullBox{Version: 0, Flags: [3]byte{0,0,0}},
        SystemID: [16]byte{0x01, 0x02, ..., 0x10},  // 🔹 UUID DRM-системи
        DataSize: 5,
        Data:     []byte{0x21, 0x22, 0x23, 0x24, 0x25},  // 🔹 Сирі DRM-дані
    },
    bin: []byte{
        0,                // version
        0x00, 0x00, 0x00, // flags
        // SystemID (16 байт UUID)
        0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
        0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
        0x00, 0x00, 0x00, 0x05, // DataSize = 5
        0x21, 0x22, 0x23, 0x24, 0x25,  // Data
    },
    str: `Version=0 Flags=0x000000 ` +
        `SystemID=01020304-0506-0708-090a-0b0c0d0e0f10 ` +
        `DataSize=5 ` +
        `Data=[0x21, 0x22, 0x23, 0x24, 0x25]`,
},
```

**🔢 Ключові моменти:**
- ✅ **Версія 0**: поля `KIDCount` та `KIDs` **відсутні** (тег `nver=0` у визначенні)
- ✅ **SystemID**: 16 байт у порядку як є, без перестановок
- ✅ **DataSize**: 4 байти uint32, big-endian: `0x00000005`
- ✅ **Stringify**: UUID форматується як `01020304-0506-0708-090a-0b0c0d0e0f10` (через `uuid.UUID(e.KID).String()`)

> 🎯 **Для вашого CCTV**: Версія 0 використовується для простих сценаріїв з одним ключем.

---

### 🔹 Кейс 2: `pssh: version 1: with KIDs`

```go
{
    name: "pssh: version 1: with KIDs",
    src: &Pssh{
        FullBox: FullBox{Version: 1, Flags: [3]byte{0,0,0}},
        SystemID: [16]byte{0x01, 0x02, ..., 0x10},
        KIDCount: 2,  // 🔹 Кількість ключів
        KIDs: []PsshKID{
            {KID: [16]byte{0x11, 0x12, ..., 0x10}},  // 🔹 Key ID #1
            {KID: [16]byte{0x21, 0x22, ..., 0x20}},  // 🔹 Key ID #2
        },
        DataSize: 5,
        Data:     []byte{0x21, 0x22, 0x23, 0x24, 0x25},
    },
    bin: []byte{
        1,                // version = 1
        0x00, 0x00, 0x00, // flags
        // SystemID (16 байт)
        0x01, 0x02, ..., 0x10,
        0x00, 0x00, 0x00, 0x02,  // 🔹 KIDCount = 2 (4 байти uint32)
        // 🔹 KIDs: 2 × 16 байт = 32 байти
        0x11, 0x12, ..., 0x10,  // KID #1
        0x21, 0x22, ..., 0x20,  // KID #2
        0x00, 0x00, 0x00, 0x05,  // DataSize
        0x21, 0x22, 0x23, 0x24, 0x25,  // Data
    },
    str: `Version=1 Flags=0x000000 ` +
        `SystemID=01020304-0506-0708-090a-0b0c0d0e0f10 ` +
        `KIDCount=2 ` +
        `KIDs=[11121314-1516-1718-191a-1b1c1d1e1f10, 21222324-2526-2728-292a-2b2c2d2e2f20] ` +
        `DataSize=5 ` +
        `Data=[0x21, 0x22, 0x23, 0x24, 0x25]`,
},
```

**🔢 Ключові моменти:**
- ✅ **Версія 1**: поля `KIDCount` та `KIDs` **присутні** (тег `nver=0` → "not version 0")
- ✅ **KIDCount**: 4 байти uint32, big-endian: `0x00000002`
- ✅ **KIDs**: масив структур `PsshKID`, кожна з 16-байтним `KID`
- ✅ **Stringify**: масив UUID форматується як `[uuid1, uuid2]` через `uuid.UUID(e.KID).String()`

> 🎯 **Для вашого CCTV**: Версія 1 потрібна для контенту з кількома ключами (напр. різні регіони, рівні доступу).

---

### 🔹 Кейс 3: `tenc: DefaultIsProtected=1 DefaultPerSampleIVSize=0`

```go
{
    name: "tenc: DefaultIsProtected=1 DefaultPerSampleIVSize=0",
    src: &Tenc{
        FullBox: FullBox{Version: 1, Flags: [3]byte{0,0,0}},
        Reserved:               0x00,
        DefaultCryptByteBlock:  0x0a,  // 🔹 10 байт шифрувати
        DefaultSkipByteBlock:   0x0b,  // 🔹 11 байт пропускати
        DefaultIsProtected:     1,     // 🔹 Зашифровано!
        DefaultPerSampleIVSize: 0,     // 🔹 Немає пер-семпл IV → використовується ConstantIV
        DefaultKID: [16]byte{0x01,0x23,...,0xef},  // 🔹 UUID ключа
        DefaultConstantIVSize: 4,      // 🔹 Розмір постійного IV
        DefaultConstantIV:     []byte{0x01,0x23,0x45,0x67},  // 🔹 Сам постійний IV
    },
    bin: []byte{
        1,                // version
        0x00, 0x00, 0x00, // flags
        0x00,       // Reserved
        0xab,       // 🔹 Crypt(4 біти) + Skip(4 біти) = 0xa | 0xb = 0xab
        0x01, 0x00, // 🔹 IsProtected(1 байт) + IVSize(1 байт) = 0x01, 0x00
        // DefaultKID (16 байт)
        0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
        0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
        0x04,                   // 🔹 DefaultConstantIVSize = 4
        0x01, 0x23, 0x45, 0x67, // 🔹 DefaultConstantIV (4 байти)
    },
    str: `Version=1 Flags=0x000000 ` +
        `Reserved=0 ` +
        `DefaultCryptByteBlock=10 ` +
        `DefaultSkipByteBlock=11 ` +
        `DefaultIsProtected=1 ` +
        `DefaultPerSampleIVSize=0 ` +
        `DefaultKID=01234567-89ab-cdef-0123-456789abcdef ` +
        `DefaultConstantIVSize=4 ` +
        `DefaultConstantIV=[0x1, 0x23, 0x45, 0x67]`,
},
```

**🔢 Ключові моменти:**
- ✅ **Crypt/Skip у одному байті**: `DefaultCryptByteBlock` (4 біти) + `DefaultSkipByteBlock` (4 біти) = `0xab`
- ✅ **IsProtected + IVSize**: два окремих байти: `0x01` (protected), `0x00` (IVSize=0)
- ✅ **Опціональні поля**: `DefaultConstantIVSize` та `DefaultConstantIV` присутні, бо `IsProtected=1` та `IVSize=0` (логіка в `IsOptFieldEnabled`)
- ✅ **UUID**: `DefaultKID` форматується як `01234567-89ab-cdef-0123-456789abcdef`

> 🎯 **Для вашого CCTV**: Цей режим (пер-семпл IV = 0) використовується рідко — тільки для спеціальних сценаріїв з постійним IV.

---

### 🔹 Кейс 4: `tenc: DefaultIsProtected=0 DefaultPerSampleIVSize=0`

```go
{
    name: "tenc: DefaultIsProtected=0 DefaultPerSampleIVSize=0",
    src: &Tenc{
        // ... аналогічно, але:
        DefaultIsProtected:     0,  // 🔹 НЕ зашифровано!
        DefaultPerSampleIVSize: 0,
        // DefaultConstantIVSize та DefaultConstantIV відсутні!
    },
    bin: []byte{
        // ... до DefaultKID ...
        0x00, 0x00,  // 🔹 IsProtected=0, IVSize=0
        // 🔹 Немає DefaultConstantIVSize та DefaultConstantIV!
    },
    str: `... DefaultIsProtected=0 DefaultPerSampleIVSize=0 ...`,  // 🔹 Без ConstantIV у виводі
},
```

**🔢 Ключові моменти:**
- ✅ **IsProtected=0**: контент не зашифрований → решта полів ігноруються
- ✅ **Опціональні поля відсутні**: `IsOptFieldEnabled` повертає `false` → `DefaultConstantIV*` не записуються/не читаються
- ✅ **Stringify**: не виводить `DefaultConstantIVSize` та `DefaultConstantIV`, бо вони не увімкнені

> 🎯 **Для вашого CCTV**: Це "прозорий" режим — контент відтворюється без ліцензії, але структура DRM-боксів зберігається для сумісності.

---

### 🔹 Кейс 5: `tenc: DefaultIsProtected=1 DefaultPerSampleIVSize=1`

```go
{
    name: "tenc: DefaultIsProtected=1 DefaultPerSampleIVSize=1",
    src: &Tenc{
        // ... аналогічно, але:
        DefaultIsProtected:     1,   // 🔹 Зашифровано
        DefaultPerSampleIVSize: 1,   // 🔹 1 байт IV на семпл (нестандартно!)
        // 🔹 DefaultConstantIV* відсутні, бо IVSize != 0
    },
    bin: []byte{
        // ... до DefaultKID ...
        0x01, 0x01,  // 🔹 IsProtected=1, IVSize=1
        // 🔹 Немає DefaultConstantIVSize та DefaultConstantIV!
    },
    str: `... DefaultIsProtected=1 DefaultPerSampleIVSize=1 ...`,  // 🔹 Без ConstantIV у виводі
},
```

**🔢 Ключові моменти:**
- ✅ **IVSize=1**: нестандартне значення (зазвичай 0 або 16), але допустиме стандартом
- ✅ **Опціональні поля відсутні**: `IsOptFieldEnabled` повертає `false`, бо `IVSize != 0`
- ✅ **Stringify**: не виводить `DefaultConstantIV*`, бо вони не увімкнені

> ⚠️ **Увага**: `IVSize=1` може не підтримуватися всіма плеєрами. Рекомендується `0` або `16`.

---

## 🔄 Чотири операції, що тестуються

### 🔹 1. `Marshal` — серіалізація (структура → байти)
```go
buf := bytes.NewBuffer(nil)
n, err := Marshal(buf, tc.src, tc.ctx)
assert.Equal(t, tc.bin, buf.Bytes())  // байт в байт!
```
**Перевіряє**: Чи коректно формуються байти з урахуванням версії, опціональних полів, бітової упаковки.

---

### 🔹 2. `Unmarshal` — десеріалізація (байти → структура)
```go
r := bytes.NewReader(tc.bin)
n, err = Unmarshal(r, uint64(len(tc.bin)), tc.dst, tc.ctx)
assert.Equal(t, tc.src, tc.dst)  // 🔁 round-trip: ідемпотентність!
```
**Перевіряє**: Чи відновлюється структура точно такою ж після читання, з урахуванням `nver=0`, `opt=dynamic`.

---

### 🔹 3. `UnmarshalAny` — динамічний парсинг за типом
```go
dst, n, err := UnmarshalAny(bytes.NewReader(tc.bin), tc.src.GetType(), ...)
assert.Equal(t, tc.src, dst)
```
**Перевіряє**: Чи працює універсальний парсер для невідомих наперед типів боксів (`pssh`, `tenc`).

---

### 🔹 4. `Stringify` — людино-читабельний вивід
```go
str, err := Stringify(tc.src, tc.ctx)
assert.Equal(t, tc.str, str)
```
**Перевіряє**: Чи коректно формуються рядки для дебагу, зокрема:
- ✅ Форматування UUID: `[16]byte` → `01020304-0506-0708-090a-0b0c0d0e0f10`
- ✅ Умовне виведення опціональних полів: `DefaultConstantIV*` тільки якщо увімкнені

---

## 🎯 Чому цей тест критичний для вашого HLS-процесора?

### 🔹 Сценарій 1: Отримання ліцензії для Widevine

```
📡 Ви передаєте fMP4 з DRM клієнту:
1. Клієнт читає pssh: SystemID=Widevine, Data=[protobuf...]
2. Надсилає PSSH-дані на ліцензійний сервер
3. Отримує ключ дешифрування для KID
4. Декодує контент та відтворює

❌ Без тесту: помилка в серіалізації SystemID → клієнт не розпізнає DRM-систему
   → помилка "unsupported DRM" → відео не відтворюється
```

### 🔹 Сценарій 2: Підтримка кількох ключів (версія 1)

```
🌐 Контент зашифрований двома ключами (регіон A + регіон B):
1. Ви генеруєте pssh версії 1 з KIDCount=2, KIDs=[keyA, keyB]
2. Клієнт з регіону A запитує ліцензію для keyA
3. Клієнт з регіону B запитує ліцензію для keyB
4. Обидва відтворюють контент

✅ Тест гарантує, що KIDs коректно серіалізуються/десеріалізуються
❌ Без тесту: KIDCount читається неправильно → клієнт запитує не той ключ
   → помилка "license not found" → контент заблоковано
```

### 🔹 Сценарій 3: Валідація параметрів шифрування

```
🔍 Ви перевіряєте, чи підтримає клієнт цей потік:
1. Читаєте tenc: IsProtected=1, IVSize=16, KID=uuid
2. Перевіряєте: чи підтримує клієнт AES-CTR з 16-байтним IV?
   • Safari/Chrome → так ✅
   • Старий плеєр → ні ❌ → конвертуєте в clear або інший режим
3. Якщо IVSize=1 (нестандарт) → логуєте попередження

✅ Тест гарантує, що бітові поля (Crypt/Skip, IsProtected/IVSize) читаються коректно
❌ Без тесту: 0xab читається як 0xba → Crypt=11, Skip=10 → неправильний патерн дешифрування
   → артефакти відео або краш декодера
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильна версія `pssh` | KIDs відсутні у версії 0 або присутні у версії 1 → помилка парсингу | Завжди перевіряйте `GetVersion()` перед доступом до `KIDCount`/`KIDs` |
| Неправильне форматування UUID | Плеєр не розпізнає SystemID/KID → помилка ліцензії | Використовуйте `uuid.UUID(bytes[:]).String()` для виводу, не форматируйте вручну |
| Ігнорування `opt=dynamic` у `tenc` | `DefaultConstantIV*` читаються навіть коли не потрібні → зсув даних | Завжди перевіряйте `IsOptFieldEnabled()` або покладайтеся на бібліотеку |
| Неправильна бітова упаковка Crypt/Skip | 0xab читається як 0xba → неправильний патерн дешифрування | Пам'ятайте: старші 4 біти = Crypt, молодші 4 = Skip |
| Невідповідність `DataSize` та реальної довжини `Data` | Обрізання або переповнення буфера → краш | Завжди встановлюйте `DataSize = int32(len(Data))` перед Marshal |

---

## 📋 Чекліст для вашого проекту

```
[ ] При роботі з `pssh`:
    • Перевіряйте версію: if pssh.GetVersion() == 1 { ... KIDs ... }
    • Форматуйте UUID через бібліотеку: uuid.UUID(sysID[:]).String()
    • Зберігайте `Data` без змін для ліцензійного запиту

[ ] При роботі з `tenc`:
    • Перевіряйте `DefaultIsProtected` перед спробою дешифрування
    • Використовуйте `DefaultPerSampleIVSize = 16` для максимальної сумісності
    • Уникайте `IVSize = 1` — може не підтримуватися плеєрами

[ ] Для генерації DRM-контенту:
    • Використовуйте офіційні бібліотеки для формування PSSH-даних (protobuf для Widevine, XML для PlayReady)
    • Завжди встановлюйте `KIDCount = len(KIDs)` для версії 1
    • Додавайте `pssh` на початку `moov` для швидкого доступу плеєра

[ ] Для дебагу:
    • Логуйте UUID у читабельному форматі: log.Printf("DRM: %s", uuid.UUID(sysID[:]).String())
    • Виводьте перші 32 байти `Data`: log.Printf("PSSH: % x", data[:32])
    • Використовуйте `Stringify()` для виводу `KIDs`: log.Printf("KIDs: %s", Stringify(pssh, ctx))

[ ] Для тестування:
    • Тестуйте з різними DRM-системами: Widevine (Android), FairPlay (iOS)
    • Перевіряйте відтворення на реальних пристроях: Chromecast, Apple TV, Smart TV
    • Симулюйте помилки ліцензії: неправильний KID, прострочена ліцензія
```

---

## 🎯 Висновок

> **Цей тест — ваш "страховий поліс" проти пошкодження DRM-параметрів**.  
> Він гарантує:
> • ✅ Коректну обробку версій `pssh` (0 vs 1) та опціональних полів
> • ✅ Правильне форматування UUID для SystemID та KIDs
> • ✅ Бітову упаковку Crypt/Skip та IsProtected/IVSize у `tenc`
> • ✅ Ідемпотентність серіалізації/десеріалізації (round-trip)
> • ✅ Динамічний парсинг через `UnmarshalAny`
> • ✅ Зручний дебаг через `Stringify` з людино-читабельними UUID

Для вашого **CCTV HLS Processor** це означає:
- 🔐 Захист преміум-контенту без ризику "зламати" DRM-бокси
- 🌐 Підтримка всіх основних платформ: Android (Widevine), iOS (FairPlay), Web (EME)
- 🔧 Гнучкість: легко додати нову DRM-систему через новий `SystemID`
- 📊 Моніторинг: логування UUID та параметрів для аналізу ліцензійних запитів

Потребуєте допомоги з інтеграцією конкретної DRM-системи (Widevine/PlayReady/FairPlay) або з генерацією валідних PSSH-даних? Напишіть — покажу готовий код! 🚀🔐