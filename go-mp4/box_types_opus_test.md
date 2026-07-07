# 🧪 Тест `TestBoxTypesOpus`: Opus Decoder Configuration (`dOps`)

Це **юніт-тест** для бібліотеки `go-mp4`, який перевіряє коректну роботу **серіалізації/десеріалізації** боксу `dOps` (Opus Decoder Configuration) згідно зі специфікацією [Opus in ISOBMFF](https://opus-codec.org/docs/opus_in_isobmff.html).

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що параметри Opus-аудіо (канали, затримка, мапінгу, посилення) коректно перетворюються між структурою Go та 15-байтовим бінарним форматом** — критично для ініціалізації Opus-декодера у плеєрах.

---

## 📋 Структура тесту

```go
func TestBoxTypesOpus(t *testing.T) {
    // 1. Масив тест-кейсів (тут 1 кейс "dOps")
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

## 🔍 Детальний розбір тест-кейсу: "dOps"

### ✅ Вхідні дані (`src`) — конфігурація Opus-декодера

```go
src: &DOps{
    Version:              0,   // 🔹 Версія формату (завжди 0)
    OutputChannelCount:   2,   // 🔹 Стерео (2 канали)
    PreSkip:              312, // 🔹 Затримка: 312 семпли @ 48000 Hz = 6.5 мс
    InputSampleRate:      48000, // 🔹 Вхідна частота: 48 kHz
    OutputGain:           0,   // 🔹 Без посилення (0/256 dB)
    ChannelMappingFamily: 2,   // 🔹 Нестандартне мапінгу!
    StreamCount:          1,   // 🔹 1 потік Opus
    CoupledCount:         1,   // 🔹 1 спарений канал (стерео)
    ChannelMapping:       []uint8{1, 2},  // 🔹 Мапа: канал 0→1, канал 1→2
},
```

**📊 Що це означає для вашого стріму:**

| Поле | Значення | Практичне значення |
|------|----------|-------------------|
| `OutputChannelCount=2` | Стерео | ✅ Стандарт для музики/відео |
| `PreSkip=312` | 6.5 мс затримки | 🔹 Для точної синхронізації аудіо/відео |
| `InputSampleRate=48000` | 48 kHz | ✅ Оптимальна частота для Opus |
| `ChannelMappingFamily=2` | Нестандартне мапінгу | 🔹 Потрібні додаткові поля (StreamCount, CoupledCount, ChannelMapping) |
| `ChannelMapping=[1,2]` | Мапа каналів | 🔹 Вихідний канал 0 ← потік 1, канал 1 ← потік 2 |

> 🎯 **Важливо**: Оскільки `ChannelMappingFamily != 0`, опціональні поля **увімкнені** і будуть записані у файл!

---

### ✅ Очікувані байти (`bin`) — 15 байт конфігурації

```go
bin: []byte{
    // 🔹 Заголовок (9 байт):
    0x00,                          // Version = 0
    0x02,                          // OutputChannelCount = 2
    0x01, 0x38,                    // PreSkip = 312 (0x0138)
    0x00, 0x00, 0xbb, 0x80,        // InputSampleRate = 48000 (0x0000bb80)
    0x00, 0x00,                    // OutputGain = 0 (int16)
    
    // 🔹 Мапінгу (6 байт, бо ChannelMappingFamily=2 != 0):
    0x02,                          // ChannelMappingFamily = 2
    0x01,                          // StreamCount = 1
    0x01,                          // CoupledCount = 1
    0x01, 0x02,                    // ChannelMapping[] = [1, 2]
},
```

**🔢 Детальна розбивка:**

```
📦 dOps бокс (всього 15 байт):
┌─────────────────────────────────┐
│ Байт 0:  Version = 0x00         │
├─────────────────────────────────┤
│ Байт 1:  OutputChannelCount=0x02│ ← 2 канали (стерео)
├─────────────────────────────────┤
│ Байти 2-3:  PreSkip = 0x0138    │ ← 312 десяткове = 6.5 мс @ 48kHz
├─────────────────────────────────┤
│ Байти 4-7:  InputSampleRate     │
│             = 0x0000bb80         │ ← 48000 десяткове
├─────────────────────────────────┤
│ Байти 8-9:  OutputGain = 0x0000 │ ← 0/256 dB = без змін
├─────────────────────────────────┤
│ Байт 10: ChannelMappingFamily=2 │ ← 🔹 Нестандартне мапінгу!
├─────────────────────────────────┤
│ Байт 11: StreamCount = 1        │ ← 1 потік Opus
├─────────────────────────────────┤
│ Байт 12: CoupledCount = 1       │ ← 1 спарений канал
├─────────────────────────────────┤
│ Байт 13: ChannelMapping[0] = 1  │ ← мапа каналів
│ Байт 14: ChannelMapping[1] = 2  │
└─────────────────────────────────┘
```

> 🎯 **Магія бібліотеки**: Ви працюєте з окремими полями (`OutputChannelCount`, `PreSkip`), а бібліотека сама пакує їх у бінарний формат за тегами `mp4:"...,size=N"`.

---

### ✅ Очікуваний рядок для дебагу (`str`)

```go
str: `Version=0 OutputChannelCount=0x2 PreSkip=312 InputSampleRate=48000 ` +
     `OutputGain=0 ChannelMappingFamily=0x2 StreamCount=0x1 CoupledCount=0x1 ` +
     `ChannelMapping=[0x1, 0x2]`,
```

**Навіщо це?**
- 🪲 **Дебаг**: Замість дампів байтів бачите `PreSkip=312` (6.5 мс затримки)
- 🧪 **Тести**: `assert.Equal(t, expected, actual)` для швидкого виявлення помилок
- 📋 **Логи**: `log.Printf("🎵 %s", Stringify(dops, ctx))` у продакшені

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
📦 Вхід: DOps{OutputChannelCount:2, PreSkip:312, ChannelMappingFamily:2, ...}
              │
              ▼
         Marshal() + бітова упаковка + логіка опціональних полів
              │
              ▼
📤 Вихід: []byte{0x00, 0x02, 0x01, 0x38, 0x00, 0x00, 0xbb, 0x80, 
                 0x00, 0x00, 0x02, 0x01, 0x01, 0x01, 0x02}
              │
              ▼
✅ Порівнюємо з еталонним масивом — кожен байт на своєму місці
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
Якщо ні → ❌ втрата даних → декодер отримає хибні параметри → звук "зламається"
```

---

### 🔹 3. `UnmarshalAny` — динамічний парсинг за типом

```go
dst, n, err := UnmarshalAny(
    bytes.NewReader(tc.bin), 
    tc.src.GetType(),        // BoxTypeDOps() = "dOps"
    uint64(len(tc.bin)), 
    tc.ctx,
)
require.NoError(t, err)
assert.Equal(t, tc.src, dst)
```

**Навіщо `UnmarshalAny`?**
```
🔍 Сценарій: Ви читаєте файл і не знаєте наперед тип боксу

1. ReadBoxInfo() → тип="dOps", size=15
2. UnmarshalAny(r, "dOps", 15, ctx) 
3. Бібліотека сама:
   • Знаходить зареєстровану структуру для "dOps" (DOps{})
   • Читає обов'язкові поля (Version...OutputGain)
   • Перевіряє ChannelMappingFamily:
     - Якщо != 0 → читає опціональні поля (StreamCount, CoupledCount, ChannelMapping[])
   • Повертає готовий об'єкт

✅ Ви не пишете 50+ `case "dOps":` у своєму коді!
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
log.Printf("dOps: % x", dopsBytes)  // [00 02 01 38 00 00 bb 80...]

// ✅ Добре: зрозумілі параметри
log.Printf("🎵 %s", Stringify(dops, ctx))
// 🎵 Version=0 OutputChannelCount=0x2 PreSkip=312 InputSampleRate=48000 
//    OutputGain=0 ChannelMappingFamily=0x2 StreamCount=0x1 CoupledCount=0x1 
//    ChannelMapping=[0x1, 0x2]

// 🔥 Ще краще: людино-читабельна інтерпретація
if dops.ChannelMappingFamily == 0 {
    log.Printf("🎵 Standard channel mapping: %d channels", dops.OutputChannelCount)
} else {
    log.Printf("🎵 Custom mapping: streams=%d, coupled=%d, map=%v", 
        dops.StreamCount, dops.CoupledCount, dops.ChannelMapping)
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
📦 Файл: [moof][traf][dOps:15B][mdat]
                    │
                    ▼
              Парсимо "dOps" (рівно 15 байт)
                    │
                    ▼
❌ Якщо Unmarshal прочитає 16 байт → "mdat" зсунутий → аудіо пошкоджене
✅ Якщо курсор точно в кінці "dOps" → "mdat" читається коректно
```

---

## 🎯 Чому цей тест критичний для вашого HLS-процесора?

### 🔹 Сценарій 1: Точна синхронізація аудіо/відео

```
📡 Ви додаєте Opus-аудіо до відео-стріму:
1. Формуєте dOps: PreSkip=312 (6.5 мс затримки кодека)
2. Записуєте у fMP4-сегмент
3. Плеєр читає PreSkip → пропускає перші 312 семпли при декодуванні
4. Аудіо ідеально синхронізується з відео-кадрами

❌ Без тесту: помилка в кодуванні PreSkip → 312 читається як 132
   → аудіо відстає на ~3.75 мс → десинхронізація губ/звуку
```

### 🔹 Сценарій 2: Підтримка нестандартного мапінгу каналів

```
🔊 Професійна камера з 5.1 аудіо та кастомним розташуванням мікрофонів:
1. Формуєте dOps: ChannelMappingFamily=2 (нестандартне)
2. Вказуєте StreamCount=3, CoupledCount=2, ChannelMapping=[0,1,2,3,4,5]
3. Плеєр з підтримкою кастомного мапінгу коректно відображає канали

✅ Тест гарантує, що опціональні поля читаються тільки коли ChannelMappingFamily != 0
❌ Без тесту: мапінгу читається для стандартного випадку → канали переплутані
```

### 🔹 Сценарій 3: Валідація параметрів перед стрімінгом

```
🔍 Ви перевіряєте, чи підтримає клієнт цей потік:
1. Читаєте dOps: OutputChannelCount=2, InputSampleRate=48000
2. Перевіряєте: чи підтримує клієнт стерео @ 48kHz?
   • Chrome/Firefox → так ✅
   • Старий плеєр → ні ❌ → конвертуєте в моно @ 24kHz
3. Логуєте параметри для моніторингу якості

✅ Тест гарантує, що всі поля коректно серіалізуються/десеріалізуються
❌ Без тесту: OutputGain читається неправильно → звук занадто гучний/тихий
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильне кодування `PreSkip` | Десинхронізація аудіо/відео на початку | Пам'ятайте: `PreSkip` — це 16-біт uint, big-endian |
| Ігнорування `opt=dynamic` | Опціональні поля читаються завжди → зсув даних | Завжди перевіряйте `IsOptFieldEnabled()` або покладайтеся на бібліотеку |
| Неправильна довжина `ChannelMapping` | Читаєте не ту кількість байт → краш | `GetFieldLength()` має повертати `OutputChannelCount` |
| Невідповідність `StreamCount`/`CoupledCount` | Декодер не може ініціалізуватися | Перевіряйте: `CoupledCount*2 + (StreamCount-CoupledCount) == OutputChannelCount` |
| Забути `Version=0` | Плеєр відмовляє відтворювати | Завжди встановлюйте `Version = 0` (єдина підтримувана версія) |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні Opus-аудіо:
    • Шукайте `dOps` бокс всередині `Opus` у `stsd`
    • Перевіряйте `Version == 0` для сумісності
    • Логувайте `OutputChannelCount` та `InputSampleRate` для моніторингу

[ ] Для сумісності з вебом:
    • Моно (1) або стерео (2) підтримується всюди
    • `InputSampleRate = 48000` — оптимальний вибір
    • Уникайте нестандартного мапінгу (`ChannelMappingFamily != 0`) для широкого розповсюдження

[ ] При генерації нових сегментів:
    • Встановлюйте `PreSkip` відповідно до затримки кодека (зазвичай 312 для 6.5 мс)
    • Для стандартного мапінгу: `ChannelMappingFamily = 0`, опціональні поля відсутні
    • Узгоджуйте `OutputChannelCount` з реальною кількістю каналів у потоці

[ ] Для дебагу:
    • Логуйте конфігурацію: log.Printf("🎵 Opus: %d ch, %d Hz, gain=%.1f dB", ...)
    • Перевіряйте мапінгу: if mappingFamily != 0 { log.Printf("Custom mapping: %v", channelMapping) }
    • Використовуйте `Stringify()` для людського виводу

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: Chrome (WebM/Opus), Firefox, VLC
    • Перевірте синхронізацію: аудіо має починатися точно з відео
```

---

## 🎯 Висновок

> **Цей тест — ваш "страховий поліс" проти пошкодження Opus-конфігурації**.  
> Він гарантує:
> • ✅ Коректну бітову упаковку всіх полів `dOps` (обов'язкових + опціональних)
> • ✅ Правильну логіку `IsOptFieldEnabled`: опціональні поля тільки при `ChannelMappingFamily != 0`
> • ✅ Ідемпотентність серіалізації/десеріалізації (round-trip)
> • ✅ Динамічний парсинг через `UnmarshalAny`
> • ✅ Зручний дебаг через `Stringify` з людино-читабельними значеннями

Для вашого **CCTV HLS Processor** це означає:
- 🔊 Високоякісне аудіо з точною синхронізацією завдяки `PreSkip`
- ⚡ Економія бітрейту без втрати якості (Opus ефективніший за AAC при <64 kbps)
- 🔄 Гнучкість: підтримка як стандартного, так і кастомного мапінгу каналів
- 🌐 Сумісність з сучасними браузерами та плеєрами (Chrome, Firefox, VLC)

Потребуєте допомоги з інтеграцією Opus у ваш конвеєр або з валідацією параметрів `dOps`? Напишіть — покажу готовий код! 🚀🎵