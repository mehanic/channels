# 🎬 `divide`: Інструмент для розділення MP4 на HLS-сегменти

Це **практичний інструмент** на основі бібліотеки `go-mp4`, який розділяє вхідний MP4-файл на **HLS-сумісні сегменти** з генерацією плейлистів `.m3u8` — ідеально для підготовки відео для адаптивного стрімінгу.

---

## 🎯 Коротка відповідь

> **Це "конвертер MP4 → HLS"**: він аналізує структуру вхідного файлу, виділяє ініціалізаційні дані (moov/ftyp), розбиває медіа-дані на сегменти (moof/mdat), та генерує master/media плейлисти для відтворення у вебі.

---

## 🗂️ Структура вихідних даних

```
📁 OUTPUT_DIR/
├── 📄 playlist.m3u8                    # 🔹 Master playlist
│
├── 📁 video/                           # 🔹 Відео-доріжка (avc1)
│   ├── 📄 init.mp4                     # 🔹 Ініціалізаційний сегмент (ftyp+moov)
│   ├── 📄 0.mp4, 1.mp4, 2.mp4...      # 🔹 Медіа-сегменти (moof+mdat)
│   └── 📄 playlist.m3u8                # 🔹 Media playlist для відео
│
├── 📁 audio/                           # 🔹 Аудіо-доріжка (mp4a)
│   ├── 📄 init.mp4
│   ├── 📄 0.mp4, 1.mp4...
│   └── 📄 playlist.m3u8
│
├── 📁 video_enc/                       # 🔹 Зашифроване відео (encv)
│   └── ... (аналогічно)
│
└── 📁 audio_enc/                       # 🔹 Зашифроване аудіо (enca)
    └── ... (аналогічно)
```

---

## 🔍 Основні компоненти

### 🔹 Класифікація доріжок (`trackType`)

```go
type trackType int
const (
    trackVideo    trackType = iota  // 🔹 avc1: H.264 відео
    trackAudio                      // 🔹 mp4a: AAC аудіо
    trackEncVideo                   // 🔹 encv: зашифроване відео (DRM)
    trackEncAudio                   // 🔹 enca: зашифроване аудіо (DRM)
)
```

**🎯 Призначення**: Розрізняти типи доріжок для:
- ✅ Правильного кодування у плейлистах (`CODECS="avc1.64001f,mp4a.40.2"`)
- ✅ Розділення на окремі директорії для адаптивного стрімінгу
- ✅ Підтримки DRM-контенту через окремі папки `_enc`

---

### 🔹 Структура `track` — метадані доріжки

```go
type track struct {
    id          uint32              // 🔹 TrackID з Tkhd
    trackType   trackType           // 🔹 Тип: video/audio/encrypted
    timescale   uint32              // 🔹 Частота дискретизації з Mdhd
    bandwidth   uint64              // 🔹 Розрахований бітрейт
    height      uint16              // 🔹 Роздільність (для відео)
    width       uint16
    segments    []segment           // 🔹 Список сегментів з тривалістю
    outputDir   string              // 🔹 Шлях для запису
    initFile    *os.File            // 🔹 Файл init.mp4
    segmentFile *os.File            // 🔹 Поточний файл сегмента
}
```

**🎯 Призначення**: Зберігати **всю необхідну інформацію** для генерації HLS-плейлистів та запису сегментів.

---

### 🔹 Етап 1: Виявлення та класифікація доріжок

```go
// 🔹 Пошук всіх trak боксів у moov
bis, err := mp4.ExtractBox(inputFile, nil, mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeTrak()})

for _, bi := range bis {
    t := new(track)
    
    // 🔹 Отримання TrackID з Tkhd
    bs, _ := mp4.ExtractBoxWithPayload(inputFile, bi, mp4.BoxPath{mp4.BoxTypeTkhd()})
    tkhd := bs[0].Payload.(*mp4.Tkhd)
    t.id = tkhd.TrackID
    
    // 🔹 Отримання timescale з Mdhd
    bs, _ = mp4.ExtractBoxWithPayload(inputFile, bi, mp4.BoxPath{mp4.BoxTypeMdia(), mp4.BoxTypeMdhd()})
    mdhd := bs[0].Payload.(*mp4.Mdhd)
    t.timescale = mdhd.Timescale
    
    // 🔹 Визначення типу кодека через Stsd
    // avc1 → video, mp4a → audio, encv/enca → encrypted
    if hasBox(inputFile, bi, "avc1") {
        t.trackType = trackVideo
        t.outputDir = path.Join(outputDir, videoDirName)
    } else if hasBox(inputFile, bi, "mp4a") {
        t.trackType = trackAudio
        t.outputDir = path.Join(outputDir, audioDirName)
    }
    // ... аналогічно для encv/enca ...
    
    tracks[t.id] = t  // 🔹 Зберігаємо у мапу за TrackID
}
```

