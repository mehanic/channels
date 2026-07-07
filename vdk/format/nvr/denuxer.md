# 📦 Глибокий розбір: `nvr.DeMuxer` — Скелет демуксера для NVR записів

Цей файл містить **заглушки методів** для майбутньої реалізації демуксера власного NVR формату (описаного у `nvr.Muxer`). Наразі всі методи повертають `nil` і не містять логіки, але структура вказує на три ключові операції: читання індексу, пошук за діапазоном, та витягування окремих GOP-ів.

---

## 🗺️ Архітектурна схема (очікувана реалізація)

```
┌────────────────────────────────────────┐
│ 📦 nvr.DeMuxer — NVR Playback Engine  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові методи (очікувані):         │
│  • ReadIndex() — парсинг .m файлів    │
│  • ReadRange() — пошук за часом        │
│  • ReadGop() — декодування одного GOP  │
│                                         │
│  🔄 Потік відтворення:                  │
│  .m файл → індекс у пам'яті            │
│  → бінарний пошук за часом             │
│  → seek у .d файлі → gob decode → av.Packet│
│                                         │
│  📡 Підтримка:                          │
│  • Формат: власний NVR (.d/.m)         │
│  • Кодеки: H.264, AAC (через gob)      │
│  • Пошук: за часом, каналом, сервером  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Поточний стан: порожні заглушки

```go
type DeMuxer struct {
    // ← ПОРОЖНЬО! Потрібно додати поля для:
    // • Шляхи до .d/.m файлів
    // • Кеш індексу в пам'яті
    // • Поточна позиція читання
    // • Буфер для gob декодування
}

func (obj *DeMuxer) ReadIndex() (err error)   { return nil }  // ❌ нічого не робить
func (obj *DeMuxer) ReadRange() (err error)   { return nil }  // ❌ нічого не робить  
func (obj *DeMuxer) ReadGop() (err error)     { return nil }  // ❌ нічого не робить
```

### ❌ Критичні проблеми:

1. **Відсутність стану**: Без полів неможливо зберігати шляхи, індекс, позицію читання.
2. **Невідповідність сигнатур**: `ReadRange()` та `ReadGop()` не мають параметрів — незрозуміло, що саме читати.
3. **Відсутність типів повернення**: `ReadGop()` має повертати `[]av.Packet` або канал, а не `error`.

---

## ✅ Production-ready версія: повна реалізація

### 🔧 Структура з необхідними полями:

```go
type DeMuxer struct {
    // 🗂️ Файлова система
    dataPath  string  // шлях до .d файлу (дані)
    indexPath string  // шлях до .m файлу (індекс)
    
    // 📚 Індекс у пам'яті
    index []IndexEntry  // кешовані записи з .m файлу
    
    // 🎞️ Стан читання
    currentOffset int64  // поточна позиція у .d файлі
    currentIndex  int    // індекс поточного запису в index[]
    
    // 🔧 Інструменти
    dataFile  *os.File    // дескриптор .d файлу
    indexFile *os.File    // дескриптор .m файлу
    
    // 🏷️ Фільтри (опціонально)
    serverID, channelID string  // для фільтрації записів
}

