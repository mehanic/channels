# Глибоке роз'яснення: `muxer.go` — ядро генерації MPEG-TS у astits

Цей файл містить **повноцінний TS-муксер**: генерацію PSI таблиць (PAT/PMT), управління elementary streams, фрагментацію PES-даних, вставку таймінгів (PCR/PTS/DTS) та підтримку динамічних оновлень. Це "фінальний етап" вашого пайплайну перед відправкою у мережу.

---

## 🎯 Архітектура: компоненти Muxer

```
┌─────────────────────────────────────────┐
│ Muxer — стан та потоки даних:          │
│                                         │
│ 📦 Конфігурація:                        │
│   • packetSize = 188 (фіксовано)       │
│   • tablesRetransmitPeriod = 40 пакети │
│   • transportStreamID, pmtPID          │
│                                         │
│ 🗂️ PSI таблиці (кешовані):             │
│   • patBytes, pmtBytes: готові пакети  │
│   • pm: programMap (PID → program_num) │
│   • pmt: PMTData з елементарними потоками│
│                                         │
│ 🔢 Лічильники (wrappingCounter):       │
│   • patVersion/pmtVersion: 5 біт (0-31)│
│   • patCC/pmtCC: 4 біти (0-15)         │
│   • esContexts[PID].cc: CC для кожного потоку│
│                                         │
│ ✍️ Вивід:                               │
│   • w: io.Writer (файл/сокет/буфер)    │
│   • bitsWriter: бітова серіалізація    │
│   • buf: тимчасовий буфер для PES      │
└─────────────────────────────────────────┘
```

---

## 🔧 Ініціалізація та конфігурація

### `NewMuxer` — створення з опціями

```go
func NewMuxer(ctx context.Context, w io.Writer, opts ...func(*Muxer)) *Muxer {
    m := &Muxer{
        ctx: ctx,
        w:   w,
        
        packetSize:             MpegTsPacketSize,  // 188 байт
        tablesRetransmitPeriod: 40,                 // PAT/PMT кожні 40 PES-пакетів
        
        pmtPID: pmtStartPID,  // 0x1000 за замовчуванням
        
        pm: newProgramMap(),  // мапінг PID → program_number
        pmt: PMTData{
            ElementaryStreams: []*PMTElementaryStream{},
            ProgramNumber:     programNumberStart,  // 1
        },
        
        // Версії таблиць: 5 біт → діапазон 0-31
        patVersion: newWrappingCounter(0b11111),  // 31
        pmtVersion: newWrappingCounter(0b11111),
        
        // Continuity counters: 4 біти → 0-15
        patCC: newWrappingCounter(0b1111),
        pmtCC: newWrappingCounter(0b1111),
        
        esContexts: map[uint32]*esContext{},  // PID → контекст потоку
    }
    
    // Ініціалізація бітових писарів
    m.bufWriter = astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &m.buf})
    m.bitsWriter = astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: m.w})
    
    // Застосування опцій конфігурації
    for _, opt := range opts {
        opt(m)
    }
    
    // 🔹 Реєстрація PMT PID у programMap
    m.pm.setUnlocked(m.pmtPID, programNumberStart)
    m.pmUpdated = true
    
    // 🔹 Примусовий вивід таблиць на старті
    m.tablesRetransmitCounter = m.tablesRetransmitPeriod
    
    return m
}
```

### Опції конфігурації (functional options pattern)

```go
// 🔹 Період повторної передачі таблиць
MuxerOptTablesRetransmitPeriod(20)  // PAT/PMT кожні 20 пакетів

// 🔹 Custom Transport Stream ID (для ідентифікації потоку)
WithTransportStreamID(0xABCD)

// 🔹 Custom PID для PMT таблиці
WithPMTPID(0x1100)  // замість стандартного 0x1000

// Приклад використання:
muxer := astits.NewMuxer(ctx, output,
    WithTransportStreamID(0x1234),
    WithPMTPID(0x1000),
    MuxerOptTablesRetransmitPeriod(30),
)
```

> 💡 **Порада**: Зменшіть `tablesRetransmitPeriod` для потоків з частими підключеннями нових клієнтів — вони швидше отримають метадані.

---

## 📡 Управління elementary streams

### `AddElementaryStream` — реєстрація нового потоку

