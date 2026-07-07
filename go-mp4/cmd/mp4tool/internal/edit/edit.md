# ✏️ `edit`: Інструмент для редагування метаданих у MP4-файлах

Це **практичний CLI-інструмент** на основі бібліотеки `go-mp4`, який дозволяє **модифікувати поля боксів** та **видаляти непотрібні бокси** у MP4-файлах без перекодування медіа-даних.

---

## 🎯 Коротка відповідь

> **Це "редактор метаданих" для MP4**: він дозволяє змінювати таймстемпи, видаляти бокси, оновлювати конфігурації — все це без перекодування відео/аудіо, що займає секунди замість годин.

---

## 🗂️ Структура інструменту

```bash
# 🔹 Базовий синтаксис:
$ mp4tool edit [OPTIONS] INPUT.mp4 OUTPUT.mp4

# 🔹 Доступні опції:
--base_media_decode_time=UINT64   # 🔹 Встановити новий BaseMediaDecodeTime у tfdt
--drop=BOX_TYPE[,BOX_TYPE...]     # 🔹 Видалити вказані типи боксів

# 🔹 Приклади:
# Змінити час декодування у фрагменті:
$ mp4tool edit -base_media_decode_time=0 input.m4s output.m4s

# Видалити бокси sgpd/sbgp (групи семплів):
$ mp4tool edit -drop=sgpd,sbgp input.mp4 output.mp4

# Комбіноване використання:
$ mp4tool edit -base_media_decode_time=90000 -drop=free input.mp4 output.mp4
```

---

## 🧱 Основні компоненти

### 🔹 Конфігурація через прапорці

```go
type Values struct {
    BaseMediaDecodeTime uint64  // 🔹 Нове значення для BaseMediaDecodeTime
}

type Boxes []string  // 🔹 Список типів боксів для видалення

type Config struct {
    values    Values   // 🔹 Значення для модифікації
    dropBoxes Boxes    // 🔹 Бокси для видалення
}

var config Config  // 🔹 Глобальна конфігурація

func Main(args []string) int {
    flagSet := flag.NewFlagSet("edit", flag.ExitOnError)
    
    // 🔹 Опція: встановити BaseMediaDecodeTime
    flagSet.Uint64Var(&config.values.BaseMediaDecodeTime, 
        "base_media_decode_time", UNoValue, 
        "set new value to base_media_decode_time")
    
    // 🔹 Опція: видалити бокси за типом
    dropBoxes := flagSet.String("drop", "", "drop boxes")
    
    flagSet.Parse(args)
    
    // 🔹 Парсинг списку боксів для видалення
    config.dropBoxes = strings.Split(*dropBoxes, ",")
    
    // 🔹 Виклик основної логіки
    err := editFile(inputPath, outputPath)
    // ...
}
```

**🎯 Призначення**: Дозволити користувачеві гнучко вказати, **що саме змінити** у файлі.

---

### 🔹 `Boxes.Exists()` — перевірка наявності типу у списку видалення

```go
func (b Boxes) Exists(boxType string) bool {
    for _, t := range b {
        if t == boxType {
            return true  // ✅ Тип знайдено у списку для видалення
        }
    }
    return false
}
```

**🎯 Призначення**: Швидка перевірка, чи потрібно видалити поточний бокс.

---

### 🔹 Основна логіка: `editFile()`

