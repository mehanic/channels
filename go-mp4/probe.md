# 🔍 `probe.go`: Швидкий аналіз та інспекція MP4-файлів

Це **потужний модуль бібліотеки `go-mp4`**, який дозволяє **швидко "зондувати" (probe) MP4/fMP4 файли** для отримання ключової інформації без повного парсингу всього вмісту.

---

## 🎯 Коротка відповідь

> **Це "рентген" для MP4-файлів**: за один прохід ви отримуєте метадані файлу, інформацію про доріжки (кодек, роздільність, таймінги), фрагменти (для fMP4), та навіть можете знайти ключові кадри (IDR) — ідеально для валідації, індексації та адаптивного стрімінгу.

---

## 🧱 Основні структури даних

### 🔹 `ProbeInfo` — головний результат аналізу

```go
type ProbeInfo struct {
    MajorBrand       [4]byte           // 🔹 Основний бренд файлу (напр. "isom", "mp42")
    MinorVersion     uint32            // 🔹 Мінорна версія
    CompatibleBrands [][4]byte         // 🔹 Список сумісних брендів
    FastStart        bool              // 🔹 Чи файл оптимізовано для web (moov перед mdat)
    Timescale        uint32            // 🔹 Глобальна частота дискретизації
    Duration         uint64            // 🔹 Загальна тривалість у одиницях timescale
    Tracks           Tracks            // 🔹 Список доріжок (відео/аудіо/субтитри)
    Segments         Segments          // 🔹 Список фрагментів (для fMP4/DASH/HLS)
}
```

**🎯 Призначення**: Зберігати **зведену інформацію** про файл для швидкого прийняття рішень.

---

### 🔹 `Track` — інформація про одну медіа-доріжку

```go
type Track struct {
    TrackID   uint32              // 🔹 Унікальний ідентифікатор доріжки
    Timescale uint32              // 🔹 Частота дискретизації доріжки
    Duration  uint64              // 🔹 Тривалість доріжки
    Codec     Codec               // 🔹 Тип кодека: CodecAVC1, CodecMP4A, CodecUnknown
    Encrypted bool                // 🔹 Чи зашифрована доріжка (DRM)
    EditList  EditList            // 🔹 Список редагувань (пропуск, затримка)
    Samples   Samples             // 🔹 Список семплів (кадрів/аудіо-чанків)
    Chunks    Chunks              // 🔹 Список чанків (груп семплів)
    AVC       *AVCDecConfigInfo   // 🔹 Конфігурація H.264 (якщо відео)
    MP4A      *MP4AInfo           // 🔹 Конфігурація AAC (якщо аудіо)
}
```

**🎯 Призначення**: Детальна інформація про одну доріжку для декодування, синхронізації або транскодування.

---

### 🔹 `Sample` / `Chunk` — базові одиниці медіа

```go
type Sample struct {
    Size                  uint32  // 🔹 Розмір семпла у байтах
    TimeDelta             uint32  // 🔹 Різниця часу з попереднім семплом (у timescale)
    CompositionTimeOffset int64   // 🔹 Зсув часу композиції (для B-фреймів)
}

type Chunk struct {
    DataOffset      uint64  // 🔹 Зміщення даних у файлі
    SamplesPerChunk uint32  // 🔹 Кількість семплів у цьому чанку
}
```

**🎯 Призначення**: Описувати **фізичне розташування та таймінг** медіа-даних у файлі.

---

### 🔹 `Segment` — інформація про фрагмент (fMP4)

```go
type Segment struct {
    TrackID               uint32  // 🔹 ID доріжки
    MoofOffset            uint64  // 🔹 Зміщення moof боксу
    BaseMediaDecodeTime   uint64  // 🔹 Базовий час декодування
    DefaultSampleDuration uint32  // 🔹 Типова тривалість семпла
    SampleCount           uint32  // 🔹 Кількість семплів у фрагменті
    Duration              uint32  // 🔹 Загальна тривалість фрагмента
    CompositionTimeOffset int32   // 🔹 Зсув часу композиції
    Size                  uint32  // 🔹 Розмір фрагмента у байтах
}
```

