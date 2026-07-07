# 📦 Глибокий розбір: `mp4fio.ElemStreamDesc` — MPEG-4 Elementary Stream Descriptor (esds)

Цей файл — **реалізація атому `esds` (Elementary Stream Descriptor)** для опису аудіо/відео потоків у форматі MPEG-4. Він використовується переважно для AAC аудіо, де містить `AudioSpecificConfig` — критичні параметри для ініціалізації декодера.

---

## 🗺️ Архітектурна схема ElemStreamDesc

```
┌────────────────────────────────────────┐
│ 📦 ElemStreamDesc — esds Atom         │
├────────────────────────────────────────┤
│                                         │
│  🔑 Призначення:                        │
│  • Опис параметрів аудіо/відео потоку  │
│  • Містить AudioSpecificConfig для AAC │
│  • Використовується у MP4ADesc (stsd)  │
│                                         │
│  🔄 Формат MPEG-4 Descriptor:          │
│  [tag:1][length:VL][payload]           │
│  • Variable-Length encoding для size   │
│  • Nested descriptors (ES→DecConfig→DecSpecific)│
│                                         │
│  📡 Структура esds атому:              │
│  [size:4][tag:4='esds'][version:4]     │
│  [ES_Descriptor]                       │
│    ├─ tag=0x03, length=VL              │
│    ├─ ES_ID (2 bytes)                  │
│    ├─ flags (1 byte)                   │
│    ├─ DecoderConfigDescriptor          │
│    │  ├─ tag=0x04, length=VL           │
│    │  ├─ objectType=0x40 (AAC)         │
│    │  ├─ streamType=0x15 (Audio)       │
│    │  ├─ bufferSize, max/avg bitrate   │
│    │  └─ DecoderSpecificInfo           │
│    │     ├─ tag=0x05, length=VL        │
│    │     └─ AudioSpecificConfig (DecConfig)│
│    └─ SLConfigDescriptor (tag=0x06)    │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Структура та поля

```go
type ElemStreamDesc struct {
    DecConfig []byte    // 🎯 AudioSpecificConfig для AAC (критично!)
    TrackId   uint16    // ID треку у межах ES descriptor
    mp4io.AtomPos       // offset, size для навігації
}
```

### 🔍 DecConfig — найважливіше поле:

```
Для AAC це MPEG-4 AudioSpecificConfig (2+ байти):

Байти 0-1 (бітова структура):
  [0-4]  audioObjectType (5 біт): 2 = AAC LC, 5 = HE-AAC, тощо
  [5-8]  samplingFrequencyIndex (4 біти): 0=96kHz, 3=48kHz, 4=44.1kHz...
  [9-12] channelConfiguration (4 біти): 1=mono, 2=stereo...

Якщо samplingFrequencyIndex == 0xF (escape value):
  наступні 24 біти = explicit sampling frequency

Приклад для AAC-LC, 48kHz, stereo:
  0x1190 = 0001 0001 1001 0000
  • audioObjectType = 00010 (2 = AAC LC)
  • samplingFrequencyIndex = 0011 (3 = 48000 Hz)
  • channelConfiguration = 0010 (2 = stereo)
```

### ✅ Ваш use-case: ініціалізація AAC декодера

```go
// InitAACDecoderFromESDS — створення CodecData з esds
func InitAACDecoderFromESDS(esds *mp4fio.ElemStreamDesc) (av.CodecData, error) {
    if esds == nil || len(esds.DecConfig) == 0 {
        return nil, fmt.Errorf("empty AAC config in esds")
    }
    
    // aacparser очікує сирий AudioSpecificConfig
    return aacparser.NewCodecDataFromMPEG4AudioConfigBytes(esds.DecConfig)
}

