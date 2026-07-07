# Глибоке роз'яснення: `TestRoundTrip` — інтеграційний тест для astits

Цей тест перевіряє **цілісність циклу "демуксинг → муксинг → демуксинг"** для MPEG-TS потоків. Це критично важливо для будь-якого інструменту, що модифікує, транскодує або аналізує TS-потоки.

---

## 🎯 Мета тесту

```
┌─────────────────────────────────────────┐
│ Round-Trip тестування:                  │
│                                         │
│ [Оригінал .ts]                          │
│       ↓                                 │
│ [Demuxer] → витягти PAT/PMT/ES          │
│       ↓                                 │
│ [Muxer] → зібрати новий .ts з тими ж    │
│           метаданими                    │
│       ↓                                 │
│ [Demuxer] → перевірити, що всі поля    │
│           збереглися без змін           │
│                                         │
│ ✅ Успіх = бінарна сумісність метаданих │
└─────────────────────────────────────────┘
```

**Чому це важливо для вашого пайплайну:**
- Гарантує, що `segmentFinalizer` не пошкодить PSI/SI таблиці при ре-муксингу
- Перевіряє коректність `createTSSegment` перед відправкою у FFmpeg
- Запобігає "тихим" помилкам, коли плейлист валідний, але потік — ні

---

## 🔧 Структура тесту: 4 фази

### 📦 Фаза 1: Демуксинг оригіналу

```go
originalBytes, err := os.ReadFile("testdata/ts/silent_audio.ts")
dmx := NewDemuxer(ctx, bytes.NewReader(originalBytes), DemuxerOptPacketSize(MpegTsPacketSize))

var originalPAT *PATData
var originalPMT *PMTData
var originalPMTPID uint16 = 0xFFFF

for {
    d, err := dmx.NextData()
    if errors.Is(err, ErrNoMorePackets) { break }
    
    if d.PAT != nil {
        originalPAT = d.PAT
        originalPMTPID = d.PAT.Programs[0].ProgramMapID  // PID таблиці PMT
    }
    if d.PMT != nil {
        originalPMT = d.PMT
    }
    if originalPMT != nil && originalPAT != nil { break }  // оптимізація
}
```

**Що витягуємо:**
| Поле | Тип | Призначення |
|------|-----|-------------|
| `TransportStreamID` | uint16 | Унікальний ID TS-потоку |
| `ProgramMapID` | uint16 | PID, де шукати PMT (зазвичай 0x1000) |
| `PCRPID` | uint16 | PID пакетів з PCR для синхронізації |
| `ElementaryStreams[]` | []PMTElementaryStream | Відео/аудіо потоки з дескрипторами |

> 💡 Тест зупиняється після знаходження PAT+PMT — не потрібно парсити весь файл, бо перевіряємо тільки метадані.

---

### ✏️ Фаза 2: Муксинг назад у TS

```go
var buf bytes.Buffer
muxer := NewMuxer(ctx, &buf,
    WithTransportStreamID(originalPAT.TransportStreamID),
    WithPMTPID(originalPMTPID),  // зберегти оригінальний PID PMT!
)

// Додати elementary streams з оригіналу
for _, es := range originalPMT.ElementaryStreams {
    err := muxer.AddElementaryStream(PMTElementaryStream{
        ElementaryPID:               es.ElementaryPID,
        StreamType:                  es.StreamType,
        ElementaryStreamDescriptors: es.ElementaryStreamDescriptors,
    })
    require.NoError(t, err)
}

// Зберегти PCR PID та program descriptors
muxer.SetPCRPID(originalPMT.PCRPID)
muxer.pmt.ProgramDescriptors = originalPMT.ProgramDescriptors

// Записати PAT/PMT таблиці у буфер
_, err = muxer.WriteTables()
require.NoError(t, err)
```

**Ключові моменти:**
1. **`WithPMTPID(originalPMTPID)`** — критично! Якщо PMT буде на іншому PID, плеєри не знайдуть програму.
2. **`ElementaryStreamDescriptors`** — містять кодек-специфічні дані (H.264 SPS/PPS, AAC config).
3. **`WriteTables()`** — генерує бінарні PSI-пакети з правильними CRC32.

