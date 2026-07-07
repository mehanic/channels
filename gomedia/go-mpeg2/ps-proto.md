# 📦 Глибокий розбір `mpeg2/types.go` — типи даних та парсинг MPEG-2 Program Stream

Це **фундаментальний шар абстракції** для роботи з форматом MPEG-2 Program Stream (PS) у вашому CCTV HLS Processor. Файл визначає структури даних, константи типів потоків та реалізує парсинг/серіалізацію ключових пакетів: pack header, system header, program stream map (PSM). Розберемо архітектурно:

---

## 🧱 1. Система помилок: type-safe обробка станів парсингу

### 🔑 Інтерфейс `Error` для специфічних станів:

```go
type Error interface {
    NeedMore() bool         // Чи потрібно більше даних для парсингу?
    ParserError() bool      // Чи сталася помилка формату?
    StreamIdNotFound() bool // Чи не знайдено stream_id у мапі?
}
```

### 🔧 Три типи помилок:

```go
// 1. errNeedMore: інкрементальний парсинг потребує більше байт
var errNeedMore error = &needmoreError{}
type needmoreError struct{}
func (e *needmoreError) Error() string          { return "need more bytes" }
func (e *needmoreError) NeedMore() bool         { return true }
// ... інші методи повертають false

// 2. errParser: невалідний формат пакету (пошкоджені дані)
var errParser error = &parserError{}
type parserError struct{}
func (e *parserError) Error() string { return "parser packet error" }
func (e *parserError) ParserError() bool { return true }

// 3. errNotFound: stream_id не зареєстровано у PSM
var errNotFound error = &sidNotFoundError{}
type sidNotFoundError struct{}
func (e *sidNotFoundError) Error() string { return "stream id not found" }
func (e *sidNotFoundError) StreamIdNotFound() bool { return true }
```

### 🎯 Практичне застосування у `PSDemuxer.Input()`:

```go
func (psd *PSDemuxer) Input(data []byte) error {
    // ... парсинг ...
    if mpegerr, ok := ret.(Error); ok {
        if mpegerr.NeedMore() {
            saveReseved()  // Зберегти часткові дані у кеш
        }
        break  // Вийти з циклу, очікувати більше даних
    }
    // ... обробка інших помилок ...
}
```

> 💡 **Чому це критично**: У мережевих сценаріях дані приходять фрагментами. Без `errNeedMore` парсер змушений був би буферизувати весь пакет перед обробкою → висока затримка. З ним — інкрементальна обробка з мінімальним буфером.

---

## 📊 2. Типи потоків: `PS_STREAM_TYPE` — мапінг кодеків

### 🔧 Константи стандарту MPEG-2:

```go
type PS_STREAM_TYPE int
const (
    PS_STREAM_UNKNOW PS_STREAM_TYPE = 0xFF  // Резерв для невідомих кодеків
    PS_STREAM_AAC    PS_STREAM_TYPE = 0x0F  // AAC-LC (Advanced Audio Coding)
    PS_STREAM_H264   PS_STREAM_TYPE = 0x1B  // H.264/AVC (ITU-T Rec. H.264)
    PS_STREAM_H265   PS_STREAM_TYPE = 0x24  // H.265/HEVC (ITU-T Rec. H.265)
    PS_STREAM_G711A  PS_STREAM_TYPE = 0x90  // G.711 A-law (telephony)
    PS_STREAM_G711U  PS_STREAM_TYPE = 0x91  // G.711 μ-law (telephony, US)
)
```

### 🔍 Звідки ці значення?

Це **стандартні stream_type значення** з таблиць специфікації MPEG-2 Systems (ISO/IEC 13818-1):

| Значення | Кодек | Специфікація |
|----------|-------|-------------|
| 0x0F | AAC | ISO/IEC 13818-7 |
| 0x1B | H.264 | ITU-T Rec. H.264 \| ISO/IEC 14496-10 |
| 0x24 | H.265 | ITU-T Rec. H.265 \| ISO/IEC 23008-2 |
| 0x90-0x91 | G.711 | ITU-T Rec. G.711 (private range) |

