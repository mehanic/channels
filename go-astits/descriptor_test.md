# Глибоке роз'яснення: Тести дескрипторів у astits — парсинг та серіалізація метаданих DVB

Цей файл містить **комплексну валідацію парсингу та запису DVB-дескрипторів** — структур метаданих, що описують кодеки, мови, розклади передач, рейтинги та іншу інформацію в таблицях MPEG-TS/DVB.

---

## 🎯 Навіщо дескриптори потрібні у вашому пайплайні?

```
┌─────────────────────────────────────────┐
│ Дескриптори у контексті CCTV HLS:      │
│                                         │
│ 🔹 Ідентифікація кодеків:               │
│   • AVCVideo → H.264 profile/level      │
│   • AC3/EnhancedAC3 → аудіо-параметри  │
│   • StreamIdentifier → маркування потоків│
│                                         │
│ 🔹 Мультимовна підтримка:               │
│   • ISO639LanguageAndAudioType → мови  │
│   • Subtitling/Teletext → субтитри     │
│                                         │
│ 🔹 Розклад та метадані (EIT):          │
│   • ShortEvent/ExtendedEvent → назви   │
│   • Content → категорії передач        │
│                                         │
│ 🔹 Якість та сумісність:                │
│   • MaximumBitrate → обмеження бітрейту│
│   • ParentalRating → вікові обмеження  │
│   • Registration → ідентифікація формату│
└─────────────────────────────────────────┘
```

---

## 🔧 Архітектура тестів: стратегія валідації

### 📦 Структура тестових даних

```go
type descriptorTest struct {
    name      string                           // Назва для t.Run()
    bytesFunc func(w *astikit.BitsWriter)     // Генератор "еталонних" байтів
    desc      Descriptor                       // Очікувана структура після парсингу
}
```

**Методологія тестування:**
```
┌─────────────────────────────────────────┐
│ 1. Генеруємо байти через bytesFunc()   │
│    (ручне кодування за специфікацією)  │
│                                         │
│ 2. Парсимо через parseDescriptors()    │
│    (тестуємо парсер)                   │
│                                         │
│ 3. Порівнюємо результат з desc         │
│    (структурна рівність)               │
│                                         │
│ 4. Зворотний тест: writeDescriptor()   │
│    → порівняння байтів (бінарна ідентичність)│
└─────────────────────────────────────────┘
```

### 🧪 Типи тестів у файлі

| Тест | Призначення | Що перевіряє |
|------|-------------|--------------|
| `TestParseDescriptorOneByOne` | Парсинг окремих дескрипторів | Кожен тип дескриптора парситься коректно |
| `TestParseDescriptorAll` | Парсинг послідовності дескрипторів | Коректна обробка кількох дескрипторів підряд |
| `TestWriteDescriptorOneByOne` | Серіалізація окремих дескрипторів | `writeDescriptor()` генерує ідентичні байти |
| `TestWriteDescriptorAll` | Серіалізація послідовності | `writeDescriptorsWithLength()` зберігає порядок та довжину |
| `BenchmarkWrite/ParseDescriptor` | Продуктивність | Швидкість та алокації для кожного типу |
| `FuzzDescriptor` | Стійкість до пошкоджених даних | Парсер не панікує на випадкових вхідних даних |

---

## 🔍 Розбір ключових дескрипторів

### 🎬 AVCVideo — параметри H.264/AVC

```go
// Тестові дані:
w.Write(uint8(DescriptorTagAVCVideo))  // Tag = 0x28
w.Write(uint8(4))                       // Length = 4 байти
w.Write(uint8(1))                       // profile_idc = 1 (Baseline)
w.Write("1")                            // constraint_set0_flag = true
w.Write("1")                            // constraint_set1_flag = true
w.Write("1")                            // constraint_set2_flag = true
w.Write("10101")                        // compatible_flags = 21
w.Write(uint8(2))                       // level_idc = 2 (Level 2)
w.Write("1")                            // AVC_still_present = true
w.Write("1")                            // AVC_24_hour_picture_flag = true
w.Write("111111")                       // reserved = 0x3F

// Очікувана структура:
Descriptor{
    Tag: DescriptorTagAVCVideo,
    Length: 4,
    AVCVideo: &DescriptorAVCVideo{
        ProfileIDC: 1,
        ConstraintSet0Flag: true,
        ConstraintSet1Flag: true,
        ConstraintSet2Flag: true,
        CompatibleFlags: 21,
        LevelIDC: 2,
        AVCStillPresent: true,
        AVC24HourPictureFlag: true,
    },
}
```

