# Глибоке роз'яснення: Тести `iframe_playlist_generator` — валідація генерації I-frame-only плейлистів

Цей файл містить **комплексні модульні тести** для функціоналу генерації I-frame-only HLS плейлистів. Тести перевіряють прозонування через ffprobe, парсинг плейлистів, витягування метаданих I-frames та побудову фінальних плейлистів.

---

## 🎯 Навіщо ці тести потрібні у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ Тести I-frame generator у контексті:   │
│                                         │
│ 🔹 Гарантія коректності byte-range:    │
│   • Перевірка позицій/розмірів I-frames│
│   • Запобігання помилкам "416 Range Not│
│     Satisfiable" у плеєрах             │
│                                         │
│ 🔹 Валідація сумісності з HLS spec:    │
│   • #EXT-X-I-FRAMES-ONLY тег           │
│   • #EXT-X-BYTERANGE формат            │
│   • Коректний bandwidth розрахунок     │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Стабільність швидкого seek у       │
│     довгих записах                     │
│   • Детекція регресій при оновленні    │
│     кодека/енкодера                    │
│   • Автоматична перевірка нових        │
│     форматів сегментів (TS/fMP4)       │
└─────────────────────────────────────────┘
```

---

## 🔧 Архітектура тестів: стратегія валідації

### 📦 Глобальна константа точності

```go
var eps = 0.001  // 🔹 Допустима похибка для порівняння float (1 мілісекунда)
```

> 💡 **Чому це важливо**: Часові значення з ffprobe можуть мати мікрорізницю через округлення. `eps` дозволяє порівнювати `float64` без хибних невдач тестів.

---

## 🔍 Тест `TestFFprobe1` / `TestFFprobe2`: базове прозонування

```go
func TestFFprobe1(t *testing.T) {
    // 🔹 Прозвонити master playlist (m3u8 файл)
    _, err := probePackets("", "tests/bigbuckbunny-400k.m3u8")
    if err != nil {
        t.Error("Cannot probe file:", err)
    }
}

func TestFFprobe2(t *testing.T) {
    // 🔹 Прозвонити транспортний сегмент (.ts файл)
    _, err := probePackets("", "tests/bigbuckbunny-400k-00004.ts")
    if err != nil {
        t.Error("Cannot probe file:", err)
    }
}
```

### 🎯 Що перевіряється:

| Аспект | Очікувана поведінка | Чому важливо |
|--------|-------------------|--------------|
| **Доступність ffprobe** | Команда виконується без помилок | Без ffprobe неможлива генерація I-frame playlist |
| **Парсинг JSON виходу** | `json.Unmarshal` не панікує | Невірний формат → падіння всього пайплайну |
| **Підтримка форматів** | Працює з `.m3u8` та `.ts` | CCTV може використовувати різні формати сегментів |

### 🔹 Типові помилки та їх діагностика:

```
❌ "exec: \"ffprobe\": executable file not found in $PATH"
   → Встановити ffmpeg/ffprobe у системі або Docker-контейнері

❌ "invalid character 'x' after top-level value"
   → ffprobe вивів помилку у stdout замість stderr; перевірити `-loglevel warning`

❌ "unexpected end of JSON input"
   → Файл пошкоджений або ffprobe не зміг його прочитати; перевірити валідність вхідного відео
