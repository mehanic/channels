# 🔍 Глибокий розбір коду: `PESParser` — парсер Packetized Elementary Stream для MPEG-2 TS

Цей код реалізує **складний state machine парсер** для PES (Packetized Elementary Stream) пакетів у стандартах MPEG-2 TS (ITU-T H.222.0). Розберемо архітектурно, з критичним аналізом та практичними рекомендаціями.

---

## 📦 Архітектура модуля: огляд компонентів

```
┌─────────────────────────────────────┐
│ Модуль: PESParser                   │
│ Призначення: парсинг PES пакетів    │
│ Стандарт: ITU-T H.222.0 (06/2021)   │
├─────────────────────────────────────┤
│ 🔹 Стани парсингу:                   │
│    • StateFindPrefix (0x000001)     │
│    • StateParseOptPESHeader         │
│    • StateReadPacket / ReadBytes    │
│    • StateReadPaddingBytes          │
│                                      │
│ 🔹 Основні типи:                     │
│    • PES — розпаршений PES пакет    │
│    • PESParser — стан парсера       │
│    • PESByte — байт з метаданими    │
│                                      │
│ 🔹 Ключові методи:                   │
│    • StartPESReadLoop() — асинхронний парсинг│
│    • EnqueueTSPacket() — інтеграція з TS│
│    • WriteBytes() — низькорівневий ввід│
└─────────────────────────────────────┘
```

### 🎯 Контекст: структура PES пакету
```
📦 PES packet (змінна довжина):
├─ Packet start code prefix: 0x000001 (3 байти) ← знаходить findPrefix()
├─ stream_id: 8 біт (тип потоку: відео/аудіо/тощо)
├─ PES_packet_length: 16 біт (довжина після цього поля)
│
├─ [Опціональний PES header] ← парситься в StateParseOptPESHeader
│  ├─ '10' marker bits (2 біти, завжди 0x02)
│  ├─ PES_scrambling_control (2 біти)
│  ├─ PES_priority, Data_alignment, Copyright, Original/original (по 1 біту)
│  ├─ PTS_DTS_flags (2 біти): 00=ні, 10=PTS, 11=PTS+DTS
│  ├─ ESCR_flag, ES_rate_flag, DSM_trick_mode_flag, тощо (по 1 біту)
│  ├─ PES_header_data_length: 8 біт (довжина цього заголовка)
│  ├─ [PTS] 40 біт (якщо PTS_DTS_flags & 0x2) ← парситься як 33-біт @90kHz
│  ├─ [DTS] 40 біт (якщо PTS_DTS_flags == 0x3)
│  ├─ [ESCR, ES_rate, тощо] ← ⚠️ Не реалізовано повністю!
│  └─ [PES extension] ← ⚠️ Не реалізовано!
│
└─ PES_packet_data_byte[] ← корисне навантаження (відео/аудіо дані)
   • Для відео/аудіо: H.264 NAL units, AAC ADTS, тощо
   • Для padding stream: байти заповнення
```

---

## 🔬 Детальний розбір ключових компонентів

### 1️⃣ Константи та типи даних

```go
// 🎯 StreamID константи (ITU-T H.222.0 Table 2-18)
const (
    StreamID_ProgramStreamMap       = 0xbc  // Program stream map
    StreamID_PrivateStream1         = 0xbd  // Private stream 1 (субтитри, меню)
    StreamID_PaddingStream          = 0xbe  // Padding stream (заповнення)
    StreamID_PrivateStream2         = 0xbf  // Private stream 2 (metadata)
    // ... ще 15+ типів ...
    StreamID_ExtendedStreamID       = 0xfd  // Розширений stream_id
)

// 🎯 PES структура — складна, з багатьма прапорцями
type PES struct {
    Prefix       uint32  // Завжди 0x000001
    StreamID     byte    // Тип потоку
    PacketLength uint16  // Довжина пакета після цього поля
    
    // 🎯 Опціональний заголовок (прапорці)
    ScramblingControl      byte   // 2 біти
    Priority               bool   // 1 біт
    DataAlignment          bool   // 1 біт
    Copyright              bool   // 1 біт
    Original               bool   // 1 біт
    PTSFlag                bool   // 1 біт ← критично для A/V синхронізації!
    DTSFlag                bool   // 1 біт
    // ... ще 7+ прапорців ...
    
    // 🎯 Таймштампи (90kHz clock)
    rawPTS  uint32  // Сирий 33-біт значення
    PTS     float64 // Конвертовано у секунди: rawPTS / 90000
    rawDTS  uint32
    DTS     float64
    
    // 🎯 Корисне навантаження
    ElementaryStream   []byte  // Для відео/аудіо потоків
    PacketDataStream   []byte  // Для потоків без опціонального заголовка
    Padding            []byte  // Для padding stream
}
```

