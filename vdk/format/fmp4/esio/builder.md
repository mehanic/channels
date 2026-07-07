# 🧱 Глибокий розбір: esio — Builder/Cursor для бітової серіалізації

Цей файл — **реалізація ефективного бітового конструктора (builder)** для серіалізації даних у форматі з довжиною-префіксом, що часто використовується у бінарних протоколах (наприклад, MPEG-TS, ISO BMFF, або власні формати). Він надає інструменти для побудови бітових структур з підтримкою зворотного запису довжини (descriptor pattern).

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема esio пакету

```
┌────────────────────────────────────────┐
│ 📦 esio — Bitwise Builder/Cursor       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • builder — основний конструктор      │
│  • cursor — позиційний курсор для зворотного запису│
│  • Descriptor pattern — tag+length+value│
│                                         │
│  📊 Підтримувані типи:                  │
│  • WriteByte, WriteU16/24/32/64 — big-endian│
│  • Write([]byte) — append slice        │
│  • Grow(n) — попереднє виділення пам'яті│
│                                         │
│  🔄 Потік даних:                        │
│  builder.Grow() → Write*() → cursor.DescriptorDone()│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. builder — основний конструктор

### Структура та призначення:

```go
type builder struct {
    buf []byte  // внутрішній буфер для серіалізованих даних
}
```

### 🔧 Основні методи:

#### `Bytes()` — отримання результату:
```go
func (b *builder) Bytes() []byte {
    return b.buf  // повертає посилання на внутрішній буфер!
}
```
> ⚠️ **Увага**: повертає посилання, а не копію. Не модифікуйте результат після виклику інших методів `builder`.

#### `Grow(n)` — попереднє виділення пам'яті:
```go
func (b *builder) Grow(n int) []byte {
    pos := len(b.buf)
    b.buf = append(b.buf, make([]byte, n)...)
    return b.buf[pos:]  // повертає слайс на нову область
}
```
> ✅ **Ефективність**: дозволяє записувати дані у виділену область без проміжних аллокацій.

#### `Write*()` — серіалізація примітивів (big-endian):

```go
// WriteU16: 16-bit unsigned big-endian
func (b *builder) WriteU16(v uint16) {
    b.buf = append(b.buf, uint8(v>>8), uint8(v))
}

// WriteU24: 24-bit unsigned big-endian (спеціальний випадок для медіа-форматів)
func (b *builder) WriteU24(v uint32) {
    b.buf = append(b.buf, uint8(v>>16), uint8(v>>8), uint8(v))
}

// WriteU32: 32-bit unsigned big-endian
func (b *builder) WriteU32(v uint32) {
    b.buf = append(b.buf, uint8(v>>24), uint8(v>>16), uint8(v>>8), uint8(v))
}

// WriteU64: 64-bit unsigned big-endian
func (b *builder) WriteU64(v uint64) {
    b.buf = append(b.buf,
        uint8(v>>56), uint8(v>>48), uint8(v>>40), uint8(v>>32),
        uint8(v>>24), uint8(v>>16), uint8(v>>8), uint8(v),
    )
}
```

### ✅ Ваш use-case: серіалізація заголовку пакету

```go
// SerializePacketHeader — створення бітового заголовку для медіа-пакету
func SerializePacketHeader(streamID uint16, timestamp uint32, payloadLen uint32) []byte {
    var b esio.builder
    
    // Заголовок: [2-byte streamID][4-byte timestamp][4-byte length]
    b.WriteU16(streamID)
    b.WriteU32(timestamp)
    b.WriteU32(payloadLen)
    
    return b.Bytes()
}

