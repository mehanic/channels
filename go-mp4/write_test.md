# 🧪 Тест `TestWriter`: Перевірка запису боксів у MP4 з автоматичним оновленням заголовків

Це **інтеграційний тест** для модуля `writer.go` бібліотеки `go-mp4`, який перевіряє коректність роботи **двофазного запису вкладених боксів** з автоматичним оновленням розмірів у заголовках після завершення запису вмісту.

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що `Writer` коректно обробляє вкладені структури боксів**: записує заголовки з тимчасовими розмірами, дозволяє запис вмісту, а потім оновлює заголовки з правильними розмірами — навіть при модифікації вже записаних даних.

---

## 📋 Структура тесту

```go
func TestWriter(t *testing.T) {
    // 🔹 Створення in-memory файлу для тесту
    output, err := memfs.New().Create("output.mp4")
    require.NoError(t, err)
    defer output.Close()
    
    w := NewWriter(output)  // 🔹 Створюємо Writer
    
    // 🔹 Крок 1: Запис ftyp боксу
    bi, err := w.StartBox(&BoxInfo{Type: BoxTypeFtyp()})
    require.NoError(t, err)
    assert.Equal(t, uint64(0), bi.Offset)  // ✅ Початок файлу
    assert.Equal(t, uint64(8), bi.Size)    // ✅ Тимчасовий розмір: тільки заголовок
    
    // 🔹 Запис вмісту ftyp через Marshal
    ftyp := &Ftyp{
        MajorBrand:   [4]byte{'a','b','e','m'},
        MinorVersion: 0x12345678,
        CompatibleBrands: []CompatibleBrandElem{
            {CompatibleBrand: [4]byte{'a','b','c','d'}},
            {CompatibleBrand: [4]byte{'e','f','g','h'}},
        },
    }
    _, err = Marshal(w, ftyp, Context{})
    require.NoError(t, err)
    
    // 🔹 Завершення ftyp: оновлення заголовка з реальним розміром
    bi, err = w.EndBox()
    require.NoError(t, err)
    assert.Equal(t, uint64(0), bi.Offset)   // ✅ Початок не змінився
    assert.Equal(t, uint64(24), bi.Size)    // ✅ Реальний розмір: 8 (заголовок) + 16 (вміст)
    
    // 🔹 Крок 2: Початок moov (вкладений контейнер)
    bi, err = w.StartBox(&BoxInfo{Type: BoxTypeMoov()})
    require.NoError(t, err)
    assert.Equal(t, uint64(24), bi.Offset)  // ✅ Після ftyp
    assert.Equal(t, uint64(8), bi.Size)     // ✅ Тимчасовий розмір
    
    // 🔹 Крок 3: CopyBox — копіювання існуючого боксу без парсингу
    err = w.CopyBox(bytes.NewReader([]byte{
        0x00,0x00,0x00,0x00,0x00,0x00,0x00,0x00,  // ← тимчасовий заголовок (ігнорується)
        0x00,0x00,0x00,0x0a, 'u','d','t','a',     // ← реальний заголовок: size=10, type="udta"
        0x01,0x02,0x03,0x04, 0x05,0x06,0x07,0x08,  // ← вміст (8 байт)
    }), &BoxInfo{Offset: 6, Size: 15})  // 🔹 Offset=6 пропускає перші 6 байт вхідного буфера
    require.NoError(t, err)
    // ✅ Після CopyBox: записано 15 байт (заголовок 8 + вміст 7)
    
    // 🔹 Крок 4: Вкладена структура trak → tkhd
    bi, err = w.StartBox(&BoxInfo{Type: BoxTypeTrak()})
    require.NoError(t, err)
    assert.Equal(t, uint64(47), bi.Offset)  // ✅ 24 (ftyp) + 8 (moov header) + 15 (udta) = 47
    
    bi, err = w.StartBox(&BoxInfo{Type: BoxTypeTkhd()})
    require.NoError(t, err)
    assert.Equal(t, uint64(55), bi.Offset)  // ✅ 47 + 8 (trak header) = 55
    
    // 🔹 Запис вмісту tkhd через Marshal
    _, err = Marshal(w, &Tkhd{
        CreationTimeV0: 1, ModificationTimeV0: 2, TrackID: 3, DurationV0: 4,
        Layer: 5, AlternateGroup: 6, Volume: 7, Width: 8, Height: 9,
    }, Context{})
    require.NoError(t, err)
    
    // 🔹 Завершення tkhd: оновлення заголовка
    bi, err = w.EndBox()
    require.NoError(t, err)
    assert.Equal(t, uint64(55), bi.Offset)  // ✅ Початок не змінився
    assert.Equal(t, uint64(92), bi.Size)    // ✅ 8 (header) + 84 (вміст Tkhd) = 92
    
    // 🔹 Завершення trak: оновлення заголовка
    bi, err = w.EndBox()
    require.NoError(t, err)
    assert.Equal(t, uint64(47), bi.Offset)  // ✅ Початок trak
    assert.Equal(t, uint64(100), bi.Size)   // ✅ 8 (header) + 92 (tkhd) = 100
    
    // 🔹 Завершення moov: оновлення заголовка
    bi, err = w.EndBox()
    require.NoError(t, err)
    assert.Equal(t, uint64(24), bi.Offset)  // ✅ Початок moov
    assert.Equal(t, uint64(123), bi.Size)   // ✅ 8 (header) + 15 (udta) + 100 (trak) = 123
    
    // 🔹 Крок 5: Модифікація вже записаних даних (оновлення ftyp)
    n, err := w.Seek(8, io.SeekStart)  // 🔹 Позиціонування після заголовка ftyp
    require.NoError(t, err)
    assert.Equal(t, int64(8), n)
    
    ftyp.CompatibleBrands[1].CompatibleBrand = [4]byte{'E','F','G','H'}  // 🔹 Зміна даних
    _, err = Marshal(w, ftyp, Context{})  // 🔹 Перезапис вмісту без оновлення заголовка
    require.NoError(t, err)
    
    // 🔹 Крок 6: Перевірка фінального вмісту файлу
    _, err = output.Seek(0, io.SeekStart)
    require.NoError(t, err)
    bin, err := io.ReadAll(output)
    require.NoError(t, err)
    
    // 🔹 Очікуваний вміст (байт в байт):
    assert.Equal(t, []byte{
        // 🔹 ftyp (24 байти)
        0x00,0x00,0x00,0x18, 'f','t','y','p',  // ← size=24, type="ftyp"
        'a','b','e','m', 0x12,0x34,0x56,0x78,  // ← MajorBrand, MinorVersion
        'a','b','c','d', 'E','F','G','H',      // ← CompatibleBrands (другий змінено!)
        
        // 🔹 moov (123 байти)
        0x00,0x00,0x00,0x7b, 'm','o','o','v',  // ← size=123 (0x7b)
        
        // 🔹 udta (копійований, 15 байт)
        0x00,0x00,0x00,0x0a, 'u','d','t','a',  // ← size=10
        0x01,0x02,0x03,0x04, 0x05,0x06,0x07,   // ← вміст (7 байт, останній 0x08 обрізано через Size=15)
        
        // 🔹 trak (100 байт)
        0x00,0x00,0x00,0x64, 't','r','a','k',  // ← size=100 (0x64)
        
        // 🔹 tkhd (92 байти)
        0x00,0x00,0x00,0x5c, 't','k','h','d',  // ← size=92 (0x5c)
        0, 0x00,0x00,0x00,  // ← Version=0, Flags=0x000000
        // ... решта полів Tkhd ...
        0x00,0x00,0x00,0x08,  // ← Width=8
        0x00,0x00,0x00,0x09,  // ← Height=9
    }, bin)
}
```

