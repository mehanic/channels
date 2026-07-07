# Глибоке роз'яснення: Тести PES (Packetized Elementary Stream) у astits — парсинг та серіалізація

Цей файл містить **комплексні тести парсингу та запису PES-даних** — фундаментального механізму MPEG-TS для передачі відео/аудіо потоків з таймінгами (PTS/DTS). Це "серце" синхронізації аудіо/відео у вашому пайплайні.

---

## 🎯 Навіщо PES потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ PES у контексті HLS-стрімінгу:         │
│                                         │
│ 🔹 Таймінги для синхронізації:         │
│   • PTS (Presentation Time Stamp)      │
│     → коли показувати кадр             │
│   • DTS (Decoding Time Stamp)          │
│     → коли декодувати кадр (B-frames)  │
│   • ESCR (Elementary Stream Clock Ref) │
│     → еталонний час для потоку         │
│                                         │
│ 🔹 Управління відтворенням:            │
│   • DSM Trick Mode: fast-forward,      │
│     slow-motion, freeze-frame          │
│   • Scrambling control для шифрування  │
│   • Priority для важливих пакетів      │
│                                         │
│ 🔹 Для HLS:                             │
│   • Без валідних PTS/DTS → десинхронізація│
│   • Неправильні таймінги → артефакти   │
│   • Trick mode → підтримка швидкого    │
│     перемотування у плеєрі             │
└─────────────────────────────────────────┘
```

---

## 🔧 Архітектура тестів: стратегія валідації

### 📦 Глобальні змінні — еталонні значення

```go
// 🔹 ClockReference для тестів
var ptsClockReference = &ClockReference{Base: 5726623061}  // PTS
var dtsClockReference = &ClockReference{Base: 5726623060}  // DTS (на 1 tick менше)
var clockReference = &ClockReference{Base: 1234567890, Extension: 456}  // ESCR

