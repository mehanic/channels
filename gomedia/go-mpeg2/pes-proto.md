# 📦 Глибокий розбір `mpeg2/pes.go` — PES пакети для MPEG-TS у CCTV HLS Processor

Це **низькорівнева реалізація стандарту ISO/IEC 13818-1 (MPEG-2 Systems)** для роботи з PES (Packetized Elementary Stream) пакетами. PES — це проміжний шар між сирими відео/аудіо кадрами та транспортним потоком MPEG-TS, який додає часові мітки (PTS/DTS), синхронізацію та метадані. Розберемо архітектурно:

---

## 🧱 1. Архітектура MPEG: від кадрів до транспортного потоку

```
[Raw Video/Audio Frames]
         ↓
[PES Packet] ← цей файл!
├─ Заголовок: PTS/DTS, прапорці, довжина
├─ Payload: сирі кадри (H.264 NAL units, AAC frames)
         ↓
[TS Packet] (188 байт, фіксований розмір)
├─ Заголовок: PID, continuity counter, adaptation field
├─ Payload: частини PES пакету
         ↓
[.ts файл / UDP потік / HLS сегмент]
```

### 🔑 Чому PES потрібен?

| Проблема | Рішення через PES |
|----------|------------------|
| **Синхронізація аудіо/відео** | PTS (Presentation Time Stamp) у кожному пакеті |
| **B-frames у відео** | Окремі PTS/DTS: DTS для декодування, PTS для відтворення |
| **Довільні розміри кадрів** | PES_packet_length поле для динамічної довжини |
| **Метадані** | Прапорці: copyright, original/copy, scrambling control |
| **Сумісність** | Стандартний формат для DVB, ATSC, HLS, RTMP→TS конвертації |

---

## 🔢 2. PES Stream IDs: ідентифікація потоків

### 🔧 Константи `PES_STREMA_ID` (опечатка: має бути `STREAM`):

```go
const (
    PES_STREAM_END         PES_STREMA_ID = 0xB9  // MPEG-2 program_end_code
    PES_STREAM_START       PES_STREMA_ID = 0xBA  // MPEG-2 pack header
    PES_STREAM_SYSTEM_HEAD PES_STREMA_ID = 0xBB  // system_header
    PES_STREAM_MAP         PES_STREMA_ID = 0xBC  // program_stream_map
    PES_STREAM_PRIVATE     PES_STREMA_ID = 0xBD  // private_stream_1 (субтитри, metadata)
    PES_STREAM_AUDIO       PES_STREMA_ID = 0xC0  // audio_stream_0 (база для AAC/MP3)
    PES_STREAM_VIDEO       PES_STREMA_ID = 0xE0  // video_stream_0 (база для H.264/H.265)
)
```

### 🔍 Динамічний розрахунок Stream ID:

```go
// Аудіо: 0xC0 + stream_index (0-31)
// Відео: 0xE0 + stream_index (0-15)

func findPESIDByStreamType(cid TS_STREAM_TYPE) PES_STREMA_ID {
    switch cid {
    case TS_STREAM_AAC, TS_STREAM_AUDIO_MPEG1, TS_STREAM_AUDIO_MPEG2:
        return PES_STREAM_AUDIO  // 0xC0
    case TS_STREAM_H264, TS_STREAM_H265:
        return PES_STREAM_VIDEO  // 0xE0
    default:
        return PES_STREAM_PRIVATE  // fallback для невідомих кодеків
    }
}
```

### 🎯 Практичне застосування у вашому пайплайні:

```go
// У TSMuxer при створенні нового потоку:
func (muxer *TSMuxer) addStream(codecID codec.CodecID) uint16 {
    pid := muxer.nextPID
    muxer.nextPID++
    
    streamType := codecToStreamType(codecID)  // H.264 → 0x1B, AAC → 0x0F
    streamID := findPESIDByStreamType(streamType)  // H.264 → 0xE0, AAC → 0xC0
    
    muxer.streams[pid] = &Stream{
        PID: pid,
        StreamType: streamType,
        StreamID: uint8(streamID),  // зберегти для PES заголовків
        // ...
    }
    return pid
}
```