```go
func (m *Muxer) AddElementaryStream(es PMTElementaryStream) error {
    // 🔹 Якщо PID не вказано — авто-генерація
    if es.ElementaryPID != 0 {
        // Перевірка на дублікати
        for _, oes := range m.pmt.ElementaryStreams {
            if oes.ElementaryPID == es.ElementaryPID {
                return ErrPIDAlreadyExists
            }
        }
    } else {
        es.ElementaryPID = m.nextPID  // авто-інкремент
        m.nextPID++
    }
    
    // 🔹 Додати у список потоків PMT
    m.pmt.ElementaryStreams = append(m.pmt.ElementaryStreams, &es)
    
    // 🔹 Створити контекст для цього потоку (з власним CC)
    m.esContexts[uint32(es.ElementaryPID)] = newEsContext(&es)
    
    // 🔹 Інвалідувати кешований PMT-пакет
    m.pmtBytes.Reset()
    m.pmtUpdated = true  // 🎯 флаг: при наступній генерації version_number інкрементується
    
    return nil
}

func newEsContext(es *PMTElementaryStream) *esContext {
    return &esContext{
        es: es,
        cc: newWrappingCounter(0b1111),  // CC = 4 біти → wrap at 15
    }
}
```

### `RemoveElementaryStream` — динамічне видалення

```go
func (m *Muxer) RemoveElementaryStream(pid uint16) error {
    // Знайти індекс потоку
    foundIdx := -1
    for i, oes := range m.pmt.ElementaryStreams {
        if oes.ElementaryPID == pid {
            foundIdx = i
            break
        }
    }
    
    if foundIdx == -1 {
        return ErrPIDNotFound
    }
    
    // Видалити зі списку (slice manipulation)
    m.pmt.ElementaryStreams = append(
        m.pmt.ElementaryStreams[:foundIdx], 
        m.pmt.ElementaryStreams[foundIdx+1:]...,
    )
    
    // Очистити контекст
    delete(m.esContexts, uint32(pid))
    
    // Інвалідувати кеш та позначити оновлення
    m.pmtBytes.Reset()
    m.pmtUpdated = true
    
    return nil
}
```

### `SetPCRPID` — вказати джерело еталонного часу

```go
func (m *Muxer) SetPCRPID(pid uint16) {
    m.pmt.PCRPID = pid
    m.pmtUpdated = true  // 🎯 зміна вмісту → нова версія таблиці
}
```

> ⚠️ **Критично**: `PCR PID` має вказувати на потік, що дійсно містить PCR (зазвичай відео). Інакше `WriteTables()` поверне `ErrPCRPIDInvalid`.

---

## 📦 Генерація PAT/PMT таблиць

### `generatePAT` — створення Program Association Table

```go
func (m *Muxer) generatePAT() error {
    // 🔹 Отримати PATData з programMap
    d := m.pm.toPATDataUnlocked()
    d.TransportStreamID = m.transportStreamID
    
    // 🔹 Версія таблиці: інкремент тільки при зміні вмісту
    versionNumber := m.patVersion.get()
    if m.pmUpdated {  // 🎯 зміни у мапінгу програм?
        versionNumber = m.patVersion.inc()  // 0→1→...→31→0
    }
    
    // 🔹 Збірка PSI секції з заголовком та даними
    syntax := &PSISectionSyntax{
        Data: &PSISectionSyntaxData{PAT: d},
        Header: &PSISectionSyntaxHeader{
            CurrentNextIndicator: true,    // ця версія актуальна зараз
            TableIDExtension: d.TransportStreamID,
            VersionNumber:    uint8(versionNumber),  // 5 біт
            // SectionNumber/LastSectionNumber для multi-packet таблиць
        },
    }
    
    section := PSISection{
        Header: &PSISectionHeader{
            SectionLength:          calcPATSectionLength(d),
            SectionSyntaxIndicator: true,
            TableID:                PSITableIDPAT,  // 0x00
        },
        Syntax: syntax,
    }
    
    psiData := PSIData{Sections: []*PSISection{&section}}
    
    // 🔹 Серіалізація у тимчасовий буфер
    m.buf.Reset()
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &m.buf})
    writePSIData(w, &psiData)  // включає CRC32 обчислення
    
    // 🔹 "Упаковка" у TS-пакет (188 байт)
    m.patBytes.Reset()
    wPacket := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &m.patBytes})
    
    pkt := Packet{
        Header: PacketHeader{
            HasPayload:                true,
            PayloadUnitStartIndicator: true,  // 🎯 PES/PSI починається одразу
            PID:                       PIDPAT,  // 0x0000
            ContinuityCounter:         uint8(m.patCC.inc()),  // 4-бітний лічильник
        },
        Payload: m.buf.Bytes(),  // серіалізована PSI секція
    }
    
    writePacket(wPacket, &pkt, m.packetSize)  // додає stuffing до 188 байт
    
    // 🔹 Скинути флаг оновлення
    m.pmUpdated = false
    
    return nil
}
```

