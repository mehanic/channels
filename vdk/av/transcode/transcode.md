# 🔄 Глибокий розбір: transcode — Аудіо-транскодер для медіа-пайплайнів

Цей файл — **реалізація прозорого аудіо-транскодера** на базі бібліотеки `vdk`. Він автоматично конвертує аудіо-потоки між кодеками (напр. MP3 → AAC) "на льоту" під час читання/запису медіа-даних, зберігаючи відео без змін.

Розберемо архітектуру, алгоритми та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема transcode

```
┌────────────────────────────────────────┐
│ 📦 transcode — Audio Transcoding Engine│
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • tStream — стан одного потоку        │
│  • Transcoder — ядро транскодування    │
│  • Muxer/Demuxer wrappers — прозора інтеграція│
│  • pktque.Timeline — корекція таймінгів│
│                                         │
│  🔄 Потік даних (аудіо):               │
│  In Packet → Decoder → AudioFrame     │
│                → Encoder → Out Packet(s)│
│                → Timeline.Pop() → коррегований час│
│                                         │
│  🎯 Особливості:                        │
│  • 1 вхідний пакет → N вихідних пакетів│
│  • Авто-корекція таймінгів             │
│  • Відео проходить без змін (passthrough)│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. tStream — стан одного потоку

```go
type tStream struct {
    codec              av.CodecData        // вихідний кодек (після транскодування)
    timeline           *pktque.Timeline    // таймлайн для корекції часу
    aencodec, adecodec av.AudioCodecData   // кодеки для енкодера/декодера
    aenc               av.AudioEncoder     // енкодер (вихідний формат)
    adec               av.AudioDecoder     // декодер (вхідний формат)
}
```

### 🎯 Поля та їх призначення:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `codec` | `av.CodecData` | Метадані **вихідного** кодека | `AAC CodecData` після MP3→AAC |
| `timeline` | `*pktque.Timeline` | Черга для вирівнювання таймінгів | `Push(in_time, duration)` → `Pop(out_duration)` |
| `aencodec` | `av.AudioCodecData` | Кодек енкодера (цільовий формат) | `AAC` для HLS сумісності |
| `adecodec` | `av.AudioCodecData` | Кодек декодера (вихідний формат) | `MP3` з вхідного потоку |
| `aenc` | `av.AudioEncoder` | Інтерфейс кодування аудіо | `aac.Encoder` |
| `adec` | `av.AudioDecoder` | Інтерфейс декодування аудіо | `mp3.Decoder` |

> 💡 **Чому окремі `aencodec`/`adecodec`?**  
> Різні кодеки мають різні методи для розрахунку тривалості пакету (`PacketDuration()`). Зберігаємо обидва для коректної конвертації таймінгів.

---

## ⚙️ 2. Options — конфігурація транскодування

```go
type Options struct {
    // Callback для визначення чи потрібне транскодування
    FindAudioDecoderEncoder func(codec av.AudioCodecData, i int) (
        need bool,           // чи потрібно транскодувати цей потік?
        dec av.AudioDecoder, // декодер для вхідного кодека
        enc av.AudioEncoder, // енкодер для цільового кодека
        err error,
    )
}
```

### 🔍 Як працює callback:

```go
// Приклад реалізації для HLS (конвертувати все у AAC)
options := transcode.Options{
    FindAudioDecoderEncoder: func(codec av.AudioCodecData, idx int) (bool, av.AudioDecoder, av.AudioEncoder, error) {
        // 1. Якщо вже AAC — не потрібно транскодувати
        if codec.Type() == av.AAC {
            return false, nil, nil, nil  // need=false
        }
        
        // 2. Створити декодер для вхідного кодека
        dec, err := avutil.DefaultHandlers.NewAudioDecoder(codec)
        if err != nil {
            return true, nil, nil, fmt.Errorf("decode %s: %w", codec.Type(), err)
        }
        
        // 3. Створити енкодер для AAC
        enc, err := avutil.DefaultHandlers.NewAudioEncoder(av.AAC)
        if err != nil {
            dec.Close()
            return true, nil, nil, fmt.Errorf("encode AAC: %w", err)
        }
        
        // 4. Повернути: need=true + decoder + encoder
        return true, dec, enc, nil
    },
}
```

### ✅ Ваш use-case: підтримка мультикодеків для CCTV

```go
// TranscodeConfig — конфігурація для різних джерел
type TranscodeConfig struct {
    // Кодеки, які приймаємо без транскодування
    PassthroughCodecs []av.CodecType
    
    // Бажаний вихідний кодек для аудіо
    TargetAudioCodec av.CodecType
}

