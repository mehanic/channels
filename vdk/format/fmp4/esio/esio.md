# 📦 Глибокий розбір: `esio` — Парсинг MPEG-4 Elementary Stream Descriptor (ES_Descriptor)

Цей файл — **реалізація парсингу та серіалізації** дескрипторів потоку MPEG-4 відповідно до стандарту ISO/IEC 14496-1:2004. Він використовується для опису аудіо/відео потоків у контейнерах MP4/MPEG-4, зокрема для передачі конфігурації кодеків (наприклад, AAC через `DecoderSpecificInfo`).

---

## 🗺️ Архітектурна схема esio

```
┌────────────────────────────────────────┐
│ 📦 esio — MPEG-4 ES Descriptor Parser │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • StreamDescriptor — головна структура│
│  • parseHeader/parseLength — EBML-like│
│    variable-length encoding            │
│  • Tag constants — типи дескрипторів  │
│  • builder — helper для серіалізації  │
│                                         │
│  🔄 Формат дескриптора:                │
│  [tag:1][length:VL][payload]          │
│  • Variable-Length encoding для size  │
│  • Nested descriptors (ES→DecoderConfig│
│    →DecoderSpecificInfo)              │
│                                         │
│  📡 Використання:                       │
│  • AAC AudioSpecificConfig у esds атомі│
│  • H.264/HEVC codec configuration     │
│  • MPEG-4 Systems stream description  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. StreamDescriptor — головна структура

### 🔧 Структура та поля:

```go
type StreamDescriptor struct {
    ESID      uint16  // Elementary Stream ID (унікальний ідентифікатор потоку)
    DependsOn *uint16 // опціонально: ID потоку, від якого залежить цей
    URL       *string // опціонально: зовнішнє джерело даних
    OCR       *uint16 // опціонально: Object Clock Reference для синхронізації

    DecoderConfig *DecoderConfigDescriptor  // конфігурація декодера (кодек, бітрейт)
    SLConfig      *SLConfigDescriptor       // Sync Layer config (таймінги, доставка)
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `ESID` | `uint16` | Унікальний ідентифікатор потоку в межах сесії | `1` для відео, `2` для аудіо |
| `DependsOn` | `*uint16` | Посилання на батьківський потік (для layered codecs) | `nil` для незалежних потоків |
| `URL` | `*string` | Зовнішнє джерело даних (рідко використовується) | `"http://example.com/stream"` |
| `OCR` | `*uint16` | Посилання на потік з годинником для синхронізації | `nil` якщо використовується системний час |
| `DecoderConfig` | `*DecoderConfigDescriptor` | **Критично**: параметри кодека (objectType, bitrate, тощо) | AAC: objectType=0x40, streamType=0x15 |
| `SLConfig` | `*SLConfigDescriptor` | Налаштування Sync Layer (таймінги, доставка пакетів) | `nil` для простих випадків |

### ✅ Ваш use-case**: ініціалізація AAC декодера

```go
// InitAACFromESDescriptor — створення CodecData з StreamDescriptor
func InitAACFromESDescriptor(es *esio.StreamDescriptor) (av.CodecData, error) {
    if es == nil || es.DecoderConfig == nil || es.DecoderConfig.DecSpecificInfo == nil {
        return nil, fmt.Errorf("missing AAC config in ES descriptor")
    }
    
    // es.DecoderConfig.DecSpecificInfo містить MPEG-4 AudioSpecificConfig
    // Формат: [audioObjectType:5][samplingFrequencyIndex:4][channelConfiguration:4]...
    return aacparser.NewCodecDataFromMPEG4AudioConfigBytes(
        es.DecoderConfig.DecSpecificInfo,
    )
}

// Використання при парсингі MP4:
// 1. Знайти esds атом у SampleDesc
// 2. Парсити ElemStreamDesc → StreamDescriptor
// 3. Викликати InitAACFromESDescriptor для отримання CodecData
```

---

## 🔑 2. Tag constants — типи дескрипторів

### 🔧 Константи згідно з ISO/IEC 14496-1:

```go
const (
    TagForbidden = Tag(iota)              // 0x00 — зарезервовано
    TagObjectDescriptor                   // 0x01 — Object Descriptor (OD)
    TagInitialObjectDescriptor            // 0x02 — Initial OD
    TagESDescriptor                       // 0x03 — Elementary Stream Descriptor ⭐
    TagDecoderConfigDescriptor            // 0x04 — Decoder Config Descriptor ⭐
    TagDecoderSpecificInfo                // 0x05 — Decoder Specific Info ⭐
    TagSLConfigDescriptor                 // 0x06 — Sync Layer Config Descriptor
)
```

### 🔍 Ієрархія дескрипторів для AAC:

```
ES_Descriptor (tag=0x03)
├─ ES_ID: 2 bytes (напр. 1)
├─ flags: 1 byte (streamDependence, URL, OCR)
├─ [optional fields based on flags]
├─ DecoderConfigDescriptor (tag=0x04) ⭐
│  ├─ objectTypeIndication: 1 byte (0x40 = AAC)
│  ├─ streamType: 1 byte (0x15 = AudioStream)
│  ├─ bufferSizeDB: 3 bytes
│  ├─ maxBitrate: 4 bytes
│  ├─ avgBitrate: 4 bytes
│  └─ DecoderSpecificInfo (tag=0x05) ⭐
│     └─ AudioSpecificConfig: variable bytes ⭐
└─ SLConfigDescriptor (tag=0x06) [опціонально]
   └─ configValue: 1 byte (зазвичай 0x02)
```

### ✅ Ваш use-case**: пошук AudioSpecificConfig

```go
// FindAudioSpecificConfig — рекурсивний пошук DecoderSpecificInfo
func FindAudioSpecificConfig(desc *esio.StreamDescriptor) ([]byte, bool) {
    if desc == nil || desc.DecoderConfig == nil {
        return nil, false
    }
    // DecoderSpecificInfo містить сирий AudioSpecificConfig
    return desc.DecoderConfig.DecSpecificInfo, true
}

// Використання:
if config, ok := FindAudioSpecificConfig(es); ok {
    log.Printf("Found AAC config: %d bytes, first byte: 0x%02X", 
        len(config), config[0])
    // config[0] & 0xF8 >> 3 = audioObjectType (напр. 2 = AAC LC)
}
```

---

## 🔑 3. Variable-Length Encoding — parseLength/parseHeader

### 🔧 Формат EBML-like довжини:

```
Кожен дескриптор має формат:
  [1-byte tag][variable-length size][payload]

Size кодується у форматі "MPEG-4 SL":
  • Кожен байт: 7 біт даних + 1 біт продовження (0x80)
  • Якщо байт & 0x80 != 0 → є наступний байт довжини
  • Максимальна довжина: 4 байти → 28 біт даних → ~268 млн байт

parseLength реалізація:
    func parseLength(start []byte) (length int, d []byte, err error) {
        d = start
        for i := 0; i < 4; i++ {  // максимум 4 байти
            if len(d) == 0 {
                err = errors.New("short tag")
                return
            }
            v := d[0]
            d = d[1:]
            length <<= 7              // зсув на 7 біт
            length |= int(v & 0x7f)   // додавання 7 біт даних
            if v&0x80 == 0 {          // біт продовження = 0 → останній байт
                break
            }
        }
        return
    }
```

### 🔍 Приклади кодування довжини:

```
Значення 100 (0x64):
  • 100 < 128 → один байт: 0x64 (біт продовження = 0)

Значення 300 (0x12C):
  • 300 = 2*128 + 44
  • Перший байт: 0x81 (біт продовження=1, дані=0000001)
  • Другий байт: 0x2C (біт продовження=0, дані=0101100)
  • Результат: [0x81, 0x2C]

Значення 16383 (максимум для 2 байт):
  • [0xFF, 0x7F] → (127 << 7) | 127 = 16256 + 127 = 16383
```

### ⚠️ Критична проблема: відсутність перевірки переповнення

```
У parseLength():
    length <<= 7
    length |= int(v & 0x7f)

Проблема:
• Якщо вхідні дані містять 4 байти з бітами продовження → length може переповнити int
• На 32-бітних системах int = 32 біти → максимум ~2 млрд
• Зловмисний файл може викликати переповнення → некоректний парсинг

✅ Виправлення: перевірка переповнення
    func parseLengthSafe(start []byte) (length int, d []byte, err error) {
        const maxLen = 1 << 28  // 28 біт даних = максимум для 4 байт
        d = start
        for i := 0; i < 4; i++ {
            if len(d) == 0 {
                err = errors.New("short tag")
                return
            }
            v := d[0]
            d = d[1:]
            
            // Перевірка переповнення перед зсувом
            if length > (maxLen >> 7) {
                err = fmt.Errorf("length overflow at byte %d", i)
                return
            }
            
            length <<= 7
            length |= int(v & 0x7f)
            if v&0x80 == 0 {
                break
            }
        }
        return
    }
```

---

## 🔑 4. ParseStreamDescriptor — основна логіка парсингу

### 🔧 Покроковий розбір:

```go
func ParseStreamDescriptor(start []byte) (desc *StreamDescriptor, remainder []byte, err error) {
    // 1. Парсинг заголовку (tag + length + payload)
    tag, d, remainder, err := parseHeader(start)
    if err != nil {
        err = fmt.Errorf("ES_Descriptor: %w", err)
        return
    } else if tag != TagESDescriptor {
        err = fmt.Errorf("expected ES_Descriptor but got tag %02X", tag)
        return
    }
    
    // 2. Читання обов'язкових полів
    desc = &StreamDescriptor{ESID: pio.U16BE(d)}  // 2 байти, big-endian
    flags := d[2]  // 1 байт прапорців
    d = d[3:]      // пропуск заголовка
    
    // 3. Обробка опціональних полів за прапорцями
    if flags&esFlagStreamDependence != 0 {  // 0x80
        v := pio.U16BE(d)
        desc.DependsOn = &v
        d = d[2:]
    }
    if flags&esFlagURL != 0 {  // 0x40
        urlLength := d[0]  // 1 байт довжини
        v := string(d[1 : 1+urlLength])
        desc.URL = &v
        d = d[1+urlLength:]
    }
    if flags&esFlagOCR != 0 {  // 0x20
        v := pio.U16BE(d)
        desc.OCR = &v
        d = d[2:]
    }
    
    // 4. Рекурсивний парсинг дочірніх дескрипторів
    for len(d) > 0 {
        var child []byte
        tag, child, d, err = parseHeader(d)
        if err != nil {
            err = fmt.Errorf("ES_Descriptor: %w", err)
            return
        }
        switch tag {
        case TagDecoderConfigDescriptor:
            desc.DecoderConfig, err = parseDecoderConfig(child)  // ⭐ критично
        case TagSLConfigDescriptor:
            desc.SLConfig, err = parseSLConfig(child)
        }
        if err != nil {
            return
        }
    }
    
    remainder = d
    return
}
```

### 🔍 Прапорці та їх значення:

```go
const (
    esFlagStreamDependence = 0x80  // біт 7: присутній DependsOn поле
    esFlagURL              = 0x40  // біт 6: присутній URL поле
    esFlagOCR              = 0x20  // біт 5: присутній OCR поле
)
```

### ⚠️ Критична проблема: відсутність валідації меж буфера

```
У обробці URL:
    urlLength := d[0]
    v := string(d[1 : 1+urlLength])  // ← Паніка якщо 1+urlLength > len(d)!

Наслідки:
• Пошкоджені або зловмисні файли можуть викликати panic
• Неможливість коректної обробки помилки

✅ Виправлення: перевірка меж перед доступом
    if flags&esFlagURL != 0 {
        if len(d) < 1 {
            err = fmt.Errorf("short URL length field")
            return
        }
        urlLength := int(d[0])
        if len(d) < 1+urlLength {
            err = fmt.Errorf("short URL data: need %d, got %d", 1+urlLength, len(d))
            return
        }
        v := string(d[1 : 1+urlLength])
        desc.URL = &v
        d = d[1+urlLength:]
    }
```

---

## 🔑 5. Marshal() — серіалізація дескриптора

### 🔧 Використання builder патерну:

```go
func (s *StreamDescriptor) Marshal() ([]byte, error) {
    var b builder
    cursor := b.Descriptor(TagESDescriptor)  // початок дескриптора
    
    // Запис обов'язкових полів
    b.WriteU16(s.ESID)
    
    // Розрахунок прапорців
    var flags uint8
    if s.DependsOn != nil { flags |= esFlagStreamDependence }
    if s.URL != nil { flags |= esFlagURL }
    if s.OCR != nil { flags |= esFlagOCR }
    b.WriteByte(flags)
    
    // Запис опціональних полів
    if s.DependsOn != nil { b.WriteU16(*s.DependsOn) }
    if s.URL != nil {
        b.WriteByte(byte(len(*s.URL)))  // довжина перед даними
        b.Write([]byte(*s.URL))
    }
    if s.OCR != nil { b.WriteU16(*s.OCR) }
    
    // Рекурсивна серіалізація дочірніх дескрипторів
    if err := s.DecoderConfig.appendTo(&b); err != nil { return nil, err }
    if err := s.SLConfig.appendTo(&b); err != nil { return nil, err }
    
    // Завершення: запис довжини (variable-length)
    cursor.DescriptorDone(-1)  // -1 = автоматичний розрахунок
    return b.Bytes(), nil
}
```

### 🔍 builder патерн для variable-length size:

```
Проблема: довжина дескриптора відома тільки після запису всіх полів.

Рішення: cursor патерн
1. Descriptor(tag) → запис tag + placeholder для length + повернення cursor
2. Запис всіх полів у буфер
3. cursor.DescriptorDone(actualLength) → повернення назад і запис правильної довжини

Приклад реалізації cursor:
    type cursor struct {
        buf *bytes.Buffer
        pos int  // позиція початку length поля
    }
    
    func (c cursor) DescriptorDone(actualLen int) {
        // Запис variable-length encoded length на позицію c.pos
        encoded := encodeVariableLength(actualLen)
        // Копіювання назад у буфер (потрібна підтримка overwrite)
    }
```

### ⚠️ Критична проблема: відсутність обробки помилок у builder

```
У поточному коді:
    b.WriteU16(s.ESID)  // ← ніякої перевірки помилок!

Якщо builder використовує bytes.Buffer, WriteU16 може не мати повернення помилки,
але для network streams або custom writers це може бути проблемою.

✅ Виправлення: додавання error повернення у всі методи builder
    func (b *builder) WriteU16(v uint16) error {
        buf := make([]byte, 2)
        pio.PutU16BE(buf, v)
        _, err := b.buf.Write(buf)
        return err
    }
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Парсинг esds атому з MP4 файлу

```go
// ParseESDSFromMP4 — витягування StreamDescriptor з MP4 esds атому
func ParseESDSFromMP4(esdsData []byte) (*esio.StreamDescriptor, error) {
    // esds атом має формат:
    // [4-byte version/flags = 0][ES_Descriptor...]
    
    if len(esdsData) < 4 {
        return nil, fmt.Errorf("esds too short: %d bytes", len(esdsData))
    }
    
    // Пропуск version/flags (завжди 0 для esds)
    payload := esdsData[4:]
    
    // Парсинг ES_Descriptor
    desc, remainder, err := esio.ParseStreamDescriptor(payload)
    if err != nil {
        return nil, fmt.Errorf("parse ES descriptor: %w", err)
    }
    
    // Перевірка що не залишилось непрочитаних даних
    if len(remainder) > 0 {
        log.Printf("warning: %d unread bytes after ES descriptor", len(remainder))
    }
    
    return desc, nil
}

// Використання:
// 1. Знайти esds атом у SampleDesc.MP4ADesc.Unknowns
// 2. Викликати ParseESDSFromMP4(esds.Content)
// 3. Використати desc.DecoderConfig.DecSpecificInfo для ініціалізації декодера
```

### 🔧 Приклад: Генерація AAC конфігурації

```go
// GenerateAACStreamDescriptor — створення ES descriptor для AAC
func GenerateAACStreamDescriptor(esid uint16, config []byte) (*esio.StreamDescriptor, error) {
    return &esio.StreamDescriptor{
        ESID: esid,
        // Опціональні поля не потрібні для простих випадків
        DecoderConfig: &esio.DecoderConfigDescriptor{
            ObjectType: 0x40,        // AAC
            StreamType: 0x15,        // AudioStream
            BufferSizeDB: 0,         // не використовується
            MaxBitrate: 200000,      // 200 kbps
            AvgBitrate: 128000,      // 128 kbps
            DecSpecificInfo: config, // AudioSpecificConfig (2+ bytes)
        },
        SLConfig: &esio.SLConfigDescriptor{
            ConfigValue: 0x02,  // стандартне значення для MP4
        },
    }, nil
}

// Використання:
// 1. Створити AudioSpecificConfig для AAC
config := []byte{0x11, 0x90}  // AAC-LC, 48kHz, stereo
// 2. Згенерувати ES descriptor
desc, err := GenerateAACStreamDescriptor(2, config)
// 3. Серіалізувати у байти для запису у esds атом
esdsBytes, err := desc.Marshal()
// 4. Записати у MP4 файл через mp4io.ElemStreamDesc
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при доступі до d[1+urlLength]** | Короткі або пошкоджені файли | Додайте перевірку `if len(d) < 1+urlLength` перед зрізом |
| **Переповнення у parseLength** | Великі значення довжини читаються некоректно | Додайте перевірку `if length > maxLen` перед зсувом |
| **Невірний variable-length encoding у Marshal** | Серіалізовані дані не парситься | Переконайтеся що cursor.DescriptorDone() коректно записує length |
| **Відсутність обробки помилок у builder** | Помилки запису ігноруються | Додайте `error` повернення у всі методи builder |
| **Некоректний порядок полів** | Дескриптор не валідний за специфікацією | Дотримуйтесь порядку: tag → length → обов'язкові поля → опціональні → дочірні |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування variable-length encoding:

```go
// PrecomputedLengths — кеш для поширених довжин
var lengthCache = sync.Map{}  // map[int][]byte

func getCachedLengthBytes(length int) []byte {
    if cached, ok := lengthCache.Load(length); ok {
        return cached.([]byte)
    }
    
    bytes := encodeVariableLength(length)
    lengthCache.Store(length, bytes)
    return bytes
}
```

### 2. Використання sync.Pool для буферів:

```go
var descriptorBufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func MarshalPooled(desc *StreamDescriptor) ([]byte, error) {
    buf := descriptorBufferPool.Get().(*bytes.Buffer)
    buf.Reset()
    defer descriptorBufferPool.Put(buf)
    
    // Серіалізація у буфер
    // ... код маршалінгу ...
    
    // Копіювання результату (щоб уникнути проблем з пулом)
    result := make([]byte, buf.Len())
    copy(result, buf.Bytes())
    return result, nil
}
```

### 3. Моніторинг продуктивності парсингу:

```go
type ParserMetrics struct {
    DescriptorsParsed prometheus.CounterVec
    ParseLatency      prometheus.HistogramVec
    DescriptorSizes   prometheus.HistogramVec
    ParseErrors       prometheus.CounterVec
}

func (m *ParserMetrics) RecordParse(tag esio.Tag, size int, duration time.Duration, err error) {
    m.DescriptorsParsed.WithLabelValues(fmt.Sprintf("0x%02X", tag)).Inc()
    m.ParseLatency.WithLabelValues(fmt.Sprintf("0x%02X", tag)).Observe(duration.Seconds())
    m.DescriptorSizes.Observe(float64(size))
    if err != nil {
        m.ParseErrors.WithLabelValues(fmt.Sprintf("0x%02X", tag)).Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання esio

```go
// ✅ 1. Перевірка меж буфера перед доступом
if len(d) < 1+urlLength {
    return fmt.Errorf("short URL data: need %d, got %d", 1+urlLength, len(d))
}

// ✅ 2. Валідація variable-length довжини
length, _, err := parseLengthSafe(data)
if err != nil {
    return fmt.Errorf("parse length: %w", err)
}
if length > maxDescriptorSize {  // напр. 100KB
    return fmt.Errorf("descriptor too large: %d bytes", length)
}

// ✅ 3. Перевірка типу дескриптора перед парсингом
if tag != esio.TagESDescriptor {
    return fmt.Errorf("expected ES_Descriptor (0x03), got 0x%02X", tag)
}

// ✅ 4. Обробка помилок рекурсивного парсингу
for len(d) > 0 {
    tag, child, d, err := parseHeader(d)
    if err != nil { return err }
    switch tag {
    case esio.TagDecoderConfigDescriptor:
        desc.DecoderConfig, err = parseDecoderConfig(child)
        if err != nil { return fmt.Errorf("parse DecoderConfig: %w", err) }
    }
}

// ✅ 5. Логування для дебагу складних випадків
log.Printf("Parsed ES descriptor: ESID=%d, flags=0x%02X, children=%d", 
    desc.ESID, flags, countChildren(desc))

// ✅ 6. Метрики для моніторингу
metrics.RecordParse(tag, len(start), time.Since(parseStart), err)
```

---

## 🔗 Корисні посилання

- 📄 [ISO/IEC 14496-1:2004 Systems](https://www.iso.org/standard/39484.html) — офіційний стандарт (платний)
- 📄 [MPEG-4 AudioSpecificConfig](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — детальний опис формату
- 📄 [ESDS Atom Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap3/qtff3.html#//apple_ref/doc/uid/TP40000939-CH205-SW1) — Apple documentation
- 🧪 [Variable-Length Quantity Encoding](https://en.wikipedia.org/wiki/Variable-length_quantity) — теорія VLQ кодування
- 💻 [Go bytes.Buffer Documentation](https://pkg.go.dev/bytes#Buffer) — робота з динамічними буферами

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед доступом** — уникнення панік при пошкоджених файлах.
> 2. **Додайте перевірку переповнення у parseLength** — захист від зловмисних вхідних даних.
> 3. **Документуйте порядок полів у Marshal** — забезпечення сумісності з парсерами інших реалізацій.
> 4. **Кешуйте variable-length encoding для частих значень** — прискорення серіалізації.
> 5. **Моніторьте `ParseErrors` метрику** — різке зростання може вказувати на пошкоджені файли або нові версії формату.

Потрібен приклад реалізації `parseDecoderConfig` та `parseSLConfig` функцій, або інтеграція `esio` з вашим `mp4io.ElemStreamDesc` для повного циклу парсингу/генерації esds атомів? Готовий допомогти! 🚀