**🎯 Ключова логіка**: Використання `ExtractBoxWithPayload` для отримання конкретних полів без парсингу всього треку.

---

### 🔹 Етап 2: Запис ініціалізаційних сегментів (`init.mp4`)

```go
// 🔹 Обробник для ReadBoxStructure
if h.BoxInfo.Type == mp4.BoxTypeMoov() || h.BoxInfo.Type == mp4.BoxTypeFtyp() || ... {
    
    // 🔹 Для Trak: визначаємо, для якої доріжки цей бокс
    if h.BoxInfo.Type == mp4.BoxTypeTrak() {
        tkhd := extractTkhd(...)  // 🔹 Отримуємо TrackID
        trackID = tkhd.TrackID
    } else {
        writeAll = true  // 🔹 Ftyp/Mvhd тощо пишуться для всіх доріжок
    }
    
    // 🔹 Для кожної доріжки: запис заголовка боксу
    for _, t := range tracks {
        if writeAll || t.id == trackID {
            offsetMap[t.id], _ = t.initFile.Seek(0, io.SeekEnd)
            biMap[t.id], _ = mp4.WriteBoxInfo(t.initFile, &h.BoxInfo)
            biMap[t.id].Size = biMap[t.id].HeaderSize  // ← тимчасовий розмір
        }
    }
    
    // 🔹 Рекурсивна обробка дітей (для Moov)
    if h.BoxInfo.Type == mp4.BoxTypeMoov() {
        vals, _ := h.Expand()
        for _, val := range vals {
            ci := val.(childInfo)  // 🔹 Отримуємо розміри дітей від рекурсії
            for _, t := range tracks {
                biMap[t.id].Size += ci[t.id]  // ← додаємо розміри дітей
            }
        }
    } else {
        // 🔹 Копіювання вмісту боксу
        for _, t := range tracks {
            if writeAll || t.id == trackID {
                n, _ := h.ReadData(t.initFile)
                biMap[t.id].Size += uint64(n)
            }
        }
    }
    
    // 🔹 Оновлення заголовків з правильними розмірами
    for _, t := range tracks {
        if writeAll || t.id == trackID {
            t.initFile.Seek(offsetMap[t.id], io.SeekStart)
            mp4.WriteBoxInfo(t.initFile, biMap[t.id])  // ← перезапис з правильним Size
        }
    }
    
    // 🔹 Повертаємо розміри для батьківських боксів
    ci := make(childInfo)
    for id, bi := range biMap { ci[id] = bi.Size }
    return ci, nil
}
```

**🔄 Потік даних:**
```
🔹 Moov бокс:
1. Запис заголовка [0]["moov"] → offset=0, Size=8 (тимчасовий)
2. Рекурсивна обробка дітей (trak, mvhd...) → отримання їхніх розмірів
3. Розрахунок: moov.Size = 8 + сума_розмірів_дітей
4. Перезапис заголовка: [правильний_розмір]["moov"]

🔹 Trak бокс:
• Записується тільки у init.mp4 відповідної доріжки (за trackID)
• Ftyp/Mvhd записуються у всі init.mp4 (writeAll=true)
```

**🎯 Ключова особливість**: **Двофазний запис** — спочатку заголовок із тимчасовим розміром, потім оновлення після обробки дітей.

---

### 🔹 Етап 3: Обробка медіа-сегментів (`moof`/`mdat`)

