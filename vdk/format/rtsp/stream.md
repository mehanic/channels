# 📦 Глибокий розбір: `rtsp.Stream` — Обробка окремого медіа-потоку в RTSP

Цей файл — **структура для представлення окремого медіа-потоку** (відео або аудіо) у межах RTSP сесії. Вона містить стан для демуксингу, буфери для фрагментованих NALU (H.264 FU-A), та метадані кодека.

---

## 🗺️ Архітектурна схема rtsp.Stream

```
┌────────────────────────────────────────┐
│ 📦 rtsp.Stream — Media Stream State   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • av.CodecData — метадані кодека      │
│  • sdp.Media — параметри з SDP         │
│  • client *Client — посилання на батька│
│                                         │
│  🎬 H.264 специфічний стан:            │
│  • fuStarted/fuBuffer — збірка FU-A    │
│  • sps/pps — параметр-сети             │
│  • spsChanged/ppsChanged — детекція змін│
│                                         │
│  ⏱️ Таймінги:                           │
│  • timestamp/firsttimestamp — RTP час  │
│  • lasttime — останній ав. пакет       │
│  • gotpkt/pkt — готовий av.Packet      │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Базові поля структури

### Вбудований інтерфейс `av.CodecData`:

```go
av.CodecData  // вбудоване поле
```

**Призначення**: надає доступ до метаданих кодека через уніфікований інтерфейс:
- `Type()` → `av.H264`, `av.AAC`, тощо
- `SampleRate()`, `ChannelLayout()` → для аудіо
- `Width()`, `Height()`, `FPS()` → для відео

**✅ Ваш use-case**: отримання параметрів потоку

```go
// GetStreamInfo — витягування метаданих для логування
func GetStreamInfo(stream *rtsp.Stream) map[string]interface{} {
    info := map[string]interface{}{
        "codec": stream.Type().String(),
        "payloadType": stream.Sdp.PayloadType,
        "clockRate": stream.Sdp.TimeScale,
    }
    
    if stream.Type().IsVideo() {
        if vc, ok := stream.CodecData.(av.VideoCodecData); ok {
            info["resolution"] = fmt.Sprintf("%dx%d", vc.Width(), vc.Height())
            info["fps"] = vc.FPS()
        }
    }
    if stream.Type().IsAudio() {
        if ac, ok := stream.CodecData.(av.AudioCodecData); ok {
            info["sampleRate"] = ac.SampleRate()
            info["channels"] = ac.ChannelLayout().Count()
        }
    }
    return info
}
```

---

### Посилання на батьківський клієнт:

```go
client *Client  // посилання на батьківський RTSP клієнт
```

**Призначення**: дозволяє потоку:
- Отримувати налаштування (`DebugRtp`, `SkipErrRtpBlock`)
- Сигналізувати про зміни кодеків (`ErrCodecDataChange`)
- Доступ до спільних ресурсів (буфери, метрики)

**⚠️ Увага**: Це посилання створює циклічну залежність. При копіюванні `Client` (у `HandleCodecDataChange`) потрібно оновлювати `stream.client`.

---

### SDP метадані:

```go
Sdp sdp.Media  // розпаршені параметри з SDP
```

**Ключові поля `sdp.Media`**:
```go
type Media struct {
    AVType             string      // "video" або "audio"
    Type               av.CodecType // av.H264, av.AAC...
    TimeScale          int         // частота дискретизації (90000 для відео)
    PayloadType        int         // RTP payload type (96-127 для dynamic)
    Control            string      // URL для SETUP (відносний/абсолютний)
    Config             []byte      // MPEG4AudioConfig для AAC
    SpropParameterSets [][]byte    // SPS+PPS для H.264 (з SDP)
    // ... інші поля ...
}
```

**✅ Ваш use-case**: валідація перед підключенням

```go
// ValidateStreamBeforeSetup — перевірка готовності потоку
func ValidateStreamBeforeSetup(m sdp.Media) error {
    if m.AVType == "video" && m.Type == av.H264 {
        if len(m.SpropParameterSets) == 0 && len(m.Config) == 0 {
            return fmt.Errorf("H.264 stream without SPS/PPS in SDP")
        }
    }
    if m.AVType == "audio" && m.Type == av.AAC && len(m.Config) == 0 {
        return fmt.Errorf("AAC stream without config in SDP")
    }
    if m.TimeScale == 0 {
        return fmt.Errorf("missing TimeScale in SDP")
    }
    return nil
}
```

---

## 🔑 2. H.264 специфічний стан

### 🔧 Збірка фрагментованих NALU (FU-A):

```go
fuStarted  bool      // чи триває збірка фрагментів
fuBuffer   []byte    // буфер для зібраного NALU
```

**🔍 Як працює FU-A фрагментація**:

```
Великі NALU (> MTU) розбиваються на кілька RTP пакетів:

Пакет 1 (Start):
  [RTP header][FU indicator][FU header: S=1, E=0, Type=7][data...]
  
Пакет 2 (Middle):
  [RTP header][FU indicator][FU header: S=0, E=0, Type=7][data...]
  
Пакет 3 (End):
  [RTP header][FU indicator][FU header: S=0, E=1, Type=7][data...]

Алгоритм збірки у handleH264Payload():
1. S=1: ініціалізувати fuBuffer = [відновлений заголовок NALU]
2. S=0,E=0: додати data до fuBuffer
3. S=0,E=1: додати останню частину, викликати рекурсивну обробку
```

### 🔧 Параметр-сети SPS/PPS:

```go
sps, pps        []byte  // поточні значення
spsChanged, ppsChanged bool  // прапорці змін
```

**Призначення**:
- `sps/pps`: зберігаються для ініціалізації `CodecData`
- `spsChanged/ppsChanged`: сигналізують про зміну параметрів (напр. зміна роздільної здатності)

**✅ Ваш use-case**: обробка динамічних змін

```go
// HandleDynamicSPSChange — реакція на зміну SPS/PPS
func HandleDynamicSPSChange(stream *rtsp.Stream) error {
    if !stream.isCodecDataChange() {
        return nil  // немає змін
    }
    
    // Спроба створити новий CodecData
    newCodec, err := h264parser.NewCodecDataFromSPSAndPPS(stream.sps, stream.pps)
    if err != nil {
        return fmt.Errorf("failed to update codec: %w", err)
    }
    
    // Оновлення (у реальності це робить HandleCodecDataChange())
    stream.CodecData = newCodec
    stream.clearCodecDataChange()
    
    log.Printf("Codec updated: new resolution %dx%d", 
        newCodec.Width(), newCodec.Height())
    return nil
}
```

---

## 🔑 3. Таймінги та буферизація пакетів

### 🔧 RTP timestamp обробка:

```go
timestamp      uint32  // поточний RTP timestamp
firsttimestamp uint32  // базовий timestamp для нормалізації
```

**🔍 Чому потрібна нормалізація**:

```
RTP timestamp — 32-бітне значення, що:
• Може починатися з довільного значення
• Переповнюється кожні ~26 годин (для 90kHz clock)
• Різні потоки мають незалежні лічильники

Нормалізація:
  normalized = (current - firsttimestamp) / timeScale * time.Second
```

### 🔧 Готовий пакет для відправки:

```go
gotpkt bool        // чи є готовий av.Packet
pkt    av.Packet   // буфер для готового пакету
```

**🔄 Життєвий цикл пакету**:

```
1. handleRtpPacket() обробляє RTP payload
2. При успіху: gotpkt = true, pkt заповнено
3. readPacket() перевіряє gotpkt → повертає pkt
4. Після повернення: gotpkt = false, pkt очищено
```

### 🔧 Останній відправлений час:

```go
lasttime time.Duration  // час останнього відправленого пакету
```

**Призначення**: детекція аномалій таймінгів:

```go
// У handleBlock():
if pkt.Time < stream.lasttime || pkt.Time-stream.lasttime > time.Minute*30 {
    err = fmt.Errorf("rtp: time invalid stream#%d time=%v lasttime=%v", 
        pkt.Idx, pkt.Time, stream.lasttime)
    return
}
stream.lasttime = pkt.Time
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Моніторинг стану потоків

