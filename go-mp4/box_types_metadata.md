# 📦 iTunes Metadata Boxes: `ilst`, `data`, `keys` — Повне пояснення

Це код з бібліотеки `go-mp4` для роботи з **метаданими iTunes/QuickTime** у форматі MP4 (ISOBMFF). Ці бокси використовуються для зберігання інформації про медіа: назва, виконавець, альбом, обкладинка, рейтинг тощо.

---

## 🎯 Коротка відповідь

> **Це "контейнер" для метаданих у стилі iTunes** — дозволяє вбудовувати текстову інформацію, обкладинки, рейтинги та інші атрибути прямо у MP4-файл, сумісний з iTunes, QuickTime та багатьма плеєрами.

---

## 🗂️ Архітектура: Ієрархія метаданих

```
📦 moov (Movie)
└── 📦 udta (User Data)
    └── 📦 ilst (Item List) ← 🔹 Кореневий контейнер метаданих
        ├── 📦 ---- (Free-form metadata)
        │   ├── 📦 mean (namespace)
        │   ├── 📦 name (key name)
        │   └── 📦 data (value)
        ├── 📦 ©nam (Title)
        │   └── 📦 data (value)
        ├── 📦 ©ART (Artist)
        │   └── 📦 data (value)
        ├── 📦 covr (Cover Art)
        │   └── 📦 data (JPEG/PNG bytes)
        └── ... 30+ типів метаданих
```

---

## 🔑 Ключові типи боксів

### 🔹 `ilst` — Item List (кореневий контейнер)

```go
func BoxTypeIlst() BoxType { return StrToBoxType("ilst") }

type Ilst struct {
    Box  // 🔹 Пустий контейнер-обгортка
}
```

**Призначення**: Кореневий бокс для всіх метаданих у стилі iTunes. Не містить власних даних — тільки вкладені бокси метаданих.

---

### 🔹 `data` — Value Box (значення метаданого)

```go
type Data struct {
    Box
    DataType uint32 `mp4:"0,size=32"`  // 🔹 Тип даних: текст, число, зображення...
    DataLang uint32 `mp4:"1,size=32"`  // 🔹 Мова (рідко використовується)
    Data     []byte `mp4:"2,size=8"`   // 🔹 Сирі байти значення
}
```

#### 🔹 `DataType` — типи даних

| Значення | Константа | Опис | Приклад |
|----------|-----------|------|---------|
| `0` | `DataTypeBinary` | 🔹 Сирі байти | Обкладинка (JPEG), бінарні дані |
| `1` | `DataTypeStringUTF8` | 🔹 UTF-8 текст | "Назва пісні", "Виконавець" |
| `2` | `DataTypeStringUTF16` | UTF-16 текст | Застарілий формат |
| `3` | `DataTypeStringMac` | MacRoman текст | Застарілий формат |
| `14` | `DataTypeStringJPEG` | JPEG зображення | Обкладинка альбому |
| `21` | `DataTypeSignedIntBigEndian` | 🔹 Ціле число (знакове) | Трек №, диск №, рейтинг |
| `22` | `DataTypeFloat32BigEndian` | Float 32-bit | Рідко |
| `23` | `DataTypeFloat64BigEndian` | Float 64-bit | Рідко |

**🎯 Для вашого CCTV**: Зазвичай `DataTypeStringUTF8` (1) для тексту, `DataTypeBinary` (0) для обкладинок.

---

### 🔹 `IlstMetaContainer` — контейнер для конкретного метаданого

```go
type IlstMetaContainer struct {
    AnyTypeBox  // 🔹 Базовий тип для "будь-якого" боксу
}
```

**Призначення**: Універсальний контейнер для будь-якого типу метаданого (`©nam`, `©ART`, `covr` тощо). Сам по собі порожній — дані у вкладеному `data` боксі.

---

### 🔹 Список підтримуваних типів метаданих

