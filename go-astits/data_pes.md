# Глибоке роз'яснення: `pes.go` — ядро парсингу та серіалізації PES (Packetized Elementary Stream) у astits

Цей файл містить **повну реалізацію парсингу та запису PES-даних** — фундаментального механізму MPEG-TS для передачі відео/аудіо потоків з таймінгами (PTS/DTS). Це "серце" синхронізації аудіо/відео у вашому пайплайні.

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

## 🔧 Константи та типи: фундамент

### Stream IDs: ідентифікація типів потоків

```go
const (
    StreamIDPrivateStream1 = 189  // 0xBD: приватні дані (напр., субтитри)
    StreamIDPaddingStream  = 190  // 0xBE: заповнення (не має опц. заголовка)
    StreamIDPrivateStream2 = 191  // 0xBF: приватні дані 2 (не має опц. заголовка)
)
```

> 💡 **Важливо**: `hasPESOptionalHeader()` повертає `false` для `PaddingStream` та `PrivateStream2` — ці потоки не потребують таймінгів.

### PTSDTSIndicator: які таймінги присутні

```go
const (
    PTSDTSIndicatorNoPTSOrDTS  = 0  // жодного
    PTSDTSIndicatorIsForbidden = 1  // заборонено (помилка)
    PTSDTSIndicatorOnlyPTS     = 2  // тільки PTS
    PTSDTSIndicatorBothPresent = 3  // PTS + DTS
)
```

**Коли використовувати:**
```
• OnlyPTS (2): для аудіо або відео без B-frames → DTS = PTS
• BothPresent (3): для відео з B-frames → DTS < PTS
• NoPTSOrDTS (0): для даних без таймінгів (напр., метадані)
```

### TrickModeControl: режими спеціального відтворення

```go
const (
    TrickModeControlFastForward = 0  // Швидке вперед
    TrickModeControlSlowMotion  = 1  // Уповільнене відтворення
    TrickModeControlFreezeFrame = 2  // Заморозка кадру
    TrickModeControlFastReverse = 3  // Швидке назад
    TrickModeControlSlowReverse = 4  // Уповільнене назад
)
```

### ClockReference: універсальний тип для таймінгів

```go
// Визначено в clock.go, але використовується у PES
type ClockReference struct {
    Base      int64  // Основне значення @ 90 kHz (PTS/DTS) або 27 MHz (ESCR)
    Extension int64  // Розширення @ 27 MHz (тільки для ESCR)
}

// Helper для створення
func newClockReference(base, extension int64) *ClockReference {
    return &ClockReference{Base: base, Extension: extension}
}

// Конвертація у time.Duration
func (cr *ClockReference) Duration() time.Duration {
    // Base @ 90 kHz → наносекунди
    baseNs := cr.Base * 1e9 / 90000
    // Extension @ 27 MHz → наносекунди (якщо є)
    extNs := cr.Extension * 1e9 / 27000000
    return time.Duration(baseNs + extNs)
}
```

---

## 📦 Структури даних

### `PESData` — контейнер для всього PES-пакета

```go
type PESData struct {
    Data   []byte       // 🎯 корисне навантаження (відео/аудіо кадри)
    Header *PESHeader   // 🎯 заголовок з метаданими
}
```

### `PESHeader` — обов'язковий заголовок

```go
type PESHeader struct {
    OptionalHeader *PESOptionalHeader  // 🎯 опціональні дані (таймінги, прапорці)
    PacketLength   uint16              // 🎯 довжина після цього поля (0 = необмежено для відео)
    StreamID       uint8               // 🎯 тип потоку (0xE0=відео, 0xC0=аудіо)
}
```

**Метод `IsVideoStream()`:**
```go
func (h *PESHeader) IsVideoStream() bool {
    return h.StreamID == 0xe0 || h.StreamID == 0xfd
}
```
> 💡 **Важливо**: Для відео `PacketLength` може бути 0 (необмежена довжина), для аудіо — має бути вказано.

### `PESOptionalHeader` — опціональні метадані