```go
func editFile(inputPath, outputPath string) error {
    // 🔹 Відкриття файлів
    inputFile, _ := os.Open(inputPath)
    outputFile, _ := os.Create(outputPath)
    
    // 🔹 Буферизований читач для ефективності
    r := bufseekio.NewReadSeeker(inputFile, 128*1024, 4)
    
    // 🔹 Writer для запису з автоматичним оновленням заголовків
    w := mp4.NewWriter(outputFile)
    
    // 🔹 Обхід структури файлу
    _, err := mp4.ReadBoxStructure(r, func(h *mp4.ReadHandle) (interface{}, error) {
        
        // 🔹 Крок 1: Видалення боксів
        if config.dropBoxes.Exists(h.BoxInfo.Type.String()) {
            return uint64(0), nil  // 🔹 Пропускаємо бокс, повертаємо розмір 0
        }
        
        // 🔹 Крок 2: Копіювання непідтримуваних боксів та mdat
        if !h.BoxInfo.IsSupportedType() || h.BoxInfo.Type == mp4.BoxTypeMdat() {
            return nil, w.CopyBox(r, &h.BoxInfo)  // 🔹 Пряме копіювання без парсингу
        }
        
        // 🔹 Крок 3: Запис заголовка боксу (з тимчасовим розміром)
        _, err := w.StartBox(&h.BoxInfo)
        if err != nil { return nil, err }
        
        // 🔹 Крок 4: Парсинг вмісту боксу
        box, _, err := h.ReadPayload()
        if err != nil { return nil, err }
        
        // 🔹 Крок 5: Модифікація полів (switch за типом боксу)
        switch h.BoxInfo.Type {
        case mp4.BoxTypeTfdt():  // 🔹 Track Fragment Decode Time
            tfdt := box.(*mp4.Tfdt)
            if config.values.BaseMediaDecodeTime != UNoValue {
                if tfdt.GetVersion() == 0 {
                    tfdt.BaseMediaDecodeTimeV0 = uint32(config.values.BaseMediaDecodeTime)
                } else {
                    tfdt.BaseMediaDecodeTimeV1 = config.values.BaseMediaDecodeTime
                }
            }
        }
        
        // 🔹 Крок 6: Запис модифікованого вмісту
        if _, err := mp4.Marshal(w, box, h.BoxInfo.Context); err != nil {
            return nil, err
        }
        
        // 🔹 Крок 7: Рекурсивна обробка вкладених боксів
        if _, err := h.Expand(); err != nil {
            return nil, err
        }
        
        // 🔹 Крок 8: Оновлення заголовка з правильним розміром
        _, err = w.EndBox()
        return nil, err
    })
    return err
}
```

**🔄 Потік даних:**
```
🔹 Вхід: input.mp4
│
▼
🔹 ReadBoxStructure(handler):
   │
   ├── 🔹 Для кожного боксу:
   │   │
   │   ├── 🔹 Чи у списку видалення? → пропустити (return 0)
   │   │
   │   ├── 🔹 Чи непідтримуваний тип або mdat? → CopyBox() (без парсингу)
   │   │
   │   ├── 🔹 Інакше:
   │   │   • StartBox() → запис заголовка з тимчасовим розміром
   │   │   • ReadPayload() → парсинг у структуру
   │   │   • Switch за типом → модифікація полів
   │   │   • Marshal() → запис модифікованого вмісту
   │   │   • Expand() → рекурсія на дітей
   │   │   • EndBox() → оновлення заголовка з правильним розміром
   │   │
   │   └── 🔹 Повернення розміру боксу для батьків
   │
   ▼
🔹 Вихід: output.mp4 з модифікованими метаданими
```

---

## 🔍 Ключові особливості

### 🔹 Модифікація `BaseMediaDecodeTime` у `tfdt`

```go
case mp4.BoxTypeTfdt():
    tfdt := box.(*mp4.Tkhd)  // 🔹 Type assertion до *Tfdt
    if config.values.BaseMediaDecodeTime != UNoValue {
        if tfdt.GetVersion() == 0 {
            // 🔹 Версія 0: 32-бітне поле
            tfdt.BaseMediaDecodeTimeV0 = uint32(config.values.BaseMediaDecodeTime)
        } else {
            // 🔹 Версія 1: 64-бітне поле
            tfdt.BaseMediaDecodeTimeV1 = config.values.BaseMediaDecodeTime
        }
    }
```

**🎯 Призначення**: Змінити **базовий час декодування** фрагмента — критично для:
- ✅ Виправлення десинхронізації аудіо/відео
- ✅ Об'єднання фрагментів у єдиний потік
- ✅ Корекції таймстемпів після редагування

**🔢 Приклад:**
```
🔹 Вхідний фрагмент:
  tfdt: BaseMediaDecodeTime = 180000 (2 секунди @ 90kHz)

🔹 Команда:
  $ mp4tool edit -base_media_decode_time=0 input.m4s output.m4s

🔹 Вихідний фрагмент:
  tfdt: BaseMediaDecodeTime = 0  ← час скинуто на початок
```

