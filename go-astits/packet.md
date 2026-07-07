# Глибоке роз'яснення: `packet.go` — ядро парсингу MPEG-TS у astits

Цей файл містить **фундаментальні структури та логіку** для роботи з пакетами MPEG-TS: парсинг, серіалізація, бітові операції. Це "серце" бібліотеки astits.

---

## 🎯 Архітектура файлу

```
┌─────────────────────────────────────────┐
│ Ключові компоненти packet.go:          │
│                                         │
│ 📦 Константи та типи:                  │
│   • ScramblingControl_* (0-3)          │
│   • MpegTsPacketSize = 188             │
│   • errSkippedPacket                   │
│                                         │
│ 🗂️ Основні структури:                  │
│   • Packet (головний контейнер)        │
│   • PacketHeader (4 байти, бітові поля)│
│   • PacketAdaptationField (змінна довж.)│
│   • PacketAdaptationExtensionField     │
│                                         │
│ 🔍 Функції парсингу:                   │
│   • parsePacket() → parseHeader → parseAF│
│   • parsePCR(), parsePTSOrDTS()        │
│                                         │
│ ✏️ Функції серіалізації:               │
│   • writePacket(), writePacketHeader() │
│   • writePCR(), calc*Length() helpers  │
│                                         │
│ 🧮 Допоміжні функції:                  │
│   • payloadOffset(), newStuffingAdaptationField()│
└─────────────────────────────────────────┘
```

---

## 🔧 Основні структури даних

### `Packet` — головний контейнер

```go
type Packet struct {
    AdaptationField *PacketAdaptationField  // опціональне поле (може бути nil)
    Header          PacketHeader            // обов'язковий заголовок (4 байти)
    Payload         []byte                  // корисне навантаження (може бути порожнім)
}
```

**Розмір пакету:**
```
Загальний розмір: 188 байт (стандарт)
├─ 1 байт: Sync byte (0x47)
├─ 3 байти: Packet header
├─ 0-183 байти: Adaptation field (опціонально)
└─ 0-184 байти: Payload (опціонально)

Важливо: Adaptation field + Payload ≤ 184 байти
```

### `PacketHeader` — 3 байти після sync (бітова структура)

```
Байт 0 (після sync):
[1] TransportErrorIndicator    (bit 7)
[1] PayloadUnitStartIndicator  (bit 6) ← ключовий для PES/PSI детекції
[1] TransportPriority          (bit 5)
[5] PID[12:8]                  (bits 4-0)

Байт 1:
[8] PID[7:0]                   (повний PID = 13 біт: 0-8191)

Байт 2:
[2] TransportScramblingControl (bits 7-6)
[2] AdaptationFieldControl     (bits 5-4): 00=reserved, 01=payload, 10=AF, 11=both
[4] ContinuityCounter          (bits 3-0): 0-15, циклічний лічильник
```

**Парсинг у коді:**
```go
return PacketHeader{
    ContinuityCounter:          uint8(bs[2] & 0xf),           // нижні 4 біти
    HasAdaptationField:         bs[2]&0x20 > 0,               // бит 5
    HasPayload:                 bs[2]&0x10 > 0,               // бит 4
    PayloadUnitStartIndicator:  bs[0]&0x40 > 0,               // бит 6
    PID:                        uint16(bs[0]&0x1f)<<8 | uint16(bs[1]),  // 13 біт
    TransportErrorIndicator:    bs[0]&0x80 > 0,               // бит 7
    TransportPriority:          bs[0]&0x20 > 0,               // бит 5
    TransportScramblingControl: uint8(bs[2]) >> 6 & 0x3,     // біти 7-6
}, nil
```

> 💡 **Ключовий момент**: `PayloadUnitStartIndicator` (PUSI) — найважливіший прапорець для детекції початку PES/PSI блоків.

### `PacketAdaptationField` — опціональне розширене поле