```

---

## 🔍 Тест `TestVariantsFromMaster`: парсинг master playlist

```go
func TestVariantsFromMaster(t *testing.T) {
    masterFile := "tests/master.m3u8"
    
    // 🔹 Викликати універсальний парсер
    _, variants, ty, err := variantsFromMaster(masterFile)
    if err != nil {
        t.Error("Error running function:", err)
        return
    }
    
    // 🔹 Перевірити тип плейлиста
    if ty != m3u8.MASTER {
        t.Error("Unexpected type for playlist")
        return
    }
    
    // 🔹 Перевірити кількість варіантів (якість)
    if len(variants) != 3 {
        t.Error("Unexpected number of variants")
        return
    }
    
    // 🔹 Завантажити контент медіа-плейлистів
    fillVariants("tests/", variants...)
    
    // 🔹 Debug: вивести перший chunklist для візуальної перевірки
    log.Println(variants[0].Chunklist)
}
```

### 🎯 Що перевіряється:

```
📁 tests/master.m3u8 (приклад вмісту):
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-STREAM-INF:BANDWIDTH=400000,RESOLUTION=640x360
bigbuckbunny-400k.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=842x478
bigbuckbunny-800k.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1400000,RESOLUTION=1280x720
bigbuckbunny-1400k.m3u8

✅ Перевірки:
• Тип = MASTER (не MEDIA)
• 3 варіанти (400k, 800k, 1400k)
• fillVariants успішно завантажує кожен медіа-плейлист
```

### 🔹 Чому `fillVariants` важлива:

```go
// Без fillVariants:
variant.Chunklist = nil  // ❌ iframePlaylistForVariant поверне помилку

// З fillVariants:
variant.Chunklist = &MediaPlaylist{  // ✅ містить список сегментів
    Segments: []*Segment{
        {URI: "segment0.ts", Duration: 6.0},
        {URI: "segment1.ts", Duration: 6.0},
        // ...
    },
}
```

---

## 🔍 Тест `TestIFramePlaylistSegment1`: витягування I-frames з сегмента

```go
func TestIFramePlaylistSegment1(t *testing.T) {
    segmentURI := "tests/bigbuckbunny-400k-00001.ts"
    
    // 🔹 Викликати основну функцію витягування
    p, err := iframeEntryForSegment("", 0, segmentURI)
    if err != nil {
        t.Error("Error running iframeEntryForSegment:", err)
        return
    }
    
    // 🔹 Перевірити кількість знайдених I-frames
    length := len(p)
    if length != 2 {  // 🔹 Очікуємо рівно 2 I-frames у цьому сегменті
        t.Error("Bad length:", length)
    }
    if length < 1 { return }  // 🔹 Далі тестуємо тільки якщо є хоча б один
    
    // 🔹 Перевірити перший I-frame детальніше
    actualFirstFrame := p[0]
    expectedFirstFrame := &IFrameEntry{
        SegmentURI:     segmentURI,
        PacketPosition: 3008,   // 🔹 Байтова позиція у файлі
        PacketSize:     376,    // 🔹 Розмір I-frame у байтах
        Duration:       9.08,   // 🔹 Тривалість до наступного I-frame (сек)
    }
    
    // 🔹 Порівняння з допуском eps для float
    if actualFirstFrame.PacketPosition != expectedFirstFrame.PacketPosition {
        t.Error("Wrong packet position. Expected", expectedFirstFrame.PacketPosition, "got", actualFirstFrame.PacketPosition)
    }
    if actualFirstFrame.PacketSize != expectedFirstFrame.PacketSize {
        t.Error("Wrong packet size. Expected", expectedFirstFrame.PacketSize, "got", actualFirstFrame.PacketSize)
    }
    if math.Abs(actualFirstFrame.Duration-expectedFirstFrame.Duration) > eps {
        t.Error("Wrong duration. Expected", expectedFirstFrame.Duration, "got", actualFirstFrame.Duration)
    }
    if actualFirstFrame.SegmentURI != expectedFirstFrame.SegmentURI {
        t.Error("Wrong segment URI. Expected", expectedFirstFrame.SegmentURI, "got", actualFirstFrame.SegmentURI)
    }
}
```

### 🎯 Ключові аспекти валідації:

#### 🔹 `PacketPosition: 3008` — коректність байтової позиції

```
Чому це важливо:
• HLS #EXT-X-BYTERANGE використовує позицію для швидкого seek
• Неправильна позиція → плеєр читає не ті байти → артефакти/помилки