---

## 🔍 Детальний розбір ключових перевірок

### 🔹 Двофазний запис: `StartBox` → вміст → `EndBox`

```
🔹 ftyp бокс:
1. StartBox(): запис [0]["ftyp"] @ offset=0, Size=8 (тимчасовий)
2. Marshal(): запис 16 байт вмісту (MajorBrand + MinorVersion + 2×CompatibleBrand)
3. EndBox(): 
   • end = 24 (поточна позиція)
   • Size = 24 - 0 = 24
   • Seek(0) → перезапис заголовка: [24]["ftyp"]
   • Seek(24) → повернення на кінець

✅ Результат: заголовок оновлено з правильним розміром
```

**🎯 Призначення**: Дозволити запис вкладених структур без попереднього розрахунку розмірів.

---

### 🔹 Вкладеність: стек `biStack` для відстеження батьків

```
🔹 Стек під час запису:
1. StartBox(moov) → stack = [moov@24]
2. StartBox(trak) → stack = [moov@24, trak@47]
3. StartBox(tkhd) → stack = [moov@24, trak@47, tkhd@55]
4. EndBox(tkhd)   → stack = [moov@24, trak@47], оновлено tkhd.Size=92
5. EndBox(trak)   → stack = [moov@24], оновлено trak.Size=100
6. EndBox(moov)   → stack = [], оновлено moov.Size=123

✅ LIFO порядок гарантує правильне оновлення батьків після дітей
```

