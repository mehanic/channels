# 📦 Глибокий розбір: `mp4fio` — Фрагментований MP4 (fMP4) для streaming

Цей файл — **реалізація атомів для фрагментованого MP4 (fMP4)**, що використовується для low-latency streaming (DASH, HLS fMP4, CMAF). Він містить структури для `moof` (Movie Fragment), `traf` (Track Fragment), `trun` (Track Fragment Run) та інших атомів, необхідних для інкрементальної генерації та відтворення медіа-потоків.

---

## 🗺️ Архітектурна схема mp4fio

```
┌────────────────────────────────────────┐
│ 📦 mp4fio — Fragmented MP4 Atoms      │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • MovieFrag (moof) — фрагмент фільму  │
│  • TrackFrag (traf) — фрагмент треку   │
│  • TrackFragRun (trun) — таблиця семплів│
│  • TrackFragHeader (tfhd) — параметри  │
│  • TrackFragDecodeTime (tfdt) — базовий час│
│                                         │
│  🔄 Потік даних для streaming:         │
│  [ftyp][moov][moof][mdat][moof][mdat]...│
│                ↑       ↑                │
│           метадані  дані фрагменту     │
│                                         │
│  📡 Використання:                       │
│  • Low-latency HLS/DASH                │
│  • Live broadcasting                   │
│  • Progressive download з seek         │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. MovieFrag (moof) — кореневий атом фрагменту

### 🔧 Структура та призначення:

```go
type MovieFrag struct {
    Header   *MovieFragHeader  // mfhd: номер фрагменту, прапорці
    Tracks   []*TrackFrag      // traf × N: фрагменти треків
    Unknowns []mp4io.Atom      // невідомі атоми для сумісності
    mp4io.AtomPos             // offset, size у файлі
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення |
|------|-----|-------------|
| `Header` | `*MovieFragHeader` | Метадані фрагменту: послідовний номер, прапорці |
| `Tracks` | `[]*TrackFrag` | Масив фрагментів для кожного треку (відео, аудіо) |
| `Unknowns` | `[]mp4io.Atom` | Підтримка майбутніх розширень стандарту |

### 🔧 Методи маршалінгу:

```go
// Marshal — серіалізація у байти з заголовком атому
func (self MovieFrag) Marshal(b []byte) (n int) {
    pio.PutU32BE(b[4:], uint32(mp4io.MOOF))  // запис tag 'moof'
    n += self.marshal(b[8:]) + 8              // запис тіла + заголовок
    pio.PutU32BE(b[0:], uint32(n))            // запис загального розміру
    return
}

// marshal — приватний метод для запису тіла атому
func (self MovieFrag) marshal(b []byte) (n int) {
    if self.Header != nil {
        n += self.Header.Marshal(b[n:])  // рекурсивний виклик
    }
    for _, atom := range self.Tracks {
        n += atom.Marshal(b[n:])         // запис кожного traf
    }
    for _, atom := range self.Unknowns {
        n += atom.Marshal(b[n:])         // запис невідомих атомів
    }
    return
}

// Len — розрахунок розміру атому у байтах
func (self MovieFrag) Len() (n int) {
    n += 8  // заголовок атому (size + tag)
    if self.Header != nil { n += self.Header.Len() }
    for _, atom := range self.Tracks { n += atom.Len() }
    for _, atom := range self.Unknowns { n += atom.Len() }
    return
}
```

### ⚠️ Критична проблема: порожній Unmarshal

```go
func (self *MovieFrag) Unmarshal(b []byte, offset int) (n int, err error) {
    return  // ← ПОВЕРТАЄ (0, nil) БЕЗ ПАРСИНГУ!
}
```

**Наслідки**: Неможливо прочитати fMP4 фрагменти з байтів → неможливий демуксинг потоку.

**✅ Виправлення**: Реалізувати парсинг аналогічно до `mp4io.Movie`:

```go
func (self *MovieFrag) Unmarshal(b []byte, offset int) (n int, err error) {
    (&self.AtomPos).setPos(offset, len(b))  // збереження позиції
    n += 8  // пропуск заголовку атому
    
    for n+8 < len(b) {
        tag := mp4io.Tag(pio.U32BE(b[n+4:]))
        size := int(pio.U32BE(b[n:]))
        if len(b) < n+size {
            err = fmt.Errorf("mp4fio: invalid atom size at offset %d", n+offset)
            return
        }
        
        switch tag {
        case mp4io.MFHD:
            atom := &MovieFragHeader{}
            if _, err = atom.Unmarshal(b[n:n+size], offset+n); err != nil {
                return n, fmt.Errorf("parse mfhd: %w", err)
            }
            self.Header = atom
            
        case mp4io.TRAF:
            atom := &TrackFrag{}
            if _, err = atom.Unmarshal(b[n:n+size], offset+n); err != nil {
                return n, fmt.Errorf("parse traf: %w", err)
            }
            if len(self.Tracks) > 100 {  // захист від зловмисних файлів
                return n, fmt.Errorf("too many track fragments")
            }
            self.Tracks = append(self.Tracks, atom)
            
        default:
            // Збереження невідомих атомів для сумісності
            atom := &mp4io.Dummy{Tag_: tag, Data: b[n : n+size]}
            if _, err = atom.Unmarshal(b[n:n+size], offset+n); err != nil {
                return n, fmt.Errorf("parse unknown atom: %w", err)
            }
            if len(self.Unknowns) > 100 {
                return n, fmt.Errorf("too many unknown atoms")
            }
            self.Unknowns = append(self.Unknowns, atom)
        }
        n += size
    }
    return
}
```

---

## 🔑 2. TrackFragRun (trun) — таблиця семплів у фрагменті

### 🔧 Структура та гнучкість:

```go
type TrackFragRun struct {
    Version          uint8                    // версія формату (0 або 1)
    Flags            uint32                   // бітові прапорці для опціональних полів
    DataOffset       uint32                   // зміщення даних відносно базового
    FirstSampleFlags uint32                   // прапорці для першого семплу
    Entries          []mp4io.TrackFragRunEntry // масив семплів
    mp4io.AtomPos
}

type TrackFragRunEntry struct {
    Duration uint32  // тривалість семплу (опціонально)
    Size     uint32  // розмір даних (опціонально)
    Flags    uint32  // прапорці семплу (опціонально)
    Cts      uint32  // composition offset (опціонально)
}
```

### 🔍 Прапорці (Flags) та їх значення:

```go
const (
    TRUN_DATA_OFFSET        = 0x01  // присутній DataOffset
    TRUN_FIRST_SAMPLE_FLAGS = 0x04  // присутній FirstSampleFlags
    TRUN_SAMPLE_DURATION    = 0x100 // Duration у кожному Entry
    TRUN_SAMPLE_SIZE        = 0x200 // Size у кожному Entry
    TRUN_SAMPLE_FLAGS       = 0x400 // Flags у кожному Entry
    TRUN_SAMPLE_CTS         = 0x800 // Cts у кожному Entry
)
```

### 🔧 Проблема у Marshal: ігнорування прапорців

```go
// У вихідному коді:
//if flags&TRUN_SAMPLE_DURATION != 0 {  // ← ЗАКОМЕНТОВАНО!
pio.PutU32BE(b[n:], entry.Duration)
n += 4
//}
```

**Наслідки**: Завжди записуються всі поля незалежно від прапорців → некоректний формат, плеєри не зможуть прочитати.

**✅ Виправлення**: Розкоментувати перевірки прапорців:

```go
func (self TrackFragRun) marshal(b []byte) (n int) {
    pio.PutU8(b[n:], self.Version); n += 1
    pio.PutU24BE(b[n:], self.Flags); n += 3
    pio.PutU32BE(b[n:], uint32(len(self.Entries))); n += 4
    
    // Опціональні поля заголовку
    if self.Flags&mp4io.TRUN_DATA_OFFSET != 0 {
        pio.PutU32BE(b[n:], self.DataOffset); n += 4
    }
    if self.Flags&mp4io.TRUN_FIRST_SAMPLE_FLAGS != 0 {
        pio.PutU32BE(b[n:], self.FirstSampleFlags); n += 4
    }
    
    // Семпли з перевіркою прапорців
    for i, entry := range self.Entries {
        var flags uint32
        if i > 0 {
            flags = self.Flags
        } else {
            flags = self.FirstSampleFlags
        }
        if flags&mp4io.TRUN_SAMPLE_DURATION != 0 {
            pio.PutU32BE(b[n:], entry.Duration); n += 4
        }
        if flags&mp4io.TRUN_SAMPLE_SIZE != 0 {
            pio.PutU32BE(b[n:], entry.Size); n += 4
        }
        if flags&mp4io.TRUN_SAMPLE_FLAGS != 0 {
            pio.PutU32BE(b[n:], entry.Flags); n += 4
        }
        if flags&mp4io.TRUN_SAMPLE_CTS != 0 {
            pio.PutU32BE(b[n:], entry.Cts); n += 4
        }
    }
    return
}
```

### ✅ Ваш use-case**: генерація fMP4 фрагменту для HLS

```go
// GenerateFragmentForHLS — створення moof + mdat для одного сегменту
func GenerateFragmentForHLS(videoPkts, audioPkts []av.Packet, seqNum uint32) ([]byte, error) {
    // 1. Створення MovieFrag
    moof := &mp4fio.MovieFrag{
        Header: &mp4fio.MovieFragHeader{
            Version: 0,
            Flags:   0,
            Seqnum:  seqNum,
        },
    }
    
    // 2. Додавання відео треку
    if len(videoPkts) > 0 {
        traf := &mp4fio.TrackFrag{
            Header: &mp4fio.TrackFragHeader{
                // ... налаштування tfhd ...
            },
            Run: &mp4fio.TrackFragRun{
                Version: 0,
                Flags:   mp4io.TRUN_SAMPLE_DURATION | mp4io.TRUN_SAMPLE_SIZE,
                Entries: make([]mp4io.TrackFragRunEntry, len(videoPkts)),
            },
        }
        
        // Заповнення Entries з пакетів
        for i, pkt := range videoPkts {
            traf.Run.Entries[i] = mp4io.TrackFragRunEntry{
                Duration: uint32(pkt.Duration),
                Size:     uint32(len(pkt.Data)),
            }
        }
        moof.Tracks = append(moof.Tracks, traf)
    }
    
    // 3. Серіалізація moof
    moofSize := moof.Len()
    moofBuf := make([]byte, moofSize)
    moof.Marshal(moofBuf)
    
    // 4. Створення mdat з даними (спрощено)
    mdat := createMdatAtom(videoPkts, audioPkts)
    
    // 5. Об'єднання moof + mdat
    result := append(moofBuf, mdat...)
    return result, nil
}
```

---

## 🔑 3. TrackFragHeader (tfhd) — спрощена реалізація

### 🔧 Поточна проблема: зберігання сирих байт

```go
type TrackFragHeader struct {
    Data []byte  // ⚠️ Сирий байтовий буфер замість структурованих полів!
    mp4io.AtomPos
}
```

**Наслідки**:
- Неможливо програмно змінити параметри треку
- Неможливо валідувати вміст при парсингу
- Порушується принцип type safety Go

**✅ Виправлення**: Реалізувати структуровані поля як у `mp4io.TrackFragHeader`:

```go
type TrackFragHeader struct {
    Version         uint8
    Flags           uint32
    BaseDataOffset  uint64  // опціонально
    StsdId          uint32  // опціонально
    DefaultDuration uint32  // опціонально
    DefaultSize     uint32  // опціонально
    DefaultFlags    uint32  // опціонально
    mp4io.AtomPos
}

func (self TrackFragHeader) marshal(b []byte) (n int) {
    pio.PutU8(b[n:], self.Version); n += 1
    pio.PutU24BE(b[n:], self.Flags); n += 3
    
    // Опціональні поля за прапорцями
    if self.Flags&mp4io.TFHD_BASE_DATA_OFFSET != 0 {
        pio.PutU64BE(b[n:], self.BaseDataOffset); n += 8
    }
    if self.Flags&mp4io.TFHD_STSD_ID != 0 {
        pio.PutU32BE(b[n:], self.StsdId); n += 4
    }
    if self.Flags&mp4io.TFHD_DEFAULT_DURATION != 0 {
        pio.PutU32BE(b[n:], self.DefaultDuration); n += 4
    }
    if self.Flags&mp4io.TFHD_DEFAULT_SIZE != 0 {
        pio.PutU32BE(b[n:], self.DefaultSize); n += 4
    }
    if self.Flags&mp4io.TFHD_DEFAULT_FLAGS != 0 {
        pio.PutU32BE(b[n:], self.DefaultFlags); n += 4
    }
    return
}
```

---

## 🔑 4. TrackFragDecodeTime (tfdt) — базовий час декодування

### 🔧 Поточна реалізація:

```go
type TrackFragDecodeTime struct {
    Version uint8
    Flags   uint32
    Time    uint64  // ⚠️ Завжди 64 біти, незалежно від Version!
    mp4io.AtomPos
}
```

### 🔍 Специфікація tfdt:

```
Якщо Version == 0: Time = 32 біти (uint32)
Якщо Version == 1: Time = 64 біти (uint64)

Це дозволяє економити місце для коротких потоків (< ~49 днів у 90kHz clock).
```

### ⚠️ Проблема: ігнорування Version при записі/читанні

```go
// У marshal завжди записується 64 біти:
pio.PutU64BE(b[n:], self.Time)  // ← неправильно для Version=0!

// У Len завжди додається 8 байт:
n += 8  // ← неправильно для Version=0!
```

**✅ Виправлення**: Додати умовну логіку як у `mp4io.TrackFragDecodeTime`:

```go
func (self TrackFragDecodeTime) marshal(b []byte) (n int) {
    pio.PutU8(b[n:], self.Version); n += 1
    pio.PutU24BE(b[n:], self.Flags); n += 3
    
    if self.Version != 0 {
        pio.PutU64BE(b[n:], self.Time); n += 8
    } else {
        pio.PutU32BE(b[n:], uint32(self.Time)); n += 4  // обрізання до 32 біт
    }
    return
}

func (self TrackFragDecodeTime) Len() (n int) {
    n += 8  // заголовок атому
    n += 1 + 3  // Version + Flags
    if self.Version != 0 {
        n += 8
    } else {
        n += 4
    }
    return
}

func (self *TrackFragDecodeTime) Unmarshal(b []byte, offset int) (n int, err error) {
    (&self.AtomPos).setPos(offset, len(b))
    n += 8
    self.Version = pio.U8(b[n:]); n += 1
    self.Flags = pio.U24BE(b[n:]); n += 3
    
    if self.Version != 0 {
        self.Time = pio.U64BE(b[n:]); n += 8
    } else {
        self.Time = uint64(pio.U32BE(b[n:])); n += 4
    }
    return
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Стрімінг fMP4 через WebSocket

```go
// StreamFragmentedMP4 — відправка fMP4 сегментів клієнту
func StreamFragmentedMP4(ws *websocket.Conn, source av.Demuxer) error {
    seqNum := uint32(0)
    fragmentDuration := 2 * time.Second  // 2-секундні сегменти
    
    for {
        // 1. Збір пакетів для одного фрагменту
        var videoPkts, audioPkts []av.Packet
        startTime := time.Now()
        
        for time.Since(startTime) < fragmentDuration {
            pkt, err := source.ReadPacket()
            if err == io.EOF { break }
            if err != nil { return err }
            
            if pkt.Idx == 0 {  // припускаємо відео = перший потік
                videoPkts = append(videoPkts, pkt)
            } else {
                audioPkts = append(audioPkts, pkt)
            }
        }
        
        if len(videoPkts) == 0 && len(audioPkts) == 0 {
            break  // кінець потоку
        }
        
        // 2. Генерація moof + mdat
        fragment, err := GenerateFragmentForHLS(videoPkts, audioPkts, seqNum)
        if err != nil { return err }
        
        // 3. Відправка через WebSocket
        if err := ws.WriteMessage(websocket.BinaryMessage, fragment); err != nil {
            return err
        }
        
        seqNum++
    }
    
    return nil
}
```

### 🔧 Приклад: Демуксинг fMP4 з мережі

```go
// ParseFragmentedMP4Stream — читання fMP4 фрагментів з потоку
func ParseFragmentedMP4Stream(r io.Reader) error {
    buf := bufio.NewReader(r)
    
    for {
        // 1. Читання заголовку атому
        header := make([]byte, 8)
        if _, err := io.ReadFull(buf, header); err == io.EOF {
            break
        } else if err != nil {
            return err
        }
        
        size := int(pio.U32BE(header[0:]))
        tag := mp4io.Tag(pio.U32BE(header[4:]))
        
        if size < 8 {
            return fmt.Errorf("invalid atom size: %d", size)
        }
        
        // 2. Читання тіла атому
        body := make([]byte, size-8)
        if _, err := io.ReadFull(buf, body); err != nil {
            return err
        }
        
        // 3. Парсинг за типом
        switch tag {
        case mp4io.MOOF:
            var moof mp4fio.MovieFrag
            if _, err := moof.Unmarshal(append(header, body...), 0); err != nil {
                return fmt.Errorf("parse moof: %w", err)
            }
            // Обробка фрагменту...
            
        case mp4io.MDAT:
            // Обробка медіа-даних...
            
        default:
            // Ігнорування невідомих атомів
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Unmarshal порожній** | Неможливо прочитати fMP4 фрагменти | Реалізувати парсинг аналогічно до `mp4io` атомів |
| **Ігнорування прапорців у trun** | Некоректний формат, плеєри не читають | Розкоментувати перевірки `flags&TRUN_*` у Marshal/Len/Unmarshal |
| **tfdt завжди 64 біти** | Зайва витрата місця, несумісність | Додати умовну логіку залежно від `Version` |
| **TrackFragHeader як []byte** | Неможлива програмна маніпуляція | Реалізувати структуровані поля з прапорцями |
| **Відсутність валідації** | Падіння при некоректних вхідних даних | Додати перевірки `len(b) >= n+size` перед читанням |

---

## ⚡ Оптимізації для low-latency streaming

### 1. Пул буферів для маршалінгу:

```go
var fragmentBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір фрагменту: 2 секунди відео @ 2Mbps = 500KB
        buf := make([]byte, 0, 512*1024)
        return &buf
    },
}

func GetFragmentBuffer() *[]byte { return fragmentBufferPool.Get().(*[]byte) }
func PutFragmentBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення пам'яті
    fragmentBufferPool.Put(b)
}

// Використання:
buf := GetFragmentBuffer()
defer PutFragmentBuffer(buf)
moof.Marshal(*buf)  // серіалізація у пулений буфер
```

### 2. Інкременальна генерація без буферизації всього фрагменту:

```go
// StreamFragmentDirectly — запис фрагменту прямо у мережу без проміжного буфера
func StreamFragmentDirectly(w io.Writer, moof *mp4fio.MovieFrag, mdatData []byte) error {
    // 1. Запис заголовку moof
    header := make([]byte, 8)
    pio.PutU32BE(header[4:], uint32(mp4io.MOOF))
    
    // 2. Розрахунок розміру та запис
    size := moof.Len()
    pio.PutU32BE(header[0:], uint32(size))
    if _, err := w.Write(header); err != nil { return err }
    
    // 3. Запис тіла moof
    body := make([]byte, size-8)
    moof.marshal(body)
    if _, err := w.Write(body); err != nil { return err }
    
    // 4. Запис mdat
    mdatHeader := make([]byte, 8)
    pio.PutU32BE(mdatHeader[4:], uint32(mp4io.MDAT))
    pio.PutU32BE(mdatHeader[0:], uint32(8+len(mdatData)))
    if _, err := w.Write(mdatHeader); err != nil { return err }
    if _, err := w.Write(mdatData); err != nil { return err }
    
    return nil
}
```

### 3. Моніторинг продуктивності генерації:

```go
type FragmentMetrics struct {
    FragmentsGenerated prometheus.CounterVec
    GenLatency         prometheus.HistogramVec
    FragmentSize       prometheus.HistogramVec
}

func (m *FragmentMetrics) RecordFragment(duration time.Duration, size int, streamID string) {
    m.FragmentsGenerated.WithLabelValues(streamID).Inc()
    m.GenLatency.WithLabelValues(streamID).Observe(duration.Seconds())
    m.FragmentSize.WithLabelValues(streamID).Observe(float64(size))
}
```

---

## 📋 Чек-лист безпечного використання mp4fio

```go
// ✅ 1. Реалізувати Unmarshal для всіх структур
// ✅ 2. Дотримуватись прапорців у TrackFragRun (TRUN_*)
// ✅ 3. Обробляти Version у TrackFragDecodeTime (32/64 біти)
// ✅ 4. Використовувати структуровані поля замість []byte
// ✅ 5. Додавати валідацію довжини перед читанням
// ✅ 6. Обмежувати кількість треків/атомів для захисту
// ✅ 7. Тестувати round-trip: Marshal → Unmarshal → порівняння
// ✅ 8. Метрики для моніторингу продуктивності
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 23009-1 (DASH)](https://www.iso.org/standard/79329.html) — стандарт для фрагментованого MP4 у streaming
- 📄 [CMAF Specification](https://www.iso.org/standard/74428.html) — Common Media Application Format
- 📄 [HLS fMP4 Guide](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 🧪 [Go sync.Pool Best Practices](https://go.dev/blog/pool) — ефективне управління пам'яттю
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди реалізуйте Unmarshal** — без нього неможливий демуксинг потоку.
> 2. **Дотримуйтесь прапорців у trun** — ігнорування призведе до некоректного формату, який не зможуть прочитати плеєри.
> 3. **Обробляйте Version у tfdt** — економте місце та забезпечуйте сумісність зі специфікацією.
> 4. **Використовуйте структуровані поля** — type safety та програмна маніпуляція критичні для підтримки.
> 5. **Моніторьте `GenLatency`** — різке зростання може вказувати на перевантаження або проблеми з мережею.

Потрібен приклад інтеграції `mp4fio` з вашим `mse.Muxer` для генерації fMP4 фрагментів у реальному часі для WebSocket streaming? Готовий допомогти! 🚀