> 💡 **Практичне значення**: Ці значення використовуються у Program Stream Map (PSM) для мапінгу `stream_id` → кодек. Без правильного мапінгу декодер не знатиме, як інтерпретувати дані потоку.

---

## 📦 3. Pack Header: `PSPackHeader` — синхронізація та тактова частота

### 🔍 Специфікація (Table 2-33):

```
pack_header() {
    pack_start_code                      32 bslbf  // 0x000001BA
    '01'                                  2 bslbf  // MPEG-2 marker
    system_clock_reference_base [32..30]  3 bslbf  // SCR base, старші 3 біти
    marker_bit                            1 bslbf  // завжди 1
    system_clock_reference_base [29..15] 15 bslbf  // SCR base, середні 15 біт
    marker_bit                            1 bslbf
    system_clock_reference_base [14..0]  15 bslbf  // SCR base, молодші 15 біт
    marker_bit                            1 bslbf
    system_clock_reference_extension      9 uimsbf  // SCR extension
    marker_bit                            1 bslbf
    program_mux_rate                     22 uimsbf  // бітрейт мультиплексу ×50 = bps
    marker_bit                            1 bslbf
    marker_bit                            1 bslbf
    reserved                              5 bslbf  // завжди 0x1F
    pack_stuffing_length                  3 uimsbf  // кількість stuffing байт (0-7)
    for (i = 0; i < pack_stuffing_length; i++) {
        stuffing_byte                     8 bslbf  // зазвичай 0xFF
    }
}
```

### 🔧 Структура Go:

```go
type PSPackHeader struct {
    IsMpeg1                          bool   // Прапорець: MPEG-1 vs MPEG-2 формат
    System_clock_reference_base      uint64 // 33 біти: основна частина SCR (90 kHz clock)
    System_clock_reference_extension uint16 // 9 біт: розширення SCR (27 MHz clock)
    Program_mux_rate                 uint32 // 22 біти: бітрейт мультиплексу (значення × 50 = bps)
    Pack_stuffing_length             uint8  // 3 біти: кількість stuffing байт (0-7)
}
```

### 🔧 Декодування: розбір 33-бітного SCR

```go
func (ps_pkg_hdr *PSPackHeader) Decode(bs *codec.BitStream) error {
    // 1. Перевірка start code
    if bs.Uint32(32) != 0x000001BA {
        panic("ps header must start with 000001BA")  // ⚠️ panic у продакшені!
    }
    
    // 2. Детекція версії: MPEG-1 vs MPEG-2
    if bs.NextBits(2) == 0x01 {  // '01' → MPEG-2
        return ps_pkg_hdr.decodeMpeg2(bs)
    } else if bs.NextBits(4) == 0x02 {  // '0010' → MPEG-1
        ps_pkg_hdr.IsMpeg1 = true
        return ps_pkg_hdr.decodeMpeg1(bs)
    } else {
        return errParser
    }
}

func (ps_pkg_hdr *PSPackHeader) decodeMpeg2(bs *codec.BitStream) error {
    // Розбір 33-бітного SCR у три частини з marker bits
    bs.SkipBits(2)  // пропустити '01'
    
    // Старші 3 біти
    ps_pkg_hdr.System_clock_reference_base = bs.GetBits(3)
    bs.SkipBits(1)  // marker_bit
    
    // Середні 15 біт
    ps_pkg_hdr.System_clock_reference_base = ps_pkg_hdr.System_clock_reference_base<<15 | bs.GetBits(15)
    bs.SkipBits(1)  // marker_bit
    
    // Молодші 15 біт
    ps_pkg_hdr.System_clock_reference_base = ps_pkg_hdr.System_clock_reference_base<<15 | bs.GetBits(15)
    bs.SkipBits(1)  // marker_bit
    
    // SCR extension (9 біт)
    ps_pkg_hdr.System_clock_reference_extension = bs.Uint16(9)
    bs.SkipBits(1)  // marker_bit
    
    // Program mux rate (22 біти)
    ps_pkg_hdr.Program_mux_rate = bs.Uint32(22)
    bs.SkipBits(1)  // marker_bit
    
    // Reserved bits + stuffing length
    bs.SkipBits(1)  // marker_bit
    bs.SkipBits(5)  // reserved (0x1F)
    ps_pkg_hdr.Pack_stuffing_length = bs.Uint8(3)
    
    // Пропуск stuffing байт
    if bs.RemainBytes() < int(ps_pkg_hdr.Pack_stuffing_length) {
        bs.UnRead(10 * 8)  // ⚠️ UnRead може не підтримуватися!
        return errNeedMore
    }
    bs.SkipBits(int(ps_pkg_hdr.Pack_stuffing_length) * 8)
    return nil
}
```

