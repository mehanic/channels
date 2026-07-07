# 📦 Глибокий розбір: `bits.Reader/Writer` — Бітове читання/запис довільної довжини

Цей файл — **реалізація бітового потоку** для читання та запису довільної кількості біт (не кратних 8) з/у `io.Reader`/`io.Writer`. Це критично важливо для парсингу відео/аудіо кодеків (H.264/265, AAC, VLC тощо), де поля заголовків часто мають нестандартну довжину (напр. 5, 13, 33 біти).

---

## 🗺️ Архітектурна схема bits пакету

```
┌────────────────────────────────────────┐
│ 📦 bits — Bit-Level I/O Streams        │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Reader — читання біт з io.Reader    │
│  • Writer — запис біт у io.Writer      │
│  • 64-бітний буфер (bits uint64)       │
│  • n — кількість валідних біт у буфері │
│                                         │
│  🔄 Потік даних:                        │
│  io.Reader → байти → біти → ReadBits64 │
│  WriteBits64 → біти → байти → io.Writer│
│                                         │
│  📡 Порядок біт: MSB first (Big-Endian)│
│  • Перший прочитаний біт = старший     │
│  • Сумісний з MPEG, H.264, AAC стандартами│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Reader — читання біт з потоку

### Структура та стан:

```go
type Reader struct {
    R    io.Reader  // джерело байт
    n    int        // кількість валідних біт у bits (0..64)
    bits uint64     // буфер: біти вирівняні по MSB (старший біт = bit 63)
}
```

### 🔧 ReadBits64(n int) — основна логіка:

```go
func (self *Reader) ReadBits64(n int) (bits uint64, err error) {
    // 1. Якщо не вистачає біт у буфері — дочитати з джерела
    if self.n < n {
        var b [8]byte
        want := (n - self.n + 7) / 8  // скільки байт потрібно (округлення вгору)
        
        got, err := self.R.Read(b[:want])
        if err != nil { return 0, err }
        if got < want { return 0, io.EOF }  // неповне читання
        
        // Додавання нових байт у буфер (MSB first)
        for i := 0; i < got; i++ {
            self.bits <<= 8          // зсув існуючих біт вліво
            self.bits |= uint64(b[i])  // додавання нового байта у молодші 8 біт
        }
        self.n += got * 8  // оновлення кількості валідних біт
    }
    
    // 2. Витягування n біт з MSB сторони буфера
    bits = self.bits >> uint(self.n - n)
    
    // 3. Очищення витягнутих біт з буфера
    self.bits ^= bits << uint(self.n - n)
    self.n -= n
    
    return bits, nil
}
```

### 🔍 Візуалізація буфера:

```
Початковий стан:
  bits = 0x123456789ABCDEF0 (64 біти)
  n = 64

Запит: ReadBits64(12)

Крок 1: bits >> (64-12) = bits >> 52 = 0x123 (старші 12 біт)
Крок 2: Очищення:
  bits ^= 0x123 << 52
  bits = 0x3456789ABCDEF0 (залишилось 52 біти)
  n = 64 - 12 = 52

