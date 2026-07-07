# 🧪 Глибокий розбір `astisub_test/webvtt_test.go` — тестування парсингу WebVTT субтитрів

Це **комплексний набір інтеграційних тестів** для валідації парсингу формату WebVTT (Web Video Text Tracks) у бібліотеці `astisub`. Тести покривають критичні шляхи: базовий парсинг, обробку метаданих, inline-тегів, голосових міток, конвертацію кольорів та конвертацію у TTML. Розберемо архітектурно:

---

## 📦 1. Архітектура тестування: table-driven + assertion-based

### 🔧 Використання testify для читабельних перевірок:

```go
import (
    "github.com/stretchr/testify/assert"    // продовжує тест після помилки
    "github.com/stretchr/testify/require"   // зупиняє тест при помилці
)

// Приклад:
assert.NoError(t, err)      // логує помилку, але продовжує тест
require.NoError(t, err)     // викликає t.FailNow() при помилці
```

### 🎯 Чому два типи assertions?

| Тип | Коли використовувати | Приклад у коді |
|-----|---------------------|---------------|
| `assert` | Для перевірок, де помилка не блокує подальші тести | `assert.Equal(t, expected, actual)` для метаданих |
| `require` | Для критичних перевірок, без яких тест не має сенсу | `require.NoError(t, err)` після парсингу |

> 💡 **Практичне значення**: Це дозволяє отримати більше інформації про помилки за один запуск тесту, але зупинятися при фатальних збоях (наприклад, неможливість відкрити файл).

---

## 🔍 2. `TestWebVTT` — базовий парсинг з валідацією всіх компонентів

### 🔧 Структура тесту:

```go
func TestWebVTT(t *testing.T) {
    // 1. Читання файлу через універсальний інтерфейс
    s, err := astisub.OpenFile("./testdata/example-in.vtt")
    assert.NoError(t, err)
    assertSubtitleItems(t, s)  // ← helper-функція для спільних перевірок
    
    // 2. Перевірка коментарів (NOTE блоки)
    assert.Equal(t, []string{"this a nice example", "of a VTT"}, s.Items[0].Comments)
    
    // 3. Перевірка регіонів (WebVTT Regions для позиціонування)
    assert.Equal(t, 2, len(s.Regions))
    assert.Equal(t, astisub.Region{
        ID: "fred",
        InlineStyle: &astisub.StyleAttributes{
            WebVTTLines: 3,
            WebVTTRegionAnchor: "0%,100%",
            WebVTTScroll: "up",
            WebVTTViewportAnchor: "10%,90%",
            WebVTTWidth: "40%",
        },
    }, *s.Regions["fred"])
    
    // 4. Перевірка стилів з автоматичною конвертацією WebVTT → TTML
    expected := astisub.StyleAttributes{
        WebVTTAlign: "left",
        WebVTTPosition: &astisub.WebVTTPosition{XPosition: "10%", Alignment: "line-left"},
        WebVTTSize: "35%",
        // propagateWebVTTAttributes() додає TTML-еквіваленти:
        TTMLOrigin: astikit.StrPtr("10% 80%"),   // X=10%, Y=80% (default)
        TTMLExtent: astikit.StrPtr("35% 10%"),   // width=35%, height=10% (default)
        TTMLTextAlign: astikit.StrPtr("left"),   // з WebVTTAlign
    }
    assert.Equal(t, expected, *s.Items[1].InlineStyle)
    
    // 5. Тест запису: roundtrip перевірка
    w := &bytes.Buffer{}
    err = s.WriteToWebVTT(w)
    assert.NoError(t, err)
    
    expectedContent, _ := ioutil.ReadFile("./testdata/example-out.vtt")
    assert.Equal(t, string(expectedContent), w.String())  // ← порівняння байт-в-байт
}
```

### 🎯 Чому `propagateWebVTTAttributes()` критична?

