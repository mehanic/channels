# 🧪 Глибокий розбір `ogg/demuxer_test.go` — інтеграційне тестування Ogg демуксера

Це **інтеграційний тест** для валідації повного циклу демуксингу Ogg контейнера: від читання сирих байт файлу через інкрементальний парсинг до екстракції медіа-кадрів з коректними часовими мітками. Тест перевіряє підтримку Opus (аудіо) та VP8 (відео) кодеків. Розберемо архітектурно:

---

## 📦 1. Структура тесту: table-driven з одним кейсом

### 🔧 Шаблон тесту:

```go
func TestDemuxer_Input(t *testing.T) {
    t.Run("ogg demux", func(t *testing.T) {
        // 1. Ініціалізація демуксера
        demuxer := NewDemuxer()
        
        // 2. Реєстрація callback'ів для спостереження за процесом
        demuxer.OnPacket = func(streamId uint32, granule uint64, packet []byte, lost int) {
            // Моніторинг сирих пакетів (закоментовано)
        }
        
        demuxer.OnFrame = func(streamId uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
            // Обробка екстрагованих кадрів: логування + перевірка метаданих
        }
        
        demuxer.OnPage = func(page *oggPage) {
            // Моніторинг сторінок (закоментовано)
        }
        
        // 3. Читання тестового файлу чанками по 4KB
        oggfile, _ := os.Open("test.ogg")  // ⚠️ ігнорування помилок!
        buf := make([]byte, 4096)
        for {
            n, err := oggfile.Read(buf)
            if err != nil {
                fmt.Println(err)  // ⚠️ логування замість t.Error!
                break
            }
            err = demuxer.Input(buf[0:n])  // ⚠️ ігнорування помилок демуксингу!
            if err != nil {
                fmt.Println(err)
            }
        }
        // ← ⚠️ Немає жодної перевірки результатів!
    })
}
```

### 🎯 Що перевіряється (неявно):

| Аспект | Очікувана поведінка | Чому це критично |
|--------|---------------------|-----------------|
| **Інкрементальний парсинг** | Коректна обробка чанків 4KB без втрати даних | Реальні мережеві потоки приходять фрагментами |
| **Детекція кодеку** | Автоматичне розпізнавання Opus/VP8 за magic bytes | Підтримка мультиплексних Ogg файлів без конфігурації |
| **Екстракція кадрів** | OnFrame викликається для кожного медіа-пакету | Основна функція демуксера: сирі байти → кадри |
| **Конвертація часу** | PTS/DTS обчислюються з granule position | Синхронізація аудіо/відео у подальшому пайплайні |
| **Метадані** | GetAudioParam()/GetVideoParam() повертають валідні дані | Ініціалізація декодерів, генерація HLS-плейлиста |

---

## 🔍 2. Callback'и: спостереження за внутрішнім станом

### 🔸 `OnPacket` — моніторинг сирих пакетів:

```go
demuxer.OnPacket = func(streamId uint32, granule uint64, packet []byte, lost int) {
    //fmt.Printf("onpacket sid:%d granule:%d package len:%d lost:%d\n", ...)
}
```

**Призначення**: 
• Відлагодження збірки пакетів з сегментів
• Детекція втрачених даних (`lost=1`)
• Аналіз розподілу гранул у потоці

**Чому закоментовано**: У продакшені цей рівень детального логування створює зайвий шум.

### 🔸 `OnFrame` — основний вихід демуксера:

```go
demuxer.OnFrame = func(streamId uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
    if cid == codec.CODECID_AUDIO_OPUS {
        // 1. Отримання метаданів при першому кадрі
        param := demuxer.GetAudioParam()
        if param != nil && !getAudioParam {
            fmt.Println(param)  // ← вивід у stdout, не у тест-лог!
            getAudioParam = true
        }
        // 2. Логування параметрів кадру
        fmt.Printf("opus frame:sid[%d] frame len:[%d] pts:[%d] dts:[%d] lost:%d\n", 
            streamId, len(frame), pts, dts, lost)
            
    } else if cid == codec.CODECID_VIDEO_VP8 {
        // Аналогічно для відео...
    }
}
```

