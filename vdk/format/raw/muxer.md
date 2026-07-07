# 📦 Глибокий розбір: `raw.Muxer` — Простий муксер для запису H.264/H.265 у Annex B формат

Цей файл — **мінімалістична реалізація муксера** для запису сирих відео-даних (H.264 або H.265) у форматі Annex B (start-code prefixed) у файл. Він призначений для простих кейсів: запис камери, відладка, або підготовка даних для подальшої обробки іншими інструментами (напр. FFmpeg).

---

## 🗺️ Архітектурна схема raw.Muxer

```
┌────────────────────────────────────────┐
│ 📦 raw.Muxer — Raw H.264/H.265 Writer │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Muxer — основна структура           │
│  • WriteHeader() — запис SPS/PPS/VPS   │
│  • WriteAvPacket() — запис NALU з 0x00000001│
│  • Close() — закриття файлу            │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet (AVCC) → Annex B (start code)│
│  → os.File (raw .h264/.h265)           │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264, H.265                 │
│  • Формат: Annex B (start-code prefixed)│
│  • Аудіо: ❌ не підтримується           │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Muxer — основна структура

### Поля та їх призначення:

```go
type Muxer struct {
    idx int8     // індекс відео-потоку (якщо кілька потоків)
    w   *os.File // файловий дескриптор для запису
}
```

### 🔧 NewMuxer() — створення з валідацією шляху:

```go
func NewMuxer(filePatch, fileName string) (*Muxer, error) {
    // 1. Перевірка/створення директорії
    if _, err := os.Stat(filePatch); os.IsNotExist(err) {
        err := os.MkdirAll(filePatch, os.ModePerm)
        if err != nil { return nil, err }
    }
    
    // 2. Створення файлу
    f2, err := os.Create(filePatch + fileName)  // ⚠️ конкатенація без "/"
    if err != nil { return nil, err }
    
    return &Muxer{w: f2}, nil
}
```

### ⚠️ Критична проблема: конкатенація шляхів

```go
f2, err := os.Create(filePatch + fileName)  // ← НЕБЕЗПЕЧНО!
```

**Проблема**: Якщо `filePatch = "/var/log"` і `fileName = "video.h264"`, результат буде `"/var/logvideo.h264"` (без роздільника).

**✅ Виправлення**: Використовувати `filepath.Join()`:

```go
import "path/filepath"

fullPath := filepath.Join(filePatch, fileName)
f2, err := os.Create(fullPath)
```

---

## 🔑 2. WriteHeader() — запис параметр-сетів (SPS/PPS/VPS)

### 🔧 Логіка для H.264:

```go
case av.H264:
    _, err = element.w.Write(append(startCode, 
        bytes.Join([][]byte{
            stream.(h264parser.CodecData).SPS(), 
            stream.(h264parser.CodecData).PPS()}, 
        startCode)...))
    element.idx = int8(i)
```

### 🔍 Формат запису:

```
Для H.264:
  [0x00000001][SPS NALU][0x00000001][PPS NALU]

Для H.265:
  [0x00000001][VPS NALU][0x00000001][SPS NALU][0x00000001][PPS NALU]

