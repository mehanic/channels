# 🧪 Тест `TestBoxTypesISO14496_14`: Повний розбір `esds` боксу

Це **юніт-тест** для бібліотеки `go-mp4`, який перевіряє коректну роботу **серіалізації/десеріалізації** боксу `esds` (Elementary Stream Descriptor) для MPEG-4 кодеків (AAC, AVC тощо).

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що складна ієрархія дескрипторів MPEG-4 (з varint-розмірами, опціональними полями, вкладеними структурами) коректно перетворюється між структурою Go та бінарним форматом** — критично для ініціалізації AAC/AVC декодерів.

---

## 📋 Структура тесту

```go
func TestBoxTypesISO14496_14(t *testing.T) {
    // 1. Масив тест-кейсів (тут 1 кейс "esds" з 5 дескрипторами)
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
            // Тестуємо 4 операції: Marshal → Unmarshal → UnmarshalAny → Stringify
        })
    }
}
```

---

## 🔍 Детальний розбір тест-кейсу: "esds"

### ✅ Вхідні дані (`src`) — 5 дескрипторів різних типів

```go
src: &Esds{
    FullBox: FullBox{Version: 0, Flags: [3]byte{0,0,0}},
    Descriptors: []Descriptor{
        // 🔹 1. ESDescriptor (Tag=0x03) з прапорцями StreamDependence + OcrStream
        {
            Tag:  ESDescrTag,  // 0x03
            Size: 0x1234567,   // 🔹 varint-розмір!
            ESDescriptor: &ESDescriptor{
                ESID:                 0x1234,
                StreamDependenceFlag: true,   // 🔹 увімкнено → є DependsOnESID
                UrlFlag:              false,   // 🔹 вимкнено → немає URL
                OcrStreamFlag:        true,    // 🔹 увімкнено → є OCRESID
                StreamPriority:       0x03,
                DependsOnESID:        0x2345,  // 🔹 читається тільки якщо StreamDependenceFlag=true
                OCRESID:              0x3456,  // 🔹 читається тільки якщо OcrStreamFlag=true
            },
        },
        
        // 🔹 2. ESDescriptor (Tag=0x03) з прапорцем UrlFlag
        {
            Tag:  ESDescrTag,
            Size: 0x1234567,
            ESDescriptor: &ESDescriptor{
                ESID:                 0x1234,
                StreamDependenceFlag: false,
                UrlFlag:              true,    // 🔹 увімкнено → є URLLength + URLString
                OcrStreamFlag:        false,
                StreamPriority:       0x03,
                URLLength:            11,      // 🔹 довжина рядка "http://hoge"
                URLString:            []byte("http://hoge"),
            },
        },
        
        // 🔹 3. DecoderConfigDescriptor (Tag=0x04) — конфігурація декодера ⭐
        {
            Tag:  DecoderConfigDescrTag,  // 0x04
            Size: 0x1234567,
            DecoderConfigDescriptor: &DecoderConfigDescriptor{
                ObjectTypeIndication: 0x12,  // 🔹 тип кодека (не AAC, тестовий)
                StreamType:           0x15,  // 🔹 тип потоку
                UpStream:             true,
                BufferSizeDB:         0x123456,  // 🔹 24-бітне поле!
                MaxBitrate:           0x12345678,
                AvgBitrate:           0x23456789,
            },
        },
        
        // 🔹 4. DecSpecificInfo (Tag=0x05) — сирі дані кодека 🔥
        {
            Tag:  DecSpecificInfoTag,  // 0x05
            Size: 0x03,
            Data: []byte{0x11, 0x22, 0x33},  // 🔥 AudioSpecificConfig для AAC
        },
        
        // 🔹 5. SLConfigDescriptor (Tag=0x06) — синхронізація
        {
            Tag:  SLConfigDescrTag,  // 0x06
            Size: 0x05,
            Data: []byte{0x11, 0x22, 0x33, 0x44, 0x55},
        },
    },
},
```

**📊 Що це означає для вашого стріму:**

| Дескриптор | Призначення | Практичне значення |
|------------|-------------|-------------------|
| `ESDescriptor` (0x03) | Опис потоку | Ідентифікатор, пріоритет, залежності між потоками |
| `DecoderConfigDescriptor` (0x04) | Параметри декодера | Тип кодека, бітрейт, буфер — ключ до ініціалізації |
| `DecSpecificInfo` (0x05) | Сирі дані кодека | 🔥 AudioSpecificConfig для AAC — без цього декодер не працює! |
| `SLConfigDescriptor` (0x06) | Синхронізація шару | Параметри таймінгу для MPEG-4 SL |