```go
// StreamMonitor — моніторинг здоров'я потоків
type StreamMonitor struct {
    channelID string
    metrics   *StreamMetrics
}

type StreamMetrics struct {
    PacketsReceived prometheus.CounterVec
    KeyFrames       prometheus.CounterVec
    CodecChanges    prometheus.CounterVec
    TimeDrift       prometheus.HistogramVec
}

// CheckStreamHealth — оцінка стану потоку
func (m *StreamMonitor) CheckStreamHealth(stream *rtsp.Stream) (health StreamHealth, err error) {
    // 1. Перевірка наявності кодека
    if stream.CodecData == nil {
        health.Status = StatusWaitingCodec
        health.Issues = append(health.Issues, "codec not initialized")
        return health, nil
    }
    
    // 2. Детекція змін параметрів
    if stream.isCodecDataChange() {
        health.Status = StatusCodecChanging
        health.Issues = append(health.Issues, "SPS/PPS changed")
        m.metrics.CodecChanges.WithLabelValues(m.channelID).Inc()
    }
    
    // 3. Перевірка таймінгів (якщо є дані)
    if stream.lasttime > 0 {
        // Логіка перевірки дрейфу...
    }
    
    // 4. Статистика
    if stream.Type() == av.H264 && stream.pkt.IsKeyFrame {
        m.metrics.KeyFrames.WithLabelValues(m.channelID).Inc()
    }
    
    if len(health.Issues) == 0 {
        health.Status = StatusHealthy
    }
    return health, nil
}

type StreamHealth struct {
    Status StreamStatus
    Issues []string
}

type StreamStatus string

const (
    StatusHealthy        StreamStatus = "healthy"
    StatusWaitingCodec   StreamStatus = "waiting_codec"
    StatusCodecChanging  StreamStatus = "codec_changing"
    StatusTimeDrift      StreamStatus = "time_drift"
)
```

### 🔧 Приклад: Обробка змін кодеків у реальному часі

```go
// ProcessStreamWithCodecChange — цикл обробки з підтримкою змін
func ProcessStreamWithCodecChange(client *rtsp.Client, handler PacketHandler) error {
    for {
        pkt, err := client.ReadPacket()
        if err == rtsp.ErrCodecDataChange {
            // 🔄 Перестворення клієнта з оновленими кодеками
            log.Printf("Codec change detected, updating client...")
            if client, err = client.HandleCodecDataChange(); err != nil {
                return fmt.Errorf("codec update failed: %w", err)
            }
            log.Printf("Codec updated successfully")
            continue
        }
        if err == io.EOF {
            break
        }
        if err != nil {
            return fmt.Errorf("read packet: %w", err)
        }
        
        // Обробка пакету
        if err := handler.HandlePacket(pkt); err != nil {
            return fmt.Errorf("handle packet: %w", err)
        }
    }
    return nil
}

// PacketHandler — інтерфейс для обробки пакетів
type PacketHandler interface {
    HandlePacket(av.Packet) error
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **`fuBuffer` не очищається при помилці** | Завислі фрагменти → витік пам'яті | Додайте `defer` або перевірку `isEnd` у всіх гілках обробки |
| **`spsChanged/ppsChanged` не скидаються** | `ErrCodecDataChange` повертається постійно | Переконайтеся, що `clearCodecDataChange()` викликається після успішного оновлення |
| **`timestamp` переповнення** | Час "стрибає" назад після ~26 годин | Додайте обробку переповнення: `if current < prev { current += 1<<32 }` |
| **`lasttime` не ініціалізується** | Перший пакет завжди вважається "дрейфом" | Ініціалізуйте `lasttime = pkt.Time` для першого пакету |
| **`CodecData == nil` після Describe** | Потік не готовий до обробки | Викличте `client.probe()` або очікуйте SPS/PPS у потоці |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування буферів для FU-A збірки:

```go
var fuBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір фрагментованого NALU: до 64KB
        buf := make([]byte, 0, 64*1024)
        return &buf
    },
}