**🎯 Призначення**: Критично для **HLS/DASH стрімінгу** — кожен fMP4-сегмент описується окремо.

---

### 🔹 `AVCDecConfigInfo` / `MP4AInfo` — конфігурації кодеків

```go
type AVCDecConfigInfo struct {
    ConfigurationVersion uint8   // 🔹 Версія конфігурації (завжди 1)
    Profile              uint8   // 🔹 Профіль: 66=Baseline, 77=Main, 100=High
    ProfileCompatibility uint8   // 🔹 Сумісність профілю (бітова маска)
    Level                uint8   // 🔹 Рівень: 30=3.0, 41=4.1, 51=5.1
    LengthSize           uint16  // 🔹 Розмір довжини NAL: 1, 2 або 4 байти
    Width                uint16  // 🔹 Ширина відео у пікселях
    Height               uint16  // 🔹 Висота відео у пікселях
}

type MP4AInfo struct {
    OTI          uint8   // 🔹 Object Type Indication (напр. 0x40=AAC)
    AudOTI       uint8   // 🔹 Audio OTI: 2=AAC LC, 5=SBR, 29=PS
    ChannelCount uint16  // 🔹 Кількість каналів: 1=моно, 2=стерео
}
```

**🎯 Призначення**: Параметри, необхідні для **ініціалізації декодера** без парсингу всього потоку.

---

## 🔍 Основна функція: `Probe()`

```go
func Probe(r io.ReadSeeker) (*ProbeInfo, error) {
    probeInfo := &ProbeInfo{
        Tracks:   make([]*Track, 0, 8),
        Segments: make([]*Segment, 0, 8),
    }
    
    // 🔹 Крок 1: Швидкий пошук ключових боксів
    bis, err := ExtractBoxes(r, nil, []BoxPath{
        {BoxTypeFtyp()},                    // 🔹 Тип файлу
        {BoxTypeMoov()},                    // 🔹 Метадані
        {BoxTypeMoov(), BoxTypeMvhd()},     // 🔹 Глобальні таймінги
        {BoxTypeMoov(), BoxTypeTrak()},     // 🔹 Доріжки
        {BoxTypeMoof()},                    // 🔹 Фрагменти (fMP4)
        {BoxTypeMdat()},                    // 🔹 Дані (для FastStart перевірки)
    })
    if err != nil { return nil, err }
    
    // 🔹 Крок 2: Обробка знайдених боксів
    var mdatAppeared bool
    for _, bi := range bis {
        switch bi.Type {
        case BoxTypeFtyp():
            // 🔹 Парсинг ftyp: бренди, версії
            var ftyp Ftyp
            bi.SeekToPayload(r)
            Unmarshal(r, bi.Size-bi.HeaderSize, &ftyp, bi.Context)
            probeInfo.MajorBrand = ftyp.MajorBrand
            // ... обробка compatible brands ...
            
        case BoxTypeMoov():
            // 🔹 Перевірка FastStart: moov перед mdat?
            probeInfo.FastStart = !mdatAppeared
            
        case BoxTypeMvhd():
            // 🔹 Глобальні таймінги
            var mvhd Mvhd
            bi.SeekToPayload(r)
            Unmarshal(r, bi.Size-bi.HeaderSize, &mvhd, bi.Context)
            probeInfo.Timescale = mvhd.Timescale
            probeInfo.Duration = mvhd.GetDuration()  // версія-агностичний геттер
            
        case BoxTypeTrak():
            // 🔹 Детальний аналіз доріжки
            track, err := probeTrak(r, bi)
            if err != nil { return nil, err }
            probeInfo.Tracks = append(probeInfo.Tracks, track)
            
        case BoxTypeMoof():
            // 🔹 Аналіз фрагмента (fMP4)
            segment, err := probeMoof(r, bi)
            if err != nil { return nil, err }
            probeInfo.Segments = append(probeInfo.Segments, segment)
            
        case BoxTypeMdat():
            mdatAppeared = true  // 🔹 Для FastStart перевірки
        }
    }
    
    return probeInfo, nil
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: io.ReadSeeker (файл, буфер, мережа)
│
▼
🔹 ExtractBoxes() → швидкий пошук ключових боксів (без парсингу вмісту)
│
▼
🔹 Для кожного знайденого боксу:
   ├── 🔹 ftyp → бренди, версії
   ├── 🔹 moov/mvhd → глобальні таймінги
   ├── 🔹 trak → виклик probeTrak() для детального аналізу
   ├── 🔹 moof → виклик probeMoof() для fMP4-сегментів
   └── 🔹 mdat → відмітка для FastStart перевірки
│
▼
🔹 Вихід: *ProbeInfo з усією зведеною інформацією
```

