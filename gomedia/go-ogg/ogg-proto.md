# 📦 Глибокий розбір `ogg/page.go` — парсинг Ogg сторінок та структура потоку

Це **фундаментальний шар абстракції** для роботи з форматом Ogg у вашому CCTV HLS Processor. Файл визначає структури даних та реалізує парсинг заголовків Ogg сторінок згідно специфікації RFC 3533. Розберемо архітектурно:

---

## 🧱 1. Архітектура Ogg: від сторінки до пакету

### 🔍 Ієрархія даних у Ogg:

```
[Ogg потік]
├─ Сторінка (Page) — атомна одиниця передачі
│  ├─ Заголовок (27+ байт): magic, granule, serial, seq, тощо
│  ├─ Таблиця сегментів (0-255 байт): довжини сегментів
│  └─ Payload: дані сегментів
│
├─ Сегмент — одиниця пакування (0-255 байт)
│  ├─ < 255 байт: останній сегмент пакету
│  └─ = 255 байт: продовження пакету у наступному сегменті
│
└─ Пакет (Packet) — логічна одиниця кодеку
   ├─ Може складатися з кількох сегментів
   ├─ Може розриватися між сторінками
   └─ Містить заголовок кодеку або медіа-дані
```

### 🔑 Чому така структура?

| Рівень | Призначення | Вигода для CCTV |
|--------|-------------|----------------|
| **Сторінка** | Синхронізація, корекція помилок, детекція розривів | Стійкість до втрат пакетів у ненадійних мережах |
| **Сегмент** | Гнучке пакування даних різного розміру | Ефективне використання bandwidth для змінних кадрів |
| **Пакет** | Логічна одиниця для кодеку (Opus frame, VP8 frame) | Проста інтеграція з декодерами без додаткового парсингу |

---

## 🔍 2. Заголовок Ogg сторінки: бітова структура

### 🔧 Специфікація (RFC 3533, Section 4):

```
Байти 0-3:  Capture pattern = 0x4F676753 ("OggS") ← завжди!
Байт   4:   Stream structure version (завжди 0)
Байт   5:   Header type flags (3 біти):
            ├─ Біт 0: continuation (1=продовжує пакет з попередньої сторінки)
            ├─ Біт 1: bos (1=beginning of stream, перша сторінка)
            └─ Біт 2: eos (1=end of stream, остання сторінка)
Байти 6-13: Granule position (uint64 LE) — часові мітки кодеку
Байти 14-17: Bitstream serial number (uint32 LE) — унікальний ID потоку
Байти 18-21: Page sequence number (uint32 LE) — послідовний номер сторінки
Байти 22-25: CRC32 checksum (uint32 LE) — цілісність заголовку+сегментів
Байт   26:   Number of segments (uint8) — кількість записів у таблиці (0-255)
Байти 27+:  Segment table — масив довжин сегментів (по 1 байту кожен)
            └─ Загальна довжина payload = sum(segment_table)
```

### 🔧 Структура Go `oggPage`:

```go
type oggPage struct {
    // Метадані заголовку
    version          byte    // версія структури (завжди 0)
    isContinuePacket bool    // біт 0: продовження пакету
    isFirstPage      bool    // біт 1: початок потоку (bos)
    eos              bool    // біт 2: кінець потоку (eos)
    
    // Ідентифікація та синхронізація
    granulePos       uint64  // granule position: часові мітки для кодеку
    streamId         uint32  // serial number: унікальний ID потоку у мультиплексі
    pageSeq          uint32  // послідовний номер сторінки (для детекції розривів)
    checkSum         uint32  // CRC32 для валідації цілісності
    
    // Пакування даних
    segmentsCount    uint8        // кількість сегментів (0-255)
    seqmentTable     [255]byte    // ⚠️ Опечатка: має бути segmentTable
    payloadLen       uint16       // загальна довжина payload = sum(segment_table)
    
    // Зібрані дані
    packets          [][]byte  // зібрані пакети з сегментів цієї сторінки
    cache            []byte    // буфер для збірки пакетів між сторінками
}
```

### ⚠️ Критична опечатка: `seqmentTable` → `segmentTable`

