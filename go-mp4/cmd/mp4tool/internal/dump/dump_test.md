# 🧪 Тест `TestDump`: Перевірка інструменту візуалізації структури MP4

Це **інтеграційний тест** для модуля `dump`, який перевіряє коректність роботи **CLI-інструменту `mp4tool dump`** — виведення людино-читабельної ієрархії боксів з підтримкою різних опцій форматування.

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що `dump` коректно виводить структуру будь-якого MP4-файлу** — з підтримкою відступів, фільтрації великих боксів, форматування розмірів (dec/hex), показу офсетів та детального вмісту за прапорцями `-full`, `-offset`, `-hex`.

---

## 📋 Структура тесту

```go
func TestDump(t *testing.T) {
    testCases := []struct {
        name    string          // 🔹 Назва тест-кейсу
        file    string          // 🔹 Шлях до тестового файлу
        options []string        // 🔹 Прапорці командного рядка
        wants   string          // 🔹 Очікуваний вивід (golden file)
    }{
        {
            name:  "sample.mp4 no-options",
            file:  "../../../../testdata/sample.mp4",
            wants: sampleMP4Output,  // 🔹 Базовий вивід без опцій
        },
        {
            name:    "sample.mp4 with -full mvhd,loci option",
            file:    "../../../../testdata/sample.mp4",
            options: []string{"-full", "mvhd,loci"},  // 🔹 Детальний вміст mvhd та loci
            wants:   sampleMP4OutputFullMvhdLoci,
        },
        {
            name:    "sample.mp4 with -offset option",
            file:    "../../../../testdata/sample.mp4",
            options: []string{"-offset"},  // 🔹 Показ офсетів
            wants:   sampleMP4OutputOffset,
        },
        {
            name:    "sample.mp4 with -hex option",
            file:    "../../../../testdata/sample.mp4",
            options: []string{"-hex"},  // 🔹 Шістнадцятковий формат розмірів
            wants:   sampleMP4OutputHex,
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // 🔹 Перехоплення stdout через pipe
            stdout := os.Stdout
            r, w, err := os.Pipe()
            require.NoError(t, err)
            defer func() { os.Stdout = stdout }()
            os.Stdout = w
            
            // 🔹 Запуск Main у горутині
            go func() {
                require.Zero(t, Main(append(tc.options, tc.file)))
                w.Close()
            }()
            
            // 🔹 Читання виводу
            b, err := io.ReadAll(r)
            require.NoError(t, err)
            
            // 🔹 Порівняння з очікуваним
            assert.Equal(t, tc.wants, string(b))
        })
    }
}
```

**🎯 Призначення**: Перевірити, що інструмент `dump`:
- ✅ Коректно обробляє різні комбінації прапорців
- ✅ Форматує вивід згідно з налаштуваннями
- ✅ Показує/приховує вміст великих боксів за правилами
- ✅ Генерує стабільний, детермінований вивід для порівняння

---

## 🔍 Детальний розбір тест-кейсів

### 🔹 Кейс 1: Базовий вивід без опцій

```go
{
    name:  "sample.mp4 no-options",
    file:  "../../../../testdata/sample.mp4",
    wants: sampleMP4Output,  // 🔹 57 рядків ієрархії
},
```

**📊 Очікуваний вивід (фрагмент):**
```
[ftyp] Size=32 MajorBrand="isom" MinorVersion=512 CompatibleBrands=[{isom}{iso2}{avc1}{mp41}]
[free] Size=8 Data=[...] (use "-full free" to show all)
[mdat] Size=6402 Data=[...] (use "-full mdat" to show all)
[moov] Size=1836
  [mvhd] Size=108 ... (use "-full mvhd" to show all)
  [trak] Size=743
    [tkhd] Size=92 ... (use "-full tkhd" to show all)
    [edts] Size=36
      [elst] Size=28 Version=0 Flags=0x000000 EntryCount=1 Entries=[{...}]
    [mdia] Size=607
      [mdhd] Size=32 Version=0 Flags=0x000000 CreationTimeV0=0 ...
      ...
```

