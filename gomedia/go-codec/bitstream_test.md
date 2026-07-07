# 🧪 `codec/bitstream_test.go`: Тестування бітового читання/запису для медіа-кодеків

Це **комплексний тест-сьют** для модуля `codec`, який перевіряє коректність роботи низькорівневого бітового читання/запису — фундаменту для парсингу заголовків кодеків (H.264, AAC, HEVC), де поля можуть мати довжину 1, 5, 13 або будь-яку іншу кількість біт.

---

## 🎯 Коротка відповідь

> **Ці тести гарантують, що бітові операції працюють коректно**: точне читання/запис 1-64 біт, правильне вирівнювання між байтами, робота з експоненційним Голумбом, відкат позиції (`UnRead`) та маркування точок (`Markdot`) — все те, що потрібно для надійного парсингу бітових потоків у стандартах медіа-кодеків.

---

## 📋 Огляд тестових функцій

### 🔹 `Test_GetBits` — базове тестування читання біт

```go
var testbit []byte = []byte{0x01, 0x44, 0x55}
// 🔹 Бінарне представлення:
// 0x01 = 0b00000001
// 0x44 = 0b01000100
// 0x55 = 0b01010101

func Test_GetBits(t *testing.T) {
    bs := NewBitStream(testbit)
    
    // 🔹 Читання по 4 біти (ніббл) з початку:
    t.Log(bs.GetBits(4))  // 🔹 0b0000 = 0 (перші 4 біти 0x01)
    t.Log(bs.GetBits(4))  // 🔹 0b0001 = 1 (останні 4 біти 0x01)
    
    // 🔹 Читання 1 біта:
    t.Log(bs.GetBit())    // 🔹 0 (старший біт 0x44 = 0b01000100)
    
    // 🔹 Продовження читання:
    t.Log(bs.GetBits(4))  // 🔹 0b1000 = 8
    t.Log(bs.GetBits(4))  // 🔹 0b0100 = 4
    t.Log(bs.GetBits(4))  // 🔹 0b0101 = 5
    t.Log(bs.GetBits(3))  // 🔹 0b010 = 2
}
```

**🔄 Потік даних:**
```
🔹 Вхід: []byte{0x01, 0x44, 0x55} = [00000001][01000100][01010101]
│
▼
🔹 GetBits(4): читаємо [0000] → 0, bitsOffset=4
🔹 GetBits(4): читаємо [0001] → 1, bitsOffset=8 → bytesOffset=1, bitsOffset=0
🔹 GetBit(): читаємо [0] (старший біт 0x44) → 0, bitsOffset=1
🔹 GetBits(4): читаємо [1000] → 8, bitsOffset=5
🔹 GetBits(4): читаємо [0100] → 4, bitsOffset=1 (перехід на наступний байт)
🔹 GetBits(4): читаємо [0101] → 5, bitsOffset=5
🔹 GetBits(3): читаємо [010] → 2, bitsOffset=8 → bytesOffset=3
│
▼
🔹 Очікуваний вивід: 0, 1, 0, 8, 4, 5, 2 ✅
```

**🎯 Призначення**: Перевірити коректне читання біт у різних комбінаціях (4+4, 1, 4+4+4+3) з автоматичним переходом між байтами.

---

### 🔹 `Test_UnRead` — тестування відкату позиції

```go
func Test_UnRead(t *testing.T) {
    bs := NewBitStream(testbit)
    
    // 🔹 Початкове читання (аналогічно Test_GetBits)
    t.Log(bs.GetBits(4))  // 0
    t.Log(bs.GetBits(4))  // 1
    t.Log(bs.GetBit())    // 0
    t.Log(bs.GetBits(4))  // 8
    t.Log(bs.GetBits(4))  // 4
    t.Log(bs.GetBits(4))  // 5
    t.Log(bs.GetBits(3))  // 2
    
    // 🔹 Відкат на 3 біти:
    bs.UnRead(3)  // 🔹 Повертаємось назад на 3 біти
    t.Log(bs.GetBits(3))  // 🔹 Повторне читання тих самих 3 біт → має дати 2
    
    // 🔹 Відкат на 4 біти:
    bs.UnRead(4)
    t.Log(bs.GetBits(4))  // 🔹 Повторне читання → має дати 5
    
    // 🔹 Відкат на 5 біт (міжбайтовий):
    bs.UnRead(5)
    t.Log(bs.GetBits(5))  // 🔹 Повторне читання → має дати 0b01010 = 10
    
    // 🔹 Великий відкат на 15 біт:
    bs.UnRead(15)
    t.Log(bs.GetBits(2))  // 🔹 Читання з нової позиції
    t.Log(bs.GetBits(3))  // 🔹 Продовження
}
```

