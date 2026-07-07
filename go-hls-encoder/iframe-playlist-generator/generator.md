# Глибоке роз'яснення: `iframe_playlist_generator` — генерація I-frame-only плейлистів для HLS

Цей файл містить **повну логіку генерації I-frame-only HLS плейлистів** — від прозонування відеофайлів через ffprobe до побудови спеціальних плейлистів для швидкого перемотування. Це критичний компонент для покращення користувацького досвіду при навігації у записі.

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

## 🔧 Основні функції: архітектура генерації

### `EnrichPlaylist`: збагачення master playlist метаданими

```go
func EnrichPlaylist(dirMaster, masterFilename, dirInfo, infoFilename, newName string) (string, error) {
    // 🔹 1. Відкрити master playlist для збагачення
    inFileFullPath := filepath.Join(dirMaster, masterFilename)
    p, _, t, err := variantsFromMaster(inFileFullPath)
    if err != nil { return "", err }
    if t != m3u8.MASTER {
        log.Println("Cannot Enrich playlist", masterFilename, "as it is not a Master Playlist. Type:", t)
        return "", nil
    }
    pMaster := p.(*m3u8.MasterPlaylist)
    
    // 🔹 2. Відкрити playlist з метаданими (напр., з ffprobe)
    inFileFullPath = filepath.Join(dirInfo, infoFilename)
    p_, _, t, err = variantsFromMaster(inFileFullPath)
    if err != nil { return "", err }
    if t != m3u8.MASTER {
        log.Println("Cannot Enrich playlist", masterFilename, "as it is not a Master Playlist. Type:", t)
        return "", nil
    }
    pInfo := p_.(*m3u8.MasterPlaylist)
    
    // 🔹 3. Оновити варіанти master playlist метаданими з pInfo
    for _, v := range pMaster.Variants {
        updateVariant(pInfo, v)
    }
    
    // 🔹 4. Записати збагачений playlist
    return writePlaylistToFile(pMaster, dirMaster, newName)
}
```

### `updateVariant`: копіювання метаданих між варіантами

```go
func updateVariant(playlistWithInfo *m3u8.MasterPlaylist, v *m3u8.Variant) {
    for _, v_ := range playlistWithInfo.Variants {
        if v_.URI == v.URI {
            // 🔹 Оновити bandwidth та codecs
            v.VariantParams.Bandwidth = v_.VariantParams.Bandwidth
            if len(v_.VariantParams.Codecs) > 0 {
                v.VariantParams.Codecs = v_.VariantParams.Codecs
            }
            break
        }
    }
}
```

> 💡 **Призначення**: Якщо master playlist не містить точних метаданих (bandwidth, codecs), ця функція дозволяє "підтягнути" їх з іншого джерела (напр., з ffprobe або попередньо згенерованого playlist).

---

### `GeneratePlaylist`: основна логіка генерації I-frame-only плейлистів

```go
func GeneratePlaylist(dir, inFile string) error {
    // 🔹 1. Отримати варіанти з master/media playlist
    inFileFullPath := filepath.Join(dir, inFile)
    _, variants, t, err := variantsFromMaster(inFileFullPath)
    if err != nil { return err }
    
    // 🔹 2. Підготувати файл для запису (якщо це master playlist)
    var f *os.File = nil
    if t == m3u8.MASTER {
        f, err = os.OpenFile(inFileFullPath, os.O_APPEND|os.O_WRONLY, 0600)
        if err != nil { return err }
        defer f.Close()
    }
    
    // 🔹 3. Заповнити chunklists варіантів (прочитати медіа-плейлисти)
    fillVariants(dir, variants...)
    
    // 🔹 4. Для кожного варіанту згенерувати I-frame-only playlist
    for _, variant := range variants {
        // 🔹 4.1. Згенерувати playlist
        iframePlaylist, err := iframePlaylistForVariant(dir, variant)
        if err != nil {
            log.Println("Cannot generate I-FRAMES-ONLY playlist for variant \""+variant.URI+
                "\"... Carrying on with the others anyway. \n\tError:", err)
            continue  // 🔹 Graceful degradation: продовжити з іншими
        }
        
        // 🔹 4.2. Записати у новий файл
        iframePlaylist.TargetDuration -= 1  // 🔹 Корекція (див. нижче)
        iframeFilename, err := writePlaylistToFile(iframePlaylist, dir, iframeOnlyFilename(variant.URI))
        if err != nil {
            log.Println("Cannot write I-FRAMES-ONLY playlist to file \""+variant.URI+
                "\"... Carrying on with the others anyway. \n\tError:", err)
            continue
        }
        
        // 🔹 4.3. Додати посилання у master playlist (якщо є)
        if t == m3u8.MASTER {
            log.Println("DEBUG:        -> yes")
            // 🔹 BUG: bandwidth розраховується як /10, це неточно!
            _, err := f.WriteString(fmt.Sprintf(
                "#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=%d,URI=\"%v\"\n",
                int(variant.Bandwidth)/10, iframeFilename))
            if err != nil {
                log.Println("Error writing to master:", err)
            }
        }
    }
    
    log.Println("DEBUG: I-FRAME-GENERATION Done")
    return nil
}
```

