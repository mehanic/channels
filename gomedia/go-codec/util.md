# 🛠️ Глибокий розбір `codec/utils.go` — утиліти для роботи з бітстрімами

Це **фундаментальний шар інфраструктури** для вашого CCTV HLS Processor, який надає низькорівневі примітиви для пошуку, розділення та валідації медіа-даних. Ці функції використовуються повсюдно: від `segmentAssembler` до `createTSSegment`. Розберемо архітектурно:

---

## 🔍 1. Пошук Start Code: `FindStartCode` — серце парсингу H.264/H.265

### 📦 Що таке Start Code?

У стандартах H.264/H.265 кожен NAL Unit (Network Abstraction Layer) починається з **магічної послідовності**:

```
Annex-B формат (RTP/TS/файли):
├─ 3-байтовий:  0x00 0x00 0x01
└─ 4-байтовий:  0x00 0x00 0x00 0x01  ← часто використовується для вирівнювання

AVCC формат (fMP4/MP4):
├─ Без start code!
└─ Замість цього: 4-byte big-endian length prefix
```

### 🔧 Реалізація `FindStartCode`:

```go
func FindStartCode(nalu []byte, offset int) (int, START_CODE_TYPE) {
    // 1. Шукаємо 3-байтовий маркер 0x000001
    idx := bytes.Index(nalu[offset:], []byte{0x00, 0x00, 0x01})
    
    // 2. Перевіряємо, чи це насправді 4-байтовий маркер
    switch {
    case idx > 0:
        // Якщо перед 0x000001 є ще один 0x00 → це 0x00000001
        if nalu[offset+idx-1] == 0x00 {
            return offset + idx - 1, START_CODE_4  // повертаємо позицію першого 0x00
        }
        fallthrough  // продовжуємо до наступного case
    case idx == 0:
        return offset + idx, START_CODE_3  // повертаємо позицію першого 0x00 з трійки
    }
    
    // 3. Не знайдено
    return -1, START_CODE_3
}
```

### 🎯 Чому це критично для вашого пайплайну:

| Сценарій | Без `FindStartCode` | З `FindStartCode` |
|----------|-------------------|------------------|
| **Розділення NAL units** | Довільне розрізання → пошкоджені кадри | Точне виділення кожного NAL → валідні сегменти |
| **Витягнення параметрів** | Не можна знайти SPS/PPS у потоці | Миттєвий пошук за маркером → ініціалізація декодера |
| **Конвертація форматів** | Ризик пошкодження даних при Annex-B ↔ AVCC | Безпечна конвертація з коректним зсувом |

### ⚠️ Потенційна проблема:

```go
// У випадку, коли idx == -1 (не знайдено), функція повертає:
return -1, START_CODE_3  // ← START_CODE_3 не має сенсу, якщо start code не знайдено!

// Краще повертати явний "невалідний" тип:
const START_CODE_NONE START_CODE_TYPE = 0
// ...
return -1, START_CODE_NONE
```

---

## 🎵 2. Пошук AAC Syncword: `FindSyncword` — маркер початку кадру ADTS

### 🔍 Структура AAC ADTS заголовку:

```
Syncword: 12 біт = 0xFFF (усі біти встановлені)
Це гарантує, що послідовність 0xFFF не зустрінеться у закодованих даних
```

### 🔧 Реалізація `FindSyncword`:

```go
func FindSyncword(aac []byte, offset int) int {
    for i := offset; i < len(aac)-1; i++ {
        // Перевіряємо два байти:
        // байт[i] == 0xFF (старші 8 біт syncword)
        // байт[i+1] & 0xF0 == 0xF0 (молодші 4 біт + 4 біти наступного поля)
        if aac[i] == 0xFF && aac[i+1]&0xF0 == 0xF0 {
            return i  // знайдено початок ADTS frame
        }
    }
    return -1  // не знайдено
}
```

### 🎯 Практичне застосування:

```go
// У SplitAACFrame:
start := FindSyncword(frames, 0)
for start >= 0 {
    adts.Decode(frames[start:])  // парсинг заголовку
    frameLen := adts.Variable_Header.Frame_length
    
    // Виділяємо повний кадр (заголовок + дані)
    onFrame(frames[start : start+int(frameLen)])
    
    // Шукаємо наступний syncword після поточного кадру
    start = FindSyncword(frames, start+int(frameLen))
}
```

