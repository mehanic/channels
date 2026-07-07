# Глибоке роз'яснення: Тест-сьют `packet_test.go` для astits

Цей файл містить **комплексні юніт-тести та бенчмарки** для низькорівневої обробки MPEG-TS пакетів: парсинг, серіалізація, робота з заголовками, адаптаційними полями та PCR. Це фундамент для надійності будь-якого TS-процесора.

---

## 🎯 Архітектура файлу

```
┌─────────────────────────────────────────┐
│ Структура тест-сьюту:                   │
│                                         │
│ 📦 Допоміжні функції (генератори):     │
│   • packet() / packetShort()            │
│   • packetHeaderBytes()                 │
│   • packetAdaptationFieldBytes()        │
│   • pcrBytes()                          │
│                                         │
│ 🧪 Тести функціональності:             │
│   • TestParsePacket                     │
│   • TestPayloadOffset                   │
│   • TestWritePacket (+ HeaderOnly)      │
│   • TestParse/WritePacketHeader         │
│   • TestParse/WritePacketAdaptationField│
│   • TestParse/WritePCR                  │
│                                         │
│ ⚡ Бенчмарки продуктивності:            │
│   • BenchmarkWritePCR                   │
│   • BenchmarkParsePacket                │
└─────────────────────────────────────────┘
```

---

## 🔧 Допоміжні функції: генерація тестових даних

### `packet()` — створення повного пакета з адаптаційним полем

```go
func packet(h PacketHeader, a PacketAdaptationField, i []byte, packet192bytes bool) ([]byte, *Packet) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.Write(uint8(syncByte))  // ✅ 0x47 — обов'язковий синхробайт
    
    if packet192bytes {
        w.Write([]byte("test"))  // 🎯 Тестування нестандартного розміру (192B замість 188B)
    }
    
    w.Write(packetHeaderBytes(h, "11"))              // Заголовок (4 байти)
    w.Write(packetAdaptationFieldBytes(a))           // Адаптаційне поле (змінна довжина)
    
    // 🔹 Формування payload з padding до 188/192 байт
    var payload = append(i, bytes.Repeat([]byte{0}, 147-len(i))...)
    w.Write(payload)
    
    return buf.Bytes(), &Packet{...}  // Повертає бінар + очікувану структуру
}
```

**Чому це важливо:**
```
┌─────────────────────────────────────────┐
│ Реальні потоки містять:                 │
│ • Пакети різної довжини (188/192/204B)  │
│ • Пусті payload (тільки адаптаційне поле)│
│ • Часткові дані, що потребують padding │
│                                         │
│ Генератор імітує ВСІ ці випадки →      │
│ тести покривають реальні сценарії      │
└─────────────────────────────────────────┘
```

### `packetHeaderBytes()` — бітова серіалізація заголовка

```go
func packetHeaderBytes(h PacketHeader, afControl string) []byte {
    // TS заголовок = 4 байти = 32 біти
    // Структура (MSB→LSB):
    // [8] Sync byte (0x47)
    // [1] Transport error indicator
    // [1] Payload unit start indicator  
    // [1] Transport priority
    // [13] PID
    // [2] Transport scrambling control
    // [2] Adaptation field control ← afControl ("11" = both AF+payload)
    // [4] Continuity counter
    
    w.Write(h.TransportErrorIndicator)           // 1 bit
    w.Write(h.PayloadUnitStartIndicator)         // 1 bit
    w.Write("1")                                  // 1 bit (priority)
    w.Write(fmt.Sprintf("%.13b", h.PID))         // 13 bits (PID 0..8191)
    w.Write("10")                                // 2 bits (scrambling)
    w.Write(afControl)                           // 2 bits ("00"=reserved, "01"=payload, "10"=AF, "11"=both)
    w.Write(fmt.Sprintf("%.4b", h.ContinuityCounter)) // 4 bits (0..15)
    
    return buf.Bytes()  // 3 байти (після sync byte)
}
```

> 💡 **Ключовий момент**: `astikit.BitsWriter` дозволяє писати окремі біти — критично для протоколів з бітово-орієнтованою структурою.

---

## 🧪 Розбір ключових тестів

### ✅ `TestParsePacket` — валідація парсингу