**🎯 Ключова оптимізація**: `ExtractBoxes()` знаходить **тільки метадані боксів** (офсети, розміри, типи), не парсячи їх вміст — це значно швидше за повний парсинг.

---

## 🔍 Детальний аналіз доріжки: `probeTrak()`

```go
func probeTrak(r io.ReadSeeker, bi *BoxInfo) (*Track, error) {
    track := new(Track)
    
    // 🔹 Пошук всіх необхідних боксів всередині trak
    bips, err := ExtractBoxesWithPayload(r, bi, []BoxPath{
        {BoxTypeTkhd()},                          // 🔹 Заголовок доріжки
        {BoxTypeEdts(), BoxTypeElst()},           // 🔹 Edit list
        {BoxTypeMdia(), BoxTypeMdhd()},           // 🔹 Медіа-заголовок
        // 🔹 Відео кодеки:
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStsd(), BoxTypeAvc1()},
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStsd(), BoxTypeAvc1(), BoxTypeAvcC()},
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStsd(), BoxTypeEncv()},  // 🔹 Зашифроване
        // 🔹 Аудіо кодеки:
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStsd(), BoxTypeMp4a()},
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStsd(), BoxTypeMp4a(), BoxTypeEsds()},
        // 🔹 Таблиці семплів:
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStco()},  // 🔹 32-біт офсети
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeCo64()},  // 🔹 64-біт офсети
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStts()},  // 🔹 Таймінги декодування
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeCtts()},  // 🔹 Таймінги композиції
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStsc()},  // 🔹 Семпли на чанк
        {BoxTypeMdia(), BoxTypeMinf(), BoxTypeStbl(), BoxTypeStsz()},  // 🔹 Розміри семплів
    })
    if err != nil { return nil, err }
    
    // 🔹 Збір знайдених боксів у змінні
    var tkhd *Tkhd, elst *Elst, mdhd *Mdhd, avcC *AVCDecoderConfiguration, esds *Esds, ...
    for _, bip := range bips {
        switch bip.Info.Type {
        case BoxTypeTkhd(): tkhd = bip.Payload.(*Tkhd)
        case BoxTypeAvcC(): avcC = bip.Payload.(*AVCDecoderConfiguration)
        case BoxTypeEsds(): esds = bip.Payload.(*Esds)
        // ... інші кейси ...
        }
    }
    
    // 🔹 Валідація обов'язкових боксів
    if tkhd == nil { return nil, errors.New("tkhd box not found") }
    track.TrackID = tkhd.TrackID
    
    // 🔹 Обробка edit list (опціонально)
    if elst != nil {
        for i := range elst.Entries {
            track.EditList = append(track.EditList, &EditListEntry{
                MediaTime:       elst.GetMediaTime(i),
                SegmentDuration: elst.GetSegmentDuration(i),
            })
        }
    }
    
    // 🔹 Обробка медіа-заголовка
    if mdhd == nil { return nil, errors.New("mdhd box not found") }
    track.Timescale = mdhd.Timescale
    track.Duration = mdhd.GetDuration()
    
    // 🔹 Обробка відео-конфігурації (H.264)
    if avc1 != nil && avcC != nil {
        track.AVC = &AVCDecConfigInfo{
            Profile:              avcC.Profile,
            Level:                avcC.Level,
            LengthSize:           uint16(avcC.LengthSizeMinusOne) + 1,  // 🔹 Конвертація: 0→1, 1→2, 2→4
            Width:                avc1.Width,
            Height:               avc1.Height,
        }
    }
    
    // 🔹 Обробка аудіо-конфігурації (AAC)
    if audioSampleEntry != nil && esds != nil {
        oti, audOTI, err := detectAACProfile(esds)  // 🔹 Складний парсинг AudioSpecificConfig
        if err != nil { return nil, err }
        track.MP4A = &MP4AInfo{
            OTI:          oti,
            AudOTI:       audOTI,
            ChannelCount: audioSampleEntry.ChannelCount,
        }
    }
    
    // 🔹 Побудова списку чанків (stco/co64)
    if stco != nil {
        for _, offset := range stco.ChunkOffset {
            track.Chunks = append(track.Chunks, &Chunk{DataOffset: uint64(offset)})
        }
    } else if co64 != nil {
        for _, offset := range co64.ChunkOffset {
            track.Chunks = append(track.Chunks, &Chunk{DataOffset: offset})
        }
    } else {
        return nil, errors.New("stco/co64 box not found")
    }
    
    // 🔹 Побудова списку семплів (stts)
    if stts == nil { return nil, errors.New("stts box not found") }
    for _, entry := range stts.Entries {
        for i := uint32(0); i < entry.SampleCount; i++ {
            track.Samples = append(track.Samples, &Sample{TimeDelta: entry.SampleDelta})
        }
    }
    
    // 🔹 Прив'язка семплів до чанків (stsc)
    if stsc == nil { return nil, errors.New("stsc box not found") }
    for si, entry := range stsc.Entries {
        end := uint32(len(track.Chunks))
        if si != len(stsc.Entries)-1 {
            end = stsc.Entries[si+1].FirstChunk - 1
        }
        for ci := entry.FirstChunk - 1; ci < end; ci++ {
            track.Chunks[ci].SamplesPerChunk = entry.SamplesPerChunk
        }
    }
    
    // 🔹 Додавання composition time offset (ctts) для B-фреймів
    if ctts != nil {
        var si uint32
        for ci, entry := range ctts.Entries {
            for i := uint32(0); i < entry.SampleCount; i++ {
                if si >= uint32(len(track.Samples)) { break }
                track.Samples[si].CompositionTimeOffset = ctts.GetSampleOffset(ci)
                si++
            }
        }
    }
    
    // 🔹 Додавання розмірів семплів (stsz)
    if stsz != nil {
        for i := 0; i < len(stsz.EntrySize) && i < len(track.Samples); i++ {
            track.Samples[i].Size = stsz.EntrySize[i]
        }
    }
    
    return track, nil
}
```