Як перевіряється:
• ffprobe повертає `pkt_pos` для кожного пакету
• `iframeEntryForSegment` коригує на `initSize` (для fMP4)
• Тест порівнює з "золотим еталоном" (попередньо валідоване значення)
```

#### 🔹 `PacketSize: 376` — розрахунок розміру I-frame

```
Алгоритм розрахунку (з коду):
  size = p.Size
  if i < nbPkts-1 {
      pNext := packets[i+1]
      size = pNext.Pos - p.Pos  // різниця позицій
  }
  size += 188  // 🔹 TS packet header (див. нижче)

Чому +188:
• MPEG-TS пакети мають фіксований заголовок 188 байт
• ffprobe повертає розмір "корисного навантаження"
• Для коректного byte-range потрібно включити заголовок

⚠️ Проблема: Це працює тільки для TS! Для fMP4 це призведе до помилки.
```

#### 🔹 `Duration: 9.08` — тривалість до наступного I-frame

```
Як розраховується:
• Початкове значення = p.DurationTime (тривалість пакету)
• Для кожного наступного не-ключового пакету: lastEntry.Duration += p.DurationTime
• Коли знайдено наступний I-frame: зберегти поточний, почати новий

Чому це важливо:
• Тривалість використовується у #EXTINF у I-frame playlist
• Неправильна тривалість → плеєр показує кадр занадто довго/коротко
```

---

## 🔍 Тест `TestIFramePlaylistSegment4`: валідація на іншому сегменті

```go
func TestIFramePlaylistSegment4(t *testing.T) {
    segmentURI := "tests/bigbuckbunny-400k-00004.ts"
    
    p, err := iframeEntryForSegment("", 0, segmentURI)
    if err != nil {
        t.Error("Error running iframeEntryForSegment:", err)
        return
    }
    
    // 🔹 Цей сегмент має 4 I-frames (інша сцена = інша частота ключових кадрів)
    length := len(p)
    if length != 4 {
        t.Error("Bad length:", length)
    }
    if length < 2 { return }
    
    // 🔹 Перевіряємо другий I-frame (індекс 1), не перший
    actualFirstFrame := p[1]
    expectedFirstFrame := &IFrameEntry{
        SegmentURI:     segmentURI,
        PacketPosition: 28388,  // 🔹 Значно більша позиція (середина файлу)
        PacketSize:     4888,   // 🔹 Більший розмір (складніша сцена)
        Duration:       0.04,   // 🔹 Дуже коротка тривалість (швидка зміна кадрів)
    }
    
    // 🔹 Ті самі перевірки, що й у TestIFramePlaylistSegment1
    // ...
}
```

### 🎯 Чому тестуємо різні сегменти:

```
Сегмент 1 (00001.ts):
• Початок відео, статична сцена
• 2 I-frames, малі розміри, довгі інтервали

Сегмент 4 (00004.ts):
• Середина відео, динамічна сцена
• 4 I-frames, більші розміри, короткі інтервали

✅ Мета: Перевірити, що алгоритм працює для різних типів контенту
```

---

## 🔍 Тест `TestPlaylistForVariant`: інтеграційна перевірка

```go
func TestPlaylistForVariant(t *testing.T) {
    masterFile := "tests/bigbuckbunny.m3u8"
    
    // 🔹 Отримати варіанти з master playlist
    _, variants, _, _ := variantsFromMaster(masterFile)
    
    dir := "tests/"
    
    // 🔹 Завантажити контент медіа-плейлистів
    fillVariants(dir, variants...)
    
    // 🔹 Згенерувати I-frame-only playlist для першого варіанту
    p, err := iframePlaylistForVariant(dir, variants[0])
    if err != nil {
        t.Error("Cannot run `iframePlaylistForVariant`", err)
        return
    }
    
    // 🔹 Перевірити кількість сегментів у фінальному playlist
    if len(p.Segments) != 17 {  // 🔹 Очікуємо 17 I-frames у всьому відео
        t.Error("Unexpected number of segments:", len(p.Segments))
        return
    }
}
```

### 🎯 Що перевіряється на інтеграційному рівні:

```
🔄 Повний пайплайн:
1. variantsFromMaster: парсинг master.m3u8 → 3 варіанти
2. fillVariants: завантаження bigbuckbunny-400k.m3u8 → список сегментів
3. iframePlaylistForVariant:
   • Для кожного сегмента: iframeEntryForSegment → I-frame entries
   • Збірка entries у MediaPlaylist з #EXT-X-I-FRAMES-ONLY