Це стандартний Annex B формат, що розуміють:
• FFmpeg (-c:v h264 -bsf:v h264_mp4toannexb)
• VLC (відкриття .h264 файлу)
• Багато апаратних декодерів
```

### ⚠️ Критичні проблеми:

#### ❌ 1. Ігнорування помилок запису

```go
_, err = element.w.Write(...)  // ← err не перевіряється!
```

**Наслідки**: Якщо запис не вдасться (диск повний, права доступу), помилка втрачається → пошкоджений файл.

**✅ Виправлення**:

```go
if _, err = element.w.Write(data); err != nil {
    return err
}
```

#### ❌ 2. Небезпечне type assertion

```go
stream.(h264parser.CodecData)  // ← паніка якщо тип не співпадає!
```

**Проблема**: Якщо `stream.Type() == av.H264`, але `stream` не є `h264parser.CodecData` (напр. фейковий тип для тестів), програма впаде з панікою.

**✅ Виправлення**:

```go
codec, ok := stream.(h264parser.CodecData)
if !ok {
    return fmt.Errorf("expected h264parser.CodecData, got %T", stream)
}
```

#### ❌ 3. Перезапис `idx` у циклі

```go
for i, stream := range streams {
    // ...
    element.idx = int8(i)  // ← останнє значення переможе!
}
```

**Проблема**: Якщо є кілька відео-потоків, `idx` буде встановлено на останній, а попередні будуть проігноровані у `WriteAvPacket()`.

**✅ Виправлення**: Зберігати `idx` тільки для першого відео-потоку:

```go
if element.idx == -1 {  // ініціалізувати -1 у конструкторі
    element.idx = int8(i)
}
```

---

## 🔑 3. WriteAvPacket() — запис відео-кадрів

### 🔧 Логіка запису:

```go
func (element *Muxer) WriteAvPacket(pkt *av.Packet) (err error) {
    if pkt.Idx == element.idx {  // фільтрація за індексом
        _, err = element.w.Write(startCode)  // префікс 0x00000001
        if err != nil { return }
        _, err = element.w.Write(pkt.Data[4:])  // ⚠️ викидання перших 4 байт!
    }
    return
}
```

### 🔍 Чому `pkt.Data[4:]`?

```
`av.Packet.Data` для H.264/H.265 зазвичай у форматі AVCC:
  [4-byte big-endian length][NALU data]

Annex B формат вимагає:
  [0x00000001 start code][NALU data]

Тому:
1. Записуємо startCode (0x00000001)
2. Пропускаємо 4-байтову довжину з pkt.Data
3. Записуємо решту (сам NALU)

Це коректно, АЛЕ тільки якщо:
• pkt.Data дійсно у форматі AVCC
• Перші 4 байти — це дійсно довжина, а не частина NALU
```

### ⚠️ Критична проблема: припущення про формат даних

**Проблема**: Якщо `pkt.Data` вже у форматі Annex B (напр. з іншого джерела), викидання перших 4 байт зламає NALU.

**✅ Виправлення**: Додати перевірку формату або параметр для вибору:

```go
// WriteAvPacketWithFormat — гнучкіша версія
func (element *Muxer) WriteAvPacketWithFormat(pkt *av.Packet, inputFormat PacketFormat) error {
    if pkt.Idx != element.idx {
        return nil
    }
    
    var data []byte
    switch inputFormat {
    case FormatAVCC:
        // Пропускаємо 4-байтову довжину
        if len(pkt.Data) < 4 {
            return fmt.Errorf("AVCC packet too short")
        }
        data = pkt.Data[4:]
    case FormatAnnexB:
        // Дані вже у правильному форматі
        data = pkt.Data
    default:
        return fmt.Errorf("unknown packet format")
    }
    
    if _, err := element.w.Write(startCode); err != nil {
        return err
    }
    _, err := element.w.Write(data)
    return err
}

type PacketFormat int

