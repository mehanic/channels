# 🎬 Глибокий розбір: ffmpeg VideoDecoder — CGO обгортка для відео-декодування

Цей файл — **CGO-обгортка бібліотеки FFmpeg** для декодування відео-потоків у бібліотеці `vdk`. Він надає інтерфейс `VideoDecoder` для декодування стиснених відео-пакетів (наприклад, H.264) у сирі фрейми `image.YCbCr`.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема ffmpeg VideoDecoder

```
┌────────────────────────────────────────┐
│ 📦 ffmpeg VideoDecoder — CGO Wrapper   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • VideoDecoder — основний інтерфейс   │
│  • VideoFrame — обгортка для AVFrame   │
│  • wrap_avcodec_decode_video2() — CGO обгортка│
│  • fromCPtr() — конвертація C-пам'яті у Go slice│
│                                         │
│  🔄 Потік даних:                        │
│  []byte (H.264 NALU) → Decode() → image.YCbCr│
│                                         │
│  🔧 CGO інтеграція:                    │
│  • #include "ffmpeg.h" — C заголовки   │
│  • unsafe.Pointer для доступу до C пам'яті│
│  • runtime.SetFinalizer() для очищення │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. VideoDecoder — основний інтерфейс декодування

### Структура:

```go
type VideoDecoder struct {
    ff        *ffctx        // внутрішній FFmpeg контекст
    Extradata []byte        // extradata для ініціалізації (SPS/PPS для H.264)
}
```

### 🔧 Метод `Setup()` — ініціалізація декодера:

```go
func (self *VideoDecoder) Setup() (err error) {
    ff := &self.ff.ff
    
    // 1. Встановлення extradata якщо є
    if len(self.Extradata) > 0 {
        ff.codecCtx.extradata = (*C.uint8_t)(unsafe.Pointer(&self.Extradata[0]))
        ff.codecCtx.extradata_size = C.int(len(self.Extradata))
    }
    
    // 2. Відкриття кодека
    if C.avcodec_open2(ff.codecCtx, ff.codec, nil) != 0 {
        err = fmt.Errorf("ffmpeg: decoder: avcodec_open2 failed")
        return
    }
    return nil
}
```

### 🔧 Метод `Decode()` — декодування пакету у фрейм:

```go
func (self *VideoDecoder) Decode(pkt []byte) (img *VideoFrame, err error) {
    ff := &self.ff.ff
    
    // 1. Виділення AVFrame для виходу
    frame := C.av_frame_alloc()
    cgotimg := C.int(0)
    
    // 2. Виклик обгорнутої функції декодування
    cerr := C.wrap_avcodec_decode_video2(
        ff.codecCtx,                    // AVCodecContext
        frame,                          // AVFrame для виходу
        unsafe.Pointer(&pkt[0]),        // вхідні дані
        C.int(len(pkt)),                // розмір вхідних даних
        &cgotimg,                       // прапорець: чи отримано фрейм
    )
    
    if cerr < C.int(0) {
        err = fmt.Errorf("ffmpeg: avcodec_decode_video2 failed: %d", cerr)
        return
    }
    
    // 3. Обробка результату
    if cgotimg != C.int(0) {
        // Конвертація AVFrame → image.YCbCr
        w := int(frame.width)
        h := int(frame.height)
        ys := int(frame.linesize[0])  // stride для Y каналу
        cs := int(frame.linesize[1])  // stride для Cb/Cr каналів
        
        img = &VideoFrame{
            Image: image.YCbCr{
                Y:              fromCPtr(unsafe.Pointer(frame.data[0]), ys*h),
                Cb:             fromCPtr(unsafe.Pointer(frame.data[1]), cs*h/2),
                Cr:             fromCPtr(unsafe.Pointer(frame.data[2]), cs*h/2),
                YStride:        ys,
                CStride:        cs,
                SubsampleRatio: image.YCbCrSubsampleRatio420,  // 4:2:0 для H.264
                Rect:           image.Rect(0, 0, w, h),
            },
            frame: frame,  // збереження C.AVFrame для подальшого звільнення
        }
        
        // Реєстрація фіналізатора для автоматичного очищення
        runtime.SetFinalizer(img, freeVideoFrame)
    }
    
    return img, nil
}
```

### 🔧 Метод `DecodeSingle()` — декодування з флешем:

```go
func (self *VideoDecoder) DecodeSingle(pkt []byte) (img *VideoFrame, err error) {
    // Аналогічно Decode(), але з додатковим флешем якщо фрейм не отримано
    
    // 1. Спроба декодування з даними
    cerr := C.wrap_avcodec_decode_video2(...)
    
    // 2. Якщо фрейм не отримано (cgotimg == 0) — спроба флешу
    if cgotimg == C.int(0) {
        cerr = C.wrap_avcodec_decode_video2_empty(
            ff.codecCtx, frame, unsafe.Pointer(&pkt[0]), C.int(0), &cgotimg)
        // ... обробка помилки ...
    }
    
    // 3. Конвертація у image.YCbCr якщо фрейм отримано
    // ... аналогічно Decode() ...
    
    return img, nil
}
```

### 🔍 Чому два методи декодування?

```
Decode():
• Стандартне декодування одного пакету
• Повертає фрейм якщо він готовий до відображення
• Може не повернути фрейм якщо потрібні наступні пакети (B-frames)

