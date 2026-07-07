# Глибоке роз'яснення: Дескриптори DVB у astits — парсинг та серіалізація метаданих

Цей файл містить **повну реалізацію парсингу та запису дескрипторів DVB** — структур метаданих, що описують кодеки, мови, розклади, рейтинги та іншу інформацію в таблицях MPEG-TS/DVB. Це "словник метаданих" вашого пайплайну.

---

## 🎯 Навіщо дескриптори потрібні у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ Дескриптори у контексті HLS-стрімінгу: │
│                                         │
│ 🔹 Ідентифікація кодеків:               │
│   • AVCVideo (0x28) → H.264 profile/level│
│   • AC3/EnhancedAC3 → аудіо-параметри  │
│   • StreamIdentifier → маркування потоків│
│                                         │
│ 🔹 Мультимовна підтримка:               │
│   • ISO639Language (0x0A) → мови аудіо │
│   • Subtitling (0x59) → субтитри       │
│   • Teletext (0x56) → телетекст        │
│                                         │
│ 🔹 Розклад та EPG (EIT таблиці):       │
│   • ShortEvent (0x4D) → назва передачі │
│   • ExtendedEvent (0x4E) → опис + метадані│
│   • Content (0x54) → категорії         │
│                                         │
│ 🔹 Якість та сумісність:                │
│   • MaximumBitrate (0x0E) → обмеження  │
│   • ParentalRating (0x55) → вікові обмеження│
│   • Registration (0x05) → ідентифікація формату│
└─────────────────────────────────────────┘
```

---

## 🔧 Архітектура: патерни парсингу та запису

### 📦 Структура `Descriptor` — універсальний контейнер

```go
type Descriptor struct {
    Tag    uint8  // 🎯 ключовий: визначає тип даних
    Length uint8  // довжина payload (без Tag+Length)
    
    // 🔹 Типізовані поля — тільки одне заповнене залежно від Tag:
    AVCVideo                   *DescriptorAVCVideo
    ISO639LanguageAndAudioType *DescriptorISO639LanguageAndAudioType
    ShortEvent                 *DescriptorShortEvent
    // ... ще 20+ типів ...
    
    // 🔹 fallback для невідомих типів:
    Unknown *DescriptorUnknown
    UserDefined []byte  // для тегів 0x80-0xFE
}
```

**Ключова ідея**: `Tag` визначає, яке поле буде заповнене після парсингу. Це типова discriminated union реалізація в Go.

---

### 🔍 Патерн парсингу: `newDescriptor*` функції

#### Приклад: `newDescriptorAC3` — прапорці та умовні поля

```go
func newDescriptorAC3(i *astikit.BytesIterator, offsetEnd int) (*DescriptorAC3, error) {
    // 1. Читання байта прапорців
    b, _ := i.NextByte()
    d := &DescriptorAC3{
        HasComponentType: b&0x80 > 0,  // 🎯 біт 7
        HasBSID:          b&0x40 > 0,  // біт 6
        HasMainID:        b&0x20 > 0,  // біт 5
        HasASVC:          b&0x10 > 0,  // біт 4
    }
    
    // 2. Умовне читання полів за прапорцями
    if d.HasComponentType {
        b, _ := i.NextByte()
        d.ComponentType = b
    }
    if d.HasBSID {
        b, _ := i.NextByte()
        d.BSID = b
    }
    // ... інші поля ...
    
    // 3. Додаткові дані до кінця дескриптора
    if i.Offset() < offsetEnd {
        d.AdditionalInfo, _ = i.NextBytes(offsetEnd - i.Offset())
    }
    return d, nil
}
```

**Візуалізація формату:**
```
AC3 Descriptor:
[Tag=0x6A][Length=N][Flags][ComponentType?][BSID?][MainID?][ASVC?][AdditionalInfo...]
                    ↑
              1 байт прапорців:
              [7] ComponentType flag
              [6] BSID flag
              [5] MainID flag
              [4] ASVC flag
              [3-0] reserved (0xF)