### `generatePMT` — створення Program Map Table

```go
func (m *Muxer) generatePMT() error {
    // 🔹 Валідація: PCR PID має існувати серед елементарних потоків
    hasPCRPID := false
    for _, es := range m.pmt.ElementaryStreams {
        if es.ElementaryPID == m.pmt.PCRPID {
            hasPCRPID = true
            break
        }
    }
    if !hasPCRPID {
        return ErrPCRPIDInvalid  // 🚨 критична помилка
    }
    
    // 🔹 Версія: інкремент при зміні вмісту PMT
    versionNumber := m.pmtVersion.get()
    if m.pmtUpdated {
        versionNumber = m.pmtVersion.inc()
    }
    
    // 🔹 Збірка PSI секції (аналогічно PAT, але з даними PMT)
    syntax := &PSISectionSyntax{
        Data: &PSISectionSyntaxData{PMT: &m.pmt},  // 🎯 посилання на структуру PMT
        Header: &PSISectionSyntaxHeader{
            CurrentNextIndicator: true,
            TableIDExtension: m.pmt.ProgramNumber,  // program_number як extension
            VersionNumber:    uint8(versionNumber),
        },
    }
    
    // ... серіалізація, пакування у пакет ...
    
    pkt := Packet{
        Header: PacketHeader{
            HasPayload:                true,
            PayloadUnitStartIndicator: true,
            PID:                       m.pmtPID,  // 🎯 не 0x0000, а custom PID
            ContinuityCounter:         uint8(m.pmtCC.inc()),
        },
        Payload: m.buf.Bytes(),
    }
    
    writePacket(wPacket, &pkt, m.packetSize)
    
    m.pmtUpdated = false
    return nil
}
```

### `WriteTables` — запис обох таблиць у потік

```go
func (m *Muxer) WriteTables() (int, error) {
    bytesWritten := 0
    
    // 🔹 Спочатку PAT, потім PMT (порядок важливий для клієнтів)
    if err := m.generatePAT(); err != nil {
        return bytesWritten, err
    }
    if err := m.generatePMT(); err != nil {
        return bytesWritten, err
    }
    
    // 🔹 Записати готові пакети у вихідний writer
    n, err := m.w.Write(m.patBytes.Bytes())
    bytesWritten += n
    if err != nil { return bytesWritten, err }
    
    n, err = m.w.Write(m.pmtBytes.Bytes())
    bytesWritten += n
    
    return bytesWritten, nil
}
```

---

## 🎞️ Запис PES-даних: фрагментація та таймінги

### `WriteData` — головний метод запису медіа-даних

