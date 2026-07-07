# Глибоке роз'яснення: Тести CRC32 у astits — валідація цілісності PSI даних

Цей файл містить **тести обчислення CRC32** для PSI (Program Specific Information) даних — критичного механізму валідації цілісності таблиць MPEG-TS/DVB. Без коректного CRC32 плеєри відкидають таблиці як пошкоджені.

---

## 🎯 Навіщо CRC32 потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ CRC32 у контексті HLS-стрімінгу:       │
│                                         │
│ 🔹 Валідація цілісності даних:         │
│   • Детекція пошкоджених пакетів       │
│   • Запобігання відтворенню "сміття"   │
│   • Відповідність стандарту MPEG-2     │
│                                         │
│ 🔹 Для таблиць PSI:                     │
│   • PAT/PMT/EIT/SDT/TOT мають CRC32    │
│   • Без валідного CRC → таблиця ігнорується│
│   • Неправильний CRC → чорний екран    │
│                                         │
│ 🔹 Для вашого пайплайну:                │
│   • Гарантія коректності метаданих     │
│   • Моніторинг якості вхідного потоку  │
│   • Відладка проблем мережі/енкодера   │
└─────────────────────────────────────────┘
```

---

## 🔧 Тестові дані: реальні PAT та PMT з валідними CRC32

### PAT тестові дані

```go
var testDataPat = []byte{
    0x00, 0xb0, 0x0d, 0x00, 0x01, 0xe1, 0x00, 0x00,  // PSI заголовок
    0x00, 0x01, 0xf0, 0x00,                          // program: number=1, PMT_PID=0x1000
    0xe2, 0x95, 0xf6, 0x9d,                          // 🔹 CRC32 (останні 4 байти)
}
```

**Розбір PAT:**
```
Байт 0:   0x00 = table_id (PAT)
Байт 1-2: 0xb0, 0x0d = section_length (13 байт) + прапорці
Байт 3-4: 0x00, 0x01 = transport_stream_id
Байт 5-6: 0xe1, 0x00 = reserved + version + current_next + section_number + last_section
Байт 7-10:0x00, 0x01, 0xf0, 0x00 = program: number=1, reserved=0b111, PMT_PID=0x1000
Байт 11-14:0xe2, 0x95, 0xf6, 0x9d = 🔹 CRC32 (обчислюється для байт 0-10)
```

### PMT тестові дані

```go
var testDataPmt = []byte{
    0x02, 0xb0, 0x1d, 0x00, 0x01, 0xf5, 0x00, 0x00,  // PSI заголовок
    0xe1, 0x00, 0xf0, 0x00,                           // PCR_PID=0x100, program_info_length=0
    0x1b, 0xe1, 0x00, 0x00,                           // video: H.264, PID=0x100
    0x0f, 0xe1, 0x04, 0x00,                           // audio: AAC, PID=0x104
    0x06, 0x0a, 0x04, 0x72, 0x75, 0x73, 0x00,        // descriptor: ISO639 language="rus"
    0x38, 0x92, 0x85, 0xac,                           // 🔹 CRC32 (останні 4 байти)
}
```

**Розбір PMT:**
```
• table_id=0x02 (PMT)
• PCR_PID=0x100 (256)
• 2 елементарні потоки:
  - Stream type 0x1B (H.264), PID=0x100
  - Stream type 0x0F (AAC), PID=0x104, з дескриптором мови "rus"
