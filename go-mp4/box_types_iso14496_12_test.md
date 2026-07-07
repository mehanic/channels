# 🧪 Файл тестів `TestBoxTypesISO14496_12`: Повний огляд

Це **інтеграційний тест-сьют** для бібліотеки `go-mp4`, який перевіряє коректність серіалізації/десеріалізації **понад 100 типів боксів** стандарту ISO/IEC 14496-12 (MP4/ISOBMFF).

---

## 🎯 Коротка відповідь

> **Це "золотий стандарт" тестування бібліотеки**: кожен тест-кейс перевіряє, що структура Go ↔ байти ↔ структура Go працює ідемпотентно для кожного типу боксу.

---

## 📋 Структура тесту

### 🔹 Основна функція: `TestBoxTypesISO14496_12`

```go
func TestBoxTypesISO14496_12(t *testing.T) {
    // 1. Масив тест-кейсів (понад 100 кейсів!)
    testCases := []struct {
        name string           // Назва тесту (напр. "trun: version=0 flag=0x101")
        src  IImmutableBox    // Вихідна структура (для Marshal)
        dst  IBox             // Порожня структура (для Unmarshal)
        bin  []byte           // Очікувані байти (еталон)
        str  string           // Очікуваний рядок для Stringify()
        ctx  Context          // Контекст парсингу
    }{ ... }

    // 2. Запуск кожного кейсу
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // Тестуємо 4 операції:
            // 1. Marshal: структура → байти
            // 2. Unmarshal: байти → структура
            // 3. UnmarshalAny: динамічний парсинг за типом
            // 4. Stringify: структура → людино-читабельний рядок
        })
    }
}
```

---

## 🔍 Детальний розбір одного тест-кейсу: `trun: version=0 flag=0x101`

```go
{
    name: "trun: version=0 flag=0x101",
    src: &Trun{
        FullBox: FullBox{
            Version: 0,
            Flags:   [3]byte{0x00, 0x01, 0x01},  // flags = 0x000101
        },
        SampleCount: 3,
        DataOffset:  50,  // опціональне поле (flag 0x01)
        Entries: []TrunEntry{
            {SampleDuration: 100},  // опціональне (flag 0x100)
            {SampleDuration: 101},
            {SampleDuration: 102},
        },
    },
    dst: &Trun{},
    // 🔹 Очікувані байти (16 байт заголовок + 12 байт даних = 28 байт)
    bin: []byte{
        0,                // version
        0x00, 0x01, 0x01, // flags (0x000101 = DataOffsetPresent + SampleDurationPresent)
        0x00, 0x00, 0x00, 0x03, // SampleCount = 3
        0x00, 0x00, 0x00, 0x32, // DataOffset = 50 (0x32)
        // Entries: тільки SampleDuration (бо flag 0x100)
        0x00, 0x00, 0x00, 0x64, // 100
        0x00, 0x00, 0x00, 0x65, // 101
        0x00, 0x00, 0x00, 0x66, // 102
    },
    // 🔹 Очікуваний вивід Stringify()
    str: `Version=0 Flags=0x000101 SampleCount=3 DataOffset=50 Entries=[{SampleDuration=100}, {SampleDuration=101}, {SampleDuration=102}]`,
},
```

**🔢 Чому саме такі байти?**

```
📦 Trun бокс з прапорцями 0x000101:
• Біт 0 (0x01): DataOffsetPresent → читаємо 4 байти: 00 00 00 32 (50)
• Біт 8 (0x100): SampleDurationPresent → кожен Entry має 4 байти Duration
• SampleCount=3 → 3 Entries × 4 байти = 12 байт

📊 Загальний розмір:
• Заголовок: 4 (version+flags) + 4 (SampleCount) = 8 байт
• DataOffset: 4 байти
• Entries: 3 × 4 = 12 байт
• Разом: 8 + 4 + 12 = 24 байти ✅
```

---

## 🔄 Чотири операції, що тестуються в кожному кейсі