#### ⚠️ Проблеми структури `PES`
```go
// ❌ Змішані типи для таймштампів:
rawPTS uint32   // 33-біт значення, але зберігається у 32-бітному uint32!
// 📋 Специфікація: PTS/DTS — 33 біти (не 32!)
// • Код парсить правильно (5 частин по 7-8 біт), але зберігає у uint32 → втрата старшого біта!
// ✅ Правильно: використовувати uint64 для 33-бітних значень:
type PES struct {
    rawPTS uint64  // ✅ 64 біти для 33-бітного значення
    PTS    float64 // Конвертоване у секунди
    // ...
}

// ❌ Float64 для таймштампів втрачає точність:
PTS float64  // float64 має ~15 десяткових знаків точності
// • Для 90kHz clock: 1 tick = 11.111... мкс
// • Після конвертації у секунди: можливі помилки округлення
// ✅ Краще: зберігати у нативному форматі (33-біт) + метод конвертації:
func (p *PES) GetPTSDuration() time.Duration {
    return time.Duration(p.rawPTS) * time.Second / 90000
}

// ❌ Відсутність валідації прапорців:
// • Що якщо PTSFlag=true, але в даних немає PTS?
// • Що якщо StreamID=0xbd (private), але парситься як відео?
// ✅ Додати метод валідації:
func (p *PES) Validate() error {
    if p.Prefix != 0x000001 {
        return fmt.Errorf("invalid prefix: 0x%X", p.Prefix)
    }
    if p.PTSFlag && p.rawPTS == 0 {
        return fmt.Errorf("PTS flag set but no PTS data")
    }
    // ... інші перевірки ...
    return nil
}
```

---

### 2️⃣ `PESParser` — state machine з конкурентністю

```go
type PESParser struct {
    packetCount      int              // Лічильник пакетів (не використовується?)
    buffer           []PESByte        // 🎯 Внутрішній буфер для парсингу
    bufferSize       int              // Максимальний розмір буфера
    byteIncomingChan chan []PESByte   // 🎯 Канал для вхідних байтів
    mutex            *sync.Mutex      // 🎯 Захист буфера
    isClosed         bool             // Прапорець закриття
    statusMutex      *sync.Mutex      // 🎯 Захист статусу
    PES                           // 🎯 Вбудована структура для поточного пакету
}
```

#### ⚠️ Проблеми дизайну
```go
// ❌ Два mutex для різних цілей — складність та ризик deadlock:
mutex       *sync.Mutex  // Для буфера
statusMutex *sync.Mutex  // Для isClosed
// • Якщо потрібно змінити і буфер, і статус → потрібні обидва м'ютекси → ризик deadlock
// ✅ Правильно: один mutex для всього стану, або використовувати atomic для простих прапорців:
type PESParser struct {
    mu sync.Mutex  // ✅ Один м'ютекс для всього стану
    // ...
    isClosed atomic.Bool  // ✅ Atomic для простого прапорця
}

// ❌ Вбудована структура `PES` → конфлікт імен та незрозумілий доступ:
pp.PES.ElementaryStream  // Дивно: чому не pp.ElementaryStream?
// ✅ Правильно: або не вбудовувати, або використовувати явний іменник:
type PESParser struct {
    currentPES PES  // ✅ Зрозуміле ім'я
    // ...
}
// Доступ: pp.currentPES.ElementaryStream

// ❌ Buffer як []PESByte — зайва обгортка навколо byte:
type PESByte struct {
    Datum         byte  // ✅ Саме дане
    StartOfPacket bool  // ✅ Метадані
    EndOfStream   bool  // ✅ Метадані
}
// • Кожен байт = 1 byte + 2 bool → ~3-4 байти замість 1
// • Для 1MB потоку: +2-3MB overhead!
// ✅ Правильно: окремі буфери для даних та метаданих:
type PESParser struct {
    dataBuffer    []byte        // ✅ Тільки дані
    metaBuffer    []PacketMeta  // ✅ Тільки метадані (синхронізовані індекси)
}
```

