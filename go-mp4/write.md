# ✍️ `writer.go`: Зручний запис боксів у MP4 з автоматичним оновленням заголовків

Це **допоміжний модуль бібліотеки `go-mp4`**, який надає зручний API для **послідовного запису вкладених боксів** у форматі MP4/ISOBMFF з автоматичним оновленням розмірів у заголовках.

---

## 🎯 Коротка відповідь

> **Це "помічник із запису"**: він дозволяє створювати вкладені структури боксів без необхідності заздалегідь знати їхній розмір — автоматично оновлює заголовки з правильним розміром після завершення запису вмісту, підтримуючи вкладеність будь-якої глибини.

---

## 🧱 Основні типи та функції

### 🔹 `Writer` — обгортка для запису з підтримкою вкладеності

```go
type Writer struct {
    writer  io.WriteSeeker  // 🔹 Базовий записувач (файл, буфер, мережа)
    biStack []*BoxInfo      // 🔹 Стек для відстеження вкладених боксів
}
```

**🎯 Призначення**: Забезпечити **зручний інтерфейс** для запису ієрархічної структури боксів, де розмір батьківського боксу залежить від розмірів дітей.

**🔑 Ключова ідея**: 
```
📦 moov (розмір невідомий на початку)
├── 📦 mvhd (108 байт)
├── 📦 trak (2048 байт)
│   ├── 📦 tkhd (92 байти)
│   └── 📦 mdia (1848 байт)
└── 📦 udta (133 байти)

🔹 Проблема: як записати розмір moov, не знаючи розмірів дітей?
🔹 Рішення: Writer.StartBox() → запис дітей → Writer.EndBox() → оновлення заголовка moov
```

---

### 🔹 `NewWriter` — конструктор

```go
func NewWriter(w io.WriteSeeker) *Writer {
    return &Writer{
        writer: w,  // 🔹 Приймає будь-який io.WriteSeeker
    }
}
```

**🎯 Призначення**: Створити екземпляр `Writer` для запису у файл, буфер або мережевий потік.

**Приклад:**
```go
// 🔹 Запис у файл
f, _ := os.Create("output.mp4")
w := mp4.NewWriter(f)

// 🔹 Запис у буфер (для тестів)
var buf bytes.Buffer
w := mp4.NewWriter(&buf)
```

---

### 🔹 `Write` / `Seek` — делегування базовому інтерфейсу

```go
func (w *Writer) Write(p []byte) (int, error) {
    return w.writer.Write(p)  // 🔹 Прямий запис байт
}

func (w *Writer) Seek(offset int64, whence int) (int64, error) {
    return w.writer.Seek(offset, whence)  // 🔹 Позиціонування у потоці
}
```

**🎯 Призначення**: Зробити `Writer` сумісним з `io.WriteSeeker` для використання у стандартних функціях на кшталт `io.CopyN`.

---

### 🔹 `StartBox` — початок запису боксу

```go
func (w *Writer) StartBox(bi *BoxInfo) (*BoxInfo, error) {
    // 🔹 Крок 1: Записати заголовок боксу (з тимчасовим розміром)
    bi, err := WriteBoxInfo(w.writer, bi)
    if err != nil { return nil, err }
    
    // 🔹 Крок 2: Додати у стек для відстеження вкладеності
    w.biStack = append(w.biStack, bi)
    
    return bi, nil  // 🔹 Повертаємо бі з актуальним офсетом
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: BoxInfo{Type:"trak", Size:0}  // ← Size=0 = "не відомо ще"
│
▼
🔹 WriteBoxInfo(w.writer, bi):
   • Записує: [розмір=0][тип="trak"]  // ← тимчасовий заголовок
   • Повертає: bi з актуальним офсетом (напр. 1024)
│
▼
🔹 Додавання у стек: w.biStack = [..., bi]
│
▼
🔹 Вихід: *BoxInfo{Offset:1024, Size:0, Type:"trak"}
```

**🎯 Призначення**: Підготувати місце для боксу, записавши заголовок із тимчасовим розміром, і запам'ятати позицію для подальшого оновлення.

---

### 🔹 `EndBox` — завершення запису боксу з оновленням заголовка