> 💡 **Важливо**: `PES_STREAM_AUDIO = 0xC0` — це база. Реальний Stream ID = `0xC0 + channel_index`. Для стерео аудіо з двома каналами: канал 0 → 0xC0, канал 1 → 0xC1.

---

## 📦 3. `PesPacket` структура — повна карта заголовку PES

### 🔍 Специфікація заголовку PES (ISO/IEC 13818-1, Section 2.4.3.6):

```
Байти 0-2:  packet_start_code_prefix = 0x000001 (24 біти)
Байт   3:   stream_id (8 біт) — тип потоку (0xE0=відео, 0xC0=аудіо)
Байти 4-5:  PES_packet_length (16 біт) — довжина решти пакета (0 = необмежено)

[Опціональний PES header, якщо PES_packet_length > 0]
Байт 6:   [10][scrambling:2][priority:1][alignment:1][copyright:1][original:1]
Байт 7:   [PTS_DTS:2][ESCR:1][ES_rate:1][trick_mode:1][copy_info:1][CRC:1][extension:1]
Байт 8:   PES_header_data_length (8 біт) — довжина решти заголовку

[Опціональні поля, залежно від прапорців]
• Якщо PTS_DTS_flags = 0x02: PTS (33 біти у спеціальному форматі)
• Якщо PTS_DTS_flags = 0x03: PTS + DTS (обидва 33 біти)
• Якщо ESCR_flag = 1: ESCR base + extension
• Якщо ES_rate_flag = 1: ES_rate (22 біти)
• Якщо trick_mode_flag = 1: trick_mode_control + value
• Якщо copy_info_flag = 1: additional_copy_info (7 біт)
• Якщо CRC_flag = 1: previous_PES_packet_CRC (16 біт)
• Якщо extension_flag = 1: розширення (не реалізовано у коді)

[Payload]
• PES_packet_data_byte: сирі дані (відео/аудіо кадри)
```

### 🔧 Структура Go з коментарями:

```go
type PesPacket struct {
    // === Обов'язкові поля ===
    Stream_id         uint8   // 0xE0=відео, 0xC0=аудіо
    PES_packet_length uint16  // довжина після цього поля (0 = необмежено)
    
    // === Основний заголовок (байт 6) ===
    PES_scrambling_control    uint8  // 0=не зашифровано
    PES_priority              uint8  // 1=вищий пріоритет
    Data_alignment_indicator  uint8  // 1=дані вирівняні (наприклад, початок GOP)
    Copyright                 uint8  // 1=захищено авторським правом
    Original_or_copy          uint8  // 1=оригінал, 0=копія
    
    // === Прапорці опціональних полів (байт 7) ===
    PTS_DTS_flags             uint8  // 0x00=немає, 0x02=PTS, 0x03=PTS+DTS
    ESCR_flag                 uint8  // 1=є ESCR (Elementary Stream Clock Reference)
    ES_rate_flag              uint8  // 1=є ES_rate (бітрейт потоку)
    DSM_trick_mode_flag       uint8  // 1=режим швидкого перемотування
    Additional_copy_info_flag uint8  // 1=додаткова інформація про копіювання
    PES_CRC_flag              uint8  // 1=є CRC попереднього пакета
    PES_extension_flag        uint8  // 1=є розширення (не реалізовано)
    
    // === Довжина решти заголовку ===
    PES_header_data_length    uint8  // кількість байт після цього поля до payload
    
    // === Часові мітки (33 біти кожна, кодуються у 5 байт) ===
    Pts uint64  // Presentation Time Stamp: коли відтворювати кадр (90 kHz clock)
    Dts uint64  // Decoding Time Stamp: коли декодувати кадр (для B-frames)
    
    // === Інші опціональні поля ===
    ESCR_base                 uint64  // базова частина ESCR (33 біти)
    ESCR_extension            uint16  // розширення ESCR (9 біт)
    ES_rate                   uint32  // бітрейт потоку у байтах/секунду
    Trick_mode_control        uint8   // тип трюкового режиму (швидкість, freeze, etc.)
    Trick_value               uint8   // параметр трюкового режиму
    Additional_copy_info      uint8   // додаткова інформація про копіювання
    Previous_PES_packet_CRC   uint16  // CRC попереднього пакета для перевірки цілісності
    
    // === Payload ===
    Pes_payload []byte  // сирі відео/аудіо дані (H.264 NAL units, AAC frames, etc.)
}
```

