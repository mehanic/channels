# 📦 Глибокий розбір: `ts.Stream` — Елементарний потік у MPEG-TS

Цей файл містить **структуру `Stream`**, яка є основним будівельним блоком для обробки окремих медіа-потоків (відео/аудіо) у межах MPEG Transport Stream у бібліотеці `vdk`. Вона використовується як демуксером, так і муксером.

---

## 🗺️ Архітектурна схема

```
┌─────────────────────────────────────┐
│ 📦 ts.Stream — Elementary Stream    │
├─────────────────────────────────────┤
│                                      │
│  🔹 Наслідує: av.CodecData          │
│     • Type(), SampleRate(), etc.    │
│                                      │
│  🔹 Контекст:                        │
│     • demuxer *Demuxer ← читання    │
│     • muxer   *Muxer   ← запис      │
│                                      │
│  🔹 Ідентифікація:                   │
│     • pid        uint16  (0x100+)   │
│     • streamId   uint8   (0xE0/0xC0)│
│     • streamType uint8   (0x1B/0x0F)│
│                                      │
│  🔹 Стан таймінгів:                  │
│     • pts, dts, pt time.Duration    │
│     • iskeyframe bool               │
│     • fps uint                      │
│                                      │
│  🔹 Буферизація (demux):            │
│     • data []byte, datalen int      │
│                                      │
│  🔹 Серіалізація (mux):             │
│     • tsw *tsio.TSWriter            │
│                                      │
└─────────────────────────────────────┘
```

---

## 🔑 Детальний опис полів

### 1. Вбудований інтерфейс `av.CodecData`

```go
av.CodecData  // вбудоване поле
```

**Призначення**: надає доступ до метаданих кодека через уніфікований інтерфейс:

```go
// Методи інтерфейсу:
Type() av.CodecType           // av.H264, av.AAC, тощо
SampleRate() int              // для аудіо: 48000, 44100...
ChannelLayout() av.ChannelLayout  // CH_MONO, CH_STEREO...
SampleFormat() av.SampleFormat    // S16, FLTP...

// Для відео (av.VideoCodecData):
Width() int, Height() int, FPS() int

// Для AAC (aacparser.CodecData):
Config() MPEG4AudioConfig, MPEG4AudioConfigBytes() []byte

// Для H.264 (h264parser.CodecData):
SPS() []byte, PPS() []byte, AVCDecoderConfRecordBytes() []byte
```

**✅ Ваш use-case**: тип-асерція для доступу до специфічних полів

```go
// GetCodecSpecificInfo — отримання деталей кодека
func GetCodecSpecificInfo(stream *ts.Stream) (info map[string]interface{}, err error) {
    info = make(map[string]interface{})
    
    switch cd := stream.CodecData.(type) {
    case h264parser.CodecData:
        info["codec"] = "H.264"
        info["width"] = cd.Width()
        info["height"] = cd.Height()
        info["fps"] = cd.FPS()
        info["profile"] = cd.Tag()  // напр. "avc1.640028"
        
    case h265parser.CodecData:
        info["codec"] = "H.265"
        info["width"] = cd.Width()
        info["height"] = cd.Height()
        
    case aacparser.CodecData:
        info["codec"] = "AAC"
        info["sampleRate"] = cd.SampleRate()
        info["channels"] = cd.ChannelLayout().Count()
        info["objectType"] = cd.Config.ObjectType
        
    default:
        return nil, fmt.Errorf("unsupported codec type: %T", cd)
    }
    
    return info, nil
}
```

---

### 2. Контекстні посилання

```go
demuxer *Demuxer  // тільки для читання: nil у muxer режимі
muxer   *Muxer    // тільки для запису: nil у demuxer режимі
```

**Призначення**: дозволяє потоку взаємодіяти з батьківським об'єктом:
- У **demuxer**: `stream.demuxer.pkts` — черга для додавання готових `av.Packet`
- У **muxer**: `stream.muxer.w` — `io.Writer` для запису сирих байт

**⚠️ Важливо**: ці поля взаємовиключні — завжди тільки одне встановлено.

---

### 3. Ідентифікація потоку