**🎯 Призначення**: Автоматичне відстеження вкладеності без явного управління позиціями.

---

### 🔹 `CopyBox`: ефективне копіювання без парсингу

```
🔹 Вхідний буфер (22 байти):
[0-5]: 00 00 00 00 00 00          ← ігнорується (Offset=6)
[6-13]: 00 00 00 0a 'u' 'd' 't' 'a'  ← заголовок: size=10, type="udta"
[14-21]: 01 02 03 04 05 06 07 08  ← вміст (8 байт)

🔹 BoxInfo{Offset:6, Size:15}:
• Починаємо читати з байту 6
• Копіюємо 15 байт: [6-20] = заголовок(8) + вміст(7)
• Останній байт 0x08 не копіюється (15 < 16)

✅ Результат: записано 15 байт без парсингу структури udta
```

**🎯 Призначення**: Швидке копіювання боксів, вміст яких не потрібно змінювати.

---

### 🔹 Модифікація вже записаних даних

```
🔹 Оновлення ftyp після EndBox():
1. Seek(8) → позиціонування після заголовка ftyp (offset=8)
2. Зміна даних: CompatibleBrands[1] = "EFGH" замість "efgh"
3. Marshal() → перезапис 16 байт вмісту (без зміни заголовка!)

✅ Результат: дані оновлено, заголовок залишився правильним (Size=24 не змінився)
```

**🎯 Призначення**: Дозволити модифікацію вмісту без необхідності переписувати весь файл.

---

## 🔍 Перевірка фінального вмісту: байт в байт

```
🔹 Очікуваний вихід (147 байт загалом):

📦 ftyp (24 байти):
00 00 00 18  ← size=24 (0x18)
66 74 79 70  ← "ftyp"
61 62 65 6d  ← "abem" (MajorBrand)
12 34 56 78  ← MinorVersion
61 62 63 64  ← "abcd" (CompatibleBrand #1)
45 46 47 48  ← "EFGH" (CompatibleBrand #2, змінено!)

📦 moov (123 байти = 0x7b):
00 00 00 7b  ← size=123
6d 6f 6f 76  ← "moov"
[15 байт udta] + [100 байт trak]

📦 udta (15 байт, скопійовано):
00 00 00 0a  ← size=10
75 64 74 61  ← "udta"
01 02 03 04 05 06 07  ← вміст (7 байт)

📦 trak (100 байт = 0x64):
00 00 00 64  ← size=100
74 72 61 6b  ← "trak"
[92 байти tkhd]

📦 tkhd (92 байти = 0x5c):
00 00 00 5c  ← size=92
74 6b 68 64  ← "tkhd"
00 00 00 00  ← Version=0, Flags=0x000000
... решта полів ...
00 00 00 08  ← Width=8
00 00 00 09  ← Height=9
```

