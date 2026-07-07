# 📦 Глибокий розбір: tsio — MPEG-TS I/O бібліотека

Цей файл — **повноцінна реалізація роботи з MPEG Transport Stream (TS)**, стандартом для цифрового телебачення, HLS-стрімінгу та систем відеоспостереження. Він надає інструменти для парсингу/генерації пакетів, таблиць PSI (PAT/PMT), заголовків PES, та конвертації часових міток.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема tsio пакету

```
┌────────────────────────────────────────┐
│ 📦 tsio — MPEG-TS I/O Engine          │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • PAT/PMT — таблиці програм           │
│  • PSI Header — Program Specific Info  │
│  • PES Header — Packetized Elementary Stream│
│  • TS Packet — 188-байтовий транспортний пакет│
│  • Time converters — PTS/PCR timestamp│
│                                         │
│  📊 Структура MPEG-TS:                  │
│  [TS Packet: 188 bytes]                │
│    ├─ Header (4 bytes)                 │
│    ├─ Adaptation Field (optional)      │
│    └─ Payload (PES/PSI data)           │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet ↔ PES ↔ TS Packet ↔ io.Writer│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. PAT/PMT — таблиці навігації по програмах

### PAT (Program Association Table) — "зміст каналів":

```go
type PATEntry struct {
    ProgramNumber uint16  // ID програми (0 = Network Information Table)
    NetworkPID    uint16  // PID для NIT (якщо ProgramNumber=0)
    ProgramMapPID uint16  // PID для PMT цієї програми
}

type PAT struct {
    Entries []PATEntry
}
```

### 🔍 Формат PAT запису (4 байти):

```
Біти 15-0:  program_number (16 біт)
Біти 15-13: завжди 0b111 (reserved)
Біти 12-0:  network_PID або program_map_PID (13 біт)

Приклад для програми 1 з PMT на PID 0x100:
  [0x00 0x01]  // program_number = 1
  [0xE1 0x00]  // 0b1110 0001 0000 0000 = reserved(3) + PID 0x100
```

### 🔧 Marshal/Unmarshal логіка:

```go
func (self PAT) Marshal(b []byte) (n int) {
    for _, entry := range self.Entries {
        pio.PutU16BE(b[n:], entry.ProgramNumber)  // 2 байти
        n += 2
        if entry.ProgramNumber == 0 {
            // NIT entry: network_PID з прапорцями
            pio.PutU16BE(b[n:], entry.NetworkPID&0x1fff|7<<13)
        } else {
            // Program entry: program_map_PID з прапорцями
            pio.PutU16BE(b[n:], entry.ProgramMapPID&0x1fff|7<<13)
        }
        n += 2
    }
    return
}
```

### ✅ Ваш use-case: створення PAT для однієї програми

```go
// CreateSimplePAT — PAT для одного каналу
func CreateSimplePAT(programNumber uint16, pmtPID uint16) tsio.PAT {
    return tsio.PAT{
        Entries: []tsio.PATEntry{
            {
                ProgramNumber: programNumber,
                ProgramMapPID: pmtPID & 0x1fff,  // маска 13 біт для PID
            },
        },
    }
}

// Використання:
pat := CreateSimplePAT(1, 0x100)  // програма 1, PMT на PID 256
patBytes := make([]byte, pat.Len())
pat.Marshal(patBytes)
// patBytes готовий для запису у PSI секцію
```

---

### PMT (Program Map Table) — "опис потоків програми":

```go
type ElementaryStreamInfo struct {
    StreamType    uint8         // 0x1B=H.264, 0x0F=AAC, 0x24=H.265
    ElementaryPID uint16        // PID цього елементарного потоку
    Descriptors   []Descriptor  // додаткові метадані
}

