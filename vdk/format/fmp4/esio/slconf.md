# 📦 Глибокий розбір: `esio.SLConfigDescriptor` — Sync Layer Config для MPEG-4

Цей файл — **реалізація парсингу та серіалізації** дескриптора конфігурації Sync Layer (SL) відповідно до стандарту ISO/IEC 14496-1:2004. Цей компонент є частиною системи опису потоків MPEG-4 і використовується для визначення параметрів синхронізації та доставки медіа-даних.

---

## 🗺️ Архітектурна схема SLConfigDescriptor

```
┌────────────────────────────────────────┐
│ 📦 esio.SLConfigDescriptor            │
├────────────────────────────────────────┤
│                                         │
│  🔑 Призначення:                        │
│  • Визначення параметрів Sync Layer   │
│  • Синхронізація аудіо/відео потоків  │
│  • Контроль доставки пакетів (timing) │
│                                         │
│  🔄 Формат дескриптора:                │
│  [tag=0x06][length:VL][predefined:1]  │
│  [custom_data...] (якщо predefined=0) │
│                                         │
│  📡 Використання:                       │
│  • AAC/H.264 у MP4 контейнерах        │
│  • MPEG-4 Systems stream description  │
│  • Синхронізація клієнт-сервер        │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. SLConfigPredefined — стандартні конфігурації

### 🔧 Константи згідно з таблицею 12 стандарту:

```go
const (
    SLConfigCustom = SLConfigPredefined(iota)  // 0x00 — кастомна конфігурація
    SLConfigNull                               // 0x01 — null config (не використовується)
    SLConfigMP4                                // 0x02 — стандартна конфігурація для MP4 ⭐
)
```

### 🔍 Значення стандартних конфігурацій:

| Значення | Назва | Призначення | Використання |
|----------|-------|-------------|--------------|
| `0x00` | SLConfigCustom | Кастомні параметри через `Custom []byte` | Рідко, для спеціальних випадків |
| `0x01` | SLConfigNull | Null config (заборонено стандартом) | Не використовується |
| `0x02` | SLConfigMP4 | **Стандартна конфігурація для MP4 контейнерів** | ⭐ 99% випадків у MP4/WebM |

### 🔍 Що означає SLConfigMP4 (0x02):

```
Згідно з ISO/IEC 14496-1:2004, Table 12:

SLConfigMP4 (0x02) визначає такі параметри за замовчуванням:
• useAccessUnitStartFlag = 0
• useAccessUnitEndFlag = 0
• useRandomAccessPointFlag = 0
• useHasRandomAccessPointFlag = 0
• usePaddingFlag = 0
• useTimeStampsFlag = 1  // ⭐ критично: використання таймстампів
• useIdleFlag = 0
• durationFlag = 0
• timeStampRes = 1000    // ⭐ роздільна здатність таймстампу: 1/1000 сек = 1ms
• OCRRes = 0             // не використовується

Це означає:
• Кожен пакет має абсолютний таймстамп у мілісекундах
• Синхронізація через системний годинник (не через OCR потік)
• Проста доставка без складних прапорців доступу
```

### ✅ Ваш use-case**: валідація конфігурації

```go
// ValidateSLConfig — перевірка коректності конфігурації
func ValidateSLConfig(sl *esio.SLConfigDescriptor) error {
    if sl == nil {
        return nil  // SLConfig опціональний
    }
    
    switch sl.Predefined {
    case esio.SLConfigMP4:
        // Стандартна конфігурація — завжди валідна
        return nil
        
    case esio.SLConfigCustom:
        // Кастомна: перевірка мінімальної довжини
        if len(sl.Custom) == 0 {
            return fmt.Errorf("custom SLConfig cannot be empty")
        }
        // Додаткові перевірки залежно від специфікації...
        return nil
        
    case esio.SLConfigNull:
        return fmt.Errorf("SLConfigNull is forbidden by specification")
        
    default:
        return fmt.Errorf("unknown SLConfig predefined value: 0x%02X", sl.Predefined)
    }
}