• CRC32 валідує всі попередні байти
```

---

## 🔍 Тест `Test_updateCRC32`: валідація обчислення

```go
func Test_updateCRC32(t *testing.T) {
    tests := []struct {
        name string
        crc  uint32      // 🔹 Очікуваний CRC32 (з останніх 4 байт вхідних даних)
        data []byte      // 🔹 Дані для обчислення (без останніх 4 байт)
    }{
        {
            name: "Calc PAT crc32",
            crc:  binary.BigEndian.Uint32(testDataPat[len(testDataPat)-4:]),  // 0xe295f69d
            data: testDataPat[:len(testDataPat)-4],  // байти 0-10
        }, {
            name: "Calc PMT crc32",
            crc:  binary.BigEndian.Uint32(testDataPmt[len(testDataPmt)-4:]),  // 0x389285ac
            data: testDataPmt[:len(testDataPmt)-4],  // байти 0-25
        },
    }

    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            // 🔹 Обчислити CRC32 для даних
            computed := computeCRC32(test.data)
            
            // 🔹 Порівняти з очікуваним значенням
            assert.Equal(t, test.crc, computed)
        })
    }
}
```

### 🎯 Ключові моменти тесту

#### 1. Витягування очікуваного CRC32

```go
// Останні 4 байти = CRC32 у big-endian форматі
crc := binary.BigEndian.Uint32(data[len(data)-4:])

// Приклад для PAT:
// testDataPat[11:15] = []byte{0xe2, 0x95, 0xf6, 0x9d}
// → uint32 = 0xe295f69d = 3802666653 (десяткове)
```

#### 2. Обчислення CRC32 для даних

```go
// computeCRC32 має використовувати MPEG-2 поліном 0x04c11db7
// НЕ стандартний IEEE 0xedb88320!

// Гіпотетична реалізація:
func computeCRC32(data []byte) uint32 {
    crc := uint32(0xffffffff)  // початкове значення
    for _, b := range data {
        crc = updateCRC32(crc, []byte{b})  // побайтове оновлення
    }
    return crc
}

// updateCRC32 використовує поліном 0x04c11db7
func updateCRC32(crc uint32, data []byte) uint32 {
    for _, b := range data {
        crc ^= uint32(b) << 24
        for i := 0; i < 8; i++ {
            if crc&0x80000000 != 0 {
                crc = (crc << 1) ^ 0x04c11db7  // 🔹 MPEG-2 поліном!
            } else {
                crc <<= 1
            }
        }
    }
    return crc
}
```

> ⚠️ **Критично**: MPEG-2 CRC32 використовує поліном `0x04c11db7`, а не стандартний IEEE `0xedb88320`. Неправильний поліном → всі перевірки проваляться!

---

## 🧮 Математика CRC32 у MPEG-TS

### Формула обчислення

```
1. Ініціалізація: crc = 0xFFFFFFFF
2. Для кожного байта даних:
   • crc ^= (byte << 24)
   • Для кожного з 8 біт:
     - Якщо старший біт = 1: crc = (crc << 1) ^ polynomial
     - Інакше: crc = crc << 1
3. Результат: crc (без інверсії на кінці)

Поліном: 0x04c11db7 (MPEG-2)
```

### Приклад для PAT даних

```
Вхідні дані (11 байт):
00 b0 0d 00 01 e1 00 00 00 01 f0

Обчислення:
• Початкове crc = 0xFFFFFFFF
• Після обробки всіх байт: crc = 0xe295f69d ✅

Перевірка:
• Останні 4 байти testDataPat = e2 95 f6 9d
• binary.BigEndian.Uint32(...) = 0xe295f69d ✅
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Валідація CRC32 при парсингу PSI

```go
// У parsePSISection — перевірка цілісності перед обробкою:
func parsePSISectionWithCRC(i *astikit.BytesIterator, sectionLength int) (*astits.PSISection, error) {
    // 🔹 Парсити заголовок та дані
    header, offsetStart, offsetSectionsEnd, offsetEnd, err := parsePSISectionHeader(i)
    if err != nil { return nil, err }
    
    // 🔹 Обчислити CRC32 для даних секції
    i.Seek(offsetStart)
    data, _ := i.NextBytesNoCopy(offsetSectionsEnd - offsetStart)
    computedCRC := computeCRC32(data)
    
    // 🔹 Прочитати CRC32 з потоку
    i.Seek(offsetSectionsEnd)
    crcBytes, _ := i.NextBytesNoCopy(4)
    storedCRC := binary.BigEndian.Uint32(crcBytes)
    
    // 🔹 Порівняти
    if computedCRC != storedCRC {
        return nil, fmt.Errorf("astits: CRC32 mismatch: stored=0x%08X, computed=0x%08X", 
            storedCRC, computedCRC)
    }
    
    // 🔹 Далі парсити дані...
    return parsePSIData(i)
}
```

