# 📡 Глибокий розбір: Teletext Parser в astisub

Цей файл — **ядро парсингу DVB Teletext субтитрів** у бібліотеці `astisub`. Він перетворює сирий TS-потік у структуровані субтитри. Розберемо архітектуру, потік даних та інтеграцію у ваш проект.

---

## 🗺️ Архітектурна схема

```
TS Потік (MPEG-TS)
       │
       ▼
┌─────────────────┐
│ astits.Demuxer  │ ← Розпаковує PES-пакети
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ teletextPID()   │ ← Визначає PID телетексту (з PMT або опцій)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Page Buffer     │ ← Збирає пакети в сторінки (state machine)
│ • parseDataUnit │ • Hamming 8/4 декодування
│ • parsePacket   │ • Фільтрація за magazine/page
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Character Decoder│ ← Байт → UTF-8 з урахуванням:
│ • tripletX28/M29│ • G0/G2 набори
│ • national subset│ • Control codes (колір, розмір)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Subtitles       │ ← Готовий результат: []Item з текстом,
│ • Items[]       │   таймінгами та стилями
└─────────────────┘
```

---

## ⚙️ Ключові компоненти

### 1. **TeletextOptions — конфігурація**

```go
type TeletextOptions struct {
    Page int  // Номер сторінки (напр. 888 для субтитрів)
    PID  int  // PID потоку (якщо відомо, інакше авто-детект)
}
```

**Приклад:**
```go
opts := TeletextOptions{
    Page: 888,  // Стандартна сторінка субтитрів
    PID:  0,    // 0 = авто-пошук через PMT
}
```

---

### 2. **ReadFromTeletext — точка входу**

```go
func ReadFromTeletext(r io.Reader, o TeletextOptions) (*Subtitles, error)
```

**Що робить:**
1. Створює `astits.Demuxer` для читання TS-пакетів
2. Визначає PID телетексту (`teletextPID`)
3. Ініціалізує `teletextPageBuffer` та `teletextCharacterDecoder`
4. Циклічно читає PES-пакети:
   - Фільтрує за PID та `StreamIDPrivateStream1`
   - Витягує таймінги (PTS/PCR)
   - Передає дані в `pageBuffer.process()`
5. Після завершення — `dump()` залишкових сторінок
6. Парсить зібрані сторінки в `Subtitles`

**Важливо:** Функція **блокуюча** — читає до кінця `io.Reader`. Для стрімінгу потрібен адаптер.

---

### 3. **PID Detection (`teletextPID`)**

```go
// Логіка пошуку PID:
if o.PID > 0 {
    return o.PID  // Явно вказано
}
// Інакше — скануємо потік до PMT:
for {
    d, _ := dmx.NextData()
    if d.PMT != nil {
        // Шукаємо дескриптори Teletext/VBITeletext
        for _, stream := range d.PMT.ElementaryStreams {
            for _, desc := range stream.ElementaryStreamDescriptors {
                if desc.Tag == DescriptorTagTeletext {
                    return stream.ElementaryPID
                }
            }
        }
    }
}
```

**Для вашого HLS-процесора:** Якщо ви отримуєте вже відфільтрований телетекст-потік, передавайте `PID` явно, щоб уникнути зайвого сканування.

---

### 4. **Page Buffer — State Machine**

```go
type teletextPageBuffer struct {
    cd             *teletextCharacterDecoder
    currentPage    *teletextPage      // Поточна сторінка в зборі
    donePages      []*teletextPage    // Готові сторінки
    magazineNumber uint8              // Журнал (0-7, 8)
    pageNumber     int                // Номер сторінки (0-99)
    receiving      bool               // Статус прийому
}
```

#### 🔄 Потік обробки пакета:

```
PES Data Unit
     │
     ▼
┌─────────────────┐
│ parseDataUnit() │
│ • framingCode=0xe4? │
│ • Hamming 8/4 decode │
│ • magazine/packet № │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ parsePacket()   │
│ • packet 0 → header │
│ • packet 1-25 → дані рядків │
│ • packet 28/29 → triplet (charset) │
│ • packet 30 → розширені опції │
└────────┬────────┘
```

#### 🔑 Критичні моменти:

**Hamming 8/4 декодування** (захист від помилок):
```go
h1, ok := astikit.ByteHamming84Decode(i[2])  // 4 біти даних + 4 біти ECC
h2, ok := astikit.ByteHamming84Decode(i[3])
magazineNumber := (h2<<4 | h1) & 0x7  // 3 біти = 0-7
if magazineNumber == 0 { magazineNumber = 8 }
```

**Фільтрація сторінок:**
```go
// Приймаємо тільки якщо:
// 1. magazineNumber == b.magazineNumber
// 2. packetNumber в діапазоні 1-25 (дані рядків)
// 3. b.receiving == true (після заголовка)
if b.receiving && magazineNumber == b.magazineNumber && packetNumber >= 1 && packetNumber <= 25 {
    b.parsePacketData(i, packetNumber)
}
```

---

### 5. **Character Decoder — байт → текст**

