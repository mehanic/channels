# 🧪 Глибокий розбір `flv/flv_test.go` — інтеграційне тестування FLV парсера/мультиплексора

Це **набір інтеграційних тестів** для валідації повного циклу роботи з форматом FLV: читання → демуксинг → запис. Тести перевіряють критичні шляхи: екстракцію H.264/H.265/AAC кадрів, roundtrip-конвертацію та обробку різних кодеків. Розберемо архітектурно:

---

## 📦 1. `TestFlvReader_Input` — екстракція кадрів з FLV у сирі бітстріми

### 🔧 Структура тесту:

```go
func TestFlvReader_Input(t *testing.T) {
    // 1. Підготовка файлів для виводу
    videoFile, _ := os.OpenFile("v.h264", os.O_CREATE|os.O_RDWR, 0666)  // ← ігнорування помилок!
    audioFile, _ := os.OpenFile("a.aac", os.O_CREATE|os.O_RDWR, 0666)
    
    // 2. Створення читача
    f := CreateFlvReader()
    
    // 3. Реєстрація callback: розподіл кадрів за кодеком
    f.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
        if cid == codec.CODECID_VIDEO_H264 {
            videoFile.Write(frame)  // ← ігнорування помилок запису!
        } else if cid == codec.CODECID_AUDIO_AAC {
            audioFile.Write(frame)
        }
    }
    
    // 4. Читання вхідного файлу чанками по 4KB
    fd, _ := os.Open("source.200kbps.768x320.flv")  // ← ігнорування помилок відкриття!
    cache := make([]byte, 4096)
    for {
        n, err := fd.Read(cache)
        if err != nil {
            fmt.Println(err)  // ← логування замість t.Error!
            break
        }
        f.Input(cache[0:n])  // ← ігнорування помилок парсингу!
    }
}
```

### 🎯 Що перевіряється:

| Крок | Очікувана поведінка | Чому це критично |
|------|---------------------|-----------------|
| **Інкрементальний Input()** | Коректна обробка чанків 4KB без втрати даних | Реальні мережеві потоки приходять фрагментами |
| **Динамічна детекція кодеку** | Автоматичне створення AVCTagDemuxer/AACTagDemuxer | Підтримка різних камер без попередньої конфігурації |
| **Розподіл за CID** | Відео → v.h264, Аудіо → a.aac | Ізоляція потоків для подальшої обробки |
| **Timestamp handling** | PTS/DTS передаються у callback (але не використовуються у тесті) | Синхронізація аудіо/відео у пайплайні |

### 🐞 Критичні проблеми:

1. **Масове ігнорування помилок**:
   ```go
   videoFile, _ := os.OpenFile(...)  // ← якщо файл не створиться — тест падатиме пізніше з незрозумілою помилкою
   f.Input(cache[0:n])  // ← якщо парсинг зламається — тест "успішно" завершиться з неповними даними!
   
   // Краще:
   videoFile, err := os.OpenFile(...)
   if err != nil { t.Fatalf("failed to create video file: %v", err) }
   
   if err := f.Input(cache[0:n]); err != nil {
       t.Errorf("FLV parse error at offset %d: %v", fdOffset, err)
       break
   }
   ```

2. **Відсутність валідації результату**:
   ```go
   // Тест нічого не перевіряє після виконання!
   // Може "успішно" завершитись з порожніми вихідними файлами
   
   // Краще додати:
   videoStat, _ := videoFile.Stat()
   if videoStat.Size() == 0 {
       t.Error("output video file is empty")
   }
   audioStat, _ := audioFile.Stat()
   if audioStat.Size() == 0 {
       t.Error("output audio file is empty")
   }
   ```

3. **`fmt.Println` замість `t.Logf`**:
   ```go
   fmt.Println(err)  // ← вивід у stdout, не у тест-лог
   // Краще:
   t.Logf("read error (expected at EOF): %v", err)
   ```

4. **Витік ресурсів при помилці**:
   ```go
   // Якщо помилка станеться після videoFile.Open, але до defer — файл не закриється
   // Краще використовувати t.Cleanup():
   t.Cleanup(func() { videoFile.Close() })
   ```

### 💡 Покращення тесту:

```go
func TestFlvReader_Input_Comprehensive(t *testing.T) {
    // 1. Підготовка тимчасових файлів з автоматичним очищенням
    tmpDir := t.TempDir()  // Go 1.15+: авто-видалення після тесту
    videoPath := filepath.Join(tmpDir, "v.h264")
    audioPath := filepath.Join(tmpDir, "a.aac")
    
    videoFile, err := os.Create(videoPath)
    if err != nil { t.Fatalf("create video file: %v", err) }
    t.Cleanup(func() { videoFile.Close() })
    
    audioFile, err := os.Create(audioPath)
    if err != nil { t.Fatalf("create audio file: %v", err) }
    t.Cleanup(func() { audioFile.Close() })
    
    // 2. Створення читача з валідацією
    f := CreateFlvReader()
    var frameCount int
    var firstVideoPTS, firstAudioPTS uint32
    
    f.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
        frameCount++
        switch cid {
        case codec.CODECID_VIDEO_H264:
            if firstVideoPTS == 0 { firstVideoPTS = pts }
            n, err := videoFile.Write(frame)
            if err != nil { t.Errorf("write video frame: %v", err) }
            if n != len(frame) { t.Errorf("short write: %d/%d", n, len(frame)) }
        case codec.CODECID_AUDIO_AAC:
            if firstAudioPTS == 0 { firstAudioPTS = pts }
            if _, err := audioFile.Write(frame); err != nil {
                t.Errorf("write audio frame: %v", err)
            }
        }
    }
    
    // 3. Читання вхідного файлу
    inputPath := "testdata/source.200kbps.768x320.flv"  // ← використовувати testdata!
    fd, err := os.Open(inputPath)
    if err != nil { t.Skipf("test file not found: %v", err) }  // Skip, а не Fail, якщо файл відсутній
    defer fd.Close()
    
    cache := make([]byte, 4096)
    var totalRead int
    for {
        n, err := fd.Read(cache)
        totalRead += n
        if err != nil {
            if err == io.EOF { break }
            t.Fatalf("read input: %v", err)
        }
        if err := f.Input(cache[:n]); err != nil {
            t.Fatalf("parse FLV at offset %d: %v", totalRead-n, err)
        }
    }
    
    // 4. Валідація результатів
    if frameCount == 0 {
        t.Error("no frames extracted")
    }
    videoStat, _ := videoFile.Stat()
    if videoStat.Size() == 0 {
        t.Error("output video is empty")
    }
    // 5. Додатково: перевірити, що перші байти відео — валідний H.264 start code
    videoFile.Seek(0, 0)
    var probe [4]byte
    videoFile.Read(probe[:])
    if !bytes.Equal(probe[:], []byte{0x00,0x00,0x00,0x01}) && 
       !bytes.Equal(probe[:3], []byte{0x00,0x00,0x01}) {
        t.Errorf("output video doesn't start with Annex-B start code: %x", probe)
    }
}
```

---

## 🔄 2. `TestFlvWriter_Write` — roundtrip тест: FLV → кадри → FLV

### 🔧 Логіка тесту:

```go
func TestFlvWriter_Write(t *testing.T) {
    // 1. Створення вихідного файлу
    newflv, _ := os.OpenFile("new.flv", os.O_CREATE|os.O_RDWR, 0666)
    wf := CreateFlvWriter(newflv)
    wf.WriteFlvHeader()  // ← ігнорування помилки запису заголовку!
    
    // 2. Створення читача з callback, що перепаковує кадри назад у FLV
    rf := CreateFlvReader()
    rf.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
        if cid == codec.CODECID_VIDEO_H264 {
            wf.WriteH264(frame, pts, dts)  // ← ігнорування помилок запису!
        } else if cid == codec.CODECID_AUDIO_AAC {
            wf.WriteAAC(frame, pts, dts)
        }
    }
    
    // 3. Читання всього файлу в пам'ять (не інкрементально!)
    fd, _ := os.Open("source.200kbps.768x320.flv")
    content, _ := ioutil.ReadAll(fd)  // ← для великих файлів: OOM ризик!
    rf.Input(content)  // ← ігнорування помилок
}
```

### 🎯 Що перевіряється:

| Аспект | Очікувана поведінка | Чому це критично |
|--------|---------------------|-----------------|
| **Roundtrip цілісність** | new.flv має бути відтворюваним після перепакування | Гарантія, що демуксинг/мультиплексинг не ламає дані |
| **Збереження timestamp'ів** | PTS/DTS передаються з Reader → Writer без спотворень | Синхронізація аудіо/відео у вихідному файлі |
| **Конвертація форматів** | Annex-B (вхід) → AVCC (FLV) → Annex-B (вихід) | Сумісність з іншими компонентами пайплайну |

### 🐞 Критичні проблеми:

1. **Відсутність порівняння вхід/вихід**:
   ```go
   // Тест ніколи не перевіряє, чи new.flv ідентичний source.flv!
   // Може "успішно" завершитись з пошкодженим вихідним файлом
   
   // Краще додати валідацію:
   newflv.Close()  // закрити перед читанням
   origData, _ := os.ReadFile("source.200kbps.768x320.flv")
   newData, _ := os.ReadFile("new.flv")
   
   // FLV теги можуть мати різний порядок/метадані, тому порівнюємо не побайтово,
   // а через ffprobe:
   cmd := exec.Command("ffprobe", "-v", "error", "-show_streams", "new.flv")
   if err := cmd.Run(); err != nil {
       t.Errorf("output FLV is not valid: %v", err)
   }
   ```

2. **`ioutil.ReadAll` для великих файлів**:
   ```go
   content, _ := ioutil.ReadAll(fd)  // ← якщо файл 1GB → OOM!
   
   // Краще використовувати інкрементальне читання як у TestFlvReader_Input:
   cache := make([]byte, 64*1024)
   for {
       n, err := fd.Read(cache)
       if err == io.EOF { break }
       rf.Input(cache[:n])
   }
   ```

3. **Відсутність `t.Cleanup` для ресурсів**:
   ```go
   // Якщо тест паде на середині — файли залишаться відкритими
   // Краще:
   t.Cleanup(func() { newflv.Close(); fd.Close() })
   ```

### 💡 Покращення тесту:

```go
func TestFlvWriter_Write_Roundtrip(t *testing.T) {
    // 1. Підготовка тимчасових файлів
    tmpDir := t.TempDir()
    outputPath := filepath.Join(tmpDir, "new.flv")
    newflv, err := os.Create(outputPath)
    if err != nil { t.Fatalf("create output: %v", err) }
    t.Cleanup(func() { newflv.Close() })
    
    wf := CreateFlvWriter(newflv)
    if err := wf.WriteFlvHeader(); err != nil {
        t.Fatalf("write FLV header: %v", err)
    }
    
    // 2. Налаштування читача з підрахунком кадрів
    rf := CreateFlvReader()
    var videoFrames, audioFrames int
    var firstDTS, lastDTS uint32
    
    rf.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
        if cid == codec.CODECID_VIDEO_H264 {
            videoFrames++
            if firstDTS == 0 { firstDTS = dts }
            lastDTS = dts
            if err := wf.WriteH264(frame, pts, dts); err != nil {
                t.Errorf("write H264 frame: %v", err)
            }
        } else if cid == codec.CODECID_AUDIO_AAC {
            audioFrames++
            if err := wf.WriteAAC(frame, pts, dts); err != nil {
                t.Errorf("write AAC frame: %v", err)
            }
        }
    }
    
    // 3. Інкрементальне читання вхідного файлу
    inputPath := "testdata/source.200kbps.768x320.flv"
    fd, err := os.Open(inputPath)
    if err != nil { t.Skipf("input file not found: %v", err) }
    defer fd.Close()
    
    cache := make([]byte, 64*1024)
    for {
        n, err := fd.Read(cache)
        if err == io.EOF { break }
        if err != nil { t.Fatalf("read input: %v", err) }
        if err := rf.Input(cache[:n]); err != nil {
            t.Fatalf("parse FLV: %v", err)
        }
    }
    
    // 4. Валідація результатів
    if videoFrames == 0 && audioFrames == 0 {
        t.Error("no frames processed")
    }
    t.Logf("processed: %d video, %d audio frames, duration: %d ms", 
        videoFrames, audioFrames, lastDTS-firstDTS)
    
    // 5. Валідація вихідного файлу через ffprobe (якщо доступно)
    newflv.Close()  // закрити перед ffprobe
    cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", 
        "-show_entries", "stream=codec_name,width,height", "new.flv")
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Run(); err != nil {
        t.Logf("ffprobe validation skipped or failed: %v", err)
        // Не Fail, бо ffprobe може бути відсутній у CI
    }
}
```