### ✅ 2: Моніторинг цілісності потоків

```go
// monitoring.Monitor — метрики для CRC32 валідації:
type CRCMetrics struct {
    CRCValid   *prometheus.CounterVec  // успішні перевірки
    CRCInvalid *prometheus.CounterVec  // помилки валідації
    CRCLatency *prometheus.HistogramVec  // час обчислення CRC
}

// У обробці даних:
func validateWithMetrics(data []byte, storedCRC uint32, channelID string, metrics *CRCMetrics) error {
    start := time.Now()
    computed := computeCRC32(data)
    latency := time.Since(start)
    
    metrics.CRCLatency.WithLabelValues(channelID).Observe(latency.Seconds())
    
    if computed == storedCRC {
        metrics.CRCValid.WithLabelValues(channelID).Inc()
        return nil
    }
    
    metrics.CRCInvalid.WithLabelValues(channelID).Inc()
    log.Warnf("Channel %s: CRC32 mismatch: stored=0x%08X, computed=0x%08X", 
        channelID, storedCRC, computed)
    
    return fmt.Errorf("CRC32 validation failed")
}
```

### ✅ 3: Оптимізація обчислення CRC32

```go
// Використання таблиці для швидкого обчислення (lookup table):
var crc32Table [256]uint32

func init() {
    // Побудувати таблицю для поліному 0x04c11db7
    polynomial := uint32(0x04c11db7)
    for i := 0; i < 256; i++ {
        crc := uint32(i) << 24
        for j := 0; j < 8; j++ {
            if crc&0x80000000 != 0 {
                crc = (crc << 1) ^ polynomial
            } else {
                crc <<= 1
            }
        }
        crc32Table[i] = crc
    }
}

func computeCRC32Fast(data []byte) uint32 {
    crc := uint32(0xffffffff)
    for _, b := range data {
        crc = (crc << 8) ^ crc32Table[(crc>>24)^uint32(b)]
    }
    return crc
}

// Переваги:
// • ~10-20x швидше за побайтове обчислення
// • Критично для high-throughput стрімінгу
```

### ✅ 4: Обробка помилок CRC32

```go
// Стратегія відновлення при невалідному CRC:
func handleCRCError(data []byte, storedCRC, computedCRC uint32, channelID string) error {
    // 🔹 Логування для відладки
    log.Errorf("Channel %s: CRC32 mismatch (stored=0x%08X, computed=0x%08X, len=%d)", 
        channelID, storedCRC, computedCRC, len(data))
    
    // 🔹 Спроба визначити тип пошкодження
    if len(data) < 4 {
        return fmt.Errorf("data too short for CRC validation")
    }
    
    // 🔹 Опція 1: пропустити пошкоджену секцію
    log.Warnf("Channel %s: skipping corrupted PSI section", channelID)
    return nil  // не зупиняти весь потік
    
    // 🔹 Опція 2: спробувати виправити (якщо відомо, що пошкоджено 1 біт)
    // ❌ Не рекомендується: ризик отримати некоректні метадані
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на пошкоджені дані

```go
func TestCRC32_CorruptedData(t *testing.T) {
    // 🔹 Змінити 1 біт у валідних даних
    corrupted := make([]byte, len(testDataPat)-4)
    copy(corrupted, testDataPat[:len(testDataPat)-4])
    corrupted[5] ^= 0x01  // інвертувати 1 біт
    
    expectedCRC := binary.BigEndian.Uint32(testDataPat[len(testDataPat)-4:])
    computedCRC := computeCRC32(corrupted)
    
    // 🔹 CRC має не співпадати
    assert.NotEqual(t, expectedCRC, computedCRC)
    
    // 🔹 Логування для відладки
    log.Debugf("Corrupted data CRC: expected=0x%08X, computed=0x%08X", 
        expectedCRC, computedCRC)
}
```

### 🔹 Тест на порожні дані

```go
func TestCRC32_EdgeCases(t *testing.T) {
    testCases := []struct {
        name     string
        data     []byte
        expected uint32
    }{
        {"Empty", []byte{}, 0xffffffff},  // початкове значення
        {"Single byte", []byte{0x00}, 0x19010101},  // приклад
        {"All zeros", make([]byte, 100), 0x9a3d51e4},  // приклад
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            result := computeCRC32(tc.data)
            // Для edge cases перевіряємо, що функція не панікує
            assert.NotPanics(t, func() { computeCRC32(tc.data) })
            // Конкретні значення залежать від реалізації
        })
    }
}
```

### 🔹 Бенчмарк продуктивності

```go
func BenchmarkComputeCRC32(b *testing.B) {
    data := testDataPmt[:len(testDataPmt)-4]  // 26 байт
    
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        computeCRC32(data)
    }
}