```go
func (m *Muxer) WriteData(d *MuxerData) (int, error) {
    // 🔹 Перевірка: чи зареєстрований цей PID?
    ctx, ok := m.esContexts[uint32(d.PID)]
    if !ok {
        return 0, ErrPIDNotFound
    }
    
    bytesWritten := 0
    
    // 🔹 Примусова передача таблиць при ключовому кадрі на PCR PID
    forceTables := d.AdaptationField != nil &&
                   d.AdaptationField.RandomAccessIndicator &&  // 🎯 keyframe
                   d.PID == m.pmt.PCRPID
    
    n, err := m.retransmitTables(forceTables)
    if err != nil { return n, err }
    bytesWritten += n
    
    // 🔹 Цикл фрагментації великого PES у кілька TS-пакетів
    payloadStart := true
    writeAf := d.AdaptationField != nil
    payloadBytesWritten := 0
    
    for payloadBytesWritten < len(d.PES.Data) {
        // Розрахунок доступного місця у пакеті
        pktLen := 1 + mpegTsPacketHeaderSize  // sync + 3 байти заголовка
        pkt := Packet{
            Header: PacketHeader{
                ContinuityCounter:         uint8(ctx.cc.inc()),  // 🎯 інкремент для цього PID
                HasAdaptationField:        writeAf,
                HasPayload:                false,
                PayloadUnitStartIndicator: false,
                PID:                       d.PID,
            },
        }
        
        if writeAf {
            pkt.AdaptationField = d.AdaptationField
            pktLen += 1 + int(calcPacketAdaptationFieldLength(d.AdaptationField))
            writeAf = false  // AF вставляється тільки у перший пакет фрагмента
        }
        
        bytesAvailable := m.packetSize - pktLen
        
        // 🔹 Логіка PUSI: тільки перший пакет фрагмента має PayloadUnitStartIndicator=1
        if payloadStart {
            pesHeaderLengthCurrent := pesHeaderLength + int(calcPESOptionalHeaderLength(d.PES.Header.OptionalHeader))
            
            if bytesAvailable < pesHeaderLengthCurrent {
                // 🚨 PES-заголовок не поміщається → додати stuffing у AF
                pkt.Header.HasAdaptationField = true
                if pkt.AdaptationField == nil {
                    pkt.AdaptationField = newStuffingAdaptationField(bytesAvailable)
                } else {
                    pkt.AdaptationField.StuffingLength = bytesAvailable
                }
            } else {
                // ✅ Місце є → встановити PUSI та дозволити payload
                pkt.Header.HasPayload = true
                pkt.Header.PayloadUnitStartIndicator = true  // 🎯 початок PES
            }
        } else {
            pkt.Header.HasPayload = true  // продовження фрагмента
        }
        
        // 🔹 Запис PES-даних у тимчасовий буфер
        if pkt.Header.HasPayload {
            m.buf.Reset()
            
            // Авто-визначення stream_id за типом потоку
            if d.PES.Header.StreamID == 0 {
                d.PES.Header.StreamID = ctx.es.StreamType.ToPESStreamID()
            }
            
            ntot, npayload, err := writePESData(
                m.bufWriter,
                d.PES.Header,
                d.PES.Data[payloadBytesWritten:],  // решта даних
                payloadStart,                       // це початок PES?
                bytesAvailable,                     // скільки місця у пакеті
            )
            if err != nil { return bytesWritten, err }
            
            payloadBytesWritten += npayload
            pkt.Payload = m.buf.Bytes()
            
            // 🔹 Stuffing, якщо залишилося місце після payload
            bytesAvailable -= ntot
            if bytesAvailable > 0 {
                pkt.Header.HasAdaptationField = true
                if pkt.AdaptationField == nil {
                    pkt.AdaptationField = newStuffingAdaptationField(bytesAvailable)
                } else {
                    pkt.AdaptationField.StuffingLength = bytesAvailable
                }
            }
            
            // 🔹 Фінальна серіалізація пакету
            n, err = writePacket(m.bitsWriter, &pkt, m.packetSize)
            if err != nil { return bytesWritten, err }
            
            bytesWritten += n
            payloadStart = false  // наступні ітерації — продовження, не початок
        }
    }
    
    // 🔹 Очистити StuffingLength для повторного використання структури
    if d.AdaptationField != nil {
        d.AdaptationField.StuffingLength = 0
    }
    
    return bytesWritten, nil
}
```

### Ключові моменти фрагментації

```
Сценарій: PES з 1000 байт відео-даних, пакет = 188 байт

Ітерація 1:
• payloadStart = true → PUSI = 1
• bytesAvailable = 188 - 4 (header) - 6 (AF з PCR) = 178
• PES header = 6+3+5 = 14 байт → поміщається
• Записано: 14 (PES header) + 164 (дані) = 178 байт payload
• payloadBytesWritten = 164, payloadStart = false

Ітерація 2:
• payloadStart = false → PUSI = 0
• bytesAvailable = 188 - 4 = 184 (немає AF)
• Записано: 184 байт даних
• payloadBytesWritten = 164 + 184 = 348

... продовжуємо доки payloadBytesWritten < 1000

Результат: 1000 байт → 6 TS-пакетів (1 з PUSI=1, 5 з PUSI=0)
```

> 💡 **Важливо**: Тільки перший пакет фрагмента має `PayloadUnitStartIndicator=1`. Це сигналізує декодеру: "тут починається новий логічний блок (PES)".

---

## 🔁 Періодична ретрансмісія таблиць

### `retransmitTables` — логіка повторної відправки PAT/PMT

```go
func (m *Muxer) retransmitTables(force bool) (int, error) {
    m.tablesRetransmitCounter++
    
    // 🔹 Пропустити, якщо не досягнуто періоду і не примусово
    if !force && m.tablesRetransmitCounter < m.tablesRetransmitPeriod {
        return 0, nil
    }
    
    // 🔹 Записати таблиці
    n, err := m.WriteTables()
    if err != nil { return n, err }
    
    // 🔹 Скинути лічильник
    m.tablesRetransmitCounter = 0
    return n, nil
}
```

