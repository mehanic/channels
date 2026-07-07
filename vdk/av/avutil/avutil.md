# 🛠️ Глибокий розбір: avutil — Утиліти для роботи з медіа в vdk

Цей файл — **ядро системи реєстрації та абстракції** бібліотеки `vdk` (Video Development Kit). Він надає уніфікований інтерфейс для відкриття/створення медіа-потоків незалежно від формату, протоколу чи джерела даних.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема avutil

```
┌────────────────────────────────────────┐
│ 📦 avutil — Handler Registry System     │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові абстракції:                 │
│  • RegisterHandler — плагін для формату│
│  • Handlers — реєстр усіх плагінів     │
│  • HandlerDemuxer/Muxer — обгортки     │
│                                         │
│  📥 Відкриття (Open):                   │
│  1. Перевірка scheme (rtsp://, http://)│
│  2. Пошук за розширенням (.ts, .m3u8)  │
│  3. Probe: аналіз перших 1024 байт     │
│  4. Створення Demuxer через Reader     │
│                                         │
│  📤 Створення (Create):                 │
│  1. Пошук за розширенням вихідного файлу│
│  2. Створення Writer → Muxer           │
│  3. Обгортка через HandlerMuxer        │
│                                         │
│  🔧 Helper functions:                   │
│  • CopyPackets — копіювання потоків    │
│  • CopyFile — повне копіювання файлу   │
│  • Equal — порівняння кодеків          │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. RegisterHandler — плагінна система для форматів

### Структура реєстрації:

```go
type RegisterHandler struct {
    Ext           string                          // розширення: ".ts", ".m3u8"
    ReaderDemuxer func(io.Reader) av.Demuxer      // створення демуксера з Reader
    WriterMuxer   func(io.Writer) av.Muxer        // створення муксера з Writer
    UrlMuxer      func(string) (bool, av.MuxCloser, error)  // URL-specific muxer
    UrlDemuxer    func(string) (bool, av.DemuxCloser, error) // URL-specific demuxer
    UrlReader     func(string) (bool, io.ReadCloser, error)  // HTTP/RTSP reader
    Probe         func([]byte) bool               // авто-детект формату по байтах
    AudioEncoder  func(av.CodecType) (av.AudioEncoder, error) // створення енкодера
    AudioDecoder  func(av.AudioCodecData) (av.AudioDecoder, error) // створення декадера
    ServerDemuxer func(string) (bool, av.DemuxCloser, error)  // серверний режим (listen:)
    ServerMuxer   func(string) (bool, av.MuxCloser, error)    // серверний muxer
    CodecTypes    []av.CodecType                  // підтримувані кодеки
}
```

### ✅ Ваш use-case: реєстрація кастомного HLS-обробника

```go
// У вашому init() або main(): реєстрація CCTV-специфічного HLS handler
func init() {
    avutil.DefaultHandlers.Add(func(h *avutil.RegisterHandler) {
        h.Ext = ".m3u8"
        h.CodecTypes = []av.CodecType{av.H264, av.AAC}
        
        // Створення демуксера для HLS-плейлиста
        h.ReaderDemuxer = func(r io.Reader) av.Demuxer {
            return newCCTVHLSDemuxer(r)  // ваш кастомний демуксер
        }
        
        // Створення муксера для запису HLS
        h.WriterMuxer = func(w io.Writer) av.Muxer {
            return newCCTVHLSMuxer(w)  // ваш кастомний муксер
        }
        
        // Авто-детект по першим байтам (M3U8 заголовок)
        h.Probe = func(buf []byte) bool {
            return bytes.HasPrefix(buf, []byte("#EXTM3U"))
        }
        
        // Підтримка RTSP вхідних потоків
        h.UrlReader = func(uri string) (bool, io.ReadCloser, error) {
            if strings.HasPrefix(uri, "rtsp://") {
                return true, openRTSPStream(uri)  // ваша реалізація
            }
            return false, nil, nil
        }
    })
}
```

---

## 📥 2. Open() — універсальне відкриття джерел

### Потік вирішення:

```
Open(uri)
   │
   ├─ 1. Перевірка "listen:" префіксу → ServerDemuxer
   │
   ├─ 2. Пошук за UrlDemuxer (для rtsp://, http://)
   │
   ├─ 3. Визначення розширення (.ts, .mp4, .m3u8)
   │   └─ Пошук handler з matching Ext + ReaderDemuxer
   │
   ├─ 4. Probe mode: читання 1024 байт → пошук handler.Probe()
   │   └─ Якщо знайдено: створення HandlerDemuxer
   │
   └─ 5. Помилка: "avutil: open %s failed"
```

### 🔍 Деталі Probe-режиму:

```go
// Читання перших 1024 байт для авто-детекту
var probebuf [1024]byte
if _, err = io.ReadFull(r, probebuf[:]); err != nil {
    return
}

// Пошук handler, що розпізнає ці байти
for _, handler := range self.handlers {
    if handler.Probe != nil && handler.Probe(probebuf[:]) {
        // Відновлення потоку для читання з початку
        var _r io.Reader
        if rs, ok := r.(io.ReadSeeker); ok {
            rs.Seek(0, 0)  // файл: просто seek назад
            _r = rs
        } else {
            // мережа: MultiReader з буфером + решта потоку
            _r = io.MultiReader(bytes.NewReader(probebuf[:]), r)
        }
        
        // Створення обгорнутого демуксера
        demuxer = &HandlerDemuxer{
            Demuxer: handler.ReaderDemuxer(_r),
            r:       r,  // для правильного Close()
        }
        return
    }
}
```

### ✅ Ваш use-case: відкриття різних джерел CCTV

```go
// OpenCCTVSource — універсальна функція відкриття для вашого проекту
func (p *CCTVProcessor) OpenCCTVSource(uri string) (av.DemuxCloser, error) {
    // 1. Спроба відкрити через avutil.Open (автоматичний детект)
    demuxer, err := avutil.Open(uri)
    if err == nil {
        log.Printf("Opened source %s via avutil", uri)
        return demuxer, nil
    }
    
    // 2. Fallback: кастомна логіка для специфічних випадків
    switch {
    case strings.HasPrefix(uri, "rtsp://"):
        return openRTSPWithAuth(uri, p.config.RTSPCredentials)
    case strings.HasPrefix(uri, "http://") && strings.HasSuffix(uri, ".ts"):
        return openHTTPSegmentStream(uri)
    case strings.HasPrefix(uri, "file://"):
        return os.Open(strings.TrimPrefix(uri, "file://"))
    }
    
    return nil, fmt.Errorf("unsupported CCTV source: %s", uri)
}
```

---

## 📤 3. Create() / FindCreate() — створення вихідних потоків

### Потік вирішення:

```
Create(uri)
   │
   ├─ 1. Перевірка "listen:" → ServerMuxer
   │
   ├─ 2. Пошук за UrlMuxer (для http://, rtsp://)
   │
   ├─ 3. Визначення розширення (.ts, .m3u8, .mp4)
   │   └─ Пошук handler з matching Ext + WriterMuxer
   │
   └─ 4. Помилка: "avutil: create muxer %s failed"
```

### 🔧 HandlerMuxer — обгортка для правильного життєвого циклу:

```go
type HandlerMuxer struct {
    av.Muxer      // базовий муксер (напр. hls.Muxer)
    w     io.WriteCloser  // файл/сеть для запису
    stage int      // 0=init, 1=header written, 2=trailer written
}

// WriteHeader — гарантує одноразовий виклик
func (self *HandlerMuxer) WriteHeader(streams []av.CodecData) error {
    if self.stage == 0 {
        if err := self.Muxer.WriteHeader(streams); err != nil {
            return err
        }
        self.stage++  // тепер stage=1
    }
    return nil
}

// WriteTrailer — гарантує виклик перед Close()
func (self *HandlerMuxer) WriteTrailer() error {
    if self.stage == 1 {
        self.stage++  // тепер stage=2
        return self.Muxer.WriteTrailer()
    }
    return nil
}

// Close — автоматично викликає WriteTrailer якщо потрібно
func (self *HandlerMuxer) Close() error {
    if err := self.WriteTrailer(); err != nil {
        return err
    }
    return self.w.Close()  // закриття файлу/з'єднання
}
```

### ✅ Ваш use-case: створення HLS-сегментів для каналу

```go
// CreateHLSSegment — створення нового сегменту для каналу
func (p *HLSGenerator) CreateHLSSegment(channelID string, segmentNum int) (av.MuxCloser, error) {
    // Формування шляху: /app/channels/{channel_id}/segments/{num}.ts
    segmentPath := filepath.Join(
        p.baseDir,
        "channels",
        channelID,
        "segments",
        fmt.Sprintf("%d.ts", segmentNum),
    )
    
    // Створення через avutil (авто-вибір handler за розширенням .ts)
    muxer, err := avutil.Create(segmentPath)
    if err != nil {
        return nil, fmt.Errorf("create segment %d: %w", segmentNum, err)
    }
    
    // Додавання метаданих каналу у muxer (якщо підтримується)
    if tagged, ok := muxer.(interface{ SetChannelID(string) }); ok {
        tagged.SetChannelID(channelID)
    }
    
    log.Printf("Created HLS segment %d for channel %s", segmentNum, channelID)
    return muxer, nil
}
```

---

## 🔧 4. Helper Functions — корисні утиліти

### CopyPackets — копіювання потоків:

```go
func CopyPackets(dst av.PacketWriter, src av.PacketReader) error {
    for {
        pkt, err := src.ReadPacket()
        if err != nil {
            if err == io.EOF {
                break  // нормальне завершення
            }
            return err
        }
        if err = dst.WritePacket(pkt); err != nil {
            return err  // помилка запису
        }
    }
    return nil
}
```

### ✅ Ваш use-case: проксі-перенаправлення потоків

```go
// ProxyStream — перенаправлення пакетів з входу на кілька виходів
func (p *StreamProxy) ProxyStream(src av.Demuxer, outputs []av.Muxer) error {
    for {
        pkt, err := src.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
        
        // Копіювання на всі виходи паралельно
        var wg sync.WaitGroup
        errChan := make(chan error, len(outputs))
        
        for _, out := range outputs {
            wg.Add(1)
            go func(muxer av.Muxer, packet av.Packet) {
                defer wg.Done()
                if err := muxer.WritePacket(packet); err != nil {
                    errChan <- err
                }
            }(out, pkt)
        }
        
        wg.Wait()
        close(errChan)
        
        // Перша помилка (якщо є)
        if err, ok := <-errChan; ok {
            return err
        }
    }
    return nil
}
```

### CopyFile — повне копіювання файлу:

```go
func CopyFile(dst av.Muxer, src av.Demuxer) error {
    // 1. Отримання інформації про потоки
    streams, err := src.Streams()
    if err != nil { return err }
    
    // 2. Запис заголовка (кодеки, метадані)
    if err = dst.WriteHeader(streams); err != nil { return err }
    
    // 3. Копіювання всіх пакетів
    if err = CopyPackets(dst, src); err != nil {
        if err != io.EOF { return err }
    }
    
    // 4. Запис трейлера (фіналізація)
    return dst.WriteTrailer()
}
```

### Equal — порівняння кодеків:

```go
func Equal(c1 []av.CodecData, c2 []av.CodecData) bool {
    if len(c1) != len(c2) { return false }
    
    for i, codec := range c1 {
        if codec.Type() != c2[i].Type() { return false }
        
        // Спеціальна перевірка для H.264: порівняння SPS/PPS
        if codec.Type() == av.H264 {
            if bytes.Compare(
                codec.(h264parser.CodecData).AVCDecoderConfRecordBytes(),
                c2[i].(h264parser.CodecData).AVCDecoderConfRecordBytes(),
            ) != 0 {
                return false
            }
        }
        
        // Спеціальна перевірка для AAC: порівняння AudioSpecificConfig
        if codec.Type() == av.AAC {
            if bytes.Compare(
                codec.(aacparser.CodecData).MPEG4AudioConfigBytes(),
                c2[i].(aacparser.CodecData).MPEG4AudioConfigBytes(),
            ) != 0 {
                return false
            }
        }
    }
    return true
}
```

### ✅ Ваш use-case: валідація сумісності потоків

```go
// ValidateStreamCompatibility — перевірка чи можна об'єднати два потоки
func (p *StreamMerger) ValidateStreamCompatibility(streams1, streams2 []av.CodecData) error {
    if !avutil.Equal(streams1, streams2) {
        // Детальне логування відмінностей
        for i := range streams1 {
            if streams1[i].Type() != streams2[i].Type() {
                return fmt.Errorf("codec mismatch at index %d: %s vs %s", 
                    i, streams1[i].Type(), streams2[i].Type())
            }
        }
        return fmt.Errorf("stream configuration mismatch")
    }
    return nil
}

// Використання перед об'єднанням потоків:
if err := p.ValidateStreamCompatibility(mainStreams, backupStreams); err != nil {
    log.Warn("Streams not compatible, using fallback", "err", err)
    // Або: автоматичне транскодування для сумісності
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// cctv_pipeline.go — інтеграція avutil у ваш CCTV HLS Processor
type CCTVPipeline struct {
    channelID   string
    inputURI    string
    outputDir   string
    handlers    *avutil.Handlers
    demuxer     av.DemuxCloser
    muxers      map[string]av.MuxCloser  // segment_num → muxer
}

func NewCCTVPipeline(channelID, inputURI, outputDir string) (*CCTVPipeline, error) {
    // 1. Реєстрація кастомних handler'ів (якщо потрібно)
    registerCCTVHandlers()
    
    // 2. Відкриття вхідного потоку
    demuxer, err := avutil.Open(inputURI)
    if err != nil {
        return nil, fmt.Errorf("open input %s: %w", inputURI, err)
    }
    
    // 3. Валідація кодеків
    streams, err := demuxer.Streams()
    if err != nil {
        return nil, err
    }
    if err := validateCodecsForHLS(streams); err != nil {
        return nil, fmt.Errorf("codec validation: %w", err)
    }
    
    return &CCTVPipeline{
        channelID:  channelID,
        inputURI:   inputURI,
        outputDir:  outputDir,
        demuxer:    demuxer,
        muxers:     make(map[string]av.MuxCloser),
    }, nil
}

// ProcessSegment — обробка одного сегменту (напр. 10 секунд)
func (p *CCTVPipeline) ProcessSegment(ctx context.Context, segmentNum int) error {
    // 1. Створення вихідного файлу сегменту
    segmentPath := filepath.Join(p.outputDir, fmt.Sprintf("%d.ts", segmentNum))
    muxer, err := avutil.Create(segmentPath)
    if err != nil {
        return err
    }
    p.muxers[fmt.Sprintf("%d", segmentNum)] = muxer
    
    // 2. Запис заголовка (тільки для першого сегменту)
    if segmentNum == 0 {
        streams, _ := p.demuxer.Streams()
        if err := muxer.WriteHeader(streams); err != nil {
            return err
        }
    }
    
    // 3. Копіювання пакетів протягом сегменту
    segmentEnd := time.Now().Add(10 * time.Second)
    for time.Now().Before(segmentEnd) {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        
        pkt, err := p.demuxer.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
        
        // Кастомна обробка: субтитри, метрики тощо
        p.processPacket(pkt)
        
        // Запис у сегмент
        if err := muxer.WritePacket(pkt); err != nil {
            return err
        }
    }
    
    // 4. Фіналізація сегменту
    if err := muxer.WriteTrailer(); err != nil {
        return err
    }
    if err := muxer.Close(); err != nil {
        return err
    }
    delete(p.muxers, fmt.Sprintf("%d", segmentNum))
    
    // 5. Оновлення HLS плейлиста
    return p.updatePlaylist(segmentNum)
}

// processPacket — кастомна обробка кожного пакету
func (p *CCTVPipeline) processPacket(pkt av.Packet) {
    // Експорт метрик
    monitoring.PacketsProcessed.WithLabelValues(p.channelID).Inc()
    
    // Обробка субтитрів якщо це потрібний потік
    if pkt.Idx == subtitleStreamIdx {
        _ = p.extractSubtitles(pkt)
    }
    
    // Детекція ключових кадрів для синхронізації
    if pkt.IsKeyFrame {
        log.Printf("Key frame at %v, channel=%s", pkt.Time, p.channelID)
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"avutil: open %s failed"** | Невідомий формат або протокол | Додайте кастомний `UrlReader` або `Probe` функцію у `RegisterHandler` |
| **"avutil: create muxer %s failed"** | Непідтримуване розширення вихідного файлу | Зареєструйте handler з `Ext=".your_ext"` та `WriterMuxer` |
| **Probe не спрацьовує** | Авто-детект не розпізнає формат | Збільшіть розмір буферу або покращте логіку `Probe()` |
| **WriteHeader викликано двічі** | Помилка у життєвому циклі муксера | Використовуйте `HandlerMuxer` обгортку для автоматичного контролю `stage` |
| **Пам'ять росте при CopyPackets** | Буфери не очищаються | Використовуйте `sync.Pool` для переиспользування `av.Packet` структур |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування handler'ів за URI:

```go
type HandlerCache struct {
    mu       sync.RWMutex
    cache    map[string]*avutil.RegisterHandler  // uri → handler
}

func (c *HandlerCache) Get(uri string) *avutil.RegisterHandler {
    c.mu.RLock()
    if h, ok := c.cache[uri]; ok {
        c.mu.RUnlock()
        return h
    }
    c.mu.RUnlock()
    
    // Пошук у DefaultHandlers (ваша логіка)
    h := findHandlerForURI(uri)
    
    c.mu.Lock()
    c.cache[uri] = h
    c.mu.Unlock()
    
    return h
}
```

### 2. Пакетне читання для зменшення системних викликів:

```go
// Замість ReadPacket для кожного пакету:
func (p *CCTVPipeline) readPacketBatch(count int) ([]av.Packet, error) {
    packets := make([]av.Packet, 0, count)
    
    for i := 0; i < count; i++ {
        pkt, err := p.demuxer.ReadPacket()
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

### 3. Async WritePacket для неблокуючого запису:

```go
type AsyncMuxer struct {
    av.Muxer
    queue chan av.Packet
    done  chan error
}

func NewAsyncMuxer(base av.Muxer, queueSize int) *AsyncMuxer {
    m := &AsyncMuxer{
        Muxer: base,
        queue: make(chan av.Packet, queueSize),
        done:  make(chan error, 1),
    }
    
    // Background writer goroutine
    go func() {
        for pkt := range m.queue {
            if err := m.Muxer.WritePacket(pkt); err != nil {
                m.done <- err
                return
            }
        }
        m.done <- nil
    }()
    
    return m
}

func (m *AsyncMuxer) WritePacketAsync(pkt av.Packet) error {
    select {
    case m.queue <- pkt:
        return nil
    case err := <-m.done:
        return err  // помилка з background goroutine
    case <-time.After(100 * time.Millisecond):
        return fmt.Errorf("write queue full")
    }
}
```

---

## 📋 Чек-лист інтеграції avutil

```go
// ✅ 1. Реєстрація кастомних handler'ів (опціонально)
avutil.DefaultHandlers.Add(func(h *avutil.RegisterHandler) {
    h.Ext = ".m3u8"
    h.Probe = func(buf []byte) bool { return bytes.HasPrefix(buf, []byte("#EXTM3U")) }
    // ... інші поля
})

// ✅ 2. Відкриття вхідного потоку
demuxer, err := avutil.Open(inputURI)  // авто-детект формату/протоколу

// ✅ 3. Валідація кодеків для HLS сумісності
streams, _ := demuxer.Streams()
if err := validateCodecsForHLS(streams); err != nil {
    // транскодування або помилка
}

// ✅ 4. Створення вихідного муксера
muxer, err := avutil.Create(outputPath)  // авто-вибір за розширенням

// ✅ 5. Запис заголовка (тільки один раз!)
muxer.WriteHeader(streams)

// ✅ 6. Основний цикл: ReadPacket → Process → WritePacket
for {
    pkt, err := demuxer.ReadPacket()
    if err == io.EOF { break }
    
    processPacket(pkt)  // ваша логіка
    muxer.WritePacket(pkt)
}

// ✅ 7. Фіналізація
muxer.WriteTrailer()
muxer.Close()
demuxer.Close()

// ✅ 8. Метрики
monitoring.StreamsOpened.Inc()
monitoring.StreamProcessingLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [vdk GitHub](https://github.com/deepch/vdk) — основна бібліотека
- 📄 [vdk avutil Package](https://pkg.go.dev/github.com/deepch/vdk/av/avutil) — GoDoc documentation
- 🎬 [HLS Spec (RFC 8216)](https://datatracker.ietf.org/doc/html/rfc8216) — для правильної реалізації HLS handler'ів
- 📡 [RTSP Spec (RFC 2326)](https://datatracker.ietf.org/doc/html/rfc2326) — якщо додаєте підтримку RTSP
- 🧪 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади використання avutil

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Зареєструйте кастомні handler'і** для ваших специфічних форматів/протоколів на старті програми.
> 2. **Використовуйте Probe-функції** для авто-детекту форматів — це спрощує підтримку нових джерел.
> 3. **Обгортайте муксери через `HandlerMuxer`** — це гарантує правильний порядок `WriteHeader`/`WriteTrailer`/`Close`.
> 4. **Кешуйте відкриті демуксери** — повторне відкриття того ж URI може бути дорогим.
> 5. **Додайте таймаути для `UrlReader`** — мережеві джерела (RTSP/HTTP) можуть зависати.

Потрібен приклад реалізації `newCCTVHLSDemuxer()` або `newCCTVHLSMuxer()` для інтеграції з вашим `segmentAssembler`? Готовий допомогти! 🚀