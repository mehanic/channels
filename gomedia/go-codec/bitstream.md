# 🪙 `codec/bitstream.go`: Низькорівневе бітове читання/запис для медіа-кодеків

Це **фундаментальний модуль** для роботи з бітовими потоками у бібліотеці медіа-кодеків. Він реалізує ефективне читання/запис даних з точністю до окремого біта — критично для парсингу/генерації заголовків кодеків (H.264, AAC, HEVC), де поля можуть мати довжину 1, 5, 13 або будь-яку іншу кількість біт.

---

## 🎯 Коротка відповідь

> **Це "бітовий скальпель" для медіа-кодеків**: він дозволяє читати/записувати дані з точністю до біта, буферизує байти для ефективності, підтримує відкат (`UnRead`), маркування позицій (`Markdot`) та спеціальні алгоритми кодування (Експоненційний Голумб) — все те, що потрібно для коректної обробки бітових потоків у стандартах ISO/IEC 14496, ITU-T H.264 тощо.

---

## 🧱 Основні компоненти

### 🔹 `BitMask` — таблиця бітових масок

```go
var BitMask [8]byte = [8]byte{0x01, 0x03, 0x07, 0x0F, 0x1F, 0x3F, 0x7F, 0xFF}
// 🔹 Відповідно: 00000001, 00000011, ..., 11111111
```

**🎯 Призначення**: Швидке виділення молодших `n` біт через `value & BitMask[n-1]` без циклів чи обчислень.

**🔢 Приклад:**
```
🔹 Виділити 5 молодших біт з 0b11010110:
   0b11010110 & BitMask[4] (0x1F = 0b00011111) = 0b00010110 = 22
```

---

### 🔹 `BitStream` — читач бітового потоку

```go
type BitStream struct {
    bits        []byte  // 🔹 Вхідний буфер
    bytesOffset int     // 🔹 Поточний байт у буфері
    bitsOffset  int     // 🔹 Поточний біт у байті (0-7)
    bitsmark    int     // 🔹 Збережена позиція біта для Markdot
    bytemark    int     // 🔹 Збережена позиція байта для Markdot
}
```

**🎯 Призначення**: Інкапсулювати **стан бітового читання** для ефективного парсингу.

**🔄 Візуалізація стану:**
```
🔹 Вхід: []byte{0b10110101, 0b01100011, ...}
🔹 Початок: bytesOffset=0, bitsOffset=0
📊 Біти:    [1][0][1][1][0][1][0][1] [0][1][1][0][0][0][1][1] ...
            ↑
            читаємо звідси (зліва направо, старший біт перший)

🔹 Після GetBits(5):
• Прочитано: 1,0,1,1,0 → значення 0b10110 = 22
• bytesOffset=0, bitsOffset=5 (залишилось 3 біти у першому байті)
```

---

### 🔹 Ключові методи читання

#### 🔸 `GetBits(n int) uint64` — читання довільної кількості біт

```go
func (bs *BitStream) GetBits(n int) uint64 {
    // 🔹 Перевірка меж
    if bs.bytesOffset >= len(bs.bits) {
        panic("OUT OF RANGE")
    }
    
    var ret uint64 = 0
    
    // 🔹 Випадок 1: всі біти вміщуються у поточному байті
    if 8-bs.bitsOffset >= n {
        ret = uint64((bs.bits[bs.bytesOffset] >> (8 - bs.bitsOffset - n)) & BitMask[n-1])
        bs.bitsOffset += n
        if bs.bitsOffset == 8 {
            bs.bytesOffset++
            bs.bitsOffset = 0
        }
    } else {
        // 🔹 Випадок 2: біти розтягнуті на кілька байт
        // 🔹 Крок 1: дочитати залишок поточного байта
        ret = uint64(bs.bits[bs.bytesOffset] & BitMask[8-bs.bitsOffset-1])
        bs.bytesOffset++
        n -= 8 - bs.bitsOffset
        bs.bitsOffset = 0
        
        // 🔹 Крок 2: читати повні байти
        for n > 0 {
            if n >= 8 {
                ret = ret<<8 | uint64(bs.bits[bs.bytesOffset])
                bs.bytesOffset++
                n -= 8
            } else {
                // 🔹 Крок 3: дочитати залишок з останнього байта
                ret = (ret << n) | uint64((bs.bits[bs.bytesOffset]>>(8-n))&BitMask[n-1])
                bs.bitsOffset = n
                break
            }
        }
    }
    return ret
}
```