**🔑 Ключові перевірки:**
| Елемент | Очікування | Чому це важливо |
|---------|------------|----------------|
| `[ftyp]` | Повний вміст | ✅ Маленький бокс, завжди показується повністю |
| `[free]`/`[mdat]` | `Data=[...]` | ✅ Великі бокси скорочені за замовчуванням |
| `[mvhd]`/`[tkhd]` | `... (use "-full ...")` | ✅ Великі бокси з дітьми скорочені, якщо не `-full` |
| Відступи | 2 пробіли на рівень | ✅ Читабельна ієрархія |
| `Stringify()` | `Version=0 Flags=0x000000` | ✅ Людсько-читабельне форматування полів |

---

### 🔹 Кейс 2: `-full mvhd,loci` — детальний вміст конкретних боксів

```go
{
    name:    "sample.mp4 with -full mvhd,loci option",
    options: []string{"-full", "mvhd,loci"},
    wants:   sampleMP4OutputFullMvhdLoci,
},
```

**📊 Відмінності у виводі:**

| Бокс | Без `-full` | З `-full mvhd,loci` |
|------|-------------|---------------------|
| `[mvhd]` | `... (use "-full mvhd" to show all)` | `Version=0 Flags=0x000000 CreationTimeV0=0 ... NextTrackID=3` |
| `[loci]` | `(unsupported box type) Data=[...]` | `(unsupported box type) Data=[0x00 0x00 0x00 0x00 0x15 ...]` |
| Інші бокси | Без змін | Без змін |

**🔑 Логіка обробки `-full`:**
```go
// 🔹 У dump():
_, full := m.full[h.BoxInfo.Type.String()]  // 🔹 Чи є тип у списку -full?

// 🔹 Для підтримуваних типів:
if !full && h.BoxInfo.Size-h.BoxInfo.HeaderSize >= 64 &&
    util.ShouldHasNoChildren(h.BoxInfo.Type) {
    // 🔹 Скорочений вивід для великих боксів без дітей
    return nil, nil
}

// 🔹 Для непідтримуваних типів:
if full {
    // 🔹 Читаємо сирі дані та форматуємо як [0x00 0x01 ...]
    buf := bytes.NewBuffer(...)
    h.ReadData(buf)
    fmt.Fprintf(line, " Data=[")
    for i, d := range buf.Bytes() { ... }
}
```

**🎯 Призначення**: Дозволити користувачеві отримати детальну інформацію тільки про потрібні бокси, не перевантажуючи вивід.

---

### 🔹 Кейс 3: `-offset` — показ зміщення боксів у файлі

```go
{
    name:    "sample.mp4 with -offset option",
    options: []string{"-offset"},
    wants:   sampleMP4OutputOffset,
},
```

**📊 Приклад виводу:**
```
[ftyp] Offset=0 Size=32 ...
[free] Offset=32 Size=8 Data=[...]
[mdat] Offset=40 Size=6402 Data=[...]
[moov] Offset=6442 Size=1836
  [mvhd] Offset=6450 Size=108 ...
  [trak] Offset=6558 Size=743
    [tkhd] Offset=6566 Size=92 ...
```

**🔑 Форматування:**
```go
if m.offset {
    fmt.Fprintf(line, " Offset="+sizeFormat, h.BoxInfo.Offset)
}
fmt.Fprintf(line, " Size="+sizeFormat, h.BoxInfo.Size)
```

**🎯 Призначення**: Дозволити аналізувати фізичне розташування боксів у файлі — корисно для дебагу пошкоджених файлів або оптимізації розташування даних.

---

### 🔹 Кейс 4: `-hex` — шістнадцятковий формат розмірів

```go
{
    name:    "sample.mp4 with -hex option",
    options: []string{"-hex"},
    wants:   sampleMP4OutputHex,
},
```

**📊 Приклад виводу:**
```
[ftyp] Size=0x20 MajorBrand="isom" ...
[free] Size=0x8 Data=[...]
[mdat] Size=0x1902 Data=[...]
[moov] Size=0x72c
  [mvhd] Size=0x6c ...
```

**🔑 Форматування:**
```go
sizeFormat := "%d"
if m.hex {
    sizeFormat = "0x%x"  // 🔹 Шістнадцятковий формат з префіксом 0x
}
fmt.Fprintf(line, " Size="+sizeFormat, h.BoxInfo.Size)
```

