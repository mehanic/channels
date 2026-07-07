# 📦 Глибокий розбір: ts — MPEG-TS Demuxer для vdk

Цей файл — **повноцінна реалізація демуксера MPEG Transport Stream (TS)**, що перетворює сирий TS потік у уніфіковані `av.Packet` об'єкти для подальшої обробки. Він підтримує авто-детект програм (PAT/PMT), парсинг аудіо/відео потоків (H.264, AAC, MJPEG), та коректну обробку таймінгів (PTS/DTS).

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема ts.Demuxer

```
┌────────────────────────────────────────┐
│ 📦 ts.Demuxer — MPEG-TS Stream Parser │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Demuxer — основний парсер           │
│  • Stream — обробка окремого потоку    │
│  • probe() — авто-детект PAT/PMT/кодеків│
│  • poll() → readTSPacket() — читання пакетів│
│                                         │
│  🔄 Потік даних:                        │
│  io.Reader → TS Packet (188 bytes)     │
│           → PAT/PMT parsing            │
│           → PES parsing                │
│           → av.Packet queue            │
│                                         │
│  📊 Підтримувані кодеки:                │
│  • H.264 (Annex B / AVCC)              │
│  • AAC (ADTS)                          │
│  • MJPEG (Alignment Descriptor)        │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Demuxer — основна структура демуксера

### Поля та їх призначення:

```go
type Demuxer struct {
    r *bufio.Reader  // буферизований вхідний потік
    
    pkts []av.Packet  // черга готових пакетів для читання
    
    pat     *tsio.PAT   // Program Association Table
    pmt     *tsio.PMT   // Program Map Table
    streams []*Stream   // масив потоків (відео/аудіо)
    
    tshdr   []byte      // буфер для 188-байтового TS заголовку
    AnnexB  bool        // режим: чи зберігати NALU у Annex B форматі
    stage   int         // стан ініціалізації: 0=проба, 1=готова
}
```

### 🔧 Метод `probe()` — авто-детект структури потоку:

```go
func (self *Demuxer) probe() (err error) {
    if self.stage == 0 {
        for {
            // Умова завершення проби: PMT знайдено + всі потоки мають CodecData
            if self.pmt != nil {
                n := 0
                for _, stream := range self.streams {
                    if stream.CodecData != nil {
                        n++
                    }
                }
                if n == len(self.streams) {
                    break  // всі кодеки визначені
                }
            }
            
            // Читання наступного пакету
            if err = self.poll(); err != nil {
                return
            }
        }
        self.stage++  // проба завершена
    }
    return
}
```

### 🔍 Чому така логіка проби?

```
MPEG-TS потік може починатися з будь-якого місця:
• Спочатку можуть йти відео/аудіо пакети без таблиць
• Таблиці PAT/PMT можуть повторюватися періодично

Проба продовжується поки:
1. Знайдено PMT (знаємо які PID є відео/аудіо)
2. Для кожного потоку отримано CodecData (SPS/PPS для H.264, AudioSpecificConfig для AAC)