type PMT struct {
    PCRPID                uint16              // PID для PCR таймінгів
    ProgramDescriptors    []Descriptor        // метадані програми
    ElementaryStreamInfos []ElementaryStreamInfo  // список потоків
}
```

### 🔍 Формат запису Elementary Stream:

```
[1 байт]  stream_type
[2 байти] reserved(3) + elementary_PID(13)
[2 байти] reserved(6) + ES_info_length(10)
[N байт]  descriptors (якщо ES_info_length > 0)
```

### 🔧 parseDescs — парсинг дескрипторів:

```go
func (self PMT) parseDescs(b []byte) (descs []Descriptor, err error) {
    n := 0
    for n < len(b) {
        if n+2 <= len(b) {
            desc := Descriptor{
                Tag:  b[n],           // тип дескриптора
                Data: make([]byte, b[n+1]),  // довжина даних
            }
            n += 2
            if n+len(desc.Data) <= len(b) {
                copy(desc.Data, b[n:])
                descs = append(descs, desc)
                n += len(desc.Data)
            } else {
                break  // недостатньо даних
            }
        } else {
            break
        }
    }
    if n < len(b) {
        err = ErrParsePMT  // залишилися непрочитані байти
        return
    }
    return
}
```

### ✅ Ваш use-case: створення PMT для H.264+AAC потоку

```go
// CreateSimplePMT — PMT для відео+аудіо програми
func CreateSimplePMT(pcrPID, videoPID, audioPID uint16) tsio.PMT {
    return tsio.PMT{
        PCRPID: pcrPID & 0x1fff,
        ElementaryStreamInfos: []tsio.ElementaryStreamInfo{
            {
                StreamType:    tsio.ElementaryStreamTypeH264,  // 0x1B
                ElementaryPID: videoPID & 0x1fff,
                Descriptors:   nil,  // можна додати registration_descriptor тощо
            },
            {
                StreamType:    tsio.ElementaryStreamTypeAdtsAAC,  // 0x0F
                ElementaryPID: audioPID & 0x1fff,
                Descriptors:   nil,
            },
        },
    }
}

// Використання:
pmt := CreateSimplePMT(0x100, 0x101, 0x102)  // PCR=256, Video=257, Audio=258
pmtBytes := make([]byte, pmt.Len())
pmt.Marshal(pmtBytes)
// pmtBytes готовий для запису у PSI секцію
```

---

## 🔑 2. PSI Header — заголовок Program Specific Information

### Формат PSI секції:

```
[1 байт]   pointer_field (зміщення до початку таблиці)
[1 байт]   table_id (0x00=PAT, 0x02=PMT)
[2 байти]  section_syntax_indicator(1) + reserved(2) + section_length(12)
[2 байти]  table_id_extension (transport_stream_id для PAT, program_number для PMT)
[1 байт]   reserved(2) + version_number(5) + current_next_indicator(1)
[1 байт]   section_number
[1 байт]   last_section_number
[N байт]   table data (PAT/PMT payload)
[4 байти]  CRC32
```

### 🔧 ParsePSI — парсинг заголовку:

```go
func ParsePSI(h []byte) (tableid uint8, tableext uint16, hdrlen int, datalen int, err error) {
    // 1. Обробка pointer_field
    pointer := h[0]
    hdrlen++
    if pointer > 0 {
        hdrlen += int(pointer)  // пропуск заповнення до таблиці
        if len(h) < hdrlen {
            err = ErrPSIHeader
            return
        }
    }
    
    // 2. Перевірка мінімального розміру
    if len(h) < hdrlen+12 {
        err = ErrPSIHeader
        return
    }
    
    // 3. Читання основних полів
    tableid = h[hdrlen]  // table_id
    hdrlen++
    
    // section_length (12 біт) мінус заголовок та CRC
    datalen = int(pio.U16BE(h[hdrlen:]))&0x3ff - 9
    hdrlen += 2
    
    if datalen < 0 {
        err = ErrPSIHeader
        return
    }
    
    // table_id_extension
    tableext = pio.U16BE(h[hdrlen:])
    hdrlen += 2
    
    // Пропуск version/current_next/section_number поля
    hdrlen += 3
    
    return
}
```

### 🔧 FillPSI — генерація заголовку:

```go
func FillPSI(h []byte, tableid uint8, tableext uint16, datalen int) (n int) {
    // pointer_field = 0 (таблиця починається одразу)
    h[n] = 0
    n++
    
    // table_id
    h[n] = tableid
    n++
    
    // section_length = 3 (заголовок після цього поля) + 4 (версія/номер) + 2 (CRC) + datalen
    pio.PutU16BE(h[n:], uint16(0xa<<12|2+3+4+datalen))  // 0xa = 0b1010 = syntax_indicator=1, reserved=01
    n += 2
    
    // table_id_extension
    pio.PutU16BE(h[n:], tableext)
    n += 2
    
    // version=0, current_next=1, reserved=0b11
    h[n] = 0x3<<6 | 1  // 0b11000001 = 0xC1
    n++
    
    // section_number = 0, last_section_number = 0
    h[n] = 0
    n++
    h[n] = 0
    n++
    
    // Пропуск місця для даних
    n += datalen
    
    // CRC32 для всієї секції (від table_id до кінця даних)
    crc := calcCRC32(0xffffffff, h[1:n])
    pio.PutU32LE(h[n:], crc)  // little-endian для CRC у MPEG-TS!
    n += 4
    
    return
}
```

### ⚠️ Важливо: CRC у little-endian!

```
MPEG-TS вимагає CRC32 у little-endian форматі, на відміну від більшості інших полів.
Це часта причина помилок при реалізації!

