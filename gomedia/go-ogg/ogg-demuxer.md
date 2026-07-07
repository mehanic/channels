# 🎵 Глибокий розбір `ogg/demuxer.go` — головний демуксер Ogg контейнера

Це **архітектурне ядро** для демуксингу формату Ogg у вашому CCTV HLS Processor. Файл реалізує скінченний автомат для інкрементального парсингу Ogg сторінок, збірки пакетів з сегментів, детекції кодеків та екстракції медіа-кадрів з часовими мітками. Розберемо архітектурно:

---

## 🧱 1. Архітектура: скінченний автомат для інкрементального парсингу

### 🔑 Стани демуксера:

```go
type DemuxState int
const (
    DEMUX_PAGE_HEAD    DemuxState = iota  // Очікування заголовку сторінки (27 байт + сегменти)
    DEMUX_PAGE_PAYLOAD                     // Очікування payload сторінки
)
```

### 🔧 Головний цикл `Input()`:

```go
func (demuxer *Demuxer) Input(buf []byte) (err error) {
    for {  // Цикл: обробляти дані поки не вичерпано буфер
        switch demuxer.state {
        case DEMUX_PAGE_HEAD:
            // === ФАЗА 1: Парсинг заголовку сторінки ===
            // 1. Перевірка наявності мінімуму даних для заголовку
            // 2. Читання кількості сегментів (байт 26)
            // 3. Перевірка наявності всіх сегментів у буфері
            // 4. Збірка повного заголовку з кешу + нового буфера
            // 5. Парсинг заголовку через readPage()
            // 6. Реєстрація/оновлення потоку (oggStream)
            // 7. Перехід у стан DEMUX_PAGE_PAYLOAD
            
        case DEMUX_PAGE_PAYLOAD:
            // === ФАЗА 2: Збірка пакетів з сегментів ===
            // 1. Читання payload сторінки (page.payloadLen байт)
            // 2. Детекція розривів у послідовності сторінок (lost=1)
            // 3. Збірка пакетів з сегментів за правилами Ogg:
            //    • Сегмент < 255 байт = кінець пакету
            //    • Сегмент = 255 байт = продовження пакету
            // 4. Обробка кожного пакету через readPacket()
            // 5. Збереження залишку для наступної сторінки
            // 6. Повернення у стан DEMUX_PAGE_HEAD
        }
    }
}
```

### 🎯 Чому скінченний автомат критичний?

```
Ogg потік може приходити фрагментами:
• Мережеві пакети: 1-1.5KB
• Файлове читання: чанки довільного розміру
• Розрізані сторінки на межі чанків

Без скінченного автомата:
• Потрібно буферизувати весь потік перед обробкою → висока затримка
• Неможлива обробка live-потоків у реальному часі

Зі скінченним автоматом:
• Інкрементальна обробка: кожен виклик Input() обробляє доступні дані
• Кешування неповних даних у headCache/page.cache
• Миттєва реакція на нові дані без очікування повного потоку
```

---

## 📦 2. Структури даних: від сторінки до кадру

### 🔸 `oggStream` — стан окремого потоку у мультиплексі:

```go
type oggStream struct {
    streamId    uint32           // Унікальний serial number потоку
    cid         codec.CodecID    // Визначений кодек (або UNRECOGNIZED)
    parser      oggParser        // Специфічний парсер для цього кодеку
    currentPage *oggPage         // Поточна сторінка для збірки пакетів
    cache       []byte           // Буфер для збірки пакетів між сторінками
    lost        uint8            // Прапорець: чи були втрачені сторінки
}
```

### 🔸 `oggPage` — представлення Ogg сторінки:

