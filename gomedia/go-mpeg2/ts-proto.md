# 📦 Глибокий розбір `mpeg2/ts_types.go` — типи даних та парсинг MPEG-2 Transport Stream

Це **фундаментальний шар абстракції** для роботи з форматом MPEG-2 Transport Stream (TS) у вашому CCTV HLS Processor. Файл визначає структури даних, константи та реалізує парсинг/серіалізацію ключових компонентів специфікації: TS пакети, адаптаційні поля, PAT/PMT таблиці. Розберемо архітектурно:

---

## 🧱 1. Система констант: PID, Table ID, Stream Type

### 🔑 Типізовані константи для type-safe коду:

```go
// PID (Packet Identifier) — унікальний ідентифікатор потоку у 13-бітному полі
type TS_PID int
const (
    TS_PID_PAT TS_PID = 0x0000  // Program Association Table (обов'язково на PID 0)
    TS_PID_CAT        = 0x0001  // Conditional Access Table
    TS_PID_TSDT       = 0x0002  // Transport Stream Description Table
    TS_PID_IPMP       = 0x0003  // IPMP Control Information
    TS_PID_Nil        = 0x1FFF  // Null packets (заповнення, ігнорується декодерами)
)

// Table ID — ідентифікатор типу секції у PSI (Program Specific Information)
type PAT_TID int
const (
    TS_TID_PAS PAT_TID = 0x00  // Program Association Section (PAT)
    TS_TID_CAS         = 0x01  // Conditional Access Section (CA)
    TS_TID_PMS         = 0x02  // Program Map Section (PMT)
    TS_TID_SDS         = 0x03  // Transport Stream Description Section
    TS_TID_FORBIDDEN   = 0xFF  // Зарезервовано
)

// Stream Type — ідентифікатор кодеку у PMT
type TS_STREAM_TYPE int
const (
    TS_STREAM_AUDIO_MPEG1 TS_STREAM_TYPE = 0x03  // MPEG-1 Audio Layer I/II
    TS_STREAM_AUDIO_MPEG2 TS_STREAM_TYPE = 0x04  // MPEG-2 Audio Layer II (MP3)
    TS_STREAM_AAC         TS_STREAM_TYPE = 0x0F  // AAC-LC (Advanced Audio Coding)
    TS_STREAM_H264        TS_STREAM_TYPE = 0x1B  // H.264/AVC
    TS_STREAM_H265        TS_STREAM_TYPE = 0x24  // H.265/HEVC
)

// Фіксований розмір пакету — основа архітектури TS
const TS_PAKCET_SIZE = 188  // ⚠️ Опечатка: має бути PACKET_SIZE
```

### 🎯 Чому типізовані константи критичні?

```
Без type-safe констант легко припуститися помилок:
• Плутанина між PID (13 біт) та Stream Type (8 біт)
• Використання "магічних чисел" (0x1B) замість TS_STREAM_H264
• Неможливість відстежити використання значень через IDE

З type-safe константами:
• Компілятор відхилить некоректні присвоєння
• IDE покаже автодоповнення з описом кожного значення
• Рефакторинг: зміна внутрішнього представлення не ламає зовнішній API
```

> 💡 **Практичне значення**: Ці константи — **контракт між модулями**, який гарантує, що всі компоненти пайплайну "говорять однією мовою" про ідентифікатори потоків, типи таблиць та кодеки.

---

## 📦 2. TS пакет: `TSPacket` — атомна одиниця передачі

### 🔍 Специфікація заголовку (Section 2.4.3.2):

```
Байт 0:   sync_byte = 0x47 (завжди!)
Байт 1:   [transport_error:1][payload_unit_start:1][transport_priority:1][PID:5 старші біти]
Байт 2:   [PID:8 молодших біт]
Байт 3:   [transport_scrambling:2][adaptation_field_control:2][continuity_counter:4]

[Опціонально] Адаптаційне поле (якщо adaptation_field_control & 0x02 != 0)
[Обов'язково] Payload (якщо adaptation_field_control & 0x01 != 0)
```

### 🔧 Структура Go:

