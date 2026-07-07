# 🧪 Тести `read.go`: Перевірка ітеративного читання структури боксів у MP4

Це **комплексний тест-сьют** для модуля `read.go` бібліотеки `go-mp4`, який перевіряє коректність роботи **ітеративного обходу та обробки ієрархічної структури боксів** у файлах формату MP4/ISOBMFF.

---

## 🎯 Коротка відповідь

> **Ці тести гарантують, що функція `ReadBoxStructure()` коректно обходить дерево боксів, викликає користувацький обробник для кожного боксу, правильно обробляє контекст (QuickTime, iTunes metadata), та безпечно працює з крайніми випадками** — критично для надійного парсингу будь-яких MP4-файлів.

---

## 📋 Огляд тестових функцій

### 🔹 `TestReadBoxStructure` — головний інтеграційний тест для стандартного MP4

```go
func TestReadBoxStructure(t *testing.T) {
    f, err := os.Open("./testdata/sample.mp4")  // 🔹 Тестовий файл
    require.NoError(t, err)
    defer f.Close()

    var n int  // 🔹 Лічильник викликів обробника
    _, err = ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        n++  // 🔹 Інкремент лічильника для кожного боксу
        
        switch n {
        case 57: // 🔹 unsupported box type "loci"
            require.False(t, h.BoxInfo.IsSupportedType())  // ✅ Не підтримується
            buf := bytes.NewBuffer(nil)
            n, err := h.ReadData(buf)  // 🔹 Читаємо сирі дані
            require.NoError(t, err)
            require.Equal(t, h.BoxInfo.Size-h.BoxInfo.HeaderSize, n)  // ✅ Прочитано весь payload
            assert.Len(t, buf.Bytes(), int(n))  // ✅ Буфер заповнено
            
        case 41: // 🔹 stbl бокс (контейнер таблиць семплів)
            require.True(t, h.BoxInfo.IsSupportedType())  // ✅ Підтримується
            require.Equal(t, BoxTypeStbl(), h.BoxInfo.Type)  // ✅ Тип = "stbl"
            infos, err := h.Expand()  // 🔹 Рекурсивно обробити дітей
            require.NoError(t, err)
            // ✅ Очікуємо 7 дітей: stsd, stts, стсс, ctts, stsc, stsz, stco
            assert.Equal(t, []interface{}{"stsd", "stts", nil, nil, "stco", nil, nil}, infos)
            
        case 42: // 🔹 stsd бокс (опис кодеків)
            require.True(t, h.BoxInfo.IsSupportedType())
            require.Equal(t, BoxTypeStsd(), h.BoxInfo.Type)
            box, n, err := h.ReadPayload()  // 🔹 Парсимо вміст stsd
            require.NoError(t, err)
            require.Equal(t, uint64(8), n)  // ✅ Прочитано 8 байт заголовка
            assert.Equal(t, &Stsd{EntryCount: 1}, box)  // ✅ 1 запис у таблиці
            _, err = h.Expand()  // 🔹 Обробити вкладені бокси (avc1/mp4a)
            require.NoError(t, err)
            return "stsd", nil  // 🔹 Повертаємо результат для збору у infos
            
        case 45: // 🔹 stts бокс (таймінги декодування)
            require.True(t, h.BoxInfo.IsSupportedType())
            require.Equal(t, BoxTypeStts(), h.BoxInfo.Type)
            _, err = h.Expand()  // 🔹 stts не має дітей → повертає порожній список
            require.NoError(t, err)
            return "stts", nil
            
        case 48: // 🔹 stco бокс (офсети чанків, 32-біт)
            require.True(t, h.BoxInfo.IsSupportedType())
            require.Equal(t, BoxTypeStco(), h.BoxInfo.Type)
            _, err = h.Expand()
            require.NoError(t, err)
            return "stco", nil
            
        case 56: // 🔹 data бокс всередині iTunes metadata (©too)
            require.True(t, h.BoxInfo.IsSupportedType())
            require.Equal(t, BoxTypeData(), h.BoxInfo.Type)
            box, n, err := h.ReadPayload()  // 🔹 Парсимо вміст data
            require.NoError(t, err)
            require.Equal(t, uint64(21), n)  // ✅ 21 байт даних
            // ✅ Очікуємо: UTF8 рядок "Lavf58.29.100" (FFmpeg версія)
            assert.Equal(t, &Data{DataType: DataTypeStringUTF8, DataLang: 0, Data: []byte("Lavf58.29.100")}, box)
            _, err = h.Expand()  // 🔹 data не має дітей
            require.NoError(t, err)
            return "stco", nil  // 🔹 Повертаємо результат (помилка у тесті: має бути "data")
            
        default: // 🔹 Всі інші бокси
            require.True(t, h.BoxInfo.IsSupportedType())  // ✅ Більшість підтримуються
            _, err = h.Expand()  // 🔹 Рекурсивно обробити дітей
            require.NoError(t, err)
        }
        return nil, nil  // 🔹 За замовчуванням не повертаємо результат
    })
    require.NoError(t, err)
    assert.Equal(t, 57, n)  // ✅ Загалом 57 боксів у файлі
}
```