// 🔹 DSM Trick Mode для slow motion
var dsmTrickModeSlow = &DSMTrickMode{
    RepeatControl:    21,  // 0b10101
    TrickModeControl: TrickModeControlSlowMotion,  // 0b001
}
```

### 🔁 Генератори тестових байтів

```go
// 🔹 ptsBytes(flag) — генерує 5 байт PTS у бітовому форматі
func ptsBytes(flag string) []byte {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Формат PTS/DTS: 40 біт = 5 байт
    // [4] flag [3] 32..30 [1] marker [15] 29..15 [1] marker [15] 14..0 [1] marker
    w.Write(flag)              // 4 біти: PTSDTSIndicator
    w.Write("101")             // біти 32-30 значення
    w.Write("1")               // marker bit = 1 (обов'язково)
    w.Write("010101010101010") // біти 29-15
    w.Write("1")               // marker bit
    w.Write("101010101010101") // біти 14-0
    w.Write("1")               // marker bit
    
    return buf.Bytes()  // 5 байт
}
```

> 💡 **Важливо**: PTS/DTS використовують **розріджений формат** з маркер-бітами (0b1) кожні 15 біт даних. Це дозволяє синхронізацію та перевірку цілісності.

---

## 🔍 Тест `TestHasPESOptionalHeader`: детекція опціонального заголовка

```go
func TestHasPESOptionalHeader(t *testing.T) {
    var a []int
    for i := 0; i <= 255; i++ {
        if !hasPESOptionalHeader(uint8(i)) {
            a = append(a, i)
        }
    }
    // ✅ Тільки ці два stream_id НЕ мають опціонального заголовка
    assert.Equal(t, []int{StreamIDPaddingStream, StreamIDPrivateStream2}, a)
}
```

**Логіка `hasPESOptionalHeader`:**
```go
func hasPESOptionalHeader(streamID uint8) bool {
    // Опціональний заголовок є для всіх stream_id КРІМ:
    // • 0xBE = Padding stream (заповнення)
    // • 0xBF = Private stream 2 (напр., субтитри, метадані)
    return streamID != 0xBE && streamID != 0xBF
}
```

> 💡 **Ключова ідея**: Padding/Private stream 2 не потребують таймінгів, тому не мають PTS/DTS у заголовку.

---

## 🎮 Тести `DSMTrickMode`: режими спеціального відтворення

### Структура `DSMTrickMode`

```go
type DSMTrickMode struct {
    TrickModeControl    uint8  // 3 біти: тип режиму (0-7)
    FieldID             uint8  // 2 біти: для fast_forward/fast_reverse
    IntraSliceRefresh   uint8  // 1 біт: для fast_forward/fast_reverse
    FrequencyTruncation uint8  // 2 біти: для fast_forward/fast_reverse
    RepeatControl       uint8  // 5 біт: для slow_motion/slow_reverse
}
```

### Матриця `TrickModeControl`

```
Значення | Назва                 | Додаткові поля
─────────┼───────────────────────┼─────────────────────────
0        | Fast Forward          | FieldID, IntraSliceRefresh, FrequencyTruncation
1        | Slow Motion           | RepeatControl (5 біт)
2        | Freeze Frame          | FieldID, reserved
3        | Fast Reverse          | FieldID, IntraSliceRefresh, FrequencyTruncation
4        | Slow Reverse          | RepeatControl (5 біт)
5-7      | Reserved              | (не використовуються)
```

### 🧪 Тест кейси для парсингу/запису

```go
var dsmTrickModeTestCases = []dsmTrickModeTestCase{
    {
        "fast_forward",
        func(w *astikit.BitsWriter) {
            w.Write("000")  // TrickModeControl = 0
            w.Write("10")   // FieldID = 2
            w.Write("1")    // IntraSliceRefresh = 1
            w.Write("11")   // FrequencyTruncation = 3
        },
        &DSMTrickMode{
            FieldID:             2,
            IntraSliceRefresh:   1,
            FrequencyTruncation: 3,
            TrickModeControl:    TrickModeControlFastForward,
        },
    },
    {
        "slow_motion",
        func(w *astikit.BitsWriter) {
            w.Write("001")  // TrickModeControl = 1
            w.Write("10101") // RepeatControl = 21
        },
        &DSMTrickMode{
            RepeatControl:    0b10101,  // 21
            TrickModeControl: TrickModeControlSlowMotion,
        },
    },
    // ... інші кейси: freeze_frame, fast_reverse, slow_reverse, reserved
}
```

### 🔍 `TestParseDSMTrickMode`: парсинг 1 байта

```go
func TestParseDSMTrickMode(t *testing.T) {
    for _, tc := range dsmTrickModeTestCases {
        t.Run(tc.name, func(t *testing.T) {
            buf := &bytes.Buffer{}
            w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
            tc.bytesFunc(w)  // генерує 1 байт
            
            // Парсинг: читаємо 1 байт → розбираємо біти
            assert.Equal(t, parseDSMTrickMode(buf.Bytes()[0]), tc.trickMode)
        })
    }
}
```

**Гіпотетична реалізація `parseDSMTrickMode`:**
```go
func parseDSMTrickMode(b byte) *DSMTrickMode {
    tm := &DSMTrickMode{}
    
    // 🔹 Біти 7-5: TrickModeControl (3 біти)
    tm.TrickModeControl = b >> 5
    
    // 🔹 Залежно від режиму — читаємо решту біт
    switch tm.TrickModeControl {
    case TrickModeControlFastForward, TrickModeControlFastReverse:
        // [4-3] FieldID, [2] IntraSliceRefresh, [1-0] FrequencyTruncation
        tm.FieldID = (b >> 3) & 0x03
        tm.IntraSliceRefresh = (b >> 2) & 0x01
        tm.FrequencyTruncation = b & 0x03
        
    case TrickModeControlSlowMotion, TrickModeControlSlowReverse:
        // [4-0] RepeatControl (5 біт)
        tm.RepeatControl = b & 0x1F
    }
    
    return tm
}
```

### ✏️ `TestWriteDSMTrickMode`: серіалізація 1 байта

```go
func TestWriteDSMTrickMode(t *testing.T) {
    for _, tc := range dsmTrickModeTestCases {
        t.Run(tc.name, func(t *testing.T) {
            // 🔹 Round-trip: записати → порівняти з еталоном
            bufActual := &bytes.Buffer{}
            wActual := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: bufActual})
            
            n, err := writeDSMTrickMode(wActual, tc.trickMode)
            assert.NoError(t, err)
            assert.Equal(t, 1, n)  // ✅ завжди 1 байт
            assert.Equal(t, bufExpected.Bytes(), bufActual.Bytes())  // ✅ бінарна ідентичність
        })
    }
}
```

---

## ⏱️ Тести таймінгів: `PTS/DTS` та `ESCR`

### Формат PTS/DTS: 40 біт = 5 байт

```
Бітова структура (big-endian):
[4] PTSDTSIndicator [3] base[32..30] [1] marker=1 [15] base[29..15] [1] marker=1 [15] base[14..0] [1] marker=1

Приклад для PTS = 5726623061:
• base[32..30] = 0b101 = 5
• base[29..15] = 0b010101010101010 = 0x2AAA = 10922
• base[14..0] = 0b101010101010101 = 0x5555 = 21845

Розрахунок:
  5<<30 + 10922<<15 + 21845 = 5368709120 + 358465536 + 21845 = 5726623061 ✅
