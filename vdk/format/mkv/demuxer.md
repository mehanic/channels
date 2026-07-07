# 📦 Глибокий розбір: `mkv.Demuxer` — Читання MKV/WebM контейнерів

Цей файл — **реалізація демуксера для формату Matroska/WebM**, що перетворює бінарну структуру контейнера у послідовність `av.Packet` для подальшої обробки. Він підтримує H.264 відео, парсинг SPS/PPS для ініціалізації кодека, та витягування таймінгів з SimpleBlock елементів.

---

## 🗺️ Архітектурна схема mkv.Demuxer

```
┌────────────────────────────────────────┐
│ 📦 mkv.Demuxer — MKV/WebM Reader      │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Demuxer — основний контролер        │
│  • probe() — пошук та парсинг кодека   │
│  • ReadPacket() — читання пакетів      │
│  • Stream — представлення треку        │
│                                         │
│  🔄 Потік даних:                        │
│  io.Reader → mkvio.Document            │
│  → ParseElement() → av.Packet          │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (SPS/PPS парсинг)     │
│  • Контейнер: MKV/WebM (EBML формат)  │
│  • Елементи: SimpleBlock, CodecPrivate│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Demuxer — основна структура

### Поля та їх призначення:

```go
type Demuxer struct {
    r       *mkvio.Document  // EBML документ для парсингу
    pkts    []av.Packet      // буфер пакетів (не використовується!)
    sps     []byte           // SPS NALU для H.264 ініціалізації
    pps     []byte           // PPS NALU для H.264 ініціалізації
    streams []*Stream        // масив треків (тільки один у цій реалізації)
    ps      uint32           // previous timestamp для розрахунку duration
    stage   int              // стан парсингу (0=probe, 1=ready)
    fc      int              // frame counter (не використовується)
    ls      time.Duration    // last timestamp / accumulated time
}
```

### 🔧 NewDemuxer() — ініціалізація:

```go
func NewDemuxer(r io.Reader) *Demuxer {
    return &Demuxer{
        r: mkvio.InitDocument(r),
    }
}
```

**✅ Ваш use-case**: відкриття файлу з перевіркою

```go
// OpenMKVFile — безпечне відкриття з валідацією
func OpenMKVFile(filename string) (*mkv.Demuxer, error) {
    f, err := os.Open(filename)
    if err != nil {
        return nil, fmt.Errorf("open file: %w", err)
    }
    
    demuxer := mkv.NewDemuxer(f)
    
    // Попередня перевірка чи файл валідний
    if _, err := demuxer.Streams(); err != nil {
        f.Close()
        return nil, fmt.Errorf("invalid MKV: %w", err)
    }
    
    return demuxer, nil
}
```

---

## 🔑 2. probe() — пошук та ініціалізація кодека

### 🔧 Основна логіка:

```go
func (self *Demuxer) probe() (err error) {
    if self.stage == 0 {  // тільки один раз
        // 1. Пошук CodecPrivate елемента
        var el *mkvio.Element
        el, err = self.r.GetVideoCodec()
        if err != nil {
            return
        }
        
        // 2. Парсинг H.264 codec initialization data
        if el.ElementRegister.ID == mkvio.ElementCodecPrivate.ID {
            payload := el.Content[6:]  // ⚠️ Магічне зміщення: пропускає 6 байт заголовку
            var reader int
            for pos := 0; pos < len(payload); pos = reader {
                // Читання довжини NALU (2 байти, big-endian)
                lens := int(binary.BigEndian.Uint16(payload[reader:]))
                reader += 2
                
                // Читання самого NALU
                nal := payload[reader : reader+lens]
                naluType := nal[0] & 0x1f  // тип NALU у молодших 5 бітах
                
                // Збереження SPS/PPS для ініціалізації кодека
                switch naluType {
                case h264parser.NALU_SPS:
                    self.sps = nal
                case h264parser.NALU_PPS:
                    self.pps = nal
                }
                reader += lens
                reader++  // ⚠️ Додатковий інкремент: чи потрібен?
            }
        }
        
        // 3. Створення CodecData якщо знайдені SPS+PPS
        if len(self.sps) > 0 && len(self.pps) > 0 {
            var codec av.CodecData
            codec, err = h264parser.NewCodecDataFromSPSAndPPS(self.sps, self.pps)
            if err != nil {
                return
            }
            
            // 4. Створення треку
            stream := &Stream{}
            stream.idx = 0
            stream.demuxer = self
            stream.CodecData = codec
            self.streams = append(self.streams, stream)
        }
        self.stage++  // мітка: probe завершено
    }
    return
}
```

### ⚠️ Критичні проблеми у парсингу CodecPrivate:

#### ❌ 1. Магічне зміщення `payload := el.Content[6:]`

```
Проблема:
• Код пропускає перші 6 байт Content без пояснення
• Це може бути специфічно для певного формату, але не універсально

