# 📦 Глибокий розбір: `mkvio.pack/unpack` — Variable-Length Integer Encoding для EBML

Цей файл — **допоміжні функції для кодування/декодування цілих чисел** у форматі EBML (Extensible Binary Meta Language), який використовується у WebM/Matroska. Вони реалізують перетворення між байтовими масивами змінної довжини та 64-бітними цілими числами, що є критичним для парсингу ідентифікаторів та розмірів елементів.

---

## 🗺️ Архітектурна схема variable-length encoding

```
┌────────────────────────────────────────┐
│ 📦 mkvio.pack/unpack — EBML Integers  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Призначення:                        │
│  • pack() — байти → uint64 (декодування)│
│  • unpack() — uint64 → байти (кодування)│
│  • Підтримка 1-8 байтних значень       │
│                                         │
│  🔄 Формат EBML integer:               │
│  [1-bit marker][7-bit data] × N        │
│  • Перший байт: маркер довжини + дані │
│  • Наступні байти: тільки дані        │
│  • Big-endian порядок байт            │
│                                         │
│  📡 Використання:                       │
│  • Element ID (1-4 байти)             │
│  • Element Size (1-8 байт)            │
│  • Інші числові поля у специфікації   │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. pack() — декодування variable-length integer

### 🔧 Реалізація:

```go
func pack(n int, b []byte) uint64 {
    var v uint64
    var k uint64 = (uint64(n) - 1) * 8  // початковий зсув для першого байта

    for i := 0; i < n; i++ {
        v |= uint64(b[i]) << k  // додавання байта зі зсувом
        k -= 8                   // зменшення зсуву для наступного байта
    }

    return v
}
```

### 🔍 Як працює:

```
Приклад: pack(3, []byte{0x20, 0x01, 0x02})

1. n=3, k=(3-1)*8=16
2. Ітерація 0 (i=0):
   • b[0]=0x20, зсув k=16
   • v |= 0x20 << 16 = 0x00200000
   • k = 16-8 = 8
3. Ітерація 1 (i=1):
   • b[1]=0x01, зсув k=8
   • v |= 0x01 << 8 = 0x00200100
   • k = 8-8 = 0
4. Ітерація 2 (i=2):
   • b[2]=0x02, зсув k=0
   • v |= 0x02 << 0 = 0x00200102
5. Результат: v = 0x200102 = 2097410

Це big-endian декодування: перший байт — старші біти.
```

### ⚠️ Критичні моменти:

```
❌ Відсутня перевірка вхідних даних:
• Якщо len(b) < n → паніка при доступі до b[i]
• Якщо n > 8 → переповнення при зсуві >> 56

❌ Не обробляє маски маркерних біт:
• У EBML перший байт містить маркер довжини у старших бітах
• pack() припускає, що маска вже застосована до b[0]
• Це має робити викликаючий код (напр. GetElementSize)

✅ Безпечна версія:
    func packSafe(n int, b []byte) (uint64, error) {
        if n < 1 || n > 8 {
            return 0, fmt.Errorf("invalid length: %d", n)
        }
        if len(b) < n {
            return 0, fmt.Errorf("buffer too short: need %d, got %d", n, len(b))
        }
        
        var v uint64
        var k uint64 = (uint64(n) - 1) * 8
        
        for i := 0; i < n; i++ {
            v |= uint64(b[i]) << k
            k -= 8
        }
        
        return v, nil
    }
```

### ✅ Ваш use-case**: декодування EBML ID

```go
// DecodeEBMLID — безпечне декодування variable-length ID
func DecodeEBMLID(b []byte) (uint32, int, error) {
    if len(b) == 0 {
        return 0, 0, fmt.Errorf("empty buffer")
    }
    
    // Визначення довжини за маркерними бітами першого байта
    var length int
    first := b[0]
    
    if first&0x80 != 0 {
        length = 1
    } else if first&0x40 != 0 {
        length = 2
    } else if first&0x20 != 0 {
        length = 3
    } else if first&0x10 != 0 {
        length = 4
    } else {
        return 0, 0, fmt.Errorf("invalid EBML ID marker: 0x%X", first)
    }
    
    if len(b) < length {
        return 0, 0, fmt.Errorf("buffer too short for %d-byte ID", length)
    }
    
    // Застосування маски до першого байта для видалення маркера
    masked := make([]byte, length)
    copy(masked, b[:length])
    
    switch length {
    case 1: masked[0] &= 0x7F
    case 2: masked[0] &= 0x3F
    case 3: masked[0] &= 0x1F
    case 4: masked[0] &= 0x0F
    }
    
    // Декодування через pack
    v, err := packSafe(length, masked)
    if err != nil {
        return 0, 0, err
    }
    
    return uint32(v), length, nil
}

