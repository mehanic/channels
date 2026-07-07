# 📦 Глибокий розбір `mpeg2/ps_muxer.go` — Program Stream мультиплексор для MPEG-1/2

Це **реалізація мультиплексингу формату Program Stream (PS)** згідно специфікації ISO/IEC 13818-1. На відміну від демуксера, цей модуль виконує зворотню операцію: приймає сирі відео/аудіо кадри та пакує їх у валідні PS-пакети для запису у .mpg файли або сумісності з legacy системами відеоспостереження. Розберемо архітектурно:

---

## 🧱 1. Архітектура: від кадрів до Program Stream

### 🔍 Потік даних у муксері:

```
[Raw Video/Audio Frames]
         ↓
[PSMuxer.Write()] ← цей файл!
├─ Конвертація часу: ms → 90 kHz clock (PTS/DTS × 90)
├─ Детекція ключових кадрів (IDR/IRAP)
├─ Авто-вставка AUD (Access Unit Delimiter)
├─ Пакування у PES пакети (з заголовками)
├─ Генерація pack header + system header + PSM
         ↓
[Program Stream байти] → файл .mpg / UDP потік / архів
```

### 🔑 Чому потрібен окремий муксер для PS?

| Сценарій | Рішення через PSMuxer |
|----------|---------------------|
| **Експорт архіву** | Конвертація HLS-сегментів → .mpg для сумісності зі старим ПЗ |
| **Legacy камери** | Генерація PS-потоку для пристроїв, що не підтримують TS/fMP4 |
| **Міжсистемний обмін** | Універсальний контейнер для передачі між різними виробниками |
| **Локальний запис** | Економія місця: PS має менший overhead (~1-2%) ніж TS (~5-10%) |

---

## 🎛️ 2. `PSMuxer` структура — стан та конфігурація

### 🔧 Ключові поля:

```go
type PSMuxer struct {
    system     *System_header        // Метадані потоків: бітрейти, буфери, bound'и
    psm        *Program_stream_map   // Мапінг stream_id → codec type (H264=0x1B, AAC=0x0F)
    OnPacket   func(pkg []byte)      // Callback: відправка згенерованих байт у файл/потік
    firstframe bool                  // Прапорець: чи потрібно вставити ініціалізаційні заголовки
}
```

### 🔧 Конструктор `NewPsMuxer()`:

```go
func NewPsMuxer() *PSMuxer {
    muxer := new(PSMuxer)
    muxer.firstframe = true  // Перший кадр → згенерувати pack/system/PSM заголовки
    
    // Ініціалізація system header з дефолтними параметрами
    muxer.system = new(System_header)
    muxer.system.Rate_bound = 26234  // ~6.5 Mbps: максимальний бітрейт програми
    
    // Ініціалізація PSM (Program Stream Map)
    muxer.psm = new(Program_stream_map)
    muxer.psm.Current_next_indicator = 1  // Поточна версія мапи (не наступна)
    muxer.psm.Program_stream_map_version = 1  // Версія для детекції змін
    
    return muxer
}
```

### 🎯 Значення дефолтних параметрів:

| Параметр | Значення | Практичне значення |
|----------|----------|-------------------|
| `Rate_bound = 26234` | 26,234 × 50 = ~1.3 Mbps | Обмеження загального бітрейту програми (специфікація: значення × 50 б/с) |
| `P_STD_buffer_size_bound = 400` (відео) | 400 × 128 = 51,200 байт | Розмір буфера декодера для відео (запобігання underflow) |
| `P_STD_buffer_size_bound = 32` (аудіо) | 32 × 128 = 4,096 байт | Менший буфер для аудіо через нижчу затримку |
| `P_STD_buffer_bound_scale` | 1=відео (1024-byte units), 0=аудіо (128-byte units) | Різні одиниці виміру для відео/аудіо буферів |

> 💡 **Практичне значення**: Ці параметри критичні для сумісності з апаратними декодерами. Неправильні значення → буфер переповнення/спустошення → артефакти відтворення.

---

## ➕ 3. `AddStream()` — реєстрація елементарних потоків

### 🔧 Логіка додавання відео та аудіо:

```go
func (muxer *PSMuxer) AddStream(cid PS_STREAM_TYPE) uint8 {
    if cid == PS_STREAM_H265 || cid == PS_STREAM_H264 {
        // === ВІДЕО ПОТІК ===
        // Stream ID діапазон: 0xE0-0xFF (відео)
        es := NewElementary_Stream(uint8(PES_STREAM_VIDEO) + muxer.system.Video_bound)
        es.P_STD_buffer_bound_scale = 1  // 1024-byte units для відео
        es.P_STD_buffer_size_bound = 400  // 51,200 байт буфер
        
        muxer.system.Streams = append(muxer.system.Streams, es)
        muxer.system.Video_bound++  // Інкремент лічильника відео потоків
        
        // Додати мапінг у PSM: codec type → stream_id
        muxer.psm.Stream_map = append(muxer.psm.Stream_map, 
            NewElementary_stream_elem(uint8(cid), es.Stream_id))
        muxer.psm.Program_stream_map_version++  // Інкремент версії мапи
        
        return es.Stream_id  // Повернути ID для подальшого використання у Write()
        
    } else {
        // === АУДІО ПОТІК ===
        // Stream ID діапазон: 0xC0-0xDF (аудіо)
        es := NewElementary_Stream(uint8(PES_STREAM_AUDIO) + muxer.system.Audio_bound)
        es.P_STD_buffer_bound_scale = 0  // 128-byte units для аудіо
        es.P_STD_buffer_size_bound = 32   // 4,096 байт буфер
        
        // Аналогічна логіка для system/psm...
    }
}
```

### 🔍 Чому `PES_STREAM_VIDEO + Video_bound`?

```
Специфікація MPEG-2 PS:
• Відео потоки: stream_id = 0xE0 + index (0-15) → 0xE0, 0xE1, ..., 0xEF
• Аудіо потоки: stream_id = 0xC0 + index (0-31) → 0xC0, 0xC1, ..., 0xDF

Приклад:
• Перший відео потік: Video_bound=0 → stream_id = 0xE0 + 0 = 0xE0
• Другий відео потік: Video_bound=1 → stream_id = 0xE0 + 1 = 0xE1
• Перший аудіо потік: Audio_bound=0 → stream_id = 0xC0 + 0 = 0xC0

Це дозволяє мультиплексити до 16 відео + 32 аудіо потоків в одному файлі.
```

### 🎯 Практичне застосування у вашому пайплайні:

```go
// У PSFileWriter при ініціалізації запису:
func (w *PSFileWriter) Start(outputPath string, videoCodec, audioCodec codec.CodecID) error {
    muxer := NewPsMuxer()
    
    // Реєстрація потоків з отриманням stream_id
    videoSID := muxer.AddStream(convertCodecToPS(videoCodec))  // H264 → PS_STREAM_H264
    audioSID := muxer.AddStream(convertCodecToPS(audioCodec))  // AAC → PS_STREAM_AAC
    
    // Налаштування callback для запису у файл
    file, _ := os.Create(outputPath)
    muxer.OnPacket = func(pkg []byte) {
        file.Write(pkg)  // Запис згенерованих байт у файл
    }
    
    // Збереження muxer для подальших викликів Write()
    w.muxer = muxer
    w.videoSID = videoSID
    w.audioSID = audioSID
    
    return nil
}
```

---

## ✍️ 4. `Write()` — ядро мультиплексингу: від кадрів до байт

### 🔧 Підпис методу:

```go
func (muxer *PSMuxer) Write(sid uint8, frame []byte, pts uint64, dts uint64) error
// sid: stream_id отриманий від AddStream()
// frame: сирі дані (Annex-B для відео, ADTS для аудіо)
// pts/dts: часові мітки у мілісекундах (конвертуються у 90 kHz всередині)
```

### 🔍 Крок 1: Пошук потоку за stream_id

```go
var stream *Elementary_stream_elem = nil
for _, es := range muxer.psm.Stream_map {
    if es.Elementary_stream_id == sid {
        stream = es
        break
    }
}
if stream == nil {
    return errNotFound  // ⚠️ Помилка: потік не зареєстровано через AddStream()
}
if len(frame) <= 0 {
    return nil  // Порожній кадр → нічого не робити
}
```

### 🔍 Крок 2: Аналіз відео-кадру (тільки для H.264/H.265)