```go
type PESOptionalHeader struct {
    // 🔹 Прапорці (1-й байт)
    MarkerBits                      uint8   // 2 біти: завжди 0b10
    ScramblingControl               uint8   // 2 біти: шифрування (0=немає)
    Priority                        bool    // 1 біт: пріоритет пакета
    DataAlignmentIndicator          bool    // 1 біт: дані вирівняні на start code
    IsCopyrighted                   bool    // 1 біт: захищено авторським правом
    IsOriginal                      bool    // 1 біт: оригінал чи копія
    
    // 🔹 Прапорці (2-й байт)
    PTSDTSIndicator                 uint8   // 2 біти: які таймінги присутні
    HasESCR                         bool    // 1 біт: ESCR присутній
    HasESRate                       bool    // 1 біт: ES rate присутній
    HasDSMTrickMode                 bool    // 1 біт: trick mode присутній
    HasAdditionalCopyInfo           bool    // 1 біт: додаткова інформація про копіювання
    HasCRC                          bool    // 1 біт: CRC присутній
    HasExtension                    bool    // 1 біт: extension присутній
    
    // 🔹 Загальні поля
    HeaderLength                    uint8   // довжина опціонального заголовка
    PTS                             *ClockReference  // 🎯 час презентації
    DTS                             *ClockReference  // 🎯 час декодування
    ESCR                            *ClockReference  // 🎯 еталонний час потоку
    ESRate                          uint32           // швидкість потоку у байтах/сек
    DSMTrickMode                    *DSMTrickMode    // 🎯 режим спеціального відтворення
    AdditionalCopyInfo              uint8            // додаткова інформація про копіювання
    CRC                             uint16           // CRC попереднього PES-пакета
    
    // 🔹 Extension (якщо HasExtension=true)
    HasPrivateData                  bool
    HasPackHeaderField              bool
    HasProgramPacketSequenceCounter bool
    HasPSTDBuffer                   bool
    HasExtension2                   bool
    PrivateData                     []byte           // 16 байт приватних даних
    PackField                       uint8            // довжина pack_header (TODO)
    PacketSequenceCounter           uint8            // лічильник пакетів
    MPEG1OrMPEG2ID                  uint8            // ідентифікатор стандарту
    OriginalStuffingLength          uint8            // довжина оригінального заповнення
    PSTDBufferScale                 uint8            // масштаб буфера (0=128Б, 1=1024Б)
    PSTDBufferSize                  uint16           // розмір буфера
    Extension2Length                uint8            // довжина extension2 даних
    Extension2Data                  []byte           // дані extension2
}
```

### `DSMTrickMode` — режим спеціального відтворення

```go
type DSMTrickMode struct {
    TrickModeControl    uint8  // 3 біти: тип режиму (0-4)
    FieldID             uint8  // 2 біти: для fast_forward/fast_reverse
    IntraSliceRefresh   uint8  // 1 біт: для fast_forward/fast_reverse
    FrequencyTruncation uint8  // 2 біти: для fast_forward/fast_reverse
    RepeatControl       uint8  // 5 біт: для slow_motion/slow_reverse
}
```

---

## 🔍 Функція `parsePESData`: головний вхідний пункт

```go
func parsePESData(i *astikit.BytesIterator) (*PESData, error) {
    d := &PESData{}
    
    // 🔹 1. Пропустити 3-байтний prefix (0x000001)
    i.Seek(3)
    
    // 🔹 2. Парсинг заголовка
    h, dataStart, dataEnd, err := parsePESHeader(i)
    if err != nil {
        return nil, fmt.Errorf("astits: parsing PES header failed: %w", err)
    }
    d.Header = h
    
    // 🔹 3. Валідація офсетів
    if dataEnd < dataStart {
        return nil, fmt.Errorf("astits: data end %d is before data start %d", dataEnd, dataStart)
    }
    
    // 🔹 4. Перейти до даних та прочитати їх
    i.Seek(dataStart)
    d.Data, err = i.NextBytes(dataEnd - dataStart)
    if err != nil {
        return nil, fmt.Errorf("astits: fetching next bytes failed: %w", err)
    }
    
    return d, nil
}
```

