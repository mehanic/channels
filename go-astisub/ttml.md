# 🧪 Глибокий розбір `astisub/ttml.go` — реалізація парсингу та запису субтитрів у форматі TTML

Це **основний модуль** для роботи з форматом TTML (Timed Text Markup Language) у бібліотеці `astisub`. Файл реалізує повний цикл: читання/парсинг `.ttml` файлів, обробку метаданих, стилів, регіонів, вкладених елементів, та запис назад у валідний TTML формат з підтримкою просторів імен XML. Розберемо архітектурно:

---

## 🧱 1. Архітектура: XML-орієнтований парсинг зі станами

### 🔑 Ключові компоненти:

```go
// Константи для мовних кодів
const (
	ttmlLanguageChinese   = "zh"
	ttmlLanguageEnglish   = "en"
	ttmlLanguageJapanese  = "ja"
	ttmlLanguageFrench    = "fr"
	ttmlLanguageNorwegian = "no"
)

// Мапінг мовних кодів (BiMap для двосторонньої конвертації)
var ttmlLanguageMapping = astikit.NewBiMap().
	Set(ttmlLanguageChinese, LanguageChinese).
	Set(ttmlLanguageEnglish, LanguageEnglish).
	// ... інші мови ...

// Регулярні вирази для парсингу часу
var (
	ttmlRegexpClockTimeFrames = regexp.MustCompile(`\:[\d]+$`)  // :fff (кадри)
	ttmlRegexpOffsetTime      = regexp.MustCompile(`^(\d+(\.\d+)?)(h|m|s|ms|f|t)$`)  // відносні одиниці
)
```

### 🔧 Структури для вхідного TTML (`TTMLIn`):

```go
// Основна структура для парсингу
type TTMLIn struct {
	Framerate int            `xml:"frameRate,attr"`      // частота кадрів відео
	Lang      string         `xml:"lang,attr"`           // мова субтитрів
	Metadata  TTMLInMetadata `xml:"head>metadata"`       // метадані
	Regions   []TTMLInRegion `xml:"head>layout>region"`  // визначення регіонів
	Styles    []TTMLInStyle  `xml:"head>styling>style"`  // визначення стилів
	Body      TTMLInBody     `xml:"body"`                // тіло з субтитрами
	Tickrate  int            `xml:"tickRate,attr"`       // tickrate для тіків
	XMLName   xml.Name       `xml:"tt"`                  // кореневий елемент
}

// Тіло з дивами (групами субтитрів)
type TTMLInBody struct {
	XMLName xml.Name        `xml:"body"`
	Divs    []TTMLInBodyDiv `xml:"div"`  // див = група субтитрів
	// Наслідувані атрибути стилю для всіх дивів
	Region string `xml:"region,attr,omitempty"`
	Style  string `xml:"style,attr,omitempty"`
	TTMLInStyleAttributes
}

// Див (група субтитрів)
type TTMLInBodyDiv struct {
	XMLName   xml.Name         `xml:"div"`
	Subtitles []TTMLInSubtitle `xml:"p"`  // p = параграф = субтитр
	// Наслідувані атрибути
	Region string `xml:"region,attr,omitempty"`
	Style  string `xml:"style,attr,omitempty"`
	TTMLInStyleAttributes
}
```

### 🔧 Структури для вихідного TTML (`TTMLOut`):

```go
// Основна структура для запису з просторами імен
type TTMLOut struct {
	Lang            string            `xml:"xml:lang,attr,omitempty"`
	Metadata        *TTMLOutMetadata  `xml:"head>metadata,omitempty"`
	Styles          []TTMLOutStyle    `xml:"head>styling>style,omitempty"`
	Regions         []TTMLOutRegion   `xml:"head>layout>region,omitempty"`
	Subtitles       []TTMLOutSubtitle `xml:"body>div>p,omitempty"`
	XMLName         xml.Name          `xml:"http://www.w3.org/ns/ttml tt"`
	XMLNamespaceTTM string            `xml:"xmlns:ttm,attr"`  // метадані
	XMLNamespaceTTS string            `xml:"xmlns:tts,attr"`  // стилі
}
```

### 🎯 Чому розділення на `TTMLIn`/`TTMLOut`?