```go
type TSPacket struct {
    // Заголовок (4 байти)
    Transport_error_indicator    uint8  // 1 біт: помилка передачі
    Payload_unit_start_indicator uint8  // 1 біт: початок нового PES/секції
    Transport_priority           uint8  // 1 біт: пріоритет пакету
    PID                          uint16 // 13 біт: ідентифікатор потоку
    Transport_scrambling_control uint8  // 2 біти: шифрування (0=немає)
    Adaptation_field_control     uint8  // 2 біти: 01=тільки payload, 10=тільки адаптація, 11=обидва
    Continuity_counter           uint8  // 4 біти: лічильник 0-15 для детекції втрат
    
    // Опціональні поля
    Field   *Adaptation_field  // Адаптаційне поле (PCR, random access, тощо)
    Payload interface{}        // Дані: *PesPacket, *Pat, *Pmt, або []byte
}
```

### 🔧 `EncodeHeader()`: серіалізація 4-байтового заголовку

```go
func (pkg *TSPacket) EncodeHeader(bsw *codec.BitStreamWriter) {
    bsw.PutByte(0x47)  // sync_byte — завжди 0x47
    
    // Байт 1: 8 біт у порядку специфікації
    bsw.PutUint8(pkg.Transport_error_indicator, 1)
    bsw.PutUint8(pkg.Payload_unit_start_indicator, 1)
    bsw.PutUint8(pkg.Transport_priority, 1)
    bsw.PutUint16(pkg.PID, 13)  // ⚠️ PID — 13 біт, не 16!
    
    // Байт 3: решта полів
    bsw.PutUint8(pkg.Transport_scrambling_control, 2)
    bsw.PutUint8(pkg.Adaptation_field_control, 2)
    bsw.PutUint8(pkg.Continuity_counter, 4)
    
    // Адаптаційне поле (якщо потрібно)
    if pkg.Field != nil && (pkg.Adaptation_field_control&0x02) != 0 {
        pkg.Field.Encode(bsw)
    }
}
```

### ⚠️ Критична проблема: `PutUint16(pkg.PID, 13)`

```go
// BitStreamWriter.PutUint16(value, bits) має коректно обробляти значення < 16 біт
// Але якщо реалізація просто записує 16 біт → старші 3 біти PID будуть неправильними!

// Приклад: PID = 0x100 (256) = 0b0001_0000_0000
// Правильно: записати молодші 13 біт = 0b000_0000_0000_0000 → 0x000
// Неправильно: записати всі 16 біт → 0x100, що змінить наступні поля!

// Краще: явна маска перед записом
bsw.PutUint16(pkg.PID & 0x1FFF, 13)  // 0x1FFF = 13 біт '1'
```

### 🔧 `DecodeHeader()`: десеріалізація з валідацією

```go
func (pkg *TSPacket) DecodeHeader(bs *codec.BitStream) error {
    // 1. Перевірка sync_byte
    sync_byte := bs.Uint8(8)
    if sync_byte != 0x47 {
        return errors.New("ts packet must start with 0x47")  // ⚠️ Краще спеціальну помилку
    }
    
    // 2. Читання полів заголовку
    pkg.Transport_error_indicator = bs.GetBit()
    pkg.Payload_unit_start_indicator = bs.GetBit()
    pkg.Transport_priority = bs.GetBit()
    pkg.PID = bs.Uint16(13)  // ⚠️ Той самий ризик з 13-бітним читанням
    pkg.Transport_scrambling_control = bs.Uint8(2)
    pkg.Adaptation_field_control = bs.Uint8(2)
    pkg.Continuity_counter = bs.Uint8(4)
    
    // 3. Пропуск null пакетів
    if pkg.PID == TS_PID_Nil {
        return nil
    }
    
    // 4. Парсинг адаптаційного поля (якщо потрібно)
    if pkg.Adaptation_field_control == 0x02 || pkg.Adaptation_field_control == 0x03 {
        if pkg.Field == nil {
            pkg.Field = new(Adaptation_field)
        }
        err := pkg.Field.Decode(bs)
        if err != nil {
            return err
        }
    }
    return nil
}
```