### 🎯 Ключові моменти реалізації

#### 🔹 `fillVariants`: завантаження медіа-плейлистів

```go
func fillVariants(dir string, variants ...*m3u8.Variant) {
    for _, v := range variants {
        uri := filepath.Join(dir, v.URI)
        p, _ := m3u8.NewMediaPlaylist(0, 1)
        v.Chunklist = p
        
        f, err := os.Open(uri)
        if err != nil {
            log.Println("Cannot read variant at URI \"" + v.URI + "\". Skipping variant...")
            continue
        }
        err = v.Chunklist.DecodeFrom(f, true)
        if err != nil {
            log.Println("Cannot decode variant at URI \"" + v.URI + "\". Skipping variant...")
            continue
        }
        f.Close()
    }
}
```

> 💡 **Призначення**: Завантажити контент медіа-плейлистів (список сегментів) у пам'ять для подальшої обробки. Без цього `iframePlaylistForVariant` не матиме доступу до сегментів.

#### 🔹 `iframePlaylistForVariant`: генерація I-frame-only медіа-плейлиста

```go
func iframePlaylistForVariant(dir string, variant *m3u8.Variant) (*m3u8.MediaPlaylist, error) {
    if variant.Chunklist == nil {
        return nil, errors.New("`nil` chunklist for variant \"" + variant.URI + "\"")
    }
    
    // 🔹 1. Зібрати I-frame entries з усіх сегментів
    var entries []*IFrameEntry
    nbSegmts := len(variant.Chunklist.Segments)
    initFilename := ""
    var initSize uint = 0
    
    for i, segment := range variant.Chunklist.Segments {
        if segment == nil { break }
        
        // 🔹 Обробка init сегмента для fMP4
        if segment.Map != nil {
            initFilename = filepath.Join(dir, segment.Map.URI)
            fi, _ := os.Stat(initFilename)
            initSize = uint(fi.Size())
        }
        
        // 🔹 Прозвонити сегмент на I-frames
        entriesPartial, err := iframeEntryForSegment(initFilename, initSize, filepath.Join(dir, segment.URI))
        if err != nil {
            log.Println("DEBUG: Error running iframeEntryForSegment on", filepath.Join(dir, segment.URI))
            return nil, err
        }
        entries = append(entries, entriesPartial...)
        fmt.Printf("DEBUG: EXT-I-Frame Progress: %d/%d\n", i, nbSegmts)
    }
    
    // 🔹 2. Створити новий медіа-плейлист
    log.Println("DEBUG: Generating playlist")
    p, _ := m3u8.NewMediaPlaylist(0, uint(len(entries)))
    p.SetIframeOnly()  // 🔹 Ключовий тег: #EXT-X-I-FRAMES-ONLY
    p.TargetDuration = variant.Chunklist.TargetDuration
    
    // 🔹 3. Додати entries у плейлист
    for _, entry := range entries {
        p.Append(entry.SegmentURI, entry.Duration, "")
        p.SetRange(int64(entry.PacketSize), int64(entry.PacketPosition))
    }
    
    return p, nil
}
```

