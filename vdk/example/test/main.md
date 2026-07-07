# 🎬 Глибокий розбір: TS File Reader — Простий дебагер MPEG-TS файлів

Цей файл — **мінімалістичний приклад читання MPEG-TS файлу** з використанням бібліотеки `vdk/format/ts`. Він демонструє базову роботу з демуксером: відкриття файлу, читання пакетів, та логування їхніх метаданих.

Розберемо архітектуру, критичні моменти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема прикладу

```
┌────────────────────────────────────────┐
│ 📦 TS File Reader — Debug Tool         │
├────────────────────────────────────────┤
│                                         │
│  📥 Вхід:                               │
│  • Файл: edb9708f....ts (MPEG-TS)      │
│                                         │
│  ⚙️  Обробка:                           │
│  ┌─────────────────┐                   │
│  │ os.Open()       │                   │
│  │ • Відкриття файлу│                   │
│  └────────┬────────┘                   │
│           │ *os.File                   │
│           ▼                            │
│  ┌─────────────────┐                   │
│  │ ts.NewDemuxer() │                   │
│  │ • Ініціалізація │                   │
│  └────────┬────────┘                   │
│           │ *ts.Demuxer                │
│           ▼                            │
│  ┌─────────────────┐                   │
│  │ ReadPacket()    │                   │
│  │ • Цикл читання  │                   │
│  │ • Логування     │                   │
│  └─────────────────┘                   │
│                                         │
│  📤 Вихід:                              │
│  • Логи: index, time, data snippet, size│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔍 Детальний розбір коду

### 1️⃣ Відкриття файлу:

```go
f, _ := os.Open("edb9708f29b24ba9b175808d6b9df9c6541e25766d4a40209a8f903948b72f3f.ts")
```

### ⚠️ Критична проблема: ігнорування помилки

```go
// ❌ НЕПРАВИЛЬНО: помилка ігнорується через _
f, _ := os.Open(filename)  // якщо файл не існує — f=nil, але код продовжується!

// ✅ ПРАВИЛЬНО: обробка помилки
f, err := os.Open(filename)
if err != nil {
    log.Fatalf("open file failed: %v", err)
}
defer f.Close()  // не забути закрити файл!
```

### 2️⃣ Створення демуксера:

```go
m := ts.NewDemuxer(f)
```

### 🔍 Що робить `ts.NewDemuxer()`:

```
• Приймає io.Reader (у нашому випадку *os.File)
• Ініціалізує внутрішній буфер для читання TS-пакетів (188 байт)
• Готується до парсингу заголовків пакетів:
  - Sync byte (0x47)
  - PID (Packet Identifier)
  - Adaptation field
  - PES payload
• Повертає *ts.Demuxer для подальшого читання
```

### 3️⃣ Основний цикл читання:

```go
var i int
for {
    p, err := m.ReadPacket()
    if err != nil {
        return  // ⚠️ Тихе завершення без логування!
    }
    
    // Скидання лічильника при ключовому кадрі
    if p.IsKeyFrame {
        i = 0
    }
    
    // Логування: індекс, час, фрагмент даних, довжина
    log.Println(i, p.Time, p.Data[4:10], len(p.Data))
    i++
}
```

### 🔍 Розбір логування:

```go
log.Println(i, p.Time, p.Data[4:10], len(p.Data))
```

| Поле | Тип | Призначення | Приклад виводу |
|------|-----|-------------|---------------|
| `i` | `int` | Лічильник пакетів (скидається на ключових кадрах) | `0`, `1`, `2`... |
| `p.Time` | `time.Duration` | PTS (Presentation Time Stamp) пакету | `1234567890` (наносекунди) |
| `p.Data[4:10]` | `[]byte` | Фрагмент даних (байти 4-9) для дебагу | `[71 0 0 1 27 16]` |
| `len(p.Data)` | `int` | Загальна довжина даних пакету | `1316` |

### 🔍 Чому `p.Data[4:10]`?

```
MPEG-TS пакет має структуру:
  • Байт 0: Sync byte (0x47)
  • Байти 1-3: Заголовок (PID, тощо)
  • Байт 4+: Початок PES payload або adaptation field