---

## ⏱️ 4. PTS/DTS кодування: 33 біти у 5 байт — магія специфікації

### 🔍 Чому 33 біти і чому таке дивне кодування?

```
MPEG використовує 90 kHz clock для часових міток:
• 33 біти → діапазон 0..8,589,934,591
• При 90,000 ticks/сек → ~26.5 годин до переповнення

Формат кодування (специфікація, Section 2.4.2.2):
Біти: [4][3][1][15][1][15][1] = 40 біт = 5 байт
      ↑  ↑  ↑   ↑   ↑   ↑   ↑
      |  |  |   |   |   |   └─ marker_bit = 1
      |  |  |   |   |   └───── PTS[14:0] (молодші 15 біт)
      |  |  |   |   └───────── marker_bit = 1
      |  |  |   └───────────── PTS[29:15] (середні 15 біт)
      |  |  └───────────────── marker_bit = 1
      |  └──────────────────── PTS[32:30] (старші 3 біти)
      └─────────────────────── '0010' для PTS, '0011' для PTS+DTS
```

### 🔧 Декодування у `Decode()`:

```go
if pkg.PTS_DTS_flags&0x02 == 0x02 {  // є PTS
    bs.SkipBits(4)                    // пропустити '0010'
    
    // Старші 3 біти
    pkg.Pts = bs.GetBits(3)           // PTS[32:30]
    bs.SkipBits(1)                    // marker_bit
    
    // Середні 15 біт
    pkg.Pts = (pkg.Pts << 15) | bs.GetBits(15)  // PTS[29:15]
    bs.SkipBits(1)                    // marker_bit
    
    // Молодші 15 біт
    pkg.Pts = (pkg.Pts << 15) | bs.GetBits(15)  // PTS[14:0]
    bs.SkipBits(1)                    // marker_bit
}

// Якщо є DTS, аналогічно, але з префіксом '0011'
if pkg.PTS_DTS_flags&0x03 == 0x03 {
    // ... аналогічний код для Dts ...
} else {
    // Якщо немає DTS, він дорівнює PTS (для потоків без B-frames)
    pkg.Dts = pkg.Pts
}
```

### 🔧 Кодування у `Encode()`:

```go
if pkg.PTS_DTS_flags == 0x02 {  // тільки PTS
    bsw.PutUint8(0x02, 4)        // префікс '0010'
    
    bsw.PutUint64(pkg.Pts>>30, 3)  // старші 3 біти
    bsw.PutUint8(0x01, 1)          // marker_bit
    
    bsw.PutUint64(pkg.Pts>>15, 15) // середні 15 біт
    bsw.PutUint8(0x01, 1)          // marker_bit
    
    bsw.PutUint64(pkg.Pts, 15)     // молодші 15 біт
    bsw.PutUint8(0x01, 1)          // marker_bit
}
```

### 📐 Приклад розрахунку:

```
PTS = 90,000 (1 секунда @ 90 kHz)

Бітове представлення: 90,000 = 0b0000_0000_0000_0001_0101_1111_0101_0000
• Старші 3 біти [32:30]: 0b000 = 0
• Середні 15 біт [29:15]: 0b000_0000_0000_0001 = 1
• Молодші 15 біт [14:0]:  0b0101_1111_0101_0000 = 24,400

Кодування у 5 байт:
Байт 0: 0b0010_0000 = 0x20  // '0010' + PTS[32:30]=000
Байт 1: 0b0_______ = 0x80   // marker_bit + верхні біти середньої частини
... (детальне кодування залежить від бітового порядку)

Результат: [0x21, 0x00, 0x01, 0x5F, 0x50] (приблизно)
```

