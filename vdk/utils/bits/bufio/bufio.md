# 📦 Глибокий розбір: `bufio.Reader` — Кастомний буферизований reader з ReadSeeker

Цей файл — **незавершена реалізація буферизованого reader**, що підтримує інтерфейс `io.ReadSeeker`. Він має незвичайну структуру з подвійним буфером, але більшість методів є заглушками.

---

## ⚠️ Критичні проблеми у вихідному коді

### ❌ 1. `ReadAt()` повертає порожні значення

```go
func (self *Reader) ReadAt(b []byte, off int64) (n int, err error) {
    return  // ← нічого не робить!
}
```

**Наслідки**:
- Завжди повертає `n=0, err=nil`
- Клієнтський код може зависнути в нескінченному циклі очікування даних
- Порушує контракт `io.ReaderAt`

**✅ Виправлення**:
```go
func (self *Reader) ReadAt(b []byte, off int64) (n int, err error) {
    // 1. Переміщення позиції у базовому читачі
    _, err = self.R.Seek(off, io.SeekStart)
    if err != nil {
        return 0, err
    }
    
    // 2. Читання даних
    return self.R.Read(b)
}
```

---

### ❌ 2. Подвійний буфер не використовується

```go
type Reader struct {
    buf [][]byte  // масив з ДВОХ слайсів
    R   io.ReadSeeker
}

func NewReaderSize(r io.ReadSeeker, size int) *Reader {
    buf := make([]byte, size*2)  // один великий масив
    return &Reader{
        R: r,
        buf: [][]byte{buf[0:size], buf[size:]},  // два "вікна" у тому ж масиві
    }
}
```

**Проблеми**:
- `buf` ініціалізується, але ніде не використовується
- Немає методів `Read()`, `Peek()`, `Discard()` — основних для буферизації
- Немає логіки заповнення/спорожнення буфера

**Призначення подвійного буфера (ймовірне)**:
```
Два буфери для ping-pong патерну:
• Поки один заповнюється з джерела, інший читається клієнтом
• Зменшує блокування при асинхронному читанні
• Але без реалізації — марна витрата пам'яті
```

---

### ❌ 3. Відсутні критичні методи `io.Reader`

```go
// ❌ НЕ реалізовано:
func (self *Reader) Read(p []byte) (n int, err error)  // основний метод!
func (self *Reader) Peek(n int) ([]byte, error)        // перегляд без споживання
func (self *Reader) Discard(n int) (int64, error)      // пропуск даних
func (self *Reader) Buffered() int                     // кількість доступних байт
```

**Наслідки**: цей тип не може використовуватися як `io.Reader`.

---

## 🗺️ Архітектурна схема (поточний стан)

```
┌────────────────────────────────────────┐
│ 📦 bufio.Reader — Incomplete Buffer   │
├────────────────────────────────────────┤
│                                         │
│  🔹 Реалізовано:                        │
│  • NewReaderSize() — ініціалізація     │
│  • Подвійний буфер (2×size)            │
│  • ReadAt() заглушка                   │
│                                         │
│  🔹 НЕ реалізовано:                     │
│  • Read() — основний метод читання     │
│  • Peek/Discard/Buffered               │
│  • Логіка заповнення буфера            │
│  • Seek() делегування                  │
│                                         │
│  🔄 Очікуваний потік:                   │
│  io.ReadSeeker → буфер → Read() → клієнт│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔧 Як завершити реалізацію: покроковий план

### Крок 1: Додати стан для управління буфером

```go
type Reader struct {
    buf     [][]byte  // подвійний буфер: [readBuf, writeBuf]
    R       io.ReadSeeker
    
    // Стан читання:
    readIdx int       // який буфер зараз читається (0 або 1)
    readPos int       // позиція у readBuf
    readLen int       // кількість валідних байт у readBuf
    
    // Стан запису:
    writePos int      // позиція запису у writeBuf
    
    // Загальні:
    size    int       // розмір одного буфера
    err     error     // остання помилка
}
```

---

### Крок 2: Реалізувати `Read()` з буферизацією

```go
func (self *Reader) Read(p []byte) (n int, err error) {
    // 1. Перевірка помилок
    if self.err != nil {
        return 0, self.err
    }
    
    // 2. Якщо є дані у буфері — віддати їх
    if self.readPos < self.readLen {
        available := self.readLen - self.readPos
        toCopy := min(len(p), available)
        
        copy(p, self.buf[self.readIdx][self.readPos:self.readPos+toCopy])
        self.readPos += toCopy
        return toCopy, nil
    }
    
    // 3. Буфер порожній — заповнити його
    if err := self.fillBuffer(); err != nil {
        self.err = err
        return 0, err
    }
    
    // 4. Рекурсивний виклик (тепер буфер має дані)
    return self.Read(p)
}