```go
seqmentTable     [255]byte  // ← опечатка у назві поля!
// Це ламає консистентність коду, ускладнює пошук та автодоповнення в IDE
// Має бути:
segmentTable     [255]byte
```

---

## 🔧 3. `readPage()` — парсинг заголовку сторінки

### 🔍 Кроки парсингу:

#### Крок 1: Перевірка magic bytes

```go
if !bytes.Equal(data[:4], CapturePattern[:]) {
    return nil, errors.New("capture pattern not found")
}
// CapturePattern = [4]byte{'O','g','g','S'} = 0x4F676753
// Це гарантує, що ми читаємо валідну Ogg сторінку, а не сміття
```

#### Крок 2: Читання базових полів

```go
data = data[4:]  // пропустити magic
page.version = data[0]  // завжди 0 у поточній специфікації

// Парсинг прапорців з байту 1 (битова маска)
if data[1]&0x01 > 0 { page.isContinuePacket = true }   // біт 0
if data[1]&0x02 > 0 { page.isFirstPage = true }         // біт 1 (bos)
if data[1]&0x04 > 0 { page.eos = true }                 // біт 2 (eos)
```

#### Крок 3: Читання числових полів (Little Endian)

```go
page.granulePos = binary.LittleEndian.Uint64(data[2:])   // байти 2-9
page.streamId   = binary.LittleEndian.Uint32(data[10:])  // байти 10-13
page.pageSeq    = binary.LittleEndian.Uint32(data[14:])  // байти 14-17
page.checkSum   = binary.LittleEndian.Uint32(data[18:])  // байти 18-21
page.segmentsCount = data[22]                             // байт 22
```

#### Крок 4: Копіювання таблиці сегментів

```go
copy(page.seqmentTable[:], data[23:23+int(page.segmentsCount)])
// ⚠️ Немає перевірки: чи достатньо даних у буфері?
// Якщо page.segmentsCount > len(data)-23 → panic: index out of range!
```

#### Крок 5: Розрахунок загальної довжини payload

```go
page.payloadLen = 0
for i := 0; i < int(page.segmentsCount); i++ {
    page.payloadLen += uint16(page.seqmentTable[i])
}
// payloadLen = сума всіх сегментів = загальна довжина даних після заголовку
```

### 🎯 Чому Little Endian для числових полів?

```
Специфікація Ogg вимагає Little Endian для всіх числових полів:
• granulePos, streamId, pageSeq, checkSum — усі uint32/uint64 у LE

Це історичний вибір формату для сумісності з x86 архітектурою.
У вашому коді: binary.LittleEndian.UintXX() — коректна реалізація.

Важливо: не плутати з Big Endian у інших форматах (наприклад, MPEG-TS)!
```

### ⚠️ Потенційна проблема: відсутня валідація довжини буфера

```go
// У readPage():
copy(page.seqmentTable[:], data[23:23+int(page.segmentsCount)])
// Якщо segmentsCount = 255, а len(data) < 23+255 → panic!

// Краще додати перевірку:
if len(data) < 23+int(page.segmentsCount) {
    return nil, fmt.Errorf("insufficient data for segment table: need %d, have %d", 
        23+int(page.segmentsCount), len(data))
}
```

---

## 🔐 4. CRC32: `crc_table` та `makeChecksum()`

### 🔍 Таблиця CRC32:

```go
var crc_table [256]uint32 = [256]uint32{
    0x00000000, 0x04c11db7, 0x09823b6e, ...  // 256 значень
}
// Це стандартна таблиця для поліному 0x04C11DB7 (CRC-32 MPEG-2)
// Використовується у Ogg, PNG, MPEG-TS та інших форматах
```

### 🔧 Функція `makeChecksum()`:

```go
func makeChecksum(crc uint32, buffer []byte) uint32 {
    var checksum uint32
    for index := range buffer {
        // Бітова операція: зсув + XOR з таблицею
        checksum = (checksum << 8) ^ crc_table[byte(checksum>>24)^buffer[index]]
    }
    return checksum
}
```

### 🎯 Як використовується CRC у Ogg?

