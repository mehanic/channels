# 🔍 Глибокий розбір коду: `ConditionalUint` для MPD (MPEG-DASH)

Цей код реалізує **гібридний тип даних** для парсингу та серіалізації атрибутів MPEG-DASH MPD-файлів, які за специфікацією можуть бути або цілим числом (`unsignedInt`), або булевим значенням (`boolean`). Розберемо архітектурно та детально.

---

## 📦 Контекст: навіщо потрібен `ConditionalUint`?

### Контекст: MPEG-DASH MPD специфікація
У стандартах MPEG-DASH (ISO/IEC 23009-1) деякі атрибути мають тип **`ConditionalUintType`** — union-тип, який може приймати:
- **Ціле число** (наприклад, `duration="30"`, `repeat="5"`)
- **Булеве значення** (наприклад, `infinite="true"`, `enabled="false"`)

### Приклади використання в MPD
```xml
<!-- Числове значення: повторити 5 разів -->
<S t="0" d="4000" r="5"/>

<!-- Булеве значення: нескінченне повторення -->
<S t="0" d="4000" r="infinite"/>  <!-- або r="true" залежно від версії -->

<!-- Атрибут відсутній = дефолтне значення (зазвичай "0" або "false") -->
<S t="0" d="4000"/>
```

### 🎯 Чому не просто `interface{}`?
```go
// ❌ interface{} втрачає type-safety:
type BadAttr struct {
    Value interface{} `xml:"r,attr"`
}
// • Неможливо перевірити тип на етапі компіляції
// • Легко помилитися при присвоєнні
// • Складна серіалізація/десеріалізація

// ✅ ConditionalUint: чіткий контракт
type ConditionalUint struct {
    u *uint64  // числове значення
    b *bool    // булеве значення
}
// • Тільки один з полів може бути встановлений
// • Type-safe доступ через методи
// • Чітка логіка маршалінгу/анмаршалінгу
```

---

## 🔬 Детальний розбір реалізації

### Структура типу
```go
type ConditionalUint struct {
    u *uint64  // ✅ Покажчик: nil = значення не встановлено
    b *bool    // ✅ Покажчик: nil = значення не встановлено
}
```

#### 🎯 Чому покажчики, а не значення?
```go
// ✅ Покажчики дозволяють розрізняти три стани:
// 1. u != nil → числове значення встановлено (напр. 42)
// 2. b != nil → булеве значення встановлено (напр. true)
// 3. u == nil && b == nil → атрибут відсутній/дефолт

// ❌ Значення не дозволяють стан "не встановлено":
type BadConditionalUint struct {
    u uint64  // Завжди має значення (0 за замовчуванням)
    b bool    // Завжди має значення (false за замовчуванням)
}
// • Як відрізнити "користувач вказав 0" від "користувач не вказав нічого"?
// • Неможливо без додаткових прапорців
```

---

### Метод `MarshalXMLAttr` — серіалізація в XML

```go
func (c ConditionalUint) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
    // 🎯 Пріоритет 1: числове значення
    if c.u != nil {
        return xml.Attr{
            Name:  name,
            Value: strconv.FormatUint(*c.u, 10),  // Десятковий формат
        }, nil
    }

    // 🎯 Пріоритет 2: булеве значення
    if c.b != nil {
        return xml.Attr{
            Name:  name,
            Value: strconv.FormatBool(*c.b),  // "true" або "false"
        }, nil
    }

    // 🎯 Обидва nil → атрибут не виводиться
    // Клієнт (парсер) інтерпретує відсутність як дефолтне значення
    return xml.Attr{}, nil
}
```

#### 🎯 Приклади серіалізації
| Стан `ConditionalUint` | XML-вивід | Пояснення |
|----------------------|-----------|-----------|
| `u = pointer(42)` | `attr="42"` | Числове значення |
| `b = pointer(true)` | `attr="true"` | Булеве значення |
| `u = nil, b = nil` | *(атрибут відсутній)* | Дефолтне значення |

#### ⚠️ Потенційна проблема: порядок перевірок
```go
// ❌ Якщо обидва поля встановлені (помилка користувача):
c := ConditionalUint{u: pointer(42), b: pointer(true)}
// → Поверне числове значення "42", ігноруючи булеве
// → Може призвести до неочікуваної поведінки

// ✅ Додати валідацію для відлову помилок розробника:
func (c ConditionalUint) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
    if c.u != nil && c.b != nil {
        return xml.Attr{}, fmt.Errorf("ConditionalUint: both uint and bool set (mutually exclusive)")
    }
    // ... решта коду ...
}
```

---

### Метод `UnmarshalXMLAttr` — десеріалізація з XML