#### 🔹 `iframeEntryForSegment`: витягування I-frame метаданих з сегмента

```go
func iframeEntryForSegment(initURI string, initSize uint, segmentURI string) ([]*IFrameEntry, error) {
    // 🔹 1. Прозвонити файл на пакети
    packets, err := probePackets(initURI, segmentURI)
    if err != nil { return nil, err }
    
    var entries []*IFrameEntry
    var lastEntry *IFrameEntry = nil
    nbPkts := len(packets)
    
    for i, p := range packets {
        if p.isFromKeyFrame() {
            // 🔹 Зберегти попередній entry (якщо є)
            if lastEntry != nil {
                entries = append(entries, lastEntry)
            }
            
            // 🔹 Розрахувати розмір пакету
            size := p.Size
            if i < nbPkts-1 {
                pNext := packets[i+1]
                size = pNext.Pos - p.Pos  // розмір = позиція наступного - поточна позиція
            }
            size += 188  // 🔹 MAGIC NUMBER: TS packet size? (див. нижче)
            
            // 🔹 Створити новий entry
            lastEntry = &IFrameEntry{
                SegmentURI:     filepath.Base(segmentURI),
                PacketPosition: p.Pos - initSize,  // корекція на розмір init сегмента
                PacketSize:     size,
                Duration:       p.DurationTime,    // початкова тривалість
            }
        } else {
            // 🔹 Додати тривалість не-ключового пакету до поточного I-frame entry
            if lastEntry != nil {
                lastEntry.Duration += p.DurationTime
            }
        }
    }
    
    // 🔹 Зберегти останній entry
    if lastEntry != nil {
        entries = append(entries, lastEntry)
    } else {
        log.Println("WARNING: Segment", segmentURI, "has no key frame.")
    }
    
    return entries, nil
}
```

### 🎯 Ключовий момент: розрахунок розміру I-frame пакету

```go
size := p.Size
if i < nbPkts-1 {
    pNext := packets[i+1]
    size = pNext.Pos - p.Pos  // різниця позицій = розмір
}
size += 188  // 🔹 MAGIC NUMBER
```

**Пояснення `+188`:**
```
• 188 байт = стандартний розмір TS-пакету (MPEG-TS)
• ffprobe повертає позицію/розмір для "корисного навантаження"
• Для коректного byte-range у HLS потрібно додати заголовок пакету

Проблема:
❌ Це працює тільки для MPEG-TS сегментів
❌ Для fMP4 (фрагментований MP4) це призведе до неправильних byte-range

Рішення:
• Визначати формат сегмента (TS vs fMP4)
• Для fMP4 не додавати 188, або використовувати іншу логіку
```

### 🎯 Ключовий момент: розрахунок bandwidth для I-frame playlist

```go
// 🔹 BUG: bandwidth розраховується як /10, це неточно!
int(variant.Bandwidth)/10
```

**Чому це проблема:**
```
• Bandwidth I-frame playlist має відображати середній бітрейт I-frames
• Ділення на 10 — емпіричне наближення, яке може бути дуже неточним
• Неправильний bandwidth → плеєр може вибрати неоптимальний варіант

Правильний підхід (з TODO у коді):
• Сумувати розміри всіх I-frame пакетів
• Ділити на загальну тривалість відео
• Результат = середній бітрейт I-frames (біт/сек)

Приклад:
  • Загальний розмір I-frames: 50 MB = 400 Mbit
  • Тривалість відео: 100 секунд
  • Правильний bandwidth: 400 Mbit / 100 s = 4 Mbps = 4,000,000 bps
```

---

## 🔧 Допоміжні функції

### `variantsFromMaster`: універсальний парсер playlist

