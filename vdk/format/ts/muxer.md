# 📦 Глибокий розбір: ts.Muxer — MPEG-TS Muxer для vdk

Цей файл — **повноцінна реалізація муксера MPEG Transport Stream (TS)**, що перетворює уніфіковані `av.Packet` об'єкти у сирий TS потік для стрімінгу або запису. Він підтримує мультиплексування відео (H.264/H.265) та аудіо (AAC) потоків, генерацію PSI таблиць (PAT/PMT), та коректну обробку таймінгів (PTS/DTS/PCR).

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема ts.Muxer

```
┌────────────────────────────────────────┐
│ 📦 ts.Muxer — MPEG-TS Stream Writer   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Muxer — основний муксер             │
│  • Stream — обробка окремого потоку    │
│  • WritePATPMT() — генерація таблиць   │
│  • WritePacket() — запис медіа даних   │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet → PES Header → TS Packet    │
│           → PAT/PMT tables             │
│           → io.Writer (file/network)   │
│                                         │
│  📊 Підтримувані кодеки:                │
│  • H.264 (Annex B з AUD/start codes)   │
│  • H.265 (аналогічно H.264)            │
│  • AAC (ADTS заголовки)                │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Muxer — основна структура муксера

### Поля та їх призначення:

```go
type Muxer struct {
    w       io.Writer              // вихідний потік (файл, мережа)
    streams map[int]*Stream        // потоки за індексом (відео=0, аудіо=1...)
    
    PaddingToMakeCounterCont bool   // чи додавати padding для вирівнювання continuity counter
    
    // Буфери для ефективної серіалізації (попередньо виділені):
    psidata []byte  // для PSI секцій (PAT/PMT)
    peshdr  []byte  // для PES заголовків
    tshdr   []byte  // для TS заголовків
    adtshdr []byte  // для ADTS заголовків (AAC)
    datav   [][]byte  // вектор для scatter-gather запису
    nalus   [][]byte  // буфер для H.264/H.265 NALU
    
    // TS writers для системних потоків (PAT/PMT):
    tswpat, tswpmt *tsio.TSWriter
}
```

### 🔧 Метод `newStream()` — реєстрація нового потоку:

```go
func (self *Muxer) newStream(idx int, codec av.CodecData) (err error) {
    // 1. Перевірка чи кодек підтримується
    ok := false
    for _, c := range CodecTypes {  // []av.CodecType{av.H264, av.H265, av.AAC}
        if codec.Type() == c {
            ok = true
            break
        }
    }
    if !ok {
        err = fmt.Errorf("ts: codec type=%s is not supported", codec.Type())
        return
    }
    
    // 2. Розрахунок PID: базовий 0x100 + індекс
    pid := uint16(idx + 0x100)  // відео=0→0x100, аудіо=1→0x101
    
    // 3. Створення об'єкта Stream
    stream := &Stream{
        muxer:     self,
        CodecData: codec,
        pid:       pid,
        tsw:       tsio.NewTSWriter(pid),  // окремий TS writer для цього PID
    }
    self.streams[idx] = stream
    return
}
```

### ✅ Ваш use-case: валідація потоків перед записом

```go
// ValidateStreamsForTS — перевірка сумісності потоків з TS форматом
func ValidateStreamsForTS(streams []av.CodecData) error {
    supported := map[av.CodecType]bool{
        av.H264: true,
        av.H265: true,
        av.AAC:  true,
    }
    
    var hasVideo bool
    for _, s := range streams {
        if !supported[s.Type()] {
            return fmt.Errorf("unsupported codec for TS muxing: %v", s.Type())
        }
        if s.Type().IsVideo() {
            hasVideo = true
        }
    }
    
    if !hasVideo {
        return fmt.Errorf("TS stream must contain at least one video track")
    }
    
    return nil
}