```go
func TestParsePacket(t *testing.T) {
    // 1️⃣ Негативний тест: пакет без синхробайта
    buf := &bytes.Buffer{}
    w.Write(uint16(1))  // ❌ Не 0x47!
    _, err := parsePacket(astikit.NewBytesIterator(buf.Bytes()), nil)
    assert.EqualError(t, err, ErrPacketMustStartWithASyncByte.Error())
    
    // 2️⃣ Позитивний тест: повний пакет
    b, ep := packet(packetHeader, *packetAdaptationField, []byte("payload"), true)
    p, err := parsePacket(astikit.NewBytesIterator(b), nil)
    assert.NoError(t, err)
    assert.Equal(t, p, ep)  // 🎯 Порівняння структур, не байтів!
    
    // 3️⃣ Тест фільтрації (skip callback)
    _, err = parsePacket(astikit.NewBytesIterator(b), func(p *Packet) bool { return true })
    assert.EqualError(t, err, errSkippedPacket.Error())
}
```

**Чому порівнюємо структури, а не байти?**
```
• Байти можуть відрізнятися через stuffing bytes (0xFF)
• Структурна рівність гарантує семантичну коректність
• Це дозволяє змінювати реалізацію серіалізації без ламання тестів
```

### ✅ `TestPayloadOffset` — розрахунок початку корисного навантаження

```go
func TestPayloadOffset(t *testing.T) {
    // Випадок 1: тільки payload, без адаптаційного поля
    // Заголовок = 4 байти, sync = 1 → offset = 1+3 = 4? Ні!
    // Насправді: 1 (sync) + 3 (header після sync) = 4, але функція повертає 3
    // → тому що offset рахується ВІД початку після sync byte
    assert.Equal(t, 3, payloadOffset(0, PacketHeader{}, nil))
    
    // Випадок 2: є адаптаційне поле довжиною 2 байти
    // offset = 3 (header) + 1 (AF length byte) + 2 (AF content) = 6? 
    // Але функція повертає 7 → тому що додається ще 1 байт для чогось...
    // 🎯 Важливо: перевіряти реалізацію payloadOffset() у коді!
    assert.Equal(t, 7, payloadOffset(1, PacketHeader{HasAdaptationField: true}, &PacketAdaptationField{Length: 2}))
}
```

> ⚠️ **Пастка**: `payloadOffset` — критична функція для коректного витягування PES/PSI даних. Помилка на 1 байт = зсув всього потоку.

### ✅ `TestWritePacket` — round-trip валідація

```go
func TestWritePacket(t *testing.T) {
    // 1. Створити очікуваний бінарний пакет
    eb, ep := packet(packetHeader, *packetAdaptationField, []byte("payload"), false)
    
    // 2. Серіалізувати структуру через writePacket()
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    n, err := writePacket(w, ep, MpegTsPacketSize)
    
    // 3. Перевірити:
    assert.NoError(t, err)
    assert.Equal(t, MpegTsPacketSize, n)      // ✅ Рівно 188 байт
    assert.Equal(t, n, buf.Len())             // ✅ Буфер заповнено повністю
    assert.Equal(t, len(eb), buf.Len())       // ✅ Довжина збігається
    assert.Equal(t, eb, buf.Bytes())          // ✅ Бінарна ідентичність
}
```

**Round-trip патерн:**
```
Структура → writePacket() → []byte → parsePacket() → Структура
                              ↓
                    [ байтова ідентичність ]
```

### ✅ `TestWritePacket_HeaderOnly` — edge case: пакет без payload

```go
func TestWritePacket_HeaderOnly(t *testing.T) {
    shortPacketHeader := packetHeader
    shortPacketHeader.HasPayload = false        // ❌ Немає корисного навантаження
    shortPacketHeader.HasAdaptationField = false // ❌ Немає адаптаційного поля
    
    _, ep := packetShort(shortPacketHeader, nil)
    
    // Серіалізувати
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    n, err := writePacket(w, ep, MpegTsPacketSize)
    
    // Перевірити розмір
    assert.Equal(t, MpegTsPacketSize, n)  // ✅ Все одно 188 байт (з padding)
    
    // 🔁 Round-trip перевірка: записали → прочитали → порівняли структури
    i := astikit.NewBytesIterator(buf.Bytes())
    p, err := parsePacket(i, nil)
    assert.NoError(t, err)
    assert.Equal(t, ep, p)  // ✅ Структури ідентичні
}
```

> 💡 **Важливо**: Навіть "порожній" пакет має бути валідним 188-байтовим блоком із правильним заголовком.

---

## 🔬 Тести адаптаційного поля та PCR

### `TestParse/WritePacketAdaptationField` — складна бітова структура

Адаптаційне поле має **змінну довжину** та **умовні поля**:

```
Adaptation Field Structure:
┌─────────────────────────┐
│ [8]  Length             │ ← загальна довжина цього поля
├─────────────────────────┤
│ [1]  Discontinuity      │ ← якщо 1: розрив у continuity counter
│ [1]  Random access      │ ← якщо 1: точка входу для декодера
│ [1]  ES priority        │
│ [1]  PCR flag           │ ← якщо 1: далі йде 6-байтний PCR
│ [1]  OPCR flag          │
│ [1]  Splicing point     │
│ [1]  Transport private  │
│ [1]  Extension flag     │ ← якщо 1: далі йде adaptation extension
├─────────────────────────┤
│ [48] PCR (optional)     │ ← 33-bit base + 6-bit reserved + 9-bit ext
│ [48] OPCR (optional)    │
│ [8]  Splice countdown   │
│ [8]  Private data len   │
│ [N]  Private data       │
├─────────────────────────┤
│ [8]  Extension length   │ ← якщо extension flag=1
│ [1]  LTW flag           │
│ [1]  Piecewise rate     │
│ [1]  Seamless splice    │
│ [5]  Reserved           │
│ [1]  LTW valid          │
│ [15] LTW offset         │
│ [2]  Reserved           │
│ [23] Piecewise rate     │
│ [4]  Splice type        │
│ [33] DTS next access    │
├─────────────────────────┤
│ [N]  Stuffing bytes     │ ← 0xFF для вирівнювання
└─────────────────────────┘
```

**Тест перевіряє:**
```go
// 1. Створити складне адаптаційне поле з ВСІМА прапорцями
var packetAdaptationField = &PacketAdaptationField{
    HasPCR: true, HasOPCR: true, HasAdaptationExtensionField: true,
    PCR: &ClockReference{Base: 5726623061, Extension: 341},
    // ... ще 15+ полів ...
}

// 2. Серіалізувати у біти через packetAdaptationFieldBytes()
eb := packetAdaptationFieldBytes(*packetAdaptationField)

// 3. Парсити назад
v, err := parsePacketAdaptationField(astikit.NewBytesIterator(eb))

// 4. Порівняти структури (включаючи вкладені ClockReference)
assert.Equal(t, packetAdaptationField, v)
```

### `TestParse/WritePCR` — критично для синхронізації

```go
var pcr = &ClockReference{
    Base:      5726623061,  // 33 біти @ 90 kHz
    Extension: 341,         // 9 біт @ 27 MHz
}

func pcrBytes() []byte {
    // PCR = 6 байт = 48 біт:
    // [33] Base (MSB first)
    // [6]  Reserved (0b111111)
    // [9]  Extension
    
    w.Write("101010101010101010101010101010101")  // 33 біти base
    w.Write("111111")                             // 6 біт reserved
    w.Write("101010101")                          // 9 біт extension
    return buf.Bytes()  // 6 байт
}

func TestWritePCR(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    bytesWritten, err := writePCR(w, pcr)
    
    assert.NoError(t, err)
    assert.Equal(t, 6, bytesWritten)  // ✅ Рівно 6 байт
    assert.Equal(t, pcrBytes(), buf.Bytes())  // ✅ Бінарна ідентичність
}
```

> 💡 **Порада**: Тестуйте крайні значення: `Base=0`, `Base=(1<<33)-1`, `Extension=0`, `Extension=299`.

---

## ⚡ Бенчмарки: вимірювання продуктивності

### `BenchmarkWritePCR`

```go
func BenchmarkWritePCR(b *testing.B) {
    buf := &bytes.Buffer{}
    buf.Grow(6)  // ✅ Попереднє виділення пам'яті
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})

    b.ReportAllocs()  // 📊 Звітувати про алокації
    for i := 0; i < b.N; i++ {
        buf.Reset()  // ✅ Очищення без deallocation
        writePCR(w, pcr)
    }
}
```

**Запуск:**
```bash
go test -bench=BenchmarkWritePCR -benchmem
```

**Очікуваний результат:**
```
BenchmarkWritePCR-8    2500000    480 ns/op    0 B/op    0 allocs/op
```
→ **0 алокацій** завдяки `buf.Grow()` + `buf.Reset()` + `BitsWriter`.

### `BenchmarkParsePacket`

```go
func BenchmarkParsePacket(b *testing.B) {
    // Підготувати реальний пакет для парсингу
    bs, _ := packet(packetHeader, *packetAdaptationField, []byte("payload"), true)

    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        parsePacket(astikit.NewBytesIterator(bs), nil)
    }
}
```

**Метрики для аналізу:**
| Метрика | Ідеальне значення | Що означає відхилення |
|---------|-------------------|----------------------|
| `ns/op` | < 1000 ns         | Повільний парсинг → оптимізувати битові операції |
| `B/op`  | < 100 B           | Зайві алокації → використовувати bytesPool |
| `allocs/op` | 0-2          | Кожна алокація = тиск на GC |

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Додати тести для вашого `createTSSegment`