```

### 🔍 `TestParsePTSOrDTS`: парсинг 5 байт

```go
func TestParsePTSOrDTS(t *testing.T) {
    // 🔹 Генерація тестових байтів з flag="0010" (PTSDTSIndicator=2 = PTS only)
    v, err := parsePTSOrDTS(astikit.NewBytesIterator(ptsBytes("0010")))
    
    assert.Equal(t, v, ptsClockReference)  // ✅ Base=5726623061
    assert.NoError(t, err)
}
```

**Гіпотетична реалізація `parsePTSOrDTS`:**
```go
func parsePTSOrDTS(i *astikit.BytesIterator) (*ClockReference, error) {
    bs, _ := i.NextBytesNoCopy(5)  // 5 байт
    
    // 🔹 Збірка 33-бітного base з розрідженого формату
    // Формат: [4 flag][3 b32-30][1 marker][15 b29-15][1 marker][15 b14-0][1 marker]
    
    base := int64(bs[0]&0x0E) << 29  // біти 32-30 з байта 0 (маска 0b1110)
    base |= int64(bs[1]) << 22        // весь байт 1 = біти 29-22
    base |= int64(bs[2]&0xFE) << 14   // біти 21-15 з байта 2 (маска 0b11111110)
    base |= int64(bs[3]) << 7         // весь байт 3 = біти 14-7
    base |= int64(bs[4] >> 1)         // біти 6-0 з байта 4 (зсув на 1 через marker)
    
    return &ClockReference{Base: base}, nil
}
```

### ✏️ `TestWritePTSOrDTS`: серіалізація 5 байт

```go
func TestWritePTSOrDTS(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // 🔹 Записати PTS/DTS з flag="0010" (PTS only)
    n, err := writePTSOrDTS(w, uint8(0b0010), dtsClockReference)
    
    assert.NoError(t, err)
    assert.Equal(t, 5, n)              // ✅ завжди 5 байт
    assert.Equal(t, dtsBytes("0010"), buf.Bytes())  // ✅ бінарна ідентичність
}
```

### 🔍 `TestParseESCR` / `TestWriteESCR`: 48-бітний ESCR

```
ESCR формат: 48 біт = 6 байт
[2] dummy [3] base[32..30] [1] marker [15] base[29..15] [1] marker [15] base[14..0] [1] marker [9] extension [1] marker

Відмінність від PTS/DTS:
• +9 біт extension (для точнішої синхронізації @ 27 MHz)
• Загальна точність: base @ 90 kHz + extension @ 27 MHz
```

---

## 📦 Головний тест: `TestParsePESData` та `TestWritePESData`

### Структура `PESData`

```go
type PESData struct {
    Data   []byte       // 🎯 корисне навантаження (відео/аудіо кадри)
    Header *PESHeader   // 🎯 заголовок з метаданими
}