### Ключовий момент: `PacketLength = 0` для відео

```
Якщо h.PacketLength == 0:
  • dataEnd = i.Len()  // читати до кінця ітератора
  • Це дозволяє необмежену довжину для відео-потоків
  • Для аудіо PacketLength має бути вказано!

Приклад:
  PacketLength = 0, i.Len() = 10000
  → dataEnd = 10000, читаємо всі доступні байти
```

---

## 🔍 Функція `parsePESHeader`: парсинг обов'язкового заголовка

```go
func parsePESHeader(i *astikit.BytesIterator) (*PESHeader, int, int, error) {
    h := &PESHeader{}
    
    // 🔹 1. Stream ID (1 байт)
    b, _ := i.NextByte()
    h.StreamID = uint8(b)
    
    // 🔹 2. Packet Length (2 байти, big-endian)
    bs, _ := i.NextBytesNoCopy(2)
    h.PacketLength = uint16(bs[0])<<8 | uint16(bs[1])
    
    // 🔹 3. Розрахунок dataEnd
    if h.PacketLength > 0 {
        dataEnd = i.Offset() + int(h.PacketLength)
    } else {
        dataEnd = i.Len()  // необмежена довжина для відео
    }
    
    // 🔹 4. Опціональний заголовок (якщо stream_id дозволяє)
    if hasPESOptionalHeader(h.StreamID) {
        h.OptionalHeader, dataStart, err = parsePESOptionalHeader(i)
    } else {
        dataStart = i.Offset()  // немає опц. заголовка → дані починаються одразу
    }
    
    return h, dataStart, dataEnd, nil
}
```

### `hasPESOptionalHeader`: фільтрація за stream_id

```go
func hasPESOptionalHeader(streamID uint8) bool {
    // ❌ Ці два типи НЕ мають опціонального заголовка:
    return streamID != StreamIDPaddingStream &&  // 0xBE
           streamID != StreamIDPrivateStream2     // 0xBF
}
```

---

## 🔍 Функція `parsePESOptionalHeader`: парсинг опціональних метаданих

### Крок 1: Читання прапорців (2 байти)

```go
// 🔹 Байт 1: загальні прапорці
b, _ := i.NextByte()
h.MarkerBits = uint8(b) >> 6              // біти 7-6
h.ScramblingControl = uint8(b) >> 4 & 0x3 // біти 5-4
h.Priority = uint8(b)&0x8 > 0             // біт 3
h.DataAlignmentIndicator = uint8(b)&0x4 > 0  // біт 2
h.IsCopyrighted = uint8(b)&0x2 > 0        // біт 1
h.IsOriginal = uint8(b)&0x1 > 0           // біт 0

// 🔹 Байт 2: прапорці таймінгів та розширень
b, _ = i.NextByte()
h.PTSDTSIndicator = uint8(b) >> 6 & 0x3   // біти 7-6
h.HasESCR = uint8(b)&0x20 > 0             // біт 5
h.HasESRate = uint8(b)&0x10 > 0           // біт 4
h.HasDSMTrickMode = uint8(b)&0x8 > 0      // біт 3
h.HasAdditionalCopyInfo = uint8(b)&0x4 > 0 // біт 2
h.HasCRC = uint8(b)&0x2 > 0               // біт 1
h.HasExtension = uint8(b)&0x1 > 0         // біт 0
```

### Крок 2: Читання довжини та розрахунок dataStart

```go
// 🔹 HeaderLength: довжина опціонального заголовка (без цих 3 байт)
b, _ = i.NextByte()
h.HeaderLength = uint8(b)

// 🔹 dataStart: де починаються корисні дані
dataStart = i.Offset() + int(h.HeaderLength)
```

### Крок 3: Умовне читання полів за прапорцями