// fillBuffer — заповнення буфера з джерела
func (self *Reader) fillBuffer() error {
    // Перемикання буферів: read → write, write → read
    self.readIdx = 1 - self.readIdx  // 0↔1
    writeIdx := 1 - self.readIdx
    
    // Скидання позицій
    self.readPos = 0
    self.writePos = 0
    
    // Читання у writeBuf
    n, err := self.R.Read(self.buf[writeIdx])
    self.readLen = n
    
    if n == 0 && err == nil {
        return io.EOF  // кінець потоку
    }
    
    // Повертаємо помилку тільки якщо не прочитано жодного байта
    if err != nil && err != io.EOF {
        return err
    }
    
    return nil
}

func min(a, b int) int {
    if a < b { return a }
    return b
}
```

---

### Крок 3: Реалізувати `ReadAt()` правильно

```go
func (self *Reader) ReadAt(b []byte, off int64) (n int, err error) {
    // ReadAt не повинен змінювати позицію читача
    // Тому читаємо напряму з базового ReadSeeker
    
    // 1. Збереження поточної позиції
    origPos, err := self.R.Seek(0, io.SeekCurrent)
    if err != nil {
        return 0, err
    }
    
    // 2. Переміщення до потрібної позиції
    _, err = self.R.Seek(off, io.SeekStart)
    if err != nil {
        return 0, err
    }
    
    // 3. Читання даних
    n, err = self.R.Read(b)
    
    // 4. Відновлення оригінальної позиції
    _, seekErr := self.R.Seek(origPos, io.SeekStart)
    if seekErr != nil && err == nil {
        err = seekErr
    }
    
    return n, err
}
```

---

### Крок 4: Додати `Seek()` делегування

```go
func (self *Reader) Seek(offset int64, whence int) (int64, error) {
    // При зміні позиції — скидання буфера
    self.readLen = 0
    self.readPos = 0
    self.writePos = 0
    self.err = nil
    
    // Делегування базовому ReadSeeker
    return self.R.Seek(offset, whence)
}
```

---

### Крок 5: Додати корисні методи

```go
// Peek — перегляд наступних n байт без споживання
func (self *Reader) Peek(n int) ([]byte, error) {
    // Якщо даних у буфері достатньо
    if self.readPos+n <= self.readLen {
        return self.buf[self.readIdx][self.readPos : self.readPos+n], nil
    }
    
    // Інакше — помилка (або реалізувати розширення буфера)
    return nil, io.ErrShortBuffer
}

// Discard — пропуск n байт
func (self *Reader) Discard(n int) (int64, error) {
    discarded := 0
    
    for n > 0 {
        if self.readPos < self.readLen {
            // Пропуск у буфері
            toSkip := min(n, self.readLen-self.readPos)
            self.readPos += toSkip
            discarded += toSkip
            n -= toSkip
        } else {
            // Буфер порожній — заповнити або пропустити у джерелі
            if err := self.fillBuffer(); err != nil {
                return int64(discarded), err
            }
        }
    }
    
    return int64(discarded), nil
}