---

### 3️⃣ `findPrefix()` — пошук префіксу 0x000001

```go
func (pp *PESParser) findPrefix() bool {
    if pp.getBufferLength() < 6 {  // ⚠️ Чому 6, а не 3?
        return false
    }
    
    // 🎯 Лінійний пошук префіксу 0x00 0x00 0x01
    prefixIndex := -1
    for i := 0; i < pp.getBufferLength()-3; i++ {
        if pp.buffer[i].Datum == 0 && pp.buffer[i+1].Datum == 0 && pp.buffer[i+2].Datum == 1 {
            prefixIndex = i
            pp.dequeue(prefixIndex)  // 🎯 Видалити байти ДО префіксу
            break
        }
    }
    
    if prefixIndex == -1 {
        // ❌ Критична проблема: видалення ВСЬОГО буфера!
        pp.dequeue(pp.getBufferLength())  // ⚠️ Втрата даних!
        return false
    }
    
    // 🎯 Парсинг заголовка після префіксу
    pp.PES.Prefix = uint32(pp.buffer[0].Datum)<<16 | uint32(pp.buffer[1].Datum)<<8 | uint32(pp.buffer[2].Datum)
    pp.PES.StreamID = pp.buffer[3].Datum
    pp.PES.PacketLength = uint16(pp.buffer[4].Datum)<<8 | uint16(pp.buffer[5].Datum)
    
    pp.dequeue(6)  // 🎯 Видалити префікс + заголовок
    return true
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Агресивне видалення даних при відсутності префіксу:
pp.dequeue(pp.getBufferLength())  // ❌ Видаляє ВСЕ, навіть якщо префікс може бути далі!
// 📋 Сценарій: потік з помилкою → префікс зміщений → втрата всіх даних до кінця буфера!
// ✅ Правильно: видаляти тільки до певного ліміту (напр. 1024 байти) або використовувати sliding window:
const MaxPrefixSearchWindow = 1024  // Розумний ліміт пошуку
if prefixIndex == -1 {
    // 🎯 Видалити тільки частину буфера, залишивши можливість знайти префікс далі
    toRemove := min(pp.getBufferLength(), MaxPrefixSearchWindow)
    pp.dequeue(toRemove)
    return false
}

// ❌ Неправильна перевірка мінімального розміру:
if pp.getBufferLength() < 6 { ... }  // ❌ Чому 6? Потрібно мінімум 3 для префіксу + 3 для заголовка
// ✅ Правильно: документувати або використовувати константу:
const MinPrefixHeaderSize = 6  // 3 байти префіксу + 3 байти заголовка (stream_id + length)
if pp.getBufferLength() < MinPrefixHeaderSize {
    return false
}

// ❌ Лінійний пошук O(n) для великих буферів:
for i := 0; i < pp.getBufferLength()-3; i++ { ... }  // ❌ Повільно для 100KB+ буферів
// ✅ Оптимізація: використовувати алгоритм пошуку підрядка (напр. Boyer-Moore) або hardware acceleration
// Або: попередньо фільтрувати байти 0x00 перед пошуком 0x000001
```

---

### 4️⃣ `parseOptionalPESHeaders()` — парсинг опціонального заголовка

