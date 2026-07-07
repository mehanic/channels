# 🧪 Глибокий розбір `astisub_test/ttml_test.go` — тестування парсингу та запису TTML субтитрів

Це **комплексний набір інтеграційних тестів** для валідації парсингу формату TTML (Timed Text Markup Language) у бібліотеці `astisub`. Тести покривають критичні шляхи: базовий парсинг, обробку метаданих, стилів, регіонів, вкладених елементів, та конвертацію у інші формати (WebVTT). Розберемо архітектурно:

---

## 📦 1. Архітектура тестування: assertion-based з глибокими порівняннями

### 🔧 Використання testify для читабельних перевірок:

```go
import (
    "github.com/stretchr/testify/assert"    // продовжує тест після помилки
    "github.com/stretchr/testify/require"   // зупиняє тест при помилці
)

// Приклад глибокого порівняння структур:
assert.Equal(t, astisub.Style{
    ID: "style_0",
    InlineStyle: &astisub.StyleAttributes{
        STLColor: astisub.ColorWhite,
        TTMLColor: astisub.ColorWhite,
        TTMLExtent: astikit.StrPtr("100% 10%"),
        TTMLFontFamily: astikit.StrPtr("sansSerif"),
        // ... багато інших полів ...
    },
}, *s.Styles["style_0"])
```

### 🎯 Чому глибокі порівняння критичні для TTML?

```
TTML — складний XML-формат з:
• Вкладеністю елементів: <region><style><p><span>текст</span></p></style></region>
• Наслідуванням стилів: регіон → стиль → рядок → фрагмент тексту
• Конвертацією між форматами: TTML ↔ WebVTT ↔ STL

Глибокі порівняння дозволяють:
1. Перевірити правильність наслідування стилів на всіх рівнях
2. Валідувати конвертацію атрибутів між форматами (TTMLColor ↔ WebVTTAlign)
3. Забезпечити детермінованість серіалізації (roundtrip тести)

Без цього: помилки у наслідуванні стилів можуть залишитись непоміченими,
що призведе до неправильного відображення субтитрів у плеєрах.
```

---

## 🔍 2. `TestTTML` — базовий парсинг з валідацією всіх компонентів

### 🔧 Структура тесту:

```go
func TestTTML(t *testing.T) {
    // 1. Читання файлу через універсальний інтерфейс
    s, err := astisub.OpenFile("./testdata/example-in.ttml")
    assert.NoError(t, err)
    assertSubtitleItems(t, s)  // ← helper-функція для спільних перевірок
    
    // 2. Перевірка метаданих
    assert.Equal(t, &astisub.Metadata{
        Framerate: 25,                    // частота кадрів для синхронізації
        Language: astisub.LanguageFrench, // мова субтитрів
        Title: "Title test",              // заголовок
        TTMLCopyright: "Copyright test",  // інформація про авторські права
    }, s.Metadata)
    
    // 3. Перевірка стилів з наслідуванням атрибутів
    assert.Equal(t, 3, len(s.Styles))
    assert.Equal(t, astisub.Style{
        ID: "style_0",
        InlineStyle: &astisub.StyleAttributes{
            // Конвертовані атрибути:
            STLColor: astisub.ColorWhite,      // з STL формату
            TTMLColor: astisub.ColorWhite,      // нативний TTML колір
            TTMLExtent: astikit.StrPtr("100% 10%"),  // розмір області
            TTMLOrigin: astikit.StrPtr("0% 90%"),    // позиція області
            TTMLTextAlign: astikit.StrPtr("center"), // вирівнювання тексту
            // Конвертовані у WebVTT еквіваленти:
            WebVTTAlign: "center",              // з TTMLTextAlign
            WebVTTPosition: &astisub.WebVTTPosition{XPosition: "90%"}, // з TTMLOrigin
            WebVTTSize: "10%",                  // з TTMLExtent
            WebVTTLines: 2,                     // розраховано з Extent/Origin
            // ... інші атрибути ...
        },
    }, *s.Styles["style_0"])
    
    // 4. Перевірка регіонів з посиланнями на стилі
    assert.Equal(t, 3, len(s.Regions))
    assert.Equal(t, astisub.Region{
        ID: "region_0",
        Style: s.Styles["style_0"],  // ← посилання на стиль, не копія!
        InlineStyle: &astisub.StyleAttributes{
            STLColor: astisub.ColorBlue,  // регіон може перевизначати атрибути
            TTMLColor: astisub.ColorBlue,
        },
    }, *s.Regions["region_0"])
    
    // 5. Перевірка елементів субтитрів з вкладеністю
    assert.Equal(t, []astisub.Line{{
        Items: []astisub.LineItem{{
            Style: s.Styles["style_1"],           // наслідування стилю
            InlineStyle: &astisub.StyleAttributes{ // перевизначення на рівні фрагменту
                STLColor: astisub.ColorBlack,
                TTMLColor: astisub.ColorBlack,
            },
            Text: "(deep rumbling)",
        }},
    }}, s.Items[0].Lines)
    
    // 6. Roundtrip тест: запис → читання → порівняння
    w := &bytes.Buffer{}
    err = s.WriteToTTML(w)
    assert.NoError(t, err)
    
    expectedContent, _ := ioutil.ReadFile("./testdata/example-out.ttml")
    assert.Equal(t, string(expectedContent), w.String())  // ← байт-в-байт порівняння
}
```

