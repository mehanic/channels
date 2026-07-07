# 🎵 Глибокий розбір: aac — AAC/ADTS Muxer/Demuxer для vdk

Цей файл — **реалізація muxer/demuxer для AAC аудіо у форматі ADTS** (Audio Data Transport Stream). Він дозволяє записувати та читати сирий AAC потік з заголовками ADTS, що є стандартним форматом для передачі AAC у контейнерах на кшталт MPEG-TS або як окремий файл `.aac`.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема aac пакету

```
┌────────────────────────────────────────┐
│ 📦 aac — ADTS Muxer/Demuxer           │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Muxer — запис AAC пакетів з ADTS   │
│  • Demuxer — читання AAC з ADTS       │
│  • aacparser інтеграція               │
│  • avutil.RegisterHandler реєстрація  │
│                                         │
│  📦 ADTS формат:                        │
│  [7-9 байт заголовок] + [AAC payload] │
│  • ObjectType, SampleRate, Channels   │
│  • Frame length, samples count        │
│                                         │
│  🔄 Потік даних:                        │
│  Write: av.Packet → ADTS header + data│
│  Read:  ADTS stream → av.Packet       │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Muxer — запис AAC пакетів у ADTS формат

### Структура:

```go
type Muxer struct {
    w       io.Writer                    // вихідний потік (файл, мережа)
    config  aacparser.MPEG4AudioConfig   // метадані кодека
    adtshdr []byte                       // буфер для ADTS заголовка (7 байт)
}
```

### 🔧 Метод `WriteHeader()`:

```go
func (self *Muxer) WriteHeader(streams []av.CodecData) (err error) {
    // 1. Перевірка: тільки один AAC потік
    if len(streams) > 1 || streams[0].Type() != av.AAC {
        err = fmt.Errorf("aac: must be only one aac stream")
        return
    }
    
    // 2. Отримання конфігурації з codecData
    self.config = streams[0].(aacparser.CodecData).Config
    
    // 3. Перевірка ObjectType для сумісності з ADTS
    if self.config.ObjectType > aacparser.AOT_AAC_LTP {
        err = fmt.Errorf("aac: AOT %d is not allowed in ADTS", self.config.ObjectType)
    }
    return nil
}
```

### 🔍 Чому перевірка `ObjectType > AOT_AAC_LTP`?

```
ADTS формат підтримує тільки певні Audio Object Types:
• AOT_AAC_MAIN (1), AOT_AAC_LC (2), AOT_AAC_SSR (3), AOT_AAC_LTP (4)

Більш складні типи (HE-AAC, HE-AACv2 тощо) вимагають додаткових метаданих,
які не вміщуються у стандартний ADTS заголовок.