**Коли `force = true`:**
```go
forceTables := d.AdaptationField != nil &&
               d.AdaptationField.RandomAccessIndicator &&  // 🎯 ключовий кадр
               d.PID == m.pmt.PCRPID                        // 🎯 потік з PCR

// Це гарантує, що нові клієнти, які підключилися на ключовому кадрі,
// одразу отримають актуальні PAT/PMT для ідентифікації потоків.
```

**Рекомендовані значення `tablesRetransmitPeriod`:**
| Сценарій | Рекомендація | Пояснення |
|----------|-------------|-----------|
| Live streaming з частими join | 20-30 пакетів | Нові клієнти швидше отримають метадані |
| VOD / файл | 40-60 пакетів | Менше накладних витрат, клієнти підключаються з початку |
| Низький бітрейт | 10-15 пакетів | Компенсація рідкісних ключових кадрів |
| Высокий бітрейт | 50-100 пакетів | Пат/ПМТ — мінімальний оверхед |

---

## 🔢 Управління лічильниками: версії та continuity

### wrappingCounter для різних цілей

```
┌─────────────────┬─────────────┬──────────┬─────────────────────────┐
| Лічильник       | wrapAt      | Діапазон | Призначення             |
├─────────────────┼─────────────┼──────────┼─────────────────────────┤
│ patVersion      | 0b11111=31  | 0-31     | Версія PAT таблиці     │
│ pmtVersion      | 0b11111=31  | 0-31     | Версія PMT таблиці     │
│ patCC           | 0b1111=15   | 0-15     | Continuity для PAT     │
│ pmtCC           | 0b1111=15   | 0-15     | Continuity для PMT     │
│ esContexts[].cc | 0b1111=15   | 0-15     | Continuity для кожного PID│
└─────────────────┴─────────────┴──────────┴─────────────────────────┘
```

### Логіка інкременту версій

```go
// PAT:
versionNumber := m.patVersion.get()
if m.pmUpdated {  // 🎯 зміни у programMap?
    versionNumber = m.patVersion.inc()  // інкремент + wrap при потребі
}
// ... згенерувати PAT з цією версією ...
m.pmUpdated = false  // 🎯 скинути флаг після успішної генерації

// Аналогічно для PMT:
if m.pmtUpdated {  // 🎯 зміни у списку потоків або PCR PID?
    versionNumber = m.pmtVersion.inc()
}
```

> 💡 **Ключова ідея**: Версія змінюється ТІЛЬКИ при зміні вмісту таблиці. Continuity counter інкрементується при КОЖНОМУ записі пакету. Це дозволяє клієнтам:
> - Детектувати оновлення метаданих (version change)
> - Виявляти втрати/дублікати пакетів (CC gap/duplicate)

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Ініціалізація Muxer для каналу

```go
// У вашому channel-aware сервері:
type ChannelMuxer struct {
    muxer    *astits.Muxer
    channelID string
    videoPID uint16
    audioPID uint16
}

func NewChannelMuxer(channelID string, videoPID, audioPID uint16, output io.Writer) *ChannelMuxer {
    muxer := astits.NewMuxer(context.Background(), output,
        WithTransportStreamID(uint16(hashChannelID(channelID))),  // унікальний ID на канал
        WithPMTPID(0x1000),
        MuxerOptTablesRetransmitPeriod(30),  // частіша ретрансмісія для live
    )
    
    // Зареєструвати потоки
    muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: videoPID,
        StreamType:    astits.StreamTypeH264Video,
    })
    muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: audioPID,
        StreamType:    astits.StreamTypeADTS,  // AAC
    })
    muxer.SetPCRPID(videoPID)  // PCR з відео-потоку
    
    return &ChannelMuxer{
        muxer:    muxer,
        channelID: channelID,
        videoPID: videoPID,
        audioPID: audioPID,
    }
}
```

### ✅ 2. Запис відео-кадру з синхронізацією

```go
func (cm *ChannelMuxer) WriteVideoFrame(frame []byte, pts, dts int64, isKeyFrame bool) error {
    // Підготувати PCR на основі PTS (конвертація 90kHz → 27MHz)
    pcrBase := pts * 300
    
    af := &astits.PacketAdaptationField{
        HasPCR:                true,
        PCR:                   astits.NewClockReference(pcrBase, 0),
        RandomAccessIndicator: isKeyFrame,  // 🎯 ключовий кадр = точка входу
    }
    
    pes := &astits.PESData{
        Data: frame,
        Header: &astits.PESHeader{
            StreamID: 0xE0,  // video stream ID
            OptionalHeader: &astits.PESOptionalHeader{
                PTS:             astits.NewClockReference(pts, 0),
                DTS:             astits.NewClockReference(dts, 0),
                PTSDTSIndicator: astits.PTSDTSIndicatorBothPresent,
            },
        },
    }
    
    _, err := cm.muxer.WriteData(&astits.MuxerData{
        PID:               cm.videoPID,
        AdaptationField:   af,
        PES:               pes,
    })
    return err
}
```