// Використання перед ініціалізацією муксера:
if err := ValidateStreamsForTS(streams); err != nil {
    return fmt.Errorf("invalid streams: %w", err)
}
muxer := ts.NewMuxer(writer)
if err := muxer.WriteHeader(streams); err != nil { /* handle */ }
```

---

## 🔑 2. WritePATPMT() — генерація PSI таблиць

### Призначення:
PAT (Program Association Table) та PMT (Program Map Table) — це метадані, що описують структуру програми: які PID містять відео/аудіо, які кодеки використовуються, тощо.

### 🔧 Генерація PAT:

```go
func (self *Muxer) WritePATPMT() (err error) {
    // PAT: програма 1 → PMT на PID 0x1000
    pat := tsio.PAT{
        Entries: []tsio.PATEntry{
            {ProgramNumber: 1, ProgramMapPID: tsio.PMT_PID},  // PMT_PID = 0x1000
        },
    }
    
    // Серіалізація PAT у буфер
    patlen := pat.Marshal(self.psidata[tsio.PSIHeaderLength:])
    n := tsio.FillPSI(self.psidata, tsio.TableIdPAT, tsio.TableExtPAT, patlen)
    self.datav[0] = self.psidata[:n]
    
    // Запис у TS потік з PID=0 (PAT_PID)
    if err = self.tswpat.WritePackets(self.w, self.datav[:1], 0, false, true); err != nil {
        return
    }
    
    // ... генерація PMT далі ...
}
```

### 🔧 Генерація PMT:

```go
// Збір інформації про елементарні потоки
var elemStreams []tsio.ElementaryStreamInfo
for _, stream := range self.streams {
    switch stream.Type() {
    case av.AAC:
        elemStreams = append(elemStreams, tsio.ElementaryStreamInfo{
            StreamType:    tsio.ElementaryStreamTypeAdtsAAC,  // 0x0F
            ElementaryPID: stream.pid,
        })
    case av.H264:
        elemStreams = append(elemStreams, tsio.ElementaryStreamInfo{
            StreamType:    tsio.ElementaryStreamTypeH264,  // 0x1B
            ElementaryPID: stream.pid,
        })
    case av.H265:
        elemStreams = append(elemStreams, tsio.ElementaryStreamInfo{
            StreamType:    tsio.ElementaryStreamTypeH265,  // 0x24
            ElementaryPID: stream.pid,
        })
    }
}

// Створення PMT
pmt := tsio.PMT{
    PCRPID:                0x100,  // PCR у відео потоці (перший потік)
    ElementaryStreamInfos: elemStreams,
}

// Серіалізація та запис
pmtlen := pmt.Len()
if pmtlen+tsio.PSIHeaderLength > len(self.psidata) {
    err = fmt.Errorf("ts: pmt too large")
    return
}
pmt.Marshal(self.psidata[tsio.PSIHeaderLength:])
n := tsio.FillPSI(self.psidata, tsio.TableIdPMT, tsio.TableExtPMT, pmtlen)
self.datav[0] = self.psidata[:n]
if err = self.tswpmt.WritePackets(self.w, self.datav[:1], 0, false, true); err != nil {
    return
}
```

### ✅ Ваш use-case: періодична повторна відправка PAT/PMT

```go
// TSMuxerWithPeriodicPSI — муксер з періодичною відправкою таблиць
type TSMuxerWithPeriodicPSI struct {
    *ts.Muxer
    psiInterval time.Duration
    lastPSITime time.Time
}

func NewTSMuxerWithPeriodicPSI(w io.Writer, psiInterval time.Duration) *TSMuxerWithPeriodicPSI {
    return &TSMuxerWithPeriodicPSI{
        Muxer:       ts.NewMuxer(w),
        psiInterval: psiInterval,
    }
}

// WritePacketWithPSI — перевизначення з періодичною відправкою таблиць
func (m *TSMuxerWithPeriodicPSI) WritePacketWithPSI(pkt av.Packet) error {
    // Періодична відправка PAT/PMT (напр. кожні 500ms)
    if time.Since(m.lastPSITime) >= m.psiInterval {
        if err := m.WritePATPMT(); err != nil {
            return err
        }
        m.lastPSITime = time.Now()
    }
    
    // Стандартний запис пакету
    return m.WritePacket(pkt)
}