---

### 🔍 Фаза 3: Повторний демуксинг результату

```go
dmx2 := NewDemuxer(ctx, bytes.NewReader(buf.Bytes()), DemuxerOptPacketSize(MpegTsPacketSize))

var rtPAT *PATData  // "round-tripped"
var rtPMT *PMTData

for {
    d, err := dmx2.NextData()
    if errors.Is(err, ErrNoMorePackets) { break }
    // ... аналогічно фазі 1
}
```

> ⚠️ Використовується той самий `DemuxerOptPacketSize(MpegTsPacketSize)` — важливо для коректного парсингу, якщо оригінал мав нестандартний розмір пакетів.

---

### ✅ Фаза 4: Валідація збереження даних

#### 🔹 PAT перевірка
```go
assert.Equal(t, originalPAT.TransportStreamID, rtPAT.TransportStreamID)
require.Equal(t, len(originalPAT.Programs), len(rtPAT.Programs))

for i, origProg := range originalPAT.Programs {
    assert.Equalf(t, origProg.ProgramNumber, rtPAT.Programs[i].ProgramNumber, ...)
    assert.Equalf(t, origProg.ProgramMapID, rtPAT.Programs[i].ProgramMapID, ...)
}
```

#### 🔹 PMT перевірка
```go
// PCR PID та program number
assert.Equal(t, originalPMT.PCRPID, rtPMT.PCRPID)
assert.Equal(t, originalPMT.ProgramNumber, rtPMT.ProgramNumber)

// Program descriptors (напр., service name, provider)
for i, desc := range originalPMT.ProgramDescriptors {
    assert.Equalf(t, desc.Tag, rtPMT.ProgramDescriptors[i].Tag, ...)
    assert.Equalf(t, desc.Length, rtPMT.ProgramDescriptors[i].Length, ...)
    // ⚠️ Не перевіряє Payload — припускає, що бінарні дані однакові
}

// Elementary streams (відео/аудіо)
for i, es := range originalPMT.ElementaryStreams {
    rtES := rtPMT.ElementaryStreams[i]
    assert.Equalf(t, es.ElementaryPID, rtES.ElementaryPID, ...)
    assert.Equalf(t, es.StreamType, rtES.ElementaryStreamType, ...)
    
    // Дескриптори потоків (напр., registration_descriptor, AAC config)
    for j, desc := range es.ElementaryStreamDescriptors {
        assert.Equalf(t, desc.Tag, rtES.ElementaryStreamDescriptors[j].Tag, ...)
        assert.Equalf(t, desc.Length, rtES.ElementaryStreamDescriptors[j].Length, ...)
    }
}
```

> ⚠️ **Обмеження тесту**: не перевіряє `Payload` дескрипторів — тільки `Tag` та `Length`. Якщо муксер змінить вміст дескриптора (напр., переставить байти), тест пройде, але потік може бути невалідним.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Валідація `segmentFinalizer` перед FFmpeg

```go
// segmentFinalizer.go — тестова функція для відладки
func validateTSSegmentRoundTrip(tsData []byte) error {
    // Фаза 1: демукс
    dmx := astits.NewDemuxer(ctx, bytes.NewReader(tsData))
    var pat *astits.PATData
    var pmt *astits.PMTData
    
    for {
        d, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) { break }
        if d.PAT != nil { pat = d.PAT }
        if d.PMT != nil { pmt = d.PMT }
        if pat != nil && pmt != nil { break }
    }
    if pat == nil || pmt == nil {
        return fmt.Errorf("missing PAT/PMT in segment")
    }
    
    // Фаза 2: мукс назад
    var buf bytes.Buffer
    mux := astits.NewMuxer(ctx, &buf,
        astits.WithTransportStreamID(pat.TransportStreamID),
        astits.WithPMTPID(pmt.ProgramNumber), // ⚠️ увага: ProgramNumber ≠ ProgramMapID!
    )
    // ... додати ES ...
    _, err = mux.WriteTables()
    if err != nil {
        return fmt.Errorf("muxing failed: %w", err)
    }
    
    // Фаза 3: повторний демукс + проста перевірка
    dmx2 := astits.NewDemuxer(ctx, bytes.NewReader(buf.Bytes()))
    rtPAT, _ := dmx2.NextData()
    if rtPAT.PAT == nil || rtPAT.PAT.TransportStreamID != pat.TransportStreamID {
        return fmt.Errorf("round-trip validation failed: TransportStreamID mismatch")
    }
    return nil
}

// Виклик у production (опціонально, тільки в debug-режимі):
if config.DebugValidateSegments {
    if err := validateTSSegmentRoundTrip(segmentData); err != nil {
        log.Errorf("Segment validation failed (seq=%d): %v", seqNum, err)
        metrics.SegmentValidationErrors.WithLabelValues(channelID).Inc()
    }
}
```