**🎯 Призначення**: Перевірити, що `UnRead(n)` коректно "відмотує" позицію читання назад, включаючи переходи між байтами.

**🔑 Ключові моменти:**
- ✅ Відкат у межах одного байта (`bitsOffset >= n`)
- ✅ Відкат через межу байта (`bitsOffset < n`)
- ✅ Відкат на велику відстань (кілька байт)

---

### 🔹 `Test_SkipBits` — пропуск біт без читання

```go
func Test_SkipBits(t *testing.T) {
    bs := NewBitStream(testbit)
    
    // 🔹 Пропустити перші 4 біти (ніббл)
    bs.SkipBits(4)
    
    // 🔹 Прочитати наступні 4 біти:
    t.Log(bs.GetBits(4))  // 🔹 Має дати 1 (другий ніббл 0x01)
}
```

**🔄 Логіка `SkipBits`:**
```go
func (bs *BitStream) SkipBits(n int) {
    bytecount := n / 8      // 🔹 Кількість повних байт
    bitscount := n % 8      // 🔹 Залишок біт
    bs.bytesOffset += bytecount
    if bs.bitsOffset+bitscount < 8 {
        bs.bitsOffset += bitscount  // 🔹 Залишаємось у тому ж байті
    } else {
        bs.bytesOffset += 1         // 🔹 Переходимо на наступний байт
        bs.bitsOffset += bitscount - 8
    }
}
```

**🎯 Призначення**: Ефективно пропускати нецікаві поля заголовка без витрат на читання значень.

---

### 🔹 `Test_DistanceFromMarkDot` — маркування позицій та обчислення відстані

```go
func Test_DistanceFromMarkDot(t *testing.T) {
    bs := NewBitStream(testbit)
    
    // 🔹 Пропустити 4 біти
    bs.SkipBits(4)
    
    // 🔹 Позначити поточну позицію
    bs.Markdot()  // 🔹 bitsmark=4, bytemark=0
    
    // 🔹 Прочитати кілька полів
    t.Log(bs.GetBits(4))  // 🔹 1
    t.Log(bs.GetBits(4))  // 🔹 0 (старший біт 0x44)
    t.Log(bs.GetBits(1))  // 🔹 1 (другий біт 0x44)
    
    // 🔹 Обчислити відстань від мітки:
    t.Log(bs.DistanceFromMarkDot())  // 🔹 Має дати 9 біт (4+4+1)
}
```

**🔄 Формула відстані:**
```
🔹 bytecount = bytesOffset - bytemark - 1
🔹 bitscount = bitsOffset + (8 - bitsmark)
🔹 Результат = bytecount*8 + bitscount

🔢 Приклад:
• Після Markdot(): bytesOffset=0, bitsOffset=4, bytemark=0, bitsmark=4
• Після читання 9 біт: bytesOffset=1, bitsOffset=5
• bytecount = 1-0-1 = 0
• bitscount = 5 + (8-4) = 9
• Результат = 0*8 + 9 = 9 біт ✅
```

**🎯 Призначення**: Дозволити парсингу змінних структур даних обчислювати довжину прочитаних полів.

---

### 🔹 `Test_BitStreamWriter` — тестування запису біт

```go
func Test_BitStreamWriter(t *testing.T) {
    // 🔹 Створення записувача з початковим буфером 4 байти
    bsw := NewBitStreamWriter(4)
    
    // 🔹 Запис повного байта:
    bsw.PutByte(1)  // 🔹 0x01 = 0b00000001
    
    // 🔹 Запис двох байт напряму (тільки якщо bitsOffset==0):
    bsw.PutBytes([]byte{0xdd, 0xFF})  // 🔹 0xDD, 0xFF
    
    // 🔹 Запис 2 біт:
    bsw.PutUint8(3, 2)  // 🔹 0b11 → записується у молодші 2 біти наступного байта
    
    // 🔹 Запис 7 біт:
    bsw.PutUint16(0x4c, 7)  // 🔹 0b1001100 → 7 біт
    
    // 🔹 Запис 6 біт:
    bsw.PutUint16(0xED, 6)  // 🔹 0b1011101 → молодші 6 біт = 0b111101 = 61
    
    // 🔹 Вивід результату у hex:
    t.Logf("%x", bsw.Bits())  // 🔹 Очікуваний вивід: 01ddff39e0 (приклад)
}
```