**🔄 Потік даних:**
```
🔹 Вхід: []byte{0b10110101, 0b01100011}, n=10
│
▼
🔹 8-bs.bitsOffset=8, n=10 → випадок 2 (міжбайтове читання)
│
▼
🔹 Крок 1: залишок першого байта (8-0=8 біт, але потрібно 10)
   • ret = 0b10110101 & 0xFF = 0b10110101
   • bytesOffset=1, n=10-8=2, bitsOffset=0
│
▼
🔹 Крок 2: n=2 < 8 → читання залишку з другого байта
   • (0b01100011 >> (8-2)) & BitMask[1] = (0b01100011 >> 6) & 0x01 = 0b01 & 0x01 = 1
   • ret = (0b10110101 << 2) | 1 = 0b1011010100 | 1 = 0b1011010101 = 725
   • bitsOffset=2 (залишилось 6 біт у другому байті)
│
▼
🔹 Вихід: 725 (0b1011010101)
```

**🎯 Призначення**: Читати поля довільної довжини (1-64 біти) з ефективним буферизуванням.

---

#### 🔸 `ReadUE()` / `ReadSE()` — декодування експоненційного Голумба

```go
// 🔹 Беззнакове кодування (unsigned Exponential-Golomb)
func (bs *BitStream) ReadUE() uint64 {
    leadingZeroBits := 0
    for bs.GetBit() == 0 {  // 🔹 Підрахунок нулів на початку
        leadingZeroBits++
    }
    if leadingZeroBits == 0 {
        return 0
    }
    info := bs.GetBits(leadingZeroBits)  // 🔹 Читання інформаційної частини
    return uint64(1)<<leadingZeroBits - 1 + info  // 🔹 Формула декодування
}

// 🔹 Знакове кодування (signed Exponential-Golomb)
func (bs *BitStream) ReadSE() int64 {
    v := bs.ReadUE()  // 🔹 Спочатку декодуємо як unsigned
    if v%2 == 0 {
        return -1 * int64(v/2)  // 🔹 Парні → від'ємні
    } else {
        return int64(v+1) / 2   // 🔹 Nepарні → додатні
    }
}
```

**🎯 Призначення**: Декодувати змінні довжини кодів, що використовуються у H.264/AVC для макроблоків, рухових векторів тощо.

**🔢 Приклад декодування:**
```
🔹 Потік біт: 0001011...
│
▼
🔹 ReadUE():
   • leadingZeroBits = 3 (три нулі)
   • info = GetBits(3) = 011 = 3
   • Результат = (1<<3) - 1 + 3 = 8 - 1 + 3 = 10
│
▼
🔹 ReadSE() з тим же потоком:
   • v = 10 (парне)
   • Результат = -1 * (10/2) = -5
```

---

#### 🔸 `Markdot()` / `DistanceFromMarkDot()` — маркування позицій

```go
func (bs *BitStream) Markdot() {
    bs.bitsmark = bs.bitsOffset
    bs.bytemark = bs.bytesOffset
}

func (bs *BitStream) DistanceFromMarkDot() int {
    bytecount := bs.bytesOffset - bs.bytemark - 1
    bitscount := bs.bitsOffset + (8 - bs.bitsmark)
    return bytecount*8 + bitscount
}
```

**🎯 Призначення**: Зберегти поточну позицію для подальшого обчислення відстані або відкату — корисно для парсингу змінних структур даних.

**🔢 Приклад:**
```
🔹 Початок: bytesOffset=5, bitsOffset=3
🔹 Markdot() → bitsmark=3, bytemark=5
🔹 Читання 10 біт → bytesOffset=6, bitsOffset=5
🔹 DistanceFromMarkDot() = (6-5-1)*8 + (5 + (8-3)) = 0 + 10 = 10 біт ✅
```

---

#### 🔸 `UnRead(n int)` — відкат позиції

```go
func (bs *BitStream) UnRead(n int) {
    if n-bs.bitsOffset <= 0 {
        bs.bitsOffset -= n
    } else {
        least := n - bs.bitsOffset
        for least >= 8 {
            bs.bytesOffset--
            least -= 8
        }
        if least > 0 {
            bs.bytesOffset--
            bs.bitsOffset = 8 - least
        }
    }
}
```