**Використання у вашому пайплайні:**
```go
// При реєстрації відео-потоку — додати AVCVideo дескриптор:
muxer.AddElementaryStream(astits.PMTElementaryStream{
    ElementaryPID: videoPID,
    StreamType: astits.StreamTypeH264Video,
    ElementaryStreamDescriptors: []*astits.Descriptor{
        {
            Tag: astits.DescriptorTagAVCVideo,
            AVCVideo: &astits.DescriptorAVCVideo{
                ProfileIDC: 100,  // High profile
                LevelIDC: 41,     // Level 4.1 для 1080p30
                AVCStillPresent: true,
            },
        },
    },
})
```

### 🔊 AC3 / EnhancedAC3 — параметри аудіо

```go
// AC3 дескриптор має прапорці для умовних полів:
w.Write("1")  // Component type flag = true → читаємо ComponentType
w.Write("1")  // BSID flag = true → читаємо BSID
// ... інші прапорці ...
w.Write(uint8(1))  // ComponentType (якщо прапорець = 1)

// Структура з прапорцями:
AC3: &DescriptorAC3{
    HasComponentType: true,  // 🎯 прапорець, а не значення!
    ComponentType: 1,        // 🎯 значення, читається тільки якщо прапорець=1
    // ...
}
```

> 💡 **Ключовий момент**: Багато дескрипторів використовують **прапорці присутності** (flag fields). Парсер спочатку читає прапорці, потім умовно читає відповідні поля.

### 🌐 ISO639LanguageAndAudioType — мовна підтримка

```go
// Простий, але критичний дескриптор:
w.Write(uint8(DescriptorTagISO639LanguageAndAudioType))
w.Write(uint8(4))                    // Length = 4 байти
w.Write([]byte("eng"))              // ISO 639-2 код мови (3 байти)
w.Write(uint8(AudioTypeCleanEffects))  // Тип аудіо (0=не визначено, 1=чисті ефекти, 2=для слабкочуючих...)

// Використання:
// • Плеєр показує "English" у списку аудіо-доріжок
// • Автоматичний вибір мови за налаштуваннями користувача
```

### 📺 ShortEvent / ExtendedEvent — метадані подій (EIT)

```go
// ShortEvent: базова інформація про передачу
Descriptor{
    Tag: DescriptorTagShortEvent,
    ShortEvent: &DescriptorShortEvent{
        Language:  []byte("eng"),      // Мова опису
        EventName: []byte("News"),     // Назва передачі
        Text:      []byte("Evening news update"),  // Короткий опис
    },
}

// ExtendedEvent: розширений опис з парами ключ-значення
ExtendedEvent: &DescriptorExtendedEvent{
    Items: []*DescriptorExtendedEventItem{
        {
            Description: []byte("Genre"),
            Content:     []byte("News"),
        },
        {
            Description: []byte("Presenter"),
            Content:     []byte("John Smith"),
        },
    },
    Text: []byte("Full episode description..."),  // Довгий опис
}
```

**Використання для HLS EPG:**
```go
// При генерації плейлиста — додавати метадані подій:
func addEventMetadata(playlist *HLSPlaylist, event *astits.EITDataEvent) {
    for _, desc := range event.Descriptors {
        if desc.ShortEvent != nil {
            // Додати назву передачі як коментар у плейлист
            playlist.AddComment(fmt.Sprintf("# %s", desc.ShortEvent.EventName))
        }
        if desc.Content != nil {
            // Додати категорію для фільтрації
            for _, item := range desc.Content.Items {
                category := contentNibbleToString(item.ContentNibbleLevel1)
                playlist.AddTag(fmt.Sprintf("#EXT-X-GENRE:%s", category))
            }
        }
    }
}
```

### 🔢 Subtitling / Teletext — субтитри та телетекст

