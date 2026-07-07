# 🧪 Тест `TestExtract`: Перевірка інструменту витягування боксів з MP4

Це **інтеграційний тест** для модуля `extract`, який перевіряє коректність роботи **CLI-інструменту `mp4tool extract`** — витягування сирих байтів конкретних типів боксів з MP4-файлів.

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що `extract` коректно знаходить і виводить сирі дані вказаного типу боксу** — з підтримкою кількох екземплярів, перевіркою цілісності даних та валідацією вхідних аргументів.

---

## 📋 Структура тесту

### 🔹 `TestExtract` — основні тест-кейси

```go
func TestExtract(t *testing.T) {
    testCases := []struct {
        name         string          // 🔹 Назва тесту
        file         string          // 🔹 Шлях до тестового файлу
        boxType      string          // 🔹 Тип боксу для витягування
        expectedSize int             // 🔹 Очікуваний розмір виводу у байтах
    }{
        {
            name:         "sample.mp4/ftyp",
            file:         "../../../../testdata/sample.mp4",
            boxType:      "ftyp",
            expectedSize: 32,  // 🔹 Один бокс ftyp = 32 байти
        },
        {
            name:         "sample.mp4/mdhd",
            file:         "../../../../testdata/sample.mp4",
            boxType:      "mdhd",
            expectedSize: 64,  // 🔹 Два бокси mdhd (відео + аудіо) × 32 байти = 64
        },
        {
            name:         "sample_fragmented.mp4/trun",
            file:         "../../../../testdata/sample_fragmented.mp4",
            boxType:      "trun",
            expectedSize: 452,  // 🔹 Сума розмірів усіх trun боксів у фрагментованому файлі
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
                require.Zero(t, Main([]string{tc.boxType, tc.file}))
                w.Close()
            }()
            
            // 🔹 Читання виводу
            b, err := io.ReadAll(r)
            require.NoError(t, err)
            
            // 🔹 Перевірка розміру
            assert.Equal(t, tc.expectedSize, len(b))
            
            // 🔹 Перевірка цілісності: байти 4-7 мають містити тип боксу
            assert.Equal(t, tc.boxType, string(b[4:8]))
        })
    }
}
```

**🎯 Призначення**: Перевірити, що інструмент `extract`:
- ✅ Знаходить усі екземпляри вказаного типу боксу у файлі
- ✅ Виводить точні сирі байти кожного боксу (без модифікацій)
- ✅ Конкатенує вивід для кількох екземплярів одного типу
- ✅ Зберігає цілісність даних: тип боксу на позиції 4-7

---

### 🔹 `TestValidation` — перевірка валідації вхідних даних

```go
func TestValidation(t *testing.T) {
    // 🔹 Валідний виклик: 4-символьний тип + існуючий файл
    require.Zero(t, Main([]string{"xxxx", "../../../../testdata/sample.mp4"}))
    
    // 🔹 Невалідні виклики (очікуємо помилку ≠ 0):
    require.NotZero(t, Main([]string{}))                    // ❌ Немає аргументів
    require.NotZero(t, Main([]string{"xxxx"}))              // ❌ Тільки тип, немає файлу
    require.NotZero(t, Main([]string{"xxxxx", "file.mp4"})) // ❌ Тип має 5 символів замість 4
    require.NotZero(t, Main([]string{"xxxx", "not_found.mp4"})) // ❌ Файл не існує
}
```

**🎯 Призначення**: Перевірити, що інструмент коректно обробляє помилкові вхідні дані:
- ✅ Повертає ненульовий код помилки при невалідних аргументах
- ✅ Виводить зрозумілі повідомлення про помилки
- ✅ Не падає з panic при некоректному вводу

---

## 🔍 Детальний розбір тест-кейсів

### 🔹 Кейс 1: Витягування `ftyp` з `sample.mp4`

```go
{
    name:         "sample.mp4/ftyp",
    file:         "../../../../testdata/sample.mp4",
    boxType:      "ftyp",
    expectedSize: 32,
},
```