### ✅ 2. Unit-тести для вашого `createTSSegment`

```go
// segmentFinalizer_test.go
func TestCreateTSSegment_RoundTrip(t *testing.T) {
    // Підготувати тестові дані: відео+аудіо з вашого pipeline
    videoFrames := generateTestH264Frames(10)
    audioChunks := generateTestAACChunks(20)
    
    segmentData, err := createTSSegment(videoFrames, audioChunks, 123)
    require.NoError(t, err)
    
    // Запустити round-trip перевірку (як у astits)
    err = validateTSSegmentRoundTrip(segmentData)
    assert.NoError(t, err, "created segment should survive round-trip")
    
    // Додатково: перевірити, що PCR PID вказує на відео-потік
    dmx := astits.NewDemuxer(ctx, bytes.NewReader(segmentData))
    for {
        d, _ := dmx.NextData()
        if d.PMT != nil {
            assert.Equal(t, videoPID, d.PMT.PCRPID, "PCR should come from video stream")
            break
        }
    }
}
```

### ✅ 3. Моніторинг цілісності через Prometheus

```go
// monitoring.Monitor
type Metrics struct {
    SegmentRoundTripErrors *prometheus.CounterVec  // помилки валідації
    PATPMTConsistencyGauge *prometheus.GaugeVec    // 1=OK, 0=mismatch
}

// У segmentFinalizer:
func finalizeSegment(seg *Segment) error {
    tsData, err := assembleTS(seg)
    if err != nil { return err }
    
    // Опціональна валідація (тільки якщо увімкнено)
    if cfg.EnableSegmentValidation {
        if ok := quickPATPMTCheck(tsData); !ok {
            metrics.PATPMTConsistencyGauge.WithLabelValues(seg.ChannelID).Set(0)
            metrics.SegmentRoundTripErrors.WithLabelValues(seg.ChannelID).Inc()
            // Не блокувати пайплайн, але залогити
            log.Warnf("PAT/PMT inconsistency in segment %d (channel=%s)", seg.SeqNum, seg.ChannelID)
        } else {
            metrics.PATPMTConsistencyGauge.WithLabelValues(seg.ChannelID).Set(1)
        }
    }
    
    return sendToFFmpeg(tsData)
}

func quickPATPMTCheck(tsData []byte) bool {
    dmx := astits.NewDemuxer(ctx, bytes.NewReader(tsData))
    var patPID, pmtPID uint16
    var foundPAT, foundPMT bool
    
    for i := 0; i < 100; i++ {  // обмежити сканування першими 100 пакетами
        d, err := dmx.NextData()
        if err != nil { break }
        if d.PAT != nil {
            foundPAT = true
            patPID = d.PID
            if len(d.PAT.Programs) > 0 {
                pmtPID = d.PAT.Programs[0].ProgramMapID
            }
        }
        if d.PMT != nil && d.PID == pmtPID {
            foundPMT = true
            break
        }
    }
    return foundPAT && foundPMT
}
```

---

## 🧪 Розширення тесту для ваших потреб

### 🔹 Додати перевірку PES-заголовків

```go
// У фазі 4 тесту — після перевірки PMT:
var originalPESRecords []pesRecord
// ... під час першого демуксингу збирати PES:
if d.PES != nil {
    originalPESRecords = append(originalPESRecords, pesRecord{
        pid: d.PID,
        pes: d.PES,
        af:  pkt.AdaptationField,  // якщо потрібно перевірити PCR
    })
}

// Після муксингу — порівняти:
for _, orig := range originalPESRecords {
    // Знайти відповідний PES у rt-потоці
    // Порівняти: StreamID, PTS/DTS flags, optional header fields
    // ⚠️ PES payload не порівнювати — він може бути перепакований
}
```