```go
// Subtitling дескриптор підтримує кілька мов:
Subtitling: &DescriptorSubtitling{
    Items: []*DescriptorSubtitlingItem{
        {
            Language:          []byte("eng"),  // Мова субтитрів
            Type:              0x10,           // Тип: 0x10 = DVB subtitles
            CompositionPageID: 1,              // Сторінка композиції
            AncillaryPageID:   2,              // Додаткова сторінка
        },
        {
            Language: []byte("ukr"),
            Type:     0x10,
            // ...
        },
    },
}
```

> 💡 **Важливо**: `CompositionPageID` та `AncillaryPageID` використовуються для синхронізації субтитрів з відео — критично для вашого orphan audio/video merge.

---

## 🔁 Round-trip валідація: парсинг ↔ запис

### Методологія `TestWriteDescriptorOneByOne`

```go
func TestWriteDescriptorOneByOne(t *testing.T) {
    for _, tc := range descriptorTestTable {
        t.Run(tc.name, func(t *testing.T) {
            // 1. Генеруємо "еталонні" байти вручну
            bufExpected := bytes.Buffer{}
            wExpected := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &bufExpected})
            tc.bytesFunc(wExpected)  // ручне кодування за специфікацією
            
            // 2. Генеруємо байти через writeDescriptor()
            bufActual := bytes.Buffer{}
            wActual := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &bufActual})
            n, err := writeDescriptor(wActual, &tc.desc)
            
            // 3. Порівнюємо бінарно
            assert.Equal(t, bufExpected.Bytes(), bufActual.Bytes())
        })
    }
}
```

**Чому це важливо:**
```
• Ручне кодування (bytesFunc) = "джерело істини" за специфікацією
• writeDescriptor() = реалізація бібліотеки
• Бінарна ідентичність = гарантія сумісності з іншими декодерами

Якщо байти відрізняються:
→ Плеєри можуть відкинути дескриптор як невалідний
→ Метадані не відобразяться у клієнта
→ Можливі помилки парсингу на стороні приймача
```

---

## 🧪 Fuzz-тест: стійкість до пошкоджених даних

```go
func FuzzDescriptor(f *testing.F) {
    // Seed: додаємо валідні дескриптори як початкові дані
    f.Add(descBytes)  // descBytes = валідна послідовність з тестів
    
    f.Fuzz(func(t *testing.T, b []byte) {
        // Парсимо довільні байти — не повинно панікувати!
        ds, err := parseDescriptors(astikit.NewBytesIterator(b))
        
        // Якщо парсинг успішний — пробуємо записати назад
        if err == nil {
            bufActual := bytes.Buffer{}
            wActual := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &bufActual})
            writeDescriptorsWithLength(wActual, ds)
            // Не перевіряємо результат — головне, що немає паніки
        }
    })
}
```

**Запуск fuzz-тесту:**
```bash
go test -fuzz=FuzzDescriptor -fuzztime=60s
```

