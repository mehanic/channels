# 📤 `extract`: Інструмент для витягування боксів з MP4-файлів

Це **практичний CLI-інструмент** на основі бібліотеки `go-mp4`, який дозволяє **витягувати сирі дані конкретного боксу** з MP4-файлу без парсингу або модифікації — ідеально для аналізу, дебагу або подальшої обробки окремих структур.

---

## 🎯 Коротка відповідь

> **Це "екстрактор боксів" для MP4**: він знаходить усі екземпляри вказаного типу боксу у файлі та виводить їхні сирі байти у stdout — без перекодування, без парсингу, максимально швидко.

---

## 🗂️ Структура інструменту

```bash
# 🔹 Базовий синтаксис:
$ mp4tool extract BOX_TYPE INPUT.mp4

# 🔹 Приклади:
# Витягнути всі бокси ftyp:
$ mp4tool extract ftyp video.mp4 > ftyp.bin

# Витягнути конфігурацію кодека avcC:
$ mp4tool extract avcC video.mp4 > avc_config.bin

# Витягнути iTunes metadata:
$ mp4tool extract ilst video.mp4 > metadata.bin

# 🔹 Вивід у файл через перенаправлення:
$ mp4tool extract moov input.mp4 > moov_only.mp4
```

---

## 🧱 Основні компоненти

### 🔹 Валідація вхідних даних

```go
func Main(args []string) int {
    flagSet := flag.NewFlagSet("extract", flag.ExitOnError)
    flagSet.Usage = func() {
        println("USAGE: mp4tool extract [OPTIONS] BOX_TYPE INPUT.mp4")
        flagSet.PrintDefaults()
    }
    flagSet.Parse(args)
    
    // 🔹 Перевірка кількості аргументів
    if len(flagSet.Args()) < 2 {
        flagSet.Usage()
        return 1
    }
    
    boxType := flagSet.Args()[0]      // 🔹 Тип боксу, напр. "moov"
    inputPath := flagSet.Args()[1]    // 🔹 Шлях до вхідного файлу
    
    // 🔹 Перевірка: тип боксу має бути рівно 4 символи
    if len(boxType) != 4 {
        println("Error:", "invalid argument:", boxType)
        println("BOX_TYPE must be 4 characters.")
        return 1
    }
    
    // 🔹 Відкриття файлу
    input, err := os.Open(inputPath)
    if err != nil {
        fmt.Println("Error:", err)
        return 1
    }
    defer input.Close()
    
    // 🔹 Буферизований читач для ефективності
    r := bufseekio.NewReadSeeker(input, blockSize, blockHistorySize)
    
    // 🔹 Виклик основної логіки
    if err := extract(r, mp4.StrToBoxType(boxType)); err != nil {
        fmt.Println("Error:", err)
        return 1
    }
    return 0
}
```

**🎯 Призначення**: Забезпечити коректну обробку аргументів та підготувати ефективний потік читання.

---

### 🔹 Основна логіка: `extract()`

```go
func extract(r io.ReadSeeker, boxType mp4.BoxType) error {
    _, err := mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
        
        // 🔹 Крок 1: Чи це цільовий бокс?
        if h.BoxInfo.Type == boxType {
            // 🔹 Позиціонування на початок боксу
            h.BoxInfo.SeekToStart(r)
            
            // 🔹 Копіювання сирих байт у stdout
            if _, err := io.CopyN(os.Stdout, r, int64(h.BoxInfo.Size)); err != nil {
                return nil, err
            }
            // 🔹 Не розгортаємо дітей — витягуємо тільки цей бокс
            return nil, nil
        }
        
        // 🔹 Крок 2: Оптимізація для непідтримуваних типів
        if !h.BoxInfo.IsSupportedType() {
            return nil, nil  // 🔹 Пропускаємо без парсингу
        }
        
        // 🔹 Крок 3: Оптимізація для великих боксів без дітей
        if h.BoxInfo.Size >= 256 && util.ShouldHasNoChildren(h.BoxInfo.Type) {
            return nil, nil  // 🔹 Пропускаємо парсинг великих "листових" боксів
        }
        
        // 🔹 Крок 4: Рекурсивна обробка дітей
        _, err := h.Expand()
        if err == mp4.ErrUnsupportedBoxVersion {
            return nil, nil  // 🔹 Ігноруємо непідтримувані версії
        }
        return nil, err
    })
    return err
}
```

**🔄 Потік даних:**
```
🔹 Вхід: io.ReadSeeker (файл з буферизацією), цільовий boxType
│
▼
🔹 ReadBoxStructure(handler):
   │
   ├── 🔹 Для кожного боксу:
   │   │
   │   ├── 🔹 Чи співпадає тип? → так:
   │   │   • SeekToStart() → позиціонування на початок боксу
   │   │   • io.CopyN(stdout, r, Size) → пряме копіювання сирих байт
   │   │   • return nil, nil → не розгортаємо дітей
   │   │
   │   ├── 🔹 Чи непідтримуваний тип? → пропускаємо
   │   │
   │   ├── 🔹 Чи великий бокс без дітей? → пропускаємо парсинг
   │   │
   │   └── 🔹 Інакше: Expand() → рекурсія на дітей
   │
   ▼
🔹 Вихід: сирі байти цільових боксів у stdout
```

---

## 🔍 Ключові особливості

### 🔹 Пряме копіювання сирих байт

```go
if h.BoxInfo.Type == boxType {
    h.BoxInfo.SeekToStart(r)
    io.CopyN(os.Stdout, r, int64(h.BoxInfo.Size))
    return nil, nil  // 🔹 Не розгортаємо дітей
}
```