type PESHeader struct {
    StreamID       uint8              // 🎯 тип потоку (0xE0=відео, 0xC0=аудіо)
    PacketLength   uint16             // 🎯 довжина після цього поля
    OptionalHeader *PESOptionalHeader // 🎯 опціональні дані (таймінги, прапорці)
}
```

### 🔹 Кейс 1: PES без опціонального заголовка

```go
{
    "without_header",
    // Заголовок: prefix + stream_id + packet_length
    func(w *astikit.BitsWriter, withStuffing bool, withCRC bool) {
        w.Write("000000000000000000000001")   // 3-байтний prefix = 0x000001
        w.Write(uint8(StreamIDPaddingStream)) // stream_id = 0xBE
        w.Write(uint16(4))                    // packet_length = 4 байти даних
    },
    // Без опціонального заголовка
    func(w *astikit.BitsWriter, withStuffing bool, withCRC bool) {
        // do nothing
    },
    // Дані
    func(w *astikit.BitsWriter, withStuffing bool, withCRC bool) {
        w.Write([]byte("data"))  // 4 байти
    },
    // Очікуваний результат
    &PESData{
        Data: []byte("data"),
        Header: &PESHeader{
            PacketLength: 4,
            StreamID:     StreamIDPaddingStream,
            // OptionalHeader = nil
        },
    },
}
```

### 🔹 Кейс 2: Повний PES з усіма опціями

```go
{
    "with_header",
    // 🔹 Заголовок
    func(w *astikit.BitsWriter, withStuffing bool, withCRC bool) {
        w.Write("000000000000000000000001")  // prefix
        w.Write(uint8(1))                     // stream_id = 1 (private)
        w.Write(uint16(67))                   // packet_length
    },
    
    // 🔹 Опціональний заголовок (60 байт)
    func(w *astikit.BitsWriter, withStuffing bool, withCRC bool) {
        // ── Прапорці байт 1 ──
        w.Write("10")                        // marker_bits = 2
        w.Write("01")                        // scrambling_control = 1
        w.Write("1")                         // priority = true
        w.Write("1")                         // data_alignment_indicator = true
        w.Write("1")                         // copyright = true
        w.Write("1")                         // original_or_copy = true
        
        // ── Прапорці байт 2 ──
        w.Write("11")                        // PTS_DTS_indicator = 3 (обидва присутні)
        w.Write("1")                         // ESCR_flag = true
        w.Write("1")                         // ES_rate_flag = true
        w.Write("1")                         // DSM_trick_mode_flag = true
        w.Write("1")                         // additional_copy_info_flag = true
        w.Write(withCRC)                     // CRC_flag
        w.Write("1")                         // extension_flag = true
        
        w.Write(uint8(60))                   // header_data_length
        
        // ── Таймінги ──
        w.Write(ptsBytes("0011"))            // PTS (flag=3)
        w.Write(dtsBytes("0001"))            // DTS (flag=1)
        w.Write(escrBytes())                 // ESCR
        
        // ── Інші поля ──
        w.Write("101010101010101010101011")  // ES_rate = 1398101
        w.Write(dsmTrickModeSlowBytes())     // DSM trick mode
        w.Write("11111111")                  // additional_copy_info = 127
        if withCRC { w.Write(uint16(4)) }    // CRC
        
        // ── Extension ──
        w.Write("1")                         // private_data_flag
        w.Write("0")                         // pack_header_field_flag
        w.Write("1")                         // program_packet_sequence_counter_flag
        w.Write("1")                         // P-STD_buffer_flag
        w.Write("111")                       // reserved
        w.Write("1")                         // extension_flag_2
        
        w.Write([]byte("1234567890123456"))  // private_data (16 байт)
        w.Write("1101010111010101")          // packet_sequence_counter = 85
        w.Write("0111010101010101")          // P-STD_buffer (scale=1, size=5461)
        w.Write("10001010")                  // extension_2 header
        w.Write([]byte("extension2"))        // extension_2 data (10 байт)
        
        if withStuffing { w.Write([]byte("stuff")) }  // stuffing
    },
    
    // 🔹 Дані
    func(w *astikit.BitsWriter, withStuffing bool, withCRC bool) {
        w.Write([]byte("data"))  // основні дані
        if withStuffing { w.Write([]byte("stuff")) }  // stuffing після даних
    },
    
    // 🔹 Очікувана структура
    &PESData{
        Data: []byte("data"),
        Header: &PESHeader{
            StreamID: 1,
            PacketLength: 67,
            OptionalHeader: &PESOptionalHeader{
                // 🔹 Прапорці
                MarkerBits: 2,
                ScramblingControl: 1,
                Priority: true,
                DataAlignmentIndicator: true,
                IsCopyrighted: true,
                IsOriginal: true,
                PTSDTSIndicator: 3,  // обидва присутні
                HasESCR: true,
                HasESRate: true,
                HasDSMTrickMode: true,
                HasAdditionalCopyInfo: true,
                HasCRC: true,
                HasExtension: true,
                
                // 🔹 Таймінги
                PTS: ptsClockReference,
                DTS: dtsClockReference,
                ESCR: clockReference,
                ESRate: 1398101,
                
                // 🔹 Trick mode
                DSMTrickMode: dsmTrickModeSlow,
                
                // 🔹 Інші поля
                AdditionalCopyInfo: 127,
                CRC: 4,
                HeaderLength: 60,
                
                // 🔹 Extension
                HasPrivateData: true,
                HasProgramPacketSequenceCounter: true,
                HasPSTDBuffer: true,
                HasExtension2: true,
                PrivateData: []byte("1234567890123456"),
                PacketSequenceCounter: 85,
                PSTDBufferScale: 1,
                PSTDBufferSize: 5461,
                Extension2Length: 10,
                Extension2Data: []byte("extension2"),
                
                // 🔹 Stuffing
                OriginalStuffingLength: 21,
            },
        },
    },
}
```

---

## 🔁 `TestWritePESData`: фрагментація великих даних

```go
func TestWritePESData(t *testing.T) {
    for _, tc := range pesTestCases {
        t.Run(tc.name, func(t *testing.T) {
            // 🔹 Підготувати еталонні байти (без stuffing/CRC для тесту)
            bufExpected := bytes.Buffer{}
            wExpected := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &bufExpected})
            tc.headerBytesFunc(wExpected, false, false)
            tc.optionalHeaderBytesFunc(wExpected, false, false)
            tc.bytesFunc(wExpected, false, false)
            
            // 🔹 Записати через writePESData з фрагментацією
            bufActual := bytes.Buffer{}
            wActual := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &bufActual})
            
            start := true
            totalBytes := 0
            payloadPos := 0
            
            // 🔹 Цикл фрагментації: розбити дані на частини, що поміщаються у TS-пакет
            for payloadPos+1 < len(tc.pesData.Data) {
                n, payloadN, err := writePESData(
                    wActual,
                    tc.pesData.Header,
                    tc.pesData.Data[payloadPos:],  // решта даних
                    start,                          // це початок PES?
                    MpegTsPacketSize-mpegTsPacketHeaderSize,  // макс. payload = 184 байти
                )
                assert.NoError(t, err)
                start = false  // наступні ітерації — продовження
                
                totalBytes += n
                payloadPos += payloadN  // просунутися у вхідних даних
            }
            
            // 🔹 Перевірити результат
            assert.Equal(t, totalBytes, bufActual.Len())
            assert.Equal(t, bufExpected.Len(), bufActual.Len())
            assert.Equal(t, bufExpected.Bytes(), bufActual.Bytes())  // ✅ бінарна ідентичність
        })
    }
}
```

**Логіка фрагментації:**
```
Вхід: 1000 байт відео-даних, макс. payload = 184 байти