```go
type oggPage struct {
    streamId        uint32   // serial number потоку
    pageSeq         uint32   // Послідовний номер сторінки (для детекції розривів)
    granulePos      uint64   // Granule position: часові мітки для кодеку
    payloadLen      uint32   // Загальна довжина payload
    segmentsCount   uint8    // Кількість сегментів у сторінці
    seqmentTable    []uint8  // Таблиця довжин сегментів (0-255 байт кожен)
    cache           []byte   // Буфер для збірки payload
    packets         [][]byte // Зібрані пакети з сегментів
    isFirstPage     bool     // Прапорець: перша сторінка потоку
    isContinuePacket bool    // Прапорець: сторінка продовжує пакет з попередньої
    eos             bool     // Прапорець: кінець потоку
}
```

### 🔸 `VideoParam` / `AudioParam` — метадані потоків:

```go
type VideoParam struct {
    CodecId     codec.CodecID  // Визначений кодек
    Width       uint32         // Роздільна здатність
    Height      uint32
    FrameRate   uint32         // Частота кадрів
    Aspectratio uint32         // Співвідношення сторін
    ExtraData   []byte         // Ініціалізаційні дані для декодера
}

type AudioParam struct {
    CodecId        codec.CodecID
    SampleRate     uint32         // Частота дискретизації
    ChannelCount   uint32         // Кількість каналів
    InitialPadding uint32         // Pre-skip для Opus
    ExtraData      []byte
}
```

### 🎯 Чому окремі структури для метаданів?

```
Метадані потрібні для:
• Ініціалізації декодерів (extradata)
• Розрахунку тривалості сегментів (frameRate, sampleRate)
• Валідації вхідного потоку (роздільна здатність, канали)
• Генерації HLS-плейлиста (CODECS, RESOLUTION, SAMPLE-FREQUENCY)

Інкапсуляція у VideoParam/AudioParam дозволяє:
1. Отримати всі параметри одним викликом (GetVideoParam())
2. Типізований доступ до полів без парсингу extradata
3. Легке тестування: можна створити тестові параметри без реального потоку
```

---

## 🔍 3. `Input()` — детальний розбір двох фаз

### 🔸 ФАЗА 1: DEMUX_PAGE_HEAD — парсинг заголовку сторінки

#### Крок 1: Перевірка мінімуму даних

```go
headLen := 0
if len(demuxer.headCache)+len(buf) < 27 {
    // Недостатньо даних навіть для базового заголовку (27 байт)
    demuxer.headCache = append(demuxer.headCache, buf...)
    return nil  // очікуємо більше даних
}
```

#### Крок 2: Читання кількості сегментів

```go
segCount := 0
if len(demuxer.headCache) >= 27 {
    segCount = int(demuxer.headCache[26])  // байт 26 = segments_count
} else {
    segCount = int(buf[26-len(demuxer.headCache)])  // з нового буфера
}
```

#### Крок 3: Перевірка наявності всіх сегментів

```go
// Заголовок = 27 байт + segCount байт (таблиця сегментів)
if len(demuxer.headCache)+len(buf) < int(segCount)+27 {
    demuxer.headCache = append(demuxer.headCache, buf...)
    return nil  // очікуємо решту заголовку
}
headLen = int(segCount) + 27  // повна довжина заголовку
```

#### Крок 4: Збірка повного заголовку

```go
var hdr []byte
if len(demuxer.headCache) > 0 {
    // Об'єднати кеш + нові дані
    hdr = demuxer.headCache
    hdr = append(hdr, buf[:headLen-len(demuxer.headCache)]...)
} else {
    // Весь заголовок у новому буфері
    hdr = buf
}
```

#### Крок 5: Парсинг заголовку через `readPage()`

```go
page, err := readPage(hdr)  // ⚠️ readPage() не показана у цьому файлі!
if err != nil {
    return err
}
if demuxer.OnPage != nil {
    demuxer.OnPage(page)  // callback для моніторингу
}
```

#### Крок 6: Реєстрація/оновлення потоку