Наслідки:
• Якщо формат CodecPrivate зміниться → парсинг зламається
• Неможливо підтримувати інші кодеки (напр. VP8/VP9 у WebM)

✅ Виправлення: парсити за специфікацією AVCDecoderConfigurationRecord:
    // AVCDecoderConfigurationRecord format:
    // [0] configurationVersion (always 1)
    // [1] AVCProfileIndication
    // [2] profile_compatibility
    // [3] AVCLevelIndication
    // [4] lengthSizeMinusOne (usually 3 = 4-byte NALU lengths)
    // [5] numOfSequenceParameterSets (usually 1)
    // [6-7] sequenceParameterSetLength
    // [8...] sequenceParameterSetNALUnit
    
    if len(el.Content) < 7 {
        return fmt.Errorf("CodecPrivate too short")
    }
    
    configVersion := el.Content[0]
    if configVersion != 1 {
        return fmt.Errorf("unsupported AVC config version: %d", configVersion)
    }
    
    // Пропуск заголовку до SPS
    offset := 6  // configurationVersion + profile/level + lengthSize + numSPS
    if len(el.Content) < offset+2 {
        return fmt.Errorf("insufficient data for SPS length")
    }
    
    spsLen := int(binary.BigEndian.Uint16(el.Content[offset:]))
    offset += 2
    if len(el.Content) < offset+spsLen {
        return fmt.Errorf("SPS data truncated")
    }
    self.sps = el.Content[offset : offset+spsLen]
    offset += spsLen
    
    // Аналогічно для PPS...
```

#### ❌ 2. Зайвий `reader++` після читання NALU

```go
reader += lens  // пропуск даних NALU
reader++        // ← Чи потрібен цей інкремент?
```

**Проблема**: Якщо `lens` вже включає всі байти NALU, додатковий `reader++` пропустить один байт → зсув парсингу.

**✅ Виправлення**: Видалити зайвий інкремент або додати коментар:

```go
reader += lens
// reader++  // видалено: lens вже включає всі байти
// АБО, якщо це потрібно для певного формату:
// reader++  // пропускаємо роздільник між NALU (якщо є)
```

#### ❌ 3. Тільки H.264 підтримка

```
Поточна реалізація працює тільки з H.264:
• Шукає SPS/PPS у CodecPrivate
• Використовує h264parser для створення CodecData

Наслідки:
• WebM файли з VP8/VP9 не підтримуються
• MKV з іншими кодеками (AAC, VP9, AV1) не працюватимуть

✅ Розширення для підтримки інших кодеків:
    switch codecID {
    case "V_MPEG4/ISO/AVC":  // H.264
        // поточна логіка...
    case "V_VP8", "V_VP9":   // VP8/VP9
        // парсинг VP CodecPrivate...
    case "V_AV1":            // AV1
        // парсинг AV1 CodecPrivate...
    default:
        return fmt.Errorf("unsupported video codec: %s", codecID)
    }