DecodeSingle():
• Гарантує повернення фрейму для кожного виклику
• Якщо Decode() не повернув фрейм — викликає "флеш" з пустим пакетом
• Корисно для real-time обробки де потрібен фрейм на кожному кроці
• Може повернути затриманий фрейм з попередніх пакетів

Вибір методу залежить від use-case:
• Decode() — для офлайн обробки, де можна чекати на B-frames
• DecodeSingle() — для real-time, де важлива низька затримка
```

---

## 🔑 2. VideoFrame — обгортка для AVFrame

### Структура:

```go
type VideoFrame struct {
    Image image.YCbCr  // Go-сумісне представлення відео-фрейму
    frame *C.AVFrame   // посилання на оригінальний C.AVFrame для звільнення
}
```

### 🔧 Методи очищення:

```go
func (self *VideoFrame) Free() {
    self.Image = image.YCbCr{}  // очищення посилань на дані
    C.av_frame_free(&self.frame)  // звільнення C-пам'яті
}

func freeVideoFrame(self *VideoFrame) {
    self.Free()  // callback для runtime.SetFinalizer()
}
```

### 🔍 Конвертація C-пам'яті у Go slice: `fromCPtr()`

```go
func fromCPtr(buf unsafe.Pointer, size int) (ret []uint8) {
    hdr := (*reflect.SliceHeader)((unsafe.Pointer(&ret)))
    hdr.Cap = size
    hdr.Len = size
    hdr.Data = uintptr(buf)
    return
}
```

### ⚠️ Критичні застереження для `fromCPtr()`:

```
Ця функція створює Go slice, що вказує на C-пам'ять.
Це ефективно (без копіювання), але небезпечно:

✅ Безпечно:
• Поки C.AVFrame не звільнено
• Поки VideoFrame не зібрано GC
• Поки не викликано Free()

❌ Небезпечно:
• Після виклику C.av_frame_free()
• Якщо C-пам'ять перезапписана
• Якщо використано після збору GC

✅ Рекомендації:
1. Завжди використовуйте runtime.SetFinalizer()
2. Не зберігайте посилання на Image після звільнення VideoFrame
3. Копіюйте дані якщо потрібно зберегти після звільнення
```

### ✅ Ваш use-case: безпечне копіювання фрейму

```go
// CopyFrame — створення незалежної копії фрейму для подальшого використання
func CopyFrame(src *ffmpeg.VideoFrame) (*ffmpeg.VideoFrame, error) {
    if src == nil {
        return nil, fmt.Errorf("nil source frame")
    }
    
    // Створення нового image.YCbCr з копіюванням даних
    dst := image.YCbCr{
        Y:              make([]uint8, len(src.Image.Y)),
        Cb:             make([]uint8, len(src.Image.Cb)),
        Cr:             make([]uint8, len(src.Image.Cr)),
        YStride:        src.Image.YStride,
        CStride:        src.Image.CStride,
        SubsampleRatio: src.Image.SubsampleRatio,
        Rect:           src.Image.Rect,
    }
    
    // Копіювання даних
    copy(dst.Y, src.Image.Y)
    copy(dst.Cb, src.Image.Cb)
    copy(dst.Cr, src.Image.Cr)
    
    // Створення нового VideoFrame без C.AVFrame (не потребує звільнення)
    return &ffmpeg.VideoFrame{
        Image: dst,
        frame: nil,  // немає C-пам'яті для звільнення
    }, nil
}