### 🎯 Чому `propagateWebVTTAttributes()` критична для стилів?

```
TTML і WebVTT використовують різні моделі стилів:

TTML:
• origin: "0% 90%" → позиція контейнера (X Y)
• extent: "100% 10%" → розмір контейнера (width height)
• textAlign: "center" → вирівнювання тексту

WebVTT:
• position: 90%,line-left → X=90%, вирівнювання по лівому краю лінії
• size: 10% → ширина 10%, висота за замовчуванням
• align: center → текстовий вирівнювання

propagateWebVTTAttributes() автоматично конвертує:
1. TTMLOrigin → WebVTTPosition + WebVTTLine
   • "0% 90%" → XPosition="90%", Alignment="line-left" (за замовчуванням)
2. TTMLExtent → WebVTTSize + WebVTTLines
   • "100% 10%" → Size="10%", Lines=2 (розраховується з висоти)
3. TTMLTextAlign → WebVTTAlign
   • "center" → "center" (пряме мапування)

Без цієї конвертації:
• Експорт у WebVTT втратив би інформацію про позиціонування
• Конвертація між форматами була б неможливою без ручного мапінгу
• Стилі регіонів не наслідувались би правильно у вкладених елементах
```

---

## 🔀 3. `TestTTMLBreakLines` — обробка переносів рядків у TTML

### 🔧 Специфіка формату:

```xml
<!-- Приклад TTML з переносами рядків -->
<p begin="00:00:01.000" end="00:00:03.000">
    <span>Перший рядок</span><br/>
    <span>Другий рядок</span>
</p>
```

### 🔧 Тестова валідація:

```go
func TestTTMLBreakLines(t *testing.T) {
    // Читання файлу з переносами рядків
    s, err := astisub.OpenFile("./testdata/example-in-breaklines.ttml")
    assert.NoError(t, err)
    
    // Запис у буфер
    w := &bytes.Buffer{}
    err = s.WriteToTTML(w)
    assert.NoError(t, err)
    
    // Порівняння з очікуваним виходом
    c, _ := ioutil.ReadFile("./testdata/example-out-breaklines.ttml")
    // ← Важливо: strings.TrimSpace для ігнорування відмінностей у пробілах/переносах
    assert.Equal(t, strings.TrimSpace(string(c)), strings.TrimSpace(w.String()))
}
```

### 🎯 Чому `strings.TrimSpace` критичний для XML-форматів?

