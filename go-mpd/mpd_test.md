# 🔍 Глибокий розбір тестів: `mpd` пакет з використанням `gocheck`

Цей файл містить **інтеграційні тести** для пакету `mpd`, які перевіряють коректність парсингу та генерації MPEG-DASH MPD-файлів через **round-trip тестування** (Decode → Encode → порівняння). Розберемо архітектурно, з критичним аналізом та рекомендаціями.

---

## 📦 Архітектура тестового файлу: огляд

```
┌─────────────────────────────────────┐
│ Файл: mpd_test.go                   │
│ Фреймворк: gopkg.in/check.v1 (gocheck)│
│ Тип тестів: Інтеграційні (E2E)      │
│ Стратегія: Round-trip (Decode→Encode)│
├─────────────────────────────────────┤
│ 🔹 Компоненти:                       │
│    • MPDSuite — тестова група       │
│    • readFile() — завантаження фікстур│
│    • checkLineByLine() — порівняння  │
│    • testUnmarshalMarshal*() — шаблони│
│                                      │
│ 🔹 Тест-кейси (4):                   │
│    • TestUnmarshalMarshalVod         │
│    • TestUnmarshalMarshalLive        │
│    • TestUnmarshalMarshalLiveDelta161│
│    • TestUnmarshalMarshalSegmentTemplate│
└─────────────────────────────────────┘
```

### 🎯 Чому `gocheck` замість стандартного `testing`?
```go
// ✅ Переваги gocheck:
// • BDD-стиль: c.Assert(), c.Check(), Commentf()
// • Suite-організація: спільні хелпери, setup/teardown
// • Детальні повідомлення про помилки

// ❌ Недоліки:
// • Менш поширений → новим розробникам важче
// • Менша інтеграція з інструментами (govulncheck, staticcheck)
// • Не підтримує t.Parallel() нативно

// ✅ Сучасна альтернатива: testify + стандартний testing
// • Більш поширений, краща документація
// • assert/require API схожий на gocheck
// • Повна сумісність з go test -race, -cover, -parallel
```

---

## 🔬 Детальний розбір ключових функцій

### 1️⃣ `readFile()` — round-trip тестування з проміжним файлом

```go
func readFile(c *C, name string) (*MPD, string, string) {
    // 🎯 Читання очікуваного XML з файлу
    expected, err := ioutil.ReadFile(name)  // ⚠️ Deprecated!
    c.Assert(err, IsNil)
    
    // 🎯 Парсинг у структуру
    mpd := new(MPD)
    err = mpd.Decode(expected)
    c.Assert(err, IsNil)
    
    // 🎯 Серіалізація назад у XML
    obtained, err := mpd.Encode()
    c.Assert(err, IsNil)
    
    // 🎯 Запис у тимчасовий файл (навіщо?)
    obtainedName := name + ".ignore"
    err = ioutil.WriteFile(obtainedName, obtained, 0666)  // ⚠️ Deprecated!
    c.Assert(err, IsNil)
    
    // 🎯 Негайне видалення файлу (навіщо писали?)
    os.Remove(obtainedName)  // ⚠️ Помилка видалення ігнорується!
    
    return mpd, string(expected), string(obtained)
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Використання ioutil (deprecated у Go 1.16+):
// • ioutil.ReadFile → os.ReadFile
// • ioutil.WriteFile → os.WriteFile

// ✅ Правильно:
expected, err := os.ReadFile(name)
// ...
err = os.WriteFile(obtainedName, obtained, 0666)

// ❌ Запис у файл + негайне видалення без причини:
// • Навіщо писати `.ignore` файл, якщо він одразу видаляється?
// • Якщо для дебагу — додати прапорець DEBUG_SAVE_FILES
// • Якщо для перевірки прав запису — це не той тест

// ✅ Правильно (якщо файл потрібен для дебагу):
if os.Getenv("DEBUG_SAVE_MPD") != "" {
    obtainedName := name + ".obtained.xml"
    _ = os.WriteFile(obtainedName, obtained, 0666)  // Ігноруємо помилку для дебагу
}

// ❌ Ігнорування помилки os.Remove:
os.Remove(obtainedName)  // Якщо не видалиться → засмічення диска в CI
// ✅ Правильно:
if err := os.Remove(obtainedName); err != nil && !os.IsNotExist(err) {
    c.Logf("Warning: failed to remove temp file %s: %v", obtainedName, err)
}
```

