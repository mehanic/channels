# Глибоке роз'яснення: `crc.go` — ядро обчислення CRC32 для MPEG-2 у astits

Цей файл містить **основні функції обчислення CRC32** з поліномом MPEG-2 (`0x04c11db7`) — критичний компонент для валідації цілісності PSI даних (PAT/PMT/EIT/SDT/TOT) у вашому пайплайні.

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

## 🔧 Константи та функції: архітектура

### 🔹 Константа `crc32Polynomial`

```go
const crc32Polynomial = uint32(0xffffffff)
```

> ⚠️ **Увага**: Назва змінної може вводити в оману! Це **не поліном**, а **початкове значення** (initial value) для CRC обчислення.

**Правильне розуміння:**
```
• Початкове значення (initial value): 0xffffffff ✅
• Поліном (polynomial): 0x04c11db7 (у таблиці tableCRC32) ✅
• Фінальна інверсія: ❌ немає для MPEG-2
• Вхід/вихід: не інвертуються (refin/refout = false)
```

### 🔹 Функція `computeCRC32`: публічний інтерфейс

```go
func computeCRC32(bs []byte) uint32 {
    return updateCRC32(crc32Polynomial, bs)
}
```

**Призначення:**
```
• Обчислити CRC32 для масиву байтів
• Використовує `updateCRC32` з початковим значенням 0xffffffff
• Повертає готовий CRC без додаткових перетворень (MPEG-2 не інвертує результат)

Приклад використання:
  data := []byte{0x00, 0xb0, 0x0d, /* ... */}
  crc := computeCRC32(data)  // → 0xe295f69d для тестового PAT
```

### 🔹 Функція `updateCRC32`: оптимізоване ядро

```go
func updateCRC32(crc32 uint32, bs []byte) uint32 {
    for _, b := range bs {
        crc32 = (crc32 << 8) ^ tableCRC32[((crc32>>24)^uint32(b))&0xff]
    }
    return crc32
}
```

**Покроковий розбір алгоритму:**

```
Вхід: crc32 = поточне значення, bs = масив байтів

Для кожного байта b у bs:
  1. index = ((crc32 >> 24) ^ uint32(b)) & 0xff
     • crc32 >> 24: старші 8 біт поточного CRC
     • ^ uint32(b): XOR з поточним байтом даних
     • & 0xff: гарантує індекс у діапазоні 0-255
  
  2. tableValue = tableCRC32[index]
     • Lookup у згенерованій таблиці (1024 байти)
  
  3. crc32 = (crc32 << 8) ^ tableValue
     • crc32 << 8: зсув на 8 біт вліво
     • ^ tableValue: XOR з табличним значенням

Повернути: фінальне значення crc32
```

**Візуалізація одного кроку:**
```
Початок: crc32 = 0xABCDEF12, b = 0x34

1. index = ((0xABCDEF12 >> 24) ^ 0x34) & 0xff
         = (0xAB ^ 0x34) & 0xff
         = 0x9F

2. tableValue = tableCRC32[0x9F]
              = 0x1A2B3C4D  // приклад з таблиці

3. crc32 = (0xABCDEF12 << 8) ^ 0x1A2B3C4D
         = 0xCDEF1200 ^ 0x1A2B3C4D
         = 0xD6E42E4D

Результат: новий crc32 = 0xD6E42E4D
```

---

## 🧮 Математика CRC32 MPEG-2

### 🔹 Параметри стандарту

```
Назва: CRC-32/MPEG-2
Поліном: 0x04C11DB7
Початкове значення: 0xFFFFFFFF
Вхідна інверсія: false (no refin)
Вихідна інверсія: false (no refout)
XOR виходу: 0x00000000
```

### 🔹 Порівняння з іншими варіантами CRC32

| Стандарт | Поліном | Початкове | In/Out | XOR виходу | Використання |
|----------|---------|-----------|--------|------------|--------------|
| **MPEG-2** | `0x04C11DB7` | `0xFFFFFFFF` | ❌/❌ | `0x00000000` | ✅ MPEG-TS, DVB |
| IEEE | `0xEDB88320` | `0xFFFFFFFF` | ✅/✅ | `0xFFFFFFFF` | Ethernet, ZIP, PNG |
| POSIX | `0x04C11DB7` | `0x00000000` | ❌/❌ | `0xFFFFFFFF` | cksum utility |

> ⚠️ **Критично**: Використання неправильного стандарту → всі перевірки проваляться!

### 🔹 Приклад обчислення для тестових даних

