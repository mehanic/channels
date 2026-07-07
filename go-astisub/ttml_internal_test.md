<think>
# 🧪 Глибокий розбір `astisub_test/ttml_duration_test.go` — тестування парсингу часових міток у форматі TTML

Це **набір юніт-тестів** для валідації парсингу та серіалізації часових міток у форматі TTML (Timed Text Markup Language). Тести покривають різні формати представлення часу: `hh:mm:ss.mmm`, `hh:mm:ss:fff` (з кадрами), відносні одиниці (`h`, `m`, `s`, `ms`), кадри (`f`) та тіки (`t`). Розберемо архітектурно:

---

## 🧱 1. Архітектура тестування: акцент на часові формати

### 🔧 Використання testify для читабельних перевірок:

```go
import (
    "github.com/stretchr/testify/assert"  // продовжує тест після помилки
)

// Приклад перевірки тривалості:
assert.Equal(t, 12*time.Hour+34*time.Minute+56*time.Second+789*time.Millisecond, d.duration())
```

### 🎯 Чому тести зосереджені на часових форматах?

```
TTML підтримує кілька форматів представлення часу:
1. "12:34:56.789" — годинник з мілісекундами (clock time)
2. "12:34:56:2" — годинник з кадрами (frames)
3. "123h", "123.4m", "123s", "123ms" — відносні одиниці
4. "100f" — кадри (залежить від framerate)
5. "6t" — тіки (залежить від tickrate)

Кожен формат має свою логіку парсингу та конвертації:
• Мілісекунди: пряме перетворення
• Кадри: залежать від framerate (наприклад, 25 fps → 1 кадр = 40ms)
• Тіки: залежать від tickrate (наприклад, 4 tickrate → 1 тік = 250ms)

Без ретельного тестування:
• Помилки у конвертації кадрів → розсинхронізація субтитрів з відео
• Неправильна обробка дробових значень → накопичення помилок у тривалих записах
• Несумісність з іншими форматами (WebVTT, STL) при експорті
```

---

## 🔍 2. Формати часу у TTML: детальний розбір тестових кейсів

### 🔸 Кейс 1: `hh:mm:ss.mmm` — стандартний формат годинника

```go
// Unmarshal hh:mm:ss.mmm format - clock time
var d = &TTMLInDuration{}
err := d.UnmarshalText([]byte("12:34:56.789"))
assert.NoError(t, err)
assert.Equal(t, 12*time.Hour+34*time.Minute+56*time.Second+789*time.Millisecond, d.duration())

// Marshal
b, err := TTMLOutDuration(d.duration()).MarshalText()
assert.NoError(t, err)
assert.Equal(t, "12:34:56.789", string(b))
```

**Особливості**:
• Формат: `години:хвилини:секунди.мілісекунди`
• Мілісекунди завжди 3 цифри (навіть `001` для 1 ms)
• Roundtrip тест: парсинг → серіалізація → порівняння з оригіналом

**Чому це критично**:
```
Це основний формат для синхронізації субтитрів з відео.
Помилка у парсингу мілісекунд призведе до:
• Розсинхронізації на 1-999 ms (помітно для людського ока)
• Накопичення помилок у довгих записах (наприклад, 1 година × 1 ms = 3.6 секунди!)
• Несумісності з плеєрами, що очікують точний формат
```

### 🔸 Кейс 2: `hh:mm:ss:fff` — формат з кадрами

```go
// Unmarshal hh:mm:ss:fff format
err = d.UnmarshalText([]byte("12:34:56:2"))
assert.NoError(t, err)
assert.Equal(t, 12*time.Hour+34*time.Minute+56*time.Second, d.duration())
assert.Equal(t, 2, d.frames)  // ← збереження окремого поля frames

// Duration з урахуванням framerate
d.framerate = 8  // 8 кадрів на секунду
assert.Equal(t, 12*time.Hour+34*time.Minute+56*time.Second+250*time.Millisecond, d.duration())
// Розрахунок: 2 кадри / 8 fps = 0.25 секунди = 250 ms ✓
```