```go
var ilstMetaBoxTypes = []BoxType{
    // 🔹 Стандартні iTunes-ключі
    StrToBoxType("aART"),  // Album Artist
    StrToBoxType("desc"),  // Description
    StrToBoxType("gnre"),  // Genre
    StrToBoxType("covr"),  // Cover Art (обкладинка)
    StrToBoxType("cprt"),  // Copyright
    // ... ще 25+ типів ...
    
    // 🔹 Ключі з префіксом © (0xA9)
    {0xA9, 'A', 'R', 'T'},  // ©ART = Artist
    {0xA9, 'a', 'l', 'b'},  // ©alb = Album
    {0xA9, 'n', 'a', 'm'},  // ©nam = Title/Name
    {0xA9, 'c', 'o', 'm'},  // ©com = Comment
    {0xA9, 'd', 'a', 'y'},  // ©day = Date/Year
    {0xA9, 'g', 'e', 'n'},  // ©gen = Genre (альтернатива)
    {0xA9, 'w', 'r', 't'},  // ©wrt = Composer
    // ... інші ...
}
```

> 💡 **Цікавий факт**: Ключі з `0xA9` (©) — це legacy-формат з QuickTime. Наприклад, `©nam` = "Title", `©ART` = "Artist".

---

### 🔹 `----` — Free-form metadata (користувацькі метадані)

```go
// У списку ilstMetaBoxTypes:
StrToBoxType("----"),  // Free-form metadata container
```

**Структура**:
```
📦 ---- (Free-form)
├── 📦 mean (namespace) ← "com.apple.iTunes"
├── 📦 name (key name)  ← "CUSTOM_FIELD"
└── 📦 data (value)     ← "значення"
```

**Призначення**: Дозволяє додавати **користувацькі метадані**, не передбачені стандартом.

**Приклад**:
```
mean: "com.example.cctv"
name: "camera_id"
data: "CAM-001"
```

---

### 🔹 `keys` — Metadata Keys (словник ключів)

```go
type Keys struct {
    FullBox    `mp4:"0,extend"`
    EntryCount int32 `mp4:"1,size=32"`
    Entries    []Key `mp4:"2,len=dynamic"`  // 🔹 Масив ключів
}

type Key struct {
    BaseCustomFieldObject
    KeySize      int32  `mp4:"0,size=32"`  // 🔹 Загальний розмір запису
    KeyNamespace []byte `mp4:"1,size=8,len=4"`  // 🔹 Простір імен (4 байти)
    KeyValue     []byte `mp4:"2,size=8,len=dynamic"`  // 🔹 Назва ключа
}
```

**Де зустрічається**: Зазвичай на рівні `moov` або `udta`, окремо від `ilst`.

**Призначення**: Словник користувацьких ключів для `----` боксів. Дозволяє визначити власні метадані.

**Приклад `Key`**:
```
KeySize: 20
KeyNamespace: [0x63, 0x6F, 0x6D, 0x2E]  // "com." (4 байти)
KeyValue:     []byte("example.cctv.camera_id")  // решта назви
```

---

## 🔍 Контекстна логіка: `isUnderIlstMeta`, `isIlstMetaContainer`

Бібліотека використовує **контекст** для визначення, як парсити бокси:

```go
func isIlstMetaContainer(ctx Context) bool {
    // 🔹 Увімкнути, тільки якщо:
    // • Ми всередині ilst (UnderIlst=true)
    // • Але НЕ всередині вкладеного мета-боксу (UnderIlstMeta=false)
    return ctx.UnderIlst && !ctx.UnderIlstMeta
}

func isUnderIlstMeta(ctx Context) bool {
    // 🔹 Увімкнути, тільки якщо ми всередині мета-боксу
    return ctx.UnderIlstMeta
}
```

**🎯 Навіщо це?** Один і той самий тип боксу (`data`) може мати **різну структуру** залежно від контексту:
- Всередині `ilst → ©nam → data` → парсити як текст/число
- Всередині `ilst → ---- → mean → data` → парсити як namespace-рядок

---

## 🔍 `StringifyField` — людино-читабельний вивід