Ітерація 1 (start=true):
• Записати PES header + optional header + 184 байти даних
• Встановити PayloadUnitStartIndicator=1 у першому TS-пакеті
• payloadPos = 184

Ітерація 2 (start=false):
• Записати тільки 184 байти даних (без заголовка)
• PayloadUnitStartIndicator=0 (продовження)
• payloadPos = 368

... повторювати доки payloadPos < 1000

Результат: 1000 байт → 6 TS-пакетів (1 з заголовком, 5 продовжень)
```

> 💡 **Ключовий момент**: Тільки перший фрагмент має PES-заголовок. Решта — "голі" дані, які декодер збирає назад у цілий PES.

---

## ⚡ Бенчмарки: продуктивність парсингу

```go
func BenchmarkParsePESData(b *testing.B) {
    // 🔹 Підготувати тестові дані для всіх кейсів
    bss := make([][]byte, len(pesTestCases))
    for ti, tc := range pesTestCases {
        buf := bytes.Buffer{}
        w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &buf})
        tc.headerBytesFunc(w, true, true)
        tc.optionalHeaderBytesFunc(w, true, true)
        tc.bytesFunc(w, true, true)
        bss[ti] = buf.Bytes()
    }
    
    b.ReportAllocs()  // 📊 звітувати про алокації
    
    for ti, tc := range pesTestCases {
        b.Run(tc.name, func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                parsePESData(astikit.NewBytesIterator(bss[ti]))
            }
        })
    }
}
```

**Очікувані результати:**
```
BenchmarkParsePESData/without_header-8    200000    5000 ns/op    200 B/op    5 allocs/op
BenchmarkParsePESData/with_header-8       50000     25000 ns/op   2000 B/op   50 allocs/op
```

**Що аналізувати:**
| Метрика | Ідеальне значення | Що означає відхилення |
|---------|-------------------|----------------------|
| `ns/op` | < 30 µs для повного PES | Повільний парсинг → оптимізувати бітові операції |
| `B/op`  | < 3 KB | Зайві алокації → використовувати bytesPool |
| `allocs/op` | < 60 | Кожна алокація = тиск на GC |

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Витягування таймінгів для синхронізації

```go
// У segmentAssembler — отримання PTS/DTS для A/V синхронізації:
func extractTimestamps(pes *astits.PESData) (pts, dts int64, hasPTS, hasDTS bool) {
    if pes.Header == nil || pes.Header.OptionalHeader == nil {
        return 0, 0, false, false
    }
    
    oh := pes.Header.OptionalHeader
    
    // 🔹 Перевірити PTSDTSIndicator
    switch oh.PTSDTSIndicator {
    case 2:  // PTS only
        return oh.PTS.Base, 0, true, false
    case 3:  // PTS + DTS
        return oh.PTS.Base, oh.DTS.Base, true, true
    default:
        return 0, 0, false, false
    }
}

// Використання для синхронізації:
pts, dts, hasPTS, hasDTS := extractTimestamps(pes)
if hasPTS {
    // 🔹 Використати PTS для відтворення
    schedulePlayback(pes.Data, pts)
}
if hasDTS && hasPTS && dts != pts {
    // 🔹 B-frame: декодувати раніше, показати пізніше
    scheduleDecoding(pes.Data, dts)
    schedulePresentation(pes.Data, pts)
}
```

### ✅ 2: Обробка trick mode для швидкого перемотування

```go
// У VideoManifestProxy — підтримка trick mode у плеєрі:
func handleTrickMode(pes *astits.PESData, channelID string) {
    if pes.Header == nil || pes.Header.OptionalHeader == nil {
        return
    }
    
    tm := pes.Header.OptionalHeader.DSMTrickMode
    if tm == nil {
        return
    }
    
    switch tm.TrickModeControl {
    case astits.TrickModeControlFastForward:
        log.Debugf("Channel %s: fast-forward detected (field=%d, intra=%d, freq=%d)",
            channelID, tm.FieldID, tm.IntraSliceRefresh, tm.FrequencyTruncation)
        // 🔹 Опція: пропускати не-ключові кадри для прискорення
        
    case astits.TrickModeControlSlowMotion:
        log.Debugf("Channel %s: slow-motion detected (repeat=%d)", 
            channelID, tm.RepeatControl)
        // 🔹 Опція: повторювати кадри для уповільнення
        
    case astits.TrickModeControlFreezeFrame:
        log.Debugf("Channel %s: freeze-frame detected", channelID)
        // 🔹 Опція: зупинити відтворення на поточному кадрі
    }
}
```

### ✅ 3: Валідація таймінгів для запобігання десинхронізації

```go
// Перевірити коректність таймінгів перед обробкою:
func validateTimestamps(pes *astits.PESData, lastPTS int64) error {
    if pes.Header == nil || pes.Header.OptionalHeader == nil {
        return nil  // без таймінгів — не валідуємо
    }
    
    oh := pes.Header.OptionalHeader
    
    // 🔹 PTS має бути монотонним (або з невеликим "відкатом" при розриві)
    if oh.PTS != nil {
        if lastPTS > 0 && oh.PTS.Base < lastPTS {
            drift := lastPTS - oh.PTS.Base
            if drift > 90000 {  // більше 1 секунди @ 90 kHz
                return fmt.Errorf("PTS drift detected: %d ticks", drift)
            }
        }
    }
    
    // 🔹 DTS <= PTS (кадр не може бути показаний раніше декодування)
    if oh.DTS != nil && oh.PTS != nil {
        if oh.DTS.Base > oh.PTS.Base {
            return fmt.Errorf("DTS > PTS: decoding after presentation")
        }
    }
    
    return nil
}
```

### ✅ 4: Моніторинг якості таймінгів

```go
// monitoring.Monitor — метрики для PES таймінгів:
type PESMetrics struct {
    PESParsed          *prometheus.CounterVec  // кількість парсингів PES
    PTSFound           *prometheus.CounterVec  // скільки мають PTS
    DTSFound           *prometheus.CounterVec  // скільки мають DTS
    TimestampDrifts    *prometheus.CounterVec  // помилки дрейфу таймінгів
    TrickModeDetected  *prometheus.CounterVec  // детектовані trick mode
    AvgPTSInterval     *prometheus.HistogramVec  // інтервал між PTS
}

