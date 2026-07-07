# 🧪 Глибокий розбір `astisub_test/teletext_test.go` — тестування парсингу телетексту

Це **набір юніт-тестів** для валідації парсингу субтитрів у форматі телетексту (Teletext) — застарілого, але досі використовуваного формату для мовлення в Європі. Тести покривають критичні шляхи: детекція типу даних, парсинг сторінок, обробка кольорів та стилів, конвертація у внутрішній формат. Розберемо архітектурно:

---

## 🧱 1. Архітектура тестування: акцент на бітову логіку телетексту

### 🔧 Використання testify для читабельних перевірок:

```go
import (
    "github.com/stretchr/testify/assert"  // продовжує тест після помилки
)

// Приклад перевірки мапінгу типів даних:
assert.Equal(t, map[int]string{19: teletextPESDataTypeEBU, ...}, m)
```

### 🎯 Чому тести зосереджені на бітовій логіці?

```
Телетекст — це бінарний формат з жорсткою специфікацією:
• Кожен байт має специфічне значення (колір, стиль, керування)
• Пакети передаються у транспортному потоці MPEG-TS
• Дані кодуються у 7-бітному форматі з парністю

Без ретельного тестування:
• Помилки у парсингу байтів → неправильні кольори/стилі
• Неправильна обробка керуючих кодів → зламана структура субтитрів
• Несумісність з реальними телетекст-потоками від мовників
```

---

## 🔍 2. `TestTeletextPESDataType` — мапінг типів даних у PES пакетах

### 🔧 Логіка тесту:

```go
func TestTeletextPESDataType(t *testing.T) {
	m := make(map[int]string)
	for i := 0; i < 255; i++ {
		t := teletextPESDataType(uint8(i))
		if t != teletextPESDataTypeUnknown {
			m[i] = t
		}
	}
	assert.Equal(t, map[int]string{
		19: teletextPESDataTypeEBU, 20: teletextPESDataTypeEBU, 
		21: teletextPESDataTypeEBU, 26: teletextPESDataTypeEBU, 
		28: teletextPESDataTypeEBU, 17: teletextPESDataTypeEBU, 
		27: teletextPESDataTypeEBU, 31: teletextPESDataTypeEBU, 
		16: teletextPESDataTypeEBU, 18: teletextPESDataTypeEBU, 
		23: teletextPESDataTypeEBU, 29: teletextPESDataTypeEBU, 
		22: teletextPESDataTypeEBU, 24: teletextPESDataTypeEBU, 
		25: teletextPESDataTypeEBU, 30: teletextPESDataTypeEBU,
	}, m)
}
```

### 🎯 Що перевіряється:

```
Функція `teletextPESDataType(uint8)` визначає тип даних у телетекст-пакеті:
• Вхід: байт (0-255) з заголовку PES пакета
• Вихід: тип даних (наприклад, teletextPESDataTypeEBU) або unknown

Очікувана поведінка:
• Тільки 16 значень (16-31) повертають EBU тип
• Решта 240 значень повертають unknown

Чому саме 16-31?
• Специфікація ETSI EN 300 706 визначає діапазон 0x10-0x1F для телетексту
• Це дозволяє відрізняти телетекст-дані від інших типів у транспортному потоці

У вашому CCTV HLS Processor:
• Ця функція використовується для фільтрації телетекст-пакетів у транспортному потоці
• Неправильний мапінг призведе до пропуску телетекст-субтитрів або обробки сміття
```

---

## 📄 3. `TestTeletextPageParse` — парсинг сторінки телетексту

### 🔧 Структура тесту:

```go
func TestTeletextPageParse(t *testing.T) {
	// 1. Створення тестової сторінки
	p := newTeletextPage(0, time.Unix(10, 0))  // номер=0, кінець=10с
	p.end = time.Unix(15, 0)                    // оновлення кінця
	p.rows = []int{2, 1}                        // порядок рядків: спочатку 2, потім 1
	p.data = map[uint8][]byte{
		1: append([]byte{0xb}, []byte("test1")...),  // рядок 1: 0x0b + "test1"
		2: append([]byte{0xb}, []byte("test2")...),  // рядок 2: 0x0b + "test2"
	}
	
	// 2. Ініціалізація декодера та субтитрів
	s := Subtitles{}
	d := newTeletextCharacterDecoder()
	d.updateCharset(astikit.UInt8Ptr(0), false)  // встановити кодування
	
	// 3. Парсинг сторінки
	p.parse(&s, d, time.Unix(5, 0))  // початок=5с
	
	// 4. Перевірка результату
	assert.Equal(t, []*Item{{
		EndAt: 10 * time.Second,  // ← важливо: використовується початковий end, не оновлений!
		Lines: []Line{
			{Items: []LineItem{{InlineStyle: &StyleAttributes{
				TeletextSpacesAfter: astikit.IntPtr(0),
				TeletextSpacesBefore: astikit.IntPtr(0),
			}, Text: "test1"}}},
			{Items: []LineItem{{InlineStyle: &StyleAttributes{
				TeletextSpacesAfter: astikit.IntPtr(0),
				TeletextSpacesBefore: astikit.IntPtr(0),
			}, Text: "test2"}}},
		},
		StartAt: 5 * time.Second,
	}}, s.Items)
}
```

### 🎯 Ключові аспекти парсингу:

```
1. Порядок рядків (p.rows = []int{2, 1}):
   • Телетекст передає рядки не по порядку через помилки передачі
   • Парсер має зібрати рядки у правильному порядку за номерами
   • У тесті: спочатку рядок 2 ("test2"), потім рядок 1 ("test1")
   • Але результат: ["test1", "test2"] → парсер сортує за номером рядка ✓

2. Формат даних рядка:
   • 0x0b — керуючий код (наприклад, встановлення кольору/стилю)
   • "test1" — текстовий контент
   • Парсер має відокремити керуючі коди від тексту

3. Часові мітки:
   • p.newTeletextPage(0, time.Unix(10, 0)) — початковий кінець сторінки
   • p.end = time.Unix(15, 0) — оновлення кінця (ігнорується у парсингу!)
   • p.parse(&s, d, time.Unix(5, 0)) — початок субтитру
   • Результат: StartAt=5с, EndAt=10с (початковий end, не оновлений)

Чому це критично:
• Неправильний порядок рядків → переплутані субтитри
• Неправильна обробка керуючих кодів → втрата стилів/кольорів
• Неправильні часові мітки → розсинхронізація з відео
```

---

## 🎨 4. `TestParseTeletextRow` — обробка кольорів та стилів у рядку

### 🔧 Тестові дані: послідовність керуючих кодів

```go
b := []byte("start")  // початковий текст (ігнорується, бо перед першим кодом)
b = append(b, 0x0, 0xb)  // 0x00 = padding, 0x0b = код кольору чорний
b = append(b, []byte("black")...)  // текст чорним кольором
b = append(b, 0x1)  // 0x01 = код кольору червоний
b = append(b, []byte("red")...)  // текст червоним кольором
// ... аналогічно для інших кольорів (зелений, жовтий, синій, пурпурний, блакитний, білий)
b = append(b, 0xd)  // 0x0d = подвійна висота
b = append(b, []byte("double height")...)
b = append(b, 0xe)  // 0x0e = подвійна ширина
b = append(b, []byte("double width")...)
b = append(b, 0xf)  // 0x0f = подвійний розмір (висота+ширина)
b = append(b, []byte("double size")...)
b = append(b, 0xc)  // 0x0c = скидання стилів
b = append(b, []byte("reset")...)  // текст без стилів
b = append(b, 0xa)  // 0x0a = кінець рядка
b = append(b, []byte("end")...)  // текст після кінця (ігнорується)
```

### 🔧 Очікуваний результат: масив LineItem з правильними стилями