```

---

## 🔑 3. ReadPacket() — читання медіа-пакетів

### 🔧 Основна логіка:

```go
func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
    var el mkvio.Element
    
    for {
        // 1. Парсинг наступного елемента
        el, err = self.r.ParseElement()
        if err != nil {
            return
        }
        
        // 2. Фільтрація: тільки SimpleBlock елементи типу Binary (6)
        if el.Type == 6 && el.ElementRegister.ID == mkvio.ElementSimpleBlock.ID {
            self.fc++  // frame counter (не використовується далі)
            
            // 3. Розбиття даних на NALU
            nals, _ := h264parser.SplitNALUs(el.Content[4:])  // ⚠️ Магічне зміщення: 4 байти
            
            for _, nal := range nals {
                naluType := nal[0] & 0x1f  // тип у молодших 5 бітах
                
                // 4. Обробка ключових кадрів (NALU type 5 = IDR)
                if naluType == 5 {
                    l1 := int(binary.BigEndian.Uint16(el.Content[2:4]))  // ⚠️ Магічне читання
                    dur := time.Duration(uint32(l1)) * time.Millisecond
                    self.ls += dur  // накопичення часу
                    self.ps = 0     // скидання попереднього timestamp
                    
                    pkt = av.Packet{
                        IsKeyFrame: true,
                        Idx: 0,
                        Duration: dur,
                        Time: self.ls,
                        Data: append(binSize(len(nal)), nal...),  // додавання 4-байтового розміру
                    }
                    return
                }
                
                // 5. Обробка звичайних кадрів (NALU type 1 = non-IDR)
                else if naluType == 1 {
                    l1 := int(binary.BigEndian.Uint16(el.Content[1:3]))  // ⚠️ Інше зміщення!
                    dur := time.Duration(uint32(l1)-self.ps) * time.Millisecond
                    self.ls += dur
                    self.ps = uint32(l1)  // збереження для наступного розрахунку
                    
                    pkt = av.Packet{
                        Idx: 0,
                        Duration: dur,
                        Time: self.ls,
                        Data: append(binSize(len(nal)), nal...),
                    }
                    return
                }
            }
        }
    }
    return
}
```

### ⚠️ Критичні проблеми у ReadPacket():

#### ❌ 1. Магічні зміщення у Content

```go
nals, _ := h264parser.SplitNALUs(el.Content[4:])  // пропускає 4 байти
l1 := int(binary.BigEndian.Uint16(el.Content[2:4]))  // читає байти 2-3
l1 := int(binary.BigEndian.Uint16(el.Content[1:3]))  // читає байти 1-2
```

**Проблема**: Формат SimpleBlock у WebM/Matroska:

```
SimpleBlock structure:
  [0] TrackNumber (variable-length, but usually 1 byte)
  [1-2] Timecode (relative to Cluster, 2 bytes, signed)
  [3] Flags (1 byte: keyframe, invisible, etc.)
  [4...] NALU data (with length prefixes)

Поточний код припускає фіксовані зміщення, що не завжди вірно:
• TrackNumber може бути 1-4 байти (variable-length)
• Timecode завжди 2 байти, але позиція залежить від TrackNumber
```

**✅ Виправлення**: парсити SimpleBlock за специфікацією:

```go
// ParseSimpleBlock — коректний парсинг SimpleBlock елемента
func ParseSimpleBlock(content []byte) (trackNum uint64, timecode int16, flags byte, data []byte, err error) {
    if len(content) < 4 {
        return 0, 0, 0, nil, fmt.Errorf("SimpleBlock too short")
    }
    
    // 1. Читання TrackNumber (variable-length)
    var tnLen int
    trackNum, tnLen, err = mkvio.DecodeEBMLInteger(content)
    if err != nil {
        return 0, 0, 0, nil, fmt.Errorf("parse TrackNumber: %w", err)
    }
    
    offset := tnLen
    if len(content) < offset+3 {
        return 0, 0, 0, nil, fmt.Errorf("insufficient data for SimpleBlock header")
    }
    
    // 2. Читання Timecode (2 байти, signed, big-endian)
    timecode = int16(binary.BigEndian.Uint16(content[offset:]))
    offset += 2
    
    // 3. Читання Flags
    flags = content[offset]
    offset++
    
    // 4. Решта — дані
    data = content[offset:]
    
    return trackNum, timecode, flags, data, nil
}
```

#### ❌ 2. Ігнорування помилок у SplitNALUs

```go
nals, _ := h264parser.SplitNALUs(el.Content[4:])  // ← помилка ігнорується!
```

**Наслідки**: Якщо парсинг NALU не вдасться → порожній список nals → функція продовжує цикл без повернення пакету → нескінченний цикл або затримка.

**✅ Виправлення**: обробляти помилки:

```go
nals, err := h264parser.SplitNALUs(data)
if err != nil {
    log.Printf("warning: failed to split NALUs: %v", err)
    continue  // пропустити цей елемент
}
if len(nals) == 0 {
    continue  // немає даних для обробки
}
```

#### ❌ 3. Розрахунок duration тільки для типів 1 та 5

```go
if naluType == 5 { /* IDR frame */ }
else if naluType == 1 { /* non-IDR */ }
// Інші типи (6=SEI, 7=SPS, 8=PPS, тощо) ігноруються
```

**Проблема**: Якщо файл містить інші типи NALU → вони пропускаються, але таймінги не оновлюються → розсинхронізація.

**✅ Виправлення**: оновлювати таймінги для всіх NALU:

```go
// Загальна обробка для будь-якого NALU
l1 := int(binary.BigEndian.Uint16(el.Content[timecodeOffset:]))  // коректне зміщення
dur := calculateDuration(l1, self.ps)
self.ls += dur
self.ps = uint32(l1)