// Використання:
muxer := NewTSMuxerWithPeriodicPSI(writer, 500*time.Millisecond)
// Тепер PAT/PMT відправлятимуться автоматично кожні 500ms
```

---

## 🔑 3. WritePacket() — запис медіа даних

### 🔧 Загальна логіка:

```go
func (self *Muxer) WritePacket(pkt av.Packet) (err error) {
    // 1. Пошук потоку за індексом
    stream, ok := self.streams[int(pkt.Idx)]
    if !ok {
        fmt.Printf("Warning, unsupported stream index: %d\n", pkt.Idx)
        return  // ігнорування невідомих потоків
    }
    
    // 2. Корекція часу: додавання 1 секунди (можливо для компенсації затримки)
    pkt.Time += time.Second
    
    // 3. Обробка за типом кодека
    switch stream.Type() {
    case av.AAC:
        // Обробка AAC...
    case av.H264:
        // Обробка H.264...
    case av.H265:
        // Обробка H.265...
    }
    return
}
```

### 🔧 Обробка AAC:

```go
case av.AAC:
    codec := stream.CodecData.(aacparser.CodecData)
    
    // 1. Генерація PES заголовку
    n := tsio.FillPESHeader(
        self.peshdr, 
        tsio.StreamIdAAC,           // 0xC0
        len(self.adtshdr)+len(pkt.Data),  // загальна довжина
        pkt.Time, 0)  // PTS, DTS=0 для аудіо
    self.datav[0] = self.peshdr[:n]
    
    // 2. Генерація ADTS заголовку
    aacparser.FillADTSHeader(self.adtshdr, codec.Config, 1024, len(pkt.Data))
    self.datav[1] = self.adtshdr
    self.datav[2] = pkt.Data  // сирі AAC дані
    
    // 3. Запис у TS: PES header + ADTS header + AAC data
    if err = stream.tsw.WritePackets(self.w, self.datav[:3], pkt.Time, true, false); err != nil {
        return
    }
```

### 🔧 Обробка H.264:

```go
case av.H264:
    codec := stream.CodecData.(h264parser.CodecData)
    
    // 1. Підготовка NALU: SPS+PPS для ключових кадрів
    nalus := self.nalus[:0]
    if pkt.IsKeyFrame {
        nalus = append(nalus, codec.SPS())
        nalus = append(nalus, codec.PPS())
    }
    
    // 2. Розбиття вхідних даних на NALU
    pktnalus, _ := h264parser.SplitNALUs(pkt.Data)
    for _, nalu := range pktnalus {
        nalus = append(nalus, nalu)
    }
    
    // 3. Підготовка datav з AUD/start codes
    datav := self.datav[:1]  // перший елемент зарезервовано для PES header
    for i, nalu := range nalus {
        if i == 0 {
            datav = append(datav, h264parser.AUDBytes)  // Access Unit Delimiter
        } else {
            datav = append(datav, h264parser.StartCodeBytes)  // 0x000001
        }
        datav = append(datav, nalu)
    }
    
    // 4. Генерація PES заголовку з PTS/DTS
    n := tsio.FillPESHeader(
        self.peshdr, 
        tsio.StreamIdH264,  // 0xE0
        -1,  // variable length для відео
        pkt.Time+pkt.CompositionTime,  // PTS
        pkt.Time)  // DTS
    datav[0] = self.peshdr[:n]
    
    // 5. Запис у TS
    if err = stream.tsw.WritePackets(self.w, datav, pkt.Time, pkt.IsKeyFrame, false); err != nil {
        return
    }
```

### 🔍 Чому `pkt.Time += time.Second`?

```
Це може бути:
1. Компенсація затримки буферизації у джерелі
2. Виправлення таймінгів якщо джерело надсилає відносні значення
3. Тимчасове рішення для синхронізації

⚠️ Увага: це може призвести до розсинхронізації якщо:
• Джерело вже надсилає абсолютні PTS
• Ви використовуєте цей муксер у каскаді з іншими обробниками