**Особливості**:
• Формат: `години:хвилини:секунди:кадри` (двокрапка замість крапки!)
• Кадри зберігаються окремо у полі `d.frames`
• Конвертація у тривалість залежить від `framerate`

**Чому це критично**:
```
Відео часто використовує кадри як одиницю часу (наприклад, 25 fps, 30 fps).
TTML дозволяє вказувати час у кадрах для точної синхронізації з відео.

Приклад розрахунку:
• Вхід: "00:00:01:2" (1 секунда + 2 кадри)
• framerate = 25 fps → 1 кадр = 40 ms
• Результат: 1000 ms + 2×40 ms = 1080 ms

Без цієї логіки:
• Субтитри з'являться на 2 кадри раніше/пізніше за відео
• У динамічних сценах це помітно як "не в попадання губ"
```

### 🔸 Кейс 3: Відносні одиниці (`h`, `m`, `s`, `ms`)

```go
// Unmarshal offset time
err = d.UnmarshalText([]byte("123h"))
assert.Equal(t, 123*time.Hour, d.duration())

err = d.UnmarshalText([]byte("123.4h"))
assert.Equal(t, 123*time.Hour+4*time.Hour/10, d.duration())  // ← дробова частина

err = d.UnmarshalText([]byte("123m"))
assert.Equal(t, 123*time.Minute, d.duration())

err = d.UnmarshalText([]byte("123.4m"))
assert.Equal(t, 123*time.Minute+4*time.Minute/10, d.duration())

// Аналогічно для секунд та мілісекунд...
```

**Особливості**:
• Підтримка дробових значень: `123.4h` = 123 години + 0.4 години = 123 год 24 хв
• Обробка через цілочисельне ділення: `4*time.Hour/10` = 24 хвилини
• Універсальність: можна вказувати час у будь-яких одиницях

**Чому це критично**:
```
Відносні одиниці зручні для:
• Коротких субтитрів: "2.5s" замість "00:00:02.500"
• Тривалих записів: "1.5h" замість "01:30:00.000"
• Конфігураційних файлів: легше читати та редагувати

Обробка дробових значень через цілочисельне ділення:
• `4*time.Hour/10` = (4×3600×1000)/10 ms = 1,440,000 ms = 24 хвилини ✓
• Це запобігає помилкам округлення у плаваючій точці
```

### 🔸 Кейс 4: Кадри (`f`) з урахуванням framerate

```go
d.framerate = 25  // 25 кадрів на секунду
err = d.UnmarshalText([]byte("100f"))
assert.Equal(t, 4*time.Second, d.duration())  // 100 кадрів / 25 fps = 4 секунди ✓
```

**Особливості**:
• Конвертація: `кадри / framerate = секунди`
• Залежність від зовнішнього параметра `framerate`
• Цілочисельне ділення: `100 / 25 = 4` (без залишку)

**Чому це критично**:
```
Різні відео стандарти використовують різні framerate:
• 24 fps — кіно
• 25 fps — PAL (Європа)
• 30 fps — NTSC (США)
• 60 fps — висока частота кадрів

Приклад помилки:
• Вхід: "100f", очікуваний framerate = 25 fps
• Якщо парсер використає 30 fps за замовчуванням: 100/30 = 3.33 секунди
• Різниця: 4.00 - 3.33 = 0.67 секунди = 670 ms → помітна розсинхронізація!

У вашому CCTV HLS Processor:
• Парсинг `framerate` з метаданих відео
• Передача цього значення у парсер часових міток
• Гарантія, що субтитри синхронізовані з конкретним відео
```

### 🔸 Кейс 5: Тіки (`t`) з урахуванням tickrate

```go
// Tick rate duration
d.tickrate = 4  // 4 тіки на секунду
err = d.UnmarshalText([]byte("6t"))
assert.Equal(t, time.Second+500*time.Millisecond, d.duration())  // 6/4 = 1.5 секунди ✓
```