// Використання:
header := SerializePacketHeader(0x1234, 1234567890, 1024)
// header = [0x12, 0x34, 0x49, 0x96, 0x02, 0xD2, 0x00, 0x00, 0x04, 0x00]
```

---

## 🔑 2. cursor — позиційний курсор для зворотного запису

### Призначення:
`cursor` дозволяє "заморозити" позицію у буфері, записати дані пізніше, а потім повернутися і оновити попередньо виділену область. Це критично для форматів, де довжина записується **після** даних (descriptor pattern).

### Структура:

```go
type cursor struct {
    builder *builder  // посилання на батьківський builder
    i, j    int       // діапазон [i, j) у buf, що належить цьому курсору
}
```

### 🔧 Методи:

#### `Cursor(length)` — створення курсору:
```go
func (b *builder) Cursor(length int) cursor {
    c := cursor{builder: b, i: len(b.buf)}  // запам'ятати поточну позицію
    b.Grow(length)                          // виділити місце
    c.j = len(b.buf)                        // запам'ятати кінець області
    return c
}
```

#### `Bytes()` — доступ до області курсору:
```go
func (c cursor) Bytes() []byte {
    return c.builder.buf[c.i:c.j]  // повертає слайс на виділену область
}
```

#### `DescriptorDone(length)` — завершення descriptor pattern:
```go
func (c cursor) DescriptorDone(length int) {
    if length < 0 {
        length = len(c.builder.buf) - c.j  // автоматичний розрахунок до кінця буфера
    }
    buf := c.Bytes()  // отримати область для запису довжини
    
    // Запис довжини у варіантному форматі (7 біт на байт, старший біт = continuation flag)
    for i := 3; i >= 0; i-- {
        v := byte(length >> uint(7*i) & 0x7f)  // витягнути 7 біт
        if i != 0 {
            v |= 0x80  // встановити continuation flag для всіх байт крім останнього
        }
        buf[3-i] = v  // записати у big-endian порядку
    }
}
```

### 🔍 Формат варіантної довжини (descriptor length):

```
Довжина кодується у 4 байтах, по 7 біт на байт:
• Біти 0-6: значення довжини
• Біт 7: флаг продовження (1=є наступний байт, 0=останній)

Приклади:
• Довжина 127 (0x7F): [0x7F] (1 байт)
• Довжина 128 (0x80): [0x81, 0x00] (2 байти: 0x80|0x01, 0x00)
• Довжина 16383 (0x3FFF): [0xFF, 0x7F] (2 байти)
• Довжина 16384 (0x4000): [0x81, 0x00, 0x00] (3 байти)

Це стандартний формат для MPEG-TS descriptor length та інших бінарних протоколів.
```

### ✅ Ваш use-case: серіалізація descriptor у MPEG-TS

```go
// WriteMPEGTSDescriptor — запис descriptor у форматі [tag][length][data]
func WriteMPEGTSDescriptor(b *esio.builder, tag esio.Tag, data []byte) {
    // 1. Створити курсор для області довжини (4 байти)
    cursor := b.Descriptor(tag)  // записує tag, повертає курсор для length
    
    // 2. Записати дані descriptor
    b.Write(data)
    
    // 3. Завершити descriptor: записати довжину даних
    cursor.DescriptorDone(len(data))  // або -1 для авто-розрахунку
}

// Приклад використання:
var b esio.builder
WriteMPEGTSDescriptor(&b, 0x02, []byte{0x01, 0x02, 0x03})  // video_stream_descriptor

// Результат: [0x02, 0x80, 0x00, 0x00, 0x03, 0x01, 0x02, 0x03]
//            ↑tag  ↑length(3)        ↑data
```

---

## 🔑 3. Descriptor pattern — tag+length+value

### Концепція:
Багато бінарних форматів (MPEG-TS, ISO BMFF, ASN.1 BER) використовують шаблон:
```
[1-byte tag][variable-length length][N bytes data]
```

### 🔧 Реалізація через `Descriptor()`/`DescriptorDone()`:

```go
// Descriptor(tag) — початок descriptor:
// 1. Записує 1-байтовий tag
// 2. Виділяє 4 байти для довжини (через Cursor(4))
// 3. Повертає курсор для подальшого завершення

func (b *builder) Descriptor(tag Tag) cursor {
    b.WriteByte(byte(tag))  // запис tag
    return b.Cursor(4)       // виділення 4 байт для length, повернення курсору
}

// cursor.DescriptorDone(length) — завершення descriptor:
// 1. Якщо length < 0, автоматично розраховує до кінця буфера
// 2. Кодирує довжину у варіантному форматі (7 біт/байт)
// 3. Записує у виділену область курсору
```

### ✅ Ваш use-case: побудова складної бітової структури

```go
// BuildComplexStructure — приклад вкладених descriptor'ів
func BuildComplexStructure() []byte {
    var b esio.builder
    
    // Зовнішній container descriptor
    outer := b.Descriptor(0x01)  // container tag
    
    // Внутрішній video descriptor
    videoData := []byte{0x01, 0x02, 0x03}  // приклад даних
    WriteMPEGTSDescriptor(&b, 0x02, videoData)
    
    // Внутрішній audio descriptor
    audioData := []byte{0x04, 0x05}
    WriteMPEGTSDescriptor(&b, 0x03, audioData)
    
    // Завершення зовнішнього descriptor: довжина = все що після tag
    outer.DescriptorDone(-1)  // авто-розрахунок
    
    return b.Bytes()
}