```go
func (c *ConditionalUint) UnmarshalXMLAttr(attr xml.Attr) error {
    // 🎯 Спроба 1: парсинг як uint64
    u, err := strconv.ParseUint(attr.Value, 10, 64)
    if err == nil {
        c.u = &u
        return nil
    }

    // 🎯 Спроба 2: парсинг як bool
    b, err := strconv.ParseBool(attr.Value)
    if err == nil {
        c.b = &b
        return nil
    }

    // 🎯 Обидві спроби не вдалися → помилка
    return fmt.Errorf("ConditionalUint: can't UnmarshalXMLAttr %#v", attr)
}
```

#### 🎯 Логіка парсингу: чому спочатку uint, потім bool?
```go
// 📋 Приклади вхідних даних:
// • "42" → ParseUint успіх → c.u = 42 ✅
// • "true" → ParseUint помилка → ParseBool успіх → c.b = true ✅
// • "false" → ParseUint помилка → ParseBool успіх → c.b = false ✅
// • "infinite" → обидві помилки → повертаємо error ✅

// ⚠️ Потенційна колізія: чи може булеве значення бути розпаршене як число?
// • "0" → ParseUint успіх → c.u = 0 (а не c.b = false!)
// • "1" → ParseUint успіх → c.u = 1 (а не c.b = true!)

// 🎯 Це ПРАВИЛЬНА поведінка за специфікацією:
// • Числа мають пріоритет над булевими значеннями
// • "0" і "1" інтерпретуються як числа, не як булеві
// • Тільки явні "true"/"false" (case-insensitive) парсяться як bool
```

#### ⚠️ Потенційна проблема: неявні перетворення
```go
// ❌ Користувач може очікувати, що "0" = false, "1" = true
// Але за поточною реалізацією:
attr := xml.Attr{Value: "0"}
var c ConditionalUint
c.UnmarshalXMLAttr(attr)
// → c.u = 0, c.b = nil  (не c.b = false!)

// ✅ Якщо потрібна семантика "0/1 = false/true", додати документацію або змінити логіку:
// • Варіант А: Документувати поточну поведінку
// • Варіант Б: Додати прапорець для режиму сумісності
// • Варіант В: Змінити порядок: спочатку bool, потім uint (але це зламає існуючі дані)
```

---

### Перевірка інтерфейсів (compile-time checks)

```go
var (
    _ xml.MarshalerAttr   = ConditionalUint{}    // ✅ Перевірка Marshal
    _ xml.UnmarshalerAttr = &ConditionalUint{}   // ✅ Перевірка Unmarshal
)
```

#### 🎯 Навіщо ці перевірки?
```go
// ✅ Compile-time гарантія: якщо сигнатура методів зміниться → помилка компіляції
// ✅ Документація: чітко видно, які інтерфейси реалізовані
// ✅ Безпека: уникнення runtime-панік через невідповідність інтерфейсів

// 🔄 Приклад: якщо видалити MarshalXMLAttr → компілятор покаже:
// "cannot use ConditionalUint{} (value of type ConditionalUint) as xml.MarshalerAttr value"
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність публічних методів для доступу до значень
```go
// ❌ Поля u та b приватні, немає методів для читання:
c := ConditionalUint{u: pointer(42)}
// ❌ Користувач не може дізнатися, яке значення встановлено!

// ✅ Додати публічні методи-гетери:
func (c ConditionalUint) Uint() (uint64, bool) {
    if c.u != nil {
        return *c.u, true
    }
    return 0, false
}

func (c ConditionalUint) Bool() (bool, bool) {
    if c.b != nil {
        return *c.b, true
    }
    return false, false
}

func (c ConditionalUint) IsSet() bool {
    return c.u != nil || c.b != nil
}

// Використання:
if val, ok := c.Uint(); ok {
    fmt.Printf("Number: %d\n", val)
} else if val, ok := c.Bool(); ok {
    fmt.Printf("Boolean: %t\n", val)
} else {
    fmt.Println("Attribute not set")
}
```

### 2️⃣ Відсутність методів для встановлення значень
```go
// ❌ Немає зручного способу створити ConditionalUint:
// Користувач має робити:
c := ConditionalUint{u: func() *uint64 { v := uint64(42); return &v }()}

// ✅ Додати конструктори:
func ConditionalUintFromUint(v uint64) ConditionalUint {
    return ConditionalUint{u: &v}
}

func ConditionalUintFromBool(v bool) ConditionalUint {
    return ConditionalUint{b: &v}
}

func ConditionalUintEmpty() ConditionalUint {
    return ConditionalUint{}  // Обидва nil
}

