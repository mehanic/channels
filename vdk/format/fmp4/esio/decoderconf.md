# 🎵 Глибокий розбір: esio — DecoderConfigDescriptor для MPEG-4 Systems

Цей файл — **реалізація парсингу та серіалізації DecoderConfigDescriptor**, структури з стандарту MPEG-4 Systems (ISO/IEC 14496-1), що використовується для опису параметрів аудіо/відео декодерів у форматах на кшталт MP4, MPEG-TS, або RTMP.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема esio.DecoderConfigDescriptor

```
┌────────────────────────────────────────┐
│ 📦 esio.DecoderConfigDescriptor        │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • DecoderConfigDescriptor — структура │
│  • parseDecoderConfig() — парсинг      │
│  • appendTo() — серіалізація           │
│  • DecoderConfigFromCodecData() — фабрика│
│                                         │
│  📊 Формат MPEG-4 DecoderConfig:       │
│  [1-byte objectType]                   │
│  [1-byte streamType + flags]           │
│  [3-byte bufferSize]                   │
│  [4-byte maxBitrate]                   │
│  [4-byte avgBitrate]                   │
│  [optional sub-descriptors...]         │
│                                         │
│  🔄 Потік даних:                        │
│  av.CodecData → DecoderConfigDescriptor│
│  → binary (MPEG-4 Systems)             │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. DecoderConfigDescriptor — структура опису декодера

### Поля та їх призначення:

```go
type DecoderConfigDescriptor struct {
    ObjectType ObjectType  // тип об'єкта: 0x40 = Audio ISO/IEC 14496-3
    StreamType StreamType  // тип потоку: 0x05 = AudioStream
    BufferSize uint32      // розмір буфера декодера у байтах
    MaxBitrate uint32      // максимальний бітрейт у бітах/секунду
    AvgBitrate uint32      // середній бітрейт у бітах/секунду
    AudioSpecific []byte   // AudioSpecificConfig для AAC (MPEG4AudioConfigBytes)
}
```

### 🔍 Стандартизовані константи:

```go
// ObjectType — тип об'єкта згідно ISO/IEC 14496-1 Table 5
const ObjectTypeAudio = ObjectType(0x40)  // Audio ISO/IEC 14496-3 (AAC)

// StreamType — тип потоку згідно ISO/IEC 14496-1 Table 6
const StreamTypeAudioStream = StreamType(0x05)  // AudioStream
```

### ✅ Ваш use-case: створення DecoderConfigDescriptor для AAC

```go
// CreateAACDecoderConfig — створення конфігурації для AAC потоку
func CreateAACDecoderConfig(codecData aacparser.CodecData, bufferSize, maxBitrate, avgBitrate uint32) *esio.DecoderConfigDescriptor {
    return &esio.DecoderConfigDescriptor{
        ObjectType:    esio.ObjectTypeAudio,
        StreamType:    esio.StreamTypeAudioStream,
        BufferSize:    bufferSize,    // напр. 1024 для низької затримки
        MaxBitrate:    maxBitrate,    // напр. 128000 для 128 kbps
        AvgBitrate:    avgBitrate,    // напр. 96000 для середнього
        AudioSpecific: codecData.MPEG4AudioConfigBytes(),  // 2-5 байт config
    }
}

// Використання:
aacCodec := getAACCodecData()  // з вашого пайплайну
config := CreateAACDecoderConfig(aacCodec, 1024, 128000, 96000)