> 💡 **Важливо**: AAC-кадри мають **змінну довжину**, тому не можна просто ітерувати по фіксованому кроку. `FindSyncword` + `Frame_length` — це єдиний надійний спосіб.

---

## ✂️ 3. Розділення кадрів: `SplitFrame` vs `SplitFrameWithStartCode`

### 🔸 `SplitFrame` — повертає NALU **без** start code:

```go
func SplitFrame(frames []byte, onFrame func(nalu []byte) bool) {
    beg, sc := FindStartCode(frames, 0)  // знайти перший start code
    for beg >= 0 {
        end, sc2 := FindStartCode(frames, beg+int(sc))  // знайти наступний
        
        if end == -1 {
            // Останній NALU: від початку до кінця буфера
            if onFrame != nil {
                onFrame(frames[beg+int(sc):])  // ← без start code!
            }
            break
        }
        
        // Нормальний випадок: від поточного start code до наступного
        if onFrame != nil && onFrame(frames[beg+int(sc):end]) == false {  // ← без start code!
            break  // callback повернув false → зупинити ітерацію
        }
        
        // Перейти до наступного NALU
        beg = end
        sc = sc2
    }
}
```

### 🔸 `SplitFrameWithStartCode` — повертає NALU **з** start code:

```go
func SplitFrameWithStartCode(frames []byte, onFrame func(nalu []byte) bool) {
    beg, sc := FindStartCode(frames, 0)
    for beg >= 0 {
        end, sc2 := FindStartCode(frames, beg+int(sc))
        
        if end == -1 {
            // Останній NALU: від початку start code до кінця
            if onFrame != nil && (beg+int(sc)) < len(frames) {
                onFrame(frames[beg:])  // ← З start code! (від beg, а не beg+sc)
            }
            break
        }
        
        // Нормальний випадок: від beg (початок start code) до end
        if onFrame != nil && (beg+int(sc)) < end && onFrame(frames[beg:end]) == false {
            break
        }
        
        beg = end
        sc = sc2
    }
}
```

### 🎯 Коли використовувати кожну:

| Функція | Повертає | Коли використовувати |
|---------|----------|---------------------|
| `SplitFrame` | NALU без start code | Парсинг параметрів (SPS/PPS), конвертація у AVCC, аналіз вмісту |
| `SplitFrameWithStartCode` | NALU з start code | Запис у файл/потік у Annex-B форматі, відправка у FFmpeg pipe |

> 💡 **Приклад у вашому пайплайні**:
> ```go
> // У segmentAssembler: потрібен тільки вміст для парсингу
> codec.SplitFrame(data, func(nalu []byte) bool {
>     nalType := codec.H264NaluTypeWithoutStartCode(nalu)
>     if nalType == codec.H264_NAL_SPS {
>         sa.processSPS(nalu)  // nalu вже без start code ✓
>     }
>     return true
> })
> 
> // У createTSSegment: потрібен Annex-B для FFmpeg
> codec.SplitFrameWithStartCode(data, func(nalu []byte) bool {
>     ffmpegPipe.Write(nalu)  // nalu зі start code ✓
>     return true
> })
> ```

---

## 🎵 4. Розділення AAC кадрів: `SplitAACFrame`

```go
func SplitAACFrame(frames []byte, onFrame func(aac []byte)) {
    var adts ADTS_Frame_Header
    start := FindSyncword(frames, 0)
    
    for start >= 0 {
        // 1. Розпарсити ADTS заголовок для отримання довжини кадру
        adts.Decode(frames[start:])
        
        // 2. Виділити повний кадр (заголовок + аудіо-дані)
        frameLen := adts.Variable_Header.Frame_length
        onFrame(frames[start : start+int(frameLen)])
        
        // 3. Шукати наступний syncword після поточного кадру
        start = FindSyncword(frames, start+int(frameLen))
    }
}
```

### 🎯 Практичне застосування:

```go
// У handleAudioChunk для AAC:
func (sa *SegmentAssembler) handleAACChunk(data []byte) error {
    return codec.SplitAACFrame(data, func(frame []byte) {
        // 1. Розпарсити заголовок для отримання семплів/кадр
        var adts ADTS_Frame_Header
        adts.Decode(frame)
        
        // 2. Розрахувати тривалість: 1024 семпли / sample_rate
        duration := time.Duration(1024 * time.Second / time.Duration(adts.Variable_Header.Sampling_frequency_index))
        
        // 3. Додати у сегмент з коректним PTS
        sa.currentAudioSegment.AppendFrame(frame[adts.HeaderLength:], sa.currentAudioPTS)
        sa.currentAudioPTS += duration
    })
}
```

