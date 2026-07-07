# 🧪 Тест `TestBoxTypesAV1`: Повний розбір

Це **інтеграційний тест** для бібліотеки `go-mp4`, який перевіряє коректну роботу **серіалізації/десеріалізації** боксу `Av1C` (AV1 Codec Configuration).

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що параметри AV1-кодека (профіль, рівень, кольорова підвибірка, конфігураційні дані) коректно перетворюються між структурою Go та бінарним форматом MP4** — і назад.

---

## 📋 Структура тесту

```go
func TestBoxTypesAV1(t *testing.T) {
    // 1. Масив тест-кейсів
    testCases := []struct {
        name string           // Назва тесту
        src  IImmutableBox    // Вихідна структура (для запису)
        dst  IBox             // Порожня структура (для читання)
        bin  []byte           // Очікувані байти у файлі
        str  string           // Очікуваний рядок для дебагу (Stringify)
        ctx  Context          // Контекст парсингу
    }{ ... }

    // 2. Запуск кожного кейсу
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // Тестова логіка: Marshal → Unmarshal → UnmarshalAny → Stringify
        })
    }
}
```

---

## 🔍 Детальний розбір тест-кейсу: "Av1C"

### ✅ Вхідні дані (`src`) — параметри кодека

```go
src: &Av1C{
    Marker:               1,           // ✅ завжди 1
    Version:              1,           // ✅ версія формату
    SeqProfile:           2,           // Professional profile
    SeqLevelIdx0:         1,           // Level 2.1
    SeqTier0:             1,           // High tier
    HighBitdepth:         1,           // 10/12-біт колір
    TwelveBit:            0,           // 10-біт (не 12)
    Monochrome:           0,           // кольорове відео
    ChromaSubsamplingX:   1,           // горизонтальна підвибірка
    ChromaSubsamplingY:   1,           // вертикальна підвибірка → 4:2:0
    ChromaSamplePosition: 0,           // Vertical (BT.601)
    ConfigOBUs: []byte{                // сирі OBU-дані
        0x08, 0x00, 0x00, 0x00, 0x42, 0xa7, 0xbf, 0xe4,
        0x60, 0x0d, 0x00, 0x40,
    },
},
```

**📊 Що означають ці параметри для вашого стріму:**

| Поле | Значення | Практичне значення |
|------|----------|-------------------|
| `SeqProfile=2` | Professional | Підтримка 4:2:2/4:4:4, 10/12-біт — для професійних камер |
| `SeqLevelIdx0=1` | Level 2.1 | Макс. 720p @ 30fps, ~3 Mbps — для мобільних мереж |
| `SeqTier0=1` | High tier | Вищий бітрейт для кращої якості |
| `HighBitdepth=1, TwelveBit=0` | 10-біт | Кращий динамічний діапазон (HDR-подібний) |
| `ChromaSubsampling=[1,1]` | 4:2:0 | Оптимально для стрімінгу (економія бітрейту) |
| `ChromaSamplePosition=0` | BT.601 | Стандарт для SD/старих систем |

> 🎯 **Висновок**: Це конфігурація для **професійної камери з 10-бітним 4:2:0 відео**, оптимізованої для мобільної передачі.

---

### ✅ Очікувані байти (`bin`) — бінарний формат

```go
bin: []byte{
    0x81, 0x41, 0xcc, 0x00,  // заголовок (4 байти)
    0x08, 0x00, 0x00, 0x00, 0x42, 0xa7, 0xbf, 0xe4, 0x60, 0x0d, 0x00, 0x40,  // ConfigOBUs
},
```

**🔢 Детальна розбивка заголовка (4 байти):**

