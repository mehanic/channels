# 📦 Глибокий розбір: `nvr.Muxer` — Гнучкий муксер для запису відео у MP4/NVR формати

Цей файл — **повноцінна реалізація муксера для системи відеоспостереження (NVR)**, що підтримує запис у два формати: стандартний MP4 (для сумісності) та власний NVR (для ефективного зберігання GOP-ів). Він надає гнучке шаблонування імен файлів, автоматичне перемикання між дисками, та обробку подій зміни файлів.

---

## 🗺️ Архітектурна схема nvr.Muxer

```
┌────────────────────────────────────────┐
│ 📦 nvr.Muxer — NVR Recording Engine   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Muxer — основна структура           │
│  • Gof/Data — серіалізовані дані для NVR│
│  • WriteHeader/WritePacket — запис    │
│  • filePatch() — шаблонування шляхів   │
│  • handleFileChange — callback подій   │
│                                         │
│  🔄 Потоки запису:                      │
│  • MP4: av.Packet → mp4.Muxer → .mp4  │
│  • NVR: GOP → gob encode → .d/.m files│
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (через mp4.Muxer)     │
│  • Аудіо: AAC (через mp4.Muxer)       │
│  • Формати: MP4 (стандарт), NVR (власний)│
│  • Шаблони: 27 тегів для іменування    │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Muxer — основна структура

### Поля та їх призначення:

```go
type Muxer struct {
    // 🎬 Муксинг
    muxer  *mp4.Muxer  // внутрішній MP4 муксер (тільки для format=MP4)
    format string      // "mp4" або "nvr"
    limit  int         // ліміт тривалості файлу у секундах (для MP4)
    
    // 💾 Файлова система
    d, m   *os.File    // .d = дані, .m = метадані (для NVR)
    patch  string      // шаблон шляху з тегами
    mpoint []string    // список точок монтування для балансування
    
    // ⏱️ Таймінги
    start, end  time.Time      // час початку/кінця запису
    pstart, pend time.Duration // PTS початку/кінцю
    dur         time.Duration  // тривалість поточного файлу
    h           int            // поточна година (для ротації по годинах у NVR)
    
    // 🎞️ Стан GOP (тільки для NVR)
    gof      *Gof      // буфер для поточного GOP
    started  bool      // чи почався запис (чекаємо перший ключовий кадр)
    
    // 🏷️ Метадані для шаблонування
    serverID, streamName, channelName, streamID, channelID string
    hostLong, hostShort string
    
    // 🔔 Callback для подій
    handleFileChange func(bool, string, string, int64, time.Time, time.Time, time.Duration)
}
```

### 🔧 NewMuxer() — ініціалізація:

```go
func NewMuxer(serverID, streamName, channelName, streamID, channelID string, 
             mpoint []string, patch, format string, limit int, 
             c func(bool, string, string, int64, time.Time, time.Time, time.Duration)) (*Muxer, error) {
    
    hostLong, _ := os.Hostname()
    var hostShort string
    if p, _, ok := strings.Cut(hostLong, "."); ok {
        hostShort = p  // скорочене ім'я хоста (без домену)
    }
    
    return &Muxer{
        mpoint:           mpoint,
        patch:            patch,
        h:                -1,  // ініціалізація для першої ротації
        gof:              &Gof{},
        format:           format,
        limit:            limit,
        serverID:         serverID,
        streamName:       streamName,
        channelName:      channelName,
        streamID:         streamID,
        channelID:        channelID,
        hostLong:         hostLong,
        hostShort:        hostShort,
        handleFileChange: c,
    }, nil
}
```

---

## 🔑 2. Два режими запису: MP4 vs NVR

### 🎬 MP4 режим (стандартний):

```
Потік даних:
  av.Packet → mp4.Muxer → .mp4 файл

Переваги:
• Сумісність з будь-яким плеєром
• Стандартний контейнер з метаданими

Недоліки:
• Більший overhead на заголовки
• Менш ефективний для пошуку по часу

Логіка ротації:
  Якщо ключовий кадр && dur > limit секунд → новий файл
```

### 🔧 writePacketMP4():

```go
func (m *Muxer) writePacketMP4(pkt av.Packet) (err error) {
    // Ротація на ключовому кадрі після досягнення ліміту
    if pkt.IsKeyFrame && m.dur > time.Duration(m.limit)*time.Second {
        m.pstart = pkt.Time  // збереження початкового PTS нового файлу
        if err = m.OpenMP4(); err != nil { return }
        m.dur = 0  // скидання лічильника
    }
    
    m.dur += pkt.Duration  // накопичення тривалості
    m.pend = pkt.Time      // оновлення кінцевого PTS
    
    return m.muxer.WritePacket(pkt)  // делегування mp4.Muxer
}
```

### 🗃️ NVR режим (власний формат):

```
Потік даних:
  av.Packet → буферизація GOP → gob encode → .d файл (дані)
                                           → .m файл (індекс)