---

### 🔹 Видалення боксів через `-drop`

```go
if config.dropBoxes.Exists(h.BoxInfo.Type.String()) {
    return uint64(0), nil  // 🔹 Пропускаємо бокс
}
```

**🎯 Призначення**: Видалити непотрібні бокси без перекодування — наприклад:
- ✅ `free`/`skip`: порожні бокси для вирівнювання
- ✅ `sgpd`/`sbgp`: групи семплів (якщо не потрібні)
- ✅ `udta`: користувацькі метадані (для приватності)

**⚠️ Важливо**: Видалення боксів змінює офсети наступних боксів — `Writer` автоматично оновлює заголовки.

---

### 🔹 Ефективне копіювання: `CopyBox` для `mdat`

```go
if !h.BoxInfo.IsSupportedType() || h.BoxInfo.Type == mp4.BoxTypeMdat() {
    return nil, w.CopyBox(r, &h.BoxInfo)  // 🔹 Пряме копіювання байт
}
```

**🎯 Призначення**: Уникнути парсингу великих медіа-даних (`mdat` може бути гігабайтами) — копіюємо сирі байти напряму.

**🔑 Переваги:**
- ⚡ Швидкість: немає накладних витрат на парсинг/маршалінг
- 💾 Пам'ять: не завантажуємо весь `mdat` у пам'ять
- 🔒 Безпека: медіа-дані залишаються незмінними

---

### 🔹 Двофазний запис через `Writer`

```go
// 🔹 Крок 1: Запис заголовка з тимчасовим розміром
w.StartBox(&h.BoxInfo)  // ← Size = HeaderSize (тимчасово)

// 🔹 Крок 2: Запис вмісту (парсинг → модифікація → маршалінг)
mp4.Marshal(w, box, ctx)

// 🔹 Крок 3: Обробка дітей
h.Expand()

// 🔹 Крок 4: Оновлення заголовка з правильним розміром
w.EndBox()  // ← Size = HeaderSize + Size(дітей) + Size(вмісту)
```

**🎯 Призначення**: Дозволити запис вкладених структур **без попереднього розрахунку розмірів** — критично для динамічної модифікації.

---

## 🛠️ Практичне використання

### 🔹 Приклад 1: Виправлення десинхронізації таймстемпів

```bash
# 🔹 Проблема: аудіо відстає на 2 секунди від відео
# 🔹 Рішення: скинути BaseMediaDecodeTime у аудіо-фрагментах

# 🔹 Для кожного аудіо-сегмента:
for seg in audio_*.m4s; do
    mp4tool edit -base_media_decode_time=0 "$seg" "fixed_$seg"
done

# 🔹 Результат: аудіо синхронізоване з відео ✅
```

---

### 🔹 Приклад 2: Очищення файлу від непотрібних метаданих

```bash
# 🔹 Видалити користувацькі метадані та порожні бокси:
$ mp4tool edit -drop=udta,free,skip input.mp4 cleaned.mp4

# 🔹 Перевірка:
$ mp4tool dump cleaned.mp4 | grep -E "\[udta\]|\[free\]|\[skip\]"
# ← немає виводу = бокси видалено ✅
```

---

### 🔹 Приклад 3: Підготовка фрагментів для об'єднання

```bash
# 🔹 Проблема: фрагменти мають різні базові часи
# 🔹 Рішення: встановити послідовні BaseMediaDecodeTime

# 🔹 Скрипт для об'єднання:
base_time=0
for seg in segment_*.m4s; do
    mp4tool edit -base_media_decode_time=$base_time "$seg" "processed_$seg"
    
    # 🔹 Розрахунок наступного базового часу (спрощено)
    duration=$(ffprobe -v quiet -show_entries format=duration -of csv=p=0 "processed_$seg")
    base_time=$(echo "$base_time + $duration * 90000" | bc)  # 90kHz timescale
done

# 🔹 Результат: фрагменти готові до конкатенації ✅
```

---

### 🔹 Приклад 4: Інтеграція у CCTV HLS Processor