**Що це ловить:**
```
• Вихід за межі буфера при читанні полів
• Ділення на нуль при обробці довжин
• Невірні припущення про формат даних
• Пам'яткові витоки при помилках парсингу
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Додавання дескрипторів при реєстрації потоків

```go
// У вашому ChannelMuxer:
func addVideoStreamWithDescriptors(muxer *astits.Muxer, pid uint16, config VideoConfig) error {
    descriptors := []*astits.Descriptor{}
    
    // 🔹 AVCVideo для H.264
    if config.Codec == "h264" {
        descriptors = append(descriptors, &astits.Descriptor{
            Tag: astits.DescriptorTagAVCVideo,
            AVCVideo: &astits.DescriptorAVCVideo{
                ProfileIDC: config.Profile,  // наприклад, 100 = High
                LevelIDC:   config.Level,    // наприклад, 41 = 4.1
                AVCStillPresent: true,
            },
        })
    }
    
    // 🔹 MaximumBitrate для адаптивного стрімінгу
    if config.MaxBitrate > 0 {
        // Бітрейт у бітах/сек → у 50-бітних одиницях (стандарт DVB)
        bitrateUnits := uint32(config.MaxBitrate / 50)
        descriptors = append(descriptors, &astits.Descriptor{
            Tag: astits.DescriptorTagMaximumBitrate,
            MaximumBitrate: &astits.DescriptorMaximumBitrate{
                Bitrate: bitrateUnits,
            },
        })
    }
    
    // 🔹 StreamIdentifier для маркування
    descriptors = append(descriptors, &astits.Descriptor{
        Tag: astits.DescriptorTagStreamIdentifier,
        StreamIdentifier: &astits.DescriptorStreamIdentifier{
            ComponentTag: 1,  // унікальний тег для цього потоку
        },
    })
    
    return muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: pid,
        StreamType: astits.StreamTypeH264Video,
        ElementaryStreamDescriptors: descriptors,
    })
}
```

### ✅ 2. Парсинг дескрипторів з вхідного потоку

```go
// У segmentAssembler — витягування метаданих з PMT:
func extractStreamMetadata(pmt *astits.PMTData) StreamMetadata {
    meta := StreamMetadata{}
    
    for _, es := range pmt.ElementaryStreams {
        for _, desc := range es.ElementaryStreamDescriptors {
            switch desc.Tag {
            case astits.DescriptorTagISO639LanguageAndAudioType:
                meta.Language = string(desc.ISO639LanguageAndAudioType.Language)
                meta.AudioType = desc.ISO639LanguageAndAudioType.Type
                
            case astits.DescriptorTagAVCVideo:
                meta.VideoProfile = desc.AVCVideo.ProfileIDC
                meta.VideoLevel = desc.AVCVideo.LevelIDC
                
            case astits.DescriptorTagSubtitling:
                for _, item := range desc.Subtitling.Items {
                    meta.Subtitles = append(meta.Subtitles, SubtitleInfo{
                        Language: string(item.Language),
                        Type: item.Type,
                    })
                }
            }
        }
    }
    return meta
}
```

### ✅ 3. Фільтрація дескрипторів за каналом

```go
// У channel-aware архітектурі — передавати тільки релевантні дескриптори:
func filterDescriptorsForChannel(descs []*astits.Descriptor, channelID string) []*astits.Descriptor {
    var filtered []*astits.Descriptor
    
    for _, d := range descs {
        // 🔹 Мовні дескриптори: тільки якщо мова підтримується каналом
        if d.ISO639LanguageAndAudioType != nil {
            lang := string(d.ISO639LanguageAndAudioType.Language)
            if !isLanguageSupported(channelID, lang) {
                continue  // пропустити непідтримувану мову
            }
        }
        
        // 🔹 Субтитри: тільки якщо увімкнено для каналу
        if d.Subtitling != nil && !subtitlesEnabled(channelID) {
            continue
        }
        
        filtered = append(filtered, d)
    }
    return filtered
}
```

### ✅ 4. Моніторинг дескрипторів для відладки

```go
// monitoring.Monitor — метрики для дескрипторів:
type DescriptorMetrics struct {
    DescriptorsParsed  *prometheus.CounterVec  // кількість парсингів по типу
    UnknownDescriptors *prometheus.CounterVec  // невідомі типи дескрипторів
    DescriptorErrors   *prometheus.CounterVec  // помилки парсингу
}