```
Специфікація вимагає:
1. Перед розрахунком: встановити поле checksum у заголовку у 0
2. Розрахувати CRC32 для: заголовку (без checksum) + всіх сегментів
3. Записати результат у поле checksum

У вашому коді: readPage() читає checksum, але не перевіряє його!
Це означає, що пошкоджені сторінки можуть пройти парсинг без попередження.

Краще: додати валідацію після readPage():
func (page *oggPage) Validate(data []byte) error {
    // Витягнути заголовок без checksum
    header := make([]byte, len(data))
    copy(header, data)
    binary.LittleEndian.PutUint32(header[18:], 0)  // тимчасово 0
    
    // Розрахувати очікуваний checksum
    expected := makeChecksum(0, append(header, data[27+int(page.segmentsCount):]...))
    
    if expected != page.checkSum {
        return fmt.Errorf("CRC mismatch: expected %08x, got %08x", expected, page.checkSum)
    }
    return nil
}
```

---

## 📦 5. `oggStream` — стан окремого потоку у мультиплексі

### 🔧 Структура:

```go
type oggStream struct {
    currentPage *oggPage        // поточна сторінка для збірки пакетів
    streamId    uint32          // serial number цього потоку
    cid         codec.CodecID   // визначений кодек (або UNRECOGNIZED)
    parser      oggParser       // специфічний парсер для цього кодеку
    lost        int             // прапорець: чи були втрачені сторінки
    cache       []byte          // буфер для збірки пакетів між сторінками
}
```

### 🎯 Роль кожного поля:

| Поле | Призначення | Критичність |
|------|-------------|-------------|
| `currentPage` | Зберігає метадані поточної сторінки для логіки збірки | Висока: без неї неможливо детектувати розриви |
| `streamId` | Унікальний ідентифікатор для розрізнення потоків у мультиплексі | Висока: без нього неможливо маршрутизувати пакети |
| `cid` | Внутрішній ідентифікатор кодеку для диспетчеризації | Середня: встановлюється після детекції заголовку |
| `parser` | Специфічна логіка для цього кодеку (Opus/VP8) | Висока: без нього неможливо розпарсити пакети |
| `lost` | Прапорець для обробки втрачених сторінок | Середня: дозволяє відновлюватися після помилок |
| `cache` | Буфер для збірки пакетів, розрізаних між сторінками | Висока: без нього втрачаються дані на межі сторінок |

### 🎯 Чому `cache` критичний?

```
Ogg дозволяє розривати пакети між сторінками:
[Сторінка 1: сегменти 1-3] [Сторінка 2: сегменти 4-6] → один пакет

Без кешування:
• Сторінка 1: отримали сегменти 1-3, але пакет не завершено → втрата даних
• Сторінка 2: отримали сегменти 4-6, але немає контексту з попередньої → помилка

З кешуванням у oggStream.cache:
• Сторінка 1: додати сегменти 1-3 у cache, чекати продовження
• Сторінка 2: якщо isContinuePacket=true → додати сегменти 4-6 до cache
• Коли сегмент < 255 → пакет завершено → відправити у parser

У вашому коді: cache ініціалізується як make([]byte, 0, 1024) — початкова ємність 1KB для оптимізації.
```

---

## 🐞 6. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Опечатка у назві поля**:
   ```go
   seqmentTable     [255]byte  // ← має бути segmentTable
   // Це ламає консистентність, ускладнює пошук та автодоповнення в IDE
   // Має бути виправлено у всьому проекті через пошук/заміну
   ```

2. **Відсутня валідація довжини буфера у `readPage()`**:
   ```go
   copy(page.seqmentTable[:], data[23:23+int(page.segmentsCount)])
   // Якщо segmentsCount великий, а data короткий → panic: index out of range!
   
   // Краще додати перевірку:
   expectedLen := 23 + int(page.segmentsCount)
   if len(data) < expectedLen {
       return nil, fmt.Errorf("insufficient data for page header: need %d, have %d", 
           expectedLen, len(data))
   }
   ```

