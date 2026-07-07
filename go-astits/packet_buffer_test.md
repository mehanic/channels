# Глибоке роз'яснення: `TestAutoDetectPacketSize` — автодетекція розміру TS-пакету

Цей тест перевіряє функцію `autoDetectPacketSize`, яка **автоматично визначає розмір пакетів у MPEG-TS потоці** (188/192/204 байти) шляхом сканування синхробайтів (`0x47`).

---

## 🎯 Навіщо потрібна автодетекція?

```
┌─────────────────────────────────────────┐
│ Проблема:                               │
│ • MPEG-TS стандартизує 3 розміри пакетів│
│   - 188 байт: базовий (DVB, ATSC)       │
│   - 192 байт: +4B FEC (деякі системи)   │
│   - 204 байт: +16Б RS-кодування         │
│                                         │
│ • Вхідний потік може не мати метаданих  │
│ • Неправильний розмір = зсув парсингу   │
│   → всі подальші дані інтерпретуються   │
│   неправильно → каскад помилок          │
│                                         │
│ Рішення: autoDetectPacketSize()         │
│ • Сканує потік на наявність 0x47        │
│ • Знаходить періодичність появи         │
│ • Повертає найімовірніший розмір        │
└─────────────────────────────────────────┘
```

**Математика детекції:**
```
Якщо синхробайти на позиціях: [0, 188, 376, 564, ...]
→ Інтервали: [188, 188, 188, ...]
→ Гіпотеза: розмір пакету = 188 байт ✅

Якщо є "шум" (фальшиві 0x47 у payload):
Позиції: [0, 21, 188, 376, ...]
Інтервали: [21, 167, 188, ...]
→ Фільтруємо аномалії, шукаємо домінуючий інтервал ≈ 188
```

---

## 🔧 Розбір тесту

### Кейс 1: Невірний синхробайт на початку

```go
// Створюємо буфер, де перший байт ≠ 0x47
buf := &bytes.Buffer{}
w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
w.Write(uint8(2))              // ❌ 0x02 замість 0x47
w.Write(byte(syncByte))        // 0x47 на позиції 1

_, err := autoDetectPacketSize(bytes.NewReader(buf.Bytes()))
assert.EqualError(t, err, ErrPacketMustStartWithASyncByte.Error())
```

**Логіка функції:**
```go
func autoDetectPacketSize(r io.Reader) (int, error) {
    // 1. Прочитати перший байт
    var first byte
    if _, err := io.ReadFull(r, []byte{first}); err != nil {
        return 0, err
    }
    
    // 2. Перевірити синхробайт
    if first != syncByte {  // syncByte = 0x47
        return 0, ErrPacketMustStartWithASyncByte
    }
    
    // 3. Далі — сканування на періодичність...
}
```

> 💡 **Важливо**: Якщо потік не починається з `0x47`, це може означати:
> - Пошкоджений файл
> - Потік з зміщенням (починається з середини пакета)
> - Не TS-формат взагалі

---

### Кейс 2: Валідна детекція розміру 188 байт

```go
buf.Reset()
w.Write(byte(syncByte))           // Позиція 0:   0x47 ✅
w.Write(make([]byte, 20))         // Позиції 1-20: 20 байт шуму
w.Write(byte(syncByte))           // Позиція 21:  0x47 (фальшивий синхр!)
w.Write(make([]byte, 166))        // Позиції 22-187: 166 байт
w.Write(byte(syncByte))           // Позиція 188: 0x47 ✅ (справжній початок пакету #2)
w.Write(make([]byte, 187))        // Позиції 189-375: 187 байт
w.Write([]byte("test"))           // Позиції 376-379: 4 байти

r := bytes.NewReader(buf.Bytes())  // Загальний розмір: 380 байт
p, err := autoDetectPacketSize(r)

assert.NoError(t, err)
assert.Equal(t, MpegTsPacketSize, p)  // MpegTsPacketSize = 188
assert.Equal(t, 380, r.Len())         // ✅ Читач "зупинився" на позиції 380
```

**Візуалізація буфера:**
```
Позиція:  0    1-20   21   22-187  188  189-375  376-379
Дані:    [47][20×00][47][166×00][47][187×00]["test"]
           ↑         ↑       ↑
        Пакет 1   Шум/Фейк  Пакет 2 (справжній)
        початок             початок
```

**Як алгоритм відрізняє справжні синхробайти від фейкових?**