// Використання:
id, length, err := DecodeEBMLID(buffer)
if err != nil { /* handle error */ }
log.Printf("Decoded ID: 0x%X (%d bytes)", id, length)
```

---

## 🔑 2. unpack() — кодування uint64 у variable-length байти

### 🔧 Реалізація:

```go
func unpack(n int, v uint64) []byte {
    var b []byte

    for i := uint(n); i > 0; i-- {
        b = append(b, byte(v>>(8*i))&0xff)
    }

    return b
}
```

### 🔍 Як працює:

```
Приклад: unpack(3, 0x200102)

1. n=3, цикл: i=3,2,1
2. Ітерація i=3:
   • v>>(8*3) = 0x200102 >> 24 = 0x00
   • byte(0x00) & 0xff = 0x00
   • b = [0x00]
3. Ітерація i=2:
   • v>>(8*2) = 0x200102 >> 16 = 0x20
   • byte(0x20) & 0xff = 0x20
   • b = [0x00, 0x20]
4. Ітерація i=1:
   • v>>(8*1) = 0x200102 >> 8 = 0x2001
   • byte(0x2001) & 0xff = 0x01
   • b = [0x00, 0x20, 0x01]

❌ Помилка: результат [0x00, 0x20, 0x01] замість очікуваного [0x20, 0x01, 0x02]!
```

### ⚠️ Критична проблема: неправильна логіка зсуву

```
Поточна реалізація:
    b = append(b, byte(v>>(8*i))&0xff)

Проблема:
• Для i=n: зсув на 8*n біт → завжди 0 для 64-бітного v
• Для i=1: зсув на 8 біт → пропускає молодший байт
• Результат: зміщені байти, неправильний порядок

✅ Виправлення: правильна логіка big-endian кодування
    func unpackFixed(n int, v uint64) []byte {
        b := make([]byte, n)
        for i := 0; i < n; i++ {
            // Big-endian: старший байт перший
            shift := uint((n - 1 - i) * 8)
            b[i] = byte((v >> shift) & 0xff)
        }
        return b
    }

// Приклад: unpackFixed(3, 0x200102)
// i=0: shift=16 → (0x200102>>16)&0xff = 0x20
// i=1: shift=8  → (0x200102>>8)&0xff  = 0x01  
// i=2: shift=0  → (0x200102>>0)&0xff  = 0x02
// Результат: [0x20, 0x01, 0x02] ✓
```

### 🔍 Додаткова проблема: відсутність маркерних біт для EBML

```
У поточній реалізації:
• unpack() повертає "чисті" байти без маркерів довжини
• Для EBML потрібно додати маркер у перший байт:
  • 1 байт: 0x80 | (value & 0x7F)
  • 2 байти: 0x40 | (value >> 8), (value & 0xFF)
  • тощо

✅ Функція для кодування з маркерами:
    func encodeEBMLInteger(v uint64, length int) ([]byte, error) {
        if length < 1 || length > 8 {
            return nil, fmt.Errorf("invalid length: %d", length)
        }
        
        // Перевірка чи значення поміщається у задану довжину
        maxVal := uint64(1<<(7*length)) - 1
        if v > maxVal {
            return nil, fmt.Errorf("value 0x%X too large for %d bytes", v, length)
        }
        
        b := make([]byte, length)
        
        // Big-endian кодування
        for i := 0; i < length; i++ {
            shift := uint((length - 1 - i) * 8)
            b[i] = byte((v >> shift) & 0xff)
        }
        
        // Додавання маркера довжини у перший байт
        marker := byte(0x80 >> (length - 1))
        b[0] |= marker
        
        return b, nil
    }