p.Data[4:10] показує перші 6 байт корисного навантаження,
що часто містить:
  • Для відео: початок NALU (напр. 0x00000001 для H.264)
  • Для аудіо: заголовок фрейму (напр. ADTS для AAC)

Це корисно для швидкої ідентифікації типу даних у дебазі.
```

---

## 🐞 Критичні проблеми у цьому коді

### ❌ Проблема 1: Ігнорування помилок

```go
// Відкриття файлу:
f, _ := os.Open(...)  // помилка ігнорується!

// Читання пакетів:
if err != nil {
    return  // тихе завершення без логування причини
}
```

### ✅ Рішення:

```go
f, err := os.Open(filename)
if err != nil {
    log.Fatalf("open file failed: %v", err)
}
defer f.Close()

for {
    p, err := m.ReadPacket()
    if err != nil {
        if err == io.EOF {
            log.Printf("EOF reached, processed %d packets", totalPackets)
            break
        }
        log.Printf("read packet failed: %v", err)
        return
    }
    // ... обробка ...
}
```

### ❌ Проблема 2: Нескінченний цикл без виходу

```go
for {
    // Ніколи не завершується, якщо немає помилки
}
```

### ✅ Рішення:

```go
// Додати ліміт пакетів для дебагу
maxPackets := 1000
for count := 0; count < maxPackets; count++ {
    p, err := m.ReadPacket()
    if err != nil {
        break
    }
    // ... обробка ...
}
```

### ❌ Проблема 3: Потенційна паніка при `p.Data[4:10]`

```go
// Якщо len(p.Data) < 10, це викличе panic: slice bounds out of range
log.Println(p.Data[4:10])  // небезпечно!
```

### ✅ Рішення:

```go
// Безпечний доступ до даних
var dataSnippet []byte
if len(p.Data) >= 10 {
    dataSnippet = p.Data[4:10]
} else if len(p.Data) > 4 {
    dataSnippet = p.Data[4:]
} else {
    dataSnippet = []byte{}
}
log.Println(i, p.Time, dataSnippet, len(p.Data))
```

### ❌ Проблема 4: Відсутність закриття ресурсів

```go
// f.Close() ніколи не викликається!
```

### ✅ Рішення:

```go
f, err := os.Open(filename)
if err != nil { /* ... */ }
defer f.Close()  // гарантоване закриття
```

---

## ✅ Ваш use-case: дебаг та аналіз TS файлів у CCTV

### Сценарій 1: Аналіз структури файлу

```go
// AnalyzeTSFile — детальний аналіз MPEG-TS файлу
func AnalyzeTSFile(filename string) error {
    f, err := os.Open(filename)
    if err != nil {
        return fmt.Errorf("open file: %w", err)
    }
    defer f.Close()
    
    demuxer := ts.NewDemuxer(f)
    
    // Отримання метаданих потоків
    streams, err := demuxer.Streams()
    if err != nil {
        return fmt.Errorf("get streams: %w", err)
    }
    
    log.Printf("File: %s", filename)
    log.Printf("Found %d streams:", len(streams))
    for i, s := range streams {
        log.Printf("  [%d] Type: %v, Codec: %v", i, s.Type(), s)
    }
    
    // Статистика пакетів
    var stats struct {
        TotalPackets   int
        VideoPackets   int
        AudioPackets   int
        KeyFrames      int
        FirstPTS       time.Duration
        LastPTS        time.Duration
    }
    
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        stats.TotalPackets++
        
        if stats.FirstPTS == 0 {
            stats.FirstPTS = pkt.Time
        }
        stats.LastPTS = pkt.Time
        
        // Класифікація за типом
        if pkt.Idx == 0 {  // припускаємо, що 0 = відео
            stats.VideoPackets++
            if pkt.IsKeyFrame {
                stats.KeyFrames++
            }
        } else if pkt.Idx == 1 {  // припускаємо, що 1 = аудіо
            stats.AudioPackets++
        }
        
        // Логування кожного 100-го пакету для дебагу
        if stats.TotalPackets%100 == 0 {
            log.Printf("Packet %d: idx=%d, time=%v, key=%v, size=%d",
                stats.TotalPackets, pkt.Idx, pkt.Time, pkt.IsKeyFrame, len(pkt.Data))
        }
    }
    
    // Фінальна статистика
    duration := stats.LastPTS - stats.FirstPTS
    log.Printf("Total: %d packets, %d video (%d keyframes), %d audio",
        stats.TotalPackets, stats.VideoPackets, stats.KeyFrames, stats.AudioPackets)
    log.Printf("Duration: %v (%.2f seconds)", duration, duration.Seconds())
    
    return nil
}
```

### Сценарій 2: Екстракція ключових кадрів для прев'ю

```go
// ExtractKeyFrames — збереження ключових кадрів у окремі файли
func ExtractKeyFrames(inputFile, outputPrefix string) error {
    f, err := os.Open(inputFile)
    if err != nil {
        return err
    }
    defer f.Close()
    
    demuxer := ts.NewDemuxer(f)
    keyFrameCount := 0
    
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
        
        // Збереження тільки відео ключових кадрів
        if pkt.IsKeyFrame && pkt.Idx == 0 {
            keyFrameCount++
            filename := fmt.Sprintf("%s_keyframe_%04d.h264", outputPrefix, keyFrameCount)
            
            outF, err := os.Create(filename)
            if err != nil {
                log.Printf("create %s failed: %v", filename, err)
                continue
            }
            
            // Запис даних (можливо з додаванням start codes)
            _, err = outF.Write(pkt.Data)
            outF.Close()
            
            if err != nil {
                log.Printf("write %s failed: %v", filename, err)
            } else {
                log.Printf("Saved keyframe %d: %s (%d bytes)", 
                    keyFrameCount, filename, len(pkt.Data))
            }
        }
    }
    
    log.Printf("Extracted %d keyframes", keyFrameCount)
    return nil
}
```

### Сценарій 3: Валідація цілісності файлу

```go
// ValidateTSFile — перевірка цілісності MPEG-TS файлу
func ValidateTSFile(filename string) error {
    f, err := os.Open(filename)
    if err != nil {
        return err
    }
    defer f.Close()
    
    demuxer := ts.NewDemuxer(f)
    
    var errors []string
    var lastPTS time.Duration
    packetCount := 0
    
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            errors = append(errors, fmt.Sprintf("packet %d: read error: %v", packetCount, err))
            break
        }
        
        packetCount++
        
        // Перевірка монотонності часу
        if pkt.Time < lastPTS {
            errors = append(errors, fmt.Sprintf("packet %d: non-monotonic PTS: %v < %v", 
                packetCount, pkt.Time, lastPTS))
        }
        lastPTS = pkt.Time
        
        // Перевірка розміру даних
        if len(pkt.Data) == 0 {
            errors = append(errors, fmt.Sprintf("packet %d: empty data", packetCount))
        }
        
        // Перевірка ключових кадрів (перший пакет має бути ключовим?)
        if packetCount == 1 && !pkt.IsKeyFrame {
            log.Printf("warning: first packet is not a keyframe")
        }
    }
    
    // Звіт про валідацію
    if len(errors) > 0 {
        log.Printf("Validation failed with %d errors:", len(errors))
        for _, e := range errors[:10] {  // показати перші 10
            log.Printf("  - %s", e)
        }
        return fmt.Errorf("validation failed: %d errors", len(errors))
    }
    
    log.Printf("Validation passed: %d packets, duration=%v", 
        packetCount, lastPTS)
    return nil
}
```

---

## ⚡ Оптимізації для великих файлів

### 1. Буферизоване читання:

```go
// Використання bufio для зменшення системних викликів
import "bufio"