```
Структура (змінна довжина):
┌─────────────────────────┐
│ [8]  Length             │ ← загальна довжина цього поля (не включаючи цей байт)
├─────────────────────────┤
│ [1]  DiscontinuityIndicator  │ ← розрив у потоці
│ [1]  RandomAccessIndicator   │ ← точка входу для декодера
│ [1]  ES priority             │
│ [1]  PCR flag                │ ← якщо 1: далі 6-байтний PCR
│ [1]  OPCR flag               │ ← Original PCR для ремуксингу
│ [1]  Splicing point flag    │
│ [1]  Transport private flag │
│ [1]  Extension flag         │ ← якщо 1: далі йде extension field
├─────────────────────────┤
│ [48] PCR (optional)         │ ← 33-bit base + 6-bit reserved + 9-bit ext
│ [48] OPCR (optional)        │
│ [8]  Splice countdown       │
│ [8]  Private data length    │
│ [N]  Private data           │
├─────────────────────────┤
│ [8]  Extension length       │ ← якщо extension flag=1
│ [1]  LTW flag               │ ← Legal Time Window
│ [1]  Piecewise rate flag    │
│ [1]  Seamless splice flag   │
│ [5]  Reserved               │
│ [1]  LTW valid flag         │
│ [15] LTW offset             │
│ [2]  Reserved               │
│ [23] Piecewise rate         │
│ [4]  Splice type            │
│ [33] DTS next access unit   │
├─────────────────────────┤
│ [N]  Stuffing bytes (0xFF)  │ ← вирівнювання до потрібної довжини
└─────────────────────────┘
```

**Ключові поля для вашого пайплайну:**

| Поле | Призначення | Використання у CCTV HLS |
|------|-------------|------------------------|
| `PCR *ClockReference` | Еталонний час програми (27 MHz) | Синхронізація аудіо/відео, корекція дрейфу |
| `DiscontinuityIndicator` | Сигналізований розрив | Вставка `#EXT-X-DISCONTINUITY` у плейлист |
| `RandomAccessIndicator` | Точка входу (keyframe) | Детекція початку сегмента |
| `SpliceCountdown` | Лічильник до точки склейки | Підготовка до seamless switching |

---

## 🔍 Функція `parsePacket` — головний парсер

### Алгоритм по кроках

```go
func parsePacket(i *astikit.BytesIterator, s PacketSkipper) (*Packet, error) {
    // 🔹 Крок 1: Читання та перевірка sync byte
    b, err := i.NextByte()
    if b != syncByte {  // 0x47
        return nil, ErrPacketMustStartWithASyncByte
    }
    
    // 🔹 Крок 2: Обробка пакетів >188 байт (192/204)
    // Ігноруємо перші байти, якщо розмір >188
    i.Seek(i.Len() - MpegTsPacketSize + 1)  // позиціонуємо на останні 188 байт
    offsetStart := i.Offset()                // запам'ятовуємо початок
    
    // 🔹 Крок 3: Парсинг заголовка (3 байти)
    p.Header, err = parsePacketHeader(i)
    
    // 🔹 Крок 4: Парсинг адаптаційного поля (якщо є)
    if p.Header.HasAdaptationField {
        p.AdaptationField, err = parsePacketAdaptationField(i)
    }
    
    // 🔹 Крок 5: Фільтрація через PacketSkipper
    if s != nil && s(p) {
        return nil, errSkippedPacket  // спеціальна помилка "пропустити"
    }
    
    // 🔹 Крок 6: Витягування payload
    if p.Header.HasPayload {
        offset := payloadOffset(offsetStart, p.Header, p.AdaptationField)
        i.Seek(offset)
        p.Payload = i.Dump()  // читати до кінця пакету
    }
    
    return p, nil
}
```

### Обробка різних розмірів пакетів

```
Сценарій: вхідний потік має пакети по 204 байти (з RS-кодуванням)

1. Читаємо перший байт: 0x47 ✅
2. i.Len() = 204, MpegTsPacketSize = 188
3. i.Seek(204 - 188 + 1) = i.Seek(17) → переходимо на позицію 17
4. Тепер i містить останні 188 байт пакету → стандартний парсинг

Результат: парсер працює з 188-байтним "ядром", ігноруючи додаткові байти
```

> ⚠️ **Обмеження**: Додаткові байти (напр., FEC) не парсяться і не зберігаються. Якщо потрібно — модифікуйте логіку.

---

## 🧮 Функція `payloadOffset` — розрахунок початку корисного навантаження

```go
func payloadOffset(offsetStart int, h PacketHeader, a *PacketAdaptationField) int {
    offset := offsetStart + 3  // 3 байти заголовка після sync
    if h.HasAdaptationField {
        offset += 1 + a.Length  // 1 байт length field + вміст адаптаційного поля
    }
    return offset
}
```