```
Проблема: вхідний TTML може бути "вільним" (без просторів імен),
а вихідний має бути валідним за специфікацією (з просторами імен).

Рішення:
• TTMLIn: гнучкий парсинг, приймає будь-який валідний XML
• TTMLOut: строгий запис, генерує стандартний TTML з xmlns:ttm/tts

Переваги:
1. Зворотна сумісність: можна читати старі файли без просторів імен
2. Прямана сумісність: записує файли, сумісні з сучасними валідаторами
3. Гнучкість: можна конвертувати між форматами без втрати даних

У вашому CCTV HLS Processor: це дозволяє імпортувати субтитри з будь-яких джерел
та експортувати у стандартний формат для сумісності з плеєрами.
```

---

## ⏱️ 2. `TTMLInDuration` — парсинг часових міток у різних форматах

### 🔧 Структура та логіка:

```go
type TTMLInDuration struct {
	d                 time.Duration  // базова тривалість
	frames, framerate int            // кадри та частота кадрів
	ticks, tickrate   int            // тіки та tickrate
}
```

### 🔧 `UnmarshalText()` — парсинг різних форматів:

```go
func (d *TTMLInDuration) UnmarshalText(i []byte) (err error) {
	text := string(i)
	
	// 1. Перевірка відносних одиниць: 123.4h, 100f, 6t, тощо
	if matches := ttmlRegexpOffsetTime.FindStringSubmatch(text); matches != nil {
		value, _ := strconv.ParseFloat(matches[1], 64)
		metric := matches[3]
		
		switch metric {
		case "t":  // тіки
			d.ticks = int(value)
		case "f":  // кадри
			d.frames = int(value)
		default:  // h, m, s, ms
			var timebase time.Duration
			switch metric {
			case "h": timebase = time.Hour
			case "m": timebase = time.Minute
			case "s": timebase = time.Second
			case "ms": timebase = time.Millisecond
			}
			d.d = time.Duration(value * float64(timebase.Nanoseconds()))
		}
		return
	}
	
	// 2. Перевірка формату з кадрами: hh:mm:ss:fff
	if indexes := ttmlRegexpClockTimeFrames.FindStringIndex(text); indexes != nil {
		// Витягнути кадри після останньої двокрапки
		s := text[indexes[0]+1 : indexes[1]]
		d.frames, err = strconv.Atoi(s)
		
		// Замінити :fff на .000 для парсингу як стандартний час
		text = text[:indexes[0]] + ".000"
	}
	
	// 3. Парсинг стандартного формату: hh:mm:ss.mmm
	d.d, err = parseDuration(text, ".", 3)
	return
}
```

### 🔧 `duration()` — конвертація у `time.Duration`:

```go
func (d TTMLInDuration) duration() (o time.Duration) {
	// 1. Обробка тіків (найвищий пріоритет)
	if d.ticks > 0 && d.tickrate > 0 {
		return time.Duration(float64(d.ticks) * 1e9 / float64(d.tickrate))
	}
	
	// 2. Базова тривалість
	o = d.d
	
	// 3. Додавання кадрів (якщо є)
	if d.frames > 0 && d.framerate > 0 {
		o += time.Duration(float64(d.frames) / float64(d.framerate) * float64(time.Second.Nanoseconds()))
	}
	return
}
```

### 🎯 Чому така ієрархія обробки часу?

```
TTML підтримує кілька форматів часу з різним пріоритетом:

1. Тіки (`6t`):
   • Найвища точність, використовується у професійних системах
   • Конвертація: ticks / tickrate = секунди
   • Приклад: 6 тіків @ 4 tickrate = 1.5 секунди

2. Кадри (`100f` або `00:00:01:2`):
   • Синхронізація з відео, залежить від framerate
   • Конвертація: frames / framerate = секунди
   • Приклад: 2 кадри @ 25 fps = 80 ms

3. Відносні одиниці (`123.4h`, `123m`, `123s`, `123ms`):
   • Зручність для конфігурації, не залежить від відео
   • Пряма конвертація у time.Duration

4. Стандартний формат (`00:00:01.234`):
   • Універсальний, сумісний з іншими форматами
   • Парсинг через загальну функцію parseDuration()

Пріоритет: тіки > кадри > відносні одиниці > стандартний формат

У вашому пайплайні: це дозволяє імпортувати субтитри з будь-яких джерел
(професійні системи, відео-редактори, ручне створення) та коректно
синхронізувати їх з відео незалежно від формату вхідних часових міток.
```

