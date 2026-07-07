# 📦 Глибокий розбір: `fmp4io.SampleTable` — Таблиці семплів для MP4/fMP4

Цей файл — **реалізація атомів `stbl` (Sample Table) та його підатомів** для опису структури медіа-семплів у форматі MP4. Ці таблиці є критичними для демуксингу, дозволяючи знаходити дані семплів, їх таймінги, розміри та ключові кадри.

---

## 🗺️ Архітектурна схема SampleTable

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.SampleTable — Sample Tables │
├────────────────────────────────────────┤
│                                         │
│  🔑 Основні атоми таблиці:              │
│  • stbl (SampleTable) — кореневий контейнер│
│  │  ├─ stsd (SampleDesc) — опис кодека │
│  │  ├─ stts (TimeToSample) — DTS розрахунок│
│  │  ├─ ctts (CompositionOffset) — PTS розрахунок│
│  │  ├─ stsc (SampleToChunk) — мапінг семплів у чанки│
│  │  ├─ stss (SyncSample) — індекси ключових кадрів│
│  │  ├─ stco (ChunkOffset) — позиції чанків у файлі│
│  │  └─ stsz (SampleSize) — розміри семплів│
│                                         │
│  🔄 Ієрархія:                            │
│  moov → trak → mdia → minf → stbl    │
│                                         │
│  📡 Призначення:                        │
│  • Пошук даних семплів у файлі        │
│  • Розрахунок таймінгів (DTS/PTS)     │
│  • Визначення ключових кадрів для seek│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. SampleTable (stbl) — кореневий контейнер таблиць

### 🔧 Структура та призначення:

```go
type SampleTable struct {
    SampleDesc        *SampleDesc        // stsd: опис кодеків (AVC1, MP4A, Opus)
    TimeToSample      *TimeToSample      // stts: розрахунок DTS
    CompositionOffset *CompositionOffset // ctts: розрахунок PTS = DTS + offset
    SampleToChunk     *SampleToChunk     // stsc: мапінг семплів у чанки
    SyncSample        *SyncSample        // stss: індекси ключових кадрів
    ChunkOffset       *ChunkOffset       // stco: позиції чанків у файлі
    SampleSize        *SampleSize        // stsz: розміри семплів
    AtomPos                            // offset/size у файлі
}
```

### 🔍 Призначення stbl атому:

```
stbl (Sample Table) містить ВСІ таблиці для навігації по семплах:

• Без stbl неможливо знайти дані семплів у файлі!
• Критичний для демуксингу: пошук даних за індексом семплу
• Дозволяє ефективний seek до будь-якого моменту у файлі

Структура:
  stbl (SampleTable)
  ├─ stsd (SampleDesc) — опис кодеків
  │  ├─ avc1 — H.264 video descriptor
  │  ├─ mp4a — AAC audio descriptor
  │  └─ Opus — Opus audio descriptor
  ├─ stts (TimeToSample) — розрахунок DTS
  │  └─ Entries: [{Count, Duration}...]
  ├─ ctts (CompositionOffset) — розрахунок PTS
  │  └─ Entries: [{Count, Offset}...]
  ├─ stsc (SampleToChunk) — мапінг семплів у чанки
  │  └─ Entries: [{FirstChunk, SamplesPerChunk, SampleDescId}...]
  ├─ stss (SyncSample) — індекси ключових кадрів ⭐
  │  └─ Entries: [sample_index_1, sample_index_2, ...]
  ├─ stco (ChunkOffset) — позиції чанків у файлі
  │  └─ Entries: [chunk_offset_1, chunk_offset_2, ...]
  └─ stsz (SampleSize) — розміри семплів
     ├─ SampleSize: 0 = змінний розмір, >0 = фіксований
     └─ Entries: [size_1, size_2, ...] (тільки якщо SampleSize=0)
```

### ✅ Ваш use-case**: пошук даних семплу за індексом

