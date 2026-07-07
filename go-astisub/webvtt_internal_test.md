# 🧪 Глибокий розбір `astisub_test/webvtt_internal_test.go` — тестування внутрішньої логіки парсингу WebVTT

Це **набір юніт-тестів** для валідації низькорівневих функцій парсингу веб-субтитрів у форматі WebVTT. На відміну від інтеграційних тестів, ці тести фокусуються на окремих алгоритмах: обробка голосових міток, інлайн-таймштампів, мапування часових міток для MPEG-TS та регулярні вирази для міжнародних символів. Розберемо архітектурно:

---

## 🧱 1. Архітектура тестування: table-driven + regex-валідація

### 🔧 Використання testify для читабельних перевірок:

```go
import (
    "github.com/stretchr/testify/assert"    // продовжує тест після помилки
    "github.com/stretchr/testify/require"   // зупиняє тест при помилці
)

// Приклад:
assert.Equal(t, expected, actual)  // для перевірки значень
require.NoError(t, err)            // для критичних помилок
```

### 🎯 Чому table-driven тести для `TestTimestampMap`?

```go
for i, c := range []struct {
    line           string
    expectedOffset time.Duration
    expectError    bool
}{
    {
        line:           "X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:180000",
        expectedOffset: 2 * time.Second,
    },
    // ... інші кейси ...
} {
    t.Run(strconv.Itoa(i), func(t *testing.T) {
        // тестова логіка
    })
}
```

**Переваги**:
• Один тестовий блок для всіх варіантів входу
• Легко додавати нові кейси без дублювання коду
• Чіткий звіт: який саме кейс не пройшов

**Недоліки**:
• Менш читабельні помилки (номер кейсу замість опису)
• Важче дебажити складні сценарії

> 💡 **Практичне значення**: Це дозволяє покрити всі можливі варіанти формату `X-TIMESTAMP-MAP` у одному тесті, що критично для надійності конвертації часових міток.

---

## 🔍 2. `TestParseTextWebVTT` — парсинг текстового контенту з тегами

### 🔧 Тестові кейси для голосових міток `<v Name>`:

```go
t.Run("When both voice tags are available", func(t *testing.T) {
    testData := `<v Bob>Correct tag</v>`
    s := parseTextWebVTT(testData, &StyleAttributes{})
    assert.Equal(t, "Bob", s.VoiceName)
    assert.Equal(t, 1, len(s.Items))
    assert.Equal(t, "Correct tag", s.Items[0].Text)
})

t.Run("When there is no end tag", func(t *testing.T) {
    testData := `<v Bob>Text without end tag`
    s := parseTextWebVTT(testData, &StyleAttributes{})
    assert.Equal(t, "Bob", s.VoiceName)  // ← VoiceName витягується навіть без </v>
    assert.Equal(t, 1, len(s.Items))
    assert.Equal(t, "Text without end tag", s.Items[0].Text)
})

t.Run("When the end tag is correct", func(t *testing.T) {
    testData := `<v Bob>Incorrect end tag</vi>`  // ← помилковий закриваючий тег
    s := parseTextWebVTT(testData, &StyleAttributes{})
    assert.Equal(t, "Bob", s.VoiceName)  // ← ігнорує </vi>
    assert.Equal(t, 1, len(s.Items))
    assert.Equal(t, "Incorrect end tag", s.Items[0].Text)
})
```

### 🎯 Чому `parseTextWebVTT` ігнорує неправильні закриваючі теги?

```
WebVTT специфікація дозволяє опціональні закриваючі теги:
• <v Name>текст → валідно (закриття наприкінці рядка)
• <v Name>текст</v> → валідно (явне закриття)
• <v Name>текст</vi> → невалідно, але парсер має бути стійким

Рішення `astisub`:
1. При парсингі <v ...> витягнути VoiceName з атрибутів
2. Ігнорувати будь-які закриваючі теги, що не співпадають з відкриваючим
3. Вважати тег закритим наприкінці рядка або при наступному тегу

Це запобігає:
• Падінню парсера на "брудних" субтитрах з реальних камер
• Втраті всього рядка через одну помилку в тегу
• Несумісності з різними реалізаціями WebVTT-генераторів
```

### 🔧 Тестові кейси для інлайн-таймштампів `<00:01:01.000>`:

```go
t.Run("When inline timestamps are included", func(t *testing.T) {
    testData := `<00:01:01.000>With inline <00:01:02.000>timestamps`
    s := parseTextWebVTT(testData, &StyleAttributes{})
    
    // Очікуємо два текстових елементи з різними часовими мітками
    assert.Equal(t, 2, len(s.Items))
    assert.Equal(t, "With inline ", s.Items[0].Text)
    assert.Equal(t, time.Minute+time.Second, s.Items[0].StartAt)  // 00:01:01.000
    assert.Equal(t, "timestamps", s.Items[1].Text)
    assert.Equal(t, time.Minute+2*time.Second, s.Items[1].StartAt)  // 00:01:02.000
})

t.Run("When inline timestamps together", func(t *testing.T) {
    testData := `<00:01:01.000><00:01:02.000>With timestamp tags together`
    s := parseTextWebVTT(testData, &StyleAttributes{})
    
    // Два таймштампи підряд → використовується останній
    assert.Equal(t, 1, len(s.Items))  // один текстовий елемент
    assert.Equal(t, "With timestamp tags together", s.Items[0].Text)
    assert.Equal(t, time.Minute+2*time.Second, s.Items[0].StartAt)  // останній таймштамп
})

t.Run("When inline timestamps is at end", func(t *testing.T) {
    testData := `With end timestamp<00:01:02.000>`
    s := parseTextWebVTT(testData, &StyleAttributes{})
    
    // Таймштамп в кінці без тексту після нього → ігнорується
    assert.Equal(t, 1, len(s.Items))
    assert.Equal(t, "With end timestamp", s.Items[0].Text)
    assert.Equal(t, time.Duration(0), s.Items[0].StartAt)  // таймштамп не застосовується
})
```

### 🎯 Чому інлайн-таймштампи важливі для CCTV?

```
WebVTT підтримує інлайн-таймштампи для:
• Синхронізації тексту з конкретними моментами відео
• Створення "караоке-ефекту" (підсвічування слів у часі)
• Точного позиціонування субтитрів у складних сценах

У вашому CCTV HLS Processor:
1. Парсинг інлайн-таймштампів дозволяє імпортувати субтитри з професійних систем
2. Конвертація у абсолютні PTS для HLS забезпечує синхронізацію з відео
3. Підтримка "караоке-режиму" для навчальних/тренувальних відео

Без цієї функціональності:
• Втрата точності синхронізації тексту з відео
• Неможливість імпорту субтитрів з деяких професійних джерел
• Обмеження у підтримці розширених функцій субтитрування
```

---

## ⏱️ 3. `TestTimestampMap` — парсинг X-TIMESTAMP-MAP для MPEG-TS синхронізації

### 🔍 Формат метаданих:

```
X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:180000

Це мапує:
• LOCAL:00:00:00.000 → WebVTT час (відносний, у форматі годинник)
• MPEGTS:180000 → абсолютний час у транспортному потоці (90 kHz clock)

Формула конвертації:
PTS_MPEG = (PTS_WebVTT_у_секундах × 90000) + MPEGTS_offset

Приклад розрахунку:
• WebVTT: 00:00:00.500 → 500 ms
• MPEGTS offset: 180000 ticks @ 90 kHz = 2 секунди
• Результат: 500 ms + 2000 ms = 2500 ms → 225000 ticks @ 90 kHz
```

### 🔧 Тестові кейси з різними варіантами:

```go
for i, c := range []struct {
    line           string
    expectedOffset time.Duration
    expectError    bool
}{
    // Базовий кейс: 180000 ticks = 2 секунди
    {
        line:           "X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:180000",
        expectedOffset: 2 * time.Second,
    },
    // LOCAL з мілісекундами: 00:00:00.500 → 500 ms
    {
        line:           "X-TIMESTAMP-MAP=LOCAL:00:00:00.500,MPEGTS:180000",
        expectedOffset: 1500 * time.Millisecond,  // 2s - 0.5s = 1.5s offset
    },
    // MPEGTS з іншим значенням: 135000 ticks = 1.5 секунди
    {
        line:           "X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:135000",
        expectedOffset: 1500 * time.Millisecond,
    },
    // Велике значення: 324090000 ticks = 1 година + 1 секунда
    {
        line:           "X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:324090000",
        expectedOffset: time.Hour + time.Second,
    },
    // Помилкові кейси: невалідні числа, відсутні значення
    {
        line:        "X-TIMESTAMP-MAP=MPEGTS:foo, LOCAL:00:00:00.000",
        expectError: true,
    },
    {
        line:        "X-TIMESTAMP-MAP=MPEGTS:180000,LOCAL:bar",
        expectError: true,
    },
    // ... інші помилкові формати ...
}
```