---

### 2️⃣ `checkLineByLine()` — порівняння XML по рядках

```go
func checkLineByLine(c *C, obtained string, expected string) {
    obtainedSlice := strings.Split(strings.TrimSpace(obtained), "\n")
    expectedSlice := strings.Split(strings.TrimSpace(expected), "\n")
    c.Assert(obtainedSlice, HasLen, len(expectedSlice))  // ⚠️ Паніка при невідповідності!
    
    for i := range obtainedSlice {
        c.Check(obtainedSlice[i], Equals, expectedSlice[i], Commentf("line %d", i+1))
    }
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Порівняння по рядках для XML — крихке:
// • Зміна порядку атрибутів: <MPD a="1" b="2"> vs <MPD b="2" a="1"> → провал
// • Зміна відступів/пробілів → провал
// • Різні переноси рядків (\n vs \r\n) → провал

// ✅ Кращі підходи для порівняння XML:
// Варіант А: Нормалізувати обидва XML перед порівнянням
func normalizeXML(xml string) string {
    // • Видалити зайві пробіли
    // • Відсортувати атрибути в тегах
    // • Уніфікувати переноси рядків
    // • Видалити коментарі
}

// Варіант Б: Парсити обидва XML у дерева та порівнювати структури
import "encoding/xml"

func assertXMLEqual(c *C, obtained, expected string) {
    var o, e interface{}  // або конкретні типи
    c.Assert(xml.Unmarshal([]byte(obtained), &o), IsNil)
    c.Assert(xml.Unmarshal([]byte(expected), &e), IsNil)
    c.Assert(o, DeepEquals, e)  // Порівняння структур, не тексту
}

// Варіант В: Використовувати спеціалізовані бібліотеки
// • github.com/kylelemons/godebug/pretty
// • github.com/google/go-cmp/cmp з налаштуваннями для XML

// ❌ c.Assert з HasLen панікує при невідповідності → решта тесту не виконується
// ✅ Використовувати c.Check для продовження тесту після помилки:
c.Check(obtainedSlice, HasLen, len(expectedSlice))  // Не панікує
if len(obtainedSlice) != len(expectedSlice) {
    return  // Не продовжувати порівняння рядків
}
```

---

### 3️⃣ `testUnmarshalMarshalElemental()` / `testUnmarshalMarshalAkamai()` — шаблони з "хаками"

```go
func testUnmarshalMarshalElemental(c *C, name string) {
    _, expected, obtained := readFile(c, name)
    
    // 🎯 "Видалення дурниць" з XML — крихкі рядкові заміни!
    expected = strings.Replace(expected, `xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" `, ``, 1)
    expected = strings.Replace(expected, `xsi:schemaLocation="urn:mpeg:dash:schema:mpd:2011 http://standards.iso.org/ittf/PubliclyAvailableStandards/MPEG-DASH_schema_files/DASH-MPD.xsd" `, ``, 1)
    
    checkLineByLine(c, obtained, expected)
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ strings.Replace для видалення XML-атрибутів — надзвичайно крихкий:
// • Зміна порядку атрибутів у вихідному файлі → заміна не спрацює
// • Додаткові пробіли/переноси → заміна не спрацює
// • Різні версії специфікації → інші URL у schemaLocation