✅ Рекомендація: зробіть це налаштовуваним параметром або видаліть якщо не потрібно
```

### ✅ Ваш use-case: запис відео з коректними таймінгами

```go
// WriteVideoPacketWithCorrectTiming — запис відео без автоматичного зсуву часу
func (m *ts.Muxer) WriteVideoPacketWithCorrectTiming(pkt av.Packet) error {
    // Знаходимо потік
    stream, ok := m.streams[int(pkt.Idx)]
    if !ok {
        return fmt.Errorf("unknown stream index: %d", pkt.Idx)
    }
    
    // Тільки для H.264/H.265
    if stream.Type() != av.H264 && stream.Type() != av.H265 {
        return m.WritePacket(pkt)  // стандартна обробка
    }
    
    // Підготовка NALU (аналогічно до WritePacket)
    codec := stream.CodecData.(h264parser.CodecData)  // або h265parser
    nalus := m.nalus[:0]
    if pkt.IsKeyFrame {
        nalus = append(nalus, codec.SPS())
        nalus = append(nalus, codec.PPS())
        if stream.Type() == av.H265 {
            nalus = append(nalus, codec.VPS())
        }
    }
    pktnalus, _ := h264parser.SplitNALUs(pkt.Data)
    for _, nalu := range pktnalus {
        nalus = append(nalus, nalu)
    }
    
    // Підготовка datav
    datav := m.datav[:1]
    for i, nalu := range nalus {
        if i == 0 {
            if stream.Type() == av.H264 {
                datav = append(datav, h264parser.AUDBytes)
            } else {
                datav = append(datav, h265parser.AUDBytes)
            }
        } else {
            if stream.Type() == av.H264 {
                datav = append(datav, h264parser.StartCodeBytes)
            } else {
                datav = append(datav, h265parser.StartCodeBytes)
            }
        }
        datav = append(datav, nalu)
    }
    
    // PES header з оригінальними таймінгами (без += time.Second)
    pts := pkt.Time + pkt.CompositionTime
    dts := pkt.Time
    n := tsio.FillPESHeader(m.peshdr, tsio.StreamIdH264, -1, pts, dts)
    datav[0] = m.peshdr[:n]
    
    // Запис
    return stream.tsw.WritePackets(m.w, datav, pkt.Time, pkt.IsKeyFrame, false)
}
```

---

## 🔑 4. Padding та continuity counter

### 🔧 Метод `writePaddingTSPackets()`:

```go
func (self *Muxer) writePaddingTSPackets(streamW *Stream) (err error) {
    // Додавання порожніх TS пакетів поки continuity counter не стане 0
    for streamW.tsw.ContinuityCounter&0xf != 0x0 {
        header := tsio.TSHeader{
            PID:               uint(streamW.pid),
            ContinuityCounter: streamW.tsw.ContinuityCounter,
        }
        if _, err = tsio.WriteTSHeader(self.w, header, 0); err != nil {
            return
        }
        streamW.tsw.ContinuityCounter++
    }
    return
}
```

### 🔍 Навіщо це потрібно?

```
MPEG-TS вимагає, щоб continuity counter:
• Був 4-бітним лічильником (0-15) для кожного PID
• Інкрементувався після кожного пакету з даними
• Скидався на 0 після 15

Деякі плеєри/декодери вимагають, щоб потік закінчувався на counter=0
для коректної обробки кінця файлу.