```go
stream, found := demuxer.streams[page.streamId]
if found {
    // Детекція розриву у послідовності сторінок
    if stream.currentPage.pageSeq+1 != page.pageSeq {
        stream.lost = 1  // втрачені сторінки!
        // Обробити залишкові дані з попередньої сторінки
        if demuxer.OnPacket != nil {
            demuxer.OnPacket(stream.streamId, stream.currentPage.granulePos, stream.cache, 1)
        }
        err = demuxer.readPacket(stream, stream.cache)
        if err != nil { return err }
        stream.cache = stream.cache[:0]  // очистити буфер
    } else {
        stream.lost = 0  // послідовність відновлена
    }
} else {
    // Новий потік: ініціалізація
    stream = &oggStream{
        currentPage: page,
        streamId:    page.streamId,
        cache:       make([]byte, 0, 1024),
        cid:         codec.CODECID_UNRECOGNIZED,  // ще не визначено
    }
    demuxer.streams[page.streamId] = stream
}
stream.currentPage = page
demuxer.currentStream = stream
```

#### Крок 7: Перехід у наступний стан

```go
demuxer.state = DEMUX_PAGE_PAYLOAD
buf = buf[headLen-len(demuxer.headCache):]  // обрізати оброблений заголовок
if len(demuxer.headCache) > 0 {
    demuxer.headCache = demuxer.headCache[:0]  // очистити кеш
}
// Продовжити цикл з обробки payload
```

### 🔸 ФАЗА 2: DEMUX_PAGE_PAYLOAD — збірка пакетів з сегментів

#### Крок 1: Читання payload сторінки

```go
stream := demuxer.currentStream
page := stream.currentPage
needLen := int(page.payloadLen) - len(page.cache)  // скільки ще потрібно

if needLen > len(buf) {
    // Недостатньо даних для повного payload
    page.cache = append(page.cache, buf...)
    return nil  // очікуємо більше даних
}

// Збірка повного payload
var tmp []byte
if len(page.cache) > 0 {
    page.cache = append(page.cache, buf[0:needLen]...)
    buf = buf[needLen:]
    tmp = page.cache
} else {
    tmp = buf[0:page.payloadLen]
    buf = buf[page.payloadLen:]
}
```

#### Крок 2: Обробка розривів та продовжень пакетів

```go
idx := 0
// Випадок 1: втрачені сторінки + продовження пакету
if stream.lost > 0 && page.isContinuePacket {
    removeLen := 0
    for ; idx < int(page.segmentsCount); idx++ {
        if page.seqmentTable[idx] < 255 {
            // Знайдено кінець пакету → пропустити пошкоджені дані
            removeLen += int(page.seqmentTable[idx])
        } else {
            // Продовження пошкодженого пакету → видалити з буфера
            tmp = tmp[removeLen:]
            break
        }
    }
} 
// Випадок 2: нормальний потік + продовження пакету
else if stream.lost == 0 && page.isContinuePacket {
    appendLen := 0
    for ; idx < int(page.segmentsCount); idx++ {
        appendLen += int(page.seqmentTable[idx])
        if page.seqmentTable[idx] < 255 {
            // Кінець пакету: додати у потік та відправити у callback
            stream.cache = append(stream.cache, tmp[:appendLen]...)
            if demuxer.OnPacket != nil {
                demuxer.OnPacket(stream.streamId, stream.currentPage.granulePos, stream.cache, 0)
            }
            page.packets = append(page.packets, stream.cache)
            stream.cache = stream.cache[:0]  // очистити для наступного
            tmp = tmp[appendLen:]  // обрізати оброблені дані
        }
    }
}
```

#### Крок 3: Збірка нових пакетів

```go
start := 0
packetLen := 0
for ; idx < int(page.segmentsCount); idx++ {
    packetLen += int(page.seqmentTable[idx])
    if page.seqmentTable[idx] < 255 {
        // Знайдено повний пакет
        packet := tmp[start : start+packetLen]
        if demuxer.OnPacket != nil {
            demuxer.OnPacket(stream.streamId, stream.currentPage.granulePos, packet, 0)
        }
        page.packets = append(page.packets, packet)
        start = start + packetLen
        packetLen = 0  // скинути для наступного пакету
    }
}
```

#### Крок 4: Обробка кожного пакету

```go
for _, pkt := range page.packets {
    if err := demuxer.readPacket(stream, pkt); err != nil {
        return err  // помилка парсингу пакету
    }
}
```