```
WebVTT і TTML використовують різні моделі стилів:

WebVTT:
• position: 10%,line-left → X=10%, вирівнювання по лівому краю лінії
• size: 35% → ширина 35%, висота за замовчуванням
• align: left → текстовий вирівнювання

TTML:
• origin: "10% 80%" → позиція контейнера (X Y)
• extent: "35% 10%" → розмір контейнера (width height)
• textAlign: "left" → вирівнювання тексту

propagateWebVTTAttributes() автоматично конвертує:
1. WebVTT position → TTML origin + textAlign
2. WebVTT size → TTML extent
3. WebVTT align → TTML textAlign

Без цієї конвертації:
• Експорт у TTML втратив би інформацію про позиціонування
• Конвертація між форматами була б неможливою без ручного мапінгу
```

---

## 🔧 3. `TestWebVTTWithVoiceName` — парсинг голосових міток `<v Name>`

### 🔍 Специфікація WebVTT voice spans:

```
Формат: <v[.class1.class2] Name>текст</v>
• <v Roger Bingham> — проста голосова мітка
• <v.first.local Roger> — з класами для CSS-стилізації
• </v> — закриваючий тег (опціональний, якщо текст до кінця рядка)
```

### 🔧 Тестові кейси:

```go
testData := `WEBVTT

1
00:02:34.000 --> 00:02:35.000
<v.first.local Roger Bingham>I'm the fist speaker  // ← класи + ім'я

2
00:02:34.000 --> 00:02:35.000
<v Bingham>I'm the second speaker  // ← тільки ім'я

3
00:00:04.000 --> 00:00:08.000
<v Lee>What are you doing here?</v>  // ← з закриваючим тегом

4
00:00:04.000 --> 00:00:08.000
<v Bob>Incorrect tag?</vi>  // ← помилковий закриваючий тег`

s, err := astisub.ReadFromWebVTT(strings.NewReader(testData))
assert.NoError(t, err)

// Перевірка: VoiceName витягується незалежно від класів/тегів
assert.Equal(t, "Roger Bingham", s.Items[0].Lines[0].VoiceName)
assert.Equal(t, "Bingham", s.Items[1].Lines[0].VoiceName)
assert.Equal(t, "Lee", s.Items[2].Lines[0].VoiceName)
assert.Equal(t, "Bob", s.Items[3].Lines[0].VoiceName)  // ← ігнорує </vi>

// Roundtrip: запис повинен нормалізувати теги
err = s.WriteToWebVTT(b)
assert.Equal(t, `<v Roger Bingham>I'm the fist speaker`, output)  // ← без класів
assert.Equal(t, `<v Bob>Incorrect tag?`, output)  // ← виправляє </vi> → закриває автоматично
```

### 🎯 Чому ігноруються класи при записі?

```
WebVTT класи (.first.local) призначені для CSS-стилізації у браузерах.
Бібліотека astisub фокусується на семантичних даних (текст, час, голос),
а не на презентаційних деталях.

Рішення:
• При читанні: зберігати VoiceName, ігнорувати класи
• При записі: генерувати мінімальний валідний `<v Name>` тег

Це компроміс між повнотою даних та сумісністю з іншими парсерами.
```

---

## ⏱️ 4. `TestWebVTTWithTimestampMap` — конвертація часів для MPEG-TS

### 🔍 Метадані X-TIMESTAMP-MAP:

```
WEBVTT
X-TIMESTAMP-MAP=MPEGTS:180000, LOCAL:00:00:00.000

Це мапує:
• LOCAL:00:00:00.000 → WebVTT час (відносний)
• MPEGTS:180000 → абсолютний час у транспортному потоці (90 kHz clock)

Формула конвертації:
PTS_MPEG = (PTS_WebVTT_у_секундах × 90000) + MPEGTS_offset