PaddingToMakeCounterCont прапорець вмикає цю поведінку у WriteTrailer().
```

### ✅ Ваш use-case: коректне завершення потоку

```go
// WriteTrailerWithPadding — гарантоване завершення з вирівнюванням counter
func (m *ts.Muxer) WriteTrailerWithPadding() error {
    // Вмикаємо padding якщо потрібно
    m.PaddingToMakeCounterCont = true
    
    // Стандартний trailer
    if err := m.WriteTrailer(); err != nil {
        return err
    }
    
    // Додаткова перевірка: всі counters повинні бути 0
    for _, stream := range m.streams {
        if stream.tsw.ContinuityCounter&0xf != 0 {
            log.Printf("warning: stream PID 0x%X counter not aligned: %d", 
                stream.pid, stream.tsw.ContinuityCounter)
        }
    }
    
    return nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// hls_ts_muxer.go — TS муксер для генерації HLS сегментів
type HLSTSMuxer struct {
    channelID    string
    segmentDir   string
    muxer        *ts.Muxer
    segmentNum   int
    segmentStart time.Time
    metrics      *HLSSegmentMetrics
}

func NewHLSTSMuxer(channelID, segmentDir string) (*HLSTSMuxer, error) {
    return &HLSTSMuxer{
        channelID:  channelID,
        segmentDir: segmentDir,
        muxer:      nil,  // ініціалізується при старті сегменту
        metrics:    NewHLSSegmentMetrics(channelID),
    }, nil
}

// StartNewSegment — початок нового .ts сегменту
func (h *HLSTSMuxer) StartNewSegment(streams []av.CodecData) error {
    // 1. Генерація імені файлу
    filename := fmt.Sprintf("%s/segment_%04d.ts", h.segmentDir, h.segmentNum)
    f, err := os.Create(filename)
    if err != nil {
        return fmt.Errorf("create segment file: %w", err)
    }
    
    // 2. Ініціалізація муксера
    h.muxer = ts.NewMuxer(f)
    if err := h.muxer.WriteHeader(streams); err != nil {
        f.Close()
        return fmt.Errorf("write header: %w", err)
    }
    
    h.segmentStart = time.Now()
    h.metrics.SegmentsStarted.Inc()
    
    log.Printf("Channel %s: started segment %d", h.channelID, h.segmentNum)
    return nil
}

// WritePacket — запис пакету у поточний сегмент
func (h *HLSTSMuxer) WritePacket(pkt av.Packet) error {
    if h.muxer == nil {
        return fmt.Errorf("no active segment")
    }
    
    start := time.Now()
    if err := h.muxer.WritePacket(pkt); err != nil {
        h.metrics.WriteErrors.Inc()
        return err
    }
    
    h.metrics.PacketsWritten.Inc()
    h.metrics.WriteLatency.Observe(time.Since(start).Seconds())
    return nil
}

// FinishSegment — завершення поточного сегменту
func (h *HLSTSMuxer) FinishSegment() error {
    if h.muxer == nil {
        return nil
    }
    
    // 1. Завершення запису
    if err := h.muxer.WriteTrailerWithPadding(); err != nil {
        return err
    }
    
    // 2. Закриття файлу
    if closer, ok := h.muxer.SetWriter(nil).(io.Closer); ok {
        closer.Close()
    }
    
    // 3. Оновлення метрик
    duration := time.Since(h.segmentStart)
    h.metrics.SegmentDuration.Observe(duration.Seconds())
    h.metrics.SegmentsCompleted.Inc()
    
    log.Printf("Channel %s: finished segment %d, duration=%v", 
        h.channelID, h.segmentNum, duration)
    
    h.segmentNum++
    h.muxer = nil
    return nil
}

// ShouldStartNewSegment — логіка вирішення про початок нового сегменту
func (h *HLSTSMuxer) ShouldStartNewSegment(lastKeyFrameTime time.Duration, currentKeyFrame bool) bool {
    // Умови для нового сегменту:
    // 1. Це ключовий кадр
    // 2. Пройшло >= 10 секунд від останнього ключового кадру
    // 3. Є активний сегмент для завершення
    
    if !currentKeyFrame {
        return false
    }
    
    if h.muxer != nil && time.Since(h.segmentStart) >= 10*time.Second {
        return true
    }
    
    return false
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"codec type=X is not supported"** | Спроба запису непідтримуваного кодека | Переконайтеся, що передаєте тільки H.264/H.265/AAC; додайте підтримку нових кодеків у `CodecTypes` |
| **"ts: pmt too large"** | PMT не поміщається у буфер `psidata` | Збільшіть розмір буфера у `NewMuxer()`; перевірте чи не занадто багато потоків/дескрипторів |
| **Розсинхронізація таймінгів** | Аудіо відстає/випереджає відео | Перевірте чи `pkt.Time += time.Second` не ламає абсолютні таймінги; використовуйте `CompositionTime` коректно |
| **Continuity counter помилки** | Плеєр скаржиться на розриви | Увімкніть `PaddingToMakeCounterCont = true`; переконайтеся, що не пропускаєте пакети при записі |
| **SPS/PPS не відправляються** | Ключові кадри не декодуються | Переконайтеся, що `pkt.IsKeyFrame` встановлено коректно; SPS/PPS мають бути у `CodecData` |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування буферів для множинних сегментів:

```go
// TSMuxerPool — пул муксерів для уникнення аллокацій
var TSMuxerPool = sync.Pool{
    New: func() interface{} {
        return ts.NewMuxer(nil)  // writer встановлюється пізніше
    },
}

func GetTSMuxer(w io.Writer) *ts.Muxer {
    m := TSMuxerPool.Get().(*ts.Muxer)
    m.SetWriter(w)
    return m
}

func PutTSMuxer(m *ts.Muxer) {
    m.SetWriter(nil)  // очищення writer
    TSMuxerPool.Put(m)
}

// Використання у циклі сегментів:
muxer := GetTSMuxer(segmentFile)
defer PutTSMuxer(muxer)
// ... використання ...
```

### 2. Пакетний запис для зменшення системних викликів:

```go
// BatchWritePackets — запис кількох пакетів за один виклик
func (m *ts.Muxer) BatchWritePackets(packets []av.Packet) error {
    for _, pkt := range packets {
        if err := m.WritePacket(pkt); err != nil {
            return err
        }
    }
    return nil
}
```

### 3. Моніторинг продуктивності муксингу:

```go
type TSMuxerMetrics struct {
    PacketsWritten prometheus.CounterVec
    WriteLatency   prometheus.HistogramVec
    PSIWriteCount  prometheus.CounterVec
    PaddingPackets prometheus.CounterVec
}

func (m *TSMuxerMetrics) RecordPacket(streamType av.CodecType, duration time.Duration, channelID string) {
    m.PacketsWritten.WithLabelValues(streamType.String(), channelID).Inc()
    m.WriteLatency.WithLabelValues(channelID).Observe(duration.Seconds())
}

func (m *TSMuxerMetrics) RecordPSIWrite(channelID string) {
    m.PSIWriteCount.WithLabelValues(channelID).Inc()
}
```

---

## 📋 Чек-лист інтеграції ts.Muxer

```go
// ✅ 1. Валідація потоків перед ініціалізацією
if err := ValidateStreamsForTS(streams); err != nil { /* handle */ }

// ✅ 2. Створення муксера з попередньо виділеними буферами
muxer := ts.NewMuxer(writer)  // буфери вже виділені у конструкторі

// ✅ 3. Запис заголовка з PAT/PMT
if err := muxer.WriteHeader(streams); err != nil { /* handle */ }

// ✅ 4. Запис пакетів з коректними таймінгами
// Увага: WritePacket() додає += time.Second, можливо потрібно обійти
pkt.Time = originalPTS  // переконатися що таймінги правильні
if err := muxer.WritePacket(pkt); err != nil { /* handle */ }

// ✅ 5. Періодична відправка PAT/PMT для стрімінгу
if time.Since(lastPSI) >= 500*time.Millisecond {
    muxer.WritePATPMT()  // повторна відправка таблиць
    lastPSI = time.Now()
}

// ✅ 6. Коректне завершення з padding якщо потрібно
muxer.PaddingToMakeCounterCont = true
if err := muxer.WriteTrailer(); err != nil { /* handle */ }

// ✅ 7. Метрики для моніторингу
metrics.RecordPacket(streamType, time.Since(start), channelID)
```

---

## 🔗 Корисні посилання

- 💻 [vdk ts Package](https://pkg.go.dev/github.com/deepch/vdk/format/ts) — GoDoc documentation
- 💻 [vdk tsio Package](https://pkg.go.dev/github.com/deepch/vdk/format/ts/tsio) — низькорівневі TS утиліти
- 📄 [MPEG-TS Packet Structure](https://en.wikipedia.org/wiki/MPEG_transport_stream#Packet) — візуальна схема
- 📄 [H.264 in TS](https://wiki.multimedia.cx/index.php/H.264_in_MPEG-TS) — особливості кодування
- 📄 [HLS TS Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до сегментів
- 🧪 [Go io.Writer Documentation](https://pkg.go.dev/io#Writer) — інтерфейси для запису

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **генерацією HLS сегментів у реальному часі**:
> 1. **Періодично відправляйте PAT/PMT** (кожні 500ms-1s) — це критично для плеєрів, що підключаються до потоку в середині.
> 2. **Уникайте автоматичного `+= time.Second`** — це може зламати синхронізацію; зробіть це налаштовуваним або видаліть.
> 3. **Кешуйте буфери через `sync.Pool`** — це значно зменшує навантаження на GC при генерації сотень сегментів.
> 4. **Валідуйте `IsKeyFrame` перед записом** — SPS/PPS відправляються тільки для ключових кадрів; неправильне встановлення зламає декодування.
> 5. **Моніторьте `WriteLatency`** — різке зростання може вказувати на повільний диск або мережу, що призведе до переповнення буферів.

Потрібен приклад інтеграції `HLSTSMuxer` з вашим `pubsub.Queue` для розподілу вже записаних .ts сегментів між підписниками (HLS playlist updater, CDN uploader, архів)? Готовий допомогти! 🚀