// Специфічна обробка для ключових кадрів
if naluType == 5 {
    pkt.IsKeyFrame = true
}

pkt = av.Packet{
    Idx: 0,
    Duration: dur,
    Time: self.ls,
    Data: append(binSize(len(nal)), nal...),
    IsKeyFrame: naluType == 5,  // або інша логіка визначення ключового кадру
}
return
```

---

## 🔑 4. binSize() — helper для формату даних

### 🔧 Реалізація:

```go
func binSize(val int) []byte {
    buf := make([]byte, 4)
    binary.BigEndian.PutUint32(buf, uint32(val))
    return buf
}
```

### 🔍 Призначення:
- Додає 4-байтовий префікс розміру до даних NALU
- Формат: `[4-byte big-endian size][NALU data]`
- Використовується для сумісності з `av.Packet.Data` форматом

### ✅ Ваш use-case**: підготовка даних для декодера

```go
// PrepareNALUForDecoder — додавання size prefix для H.264 декодера
func PrepareNALUForDecoder(nal []byte) []byte {
    // Деякі декодери очікують формат: [size][NALU]
    sizeBuf := make([]byte, 4)
    binary.BigEndian.PutUint32(sizeBuf, uint32(len(nal)))
    return append(sizeBuf, nal...)
}

// Альтернатива: якщо декодер приймає "annex B" формат (0x00000001 prefix)
func PrepareNALUAnnexB(nal []byte) []byte {
    prefix := []byte{0x00, 0x00, 0x00, 0x01}
    return append(prefix, nal...)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Транскодування MKV → HLS

```go
// TranscodeMKVToHLS — конвертація локального MKV у HLS сегменти
func TranscodeMKVToHLS(inputFile, outputDir string, segmentDuration time.Duration) error {
    // 1. Відкриття вхідного файлу
    demuxer, err := mkv.OpenMKVFile(inputFile)
    if err != nil {
        return fmt.Errorf("open input: %w", err)
    }
    defer demuxer.Close()
    
    // 2. Отримання метаданих
    streams, err := demuxer.Streams()
    if err != nil {
        return fmt.Errorf("probe streams: %w", err)
    }
    
    // 3. Ініціалізація HLS муксера
    hlsMuxer, err := hls.NewMuxer(outputDir, streams)
    if err != nil {
        return fmt.Errorf("init HLS: %w", err)
    }
    
    // 4. Основний цикл транскодування
    var segmentStart time.Duration
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF { break }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Запис у HLS
        if err := hlsMuxer.WritePacket(pkt); err != nil {
            return fmt.Errorf("write HLS: %w", err)
        }
        
        // Ротація сегменту за часом
        if pkt.Time-segmentStart >= segmentDuration {
            if err := hlsMuxer.StartNewSegment(); err != nil {
                return fmt.Errorf("new segment: %w", err)
            }
            segmentStart = pkt.Time
        }
    }
    
    // 5. Фіналізація
    return hlsMuxer.WriteTrailer()
}
```

### 🔧 Приклад: Відео-прев'ю з seek

```go
// GenerateVideoPreview — створення прев'ю з довільного моменту
func GenerateVideoPreview(inputFile string, previewTime time.Duration, duration time.Duration) ([]byte, error) {
    demuxer, err := mkv.OpenMKVFile(inputFile)
    if err != nil { return nil, err }
    defer demuxer.Close()
    
    // ⚠️ Поточна реалізація не підтримує seek!
    // Потрібно читати послідовно до потрібного часу
    var previewData bytes.Buffer
    endTime := previewTime.Add(duration)
    
    for {
        pkt, err := demuxer.ReadPacket()
        if err == io.EOF || pkt.Time.After(endTime) { break }
        if err != nil { return nil, err }
        
        // Копіювання даних у буфер (спрощено)
        previewData.Write(pkt.Data)
    }
    
    return previewData.Bytes(), nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"streams not found"** | probe() не знаходить CodecPrivate | Перевірте чи файл містить H.264 відео; додайте підтримку інших кодеків |
| **Паніка при доступі до Content** | el.Content[6:] при короткому Content | Додайте перевірку `if len(el.Content) < 7` перед доступом |
| **Розсинхронізація таймінгів** | Duration розраховується тільки для типів 1/5 | Оновлюйте таймінги для всіх NALU, не тільки для відео кадрів |
| **Нескінченний цикл у ReadPacket** | SplitNALUs повертає помилку, але вона ігнорується | Обробляйте помилки від SplitNALUs та інших функцій |
| **Невірне читання SimpleBlock** | Магічні зміщення не враховують variable-length TrackNumber | Парсити SimpleBlock за специфікацією: TrackNumber → Timecode → Flags → Data |

---

## ⚡ Оптимізації для великих файлів

### 1. Кешування розпарсених NALU:

```go
type NALUCache struct {
    mu    sync.RWMutex
    cache map[uint32][]byte  // hash(NALU) → NALU data
}

func (c *NALUCache) Get(key uint32) ([]byte, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    nal, ok := c.cache[key]
    return nal, ok
}

func (c *NALUCache) Set(key uint32, nal []byte) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.cache == nil {
        c.cache = make(map[uint32][]byte)
    }
    c.cache[key] = nal
}
```

### 2. Пакетне читання елементів:

```go
// ReadPacketsBatch — читання кількох пакетів за один виклик
func (d *Demuxer) ReadPacketsBatch(count int) ([]av.Packet, error) {
    pkts := make([]av.Packet, 0, count)
    
    for len(pkts) < count {
        pkt, err := d.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return pkts, err
        }
        pkts = append(pkts, pkt)
    }
    
    return pkts, nil
}
```

### 3. Моніторинг продуктивності демуксингу:

```go
type DemuxerMetrics struct {
    PacketReadLatency prometheus.HistogramVec
    NALUParseErrors   prometheus.CounterVec
    FrameTypes        prometheus.CounterVec
}