```go
func variantsFromMaster(playlistPath string) (m3u8.Playlist, []*m3u8.Variant, m3u8.ListType, error) {
    f, err := os.Open(playlistPath)
    if err != nil { return nil, []*m3u8.Variant{}, 0, err }
    defer f.Close()
    
    p, t, err := m3u8.DecodeFrom(f, false)
    if err != nil { return nil, []*m3u8.Variant{}, 0, err }
    
    switch t {
    case m3u8.MASTER:
        variants := p.(*m3u8.MasterPlaylist).Variants
        return p, variants, t, nil
    case m3u8.MEDIA:
        p := p.(*m3u8.MediaPlaylist)
        variant := m3u8.Variant{
            URI:       playlistPath,
            Chunklist: p,
        }
        return nil, []*m3u8.Variant{&variant}, t, nil
    default:
        err := errors.New("assertion error: unknown mediaplaylist type")
        return nil, []*m3u8.Variant{}, t, err
    }
}
```

> 💡 **Призначення**: Універсальна функція для роботи з обома типами HLS плейлистів — master та media. Повертає список варіантів для подальшої обробки.

### `iframeOnlyFilename`: генерація імені I-frame-only файлу

```go
func iframeOnlyFilename(originalName string) (newName string) {
    extName := filepath.Ext(originalName)
    bName := originalName[:len(originalName)-len(extName)]
    newName = bName + "_I-FRAME-ONLY" + extName
    return
}
```

**Приклад:**
```
original: "channel1_0.m3u8"
result:   "channel1_0_I-FRAME-ONLY.m3u8"
```

### `writePlaylistToFile`: запис playlist у файл

```go
func writePlaylistToFile(p m3u8.Playlist, dir string, playlistFilename string) (string, error) {
    playlistFullPath := filepath.Join(dir, playlistFilename)
    f, err := os.OpenFile(playlistFullPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
    if err != nil { return "", err }
    
    _, err = f.Write(p.Encode().Bytes())
    if err != nil { return "", err }
    
    return playlistFilename, nil
}
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Запуск генерації I-frame playlist після енкодингу

```go
// У callFFmpeg (після cmd.Wait()):
func postProcessHLS(dir, masterFilename string) error {
    // 🔹 Опціонально: збагатити master playlist метаданими
    if GENERATE_IPLAYLIST {
        // 🔹 Згенерувати I-frame-only плейлисти
        err := GeneratePlaylist(dir, masterFilename)
        if err != nil {
            log.Printf("Warning: I-frame playlist generation failed: %v", err)
            // ❌ Не зупиняти весь процес, продовжити
        }
    }
    return nil
}
```

### ✅ 2: Оптимізація для великих CCTV записів

```go
// Для довгих записів — генерувати I-frame playlist тільки для останніх N годин:
func generateIFramePlaylistRecent(dir, masterFilename string, recentHours int) error {
    // 🔹 Отримати список сегментів
    segments, err := parseHLSMasterPlaylist(filepath.Join(dir, masterFilename))
    if err != nil { return err }
    
    // 🔹 Фільтрувати тільки недавні сегменти
    cutoffTime := time.Now().Add(-time.Duration(recentHours) * time.Hour)
    recentSegments := filterSegmentsByTime(segments, cutoffTime)
    
    // 🔹 Згенерувати I-frame playlist тільки для них
    return generateIFramePlaylistForSegments(dir, recentSegments)
}

func filterSegmentsByTime(segments []HLSSegment, cutoff time.Time) []HLSSegment {
    var recent []HLSSegment
    for _, seg := range segments {
        if seg.StartTime.After(cutoff) {
            recent = append(recent, seg)
        }
    }
    return recent
}
```

### ✅ 3: Кешування результатів прозонування

```go
// Щоб не прозвонювати той самий сегмент багато разів:
type IFrameCache struct {
    mu    sync.RWMutex
    cache map[string][]*IFrameEntry  // key = filepath, value = entries
    ttl   time.Duration
}

func (c *IFrameCache) GetOrProbe(initURI, segmentURI string) ([]*IFrameEntry, error) {
    key := initURI + ":" + segmentURI
    
    // 🔹 Спробувати отримати з кешу
    c.mu.RLock()
    if entries, ok := c.cache[key]; ok {
        c.mu.RUnlock()
        return entries, nil
    }
    c.mu.RUnlock()
    
    // 🔹 Прозвонити файл
    entries, err := iframeEntryForSegment(initURI, 0, segmentURI)
    if err != nil { return nil, err }
    
    // 🔹 Зберегти у кеш
    c.mu.Lock()
    c.cache[key] = entries
    c.mu.Unlock()
    
    return entries, nil
}