// Використання:
c1 := ConditionalUintFromUint(42)
c2 := ConditionalUintFromBool(true)
c3 := ConditionalUintEmpty()
```

### 3️⃣ Відсутність валідації при маршалінгу
```go
// ❌ Якщо обидва поля встановлені (помилка розробника) → неочікувана поведінка
// ✅ Додати валідацію:
func (c ConditionalUint) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
    if c.u != nil && c.b != nil {
        return xml.Attr{}, fmt.Errorf("ConditionalUint: mutually exclusive fields both set")
    }
    // ... решта коду ...
}
```

### 4️⃣ Неінформативна помилка при анмаршалінгу
```go
// ❌ Поточна помилка:
return fmt.Errorf("ConditionalUint: can't UnmarshalXMLAttr %#v", attr)
// → "ConditionalUint: can't UnmarshalXMLAttr xml.Attr{Name:xml.Name{...}, Value:\"invalid\"}"

// ✅ Покращити читабельність:
return fmt.Errorf("ConditionalUint: invalid value %q for attribute %q: expected uint or bool", 
    attr.Value, attr.Name.Local)
```

### 5️⃣ Відсутність тестів
```go
// ✅ Додати юніт-тести для покриття всіх сценаріїв:
func TestConditionalUint_Marshal(t *testing.T) {
    tests := []struct{
        name     string
        input    ConditionalUint
        expected string  // Очікуваний XML-атрибут або "" для відсутності
    }{
        {"Uint", ConditionalUintFromUint(42), `name="42"`},
        {"BoolTrue", ConditionalUintFromBool(true), `name="true"`},
        {"BoolFalse", ConditionalUintFromBool(false), `name="false"`},
        {"Empty", ConditionalUintEmpty(), ``},  // Порожній атрибут
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            attr, err := tt.input.MarshalXMLAttr(xml.Name{Local: "name"})
            require.NoError(t, err)
            
            if tt.expected == "" {
                assert.Equal(t, xml.Attr{}, attr)  // Порожній атрибут
            } else {
                assert.Equal(t, tt.expected, attr.String())
            }
        })
    }
}

func TestConditionalUint_Unmarshal(t *testing.T) {
    tests := []struct{
        name     string
        input    string
        wantUint *uint64
        wantBool *bool
        wantErr  bool
    }{
        {"Uint", "42", pointer(42), nil, false},
        {"BoolTrue", "true", nil, pointer(true), false},
        {"BoolFalse", "false", nil, pointer(false), false},
        {"Invalid", "invalid", nil, nil, true},
        {"Zero", "0", pointer(0), nil, false},  // 0 = uint, не bool!
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var c ConditionalUint
            err := c.UnmarshalXMLAttr(xml.Attr{Value: tt.input})
            
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                if tt.wantUint != nil {
                    assert.NotNil(t, c.u)
                    assert.Equal(t, *tt.wantUint, *c.u)
                } else {
                    assert.Nil(t, c.u)
                }
                // ... аналогічно для bool ...
            }
        })
    }
}
```

### 6️⃣ Відсутність підтримки `encoding.TextMarshaler`/`TextUnmarshaler`
```go
// ✅ Для більшої гнучкості можна реалізувати текстові інтерфейси:
func (c ConditionalUint) MarshalText() ([]byte, error) {
    if c.u != nil {
        return []byte(strconv.FormatUint(*c.u, 10)), nil
    }
    if c.b != nil {
        return []byte(strconv.FormatBool(*c.b)), nil
    }
    return []byte{}, nil  // Порожній байт-слайс = відсутнє значення
}

func (c *ConditionalUint) UnmarshalText(text []byte) error {
    return c.UnmarshalXMLAttr(xml.Attr{Value: string(text)})
}
```

---

## 🔗 Інтеграція у ваш MPEG-DASH MPD парсер

З урахуванням вашої архітектури з **HLS/DASH конвертацією**:

### 🎯 Сценарій: використання в структурі `SegmentTimeline`
```go
// У структурі, що відповідає <S> елементу MPD:
type SegmentTimelineS struct {
    T *uint64          `xml:"t,attr,omitempty"`  // Початковий час
    D uint64           `xml:"d,attr"`            // Тривалість (обов'язкова)
    R ConditionalUint  `xml:"r,attr"`            // Повторення: число або "infinite"
}

// 🎯 Приклад парсингу:
// <S t="0" d="4000" r="5"/> → R = ConditionalUintFromUint(5)
// <S t="0" d="4000" r="infinite"/> → помилка парсингу (потрібно обробити "infinite")