// Використання:
frame, err := decoder.Decode(packet)
if err != nil { /* handle error */ }

// Створення копії для асинхронної обробки
frameCopy, err := CopyFrame(frame)
if err != nil { /* handle error */ }

// Звільнення оригіналу (можна відразу)
frame.Free()

// Використання копії у іншій горутині
go processFrame(frameCopy)
```

---

## 🔑 3. NewVideoDecoder — фабрика відео-декодерів

### Логіка створення:

```go
func NewVideoDecoder(stream av.CodecData) (dec *VideoDecoder, err error) {
    _dec := &VideoDecoder{}
    var id uint32
    
    // Визначення FFmpeg codec_id за типом з av.CodecData
    switch stream.Type() {
    case av.H264:
        // H.264 вимагає extradata (AVCDecoderConfRecord)
        h264 := stream.(h264parser.CodecData)
        _dec.Extradata = h264.AVCDecoderConfRecordBytes()
        id = C.AV_CODEC_ID_H264
        
    default:
        err = fmt.Errorf("ffmpeg: NewVideoDecoder codec=%v unsupported", stream.Type())
        return
    }
    
    // Пошук декодера у FFmpeg
    c := C.avcodec_find_decoder(id)
    if c == nil || C.avcodec_get_type(id) != C.AVMEDIA_TYPE_VIDEO {
        err = fmt.Errorf("ffmpeg: cannot find video decoder codecId=%d", id)
        return
    }
    
    // Створення внутрішнього контексту
    if _dec.ff, err = newFFCtxByCodec(c); err != nil {
        return
    }
    
    // Ініціалізація декодера
    if err = _dec.Setup(); err != nil {
        return
    }
    
    return _dec, nil
}
```

### ✅ Ваш use-case: створення H.264 декодера для CCTV

```go
// CreateH264Decoder — створення декодера з codecData
func CreateH264Decoder(codecData av.CodecData) (*ffmpeg.VideoDecoder, error) {
    // 1. Перевірка типу кодека
    if codecData.Type() != av.H264 {
        return nil, fmt.Errorf("expected H.264 codec, got %v", codecData.Type())
    }
    
    // 2. Створення декодера
    decoder, err := ffmpeg.NewVideoDecoder(codecData)
    if err != nil {
        return nil, fmt.Errorf("create H.264 decoder: %w", err)
    }
    
    // 3. Реєстрація фіналізатора як додаткова страховка
    runtime.SetFinalizer(decoder, func(d *ffmpeg.VideoDecoder) {
        log.Printf("warning: VideoDecoder not explicitly closed")
        // Примусове звільнення ресурсів
        // (у реальності краще викликати Close() явно)
    })
    
    return decoder, nil
}