Результат: повернуто 0x123, буфер оновлено
```

### ⚠️ Критична проблема: очищення біт через XOR

```go
self.bits ^= bits << uint(self.n-n)  // ⚠️ НЕБЕЗПЕЧНО!
```

**Проблема**: XOR працює тільки якщо біти, що очищаються, точно співпадають з `bits`. Якщо у буфері були "сміттєві" біти праворуч, XOR може їх змінити неправильно.

**✅ Безпечніша альтернатива**:
```go
// Маска для очищення старших n біт
mask := ^uint64(0) << uint(64 - self.n + n)
self.bits &= ^mask  // або простіше:
self.bits <<= uint(n)  // зсув вліво автоматично "виштовхує" прочитані біти
self.n -= n
```

Або ще простіше — завжди тримати біти вирівняними по **молодшому** краю:

```go
// Альтернативна реалізація (біти вирівняні по LSB):
func (self *Reader) ReadBits64(n int) (uint64, error) {
    for self.n < n {
        b, err := self.R.ReadByte()
        if err != nil { return 0, err }
        self.bits = (self.bits << 8) | uint64(b)
        self.n += 8
    }
    result := self.bits >> uint(self.n - n)
    self.n -= n
    // Не потрібно очищати — наступне читання використає тільки self.n біт
    return result, nil
}
```

---

### 🔧 Read(p []byte) — реалізація io.Reader:

```go
func (self *Reader) Read(p []byte) (n int, err error) {
    for n < len(p) {
        want := 8
        if len(p)-n < want {
            want = len(p) - n
        }
        // Читання want*8 біт = want байт
        bits, err := self.ReadBits64(want * 8)
        if err != nil { break }
        
        // Конвертація 64-бітного значення у want байт (MSB first)
        for i := 0; i < want; i++ {
            p[n+i] = byte(bits >> uint((want-i-1)*8))
        }
        n += want
    }
    return n, err
}
```

### ✅ Ваш use-case: парсинг H.264 SPS заголовку

```go
// ParseSPSHeader — витягування полів з SPS NALU
func ParseSPSHeader(r *bits.Reader) (*SPSInfo, error) {
    // H.264 SPS: profile_idc (8 біт) + flags (8 біт) + level_idc (8 біт)
    profileIdc, err := r.ReadBits(8)
    if err != nil { return nil, err }
    
    constraintSetFlags, err := r.ReadBits(8)
    if err != nil { return nil, err }
    
    levelIdc, err := r.ReadBits(8)
    if err != nil { return nil, err }
    
    // seq_parameter_set_id: exp-Golomb кодоване (змінна довжина)
    spsId, err := readExpGolomb(r)
    if err != nil { return nil, err }
    
    return &SPSInfo{
        ProfileIdc:           uint8(profileIdc),
        ConstraintSetFlags:   uint8(constraintSetFlags),
        LevelIdc:             uint8(levelIdc),
        SPSId:                spsId,
    }, nil
}

// readExpGolomb — декодування експоненційного Голомба (змінна довжина)
func readExpGolomb(r *bits.Reader) (int, error) {
    // Крок 1: підрахунок провідних нулів
    leadingZeros := 0
    for {
        bit, err := r.ReadBits(1)
        if err != nil { return 0, err }
        if bit == 1 { break }
        leadingZeros++
    }
    
    // Крок 2: читання info біт
    info, err := r.ReadBits(leadingZeros)
    if err != nil { return 0, err }
    
    // Формула: codeNum = 2^leadingZeros - 1 + info
    return (1 << leadingZeros) - 1 + int(info), nil
}
```

---

## 🔑 2. Writer — запис біт у потік

### Структура та стан:

```go
type Writer struct {
    W    io.Writer  // приймач байт
    n    int        // кількість накопичених біт у bits
    bits uint64     // буфер: біти вирівняні по MSB
}
```

### 🔧 WriteBits64(bits uint64, n int) — основна логіка:

```go
func (self *Writer) WriteBits64(bits uint64, n int) (err error) {
    // 1. Якщо буфер переповниться — флеш перед додаванням
    if self.n + n > 64 {
        move := uint(64 - self.n)  // скільки біт ще вміщається
        mask := bits >> move        // біти, що помістяться у поточний буфер
        
        self.bits = (self.bits << move) | mask  // додавання
        self.n = 64
        
        if err = self.FlushBits(); err != nil {
            return err
        }
        
        // Обробка решти біт
        n -= int(move)
        bits ^= (mask << move)  // ⚠️ Та сама проблема з XOR!
    }
    
    // 2. Додавання біт у буфер
    self.bits = (self.bits << uint(n)) | bits
    self.n += n
    return nil
}
```

### 🔧 FlushBits() — запис накопичених біт:

```go
func (self *Writer) FlushBits() (err error) {
    if self.n > 0 {
        var b [8]byte
        bits := self.bits
        
        // Вирівнювання по байту: якщо n не кратне 8, доповнюємо нулями справа
        if self.n%8 != 0 {
            bits <<= uint(8 - (self.n % 8))  // padding нулями
        }
        
        want := (self.n + 7) / 8  // скільки байт потрібно
        for i := 0; i < want; i++ {
            // Витягування байт з MSB сторони
            b[i] = byte(bits >> uint((want-i-1)*8))
        }
        
        if _, err = self.W.Write(b[:want]); err != nil {
            return err
        }
        self.n = 0  // скидання буфера
    }
    return nil
}
```

### ✅ Ваш use-case: запис H.264 NALU заголовку

```go
// WriteNALUHeader — запис заголовку NALU (1 байт + опціонально)
func WriteNALUHeader(w *bits.Writer, nalType uint8, refIdc uint8) error {
    // H.264 NAL header: [forbidden_zero_bit(1)][nal_ref_idc(2)][nal_unit_type(5)]
    
    // Запис 1 біт: forbidden_zero_bit = 0
    if err := w.WriteBits(0, 1); err != nil { return err }
    
    // Запис 2 біти: nal_ref_idc
    if err := w.WriteBits(uint(refIdc&0x3), 2); err != nil { return err }
    
    // Запис 5 біт: nal_unit_type
    if err := w.WriteBits(uint(nalType&0x1F), 5); err != nil { return err }
    
    // Флеш якщо потрібно (але зазвичай 1+2+5=8 біт = 1 байт)
    return w.FlushBits()
}