### 🎯 Чому `Payload_unit_start_indicator` критичний?

```
Цей біт сигналізує декодеру:
• 1 = цей пакет починає нову структуру вищого рівня:
  - Для PID=0 (PAT): початок нової секції PAT
  - Для PID=PMT: початок нової секції PMT  
  - Для елементарних потоків: початок нового PES пакету

Без коректної обробки цього біта:
• Декодер не зможе знайти початок секцій → не розпарсить PAT/PMT
• PES пакети будуть розрізані неправильно → пошкоджені кадри
• Синхронізація аудіо/відео буде втрачена

У вашому пайплайні: цей біт використовується у TSDemuxer.Input() для вирішення: "чи парсити заголовок секції/PES, чи тільки додати payload до буфера".
```

---

## ⚙️ 3. Адаптаційне поле: `Adaptation_field` — метадані для синхронізації

### 🔍 Специфікація (Section 2.4.3.3):

```
Адаптаційне поле — опціональна структура для:
• PCR (Program Clock Reference): синхронізація системного годинника
• Random access indicator: маркер точки входу (IDR кадр)
• Stuffing bytes: вирівнювання розміру пакету до 188 байт
• Розширення: splicing, private data, тощо

Структура:
Байт 0: adaptation_field_length (кількість наступних байт)
Байт 1: [discontinuity:1][random_access:1][priority:1][PCR:1][OPCR:1][splice:1][private:1][extension:1]
[Опціонально] PCR: 6 байт (33 біти base + 6 reserved + 9 біт extension)
[Опціонально] Інші поля за прапорцями
[Обов'язково] Stuffing bytes для вирівнювання
```

### 🔧 Структура Go з усіма полями:

```go
type Adaptation_field struct {
    // Прапорець для оптимізації: один байт stuffing
    SingleStuffingByte bool
    
    // Основні поля
    Adaptation_field_length                    uint8   // довжина решти поля
    Discontinuity_indicator                    uint8   // розрив у потоці
    Random_access_indicator                    uint8   // точка входу для декодера
    Elementary_stream_priority_indicator       uint8   // пріоритет потоку
    PCR_flag                                   uint8   // чи присутній PCR
    OPCR_flag                                  uint8   // оригінальний PCR (для сплайсингу)
    Splicing_point_flag                        uint8   // точка монтажу
    Transport_private_data_flag                uint8   // приватні дані
    Adaptation_field_extension_flag            uint8   // розширення поля
    
    // PCR (найважливіше поле для синхронізації)
    Program_clock_reference_base               uint64  // 33 біти @ 27 MHz
    Program_clock_reference_extension          uint16  // 9 біт @ 27 MHz
    
    // Інші опціональні поля (не реалізовані повністю)
    // ...
    
    // Stuffing для вирівнювання
    Stuffing_byte                              uint8
}
```

### 🔧 `Encode()`: серіалізація з динамічним розрахунком довжини

```go
func (adaptation *Adaptation_field) Encode(bsw *codec.BitStreamWriter) {
    // Резервувати місце для adaptation_field_length
    loc := bsw.ByteOffset()
    bsw.PutUint8(adaptation.Adaptation_field_length, 8)
    
    // Оптимізація: один байт stuffing
    if adaptation.SingleStuffingByte {
        return  // length вже встановлено, більше нічого не потрібно
    }
    
    bsw.Markdot()  // позначити початок даних для розрахунку довжини
    
    // Запис прапорців (байт 1)
    bsw.PutUint8(adaptation.Discontinuity_indicator, 1)
    bsw.PutUint8(adaptation.Random_access_indicator, 1)
    // ... інші прапорці ...
    
    // PCR (якщо потрібно)
    if adaptation.PCR_flag == 1 {
        bsw.PutUint64(adaptation.Program_clock_reference_base, 33)  // ⚠️ 33 біти!
        bsw.PutUint8(0, 6)  // reserved
        bsw.PutUint16(adaptation.Program_clock_reference_extension, 9)  // ⚠️ 9 біт!
    }
    
    // ... інші опціональні поля ...
    
    // Розрахунок фактичної довжини
    adaptation.Adaptation_field_length = uint8(bsw.DistanceFromMarkDot() / 8)
    
    // Stuffing bytes для вирівнювання
    bsw.PutRepetValue(0xff, int(adaptation.Stuffing_byte))
    adaptation.Adaptation_field_length += adaptation.Stuffing_byte
    
    // Запис фактичної довжини у зарезервоване місце
    bsw.SetByte(adaptation.Adaptation_field_length, loc)
}
```