---

## 🔍 3. `ReadFromTTML()` — парсинг XML зі складною логікою наслідування

### 🔧 Крок 1: Розпарсити XML у структури

```go
func ReadFromTTML(i io.Reader) (o *Subtitles, err error) {
	o = NewSubtitles()
	
	// Розпарсити XML у TTMLIn структури
	var ttml TTMLIn
	if err = xml.NewDecoder(i).Decode(&ttml); err != nil {
		err = fmt.Errorf("astisub: xml decoding failed: %w", err)
		return
	}
	
	// Додати метадані
	o.Metadata = ttml.metadata()
	// ...
}
```

### 🔧 Крок 2: Обробка стилів з наслідуванням

```go
// Loop through styles
var parentStyles = make(map[string]*Style)
for _, ts := range ttml.Styles {
	var s = &Style{
		ID:          ts.ID,
		InlineStyle: ts.TTMLInStyleAttributes.styleAttributes(),
	}
	o.Styles[s.ID] = s
	if len(ts.Style) > 0 {
		// Зберегти посилання на батьківський стиль для подальшого наслідування
		parentStyles[ts.Style] = s
	}
}

// Take care of parent styles
for id, s := range parentStyles {
	if _, ok := o.Styles[id]; !ok {
		err = fmt.Errorf("astisub: Style %s requested by style %s doesn't exist", id, s.ID)
		return
	}
	s.Style = o.Styles[id]  // встановити посилання на батьківський стиль
}
```

### 🔧 Крок 3: Обробка регіонів з посиланнями на стилі

```go
// Loop through regions
for _, tr := range ttml.Regions {
	var r = &Region{
		ID:          tr.ID,
		InlineStyle: tr.TTMLInStyleAttributes.styleAttributes(),
	}
	if len(tr.Style) > 0 {
		if _, ok := o.Styles[tr.Style]; !ok {
			err = fmt.Errorf("astisub: Style %s requested by region %s doesn't exist", tr.Style, r.ID)
			return
		}
		r.Style = o.Styles[tr.Style]  // посилання на стиль
	}
	o.Regions[r.ID] = r
}
```

### 🔧 Крок 4: Обробка субтитрів з багаторівневим наслідуванням

```go
// Loop through subtitles
bodyInlineStyle := ttml.Body.TTMLInStyleAttributes.styleAttributes()
for _, div := range ttml.Body.Divs {
	divInlineStyle := div.TTMLInStyleAttributes.styleAttributes()
	
	// Наслідування: Body → Div
	divInlineStyle.merge(bodyInlineStyle)
	if div.Region == "" { div.Region = ttml.Body.Region }
	if div.Style == "" { div.Style = ttml.Body.Style }
	
	for _, ts := range div.Subtitles {
		// Встановити framerate/tickrate для парсингу часу
		ts.Begin.framerate = ttml.Framerate
		ts.Begin.tickrate = ttml.Tickrate
		ts.End.framerate = ttml.Framerate
		ts.End.tickrate = ttml.Tickrate
		
		itemInlineStyle := ts.TTMLInStyleAttributes.styleAttributes()
		
		// Наслідування: Body → Div → Item
		itemInlineStyle.merge(divInlineStyle)
		if ts.Region == "" { ts.Region = div.Region }
		if ts.Style == "" { ts.Style = div.Style }
		
		var s = &Item{
			EndAt:       ts.End.duration(),
			InlineStyle: itemInlineStyle,
			StartAt:     ts.Begin.duration(),
		}
		
		// Додати посилання на регіон/стиль, якщо вказано
		if len(ts.Region) > 0 { s.Region = o.Regions[ts.Region] }
		if len(ts.Style) > 0 { s.Style = o.Styles[ts.Style] }
		
		// ... парсинг текстового контенту ...
	}
}
```

### 🎯 Чому таке складне наслідування?