func (m *DemuxerMetrics) RecordPacket(naluType byte, duration time.Duration, err error) {
    if err != nil {
        m.NALUParseErrors.Inc()
        return
    }
    m.FrameTypes.WithLabelValues(fmt.Sprintf("type_%d", naluType)).Inc()
}
```

---

## 📋 Чек-лист використання mkv.Demuxer

```go
// ✅ 1. Відкриття файлу з перевіркою
demuxer, err := mkv.OpenMKVFile("video.mkv")
if err != nil { /* handle error */ }
defer demuxer.Close()

// ✅ 2. Отримання метаданих перед читанням
streams, err := demuxer.Streams()
if err != nil { /* handle error */ }

// ✅ 3. Обробка помилок читання з відновленням
for {
    pkt, err := demuxer.ReadPacket()
    if err == io.EOF {
        break
    }
    if err != nil {
        log.Printf("read error: %v, attempting recovery", err)
        // логіка відновлення...
        continue
    }
    // обробка пакету...
}

// ✅ 4. Перевірка IsKeyFrame для seek точок
if pkt.IsKeyFrame {
    // можна почати новий сегмент або seek точку
}

// ✅ 5. Синхронізація за Time полем
if pkt.Time < lastAudioTime {
    // відео відстає — можна пропустити кадр або буферизувати
}

// ✅ 6. Метрики для моніторингу
metrics.RecordPacket(naluType, pkt.Duration, err)
```

---

## 🔗 Корисні посилання

- 💻 [vdk mkv Package](https://pkg.go.dev/github.com/deepch/vdk/format/mkv) — GoDoc documentation
- 📄 [Matroska Specification](https://matroska.org/technical/specs/index.html) — офіційна специфікація формату
- 📄 [H.264/AVC NALU Structure](https://wiki.videolan.org/NAL/) — структура Network Abstraction Layer Units
- 🧪 [Go encoding/binary Package](https://pkg.go.dev/encoding/binary) — робота з бінарними даними
- 📦 [Prometheus Metrics for Media](https://prometheus.io/docs/practices/instrumentation/) — моніторинг продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте довжину Content перед доступом** — уникнення панік при коротких елементах.
> 2. **Парсити SimpleBlock за специфікацією** — variable-length TrackNumber критичний для коректного читання.
> 3. **Обробляти помилки від SplitNALUs** — ігнорування призводить до нескінченних циклів або втрати даних.
> 4. **Оновлювати таймінги для всіх NALU** — забезпечення синхронізації навіть при пропуску кадрів.
> 5. **Додати підтримку інших кодеків** — VP8/VP9/AV1 для повної сумісності з WebM.

Потрібен приклад реалізації seek функціоналу для mkv.Demuxer, або інтеграція з вашим `mse.Muxer` для стрімінгу MKV через WebSocket? Готовий допомогти! 🚀