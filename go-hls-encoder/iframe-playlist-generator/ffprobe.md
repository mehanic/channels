# Глибоке роз'яснення: `iframe_playlist_generator` — генерація I-frame-only плейлистів для HLS

Цей файл містить **логіку прозонування відеофайлів через ffprobe** для виявлення ключових кадрів (I-frames) та побудови спеціальних HLS-плейлистів для швидкого перемотування. Це критичний компонент для покращення користувацького досвіду при навігації у записі.

---

## 🎯 Навіщо I-frame-only плейлисти потрібні у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ I-frame playlists у контексті HLS:     │
│                                         │
│ 🔹 Швидке перемотування (trick play):  │
│   • Плеєр завантажує тільки I-frames   │
│   • Миттєвий перехід до будь-якої точки│
│   • Економія трафіку при seek          │
│                                         │
│ 🔹 Прев'ю таймлайну:                   │
│   • Генерація мініатюр для скролу      │
│   • Візуальна навігація по запису      │
│   • Покращення UX для користувачів     │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Швидкий пошук подій у довгих       │
│     записах (години/дні)               │
│   • Ефективне використання bandwidth   │
│     при перегляді архівів              │
│   • Сумісність з Apple HLS spec        │
└─────────────────────────────────────────┘
```

---

## 🔧 Структури даних: прозонування через ffprobe

### `ProbeFrame`: метадані ключового кадру

```go
type ProbeFrame struct {
    // 🔹 Ідентифікація
    MediaType               string `json:"media_type"`        // "video"
    StreamIndex             int    `json:"stream_index"`      // індекс відеопотоку
    KeyFrame                bool   `json:"key_frame"`         // ✅ завжди true (фільтр -skip_frame nokey)
    
    // 🔹 Таймінги
    PktPts                  int    `json:"pkt_pts"`           // PTS у таймбейзах
    PktPtsTime              string `json:"pkt_pts_time"`      // PTS у секундах (строка)
    PktDts                  int    `json:"pkt_dts"`           // DTS у таймбейзах
    PktDtsTime              string `json:"pkt_dts_time"`      // DTS у секундах
    BestEffortTimestamp     int    `json:"best_effort_timestamp"`
    BestEffortTimestampTime string `json:"best_effort_timestamp_time"`
    
    // 🔹 Позиція та розмір
    PktPos                  string `json:"pkt_pos"`           // байтова позиція у файлі
    PktSize                 string `json:"pkt_size"`          // розмір пакету у байтах
    
    // 🔹 Відео-параметри
    Width                   int    `json:"width"`             // ширина кадру
    Height                  int    `json:"height"`            // висота кадру
    PixFmt                  string `json:"pix_fmt"`           // формат пікселів (yuv420p...)
    SampleAspectRatio       string `json:"sample_aspect_ratio"`
    
    // 🔹 Тип кадру
    PictType                string `json:"pict_type"`         // "I", "P", "B"...
    CodedPictureNumber      int    `json:"coded_picture_number"`
    DisplayPictureNumber    int    `json:"display_picture_number"`
    
    // 🔹 Інтерлейс та колір
    InterlacedFrame         int    `json:"interlaced_frame"`
    TopFieldFirst           int    `json:"top_field_first"`
    RepeatPict              int    `json:"repeat_pict"`
    ColorRange              string `json:"color_range"`
    ColorSpace              string `json:"color_space"`
    ColorPrimaries          string `json:"color_primaries"`
    ColorTransfer           string `json:"color_transfer"`
    ChromaLocation          string `json:"chroma_location"`
}
```

> 💡 **Важливо**: `KeyFrame` завжди `true`, бо `probeKeyFrames` використовує `-skip_frame nokey` — ffprobe повертає тільки ключові кадри.

### `ProbePacket`: метадані пакету

```go
type ProbePacket struct {
    PtsTime      float64 `json:"pts_time,string"`      // 🔹 PTS у секундах (парситься з строки)
    DtsTime      float64 `json:"dts_time,string"`      // 🔹 DTS у секундах
    DurationTime float64 `json:"duration_time,string"` // 🔹 Тривалість пакету
    Size         uint    `json:"size,string"`          // 🔹 Розмір у байтах
    Pos          uint    `json:"pos,string"`           // 🔹 Позиція у файлі
    Flags        string  `json:"flags"`                // 🔹 Прапорці: "K_" = key frame, "__" = не ключовий
}
```

### 🎯 Метод `isFromKeyFrame()`: детекція ключових кадрів

```go
func (p *ProbePacket) isFromKeyFrame() bool {
    if len(p.Flags) < 1 {
        log.Println("Assertion Failed: Flags length is 0. Should be at least 1")
        return false
    }
    return p.Flags[0] == 'K'  // 🔹 Перший символ 'K' = key frame
}
```

**Формат прапорців ffprobe:**
```
"K_" = ключовий кадр (Key frame)
"__" = звичайний кадр (не ключовий)
"D_" = discarded packet
"_A" = audio packet