```go
assert.Equal(t, []LineItem{
	{Text: "black", InlineStyle: &StyleAttributes{
		TeletextColor: ColorBlack,
		TTMLColor: ColorBlack,  // ← конвертація у TTML-еквівалент
		// ... інші атрибути ...
	}},
	{Text: "red", InlineStyle: &StyleAttributes{
		TeletextColor: ColorRed,
		TTMLColor: ColorRed,
		// ...
	}},
	// ... інші кольори ...
	{Text: "double height", InlineStyle: &StyleAttributes{
		TeletextColor: ColorWhite,  // колір не змінився, тільки стиль
		TeletextDoubleHeight: astikit.BoolPtr(true),  // ← встановлено стиль
		// ...
	}},
	{Text: "double width", InlineStyle: &StyleAttributes{
		TeletextDoubleHeight: astikit.BoolPtr(true),  // ← збережено попередній стиль
		TeletextDoubleWidth: astikit.BoolPtr(true),   // ← додано новий стиль
		// ...
	}},
	{Text: "double size", InlineStyle: &StyleAttributes{
		TeletextDoubleHeight: astikit.BoolPtr(true),
		TeletextDoubleWidth: astikit.BoolPtr(true),
		TeletextDoubleSize: astikit.BoolPtr(true),  // ← комбінація стилів
		// ...
	}},
	{Text: "reset", InlineStyle: &StyleAttributes{
		TeletextDoubleHeight: astikit.BoolPtr(false),  // ← стилі скинуті
		TeletextDoubleWidth: astikit.BoolPtr(false),
		TeletextDoubleSize: astikit.BoolPtr(false),
		// ...
	}},
}, i.Lines[0].Items)
```

### 🎯 Чому така складна логіка стилів?

```
Телетекст використовує інкрементальну модель стилів:
• Кожен керуючий код змінює поточний стан
• Зміни застосовуються до всього наступного тексту до наступного коду
• Стилі накопичуються: подвійна висота + подвійна ширина = подвійний розмір
• Скидання (0x0c) повертає до базового стану

Приклад ланцюжка:
1. 0x0b (чорний) → текст "black" чорним
2. 0x01 (червоний) → текст "red" червоним (колір змінено)
3. 0x0d (подвійна висота) → текст "double height" червоним + подвійна висота
4. 0x0e (подвійна ширина) → текст "double width" червоним + подвійна висота + ширина
5. 0x0f (подвійний розмір) → текст "double size" червоним + подвійний розмір
6. 0x0c (скидання) → текст "reset" білим (базовий колір) + без стилів

У вашому коді: це реалізовано через `StyleAttributes`, що зберігає поточний стан:
• Кольори: TeletextColor + конвертація у TTMLColor
• Стилі: TeletextDoubleHeight, TeletextDoubleWidth, TeletextDoubleSize
• Скидання: встановлення всіх стилів у false

Без цієї логіки: втрата інформації про стилі → субтитри відображаються без форматування.
```

---

## ✂️ 5. `TestAppendTeletextLineItem` — обробка пробілів та форматування

### 🔧 Логіка тесту:

```go
func TestAppendTeletextLineItem(t *testing.T) {
	// 1. Порожній випадок
	l := Line{}
	appendTeletextLineItem(&l, LineItem{}, nil)
	assert.Equal(t, 0, len(l.Items))  // порожній елемент не додається
	
	// 2. Обробка тексту з пробілами
	appendTeletextLineItem(&l, LineItem{Text: " test  "}, nil)
	assert.Equal(t, "test", l.Items[0].Text)  // пробіли видалені
	assert.Equal(t, StyleAttributes{
		TeletextSpacesAfter:  astikit.IntPtr(2),  // 2 пробіли в кінці
		TeletextSpacesBefore: astikit.IntPtr(1),  // 1 пробіл на початку
	}, *l.Items[0].InlineStyle)
}
```

### 🎯 Чому збереження інформації про пробіли критичне?

```
Телетекст використовує пробіли для вирівнювання тексту на екрані:
• Фіксована ширина символу (моноширинний шрифт)
• Позиціонування через пробіли: "  Текст" → вирівнювання праворуч
• Розділення колонок: "Колонка1   Колонка2"

Проблема: при експорті у інші формати (WebVTT, TTML) пробіли можуть:
• Видалятись автоматично (наприклад, при HTML-екрануванні)
• Змінюватись через різну ширину символів у пропорційних шрифтах

Рішення у вашому коді:
• Видалити пробіли з тексту для чистого відображення
• Зберегти кількість пробілів до/після у атрибутах:
  • TeletextSpacesBefore: для відтворення вирівнювання у телетекст-плеєрах
  • TeletextSpacesAfter: для сумісності з оригінальним форматуванням

У вашому CCTV HLS Processor:
• При експорті у WebVTT: використовувати TeletextSpacesBefore для CSS margin-left
• При експорті у TTML: використовувати TeletextSpacesAfter для padding-right
• При відтворенні у власному плеєрі: відновлювати оригінальне форматування
```