Для HLS сумісності зазвичай використовується AAC-LC (ObjectType=2).
```

### 🔧 Метод `WritePacket()`:

```go
func (self *Muxer) WritePacket(pkt av.Packet) (err error) {
    // 1. Заповнення ADTS заголовка
    aacparser.FillADTSHeader(self.adtshdr, self.config, 1024, len(pkt.Data))
    // • self.config: метадані кодека
    // • 1024: кількість семплів у фреймі (AAC-LC завжди 1024)
    // • len(pkt.Data): довжина AAC payload
    
    // 2. Запис заголовка
    if _, err = self.w.Write(self.adtshdr); err != nil {
        return
    }
    
    // 3. Запис даних
    if _, err = self.w.Write(pkt.Data); err != nil {
        return
    }
    return nil
}
```

### ✅ Ваш use-case: запис аудіо у файл .aac

```go
// WriteAACFile — запис AAC пакетів у файл з ADTS заголовками
func WriteAACFile(filename string, packets []av.Packet, codecData av.AudioCodecData) error {
    f, err := os.Create(filename)
    if err != nil {
        return fmt.Errorf("create file: %w", err)
    }
    defer f.Close()
    
    // Створення muxer
    muxer := aac.NewMuxer(f)
    
    // Запис заголовка
    if err := muxer.WriteHeader([]av.CodecData{codecData}); err != nil {
        return fmt.Errorf("write header: %w", err)
    }
    
    // Запис кожного пакету
    for _, pkt := range packets {
        if err := muxer.WritePacket(pkt); err != nil {
            return fmt.Errorf("write packet: %w", err)
        }
    }
    
    return nil
}
```

---

## 🔑 2. Demuxer — читання AAC потоків з ADTS заголовками

### Структура:

```go
type Demuxer struct {
    r         *bufio.Reader              // буферизований вхідний потік
    config    aacparser.MPEG4AudioConfig // кешована конфігурація
    codecdata av.CodecData              // кешований codecData для Streams()
    ts        time.Duration             // поточний PTS для пакетів
}
```

### 🔧 Метод `Streams()`:

```go
func (self *Demuxer) Streams() (streams []av.CodecData, err error) {
    // 1. Кешування: якщо вже отримали — повертаємо
    if self.codecdata == nil {
        // 2. Peek 9 байт для парсингу ADTS заголовка
        var adtshdr []byte
        if adtshdr, err = self.r.Peek(9); err != nil {
            return
        }
        
        // 3. Парсинг конфігурації з заголовка
        var config aacparser.MPEG4AudioConfig
        if config, _, _, _, err = aacparser.ParseADTSHeader(adtshdr); err != nil {
            return
        }
        
        // 4. Створення codecData для повернення
        if self.codecdata, err = aacparser.NewCodecDataFromMPEG4AudioConfig(config); err != nil {
            return
        }
    }
    
    streams = []av.CodecData{self.codecdata}
    return
}
```

### 🔧 Метод `ReadPacket()`:

```go
func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
    // 1. Peek ADTS заголовок для парсингу
    var adtshdr []byte
    var config aacparser.MPEG4AudioConfig
    var hdrlen, framelen, samples int
    
    if adtshdr, err = self.r.Peek(9); err != nil {
        return
    }
    if config, hdrlen, framelen, samples, err = aacparser.ParseADTSHeader(adtshdr); err != nil {
        return
    }
    
    // 2. Читання всього фрейму (заголовок + payload)
    pkt.Data = make([]byte, framelen)
    if _, err = io.ReadFull(self.r, pkt.Data); err != nil {
        return
    }
    
    // 3. Видалення заголовка — залишаємо тільки AAC payload
    pkt.Data = pkt.Data[hdrlen:]
    
    // 4. Встановлення часу пакету
    pkt.Time = self.ts
    
    // 5. Оновлення таймстемпу для наступного пакету
    // Формула: ts += (samples / sampleRate) секунд
    self.ts += time.Duration(samples) * time.Second / time.Duration(config.SampleRate)
    
    return pkt, nil
}
```

### 🔍 Розрахунок тривалості та часу:

```
AAC-LC завжди має 1024 семпли на фрейм.
Тривалість фрейму = 1024 / sampleRate секунд.

Приклади:
• 48000 Hz: 1024/48000 = 21.333... ms
• 44100 Hz: 1024/44100 = 23.22... ms

PTS розрахунок:
  pkt[0].Time = 0
  pkt[1].Time = 0 + 21.333ms
  pkt[2].Time = 21.333ms + 21.333ms = 42.666ms
  ...