// Buffered — кількість байт, доступних для читання без блокування
func (self *Reader) Buffered() int {
    return self.readLen - self.readPos
}

// Reset — перевикористання reader з новим джерелом
func (self *Reader) Reset(r io.ReadSeeker) {
    self.R = r
    self.readIdx = 0
    self.readPos = 0
    self.readLen = 0
    self.writePos = 0
    self.err = nil
}
```

---

## ✅ Ваш use-case: читання файлу з буферизацією

```go
// ReadFileWithBuffer — приклад використання
func ReadFileWithBuffer(filename string, bufferSize int) error {
    // Відкриття файлу
    f, err := os.Open(filename)
    if err != nil { return err }
    defer f.Close()
    
    // Створення буферизованого reader
    reader := bufio.NewReaderSize(f, bufferSize)
    
    // Читання порціями
    buf := make([]byte, 1024)
    for {
        n, err := reader.Read(buf)
        if n > 0 {
            // Обробка даних
            process(buf[:n])
        }
        if err == io.EOF {
            break
        }
        if err != nil {
            return err
        }
    }
    
    return nil
}

// ReadAtExample — читання з довільної позиції
func ReadAtExample(filename string, offset int64, length int) ([]byte, error) {
    f, err := os.Open(filename)
    if err != nil { return nil, err }
    defer f.Close()
    
    reader := bufio.NewReaderSize(f, 4096)
    
    buf := make([]byte, length)
    n, err := reader.ReadAt(buf, offset)
    if err != nil {
        return nil, err
    }
    
    return buf[:n], nil
}
```

---

## 🔄 Ping-pong буферизація (просунутий use-case)

Якщо ви хочете використати подвійний буфер для асинхронного читання:

```go
// AsyncReader — reader з фоновим заповненням буфера
type AsyncReader struct {
    *Reader  // вбудовуємо базовий Reader
    done     chan struct{}
    fillCh   chan int  // сигнал про завершення заповнення
}

func NewAsyncReader(r io.ReadSeeker, size int) *AsyncReader {
    ar := &AsyncReader{
        Reader: NewReaderSize(r, size),
        done:   make(chan struct{}),
        fillCh: make(chan int, 1),
    }
    
    // Запуск фонового заповнювача
    go ar.fillLoop()
    
    return ar
}

func (ar *AsyncReader) fillLoop() {
    for {
        select {
        case <-ar.done:
            return
        default:
            // Якщо буфер майже порожній — заповнити
            if ar.Buffered() < ar.size/4 {
                ar.fillBuffer()  // неблокуюче заповнення
            }
            time.Sleep(10 * time.Millisecond)  // уникнення активного очікування
        }
    }
}