```
TTML підтримує багаторівневе наслідування стилів:
1. Рівень 1: <body style="body_style"> → застосовується до всіх дивів
2. Рівень 2: <div style="div_style"> → перевизначає/доповнює body_style
3. Рівень 3: <p style="subtitle_style"> → перевизначає/доповнює div_style
4. Рівень 4: <span style="fragment_style"> → перевизначає/доповнює subtitle_style

Приклад ієрархії:
<body tts:color="white" tts:origin="0% 80%">
  <div tts:color="yellow">  <!-- перевизначає колір -->
    <p style="subtitle1">  <!-- наслідує origin з body, color з div -->
      <span tts:color="red">Текст</span>  <!-- перевизначає колір -->
    </p>
  </div>
</body>

Результат для <span>:
• color: red (найнижчий рівень має пріоритет)
• origin: 0% 80% (з body, не перевизначено вище)

У вашому коді: метод `merge()` реалізує цю логіку:
• Значення з нижчих рівнів перевизначають значення з вищих
• Відсутні значення на нижчих рівнях наслідуються з вищих

Без цього: втрата інформації про стилі при парсингу складних TTML файлів.
```

---

## 🔤 4. Парсинг текстового контенту з підтримкою вкладених тегів

### 🔧 Спеціальний декодер для обробки `<br>`:

```go
// Проблема: стандартний xml.Decoder ігнорує <br> теги у текстовому контенті
// Рішення: кастомний TokenReader, що замінює <br> на \n

type ttmlXmlTokenReader struct {
	xmlTokenReader xml.TokenReader
	holdingToken   xml.Token  // буфер для "відкладеного" токена
}

func (r *ttmlXmlTokenReader) Token() (xml.Token, error) {
	// Якщо є відкладений токен (наприклад, </br>), повернути його
	if r.holdingToken != nil {
		returnToken := r.holdingToken
		r.holdingToken = nil
		return returnToken, nil
	}
	
	// Отримати наступний токен
	t, err := r.xmlTokenReader.Token()
	if err != nil { return nil, err }
	
	// Якщо це <br> → замінити на \n та зберегти оригінальний токен
	if se, ok := t.(xml.StartElement); ok && strings.ToLower(se.Name.Local) == "br" {
		r.holdingToken = t  // зберегти для подальшої обробки
		return xml.CharData("\n"), nil  // повернути перенос рядка
	}
	
	return t, nil
}

func newTTMLXmlDecoder(s string) *xml.Decoder {
	return xml.NewTokenDecoder(
		&ttmlXmlTokenReader{
			xmlTokenReader: xml.NewDecoder(strings.NewReader("<p>" + s + "</p>")),
		},
	)
}
```

### 🔧 Парсинг текстових елементів:

```go
// Remove items indentation (видалення провідних пробілів)
lines := strings.Split(ts.Items, "\n")
for i := 0; i < len(lines); i++ {
	lines[i] = strings.TrimLeftFunc(lines[i], unicode.IsSpace)
}

// Unmarshal items через кастомний декодер
var items = TTMLInItems{}
if err = newTTMLXmlDecoder(strings.Join(lines, "")).Decode(&items); err != nil {
	err = fmt.Errorf("astisub: unmarshaling items failed: %w", err)
	return
}

// Loop through texts
var l = &Line{}
for _, tt := range items {
	// Обробка <br> тегів (як переноси рядків)
	if strings.ToLower(tt.XMLName.Local) == "br" {
		s.Lines = append(s.Lines, *l)
		l = &Line{}
		continue
	}
	
	// Обробка тексту з можливими переносами рядків
	for idx, li := range strings.Split(tt.Text, "\n") {
		if idx > 0 {  // новий рядок
			s.Lines = append(s.Lines, *l)
			l = &Line{}
		}
		
		// Створення елементу рядка зі стилями
		var t = LineItem{
			InlineStyle: tt.TTMLInStyleAttributes.styleAttributes(),
			Text:        li,
		}
		
		// Додати посилання на стиль, якщо вказано
		if len(tt.Style) > 0 {
			if _, ok := o.Styles[tt.Style]; !ok {
				err = fmt.Errorf("astisub: Style %s requested by item with text %s doesn't exist", tt.Style, tt.Text)
				return
			}
			t.Style = o.Styles[tt.Style]
		}
		
		l.Items = append(l.Items, t)
	}
}
s.Lines = append(s.Lines, *l)  // додати останній рядок
```

