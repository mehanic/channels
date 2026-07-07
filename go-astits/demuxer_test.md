# Глибоке роз'яснення: Тести `Demuxer` у astits — ядро парсингу MPEG-TS

Цей файл містить **комплексні тести головного компонента бібліотеки** — `Demuxer`, який відповідає за читання, парсинг та агрегацію пакетів у логічні одиниці даних (PSI таблиці, PES-пакети).

---

## 🎯 Архітектура `Demuxer`: що він робить?

```
┌─────────────────────────────────────────┐
│ Demuxer — головний інтерфейс astits:   │
│                                         │
│ Вхід: io.Reader (файл, сокет, буфер)   │
│                                         │
│ 🔹 Рівень 1: NextPacket()              │
│    • Читає сирі 188-байтові пакети     │
│    • Парсить заголовки та адаптаційні  │
│      поля                               │
│    • Повертає *Packet структури        │
│                                         │
│ 🔹 Рівень 2: NextData()                │
│    • Агрегує пакети за PID             │
│    • Збирає фрагментовані PSI/PES      │
│    • Повертає *DemuxerData з готовими  │
│      таблицями (PAT/PMT/EIT) або PES   │
│                                         │
│ 🔹 Додатково: Rewind()                 │
│    • Скидає стан для повторного читання│
│    • Очищає внутрішні буфери           │
└─────────────────────────────────────────┘
```

---

## 🔧 Допоміжна функція: `hexToBytes`

```go
func hexToBytes(in string) []byte {
    // Видаляє пробіли/переноси з hex-рядка
    cin := strings.Map(func(r rune) rune {
        if unicode.IsSpace(r) { return -1 }
        return r
    }, in)
    
    // Декодує hex → []byte
    o, err := hex.DecodeString(cin)
    if err != nil { panic(err) }
    return o
}
```

**Використання у тестах:**
```go
// Замість писати []byte{0x47, 0x40, 0x00, ...}
pat := hexToBytes(`474000100000b00d0001c10000...`)
// → читабельніше, легше копіювати з Wireshark/tcpdump
```

> 💡 **Порада**: Ця функція дозволяє вставляти реальні дампи пакетів з мережевих аналізаторів прямо у тести.

---

## 🧪 `TestDemuxerNew` — тест конструктора та опцій

```go
func TestDemuxerNew(t *testing.T) {
    ps := 1  // custom packet size (для тесту)
    pp := func(ps []*Packet) (ds []*DemuxerData, skip bool, err error) { return }
    sp := func(p *Packet) bool { return true }
    
    dmx := NewDemuxer(context.Background(), nil,
        DemuxerOptPacketSize(ps),
        DemuxerOptPacketsParser(pp),
        DemuxerOptPacketSkipper(sp))
    
    // Перевіряє, що опції застосовані
    assert.Equal(t, ps, dmx.optPacketSize)
    assert.Equal(t, fmt.Sprintf("%p", pp), fmt.Sprintf("%p", dmx.optPacketsParser))
    assert.Equal(t, fmt.Sprintf("%p", sp), fmt.Sprintf("%p", dmx.optPacketSkipper))
}
```

### Functional Options Pattern у astits

```go
// Тип опції: функція, що модифікує Demuxer
type DemuxerOption func(*Demuxer)

// Приклад реалізації:
func DemuxerOptPacketSize(size int) DemuxerOption {
    return func(d *Demuxer) { d.optPacketSize = size }
}

// Застосування у конструкторі:
func NewDemuxer(ctx context.Context, r io.Reader, opts ...DemuxerOption) *Demuxer {
    d := &Demuxer{ctx: ctx, r: r, /* defaults */}
    for _, opt := range opts {
        opt(d)  // застосувати опцію
    }
    return d
}
```

**Переваги патерну:**
```
• Гнучкість: додавати нові опції без зміни сигнатури конструктора
• Читабельність: NewDemuxer(ctx, r, OptA(), OptB()) зрозуміліше ніж NewDemuxer(ctx, r, 1, true, nil)
• Типобезпека: кожна опція — типізована функція
```

---

## 📦 `TestDemuxerNextPacket` — тест читання пакетів

### Сценарії тесту