**🎯 Ключова складність**: `probeTrak()` **об'єднує інформацію з 10+ різних боксів** (`tkhd`, `mdhd`, `stts`, `stsc`, `stsz`, `ctts`, `stco`/`co64`, `avcC`, `esds`...) у єдину зручну структуру `Track`.

---

## 🔍 Детальний аналіз фрагмента: `probeMoof()`

```go
func probeMoof(r io.ReadSeeker, bi *BoxInfo) (*Segment, error) {
    // 🔹 Пошук ключових боксів всередині moof
    bips, err := ExtractBoxesWithPayload(r, bi, []BoxPath{
        {BoxTypeTraf(), BoxTypeTfhd()},  // 🔹 Заголовок фрагмента доріжки
        {BoxTypeTraf(), BoxTypeTfdt()},  // 🔹 Час декодування
        {BoxTypeTraf(), BoxTypeTrun()},  // 🔹 Дані семплів
    })
    if err != nil { return nil, err }
    
    var tfhd *Tfhd, tfdt *Tfdt, trun *Trun
    segment := &Segment{MoofOffset: bi.Offset}
    
    for _, bip := range bips {
        switch bip.Info.Type {
        case BoxTypeTfhd(): tfhd = bip.Payload.(*Tfhd)
        case BoxTypeTfdt(): tfdt = bip.Payload.(*Tfdt)
        case BoxTypeTrun(): trun = bip.Payload.(*Trun)
        }
    }
    
    if tfhd == nil { return nil, errors.New("tfhd not found") }
    segment.TrackID = tfhd.TrackID
    segment.DefaultSampleDuration = tfhd.DefaultSampleDuration
    
    // 🔹 Базовий час декодування
    if tfdt != nil {
        segment.BaseMediaDecodeTime = tfdt.GetBaseMediaDecodeTime()  // версія-агностичний геттер
    }
    
    // 🔹 Обробка trun: таймінги, розміри, CTS offset
    if trun != nil {
        segment.SampleCount = trun.SampleCount
        
        // 🔹 Розрахунок тривалості
        if trun.CheckFlag(0x000100) {  // 🔹 sample-duration-present
            segment.Duration = 0
            for ei := range trun.Entries {
                segment.Duration += trun.Entries[ei].SampleDuration
            }
        } else {
            segment.Duration = tfhd.DefaultSampleDuration * segment.SampleCount
        }
        
        // 🔹 Розрахунок розміру
        if trun.CheckFlag(0x000200) {  // 🔹 sample-size-present
            segment.Size = 0
            for ei := range trun.Entries {
                segment.Size += trun.Entries[ei].SampleSize
            }
        } else {
            segment.Size = tfhd.DefaultSampleSize * segment.SampleCount
        }
        
        // 🔹 Розрахунок composition time offset
        var duration uint32
        for ei := range trun.Entries {
            offset := int32(duration) + int32(trun.GetSampleCompositionTimeOffset(ei))
            if ei == 0 || offset < segment.CompositionTimeOffset {
                segment.CompositionTimeOffset = offset
            }
            // ... оновлення duration ...
        }
    }
    
    return segment, nil
}
```