#### Крок 5: Збереження залишку для наступної сторінки

```go
if packetLen > 0 {
    // Незавершений пакет: зберегти у кеш потоку
    stream.cache = append(stream.cache, tmp[start:]...)
}
page.cache = page.cache[:0]  // очистити кеш сторінки
demuxer.state = DEMUX_PAGE_HEAD  // повернутись до очікування нового заголовку
```

### 🎯 Чому логіка сегментів така складна?

```
Ogg використовує сегменти для гнучкого пакування:
• Сегмент < 255 байт: останній сегмент пакету
• Сегмент = 255 байт: продовження пакету у наступному сегменті

Це дозволяє:
1. Пакувати пакети довільного розміру (до 64KB)
2. Ефективно використовувати простір (мінімальний overhead)
3. Відновлюватися після втрати сегментів (детекція за <255)

У вашому коді:
• page.seqmentTable[idx] < 255 → кінець пакету → обробити та відправити
• page.seqmentTable[idx] == 255 → продовжити накопичення у stream.cache
• isContinuePacket → сторінка продовжує пакет з попередньої → об'єднати з stream.cache
```

---

## 🔍 4. `readPacket()` — детекція кодеку та обробка пакетів

### 🔧 Логіка детекції кодеку:

```go
func (demuxer *Demuxer) readPacket(stream *oggStream, packet []byte) error {
    // 1. Детекція кодеку на першій сторінці
    if stream.currentPage.isFirstPage {
        if stream.cid == codec.CODECID_UNRECOGNIZED {
            demuxer.findCodec(stream, packet)  // пошук за magic bytes
        }
    }

    // 2. Перевірка, чи кодек визначено
    if stream.cid == codec.CODECID_UNRECOGNIZED {
        return errors.New("not find codec id ")
    }

    // 3. Обробка за типом кодеку
    switch stream.cid {
    case codec.CODECID_AUDIO_OPUS:
        // Opus: заголовки на початку, медіа-пакети далі
        if stream.currentPage.isFirstPage || stream.currentPage.granulePos == 0 {
            // Парсинг заголовку (OpusHead/OpusTags)
            err := stream.parser.header(stream, packet)
            if err != nil { return err }
            
            // Збереження метаданів для пайплайну
            if demuxer.aparam == nil {
                opus, _ := stream.parser.(*opusDemuxer)
                demuxer.aparam = &AudioParam{
                    CodecId:        codec.CODECID_AUDIO_OPUS,
                    SampleRate:     uint32(opus.ctx.SampleRate),
                    ChannelCount:   uint32(opus.ctx.ChannelCount),
                    InitialPadding: uint32(opus.ctx.Preskip),
                    ExtraData:      opus.extradata,
                }
            }
        } else {
            // Медіа-пакет: конвертація у кадр + часові мітки
            frame, pts, dts := stream.parser.packet(stream, packet)
            if demuxer.OnFrame != nil {
                demuxer.OnFrame(stream.streamId, stream.cid, frame, pts, dts, stream.lost)
            }
        }
        
    case codec.CODECID_VIDEO_VP8:
        // Аналогічна логіка для VP8...
        
    default:
        return errors.New("unsupport  codec id ")
    }
    return nil
}
```

### 🔧 `findCodec()` — пошук за magic bytes:

```go
func (demuxer *Demuxer) findCodec(stream *oggStream, packet []byte) {
    for _, ogg_codec := range codecs {  // глобальний список з init()
        // Перевірка: чи починається пакет з magic bytes кодеку?
        if bytes.Equal(ogg_codec.magic(), packet[0:ogg_codec.magicSize()]) {
            stream.cid = ogg_codec.codecid()  // встановити внутрішній ID
            stream.parser = createParser(stream.cid)  // створити специфічний парсер
            return
        }
    }
}
```

### 🎯 Чому детекція тільки на першій сторінці?