f, _ := os.Open(filename)
defer f.Close()

// Буфер 1MB для ефективного читання
buffered := bufio.NewReaderSize(f, 1024*1024)
demuxer := ts.NewDemuxer(buffered)
```

### 2. Прогрес-бар для довгих файлів:

```go
// Прогрес при обробці великих файлів
func ReadWithProgress(filename string) error {
    f, err := os.Open(filename)
    if err != nil { return err }
    defer f.Close()
    
    // Отримання розміру файлу
    info, err := f.Stat()
    if err != nil { return err }
    fileSize := info.Size()
    
    demuxer := ts.NewDemuxer(f)
    
    var processed int64
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    go func() {
        for range ticker.C {
            progress := float64(processed) / float64(fileSize) * 100
            log.Printf("Progress: %.1f%% (%d/%d bytes)", 
                progress, processed, fileSize)
        }
    }()
    
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF { break }
        if err != nil { return err }
        
        processed += int64(len(pkt.Data))
        // ... обробка ...
    }
    
    return nil
}
```

### 3. Паралельна обробка пакетів:

```go
// ParallelProcess — обробка пакетів у кількох горутинах
func ParallelProcess(demuxer *ts.Demuxer, workerCount int) error {
    packetChan := make(chan av.Packet, workerCount*10)
    errChan := make(chan error, workerCount)
    
    // Воркери для обробки
    for w := 0; w < workerCount; w++ {
        go func(id int) {
            for pkt := range packetChan {
                if err := processPacket(id, pkt); err != nil {
                    errChan <- err
                    return
                }
            }
        }(w)
    }
    
    // Читання пакетів у головній горутині
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF { break }
        if err != nil { return err }
        
        packetChan <- pkt
    }
    close(packetChan)
    
    // Чекаємо завершення воркерів
    for w := 0; w < workerCount; w++ {
        if err := <-errChan; err != nil {
            return err
        }
    }
    
    return nil
}
```

---

## 📋 Чек-лист безпечного читання TS файлів

```go
// ✅ 1. Відкриття файлу з обробкою помилок
f, err := os.Open(filename)
if err != nil {
    return fmt.Errorf("open: %w", err)
}
defer f.Close()