**🎯 Призначення**: Зручне порівняння розмірів з шістнадцятковими дампами файлів або специфікаціями стандарту.

---

## 🔍 Технічні деталі тестування

### 🔹 Перехоплення stdout через pipe

```go
stdout := os.Stdout
r, w, err := os.Pipe()
require.NoError(t, err)
defer func() { os.Stdout = stdout }()  // 🔹 Відновлення після тесту
os.Stdout = w

go func() {
    require.Zero(t, Main(append(tc.options, tc.file)))  // 🔹 Запуск інструменту
    w.Close()  // 🔹 Закриття запису для сигналу завершення читання
}()

b, err := io.ReadAll(r)  // 🔹 Читання всього виводу
require.NoError(t, err)
assert.Equal(t, tc.wants, string(b))  // 🔹 Порівняння з golden file
```

**🎯 Призначення**: Тестувати CLI-інструмент як "чорний ящик" — без модифікації коду `dump`, тільки через стандартний вивід.

**⚠️ Важливо**: Використання горутини для `Main()` запобігає deadlock, бо `io.ReadAll()` блокується, поки `w.Close()` не сигналізує про кінець даних.

---

### 🔹 Golden files: `sampleMP4Output*`

```go
var sampleMP4Output = "" +
    `[ftyp] Size=32 MajorBrand="isom" ...` + "\n" +
    `[free] Size=8 Data=[...] ...` + "\n" +
    // ... ще 55 рядків ...

var sampleMP4OutputFullMvhdLoci = "" + ...  // 🔹 Інший очікуваний вивід
```

**🎯 Призначення**: Зберігати **очікуваний вивід** як константи для порівняння — підхід "golden file testing".

**🔑 Переваги:**
- ✅ Детермінованість: однаковий вивід для однакових вхідних даних
- ✅ Легке оновлення: змінили логіку → оновили константи
- ✅ Повне покриття: перевіряється кожен символ виводу

**⚠️ Недоліки:**
- ❌ Крихкість: будь-яка зміна форматування ламає тест
- ❌ Великі константи: важко читати/підтримувати

**🎯 Рекомендація**: Для складних випадків використовувати окремі файли `.golden` замість констант у коді.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Валідація структури вхідного сегмента

```go
func validateSegmentStructure(filePath string) error {
    var buf bytes.Buffer
    
    // 🔹 Запускаємо dump у режимі тільки заголовків
    cmd := exec.Command("mp4tool", "dump", filePath)
    cmd.Stdout = &buf
    
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("dump failed: %w", err)
    }
    
    output := buf.String()
    
    // 🔹 Перевірка наявності обов'язкових боксів
    required := []string{"[ftyp]", "[moov]", "[trak]", "[stbl]"}
    for _, box := range required {
        if !strings.Contains(output, box) {
            return fmt.Errorf("missing required box: %s", box)
        }
    }
    
    // 🔹 Перевірка відсутності пошкоджень
    if strings.Contains(output, "unsupported box version") {
        log.Printf("⚠️  Unsupported box version detected")
    }
    if strings.Contains(output, "Error:") {
        return fmt.Errorf("parsing error in dump output")
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Дебаг парсингу через порівняння виводу

```go
func debugBoxParsing(filePath, boxType string) error {
    // 🔹 Отримуємо детальний вивід конкретного боксу
    cmd := exec.Command("mp4tool", "dump", "-full", boxType, filePath)
    output, err := cmd.Output()
    if err != nil {
        return err
    }
    
    // 🔹 Парсимо вивід для перевірки конкретних полів
    lines := strings.Split(string(output), "\n")
    for _, line := range lines {
        if strings.Contains(line, "["+boxType+"]") {
            // 🔹 Перевірка очікуваних значень
            if !strings.Contains(line, "Version=0") {
                log.Printf("⚠️  Unexpected version in %s", boxType)
            }
            if strings.Contains(line, "EntryCount=0") {
                log.Printf("⚠️  Empty entries in %s", boxType)
            }
            break
        }
    }
    
    return nil
}