// Серіалізація у бінарний формат
var b esio.builder
if err := config.appendTo(&b); err != nil {
    log.Printf("serialize failed: %v", err)
}
binaryData := b.Bytes()
// binaryData готовий для запису у MP4/MPEG-TS/RTMP
```

---

## 🔑 2. parseDecoderConfig() — парсинг бінарних даних

### Логіка парсингу:

```go
func parseDecoderConfig(d []byte) (*DecoderConfigDescriptor, error) {
    // 1. Перевірка мінімальної довжини (13 байт для базових полів)
    if len(d) < 13 {
        return nil, errors.New("DecoderConfigDescriptor short")
    }
    
    // 2. Розпакування базових полів
    conf := &DecoderConfigDescriptor{
        ObjectType: ObjectType(d[0]),                    // байт 0
        StreamType: StreamType(d[1] >> 2),               // біти 7-2 байта 1
        BufferSize: pio.U24BE(d[2:]),                    // байти 2-4 (24-bit big-endian)
        MaxBitrate: pio.U32BE(d[5:]),                    // байти 5-8 (32-bit big-endian)
        AvgBitrate: pio.U32BE(d[9:]),                    // байти 9-12 (32-bit big-endian)
    }
    
    // 3. Пропуск базових полів, перехід до опціональних дескрипторів
    d = d[13:]
    
    // 4. Парсинг вкладених дескрипторів (tag-length-value формат)
    for len(d) > 0 {
        tag, contents, remainder, err := parseHeader(d)  // парсинг заголовку дескриптора
        if err != nil {
            return nil, fmt.Errorf("DecoderConfigDescriptor: %w", err)
        }
        d = remainder  // перехід до наступного дескриптора
        
        switch tag {
        case TagDecoderSpecificInfo:  // 0x05: DecoderSpecificInfo
            switch conf.ObjectType {
            case ObjectTypeAudio:  // тільки для аудіо
                conf.AudioSpecific = contents  // збереження AudioSpecificConfig
            }
        }
    }
    
    return conf, nil
}
```

### 🔍 Формат вкладених дескрипторів (tag-length-value):

```
Кожен дескриптор має формат:
[1-byte tag][variable-length length][N bytes data]

parseHeader() (не показаний у коді, але ймовірно у esio пакеті):
1. Читання 1-байтового tag
2. Читання варіантної довжини (7 біт/байт, старший біт = continuation flag)
3. Повернення: (tag, contents, remainder, error)

Приклад для AudioSpecificConfig:
[0x05][0x80, 0x02][0x11, 0x90]  // tag=5, length=2, data=AudioSpecificConfig
```

### ✅ Ваш use-case: парсинг DecoderConfig з медіа-контейнера

```go
// ParseAACDecoderConfigFromMP4 — витягування конфігурації з MP4 esds box
func ParseAACDecoderConfigFromMP4(esdsData []byte) (*esio.DecoderConfigDescriptor, error) {
    // MP4 esds box має структуру:
    // [4-byte version+flags][ES_Descriptor][DecoderConfigDescriptor][...]
    
    // Пропуск версії та прапорців (4 байти)
    if len(esdsData) < 4 {
        return nil, errors.New("esds too short")
    }
    d := esdsData[4:]
    
    // Парсинг ES_Descriptor (пропускаємо, нас цікавить тільки DecoderConfig)
    // ... логіка парсингу ES_Descriptor ...
    
    // Знаходження DecoderConfigDescriptor (tag=0x04)
    // ... пошук tag 0x04 у d ...
    
    // Парсинг DecoderConfigDescriptor
    config, err := parseDecoderConfig(d)
    if err != nil {
        return nil, fmt.Errorf("parse DecoderConfig: %w", err)
    }
    
    return config, nil
}

// Використання:
esdsBox := getESDSBoxFromMP4()  // витягнуто з файлу
config, err := ParseAACDecoderConfigFromMP4(esdsBox)
if err != nil { /* handle error */ }

log.Printf("AAC config: objectType=0x%X, bitrate=%d/%d bps",
    config.ObjectType, config.AvgBitrate, config.MaxBitrate)