---

### ✅ Очікувані байти (`bin`) — бітова упаковка з varint

```go
bin: []byte{
    // 🔹 Заголовок FullBox (4 байти)
    0,                // version
    0x00, 0x00, 0x00, // flags
    
    // 🔹 Дескриптор 1: ESDescriptor (Tag=0x03) з прапорцями
    0x03,                   // tag
    0x89, 0x8d, 0x8a, 0x67, // 🔹 size як varint: 0x1234567
    0x12, 0x34,             // ESID
    0xa3,                   // 🔹 прапорці + priority: [1][0][1][00011] = 0xa3
    0x23, 0x45,             // DependsOnESID (бо StreamDependenceFlag=1)
    0x34, 0x56,             // OCRESID (бо OcrStreamFlag=1)
    
    // 🔹 Дескриптор 2: ESDescriptor (Tag=0x03) з URL
    0x03,                   // tag
    0x89, 0x8d, 0x8a, 0x67, // size (varint)
    0x12, 0x34,             // ESID
    0x43,                   // прапорці + priority: [0][1][0][00011] = 0x43
    11,                     // URLLength
    'h','t','t','p',':','/','/','h','o','g','e',  // URLString
    
    // 🔹 Дескриптор 3: DecoderConfigDescriptor (Tag=0x04)
    0x04,                   // tag
    0x89, 0x8d, 0x8a, 0x67, // size (varint)
    0x12,                   // ObjectTypeIndication
    0x56,                   // 🔹 StreamType(6) + UpStream(1) + Reserved(1) = 0x56
    0x12, 0x34, 0x56,       // 🔹 BufferSizeDB: 24 біти = 3 байти!
    0x12, 0x34, 0x56, 0x78, // MaxBitrate (32 біти)
    0x23, 0x45, 0x67, 0x89, // AvgBitrate (32 біти)
    
    // 🔹 Дескриптор 4: DecSpecificInfo (Tag=0x05) — сирі дані
    0x05,                   // tag
    0x80, 0x80, 0x80, 0x03, // 🔹 size=3 як varint: 4 байти!
    0x11, 0x22, 0x33,       // Data: AudioSpecificConfig байти
    
    // 🔹 Дескриптор 5: SLConfigDescriptor (Tag=0x06)
    0x06,                   // tag
    0x80, 0x80, 0x80, 0x05, // 🔹 size=5 як varint: 4 байти!
    0x11, 0x22, 0x33, 0x44, 0x55,  // Data
},
```

**🔢 Детальна розбивка ключових моментів:**

#### 🔹 Varint-кодування розміру: `0x1234567` → `[0x89, 0x8d, 0x8a, 0x67]`

```
📐 MPEG-4 varint формат:
• Кожен байт: [продовження:1][дані:7]
• Біт 7 = 1 → читаємо наступний байт
• Біт 7 = 0 → останній байт

🔢 Розбивка 0x1234567 (19088743 десяткове):
Бінарно: 0001 0010 0011 0100 0101 0110 0111

Групуємо по 7 біт (з кінця):
• 0000111 = 0x07 → байт 3: 0x07 (біт 7=0 → кінець)
• 0010110 = 0x16 → байт 2: 0x80|0x16 = 0x96? Ні, чекайте...

🔄 Правильне кодування (з початку):
0x1234567 = 0001 0010 0011 0100 0101 0110 0111

Байт 0: [1][0001001] = 1000 1001 = 0x89  ← продовжуємо
Байт 1: [1][0001101] = 1000 1101 = 0x8d  ← продовжуємо
Байт 2: [1][0001010] = 1000 1010 = 0x8a  ← продовжуємо
Байт 3: [0][1100111] = 0110 0111 = 0x67  ← кінець!

✅ Результат: [0x89, 0x8d, 0x8a, 0x67] — саме те, що в тесті!
```

#### 🔹 Прапорці в `ESDescriptor`: `0xa3` = `1010 0011`

```
📐 Формат байта прапорців:
[StreamDependence:1][UrlFlag:1][OcrStream:1][Priority:5]

🔢 Для першого дескриптора:
StreamDependenceFlag = true  → 1
UrlFlag              = false → 0
OcrStreamFlag        = true  → 1
StreamPriority       = 0x03  → 00011

Разом: 1 0 1 00011 = 1010 0011 = 0xa3 ✅
```