**🎯 Призначення**: Критично для **HLS/DASH** — кожен fMP4-сегмент має власні таймінги, які можуть відрізнятися від глобальних.

---

## 🔍 Спеціальні функції

### 🔹 `detectAACProfile()` — визначення профілю AAC

```go
func detectAACProfile(esds *Esds) (oti, audOTI uint8, err error) {
    // 🔹 Пошук DecoderConfigDescriptor (Tag=0x04)
    configDscr := findDescriptorByTag(esds.Descriptors, DecoderConfigDescrTag)
    if configDscr == nil || configDscr.DecoderConfigDescriptor == nil {
        return 0, 0, nil
    }
    
    // 🔹 Перевірка ObjectTypeIndication
    if configDscr.DecoderConfigDescriptor.ObjectTypeIndication != 0x40 {
        // 🔹 Не AAC → повертаємо OTI як є
        return configDscr.DecoderConfigDescriptor.ObjectTypeIndication, 0, nil
    }
    
    // 🔹 Пошук DecSpecificInfo (Tag=0x05) з AudioSpecificConfig
    specificDscr := findDescriptorByTag(esds.Descriptors, DecSpecificInfoTag)
    if specificDscr == nil {
        return 0, 0, errors.New("DecoderSpecificationInfoDescriptor not found")
    }
    
    // 🔹 Бітовий парсинг AudioSpecificConfig
    r := bitio.NewReader(bytes.NewReader(specificDscr.Data))
    
    // 🔹 audio object type (5 біт, або 11 біт якщо 0x1F)
    audioObjectType, read, err := getAudioObjectType(r)
    
    // 🔹 sampling frequency index (4 біти)
    samplingFrequencyIndex, err := r.ReadBits(4)
    if samplingFrequencyIndex[0] == 0x0f {
        // 🔹 Explicit frequency: 24 біти
        _, err = r.ReadBits(24)
    }
    
    // 🔹 SBR/PS detection для HE-AAC / HE-AAC v2
    if audioObjectType == 2 && remaining >= 20 {
        // 🔹 Перевірка syncExtensionType = 0x2b7
        // 🔹 Якщо extAudioObjectType = 5 (SBR) або 22 (PS)
        // 🔹 Читаємо sbr/ps прапорці
        if sbr[0] != 0 {
            if extAudioObjectType == 5 {
                // 🔹 HE-AAC (AAC+SBR)
                return 0x40, 5, nil
            }
            // 🔹 Перевірка PS для HE-AAC v2
            if ps[0] != 0 {
                return 0x40, 29, nil  // 🔹 AAC+SBR+PS
            }
        }
    }
    
    return 0x40, audioObjectType, nil
}
```