```

> 💡 **Ключовий момент**: Прапорці дозволяють економити місце — поля читаються тільки якщо потрібні. Це критично для обмежених дескрипторів (макс. 255 байт).

---

#### Приклад: `newDescriptorAVCVideo` — фіксована структура

```go
func newDescriptorAVCVideo(i *astikit.BytesIterator) (*DescriptorAVCVideo, error) {
    d := &DescriptorAVCVideo{}
    
    // Байт 1: profile_idc
    b, _ := i.NextByte()
    d.ProfileIDC = b
    
    // Байт 2: constraint flags + compatible_flags
    b, _ = i.NextByte()
    d.ConstraintSet0Flag = b&0x80 > 0
    d.ConstraintSet1Flag = b&0x40 > 0
    d.ConstraintSet2Flag = b&0x20 > 0
    d.CompatibleFlags = b & 0x1f  // нижні 5 біт
    
    // Байт 3: level_idc
    b, _ = i.NextByte()
    d.LevelIDC = b
    
    // Байт 4: AVC flags
    b, _ = i.NextByte()
    d.AVCStillPresent = b&0x80 > 0
    d.AVC24HourPictureFlag = b&0x40 > 0
    // решта 6 біт = reserved
    
    return d, nil
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
                ConstraintSet0Flag: true,  // сумісність
                AVCStillPresent: true,
            },
        },
    },
})
```

---

#### Приклад: `newDescriptorShortEvent` — змінні рядки

```go
func newDescriptorShortEvent(i *astikit.BytesIterator) (*DescriptorShortEvent, error) {
    d := &DescriptorShortEvent{}
    
    // 1. Мова (фіксовано 3 байти, ISO 639-2)
    d.Language, _ = i.NextBytes(3)  // напр. "eng", "ukr"
    
    // 2. Назва події (довжина + дані)
    b, _ := i.NextByte()
    eventLength := int(b)
    d.EventName, _ = i.NextBytes(eventLength)
    
    // 3. Текст опису (довжина + дані)
    b, _ = i.NextByte()
    textLength := int(b)
    d.Text, _ = i.NextBytes(textLength)
    
    return d, nil
}
```

**Формат у байтах:**
```
[0x4D][Length][lang:3][eventName_len][eventName...][text_len][text...]
```

---

### ✏️ Патерн запису: `calc*Length` + `writeDescriptor*`

#### Двоетапна серіалізація

```go
// Етап 1: Розрахунок довжини
func calcDescriptorAC3Length(d *DescriptorAC3) uint8 {
    if d == nil { return 0 }
    ret := 1  // байт прапорців
    
    // Додати байти для умовних полів
    if d.HasComponentType { ret++ }
    if d.HasBSID { ret++ }
    if d.HasMainID { ret++ }
    if d.HasASVC { ret++ }
    
    // Додати довжину додаткових даних
    ret += len(d.AdditionalInfo)
    return uint8(ret)
}