```
XML/TTML чутливі до:
• Пробілів між тегами: <p>текст</p> vs <p> текст </p>
• Переносів рядків: один рядок vs кілька рядків для читабельності
• Табуляцій для відступів: для людиночитабельності, але не для парсингу

Проблема порівняння:
• Генератор може додавати відступи для читабельності
• Парсер може ігнорувати зайві пробіли
• Результат семантично ідентичний, але байтово відрізняється

Рішення:
• Порівнювати після trim: ігнорувати провідні/завершальні пробіли
• Або: парсити обидва XML і порівнювати структури, не текст

У вашому тесті: trim дозволяє фокусуватись на семантиці, а не форматуванні.
```

---

## ⚙️ 4. `TestWriteToTTMLWithIndentOption` — контроль форматування виходу

### 🔧 Опція індентації:

```go
func TestWriteToTTMLWithIndentOption(t *testing.T) {
    s, _ := astisub.OpenFile("./testdata/example-in.ttml")
    
    w := &bytes.Buffer{}
    // Запис без індентації: "" → компактний вихід без відступів
    err = s.WriteToTTML(w, astisub.WriteToTTMLWithIndentOption(""))
    assert.NoError(t, err)
    
    c, _ := ioutil.ReadFile("./testdata/example-out-no-indent.ttml")
    assert.Equal(t, strings.TrimSpace(string(c)), strings.TrimSpace(w.String()))
}
```

### 🎯 Чому контроль індентації важливий?

```
Сценарії використання:
1. Продакшен: компактний вихід без відступів → менший розмір файлу, швидша передача
2. Дебаг/розробка: відформатований вихід з відступами → легше читати та редагувати
3. Контроль версій: детерміноване форматування → менше змін у git diff при зміні логіки

У вашому CCTV HLS Processor:
• Для мережевої передачі субтитрів: використовувати компактний формат
• Для логування/дебагу: використовувати відформатований вихід
• Для тестів: порівнювати після trim, щоб ігнорувати відмінності у форматуванні
```

---

## 🔗 5. `TestTTMLMergeStyleAttributes` — наслідування та злиття стилів

### 🔧 Специфіка наслідування у TTML:

```
TTML підтримує багаторівневе наслідування стилів:
1. Регіон має стиль → всі субтитри у регіоні наслідують його атрибути
2. Субтитр має стиль → перевизначає атрибути регіону
3. Рядок має inline-стиль → перевизначає атрибути субтитру
4. Фрагмент тексту має стиль → перевизначає атрибути рядка

Приклад ієрархії:
<region style="region_style">          <!-- рівень 1: регіон -->
  <p style="subtitle_style">           <!-- рівень 2: субтитр -->
    <span style="line_style">          <!-- рівень 3: рядок -->
      <span style="fragment_style">    <!-- рівень 4: фрагмент тексту
        Текст з усіма накладеними стилями
      </span>
    </span>
  </p>
</region>
```

### 🔧 Тестова валідація злиття:

```go
func TestTTMLMergeStyleAttributes(t *testing.T) {
    s, _ := astisub.OpenFile("./testdata/example-in-merging-style.ttml")
    
    // Перевірка 4 елементів з різними комбінаціями наслідування
    assert.Equal(t, 4, len(s.Items))
    
    // Елемент 0: регіон + стиль субтитру + inline-колір
    assert.Equal(t, "region_0", s.Items[0].Region.ID)
    assert.Equal(t, "style_1", s.Items[0].Style.ID)
    assert.Equal(t, astisub.ColorRed, s.Items[0].InlineStyle.TTMLColor)  // перевизначення
    
    // Елемент 1: інший регіон + інший стиль + вкладений фрагмент зі стилем
    assert.Equal(t, "region_1", s.Items[1].Region.ID)
    assert.Equal(t, "style_0", s.Items[1].Style.ID)
    assert.Equal(t, "style_1", s.Items[1].Lines[0].Items[0].Style.ID)  // вкладений стиль
    
    // Елемент 2: регіон + стиль без додаткових перевизначень
    assert.Equal(t, "region_2", s.Items[2].Region.ID)
    assert.Equal(t, "style_0", s.Items[2].Style.ID)
    
    // Елемент 3: тільки inline-колір без посилань на стилі/регіони
    assert.Equal(t, astisub.ColorBlue, s.Items[3].InlineStyle.TTMLColor)
}
```