> 💡 **Практичне значення**: У вашому `segmentAssembler` PTS використовується для:
> 1. Розрахунку тривалості сегменту (#EXTINF у HLS)
> 2. Синхронізації аудіо/відео через порівняння PTS
> 3. Генерації #EXT-X-PROGRAM-DATE-TIME з абсолютним часом

---

## 🔧 5. `Decode()` — парсинг PES заголовку з бітового потоку

### 🔍 Кроки парсингу:

#### Крок 1: Перевірка мінімальної довжини

```go
if bs.RemainBytes() < 9 {
    return errNeedMore  // недостатньо даних для базового заголовку
}
```

#### Крок 2: Читання обов'язкових полів

```go
bs.SkipBits(24)             // packet_start_code_prefix = 0x000001
pkg.Stream_id = bs.Uint8(8) // stream_id (0xE0/0xC0)
pkg.PES_packet_length = bs.Uint16(16)  // довжина решти пакета
```

#### Крок 3: Основний заголовок (байти 6-7)

```go
bs.SkipBits(2)  // '10' — завжди встановлені біти
pkg.PES_scrambling_control = bs.Uint8(2)  // 2 біти
pkg.PES_priority = bs.Uint8(1)             // 1 біт
pkg.Data_alignment_indicator = bs.Uint8(1) // 1 біт
pkg.Copyright = bs.Uint8(1)                // 1 біт
pkg.Original_or_copy = bs.Uint8(1)         // 1 біт

// Прапорці опціональних полів
pkg.PTS_DTS_flags = bs.Uint8(2)
pkg.ESCR_flag = bs.Uint8(1)
// ... інші прапорці ...
pkg.PES_extension_flag = bs.Uint8(1)

pkg.PES_header_data_length = bs.Uint8(8)  // довжина решти заголовку
```

#### Крок 4: Перевірка наявності даних для опціональних полів

```go
if bs.RemainBytes() < int(pkg.PES_header_data_length) {
    bs.UnRead(9 * 8)  // відмотати назад на початок заголовку
    return errNeedMore
}
```

#### Крок 5: Парсинг PTS/DTS та інших опціональних полів

```go
bs.Markdot()  // позначити поточну позицію для розрахунку зміщення

// PTS (якщо прапорець встановлено)
if pkg.PTS_DTS_flags&0x02 == 0x02 {
    // ... кодування 33 біт у 5 байт, як описано вище ...
}

// Аналогічно для DTS, ESCR, ES_rate, тощо...
```

#### Крок 6: Пропуск невикористаних байт заголовку

```go
loc := bs.DistanceFromMarkDot()  // скільки біт вже прочитано з опціональних полів
bs.SkipBits(int(pkg.PES_header_data_length)*8 - loc)  // пропустити решту
```

#### Крок 7: Читання payload

```go
// Розрахунок довжини payload:
// PES_packet_length включає:
// • 2 байти: PES_packet_length поле саме
// • 1 байт: PES_header_data_length поле  
// • PES_header_data_length байт: решта заголовку
// • решта: payload

dataLen := int(pkg.PES_packet_length - 3 - uint16(pkg.PES_header_data_length))

if bs.RemainBytes() < dataLen {
    // Недостатньо даних для повного payload
    pkg.Pes_payload = bs.RemainData()
    bs.UnRead((9 + int(pkg.PES_header_data_length)) * 8)  // відмотати для повторної спроби
    return errNeedMore
}

// Читання повного payload
if pkg.PES_packet_length == 0 || bs.RemainBytes() <= dataLen {
    // Необмежена довжина або кінець потоку
    pkg.Pes_payload = bs.RemainData()
    bs.SkipBits(bs.RemainBits())
} else {
    pkg.Pes_payload = bs.RemainData()[:dataLen]
    bs.SkipBits(dataLen * 8)
}
```

### ⚠️ Потенційні проблеми:

1. **`errNeedMore` не визначено у цьому файлі**:
   ```go
   return errNeedMore  // ← де оголошено цю помилку?
   // Має бути: var errNeedMore = errors.New("need more data")
   ```

2. **`UnRead()` може не працювати коректно**:
   ```go
   bs.UnRead(9 * 8)  // ← чи підтримує BitStream відмотування?
   // Якщо ні — це призведе до втрати даних при інкрементальному парсингу
   ```

3. **Некоректний розрахунок `dataLen` для `PES_packet_length = 0`**:
   ```go
   // Якщо PES_packet_length = 0 (необмежено), формула дає від'ємне значення!
   dataLen := int(pkg.PES_packet_length - 3 - uint16(pkg.PES_header_data_length))
   // Має бути окрема обробка для випадку 0:
   if pkg.PES_packet_length == 0 {
       dataLen = bs.RemainBytes()  // читати до кінця потоку
   } else {
       dataLen = int(pkg.PES_packet_length - 3 - uint16(pkg.PES_header_data_length))
   }
   ```

4. **Помилка у кодуванні ESCR_base**:
   ```go
   // У Decode():
   pkg.ESCR_base = (pkg.Pts << 15) | bs.GetBits(15)  // ← використовує pkg.Pts замість pkg.ESCR_base!
   // Має бути:
   pkg.ESCR_base = (pkg.ESCR_base << 15) | bs.GetBits(15)
   ```

---

## 🎞️ 6. `DecodeMpeg1()` — спрощений парсинг для MPEG-1

### 🔍 Відмінності MPEG-1 PES:

```
MPEG-1 має простіший заголовок:
• Немає прапорців для опціональних полів
• PTS/DTS кодуються безпосередньо після заголовку
• Немає ESCR, ES_rate, trick_mode, etc.

Формат:
[0x000001][stream_id][length]
[stuffing bytes: 0xFF...]
[optional: STD buffer scale/size]
[PTS або PTS+DTS у тому ж форматі 33-біт]
[payload]
```

### 🔧 Ключова логіка:

```go
// Пропуск stuffing bytes (0xFF)
for bs.NextBits(8) == 0xFF {
    bs.SkipBits(8)
}

// Детекція типу часової мітки за першими 4 бітами
if bs.NextBits(4) == 0x02 {  // '0010' = тільки PTS
    // ... парсинг PTS ...
} else if bs.NextBits(4) == 0x03 {  // '0011' = PTS+DTS
    // ... парсинг PTS та DTS ...
} else if bs.NextBits(8) == 0x0F {  // stuffing або інше
    bs.SkipBits(8)
} else {
    return errParser  // невідомий формат
}
```

### 🎯 Коли використовується:

```
MPEG-1 PES використовується для:
• Старих камер/енкодерів
• MP3 аудіо у MPEG-TS
• Сумісність з legacy плеєрами

У вашому CCTV пайплайні: якщо камера передає MPEG-1 замість MPEG-2,
цей метод забезпечує зворотну сумісність без зміни основної логіки.
```

---

## ✍️ 7. `Encode()` — серіалізація PES пакету

### 🔧 Структура кодування:

```go
func (pkg *PesPacket) Encode(bsw *codec.BitStreamWriter) {
    // 1. Обов'язковий префікс
    bsw.PutBytes([]byte{0x00, 0x00, 0x01})  // packet_start_code_prefix
    bsw.PutByte(pkg.Stream_id)                // stream_id
    bsw.PutUint16(pkg.PES_packet_length, 16)  // довжина
    
    // 2. Основний заголовок (байт 6)
    bsw.PutUint8(0x02, 2)  // '10' — завжди встановлено
    bsw.PutUint8(pkg.PES_scrambling_control, 2)
    bsw.PutUint8(pkg.PES_priority, 1)
    bsw.PutUint8(pkg.Data_alignment_indicator, 1)
    bsw.PutUint8(pkg.Copyright, 1)
    bsw.PutUint8(pkg.Original_or_copy, 1)
    
    // 3. Прапорці (байт 7)
    bsw.PutUint8(pkg.PTS_DTS_flags, 2)
    bsw.PutUint8(pkg.ESCR_flag, 1)
    // ... інші прапорці ...
    
    bsw.PutByte(pkg.PES_header_data_length)  // довжина решти заголовку
    
    // 4. PTS (якщо потрібно)
    if pkg.PTS_DTS_flags == 0x02 {
        bsw.PutUint8(0x02, 4)  // префікс '0010'
        // ... кодування 33 біт у 5 байт, як у Decode() ...
    }
    
    // 5. PTS+DTS (якщо потрібно)
    if pkg.PTS_DTS_flags == 0x03 {
        bsw.PutUint8(0x03, 4)  // префікс '0011'
        // ... кодування PTS ...
        // ... кодування DTS ...
    }
    
    // 6. Інші опціональні поля (ESCR, ES_rate, etc.)
    
    // 7. Payload
    bsw.PutBytes(pkg.Pes_payload)
}
```

### 🎯 Практичне застосування у `TSMuxer`:

```go
func (muxer *TSMuxer) writePESPacket(stream *Stream, frame []byte, pts, dts uint64) error {
    pes := &PesPacket{
        Stream_id: stream.StreamID,
        PTS_DTS_flags: 0x03,  // PTS+DTS для підтримки B-frames
        Pts: pts,
        Dts: dts,
        Pes_payload: frame,
        // ... інші поля за замовчуванням ...
    }
    
    // Розрахунок довжини заголовку
    headerLen := 3 + 3  // 3 байти префікс+stream_id+length + 3 байти PTS/DTS прапорці
    if pes.PTS_DTS_flags != 0 {
        headerLen += 5  // +5 байт для PTS
        if pes.PTS_DTS_flags == 0x03 {
            headerLen += 5  // +5 байт для DTS
        }
    }
    pes.PES_header_data_length = uint8(headerLen - 3)  // -3 для pre-fixed полів
    
    // Розрахунок загальної довжини
    if len(frame) + headerLen <= 0xFFFF {
        pes.PES_packet_length = uint16(len(frame) + headerLen)
    } else {
        pes.PES_packet_length = 0  // необмежена довжина
    }
    
    // Серіалізація
    bsw := codec.NewBitStreamWriter(2048)
    pes.Encode(bsw)
    
    // Розділення на TS пакети (188 байт)
    return muxer.writeTSPackets(bsw.Bits(), stream.PID)
}
```

---

## 🎯 8. Інтеграція з вашим CCTV HLS Processor

### 📍 У `TSMuxer` — пакування кадрів у TS:

```go
type TSMuxer struct {
    streams map[uint16]*Stream  // PID → Stream info
    nextPID uint16
}

func (muxer *TSMuxer) WriteVideoFrame(codecID codec.CodecID, frame []byte, pts, dts uint64) error {
    // 1. Знайти або створити потік для цього кодеку
    stream := muxer.getOrCreateStream(codecID, TS_STREAM_TYPE_VIDEO)
    
    // 2. Додати AUD (Access Unit Delimiter) для H.264/H.265
    if codecID == codec.CODECID_VIDEO_H264 {
        frame = append(H264_AUD_NALU, frame...)  // 0x00000001 0x09F0
    } else if codecID == codec.CODECID_VIDEO_H265 {
        frame = append(H265_AUD_NALU, frame...)  // 0x00000001 0x460150
    }
    
    // 3. Створити PES пакет
    pes := &PesPacket{
        Stream_id: uint8(PES_STREAM_VIDEO) + stream.Index,
        PTS_DTS_flags: 0x03,  // завжди PTS+DTS для відео
        Pts: pts,
        Dts: dts,
        Data_alignment_indicator: 1,  // вирівнювання на початку GOP
        Pes_payload: frame,
    }
    
    // 4. Серіалізувати та розділити на TS пакети
    return muxer.writePESPacket(stream, pes)
}
```

### 📍 У `HLSGenerator` — конвертація TS → HLS:

```go
func (gen *HLSGenerator) processTSSegment(tsData []byte) (*HLSSegment, error) {
    // 1. Парсинг TS пакетів → PES → кадри
    parser := NewTSParser()
    frames, err := parser.Parse(tsData)
    if err != nil {
        return nil, fmt.Errorf("parse TS: %w", err)
    }
    
    // 2. Розрахунок тривалості сегменту за PTS
    if len(frames) == 0 {
        return nil, errors.New("empty segment")
    }
    firstPTS := frames[0].PTS
    lastPTS := frames[len(frames)-1].PTS
    duration := float64(lastPTS - firstPTS) / 90000.0  // конвертація 90 kHz → секунди
    
    // 3. Генерація #EXTINF та #EXT-X-PROGRAM-DATE-TIME
    segment := &HLSSegment{
        Duration: duration,
        ProgramDateTime: time.Unix(0, int64(firstPTS)*1e9/90000),  // PTS → UTC
        // ... інші поля ...
    }
    
    return segment, nil
}
```

### 📍 У `RTMPToTSConverter` — транскодування на льоту:

```go
func (conv *RTMPToTSConverter) onVideoFrame(codecid codec.CodecID, frame []byte, pts, dts uint32) {
    // 1. Конвертація FLV timestamp → MPEG-TS PTS (90 kHz clock)
    tsPTS := uint64(pts) * 90  // FLV: 1 ms, MPEG-TS: 1/90000 sec
    tsDTS := uint64(dts) * 90
    
    // 2. Пакування у PES → TS
    conv.tsMuxer.WriteVideoFrame(codecid, frame, tsPTS, tsDTS)
    
    // 3. Якщо накопичено достатньо даних для HLS сегменту → записати файл
    if conv.tsMuxer.SegmentReady() {
        segmentData := conv.tsMuxer.FlushSegment()
        conv.hlsGenerator.WriteSegment(segmentData)
    }
}
```

---

## 🐞 9. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Помилка у кодуванні ESCR_base**:
   ```go
   // У Decode():
   pkg.ESCR_base = (pkg.Pts << 15) | bs.GetBits(15)  // ← pkg.Pts замість pkg.ESCR_base!
   // Має бути:
   pkg.ESCR_base = (pkg.ESCR_base << 15) | bs.GetBits(15)
   ```

2. **`errNeedMore` не визначено**:
   ```go
   // Додати у файл:
   var (
       errNeedMore = errors.New("need more data to parse PES packet")
       errParser   = errors.New("invalid PES packet format")
   )
   ```

3. **Некоректна обробка `PES_packet_length = 0`**:
   ```go
   // Розрахунок dataLen дає від'ємне значення при length=0
   // Має бути:
   var dataLen int
   if pkg.PES_packet_length == 0 {
       dataLen = bs.RemainBytes()  // читати до кінця
   } else {
       dataLen = int(pkg.PES_packet_length - 3 - uint16(pkg.PES_header_data_length))
       if dataLen < 0 {
           return errParser  // невалідна довжина
       }
   }
   ```

4. **Відсутня валідація `PES_header_data_length`**:
   ```go
   // Якщо PES_header_data_length > 255 або менше необхідного мінімуму
   if pkg.PES_header_data_length > 255 {
       return errParser
   }
   // Мінімальна довжина заголовку при PTS_DTS_flags=0x03: 10 байт
   if pkg.PTS_DTS_flags == 0x03 && pkg.PES_header_data_length < 10 {
       return errParser
   }
   ```

5. **`UnRead()` може не підтримуватися**:
   ```go
   // Якщо BitStream не підтримує відмотування, це призведе до втрати даних
   // Краще зберігати позицію перед парсингом і відновлювати її:
   pos := bs.Position()
   // ... парсинг ...
   if error {
       bs.Seek(pos)  // відновити позицію
       return errNeedMore
   }
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечного кодування/декодування 33-бітних часових міток
func EncodeTimestamp(ts uint64, prefix uint8) []byte {
    buf := make([]byte, 5)
    buf[0] = prefix<<4 | byte(ts>>30)
    buf[1] = 0x80 | byte(ts>>22)  // marker_bit + біти
    buf[2] = byte(ts>>15)
    buf[3] = 0x80 | byte(ts>>7)
    buf[4] = 0x80 | byte(ts)
    return buf
}

func DecodeTimestamp(buf []byte, prefix uint8) (uint64, error) {
    if len(buf) < 5 || (buf[0]>>4) != prefix {
        return 0, errors.New("invalid timestamp format")
    }
    ts := uint64(buf[0]&0x07)<<30 |
          uint64(buf[1]&0x7F)<<22 |
          uint64(buf[2])<<15 |
          uint64(buf[3]&0x7F)<<7 |
          uint64(buf[4]&0x7F)
    return ts, nil
}

// 2. Методи для перевірки валідності заголовку
func (pkg *PesPacket) Validate() error {
    if pkg.Stream_id < 0xC0 || pkg.Stream_id > 0xEF {
        return fmt.Errorf("invalid stream_id: 0x%02x", pkg.Stream_id)
    }
    if pkg.PTS_DTS_flags > 0x03 {
        return fmt.Errorf("invalid PTS_DTS_flags: 0x%02x", pkg.PTS_DTS_flags)
    }
    // ... інші перевірки ...
    return nil
}

// 3. Юніт-тести для PTS/DTS roundtrip
func TestTimestampEncodeDecode(t *testing.T) {
    tests := []uint64{0, 1, 90000, 1<<33 - 1}  // 0, 1ms, 1s, max value
    for _, ts := range tests {
        encoded := EncodeTimestamp(ts, 0x02)  // PTS prefix
        decoded, err := DecodeTimestamp(encoded, 0x02)
        if err != nil {
            t.Errorf("DecodeTimestamp(%x) error: %v", encoded, err)
        }
        if decoded != ts {
            t.Errorf("roundtrip failed: %d → %x → %d", ts, encoded, decoded)
        }
    }
}
```

---

## 🧭 Висновок: чому PES — критичний шар для MPEG-TS/HLS

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **PTS/DTS кодування** | Точна синхронізація аудіо/відео, підтримка B-frames | Розсинхронізація, "стрибаюче" відео, артефакти відтворення |
| **Stream ID мапінг** | Коректна ідентифікація потоків у мультиплексі | Аудіо/відео переплутані, плеєр не може розпарсити потік |
| **Опціональні прапорці** | Гнучкість для шифрування, метаданих, трюкових режимів | Неможливість підтримки розширених функцій (DRM, HDR metadata) |
| **Інкрементальний парсинг** | Обробка потоків реального часу без буферизації всього пакета | Висока затримка, неможливість live-трансляції |
| **MPEG-1 сумісність** | Підтримка старих камер/енкодерів | Неможливість інтеграції з legacy обладнанням |

> 🔑 **Головна ідея**: Цей файл — **протокольний міст** між сирими медіа-кадрами та транспортним потоком. Він гарантує, що:
> 1. Кожен кадр має коректну часову мітку для синхронізації
> 2. Потоки ідентифікуються стандартним чином для сумісності з плеєрами
> 3. Опціональні функції (шифрування, метадані) підтримуються без ламання базової логіки

Без нього ваш `TSMuxer` змушений був би реалізовувати власну логіку кодування часових міток, мапінгу потоків та обробки прапорців — це призвело б до дублювання коду, помилок у синхронізації та важкої підтримки.

💡 **Фінальна порада**: 
1. Виправте помилку з `pkg.Pts` у кодуванні `ESCR_base`
2. Додайте визначення `errNeedMore` та `errParser`
3. Додайте валідацію `PES_packet_length = 0` та `PES_header_data_length`
4. Реалізуйте `EncodeTimestamp`/`DecodeTimestamp` helpers для тестування
5. Додайте юніт-тести для PTS/DTS roundtrip з граничними значеннями

Це перетворить цей файл з "робочої реалізації" на "гарантовано коректний протокольний шар" для всього вашого MPEG-TS/HLS пайплайну.