```go
// FindSampleData — пошук позиції та розміру даних семплу за індексом
func FindSampleData(stbl *fmp4io.SampleTable, sampleIndex int) (offset uint64, size uint32, err error) {
    if stbl == nil {
        return 0, 0, fmt.Errorf("nil sample table")
    }
    
    // 1. Отримання розміру семплу
    if stbl.SampleSize == nil {
        return 0, 0, fmt.Errorf("missing stsz table")
    }
    
    if stbl.SampleSize.SampleSize != 0 {
        // Фіксований розмір для всіх семплів
        size = stbl.SampleSize.SampleSize
    } else {
        // Змінний розмір: читаємо з Entries
        if sampleIndex >= len(stbl.SampleSize.Entries) {
            return 0, 0, fmt.Errorf("sample index %d out of range", sampleIndex)
        }
        size = stbl.SampleSize.Entries[sampleIndex]
    }
    
    // 2. Знаходження чанку, що містить цей семпл
    chunkIndex, sampleIndexInChunk, err := findChunkForSample(stbl.SampleToChunk, sampleIndex)
    if err != nil {
        return 0, 0, fmt.Errorf("find chunk: %w", err)
    }
    
    // 3. Отримання позиції чанку у файлі
    if stbl.ChunkOffset == nil {
        return 0, 0, fmt.Errorf("missing stco table")
    }
    if chunkIndex >= len(stbl.ChunkOffset.Entries) {
        return 0, 0, fmt.Errorf("chunk index %d out of range", chunkIndex)
    }
    chunkOffset := uint64(stbl.ChunkOffset.Entries[chunkIndex])
    
    // 4. Розрахунок зміщення семплу у чанку
    sampleOffsetInChunk := calculateSampleOffsetInChunk(stbl, chunkIndex, sampleIndexInChunk)
    
    offset = chunkOffset + sampleOffsetInChunk
    return offset, size, nil
}

// findChunkForSample — пошук чанку за індексом семплу (спрощено)
func findChunkForSample(stsc *fmp4io.SampleToChunk, sampleIndex int) (chunkIndex, sampleIndexInChunk int, err error) {
    if stsc == nil {
        return 0, 0, fmt.Errorf("missing stsc table")
    }
    
    // Лінійний пошук у стиснутій таблиці stsc
    sampleCount := 0
    for i, entry := range stsc.Entries {
        nextFirstChunk := uint32(len(stsc.Entries))
        if i+1 < len(stsc.Entries) {
            nextFirstChunk = stsc.Entries[i+1].FirstChunk
        }
        
        for chunk := entry.FirstChunk; chunk < nextFirstChunk; chunk++ {
            samplesInChunk := int(entry.SamplesPerChunk)
            if sampleIndex < sampleCount+samplesInChunk {
                return int(chunk) - 1, sampleIndex - sampleCount, nil
            }
            sampleCount += samplesInChunk
        }
    }
    
    return 0, 0, fmt.Errorf("sample index %d not found", sampleIndex)
}
```

---

## 🔑 2. SampleDesc (stsd) — опис кодеків

### 🔧 Структура та призначення:

```go
type SampleDesc struct {
    Version  uint8          // версія формату (зазвичай 1)
    AVC1Desc *AVC1Desc      // опис кодека H.264 (якщо є)
    MP4ADesc *MP4ADesc      // опис кодека AAC (якщо є)
    OpusDesc *OpusSampleEntry // опис кодека Opus (якщо є)
    Unknowns []Atom         // невідомі дочірні атоми для сумісності
    AtomPos                 // offset/size у файлі
}
```

### 🔍 Призначення stsd атому:

```
stsd (Sample Description) містить опис кодеків для треку:

• Визначає який кодек використовується (H.264, AAC, Opus)
• Містить конфігурацію декодера (AVCDecoderConfigurationRecord, тощо)
• Критичний для ініціалізації декодера на клієнті

Структура:
  stsd (SampleDesc)
  ├─ Version: 1
  ├─ EntryCount: 1 (зазвичай один опис на трек)
  └─ Codec Descriptor (один з):
     ├─ avc1 (AVC1Desc) — H.264 video
     │  └─ avcC — AVCDecoderConfigurationRecord
     ├─ mp4a (MP4ADesc) — AAC audio
     │  └─ esds — MPEG-4 Stream Descriptor
     └─ Opus (OpusSampleEntry) — Opus audio
        └─ dOps — OpusSpecificConfiguration
```

### ✅ Ваш use-case**: ініціалізація декодера з stsd