```go
func (data *Data) StringifyField(name string, indent string, depth int, ctx Context) (string, bool) {
    switch name {
    case "DataType":
        switch data.DataType {
        case DataTypeStringUTF8:
            return "UTF8", true  // 🔹 Замість "1" виводимо "UTF8"
        case DataTypeBinary:
            return "BINARY", true
        // ... інші типи ...
        }
    case "Data":
        switch data.DataType {
        case DataTypeStringUTF8:
            // 🔹 Для тексту: екранувати недрюковані символи та додати лапки
            return fmt.Sprintf("\"%s\"", util.EscapeUnprintables(string(data.Data))), true
        }
    }
    return "", false  // Для інших полів — стандартний вивід
}
```

**Результат у логах**:
```
📦 data: DataType=UTF8 Data="Привіт, світ!"
📦 data: DataType=BINARY Data=[0xFF 0xD8 0xFF 0xE0 ...]  // JPEG обкладинка
📦 data: DataType=INT Data=42  // Рейтинг, трек №
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Додавання метаданих до fMP4-сегмента

```go
import "github.com/abema/go-mp4"

func addMetadataToSegment(filePath string, title, artist, cameraID string) error {
    f, err := os.OpenFile(filePath, os.O_RDWR, 0644)
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 1. Знайти або створити ilst бокс
    var ilstOffset int64
    mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeIlst() {
            ilstOffset = h.BoxInfo.Offset
        }
        return nil, nil
    })
    
    // 🔹 2. Додати метадані (спрощено — у реальності потрібно оновлювати офсети)
    // Додати ©nam (Title)
    addIlstMeta(f, mp4.StrToBoxType("©nam"), title, mp4.DataTypeStringUTF8)
    
    // Додати ©ART (Artist)
    addIlstMeta(f, mp4.StrToBoxType("©ART"), artist, mp4.DataTypeStringUTF8)
    
    // Додати користувацьке метадане через ----
    addFreeFormMeta(f, "com.example.cctv", "camera_id", cameraID)
    
    return nil
}

// Допоміжна функція: додати стандартне метадане
func addIlstMeta(f *os.File, boxType mp4.BoxType, value string, dataType uint32) error {
    // Створити контейнер (напр. ©nam)
    container := &mp4.IlstMetaContainer{}
    
    // Створити data бокс
    dataBox := &mp4.Data{
        DataType: dataType,
        DataLang: 0,  // за замовчуванням
        Data:     []byte(value),
    }
    
    // Записати у файл (спрощено)
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: boxType})
    container.Marshal(f)
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeData()})
    dataBox.Marshal(f)
    
    return nil
}