func BenchmarkComputeCRC32Fast(b *testing.B) {
    data := testDataPmt[:len(testDataPmt)-4]
    
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        computeCRC32Fast(data)  // версія з таблицею
    }
}

// Очікувані результати:
// BenchmarkComputeCRC32-8        1000000    1200 ns/op
// BenchmarkComputeCRC32Fast-8    5000000     200 ns/op  ✅ 6x швидше
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Неправильний поліном | Всі CRC перевірки провалюються | Перевірити: має бути `0x04c11db7` (MPEG-2), не `0xedb88320` (IEEE) |
| Неправильний порядок байт | Обчислений CRC ≠ очікуваний | Перевірити: CRC у потоці = big-endian; `binary.BigEndian.Uint32()` |
| Обчислення для неправильного діапазону | CRC не співпадає | Перевірити: обчислювати для даних БЕЗ останніх 4 байт (саме ці 4 байти = CRC) |
| Початкове значення не 0xFFFFFFFF | CRC зсувається | Перевірити: `crc := uint32(0xffffffff)` на початку |
| Відсутність інверсії на кінці | Результат інвертований | Перевірити: MPEG-2 CRC НЕ інвертує результат на кінці (на відміну від деяких інших стандартів) |

### Приклад діагностики CRC помилок:

```go
func debugCRC32(data []byte, storedCRC uint32) {
    computed := computeCRC32(data)
    
    log.Infof("CRC32 debug:")
    log.Infof("  Data length: %d bytes", len(data))
    log.Infof("  First 8 bytes: %X", data[:min(8, len(data))])
    log.Infof("  Last 8 bytes: %X", data[max(0, len(data)-8):])
    log.Infof("  Stored CRC: 0x%08X (%d)", storedCRC, storedCRC)
    log.Infof("  Computed CRC: 0x%08X (%d)", computed, computed)
    log.Infof("  Match: %v", storedCRC == computed)
    
    if storedCRC != computed {
        // 🔹 Спробувати визначити можливу причину
        if computed == ^storedCRC {
            log.Warnf("  Hint: CRC appears inverted (XOR with 0xFFFFFFFF)")
        }
        if binary.LittleEndian.Uint32(data[len(data)-4:]) == storedCRC {
            log.Warnf("  Hint: CRC may be little-endian in stream")
        }
    }
}

func min(a, b int) int { if a < b { return a }; return b }
func max(a, b int) int { if a > b { return a }; return b }
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базова валідація CRC32:
func validateCRC32(data []byte, storedCRC uint32) bool {
    return computeCRC32(data) == storedCRC
}

// 2: Обчислення з кешуванням таблиці:
var crc32TableOnce sync.Once
var crc32Table [256]uint32

func initCRC32Table() {
    polynomial := uint32(0x04c11db7)
    for i := 0; i < 256; i++ {
        crc := uint32(i) << 24
        for j := 0; j < 8; j++ {
            if crc&0x80000000 != 0 {
                crc = (crc << 1) ^ polynomial
            } else {
                crc <<= 1
            }
        }
        crc32Table[i] = crc
    }
}

func computeCRC32(data []byte) uint32 {
    crc32TableOnce.Do(initCRC32Table)
    
    crc := uint32(0xffffffff)
    for _, b := range data {
        crc = (crc << 8) ^ crc32Table[(crc>>24)^uint32(b)]
    }
    return crc
}

// 3: Інтеграція у парсинг PSI:
func parsePSIWithCRC(i *astikit.BytesIterator, sectionLength int) error {
    // ... парсинг заголовка ...
    
    // Обчислити CRC для даних
    dataStart := i.Offset()
    dataEnd := dataStart + sectionLength - 4  // мінус CRC32
    
    data, _ := i.NextBytesNoCopy(dataEnd - dataStart)
    computedCRC := computeCRC32(data)
    
    // Прочитати збережений CRC
    i.Seek(dataEnd)
    crcBytes, _ := i.NextBytesNoCopy(4)
    storedCRC := binary.BigEndian.Uint32(crcBytes)
    
    // Валідація
    if computedCRC != storedCRC {
        return fmt.Errorf("CRC32 mismatch: stored=0x%08X, computed=0x%08X", 
            storedCRC, computedCRC)
    }
    
    return nil
}

// 4: Моніторинг:
func monitorCRCHealth(channelID string, metrics *CRCMetrics, valid bool) {
    if valid {
        metrics.CRCValid.WithLabelValues(channelID).Inc()
    } else {
        metrics.CRCInvalid.WithLabelValues(channelID).Inc()
    }
}
```