```go
func (pp *PESParser) parseOptionalPESHeaders() {
    // 🎯 Парсинг прапорців з перших 3 байт опціонального заголовка
    pp.PES.ScramblingControl = (pp.buffer[0].Datum >> 4) & 0x03
    pp.PES.Priority = (pp.buffer[0].Datum>>3)&0x01 == 1
    // ... ще 10+ прапорців ...
    
    pp.PES.HeaderDataLength = pp.buffer[2].Datum
    
    // 🎯 Парсинг PTS (якщо встановлено прапорець)
    trimIndex := 2  // Початковий зсув після прапорців
    if pp.PTSFlag {
        // 🎯 Складний бітовий парсинг 33-бітного PTS з 5 частин
        pp.PES.rawPTS = uint32((pp.buffer[3].Datum>>1)&0x07)<<30 | 
                       uint32(pp.buffer[4].Datum)<<22 | 
                       uint32(pp.buffer[5].Datum>>1)<<15 | 
                       uint32(pp.buffer[6].Datum)<<7 | 
                       uint32(pp.buffer[7].Datum>>1)
        pp.PES.PTS = float64(pp.PES.rawPTS) / 90000  // ✅ Конвертація у секунди
        trimIndex += 5  // PTS займає 5 байт
    }
    // 🎯 Аналогічно для DTS...
    
    pp.dequeue(trimIndex + 1)  // 🎯 Видалення оброблених байтів
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Втрата старшого біта у 33-бітному PTS через uint32:
pp.PES.rawPTS = uint32(...)  // ❌ uint32 має тільки 32 біти, а PTS — 33!
// 📋 Специфікація: PTS = 33 біти, формат:
// [3 біти][1 біт][15 біт][1 біт][15 біт] = 33 біти
// • Код правильно парсить біти, але зберігає у uint32 → старший біт (бит 32) втрачається!
// ✅ Правильно: використовувати uint64 для 33-бітних значень:
pp.PES.rawPTS = uint64((pp.buffer[3].Datum>>1)&0x07)<<30 | 
               uint64(pp.buffer[4].Datum)<<22 | 
               uint64(pp.buffer[5].Datum>>1)<<15 | 
               uint64(pp.buffer[6].Datum)<<7 | 
               uint64(pp.buffer[7].Datum>>1)

// ❌ Відсутність перевірки marker bits (завжди мають бути 0x02):
// 📋 Специфікація: після stream_id має бути '10' (2 біти) перед прапорцями
// • Код перевіряє це в StateParseOptPESHeader, але не в parseOptionalPESHeaders
// ✅ Правильно: додати перевірку на початку функції:
if (pp.buffer[0].Datum>>6)&0x03 != 0x02 {
    return fmt.Errorf("invalid marker bits in PES header: expected 0x02")
}

// ❌ Неповний парсинг опціонального заголовка:
// • ESCRFlag, ESRateFlag, тощо — тільки оголошені, але не реалізовані
// ✅ Правильно: або реалізувати, або явно ігнорувати з логуванням:
if pp.ESCRFlag {
    logger.Debug("ESCR flag not implemented, skipping", "stream_id", pp.PES.StreamID)
    // Пропустити 6 байт ESCR
    trimIndex += 6
}
```

---

### 5️⃣ `StartPESReadLoop()` — головний цикл парсингу

Це **найскладніша функція** модуля. Розберемо state machine.

#### 🎯 State machine діаграма
```
[StateFindPrefix] 
   │
   ├─(знайдено 0x000001)→ [StateParseOptPESHeader]
   │                        │
   │                        ├─(StreamID ∈ {відео/аудіо})→ [StateReadPacket]
   │                        ├─(StreamID ∈ {private, ECM, тощо})→ [StateReadBytes]
   │                        └─(StreamID == Padding)→ [StateReadPaddingBytes]
   │
   └─(не знайдено)→ чекати більше даних

[StateReadPacket] 
   │
   ├─(читання ElementaryStream до наступного StartOfPacket)
   └─(StartOfPacket знайдено)→ відправити PES → [StateFindPrefix]

[StateReadBytes] / [StateReadPaddingBytes]
   │
   ├─(прочитано PacketLength байт)→ [StateFindPrefix]
```