Приклад:
• WebVTT: 00:00:00.933 → 933 ms
• MPEGTS offset: 180000 ticks @ 90 kHz = 2 секунди
• Результат: 933 ms + 2000 ms = 2933 ms → 263970 ticks @ 90 kHz
```

### 🔧 Тестова валідація:

```go
testData := `WEBVTT
X-TIMESTAMP-MAP=MPEGTS:180000, LOCAL:00:00:00.000

00:00.933 --> 00:02.366
♪ ♪`

s, err := astisub.ReadFromWebVTT(strings.NewReader(testData))
assert.NoError(t, err)

// Перевірка парсингу часових міток
assert.Equal(t, int64(933), s.Items[0].StartAt.Milliseconds())
assert.Equal(t, int64(2366), s.Items[0].EndAt.Milliseconds())

// Перевірка метаданих мапування
assert.Equal(t, time.Duration(time.Second*2), s.Metadata.WebVTTTimestampMap.Offset())
// Offset = MPEGTS / 90000 = 180000 / 90000 = 2 секунди ✓

// Roundtrip: запис повинен нормалізувати порядок параметрів
err = s.WriteToWebVTT(b)
assert.Equal(t, `X-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:180000`, output)
// Замість "MPEGTS:..., LOCAL:..." → "LOCAL:..., MPEGTS:..." (канонічний порядок)
```

### 🎯 Чому це критично для HLS?

```
HLS використовує MPEG-TS сегменти з абсолютними часовими мітками (PTS/DTS).
WebVTT субтитри мають відносні часові мітки (від початку файлу).

Без X-TIMESTAMP-MAP:
• Неможливо синхронізувати субтитри з відео у транспортному потоці
• Субтитри можуть з'являтись на 2 секунди раніше/пізніше

З X-TIMESTAMP-MAP:
• Парсер обчислює абсолютні PTS для кожного субтитру
• Синхронізація з відео гарантована навіть при seek/перемотуванні

У вашому CCTV HLS Processor: цей механізм дозволяє імпортувати WebVTT субтитри
з камер/серверів та інтегрувати їх у HLS-плейлисти з коректною синхронізацією.
```

---

## 🎨 5. `TestWebVTTTags` — парсинг inline-тегів з різними сценаріями

### 🔧 Тестові кейси та очікувана поведінка:

```go
testData := `WEBVTT
// 1. Вкладені теги
00:01:00.000 --> 00:02:00.000
<u><i>Italic with underline text</i></u> some extra

// 2. Кольори + мова
00:02:00.000 --> 00:03:00.000
<lang en>English here</lang> <c.yellow.bg_blue>Yellow text on blue background</c>

// 3. Голос + колір + вкладеність
00:03:00.000 --> 00:04:00.000
<v Joe><c.red><i>Joe's words are red in italic</i></c>

// 4. Кастомні теги (не стандартні, але допустимі)
00:04:00.000 --> 00:05:00.000
<customed_tag.class1.class2>Text here</customed_tag>

// 5. Кілька голосових міток в одному рядку
00:05:00.000 --> 00:06:00.000
<v Joe>Joe says something</v> <v Bob>Bob says something</v>

// 6. Timestamp всередині тексту (не як таймінг!)
00:06:00.000 --> 00:07:00.000
Text with a <00:06:30.000>timestamp in the middle

// 7. Багаторядкові теги
00:08:00.000 --> 00:09:00.000
<i>Test with multi line italics
Terminated on the next line</i>

// 8. Незакриті теги (мають бути закриті автоматично)
00:09:00.000 --> 00:10:00.000
<i>Unterminated styles

// 9. Теги, що не повинні "витікати" у наступний субтитр
00:10:00.000 --> 00:11:00.000
Do no fall to the next item

// 10. Теги з математичними символами
00:12:00.000 --> 00:13:00.000
<i>x</i>^3 * <i>x</i> = 100`
```

### 🎯 Ключові аспекти парсингу:

| Кейс | Очікувана поведінка | Чому це критично |
|------|---------------------|-----------------|
| **Вкладені теги** | `<u><i>текст</i></u>` → зберегти вкладеність у структурі | Браузери відтворюють вкладені стилі коректно |
| **Кольори з фоном** | `<c.yellow.bg_blue>` → окремі поля для fg/bg кольору | Конвертація у TTML вимагає розділення кольорів |
| **Кілька `<v>` в рядку** | Кожен `<v>` створює окремий елемент з власним VoiceName | Підтримка діалогів у одному субтитрі |
| **Таймінг у тексті** | `<00:06:30.000>` не парситься як таймінг, а як текст | Уникнення хибних спрацьовувань на часових мітках у контенті |
| **Незакриті теги** | Автоматичне закриття на межі рядка/субтитру | Сумісність з "брудними" WebVTT файлами від реальних камер |
| **Теги між субтитрами** | Стилі не "витікають" у наступний субтитр | Ізоляція стилів для кожного таймінг-блоку |

### 🔧 Очікуваний вихід після нормалізації:

```webvtt
// Кейс 5: кілька <v> → об'єднання без закриваючих тегів
00:05:00.000 --> 00:06:00.000
<v Joe>Joe says something Bob says something
// ← Закриваючі </v> видалені, бо вони опціональні у WebVTT

