# 🎬 Глибокий розбір `flv/flv.go` — FLV парсер/мультиплексор для CCTV HLS Processor

Це **архітектурне ядро** для роботи з форматом FLV (Flash Video) у вашому пайплайні. Реалізує двонаправлений потік: `FlvReader` парсить вхідні RTMP/FLV-дані у кадри, а `FlvWriter` пакує кадри назад у FLV для запису або ретрансляції. Розберемо детально:

---

## 📦 1. Структура файлу FLV (специфікація Adobe)

### 🔍 FLV Header (9 байт):

```
Байти 0-2:  'F' 'L' 'V' (магічна сигнатура)
Байт   3:   Version (0x01 для FLV v1)
Байт   4:   TypeFlags
            ├─ Біт 2: TypeFlagsAudio (1=є аудіо)
            └─ Біт 0: TypeFlagsVideo (1=є відео)
Байти 5-8:  DataOffset (UI32 BE) = 9 (розмір заголовку)
```

### 🔍 FLV File Body — чергування тегів та back-pointer'ів:

```
[PreviousTagSize0: 4 байти = 0]  ← завжди нуль на початку
[Tag1: FLV Tag]
[PreviousTagSize1: 4 байти = розмір Tag1 + 11]
[Tag2: FLV Tag]
[PreviousTagSize2: 4 байти = розмір Tag2 + 11]
...
```

### 🔍 Структура FLV Tag (11-байтовий заголовок + payload):

```
Байти 0:   TagType (8=audio, 9=video, 18=script)
Байти 1-3: DataSize (UI24 BE) — розмір payload
Байти 4-6: Timestamp (UI24 BE) — нижні 24 біти
Байт   7:  TimestampExtended (UI8) — старші 8 біт → 32-бітний timestamp
Байти 8-10: StreamID (завжди 0)

[Payload: DataSize байт]
[PreviousTagSize: 4 байти = 11 + DataSize]
```

> 💡 **Ключова особливість**: 32-бітний timestamp = `(TimestampExtended << 24) | Timestamp` → підтримка до ~24 днів без переповнення.

---

## 🔄 2. `FlvReader` — скінченний автомат для потокового парсингу

### 🔑 Дизайн: state machine + інкрементальний буфер

```go
type FLV_PARSER_STATE int
const (
    FLV_PARSER_INIT          // Старт
    FLV_PARSER_FILE_HEAD     // Читання 9-байтового заголовку
    FLV_PARSER_TAG_SIZE      // Пропуск PreviousTagSize (4 байти)
    FLV_PARSER_FLV_TAG       // Читання 11-байтового заголовку тега
    FLV_PARSER_DETECT_VIDEO  // Детекція відео-кодеку з першого байту payload
    FLV_PARSER_DETECT_AUDIO  // Детекція аудіо-кодеку
    FLV_PARSER_VIDEO_TAG     // Передача даних у VideoTagDemuxer
    FLV_PARSER_AUDIO_TAG     // Передача даних у AudioTagDemuxer
    FLV_PARSER_SCRIPT_TAG    // Пропуск метаданих (onMetaData)
)
```

### 🔧 Механізм `Input([]byte)` — обробка чанків будь-якого розміру:

```go
func (f *FlvReader) Input(data []byte) (err error) {
    // 1. Об'єднання з кешем для обробки часткових даних
    var buf []byte
    if len(f.cache) > 0 {
        f.cache = append(f.cache, data...)  // додати нові байти до кешу
        buf = f.cache
    } else {
        buf = data  // якщо кеш порожній — працюємо напряму
    }

    // 2. Цикл обробки: поки є дані → виконувати переходи станів
    for len(buf) > 0 {
        switch f.state {
        case FLV_PARSER_FILE_HEAD:
            if len(buf) < 9 { goto end }  // недостатньо даних → вийти, зачекати більше
            if err = f.readFlvHeader(buf[:9]); err != nil { goto end }
            buf = buf[9:]  // "з'їсти" оброблені байти
            f.state = FLV_PARSER_TAG_SIZE
            
        case FLV_PARSER_FLV_TAG:
            if len(buf) < 11 { goto end }
            f.flvTag.Decode(buf)  // розпарсити 11-байтовий заголовок
            buf = buf[11:]
            
            // 3. Динамічне створення демуксера при першому тегі
            if f.flvTag.TagType == uint8(VIDEO_TAG) {
                if f.videoDemuxer == nil {
                    f.state = FLV_PARSER_DETECT_VIDEO  // потрібно визначити кодек
                } else {
                    f.state = FLV_PARSER_VIDEO_TAG     // демуксер вже є
                }
            }
            // ... аналогічно для AUDIO_TAG
            
        case FLV_PARSER_VIDEO_TAG:
            // Перевірка: чи вистачає даних для всього payload тега?
            if f.flvTag.DataSize > uint32(len(buf)) {
                goto end  // частковий тег → зачекати наступний чанк
            }
            // 4. Делегування демуксеру
            err = f.videoDemuxer.Decode(buf[:f.flvTag.DataSize])
            if err != nil { return err }
            buf = buf[f.flvTag.DataSize:]  // "з'їсти" payload
            f.state = FLV_PARSER_TAG_SIZE  // наступний: PreviousTagSize
        }
    }
    
end:
    // 5. Оновлення кешу: зберегти необроблені байти для наступного виклику
    if len(buf) > 0 {
        if len(f.cache) > 0 {
            f.cache = buf  // замінити кеш на залишок
        } else {
            f.cache = append(f.cache, buf...)  // створити новий кеш
        }
    } else {
        f.cache = f.cache[:0]  // очистити кеш, якщо все оброблено
    }
    return nil
}
```

### 🎯 Чому такий дизайн критичний для CCTV:

| Проблема | Рішення у `FlvReader` |
|----------|----------------------|
| **Мережеві чанки довільного розміру** | Інкрементальний буфер `cache` + `goto end` для паузи |
| **Динамічне визначення кодеку** | Стани `DETECT_VIDEO/AUDIO` + фабрика демуксерів |
| **Часткові теги на межі чанків** | Перевірка `DataSize > len(buf)` перед обробкою |
| **Ефективність пам'яті** | `cache[:0]` для очищення без реалокації |

---

## 🎞️ 3. Динамічне створення демуксерів: `createVideoTagDemuxer`

### 🔧 Логіка детекції відео-кодеку:

```go
func (f *FlvReader) createVideoTagDemuxer(cid FLV_VIDEO_CODEC_ID) error {
    switch cid {
    case FLV_AVC:  f.videoDemuxer = NewAVCTagDemuxer()   // H.264
    case FLV_HEVC: f.videoDemuxer = NewHevcTagDemuxer()  // H.265
    default:       return errors.New("unsupport video codec")
    }
    
    // Реєстрація callback для отримання розпакованих кадрів
    f.videoDemuxer.OnFrame(func(codecid codec.CodecID, frame []byte, cts int) {
        // 1. Розрахунок 32-бітного DTS з 24+8 біт
        dts := uint32(f.flvTag.TimestampExtended)<<24 | f.flvTag.Timestamp
        
        // 2. Розрахунок PTS: DTS + CompositionTimeOffset (для B-frames)
        pts := dts + uint32(cts)
        
        // 3. Передача у пайплайн
        f.OnFrame(codecid, frame, pts, dts)
    })
    return nil
}
```

### 🔍 Формула timestamp у FLV:

```
FLV зберігає 32-бітний timestamp у двох частинах:
• Timestamp (байти 4-6 заголовку): молодші 24 біти
• TimestampExtended (байт 7): старші 8 біт

Розрахунок:
dts = (TimestampExtended << 24) | Timestamp

Приклад:
Timestamp = 0x00F0B0 (15720), TimestampExtended = 0x01
dts = (0x01 << 24) | 0x00F0B0 = 0x01000000 + 0xF0B0 = 16,843,952 ms ≈ 4.68 годин
```

### 🎯 Для чого `cts` (Composition Time Offset)?

```
У H.264/HEVC з B-frames порядок декодування (DTS) ≠ порядок відтворення (PTS):

Кадр:   I0  B1  B2  P3  B4  B5  P6
DTS:    0   1   2   3   4   5   6   (порядок у потоці)
PTS:    0   3   4   6   7   8   9   (порядок відтворення)

FLV зберігає:
• DTS у заголовку тега
• CTS = PTS - DTS у AVCPacketType=1 payload

Розрахунок у коді:
pts = dts + uint32(cts)  // коректне відновлення часу відтворення
```