**🎯 Призначення**: "Відмотати" позицію читання назад на `n` біт — критично для повторного парсингу або перевірки гіпотез.

**⚠️ Обмеження**: Не перевіряє меж масиву — може призвести до паніки при неправильному використанні.

---

### 🔹 `BitStreamWriter` — записувач бітового потоку

```go
type BitStreamWriter struct {
    bits       []byte   // 🔹 Вихідний буфер (динамічно розширюється)
    byteoffset int      // 🔹 Поточний байт
    bitsoffset int      // 🔹 Поточний біт у байті
    bitsmark   int      // 🔹 Збережена позиція для Markdot
    bytemark   int      // 🔹 Збережена позиція для Markdot
}
```

**🎯 Призначення**: Серіалізувати дані у бітовий потік з підтримкою динамічного розширення буфера.

---

#### 🔸 `PutUint64(v uint64, n int)` — запис довільної кількості біт

```go
func (bsw *BitStreamWriter) PutUint64(v uint64, n int) {
    bsw.expandSpace(n)  // 🔹 Гарантувати достатньо місця
    
    // 🔹 Випадок 1: всі біти вміщуються у поточному байті
    if 8-bsw.bitsoffset >= n {
        bsw.bits[bsw.byteoffset] |= uint8(v) & BitMask[n-1] << (8 - bsw.bitsoffset - n)
        bsw.bitsoffset += n
        if bsw.bitsoffset == 8 {
            bsw.bitsoffset = 0
            bsw.byteoffset++
        }
    } else {
        // 🔹 Випадок 2: міжбайтове записування
        // 🔹 Крок 1: записати залишок поточного байта
        bsw.bits[bsw.byteoffset] |= uint8(v>>(n-int(8-bsw.bitsoffset))) & BitMask[8-bsw.bitsoffset-1]
        bsw.byteoffset++
        n -= 8 - bsw.bitsoffset
        
        // 🔹 Крок 2: записати повні байти
        for n-8 >= 0 {
            bsw.bits[bsw.byteoffset] = uint8(v>>(n-8)) & 0xFF
            bsw.byteoffset++
            n -= 8
        }
        
        // 🔹 Крок 3: записати залишок у новий байт
        bsw.bitsoffset = n
        if n > 0 {
            bsw.bits[bsw.byteoffset] |= (uint8(v) & BitMask[n-1]) << (8 - n)
        }
    }
}
```

**🔄 Потік даних:**
```
🔹 Вхід: v=0b1011010101 (725), n=10, початковий стан: byteoffset=0, bitsoffset=0
│
▼
🔹 8-0=8 < 10 → випадок 2
│
▼
🔹 Крок 1: записати старші 8 біт у перший байт
   • 725 >> (10-8) = 725 >> 2 = 0b10110101
   • bits[0] |= 0b10110101 & 0xFF = 0b10110101
   • byteoffset=1, n=10-8=2
│
▼
🔹 Крок 2: n=2 < 8 → записати молодші 2 біти у другий байт
   • bits[1] |= (725 & BitMask[1]) << (8-2) = (1) << 6 = 0b01000000
   • bitsoffset=2
│
▼
🔹 Вихід: []byte{0b10110101, 0b01000000} (з 6 нулями у другому байті)
```

**🎯 Призначення**: Записувати поля довільної довжини з автоматичним вирівнюванням та розширенням буфера.

---

#### 🔸 `expandSpace(n int)` — динамічне розширення буфера

```go
func (bsw *BitStreamWriter) expandSpace(n int) {
    // 🔹 Перевірка: чи вистачає місця для n біт
    if (len(bsw.bits)-bsw.byteoffset-1)*8+8-bsw.bitsoffset < n {
        newlen := 0
        if len(bsw.bits)*8 < n {
            newlen = len(bsw.bits) + n/8 + 1  // 🔹 Додати точно стільки, скільки потрібно
        } else {
            newlen = len(bsw.bits) * 2  // 🔹 Подвоїти розмір для амортизації
        }
        tmp := make([]byte, newlen)
        copy(tmp, bsw.bits)
        bsw.bits = tmp
    }
}
```

**🎯 Призначення**: Забезпечити **амортизовану складність O(1)** для запису через подвоєння буфера.

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Паніка замість помилок