Приклад:
  • Flags = "K_" → isFromKeyFrame() = true ✅
  • Flags = "__" → isFromKeyFrame() = false ✅
```

---

## 🔍 Функція `probeKeyFrames`: виявлення I-frames

```go
func probeKeyFrames(filename string) ([]*ProbeFrame, error) {
    type ProbeFrames struct {
        Frames []*ProbeFrame `json:"frames"`
    }

    // 🔹 Команда ffprobe для отримання тільки ключових кадрів
    rf, errf := exec.Command("ffprobe",
        "-skip_frame", "nokey",        // 🔹 Фільтр: тільки key frames
        "-select_streams", "v",        // 🔹 Тільки відеопотік
        "-show_frames", filename,      // 🔹 Показати метадані кадрів
        "-print_format", "json",       // 🔹 Вивід у JSON для парсингу
    ).Output()
    
    if errf != nil {
        return nil, errf
    }

    // 🔹 Парсинг JSON у структури
    var v ProbeFrames
    err := json.Unmarshal(rf, &v)
    if err != nil {
        return v.Frames, err
    }

    return v.Frames, err
}
```

### 🎯 Ключові параметри ffprobe

| Параметр | Значення | Призначення |
|----------|----------|-------------|
| `-skip_frame nokey` | — | 🔹 Пропускати не-ключові кадри → значне прискорення прозонування |
| `-select_streams v` | — | 🔹 Аналізувати тільки відеопотік (ігнорувати аудіо/субтитри) |
| `-show_frames` | — | 🔹 Вивести детальні метадані кожного кадру |
| `-print_format json` | — | 🔹 Структурований вивід для автоматичного парсингу |

### 🧮 Приклад вихідних даних (спрощено)

```json
{
  "frames": [
    {
      "media_type": "video",
      "stream_index": 0,
      "key_frame": 1,
      "pkt_pts_time": "0.000000",
      "pkt_dts_time": "0.000000",
      "pkt_pos": "48",
      "pkt_size": "125000",
      "width": 1920,
      "height": 1080,
      "pix_fmt": "yuv420p",
      "pict_type": "I",
      "coded_picture_number": 0
    },
    {
      "media_type": "video",
      "stream_index": 0,
      "key_frame": 1,
      "pkt_pts_time": "2.000000",
      "pkt_dts_time": "2.000000",
      "pkt_pos": "125048",
      "pkt_size": "118000",
      "width": 1920,
      "height": 1080,
      "pix_fmt": "yuv420p",
      "pict_type": "I",
      "coded_picture_number": 60
    }
  ]
}
```

> 💡 **Інтерпретація**: Кожен об'єкт у масиві `frames` — це I-frame. `pkt_pts_time` — час початку кадру у секундах, `pkt_pos` — байтова позиція у файлі для швидкого seek.

---

## 🔍 Функція `probePackets`: прозонування пакетів з підтримкою конкатенації

```go
func probePackets(initfilename string, filename string) ([]*ProbePacket, error) {
    type ProbePackets struct {
        Packets []*ProbePacket `json:"packets"`
    }

    var cmd *exec.Cmd
    var rp []byte
    var errp error
    
    // 🔹 Варіант 1: Конкатенація двох файлів (для фрагментованих MP4/fMP4)
    if len(initfilename) > 0 {
        // 🔹 ffprobe читає з stdin (pipe)
        cmd = exec.Command("ffprobe", "-hide_banner", "-loglevel", "warning",
            "-show_packets",
            "-select_streams", "v",
            "-show_entries", "packet=pts_time,dts_time,size,pos,flags,duration_time",
            "-print_format", "json",
            "-",  // 🔹 Читати з stdin
        )
        cmd.Stderr = os.Stderr
        
        var stdout bytes.Buffer
        cmd.Stdout = &stdout
        
        // 🔹 Створити pipe: cat initfilename filename | ffprobe ...
        var err error
        log.Println("DEBUG: cat", initfilename, filename)
        cmdCat := exec.Command("cat", initfilename, filename)
        cmd.Stdin, err = cmdCat.StdoutPipe()
        if err != nil {
            log.Println("DEBUG: cannot create pipe")
            return []*ProbePacket{}, err
        }
        
        // 🔹 Запустити обидва процеси
        err = cmd.Start()  // ffprobe
        if err != nil {
            log.Println("DEBUG: Cannot start ffprobe:", err)
            return []*ProbePacket{}, err
        }
        err = cmdCat.Run()  // cat
        if err != nil {
            log.Println("DEBUG: Error running cat:", err)
            return []*ProbePacket{}, err
        }
        errp = cmd.Wait()  // дочекатися ffprobe
        rp = stdout.Bytes()
        
    // 🔹 Варіант 2: Звичайний файл без конкатенації
    } else {
        cmd = exec.Command("ffprobe", "-hide_banner",
            "-show_packets",
            "-select_streams", "v",
            "-show_entries", "packet=pts_time,dts_time,size,pos,flags,duration_time",
            "-print_format", "json",
            filename,
        )
        cmd.Stderr = os.Stderr
        rp, errp = cmd.Output()
    }
    
    if errp != nil {
        return nil, errp
    }

    // 🔹 Парсинг JSON
    var v ProbePackets
    err := json.Unmarshal(rp, &v)
    if err != nil {
        return v.Packets, err
    }

    return v.Packets, err
}
```

### 🎯 Навіщо потрібна конкатенація (`initfilename`)?

```
Проблема:
• fMP4 (фрагментований MP4) складається з init сегмента + медіа сегментів
• ffprobe не може прозвонити медіа сегмент без init (немає moov box)