### 📐 Формула SCR (System Clock Reference):

```
SCR — 42-бітне значення для синхронізації системного годинника:
• SCR_base: 33 біти @ 90 kHz → діапазон ~26.5 годин
• SCR_extension: 9 біт @ 27 MHz → точність до 37 ns

Повний SCR = (SCR_base × 300) + SCR_extension

Конвертація у час:
• 90 kHz clock: 1 tick = 1/90000 секунди ≈ 11.11 мкс
• 27 MHz clock: 1 tick = 1/27000000 секунди ≈ 37 ns

У вашому пайплайні:
• PTS/DTS у кадрах використовують 90 kHz clock
• SCR у pack header забезпечує загальну синхронізацію потоків
• Розбіжність між SCR та PTS/DTS > 100ms може викликати розсинхронізацію
```

### 🎯 Практичне застосування у `PSMuxer.Write()`:

```go
// У муксері: генерація pack header з коректним SCR
pack.System_clock_reference_base = dts - 3600  // offset 40ms для буфера
pack.System_clock_reference_extension = 0
pack.Encode(bsw)  // Серіалізація у байти згідно специфікації
```

---

## ⚙️ 4. System Header: `System_header` — метадані потоків

### 🔍 Специфікація (Section 2.5.3):

```
system_header() {
    system_header_start_code         32 bslbf  // 0x000001BB
    header_length                     16 uimsbf  // довжина решти заголовку
    marker_bit                         1 bslbf
    rate_bound                         22 uimsbf  // макс. бітрейт програми ×50 = bps
    marker_bit                         1 bslbf
    audio_bound                         6 uimsbf  // макс. кількість аудіо потоків (0-63)
    fixed_flag                         1 bslbf
    CSPS_flag                         1 bslbf
    system_audio_lock_flag             1 bslbf
    system_video_lock_flag             1 bslbf
    marker_bit                      1 bslbf
    video_bound                     5 uimsbf  // макс. кількість відео потоків (0-31)
    packet_rate_restriction_flag    1 bslbf
    reserved_bits                     7 bslbf
    while (nextbits() == '1') {  // повторювати для кожного потоку
        stream_id                     8 uimsbf  // stream_id (0xC0-0xDF audio, 0xE0-0xFF video)
        '11'                         2 bslbf
        P-STD_buffer_bound_scale     1 bslbf  // 0=128-byte units, 1=1024-byte units
        P-STD_buffer_size_bound     13 uimsbf  // розмір буфера декодера
    }
}
```

### 🔧 Структура Go:

```go
type System_header struct {
    Header_length                uint16  // довжина решти заголовку (мін. 6 байт)
    Rate_bound                   uint32  // макс. бітрейт програми (значення × 50 = bps)
    Audio_bound                  uint8   // лічильник аудіо потоків
    Fixed_flag                   uint8   // прапорець фіксованого бітрейту
    CSPS_flag                    uint8   // Constrained System Parameter Stream
    System_audio_lock_flag       uint8   // синхронізація аудіо з системним годинником
    System_video_lock_flag       uint8   // синхронізація відео з системним годинником
    Video_bound                  uint8   // лічильник відео потоків
    Packet_rate_restriction_flag uint8   // обмеження на розмір пакетів
    Streams                      []*Elementary_Stream  // масив потоків
}

type Elementary_Stream struct {
    Stream_id                uint8   // stream_id цього потоку
    P_STD_buffer_bound_scale uint8   // одиниці виміру буфера (0=128, 1=1024 байт)
    P_STD_buffer_size_bound  uint16  // розмір буфера в одиницях bound_scale
}
```