### 🔹 1. `Marshal` — серіалізація (структура → байти)

```go
buf := bytes.NewBuffer(nil)
n, err := Marshal(buf, tc.src, tc.ctx)
require.NoError(t, err)
assert.Equal(t, uint64(len(tc.bin)), n)  // перевірка розміру
assert.Equal(t, tc.bin, buf.Bytes())      // перевірка вмісту: байт в байт!
```

**Що перевіряємо:**
```
📦 Вхід: Trun{Flags:0x000101, SampleCount:3, DataOffset:50, ...}
              │
              ▼
         Marshal() + бітова логіка
              │
              ▼
📤 Вихід: []byte{0, 0x00,0x01,0x01, 0x00,0x00,0x00,0x03, ...}
              │
              ▼
✅ Порівнюємо з еталонним масивом — кожен біт на своєму місці
```

---

### 🔹 2. `Unmarshal` — десеріалізація (байти → структура)

```go
r := bytes.NewReader(tc.bin)
n, err = Unmarshal(r, uint64(len(tc.bin)), tc.dst, tc.ctx)
require.NoError(t, err)
assert.Equal(t, uint64(buf.Len()), n)  // прочитано стільки ж, скільки записано
assert.Equal(t, tc.src, tc.dst)         // 🔁 round-trip: структура відновлена точно!
```

**🔁 Round-trip тест — найважливіша перевірка:**
```
Структура → байти → Структура'

Якщо Структура == Структура' → ✅ серіалізація ідемпотентна
Якщо ні → ❌ втрата даних або помилка бітової упаковки
```

---

### 🔹 3. `UnmarshalAny` — динамічний парсинг за типом

```go
dst, n, err := UnmarshalAny(
    bytes.NewReader(tc.bin), 
    tc.src.GetType(),        // BoxTypeTrun() = "trun"
    uint64(len(tc.bin)), 
    tc.ctx,
)
require.NoError(t, err)
assert.Equal(t, tc.src, dst)
```

**Навіщо `UnmarshalAny`?**
```
🔍 Сценарій: Ви читаєте файл і не знаєте наперед тип боксу

1. ReadBoxInfo() → тип="trun", size=24
2. UnmarshalAny(r, "trun", 24, ctx) 
3. Бібліотека сама:
   • Знаходить зареєстровану структуру для "trun" (Trun{})
   • Створює її екземпляр
   • Парсить дані з урахуванням прапорців
   • Повертає готовий об'єкт

✅ Ви не пишете 100+ `case "trun":` у своєму коді!
```

---

### 🔹 4. `Stringify` — людський формат для дебагу

```go
str, err := Stringify(tc.src, tc.ctx)
require.NoError(t, err)
assert.Equal(t, tc.str, str)
```

**Приклад використання в логах:**
```go
// ❌ Погано: незрозумілий дамп байтів
log.Printf("trun: % x", trunBytes)  // [00 00 01 01 00 00 00 03...]

// ✅ Добре: зрозумілі параметри
log.Printf("📦 %s", Stringify(trun, ctx))
// 📦 Version=0 Flags=0x000101 SampleCount=3 DataOffset=50 Entries=[...]
```

---

## 🧪 Додаткові тести у файлі

### 🔹 `TestFtypCompatibleBrands` — тестування списку брендів

```go
func TestFtypCompatibleBrands(t *testing.T) {
    ftyp := &Ftyp{}
    
    // Додавання брендів (без дублікатів!)
    ftyp.AddCompatibleBrand(BrandMP41())  // "mp41"
    ftyp.AddCompatibleBrand(BrandAVC1())  // "avc1"
    ftyp.AddCompatibleBrand(BrandISO5())  // "iso5"
    
    // Перевірка наявності
    require.True(t, ftyp.HasCompatibleBrand(BrandMP41()))
    require.False(t, ftyp.HasCompatibleBrand(BrandMP71()))  // не додавали
    
    // Видалення
    ftyp.RemoveCompatibleBrand(BrandMP41())
    require.False(t, ftyp.HasCompatibleBrand(BrandMP41()))
}
```