```go
var withaud bool = false    // Чи містить кадр AUD NAL unit?
var idr_flag bool = false   // Чи є IDR/IRAP кадр (точка входу)?
var first bool = true       // Прапорець для першого NAL у пакеті
var vcl bool = false        // Чи є VCL (Video Coding Layer) NAL?

if stream.Stream_type == uint8(PS_STREAM_H264) || stream.Stream_type == uint8(PS_STREAM_H265) {
    // Розділити вхідний буфер на окремі NAL units
    codec.SplitFrame(frame, func(nalu []byte) bool {
        if stream.Stream_type == uint8(PS_STREAM_H264) {
            nalu_type := codec.H264NaluTypeWithoutStartCode(nalu)
            
            if nalu_type == codec.H264_NAL_AUD {
                withaud = true    // AUD знайдено → не вставляти дублікат
                return false      // Зупинити ітерацію (AUD не потрібен у payload)
                
            } else if codec.IsH264VCLNaluType(nalu_type) {
                if nalu_type == codec.H264_NAL_I_SLICE {
                    idr_flag = true  // IDR кадр → точка синхронізації
                }
                vcl = true           // Знайдено відео-дані
                return false         // Зупинити після першого VCL
            }
            return true  // Продовжити пошук (SPS/PPS/SEI не впливають на логіку)
            
        } else {  // H.265 логіка
            nalu_type := codec.H265NaluTypeWithoutStartCode(nalu)
            if nalu_type == codec.H265_NAL_AUD {
                withaud = true
                return false
            } else if codec.IsH265VCLNaluType(nalu_type) {
                // IRAP frames: types 16-21 (BLA/CRA/IDR)
                if nalu_type >= codec.H265_NAL_SLICE_BLA_W_LP && 
                   nalu_type <= codec.H265_NAL_SLICE_CRA {
                    idr_flag = true
                }
                vcl = true
                return false
            }
            return true
        }
    })
}
```

### 🎯 Чому зупиняємо ітерацію після першого VCL?

```
Оптимізація продуктивності: нам потрібно знати тільки:
1. Чи є AUD у кадрі? → щоб не дублювати
2. Чи є IDR/IRAP? → для data_alignment_indicator у PES
3. Чи є хоча б один VCL? → щоб знати, що це відео-кадр, а не тільки параметри

Після отримання цих трьох відповідей подальший парсинг зайвий.
```

### 🔍 Крок 3: Конвертація часу та ініціалізація writer

```go
// Конвертація: мілісекунди → 90 kHz clock (специфікація MPEG)
dts = dts * 90  // 1 ms = 90 ticks @ 90 kHz
pts = pts * 90

bsw := codec.NewBitStreamWriter(1024)  // Буфер для генерації байт
```

### 🔍 Крок 4: Генерація pack header (завжди на початку)

```go
var pack PSPackHeader
// SCR (System Clock Reference) = DTS - 3600 (40ms offset для синхронізації)
pack.System_clock_reference_base = dts - 3600
pack.System_clock_reference_extension = 0
pack.Program_mux_rate = 6106  // ~305 kbps: дефолтний бітрейт мультиплексу

pack.Encode(bsw)  // Серіалізація у байти
```

### 📐 Формула SCR та mux_rate:

```
SCR (System Clock Reference):
• 33 біти base + 9 біт extension = 42 біти загальний час
• Частота: 27 MHz → конвертація у 90 kHz для PTS/DTS
• Offset -3600 (40ms) забезпечує буфер для синхронізації аудіо/відео

Program_mux_rate:
• Формула: значення × 50 = біти/секунду
• 6106 × 50 = 305,300 bps ≈ 305 kbps
• Це дефолт; у продакшені варто розраховувати динамічно:
  mux_rate = (video_bitrate + audio_bitrate) / 50 + 10% запас
```

### 🔍 Крок 5: Генерація system header + PSM (тільки на початку або при IDR)

```go
if muxer.firstframe || idr_flag {
    // System header: метадані про всі потоки програми
    muxer.system.Encode(bsw)
    
    // Program Stream Map: мапінг stream_id → codec type
    muxer.psm.Encode(bsw)
    
    muxer.firstframe = false  // Більше не генерувати ініціалізацію
}
```