| Поле | Тип | Опис | Приклад |
|------|-----|------|---------|
| `pid` | `uint16` | Packet ID у TS (13 біт) | `0x100` (256), `0x101` (257) |
| `streamId` | `uint8` | PES stream ID | `0xE0` (відео), `0xC0` (аудіо) |
| `streamType` | `uint8` | Тип згідно MPEG-TS | `0x1B`=H.264, `0x24`=H.265, `0x0F`=AAC |

**🔍 PID розподіл у vdk**:
```go
// З newStream() у Muxer:
pid := uint16(idx + 0x100)  // індекс 0 → PID 0x100, індекс 1 → 0x101

// Стандартні PID:
const (
    PAT_PID = 0      // Program Association Table
    PMT_PID = 0x1000 // Program Map Table
    // Елементарні потоки: 0x100, 0x101, 0x102...
)
```

**✅ Ваш use-case**: фільтрація пакетів за PID

```go
// FilterStreamByPID — отримання потоку за PID
func FilterStreamByPID(streams []*ts.Stream, targetPID uint16) *ts.Stream {
    for _, s := range streams {
        if s.pid == targetPID {
            return s
        }
    }
    return nil
}

// Використання при обробці TS пакету:
pid, _, _, _, _ := tsio.ParseTSHeader(header)
if stream := FilterStreamByPID(demuxer.streams, pid); stream != nil {
    // Обробка цього потоку
    stream.handleTSPacket(start, iskeyframe, payload)
}
```

---

### 4. TS Writer (тільки для muxer)

```go
tsw *tsio.TSWriter  // серіалізатор для цього PID
```

**Призначення**: перетворює `av.Packet` → PES → TS пакети (188 байт).

**🔧 Ключові можливості `tsio.TSWriter`**:
- Автоматичне управління `continuity_counter` (4-бітний лічильник)
- Додавання адаптаційного поля з PCR при потребі
- Padding до 188 байт
- Scatter-gather запис для ефективності

**✅ Ваш use-case**: створення потоку у muxer

```go
// newStream — реєстрація нового потоку у Muxer
func (m *Muxer) newStream(idx int, codec av.CodecData) error {
    pid := uint16(idx + 0x100)
    
    stream := &Stream{
        muxer:     m,              // посилання на батька
        CodecData: codec,          // метадані
        pid:       pid,            // унікальний PID
        tsw:       tsio.NewTSWriter(pid),  // окремий writer для цього PID
    }
    m.streams[idx] = stream
    return nil
}
```

---

### 5. Стан таймінгів

```go
fps          uint            // FPS для відео (з SPS), для розрахунку duration
iskeyframe   bool            // прапорець ключового кадру (IDR для H.264)
pts, dts, pt time.Duration   // Presentation/Decoding Time, previous timestamp
```

**🔍 Як працює розрахунок `Duration`**:

```go
// З addPacket() у Demuxer:
func (s *Stream) addPacket(payload []byte, timedelta, fixed time.Duration) {
    dts := s.dts
    if dts == 0 { dts = s.pts }  // fallback якщо DTS відсутній
    
    var dur time.Duration
    if s.pt > 0 {
        // Розрахунок з різниці: поточний - попередній
        dur = dts + timedelta - s.pt
    } else {
        // Fallback на фіксоване значення (напр. з FPS)
        dur = fixed
    }
    
    s.pt = dts + timedelta  // оновлення для наступного пакету
    
    // Створення av.Packet з розрахованим Duration...
}
```

**🔢 Приклад для AAC @ 48kHz**:
```
AAC-LC: 1024 семпли/фрейм → 1024/48000 = 21.333... ms

Фрейм 1: pts=0, pt=0 → dur = fixed = 21.333ms, pt оновлюється на 0
Фрейм 2: pts=21.333ms, pt=0 → dur = 21.333 - 0 = 21.333ms, pt = 21.333ms
Фрейм 3: pts=42.666ms, pt=21.333ms → dur = 42.666 - 21.333 = 21.333ms
```

**✅ Ваш use-case**: синхронізація аудіо/відео