✅ Правильно: pio.PutU32LE(h[n:], crc)
❌ Неправильно: pio.PutU32BE(h[n:], crc)
```

### ✅ Ваш use-case: генерація PAT секції

```go
// GeneratePATSection — створення повної PAT секції для запису у TS
func GeneratePATSection(transportStreamID uint16, entries []tsio.PATEntry) ([]byte, error) {
    pat := tsio.PAT{Entries: entries}
    patData := make([]byte, pat.Len())
    pat.Marshal(patData)
    
    // Розрахунок загального розміру секції
    psiHeaderSize := 9  // pointer(1) + table_id(1) + section_length(2) + table_ext(2) + version(1) + section_num(2)
    crcSize := 4
    totalSize := psiHeaderSize + len(patData) + crcSize
    
    // Виділення буфера
    section := make([]byte, totalSize)
    
    // Заповнення PSI заголовку
    n := tsio.FillPSI(section, tsio.TableIdPAT, transportStreamID, len(patData))
    
    // Копіювання PAT даних
    copy(section[psiHeaderSize:], patData)
    
    // CRC вже заповнено у FillPSI
    
    return section, nil
}

// Використання:
patSection, err := GeneratePATSection(1, []tsio.PATEntry{
    {ProgramNumber: 1, ProgramMapPID: 0x100},
})
if err != nil { /* handle error */ }
// patSection готовий для запису у TS пакет з PID=0
```

---

## 🔑 3. PES Header — Packetized Elementary Stream

### Призначення:
PES обгортає елементарні потоки (відео/аудіо дані) для передачі у TS, додаючи таймінги (PTS/DTS) та метадані.

### 🔧 ParsePESHeader — парсинг:

```go
func ParsePESHeader(h []byte) (hdrlen int, streamid uint8, datalen int, pts, dts time.Duration, err error) {
    // 1. Перевірка start code prefix: 0x000001
    if h[0] != 0 || h[1] != 0 || h[2] != 1 {
        err = ErrPESHeader
        return
    }
    streamid = h[3]  // 0xE0=video, 0xC0=audio
    
    // 2. PES packet length (може бути 0 для variable length)
    datalen = int(pio.U16BE(h[4:6]))
    
    // 3. Flags та довжина заголовку
    flags := h[7]
    hdrlen = int(h[8]) + 9  // 9 = start_code(4) + length(2) + flags(2) + header_len(1)
    
    // 4. Корекція datalen: віднімаємо розмір заголовку
    if datalen > 0 {
        datalen -= int(h[8]) + 3  // +3 = PES_packet_data_byte поля після заголовку
    }
    
    // 5. Парсинг PTS/DTS якщо присутні
    const PTS = 1 << 7
    const DTS = 1 << 6
    
    if flags&PTS != 0 {
        if len(h) < 14 {
            err = ErrPESHeader
            return
        }
        pts = TsToTime(pio.U40BE(h[9:14]))  // 40-бітний timestamp
        if flags&DTS != 0 {
            if len(h) < 19 {
                err = ErrPESHeader
                return
            }
            dts = TsToTime(pio.U40BE(h[14:19]))
        }
    }
    
    return
}
```

### 🔧 FillPESHeader — генерація:

```go
func FillPESHeader(h []byte, streamid uint8, datalen int, pts, dts time.Duration) (n int) {
    // 1. Start code prefix
    h[0] = 0
    h[1] = 0
    h[2] = 1
    h[3] = streamid
    
    // 2. Flags для PTS/DTS
    const PTS = 1 << 7
    const DTS = 1 << 6
    var flags uint8
    if pts != 0 {
        flags |= PTS
        if dts != 0 {
            flags |= DTS
        }
    }
    
    // 3. Розрахунок довжини PES header extensions
    if flags&PTS != 0 {
        n += 5  // 40 біт = 5 байт для PTS
    }
    if flags&DTS != 0 {
        n += 5  // ще 5 байт для DTS
    }
    
    // 4. PES_packet_length (0 = variable length для відео)
    var pktlen uint16
    if datalen >= 0 {
        pktlen = uint16(datalen + n + 3)  // +3 = PES_packet_data_byte поля
    }
    pio.PutU16BE(h[4:6], pktlen)
    
    // 5. PES flags: reserved(2)=0b10, original/copy=1
    h[6] = 2<<6 | 1  // 0b10000001 = 0x81
    h[7] = flags     // PTS/DTS прапорці
    h[8] = uint8(n)  // PES_header_data_length
    
    // 6. Запис PTS/DTS у 40-бітному форматі
    if flags&PTS != 0 {
        if flags&DTS != 0 {
            // Обидва: PTS з marker bits 0b0011, DTS з 0b0001
            pio.PutU40BE(h[9:14], TimeToTs(pts)|3<<36)
            pio.PutU40BE(h[14:19], TimeToTs(dts)|1<<36)
        } else {
            // Тільки PTS з marker bits 0b0010
            pio.PutU40BE(h[9:14], TimeToTs(pts)|2<<36)
        }
    }
    
    n += 9  // загальна довжина заголовку
    return
}
```

### 🔍 40-бітний формат PTS/DTS:

```
Бітова структура (5 байт = 40 біт):
  Біти 39-36: 0010 (для PTS) або 0001 (для DTS) — marker bits
  Біти 35-33: PTS[32..30]
  Біт 32:     1 (marker)
  Біти 31-17: PTS[29..15]
  Біт 16:     1 (marker)
  Біти 15-1:  PTS[14..0]
  Біт 0:      1 (marker)