```go
// 🔹 PTS/DTS (залежить від PTSDTSIndicator)
if h.PTSDTSIndicator == PTSDTSIndicatorOnlyPTS {
    h.PTS, _ = parsePTSOrDTS(i)  // 5 байт
} else if h.PTSDTSIndicator == PTSDTSIndicatorBothPresent {
    h.PTS, _ = parsePTSOrDTS(i)  // 5 байт
    h.DTS, _ = parsePTSOrDTS(i)  // 5 байт
}

// 🔹 ESCR (якщо HasESCR)
if h.HasESCR {
    h.ESCR, _ = parseESCR(i)  // 6 байт
}

// 🔹 ES Rate (якщо HasESRate)
if h.HasESRate {
    bs, _ = i.NextBytesNoCopy(3)
    // Формат: [1][22 біти][1] → 22-бітне значення
    h.ESRate = uint32(bs[0])&0x7f<<15 | uint32(bs[1])<<7 | uint32(bs[2])>>1
}

// 🔹 DSM Trick Mode (якщо HasDSMTrickMode)
if h.HasDSMTrickMode {
    b, _ = i.NextByte()
    h.DSMTrickMode = parseDSMTrickMode(b)  // 1 байт
}

// 🔹 Additional Copy Info (якщо HasAdditionalCopyInfo)
if h.HasAdditionalCopyInfo {
    b, _ = i.NextByte()
    h.AdditionalCopyInfo = b & 0x7f  // 7 біт
}

// 🔹 CRC (якщо HasCRC) — TODO: не реалізовано
// 🔹 Extension (якщо HasExtension) — див. нижче
```

### Крок 4: Парсинг Extension (складна вкладена структура)

```go
if h.HasExtension {
    // 🔹 Прапорці extension
    b, _ = i.NextByte()
    h.HasPrivateData = b&0x80 > 0
    h.HasPackHeaderField = b&0x40 > 0
    h.HasProgramPacketSequenceCounter = b&0x20 > 0
    h.HasPSTDBuffer = b&0x10 > 0
    h.HasExtension2 = b&0x1 > 0
    
    // 🔹 Private Data (16 байт, якщо є)
    if h.HasPrivateData {
        h.PrivateData, _ = i.NextBytes(16)
    }
    
    // 🔹 Pack Header Field — TODO: не реалізовано повністю
    if h.HasPackHeaderField {
        b, _ = i.NextByte()
        h.PackField = uint8(b)  // тільки довжина, не дані
    }
    
    // 🔹 Program Packet Sequence Counter (2 байти)
    if h.HasProgramPacketSequenceCounter {
        bs, _ = i.NextBytesNoCopy(2)
        h.PacketSequenceCounter = uint8(bs[0]) & 0x7f  // 7 біт
        h.MPEG1OrMPEG2ID = uint8(bs[1]) >> 6 & 0x1     // 1 біт
        h.OriginalStuffingLength = uint8(bs[1]) & 0x3f // 6 біт
    }
    
    // 🔹 P-STD Buffer (2 байти)
    if h.HasPSTDBuffer {
        bs, _ = i.NextBytesNoCopy(2)
        h.PSTDBufferScale = bs[0] >> 5 & 0x1  // 1 біт: 0=128Б, 1=1024Б
        h.PSTDBufferSize = uint16(bs[0])&0x1f<<8 | uint16(bs[1])  // 13 біт
    }
    
    // 🔹 Extension 2 (змінна довжина)
    if h.HasExtension2 {
        b, _ = i.NextByte()
        h.Extension2Length = uint8(b) & 0x7f  // 7 біт
        h.Extension2Data, _ = i.NextBytes(int(h.Extension2Length))
    }
}
```

---

## ⏱️ Парсинг таймінгів: `parsePTSOrDTS` та `parseESCR`

### `parsePTSOrDTS`: 40 біт = 5 байт у розрідженому форматі

```
Формат: [4 flag][3 base[32..30]][1 marker=1][15 base[29..15]][1 marker=1][15 base[14..0]][1 marker=1]

Приклад для base = 5726623061:
  • base[32..30] = 0b101 = 5
  • base[29..15] = 0b010101010101010 = 0x2AAA = 10922
  • base[14..0] = 0b101010101010101 = 0x5555 = 21845

Розрахунок:
  5<<30 + 10922<<15 + 21845 = 5368709120 + 358465536 + 21845 = 5726623061 ✅
```