### 🎯 Чому offset розраховується як `MPEGTS - LOCAL`?

```
Формула: offset = MPEGTS_ticks - (LOCAL_у_секундах × 90000)

Приклад 1:
• LOCAL:00:00:00.000 → 0 секунд → 0 ticks
• MPEGTS:180000 → 180000 ticks
• offset = 180000 - 0 = 180000 ticks = 2 секунди ✓

Приклад 2:
• LOCAL:00:00:00.500 → 0.5 секунди → 45000 ticks
• MPEGTS:180000 → 180000 ticks
• offset = 180000 - 45000 = 135000 ticks = 1.5 секунди ✓

Це дозволяє:
• Синхронізувати субтитри з відео навіть якщо вони починаються не з нуля
• Компенсувати затримки у кодуванні/передачі
• Підтримувати seek/перемотування у HLS-плеєрах

У вашому пайплайні: цей offset використовується для конвертації відносних часових міток WebVTT у абсолютні PTS для MPEG-TS сегментів.
```

---

## 🌍 4. `TestCueVoiceSpanRegex` — підтримка міжнародних символів у голосових мітках

### 🔧 Тестові кейси з різними мовами:

```go
tests := []struct {
    give string  // вхідний рядок з тегом
    want string  // очікуване ім'я голосу
}{
    {
        give: `<v 中文> this is the content</v>`,  // китайська
        want: `中文`,
    },
    {
        give: `<v.abc 中文> this is the content</v>`,  // з класами
        want: `中文`,
    },
    {
        give: `<v.jp 言語の> this is the content</v>`,  // японська
        want: `言語の`,
    },
    {
        give: `<v.ko 언어> this is the content</v>`,  // корейська
        want: `언어`,
    },
    {
        give: `<v foo bar> this is the content</v>`,  // латиниця з пробілами
        want: `foo bar`,
    },
    {
        give: `<v هذا عربي> this is the content</v>`,  // арабська
        want: `هذا عربي`,
    },
}
```

### 🔍 Регулярний вираз `webVTTRegexpTag`:

```go
// Ймовірна реалізація (спрощено):
var webVTTRegexpTag = regexp.MustCompile(`^<v(?:\.[\w.-]+)*\s+([^>]+)>(.*)$`)

// Розбір:
// ^<v                    → початок з <v
// (?:\.[\w.-]+)*         → опціональні класи (.abc.def)
// \s+                    → один або більше пробілів
// ([^>]+)                → група 1: ім'я голосу (будь-які символи до >)
// >                      → закриваюча дужка тегу
// (.*)                   → група 2: текст після тегу
```

### 🎯 Чому підтримка міжнародних символів критична для CCTV?

```
Сучасні системи відеоспостереження працюють у глобальному масштабі:
• Китайські камери → субтитри китайською
• Японські системи → субтитри японською
• Арабські регіони → субтитри арабською

Без підтримки міжнародних символів:
• Голосові мітки відображаються як "????" або кракозябри
• Неможливість ідентифікувати диктора у мультиязычних записах
• Проблеми з пошуком/індексацією субтитрів

У вашому пайплайні: цей парсер дозволяє коректно обробляти субтитри з будь-яких джерел, забезпечуючи глобальну сумісність системи.
```

---

## 🔤 5. `TestLineWebVTTBytes` — серіалізація структури у WebVTT текст

### 🔧 Тест на конвертацію `Line` → WebVTT рядок:

```go
require.Equal(t, "<t1>1 <t2>2</t2> 3</t1>\n", string(Line{Items: []LineItem{
    {
        InlineStyle: &StyleAttributes{WebVTTTags: []WebVTTTag{
            {Name: "t1"},  // відкриття <t1>
        }},
        Text: "1 ",  // текст з пробілом
    },
    {
        InlineStyle: &StyleAttributes{WebVTTTags: []WebVTTTag{
            {Name: "t1"},  // продовження <t1>
            {Name: "t2"},  // вкладення <t2>
        }},
        Text: "2",  // текст без пробілів
    },
    {
        InlineStyle: &StyleAttributes{WebVTTTags: []WebVTTTag{
            {Name: "t1"},  // закриття <t2>, але <t1> ще відкритий
        }},
        Text: " 3",  // текст з початковим пробілом
    },
}}.webVTTBytes()))
```