Функція TimeToTs конвертує time.Duration → 33-бітний timestamp @ 90kHz:
  ts = duration * 90000 / time.Second
  
Потім біти розподіляються по 40-бітному полю з marker bits.
```

### ✅ Ваш use-case: створення PES пакету для відео

```go
// CreateVideoPES — створення PES заголовку для H.264/H.264 відео
func CreateVideoPES(nalu []byte, pts, dts time.Duration, streamID uint8) ([]byte, error) {
    // Максимальний розмір PES заголовку
    header := make([]byte, tsio.MaxPESHeaderLength)
    
    // Генерація заголовку
    hdrlen := tsio.FillPESHeader(header, streamID, len(nalu), pts, dts)
    
    // Об'єднання заголовку + даних
    packet := make([]byte, hdrlen+len(nalu))
    copy(packet, header[:hdrlen])
    copy(packet[hdrlen:], nalu)
    
    return packet, nil
}

// Використання:
nalu := getH264NALU()  // ваші відео дані
pts := 100 * time.Millisecond
dts := 90 * time.Millisecond  // для B-frames

pesPacket, err := CreateVideoPES(nalu, pts, dts, tsio.StreamIdH264)
if err != nil { /* handle error */ }
// pesPacket готовий для запису у TS пакет
```

---

## 🔑 4. TS Packet — 188-байтовий транспортний пакет

### Структура заголовку (4 байти + опціонально адаптаційне поле):

```
Байт 0:  0x47 (sync byte)
Байт 1-2: транспортний прапорець(1) + PID(13) + scrambling(2)
Байт 3:  adaptation_field_control(2) + continuity_counter(4)

Адаптаційне поле (якщо adaptation_field_control & 0x2 != 0):
  Байт 0:  довжина адаптаційного поля
  Байт 1:  прапорці (PCR_flag, OPCR_flag, random_access_indicator, тощо)
  Байти 2-7: PCR (48 біт) якщо PCR_flag встановлено