4. Перевірка: 17 сегментів у фінальному playlist

✅ Це гарантує, що всі компоненти працюють разом коректно
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Додавання тестових даних для вашого контенту

```go
// 🔹 Створіть тестові сегменти з вашого CCTV потоку:
func createCCTVTestFixtures(t *testing.T) (string, []string) {
    tempDir := t.TempDir()
    
    // 🔹 Згенерувати тестовий HLS з відомими параметрами
    segments := generateTestHLS(t, tempDir, HLSConfig{
        Duration: 60 * time.Second,  // 1 хвилина запису
        GOP:      60,                // I-frame кожні 2 секунди @ 30fps
        Codec:    "libx264",
        Format:   "mpegts",          // або "fmp4"
    })
    
    // 🔹 Створити master playlist
    masterPath := filepath.Join(tempDir, "master.m3u8")
    writeTestMasterPlaylist(t, masterPath, segments)
    
    return masterPath, segments
}

// 🔹 Використання у тестах:
func TestCCTV_IFrameGeneration(t *testing.T) {
    masterPath, segments := createCCTVTestFixtures(t)
    
    // 🔹 Запустити генерацію
    err := GeneratePlaylist(filepath.Dir(masterPath), filepath.Base(masterPath))
    assert.NoError(t, err)
    
    // 🔹 Перевірити вихідні I-frame playlist
    for _, seg := range segments {
        iframePath := filepath.Join(filepath.Dir(masterPath), iframeOnlyFilename(seg))
        assert.FileExists(t, iframePath)
        
        // 🔹 Валідувати byte-range
        validateIFramePlaylist(t, iframePath)
    }
}
```

### ✅ 2: Параметризовані тести для різних форматів

```go
// 🔹 Тестова матриця: TS vs fMP4, різні кодеки, різні GOP
var iframeTestCases = []struct {
    name           string
    format         string  // "mpegts" or "fmp4"
    codec          string  // "libx264", "libx265"
    gop            int     // інтервал ключових кадрів
    expectedIFrames int    // очікувана кількість у тестовому відео
}{
    {"H264_TS_GOP60", "mpegts", "libx264", 60, 30},   // 60 сек / 2 сек = 30 I-frames
    {"H264_fMP4_GOP60", "fmp4", "libx264", 60, 30},
    {"H265_TS_GOP120", "mpegts", "libx265", 120, 15}, // 60 сек / 4 сек = 15 I-frames
}

func TestIFrameGeneration_Matrix(t *testing.T) {
    for _, tc := range iframeTestCases {
        t.Run(tc.name, func(t *testing.T) {
            // 🔹 Згенерувати тестові дані з параметрами
            dir := generateTestHLSWithParams(t, tc.format, tc.codec, tc.gop)
            
            // 🔹 Запустити генерацію
            err := GeneratePlaylist(dir, "master.m3u8")
            assert.NoError(t, err, "Generation failed for %s", tc.name)
            
            // 🔹 Перевірити кількість I-frames (з допуском ±10%)
            iframePath := filepath.Join(dir, "stream_0_I-FRAME-ONLY.m3u8")
            count := countIFrameSegments(t, iframePath)
            
            tolerance := tc.expectedIFrames / 10  // ±10%
            assert.InDelta(t, tc.expectedIFrames, count, tolerance,
                "Wrong I-frame count for %s: expected ~%d, got %d", 
                tc.name, tc.expectedIFrames, count)
        })
    }
}
```