### ✅ 3. Синхронізація аудіо через спільний PTS

```go
func (cm *ChannelMuxer) WriteAudioChunk(chunk []byte, pts int64) error {
    // 🔹 Використовувати той самий PTS, що й у відео для синхронізації
    pes := &astits.PESData{
        Data: chunk,
        Header: &astits.PESHeader{
            StreamID: 0xC0,  // audio stream ID
            OptionalHeader: &astits.PESOptionalHeader{
                PTS:             astits.NewClockReference(pts, 0),
                PTSDTSIndicator: astits.PTSDTSIndicatorPTSOnly,  // аудіо: тільки PTS
            },
        },
    }
    
    // 🔹 Не вставляти PCR у аудіо — достатньо у відео-потоці
    _, err := cm.muxer.WriteData(&astits.MuxerData{
        PID: cm.audioPID,
        PES: pes,
        // AdaptationField: nil,
    })
    return err
}
```

### ✅ 4. Фіналізація сегмента з коректними таблицями

```go
func (cm *ChannelMuxer) FinalizeSegment() error {
    // 🔹 Примусово записати PAT/PMT на початку кожного сегмента
    // Це гарантує, що кожен .ts файл самодостатній
    _, err := cm.muxer.WriteTables()
    if err != nil {
        return fmt.Errorf("failed to write tables for segment: %w", err)
    }
    
    // 🔹 Опціонально: додати #EXT-X-DISCONTINUITY логіку
    // (це робиться на рівні HLS плейлиста, не TS-потоку)
    
    return nil
}
```

### ✅ 5. Обробка динамічних змін (напр., додавання субтитрів)

```go
func (cm *ChannelMuxer) AddSubtitleStream(pid uint16, language string) error {
    // Додати новий потік
    err := cm.muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: pid,
        StreamType:    astits.StreamTypeDVBSubtitles,
        ElementaryStreamDescriptors: []*astits.Descriptor{
            {
                Tag: astits.DescriptorTagISO639LanguageAndAudioType,
                ISO639LanguageAndAudioType: &astits.DescriptorISO639LanguageAndAudioType{
                    Language: language,
                    Type:     0x02,  // hearing impaired
                },
            },
        },
    })
    if err != nil {
        return err
    }
    
    // 🔹 Наступний виклик WriteTables() автоматично використає нову version_number
    // через флаг pmtUpdated=true, встановлений у AddElementaryStream
    
    // 🔹 Примусово записати оновлений PMT у потік
    _, err = cm.muxer.WriteTables()
    return err
}
```

### ✅ 6. Моніторинг стану муксера