---

## 🔢 5. Витягнення NAL Unit Type: helpers для H.264/H.265

### 🔸 H.264: 5 біт типу у першому байті після заголовку

```go
// H.264 NAL header (1 байт):
// [0:1] = forbidden_zero_bit
// [1:3] = nal_ref_idc  
// [3:8] = nal_unit_type (5 біт) ← це нас цікавить

func H264NaluTypeWithoutStartCode(h264 []byte) H264_NAL_TYPE {
    // Маска 0x1F = 0b00011111 → виділяє молодші 5 біт
    return H264_NAL_TYPE(h264[0] & 0x1F)
}

func H264NaluType(h264 []byte) H264_NAL_TYPE {
    // Спочатку знайти start code, потім застосувати ту ж логіку
    loc, sc := FindStartCode(h264, 0)
    return H264_NAL_TYPE(h264[loc+int(sc)] & 0x1F)  // пропустити start code
}
```

### 🔸 H.265: 6 біт типу, зсунутих на 1 біт

```go
// H.265 NAL header (2 байти):
// Байт 0: [0:1]=forbidden, [1:7]=nal_unit_type (6 біт)
// Байт 1: nuh_layer_id + temporal_id

func H265NaluTypeWithoutStartCode(h265 []byte) H265_NAL_TYPE {
    // Зсунути вправо на 1 біт, потім маска 0x3F = 0b00111111 (6 біт)
    return H265_NAL_TYPE((h265[0] >> 1) & 0x3F)
}

func H265NaluType(h265 []byte) H265_NAL_TYPE {
    loc, sc := FindStartCode(h265, 0)
    return H265_NAL_TYPE((h265[loc+int(sc)] >> 1) & 0x3F)
}
```

### 🎯 Практичне використання:

```go
// У segmentAssembler для детекції ключових кадрів:
func (sa *SegmentAssembler) shouldStartNewSegment(nalu []byte, codecID codec.CodecID) bool {
    switch codecID {
    case codec.CODECID_VIDEO_H264:
        nalType := codec.H264NaluTypeWithoutStartCode(nalu)
        return nalType == codec.H264_NAL_I_SLICE  // IDR frame
        
    case codec.CODECID_VIDEO_H265:
        nalType := codec.H265NaluTypeWithoutStartCode(nalu)
        // IRAP frames: 16-23 (BLA, CRA, IDR)
        return nalType >= 16 && nalType <= 23
    }
    return false
}
```

---

## 🎯 6. Детекція IDR/IRAP кадрів: `IsH264IDRFrame` / `IsH265IDRFrame`

### 🔸 H.264: IDR = NAL type 5

```go
func IsH264IDRFrame(h264 []byte) bool {
    ret := false
    onnalu := func(nalu []byte) bool {
        nal_type := H264NaluTypeWithoutStartCode(nalu)
        
        // Оптимізація: зупинитись при першому VCL NAL (slice)
        if nal_type < 5 {  // P/B/A slices
            return false  // не IDR, і далі шукати не треба
        } else if nal_type == 5 {  // IDR slice
            ret = true
            return false  // знайшли, зупинити ітерацію
        } else {
            return true  // SPS/PPS/SEI — продовжити пошук
        }
    }
    SplitFrame(h264, onnalu)  // ітерувати по всіх NAL у пакеті
    return ret
}
```

### 🔸 H.265: IRAP = NAL types 16-23

```go
func IsH265IDRFrame(h265 []byte) bool {
    ret := false
    onnalu := func(nalu []byte) bool {
        nal_type := H265NaluTypeWithoutStartCode(nalu)
        
        // Пропустити не-VCL NAL types (0-9: параметри, мета-дані)
        if nal_type <= 9 && nal_type >= 0 {
            return false
        }
        // IRAP frames: 16-21 (BLA_W_LP, BLA_W_RADL, BLA_N_LP, IDR_W_RADL, IDR_N_LP, CRA)
        else if nal_type >= 16 && nal_type <= 21 {
            ret = true
            return false
        } else {
            return true  // інші VCL slices — продовжити пошук
        }
    }
    SplitFrame(h265, onnalu)
    return ret
}
```

### 🎯 Навіщо це у вашому пайплайні:

| Функція | Роль у HLS Processor |
|---------|---------------------|
| `IsH264IDRFrame` | Детекція точки старту нового сегменту → гарантія, що кожен `.ts` файл починається з ключового кадру |
| `IsH265IDRFrame` | Аналогічно для HEVC, але з урахуванням складнішої ієрархії типів |
| **Оптимізація** | Раннє завершення (`return false`) економить час парсингу — не треба обробляти всі NAL, якщо IDR знайдено на початку |

---

## ✅ 7. Перевірка VCL NAL Type: `IsH264VCLNaluType` / `IsH265VCLNaluType`

### 🔍 Що таке VCL (Video Coding Layer)?

VCL NAL units містять **фактичні дані зображення** (слайси), на відміну від параметрів (SPS/PPS) або метаданих (SEI).

### 🔧 Реалізація:

```go
// H.264: VCL = types 1-5 (P, A, B, C, I slices)
func IsH264VCLNaluType(nal_type H264_NAL_TYPE) bool {
    if nal_type <= H264_NAL_I_SLICE && nal_type > H264_NAL_RESERVED {
        return true  // 1 <= nal_type <= 5
    }
    return false
}

// H.265: VCL = types 0-15 (TRAIL, TSA, STSA, RADL, RASL) + 16-23 (IRAP)
func IsH265VCLNaluType(nal_type H265_NAL_TYPE) bool {
    // Діапазон 1: 0-9 (звичайні слайси)
    // Діапазон 2: 16-23 (IRAP слайси)
    if (nal_type <= H265_NAL_SLICE_CRA && nal_type >= H265_NAL_SLICE_BLA_W_LP) ||
        (nal_type <= H265_NAL_SLICE_RASL_R && nal_type >= H265_NAL_Slice_TRAIL_N) {
        return true
    }
    return false
}
```

### 🎯 Практичне застосування:

```go
// У segmentFinalizer: перевірка, чи сегмент містить хоча б один VCL NAL
func (sf *SegmentFinalizer) validateSegment(nalus [][]byte, codecID codec.CodecID) error {
    hasVCL := false
    for _, nalu := range nalus {
        switch codecID {
        case codec.CODECID_VIDEO_H264:
            nalType := codec.H264NaluTypeWithoutStartCode(nalu)
            if codec.IsH264VCLNaluType(nalType) {
                hasVCL = true
                break
            }
        case codec.CODECID_VIDEO_H265:
            nalType := codec.H265NaluTypeWithoutStartCode(nalu)
            if codec.IsH265VCLNaluType(nalType) {
                hasVCL = true
                break
            }
        }
    }
    
    if !hasVCL {
        return errors.New("segment contains no video data (VCL NAL units)")
    }
    return nil
}
```

---

## 🔢 8. Допоміжні функції: `Max`, `Min`, `ShowPacketHexdump`

### 🔸 `Max` / `Min`:

```go
func Max(x, y int) int {
    if x > y { return x }
    return y
}
// Аналогічно для Min

// Використання: розрахунок буферів, обмеження діапазонів
bufferSize := codec.Min(len(data), MAX_CHUNK_SIZE)
```

### 🔸 `ShowPacketHexdump`:

```go
func ShowPacketHexdump(data []byte) {
    for k := 0; k < len(data); k++ {
        if k%8 == 0 && k != 0 {
            fmt.Printf("\n")  // новий рядок кожні 8 байт
        }
        fmt.Printf("%02x ", data[k])  // hex з двома цифрами
    }
    fmt.Printf("\n")
}

// Приклад виводу:
// 00 00 00 01 67 64 00 28 
// ac 2c a4 01 e0 08 9f 97 
// ff 00 01 00 01 52 02 02
```

### 🎯 Використання для дебагу:

```go
// У тестах або при логуванні помилок:
if err != nil {
    logger.Error("failed to parse NAL", "hexdump", func() string {
        var buf bytes.Buffer
        codec.ShowPacketHexdumpRedirect(nalu, &buf)  // уявна версія з io.Writer
        return buf.String()
    })
}
```

> 💡 **Порада**: Додайте версію `ShowPacketHexdump` з `io.Writer` для гнучкості (запис у файл, буфер, logger).

---

## 🔐 9. CRC32 таблиця та `CalcCrc32` — валідація цілісності даних

### 🔍 Таблиця `crc32table`:

Це **пре-обчислена таблиця** для алгоритму CRC32 (поліном 0xEDB88320, як у Ethernet/PNG/ISO 3309):