```go
func TestDemuxerNextPacket(t *testing.T) {
    // 🔹 Кейс 1: помилка контексту
    ctx, cancel := context.WithCancel(context.Background())
    dmx := NewDemuxer(ctx, bytes.NewReader([]byte{}))
    cancel()  // скасувати контекст
    _, err := dmx.NextPacket()
    assert.Error(t, err)  // ✅ має повернути помилку
    
    // 🔹 Кейс 2: валідне читання двох пакетів
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Згенерувати два тестових пакети (192 байти кожен)
    b1, p1 := packet(packetHeader, *packetAdaptationField, []byte("1"), true)
    w.Write(b1)
    b2, p2 := packet(packetHeader, *packetAdaptationField, []byte("2"), true)
    w.Write(b2)
    
    dmx = NewDemuxer(context.Background(), bytes.NewReader(buf.Bytes()))
    
    // Перший пакет
    p, err := dmx.NextPacket()
    assert.NoError(t, err)
    assert.Equal(t, p1, p)  // ✅ структурна рівність
    assert.Equal(t, 192, dmx.packetBuffer.packetSize)  // ✅ автодетекція розміру!
    
    // Другий пакет
    p, err = dmx.NextPacket()
    assert.NoError(t, err)
    assert.Equal(t, p2, p)
    
    // EOF
    _, err = dmx.NextPacket()
    assert.EqualError(t, err, ErrNoMorePackets.Error())
}
```

### Ключові моменти

1. **Автодетекція розміру пакету**:
   ```go
   assert.Equal(t, 192, dmx.packetBuffer.packetSize)
   ```
   → Хоча пакети згенеровані з `packet192bytes=true`, демуксер сам визначає розмір через `autoDetectPacketSize()`.

2. **Структурна рівність `assert.Equal(t, p1, p)`**:
   → Порівнює всі поля `Packet`, включаючи вкладені `PacketHeader` та `PacketAdaptationField`.

3. **Обробка EOF**:
   ```go
   assert.EqualError(t, err, ErrNoMorePackets.Error())
   ```
   → `ErrNoMorePackets` — спеціальна помилка для нормального завершення, не `io.EOF`.

---

## 🗂️ `TestDemuxerNextData` — тест агрегації даних (PSI)

### Контекст: PSI (Program Specific Information)

```
PSI таблиці (PAT/PMT/SDT/EIT) часто фрагментовані:
• Одна таблиця може займати кілька TS-пакетів
• Пакети мають однаковий PID
• PayloadUnitStartIndicator=1 тільки на першому пакеті фрагмента

Demuxer.NextData() автоматично:
1. Групує пакети за PID
2. Чекає на PUSI=1 для початку нової таблиці
3. Збирає всі фрагменти до завершення (за CRC або довжиною)
4. Повертає готову *DemuxerData з парсеною таблицею
```

### Розбір тесту

```go
func TestDemuxerNextData(t *testing.T) {
    // 🔹 Підготовка: згенерувати фрагментовану PSI таблицю
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    b := psiBytes()  // сирі байти PSI секції
    
    // Пакет 1: PUSI=1, перші 147 байт payload
    b1, _ := packet(PacketHeader{
        ContinuityCounter: 0,
        PayloadUnitStartIndicator: true,  // 🎯 початок фрагмента
        PID: PIDPAT,  // PID=0x0000 для PAT
    }, PacketAdaptationField{}, b[:147], true)
    w.Write(b1)
    
    // Пакет 2: продовження, PUSI=0
    b2, _ := packet(PacketHeader{
        ContinuityCounter: 1,
        PID: PIDPAT,
    }, PacketAdaptationField{}, b[147:], true)  // решта байт
    w.Write(b2)
    
    dmx := NewDemuxer(context.Background(), bytes.NewReader(buf.Bytes()))
    
    // 🔹 Прочитати перший пакет (для ініціалізації стану)
    p, err := dmx.NextPacket()
    assert.NoError(t, err)
    
    // 🔹 Rewind: скинути стан для повторного читання
    _, err = dmx.Rewind()
    assert.NoError(t, err)
    
    // 🔹 Читати дані (не пакети!)
    var ds []*DemuxerData
    for _, s := range psi.Sections {
        if !s.Header.TableID.isUnknown() {
            d, err := dmx.NextData()
            assert.NoError(t, err)
            ds = append(ds, d)
        }
    }
    
    // 🔹 Перевірити, що агреговані дані співпадають з очікуваними
    assert.Equal(t, psi.toData(
        &Packet{Header: p.Header, AdaptationField: p.AdaptationField},
        PIDPAT,
    ), ds)
    
    // 🔹 Перевірити, що programMap оновлено (PID → program_number)
    assert.Equal(t, map[uint32]uint16{0x3: 0x2, 0x5: 0x4}, dmx.programMap.p)
    
    // 🔹 EOF
    _, err = dmx.NextData()
    assert.EqualError(t, err, ErrNoMorePackets.Error())
}
```