**Ключові перевірки**:
```go
// 1. Чи викликається OnFrame для кожного кодеку?
// 2. Чи коректні значення:
//    • len(frame) > 0 (не порожній кадр)
//    • pts, dts не переповнені (не ^uint64(0))
//    • lost == 0 (немає втрат у тестовому файлі)
// 3. Чи метадані ініціалізуються один раз (прапорці getAudioParam/getVideoParam)
```

### 🔸 `OnPage` — моніторинг сторінок:

```go
demuxer.OnPage = func(page *oggPage) {
    // PrintPage(page)  // ← закоментовано
}
```

**Призначення**: Відлагодження парсингу заголовків сторінок, детекція розривів у послідовності.

---

## 📁 3. Робота з файлом: інкрементальне читання

### 🔧 Цикл читання:

```go
oggfile, _ := os.Open("test.ogg")  // ⚠️ ігнорування помилки відкриття!
defer oggfile.Close()  // ← відсутній! витік файлових дескрипторів

buf := make([]byte, 4096)  // 4KB буфер — типовий розмір для мережевих чанків
for {
    n, err := oggfile.Read(buf)
    if err != nil {
        fmt.Println(err)  // ⚠️ логування замість коректної обробки
        break  // ⚠️ вихід без перевірки, чи це очікуваний EOF
    }
    
    // Інкрементальний демуксинг: кожен чанк обробляється окремо
    err = demuxer.Input(buf[0:n])
    if err != nil {
        fmt.Println(err)  // ⚠️ помилка демуксингу ігнорується!
        // Потік продовжує обробку, хоча дані можуть бути пошкоджені
    }
}
// ← Немає фіналізації: flush(), перевірки результатів, валідації
```

### 🎯 Чому інкрементальне читання критичне?

```
Тест імітує реальні сценарії:
• Файлове читання: чанки довільного розміру
• Мережевий потік: пакети 1-1.5KB
• Розрізані сторінки на межі чанків

Без інкрементальної обробки:
• Потрібно завантажити весь файл у пам'ять → OOM для великих файлів
• Неможлива обробка live-потоків
• Висока затримка перед початком відтворення

З інкрементальною обробкою:
• Кожен виклик Input() обробляє доступні дані
• Кешування неповних сторінок у headCache/page.cache
• Миттєва реакція на нові дані
```

---

## 🐞 4. Критичні проблеми у тесті

### ❗ Найсерйозніші недоліки:

1. **Відсутність валідації результатів**:
   ```go
   // Тест ніколи не перевіряє:
   // • Чи були викликані OnFrame хоча б раз?
   // • Чи коректні значення PTS/DTS?
   // • Чи метадані містять очікувані параметри?
   // • Чи немає помилок демуксингу?
   
   // Тест може "успішно" завершитись з порожнім виводом!
   ```

2. **Масове ігнорування помилок**:
   ```go
   oggfile, _ := os.Open("test.ogg")  // ← якщо файл відсутній → panic при читанні!
   err = demuxer.Input(buf[0:n])
   if err != nil {
       fmt.Println(err)  // ← логування, але тест продовжується!
   }
   
   // Краще:
   oggfile, err := os.Open("test.ogg")
   if err != nil {
       t.Fatalf("open test file: %v", err)
   }
   defer oggfile.Close()  // ← обов'язкове закриття!
   
   if err := demuxer.Input(buf[:n]); err != nil {
       t.Errorf("demux error at offset %d: %v", fileOffset, err)
       break  // зупинити тест при помилці
   }
   ```

3. **`fmt.Printf` замість `t.Logf`**:
   ```go
   fmt.Printf("opus frame:sid[%d] frame len:[%d] pts:[%d] dts:[%d]\n", ...)
   // ← вивід у stdout, не у тест-лог → важкий дебаг у CI
   
   // Краще:
   if testing.Verbose() {
       t.Logf("Opus frame: stream=%d, len=%d, pts=%d, dts=%d", 
           streamId, len(frame), pts, dts)
   }
   ```

4. **Витік ресурсів**:
   ```go
   // Відсутній defer oggfile.Close() → файловий дескриптор не закриється при помилці!
   // Краще:
   t.Cleanup(func() { oggfile.Close() })  // гарантоване закриття навіть при panic
   ```