> 💡 **Практичне значення**: Без урахування `cts` B-frames відтворюватимуться не в тому порядку → "стрибаюче" відео, розсинхронізація з аудіо.

---

## 🎵 4. Аудіо-демуксинг: `createAudioTagDemuxer`

### 🔧 Особливість: аудіо не має CTS

```go
func (f *FlvReader) createAudioTagDemuxer(formats FLV_SOUND_FORMAT) error {
    switch formats {
    case FLV_AAC:        f.audioDemuxer = NewAACTagDemuxer()
    case FLV_G711A, FLV_G711U, FLV_MP3:
                         f.audioDemuxer = NewG711Demuxer(formats)
    default: return errors.New("unsupport audio codec")
    }
    
    f.audioDemuxer.OnFrame(func(codecid codec.CodecID, frame []byte) {
        dts := uint32(f.flvTag.TimestampExtended)<<24 | f.flvTag.Timestamp
        pts := dts  // ← аудіо завжди PTS == DTS (немає B-frames)
        f.OnFrame(codecid, frame, pts, dts)
    })
    return nil
}
```

### 🎯 Чому аудіо простіше за відео:

| Аспект | Відео (H.264/HEVC) | Аудіо (AAC/G.711) |
|--------|-------------------|------------------|
| **B-frames** | Можливі → потрібен CTS | Неможливі → PTS==DTS |
| **Параметри** | SPS/PPS/VPS у окремому тегу | ASC у окремому тегі (AAC) або відсутні (G.711) |
| **Конвертація** | AVCC → Annex-B | Raw AAC → ADTS (тільки для AAC) |
| **Детекція кодеку** | З першого байту payload | З перших 4 біт заголовку тега |

---

## ✍️ 5. `FlvWriter` — зворотній шлях: кадри → FLV

### 🔧 Архітектура: делегування `FlvMuxer`

```go
type FlvWriter struct {
    writer io.Writer  // кінцевий вихід: файл, socket, pipe
    muxer  *FlvMuxer  // логіка пакування кадрів у теги
}
```

### 🔧 Запис заголовку:

```go
func (f *FlvWriter) WriteFlvHeader() error {
    var flvhdr [9]byte
    flvhdr[0] = 'F'; flvhdr[1] = 'L'; flvhdr[2] = 'V'  // сигнатура
    flvhdr[3] = 0x01  // version 1
    flvhdr[4] = 0x05  // TypeFlags: 0b00000101 = audio+video present
    flvhdr[5] = 0; flvhdr[6] = 0  // reserved
    flvhdr[7] = 0; flvhdr[8] = 9  // DataOffset = 9
    
    f.writer.Write(flvhdr[:9])
    
    // PreviousTagSize0 = 0
    f.writer.Write([]byte{0,0,0,0})
    return nil
}
```

### 🔧 Запис аудіо-кадру (приклад для AAC):

```go
func (f *FlvWriter) WriteAAC(data []byte, pts uint32, dts uint32) error {
    // 1. Ініціалізація муксера при першому кадрі
    if f.muxer.audioMuxer == nil {
        f.muxer.SetAudioCodeId(FLV_AAC)  // створити AACMuxer
    } else {
        // 2. Захист від зміни кодеку "на льоту"
        if _, ok := f.muxer.audioMuxer.(*AACMuxer); !ok {
            panic("audio codec change")  // ⚠️ panic у продакшені — ризик!
        }
    }
    return f.writeAudio(data, pts, dts)
}

func (f *FlvWriter) writeAudio(data []byte, pts uint32, dts uint32) error {
    // 1. Делегувати пакування у FlvMuxer
    tags, err := f.muxer.WriteAudio(data, pts, dts)
    if err != nil { return err }
    
    // 2. Записати кожен згенерований тег
    for _, tag := range tags {
        f.writer.Write(tag)  // сам тег (заголовок + payload)
        f.writePreviousTagSize(uint32(len(tag)))  // +4 байти back-pointer
    }
    return nil
}
```

### 🔧 `writePreviousTagSize` — запис back-pointer:

```go
func (f *FlvWriter) writePreviousTagSize(preTagSize uint32) error {
    tagsize := make([]byte, 4)
    binary.BigEndian.PutUint32(tagsize, preTagSize)  // UI32 BE
    f.writer.Write(tagsize)
    return nil
}
```

> 💡 **Чому back-pointer важливий**: Він дозволяє програвачам швидко пропускати теги при seek, не парсячи весь payload.

---

## 🐞 6. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **`panic` при зміні кодеку**:
   ```go
   if _, ok := f.muxer.audioMuxer.(*AACMuxer); !ok {
       panic("audio codec change")  // ← у продакшені це crash сервера!
   }
   // Краще повернути помилку:
   return errors.New("audio codec mismatch")
   ```

2. **Race condition у кеші `FlvReader`**:
   ```go
   // Якщо Input() викликається з кількох горутин → data race на f.cache!
   // Рішення: додати mutex або гарантувати однопоточний виклик
   
   type FlvReader struct {
       mu sync.Mutex
       // ...
   }
   func (f *FlvReader) Input(data []byte) error {
       f.mu.Lock()
       defer f.mu.Unlock()
       // ... існуюча логіка
   }
   ```

3. **Необроблений `ScriptTag` (onMetaData)**:
   ```go
   case FLV_PARSER_SCRIPT_TAG:
       // TODO MateData tag ← досі не реалізовано!
       buf = buf[f.flvTag.DataSize:]  // просто пропускаємо
       // Але метадані можуть містити критичну інформацію:
       // • duration, width, height для HLS-плейлиста
       // • codec initialization для клієнтів
   ```

4. **Відсутня валідація `TimestampExtended`**:
   ```go
   // Якщо TimestampExtended > 0xFF (неможливо, але гіпотетично) → переповнення
   // Краще додати:
   if f.flvTag.TimestampExtended > 0xFF {
       return errors.New("invalid timestamp extended")
   }
   ```

5. **Ефективність `append` у кеші**:
   ```go
   // f.cache = append(f.cache, data...) може реалокувати при кожному виклику
   // Краще: попередньо виділити буфер з запасом
   type FlvReader struct {
       cache []byte
       // Додати:
       cacheCap int  // поточна ємність
   }
   func CreateFlvReader() *FlvReader {
       return &FlvReader{
           cache: make([]byte, 0, 65536),  // 64KB початкова ємність
           cacheCap: 65536,
       }
   }
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для безпечного розрахунку timestamp
func decodeFlvTimestamp(ts uint32, tsExt uint8) uint64 {
    return uint64(tsExt)<<24 | uint64(ts&0xFFFFFF)
}

// 2. Метрики для моніторингу парсингу
func (f *FlvReader) recordMetrics(tagType uint8, codecID codec.CodecID, dataSize uint32) {
    metrics.FLVTagsReceived.WithLabelValues(
        map[uint8]string{8:"audio", 9:"video", 18:"script"}[tagType],
        codec.CodecString(codecID),
    ).Inc()
    metrics.FLVTagSize.Observe(float64(dataSize))
}

// 3. Підтримка seek у `FlvReader` (для запису/редагування)
type FlvReader struct {
    // ...
    index []TagIndexEntry  // індекс тегів для швидкого пошуку
}

type TagIndexEntry struct {
    Offset   uint64  // позиція у потоці
    DTS      uint32  // timestamp для seek
    KeyFrame bool    // чи є точка входу для відтворення
}
```

---

## 🎯 7. Інтеграція з вашим CCTV HLS Processor

### 📍 У `RTMPReceiver` — обробка вхідного потоку:

```go
type RTMPReceiver struct {
    flvReader *flv.FlvReader
    assembler *SegmentAssembler
}

func (r *RTMPReceiver) Start(conn net.Conn) {
    r.flvReader = flv.CreateFlvReader()
    
    // Реєстрація callback: отримання кадрів від демуксера
    r.flvReader.OnFrame = func(codecid codec.CodecID, frame []byte, pts, dts uint32) {
        // Передача у segmentAssembler з коректними часовими мітками
        switch {
        case codecid.IsVideo():
            r.assembler.HandleVideoFrame(codecid, frame, pts, dts)
        case codecid.IsAudio():
            r.assembler.HandleAudioFrame(codecid, frame, pts)
        }
    }
    
    // Читання з socket у циклі
    buf := make([]byte, 32*1024)  // 32KB буфер
    for {
        n, err := conn.Read(buf)
        if err != nil { break }
        
        // Інкрементальний парсинг: можна викликати з будь-яким розміром чанку
        if err := r.flvReader.Input(buf[:n]); err != nil {
            logger.Error("FLV parse error", "error", err)
            break
        }
    }
}
```