### ⚠️ Критична проблема: `PutUint64(value, 33)` та `PutUint16(value, 9)`

```go
// BitStreamWriter має коректно обробляти запис довільної кількості біт
// Але якщо реалізація просто записує 64/16 біт → зайві біти потраплять у наступні поля!

// Приклад для PCR_base (33 біти):
// Правильно: записати тільки молодші 33 біти значення
// Неправильно: записати всі 64 біти → наступні 31 біт "з'їдять" reserved та PCR_ext

// Краще: явна маска перед записом
bsw.PutUint64(adaptation.Program_clock_reference_base & 0x1FFFFFFFF, 33)  // 33 біти '1'
bsw.PutUint16(adaptation.Program_clock_reference_extension & 0x1FF, 9)     // 9 біт '1'
```

### 🔧 `Decode()`: парсинг з обробкою stuffing

```go
func (adaptation *Adaptation_field) Decode(bs *codec.BitStream) error {
    // 1. Читання довжини
    if bs.RemainBytes() < 1 {
        return errors.New("len of data < 1 byte")
    }
    adaptation.Adaptation_field_length = bs.Uint8(8)
    
    // 2. Перевірка наявності даних
    startoffset := bs.ByteOffset()
    if bs.RemainBytes() < int(adaptation.Adaptation_field_length) {
        return errors.New("len of data < Adaptation_field_length")
    }
    if adaptation.Adaptation_field_length == 0 {
        return nil  // порожнє адаптаційне поле
    }
    
    // 3. Читання прапорців
    adaptation.Discontinuity_indicator = bs.GetBit()
    adaptation.Random_access_indicator = bs.GetBit()
    // ... інші прапорці ...
    
    // 4. PCR (якщо потрібно)
    if adaptation.PCR_flag == 1 {
        adaptation.Program_clock_reference_base = bs.GetBits(33)  // ⚠️ 33 біти!
        bs.SkipBits(6)  // reserved
        adaptation.Program_clock_reference_extension = uint16(bs.GetBits(9))  // ⚠️ 9 біт!
    }
    
    // 5. Пропуск не реалізованих полів
    if adaptation.Transport_private_data_flag == 1 {
        adaptation.Transport_private_data_length = bs.Uint8(8)
        bs.SkipBits(8 * int(adaptation.Transport_private_data_length))
    }
    
    // 6. Пропуск stuffing bytes
    endoffset := bs.ByteOffset()
    bs.SkipBits((int(adaptation.Adaptation_field_length) - (endoffset - startoffset)) * 8)
    return nil
}
```

### 🎯 Чому PCR найважливіше поле адаптаційного поля?

```
PCR (Program Clock Reference) — 42-бітне значення для синхронізації:
• Частота: 27 MHz → точність до 37 ns
• Формат: 33 біти base + 9 біт extension
• Призначення:
  - Синхронізація аудіо/відео потоків у декодері
  - Відновлення тактової частоти після розривів передачі
  - Запобігання buffer underflow/overflow

Специфікація вимагає:
• Передавати PCR щонайменше кожні 100 ms
• Передавати у потоці з найвищим пріоритетом (зазвичай відео)
• Точність: ±500 ns

У вашому коді: PCR розраховується у TSMuxer.writePES() як dts*300 (конвертація 90 kHz → 27 MHz), але є помилка у бітовому розділенні (див. вище).
```

---

## 🗂️ 4. Таблиці PSI: `Pat` та `Pmt` — мапінг програм та потоків

### 🔸 PAT (Program Association Table):