```go
// 🔹 Функція для корекції таймстемпів у реальному часі
func fixFragmentTimestamps(inputPath, outputPath string, baseTime uint64) error {
    // 🔹 Виклик mp4tool edit як підпроцес
    cmd := exec.Command("mp4tool", "edit", 
        fmt.Sprintf("-base_media_decode_time=%d", baseTime),
        inputPath, outputPath)
    
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to fix timestamps: %w", err)
    }
    return nil
}

// 🔹 Використання у конвеєрі обробки:
go func() {
    for fragment := range fragmentQueue {
        // 🔹 Корекція таймстемпів перед додаванням у плейлист
        if err := fixFragmentTimestamps(fragment.Input, fragment.Output, fragment.BaseTime); err != nil {
            log.Printf("❌ Failed to fix %s: %v", fragment.Input, err)
            continue
        }
        
        // 🔹 Додавання у HLS-плейлист
        addToPlaylist(fragment.Output, fragment.Duration)
    }
}()
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Видалення обов'язкових боксів | Плеєр не може відтворити файл | Не видаляйте `ftyp`, `moov`, `trak`, `stbl` — тільки опціональні бокси |
| Неправильний `BaseMediaDecodeTime` | Десинхронізація або "стрибки" у відтворенні | Розраховуйте значення на основі `timescale`: `seconds * timescale` |
| Видалення боксів з дітьми | Втрата вкладених даних | Пам'ятайте: видалення батьківського боксу видаляє всіх дітей |
| Ігнорування версії `tfdt` | Переповнення 32-бітного поля | Перевіряйте `GetVersion()`: версія 0 → 32 біти, версія 1 → 64 біти |
| Забути `EndBox()` | Пошкоджений заголовок → невалідний файл | Завжди викликайте `w.EndBox()` після запису вмісту |

---

## 📋 Чекліст для вашого проекту

```
[ ] При модифікації таймстемпів:
    • Розраховуйте BaseMediaDecodeTime як: seconds * timescale
    • Перевіряйте версію tfdt: GetVersion() == 0 → uint32, else → uint64
    • Тестуйте відтворення після модифікації у різних плеєрах

[ ] При видаленні боксів:
    • Видаляйте тільки опціональні бокси: free, skip, udta, sgpd, sbgp
    • Не видаляйте обов'язкові: ftyp, moov, trak, stbl, stsd
    • Перевіряйте, чи не залежать інші бокси від видалених

[ ] Для ефективності:
    • Використовуйте bufseekio з blockSize=128KB для великих файлів
    • CopyBox для mdat та непідтримуваних типів — без парсингу
    • Уникайте зайвих викликів Marshal для боксів без змін

[ ] Для дебагу:
    • Порівнюйте вивід dump до/після: mp4tool dump input.mp4 > before.txt
    • Перевіряйте розміри боксів: чи оновилися заголовки коректно
    • Логувайте модифікації: log.Printf("✏️  Modified tfdt: %d → %d", old, new)

[ ] Для тестування:
    • Створюйте тестові файли з відомою структурою
    • Перевіряйте, що модифікації не ламають відтворення
    • Тестуйте крайні випадки: максимальні значення, порожні файли, DRM
```

---

## 🎯 Висновок

> **`edit` — це "хірургічний інструмент" для метаданих MP4**, який забезпечує:
> • ✅ Швидку модифікацію полів без перекодування медіа-даних
> • ✅ Безпечне видалення непотрібних боксів з автоматичним оновленням офсетів
> • ✅ Підтримку версій боксів (tfdt v0/v1) для коректної обробки
> • ✅ Ефективне копіювання великих боксів через CopyBox
> • ✅ Двофазний запис через Writer для автоматичного оновлення розмірів

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Миттєва корекція таймстемпів у фрагментах без перекодування відео
- 🔧 Гнучке очищення метаданих для приватності або сумісності
- 🔄 Легке підготовка фрагментів для об'єднання у єдиний потік
- 🛡️ Надійність: автоматичне оновлення заголовків запобігає пошкодженню файлів

Потребуєте допомоги з інтеграцією `edit` у ваш конвеєр корекції таймстемпів або з реалізацією кастомних модифікацій боксів? Напишіть — покажу готовий код для вашого сценарію! 🚀✏️