Структура файлів:
  /patch/YYYY/MM/DD/
    ├── HH.d  — дані (серіалізовані GOP через gob)
    └── HH.m  — індекс (offset + duration + MIME signature)

Переваги:
• Ефективне зберігання (немає контейнерного overhead)
• Швидкий пошук по часу через індекс .m
• Легке видалення старих даних (по годинах)

Недоліки:
• Власний формат, потрібен спеціальний читач
• Менша сумісність із зовнішніми інструментами
```

### 🔧 writePacketNVR() — буферизація GOP:

```go
func (m *Muxer) writePacketNVR(pkt av.Packet) (err error) {
    // Новий GOP починається з ключового кадру
    if pkt.IsKeyFrame {
        if len(m.gof.Packet) > 0 {
            // Запис попереднього GOP
            if err = m.writeGop(); err != nil { return }
        }
        // Скидання буфера для нового GOP
        m.gof.Packet, m.dur = nil, 0
    }
    
    // Накопичення тривалості тільки для відео (Idx==0)
    if pkt.Idx == 0 {
        m.dur += pkt.Duration
    }
    
    // Додавання пакету у буфер поточного GOP
    m.gof.Packet = append(m.gof.Packet, pkt)
    return
}
```

### 🔧 writeGop() — серіалізація та запис:

```go
func (m *Muxer) writeGop() (err error) {
    t := time.Now().UTC()
    
    // Ротація файлів по годинах
    if m.h != t.Hour() {
        if err = m.OpenNVR(); err != nil { return }
    }
    
    // Метадані для індексу
    f := Data{
        Time: t.UnixNano(),  // час запису
        Dur:  m.dur.Milliseconds(),  // тривалість GOP
    }
    
    // Запис даних у .d файл
    if f.Start, err = m.d.Seek(0, 2); err != nil { return }  // offset у кінці
    enc := gob.NewEncoder(m.d)
    if err = enc.Encode(m.gof); err != nil { return }  // серіалізація GOP
    
    // Запис індексу у .m файл
    buf := bytes.NewBuffer([]byte{})
    if err = binary.Write(buf, binary.LittleEndian, f); err != nil { return }
    if _, err = buf.Write(MIME); err != nil { return }  // сигнатура для валідації
    _, err = m.m.Write(buf.Bytes())
    
    return
}
```

### 🔍 Чому два файли (.d та .m)?

```
.d файл (дані):
• Містить серіалізовані GOP через gob
• Послідовний запис, ефективний для великих обсягів

.m файл (метадані/індекс):
• Містить: [offset (int64)][duration (int64)][MIME signature (8 bytes)]
• Дозволяє швидкий пошук: "знайти GOP для часу T" без читання всіх даних
• MIME сигнатура (11,22,111,222,11,22,111,222) для валідації цілісності

Приклад пошуку:
1. Читання .m файлу → масив записів {offset, dur, time}
2. Бінарний пошук за часом → знаходимо потрібний offset
3. Seek у .d файлі → декодування одного GOP через gob
```

---

## 🔑 3. filePatch() — гнучке шаблонування шляхів

### 🔧 27 підтримуваних тегів:

```go
var listTag = []string{
    // 🏷️ Ідентифікатори
    "{server_id}", "{stream_name}", "{channel_name}", 
    "{stream_id}", "{channel_id}",
    
    // 🖥️ Хост
    "{host_name}", "{host_name_short}", "{host_name_long}",
    
    // ⏰ Час початку (2006-01-02 15:04:05)
    "{start_year}", "{start_month}", "{start_day}", 
    "{start_hour}", "{start_minute}", "{start_second}",
    "{start_millisecond}", "{start_unix_second}", "{start_unix_millisecond}",
    "{start_time}", "{start_pts}",
    
    // ⏰ Час кінця (аналогічно)
    "{end_year}", ..., "{end_pts}",
    
    // ⏱️ Тривалість
    "{duration_second}", "{duration_millisecond}",
}
```

### 🔧 Приклад шаблону:

```
Вхідний patch:
  "/recordings/{server_id}/{stream_name}/{start_year}/{start_month}/{start_day}/{start_hour}.mp4"

Підстановка:
  "/recordings/server123/camera_front/2024/01/15/14.mp4"

Для NVR формату:
  "/nvr/{channel_id}/{start_year}/{start_month}/{start_day}/"
  → "/nvr/cam01/2024/01/15/"
  → файли: 14.d, 14.m (для години 14)