```go
// 🔹 Обробка Moof боксу (метадані фрагмента)
if h.BoxInfo.Type == mp4.BoxTypeMoof() {
    
    // 🔹 Визначення доріжки через Tfhd
    bs, _ := mp4.ExtractBoxWithPayload(inputFile, &h.BoxInfo, mp4.BoxPath{mp4.BoxTypeTraf(), mp4.BoxTypeTfhd()})
    tfhd := bs[0].Payload.(*mp4.Tfhd)
    currTrackID = tfhd.TrackID
    
    // 🔹 Розрахунок тривалості фрагмента через Trun
    bs, _ = mp4.ExtractBoxWithPayload(inputFile, &h.BoxInfo, mp4.BoxPath{mp4.BoxTypeTraf(), mp4.BoxTypeTrun()})
    trun := bs[0].Payload.(*mp4.Trun)
    
    var duration uint32
    for i := range trun.Entries {
        if trun.CheckFlag(0x000100) {  // 🔹 sample-duration-present
            duration += trun.Entries[i].SampleDuration
        } else {
            duration += tfhd.DefaultSampleDuration  // 🔹 fallback на default
        }
    }
    
    // 🔹 Закриття попереднього сегмента, створення нового
    t := tracks[currTrackID]
    t.segmentFile.Close()
    t.segmentFile, _ = os.Create(path.Join(t.outputDir, segmentFileName(len(t.segments))))
    t.segments = append(t.segments, segment{
        duration: float64(duration) / float64(t.timescale),  // 🔹 Конвертація у секунди
    })
}

// 🔹 Обробка Mdat боксу (медіа-дані)
if h.BoxInfo.Type == mp4.BoxTypeMdat() {
    t := tracks[currTrackID]
    
    // 🔹 Розрахунок бітрейту для цього сегмента
    bandwidth := uint64(float64(h.BoxInfo.Size) * 8 / t.segments[len(t.segments)-1].duration)
    if bandwidth > t.bandwidth {
        t.bandwidth = bandwidth  // 🔹 Зберігаємо максимум для плейлиста
    }
}

// 🔹 Запис боксу у файл сегмента
t := tracks[currTrackID]
mp4.WriteBoxInfo(t.segmentFile, &h.BoxInfo)  // 🔹 Заголовок
h.ReadData(t.segmentFile)                     // 🔹 Вміст
```

**🎯 Ключова логіка:**
- ✅ `currTrackID` відстежує, якій доріжці належить поточний фрагмент
- ✅ Тривалість розраховується з `Trun.Entries` або `DefaultSampleDuration`
- ✅ Бітрейт оновлюється як максимум серед усіх сегментів доріжки
- ✅ Кожен новий `moof` → новий файл сегмента (`0.mp4`, `1.mp4`...)

---

### 🔹 Етап 4: Генерація HLS-плейлистів

#### 🔹 Master playlist (`playlist.m3u8`)

```go
func outputMasterPlaylist(filePath string, trackTypeMap map[trackType]*track) error {
    file.WriteString("#EXTM3U\n")
    
    // 🔹 Посилання на аудіо-плейлист (якщо є)
    if adir != "" {
        file.WriteString("#EXT-X-MEDIA:TYPE=AUDIO,URI=\"" + adir + "/" + playlistFileName + 
            "\",GROUP-ID=\"audio\",NAME=\"audio\",AUTOSELECT=YES,CHANNELS=\"2\"\n")
    }
    
    // 🔹 Посилання на відео-плейлист з параметрами
    if vdir != "" {
        fmt.Fprintf(file, "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"avc1.64001f,mp4a.40.2\",RESOLUTION=%dx%d",
            vt.bandwidth, vt.width, vt.height)
        if adir != "" { file.WriteString(",AUDIO=\"audio\"") }
        file.WriteString("\n" + vdir + "/" + playlistFileName + "\n")
    }
    return nil
}
```

**🎯 Призначення**: Дозволити плеєру обрати потрібну доріжку (відео+аудіо або тільки аудіо).

**Приклад виводу:**
```m3u8
#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,URI="audio/playlist.m3u8",GROUP-ID="audio",NAME="audio",AUTOSELECT=YES,CHANNELS="2"
#EXT-X-STREAM-INF:BANDWIDTH=1500000,CODECS="avc1.64001f,mp4a.40.2",RESOLUTION=1280x720,AUDIO="audio"
video/playlist.m3u8
```