**🎯 Ключові перевірки:**
- ✅ Розміри заголовків оновлено після `EndBox()`
- ✅ Вкладеність оброблено правильно (moov → trak → tkhd)
- ✅ `CopyBox` скопіював рівно 15 байт, обрізавши останній
- ✅ Модифікація ftyp не зламала заголовок
- ✅ Всі офсети співпадають з очікуваними

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Генерація fMP4-сегмента з динамічним вмістом

```go
func generateFragment(seq int, frames []VideoFrame, samples []AudioSample) error {
    f, err := os.Create(fmt.Sprintf("seg_%06d.m4s", seq))
    if err != nil { return err }
    defer f.Close()
    
    w := mp4.NewWriter(f)
    
    // 🔹 moof: метадані фрагмента
    moof := &mp4.BoxInfo{Type: mp4.BoxTypeMoof()}
    w.StartBox(moof)
    
    // 🔹 mfhd: номер фрагмента
    mfhd := &mp4.BoxInfo{Type: mp4.BoxTypeMfhd()}
    w.StartBox(mfhd)
    binary.Write(f, binary.BigEndian, uint32(0))  // Version+Flags
    binary.Write(f, binary.BigEndian, uint32(seq))  // SequenceNumber
    w.EndBox()
    
    // 🔹 traf: метадані доріжки
    traf := &mp4.BoxInfo{Type: mp4.BoxTypeTraf()}
    w.StartBox(traf)
    
    // 🔹 tfhd: базові параметри
    tfhd := &mp4.BoxInfo{Type: mp4.BoxTypeTfhd()}
    w.StartBox(tfhd)
    // ... запис полів ...
    w.EndBox()
    
    // 🔹 trun: таймстемпи та розміри
    trun := &mp4.BoxInfo{Type: mp4.BoxTypeTrun()}
    w.StartBox(trun)
    // ... запис SampleCount, Entries ...
    w.EndBox()
    
    w.EndBox()  // traf
    w.EndBox()  // moof
    
    // 🔹 mdat: сирі дані
    mdat := &mp4.BoxInfo{Type: mp4.BoxTypeMdat()}
    w.StartBox(mdat)
    
    // 🔹 Запис відео
    for _, frame := range frames {
        binary.Write(f, binary.BigEndian, uint32(len(frame.Data)))
        f.Write(frame.Data)
    }
    
    // 🔹 Запис аудіо
    for _, sample := range samples {
        f.Write(sample.Data)
    }
    
    w.EndBox()  // mdat — оновлює заголовок з реальним розміром
    
    return nil
}
```

---

### 🔹 Приклад 2: Фільтрація та перепакування існуючого файлу

```go
func filterBoxes(inputPath, outputPath string, keepTypes []mp4.BoxType) error {
    in, err := os.Open(inputPath)
    if err != nil { return err }
    defer in.Close()
    
    out, err := os.Create(outputPath)
    if err != nil { return err }
    defer out.Close()
    
    w := mp4.NewWriter(out)
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Перевірка: чи зберігаємо цей тип?
        keep := false
        for _, t := range keepTypes {
            if h.BoxInfo.Type == t {
                keep = true
                break
            }
        }
        
        if keep {
            // 🔹 Ефективне копіювання без парсингу
            return nil, w.CopyBox(in, &h.BoxInfo)
        }
        
        // 🔹 Ігноруємо, але рекурсивно обробляємо дітей
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(in, handler)
    return err
}

// 🔹 Використання: зберегти тільки moov + mdat
filterBoxes("input.mp4", "output.mp4", 
    []mp4.BoxType{mp4.BoxTypeMoov(), mp4.BoxTypeMdat()})
```

---

### 🔹 Приклад 3: Оновлення метаданих без переписування всього файлу