// Використання:
var buf bytes.Buffer
bw := bits.NewWriter(&buf)
WriteNALUHeader(bw, 7, 3)  // SPS NALU, high priority
// buf тепер містить 0x67 (0b01100111)
```

---

## 🔑 3. Конструктори та io.Reader/io.Writer сумісність

### 🔧 Відсутні конструктори (проблема!):

```go
// ❌ У коді немає:
// func NewReader(r io.Reader) *Reader { return &Reader{R: r} }
// func NewWriter(w io.Writer) *Writer { return &Writer{W: w} }
```

**Наслідки**: Користувачі мають ініціалізувати структури вручну → ризик помилок.

**✅ Виправлення**:
```go
func NewReader(r io.Reader) *Reader {
    return &Reader{R: r}
}

func NewWriter(w io.Writer) *Writer {
    return &Writer{W: w}
}
```

### 🔧 io.Reader/io.Writer реалізація:

```go
// Reader.Read() — читає байти, використовуючи ReadBits64
// Writer.Write() — пише байти через WriteBits64

// Це дозволяє використовувати bits.Reader як io.Reader:
var r bits.Reader = *bits.NewReader(someReader)
buf := make([]byte, 100)
n, err := r.Read(buf)  // читає 100 байт = 800 біт
```

---

## ⚠️ Критичні проблеми у вихідному коді

### ❌ 1. Небезпечне очищення біт через XOR

```go
// У ReadBits64:
self.bits ^= bits << uint(self.n-n)  // ⚠️ Може зламатися при "сміттєвих" бітах