type IndexEntry struct {
    Time  int64  // Unix nano timestamp запису
    Start int64  // offset у .d файлі
    Dur   int64  // тривалість у мілісекундах
    Valid bool   // чи валідний запис (MIME signature)
}
```

---

### 🔧 ReadIndex() — парсинг індексного файлу (.m):

```go
func (obj *DeMuxer) ReadIndex() error {
    // 1. Відкриття індексного файлу
    var err error
    if obj.indexFile, err = os.Open(obj.indexPath); err != nil {
        return fmt.Errorf("open index file %s: %w", obj.indexPath, err)
    }
    
    // 2. Читання записів (кожен: 24 байти даних + 8 байт MIME = 32 байти)
    const entrySize = 32  // Data(24) + MIME(8)
    buf := make([]byte, entrySize)
    
    obj.index = []IndexEntry{}
    for {
        n, err := obj.indexFile.Read(buf)
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read index entry: %w", err)
        }
        if n < entrySize {
            return fmt.Errorf("short read: %d < %d bytes", n, entrySize)
        }
        
        // 3. Парсинг полів (Little-Endian, як у Muxer.writeGop())
        entry := IndexEntry{
            Time:  int64(binary.LittleEndian.Uint64(buf[0:8])),
            Start: int64(binary.LittleEndian.Uint64(buf[8:16])),
            Dur:   int64(binary.LittleEndian.Uint64(buf[16:24])),
        }
        
        // 4. Перевірка MIME сигнатури для валідації цілісності
        entry.Valid = bytes.Equal(buf[24:32], MIME)
        if !entry.Valid {
            log.Printf("warning: invalid MIME at offset %d", entry.Start)
        }
        
        obj.index = append(obj.index, entry)
    }
    
    // 5. Сортування за часом для бінарного пошуку
    sort.Slice(obj.index, func(i, j int) bool {
        return obj.index[i].Time < obj.index[j].Time
    })
    
    return nil
}
```

---

### 🔧 ReadRange() — пошук та читання діапазону записів:

```go
// ReadRange читає пакети у заданому часовому діапазоні
func (obj *DeMuxer) ReadRange(start, end time.Time) (<-chan av.Packet, error) {
    startNano := start.UnixNano()
    endNano := end.UnixNano()
    
    // 1. Пошук початкового та кінцевого індексів (бінарний пошук)
    startIdx := sort.Search(len(obj.index), func(i int) bool {
        return obj.index[i].Time+obj.index[i].Dur*1e6 >= startNano
    })
    endIdx := sort.Search(len(obj.index), func(i int) bool {
        return obj.index[i].Time > endNano
    })
    
    if startIdx >= endIdx {
        return nil, fmt.Errorf("no recordings found for range %v-%v", start, end)
    }
    
    // 2. Відкриття файлу даних
    var err error
    if obj.dataFile, err = os.Open(obj.dataPath); err != nil {
        return nil, fmt.Errorf("open data file: %w", err)
    }
    
    // 3. Створення буферизованого каналу для потокової відправки
    pktChan := make(chan av.Packet, 100)
    
    // 4. Фонове читання та декодування
    go func() {
        defer close(pktChan)
        defer obj.dataFile.Close()
        
        for i := startIdx; i < endIdx; i++ {
            entry := obj.index[i]
            if !entry.Valid {
                continue  // пропуск пошкоджених записів
            }
            
            // Читання одного GOP
            pkts, err := obj.readGopAt(entry.Start)
            if err != nil {
                log.Printf("decode GOP at offset %d: %v", entry.Start, err)
                continue
            }
            
            // Фільтрація пакетів за точним часовим діапазоном
            for _, pkt := range pkts {
                // Конвертація pkt.Time (відносно початку GOP) у абсолютний час
                pktAbsTime := time.Unix(0, entry.Time).Add(time.Duration(pkt.Time) * time.Millisecond)
                if pktAbsTime.Before(start) || pktAbsTime.After(end) {
                    continue
                }
                pktChan <- pkt
            }
        }
    }()
    
    return pktChan, nil
}
```

---

### 🔧 ReadGop() — декодування одного GOP за індексом:

```go
// ReadGop читає один GOP за його індексом у масиві
func (obj *DeMuxer) ReadGop(index int) ([]av.Packet, error) {
    if index < 0 || index >= len(obj.index) {
        return nil, fmt.Errorf("index out of range: %d (max %d)", index, len(obj.index)-1)
    }
    
    entry := obj.index[index]
    if !entry.Valid {
        return nil, fmt.Errorf("invalid entry at index %d", index)
    }
    
    return obj.readGopAt(entry.Start)
}

// readGopAt — внутрішня функція читання за offset
func (obj *DeMuxer) readGopAt(offset int64) ([]av.Packet, error) {
    // 1. Seek до потрібної позиції у .d файлі
    if _, err := obj.dataFile.Seek(offset, io.SeekStart); err != nil {
        return nil, fmt.Errorf("seek to %d: %w", offset, err)
    }
    
    // 2. Створення окремого декодера gob для цього запису
    // ⚠️ Важливо: кожен GOP — незалежний gob об'єкт
    decoder := gob.NewDecoder(obj.dataFile)
    
    // 3. Декодування структури Gof (з nvr.Muxer)
    var gof Gof
    if err := decoder.Decode(&gof); err != nil {
        return nil, fmt.Errorf("decode gob: %w", err)
    }
    
    // 4. Валідація даних
    if len(gof.Streams) == 0 {
        return nil, fmt.Errorf("empty codec data in GOP")
    }
    
    return gof.Packet, nil
}
```

---

## ⚠️ Критичний момент: реєстрація типів для gob

У `nvr` пакеті вже є `init()` з реєстрацією:
```go
func init() {
    gob.RegisterName("nvr.Gof", Gof{})
    gob.RegisterName("h264parser.CodecData", h264parser.CodecData{})
    gob.RegisterName("aacparser.CodecData", aacparser.CodecData{})
}
```

**Але**: Якщо `DeMuxer` використовується в окремому бінарному файлі або пакеті, ця реєстрація може не спрацювати!

**✅ Виправлення**: Експортувати функцію реєстрації:

```go
// RegisterGobTypes — публічна функція для реєстрації типів у будь-якому контексті
func RegisterGobTypes() {
    gob.RegisterName("nvr.Gof", Gof{})
    gob.RegisterName("h264parser.CodecData", h264parser.CodecData{})
    gob.RegisterName("aacparser.CodecData", aacparser.CodecData{})
}