func getFUBuffer() *[]byte { return fuBufferPool.Get().(*[]byte) }
func putFUBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    fuBufferPool.Put(b)
}

// У handleH264Payload() для FU-A:
if isStart {
    if stream.fuBuffer == nil {
        stream.fuBuffer = *getFUBuffer()
    } else {
        stream.fuBuffer = stream.fuBuffer[:0]
    }
    // ... збірка ...
}
if isEnd {
    defer putFUBuffer(&stream.fuBuffer)  // повернення у пул після обробки
}
```

### 2. Попередня аллокація `pkt.Data`:

```go
// PreallocatePacketBuffer — виділення буфера заздалегідь
func PreallocatePacketBuffer(stream *rtsp.Stream, maxNALUSize int) {
    if stream.pkt.Data == nil || cap(stream.pkt.Data) < maxNALUSize+4 {
        stream.pkt.Data = make([]byte, 0, maxNALUSize+4)  // +4 для AVCC префіксу
    }
}
```

### 3. Моніторинг стану потоків:

```go
type StreamRuntimeMetrics struct {
    FUReassemblies prometheus.CounterVec  // кількість зібраних FU-A
    AvgNALUSize    prometheus.HistogramVec
    Fragmentation  prometheus.CounterVec  // NALU, що потребували фрагментації
}

func (m *StreamRuntimeMetrics) RecordNALU(streamType av.CodecType, size int, fragmented bool, channelID string) {
    m.AvgNALUSize.WithLabelValues(streamType.String(), channelID).Observe(float64(size))
    if fragmented {
        m.Fragmentation.WithLabelValues(channelID).Inc()
    }
}
```

---

## 📋 Чек-лист інтеграції rtsp.Stream

```go
// ✅ 1. Перевірка ініціалізації кодека
if stream.CodecData == nil {
    // Очікуйте SPS/PPS у потоці або викличте probe()
}

// ✅ 2. Обробка змін SPS/PPS
if stream.isCodecDataChange() {
    client, err = client.HandleCodecDataChange()
    if err != nil { /* handle */ }
}

// ✅ 3. Валідація таймінгів
if pkt.Time < stream.lasttime {
    // Обробка переповнення або дрейфу
}

// ✅ 4. Очищення ресурсів при завершенні
if stream.fuBuffer != nil {
    putFUBuffer(&stream.fuBuffer)  // повернення у пул
}

// ✅ 5. Метрики для моніторингу
metrics.RecordNALU(stream.Type(), len(pkt.Data), stream.fuStarted, channelID)

// ✅ 6. Логування для дебагу
if client.DebugRtp {
    log.Printf("Stream %d: pkt time=%v, key=%v, size=%d", 
        pkt.Idx, pkt.Time, pkt.IsKeyFrame, len(pkt.Data))
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk rtsp Package](https://pkg.go.dev/github.com/deepch/vdk/format/rtsp)
- 📄 [RTP Payload Format for H.264 (RFC 6184)](https://datatracker.ietf.org/doc/html/rfc6184#section-5.8) — FU-A фрагментація
- 📄 [H.264 SPS/PPS Structure](https://wiki.multimedia.cx/index.php/H.264#Sequence_parameter_set) — формат параметр-сетів
- 🧪 [Go sync.Pool Best Practices](https://go.dev/blog/pool) — ефективне управління пам'яттю

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте `CodecData != nil`** перед обробкою пакетів — інакше паніка при доступі до методів.
> 2. **Обробляйте `ErrCodecDataChange` у циклі читання** — ігнорування призведе до некоректного декодування.
> 3. **Кешуйте буфери `fuBuffer` через `sync.Pool`** — уникнення аллокацій критично для high-FPS потоків.
> 4. **Додайте обробку переповнення `timestamp`** — 32-бітний лічильник переповнюється кожні ~26 годин.
> 5. **Моніторьте `Fragmentation` метрику** — високий рівень фрагментації може вказувати на проблеми з MTU або мережею.

Потрібен приклад реалізації `PacketHandler` для вашого `pubsub.Queue`, що розподіляє пакети між HLS muxer, WebSocket та архівом? Готовий допомогти! 🚀