```

### 🔧 ParseTSHeader — парсинг заголовку:

```go
func ParseTSHeader(tshdr []byte) (pid uint16, start bool, iskeyframe bool, hdrlen int, err error) {
    // 1. Перевірка sync byte
    if tshdr[0] != 0x47 {
        err = fmt.Errorf("tshdr sync invalid")
        return
    }
    
    // 2. Витягування PID (13 біт)
    pid = uint16((tshdr[1]&0x1f))<<8 | uint16(tshdr[2])
    
    // 3. Payload unit start indicator
    start = tshdr[1]&0x40 != 0
    
    // 4. Базова довжина заголовку
    hdrlen = 4
    
    // 5. Обробка адаптаційного поля
    if tshdr[3]&0x20 != 0 {  // adaptation_field_control має біт адаптації
        hdrlen += int(tshdr[4]) + 1  // довжина + байт довжини
        iskeyframe = tshdr[5]&0x40 != 0  // random_access_indicator
    }
    return
}
```

### 🔧 TSWriter — запис пакетів:

```go
type TSWriter struct {
    w                 io.Writer
    ContinuityCounter uint  // 4-бітний лічильник для кожного PID
    tshdr             []byte  // буфер для заголовку (188 байт)
}

func NewTSWriter(pid uint16) *TSWriter {
    w := &TSWriter{}
    w.tshdr = make([]byte, 188)
    w.tshdr[0] = 0x47  // sync byte
    pio.PutU16BE(w.tshdr[1:3], pid&0x1fff)  // PID з прапорцями
    // Заповнення залишку 0xFF для padding
    for i := 6; i < 188; i++ {
        w.tshdr[i] = 0xff
    }
    return w
}
```

### 🔧 WritePackets — основна логіка запису:

```go
func (self *TSWriter) WritePackets(w io.Writer, datav [][]byte, pcr time.Duration, sync bool, paddata bool) (err error) {
    datavlen := pio.VecLen(datav)  // загальна довжина даних
    writev := make([][]byte, len(datav))
    writepos := 0
    
    for writepos < datavlen {
        // 1. Підготовка заголовку
        self.tshdr[1] = self.tshdr[1] & 0x1f  // очистка прапорців
        self.tshdr[3] = byte(self.ContinuityCounter)&0xf | 0x30  // continuity + reserved
        self.tshdr[5] = 0  // адаптаційні прапорці
        hdrlen := 6  // базова довжина заголовку
        self.ContinuityCounter++  // інкремент лічильника
        
        // 2. Обробка першого пакету у послідовності
        if writepos == 0 {
            self.tshdr[1] = 0x40 | self.tshdr[1]  // payload_unit_start_indicator
            if pcr != 0 {
                // Додавання PCR у адаптаційне поле
                hdrlen += 6
                self.tshdr[5] = 0x10 | self.tshdr[5]  // PCR_flag
                pio.PutU48BE(self.tshdr[6:12], TimeToPCR(pcr))
            }
            if sync {
                self.tshdr[5] = 0x40 | self.tshdr[5]  // random_access_indicator
            }
        }
        
        // 3. Розрахунок скільки даних поміститься у пакет
        padtail := 0
        end := writepos + 188 - hdrlen  // доступне місце для даних
        if end > datavlen {
            if paddata {
                // Додати padding якщо потрібно
                padtail = end - datavlen
            } else {
                // Зменшити заголовок щоб уникнути padding
                hdrlen += end - datavlen
            }
            end = datavlen
        }
        
        // 4. Підготовка даних для запису
        n := pio.VecSliceTo(datav, writev, writepos, end)
        
        // 5. Запис адаптаційної довжини
        self.tshdr[4] = byte(hdrlen) - 5  // адаптаційна довжина = hdrlen - 4 (базовий заголовок) - 1 (байт довжини)
        
        // 6. Запис заголовку + даних + padding
        if _, err = w.Write(self.tshdr[:hdrlen]); err != nil { return }
        for i := 0; i < n; i++ {
            if _, err = w.Write(writev[i]); err != nil { return }
        }
        if padtail > 0 {
            if _, err = w.Write(self.tshdr[188-padtail : 188]); err != nil { return }
        }
        
        writepos = end
    }
    return nil
}
```

### ✅ Ваш use-case: запис відео-потоку у TS файл

```go
// WriteVideoToTS — запис H.264 NALU у TS файл
func WriteVideoToTS(filename string, nalus [][]byte, ptsList []time.Duration) error {
    f, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer f.Close()
    
    // Створення TS writer для відео потоку (PID 0x101)
    writer := tsio.NewTSWriter(0x101)
    
    // Запис кожного NALU
    for i, nalu := range nalus {
        // Створення PES пакету
        pesData, err := CreateVideoPES(nalu, ptsList[i], ptsList[i], tsio.StreamIdH264)
        if err != nil {
            return err
        }
        
        // Розбиття PES на 188-байтові TS пакети
        err = writer.WritePackets(f, [][]byte{pesData}, 0, false, true)
        if err != nil {
            return err
        }
    }
    
    return nil
}
```

---

## 🔑 5. Time converters — PTS/PCR timestamp конвертації

### Константи частот:

```go
const (
    PTS_HZ = 90000      // 90 kHz для PTS/DTS (стандарт MPEG)
    PCR_HZ = 27000000   // 27 MHz для PCR (300× PTS для точності)
)
```

### 🔧 TimeToPCR / PCRToTime:

```go
func TimeToPCR(tm time.Duration) (pcr uint64) {
    // Конвертація duration → ticks @ 27MHz
    ts := uint64(tm * PCR_HZ / time.Second)
    
    // Розбиття на base (33 біти) та extension (9 біт)
    base := ts / 300  // base = ticks / 300
    ext := ts % 300   // ext = ticks % 300
    
    // Формат: base(33) + reserved(6) + ext(9)
    pcr = base<<15 | 0x3f<<9 | ext
    return
}