**Особливості**:
• Тіки — внутрішня одиниця часу у деяких системах (наприклад, MPEG-TS)
• Конвертація: `тіки / tickrate = секунди`
• Підтримка дробових результатів: `6/4 = 1.5` секунди

**Чому це критично**:
```
Тіки використовуються у:
• MPEG-TS транспортних потоках (90 kHz clock)
• Деяких професійних системах субтитрування
• Внутрішніх представленнях часу у бібліотеках

Приклад конвертації для CCTV:
• Вхід: субтитри у тіках з tickrate = 4
• Відео: 25 fps, PTS у 90 kHz clock
• Конвертація: тіки → секунди → PTS → синхронізація з відео

Без підтримки тіків:
• Неможливість імпорту субтитрів з професійних джерел
• Втрата точності при конвертації між форматами
```

---

## 🔄 3. Roundtrip тести: парсинг → серіалізація → порівняння

### 🔧 Перевірка детермінованості:

```go
// 1. Парсинг вхідного рядка
err := d.UnmarshalText([]byte("12:34:56.789"))
assert.NoError(t, err)

// 2. Серіалізація назад у рядок
b, err := TTMLOutDuration(d.duration()).MarshalText()
assert.NoError(t, err)

// 3. Порівняння з оригіналом
assert.Equal(t, "12:34:56.789", string(b))
```

### 🎯 Чому roundtrip тести критичні?

```
Мета: гарантувати, що парсинг + серіалізація зберігають дані без втрат.

Сценарії, де це важливо:
1. Конвертація форматів: TTML → WebVTT → TTML
   • Якщо roundtrip не детермінований → втрата точності часу
2. Редагування субтитрів: читання → зміна → запис
   • Якщо формат змінюється → проблеми з сумісністю
3. Кешування: серіалізація у базу даних → десеріалізація
   • Якщо дані змінюються → помилки синхронізації

У вашому пайплайні:
• Імпорт субтитрів з різних джерел (TTML, WebVTT, STL)
• Конвертація у внутрішній формат для обробки
• Експорт у потрібний формат для HLS-плейлиста
• Roundtrip гарантує, що часові мітки не "пливуть" при конвертаціях
```

---

## 🐞 4. Потенційні проблеми та покращення тестів

### ❗ Критичні недоліки:

1. **Відсутність тестів на невалідні вхідні дані**:
   ```go
   // Що станеться, якщо передати невалідний формат?
   // "12:34:56.789.123" — зайві мілісекунди
   // "12:34:56" — відсутні мілісекунди
   // "12:34:56:2:3" — зайві кадри
   
   // Додати тест:
   func TestTTMLDuration_InvalidInput(t *testing.T) {
       invalidInputs := []string{
           "12:34:56.789.123",  // зайві мілісекунди
           "12:34:56",          // відсутні мілісекунди
           "12:34:56:2:3",      // зайві кадри
           "123.4.5h",          // невалідне дробове число
           "100x",              // невідома одиниця
       }
       
       for _, input := range invalidInputs {
           t.Run(input, func(t *testing.T) {
               d := &TTMLInDuration{}
               err := d.UnmarshalText([]byte(input))
               assert.Error(t, err)  // має повернути помилку
           })
       }
   }
   ```

2. **Не тестується обробка граничних значень**:
   ```go
   // Максимальні/мінімальні значення для кожного формату
   // "99:59:59.999" — максимальний час у форматі годинника
   // "00:00:00.000" — мінімальний час
   // "0f" — нуль кадрів
   // "999999999f" — дуже велика кількість кадрів (переповнення?)
   
   // Додати тест:
   func TestTTMLDuration_BoundaryValues(t *testing.T) {
       tests := []struct{
           input string
           expected time.Duration
           expectError bool
       }{
           {"00:00:00.000", 0, false},
           {"99:59:59.999", 99*time.Hour + 59*time.Minute + 59*time.Second + 999*time.Millisecond, false},
           {"0f", 0, false},
           {"999999999f", 999999999/25 * time.Second, false},  // при framerate=25
           // Додати тест на переповнення, якщо потрібно
       }
       
       for _, tt := range tests {
           t.Run(tt.input, func(t *testing.T) {
               d := &TTMLInDuration{framerate: 25}
               err := d.UnmarshalText([]byte(tt.input))
               if tt.expectError {
                   assert.Error(t, err)
               } else {
                   assert.NoError(t, err)
                   assert.Equal(t, tt.expected, d.duration())
               }
           })
       }
   }
   ```