### 🎯 Чому тестування наслідування критичне?

```
Помилки у наслідуванні стилів призводять до:
• Неправильного кольору/позиціонування субтитрів
• Втрата інформації при конвертації у інші формати
• Несумісності з плеєрами, що очікують певну ієрархію

Тест покриває ключові сценарії:
1. Перевизначення атрибутів на нижчих рівнях (колір)
2. Вкладені стилі у фрагментах тексту
3. Відсутність посилань на стилі (тільки inline-атрибути)
4. Комбінації регіон + стиль + inline

У вашому пайплайні: це гарантує, що субтитри з професійних джерел
(де використовуються складні ієрархії стилів) відображатимуться коректно.
```

---

## 🐞 6. Потенційні проблеми та покращення тестів

### ❗ Критичні недоліки:

1. **Відсутність тестів на великі/складні TTML файли**:
   ```go
   // Усі тести використовують маленькі файли з 3-6 субтитрами
   // Але реальні субтитри можуть мати тисячі елементів з глибокою вкладеністю
   
   // Додати тест на продуктивність:
   func TestTTML_LargeFile(t *testing.T) {
       // Згенерувати TTML з 10,000 субтитрами та 5 рівнями вкладеності стилів
       // Виміряти час парсингу: має бути < 500ms
       // Перевірити використання пам'яті: має бути < 100MB
   }
   ```

2. **Не тестується обробка невалідного XML**:
   ```go
   // Що станеться, якщо TTML містить невалідний XML?
   // Чи повертається зрозуміла помилка, чи паніка?
   
   // Додати тест:
   func TestTTML_InvalidXML(t *testing.T) {
       invalidXML := `<tt><body><p begin="00:00:01.000" end="00:00:02.000">Unclosed tag`
       _, err := astisub.ReadFromTTML(strings.NewReader(invalidXML))
       assert.Error(t, err)  // має повернути помилку парсингу XML
       assert.NotPanics(t, func() { /* перевірка на відсутність паніки */ })
   }
   ```

3. **Жорсткі порівняння рядків у roundtrip тестах**:
   ```go
   assert.Equal(t, string(c), w.String())
   // Чутливе до порядку атрибутів, пробілів, переносів рядків
   
   // Краще: парсити вихід і порівнювати семантично
   outputSubs, _ := astisub.ReadFromTTML(bytes.NewReader(w.Bytes()))
   assert.Equal(t, s.Items, outputSubs.Items)  // порівняння структур, не тексту
   ```

4. **Відсутність тестів на конвертацію кольорів між форматами**:
   ```go
   // TTML підтримує #RRGGBB, іменовані кольори, прозорість
   // Чи коректно конвертуються вони у WebVTT/STL еквіваленти?
   
   // Додати тест:
   func TestTTML_ColorConversion(t *testing.T) {
       testData := `<tt><body><p><span tts:color="#FF000080">Red with alpha</span></p></body></tt>`
       s, _ := astisub.ReadFromTTML(strings.NewReader(testData))
       
       // Перевірка конвертації у WebVTT
       webvtt := &bytes.Buffer{}
       s.WriteToWebVTT(webvtt)
       assert.Contains(t, webvtt.String(), "<c.red>")  // або інше очікуване представлення
   }
   ```