```go
// SyncAVStreams — корекція розсинхронізації
func SyncAVStreams(video, audio *ts.Stream, maxDrift time.Duration) error {
    if video.pt == 0 || audio.pt == 0 {
        return nil  // недостатньо даних
    }
    
    drift := video.pt - audio.pt
    if abs(drift) > maxDrift {
        log.Printf("A/V drift: %v (max: %v)", drift, maxDrift)
        // У реальній системі: ресемплінг аудіо або буферизація відео
        return fmt.Errorf("sync adjustment needed")
    }
    return nil
}

func abs(d time.Duration) time.Duration {
    if d < 0 { return -d }
    return d
}
```

---

### 6. Буферизація даних (тільки для demuxer)

```go
data    []byte  // накопичені дані поточного PES пакету
datalen int     // очікувана довжина (0 = variable length)
```

**🔍 Як працює збірка PES у Demuxer**:

```
1. TS пакет приходить → handleTSPacket()
2. Якщо start=true (новий PES):
   • payloadEnd() завершує попередній PES
   • ParsePESHeader() витягує pts/dts/datalen
   • data = make([]byte, 0, datalen або 4096)
3. payload[hdrlen:] додається у data
4. Якщо start=false (продовження): просто append
5. Коли зібрано datalen байт (або кінець потоку) → payloadEnd()
```

**⚠️ Критичний момент**: `datalen == 0` для відео = variable length

```go
// MPEG-TS стандарт:
// • packet_length = 0 у PES заголовку → variable length (дозволено тільки для відео)
// • packet_length > 0 → фіксована довжина (аудіо, субтитри)

// У коді:
if self.datalen == 0 {
    self.data = make([]byte, 0, 4096)  // динамічний ріст
} else {
    self.data = make([]byte, 0, self.datalen)  // попереднє виділення
}
```

**✅ Ваш use-case**: обробка великих відео кадрів

```go
// HandleLargeNALU — валідація розміру перед обробкою
func (s *Stream) HandleLargeNALU(nalu []byte, maxBytes int) error {
    if len(nalu) > maxBytes {
        return fmt.Errorf("NALU too large: %d > %d bytes", len(nalu), maxBytes)
    }
    return nil
}

// Використання у payloadEnd() для H.264:
case tsio.ElementaryStreamTypeH264:
    nalus, _ := h264parser.SplitNALUs(payload)
    for _, nalu := range nalus {
        if err := stream.HandleLargeNALU(nalu, 4*1024*1024); err != nil {
            log.Printf("warning: skipping large NALU: %v", err)
            continue
        }
        // ... обробка ...
    }
```

---

## 🔄 Діаграма станів Stream

```
┌─────────────────────────────────────┐
│ 🔄 Життєвий цикл Stream             │
├─────────────────────────────────────┤
│                                      │
│  📥 DEMUXER режим:                  │
│  ┌─────────────────────────┐       │
│  │ 1. Ініціалізація:        │       │
│  │    • pid, streamType    │       │
│  │    • CodecData з PMT    │       │
│  ├─────────────────────────┤       │
│  │ 2. Обробка TS пакетів:   │       │
│  │    • handleTSPacket()   │       │
│  │    • накопичення у data │       │
│  ├─────────────────────────┤       │
│  │ 3. Завершення PES:       │       │
│  │    • payloadEnd()       │       │
│  │    • addPacket() → queue│       │
│  └─────────────────────────┘       │
│                                      │
│  📤 MUXER режим:                    │
│  ┌─────────────────────────┐       │
│  │ 1. Ініціалізація:        │       │
│  │    • pid, tsw, CodecData│       │
│  ├─────────────────────────┤       │
│  │ 2. Отримання av.Packet:  │       │
│  │    • WritePacket()      │       │
│  │    • FillPESHeader()    │       │
│  ├─────────────────────────┤       │
│  │ 3. Серіалізація:         │       │
│  │    • tsw.WritePackets() │       │
│  │    → io.Writer          │       │
│  └─────────────────────────┘       │
│                                      │
└─────────────────────────────────────┘
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **`data` буфер росте нескінченно** | `payloadEnd()` не викликається | Перевірте `start` прапорець у `handleTSPacket()`; додайте таймаут очищення |
| **`pt` не оновлюється** | `Duration = 0` у всіх пакетах | Переконайтеся, що `addPacket()` викликається; перевірте розрахунок `timedelta` |
| **`fps = 0` для відео** | `Duration` розраховується неправильно | Переконайтеся, що SPS парситься у `payloadEnd()`; встановіть дефолт (25/30) |
| **PID конфлікт** | Два потоки з однаковим `pid` | Переконайтеся, що PMT описує унікальні PID; перевірте `newStream()` логіку |
| **Type assertion fails** | `stream.CodecData.(h264parser.CodecData)` панікує | Переконайтеся, що `CodecData` ініціалізовано правильним типом, не `fake.CodecData` |

---

## ⚡ Оптимізації для real-time

### 1. Кешування буферів `data`:

```go
var streamBufferPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 0, 64*1024)  // 64KB початкова ємність
        return &buf
    },
}