**📊 Що тестується:**

| № виклику | Тип боксу | Дія | Очікуваний результат | Чому це важливо |
|-----------|-----------|-----|---------------------|----------------|
| 41 | `stbl` | `h.Expand()` | `["stsd","stts",nil,nil,"stco",nil,nil]` | ✅ Перевірка рекурсивної обробки дітей |
| 42 | `stsd` | `h.ReadPayload()` + `h.Expand()` | `&Stsd{EntryCount:1}` + обробка avc1/mp4a | ✅ Парсинг заголовка таблиці семплів |
| 45 | `stts` | `h.Expand()` | порожній список (немає дітей) | ✅ Обробка боксів без вкладених структур |
| 48 | `stco` | `h.Expand()` | порожній список | ✅ Обробка 32-біт офсетів чанків |
| 56 | `data` (в ilst) | `h.ReadPayload()` | `&Data{DataType:UTF8, Data:"Lavf58.29.100"}` | ✅ Парсинг iTunes metadata з правильним контекстом |
| 57 | `loci` (unsupported) | `h.ReadData()` | сирі байти без парсингу | ✅ Безпечна обробка невідомих типів боксів |

**🔑 Ключова логіка `ReadHandle`:**
```
🔹 h.ReadPayload() → парсить вміст боксу у структуру через UnmarshalAny()
🔹 h.ReadData(w) → копіює сирі дані боксу у io.Writer без парсингу
🔹 h.Expand() → рекурсивно обробляє вкладені бокси, повертає []interface{} з результатами
🔹 h.Path → шлях до поточного боксу в ієрархії (напр. [moov,trak,mdia,minf,stbl])
🔹 h.BoxInfo → метадані: тип, розмір, офсет, контекст
```

---

### 🔹 `TestReadBoxStructureQT` — тест для QuickTime-сумісних файлів