// Кейс 7: багаторядковий тег → розбиття на два <i>
00:08:00.000 --> 00:09:00.000
<i>Test with multi line italics</i>
<i>Terminated on the next line</i>
// ← Кожен рядок у власному <i> для сумісності з парсерами

// Кейс 8: незакритий тег → автоматичне закриття
00:09:00.000 --> 00:10:00.000
<i>Unterminated styles</i>
// ← Додано </i> наприкінці
```

---

## 🎨 6. `TestWebVTTColorToTTML` — конвертація кольорів у TTML-сумісні значення

### 🔍 Мапінг WebVTT кольорів → астисуб.Color:

```go
// Внутрішня таблиця webVTTColorMap (спрощено):
var webVTTColorMap = map[string]Color{
    "red":    ColorRed,      // #FF0000
    "blue":   ColorBlue,     // #0000FF
    "green":  ColorGreen,    // #008000
    "yellow": ColorYellow,   // #FFFF00
    "cyan":   ColorCyan,     // #00FFFF
    "magenta": ColorMagenta, // #FF00FF
    // "orange" відсутній → невідомий колір
}
```

### 🔧 Тестові перевірки:

```go
testData := `WEBVTT
1
00:00:01.000 --> 00:00:03.000
<c.red>Red text</c> and <c.blue.bg_yellow>blue text on yellow background</c>

2
00:00:04.000 --> 00:00:06.000
<c.green>Green text</c> with <c.bg_cyan>text on cyan background</c>

3
00:00:07.000 --> 00:00:09.000
Normal text with <c.magenta>magenta</c> and <c.orange>unknown color</c>`

s, err := astisub.ReadFromWebVTT(strings.NewReader(testData))
require.NoError(t, err)

// Перевірка червоного тексту (тільки foreground)
redItem := s.Items[0].Lines[0].Items[0]
assert.Equal(t, "Red text", redItem.Text)
assert.Equal(t, astisub.ColorRed, redItem.InlineStyle.TTMLColor)
assert.Nil(t, redItem.InlineStyle.TTMLBackgroundColor)  // ← немає bg

// Перевірка синього на жовтому (foreground + background)
blueYellowItem := s.Items[0].Lines[0].Items[2]
assert.Equal(t, astisub.ColorBlue, blueYellowItem.InlineStyle.TTMLColor)
assert.Equal(t, astisub.ColorYellow, blueYellowItem.InlineStyle.TTMLBackgroundColor)

// Перевірка тільки фону (без foreground)
cyanBgItem := s.Items[1].Lines[0].Items[2]
assert.Nil(t, cyanBgItem.InlineStyle.TTMLColor)  // ← немає fg
assert.Equal(t, astisub.ColorCyan, cyanBgItem.InlineStyle.TTMLBackgroundColor)