5. **Відсутність фіналізації**:
   ```go
   // Після циклу читання:
   // • Не викликається flush() для відправки залишкових даних
   // • Не перевіряється, чи всі потоки коректно завершено
   // • Не валідується загальна кількість екстрагованих кадрів
   ```

6. **Жорстка залежність від `test.ogg`**:
   ```go
   // Якщо файл відсутній → тест падає без зрозумілого повідомлення
   // Краще: використовувати t.Skip() або embed тестових даних
   ```

---

## 💡 5. Покращення тесту: від "запуску коду" до "гарантії коректності"

### 🔧 Покращена версія тесту:

```go
func TestDemuxer_Input_Comprehensive(t *testing.T) {
    // 1. Підготовка: перевірка наявності тестового файлу
    testFile := "testdata/test.ogg"  // ← стандартне розташування
    if _, err := os.Stat(testFile); os.IsNotExist(err) {
        t.Skipf("test file %s not found — run generate_testdata.sh first", testFile)
    }
    
    // 2. Ініціалізація демуксера з валідацією
    demuxer := NewDemuxer()
    
    // 3. Збір статистики через callback'и
    var stats struct {
        audioFrames, videoFrames int
        firstAudioPTS, firstVideoPTS uint64
        lastAudioPTS, lastVideoPTS uint64
        errors []error
    }
    
    demuxer.OnFrame = func(streamId uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
        if lost == 1 {
            stats.errors = append(stats.errors, fmt.Errorf("lost packet in stream %d", streamId))
            return
        }
        
        switch cid {
        case codec.CODECID_AUDIO_OPUS:
            stats.audioFrames++
            if stats.firstAudioPTS == 0 { stats.firstAudioPTS = pts }
            stats.lastAudioPTS = pts
            if len(frame) == 0 {
                stats.errors = append(stats.errors, errors.New("empty Opus frame"))
            }
        case codec.CODECID_VIDEO_VP8:
            stats.videoFrames++
            if stats.firstVideoPTS == 0 { stats.firstVideoPTS = pts }
            stats.lastVideoPTS = pts
            if len(frame) == 0 {
                stats.errors = append(stats.errors, errors.New("empty VP8 frame"))
            }
        }
    }
    
    // 4. Читання файлу з обробкою помилок
    file, err := os.Open(testFile)
    if err != nil {
        t.Fatalf("open test file: %v", err)
    }
    t.Cleanup(func() { file.Close() })
    
    buf := make([]byte, 4096)
    var totalRead int
    for {
        n, err := file.Read(buf)
        totalRead += n
        if err == io.EOF {
            break
        }
        if err != nil {
            t.Fatalf("read error at offset %d: %v", totalRead-n, err)
        }
        if err := demuxer.Input(buf[:n]); err != nil {
            t.Errorf("demux error at offset %d: %v", totalRead-n, err)
            // Не break — продовжити обробку для збору більше статистики
        }
    }
    
    // 5. Фіналізація: відправити залишкові дані
    // (якщо Demuxer має метод Flush, викликати його тут)
    
    // 6. Валідація результатів
    if stats.audioFrames == 0 && stats.videoFrames == 0 {
        t.Error("no frames extracted from Ogg file")
    }
    
    if len(stats.errors) > 0 {
        t.Errorf("encountered %d errors during demuxing: %v", len(stats.errors), stats.errors)
    }
    
    // 7. Перевірка метаданів
    if audioParam := demuxer.GetAudioParam(); audioParam != nil {
        if audioParam.CodecId != codec.CODECID_AUDIO_OPUS {
            t.Errorf("unexpected audio codec: got %v, want %v", 
                audioParam.CodecId, codec.CODECID_AUDIO_OPUS)
        }
        if audioParam.SampleRate == 0 {
            t.Error("audio sample rate should be non-zero")
        }
        if audioParam.ChannelCount == 0 {
            t.Error("audio channel count should be non-zero")
        }
        t.Logf("Audio params: %d Hz, %d channels, preskip=%d", 
            audioParam.SampleRate, audioParam.ChannelCount, audioParam.InitialPadding)
    }
    
    if videoParam := demuxer.GetVideoParam(); videoParam != nil {
        if videoParam.CodecId != codec.CODECID_VIDEO_VP8 {
            t.Errorf("unexpected video codec: got %v, want %v", 
                videoParam.CodecId, codec.CODECID_VIDEO_VP8)
        }
        if videoParam.Width == 0 || videoParam.Height == 0 {
            t.Error("video resolution should be non-zero")
        }
        t.Logf("Video params: %dx%d @ %d fps", 
            videoParam.Width, videoParam.Height, videoParam.FrameRate)
    }
    
    // 8. Перевірка часових міток (якщо є і аудіо, і відео)
    if stats.audioFrames > 0 && stats.videoFrames > 0 {
        // PTS мають бути в розумному діапазоні (не переповнені)
        if stats.firstAudioPTS == ^uint64(0) || stats.firstVideoPTS == ^uint64(0) {
            t.Error("PTS values appear uninitialized")
        }
        // Аудіо та відео мають приблизно синхронізовані часові мітки
        // (з допуском на початковий preskip для Opus)
        audioDuration := stats.lastAudioPTS - stats.firstAudioPTS
        videoDuration := stats.lastVideoPTS - stats.firstVideoPTS
        if audioParam := demuxer.GetAudioParam(); audioParam != nil {
            // Конвертація: семпли @ 48 kHz → ms
            audioDurationMs := audioDuration * 1000 / 48000
            videoDurationMs := videoDuration * 1000 / uint64(videoParam.FrameRate)
            if audioDurationMs > videoDurationMs+100 || videoDurationMs > audioDurationMs+100 {
                t.Logf("warning: audio/video duration mismatch: audio=%dms, video=%dms", 
                    audioDurationMs, videoDurationMs)
                // Не Fail, бо це може бути легітимним (наприклад, аудіо коротше)
            }
        }
    }
    
    // 9. Логування підсумків
    t.Logf("Demuxed: %d audio frames, %d video frames, total read: %d bytes", 
        stats.audioFrames, stats.videoFrames, totalRead)
}
```