func getStreamBuffer() *[]byte { return streamBufferPool.Get().(*[]byte) }
func putStreamBuffer(buf *[]byte) {
    *buf = (*buf)[:0]  // скидання без звільнення
    streamBufferPool.Put(buf)
}

// Використання у handleTSPacket():
if start {
    if s.data == nil {
        s.data = *getStreamBuffer()
    } else {
        s.data = s.data[:0]  // reuse
    }
}
```

### 2. Попередній розрахунок `fps`:

```go
// PrecomputeFPS — встановлення до початку обробки
func PrecomputeFPS(s *Stream, codec av.CodecData) {
    if cd, ok := codec.(h264parser.CodecData); ok && cd.FPS() > 0 {
        s.fps = uint(cd.FPS())
    } else if cd, ok := codec.(h265parser.CodecData); ok && cd.FPS() > 0 {
        s.fps = uint(cd.FPS())
    } else {
        s.fps = 25  // дефолт для CCTV
    }
}
```

### 3. Моніторинг стану:

```go
type StreamMetrics struct {
    PendingBytes prometheus.GaugeVec
    PacketCount  prometheus.CounterVec
    KeyFrames    prometheus.CounterVec
}

func (m *StreamMetrics) RecordState(s *Stream, channelID string) {
    m.PendingBytes.WithLabelValues(channelID).Set(float64(len(s.data)))
    if s.iskeyframe {
        m.KeyFrames.WithLabelValues(channelID).Inc()
    }
}
```

---

## 📋 Чек-лист використання

```go
// ✅ 1. Ініціалізація з коректними типами
stream := &Stream{
    CodecData: h264parser.CodecData{...},  // не fake.CodecData!
    pid:       0x101,
    streamType: tsio.ElementaryStreamTypeH264,
}

// ✅ 2. Встановлення fps до обробки
PrecomputeFPS(stream, codecData)

// ✅ 3. Моніторинг буфера у demuxer
if stream.data != nil && len(stream.data) > 1<<20 {  // >1MB
    log.Printf("warning: large buffer for PID 0x%X", stream.pid)
}

// ✅ 4. Валідація таймінгів
if pkt.Time < stream.pt {
    log.Printf("non-monotonic timestamp: %v < %v", pkt.Time, stream.pt)
}

// ✅ 5. Очищення ресурсів
if stream.data != nil {
    putStreamBuffer(&stream.data)
    stream.data = nil
}

// ✅ 6. Метрики
metrics.RecordState(stream, channelID)
```

---

## 🔗 Корисні посилання

- 💻 [vdk ts Package](https://pkg.go.dev/github.com/deepch/vdk/format/ts)
- 📄 [MPEG-TS Elementary Streams](https://en.wikipedia.org/wiki/MPEG_transport_stream#Elementary_streams)
- 📄 [PES Packet Format](https://wiki.multimedia.cx/index.php/PES)
- 🧪 [Go sync.Pool](https://pkg.go.dev/sync#Pool)

---

> 💡 **Ключові рекомендації**:
> 1. **Кешуйте `data` буфери** — зменшує GC навантаження при тисячах пакетів/сек.
> 2. **Встановлюйте `fps` заздалегідь** — уникнення помилок при відсутності SPS.
> 3. **Моніторьте розмір `data`** — різке зростання = завислий PES або помилка парсингу.
> 4. **Валідуйте монотонність таймінгів** — `pkt.Time < stream.pt` = розсинхронізація.
> 5. **Очищайте ресурси** — повернення буферів у пул уникнення витоку пам'яті.

Потрібен приклад інтеграції `Stream` з вашим `pubsub.Queue` для розподілу оброблених пакетів? Готовий допомогти! 🚀