Це гарантує, що `Streams()` поверне повні метадані перед початком читання пакетів.
```

### ✅ Ваш use-case: отримання метаданих перед обробкою

```go
// GetStreamMetadata — безпечне отримання метаданих з демуксера
func GetStreamMetadata(demuxer *ts.Demuxer) ([]av.CodecData, error) {
    // Streams() автоматично запустить пробу якщо потрібно
    streams, err := demuxer.Streams()
    if err != nil {
        return nil, fmt.Errorf("probe failed: %w", err)
    }
    
    log.Printf("Detected %d streams:", len(streams))
    for i, s := range streams {
        log.Printf("  [%d] Type: %v", i, s.Type())
        switch v := s.(type) {
        case h264parser.CodecData:
            log.Printf("      H.264: %dx%d @ %d fps", v.Width(), v.Height(), v.FPS())
        case aacparser.CodecData:
            log.Printf("      AAC: %d Hz, %d channels", v.SampleRate(), v.ChannelLayout().Count())
        }
    }
    
    return streams, nil
}
```

---

## 🔑 2. Stream — обробка окремого елементарного потоку

### Структура (не показана явно, але використовується):

```go
type Stream struct {
    idx        int              // індекс у масиві streams
    demuxer    *Demuxer         // посилання на батьківський демуксер
    pid        uint16           // PID цього потоку
    streamType uint8           // тип: 0x1B=H.264, 0x0F=AAC, тощо
    
    CodecData  av.CodecData    // метадані кодека (заповнюється під час проби)
    
    // Стан для збирання PES пакетів:
    data       []byte          // буфер для накопичення даних
    datalen    int             // очікувана довжина (0 = variable length)
    pts, dts   time.Duration   // таймінги з PES заголовку
    pt         time.Duration   // попередній timestamp для розрахунку duration
    iskeyframe bool            // прапорець ключового кадру
    fps        uint            // FPS для H.264 (з SPS)
}
```

### 🔧 Метод `handleTSPacket()` — обробка одного TS пакету:

```go
func (self *Stream) handleTSPacket(start bool, iskeyframe bool, payload []byte) (err error) {
    // 1. Якщо це початок нового PES пакету — завершити попередній
    if start {
        if _, err = self.payloadEnd(); err != nil {
            return
        }
        
        // Парсинг PES заголовку
        var hdrlen int
        if hdrlen, _, self.datalen, self.pts, self.dts, err = tsio.ParsePESHeader(payload); err != nil {
            return
        }
        
        self.iskeyframe = iskeyframe
        
        // Ініціалізація буфера для даних
        if self.datalen == 0 {
            self.data = make([]byte, 0, 4096)  // variable length
        } else {
            self.data = make([]byte, 0, self.datalen)  // фіксована довжина
        }
        
        // Додавання першої частини payload
        self.data = append(self.data, payload[hdrlen:]...)
    } else {
        // Продовження попереднього PES пакету
        self.data = append(self.data, payload...)
    }
    return
}
```

### 🔧 Метод `payloadEnd()` — фіналізація PES пакету:

```go
func (self *Stream) payloadEnd() (n int, err error) {
    payload := self.data
    if payload == nil {
        return  // немає даних для обробки
    }
    
    // Перевірка цілісності даних
    if self.datalen != 0 && len(payload) != self.datalen {
        err = fmt.Errorf("ts: packet size mismatch size=%d correct=%d", len(payload), self.datalen)
        return
    }
    self.data = nil  // очищення буфера
    
    // Обробка за типом потоку
    switch self.streamType {
    case tsio.ElementaryStreamTypeH264:
        // Розбиття на NALU, пошук SPS/PPS, конвертація Annex B → AVCC
        nalus, _ := h264parser.SplitNALUs(payload)
        // ... обробка кожного NALU ...
        
    case tsio.ElementaryStreamTypeAdtsAAC:
        // Парсинг ADTS заголовків, витягування аудіо фреймів
        for len(payload) > 0 {
            config, hdrlen, framelen, samples, err := aacparser.ParseADTSHeader(payload)
            // ... створення пакетів з коректними таймінгами ...
        }
        
    case tsio.ElementaryStreamTypeAlignmentDescriptor:
        // Обробка MJPEG (простий випадок)
        // ...
    }
    
    return n, nil
}
```

### ✅ Ваш use-case: обробка H.264 потоку з конвертацією формату

```go
// ProcessH264Stream — приклад обробки відео потоку
func (s *Stream) ProcessH264Stream(payload []byte, annexB bool) ([]av.Packet, error) {
    var packets []av.Packet
    
    // 1. Розбиття на NALU
    nalus, err := h264parser.SplitNALUs(payload)
    if err != nil {
        return nil, err
    }
    
    var sps, pps []byte
    for _, nalu := range nalus {
        if len(nalu) == 0 {
            continue
        }
        
        naltype := nalu[0] & 0x1f
        switch naltype {
        case 7:  // SPS
            sps = nalu
            // Парсинг для отримання метаданих
            info, err := h264parser.ParseSPS(sps)
            if err == nil {
                s.fps = info.FPS
            }
            
        case 8:  // PPS
            pps = nalu
            
        case 1, 2, 3, 4, 5:  // VCL NALU (відео дані)
            if !annexB {
                // Конвертація Annex B → AVCC: додаємо 4-байтову довжину
                b := make([]byte, 4+len(nalu))
                pio.PutU32BE(b[0:4], uint32(len(nalu)))
                copy(b[4:], nalu)
                
                // Розрахунок duration з FPS
                fps := s.fps
                if fps == 0 {
                    fps = 25  // дефолт
                }
                duration := (1000 * time.Millisecond) / time.Duration(fps)
                
                pkt := av.Packet{
                    Idx:        int8(s.idx),
                    IsKeyFrame: naltype == 5,  // IDR = ключовий кадр
                    Time:       s.dts,
                    Data:       b,
                    Duration:   duration,
                }
                if s.pts != s.dts {
                    pkt.CompositionTime = s.pts - s.dts
                }
                packets = append(packets, pkt)
            }
        }
    }
    
    // Створення CodecData якщо є SPS+PPS
    if s.CodecData == nil && len(sps) > 0 && len(pps) > 0 {
        s.CodecData, err = h264parser.NewCodecDataFromSPSAndPPS(sps, pps)
        if err != nil {
            return nil, err
        }
    }
    
    return packets, nil
}
```

---

## 🔑 3. Таймінги: PTS/DTS обробка та розрахунок duration

### 🔧 Метод `addPacket()` — створення av.Packet з коректними таймінгами:

```go
func (self *Stream) addPacket(payload []byte, timedelta time.Duration, fixed time.Duration) {
    dts := self.dts
    pts := self.pts
    
    // Якщо DTS не вказано — використовуємо PTS
    if dts == 0 {
        dts = pts
    }
    
    // Розрахунок duration
    dur := time.Duration(0)
    if self.pt > 0 {
        // Якщо є попередній timestamp — розрахунок з різниці
        dur = dts + timedelta - self.pt
    } else {
        // Інакше — використовуємо фіксоване значення (напр. з FPS)
        dur = fixed
    }
    
    // Оновлення попереднього timestamp
    self.pt = dts + timedelta
    
    // Створення пакету
    demuxer := self.demuxer
    pkt := av.Packet{
        Idx:        int8(self.idx),
        IsKeyFrame: self.iskeyframe,
        Time:       dts + timedelta,  // фінальний DTS
        Data:       payload,
        Duration:   dur,
    }
    
    // CompositionTime для B-frames: PTS - DTS
    if pts != dts {
        pkt.CompositionTime = pts - dts
    }
    
    // Додавання у чергу демуксера
    demuxer.pkts = append(demuxer.pkts, pkt)
}
```

### 🔍 Розрахунок таймінгів для AAC:

```go
// Приклад з payloadEnd() для AAC:
delta := time.Duration(0)
for len(payload) > 0 {
    config, hdrlen, framelen, samples, err := aacparser.ParseADTSHeader(payload)
    
    // Створення пакету з коректним таймінгом
    self.addPacket(
        payload[hdrlen:framelen],  // аудіо дані без ADTS заголовку
        delta,                      // зсув від початку PES пакету
        time.Duration(samples)*time.Second/time.Duration(config.SampleRate),  // duration з кількості семплів
    )
    
    // Оновлення зсуву для наступного фрейму
    delta += time.Duration(samples) * time.Second / time.Duration(config.SampleRate)
    payload = payload[framelen:]  // перехід до наступного фрейму
}
```

### ✅ Ваш use-case: синхронізація аудіо/відео таймінгів

```go
// SyncAVTimestamps — корекція розсинхронізації аудіо/відео
func SyncAVTimestamps(videoPTS, audioPTS time.Duration, videoSampleRate, audioSampleRate int) (time.Duration, error) {
    // Конвертація у ticks для порівняння
    videoTicks := uint64(videoPTS * 90000 / time.Second)  // PTS_HZ
    audioTicks := uint64(audioPTS * 90000 / time.Second)
    
    // Розрахунок різниці
    diff := int64(videoTicks) - int64(audioTicks)
    
    // Якщо різниця > 100ms — корегуємо
    if abs(diff) > 9000 {  // 9000 ticks = 100ms @ 90kHz
        log.Printf("A/V sync drift: %d ms", diff*1000/90000)
        // Корекція: зсув аудіо до відео
        return time.Duration(int64(audioPTS) + diff*int64(time.Second)/90000), nil
    }
    
    return audioPTS, nil
}