```go
func (bs *BitStream) GetBits(n int) uint64 {
    if bs.bytesOffset >= len(bs.bits) {
        panic("OUT OF RANGE")  // ❌ Небезпечно для продакшену
    }
    // ...
}
```

**✅ Рішення**: Повертати `(value, error)`:
```go
func (bs *BitStream) GetBits(n int) (uint64, error) {
    if n < 1 || n > 64 {
        return 0, fmt.Errorf("invalid bit count: %d (must be 1-64)", n)
    }
    if bs.bytesOffset >= len(bs.bits) {
        return 0, io.EOF
    }
    // ... парсинг
    return ret, nil
}
```

---

### 🔴 Проблема 2: Відсутність перевірки меж у `UnRead`

```go
func (bs *BitStream) UnRead(n int) {
    // 🔹 Може вийти за межі масиву при неправильному n
    bs.bytesOffset--  // ❌ Без перевірки
}
```

**✅ Рішення**: Додати валідацію:
```go
func (bs *BitStream) UnRead(n int) error {
    totalBits := bs.bytesOffset*8 + bs.bitsOffset
    if n > totalBits {
        return fmt.Errorf("cannot unread %d bits: only %d bits read", n, totalBits)
    }
    // ... логіка відкату
    return nil
}
```

---

### 🟡 Проблема 3: Неочевидна логіка бітових зсувів

```go
// 🔹 У GetBits:
ret = uint64((bs.bits[bs.bytesOffset] >> (8 - bs.bitsOffset - n)) & BitMask[n-1])
// 🔹 Складна формула зсуву важко читати та підтримувати
```

**✅ Рішення**: Винести у допоміжну функцію з коментарем:
```go
// extractBits extracts n bits starting from bitOffset in byte b
// Bits are read MSB-first (bit 7 is first)
func extractBits(b byte, bitOffset, n int) uint8 {
    shift := 8 - bitOffset - n  // 🔹 Зсув для вирівнювання потрібних біт у молодші позиції
    mask := BitMask[n-1]         // 🔹 Маска для виділення n молодших біт
    return (b >> shift) & mask
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Парсинг SPS (Sequence Parameter Set) у H.264

```go
func ParseSPS(spsNALU []byte) (*H264SPS, error) {
    // 🔹 Пропустити заголовок NALU (1 байт)
    bs := codec.NewBitStream(spsNALU[1:])
    
    sps := &H264SPS{}
    
    // 🔹 Читання полів згідно зі специфікацією H.264
    sps.ProfileIDC = bs.Uint8(8)
    sps.ConstraintSetFlags = bs.Uint8(8)
    sps.LevelIDC = bs.Uint8(8)
    
    // 🔹 Decodue seq_parameter_set_id (експоненційний Голумб)
    sps.SeqParameterSetID = int(bs.ReadUE())
    
    // 🔹 Читання log2_max_frame_num_minus4
    sps.Log2MaxFrameNum = int(bs.ReadUE()) + 4
    
    // 🔹 Читання pic_order_cnt_type
    picOrderCntType := bs.ReadUE()
    if picOrderCntType == 0 {
        sps.Log2MaxPicOrderCntLsb = int(bs.ReadUE()) + 4
    }
    
    // 🔹 ... інші поля ...
    
    return sps, nil
}
```

---

### 🔹 Приклад 2: Генерація заголовка AAC з бітовим записом

```go
func GenerateAACHeader(profile codec.AAC_PROFILE, sampleRateIdx int, channels int, frameLength int) ([]byte, error) {
    // 🔹 Створення записувача з запасом місця (7 байт для ADTS)
    bsw := codec.NewBitStreamWriter(10)
    
    // 🔹 Запис syncword (12 біт = 0xFFF)
    bsw.PutUint64(0xFFF, 12)
    
    // 🔹 Запис фіксованого заголовка
    bsw.PutUint64(0, 1)  // ID = 0 (MPEG-4)
    bsw.PutUint64(0, 2)  // layer = 0
    bsw.PutUint64(1, 1)  // protection_absent = 1 (без CRC)
    bsw.PutUint64(uint64(profile), 2)  // profile
    bsw.PutUint64(uint64(sampleRateIdx), 4)  // sampling_frequency_index
    bsw.PutUint64(0, 1)  // private_bit
    bsw.PutUint64(uint64(channels), 3)  // channel_configuration
    bsw.PutUint64(0, 1)  // original/copy
    bsw.PutUint64(0, 1)  // home
    
    // 🔹 Запис змінного заголовка
    bsw.PutUint64(0, 2)  // copyright bits
    bsw.PutUint64(uint64(frameLength), 13)  // frame_length
    bsw.PutUint64(0x7FF, 11)  // adts_buffer_fullness (VBR)
    bsw.PutUint64(0, 2)  // number_of_raw_data_blocks_in_frame
    
    return bsw.Bits()[:7], nil  // 🔹 Повернути тільки 7 байт заголовка
}
```

---

### 🔹 Приклад 3: Ефективне копіювання бітових потоків

```go
func CopyBitStream(src *codec.BitStream, dst *codec.BitStreamWriter, bitCount int) error {
    // 🔹 Читати та записувати блоками по 32 біти для ефективності
    for bitCount > 0 {
        n := bitCount
        if n > 32 { n = 32 }  // 🔹 Максимум 32 біти за раз для uint32
        
        value := src.GetBits(n)  // 🔹 Читання
        dst.PutUint64(value, n)   // 🔹 Запис
        
        bitCount -= n
    }
    return nil
}