func (cfg *TranscodeConfig) BuildFindCallback() func(av.AudioCodecData, int) (bool, av.AudioDecoder, av.AudioEncoder, error) {
    return func(codec av.AudioCodecData, idx int) (bool, av.AudioDecoder, av.AudioEncoder, error) {
        // 1. Перевірка чи кодек вже підходить
        for _, passthrough := range cfg.PassthroughCodecs {
            if codec.Type() == passthrough {
                return false, nil, nil, nil  // не потрібно транскодувати
            }
        }
        
        // 2. Створення декодера для вхідного кодека
        dec, err := avutil.DefaultHandlers.NewAudioDecoder(codec)
        if err != nil {
            return true, nil, nil, err
        }
        
        // 3. Створення енкодера для цільового кодека
        enc, err := avutil.DefaultHandlers.NewAudioEncoder(cfg.TargetAudioCodec)
        if err != nil {
            dec.Close()
            return true, nil, nil, err
        }
        
        log.Printf("Stream %d: transcoding %s → %s", idx, codec.Type(), cfg.TargetAudioCodec)
        return true, dec, enc, nil
    }
}
```

---

## 🔄 3. Transcoder — ядро транскодування

### Ініціалізація:

```go
func NewTranscoder(streams []av.CodecData, options Options) (*Transcoder, error) {
    self := &Transcoder{}
    
    for i, stream := range streams {
        ts := &tStream{codec: stream}  // за замовчуванням: passthrough
        
        // Тільки для аудіо-потоків
        if stream.Type().IsAudio() {
            if options.FindAudioDecoderEncoder != nil {
                ok, dec, enc, err := options.FindAudioDecoderEncoder(
                    stream.(av.AudioCodecData), i)
                
                if ok && err == nil {
                    // Налаштування транскодування для цього потоку
                    ts.timeline = &pktque.Timeline{}
                    ts.codec, _ = enc.CodecData()  // вихідний кодек
                    ts.aencodec = ts.codec.(av.AudioCodecData)
                    ts.adecodec = stream.(av.AudioCodecData)
                    ts.aenc = enc
                    ts.adec = dec
                }
            }
        }
        self.streams = append(self.streams, ts)
    }
    return self, nil
}
```

### 🔑 Ключовий метод: `audioDecodeAndEncode()`

```go
func (self *tStream) audioDecodeAndEncode(inpkt av.Packet) (outpkts []av.Packet, err error) {
    // 1. Декодування вхідного пакету у сирий аудіо-фрейм
    ok, frame, err := self.adec.Decode(inpkt.Data)
    if err != nil || !ok {
        return  // помилка або немає даних
    }
    
    // 2. Розрахунок тривалості вхідного пакету
    dur, err := self.adecodec.PacketDuration(inpkt.Data)
    if err != nil {
        return nil, fmt.Errorf("PacketDuration input: %w", err)
    }
    
    // 3. Додавання у таймлайн для корекції часу
    if Debug { fmt.Println("transcode: push", inpkt.Time, dur) }
    self.timeline.Push(inpkt.Time, dur)
    
    // 4. Кодування фрейму у вихідний формат (може дати кілька пакетів!)
    _outpkts, err := self.aenc.Encode(frame)
    if err != nil {
        return nil, err
    }
    
    // 5. Для кожного вихідного пакету: корекція таймінгу
    for _, _outpkt := range _outpkts {
        // Розрахунок тривалості вихідного пакету
        dur, err := self.aencodec.PacketDuration(_outpkt)
        if err != nil {
            return nil, fmt.Errorf("PacketDuration output: %w", err)
        }
        
        // Створення пакету з корегованим часом
        outpkt := av.Packet{Idx: inpkt.Idx, Data: _outpkt}
        outpkt.Time = self.timeline.Pop(dur)  // ← магія корекції часу!
        
        if Debug { fmt.Println("transcode: pop", outpkt.Time, dur) }
        outpkts = append(outpkts, outpkt)
    }
    
    return outpkts, nil
}
```

### 🔍 Чому `Push`/`Pop` для таймінгів?

```
Проблема: Різні кодеки мають різну гранулярність часу.
  • MP3: пакети по 1152 семпли @ 44.1kHz = ~26.1ms
  • AAC: пакети по 1024 семпли @ 48kHz = ~21.3ms

Без корекції:
  Вхід:  [0ms, 26ms] + [26ms, 52ms] + [52ms, 78ms]
  Вихід: [0ms, 21ms] + [21ms, 42ms] + [42ms, 63ms]  ← розсинхронізація!

З Timeline:
  Push(0ms, 26ms)   → "запам'ятали" що вхідний інтервал 26ms
  Pop(21ms)         → "віддали" перший вихідний пакет з часом 0ms
  Pop(21ms)         → "віддали" другий пакет з часом 21ms
  Pop(21ms)         → "віддали" третій пакет з часом 42ms
  Залишок 5ms "запам'ятовується" для наступного циклу