**🎯 Призначення**: Визначити **точний профіль AAC** (LC, SBR, PS) для сумісності з декодерами.

---

### 🔹 `FindIDRFrames()` — пошук ключових кадрів (IDR)

```go
func FindIDRFrames(r io.ReadSeeker, trackInfo *TrackInfo) ([]int, error) {
    if trackInfo.AVC == nil { return nil, nil }  // 🔹 Тільки для H.264
    
    lengthSize := uint32(trackInfo.AVC.LengthSize)  // 🔹 1, 2 або 4 байти
    var si int
    idxs := make([]int, 0, 8)
    
    for _, chunk := range trackInfo.Chunks {
        end := si + int(chunk.SamplesPerChunk)
        dataOffset := chunk.DataOffset
        
        for ; si < end && si < len(trackInfo.Samples); si++ {
            sample := trackInfo.Samples[si]
            if sample.Size == 0 { continue }
            
            // 🔹 Парсинг NAL units всередині семпла
            for nalOffset := uint32(0); nalOffset+lengthSize+1 <= sample.Size; {
                // 🔹 Читання довжини NAL
                if _, err := r.Seek(int64(dataOffset+uint64(nalOffset)), io.SeekStart); err != nil {
                    return nil, err
                }
                data := make([]byte, lengthSize+1)
                io.ReadFull(r, data)
                
                // 🔹 Декодування довжини (big-endian)
                var length uint32
                for i := 0; i < int(lengthSize); i++ {
                    length = (length << 8) + uint32(data[i])
                }
                
                // 🔹 Перевірка типу NAL: 5 = IDR (ключовий кадр)
                nalHeader := data[lengthSize]
                nalType := nalHeader & 0x1f
                if nalType == 5 {
                    idxs = append(idxs, si)  // 🔹 Запам'ятовуємо індекс семпла
                    break
                }
                nalOffset += lengthSize + length
            }
            dataOffset += uint64(sample.Size)
        }
    }
    return idxs, nil
}
```

**🎯 Призначення**: Знайти **ключові кадри (IDR)** для швидкого seek, генерації HLS-плейлистів з ключовими точками, або адаптивного стрімінгу.

---

### 🔹 `GetBitrate()` / `GetMaxBitrate()` — розрахунок бітрейту

```go
func (samples Samples) GetBitrate(timescale uint32) uint64 {
    var totalSize uint64
    var totalDuration uint64
    for _, sample := range samples {
        totalSize += uint64(sample.Size)
        totalDuration += uint64(sample.TimeDelta)
    }
    if totalDuration == 0 { return 0 }
    // 🔹 Формула: (байти * 8 * timescale) / тривалість = біт/сек
    return 8 * totalSize * uint64(timescale) / totalDuration
}

func (samples Samples) GetMaxBitrate(timescale uint32, timeDelta uint64) uint64 {
    // 🔹 Скользяче вікно для пошуку пікового бітрейту
    // ... складний алгоритм з двома вказівниками (begin/end) ...
    return maxBitrate
}
```

**🎯 Призначення**: Розрахунок **середнього та пікового бітрейту** для адаптивного стрімінгу, валідації обмежень мережі, або оптимізації енкодингу.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Швидка валідація вхідного fMP4-сегмента