// Використання у обробці відео:
func (p *VideoProcessor) ProcessH264Packet(pkt []byte) error {
    // 1. Створення декодера якщо ще не створено
    if p.decoder == nil {
        codecData := p.getH264CodecData()  // з вашого pipeline
        var err error
        p.decoder, err = CreateH264Decoder(codecData)
        if err != nil {
            return fmt.Errorf("create decoder: %w", err)
        }
    }
    
    // 2. Декодування пакету
    frame, err := p.decoder.Decode(pkt)
    if err != nil {
        return fmt.Errorf("decode H.264: %w", err)
    }
    if frame == nil {
        // Фрейм ще не готовий (потрібні наступні пакети для B-frames)
        return nil
    }
    
    // 3. Обробка фрейму (напр.: детекція руху, аналіз якості)
    if err := p.analyzeFrame(frame); err != nil {
        frame.Free()
        return err
    }
    
    // 4. Звільнення фрейму після обробки
    frame.Free()
    
    return nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// video_processor.go — обробка відео для CCTV з використанням ffmpeg
type VideoProcessor struct {
    channelID    string
    decoder      *ffmpeg.VideoDecoder
    codecData    av.CodecData
    frameQueue   chan *ffmpeg.VideoFrame
    metrics      *VideoMetrics
}

func NewVideoProcessor(channelID string, codecData av.CodecData) (*VideoProcessor, error) {
    // 1. Створення декодера
    decoder, err := ffmpeg.NewVideoDecoder(codecData)
    if err != nil {
        return nil, fmt.Errorf("create decoder: %w", err)
    }
    
    // 2. Ініціалізація черги фреймів
    frameQueue := make(chan *ffmpeg.VideoFrame, 10)  // буфер на 10 фреймів
    
    return &VideoProcessor{
        channelID:  channelID,
        decoder:    decoder,
        codecData:  codecData,
        frameQueue: frameQueue,
        metrics:    NewVideoMetrics(channelID),
    }, nil
}

// ProcessPacket — обробка одного відео-пакету
func (p *VideoProcessor) ProcessPacket(pkt []byte) error {
    start := time.Now()
    
    // 1. Декодування пакету у фрейм
    frame, err := p.decoder.Decode(pkt)
    if err != nil {
        return fmt.Errorf("decode: %w", err)
    }
    
    p.metrics.DecodeLatency.Observe(time.Since(start).Seconds())
    
    // 2. Якщо фрейм отримано — відправка у чергу для подальшої обробки
    if frame != nil {
        select {
        case p.frameQueue <- frame:
            // успішно відправлено
        default:
            // черга переповнена — звільняємо фрейм
            frame.Free()
            p.metrics.DroppedFrames.Inc()
        }
    }
    
    return nil
}

// StartFrameProcessor — запуск фонового обробника фреймів
func (p *VideoProcessor) StartFrameProcessor(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                // Очищення черги при завершенні
                for frame := range p.frameQueue {
                    frame.Free()
                }
                return
                
            case frame := <-p.frameQueue:
                // Обробка фрейму (напр.: детекція руху, аналіз якості)
                if err := p.analyzeFrame(frame); err != nil {
                    log.Printf("analyze frame failed: %v", err)
                }
                
                // Звільнення фрейму після обробки
                frame.Free()
            }
        }
    }()
}

// analyzeFrame — приклад аналізу фрейму
func (p *VideoProcessor) analyzeFrame(frame *ffmpeg.VideoFrame) error {
    // Приклад: розрахунок середньої яскравості
    var totalBrightness int64
    for _, y := range frame.Image.Y {
        totalBrightness += int64(y)
    }
    avgBrightness := totalBrightness / int64(len(frame.Image.Y))
    
    // Логування для метрик
    p.metrics.AvgBrightness.WithLabelValues(p.channelID).Set(float64(avgBrightness))
    
    // Тут можна додати: детекцію руху, розпізнавання об'єктів тощо
    
    return nil
}