// У парсингу:
func parseWithMetrics(iter *astikit.BytesIterator, channelID string, metrics *DescriptorMetrics) ([]*astits.Descriptor, error) {
    descs, err := parseDescriptors(iter)
    
    for _, d := range descs {
        metrics.DescriptorsParsed.WithLabelValues(
            channelID, 
            descriptorTagName(d.Tag),  // human-readable name
        ).Inc()
        
        if d.Unknown != nil {
            metrics.UnknownDescriptors.WithLabelValues(
                channelID,
                fmt.Sprintf("tag_0x%02X", d.Tag),
            ).Inc()
        }
    }
    
    if err != nil {
        metrics.DescriptorErrors.WithLabelValues(channelID).Inc()
    }
    
    return descs, err
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на дескриптори з українською мовою

```go
func TestDescriptor_UkrainianLanguage(t *testing.T) {
    // Дескриптор для української мови (ISO 639-2: "ukr")
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.Write(uint8(astits.DescriptorTagISO639LanguageAndAudioType))
    w.Write(uint8(4))                    // Length
    w.Write([]byte("ukr"))              // Ukrainian language code
    w.Write(uint8(astits.AudioTypeNormal))  // Normal audio
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    descs, err := parseDescriptors(iter)
    assert.NoError(t, err)
    assert.Len(t, descs, 1)
    
    desc := descs[0]
    assert.Equal(t, astits.DescriptorTagISO639LanguageAndAudioType, desc.Tag)
    assert.Equal(t, []byte("ukr"), desc.ISO639LanguageAndAudioType.Language)
}
```

### 🔹 Тест на велику кількість субтитрів

```go
func TestDescriptor_MultipleSubtitles(t *testing.T) {
    // Субтитри для 10 мов одночасно
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.Write(uint8(astits.DescriptorTagSubtitling))
    w.Write(uint8(16 * 8))  // Length: 8 items × 16 bytes each
    
    for i := 0; i < 8; i++ {
        lang := fmt.Sprintf("lg%d", i)  // "lg0", "lg1", ...
        w.Write([]byte(lang))           // 3 bytes language
        w.Write(uint8(0x10))            // type = DVB subtitles
        w.Write(uint16(i + 1))          // composition page ID
        w.Write(uint16(i + 10))         // ancillary page ID
    }
    
    // Парсинг
    descs, err := parseDescriptors(astikit.NewBytesIterator(buf.Bytes()))
    assert.NoError(t, err)
    assert.Len(t, descs[0].Subtitling.Items, 8)
    
    // Перевірити, що всі мови збереглися
    for i, item := range descs[0].Subtitling.Items {
        expectedLang := fmt.Sprintf("lg%d", i)
        assert.Equal(t, expectedLang, string(item.Language))
        assert.Equal(t, uint16(i+1), item.CompositionPageID)
    }
}
```

### 🔹 Тест на дескриптори з динамічними довжинами

```go
func TestDescriptor_VariableLengthFields(t *testing.T) {
    // ExtendedEvent з різними довжинами описів
    testCases := []struct {
        descLen int
        textLen int
    }{
        {1, 1},    // мінімальні значення
        {50, 100}, // середні
        {255, 255},// максимальні (1 байт довжини)
    }
    
    for _, tc := range testCases {
        t.Run(fmt.Sprintf("desc_%d_text_%d", tc.descLen, tc.textLen), func(t *testing.T) {
            buf := &bytes.Buffer{}
            w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
            
            w.Write(uint8(astits.DescriptorTagExtendedEvent))
            // Розрахувати загальну довжину...
            
            // Згенерувати довгі рядки
            description := bytes.Repeat([]byte("D"), tc.descLen)
            content := bytes.Repeat([]byte("C"), tc.textLen)
            
            w.Write(uint8(tc.descLen))  // description length
            w.Write(description)
            w.Write(uint8(tc.textLen))  // content length
            w.Write(content)
            
            // Парсинг має впоратися з будь-якою довжиною
            descs, err := parseDescriptors(astikit.NewBytesIterator(buf.Bytes()))
            assert.NoError(t, err)
            assert.Equal(t, description, descs[0].ExtendedEvent.Items[0].Description)
            assert.Equal(t, content, descs[0].ExtendedEvent.Items[0].Content)
        })
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання прапорців у AC3 | Поля читаються не за тими умовами | Перевірити порядок: спочатку прапорці, потім умовні поля |
| Переповнення буфера при довгих рядках | Паніка при `NextBytes(length)` | Додати перевірку: `if length > remaining { return error }` |
| Невідомі теги дескрипторів | `desc.Unknown != nil` замість конкретного типу | Додати лог-повідомлення з тегом для відладки нових типів |
| Невірний розрахунок загальної довжини | `program_info_length` не збігається з фактичним | Використовувати `calc*Length()` helper-функції перед записом |
| Порядок дескрипторів змінюється | Клієнти не бачать очікуваних метаданих | Зберігати порядок при парсингу/записі; не використовувати map для зберігання |

### Приклад безпечного парсингу змінної довжини:

```go
func parseVariableLengthField(i *astikit.BytesIterator, maxLen int) ([]byte, error) {
    lengthByte, err := i.NextByte()
    if err != nil { return nil, err }
    
    length := int(lengthByte)
    if length > maxLen {
        return nil, fmt.Errorf("field length %d exceeds max %d", length, maxLen)
    }
    
    // Перевірити, чи достатньо даних у ітераторі
    if i.Len() < length {
        return nil, fmt.Errorf("insufficient data: need %d, have %d", length, i.Len())
    }
    
    return i.NextBytes(length)
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Створення дескриптора з прапорцями (AC3 приклад):
func createAC3Descriptor(config AC3Config) *astits.Descriptor {
    desc := &astits.Descriptor{
        Tag:    astits.DescriptorTagAC3,
        Length: 0,  // буде розраховано при записі
        AC3: &astits.DescriptorAC3{
            HasComponentType: config.ComponentType != 0,
            ComponentType:    uint8(config.ComponentType),
            HasBSID:          config.BSID != 0,
            BSID:             uint8(config.BSID),
            // ... інші поля ...
        },
    }
    // Length розраховується автоматично у writeDescriptor()
    return desc
}

// 2. Парсинг з обробкою невідомих типів:
func safeParseDescriptors(iter *astikit.BytesIterator, channelID string) ([]*astits.Descriptor, error) {
    descs, err := parseDescriptors(iter)
    
    for _, d := range descs {
        if d.Unknown != nil {
            log.Debugf("Channel %s: unknown descriptor tag 0x%02X (len=%d)", 
                channelID, d.Tag, d.Length)
            // Опція: зберегти для подальшого аналізу
            storeUnknownDescriptor(channelID, d)
        }
    }
    
    return descs, err
}

// 3. Фільтрація за типом для оптимізації:
func findDescriptorByTag(descs []*astits.Descriptor, tag uint8) *astits.Descriptor {
    for _, d := range descs {
        if d.Tag == tag {
            return d
        }
    }
    return nil
}

// Використання:
if ac3Desc := findDescriptorByTag(descs, astits.DescriptorTagAC3); ac3Desc != nil {
    // Обробити AC3-параметри
    processAC3Config(ac3Desc.AC3)
}

// 4. Серіалізація з перевіркою довжини:
func writeDescriptorSafe(w *astikit.BitsWriter, desc *astits.Descriptor) error {
    // Попередній розрахунок довжини для валідації
    expectedLen := calcDescriptorLength(desc)  // ваша helper-функція
    if expectedLen > 255 {  // 1 байт для Length field
        return fmt.Errorf("descriptor too long: %d > 255", expectedLen)
    }
    
    _, err := writeDescriptor(w, desc)
    return err
}
```

---

## 📊 Матриця дескрипторів для CCTV HLS

```
Тип дескриптора          | Теги (hex) | Використання у вашому пайплайні
─────────────────────────┼────────────┼─────────────────────────────────
AVCVideo                 | 0x28       | ✅ Параметри H.264 для сумісності
AC3 / EnhancedAC3        | 0x6A / 0x7A| ✅ Налаштування аудіо-кодека
ISO639Language           | 0x0A       | ✅ Мовна підтримка для UI
StreamIdentifier         | 0x52       | ✅ Маркування потоків для merge
Subtitling / Teletext    | 0x59 / 0x56| ⚠️ Якщо потрібні субтитри
ShortEvent / ExtendedEvent| 0x4D / 0x4E| ⚠️ Для EPG у HLS-плейлистах
MaximumBitrate           | 0x0E       | ✅ Для адаптивного бітрейту
Registration             | 0x05       | ✅ Ідентифікація формату (напр., "AC3 ")
Unknown                  | будь-який  | 🔍 Логування для відладки нових типів
```

---

## 📚 Корисні посилання

- [ETSI EN 300 468: DVB descriptors specification](https://www.etsi.org/deliver/etsi_en/300400_300499/300468/)
- [ISO/IEC 13818-1: MPEG-2 Systems](https://www.iso.org/standard/61236.html)
- [astits descriptor parsing source](https://github.com/asticode/go-astits/blob/master/descriptor.go)

> 💡 **Ключова ідея**: Дескриптори — це "мова метаданих" MPEG-TS. Кожен `Tag + Length + Payload` — це стандартизований спосіб передати інформацію про потік без зміни основного формату. У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Забезпечити сумісність з будь-яким плеєром через стандартні дескриптори
> - 🌐 Підтримувати мультимовність та субтитри для міжнародної аудиторії
> - 📊 Збагачувати HLS-плейлисти метаданими для EPG та аналітики
> - 🔍 Відлагоджувати нові формати через логування `Unknown` дескрипторів

Якщо потрібно — можу допомогти:
- 🧩 Реалізувати кастомні дескриптори для ваших внутрішніх метаданих (напр., channel_id, seq_num)
- 🔄 Додати автоматичну конвертацію дескрипторів при транскодуванні між форматами
- 🧪 Написати property-based тести для валідації edge cases у парсингу змінних полів

🛠️