```go
func (w *Writer) EndBox() (*BoxInfo, error) {
    // 🔹 Крок 1: Отримати бі з вершини стека
    bi := w.biStack[len(w.biStack)-1]
    w.biStack = w.biStack[:len(w.biStack)-1]  // 🔹 Видалити зі стека
    
    // 🔹 Крок 2: Запам'ятати поточну позицію (кінець вмісту боксу)
    end, err := w.writer.Seek(0, io.SeekCurrent)
    if err != nil { return nil, err }
    
    // 🔹 Крок 3: Розрахувати реальний розмір
    bi.Size = uint64(end) - bi.Offset  // 🔹 кінець - початок = розмір
    
    // 🔹 Крок 4: Повернутись на початок боксу для оновлення заголовка
    if _, err = bi.SeekToStart(w.writer); err != nil { return nil, err }
    
    // 🔹 Крок 5: Перезаписати заголовок з правильним розміром
    if bi2, err := WriteBoxInfo(w.writer, bi); err != nil {
        return nil, err
    } else if bi.HeaderSize != bi2.HeaderSize {
        // 🔹 Обробка зміни розміру заголовка (напр. 4→8 байт для large size)
        return nil, errors.New("header size changed")
    }
    
    // 🔹 Крок 6: Повернутись на кінець боксу для продовження запису
    if _, err := w.writer.Seek(end, io.SeekStart); err != nil {
        return nil, err
    }
    
    return bi, nil  // 🔹 Повертаємо бі з оновленим розміром
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: стек з бі "trak" на вершині, поточна позиція = кінець вмісту
│
▼
🔹 Розрахунок розміру:
   • end = 3072 (поточна позиція)
   • bi.Offset = 1024 (початок боксу)
   • bi.Size = 3072 - 1024 = 2048 ✅

🔹 Оновлення заголовка:
   • Seek(1024) ← повертаємось на початок боксу
   • WriteBoxInfo() ← перезаписуємо [2048]["trak"] замість [0]["trak"]
   • Перевірка: чи не змінився розмір заголовка? (напр. якщо Size > 2^32)

🔹 Відновлення позиції:
   • Seek(3072) ← повертаємось на кінець для запису наступного боксу

🔹 Вихід: *BoxInfo{Offset:1024, Size:2048, Type:"trak"}
```

**🎯 Ключова особливість**: **Двофазний запис** — спочатку заголовок із тимчасовим розміром, потім оновлення після запису вмісту.

---

### 🔹 `CopyBox` — ефективне копіювання боксу без парсингу

```go
func (w *Writer) CopyBox(r io.ReadSeeker, bi *BoxInfo) error {
    // 🔹 Крок 1: Позиціонування на початок вихідного боксу
    if _, err := bi.SeekToStart(r); err != nil { return err }
    
    // 🔹 Крок 2: Пряме копіювання байт (заголовок + вміст)
    if n, err := io.CopyN(w, r, int64(bi.Size)); err != nil {
        return err
    } else if n != int64(bi.Size) {
        return errors.New("failed to copy box")  // 🔹 Перевірка повноти копіювання
    }
    
    return nil
}
```

**🎯 Призначення**: Швидко скопіювати цілий бокс з одного файлу в інший **без парсингу вмісту** — ідеально для перепакування, фільтрації або модифікації лише окремих боксів.

**Приклад використання:**
```go
// 🔹 Копіювання всіх боксів, крім "free"
handler := func(h *mp4.ReadHandle) (interface{}, error) {
    if h.BoxInfo.Type != mp4.BoxTypeFree() {
        // 🔹 Копіюємо бокс без парсингу
        return nil, writer.CopyBox(reader, &h.BoxInfo)
    }
    // 🔹 Пропускаємо "free" бокси
    return h.Expand()
}
```

---

## 🔍 Повний приклад: Створення вкладеної структури боксів