### 🎯 Чому кастомний декодер для `<br>`?

```
Проблема стандартного xml.Decoder:
• <br> теги у текстовому контенті ігноруються або парсуються неправильно
• Результат: втрата переносів рядків у субтитрах

Рішення ttmlXmlTokenReader:
1. Перехоплює токени від стандартного декодера
2. Якщо токен = <br> → замінює його на `\n` (CharData)
3. Зберігає оригінальний токен для подальшої обробки (якщо потрібно)

Приклад:
Вхід: "Перший рядок<br/>Другий рядок"
Стандартний декодер: "Перший рядокДругий рядок" (без переносу)
Кастомний декодер: "Перший рядок\nДругий рядок" ✓

У вашому пайплайні: це гарантує, що багаторядкові субтитри
відображатимуться коректно з переносами рядків у плеєрах.
```

---

## ✍️ 5. `WriteToTTML()` — серіалізація у валідний TTML з просторами імен

### 🔧 Крок 1: Ініціалізація вихідної структури

```go
func (s Subtitles) WriteToTTML(o io.Writer, opts ...WriteToTTMLOption) (err error) {
	// Обробка опцій (наприклад, індентація)
	wo := &WriteToTTMLOptions{Indent: "    "}
	for _, opt := range opts { opt(wo) }
	
	if len(s.Items) == 0 { return ErrNoSubtitlesToWrite }
	
	// Ініціалізація TTMLOut з просторами імен
	var ttml = TTMLOut{
		XMLNamespaceTTM: "http://www.w3.org/ns/ttml#metadata",
		XMLNamespaceTTS: "http://www.w3.org/ns/ttml#styling",
	}
	
	// Додати метадані з конвертацією мови
	if s.Metadata != nil {
		if v, ok := ttmlLanguageMapping.GetInverse(s.Metadata.Language); ok {
			ttml.Lang = v.(string)  // LanguageFrench → "fr"
		}
		if len(s.Metadata.TTMLCopyright) > 0 || len(s.Metadata.Title) > 0 {
			ttml.Metadata = &TTMLOutMetadata{
				Copyright: s.Metadata.TTMLCopyright,
				Title:     s.Metadata.Title,
			}
		}
	}
```

### 🔧 Крок 2: Серіалізація регіонів та стилів

```go
// Add regions (сортування за ID для детермінованого виводу)
var k []string
for _, region := range s.Regions { k = append(k, region.ID) }
sort.Strings(k)
for _, id := range k {
	var ttmlRegion = TTMLOutRegion{TTMLOutHeader: TTMLOutHeader{
		ID:                     s.Regions[id].ID,
		TTMLOutStyleAttributes: ttmlOutStyleAttributesFromStyleAttributes(s.Regions[id].InlineStyle),
	}}
	if s.Regions[id].Style != nil {
		ttmlRegion.Style = s.Regions[id].Style.ID
	}
	ttml.Regions = append(ttml.Regions, ttmlRegion)
}

// Add styles (аналогічно)
// ...
```

### 🔧 Крок 3: Серіалізація субтитрів з оптимізацією тегів

```go
// Add items
for _, item := range s.Items {
	var ttmlSubtitle = TTMLOutSubtitle{
		Begin:                  TTMLOutDuration(item.StartAt),
		End:                    TTMLOutDuration(item.EndAt),
		TTMLOutStyleAttributes: ttmlOutStyleAttributesFromStyleAttributes(item.InlineStyle),
	}
	
	// Додати посилання на регіон/стиль
	if item.Region != nil { ttmlSubtitle.Region = item.Region.ID }
	if item.Style != nil { ttmlSubtitle.Style = item.Style.ID }
	
	// Додати рядки та елементи
	for _, line := range item.Lines {
		for _, lineItem := range line.Items {
			var ttmlItem = TTMLOutItem{
				Text:                   lineItem.Text,
				TTMLOutStyleAttributes: ttmlOutStyleAttributesFromStyleAttributes(lineItem.InlineStyle),
				XMLName:                xml.Name{Local: "span"},  // <span> для фрагментів тексту
			}
			if lineItem.Style != nil { ttmlItem.Style = lineItem.Style.ID }
			ttmlSubtitle.Items = append(ttmlSubtitle.Items, ttmlItem)
		}
		// Додати <br> для переносу рядка
		ttmlSubtitle.Items = append(ttmlSubtitle.Items, TTMLOutItem{XMLName: xml.Name{Local: "br"}})
	}
	
	// Видалити останній <br> (не потрібен в кінці субтитру)
	if len(ttmlSubtitle.Items) > 0 {
		ttmlSubtitle.Items = ttmlSubtitle.Items[:len(ttmlSubtitle.Items)-1]
	}
	
	ttml.Subtitles = append(ttml.Subtitles, ttmlSubtitle)
}
```