3. **Відсутність тестів на дробові кадри/тіки**:
   ```go
   // Що станеться, якщо кадри не діляться націло на framerate?
   // "101f" при framerate=25 → 101/25 = 4.04 секунди
   // Чи округлюється результат, чи зберігається дробова частина?
   
   // Додати тест:
   func TestTTMLDuration_FractionalFrames(t *testing.T) {
       d := &TTMLInDuration{framerate: 25}
       err := d.UnmarshalText([]byte("101f"))
       assert.NoError(t, err)
       // 101 кадр / 25 fps = 4.04 секунди = 4040 ms
       expected := 4*time.Second + 40*time.Millisecond
       assert.Equal(t, expected, d.duration())
   }
   ```

4. **Не тестується залежність від зовнішніх параметрів (`framerate`, `tickrate`)**:
   ```go
   // Якщо framerate не встановлено, яка поведінка за замовчуванням?
   // Чи повертається помилка, чи ігнорується поле кадрів?
   
   // Додати тест:
   func TestTTMLDuration_MissingFramerate(t *testing.T) {
       d := &TTMLInDuration{}  // framerate = 0 за замовчуванням
       err := d.UnmarshalText([]byte("100f"))
       // Очікуємо: або помилка, або ігнорування кадрів
       assert.Error(t, err)  // або assert.NoError(t, err) + перевірка duration
   }
   ```

5. **Відсутність тестів на серіалізацію різних форматів**:
   ```go
   // Чи серіалізується тривалість у тому ж форматі, що й вхід?
   // "123.4h" → парсинг → серіалізація → "123:24:00.000" чи "123.4h"?
   
   // Додати тест:
   func TestTTMLDuration_SerializationFormat(t *testing.T) {
       // Вхід у відносних одиницях
       d := &TTMLInDuration{}
       err := d.UnmarshalText([]byte("1.5h"))
       assert.NoError(t, err)
       
       // Серіалізація: який формат виходу?
       b, err := TTMLOutDuration(d.duration()).MarshalText()
       assert.NoError(t, err)
       
       // Очікуємо стандартний формат годинника
       assert.Equal(t, "01:30:00.000", string(b))
   }
   ```

### 💡 Покращення:

```go
// 1. Helper для генерації тестових кейсів
func generateDurationTestCases() []struct{
    input string
    expected time.Duration
    framerate int
    tickrate int
    expectError bool
}{
    // Стандартні кейси
    {"12:34:56.789", 12*time.Hour+34*time.Minute+56*time.Second+789*time.Millisecond, 0, 0, false},
    {"00:00:01:2", 1*time.Second+2*40*time.Millisecond, 25, 0, false},  // 2 кадри @ 25 fps
    {"100f", 4*time.Second, 25, 0, false},  // 100 кадрів @ 25 fps
    {"6t", 1500*time.Millisecond, 0, 4, false},  // 6 тіків @ 4 tickrate
    
    // Граничні кейси
    {"00:00:00.000", 0, 0, 0, false},
    {"99:59:59.999", 99*time.Hour + 59*time.Minute + 59*time.Second + 999*time.Millisecond, 0, 0, false},
    
    // Невалідні кейси
    {"12:34:56.789.123", 0, 0, 0, true},  // зайві мілісекунди
    {"100x", 0, 0, 0, true},  // невідома одиниця
}

// 2. Параметризовані тести для всіх форматів
func TestTTMLDuration_AllFormats(t *testing.T) {
    testCases := generateDurationTestCases()
    
    for _, tc := range testCases {
        t.Run(tc.input, func(t *testing.T) {
            d := &TTMLInDuration{
                framerate: tc.framerate,
                tickrate: tc.tickrate,
            }
            err := d.UnmarshalText([]byte(tc.input))
            if tc.expectError {
                assert.Error(t, err)
                return
            }
            assert.NoError(t, err)
            assert.Equal(t, tc.expected, d.duration())
        })
    }
}

// 3. Тест на стійкість до невалідних даних
func TestTTMLDuration_Robustness(t *testing.T) {
    invalidInputs := []string{
        "",                    // порожній рядок
        ":",                   // тільки роздільники
        "12:34:56.789.123",   // зайві мілісекунди
        "12:34:56:2:3",       // зайві кадри
        "123.4.5h",           // невалідне дробове число
        "100x",               // невідома одиниця
    }
    
    for _, input := range invalidInputs {
        t.Run(input, func(t *testing.T) {
            // Не повинно панікувати
            defer func() {
                if r := recover(); r != nil {
                    t.Errorf("panic on input %q: %v", input, r)
                }
            }()
            d := &TTMLInDuration{}
            err := d.UnmarshalText([]byte(input))
            // Помилка допустима, головне — стабільність
            _ = err
        })
    }
}
```

---

## 🎯 5. Інтеграція з вашим CCTV HLS Processor

### 📍 У `SubtitleImporter` — парсинг часових міток з різних форматів:

```go
type SubtitleImporter struct {
    framerate int  // частота кадрів відео для конвертації кадрів → час
    tickrate  int  // tickrate для конвертації тіків → час
}

func (imp *SubtitleImporter) parseTTMLDuration(durationStr string) (time.Duration, error) {
    d := &astisub.TTMLInDuration{
        framerate: imp.framerate,
        tickrate:  imp.tickrate,
    }
    
    err := d.UnmarshalText([]byte(durationStr))
    if err != nil {
        return 0, fmt.Errorf("parse TTML duration %q: %w", durationStr, err)
    }
    
    return d.duration(), nil
}

func (imp *SubtitleImporter) ImportTTML(filePath string) error {
    // ... читання файлу ...
    
    for _, item := range subs.Items {
        // Парсинг часових міток з підтримкою різних форматів
        start, err := imp.parseTTMLDuration(item.StartAtStr)  // "00:00:01:2" → time.Duration
        if err != nil { return err }
        
        end, err := imp.parseTTMLDuration(item.EndAtStr)
        if err != nil { return err }
        
        // Конвертація у абсолютні PTS для HLS
        frame := &SubtitleFrame{
            StartPTS: convertDurationToPTS(start),
            EndPTS:   convertDurationToPTS(end),
            // ... інші поля ...
        }
        imp.subtitleQueue <- frame
    }
    return nil
}
```

### 📍 У `HLSGenerator` — генерація часових міток для різних форматів:

```go
func (gen *HLSSubtitleGenerator) formatDurationForTTML(d time.Duration, format string) (string, error) {
    switch format {
    case "clock":  // hh:mm:ss.mmm
        return astisub.TTMLOutDuration(d).MarshalText()
    case "frames":  // hh:mm:ss:fff
        if gen.framerate == 0 {
            return "", errors.New("framerate required for frames format")
        }
        frames := int64(d) * int64(gen.framerate) / int64(time.Second)
        base := d / time.Second * time.Second  // цілі секунди
        remainingFrames := frames - int64(base/time.Second*gen.framerate)
        return fmt.Sprintf("%s:%d", formatDuration(base), remainingFrames), nil
    case "relative":  // 123.4h, 123.4m, тощо
        // Вибір одиниці за величиною тривалості
        if d >= time.Hour {
            return fmt.Sprintf("%.1fh", float64(d)/float64(time.Hour)), nil
        } else if d >= time.Minute {
            return fmt.Sprintf("%.1fm", float64(d)/float64(time.Minute)), nil
        } else {
            return fmt.Sprintf("%.3fs", float64(d)/float64(time.Second)), nil
        }
    default:
        return "", fmt.Errorf("unsupported duration format: %s", format)
    }
}
```