// Використання при парсингі:
if err := ValidateSLConfig(desc.SLConfig); err != nil {
    log.Printf("warning: invalid SL config: %v", err)
    // Можна спробувати відновитися або використати дефолтні значення
}
```

---

## 🔑 2. parseSLConfig() — парсинг дескриптора

### 🔧 Основна логіка:

```go
func parseSLConfig(d []byte) (*SLConfigDescriptor, error) {
    // 1. Перевірка мінімальної довжини
    if len(d) == 0 {
        return nil, errors.New("SLConfigDescriptor short")
    }
    
    // 2. Читання predefined значення (1 байт)
    sl := &SLConfigDescriptor{Predefined: SLConfigPredefined(d[0])}
    
    // 3. Обробка кастомної конфігурації
    if sl.Predefined == SLConfigCustom {
        sl.Custom = d[1:]  // решта байт = кастомні дані
    }
    
    return sl, nil
}
```

### 🔍 Чому тільки 1 байт для стандартних конфігурацій?

```
Стандартні конфігурації (SLConfigMP4=0x02) мають фіксовані параметри,
визначені у специфікації. Тому достатньо тільки ідентифікатора (1 байт).

Кастомна конфігурація (SLConfigCustom=0x00) вимагає додаткових даних:
• Формат кастомних даних визначається застосунком
• Зазвичай це бітова маска параметрів + значення
• Довжина = загальна довжина дескриптора - 1 (заголовок)
```

### ⚠️ Критична проблема: відсутність валідації кастомних даних

```
У поточній реалізації:
    if sl.Predefined == SLConfigCustom {
        sl.Custom = d[1:]  // ← приймає будь-які дані без перевірки
    }

Проблема:
• Невалідні кастомні дані можуть призвести до помилок при використанні
• Неможливість виявлення пошкоджених файлів на етапі парсингу
• Потенційна вразливість до зловмисних вхідних даних

✅ Виправлення: додавання базової валідації
    if sl.Predefined == SLConfigCustom {
        custom := d[1:]
        // Мінімальна валідація: перевірка довжини
        if len(custom) < minCustomLength {  // напр. 2 байти для базових прапорців
            return nil, fmt.Errorf("custom SLConfig too short: %d bytes", len(custom))
        }
        // Додаткові перевірки залежно від очікуваного формату...
        sl.Custom = custom
    }
```

---

## 🔑 3. appendTo() — серіалізація дескриптора

### 🔧 Використання builder патерну:

```go
func (c *SLConfigDescriptor) appendTo(b *builder) error {
    if c == nil {
        return nil  // опціональний дескриптор
    }
    
    // 1. Початок дескриптора: tag + placeholder для length
    cursor := b.Descriptor(TagSLConfigDescriptor)
    defer cursor.DescriptorDone(-1)  // автоматичний розрахунок довжини
    
    // 2. Запис predefined значення
    b.WriteByte(byte(c.Predefined))
    
    // 3. Запис кастомних даних якщо потрібно
    if c.Predefined == SLConfigCustom {
        b.Write(c.Custom)
    }
    
    return nil
}
```

### 🔍 cursor патерн для variable-length size:

```
Проблема: довжина дескриптора відома тільки після запису всіх полів.

Рішення: cursor патерн
1. b.Descriptor(tag) → запис tag + резервування місця для length + повернення cursor
2. Запис всіх полів у буфер
3. cursor.DescriptorDone(actualLen) → повернення назад і запис правильної довжини

Приклад реалізації:
    type cursor struct {
        buf *bytes.Buffer
        lengthPos int  // позиція початку length поля
    }
    
    func (c cursor) DescriptorDone(actualLen int) {
        // Кодирування actualLen у variable-length формат
        encoded := encodeVariableLength(actualLen)
        // Запис назад у буфер на позицію c.lengthPos
        // (потрібна підтримка overwrite у builder)
    }
```

### ⚠️ Критична проблема: відсутність обробки помилок у Write

```
У поточному коді:
    b.Write(c.Custom)  // ← ніякої перевірки помилок!

Якщо builder використовує bytes.Buffer, Write зазвичай не повертає помилку,
але для network streams або custom writers це може бути проблемою.

✅ Виправлення: додавання error повернення у всі методи builder
    func (b *builder) Write(data []byte) error {
        _, err := b.buf.Write(data)
        return err
    }
    
    func (c *SLConfigDescriptor) appendTo(b *builder) error {
        // ...
        if c.Predefined == SLConfigCustom {
            if err := b.Write(c.Custom); err != nil {
                return fmt.Errorf("write custom SLConfig: %w", err)
            }
        }
        return nil
    }