```
Ogg специфікація вимагає:
• Перша сторінка потоку містить ідентифікаційний заголовок (OpusHead, OVP80)
• Цей заголовок описує параметри кодеку для ініціалізації декодера
• Наступні сторінки містять тільки медіа-дані

Детекція на першій сторінці дозволяє:
1. Уникнути зайвих перевірок для кожного пакету
2. Гарантувати, що параметри визначені до обробки медіа-даних
3. Підтримувати мультиплексні потоки з різними кодеками

У вашому коді: якщо пакет не збігається з жодним magic → cid залишається UNRECOGNIZED → помилка.
```

---

## 🐞 5. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Відсутня валідація довжини пакету у `findCodec()`**:
   ```go
   if bytes.Equal(ogg_codec.magic(), packet[0:ogg_codec.magicSize()]) {
       // Якщо len(packet) < ogg_codec.magicSize() → panic: index out of range!
   
   // Краще додати перевірку:
   if len(packet) < ogg_codec.magicSize() {
       continue  // пропустити цей кодек, спробувати наступний
   }
   ```

2. **Необроблений випадок `nil` parser у `readPacket()`**:
   ```go
   // Після findCodec() parser може бути nil, якщо createParser() поверне nil
   // Але код одразу використовує stream.parser.header() → panic!
   
   // Краще:
   if stream.parser == nil {
       return fmt.Errorf("parser not initialized for codec %v", stream.cid)
   }
   ```

3. **Race condition у `streams` map**:
   ```go
   // Якщо Input() викликається з кількох горутин → data race на demuxer.streams!
   // Рішення: додати sync.RWMutex до Demuxer
   type Demuxer struct {
       mu sync.RWMutex
       streams map[uint32]*oggStream
       // ...
   }
   func (d *Demuxer) Input(buf []byte) error {
       d.mu.Lock()
       defer d.mu.Unlock()
       // ... існуюча логіка ...
   }
   ```

4. **Витік пам'яті у `page.cache` та `stream.cache`**:
   ```go
   // При кожному виклику Input() створюються нові слайси для cache
   // Але старі слайси можуть посилатися на великі масиви → витік пам'яті
   
   // Краще: використовувати sync.Pool для буферів або обмежити розмір кешу
   const MAX_CACHE_SIZE = 10 * 1024 * 1024  // 10MB
   if len(stream.cache) > MAX_CACHE_SIZE {
       logger.Warn("stream cache overflow, resetting", "size", len(stream.cache))
       stream.cache = stream.cache[:0]
   }
   ```

5. **Некоректна обробка `lost=1` для першого пакету**:
   ```go
   // У DEMUX_PAGE_PAYLOAD:
   if stream.lost > 0 && page.isContinuePacket {
       // ... пропуск пошкоджених даних ...
   }
   // Але якщо lost=1 і isFirstPage → should we skip header parsing?
   // Поточний код може спробувати парсити пошкоджений заголовок → помилка
   
   // Краще: додати перевірку isFirstPage при lost=1
   if stream.lost > 0 && page.isContinuePacket && !page.isFirstPage {
       // ... існуюча логіка ...
   }
   ```