Це забезпечує коректну синхронізацію аудіо з відео у контейнерах.
```

### ✅ Ваш use-case: читання та аналіз .aac файлу

```go
// AnalyzeAACFile — читання та логування метаданих AAC файлу
func AnalyzeAACFile(filename string) error {
    f, err := os.Open(filename)
    if err != nil {
        return fmt.Errorf("open file: %w", err)
    }
    defer f.Close()
    
    demuxer := aac.NewDemuxer(f)
    
    // Отримання метаданих потоку
    streams, err := demuxer.Streams()
    if err != nil {
        return fmt.Errorf("get streams: %w", err)
    }
    
    codec := streams[0].(aacparser.CodecData)
    log.Printf("File: %s", filename)
    log.Printf("Codec: AAC-LC (ObjectType=%d)", codec.Config.ObjectType)
    log.Printf("SampleRate: %d Hz", codec.SampleRate())
    log.Printf("Channels: %d", codec.ChannelLayout().Count())
    
    // Читання та логування перших 10 пакетів
    for i := 0; i < 10; i++ {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read packet %d: %w", i, err)
        }
        
        duration, _ := codec.PacketDuration(pkt.Data)
        log.Printf("Packet %d: time=%v, duration=%v, size=%d", 
            i, pkt.Time, duration, len(pkt.Data))
    }
    
    return nil
}
```

---

## 🔑 3. Handler — реєстрація у avutil

### Функція `Handler()`:

```go
func Handler(h *avutil.RegisterHandler) {
    // 1. Розширення файлу для авто-детекту
    h.Ext = ".aac"
    
    // 2. Factory-функції для створення demuxer/muxer
    h.ReaderDemuxer = func(r io.Reader) av.Demuxer {
        return NewDemuxer(r)
    }
    h.WriterMuxer = func(w io.Writer) av.Muxer {
        return NewMuxer(w)
    }
    
    // 3. Probe-функція для авто-детекту формату
    h.Probe = func(b []byte) bool {
        // Спроба парсингу як ADTS заголовка
        _, _, _, _, err := aacparser.ParseADTSHeader(b)
        return err == nil  // якщо парсинг успішний — це AAC/ADTS
    }
    
    // 4. Підтримувані типи кодеків
    h.CodecTypes = []av.CodecType{av.AAC}
}
```

### 🔍 Як працює авто-детект:

```
1. avutil.Open("file.aac") викликає Probe() з першими байтами файлу
2. ParseADTSHeader() намагається розпарсити їх як ADTS заголовок
3. Якщо успішно → повертається true → використовується aac.Demuxer
4. Якщо ні → спроба інших форматів (mp3, opus тощо)

Це дозволяє відкривати файли без явного вказання формату.
```

### ✅ Ваш use-case: реєстрація handler у вашому проекті

```go
// init.go — реєстрація всіх підтримуваних форматів
func init() {
    // Реєстрація AAC handler
    aac.Handler(avutil.DefaultHandlers)
    
    // Реєстрація інших форматів...
    // h264.Handler(avutil.DefaultHandlers)
    // ts.Handler(avutil.DefaultHandlers)
    // тощо
}