```

---

## 🔑 4. Інтеграція з ES_Descriptor

### 🔧 Повна структура для AAC у MP4:

```
ES_Descriptor (tag=0x03)
├─ ES_ID: 2 bytes (напр. 2 для аудіо)
├─ flags: 1 byte (зазвичай 0)
├─ DecoderConfigDescriptor (tag=0x04)
│  ├─ objectTypeIndication: 1 byte (0x40 = AAC)
│  ├─ streamType: 1 byte (0x15 = AudioStream)
│  ├─ bufferSizeDB: 3 bytes (0)
│  ├─ maxBitrate: 4 bytes (напр. 200000)
│  ├─ avgBitrate: 4 bytes (напр. 128000)
│  └─ DecoderSpecificInfo (tag=0x05) ⭐
│     └─ AudioSpecificConfig: 2+ bytes ⭐
└─ SLConfigDescriptor (tag=0x06) ⭐
   └─ predefined: 1 byte (0x02 = SLConfigMP4)
```

### ✅ Ваш use-case**: генерація повного ES descriptor для AAC

```go
// GenerateAACDescriptor — створення повного ES descriptor для AAC потоку
func GenerateAACDescriptor(esid uint16, sampleRate int, channels int) (*esio.StreamDescriptor, error) {
    // 1. Генерація AudioSpecificConfig
    audioConfig, err := generateAudioSpecificConfig(sampleRate, channels)
    if err != nil {
        return nil, fmt.Errorf("generate AAC config: %w", err)
    }
    
    return &esio.StreamDescriptor{
        ESID: esid,
        DecoderConfig: &esio.DecoderConfigDescriptor{
            ObjectType: 0x40,        // AAC
            StreamType: 0x15,        // AudioStream
            BufferSizeDB: 0,
            MaxBitrate: 200000,      // 200 kbps
            AvgBitrate: 128000,      // 128 kbps
            DecSpecificInfo: audioConfig,
        },
        SLConfig: &esio.SLConfigDescriptor{
            Predefined: esio.SLConfigMP4,  // стандартна конфігурація
        },
    }, nil
}