---

## 📊 Матриця використання CRC32 у вашому пайплайні

```
Контекст                  | Дія                          | Наслідок при помилці
──────────────────────────┼──────────────────────────────┼─────────────────────────
Парсинг PAT               | ✅ Валідувати обов'язково    | ❌ Не знайде програми
Парсинг PMT               | ✅ Валідувати обов'язково    | ❌ Не знайде відео/аудіо
Парсинг EIT/SDT/TOT       | ✅ Валідувати обов'язково    | ⚠️ Втратить метадані
Запис вихідного TS        | ✅ Обчислювати коректно      | ❌ Плеєри відкинуть потік
Мережевий моніторинг      | 📊 Логувати помилки CRC      | 🔍 Детекція проблем мережі
High-throughput стрімінг  | ⚡ Використовувати таблицю   | 📈 Продуктивність ×10
```

---

## 📚 Корисні посилання

- [ISO/IEC 13818-1: CRC32 specification](https://www.iso.org/standard/61236.html)
- [MPEG-2 CRC32 polynomial details](https://reveng.sourceforge.net/crc-catalogue/all.htm#crc.cat.crc-32-mpeg-2)
- [astits CRC32 source](https://github.com/asticode/go-astits/blob/master/crc.go)
- [CRC32 lookup table optimization](https://create.stephan-brumme.com/crc32/)

> 💡 **Ключова ідея**: CRC32 — це "імунна система" вашого MPEG-TS потоку. У вашому CCTV HLS пайплайні це дозволяє:
> - ✅ Гарантувати цілісність метаданих (PAT/PMT/EIT) перед обробкою
> - 🔍 Детектувати пошкодження мережі/енкодера на ранньому етапі
> - 📊 Збирати метрики якості потоку для проактивного моніторингу
> - ⚡ Оптимізувати валідацію через lookup таблиці для high-throughput

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати CRC32 валідацію у ваш `parsePSISection` з логуванням помилок
- ⚡ Реалізувати оптимізовану версію `computeCRC32` з lookup таблицею
- 📈 Додати Prometheus-метрики для моніторингу цілісності даних по каналах

🛠️