### 🔧 Крок 4: XML-енкодинг з індентацією

```go
// Marshal XML
var e = xml.NewEncoder(o)
e.Indent("", wo.Indent)  // встановити індентацію (за замовчуванням 4 пробіли)

if err = e.Encode(ttml); err != nil {
	err = fmt.Errorf("astisub: xml encoding failed: %w", err)
	return
}
return
```

### 🎯 Чому простори імен критичні для вихідного TTML?

```
Специфікація TTML вимагає простори імен для атрибутів стилів:
• xmlns:ttm="http://www.w3.org/ns/ttml#metadata" → для метаданих (title, copyright)
• xmlns:tts="http://www.w3.org/ns/ttml#styling" → для стилів (color, origin, тощо)

Приклад валідного виходу:
<tt xmlns:ttm="..." xmlns:tts="...">
  <head>
    <metadata>
      <ttm:title>Заголовок</ttm:title>  ← простір імен ttm
    </metadata>
    <styling>
      <style tts:color="white" tts:origin="0% 80%"/>  ← простір імен tts
    </styling>
  </head>
  ...
</tt>

Без просторів імен:
• Валідатори відхилять файл як невалідний TTML
• Плеєри можуть не розпізнати атрибути стилів
• Конвертація у інші формати може втратити інформацію

У вашому коді: TTMLOut використовує структурні теги з просторами імен:
`xml:"tts:color,attr,omitempty"` → генерує `tts:color="white"` ✓
```

---

## 🔄 6. Конвертація стилів: `TTMLInStyleAttributes` ↔ `StyleAttributes` ↔ `TTMLOutStyleAttributes`

### 🔧 Вхід → внутрішній формат:

```go
func (s TTMLInStyleAttributes) styleAttributes() (o *StyleAttributes) {
	o = &StyleAttributes{
		TTMLDirection:      s.Direction,
		TTMLDisplay:        s.Display,
		TTMLDisplayAlign:   s.DisplayAlign,
		TTMLExtent:         s.Extent,
		// ... інші атрибути ...
	}
	
	// Парсинг кольорів з HTML-формату (#RRGGBB або іменовані)
	if s.Color != nil {
		if color, err := newColorFromHTMLString(*s.Color); err == nil {
			o.TTMLColor = color
		}
	}
	if s.BackgroundColor != nil {
		if color, err := newColorFromHTMLString(*s.BackgroundColor); err == nil {
			o.TTMLBackgroundColor = color
		}
	}
	
	// Конвертація у WebVTT-еквіваленти
	o.propagateTTMLAttributes()
	return
}
```

### 🔧 Внутрішній → вихідний формат:

```go
func ttmlOutStyleAttributesFromStyleAttributes(s *StyleAttributes) TTMLOutStyleAttributes {
	if s == nil { return TTMLOutStyleAttributes{} }
	
	var color *string
	if s.TTMLColor != nil {
		// Конвертація у HTML-формат (#RRGGBB)
		color = astikit.StrPtr(s.TTMLColor.HTMLString())
	}
	var backgroundColor *string
	if s.TTMLBackgroundColor != nil {
		backgroundColor = astikit.StrPtr(s.TTMLBackgroundColor.HTMLString())
	}
	
	return TTMLOutStyleAttributes{
		BackgroundColor: backgroundColor,
		Color:           color,
		Direction:       s.TTMLDirection,
		// ... інші атрибути ...
	}
}
```

### 🎯 Чому така трирівнева система стилів?