```go
func TestReadBoxStructureQT(t *testing.T) {
    f, err := os.Open("./testdata/sample_qt.mp4")  // 🔹 QuickTime-файл
    require.NoError(t, err)
    defer f.Close()

    var n int
    _, err = ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        n++
        switch n {
        case 51, 44: // 🔹 unsupported box types: (c)enc, 0x00000000
            require.False(t, h.BoxInfo.IsSupportedType())  // ✅ Не підтримуються
            buf := bytes.NewBuffer(nil)
            n, err := h.ReadData(buf)  // 🔹 Читаємо сирі дані
            require.NoError(t, err)
            require.Equal(t, h.BoxInfo.Size-h.BoxInfo.HeaderSize, n)
            assert.Len(t, buf.Bytes(), int(n))
            
        case 39: // 🔹 mp4a бокс з QuickTime-розширенням
            require.True(t, h.BoxInfo.IsSupportedType())
            require.Equal(t, StrToBoxType("mp4a"), h.BoxInfo.Type)
            box, n, err := h.ReadPayload()  // 🔹 Парсимо AudioSampleEntry
            require.NoError(t, err)
            require.Equal(t, uint64(44), n)  // ✅ 44 байти заголовка
            // ✅ Перевірка QuickTime-specific поля: 16 байт додаткових даних
            assert.Equal(t, []byte{0x0,0x0,0x4,0x0,0x0,0x0,0x0,0x0,0x0,0x0,0x0,0x0,0x0,0x0,0x0,0x2}, 
                box.(*AudioSampleEntry).QuickTimeData)
            _, err = h.Expand()  // 🔹 Обробити вкладені бокси (wave, esds...)
            require.NoError(t, err)
            
        case 42: // 🔹 вкладений mp4a бокс всередині wave
            require.True(t, h.BoxInfo.IsSupportedType())
            require.Equal(t, StrToBoxType("mp4a"), h.BoxInfo.Type)
            box, n, err := h.ReadPayload()
            require.NoError(t, err)
            require.Equal(t, uint64(4), n)  // ✅ Тільки 4 байти заголовка
            assert.Equal(t, []byte{0x0,0x0,0x0,0x0}, box.(*AudioSampleEntry).QuickTimeData)
            _, err = h.Expand()
            require.NoError(t, err)
            
        case 54: // 🔹 keys бокс для iTunes metadata
            require.True(t, h.BoxInfo.IsSupportedType())
            require.Equal(t, StrToBoxType("keys"), h.BoxInfo.Type)
            box, n, err := h.ReadPayload()
            require.NoError(t, err)
            require.Equal(t, uint64(35), n)
            assert.Equal(t, int32(1), box.(*Keys).EntryCount)  // ✅ 1 ключ у словнику
            _, err = h.Expand()
            require.NoError(t, err)
            
        case 56: // 🔹 нумерований item у ilst (ID=1)
            require.True(t, h.BoxInfo.IsSupportedType())
            _, err = h.Expand()  // 🔹 Обробити вкладені бокси
            // ✅ Перевірка: тип боксу — це числовий ID, а не 4-символьний код
            require.Equal(t, Uint32ToBoxType(1), h.BoxInfo.Type)
            require.NoError(t, err)
            
        default: // 🔹 Всі інші бокси
            require.True(t, h.BoxInfo.IsSupportedType())
            _, err = h.Expand()
            require.NoError(t, err)
        }
        return nil, nil
    })
    require.NoError(t, err)
    assert.Equal(t, 56, n)  // ✅ Загалом 56 боксів у файлі
}
```

**🎯 Що тестується:**

| № виклику | Тип боксу | Особливість | Очікуваний результат | Чому це важливо |
|-----------|-----------|-------------|---------------------|----------------|
| 39 | `mp4a` | QuickTime-розширення | `QuickTimeData=[16 байт]` | ✅ Обробка нестандартних полів у QuickTime |
| 42 | `mp4a` (в `wave`) | Вкладений бокс | `QuickTimeData=[4 байти]` | ✅ Рекурсивна обробка вкладених структур |
| 54 | `keys` | iTunes metadata словник | `EntryCount=1` | ✅ Збереження `EntryCount` у контексті для подальшого парсингу |
| 56 | `0x00000001` | Нумерований ilst item | `Type=Uint32ToBoxType(1)` | ✅ Спеціальна логіка для QuickTime-style metadata |

**🔑 Ключова логіка контексту для QuickTime/iTunes:**
```
🔹 При читанні ftyp:
   • Якщо BrandQT() у CompatibleBrands → ctx.IsQuickTimeCompatible = true

🔹 При читанні keys:
   • Парсимо EntryCount → зберігаємо у bi.QuickTimeKeysMetaEntryCount
   • Поширюємо у контекст для подальшої обробки ilst item

🔹 При читанні ilst item:
   • Якщо тип боксу — числовий ID (1..EntryCount) → парсимо як Item{}
   • Інакше → стандартна логіка для ©nam, covr тощо
```

---

### 🔹 `TestReadBoxStructureZeroSize` — тест крайнього випадку: бокс з розміром 1