Рішення:
• Конкатенувати init + media "на льоту" через pipe: cat init.mp4 media.m4s | ffprobe ...
• ffprobe бачить валідний потік та може витягнути метадані

Архітектура:
[init.mp4] + [media.m4s] --cat--> [pipe] --ffprobe--> JSON з пакетами

Переваги:
✅ Не створює тимчасових файлів на диску
✅ Працює з будь-якими fMP4 сегментами
✅ Швидше, ніж повна конкатенація у файл

⚠️ Увага:
• Потребує, щоб init та media були сумісними (той самий codec, timescale...)
• Помилка у pipe може призвести до зависання ffprobe
```

### 🧮 Приклад вихідних даних пакетів (спрощено)

```json
{
  "packets": [
    {
      "pts_time": "0.000000",
      "dts_time": "0.000000",
      "duration_time": "0.033333",
      "size": "125000",
      "pos": "48",
      "flags": "K_"
    },
    {
      "pts_time": "0.033333",
      "dts_time": "0.033333",
      "duration_time": "0.033333",
      "size": "45000",
      "pos": "125048",
      "flags": "__"
    }
  ]
}
```

> 💡 **Інтерпретація**: Кожен пакет має `flags`, де перший символ `'K'` вказує на ключовий кадр. Це дозволяє фільтрувати I-frames без повторного прозонування кадрів.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Генерація I-frame-only плейлиста

```go
// У callFFmpeg (після завершення основного енкодингу):
func generateIFramePlaylist(masterFilename string) error {
    dir, filename := filepath.Split(masterFilename)
    
    // 🔹 Отримати список відео-сегментів з master playlist
    segments, err := parseHLSMasterPlaylist(masterFilename)
    if err != nil {
        return fmt.Errorf("failed to parse master playlist: %w", err)
    }
    
    // 🔹 Для кожного сегмента знайти I-frames
    var iframeEntries []IFrameEntry
    for _, seg := range segments {
        segPath := filepath.Join(dir, seg.URL)
        
        // 🔹 Прозвонити сегмент на ключові кадри
        frames, err := probeKeyFrames(segPath)
        if err != nil {
            log.Warnf("Failed to probe %s: %v", seg.URL, err)
            continue
        }
        
        // 🔹 Конвертувати у I-frame playlist entries
        for _, frame := range frames {
            time, _ := strconv.ParseFloat(frame.PktPtsTime, 64)
            iframeEntries = append(iframeEntries, IFrameEntry{
                Time:    time,
                URI:     seg.URL,
                ByteRange: fmt.Sprintf("%s@%s", frame.PktSize, frame.PktPos),
                Duration: 0.0,  // обчислити з наступного I-frame
            })
        }
    }
    
    // 🔹 Записати I-frame-only playlist
    iframePath := filepath.Join(dir, "iframes.m3u8")
    return writeIFramePlaylist(iframePath, iframeEntries)
}