---

## 🐞 6. Потенційні проблеми та покращення тестів

### ❗ Критичні недоліки:

1. **Відсутність тестів на невалідні керуючі коди**:
   ```go
   // Що станеться, якщо передати невідомий код (наприклад, 0xFF)?
   // Чи ігнорується він, чи повертається помилка?
   
   // Додати тест:
   func TestParseTeletextRow_UnknownControlCode(t *testing.T) {
       b := []byte{0xFF, []byte("text")...}  // невідомий код + текст
       i := Item{}
       d := newTeletextCharacterDecoder()
       parseTeletextRow(&i, d, nil, b)
       // Очікуємо: текст "text" без змін у стилях (код ігноровано)
       assert.Equal(t, "text", i.Lines[0].Items[0].Text)
       assert.Nil(t, i.Lines[0].Items[0].InlineStyle.TeletextColor)
   }
   ```

2. **Не тестується обробка помилок у декодері символів**:
   ```go
   // Що станеться, якщо декодер не може розпарсити символ?
   // Чи повертається помилка, чи ігнорується символ?
   
   // Додати тест:
   func TestTeletextCharacterDecoder_InvalidByte(t *testing.T) {
       d := newTeletextCharacterDecoder()
       d.updateCharset(astikit.UInt8Ptr(0), false)
       // Спробувати декодувати невалідний байт (наприклад, 0x7F у 7-бітному режимі)
       char, err := d.decode(0x7F)
       assert.Error(t, err)  // або assert.Equal(t, '?', char) для fallback
   }
   ```

3. **Відсутність тестів на багаторядкові субтитри**:
   ```go
   // Усі тести використовують один рядок
   // Але реальні телетекст-субтитри часто багаторядкові
   
   // Додати тест:
   func TestTeletextPageParse_MultiRow(t *testing.T) {
       p := newTeletextPage(0, time.Unix(10, 0))
       p.rows = []int{1, 2, 3}
       p.data = map[uint8][]byte{
           1: []byte("Row 1"),
           2: []byte("Row 2"),
           3: []byte("Row 3"),
       }
       s := Subtitles{}
       d := newTeletextCharacterDecoder()
       p.parse(&s, d, time.Unix(5, 0))
       
       assert.Equal(t, 3, len(s.Items[0].Lines))  // три рядки
       assert.Equal(t, "Row 1", s.Items[0].Lines[0].Items[0].Text)
       assert.Equal(t, "Row 2", s.Items[0].Lines[1].Items[0].Text)
       assert.Equal(t, "Row 3", s.Items[0].Lines[2].Items[0].Text)
   }
   ```

4. **Не тестується обробка переповнення буфера**:
   ```go
   // Телетекст має обмеження на довжину рядка (40 символів)
   // Що станеться, якщо передати довший рядок?
   
   // Додати тест:
   func TestParseTeletextRow_LongRow(t *testing.T) {
       longText := strings.Repeat("x", 100)  // 100 символів замість 40
       b := append([]byte{0xb}, []byte(longText)...)
       i := Item{}
       d := newTeletextCharacterDecoder()
       parseTeletextRow(&i, d, nil, b)
       // Очікуємо: обрізання до 40 символів або помилка
       assert.LessOrEqual(t, len(i.Lines[0].Items[0].Text), 40)
   }
   ```

5. **Відсутність тестів на конвертацію часових міток**:
   ```go
   // У TestTeletextPageParse перевіряється StartAt/EndAt,
   // але не тестується конвертація між різними форматами часу
   
   // Додати тест:
   func TestTeletextPageParse_TimeConversion(t *testing.T) {
       // Телетекст використовує власний формат часу (наприклад, BCD)
       // Перевірити конвертацію у time.Duration
       p := newTeletextPage(0, time.Unix(0, 0))
       p.end = time.Unix(0, 500000000)  // 0.5 секунди
       // ... парсинг ...
       assert.Equal(t, 500*time.Millisecond, s.Items[0].EndAt-s.Items[0].StartAt)
   }
   ```

### 💡 Покращення:

```go
// 1. Helper для генерації тестових даних з керуючими кодами
func generateTeletextRowWithCodes(codes []struct{code uint8; text string}) []byte {
    var b []byte
    for _, c := range codes {
        b = append(b, c.code)
        b = append(b, []byte(c.text)...)
    }
    return b
}

// 2. Параметризовані тести для всіх керуючих кодів
func TestParseTeletextRow_AllControlCodes(t *testing.T) {
    testCases := []struct{
        code uint8
        expectedStyle StyleAttributes
        description string
    }{
        {0x00, StyleAttributes{TeletextColor: ColorBlack}, "black"},
        {0x01, StyleAttributes{TeletextColor: ColorRed}, "red"},
        // ... інші коди ...
        {0x0c, StyleAttributes{TeletextDoubleHeight: astikit.BoolPtr(false)}, "reset"},
    }
    
    for _, tc := range testCases {
        t.Run(tc.description, func(t *testing.T) {
            b := append([]byte{tc.code}, []byte("text")...)
            i := Item{}
            d := newTeletextCharacterDecoder()
            parseTeletextRow(&i, d, nil, b)
            assert.Equal(t, tc.expectedStyle, *i.Lines[0].Items[0].InlineStyle)
        })
    }
}

// 3. Тест на стійкість до невалідних даних
func TestTeletext_Robustness(t *testing.T) {
    invalidInputs := []struct{
        name string
        data []byte
    }{
        {"empty", []byte{}},
        {"only control codes", []byte{0x00, 0x01, 0x02}},
        {"invalid charset", append([]byte{0x00}, []byte{0x80, 0x81, 0x82}...)},  // 8-бітні символи у 7-бітному режимі
    }
    
    for _, tc := range invalidInputs {
        t.Run(tc.name, func(t *testing.T) {
            // Не повинно панікувати
            defer func() {
                if r := recover(); r != nil {
                    t.Errorf("panic on input %q: %v", tc.name, r)
                }
            }()
            i := Item{}
            d := newTeletextCharacterDecoder()
            parseTeletextRow(&i, d, nil, tc.data)
            // Результат може бути порожнім, але не повинен викликати помилку
        })
    }
}

// 4. Тест на конвертацію стилів у інші формати
func TestTeletextStyleConversion(t *testing.T) {
    // Створити StyleAttributes з телетекст-стилями
    sa := &StyleAttributes{
        TeletextColor: ColorRed,
        TeletextDoubleHeight: astikit.BoolPtr(true),
    }
    
    // Конвертація у TTML
    ttml := ttmlOutStyleAttributesFromStyleAttributes(sa)
    assert.NotNil(t, ttml.Color)
    assert.Equal(t, "#FF0000", *ttml.Color)  // червоний у hex
    
    // Конвертація у WebVTT (якщо реалізовано)
    // ...
}
```

---

## 🎯 7. Інтеграція з вашим CCTV HLS Processor

### 📍 У `TeletextDemuxer` — демуксинг телетекст-пакетів з транспортного потоку:

```go
type TeletextDemuxer struct {
	pages map[uint16]*teletextPage  // номер сторінки → дані
	decoder *teletextCharacterDecoder
}

func (d *TeletextDemuxer) ProcessPESPacket(pid uint16, payload []byte, pts time.Duration) error {
	// 1. Перевірка типу даних
	dataType := teletextPESDataType(payload[0])
	if dataType != teletextPESDataTypeEBU {
		return nil  // не телетекст, ігноруємо
	}
	
	// 2. Парсинг заголовку сторінки
	pageNum := binary.BigEndian.Uint16(payload[1:3])
	page := d.pages[pageNum]
	if page == nil {
		page = newTeletextPage(pageNum, pts)
		d.pages[pageNum] = page
	}
	
	// 3. Додавання даних рядка
	rowNum := payload[3]
	page.data[rowNum] = payload[4:]  // пропустити заголовок
	
	// 4. Якщо сторінка завершена → парсинг у субтитри
	if page.isComplete() {
		subs := Subtitles{}
		page.parse(&subs, d.decoder, page.start)
		
		// 5. Передача у пайплайн
		for _, item := range subs.Items {
			d.subtitleQueue <- &SubtitleFrame{
				StartPTS: item.StartAt,
				EndPTS:   item.EndAt,
				Text:     extractTextFromItem(item),
				Styles:   convertTeletextStyles(item.InlineStyle),
			}
		}
		
		// 6. Очищення сторінки
		delete(d.pages, pageNum)
	}
	return nil
}
```