```
Проблема: різні формати використовують різні представлення стилів:
• TTML: tts:color="#FF0000", tts:origin="0% 80%"
• WebVTT: <c.red>, position:90%,line-left
• Внутрішній формат: універсальне представлення для обробки

Рішення: трирівнева конвертація:
1. TTMLInStyleAttributes → StyleAttributes (парсинг + нормалізація)
2. StyleAttributes → WebVTT/інші формати (експорт)
3. StyleAttributes → TTMLOutStyleAttributes (серіалізація назад у TTML)

Переваги:
• Уніфікована обробка стилів незалежно від вхідного формату
• Автоматична конвертація між форматами (TTML ↔ WebVTT ↔ STL)
• Збереження всіх атрибутів без втрати інформації

У вашому пайплайні: це дозволяє імпортувати субтитри у будь-якому форматі,
обробляти їх у єдиному внутрішньому представленні, та експортувати
у потрібний формат без втрати стилів та позиціонування.
```

---

## 🐞 7. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Відсутня валідація посилань на стилі/регіони**:
   ```go
   // У парсингу: перевірка існування стилю/регіону
   if _, ok := o.Styles[ts.Style]; !ok {
       err = fmt.Errorf("astisub: Style %s requested by subtitle... doesn't exist", ts.Style)
       return
   }
   // Але що якщо стиль/регіон визначено пізніше у файлі?
   // Поточна реалізація не підтримує forward references
   
   // Краще: два проходи парсингу або відкладена валідація
   ```

2. **Необроблений випадок циклічних посилань у стилях**:
   ```go
   // Якщо стиль A посилається на B, а B на A → нескінченний цикл при наслідуванні
   // Краще: детекція циклів через visited map або обмеження глибини наслідування
   ```

3. **Витік пам'яті при глибокій вкладеності тегів**:
   ```go
   // У парсингу текстових елементів:
   itemInlineStyle.merge(divInlineStyle)
   // Якщо вкладеність велика (наприклад, 100 рівнів), копіювання атрибутів стає дорогим
   // Краще: використовувати посилання або immutable структури для атрибутів
   ```

4. **Некоректна обробка невалідних часових міток**:
   ```go
   // У TTMLInDuration.UnmarshalText():
   d.d, err = parseDuration(text, ".", 3)
   // Якщо parseDuration повертає помилку, вона повертається наверх
   // Але що якщо вхідний рядок частково валідний? Наприклад, "00:00:01.234.567"
   // Краще: більш суворий парсинг або логування попереджень
   ```

5. **Відсутня підтримка нових атрибутів специфікації TTML**:
   ```go
   // TTML специфікація розширюється: нові атрибути для HDR, 3D, accessibility
   // Поточна реалізація ігнорує невідомі атрибути
   // Краще: додати попередження у метрики або логування для дебагу
   ```

6. **`log.Printf` замість структурованого логування**:
   ```go
   // У парсингу: log.Printf("astisub: found another voice name...")
   // У продакшені це може залишитись непоміченим
   // Краще: додати поле `Warnings []error` у Subtitles або повертати помилки для критичних випадків
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечної конвертації кольорів
func parseColorSafely(colorStr *string) (*Color, error) {
	if colorStr == nil { return nil, nil }
	color, err := newColorFromHTMLString(*colorStr)
	if err != nil {
		return nil, fmt.Errorf("parse color %q: %w", *colorStr, err)
	}
	return color, nil
}

// 2. Детекція циклічних посилань у стилях
func validateStyleHierarchy(styles map[string]*Style) error {
	visited := make(map[string]bool)
	var visit func(id string) error
	visit = func(id string) error {
		if visited[id] {
			return fmt.Errorf("cyclic style reference detected at %s", id)
		}
		visited[id] = true
		if style, ok := styles[id]; ok && style.Style != nil {
			return visit(style.Style.ID)
		}
		return nil
	}
	for id := range styles {
		visited = make(map[string]bool)  // скинути для нового кореня
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// 3. Метрики для моніторингу парсингу
func (s *Subtitles) recordParseMetrics(lineNum int, blockName string, err error) {
	if err != nil {
		metrics.TTMLParseErrors.WithLabelValues(blockName, err.Error()).Inc()
		return
	}
	metrics.TTMLBlocksParsed.WithLabelValues(blockName).Inc()
	metrics.TTMLLineProcessed.Observe(float64(lineNum))
}

// 4. Юніт-тести для циклічних посилань
func TestReadFromTTML_CyclicStyleReference(t *testing.T) {
	testData := `<tt xmlns:tts="http://www.w3.org/ns/ttml#styling">
		<head>
			<styling>
				<style xml:id="a" style="b"/>
				<style xml:id="b" style="a"/>
			</styling>
		</head>
		<body><p>Text</p></body>
	</tt>`
	_, err := ReadFromTTML(strings.NewReader(testData))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cyclic")
}

// 5. Підтримка невідомих атрибутів через мапу
type TTMLInStyleAttributes struct {
	// ... відомі атрибути ...
	UnknownAttributes map[string]string `xml:"-,any"`  // захопити невідомі атрибути
}

// При парсингу: зберегти невідомі атрибути для подальшого використання
// При записі: додати їх назад у вихідний XML (якщо потрібно)
```

