# Глибоке роз'яснення: `webvtt.TestRead` — інтеграційний тест сегментації WebVTT

Цей файл містить **простий інтеграційний тест** для перевірки роботи парсингу та сегментації WebVTT субтитрів. Тест використовує реальний файл з баг-трекера FFmpeg для валідації сумісності.

---

## 🎯 Навіщо цей тест потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ TestRead у контексті розробки:         │
│                                         │
│ 🔹 Регресійне тестування:              │
│   • Файл з FFmpeg ticket #4048 —       │
│     відомий кейс з проблемами парсингу │
│   • Гарантує, що виправлення не        │
│     зламають існуючу функціональність  │
│                                         │
│ 🔹 Інтеграційна перевірка:             │
│   • Тестує повний пайплайн:            │
│     ReadFromWebVTT → segment → файли   │
│   • Перевіряє взаємодію компонентів    │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Валідація обробки реальних         │
│     субтитрів з різних джерел          │
│   • Детекція проблем синхронізації     │
│     таймінгів                          │
└─────────────────────────────────────────┘
```

---

## 🔍 Розбір тесту `TestRead`

```go
func TestRead(t *testing.T) {
    // 🔹 1. Відкрити тестовий WebVTT файл
    // Джерело: https://trac.ffmpeg.org/ticket/4048
    f, err := os.Open("tests/test1.vtt")
    if err != nil {
        t.Error("Cannot read test vtt file:", err)
        return  // 🔹 Важливо: повернутися при помилці
    }
    defer f.Close()  // 🔹 Закрити файл після тесту

    // 🔹 2. Створити тимчасовий каталог для виходу
    outputDir, err := ioutil.TempDir("", "go-hls-encoder-test")
    if err != nil {
        t.Error("Cannot create output dir:", err)
        return
    }
    defer os.RemoveAll(outputDir)  // 🔹 Прибрати після тесту
    
    fmt.Printf("Output directory: %q\n", outputDir)  // 🔹 Debug: показати шлях

    // 🔹 3. Запустити парсинг у фоні
    c := make(chan SubtitleBlock)
    go ReadFromWebVTT(f, c)  // 🔹 Асинхронний парсинг

    // 🔹 4. Виконати сегментацію: 5-секундні сегменти
    segment(c, 5*time.Second, outputDir, "test1")
    
    // 🔹 TODO: Перевірити вихідні файли
    // ❌ Зараз тест завжди проходить, навіть якщо сегментація не спрацювала
}
```

### ⚠️ Критичні проблеми поточного тесту

| Проблема | Наслідок | Рішення |
|----------|----------|---------|
| **Немає `defer f.Close()`** | Витік файлових дескрипторів при помилці | 🔹 Додати `defer f.Close()` після успішного `os.Open` |
| **Немає `defer os.RemoveAll()`** | Тимчасові файли залишаються після тесту | 🔹 Додати очищення каталогу |
| **Немає валідації виходу** | Тест проходить навіть при порожньому виході | 🔹 Перевірити наявність та вміст `.vtt`/`.m3u8` файлів |
| **Немає `t.Parallel()`** | Тести виконуються послідовно, повільніше | 🔹 Додати для паралельного запуску |
| **Жорсткий шлях до файлу** | Тест не працює при запуску з іншої директорії | 🔹 Використовувати `filepath.Join` або `embed` |

---

## 🛠️ Покращена версія тесту для вашого пайплайну

```go
func TestRead_Integration(t *testing.T) {
    t.Parallel()  // 🔹 Дозволити паралельний запуск
    
    // 🔹 1. Відкрити тестовий файл з перевіркою існування
    testFile := filepath.Join("tests", "test1.vtt")
    if _, err := os.Stat(testFile); os.IsNotExist(err) {
        t.Skipf("Test file not found: %s", testFile)  // 🔹 Пропустити, не провалити
    }
    
    f, err := os.Open(testFile)
    if err != nil {
        t.Fatalf("Cannot open test vtt file: %v", err)  // 🔹 Fatalf зупиняє тест одразу
    }
    defer f.Close()  // 🔹 Гарантувати закриття

    // 🔹 2. Створити тимчасовий каталог
    outputDir := t.TempDir()  // 🔹 Краще за ioutil.TempDir: авто-очищення
    t.Logf("Output directory: %q", outputDir)

    // 🔹 3. Запустити сегментацію
    targetDuration := 5 * time.Second
    basename := "test1"
    
    errChan := make(chan error, 1)
    go func() {
        c := make(chan SubtitleBlock)
        go ReadFromWebVTT(f, c)
        errChan <- segment(c, targetDuration, outputDir, basename)
    }()
    
    // 🔹 4. Дочекатися завершення з таймаутом
    select {
    case err := <-errChan:
        if err != nil {
            t.Fatalf("Segmentation failed: %v", err)
        }
    case <-time.After(30 * time.Second):  // 🔹 Запобігання зависанню
        t.Fatal("Segmentation timed out")
    }

    // 🔹 5. Валідація вихідних файлів
    validateSegmentationOutput(t, outputDir, basename, targetDuration)
}