```

### ⚠️ Критична проблема: порядок підстановки

```
У вихідному коді:
  for _, s := range listTag {
      switch s {
      case "{server_id}":
          ts = strings.Replace(ts, "{server_id}", m.serverID, -1)
      // ... інші теги ...
      }
  }
```

**Проблема**: Якщо шаблон містить вкладені теги (напр. "{start_time}_{server_id}"), порядок обробки може призвести до некоректної підстановки.

**✅ Виправлення**: Сортувати теги за довжиною (спочатку довші) або використовувати регулярні вирази:

```go
// Безпечніша підстановка з перевіркою
func (m *Muxer) replaceTag(ts, tag, value string) string {
    // Перевірка чи тег дійсно присутній
    if !strings.Contains(ts, tag) {
        return ts
    }
    return strings.Replace(ts, tag, value, -1)
}

// Використання у циклі:
for _, tag := range listTag {
    switch tag {
    case "{server_id}":
        ts = m.replaceTag(ts, tag, m.serverID)
    // ...
    }
}
```

---

## 🔑 4. Балансування дисків: вибір точки монтування

### 🔧 Логіка вибору найменш завантаженого диска:

```go
func (m *Muxer) filePatch() (string, error) {
    var (
        mu = float64(100)  // початкове значення: 100% завантаження
        ui = -1            // індекс вибраної точки
    )
    
    for i, mountPoint := range m.mpoint {
        if d, err := disk.Usage(mountPoint); err == nil {
            // Вибір точки з найменшим % використання
            if d.UsedPercent < mu {
                ui = i
                mu = d.UsedPercent
            }
        }
    }
    
    if ui == -1 {
        return "", errors.New("not mount ready")  // жодна точка не доступна
    }
    
    // Побудова шляху: точка монтування + шаблон
    ts := filepath.Join(m.mpoint[ui], m.patch)
    // ... підстановка тегів ...
    return ts, nil
}
```

### ✅ Ваш use-case: налаштування точок монтування

```go
// Приклад ініціалізації з кількома дисками
mpoint := []string{
    "/mnt/disk1",  // 500GB SSD
    "/mnt/disk2",  // 2TB HDD
    "/mnt/disk3",  // 4TB NAS
}

muxer, err := nvr.NewMuxer(
    "server01", "main_stream", "lobby_cam", 
    "stream_123", "channel_456",
    mpoint,  // балансуються автоматично
    "/recordings/{channel_name}/{start_year}/{start_month}/{start_day}.mp4",
    nvr.MP4,  // або nvr.NVR
    3600,     // 1 година на файл
    onFileChange,  // callback
)
```

---

## 🔑 5. handleFileChange — callback для подій

### 🔧 Сигнатура та параметри:

```go
handleFileChange func(
    isNew bool,              // true = початок запису, false = завершення
    codecs string,           // "h264,aac" — список кодеків
    path string,             // повний шлях до файлу
    size int64,              // розмір файлу у байтах (0 для початку)
    start, end time.Time,    // час початку/кінця запису
    duration time.Duration,  // тривалість у секундах
)
```

### ✅ Ваш use-case: інтеграція з системою сповіщень

```go
// onFileChange — обробка подій запису
func onFileChange(isNew bool, codecs, path string, size int64, 
                  start, end time.Time, duration time.Duration) {
    
    if isNew {
        log.Printf("🎬 Start recording: %s (%s)", path, codecs)
        // Можна відправити webhook про початок запису
    } else {
        log.Printf("✅ Finished: %s (%.1f MB, %v)", 
            path, float64(size)/1024/1024, duration)
        
        // Інтеграція з хмарним архівом
        go uploadToCloud(path, start, end)
        
        // Оновлення бази даних
        go db.RecordSegment(path, start, end, duration, size, codecs)
    }
}