// У WriteBits64:
bits ^= (mask << move)  // ⚠️ Та сама проблема
```

**Ризик**: Якщо у `self.bits` були неініціалізовані біти праворуч, XOR змінить їх непередбачувано.

**✅ Безпечна альтернатива для Reader**:
```go
// Просто зсуваємо вліво — прочитані біти "виштовхуються"
self.bits <<= uint(n)
self.n -= n
// Не потрібно очищати — наступне читання використає тільки молодші self.n біт
```

**✅ Безпечна альтернатива для Writer**:
```go
// Замість XOR використовуємо маску:
remaining := bits & ((1 << uint(n)) - 1)  // залишаємо тільки молодші n біт
self.bits = (self.bits << uint(n)) | remaining
```

---

### ❌ 2. Відсутність обробки io.EOF у ReadBits64

```go
if got, err = self.R.Read(b[:want]); err != nil {
    return  // ⚠️ Повертає (0, err), але не перевіряє io.EOF спеціально
}
if got < want {
    err = io.EOF  // ⚠️ Перезаписує оригінальну помилку!
    return
}
```

**Проблема**: Якщо `R.Read` повертає `io.ErrUnexpectedEOF`, код перезапише його на `io.EOF` → втрата інформації.

**✅ Виправлення**:
```go
if got, err = self.R.Read(b[:want]); err != nil {
    if err == io.EOF && got > 0 {
        // Часткове читання перед кінцем — дозволити
    } else {
        return 0, err
    }
}
if got < want {
    // Часткове читання — повертаємо те, що є, і io.EOF
    // ... обробка часткових даних ...
    return 0, io.EOF
}
```

---

### ❌ 3. Необроблене переповнення у WriteBits64

```go
if self.n+n > 64 {
    // ... флеш ...
    n -= int(move)  // ⚠️ n може стати від'ємним якщо n > 64!
    bits ^= (mask << move)
}
```

**Ризик**: Виклик `WriteBits64(v, 100)` призведе до некоректної поведінки.

**✅ Виправлення**:
```go
func (self *Writer) WriteBits64(bits uint64, n int) error {
    if n < 0 || n > 64 {
        return fmt.Errorf("invalid bit count: %d (must be 0..64)", n)
    }
    // ... решта логіки ...
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### Сценарій 1: Парсинг AAC AudioSpecificConfig

```go
// ParseAudioSpecificConfig — декодування AAC конфігурації (бітове поле)
func ParseAudioSpecificConfig(r *bits.Reader) (*AACConfig, error) {
    // audioObjectType: 5 біт
    audioObjectType, err := r.ReadBits(5)
    if err != nil { return nil, err }
    if audioObjectType == 31 {  // escape value
        ext, err := r.ReadBits(6)
        if err != nil { return nil, err }
        audioObjectType += ext
    }
    
    // samplingFrequencyIndex: 4 біти
    samplingFrequencyIndex, err := r.ReadBits(4)
    if err != nil { return nil, err }
    
    var samplingFrequency uint32
    if samplingFrequencyIndex == 0xF {
        // explicit frequency: 24 біти
        samplingFrequency, err = r.ReadBits(24)
        if err != nil { return nil, err }
    } else {
        // таблиця частот
        samplingFrequency =aacSampleRates[samplingFrequencyIndex]
    }
    
    // channelConfiguration: 4 біти
    channelConfig, err := r.ReadBits(4)
    if err != nil { return nil, err }
    
    return &AACConfig{
        ObjectType:         uint8(audioObjectType),
        SamplingFrequency:  samplingFrequency,
        ChannelConfig:      uint8(channelConfig),
    }, nil
}
```

### Сценарій 2: Запис H.264 SEI повідомлення

```go
// WriteSEIMessage — запис Supplemental Enhancement Information
func WriteSEIMessage(w *bits.Writer, payloadType uint, payload []byte) error {
    // payloadType: exp-Golomb кодоване
    if err := writeExpGolomb(w, int(payloadType)); err != nil {
        return err
    }
    
    // payloadSize: exp-Golomb кодоване
    if err := writeExpGolomb(w, len(payload)); err != nil {
        return err
    }
    
    // payload: байт за байтом
    for _, b := range payload {
        if err := w.WriteBits(uint(b), 8); err != nil {
            return err
        }
    }
    
    // bit_equal_to_one: 1 біт (обов'язковий)
    if err := w.WriteBits(1, 1); err != nil {
        return err
    }
    
    // bit_equal_to_zero: вирівнювання до байта
    remaining := w.BitsPending() % 8
    if remaining > 0 {
        if err := w.WriteBits(0, 8-remaining); err != nil {
            return err
        }
    }
    
    return w.FlushBits()
}

// Допоміжна функція для перевірки залишку біт
func (w *Writer) BitsPending() int {
    return w.n
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Некоректне очищення біт (XOR)** | "Сміттєві" біти змінюють результат | Замінити XOR на зсув вліво або маскування |
| **Переповнення буфера у Writer** | `WriteBits64(v, 100)` ламає стан | Додати валідацію: `if n > 64 { return error }` |
| **Втрата часткових даних при EOF** | Останні біти втрачаються | Обробляти `got < want` як часткове читання, не одразу `io.EOF` |
| **Невирівняний флеш у Writer** | Останній байт містить зайві нулі | Документувати, що `FlushBits` доповнює нулями; додати `FlushBitsWithPadding(bit)` |
| **Відсутність конструкторів** | Користувачі ініціалізують вручну → помилки | Додати `NewReader`/`NewWriter` функції |

---

## ⚡ Оптимізації для high-performance бітового I/O

### 1. Використання `io.ByteReader`/`io.ByteWriter`:

```go
// Оптимізований Reader для побайтового читання
type FastReader struct {
    *Reader
    byteReader io.ByteReader
}

func (fr *FastReader) refill() error {
    if br, ok := fr.R.(io.ByteReader); ok {
        b, err := br.ReadByte()
        if err != nil { return err }
        fr.bits = (fr.bits << 8) | uint64(b)
        fr.n += 8
        return nil
    }
    // Fallback на загальний Read
    return fr.Reader.refill()
}
```

### 2. Пакетне читання/запис для зменшення викликів:

```go
// ReadBitsVec — читання кількох полів за один виклик
func (r *Reader) ReadBitsVec(sizes []int) ([]uint64, error) {
    results := make([]uint64, len(sizes))
    for i, n := range sizes {
        v, err := r.ReadBits64(n)
        if err != nil { return nil, err }
        results[i] = v
    }
    return results, nil
}

// Використання для парсингу заголовків:
fields, err := r.ReadBitsVec([]int{1, 2, 5, 8})  // forbidden, ref_idc, type, profile
```

### 3. Кешування буфера для частого флешу:

```go
// BufferedWriter — Writer з більшим внутрішнім буфером
type BufferedWriter struct {
    *Writer
    flushThreshold int  // флешити коли n >= threshold
}

func (bw *BufferedWriter) WriteBits64(bits uint64, n int) error {
    if err := bw.Writer.WriteBits64(bits, n); err != nil {
        return err
    }
    // Авто-флеш якщо буфер майже повний
    if bw.n >= bw.flushThreshold {
        return bw.FlushBits()
    }
    return nil
}
```

---

## 📋 Чек-лист безпечного використання

```go
// ✅ 1. Завжди перевіряти повернені помилки
bits, err := r.ReadBits64(12)
if err != nil {
    return fmt.Errorf("read bits: %w", err)
}

// ✅ 2. Валідувати кількість біт (0..64)
if n < 0 || n > 64 {
    return fmt.Errorf("invalid bit count: %d", n)
}

// ✅ 3. Флешити Writer після завершення запису
defer w.FlushBits()  // гарантований запис залишку

// ✅ 4. Пам'ятати про MSB-first порядок
// Перший прочитаний біт = старший у поверненому значенні

// ✅ 5. Обробляти часткове читання при EOF
n, err := r.Read(buf)
if err == io.EOF && n > 0 {
    // Обробити прочитані дані перед виходом
}

// ✅ 6. Використовувати конструктори (якщо додані)
r := bits.NewReader(reader)
w := bits.NewWriter(writer)

// ✅ 7. Метрики для моніторингу
metrics.BitsRead.Add(float64(totalBits))
metrics.ReadCalls.Inc()
```

---

## 🔗 Корисні посилання

- 💻 [Go io.Reader/Writer Interfaces](https://pkg.go.dev/io)
- 📄 [H.264 Bitstream Syntax](https://www.itu.int/rec/T-REC-H.264) — приклад бітового парсингу
- 📄 [AAC AudioSpecificConfig](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — змінна довжина полів
- 🧪 [Exp-Golomb Coding](https://en.wikipedia.org/wiki/Exponential-Golomb_coding) — алгоритм кодування змінної довжини

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Замініть XOR на зсув/маскування** — уникнення непередбачуваної поведінки при "сміттєвих" бітах.
> 2. **Додайте валідацію `0 <= n <= 64`** — запобігання переповненню буфера.
> 3. **Обробляйте часткове читання при EOF** — не втрачайте останні біти потоку.
> 4. **Документуйте MSB-first порядок** — це критично для сумісності з кодеками.
> 5. **Додайте `NewReader`/`NewWriter` конструктори** — зменшення помилок ініціалізації.

Потрібен приклад інтеграції `bits.Reader` з вашим `h264parser` для парсингу SPS/PPS заголовків з бітовою точністю? Готовий допомогти! 🚀