#### ⚠️ Критичні проблеми в циклі
```go
// ❌ Потенційний deadlock через блокування каналу:
select {
case w, ok := <-pp.byteIncomingChan:  // ❌ Блокує, якщо канал порожній
    // ...
case <-ctx.Done():
    // ...
}
// 📋 Якщо consumer не читає з output channel → parser блокується → byteIncomingChan переповнюється → WriteBytes повертає error
// ✅ Правильно: використовувати non-blocking receive з таймаутом або backpressure mechanism

// ❌ Неправильна обробка EndOfStream:
for i := 0; i < len(in); i++ {
    if in[i].EndOfStream {
        in = in[:i]  // ❌ Обрізає вхідні дані, але не обробляє останній пакет!
        isLast = true
        break
    }
}
// 📋 Якщо EndOfStream встановлено, потрібно завершити поточний PES пакет перед виходом
// ✅ Правильно: обробити поточний стан перед виходом:
if isLast {
    // 🎯 Завершити поточний PES пакет, якщо він в процесі
    if state != StateFindPrefix {
        pp.flushCurrentPES(pesOutChan)  // Helper для завершення пакету
    }
    return
}

// ❌ Відсутність обмеження на розмір ElementaryStream:
pp.PES.ElementaryStream = append(pp.PES.ElementaryStream, v.Datum)  // ❌ Може рости необмежено!
// 📋 Для 4K відео: PES пакет може бути >1MB → OOM!
// ✅ Правильно: додати максимальний розмір та backpressure:
const MaxElementaryStreamSize = 64 * 1024 * 1024  // 64MB
if len(pp.PES.ElementaryStream) + len(in) > MaxElementaryStreamSize {
    logger.Error("ElementaryStream too large, dropping packet", 
        "size", len(pp.PES.ElementaryStream), "max", MaxElementaryStreamSize)
    pp.resetPES()  // Скинути поточний пакет
    state = StateFindPrefix
    continue
}

// ❌ Неефективне копіювання в output channel:
pr := pp.PES.DeepCopy()  // ❌ Копіює ВСІ поля, включаючи великі слайси!
pesOutChan <- pr
// 📋 DeepCopy() копіює ElementaryStream, PacketDataStream, тощо → великі аллокації
// ✅ Правильно: використовувати zero-copy або посилання з м'ютексом:
// Варіант А: Відправляти посилання + документувати, що отримувач не модифікує
// Варіант Б: Використовувати sync.Pool для повторного використання буферів
```

---

### 6️⃣ `WriteBytes()` та інтеграція з TS

```go
func (pp *PESParser) WriteBytes(p []byte, sop, eos bool) (n int, error) {
    // 🎯 Конвертація []byte → []PESByte
    pesBytes := make([]PESByte, 0, len(p))
    for _, v := range p {
        b = PESByte{Datum: v}
        pesBytes = append(pesBytes, b)
    }
    pesBytes[0].StartOfPacket = sop  // ✅ Встановлення метаданих
    pesBytes[len(p)-1].EndOfStream = eos
    
    // 🎯 Non-blocking send у канал
    select {
    case pp.byteIncomingChan <- pesBytes:
        return len(p), nil
    default:
        return 0, errors.New("byteIncomingChan blocked")  // ❌ Втрата даних!
    }
}

func (pp *PESParser) EnqueueTSPacket(tsPacket Packet) error {
    byteBuffer, err := tsPacket.GetPayload()  // ✅ Отримання payload з TS пакету
    if err != nil {
        return err
    }
    // 🎯 PUSI вказує на початок нового PES пакету
    _, err = pp.WriteBytes(byteBuffer, tsPacket.PayloadUnitStartIndicator, false)
    return err
}
```

#### ⚠️ Проблеми інтеграції
```go
// ❌ Втрата даних при переповненні каналу:
default:
    return 0, errors.New("byteIncomingChan blocked")  // ❌ Дані просто відкидаються!
// 📋 У live-стрімінгу це призведе до втрати кадрів/аудіо
// ✅ Правильно: блокувати відправника або використовувати backpressure:
// Варіант А: Блокувати WriteBytes доки канал не звільниться (може призвести до затримок)
// Варіант Б: Повертати спеціальну помилку для retry логіки
// Варіант В: Використовувати більший буфер каналу + моніторинг заповнення

// ❌ Неправильне встановлення EndOfStream:
pesBytes[len(p)-1].EndOfStream = eos  // ❌ Встановлює тільки на останній байт чанку
// 📋 EndOfStream має вказувати на кінець потоку, не чанку!
// ✅ Правильно: EndOfStream має встановлюватися тільки при реальному завершенні PES потоку
// (напр., при закритті каналу), не для кожного чанку
```

---

## ⚠️ Загальні проблеми модуля

### 1️⃣ Відсутність валідації вхідних даних
```go
// ❌ Парсер приймає будь-які дані без перевірки:
// • Чи StreamID валідний?
// • Чи PacketLength узгоджений з реальними даними?
// • Чи PTS/DTS у допустимому діапазоні?

// ✅ Додати валідацію на кожному етапі:
func (pp *PESParser) validatePESHeader() error {
    if pp.PES.StreamID > 0xff {
        return fmt.Errorf("invalid StreamID: 0x%X", pp.PES.StreamID)
    }
    if pp.PES.PacketLength > 65535 {
        return fmt.Errorf("PacketLength too large: %d", pp.PES.PacketLength)
    }
    // ... інші перевірки ...
    return nil
}
```