### 🎯 Чому серіалізація складніша за парсинг?

```
Парсинг: текст → структура (одностороннє перетворення)
Серіалізація: структура → текст (збереження семантики тегів)

Проблеми серіалізації:
1. Вкладеність тегів: <t1><t2>текст</t2></t1>
2. Оптимізація: не закривати теги, якщо вони продовжуються у наступному елементі
3. Форматування: пробіли, переноси рядків, уникнення зайвих тегів

У тесті:
• Елемент 1: відкриття <t1>, текст "1 "
• Елемент 2: продовження <t1>, вкладення <t2>, текст "2"
• Елемент 3: закриття <t2> (автоматично), продовження <t1>, текст " 3"
• Результат: <t1>1 <t2>2</t2> 3</t1>

Це демонструє:
• Правильне управління стеком тегів
• Оптимізацію: </t2> вставляється тільки коли потрібно
• Збереження форматування тексту (пробіли)
```

---

## 🐞 6. Потенційні проблеми та покращення тестів

### ❗ Критичні недоліки:

1. **Відсутність тестів на продуктивність парсингу**:
   ```go
   // Усі тести використовують короткі рядки (<100 символів)
   // Але реальні субтитри можуть містити тисячі символів з багатьма тегами
   
   // Додати тест на продуктивність:
   func TestParseTextWebVTT_Performance(t *testing.T) {
       // Згенерувати рядок з 1000 вкладеними тегами
       testData := generateNestedTags(1000)
       
       start := time.Now()
       s := parseTextWebVTT(testData, &StyleAttributes{})
       duration := time.Since(start)
       
       assert.Less(t, duration, 100*time.Millisecond)  // має бути швидко
       assert.NotNil(t, s)  // і результат має бути валідним
   }
   ```

2. **Не тестується обробка помилкових таймштампів**:
   ```go
   // Що станеться, якщо таймштамп невалідний?
   // <00:01:61.000> → 61 секунда не існує
   // <00:01:01.1234> → 4 цифри мілісекунд замість 3
   
   // Додати тест:
   t.Run("When inline timestamp is invalid", func(t *testing.T) {
       testData := `<00:01:61.000>Invalid timestamp`
       s := parseTextWebVTT(testData, &StyleAttributes{})
       // Очікуємо: або помилка, або ігнорування таймштампу
       assert.Equal(t, "Invalid timestamp", s.Items[0].Text)
       assert.Equal(t, time.Duration(0), s.Items[0].StartAt)  // таймштамп ігноровано
   })
   ```

3. **Відсутність тестів на екранування спеціальних символів**:
   ```go
   // WebVTT вимагає екранування <, >, & у тексті
   // Чи обробляє парсер це коректно?
   
   t.Run("When text contains special characters", func(t *testing.T) {
       testData := `Text with &lt;angle brackets&gt; and &amp; ampersand`
       s := parseTextWebVTT(testData, &StyleAttributes{})
       assert.Equal(t, "Text with <angle brackets> and & ampersand", s.Items[0].Text)
   })
   ```

4. **Жорсткі перевірки у `TestCueVoiceSpanRegex`**:
   ```go
   assert.True(t, len(results) == 5)  // очікуємо рівно 5 груп захоплення
   // Але якщо регулярний вираз зміниться, тест зламається без пояснення
   
   // Краще: перевіряти конкретні групи за індексом з описом
   assert.Equal(t, tt.want, results[4], "voice name should be extracted correctly")
   ```

5. **Відсутність тестів на порожні/нульові значення**:
   ```go
   // Що станеться, якщо передати порожній рядок?
   // Чи поверне парсер помилку, чи порожню структуру?
   
   t.Run("When input is empty", func(t *testing.T) {
       s := parseTextWebVTT("", &StyleAttributes{})
       assert.Equal(t, 0, len(s.Items))  // має бути порожній результат
       assert.Equal(t, "", s.VoiceName)  // без голосової мітки
   })
   ```