```go
var crc32table [256]uint32 = [256]uint32{
    0x00000000, 0xB71DC104, 0x6E3B8209, ...  // 256 значень
}
```

### 🔧 Функція `CalcCrc32`:

```go
func CalcCrc32(crc uint32, buffer []byte) uint32 {
    for i := 0; i < len(buffer); i++ {
        // XOR поточного байта з молодшими 8 бітами CRC
        // Використання таблиці для швидкого оновлення
        crc = crc32table[(crc^uint32(buffer[i]))&0xff] ^ (crc >> 8)
    }
    return crc
}
```

### 🎯 Практичне застосування:

```go
// У WebSocket-протоколі: перевірка цілісності повідомлень
type SubtitleMessage struct {
    Payload []byte
    CRC32   uint32  // додається відправником
}

func (sm *SubtitleMessage) Validate() error {
    calculated := codec.CalcCrc32(0, sm.Payload)
    if calculated != sm.CRC32 {
        return fmt.Errorf("CRC mismatch: expected %08x, got %08x", sm.CRC32, calculated)
    }
    return nil
}

// У RTP-пакетах: деякі профілі використовують CRC для захисту від помилок
```

> 💡 **Для CCTV**: Якщо ви передаєте субтитри/метадані через ненадійні канали (3G/4G), CRC32 допомагає відкидати пошкоджені пакети до потрапляння у пайплайн.

---

## 🔄 10. `CovertRbspToSodb` — видалення emulation prevention bytes

### 🔍 Навіщо потрібні emulation prevention bytes?

У H.264/H.265 бітстрімі заборонено послідовності, що збігаються зі start code:

```
Заборонені послідовності у RBSP (Raw Byte Sequence Payload):
├─ 0x000000  (три нулі)
├─ 0x000001  (start code!)
├─ 0x000002  (reserved)
├─ 0x000003  (emulation prevention marker)

Рішення: якщо у даних зустрічається 0x00000X, вставляємо 0x03 після 0x0000:
Оригінал:  [0x00 0x00 0x01 ...]  ← колізія зі start code!
Кодування: [0x00 0x00 0x03 0x01 ...]  ← 0x03 "екранує" наступний байт
```

### 🔧 Реалізація `CovertRbspToSodb`:

```go
func CovertRbspToSodb(rbsp []byte) []byte {
    bs := NewBitStream(rbsp)           // читання з вхідного буфера
    bsw := NewBitStreamWriter(len(rbsp))  // запис у вихідний
    
    for !bs.EOS() {  // поки не кінець потоку
        // Перевірити наступні 24 біти (3 байти)
        if bs.RemainBytes() > 3 && bs.NextBits(24) == 0x000003 {
            // Знайдено emulation prevention: 0x000003
            // Копіюємо перші два байти (0x00 0x00), пропускаємо 0x03
            bsw.PutByte(bs.Uint8(8))  // 0x00
            bsw.PutByte(bs.Uint8(8))  // 0x00
            bs.SkipBits(8)            // пропустити 0x03
        } else {
            // Звичайний байт — копіювати як є
            bsw.PutByte(bs.Uint8(8))
        }
    }
    return bsw.Bits()  // повернути "очищений" SODB (String Of Data Bits)
}
```

### 🎯 Коли це використовується:

```go
// У парсингу SPS/PPS: перед декодуванням потрібно видалити emulation bytes
func (sa *SegmentAssembler) processH264SPS(nalu []byte) error {
    // 1. Знайти start code та пропустити NAL header
    start, sc := codec.FindStartCode(nalu, 0)
    payload := nalu[start+int(sc)+1:]  // +1 для пропуску NAL header
    
    // 2. Видалити emulation prevention bytes
    sodb := codec.CovertRbspToSodb(payload)
    
    // 3. Розпарсити SPS з "чистого" бітстріму
    var sps codec.SPS
    sps.Decode(codec.NewBitStream(sodb))
    
    // 4. Зберегти параметри
    sa.currentResolution = sps.GetResolution()
    return nil
}
```

> ⚠️ **Важливо**: Ця функція працює на рівні **байтів**, а не біт. Якщо вам потрібна бітова точність (наприклад, для парсингу полів < 8 біт), використовуйте `BitStream` без попереднього `CovertRbspToSodb`.

---