// uploadToCloud — асинхронне завантаження
func uploadToCloud(path string, start, end time.Time) {
    // Перевірка політик зберігання
    if shouldArchive(start, end) {
        if err := cloud.Upload(path, "nvr-archive"); err != nil {
            log.Printf("❌ Upload failed: %v", err)
        } else {
            // Видалення локальної копії після успішного завантаження
            os.Remove(path)
        }
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Шлях не створюється** | `os.MkdirAll` падає через відсутні права | Перевірте права на `patch` та `mpoint`; використовуйте абсолютні шляхи |
| **Теги не підставляються** | У імені файлу залишаються `{tag}` | Перевірте регістр тегів; додайте логування перед/після заміни |
| **Диск переповнюється** | Балансування не працює | Переконайтеся, що `disk.Usage()` повертає дані; перевірте права читання `/proc/mounts` |
| **GOP не записується** | .d файл порожній у NVR режимі | Перевірте чи `pkt.IsKeyFrame` коректно встановлено; додайте логування у `writeGop()` |
| **Callback не викликається** | Події не обробляються | Переконайтеся, що `handleFileChange` не `nil`; перевірте чи `WriteTrailer()` викликається |

---

## ⚡ Оптимізації для high-throughput запису

### 1. Буферизований запис для NVR:

```go
// OpenNVRBuffered — створення з буферизацією
func (m *Muxer) OpenNVRBuffered(bufferSize int) error {
    if err := m.OpenNVR(); err != nil {
        return err
    }
    // Обгортка файлів у буферизовані writers
    m.d = bufio.NewWriterSize(m.d, bufferSize)
    m.m = bufio.NewWriterSize(m.m, bufferSize)
    return nil
}

// У writeGop():
if bw, ok := m.d.(*bufio.Writer); ok {
    if err := bw.Flush(); err != nil { return err }
}
```

### 2. Асинхронна серіалізація GOP:

```go
// writeGopAsync — запис у фоновій горутині
func (m *Muxer) writeGopAsync() error {
    // Копіювання даних для безпечної передачі у горутину
    gofCopy := &Gof{
        Streams: m.gof.Streams,  // shallow copy, але CodecData зазвичай immutable
        Packet:  make([]av.Packet, len(m.gof.Packet)),
    }
    copy(gofCopy.Packet, m.gof.Packet)
    
    go func() {
        if err := m.writeGopSync(gofCopy); err != nil {
            log.Printf("async write error: %v", err)
        }
    }()
    return nil
}
```

### 3. Моніторинг продуктивності запису:

```go
type NVRMetrics struct {
    FilesCreated   prometheus.CounterVec
    BytesWritten   prometheus.CounterVec
    WriteLatency   prometheus.HistogramVec
    DiskUsage      prometheus.GaugeVec
    GOPBufferDepth prometheus.GaugeVec
}

func (m *NVRMetrics) RecordWrite(format string, size int, duration time.Duration, mountPoint string) {
    m.BytesWritten.WithLabelValues(format, mountPoint).Add(float64(size))
    m.WriteLatency.WithLabelValues(format).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист безпечного використання

```go
// ✅ 1. Валідація вхідних параметрів
if patch == "" || len(mpoint) == 0 {
    return fmt.Errorf("patch and mpoint are required")
}

// ✅ 2. Перевірка прав на запис
for _, mp := range mpoint {
    if err := syscall.Access(mp, syscall.O_WRONLY); err != nil {
        log.Printf("warning: no write access to %s", mp)
    }
}

// ✅ 3. Обробка помилок у callback
func safeFileChange(isNew bool, /* ... */) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("handleFileChange panic: %v", r)
        }
    }()
    // ... ваш код ...
}

// ✅ 4. Гарантоване закриття файлів
defer func() {
    if m.d != nil { m.d.Close() }
    if m.m != nil { m.m.Close() }
}()

// ✅ 5. Логування для дебагу
if debug {
    log.Printf("NVR: writing GOP at %v, dur=%v, packets=%d", 
        time.Now(), m.dur, len(m.gof.Packet))
}

// ✅ 6. Метрики для моніторингу
metrics.RecordWrite(m.format, len(pkt.Data), time.Since(start), chosenMountPoint)
```

---

## 🔗 Корисні посилання

- 💻 [vdk mp4 Package](https://pkg.go.dev/github.com/deepch/vdk/format/mp4) — MP4 муксер
- 📄 [Gob Encoding](https://pkg.go.dev/encoding/gob) — серіалізація для NVR формату
- 📄 [shirou/gopsutil Disk Usage](https://pkg.go.dev/github.com/shirou/gopsutil/v3/disk) — моніторинг дисків
- 🧪 [Go filepath Package](https://pkg.go.dev/path/filepath) — безпечна робота з шляхами
- 📦 [Prometheus Metrics](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — моніторинг продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди використовуйте `filepath.Join()`** — уникнення помилок шляхів на різних ОС.
> 2. **Валідуйте `disk.Usage()` помилки** — якщо моніторинг дисків не працює, запис може зупинитися.
> 3. **Тестуйте шаблони з усіма тегами** — деякі теги (напр. `{start_pts}`) можуть бути нульовими на початку.
> 4. **Обробляйте `handleFileChange` асинхронно** — щоб не блокувати основний потік запису.
> 5. **Моніторьте `GOPBufferDepth`** — різке зростання може вказувати на відсутність ключових кадрів.

Потрібен приклад реалізації читача для власного NVR формату (.d/.m файли) для відтворення записів? Готовий допомогти! 🚀