```go
func validateIncomingSegment(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 Крок 1: Швидкий пробінг
    info, err := mp4.Probe(f)
    if err != nil {
        return fmt.Errorf("failed to probe file: %w", err)
    }
    
    // 🔹 Крок 2: Перевірка FastStart (для web-оптимізації)
    if !info.FastStart {
        log.Printf("⚠️  File not optimized for web: moov after mdat")
    }
    
    // 🔹 Крок 3: Перевірка відео-доріжки
    var videoTrack *mp4.Track
    for _, t := range info.Tracks {
        if t.Codec == mp4.CodecAVC1 {
            videoTrack = t
            break
        }
    }
    if videoTrack == nil {
        return fmt.Errorf("no H.264 video track found")
    }
    
    // 🔹 Крок 4: Перевірка роздільності
    if videoTrack.AVC == nil {
        return fmt.Errorf("missing AVC configuration")
    }
    if videoTrack.AVC.Width < 640 || videoTrack.AVC.Height < 360 {
        return fmt.Errorf("resolution too low: %dx%d", 
            videoTrack.AVC.Width, videoTrack.AVC.Height)
    }
    
    // 🔹 Крок 5: Перевірка бітрейту
    bitrate := videoTrack.Samples.GetBitrate(videoTrack.Timescale)
    if bitrate > 5_000_000 {  // 5 Mbps limit
        log.Printf("⚠️  High bitrate: %d bps", bitrate)
    }
    
    // 🔹 Крок 6: Перевірка фрагментів (для HLS)
    if len(info.Segments) == 0 {
        log.Printf("ℹ️  File is not fragmented — consider using fMP4 for HLS")
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Генерація HLS-плейлиста з ключовими кадрами

```go
func generateHLSPlaylistWithKeyframes(filePath string) (string, error) {
    f, err := os.Open(filePath)
    if err != nil { return "", err }
    defer f.Close()
    
    // 🔹 Пробінг файлу
    info, err := mp4.Probe(f)
    if err != nil { return "", err }
    
    // 🔹 Пошук відео-доріжки
    var videoTrack *mp4.Track
    for _, t := range info.Tracks {
        if t.Codec == mp4.CodecAVC1 {
            videoTrack = t
            break
        }
    }
    if videoTrack == nil {
        return "", fmt.Errorf("no video track")
    }
    
    // 🔹 Пошук ключових кадрів (IDR)
    keyframes, err := mp4.FindIDRFrames(f, videoTrack)
    if err != nil { return "", err }
    
    // 🔹 Генерація плейлиста
    var sb strings.Builder
    sb.WriteString("#EXTM3U\n")
    sb.WriteString("#EXT-X-VERSION:6\n")
    sb.WriteString("#EXT-X-TARGETDURATION:4\n")
    
    timescale := videoTrack.Timescale
    var currentTime uint64
    
    for _, kfIdx := range keyframes {
        if kfIdx >= len(videoTrack.Samples) { break }
        
        // 🔹 Розрахунок часу ключового кадру
        for i := 0; i < kfIdx; i++ {
            currentTime += uint64(videoTrack.Samples[i].TimeDelta)
        }
        extinf := float64(currentTime) / float64(timescale)
        
        // 🔹 Додавання сегмента у плейлист
        sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", extinf))
        sb.WriteString(fmt.Sprintf("segment_%06d.ts\n", kfIdx))
    }
    
    sb.WriteString("#EXT-X-ENDLIST\n")
    return sb.String(), nil
}
```

---

### 🔹 Приклад 3: Адаптивний стрімінг на основі бітрейту

```go
func selectOptimalBitrate(segments mp4.Segments, trackID uint32, 
                         timescale uint32, maxBitrate uint64) *mp4.Segment {
    
    var best *mp4.Segment
    var bestScore float64
    
    for _, seg := range segments {
        if seg.TrackID != trackID { continue }
        
        // 🔹 Розрахунок бітрейту сегмента
        bitrate := 8 * uint64(seg.Size) * uint64(timescale) / uint64(seg.Duration)
        
        // 🔹 Score: чим ближче до maxBitrate без перевищення — тим краще
        if bitrate <= maxBitrate {
            score := float64(bitrate) / float64(maxBitrate)
            if score > bestScore {
                bestScore = score
                best = seg
            }
        }
    }
    
    return best  // 🔹 Повертаємо оптимальний сегмент або nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Ігнорування `FastStart` | Веб-плеєри довго завантажують початок відео | Перевіряйте `info.FastStart` і попереджайте користувачів |
| Неправильна обробка `LengthSizeMinusOne` | Неправильний парсинг NAL units → помилки декодування | Пам'ятайте: `LengthSize = LengthSizeMinusOne + 1` (0→1, 1→2, 2→4) |
| Забути перевірку `ctts` для B-фреймів | Десинхронізація аудіо/відео при наявності B-фреймів | Завжди обробляйте `CompositionTimeOffset` з `ctts` |
| Неправильний розрахунок бітрейту | Перевищення лімітів мережі → буферизація | Використовуйте `GetMaxBitrate()` з розумним `timeDelta` (напр. 1 секунда) |
| Ігнорування `Encrypted` прапорця | Спроба декодувати DRM-контент → помилка | Перевіряйте `track.Encrypted` перед спробою декодування |

---

## 📋 Чекліст для вашого проекту

```
[ ] При прийомі нових сегментів:
    • Викликайте Probe() для швидкої валідації структури
    • Перевіряйте FastStart для web-оптимізації
    • Логувайте codec, resolution, bitrate для моніторингу

[ ] Для HLS-генерації:
    • Використовуйте FindIDRFrames() для пошуку ключових точок
    • Розраховуйте тривалість сегментів через timescale
    • Додавайте #EXT-X-KEY якщо track.Encrypted=true

[ ] Для адаптивного стрімінгу:
    • Розраховуйте бітрейт через GetBitrate() / GetMaxBitrate()
    • Фільтруйте сегменти за trackID для мульти-доріжкових файлів
    • Обирайте оптимальний бітрейт на основі мережевих умов

[ ] Для дебагу:
    • Логуйте зведену інформацію: log.Printf("📊 %dx%d@%dbps", w, h, bitrate)
    • Перевіряйте наявність обов'язкових боксів: tkhd, mdhd, stts, stsc
    • Використовуйте String() для бокс-типів у логах: log.Printf("📦 %s", boxType)

[ ] Для тестування:
    • Створюйте тестові MP4 з різними конфігураціями (H.264/AAC, різні роздільності)
    • Перевіряйте коректність розрахунку таймінгів та бітрейту
    • Тестуйте edge cases: порожні файли, пошкоджені заголовки, DRM
```

---

## 🎯 Висновок

> **`probe.go` — це ваш "швидкий сканер" для MP4-файлів**, який забезпечує:
> • ✅ Швидкий аналіз без повного парсингу (тільки метадані боксів)
> • ✅ Зведену інформацію про файл, доріжки, фрагменти у зручних структурах
> • ✅ Детальний парсинг кодекових конфігурацій (H.264, AAC) для ініціалізації декодерів
> • ✅ Пошук ключових кадрів (IDR) для seek та адаптивного стрімінгу
> • ✅ Розрахунок бітрейту для оптимізації мережевої передачі

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Миттєва валідація вхідних сегментів без затримки обробки
- 🔍 Точна інформація про кодек, роздільність, таймінги для адаптації стріму
- 🎯 Швидкий пошук ключових кадрів для генерації HLS-плейлистів
- 📊 Моніторинг бітрейту для оптимізації під різні мережеві умови
- 🛡️ Безпечна обробка зашифрованих потоків через прапорець `Encrypted`

Потребуєте допомоги з інтеграцією `Probe()` у ваш конвеєр прийому сегментів або з генерацією HLS-плейлистів на основі ключових кадрів? Напишіть — покажу готовий код для вашого сценарію! 🚀🔍