// Допоміжна функція: додати free-form метадане
func addFreeFormMeta(f *os.File, namespace, name, value string) error {
    // Створити ---- контейнер
    freeForm := &mp4.IlstMetaContainer{}
    
    // Додати mean (namespace)
    mean := &mp4.StringData{Data: []byte(namespace)}
    
    // Додати name (key name)
    keyName := &mp4.StringData{Data: []byte(name)}
    
    // Додати data (value)
    dataBox := &mp4.Data{
        DataType: mp4.DataTypeStringUTF8,
        Data:     []byte(value),
    }
    
    // Записати у файл
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.StrToBoxType("----")})
    freeForm.Marshal(f)
    
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.StrToBoxType("mean")})
    mean.Marshal(f)
    
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.StrToBoxType("name")})
    keyName.Marshal(f)
    
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeData()})
    dataBox.Marshal(f)
    
    return nil
}
```

---

### 🔹 Приклад 2: Читання метаданих з існуючого сегмента

```go
func extractMetadata(filePath string) (map[string]string, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    result := make(map[string]string)
    
    _, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Шукаємо бокси всередині ilst
        if h.BoxInfo.Context.UnderIlst && !h.BoxInfo.Context.UnderIlstMeta {
            boxType := h.BoxInfo.Type.String()
            
            // 🔹 Шукаємо вкладений data бокс
            mp4.ReadBoxStructure(h.Reader(), func(inner *mp4.ReadHandle) (interface{}, error) {
                if inner.BoxInfo.Type == mp4.BoxTypeData() {
                    dataBox := &mp4.Data{}
                    if _, err := inner.ReadPayload(dataBox); err != nil {
                        return nil, err
                    }
                    
                    // 🔹 Інтерпретуємо дані залежно від типу
                    switch dataBox.DataType {
                    case mp4.DataTypeStringUTF8:
                        result[boxType] = string(dataBox.Data)
                    case mp4.DataTypeSignedIntBigEndian:
                        // Парсинг big-endian int32
                        if len(dataBox.Data) >= 4 {
                            val := int32(dataBox.Data[0])<<24 | 
                                   int32(dataBox.Data[1])<<16 | 
                                   int32(dataBox.Data[2])<<8 | 
                                   int32(dataBox.Data[3])
                            result[boxType] = fmt.Sprintf("%d", val)
                        }
                    case mp4.DataTypeBinary:
                        // Для обкладинок: зберігаємо розмір або хеш
                        result[boxType] = fmt.Sprintf("BINARY(%d bytes)", len(dataBox.Data))
                    }
                }
                return nil, nil
            })
        }
        return nil, nil
    })
    
    return result, err
}
```

---

### 🔹 Приклад 3: Додавання обкладинки (Cover Art)

```go
func addCoverArt(filePath string, jpegData []byte) error {
    f, err := os.OpenFile(filePath, os.O_RDWR, 0644)
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 Створити data бокс для обкладинки
    dataBox := &mp4.Data{
        DataType: mp4.DataTypeBinary,  // або DataTypeStringJPEG (14)
        DataLang: 0,
        Data:     jpegData,  // сирі байти JPEG/PNG
    }
    
    // 🔹 Записати у covr бокс
    mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.StrToBoxType("covr")})
    // (спрощено: без вкладеності)
    dataBox.Marshal(f)
    
    return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `DataType` | Текст відображається як "кракозябри" або числа як текст | Завжди використовуйте `DataTypeStringUTF8` (1) для тексту, `DataTypeSignedIntBigEndian` (21) для чисел |
| Ігнорування контексту `UnderIlstMeta` | `data` бокс парситься неправильно → помилка | Завжди перевіряйте `ctx.UnderIlstMeta` перед доступом до полів |
| Неправильне кодування тексту | Кирилиця/емодзі → "кракозябри" | Використовуйте UTF-8 без BOM, перевіряйте кодування при записі |
| Переповнення `KeySize` у `keys` | Зсув даних → пошкодження файлу | Завжди встановлюйте `KeySize = 8 + len(KeyValue)` (8 байт заголовка + довжина значення) |
| Відсутній `data` бокс у метаданому | Метадане ігнорується плеєром | Завжди додавайте `Data` бокс всередині контейнера метаданого |

---

## 📋 Чекліст для вашого проекту

```
[ ] При додаванні метаданих:
    • Використовуйте стандартні ключі (`©nam`, `©ART`, `covr`) для максимальної сумісності
    • Для користувацьких полів: використовуйте `----` + `mean`/`name`/`data`
    • Завжди встановлюйте правильний `DataType`: 1 для тексту, 0 для бінарних даних

[ ] Для читання метаданих:
    • Шукайте бокси всередині `ilst` з `ctx.UnderIlst=true`
    • Інтерпретуйте `Data` залежно від `DataType`
    • Логувайте невідомі типи: log.Printf("⚠️  Unknown DataType: %d", dataType)

[ ] Для обкладинок:
    • Використовуйте `DataTypeBinary` (0) або `DataTypeStringJPEG` (14)
    • Переконайтеся, що дані — валідний JPEG/PNG
    • Обмежуйте розмір обкладинки (<100KB) для економії місця

[ ] Для дебагу:
    • Логуйте тип метаданого та значення: 
      log.Printf("📝 %s: %s", boxType, Stringify(dataBox, ctx))
    • Перевіряйте кодування тексту: if !utf8.Valid(data) { ... }
    • Використовуйте `Stringify()` для людського виводу

[ ] Для тестування:
    • Перевіряйте відтворення метаданих у різних плеєрах: iTunes, VLC, QuickTime
    • Тестуйте з різними кодуваннями: кирилиця, арабська, емодзі
    • Перевіряйте коректність обкладинок: чи відображаються у плеєрі
```