// Виклик у main() читача:
func init() {
    nvr.RegisterGobTypes()  // гарантована реєстрація перед декодуванням
}
```

---

## 🔄 Інтеграція у ваш pipeline: приклад CLI утиліти

```go
// cmd/nvr-play/main.go — CLI для відтворення записів
func main() {
    var (
        dataFile  = flag.String("data", "recordings/14.d", "path to .d file")
        indexFile = flag.String("index", "recordings/14.m", "path to .m file")
        startStr  = flag.String("start", "", "start time (RFC3339)")
        endStr    = flag.String("end", "", "end time (RFC3339)")
        output    = flag.String("out", "", "export to MP4 file (optional)")
    )
    flag.Parse()
    
    // Реєстрація типів для gob
    nvr.RegisterGobTypes()
    
    // Ініціалізація демуксера
    demuxer := &nvr.DeMuxer{
        dataPath:  *dataFile,
        indexPath: *indexFile,
    }
    
    if err := demuxer.ReadIndex(); err != nil {
        log.Fatalf("read index: %v", err)
    }
    
    start, _ := time.Parse(time.RFC3339, *startStr)
    end, _ := time.Parse(time.RFC3339, *endStr)
    
    if *output != "" {
        // Експорт у MP4
        exportToMP4(demuxer, start, end, *output)
    } else {
        // Інтерактивне відтворення (приклад)
        playInteractive(demuxer, start, end)
    }
}