// Використання при парсингі MP4:
track := findAudioTrack(moov)
mp4aDesc := track.Media.Info.Sample.SampleDesc.MP4ADesc
if mp4aDesc == nil || mp4aDesc.Conf == nil {
    return fmt.Errorf("no AAC config found")
}
codecData, err := InitAACDecoderFromESDS(mp4aDesc.Conf)
if err != nil {
    return fmt.Errorf("init AAC decoder: %w", err)
}
```

---

## 🔑 2. MPEG-4 Descriptor Format — Variable-Length Encoding

### 🔧 Формат довжини (Variable-Length):

```go
func (self ElemStreamDesc) fillLength(b []byte, length int) (n int) {
    b[n] = uint8(length & 0x7f)  // ⚠️ Тільки 7 біт! Максимум 127
    n++
    return
}
```

**🔍 Специфікація MPEG-4 SL:**

```
Довжина дескриптора кодується у форматі "більшість-перший" (MSB first):
• Кожен байт: 7 біт даних + 1 біт продовження (0x80)
• Якщо байт & 0x80 != 0 → є наступний байт довжини
• Максимальна довжина: 4 байти → 28 біт даних → ~268 млн байт

Приклади:
  100 = 0x64 → [0x64] (1 байт, біт продовження = 0)
  300 = 0x12C → [0x81, 0x2C] (2 байти: 1*128 + 44 = 172? Ні, це складніше)
  
Справжня логіка декодування:
  length = 0
  for each byte:
    length = (length << 7) | (byte & 0x7F)
    if byte & 0x80 == 0: break  // останній байт
```

### ⚠️ Критична проблема: `fillLength` не підтримує багатобайтові значення

```
Поточна реалізація:
    b[n] = uint8(length & 0x7f)  // ← тільки 7 біт, максимум 127!

Наслідки:
• Якщо `len(self.DecConfig) > 127` → некоректна серіалізація
• Декодер не зможе правильно прочитати довжину дескриптора
• AAC декодер не ініціалізується → помилка відтворення

✅ Виправлення: повна реалізація variable-length encoding:

func (self ElemStreamDesc) fillLength(b []byte, length int) (n int) {
    // Запис у зворотному порядку (LSB first для простоти)
    var bytes []byte
    for {
        b := byte(length & 0x7F)
        length >>= 7
        if length > 0 {
            b |= 0x80  // біт продовження
        }
        bytes = append(bytes, b)
        if length == 0 {
            break
        }
    }
    // Копіювання у правильному порядку (MSB first)
    for i := len(bytes) - 1; i >= 0; i-- {
        b[n] = bytes[i]
        n++
    }
    return
}
```

---

## 🔑 3. Marshal — серіалізація esds атому

### 🔧 Поточна реалізація (з проблемами):

```go
func (self ElemStreamDesc) Marshal(b []byte) (n int) {
    pio.PutU32BE(b[4:], uint32(mp4io.ESDS))  // запис tag 'esds'
    n += 8  // пропуск заголовку атому
    
    pio.PutU32BE(b[n:], 0)  // Version + Flags = 0
    n += 4
    
    datalen := self.Len()  // ⚠️ Може бути некоректним через fillLength!
    
    // Заповнення ES Descriptor
    n += self.fillESDescHdr(b[n:], datalen-n-self.lenESDescHdr()+3)
    
    // Заповнення DecoderConfigDescriptor
    n += self.fillDecConfigDescHdr(b[n:], datalen-n-self.lenDescHdr()-3)
    
    // Копіювання DecConfig (AudioSpecificConfig)
    copy(b[n:], self.DecConfig)
    n += len(self.DecConfig)
    
    // Заповнення SLConfigDescriptor (tag=0x06)
    n += self.fillDescHdr(b[n:], 0x06, datalen-n-self.lenDescHdr())
    b[n] = 0x02  // значення для SLConfigDescriptor
    n++
    
    // Запис загального розміру атому
    pio.PutU32BE(b[0:], uint32(n))
    return
}
```

### 🔍 Детальна структура запису:

```
esds атом (загальна структура):
  [size:4][tag:4='esds'][version:4=0]
  [ES_Descriptor]
    ├─ [tag:1=0x03][length:VL][ES_ID:2][flags:1]
    ├─ [DecoderConfigDescriptor]
    │  ├─ [tag:1=0x04][length:VL]
    │  ├─ [objectType:1=0x40 (AAC)][streamType:1=0x15 (Audio)]
    │  ├─ [bufferSizeDB:3=0][maxBitrate:4][avgBitrate:4]
    │  └─ [DecoderSpecificInfo]
    │     ├─ [tag:1=0x05][length:VL]
    │     └─ [DecConfig: N bytes] ← AudioSpecificConfig
    └─ [SLConfigDescriptor]
       ├─ [tag:1=0x06][length:VL=1]
       └─ [value:1=0x02]