```
1. Збирає всі позиції синхробайтів: [0, 21, 188]
2. Обчислює інтервали: [21, 167]
3. Шукає інтервали, близькі до стандартних розмірів:
   - 21: не близько до 188/192/204 → відкидаємо як шум
   - 167: 188-167=21 → можливо, пропущено один синхр?
4. Перевіряє гіпотезу "розмір=188":
   - Очікувані позиції: 0, 188, 376...
   - Знайдені позиції: 0✅, 21❌, 188✅
   - Співпадіння: 2 з 3 → достатньо для впевненості ✅
5. Повертає 188
```

> 💡 **Ключова ідея**: Алгоритм толерантний до "шуму" (випадкових `0x47` у payload), шукаючи **найбільш послідовну періодичність**.

---

## 🔍 Гіпотетична реалізація `autoDetectPacketSize`

```go
func autoDetectPacketSize(r io.Reader) (int, error) {
    // 1. Перевірка першого байта
    var first byte
    if _, err := io.ReadFull(r, []byte{&first}); err != nil {
        return 0, err
    }
    if first != syncByte {
        return 0, ErrPacketMustStartWithASyncByte
    }
    
    // 2. Сканування потоку для пошуку синхробайтів
    const scanBytes = 4096  // скануємо перші 4KB
    buf := make([]byte, scanBytes)
    n, _ := io.ReadFull(r, buf)  // ігноруємо помилку неповного читання
    
    // 3. Збираємо позиції всіх 0x47
    var syncPositions []int
    for i := 0; i < n; i++ {
        if buf[i] == syncByte {
            syncPositions = append(syncPositions, i)
        }
    }
    
    // 4. Обчислюємо інтервали між сусідніми синхробайтами
    var intervals []int
    for i := 1; i < len(syncPositions); i++ {
        intervals = append(intervals, syncPositions[i]-syncPositions[i-1])
    }
    
    // 5. Шукаємо найчастіший інтервал серед стандартних розмірів
    standardSizes := []int{188, 192, 204}
    bestSize := 0
    bestScore := 0
    
    for _, size := range standardSizes {
        score := 0
        for _, interval := range intervals {
            // Допускаємо невелике відхилення (шум/помилки)
            if abs(interval-size) <= 5 {
                score++
            }
        }
        if score > bestScore {
            bestScore = score
            bestSize = size
        }
    }
    
    if bestSize == 0 {
        return 0, fmt.Errorf("could not detect packet size")
    }
    
    return bestSize, nil
}

func abs(x int) int {
    if x < 0 { return -x }
    return x
}
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Автодетекція при ініціалізації потоку

```go
// У вашому WebSocket-приймачі або файловому читачі:
func initTSReader(r io.Reader) (*astits.Demuxer, error) {
    // Спробуємо автодетектувати розмір пакету
    packetSize, err := autoDetectPacketSize(r)
    if err != nil {
        // fallback на стандартний розмір
        log.Warnf("Auto-detect failed: %v, using default 188", err)
        packetSize = astits.MpegTsPacketSize
    } else {
        log.Infof("Detected TS packet size: %d", packetSize)
    }
    
    // Створити демуксер з правильним розміром
    // ⚠️ Потрібно скинути reader на початок або використати io.MultiReader
    return astits.NewDemuxer(ctx, r, astits.DemuxerOptPacketSize(packetSize)), nil
}
```

> ⚠️ **Важливо**: `autoDetectPacketSize` читає дані з `io.Reader`. Після виклику потрібно:
> - або скинути читач на початок (`Seek(0)` для файлів)
> - або використати `io.MultiReader(bytes.NewReader(detectedBytes), originalReader)`

### ✅ 2. Обробка потоків з невідомим форматуванням

```go
// У segmentAssembler — для вхідних fMP4-фрагментів, які можуть бути "загорнуті" в TS:
func detectAndProcess(rawData []byte, channelID string) error {
    // Спроба 1: припустити стандартний 188-байтний TS
    if isValidTS(rawData, 188) {
        return processTS(rawData, 188, channelID)
    }
    
    // Спроба 2: автодетекція
    reader := bytes.NewReader(rawData)
    size, err := autoDetectPacketSize(reader)
    if err == nil && size != 188 {
        log.Infof("Channel %s: non-standard packet size %d detected", channelID, size)
        reader.Seek(0, io.SeekStart)  // скинути для повторного читання
        return processTS(rawData, size, channelID)
    }
    
    // Спроба 3: можливо, це чистий fMP4 без TS-обгортки
    return processRawMP4(rawData, channelID)
}