```go
type Pat struct {
    Table_id                 uint8      // завжди 0x00 для PAT
    Section_syntax_indicator uint8      // завжди 1 для PAT/PMT
    Section_length           uint16     // довжина решти секції (12 біт)
    Transport_stream_id      uint16     // ідентифікатор транспортного потоку
    Version_number           uint8      // версія таблиці (5 біт)
    Current_next_indicator   uint8      // 1=поточна, 0=наступна версія
    Section_number           uint8      // номер секції (для розбиття великих таблиць)
    Last_section_number      uint8      // останній номер секції
    Pmts                     []PmtPair  // масив записів: program_number → PMT PID
}

type PmtPair struct {
    Program_number uint16  // 0x0000 = NIT, інші = program number
    PID            uint16  // PID, на якому передається PMT цієї програми
}
```

### 🔸 PMT (Program Map Table):

```go
type Pmt struct {
    Table_id                 uint8        // завжди 0x02 для PMT
    Section_syntax_indicator uint8        // завжди 1
    Section_length           uint16       // довжина решти секції (12 біт)
    Program_number           uint16       // program number цієї програми
    Version_number           uint8        // версія таблиці (5 біт)
    Current_next_indicator   uint8        // 1=поточна, 0=наступна
    Section_number           uint8        // номер секції
    Last_section_number      uint8        // останній номер секції
    PCR_PID                  uint16       // PID потоку, що несе PCR (13 біт)
    Program_info_length      uint16       // довжина дескрипторів програми (12 біт)
    Streams                  []StreamPair // масив елементарних потоків програми
}

type StreamPair struct {
    StreamType     uint8   // тип кодеку (TS_STREAM_TYPE)
    Elementary_PID uint16  // PID цього потоку (13 біт)
    ES_Info_Length uint16  // довжина дескрипторів потоку (12 біт)
}
```

### 🔧 `Encode()` для PAT: серіалізація з CRC32

```go
func (pat *Pat) Encode(bsw *codec.BitStreamWriter) {
    bsw.PutUint8(0x00, 8)  // Table_id = 0x00
    
    // Резервувати місце для Section_length
    loc := bsw.ByteOffset()
    bsw.PutUint8(pat.Section_syntax_indicator, 1)
    bsw.PutUint8(0x00, 1)  // reserved
    bsw.PutUint8(0x03, 2)  // reserved
    bsw.PutUint16(0, 12)   // тимчасове значення Section_length
    
    bsw.Markdot()  // позначити початок даних
    
    // Запис полів заголовку
    bsw.PutUint16(pat.Transport_stream_id, 16)
    bsw.PutUint8(0x03, 2)  // reserved
    bsw.PutUint8(pat.Version_number, 5)
    bsw.PutUint8(pat.Current_next_indicator, 1)
    bsw.PutUint8(pat.Section_number, 8)
    bsw.PutUint8(pat.Last_section_number, 8)
    
    // Запис записів PMT
    for _, pms := range pat.Pmts {
        bsw.PutUint16(pms.Program_number, 16)
        bsw.PutUint8(0x07, 3)  // reserved
        bsw.PutUint16(pms.PID, 13)  // ⚠️ PID — 13 біт!
    }
    
    // Розрахунок фактичної довжини
    length := bsw.DistanceFromMarkDot()
    pat.Section_length = uint16(length)/8 + 4  // +4 для заголовку секції
    bsw.SetUint16(pat.Section_length&0x0FFF|(uint16(pat.Section_syntax_indicator)<<15)|0x3000, loc)
    
    // Розрахунок CRC32
    crc := codec.CalcCrc32(0xffffffff, bsw.Bits()[bsw.ByteOffset()-int(pat.Section_length-4)-3:bsw.ByteOffset()])
    tmpcrc := make([]byte, 4)
    binary.LittleEndian.PutUint32(tmpcrc, crc)  // ⚠️ Little-endian для CRC!
    bsw.PutBytes(tmpcrc)
}
```

### 🎯 Чому CRC у Little-endian?