6. **Відсутня обробка `eos` (end of stream)**:
   ```go
   // Якщо page.eos = true → потік завершено
   // Але код не обробляє це спеціальним чином
   // Краще: викликати flush() для відправки залишкових даних
   if page.eos {
       demuxer.flushStream(stream)  // нова функція для фіналізації
   }
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для безпечної детекції кодеку
func (demuxer *Demuxer) safeFindCodec(stream *oggStream, packet []byte) bool {
    for _, ogg_codec := range codecs {
        if len(packet) < ogg_codec.magicSize() {
            continue
        }
        if bytes.Equal(ogg_codec.magic(), packet[0:ogg_codec.magicSize()]) {
            stream.cid = ogg_codec.codecid()
            stream.parser = createParser(stream.cid)
            return stream.parser != nil  // повернути успіх тільки якщо parser створено
        }
    }
    return false
}

// 2. Метрики для моніторингу демуксингу
func (demuxer *Demuxer) recordMetrics(streamId uint32, packetSize int, pts uint64) {
    metrics.OggDemuxBytesReceived.WithLabelValues(
        fmt.Sprintf("stream_%d", streamId),
    ).Add(float64(packetSize))
    
    metrics.OggDemuxPTS.Observe(float64(pts) / 48000.0)  // для Opus: конвертація у секунди
}

// 3. Фіналізація потоків при завершенні
func (demuxer *Demuxer) flushStream(stream *oggStream) {
    if len(stream.cache) > 0 && stream.cid != codec.CODECID_UNRECOGNIZED {
        // Відправити залишкові дані як останній пакет
        frame, pts, dts := stream.parser.packet(stream, stream.cache)
        if demuxer.OnFrame != nil {
            demuxer.OnFrame(stream.streamId, stream.cid, frame, pts, dts, 1)  // lost=1 для фіналізації
        }
        stream.cache = stream.cache[:0]
    }
    // Видалити потік з мапи, якщо більше не потрібен
    delete(demuxer.streams, stream.streamId)
}

// 4. Юніт-тести для edge cases
func TestDemuxer_Input_SplitPage(t *testing.T) {
    demuxer := NewDemuxer()
    var frames []byte
    
    demuxer.OnFrame = func(streamId uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
        frames = append(frames, frame...)
    }
    
    // Створити Ogg сторінку, розрізану на два чанки
    page := createTestOggPage()  // helper function
    chunk1 := page.headerAndFirstHalf()
    chunk2 := page.secondHalf()
    
    // Перший виклик: неповна сторінка
    err1 := demuxer.Input(chunk1)
    if err1 != nil {
        t.Errorf("unexpected error on partial page: %v", err1)
    }
    if len(frames) > 0 {
        t.Error("no frames should be extracted from partial page")
    }
    
    // Другий виклик: завершення сторінки
    err2 := demuxer.Input(chunk2)
    if err2 != nil {
        t.Errorf("unexpected error on complete page: %v", err2)
    }
    if len(frames) == 0 {
        t.Error("frames should be extracted from complete page")
    }
}
```

---

## 🎯 6. Інтеграція з вашим CCTV HLS Processor

### 📍 У `OggFileReader` — читання .ogg/.webm файлів:

```go
type OggFileReader struct {
    demuxer *ogg.Demuxer
    file    *os.File
}

func (r *OggFileReader) Process(filePath string, assembler *SegmentAssembler) error {
    file, err := os.Open(filePath)
    if err != nil { return err }
    defer file.Close()
    
    r.demuxer = ogg.NewDemuxer()
    r.demuxer.OnFrame = func(streamId uint32, cid codec.CodecID, frame []byte, pts, dts uint64, lost int) {
        // Конвертація часових міток у формат пайплайну
        if cid == codec.CODECID_AUDIO_OPUS {
            // Opus: PTS у семплах @ 48 kHz → ms
            pts = pts * 1000 / 48000
            dts = dts * 1000 / 48000
        } else if cid == codec.CODECID_VIDEO_VP8 {
            // VP8: PTS у кадрах → ms через frameRate
            if vp8Param := r.demuxer.GetVideoParam(); vp8Param != nil && vp8Param.FrameRate > 0 {
                pts = pts * 1000 / uint64(vp8Param.FrameRate)
                dts = dts * 1000 / uint64(vp8Param.FrameRate)
            }
        }
        
        // Ігнорувати втрачені пакети (lost=1) або логувати попередження
        if lost == 1 {
            logger.Warn("lost packet in Ogg stream", "stream", streamId, "codec", cid)
            return
        }
        
        // Передача у segmentAssembler
        switch {
        case cid.IsVideo():
            assembler.HandleVideoFrame(cid, frame, pts, dts)
        case cid.IsAudio():
            assembler.HandleAudioFrame(cid, frame, pts)
        }
    }
    
    // Інкрементальне читання файлу
    buf := make([]byte, 64*1024)
    for {
        n, err := file.Read(buf)
        if n > 0 {
            if err := r.demuxer.Input(buf[:n]); err != nil && err != io.EOF {
                return fmt.Errorf("demux error: %w", err)
            }
        }
        if err == io.EOF {
            break
        }
    }
    return nil
}
```