// ✅ Правильні підходи:
// Варіант А: Використовувати XML-парсер для видалення атрибутів
func stripXMLNamespaces(xmlStr string) (string, error) {
    var buf bytes.Buffer
    decoder := xml.NewDecoder(strings.NewReader(xmlStr))
    encoder := xml.NewEncoder(&buf)
    
    for {
        token, err := decoder.Token()
        if err == io.EOF {
            break
        }
        if err != nil {
            return "", err
        }
        
        if startElem, ok := token.(xml.StartElement); ok {
            // Видалити атрибути xmlns:xsi та xsi:*
            filtered := make([]xml.Attr, 0, len(startElem.Attr))
            for _, attr := range startElem.Attr {
                if attr.Name.Space != "xmlns" && attr.Name.Local != "schemaLocation" {
                    filtered = append(filtered, attr)
                }
            }
            startElem.Attr = filtered
            token = startElem
        }
        encoder.EncodeToken(token)
    }
    encoder.Flush()
    return buf.String(), nil
}

// Варіант Б: Прийняти, що парсер додає ці атрибути, і порівнювати з ними
// • Додати їх у очікуваний вивід фікстур
// • Або ігнорувати при порівнянні через нормалізацію

// ❌ Дублювання коду між Elemental та Akamai функціями:
// • Різні рядки для заміни → важко підтримувати
// ✅ Винести спільну логіку в параметризовану функцію:
func testUnmarshalMarshal(c *C, name string, xmlCleaners []func(string) string) {
    _, expected, obtained := readFile(c, name)
    for _, cleaner := range xmlCleaners {
        expected = cleaner(expected)
    }
    checkLineByLine(c, obtained, expected)
}
```

---

### 4️⃣ Тест-методи: `TestUnmarshalMarshal*`

```go
func (s *MPDSuite) TestUnmarshalMarshalVod(c *C) {
    testUnmarshalMarshalElemental(c, "fixtures/elemental_delta_vod.mpd")
}
// ... аналогічно для інших тестів ...
```

#### ⚠️ Потенційні проблеми
```go
// ❌ Жорстко закодовані шляхи до фікстур:
// • "fixtures/..." → залежить від поточної директорії
// • При запуску з іншої директорії → файл не знайдено

// ✅ Правильно: використовувати embed або відносні шляхи через testing.T
//go:embed fixtures/*.mpd
var testFixtures embed.FS

func (s *MPDSuite) TestUnmarshalMarshalVod(c *C) {
    content, err := testFixtures.ReadFile("fixtures/elemental_delta_vod.mpd")
    c.Assert(err, IsNil)
    // ... парсити з memory, не з файлу ...
}

// ❌ Відсутність паралельного виконання:
// • 4 тести × ~100мс кожен → 400мс послідовно
// ✅ Додати паралельність (якщо gocheck підтримує, або перейти на testify)

// ❌ Немає перевірки на відсутність фікстур:
// • Якщо fixtures/ видалено → тест падає з незрозумілою помилкою
// ✅ Додати перевірку:
func (s *MPDSuite) TestUnmarshalMarshalVod(c *C) {
    if _, err := os.Stat("fixtures/elemental_delta_vod.mpd"); os.IsNotExist(err) {
        c.Skip("Fixture file not found, skipping test")
    }
    testUnmarshalMarshalElemental(c, "fixtures/elemental_delta_vod.mpd")
}
```

---

## ⚠️ Загальні проблеми тестового файлу

### 1️⃣ Застарілі залежності та ідіоми
```go
// ❌ ioutil (deprecated Go 1.16+):
import "io/ioutil"  // → використовувати os.ReadFile, os.WriteFile

// ❌ strings.Replace замість strings.ReplaceAll:
expected = strings.Replace(expected, `foo`, ``, 1)  // ОК, якщо треба 1 заміна
// Але для всіх входжень:
expected = strings.ReplaceAll(expected, `foo`, ``)  // Читабельніше

// ❌ Відсутність error wrapping:
err = mpd.Decode(expected)
c.Assert(err, IsNil)  // Якщо помилка → не зрозуміло, де саме
// ✅ Правильно:
if err := mpd.Decode(expected); err != nil {
    c.Fatalf("failed to decode %s: %v", name, err)
}
```

### 2️⃣ Відсутність негативних тестів
```go
// ❌ Тести перевіряють тільки "щасливий шлях"
// ✅ Додати тести на невалідний ввід:

func (s *MPDSuite) TestDecode_InvalidXML(c *C) {
    invalid := []byte(`<?xml version="1.0"?><MPD profiles="invalid`)  // Невалідний XML
    mpd := new(MPD)
    err := mpd.Decode(invalid)
    c.Assert(err, NotNil)  // Очікуємо помилку парсингу
}

func (s *MPDSuite) TestDecode_MissingRequiredFields(c *C) {
    minimal := []byte(`<?xml version="1.0"?><MPD></MPD>`)  // Немає profiles
    mpd := new(MPD)
    err := mpd.Decode(minimal)
    // Залежить від реалізації: чи валідує Decode обов'язкові поля?
    // Якщо ні → додати валідацію в Decode або окремий Validate() метод
}
```

### 3️⃣ Відсутність бенчмарків
```go
// ✅ Додати бенчмарк для великих MPD-файлів:
func BenchmarkMPD_Decode_Large(c *C) {
    content, err := os.ReadFile("fixtures/large_vod.mpd")
    c.Assert(err, IsNil)
    
    c.ResetTimer()
    for i := 0; i < c.N; i++ {
        mpd := new(MPD)
        err := mpd.Decode(content)
        c.Assert(err, IsNil)
    }
}

// 🚀 Запуск: go test -bench=. -benchmem
// Результат покаже, чи потрібна оптимізація парсингу
```

### 4️⃣ Відсутність тестів на сумісність версій
```go
// ❌ Фікстури можуть бути з різних версій DASH специфікації
// ✅ Додати перевірку версії в тестах:

func (s *MPDSuite) TestMPD_VersionCompatibility(c *C) {
    fixtures := []struct{
        name     string
        profiles string  // Очікуваний профіль
    }{
        {"elemental_delta_vod.mpd", "urn:mpeg:dash:profile:isoff-live:2011"},
        {"akamai_bbb_30fps.mpd", "urn:mpeg:dash:profile:isoff-live:2011"},
    }
    
    for _, f := range fixtures {
        c.Logf("Testing %s", f.name)
        mpd, _, _ := readFile(c, "fixtures/"+f.name)
        c.Assert(mpd.Profiles, Equals, f.profiles)
    }
}
```

### 5️⃣ Відсутність cleanup при паніках
```go
// ❌ Якщо тест панікує після створення .ignore файлу → файл залишиться
// ✅ Використовувати defer для гарантованого очищення:

func readFile(c *C, name string) (*MPD, string, string) {
    // ...
    obtainedName := name + ".ignore"
    defer func() {
        if err := os.Remove(obtainedName); err != nil && !os.IsNotExist(err) {
            c.Logf("Warning: failed to cleanup %s: %v", obtainedName, err)
        }
    }()
    
    err = ioutil.WriteFile(obtainedName, obtained, 0666)
    c.Assert(err, IsNil)
    // ...
}
```

---

## 🔗 Рекомендації для рефакторингу

### 🎯 Рішення 1: Міграція на стандартний `testing` + `testify`

```go
// ✅ Перехід на більш поширені інструменти:
package mpd

