# 📦 Глибокий розбір: `fmp4io.Media` — Метадані медіа-треку у MP4

Цей файл — **реалізація атомів `mdia` (Media), `mdhd` (Media Header), `minf` (Media Info), `vmhd` (Video Media Header), `smhd` (Sound Media Header), та `dinf` (Data Info)** для опису медіа-треків у форматі MP4. Ці атоми містять критичні метадані для відтворення відео та аудіо потоків.

---

## 🗺️ Архітектурна схема Media ієрархії

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.Media — Media Metadata      │
├────────────────────────────────────────┤
│                                         │
│  🔑 Основні атоми:                      │
│  • mdia (Media) — кореневий контейнер │
│  │  ├─ mdhd (MediaHeader) — час, мова │
│  │  ├─ hdlr (HandlerRefer) — тип треку│
│  │  └─ minf (MediaInfo) — специфіка медіа│
│  │      ├─ vmhd/smhd — відео/аудіо специфіка│
│  │      ├─ dinf — дані посилання      │
│  │      └─ stbl — таблиці семплів    │
│                                         │
│  🔄 Ієрархія треку:                     │
│  moov → trak → mdia → minf → stbl    │
│                                         │
│  📡 Призначення:                        │
│  • Опис параметрів медіа-треку        │
│  • Синхронізація аудіо/відео          │
│  • Ініціалізація декодерів            │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Media (mdia) — кореневий контейнер медіа-треку

### 🔧 Структура та призначення:

```go
type Media struct {
    Header   *MediaHeader   // mdhd: час, timeScale, мова
    Handler  *HandlerRefer  // hdlr: тип треку (відео/аудіо/субтитри)
    Info     *MediaInfo     // minf: специфічна інформація про медіа
    Unknowns []Atom         // невідомі дочірні атоми для сумісності
    AtomPos                 // offset/size у файлі
}
```

### 🔍 Призначення mdia атому:

```
mdia (Media) містить всі метадані для одного медіа-треку:

• Визначає тип треку (відео/аудіо/текст) через Handler
• Задає часову шкалу (timeScale) для синхронізації
• Містить посилання на таблиці семплів (stbl) для навігації
• Критичний для коректного відтворення треку

Структура:
  mdia (Media)
  ├─ mdhd (MediaHeader) — базові параметри треку
  │  ├─ TimeScale — ticks per second для цього треку
  │  ├─ Duration — тривалість треку у ticks
  │  ├─ Language — код мови (ISO 639-2/T)
  │  └─ CreateTime/ModifyTime — часові мітки
  ├─ hdlr (HandlerRefer) — тип обробника
  │  ├─ SubType — 'vide'/'soun'/'subt' тощо
  │  └─ Name — людино-читабельна назва
  └─ minf (MediaInfo) — специфічна інформація
     ├─ vmhd/smhd — відео/аудіо специфіка
     ├─ dinf — посилання на дані
     └─ stbl — таблиці семплів (критично!)
```

### ✅ Ваш use-case**: визначення типу треку

```go
// GetTrackType — визначення типу треку (відео/аудіо/субтитри)
func GetTrackType(media *fmp4io.Media) TrackType {
    if media == nil || media.Handler == nil {
        return TrackTypeUnknown
    }
    
    subType := string(media.Handler.SubType[:])
    switch subType {
    case "vide":
        return TrackTypeVideo
    case "soun":
        return TrackTypeAudio
    case "subt", "text":
        return TrackTypeSubtitle
    case "hint":
        return TrackTypeHint
    default:
        return TrackTypeUnknown
    }
}

type TrackType int

const (
    TrackTypeUnknown TrackType = iota
    TrackTypeVideo
    TrackTypeAudio
    TrackTypeSubtitle
    TrackTypeHint
)

// Використання:
for _, track := range moov.Tracks {
    trackType := GetTrackType(track.Media)
    log.Printf("Track %d: type=%v", track.Header.TrackId, trackType)
}
```

---