```go
func TestReadBoxStructureZeroSize(t *testing.T) {
    // 🔹 Штучний вхід: бокс з розміром 1 (менше мінімального заголовка 8 байт)
    b := []byte("\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01")
    
    // 🔹 Очікуємо помилку, а не нескінченний цикл або краш
    _, err := ReadBoxStructure(bytes.NewReader(b), func(h *ReadHandle) (interface{}, error) {
        return nil, nil
    })
    require.Error(t, err)  // ✅ Помилка очікувана
}
```

**🎯 Призначення**: Перевірити, що бібліотека **безпечно обробляє пошкоджені або спеціально сформовані файли** без зависання або крашу.

**🔑 Ключова перевірка**: У `readBoxStructure()` є захист:
```go
if !isRoot && bi.Size > totalSize {
    return nil, fmt.Errorf("too large box size: type=%s, size=%d, actualBufSize=%d", ...)
}
```
А також перевірка мінімального розміру заголовка (`SmallHeaderSize = 8`).

---

### 🔹 `FuzzReadBoxStructure` — fuzz-тест для пошуку вразливостей

```go
func FuzzReadBoxStructure(f *testing.F) {
    // 🔹 Додаємо реальний приклад: AC-3 трек з Apple HLS
    f.Add([]byte{
        0x00, 0x00, 0x00, 0x20, 0x66, 0x74, 0x79, 0x70,  // "ftyp" бокс
        0x6d, 0x70, 0x34, 0x32, ...  // "mp42" бренд
        // ... ще 200+ байт валідної структури моов/трак/медіа ...
    })

    f.Fuzz(func(t *testing.T, b []byte) {
        // 🔹 Для кожного випадкового входу:
        ReadBoxStructure(bytes.NewReader(b), func(h *ReadHandle) (interface{}, error) {
            if h.BoxInfo.IsSupportedType() {
                // 🔹 Парсимо тільки підтримувані типи
                _, _, err := h.ReadPayload()
                if err != nil {
                    return nil, err  // ✅ Повертаємо помилку, не крашимо
                }
                return h.Expand()  // 🔹 Рекурсивно обробляємо дітей
            }
            return nil, nil  // 🔹 Ігноруємо непідтримувані типи
        })
        // 🔹 Якщо функція завершилася без panic → тест пройдено
    })
}
```

**🎯 Призначення**: Автоматично генерувати тисячі випадкових вхідних даних для пошуку:
- ❌ Пам'яті витоки (memory leaks)
- ❌ Переповнення буфера (buffer overflows)
- ❌ Нескінченні цикли (infinite loops)
- ❌ Panic при неочікуваних вхідних даних

**🔑 Ключова властивість**: Функція має бути **стійкою до будь-яких вхідних даних** — повертати помилку, а не крашити.

---

## 🔍 Як це працює разом: Повний потік `TestReadBoxStructure`