// 🔹 Helper для валідації виходу
func validateSegmentationOutput(t *testing.T, outputDir, basename string, targetDuration time.Duration) {
    t.Helper()  // 🔹 Показувати помилки у викликаючому тесті, не у хелпері
    
    // 🔹 5.1. Перевірити наявність плейлиста
    playlistPath := filepath.Join(outputDir, basename+".m3u8")
    playlistContent, err := os.ReadFile(playlistPath)
    if err != nil {
        t.Fatalf("Cannot read playlist %s: %v", playlistPath, err)
    }
    
    // 🔹 5.2. Перевірити заголовок M3U8
    playlistStr := string(playlistContent)
    assert.Contains(t, playlistStr, "#EXTM3U", "Missing M3U8 header")
    assert.Contains(t, playlistStr, "#EXT-X-VERSION:5", "Wrong HLS version")
    assert.Contains(t, playlistStr, fmt.Sprintf("#EXT-X-TARGETDURATION:%d", int(targetDuration.Seconds())))
    assert.Contains(t, playlistStr, "#EXT-X-ENDLIST", "Missing endlist tag")
    
    // 🔹 5.3. Підрахувати сегменти у плейлисті
    segmentLines := strings.Count(playlistStr, ".vtt\n")
    if segmentLines == 0 {
        t.Error("No segments found in playlist")
        return
    }
    t.Logf("Found %d segments in playlist", segmentLines)
    
    // 🔹 5.4. Перевірити кожен сегмент-файл
    for i := 0; i < segmentLines; i++ {
        segmentPath := filepath.Join(outputDir, fmt.Sprintf("%s-%05d.vtt", basename, i))
        
        // Файл має існувати
        if _, err := os.Stat(segmentPath); os.IsNotExist(err) {
            t.Errorf("Segment file not found: %s", segmentPath)
            continue
        }
        
        // Файл має бути валідним WebVTT
        content, err := os.ReadFile(segmentPath)
        if err != nil {
            t.Errorf("Cannot read segment %s: %v", segmentPath, err)
            continue
        }
        
        contentStr := string(content)
        assert.Contains(t, contentStr, "WEBVTT", "Missing WebVTT header in segment")
        
        // 🔹 Перевірити наявність таймкодів
        if !strings.Contains(contentStr, " --> ") {
            t.Errorf("No time boundaries found in segment %d", i)
        }
    }
    
    // 🔹 5.5. Перевірити тривалості у плейлисті
    // #EXTINF:5.000000,
    re := regexp.MustCompile(`#EXTINF:([\d.]+),`)
    matches := re.FindAllStringSubmatch(playlistStr, -1)
    
    for i, match := range matches {
        if len(match) < 2 {
            continue
        }
        duration, err := strconv.ParseFloat(match[1], 64)
        if err != nil {
            t.Errorf("Invalid duration in segment %d: %s", i, match[1])
            continue
        }
        
        // 🔹 Тривалість має бути <= TARGETDURATION + tolerance
        maxDuration := targetDuration.Seconds() + 0.5  // +500мс толерантність
        if duration > maxDuration {
            t.Errorf("Segment %d duration %.3fs exceeds max %.3fs", i, duration, maxDuration)
        }
        if duration <= 0 {
            t.Errorf("Segment %d has invalid duration: %.3fs", i, duration)
        }
    }
}
```

---

## 🧪 Додаткові тести для покриття кейсів

### 🔹 Тест на порожній вхідний файл

```go
func TestRead_EmptyFile(t *testing.T) {
    input := "WEBVTT\n\n"  // 🔹 Тільки заголовок, без блоків
    outputDir := t.TempDir()
    
    err := Segment(strings.NewReader(input), 5*time.Second, outputDir, "empty")
    assert.NoError(t, err)
    
    // 🔹 Має створити плейлист, але без сегментів
    playlist, _ := os.ReadFile(filepath.Join(outputDir, "empty.m3u8"))
    assert.Contains(t, string(playlist), "#EXT-X-ENDLIST")
    
    // 🔹 Не має бути .vtt файлів окрім можливо порожніх
    files, _ := filepath.Glob(filepath.Join(outputDir, "*.vtt"))
    // Фільтруємо сам плейлист
    vttFiles := make([]string, 0)
    for _, f := range files {
        if !strings.HasSuffix(f, ".m3u8") {
            vttFiles = append(vttFiles, f)
        }
    }
    // Може бути 0 або 1 порожній сегмент — обидва варіанти прийнятні
    assert.LessOrEqual(t, len(vttFiles), 1)
}
```

### 🔹 Тест на блоки на межі сегментів

```go
func TestRead_BoundaryBlocks(t *testing.T) {
    // 🔹 Блок, що перетинає межу 5с: [4.5с --> 5.5с]
    input := `WEBVTT

00:00:04.500 --> 00:00:05.500
Boundary test
`
    outputDir := t.TempDir()
    
    err := Segment(strings.NewReader(input), 5*time.Second, outputDir, "boundary")
    assert.NoError(t, err)
    
    // 🔹 Блок має з'явитися в ОБИДВОХ сегментах
    seg0 := readFile(t, filepath.Join(outputDir, "boundary-00000.vtt"))
    seg1 := readFile(t, filepath.Join(outputDir, "boundary-00001.vtt"))
    
    assert.Contains(t, seg0, "Boundary test", "Block missing from segment 0")
    assert.Contains(t, seg1, "Boundary test", "Block missing from segment 1 (astride duplication)")
}