### 🔧 Енкодинг з динамічним розрахунком довжини:

```go
func (sh *System_header) Encode(bsw *codec.BitStreamWriter) {
    bsw.PutBytes([]byte{0x00, 0x00, 0x01, 0xBB})  // start code
    
    // Резервувати місце для header_length (буде записано пізніше)
    loc := bsw.ByteOffset()
    bsw.PutUint16(0, 16)  // тимчасове значення 0
    
    bsw.Markdot()  // позначити початок даних для розрахунку довжини
    
    // Запис полів заголовку
    bsw.PutUint8(1, 1)  // marker_bit
    bsw.PutUint32(sh.Rate_bound, 22)
    // ... інші поля ...
    
    // Запис інформації про потоки
    for _, stream := range sh.Streams {
        bsw.PutUint8(stream.Stream_id, 8)
        bsw.PutUint8(3, 2)  // '11' marker
        bsw.PutUint8(stream.P_STD_buffer_bound_scale, 1)
        bsw.PutUint16(stream.P_STD_buffer_size_bound, 13)
    }
    
    // Розрахунок фактичної довжини та запис у зарезервоване місце
    length := bsw.DistanceFromMarkDot() / 8  // конвертація біт → байти
    bsw.SetUint16(uint16(length), loc)  // записати фактичну довжину
}
```

### 🎯 Значення параметрів буфера:

```
P-STD (Program Stream System Target Decoder) буфер:
• P_STD_buffer_bound_scale: одиниці виміру
  - 0 = 128-byte units (аудіо)
  - 1 = 1024-byte units (відео)
• P_STD_buffer_size_bound: розмір буфера в цих одиницях

Приклад для відео:
• bound_scale = 1 → одиниця = 1024 байти
• buffer_size_bound = 400 → 400 × 1024 = 409,600 байт ≈ 400 KB

Приклад для аудіо:
• bound_scale = 0 → одиниця = 128 байт
• buffer_size_bound = 32 → 32 × 128 = 4,096 байт = 4 KB

Чому це важливо:
• Декодер виділяє буфер цього розміру для кожного потоку
• Замалий буфер → underflow → артефакти відтворення
• Завеликий буфер → зайве споживання пам'яті, особливо на embedded-пристроях
```

---

## 🗺️ 5. Program Stream Map: `Program_stream_map` — мапінг потоків

### 🔍 Специфікація (Section 2.5.4):

```
program_stream_map() {
    packet_start_code_prefix             24 bslbf  // 0x000001
    map_stream_id                         8 uimsbf  // завжди 0xBC
    program_stream_map_length             16 uimsbf  // довжина решти пакета
    current_next_indicator                 1 bslbf  // 1=поточна, 0=наступна версія
    reserved                             2 bslbf
    program_stream_map_version             5 uimsbf  // версія мапи (0-31)
    reserved                             7 bslbf
    marker_bit                             1 bslbf
    program_stream_info_length             16 uimsbf  // довжина загальних дескрипторів
    for (i = 0; i < N; i++) { descriptor() }  // загальні дескриптори
    elementary_stream_map_length         16 uimsbf  // довжина мапи потоків
    for (i = 0; i < N1; i++) {
        stream_type                         8 uimsbf  // тип кодеку (0x0F=AAC, 0x1B=H264, тощо)
        elementary_stream_id             8 uimsbf  // stream_id цього потоку
        elementary_stream_info_length     16 uimsbf  // довжина дескрипторів потоку
        for (i = 0; i < N2; i++) { descriptor() }  // дескриптори потоку
    }
    CRC_32                                 32 rpchof  // CRC для перевірки цілісності
}
```

### 🔧 Структура Go:

```go
type Program_stream_map struct {
    Map_stream_id                uint8  // завжди 0xBC
    Program_stream_map_length    uint16  // довжина решти пакета
    Current_next_indicator       uint8   // 1=поточна версія
    Program_stream_map_version   uint8   // версія мапи (інкремент при зміні)
    Program_stream_info_length   uint16  // довжина загальних дескрипторів (не реалізовано)
    Elementary_stream_map_length uint16  // довжина мапи потоків
    Stream_map                   []*Elementary_stream_elem  // масив потоків
}

type Elementary_stream_elem struct {
    Stream_type                   uint8  // тип кодеку (PS_STREAM_TYPE)
    Elementary_stream_id          uint8  // stream_id (0xC0-0xDF audio, 0xE0-0xFF video)
    Elementary_stream_info_length uint16  // довжина дескрипторів (не реалізовано)
}
```

### 🔧 Енкодинг з CRC32:

```go
func (psm *Program_stream_map) Encode(bsw *codec.BitStreamWriter) {
    bsw.PutBytes([]byte{0x00, 0x00, 0x01, 0xBC})  // start code + map_stream_id
    
    // Резервувати місце для program_stream_map_length
    loc := bsw.ByteOffset()
    bsw.PutUint16(psm.Program_stream_map_length, 16)
    
    bsw.Markdot()  // позначити початок даних
    
    // Запис полів заголовку
    bsw.PutUint8(psm.Current_next_indicator, 1)
    bsw.PutUint8(3, 2)  // reserved
    bsw.PutUint8(psm.Program_stream_map_version, 5)
    bsw.PutUint8(0x7F, 7)  // reserved + marker_bit
    bsw.PutUint16(0, 16)  // program_stream_info_length = 0 (немає дескрипторів)
    
    // Розрахунок довжини мапи потоків: кожен елемент = 4 байти (type + id + length)
    psm.Elementary_stream_map_length = uint16(len(psm.Stream_map) * 4)
    bsw.PutUint16(psm.Elementary_stream_map_length, 16)
    
    // Запис інформації про кожен потік
    for _, streaminfo := range psm.Stream_map {
        bsw.PutUint8(streaminfo.Stream_type, 8)  // тип кодеку
        bsw.PutUint8(streaminfo.Elementary_stream_id, 8)  // stream_id
        bsw.PutUint16(0, 16)  // elementary_stream_info_length = 0
    }
    
    // Розрахунок загальної довжини та запис у зарезервоване місце
    length := bsw.DistanceFromMarkDot()/8 + 4  // +4 для CRC
    bsw.SetUint16(uint16(length), loc)
    
    // Розрахунок CRC32 для перевірки цілісності
    crc := codec.CalcCrc32(0xffffffff, bsw.Bits()[bsw.ByteOffset()-int(length-4)-4:bsw.ByteOffset()])
    tmpcrc := make([]byte, 4)
    binary.LittleEndian.PutUint32(tmpcrc, crc)  // ⚠️ Little-endian для CRC!
    bsw.PutBytes(tmpcrc)
}
```

### 🎯 Чому CRC у Little-endian?

```
Специфікація MPEG-2 вимагає CRC32 у little-endian форматі,
на відміну від більшості інших полів, що використовують big-endian.

Це історична особливість стандарту:
• CRC розраховується на байтовому рівні
• Записується у порядку байт: [LSB, ..., MSB]

У вашому коді:
binary.LittleEndian.PutUint32(tmpcrc, crc)  // ✓ коректно

Якщо використати BigEndian → декодери відкинуть пакет як пошкоджений.
```

### 🎯 Практичне застосування: динамічне оновлення мапи