```

### ⚠️ Проблеми у Marshal:

#### ❌ 1. Неправильний розрахунок `datalen`

```go
datalen := self.Len()  // ← Len() може бути некоректним
// ...
n += self.fillESDescHdr(b[n:], datalen-n-self.lenESDescHdr()+3)  // ← "магічні" +3/-3
```

**Проблема**: "Магічні числа" `+3`/`-3` не документовані і можуть призвести до некоректних довжин вкладених дескрипторів.

**✅ Виправлення**: Розраховувати довжини послідовно, без "магічних" корекцій:

```go
func (self ElemStreamDesc) Marshal(b []byte) (n int) {
    // 1. Запис заголовку атому
    pio.PutU32BE(b[4:], uint32(mp4io.ESDS))
    n += 8
    
    // 2. Version + Flags
    pio.PutU32BE(b[n:], 0)
    n += 4
    
    // 3. Розрахунок довжини DecConfig дескриптора
    decSpecificLen := len(self.DecConfig)
    decConfigLen := 2 + 3 + 4 + 4 + (1 + self.calcVLLen(decSpecificLen) + decSpecificLen)
    
    // 4. Розрахунок довжини ES дескриптора
    esPayloadLen := 2 + 1 + (1 + self.calcVLLen(decConfigLen) + decConfigLen) + (1 + self.calcVLLen(1) + 1)
    
    // 5. Запис ES Descriptor
    n += self.writeESDescriptor(b[n:], esPayloadLen)
    
    return n
}

// calcVLLen — розрахунок розміру variable-length поля
func (self ElemStreamDesc) calcVLLen(length int) int {
    if length < 128 { return 1 }
    if length < 16384 { return 2 }
    if length < 2097152 { return 3 }
    return 4
}
```

#### ❌ 2. Відсутність `Tag()` методу

```
ElemStreamDesc реалізує mp4io.Atom інтерфейс, але:
• немає методу `Tag() mp4io.Tag` → неможливо ідентифікувати атом
• немає повноцінного `Unmarshal()` → неможливо парсити з байтів
• `Children()` повертає nil, але це правильно для листяного атому

✅ Виправлення: додати відсутні методи:

func (self ElemStreamDesc) Tag() mp4io.Tag {
    return mp4io.ESDS
}

func (self *ElemStreamDesc) Unmarshal(b []byte, offset int) (n int, err error) {
    (&self.AtomPos).setPos(offset, len(b))
    n += 8  // пропуск заголовку атому
    
    // Пропуск version/flags
    if len(b) < n+4 { return n, fmt.Errorf("short esds header") }
    n += 4
    
    // Парсинг ES Descriptor (спрощено)
    var tag uint8
    var length int
    if tag, length, n, err = self.readDescriptorHeader(b, n); err != nil { return }
    if tag != mp4io.MP4ESDescrTag { return n, fmt.Errorf("expected ES descriptor") }
    
    // ES_ID + flags
    if len(b) < n+3 { return n, fmt.Errorf("short ES header") }
    self.TrackId = pio.U16BE(b[n:])
    n += 3
    
    // Парсинг DecoderConfigDescriptor для витягування DecConfig
    if tag, length, n, err = self.readDescriptorHeader(b, n); err != nil { return }
    if tag != mp4io.MP4DecConfigDescrTag { return n, fmt.Errorf("expected DecConfig") }
    
    // Пропуск objectType, streamType, bufferSize, bitrates
    n += 2 + 3 + 4 + 4
    
    // Парсинг DecoderSpecificInfo (DecConfig)
    if tag, length, n, err = self.readDescriptorHeader(b, n); err != nil { return }
    if tag != mp4io.MP4DecSpecificDescrTag { return n, fmt.Errorf("expected DecSpecific") }
    
    if len(b) < n+length { return n, fmt.Errorf("short DecConfig") }
    self.DecConfig = make([]byte, length)
    copy(self.DecConfig, b[n:n+length])
    n += length
    
    return n, nil
}