```
📦 Байт 0: 0x81 = 1000 0001
   ┌─────┬───────────────┐
   │ 1   │ 000 0001      │
   │Marker│ Version=1   │
   │(1 біт)│ (7 біт)    │
   └─────┴───────────────┘
   ✅ Marker=1, Version=1

📦 Байт 1: 0x41 = 0100 0001
   ┌─────────┬─────────┐
   │ 010     │ 0 0001  │
   │Profile=2│ Level=1 │
   │(3 біти) │(5 біт)  │
   └─────────┴─────────┘
   ✅ SeqProfile=2, SeqLevelIdx0=1

📦 Байт 2: 0xcc = 1100 1100
   ┌──┬──┬──┬──┬──┬──┬────┐
   │1 │1 │0 │0 │1 │1 │00  │
   │T │H │12│M │X │Y │Pos │
   └──┴──┴──┴──┴──┴──┴────┘
   Tier=1, HighBit=1, 12Bit=0, Mono=0, SubX=1, SubY=1, Pos=0
   ✅ Всі прапорці співпадають!

📦 Байт 3: 0x00 = 0000 0000
   ┌─────┬──┬────┐
   │000  │0 │0000│
   │Res. │DP│Delay-1│
   └─────┴──┴────┘
   Reserved=0, DelayPresent=0, Delay=0
   ✅ InitialPresentationDelay не вказано
```

> 🎯 **Магія бібліотеки**: Ви працюєте з окремими полями (`SeqProfile`, `HighBitdepth`), а бібліотека сама пакує їх у біти за тегами `mp4:"...,size=N"`.

---

### ✅ Очікуваний рядок для дебагу (`str`)

```go
str: `SeqProfile=0x2 SeqLevelIdx0=0x1 SeqTier0=0x1 HighBitdepth=0x1 
      TwelveBit=0x0 Monochrome=0x0 ChromaSubsamplingX=0x1 ChromaSubsamplingY=0x1 
      ChromaSamplePosition=0x0 InitialPresentationDelayPresent=0x0 
      InitialPresentationDelayMinusOne=0x0 
      ConfigOBUs=[0x8, 0x0, 0x0, 0x0, 0x42, 0xa7, 0xbf, 0xe4, 0x60, 0xd, 0x0, 0x40]`,
```

**Навіщо це?**
- 🪲 Дебаг парсингу: замість дампів байтів бачите зрозумілі назви полів
- 🧪 Порівняння в тестах: `assert.Equal(t, expected, actual)`
- 📋 Логування: `log.Printf("🎬 %s", Stringify(av1c, ctx))`

---

## 🔄 Чотири операції, що тестуються

### 🔹 1. `Marshal` — серіалізація (структура → байти)

```go
buf := bytes.NewBuffer(nil)
n, err := Marshal(buf, tc.src, tc.ctx)
require.NoError(t, err)
assert.Equal(t, uint64(len(tc.bin)), n)  // перевірка розміру
assert.Equal(t, tc.bin, buf.Bytes())      // перевірка вмісту
```

**Що перевіряємо:**
```
📦 Вхід: Av1C{SeqProfile:2, Level:1, HighBit:1, ...}
              │
              ▼
         Marshal() + бітова упаковка
              │
              ▼
📤 Вихід: []byte{0x81, 0x41, 0xcc, 0x00, ...}
              │
              ▼
✅ Порівнюємо з очікуваним масивом — байт в байт!
```

---

### 🔹 2. `Unmarshal` — десеріалізація (байти → структура)

```go
r := bytes.NewReader(tc.bin)
n, err = Unmarshal(r, uint64(len(tc.bin)), tc.dst, tc.ctx)
require.NoError(t, err)
assert.Equal(t, uint64(buf.Len()), n)  // прочитано стільки ж, скільки записано
assert.Equal(t, tc.src, tc.dst)         // структура відновлена точно!
```

**🔁 Round-trip тест:**
```
Структура → байти → Структура'

Якщо Структура == Структура' → ✅ серіалізація ідемпотентна
Якщо ні → ❌ втрата даних або помилка вирівнювання
```

---

### 🔹 3. `UnmarshalAny` — динамічний парсинг за типом

```go
dst, n, err := UnmarshalAny(
    bytes.NewReader(tc.bin), 
    tc.src.GetType(),        // BoxTypeAv1C() = "av1C"
    uint64(len(tc.bin)), 
    tc.ctx,
)
require.NoError(t, err)
assert.Equal(t, tc.src, dst)
```