**Приклад розрахунку:**
```
Вхідні дані:
• offsetStart = 17 (після Seek для 204-байтного пакету)
• HasAdaptationField = true
• AdaptationField.Length = 7

Розрахунок:
1. offset = 17 + 3 = 20 (кінець заголовка)
2. + 1 (байт length) = 21
3. + 7 (вміст AF) = 28
4. Payload починається з позиції 28

Перевірка: 188 - 28 = 160 байт payload ✅
```

---

## 🔬 Парсинг адаптаційного поля — складна умовна логіка

### Структура парсингу

```go
func parsePacketAdaptationField(i *astikit.BytesIterator) (*PacketAdaptationField, error) {
    a := &PacketAdaptationField{}
    
    // 1. Читання довжини поля
    b, _ := i.NextByte()
    a.Length = int(b)
    afStartOffset := i.Offset()  // для розрахунку stuffing
    
    if a.Length == 0 { return a, nil }  // порожнє адаптаційне поле
    
    // 2. Читання байта прапорців
    b, _ = i.NextByte()
    a.DiscontinuityIndicator = b&0x80 > 0
    a.RandomAccessIndicator = b&0x40 > 0
    // ... інші прапорці ...
    
    // 3. Умовне читання опціональних полів (за прапорцями)
    if a.HasPCR {
        a.PCR, _ = parsePCR(i)  // 6 байт
    }
    if a.HasOPCR {
        a.OPCR, _ = parsePCR(i)
    }
    if a.HasSplicingCountdown {
        b, _ = i.NextByte()
        a.SpliceCountdown = int(b)  // signed 8-bit
    }
    if a.HasTransportPrivateData {
        // ... читання private data ...
    }
    if a.HasAdaptationExtensionField {
        // ... парсинг вкладеного extension field ...
    }
    
    // 4. Розрахунок stuffing bytes
    a.StuffingLength = a.Length - (i.Offset() - afStartOffset)
    
    return a, nil
}
```

### Ключові моменти

1. **Порядок полів фіксований**: PCR → OPCR → Splice → PrivateData → Extension
2. **Stuffing розраховується в кінці**: `Length - фактично_прочитане`
3. **Extension field має власну довжину**: не плутати з довжиною основного AF

---

## ⏱️ Парсинг PCR — 6 байт → ClockReference

```go
func parsePCR(i *astikit.BytesIterator) (*ClockReference, error) {
    bs, _ := i.NextBytesNoCopy(6)  // 6 байт без копіювання (оптимізація)
    
    // Збірка 48-бітного значення з 6 байт (big-endian)
    pcr := uint64(bs[0])<<40 | uint64(bs[1])<<32 | uint64(bs[2])<<24 | 
           uint64(bs[3])<<16 | uint64(bs[4])<<8 | uint64(bs[5])
    
    // Розділення на base (33 біти) та extension (9 біт)
    // Формат: [33-bit base][6-bit reserved][9-bit extension]
    cr = newClockReference(
        int64(pcr>>15),      // base: старші 33 біти
        int64(pcr&0x1ff),    // extension: молодші 9 біт (0x1ff = 0b111111111)
    )
    return cr, nil
}
```

**Візуалізація бітів:**
```
PCR у потоці (48 біт = 6 байт):
[33] PCR_base     @ 90 kHz   ← використовується для PTS/DTS синхронізації
[6]  Reserved     = 0b111111 ← завжди 0x3F
[9]  PCR_extension @ 27 MHz  ← точна підгонка (1 base tick = 300 ext ticks)

Приклад значення:
pcr = 0x123456789ABC (48 біт)
→ base = 0x123456789ABC >> 15 = 0x2468ACF13 (33 біти)
→ ext  = 0x123456789ABC & 0x1FF = 0x1BC (9 біт)
```

> 💡 **Важливо**: `NextBytesNoCopy` повертає посилання на внутрішній буфер ітератора — економить пам'ять, але дані можуть бути перезаписані при наступному читанні.

---

## ✏️ Серіалізація: `writePacket` та допоміжні функції

### `writePacket` — збірка пакету у біти

```go
func writePacket(w *astikit.BitsWriter, p *Packet, targetPacketSize int) (int, error) {
    written := 0
    
    // 1. Sync byte
    w.Write(uint8(syncByte))  // 0x47
    written++
    
    // 2. Заголовок
    n, _ := writePacketHeader(w, p.Header)
    written += n
    
    // 3. Адаптаційне поле (якщо є)
    if p.Header.HasAdaptationField {
        n, _ = writePacketAdaptationField(w, p.AdaptationField)
        written += n
    }
    
    // 4. Перевірка місця для payload
    if targetPacketSize-written < len(p.Payload) {
        return 0, fmt.Errorf("payload too large")
    }
    
    // 5. Payload
    if p.Header.HasPayload {
        w.Write(p.Payload)
        written += len(p.Payload)
    }
    
    // 6. Stuffing до targetPacketSize (заповнення 0xFF)
    for written < targetPacketSize {
        w.Write(uint8(0xff))
        written++
    }
    
    return written, nil
}
```