**🎯 Для вашого HLS-процесора**: Це дозволяє динамічно додавати/видаляти бренди сумісності у `ftyp` боксі.

---

### 🔹 `TestHdlrUnmarshalHandlerName` — парсинг імен хендлерів

```go
func TestHdlrUnmarshalHandlerName(t *testing.T) {
    testCases := []struct {
        name          string
        componentType []byte  // "mhlr" для QuickTime, 0 для ISO
        bytes         []byte  // сирі байти імені
        want          string  // очікуваний результат
    }{
        {
            name:          "AppleQuickTimePascalString",
            componentType: []byte("mhlr"),  // QuickTime
            bytes:         []byte{5, 'a', 'b', 'e', 'm', 'a'},  // Pascal: [len][data]
            want:          "abema",
        },
        {
            name:          "NormalString",
            componentType: []byte{0,0,0,0},  // ISO
            bytes:         []byte("abema"),  // C-string: [data][0]
            want:          "abema",
        },
    }
    // ... тестова логіка ...
}
```

**🎯 Навіщо це?** Бокс `hdlr` (Handler Reference) може зберігати ім'я у двох форматах:
- **C-string** (нуль-термінований) для стандарту ISO
- **Pascal-string** (довжина+дані) для QuickTime

Тест гарантує, що бібліотека коректно визначає формат за `componentType`.

---

### 🔹 `TestFixedPoint` — тестування фіксовано-крапкових чисел

```go
func TestFixedPoint(t *testing.T) {
    // Rate у Mvhd: 16.16 fixed-point
    mvhd := Mvhd{Rate: 0x4d2b000}  // 0x4d2b.0000 = 19755.0 у 16.16 форматі
    assert.Equal(t, float64(1234.6875), mvhd.GetRate())  // 1234 + 11/16
    assert.Equal(t, int16(1234), mvhd.GetRateInt())      // тільки ціла частина
    
    // Balance у Smhd: 8.8 fixed-point
    smhd := Smhd{Balance: 0x3420}  // 0x34.20 = 52.125
    assert.Equal(t, float32(52.125), smhd.GetBalance())
    
    // Width/Height у Tkhd: 16.16 fixed-point
    tkhd := Tkhd{Width: 0x205800, Height: 0x5ec2c00}
    assert.Equal(t, float64(32.34375), tkhd.GetWidth())   // 1920px / 65536
    assert.Equal(t, float64(1516.171875), tkhd.GetHeight()) // 1080px / 65536
}
```

**🔢 Формат 16.16 fixed-point:**
```
📐 32-бітне число: [16 біт ціла частина][16 біт дробова частина]

🔢 Приклад: 0x00010000 = 1.0
• Ціла: 0x0001 = 1
• Дробова: 0x0000 = 0/65536

🔢 Приклад: 0x00018000 = 1.5
• Ціла: 0x0001 = 1
• Дробова: 0x8000 = 32768/65536 = 0.5

🎯 Для вашого HLS: Width=1920 → 1920 × 65536 = 0x001E0000
```

---

### 🔹 `TestGetters` — тестування версійних геттерів

```go
func TestGetters(t *testing.T) {
    t.Run("cslg", func(t *testing.T) {
        cslg := &Cslg{
            CompositionToDTSShiftV0: math.MaxInt32,  // версія 0: 32-біт
            CompositionToDTSShiftV1: math.MaxInt64,  // версія 1: 64-біт
        }
        
        // Тест версії 0
        cslg.SetVersion(0)
        assert.EqualValues(t, math.MaxInt32, cslg.GetCompositionToDTSShift())
        
        // Тест версії 1
        cslg.SetVersion(1)
        assert.EqualValues(t, math.MaxInt64, cslg.GetCompositionToDTSShift())
    })
    
    // ... аналогічні тести для ctts, elst, mdhd, mehd, mvhd, saio, sidx, tfdt, tfra, tkhd, trun ...
}
```