### 🎯 Чому генеруємо при IDR?

```
IDR/IRAP кадр — точка входу для декодера. Якщо клієнт починає 
відтворення з середини файлу (seek), йому потрібні:
1. System header: щоб знати параметри буферів/бітрейтів
2. PSM: щоб знати, який stream_id відповідає якому кодеку

Без цього декодер не зможе коректно ініціалізуватись → артефакти.
```

### 🔍 Крок 6: Пакування у PES пакети з розбиттям на частини

```go
pespkg := NewPesPacket()
for len(frame) > 0 {  // Цикл: розбивати великі кадри на кілька PES
    peshdrlen := 13  // Базова довжина PES заголовку з PTS+DTS
    
    // Налаштування обов'язкових полів PES
    pespkg.Stream_id = sid
    pespkg.PTS_DTS_flags = 0x03  // Обидві часові мітки присутні
    pespkg.PES_header_data_length = 10  // 5 байт PTS + 5 байт DTS
    pespkg.Pts = pts
    pespkg.Dts = dts
    
    // Data alignment indicator для IDR кадрів
    if idr_flag {
        pespkg.Data_alignment_indicator = 1
    }
    
    // Авто-вставка AUD, якщо відсутній у вхідному кадрі
    if first && !withaud && vcl {
        if stream.Stream_type == uint8(PS_STREAM_H264) {
            pespkg.Pes_payload = append(pespkg.Pes_payload, H264_AUD_NALU...)  // 6 байт
            peshdrlen += 6
        } else if stream.Stream_type == uint8(PS_STREAM_H265) {
            pespkg.Pes_payload = append(pespkg.Pes_payload, H265_AUD_NALU...)  // 7 байт
            peshdrlen += 7
        }
    }
    
    // Розрахунок довжини пакета з обмеженням 0xFFFF (65,535 байт)
    if peshdrlen+len(frame) >= 0xFFFF {
        // Великий кадр → розбити на кілька PES пакетів
        pespkg.PES_packet_length = 0xFFFF  // Спеціальне значення: "необмежено"
        pespkg.Pes_payload = append(pespkg.Pes_payload, frame[0:0xFFFF-peshdrlen]...)
        frame = frame[0xFFFF-peshdrlen:]   // Залишок для наступної ітерації
    } else {
        // Кадр поміщається в один PES
        pespkg.PES_packet_length = uint16(peshdrlen + len(frame))
        pespkg.Pes_payload = append(pespkg.Pes_payload, frame[0:]...)
        frame = frame[:0]  // Очищення: все оброблено
    }
    
    // Серіалізація PES пакету у байти
    pespkg.Encode(bsw)
    
    // Очищення payload для наступної ітерації
    pespkg.Pes_payload = pespkg.Pes_payload[:0]
    
    // Відправка згенерованих байт у callback
    if muxer.OnPacket != nil {
        muxer.OnPacket(bsw.Bits())
    }
    
    bsw.Reset()  // Скидання буфера для наступного пакета
    first = false  // AUD вставляється тільки перед першим NAL
}
return nil
```

### 🎯 Чому розбиваємо великі кадри?

```
Обмеження специфікації: PES_packet_length — 16-бітне поле → макс. 65,535 байт.

Для 4K відео кадр може бути >100KB → неможливо вмістити в один PES.

Рішення: циклічне розбиття:
[Кадр 150KB] → [PES 1: 64KB] + [PES 2: 64KB] + [PES 3: 22KB]

Важливо: тільки перший PES містить PTS/DTS заголовок, 
наступні можуть мати спрощений заголовок (оптимізація, не реалізована тут).
```

---

## ⚠️ 5. Потенційні баги та критичні проблеми

### ❗ Критичні проблеми:

1. **Некоректна обробка `PES_packet_length = 0xFFFF`**:
   ```go
   if peshdrlen+len(frame) >= 0xFFFF {
       pespkg.PES_packet_length = 0xFFFF  // "необмежено"
       // Але потім:
       pespkg.Pes_payload = append(pespkg.Pes_payload, frame[0:0xFFFF-peshdrlen]...)
       // ← Це обмежує payload, суперечить значенню 0xFFFF!
       
       // Правильно: при 0xFFFF payload читається до кінця потоку,
       // але тут ми штучно обрізаємо. Краще:
       if peshdrlen+len(frame) > 0xFFFF {
           // Розбити на кілька пакетів з коректними довжинами
           chunkSize := 0xFFFF - peshdrlen - 10  // запас для безпеки
           pespkg.PES_packet_length = uint16(peshdrlen + chunkSize)
           // ...
       }
   }
   ```