```go
func parsePTSOrDTS(i *astikit.BytesIterator) (*ClockReference, error) {
    bs, _ := i.NextBytesNoCopy(5)  // 5 байт
    
    // 🔹 Збірка 33-бітного base з розрідженого формату
    base := int64(
        uint64(bs[0])>>1&0x7<<30 |  // біти 32-30 з байта 0
        uint64(bs[1])<<22 |          // весь байт 1 = біти 29-22
        uint64(bs[2])>>1&0x7f<<15 |  // біти 21-15 з байта 2
        uint64(bs[3])<<7 |           // весь байт 3 = біти 14-7
        uint64(bs[4])>>1&0x7f,       // біти 6-0 з байта 4
    )
    
    return newClockReference(base, 0), nil
}
```

### `parseESCR`: 48 біт = 6 байт з 9-бітним extension

```
Формат: [2 dummy][3 base[32..30]][1 marker][15 base[29..15]][1 marker][15 base[14..0]][1 marker][9 extension][1 marker]

Відмінність від PTS/DTS:
• +9 біт extension для точнішої синхронізації @ 27 MHz
• Загальна точність: base @ 90 kHz + extension @ 27 MHz
```

```go
func parseESCR(i *astikit.BytesIterator) (*ClockReference, error) {
    bs, _ := i.NextBytesNoCopy(6)  // 6 байт
    
    // 🔹 Складна збірка 42-бітного base + 9-бітного extension
    escr := uint64(
        uint64(bs[0])>>3&0x7<<39 |  // base[32..30]
        uint64(bs[0])&0x3<<37 |     // base[29..28]
        uint64(bs[1])<<29 |          // base[27..22]
        uint64(bs[2])>>3<<24 |      // base[21..16]
        uint64(bs[2])&0x3<<22 |     // base[15..14]
        uint64(bs[3])<<14 |          // base[13..8]
        uint64(bs[4])>>3<<9 |       // base[7..3]
        uint64(bs[4])&0x3<<7 |      // base[2..0]
        uint64(bs[5])>>1,            // extension[8..1]
    )
    
    // Розділити на base (33 біти) та extension (9 біт)
    return newClockReference(int64(escr>>9), int64(escr&0x1ff)), nil
}
```

---

## 🎮 Парсинг `DSMTrickMode`: 1 байт = 8 біт

```go
func parseDSMTrickMode(b byte) *DSMTrickMode {
    m := &DSMTrickMode{}
    
    // 🔹 Біти 7-5: TrickModeControl (3 біти)
    m.TrickModeControl = b >> 5
    
    // 🔹 Залежно від режиму — читаємо решту біт
    switch m.TrickModeControl {
    case TrickModeControlFastForward, TrickModeControlFastReverse:
        // [4-3] FieldID, [2] IntraSliceRefresh, [1-0] FrequencyTruncation
        m.FieldID = (b >> 3) & 0x03
        m.IntraSliceRefresh = (b >> 2) & 0x01
        m.FrequencyTruncation = b & 0x03
        
    case TrickModeControlFreezeFrame:
        // [4-3] FieldID, [2-0] reserved
        m.FieldID = (b >> 3) & 0x03
        
    case TrickModeControlSlowMotion, TrickModeControlSlowReverse:
        // [4-0] RepeatControl (5 біт)
        m.RepeatControl = b & 0x1F
    }
    
    return m
}
```

---

## ✏️ Серіалізація: `writePESData`, `writePESHeader`, `writePESOptionalHeader`

### `writePESData`: фрагментація великих даних