```go
type teletextCharacterDecoder struct {
    c                   teletextCharset      // Поточна таблиця 96 символів
    lastPageCharsetCode *uint8               // Останній код кодування
    tripletM29          *uint32              // Triplet з packet 29
    tripletX28          *uint32              // Triplet з packet 28
}
```

#### 🔄 Логіка оновлення charset:

```go
func (d *teletextCharacterDecoder) updateCharset(pageCharsetCode *uint8, force bool) {
    // 1. Отримуємо triplet (пріоритет: X28 > M29)
    var triplet uint32
    if d.tripletX28 != nil { triplet = *d.tripletX28 }
    else if d.tripletM29 != nil { triplet = *d.tripletM29 }
    
    // 2. Шукаємо в teletextCharsets[triplet1][nationalOption]
    if v1, ok := teletextCharsets[uint8((triplet&0x3f80)>>10)]; ok {
        if v2, ok := v1[*pageCharsetCode]; ok {
            d.c = *v2.g0  // Основний набір
            nationalOptionSubset = v2.national
        }
    }
    
    // 3. Застосовуємо national subset (перезапис 13 позицій)
    if nationalOptionSubset != nil {
        for k, v := range nationalOptionSubset {
            d.c[teletextNationalSubsetCharactersPositionInG0[k]] = v
        }
    }
}
```

#### 🔤 Декодування символу:

```go
func (d *teletextCharacterDecoder) decode(i byte) []byte {
    if i < 0x20 { return []byte{} }  // Control codes
    return d.c[i-0x20]  // Індексація в таблиці 96 символів
}
```

---

### 6. **Style Attributes — кольори, розмір, вирівнювання**

Teletext передає стилі **вбудовано в потік символів** через control codes:

| Байт | Дія | Приклад |
|------|-----|---------|
| `0x00-0x07` | Колір тексту | `0x01` = червоний |
| `0x0a` | Приховати текст | До `0x0b` |
| `0x0b` | Показати текст | |
| `0x0c` | Скинути розмір | |
| `0x0d` | Подвійна висота | |
| `0x0e` | Подвійна ширина | |
| `0x0f` | Подвійний розмір (2×2) | |

**Обробка в `parseTeletextRow`:**
```go
for _, v := range row {
    switch v {
    case 0x01: color = ColorRed
    case 0x0d: doubleHeight = astikit.BoolPtr(true)
    // ...
    }
    
    // Якщо стиль змінився — розбиваємо LineItem
    if color != li.InlineStyle.TeletextColor || ... {
        appendTeletextLineItem(&l, li, s)  // Закриваємо попередній
        li = LineItem{InlineStyle: &StyleAttributes{...}}  // Новий
    }
    
    // Додаємо текст
    li.Text += string(d.decode(v))
}
```

**Результат:** Кожен `LineItem` має власні `StyleAttributes`:
```go
type StyleAttributes struct {
    TeletextColor        *Color
    TeletextDoubleHeight *bool
    TeletextDoubleWidth  *bool
    TeletextSpacesBefore *int  // Для вирівнювання
    // ...
}
```

---

## 🚀 Інтеграція у ваш CCTV HLS Processor

### Сценарій: Real-time парсинг телетексту з сегментів

```go
// 1. Адаптер для потокової обробки (не блокує до кінця файлу)
type TeletextStreamParser struct {
    decoder  *teletextCharacterDecoder
    buffer   *teletextPageBuffer
    callback func(*SubtitleMessage)  // Ваш WebSocket sender
}

func NewTeletextStreamParser(page int, cb func(*SubtitleMessage)) *TeletextStreamParser {
    return &TeletextStreamParser{
        decoder:  newTeletextCharacterDecoder(),
        buffer:   newTeletextPageBuffer(page, newTeletextCharacterDecoder()),
        callback: cb,
    }
}

// 2. Обробка одного TS-сегменту (напр. 4-10 секунд)
func (p *TeletextStreamParser) ProcessSegment(data []byte, segmentNum uint64) error {
    reader := bytes.NewReader(data)
    dmx := astits.NewDemuxer(context.Background(), reader)
    
    for {
        d, err := dmx.NextData()
        if err == astits.ErrNoMorePackets { break }
        if err != nil { return err }
        
        if d.PES == nil || d.PID != yourTeletextPID { continue }
        
        t := teletextDataTime(d)  // PTS/PCR таймінг
        pages := p.buffer.process(d.PES, t)
        
        for _, page := range pages {
            // Конвертуємо в ваш формат
            msg := p.pageToSubtitleMessage(page, segmentNum)
            p.callback(msg)  // Відправка у WebSocket
        }
    }
    return nil
}

// 3. Конвертація в SubtitleMessage (ваш формат)
func (p *TeletextStreamParser) pageToSubtitleMessage(page *teletextPage, segmentNum uint64) *SubtitleMessage {
    p.decoder.updateCharset(astikit.UInt8Ptr(page.charsetCode), false)
    
    var arabicText, englishText strings.Builder
    sort.Ints(page.rows)
    
    for _, rowIdx := range page.rows {
        row := page.data[uint8(rowIdx)]
        for _, b := range row {
            if b >= 0x20 {
                utf8 := p.decoder.decode(b)
                // Тут можна додати логіку розпізнавання мови
                arabicText.WriteString(string(utf8))
            }
        }
        arabicText.WriteString("\n")
    }
    
    // Розрахунок таймінгів відносно початку стріму
    startTime := page.start.Sub(streamStartTime)
    endTime := page.end.Sub(streamStartTime)
    
    return &SubtitleMessage{
        Seq:        segmentNum,
        TimeStart:  startTime.Milliseconds(),
        TimeEnd:    endTime.Milliseconds(),
        StartTimeUTC: page.start.UTC().Format(time.RFC3339),
        Arabic:     arabicText.String(),
        // English/Russian — через ваш NLLB pipeline
        VideoSource: nil,  // Заповнюється в segmentAssembler
    }
}
```