2. **Відсутня валідація `sid` перед використанням**:
   ```go
   // Якщо sid не знайдено у PSM → return errNotFound
   // Але errNotFound не визначено у цьому файлі!
   // Має бути: var errNotFound = errors.New("stream not found")
   ```

3. **Race condition у спільних структурах**:
   ```go
   // muxer.system та muxer.psm модифікуються у AddStream() і читаються у Write()
   // Якщо викликаються з різних горутин → data race!
   // Рішення: додати sync.RWMutex до PSMuxer
   ```

4. **Жорстко закодовані параметри бітрейту**:
   ```go
   pack.Program_mux_rate = 6106  // ~305 kbps
   // Це не підходить для:
   // • 4K відео (потрібно >10 Mbps)
   // • Мульти-поточних записів (сума бітрейтів)
   // Краще: розраховувати динамічно або приймати як параметр
   ```

5. **Необроблений випадок аудіо у циклі розбиття**:
   ```go
   // Логіка розбиття кадру на кілька PES реалізована,
   // але для аудіо це рідко потрібно (кадри зазвичай <1KB).
   // Однак: якщо аудіо-кадр >64KB (теоретично можливо) → баг.
   // Краще: універсальна логіка для будь-якого типу потоку.
   ```

6. **Втрата точності при конвертації часу**:
   ```go
   dts = dts * 90  // ms → 90 kHz
   // Якщо dts > 2^53/90 ≈ 10^13 ms (~300 років) → втрата точності у float64
   // Практично не критично, але краще використовувати uint64 арифметику.
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для розрахунку mux_rate на основі бітрейтів
func CalculateMuxRate(videoBitrate, audioBitrate int) uint32 {
    totalBps := videoBitrate + audioBitrate + (videoBitrate/10)  // +10% запас
    return uint32(totalBps / 50)  // специфікація: значення × 50 = bps
}

// 2. Безпечне розбиття великих кадрів
func splitFrameForPES(frame []byte, maxPayload int) [][]byte {
    var chunks [][]byte
    for len(frame) > 0 {
        chunkSize := len(frame)
        if chunkSize > maxPayload {
            chunkSize = maxPayload
        }
        chunks = append(chunks, frame[:chunkSize])
        frame = frame[chunkSize:]
    }
    return chunks
}

// 3. Метрики для моніторингу мультиплексингу
func (muxer *PSMuxer) recordMetrics(streamType PS_STREAM_TYPE, frameSize int, pts uint64) {
    metrics.PSMuxBytesWritten.WithLabelValues(
        codec.StreamTypeString(streamType),
    ).Add(float64(frameSize))
    
    metrics.PSMuxPTS.Observe(float64(pts) / 90000.0)  // конвертація у секунди
}

// 4. Юніт-тести для edge cases
func TestPSMuxer_Write_LargeFrame(t *testing.T) {
    muxer := NewPsMuxer()
    sid := muxer.AddStream(PS_STREAM_H264)
    
    var packets [][]byte
    muxer.OnPacket = func(pkg []byte) {
        packets = append(packets, pkg)
    }
    
    // Кадр 100KB → має розбитися на кілька PES
    largeFrame := make([]byte, 100*1024)
    largeFrame[0] = 0x00; largeFrame[1] = 0x00; largeFrame[2] = 0x00; largeFrame[3] = 0x01
    largeFrame[4] = 0x65  // H.264 IDR NAL type
    
    err := muxer.Write(sid, largeFrame, 1000, 1000)  // pts/dts = 1000ms
    if err != nil {
        t.Fatalf("Write error: %v", err)
    }
    
    if len(packets) < 2 {
        t.Errorf("large frame should be split into multiple PES packets, got %d", len(packets))
    }
    
    // Перевірити, що перший пакет містить pack/system/PSM заголовки
    // (тут потрібен парсинг згенерованих байт для валідації)
}
```

---