Результат: Вихідні таймінги вирівнюються під вхідні → A/V синхронізація зберігається ✓
```

---

## 🎛️ 4. Muxer/Demuxer Wrappers — прозора інтеграція

### Demuxer — транскодування при читанні:

```go
type Demuxer struct {
    av.Demuxer        // базовий демуксер (джерело)
    Options           // конфігурація транскодування
    transcoder *Transcoder
    outpkts    []av.Packet  // буфер для пакетів 1→N
}

func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
    // 1. Ініціалізація транскодера при першому виклику
    if err = self.prepare(); err != nil { return }
    
    // 2. Якщо є буферовані вихідні пакети — повертаємо їх
    if len(self.outpkts) > 0 {
        pkt = self.outpkts[0]
        self.outpkts = self.outpkts[1:]
        return
    }
    
    // 3. Читання сирого пакету з джерела
    rpkt, err := self.Demuxer.ReadPacket()
    if err != nil { return }
    
    // 4. Транскодування (може дати 0, 1 або N пакетів)
    self.outpkts, err = self.transcoder.Do(rpkt)
    if err != nil { return }
    
    // 5. Рекурсивний виклик для повернення першого пакету
    return self.ReadPacket()
}
```

### Muxer — транскодування при записі:

```go
type Muxer struct {
    av.Muxer          // базовий муксер (призначення)
    Options           // конфігурація транскодування
    transcoder *Transcoder
}

func (self *Muxer) WriteHeader(streams []av.CodecData) error {
    // 1. Створення транскодера з метаданими потоків
    self.transcoder, err = NewTranscoder(streams, self.Options)
    
    // 2. Отримання транскодованих метаданих
    newstreams, _ := self.transcoder.Streams()
    
    // 3. Запис заголовка з новими кодеками
    return self.Muxer.WriteHeader(newstreams)
}

func (self *Muxer) WritePacket(pkt av.Packet) error {
    // 1. Транскодування пакету
    outpkts, err := self.transcoder.Do(pkt)
    if err != nil { return err }
    
    // 2. Запис всіх вихідних пакетів (0, 1 або N)
    for _, outpkt := range outpkts {
        if err := self.Muxer.WritePacket(outpkt); err != nil {
            return err
        }
    }
    return nil
}
```

### ✅ Ваш use-case: інтеграція з HLS-генератором

```go
// HLSTranscoder — обгортка для створення HLS з транскодуванням
type HLSTranscoder struct {
    inputURI   string
    outputDir  string
    options    transcode.Options
}

func (h *HLSTranscoder) Start(ctx context.Context) error {
    // 1. Відкриття вхідного потоку
    demuxer, err := avutil.Open(h.inputURI)
    if err != nil { return err }
    
    // 2. Створення транскодуючого демуксера
    transDemux := &transcode.Demuxer{
        Demuxer: demuxer,
        Options: h.options,  // з callback для MP3→AAC
    }
    
    // 3. Відкриття HLS муксера
    hlsPath := filepath.Join(h.outputDir, "stream.m3u8")
    muxer, err := avutil.Create(hlsPath)
    if err != nil { return err }
    
    // 4. Створення транскодуючого муксера
    transMux := &transcode.Muxer{
        Muxer:   muxer,
        Options: h.options,
    }
    
    // 5. Копіювання потоків з автоматичним транскодуванням
    return avutil.CopyFile(transMux, transDemux)
}
```

---

## 🔄 5. Do() — основний метод транскодування

```go
func (self *Transcoder) Do(pkt av.Packet) (out []av.Packet, err error) {
    stream := self.streams[pkt.Idx]
    
    // Якщо потік налаштований на транскодування
    if stream.aenc != nil && stream.adec != nil {
        // Аудіо-транскодування (може дати кілька пакетів)
        return stream.audioDecodeAndEncode(pkt)
    }
    
    // Інакше: passthrough (відео або аудіо без змін)
    out = append(out, pkt)
    return
}
```

### 🎯 Сценарії повернення:

| Вхід | Транскодування | Вихід | Приклад |
|------|---------------|-------|---------|
| Відео (H.264) | Ні | 1 пакет | `pkt` без змін |
| Аудіо (AAC) | Ні (вже цільовий) | 1 пакет | `pkt` без змін |
| Аудіо (MP3→AAC) | Так | 1-3 пакети | 1 MP3 пакет → 1-3 AAC пакети |
| Аудіо (непідтримуваний) | Помилка | 0 пакетів + error | `err != nil` |

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"PacketDuration failed"** | Кодек не підтримує розрахунок тривалості | Переконайтеся, що обидва кодеки (`aencodec`/`adecodec`) реалізують `PacketDuration()` |
| **Аудіо розсинхронізоване** | `timeline.Push/Pop` не вирівнює час | Перевірте чи `dur` розраховується коректно для обох кодеків; додайте логування `Debug=true` |
| **Пам'ять росте через outpkts буфер** | 1 вхідний пакет → багато вихідних | Обмежте розмір `outpkts` або обробляйте їх пакетно; моніторьте `len(outpkts)` |
| **Транскодування не відбувається** | `FindAudioDecoderEncoder` повертає `need=false` | Додайте логування у callback; переконайтеся, що `codec.Type()` співпадає з очікуваним |
| **Закриття ресурсів** | `Close()` не викликається → leak | Використовуйте `defer transcoder.Close()` після успішного створення |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування енкодерів/декодерів:

```go
type CodecCache struct {
    mu       sync.RWMutex
    decoders map[av.CodecType]av.AudioDecoder
    encoders map[av.CodecType]av.AudioEncoder
}