```

---

## 🔑 3. appendTo() — серіалізація у бінарний формат

### Логіка серіалізації:

```go
func (c *DecoderConfigDescriptor) appendTo(b *builder) error {
    if c == nil {
        return nil  // нічого не робити якщо конфігурація nil
    }
    
    // 1. Початок DecoderConfigDescriptor: запис tag + виділення місця для length
    cursor := b.Descriptor(TagDecoderConfigDescriptor)  // tag=0x04
    defer cursor.DescriptorDone(-1)  // гарантоване завершення навіть при помилці
    
    // 2. Запис базових полів
    b.WriteByte(byte(c.ObjectType))  // 1 байт
    b.WriteByte(byte(c.StreamType<<2) | 1)  // 1 байт: streamType у бітах 7-2, прапорці у бітах 1-0
    b.WriteU24(c.BufferSize)  // 3 байти big-endian
    b.WriteU32(c.MaxBitrate)  // 4 байти big-endian
    b.WriteU32(c.AvgBitrate)  // 4 байти big-endian
    
    // 3. Запис опціональних під-дескрипторів
    switch {
    case c.AudioSpecific != nil:
        // ISO/IEC 14496-3 AudioSpecificConfig
        c2 := b.Descriptor(TagDecoderSpecificInfo)  // tag=0x05
        b.Write(c.AudioSpecific)  // запис сирих байт AudioSpecificConfig
        c2.DescriptorDone(-1)  // завершення дескриптора
    }
    
    return nil
}
```

### 🔍 Бітова структура поля `StreamType<<2 | 1`:

```
Байт 1 у DecoderConfigDescriptor:
  Біти 7-2: StreamType (0x05 = AudioStream)
  Біт 1: upstream flag (0 = не upstream)
  Біт 0: reserved (завжди 1)

Приклад для AudioStream:
  StreamType = 0x05 = 0b00000101
  StreamType<<2 = 0b00010100
  | 1 = 0b00010101 = 0x15

Результат: байт 1 = 0x15
```

### ✅ Ваш use-case: генерація esds box для MP4

```go
// GenerateESDSBox — створення esds box для MP4 контейнера
func GenerateESDSBox(codecData av.CodecData) ([]byte, error) {
    // 1. Конвертація av.CodecData → DecoderConfigDescriptor
    config, err := esio.DecoderConfigFromCodecData(codecData)
    if err != nil {
        return nil, fmt.Errorf("create config: %w", err)
    }
    
    // 2. Серіалізація у бінарний формат
    var b esio.builder
    
    // ES_Descriptor (tag=0x03) — обгортає DecoderConfigDescriptor
    esCursor := b.Descriptor(0x03)  // ES_Descriptor tag
    defer esCursor.DescriptorDone(-1)
    
    // ES_Descriptor fields (спрощено)
    b.WriteU16(0)  // ES_ID
    b.WriteByte(0) // flags + streamPriority
    
    // Вкладений DecoderConfigDescriptor
    if err := config.appendTo(&b); err != nil {
        return nil, err
    }
    
    // 3. Додавання заголовку box (size + type)
    boxData := b.Bytes()
    boxSize := uint32(len(boxData) + 8)  // +8 для size(4) + type(4)
    
    var header esio.builder
    header.WriteU32(boxSize)
    header.Write([]byte("esds"))  // box type
    header.Write(boxData)
    
    return header.Bytes(), nil
}

// Використання для AAC:
aacCodec := getAACCodecData()
esdsBox, err := GenerateESDSBox(aacCodec)
if err != nil { /* handle error */ }
// esdsBox готовий для запису у MP4 файл
```

---

## 🔑 4. DecoderConfigFromCodecData() — фабрика з av.CodecData

### Призначення:
Конвертація уніфікованого `av.CodecData` інтерфейсу у специфічний `DecoderConfigDescriptor` для подальшої серіалізації у бінарні формати.

### Реалізація:

```go
func DecoderConfigFromCodecData(stream av.CodecData) (*DecoderConfigDescriptor, error) {
    switch cd := stream.(type) {
    case aacparser.CodecData:  // тільки AAC підтримується зараз
        return &DecoderConfigDescriptor{
            ObjectType:    ObjectTypeAudio,      // 0x40 = Audio ISO/IEC 14496-3
            StreamType:    StreamTypeAudioStream, // 0x05 = AudioStream
            AudioSpecific: cd.MPEG4AudioConfigBytes(),  // 2-5 байт config
            // BufferSize, MaxBitrate, AvgBitrate залишаються 0 (можна налаштувати пізніше)
        }, nil
    }
    return nil, fmt.Errorf("can't marshal %T to DecoderConfigDescriptor", stream)
}
```

### 🔍 Чому тільки AAC?

```
DecoderConfigDescriptor використовується переважно для:
• Аудіо: AAC (ObjectType=0x40), MP3, тощо
• Відео: H.264 (ObjectType=0x20), H.265 (ObjectType=0x21), тощо