### 📍 У `HLSGenerator` — генерація субтитрів для плейлиста:

```go
func (gen *HLSSubtitleGenerator) WriteWebVTTSegment(frames []*SubtitleFrame, outputPath string) error {
	// Конвертація телетекст-стилів у WebVTT-еквіваленти
	convertTeletextToWebVTT := func(sa *StyleAttributes) *astisub.StyleAttributes {
		result := &astisub.StyleAttributes{}
		
		// Колір
		if sa.TeletextColor != nil {
			result.WebVTTColor = sa.TeletextColor.WebVTTString()
		}
		
		// Подвійний розмір → збільшення шрифту
		if sa.TeletextDoubleSize != nil && *sa.TeletextDoubleSize {
			result.WebVTTSize = "200%"  // подвійний розмір
		}
		
		// Пробіли для вирівнювання
		if sa.TeletextSpacesBefore != nil && *sa.TeletextSpacesBefore > 0 {
			// WebVTT не підтримує margin-left напряму,
			// але можна використати position: X%,line-left
			result.WebVTTPosition = &astisub.WebVTTPosition{
				XPosition: fmt.Sprintf("%d%%", 10+*sa.TeletextSpacesBefore*2),
				Alignment: "line-left",
			}
		}
		
		return result
	}
	
	// ... генерація WebVTT з використанням convertTeletextToWebVTT ...
}
```

### 📍 У метриках — моніторинг якості демуксингу:

```go
func (d *TeletextDemuxer) recordParseMetrics(pageNum uint16, err error) {
	if err != nil {
		metrics.TeletextParseErrors.WithLabelValues(fmt.Sprintf("page_%d", pageNum), err.Error()).Inc()
		return
	}
	
	metrics.TeletextPagesParsed.Inc()
	
	// Статистика по стилях
	// ... аналогічно до інших форматів ...
}
```

---

## 🧭 Висновок: чому ці тести — гарантія надійності парсингу телетексту

| Компонент | Роль у телетекст парсері | Вартість помилки без нього |
|-----------|-------------------------|---------------------------|
| **Мапінг типів даних** | Фільтрація телетекст-пакетів у транспортному потоці | Обробка сміття як субтитрів → пошкоджені дані у пайплайні |
| **Парсинг сторінок з сортуванням рядків** | Відновлення правильного порядку рядків після помилок передачі | Переплутані субтитри → нечитабельний текст |
| **Обробка керуючих кодів для стилів** | Збереження форматування (кольори, подвійний розмір) | Втрата візуальної інформації → субтитри відображаються без стилів |
| **Збереження інформації про пробіли** | Підтримка вирівнювання тексту для сумісності з оригіналом | Зламана верстка субтитрів → текст "з'їжджає" з потрібної позиції |
| **Конвертація стилів у інші формати** | Сумісність з WebVTT/TTML при експорті | Неможливість використання телетекст-субтитрів у сучасних плеєрах |

> 🔑 **Головна ідея**: Ці тести — **страховка від регресій** при зміні логіки парсингу телетексту. Вони документують:
> 1. Як обробляються керуючі коди для кольорів та стилів
> 2. Як відновлюється порядок рядків після помилок передачі
> 3. Як конвертуються стилі у інші формати для сумісності
> 4. Як парсер поводиться з невалідними/пошкодженими даними

Без них будь-яка оптимізація парсера може непомітно зламати сумісність з реальними телетекст-потоками — і ви дізнаєтесь про це тільки коли субтитри з певних мовників перестануть відтворюватись коректно.

💡 **Фінальна порада**: 
1. Додайте тести на невалідні керуючі коди та символи
2. Покрийте обробку багаторядкових субтитрів та переповнення буфера
3. Додайте тести на конвертацію стилів у WebVTT/TTML
4. Реалізуйте fuzz-тести для перевірки стійкості до випадкових/пошкоджених даних
5. Додайте метрики для моніторингу якості демуксингу телетексту у продакшені

Це перетворить ці тести з "перевірки базової функціональності" на "гарантію надійності парсингу" для всього вашого пайплайну обробки телетекст-субтитрів у CCTV HLS Processor.