**Навіщо `UnmarshalAny`?**
```
🔍 Сценарій: Ви читаєте файл і не знаєте наперед тип боксу

1. ReadBoxInfo() → тип="av1C", size=16
2. UnmarshalAny(r, "av1C", size, ctx) 
3. Бібліотека сама:
   • Знаходить зареєстровану структуру для "av1C" (Av1C{})
   • Створює її екземпляр
   • Парсить дані з бітовою упаковкою
   • Повертає готовий об'єкт

✅ Ви не пишете switch/case для 100+ типів боксів!
```

---

### 🔹 4. `Stringify` — людський формат для дебагу

```go
str, err := Stringify(tc.src, tc.ctx)
require.NoError(t, err)
assert.Equal(t, tc.str, str)
```

**Приклад використання в логах:**
```go
// Замість:
log.Printf("av1C: % x", av1cBytes)  // [81 41 cc 00 08 00...]

// Краще:
log.Printf("🎬 %s", Stringify(av1c, ctx))
// 🎬 SeqProfile=0x2 SeqLevelIdx0=0x1 HighBitdepth=0x1 ...
```

---

## 🛠️ Додаткові перевірки: позиція читача

```go
// Після Unmarshal: перевіряємо, що курсор в кінці прочитаних даних
s, err := r.Seek(0, io.SeekCurrent)
require.NoError(t, err)
assert.Equal(t, int64(buf.Len()), s)  // прочитано рівно n байт
```

**Чому це важливо?**
```
📦 Файл: [moof][traf][av1C][trun][mdat]
                    │
                    ▼
              Парсимо "av1C" (16 байт)
                    │
                    ▼
❌ Якщо Unmarshal прочитає 17 байт → "trun" пошкоджений
✅ Якщо курсор точно в кінці "av1C" → "trun" читається коректно
```

---

## 🎯 Чому цей тест критичний для вашого HLS-процесора?

### 🔹 Сценарій 1: Валідація вхідного AV1-стріму

```
📡 Камера надсилає fMP4 з AV1:
1. Unmarshal читає av1C бокс
2. Ви перевіряєте: 
   • SeqProfile <= 1? (для сумісності з вебом)
   • SeqLevelIdx0 <= 23? (для 1080p у браузері)
   • ConfigOBUs містить Sequence Header?
3. Якщо ні — відхиляєте або транскодуєте

❌ Без тесту: помилка в бітовій упаковці → неправильна валідація
```

### 🔹 Сценарій 2: Генерація HLS-плейлиста з кодеками

```
📝 Ви формуєте .m3u8 для клієнта:
#EXT-X-STREAM-INF:CODECS="av01.0.05M.08"

Де "av01.0.05M.08" кодується з:
• 0 = SeqProfile (Main)
• 05 = SeqLevelIdx0 (Level 2.1)
• M = SeqTier0 (Main/High)
• 08 = бітова глибина + підвибірка

✅ Тест гарантує, що Marshal/Unmarshal коректно зберігають ці параметри
❌ Без тесту: неправильний CODECS → плеєр відмовляє відтворити
```

### 🔹 Сценарій 3: Транскодування/репакування на льоту

```
🔄 Ви змінюєте параметри стріму:
1. Прочитали av1C з вхідного потоку
2. Змінили SeqLevelIdx0 з 1 → 16 (для кращої якості)
3. Записали оновлений av1C у новий сегмент

✅ Тест гарантує ідемпотентність: зміни не ламають формат
❌ Без тесту: пошкоджений заголовок → декодер падає
```

---

## 🧪 Як додати свій тест-кейс?