## 🎯 6. Інтеграція з вашим CCTV HLS Processor

### 📍 У `PSFileWriter` — запис архівних сегментів:

```go
type PSFileWriter struct {
    muxer    *PSMuxer
    file     *os.File
    videoSID uint8
    audioSID uint8
}

func (w *PSFileWriter) WriteFrame(codecid codec.CodecID, frame []byte, pts, dts uint32) error {
    // 1. Вибрати stream_id за кодеком
    sid := w.videoSID
    if codecid.IsAudio() {
        sid = w.audioSID
    }
    
    // 2. Конвертувати внутрішній codec.CodecID → PS_STREAM_TYPE
    psType := convertCodecToPSType(codecid)
    
    // 3. Викликати муксер (час уже у ms, конвертація всередині)
    return w.muxer.Write(sid, frame, uint64(pts), uint64(dts))
}

func (w *PSFileWriter) Close() error {
    // Фіналізація: записати кінцеві маркери якщо потрібно
    // (специфікація PS не вимагає явного end code, але можна додати)
    return w.file.Close()
}
```

### 📍 У `ArchiveExporter` — конвертація HLS → PS для legacy систем:

```go
func ExportHLSToPS(hlsSegments []string, outputPath string) error {
    writer := NewPSFileWriter(outputPath, codec.CODECID_VIDEO_H264, codec.CODECID_AUDIO_AAC)
    defer writer.Close()
    
    for _, segPath := range hlsSegments {
        // Парсинг TS сегменту → кадри
        frames, err := parseTSSegment(segPath)
        if err != nil { continue }
        
        // Запис кожного кадру у PS формат
        for _, frame := range frames {
            writer.WriteFrame(frame.CodecID, frame.Data, frame.PTS, frame.DTS)
        }
    }
    return nil
}
```

### 📍 У метриках — моніторинг якості мультиплексингу:

```go
func (muxer *PSMuxer) recordHealthMetrics() {
    // Розмір згенерованих пакетів: відхилення вказує на проблеми
    metrics.PSMuxPacketSize.Observe(float64(len(lastGeneratedPacket)))
    
    // Частота генерації pack/system/PSM заголовків
    if muxer.firstframe {
        metrics.PSMuxInitHeadersPending.Inc()
    }
    
    // Детекція "завислих" потоків (дані не відправляються)
    // (потрібно додати лічильник останнього виклику OnPacket)
}
```

---

## 🧭 Висновок: чому PS муксер — критичний компонент для сумісності

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **Авто-вставка AUD** | Гарантія синхронізації кадрів у декодерах | Розсинхронізація → артефакти відтворення у legacy плеєрах |
| **IDR детекція + data_alignment** | Точки входу для seek/відновлення потоку | Неможливість швидкого перемотування у архівних записах |
| **Розбиття великих кадрів** | Підтримка 4K/високобітрейтних потоків | Переповнення PES_packet_length → пошкоджені файли |
| **System header + PSM генерація** | Метадані для ініціалізації декодерів | Декодери не розпізнають потоки → "невалідний файл" помилки |
| **Конвертація часу (ms → 90 kHz)** | Сумісність зі специфікацією MPEG | Неправильна синхронізація аудіо/відео у відтворенні |

> 🔑 **Головна ідея**: Цей код — **міст між сучасним та legacy світами**. Він дозволяє вашому HLS-орієнтованому пайплайну експортувати дані у формат, зрозумілий старим системам, без втрати якості або синхронізації. Без нього ви були б змушені підтримувати окремий пайплайн для legacy експорту, що подвоїло б складність підтримки.

💡 **Фінальна порада**: 
1. Виправте логіку `PES_packet_length = 0xFFFF` для коректного розбиття великих кадрів
2. Додайте динамічний розрахунок `Program_mux_rate` на основі реальних бітрейтів
3. Реалізуйте `sync.RWMutex` для потокобезпеки при паралельному записі
4. Додайте юніт-тести для великих кадрів, IDR детекції та конвертації часу
5. Реалізуйте валідацію згенерованих байт через `ffprobe` у інтеграційних тестах

Це перетворить цей муксер з "робочого прототипу" на "надійний компонент продакшен-рівня" для експорту архівів та сумісності з legacy системами у вашому CCTV HLS Processor.