// У обробці PES:
func monitorPES(pes *astits.PESData, channelID string, metrics *PESMetrics, lastPTS *int64) {
    metrics.PESParsed.WithLabelValues(channelID).Inc()
    
    if pes.Header != nil && pes.Header.OptionalHeader != nil {
        oh := pes.Header.OptionalHeader
        
        if oh.PTS != nil {
            metrics.PTSFound.WithLabelValues(channelID).Inc()
            
            // 🔹 Виміряти інтервал між PTS
            if *lastPTS > 0 {
                interval := float64(oh.PTS.Base - *lastPTS) / 90000.0  // у секундах
                metrics.AvgPTSInterval.WithLabelValues(channelID).Observe(interval)
            }
            *lastPTS = oh.PTS.Base
        }
        
        if oh.DTS != nil {
            metrics.DTSFound.WithLabelValues(channelID).Inc()
        }
        
        if oh.DSMTrickMode != nil {
            metrics.TrickModeDetected.WithLabelValues(
                channelID,
                trickModeToString(oh.DSMTrickMode.TrickModeControl),
            ).Inc()
        }
    }
}
```

### ✅ 5: Фрагментація для HLS сегментів

```go
// У segmentFinalizer — розбиття PES на сегменти для HLS:
func fragmentPESForHLS(pes *astits.PESData, maxSegmentSize int) [][]byte {
    var segments [][]byte
    data := pes.Data
    headerSize := calculatePESHeaderSize(pes.Header)
    
    // 🔹 Перший сегмент: заголовок + частина даних
    firstSegmentSize := min(maxSegmentSize, headerSize+len(data))
    firstSegment := make([]byte, firstSegmentSize)
    
    // Записати заголовок
    writePESHeaderToBuffer(firstSegment, pes.Header)
    
    // Додати дані
    dataCopied := copy(firstSegment[headerSize:], data)
    segments = append(segments, firstSegment)
    
    // 🔹 Решта даних: без заголовка
    remaining := data[dataCopied:]
    for len(remaining) > 0 {
        chunkSize := min(maxSegmentSize, len(remaining))
        chunk := make([]byte, chunkSize)
        copy(chunk, remaining[:chunkSize])
        segments = append(segments, chunk)
        remaining = remaining[chunkSize:]
    }
    
    return segments
}