// 🔹 Використання:
debugBoxParsing("segment.m4s", "trun")  // 🔹 Перевірка таймстемпів
debugBoxParsing("segment.m4s", "avcC")   // 🔹 Перевірка конфігурації кодека
```

---

### 🔹 Приклад 3: Генерація звіту про структуру файлу

```go
func generateStructureReport(filePath string) (string, error) {
    // 🔹 Отримуємо ієрархію з офсетами та шістнадцятковими розмірами
    cmd := exec.Command("mp4tool", "dump", "-offset", "-hex", filePath)
    output, err := cmd.Output()
    if err != nil {
        return "", err
    }
    
    // 🔹 Форматуємо для звіту
    var report strings.Builder
    report.WriteString("# MP4 Structure Report\n")
    report.WriteString(fmt.Sprintf("File: %s\n\n", filePath))
    report.WriteString("```\n")
    report.WriteString(string(output))
    report.WriteString("```\n")
    
    return report.String(), nil
}

// 🔹 Інтеграція у веб-інтерфейс:
http.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
    filePath := r.URL.Query().Get("file")
    report, err := generateStructureReport(filePath)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.Header().Set("Content-Type", "text/markdown")
    w.Write([]byte(report))
})
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути `-full` для великих боксів | Вивід: `Data=[...]` замість реального вмісту | Використовуйте `-full mdat,stss,ctts` для детального аналізу |
| Неправильне порівняння через пробіли/переноси | Тест падає через відмінності у форматуванні | Використовуйте `strings.TrimSpace()` або спеціальні порівняння для golden files |
| Ігнорування `ErrUnsupportedBoxVersion` | Краш при парсингу нової версії боксу | Обробляйте цю помилку окремо: `if err != mp4.ErrUnsupportedBoxVersion` |
| Забути відновити `os.Stdout` | Наступні тести пишуть у pipe замість консолі | Завжди використовуйте `defer func() { os.Stdout = stdout }()` |
| Deadlock при читанні pipe | Тест зависає на `io.ReadAll()` | Закривайте `w` у горутині після завершення `Main()` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При інтеграції dump для валідації:
    • Використовуйте базовий режим для швидкої перевірки структури
    • Додавайте -full тільки для боксів, вміст яких потрібно перевірити
    • Фільтруйте вивід через grep для пошуку конкретних боксів

[ ] При дебагу парсингу:
    • Порівнюйте вивід dump з очікуваним через golden files
    • Використовуйте -offset -hex для точного аналізу позицій
    • Логувайте попередження при виявленні аномалій (нульові розміри, неподтримувані версії)

[ ] Для тестування власних інструментів:
    • Використовуйте pipe для перехоплення stdout, як у тесті
    • Завжди відновлюйте os.Stdout через defer
    • Закривайте writer у горутині для уникнення deadlock

[ ] Для оптимізації продуктивності:
    • Уникайте -full для mdat/free, якщо не потрібен сирий вміст
    • Фільтруйте бокси за типом на ранніх етапах обробника
    • Використовуйте буферизацію для великих файлів

[ ] Для дебагу самого інструменту:
    • Логувайте terminalWidth: log.Printf("📏 Terminal width: %d", terminalWidth)
    • Перевіряйте обрізання: if line.Len() > terminalWidth { ... }
    • Тестуйте з різними типами файлів: фрагментовані, DRM, QuickTime
```

---

## 🎯 Висновок

> **Цей тест — ваш "золотий стандарт" для надійного CLI-інструменту `dump`**.  
> Він гарантує:
> • ✅ Коректну обробку різних комбінацій прапорців: `-full`, `-offset`, `-hex`
> • ✅ Стабільний, детермінований вивід для порівняння через golden files
> • ✅ Безпечне перехоплення stdout через pipe без deadlock
> • ✅ Правильне форматування: відступи, скорочення великих боксів, людсько-читабельні значення
> • ✅ Надійну обробку крайніх випадків: непідтримувані типи, помилки парсингу

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Миттєва валідація структури вхідних сегментів через CLI-інструмент
- 🛠️ Зручний дебаг парсингу через порівняння виводу з очікуваним
- ⚡ Ефективна інтеграція у конвеєр обробки через виклик як підпроцесу
- 🧪 Надійне тестування власних інструментів за зразком цього тесту

Потребуєте допомоги з інтеграцією `dump` у ваш конвеєр валідації або з налаштуванням кастомного формату виводу? Напишіть — покажу готовий код для вашого сценарію! 🚀📋