### ✅ 3: Моніторинг якості генерації у production

```go
// 🔹 Prometheus-метрики для I-frame генерації:
type IFrameQualityMetrics struct {
    ByteRangeErrors    *prometheus.CounterVec  // помилки "416 Range Not Satisfiable"
    IFramesPerMinute   *prometheus.GaugeVec    // щільність I-frames у записі
    GenerationLatency  *prometheus.HistogramVec  // час генерації на сегмент
    BandwidthAccuracy  *prometheus.HistogramVec  // відхилення розрахункового bandwidth
}

// 🔹 Валідація byte-range перед записом:
func validateByteRange(segmentPath string, pos, size uint64) error {
    fi, err := os.Stat(segmentPath)
    if err != nil { return err }
    
    fileSize := uint64(fi.Size())
    if pos >= fileSize {
        return fmt.Errorf("position %d exceeds file size %d", pos, fileSize)
    }
    if pos+size > fileSize {
        return fmt.Errorf("range [%d, %d) exceeds file size %d", pos, pos+size, fileSize)
    }
    return nil
}

// 🔹 Логування підозрілих значень:
func logSuspiciousIFrame(entry *IFrameEntry, segmentPath string) {
    if entry.PacketSize > 10*1024*1024 {  // >10 MB для одного I-frame?
        log.Warnf("Suspicious large I-frame in %s: size=%d, pos=%d", 
            segmentPath, entry.PacketSize, entry.PacketPosition)
    }
    if entry.Duration > 30.0 {  // >30 сек між I-frames?
        log.Warnf("Long I-frame interval in %s: duration=%.2fs", 
            segmentPath, entry.Duration)
    }
}
```

### ✅ 4: Graceful degradation при помилках прозонування

```go
// 🔹 Стратегія: якщо сегмент не можна прозвонити — пропустити, не зупиняти весь процес
func iframeEntryForSegmentSafe(initURI string, initSize uint, segmentURI string) ([]*IFrameEntry, error) {
    entries, err := iframeEntryForSegment(initURI, initSize, segmentURI)
    if err != nil {
        log.Warnf("Failed to probe %s: %v. Skipping I-frame extraction for this segment.", 
            segmentURI, err)
        // 🔹 Повернути порожній список замість помилки
        return []*IFrameEntry{}, nil
    }
    return entries, nil
}

// 🔹 Використання у iframePlaylistForVariant:
for i, segment := range variant.Chunklist.Segments {
    entriesPartial, err := iframeEntryForSegmentSafe(initFilename, initSize, filepath.Join(dir, segment.URI))
    if err != nil {
        // ❌ Ніколи не має статися, бо Safe версія не повертає помилку
        continue
    }
    entries = append(entries, entriesPartial...)
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на fMP4 сегменти з init файлом

```go
func TestIFrameEntryForSegment_fMP4(t *testing.T) {
    // 🔹 Підготувати init + media сегменти
    initFile := "tests/init.mp4"
    mediaFile := "tests/segment.m4s"
    
    // 🔹 Прозвонити з конкатенацією
    entries, err := iframeEntryForSegment(initFile, 1024, mediaFile)  // initSize = 1024 байти
    assert.NoError(t, err)
    assert.NotEmpty(t, entries)
    
    // 🔹 Перевірити, що позиції кореговані на initSize
    for _, entry := range entries {
        // 🔹 PacketPosition має бути < розміру media файлу (без init)
        fi, _ := os.Stat(mediaFile)
        assert.Less(t, entry.PacketPosition, uint(fi.Size()))
    }
}
```

### 🔹 Тест на обробку сегментів без I-frames (помилковий випадок)

```go
func TestIFrameEntryForSegment_NoKeyframes(t *testing.T) {
    // 🔹 Створити "пошкоджений" файл без ключових кадрів
    corruptFile := createFileWithoutKeyframes(t)
    
    entries, err := iframeEntryForSegment("", 0, corruptFile)
    // 🔹 Очікуємо: помилка або порожній список, але не паніка
    if err != nil {
        log.Printf("Expected warning for file without keyframes: %v", err)
    }
    // 🔹 Головне — функція завершилась коректно
}
```

### 🔹 Бенчмарк продуктивності прозонування

```go
func BenchmarkIFrameEntryForSegment(b *testing.B) {
    segmentURI := "tests/bigbuckbunny-400k-00001.ts"
    
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        _, err := iframeEntryForSegment("", 0, segmentURI)
        if err != nil {
            b.Fatal(err)
        }
    }
}