---

## 🎯 Інтеграція у ваш CCTV HLS Processor

```
📡 Ваш потік обробки з метаданими:
1. Приймаєте відео-потік + метадані (окремим каналом або вбудовані)
   │
   ▼
2. Формуєте iTunes-сумісні метадані:
   • Назва передачі → `©nam`
   • Канал/джерело → `©ART` або користувацьке `----`
   • Обкладинка каналу → `covr` (JPEG)
   • ID камери → `----` з `mean="com.example.cctv"`
   │
   ▼
3. Вбудовуєте у fMP4-сегмент:
   • Додаєте `ilst` бокс у `udta` (якщо ще немає)
   • Для кожного метаданого: контейнер + `data` бокс
   │
   ▼
4. Клієнт (iTunes, VLC, веб-плеєр) відображає метадані ✅
```

---

## ❓ Часті питання

**Q: Чи підтримують вебові плеєри iTunes-метадані?**  
A: ⚠️ Обмежено.  
• ✅ VLC, QuickTime, iTunes — повна підтримка  
• ⚠️ hls.js, video.js — можуть читати `ilst`, але не завжди відображають  
• ❌ Старі браузери — ігнорують  
🎯 **Рекомендація**: Використовуйте метадані для офлайн-плеєрів та аналітики, не покладайтеся на них для критичної функціональності у вебі.

**Q: Як додати субтитри через `ilst`?**  
A: `ilst` не призначений для субтитрів! Для текстових доріжок використовуйте:
• `wvtt`/`vttc` бокси для WebVTT (стандарт HLS)
• `stpp` для TTML/IMSC субтитрів

**Q: Чи можу я змінити метадані у вже записаному файлі?**  
A: Так, але обережно:
1. Знайдіть офсет `ilst` боксу
2. Перезапишіть `data` бокси з новими значеннями
3. Оновіть розміри боксів, якщо змінилася довжина
⚠️ **Ризик**: Неправильне оновлення розмірів може пошкодити файл. Краще створювати новий файл з оновленими метаданими.

**Q: Як перевірити, чи коректні мої метадані?**  
```bash
# ffprobe покаже метадані:
ffprobe -show_format -print_format json segment.m4s | grep -A 20 "tags"

# Або спеціалізовані інструменти:
# • AtomicParsley: https://github.com/wez/AtomicParsley
# • Mp4box (GPAC): mp4box -info segment.m4s
```

---

## 🎯 Висновок

> **Цей код — ваш міст між iTunes-метаданими та бінарним форматом MP4**.  
> Він дозволяє:
> • ✅ Вбудовувати текстову інформацію, обкладинки, рейтинги у MP4
> • ✅ Підтримувати як стандартні ключі (`©nam`, `©ART`), так і користувацькі (`----`)
> • ✅ Коректно обробляти різні типи даних: текст, числа, бінарні дані
> • ✅ Сумісність з iTunes, QuickTime, VLC та багатьма іншими плеєрами

Для вашого **CCTV HLS Processor** це означає:
- 📺 Клієнти бачать назви передач, інформацію про канал, обкладинки
- 🗂️ Легка інтеграція з медіа-бібліотеками (iTunes, Plex, Kodi)
- 🔧 Гнучкість: додавання користувацьких полів для аналітики/керування
- ♿ Підтримка описів для слабозорих через `desc` метадане

Потребуєте допомоги з інтеграцією iTunes-метаданих у ваш конвеєр або з додаванням обкладинок? Напишіть — покажу готовий код! 🚀📦