func (ar *AsyncReader) Close() error {
    close(ar.done)
    return ar.Reader.R.(io.Closer).Close()
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **ReadAt() повертає 0 байт** | Клієнт зависає у циклі читання | Реалізуйте читання з базового `ReadSeeker` |
| **Буфер не заповнюється** | `Read()` завжди повертає `EOF` | Додайте логіку `fillBuffer()` |
| **Позиція збивається після ReadAt** | Наступні `Read()` читають не ті дані | Зберігайте/відновлюйте позицію у `ReadAt()` |
| **Race condition у подвійному буфері** | Дані пошкоджуються при асинхронному доступі | Використовуйте `sync.Mutex` або канал для синхронізації |
| **Витік пам'яті** | Буфери не очищаються | Реалізуйте `Reset()` або `Close()` |

---

## ⚡ Оптимізації для великих файлів

### 1. Динамічний розмір буфера:

```go
// AdaptiveReader — reader, що змінює розмір буфера
type AdaptiveReader struct {
    *Reader
    minSize, maxSize int
    currentSize int
}

func (ar *AdaptiveReader) adjustBufferSize(bytesRead int, duration time.Duration) {
    throughput := float64(bytesRead) / duration.Seconds()
    
    if throughput > 10*1024*1024 {  // >10 MB/s
        // Збільшуємо буфер для високої пропускної здатності
        ar.currentSize = min(ar.currentSize*2, ar.maxSize)
    } else if throughput < 100*1024 {  // <100 KB/s
        // Зменшуємо буфер для економії пам'яті
        ar.currentSize = max(ar.currentSize/2, ar.minSize)
    }
    
    ar.Reset(ar.R)  // перевикористання з новим розміром
}
```

### 2. Прямий доступ до буфера (zero-copy):

```go
// ReadSlice — повертає слайс прямо з буфера (без копіювання)
func (self *Reader) ReadSlice(n int) ([]byte, error) {
    if self.readPos+n > self.readLen {
        return nil, io.ErrShortBuffer
    }
    
    result := self.buf[self.readIdx][self.readPos : self.readPos+n]
    self.readPos += n
    return result, nil
}

// ⚠️ Увага: повернутий слайс стає недійсним після наступного Read()!
```

### 3. Моніторинг продуктивності:

```go
type BufferMetrics struct {
    ReadsTotal prometheus.Counter
    BytesRead  prometheus.Counter
    BufferHits prometheus.Counter  // читання з буфера без I/O
    BufferMisses prometheus.Counter  // читання з джерела
}

func (self *Reader) Read(p []byte) (n int, err error) {
    start := time.Now()
    
    if self.readPos < self.readLen {
        self.metrics.BufferHits.Inc()
    } else {
        self.metrics.BufferMisses.Inc()
    }
    
    n, err = self.readImpl(p)  // реальна реалізація
    
    self.metrics.ReadsTotal.Inc()
    self.metrics.BytesRead.Add(float64(n))
    self.metrics.ReadLatency.Observe(time.Since(start).Seconds())
    
    return n, err
}
```

---

## 📋 Чек-лист завершення реалізацї

```go
// ✅ 1. Виправити ReadAt() — читати з базового ReadSeeker
func (self *Reader) ReadAt(b []byte, off int64) (int, error) {
    // Зберегти позицію → Seek → Read → відновити позицію
}

// ✅ 2. Реалізувати Read() з буферизацією
func (self *Reader) Read(p []byte) (int, error) {
    // Віддати дані з буфера → якщо порожньо, заповнити → рекурсія
}

// ✅ 3. Додати fillBuffer() для заповнення буфера
// ✅ 4. Реалізувати Seek() з скиданням буфера
// ✅ 5. Додати Peek/Discard/Buffered для сумісності з stdlib
// ✅ 6. Обробити edge cases: EOF, помилки, порожній буфер
// ✅ 7. Протестувати з різними розмірами буфера
// ✅ 8. Додати метрики для моніторингу ефективності
```

---

## 🔗 Корисні посилання

- 💻 [Go io.ReaderAt Interface](https://pkg.go.dev/io#ReaderAt)
- 💻 [Go io.ReadSeeker Interface](https://pkg.go.dev/io#ReadSeeker)
- 📄 [Double Buffering Pattern](https://en.wikipedia.org/wiki/Multiple_buffering)
- 🧪 [Go sync.Pool for Buffer Reuse](https://pkg.go.dev/sync#Pool)

---

> 💡 **Ключові рекомендації**:
> 1. **Почніть з реалізації `Read()`** — це основний метод, без якого тип некорисний.
> 2. **Виправте `ReadAt()`** — він не повинен змінювати позицію читача.
> 3. **Додайте `Seek()` делегування** — для підтримки `io.ReadSeeker`.
> 4. **Тестуйте з великими файлами** — перевірте, що буферизація дійсно покращує продуктивність.
> 5. **Документуйте обмеження** — наприклад, що `ReadSlice()` повертає нестабільні слайси.

Потрібен приклад інтеграції цього `bufio.Reader` з вашим `ts.Demuxer` для ефективного читання TS файлів? Готовий допомогти! 🚀