```go
// monitoring.Monitor — метрики для Muxer:
type MuxerMetrics struct {
    TablesWritten      *prometheus.CounterVec  // скільки разів записано PAT/PMT
    PESPacketsWritten  *prometheus.CounterVec  // кількість PES-пакетів по PID
    BytesWritten       *prometheus.CounterVec  // загальний обсяг даних
    VersionChanges     *prometheus.CounterVec  // зміни версій таблиць
}

// У WriteData:
func (cm *ChannelMuxer) WriteVideoFrame(...) error {
    before := time.Now()
    n, err := cm.muxer.WriteData(...)
    latency := time.Since(before)
    
    if err == nil {
        metrics.PESPacketsWritten.WithLabelValues(cm.channelID, "video").Inc()
        metrics.BytesWritten.WithLabelValues(cm.channelID).Add(float64(n))
        metrics.WriteLatency.WithLabelValues(cm.channelID).Observe(latency.Seconds())
    }
    return err
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на авто-генерацію PID

```go
func TestMuxer_AutoGeneratePID(t *testing.T) {
    muxer := astits.NewMuxer(context.Background(), &bytes.Buffer{})
    
    // Додати потік без вказання PID
    err := muxer.AddElementaryStream(astits.PMTElementaryStream{
        StreamType: astits.StreamTypeH264Video,
        // ElementaryPID = 0 → авто-генерація
    })
    assert.NoError(t, err)
    
    // Перевірити, що PID згенеровано та унікальний
    es := muxer.PMT().ElementaryStreams[0]
    assert.GreaterOrEqual(t, es.ElementaryPID, astits.StartPID)  // >= 0x0100
    
    // Додати ще один — має отримати інший PID
    err = muxer.AddElementaryStream(astits.PMTElementaryStream{
        StreamType: astits.StreamTypeADTS,
    })
    assert.NoError(t, err)
    
    es2 := muxer.PMT().ElementaryStreams[1]
    assert.NotEqual(t, es.ElementaryPID, es2.ElementaryPID)
}
```

### 🔹 Тест на фрагментацію великого PES

```go
func TestMuxer_PESFragmentation(t *testing.T) {
    buf := &bytes.Buffer{}
    muxer := astits.NewMuxer(context.Background(), buf)
    
    muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: 0x100,
        StreamType:    astits.StreamTypeH264Video,
    })
    muxer.SetPCRPID(0x100)
    
    // Великий payload: 1000 байт → має розбитися на кілька пакетів
    largePayload := make([]byte, 1000)
    for i := range largePayload {
        largePayload[i] = byte(i % 256)
    }
    
    n, err := muxer.WriteData(&astits.MuxerData{
        PID: 0x100,
        PES: &astits.PESData{
            Data: largePayload,
            Header: &astits.PESHeader{
                StreamID: 0xE0,
            },
        },
    })
    assert.NoError(t, err)
    
    // Перевірити вирівнювання: загальний розмір кратний 188
    assert.Equal(t, 0, buf.Len()%188)
    
    // Перевірити, що перший пакет має PUSI=1
    firstPkt := buf.Bytes()[:188]
    assert.Equal(t, uint8(0x47), firstPkt[0])  // sync byte
    assert.True(t, firstPkt[1]&0x40 > 0)       // PUSI=1 (бит 6)
}
```

### 🔹 Тест на версію таблиць при динамічних змінах

```go
func TestMuxer_VersionIncrement_OnStreamChange(t *testing.T) {
    buf := &bytes.Buffer{}
    muxer := astits.NewMuxer(context.Background(), buf)
    
    // Початкова конфігурація
    muxer.AddElementaryStream(astits.PMTElementaryStream{PID: 0x100, Type: H264})
    muxer.SetPCRPID(0x100)
    
    // Перша генерація: version=0
    muxer.WriteTables()
    patV0 := extractVersionFromPAT(buf.Bytes()[:188])
    assert.Equal(t, uint8(0), patV0)
    
    // Додати аудіо → версія має змінитися
    buf.Reset()
    muxer.AddElementaryStream(astits.PMTElementaryStream{PID: 0x101, Type: AAC})
    muxer.WriteTables()
    
    patV1 := extractVersionFromPAT(buf.Bytes()[:188])
    assert.Equal(t, uint8(1), patV1)  // version інкрементувався
    
    // Повторна генерація без змін → версія НЕ змінюється
    buf.Reset()
    muxer.WriteTables()
    patV2 := extractVersionFromPAT(buf.Bytes()[:188])
    assert.Equal(t, uint8(1), patV2)  // та сама версія
}