---

#### 🔹 Media playlist (`video/playlist.m3u8`)

```go
func outputMediaPlaylist(filePath string, segments []segment) error {
    // 🔹 Розрахунок максимальної тривалості сегмента для TARGETDURATION
    var maxDur float64
    for i := range segments {
        if segments[i].duration > maxDur { maxDur = segments[i].duration }
    }
    
    file.WriteString("#EXTM3U\n")
    file.WriteString("#EXT-X-VERSION:7\n")  // 🔹 Підтримка fMP4
    fmt.Fprintf(file, "#EXT-X-TARGETDURATION:%d\n", int(math.Ceil(maxDur)))
    file.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")  // 🔹 Відео на вимогу
    file.WriteString("#EXT-X-MAP:URI=\"" + initMP4FileName + "\"\n")  // 🔹 Ініціалізаційний сегмент
    
    // 🔹 Список сегментів
    for i := range segments {
        fmt.Fprintf(file, "#EXTINF:%f,\n", segments[i].duration)
        fmt.Fprintf(file, "%s\n", segmentFileName(i))
    }
    file.WriteString("#EXT-X-ENDLIST\n")  // 🔹 Кінець плейлиста
    return nil
}
```

**Приклад виводу:**
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-MAP:URI="init.mp4"
#EXTINF:3.840000,
0.mp4
#EXTINF:4.000000,
1.mp4
#EXTINF:3.920000,
2.mp4
#EXT-X-ENDLIST
```

---

## 🔍 Повний потік обробки файлу

```
🔹 Вхід: input.mp4 (файл з moov + послідовність moof/mdat)
│
▼
🔹 Етап 1: Аналіз структури
   • ExtractBox(moov/trak) → виявлення доріжок
   • Визначення типів: avc1/mp4a/encv/enca
   • Створення мапи tracks[trackID] → *track
│
▼
🔹 Етап 2: Підготовка вихідних файлів
   • MkdirAll(video/, audio/, ...)
   • Створення init.mp4 та 0.mp4 для кожної доріжки
│
▼
🔹 Етап 3: ReadBoxStructure з обробником
   │
   ├── 🔹 Для ftyp/moov/mvhd/trak...:
   │   • Запис у init.mp4 відповідної доріжки
   │   • Двофазне оновлення заголовків
   │
   ├── 🔹 Для moof:
   │   • Визначення trackID через Tfhd
   │   • Розрахунок тривалості через Trun
   │   • Створення нового файлу сегмента
   │
   ├── 🔹 Для mdat:
   │   • Розрахунок бітрейту
   │   • Запис медіа-даних у поточний сегмент
   │
   └── 🔹 Для інших боксів: помилка або пропуск
│
▼
🔹 Етап 4: Генерація плейлистів
   • Master playlist: посилання на відео/аудіо плейлисти
   • Media playlists: список сегментів з тривалістю
│
▼
🔹 Вихід: HLS-сумісна структура директорій ✅
```

---

## 🛠️ Практичне використання

### 🔹 Командний рядок

```bash
# 🔹 Базове використання:
$ mp4tool divide input.mp4 output/

# 🔹 Результат:
output/
├── playlist.m3u8
├── video/
│   ├── init.mp4
│   ├── 0.mp4, 1.mp4, 2.mp4...
│   └── playlist.m3u8
└── audio/
    ├── init.mp4
    ├── 0.mp4, 1.mp4...
    └── playlist.m3u8
```

### 🔹 Інтеграція у CCTV HLS Processor

```go
// 🔹 Приклад: автоматична підготовка записів для стрімінгу
func prepareRecordingForStreaming(recordingPath, outputDir string) error {
    // 🔹 Крок 1: Розділення на сегменти
    err := divide.Main([]string{recordingPath, outputDir})
    if err != nil {
        return fmt.Errorf("failed to divide: %w", err)
    }
    
    // 🔹 Крок 2: Додавання метаданих для аналітики
    addAnalyticsMetadata(outputDir)
    
    // 🔹 Крок 3: Завантаження на CDN
    return uploadToCDN(outputDir)
}