## 🔑 2. MediaHeader (mdhd) — заголовок медіа-треку

### 🔧 Структура та призначення:

```go
type MediaHeader struct {
    Version    uint8      // версія формату (0=32-бітний час, 1=64-бітний)
    Flags      uint32     // бітові прапорці (зазвичай 0)
    CreateTime time.Time  // час створення треку (епоха 1904)
    ModifyTime time.Time  // час останньої модифікації
    TimeScale  uint32     // ⭐ ticks per second для цього треку
    Duration   uint32     // ⭐ тривалість треку у ticks
    Language   int16      // код мови (5-бітний packed format)
    Quality    int16      // якість медіа (зазвичай 0)
    AtomPos
}
```

### 🔍 Призначення критичних полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `TimeScale` | `uint32` | **Критично**: кількість "тіків" за секунду для цього треку | `90000` для відео (MPEG), `48000` для аудіо |
| `Duration` | `uint32` | **Критично**: загальна тривалість треку у ticks | `360000` = 4 секунди @ 90kHz |
| `Language` | `int16` | Код мови у 5-бітному packed format (ISO 639-2/T) | `21956` = 'und' (undefined) |

### 🔍 Як працює Language field:

```
Language кодується у 16-бітному полі у форматі:
• Біти 0-4: не використовуються (0)
• Біти 5-9: перша літера мови (a=1, b=2, ..., z=26)
• Біти 10-14: друга літера
• Біти 15-19: третя літера

Приклади:
• 'eng' (англійська): 1*1024 + 14*32 + 7 = 1487
• 'und' (невизначена): 21*1024 + 14*32 + 4 = 21956

Декодування:
    func DecodeLanguage(code int16) string {
        if code == 0 { return "und" }
        c1 := byte((code>>10)&0x1F + 0x60)  // +0x60 = 'a'-1
        c2 := byte((code>>5)&0x1F + 0x60)
        c3 := byte(code&0x1F + 0x60)
        return string([]byte{c1, c2, c3})
    }
```

### ✅ Ваш use-case**: конвертація тривалості у time.Duration

```go
// GetTrackDuration — отримання тривалості треку у time.Duration
func GetTrackDuration(header *fmp4io.MediaHeader) time.Duration {
    if header == nil || header.TimeScale == 0 {
        return 0
    }
    
    // Конвертація ticks у секунди
    return time.Duration(header.Duration) * time.Second / time.Duration(header.TimeScale)
}

// GetTrackTimeScale — отримання timeScale для конвертації таймінгів
func GetTrackTimeScale(header *fmp4io.MediaHeader) int64 {
    if header == nil {
        return 90000  // default для відео
    }
    return int64(header.TimeScale)
}

// Використання для синхронізації:
videoScale := GetTrackTimeScale(videoTrack.Media.Header)
audioScale := GetTrackTimeScale(audioTrack.Media.Header)

// Конвертація відео часу у аудіо шкалу для порівняння
videoTimeInAudioScale := videoDTS * audioScale / videoScale
if videoTimeInAudioScale > audioDTS {
    // Відео відстає — можна пропустити кадр або буферизувати
}
```

---

## 🔑 3. MediaInfo (minf) — специфічна інформація про медіа

### 🔧 Структура та призначення:

```go
type MediaInfo struct {
    Sound    *SoundMediaInfo  // smhd: специфіка аудіо треку
    Video    *VideoMediaInfo  // vmhd: специфіка відео треку
    Data     *DataInfo        // dinf: посилання на дані
    Sample   *SampleTable     // ⭐ stbl: таблиці семплів (критично!)
    Unknowns []Atom           // невідомі дочірні атоми
    AtomPos
}
```

### 🔍 Призначення minf атому:

```
minf (Media Information) містить специфічну інформацію для типу медіа:

• Для відео: vmhd (VideoMediaHeader) з параметрами відображення
• Для аудіо: smhd (SoundMediaHeader) з балансом каналів
• Для всіх: dinf (DataInformation) з посиланнями на дані
• Для всіх: ⭐ stbl (SampleTable) — ТАБЛИЦІ СЕМПЛІВ (найважливіше!)

SampleTable (stbl) містить:
• stsd — опис кодеків (AVC1Desc, MP4ADesc)
• stts — Time-To-Sample (розрахунок DTS)
• ctts — Composition Offset (розрахунок PTS = DTS + offset)
• stsc — Sample-To-Chunk (мапінг семплів у чанки)
• stsz — Sample Size (розміри семплів)
• stco — Chunk Offset (позиції чанків у файлі)
• stss — Sync Sample (індекси ключових кадрів)

Без stbl неможливо знайти дані семплів у файлі!
```

### ✅ Ваш use-case**: пошук SampleTable для демуксингу

```go
// GetSampleTable — отримання таблиці семплів для треку
func GetSampleTable(media *fmp4io.Media) (*fmp4io.SampleTable, error) {
    if media == nil || media.Info == nil {
        return nil, fmt.Errorf("missing media info")
    }
    
    stbl := media.Info.Sample
    if stbl == nil {
        return nil, fmt.Errorf("missing sample table (stbl)")
    }
    
    // Перевірка наявності критичних таблиць
    if stbl.SampleDesc == nil {
        return nil, fmt.Errorf("missing sample description (stsd)")
    }
    if stbl.TimeToSample == nil {
        return nil, fmt.Errorf("missing time-to-sample table (stts)")
    }
    if stbl.SampleToChunk == nil {
        return nil, fmt.Errorf("missing sample-to-chunk table (stsc)")
    }
    if stbl.ChunkOffset == nil {
        return nil, fmt.Errorf("missing chunk offset table (stco)")
    }
    if stbl.SampleSize == nil {
        return nil, fmt.Errorf("missing sample size table (stsz)")
    }
    
    return stbl, nil
}

// Використання у демуксері:
stbl, err := GetSampleTable(track.Media)
if err != nil {
    return fmt.Errorf("init track: %w", err)
}

// Тепер можна шукати дані семплів у файлі
offset, size, err := FindSampleData(stbl, sampleIndex)
```

---

## 🔑 4. VideoMediaInfo (vmhd) — специфіка відео треку

### 🔧 Структура та призначення:

```go
type VideoMediaInfo struct {
    Version      uint8       // версія формату (зазвичай 1)
    Flags        uint32      // бітові прапорці (0x000001 = graphics mode present)
    GraphicsMode int16       // режим композитингу (0 = copy, 1 = transparent, тощо)
    Opcolor      [3]int16    // колір для композитингу [red, green, blue] у 0..256
    AtomPos
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `GraphicsMode` | `int16` | Режим композитингу відео | `0` = copy (просто копіювати), `64` = transparent |
| `Opcolor` | `[3]int16` | Колір для композитингу у форматі 0..256 | `[256, 256, 256]` = білий |

### 🔍 Графічні режими:

```
GraphicsMode значення:
• 0 = copy: просто копіювати відео (найпоширеніший)
• 32 = blend: змішування з фоном за Opcolor
• 64 = transparent: прозорість за ключовим кольором
• 66 = straight alpha: alpha-канал у даних

Opcolor формат:
• Значення 0..256 (не 0..255!)
• 256 = максимальна інтенсивність
• Для blend mode: Opcolor = колір фону

Приклад для transparent mode:
• GraphicsMode = 64
• Opcolor = [0, 256, 0] → зелений колір як прозорий
```

### ✅ Ваш use-case**: налаштування відео композитингу

```go
// ConfigureVideoComposition — налаштування параметрів відео композитингу
func ConfigureVideoComposition(vmhd *fmp4io.VideoMediaInfo, mode CompositionMode, bgColor Color) {
    if vmhd == nil {
        return
    }
    
    vmhd.GraphicsMode = int16(mode)
    
    // Конвертація 0..255 → 0..256 формат
    vmhd.Opcolor[0] = int16(bgColor.R) * 256 / 255
    vmhd.Opcolor[1] = int16(bgColor.G) * 256 / 255
    vmhd.Opcolor[2] = int16(bgColor.B) * 256 / 255
    
    // Встановлення прапорця якщо потрібно
    if mode != ModeCopy {
        vmhd.Flags |= 0x000001  // graphics mode present
    }
}