// Очікувані результати:
// BenchmarkIFrameEntryForSegment-8    100    15000000 ns/op    200000 B/op    500 allocs/op
// 🔹 ~15 мс на сегмент — прийнятно для фоновой генерації
// 🔹 Для real-time: додати кешування результатів прозонування
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `+188` для fMP4 | Неправильні byte-range, помилки 416 у плеєрі | 🔹 Визначати формат сегмента; для fMP4 не додавати 188 |
| Неправильний bandwidth (`/10`) | Плеєр вибирає неоптимальний варіант | 🔹 Реалізувати правильний розрахунок: `sum(sizes)/duration` |
| `TargetDuration -= 1` | Невідомо, навіщо; може зламати сумісність | 🔹 Дослідити причину; якщо не потрібно — видалити |
| Конкатенація init+media зависає | `probePackets` не повертає результат | 🔹 Додати таймаути; перевірити коректність pipe |
| Помилка "no key frame" | Сегмент без I-frames (неможливо для валідного відео) | 🔹 Перевірити GOP налаштування енкодера; додати валідацію вхідних даних |

### Приклад визначення формату сегмента:

```go
func isFragmentedMP4(filePath string) bool {
    // 🔹 Перевірити розширення
    ext := strings.ToLower(filepath.Ext(filePath))
    if ext == ".m4s" || ext == ".mp4" || ext == ".cmfv" {
        return true
    }
    
    // 🔹 Перевірити magic bytes (box structure)
    f, err := os.Open(filePath)
    if err != nil { return false }
    defer f.Close()
    
    buf := make([]byte, 16)
    n, _ := f.Read(buf)
    if n < 16 { return false }
    
    // 🔹 fMP4 має "ftyp" box на початку
    return bytes.Contains(buf, []byte("ftyp"))
}

// Використання у розрахунку розміру:
func calculatePacketSize(p *ProbePacket, nextP *ProbePacket, isFMP4 bool) uint {
    size := p.Size
    if nextP != nil {
        size = nextP.Pos - p.Pos
    }
    
    // 🔹 Додати заголовок тільки для TS
    if !isFMP4 {
        size += 188  // MPEG-TS packet header
    }
    
    return size
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Запуск тестів для нового формату:
func TestNewFormat_IFrameGeneration(t *testing.T) {
    if !isFFprobeAvailable() {
        t.Skip("ffprobe not found in PATH")
    }
    
    // 🔹 Згенерувати тестові дані
    dir := generateTestHLS(t, "new_format", HLSConfig{
        Format: "fmp4",
        Codec:  "libx265",
        GOP:    120,
    })
    
    // 🔹 Запустити генерацію
    err := GeneratePlaylist(dir, "master.m3u8")
    assert.NoError(t, err)
    
    // 🔹 Валідувати вихід
    validateIFramePlaylist(t, filepath.Join(dir, "stream_0_I-FRAME-ONLY.m3u8"))
}

// 2: Helper для валідації I-frame playlist:
func validateIFramePlaylist(t *testing.T, playlistPath string) {
    content, err := os.ReadFile(playlistPath)
    assert.NoError(t, err)
    
    // 🔹 Перевірити обов'язкові теги
    assert.Contains(t, string(content), "#EXT-X-I-FRAMES-ONLY")
    assert.Contains(t, string(content), "#EXT-X-BYTERANGE:")
    assert.Contains(t, string(content), "#EXTINF:")
    
    // 🔹 Перевірити формат byte-range
    lines := strings.Split(string(content), "\n")
    for i, line := range lines {
        if strings.HasPrefix(line, "#EXT-X-BYTERANGE:") {
            // 🔹 Формат: "size@position"
            rangeSpec := strings.TrimPrefix(line, "#EXT-X-BYTERANGE:")
            parts := strings.Split(rangeSpec, "@")
            assert.Len(t, parts, 2, "Invalid BYTERANGE format: %s", line)
            
            // 🔹 Перевірити, що наступний рядок — URI
            if i+1 < len(lines) {
                uri := strings.TrimSpace(lines[i+1])
                assert.NotEmpty(t, uri, "Missing URI after BYTERANGE")
                assert.NotContains(t, uri, "#", "URI should not be a comment")
            }
        }
    }
}

// 3: Моніторинг у production:
func logIFrameGenerationStats(channelID string, startTime time.Time, 
                            segmentsProbed, iframesFound int, err error) {
    
    latency := time.Since(startTime)
    
    if err != nil {
        log.Errorf("Channel %s: I-frame generation failed after %v: %v", 
            channelID, latency, err)
        return
    }
    
    log.Infof("Channel %s: I-frame generation complete in %v: %d segments probed, %d I-frames found", 
        channelID, latency, segmentsProbed, iframesFound)
    
    // 🔹 Попередження про підозрілі значення
    if segmentsProbed > 0 {
        density := float64(iframesFound) / float64(segmentsProbed)
        if density < 0.1 {  // менше 1 I-frame на 10 сегментів?
            log.Warnf("Channel %s: Low I-frame density: %.2f I-frames/segment", 
                channelID, density)
        }
    }
}
```