**🎯 Призначення**: Витягнути **точні байти боксу** без будь-якої модифікації — ідеально для:
- ✅ Аналізу структури через зовнішні інструменти
- ✅ Дебагу парсингу через порівняння сирих даних
- ✅ Експорту конфігурацій кодеків для подальшої обробки

**🔑 Переваги:**
- ⚡ Швидкість: немає накладних витрат на парсинг/маршалінг
- 💾 Точність: байт в байт відповідність оригіналу
- 🔒 Безпека: вихідний файл залишається незмінним

---

### 🔹 Оптимізація: пропуск непотрібних боксів

```go
// 🔹 Непідтримувані типи: не парсимо
if !h.BoxInfo.IsSupportedType() {
    return nil, nil
}

// 🔹 Великі бокси без дітей: не парсимо
if h.BoxInfo.Size >= 256 && util.ShouldHasNoChildren(h.BoxInfo.Type) {
    return nil, nil
}
```

**🎯 Призначення**: Прискорити обхід файлу, уникаючи парсингу боксів, які:
- ❌ Не підтримуються бібліотекою (немає сенсу заглиблюватися)
- ❌ Великі та не мають дітей (напр. `stsz`, `stco`) — парсинг не потрібен для пошуку

**🔢 Приклад `util.ShouldHasNoChildren`:**
```go
func ShouldHasNoChildren(boxType mp4.BoxType) bool {
    switch boxType {
    case mp4.BoxTypeStsz(), mp4.BoxTypeStco(), mp4.BoxTypeCo64():
        return true  // ✅ Тільки дані, без вкладених структур
    default:
        return false
    }
}
```

---

### 🔹 Обробка `ErrUnsupportedBoxVersion`

```go
_, err := h.Expand()
if err == mp4.ErrUnsupportedBoxVersion {
    return nil, nil  // 🔹 Ігноруємо, продовжуємо обхід
}
```

**🎯 Призначення**: Запобігти крашу при зустрічі з боксами невідомої версії — інструмент продовжує працювати, просто пропускаючи проблемні бокси.

---

## 🛠️ Практичне використання

### 🔹 Приклад 1: Експорт конфігурації кодека для аналізу

```bash
# 🔹 Витягнути avcC (H.264 конфігурація):
$ mp4tool extract avcC video.mp4 > avc_config.bin

# 🔹 Аналіз через медіа-інструменти:
$ mediainfo --Details=1 avc_config.bin
$ hexdump -C avc_config.bin | head -20

# 🔹 Результат: сирі байти конфігурації для подальшої обробки ✅
```

---

### 🔹 Приклад 2: Витягування метаданих iTunes

```bash
# 🔹 Витягнути ilst (iTunes metadata):
$ mp4tool extract ilst video.mp4 > metadata.bin

# 🔹 Парсинг через Python:
$ python3 -c "
import struct
with open('metadata.bin', 'rb') as f:
    data = f.read()
    # Парсинг структури ilst...
    print(f'Extracted {len(data)} bytes of metadata')
"

# 🔹 Результат: метадані для аналізу або модифікації ✅
```

---

### 🔹 Приклад 3: Створення "обрізаного" MP4 тільки з moov

```bash
# 🔹 Витягнути тільки moov бокс:
$ mp4tool extract moov input.mp4 > moov_only.mp4

# 🔹 Використання: швидкий аналіз структури без завантаження медіа-даних
$ ffprobe -show_format moov_only.mp4

# 🔹 Результат: міні-файл з метаданими для швидкої обробки ✅
```

---

### 🔹 Приклад 4: Інтеграція у CCTV HLS Processor

```go
// 🔹 Функція для витягування конфігурації кодека з сегмента
func extractCodecConfig(segmentPath string, boxType string) ([]byte, error) {
    // 🔹 Запускаємо mp4tool extract як підпроцес
    cmd := exec.Command("mp4tool", "extract", boxType, segmentPath)
    
    var stdout bytes.Buffer
    cmd.Stdout = &stdout
    
    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("extract failed: %w", err)
    }
    
    return stdout.Bytes(), nil
}

// 🔹 Використання у конвеєрі валідації:
go func() {
    for segment := range segmentQueue {
        // 🔹 Витягуємо avcC для перевірки конфігурації кодека
        config, err := extractCodecConfig(segment.Path, "avcC")
        if err != nil {
            log.Printf("❌ Failed to extract avcC from %s: %v", segment.Path, err)
            continue
        }
        
        // 🔹 Аналіз конфігурації: профіль, рівень, роздільність
        if err := validateAVCConfig(config); err != nil {
            log.Printf("⚠️  Invalid AVC config in %s: %v", segment.Path, err)
        }
    }
}()
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

> **`extract` — це "скальпель" для роботи з окремими боксами MP4**, який забезпечує:
> • ✅ Швидке витягування сирих байт конкретного боксу без парсингу
> • ✅ Ефективний обхід файлу з оптимізацією для великих/непідтримуваних боксів
> • ✅ Безпечну обробку невідомих версій через ігнорування помилок
> • ✅ Просту інтеграцію з іншими інструментами через stdout
> • ✅ Мінімум залежностей: тільки буферизація та бібліотека go-mp4

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Миттєве витягування конфігурацій кодеків для валідації без перекодування
- 🔧 Гнучкий аналіз метаданих через експорт у зручні формати
- 🔄 Легка інтеграція з Python/Node.js скриптами через stdout
- 🛡️ Надійність: обхід файлу не ламається на невідомих боксах

Потребуєте допомоги з інтеграцією `extract` у ваш конвеєр аналізу сегментів або з реалізацією кастомної обробки витягнутих даних? Напишіть — покажу готовий код для вашого сценарію! 🚀📤