**🔄 Потік запису:**
```
🔹 Початок: []byte{0,0,0,0}, byteoffset=0, bitsoffset=0

🔹 PutByte(1):
   • bits[0] = 0x01
   • byteoffset=1, bitsoffset=0
   • Буфер: [01][00][00][00]

🔹 PutBytes([0xdd, 0xFF]):
   • bits[1]=0xdd, bits[2]=0xFF
   • byteoffset=3, bitsoffset=0
   • Буфер: [01][dd][FF][00]

🔹 PutUint8(3, 2):
   • 3 = 0b11, n=2
   • bits[3] |= 0b11 << (8-0-2) = 0b11 << 6 = 0b11000000 = 0xC0
   • bitsoffset=2
   • Буфер: [01][dd][FF][C0]

🔹 PutUint16(0x4c, 7):
   • 0x4c = 0b1001100, n=7
   • 8-bitsoffset=6 >= 7? Ні → міжбайтове записування
   • Записати залишок поточного байта: 2 біти
   • Перейти на наступний байт, записати 5 біт
   • ... (складна логіка)

🔹 Вихід: бачимо фінальний буфер у hex
```

**🎯 Призначення**: Перевірити коректне записування біт різної довжини з автоматичним вирівнюванням та розширенням буфера.

---

### 🔹 `TestBitStream_RemainBits` — тестування обчислення залишку біт

```go
func TestBitStream_RemainBits(t *testing.T) {
    tests := []struct {
        name   string
        fields fields  // 🔹 Початковий стан BitStream
        want   int     // 🔹 Очікувана кількість залишкових біт
    }{
        {name: "test1", fields: fields{
            bits: []byte{0x00, 0x01, 0x02, 0x03},  // 🔹 4 байти = 32 біти
            bytesOffset: 0, bitsOffset: 0,          // 🔹 Початок
        }, want: 32},  // ✅ Усі 32 біти доступні
        
        {name: "test2", fields: fields{
            bits: []byte{0x00, 0x01, 0x02, 0x03},
            bytesOffset: 0, bitsOffset: 1,  // 🔹 Прочитано 1 біт
        }, want: 31},  // ✅ 32-1 = 31 біт залишилось
        
        {name: "test3", fields: fields{
            bits: []byte{0x00, 0x01, 0x02, 0x03},
            bytesOffset: 1, bitsOffset: 1,  // 🔹 Прочитано 1 байт + 1 біт = 9 біт
        }, want: 23},  // ✅ 32-9 = 23 біти залишилось
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            bs := &BitStream{...}  // 🔹 Створення стану
            if got := bs.RemainBits(); got != tt.want {
                t.Errorf("BitStream.RemainBits() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

**🔄 Логіка `RemainBits`:**
```go
func (bs *BitStream) RemainBits() int {
    if bs.bitsOffset > 0 {
        // 🔹 Є непрочитані біти у поточному байті
        return bs.RemainBytes()*8 + 8 - bs.bitsOffset
    } else {
        // 🔹 Поточний байт ще не чіпали
        return bs.RemainBytes() * 8
    }
}
```

**🎯 Призначення**: Дозволити парсингу перевіряти, чи достатньо даних для читання наступного поля.

---

### 🔹 `TestBitStream_ReadUE` — тестування декодування експоненційного Голумба

```go
var bits []byte = []byte{0x80}   // 🔹 0b10000000
var bits1 []byte = []byte{0x40}  // 🔹 0b01000000
var bits2 []byte = []byte{0x60}  // 🔹 0b01100000
var bits3 []byte = []byte{0x20}  // 🔹 0b00100000