// 🔹 Використання: перепакування аудіо з ADTS у fMP4
func RepackAAC(adtsStream []byte) ([]byte, error) {
    src := codec.NewBitStream(adtsStream)
    dst := codec.NewBitStreamWriter(len(adtsStream))
    
    // 🔹 Пропустити ADTS-заголовок (7 байт = 56 біт)
    src.SkipBits(56)
    
    // 🔹 Скопіювати аудіо-дані (без заголовка)
    remainingBits := src.RemainBits()
    CopyBitStream(src, dst, remainingBits)
    
    return dst.Bits(), nil
}
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При читанні бітових потоків:
    • Перевіряйте межі масиву перед GetBits/GetByte
    • Використовуйте ReadUE/ReadSE для полів H.264/HEVC
    • Зберігайте позицію через Markdot() перед парсингом змінних структур

[ ] При записі бітових потоків:
    • Використовуйте expandSpace() для гарантії місця
    • Групувайте біти у байти для зменшення викликів
    • Використовуйте PutBytes() для вирівняних даних замість побітового запису

[ ] Для безпеки:
    • Замініть panic на повернення помилок у публічних методах
    • Валідуйте n у діапазоні [1, 64] для GetBits/PutUint64
    • Перевіряйте межі при UnRead/NextBits

[ ] Для оптимізації:
    • Використовуйте BitMask для швидкого виділення біт
    • Читайте/записуйте блоками по 32/64 біти замість побітових операцій
    • Уникайте частих викликів GetBit/PutBit — використовуйте GetBits(n)

[ ] Для тестування:
    • Створюйте тестові вектори з відомими бітовими послідовностями
    • Перевіряйте round-trip: запис → читання → порівняння значень
    • Тестуйте крайні випадки: 1 біт, 64 біти, міжбайтові межі
```

---

## 🎯 Висновок

> **Цей модуль — "універсальний інструмент" для бітової обробки**, який забезпечує:
> • ✅ Точне читання/запис полів довільної бітової довжини (1-64 біти)
> • ✅ Ефективну буферизацію: один системний виклик на 8+ біт
> • ✅ Підтримку спеціальних алгоритмів: експоненційний Голумб для H.264/HEVC
> • ✅ Гнучкість: маркування позицій, відкат, перевірка залишку
> • ✅ Інтеграцію з медіа-кодеками через типобезпечний API

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Надійний парсинг заголовків H.264/AAC з точністю до біта
- ⚡ Ефективна генерація транспортних потоків (ADTS, TS) без зайвих копіювань
- 🔄 Прозора конвертація між форматами через бітові операції
- 🛡️ Захист від невалідних даних через валідацію меж та форматів
- 🧪 Легке тестування через контрольовані бітові вектори

Потребуєте допомоги з інтеграцією цього модуля у парсинг конкретних кодеків (H.264 SPS/PPS, AAC ASC) або з оптимізацією бітових операцій для високопродуктивних сценаріїв? Напишіть — покажу готовий код для вашого випадку! 🚀🪙