// Використання: авто-відкриття файлу за розширенням
func OpenMediaFile(filename string) (av.DemuxCloser, error) {
    // avutil.Open автоматично визначить формат за розширенням або Probe()
    return avutil.Open(filename)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// aac_processor.go — обробка AAC аудіо для CCTV HLS Processor
type AACProcessor struct {
    channelID    string
    muxer        *aac.Muxer
    demuxer      *aac.Demuxer
    codecData    av.AudioCodecData
    packetQueue  chan av.Packet
    metrics      *AudioMetrics
}

func NewAACProcessor(channelID string) *AACProcessor {
    return &AACProcessor{
        channelID:   channelID,
        packetQueue: make(chan av.Packet, 100),
        metrics:     NewAudioMetrics(channelID),
    }
}

// InitForWrite — ініціалізація для запису аудіо у файл/потік
func (p *AACProcessor) InitForWrite(w io.Writer, codecData av.AudioCodecData) error {
    p.codecData = codecData
    p.muxer = aac.NewMuxer(w)
    
    // Запис заголовка
    if err := p.muxer.WriteHeader([]av.CodecData{codecData}); err != nil {
        return fmt.Errorf("write header: %w", err)
    }
    
    log.Printf("Channel %s: AAC muxer initialized", p.channelID)
    return nil
}

// WritePacket — запис одного аудіо-пакету
func (p *AACProcessor) WritePacket(pkt av.Packet) error {
    start := time.Now()
    
    if err := p.muxer.WritePacket(pkt); err != nil {
        p.metrics.WriteErrors.Inc()
        return fmt.Errorf("write packet: %w", err)
    }
    
    p.metrics.WriteLatency.Observe(time.Since(start).Seconds())
    p.metrics.PacketsWritten.Inc()
    
    return nil
}

// InitForRead — ініціалізація для читання аудіо з файлу/потоку
func (p *AACProcessor) InitForRead(r io.Reader) error {
    p.demuxer = aac.NewDemuxer(r)
    
    // Отримання метаданих
    streams, err := p.demuxer.Streams()
    if err != nil {
        return fmt.Errorf("get streams: %w", err)
    }
    
    p.codecData = streams[0].(av.AudioCodecData)
    log.Printf("Channel %s: AAC demuxer initialized, sampleRate=%d", 
        p.channelID, p.codecData.SampleRate())
    
    return nil
}

// ReadPacket — читання одного аудіо-пакету
func (p *AACProcessor) ReadPacket() (av.Packet, error) {
    start := time.Now()
    
    pkt, err := p.demuxer.ReadPacket()
    if err != nil {
        if err == io.EOF {
            return av.Packet{}, io.EOF
        }
        p.metrics.ReadErrors.Inc()
        return av.Packet{}, fmt.Errorf("read packet: %w", err)
    }
    
    p.metrics.ReadLatency.Observe(time.Since(start).Seconds())
    p.metrics.PacketsRead.Inc()
    
    return pkt, nil
}

// StartReaderLoop — фонове читання пакетів у чергу
func (p *AACProcessor) StartReaderLoop(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                return
                
            default:
                pkt, err := p.ReadPacket()
                if err == io.EOF {
                    log.Printf("Channel %s: AAC EOF reached", p.channelID)
                    return
                }
                if err != nil {
                    log.Printf("Channel %s: read error: %v", p.channelID, err)
                    return
                }
                
                // Відправка у чергу для подальшої обробки
                select {
                case p.packetQueue <- pkt:
                    // успішно
                default:
                    // черга переповнена
                    p.metrics.DroppedPackets.Inc()
                }
            }
        }
    }()
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"AOT X is not allowed in ADTS"** | ObjectType > 4 (напр. HE-AAC) | Використовуйте AAC-LC (ObjectType=2) для сумісності; або конвертуйте у інший формат |
| **"must be only one aac stream"** | Передача кількох потоків у WriteHeader | Переконайтеся, що передаєте тільки один `av.CodecData` типу `av.AAC` |
| **Некоректний PTS** | `self.ts` не оновлюється коректно | Переконайтеся, що `config.SampleRate` співпадає з реальним; перевірте `samples` з парсингу |
| **Peek(9) повертає помилку** | Недостатньо даних у потоці | Використовуйте `bufio.Reader` з достатнім буфером; перевірте цілісність вхідного потоку |
| **io.ReadFull не читає весь фрейм** | Обрізані дані або помилка мережі | Обробляйте `io.ErrUnexpectedEOF`; реалізуйте повторні спроби читання |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування ADTS заголовка:

```go
// ADTSHeaderCache — кешування заголовків для однакових конфігурацій
type ADTSHeaderCache struct {
    mu    sync.RWMutex
    cache map[string][]byte  // key: "rate_channels_objectType" → pre-filled header
}

func (c *ADTSHeaderCache) Get(config aacparser.MPEG4AudioConfig, payloadLen int) []byte {
    key := fmt.Sprintf("%d_%d_%d", config.SampleRate, config.ChannelConfig, config.ObjectType)
    
    c.mu.RLock()
    if header, ok := c.cache[key]; ok {
        // Копіюємо щоб уникнути race condition
        result := make([]byte, len(header))
        copy(result, header)
        // Оновлюємо довжину фрейму у заголовку
        aacparser.FillADTSHeader(result, config, 1024, payloadLen)
        c.mu.RUnlock()
        return result
    }
    c.mu.RUnlock()
    
    // Створення нового заголовка
    header := make([]byte, aacparser.ADTSHeaderLength)
    aacparser.FillADTSHeader(header, config, 1024, payloadLen)
    
    c.mu.Lock()
    c.cache[key] = header
    c.mu.Unlock()
    
    return header
}
```