func readFile(t *testing.T, path string) string {
    t.Helper()
    content, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("Cannot read %s: %v", path, err)
    }
    return string(content)
}
```

### 🔹 Тест на толерантність +500мс

```go
func TestRead_Tolerance(t *testing.T) {
    // 🔹 Блок трохи довший за targetDuration: [0с --> 5.3с] при target=5с
    input := `WEBVTT

00:00:00.000 --> 00:00:05.300
Slightly long block
`
    outputDir := t.TempDir()
    
    err := Segment(strings.NewReader(input), 5*time.Second, outputDir, "tolerance")
    assert.NoError(t, err)
    
    // 🔹 Блок має вміститися у перший сегмент завдяки +500мс
    seg0 := readFile(t, filepath.Join(outputDir, "tolerance-00000.vtt"))
    assert.Contains(t, seg0, "Slightly long block")
    
    // 🔹 Перевірити тривалість у плейлисті
    playlist := readFile(t, filepath.Join(outputDir, "tolerance.m3u8"))
    assert.Contains(t, playlist, "#EXTINF:5.300000,")  // ✅ Фактична тривалість
}
```

### 🔹 Бенчмарк продуктивності сегментації

```go
func BenchmarkSegment_LargeFile(b *testing.B) {
    // 🔹 Згенерувати великий WebVTT (1000 блоків)
    var builder strings.Builder
    builder.WriteString("WEBVTT\n\n")
    for i := 0; i < 1000; i++ {
        start := time.Duration(i*3) * time.Second
        end := start + 2*time.Second
        builder.WriteString(fmt.Sprintf("%02d:%02d:%02d.000 --> %02d:%02d:%02d.000\nBlock %d\n\n",
            start/time.Hour, (start/time.Minute)%60, (start/time.Second)%60,
            end/time.Hour, (end/time.Minute)%60, (end/time.Second)%60, i))
    }
    
    input := strings.NewReader(builder.String())
    outputDir := b.TempDir()
    
    b.ReportAllocs()
    b.ResetTimer()
    
    for i := 0; i < b.N; i++ {
        // 🔹 Створити новий reader для кожного запуску
        reader := strings.NewReader(builder.String())
        Segment(reader, 5*time.Second, outputDir, "bench")
    }
}

// Очікувані результати:
// BenchmarkSegment_LargeFile-8    100    15000000 ns/op    500000 B/op    200 allocs/op
// 🔹 ~15 мс на 1000 блоків — прийнятно для фоновой обробки
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Тест не знаходить `tests/test1.vtt` | `os.Open` помилка, тест падає | 🔹 Використовувати `t.Skip()` замість `t.Error()`; додати файл у репозиторій |
| Тест зависає | `segment()` не завершується, таймаут | 🔹 Додати `select` з таймаутом; перевірити, що `ReadFromWebVTT` закриває канал |
| Неправильні тривалості у плейлисті | Плеєр показує помилки синхронізації | 🔹 Перевірити, що `addSegmentToPlaylist` використовує `endTime-startTime` |
| Дублювання блоків небажане | Одна репліка з'являється двічі | 🔹 Це очікувана поведінка; якщо потрібно змінити — модифікувати логіку "astride" у `segment()` |
| Тест не очищає файли | Диск заповнюється тимчасовими файлами | 🔹 Використовувати `t.TempDir()` замість `ioutil.TempDir` + `defer os.RemoveAll` |