---

## 🎯 8. Інтеграція з вашим CCTV HLS Processor

### 📍 У `SubtitleImporter` — імпорт TTML субтитрів:

```go
type SubtitleImporter struct {
	framerate int  // частота кадрів відео для конвертації кадрів → час
	tickrate  int  // tickrate для конвертації тіків → час
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
		// ... конвертація у PTS для HLS ...
		
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
		Metadata: &astisub.Metadata{
			Language: gen.language,
			Title:    gen.title,
		},
	}
	
	// Створення регіонів за замовчуванням
	defaultRegion := &astisub.Region{
		ID: "default",
		InlineStyle: &astisub.StyleAttributes{
			TTMLOrigin: astikit.StrPtr("0% 80%"),
			TTMLExtent: astikit.StrPtr("100% 20%"),
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
			Region: defaultRegion,
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
	
	// Розподіл тривалостей (для оптимізації буферів)
	for _, item := range subs.Items {
		duration := item.EndAt - item.StartAt
		metrics.SubtitleDurationDistribution.Observe(float64(duration.Milliseconds()))
	}
}
```

---

## 🧭 Висновок: чому цей модуль — фундамент підтримки TTML

| Компонент | Роль у TTML обробці | Вартість помилки без нього |
|-----------|---------------------|---------------------------|
| **XML-орієнтований парсинг** | Надійне читання складних XML-структур з наслідуванням | Неправильне наслідування стилів → втрата інформації про позиціонування/кольори |
| **Кастомний декодер для `<br>`** | Коректна обробка переносів рядків у текстовому контенті | Втрата багаторядковості → нечитабельні субтитри |
| **TTMLInDuration з підтримкою різних форматів** | Гнучкий парсинг часових міток з кадрами/тіками/відносними одиницями | Розсинхронізація субтитрів з відео через помилки у конвертації часу |
| **Трирівнева система стилів** | Уніфікована обробка стилів незалежно від вхідного формату | Неможливість конвертації між TTML/WebVTT/STL без втрати стилів |
| **Простори імен у вихідному TTML** | Генерація валідних файлів, сумісних зі специфікацією | Відмова валідаторів/плеєрів відтворювати субтитри через невалідний XML |

> 🔑 **Головна ідея**: Цей код — **універсальний адаптер** для формату TTML, який абстрагує складність парсингу, валідації та серіалізації від бізнес-логіки вашого пайплайну. Без нього кожен компонент змушений був би реалізовувати власну логіку роботи з TTML, що призвело б до дублювання коду, помилок у синхронізації та важкої підтримки.

💡 **Фінальна порада**: 
1. Додайте валідацію циклічних посилань у стилях та регіонах
2. Оптимізуйте копіювання атрибутів через `sync.Pool` для глибокої вкладеності
3. Замініть `log.Printf` на структуроване логування або повернення попереджень
4. Додайте підтримку невідомих атрибутів через мапу для майбутніх розширень специфікації
5. Реалізуйте метрики для моніторингу якості парсингу у продакшені

Це перетворить цей модуль з "робочої реалізації" на "гарантовано надійний фундамент" для підтримки TTML субтитрів у вашому CCTV HLS Processor.