### 📍 У метриках — моніторинг якості парсингу часових міток:

```go
func (imp *SubtitleImporter) recordDurationParseMetrics(input string, err error) {
    if err != nil {
        metrics.SubtitleDurationParseErrors.WithLabelValues("TTML", err.Error()).Inc()
        return
    }
    
    metrics.SubtitleDurationsParsed.WithLabelValues("TTML").Inc()
    
    // Статистика за форматами
    if strings.Contains(input, ":") && strings.Contains(input, ".") {
        metrics.SubtitleDurationFormats.WithLabelValues("clock").Inc()
    } else if strings.Contains(input, ":") && !strings.Contains(input, ".") {
        metrics.SubtitleDurationFormats.WithLabelValues("frames").Inc()
    } else if strings.HasSuffix(input, "h") || strings.HasSuffix(input, "m") || strings.HasSuffix(input, "s") {
        metrics.SubtitleDurationFormats.WithLabelValues("relative").Inc()
    } else if strings.HasSuffix(input, "f") {
        metrics.SubtitleDurationFormats.WithLabelValues("frames-only").Inc()
    } else if strings.HasSuffix(input, "t") {
        metrics.SubtitleDurationFormats.WithLabelValues("ticks").Inc()
    }
    
    // Розподіл тривалостей (для оптимізації буферів)
    duration, _ := imp.parseTTMLDuration(input)
    metrics.SubtitleDurationDistribution.Observe(float64(duration.Milliseconds()))
}
```

---

## 🧭 Висновок: чому ці тести — гарантія точності часових міток

| Компонент | Роль у TTML парсері | Вартість помилки без нього |
|-----------|---------------------|---------------------------|
| **Тести на формат `hh:mm:ss.mmm`** | Базова синхронізація субтитрів з відео | Розсинхронізація на мілісекунди → "не в попадання губ" |
| **Тести на формат з кадрами** | Точна синхронізація з відео, що використовує кадри | Помилка на 1 кадр = 33-40 ms → помітна у динамічних сценах |
| **Тести на відносні одиниці** | Підтримка різних способів вказівки часу | Неможливість імпорту субтитрів з деяких джерел |
| **Тести на залежність від framerate/tickrate** | Адаптація до різних відео стандартів | Субтитри не синхронізуються з відео при зміні framerate |
| **Roundtrip тести** | Гарантія детермінованості при конвертації форматів | Втрата точності часу при імпорті/експорті субтитрів |

> 🔑 **Головна ідея**: Ці тести — **страховка від регресій** при зміні логіки парсингу часових міток. Вони документують:
> 1. Як обробляються різні формати представлення часу
> 2. Як конвертуються кадри/тіки у тривалості з урахуванням зовнішніх параметрів
> 3. Як парсер поводиться з невалідними/граничними даними
> 4. Які компроміси прийняті між точністю та сумісністю

Без них будь-яка оптимізація парсера може непомітно зламати синхронізацію субтитрів з відео — і ви дізнаєтесь про це тільки коли користувачі почнуть скаржитись на "роз'їзд" звуку та тексту.

💡 **Фінальна порада**: 
1. Додайте тести на невалідні вхідні дані та граничні значення
2. Покрийте обробку дробових кадрів/тіків та залежність від framerate/tickrate
3. Додайте тести на серіалізацію у різних форматах
4. Реалізуйте fuzz-тести для перевірки стійкості до випадкових/пошкоджених даних
5. Додайте метрики для моніторингу якості парсингу часових міток у продакшені

Це перетворить ці тести з "перевірки базової функціональності" на "гарантію точності синхронізації" для всього вашого пайплайну обробки субтитрів у CCTV HLS Processor.