// ✅ Розширення для підтримки "infinite":
type RepeatCount struct {
    ConditionalUint
    Infinite bool  // Додатковий прапорець для "infinite"
}

func (rc *RepeatCount) UnmarshalXMLAttr(attr xml.Attr) error {
    if attr.Value == "infinite" {
        rc.Infinite = true
        return nil
    }
    return rc.ConditionalUint.UnmarshalXMLAttr(attr)
}
```

### 🎯 Сценарій: генерація MPD з повтореннями
```go
// У генераторі MPD для live-стріму:
func generateSegmentTimeline(segments []Segment) []SegmentTimelineS {
    var timeline []SegmentTimelineS
    
    for _, seg := range segments {
        s := SegmentTimelineS{
            T: pointer(seg.StartTime),
            D: seg.Duration,
        }
        
        // 🎯 Встановлення повторень:
        if seg.RepeatCount >= 0 {
            s.R = ConditionalUintFromUint(uint64(seg.RepeatCount))
        } else {
            // Для нескінченних повторень у live:
            // Потрібно розширити ConditionalUint або використовувати окремий тип
            s.R = ConditionalUintFromBool(false)  // Або інше значення за домовленістю
        }
        
        timeline = append(timeline, s)
    }
    return timeline
}
```

### 🎯 Сценарій: валідація перед серіалізацією
```go
// У валідаторі MPD перед записом у файл:
func validateSegmentTimeline(s *SegmentTimelineS) error {
    // 🎯 Перевірка, що D > 0 (обов'язковий)
    if s.D == 0 {
        return fmt.Errorf("SegmentTimeline S element: duration (d) must be positive")
    }
    
    // 🎯 Перевірка R: якщо булеве, то тільки для певних сценаріїв
    if _, ok := s.R.Bool(); ok {
        // Булеве значення для r= має специфічне значення в специфікації
        // Перевірити контекст використання
        if !isValidBooleanRepeatContext(s) {
            return fmt.Errorf("boolean repeat value not allowed in this context")
        }
    }
    
    return nil
}
```

---

## 🧪 Приклад: повний набір тестів для `ConditionalUint`

```go
// ✅ Комплексні тести з покриттям всіх сценаріїв:
func TestConditionalUint(t *testing.T) {
    t.Parallel()
    
    t.Run("Marshal/Uint", func(t *testing.T) {
        t.Parallel()
        c := ConditionalUintFromUint(12345)
        attr, err := c.MarshalXMLAttr(xml.Name{Local: "test"})
        require.NoError(t, err)
        assert.Equal(t, `test="12345"`, attr.String())
    })
    
    t.Run("Marshal/BoolTrue", func(t *testing.T) {
        t.Parallel()
        c := ConditionalUintFromBool(true)
        attr, err := c.MarshalXMLAttr(xml.Name{Local: "test"})
        require.NoError(t, err)
        assert.Equal(t, `test="true"`, attr.String())
    })
    
    t.Run("Marshal/BoolFalse", func(t *testing.T) {
        t.Parallel()
        c := ConditionalUintFromBool(false)
        attr, err := c.MarshalXMLAttr(xml.Name{Local: "test"})
        require.NoError(t, err)
        assert.Equal(t, `test="false"`, attr.String())
    })
    
    t.Run("Marshal/Empty", func(t *testing.T) {
        t.Parallel()
        c := ConditionalUintEmpty()
        attr, err := c.MarshalXMLAttr(xml.Name{Local: "test"})
        require.NoError(t, err)
        assert.Equal(t, xml.Attr{}, attr)  // Порожній атрибут = не виводиться
    })
    
    t.Run("Unmarshal/Uint", func(t *testing.T) {
        t.Parallel()
        var c ConditionalUint
        err := c.UnmarshalXMLAttr(xml.Attr{Value: "999"})
        require.NoError(t, err)
        
        val, ok := c.Uint()
        assert.True(t, ok)
        assert.Equal(t, uint64(999), val)
        assert.False(t, c.IsBool())  // Допоміжний метод
    })
    
    t.Run("Unmarshal/BoolCaseInsensitive", func(t *testing.T) {
        t.Parallel()
        // ParseBool case-insensitive: "TRUE", "True", "true" → true
        for _, input := range []string{"true", "TRUE", "True", "false", "FALSE", "False"} {
            t.Run(input, func(t *testing.T) {
                var c ConditionalUint
                err := c.UnmarshalXMLAttr(xml.Attr{Value: input})
                require.NoError(t, err)
                
                val, ok := c.Bool()
                assert.True(t, ok)
                expected := strings.ToLower(input) == "true"
                assert.Equal(t, expected, val)
            })
        }
    })
    
    t.Run("Unmarshal/Invalid", func(t *testing.T) {
        t.Parallel()
        var c ConditionalUint
        err := c.UnmarshalXMLAttr(xml.Attr{Value: "not-a-number-or-bool"})
        assert.Error(t, err)
        assert.Contains(t, err.Error(), "invalid value")
    })
    
    t.Run("RoundTrip/Uint", func(t *testing.T) {
        t.Parallel()
        original := ConditionalUintFromUint(42)
        
        // Marshal
        attr, err := original.MarshalXMLAttr(xml.Name{Local: "r"})
        require.NoError(t, err)
        
        // Unmarshal
        var restored ConditionalUint
        err = restored.UnmarshalXMLAttr(attr)
        require.NoError(t, err)
        
        // Compare
        assert.Equal(t, original, restored)
    })
    
    t.Run("InterfaceChecks", func(t *testing.T) {
        t.Parallel()
        // Compile-time checks already verify this, but runtime check for documentation:
        var _ xml.MarshalerAttr = ConditionalUint{}
        var _ xml.UnmarshalerAttr = (*ConditionalUint)(nil)
    })
}