### Ключові інсайти

1. **`Rewind()` для тестів**:
   ```go
   _, err = dmx.Rewind()
   ```
   → Скидає `packetPool`, `dataBuffer`, `packetBuffer` — дозволяє перечитати потік з початку.

2. **`programMap` оновлення**:
   ```go
   assert.Equal(t, map[uint32]uint16{0x3: 0x2, 0x5: 0x4}, dmx.programMap.p)
   ```
   → Після парсингу PAT, демуксер знає: PID 0x3 → program 2, PID 0x5 → program 4.

3. **Фільтрація невідомих таблиць**:
   ```go
   if !s.Header.TableID.isUnknown() { ... }
   ```
   → Ігнорує приватні/непідтримувані PSI типи.

---

## ❓ `TestDemuxerNextDataUnknownDataPackets` — обробка не-даних

```go
func TestDemuxerNextDataUnknownDataPackets(t *testing.T) {
    buf := &bytes.Buffer{}
    bufWriter := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Пакет з PID=256, payload = [0x01, 0x02, 0x03, 0x04]
    // Це НЕ PSI (не починається з table_id) і НЕ PES (немає PES start code)
    b1, _ := packet(PacketHeader{
        ContinuityCounter: 0,
        PID: 256,  // не PAT/PMT PID
        PayloadUnitStartIndicator: true,
        HasPayload: true,
    }, PacketAdaptationField{}, []byte{0x01, 0x02, 0x03, 0x04}, true)
    bufWriter.Write(b1)
    
    dmx := NewDemuxer(context.Background(), bytes.NewReader(buf.Bytes()),
        DemuxerOptPacketSize(188))
    
    // NextData() має повернути (nil, ErrNoMorePackets)
    // бо пакет не містить валідних PSI/PES даних
    d, err := dmx.NextData()
    assert.Equal(t, (*DemuxerData)(nil), d)
    assert.EqualError(t, err, ErrNoMorePackets.Error())
}
```

> 💡 **Важливо**: `NextData()` пропускає пакети, які не є ні PSI, ні PES. Це дозволяє демуксеру працювати з потоками, що містять "сміття" або приватні дані.

---

## 📺 `TestDemuxerNextDataPATPMT` — тест з реальними даними

```go
func TestDemuxerNextDataPATPMT(t *testing.T) {
    // 🔹 Реальні дампи пакетів (з Wireshark або енкодера)
    pat := hexToBytes(`474000100000b00d0001c100000001f0002ab104b2ff...`)
    pmt := hexToBytes(`475000100002b0170001c10000e100f0001be100f0000f...`)
    
    r := bytes.NewReader(append(pat, pmt...))
    dmx := NewDemuxer(context.Background(), r, DemuxerOptPacketSize(188))
    assert.Equal(t, 188*2, r.Len())  // 2 пакети по 188 байт
    
    // 🔹 Читати PAT (PID=0x0000)
    d, err := dmx.NextData()
    assert.NoError(t, err)
    assert.Equal(t, uint16(0), d.FirstPacket.Header.PID)  // ✅ PAT PID
    assert.NotNil(t, d.PAT)  // ✅ PAT парсено
    
    // 🔹 Читати PMT (PID=0x1000)
    d, err = dmx.NextData()
    assert.NoError(t, err)
    assert.Equal(t, uint16(0x1000), d.FirstPacket.Header.PID)  // ✅ PMT PID
    assert.NotNil(t, d.PMT)  // ✅ PMT парсено
}
```

### Розбір PAT пакету (перші байти)