3. **CRC не перевіряється після парсингу**:
   ```go
   // readPage() читає page.checkSum, але не валідує його!
   // Пошкоджені сторінки можуть пройти парсинг без попередження
   
   // Краще: додати метод Validate() та викликати його після readPage()
   func (page *oggPage) Validate(data []byte) error {
       // ... розрахунок очікуваного CRC ...
       if expected != page.checkSum {
           return fmt.Errorf("CRC mismatch")
       }
       return nil
   }
   ```

4. **Фіксований розмір `segmentTable [255]byte`**:
   ```go
   // Займає 255 байт навіть якщо segmentsCount = 1
   // Для пам'яті це не критично, але можна оптимізувати:
   segmentTable []byte  // динамічний слайс
   // Ініціалізація: page.segmentTable = make([]byte, page.segmentsCount)
   ```

5. **Відсутня обробка `version != 0`**:
   ```go
   page.version = data[0]
   // Але специфікація: version має бути 0, інші значення зарезервовані
   // Краще додати перевірку:
   if page.version != 0 {
       return nil, fmt.Errorf("unsupported ogg page version: %d", page.version)
   }
   ```

6. **`PrintPage()` використовує `fmt.Printf` замість `io.Writer`**:
   ```go
   // Важко тестувати або перенаправляти вивід
   // Краще: приймати io.Writer як параметр
   func PrintPage(page *oggPage, w io.Writer) {
       fmt.Fprintf(w, "version:%d\n", page.version)
       // ...
   }
   ```

### 💡 Покращення:

```go
// 1. Helper для безпечного читання сегментів
func (page *oggPage) readSegmentTable(data []byte) error {
    expectedLen := 23 + int(page.segmentsCount)
    if len(data) < expectedLen {
        return fmt.Errorf("insufficient data for segment table: need %d, have %d", 
            expectedLen, len(data))
    }
    // Використовувати динамічний слайс замість фіксованого масиву
    page.segmentTable = make([]byte, page.segmentsCount)
    copy(page.segmentTable, data[23:expectedLen])
    return nil
}

// 2. Валідація CRC після парсингу
func (page *oggPage) Validate(headerAndSegments []byte) error {
    if len(headerAndSegments) < 27 {
        return errors.New("data too short for CRC validation")
    }
    
    // Витягнути заголовок без checksum (байти 18-21 = 0)
    header := make([]byte, 27)
    copy(header, headerAndSegments[:27])
    binary.LittleEndian.PutUint32(header[18:], 0)  // тимчасово 0
    
    // Додати сегменти до даних для CRC
    segmentsStart := 27 + int(page.segmentsCount)
    dataForCRC := append(header, headerAndSegments[segmentsStart:]...)
    
    expected := makeChecksum(0, dataForCRC)
    if expected != page.checkSum {
        return fmt.Errorf("CRC mismatch: expected %08x, got %08x", expected, page.checkSum)
    }
    return nil
}

// 3. Методи для роботи з прапорцями
func (page *oggPage) IsBeginningOfStream() bool { return page.isFirstPage }
func (page *oggPage) IsEndOfStream() bool       { return page.eos }
func (page *oggPage) ContinuesPreviousPacket() bool { return page.isContinuePacket }

// 4. Юніт-тести для readPage()
func TestReadPage_Valid(t *testing.T) {
    // Створити валідний заголовок сторінки
    data := createValidOggPageHeader()  // helper function
    page, err := readPage(data)
    if err != nil {
        t.Fatalf("readPage failed: %v", err)
    }
    if page.streamId != expectedStreamId {
        t.Errorf("streamId mismatch: got %d, want %d", page.streamId, expectedStreamId)
    }
    // ... інші перевірки ...
}

func TestReadPage_InvalidMagic(t *testing.T) {
    data := []byte("not an ogg page")
    _, err := readPage(data)
    if err == nil || !strings.Contains(err.Error(), "capture pattern") {
        t.Errorf("expected capture pattern error, got: %v", err)
    }
}

func TestReadPage_ShortBuffer(t *testing.T) {
    data := []byte("OggS")  // тільки magic, недостатньо даних
    _, err := readPage(data)
    if err == nil {
        t.Error("expected error for short buffer")
    }
}
```

---

## 🎯 7. Інтеграція з `Demuxer.Input()` — як використовується `oggPage`