**🎯 Навіщо це?** Багато боксів мають різні версії з різними типами полів (32-біт ↔ 64-біт). Геттери (`GetDuration()`, `GetCreationTime()` тощо) автоматично обирають правильне поле за версією.

---

### 🔹 `TestAvcCInconsistentError` — тест валідації

```go
func TestAvcCInconsistentError(t *testing.T) {
    avcc := &AVCDecoderConfiguration{
        Profile: AVCMainProfile,  // 77 = Main profile
        HighProfileFieldsEnabled: true,  // ❌ Несумісно!
        // ... інші поля ...
    }
    
    buf := bytes.NewBuffer(nil)
    _, err := Marshal(buf, avcc, Context{})
    
    // 🔹 Очікуємо помилку: HighProfileFieldsEnabled тільки для High/High10/High422 профілів
    require.Error(t, err)
    assert.Equal(t, "each values of Profile and HighProfileFieldsEnabled are inconsistent", err.Error())
}
```

**🎯 Навіщо це?** Бібліотека валідує логічну сумісність полів при записі, щоб уникнути створення невалідних файлів.

---

## 🗂️ Категорії тест-кейсів у `TestBoxTypesISO14496_12`

### 🔹 📁 Метадані файлу
| Тест | Бокс | Призначення |
|------|------|-------------|
| `ftyp` | File Type | Перевірка брендів сумісності |
| `moov` | Movie | Кореневий бокс метаданих |
| `mvhd` | Movie Header | Тривалість, timescale, матриця |

### 🔹 🎥 Відео-доріжки
| Тест | Бокс | Призначення |
|------|------|-------------|
| `trak` | Track | Опис однієї доріжки |
| `tkhd` | Track Header | Розмір, тривалість, увімкнення |
| `mdhd` | Media Header | Timescale, мова, створення |
| `vmhd` | Video Media Header | Графічний режим, колір накладок |

### 🔹 ⏱️ Таймінги та синхронізація ⭐ Найважливіші для HLS!
| Тест | Бокс | Призначення |
|------|------|-------------|
| `stts` | Decoding Time to Sample | PTS для декодування |
| `ctts` | Composition Time to Sample | PTS для відображення (B-фрейми) |
| `stss` | Sync Samples | Список ключових кадрів (I-фрейми) |
| `trun` | Track Fragment Run | **Таймстемпи, розміри, прапорці кадрів у fMP4** |
| `tfdt` | Track Fragment Decode Time | Базовий час декодування фрагмента |

### 🔹 🔊 Аудіо-параметри
| Тест | Бокс | Призначення |
|------|------|-------------|
| `smhd` | Sound Media Header | Balance (стерео-панорама) |
| `dac3` | AC-3 config | Параметри Dolby Digital (в іншому файлі) |

### 🔹 📦 Фрагментований MP4 (fMP4) — основа HLS/DASH
| Тест | Бокс | Призначення |
|------|------|-------------|
| `moof` | Movie Fragment | Метадані фрагмента |
| `mfhd` | Movie Fragment Header | Порядковий номер фрагмента |
| `traf` | Track Fragment | Фрагмент доріжки |
| `tfhd` | Track Fragment Header | Базові параметри для семплів |
| `trun` | Track Fragment Run | **Таймстемпи кадрів** |
| `sidx` | Segment Index | Індекс під-сегментів для швидкого seek |

### 🔹 🔐 DRM та захист
| Тест | Бокс | Призначення |
|------|------|-------------|
| `sinf` | Protection Scheme Info | Опис системи захисту |
| `schm` | Scheme Type | Тип DRM: "cenc" (Common Encryption) |
| `saio` / `saiz` | Sample Auxiliary Info | Офсети/розміри зашифрованих блоків |

### 🔹 🎨 Візуальні параметри
| Тест | Бокс | Призначення |
|------|------|-------------|
| `colr` | Colour Information | HDR, колірний простір (BT.709, BT.2020) |
| `pasp` | Pixel Aspect Ratio | Співвідношення пікселів (анаморфне відео) |
| `fiel` | Field Information | Interlaced video: порядок полів |