```
🔹 Вхід: файл "sample.mp4" (стандартний ISO MP4)
│
▼
🔹 ReadBoxStructure(r, handler):
   │
   ├── 🔹 Цикл 1-40: Обхід загальної структури (ftyp, free, mdat, moov, mvhd, trak...)
   │   • Для кожного боксу: handler(h) → h.Expand() → рекурсія
   │   • Більшість повертають nil (не збирають результати)
   │
   ├── 🔹 Виклик 41: stbl бокс
   │   • handler: h.Expand() → рекурсивний обхід дітей stbl
   │   • Діти: stsd(42), stts(45), стсс(43), ctts(44), stsc(46), stsz(47), stco(48)
   │   • Повертає: ["stsd","stts",nil,nil,"stco",nil,nil] ← nil для боксів без return
   │
   ├── 🔹 Виклик 42: stsd бокс
   │   • handler: h.ReadPayload() → парсинг заголовка → &Stsd{EntryCount:1}
   │   • h.Expand() → обробка вкладених avc1/mp4a
   │   • return "stsd" → додається у список результатів
   │
   ├── 🔹 Виклик 56: data бокс всередині ilst → ©too
   │   • Контекст: ctx.UnderIlst=true, ctx.UnderIlstMeta=true
   │   • h.ReadPayload() → парсинг Data{DataType:UTF8, Data:"Lavf58.29.100"}
   │   • return "stco" ← помилка у тесті (має бути "data"), але не впливає на результат
   │
   ├── 🔹 Виклик 57: loci бокс (непідтримуваний тип)
   │   • handler: !h.BoxInfo.IsSupportedType() → true
   │   • h.ReadData(buf) → копіювання сирих байт без парсингу
   │   • return nil → ігноруємо непідтримуваний бокс
   │
   ▼
🔹 Вихід: []interface{} (порожній, бо більшість handler повертають nil)
   + ✅ 57 викликів handler + ✅ коректна обробка всіх боксів
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Збір всіх trun боксів для аналізу таймстемпів

```go
func collectTrunBoxes(filePath string) ([]*mp4.Trun, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    var truns []*mp4.Trun
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeTrun() {
            // 🔹 Парсимо вміст trun
            box, _, err := h.ReadPayload()
            if err != nil { return nil, err }
            
            if trun, ok := box.(*mp4.Trun); ok {
                truns = append(truns, trun)  // 🔹 Зберігаємо результат
            }
        }
        // 🔹 Рекурсивно обробити вкладені бокси
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(f, handler)
    return truns, err
}
```

---

### 🔹 Приклад 2: Експорт сирих даних конкретного боксу

```go
func extractBoxRawData(filePath string, targetPath mp4.BoxPath) ([]byte, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    var result []byte
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Перевірка: чи співпадає поточний шлях з цільовим?
        if h.Path.compareWith(targetPath) == (false, true) {  // ✅ точне співпадіння
            var buf bytes.Buffer
            // 🔹 Копіюємо сирі дані боксу у буфер
            _, err := h.ReadData(&buf)
            if err != nil { return nil, err }
            result = buf.Bytes()
        }
        // 🔹 Продовжуємо обхід для пошуку інших екземплярів
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(f, handler)
    return result, err
}

// 🔹 Використання: експорт даних аватарки з iTunes metadata
avatarData, err := extractBoxRawData("video.mp4", 
    mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeUdta(), mp4.BoxTypeIlst(), 
                mp4.StrToBoxType("covr"), mp4.BoxTypeData()})
```

---

### 🔹 Приклад 3: Аналіз структури файлу для дебагу

```go
func printBoxTree(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    handler := func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Відступ залежно від глибини
        indent := strings.Repeat("  ", len(h.Path)-1)
        
        // 🔹 Форматування інформації про бокс
        info := fmt.Sprintf("%s📦 %s @ offset=%d, size=%d", 
            indent, 
            h.BoxInfo.Type.String(), 
            h.BoxInfo.Offset, 
            h.BoxInfo.Size)
        
        // 🔹 Додаткова інформація для відомих типів
        if h.BoxInfo.Type == mp4.BoxTypeTrun() {
            box, _, _ := h.ReadPayload()
            if trun, ok := box.(*mp4.Trun); ok {
                info += fmt.Sprintf(" (samples=%d)", trun.SampleCount)
            }
        }
        
        fmt.Println(info)
        
        // 🔹 Рекурсивно обробити дітей
        return h.Expand()
    }
    
    _, err = mp4.ReadBoxStructure(f, handler)
    return err
}