type CompositionMode int
const (
    ModeCopy        CompositionMode = 0
    ModeBlend       CompositionMode = 32
    ModeTransparent CompositionMode = 64
    ModeAlpha       CompositionMode = 66
)

type Color struct {
    R, G, B uint8
}

// Використання:
vmhd := &fmp4io.VideoMediaInfo{
    Version: 1,
    Flags:   0,
}
ConfigureVideoComposition(vmhd, ModeTransparent, Color{0, 0, 0})  // чорний прозорий
```

---

## 🔑 5. SoundMediaInfo (smhd) — специфіка аудіо треку

### 🔧 Структура та призначення:

```go
type SoundMediaInfo struct {
    Version uint8   // версія формату (зазвичай 0)
    Flags   uint32  // бітові прапорці (зазвичай 0)
    Balance int16   // ⭐ баланс каналів у fixed-point 8.8 форматі
    AtomPos
}
```

### 🔍 Balance field — фіксована крапка 8.8:

```
Balance кодується у 16-бітному полі у форматі 8.8 (фіксована крапка):
• Біти 0-7: дробова частина
• Біти 8-15: ціла частина (зі знаком)

Значення:
• 0 = центр (моно або стерео збалансовано)
• -256 = повний лівий канал
• +256 = повний правий канал
• Проміжні значення = пропорційний баланс

Конвертація:
    func GetBalanceFloat(balance int16) float64 {
        return float64(balance) / 256.0
    }
    
    func SetBalanceFloat(f float64) int16 {
        return int16(f * 256.0)
    }

Приклади:
• 0 = 0.0 = центр
• -128 = -0.5 = перевага лівого
• +128 = +0.5 = перевага правого
• -256 = -1.0 = тільки лівий
```

### ✅ Ваш use-case**: налаштування аудіо балансу

```go
// ConfigureAudioBalance — налаштування балансу аудіо каналів
func ConfigureAudioBalance(smhd *fmp4io.SoundMediaInfo, balance float64) {
    if smhd == nil {
        return
    }
    
    // Обмеження діапазону [-1.0, 1.0]
    if balance < -1.0 {
        balance = -1.0
    } else if balance > 1.0 {
        balance = 1.0
    }
    
    // Конвертація у 8.8 fixed-point
    smhd.Balance = int16(balance * 256.0)
}

// GetAudioBalance — отримання балансу у float форматі
func GetAudioBalance(smhd *fmp4io.SoundMediaInfo) float64 {
    if smhd == nil {
        return 0.0
    }
    return float64(smhd.Balance) / 256.0
}

// Використання:
smhd := &fmp4io.SoundMediaInfo{
    Version: 0,
    Flags:   0,
}
ConfigureAudioBalance(smhd, -0.3)  // перевага лівого каналу
log.Printf("Audio balance: %.2f", GetAudioBalance(smhd))  // -0.30
```

---

## 🔑 6. DataInfo (dinf) — посилання на дані

### 🔧 Структура та призначення:

```go
type DataInfo struct {
    Refer    *DataRefer  // dref: посилання на дані (зазвичай self-reference)
    Unknowns []Atom      // невідомі дочірні атоми
    AtomPos
}
```

### 🔍 Призначення dinf атому:

```
dinf (Data Information) містить посилання на медіа-дані:

• Зазвичай містить один dref (DataReference) атом
• dref вказує де знаходяться дані: у тому ж файлі або зовні
• Для локальних файлів: self-reference (прапорець 0x000001)

Структура dref:
  dref (DataRefer)
  ├─ Version: 0
  ├─ Flags: 0x000001 (self-reference)
  └─ url (DataReferUrl) — порожній атом для self-reference