#### 🔹 24-бітне поле `BufferSizeDB`: `0x123456` → 3 байти

```
📐 Стандартне поле: 32 біти = 4 байти
📐 MPEG-4 оптимізація: 24 біти = 3 байти для економії місця

🔢 0x123456 у байтах:
[0x12] [0x34] [0x56]  ← саме так записано в тесті!
```

---

### ✅ Очікуваний рядок для дебагу (`str`)

```go
str: `Version=0 Flags=0x000000 Descriptors=[` +
    `{Tag=ESDescr Size=19088743 ESID=4660 StreamDependenceFlag=true ...}, ` +
    `{Tag=ESDescr Size=19088743 ESID=4660 UrlFlag=true URLString="http://hoge"}, ` +
    `{Tag=DecoderConfigDescr Size=19088743 ObjectTypeIndication=0x12 ...}, ` +
    "{Tag=DecSpecificInfo Size=3 Data=[0x11, 0x22, 0x33]}, " +
    "{Tag=SLConfigDescr Size=5 Data=[0x11, 0x22, 0x33, 0x44, 0x55]}]",
```

**Навіщо це?**
- 🪲 **Дебаг**: Замість дампів байтів бачите `Tag=DecSpecificInfo Data=[0x11, 0x22, 0x33]`
- 🧪 **Тести**: `assert.Equal(t, expected, actual)` для швидкого виявлення помилок
- 📋 **Логи**: `log.Printf("🎵 %s", Stringify(esds, ctx))` у продакшені

---

## 🔄 Чотири операції, що тестуються

### 🔹 1. `Marshal` — серіалізація (структура → байти)

```go
buf := bytes.NewBuffer(nil)
n, err := Marshal(buf, tc.src, tc.ctx)
require.NoError(t, err)
assert.Equal(t, uint64(len(tc.bin)), n)  // перевірка розміру
assert.Equal(t, tc.bin, buf.Bytes())      // перевірка вмісту: байт в байт!
```

**Що перевіряємо:**
```
📦 Вхід: Esds{Descriptors: [ESDescr, DecoderConfig, DecSpecificInfo...]}
              │
              ▼
         Marshal() + varint-кодування + бітова упаковка прапорців
              │
              ▼
📤 Вихід: []byte{0, 0x00,0x00,0x00, 0x03, 0x89,0x8d,0x8a,0x67, ...}
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
assert.Equal(t, uint64(buf.Len()), n)  // прочитано стільки ж, скільки записано
assert.Equal(t, tc.src, tc.dst)         // 🔁 round-trip: структура відновлена точно!
```

**🔁 Round-trip тест — найважливіша перевірка:**
```
Структура → байти → Структура'

Якщо Структура == Структура' → ✅ серіалізація ідемпотентна
Якщо ні → ❌ втрата даних або помилка varint-парсингу
```

---

### 🔹 3. `UnmarshalAny` — динамічний парсинг за типом

```go
dst, n, err := UnmarshalAny(
    bytes.NewReader(tc.bin), 
    tc.src.GetType(),        // BoxTypeEsds() = "esds"
    uint64(len(tc.bin)), 
    tc.ctx,
)
require.NoError(t, err)
assert.Equal(t, tc.src, dst)
```

**Навіщо `UnmarshalAny`?**
```
🔍 Сценарій: Ви читаєте файл і не знаєте наперед тип боксу

1. ReadBoxInfo() → тип="esds", size=100
2. UnmarshalAny(r, "esds", 100, ctx) 
3. Бібліотека сама:
   • Знаходить зареєстровану структуру для "esds" (Esds{})
   • Парсить FullBox заголовок
   • Рекурсивно парсить масив дескрипторів:
     - Читає Tag (1 байт)
     - Читає Size як varint (1-4 байти)
     - Викликає IsOptFieldEnabled() для кожного поля
     - Читає тільки увімкнені поля
   • Повертає готовий об'єкт

✅ Ви не пишете 50+ `case "esds":` у своєму коді!
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
log.Printf("esds: % x", esdsBytes)  // [00 00 00 00 03 89 8d...]

// ✅ Добре: зрозумілі параметри
log.Printf("🎵 %s", Stringify(esds, ctx))
// 🎵 Version=0 Flags=0x000000 Descriptors=[
//   {Tag=ESDescr Size=19088743 ESID=4660 StreamDependenceFlag=true ...},
//   {Tag=DecSpecificInfo Size=3 Data=[0x11, 0x22, 0x33]}
// ]

// 🔥 Ще краще: перевірка AudioSpecificConfig
if desc.Tag == mp4.DecSpecificInfoTag {
    log.Printf("🔥 AAC config: % x", desc.Data)
    // 🔥 AAC config: 11 22 33
}
```

---

## 🛠️ Додаткові перевірки: позиція читача

```go
// Після Unmarshal: перевіряємо, що курсор в кінці прочитаних даних
s, err := r.Seek(0, io.SeekCurrent)
require.NoError(t, err)
assert.Equal(t, int64(buf.Len()), s)  // прочитано рівно n байт
```

**Чому це критично?**
```
📦 Файл: [moof][traf][esds:100B][trun][mdat]
                    │
                    ▼
              Парсимо "esds" (100 байт)
                    │
                    ▼
❌ Якщо Unmarshal прочитає 101 байт → "trun" зсунутий → таймстемпи зламались
✅ Якщо курсор точно в кінці "esds" → "trun" читається коректно
```

---

## 🎯 Чому цей тест критичний для вашого HLS-процесора?

### 🔹 Сценарій 1: Ініціалізація AAC декодера на клієнті

```
📡 Камера надсилає fMP4 з AAC аудіо:
1. Unmarshal читає esds бокс
2. Ви знаходите DecSpecificInfo (Tag=0x05)
3. Передаєте `Data: []byte{0x11, 0x22, 0x33}` у Web Audio API / ExoPlayer
4. Декодер ініціалізується з правильними параметрами (48kHz, стерео...)

❌ Без тесту: помилка в varint-парсингу → читаємо не ту кількість байт
   → AudioSpecificConfig пошкоджений → декодер падає → немає звуку
```

### 🔹 Сценарій 2: Валідація вхідного аудіо-стріму

```
🔍 Ви перевіряєте, чи підтримує клієнт цей кодек:
1. Читаєте ObjectTypeIndication з DecoderConfigDescriptor
2. Якщо 0x40 → AAC LC → підтримується всіма браузерами ✅
3. Якщо 0x6B → AC-3 → потрібен додатковий плагін ⚠️
4. Якщо невідоме значення → відхиляєте сегмент ❌

✅ Тест гарантує, що бітова упаковка прапорців коректна
❌ Без тесту: 0x40 читається як 0x41 → неправильна валідація
```

### 🔹 Сценарій 3: Генерація нових fMP4 з правильними параметрами

```
📝 Ви змінюєте бітрейт аудіо на льоту:
1. Прочитали esds з вхідного потоку
2. Змінили AvgBitrate з 128000 → 64000 у DecoderConfigDescriptor
3. Записали оновлений esds у новий сегмент
4. Клієнт отримує легший потік, але звук залишається чітким

✅ Тест гарантує ідемпотентність: зміни не ламають формат
❌ Без тесту: пошкоджений varint → плеєр не може парсити esds → помилка
```

---

## 🧪 Як додати свій тест-кейс?

```go
// Додайте у масив testCases новий елемент:
{
    name: "esds: AAC LC 48kHz stereo",
    src: &Esds{
        FullBox: FullBox{Version: 0, Flags: [3]byte{0,0,0}},
        Descriptors: []Descriptor{
            // ESDescriptor
            {
                Tag:  mp4.ESDescrTag,
                Size: 25,
                ESDescriptor: &mp4.ESDescriptor{
                    ESID:           1,
                    StreamPriority: 15,
                },
            },
            // DecoderConfigDescriptor
            {
                Tag:  mp4.DecoderConfigDescrTag,
                Size: 15 + 2,  // 15 байт заголовка + 2 байти AudioSpecificConfig
                DecoderConfigDescriptor: &mp4.DecoderConfigDescriptor{
                    ObjectTypeIndication: 0x40,  // AAC LC
                    StreamType:           0x05,  // Audio
                    BufferSizeDB:         1536,
                    MaxBitrate:           128000,
                    AvgBitrate:           128000,
                },
            },
            // DecSpecificInfo: AudioSpecificConfig для AAC LC, 48kHz, стерео
            {
                Tag:  mp4.DecSpecificInfoTag,
                Size: 2,
                Data: []byte{0x12, 0x10},  // ObjectType=2, FreqIdx=4, ChanCfg=2
            },
            // SLConfigDescriptor
            {
                Tag:  mp4.SLConfigDescrTag,
                Size: 1,
                Data: []byte{0x02},
            },
        },
    },
    dst: &Esds{},
    // Очікувані байти (розраховуються бібліотекою):
    bin: []byte{
        0, 0x00, 0x00, 0x00,  // FullBox
        0x03, 0x19, 0x00, 0x01, 0x0f,  // ESDescriptor
        0x04, 0x13, 0x40, 0x15, 0x00, 0x00, 0x06, 0x00, 0x01, 0xf4, 0x00, 0x01, 0xf4,  // DecoderConfig
        0x05, 0x02, 0x12, 0x10,  // DecSpecificInfo
        0x06, 0x01, 0x02,  // SLConfig
    },
    str: `Version=0 Flags=0x000000 Descriptors=[` +
        `{Tag=ESDescr Size=25 ESID=1 StreamPriority=15}, ` +
        `{Tag=DecoderConfigDescr Size=17 ObjectTypeIndication=0x40 StreamType=5 AvgBitrate=128000}, ` +
        `{Tag=DecSpecificInfo Size=2 Data=[0x12, 0x10]}, ` +
        `{Tag=SLConfigDescr Size=1 Data=[0x2]}]`,
},
```

**Запустіть тест**:
```bash
go test -v ./mp4 -run TestBoxTypesISO14496_14/esds_AAC_LC_48kHz_stereo
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний парсинг varint | `Size` читається неправильно → зсув даних | Використовуйте бібліотечний парсер, не пишіть свій |
| Ігнорування `opt=dynamic` | Читаєте "зайві" поля → помилка парсингу | Завжди перевіряйте `IsOptFieldEnabled()` перед доступом до опціональних полів |
| Неправильне кодування прапорців | `StreamDependenceFlag=1` читається як `0` | Пам'ятайте порядок бітів: [Dependence][Url][Ocr][Priority:5] |
| Забути 24-бітне `BufferSizeDB` | Читаєте 4 байти замість 3 → зсув | Пам'ятайте: BufferSizeDB = 3 байти, не 4! |
| Неправильна вкладеність дескрипторів | `DecSpecificInfo` не знайдено → декодер не ініціалізується | Рекурсивно парсіть `Descriptors[]` у `DecoderConfigDescriptor` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні fMP4 з аудіо:
    • Шукайте `esds` бокс у `stsd` → `mp4a` → `esds`
    • Перевіряйте `ObjectTypeIndication == 0x40` для AAC LC
    • Витягуйте `DecSpecificInfo` (Tag=0x05) для ініціалізації декодера

[ ] Для валідації аудіо-потоку:
    • Перевіряйте `StreamType == 0x05` (аудіо)
    • Логувайте `AvgBitrate` для моніторингу якості
    • Відхиляйте невідомі `ObjectTypeIndication`

[ ] При генерації нових сегментів:
    • Формуйте правильний `AudioSpecificConfig` для вашої конфігурації
    • Встановлюйте `BufferSizeDB` адекватно до бітрейту
    • Додавайте `SLConfigDescriptor` (Tag=0x06) для сумісності

[ ] Для дебагу:
    • Логуйте сирий вміст `DecSpecificInfo`: 
      log.Printf("🔥 AAC config: % x", configData)
    • Використовуйте `Stringify()` для людського виводу: 
      log.Printf("🎵 %s", Stringify(esds, ctx))

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: VLC, hls.js, ExoPlayer, Safari
    • Перевірте відтворення на різних пристроях (ТВ, телефон, ПК)
```

---

## 🎯 Висновок

> **Цей тест — ваш "страховий поліс" проти пошкодження MPEG-4 конфігурації**.  
> Він гарантує:
> • ✅ Коректне varint-кодування розмірів дескрипторів
> • ✅ Правильну бітову упаковку прапорців у `ESDescriptor`
> • ✅ Обробку 24-бітних полів (`BufferSizeDB`)
> • ✅ Ідемпотентність серіалізації/десеріалізації (round-trip)
> • ✅ Динамічний парсинг через `UnmarshalAny`
> • ✅ Зручний дебаг через `Stringify`

Для вашого **CCTV HLS Processor** це означає:
- 🔊 Клієнти отримують коректну конфігурацію AAC для ініціалізації декодера
- 🌐 Підтримка всіх сучасних плеєрів (Safari, Chrome, VLC, ExoPlayer)
- 📉 Економія бітрейту завдяки правильній валідації параметрів
- 🔧 Безпечна модифікація аудіо-параметрів на льоту без пошкодження формату

Потребуєте допомоги з парсингом `AudioSpecificConfig` для AAC або з інтеграцією `esds` у ваш конвеєр обробки аудіо? Напишіть — покажу готовий код! 🚀🔊