// Етап 2: Запис даних
func writeDescriptorAC3(w *astikit.BitsWriter, d *DescriptorAC3) error {
    b := astikit.NewBitsWriterBatch(w)
    
    // Запис прапорців у правильному порядку біт
    b.Write(d.HasComponentType)  // біт 7
    b.Write(d.HasBSID)           // біт 6
    b.Write(d.HasMainID)         // біт 5
    b.Write(d.HasASVC)           // біт 4
    b.WriteN(uint8(0xff), 4)     // reserved = 0b1111
    
    // Умовні поля
    if d.HasComponentType { b.Write(d.ComponentType) }
    if d.HasBSID { b.Write(d.BSID) }
    if d.HasMainID { b.Write(d.MainID) }
    if d.HasASVC { b.Write(d.ASVC) }
    
    // Додаткові дані
    b.Write(d.AdditionalInfo)
    
    return b.Err()  // перевірка помилок пакетно
}
```

> 💡 **Патерн `BitsWriterBatch`**: дозволяє записувати окремі біти та перевіряти помилки тільки в кінці, замість перевірки після кожного `Write()`.

---

### 🔄 Головний парсер: `parseDescriptors`

```go
func parseDescriptors(i *astikit.BytesIterator) ([]*Descriptor, error) {
    // 1. Читання загальної довжини списку дескрипторів
    bs, _ := i.NextBytesNoCopy(2)
    length := int(uint16(bs[0]&0xf)<<8 | uint16(bs[1]))  // 12 біт
    
    // 2. Цикл по кожному дескриптору
    offsetEnd := i.Offset() + length
    for i.Offset() < offsetEnd {
        // Читання заголовка дескриптора
        bs, _ = i.NextBytesNoCopy(2)
        d := &Descriptor{
            Tag:    bs[0],
            Length: bs[1],
        }
        
        // 3. Switch за тегом → виклик специфічного парсера
        offsetDescriptorEnd := i.Offset() + int(d.Length)
        switch d.Tag {
        case DescriptorTagAVCVideo:
            d.AVCVideo, _ = newDescriptorAVCVideo(i)
        case DescriptorTagISO639LanguageAndAudioType:
            d.ISO639LanguageAndAudioType, _ = newDescriptorISO639LanguageAndAudioType(i, offsetDescriptorEnd)
        // ... інші теги ...
        default:
            d.Unknown, _ = newDescriptorUnknown(i, d.Tag, d.Length)
        }
        
        // 4. Примусовий seek до кінця дескриптора (захист від пошкоджених даних)
        i.Seek(offsetDescriptorEnd)
        
        o = append(o, d)
    }
    return o, nil
}
```

**Ключові моменти:**
1. **`NextBytesNoCopy`**: повертає посилання на внутрішній буфер — економить пам'ять, але дані можуть бути перезаписані
2. **`offsetDescriptorEnd` + `Seek`**: гарантує, що парсер не "з'їде" через невірну довжину в дескрипторі
3. **`default: Unknown`**: fallback для нових/невідомих типів дескрипторів

---

## 🎬 Ключові дескриптори для вашого пайплайну

### 📺 AVCVideo (0x28) — параметри H.264

```go
type DescriptorAVCVideo struct {
    ProfileIDC           uint8  // 66=Baseline, 77=Main, 100=High
    ConstraintSet0Flag   bool   // сумісність з older profiles
    ConstraintSet1Flag   bool
    ConstraintSet2Flag   bool
    CompatibleFlags      uint8  // бітова маска сумісності
    LevelIDC             uint8  // 30=3.0, 40=4.0, 41=4.1, 51=5.1
    AVCStillPresent      bool   // чи є still frames
    AVC24HourPictureFlag bool   // 24-годинний формат
}
```

**Використання:**
```go
// Для 1080p30 H.264 High@L4.1:
avcDesc := &astits.Descriptor{
    Tag: astits.DescriptorTagAVCVideo,
    AVCVideo: &astits.DescriptorAVCVideo{
        ProfileIDC: 100,  // High profile
        LevelIDC: 41,     // Level 4.1
        ConstraintSet0Flag: true,
        AVCStillPresent: true,
    },
}
```

### 🔊 ISO639LanguageAndAudioType (0x0A) — мовна підтримка

```go
type DescriptorISO639LanguageAndAudioType struct {
    Language []byte  // 3 байти: "eng", "ukr", "rus" (ISO 639-2/T)
    Type     uint8   // 0=undefined, 1=clean, 2=hearing impaired, 3=visual commentary
}
```

**Важлива примітка у коді:**
```go
// FIXME: according to spec there could be MULTIPLE such descriptors
// Реальні потоки можуть мати кілька мов для одного потоку!
```

**Рішення у вашому пайплайні:**
```go
// Збирати всі мовні дескриптори для потоку:
func extractLanguages(descs []*astits.Descriptor) []string {
    var langs []string
    for _, d := range descs {
        if d.ISO639LanguageAndAudioType != nil {
            langs = append(langs, string(d.ISO639LanguageAndAudioType.Language))
        }
    }
    return langs
}
```

### 📝 ShortEvent (0x4D) / ExtendedEvent (0x4E) — EPG метадані

```go
// ShortEvent: базова інформація
type DescriptorShortEvent struct {
    Language  []byte  // мова опису
    EventName []byte  // назва передачі
    Text      []byte  // короткий опис
}