func PCRToTime(pcr uint64) (tm time.Duration) {
    base := pcr >> 15
    ext := pcr & 0x1ff
    ts := base*300 + ext
    tm = time.Duration(ts) * time.Second / time.Duration(PCR_HZ)
    return
}
```

### 🔧 TimeToTs / TsToTime (для PTS/DTS):

```go
func TimeToTs(tm time.Duration) (v uint64) {
    // Конвертація duration → ticks @ 90kHz
    ts := uint64(tm * PTS_HZ / time.Second)
    
    // Формат 33-бітного timestamp з marker bits для 40-бітного PES поля:
    // 0010 + PTS[32..30] + 1 + PTS[29..15] + 1 + PTS[14..0] + 1
    v = ((ts>>30)&0x7)<<33 | ((ts>>15)&0x7fff)<<17 | (ts&0x7fff)<<1 | 0x100010001
    return
}

func TsToTime(v uint64) (tm time.Duration) {
    // Зворотна конвертація: витягування 33 біт з marker bits
    ts := (((v >> 33) & 0x7) << 30) | (((v >> 17) & 0x7fff) << 15) | ((v >> 1) & 0x7fff)
    tm = time.Duration(ts) * time.Second / time.Duration(PTS_HZ)
    return
}
```

### ✅ Ваш use-case: синхронізація аудіо/відео таймінгів

```go
// SyncAudioVideoPTS — корекція аудіо таймінгів під відео
func SyncAudioVideoPTS(videoPTS, audioPTS time.Duration, videoSampleRate, audioSampleRate int) (time.Duration, error) {
    // Конвертація у ticks для порівняння
    videoTicks := uint64(videoPTS * tsio.PTS_HZ / time.Second)
    audioTicks := uint64(audioPTS * tsio.PTS_HZ / time.Second)
    
    // Розрахунок різниці
    diff := int64(videoTicks) - int64(audioTicks)
    
    // Якщо різниця > 100ms — корегуємо
    if abs(diff) > tsio.PTS_HZ/10 {
        log.Printf("A/V sync drift: %d ms", diff*1000/int64(tsio.PTS_HZ))
        // Корекція: зсув аудіо до відео
        return time.Duration(int64(audioPTS) + diff*int64(time.Second)/int64(tsio.PTS_HZ)), nil
    }
    
    return audioPTS, nil
}