Це дозволяє:
• Зберігати метадані окремо від медіа-даних
• Посилатися на зовнішні джерела даних
• Підтримувати розподілені медіа-системи
```

### ✅ Ваш use-case**: перевірка self-reference

```go
// IsDataSelfReferenced — перевірка чи дані знаходяться у тому ж файлі
func IsDataSelfReferenced(dinf *fmp4io.DataInfo) bool {
    if dinf == nil || dinf.Refer == nil {
        return false
    }
    
    // Перевірка прапорця self-reference у dref
    return dinf.Refer.Flags&0x000001 != 0
}

// Використання:
if !IsDataSelfReferenced(track.Media.Info.Data) {
    log.Printf("warning: track data may be external")
    // Може знадобитися додаткова обробка для зовнішніх даних
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення відео треку з метаданими

```go
// CreateVideoTrack — генерація повного відео треку для MP4
func CreateVideoTrack(trackID uint32, width, height int, timeScale int32, codecData []byte) (*fmp4io.Track, error) {
    // 1. Створення MediaHeader
    now := time.Now()
    mdhd := &fmp4io.MediaHeader{
        Version:    0,
        Flags:      0,
        CreateTime: now,
        ModifyTime: now,
        TimeScale:  uint32(timeScale),  // зазвичай 90000 для відео
        Duration:   0,  // буде оновлено при записі
        Language:   21956,  // 'und' = undefined
        Quality:    0,
    }
    
    // 2. Створення HandlerRefer
    hdlr := &fmp4io.HandlerRefer{
        Version: 0,
        Flags:   0,
        SubType: [4]byte{'v', 'i', 'd', 'e'},  // 'vide' = video track
        Name:    []byte("Video Media Handler\x00"),
    }
    
    // 3. Створення VideoMediaInfo
    vmhd := &fmp4io.VideoMediaInfo{
        Version:      1,
        Flags:        0x000001,  // graphics mode present
        GraphicsMode: 0,         // copy mode
        Opcolor:      [3]int16{0, 0, 0},  // чорний (не використовується у copy mode)
    }
    
    // 4. Створення SampleTable з AVC1Desc
    stsd := &fmp4io.SampleDesc{
        AVC1Desc: &fmp4io.AVC1Desc{
            DataRefIdx:           1,
            Width:                int16(width),
            Height:               int16(height),
            HorizontalResolution: 72.0,
            VerticalResolution:   72.0,  // ⚠️ Виправлена опечатка!
            FrameCount:           1,
            Depth:                24,
            ColorTableId:         -1,
            Conf: &fmp4io.AVC1Conf{
                Data: codecData,  // AVCDecoderConfigurationRecord
            },
        },
    }
    
    stbl := &fmp4io.SampleTable{
        SampleDesc:   stsd,
        TimeToSample: &fmp4io.TimeToSample{},  // буде заповнено при записі
        SampleToChunk: &fmp4io.SampleToChunk{
            Entries: []fmp4io.SampleToChunkEntry{
                {FirstChunk: 1, SamplesPerChunk: 1, SampleDescId: 1},
            },
        },
        SampleSize:  &fmp4io.SampleSize{},
        ChunkOffset: &fmp4io.ChunkOffset{},
        SyncSample:  &fmp4io.SyncSample{},  // для ключових кадрів
    }
    
    // 5. Створення DataInfo з self-reference
    dinf := &fmp4io.DataInfo{
        Refer: &fmp4io.DataRefer{
            Version: 0,
            Flags:   0x000001,  // self-reference
            Url: &fmp4io.DataReferUrl{
                Version: 0,
                Flags:   0x000001,
            },
        },
    }
    
    // 6. Об'єднання у MediaInfo
    minf := &fmp4io.MediaInfo{
        Video: vmhd,
        Data:  dinf,
        Sample: stbl,
    }
    
    // 7. Об'єднання у Media
    media := &fmp4io.Media{
        Header:  mdhd,
        Handler: hdlr,
        Info:    minf,
    }
    
    // 8. Створення повного треку
    track := &fmp4io.Track{
        Header: &fmp4io.TrackHeader{
            TrackId:    int32(trackID),
            Flags:      0x0003,  // enabled | in-movie
            Duration:   0,  // буде оновлено
            Matrix:     [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},  // identity matrix
            TrackWidth:  float64(width),
            TrackHeight: float64(height),
        },
        Media: media,
    }
    
    return track, nil
}
```

### 🔧 Приклад: Парсинг медіа-метаданих для аналізу

```go
// AnalyzeMediaMetadata — витягування метаданих треку для логування/аналізу
func AnalyzeMediaMetadata(media *fmp4io.Media) (*TrackAnalysis, error) {
    if media == nil {
        return nil, fmt.Errorf("nil media")
    }
    
    analysis := &TrackAnalysis{}
    
    // 1. Аналіз MediaHeader
    if media.Header != nil {
        header := media.Header
        analysis.TimeScale = int64(header.TimeScale)
        analysis.Duration = time.Duration(header.Duration) * time.Second / time.Duration(header.TimeScale)
        analysis.Language = DecodeLanguage(header.Language)
        analysis.CreateTime = header.CreateTime
    }
    
    // 2. Визначення типу треку
    analysis.TrackType = GetTrackType(media)
    
    // 3. Аналіз специфіки медіа
    if media.Info != nil {
        info := media.Info
        
        // Відео специфіка
        if info.Video != nil {
            analysis.VideoMode = CompositionMode(info.Video.GraphicsMode)
            analysis.VideoBalance = [3]float64{
                float64(info.Video.Opcolor[0]) / 256.0,
                float64(info.Video.Opcolor[1]) / 256.0,
                float64(info.Video.Opcolor[2]) / 256.0,
            }
        }
        
        // Аудіо специфіка
        if info.Sound != nil {
            analysis.AudioBalance = float64(info.Sound.Balance) / 256.0
        }
        
        // Перевірка наявності SampleTable
        if info.Sample != nil {
            analysis.HasSampleTable = true
            
            // Аналіз кодека
            if info.Sample.SampleDesc != nil {
                desc := info.Sample.SampleDesc
                if desc.AVC1Desc != nil {
                    analysis.Codec = "H.264"
                    analysis.Width = int(desc.AVC1Desc.Width)
                    analysis.Height = int(desc.AVC1Desc.Height)
                } else if desc.MP4ADesc != nil {
                    analysis.Codec = "AAC"
                    analysis.Channels = int(desc.MP4ADesc.NumberOfChannels)
                    analysis.SampleRate = int(desc.MP4ADesc.SampleRate)
                }
            }
        }
    }
    
    return analysis, nil
}

type TrackAnalysis struct {
    TrackType      TrackType
    TimeScale      int64
    Duration       time.Duration
    Language       string
    CreateTime     time.Time
    Codec          string
    Width, Height  int
    Channels       int
    SampleRate     int
    VideoMode      CompositionMode
    VideoBalance   [3]float64
    AudioBalance   float64
    HasSampleTable bool
}

// Використання:
for _, track := range moov.Tracks {
    analysis, err := AnalyzeMediaMetadata(track.Media)
    if err != nil {
        log.Printf("warning: analyze track %d: %v", track.Header.TrackId, err)
        continue
    }
    
    log.Printf("Track %d: type=%v, codec=%s, %dx%d, duration=%v",
        track.Header.TrackId, analysis.TrackType, analysis.Codec,
        analysis.Width, analysis.Height, analysis.Duration)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при читанні 3-байтових полів** | Доступ за межами буфера у Unmarshal | Додайте перевірку `if len(b) < n+3` перед `pio.U24BE()` |
| **Невірний розрахунок тривалості** | Duration не співпадає з реальним часом | Переконайтеся що використовуєте правильний `TimeScale` для конвертації |
| **Некоректне декодування мови** | Language показує "??? " замість коду | Використовуйте правильну формулу декодування 5-бітних символів |
| **Відсутній SampleTable** | Неможливо знайти дані семплів | Перевіряйте наявність `media.Info.Sample` перед використанням |
| **Невірний Balance для аудіо** | Звук тільки в одному каналі | Переконайтеся що конвертуєте між 8.8 fixed-point та float коректно |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування конвертацій часу:

```go
var durationCache = sync.Map{}  // map[uint64]time.Duration

func CachedGetDuration(duration, timeScale uint32) time.Duration {
    key := uint64(duration)<<32 | uint64(timeScale)
    
    if cached, ok := durationCache.Load(key); ok {
        return cached.(time.Duration)
    }
    
    result := time.Duration(duration) * time.Second / time.Duration(timeScale)
    durationCache.Store(key, result)
    return result
}
```

### 2. Попередня аллокація буферів для Marshal:

```go
// PreallocateMediaBuffer — виділення місця для серіалізації заздалегідь
func PreallocateMediaBuffer(media *fmp4io.Media) []byte {
    estimatedSize := media.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateMediaBuffer(media)
n := media.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type MediaMetrics struct {
    TracksParsed prometheus.CounterVec
    ParseLatency prometheus.HistogramVec
    CodecTypes   prometheus.CounterVec
    ParseErrors  prometheus.CounterVec
}

func (m *MediaMetrics) RecordTrack(codec string, duration time.Duration, err error) {
    m.TracksParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    if codec != "" {
        m.CodecTypes.WithLabelValues(codec).Inc()
    }
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання Media атомів

```go
// ✅ 1. Перевірка меж буфера перед читанням 3-байтових полів
if len(b) < n+3 {
    err = parseErr("Flags", n+offset, err)
    return
}
a.Flags = pio.U24BE(b[n:])
n += 3

// ✅ 2. Валідація TimeScale перед діленням
if header.TimeScale == 0 {
    return fmt.Errorf("invalid TimeScale: cannot be zero")
}
duration := time.Duration(header.Duration) * time.Second / time.Duration(header.TimeScale)

// ✅ 3. Декодування Language field коректно
func DecodeLanguage(code int16) string {
    if code == 0 { return "und" }
    c1 := byte((code>>10)&0x1F + 0x60)
    c2 := byte((code>>5)&0x1F + 0x60)
    c3 := byte(code&0x1F + 0x60)
    return string([]byte{c1, c2, c3})
}

// ✅ 4. Перевірка наявності SampleTable перед використанням
if media.Info == nil || media.Info.Sample == nil {
    return fmt.Errorf("missing sample table for track")
}

// ✅ 5. Конвертація Balance між 8.8 fixed-point та float
balanceFloat := float64(smhd.Balance) / 256.0
smhd.Balance = int16(balanceFloat * 256.0)

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Parsed media: type=%v, codec=%s, timeScale=%d, duration=%v", 
    GetTrackType(media), codec, header.TimeScale, duration)

// ✅ 7. Метрики для моніторингу
metrics.RecordTrack(codec, time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [MP4 Language Codes](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25688) — Apple documentation про кодування мов
- 📄 [Fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic) — теорія формату 8.8
- 🧪 [Go time Package Documentation](https://pkg.go.dev/time) — робота з часом у Go
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед `pio.U24BE()`** — уникнення панік при пошкоджених файлах.
> 2. **Валідуйте `TimeScale != 0` перед діленням** — уникнення панік при некоректних метаданих.
> 3. **Коректно декодуйте `Language` field** — уникнення невірних кодів мов у метаданих.
> 4. **Перевіряйте наявність `SampleTable`** — без нього неможливо знайти дані семплів.
> 5. **Конвертуйте `Balance` між 8.8 fixed-point та float коректно** — уникнення невірної гучності каналів.

Потрібен приклад реалізації повного циклу створення/парсингу відео треку з підтримкою H.264/AAC кодеків, або інтеграція `fmp4io.Media` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