func min(a, b int) int {
    if a < b { return a }
    return b
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на фрагментацію великого PES

```go
func TestPESData_LargeFragmentation(t *testing.T) {
    // Створити PES з 10000 байт даних
    largeData := make([]byte, 10000)
    for i := range largeData {
        largeData[i] = byte(i % 256)
    }
    
    pes := &astits.PESData{
        Data: largeData,
        Header: &astits.PESHeader{
            StreamID: 0xE0,  // відео
            PacketLength: uint16(len(largeData) + 10),  // + заголовок
            OptionalHeader: &astits.PESOptionalHeader{
                PTS: &astits.ClockReference{Base: 90000},  // 1 секунда
                PTSDTSIndicator: 2,  // PTS only
            },
        },
    }
    
    // 🔹 Фрагментувати на частини по 184 байти (макс. payload TS-пакету)
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    start := true
    payloadPos := 0
    maxPayload := 184  // 188 - 4 байти заголовка TS
    
    for payloadPos < len(largeData) {
        n, written, err := writePESData(
            w,
            pes.Header,
            largeData[payloadPos:],
            start,
            maxPayload,
        )
        assert.NoError(t, err)
        
        start = false
        payloadPos += written
        
        // 🔹 Перевірити, що перший фрагмент має заголовок
        if payloadPos == written {  // перша ітерація
            assert.Greater(t, n, maxPayload)  // заголовок + дані
        } else {
            assert.Equal(t, n, min(maxPayload, len(largeData)-payloadPos+written))
        }
    }
    
    // 🔹 Перевірити загальний розмір
    assert.Equal(t, len(largeData)+calculatePESHeaderSize(pes.Header), buf.Len())
}
```

### 🔹 Тест на валідацію маркер-бітів у PTS/DTS

```go
func TestPTSOrDTS_MarkerBits(t *testing.T) {
    // 🔹 Невірні маркер-біти мають викликати помилку
    invalidBytes := []byte{
        0x21,  // flag=2, base[32..30]=0b001, marker=0 ❌ має бути 1
        0x00, 0x00, 0x00, 0x00,
    }
    
    _, err := parsePTSOrDTS(astikit.NewBytesIterator(invalidBytes))
    // 🔹 Залежить від реалізації: може ігнорувати маркери або повертати помилку
    // assert.Error(t, err)  // якщо реалізація валідує маркери
}
```

### 🔹 Тест на round-trip з усіма опціями

```go
func TestPESData_RoundTrip_Full(t *testing.T) {
    original := &astits.PESData{
        Data: []byte("test video frame data"),
        Header: &astits.PESHeader{
            StreamID: 0xE0,
            PacketLength: 100,
            OptionalHeader: &astits.PESOptionalHeader{
                MarkerBits: 2,
                ScramblingControl: 0,
                Priority: false,
                DataAlignmentIndicator: true,
                IsCopyrighted: false,
                IsOriginal: true,
                PTSDTSIndicator: 3,  // обидва
                PTS: &astits.ClockReference{Base: 90000},
                DTS: &astits.ClockReference{Base: 85000},
                HasESCR: false,
                HasESRate: false,
                HasDSMTrickMode: false,
                HasExtension: false,
                HeaderLength: 10,
            },
        },
    }
    
    // 🔹 Серіалізувати
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    writePESData(w, original.Header, original.Data, true, 184)
    
    // 🔹 Парсити назад
    parsed, err := parsePESData(astikit.NewBytesIterator(buf.Bytes()))
    assert.NoError(t, err)
    
    // 🔹 Порівняти ключові поля
    assert.Equal(t, original.Header.StreamID, parsed.Header.StreamID)
    assert.Equal(t, original.Header.OptionalHeader.PTS.Base, parsed.Header.OptionalHeader.PTS.Base)
    assert.Equal(t, original.Header.OptionalHeader.DTS.Base, parsed.Header.OptionalHeader.DTS.Base)
    assert.Equal(t, original.Data, parsed.Data)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання розрідженого формату | PTS/DTS значення зміщені на біти | Перевірити маски: `bs[0]&0x0E` для біт 32-30, `bs[4]>>1` для біт 6-0 |
| Маркер-біти не валідуються | Пошкоджені таймінги не детектуються | Додати перевірку: `if bs[i]&markerMask != markerValue { return error }` |
| Фрагментація не зберігає заголовок | Тільки перший фрагмент має PES header | Перевірити логіку `start` прапорця у `writePESData` |
| PTSDTSIndicator не обробляється | PTS/DTS читаються навіть коли не присутні | Додати switch за `PTSDTSIndicator` перед читанням таймінгів |
| Великі PES не фрагментуються | Помилка "payload too large" | Перевірити цикл фрагментації: `for payloadPos < len(data)` |

### Приклад валідації маркер-бітів:

```go
func parsePTSOrDTSWithValidation(i *astikit.BytesIterator) (*ClockReference, error) {
    bs, _ := i.NextBytesNoCopy(5)
    
    // 🔹 Перевірити маркер-біти (мають бути 1)
    if bs[0]&0x01 != 0x01 { return nil, fmt.Errorf("invalid marker at byte 0") }
    if bs[2]&0x01 != 0x01 { return nil, fmt.Errorf("invalid marker at byte 2") }
    if bs[4]&0x01 != 0x01 { return nil, fmt.Errorf("invalid marker at byte 4") }
    
    // ... далі стандартний парсинг ...
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Витягування таймінгів з PES:
func extractPESClocks(pes *astits.PESData) (pts, dts *astits.ClockReference, hasPTS, hasDTS bool) {
    if pes.Header == nil || pes.Header.OptionalHeader == nil {
        return nil, nil, false, false
    }
    
    oh := pes.Header.OptionalHeader
    switch oh.PTSDTSIndicator {
    case 2:  // PTS only
        return oh.PTS, nil, true, false
    case 3:  // PTS + DTS
        return oh.PTS, oh.DTS, true, true
    default:
        return nil, nil, false, false
    }
}

// 2: Перевірка монотонності таймінгів:
func validateTimestampsMonotonic(currentPTS, lastPTS int64) error {
    if lastPTS > 0 && currentPTS < lastPTS {
        // 🔹 Дозволити невеликий "відкат" при розриві (< 1 секунди)
        if lastPTS-currentPTS > 90000 {  // 90 kHz ticks
            return fmt.Errorf("PTS regression: %d < %d (drift=%d)", 
                currentPTS, lastPTS, lastPTS-currentPTS)
        }
    }
    return nil
}

// 3: Конвертація ClockReference → time.Duration:
func clockToDuration(cr *astits.ClockReference, clockRate int64) time.Duration {
    if cr == nil {
        return 0
    }
    // 🔹 Base @ clockRate Hz + Extension @ 27 MHz (якщо є)
    baseNs := cr.Base * 1e9 / clockRate
    extNs := int64(0)
    if cr.Extension > 0 {
        extNs = cr.Extension * 1e9 / 27000000
    }
    return time.Duration(baseNs + extNs)
}

// Використання:
ptsDuration := clockToDuration(pts, 90000)  // PTS @ 90 kHz
escrDuration := clockToDuration(escr, 27000000)  // ESCR @ 27 MHz

// 4: Фрагментація для TS-пакетів:
func fragmentForTS(pes *astits.PESData, maxPayload int) [][]byte {
    var fragments [][]byte
    headerSize := calculatePESHeaderSize(pes.Header)
    data := pes.Data
    
    // 🔹 Перший фрагмент: заголовок + дані
    firstSize := min(maxPayload, headerSize+len(data))
    first := make([]byte, firstSize)
    writePESHeaderToBuffer(first, pes.Header)
    copy(first[headerSize:], data)
    fragments = append(fragments, first)
    
    // 🔹 Решта: тільки дані
    pos := firstSize - headerSize
    for pos < len(data) {
        chunkSize := min(maxPayload, len(data)-pos)
        chunk := make([]byte, chunkSize)
        copy(chunk, data[pos:pos+chunkSize])
        fragments = append(fragments, chunk)
        pos += chunkSize
    }
    
    return fragments
}

// 5: Моніторинг:
func monitorPESHealth(pes *astits.PESData, channelID string, metrics *PESMetrics) {
    if pes.Header != nil && pes.Header.OptionalHeader != nil {
        oh := pes.Header.OptionalHeader
        
        if oh.PTS != nil {
            metrics.PTSInterval.WithLabelValues(channelID).Observe(
                float64(oh.PTS.Base) / 90000.0,  // у секундах
            )
        }
        
        if oh.DSMTrickMode != nil {
            metrics.TrickMode.WithLabelValues(
                channelID,
                fmt.Sprintf("control_%d", oh.DSMTrickMode.TrickModeControl),
            ).Inc()
        }
    }
}
```

---

## 📊 Матриця полів PES Optional Header

```
Поле                     | Розмір   | Призначення                     | Використання у CCTV HLS
─────────────────────────┼──────────┼─────────────────────────────────┼─────────────────────────
PTSDTSIndicator          | 2 біти   | Які таймінги присутні          | ✅ Визначення наявності PTS/DTS
PTS/DTS                  | 33 біти  | Час презентації/декодування    | ✅ A/V синхронізація
ESCR                     | 33+9 біт | Еталонний час потоку @ 27 MHz  | ⚠️ Точна синхронізація
ESRate                   | 22 біти  | Швидкість потоку у байтах/сек  | ⚠️ Адаптивний бітрейт
DSMTrickMode             | 8 біт    | Режим спеціального відтворення | ⚠️ Підтримка перемотування
DataAlignmentIndicator   | 1 біт    | Дані вирівняні на початку      | ✅ Оптимізація парсингу
ScramblingControl        | 2 біти   | Шифрування потоку              | ❌ Ігноруємо (не підтримуємо)
Priority                 | 1 біт    | Пріоритет пакета               | ⚠️ Можна для QoS
Extension                | змінна   | Додаткові приватні дані        | ⚠️ Залежить від реалізації
```

---

## 📚 Корисні посилання

- [ISO/IEC 13818-1: PES packet structure](https://www.iso.org/standard/61236.html)
- [MPEG-TS PTS/DTS explanation](https://en.wikipedia.org/wiki/Packetized_Elementary_Stream)
- [astits PES parsing source](https://github.com/asticode/go-astits/blob/master/pes.go)

> 💡 **Ключова ідея**: PES — це "контейнер таймінгів" для відео/аудіо даних. У вашому CCTV HLS пайплайні це дозволяє:
> - ⏱️ Точну A/V синхронізацію через PTS/DTS @ 90 kHz
> - 🎮 Підтримку trick mode для швидкого перемотування у плеєрі
> - 🔍 Валідацію цілісності таймінгів для запобігання десинхронізації
> - 📊 Збір метрик про інтервали кадрів для моніторингу якості потоку

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати валідацію таймінгів у ваш `segmentAssembler` для раннього виявлення десинхронізації
- 🧪 Написати integration-тест для перевірки A/V синхронізації з реальними відео-потоками
- 📈 Додати Prometheus-метрики для моніторингу інтервалів PTS та дрейфу часу по каналах

🛠️