```go
func createSampleMP4(filePath string) error {
    f, err := os.Create(filePath)
    if err != nil { return err }
    defer f.Close()
    
    w := mp4.NewWriter(f)  // 🔹 Створюємо Writer
    
    // 🔹 Крок 1: Початок файлового боксу (ftyp)
    ftyp := &mp4.BoxInfo{Type: mp4.BoxTypeFtyp()}
    w.StartBox(ftyp)
    f.Write([]byte("isom"))  // MajorBrand
    f.Write([]byte{0,0,2,0}) // MinorVersion
    f.Write([]byte("isomavc1mp41")) // CompatibleBrands
    w.EndBox()  // 🔹 Оновлюємо заголовок ftyp з розміром 24
    
    // 🔹 Крок 2: Початок moov (контейнер метаданих)
    moov := &mp4.BoxInfo{Type: mp4.BoxTypeMoov()}
    w.StartBox(moov)  // ← Size=0, Offset=24
    
    // 🔹 Вкладений mvhd
    mvhd := &mp4.BoxInfo{Type: mp4.BoxTypeMvhd()}
    w.StartBox(mvhd)
    // ... запис вмісту mvhd (108 байт) ...
    w.EndBox()  // ← Оновлюємо заголовок mvhd: Size=108
    
    // 🔹 Вкладений trak
    trak := &mp4.BoxInfo{Type: mp4.BoxTypeTrak()}
    w.StartBox(trak)
    // ... запис tkhd, mdia, тощо ...
    w.EndBox()  // ← Оновлюємо заголовок trak: Size=2048
    
    // 🔹 Завершення moov
    w.EndBox()  // ← Оновлюємо заголовок moov: Size=10240
    
    // 🔹 Крок 3: mdat (медіа-дані)
    mdat := &mp4.BoxInfo{Type: mp4.BoxTypeMdat()}
    w.StartBox(mdat)
    // ... запис сирих відео/аудіо даних ...
    w.EndBox()  // ← Оновлюємо заголовок mdat з реальним розміром
    
    return nil
}
```

**🔄 Потік даних:**
```
🔹 StartBox(ftyp) → запис [0]["ftyp"] @ offset=0
🔹 Запис вмісту ftyp (20 байт)
🔹 EndBox() → Size=24, перезапис заголовка: [24]["ftyp"] @ offset=0

🔹 StartBox(moov) → запис [0]["moov"] @ offset=24
🔹 StartBox(mvhd) → запис [0]["mvhd"] @ offset=32
🔹 Запис вмісту mvhd (108 байт)
🔹 EndBox(mvhd) → Size=108, перезапис: [108]["mvhd"] @ offset=32
🔹 ... інші вкладені бокси ...
🔹 EndBox(moov) → Size=10240, перезапис: [10240]["moov"] @ offset=24

🔹 StartBox(mdat) → запис [0]["mdat"] @ offset=10264
🔹 Запис медіа-даних (напр. 1 МБ)
🔹 EndBox(mdat) → Size=1048576, перезапис: [1048576]["mdat"] @ offset=10264
```

---

## ⚠️ Обробка зміни розміру заголовка

```go
if bi2, err := WriteBoxInfo(w.writer, bi); err != nil {
    return nil, err
} else if bi.HeaderSize != bi2.HeaderSize {
    return nil, errors.New("header size changed")
}
```

**🎯 Проблема**: Якщо розмір боксу перевищує `2^32-1`, заголовок має змінитися з **small** (4 байти для розміру) на **large** (8 байт для розміру + 4 байти `size=1`).