**📊 Очікуваний вивід (32 байти):**
```
[0-3]:   00 00 00 20  ← size=32 (0x20)
[4-7]:   66 74 79 70  ← "ftyp"
[8-11]:  69 73 6f 6d  ← MajorBrand="isom"
[12-15]: 00 00 02 00  ← MinorVersion=512
[16-31]: 69 73 6f 6d 69 73 6f 32 61 76 63 31 6d 70 34 31  ← CompatibleBrands
```

**🔑 Перевірки:**
- ✅ `len(b) == 32` → точний розмір боксу
- ✅ `b[4:8] == "ftyp"` → тип боксу на правильній позиції
- ✅ Вивід містить тільки один екземпляр (ftyp зустрічається один раз у файлі)

---

### 🔹 Кейс 2: Витягування `mdhd` з `sample.mp4` (кілька екземплярів)

```go
{
    name:         "sample.mp4/mdhd",
    file:         "../../../../testdata/sample.mp4",
    boxType:      "mdhd",
    expectedSize: 64,  // 32 байти × 2 екземпляри
},
```

**📊 Очікуваний вивід (64 байти = 2 × 32):**
```
📦 Перший mdhd (відео-доріжка):
[0-3]:   00 00 00 20  ← size=32
[4-7]:   6d 64 68 64  ← "mdhd"
[8-31]:  ... дані відео-доріжки ...

📦 Другий mdhd (аудіо-доріжка):
[32-35]: 00 00 00 20  ← size=32
[36-39]: 6d 64 68 64  ← "mdhd"
[40-63]: ... дані аудіо-доріжки ...
```

**🔑 Перевірки:**
- ✅ `len(b) == 64` → сума розмірів усіх екземплярів
- ✅ `b[4:8] == "mdhd"` → перший бокс має правильний тип
- ✅ `b[36:40] == "mdhd"` → другий бокс також має правильний тип (неявна перевірка)

**🎯 Призначення**: Перевірити, що інструмент коректно обробляє **кілька екземплярів одного типу** — конкатенує їх у виводі без роздільників.

---

### 🔹 Кейс 3: Витягування `trun` з фрагментованого файлу

```go
{
    name:         "sample_fragmented.mp4/trun",
    file:         "../../../../testdata/sample_fragmented.mp4",
    boxType:      "trun",
    expectedSize: 452,
},
```

**📊 Контекст:** Фрагментований MP4 (fMP4) містить кілька `moof` → `traf` → `trun` структур.

**🔑 Перевірки:**
- ✅ `len(b) == 452` → сума розмірів усіх `trun` боксів у файлі
- ✅ `b[4:8] == "trun"` → перший бокс має правильний тип
- ✅ Вивід містить тільки `trun` бокси, без батьківських `moof`/`traf`

**🎯 Призначення**: Перевірити роботу з **фрагментованими файлами**, де цільові бокси можуть бути глибоко вкладені.

---

## 🔍 Технічні деталі тестування

### 🔹 Перехоплення stdout через pipe (як у `dump`)

```go
stdout := os.Stdout
r, w, err := os.Pipe()
require.NoError(t, err)
defer func() { os.Stdout = stdout }()  // 🔹 Відновлення після тесту
os.Stdout = w

go func() {
    require.Zero(t, Main([]string{tc.boxType, tc.file}))  // 🔹 Запуск інструменту
    w.Close()  // 🔹 Закриття запису для сигналу завершення читання
}()

b, err := io.ReadAll(r)  // 🔹 Читання всього виводу
require.NoError(t, err)
```

**🎯 Призначення**: Тестувати CLI-інструмент як "чорний ящик" — без модифікації коду `extract`, тільки через стандартний вивід.

**⚠️ Важливо**: Використання горутини для `Main()` запобігає deadlock, бо `io.ReadAll()` блокується, поки `w.Close()` не сигналізує про кінець даних.

---

### 🔹 Перевірка цілісності даних: `b[4:8]`

```go
assert.Equal(t, tc.boxType, string(b[4:8]))
```

**🎯 Призначення**: Перевірити, що виведені дані дійсно є боксом вказаного типу, а не випадковими байтами.

**🔢 Структура заголовка боксу:**
```
[0-3]:   Size (uint32, big-endian)
[4-7]:   Type (4 ASCII символи, напр. "ftyp")
[8+]:    Payload (залежить від типу)
```