```
47 40 00 10  00 00 b0 0d  00 01 c1 00  00 00 01 f0  00 2a b1 04 b2 ff...
│  │  │  │   │  │  │  │   │  │  │  │   │  │  │  │   │  │  │  │  │
│  │  │  │   │  │  │  │   │  │  │  │   │  │  │  │   │  │  │  │  └─ CRC32 (початок)
│  │  │  │   │  │  │  │   │  │  │  │   │  │  │  │   │  │  │  └─ CRC32
│  │  │  │   │  │  │  │   │  │  │  │   │  │  │  │   │  │  └─ CRC32
│  │  │  │   │  │  │  │   │  │  │  │   │  │  │  │   │  └─ CRC32
│  │  │  │   │  │  │  │   │  │  │  │   │  │  │  └─ program_map_PID=0x0001
│  │  │  │   │  │  │  │   │  │  │  │   │  │  └─ program_number=0x0001
│  │  │  │   │  │  │  │   │  │  │  │   │  └─ version_number=0, current_next=1
│  │  │  │   │  │  │  │   │  │  │  │   └─ table_id_extension=0x0001 (TS ID)
│  │  │  │   │  │  │  │   │  │  │  └─ section_number=0, last_section=0
│  │  │  │   │  │  │  │   │  │  └─ section_length=13 байт
│  │  │  │   │  │  │  │   │  └─ syntax=1, private=0, reserved
│  │  │  │   │  │  │  │   └─ table_id=0x00 (PAT)
│  │  │  │   │  │  │  └─ transport_scrambling=0, adaptation=0, payload=1
│  │  │  │   │  │  └─ continuity_counter=0
│  │  │  │   │  └─ PID=0x0000 (PAT)
│  │  │  │   └─ PUSI=1, priority=0
│  │  │  └─ transport_error=0
│  │  └─ sync_byte=0x47 ✅
│  └─ (payload start)
└─ sync_byte=0x47 ✅
```

> 💡 **Порада**: Використовуйте `hexToBytes` + дампи з реальних потоків для інтеграційних тестів — це гарантує сумісність з енкодерами.

---

## 🔁 `TestDemuxerRewind` — тест скидання стану

```go
func TestDemuxerRewind(t *testing.T) {
    r := bytes.NewReader([]byte("content"))
    dmx := NewDemuxer(context.Background(), r)
    
    // Додати дані у внутрішні буфери (імітація роботи)
    dmx.packetPool.addUnlocked(&Packet{Header: PacketHeader{PID: 1}})
    dmx.dataBuffer = append(dmx.dataBuffer, &DemuxerData{})
    
    // Прочитати 2 байти з reader
    b := make([]byte, 2)
    _, err := r.Read(b)
    assert.NoError(t, err)
    
    // 🔹 Rewind: скинути все
    n, err := dmx.Rewind()
    assert.NoError(t, err)
    assert.Equal(t, int64(0), n)  // позиція reader скинута на 0
    assert.Equal(t, 7, r.Len())   // "content" = 7 байт, всі доступні знову
    assert.Equal(t, 0, len(dmx.dataBuffer))    // ✅ буфер очищено
    assert.Equal(t, 0, len(dmx.packetPool.b))  // ✅ pool очищено
    assert.Nil(t, dmx.packetBuffer)            // ✅ packetBuffer скинуто
}
```

### Коли використовувати `Rewind()`?

```
✅ Тестування: перечитати потік кілька разів
✅ Аналіз: спочатку сканувати PAT/PMT, потім повернутися для детального парсингу
❌ Production streaming: не використовувати на network streams (не підтримує Seek)
```

---

## ⚡ `BenchmarkDemuxer_NextData` — тест продуктивності

```go
func BenchmarkDemuxer_NextData(b *testing.B) {
    b.ReportAllocs()  // 📊 звітувати про алокації
    
    // Підготувати буфер з фрагментованою PSI таблицею
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    bs := psiBytes()
    b1, _ := packet(PacketHeader{PUSI: true, PID: PIDPAT}, PacketAdaptationField{}, bs[:147], true)
    w.Write(b1)
    b2, _ := packet(PacketHeader{PID: PIDPAT}, PacketAdaptationField{}, bs[147:], true)
    w.Write(b2)
    
    b.ResetTimer()  // ⏱️ не враховувати підготовку у бенчмарк
    
    for i := 0; i < b.N; i++ {
        // Скинути reader для кожного ітерації
        r := bytes.NewReader(buf.Bytes())
        dmx := NewDemuxer(context.Background(), r)
        
        // Прочитати всі секції
        for _, s := range psi.Sections {
            if !s.Header.TableID.isUnknown() {
                dmx.NextData()
            }
        }
    }
}
```