```go
// InitDecoderFromSampleDesc — ініціалізація кодека з SampleDesc
func InitDecoderFromSampleDesc(stsd *fmp4io.SampleDesc) (av.CodecData, error) {
    if stsd == nil {
        return nil, fmt.Errorf("nil sample description")
    }
    
    // Перевірка типів кодеків
    if stsd.AVC1Desc != nil {
        // H.264: отримання AVCDecoderConfigurationRecord
        if stsd.AVC1Desc.Conf == nil || len(stsd.AVC1Desc.Conf.Data) == 0 {
            return nil, fmt.Errorf("missing AVC config")
        }
        return h264parser.NewCodecDataFromAVCDecoderConfRecord(stsd.AVC1Desc.Conf.Data)
        
    } else if stsd.MP4ADesc != nil {
        // AAC: отримання AudioSpecificConfig з esds
        if stsd.MP4ADesc.Conf == nil || stsd.MP4ADesc.Conf.StreamDescriptor == nil {
            return nil, fmt.Errorf("missing AAC config")
        }
        desc := stsd.MP4ADesc.Conf.StreamDescriptor
        if desc.DecoderConfig == nil || desc.DecoderConfig.DecSpecificInfo == nil {
            return nil, fmt.Errorf("missing DecoderSpecificInfo")
        }
        return aacparser.NewCodecDataFromMPEG4AudioConfigBytes(desc.DecoderConfig.DecSpecificInfo)
        
    } else if stsd.OpusDesc != nil {
        // Opus: отримання конфігурації з dOps
        if stsd.OpusDesc.Conf == nil {
            return nil, fmt.Errorf("missing Opus config")
        }
        return opusparser.NewCodecDataFromOpusConfig(stsd.OpusDesc.Conf)
    }
    
    return nil, fmt.Errorf("no supported codec found in sample description")
}
```

---

## 🔑 3. TimeToSample (stts) — розрахунок DTS

### 🔧 Структура та призначення:

```go
type TimeToSample struct {
    Version uint8              // версія формату (зазвичай 0)
    Flags   uint32             // бітові прапорці (зазвичай 0)
    Entries []TimeToSampleEntry // ⭐ стиснута таблиця: [{Count, Duration}...]
    AtomPos
}

type TimeToSampleEntry struct {
    Count    uint32  // кількість семплів з цією тривалістю
    Duration uint32  // тривалість семплу у ticks
}
```

### 🔍 Призначення stts таблиці:

```
stts (Time-To-Sample) використовується для розрахунку DTS (Decoding Time Stamp):

• Замість зберігання тривалості для кожного семплу окремо,
  використовується стиснута таблиця: {Count, Duration}
• Це економить пам'ять коли багато семплів мають однакову тривалість

Приклад:
  Entries = [
    {Count: 25, Duration: 1000},  // 25 семплів по 1000 ticks
    {Count: 1, Duration: 1001},   // 1 семпл 1001 ticks (корекція)
  ]
  
  Це означає:
  • Семпли 0-24: тривалість 1000 ticks кожен
  • Семпл 25: тривалість 1001 ticks

Розрахунок DTS для семплу:
    func CalculateDTS(stts *TimeToSample, sampleIndex int, timeScale int64) time.Duration {
        accumulated := uint64(0)
        currentSample := 0
        
        for _, entry := range stts.Entries {
            if sampleIndex < currentSample+int(entry.Count) {
                // Знайдено запис: розрахунок точного DTS
                offset := sampleIndex - currentSample
                accumulated += uint64(offset) * uint64(entry.Duration)
                break
            }
            accumulated += uint64(entry.Count) * uint64(entry.Duration)
            currentSample += int(entry.Count)
        }
        
        return time.Duration(accumulated) * time.Second / time.Duration(timeScale)
    }
```

### ✅ Ваш use-case**: розрахунок часу відтворення семплу