### 🔹 📝 Субтитри та тексти
| Тест | Бокс | Призначення |
|------|------|-------------|
| `stpp` | XML Subtitle Sample Entry | TTML/IMSC субтитри |
| `sbtt` | Text Subtitle Sample Entry | SRT/VTT-подібні субтитри |
| `elst` | Edit List | Пропуск тиші, обрізка початку/кінця |

---

## 🛠️ Практичне використання: Приклади для вашого HLS-процесора

### 🔹 Приклад 1: Валідація fMP4-сегмента перед відправкою клієнту

```go
func validateFragment(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    // 1. Перевірка наявності moof (фрагментований формат)
    hasMoof := false
    mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeMoof() {
            hasMoof = true
        }
        return nil, nil
    })
    if !hasMoof {
        return fmt.Errorf("not a fragmented MP4: missing moof box")
    }
    
    // 2. Перевірка trun: чи є таймстемпи для всіх кадрів?
    f.Seek(0, io.SeekStart)
    mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeTrun() {
            trun := &mp4.Trun{}
            h.ReadPayload(trun)
            
            // Перевірка: чи увімкнено SampleCompositionTimeOffset?
            if trun.GetFlags() & 0x800 == 0 {
                log.Printf("⚠️  trun missing composition time offset — можлива десинхронізація")
            }
        }
        return nil, nil
    })
    
    return nil
}
```

---

### 🔹 Приклад 2: Генерація fMP4 з правильними таймстемпами

```go
func createFragment(seq int, frames []Frame) (*bytes.Buffer, error) {
    buf := bytes.NewBuffer(nil)
    
    // 1. Запис moof
    moof := &mp4.Moof{}
    mp4.WriteBoxInfo(buf, &mp4.BoxInfo{Type: mp4.BoxTypeMoof()})
    moof.Marshal(buf)
    
    // 2. Запис traf + trun з таймстемпами
    traf := &mp4.Traf{}
    mp4.WriteBoxInfo(buf, &mp4.BoxInfo{Type: mp4.BoxTypeTraf()})
    traf.Marshal(buf)
    
    trun := &mp4.Trun{
        FullBox: mp4.FullBox{Version: 0, Flags: [3]byte{0x00, 0x0c, 0x00}}, // flags: SampleFlags + SampleCompositionTimeOffset
        SampleCount: uint32(len(frames)),
        Entries: make([]mp4.TrunEntry, len(frames)),
    }
    
    for i, frame := range frames {
        trun.Entries[i] = mp4.TrunEntry{
            SampleFlags:                   frame.Flags,
            SampleCompositionTimeOffsetV0: frame.CTSOffset,  // різниця між DTS та PTS
        }
    }
    
    mp4.WriteBoxInfo(buf, &mp4.BoxInfo{Type: mp4.BoxTypeTrun()})
    trun.Marshal(buf)
    
    // 3. Запис mdat з сирими даними
    mdat := &mp4.Mdat{Data: frameData}
    mp4.WriteBoxInfo(buf, &mp4.BoxInfo{Type: mp4.BoxTypeMdat()})
    mdat.Marshal(buf)
    
    return buf, nil
}
```

---

### 🔹 Приклад 3: Читання метаданих для HLS-плейлиста