type IFrameEntry struct {
    Time      float64
    URI       string
    ByteRange string
    Duration  float64
}

func writeIFramePlaylist(path string, entries []IFrameEntry) error {
    f, err := os.Create(path)
    if err != nil { return err }
    defer f.Close()
    
    f.WriteString("#EXTM3U\n")
    f.WriteString("#EXT-X-VERSION:7\n")
    f.WriteString("#EXT-X-I-FRAMES-ONLY\n")  // 🔹 Ключовий тег для I-frame-only playlist
    
    for _, e := range entries {
        f.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", e.Duration))
        f.WriteString(fmt.Sprintf("#EXT-X-BYTERANGE:%s\n", e.ByteRange))
        f.WriteString(fmt.Sprintf("#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=...,TIME-OFFSET=%.3f\n", e.Time))
        f.WriteString(e.URI + "\n")
    }
    
    return nil
}
```

### ✅ 2: Оптимізація прозонування для великих файлів

```go
// Для довгих CCTV записів (години) — прозвонювати тільки кожен N-й сегмент:
func probeKeyFramesSampled(filename string, sampleInterval time.Duration) ([]*ProbeFrame, error) {
    // 🔹 Отримати всі I-frames
    allFrames, err := probeKeyFrames(filename)
    if err != nil {
        return nil, err
    }
    
    // 🔹 Фільтрувати: залишити тільки кожен N-й кадр за часом
    var sampled []*ProbeFrame
    var lastTime float64 = -1
    
    for _, frame := range allFrames {
        currentTime, _ := strconv.ParseFloat(frame.PktPtsTime, 64)
        
        // 🔹 Додати кадр, якщо пройшло достатньо часу
        if lastTime < 0 || currentTime-lastTime >= sampleInterval.Seconds() {
            sampled = append(sampled, frame)
            lastTime = currentTime
        }
    }
    
    return sampled, nil
}

// Використання:
// 🔹 Для прев'ю таймлайну: один I-frame кожні 10 секунд
frames, _ := probeKeyFramesSampled("recording.mp4", 10*time.Second)
// 🔹 Для швидкого seek: один I-frame кожні 2 секунди
frames, _ = probeKeyFramesSampled("recording.mp4", 2*time.Second)
```

### ✅ 3: Кешування результатів прозонування

```go
// Щоб не прозвонювати той самий файл багато разів:
type ProbeCache struct {
    mu    sync.RWMutex
    cache map[string][]*ProbeFrame  // key = filepath, value = I-frames
    ttl   time.Duration
}

func (c *ProbeCache) GetOrProbe(filename string) ([]*ProbeFrame, error) {
    // 🔹 Спробувати отримати з кешу
    c.mu.RLock()
    if frames, ok := c.cache[filename]; ok {
        c.mu.RUnlock()
        return frames, nil
    }
    c.mu.RUnlock()
    
    // 🔹 Прозвонити файл
    frames, err := probeKeyFrames(filename)
    if err != nil {
        return nil, err
    }
    
    // 🔹 Зберегти у кеш
    c.mu.Lock()
    c.cache[filename] = frames
    c.mu.Unlock()
    
    return frames, nil
}

// Використання:
probeCache := &ProbeCache{
    cache: make(map[string][]*ProbeFrame),
    ttl:   1 * time.Hour,  // оновлювати кеш кожну годину
}