func abs(x int64) int64 {
    if x < 0 { return -x }
    return x
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// ts_muxer.go — MPEG-TS muxer для CCTV HLS Processor
type TSMuxer struct {
    channelID    string
    w            io.Writer
    patWriter    *tsio.TSWriter  // PID 0x0000
    pmtWriter    *tsio.TSWriter  // PID 0x1000
    videoWriter  *tsio.TSWriter  // PID 0x1001
    audioWriter  *tsio.TSWriter  // PID 0x1002
    pcrPID       uint16
    continuity   map[uint16]uint  // continuity counter per PID
    metrics      *TSMuxerMetrics
}

func NewTSMuxer(w io.Writer, channelID string) *TSMuxer {
    return &TSMuxer{
        channelID:  channelID,
        w:          w,
        patWriter:  tsio.NewTSWriter(0x0000),
        pmtWriter:  tsio.NewTSWriter(0x1000),
        videoWriter: tsio.NewTSWriter(0x1001),
        audioWriter: tsio.NewTSWriter(0x1002),
        pcrPID:     0x1001,  // PCR у відео потоці
        continuity: make(map[uint16]uint),
        metrics:    NewTSMuxerMetrics(channelID),
    }
}

// WriteHeader — запис PAT/PMT на початку потоку
func (m *TSMuxer) WriteHeader(videoCodec, audioCodec av.CodecData) error {
    // 1. Генерація PAT
    pat := tsio.PAT{
        Entries: []tsio.PATEntry{
            {ProgramNumber: 1, ProgramMapPID: 0x1000},
        },
    }
    patSection, err := GeneratePATSection(1, pat.Entries)
    if err != nil {
        return err
    }
    
    // 2. Запис PAT у TS пакет
    if err := m.patWriter.WritePackets(m.w, [][]byte{patSection}, 0, false, true); err != nil {
        return err
    }
    
    // 3. Генерація PMT
    pmt := tsio.PMT{
        PCRPID: m.pcrPID,
        ElementaryStreamInfos: []tsio.ElementaryStreamInfo{
            {
                StreamType:    tsio.ElementaryStreamTypeH264,
                ElementaryPID: 0x1001,
            },
            {
                StreamType:    tsio.ElementaryStreamTypeAdtsAAC,
                ElementaryPID: 0x1002,
            },
        },
    }
    pmtSection, err := GeneratePMTSection(1, pmt)
    if err != nil {
        return err
    }
    
    // 4. Запис PMT у TS пакет
    if err := m.pmtWriter.WritePackets(m.w, [][]byte{pmtSection}, 0, false, true); err != nil {
        return err
    }
    
    m.metrics.HeadersWritten.Inc()
    return nil
}

// WriteVideoPacket — запис відео пакету з синхронізацією
func (m *TSMuxer) WriteVideoPacket(nalu []byte, pts, dts time.Duration, isKeyFrame bool) error {
    start := time.Now()
    
    // 1. Створення PES пакету
    pesData, err := CreateVideoPES(nalu, pts, dts, tsio.StreamIdH264)
    if err != nil {
        return err
    }
    
    // 2. Визначення чи це початок нового доступу (для random_access_indicator)
    sync := isKeyFrame
    
    // 3. PCR тільки для ключових кадрів або періодично
    var pcr time.Duration
    if isKeyFrame {
        pcr = pts  // PCR синхронізований з PTS для ключових кадрів
    }
    
    // 4. Запис у TS
    err = m.videoWriter.WritePackets(m.w, [][]byte{pesData}, pcr, sync, true)
    if err != nil {
        return err
    }
    
    m.metrics.VideoPacketsWritten.Inc()
    m.metrics.WriteLatency.Observe(time.Since(start).Seconds())
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"tshdr sync invalid"** | Перший байт не 0x47 | Переконайтеся, що читаєте з правильної позиції; можливе зміщення через попередні помилки |
| **CRC не співпадає** | Little-endian vs big-endian плутанина | MPEG-TS CRC32 завжди little-endian; використовуйте `pio.PutU32LE` |
| **PTS/DTS не коректні** | Неправильний marker bits у 40-бітному полі | Використовуйте `TimeToTs()` + правильні marker bits: PTS=0b0010, DTS=0b0001 |
| **Continuity counter помилки** | Лічильник не інкрементується або скидається | Зберігайте окремий лічильник для кожного PID; інкремент після кожного пакету |
| **PCR розсинхронізація** | PCR не оновлюється достатньо часто | Вставляйте PCR кожні 40-100ms або при кожному ключовому кадрі |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування TS заголовків для кожного PID:

```go
type TSHeaderCache struct {
    mu      sync.RWMutex
    headers map[uint16][]byte  // PID → pre-filled 188-byte header template
}

func (c *TSHeaderCache) Get(pid uint16) []byte {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if hdr, ok := c.headers[pid]; ok {
        // Повертаємо копію щоб уникнути race condition
        result := make([]byte, 188)
        copy(result, hdr)
        return result
    }
    return nil
}

func (c *TSHeaderCache) Set(pid uint16, hdr []byte) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.headers == nil {
        c.headers = make(map[uint16][]byte)
    }
    c.headers[pid] = hdr
}
```

### 2. Пакетний запис для зменшення системних викликів:

```go
// BatchWritePackets — запис кількох PES пакетів за один виклик
func (w *TSWriter) BatchWritePackets(writer io.Writer, pesPackets [][]byte, pcr time.Duration) error {
    // Об'єднання даних для мінімізації overhead
    var combined [][]byte
    for _, pes := range pesPackets {
        combined = append(combined, pes)
    }
    return w.WritePackets(writer, combined, pcr, false, true)
}
```

### 3. Моніторинг продуктивності muxing:

```go
type TSMuxerMetrics struct {
    PacketsWritten prometheus.CounterVec
    WriteLatency   prometheus.HistogramVec
    ContinuityErrors prometheus.CounterVec
    PCRDistribution prometheus.HistogramVec
}

func (m *TSMuxerMetrics) RecordPacket(pid uint16, duration time.Duration, channelID string) {
    m.PacketsWritten.WithLabelValues(fmt.Sprintf("0x%X", pid), channelID).Inc()
    m.WriteLatency.WithLabelValues(channelID).Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист інтеграції tsio

```go
// ✅ 1. Правильне ініціалізування TS writer для кожного PID
videoWriter := tsio.NewTSWriter(0x1001)
audioWriter := tsio.NewTSWriter(0x1002)

// ✅ 2. Генерація PAT/PMT перед початком потоку
pat := CreateSimplePAT(1, 0x1000)
pmt := CreateSimplePMT(0x1001, 0x1001, 0x1002)

// ✅ 3. Коректна конвертація часу
pts := tsio.TimeToTs(100 * time.Millisecond)  // для PES
pcr := tsio.TimeToPCR(100 * time.Millisecond)  // для адаптаційного поля

// ✅ 4. Інкремент continuity counter для кожного PID
// TSWriter робить це автоматично, але переконайтеся що не створюєте новий writer для кожного пакету

// ✅ 5. Вставка PCR періодично (кожні 40-100ms) або при ключових кадрах
if isKeyFrame || time.Since(lastPCR) > 40*time.Millisecond {
    pcr = currentPTS
    lastPCR = time.Now()
}

// ✅ 6. Обробка помилок запису
err := writer.WritePackets(w, [][]byte{pesData}, pcr, sync, true)
if err != nil {
    log.Printf("TS write failed: %v", err)
    return err
}

// ✅ 7. Метрики для моніторингу
metrics.RecordPacket(pid, time.Since(start), channelID)
```

---

## 🔗 Корисні посилання

- 📄 [ISO/IEC 13818-1:2022 — MPEG-TS Specification](https://www.iso.org/standard/82746.html) — офіційний стандарт
- 📄 [MPEG-TS Packet Structure](https://en.wikipedia.org/wiki/MPEG_transport_stream) — візуальна схема пакету
- 📄 [PES Packet Format](https://wiki.multimedia.cx/index.php/PES) — детальний опис заголовків
- 📄 [HLS TS Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до TS у HLS
- 💻 [vdk tsio Package](https://pkg.go.dev/github.com/deepch/vdk/format/tsio) — GoDoc documentation

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Вставляйте PCR кожні 40-100ms** — це критично для синхронізації плеєрів; без регулярних PCR відео може "зависати".
> 2. **Використовуйте окремий TSWriter для кожного PID** — continuity counter має бути незалежним для кожного потоку.
> 3. **Кешуйте TS заголовки** — створення нового 188-байтового буфера для кожного пакету марнує пам'ять.
> 4. **Валідуйте PTS/DTS порядок** — DTS має бути ≤ PTS; неправильний порядок зламає декодування B-frames.
> 5. **Моніторьте continuity counter помилки** — різкі стрибки можуть вказувати на втрачені пакети або помилки мережі.

Потрібен приклад інтеграції `TSMuxer` з вашим `pubsub.Queue` для розподілу вже сформованих TS пакетів між підписниками (HLS сегментатор, архів, WebSocket)? Готовий допомогти! 🚀