// 🔹 Використання у конвеєрі обробки:
go func() {
    for recording := range recordingQueue {
        if err := prepareRecordingForStreaming(recording.Path, recording.OutputDir); err != nil {
            log.Printf("❌ Failed to process %s: %v", recording.Path, err)
        } else {
            log.Printf("✅ Ready for streaming: %s", recording.OutputDir)
        }
    }
}()
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильне визначення типу доріжки | Аудіо потрапляє у video/ або навпаки | Перевіряйте наявність avc1/mp4a/encv/enca у Stsd перед класифікацією |
| Забути оновити заголовки init.mp4 | Плеєр не може ініціалізувати декодер | Завжди використовуйте двофазний запис: WriteBoxInfo → діти → оновлення |
| Неправильний розрахунок тривалості | #EXTINF не співпадає з реальною тривалістю → десинхронізація | Використовуйте `timescale` для конвертації: `duration / timescale` |
| Ігнорування прапорців Trun | Пропуск SampleDuration → неправильна тривалість | Перевіряйте `trun.CheckFlag(0x000100)` перед доступом до `SampleDuration` |
| Hard-coded кодеки у плейлисті | Неправильний CODECS → плеєр не відтворює | Визначайте кодеки динамічно з Stsd: avc1.64001f, mp4a.40.2 тощо |

---

## 📋 Чекліст для вашого проекту

```
[ ] При підготовці вхідних файлів:
    • Переконайтеся, що файл має фрагментовану структуру (moof/mdat)
    • Перевірте наявність обов'язкових боксів: ftyp, moov, trak, stsd
    • Для DRM-контенту: переконайтеся у наявності pssh/sinf боксів

[ ] При налаштуванні вихідної структури:
    • Створіть окремі директорії для відео/аудіо/encrypted
    • Переконайтеся у правах доступу: os.MkdirAll(..., 0777)
    • Закривайте файли через defer для уникнення витоку ресурсів

[ ] Для коректності плейлистів:
    • Розраховуйте TARGETDURATION як ceil(max_segment_duration)
    • Додавайте #EXT-X-MAP:URI="init.mp4" для fMP4-сумісності
    • Використовуйте #EXT-X-PLAYLIST-TYPE:VOD для записів на вимогу

[ ] Для адаптивного стрімінгу:
    • Розраховуйте bandwidth як максимум серед сегментів
    • Додавайте AUDIO="audio" у STREAM-INF при наявності окремої аудіо-доріжки
    • Перевіряйте CODECS на відповідність реальним кодекам у файлі

[ ] Для дебагу:
    • Логувайте знайдені доріжки: log.Printf("📊 Track %d: %s, %dx%d", id, type, w, h)
    • Перевіряйте розрахунок тривалості: log.Printf("⏱️  Segment %d: %.3fs", i, dur)
    • Валідуйте плейлисти через HLS-валідатори (напр. apple's mediastreamvalidator)
```

---

## 🎯 Висновок

> **`divide` — це "міст" між звичайним MP4 та HLS-стрімінгом**, який забезпечує:
> • ✅ Автоматичне розділення фрагментованих файлів на сегменти
> • ✅ Правильну обробку ініціалізаційних даних для кожної доріжки
> • ✅ Точний розрахунок тривалості та бітрейту для плейлистів
> • ✅ Підтримку зашифрованого контенту через окремі директорії
> • ✅ Генерацію стандартних HLS-плейлистів (master + media)

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Миттєва підготовка записів для веб-відтворення без ручного налаштування
- 🔧 Гнучкість: підтримка відео/аудіо/DRM-доріжок з автоматичним розділенням
- 🌐 Сумісність з усіма HLS-плеєрами: Safari, hls.js, Video.js, ExoPlayer
- 📊 Аналітика: розрахований bandwidth та роздільність для адаптивного вибору якості

Потребуєте допомоги з інтеграцією `divide` у ваш конвеєр обробки записів або з налаштуванням адаптивного стрімінгу? Напишіть — покажу готовий код для вашого сценарію! 🚀🎬