### 📍 У `WebRTCToHLSConverter` — транскодування WebRTC → HLS:

```go
func (conv *WebRTCToHLSConverter) onOggData(data []byte) {
    // 1. Інкрементальний демуксинг
    if err := conv.oggDemuxer.Input(data); err != nil {
        logger.Error("Ogg demux error", "error", err)
        return
    }
    
    // 2. Ініціалізація при отриманні параметрів
    if conv.videoWriter == nil && conv.oggDemuxer.GetVideoParam() != nil {
        vp := conv.oggDemuxer.GetVideoParam()
        conv.videoWriter = NewTSVideoWriter(vp.CodecId, vp.Width, vp.Height, vp.FrameRate)
    }
    if conv.audioWriter == nil && conv.oggDemuxer.GetAudioParam() != nil {
        ap := conv.oggDemuxer.GetAudioParam()
        conv.audioWriter = NewTSAudioWriter(ap.CodecId, ap.SampleRate, ap.ChannelCount)
    }
    
    // 3. Запис кадрів у сегменти (OnFrame callback вже налаштовано)
}
```

### 📍 У метриках — моніторинг якості демуксингу:

```go
func (demuxer *Demuxer) recordHealthMetrics() {
    // Кількість активних потоків
    metrics.OggDemuxActiveStreams.Observe(float64(len(demuxer.streams)))
    
    // Розмір кешів (детекція "завислих" потоків)
    for sid, stream := range demuxer.streams {
        if len(stream.cache) > 0 {
            metrics.OggDemuxBufferedBytes.WithLabelValues(
                fmt.Sprintf("stream_%d", sid),
            ).Observe(float64(len(stream.cache)))
        }
    }
    
    // Частота втрачених сторінок
    var lostCount int
    for _, stream := range demuxer.streams {
        if stream.lost > 0 {
            lostCount++
        }
    }
    metrics.OggDemuxLostStreams.Observe(float64(lostCount))
}
```

---

## 🧭 Висновок: чому цей демуксер — критичний компонент для сумісності

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **Скінченний автомат `Input()`** | Інкрементальна обробка потоків реального часу | Висока затримка, неможливість live-трансляції |
| **Детекція кодеку за magic bytes** | Автоматична ідентифікація потоків без попередньої конфігурації | Неможливість обробки мультиплексних Ogg файлів |
| **Збірка пакетів з сегментів** | Коректна обробка розрізаних даних між сторінками | Втрата кадрів при розрізанні на межі сегментів |
| **Granule position конвертація** | Точні часові мітки для синхронізації аудіо/відео | Розсинхронізація, "стрибаюче" відео у відтворенні |
| **Інкапсуляція через oggParser** | Легке додавання нових кодеків без зміни ядра демуксера | Неможливість підтримки нових форматів без переписування |

> 🔑 **Головна ідея**: Цей код — **протокольний адаптер**, який перетворює "брудний" Ogg потік з мережі або файлу на "чисті" медіа-кадри з коректними часовими мітками. Без нього ваш `segmentAssembler` змушений був би знати деталі формату Ogg, сегментів, granule position — це порушило б принцип єдиної відповідальності.

💡 **Фінальна порада**: 
1. Додайте валідацію довжини пакету перед доступом до `packet[0:magicSize]`
2. Реалізуйте `sync.RWMutex` для потокобезпеки при паралельній обробці
3. Додайте обмеження розміру кешів для захисту від переповнення пам'яті
4. Реалізуйте фіналізацію потоків при `eos=true` або завершенні входу
5. Додайте юніт-тести для розрізаних сторінок, втрачених пакетів та граничних значень granule position

Це перетворить цей демуксер з "робочого прототипу" на "надійний компонент продакшен-рівня" для обробки Ogg/Opus/VP8 потоків у вашому CCTV HLS Processor.