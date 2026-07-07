# 📦 Глибокий розбір: `pio` — Бітові утиліти для Big-Endian/Little-Endian конвертації

Цей файл — **набір низькорівневих функцій** для читання цілочисельних значень різних розмірів (8-64 біт) з масивів байт у форматах Big-Endian (BE) або Little-Endian (LE). Це критично важливо для парсингу бінарних протоколів (RTSP, RTP, MPEG-TS, FLV тощо), де порядок байт визначається стандартом.

---

## 🗺️ Архітектурна схема pio пакету

```
┌────────────────────────────────────────┐
│ 📦 pio — Bitwise Conversion Utilities │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові функції:                    │
│  • U*/I* — unsigned/signed конвертація │
│  • BE/LE — Big-Endian/Little-Endian    │
│  • Розміри: 8, 16, 24, 32, 40, 64 біт  │
│                                         │
│  🔄 Потік даних:                        │
│  []byte → бітові операції → uint*/int* │
│                                         │
│  📡 Використання:                       │
│  • Парсинг заголовків мережевих протоколів│
│  • Робота з медіа-контейнерами         │
│  • Серіалізація/десеріалізація бінарних даних│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Основи: Big-Endian vs Little-Endian

### 🔍 Чому це важливо?

```
Бінарні протоколи часто використовують Network Byte Order (Big-Endian):
• Перший байт = старші біти (most significant)
• Останній байт = молодші біти (least significant)

Приклад: число 0x12345678 у пам'яті:
• Big-Endian:    [0x12][0x34][0x56][0x78] ← мережевий стандарт
• Little-Endian: [0x78][0x56][0x34][0x12] ← x86/x64 архітектури

Неправильний порядок → неправильні значення → помилки парсингу!
```

### 🔧 Як працює U32BE (приклад):

```go
func U32BE(b []byte) (i uint32) {
    i = uint32(b[0])           // i = 0x12
    i <<= 8; i |= uint32(b[1]) // i = 0x1200 | 0x34 = 0x1234
    i <<= 8; i |= uint32(b[2]) // i = 0x123400 | 0x56 = 0x123456
    i <<= 8; i |= uint32(b[3]) // i = 0x12345600 | 0x78 = 0x12345678
    return
}
```

### 🔧 Як працює U32LE (зворотний порядок):

```go
func U32LE(b []byte) (i uint32) {
    i = uint32(b[3])           // i = 0x78 (останній байт = молодші біти)
    i <<= 8; i |= uint32(b[2]) // i = 0x7800 | 0x56 = 0x7856
    i <<= 8; i |= uint32(b[1]) // i = 0x785600 | 0x34 = 0x785634
    i <<= 8; i |= uint32(b[0]) // i = 0x78563400 | 0x12 = 0x78563412
    return
}
```

---

## 🔑 2. Особливі випадки: 24-бітні та 40-бітні значення

### 🔍 Чому 24 біти?

```
Деякі протоколи використовують нестандартні розміри:
• MPEG-TS: довжина секції = 12 біт, але часто зберігається як 24 біти
• RTP: timestamp extensions, padding lengths
• FLV: розміри даних у тегах

24 біти = 3 байти → не вміщується у стандартні типи (uint16/uint32)
→ використовуємо int32/uint32 з ігноруванням старшого байта
```

### 🔧 I24BE vs U24BE — знакові vs беззнакові:

```go
// U24BE — беззнакове 24-бітне число (0..16,777,215)
func U24BE(b []byte) (i uint32) {
    i = uint32(b[0])
    i <<= 8; i |= uint32(b[1])
    i <<= 8; i |= uint32(b[2])
    return
}