5. **Відсутність тестів на підтримку нових атрибутів TTML**:
   ```go
   // TTML специфікація розширюється: нові атрибути для HDR, 3D, тощо
   // Чи ігноруються невідомі атрибути, чи повертається помилка?
   
   // Додати тест:
   func TestTTML_UnknownAttributes(t *testing.T) {
       testData := `<tt><body><p tts:unknownAttribute="value">Text</p></body></tt>`
       s, err := astisub.ReadFromTTML(strings.NewReader(testData))
       assert.NoError(t, err)  // невідомі атрибути мають ігноруватись
       // Але можна додати попередження у метрики
   }
   ```

### 💡 Покращення:

```go
// 1. Helper для генерації складних тестових даних
func generateComplexTTML(depth int, numItems int) string {
    var sb strings.Builder
    sb.WriteString(`<tt xmlns:tts="http://www.w3.org/ns/ttml#styling"><head>`)
    
    // Генерація стилів з вкладеністю
    for i := 0; i < depth; i++ {
        sb.WriteString(fmt.Sprintf(`<style xml:id="style_%d" tts:color="#%06X" />`, 
            i, i*0x111111))
    }
    sb.WriteString(`</head><body>`)
    
    // Генерація субтитрів
    for i := 0; i < numItems; i++ {
        start := time.Duration(i*100) * time.Millisecond
        end := start + 2*time.Second
        sb.WriteString(fmt.Sprintf(`<p begin="%s" end="%s" style="style_%d">`, 
            formatDuration(start), formatDuration(end), i%depth))
        
        // Вкладені фрагменти з різними стилями
        for j := 0; j < depth; j++ {
            sb.WriteString(fmt.Sprintf(`<span style="style_%d">Fragment %d</span>`, j, j))
        }
        sb.WriteString(`</p>`)
    }
    sb.WriteString(`</body></tt>`)
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
        // Порівняння стилів, регіонів, вкладеності...
    }
}

// 3. Тест на стійкість до невалідних даних
func TestTTML_Robustness(t *testing.T) {
    invalidInputs := []string{
        "",                    // порожній файл
        "<tt>",               // неповний XML
        `<p begin="invalid">`, // невалідний таймінг
        `<span tts:color="notacolor">`, // невідомий колір
        `<tt xmlns:tts="http://www.w3.org/ns/ttml#styling"><body><p>Unclosed`, // невалідний XML
    }
    
    for _, input := range invalidInputs {
        t.Run(input, func(t *testing.T) {
            // Не повинно панікувати
            defer func() {
                if r := recover(); r != nil {
                    t.Errorf("panic on input %q: %v", input, r)
                }
            }()
            _, err := astisub.ReadFromTTML(strings.NewReader(input))
            // Помилка допустима, головне — стабільність
            _ = err
        })
    }
}

// 4. Тест на конвертацію кольорів
func TestTTML_ColorConversion(t *testing.T) {
    tests := []struct{
        ttmlColor string
        expectedWebVTT string
    }{
        {"#FF0000", "<c.red>"},      // іменований колір
        {"#00FF00", "<c.green>"},    // інший іменований
        {"#123456", "<c.#123456>"},  // невідомий колір → залишається як є
        {"#FF000080", "<c.red>"},    // з прозорістю → ігноруємо alpha
    }
    
    for _, tt := range tests {
        t.Run(tt.ttmlColor, func(t *testing.T) {
            testData := fmt.Sprintf(`<tt xmlns:tts="http://www.w3.org/ns/ttml#styling">
                <body><p><span tts:color="%s">Text</span></p></body></tt>`, tt.ttmlColor)
            
            s, _ := astisub.ReadFromTTML(strings.NewReader(testData))
            w := &bytes.Buffer{}
            s.WriteToWebVTT(w)
            
            // Перевірка, що колір конвертовано коректно
            assert.Contains(t, w.String(), tt.expectedWebVTT)
        })
    }
}
```

---

## 🎯 7. Інтеграція з вашим CCTV HLS Processor

### 📍 У `SubtitleImporter` — імпорт TTML субтитрів:

```go
type SubtitleImporter struct {
    timestampMap *astisub.WebVTTTimestampMap  // для конвертації часу
}

func (imp *SubtitleImporter) ImportTTML(filePath string) error {
    file, err := os.Open(filePath)
    if err != nil { return fmt.Errorf("open TTML file: %w", err) }
    defer file.Close()
    
    subs, err := astisub.ReadFromTTML(file)
    if err != nil {
        return fmt.Errorf("parse TTML: %w", err)
    }
    
    // Конвертація у внутрішній формат з урахуванням наслідування стилів
    for _, item := range subs.Items {
        // Розрахунок абсолютних часових міток
        absoluteStart := item.StartAt
        absoluteEnd := item.EndAt
        if imp.timestampMap != nil {
            absoluteStart += imp.timestampMap.Offset()
            absoluteEnd += imp.timestampMap.Offset()
        }
        
        // Екстракція тексту з урахуванням вкладених стилів
        for _, line := range item.Lines {
            for _, textItem := range line.Items {
                // Злиття стилів: регіон → субтитр → рядок → фрагмент
                mergedStyle := mergeStyleAttributes(
                    item.Region?.InlineStyle,
                    item.Style?.InlineStyle,
                    line.InlineStyle,
                    textItem.InlineStyle,
                )
                
                frame := &SubtitleFrame{
                    StartPTS: convertDurationToPTS(absoluteStart),
                    EndPTS:   convertDurationToPTS(absoluteEnd),
                    Text:     textItem.Text,
                    Styles:   convertStyleAttributes(mergedStyle),
                }
                imp.subtitleQueue <- frame
            }
        }
    }
    return nil
}

// Helper для злиття стилів з різних рівнів
func mergeStyleAttributes(levels ...*astisub.StyleAttributes) *astisub.StyleAttributes {
    result := &astisub.StyleAttributes{}
    for _, level := range levels {
        if level == nil { continue }
        // Перевизначення: значення з нижчих рівнів мають пріоритет
        if level.TTMLColor != nil { result.TTMLColor = level.TTMLColor }
        if level.TTMLOrigin != nil { result.TTMLOrigin = level.TTMLOrigin }
        // ... інші атрибути ...
    }
    return result
}
```

### 📍 У `HLSGenerator` — генерація TTML для плейлиста:

```go
func (gen *HLSSubtitleGenerator) WriteTTMLSegment(frames []*SubtitleFrame, outputPath string) error {
    // Конвертація внутрішніх кадрів у формат astisub
    subs := &astisub.Subtitles{
        Meta &astisub.Metadata{
            // Додати метадані: мова, заголовок, авторські права
            Language: gen.language,
            Title: gen.title,
        },
    }
    
    // Створення регіонів за замовчуванням (якщо потрібно)
    defaultRegion := &astisub.Region{
        ID: "default",
        InlineStyle: &astisub.StyleAttributes{
            TTMLOrigin: astikit.StrPtr("0% 80%"),  // нижня частина екрану
            TTMLExtent: astikit.StrPtr("100% 20%"), // 20% висоти
            TTMLTextAlign: astikit.StrPtr("center"),
        },
    }
    subs.Regions["default"] = defaultRegion
    
    for i, frame := range frames {
        // Конвертація стилів у TTML атрибути
        ttmlStyle := convertStylesToTTML(frame.Styles)
        
        item := &astisub.Item{
            StartAt: frame.StartPTS,
            EndAt: frame.EndPTS,
            Index: i,
            Region: defaultRegion,  // призначити регіон за замовчуванням
            InlineStyle: ttmlStyle,
            Lines: []*astisub.Line{
                {
                    Items: []*astisub.LineItem{
                        {
                            Text: frame.Text,
                            InlineStyle: ttmlStyle,
                        },
                    },
                },
            },
        }
        subs.Items = append(subs.Items, item)
    }
    
    // Запис у файл з опцією компактного формату для мережі
    file, err := os.Create(outputPath)
    if err != nil {
        return fmt.Errorf("create TTML file: %w", err)
    }
    defer file.Close()
    
    // Використання компактного формату без відступів
    return subs.WriteToTTML(file, astisub.WriteToTTMLWithIndentOption(""))
}
```