```

### ✅ Ваш use-case**: кодування EBML size поля

```go
// EncodeEBMLSize — кодування розміру елемента у variable-length формат
func EncodeEBMLSize(size uint64) ([]byte, error) {
    // Визначення мінімальної довжини для заданого значення
    var length int
    switch {
    case size < (1 << 7):
        length = 1
    case size < (1 << 14):
        length = 2
    case size < (1 << 21):
        length = 3
    case size < (1 << 28):
        length = 4
    case size < (1 << 35):
        length = 5
    case size < (1 << 42):
        length = 6
    case size < (1 << 49):
        length = 7
    default:
        length = 8
    }
    
    return encodeEBMLInteger(size, length)
}

// Використання при записі елемента:
sizeBytes, err := EncodeEBMLSize(dataSize)
if err != nil { return err }
writer.Write(sizeBytes)  // запис variable-length size
writer.Write(data)       // запис даних
```

---

## 🔑 3. Інтеграція з парсером/серіалізатором

### 🔧 Приклад: повний цикл кодування/декодування

```go
// RoundTripTest — перевірка коректності pack/unpack
func RoundTripTest() error {
    testCases := []struct {
        value uint64
        length int
    }{
        {0x7F, 1},      // максимальне 1-байтне значення
        {0x3FFF, 2},    // максимальне 2-байтне
        {0x1FFFFF, 3},  // максимальне 3-байтне
        {0x200102, 3},  // довільне значення
    }
    
    for _, tc := range testCases {
        // Кодирування
        encoded := unpackFixed(tc.length, tc.value)
        
        // Застосування маски (симуляція EBML маркера)
        encoded[0] &= getMask(tc.length)
        
        // Декодування
        decoded, err := packSafe(tc.length, encoded)
        if err != nil {
            return fmt.Errorf("pack failed: %w", err)
        }
        
        if decoded != tc.value {
            return fmt.Errorf("round-trip failed: 0x%X → 0x%X", tc.value, decoded)
        }
    }
    
    return nil
}

func getMask(length int) byte {
    masks := []byte{0x7F, 0x3F, 0x1F, 0x0F, 0x07, 0x03, 0x01, 0x00}
    if length < 1 || length > 8 {
        return 0x7F
    }
    return masks[length-1]
}
```

### 🔧 Приклад: серіалізація елемента з variable-length полями

```go
// WriteElement — запис елемента у EBML формат
func WriteElement(w io.Writer, id uint32, data []byte) error {
    // 1. Кодирування ID (1-4 байти)
    idBytes, err := encodeEBMLInteger(uint64(id), getIDLength(id))
    if err != nil { return err }
    if _, err := w.Write(idBytes); err != nil { return err }
    
    // 2. Кодирування розміру даних (1-8 байт)
    sizeBytes, err := EncodeEBMLSize(uint64(len(data)))
    if err != nil { return err }
    if _, err := w.Write(sizeBytes); err != nil { return err }
    
    // 3. Запис даних
    if _, err := w.Write(data); err != nil { return err }
    
    return nil
}