### 2. Пакетне читання для зменшення накладних витрат:

```go
// BatchReadPackets — читання кількох пакетів за один виклик
func (d *Demuxer) BatchReadPackets(count int) ([]av.Packet, error) {
    packets := make([]av.Packet, 0, count)
    
    for i := 0; i < count; i++ {
        pkt, err := d.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return packets, err
        }
        packets = append(packets, pkt)
    }
    return packets, nil
}
```

### 3. Моніторинг параметрів потоку:

```go
type AACMetrics struct {
    SampleRate    prometheus.GaugeVec
    PacketSize    prometheus.HistogramVec
    ReadLatency   prometheus.HistogramVec
    WriteLatency  prometheus.HistogramVec
    PacketsProcessed prometheus.CounterVec
}

func (m *AACMetrics) RecordPacket(size int, duration time.Duration, channelID string, isRead bool) {
    m.PacketSize.WithLabelValues(channelID).Observe(float64(size))
    if isRead {
        m.ReadLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    } else {
        m.WriteLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    }
    m.PacketsProcessed.WithLabelValues(channelID).Inc()
}
```

---

## 📋 Чек-лист інтеграції aac пакету

```go
// ✅ 1. Реєстрація handler у init()
func init() {
    aac.Handler(avutil.DefaultHandlers)
}

// ✅ 2. Створення muxer з правильним codecData
muxer := aac.NewMuxer(writer)
codecData := aacparser.NewCodecDataFromMPEG4AudioConfig(config)
if err := muxer.WriteHeader([]av.CodecData{codecData}); err != nil {
    return err
}

// ✅ 3. Запис пакетів з коректними таймінгами
for _, pkt := range packets {
    if err := muxer.WritePacket(pkt); err != nil {
        return err
    }
}

// ✅ 4. Читання з демуксером з обробкою EOF
demuxer := aac.NewDemuxer(reader)
for {
    pkt, err := demuxer.ReadPacket()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    // обробка pkt
}

// ✅ 5. Авто-детект формату через avutil
demuxer, err := avutil.Open("file.aac")  // автоматично використає aac.Demuxer

// ✅ 6. Метрики для моніторингу
metrics.PacketSize.Observe(float64(len(pkt.Data)))
metrics.ProcessLatency.Observe(duration.Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [vdk aac Package](https://pkg.go.dev/github.com/deepch/vdk/codec/aac) — GoDoc documentation
- 📄 [ADTS Format Specification](https://wiki.multimedia.cx/index.php/ADTS) — детальний опис бітової структури
- 📄 [AAC Object Types](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#Audio_Object_Types) — список підтримуваних AOT
- 💻 [vdk aacparser Package](https://pkg.go.dev/github.com/deepch/vdk/codec/aacparser) — парсинг ADTS заголовків
- 🎬 [HLS Audio Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до аудіо у HLS

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **аудіо в реальному часі**:
> 1. **Завжди використовуйте AAC-LC (ObjectType=2)** для максимальної сумісності з ADTS та HLS.
> 2. **Кешуйте ADTS заголовки** — генерація бітових полів "на льоту" дорога для високобітрейтних потоків.
> 3. **Моніторьте `PacketSize` та `ReadLatency`** — різкі зміни можуть вказувати на проблеми з мережею або джерелом.
> 4. **Обробляйте `io.EOF` коректно** — це нормальне завершення потоку, а не помилка.
> 5. **Тестуйте з різними `SampleRate`** — CCTV камери часто використовують 48kHz, але іноді 44.1kHz або 16kHz.

Потрібен приклад інтеграції `AACProcessor` з вашим `pubsub.Queue` для розподілу вже оброблених аудіо-пакетів між підписниками (транскодер, WebSocket, архів)? Готовий допомогти! 🚀