```go
func extractMetadataForPlaylist(filePath string) (*PlaylistMetadata, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    meta := &PlaylistMetadata{}
    
    mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        switch h.BoxInfo.Type {
        case mp4.BoxTypeTkhd():  // Track Header
            tkhd := &mp4.Tkhd{}
            h.ReadPayload(tkhd)
            meta.Width = int(tkhd.GetWidthInt())
            meta.Height = int(tkhd.GetHeightInt())
            
        case mp4.BoxTypeMdhd():  // Media Header
            mdhd := &mp4.Mdhd{}
            h.ReadPayload(mdhd)
            meta.Timescale = mdhd.Timescale
            meta.Duration = time.Duration(mdhd.GetDuration()) * time.Second / time.Duration(mdhd.Timescale)
            
        case mp4.BoxTypeHdlr():  // Handler Reference
            hdlr := &mp4.Hdlr{}
            h.ReadPayload(hdlr)
            if string(hdlr.HandlerType[:]) == "vide" {
                meta.Type = "video"
            } else if string(hdlr.HandlerType[:]) == "soun" {
                meta.Type = "audio"
            }
        }
        return nil, nil
    })
    
    return meta, nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Ігнорування `opt=...` у `trun` | Читаєте "зайві" поля → зсув таймстемпів | Завжди перевіряйте `trun.GetFlags() & 0x100` перед читанням `SampleDuration` |
| Неправильна обробка `ver=0` / `nver=0` | 32-бітне значення читається як 64-бітне → помилка | Використовуйте допоміжні методи: `trun.GetSampleCompositionTimeOffset()` |
| Забути `const=N` валідацію | Пошкоджені файли не відхиляються → артефакти | Додайте перевірку: `if box.Reserved != 0 { return err }` |
| Неправильне кодування `iso639-2` | Мова відображається як "und" | Використовуйте формулу: `код = (літера - 'a' + 1)` |
| Ігнорування `len=dynamic` | Читаєте не ту кількість елементів → краш | Завжди реалізуйте `GetFieldLength()` для слайсів |

---

## 📋 Чекліст для вашого проекту

```
[ ] Для парсингу fMP4-сегментів:
    • Завжди шукайте `trun` бокси для таймстемпів
    • Перевіряйте прапорці перед читанням опціональних полів
    • Використовуйте `GetSampleCompositionTimeOffset()` для CTS

[ ] Для сегментації на ключових кадрах:
    • Читайте `stss` для пошуку I-фреймів
    • Або перевіряйте `SampleFlags & 0x00010000` у `trun.Entries`

[ ] Для валідації кодеків:
    • Читайте `avcC` / `hvcC` / `Av1C` з `stsd`
    • Перевіряйте профіль/рівень для сумісності з вебом

[ ] Для метаданих:
    • Шукайте бокси всередині `udta` з `Context.UnderUdta=true`
    • Декодуйте мову з 5-бітного формату для 3GPP-рядків

[ ] Для дебагу:
    • Логуйте тип боксу та розмір: 
      log.Printf("📦 %s @ offset=%d, size=%d", 
          bi.Type, bi.Offset, bi.Size)
    • Використовуйте `Stringify()` для людського виводу структур

[ ] Для тестування:
    • Напишіть тести з `Marshal` → `Unmarshal` для ваших боксів
    • Перевіряйте round-trip: структура → байти → структура
```

---

## 🎯 Висновок

> **Цей файл — "енциклопедія тестів" для бібліотеки `go-mp4`**.  
> Він гарантує:
> • ✅ Коректну бітову упаковку для 100+ типів боксів
> • ✅ Ідемпотентність серіалізації/десеріалізації (round-trip)
> • ✅ Динамічний парсинг через `UnmarshalAny`
> • ✅ Зручний дебаг через `Stringify`
> • ✅ Валідацію логічної сумісності полів

Для вашого **CCTV HLS Processor** це означає:
- 🎥 Коректна синхронізація аудіо/відео через `trun` / `ctts`
- 🔍 Швидка сегментація на ключових кадрах через `stss`
- 🌐 Сумісність з різними кодеками (H.264, HEVC, AV1) через `avcC` / `hvcC` / `Av1C`
- 📝 Підтримка метаданих (назви, описи, мови) через `udta` + 3GPP-рядки
- 🧪 Впевненість, що ваш код не "зламає" формат при модифікації

Потребуєте допомоги з парсингом конкретного боксу (`trun` для таймстемпів, `stss` для ключових кадрів) або з інтеграцією цих структур у ваш конвеєр? Напишіть — покажу готовий код! 🚀🎬