```go
// У PSMuxer.AddStream():
func (muxer *PSMuxer) AddStream(cid PS_STREAM_TYPE) uint8 {
    // ... створення Elementary_Stream ...
    
    // Додати мапінг у PSM
    muxer.psm.Stream_map = append(muxer.psm.Stream_map, 
        NewElementary_stream_elem(uint8(cid), es.Stream_id))
    
    // Інкремент версії мапи для детекції змін декодерами
    muxer.psm.Program_stream_map_version++
    
    return es.Stream_id
}

// У демуксері при отриманні PSM:
if ret = psdemuxer.pkg.Psm.Decode(bs); ret == nil {
    for _, streaminfo := range psdemuxer.pkg.Psm.Stream_map {
        if _, found := psdemuxer.streamMap[streaminfo.Elementary_stream_id]; !found {
            // Новий потік → створити обробник
            stream := newpsstream(streaminfo.Elementary_stream_id, 
                                 PS_STREAM_TYPE(streaminfo.Stream_type))
            psdemuxer.streamMap[stream.sid] = stream
        }
    }
}
```

---

## 🧩 6. Допоміжні структури: `Program_stream_directory`, `CommonPesPacket`, `PSPacket`

### 🔸 `Program_stream_directory` (0x000001FF):

```go
type Program_stream_directory struct {
    PES_packet_length uint16
}
// Призначення: індекс для швидкого seek у великих файлах
// У вашому коді: тільки парсинг заголовку, payload пропускається
```

### 🔸 `CommonPesPacket` (private streams 0x000001BD-0x000001BF):

```go
type CommonPesPacket struct {
    Stream_id         uint8
    PES_packet_length uint16
}
// Призначення: обробка приватних потоків (субтитри, метадані)
// У вашому коді: тільки пропуск даних, без детального парсингу
```

### 🔸 `PSPacket` — контейнер для поточного пакету:

```go
type PSPacket struct {
    Header  *PSPackHeader      // pack header (0xBA)
    System  *System_header     // system header (0xBB)
    Psm     *Program_stream_map // program stream map (0xBC)
    Psd     *Program_stream_directory // program stream directory (0xFF)
    CommPes *CommonPesPacket   // private streams (0xBD-0xBF)
    Pes     *PesPacket         // елементарні потоки (0xC0-0xEF)
}
```

> 💡 **Архітектурне рішення**: Один контейнер для всіх типів пакетів спрощує обробку у `PSDemuxer.Input()` — не потрібно окремих змінних для кожного типу.

---

## 🐞 7. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **`panic` замість повернення помилки**:
   ```go
   // У Decode() методах:
   if bs.Uint32(32) != 0x000001BA {
       panic("ps header must start with 000001BA")  // ← crash сервера!
   }
   
   // Краще:
   return errParser  // або спеціальну помилку "invalid start code"
   ```

2. **`UnRead()` може не підтримуватися**:
   ```go
   // У decodeMpeg2():
   bs.UnRead(10 * 8)  // ← якщо BitStream не підтримує відмотування → втрата даних!
   
   // Краще: зберігати позицію перед парсингом і відновлювати її при помилці
   pos := bs.Position()
   // ... парсинг ...
   if error {
       bs.Seek(pos)  // відновити позицію
       return errNeedMore
   }
   ```

3. **Відсутня валідація `Pack_stuffing_length`**:
   ```go
   // Якщо Pack_stuffing_length > 7 (макс. за специфікацією) → невалідні дані
   if ps_pkg_hdr.Pack_stuffing_length > 7 {
       return errParser
   }
   ```

4. **Некоректний розрахунок довжини у `System_header.Encode()`**:
   ```go
   length := bsw.DistanceFromMarkDot() / 8  // ← ділення на 8 для біт→байти
   // Але DistanceFromMarkDot() повертає біти, тому ділення коректне ✓
   // Однак: якщо не кратне 8 → втрата дробової частини!
   // Краще: перевірити кратність або використовувати бітову арифметику
   ```

5. **CRC розраховується на неправильному діапазоні**:
   ```go
   // У Program_stream_map.Encode():
   crc := codec.CalcCrc32(0xffffffff, bsw.Bits()[bsw.ByteOffset()-int(length-4)-4:bsw.ByteOffset()])
   // ← складний індекс, важко перевірити коректність
   // Краще: зберігати початкову позицію перед записом даних для CRC
   crcStart := bsw.ByteOffset()
   // ... запис даних ...
   crc := codec.CalcCrc32(0xffffffff, bsw.Bits()[crcStart:bsw.ByteOffset()])
   ```