```go
// Додайте у масив testCases новий елемент:
{
    name: "Av1C Main Profile 1080p",
    src: &Av1C{
        Marker:               1,
        Version:              1,
        SeqProfile:           0,           // Main profile
        SeqLevelIdx0:         16,          // Level 4.0 (1080p)
        SeqTier0:             0,           // Main tier
        HighBitdepth:         0,           // 8-біт
        TwelveBit:            0,
        Monochrome:           0,
        ChromaSubsamplingX:   1,           // 4:2:0
        ChromaSubsamplingY:   1,
        ChromaSamplePosition: 1,           // BT.709
        ConfigOBUs: []byte{0x08, 0x00, 0x00, 0x00, 0x12, 0x34}, // приклад
    },
    dst: &Av1C{},
    // Очікувані байти (розраховуються бібліотекою):
    bin: []byte{0x81, 0x10, 0x44, 0x00, 0x08, 0x00, 0x00, 0x00, 0x12, 0x34},
    str: `SeqProfile=0x0 SeqLevelIdx0=0x10 SeqTier0=0x0 ...`,
    ctx: Context{},
},
```

**Запустіть тест**:
```bash
go test -v ./mp4 -run TestBoxTypesAV1/Av1C_Main_Profile_1080p
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний порядок полів у структурі | Байти "з'їжджають", парсинг ламається | Дотримуйтесь порядку `mp4:"0", mp4:"1", ...` |
| Забути `const=1` для Marker/Version | Запис невалідного заголовка → плеєр відмовляє | Завжди встановлюйте `Marker=1, Version=1` |
| Неправильне кодування прапорців | `HighBitdepth=1` читається як `0` | Перевіряйте бітові маски: `0xcc = 1100 1100` |
| Порожній `ConfigOBUs` | Декодер не ініціалізується → чорний екран | Переконайтеся, що є OBU_SEQUENCE_HEADER |
| Ігнорування `ChromaSamplePosition` | Кольори зі зсувом у плеєрі | Використовуйте `1` для HD (BT.709) |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні AV1-сегмента:
    • Витягніть av1C через ReadBoxStructure + UnmarshalAny
    • Перевірте Marker=1, Version=1
    • Залогуйте профіль/рівень: log.Printf("🎬 %s", Stringify(av1c, ctx))

[ ] Для сумісності з вебом:
    • Відхиляйте профілі >1 (High/Professional)
    • Обмежте рівень <=23 для 1080p у браузері
    • Переконайтеся, що 4:2:0 (ChromaSubsamplingX=Y=1)

[ ] При генерації HLS-плейлиста:
    • Кодуйте CODECS="av01.P.L.T" з параметрів av1C:
      P = SeqProfile, L = SeqLevelIdx0, T = SeqTier0+Bitdepth
    • Приклад: av01.0.16M.08 для Main/Level4/Main/8-біт

[ ] Для дебагу:
    • Логуйте перші байти ConfigOBUs: 
      log.Printf("📦 OBU type: 0x%02x", av1c.ConfigOBUs[0] & 0x3F)
    • Перевіряйте наявність Sequence Header (type=1)

[ ] Для тестування:
    • Напишіть round-trip тест для ваших параметрів
    • Протестуйте на реальних плеєрах: VLC, hls.js, ExoPlayer
    • Перевірте відтворення на слабких пристроях
```

---

## 🎯 Висновок

> **Цей тест — ваш "золотий стандарт" для роботи з AV1 у MP4**.  
> Він гарантує:
> • ✅ Коректну бітову упаковку 8+ полів у 4 байти заголовка
> • ✅ Ідемпотентність серіалізації/десеріалізації
> • ✅ Динамічний парсинг через `UnmarshalAny`
> • ✅ Зручний дебаг через `Stringify`

Для вашого **CCTV HLS Processor** це означає:
- 🎥 Клієнти коректно декодують AV1 без артефактів
- 🌐 Підтримка сучасних браузерів з правильним `CODECS=`
- 📉 Економія бітрейту на 30-50% порівняно з H.264
- 🔧 Безпечна модифікація параметрів на льоту

Потребуєте допомоги з кодуванням `CODECS=` для HLS-плейлиста або з валідацією `ConfigOBUs`? Напишіть — покажу готовий код! 🚀🎬