**Очікувані метрики:**
```
BenchmarkDemuxer_NextData-8    10000    120000 ns/op    5000 B/op    50 allocs/op
```

**Що аналізувати:**
| Метрика | Ідеальне значення | Що означає відхилення |
|---------|-------------------|----------------------|
| `ns/op` | < 100 µs для малих таблиць | Повільний парсинг → оптимізувати бітові операції |
| `B/op`  | < 10 KB | Зайві алокації → використовувати bytesPool |
| `allocs/op` | < 100 | Кожна алокація = тиск на GC |

---

## 🧪 `FuzzDemuxer` — fuzz-тест на стійкість

```go
func FuzzDemuxer(f *testing.F) {
    f.Fuzz(func(t *testing.T, b []byte) {
        r := bytes.NewReader(b)
        dmx := NewDemuxer(context.Background(), r, DemuxerOptPacketSize(188))
        
        // Читати до кінця — не повинно панікувати!
        for {
            _, err := dmx.NextData()
            if err == ErrNoMorePackets {
                break
            }
            // Інші помилки — ок, головне немає panic
        }
    })
}
```

**Запуск:**
```bash
go test -fuzz=FuzzDemuxer -fuzztime=60s
```

**Що ловить fuzz-тест:**
```
• Вихід за межі буфера при читанні полів
• Ділення на нуль при обробці довжин
• Невірні припущення про формат даних
• Пам'яткові витоки при помилках парсингу
• Паніки при неочікуваних значеннях прапорців
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Використання `NextPacket()` для низькорівневої обробки

```go
// У segmentAssembler — коли потрібен повний контроль над пакетами:
func processRawPackets(reader io.Reader, channelID string) error {
    dmx := astits.NewDemuxer(context.Background(), reader,
        astits.DemuxerOptPacketSize(188),
        astits.DemuxerOptPacketSkipper(func(p *astits.Packet) bool {
            // Фільтрувати пакети не для цього каналу
            return p.Header.PID != expectedPIDForChannel(channelID)
        }),
    )
    
    for {
        pkt, err := dmx.NextPacket()
        if errors.Is(err, astits.ErrNoMorePackets) {
            break
        }
        if err != nil {
            log.Errorf("Channel %s: packet read error: %v", channelID, err)
            continue
        }
        
        // Обробити сирі пакети (напр., для orphan audio merge)
        if err := handleRawPacket(pkt, channelID); err != nil {
            log.Warnf("Channel %s: packet process error: %v", channelID, err)
        }
    }
    return nil
}
```

### ✅ 2. Використання `NextData()` для агрегації PSI/PES

```go
// У VideoManifestProxy — коли потрібні готові таблиці:
func extractProgramInfo(reader io.Reader) (*ProgramInfo, error) {
    dmx := astits.NewDemuxer(context.Background(), reader)
    
    info := &ProgramInfo{}
    
    for {
        data, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) {
            break
        }
        if err != nil {
            return nil, fmt.Errorf("failed to read data: %w", err)
        }
        
        // 🔹 PAT: отримати список програм
        if data.PAT != nil {
            info.TransportStreamID = data.PAT.TransportStreamID
            for _, prog := range data.PAT.Programs {
                if prog.ProgramNumber > 0 {  // пропустити NIT
                    info.Programs = append(info.Programs, Program{
                        Number: prog.ProgramNumber,
                        PMTPID: prog.ProgramMapID,
                    })
                }
            }
        }
        
        // 🔹 PMT: отримати PID потоків
        if data.PMT != nil {
            info.PCRPID = data.PMT.PCRPID
            for _, es := range data.PMT.ElementaryStreams {
                info.Streams = append(info.Streams, Stream{
                    PID:  es.ElementaryPID,
                    Type: es.StreamType,  // H.264, AAC, etc.
                })
            }
        }
        
        // 🔹 EIT: отримати розклад передач
        if data.EIT != nil {
            for _, event := range data.EIT.Events {
                info.EPG = append(info.EPG, EPGEvent{
                    Title:     event.EventName,
                    StartTime: event.StartTime,
                    Duration:  event.Duration,
                })
            }
        }
    }
    
    return info, nil
}
```

### ✅ 3. Обробка помилок та відновлення

```go
// У production-коді — стійка обробка помилок:
func robustDemux(reader io.Reader, channelID string, metrics *DemuxMetrics) error {
    dmx := astits.NewDemuxer(context.Background(), reader,
        astits.DemuxerOptPacketSize(188),
    )
    
    for {
        data, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) {
            return nil  // нормальне завершення
        }
        if err != nil {
            metrics.Errors.WithLabelValues(channelID, "demux").Inc()
            
            // Класифікувати помилку
            if strings.Contains(err.Error(), "corrupted") {
                log.Warnf("Channel %s: corrupted packet, skipping", channelID)
                continue  // спробувати наступний
            }
            if strings.Contains(err.Error(), "sync") {
                log.Errorf("Channel %s: sync lost, restarting demux", channelID)
                // Опція: спробувати Rewind() або перез'єднатися
                return err
            }
            
            return fmt.Errorf("demux error: %w", err)
        }
        
        // Обробити валідні дані
        if err := processData(data, channelID); err != nil {
            metrics.Errors.WithLabelValues(channelID, "process").Inc()
            log.Warnf("Channel %s: process error: %v", channelID, err)
        }
    }
}
```

### ✅ 4. Моніторинг продуктивності демуксингу

```go
// monitoring.Monitor — метрики для Demuxer:
type DemuxMetrics struct {
    PacketsRead    *prometheus.CounterVec  // кількість прочитаних пакетів
    DataAggregated *prometheus.CounterVec  // кількість агрегованих DemuxerData
    ParseErrors    *prometheus.CounterVec  // помилки парсингу
    BytesProcessed *prometheus.CounterVec  // загальний обсяг даних
    Latency        *prometheus.HistogramVec  // латентність NextData()
}