### 2️⃣ Пам'ять та продуктивність
```go
// ❌ Копіювання даних на кожному кроці:
// • WriteBytes: []byte → []PESByte (копія)
// • findPrefix: dequeue через slice copying (O(n))
// • StartPESReadLoop: DeepCopy() для output (копія великих слайсів)

// ✅ Оптимізації:
// • Використовувати zero-copy де можливо (посилання з м'ютексом)
// • Замінити slice-based buffer на ring buffer для O(1) dequeue
// • Використовувати sync.Pool для повторного використання буферів PES

// Приклад ring buffer для PESByte:
type RingBuffer struct {
    data  []PESByte
    head  int
    tail  int
    count int
}

func (rb *RingBuffer) Dequeue(n int) []PESByte {
    // O(1) операція замість O(n) copying
    // ...
}
```

### 3️⃣ Обробка помилок
```go
// ❌ Парсер "тихо" скидає стан при помилках:
if (pp.buffer[0].Datum>>6)&0x03 != 0x02 {
    pp.dequeue(1)  // ❌ Пропускає 1 байт і продовжує
    state = StateFindPrefix
    break ReadLoop
}
// 📋 Це може призвести до каскадних помилок при пошкоджених даних
// ✅ Правильно: логувати помилки та надавати статистику:
type ParseStats struct {
    PrefixErrors    int
    HeaderErrors    int
    CRCErrors       int
    // ...
}

func (pp *PESParser) GetStats() ParseStats {
    return pp.stats
}
```

### 4️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу
// • Неможливо перевірити коректність бітового парсингу
// • Неможливо покрити edge cases (пошкоджені PES, незвичні StreamID)

// ✅ Додати мінімальні тести:
func TestParseOptionalPESHeaders_ValidPTS(t *testing.T) {
    // 🎯 Створити моковий PES з відомим PTS
    parser := NewPESParser(1024)
    mockData := createMockPESWithPTS(123456789)  // 33-біт значення
    
    parser.enqueue(mockData)
    parser.parseOptionalPESHeaders()
    
    assert.Equal(t, float64(123456789)/90000, parser.PES.PTS)
    assert.True(t, parser.PES.PTSFlag)
}

func TestFindPrefix_Resync(t *testing.T) {
    // 🎯 Тест ресинхронізації після втрати префіксу
    parser := NewPESParser(1024)
    // 🎯 Дані з помилкою: префікс зміщений
    corrupted := []byte{0x00, 0x00, 0x02, 0x00, 0x00, 0x01, ...}  // 0x000002 замість 0x000001
    parser.enqueue(corrupted)
    
    found := parser.findPrefix()
    assert.False(t, found)  // Не знайдено з першої спроби
    // 🎯 Після додавання правильних даних має знайти
}
```

### 5️⃣ Документація та читабельність
```go
// ❌ Відсутність коментарів для складної логіки:
// • Чому trimIndex += 5 для PTS?
// • Як працює бітовий парсинг 33-бітного значення?