### ⚡ Оптимізації для вашого use-case:

1. **Кешування charset**:
```go
// Замість пошуку в teletextCharsets для кожного байта:
type CachedCharset struct {
    triplet1 uint8
    option   uint8
    charset  teletextCharset
}
// Зберігайте останній активний charset на рівні каналу
```

2. **Channel-aware ізоляція**:
```go
// У вашому ChannelConfig:
type ChannelConfig struct {
    ChannelID string
    TeletextPID uint16
    TeletextPage int
    CharsetCache *sync.Map  // triplet1→cached charset
}
```

3. **Backpressure-безпечна відправка**:
```go
// У callback для WebSocket:
func (s *WSSender) SendSubtitle(msg *SubtitleMessage) bool {
    select {
    case s.send <- msg:
        return true
    case <-time.After(100 * time.Millisecond):
        // Клієнт не встигає — логуємо, але не блокуємо
        log.Warn("subtitle dropped", "seq", msg.Seq, "channel", s.channelID)
        return false
    }
}
```

4. **Обробка розривів у потоці**:
```go
// Якщо між сегментами >1с розрив:
if !lastPTS.IsZero() && pts.Sub(lastPTS) > time.Second {
    log.Warn("teletext PTS gap", "gap", pts.Sub(lastPTS), "channel", channelID)
    // Скидаємо buffer, щоб уникнути "залипання" старої сторінки
    p.buffer = newTeletextPageBuffer(p.pageNumber, p.decoder)
}
```

---

## 🐞 Потенційні проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Немає субтитрів** | Пустий результат | Перевірте `PID` через `ffprobe -show_streams input.ts` |
| **Крокозябри замість тексту** | Неправильний charset | Логувати `triplet1` та `charsetCode`, порівняти зі стандартом |
| **Пропуски в тексті** | Помилки Hamming декодування | Додати fallback: якщо `!ok` — спробувати сусідні значення |
| **Затримка субтитрів** | Буферизація сторінок | Викликати `dump()` кожні N секунд, навіть без завершення сторінки |
| **Пам'ять росте** | Накопичення `donePages` | Очищати `donePages` після обробки, не чекати кінця потоку |

---

## 📋 Чек-лист інтеграції

```go
// ✅ 1. Визначити PID телетексту
// ffprobe -show_streams your_stream.ts | grep -i teletext
// або отримати з вашого videoQueue metadata

// ✅ 2. Налаштувати TeletextOptions
opts := TeletextOptions{
    Page: 888,  // або з конфігурації каналу
    PID:  detectedPID,
}

// ✅ 3. Створити потоковий парсер (не ReadFromTeletext!)
parser := NewTeletextStreamParser(opts.Page, wsSender.Broadcast)

// ✅ 4. Підключити до segmentFinalizer
// Після отримання TS-сегменту:
go func(segment TSChunk) {
    if err := parser.ProcessSegment(segment.Data, segment.SeqNum); err != nil {
        log.Error("teletext parse failed", "err", err, "seq", segment.SeqNum)
    }
}(segment)

// ✅ 5. Додати метрики
monitoring.TeletextProcessed.Inc()
monitoring.TeletextLatency.Observe(time.Since(segment.ReceivedAt).Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [ETSI EN 300 472](https://www.etsi.org/deliver/etsi_en/300400_300499/300472/01.03.01_60/en_300472v010301p.pdf) — Teletext spec
- 📄 [ETSI EN 300 706](https://www.etsi.org/deliver/etsi_en/300700_300799/300706/02.01.01_60/en_300706v020101p.pdf) — Enhanced Teletext
- 💻 [astisub GitHub](https://github.com/asticode/go-astisub) — вихідний код
- 🧪 [astits](https://github.com/asticode/go-astits) — TS demuxer, який використовується всередині

---

> 💡 **Ключова рекомендація**: Оскільки `ReadFromTeletext` очікує `io.Reader` до EOF, для real-time HLS **не використовуйте її напряму**. Замість цього — скопіюйте логіку `pageBuffer.process()` та `parsePacket*` у ваш потоковий обробник, як показано вище. Це дозволить обробляти сегменти по мірі надходження без затримок.

Потрібен приклад адаптації під ваш `segmentAssembler` з orphan audio/video синхронізацією? Готовий допомогти! 🛠️