---

## 📊 Матриця очікуваних значень для тестів

```
Тестовий файл              | Очікувані I-frames | PacketPosition | PacketSize | Duration
───────────────────────────┼───────────────────┼────────────────┼────────────┼─────────
bigbuckbunny-400k-00001.ts | 2                 | 3008           | 376        | 9.08s
bigbuckbunny-400k-00004.ts | 4                 | 28388          | 4888       | 0.04s
bigbuckbunny.m3u8 (всьо)   | 17                | -              | -          | -

🔹 Ці значення отримані експериментально з реального відео
🔹 При зміні енкодера/GOP/бітрейту — оновити еталони
🔹 Використовувати math.Abs(...-...) > eps для float порівнянь
```

---

## 📚 Корисні посилання

- [HLS I-frame playlist specification](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices#3249247)
- [FFmpeg ffprobe documentation](https://ffmpeg.org/ffprobe.html)
- [Go testing best practices](https://go.dev/doc/tutorial/add-a-test)
- [m3u8 library for Go](https://github.com/grafov/m3u8)

> 💡 **Ключова ідея**: Ці тести — це "страхова поліс" вашого I-frame generator. Вони:
> - 🎯 Гарантують коректність byte-range для швидкого seek у плеєрах
> - 🔍 Детектують регресії при зміні кодека/енкодера/формату
> - ⚡ Допомагають оптимізувати продуктивність через бенчмарки
> - 🛡️ Запобігають production-помилкам через валідацію edge cases

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку тестування fMP4 сегментів з автоматичним визначенням формату
- 🧪 Написати property-based тести для генерації випадкових сегментів з перевіркою інваріантів
- 📈 Додати Prometheus-метрики для моніторингу якості I-frame генерації у production

🛠️