### 🔧 Додаткові тести для edge cases:

```go
// Тест: розрізана сторінка на межі чанків
func TestDemuxer_Input_SplitPage(t *testing.T) {
    demuxer := NewDemuxer()
    var frameCount int
    demuxer.OnFrame = func(streamId uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
        frameCount++
    }
    
    // Створити тестову Ogg сторінку, розрізану на два чанки
    page := createTestOggPage()  // helper function
    chunk1 := page.headerAndFirstHalf()
    chunk2 := page.secondHalf()
    
    // Перший виклик: неповна сторінка
    err1 := demuxer.Input(chunk1)
    if err1 != nil {
        t.Errorf("unexpected error on partial page: %v", err1)
    }
    if frameCount > 0 {
        t.Error("no frames should be extracted from partial page")
    }
    
    // Другий виклик: завершення сторінки
    err2 := demuxer.Input(chunk2)
    if err2 != nil {
        t.Errorf("unexpected error on complete page: %v", err2)
    }
    if frameCount == 0 {
        t.Error("frames should be extracted from complete page")
    }
}

// Тест: втрата сторінки (імітація мережевих помилок)
func TestDemuxer_Input_LostPage(t *testing.T) {
    demuxer := NewDemuxer()
    var lostCount int
    demuxer.OnFrame = func(streamId uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
        if lost == 1 {
            lostCount++
        }
    }
    
    // Прочитати файл, але пропустити один чанк посередині
    file, _ := os.Open("testdata/test.ogg")
    defer file.Close()
    
    buf := make([]byte, 4096)
    chunkNum := 0
    for {
        n, err := file.Read(buf)
        if err == io.EOF { break }
        chunkNum++
        if chunkNum == 5 {
            // Імітувати втрату чанку: не викликати Input()
            continue
        }
        demuxer.Input(buf[:n])  // ігноруємо помилки для цього тесту
    }
    
    // Очікуємо хоча б один кадр з lost=1
    if lostCount == 0 {
        t.Log("warning: no lost frames detected — test may need adjustment")
    } else {
        t.Logf("detected %d lost frames after simulated packet loss", lostCount)
    }
}

// Тест: порожній вхід
func TestDemuxer_Input_Empty(t *testing.T) {
    demuxer := NewDemuxer()
    err := demuxer.Input([]byte{})
    if err != nil {
        t.Errorf("empty input should not return error: %v", err)
    }
    // Повинно повернутись без помилок і без виклику callback'ів
}

// Тест: невалідні дані (не Ogg потік)
func TestDemuxer_Input_Invalid(t *testing.T) {
    demuxer := NewDemuxer()
    // Випадкові байти, що не містять "OggS" magic
    invalidData := []byte("this is not an ogg file")
    err := demuxer.Input(invalidData)
    // Повинно або повернути помилку, або ігнорувати дані без паніки
    // Залежить від реалізації readPage()
    if err == nil {
        t.Log("invalid data was silently ignored — consider returning error for robustness")
    }
}
```

