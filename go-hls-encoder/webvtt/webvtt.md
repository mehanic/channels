# Глибоке роз'яснення: `webvtt.Segment` — сегментація WebVTT субтитрів для HLS

Цей файл містить **логіку розбиття WebVTT файлів на сегменти**, синхронізовані з відео-сегментами HLS. Він створює окремі `.vtt` файли для кожного сегмента та генерує відповідний `.m3u8` плейлист для субтитрів.

---

## 🎯 Навіщо сегментація субтитрів потрібна у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ Сегментація WebVTT у контексті HLS:    │
│                                         │
│ 🔹 Синхронізація з відео:              │
│   • Кожен відео-сегмент має відповідний│
│     файл субтитрів (.vtt)              │
│   • Плеєр завантажує тільки потрібні   │
│     субтитри для поточного сегмента    │
│                                         │
│ 🔹 Ефективність:                       │
│   • Не завантажувати всі субтитри      │
│     одразу (економія трафіку)          │
│   • Паралельне завантаження сегментів  │
│   • Кешування окремих сегментів        │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Підтримка довгих записів (години)  │
│   • Швидкий seek без завантаження      │
│     всього файлу субтитрів             │
│   • Адаптація до змінної довжини       │
│     сегментів                          │
└─────────────────────────────────────────┘
```

---

## 🔧 Типи даних: `SubtitleBlock`

```go
type SubtitleBlock struct {
    StartTime, EndTime time.Duration  // 🔹 Час початку/кінцю блоку (від початку файлу)
    Lines              bytes.Buffer   // 🔹 Вміст блоку: таймкоди + текст + стилі
}
```

### 🎯 Приклад використання:

```
Вхідний WebVTT:
  00:01:23.456 --> 00:01:26.789 align:center
  Привіт, світе!

Результат у SubtitleBlock:
• StartTime = 1m23.456s
• EndTime = 1m26.789s
• Lines = "00:01:23.456 --> 00:01:26.789 align:center\nПривіт, світе!\n"
```

> 💡 **Важливо**: `Lines` містить **весь оригінальний рядок**, включаючи таймкоди та inline-стилі. Це дозволяє зберегти форматування при записі.

---

## 🔍 Функція `Segment`: вхідна точка сегментації

```go
func Segment(r io.Reader, targetDuration time.Duration, outputDir, name string) error {
    // 🔹 1. Створити канал для блоків
    c := make(chan SubtitleBlock)
    
    // 🔹 2. Запустити парсинг у фоні
    go ReadFromWebVTT(r, c)
    
    // 🔹 3. Виконати сегментацію
    return segment(c, targetDuration, outputDir, name)
}
```

### 🎯 Архітектура конвеєра:

```
[WebVTT файл] 
       │
       ▼
[ReadFromWebVTT] --chan SubtitleBlock--> [segment()] 
                                               │
                                               ▼
                              [writeBlocksToVTT] + [addSegmentToPlaylist]
                                               │
                                               ▼
                              [outputDir/name-00000.vtt] + [name.m3u8]