### Приклад перевірки таймауту у тесті:

```go
// 🔹 Запобігання зависанню тестів
func runWithTimeout(t *testing.T, fn func() error, timeout time.Duration) error {
    done := make(chan error, 1)
    go func() { done <- fn() }()
    
    select {
    case err := <-done:
        return err
    case <-time.After(timeout):
        t.Fatalf("Test timed out after %v", timeout)
        return fmt.Errorf("timeout")
    }
}

// Використання:
err := runWithTimeout(t, func() error {
    return Segment(reader, 5*time.Second, outputDir, "test")
}, 30*time.Second)
```

---

## 📦 Швидкий референс для написання тестів

```go
// 1: Базовий шаблон інтеграційного тесту:
func TestYourFeature_Integration(t *testing.T) {
    t.Parallel()
    
    // 🔹 Підготувати вхідні дані
    input := createTestInput(t)  // ваша helper-функція
    outputDir := t.TempDir()     // авто-очищення
    
    // 🔹 Запустити тестовану функцію
    err := YourFunction(input, outputDir, params)
    assert.NoError(t, err)
    
    // 🔹 Валідувати вихід
    validateOutput(t, outputDir, expectedResults)
}

// 2: Helper для читання файлів у тестах:
func readTestFile(t *testing.T, path string) string {
    t.Helper()
    content, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("Failed to read %s: %v", path, err)
    }
    return string(content)
}

// 3: Helper для порівняння з ігноруванням порядку:
func assertContainsAll(t *testing.T, haystack string, needles ...string) {
    t.Helper()
    for _, needle := range needles {
        assert.Contains(t, haystack, needle)
    }
}

// 4: Логування для відладки тестів:
func debugTestOutput(t *testing.T, outputDir string) {
    if !testing.Verbose() {
        return  // 🔹 Тільки у verbose режимі
    }
    
    files, _ := filepath.Glob(filepath.Join(outputDir, "*"))
    t.Logf("Generated %d files:", len(files))
    for _, f := range files {
        content, _ := os.ReadFile(f)
        t.Logf("  %s:\n%s", filepath.Base(f), string(content))
    }
}

// 5: Пропуск тесту за умови:
func skipIfNoTestFile(t *testing.T, path string) {
    t.Helper()
    if _, err := os.Stat(path); os.IsNotExist(err) {
        t.Skipf("Test file not found: %s (run with -tags=integration to download)", path)
    }
}
```

---

## 📊 Матриця типів тестів для webvtt

```
Тип тесту              | Призначення                     | Приклад
───────────────────────┼─────────────────────────────────┼─────────────────────────
Юніт-тест              | Перевірка окремої функції       | TestParseDurationWebVTT_Formats
Інтеграційний тест     | Перевірка взаємодії компонентів | TestRead_Integration (цей файл)
Регресійний тест       | Запобігання поломки існуючого   | TestRead з FFmpeg ticket #4048
Бенчмарк               | Вимір продуктивності            | BenchmarkSegment_LargeFile
Fuzz-тест              | Пошук крашів на випадкових даних| FuzzSegment_WebVTT (потрібно додати)
```

---

## 📚 Корисні посилання

- [Go testing package documentation](https://pkg.go.dev/testing)
- [WebVTT specification (W3C)](https://www.w3.org/TR/webvtt1/)
- [FFmpeg ticket #4048 (джерело тесту)](https://trac.ffmpeg.org/ticket/4048)
- [Go testing best practices](https://go.dev/doc/tutorial/add-a-test)

> 💡 **Ключова ідея**: Цей тест — це "страхова поліс" вашого webvtt модуля. Він:
> - 🎯 Гарантує, що зміни не зламають обробку реальних WebVTT файлів
> - 🔍 Детектує регресії при оновленні залежностей або рефакторингу
> - ⚡ Допомагає оптимізувати продуктивність через бенчмарки
> - 🛡️ Запобігає production-помилкам через валідацію edge cases

Якщо потрібно — можу допомогти:
- 🔄 Додати fuzz-тести для пошуку крашів на випадкових WebVTT вхідних даних
- 🧪 Написати property-based тести для валідації інваріантів сегментації (напр., "сума тривалостей сегментів ≈ тривалості входу")
- 📈 Додати Prometheus-метрики для моніторингу успішності сегментації у production-середовищі

🛠️