```go
// segmentFinalizer_test.go
func TestCreateTSSegment_RoundTrip(t *testing.T) {
    // Підготувати тестові дані
    video := generateH264NALUs(10)
    audio := generateAACFrames(20)
    
    // Створити сегмент через ваш пайплайн
    tsData, err := createTSSegment(video, audio, 123)
    require.NoError(t, err)
    
    // 🔁 Round-trip перевірка як у astits
    for i := 0; i < len(tsData); i += 188 {
        pktData := tsData[i : i+188]
        if pktData[0] != 0x47 {
            t.Errorf("Missing sync byte at offset %d", i)
            continue
        }
        
        // Парсити через astits
        pkt, err := astits.ParsePacket(astikit.NewBytesIterator(pktData), nil)
        if err != nil {
            t.Errorf("Failed to parse packet at offset %d: %v", i, err)
            continue
        }
        
        // Перевірити ключові поля
        if pkt.Header.PID == expectedVideoPID {
            assert.True(t, pkt.Header.PayloadUnitStartIndicator, 
                "Video PES should start with PUSI=1")
        }
    }
}
```

### ✅ 2. Тести на edge cases для orphan audio merge

```go
func TestPacket_AudioWithoutVideo(t *testing.T) {
    // Сценарій: аудіо-пакет без відповідного відео (orphan)
    header := PacketHeader{
        PID: audioPID,
        PayloadUnitStartIndicator: true,
        HasPayload: true,
        HasAdaptationField: false,
        ContinuityCounter: 5,
    }
    
    // PES з аудіо-даними
    pesData := buildAACPesHeader(pts=90000*4) // 4 секунди
    payload := append(pesData, generateAACFrame()...)
    
    bin, expected := packet(header, PacketAdaptationField{}, payload, false)
    
    // Парсити і перевірити
    pkt, err := parsePacket(astikit.NewBytesIterator(bin), nil)
    assert.NoError(t, err)
    assert.Equal(t, expected.Header.PID, pkt.Header.PID)
    assert.True(t, pkt.Header.PayloadUnitStartIndicator)
}
```

### ✅ 3. Бенчмарк для вашого `segmentAssembler`

```go
func BenchmarkSegmentAssembler_Merge(b *testing.B) {
    // Підготувати 10 секунд відео + аудіо
    videoChunks := generateVideoChunks(10 * 25)  // 25 fps
    audioChunks := generateAudioChunks(10 * 50)  // 50 Hz AAC
    
    assembler := NewAssembler(config)
    
    b.ResetTimer()
    b.ReportAllocs()
    
    for i := 0; i < b.N; i++ {
        assembler.Reset()
        for j := 0; j < len(videoChunks); j++ {
            assembler.AddVideo(videoChunks[j])
            if j%2 == 0 {
                assembler.AddAudio(audioChunks[j/2])
            }
        }
        _, _ = assembler.Finalize()
    }
}
```

---

## 🐛 Поширені проблеми, які виявляють ці тести

| Проблема | Тест, що ловить | Як виправити |
|----------|----------------|--------------|
| Неправильний порядок бітів у заголовку | `TestParsePacketHeader` | Перевірити `fmt.Sprintf("%.13b", pid)` — MSB first |
| Padding не заповнює до 188 байт | `TestWritePacket` | Додати `bytes.Repeat([]byte{0xFF}, remaining)` |
| PCR записується з неправильним reserved | `TestWritePCR` | Завжди писати `0b111111` у 6 біт reserved |
| Адаптаційне поле ігнорує прапорці | `TestParsePacketAdaptationField` | Реалізувати умовне читання: `if af.HasPCR { readPCR() }` |
| Skip callback не викликається | `TestParsePacket` (3й кейс) | Перевірити порядок: спочатку callback, потім парсинг |

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на wrap-around Continuity Counter

```go
func TestPacket_ContinuityCounterWrap(t *testing.T) {
    baseHeader := packetHeader
    
    // Згенерувати 20 пакетів з лічильником 12→13→...→15→0→1→...
    for i := 0; i < 20; i++ {
        h := baseHeader
        h.ContinuityCounter = uint8((12 + i) % 16)
        
        bin, expected := packet(h, PacketAdaptationField{}, []byte("data"), false)
        pkt, err := parsePacket(astikit.NewBytesIterator(bin), nil)
        
        assert.NoError(t, err)
        assert.Equal(t, expected.Header.ContinuityCounter, 
                    pkt.Header.ContinuityCounter,
                    "Counter mismatch at iteration %d", i)
    }
}
```