Переваги:
✅ Паралельність: парсинг і запис працюють одночасно
✅ Потоковість: не завантажує весь файл у пам'ять
✅ Гнучкість: можна додати фільтрацію/трансформацію між етапами
```

---

## 🔍 Функція `segment`: основна логіка розбиття

```go
func segment(c <-chan SubtitleBlock, targetDuration time.Duration, 
            outputDir, name string) error {
    
    // 🔹 1. Створити M3U8 плейлист для субтитрів
    playlistPath := filepath.Join(outputDir, name+".m3u8")
    playlist, err := createPlaylistFile(playlistPath, targetDuration)
    if err != nil { return err }
    defer closePlaylistFile(playlist)  // 🔹 Додати #EXT-X-ENDLIST при завершенні
    
    // 🔹 2. Ініціалізація стану сегментації
    var blocks []SubtitleBlock           // 🔹 Блоки поточного сегмента
    var startTime time.Duration = 0      // 🔹 Початок поточного сегмента
    var endTime time.Duration = 0        // 🔹 Кінець поточного сегмента
    var count uint = 0                   // 🔹 Лічильник сегментів
    
    // 🔹 3. Отримати перший блок
    b, ok := <-c
    segmentAdded := false                // 🔹 Прапорець: чи додано блок у поточний сегмент
    
    // 🔹 4. Основний цикл обробки блоків
    for ok {
        // ── Крок 4.1: Визначити кінець сегмента з толерантністю ──
        newEnd := b.EndTime
        if newEnd-startTime > targetDuration+500*time.Millisecond {
            // 🔹 Блок задовгий для сегмента → обрізати кінець
            newEnd = targetDuration + startTime
        }
        endTime = newEnd
        
        // ── Крок 4.2: Додати блок, якщо він частково у сегменті ──
        if b.StartTime < endTime {
            blocks = append(blocks, b)
            segmentAdded = true
        }
        
        // ── Крок 4.3: Чи досягнуто цільової довжини сегмента? ──
        if endTime-startTime >= targetDuration {
            // 🔹 Так → записати сегмент
            createSegment(name, count, outputDir, blocks, playlist, startTime, endTime)
            
            // 🔹 Підготувати наступний сегмент
            blocks = make([]SubtitleBlock, 0, 5)  // 🔹 Передбачити ~5 блоків
            startTime = endTime
            count += 1
            
            // ── Крок 4.4: Обробка блоків на межі сегментів ──
            if b.StartTime < startTime && b.EndTime > startTime {
                // 🔹 Блок "перетинає" межу → не читати наступний, обробити цей знову
                segmentAdded = false
            }
        }
        
        // ── Крок 4.5: Отримати наступний блок (якщо поточний вже оброблено) ──
        if segmentAdded {
            b, ok = <-c
            segmentAdded = false
        }
    }
    
    // 🔹 5. Записати останній (неповний) сегмент
    if endTime-startTime > 0 {
        createSegment(name, count, outputDir, blocks, playlist, startTime, endTime)
    }
    
    return nil
}
```

### 🎯 Ключові аспекти логіки

#### 🔹 Толерантність +500мс для гнучкої сегментації

```go
if newEnd-startTime > targetDuration+500*time.Millisecond {
    newEnd = targetDuration + startTime
}
```

**Чому це потрібно:**
```
Проблема:
• Субтитри не завжди ідеально вирівняні з відео-сегментами
• Блок може закінчуватися на 6.2с при targetDuration=6с
• Жорстке обрізання → втрата частини репліки

Рішення:
• Дозволити +500мс "переливу" за межі сегмента
• Якщо блок задовгий → обрізати endTime до targetDuration
• Це баланс між цілісністю реплік та синхронізацією

Приклад:
• targetDuration = 6с
• Блок: 5.8с → 6.3с (тривалість 0.5с)
• Без толерантності: блок не вміщується → помилка
• З толерантністю: endTime = min(6.3, 6.0+0.5) = 6.3с → блок зберігається ✅
```

#### 🔹 Обробка блоків на межі сегментів ("astride")

```go
if b.StartTime < startTime && b.EndTime > startTime {
    // 🔹 Блок перетинає межу: [---блок---|---новий сегмент---]
    segmentAdded = false  // 🔹 Не читати наступний блок, обробити цей знову
}
```

**Візуалізація:**
```
Сегмент 1: [0с -------- 6с]
Сегмент 2:           [6с -------- 12с]

Блок: [5с -------- 7с]  ← перетинає межу!