func isValidTS(data []byte, packetSize int) bool {
    if len(data) < packetSize { return false }
    for i := 0; i < len(data); i += packetSize {
        if data[i] != 0x47 { return false }
    }
    return true
}
```

### ✅ 3. Моніторинг якості вхідного потоку

```go
// monitoring.Monitor — метрики для автодетекції:
type DetectionMetrics struct {
    AutoDetectAttempts  *prometheus.CounterVec  // скільки разів викликали детекцію
    AutoDetectSuccess   *prometheus.CounterVec  // успішні детекції
    DetectedPacketSizes *prometheus.HistogramVec  // розподіл виявлених розмірів
    FallbackToDefault   *prometheus.CounterVec  // скільки разів використовували 188 за замовчуванням
}

// У ініціалізації читача:
func initReaderWithMetrics(r io.Reader, channelID string, metrics *DetectionMetrics) (*astits.Demuxer, error) {
    metrics.AutoDetectAttempts.WithLabelValues(channelID).Inc()
    
    packetSize, err := autoDetectPacketSize(r)
    if err != nil {
        metrics.FallbackToDefault.WithLabelValues(channelID).Inc()
        log.Warnf("Channel %s: auto-detect failed, using default", channelID)
        packetSize = astits.MpegTsPacketSize
    } else {
        metrics.AutoDetectSuccess.WithLabelValues(channelID).Inc()
        metrics.DetectedPacketSizes.WithLabelValues(channelID).Observe(float64(packetSize))
    }
    
    // ... створення демуксера ...
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на детекцію 192-байтних пакетів

```go
func TestAutoDetectPacketSize_192Bytes(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Генеруємо 5 пакетів по 192 байти
    for i := 0; i < 5; i++ {
        w.Write(byte(syncByte))                    // початок пакету
        w.Write(make([]byte, 191))                 // 191 байт payload
    }
    
    r := bytes.NewReader(buf.Bytes())
    size, err := autoDetectPacketSize(r)
    
    assert.NoError(t, err)
    assert.Equal(t, 192, size)  // ✅ виявлено нестандартний розмір
}
```

### 🔹 Тест на стійкість до шуму (багато фальшивих 0x47)

```go
func TestAutoDetectPacketSize_NoisyPayload(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Реальні пакети по 188 байт + випадкові 0x47 у payload
    for i := 0; i < 10; i++ {
        w.Write(byte(syncByte))  // справжній синхр
        
        // Згенерувати 187 байт "шумного" payload
        payload := make([]byte, 187)
        for j := range payload {
            // 5% ймовірність фальшивого синхробайта
            if rand.Intn(100) < 5 {
                payload[j] = syncByte
            } else {
                payload[j] = byte(rand.Intn(256))
            }
        }
        w.Write(payload)
    }
    
    r := bytes.NewReader(buf.Bytes())
    size, err := autoDetectPacketSize(r)
    
    assert.NoError(t, err)
    assert.Equal(t, 188, size)  // ✅ алгоритм відфільтрував шум
}
```

### 🔹 Тест на детекцію зі зміщенням (потік починається не з початку пакету)

```go
func TestAutoDetectPacketSize_OffsetStart(t *testing.T) {
    // Сценарій: отримали потік, що починається з середини пакету
    fullPacket := make([]byte, 188)
    fullPacket[0] = syncByte
    // ... заповнити решту ...
    
    // Взяти тільки другу половину + наступний повний пакет
    offsetData := append(fullPacket[100:], fullPacket...)  // зміщення 100 байт
    
    r := bytes.NewReader(offsetData)
    _, err := autoDetectPacketSize(r)
    
    // Очікуємо помилку: перший байт ≠ 0x47
    assert.Error(t, err)
    assert.Equal(t, ErrPacketMustStartWithASyncByte, err)
    
    // Рішення: спробувати знайти перший 0x47 і почати з нього
    // (це вже має робити вищий рівень, не autoDetectPacketSize)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Помилкова детекція через шум у payload | Повертається 192 замість 188 | Збільшити `scanBytes` для більшої статистики; додати поріг впевненості (мінімум 3 співпадіння) |
| Потік починається не з синхробайта | `ErrPacketMustStartWithASyncByte` | Реалізувати "пошук першого синхробайта" перед детекцією: `findFirstSyncByte(r)` |
| Дуже короткий вхідний буфер | Недостатньо даних для статистики | Повертати `fallbackSize` за замовчуванням, якщо `< 2*packetSize` даних |
| Змішані розміри пакетів у потоці | Нестабільна детекція | Це порушення стандарту — залогити помилку та використовувати перший виявлений розмір |
| `io.Reader` не підтримує `Seek` | Неможливо повторно прочитати після детекції | Використовувати `io.TeeReader` або буферизувати перші байти перед детекцією |

### Приклад безпечної ініціалізації з буферизацією:

```go
func safeInitDemuxer(r io.Reader) (*astits.Demuxer, error) {
    // Буферизуємо перші 4KB для детекції
    const detectBuffer = 4096
    buf := make([]byte, detectBuffer)
    n, err := io.ReadFull(r, buf)
    if err != nil && err != io.ErrUnexpectedEOF {
        return nil, err
    }
    
    // Детекція на буфері
    size, err := autoDetectPacketSize(bytes.NewReader(buf[:n]))
    if err != nil {
        size = astits.MpegTsPacketSize  // fallback
    }
    
    // Об'єднуємо буфер + оригінальний reader для продовження читання
    combined := io.MultiReader(bytes.NewReader(buf[:n]), r)
    
    return astits.NewDemuxer(ctx, combined, astits.DemuxerOptPacketSize(size)), nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Ініціалізація з автодетекцією:
func newTSProcessor(input io.Reader, channelID string) (*TSProcessor, error) {
    // Безпечна детекція з буферизацією
    demuxer, err := safeInitDemuxer(input)
    if err != nil {
        return nil, fmt.Errorf("failed to init demuxer for %s: %w", channelID, err)
    }
    
    return &TSProcessor{
        demuxer: demuxer,
        channelID: channelID,
        // ... інші поля ...
    }, nil
}

// 2. Обробка з fallback логікою:
func processWithFallback(rawData []byte) error {
    // Спроба 1: стандартний розмір
    if err := tryProcess(rawData, 188); err == nil {
        return nil
    }
    
    // Спроба 2: автодетекція
    if size, err := autoDetectPacketSize(bytes.NewReader(rawData)); err == nil {
        if err := tryProcess(rawData, size); err == nil {
            log.Infof("Auto-detected packet size: %d", size)
            return nil
        }
    }
    
    // Спроба 3: інші формати
    return tryProcessRaw(rawData)
}

// 3. Логування для відладки:
if config.DebugTS {
    log.Debugf("Channel %s: scanning for packet size in %d bytes", 
        channelID, len(sampleData))
    positions := findSyncBytes(sampleData)
    log.Debugf("Sync byte positions: %v", positions[:min(20, len(positions))])
}
```

---

## 📊 Матриця сценаріїв детекції

```
Вхідні дані                    | Очікуваний результат      | Коментар
───────────────────────────────┼───────────────────────────┼─────────
[47][187×дані][47][187×дані]  | ✅ 188                     | Ідеальний випадок
[47][20×00][47][166×00][47]   | ✅ 188                     | Шум між пакетами
[47][191×дані][47][191×дані]  | ✅ 192                     | FEC-розширення
[47][203×дані][47][203×дані]  | ✅ 204                     | RS-кодування
[02][47][187×дані]...         | ❌ помилка                 | Не починається з 0x47
[47][50×00][47][50×00]...     | ❌ невизначено             | Занадто мало даних
[47][187×47]...               | ⚠️ ризик помилкової детекції| Всі байти = 0x47 (малоймовірно)
```

---

## 📚 Корисні посилання

- [MPEG-TS packet structure](https://en.wikipedia.org/wiki/MPEG_transport_stream#Packet)
- [DVB transmission standards (188/204 bytes)](https://www.dvb.org/resources/public/standards)
- [astits demuxer options](https://pkg.go.dev/github.com/asticode/go-astits#DemuxerOptPacketSize)

> 💡 **Ключова ідея**: `autoDetectPacketSize` — це "перша лінія оборони" проти некоректних вхідних даних. У вашому CCTV HLS пайплайні це дозволяє:
> - 🔄 Автоматично адаптуватися до різних джерел потоку (різне обладнання, кодування)
> - 🚨 Ранньо детектувати пошкоджені або не-форматовані дані
> - 📊 Збирати метрики про різноманітність вхідних форматів для аналізу

Якщо потрібно — можу допомогти:
- 🔍 Реалізувати `findFirstSyncByte()` для обробки потоків зі зміщенням
- 📦 Інтегрувати автодетекцію у ваш `buildReader()` з підтримкою UDP/файлів
- 🧪 Написати fuzz-тест для перевірки стійкості детекції до випадкових даних

🛠️