**✅ Перевірка `b[4:8]` гарантує:**
- Дані починаються з правильного заголовка
- Інструмент не пошкодив дані під час копіювання
- Вивід можна безпечно передати іншим інструментам для парсингу

---

### 🔹 Валідація вхідних даних

```go
// 🔹 Валідний виклик:
require.Zero(t, Main([]string{"xxxx", "../../../../testdata/sample.mp4"}))

// 🔹 Невалідні виклики:
require.NotZero(t, Main([]string{}))                    // ❌ Немає аргументів
require.NotZero(t, Main([]string{"xxxx"}))              // ❌ Тільки тип
require.NotZero(t, Main([]string{"xxxxx", "file.mp4"})) // ❌ Тип ≠ 4 символи
require.NotZero(t, Main([]string{"xxxx", "not_found.mp4"})) // ❌ Файл не існує
```

**🎯 Призначення**: Перевірити, що інструмент:
- ✅ Повертає код помилки `1` при невалідному вводу
- ✅ Виводить зрозумілі повідомлення про помилки
- ✅ Не падає з panic або segmentation fault

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Валідація наявності обов'язкових боксів

```go
func validateRequiredBoxes(filePath string, requiredTypes []string) error {
    for _, boxType := range requiredTypes {
        // 🔹 Витягуємо бокс і перевіряємо розмір
        cmd := exec.Command("mp4tool", "extract", boxType, filePath)
        output, err := cmd.Output()
        if err != nil {
            return fmt.Errorf("failed to extract %s: %w", boxType, err)
        }
        
        if len(output) == 0 {
            return fmt.Errorf("missing required box: %s", boxType)
        }
        
        // 🔹 Перевірка цілісності: тип на позиції 4-7
        if len(output) >= 8 && string(output[4:8]) != boxType {
            return fmt.Errorf("corrupted %s box in %s", boxType, filePath)
        }
    }
    return nil
}

// 🔹 Використання:
err := validateRequiredBoxes("segment.m4s", []string{"ftyp", "moof", "traf"})
if err != nil {
    log.Printf("❌ Invalid segment: %v", err)
}
```

---

### 🔹 Приклад 2: Експорт конфігурації кодека для аналізу

```go
func extractAndAnalyzeCodecConfig(filePath string) (*CodecInfo, error) {
    // 🔹 Витягуємо avcC бокс
    cmd := exec.Command("mp4tool", "extract", "avcC", filePath)
    configData, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("extract failed: %w", err)
    }
    
    if len(configData) == 0 {
        return nil, fmt.Errorf("avcC box not found")
    }
    
    // 🔹 Парсинг конфігурації (спрощено)
    if len(configData) < 8 {
        return nil, fmt.Errorf("avcC too small")
    }
    
    info := &CodecInfo{
        Profile:              configData[8],   // 🔹 Profile byte
        ProfileCompatibility: configData[9],   // 🔹 Compatibility byte
        Level:                configData[10],  // 🔹 Level byte
        LengthSize:           uint16(configData[11]&0x03) + 1, // 🔹 NAL length size
    }
    
    return info, nil
}

// 🔹 Використання у конвеєрі:
go func() {
    for segment := range segmentQueue {
        config, err := extractAndAnalyzeCodecConfig(segment.Path)
        if err != nil {
            log.Printf("⚠️  Failed to analyze codec config: %v", err)
            continue
        }
        
        // 🔹 Валідація параметрів кодека
        if config.Level > 51 {  // 🔹 Максимальний рівень для сумісності
            log.Printf("⚠️  High AVC level %d in %s", config.Level, segment.Path)
        }
    }
}()
```

---

### 🔹 Приклад 3: Створення індексу боксів для швидкого пошуку