// I24BE — знакове 24-бітне число (-8,388,608..8,388,607)
func I24BE(b []byte) (i int32) {
    // ❗ Ключовий момент: перший байт конвертуємо у int8 для збереження знаку
    i = int32(int8(b[0]))  // sign extension!
    i <<= 8; i |= int32(b[1])
    i <<= 8; i |= int32(b[2])
    return
}
```

### 🔍 Приклад sign extension:

```
Вхід: []byte{0xFF, 0xFF, 0xFF}  // 24 біти = 0xFFFFFF

U24BE:
  i = uint32(0xFF) = 0x000000FF
  i = 0x0000FFFF | 0xFF = 0x00FFFFFF = 16,777,215

I24BE:
  i = int32(int8(0xFF)) = int32(-1) = 0xFFFFFFFF  // sign extension!
  i = 0xFFFFFFFF << 8 | 0xFF = 0xFFFFFF00 | 0xFF = 0xFFFFFFFF = -1

Результат:
• U24BE([0xFF,0xFF,0xFF]) = 16777215
• I24BE([0xFF,0xFF,0xFF]) = -1
```

### ✅ Ваш use-case: парсинг MPEG-TS section_length

```go
// ParseMPEGTSSectionLength — отримання довжини секції (12 біт у 2 байтах)
func ParseMPEGTSSectionLength(b []byte) (int, error) {
    if len(b) < 2 {
        return 0, fmt.Errorf("too short for section_length")
    }
    
    // section_length у бітах 11-0 другого байта + біти 7-4 першого
    // Формат: [0][0][section_length(12)]
    raw := pio.U16BE(b) & 0x0FFF  // маска на нижні 12 біт
    return int(raw), nil
}
```

---

## 🔑 3. 40-бітні значення: PTS/DTS у RTP

### 🔍 Чому 40 біт?

```
RTP Payload Format for H.264 (RFC 6184) використовує 40-бітні поля для:
• Presentation Timestamp (PTS)
• Decoding Timestamp (DTS)

Формат: 5 байт = 40 біт
• Біти 39-32: старші 8 біт
• ...
• Біти 7-0: молодші 8 біт

Це дозволяє представити час з точністю до 90kHz у діапазоні ~26 годин.
```

### 🔧 U40BE реалізація:

```go
func U40BE(b []byte) (i uint64) {
    i = uint64(b[0])
    i <<= 8; i |= uint64(b[1])
    i <<= 8; i |= uint64(b[2])
    i <<= 8; i |= uint64(b[3])
    i <<= 8; i |= uint64(b[4])
    return
}
```

### ✅ Ваш use-case: конвертація RTP timestamp → time.Duration

```go
// RTPTimeToDuration — конвертація 40-бітного RTP timestamp у time.Duration
func RTPTimeToDuration(b []byte, clockRate int64) (time.Duration, error) {
    if len(b) < 5 {
        return 0, fmt.Errorf("too short for 40-bit timestamp")
    }
    
    ts := pio.U40BE(b)  // 40-бітне значення
    // Конвертація: ticks → секунди → duration
    return time.Duration(ts * uint64(time.Second) / uint64(clockRate)), nil
}

// Використання для H.264 @ 90kHz:
duration, err := RTPTimeToDuration(rtpTimestampBytes, 90000)
if err != nil { /* handle error */ }
```

---

## 🔑 4. Знакові vs беззнакові: I* vs U*

### 🔍 Коли використовувати знакові типи?

```
Більшість протоколів використовують беззнакові значення для:
• Розмірів, довжин, ідентифікаторів → U*
• Timestamps, sequence numbers → U*

Знакові типи потрібні для:
• Зміщень (offsets), які можуть бути від'ємними
• Різниць часу (delta), які можуть бути від'ємними
• Специфічних полів протоколів, що вимагають sign extension

Приклад: Composition Time Offset у H.264 може бути від'ємним для B-frames.
```

### 🔧 I32BE vs U32BE — порівняння:

```go
// Вхід: []byte{0xFF, 0xFF, 0xFF, 0xFF}

U32BE:
  i = uint32(0xFF) = 0x000000FF
  ... = 0xFFFFFFFF = 4,294,967,295