// ✅ Допоміжні методи для тестів (додати в основний код):
func (c ConditionalUint) IsBool() bool {
    return c.b != nil
}

func (c ConditionalUint) IsUint() bool {
    return c.u != nil
}
```

---

## 📋 Специфікація MPEG-DASH — вимоги до `ConditionalUintType`

```
✅ Тип: union of unsignedInt (xsd:unsignedInt) and boolean (xsd:boolean)
✅ Формат значень:
   • Числа: десятковий запис без знаків (0, 1, 42, 999999)
   • Булеві: "true" або "false" (case-insensitive за XML Schema)
✅ Відсутність атрибута: інтерпретується як дефолтне значення (залежить від контексту)
✅ Пріоритет парсингу: числа мають пріоритет над булевими значеннями
   • "0" → uint64(0), не bool(false)
   • "1" → uint64(1), не bool(true)
   • Тільки явні "true"/"false" парсяться як bool
✅ Використання в MPD:
   • @r (repeat) у <S> елементах SegmentTimeline
   • @duration у деяких контекстах
   • Інші атрибути, де потрібна гнучкість число/булеве
✅ Сумісність: парсери мають ігнорувати невідомі значення або повертати помилку
```

---

## 🎯 Висновок

Цей код — **елегантне рішення** для роботи з union-типами у XML:

✅ Чіткий контракт через приватні поля-покажчики  
✅ Коректна реалізація `xml.MarshalerAttr`/`UnmarshalerAttr`  
✅ Compile-time перевірка інтерфейсів

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати публічні методи-гетери: `Uint()`, `Bool()`, `IsSet()`
2. ✅ Додати конструктори: `ConditionalUintFromUint()`, `FromBool()`, `Empty()`
3. ✅ Додати валідацію при маршалінгу (обидва поля не можуть бути встановлені)
4. ✅ Покращити повідомлення про помилки при анмаршалінгу
5. ✅ Додати юніт-тести з покриттям всіх сценаріїв (включно з round-trip)
6. ✅ Розглянути підтримку "infinite" для специфічних сценаріїв DASH

**Приклад розширення для підтримки "infinite"**:
```go
// 🎯 Спеціальний тип для repeat-атрибута в SegmentTimeline:
type RepeatCount struct {
    value    ConditionalUint
    infinite bool  // Додатковий прапорець для "infinite"
}

func (rc *RepeatCount) UnmarshalXMLAttr(attr xml.Attr) error {
    if strings.ToLower(attr.Value) == "infinite" {
        rc.infinite = true
        return nil
    }
    return rc.value.UnmarshalXMLAttr(attr)
}

func (rc RepeatCount) MarshalXMLAttr(name xml.Name) (xml.Attr, error) {
    if rc.infinite {
        return xml.Attr{Name: name, Value: "infinite"}, nil
    }
    return rc.value.MarshalXMLAttr(name)
}

// Використання:
// <S t="0" d="4000" r="infinite"/> → RepeatCount{infinite: true}
// <S t="0" d="4000" r="5"/> → RepeatCount{value: ConditionalUintFromUint(5)}
```

Потрібно допомогти з:
- 🔗 Реалізацією публічних методів-гетерів та конструкторів для `ConditionalUint`?
- 🧠 Розширенням типу для підтримки "infinite" у MPEG-DASH SegmentTimeline?
- 🧪 Написанням повного набору юніт-тестів з property-based testing для крайніх випадків?

Чекаю на ваші питання! 🛠️📊🎬