// readDescriptorHeader — читання tag + variable-length length
func (self *ElemStreamDesc) readDescriptorHeader(b []byte, offset int) (tag uint8, length, n int, err error) {
    if len(b) < offset+1 { return 0, 0, 0, fmt.Errorf("short descriptor header") }
    tag = b[offset]
    n = offset + 1
    
    // Читання variable-length length
    length = 0
    for n < len(b) && n-offset < 4 {
        b := b[n]
        n++
        length = (length << 7) | int(b&0x7F)
        if b&0x80 == 0 { break }
    }
    return
}
```

---

## 🔑 4. Len() — розрахунок розміру атому

### 🔧 Поточна реалізація:

```go
func (self ElemStreamDesc) Len() (n int) {
    return 8 +  // size + tag атому
        4 +     // version + flags
        self.lenESDescHdr() +
        self.lenDecConfigDescHdr() +
        len(self.DecConfig) +
        self.lenDescHdr() + 1  // SLConfigDescriptor
}
```

### ⚠️ Проблема: не враховує variable-length encoding

```
lenDescHdr() повертає 2 (tag:1 + length:1), але:
• Якщо довжина дескриптора > 127, length займає 2+ байти
• Це призводить до некоректного розрахунку загального розміру

✅ Виправлення: використовувати calcVLLen для точного розрахунку:

func (self ElemStreamDesc) lenDescHdrWithVL(length int) int {
    return 1 + self.calcVLLen(length)  // tag:1 + variable-length
}