```go
func updateTrackDuration(filePath string, trackID uint32, newDuration uint64) error {
    f, err := os.OpenFile(filePath, os.O_RDWR, 0644)
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 Крок 1: Знайти tkhd для потрібного trackID
    var tkhdBi *mp4.BoxInfo
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeTkhd() {
            box, _, _ := h.ReadPayload()
            if tkhd, ok := box.(*mp4.Tkhd); ok && tkhd.TrackID == trackID {
                tkhdBi = &h.BoxInfo
                return nil, nil
            }
        }
        return h.Expand()
    }
    mp4.ReadBoxStructure(f, handler)
    
    if tkhdBi == nil {
        return fmt.Errorf("track %d not found", trackID)
    }
    
    // 🔹 Крок 2: Оновити Duration
    w := mp4.NewWriter(f)
    tkhdBi.SeekToStart(f)
    
    var tkhd mp4.Tkhd
    mp4.Unmarshal(f, tkhdBi.Size-tkhdBi.HeaderSize, &tkhd, tkhdBi.Context)
    tkhd.DurationV0 = uint32(newDuration)  // 🔹 Оновлення
    
    // 🔹 Крок 3: Перезаписати вміст (заголовок не змінюється, бо розмір той самий)
    w.StartBox(tkhdBi)  // ← Запис заголовка (Size не зміниться)
    mp4.Marshal(f, &tkhd, tkhdBi.Context)  // ← Запис оновленого вмісту
    w.EndBox()  // ← Оновлення заголовка (Size залишиться тим самим)
    
    return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути `EndBox()` | Заголовок залишається з розміром 0 → пошкоджений файл | Завжди викликайте `EndBox()` після завершення запису вмісту боксу |
| Неправильний порядок `StartBox`/`EndBox` | Стек розбалансований → "index out of range" | Дотримуйтесь LIFO: останній `StartBox` → перший `EndBox` |
| Ігнорування зміни розміру заголовка | Помилка "header size changed" для боксів >4 ГБ | Використовуйте `Size: 1` у `BoxInfo` для примусового large header |
| Запис після `EndBox()` без позиціонування | Дані записуються у неправильне місце | `EndBox()` вже повертає позицію на кінець — продовжуйте запис звідти |
| Використання `CopyBox` для модифікованих боксів | Копіюється старий вміст | Для модифікації: парсіть → змінюйте → записуйте через `StartBox`/`EndBox` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні нових файлів:
    • Використовуйте Writer.StartBox() / EndBox() для всіх вкладених боксів
    • Для боксів >4 ГБ: встановлюйте Size=1 у BoxInfo для large header
    • Дотримуйтесь порядку: батьківський StartBox → діти → батьківський EndBox

[ ] При копіюванні/фільтрації:
    • Використовуйте CopyBox() для ефективності (без парсингу)
    • Перевіряйте n == bi.Size для гарантії повноти копіювання
    • Комбінуйте з ReadBoxStructure для селективного копіювання за типом

[ ] При модифікації існуючих файлів:
    • Відкривайте файл у os.O_RDWR для запису на місце
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

> **Цей тест — ваш "золотий стандарт" для надійного запису вкладених боксів**.  
> Він гарантує:
> • ✅ Коректну двофазну обробку: тимчасовий заголовок → вміст → оновлений заголовок
> • ✅ Правильне відстеження вкладеності через стек `biStack`
> • ✅ Ефективне копіювання без парсингу через `CopyBox`
> • ✅ Безпечну модифікацію вже записаних даних без переписування заголовків
> • ✅ Обробку зміни розміру заголовка (small → large) для боксів >4 ГБ

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Швидка генерація fMP4-сегментів з динамічним вмістом без попереднього розрахунку розмірів
- 🔧 Гнучка фільтрація та перепакування існуючих файлів з мінімальними накладними витратами
- 🛡️ Надійність: автоматичне оновлення заголовків запобігає пошкодженню файлів
- 🔄 Легка модифікація метаданих (тривалість, таймстемпи) без переписування всього файлу

Потребуєте допомоги з інтеграцією `Writer` у ваш конвеєр генерації сегментів або з реалізацією фільтрації боксів? Напишіть — покажу готовий код для вашого сценарію! 🚀✍️