---

## 🎯 6. Інтеграція з CI/CD: автоматизація тестування

### 🔧 GitHub Actions приклад:

```yaml
# .github/workflows/test-ogg.yml
name: Test Ogg Demuxer

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    
    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'
    
    - name: Generate test data
      run: |
        # Створити тестовий Ogg файл з відомими параметрами
        ffmpeg -f lavfi -i testsrc=duration=1:size=320x240:rate=30 \
               -f lavfi -i sine=frequency=1000:duration=1 \
               -c:v libvpx -c:a libopus -shortest testdata/test.ogg
    
    - name: Run tests
      run: go test -v -coverprofile=ogg.cover ./ogg/...
    
    - name: Check coverage
      run: |
        go tool cover -func=ogg.cover | grep -E "(Demuxer|readPacket|findCodec)" 
        # Перевірити, що критичні функції мають >80% покриття
```

### 🔧 Fuzz-тест для стійкості:

```go
func FuzzDemuxer_Input(f *testing.F) {
    // Seed з валідним Ogg файлом
    testData, _ := os.ReadFile("testdata/test.ogg")
    f.Add(testData)
    
    // Seed з частковими даними
    f.Add(testData[:100])
    f.Add([]byte("OggS"))  // тільки magic
    
    f.Fuzz(func(t *testing.T, data []byte) {
        demuxer := NewDemuxer()
        // Не повинно панікувати навіть на сміттєвих даних
        defer func() {
            if r := recover(); r != nil {
                t.Errorf("panic on input %x: %v", data[:min(32, len(data))], r)
            }
        }()
        _ = demuxer.Input(data)  // ігноруємо помилку, головне — стабільність
    })
}
```

---

## 🧭 Висновок: чому цей тест потребує розширення

| Проблема | Ризик | Вартість виправлення |
|----------|-------|---------------------|
| Відсутність перевірки результатів | Тести "проходять" при реальному збої → хибне відчуття надійності | Низька: додати лічильники кадрів та валідацію метаданих |
| Ігнорування помилок | Помилки демуксингу не детектуються → пошкоджені дані у пайплайні | Низька: замінити `fmt.Println` на `t.Error`/`t.Fatal` |
| `fmt.Printf` замість `t.Logf` | Шум у логах, важкий дебаг у CI | Низька: замінити виклики |
| Витік файлових дескрипторів | Вичерпання ресурсів при тривалому тестуванні | Низька: додати `defer`/`t.Cleanup` |
| Жорстка залежність від `test.ogg` | Тест неможливо запустити без підготовки даних | Середня: додати генерацію тестових даних або embed |

> 🔑 **Головна ідея**: Цей тест — **перша лінія оборони** для вашого Ogg демуксера. Він має не просто "запустити код", а гарантувати:
> 1. Коректну обробку помилок на кожному кроці
> 2. Валідність екстрагованих кадрів та метаданих
> 3. Стійкість до невалідних вхідних даних
> 4. Масштабованість (інкрементальна обробка, обмеження кешів)
> 5. Чистоту ресурсів (автоматичне закриття файлів)

💡 **Фінальна порада**: 
1. Додайте валідацію результатів: лічильники кадрів, перевірку PTS/DTS, валідацію метаданих
2. Замініть `fmt.Printf` на `t.Logf` для чистого виводу в CI/CD
3. Додайте `defer file.Close()` або `t.Cleanup` для гарантованого закриття ресурсів
4. Створіть скрипт генерації тестових даних (`testdata/generate.sh`) для відтворюваності
5. Додайте fuzz-тест для перевірки стійкості до випадкових/пошкоджених даних

Це перетворить цей тест з "формальної перевірки" на "гарантію коректності демуксингу" для всього вашого Ogg/HLS пайплайну.