func abs(x int64) int64 {
    if x < 0 { return -x }
    return x
}
```

---

## 🔑 4. Читання пакетів: ReadPacket() та poll()

### 🔧 Метод `ReadPacket()` — публічний інтерфейс:

```go
func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
    // 1. Запуск проби якщо потрібно
    if err = self.probe(); err != nil {
        return
    }
    
    // 2. Якщо черга порожня — читаємо нові пакети
    for len(self.pkts) == 0 {
        if err = self.poll(); err != nil {
            return
        }
    }
    
    // 3. Повернення першого пакету з черги
    pkt = self.pkts[0]
    self.pkts = self.pkts[1:]
    return
}
```

### 🔧 Метод `poll()` → `readTSPacket()` — низькорівневе читання:

```go
func (self *Demuxer) poll() (err error) {
    if err = self.readTSPacket(); err == io.EOF {
        // Обробка кінця потоку: фіналізація залишкових даних
        var n int
        if n, err = self.payloadEnd(); err != nil {
            return
        }
        if n == 0 {
            err = io.EOF  // дійсний кінець
        }
    }
    return
}

func (self *Demuxer) readTSPacket() (err error) {
    // 1. Читання 188-байтового TS пакету
    if _, err = io.ReadFull(self.r, self.tshdr); err != nil {
        return
    }
    
    // 2. Парсинг заголовку
    var hdrlen int
    var pid uint16
    var start bool
    var iskeyframe bool
    if pid, start, iskeyframe, hdrlen, err = tsio.ParseTSHeader(self.tshdr); err != nil {
        return
    }
    payload := self.tshdr[hdrlen:]
    
    // 3. Обробка залежно від PID та стану демуксера
    if self.pat == nil {
        // Ще не знайдено PAT — шукаємо на PID 0
        if pid == 0 {
            // Парсинг PAT секції
            var psihdrlen, datalen int
            if _, _, psihdrlen, datalen, err = tsio.ParsePSI(payload); err != nil {
                return
            }
            self.pat = &tsio.PAT{}
            if _, err = self.pat.Unmarshal(payload[psihdrlen : psihdrlen+datalen]); err != nil {
                return
            }
        }
    } else if self.pmt == nil {
        // PAT знайдено, шукаємо PMT за ProgramMapPID
        for _, entry := range self.pat.Entries {
            if entry.ProgramMapPID == pid {
                if err = self.initPMT(payload); err != nil {
                    return
                }
                break
            }
        }
    } else {
        // Обидві таблиці знайдені — обробка елементарних потоків
        for _, stream := range self.streams {
            if pid == stream.pid {
                // Специфічна обробка для AAC
                if stream.streamType == tsio.ElementaryStreamTypeAdtsAAC {
                    iskeyframe = false  // аудіо не має ключових кадрів
                }
                if err = stream.handleTSPacket(start, iskeyframe, payload); err != nil {
                    return
                }
                break
            }
        }
    }
    
    return
}
```

### ✅ Ваш use-case: читання потоків з обробкою помилок

```go
// ReadPacketsWithTimeout — читання пакетів з таймаутом
func ReadPacketsWithTimeout(demuxer *ts.Demuxer, count int, timeout time.Duration) ([]av.Packet, error) {
    packets := make([]av.Packet, 0, count)
    deadline := time.Now().Add(timeout)
    
    for len(packets) < count && time.Now().Before(deadline) {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF {
            break  // нормальне завершення
        }
        if err != nil {
            return packets, fmt.Errorf("read packet: %w", err)
        }
        packets = append(packets, pkt)
    }
    
    return packets, nil
}