### `writePacketHeader` — бітова серіалізація заголовка

```go
func writePacketHeader(w *astikit.BitsWriter, h PacketHeader) (int, error) {
    b := astikit.NewBitsWriterBatch(w)  // batch для ефективності
    
    // Байт 0 (після sync)
    b.Write(h.TransportErrorIndicator)    // bit 7
    b.Write(h.PayloadUnitStartIndicator)  // bit 6
    b.Write(h.TransportPriority)          // bit 5
    b.WriteN(h.PID, 13)                   // біти 4-0 + весь байт 1
    
    // Байт 2
    b.WriteN(h.TransportScramblingControl, 2)  // bits 7-6
    b.Write(h.HasAdaptationField)              // bit 5 (AF control high)
    b.Write(h.HasPayload)                      // bit 4 (AF control low)
    b.WriteN(h.ContinuityCounter, 4)           // bits 3-0
    
    return 3, b.Err()  // завжди 3 байти
}
```

> 💡 **Патерн**: `astikit.BitsWriterBatch` дозволяє писати окремі біти та перевіряти помилки в кінці, замість перевірки після кожного `Write()`.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Витягування PCR для синхронізації

```go
// У segmentAssembler — збереження еталонного часу:
func extractPCRFromPacket(pkt *astits.Packet) (*astits.ClockReference, bool) {
    if pkt.AdaptationField != nil && pkt.AdaptationField.HasPCR {
        return pkt.AdaptationField.PCR, true
    }
    return nil, false
}

// Використання для нормалізації PTS:
func normalizePTSWithPCR(rawPTS int64, pcr *astits.ClockReference, basePCR *astits.ClockReference) int64 {
    if pcr == nil || basePCR == nil {
        return rawPTS  // fallback без корекції
    }
    
    // Розрахувати дрейф у наносекундах
    pcrDuration := pcr.Duration()
    baseDuration := basePCR.Duration()
    drift := pcrDuration - baseDuration
    
    // Перевести дрейф у 90 kHz ticks і скоригувати PTS
    driftTicks := drift.Nanoseconds() * 90000 / 1e9
    return rawPTS + driftTicks
}
```

### ✅ 2. Детекція ключових кадрів через PUSI + RandomAccessIndicator

```go
// У segmentAssembler — визначення початку нового сегмента:
func isKeyFrameStart(pkt *astits.Packet) bool {
    // Умови для відео-ключового кадру:
    // 1. Прапорець початку одиниці (PES/PSI починається)
    // 2. Індикатор довільного доступу (можна декодувати з цього місця)
    // 3. Наявність payload (дані, а не тільки заголовки)
    
    return pkt.Header.PayloadUnitStartIndicator &&
           pkt.AdaptationField != nil &&
           pkt.AdaptationField.RandomAccessIndicator &&
           pkt.Header.HasPayload
}

// Використання:
if isVideoPID(pkt.Header.PID) && isKeyFrameStart(pkt) {
    if assembler.shouldStartNewSegment() {
        assembler.FinalizeCurrentSegment()
        assembler.StartNewSegment()
    }
}
```

### ✅ 3. Обробка discontinuity для HLS-плейлиста

```go
// У VideoManifestProxy — вставка #EXT-X-DISCONTINUITY:
func handleDiscontinuity(pkt *astits.Packet, playlist *HLSPlaylist) {
    if pkt.AdaptationField != nil && pkt.AdaptationField.DiscontinuityIndicator {
        log.Infof("Discontinuity signaled at PID %d", pkt.Header.PID)
        
        // Вставити маркер у плейлист
        playlist.AddDiscontinuity()
        
        // Скинути стан синхронізації для цього потоку
        syncState[pkt.Header.PID].Reset()
    }
}
```

### ✅ 4. Моніторинг якості потоку через заголовок