```go
func writePESData(w *astikit.BitsWriter, h *PESHeader, payloadLeft []byte, 
                  isPayloadStart bool, bytesAvailable int) (totalBytesWritten, payloadBytesWritten int, error) {
    
    // 🔹 Якщо це початок — записати заголовок
    if isPayloadStart {
        n, err := writePESHeader(w, h, len(payloadLeft))
        if err != nil { return 0, 0, err }
        totalBytesWritten += n
    }
    
    // 🔹 Розрахувати, скільки даних поміститься
    payloadBytesWritten = bytesAvailable - totalBytesWritten
    if payloadBytesWritten > len(payloadLeft) {
        payloadBytesWritten = len(payloadLeft)
    }
    
    // 🔹 Записати дані
    err := w.Write(payloadLeft[:payloadBytesWritten])
    if err != nil { return 0, 0, err }
    
    totalBytesWritten += payloadBytesWritten
    return totalBytesWritten, payloadBytesWritten, nil
}
```

**Логіка фрагментації:**
```
Вхід: 1000 байт відео-даних, bytesAvailable = 184 (макс. payload TS-пакету)

Ітерація 1 (isPayloadStart=true):
• Записати PES header + optional header + 184 байти даних
• Встановити PayloadUnitStartIndicator=1 у першому TS-пакеті
• Повернути: total=200, payload=184

Ітерація 2 (isPayloadStart=false):
• Записати тільки 184 байти даних (без заголовка)
• PayloadUnitStartIndicator=0 (продовження)
• Повернути: total=184, payload=184

... повторювати доки payloadLeft не вичерпається

Результат: 1000 байт → 6 TS-пакетів (1 з заголовком, 5 продовжень)
```

### `calcPESDataLength`: попередній розрахунок для stuffing

```go
func calcPESDataLength(h *PESHeader, payloadLeft []byte, isPayloadStart bool, bytesAvailable int) (totalBytes, payloadBytes int) {
    totalBytes += pesHeaderLength  // 6 байт: prefix(3) + stream_id(1) + packet_length(2)
    
    if isPayloadStart {
        totalBytes += int(calcPESOptionalHeaderLength(h.OptionalHeader))
    }
    
    bytesAvailable -= totalBytes
    
    if len(payloadLeft) < bytesAvailable {
        payloadBytes = len(payloadLeft)
    } else {
        payloadBytes = bytesAvailable
    }
    
    return
}
```

> 💡 **Патерн**: Спочатку розрахувати, скільки місця залишиться для даних, потім записати. Це дозволяє додати stuffing у адаптаційне поле, якщо дані не заповнюють весь пакет.

### `writePESHeader`: запис обов'язкового заголовка

```go
func writePESHeader(w *astikit.BitsWriter, h *PESHeader, payloadSize int) (int, error) {
    b := astikit.NewBitsWriterBatch(w)
    
    // 🔹 Packet start code prefix: 0x000001 (24 біти)
    b.WriteN(uint32(0x000001), 24)
    
    // 🔹 Stream ID (1 байт)
    b.Write(h.StreamID)
    
    // 🔹 Packet Length (2 байти)
    pesPacketLength := 0
    if !h.IsVideoStream() {
        // Для аудіо: вказати довжину
        pesPacketLength = payloadSize
        if hasPESOptionalHeader(h.StreamID) {
            pesPacketLength += int(calcPESOptionalHeaderLength(h.OptionalHeader))
        }
        if pesPacketLength > 0xffff {
            pesPacketLength = 0  // fallback, якщо занадто велике
        }
    }
    // Для відео: залишити 0 (необмежена довжина)
    
    b.Write(uint16(pesPacketLength))
    
    bytesWritten := pesHeaderLength  // 6 байт
    
    // 🔹 Опціональний заголовок (якщо є)
    if hasPESOptionalHeader(h.StreamID) {
        n, err := writePESOptionalHeader(w, h.OptionalHeader)
        if err != nil { return 0, err }
        bytesWritten += n
    }
    
    return bytesWritten, b.Err()
}
```

### `writePESOptionalHeader`: запис опціональних метаданих