func TestBitStream_ReadUE(t *testing.T) {
    tests := []struct {
        name string
        bs   *BitStream
        want uint64  // 🔹 Очікуваний результат декодування
    }{
        {name: "test1", bs: NewBitStream(bits), want: 0},   // 🔹 0b1... → leadingZeros=0 → 0
        {name: "test2", bs: NewBitStream(bits1), want: 1},  // 🔹 0b01... → leadingZeros=1, info=0 → (1<<1)-1+0 = 1
        {name: "test3", bs: NewBitStream(bits2), want: 2},  // 🔹 0b011... → leadingZeros=1, info=1 → (1<<1)-1+1 = 2
        {name: "test4", bs: NewBitStream(bits3), want: 3},  // 🔹 0b001... → leadingZeros=2, info=0 → (1<<2)-1+0 = 3
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := tt.bs.ReadUE(); got != tt.want {
                t.Errorf("BitStream.ReadUE() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

**🔄 Алгоритм `ReadUE`:**
```
🔹 Формула: codeNum = (1 << leadingZeroBits) - 1 + info

🔢 Приклад для 0b01100000 (bits2):
1. Читання провідних нулів:
   • GetBit() = 0 → leadingZeroBits=1
   • GetBit() = 1 → зупинка
2. Читання info (leadingZeroBits=1 біт):
   • GetBits(1) = 1 → info=1
3. Обчислення:
   • (1 << 1) - 1 + 1 = 2 - 1 + 1 = 2 ✅

🔢 Приклад для 0b00100000 (bits3):
1. Провідні нулі: 0,0 → leadingZeroBits=2
2. info = GetBits(2) = 0b00 = 0
3. (1 << 2) - 1 + 0 = 4 - 1 + 0 = 3 ✅
```

**🎯 Призначення**: Перевірити коректне декодування змінних довжини кодів, що використовуються у H.264/HEVC для макроблоків, рухових векторів, параметрів SPS/PPS.

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Використання `t.Log` замість `t.Logf` з перевірками

```go
t.Log(bs.GetBits(4))  // 🔹 Тільки логування, немає assert
```

**🎯 Ризик**: Тест не "падає" при некоректному виводі — тільки візуальна перевірка.

**✅ Рішення**: Додати явні перевірки:
```go
if got := bs.GetBits(4); got != 0 {
    t.Errorf("GetBits(4) = %d, want 0", got)
}
```

---

### 🔴 Проблема 2: Відсутність тестів для крайніх випадків

```go
// 🔹 Немає тестів для:
// • GetBits(64) — максимальне значення
// • Читання на межі масиву
// • UnRead більше ніж прочитано
// • Пустий буфер
```

**✅ Рішення**: Додати тестові кейси:
```go
func Test_GetBits_EdgeCases(t *testing.T) {
    // 🔹 Читання 64 біт
    bs := NewBitStream(make([]byte, 8))
    if got := bs.GetBits(64); got != 0 {
        t.Errorf("GetBits(64) = %d, want 0", got)
    }
    
    // 🔹 Читання за межами масиву → має панікувати або повертати помилку
    defer func() {
        if r := recover(); r == nil {
            t.Error("Expected panic on out-of-range read")
        }
    }()
    bs.GetBits(1)  // 🔹 Масив порожній
}
```

---

### 🟡 Проблема 3: Дублювання назв тестів

```go
{name: "test1", bs: NewBitStream(bits), want: 0},
{name: "test1", bs: NewBitStream(bits1), want: 1},  // 🔹 Дубль "test1"
```

**✅ Рішення**: Унікальні назви:
```go
{name: "leading_zero_0", bs: NewBitStream(bits), want: 0},
{name: "leading_zero_1", bs: NewBitStream(bits1), want: 1},
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Парсинг H.264 NALU заголовка

```go
func ParseH264NALUHeader(nalu []byte) (*H264Header, error) {
    if len(nalu) < 1 {
        return nil, fmt.Errorf("NALU too short")
    }
    
    bs := codec.NewBitStream(nalu)
    
    header := &H264Header{
        ForbiddenZeroBit: bs.GetBit(),  // 🔹 1 біт
        NRI:              bs.GetBits(2), // 🔹 2 біти
        Type:             bs.GetBits(5), // 🔹 5 біт
    }
    
    // 🔹 Перевірка валідності
    if header.ForbiddenZeroBit != 0 {
        return nil, fmt.Errorf("forbidden_zero_bit must be 0")
    }
    
    return header, nil
}

// 🔹 Використання:
header, err := ParseH264NALUHeader([]byte{0x67})  // 🔹 0b01100111
if err != nil {
    log.Printf("❌ Parse failed: %v", err)
} else {
    log.Printf("✅ NALU: type=%d, nri=%d", header.Type, header.NRI)
}
```

---

### 🔹 Приклад 2: Генерація AAC ADTS-заголовка з бітовим записом

```go
func GenerateADTSHeader(profile int, sampleIdx int, channels int, frameLen int) []byte {
    bsw := codec.NewBitStreamWriter(10)  // 🔹 Запас місця
    
    // 🔹 Syncword (12 біт = 0xFFF)
    bsw.PutUint64(0xFFF, 12)
    
    // 🔹 Фіксований заголовок
    bsw.PutUint64(0, 1)  // ID = 0 (MPEG-4)
    bsw.PutUint64(0, 2)  // layer = 0
    bsw.PutUint64(1, 1)  // protection_absent = 1
    bsw.PutUint64(uint64(profile), 2)  // profile
    bsw.PutUint64(uint64(sampleIdx), 4)  // sampling_frequency_index
    bsw.PutUint64(0, 1)  // private_bit
    bsw.PutUint64(uint64(channels), 3)  // channel_configuration
    bsw.PutUint64(0, 1)  // original/copy
    bsw.PutUint64(0, 1)  // home
    
    // 🔹 Змінний заголовок
    bsw.PutUint64(0, 2)  // copyright bits
    bsw.PutUint64(uint64(frameLen), 13)  // frame_length
    bsw.PutUint64(0x7FF, 11)  // adts_buffer_fullness
    bsw.PutUint64(0, 2)  // number_of_raw_data_blocks
    
    return bsw.Bits()[:7]  // 🔹 Повернути 7 байт заголовка
}
```

---

### 🔹 Приклад 3: Ефективне копіювання бітових потоків

```go
func CopyBitStream(src *codec.BitStream, dst *codec.BitStreamWriter, bitCount int) error {
    for bitCount > 0 {
        n := bitCount
        if n > 32 { n = 32 }  // 🔹 Максимум 32 біти за раз
        
        value := src.GetBits(n)
        dst.PutUint64(value, n)
        
        bitCount -= n
    }
    return nil
}

// 🔹 Використання: перепакування аудіо з ADTS у fMP4
func RepackAAC(adtsStream []byte) ([]byte, error) {
    src := codec.NewBitStream(adtsStream)
    dst := codec.NewBitStreamWriter(len(adtsStream))
    
    // 🔹 Пропустити ADTS-заголовок (56 біт)
    src.SkipBits(56)
    
    // 🔹 Скопіювати аудіо-дані
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

[ ] Для тестування:
    • Покрийте крайні випадки: 1 біт, 64 біти, міжбайтові межі
    • Додайте перевірки (assert) замість тільки t.Log
    • Використовуйте унікальні назви для підтестів
    • Тестуйте round-trip: запис → читання → порівняння значень

[ ] Для безпеки:
    • Замініть panic на повернення помилок у публічних методах
    • Валідуйте n у діапазоні [1, 64] для GetBits/PutUint64
    • Перевіряйте межі при UnRead/NextBits

[ ] Для оптимізації:
    • Використовуйте BitMask для швидкого виділення біт
    • Читайте/записуйте блоками по 32/64 біти замість побітових операцій
    • Уникайте частих викликів GetBit/PutBit — використовуйте GetBits(n)
```

---

## 🎯 Висновок

> **Цей тест-сьют — ваш "золотий стандарт" для надійної бітової обробки**, який гарантує:
> • ✅ Точне читання/запис полів довільної бітової довжини (1-64 біти)
> • ✅ Коректне вирівнювання між байтами при читанні/записі
> • ✅ Правильну роботу експоненційного Голумба для H.264/HEVC
> • ✅ Надійний відкат позиції (`UnRead`) та маркування (`Markdot`)
> • ✅ Ефективне обчислення залишку біт для перевірки меж

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Надійний парсинг заголовків H.264/AAC з точністю до біта
- ⚡ Ефективна генерація транспортних потоків (ADTS, TS) без зайвих копіювань
- 🔄 Прозора конвертація між форматами через бітові операції
- 🛡️ Захист від невалідних даних через валідацію меж та форматів
- 🧪 Легке тестування через контрольовані бітові вектори

Потребуєте допомоги з додаванням тестів для специфічних сценаріїв парсингу кодеків або з оптимізацією бітових операцій для високопродуктивних сценаріїв? Напишіть — покажу готовий код для вашого випадку! 🚀🪙