### 🔹 Перевірка CRC32 таблиць

```go
// astits автоматично перевіряє CRC при демуксингу,
// але можна додати явну перевірку для відладки:

func verifyTableCRC(tableData []byte, expectedCRC uint32) bool {
    // Взяти перші 4 байти після заголовку таблиці як CRC
    // Обчислити CRC32 MPEG-2 для решти даних
    // Порівняти
    return computedCRC == expectedCRC
}

// У тесті:
if rtPAT != nil {
    // Витягти бінарне представлення PAT з buf.Bytes()
    // verifyTableCRC(patBytes, patCRC)
}
```

### 🔹 Тест на wrap-around Continuity Counter

```go
func TestRoundTrip_ContinuityCounterWrap(t *testing.T) {
    // Створити штучний потік з 20+ пакетами одного PID
    // Щоб перевірити, що лічильник 0→1→...→15→0 зберігається
    // Це критично для вашого `wrappingCounter` у segmentAssembler
}
```

---

## 🐛 Поширені проблеми, які виявляє цей тест

| Проблема | Як проявляється у тесті | Як виправити |
|----------|-------------------------|--------------|
| Неправильний PMT PID | `rtPAT.Programs[0].ProgramMapID != originalPMTPID` | Перевірити `WithPMTPID()` у муксері |
| Втрата дескрипторів | `len(originalPMT.ProgramDescriptors) != len(rtPMT...)` | Переконатися, що `muxer.pmt.ProgramDescriptors` присвоюється перед `WriteTables()` |
| Невірний StreamType | `es.StreamType != rtES.StreamType` | Перевірити mapping між вашими внутрішніми типами та `astits.StreamType*` константами |
| CRC помилки | `ErrNoMorePackets` або помилка парсингу у фазі 3 | Перевірити, чи `WriteTables()` коректно обчислює CRC32 через `tableCRC32` |
| PCR PID не збігається | `originalPMT.PCRPID != rtPMT.PCRPID` | Викликати `muxer.SetPCRPID()` перед `WriteTables()` |

---

## 📦 Швидкий чек-лист для вашого `segmentFinalizer`

```go
// ✅ Перед відправкою сегмента у FFmpeg:
func validateSegmentMetadata(tsData []byte, expected Metadata) error {
    dmx := astits.NewDemuxer(ctx, bytes.NewReader(tsData))
    
    // 1. PAT: TransportStreamID
    // 2. PMT: ProgramNumber, PCRPID, кількість ES
    // 3. Кожен ES: PID, StreamType, наявність ключових дескрипторів
    // 4. Опціонально: перший PCR для синхронізації
    
    // Якщо щось не збігається — повернути помилку для retry/log
}

// ✅ У тестах:
func TestSegmentFinalizer_RoundTrip(t *testing.T) {
    // 1. Згенерувати тестовий сегмент через ваш pipeline
    // 2. Запустити validateSegmentMetadata()
    // 3. Запустити повний round-trip як у astits
    // 4. Перевірити, що ffprobe коректно читає результат
}
```

---

## 📚 Корисні посилання

- [astits Muxer API](https://pkg.go.dev/github.com/asticode/go-astits#Muxer)
- [MPEG-TS PSI/SI специфікація](https://www.iso.org/standard/61236.html)
- [Тестові файли astits](https://github.com/asticode/go-astits/tree/master/testdata/ts)

> 💡 **Порада**: Скопіюйте `silent_audio.ts` у ваш тестовий каталог і адаптуйте `TestRoundTrip` для валідації вашого `createTSSegment`. Це дасть вам "безпекову сітку" проти регресій при рефакторингі пайплайну.

Якщо потрібно — можу допомогти:
- 🧩 Адаптувати цей тест під ваш `segmentFinalizer` з урахуванням orphan audio merge
- 🔄 Додати перевірку PTS нормалізації після round-trip
- 🧪 Написати fuzz-тест для генерації випадкових TS-сегментів

🛠️