// generateAudioSpecificConfig — створення 2-байтового AAC config
func generateAudioSpecificConfig(sampleRate, channels int) ([]byte, error) {
    // Мапінг частоти дискретизації у index
    freqIndex := map[int]int{
        96000: 0, 88200: 1, 64000: 2, 48000: 3,
        44100: 4, 32000: 5, 24000: 6, 22050: 7,
        16000: 8, 12000: 9, 11025: 10, 8000: 11,
    }
    
    fi, ok := freqIndex[sampleRate]
    if !ok {
        return nil, fmt.Errorf("unsupported sample rate: %d", sampleRate)
    }
    
    // Бітова упаковка: [audioObjectType:5][freqIndex:4][channels:4]
    // AAC LC = objectType 2
    config := uint16(2<<11) | uint16(fi<<7) | uint16(channels<<3)
    
    return []byte{byte(config >> 8), byte(config & 0xFF)}, nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Валідація SLConfig у MP4 файлі

```go
// ValidateMP4SLConfig — перевірка SLConfig у всіх треках MP4 файлу
func ValidateMP4SLConfig(filename string) error {
    f, err := os.Open(filename)
    if err != nil { return err }
    defer f.Close()
    
    // Парсинг атомів (спрощено)
    atoms, err := mp4io.ReadFileAtoms(f)
    if err != nil { return fmt.Errorf("parse atoms: %w", err) }
    
    // Пошук moov → trak → mdia → minf → stbl → stsd → mp4a → esds
    moov := mp4io.FindChildrenByName(atoms[0], "moov")
    if moov == nil { return fmt.Errorf("moov not found") }
    
    movie, ok := moov.(*mp4io.Movie)
    if !ok { return fmt.Errorf("unexpected moov type") }
    
    for i, trak := range movie.Tracks {
        if trak.Media == nil || trak.Media.Info == nil { continue }
        
        stsd := trak.Media.Info.Sample.SampleDesc
        if stsd == nil || stsd.MP4ADesc == nil { continue }
        
        // Пошук esds атому
        var esds *mp4io.ElemStreamDesc
        for _, unknown := range stsd.MP4ADesc.Unknowns {
            if unknown.Tag() == mp4io.ESDS {
                esds, _ = unknown.(*mp4io.ElemStreamDesc)
                break
            }
        }
        if esds == nil { continue }
        
        // Парсинг ES descriptor
        desc, err := esio.ParseStreamDescriptor(esds.DecConfig)
        if err != nil {
            return fmt.Errorf("track %d: parse ES descriptor: %w", i, err)
        }
        
        // Валідація SLConfig
        if err := ValidateSLConfig(desc.SLConfig); err != nil {
            log.Printf("warning: track %d: %v", i, err)
            // Не фатальна помилка — можна продовжити
        }
    }
    
    return nil
}
```

### 🔧 Приклад: Генерація esds атому для нового треку

```go
// CreateAACTrackWithESDS — створення треку з правильним esds атомом
func CreateAACTrackWithESDS(trackID uint16, sampleRate, channels int) (*mp4io.Track, error) {
    // 1. Генерація ES descriptor
    esDesc, err := GenerateAACDescriptor(trackID, sampleRate, channels)
    if err != nil {
        return nil, fmt.Errorf("generate ES descriptor: %w", err)
    }
    
    // 2. Серіалізація у байти
    esBytes, err := esDesc.Marshal()
    if err != nil {
        return nil, fmt.Errorf("marshal ES descriptor: %w", err)
    }
    
    // 3. Створення ElemStreamDesc атому
    esds := &mp4io.ElemStreamDesc{
        DecConfig: esBytes,  // ⚠️ Увага: DecConfig має містити тільки AudioSpecificConfig!
        TrackId:   trackID,
    }
    
    // 4. Створення треку
    track := &mp4io.Track{
        Header: &mp4io.TrackHeader{
            TrackId: int32(trackID),
            // ... інші параметри ...
        },
        Media: &mp4io.Media{
            Header: &mp4io.MediaHeader{
                TimeScale: int32(sampleRate),
            },
            Handler: &mp4io.HandlerRefer{
                SubType: [4]byte{'s', 'o', 'u', 'n'},
            },
            Info: &mp4io.MediaInfo{
                Sound: &mp4io.SoundMediaInfo{},
                Sample: &mp4io.SampleTable{
                    SampleDesc: &mp4io.SampleDesc{
                        MP4ADesc: &mp4io.MP4ADesc{
                            Conf: esds,  // ⭐ esds атом
                            // ... інші параметри ...
                        },
                    },
                },
            },
        },
    }
    
    return track, nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при доступі до d[1:]** | Короткі або пошкоджені файли | Додайте перевірку `if len(d) < 1` перед зрізом |
| **Невірне кодування variable-length size** | Серіалізовані дані не парситься | Переконайтеся що cursor.DescriptorDone() коректно записує length |
| **Відсутність обробки помилок у Write** | Помилки запису ігноруються | Додайте `error` повернення у всі методи builder |
| **Некоректне predefined значення** | Дескриптор не валідний за специфікацією | Додайте валідацію у ValidateSLConfig() |
| **Кастомні дані без формату** | Неможливість інтерпретації Custom []byte | Документуйте очікуваний формат кастомних даних |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування стандартних конфігурацій:

```go
// PrecomputedSLConfigs — кеш для стандартних конфігурацій
var slConfigCache = sync.Map{}  // map[SLConfigPredefined][]byte

func getCachedSLConfigBytes(cfg SLConfigPredefined) []byte {
    if cached, ok := slConfigCache.Load(cfg); ok {
        return cached.([]byte)
    }
    
    // Серіалізація стандартної конфігурації
    desc := &SLConfigDescriptor{Predefined: cfg}
    bytes, _ := desc.Marshal()  // ігноруємо помилку для кешу
    slConfigCache.Store(cfg, bytes)
    return bytes
}
```

### 2. Використання sync.Pool для буферів:

```go
var slConfigBufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func MarshalSLConfigPooled(cfg *SLConfigDescriptor) ([]byte, error) {
    buf := slConfigBufferPool.Get().(*bytes.Buffer)
    buf.Reset()
    defer slConfigBufferPool.Put(buf)
    
    // Серіалізація у буфер
    builder := &builder{buf: buf}
    if err := cfg.appendTo(builder); err != nil {
        return nil, err
    }
    
    // Копіювання результату (щоб уникнути проблем з пулом)
    result := make([]byte, buf.Len())
    copy(result, buf.Bytes())
    return result, nil
}
```

### 3. Моніторинг продуктивності парсингу:

```go
type SLConfigMetrics struct {
    ConfigsParsed prometheus.CounterVec
    ParseLatency  prometheus.HistogramVec
    CustomConfigs prometheus.CounterVec
    ParseErrors   prometheus.CounterVec
}

func (m *SLConfigMetrics) RecordParse(predefined SLConfigPredefined, customLen int, duration time.Duration, err error) {
    m.ConfigsParsed.WithLabelValues(fmt.Sprintf("0x%02X", predefined)).Inc()
    m.ParseLatency.WithLabelValues(fmt.Sprintf("0x%02X", predefined)).Observe(duration.Seconds())
    if predefined == SLConfigCustom {
        m.CustomConfigs.Observe(float64(customLen))
    }
    if err != nil {
        m.ParseErrors.WithLabelValues(fmt.Sprintf("0x%02X", predefined)).Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання SLConfigDescriptor

```go
// ✅ 1. Перевірка меж буфера перед доступом
if len(d) < 1 {
    return nil, fmt.Errorf("SLConfigDescriptor too short")
}

// ✅ 2. Валідація predefined значення
switch sl.Predefined {
case SLConfigMP4, SLConfigCustom:
    // валідні значення
default:
    return nil, fmt.Errorf("invalid SLConfig predefined: 0x%02X", sl.Predefined)
}

// ✅ 3. Обробка кастомних даних з перевіркою
if sl.Predefined == SLConfigCustom {
    if len(sl.Custom) < minCustomLength {
        return nil, fmt.Errorf("custom SLConfig too short")
    }
    // Додаткові перевірки формату...
}

// ✅ 4. Обробка помилок у серіалізації
func (c *SLConfigDescriptor) appendTo(b *builder) error {
    if err := b.WriteByte(byte(c.Predefined)); err != nil {
        return fmt.Errorf("write predefined: %w", err)
    }
    if c.Predefined == SLConfigCustom {
        if err := b.Write(c.Custom); err != nil {
            return fmt.Errorf("write custom: %w", err)
        }
    }
    return nil
}

// ✅ 5. Логування для дебагу
log.Printf("Parsed SLConfig: predefined=0x%02X, customLen=%d", 
    sl.Predefined, len(sl.Custom))

// ✅ 6. Метрики для моніторингу
metrics.RecordParse(sl.Predefined, len(sl.Custom), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 📄 [ISO/IEC 14496-1:2004 Systems](https://www.iso.org/standard/39484.html) — офіційний стандарт (платний)
- 📄 [MPEG-4 SL Config Specification](https://wiki.multimedia.cx/index.php/MPEG-4#SL_Configuration) — детальний опис формату
- 📄 [ESDS Atom Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap3/qtff3.html#//apple_ref/doc/uid/TP40000939-CH205-SW1) — Apple documentation
- 🧪 [Variable-Length Quantity Encoding](https://en.wikipedia.org/wiki/Variable-length_quantity) — теорія VLQ кодування
- 💻 [Go sync.Pool Documentation](https://pkg.go.dev/sync#Pool) — ефективне управління пам'яттю

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед доступом** — уникнення панік при пошкоджених файлах.
> 2. **Валідуйте predefined значення** — забезпечення сумісності зі специфікацією.
> 3. **Документуйте формат кастомних даних** — уникнення плутанини при підтримці кастомних конфігурацій.
> 4. **Кешуйте стандартні конфігурації** — прискорення серіалізації для частих випадків.
> 5. **Моніторьте `ParseErrors` метрику** — різке зростання може вказувати на пошкоджені файли або нові версії формату.

Потрібен приклад реалізації повного циклу генерації/парсингу esds атому з інтеграцією `esio` та `mp4io`, або інтеграція з вашим `mse.Muxer` для стрімінгу аудіо через WebSocket? Готовий допомогти! 🚀