```go
// GetSamplePresentationTime — розрахунок PTS для семплу
func GetSamplePresentationTime(stbl *fmp4io.SampleTable, sampleIndex int, timeScale int64) (dts, pts time.Duration, err error) {
    if stbl == nil || stbl.TimeToSample == nil {
        return 0, 0, fmt.Errorf("missing stts table")
    }
    
    // 1. Розрахунок DTS через stts
    dts = CalculateDTS(stbl.TimeToSample, sampleIndex, timeScale)
    
    // 2. Додавання composition offset через ctts (якщо є)
    if stbl.CompositionOffset != nil && len(stbl.CompositionOffset.Entries) > 0 {
        cts := CalculateCTS(stbl.CompositionOffset, sampleIndex, timeScale)
        pts = dts + cts
    } else {
        pts = dts  // без B-frames: PTS = DTS
    }
    
    return dts, pts, nil
}

// CalculateCTS — розрахунок composition time offset
func CalculateCTS(ctts *fmp4io.CompositionOffset, sampleIndex int, timeScale int64) time.Duration {
    accumulated := uint64(0)
    currentSample := 0
    
    for _, entry := range ctts.Entries {
        if sampleIndex < currentSample+int(entry.Count) {
            offset := sampleIndex - currentSample
            accumulated += uint64(offset) * uint64(entry.Offset)
            break
        }
        accumulated += uint64(entry.Count) * uint64(entry.Offset)
        currentSample += int(entry.Count)
    }
    
    return time.Duration(accumulated) * time.Second / time.Duration(timeScale)
}
```

---

## 🔑 4. SyncSample (stss) — індекси ключових кадрів

### 🔧 Структура та призначення:

```go
type SyncSample struct {
    Version uint8    // версія формату (зазвичай 0)
    Flags   uint32   // бітові прапорці (зазвичай 0)
    Entries []uint32 // ⭐ індекси ключових кадрів (1-based indexing!)
    AtomPos
}
```

### 🔍 Призначення stss таблиці:

```
stss (Sync Sample) містить індекси ключових кадрів (синхронізаційних семплів):

• Ключовий кадр = незалежний кадр, з якого можна почати декодування
• Критичний для seek: можна шукати тільки до ключових кадрів
• Для відео: IDR frames у H.264/H.265
• Для аудіо: зазвичай відсутня (всі аудіо семпли незалежні)

⚠️ Важливо: індекси у Entries є 1-based (починаються з 1), не 0-based!

Приклад:
  Entries = [1, 31, 61, 91]  // ключові кадри на позиціях 1, 31, 61, 91
  
  Це означає:
  • Семпл 0 (перший) — ключовий кадр (індекс 1 у таблиці)
  • Семпл 30 — ключовий кадр (індекс 31 у таблиці)
  • тощо
```

### ✅ Ваш use-case**: пошук найближчого ключового кадру

```go
// FindNearestKeyFrame — пошук індексу найближчого ключового кадру
func FindNearestKeyFrame(stss *fmp4io.SyncSample, targetSampleIndex int) int {
    if stss == nil || len(stss.Entries) == 0 {
        // Якщо немає stss таблиці — припускаємо що всі семпли ключові
        // (напр. для аудіо)
        return targetSampleIndex
    }
    
    // Бінарний пошук у відсортованому масиві індексів
    // Перетворення 1-based → 0-based для порівняння
    left, right := 0, len(stss.Entries)-1
    bestMatch := int(stss.Entries[0]) - 1  // перший ключовий кадр
    
    for left <= right {
        mid := (left + right) / 2
        keyFrameIndex := int(stss.Entries[mid]) - 1  // 1-based → 0-based
        
        if keyFrameIndex == targetSampleIndex {
            return keyFrameIndex  // точний збіг
        } else if keyFrameIndex < targetSampleIndex {
            bestMatch = keyFrameIndex  // запам'ятовуємо як кандидат
            left = mid + 1
        } else {
            right = mid - 1
        }
    }
    
    return bestMatch  // найближчий ключовий кадр ДО цільового індексу
}

// Використання для реалізації seek:
targetIndex := 150  // хочемо шукати до семплу 150
keyFrameIndex := FindNearestKeyFrame(stbl.SyncSample, targetIndex)
log.Printf("Seek to key frame at index %d (target was %d)", keyFrameIndex, targetIndex)

// Починаємо декодування з keyFrameIndex
```

---

## 🔑 5. SampleSize (stsz) — розміри семплів

### 🔧 Структура та призначення:

```go
type SampleSize struct {
    Version    uint8    // версія формату (зазвичай 0)
    Flags      uint32   // бітові прапорці (зазвичай 0)
    SampleSize uint32   // ⭐ фіксований розмір для всіх семплів (0 = змінний)
    Entries    []uint32 // ⭐ розміри окремих семплів (тільки якщо SampleSize=0)
    AtomPos
}
```

### 🔍 Призначення stsz таблиці:

```
stsz (Sample Size) визначає розміри медіа-семплів:

• Якщо SampleSize > 0: всі семпли мають однаковий розмір
  → Entries не використовується, економить пам'ять
• Якщо SampleSize = 0: семпли мають змінний розмір
  → Entries містить розмір для кожного семплу окремо

Приклади:
• Аудіо (AAC): зазвичай фіксований розмір (SampleSize > 0)
• Відео (H.264): зазвичай змінний розмір (SampleSize = 0)

Розрахунок розміру семплу:
    func GetSampleSize(stsz *fmp4io.SampleSize, sampleIndex int) (uint32, error) {
        if stsz == nil {
            return 0, fmt.Errorf("missing stsz table")
        }
        
        if stsz.SampleSize != 0 {
            // Фіксований розмір для всіх семплів
            return stsz.SampleSize, nil
        }
        
        // Змінний розмір: читаємо з Entries
        if sampleIndex >= len(stsz.Entries) {
            return 0, fmt.Errorf("sample index %d out of range", sampleIndex)
        }
        return stsz.Entries[sampleIndex], nil
    }
```

### ⚠️ Критична проблема: обробка фіксованого розміру

```
У поточному коді Marshal/Unmarshal:
    if a.SampleSize != 0 {
        return  // ⚠️ Пропуск запису Entries!
    }

Проблема:
• Якщо SampleSize != 0, Entries не записується/не читається
• Але якщо хтось помилково встановив Entries при SampleSize != 0 → дані втрачаються

✅ Виправлення: валідація узгодженості даних
    func (a *SampleSize) Unmarshal(b []byte, offset int) (n int, err error) {
        // ... читання заголовку ...
        
        if a.SampleSize != 0 {
            // Фіксований розмір: Entries не повинен бути встановлений
            if len(a.Entries) > 0 {
                log.Printf("warning: Entries set but SampleSize=%d, ignoring Entries", a.SampleSize)
                a.Entries = nil
            }
            return
        }
        
        // ... читання Entries для змінного розміру ...
    }
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Демуксинг відео з пошуком даних семплів

```go
// DemuxVideoTrack — читання відео семплів з файлу
func DemuxVideoTrack(file io.ReadSeeker, stbl *fmp4io.SampleTable, timeScale int64) ([]VideoFrame, error) {
    var frames []VideoFrame
    
    // Отримання загальної кількості семплів
    sampleCount := getSampleCount(stbl)
    
    for i := 0; i < sampleCount; i++ {
        // 1. Пошук позиції та розміру даних
        offset, size, err := FindSampleData(stbl, i)
        if err != nil {
            return nil, fmt.Errorf("find sample %d: %w", i, err)
        }
        
        // 2. Читання даних з файлу
        data := make([]byte, size)
        if _, err := file.Seek(int64(offset), io.SeekStart); err != nil {
            return nil, fmt.Errorf("seek to %d: %w", offset, err)
        }
        if _, err := io.ReadFull(file, data); err != nil {
            return nil, fmt.Errorf("read sample %d: %w", i, err)
        }
        
        // 3. Розрахунок таймінгів
        dts, pts, err := GetSamplePresentationTime(stbl, i, timeScale)
        if err != nil {
            return nil, fmt.Errorf("calculate time for sample %d: %w", i, err)
        }
        
        // 4. Визначення чи це ключовий кадр
        isKeyFrame := false
        if stbl.SyncSample != nil {
            for _, idx := range stbl.SyncSample.Entries {
                if int(idx)-1 == i {  // 1-based → 0-based
                    isKeyFrame = true
                    break
                }
            }
        }
        
        frames = append(frames, VideoFrame{
            Data:       data,
            DTS:        dts,
            PTS:        pts,
            IsKeyFrame: isKeyFrame,
        })
    }
    
    return frames, nil
}

type VideoFrame struct {
    Data       []byte
    DTS, PTS   time.Duration
    IsKeyFrame bool
}