// ✅ Додати коментарі з посиланнями на специфікацію:
// 📋 ITU-T H.222.0 Section 2.5.3.6: PTS/DTS syntax
// PTS is coded as 33 bits in 5 parts:
// [3 bits][marker:1][15 bits][marker:1][15 bits]
// This function reconstructs the 33-bit value by shifting and masking each part.
if pp.PTSFlag {
    // Part 1: bits 32-30 (3 bits) from buffer[3]
    part1 := uint64((pp.buffer[3].Datum>>1) & 0x07) << 30
    // Part 2: bits 29-22 (8 bits) from buffer[4]
    part2 := uint64(pp.buffer[4].Datum) << 22
    // ... інші частини ...
    pp.PES.rawPTS = part1 | part2 | part3 | part4 | part5
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **WebSocket-приймачем TS-фрагментів**:

### 🎯 Сценарій: витяг відео/аудіо PES для транскодації
```go
// У PESRouter для розділення відео/аудіо потоків:
type PESRouter struct {
    videoStreamID  byte  // Напр. 0xE0 для H.264
    audioStreamID  byte  // Напр. 0xC0 для AAC
    onVideoPES     func(pes *PES)
    onAudioPES     func(pes *PES)
}

func (r *PESRouter) ProcessPES(pes *PES) {
    switch pes.StreamID {
    case r.videoStreamID:
        if r.onVideoPES != nil {
            r.onVideoPES(pes)  // ✅ Відправка у відео транскодер
        }
    case r.audioStreamID:
        if r.onAudioPES != nil {
            r.onAudioPES(pes)  // ✅ Відправка у аудіо транскодер
        }
    }
}

// Інтеграція з PESParser:
go func() {
    pesChan := pesParser.StartPESReadLoop(ctx)
    router := NewPESRouter(0xE0, 0xC0)
    
    for pes := range pesChan {
        // 🎯 Валідація перед обробкою
        if err := pes.Validate(); err != nil {
            log.Printf("Invalid PES: %v", err)
            continue
        }
        
        // 🎯 Маршрутизація
        router.ProcessPES(&pes)
    }
}()
```

### 🎯 Сценарій: A/V синхронізація через PTS/DTS
```go
// У AVSync модулі для корекції drift:
type AVSync struct {
    lastVideoPTS float64
    lastAudioPTS float64
    maxDrift     time.Duration
}

func (s *AVSync) ProcessVideoPES(pes *PES) {
    if !pes.PTSFlag {
        return  // Немає таймштампу → не можна синхронізувати
    }
    
    currentPTS := time.Duration(pes.rawPTS) * time.Second / 90000
    drift := currentPTS - time.Duration(s.lastVideoPTS)*time.Second/90000
    
    if drift.Abs() > s.maxDrift {
        log.Printf("Video drift detected: %v", drift)
        // 🎯 Тут можна скоригувати PTS у payload або відправити алерт
    }
    
    s.lastVideoPTS = pes.rawPTS
    // 🎯 Відправка у транскодер...
}
```

### 🎯 Сценарій: моніторинг якості PES потоку
```go
// У monitoring.Monitor для агрегації метрик:
type PESQualityMetrics struct {
    TotalPackets      int64
    ParseErrors       int64
    MissingPTS        int64
    InvalidStreamIDs  int64
    AvgPacketSize     float64
}

func (m *Monitor) AnalyzePESQuality(parser *PESParser) PESQualityMetrics {
    // 🎯 Отримання статистики з парсера (потрібно додати метод)
    stats := parser.GetStats()
    
    metrics := PESQualityMetrics{
        TotalPackets:     stats.TotalPackets,
        ParseErrors:      stats.HeaderErrors + stats.PrefixErrors,
        MissingPTS:       stats.MissingPTS,
        // ...
    }
    
    // 🎯 Алерти при поганій якості
    if float64(metrics.ParseErrors)/float64(metrics.TotalPackets) > 0.01 {
        m.alerts["pes_parse_errors"].Inc()
    }
    
    return metrics
}
```

---

## 🧪 Приклад: рефакторинг `findPrefix()` з кращою стійкістю

```go
// ✅ Стійкий пошук префіксу з обмеженням:
func (pp *PESParser) findPrefix() bool {
    const (
        MinPrefixSize = 3  // 0x000001
        MaxSearchWindow = 1024  // Максимум байтів для пошуку
    )
    
    bufferLen := pp.getBufferLength()
    if bufferLen < MinPrefixSize {
        return false
    }
    
    // 🎯 Обмежуємо пошук вікном, щоб не втратити дані
    searchLimit := min(bufferLen, MaxSearchWindow)
    
    // 🎯 Оптимізований пошук: спочатку шукаємо 0x01, потім перевіряємо попередні байти
    for i := 2; i < searchLimit; i++ {
        if pp.buffer[i].Datum == 0x01 && 
           pp.buffer[i-1].Datum == 0x00 && 
           pp.buffer[i-2].Datum == 0x00 {
            
            // 🎯 Знайдено префікс на позиції i-2
            prefixIndex := i - 2
            
            // 🎯 Видалити байти ДО префіксу (не включаючи префікс)
            pp.dequeue(prefixIndex)
            
            // 🎯 Парсинг заголовка після префіксу
            if pp.getBufferLength() < 3 {  // stream_id + packet_length
                return false
            }
            
            pp.PES.Prefix = 0x000001  // ✅ Відоме значення
            pp.PES.StreamID = pp.buffer[0].Datum
            pp.PES.PacketLength = uint16(pp.buffer[1].Datum)<<8 | uint16(pp.buffer[2].Datum)
            
            // 🎯 Видалити префікс + заголовок (6 байт)
            pp.dequeue(3)  // Префікс вже видалено, залишилось 3 байти заголовка
            return true
        }
    }
    
    // 🎯 Префікс не знайдено у вікні → видалити частину буфера
    toRemove := min(bufferLen, MaxSearchWindow/2)  // Видалити половину вікна
    pp.dequeue(toRemove)
    return false
}
```

---

## 📋 Best Practices для PES парсингу

```
✅ Валідація вхідних даних:
   • Перевіряти StreamID діапазон (0x00-0xFF)
   • Валідувати marker bits (завжди 0x02 для опціонального заголовка)
   • Перевіряти PTS/DTS діапазон (33 біти, 0-2^33-1)

✅ Управління пам'яттю:
   • Встановлювати максимальний розмір ElementaryStream (напр. 64MB)
   • Використовувати ring buffer замість slice copying для dequeue
   • Застосовувати sync.Pool для повторного використання буферів

✅ Обробка помилок:
   • Логувати помилки з контекстом (StreamID, позиція у потоці)
   • Надавати статистику помилок через GetStats() метод
   • Реалізувати graceful degradation при пошкоджених даних

✅ Продуктивність:
   • Уникати зайвих копій даних (zero-copy де можливо)
   • Оптимізувати пошук префіксу (Boyer-Moore або hardware acceleration)
   • Використовувати buffered channels з розумним розміром

✅ Тестування:
   • Додати юніт-тести для бітового парсингу (PTS/DTS, прапорці)
   • Покрити edge cases: пошкоджені префікси, невалідні StreamID
   • Додати інтеграційні тести з реальними TS/PES файлами

✅ Документація:
   • Коментувати складну бітову логіку з посиланнями на специфікацію
   • Документувати обмеження (напр. не підтримуються ESCR, PES extensions)
   • Надавати приклади використання для інтеграції з TS парсером
```

---

## 🎯 Висновок

Цей `PESParser` — **потужний, але складний** інструмент для парсингу PES:

✅ Правильна реалізація state machine за специфікацією ITU-T H.222.0  
✅ Підтримка конкурентності через channels та mutexes  
✅ Інтеграція з TS парсером через EnqueueTSPacket

**Критичні виправлення перед продакшеном**:

1. ✅ **Виправити втрату старшого біта у PTS/DTS** → використовувати `uint64` замість `uint32`
2. ✅ **Додати обмеження на розмір ElementaryStream** для запобігання OOM
3. ✅ **Оптимізувати буферизацію** → замінити slice copying на ring buffer
4. ✅ **Покращити обробку помилок** → логування + статистика замість "тихого" скидання
5. ✅ **Додати валідацію вхідних даних** на кожному етапі парсингу
6. ✅ **Документувати обмеження** (не підтримуються ESCR, PES extensions, тощо)
7. ✅ **Додати тести** для бітового парсингу та edge cases

**Приклад інтеграції у ваш pipeline**:
```go
// 🎯 PESIngestor для вашого WebSocket-сервера:
type PESIngestor struct {
    channelID  string
    parser     *PESParser
    router     *PESRouter
    transcoder *Transcoder  // Ваш існуючий компонент
}

func (i *PESIngestor) Start(ctx context.Context) error {
    pesChan := i.parser.StartPESReadLoop(ctx)
    
    for {
        select {
        case pes, ok := <-pesChan:
            if !ok {
                return nil  // Канал закрито
            }
            
            // 🎯 Валідація та маршрутизація
            if err := pes.Validate(); err != nil {
                i.logger.Warn("Invalid PES", "error", err, "stream_id", pes.StreamID)
                continue
            }
            
            switch pes.StreamID {
            case 0xE0:  // H.264 відео
                if err := i.transcoder.ProcessVideoPES(&pes); err != nil {
                    i.logger.Error("Video transcode failed", "error", err)
                }
            case 0xC0:  // AAC аудіо
                if err := i.transcoder.ProcessAudioPES(&pes); err != nil {
                    i.logger.Error("Audio transcode failed", "error", err)
                }
            }
            
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом `findPrefix()` та `parseOptionalPESHeaders()` з кращою стійкістю та валідацією?
- 🧠 Оптимізацією буферизації через ring buffer для high-throughput сценаріїв?
- 🧪 Написанням тестів для бітового парсингу з моковими даними та валідацією меж?

Чекаю на ваші питання! 🛠️📡🎬