### 📍 У метриках — моніторинг якості парсингу:

```go
func (imp *SubtitleImporter) recordParseMetrics(subs *astisub.Subtitles, err error) {
    if err != nil {
        metrics.SubtitleParseErrors.WithLabelValues("TTML", err.Error()).Inc()
        return
    }
    
    metrics.SubtitleItemsParsed.WithLabelValues("TTML").Add(float64(len(subs.Items)))
    
    // Статистика по стилях та регіонах
    var styleCount, regionCount, nestedStyleCount int
    for _, item := range subs.Items {
        if item.Style != nil { styleCount++ }
        if item.Region != nil { regionCount++ }
        for _, line := range item.Lines {
            for _, textItem := range line.Items {
                if textItem.Style != nil { nestedStyleCount++ }
            }
        }
    }
    metrics.SubtitleFeaturesUsed.WithLabelValues("style").Add(float64(styleCount))
    metrics.SubtitleFeaturesUsed.WithLabelValues("region").Add(float64(regionCount))
    metrics.SubtitleFeaturesUsed.WithLabelValues("nested-style").Add(float64(nestedStyleCount))
    
    // Розмір тексту (для оптимізації буферів)
    totalTextSize := 0
    for _, item := range subs.Items {
        for _, line := range item.Lines {
            for _, textItem := range line.Items {
                totalTextSize += len(textItem.Text)
            }
        }
    }
    metrics.SubtitleTextSize.Observe(float64(totalTextSize))
}
```

---

## 🧭 Висновок: чому ці тести — гарантія надійності парсингу

| Компонент | Роль у TTML парсері | Вартість помилки без нього |
|-----------|---------------------|---------------------------|
| **Глибокі порівняння структур** | Валідація наслідування стилів на всіх рівнях | Неправильне відображення субтитрів через помилки у злитті атрибутів |
| **Roundtrip тести** | Гарантія, що парсинг + запис зберігають дані | Втрата субтитрів або стилів при конвертації між форматами |
| **Тести на обробку переносів рядків** | Коректна підтримка багаторядкових субтитрів | Об'єднання рядків у один → нечитабельні субтитри |
| **Тести на контроль форматування** | Гнучкість виводу для різних сценаріїв | Зайве споживання bandwidth або складність дебагу |
| **Тести на злиття стилів** | Підтримка складних ієрархій TTML | Втрата інформації про позиціонування/кольори у вкладених елементах |

> 🔑 **Головна ідея**: Ці тести — **страховка від регресій** при зміні логіки парсингу або додаванні нових атрибутів TTML. Вони документують:
> 1. Як обробляється наслідування стилів між регіонами, субтитрами та фрагментами тексту
> 2. Як конвертуються атрибути між TTML та іншими форматами (WebVTT, STL)
> 3. Як парсер поводиться з невалідними/неповними даними
> 4. Які компроміси прийняті між повнотою даних та сумісністю

Без них будь-яка оптимізація парсера може непомітно зламати сумісність з існуючими TTML файлами — і ви дізнаєтесь про це тільки коли субтитри з певних камер перестануть відтворюватись коректно.

💡 **Фінальна порада**: 
1. Додайте тести на продуктивність для великих/складних файлів
2. Покрийте обробку невалідного XML та невідомих атрибутів
3. Замініть текстові roundtrip порівняння на семантичні (парсинг виходу + порівняння структур)
4. Додайте тести на конвертацію кольорів та інших атрибутів між форматами
5. Реалізуйте fuzz-тести для перевірки стійкості до випадкових/пошкоджених даних

Це перетворить ці тести з "перевірки базової функціональності" на "гарантію надійності парсингу" для всього вашого пайплайну обробки субтитрів у CCTV HLS Processor.