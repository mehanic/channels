# 🧪 Тест `TestBoxTypesVp`: VP Codec Configuration (`vpcC`)

Це **юніт-тест** для бібліотеки `go-mp4`, який перевіряє коректну роботу **серіалізації/десеріалізації** боксу `vpcC` (VP Codec Configuration) згідно зі специфікацією [VP9 in ISOBMFF](https://www.webmproject.org/vp9/mp4/).

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що параметри VP8/VP9-відео (профіль, рівень, бітова глибина, колір, ініціалізаційні дані) коректно перетворюються між структурою Go та 15-байтовим бінарним форматом** — критично для ініціалізації VP9-декодера у плеєрах.

---

## 📋 Структура тесту

```go
func TestBoxTypesVp(t *testing.T) {
    // 1. Масив тест-кейсів (тут 1 кейс "vpcC")
    testCases := []struct {
        name string           // Назва тесту
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

## 🔍 Детальний розбір тест-кейсу: "vpcC"

### ✅ Вхідні дані (`src`) — конфігурація VP9-декодера

```go
src: &VpcC{
    FullBox: FullBox{Version: 1},  // 🔹 Версія 1 (новіша специфікація)
    Profile:                     1,   // 🔹 Profile 1 (8-біт, підтримка 4:2:2/4:4:4)
    Level:                       50,  // 🔹 Level 5.0 (4K відео)
    BitDepth:                    10,  // 🔹 10-бітний колір (HDR)
    ChromaSubsampling:           3,   // 🔹 4:4:4 (без підвибірки)
    VideoFullRangeFlag:          1,   // 🔹 Full range (0-255)
    ColourPrimaries:             0,   // 🔹 Невідомі/за замовчуванням
    TransferCharacteristics:     1,   // 🔹 BT.709 (стандартна гамма)
    MatrixCoefficients:          10,  // 🔹 BT.2020 non-constant (для HDR)
    CodecInitializationDataSize: 3,   // 🔹 Розмір ініціалізаційних даних
    CodecInitializationData:     []byte{5, 4, 3},  // 🔹 Сирі байти (приклад)
},
```

**📊 Що це означає для вашого стріму:**

| Поле | Значення | Практичне значення |
|------|----------|-------------------|
| `Profile=1` | Profile 1 | 8-біт, підтримка 4:2:2/4:4:4 — для професійного відео |
| `Level=50` | Level 5.0 | Підтримка 4K роздільності |
| `BitDepth=10` | 10-біт | 🔹 HDR-контент, широкий динамічний діапазон |
| `ChromaSubsampling=3` | 4:4:4 | 🔹 Без підвибірки кольору — для графіки/тексту |
| `VideoFullRangeFlag=1` | Full range (0-255) | 🔹 Для ПК/ігор, не для ТВ |
| `MatrixCoefficients=10` | BT.2020 non-constant | 🔹 HDR колірний простір |

> 🎯 **Це конфігурація для професійного 4K HDR-відео з 4:4:4 кольором** — ідеально для скрінкастів, графіки, медичних зображень.

---

### ✅ Очікувані байти (`bin`) — 15 байт конфігурації

```go
bin: []byte{
    // 🔹 Заголовок FullBox (4 байти):
    0x01, 0x00, 0x00, 0x00,  // Version=1, Flags=0x000000
    
    // 🔹 Основні параметри (6 байт):
    0x01,                    // Profile = 1
    0x32,                    // Level = 50 (0x32)
    0xa7,                    // 🔹 Упаковані: BitDepth(4)+ChromaSubsampling(3)+FullRange(1)
    0x00,                    // ColourPrimaries = 0
    0x01,                    // TransferCharacteristics = 1
    0x0a,                    // MatrixCoefficients = 10 (0x0a)
    
    // 🔹 Ініціалізаційні дані (5 байт):
    0x00, 0x03,              // CodecInitializationDataSize = 3 (uint16)
    0x05, 0x04, 0x03,        // CodecInitializationData = [5, 4, 3]
},
```

**🔢 Детальна розбивка упаковки `0xa7`:**

```
📐 Формат: [BitDepth:4][ChromaSubsampling:3][VideoFullRangeFlag:1]

🔢 Для нашого кейсу:
• BitDepth = 10 = 0xa = 1010 (4 біти)
• ChromaSubsampling = 3 = 0x3 = 011 (3 біти)
• VideoFullRangeFlag = 1 = 1 (1 біт)

📦 Упаковка:
1010 011 1 = 1010 0111 = 0xa7 ✅

🎯 Результат: байт 0xa7 містить три параметри в одному байті!
```

---

### ✅ Очікуваний рядок для дебагу (`str`)

```go
str: `Version=1 Flags=0x000000 ` +
     `Profile=0x1 Level=0x32 BitDepth=0xa ChromaSubsampling=0x3 VideoFullRangeFlag=0x1 ` +
     `ColourPrimaries=0x0 TransferCharacteristics=0x1 MatrixCoefficients=0xa ` +
     `CodecInitializationDataSize=3 CodecInitializationData=[0x5, 0x4, 0x3]`,
```

**Навіщо це?**
- 🪲 **Дебаг**: Замість дампів байтів бачите `BitDepth=0xa` (10-біт)
- 🧪 **Тести**: `assert.Equal(t, expected, actual)` для швидкого виявлення помилок
- 📋 **Логи**: `log.Printf("🎬 %s", Stringify(vpcc, ctx))` у продакшені

---

## 🔄 Чотири операції, що тестуються

### 🔹 1. `Marshal` — серіалізація (структура → байти)

```go
buf := bytes.NewBuffer(nil)
n, err := Marshal(buf, tc.src, tc.ctx)
require.NoError(t, err)
assert.Equal(t, uint64(len(tc.bin)), n)  // має бути рівно 15 байт!
assert.Equal(t, tc.bin, buf.Bytes())      // байт в байт!
```

**Що перевіряємо:**
```
📦 Вхід: VpcC{Profile:1, Level:50, BitDepth:10, ChromaSubsampling:3, ...}
              │
              ▼
         Marshal() + бітова упаковка (BitDepth+Chroma+FullRange в 1 байт)
              │
              ▼
📤 Вихід: []byte{0x01,0x00,0x00,0x00, 0x01,0x32,0xa7,0x00,0x01,0x0a, 0x00,0x03, 0x05,0x04,0x03}
              │
              ▼
✅ Порівнюємо з еталонним масивом — кожен біт на своєму місці
```

---

### 🔹 2. `Unmarshal` — десеріалізація (байти → структура)

```go
r := bytes.NewReader(tc.bin)
n, err = Unmarshal(r, uint64(len(tc.bin)), tc.dst, tc.ctx)
require.NoError(t, err)
assert.Equal(t, uint64(buf.Len()), n)  // прочитано рівно 15 байт
assert.Equal(t, tc.src, tc.dst)         // 🔁 round-trip: структура відновлена точно!
```

**🔁 Round-trip тест — найважливіша перевірка:**
```
Структура → 15 байт → Структура'

Якщо Структура == Структура' → ✅ серіалізація ідемпотентна
Якщо ні → ❌ втрата даних → декодер отримає хибні параметри → відео "зламається"
```

---

### 🔹 3. `UnmarshalAny` — динамічний парсинг за типом

```go
dst, n, err := UnmarshalAny(
    bytes.NewReader(tc.bin), 
    tc.src.GetType(),        // BoxTypeVpcC() = "vpcC"
    uint64(len(tc.bin)), 
    tc.ctx,
)
require.NoError(t, err)
assert.Equal(t, tc.src, dst)
```

**Навіщо `UnmarshalAny`?**
```
🔍 Сценарій: Ви читаєте файл і не знаєте наперед тип боксу

1. ReadBoxInfo() → тип="vpcC", size=15
2. UnmarshalAny(r, "vpcC", 15, ctx) 
3. Бібліотека сама:
   • Знаходить зареєстровану структуру для "vpcC" (VpcC{})
   • Парсить FullBox заголовок
   • Розпаковує упакований байт 0xa7 у три окремих поля
   • Читає ініціалізаційні дані з динамічною довжиною
   • Повертає готовий об'єкт

✅ Ви не пишете 50+ `case "vpcC":` у своєму коді!
```

---

### 🔹 4. `Stringify` — людський формат для дебагу

```go
str, err := Stringify(tc.src, tc.ctx)
require.NoError(t, err)
assert.Equal(t, tc.str, str)
```

**Приклад використання в логах вашого процесора:**
```go
// ❌ Погано: незрозумілий дамп байтів
log.Printf("vpcC: % x", vpccBytes)  // [01 00 00 00 01 32 a7...]

// ✅ Добре: зрозумілі параметри
log.Printf("🎬 %s", Stringify(vpcc, ctx))
// 🎬 Version=1 Flags=0x000000 Profile=0x1 Level=0x32 BitDepth=0xa ChromaSubsampling=0x3 ...

// 🔥 Ще краще: людино-читабельна інтерпретація
if vpcc.BitDepth == 10 && vpcc.MatrixCoefficients == 10 {
    log.Printf("🎨 HDR content detected (10-bit, BT.2020)")
}
```

---

## 🛠️ Додаткові перевірки: позиція читача

```go
// Після Unmarshal: перевіряємо, що курсор в кінці прочитаних даних
s, err := r.Seek(0, io.SeekCurrent)
require.NoError(t, err)
assert.Equal(t, int64(buf.Len()), s)  // прочитано рівно 15 байт
```

**Чому це критично?**
```
📦 Файл: [moof][traf][vpcC:15B][mdat]
                    │
                    ▼
              Парсимо "vpcC" (рівно 15 байт)
                    │
                    ▼
❌ Якщо Unmarshal прочитає 16 байт → "mdat" зсунутий → відео пошкоджене
✅ Якщо курсор точно в кінці "vpcC" → "mdat" читається коректно
```

---

## 🎯 Чому цей тест критичний для вашого HLS-процесора?

### 🔹 Сценарій 1: Підтримка HDR-відео

```
📡 Ви додаєте 4K HDR-відео до HLS-стріму:
1. Формуєте vpcC: BitDepth=10, MatrixCoefficients=10 (BT.2020)
2. Записуєте у fMP4-сегмент
3. Плеєр з підтримкою HDR (Safari, Chrome) читає параметри
4. Відтворює відео з правильним кольоровим простором

❌ Без тесту: помилка в упаковці 0xa7 → BitDepth читається як 8
   → плеєр відтворює 8-біт замість 10-біт → кольори "вимилені", немає HDR
```

### 🔹 Сценарій 2: Економія бітрейту без втрати якості

```
📉 Ви хочете оптимізувати стрімінг для мобільних:
1. Змінюєте Level з 50 (4K) → 12 (1080p) у vpcC
2. Записуєте оновлений бокс у новий сегмент
3. Клієнт отримує легший потік, але якість залишається високою

✅ Тест гарантує, що бітова упаковка коректна
❌ Без тесту: Level=50 читається як 12 → плеєр очікує 1080p, отримує 4K
   → буферизація або помилка декодування
```

### 🔹 Сценарій 3: Ініціалізація декодера

```
🔑 Ви передаєте ініціалізаційні дані для VP9:
1. Формуєте vpcC з CodecInitializationData = [Sequence Header байти]
2. Записуєте у fMP4
3. Плеєр читає ці дані для ініціалізації декодера
4. Відео починає відтворюватися без затримки

✅ Тест гарантує, що динамічні дані читаються з правильною довжиною
❌ Без тесту: CodecInitializationDataSize=3 читається як 4
   → декодер читає зайвий байт → помилка ініціалізації → чорний екран
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильна упаковка `BitDepth`/`ChromaSubsampling`/`FullRange` | Поля читаються неправильно → кольори/якість пошкоджені | Пам'ятайте порядок: [BitDepth:4][Chroma:3][FullRange:1] |
| Неправильний `CodecInitializationDataSize` | Обрізання або переповнення буфера → краш декодера | Завжди встановлюйте `Size = len(Data)` перед Marshal |
| Ігнорування `len=dynamic` для `CodecInitializationData` | Читаєте не ту кількість байт → зсув даних | Завжди реалізуйте `GetFieldLength()` для масивів |
| Невідповідність `Profile` та `BitDepth` | Декодер не підтримує комбінацію → помилка | Профіль 0/1 → 8-біт, Профіль 2/3 → 10/12-біт |
| Неправильний `MatrixCoefficients` для HDR | Кольори відображаються неправильно | Для HDR: `MatrixCoefficients=9` (BT.2020 constant) або `10` (non-constant) |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні VP9-відео:
    • Шукайте `vpcC` бокс всередині `vp09` у `stsd`
    • Перевіряйте `Profile <= 1` для сумісності з вебом
    • Логувайте `BitDepth` та `MatrixCoefficients` для HDR-детекції

[ ] Для сумісності з вебом:
    • Profile 0 + 8-біт + 4:2:0 підтримується всюди
    • Рівень 4.0-4.2 (10-12) — оптимальний для 1080p
    • BT.709 колірний простір (всі три поля = 1) — стандарт для вебу

[ ] При генерації нових сегментів:
    • Отримуйте `CodecInitializationData` з енкодера (не генеруйте вручну!)
    • Встановлюйте `CodecInitializationDataSize = len(CodecInitializationData)`
    • Узгоджуйте `BitDepth` з реальною глибиною у відео-потоці

[ ] Для дебагу:
    • Логуйте конфігурацію: log.Printf("🎬 VP9: Profile=%d, Level=%d, %d-bit", ...)
    • Перевіряйте ініціалізаційні дані: log.Printf("🔥 InitData: % x", initData[:16])
    • Використовуйте `Stringify()` для людського виводу

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: Chrome (VP9), Firefox, VLC
    • Перевірте відтворення з різними профілями: Profile 0, Profile 2 (HDR)
```

---

## 🎯 Висновок

> **Цей тест — ваш "страховий поліс" проти пошкодження VP9-конфігурації**.  
> Він гарантує:
> • ✅ Коректну бітову упаковку трьох полів в одному байті (`BitDepth`+`ChromaSubsampling`+`FullRange`)
> • ✅ Правильну обробку динамічних даних ініціалізації
> • ✅ Ідемпотентність серіалізації/десеріалізації (round-trip)
> • ✅ Динамічний парсинг через `UnmarshalAny`
> • ✅ Зручний дебаг через `Stringify` з людино-читабельними значеннями

Для вашого **CCTV HLS Processor** це означає:
- 🎥 Високоякісне відео з правильною передачею кольору (особливо для HDR)
- ⚡ Економія бітрейту завдяки точному опису параметрів кодека
- 🔄 Гнучкість: підтримка як SDR, так і HDR контенту
- 🌐 Сумісність з сучасними браузерами та плеєрами (Chrome, Firefox, Safari)

Потребуєте допомоги з інтеграцією VP9 у ваш конвеєр або з валідацією `CodecInitializationData`? Напишіть — покажу готовий код! 🚀🎬