У цьому файлі реалізовано тільки AAC, але можна розширити:
• Додати case для h264parser.CodecData → ObjectTypeVideo
• Додати case для h265parser.CodecData → ObjectTypeHEVC
• Налаштувати BufferSize/MaxBitrate/AvgBitrate з метаданих кодека
```

### ✅ Ваш use-case: розширення підтримки кодеків

```go
// DecoderConfigFromCodecDataExtended — розширена версія з підтримкою відео
func DecoderConfigFromCodecDataExtended(stream av.CodecData) (*esio.DecoderConfigDescriptor, error) {
    switch cd := stream.(type) {
    case aacparser.CodecData:
        return &esio.DecoderConfigDescriptor{
            ObjectType:    esio.ObjectTypeAudio,
            StreamType:    esio.StreamTypeAudioStream,
            BufferSize:    1024,  // налаштувати за потребами
            MaxBitrate:    128000,
            AvgBitrate:    96000,
            AudioSpecific: cd.MPEG4AudioConfigBytes(),
        }, nil
        
    case h264parser.CodecData:
        return &esio.DecoderConfigDescriptor{
            ObjectType:    esio.ObjectType(0x20),  // Visual ISO/IEC 14496-10 (H.264)
            StreamType:    esio.StreamType(0x04),  // VisualStream
            BufferSize:    1024*1024,  // 1MB для відео
            MaxBitrate:    5000000,    // 5 Mbps
            AvgBitrate:    3000000,    // 3 Mbps
            // VideoSpecific можна додати аналогічно до AudioSpecific
        }, nil
        
    case h265parser.CodecData:
        return &esio.DecoderConfigDescriptor{
            ObjectType:    esio.ObjectType(0x21),  // Visual ISO/IEC 23008-2 (H.265)
            StreamType:    esio.StreamType(0x04),
            BufferSize:    2*1024*1024,  // 2MB для HEVC
            MaxBitrate:    10000000,     // 10 Mbps
            AvgBitrate:    6000000,      // 6 Mbps
        }, nil
    }
    return nil, fmt.Errorf("unsupported codec type: %T", stream)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// mp4_metadata_generator.go — генерація метаданих для MP4/HLS
type MP4MetadataGenerator struct {
    channelID string
    metrics   *MetadataMetrics
}

// GenerateInitSegment — створення init segment для HLS fMP4
func (g *MP4MetadataGenerator) GenerateInitSegment(videoCodec, audioCodec av.CodecData) ([]byte, error) {
    var b esio.builder
    
    // FTYP box
    b.WriteU32(24)  // size
    b.Write([]byte("ftyp"))
    b.Write([]byte("iso6"))  // major brand
    b.WriteU32(0)  // minor version
    b.Write([]byte("iso6mp41"))  // compatible brands
    
    // MOOV box
    moovCursor := b.Descriptor(0x6D6F6F76)  // 'moov' tag (спрощено)
    
    // MVHD box (movie header)
    // ... запис mvhd ...
    
    // TRAK box для відео
    if videoCodec != nil {
        if err := g.writeVideoTrak(&b, videoCodec); err != nil {
            return nil, err
        }
    }
    
    // TRAK box для аудіо
    if audioCodec != nil {
        if err := g.writeAudioTrak(&b, audioCodec); err != nil {
            return nil, err
        }
    }
    
    // Завершення MOOV
    moovCursor.DescriptorDone(-1)
    
    g.metrics.InitSegmentsGenerated.Inc()
    return b.Bytes(), nil
}

// writeAudioTrak — запис аудіо track з esds box
func (g *MP4MetadataGenerator) writeAudioTrak(b *esio.builder, codec av.CodecData) error {
    // TKHD box (track header)
    // ... запис tkhd ...
    
    // MDIA box (media)
    mdiaCursor := b.Descriptor(0x6D646961)  // 'mdia'
    
    // MDHD box (media header)
    // ... запис mdhd ...
    
    // HDLR box (handler reference)
    b.WriteU32(32)  // size
    b.Write([]byte("hdlr"))
    b.WriteU32(0)  // version+flags
    b.Write([]byte("soun"))  // handler type = sound
    // ... інші поля ...
    
    // MINF box (media information)
    // ... запис minf ...
    
    // STBL box (sample table)
    stblCursor := b.Descriptor(0x7374626C)  // 'stbl'
    
    // STSD box (sample descriptions)
    stsdCursor := b.Descriptor(0x73747364)  // 'stsd'
    b.WriteU32(1)  // entry count
    
    // MP4A box (audio sample entry)
    mp4aCursor := b.Descriptor(0x6D703461)  // 'mp4a'
    // ... запис полів mp4a ...
    
    // ESDS box — критично: містить DecoderConfigDescriptor
    esdsData, err := GenerateESDSBox(codec)
    if err != nil {
        return fmt.Errorf("generate esds: %w", err)
    }
    b.Write(esdsData)
    
    // Завершення mp4a
    mp4aCursor.DescriptorDone(-1)
    
    // Завершення stsd
    stsdCursor.DescriptorDone(-1)
    
    // ... інші таблиці (stts, stsc, stsz, тощо) ...
    
    // Завершення stbl
    stblCursor.DescriptorDone(-1)
    
    // Завершення mdia
    mdiaCursor.DescriptorDone(-1)
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"DecoderConfigDescriptor short"** | Вхідні дані < 13 байт | Переконайтеся, що передаєте повний DecoderConfigDescriptor; перевірте цілісність вхідного потоку |
| **Неправильний StreamType** | Біти 1-0 байта 1 не встановлені коректно | Використовуйте `byte(c.StreamType<<2) | 1` для встановлення reserved біта = 1 |
| **AudioSpecific не парситься** | Неправильний формат AudioSpecificConfig | Переконайтеся, що `cd.MPEG4AudioConfigBytes()` повертає валідний MPEG4AudioConfig (2-5 байт для AAC-LC) |
| **Переповнення 24-бітного BufferSize** | `WriteU24(c.BufferSize)` з c.BufferSize > 0xFFFFFF | Валідуйте вхідні дані: `if c.BufferSize > 0xFFFFFF { return error }` |
| **Невірне кодування варіантної довжини** | Дескриптори не парсяться на стороні клієнта | Переконайтеся, що `parseHeader()`/`DescriptorDone()` використовують однаковий формат (7 біт/байт, continuation flag) |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування серіалізованих конфігурацій:

```go
type ConfigCache struct {
    mu    sync.RWMutex
    cache map[string][]byte  // hash(codecData) → serialized bytes
}

func (c *ConfigCache) GetOrSerialize(codec av.CodecData) ([]byte, error) {
    key := fmt.Sprintf("%T:%v", codec, codec)  // простий хеш, на практиці використовуйте proper hashing
    
    c.mu.RLock()
    if data, ok := c.cache[key]; ok {
        c.mu.RUnlock()
        return data, nil
    }
    c.mu.RUnlock()
    
    // Серіалізація якщо не в кеші
    config, err := esio.DecoderConfigFromCodecData(codec)
    if err != nil {
        return nil, err
    }
    
    var b esio.builder
    if err := config.appendTo(builder); err != nil {
        return nil, err
    }
    result := b.Bytes()
    
    c.mu.Lock()
    if c.cache == nil {
        c.cache = make(map[string][]byte)
    }
    c.cache[key] = result
    c.mu.Unlock()
    
    return result, nil
}
```

### 2. Попереднє виділення буфера для серіалізації:

```go
// NewPreallocatedBuilderForDecoderConfig — конструктор з ємністю для типового DecoderConfig
func NewPreallocatedBuilderForDecoderConfig() *esio.builder {
    // Типовий розмір: tag(1) + length(4) + base fields(13) + AudioSpecific(2-5) + sub-descriptor overhead
    return esio.NewPreallocatedBuilder(32)
}

// Використання:
builder := NewPreallocatedBuilderForDecoderConfig()
if err := config.appendTo(builder); err != nil { /* handle */ }
result := builder.Bytes()
```

### 3. Моніторинг продуктивності серіалізації:

```go
type ConfigSerializationMetrics struct {
    SerializeLatency prometheus.HistogramVec
    ConfigSize       prometheus.HistogramVec
    Errors           prometheus.CounterVec
}

func (m *ConfigSerializationMetrics) RecordSerialization(codecType string, size int, duration time.Duration, err error) {
    if err != nil {
        m.Errors.WithLabelValues(codecType).Inc()
        return
    }
    m.SerializeLatency.WithLabelValues(codecType).Observe(duration.Seconds())
    m.ConfigSize.WithLabelValues(codecType).Observe(float64(size))
}
```

---

## 📋 Чек-лист інтеграції esio.DecoderConfigDescriptor

```go
// ✅ 1. Конвертація av.CodecData → DecoderConfigDescriptor
config, err := esio.DecoderConfigFromCodecData(codecData)
if err != nil { /* handle error */ }

// ✅ 2. Налаштування параметрів бітрейту/буфера якщо потрібно
config.BufferSize = 1024  // для низької затримки
config.MaxBitrate = 128000
config.AvgBitrate = 96000

// ✅ 3. Серіалізація з попереднім виділенням буфера
builder := esio.NewPreallocatedBuilder(32)
if err := config.appendTo(builder); err != nil { /* handle */ }
binaryData := builder.Bytes()

// ✅ 4. Валідація вихідних даних
if len(binaryData) < 13 {
    return fmt.Errorf("serialized config too short")
}

// ✅ 5. Парсинг для перевірки (опціонально)
parsed, err := parseDecoderConfig(binaryData)
if err != nil {
    log.Printf("warning: self-parse failed: %v", err)
}

// ✅ 6. Метрики для моніторингу
metrics.RecordSerialization(codecData.Type().String(), len(binaryData), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 📄 [ISO/IEC 14496-1:2021 — MPEG-4 Systems](https://www.iso.org/standard/82746.html) — офіційний стандарт (платний)
- 📄 [MPEG-4 AudioSpecificConfig](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — детальний опис формату
- 📄 [MP4 Esds Box Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap3/qtff3.html#//apple_ref/doc/uid/TP40000939-CH205-SW1) — використання у MP4
- 💻 [vdk esio Package](https://pkg.go.dev/github.com/deepch/vdk/format/esio) — GoDoc documentation (якщо доступна)
- 🧪 [Go encoding/binary Documentation](https://pkg.go.dev/encoding/binary) — робота з бітовими даними

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **медіа-метаданими у реальному часі**:
> 1. **Кешуйте серіалізовані конфігурації** — уникнення повторної серіалізації однакових кодеків значно зменшує навантаження на CPU.
> 2. **Валідуйте BufferSize/Bitrate перед серіалізацією** — переповнення 24-бітних полів може зламати парсинг на стороні клієнта.
> 3. **Використовуйте `defer cursor.DescriptorDone(-1)`** — це гарантує коректне завершення дескриптора навіть при помилках у проміжних викликах.
> 4. **Моніторьте `ConfigSize`** — різкі зміни можуть вказувати на неправильну конфігурацію кодека або пошкоджені метадані.
> 5. **Тестуйте парсинг-серіалізацію циклічно** — `serialize(parse(data)) == data` має виконуватися для коректної реалізації.

Потрібен приклад інтеграції `MP4MetadataGenerator` з вашим `flv.Muxer` для генерації fMP4 init segments для HLS? Готовий допомогти! 🚀