// getSampleCount — отримання загальної кількості семплів
func getSampleCount(stbl *fmp4io.SampleTable) int {
    if stbl.SampleSize != nil && stbl.SampleSize.SampleSize != 0 {
        // Фіксований розмір: кількість семплів = кількість записів у stsc
        return countSamplesFromSTSC(stbl.SampleToChunk)
    }
    // Змінний розмір: кількість семплів = довжина Entries у stsz
    if stbl.SampleSize != nil {
        return len(stbl.SampleSize.Entries)
    }
    return 0
}
```

### 🔧 Приклад: Реалізація seek до часу

```go
// SeekToTime — пошук позиції у файлі для відтворення з заданого часу
func SeekToTime(file io.ReadSeeker, stbl *fmp4io.SampleTable, targetTime time.Duration, timeScale int64) (int64, error) {
    // 1. Конвертація цільового часу у ticks
    targetTicks := uint64(targetTime * time.Duration(timeScale) / time.Second)
    
    // 2. Пошук індексу семплу через stts
    sampleIndex := findSampleIndexByTime(stbl.TimeToSample, targetTicks)
    
    // 3. Знаходження найближчого ключового кадру ДО цього індексу
    keyFrameIndex := FindNearestKeyFrame(stbl.SyncSample, sampleIndex)
    
    // 4. Пошук позиції даних для ключового кадру
    offset, _, err := FindSampleData(stbl, keyFrameIndex)
    if err != nil {
        return 0, fmt.Errorf("find key frame data: %w", err)
    }
    
    // 5. Повернення позиції у файлі для seek
    return int64(offset), nil
}