// Результат (спрощено):
// [0x01][length(outer)][0x02][length(video)][0x01,0x02,0x03][0x03][length(audio)][0x04,0x05]
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// ts_packet_builder.go — побудова MPEG-TS пакетів з esio
type TSPacketBuilder struct {
    builder esio.builder
    pid     uint16
}

func NewTSPacketBuilder(pid uint16) *TSPacketBuilder {
    return &TSPacketBuilder{pid: pid}
}

// BuildPacket — створення повного TS пакету (188 байт)
func (b *TSPacketBuilder) BuildPacket(payload []byte, continuityCounter uint8) ([]byte, error) {
    // 1. Очищення буфера
    b.builder = esio.builder{}
    
    // 2. Заголовок пакету (4 байти)
    b.builder.WriteU32(0x47 << 24)  // sync byte 0x47 + flags
    // ... додаткові біти заголовку (TEI, PUSI, priority, PID, etc.)
    
    // 3. Адаптаційне поле (опціонально)
    // ...
    
    // 4. Корисне навантаження
    b.builder.Write(payload)
    
    // 5. Заповнення до 188 байт (padding)
    if len(b.builder.Bytes()) < 188 {
        padding := make([]byte, 188-len(b.builder.Bytes()))
        b.builder.Write(padding)
    }
    
    return b.builder.Bytes(), nil
}

