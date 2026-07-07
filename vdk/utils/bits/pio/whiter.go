# 📦 Глибокий розбір: `pio.Put*` — Бітова серіалізація цілих чисел у Big-Endian/Little-Endian

Цей файл — **набір низькорівневих функцій для запису** цілочисельних значень різних розмірів (8-64 біт) у масиви байт у форматах Big-Endian (BE) або Little-Endian (LE). Це "зворотна" сторона функцій `U*/I*` — використовується для серіалізації заголовків мережевих протоколів (RTSP, RTP, MPEG-TS, FLV тощо).

---

## 🗺️ Архітектурна схема pio.Put* функцій

```
┌────────────────────────────────────────┐
│ 📦 pio.Put* — Bitwise Serialization   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові функції:                    │
│  • PutU*/PutI* — unsigned/signed запис │
│  • BE/LE — Big-Endian/Little-Endian    │
│  • Розміри: 8, 16, 24, 32, 40, 48, 64 біт│
│                                         │
│  🔄 Потік даних:                        │
│  uint*/int* → бітові зсуви → []byte    │
│                                         │
│  📡 Використання:                       │
│  • Генерація заголовків мережевих протоколів│
│  • Створення медіа-контейнерів         │
│  • Серіалізація бінарних структур      │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Основи: як працює запис у Big-Endian

### 🔧 Приклад: `PutU32BE`

```go
func PutU32BE(b []byte, v uint32) {
    b[0] = byte(v>>24)  // старші 8 біт → перший байт
    b[1] = byte(v>>16)  // наступні 8 біт
    b[2] = byte(v>>8)   // наступні 8 біт
    b[3] = byte(v)      // молодші 8 біт → останній байт
}
```

### 🔍 Візуалізація для `v = 0x12345678`:

```
Вхід: v = 0x12345678 (305419896 у десятковій)

Крок 1: v>>24 = 0x12 → b[0] = 0x12
Крок 2: v>>16 = 0x1234 → byte(0x1234) = 0x34 → b[1] = 0x34
Крок 3: v>>8  = 0x123456 → byte(0x123456) = 0x56 → b[2] = 0x56
Крок 4: v     = 0x12345678 → byte(0x12345678) = 0x78 → b[3] = 0x78

Результат: b = [0x12, 0x34, 0x56, 0x78] ← Network Byte Order
```

### 🔧 Little-Endian: `PutU32LE` (зворотний порядок)

```go
func PutU32LE(b []byte, v uint32) {
    b[3] = byte(v>>24)  // старші 8 біт → останній байт
    b[2] = byte(v>>16)
    b[1] = byte(v>>8)
    b[0] = byte(v)      // молодші 8 біт → перший байт
}
```

### 🔍 Для `v = 0x12345678`:
```
Результат: b = [0x78, 0x56, 0x34, 0x12] ← x86/x64 порядок
```

---

## 🔑 2. Особливі випадки: 24/40/48-бітні значення

### 🔍 Чому нестандартні розміри?

```
Деякі протоколи вимагають специфічних розмірів полів:
• 24 біти: MPEG-TS section_length, RTP padding length
• 40 біт: RTP timestamp extensions, PTS/DTS у деяких форматах
• 48 біт: PCR (Program Clock Reference) у MPEG-TS

Це економить місце у заголовках, але вимагає ручної обробки.
```

### 🔧 `PutU24BE` — запис 24-бітного беззнакового:

```go
func PutU24BE(b []byte, v uint32) {
    b[0] = byte(v>>16)  // старші 8 біт з 24
    b[1] = byte(v>>8)   // середні 8 біт
    b[2] = byte(v)      // молодші 8 біт
}
```

### 🔧 `PutI24BE` — запис 24-бітного знакового:

```go
func PutI24BE(b []byte, v int32) {
    // ⚠️ Важливо: sign extension працює автоматично при зсуві
    b[0] = byte(v>>16)  // старший байт зі знаком
    b[1] = byte(v>>8)
    b[2] = byte(v)
}
```

### 🔍 Приклад sign handling:
```
Вхід: v = -1 (int32) = 0xFFFFFFFF

PutI24BE:
  v>>16 = 0xFFFFFFFF >> 16 = 0xFFFF → byte(0xFFFF) = 0xFF → b[0] = 0xFF
  v>>8  = 0xFFFFFFFF >> 8  = 0xFFFFFF → byte(0xFFFFFF) = 0xFF → b[1] = 0xFF
  v     = 0xFFFFFFFF → byte(0xFFFFFFFF) = 0xFF → b[2] = 0xFF