```
Специфікація MPEG-2 вимагає CRC32 у little-endian форматі для PSI секцій,
на відміну від більшості інших полів, що використовують big-endian.

Це історична особливість стандарту:
• CRC розраховується на байтовому рівні
• Записується у порядку байт: [LSB, ..., MSB]

У вашому коді:
binary.LittleEndian.PutUint32(tmpcrc, crc)  // ✓ коректно

Якщо використати BigEndian → декодери відкинуть таблицю як пошкоджену.
```

### 🎯 Практичне застосування у `TSDemuxer`:

```go
// При отриманні PAT (PID=0):
if pkg.PID == uint16(TS_PID_PAT) {
    if pkg.Payload_unit_start_indicator == 1 {
        bs.SkipBits(8)  // пропустити pointer_field
    }
    pkg.Payload, err = ReadSection(TS_TID_PAS, bs)
    pat := pkg.Payload.(*Pat)
    
    // Зареєструвати PMT PID для кожної програми
    for _, pmt := range pat.Pmts {
        if pmt.Program_number != 0x0000 {  // пропустити NIT
            if _, found := demuxer.programs[pmt.PID]; !found {
                demuxer.programs[pmt.PID] = &tsprogram{
                    pn: pmt.Program_number,
                    streams: make(map[uint16]*tsstream),
                }
            }
        }
    }
}
```

---

## 🐞 5. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Опечатка у константі**:
   ```go
   const TS_PAKCET_SIZE = 188  // ← має бути TS_PACKET_SIZE
   // Це ламає консистентність, ускладнює пошук та автодоповнення в IDE
   ```

2. **Некоректна обробка 13-бітних полів (PID)**:
   ```go
   // У TSPacket.EncodeHeader():
   bsw.PutUint16(pkg.PID, 13)  // ← якщо BitStreamWriter не маскує старші біти → помилка!
   
   // Краще:
   bsw.PutUint16(pkg.PID & 0x1FFF, 13)  // 0x1FFF = 13 біт '1'
   
   // Аналогічно у DecodeHeader(), Pat.Encode(), Pmt.Encode(), тощо.
   ```

3. **Некоректна обробка 33/9-бітних полів (PCR)**:
   ```go
   // У Adaptation_field.Encode():
   bsw.PutUint64(adaptation.Program_clock_reference_base, 33)  // ← ризик запису зайвих біт!
   bsw.PutUint16(adaptation.Program_clock_reference_extension, 9)
   
   // Краще:
   bsw.PutUint64(adaptation.Program_clock_reference_base & 0x1FFFFFFFF, 33)  // 33 біти '1'
   bsw.PutUint16(adaptation.Program_clock_reference_extension & 0x1FF, 9)     // 9 біт '1'
   ```

4. **`panic` замість повернення помилки**:
   ```go
   // У Adaptation_field.Decode():
   if bitscount%8 > 0 {
       panic("maybe parser ts file failed")  // ← crash сервера!
   }
   
   // Краще:
   if bitscount%8 > 0 {
       return fmt.Errorf("adaptation field extension not byte-aligned: %d bits", bitscount%8)
   }
   ```

5. **Відсутня підтримка дескрипторів у PAT/PMT**:
   ```go
   // У Pat.Encode()/Decode() та Pmt.Encode()/Decode():
   // Program_info_length та ES_Info_Length парситься, але дескриптори просто пропускаються:
   bs.SkipBits(int(pmt.Program_info_length) * 8)
   
   // Але дескриптори можуть містити критичну інформацію:
   // • language codes для аудіо
   // • aspect ratio для відео
   // • HDR metadata
   // Краще: парсити хоча б базові дескриптори або логувати попередження
   ```