// ✅ 2. Створення демуксера
demuxer := ts.NewDemuxer(f)

// ✅ 3. Отримання метаданих потоків
streams, err := demuxer.Streams()
if err != nil { /* handle */ }

// ✅ 4. Безпечний цикл читання
for {
    pkt, err := demuxer.ReadPacket()
    if err == io.EOF {
        break  // нормальне завершення
    }
    if err != nil {
        log.Printf("read error: %v", err)
        break  // або return, залежно від вимог
    }
    
    // ✅ 5. Безпечний доступ до даних
    var snippet []byte
    if len(pkt.Data) >= 10 {
        snippet = pkt.Data[4:10]
    }
    
    // ✅ 6. Логування з контекстом
    log.Printf("Pkt[%d]: idx=%d, time=%v, key=%v, size=%d, data=%x",
        packetNum, pkt.Idx, pkt.Time, pkt.IsKeyFrame, len(pkt.Data), snippet)
    
    packetNum++
}

// ✅ 7. Фінальне логування
log.Printf("Processed %d packets", packetNum)
```

---

## 🔗 Корисні посилання

- 💻 [vdk ts Package](https://pkg.go.dev/github.com/deepch/vdk/format/ts) — GoDoc documentation
- 📄 [MPEG-TS Specification (ISO/IEC 13818-1)](https://www.iso.org/standard/61246.html) — офіційний стандарт
- 📄 [H.264 NALU Structure](https://wiki.multimedia.cx/index.php/H.264) — для розуміння p.Data[4:10]
- 🧪 [Go os.File Documentation](https://pkg.go.dev/os#File) — робота з файлами у Go

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV записами**:
> 1. **Завжди обробляйте помилки** — ігнорування `err` може призвести до складних для дебагу проблем.
> 2. **Використовуйте `defer f.Close()`** — уникнення витоку файлових дескрипторів при обробці багатьох файлів.
> 3. **Перевіряйте `len(p.Data)` перед доступом до слайсу** — уникнення панік при коротких пакетах.
> 4. **Додайте ліміт пакетів для дебагу** — щоб не "зависнути" на великих файлах під час розробки.
> 5. **Логувайте контекст** (номер пакету, тип, час) — це значно спрощує аналіз логів.

Потрібен приклад інтеграції цього TS-рідера з вашим `pubsub.Queue` для розподілу прочитаних пакетів між підписниками (декодер, аналізатор, архів)? Готовий допомогти! 🚀