import (
    "os"
    "strings"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestMPD_RoundTrip_Vod(t *testing.T) {
    t.Parallel()
    
    content, err := os.ReadFile("fixtures/elemental_delta_vod.mpd")
    require.NoError(t, err, "Failed to read fixture")
    
    var mpd MPD
    err = mpd.Decode(content)
    require.NoError(t, err, "Failed to decode MPD")
    
    obtained, err := mpd.Encode()
    require.NoError(t, err, "Failed to encode MPD")
    
    // 🎯 Нормалізувати обидва XML перед порівнянням
    expectedNorm := normalizeXMLForComparison(string(content))
    obtainedNorm := normalizeXMLForComparison(string(obtained))
    
    assert.Equal(t, expectedNorm, obtainedNorm, "Round-trip should produce equivalent XML")
}

// 🎯 Helper для нормалізації (спрощений приклад)
func normalizeXMLForComparison(xml string) string {
    // • Видалити xmlns:xsi атрибути
    // • Відсортувати атрибути в тегах
    // • Уніфікувати пробіли
    // Реалізація залежить від потреб
    return xml  // заглушка
}
```

### 🎯 Рішення 2: Додати embed для фікстур

```go
// ✅ Ізольованість тестів від файлової системи:
//go:embed fixtures/*.mpd
var testFixtures embed.FS

func TestMPD_RoundTrip(t *testing.T) {
    fixtures := []string{
        "fixtures/elemental_delta_vod.mpd",
        "fixtures/elemental_delta_live.mpd",
        // ...
    }
    
    for _, name := range fixtures {
        t.Run(name, func(t *testing.T) {
            t.Parallel()
            
            content, err := testFixtures.ReadFile(name)
            require.NoError(t, err)
            
            var mpd MPD
            require.NoError(t, mpd.Decode(content))
            
            obtained, err := mpd.Encode()
            require.NoError(t, err)
            
            // 🎯 Порівняння з нормалізацією
            assertXMLRoundTrip(t, content, obtained)
        })
    }
}
```

### 🎯 Рішення 3: Додати валідацію після парсингу

```go
// ✅ Гарантія, що розпаршений MPD валідний:
func (m *MPD) Validate() error {
    if m.Profiles == "" {
        return fmt.Errorf("profiles attribute is required")
    }
    if m.Type != nil && *m.Type != "static" && *m.Type != "dynamic" {
        return fmt.Errorf("invalid type: %q", *m.Type)
    }
    if len(m.Period) == 0 {
        return fmt.Errorf("at least one Period required")
    }
    // ... інші перевірки ...
    return nil
}

// Використання в тестах:
func TestMPD_Decode_Validates(c *testing.T) {
    content := []byte(`<?xml version="1.0"?><MPD></MPD>`)  // Немає profiles
    var mpd MPD
    err := mpd.Decode(content)
    require.NoError(t, err)  // Парсинг успішний (XML валідний)
    
    // Але валідація має виявити відсутність обов'язкових полів
    err = mpd.Validate()
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "profiles")
}
```

### 🎯 Рішення 4: Додати property-based тести для XML-хаков

```go
// ✅ Перевірка, що "хаки" для XML не ламають валідний контент:
func TestXMLCleaners_Safety(t *testing.T) {
    // 🎯 Згенерувати випадковий валідний XML з різними атрибутами
    // 🎯 Застосувати xmlCleaners
    // 🎯 Перевірити, що основна структура не пошкоджена
    
    // Приклад: перевірка, що видалення xmlns:xsi не видаляє інші атрибути
    input := `<MPD xmlns:xsi="http://example.com" profiles="test" custom="value"/>`
    cleaned := stripXSIAttributes(input)  // Наша функція очищення
    
    assert.Contains(t, cleaned, `profiles="test"`, "Required attribute preserved")
    assert.Contains(t, cleaned, `custom="value"`, "Custom attribute preserved")
    assert.NotContains(t, cleaned, `xmlns:xsi`, "XSI namespace removed")
}
```

---

## 🧪 Приклад: повністю рефакторинг одного тесту

```go
// ✅ TestUnmarshalMarshalVod після рефакторингу:
func TestMPD_RoundTrip_ElementalVod(t *testing.T) {
    t.Parallel()
    
    // 🎯 Читання фікстури з embed
    content, err := testFixtures.ReadFile("fixtures/elemental_delta_vod.mpd")
    require.NoError(t, err, "Failed to read fixture")
    
    // 🎯 Парсинг
    var mpd MPD
    err = mpd.Decode(content)
    require.NoError(t, err, "Decode should succeed")
    
    // 🎯 Додаткова валідація структури
    require.NotEmpty(t, mpd.Profiles, "Profiles must be set")
    require.NotEmpty(t, mpd.Period, "At least one Period required")
    
    // 🎯 Серіалізація
    obtained, err := mpd.Encode()
    require.NoError(t, err, "Encode should succeed")
    
    // 🎯 Порівняння з нормалізацією
    expectedNorm := normalizeMPDForComparison(string(content))
    obtainedNorm := normalizeMPDForComparison(string(obtained))
    
    if expectedNorm != obtainedNorm {
        // 🎯 Детальне повідомлення про відмінності
        t.Errorf("Round-trip mismatch:\nExpected:\n%s\n\nObtained:\n%s", 
            expectedNorm, obtainedNorm)
        
        // 🎯 Опціонально: зберегти файли для дебагу
        if os.Getenv("DEBUG_SAVE_MPD") != "" {
            _ = os.WriteFile("debug_expected.xml", []byte(expectedNorm), 0644)
            _ = os.WriteFile("debug_obtained.xml", []byte(obtainedNorm), 0644)
        }
    }
}