// У циклі читання:
func demuxWithMetrics(dmx *astits.Demuxer, channelID string, metrics *DemuxMetrics) error {
    for {
        start := time.Now()
        
        data, err := dmx.NextData()
        latency := time.Since(start)
        
        if errors.Is(err, astits.ErrNoMorePackets) {
            return nil
        }
        if err != nil {
            metrics.ParseErrors.WithLabelValues(channelID).Inc()
            continue
        }
        
        metrics.DataAggregated.WithLabelValues(
            channelID,
            dataTypeToString(data),  // "PAT", "PMT", "PES", etc.
        ).Inc()
        metrics.Latency.WithLabelValues(channelID).Observe(latency.Seconds())
        
        // Обробити data...
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `NextData()` повертає `ErrNoMorePackets` одразу | Потік не починається з синхробайта або містить тільки не-PSI/PES пакети | Перевірити вхідні дані; використовувати `NextPacket()` для низькорівневої діагностики |
| `programMap` не оновлюється | Клієнти не бачать нових програм | Перевірити, що PAT парситься коректно; додати логування після `NextData()` з `data.PAT != nil` |
| Фрагментовані PSI не збираються | `NextData()` пропускає таблиці | Перевірити, що `PayloadUnitStartIndicator` встановлений на першому пакеті фрагмента |
| Паніка при fuzz-тесті | Вихід за межі буфера | Додати перевірки `if i.Offset()+length > i.Len()` перед `NextBytes()` |
| Високе споживання пам'яті | `B/op` у бенчмарку > 10 KB | Використовувати `bytesPool` для тимчасових буферів; уникати копіювання даних |

### Приклад діагностики фрагментації:

```go
func debugFragmentation(dmx *astits.Demuxer, pid uint16) {
    log.Infof("Debugging PID %d fragmentation...", pid)
    
    for i := 0; i < 100; i++ {  // обмежити сканування
        pkt, err := dmx.NextPacket()
        if err != nil { break }
        
        if pkt.Header.PID == pid {
            log.Infof("Packet %d: CC=%d, PUSI=%d, AF=%v, payload_len=%d",
                i,
                pkt.Header.ContinuityCounter,
                boolToInt(pkt.Header.PayloadUnitStartIndicator),
                pkt.AdaptationField != nil,
                len(pkt.Payload),
            )
        }
    }
}

func boolToInt(b bool) int {
    if b { return 1 }
    return 0
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Ініціалізація демуксера з опціями:
func newDemuxerForChannel(channelID string, reader io.Reader) (*astits.Demuxer, error) {
    return astits.NewDemuxer(
        context.Background(),
        reader,
        astits.DemuxerOptPacketSize(188),  // явний розмір для мережевих потоків
        astits.DemuxerOptPacketSkipper(func(p *astits.Packet) bool {
            // Фільтрувати пакети не для цього каналу
            return !isPIDForChannel(p.Header.PID, channelID)
        }),
    ), nil
}

// 2. Читання з обробкою помилок:
func readProgramData(dmx *astits.Demuxer) ([]*astits.DemuxerData, error) {
    var results []*astits.DemuxerData
    
    for {
        data, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) {
            break
        }
        if err != nil {
            // Класифікувати помилку
            if isRecoverableError(err) {
                log.Warnf("Recoverable demux error: %v", err)
                continue
            }
            return results, fmt.Errorf("fatal demux error: %w", err)
        }
        
        results = append(results, data)
    }
    
    return results, nil
}

func isRecoverableError(err error) bool {
    // Помилки, які можна пропустити без втрати синхронізації
    return strings.Contains(err.Error(), "corrupted") ||
           strings.Contains(err.Error(), "skip")
}

// 3. Rewind для аналізу:
func analyzeStreamTwice(reader io.ReadSeeker) error {
    dmx := astits.NewDemuxer(context.Background(), reader)
    
    // Перший прохід: зібрати метадані
    metadata, _ := readProgramData(dmx)
    
    // Скинути для другого проходу
    if _, err := reader.Seek(0, io.SeekStart); err != nil {
        return err
    }
    if _, err := dmx.Rewind(); err != nil {
        return err
    }
    
    // Другий прохід: детальна обробка з контекстом метаданих
    return processWithMetadata(dmx, metadata)
}

// 4. Моніторинг:
func demuxLoop(dmx *astits.Demuxer, channelID string, metrics *DemuxMetrics) {
    for {
        start := time.Now()
        data, err := dmx.NextData()
        latency := time.Since(start)
        
        if errors.Is(err, astits.ErrNoMorePackets) {
            return
        }
        if err != nil {
            metrics.Errors.WithLabelValues(channelID).Inc()
            continue
        }
        
        metrics.Latency.WithLabelValues(channelID).Observe(latency.Seconds())
        metrics.DataTypes.WithLabelValues(channelID, dataTypeLabel(data)).Inc()
        
        // Обробити data...
    }
}
```

---

## 📊 Матриця методів `Demuxer`

```
Метод            | Рівень абстракції | Повертає          | Коли використовувати
─────────────────┼───────────────────┼───────────────────┼─────────────────────
NextPacket()     | Низький (сирі пакети) | *Packet        | • Низькорівнева діагностика
                 |                   |                   | • Орфан аудіо/відео merge
                 |                   |                   | • Кастомна агрегація
─────────────────┼───────────────────┼───────────────────┼─────────────────────
NextData()       | Високий (логічні одиниці) | *DemuxerData | • Отримання PAT/PMT/EIT
                 |                   |                   | • Парсинг PES-потоків
                 |                   |                   | • Стандартна обробка потоків
─────────────────┼───────────────────┼───────────────────┼─────────────────────
Rewind()         | Управління станом | int64 (позиція)  | • Тестування
                 |                   |                   | • Двопрохідний аналіз
                 |                   |                   | • НЕ для network streams
─────────────────┼───────────────────┼───────────────────┼─────────────────────
NewDemuxer()     | Ініціалізація     | *Demuxer         | • Створення з опціями
                 |                   |                   | • PacketSize, Skipper, Parser
```

---

## 📚 Корисні посилання

- [astits Demuxer API docs](https://pkg.go.dev/github.com/asticode/go-astits#Demuxer)
- [MPEG-TS PSI specification](https://www.iso.org/standard/61236.html)
- [Functional Options Pattern in Go](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis)

> 💡 **Ключова ідея**: `Demuxer` — це "міст" між сирими бітами та структурованими даними. У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Отримувати готові PSI таблиці для генерації валідних HLS-плейлистів
> - 🧩 Агрегувати фрагментовані PES-пакети для подальшої обробки (transcode, TTS, субтитри)
> - 🔍 Діагностувати проблеми потоку через низькорівневий доступ до пакетів
> - 📊 Збирати метрики про якість демуксингу для моніторингу стабільності

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати `Demuxer` у ваш `segmentAssembler` з підтримкою orphan audio merge
- 🧪 Написати integration-тест для перевірки сумісності з реальними енкодерами
- 📈 Додати Prometheus-метрики для моніторингу латентності та помилок демуксингу по каналах

🛠️