### 🔹 Тест на різні розміри пакетів (188/192/204)

```go
func TestPacket_VariableSizes(t *testing.T) {
    sizes := []int{188, 192, 204}  // Стандартні розміри TS
    
    for _, size := range sizes {
        t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
            // Створити пакет з відповідним padding
            bin, expected := packet(packetHeader, *packetAdaptationField, 
                                   []byte("payload"), size == 192)
            
            // Перевірити довжину
            assert.Equal(t, size, len(bin))
            
            // Парсити (parsePacket очікує 188, тому для 192/204 потрібен wrapper)
            if size == 188 {
                pkt, err := parsePacket(astikit.NewBytesIterator(bin), nil)
                assert.NoError(t, err)
                assert.Equal(t, expected.Header.PID, pkt.Header.PID)
            }
        })
    }
}
```

### 🔹 Fuzz-тест для стійкості до пошкоджених даних

```go
func FuzzParsePacket(f *testing.F) {
    // Seed: додати валідні пакети
    bin, _ := packet(packetHeader, *packetAdaptationField, []byte("payload"), false)
    f.Add(bin)
    
    f.Fuzz(func(t *testing.T, data []byte) {
        // Парсити будь-які вхідні дані — не повинно панікувати
        defer func() {
            if r := recover(); r != nil {
                t.Errorf("Panic on input: %v", r)
            }
        }()
        
        _, _ = parsePacket(astikit.NewBytesIterator(data), nil)
        // Помилки — ок, паніки — ні
    })
}
```

Запуск: `go test -fuzz=FuzzParsePacket -fuzztime=30s`

---

## 📦 Швидкий чек-лист для вашого коду

```go
// ✅ При створенні тестових пакетів:
// 1. Завжди починати з 0x47 (sync byte)
// 2. Використовувати astikit.BitsWriter для бітової точності
// 3. Додавати padding до 188/192/204 байт
// 4. Повертати і бінар, і очікувану структуру для порівняння

// ✅ При парсингу:
// 1. Перевіряти sync byte одразу
// 2. Обробляти skip callback ДО важкої логіки
// 3. Використовувати bytesPool для тимчасових буферів
// 4. Повертати структурні помилки, не панікувати

// ✅ При серіалізації:
// 1. Перевіряти, що вихід = рівно MpegTsPacketSize байт
// 2. Заповнювати stuffing bytes значенням 0xFF (не 0x00!)
// 3. Дотримуватися порядку бітів (MSB first)
// 4. Тестувати round-trip: write → parse → compare structs
```

---

## 📊 Матриця покриття тестами

```
Компонент              | Тест                     | Статус
───────────────────────┼──────────────────────────┼───────
Sync byte validation   | TestParsePacket (case 1) | ✅
Header parsing         | TestParsePacketHeader    | ✅
Header writing         | TestWritePacketHeader    | ✅
Adaptation field parse | TestParsePacketAF        | ✅
Adaptation field write | TestWritePacketAF        | ✅
PCR parsing            | TestParsePCR             | ✅
PCR writing            | TestWritePCR             | ✅
Payload offset calc    | TestPayloadOffset        | ✅
Full packet round-trip | TestWritePacket          | ✅
Header-only packet     | TestWritePacket_HeaderOnly| ✅
Skip callback          | TestParsePacket (case 3) | ✅
Performance            | Benchmark*               | ✅
Variable packet size   | packet() + packet192bytes| ⚠️ Частково
Fuzz resilience        | (відсутній)              | ❌ Додати
Continuity wrap-around | (відсутній)              | ❌ Додати
```

---

## 📚 Корисні посилання

- [MPEG-TS packet structure](https://en.wikipedia.org/wiki/MPEG_transport_stream#Packet)
- [astikit BitsWriter docs](https://pkg.go.dev/github.com/asticode/go-astikit#BitsWriter)
- [Go testing/fuzzing guide](https://go.dev/doc/fuzz/)

> 💡 **Ключова ідея**: Ці тести — не просто перевірка коду, а **документація бітового формату**. Кожен `w.Write("101")` — це специфікація протоколу. Використовуйте їх як референс при реалізації власного парсингу або модифікації пакетів у вашому `segmentFinalizer`.

Якщо потрібно — можу допомогти:
- 🧩 Адаптувати ці тести під ваш формат orphan audio merge
- 🔄 Додати round-trip валідацію для вашого `createTSSegment`
- ⚡ Оптимізувати бенчмарки з урахуванням вашого pipeline

🛠️