**Приклад:**
```
🔹 Початок: Size=0 → WriteBoxInfo() записує small header (8 байт загалом)
🔹 Кінець: Size=5000000000 (>2^32) → WriteBoxInfo() намагається записати large header (16 байт загалом)
🔹 Результат: bi.HeaderSize=8, bi2.HeaderSize=16 → помилка "header size changed"

🔹 Рішення: Завжди використовувати large header для боксів, розмір яких може перевищити 4 ГБ:
   bi := &BoxInfo{Type: mp4.BoxTypeMdat(), Size: 1}  // Size=1 → примусово large header
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Генерація fMP4-сегмента з динамічним вмістом

```go
func generateFragmentedSegment(seq int, videoFrames []Frame, audioSamples []Sample) error {
    filePath := fmt.Sprintf("segment_%06d.m4s", seq)
    f, err := os.Create(filePath)
    if err != nil { return err }
    defer f.Close()
    
    w := mp4.NewWriter(f)
    
    // 🔹 moof: метадані фрагмента
    moof := &mp4.BoxInfo{Type: mp4.BoxTypeMoof()}
    w.StartBox(moof)
    
    // 🔹 mfhd: заголовок фрагмента
    mfhd := &mp4.BoxInfo{Type: mp4.BoxTypeMfhd()}
    w.StartBox(mfhd)
    binary.Write(f, binary.BigEndian, uint32(0))  // Version+Flags
    binary.Write(f, binary.BigEndian, uint32(seq))  // SequenceNumber
    w.EndBox()
    
    // 🔹 traf: метадані доріжки
    traf := &mp4.BoxInfo{Type: mp4.BoxTypeTraf()}
    w.StartBox(traf)
    
    // 🔹 tfhd: заголовок доріжки фрагмента
    tfhd := &mp4.BoxInfo{Type: mp4.BoxTypeTfhd()}
    w.StartBox(tfhd)
    // ... запис полів tfhd ...
    w.EndBox()
    
    // 🔹 trun: таймстемпи та розміри семплів
    trun := &mp4.BoxInfo{Type: mp4.BoxTypeTrun()}
    w.StartBox(trun)
    // ... запис SampleCount, Entries ...
    w.EndBox()
    
    w.EndBox()  // traf
    w.EndBox()  // moof
    
    // 🔹 mdat: сирі медіа-дані
    mdat := &mp4.BoxInfo{Type: mp4.BoxTypeMdat()}
    w.StartBox(mdat)
    
    // 🔹 Запис відео-кадрів
    for _, frame := range videoFrames {
        binary.Write(f, binary.BigEndian, uint32(len(frame.Data)))  // NAL length
        f.Write(frame.Data)
    }
    
    // 🔹 Запис аудіо-семплів
    for _, sample := range audioSamples {
        f.Write(sample.Data)
    }
    
    w.EndBox()  // mdat — оновлює заголовок з реальним розміром
    
    return nil
}
```

---

### 🔹 Приклад 2: Фільтрація та перепакування існуючого файлу

```go
func filterAndReplicate(inputPath, outputPath string, keepTypes []mp4.BoxType) error {
    in, err := os.Open(inputPath)
    if err != nil { return err }
    defer in.Close()
    
    out, err := os.Create(outputPath)
    if err != nil { return err }
    defer out.Close()
    
    w := mp4.NewWriter(out)
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Перевірка: чи зберігаємо цей тип боксу?
        keep := false
        for _, t := range keepTypes {
            if h.BoxInfo.Type == t {
                keep = true
                break
            }
        }
        
        if keep {
            // 🔹 Копіюємо бокс без парсингу (ефективно!)
            return nil, w.CopyBox(in, &h.BoxInfo)
        }
        
        // 🔹 Ігноруємо бокс, але рекурсивно обробляємо дітей
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(in, handler)
    return err
}

// 🔹 Використання: зберегти тільки moov + mdat, видалити free/udta
filterAndReplicate("input.mp4", "output.mp4", 
    []mp4.BoxType{mp4.BoxTypeMoov(), mp4.BoxTypeMdat()})
```

---

### 🔹 Приклад 3: Модифікація існуючого боксу (напр. оновлення тривалості)

```go
func updateDuration(filePath string, newDuration uint64) error {
    // 🔹 Крок 1: Знайти mvhd бокс
    f, err := os.OpenFile(filePath, os.O_RDWR, 0644)
    if err != nil { return err }
    defer f.Close()
    
    var mvhdBi *mp4.BoxInfo
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeMvhd() {
            mvhdBi = &h.BoxInfo  // 🔹 Запам'ятовуємо метадані
            return nil, nil  // 🔹 Не розгортаємо дітей
        }
        return h.Expand()
    }
    mp4.ReadBoxStructure(f, handler)
    
    if mvhdBi == nil {
        return fmt.Errorf("mvhd box not found")
    }
    
    // 🔹 Крок 2: Переписати Duration поле
    w := mp4.NewWriter(f)
    
    // 🔹 Позиціонування на початок mvhd
    mvhdBi.SeekToStart(f)
    
    // 🔹 Читання та модифікація заголовка
    var mvhd mp4.Mvhd
    mp4.Unmarshal(f, mvhdBi.Size-mvhdBi.HeaderSize, &mvhd, mvhdBi.Context)
    mvhd.DurationV0 = uint32(newDuration)  // 🔹 Оновлення тривалості
    
    // 🔹 Запис оновленого вмісту
    w.StartBox(mvhdBi)  // ← Запис заголовка з тимчасовим розміром
    mp4.Marshal(f, &mvhd, mvhdBi.Context)  // ← Запис оновленого вмісту
    w.EndBox()  // ← Оновлення заголовка з правильним розміром
    
    return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути викликати `EndBox()` | Заголовок боксу залишається з розміром 0 → пошкоджений файл | Завжди викликайте `EndBox()` після завершення запису вмісту боксу |