func (c *CodecCache) GetDecoder(codec av.AudioCodecData) (av.AudioDecoder, error) {
    c.mu.RLock()
    if dec, ok := c.decoders[codec.Type()]; ok {
        c.mu.RUnlock()
        return dec, nil
    }
    c.mu.RUnlock()
    
    dec, err := avutil.DefaultHandlers.NewAudioDecoder(codec)
    if err != nil { return nil, err }
    
    c.mu.Lock()
    c.decoders[codec.Type()] = dec
    c.mu.Unlock()
    
    return dec, nil
}
```

### 2. Пакетна обробка для зменшення накладних витрат:

```go
// TranscodeBatch — транскодування кількох пакетів за один виклик
func (t *Transcoder) TranscodeBatch(packets []av.Packet) ([][]av.Packet, error) {
    results := make([][]av.Packet, 0, len(packets))
    
    for _, pkt := range packets {
        out, err := t.Do(pkt)
        if err != nil {
            return results, err
        }
        results = append(results, out)
    }
    return results, nil
}
```

### 3. Моніторинг ефективності транскодування:

```go
type TranscodeMetrics struct {
    PacketsIn      prometheus.Counter
    PacketsOut     prometheus.Counter
    TranscodeRatio prometheus.Gauge  // out/in
    ProcessingTime prometheus.Histogram
}

func (t *Transcoder) RecordMetrics(inCount, outCount int, duration time.Duration, m *TranscodeMetrics) {
    m.PacketsIn.Add(float64(inCount))
    m.PacketsOut.Add(float64(outCount))
    if inCount > 0 {
        m.TranscodeRatio.Set(float64(outCount) / float64(inCount))
    }
    m.ProcessingTime.Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції transcode

```go
// ✅ 1. Налаштування callback для вибору кодеків
options := transcode.Options{
    FindAudioDecoderEncoder: buildFindCallback(config),
}

// ✅ 2. Створення транскодуючого демуксера
transDemux := &transcode.Demuxer{
    Demuxer: inputDemuxer,
    Options: options,
}

// ✅ 3. Створення транскодуючого муксера
transMux := &transcode.Muxer{
    Muxer:   outputMuxer,
    Options: options,
}

// ✅ 4. Копіювання з автоматичним транскодуванням
err := avutil.CopyFile(transMux, transDemux)
if err != nil {
    log.Error("transcode failed", "err", err)
}

// ✅ 5. Закриття ресурсів
defer transDemux.Close()
defer transMux.Close()

// ✅ 6. Метрики
monitoring.TranscodePacketsIn.Add(float64(inCount))
monitoring.TranscodeLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [vdk transcode Package](https://pkg.go.dev/github.com/deepch/vdk/av/transcode) — GoDoc documentation
- 📄 [AAC vs MP3 Timing](https://wiki.multimedia.cx/index.php/Understanding_AAC) — відмінності у гранулярності пакетів
- 🎬 [HLS Audio Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — чому AAC потрібен для HLS
- 🧪 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади транскодування

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Кешуйте енкодери/декодери** — створення нового енкодера для кожного каналу дороге.
> 2. **Моніторьте `TranscodeRatio`** — якщо 1 вхідний пакет → 3+ вихідних, це може перевантажити буфери.
> 3. **Використовуйте `Debug=true` для відладки** таймінгів — логування `Push`/`Pop` допомагає виявити розсинхронізацію.
> 4. **Тестуйте з різними вхідними кодеками** — CCTV камери часто використовують MP3/PCM, які потребують транскодування у AAC для HLS.
> 5. **Додайте таймаути для `Encode()`/`Decode()`** — програмне транскодування може бути повільним на слабкому залізі.

Потрібен приклад інтеграції `transcode.Demuxer` з вашим `pubsub.Queue` для розподілу вже транскодованих потоків між кількома підписниками (HLS, WebSocket, архів)? Готовий допомогти! 🚀