```go
func writePESOptionalHeader(w *astikit.BitsWriter, h *PESOptionalHeader) (int, error) {
    if h == nil { return 0, nil }
    
    b := astikit.NewBitsWriterBatch(w)
    
    // 🔹 Байт 1: загальні прапорці
    b.WriteN(uint8(0b10), 2)  // marker_bits = 0b10
    b.WriteN(h.ScramblingControl, 2)
    b.Write(h.Priority)
    b.Write(h.DataAlignmentIndicator)
    b.Write(h.IsCopyrighted)
    b.Write(h.IsOriginal)
    
    // 🔹 Байт 2: прапорці таймінгів
    b.WriteN(h.PTSDTSIndicator, 2)
    b.Write(h.HasESCR)
    b.Write(h.HasESRate)
    b.Write(h.HasDSMTrickMode)
    b.Write(h.HasAdditionalCopyInfo)
    b.Write(false)  // CRC flag — TODO: не реалізовано
    b.Write(h.HasExtension)
    
    // 🔹 HeaderLength: довжина даних (без цих 3 байт)
    pesOptionalHeaderDataLength := calcPESOptionalHeaderDataLength(h)
    b.Write(pesOptionalHeaderDataLength)
    
    bytesWritten := 3  // 3 байти прапорців + довжини
    
    // 🔹 Умовний запис полів за прапорцями
    if h.PTSDTSIndicator == PTSDTSIndicatorOnlyPTS {
        n, _ := writePTSOrDTS(w, 0b0010, h.PTS)  // flag=2
        bytesWritten += n
    }
    if h.PTSDTSIndicator == PTSDTSIndicatorBothPresent {
        n, _ := writePTSOrDTS(w, 0b0011, h.PTS)  // flag=3
        bytesWritten += n
        n, _ = writePTSOrDTS(w, 0b0001, h.DTS)   // flag=1
        bytesWritten += n
    }
    if h.HasESCR {
        n, _ := writeESCR(w, h.ESCR)
        bytesWritten += n
    }
    if h.HasESRate {
        b.Write(true)  // marker
        b.WriteN(h.ESRate, 22)
        b.Write(true)  // marker
        bytesWritten += 3
    }
    if h.HasDSMTrickMode {
        n, _ := writeDSMTrickMode(w, h.DSMTrickMode)
        bytesWritten += n
    }
    if h.HasAdditionalCopyInfo {
        b.Write(true)  // marker
        b.WriteN(h.AdditionalCopyInfo, 7)
        bytesWritten++
    }
    // 🔹 Extension — аналогічно парсингу, але у зворотному порядку
    
    return bytesWritten, b.Err()
}
```

### `calcPESOptionalHeaderLength`: розрахунок довжини для заголовка

```go
func calcPESOptionalHeaderLength(h *PESOptionalHeader) uint8 {
    if h == nil { return 0 }
    return 3 + calcPESOptionalHeaderDataLength(h)  // 3 байти прапорців + дані
}

func calcPESOptionalHeaderDataLength(h *PESOptionalHeader) uint8 {
    length := uint8(0)
    
    // 🔹 PTS/DTS
    if h.PTSDTSIndicator == PTSDTSIndicatorOnlyPTS {
        length += ptsOrDTSByteLength  // 5 байт
    } else if h.PTSDTSIndicator == PTSDTSIndicatorBothPresent {
        length += 2 * ptsOrDTSByteLength  // 10 байт
    }
    
    // 🔹 Інші поля
    if h.HasESCR { length += escrLength }  // 6 байт
    if h.HasESRate { length += 3 }
    if h.HasDSMTrickMode { length += dsmTrickModeLength }  // 1 байт
    if h.HasAdditionalCopyInfo { length++ }
    if h.HasCRC { /* TODO */ }
    
    // 🔹 Extension
    if h.HasExtension {
        length++  // байт прапорців extension
        if h.HasPrivateData { length += 16 }
        if h.HasProgramPacketSequenceCounter { length += 2 }
        if h.HasPSTDBuffer { length += 2 }
        if h.HasExtension2 { length += 1 + uint8(len(h.Extension2Data)) }
    }
    
    return length
}
```

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

## 🧪 Тестування: стратегії валідації

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