### 📍 У `HLSRecorder` — запис у FLV для архіву:

```go
type HLSRecorder struct {
    flvWriter *flv.FlvWriter
    file      *os.File
}

func (r *HLSRecorder) Start(segmentPath string) error {
    file, _ := os.Create(segmentPath)
    r.file = file
    r.flvWriter = flv.CreateFlvWriter(file)
    
    // Запис заголовку
    r.flvWriter.WriteFlvHeader()
    
    // Реєстрація обробників кадрів від segmentAssembler
    r.assembler.OnVideoFrame = func(codecid codec.CodecID, frame []byte, pts, dts uint32) {
        switch codecid {
        case codec.CODECID_VIDEO_H264:
            r.flvWriter.WriteH264(frame, pts, dts)  // конвертація Annex-B → FLV
        case codec.CODECID_VIDEO_H265:
            r.flvWriter.WriteH265(frame, pts, dts)
        }
    }
    r.assembler.OnAudioFrame = func(codecid codec.CodecID, frame []byte, pts uint32) {
        switch codecid {
        case codec.CODECID_AUDIO_AAC:
            r.flvWriter.WriteAAC(frame, pts, pts)  // AAC: PTS==DTS
        }
    }
    
    return nil
}
```

### 📍 У `VideoManifestProxy` — синхронізація часових міток:

```go
func calculateHLSTimestamps(flvDTS uint32, flvPTS uint32, streamStartTime time.Time) (hlsPTS time.Time) {
    // FLV timestamp: milliseconds since stream start
    // HLS PROGRAM-DATE-TIME: absolute UTC time
    
    return streamStartTime.Add(time.Duration(flvPTS) * time.Millisecond)
}

// При генерації #EXT-X-PROGRAM-DATE-TIME:
func generateProgramDateTimeTag(pts uint32, baseTime time.Time) string {
    absoluteTime := baseTime.Add(time.Duration(pts) * time.Millisecond)
    return fmt.Sprintf("#EXT-X-PROGRAM-DATE-TIME:%s", absoluteTime.Format(time.RFC3339Nano))
}
```

---

## 🧭 Висновок: чому цей модуль — міст між протоколами

| Компонент | Роль у CCTV HLS Processor |
|-----------|---------------------------|
| **FlvReader** | Потоковий парсер RTMP/FLV → кадри з коректними PTS/DTS → живлення `segmentAssembler` |
| **State Machine** | Обробка чанків будь-якого розміру без блокування → low-latency прийом |
| **Динамічні демуксери** | Авто-детекція H.264/H.265/AAC/G.711 → підтримка різних камер без конфігурації |
| **FlvWriter** | Зворотня конвертація кадрів → FLV для архіву/ретрансляції → сумісність з існуючими системами |
| **Timestamp Handling** | Коректне відновлення 32-бітних часів + CTS для B-frames → точна синхронізація аудіо/відео |

> 🔑 **Головна ідея**: Цей код — **протокольний адаптер**, який абстрагує складність формату FLV від бізнес-логіки вашого пайплайну. Без нього `segmentAssembler` змушений був би знати деталі бітової структури тегів, розрахунку timestamp'ів, конвертації AVCC/ADTS — це порушило б принцип єдиної відповідальності.

💡 **Фінальна порада**: Додайте інтеграційний тест, який:
1. Генерує сирий FLV-потік з H.264 IDR + AAC кадрами
2. Пропускає через `FlvReader` → отримує кадри з PTS/DTS
3. Передає у `FlvWriter` → записує у новий файл
4. Валідує через `ffprobe` що вихідний файл ідентичний вхідному (окрім можливих метаданих)

Це гарантує, що парсер/мультиплексер коректно обробляє повний цикл: прийом → обробка → запис без втрати синхронізації або пошкодження даних.