6. **Відсутня підтримка дескрипторів**:
   ```go
   // У Program_stream_map.Decode():
   bs.SkipBits(int(psm.Program_stream_info_length) * 8)  // ← просто пропускаємо
   // Але дескриптори можуть містити критичну інформацію:
   // • language codes для аудіо
   // • aspect ratio для відео
   // • HDR metadata
   // Краще: парсити хоча б базові дескриптори або логувати попередження
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечного парсингу start code
func expectStartCode(bs *codec.BitStream, expected uint32) error {
    if bs.RemainBits() < 32 {
        return errNeedMore
    }
    if bs.Uint32(32) != expected {
        return errParser
    }
    return nil
}

// 2. Валідація полів після парсингу
func (ps_pkg_hdr *PSPackHeader) Validate() error {
    if ps_pkg_hdr.Pack_stuffing_length > 7 {
        return fmt.Errorf("invalid stuffing length: %d", ps_pkg_hdr.Pack_stuffing_length)
    }
    if ps_pkg_hdr.Program_mux_rate == 0 {
        return errors.New("mux rate cannot be zero")
    }
    return nil
}

// 3. Юніт-тести для енкодинг/декодинг roundtrip
func TestPSPackHeader_EncodeDecode(t *testing.T) {
    original := &PSPackHeader{
        System_clock_reference_base: 123456789,
        System_clock_reference_extension: 0x123,
        Program_mux_rate: 6106,
        Pack_stuffing_length: 2,
    }
    
    bsw := codec.NewBitStreamWriter(64)
    original.Encode(bsw)
    
    bs := codec.NewBitStream(bsw.Bits())
    var decoded PSPackHeader
    if err := decoded.Decode(bs); err != nil {
        t.Fatalf("decode error: %v", err)
    }
    
    if decoded.System_clock_reference_base != original.System_clock_reference_base {
        t.Errorf("SCR base mismatch: got %d, want %d", 
            decoded.System_clock_reference_base, original.System_clock_reference_base)
    }
    // ... перевірка інших полів ...
}

// 4. Підтримка базових дескрипторів у PSM
type Descriptor struct {
    Tag  uint8
    Length uint8
    Data []byte
}

func (psm *Program_stream_map) ParseDescriptors(bs *codec.BitStream, length uint16) ([]Descriptor, error) {
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

## 🎯 8. Інтеграція з вашим CCTV HLS Processor

### 📍 У `PSDemuxer` — обробка вхідного потоку:

```go
func (psd *PSDemuxer) Input(data []byte) error {
    // ... ініціалізація ...
    
    for !bs.EOS() {
        prefix_code := bs.NextBits(32)
        switch prefix_code {
        case 0x000001BA: // pack header
            if psd.pkg.Header == nil {
                psd.pkg.Header = new(PSPackHeader)
            }
            ret = psd.pkg.Header.Decode(bs)
            psd.mpeg1 = psd.pkg.Header.IsMpeg1  // зберегти версію для PES парсингу
            
        case 0x000001BC: // program stream map
            if psd.pkg.Psm == nil {
                psd.pkg.Psm = new(Program_stream_map)
            }
            if ret = psd.pkg.Psm.Decode(bs); ret == nil {
                // Оновити мапу потоків
                for _, streaminfo := range psd.pkg.Psm.Stream_map {
                    if _, found := psd.streamMap[streaminfo.Elementary_stream_id]; !found {
                        stream := newpsstream(streaminfo.Elementary_stream_id, 
                                             PS_STREAM_TYPE(streaminfo.Stream_type))
                        psd.streamMap[stream.sid] = stream
                    }
                }
            }
            
        // ... інші типи пакетів ...
        }
    }
    // ... обробка кешу ...
}
```

### 📍 У `PSMuxer` — генерація вихідного потоку:

```go
func (muxer *PSMuxer) Write(sid uint8, frame []byte, pts, dts uint64) error {
    // ... підготовка ...
    
    bsw := codec.NewBitStreamWriter(1024)
    
    // 1. Pack header з SCR на основі DTS
    var pack PSPackHeader
    pack.System_clock_reference_base = dts - 3600  // offset 40ms
    pack.Program_mux_rate = 6106  // дефолтний бітрейт
    pack.Encode(bsw)
    
    // 2. System header + PSM тільки на початку або при IDR
    if muxer.firstframe || idr_flag {
        muxer.system.Encode(bsw)  // метадані потоків
        muxer.psm.Encode(bsw)     // мапінг stream_id → codec
        muxer.firstframe = false
    }
    
    // 3. PES пакет з відео/аудіо даними
    pespkg := NewPesPacket()
    pespkg.Stream_id = sid
    pespkg.PTS_DTS_flags = 0x03  // PTS+DTS присутні
    // ... налаштування інших полів ...
    pespkg.Encode(bsw)
    
    // 4. Відправка згенерованих байт
    if muxer.OnPacket != nil {
        muxer.OnPacket(bsw.Bits())
    }
    
    return nil
}
```

### 📍 У метриках — моніторинг якості парсингу:

```go
func (psd *PSDemuxer) recordParseMetrics(packetType uint32, err error) {
    var typeName string
    switch packetType {
    case 0x000001BA: typeName = "pack_header"
    case 0x000001BB: typeName = "system_header"
    case 0x000001BC: typeName = "program_stream_map"
    default: typeName = "unknown"
    }
    
    if err == nil {
        metrics.PSPacketsParsed.WithLabelValues(typeName).Inc()
    } else if err == errNeedMore {
        metrics.PSPacketsIncomplete.WithLabelValues(typeName).Inc()
    } else {
        metrics.PSPacketsErrors.WithLabelValues(typeName, err.Error()).Inc()
    }
}
```

---

## 🧭 Висновок: чому цей файл — фундамент бінарної сумісності

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **Type-safe помилки** | Коректна обробка інкрементального парсингу | Втрата даних при мережевих затримках, неможливість live-обробки |
| **PS_STREAM_TYPE константи** | Стандартизований мапінг кодеків | Декодери не розпізнають потоки → "невалідний файл" помилки |
| **SCR кодування/декодування** | Синхронізація системного годинника | Розсинхронізація аудіо/відео, артефакти відтворення |
| **System header з буферами** | Коректна ініціалізація декодерів | Buffer underflow/overflow → crash плеєрів |
| **PSM з CRC32** | Валідація цілісності мапи потоків | Пошкоджені мапи → неправильна інтерпретація даних потоків |
| **Динамічний розрахунок довжин** | Коректна серіалізація змінних заголовків | Невалідні пакети → відхилення декодерами |

> 🔑 **Головна ідея**: Цей файл — **бінарний контракт** між вашим кодом та специфікацією MPEG-2 Systems. Він гарантує, що:
> 1. Кожен байт заголовку кодується/декодується згідно стандарту
> 2. Помилки обробляються type-safe способом для інкрементального парсингу
> 3. Метадані потоків (бітрейти, буфери, мапінг) передаються коректно
> 4. Цілісність даних перевіряється через CRC32

Без нього кожен компонент пайплайну змушений був би реалізовувати власну логіку парсингу заголовків, що призвело б до дублювання коду, помилок у синхронізації та важкої підтримки.

💡 **Фінальна порада**: 
1. Замініть `panic` на повернення помилок у всіх `Decode()` методах
2. Реалізуйте безпечне відмотування позиції у `BitStream` або зберігайте позицію перед парсингом
3. Додайте валідацію полів після парсингу (stuffing_length, mux_rate, тощо)
4. Реалізуйте базову підтримку дескрипторів у PSM для майбутніх розширень
5. Додайте юніт-тести для Encode/Decode roundtrip з перевіркою граничних значень

Це перетворить цей файл з "робочої реалізації" на "гарантовано коректний бінарний шар" для всього вашого MPEG-PS/HLS пайплайну.