// ExtendedEvent: розширений опис з парами ключ-значення
type DescriptorExtendedEvent struct {
    ISO639LanguageCode []byte
    Items []*DescriptorExtendedEventItem  // список {Description, Content}
    Text  []byte  // довгий опис
}
```

**Використання для HLS EPG:**
```go
// При генерації плейлиста — додавати метадані подій:
func addEventMetadata(playlist *HLSPlaylist, event *astits.EITDataEvent) {
    for _, desc := range event.Descriptors {
        if desc.ShortEvent != nil {
            // Додати назву передачі як коментар
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

### 🔢 MaximumBitrate (0x0E) — для адаптивного стрімінгу

```go
type DescriptorMaximumBitrate struct {
    Bitrate uint32  // у одиницях по 50 біт/сек!
}

// Парсинг:
d.Bitrate = (uint32(bs[0]&0x3f)<<16 | uint32(bs[1])<<8 | uint32(bs[2])) * 50
// ↑ верхні 2 біти байта 0 = reserved, решта 22 біти = значення
```

**Конвертація:**
```
Значення у дескрипторі: 40000
Реальний бітрейт: 40000 × 50 = 2_000_000 біт/сек = 2 Мбіт/сек
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
                ProfileIDC: config.Profile,  // напр. 100 = High
                LevelIDC:   config.Level,    // напр. 41 = 4.1
                AVCStillPresent: true,
            },
        })
    }
    
    // 🔹 MaximumBitrate для адаптивного стрімінгу
    if config.MaxBitrate > 0 {
        bitrateUnits := uint32(config.MaxBitrate / 50)  // конвертація!
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
    
    // 🔹 Мовний дескриптор (якщо аудіо)
    if config.Language != "" {
        descriptors = append(descriptors, &astits.Descriptor{
            Tag: astits.DescriptorTagISO639LanguageAndAudioType,
            ISO639LanguageAndAudioType: &astits.DescriptorISO639LanguageAndAudioType{
                Language: []byte(config.Language),  // "eng", "ukr"...
                Type: astits.AudioTypeNormal,
            },
        })
    }
    
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
                meta.Languages = append(meta.Languages, 
                    string(desc.ISO639LanguageAndAudioType.Language))
                meta.AudioType = desc.ISO639LanguageAndAudioType.Type
                
            case astits.DescriptorTagAVCVideo:
                meta.VideoProfile = desc.AVCVideo.ProfileIDC
                meta.VideoLevel = desc.AVCVideo.LevelIDC
                
            case astits.DescriptorTagMaximumBitrate:
                meta.MaxBitrate = int64(desc.MaximumBitrate.Bitrate) * 50
                
            case astits.DescriptorTagSubtitling:
                for _, item := range desc.Subtitling.Items {
                    meta.Subtitles = append(meta.Subtitles, SubtitleInfo{
                        Language: string(item.Language),
                        Type: item.Type,
                        CompositionPageID: item.CompositionPageID,
                    })
                }
            }
        }
    }
    return meta
}
```

### ✅ 3. Обробка невідомих дескрипторів

```go
// Логування нових типів для відладки:
func parseWithUnknownLogging(iter *astikit.BytesIterator, channelID string) ([]*astits.Descriptor, error) {
    descs, err := parseDescriptors(iter)
    
    for _, d := range descs {
        if d.Unknown != nil {
            log.Debugf("Channel %s: unknown descriptor tag 0x%02X (len=%d, content=%X)", 
                channelID, d.Tag, d.Length, d.Unknown.Content[:min(16, len(d.Unknown.Content))])
            
            // Опція: зберегти для подальшого аналізу
            storeUnknownDescriptor(channelID, d)
        }
    }
    
    return descs, err
}
```

### ✅ 4. Фільтрація дескрипторів за каналом

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
        
        // 🔹 EPG метадані: тільки якщо потрібні
        if (d.ShortEvent != nil || d.ExtendedEvent != nil) && !epgEnabled(channelID) {
            continue
        }
        
        filtered = append(filtered, d)
    }
    return filtered
}
```

### ✅ 5. Моніторинг дескрипторів

```go
// monitoring.Monitor — метрики для дескрипторів:
type DescriptorMetrics struct {
    DescriptorsParsed  *prometheus.CounterVec  // кількість парсингів по типу
    UnknownDescriptors *prometheus.CounterVec  // невідомі типи
    DescriptorErrors   *prometheus.CounterVec  // помилки парсингу
    LanguagesDetected  *prometheus.CounterVec  // виявлені мови
}

// У парсингу:
func parseWithMetrics(iter *astikit.BytesIterator, channelID string, metrics *DescriptorMetrics) ([]*astits.Descriptor, error) {
    descs, err := parseDescriptors(iter)
    
    for _, d := range descs {
        metrics.DescriptorsParsed.WithLabelValues(
            channelID, 
            descriptorTagName(d.Tag),  // human-readable name
        ).Inc()
        
        if d.ISO639LanguageAndAudioType != nil {
            metrics.LanguagesDetected.WithLabelValues(
                channelID,
                string(d.ISO639LanguageAndAudioType.Language),
            ).Inc()
        }
        
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

## 🧪 Тестування: стратегії валідації

### 🔹 Round-trip тест: парсинг ↔ запис

```go
func TestDescriptor_RoundTrip(t *testing.T) {
    // 1. Створити дескриптор
    original := &astits.Descriptor{
        Tag: astits.DescriptorTagAVCVideo,
        AVCVideo: &astits.DescriptorAVCVideo{
            ProfileIDC: 100,
            LevelIDC: 41,
            AVCStillPresent: true,
        },
    }
    
    // 2. Записати у байти
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    _, err := writeDescriptor(w, original)
    require.NoError(t, err)
    
    // 3. Прочитати назад
    parsed, err := parseDescriptors(astikit.NewBytesIterator(buf.Bytes()))
    require.NoError(t, err)
    require.Len(t, parsed, 1)
    
    // 4. Порівняти структури
    assert.Equal(t, original.Tag, parsed[0].Tag)
    assert.Equal(t, original.AVCVideo.ProfileIDC, parsed[0].AVCVideo.ProfileIDC)
    assert.Equal(t, original.AVCVideo.LevelIDC, parsed[0].AVCVideo.LevelIDC)
}
```

### 🔹 Тест на українську мову

```go
func TestDescriptor_UkrainianLanguage(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Записати мовний дескриптор для української
    w.Write(uint8(astits.DescriptorTagISO639LanguageAndAudioType))
    w.Write(uint8(4))                    // Length = 4 байти
    w.Write([]byte("ukr"))              // Ukrainian language code (ISO 639-2/T)
    w.Write(uint8(astits.AudioTypeNormal))
    
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

### 🔹 Fuzz-тест для стійкості

```go
func FuzzDescriptor(f *testing.F) {
    // Seed: валідні дескриптори з тестів
    for _, tc := range descriptorTestTable {
        buf := bytes.Buffer{}
        w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &buf})
        tc.bytesFunc(w)
        f.Add(buf.Bytes())
    }
    
    f.Fuzz(func(t *testing.T, b []byte) {
        // Парсимо довільні байти — не повинно панікувати!
        descs, err := parseDescriptors(astikit.NewBytesIterator(b))
        
        // Якщо парсинг успішний — пробуємо записати назад
        if err == nil {
            buf := bytes.Buffer{}
            w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &buf})
            writeDescriptors(w, descs)  // головне — немає паніки
        }
    })
}
```

Запуск: `go test -fuzz=FuzzDescriptor -fuzztime=60s`

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання прапорців | Поля читаються не за тими умовами | Перевірити порядок біт: `b&0x80` = біт 7 (старший), `b&0x01` = біт 0 (молодший) |
| Переповнення буфера | Паніка при `NextBytes(length)` | Додати перевірку: `if length > remaining { return error }` |
| Невідомі теги дескрипторів | `desc.Unknown != nil` замість конкретного типу | Додати лог з тегом: `log.Debugf("Unknown tag 0x%02X", d.Tag)` |
| Невірний розрахунок довжини | `program_info_length` не збігається | Використовувати `calcDescriptorLength()` перед записом |
| Порядок дескрипторів змінюється | Клієнти не бачать очікуваних метаданих | Зберігати порядок у slice, не використовувати map для зберігання |
| Мова кодується неправильно | "ukr" не розпізнається плеєром | Використовувати ISO 639-2/T: "ukr" (не "ua" чи "uk") |

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
if avcDesc := findDescriptorByTag(descs, astits.DescriptorTagAVCVideo); avcDesc != nil {
    // Обробити H.264 параметри
    processAVCConfig(avcDesc.AVCVideo)
}

// 4. Серіалізація з перевіркою довжини:
func writeDescriptorSafe(w *astikit.BitsWriter, desc *astits.Descriptor) error {
    // Попередній розрахунок довжини для валідації
    expectedLen := calcDescriptorLength(desc)
    if expectedLen > 255 {  // 1 байт для Length field
        return fmt.Errorf("descriptor too long: %d > 255", expectedLen)
    }
    
    _, err := writeDescriptor(w, desc)
    return err
}

// 5. Конвертація бітрейту для MaximumBitrate:
func bitrateToDescriptorUnits(bitrateBps int64) uint32 {
    // Дескриптор використовує одиниці по 50 біт/сек
    return uint32(bitrateBps / 50)
}

func descriptorUnitsToBitrate(units uint32) int64 {
    return int64(units) * 50
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
ParentalRating           | 0x55       | ⚠️ Якщо потрібні вікові обмеження
Unknown                  | будь-який  | 🔍 Логування для відладки нових типів
```

---

## 📚 Корисні посилання

- [ETSI EN 300 468: DVB descriptors specification](https://www.etsi.org/deliver/etsi_en/300400_300499/300468/)
- [ISO/IEC 13818-1: MPEG-2 Systems](https://www.iso.org/standard/61236.html)
- [astits descriptor parsing source](https://github.com/asticode/go-astits/blob/master/descriptor.go)
- [ISO 639-2 language codes](https://www.loc.gov/standards/iso639-2/php/code_list.php)

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