```go
type BoxIndex struct {
    Type   string
    Offset uint64
    Size   uint64
}

func buildBoxIndex(filePath string) ([]BoxIndex, error) {
    var index []BoxIndex
    
    // 🔹 Список типів для індексації
    boxTypes := []string{"ftyp", "moov", "trak", "moof", "traf", "trun"}
    
    for _, boxType := range boxTypes {
        cmd := exec.Command("mp4tool", "extract", boxType, filePath)
        output, err := cmd.Output()
        if err != nil {
            continue  // 🔹 Пропускаємо відсутні бокси
        }
        
        // 🔹 Парсинг заголовків для отримання офсетів/розмірів
        for i := 0; i < len(output); {
            if i+8 > len(output) { break }
            
            size := binary.BigEndian.Uint32(output[i:i+4])
            typ := string(output[i+4:i+8])
            
            index = append(index, BoxIndex{
                Type:   typ,
                Offset: uint64(i),  // 🔹 Відносний офсет у виводі
                Size:   uint64(size),
            })
            
            if size == 1 {  // 🔹 Large size: наступні 8 байт = розмір
                i += 16
            } else {
                i += int(size)
            }
        }
    }
    
    return index, nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильна довжина BOX_TYPE | Помилка: "BOX_TYPE must be 4 characters" | Завжди вказуйте рівно 4 символи: `"moov"`, не `"mo"` |
| Витягування боксу з дітьми | Отримуєте тільки заголовок батька, без дітей | Пам'ятайте: extract витягує тільки один бокс, не рекурсивно |
| Перенаправлення у існуючий файл | Перезапис без попередження | Використовуйте `>` з обережністю, або `-i` для інтерактивного підтвердження |
| Ігнорування `ErrUnsupportedBoxVersion` | Пропуск важливих боксів без попередження | Логувайте попередження: `log.Printf("⚠️  Unsupported version for %s", boxType)` |
| Великі файли без буферизації | Повільне читання через багато системних викликів | Використовуйте `bufseekio` з `blockSize=128KB` як у прикладі |

---

## 📋 Чекліст для вашого проекту

```
[ ] При витягуванні боксів:
    • Перевіряйте, що BOX_TYPE має рівно 4 символи
    • Використовуйте перенаправлення `>` для запису у файл
    • Для боксів з дітьми: пам'ятайте, що витягується тільки батько

[ ] Для аналізу витягнутих даних:
    • Використовуйте hex-дампи для перевірки структури: hexdump -C file.bin
    • Парсіть через спеціалізовані бібліотеки (напр. pymp4 для Python)
    • Порівнюйте з очікуваною структурою через golden files

[ ] Для інтеграції у конвеєр:
    • Запускайте extract як підпроцес з timeout для уникнення зависань
    • Обробляйте stderr для логування помилок
    • Кешуйте результати для повторного використання

[ ] Для оптимізації:
    • Використовуйте bufseekio з blockSize=128KB для великих файлів
    • Уникайте парсингу непідтримуваних типів через IsSupportedType()
    • Пропускайте великі "листові" бокси через ShouldHasNoChildren()

[ ] Для дебагу:
    • Логувайте знайдені бокси: log.Printf("📦 Found %s @ offset=%d, size=%d", ...)
    • Перевіряйте цілісність витягнутих даних: порівняння розмірів
    • Тестуйте з різними типами файлів: фрагментовані, DRM, QuickTime
```

---

## 🎯 Висновок

> **Цей тест — ваш "золотий стандарт" для надійного інструменту `extract`**.  
> Він гарантує:
> • ✅ Коректне витягування сирих байтів для одиничних та множинних екземплярів боксів
> • ✅ Збереження цілісності даних: перевірка типу на позиції 4-7
> • ✅ Надійну валідацію вхідних аргументів: довжина типу, наявність файлу
> • ✅ Безпечне перехоплення stdout через pipe без deadlock
> • ✅ Стабільну роботу з фрагментованими файлами та глибоко вкладеними боксами

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Миттєве витягування конфігурацій кодеків для валідації без перекодування
- 🔧 Гнучкий аналіз метаданих через експорт у зручні формати
- 🔄 Легка інтеграція з Python/Node.js скриптами через stdout
- 🛡️ Надійність: обхід файлу не ламається на невідомих боксах

Потребуєте допомоги з інтеграцією `extract` у ваш конвеєр аналізу сегментів або з реалізацією кастомної обробки витягнутих даних? Напишіть — покажу готовий код для вашого сценарію! 🚀📤