Обробка:
1. Блок додається у Сегмент 1 (бо StartTime=5с < 6с)
2. Досягнуто targetDuration → записати Сегмент 1
3. startTime = 6с (початок Сегмента 2)
4. Перевірка: b.StartTime(5с) < startTime(6с) && b.EndTime(7с) > startTime(6с) → true!
5. segmentAdded = false → не читати новий блок
6. Наступна ітерація: той самий блок додається у Сегмент 2 (бо StartTime=5с < endTime=12с)

Результат: блок з'являється у ОБИДВОХ сегментах ✅
Це запобігає втраті реплік на межі сегментів.
```

> ⚠️ **Увага**: Це призводить до дублювання блоків на межах. Для деяких плеєрів це може бути небажаним. Альтернатива: обрізати блок на межі або використовувати WebVTT `REGION` для точного позиціонування.

#### 🔹 Прапорець `segmentAdded`: уникнення пропуску блоків

```
Складність:
• Блок може бути доданий у поточний сегмент
• Але сегмент ще не заповнений → не записувати
• Потрібно отримати наступний блок для продовження

Рішення:
• segmentAdded = true після додавання блоку
• Якщо сегмент записано → скинути прапорець
• Тільки якщо segmentAdded=true → читати наступний блок

Це запобігає:
❌ Пропуску блоків при швидкій зміні сегментів
❌ Подвійному читанню одного блоку (окрім випадку "astride")
❌ Зависанню на порожньому каналі
```

---

## 🔍 Допоміжні функції: запис сегментів та плейлистів

### `createSegment`: створення файлу сегмента

```go
func createSegment(basename string, segmentCount uint, outputDir string, 
                  blocks []SubtitleBlock, playlist *os.File, 
                  startTime, endTime time.Duration) {
    
    // 🔹 Ім'я файлу: name-00000.vtt, name-00001.vtt...
    segmentName := fmt.Sprintf("%s-%05d.vtt", basename, segmentCount)
    segmentFilepath := filepath.Join(outputDir, segmentName)
    
    // 🔹 Записати блоки у WebVTT файл
    writeBlocksToVTT(blocks, segmentFilepath)
    
    // 🔹 Додати запис у M3U8 плейлист
    addSegmentToPlaylist(playlist, endTime-startTime, segmentName)
}
```

### `writeBlocksToVTT`: запис блоків у файл

```go
func writeBlocksToVTT(blocks []SubtitleBlock, filepath string) error {
    var dataBuffer bytes.Buffer
    
    // 🔹 Заголовок WebVTT
    dataBuffer.WriteString("WEBVTT\n\n")
    
    // 🔹 Додати всі блоки (включаючи таймкоди та стилі)
    for _, b := range blocks {
        dataBuffer.Write(b.Lines.Bytes())  // 🔹 Lines вже містить повний формат
    }
    
    // 🔹 Записати у файл
    return ioutil.WriteFile(filepath, dataBuffer.Bytes(), 0644)
}
```

> 💡 **Важливо**: `b.Lines.Bytes()` вже містить таймкоди у форматі `00:01:23.456 --> 00:01:26.789`, тому не потрібно їх форматувати знову.

### `createPlaylistFile` / `addSegmentToPlaylist` / `closePlaylistFile`: генерація M3U8

```go
// 🔹 Створення заголовку плейлиста
func createPlaylistFile(filepath string, targetDuration time.Duration) (*os.File, error) {
    f, err := os.Create(filepath)
    if err != nil { return nil, err }
    
    // 🔹 Стандартний заголовок HLS для субтитрів
    _, err = f.WriteString(
        "#EXTM3U\n" + 
        "#EXT-X-VERSION:5\n" +  // 🔹 Версія 5 підтримує WebVTT субтитри
        fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", int(targetDuration.Seconds())))
    
    return f, err
}

// 🔹 Додавання сегмента у плейлист
func addSegmentToPlaylist(p *os.File, duration time.Duration, name string) {
    // 🔹 Формат: #EXTINF:тривалість,\nім'я_файлу
    p.WriteString(fmt.Sprintf("#EXTINF:%.6f,\n%s\n", duration.Seconds(), name))
}

// 🔹 Завершення плейлиста
func closePlaylistFile(f *os.File) {
    f.WriteString("#EXT-X-ENDLIST\n")  // 🔹 Ознака VOD-контенту (не live)
    f.Close()
}
```

### 🎯 Приклад вихідного M3U8 плейлиста:

```
#EXTM3U
#EXT-X-VERSION:5
#EXT-X-TARGETDURATION:6
#EXTINF:6.000000,
name-00000.vtt
#EXTINF:6.000000,
name-00001.vtt
#EXTINF:4.320000,
name-00002.vtt
#EXT-X-ENDLIST
```

> 💡 **Примітка**: Тривалість останнього сегмента може бути меншою за `TARGETDURATION` — це нормально для VOD-контенту.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Запуск сегментації у converter/subtitle.go

```go
// У callSubtitleConversions — асинхронна сегментація:
func convertSubtitle(variant suggest.SubtitleVariant, outputDir string) subtitleConversionCommand {
    // 🔹 FFmpeg конвертує субтитри у WebVTT та виводить у stdout
    args := ffmpegDefaultArguments()
    args = append(args, "-i", variant.InputURL, "-map", fmt.Sprintf("0:%d", variant.StreamIndex), 
                  "-c:s:0", "webvtt", "-f", "webvtt", "-")  // 🔹 "-" = stdout
    
    encode := exec.Command("ffmpeg", args...)
    
    // 🔹 Pipe stdout → webvtt.Segment
    webvttPipe, _ := encode.StdoutPipe()
    
    // 🔹 Запустити сегментацію у фоні
    go webvtt.Segment(webvttPipe, 6*time.Second, outputDir, variant.Name)
    
    // 🔹 Запустити FFmpeg
    encode.Start()
    
    return subtitleConversionCommand{EncoderCommand: encode, /* ... */}
}
```

### ✅ 2: Синхронізація таймінгів субтитрів з відео

```go
// У segmentAssembler — корекція таймкодів субтитрів:
type SubtitleSync struct {
    segmentStartTime time.Duration  // 🔹 Початок відео-сегмента у загальному таймлайні
    ptsOffset        int64          // 🔹 Зсув PTS для синхронізації з відео
}

func adjustSubtitleBlock(block SubtitleBlock, sync *SubtitleSync) SubtitleBlock {
    // 🔹 Скоригувати таймкоди відносно початку сегмента
    block.StartTime -= sync.segmentStartTime
    block.EndTime -= sync.segmentStartTime
    
    // 🔹 Конвертувати у PTS для порівняння з відео
    blockStartPTS := int64(block.StartTime.Seconds() * 90000) + sync.ptsOffset
    blockEndPTS := int64(block.EndTime.Seconds() * 90000) + sync.ptsOffset
    
    // 🔹 Перевірити перетин з відео-сегментом
    if blockEndPTS < sync.ptsOffset || blockStartPTS > sync.ptsOffset+segmentDurationPTS {
        return SubtitleBlock{}  // 🔹 Пропустити блок, що не належить сегменту
    }
    
    return block
}
```

### ✅ 3: Моніторинг якості сегментації

```go
// monitoring.Monitor — метрики для сегментації субтитрів:
type SubtitleSegmentMetrics struct {
    SegmentsCreated  *prometheus.CounterVec  // кількість створених сегментів
    BlocksPerSegment *prometheus.HistogramVec  // розподіл блоків на сегмент
    SegmentDuration  *prometheus.HistogramVec  // фактична тривалість сегментів
    BoundaryBlocks   *prometheus.CounterVec  // кількість блоків на межах (дублікати)
}

// У процесі сегментації:
func monitorSegmentCreation(channelID string, segmentCount uint, blocks []SubtitleBlock, 
                           duration time.Duration, metrics *SubtitleSegmentMetrics) {
    
    metrics.SegmentsCreated.WithLabelValues(channelID).Inc()
    metrics.BlocksPerSegment.WithLabelValues(channelID).Observe(float64(len(blocks)))
    metrics.SegmentDuration.WithLabelValues(channelID).Observe(duration.Seconds())
    
    // 🔹 Підрахувати блоки на межах (дублікати)
    boundaryCount := 0
    for _, b := range blocks {
        if b.StartTime < 0.1*time.Second || duration-b.EndTime < 0.1*time.Second {
            boundaryCount++
        }
    }
    metrics.BoundaryBlocks.WithLabelValues(channelID).Add(float64(boundaryCount))
}
```

### ✅ 4: Обробка помилок запису

```go
// Стратегія: продовжити сегментацію навіть при помилці запису одного файлу
func writeBlocksToVTTWithRetry(blocks []SubtitleBlock, filepath string, maxRetries int) error {
    var lastErr error
    for attempt := 0; attempt < maxRetries; attempt++ {
        err := writeBlocksToVTT(blocks, filepath)
        if err == nil {
            return nil
        }
        lastErr = err
        log.Warnf("Attempt %d failed to write %s: %v", attempt+1, filepath, err)
        time.Sleep(100 * time.Millisecond)
    }
    return fmt.Errorf("failed to write %s after %d attempts: %w", filepath, maxRetries, lastErr)
}

// Використання у createSegment:
err := writeBlocksToVTTWithRetry(blocks, segmentFilepath, 3)
if err != nil {
    log.Errorf("Skipping segment %d due to write error: %v", segmentCount, err)
    // 🔹 Продовжити з наступним сегментом, не зупиняти весь процес
    return
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на базову сегментацію

```go
func TestSegment_Basic(t *testing.T) {
    // 🔹 Вхід: 3 блоки, targetDuration=6с
    input := `WEBVTT

00:00:01.000 --> 00:00:03.000
Блок 1

00:00:05.000 --> 00:00:07.000
Блок 2 (на межі)

00:00:10.000 --> 00:00:12.000
Блок 3
`
    outputDir := t.TempDir()
    
    // 🔹 Запустити сегментацію
    err := Segment(strings.NewReader(input), 6*time.Second, outputDir, "test")
    assert.NoError(t, err)
    
    // 🔹 Перевірити створені файли
    assert.FileExists(t, filepath.Join(outputDir, "test.m3u8"))
    assert.FileExists(t, filepath.Join(outputDir, "test-00000.vtt"))  // 0-6с
    assert.FileExists(t, filepath.Join(outputDir, "test-00001.vtt"))  // 6-12с
    
    // 🔹 Перевірити вміст першого сегмента
    content0, _ := os.ReadFile(filepath.Join(outputDir, "test-00000.vtt"))
    assert.Contains(t, string(content0), "Блок 1")
    assert.Contains(t, string(content0), "Блок 2")  // 🔹 Блок на межі дублюється
    
    // 🔹 Перевірити вміст другого сегмента
    content1, _ := os.ReadFile(filepath.Join(outputDir, "test-00001.vtt"))
    assert.Contains(t, string(content1), "Блок 2")  // 🔹 Дублікат
    assert.Contains(t, string(content1), "Блок 3")
    
    // 🔹 Перевірити плейлист
    playlist, _ := os.ReadFile(filepath.Join(outputDir, "test.m3u8"))
    assert.Contains(t, string(playlist), "#EXT-X-TARGETDURATION:6")
    assert.Contains(t, string(playlist), "test-00000.vtt")
    assert.Contains(t, string(playlist), "test-00001.vtt")
    assert.Contains(t, string(playlist), "#EXT-X-ENDLIST")
}
```

### 🔹 Тест на обробку блоків на межі

```go
func TestSegment_BoundaryBlocks(t *testing.T) {
    // 🔹 Блок, що точно перетинає межу: [5.5с --> 6.5с]
    input := `WEBVTT

00:00:05.500 --> 00:00:06.500
Boundary block
`
    outputDir := t.TempDir()
    
    err := Segment(strings.NewReader(input), 6*time.Second, outputDir, "test")
    assert.NoError(t, err)
    
    // 🔹 Блок має з'явитися в ОБИДВОХ сегментах
    content0, _ := os.ReadFile(filepath.Join(outputDir, "test-00000.vtt"))
    content1, _ := os.ReadFile(filepath.Join(outputDir, "test-00001.vtt"))
    
    assert.Contains(t, content0, "Boundary block")
    assert.Contains(t, content1, "Boundary block")  // ✅ Дублікат на межі
}
```

### 🔹 Тест на толерантність +500мс

```go
func TestSegment_Tolerance(t *testing.T) {
    // 🔹 Блок, що трохи перевищує targetDuration: [0с --> 6.3с]
    input := `WEBVTT

00:00:00.000 --> 00:00:06.300
Long block
`
    outputDir := t.TempDir()
    
    // 🔹 targetDuration=6с, але блок 6.3с → має вміститися завдяки +500мс
    err := Segment(strings.NewReader(input), 6*time.Second, outputDir, "test")
    assert.NoError(t, err)
    
    // 🔹 Блок має бути у першому сегменті (не обрізаний)
    content0, _ := os.ReadFile(filepath.Join(outputDir, "test-00000.vtt"))
    assert.Contains(t, content0, "Long block")
    
    // 🔹 Перевірити, що тривалість сегмента враховує блок
    playlist, _ := os.ReadFile(filepath.Join(outputDir, "test.m3u8"))
    assert.Contains(t, playlist, "#EXTINF:6.300000,")  // ✅ Фактична тривалість, не 6.0
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Дублювання блоків на межах | Одна репліка з'являється двічі у плеєрі | 🔹 Це очікувана поведінка для запобігання втраті; якщо небажано → модифікувати логіку "astride" |
| Неправильна тривалість у плейлисті | Плеєр показує помилки синхронізації | 🔹 Перевірити, що `addSegmentToPlaylist` використовує `endTime-startTime`, не `targetDuration` |
| Файли не створюються | Помилка запису, порожній outputDir | 🔹 Перевірити права доступу; додати `os.MkdirAll(outputDir, 0755)` перед записом |
| Канал не закривається | Горутина зависає, пам'ять не звільняється | 🔹 `ReadFromWebVTT` має `defer close(c)` — перевірити, що парсер завершується коректно |
| Таймкоди не синхронізовані з відео | Субтитри випереджають/відстають | 🔹 Додати корекцію таймкодів у `adjustSubtitleBlock` на основі PTS відео |

### Приклад додавання `MkdirAll` для надійності:

```go
func createSegment(basename string, segmentCount uint, outputDir string, 
                  blocks []SubtitleBlock, playlist *os.File, 
                  startTime, endTime time.Duration) {
    
    // 🔹 Гарантувати існування каталогу
    if err := os.MkdirAll(outputDir, 0755); err != nil {
        log.Printf("Failed to create output dir %s: %v", outputDir, err)
        return
    }
    
    // ... решта логіки ...
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базовий виклик сегментації:
func segmentSubtitles(inputPath, outputDir, name string, duration time.Duration) error {
    f, err := os.Open(inputPath)
    if err != nil { return err }
    defer f.Close()
    
    return webvtt.Segment(f, duration, outputDir, name)
}

// 2: Асинхронна сегментація з обробкою помилок:
func segmentSubtitlesAsync(input io.Reader, outputDir, name string, 
                          duration time.Duration, resultCh chan<- error) {
    go func() {
        err := webvtt.Segment(input, duration, outputDir, name)
        resultCh <- err
    }()
}

// 3: Перевірка валідності згенерованих файлів:
func validateSubtitleSegments(outputDir, name string, expectedCount int) error {
    // 🔹 Перевірити наявність плейлиста
    playlistPath := filepath.Join(outputDir, name+".m3u8")
    if _, err := os.Stat(playlistPath); os.IsNotExist(err) {
        return fmt.Errorf("playlist not found: %s", playlistPath)
    }
    
    // 🔹 Перевірити кількість сегментів
    for i := 0; i < expectedCount; i++ {
        segmentPath := filepath.Join(outputDir, fmt.Sprintf("%s-%05d.vtt", name, i))
        if _, err := os.Stat(segmentPath); os.IsNotExist(err) {
            return fmt.Errorf("segment %d not found: %s", i, segmentPath)
        }
    }
    return nil
}

// 4: Логування прогресу сегментації:
func logSegmentProgress(channelID string, count uint, blocks []SubtitleBlock, duration time.Duration) {
    log.Debugf("Channel %s: segment %d created with %d blocks, duration=%.3fs", 
        channelID, count, len(blocks), duration.Seconds())
}

// 5: Об'єднання сегментів назад (для відладки):
func mergeSubtitleSegments(outputDir, name string, outputPath string) error {
    f, err := os.Create(outputPath)
    if err != nil { return err }
    defer f.Close()
    
    f.WriteString("WEBVTT\n\n")
    
    for i := 0; ; i++ {
        segmentPath := filepath.Join(outputDir, fmt.Sprintf("%s-%05d.vtt", name, i))
        if _, err := os.Stat(segmentPath); os.IsNotExist(err) {
            break  // 🔹 Більше немає сегментів
        }
        
        content, err := os.ReadFile(segmentPath)
        if err != nil { return err }
        
        // 🔹 Пропустити заголовок "WEBVTT" у сегментах
        lines := strings.SplitN(string(content), "\n", 3)
        if len(lines) >= 3 {
            f.WriteString(lines[2])  // 🔹 Тільки контент, без заголовка
        }
    }
    return nil
}
```

---

## 📊 Матриця параметрів сегментації

```
Параметр                | Тип       | Рекомендоване значення      | Призначення
────────────────────────┼───────────┼─────────────────────────────┼─────────────────────────
targetDuration          | time.Duration | 4-6 секунд              | 🔹 Синхронізація з відео-сегментами
Tolerance window        | const     | +500мс                    | 🔹 Гнучкість для блоків на межах
Buffer size (blocks)    | int       | 5                         | 🔹 Передбачення кількості блоків у сегменті
Playlist version        | int       | 5                         | 🔹 Підтримка WebVTT у HLS
File naming pattern     | string    | name-00000.vtt            | 🔹 Сортування та ідентифікація сегментів
```

---

## 📚 Корисні посилання

- [WebVTT specification (W3C)](https://www.w3.org/TR/webvtt1/)
- [HLS subtitle support](https://developer.apple.com/documentation/http_live_streaming/about_the_radio_stream_format)
- [go-astisub source code](https://github.com/asticode/go-astisub)
- [Go io.Reader best practices](https://pkg.go.dev/io#Reader)

> 💡 **Ключова ідея**: Цей `Segment` модуль — це "розрізач" для субтитрів у вашому пайплайні. Він:
> - 🎯 Розбиває довгі WebVTT файли на сегменти, синхронізовані з відео
> - 🔧 Зберігає цілісність реплік на межах через дублювання ("astride" логіка)
> - ⚡ Працює потоково через канали, не завантажуючи весь файл у пам'ять
> - 📋 Генерує валідні M3U8 плейлисти для безпосереднього використання у HLS

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку динамічної довжини сегментів на основі ключових кадрів відео
- 🧪 Написати integration-тест для перевірки синхронізації субтитрів з відео-сегментами у реальному плеєрі
- 📈 Додати Prometheus-метрики для моніторингу продуктивності сегментації та якості таймінгів по каналах

🛠️