| Неправильний порядок `StartBox`/`EndBox` | Стек розбалансований → помилка "index out of range" | Дотримуйтесь принципу LIFO: останній `StartBox` → перший `EndBox` |
| Ігнорування зміни розміру заголовка | Помилка "header size changed" для боксів >4 ГБ | Використовуйте `Size: 1` у `BoxInfo` для примусового large header |
| Запис після `EndBox()` без позиціонування | Дані записуються у неправильне місце → пошкодження файлу | `EndBox()` вже повертає позицію на кінець боксу — продовжуйте запис звідти |
| Використання `CopyBox` для модифікованих боксів | Копіюється старий вміст, а не новий | Для модифікації: парсіть → змінюйте → записуйте через `StartBox`/`EndBox`, не `CopyBox` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні нових файлів:
    • Використовуйте Writer.StartBox() / EndBox() для всіх вкладених боксів
    • Для боксів, розмір яких може перевищити 4 ГБ: встановлюйте Size=1 у BoxInfo
    • Дотримуйтесь порядку: батьківський StartBox → діти → батьківський EndBox

[ ] При копіюванні/фільтрації:
    • Використовуйте CopyBox() для ефективності (без парсингу)
    • Перевіряйте повернене n == bi.Size для гарантії повноти копіювання
    • Комбінуйте з ReadBoxStructure для селективного копіювання за типом

[ ] При модифікації існуючих файлів:
    • Відкривайте файл у режимі os.O_RDWR для запису на місце
    • Знаходьте цільовий бокс через ReadBoxStructure перед модифікацією
    • Використовуйте StartBox/EndBox для оновлення заголовка після змін

[ ] Для дебагу:
    • Логувайте стек: log.Printf("📚 Stack depth: %d", len(w.biStack))
    • Перевіряйте офсети: log.Printf("📍 Box %s @ %d+%d", bi.Type, bi.Offset, bi.Size)
    • Відстежуйте зміну розміру заголовка: if bi.HeaderSize != bi2.HeaderSize { ... }

[ ] Для тестування:
    • Створюйте тестові файли з відомою структурою та порівнюйте розміри боксів
    • Перевіряйте коректність оновлення заголовків після EndBox()
    • Тестуйте крайні випадки: порожні бокси, вкладеність >10 рівнів, розмір >4 ГБ
```

---

## 🎯 Висновок

> **`writer.go` — це "помічник із запису"**, який забезпечує:
> • ✅ Зручне створення вкладених структур боксів без попереднього знання розмірів
> • ✅ Автоматичне оновлення заголовків з правильними розмірами після завершення запису
> • ✅ Підтримку будь-якої глибини вкладеності через стек `biStack`
> • ✅ Ефективне копіювання боксів без парсингу через `CopyBox`
> • ✅ Безпечну обробку зміни розміру заголовка (small → large)

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Швидка генерація fMP4-сегментів з динамічним вмістом без попереднього розрахунку розмірів
- 🔧 Гнучка фільтрація та перепакування існуючих файлів з мінімальними накладними витратами
- 🛡️ Надійність: автоматичне оновлення заголовків запобігає пошкодженню файлів
- 🔄 Легка модифікація метаданих (тривалість, таймстемпи) без переписування всього файлу

Потребуєте допомоги з інтеграцією `Writer` у ваш конвеєр генерації сегментів або з реалізацією фільтрації боксів? Напишіть — покажу готовий код для вашого сценарію! 🚀✍️