func getIDLength(id uint32) int {
    if id <= 0x7F { return 1 }
    if id <= 0x3FFF { return 2 }
    if id <= 0x1FFFFF { return 3 }
    return 4
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при доступі до b[i]** | len(b) < n у pack() | Додайте перевірку `if len(b) < n` перед циклом |
| **Неправильний порядок байт** | unpack() повертає зміщені значення | Використовуйте `shift := (n-1-i)*8` замість `8*i` |
| **Відсутність маркерних біт** | Декодовані ID/size не співпадають з очікуваними | Застосовуйте маску до першого байта перед pack() |
| **Переповнення при зсуві** | n > 8 у pack() → зсув >> 56 | Обмежте n діапазоном 1-8, перевіряйте вхідні дані |
| **Некоректне кодування великих значень** | size > 2^56 не кодується у 8 байт | Використовуйте uint64 для всіх проміжних обчислень |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування результатів для частих значень:

```go
// PrecomputedSizes — кеш для поширених розмірів
var sizeCache = sync.Map{}  // map[uint64][]byte

func getCachedSizeBytes(size uint64) []byte {
    if cached, ok := sizeCache.Load(size); ok {
        return cached.([]byte)
    }
    
    bytes, _ := EncodeEBMLSize(size)  // ігноруємо помилку для кешу
    sizeCache.Store(size, bytes)
    return bytes
}
```

### 2. Використання sync.Pool для буферів:

```go
var byteBufferPool = sync.Pool{
    New: func() interface{} {
        return new(bytes.Buffer)
    },
}

func WriteElementPooled(w io.Writer, id uint32, data []byte) error {
    buf := byteBufferPool.Get().(*bytes.Buffer)
    buf.Reset()
    defer byteBufferPool.Put(buf)
    
    // Кодирування у буфер
    idBytes, _ := encodeEBMLInteger(uint64(id), getIDLength(id))
    sizeBytes, _ := EncodeEBMLSize(uint64(len(data)))
    
    buf.Write(idBytes)
    buf.Write(sizeBytes)
    buf.Write(data)
    
    // Запис у вихідний потік
    _, err := w.Write(buf.Bytes())
    return err
}
```

### 3. Моніторинг продуктивності кодування:

```go
type EncodingMetrics struct {
    EncodeLatency prometheus.HistogramVec
    DecodeLatency prometheus.HistogramVec
    ValueSizes    prometheus.HistogramVec
}

func (m *EncodingMetrics) RecordEncode(value uint64, length int, duration time.Duration) {
    m.EncodeLatency.WithLabelValues(fmt.Sprintf("%d-byte", length)).Observe(duration.Seconds())
    m.ValueSizes.Observe(float64(value))
}
```

---

## 📋 Чек-лист безпечного використання pack/unpack

```go
// ✅ 1. Перевірка вхідних даних перед pack()
if n < 1 || n > 8 {
    return fmt.Errorf("invalid length: %d", n)
}
if len(b) < n {
    return fmt.Errorf("buffer too short: need %d, got %d", n, len(b))
}

// ✅ 2. Застосування маски до першого байта для EBML
masked := make([]byte, n)
copy(masked, b[:n])
masked[0] &= getMask(n)  // видалення маркерних біт
v, err := packSafe(n, masked)

// ✅ 3. Використання правильної логіки зсуву в unpack()
func unpackFixed(n int, v uint64) []byte {
    b := make([]byte, n)
    for i := 0; i < n; i++ {
        shift := uint((n - 1 - i) * 8)  // big-endian
        b[i] = byte((v >> shift) & 0xff)
    }
    return b
}

// ✅ 4. Додавання маркерних біт при кодуванні для EBML
b[0] |= (0x80 >> (n - 1))  // маркер довжини

// ✅ 5. Обмеження максимального значення для заданої довжини
maxVal := uint64(1<<(7*n)) - 1
if value > maxVal {
    return fmt.Errorf("value too large for %d bytes", n)
}

// ✅ 6. Логування для дебагу складних випадків
log.Printf("Encoded 0x%X → %v (length=%d)", value, encoded, length)

// ✅ 7. Метрики для моніторингу
metrics.RecordEncode(value, length, time.Since(start))
```

---

## 🔗 Корисні посилання

- 📄 [EBML Specification (RFC 8794)](https://datatracker.ietf.org/doc/html/rfc8794#section-4.1) — розділ про variable-length integers
- 💻 [Go encoding/binary Package](https://pkg.go.dev/encoding/binary) — стандартні функції для бінарного кодування
- 🧪 [Big-endian vs Little-endian](https://en.wikipedia.org/wiki/Endianness) — теорія порядку байт
- 📦 [sync.Pool Best Practices](https://go.dev/blog/pool) — ефективне управління пам'яттю
- 📊 [Prometheus Histograms](https://prometheus.io/docs/practices/histograms/) — моніторинг латентності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте `len(b) >= n` перед pack()** — уникнення панік при некоректних вхідних даних.
> 2. **Використовуйте правильну логіку зсуву в unpack()** — `(n-1-i)*8` замість `8*i` для big-endian.
> 3. **Застосовуйте маски маркерних біт для EBML** — інакше декодовані ID/size будуть некоректними.
> 4. **Обмежуйте максимальне значення для заданої довжини** — запобігання переповненню при кодуванні.
> 5. **Кешуйте результати для частих значень** — прискорення серіалізації розмірів елементів.

Потрібен приклад повної реалізації EBML серіалізатора з підтримкою всіх типів даних, або інтеграція цих функцій у ваш парсер WebM для потокової обробки медіа? Готовий допомогти! 🚀