### 🔍 Потік даних у головному циклі:

```go
// У Demuxer.Input(), стан DEMUX_PAGE_HEAD:
page, err := readPage(hdr)  // парсинг заголовку
if err != nil { return err }

// Реєстрація/оновлення потоку
stream, found := demuxer.streams[page.streamId]
if !found {
    stream = &oggStream{
        currentPage: page,  // ← зберегти поточну сторінку
        streamId:    page.streamId,
        // ... ініціалізація ...
    }
    demuxer.streams[page.streamId] = stream
}
stream.currentPage = page  // ← оновити посилання

// Перехід у стан DEMUX_PAGE_PAYLOAD для читання даних
```

### 🔍 Збірка пакетів у стані DEMUX_PAGE_PAYLOAD:

```go
// Читання payload сторінки
tmp := ...  // повний payload цієї сторінки

// Збірка пакетів з сегментів за правилами Ogg:
for idx := 0; idx < int(page.segmentsCount); idx++ {
    packetLen += int(page.segmentTable[idx])  // ← використання таблиці
    if page.segmentTable[idx] < 255 {
        // Кінець пакету: сегмент < 255 байт
        packet := tmp[start : start+packetLen]
        page.packets = append(page.packets, packet)  // ← зберегти у сторінці
        start = start + packetLen
        packetLen = 0
    }
    // Якщо сегмент = 255 → продовжити накопичення
}

// Обробка кожного пакету
for _, pkt := range page.packets {
    demuxer.readPacket(stream, pkt)  // ← передача у специфічний парсер
}
```

### 🎯 Чому `page.packets` зберігається у сторінці?

```
Це дозволяє:
1. Інкапсуляцію: логіка збірки пакетів ізольована у readPage()/Input()
2. Повторне використання: можна ітерувати по packets кілька разів
3. Дебаг: PrintPage() може показати зібрані пакети

Альтернатива: відправляти пакети напряму у callback без збереження у page.packets
→ менше пам'яті, але складніше тестувати та дебажити.
```

---

## 🧭 Висновок: чому цей файл — фундамент бінарної сумісності

| Компонент | Роль у Ogg демуксері | Вартість помилки без нього |
|-----------|---------------------|---------------------------|
| **Capture pattern перевірка** | Гарантія, що читаємо валідну Ogg сторінку | Парсинг сміття → помилки, паніки, пошкоджені дані |
| **Little Endian парсинг** | Сумісність зі специфікацією Ogg | Неправильні granule/streamId/pageSeq → розсинхронізація |
| **Таблиця сегментів** | Гнучке пакування пакетів змінного розміру | Неможливість збірки розрізаних пакетів → втрата кадрів |
| **CRC32 таблиця** | Можливість валідації цілісності даних | Пошкоджені сторінки проходять парсинг → артефакти у відтворенні |
| **oggStream з cache** | Збірка пакетів між сторінками | Втрата даних на межі сторінок → обрізані кадри |

> 🔑 **Головна ідея**: Цей файл — **бінарний контракт** між вашим кодом та специфікацією Ogg. Він гарантує, що:
> 1. Кожен байт заголовку парситься згідно стандарту
> 2. Прапорці (bos/eos/continuation) обробляються коректно
> 3. Таблиця сегментів інтерпретується правильно для збірки пакетів
> 4. Метадані (granule, streamId, pageSeq) зберігаються для подальшої логіки

Без нього кожен компонент пайплайну змушений був би реалізовувати власну логіку парсингу заголовків, що призвело б до дублювання коду, помилок у синхронізації та важкої підтримки.

💡 **Фінальна порада**: 
1. Виправте опечатку: `seqmentTable` → `segmentTable` у всьому проекті
2. Додайте валідацію довжини буфера перед доступом до `data[23:23+segmentsCount]`
3. Реалізуйте метод `Validate()` для перевірки CRC32 після парсингу
4. Додайте перевірку `version == 0` для майбутньої сумісності
5. Додайте юніт-тести для `readPage()` з валідними/невалідними вхідними даними

Це перетворить цей файл з "робочої реалізації" на "гарантовано коректний бінарний шар" для всього вашого Ogg/HLS пайплайну.