// WriteProgramMapSection — запис PMT section з descriptor'ами
func (b *TSPacketBuilder) WriteProgramMapSection(programNumber uint16, streams []StreamInfo) error {
    // PMT section має формат: [table_id][section_length][...][descriptors][CRC]
    
    // 1. Початок section
    b.builder.WriteByte(0x02)  // table_id for PMT
    sectionCursor := b.builder.Cursor(4)  // місце для section_length
    
    // 2. Запис основних полів PMT
    b.builder.WriteU16(programNumber)
    b.builder.WriteByte(0xC1)  // version, current_next_indicator, etc.
    b.builder.WriteU16(0xF000)  // program_info_length (поки 0)
    
    // 3. Запис stream loop
    for _, stream := range streams {
        b.builder.WriteByte(stream.StreamType)  // 0x1B=H.264, 0x0F=AAC, etc.
        b.builder.WriteU16(0xE000 | stream.ElementaryPID)  // PID з прапорцями
        esioCursor := b.builder.Cursor(4)  // місце для ES_info_length
        
        // Запис descriptor'ів для цього stream
        for _, desc := range stream.Descriptors {
            WriteMPEGTSDescriptor(&b.builder, desc.Tag, desc.Data)
        }
        esioCursor.DescriptorDone(-1)  // завершити ES_info_length
    }
    
    // 4. CRC32 (поки заглушка)
    b.builder.WriteU32(0x00000000)
    
    // 5. Завершення section_length
    sectionLength := len(b.builder.Bytes()) - sectionCursor.i - 4  // мінус заголовок + CRC
    sectionCursor.DescriptorDone(sectionLength)
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при access out of bounds** | `cursor.Bytes()` використовується після росту буфера | Не використовуйте `cursor.Bytes()` після виклику інших методів `builder`; зберігайте дані у тимчасову змінну якщо потрібно |
| **Неправильна довжина у descriptor** | `DescriptorDone(-1)` дає неправильний результат | Переконайтеся, що після виклику `Descriptor()` не було додаткових `Grow()` викликів, які змінили структуру буфера |
| **Невірний порядок байт** | Дані читаються неправильно на іншій платформі | Усі `WriteU*()` методи використовують big-endian; переконайтеся, що парсер також очікує big-endian |
| **Переповнення 24-бітного поля** | `WriteU24(v)` з v > 0xFFFFFF дає неправильний результат | Валідуйте вхідні дані: `if v > 0xFFFFFF { return error }` перед викликом `WriteU24()` |
| **Некоректне кодування варіантної довжини** | Довжина > 0x1FFFFF не кодується правильно | Максимальна довжина у 4-байтовому варіантному форматі: 0x1FFFFFFF (28 біт); валідуйте вхідні дані |

---

## ⚡ Оптимізації для real-time обробки

### 1. Попереднє виділення буфера конструктора:

```go
// NewPreallocatedBuilder — конструктор з попередньо виділеним буфером
func NewPreallocatedBuilder(capacity int) *esio.builder {
    return &esio.builder{
        buf: make([]byte, 0, capacity),  // нульова довжина, але ємність capacity
    }
}

// Використання для TS пакетів (фіксований розмір 188 байт):
builder := NewPreallocatedBuilder(188)
```

### 2. Пакетна серіалізація для зменшення аллокацій:

```go
// BatchWriteDescriptors — запис кількох descriptor'ів за один виклик
func BatchWriteDescriptors(b *esio.builder, descriptors []Descriptor) error {
    for _, desc := range descriptors {
        cursor := b.Descriptor(desc.Tag)
        b.Write(desc.Data)
        cursor.DescriptorDone(len(desc.Data))
    }
    return nil
}
```

### 3. Моніторинг продуктивності серіалізації:

```go
type SerializationMetrics struct {
    BytesSerialized prometheus.CounterVec
    SerializeLatency prometheus.HistogramVec
    DescriptorCount prometheus.CounterVec
}

func (m *SerializationMetrics) RecordSerialization(bytes int, descriptorCount int, duration time.Duration, context string) {
    m.BytesSerialized.WithLabelValues(context).Add(float64(bytes))
    m.SerializeLatency.WithLabelValues(context).Observe(duration.Seconds())
    m.DescriptorCount.WithLabelValues(context).Add(float64(descriptorCount))
}
```

---

## 📋 Чек-лист інтеграції esio

```go
// ✅ 1. Ініціалізація builder з адекватною ємністю
builder := &esio.builder{}
// або для відомих розмірів:
builder := NewPreallocatedBuilder(expectedSize)

// ✅ 2. Використання cursor для зворотного запису довжини
cursor := builder.Descriptor(tag)
// ... запис даних ...
cursor.DescriptorDone(-1)  // або конкретна довжина

// ✅ 3. Валідація діапазонів перед записом
if value > 0xFFFFFF {
    return fmt.Errorf("value too large for 24-bit field")
}
builder.WriteU24(value)

// ✅ 4. Отримання результату тільки після завершення серіалізації
result := builder.Bytes()
// Не модифікуйте result, якщо плануєте далі використовувати builder

// ✅ 5. Обробка помилок (хоча Write* методи не повертають error)
// Якщо потрібно обробляти помилки, обгорніть виклики:
func safeWriteU32(b *esio.builder, v uint32) error {
    defer func() {
        if r := recover(); r != nil {
            // Обробка паніки при нестачі пам'яті
        }
    }()
    b.WriteU32(v)
    return nil
}

// ✅ 6. Метрики для моніторингу
start := time.Now()
// ... серіалізація ...
metrics.RecordSerialization(len(result), descriptorCount, time.Since(start), "ts_packet")
```

---

## 🔗 Корисні посилання

- 📄 [MPEG-TS Specification (ISO/IEC 13818-1)](https://www.iso.org/standard/61246.html) — використання descriptor pattern
- 📄 [ASN.1 BER Encoding Rules](https://www.itu.int/ITU-T/studygroups/com17/languages/X.690-0207.pdf) — аналогічний формат довжини
- 📄 [ISO BMFF (MP4) File Format](https://chromium.googlesource.com/chromium/src.git/+/master/docs/codecs/mp4.md) — приклад вкладених структур з довжиною
- 🧪 [Go Slices: Usage and Internals](https://blog.golang.org/slices-intro) — розуміння поведінки слайсів у Go

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **медіа-потоками у реальному часі**:
> 1. **Попередньо виділяйте буфер конструктора** — це уникне дорогих реаллокацій при серіалізації тисяч пакетів на секунду.
> 2. **Використовуйте `cursor.DescriptorDone(-1)` тільки коли впевнені** — автоматичний розрахунок довжини може дати неправильний результат якщо буфер росте після створення курсору.
> 3. **Валідуйте вхідні дані перед записом** — `WriteU24()` не перевіряє переповнення; неправильні дані можуть зламати парсинг на стороні клієнта.
> 4. **Не зберігайте посилання на `builder.Bytes()`** — результат може стати недійсним після наступних викликів методів `builder`.
> 5. **Моніторьте `SerializeLatency`** — різке зростання може вказувати на проблеми з пам'яттю або перевантаження процесора.

Потрібен приклад інтеграції `TSPacketBuilder` з вашим `flv.Muxer` для конвертації FLV потоків у MPEG-TS для HLS? Готовий допомогти! 🚀