// Перевірка невідомого кольору (не мапиться)
unknownColorItem := s.Items[2].Lines[0].Items[3]
assert.Nil(t, unknownColorItem.InlineStyle.TTMLColor)  // ← "orange" не у мапі
```

### 🎯 Чому невідомі кольори ігноруються?

```
WebVTT дозволяє будь-які назви кольорів (включаючи CSS-імена),
але TTML вимагає конкретні значення (#RRGGBB або іменовані).

Рішення astisub:
• Відомі кольори (red, blue, ...) → мапяться у ColorRed, ColorBlue...
• Невідомі кольори (orange, custom_color) → ігноруються, не встановлюють TTMLColor

Це запобігає:
• Генерації невалідного TTML з невідомими іменами кольорів
• Помилкам у декодерах, що не підтримують розширені CSS-кольори

Компроміс: втрата інформації про невідомі кольори заради валідності виходу.
```

---

## 🐞 7. Потенційні проблеми та покращення тестів

### ❗ Критичні недоліки:

1. **Відсутність тестів на великі файли/продуктивність**:
   ```go
   // Усі тести використовують маленькі рядки або файли <1KB
   // Але реальні субтитри можуть бути 100KB+ з тисячами записів
   
   // Додати тест на продуктивність:
   func TestWebVTT_LargeFile(t *testing.T) {
       // Згенерувати 10,000 субтитрів
       // Виміряти час парсингу: має бути < 100ms
       // Перевірити використання пам'яті: має бути < 50MB
   }
   ```

2. **Не тестується обробка помилок у `propagateWebVTTAttributes()`**:
   ```go
   // Якщо WebVTTPosition має невалідні значення (наприклад, "150%")
   // Чи повертається помилка, чи ігнорується?
   
   // Додати тест:
   func TestWebVTT_InvalidPosition(t *testing.T) {
       testData := `WEBVTT
       00:00:01.000 --> 00:00:02.000
       <c.red position:150%>Invalid position</c>`
       
       s, err := astisub.ReadFromWebVTT(strings.NewReader(testData))
       // Очікуємо: або помилка, або ігнорування невалідного значення
       assert.NoError(t, err)  // або assert.Error(t, err)
   }
   ```

3. **Відсутність fuzz-тестів для стійкості**:
   ```go
   // WebVTT парсер має бути стійким до "брудних" вхідних даних
   // Додати fuzz-тест:
   func FuzzWebVTTParsing(f *testing.F) {
       f.Add("WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello")
       f.Fuzz(func(t *testing.T, data string) {
           _, err := astisub.ReadFromWebVTT(strings.NewReader(data))
           // Не повинно панікувати навіть на випадкових даних
           if err != nil {
               // Помилка допустима, головне — немає panic
           }
       })
   }
   ```

4. **Жорсткі порівняння рядків у roundtrip тестах**:
   ```go
   assert.Equal(t, string(c), w.String())
   // Чутливе до порядку пробілів, переносів рядків, порядку атрибутів
   
   // Краще: парсити вихід і порівнювати семантично
   outputSubs, _ := astisub.ReadFromWebVTT(bytes.NewReader(w.Bytes()))
   assert.Equal(t, s.Items, outputSubs.Items)  // порівняння структур, не тексту
   ```

5. **Відсутність тестів на кодування/декодування спеціальних символів**:
   ```go
   // WebVTT підтримує UTF-8, HTML-ентиті, emoji
   // Додати тест:
   func TestWebVTT_SpecialChars(t *testing.T) {
       testData := `WEBVTT
       00:00:01.000 --> 00:00:02.000
       &lt;script&gt; alert("XSS") &amp; &copy; 2024 🎬`
       
       s, _ := astisub.ReadFromWebVTT(strings.NewReader(testData))
       assert.Equal(t, `<script> alert("XSS") & © 2024 🎬`, s.Items[0].String())
   }
   ```

### 💡 Покращення:

```go
// 1. Helper для генерації великих тестових даних
func generateLargeWebVTT(numItems int) string {
    var sb strings.Builder
    sb.WriteString("WEBVTT\n\n")
    for i := 0; i < numItems; i++ {
        start := time.Duration(i*100) * time.Millisecond
        end := start + 2*time.Second
        fmt.Fprintf(&sb, "%d\n%s --> %s\nSubtitle %d\n\n", 
            i+1, formatDuration(start), formatDuration(end), i+1)
    }
    return sb.String()
}

// 2. Семантичне порівняння замість текстового
func assertSubtitlesEqual(t *testing.T, expected, actual *astisub.Subtitles) {
    assert.Equal(t, expected.Metadata, actual.Metadata)
    assert.Equal(t, len(expected.Items), len(actual.Items))
    for i := range expected.Items {
        assert.Equal(t, expected.Items[i].StartAt, actual.Items[i].StartAt)
        assert.Equal(t, expected.Items[i].EndAt, actual.Items[i].EndAt)
        assert.Equal(t, expected.Items[i].String(), actual.Items[i].String())
        // Порівняння стилів, регіонів, коментарів...
    }
}

// 3. Тест на стійкість до невалідних даних
func TestWebVTT_Robustness(t *testing.T) {
    invalidInputs := []string{
        "",                    // порожній файл
        "WEBVTT",              // тільки заголовок
        "WEBVTT\n\nbad time",  // невалідний таймінг
        "WEBVTT\n\n00:00:01.000 --> 00:00:02.000",  // без тексту
        "WEBVTT\r\n",          // неправильні переноси рядків
    }
    
    for _, input := range invalidInputs {
        t.Run(input, func(t *testing.T) {
            // Не повинно панікувати
            defer func() {
                if r := recover(); r != nil {
                    t.Errorf("panic on input %q: %v", input, r)
                }
            }()
            _, err := astisub.ReadFromWebVTT(strings.NewReader(input))
            // Помилка допустима, головне — стабільність
            _ = err
        })
    }
}
```

---

## 🎯 8. Інтеграція з вашим CCTV HLS Processor

### 📍 У `SubtitleImporter` — імпорт WebVTT субтитрів:

```go
type SubtitleImporter struct {
    subtitles *astisub.Subtitles
}

func (imp *SubtitleImporter) ImportWebVTT(filePath string) error {
    subs, err := astisub.OpenFile(filePath)
    if err != nil {
        return fmt.Errorf("parse WebVTT: %w", err)
    }
    
    // Конвертація у внутрішній формат
    for _, item := range subs.Items {
        // Конвертація часових міток: WebVTT (відносні) → HLS PTS (абсолютні)
        pts := convertWebVTTPtsToHLS(item.StartAt, subs.Metadata.WebVTTTimestampMap)
        
        // Екстракція тексту з inline-тегів
        text := extractTextFromItems(item.Lines)
        
        // Додавання у пайплайн
        imp.subtitleQueue <- SubtitleFrame{
            PTS: pts,
            Text: text,
            Language: detectLanguage(item),  // з <lang> тегів
            Voice: extractVoice(item),       // з <v Name> тегів
        }
    }
    return nil
}
```

### 📍 У `HLSGenerator` — генерація WebVTT для HLS:

```go
func (gen *HLSGenerator) WriteWebVTTPlaylist(subtitles []*SubtitleFrame, outputPath string) error {
    // Конвертація внутрішніх субтитрів у astisub формат
    subs := &astisub.Subtitles{
        Metadata: &astisub.Metadata{
            WebVTTTimestampMap: &astisub.WebVTTTimestampMap{
                // Мапування: HLS PTS → WebVTT відносний час
                Offset: gen.basePTS,  // абсолютний початок сегменту
            },
        },
    }
    
    for _, frame := range subtitles {
        // Конвертація: абсолютний PTS → відносний WebVTT час
        relativeTime := frame.PTS - gen.basePTS
        
        subs.Items = append(subs.Items, &astisub.Item{
            StartAt: relativeTime,
            EndAt: relativeTime + frame.Duration,
            Lines: []*astisub.Line{
                {Items: []*astisub.LineItem{{Text: frame.Text}}},
            },
        })
    }
    
    // Запис у файл
    file, _ := os.Create(outputPath)
    defer file.Close()
    return subs.WriteToWebVTT(file)
}
```

### 📍 У метриках — моніторинг якості парсингу:

```go
func (imp *SubtitleImporter) recordParseMetrics(subs *astisub.Subtitles, err error) {
    if err != nil {
        metrics.SubtitleParseErrors.WithLabelValues("WebVTT", err.Error()).Inc()
        return
    }
    
    metrics.SubtitleItemsParsed.WithLabelValues("WebVTT").Add(float64(len(subs.Items)))
    
    // Статистика по стилях/тегах
    var voiceCount, colorCount, regionCount int
    for _, item := range subs.Items {
        if item.Lines[0].VoiceName != "" { voiceCount++ }
        if hasColorTags(item) { colorCount++ }
        if item.Region != nil { regionCount++ }
    }
    metrics.SubtitleFeaturesUsed.WithLabelValues("voice").Add(float64(voiceCount))
    metrics.SubtitleFeaturesUsed.WithLabelValues("color").Add(float64(colorCount))
    metrics.SubtitleFeaturesUsed.WithLabelValues("region").Add(float64(regionCount))
}
```

---

## 🧭 Висновок: чому ці тести — гарантія надійності парсингу

| Компонент | Роль у WebVTT парсері | Вартість помилки без нього |
|-----------|---------------------|---------------------------|
| **Roundtrip тести** | Гарантія, що парсинг + запис зберігають дані | Втрата субтитрів при конвертації між форматами |
| **Перевірка мапінгу кольорів** | Коректна конвертація WebVTT → TTML | Невалідний TTML → помилки у плеєрах, що використовують TTML |
| **Тести на edge cases** (незакриті теги, таймінги у тексті) | Стійкість до "брудних" реальних даних | Падіння парсера на субтитрах з реальних камер/серверів |
| **Перевірка X-TIMESTAMP-MAP** | Синхронізація з транспортним потоком | Розсинхронізація субтитрів з відео у HLS-плейлистах |
| **Семантичні перевірки стилів** | Збереження презентаційної інформації | Втрата кольорів/позиціонування → субтитри нечитабельні |

> 🔑 **Головна ідея**: Ці тести — **страховка від регресій** при зміні логіки парсингу. Вони документують:
> 1. Як обробляються вкладені теги, голосові мітки, кольори
> 2. Як конвертуються часові мітки між WebVTT та іншими форматами
> 3. Як парсер поводиться з невалідними/неповними даними
> 4. Які компроміси прийняті між повнотою даних та сумісністю

Без них будь-яка оптимізація парсера може непомітно зламати сумісність з існуючими WebVTT файлами — і ви дізнаєтесь про це тільки коли субтитри з певних камер перестануть відтворюватись коректно.

💡 **Фінальна порада**: 
1. Додайте fuzz-тести для перевірки стійкості до випадкових/пошкоджених даних
2. Замініть текстові roundtrip порівняння на семантичні (парсинг виходу + порівняння структур)
3. Додайте тести на спеціальні символи (UTF-8, HTML-ентиті, emoji)
4. Реалізуйте тести продуктивності для великих файлів (>10,000 субтитрів)
5. Додайте перевірки обробки помилок у `propagateWebVTTAttributes()` для невалідних значень

Це перетворить ці тести з "перевірки базової функціональності" на "гарантію надійності парсингу" для всього вашого пайплайну обробки субтитрів у CCTV HLS Processor.