## 🐞 11. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **`FindStartCode` повертає некоректний тип при -1**:
   ```go
   return -1, START_CODE_3  // ← START_CODE_3 не має сенсу, якщо start code не знайдено
   // Краще:
   const START_CODE_NONE START_CODE_TYPE = 0
   return -1, START_CODE_NONE
   ```

2. **`SplitFrame` не перевіряє межі буфера**:
   ```go
   onFrame(frames[beg+int(sc):])  // ← якщо beg+sc > len(frames) → panic!
   // Краще додати перевірку:
   if beg+int(sc) >= len(frames) {
       break
   }
   ```

3. **`CovertRbspToSodb` може нескінченно циклити**:
   ```go
   if bs.RemainBytes() > 3 && bs.NextBits(24) == 0x000003 {
       // Якщо буфер закінчується на 0x000003, bs.NextBits(24) може читати за межами!
       // Краще:
       if bs.RemainBytes() >= 3 && bs.PeekBytes(3) == []byte{0x00, 0x00, 0x03} {
           // ...
       }
   }
   ```

4. **Відсутність юніт-тестів для крайніх випадків**:
   ```go
   // Додати тести для:
   // - Порожнього буфера: FindStartCode([]byte{}, 0)
   // - Буфера без start code: FindStartCode([]byte{0x64, 0x00, 0x28}, 0)
   // - Некоректних даних: SplitFrame([]byte{0xFF, 0xFE}, nil)
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для безпечного пошуку start code з перевіркою меж
func FindStartCodeSafe(data []byte, offset int) (int, START_CODE_TYPE, error) {
    if offset >= len(data) {
        return -1, START_CODE_NONE, errors.New("offset out of bounds")
    }
    pos, typ := FindStartCode(data, offset)
    if pos == -1 {
        return -1, START_CODE_NONE, nil
    }
    return pos, typ, nil
}

// 2. Інтеграція з метриками для моніторингу парсингу
func (sa *SegmentAssembler) recordParsingMetrics(codecID codec.CodecID, nalType uint8, duration time.Duration) {
    metrics.ParsingDuration.WithLabelValues(
        codec.CodecString(codecID),
        fmt.Sprintf("NAL_%d", nalType),
    ).Observe(float64(duration) / float64(time.Millisecond))
}

// 3. Кешування результатів FindStartCode для великих буферів
type StartCodeCache struct {
    lastPos  int
    lastType START_CODE_TYPE
    lastHash uint64  // hash буфера для детекції змін
    mu       sync.RWMutex
}

func (scc *StartCodeCache) Find(data []byte, offset int) (int, START_CODE_TYPE) {
    // Якщо буфер не змінився і offset співпадає → повернути кешоване значення
    // Інакше → викликати FindStartCode та оновити кеш
}
```

---

## 🧭 Висновок: чому ці утиліти — фундамент надійності

| Функція | Роль у CCTV HLS Processor | Вартість помилки без неї |
|---------|---------------------------|-------------------------|
| `FindStartCode` | Точне розділення NAL units | Пошкоджені сегменти → FFmpeg помиляється → розриви у HLS |
| `FindSyncword` | Коректне виділення AAC кадрів | Розсинхронізація аудіо → артефакти відтворення |
| `SplitFrame*` | Гнучка ітерація по кадрах | Неможливість обробки змінних розмірів пакетів |
| `H264/265NaluType` | Швидка класифікація вмісту | Неправильна сегментація → відсутність ключових кадрів у сегментах |
| `Is*IDRFrame` | Детекція точок старту сегментів | Сегменти без IDR → клієнти не можуть почати відтворення |
| `CovertRbspToSodb` | Безпечний парсинг параметрів | Паник при парсингу SPS/PPS → падіння сервера |

> 🔑 **Головна ідея**: Ці функції — **інфраструктурний шар**, який абстрагує складність бітстрімів від бізнес-логіки пайплайну. Без них `segmentAssembler` змушений був би реалізовувати пошук start code, парсинг заголовків та валідацію даних вручну — це призвело б до дублювання коду, помилок та важкої підтримки.

💡 **Фінальна порада**: Додайте інтеграційний тест, який:
1. Генерує "брудний" буфер з перемішаними H.264 NAL units, AAC frames та ID3 тегами
2. Перевіряє, що `SplitFrame` + `SplitAACFrame` коректно виділяють всі елементи
3. Валідує, що `CovertRbspToSodb` не видаляє легітимні дані, що випадково збігаються з `0x000003`

Це вбереже від регресій при рефакторингу низькорівневих функцій.