func extractVersionFromPAT(packet []byte) uint8 {
    // Спрощений парсинг: версія у байті 8, біти 1-5
    // Реальна реалізація має парсити PSI секцію коректно
    return (packet[8] >> 1) & 0x1F
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `ErrPCRPIDInvalid` при `WriteTables()` | PCR PID не знайдено серед елементарних потоків | Викликати `SetPCRPID()` після `AddElementaryStream()` для відео-потоку |
| Дублікати пакетів у виході | Клієнти бачать "стрибки" у відтворенні | Перевірити, що `esContexts[PID].cc` не скидається між викликами `WriteData()` |
| PES не фрагментується коректно | Обрізані кадри, артефакти у плеєрі | Перевірити розрахунок `bytesAvailable` та `pesHeaderLengthCurrent` |
| Версія таблиць не інкрементується | Клієнти не бачать оновлень потоків | Переконатися, що `pmtUpdated=true` встановлюється при `Add/RemoveElementaryStream` |
| Stuffing не заповнює до 188 байт | Вихідні пакети < 188 байт → помилки парсингу | Перевірити цикл `for written < targetPacketSize` у `writePacket()` |

### Приклад коректного порядку ініціалізації:

```go
// ❌ Неправильно:
muxer.AddElementaryStream(videoES)
muxer.WriteTables()  // 🚨 ErrPCRPIDInvalid: PCR PID ще не встановлено!
muxer.SetPCRPID(videoPID)

// ✅ Правильно:
muxer.AddElementaryStream(videoES)
muxer.SetPCRPID(videoPID)  // 🎯 спочатку вказати PCR PID
muxer.WriteTables()        // ✅ тепер валідація пройде
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Ініціалізація з конфігурацією:
func initChannelMuxer(channelID string, output io.Writer) (*ChannelMuxer, error) {
    videoPID := allocatePID(channelID, "video")
    audioPID := allocatePID(channelID, "audio")
    
    muxer := astits.NewMuxer(context.Background(), output,
        WithTransportStreamID(channelIDToTSID(channelID)),
        WithPMTPID(0x1000),
        MuxerOptTablesRetransmitPeriod(30),
    )
    
    // Додати потоки
    if err := muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: videoPID,
        StreamType:    astits.StreamTypeH264Video,
    }); err != nil {
        return nil, err
    }
    if err := muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: audioPID,
        StreamType:    astits.StreamTypeADTS,
    }); err != nil {
        return nil, err
    }
    
    // 🔹 Критично: вказати PCR PID після додавання потоків
    muxer.SetPCRPID(videoPID)
    
    return &ChannelMuxer{muxer: muxer, channelID: channelID}, nil
}

// 2. Запис даних з таймінгами:
func writeFrame(cm *ChannelMuxer, pid uint16, data []byte, pts, dts int64, isKey bool) error {
    af := &astits.PacketAdaptationField{
        HasPCR:                pid == cm.videoPID,  // PCR тільки у відео
        PCR:                   astits.NewClockReference(pts*300, 0),
        RandomAccessIndicator: isKey,
    }
    
    pes := &astits.PESData{
        Data: data,
        Header: &astits.PESHeader{
            OptionalHeader: &astits.PESOptionalHeader{
                PTS: astits.NewClockReference(pts, 0),
                DTS: astits.NewClockReference(dts, 0),
            },
        },
    }
    
    _, err := cm.muxer.WriteData(&astits.MuxerData{
        PID: pid,
        AdaptationField: af,
        PES: pes,
    })
    return err
}

// 3. Фіналізація сегмента:
func finalizeSegment(cm *ChannelMuxer) error {
    // 🔹 Записати актуальні PAT/PMT на початку сегмента
    if _, err := cm.muxer.WriteTables(); err != nil {
        return err
    }
    // 🔹 Додати логику #EXT-X-DISCONTINUITY на рівні HLS плейлиста
    return nil
}
```

---

## 📊 Матриця станів Muxer

```
Подія                    | pmUpdated | pmtUpdated | Версія PAT | Версія PMT | CC PAT | CC PMT
─────────────────────────┼───────────┼────────────┼────────────┼────────────┼────────┼───────
NewMuxer()               | true      | false      | 0          | 0          | 0      | 0
AddElementaryStream()    | false     | true ✅    | -          | інкремент при генерації | - | -
SetPCRPID()              | false     | true ✅    | -          | інкремент при генерації | - | -
WriteTables() (без змін) | -         | -          | та сама    | та сама    | +1     | +1
WriteTables() (зі змінами)| -        | -          | +1*        | +1*        | +1     | +1
WriteData()              | -         | -          | -          | -          | -      | - (CC у esContext)

* тільки якщо відповідний updated=true перед викликом
```

---

## 📚 Корисні посилання

- [MPEG-TS PSI tables specification](https://www.iso.org/standard/61236.html)
- [astits Muxer API docs](https://pkg.go.dev/github.com/asticode/go-astits#Muxer)
- [PES packetization guide](https://en.wikipedia.org/wiki/Packetized_Elementary_Stream)

> 💡 **Ключова ідея**: `Muxer` — це "фінальний конструктор" вашого TS-потоку. Він гарантує:
> - 🎯 Валідність: коректні PAT/PMT з CRC32, правильні заголовки
> - ⏱️ Синхронізацію: точні PCR/PTS/DTS для A/V lock
> - 🔄 Динамічність: версії таблиць оновлюються при зміні потоків
> - 🧩 Фрагментацію: великі PES автоматично розбиваються на 188-байтові пакети

У вашому CCTV HLS пайплайні це дозволяє генерувати самодостатні .ts сегменти, сумісні з будь-яким плеєром, з коректною синхронізацією та підтримкою динамічних змін.

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати `Muxer` у ваш `segmentFinalizer` з підтримкою orphan audio merge
- 🧪 Написати integration-тест для перевірки сумісності виходу з ffprobe/Apple HLS Validator
- 📈 Додати метрики для моніторингу розмірів сегментів, інтервалів PCR та версій таблиць

🛠️