// Використання у iframePlaylistForVariant:
iframeCache := &IFrameCache{
    cache: make(map[string][]*IFrameEntry),
    ttl:   1 * time.Hour,
}

// Замість прямого виклику:
entries, err := iframeCache.GetOrProbe(initFilename, segmentURI)
```

### ✅ 4: Моніторинг продуктивності генерації

```go
// monitoring.Monitor — метрики для I-frame генерації:
type IFrameMetrics struct {
    VariantsProcessed *prometheus.CounterVec  // кількість оброблених варіантів
    SegmentsProbed    *prometheus.CounterVec  // кількість прозвонених сегментів
    IFramesFound      *prometheus.CounterVec  // кількість знайдених I-frames
    GenerationLatency *prometheus.HistogramVec  // час генерації на варіант
    Errors            *prometheus.CounterVec  // помилки генерації
}

// У процесі генерації:
func monitorIFrameGeneration(variantURI string, metrics *IFrameMetrics, 
                            startTime time.Time, segmentsProbed, iframesFound int, err error) {
    
    latency := time.Since(startTime)
    
    if err != nil {
        metrics.Errors.WithLabelValues(variantURI).Inc()
        return
    }
    
    metrics.VariantsProcessed.WithLabelValues(variantURI).Inc()
    metrics.SegmentsProbed.WithLabelValues(variantURI).Add(float64(segmentsProbed))
    metrics.IFramesFound.WithLabelValues(variantURI).Add(float64(iframesFound))
    metrics.GenerationLatency.WithLabelValues(variantURI).Observe(latency.Seconds())
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Інтеграційний тест на генерацію I-frame playlist

```go
func TestGeneratePlaylist_Integration(t *testing.T) {
    // 🔹 Підготувати тестові дані
    tempDir := t.TempDir()
    createTestHLSStructure(t, tempDir)  // ваша helper-функція
    
    // 🔹 Запустити генерацію
    err := GeneratePlaylist(tempDir, "master.m3u8")
    assert.NoError(t, err)
    
    // 🔹 Перевірити вихідні файли
    iframeFile := filepath.Join(tempDir, "channel1_0_I-FRAME-ONLY.m3u8")
    assert.FileExists(t, iframeFile)
    
    // 🔹 Перевірити вміст
    content, _ := os.ReadFile(iframeFile)
    assert.Contains(t, string(content), "#EXT-X-I-FRAMES-ONLY")
    assert.Contains(t, string(content), "#EXT-X-BYTERANGE:")
    assert.Contains(t, string(content), "#EXTINF:")
    
    // 🔹 Перевірити, що byte-range валідні
    lines := strings.Split(string(content), "\n")
    for i, line := range lines {
        if strings.HasPrefix(line, "#EXT-X-BYTERANGE:") {
            // 🔹 Формат: "size@position"
            parts := strings.Split(strings.TrimPrefix(line, "#EXT-X-BYTERANGE:"), "@")
            assert.Len(t, parts, 2)
            size, err1 := strconv.ParseUint(parts[0], 10, 64)
            pos, err2 := strconv.ParseUint(parts[1], 10, 64)
            assert.NoError(t, err1)
            assert.NoError(t, err2)
            assert.Greater(t, size, uint64(0))
            assert.Greater(t, pos, uint64(0))
            
            // 🔹 Перевірити, що наступний рядок — URI сегмента
            if i+1 < len(lines) {
                nextLine := strings.TrimSpace(lines[i+1])
                assert.NotEmpty(t, nextLine)
                assert.NotContains(t, nextLine, "#")  // не коментар
            }
        }
    }
}
```

### 🔹 Тест на обробку помилок прозонування

```go
func TestIFrameEntryForSegment_InvalidFile(t *testing.T) {
    // 🔹 Невірний файл
    _, err := iframeEntryForSegment("", 0, "/nonexistent/file.ts")
    assert.Error(t, err)
    
    // 🔹 Пошкоджений файл
    corruptFile := createCorruptVideoFile(t)
    _, err = iframeEntryForSegment("", 0, corruptFile)
    // 🔹 Може повернути помилку або порожній список (залежить від ffprobe)
    // Головне — не панікувати
}
```

### 🔹 Тест на розрахунок byte-range

```go
func TestIFrameEntryForSegment_ByteRangeCalculation(t *testing.T) {
    // 🔹 Підготувати файл з відомими I-frames
    testFile := createTestVideoWithKnownKeyframes(t)
    
    entries, err := iframeEntryForSegment("", 0, testFile)
    assert.NoError(t, err)
    assert.NotEmpty(t, entries)
    
    // 🔹 Перевірити, що розміри розраховані коректно
    for i, entry := range entries {
        assert.Greater(t, entry.PacketSize, uint(0))
        assert.Greater(t, entry.PacketPosition, uint(0))
        
        // 🔹 Перевірити монотонність позицій
        if i > 0 {
            prev := entries[i-1]
            assert.Greater(t, entry.PacketPosition, prev.PacketPosition+prev.PacketSize)
        }
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `+188` для fMP4 | Неправильні byte-range, плеєр не може завантажити I-frame | 🔹 Визначати формат сегмента (TS vs fMP4); для fMP4 не додавати 188 |
| Неправильний bandwidth (`/10`) | Плеєр вибирає неоптимальний варіант для trick play | 🔹 Реалізувати правильний розрахунок: сумарний розмір I-frames / тривалість |
| `TargetDuration -= 1` | Невідомо, навіщо ця корекція; може зламати сумісність | 🔹 Дослідити причину; якщо не потрібно — видалити |
| Конкатенація init+media зависає | `probePackets` не повертає результат | 🔹 Додати таймаути; перевірити, що pipe коректно закривається |
| Помилка "no key frame" | Сегмент без I-frames (неможливо для валідного відео) | 🔹 Перевірити, що GOP налаштовано коректно; додати валідацію вхідних даних |

### Приклад визначення формату сегмента:

```go
func isFragmentedMP4(filePath string) bool {
    // 🔹 Перевірити розширення
    if strings.HasSuffix(filePath, ".m4s") || strings.HasSuffix(filePath, ".mp4") {
        return true
    }
    
    // 🔹 Перевірити magic bytes (box structure)
    f, err := os.Open(filePath)
    if err != nil { return false }
    defer f.Close()
    
    buf := make([]byte, 12)
    n, _ := f.Read(buf)
    if n < 12 { return false }
    
    // 🔹 fMP4 починається з "ftyp" box
    return bytes.Contains(buf, []byte("ftyp"))
}

// Використання у iframeEntryForSegment:
func iframeEntryForSegment(initURI string, initSize uint, segmentURI string) ([]*IFrameEntry, error) {
    // 🔹 Визначити формат
    isFMP4 := isFragmentedMP4(segmentURI)
    
    // 🔹 Прозвонити
    packets, err := probePackets(initURI, segmentURI)
    if err != nil { return nil, err }
    
    // ... обробка пакетів ...
    
    // 🔹 Розрахунок розміру
    size := p.Size
    if i < nbPkts-1 {
        pNext := packets[i+1]
        size = pNext.Pos - p.Pos
    }
    
    // 🔹 Додати заголовок тільки для TS
    if !isFMP4 {
        size += 188  // TS packet header
    }
    
    // ... створення entry ...
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Запуск генерації I-frame playlist:
func generateIFramePlaylistForChannel(channelID, outputDir, masterPlaylist string) error {
    log.Infof("Channel %s: generating I-frame playlist", channelID)
    
    start := time.Now()
    err := GeneratePlaylist(outputDir, masterPlaylist)
    latency := time.Since(start)
    
    if err != nil {
        log.Errorf("Channel %s: I-frame generation failed: %v", channelID, err)
        return err
    }
    
    log.Infof("Channel %s: I-frame playlist generated in %v", channelID, latency)
    return nil
}

// 2: Валідація byte-range перед записом:
func validateByteRange(size, pos uint64, fileSize uint64) error {
    if pos >= fileSize {
        return fmt.Errorf("position %d exceeds file size %d", pos, fileSize)
    }
    if pos+size > fileSize {
        return fmt.Errorf("range [%d, %d) exceeds file size %d", pos, pos+size, fileSize)
    }
    return nil
}

// 3: Розрахунок bandwidth для I-frame playlist:
func calculateIFrameBandwidth(entries []*IFrameEntry, totalDuration float64) uint {
    if totalDuration <= 0 { return 0 }
    
    var totalBits uint64
    for _, e := range entries {
        totalBits += uint64(e.PacketSize) * 8  // байти → біти
    }
    
    // 🔹 Середній бітрейт у бітах на секунду
    return uint(float64(totalBits) / totalDuration)
}

// 4: Логування прогресу:
func logIFrameProgress(variantURI string, current, total int) {
    if total > 0 && current%10 == 0 {  // логувати кожні 10 сегментів
        log.Debugf("Variant %s: I-frame progress %d/%d (%.1f%%)", 
            variantURI, current, total, float64(current)/float64(total)*100)
    }
}

// 5: Обробка помилок з retry:
func probeWithRetry(initURI, segmentURI string, maxRetries int) ([]*IFrameEntry, error) {
    var lastErr error
    for attempt := 0; attempt < maxRetries; attempt++ {
        entries, err := iframeEntryForSegment(initURI, 0, segmentURI)
        if err == nil {
            return entries, nil
        }
        lastErr = err
        log.Warnf("Probe attempt %d failed for %s: %v", attempt+1, segmentURI, err)
        time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
    }
    return nil, fmt.Errorf("all %d probe attempts failed: %w", maxRetries, lastErr)
}
```

---

## 📊 Матриця параметрів I-frame playlist для CCTV HLS

```
Параметр                | Тип       | Рекомендоване значення      | Призначення
────────────────────────┼───────────┼─────────────────────────────┼─────────────────────────
#EXT-X-I-FRAMES-ONLY    | тег       | обов'язково                 | 🔹 Позначає playlist як I-frame-only
#EXT-X-BYTERANGE        | атрибут   | size@position               | 🔹 Вказує байтовий діапазон I-frame у файлі
#EXT-X-I-FRAME-STREAM-INF| тег      | BANDWIDTH, URI              | 🔹 Посилання у master playlist
TargetDuration          | int       | як у медіа-плейлиста        | 🔹 Максимальна тривалість сегмента
Bandwidth (розрахунок)  | uint      | sum(I-frame sizes)/duration | 🔹 Середній бітрейт I-frames для плеєра
```

---

## 📚 Корисні посилання

- [HLS I-frame playlist specification](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices#3249247)
- [FFmpeg ffprobe for key frame detection](https://ffmpeg.org/ffprobe.html)
- [Understanding MPEG-TS byte ranges](https://video.stackexchange.com/questions/14176/what-is-a-key-frame)
- [m3u8 library for Go](https://github.com/grafov/m3u8)

> 💡 **Ключова ідея**: Цей модуль — це "навігатор" у вашому відеоархіві. Він:
> - 🎯 Швидко знаходить ключові точки (I-frames) для миттєвого seek
> - 🔧 Генерує валідні HLS-плейлисти з byte-range для сумісності з плеєрами
> - ⚡ Масштабується через асинхронну обробку та кешування для довгих записів
> - 🛡️ Граційно обробляє помилки без зупинки всього пайплайну

Якщо потрібно — можу допомогти:
- 🔄 Виправити розрахунок bandwidth та `+188` для підтримки fMP4
- 🧪 Написати integration-тест для перевірки коректності I-frame playlist у реальних плеєрах (AVPlayer, hls.js)
- 📈 Додати Prometheus-метрики для моніторингу часу генерації та кількості знайдених I-frames по каналах

🛠️