const (
    FormatAVCC PacketFormat = iota
    FormatAnnexB
)
```

---

## 🔑 4. WriteRTPPacket() — заглушка

```go
func (element *Muxer) WriteRTPPacket(pkt *[]byte) (err error) {
    return  // ← нічого не робить!
}
```

**Призначення**: Можливо, планувалася підтримка запису сирих RTP пакетів, але не реалізована.

**✅ Виправлення**: Або видалити метод, або реалізувати:

```go
// WriteRTPPacket — запис сирих RTP пакетів (якщо потрібно)
func (element *Muxer) WriteRTPPacket(pkt *[]byte) error {
    if pkt == nil || len(*pkt) == 0 {
        return nil
    }
    _, err := element.w.Write(*pkt)
    return err
}
```

---

## 🔑 5. Close() — закриття файлу

```go
func (element *Muxer) Close() error {
    return element.w.Close()
}
```

**✅ Правильно**: Завжди закривати файл для флешу буферів та звільнення дескриптора.

**💡 Покращення**: Додати `Sync()` для гарантованого запису на диск:

```go
func (element *Muxer) Close() error {
    if err := element.w.Sync(); err != nil {  // флеш на диск
        element.w.Close()  // все одно закрити
        return err
    }
    return element.w.Close()
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Запис камери у raw H.264 файл

```go
// RecordCameraToRaw — запис RTSP потоку у raw файл
func RecordCameraToRaw(rtspURL, outputPath, fileName string, duration time.Duration) error {
    // 1. Підключення до RTSP
    client, err := rtsp.Dial(rtspURL)
    if err != nil {
        return fmt.Errorf("dial RTSP: %w", err)
    }
    defer client.Close()
    
    // 2. Отримання метаданих
    streams, err := client.Streams()
    if err != nil {
        return fmt.Errorf("get streams: %w", err)
    }
    
    // 3. Створення raw муксера
    muxer, err := raw.NewMuxer(outputPath, fileName)
    if err != nil {
        return fmt.Errorf("create muxer: %w", err)
    }
    defer muxer.Close()
    
    // 4. Запис заголовка (SPS/PPS)
    if err := muxer.WriteHeader(streams); err != nil {
        return fmt.Errorf("write header: %w", err)
    }
    
    // 5. Основний цикл запису
    deadline := time.Now().Add(duration)
    for time.Now().Before(deadline) {
        pkt, err := client.ReadPacket()
        if err == io.EOF { break }
        if err != nil {
            log.Printf("read error: %v", err)
            continue
        }
        
        // Запис тільки відео пакетів
        if pkt.IsKeyFrame {
            log.Printf("key frame at %v", pkt.Time)
        }
        
        if err := muxer.WriteAvPacket(&pkt); err != nil {
            log.Printf("write error: %v", err)
            break
        }
    }
    
    return nil
}
```

### 🔧 Приклад: Конвертація MP4 → raw H.264

```go
// ConvertMP4ToRaw — витягування H.264 з MP4 у raw файл
func ConvertMP4ToRaw(inputMP4, outputRaw string) error {
    // 1. Відкриття MP4
    demuxer, err := avutil.Open(inputMP4)
    if err != nil {
        return err
    }
    defer demuxer.Close()
    
    // 2. Створення raw муксера
    dir, file := filepath.Split(outputRaw)
    muxer, err := raw.NewMuxer(dir, file)
    if err != nil {
        return err
    }
    defer muxer.Close()
    
    // 3. Запис заголовка
    streams, _ := demuxer.Streams()
    if err := muxer.WriteHeader(streams); err != nil {
        return err
    }
    
    // 4. Копіювання пакетів
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF { break }
        if err != nil { return err }
        
        // Тільки відео, тільки H.264
        if pkt.Idx == 0 {  // припускаємо, що відео = перший потік
            if err := muxer.WriteAvPacket(&pkt); err != nil {
                return err
            }
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"invalid argument" при створенні файлу** | Шлях без роздільника: `/var/logvideo.h264` | Використовувати `filepath.Join(filePatch, fileName)` |
| **Паніка при type assertion** | `stream.(h264parser.CodecData)` не співпадає | Використовувати `codec, ok := stream.(Type); if !ok { return error }` |
| **Пошкоджені NALU у вихідному файлі** | `pkt.Data[4:]` викидає важливі байти | Перевіряти формат вхідних даних або додати параметр формату |
| **Записуються не ті пакети** | `idx` встановлено на останній потік у циклі | Ініціалізувати `idx = -1` і встановлювати тільки один раз |
| **Дані не записуються на диск** | Файл порожній після збою | Додати `w.Sync()` у `Close()` для гарантованого флешу |

---

## ⚡ Оптимізації для великих файлів

### 1. Буферизований запис:

```go
// NewMuxerBuffered — створення з буферизацією
func NewMuxerBuffered(filePatch, fileName string, bufferSize int) (*Muxer, error) {
    m, err := NewMuxer(filePatch, fileName)
    if err != nil {
        return nil, err
    }
    // Обгортка файлу у буферизований writer
    m.w = bufio.NewWriterSize(m.w, bufferSize)
    return m, nil
}

// У Close():
func (element *Muxer) Close() error {
    // Якщо використовується bufio.Writer
    if bw, ok := element.w.(*bufio.Writer); ok {
        if err := bw.Flush(); err != nil {
            return err
        }
        // Отримати базовий *os.File для Sync/Close
        if f, ok := bw.Writer.(*os.File); ok {
            if err := f.Sync(); err != nil {
                f.Close()
                return err
            }
            return f.Close()
        }
    }
    return element.w.Close()
}
```

### 2. Періодичний флеш для критичних даних:

```go
// WriteAvPacketWithFlush — флеш після ключових кадрів
func (element *Muxer) WriteAvPacketWithFlush(pkt *av.Packet) error {
    if err := element.WriteAvPacket(pkt); err != nil {
        return err
    }
    
    // Флеш після ключового кадру для мінімізації втрат при збої
    if pkt.IsKeyFrame {
        if err := element.w.Sync(); err != nil {
            return err
        }
    }
    return nil
}
```

### 3. Моніторинг розміру файлу:

```go
type MuxerWithMetrics struct {
    *Muxer
    maxSize int64  // ліміт розміру файлу
    written int64  // лічильник записаних байт
}

func (m *MuxerWithMetrics) WriteAvPacket(pkt *av.Packet) error {
    if m.maxSize > 0 && m.written+int64(len(pkt.Data)+4) > m.maxSize {
        return fmt.Errorf("file size limit exceeded")
    }
    
    if err := m.Muxer.WriteAvPacket(pkt); err != nil {
        return err
    }
    
    m.written += int64(len(pkt.Data) + 4)  // +4 для start code
    return nil
}
```

---

## 📋 Чек-лист безпечного використання

```go
// ✅ 1. Використовувати filepath.Join для шляхів
fullPath := filepath.Join(dir, name)
muxer, err := raw.NewMuxer(filepath.Dir(fullPath), filepath.Base(fullPath))

// ✅ 2. Перевіряти помилки запису
if _, err = w.Write(data); err != nil {
    return fmt.Errorf("write failed: %w", err)
}

// ✅ 3. Безпечне type assertion
switch stream.Type() {
case av.H264:
    codec, ok := stream.(h264parser.CodecData)
    if !ok { return fmt.Errorf("invalid H.264 codec data") }
    // використання codec.SPS(), codec.PPS()
}

// ✅ 4. Ініціалізація idx = -1
func NewMuxer(...) (*Muxer, error) {
    return &Muxer{
        w: f2,
        idx: -1,  // ← важливо!
    }, nil
}

// ✅ 5. Sync() перед Close()
func (m *Muxer) Close() error {
    if err := m.w.Sync(); err != nil {
        m.w.Close()
        return err
    }
    return m.w.Close()
}

// ✅ 6. Перевірка формату вхідних даних
if len(pkt.Data) >= 4 && binary.BigEndian.Uint32(pkt.Data[:4]) == uint32(len(pkt.Data)-4) {
    // AVCC формат — пропустити перші 4 байти
    data = pkt.Data[4:]
} else {
    // Annex B або інший формат — записати як є
    data = pkt.Data
}
```

---

## 🔗 Корисні посилання

- 📄 [H.264 Annex B Format](https://wiki.multimedia.cx/index.php/H.264#Annex_B) — опис start-code формату
- 📄 [H.265/H.266 Annex B](https://itu.int/rec/T-REC-H.265) — аналогічно для HEVC
- 💻 [Go path/filepath Package](https://pkg.go.dev/path/filepath) — безпечна робота з шляхами
- 🧪 [Go os.File.Sync](https://pkg.go.dev/os#File.Sync) — гарантований запис на диск

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди використовуйте `filepath.Join()`** — уникнення помилок шляхів на різних ОС.
> 2. **Перевіряйте тип кодека через type assertion з перевіркою** — уникнення панік у production.
> 3. **Валідуйте формат `pkt.Data`** — AVCC vs Annex B, щоб не викидати важливі байти.
> 4. **Ініціалізуйте `idx = -1`** — уникнення запису не того потоку при кількох відео-стрімах.
> 5. **Викликайте `Sync()` перед `Close()`** — гарантія, що дані записані на диск, особливо для критичних записів.

Потрібен приклад додавання підтримки аудіо (AAC у ADTS форматі) до цього `raw.Muxer` для запису повних авідео-потоків? Готовий допомогти! 🚀