6. **Некоректний розрахунок довжини секції**:
   ```go
   // У Pat.Encode():
   pat.Section_length = uint16(length)/8 + 4
   // Але length — у бітах, тому ділення на 8 коректне ✓
   // Однак: якщо не кратне 8 → втрата дробової частини!
   // Краще: перевірити кратність або використовувати бітову арифметику
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечного запису 13-бітних полів
func PutPID(bsw *codec.BitStreamWriter, pid uint16) {
    bsw.PutUint16(pid & 0x1FFF, 13)  // маска 13 біт
}

func GetPID(bs *codec.BitStream) uint16 {
    return bs.Uint16(13) & 0x1FFF  // маска після читання
}

// 2. Helper для PCR розрахунку
func EncodePCR(timestamp90kHz uint64) (base uint64, ext uint16) {
    pcr_value := timestamp90kHz * 300  // 90 kHz → 27 MHz
    base = pcr_value >> 9              // старші 33 біти
    ext = uint16(pcr_value & 0x1FF)    // молодші 9 біт
    return
}

func DecodePCR(base uint64, ext uint16) uint64 {
    return (base << 9) | uint64(ext)  // конвертація назад у 90 kHz
}

// 3. Валідація полів після парсингу
func (pat *Pat) Validate() error {
    if pat.Table_id != uint8(TS_TID_PAS) {
        return fmt.Errorf("invalid PAT table_id: 0x%02x", pat.Table_id)
    }
    if pat.Section_syntax_indicator != 1 {
        return errors.New("PAT section_syntax_indicator must be 1")
    }
    // ... інші перевірки ...
    return nil
}

// 4. Юніт-тести для енкодинг/декодинг roundtrip
func TestPat_EncodeDecode(t *testing.T) {
    original := &Pat{
        Transport_stream_id: 0x1234,
        Version_number: 5,
        Pmts: []PmtPair{
            {Program_number: 1, PID: 0x100},
            {Program_number: 2, PID: 0x101},
        },
    }
    
    bsw := codec.NewBitStreamWriter(256)
    original.Encode(bsw)
    
    bs := codec.NewBitStream(bsw.Bits())
    var decoded Pat
    if err := decoded.Decode(bs); err != nil {
        t.Fatalf("decode error: %v", err)
    }
    
    if decoded.Transport_stream_id != original.Transport_stream_id {
        t.Errorf("Transport_stream_id mismatch: got 0x%04x, want 0x%04x", 
            decoded.Transport_stream_id, original.Transport_stream_id)
    }
    // ... перевірка інших полів ...
}

// 5. Підтримка базових дескрипторів
type Descriptor struct {
    Tag  uint8
    Length uint8
    Data []byte
}

func ParseDescriptors(bs *codec.BitStream, length uint16) ([]Descriptor, error) {
    var descs []Descriptor
    end := bs.Position() + int(length)*8
    for bs.Position() < end {
        if bs.RemainBits() < 16 {
            return nil, errNeedMore
        }
        desc := Descriptor{
            Tag: bs.Uint8(8),
            Length: bs.Uint8(8),
        }
        if bs.RemainBits() < int(desc.Length)*8 {
            return nil, errNeedMore
        }
        desc.Data = bs.GetBytes(int(desc.Length))
        descs = append(descs, desc)
    }
    return descs, nil
}
```

---

## 🎯 6. Інтеграція з вашим CCTV HLS Processor

### 📍 У `TSDemuxer` — парсинг вхідного потоку:

```go
func (demuxer *TSDemuxer) Input(r io.Reader) error {
    // ... ініціалізація ...
    
    for {
        // Читання та синхронізація пакету через probe()
        buf, err := demuxer.probe(r)
        if err != nil { /* обробка */ }
        
        // Парсинг заголовку
        bs := codec.NewBitStream(buf[:TS_PAKCET_SIZE])
        var pkg TSPacket
        if err := pkg.DecodeHeader(bs); err != nil {
            return err
        }
        
        // Обробка за PID
        switch pkg.PID {
        case uint16(TS_PID_PAT):
            // Парсинг PAT → реєстрація PMT PID
            // ...
        case pmtPID:  // PID з PAT
            // Парсинг PMT → реєстрація елементарних потоків
            // ...
        default:
            // Елементарний потік: пошук у demuxer.programs → обробка PES
            // ...
        }
    }
}
```

### 📍 У `TSMuxer` — генерація вихідного потоку:

```go
func (mux *TSMuxer) Write(pid uint16, data []byte, pts, dts uint64) error {
    // ... підготовка ...
    
    // Періодична генерація PAT/PMT
    if mux.pat_period == 0 || mux.pat_period+400 < dts {
        mux.writePat(tmppat)  // PID=0
        for _, pmt := range mux.pat.pmts {
            mux.writePmt(tmppmt, pmt)  // PID з table_pmt
        }
        mux.pat_period = dts
    }
    
    // Пакування даних у PES → розбиття на 188-байтові пакети
    mux.writePES(whichstream, whichpmt, data, pts*90, dts*90, idr_flag, withaud)
    
    return nil
}

func (mux *TSMuxer) writePat(pat *Pat) {
    var tshdr TSPacket
    tshdr.PID = 0  // PAT завжди на PID 0
    tshdr.Continuity_counter = mux.pat.cc
    mux.pat.cc = (mux.pat.cc + 1) % 16
    
    bsw := codec.NewBitStreamWriter(TS_PAKCET_SIZE)
    tshdr.EncodeHeader(bsw)
    bsw.PutByte(0x00)  // pointer_field
    pat.Encode(bsw)    // серіалізація з CRC
    bsw.FillRemainData(0xff)  // stuffing до 188 байт
    mux.OnPacket(bsw.Bits())  // відправка
}
```

### 📍 У метриках — моніторинг якості парсингу:

```go
func (demuxer *TSDemuxer) recordParseMetrics(pid uint16, err error) {
    var typeName string
    switch pid {
    case uint16(TS_PID_PAT): typeName = "PAT"
    case uint16(TS_PID_Nil): typeName = "null"
    default:
        if pid >= 0x100 && pid <= 0x1FFE {
            typeName = "elementary"
        } else {
            typeName = "unknown"
        }
    }
    
    if err == nil {
        metrics.TSPacketsParsed.WithLabelValues(typeName).Inc()
    } else {
        metrics.TSPacketsErrors.WithLabelValues(typeName, err.Error()).Inc()
    }
}
```

---

## 🧭 Висновок: чому цей файл — фундамент бінарної сумісності

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **Type-safe константи** | Коректна ідентифікація потоків/таблиць/кодеків | Плутанина між PID/Stream Type → неправильний парсинг → втрата даних |
| **TSPacket з 13-бітним PID** | Сумісність зі специфікацією (2^13 = 8192 можливі PID) | Неправильне кодування → декодери не розпізнають пакети → "невалідний TS" помилки |
| **Adaptation_field з PCR** | Синхронізація системного годинника для аудіо/відео | Розсинхронізація → "роз'їзд" звуку та відео, артефакти буфера у плеєрах |
| **PAT/PMT з CRC32** | Валідація цілісності мапи програм/потоків | Пошкоджені таблиці → неправильна інтерпретація потоків → чорний екран/тиша |
| **Динамічний розрахунок довжин** | Коректна серіалізація змінних секцій | Невалідні секції → відхилення декодерами, неможливість seek |

> 🔑 **Головна ідея**: Цей файл — **бінарний контракт** між вашим кодом та специфікацією MPEG-2 Systems. Він гарантує, що:
> 1. Кожен байт заголовку кодується/декодується згідно стандарту
> 2. 13-бітні, 33-бітні, 9-бітні поля обробляються коректно без переповнення
> 3. Метадані потоків (PID, stream_type, PCR) передаються точно
> 4. Цілісність даних перевіряється через CRC32 у little-endian

Без нього кожен компонент пайплайну змушений був би реалізовувати власну логіку парсингу заголовків, що призвело б до дублювання коду, помилок у синхронізації та важкої підтримки.

💡 **Фінальна порада**: 
1. Виправте опечатку: `TS_PAKCET_SIZE` → `TS_PACKET_SIZE`
2. Додайте маски для 13-бітних (0x1FFF), 33-бітних (0x1FFFFFFFF), 9-бітних (0x1FF) полів при записі/читанні
3. Замініть `panic` на повернення помилок у всіх `Decode()` методах
4. Реалізуйте базову підтримку дескрипторів у PAT/PMT для майбутніх розширень
5. Додайте юніт-тести для Encode/Decode roundtrip з перевіркою граничних значень (макс. PID, PCR, тощо)

Це перетворить цей файл з "робочої реалізації" на "гарантовано коректний бінарний шар" для всього вашого MPEG-TS/HLS пайплайну.