---

## 🌀 3. `TestFlvWriter_WriteHevc` — запис сирих H.265 NAL units у FLV

### 🔧 Унікальність тесту:

```go
func TestFlvWriter_WriteHevc(t *testing.T) {
    // 1. Створення FLV-файлу для H.265
    newflv, _ := os.OpenFile("h265.flv", os.O_CREATE|os.O_RDWR, 0666)
    wf := CreateFlvWriter(newflv)
    wf.WriteFlvHeader()
    
    // 2. Читання сирих H.265 NAL units (Annex-B формат)
    rawh265, err := os.Open("1.h265")
    buf, _ := ioutil.ReadAll(rawh265)  // ← знову ReadAll!
    
    // 3. Розділення на NAL units через helper з codec пакету
    codec.SplitFrameWithStartCode(buf, func(nalu []byte) bool {
        // Дебаг-вивід: перші 5 байт кожного NAL
        fmt.Printf("%x %x %x %x %x\n", nalu[0], nalu[1], nalu[2], nalu[3], nalu[4])
        fmt.Printf("nalu size %d\n", len(nalu))
        
        // 4. Запис у FLV з фіксованим інтервалом 40ms (25fps)
        if err := wf.WriteH265(nalu, pts, dts); err != nil {
            fmt.Println(err)
        }
        pts += 40  // ← фіксований крок, не залежить від реального таймінгу!
        dts += 40
        return true  // продовжити ітерацію
    })
}
```

### 🎯 Що перевіряється:

| Аспект | Очікувана поведінка | Чому це критично |
|--------|---------------------|-----------------|
| **Запис H.265 у FLV** | Конвертація Annex-B → hvcc/AVCC → FLV теги | Підтримка HEVC камер у пайплайні |
| **Обробка окремих NAL units** | Кожен NAL записується як окремий кадр | Гнучкість: можна записувати тільки SPS/PPS, тільки IDR, тощо |
| **Генерація timestamp'ів** | Фіксований крок 40ms → 25fps у вихідному файлі | Контроль частоти кадрів при записі з сирих даних |

### 🐞 Критичні проблеми:

1. **Фіксований PTS/DTS крок ігнорує реальний таймінг**:
   ```go
   pts += 40; dts += 40  // ← припускає, що всі кадри рівновіддалені!
   
   // У реальності:
   // • IDR кадри можуть бути рідшими (наприклад, кожен 1с)
   // • B-frames мають різний CTS
   // • Змінний fps (наприклад, при детекції руху)
   
   // Краще: витягувати таймінг з метаданих або використовувати системний час
   ```

2. **Відсутність валідації вихідного файлу**:
   ```go
   // Тест не перевіряє, чи h265.flv валідний!
   // Може записати пошкоджені дані без попередження
   
   // Краще додати:
   newflv.Close()
   cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", 
       "-show_entries", "stream=codec_name", "h265.flv")
   if output, err := cmd.CombinedOutput(); err != nil {
       t.Errorf("output HEVC FLV invalid: %v\n%s", err, output)
   }
   ```

3. **`fmt.Printf` для дебагу замість `t.Logf`**:
   ```go
   fmt.Printf("%x %x...\n", ...)  // ← шум у stdout при запусці тестів
   // Краще:
   if testing.Verbose() {
       t.Logf("NAL header: %x %x...", nalu[0], nalu[1])
   }
   ```

### 💡 Покращення тесту:

```go
func TestFlvWriter_WriteHevc_Comprehensive(t *testing.T) {
    tmpDir := t.TempDir()
    outputPath := filepath.Join(tmpDir, "h265.flv")
    newflv, err := os.Create(outputPath)
    if err != nil { t.Fatalf("create output: %v", err) }
    defer newflv.Close()
    
    wf := CreateFlvWriter(newflv)
    if err := wf.WriteFlvHeader(); err != nil {
        t.Fatalf("write header: %v", err)
    }
    
    // Читання сирих H.265 даних
    inputPath := "testdata/1.h265"
    rawh265, err := os.Open(inputPath)
    if err != nil { t.Skipf("HEVC test file not found: %v", err) }
    defer rawh265.Close()
    
    buf, err := ioutil.ReadAll(rawh265)
    if err != nil { t.Fatalf("read HEVC: %v", err) }
    
    // Підрахунок статистики
    var naluCount, idrCount, spsCount, ppsCount int
    var lastPTS uint32
    
    codec.SplitFrameWithStartCode(buf, func(nalu []byte) bool {
        naluCount++
        
        // Детекція типів NAL для статистики
        if len(nalu) > 0 {
            nalType := codec.H265NaluTypeWithoutStartCode(nalu)
            switch {
            case nalType >= 16 && nalType <= 21: idrCount++  // IRAP
            case nalType == codec.H265_NAL_SPS: spsCount++
            case nalType == codec.H265_NAL_PPS: ppsCount++
            }
        }
        
        // Запис з реальним таймінгом (якщо доступно) або фіксованим
        // Для тесту використовуємо фіксований 40ms крок
        if err := wf.WriteH265(nalu, lastPTS, lastPTS); err != nil {
            t.Errorf("write HEVC NAL #%d: %v", naluCount, err)
            return false  // зупинити при помилці
        }
        lastPTS += 40
        return true
    })
    
    // Валідація статистики
    if naluCount == 0 {
        t.Error("no NAL units processed")
    }
    t.Logf("HEVC stats: %d NALs, %d IDR, %d SPS, %d PPS", 
        naluCount, idrCount, spsCount, ppsCount)
    
    // SPS/PPS мають бути перед першим IDR для валідного потоку
    if idrCount > 0 && (spsCount == 0 || ppsCount == 0) {
        t.Log("warning: IDR frames without SPS/PPS may cause decoding issues")
    }
    
    // Валідація вихідного файлу
    newflv.Close()
    cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", 
        "-show_entries", "stream=codec_name,profile,width,height", "h265.flv")
    output, err := cmd.CombinedOutput()
    if err != nil {
        t.Logf("ffprobe validation: %v\n%s", err, output)
    } else {
        t.Logf("ffprobe output:\n%s", output)
    }
}
```

---

## 🎬 4. `TestFlvReadH265` — читання H.265 з FLV

### 🔧 Простий тест екстракції:

```go
func TestFlvReadH265(t *testing.T) {
    videoFile, _ := os.OpenFile("v2.h265", os.O_CREATE|os.O_RDWR, 0666)
    f := CreateFlvReader()
    f.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint32) {
        if cid == codec.CODECID_VIDEO_H265 {
            videoFile.Write(frame)  // ← ігнорування помилок
        }
    }
    fd, _ := os.Open("l.flv")
    content, _ := ioutil.ReadAll(fd)  // ← ReadAll знову!
    f.Input(content)  // ← ігнорування помилок
}
```

### 🎯 Що перевіряється:
- Коректна детекція H.265 у FLV (CodecID = 12 у FLV spec)
- Створення HevcTagDemuxer при першому відео-тезі
- Екстракція кадрів у Annex-B форматі (з start codes)

### 💡 Покращення: аналогічно до інших тестів — додати валідацію вихідного файлу, обробку помилок, `t.Cleanup`.

---

## 🧭 Загальні рекомендації для тестів `flv/`

### ✅ Структура тестових даних:

```
testdata/
├── source.200kbps.768x320.flv    # H.264+AAC, низький бітрейт
├── 1.h265                        # Сирий H.265 Annex-B для тесту запису
├── l.flv                         # H.265 у FLV для тесту читання
├── README.md                     # Опис файлів: кодек, роздільна здатність, тривалість
└── generate_testdata.sh          # Скрипт генерації тестових файлів через ffmpeg
```

### ✅ Pattern: `t.Cleanup` + `t.TempDir`:

```go
func TestSomething(t *testing.T) {
    tmpDir := t.TempDir()  // авто-очищення після тесту
    outputPath := filepath.Join(tmpDir, "output.flv")
    
    file, err := os.Create(outputPath)
    if err != nil { t.Fatalf("..."); }
    t.Cleanup(func() { file.Close() })  // гарантоване закриття навіть при panic
    
    // ... тестова логіка ...
}
```

### ✅ Pattern: інкрементальна обробка замість `ReadAll`:

```go
// Погано для великих файлів:
content, _ := ioutil.ReadAll(fd)  // OOM ризик

// Краще:
cache := make([]byte, 64*1024)
for {
    n, err := fd.Read(cache)
    if err == io.EOF { break }
    if err != nil { t.Fatalf("read: %v", err) }
    processor.Input(cache[:n])  // інкрементальна обробка
}
```

### ✅ Pattern: валідація через `ffprobe`:

```go
func validateFLV(t *testing.T, path string) {
    t.Helper()  // показувати помилку у викликаючому тесті, не у цій функції
    cmd := exec.Command("ffprobe", "-v", "error", "-show_streams", path)
    output, err := cmd.CombinedOutput()
    if err != nil {
        t.Errorf("ffprobe failed for %s: %v\n%s", path, err, output)
    }
}
```

### ✅ Pattern: умовний `Skip` замість `Fail` при відсутності тестових файлів:

```go
fd, err := os.Open("testdata/large.flv")
if err != nil {
    if os.IsNotExist(err) {
        t.Skip("large test file not found — run generate_testdata.sh first")
    }
    t.Fatalf("open file: %v", err)
}
```

---

## 🎯 Висновок: чому ці тести потребують покращення

| Проблема | Ризик | Вартість виправлення |
|----------|-------|---------------------|
| Ігнорування помилок | Тести "проходять" при реальних помилках → хибне відчуття надійності | Низька: додати `if err != nil { t.Fatal(...) }` |
| Відсутність валідації результату | Пошкоджені вихідні файли не детектуються | Середня: додати ffprobe-перевірки |
| `ReadAll` для великих файлів | OOM у CI/при великих тестах | Низька: замінити на інкрементальне читання |
| `fmt.Println` замість `t.Logf` | Шум у логах, важкий дебаг | Низька: замінити виклики |
| Відсутність `t.Cleanup` | Витік файлових дескрипторів при помилках | Низька: додати cleanup-функції |

> 🔑 **Головна ідея**: Ці тести — **перша лінія оборони** для вашого FLV-підсистеми. Вони мають не просто "запустити код", а гарантувати:
> 1. Коректну обробку помилок на кожному кроці
> 2. Валідність вихідних даних через зовнішні інструменти (ffprobe)
> 3. Масштабованість (інкрементальна обробка, тимчасові файли)
> 4. Чистоту ресурсів (автоматичне закриття файлів)

💡 **Фінальна порада**: Створіть `flv/testhelpers.go` з reusable-функціями:

```go
// flv/testhelpers.go
package flv

import "testing"

// OpenTestFile відкриває файл з testdata/ або Skip-ить тест
func OpenTestFile(t *testing.T, name string) *os.File {
    t.Helper()
    path := filepath.Join("testdata", name)
    f, err := os.Open(path)
    if err != nil {
        if os.IsNotExist(err) {
            t.Skipf("test file %s not found", name)
        }
        t.Fatalf("open %s: %v", name, err)
    }
    t.Cleanup(func() { f.Close() })
    return f
}

// ValidateFLV перевіряє валідність файлу через ffprobe
func ValidateFLV(t *testing.T, path string) {
    t.Helper()
    cmd := exec.Command("ffprobe", "-v", "error", "-show_streams", path)
    if err := cmd.Run(); err != nil {
        t.Errorf("invalid FLV %s: %v", path, err)
    }
}

// ReadIncrementally читає файл чанками та викликає callback
func ReadIncrementally(t *testing.T, f *os.File, chunkSize int, fn func([]byte) error) {
    t.Helper()
    buf := make([]byte, chunkSize)
    for {
        n, err := f.Read(buf)
        if err == io.EOF { break }
        if err != nil { t.Fatalf("read: %v", err) }
        if err := fn(buf[:n]); err != nil {
            t.Fatalf("process chunk: %v", err)
        }
    }
}
```

Тоді тести стануть коротшими, надійнішими та легшими для підтримки:

```go
func TestFlvReader_Input(t *testing.T) {
    f := CreateFlvReader()
    // ... налаштування callback ...
    
    fd := OpenTestFile(t, "source.200kbps.768x320.flv")
    ReadIncrementally(t, fd, 4096, f.Input)
    
    // Валідація результату...
}
```