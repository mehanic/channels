# 📦 Глибокий розбір: `mpeg.Buffer` — Бітовий буфер для медіа-декодерів

Цей файл — **реалізація універсального бітового буфера** з підтримкою потокового завантаження, seek, та бітових операцій. Він є фундаментом для MP2 декодера та інших медіа-парсерів, що працюють з даними змінної довжини на рівні біт.

---

## 🗺️ Архітектурна схема Buffer

```
┌────────────────────────────────────────┐
│ 📦 mpeg.Buffer — Bit Buffer Core      │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Bit-level reading (read, read1)    │
│  • Stream loading (LoadReaderCallback)│
│  • Seek support (io.Seeker)           │
│  • Sync code detection (findFrameSync)│
│                                         │
│  🔄 Потік даних:                        │
│  io.Reader → Buffer.Write()           │
│  → bitIndex → read()/read1()          │
│  → Media decoder                      │
│                                         │
│  📡 Особливості:                        │
│  • Автоматичне завантаження даних     │
│  • Утилізація прочитаних байт         │
│  • Пошук синхронізаційних кодів       │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Buffer — структура бітового буфера

### 🔧 Структура та призначення:

```go
type Buffer struct {
    reader io.Reader        // ⭐ вхідний потік даних (може бути nil)
    bytes  []byte          // ⭐ буфер непрочитаних даних
    
    bitIndex  int          // ⭐ поточна позиція у бітах (не байтах!)
    totalSize int          // ⭐ загальний розмір для seekable джерел
    
    hasEnded    bool       // ⭐ чи досягнуто кінця потоку
    discardRead bool       // ⭐ чи видаляти прочитані дані з буфера
    
    available    []byte    // ⭐ тимчасовий буфер для завантаження
    loadCallback LoadFunc  // ⭐ callback для автоматичного завантаження
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `reader` | `io.Reader` | **Критично**: джерело вхідних даних | `os.File`, `net.Conn`, або `nil` для попередньо завантажених даних |
| `bytes` | `[]byte` | **Критично**: буфер непрочитаних даних | `[0xFF, 0xFB, 0x90, ...]` — сирий бітовий потік |
| `bitIndex` | `int` | **Критично**: поточна позиція у бітах | `17` = 2 байти + 1 біт прочитано |
| `totalSize` | `int` | **Критично**: загальний розмір для seekable джерел | `1234567` байт для файлу |
| `hasEnded` | `bool` | **Критично**: прапорець кінця потоку | `true` = немає більше даних очікувати |
| `discardRead` | `bool` | **Критично**: чи видаляти прочитані дані | `true` = економити пам'ять, `false` = зберігати історію |
| `loadCallback` | `LoadFunc` | **Критично**: callback для автоматичного завантаження | `LoadReaderCallback` для потокового читання |

### 🔍 Чому `bitIndex` замість `byteIndex`?

```
Медіа-формати (MP2, H.264, AAC) використовують бітове кодування змінної довжини:
• Заголовки: 11-бітний sync, 2-бітний version, тощо
• Квантовані семпли: 3-7 біт на значення
• VLC таблиці: 1 біт за раз для декодування

Приклад читання:
  1. read(11) → читає 11 біт для sync word
  2. read(2)  → читає 2 біти для version
  3. read1()  → читає 1 біт для прапорця
  
  bitIndex прогресує: 0 → 11 → 13 → 14 → ...
  
Це дозволяє:
• Ефективне декодування без вирівнювання до байт
• Підтримку кодування змінної довжини (VLC)
• Точний контроль позиції для resync
```

### ✅ Ваш use-case**: ініціалізація буфера з валідацією

```go
// NewBufferWithValidation — безпечне створення буфера
func NewBufferWithValidation(r io.Reader) (*mpeg.Buffer, error) {
    buf, err := mpeg.NewBuffer(r)
    if err != nil {
        return nil, fmt.Errorf("create buffer: %w", err)
    }
    
    // Валідація для seekable джерел
    if buf.Seekable() {
        size := buf.Size()
        if size < 1024 {  // мінімум для валідного медіа-файлу
            return nil, fmt.Errorf("file too small: %d bytes", size)
        }
    }
    
    // Налаштування callback для автоматичного завантаження
    buf.SetLoadCallback(buf.LoadReaderCallback)
    
    return buf, nil
}

// Використання:
file, err := os.Open("audio.mp2")
if err != nil { /* handle error */ }
defer file.Close()

buffer, err := NewBufferWithValidation(file)
if err != nil {
    log.Printf("error creating buffer: %v", err)
    return
}

log.Printf("Initialized buffer: seekable=%v, size=%d", 
    buffer.Seekable(), buffer.Size())
```

---

## 🔑 2. Бітові операції: read(), read1(), align()

### 🔧 read(count) — читання N біт:

```go
func (b *Buffer) read(count int) int {
    value := 0
    for count != 0 {
        // 1. Отримання поточного байта
        currentByte := int(b.bytes[b.bitIndex>>3])  // bitIndex/8 = byte index
        
        // 2. Розрахунок залишкових біт у цьому байті
        remaining := 8 - (b.bitIndex & 7)  // 8 - (bitIndex % 8)
        read := count
        if remaining < count {  // якщо потрібно більше біт ніж залишилось
            read = remaining
        }
        
        // 3. Виділення потрібних біт
        shift := remaining - read  // зсув для вирівнювання праворуч
        mask := 0xff >> (8 - read)  // маска для read біт
        
        // 4. Комбінування у результат
        value = (value << read) | ((currentByte & (mask << shift)) >> shift)
        
        // 5. Оновлення позиції
        b.bitIndex += read
        count -= read
    }
    return value
}
```

### 🔍 Як працює читання через байтові межі:

```
Приклад: читання 11 біт починаючи з bitIndex=5 (середина байта):

Байт 0: [b7 b6 b5 b4 b3 b2 b1 b0]  ← bitIndex=5 вказує на b2
Байт 1: [b7 b6 b5 b4 b3 b2 b1 b0]

Кроки:
1. currentByte = байт0, remaining = 8-5 = 3 біти залишилось
2. read = min(11, 3) = 3 біти
3. shift = 3-3 = 0, mask = 0xff >> 5 = 0x07 (біти 0-2)
4. value = 0 << 3 | ((байт0 & 0x07) >> 0) = нижні 3 біти байта0
5. bitIndex = 5+3 = 8, count = 11-3 = 8

Друга ітерація:
1. currentByte = байт1 (bitIndex=8 → byte index=1), remaining = 8
2. read = min(8, 8) = 8 біт
3. shift = 8-8 = 0, mask = 0xff
4. value = (попереднє значення << 8) | байт1
5. bitIndex = 8+8 = 16, count = 0 → готово

Результат: 11-бітне значення з 3 біт байта0 + 8 біт байта1
```

### 🔧 read1() — оптимізоване читання 1 біта:

```go
func (b *Buffer) read1() int {
    currentByte := int(b.bytes[b.bitIndex>>3])
    shift := 7 - (b.bitIndex & 7)  // зсув від старшого біта
    value := (currentByte & (1 << shift)) >> shift  // виділення 1 біта
    b.bitIndex += 1
    return value
}
```

### 🔍 Чому окрема реалізація для 1 біта?

```
read1() використовується дуже часто у:
• VLC декодуванні (1 біт за раз для таблиць)
• Читанні прапорців (hasCRC, padding, тощо)
• Пошуку синхронізаційних кодів

Оптимізації у read1():
• Уникнення циклу for з read()
• Прямий доступ до біта через бітові операції
• Менше обчислень: немає mask, shift розраховується один раз

Продуктивність:
• read(1): ~10-15 операцій (цикл, mask, shift, комбінування)
• read1(): ~4-5 операцій (прямий доступ, один зсув)
• Економія: 2-3x швидше для частого читання 1 біта
```

### 🔧 align() — вирівнювання до байта:

```go
func (b *Buffer) align() {
    b.bitIndex = ((b.bitIndex + 7) >> 3) << 3  // округлення вгору до кратного 8
}
```

### 🔍 Призначення вирівнювання:

```
Багато форматів вимагають вирівнювання до байта:
• Заголовки кадру часто починаються з байтової межі
• Синхронізаційні коди вирівняні для швидкого пошуку
• Деякі поля мають фіксовану байтову довжину

Формула:
  ((bitIndex + 7) >> 3) << 3
  
Приклади:
  • bitIndex=0  → ((0+7)>>3)<<3 = (7>>3)<<3 = 0<<3 = 0 ✓
  • bitIndex=5  → ((5+7)>>3)<<3 = (12>>3)<<3 = 1<<3 = 8 ✓
  • bitIndex=8  → ((8+7)>>3)<<3 = (15>>3)<<3 = 1<<3 = 8 ✓
  • bitIndex=13 → ((13+7)>>3)<<3 = (20>>3)<<3 = 2<<3 = 16 ✓

Це ефективно реалізує: ceil(bitIndex / 8) * 8
```

### ✅ Ваш use-case**: бітове декодування VLC таблиць

```go
// DecodeVLC — декодування значення з VLC таблиці
func DecodeVLC(buffer *mpeg.Buffer, table []mpeg.VLC) (int, error) {
    var state mpeg.VLC
    
    for {
        // Читання 1 біта для навігації по дереву VLC
        bit := buffer.Read1()
        state = table[int(state.Index)+bit]
        
        // Вихід коли досягнуто листового вузла (Index <= 0)
        if state.Index <= 0 {
            break
        }
    }
    
    return int(state.Value), nil
}

// Використання для декодування квантованих значень:
quantValue, err := DecodeVLC(buffer, quantizerVLCTable)
if err != nil {
    return fmt.Errorf("decode quantizer: %w", err)
}
// quantValue тепер містить декодоване значення з таблиці
```

---

## 🔑 3. Управління буфером: Write(), discardReadBytes(), has()

### 🔧 Write(p) — додавання даних у буфер:

```go
func (b *Buffer) Write(p []byte) int {
    if b.discardRead {
        b.discardReadBytes()  // видалення прочитаних даних для економії пам'яті
    }
    
    b.bytes = append(b.bytes, p...)  // додавання нових даних
    b.hasEnded = false  // скидання прапорця кінця
    return len(p)
}
```

### 🔍 Чому `discardRead` важливий?

```
Для довгих потоків (напр. streaming) буфер може рости нескінченно:
• Без discardRead: bytes = [прочитані + непрочитані] → пам'ять зростає
• З discardRead=true: bytes = [тільки непрочитані] → постійний розмір

Логіка discardReadBytes():
  bytePos = bitIndex >> 3  // позиція у байтах
  if bytePos == len(bytes):  // все прочитано
      bytes = bytes[:0]  // повне очищення
      bitIndex = 0
  else if bytePos > 0:  // частково прочитано
      copy(bytes, bytes[bytePos:])  // зсув непрочитаних на початок
      bytes = bytes[:len(bytes)-bytePos]  // обрізання
      bitIndex -= bytePos << 3  // корекція позиції

Приклад:
  bytes = [A B C D E F], bitIndex=20 (2.5 байта)
  bytePos = 20>>3 = 2
  copy(bytes, bytes[2:]) → bytes = [C D E F E F]
  bytes = bytes[:4] → bytes = [C D E F]
  bitIndex = 20 - (2<<3) = 20-16 = 4 (тепер вказує на C)
```

### ⚠️ Критична проблема: race condition у discardReadBytes

```
У поточному коді:
    copy(b.bytes, b.bytes[bytePos:])
    b.bytes = b.bytes[:len(b.bytes)-bytePos]

Проблема:
• Якщо bytePos > len(b.bytes)/2, то src і dst перекриваються
• copy() у Go безпечний для перекриття, але це може бути неочевидно
• Для великих буферів це може призвести до некоректних даних

✅ Виправлення: явна перевірка перекриття
    func (b *Buffer) discardReadBytes() {
        bytePos := b.bitIndex >> 3
        if bytePos == len(b.bytes) {
            b.bytes = b.bytes[:0]
            b.bitIndex = 0
        } else if bytePos > 0 {
            unread := len(b.bytes) - bytePos
            if bytePos > unread {
                // Безпечне копіювання через тимчасовий буфер
                tmp := make([]byte, unread)
                copy(tmp, b.bytes[bytePos:])
                copy(b.bytes, tmp)
            } else {
                copy(b.bytes, b.bytes[bytePos:])
            }
            b.bytes = b.bytes[:unread]
            b.bitIndex -= bytePos << 3
        }
    }
```

### 🔧 has(count) — перевірка наявності даних:

```go
func (b *Buffer) has(count int) bool {
    // 1. Перевірка чи достатньо даних у буфері
    if ((len(b.bytes) << 3) - b.bitIndex) >= count {
        return true
    }
    
    // 2. Спроба завантажити більше даних через callback
    if b.loadCallback != nil {
        b.loadCallback(b)
        if ((len(b.bytes) << 3) - b.bitIndex) >= count {
            return true
        }
    }
    
    // 3. Перевірка кінця для seekable джерел
    if b.totalSize != 0 && len(b.bytes) == b.totalSize {
        b.hasEnded = true
    }
    
    return false
}
```

### 🔍 Логіка автоматичного завантаження:

```
has() забезпечує прозору буферизацію для потокових джерел:

1. Перевірка локального буфера:
   • (len(bytes) << 3) = загальна кількість біт у буфері
   • Мінус bitIndex = непрочитані біти
   • Якщо >= count → дані доступні

2. Автоматичне завантаження:
   • Якщо loadCallback встановлено → виклик callback
   • LoadReaderCallback читає з io.Reader у available буфер
   • Записує прочитане у bytes через Write()
   • Повторна перевірка наявності даних

3. Визначення кінця:
   • Для seekable: якщо bytes == totalSize → кінець
   • Для потоків: LoadReaderCallback встановлює hasEnded при EOF

Приклад потоку:
  has(11) → недостатньо даних → LoadReaderCallback → 
  читає 128KB з мережі → Write() → повторна перевірка → true
```

### ✅ Ваш use-case**: налаштування буфера для low-latency streaming

```go
// LowLatencyBuffer — буфер з оптимізацією для низької затримки
type LowLatencyBuffer struct {
    base *mpeg.Buffer
    minRead int  // мінімальний розмір для завантаження
}

func NewLowLatencyBuffer(r io.Reader) (*LowLatencyBuffer, error) {
    buf, err := mpeg.NewBuffer(r)
    if err != nil {
        return nil, err
    }
    
    // Налаштування меншого буфера для швидшої реакції
    mpeg.BufferSize = 32 * 1024  // 32KB замість 128KB
    
    // Вимкнення discardRead для збереження історії (корисно для resync)
    buf.discardRead = false
    
    // Налаштування callback з мінімальним завантаженням
    buf.SetLoadCallback(func(b *mpeg.Buffer) {
        if b.Remaining() < 1024 {  // завантажувати якщо <1KB залишилось
            b.LoadReaderCallback(b)
        }
    })
    
    return &LowLatencyBuffer{
        base: buf,
        minRead: 1024,
    }, nil
}

func (b *LowLatencyBuffer) Read(count int) (int, error) {
    // Примусове завантаження якщо потрібно
    if !b.base.has(count) {
        // Спроба завантажити мінімальний розмір
        b.base.LoadReaderCallback(b.base)
    }
    
    if !b.base.has(count) {
        return 0, io.ErrNoData  // немає достатньо даних
    }
    
    return b.base.read(count), nil
}
```

---

## 🔑 4. Пошук синхронізаційних кодів: findFrameSync(), nextStartCode()

### 🔧 findFrameSync() — пошук синхронізації кадру:

```go
func (b *Buffer) findFrameSync() bool {
    var i int
    for i = b.bitIndex >> 3; i < len(b.bytes)-1; i++ {
        // Пошук шаблону: 0xFF + (0xFC..0xFF) для MP2 sync
        if b.bytes[i] == 0xFF && (b.bytes[i+1]&0xFE) == 0xFC {
            // Знайдено: встановлення позиції після sync word
            b.bitIndex = ((i + 1) << 3) + 3  // після 11 біт sync
            return true
        }
    }
    
    // Не знайдено: встановлення позиції в кінець буфера
    b.bitIndex = (i + 1) << 3
    return false
}
```

### 🔍 Формат синхронізації MP2:

```
MP2 кадр починається з 11-бітного sync word: 0x7FF (біти: 11111111111)

У байтовому представленні:
  Байт 0: 0xFF (8 біт: 11111111)
  Байт 1: 0xFC..0xFF (верхні 3 біти: 111, решта варіюється)
  
Умова пошуку:
  bytes[i] == 0xFF && (bytes[i+1] & 0xFE) == 0xFC
  
Де:
  • 0xFE = 11111110 — маска для перевірки верхніх 7 біт
  • 0xFC = 11111100 — очікуване значення верхніх 6 біт
  • Разом: перевірка що байт1 має біти 111111х (де х = будь-який)

Після знаходження:
  • bitIndex = ((i+1) << 3) + 3
  • (i+1) << 3 = початок байта1 у бітах
  • +3 = пропуск перших 3 біт байта1 (разом з 8 біт байта0 = 11 біт sync)
```

### ⚠️ Критична проблема: неефективний пошук для великих буферів

```
У поточному коді:
    for i = b.bitIndex >> 3; i < len(b.bytes)-1; i++ {
        if b.bytes[i] == 0xFF && (b.bytes[i+1]&0xFE) == 0xFC { ... }
    }

Проблема:
• Лінійний пошук O(n) для кожного виклику findFrameSync()
• Для пошкоджених потоків може шукати дуже довго
• Немає обмеження на кількість перевірок

✅ Виправлення: оптимізація через таблицю пошуку 0xFF
    func (b *Buffer) findFrameSyncOptimized() bool {
        // Попередній пошук 0xFF для зменшення перевірок
        start := b.bitIndex >> 3
        for i := start; i < len(b.bytes)-1; {
            // Швидкий пошук наступного 0xFF
            nextFF := bytes.IndexByte(b.bytes[i:], 0xFF)
            if nextFF == -1 {
                break  // немає більше 0xFF у буфері
            }
            
            i += nextFF
            // Перевірка тільки якщо знайдено 0xFF
            if (b.bytes[i+1] & 0xFE) == 0xFC {
                b.bitIndex = ((i + 1) << 3) + 3
                return true
            }
            i++  // продовження пошуку
        }
        
        b.bitIndex = (len(b.bytes)) << 3
        return false
    }
```

### 🔧 nextStartCode() — пошук start code (для MPEG video):

```go
func (b *Buffer) nextStartCode() int {
    b.align()  // вирівнювання до байта
    
retry:
    for ((len(b.bytes) << 3) - b.bitIndex) >= (5 << 3) {  // мінімум 5 байт
        data := b.bytes
        byteIndex := b.bitIndex >> 3
        
        // Пошук шаблону: 00 00 01 XX
        if data[byteIndex] == 0x00 &&
           data[byteIndex+1] == 0x00 &&
           data[byteIndex+2] == 0x01 {
            b.bitIndex = (byteIndex + 4) << 3  // позиція після start code
            return int(data[byteIndex+3])  // повертає тип start code
        }
        
        b.bitIndex += 8  // перехід до наступного байта
    }
    
    // Якщо недостатньо даних → спроба завантажити
    if b.has(5 << 3) {
        goto retry
    }
    
    return -1  // не знайдено
}
```

### ✅ Ваш use-case**: обробка втраченої синхронізації

```go
// ResyncDecoder — декодер з автоматичним відновленням синхронізації
type ResyncDecoder struct {
    buffer *mpeg.Buffer
    maxResyncAttempts int
}

func (r *ResyncDecoder) DecodeFrame() (*mpeg.Samples, error) {
    for attempt := 0; attempt < r.maxResyncAttempts; attempt++ {
        // Спроба декодувати заголовок
        if r.buffer.has(48) {  // мінімум для заголовка
            samples, err := tryDecodeFrame(r.buffer)
            if err == nil {
                return samples, nil
            }
            // Помилка → можлива втрата синхронізації
        }
        
        // Пошук нової синхронізації
        if !r.buffer.findFrameSync() {
            // Спроба завантажити більше даних
            if r.buffer.loadCallback != nil {
                r.buffer.loadCallback(r.buffer)
                if r.buffer.findFrameSync() {
                    continue  // повторна спроба декодування
                }
            }
            break  // не вдалося відновити
        }
    }
    
    return nil, fmt.Errorf("failed to resync after %d attempts", r.maxResyncAttempts)
}

// tryDecodeFrame — спроба декодувати один кадр (спрощено)
func tryDecodeFrame(buf *mpeg.Buffer) (*mpeg.Samples, error) {
    // Читання заголовка
    sync := buf.read(11)
    if sync != 0x7FF {
        return nil, fmt.Errorf("invalid sync: 0x%X", sync)
    }
    // ... решта декодування ...
    return &mpeg.Samples{}, nil
}
```

---

## 🔑 5. Seek та навігація: seek(), tell(), Rewind()

### 🔧 seek(pos) — переміщення позиції:

```go
func (b *Buffer) seek(pos int) {
    b.hasEnded = false
    
    if b.reader != nil && b.totalSize > 0 {
        // Seekable джерело: використовуємо io.Seeker
        seeker := b.reader.(io.Seeker)
        _, _ = seeker.Seek(int64(pos), io.SeekStart)
        b.bytes = b.bytes[:0]  // очищення буфера
        b.bitIndex = 0
    } else if b.reader == nil {
        // Тільки пам'ять: підтримуємо тільки rewind до початку
        if pos != 0 {
            return  // не підтримується
        }
        b.bytes = b.bytes[:0]
        b.bitIndex = 0
    }
}
```

### 🔍 Обмеження seek для non-seekable джерел:

```
Для io.Reader без io.Seeker (напр. мережевий потік):
• seek(pos) працює тільки для pos=0 (rewind)
• Це обмеження архітектури: неможливо "перемотати" мережевий потік

Для io.ReadSeeker (напр. файл):
• seek(pos) використовує Seek() для точного переміщення
• Буфер очищається бо дані після seek можуть бути іншими
• bitIndex скидається бо позиція у бітах більше не валідна

Приклад використання:
  // Для файлу:
  buffer.seek(1024)  // перехід до байта 1024
  
  // Для мережі:
  buffer.seek(0)  // тільки rewind підтримується
  buffer.seek(100)  // ігнорується, позиція не змінюється
```

### 🔧 tell() — отримання поточної позиції:

```go
func (b *Buffer) tell() int {
    if b.reader != nil && b.totalSize > 0 {
        // Seekable: позиція = offset файлу + непрочитані байти у буфері
        seeker := b.reader.(io.Seeker)
        off, _ := seeker.Seek(0, io.SeekCurrent)  // поточна позиція файлу
        return int(off) + (b.bitIndex >> 3) - len(b.bytes)
    }
    // Non-seekable: тільки позиція у буфері
    return b.bitIndex >> 3
}
```

### 🔍 Формула для seekable джерел:

```
tell() = file_offset + buffered_unread_bytes

Де:
  • off = seeker.Seek(0, SeekCurrent) = позиція у файлі після останнього читання
  • (bitIndex >> 3) = байтова позиція у буфері
  • len(b.bytes) = загальний розмір буфера
  • (bitIndex>>3) - len(bytes) = від'ємне число = непрочитані байти у буфері

Приклад:
  • Файл: 10000 байт прочитано з диску
  • Буфер: [A B C D E F], bitIndex=20 (2.5 байта прочитано з буфера)
  • off = 10000, len(bytes)=6, bitIndex>>3=2
  • tell() = 10000 + (2 - 6) = 10000 - 4 = 9996
  
  Це правильно: 9996 байт прочитано загалом (10000 з файлу - 4 ще у буфері)
```

### ✅ Ваш use-case**: реалізація random access для індексованого медіа

```go
// IndexedMediaReader — читач з підтримкою random access через індекс
type IndexedMediaReader struct {
    buffer *mpeg.Buffer
    index  []FrameIndex  // індекс кадрів: {byteOffset, time, duration}
}

type FrameIndex struct {
    ByteOffset int
    Time       float64
    Duration   float64
}

func (i *IndexedMediaReader) SeekToTime(targetTime float64) error {
    // Бінарний пошук у індексі для знаходження найближчого кадру
    frameIdx := binarySearchFrame(i.index, targetTime)
    if frameIdx < 0 {
        return fmt.Errorf("time %f out of range", targetTime)
    }
    
    // Seek до байтового зміщення кадру
    if err := i.buffer.seek(i.index[frameIdx].ByteOffset); err != nil {
        return fmt.Errorf("seek to %d: %w", i.index[frameIdx].ByteOffset, err)
    }
    
    // Синхронізація до початку кадру (на випадок неточності індексу)
    if !i.buffer.findFrameSync() {
        return fmt.Errorf("failed to resync after seek")
    }
    
    return nil
}

// binarySearchFrame — бінарний пошук у індексі кадрів
func binarySearchFrame(index []FrameIndex, targetTime float64) int {
    left, right := 0, len(index)-1
    bestMatch := -1
    
    for left <= right {
        mid := (left + right) / 2
        frame := index[mid]
        
        if frame.Time <= targetTime && targetTime < frame.Time+frame.Duration {
            return mid  // точний збіг
        }
        
        if frame.Time < targetTime {
            bestMatch = mid  // запам'ятовуємо як кандидат
            left = mid + 1
        } else {
            right = mid - 1
        }
    }
    
    return bestMatch  // найближчий кадр до цільового часу
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Streaming decoder з автоматичним завантаженням

```go
// StreamingDecoder — декодер для потокового медіа з мережі
type StreamingDecoder struct {
    buffer   *mpeg.Buffer
    decoder  MediaDecoder  // інтерфейс для конкретного кодека
    conn     net.Conn
    done     chan struct{}
    mu       sync.Mutex
}

func NewStreamingDecoder(conn net.Conn, codec CodecType) (*StreamingDecoder, error) {
    // Створення буфера з network reader
    buffer, err := mpeg.NewBuffer(conn)
    if err != nil {
        return nil, fmt.Errorf("create buffer: %w", err)
    }
    
    // Налаштування callback для автоматичного завантаження
    buffer.SetLoadCallback(func(b *mpeg.Buffer) {
        // Non-blocking read з мережі
        b.LoadReaderCallback(b)
    })
    
    // Створення декодера залежно від кодека
    var decoder MediaDecoder
    switch codec {
    case CodecMP2:
        decoder = mpeg.NewAudio(buffer)
    // ... інші кодеки ...
    default:
        return nil, fmt.Errorf("unsupported codec: %v", codec)
    }
    
    return &StreamingDecoder{
        buffer:  buffer,
        decoder: decoder,
        conn:    conn,
        done:    make(chan struct{}),
    }, nil
}

func (s *StreamingDecoder) Start(ctx context.Context) (<-chan MediaSamples, error) {
    samplesChan := make(chan MediaSamples, 10)  // буфер для 10 кадрів
    
    go func() {
        defer close(samplesChan)
        
        for {
            select {
            case <-ctx.Done():
                return
            case <-s.done:
                return
            default:
                // Декодування кадру
                samples := s.decoder.Decode()
                if samples == nil {
                    if s.buffer.HasEnded() {
                        return  // нормальне завершення
                    }
                    // Немає достатньо даних → очікування
                    time.Sleep(10 * time.Millisecond)
                    continue
                }
                
                // Відправка у канал
                select {
                case samplesChan <- samples:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()
    
    return samplesChan, nil
}

func (s *StreamingDecoder) Close() error {
    close(s.done)
    return s.conn.Close()
}

// Використання:
conn, err := net.Dial("tcp", "streaming.example.com:8080")
if err != nil { /* handle error */ }

decoder, err := NewStreamingDecoder(conn, CodecMP2)
if err != nil { /* handle error */ }

samplesChan, err := decoder.Start(context.Background())
if err != nil { /* handle error */ }

for samples := range samplesChan {
    // Обробка медіа семплів
    processMedia(samples)
}
```

### 🔧 Приклад: Файловий reader з індексацією

```go
// IndexedFileReader — файловий читач з попередньою індексацією
type IndexedFileReader struct {
    file    *os.File
    buffer  *mpeg.Buffer
    decoder MediaDecoder
    index   []FrameIndex
}

func NewIndexedFileReader(filename string, codec CodecType) (*IndexedFileReader, error) {
    file, err := os.Open(filename)
    if err != nil {
        return nil, err
    }
    
    buffer, err := mpeg.NewBuffer(file)
    if err != nil {
        file.Close()
        return nil, err
    }
    
    reader := &IndexedFileReader{
        file:   file,
        buffer: buffer,
    }
    
    // Попередня індексація файлу
    if err := reader.buildIndex(codec); err != nil {
        file.Close()
        return nil, fmt.Errorf("build index: %w", err)
    }
    
    // Ініціалізація декодера після індексації
    switch codec {
    case CodecMP2:
        reader.decoder = mpeg.NewAudio(reader.buffer)
    // ... інші кодеки ...
    }
    
    return reader, nil
}

func (r *IndexedFileReader) buildIndex(codec CodecType) error {
    // Тимчасовий буфер для сканування
    scanBuf := make([]byte, 4096)
    var frames []FrameIndex
    var currentTime float64
    
    // Сканування файлу для пошуку кадрів
    for {
        n, err := r.file.Read(scanBuf)
        if n == 0 && err != nil {
            if err == io.EOF {
                break
            }
            return err
        }
        
        // Пошук sync кодів у прочитаному буфері (залежить від кодека)
        switch codec {
        case CodecMP2:
            for i := 0; i < n-1; i++ {
                if scanBuf[i] == 0xFF && (scanBuf[i+1]&0xFE) == 0xFC {
                    // Знайдено потенційний кадр
                    offset, _ := r.file.Seek(0, io.SeekCurrent)
                    frameOffset := int(offset) - n + i
                    
                    // Розрахунок тривалості кадру (спрощено)
                    duration := 1152.0 / 48000.0  // 1152 семпли @ 48kHz = 24ms
                    
                    frames = append(frames, FrameIndex{
                        ByteOffset: frameOffset,
                        Time:       currentTime,
                        Duration:   duration,
                    })
                    
                    currentTime += duration
                    i += 3  // пропуск sync word для наступного пошуку
                }
            }
        // ... інші кодеки ...
        }
    }
    
    r.index = frames
    
    // Повернення на початок файлу
    _, err := r.file.Seek(0, io.SeekStart)
    return err
}

func (r *IndexedFileReader) SeekToTime(targetTime float64) error {
    frameIdx := binarySearchFrame(r.index, targetTime)
    if frameIdx < 0 {
        return fmt.Errorf("time out of range")
    }
    
    // Seek до кадру
    if err := r.buffer.seek(r.index[frameIdx].ByteOffset); err != nil {
        return err
    }
    
    // Синхронізація
    if !r.buffer.findFrameSync() {
        return fmt.Errorf("resync failed")
    }
    
    return nil
}

func (r *IndexedFileReader) Duration() float64 {
    if len(r.index) == 0 {
        return 0
    }
    last := r.index[len(r.index)-1]
    return last.Time + last.Duration
}

// Використання:
reader, err := NewIndexedFileReader("audio.mp2", CodecMP2)
if err != nil { /* handle error */ }

// Seek до 30 секунди
if err := reader.SeekToTime(30.0); err != nil { /* handle error */ }

// Декодування з нової позиції
for {
    samples := reader.decoder.Decode()
    if samples == nil {
        break
    }
    processMedia(samples)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Переповнення буфера при discardRead=false** | Витрата пам'яті зростає для довгих потоків | Увімкніть `discardRead=true` або реалізуйте обмеження розміру буфера |
| **Зависання у findFrameSync() для пошкоджених даних** | Декодер не реагує на нові дані | Додайте обмеження на кількість ітерацій пошуку або таймаут |
| **Некоректне tell() для seekable джерел** | Позиція не співпадає з очікуванням | Перевірте формулу: `off + (bitIndex>>3) - len(bytes)` |
| **Race condition у discardReadBytes()** | Пошкоджені дані при копіюванні | Використовуйте тимчасовий буфер для перекритих копій |
| **Втрата даних при seek для non-seekable** | Помилки при спробі seek у мережевий потік | Перевіряйте `Seekable()` перед викликом seek() |

---

## ⚡ Оптимізації для high-performance буферизації

### 1. Reuse буферів для завантаження:

```go
var loadBufferPool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, mpeg.BufferSize)
        return &buf
    },
}

func getLoadBuffer() *[]byte {
    return loadBufferPool.Get().(*[]byte)
}

func putLoadBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    loadBufferPool.Put(b)
}

// Використання у LoadReaderCallback:
func (b *Buffer) LoadReaderCallbackOptimized(buffer *Buffer) {
    if buffer.hasEnded {
        return
    }
    
    tmp := getLoadBuffer()
    defer putLoadBuffer(tmp)
    
    n, err := io.ReadFull(buffer.reader, *tmp)
    // ... обробка як у оригіналі ...
}
```

### 2. SIMD-оптимізація пошуку sync кодів:

```go
//go:build amd64 && !nosimd

package mpeg

import "golang.org/x/sys/cpu"

func findFrameSyncSIMD(bytes []byte, start int) int {
    if cpu.X86.HasAVX2 {
        return findFrameSyncAVX2(bytes, start)
    }
    return findFrameSyncScalar(bytes, start)
}

// findFrameSyncAVX2 — AVX2-оптимізований пошук (псевдокод)
func findFrameSyncAVX2(bytes []byte, start int) int {
    // Використання 256-бітних регістрів для пошуку 0xFF
    // ... реалізація з intrinsics ...
    return -1  // не знайдено
}
```

### 3. Моніторинг продуктивності буфера:

```go
type BufferMetrics struct {
    BytesRead prometheus.CounterVec
    LoadLatency prometheus.HistogramVec
    SyncSearches prometheus.CounterVec
    BufferSize prometheus.GaugeVec
}

func (m *BufferMetrics) RecordRead(n int, duration time.Duration) {
    m.BytesRead.WithLabelValues("total").Add(float64(n))
    m.LoadLatency.Observe(duration.Seconds())
}

func (m *BufferMetrics) RecordSyncSearch(found bool) {
    m.SyncSearches.WithLabelValues(
        map[bool]string{true: "found", false: "not_found"}[found]).Inc()
}

func (m *BufferMetrics) RecordBufferSize(size int) {
    m.BufferSize.Set(float64(size))
}
```

---

## 📋 Чек-лист безпечного використання Buffer

```go
// ✅ 1. Валідація вхідного reader перед створенням буфера
if r == nil {
    return nil, fmt.Errorf("reader cannot be nil")
}

// ✅ 2. Перевірка Seekable() перед викликом seek()
if !buffer.Seekable() && pos != 0 {
    return fmt.Errorf("seek only supported for rewind on non-seekable sources")
}

// ✅ 3. Обмеження пошуку синхронізації для уникнення зависання
const maxSyncSearchBytes = 64 * 1024
if !buffer.findFrameSyncLimited(maxSyncSearchBytes) {
    return fmt.Errorf("sync not found within %d bytes", maxSyncSearchBytes)
}

// ✅ 4. Безпечне копіювання у discardReadBytes()
if bytePos > unread {
    // Використання тимчасового буфера для перекритих копій
    tmp := make([]byte, unread)
    copy(tmp, b.bytes[bytePos:])
    copy(b.bytes, tmp)
} else {
    copy(b.bytes, b.bytes[bytePos:])
}

// ✅ 5. Перевірка hasEnded перед очікуванням даних
if buffer.HasEnded() && !buffer.has(count) {
    return io.EOF  // нормальне завершення
}

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Buffer state: bitIndex=%d, len=%d, remaining=%d, hasEnded=%v", 
    buffer.bitIndex, len(buffer.bytes), buffer.Remaining(), buffer.HasEnded())

// ✅ 7. Метрики для моніторингу
metrics.RecordRead(n, time.Since(start))
metrics.RecordBufferSize(len(buffer.bytes))
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 11172-3:1993 (MPEG-1 Audio)](https://www.iso.org/standard/22412.html) — офіційний стандарт
- 📄 [Bitstream Parsing Techniques](https://en.wikipedia.org/wiki/Bitstream) — теорія бітового парсингу
- 📄 [Go io.Reader Documentation](https://pkg.go.dev/io#Reader) — інтерфейси для потокового читання
- 🧪 [Go sync.Pool Documentation](https://pkg.go.dev/sync#Pool) — ефективне управління пам'яттю
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Увімкніть `discardRead=true` для потокових джерел** — уникнення витрати пам'яті.
> 2. **Обмежуйте пошук синхронізації** — запобігання зависанням на пошкоджених даних.
> 3. **Перевіряйте `Seekable()` перед seek()** — уникнення помилок для мережевих потоків.
> 4. **Використовуйте тимчасовий буфер для перекритих копій** — запобігання race conditions.
> 5. **Моніторьте `BufferSize` метрику** — виявлення проблем з буферизацією у реальному часі.

Потрібен приклад реалізації повного циклу streaming decoder з адаптивною буферизацією, або інтеграція цього буфера з вашим медіа-пайплайном для WebSocket стрімінгу? Готовий допомогти! 🚀