```go
// monitoring.Monitor — метрики з полів заголовка:
type PacketMetrics struct {
    TransportErrors    *prometheus.CounterVec  // пакети з помилками FEC
    ScrambledPackets   *prometheus.CounterVec  // зашифровані пакети
    PriorityPackets    *prometheus.CounterVec  // пакети з високим пріоритетом
    ContinuityGaps     *prometheus.HistogramVec  // розмір пропусків у CC
}

// У обробці пакета:
if pkt.Header.TransportErrorIndicator {
    metrics.TransportErrors.WithLabelValues(channelID).Inc()
    // Опція: відкинути пошкоджений пакет
    return errSkippedPacket
}

if pkt.Header.TransportScramblingControl != ScramblingControlNotScrambled {
    metrics.ScrambledPackets.WithLabelValues(channelID).Inc()
    log.Warnf("Encrypted packet detected (control=%d)", 
        pkt.Header.TransportScramblingControl)
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на парсинг PCR з відомими значеннями

```go
func TestParsePCR_KnownValues(t *testing.T) {
    // Відоме значення: base=90000 (1 секунда @ 90kHz), ext=300 (1 tick @ 27MHz)
    // PCR = (90000 << 15) | (0x3F << 9) | 300
    pcrValue := (uint64(90000) << 15) | (uint64(0x3F) << 9) | uint64(300)
    
    // Створити 6 байт у big-endian форматі
    bs := make([]byte, 6)
    for i := 0; i < 6; i++ {
        bs[i] = byte(pcrValue >> (40 - i*8))
    }
    
    // Парсити
    cr, err := parsePCR(astikit.NewBytesIterator(bs))
    assert.NoError(t, err)
    assert.Equal(t, int64(90000), cr.Base)
    assert.Equal(t, int64(300), cr.Extension)
    
    // Перевірити Duration()
    expected := time.Second + 300*time.Nanosecond*1e9/27e6  // ~1.000011111 сек
    assert.InDelta(t, expected.Nanoseconds(), cr.Duration().Nanoseconds(), 100)
}
```

### 🔹 Тест на обробку пакетів різного розміру

```go
func TestParsePacket_VariableSizes(t *testing.T) {
    sizes := []int{188, 192, 204}
    
    for _, size := range sizes {
        t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
            // Створити пакет з потрібним розміром
            buf := make([]byte, size)
            buf[0] = 0x47  // sync byte
            // ... заповнити заголовок та payload ...
            
            // Парсити
            pkt, err := parsePacket(astikit.NewBytesIterator(buf), nil)
            assert.NoError(t, err)
            assert.NotNil(t, pkt.Header)
            
            // Перевірити, що payload має очікуваний розмір
            expectedPayloadSize := size - 4  // мінус sync + header
            if pkt.Header.HasAdaptationField {
                expectedPayloadSize -= (1 + pkt.AdaptationField.Length)
            }
            assert.Equal(t, expectedPayloadSize, len(pkt.Payload))
        })
    }
}
```

### 🔹 Бенчмарк на парсинг заголовка

```go
func BenchmarkParsePacketHeader(b *testing.B) {
    // Підготувати 3 байти заголовка
    headerBytes := []byte{0x47, 0x10, 0x10}  // приклад: PID=16, CC=0, PUSI=1
    
    b.ResetTimer()
    b.ReportAllocs()
    
    for i := 0; i < b.N; i++ {
        iter := astikit.NewBytesIterator(headerBytes)
        _, err := parsePacketHeader(iter)
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

**Очікуваний результат:**
```
BenchmarkParsePacketHeader-8    50000000    25 ns/op    0 B/op    0 allocs/op
```
→ **0 алокацій** завдяки `NextBytesNoCopy` та відсутності створення проміжних структур.

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Неправильний порядок бітів у заголовку | Помилковий PID або CC | Перевірити бітові маски: `bs[0]&0x1f` для старших біт PID, `bs[1]` для молодших |
| Переповнення при розрахунку PCR | `pcr >> 15` дає невірний base для великих значень | Використовувати `uint64` для проміжних обчислень, як у коді ✅ |
| `NextBytesNoCopy` перезаписує дані | Дані payload змінюються після парсингу | Копіювати `p.Payload = append([]byte(nil), i.Dump()...)` якщо потрібно зберегти |
| Stuffing не заповнює до 188 байт | Вихідний пакет < 188 байт | Перевірити цикл `for written < targetPacketSize` у `writePacket` |
| Адаптаційне поле з неправильною довжиною | `Length` > доступне місце | Додати валідацію: `if a.Length > 183 { return error }` |

### Приклад безпечного копіювання payload:

```go
// Якщо потрібно зберегти payload поза функцією парсингу:
if p.Header.HasPayload {
    offset := payloadOffset(offsetStart, p.Header, p.AdaptationField)
    i.Seek(offset)
    // ❌ Небезпечно: p.Payload = i.Dump()  // посилання на внутрішній буфер
    // ✅ Безпечно:
    raw := i.Dump()
    p.Payload = make([]byte, len(raw))
    copy(p.Payload, raw)  // глибока копія
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Парсинг пакета з обробкою помилок:
func safeParsePacket(data []byte) (*astits.Packet, error) {
    iter := astikit.NewBytesIterator(data)
    pkt, err := parsePacket(iter, nil)
    if err != nil {
        if errors.Is(err, errSkippedPacket) {
            return nil, nil  // не помилка, просто пропущено
        }
        return nil, fmt.Errorf("parse failed: %w", err)
    }
    return pkt, nil
}

// 2. Серіалізація з валідацією розміру:
func safeWritePacket(pkt *astits.Packet, size int) ([]byte, error) {
    buf := &bytes.Buffer{}
    buf.Grow(size)  // попереднє виділення
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    n, err := writePacket(w, pkt, size)
    if err != nil {
        return nil, err
    }
    if n != size {
        return nil, fmt.Errorf("written %d != expected %d", n, size)
    }
    return buf.Bytes(), nil
}

// 3. Витягування ключових полів для вашого пайплайну:
type PacketInfo struct {
    PID        uint16
    CC         uint8
    PUSI       bool
    PCR        *astits.ClockReference
    Discontinuity bool
    PayloadSize int
}

func extractPacketInfo(pkt *astits.Packet) PacketInfo {
    info := PacketInfo{
        PID:        pkt.Header.PID,
        CC:         pkt.Header.ContinuityCounter,
        PUSI:       pkt.Header.PayloadUnitStartIndicator,
        PayloadSize: len(pkt.Payload),
    }
    if pkt.AdaptationField != nil {
        info.PCR = pkt.AdaptationField.PCR
        info.Discontinuity = pkt.AdaptationField.DiscontinuityIndicator
    }
    return info
}
```

---

## 📊 Матриця полів заголовка та їх значення

```
Поле                     | Біти | Діапазон   | Використання у вашому пайплайні
─────────────────────────┼──────┼────────────┼────────────────────────────────
TransportErrorIndicator  | 1    | 0/1        | Детекція пошкоджених пакетів → логування/відкидання
PayloadUnitStartIndicator| 1    | 0/1        | ✅ Детекція початку PES/PSI → сегментація
TransportPriority        | 1    | 0/1        | Рідко використовується, можна ігнорувати
PID                      | 13   | 0-8191     | ✅ Маршрутизація: відео/аудіо/метадані
TransportScramblingControl| 2   | 0-3        | Детекція зашифрованих потоків → сповіщення
AdaptationFieldControl   | 2    | 0-3        | ✅ Визначення структури пакету (AF/payload/both)
ContinuityCounter        | 4    | 0-15       | ✅ Детекція втрат/дублікатів → синхронізація
```

---

## 📚 Корисні посилання

- [MPEG-TS packet structure (Wikipedia)](https://en.wikipedia.org/wiki/MPEG_transport_stream#Packet)
- [ISO/IEC 13818-1 specification](https://www.iso.org/standard/61236.html)
- [astikit BitsWriter documentation](https://pkg.go.dev/github.com/asticode/go-astikit#BitsWriter)
- [astits source: packet.go](https://github.com/asticode/go-astits/blob/master/packet.go)

> 💡 **Ключова ідея**: Цей файл — "бітова мова" MPEG-TS. Кожен `& 0x40`, `>> 15`, `| uint16(bs[1])` — це пряме відображення специфікації стандарту. Розуміння цієї логіки дозволяє:
> - 🎯 Точно витягувати критичні поля (PCR, PUSI, CC) для синхронізації
> - 🛠️ Модифікувати пакети "на льоту" (напр., оновлювати CC при ремуксингу)
> - 🔍 Діагностувати проблеми потоку через аналіз заголовків

Якщо потрібно — можу допомогти:
- 🔄 Реалізувати модифікацію заголовків (напр., оновлення Continuity Counter при ре-пакуванні)
- 🧩 Додати підтримку розширених полів адаптації для seamless splicing
- 🧪 Написати fuzz-тест для стійкості парсингу до пошкоджених заголовків

🛠️