// 🎯 Нормалізація для порівняння (спрощена версія)
func normalizeMPDForComparison(xml string) string {
    // 🎯 Видалити xmlns:xsi та xsi:schemaLocation атрибути
    re := regexp.MustCompile(`\s*(xmlns:xsi="[^"]*"|xsi:schemaLocation="[^"]*")`)
    normalized := re.ReplaceAllString(xml, "")
    
    // 🎯 Уніфікувати пробіли та переноси
    normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
    normalized = strings.TrimSpace(normalized)
    
    return normalized
}
```

---

## 📋 Best Practices для тестів парсингу/генерації

```
✅ Ізоляція:
   • Використовувати embed для фікстур
   • Кожен тест самодостатній
   • Уникати спільного стану між тестами

✅ Надійність порівняння:
   • Нормалізувати XML перед порівнянням
   • Порівнювати структури, а не текст (якщо можливо)
   • Додавати детальні повідомлення при провалі

✅ Обробка помилок:
   • Використовувати require для критичних кроків
   • Додавати контекст до повідомлень про помилки
   • Не ігнорувати помилки cleanup

✅ Продуктивність:
   • Додати t.Parallel() де можливо
   • Використовувати бенчмарки для великих файлів
   • Кешувати розпаршені фікстури якщо потрібно

✅ Підтримка:
   • Документувати, чому потрібні "хаки" для очищення XML
   • Додати коментарі про специфіку фікстур (Elemental vs Akamai)
   • Використовувати table-driven тести для подібних кейсів

✅ Розширюваність:
   • Параметризувати функції очищення XML
   • Додати інтерфейс для різних стратегій порівняння
   • Підготувати міграцію з gocheck на testify
```

---

## 🎯 Висновок

Ці тести — **функціональні, але крихкі** інтеграційні перевірки:

✅ Покривають основні сценарії: VOD, live, різні генератори (Elemental, Akamai)  
✅ Перевіряють round-trip коректність (Decode → Encode)  
✅ Використовують реальні фікстури з продакшену

**Критичні виправлення перед використанням в CI/CD**:

1. ✅ **Замінити `ioutil` на `os`** (Go 1.16+ сумісність)
2. ✅ **Прибрати зайвий запис/видалення файлів** або додати прапорець для дебагу
3. ✅ **Замінити рядкові заміни на нормалізацію через XML-парсер**
4. ✅ **Додати `t.Parallel()`** або мігрувати на `testify` для кращої паралельності
5. ✅ **Використовувати `embed` для фікстур** замість файлової системи
6. ✅ **Додати негативні тести** на невалідний XML та відсутні обов'язкові поля
7. ✅ **Додати валідацію `MPD.Validate()`** для перевірки обов'язкових полів після парсингу

**Приклад міграції на сучасні інструменти**:
```go
// 🎯 План міграції:
// 1. Додати testify: go get github.com/stretchr/testify
// 2. Переписати один тест як приклад
// 3. Поступово мігрувати решту
// 4. Видалити залежність від gocheck

// 🎯 Переваги після міграції:
// • Стандартний `go test` без додаткових налаштувань
// • Краща інтеграція з IDE та CI/CD
// • Більша спільнота та документація
// • Легше залучати нових розробників
```

Потрібно допомогти з:
- 🔗 Рефакторингом тестів на стандартний `testing` + `testify`?
- 🧠 Реалізацією надійної нормалізації XML для порівняння?
- 🧪 Додаванням property-based тестів для перевірки стійкості парсингу?

Чекаю на ваші питання! 🛠️🧪📊