```go
// testDataPat без останніх 4 байт:
data := []byte{
    0x00, 0xb0, 0x0d, 0x00, 0x01, 0xe1, 0x00, 0x00,  // PSI header
    0x00, 0x01, 0xf0, 0x00,                           // program entry
}

// Обчислення:
crc := computeCRC32(data)
// Результат: crc = 0xe295f69d ✅

// Перевірка:
storedCRC := binary.BigEndian.Uint32([]byte{0xe2, 0x95, 0xf6, 0x9d})
assert.Equal(t, storedCRC, crc)  // ✅ співпадає
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Валідація CRC32 при парсингу PSI

```go
// У parsePSISection — перевірка цілісності перед обробкою:
func parsePSISectionWithCRC(i *astikit.BytesIterator, sectionLength int) (*astits.PSISection, error) {
    // 🔹 Парсити заголовок для отримання офсетів
    header, offsetStart, offsetSectionsEnd, offsetEnd, err := parsePSISectionHeader(i)
    if err != nil { return nil, err }
    
    // 🔹 Обчислити CRC32 для даних секції (без останніх 4 байт)
    i.Seek(offsetStart)
    data, _ := i.NextBytesNoCopy(offsetSectionsEnd - offsetStart)
    computedCRC := computeCRC32(data)
    
    // 🔹 Прочитати CRC32 з потоку (останні 4 байти, big-endian)
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
    CRCValid       *prometheus.CounterVec  // успішні перевірки
    CRCInvalid     *prometheus.CounterVec  // помилки валідації
    CRCLatency     *prometheus.HistogramVec  // час обчислення CRC
    BytesValidated *prometheus.CounterVec  // загальний обсяг перевірених даних
}

// У обробці даних:
func validateWithMetrics(data []byte, storedCRC uint32, channelID string, 
                        metrics *CRCMetrics) error {
    start := time.Now()
    computed := computeCRC32(data)
    latency := time.Since(start)
    
    metrics.CRCLatency.WithLabelValues(channelID).Observe(latency.Seconds())
    metrics.BytesValidated.WithLabelValues(channelID).Add(float64(len(data)))
    
    if computed == storedCRC {
        metrics.CRCValid.WithLabelValues(channelID).Inc()
        return nil
    }
    
    metrics.CRCInvalid.WithLabelValues(channelID).Inc()
    log.Warnf("Channel %s: CRC32 mismatch (stored=0x%08X, computed=0x%08X, len=%d)", 
        channelID, storedCRC, computed, len(data))
    
    return fmt.Errorf("CRC32 validation failed")
}
```

### ✅ 3: Оптимізація для high-throughput

```go
// 🔹 Пакетна обробка для зменшення накладних витрат:
func computeCRC32Batch(chunks [][]byte) []uint32 {
    results := make([]uint32, len(chunks))
    for i, chunk := range chunks {
        results[i] = computeCRC32(chunk)
    }
    return results
}

// 🔹 Кешування результатів для повторюваних даних:
type CRCCache struct {
    mu    sync.RWMutex
    cache map[string]uint32  // key = hex(data), value = crc
}

func (c *CRCCache) GetOrCompute(data []byte) uint32 {
    key := hex.EncodeToString(data)
    
    // Спробувати отримати з кешу
    c.mu.RLock()
    if crc, ok := c.cache[key]; ok {
        c.mu.RUnlock()
        return crc
    }
    c.mu.RUnlock()
    
    // Обчислити та зберегти
    crc := computeCRC32(data)
    
    c.mu.Lock()
    c.cache[key] = crc
    c.mu.Unlock()
    
    return crc
}

// 🔹 Використання:
crcCache := &CRCCache{cache: make(map[string]uint32)}
crc := crcCache.GetOrCompute(psiData)
```

### ✅ 4: Обробка помилок CRC32

```go
// Стратегія відновлення при невалідному CRC:
func handleCRCError(data []byte, storedCRC, computedCRC uint32, 
                   channelID string, metrics *CRCMetrics) error {
    
    // 🔹 Логування для відладки
    log.Errorf("Channel %s: CRC32 mismatch", channelID)
    log.Debugf("  Data length: %d bytes", len(data))
    log.Debugf("  First 8 bytes: %X", data[:min(8, len(data))])
    log.Debugf("  Stored CRC: 0x%08X", storedCRC)
    log.Debugf("  Computed CRC: 0x%08X", computedCRC)
    
    // 🔹 Спроба визначити тип пошкодження
    if computedCRC == ^storedCRC {
        log.Warnf("  Hint: CRC appears inverted (XOR with 0xFFFFFFFF)")
    }
    if binary.LittleEndian.Uint32(data[len(data)-4:]) == storedCRC {
        log.Warnf("  Hint: CRC may be little-endian in stream")
    }
    
    // 🔹 Опція 1: пропустити пошкоджену секцію (рекомендовано)
    log.Warnf("Channel %s: skipping corrupted PSI section", channelID)
    metrics.CRCInvalid.WithLabelValues(channelID).Inc()
    return nil  // не зупиняти весь потік
    
    // 🔹 Опція 2: спробувати виправити (НЕ рекомендується)
    // ❌ Ризик отримати некоректні метадані
}

func min(a, b int) int { if a < b { return a }; return b }
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Базовий тест на валідних даних