// findSampleIndexByTime — пошук індексу семплу за часом (спрощено)
func findSampleIndexByTime(stts *fmp4io.TimeToSample, targetTicks uint64) int {
    accumulated := uint64(0)
    sampleIndex := 0
    
    for _, entry := range stts.Entries {
        entryDuration := uint64(entry.Duration)
        entryCount := uint64(entry.Count)
        
        if targetTicks < accumulated+entryDuration*entryCount {
            // Цільовий час у межах цього запису
            offset := (targetTicks - accumulated) / entryDuration
            return sampleIndex + int(offset)
        }
        
        accumulated += entryDuration * entryCount
        sampleIndex += int(entryCount)
    }
    
    // Цільовий час після останнього семплу — повертаємо останній
    return sampleIndex - 1
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при доступі до Entries** | Index out of range при читанні таблиць | Додайте перевірку `if index >= len(Entries)` перед доступом |
| **Невірний розрахунок DTS/PTS** | Розсинхронізація аудіо/відео | Переконайтеся що правильно обробляєте стиснуті таблиці stts/ctts |
| **Некоректний пошук ключових кадрів** | Seek не працює або веде до неключового кадру | Пам'ятайте що stss.Entries використовує 1-based indexing! |
| **Відсутня stbl таблиця** | Неможливо знайти дані семплів | Перевіряйте наявність всіх критичних підатомів перед використанням |
| **Неузгодженість SampleSize/Entries** | Невірні розміри семплів | Валідуйте що якщо SampleSize != 0, то Entries має бути порожнім |

---

## ⚡ Оптимізації для high-performance демуксингу

### 1. Кешування результатів пошуку чанків:

```go
type ChunkCache struct {
    mu sync.RWMutex
    cache map[int]int  // sampleIndex → chunkIndex
}

func (c *ChunkCache) Get(sampleIndex int) (int, bool) {
    c.mu.RLock()
    chunkIndex, ok := c.cache[sampleIndex]
    c.mu.RUnlock()
    return chunkIndex, ok
}

func (c *ChunkCache) Set(sampleIndex, chunkIndex int) {
    c.mu.Lock()
    if c.cache == nil {
        c.cache = make(map[int]int)
    }
    c.cache[sampleIndex] = chunkIndex
    c.mu.Unlock()
}
```

### 2. Попередній розрахунок кумулятивних значень:

```go
// PrecomputeSTTS — попередній розрахунок кумулятивних DTS для швидкого пошуку
type PrecomputedSTTS struct {
    CumulativeTicks []uint64  // кумулятивні ticks для кожного запису
    CumulativeSamples []int   // кумулятивна кількість семплів
}

func PrecomputeSTTS(stts *fmp4io.TimeToSample) *PrecomputedSTTS {
    p := &PrecomputedSTTS{
        CumulativeTicks: make([]uint64, len(stts.Entries)),
        CumulativeSamples: make([]int, len(stts.Entries)),
    }
    
    var totalTicks uint64
    var totalSamples int
    
    for i, entry := range stts.Entries {
        totalTicks += uint64(entry.Duration) * uint64(entry.Count)
        totalSamples += int(entry.Count)
        p.CumulativeTicks[i] = totalTicks
        p.CumulativeSamples[i] = totalSamples
    }
    
    return p
}

// Використання для бінарного пошуку замість лінійного
func FindSampleIndexFast(p *PrecomputedSTTS, targetTicks uint64) int {
    // Бінарний пошук у CumulativeTicks...
    // O(log n) замість O(n)
}
```

### 3. Моніторинг продуктивності демуксингу:

```go
type DemuxMetrics struct {
    SamplesRead prometheus.CounterVec
    SeekLatency prometheus.HistogramVec
    CacheHitRatio prometheus.GaugeVec
}

func (m *DemuxMetrics) RecordSampleRead(duration time.Duration, cacheHit bool) {
    m.SamplesRead.Inc()
    m.SeekLatency.Observe(duration.Seconds())
    if cacheHit {
        m.CacheHitRatio.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання SampleTable

```go
// ✅ 1. Перевірка наявності критичних таблиць перед використанням
if stbl.SampleDesc == nil {
    return fmt.Errorf("missing stsd table")
}
if stbl.TimeToSample == nil {
    return fmt.Errorf("missing stts table")
}
if stbl.SampleToChunk == nil {
    return fmt.Errorf("missing stsc table")
}
if stbl.ChunkOffset == nil {
    return fmt.Errorf("missing stco table")
}
if stbl.SampleSize == nil {
    return fmt.Errorf("missing stsz table")
}

// ✅ 2. Валідація індексів перед доступом до Entries
if sampleIndex >= len(stbl.SampleSize.Entries) {
    return fmt.Errorf("sample index %d out of range", sampleIndex)
}

// ✅ 3. Коректна обробка 1-based indexing у stss
for _, idx := range stbl.SyncSample.Entries {
    keyFrameIndex := int(idx) - 1  // 1-based → 0-based
    // ... використання keyFrameIndex ...
}

// ✅ 4. Узгодженість SampleSize та Entries
if stbl.SampleSize.SampleSize != 0 && len(stbl.SampleSize.Entries) > 0 {
    log.Printf("warning: SampleSize=%d but Entries has %d items, ignoring Entries", 
        stbl.SampleSize.SampleSize, len(stbl.SampleSize.Entries))
}

// ✅ 5. Логування з контекстом для дебагу
log.Printf("Parsed stbl: stts=%d entries, stss=%d keyframes, stsz=%d samples", 
    len(stbl.TimeToSample.Entries), 
    len(stbl.SyncSample.Entries),
    getSampleCount(stbl))

// ✅ 6. Метрики для моніторингу
metrics.RecordSampleRead(time.Since(start), cacheHit)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [MP4 Sample Table Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25688) — Apple documentation про stbl атоми
- 📄 [Time-To-Sample Atom Format](https://wiki.multimedia.cx/index.php/MP4#stts) — детальний опис stts таблиці
- 🧪 [Binary search in Go](https://pkg.go.dev/sort#Search) — стандартна бібліотека для бінарного пошуку
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте наявність критичних таблиць** — без stbl неможливо демуксити файл.
> 2. **Пам'ятайте про 1-based indexing у stss** — уникнення помилок при пошуку ключових кадрів.
> 3. **Валідуйте узгодженість SampleSize/Entries** — уникнення невірних розмірів семплів.
> 4. **Використовуйте кешування для пошуку чанків** — прискорення демуксингу великих файлів.
> 5. **Попередньо розраховуйте кумулятивні значення** — O(log n) пошук замість O(n) для великих таблиць.

Потрібен приклад реалізації повного циклу демуксингу з підтримкою seek та буферизації, або інтеграція `fmp4io.SampleTable` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