// 🔹 Приклад виводу:
// 📦 ftyp @ offset=0, size=24
// 📦 moov @ offset=24, size=10240
//   📦 trak @ offset=100, size=2048
//     📦 tkhd @ offset=108, size=92
//     📦 mdia @ offset=200, size=1848
//       📦 mdhd @ offset=208, size=32
//       📦 hdlr @ offset=240, size=44
//       📦 minf @ offset=284, size=1764
//         📦 stbl @ offset=300, size=1748
//           📦 stts @ offset=308, size=28
//           📦 stss @ offset=336, size=20
//           📦 trun @ offset=356, size=120 (samples=10)  ← додаткова інформація!
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Забути викликати `h.Expand()` | Вкладені бокси не обробляються → пропуск даних | Завжди викликайте `return h.Expand()` у кінці обробника, якщо потрібна рекурсія |
| Неправильне використання `ReadPayload()` | Подвійне читання → зсув позиції → помилки парсингу | Викликайте `ReadPayload()` тільки один раз на бокс, або використовуйте `ReadData()` для сирих даних |
| Ігнорування контексту (`UnderIlst`, `IsQuickTimeCompatible`) | Неправильний парсинг metadata → "кракозябри" в текстах | Завжди передавайте `bi.Context` у `UnmarshalAny()` та перевіряйте контекстні прапорці |
| Неправильна обробка `totalSize` у вкладених циклах | Переповнення буфера → помилка "too large box size" | Передавайте `totalSize` у рекурсивні виклики та оновлюйте його після кожного боксу |
| Забути `SeekToEnd()` після обробки | Наступний бокс читається з неправильної позиції → пошкодження даних | Завжди викликайте `bi.SeekToEnd(r)` у кінці `readBoxStructureFromInternal` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні ReadHandler:
    • Завжди перевіряйте h.BoxInfo.Type для фільтрації потрібних боксів
    • Викликайте h.ReadPayload() тільки для боксів, вміст яких потрібен
    • Викликайте h.Expand() для рекурсивної обробки вкладених структур
    • Повертайте val/interface{} для збору результатів, якщо потрібно

[ ] Для оптимізації продуктивності:
    • Уникайте парсингу великих боксів (mdat) через ReadPayload() — використовуйте ReadData() або пропускайте
    • Фільтруйте бокси за типом на ранніх етапах, щоб уникнути зайвих викликів
    • Використовуйте BoxPath.compareWith() для швидкого пошуку за шляхом

[ ] Для роботи з контекстом:
    • Передавайте bi.Context у UnmarshalAny() для коректної обробки версій/прапорців
    • Перевіряйте ctx.UnderIlst, ctx.IsQuickTimeCompatible для спеціальної логіки
    • Оновлюйте контекст при обробці ftyp/keys для iTunes metadata

[ ] Для дебагу:
    • Логуйте шлях: log.Printf("🔍 Path: %v", h.Path)
    • Виводьте метадані: log.Printf("📦 %s @ %d+%d", h.BoxInfo.Type, h.BoxInfo.Offset, h.BoxInfo.Size)
    • Використовуйте printBoxTree() для візуалізації структури файлу

[ ] Для тестування:
    • Створюйте тестові файли з відомою структурою боксів
    • Перевіряйте, що handler викликається для очікуваних типів/шляхів
    • Тестуйте edge cases: порожні файли, пошкоджені заголовки, вкладеність >10 рівнів
```

---

## 🎯 Висновок

> **Ці тести — ваш "золотий стандарт" для надійного обходу структури MP4-файлів**.  
> Вони гарантують:
> • ✅ Коректну рекурсивну обробку ієрархії боксів з користувацькою логікою
> • ✅ Безпечну роботу з непідтримуваними типами боксів через ReadData()
> • ✅ Правильну обробку контексту для QuickTime та iTunes metadata
> • ✅ Стійкість до крайніх випадків: пошкоджені файли, нульові розміри, fuzz-входи
> • ✅ Гнучкість: збір результатів, експорт даних, дебаг-вивід через єдиний інтерфейс

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Швидкий аналіз структури вхідних fMP4-сегментів без повного парсингу
- 🔍 Точний пошук ключових боксів (trun, stts, avcC) для синхронізації та валідації
- 🔄 Гнучкість: легко додавати нові типи обробки без зміни ядра бібліотеки
- 🛡️ Надійність: коректна обробка краєвих випадків, пошкоджених файлів, різних стандартів

Потребуєте допомоги зі створенням ReadHandler для вашого сценарію (пошук таймстемпів, експорт даних, аналіз структури)? Напишіть — покажу готовий код! 🚀🔍