```go
func TestComputeCRC32_ValidData(t *testing.T) {
    testCases := []struct {
        name     string
        data     []byte
        expected uint32
    }{
        {
            name:     "PAT",
            data:     testDataPat[:len(testDataPat)-4],
            expected: 0xe295f69d,
        },
        {
            name:     "PMT",
            data:     testDataPmt[:len(testDataPmt)-4],
            expected: 0x389285ac,
        },
        {
            name:     "Empty",
            data:     []byte{},
            expected: 0xffffffff,  // початкове значення
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            result := computeCRC32(tc.data)
            assert.Equal(t, tc.expected, result)
        })
    }
}
```

### 🔹 Тест на пошкоджені дані

```go
func TestComputeCRC32_CorruptedData(t *testing.T) {
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

### 🔹 Бенчмарк продуктивності

```go
func BenchmarkComputeCRC32(b *testing.B) {
    data := testDataPmt[:len(testDataPmt)-4]  // 26 байт
    
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        computeCRC32(data)
    }
}

// Очікувані результати:
// BenchmarkComputeCRC32-8    5000000    200 ns/op    0 B/op    0 allocs/op
// ✅ 0 алокацій завдяки lookup-таблиці
// ✅ ~200 ns/op для 26 байт = ~8 ns/байт
// ✅ ~10-20× швидше за побайтове обчислення без таблиці
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Неправильне початкове значення | Всі CRC перевірки провалюються | Перевірити: має бути `0xffffffff`, не `0x00000000` |
| Неправильний порядок байт | Обчислений CRC ≠ очікуваний | Перевірити: CRC у потоці = big-endian; використовувати `binary.BigEndian.Uint32()` |
| Обчислення для неправильного діапазону | CRC не співпадає | Перевірити: обчислювати для даних БЕЗ останніх 4 байт (саме ці 4 байти = CRC) |
| Інверсія на кінці | Результат інвертований | Перевірити: MPEG-2 CRC НЕ інвертує результат на кінці (на відміну від IEEE) |
| Таблиця не ініціалізована | Паніка або неправильні значення | Перевірити: `tableCRC32` має бути згенерована у `crc32_table.go` |

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
        // 🔹 Перевірити, чи дані обрізані
        if len(data) < 4 {
            log.Warnf("  Hint: Data too short for CRC validation")
        }
    }
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Базова валідація:
func validateCRC32(data []byte, storedCRC uint32) bool {
    return computeCRC32(data) == storedCRC
}

// 2: Інтеграція у парсинг PSI:
func parsePSIWithCRC(i *astikit.BytesIterator, sectionLength int) error {
    // ... парсинг заголовка ...
    
    // Обчислити CRC для даних (без останніх 4 байт)
    dataStart := i.Offset()
    dataEnd := dataStart + sectionLength - 4
    
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

// 3: Моніторинг:
func monitorCRCHealth(channelID string, metrics *CRCMetrics, valid bool, latency time.Duration) {
    metrics.CRCLatency.WithLabelValues(channelID).Observe(latency.Seconds())
    if valid {
        metrics.CRCValid.WithLabelValues(channelID).Inc()
    } else {
        metrics.CRCInvalid.WithLabelValues(channelID).Inc()
    }
}

// 4: Пакетна валідація для оптимізації:
func validateBatchCRC(chunks [][]byte, storedCRCs []uint32) []bool {
    results := make([]bool, len(chunks))
    for i, data := range chunks {
        results[i] = computeCRC32(data) == storedCRCs[i]
    }
    return results
}

// 5: Helper для логування:
func logCRCResult(channelID string, data []byte, storedCRC, computedCRC uint32, valid bool) {
    if valid {
        log.Debugf("Channel %s: CRC32 OK (0x%08X, %d bytes)", 
            channelID, computedCRC, len(data))
    } else {
        log.Errorf("Channel %s: CRC32 FAIL (stored=0x%08X, computed=0x%08X)", 
            channelID, storedCRC, computedCRC)
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
- [VLC CRC implementation reference](https://github.com/videolan/vlc/blob/master/modules/mux/mpeg/ps.c)
- [CRC32 lookup table optimization](https://create.stephan-brumme.com/crc32/)

> 💡 **Ключова ідея**: CRC32 — це "імунна система" вашого MPEG-TS потоку. У вашому CCTV HLS пайплайні це дозволяє:
> - ✅ Гарантувати цілісність метаданих (PAT/PMT/EIT) перед обробкою
> - 🔍 Детектувати пошкодження мережі/енкодера на ранньому етапі
> - 📊 Збирати метрики якості потоку для проактивного моніторингу
> - ⚡ Оптимізувати валідацію через lookup таблиці для high-throughput

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати CRC32 валідацію у ваш `parsePSISection` з логуванням помилок
- ⚡ Реалізувати пакетну обробку для зменшення накладних витрат
- 📈 Додати Prometheus-метрики для моніторингу цілісності даних по каналах

🛠️