Результат: [0xFF, 0xFF, 0xFF] ← коректне представлення -1 у 24 бітах (two's complement)
```

### 🔧 `PutU48BE` — для PCR у MPEG-TS:

```go
func PutU48BE(b []byte, v uint64) {
    b[0] = byte(v>>40)  // біти 47-40
    b[1] = byte(v>>32)  // біти 39-32
    b[2] = byte(v>>24)  // біти 31-24
    b[3] = byte(v>>16)  // біти 23-16
    b[4] = byte(v>>8)   // біти 15-8
    b[5] = byte(v)      // біти 7-0
}
```

### ✅ Ваш use-case: запис PCR у MPEG-TS адаптаційне поле

```go
// WritePCR — запис Program Clock Reference (48 біт)
func WritePCR(buf []byte, pcr uint64) error {
    if len(buf) < 6 {
        return fmt.Errorf("buffer too small for PCR")
    }
    pio.PutU48BE(buf, pcr)
    return nil
}

// Приклад: PCR = 27MHz timestamp
timestamp := uint64(time.Now().UnixNano()) * 27 / 1e9  // конвертація у 27MHz ticks
buf := make([]byte, 6)
WritePCR(buf, timestamp)
// buf тепер містить PCR у Big-Endian форматі
```

---

## 🔑 3. Знакові vs беззнакові: однакова реалізація?

### 🔍 Чому `PutI32BE` та `PutU32BE` виглядають однаково?

```go
func PutI32BE(b []byte, v int32) {
    b[0] = byte(v>>24)
    b[1] = byte(v>>16)
    b[2] = byte(v>>8)
    b[3] = byte(v)
}

func PutU32BE(b []byte, v uint32) {
    b[0] = byte(v>>24)
    b[1] = byte(v>>16)
    b[2] = byte(v>>8)
    b[3] = byte(v)
}
```

**Відповідь**: Бітове представлення однакове для two's complement! Різниця лише у **інтерпретації** при читанні.

### 🔍 Приклад:
```
v = -1 (int32) = 0xFFFFFFFF
v = 4294967295 (uint32) = 0xFFFFFFFF

Обидва дадуть: [0xFF, 0xFF, 0xFF, 0xFF]

Різниця проявляється лише при читанні:
• I32BE([0xFF,0xFF,0xFF,0xFF]) → -1
• U32BE([0xFF,0xFF,0xFF,0xFF]) → 4294967295
```

### ✅ Ваш use-case: запис від'ємного Composition Time Offset

```go
// WriteCompositionTimeOffset — запис зсуву часу (може бути від'ємним)
func WriteCompositionTimeOffset(buf []byte, offset time.Duration, clockRate int64) error {
    if len(buf) < 3 {
        return fmt.Errorf("buffer too small for 24-bit offset")
    }

    // Конвертація duration → ticks @ clockRate
    ticks := int32(offset * time.Duration(clockRate) / time.Second)

    // Запис як знакове 24-бітне значення
    pio.PutI24BE(buf, ticks)
    return nil
}

// Використання для від'ємного зсуву (B-frame):
offset := -33 * time.Millisecond  // кадр на 33мс "у минулому"
buf := make([]byte, 3)
WriteCompositionTimeOffset(buf, offset, 90000)
// buf = [0xFF, 0xFD, 0xE8] ← two's complement представлення
```

---

## ⚠️ Критичні проблеми у вихідному коді

### ❌ 1. Відсутність перевірки довжини буфера

```go
func PutU32BE(b []byte, v uint32) {
    b[0] = byte(v>>24)  // ← Паніка якщо len(b) < 4!
    // ...
}
```

**Наслідки**: `panic: runtime error: index out of range` у production.

**✅ Виправлення (безпечна версія)**:
```go
func PutU32BE(b []byte, v uint32) error {
    if len(b) < 4 {
        return fmt.Errorf("buffer too small: need 4 bytes, got %d", len(b))
    }
    b[0] = byte(v>>24)
    b[1] = byte(v>>16)
    b[2] = byte(v>>8)
    b[3] = byte(v)
    return nil
}
```

> 💡 **Альтернатива**: Залишити без перевірки для швидкодії, але **документувати вимогу** та використовувати `//go:inline` для оптимізації.

---

### ❌ 2. Неповний набір функцій

```
Реалізовано:          Відсутньо:
• PutU8               • PutI8
• PutU16BE, PutI16BE  • PutU16LE, PutI16LE
• PutU24BE, PutI24BE  • PutU24LE, PutI24LE
• PutU32BE, PutI32BE  • PutI32LE
• PutU32LE            • PutU40LE, PutI40BE, PutI40LE
• PutU40BE            • PutI48BE, PutU48LE, PutI48LE
• PutU48BE            • PutI64LE
• PutU64BE, PutI64BE
```

**Наслідки**: Неможливо серіалізувати деякі протоколи, що вимагають LE або знакові нестандартні розміри.

**✅ Виправлення**: Додати відсутні функції за потребою, або створити універсальну обгортку:

```go
// PutIntBE — універсальна функція для знакових значень будь-якого розміру
func PutIntBE(b []byte, v int64, bits int) error {
    if len(b)*8 < bits {
        return fmt.Errorf("buffer too small for %d bits", bits)
    }

    for i := 0; i < bits/8; i++ {
        shift := uint(bits - 8 - i*8)
        b[i] = byte(v >> shift)
    }
    return nil
}
```

---

### ❌ 3. Неявна поведінка при переповненні

```go
// Запис uint64 у 24-бітне поле:
v := uint64(0x123456789ABC)  // > 24 біт
pio.PutU24BE(buf, uint32(v))  // ⚠️ Втрачаються старші біти!
```

**Наслідки**: Тихе обрізання значень → некоректні заголовки → помилки парсингу на стороні клієнта.

**✅ Виправлення**: Додати валідацію діапазону:

```go
// PutU24BEChecked — безпечна версія з перевіркою діапазону
func PutU24BEChecked(b []byte, v uint32) error {
    if v > 0xFFFFFF {
        return fmt.Errorf("value 0x%X exceeds 24-bit range", v)
    }
    if len(b) < 3 {
        return fmt.Errorf("buffer too small")
    }
    PutU24BE(b, v)
    return nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### Сценарій 1: Генерація RTP заголовку

```go
// WriteRTPHeader — серіалізація 12-байтового RTP заголовку
func WriteRTPHeader(buf []byte, header *RTPHeader) error {
    if len(buf) < 12 {
        return fmt.Errorf("buffer too small for RTP header")
    }

    // Байт 0: V(2) P(1) X(1) CC(4)
    buf[0] = (header.Version << 6) |
             (boolToByte(header.Padding) << 5) |
             (boolToByte(header.Extension) << 4) |
             byte(header.CSRCCount & 0x0F)

    // Байт 1: M(1) PT(7)
    buf[1] = (boolToByte(header.Marker) << 7) | byte(header.PayloadType & 0x7F)

    // Байти 2-3: Sequence Number (16 біт BE)
    pio.PutU16BE(buf[2:4], header.SequenceNumber)

    // Байти 4-7: Timestamp (32 біт BE)
    pio.PutU32BE(buf[4:8], header.Timestamp)

    // Байти 8-11: SSRC (32 біт BE)
    pio.PutU32BE(buf[8:12], header.SSRC)

    return nil
}

func boolToByte(b bool) byte {
    if b { return 1 }
    return 0
}
```

### Сценарій 2: Створення MPEG-TS пакету з PCR

```go
// WriteTSAdaptationField — запис адаптаційного поля з PCR
func WriteTSAdaptationField(buf []byte, pcr uint64, discontinuity bool) (int, error) {
    if len(buf) < 7 {  // 1 (length) + 1 (flags) + 6 (PCR)
        return 0, fmt.Errorf("buffer too small")
    }

    offset := 0

    // Довжина адаптаційного поля (без цього байта)
    buf[offset] = 6  // 1 байт flags + 6 байт PCR
    offset++

    // Прапорці: PCR_flag = 1, інші = 0
    flags := byte(0x10)  // біт 4 = PCR_flag
    if discontinuity {
        flags |= 0x80  // біт 7 = discontinuity_indicator
    }
    buf[offset] = flags
    offset++

    // PCR: 48 біт = 33 біти base + 6 біт reserved + 9 біт extension
    base := pcr / 300
    ext := pcr % 300
    pcrValue := (base << 15) | (0x3F << 9) | ext

    pio.PutU48BE(buf[offset:], pcrValue)
    offset += 6

    return offset, nil
}
```

### Сценарій 3: Серіалізація FLV тега

```go
// WriteFLVTagHeader — запис 11-байтового заголовку тега
func WriteFLVTagHeader(buf []byte, tagType uint8, dataLen uint32, timestamp uint32) error {
    if len(buf) < 11 {
        return fmt.Errorf("buffer too small for FLV tag header")
    }

    // Байт 0: Tag Type (8=audio, 9=video, 18=script)
    buf[0] = tagType

    // Байти 1-3: Data Size (24 біт BE)
    pio.PutU24BE(buf[1:4], dataLen)

    // Байти 4-6: Timestamp (24 біт BE, нижні біти)
    pio.PutU24BE(buf[4:7], timestamp & 0xFFFFFF)

    // Байт 7: Timestamp Extended (верхні 8 біт)
    buf[7] = byte(timestamp >> 24)

    // Байти 8-10: Stream ID (завжди 0 для FLV)
    pio.PutU24BE(buf[8:11], 0)

    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка `index out of range`** | `PutU32BE(buf, v)` з `len(buf) < 4` | Додайте перевірку `len(b) >= required` або використовуйте `PutU32BEChecked` |
| **Неправильний endian** | Значення "перевернуті" на іншій архітектурі | Пам'ятайте: мережеві протоколи = BE, x86 файли = LE |
| **Переповнення при записі** | Велике значення обрізається у 24-бітне поле | Валідуйте діапазон: `if v > 0xFFFFFF { return error }` |
| **Sign extension помилки** | Від'ємні значення записуються неправильно | Використовуйте `PutI*` для знакових полів, навіть якщо реалізація однакова |
| **Неповний буфер** | `PutU48BE` пише у `len(b) < 6` | Завжди перевіряйте розмір перед викликом |

---

## ⚡ Оптимізації для high-performance серіалізації

### 1. Inlining для зменшення накладних витрат:

```go
//go:inline
func PutU32BE(b []byte, v uint32) {
    b[0] = byte(v>>24)
    b[1] = byte(v>>16)
    b[2] = byte(v>>8)
    b[3] = byte(v)
}
```

### 2. Пакетний запис для зменшення викликів:

```go
// PutU32BEVec — запис кількох U32BE значень у один буфер
func PutU32BEVec(b []byte, values []uint32) error {
    if len(b) < len(values)*4 {
        return fmt.Errorf("buffer too small")
    }
    for i, v := range values {
        offset := i * 4
        b[offset] = byte(v>>24)
        b[offset+1] = byte(v>>16)
        b[offset+2] = byte(v>>8)
        b[offset+3] = byte(v)
    }
    return nil
}
```

### 3. Використання `unsafe` для прямого запису (тільки для BE на BE архітектурі):

```go
// ⚠️ Тільки для big-endian машин або якщо дані теж BE!
func PutU32BEUnsafe(b []byte, v uint32) {
    if len(b) < 4 {
        panic("buffer too small")
    }
    *(*uint32)(unsafe.Pointer(&b[0])) = binary.BigEndian.Uint32(b)  // або ручний запис
}
```

> 💡 **Порада**: У більшості випадків ручні зсуви швидші за `binary.BigEndian.Put*` через уникнення функціональних викликів.

---

## 📋 Чек-лист безпечного використання

```go
// ✅ 1. Перевірка довжини буфера
if len(buf) < 4 {
    return fmt.Errorf("need 4 bytes for U32BE")
}
pio.PutU32BE(buf, value)

// ✅ 2. Вибір правильного endian
// Мережеві протоколи: BE
pio.PutU32BE(buf, value)
// x86 файли: LE
pio.PutU32LE(buf, value)

// ✅ 3. Вибір sign/unsigned
// Довжини, ID, timestamps: unsigned
pio.PutU24BE(buf, uint32(length))
// Зміщення, delta: signed
pio.PutI24BE(buf, int32(offset))

// ✅ 4. Валідація діапазону для нестандартних розмірів
if value > 0xFFFFFF {
    return fmt.Errorf("value exceeds 24-bit range")
}
pio.PutU24BE(buf, value)

// ✅ 5. Логування для дебагу
if Debug {
    log.Printf("Serialized U32BE: 0x%08X → %v", value, buf[:4])
}

// ✅ 6. Використання пулів буферів
buf := getHeaderBuffer()  // з sync.Pool
pio.PutU32BE(buf, timestamp)
// ... використання ...
putHeaderBuffer(buf)
```

---

## 🔗 Корисні посилання

- 💻 [Go encoding/binary Package](https://pkg.go.dev/encoding/binary) — стандартна бібліотека
- 📄 [Network Byte Order (RFC 1700)](https://datatracker.ietf.org/doc/html/rfc1700) — Big-Endian стандарт
- 📄 [Twos Complement Representation](https://en.wikipedia.org/wiki/Two%27s_complement) — знакові цілі
- 🧪 [Go unsafe Package](https://pkg.go.dev/unsafe) — для advanced оптимізацій

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте `len(b)`** перед записом — уникнення панік у production.
> 2. **Використовуйте BE для мережевих протоколів** — RTSP, RTP, MPEG-TS вимагають Network Byte Order.
> 3. **Валідуйте діапазон для 24/40/48-бітних полів** — тихе обрізання призводить до складних для дебагу помилок.
> 4. **Документуйте очікуваний формат** — коментуйте, чому використовується нестандартний розмір.
> 5. **Кешуйте буфери через `sync.Pool`** — зменшення аллокацій критично для real-time медіа.

Потрібен приклад інтеграції `Put*` функцій з вашим `ts.Muxer` для генерації TS пакетів з коректними заголовками? Готовий допомогти! 🚀