### 💡 Покращення:

```go
// 1. Helper для генерації складних тестових даних
func generateNestedTags(depth int) string {
    var sb strings.Builder
    for i := 0; i < depth; i++ {
        sb.WriteString(fmt.Sprintf("<t%d>", i))
    }
    sb.WriteString("content")
    for i := depth - 1; i >= 0; i-- {
        sb.WriteString(fmt.Sprintf("</t%d>", i))
    }
    return sb.String()
}

// 2. Семантичні перевірки замість жорстких порівнянь
func assertVoiceNameExtracted(t *testing.T, input, expectedName string) {
    results := webVTTRegexpTag.FindStringSubmatch(input)
    require.NotNil(t, results, "regex should match voice tag")
    require.GreaterOrEqual(t, len(results), 5, "should have at least 5 capture groups")
    assert.Equal(t, expectedName, results[4], "voice name should be extracted correctly")
}

// 3. Тест на стійкість до невалідних даних
func TestParseTextWebVTT_Robustness(t *testing.T) {
    invalidInputs := []string{
        "",                    // порожній рядок
        "<v>",                 // неповний тег голосу
        "<00:01:01>",          // неповний таймштамп
        "<v Bob><00:01:01.000", // змішані теги без тексту
        "<v Bob>Text</v><00:01:01.000>", // таймштамп після закриття
    }
    
    for _, input := range invalidInputs {
        t.Run(input, func(t *testing.T) {
            // Не повинно панікувати
            defer func() {
                if r := recover(); r != nil {
                    t.Errorf("panic on input %q: %v", input, r)
                }
            }()
            s := parseTextWebVTT(input, &StyleAttributes{})
            // Результат може бути порожнім, але не повинен викликати помилку
            _ = s
        })
    }
}

// 4. Тест на конвертацію структури → текст (roundtrip)
func TestLineWebVTTBytes_Roundtrip(t *testing.T) {
    original := "Text with <i>italic</i> and <b>bold</b>"
    
    // Парсинг у структуру
    parsed := parseTextWebVTT(original, &StyleAttributes{})
    
    // Серіалізація назад у текст
    output := parsed.webVTTBytes()
    
    // Порівняння (може відрізнятися форматуванням, але семантика має зберегтись)
    assert.Contains(t, string(output), "italic")
    assert.Contains(t, string(output), "bold")
    // Повне порівняння може не спрацювати через оптимізації тегів
}
```

---

## 🎯 7. Інтеграція з вашим CCTV HLS Processor

### 📍 У `SubtitleParser` — обробка вхідних субтитрів:

```go
type SubtitleParser struct {
    timestampMap *astisub.WebVTTTimestampMap  // для конвертації часу
}

func (p *SubtitleParser) ParseWebVTTLine(line string) (*SubtitleFrame, error) {
    // 1. Парсинг тексту з тегами
    parsed := parseTextWebVTT(line, &astisub.StyleAttributes{})
    
    // 2. Екстракція базової інформації
    frame := &SubtitleFrame{
        Text:     extractTextFromItems(parsed.Items),
        Voice:    parsed.VoiceName,
        Styles:   convertStyles(parsed.InlineStyle),
    }
    
    // 3. Обробка інлайн-таймштампів (якщо є)
    if hasInlineTimestamps(parsed.Items) {
        frame.TimedWords = extractTimedWords(parsed.Items)
    }
    
    // 4. Конвертація відносного часу у абсолютний PTS (якщо є timestampMap)
    if p.timestampMap != nil && parsed.StartAt != 0 {
        frame.PTS = convertWebVTTPtsToMPEG(parsed.StartAt, p.timestampMap)
    }
    
    return frame, nil
}
```

### 📍 У `HLSSubtitleGenerator` — генерація WebVTT для плейлиста:

```go
func (gen *HLSSubtitleGenerator) WriteWebVTTSegment(frames []*SubtitleFrame, outputPath string) error {
    // 1. Створення структури astisub.Subtitles
    subs := &astisub.Subtitles{
        Meta &astisub.Metadata{
            WebVTTTimestampMap: gen.timestampMap,  // для синхронізації з відео
        },
    }
    
    // 2. Конвертація внутрішніх кадрів у формат astisub
    for _, frame := range frames {
        // Конвертація абсолютного PTS → відносний час WebVTT
        relativeTime := convertMPEGPtsToWebVTT(frame.PTS, gen.timestampMap)
        
        // Створення елементу з підтримкою стилів
        item := &astisub.Item{
            StartAt: relativeTime,
            EndAt: relativeTime + frame.Duration,
            Lines: []*astisub.Line{
                {
                    Items: []*astisub.LineItem{
                        {
                            Text: frame.Text,
                            InlineStyle: convertStylesToWebVTT(frame.Styles),
                        },
                    },
                },
            },
        }
        
        // Додавання голосової мітки, якщо є
        if frame.Voice != "" {
            item.Lines[0].Items[0].VoiceName = frame.Voice
        }
        
        subs.Items = append(subs.Items, item)
    }
    
    // 3. Запис у файл
    file, err := os.Create(outputPath)
    if err != nil {
        return fmt.Errorf("create WebVTT file: %w", err)
    }
    defer file.Close()
    
    return subs.WriteToWebVTT(file)
}
```

### 📍 У метриках — моніторинг якості парсингу:

```go
func (p *SubtitleParser) recordParseMetrics(line string, err error) {
    if err != nil {
        metrics.SubtitleParseErrors.WithLabelValues("WebVTT", err.Error()).Inc()
        return
    }
    
    metrics.SubtitleLinesParsed.WithLabelValues("WebVTT").Inc()
    
    // Статистика по функціональності
    if containsVoiceTag(line) {
        metrics.SubtitleFeaturesUsed.WithLabelValues("voice").Inc()
    }
    if containsInlineTimestamp(line) {
        metrics.SubtitleFeaturesUsed.WithLabelValues("inline-timestamp").Inc()
    }
    if containsInternationalChars(line) {
        metrics.SubtitleFeaturesUsed.WithLabelValues("international").Inc()
    }
    
    // Розмір тексту (для оптимізації буферів)
    metrics.SubtitleTextSize.Observe(float64(len(line)))
}
```

---

## 🧭 Висновок: чому ці тести — гарантія надійності парсингу

| Компонент | Роль у WebVTT парсері | Вартість помилки без нього |
|-----------|---------------------|---------------------------|
| **Тести на голосові мітки** | Коректна екстракція імен дикторів | Втрата інформації про мовця → неможливість ідентифікації у мультиязычних записах |
| **Тести на інлайн-таймштампи** | Точна синхронізація тексту з відео | Розсинхронізація субтитрів → "не в попадання губ" у навчальних/тренувальних відео |
| **Тести на X-TIMESTAMP-MAP** | Конвертація між відносним та абсолютним часом | Неможливість інтеграції з MPEG-TS/HLS → субтитри не синхронізуються з відео |
| **Тести на міжнародні символи** | Глобальна сумісність з різними мовами | Кракозябри замість тексту → нечитабельні субтитри для міжнародних користувачів |
| **Тести на серіалізацію** | Коректний експорт у валідний WebVTT | Невалідні файли → відмова плеєрів відтворювати субтитри |

> 🔑 **Головна ідея**: Ці тести — **страховка від регресій** при зміні низькорівневої логіки парсингу. Вони документують:
> 1. Як обробляються крайні випадки (неповні теги, невалідні таймштампи)
> 2. Як конвертуються часові мітки між різними форматами
> 3. Як підтримуються міжнародні символи без втрати даних
> 4. Як зберігається семантика тегів при серіалізації

Без них будь-яка оптимізація парсера може непомітно зламати сумісність з існуючими субтитрами — і ви дізнаєтесь про це тільки коли субтитри з певних камер перестануть відтворюватись коректно.

💡 **Фінальна порада**: 
1. Додайте тести на продуктивність для великих/складних рядків
2. Покрийте обробку невалідних таймштампів та спеціальних символів
3. Замініть жорсткі перевірки на семантичні з кращими повідомленнями про помилки
4. Додайте roundtrip тести для перевірки парсинг → серіалізація → парсинг
5. Реалізуйте fuzz-тести для перевірки стійкості до випадкових/пошкоджених даних

Це перетворить ці тести з "перевірки базової функціональності" на "гарантію надійності парсингу" для всього вашого пайплайну обробки субтитрів у CCTV HLS Processor.