func (self ElemStreamDesc) Len() (n int) {
    decSpecificLen := len(self.DecConfig)
    decConfigLen := 2+3+4+4 + (1 + self.calcVLLen(decSpecificLen) + decSpecificLen)
    esPayloadLen := 2+1 + (1 + self.calcVLLen(decConfigLen) + decConfigLen) + (1 + self.calcVLLen(1) + 1)
    
    return 8 + 4 + (1 + self.calcVLLen(esPayloadLen) + esPayloadLen)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення AAC треку з esds

```go
// CreateAACStream — ініціалізація аудіо треку з AAC конфігурацією
func CreateAACStream(sampleRate int, channels int, profile int) (*mp4io.Track, error) {
    // 1. Генерація AudioSpecificConfig
    audioConfig, err := generateAudioSpecificConfig(sampleRate, channels, profile)
    if err != nil {
        return nil, fmt.Errorf("generate AAC config: %w", err)
    }
    
    // 2. Створення ElemStreamDesc
    esds := &mp4fio.ElemStreamDesc{
        DecConfig: audioConfig,
        TrackId:   1,
    }
    
    // 3. Створення MP4ADesc (sample description)
    mp4a := &mp4io.MP4ADesc{
        DataRefIdx:       1,
        NumberOfChannels: int16(channels),
        SampleSize:       16,
        SampleRate:       float64(sampleRate),
        Conf:             esds,
    }
    
    // 4. Створення SampleDesc
    stsd := &mp4io.SampleDesc{
        MP4ADesc: mp4a,
    }
    
    // 5. Створення SampleTable
    stbl := &mp4io.SampleTable{
        SampleDesc:   stsd,
        TimeToSample: &mp4io.TimeToSample{},
        // ... інші таблиці ...
    }
    
    // 6. Створення треку
    track := &mp4io.Track{
        Header: &mp4io.TrackHeader{
            TrackId: 1,
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
                Sound:  &mp4io.SoundMediaInfo{},
                Sample: stbl,
            },
        },
    }
    
    return track, nil
}

// generateAudioSpecificConfig — створення 2-байтового AAC config
func generateAudioSpecificConfig(sampleRate, channels, profile int) ([]byte, error) {
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
    config := uint16(profile<<11) | uint16(fi<<7) | uint16(channels<<3)
    
    return []byte{byte(config >> 8), byte(config & 0xFF)}, nil
}
```

### 🔧 Приклад: Парсинг esds з існуючого файлу

```go
// ExtractAACConfigFromMP4 — витягування AAC config з MP4 файлу
func ExtractAACConfigFromMP4(filename string) ([]byte, error) {
    f, err := os.Open(filename)
    if err != nil { return nil, err }
    defer f.Close()
    
    // Парсинг атомів (спрощено)
    atoms, err := mp4io.ReadFileAtoms(f)
    if err != nil { return nil, err }
    
    // Пошук moov → trak → mdia → minf → stbl → stsd → mp4a → esds
    moov := mp4io.FindChildrenByName(atoms[0], "moov")
    if moov == nil { return nil, fmt.Errorf("moov not found") }
    
    for _, trak := range moov.(*mp4io.Movie).Tracks {
        if trak.Media == nil || trak.Media.Handler == nil { continue }
        if string(trak.Media.Handler.SubType[:]) != "soun" { continue }
        
        stsd := trak.Media.Info.Sample.SampleDesc
        if stsd == nil || stsd.MP4ADesc == nil { continue }
        
        esds := stsd.MP4ADesc.Conf
        if esds == nil || len(esds.DecConfig) == 0 { continue }
        
        return esds.DecConfig, nil
    }
    
    return nil, fmt.Errorf("AAC config not found")
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **fillLength не підтримує >127** | Некоректна серіалізація великих DecConfig | Реалізувати повне variable-length encoding |
| **Відсутній Tag() метод** | Неможливо ідентифікувати атом у навігації | Додати `func (ElemStreamDesc) Tag() mp4io.Tag` |
| **Порожній Unmarshal** | Неможливо парсити esds з байтів | Реалізувати парсинг descriptor-ів з перевіркою тегів |
| **Некоректний Len()** | Розмір атому не співпадає з реальним | Використовувати calcVLLen для точного розрахунку |
| **"Магічні числа" +3/-3** | Непередбачувана поведінка при зміні структури | Замінити на послідовний розрахунок довжин |

---

## ⚡ Оптимізації для high-performance streaming

### 1. Кешування серіалізованого esds:

```go
type CachedElemStreamDesc struct {
    *ElemStreamDesc
    serialized []byte
    dirty      bool
    mu         sync.RWMutex
}

func (c *CachedElemStreamDesc) Marshal(b []byte) (n int) {
    c.mu.RLock()
    if !c.dirty && len(c.serialized) > 0 {
        n = copy(b, c.serialized)
        c.mu.RUnlock()
        return n
    }
    c.mu.RUnlock()
    
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Серіалізація якщо не в кеші
    n = c.ElemStreamDesc.Marshal(b)
    c.serialized = make([]byte, n)
    copy(c.serialized, b[:n])
    c.dirty = false
    return n
}

func (c *CachedElemStreamDesc) MarkDirty() {
    c.mu.Lock()
    c.dirty = true
    c.serialized = nil
    c.mu.Unlock()
}
```

### 2. Попередня валідація DecConfig:

```go
// ValidateAACConfig — перевірка коректності AudioSpecificConfig
func ValidateAACConfig(config []byte) error {
    if len(config) < 2 {
        return fmt.Errorf("AAC config too short: %d bytes", len(config))
    }
    
    // Перевірка audioObjectType (біти 7-3 першого байта)
    objectType := (config[0] >> 3) & 0x1F
    if objectType == 0 || objectType > 31 {
        return fmt.Errorf("invalid audioObjectType: %d", objectType)
    }
    
    // Перевірка samplingFrequencyIndex (біти 2-0 першого + біт 7 другого)
    freqIndex := ((config[0] & 0x07) << 1) | (config[1] >> 7)
    if freqIndex == 0xF {
        // Escape value: перевірка explicit frequency (24 біти)
        if len(config) < 5 {
            return fmt.Errorf("short config for explicit frequency")
        }
    } else if freqIndex > 12 {
        return fmt.Errorf("invalid samplingFrequencyIndex: %d", freqIndex)
    }
    
    // Перевірка channelConfiguration (біти 6-3 другого байта)
    channels := (config[1] >> 3) & 0x0F
    if channels == 0 || channels > 8 {
        return fmt.Errorf("invalid channelConfiguration: %d", channels)
    }
    
    return nil
}
```

### 3. Моніторинг продуктивності:

```go
type ESDSMetrics struct {
    SerializationLatency prometheus.HistogramVec
    ConfigSize           prometheus.HistogramVec
    ParseErrors          prometheus.CounterVec
}

func (m *ESDSMetrics) RecordSerialization(duration time.Duration, configSize int, streamID string) {
    m.SerializationLatency.WithLabelValues(streamID).Observe(duration.Seconds())
    m.ConfigSize.WithLabelValues(streamID).Observe(float64(configSize))
}
```

---

## 📋 Чек-лист безпечного використання ElemStreamDesc

```go
// ✅ 1. Валідація DecConfig перед використанням
if err := ValidateAACConfig(esds.DecConfig); err != nil {
    return fmt.Errorf("invalid AAC config: %w", err)
}

// ✅ 2. Перевірка variable-length encoding для великих config
if len(esds.DecConfig) > 127 {
    // Переконайтеся, що fillLength підтримує багатобайтові довжини
}

// ✅ 3. Додавання Tag() методу для сумісності з mp4io.Atom
func (self ElemStreamDesc) Tag() mp4io.Tag { return mp4io.ESDS }

// ✅ 4. Реалізація Unmarshal для парсингу з байтів
// ✅ 5. Уникнення "магічних чисел" у розрахунку довжин
// ✅ 6. Кешування серіалізованого результату для повторного використання
// ✅ 7. Метрики для моніторингу продуктивності серіалізації
```

---

## 🔗 Корисні посилання

- 📄 [MPEG-4 Systems Descriptor Syntax (ISO/IEC 14496-1)](https://www.iso.org/standard/69986.html) — офіційна специфікація
- 📄 [AAC AudioSpecificConfig](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — детальний опис формату
- 📄 [ESDS Atom Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap3/qtff3.html#//apple_ref/doc/uid/TP40000939-CH205-SW1) — Apple QuickTime documentation
- 🧪 [Variable-Length Integer Encoding](https://en.wikipedia.org/wiki/Variable-length_quantity) — теорія VLQ кодування
- 💻 [Go encoding/binary](https://pkg.go.dev/encoding/binary) — бітова робота з числами

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди валідуйте `DecConfig`** — некоректний AudioSpecificConfig призведе до падіння декодера.
> 2. **Реалізуйте повне variable-length encoding** — інакше config >127 байт не серіалізується коректно.
> 3. **Додайте `Tag()` та `Unmarshal()`** — для сумісності з `mp4io.Atom` інтерфейсом та навігацією.
> 4. **Уникайте "магічних чисел"** — замініть `+3`/`-3` на послідовний розрахунок довжин дескрипторів.
> 5. **Кешуйте серіалізований результат** — esds не змінюється протягом сесії, кешування економить CPU.

Потрібен приклад інтеграції `ElemStreamDesc` з вашим `mp4.Muxer` для автоматичної генерації esds при створенні AAC треків? Готовий допомогти! 🚀