// При генерації I-frame playlist:
frames, err := probeCache.GetOrProbe(segmentPath)
```

### ✅ 4: Моніторинг продуктивності прозонування

```go
// monitoring.Monitor — метрики для ffprobe:
type ProbeMetrics struct {
    ProbeRequests  *prometheus.CounterVec  // кількість запитів на прозонування
    ProbeLatency   *prometheus.HistogramVec  // час прозонування
    KeyFramesFound *prometheus.CounterVec  // кількість знайдених I-frames
    ProbeErrors    *prometheus.CounterVec  // помилки ffprobe
}

// У процесі прозонування:
func probeWithMetrics(filename string, metrics *ProbeMetrics) ([]*ProbeFrame, error) {
    start := time.Now()
    frames, err := probeKeyFrames(filename)
    latency := time.Since(start)
    
    metrics.ProbeRequests.WithLabelValues(filename).Inc()
    metrics.ProbeLatency.WithLabelValues(filename).Observe(latency.Seconds())
    
    if err != nil {
        metrics.ProbeErrors.WithLabelValues(filename).Inc()
        return nil, err
    }
    
    metrics.KeyFramesFound.WithLabelValues(filename).Add(float64(len(frames)))
    return frames, nil
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Інтеграційний тест на прозонування

```go
func TestProbeKeyFrames_Integration(t *testing.T) {
    // 🔹 Підготувати тестовий відеофайл з відомими I-frames
    testFile := createTestVideoWithKeyframes(t)  // ваша helper-функція
    
    // 🔹 Прозвонити на ключові кадри
    frames, err := probeKeyFrames(testFile)
    assert.NoError(t, err)
    assert.NotEmpty(t, frames)
    
    // 🔹 Перевірити, що всі кадри дійсно ключові
    for _, frame := range frames {
        assert.True(t, frame.KeyFrame)
        assert.Equal(t, "I", frame.PictType)
        assert.NotEmpty(t, frame.PktPtsTime)
        assert.NotEmpty(t, frame.PktPos)
    }
    
    // 🔹 Перевірити таймінги (мають бути монотонними)
    var lastTime float64 = -1
    for _, frame := range frames {
        currentTime, _ := strconv.ParseFloat(frame.PktPtsTime, 64)
        assert.Greater(t, currentTime, lastTime)
        lastTime = currentTime
    }
}
```

### 🔹 Тест на конкатенацію init+media

```go
func TestProbePackets_WithInitSegment(t *testing.T) {
    // 🔹 Підготувати init та media сегменти
    initFile := createTestInitSegment(t)
    mediaFile := createTestMediaSegment(t)
    
    // 🔹 Прозвонити з конкатенацією
    packets, err := probePackets(initFile, mediaFile)
    assert.NoError(t, err)
    assert.NotEmpty(t, packets)
    
    // 🔹 Перевірити, що прапорці парсяться коректно
    keyFrameCount := 0
    for _, pkt := range packets {
        if pkt.isFromKeyFrame() {
            keyFrameCount++
        }
    }
    assert.Greater(t, keyFrameCount, 0)  // має бути хоча б один I-frame
}
```

### 🔹 Тест на обробку помилок ffprobe

```go
func TestProbeKeyFrames_InvalidFile(t *testing.T) {
    // 🔹 Невірний файл (не відео)
    _, err := probeKeyFrames("/dev/null")
    assert.Error(t, err)
    
    // 🔹 Невірний шлях
    _, err = probeKeyFrames("/nonexistent/file.mp4")
    assert.Error(t, err)
    
    // 🔹 Пошкоджений файл
    corruptFile := createCorruptVideoFile(t)
    _, err = probeKeyFrames(corruptFile)
    assert.Error(t, err)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `Flags` порожній | `isFromKeyFrame()` панікує або повертає false | 🔹 Додати валідацію: `if len(p.Flags) < 1 { return false }` (вже є) |
| Конкатенація зависає | `probePackets` не повертає результат | 🔹 Перевірити, що `cmdCat.Run()` завершується до `cmd.Wait()`; додати таймаути |
| Неправильний парсинг `pkt_pts_time` | Помилка `strconv.ParseFloat` | 🔹 Перевірити, що ffprobe виводить крапку як десятковий роздільник (локаль) |
| Великі файли прозвонюються довго | Затримки у генерації I-frame playlist | 🔹 Використовувати `-skip_frame nokey` (вже є); додати семплінг або кешування |
| Помилка "moov atom not found" | ffprobe не може прозвонити fMP4 сегмент | 🔹 Завжди передавати `initfilename` для fMP4; валідувати сумісність init+media |

### Приклад обробки локалей для парсингу чисел:

```go
func parseFloatSafe(s string) (float64, error) {
    // 🔹 Замінити кому на крапку для сумісності з різними локалями
    s = strings.Replace(s, ",", ".", -1)
    return strconv.ParseFloat(s, 64)
}

// Використання:
time, err := parseFloatSafe(frame.PktPtsTime)
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базове прозонування на I-frames:
func getIFrames(filePath string) ([]*ProbeFrame, error) {
    return probeKeyFrames(filePath)
}

// 2: Перевірка, чи пакет є ключовим кадром:
func isKeyFrame(packet *ProbePacket) bool {
    return packet.isFromKeyFrame()
}

// 3: Прозонування з конкатенацією для fMP4:
func probeFragmentedMP4(initPath, mediaPath string) ([]*ProbePacket, error) {
    return probePackets(initPath, mediaPath)
}

// 4: Фільтрація I-frames за інтервалом часу:
func sampleIFrames(frames []*ProbeFrame, interval time.Duration) []*ProbeFrame {
    var sampled []*ProbeFrame
    var lastTime float64 = -1
    
    for _, frame := range frames {
        currentTime, err := parseFloatSafe(frame.PktPtsTime)
        if err != nil { continue }
        
        if lastTime < 0 || currentTime-lastTime >= interval.Seconds() {
            sampled = append(sampled, frame)
            lastTime = currentTime
        }
    }
    return sampled
}

// 5: Логування результатів прозонування:
func logProbeResults(filename string, frames []*ProbeFrame) {
    log.Infof("Probed %s: found %d key frames", filename, len(frames))
    if len(frames) > 0 {
        first := frames[0]
        last := frames[len(frames)-1]
        log.Debugf("  First I-frame: time=%s, pos=%s, size=%s", 
            first.PktPtsTime, first.PktPos, first.PktSize)
        log.Debugf("  Last I-frame: time=%s, pos=%s, size=%s", 
            last.PktPtsTime, last.PktPos, last.PktSize)
    }
}
```

---

## 📊 Матриця параметрів ffprobe для I-frame детекції

```
Параметр ffprobe        | Значення          | Призначення
────────────────────────┼───────────────────┼─────────────────────────
-skip_frame nokey       | —                 | 🔹 Фільтр: тільки ключові кадри
-select_streams v       | —                 | 🔹 Аналізувати тільки відео
-show_frames            | —                 | 🔹 Детальні метадані кадрів
-show_packets           | —                 | 🔹 Метадані пакетів (для flags)
-show_entries packet=...| pts_time,flags... | 🔹 Обмежити вивід потрібними полями
-print_format json      | —                 | 🔹 Структурований вивід для парсингу
-hide_banner            | —                 | 🔹 Приховати версію (менше шуму)
-loglevel warning       | —                 | 🔹 Показувати тільки попередження
```

---

## 📚 Корисні посилання

- [FFmpeg ffprobe documentation](https://ffmpeg.org/ffprobe.html)
- [HLS I-frame playlist specification](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices#3249247)
- [Understanding MPEG-TS key frames](https://video.stackexchange.com/questions/14176/what-is-a-key-frame)
- [Go exec.Command pipe patterns](https://pkg.go.dev/os/exec#Cmd.StdoutPipe)

> 💡 **Ключова ідея**: Цей модуль — це "детектор ключових точок" у вашому відеопотоці. Він:
> - 🎯 Швидко знаходить I-frames без декодування всього відео
> - 🔧 Підтримує фрагментовані формати (fMP4) через конкатенацію pipe
> - ⚡ Масштабується через кешування та семплінг для довгих записів
> - 🛡️ Граційно обробляє помилки без зупинки всього пайплайну

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку паралельного прозонування кількох сегментів для прискорення
- 🧪 Написати integration-тест для перевірки коректності I-frame playlist у реальних плеєрах (AVPlayer, hls.js)
- 📈 Додати Prometheus-метрики для моніторингу часу прозонування та кількості знайдених I-frames по каналах

🛠️