I32BE:
  i = int32(int8(0xFF)) = int32(-1) = 0xFFFFFFFF  // sign extension!
  ... = 0xFFFFFFFF = -1

Результат:
• U32BE([0xFF,0xFF,0xFF,0xFF]) = 4294967295
• I32BE([0xFF,0xFF,0xFF,0xFF]) = -1
```

### ✅ Ваш use-case: обробка від'ємних Composition Time

```go
// ParseCompositionTimeOffset — парсинг від'ємного зсуву часу
func ParseCompositionTimeOffset(b []byte) (time.Duration, error) {
    if len(b) < 3 {
        return 0, fmt.Errorf("too short for 24-bit offset")
    }
    
    // Composition time offset може бути від'ємним → використовуємо I24BE
    offsetTicks := pio.I24BE(b)  // знакове 24-бітне значення
    
    // Конвертація у duration @ 90kHz
    return time.Duration(offsetTicks) * time.Second / 90000, nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// rtsp_packet_parser.go — парсинг RTP пакетів з використанням pio
type RTPPacketParser struct {
    clockRate int64
}

func NewRTPPacketParser(clockRate int64) *RTPPacketParser {
    return &RTPPacketParser{clockRate: clockRate}
}

// ParseRTPHeader — парсинг стандартного 12-байтового RTP заголовку
func (p *RTPPacketParser) ParseRTPHeader(b []byte) (*RTPHeader, error) {
    if len(b) < 12 {
        return nil, fmt.Errorf("RTP header too short: %d < 12", len(b))
    }
    
    // Байт 0: V(2) P(1) X(1) CC(4)
    firstByte := pio.U8(b[0:1])
    version := (firstByte >> 6) & 0x3
    padding := (firstByte >> 5) & 0x1 != 0
    extension := (firstByte >> 4) & 0x1 != 0
    csrcCount := int(firstByte & 0x0F)
    
    // Байт 1: M(1) PT(7)
    secondByte := pio.U8(b[1:2])
    marker := (secondByte >> 7) & 0x1 != 0
    payloadType := secondByte & 0x7F
    
    // Байти 2-3: Sequence Number (16 біт BE)
    sequenceNumber := pio.U16BE(b[2:4])
    
    // Байти 4-7: Timestamp (32 біт BE)
    timestamp := pio.U32BE(b[4:8])
    
    // Байти 8-11: SSRC (32 біт BE)
    ssrc := pio.U32BE(b[8:12])
    
    return &RTPHeader{
        Version:        version,
        Padding:        padding,
        Extension:      extension,
        CSRCCount:      csrcCount,
        Marker:         marker,
        PayloadType:    payloadType,
        SequenceNumber: sequenceNumber,
        Timestamp:      timestamp,
        SSRC:           ssrc,
        ClockRate:      p.clockRate,
    }, nil
}

// ParseH264Timestamp — парсинг 40-бітного PTS для H.264
func (p *RTPPacketParser) ParseH264Timestamp(b []byte) (time.Duration, error) {
    if len(b) < 5 {
        return 0, fmt.Errorf("too short for 40-bit timestamp")
    }
    
    pts := pio.U40BE(b)
    return time.Duration(pts * uint64(time.Second) / uint64(p.clockRate)), nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при access out of bounds** | `b[0]` коли `len(b) == 0` | Завжди перевіряйте `len(b) >= required` перед викликом `pio.U*` |
| **Неправильний знак у I24BE** | Від'ємні значення стають великими додатними | Переконайтеся, що використовуєте `int8(b[0])` для sign extension |
| **Переповнення при U40BE** | Значення > 2^40 не вміщуються у uint64 | U40BE повертає uint64, що вміщує 40 біт без проблем; перевіряйте вхідні дані |
| **Неправильний endian** | Значення "перевернуті" | Переконайтеся, що використовуєте BE для мережевих протоколів, LE для x86 даних |
| **Невірний розмір зрізу** | `U32BE(b[0:3])` замість `b[0:4]` | Завжди передавайте зріз правильної довжини: `U32BE` → 4 байти |

---

## ⚡ Оптимізації для high-performance парсингу

### 1. Inlining для зменшення накладних витрат:

```go
//go:inline
func U32BE(b []byte) uint32 {
    // Компілятор вставить код функції на місці виклику
    // → уникнення виклику функції → швидше
    i := uint32(b[0])
    i <<= 8; i |= uint32(b[1])
    i <<= 8; i |= uint32(b[2])
    i <<= 8; i |= uint32(b[3])
    return i
}
```

### 2. Пакетне читання для зменшення викликів:

```go
// ReadU32BEVec — читання кількох U32BE значень з одного масиву
func ReadU32BEVec(b []byte, count int) []uint32 {
    if len(b) < count*4 {
        return nil
    }
    
    result := make([]uint32, count)
    for i := 0; i < count; i++ {
        offset := i * 4
        result[i] = U32BE(b[offset : offset+4])
    }
    return result
}
```

### 3. Використання unsafe для прямого доступу (тільки для BE на BE архітектурі):

```go
// ⚠️ Тільки для little-endian машин, якщо дані теж little-endian!
func U32LEUnsafe(b []byte) uint32 {
    if len(b) < 4 {
        return 0
    }
    return *(*uint32)(unsafe.Pointer(&b[0]))
}
```

---

## 📋 Чек-лист використання pio

```go
// ✅ 1. Перевірка довжини перед доступом
if len(b) < 4 {
    return fmt.Errorf("too short for U32BE")
}
value := pio.U32BE(b[0:4])

// ✅ 2. Вибір правильного endian
// Мережеві протоколи: BE
value := pio.U32BE(b)
// x86 файли: LE
value := pio.U32LE(b)

// ✅ 3. Вибір sign/unsigned
// Довжини, ID, timestamps: unsigned
length := pio.U24BE(b)
// Зміщення, delta: signed
offset := pio.I24BE(b)

// ✅ 4. Обробка нестандартних розмірів
// 40-бітний timestamp
pts := pio.U40BE(b[0:5])

// ✅ 5. Конвертація у duration/time
duration := time.Duration(pio.U32BE(b)) * time.Second / 90000

// ✅ 6. Логування для дебагу
if Debug {
    log.Printf("Parsed U32BE: 0x%08X (%d)", value, value)
}
```

---

## 🔗 Корисні посилання

- 💻 [Go encoding/binary Package](https://pkg.go.dev/encoding/binary) — стандартна бібліотека для endian конвертації
- 📄 [Network Byte Order (RFC 1700)](https://datatracker.ietf.org/doc/html/rfc1700) — Big-Endian стандарт
- 📄 [Two's Complement Representation](https://en.wikipedia.org/wiki/Two%27s_complement) — як працюють знакові цілі
- 🧪 [Go unsafe Package](https://pkg.go.dev/unsafe) — для advanced оптимізацій

---

> 💡 **Ключові рекомендації**:
> 1. **Завжди перевіряйте `len(b)`** перед викликом `pio.U*` — уникнення панік.
> 2. **Використовуйте BE для мережевих протоколів** — RTSP, RTP, MPEG-TS вимагають Network Byte Order.
> 3. **Обирайте sign/unsigned свідомо** — `I24BE` для від'ємних зсувів, `U24BE` для довжин.
> 4. **Тестуйте з крайніми значеннями** — `0x000000`, `0xFFFFFF`, `0x800000` для 24-бітних полів.
> 5. **Документуйте очікуваний формат** — коментуйте, чому використовується 24/40 біт замість стандартних розмірів.

Потрібен приклад інтеграції `pio` з вашим `rtspv2.RTPDemuxer` для парсингу 40-бітних timestamp? Готовий допомогти! 🚀