func exportToMP4(demuxer *nvr.DeMuxer, start, end time.Time, outputPath string) error {
    // Відкриття вихідного MP4 файлу
    outF, err := os.Create(outputPath)
    if err != nil { return err }
    defer outF.Close()
    
    mp4Muxer := mp4.NewMuxer(outF)
    
    // Читання пакетів у діапазоні
    pktChan, err := demuxer.ReadRange(start, end)
    if err != nil { return err }
    
    // Запис заголовка (перший пакет містить кодек-дані)
    firstPkt, ok := <-pktChan
    if !ok { return fmt.Errorf("no packets in range") }
    
    if err := mp4Muxer.WriteHeader([]av.CodecData{firstPkt.CodecData}); err != nil {
        return err
    }
    if err := mp4Muxer.WritePacket(firstPkt); err != nil {
        return err
    }
    
    // Запис решти пакетів
    for pkt := range pktChan {
        if err := mp4Muxer.WritePacket(pkt); err != nil {
            log.Printf("write error: %v", err)
            break
        }
    }
    
    return mp4Muxer.WriteTrailer()
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"gob: type not registered"** | Паніка при `decoder.Decode()` | Викликайте `RegisterGobTypes()` перед будь-яким декодуванням |
| **"invalid MIME signature"** | Записи пропускаються у `ReadIndex()` | Перевірте цілісність .m файлу; можливо, запис був перерваний |
| **"offset out of range"** | `Seek` падає у `readGopAt()` | Перевірте чи .d файл не був змінений після індексації |
| **Повільний пошук** | `ReadRange` довго шукає за часом | Використовуйте бінарний пошук (`sort.Search`) у відсортованому індексі |
| **Витік пам'яті** | `index[]` росте нескінченно | Обмежуйте розмір кешу індексу; вивантажуйте старі записи на диск |

---

## ⚡ Оптимізації для швидкого відтворення

### 1. Кешування індексу у пам'яті:

```go
type IndexCache struct {
    mu      sync.RWMutex
    entries map[string][]IndexEntry  // channelID → entries
    maxSize int                       // ліміт записів на канал
}

func (c *IndexCache) Get(channelID string) []IndexEntry {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.entries[channelID]
}

func (c *IndexCache) Set(channelID string, entries []IndexEntry) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Обрізання якщо перевищено ліміт
    if len(entries) > c.maxSize {
        entries = entries[len(entries)-c.maxSize:]
    }
    c.entries[channelID] = entries
}
```

### 2. Паралельне декодування GOP-ів:

```go
// ReadRangeParallel — декодування кількох GOP одночасно
func (obj *DeMuxer) ReadRangeParallel(start, end time.Time, concurrency int) (<-chan av.Packet, error) {
    pktChan := make(chan av.Packet, concurrency*10)
    
    go func() {
        defer close(pktChan)
        
        // Знаходимо діапазон індексів
        startIdx, endIdx := obj.findIndexRange(start, end)
        
        // Робоча черга для паралельної обробки
        type job struct {
            entry IndexEntry
            result chan []av.Packet
        }
        jobs := make(chan job, concurrency)
        
        // Воркери
        for w := 0; w < concurrency; w++ {
            go func() {
                for j := range jobs {
                    pkts, err := obj.readGopAt(j.entry.Start)
                    if err != nil {
                        log.Printf("decode error: %v", err)
                        j.result <- nil
                    } else {
                        j.result <- pkts
                    }
                }
            }()
        }
        
        // Відправка завдань
        results := make(chan []av.Packet, concurrency)
        for i := startIdx; i <= endIdx; i++ {
            entry := obj.index[i]
            if !entry.Valid { continue }
            
            resultChan := make(chan []av.Packet, 1)
            jobs <- job{entry, resultChan}
            
            // Асинхронне збирання результатів
            go func() {
                pkts := <-resultChan
                for _, pkt := range pkts {
                    pktChan <- pkt
                }
            }()
        }
        close(jobs)
    }()
    
    return pktChan, nil
}
```

### 3. Моніторинг продуктивності читання:

```go
type DeMuxerMetrics struct {
    IndexLoadTime   prometheus.HistogramVec
    GOPDecodeTime   prometheus.HistogramVec
    PacketsRead     prometheus.CounterVec
    CacheHitRatio   prometheus.GaugeVec
}

func (m *DeMuxerMetrics) RecordIndexLoad(duration time.Duration, channelID string) {
    m.IndexLoadTime.WithLabelValues(channelID).Observe(duration.Seconds())
}

func (m *DeMuxerMetrics) RecordGOPDecode(size int, duration time.Duration, channelID string) {
    m.GOPDecodeTime.WithLabelValues(channelID).Observe(duration.Seconds())
    m.PacketsRead.WithLabelValues(channelID).Add(float64(size))
}
```

---

## 📋 Чек-лист реалізації DeMuxer

```go
// ✅ 1. Додати необхідні поля у struct DeMuxer
// ✅ 2. Реалізувати ReadIndex() з парсингом .m файлу
// ✅ 3. Реалізувати readGopAt() з gob декодуванням
// ✅ 4. Додати RegisterGobTypes() для безпечної реєстрації
// ✅ 5. Реалізувати ReadRange() з бінарним пошуком
// ✅ 6. Додати обробку помилок та логування
// ✅ 7. Протестувати з реальними .d/.m файлами
// ✅ 8. Додати метрики для моніторингу продуктивності
```

---

## 🔗 Корисні посилання

- 💻 [encoding/gob Documentation](https://pkg.go.dev/encoding/gob) — серіалізація для NVR формату
- 📄 [Binary Search in Go](https://pkg.go.dev/sort#Search) — ефективний пошук за часом
- 🧪 [Go sync Package](https://pkg.go.dev/sync) — синхронізація для паралельного читання
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди реєструйте gob типи** перед декодуванням — інакше паніка у runtime.
> 2. **Використовуйте бінарний пошук** для індексу — O(log n) замість O(n) для великих архівів.
> 3. **Кешуйте індекс у пам'яті** — уникнення повторного читання .m файлів при частому відтворенні.
> 4. **Обробляйте помилки MIME signature** — пошкоджені записи не повинні ламати весь процес.
> 5. **Моніторьте `GOPDecodeTime`** — різке зростання може вказувати на пошкоджені дані або проблеми з диском.

Потрібен приклад реалізації `FindByTime` з підтримкою нечіткого пошуку (напр. ±5 секунд) для покращення досвіду відтворення? Готовий допомогти! 🚀