// Використання:
pkts, err := ReadPacketsWithTimeout(demuxer, 100, 5*time.Second)
if err != nil {
    log.Printf("warning: read timeout or error: %v", err)
}
log.Printf("Read %d packets", len(pkts))
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// ts_to_hls_processor.go — конвертація TS потоку у HLS сегменти
type TStoHLSProcessor struct {
    channelID    string
    demuxer      *ts.Demuxer
    hlsWriter    *HLSWriter
    metrics      *ConversionMetrics
}

func NewTStoHLSProcessor(channelID string, input io.Reader) (*TStoHLSProcessor, error) {
    return &TStoHLSProcessor{
        channelID:  channelID,
        demuxer:    ts.NewDemuxer(input),
        hlsWriter:  NewHLSWriter(channelID),
        metrics:    NewConversionMetrics(channelID),
    }, nil
}

// Process — основний цикл конвертації
func (p *TStoHLSProcessor) Process(ctx context.Context) error {
    // 1. Отримання метаданих потоків (автоматична проба)
    streams, err := p.demuxer.Streams()
    if err != nil {
        return fmt.Errorf("probe streams: %w", err)
    }
    
    // 2. Ініціалізація HLS writer з метаданими
    if err := p.hlsWriter.WriteHeader(streams); err != nil {
        return fmt.Errorf("init HLS: %w", err)
    }
    
    // 3. Стан для сегментації
    var currentSegment *HLSSegment
    var lastKeyFrameTime time.Duration
    
    // 4. Основний цикл читання/запису
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        // Читання пакету
        pkt, err := p.demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Оновлення метрик
        p.metrics.PacketsProcessed.Inc()
        
        // Детекція ключових кадрів для сегментації
        if pkt.IsKeyFrame && pkt.Idx == 0 {  // відео ключовий кадр
            if currentSegment != nil && pkt.Time-lastKeyFrameTime >= 10*time.Second {
                // Завершити поточний сегмент
                if err := p.hlsWriter.WriteSegment(currentSegment); err != nil {
                    return err
                }
                currentSegment = nil
            }
            if currentSegment == nil {
                currentSegment = p.hlsWriter.StartNewSegment(pkt.Time)
                lastKeyFrameTime = pkt.Time
            }
        }
        
        // Додавання пакету у поточний сегмент
        if currentSegment != nil {
            if err := currentSegment.AddPacket(pkt); err != nil {
                return err
            }
        }
    }
    
    // Фіналізація останнього сегменту
    if currentSegment != nil {
        if err := p.hlsWriter.WriteSegment(currentSegment); err != nil {
            return err
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"ts: packet size mismatch"** | Очікувана довжина PES не співпадає з реальною | Перевірте цілісність вхідного потоку; можливе обрізання пакету при мережевих помилках |
| **probe() зависає** | Не знаходить PMT або CodecData за розумний час | Збільште `MaxProbePacketCount`; перевірте чи потік дійсно містить PAT/PMT на початку |
| **PTS/DTS розсинхронізація** | Аудіо відстає або випереджає відео | Переконайтеся, що `addPacket()` коректно розраховує `CompositionTime`; перевірте джерело таймінгів |
| **H.264 NALU не парситься** | `SplitNALUs()` не знаходить start codes | Переконайтеся, що потік дійсно у Annex B форматі; перевірте `AnnexB` прапорець демуксера |
| **AAC фрейми обрізані** | `ParseADTSHeader()` повертає помилку | Переконайтеся, що `payload` містить повний ADTS фрейм; перевірте цілісність даних |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування буферів для TS пакетів:

```go
// TSPacketBufferPool — пул буферів для уникнення аллокацій
var TSPacketBufferPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 188)  // фіксований розмір TS пакету
        return &buf
    },
}

func GetTSPacketBuffer() *[]byte {
    return TSPacketBufferPool.Get().(*[]byte)
}

func PutTSPacketBuffer(buf *[]byte) {
    // Очищення не потрібне, бо буфер перезаписується при читанні
    TSPacketBufferPool.Put(buf)
}

// Використання у readTSPacket():
buf := GetTSPacketBuffer()
defer PutTSPacketBuffer(buf)
if _, err = io.ReadFull(self.r, *buf); err != nil { /* handle */ }
```

### 2. Пакетне читання для зменшення системних викликів:

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

### 3. Моніторинг продуктивності демуксингу:

```go
type DemuxerMetrics struct {
    PacketsRead    prometheus.CounterVec
    ReadLatency    prometheus.HistogramVec
    ProbeDuration  prometheus.HistogramVec
    StreamTypes    prometheus.CounterVec
}

func (m *DemuxerMetrics) RecordPacket(streamType uint8, duration time.Duration, channelID string) {
    m.PacketsRead.WithLabelValues(fmt.Sprintf("0x%X", streamType), channelID).Inc()
    m.ReadLatency.WithLabelValues(channelID).Observe(duration.Seconds())
}

func (m *DemuxerMetrics) RecordProbe(duration time.Duration, channelID string) {
    m.ProbeDuration.WithLabelValues(channelID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції ts.Demuxer

```go
// ✅ 1. Створення демуксера з буферизацією
demuxer := ts.NewDemuxer(reader)  // автоматично використовує bufio.Reader

// ✅ 2. Отримання метаданих перед обробкою (автоматична проба)
streams, err := demuxer.Streams()
if err != nil { /* handle error */ }

// ✅ 3. Читання пакетів з перевіркою EOF
for {
    pkt, err := demuxer.ReadPacket()
    if err == io.EOF {
        break  // нормальне завершення
    }
    if err != nil {
        log.Printf("read error: %v", err)
        break
    }
    // обробка pkt
}

// ✅ 4. Обробка таймінгів з урахуванням CompositionTime
if pkt.CompositionTime != 0 {
    pts := pkt.Time + pkt.CompositionTime
    // використання PTS для синхронізації
}

// ✅ 5. Налаштування AnnexB режиму якщо потрібно
demuxer.AnnexB = true  // зберігати NALU у Annex B форматі (для деяких плеєрів)

// ✅ 6. Метрики для моніторингу
start := time.Now()
pkt, err := demuxer.ReadPacket()
metrics.RecordPacket(pkt.Idx, time.Since(start), channelID)

// ✅ 7. Закриття ресурсів
if closer, ok := reader.(io.Closer); ok {
    defer closer.Close()
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk ts Package](https://pkg.go.dev/github.com/deepch/vdk/format/ts) — GoDoc documentation
- 💻 [vdk tsio Package](https://pkg.go.dev/github.com/deepch/vdk/format/ts/tsio) — низькорівневі TS утиліти
- 📄 [MPEG-TS Specification (ISO/IEC 13818-1)](https://www.iso.org/standard/82746.html) — офіційний стандарт
- 📄 [H.264 in TS](https://wiki.multimedia.cx/index.php/H.264_in_MPEG-TS) — особливості кодування
- 📄 [AAC ADTS in TS](https://wiki.multimedia.cx/index.php/ADTS) — формат аудіо фреймів
- 🧪 [Go bufio Package](https://pkg.go.dev/bufio) — буферизований I/O для ефективності

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **медіа потоками у реальному часі**:
> 1. **Завжди викликайте `Streams()` перед `ReadPacket()`** — це гарантує, що проба завершена і метадані доступні.
> 2. **Кешуйте буфери через `sync.Pool`** — це значно зменшує навантаження на GC при обробці тисяч пакетів на секунду.
> 3. **Моніторьте `ProbeDuration`** — якщо проба займає занадто довго, потік може бути пошкоджений або використовувати нестандартну структуру.
> 4. **Обробляйте `CompositionTime` коректно** — це критично для синхронізації аудіо/відео при наявності B-frames.
> 5. **Тестуйте з різними джерелами** — камери, енкодери, мережеві потоки можуть надсилати трохи різні формати пакетів.

Потрібен приклад інтеграції `TStoHLSProcessor` з вашим `pubsub.Queue` для розподілу вже конвертованих пакетів між підписниками (WebSocket, архів, аналітика)? Готовий допомогти! 🚀