// Close — закриття ресурсів
func (p *VideoProcessor) Close() {
    if p.decoder != nil {
        // Звільнення декодера (викликає freeFFCtx через фіналізатор)
        // Але краще мати явний Close() у VideoDecoder
        runtime.SetFinalizer(p.decoder, nil)  // скасування фіналізатора
        // У реальності: p.decoder.Close() якщо метод існує
    }
    
    // Очищення черги
    close(p.frameQueue)
    for frame := range p.frameQueue {
        frame.Free()
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"avcodec_open2 failed"** | Неправильні extradata або відсутній кодек | Переконайтеся, що `h264parser.CodecData` передається правильно; перевірте наявність H.264 декодера у FFmpeg |
| **Паніка при доступі до Image.Y** | `fromCPtr()` використано після звільнення AVFrame | Завжди звільняйте фрейм через `frame.Free()` тільки після завершення обробки; не зберігайте посилання на дані після звільнення |
| **Витік пам'яті при декодуванні** | `runtime.SetFinalizer()` не викликається вчасно | Завжди викликайте `frame.Free()` явно; не покладайтеся на фіналізатори для критичних ресурсів |
| **Некоректні розміри фрейму** | `linesize` не враховує вирівнювання пам'яті | Використовуйте `YStride`/`CStride` замість простого `width*height`; FFmpeg може додавати паддінг |
| **Повільне декодування через CGO** | Багато переходів між Go та C | Кешуйте декодер на рівні каналу; уникайте створення нових декодерів для кожного пакету |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування декодерів на рівні каналу:

```go
type DecoderCache struct {
    mu       sync.RWMutex
    decoders map[string]*ffmpeg.VideoDecoder  // channelID → decoder
}

func (c *DecoderCache) Get(channelID string) (*ffmpeg.VideoDecoder, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    dec, ok := c.decoders[channelID]
    return dec, ok
}

func (c *DecoderCache) Set(channelID string, dec *ffmpeg.VideoDecoder) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.decoders[channelID] = dec
}
```

### 2. Пакетне декодування для зменшення накладних витрат:

```go
// BatchDecode — декодування кількох пакетів за один виклик
func (d *VideoDecoder) BatchDecode(packets [][]byte) ([]*ffmpeg.VideoFrame, error) {
    frames := make([]*ffmpeg.VideoFrame, 0, len(packets))
    
    for _, pkt := range packets {
        frame, err := d.Decode(pkt)
        if err != nil {
            // Звільнення вже отриманих фреймів при помилці
            for _, f := range frames {
                f.Free()
            }
            return nil, err
        }
        if frame != nil {
            frames = append(frames, frame)
        }
    }
    return frames, nil
}
```

### 3. Моніторинг продуктивності декодування:

```go
type VideoMetrics struct {
    DecodeLatency   prometheus.HistogramVec
    FrameRate       prometheus.GaugeVec
    DroppedFrames   prometheus.CounterVec
    AvgBrightness   prometheus.GaugeVec
}

func (m *VideoMetrics) RecordDecode(duration time.Duration, channelID string) {
    m.DecodeLatency.WithLabelValues(channelID).Observe(duration.Seconds())
}

func (m *VideoMetrics) RecordFrame(channelID string) {
    m.FrameRate.WithLabelValues(channelID).Inc()
}
```

---

## 📋 Чек-лист інтеграції ffmpeg VideoDecoder

```go
// ✅ 1. Створення декодера з правильним codecData
codecData := getH264CodecData()  // з h264parser.CodecData
decoder, err := ffmpeg.NewVideoDecoder(codecData)
if err != nil { /* handle error */ }

// ✅ 2. Декодування пакету з обробкою помилок
frame, err := decoder.Decode(packet)
if err != nil {
    log.Printf("decode failed: %v", err)
    return err
}
if frame == nil {
    // Фрейм ще не готовий (потрібні наступні пакети)
    return nil
}

// ✅ 3. Безпечна обробка фрейму
// Копіювання даних якщо потрібно зберегти після звільнення
frameCopy, err := CopyFrame(frame)
if err != nil { /* handle error */ }

// ✅ 4. Звільнення оригінального фрейму
frame.Free()

// ✅ 5. Використання копії у асинхронній обробці
go processFrameAsync(frameCopy)

// ✅ 6. Закриття декодера при завершенні
defer func() {
    if decoder != nil {
        // Явне закриття якщо метод існує
        // decoder.Close()
        
        // Скасування фіналізатора щоб уникнути подвійного звільнення
        runtime.SetFinalizer(decoder, nil)
    }
}()

// ✅ 7. Метрики для моніторингу
start := time.Now()
// ... декодування ...
metrics.DecodeLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [vdk ffmpeg Package](https://pkg.go.dev/github.com/deepch/vdk/codec/ffmpeg) — GoDoc documentation
- 📄 [FFmpeg Libavcodec Video Decoding](https://ffmpeg.org/doxygen/trunk/group__lavc__decoding.html) — офіційна довідка
- 📄 [Go image.YCbCr Documentation](https://pkg.go.dev/image#YCbCr) — структура для представлення YUV даних у Go
- 🧪 [CGO and Unsafe Pointers](https://golang.org/cmd/cgo/#hdr-Go_references_to_C) — безпека роботи з C-пам'яттю
- 🎬 [H.264 Decoding Guide](https://trac.ffmpeg.org/wiki/H.264) — практичні поради для декодування H.264

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **відео в реальному часі**:
> 1. **Завжди звільняйте фрейми через `frame.Free()`** — не покладайтеся на фіналізатори для звільнення C-пам'яті.
> 2. **Копіюйте дані якщо потрібно зберегти фрейм** — `fromCPtr()` створює slice, що вказує на C-пам'ять, яка може бути звільнена.
> 3. **Використовуйте `DecodeSingle()` для real-time** — гарантує повернення фрейму на кожному виклику, що важливо для низької затримки.
> 4. **Кешуйте декодери на рівні каналу** — створення нових декодерів для кожного пакету дороге через CGO виклики.
> 5. **Моніторьте латентність декодування** — різке зростання може вказувати на перевантаження або проблеми з FFmpeg.

Потрібен приклад інтеграції `VideoProcessor` з вашим `pubsub.Queue` для розподілу вже декодованих фреймів між підписниками (аналіз, архів, WebSocket)? Готовий допомогти! 🚀