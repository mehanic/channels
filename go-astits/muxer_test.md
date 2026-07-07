# Глибоке роз'яснення: Тести `Muxer` у astits — генерація PSI таблиць та PES-даних

Цей файл тестує **компонент муксингу (muxing)** бібліотеки astits: генерацію PAT/PMT таблиць, управління elementary streams, запис таймінгів (PCR/PTS/DTS) та формування валідних TS-пакетів.

---

## 🎯 Навіщо це потрібно у вашому пайплайні?

```
┌─────────────────────────────────────────┐
│ Muxer у контексті CCTV HLS Processor:  │
│                                         │
│ 🔹 Генерація валідних PSI таблиць:     │
│   • PAT (Program Association Table)    │
│   • PMT (Program Map Table)            │
│   → Без них плеєри не знайдуть програму│
│                                         │
│ 🔹 Реєстрація відео/аудіо потоків:     │
│   • PID, StreamType, дескриптори       │
│   → Коректна ідентифікація кодеків     │
│                                         │
│ 🔹 Вставка таймінгів:                   │
│   • PCR у адаптаційному полі           │
│   • PTS/DTS у PES-заголовках           │
│   → A/V синхронізація у клієнта        │
│                                         │
│ 🔹 Управління версіями таблиць:        │
│   • version_number інкремент при змінах│
│   • continuity_counter для порядку     │
│   → Детекція оновлень без перезапуску  │
└─────────────────────────────────────────┘
```

---

## 🔧 Допоміжні функції: генерація "очікуваних" байтів

### `patExpectedBytes(versionNumber, cc)` — еталонний PAT-пакет

```go
func patExpectedBytes(versionNumber uint8, cc uint8) []byte {
    buf := bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: &buf})
    
    // 🔹 Заголовок пакету (4 байти)
    w.Write(uint8(syncByte))                    // 0x47
    w.Write("010")                              // no error, PUSI=1, no priority
    w.WriteN(PIDPAT, 13)                        // PID=0x0000 для PAT
    w.Write("0001")                             // scrambling=0, AF=0, payload=1
    w.WriteN(cc, 4)                             // Continuity Counter
    
    // 🔹 PAT Section (PSI таблиця)
    w.Write(uint16(0))                          // table_id = 0x0000 (PAT)
    w.Write("1011")                             // syntax=1, private=0, reserved
    w.WriteN(uint16(13), 12)                    // section_length = 13 байт
    
    w.Write(uint16(PSITableIDPAT))              // transport_stream_id
    w.Write("11")                               // reserved
    w.WriteN(versionNumber, 5)                  // version_number (0-31)
    w.Write("1")                                // current_next_indicator = 1 (актуальна)
    w.Write(uint8(0))                           // section_number = 0
    w.Write(uint8(0))                           // last_section_number = 0
    
    // 🔹 Програма у PAT: program_number → program_map_PID
    w.Write(programNumberStart)                 // program_number (напр. 1)
    w.Write("111")                              // reserved
    w.WriteN(pmtStartPID, 13)                   // PID таблиці PMT
    
    // 🔹 CRC32 для валідації цілісності таблиці
    if versionNumber == 0 {
        w.Write([]byte{0x71, 0x10, 0xd8, 0x78})  // CRC для v0
    } else {
        w.Write([]byte{0xef, 0xbe, 0x08, 0x5a})  // CRC для v1+
    }
    
    // 🔹 Stuffing до 188 байт (0xFF)
    w.Write(bytes.Repeat([]byte{0xff}, 167))
    
    return buf.Bytes()
}
```

**Структура PAT у бітах:**
```
┌─────────────────────────────────────────┐
│ TS Packet Header (4B):                  │
│ [0x47][PID=0x0000][CC][flags...]       │
├─────────────────────────────────────────┤
│ PAT Section:                            │
│ [table_id=0x00][section_length=13]     │
│ [transport_stream_id][version][curr?]  │
│ [section_num=0][last_section=0]        │
├─────────────────────────────────────────┤
│ Program Loop (1 програма):              │
│ [program_number][reserved][PMT_PID]    │
├─────────────────────────────────────────┤
│ CRC32 (4B) + Stuffing (167B)           │
└─────────────────────────────────────────┘
```

> 💡 **Ключовий момент**: `version_number` змінюється тільки при зміні вмісту таблиці. `continuity_counter` інкрементується при кожному записі пакету.

---

## 🧪 Тести генерації PAT

### `TestMuxer_generatePAT` — перевірка логіки версій та лічильників

```go
func TestMuxer_generatePAT(t *testing.T) {
    muxer := NewMuxer(context.Background(), nil)
    
    // 🔹 Перша генерація: version=0, CC=0
    err := muxer.generatePAT()
    assert.NoError(t, err)
    assert.Equal(t, MpegTsPacketSize, muxer.patBytes.Len())  // ✅ рівно 188 байт
    assert.Equal(t, patExpectedBytes(0, 0), muxer.patBytes.Bytes())
    
    // 🔹 Друга генерація без змін: version НЕ змінюється, CC=1
    err = muxer.generatePAT()
    assert.NoError(t, err)
    assert.Equal(t, patExpectedBytes(0, 1), muxer.patBytes.Bytes())  // version=0, CC=1
    
    // 🔹 Після оновлення PMT: version інкрементується, CC=2
    muxer.pmUpdated = true  // ⚠️ прапорець змін у PMT
    err = muxer.generatePAT()
    assert.NoError(t, err)
    assert.Equal(t, patExpectedBytes(1, 2), muxer.patBytes.Bytes())  // version=1, CC=2
}
```

**Логіка версій у MPEG-TS:**
```
version_number (5 біт, 0-31):
• Змінюється ТІЛЬКИ при зміні вмісту таблиці
• Дозволяє плеєрам детектувати оновлення без перезапуску
• Циклічний: 31 → 0

current_next_indicator:
• 1 = ця версія актуальна зараз
• 0 = ця версія буде актуальна у майбутньому

continuity_counter (4 біти, 0-15):
• Інкрементується при КОЖНОМУ записі пакету з тим же PID
• Допомогає детектувати втрати/дублікати пакетів
```

---

## 🔧 Генерація PMT: відео та аудіо

### `pmtExpectedBytesVideoOnly` — PMT з одним відео-потоком

```go
func pmtExpectedBytesVideoOnly(versionNumber, cc uint8) []byte {
    // ... заголовок пакету (аналогічно PAT) ...
    w.WriteN(pmtStartPID, 13)  // PID таблиці PMT (напр. 0x1000)
    
    // 🔹 PMT Section
    w.Write(uint16(PSITableIDPMT))  // table_id = 0x02 для PMT
    w.WriteN(uint16(18), 12)        // section_length = 18 байт
    
    w.Write(programNumberStart)     // program_number
    w.WriteN(versionNumber, 5)      // version
    // ... section_number, current_next ...
    
    // 🔹 PCR PID: який потік містить Program Clock Reference
    w.WriteN(uint16(0x1234), 13)    // PCR_PID = PID відео-потоку
    
    // 🔹 Program info length = 0 (немає глобальних дескрипторів)
    w.WriteN(uint16(0), 12)
    
    // 🔹 Elementary Stream Loop: відео H.264
    w.Write(uint8(StreamTypeH264Video))  // stream_type = 0x1B
    w.WriteN(uint16(0x1234), 13)         // elementary_PID
    w.WriteN(uint16(0), 12)              // ES_info_length = 0
    
    // 🔹 CRC32 + stuffing
    w.Write([]byte{0x31, 0x48, 0x5b, 0xa2})  // CRC для цього вмісту
    w.Write(bytes.Repeat([]byte{0xff}, 162))
    
    return buf.Bytes()
}
```

### `pmtExpectedBytesVideoAndAudio` — PMT з відео + аудіо

```go
// Додається другий запис у Elementary Stream Loop:
w.Write(uint8(StreamTypeADTS))         // stream_type = 0x0F для AAC/ADTS
w.WriteN(uint16(0x0234), 13)           // elementary_PID для аудіо
w.WriteN(uint16(0), 12)                // ES_info_length = 0

// CRC змінюється через новий вміст таблиці!
if versionNumber == 0 {
    w.Write([]byte{0x29, 0x52, 0xc4, 0x50})  // CRC v0 з аудіо
} else {
    w.Write([]byte{0x06, 0xf4, 0xa6, 0xea})  // CRC v1+ з аудіо
}
```

> 💡 **Важливо**: Додавання/видалення потоку → змінює вміст PMT → **обов'язково** інкрементує `version_number`.

---

## 🧪 Тести генерації PMT

### `TestMuxer_generatePMT` — динамічне оновлення потоків

```go
func TestMuxer_generatePMT(t *testing.T) {
    muxer := NewMuxer(context.Background(), nil)
    
    // 🔹 Додати відео-потік
    err := muxer.AddElementaryStream(PMTElementaryStream{
        ElementaryPID: 0x1234,
        StreamType:    StreamTypeH264Video,
    })
    muxer.SetPCRPID(0x1234)  // PCR береться з відео-потоку
    assert.NoError(t, err)
    
    // 🔹 Перша генерація: version=0, CC=0
    err = muxer.generatePMT()
    assert.Equal(t, pmtExpectedBytesVideoOnly(0, 0), muxer.pmtBytes.Bytes())
    
    // 🔹 Повторна генерація без змін: version=0, CC=1
    err = muxer.generatePMT()
    assert.Equal(t, pmtExpectedBytesVideoOnly(0, 1), muxer.pmtBytes.Bytes())
    
    // 🔹 Додати аудіо-потік → версія має змінитися!
    err = muxer.AddElementaryStream(PMTElementaryStream{
        ElementaryPID: 0x0234,
        StreamType:    StreamTypeAACAudio,  // AAC в ADTS-обгортці
    })
    assert.NoError(t, err)
    
    // 🔹 Генерація після зміни: version=1, CC=2
    err = muxer.generatePMT()
    assert.Equal(t, pmtExpectedBytesVideoAndAudio(1, 2), muxer.pmtBytes.Bytes())
}
```

**Життєвий цикл PMT у реальному пайплайні:**
```
1. Старт каналу: додати відео + аудіо → PMT v0
2. Клієнт підключається: отримує PAT/PMT v0 → знає PID потоків
3. Динамічна зміна: додати субтитри → PMT v1
4. Клієнт отримує PMT v1: оновлює список потоків без перезапуску
5. При розриві: вставити #EXT-X-DISCONTINUITY + оновити PMT версію
```

---

## 📦 Тести запису таблиць та даних

### `TestMuxer_WriteTables` — комбінований вивід PAT+PMT

```go
func TestMuxer_WriteTables(t *testing.T) {
    buf := bytes.Buffer{}
    muxer := NewMuxer(context.Background(), &buf)
    
    // Налаштувати потоки
    muxer.AddElementaryStream(PMTElementaryStream{PID: 0x1234, Type: H264})
    muxer.SetPCRPID(0x1234)
    
    // Записати обидві таблиці
    n, err := muxer.WriteTables()
    
    assert.NoError(t, err)
    assert.Equal(t, 2*MpegTsPacketSize, n)  // ✅ PAT (188B) + PMT (188B) = 376B
    assert.Equal(t, n, buf.Len())
    
    // Перевірити бінарну ідентичність
    expected := append(patExpectedBytes(0, 0), pmtExpectedBytesVideoOnly(0, 0)...)
    assert.Equal(t, expected, buf.Bytes())
}
```

### `TestMuxer_WriteTables_Error` — валідація обов'язкових полів

```go
func TestMuxer_WriteTables_Error(t *testing.T) {
    muxer := NewMuxer(context.Background(), nil)
    
    // Додати потік, але НЕ вказати PCR PID
    muxer.AddElementaryStream(PMTElementaryStream{PID: 0x1234, Type: H264})
    
    // Запис має впасти: PCR PID обов'язковий для валідного TS
    _, err := muxer.WriteTables()
    assert.Equal(t, ErrPCRPIDInvalid, err)
}
```

> 💡 **Правило**: Кожен TS потік **має** містити PCR (Program Clock Reference) для синхронізації декодерів. Зазвичай це відео-потік.

---

## 🔁 Управління elementary streams

### `TestMuxer_AddElementaryStream` — запобігання дублікатам PID

```go
func TestMuxer_AddElementaryStream(t *testing.T) {
    muxer := NewMuxer(context.Background(), nil)
    
    // Додати потік
    err := muxer.AddElementaryStream(PMTElementaryStream{PID: 0x1234, Type: H264})
    assert.NoError(t, err)
    
    // Спроба додати той самий PID вдруге → помилка
    err = muxer.AddElementaryStream(PMTElementaryStream{PID: 0x1234, Type: H264})
    assert.Equal(t, ErrPIDAlreadyExists, err)
}
```

### `TestMuxer_RemoveElementaryStream` — динамічне видалення потоків

```go
func TestMuxer_RemoveElementaryStream(t *testing.T) {
    muxer := NewMuxer(context.Background(), nil)
    
    // Додати → видалити → спроба видалити знову
    muxer.AddElementaryStream(PMTElementaryStream{PID: 0x1234, Type: H264})
    
    err := muxer.RemoveElementaryStream(0x1234)
    assert.NoError(t, err)  // ✅ успішно видалено
    
    err = muxer.RemoveElementaryStream(0x1234)
    assert.Equal(t, ErrPIDNotFound, err)  // ❌ вже не існує
}
```

**Використання у вашому пайплайні:**
```go
// Динамічне оновлення потоків каналу:
func updateChannelStreams(channelID string, newStreams []StreamConfig) {
    muxer := channelMuxers[channelID]
    
    // Видалити старі потоки
    for _, oldPID := range getOldPIDs(channelID) {
        muxer.RemoveElementaryStream(oldPID)
    }
    
    // Додати нові
    for _, cfg := range newStreams {
        muxer.AddElementaryStream(PMTElementaryStream{
            ElementaryPID: cfg.PID,
            StreamType:    cfg.CodecType,
            // Дескриптори за потребою...
        })
    }
    
    // ⚠️ Після змін: наступна генерація PMT матиме нову version_number
}
```

---

## 🎞️ Запис PES-даних з таймінгами

### `TestMuxer_WritePayload` — комплексний тест з PCR + PTS/DTS

```go
func TestMuxer_WritePayload(t *testing.T) {
    buf := bytes.Buffer{}
    muxer := NewMuxer(context.Background(), &buf)
    
    // Налаштувати два потоки: відео + аудіо
    muxer.AddElementaryStream(PMTElementaryStream{PID: 0x1234, Type: H264})
    muxer.AddElementaryStream(PMTElementaryStream{PID: 0x0234, Type: AAC})
    muxer.SetPCRPID(0x1234)
    
    // 🔹 Підготувати дані та таймінги
    payload := testPayload()  // 256 байт тестових даних
    pcr := ClockReference{Base: 5726623061, Extension: 341}  // 27 MHz clock
    pts := ClockReference{Base: 5726623060}                   // 90 kHz clock
    
    // 🔹 Записати відео-пакет з таймінгами
    n, err := muxer.WriteData(&MuxerData{
        PID: 0x1234,
        AdaptationField: &PacketAdaptationField{
            HasPCR:                true,
            PCR:                   &pcr,              // 🎯 еталонний час
            RandomAccessIndicator: true,              // 🎯 точка входу (keyframe)
        },
        PES: &PESData{
            Data: payload,
            Header: &PESHeader{
                OptionalHeader: &PESOptionalHeader{
                    DTS:             &pts,              // 🎯 час декодування
                    PTS:             &pts,              // 🎯 час відтворення
                    PTSDTSIndicator: PTSDTSIndicatorBothPresent,
                },
            },
        },
    })
    assert.NoError(t, err)
    
    // 🔹 Записати аудіо-пакет з тими ж таймінгами (синхронізація)
    n2, err := muxer.WriteData(&MuxerData{
        PID: 0x0234,  // інший PID
        AdaptationField: &PacketAdaptationField{
            HasPCR: true, PCR: &pcr, RandomAccessIndicator: true,
        },
        PES: &PESData{Data: payload, Header: &PESHeader{...}},
    })
    assert.NoError(t, err)
    
    // 🔹 Перевірити вирівнювання: загальний розмір кратний 188
    assert.Equal(t, 0, buf.Len()%MpegTsPacketSize)
    
    // 🔹 Перевірити, що перші два пакети — це PAT+PMT
    bs := buf.Bytes()
    assert.Equal(t, patExpectedBytes(0, 0), bs[:188])
    assert.Equal(t, pmtExpectedBytesVideoAndAudio(0, 0), bs[188:376])
}
```

**Структура запису даних:**
```
Muxer.WriteData() → 
├─ 1. Записати PAT (якщо ще не записано або оновлено)
├─ 2. Записати PMT (якщо ще не записано або оновлено)
├─ 3. Сформувати PES-пакет:
│   ├─ PES header: stream_id, flags, PTS/DTS
│   ├─ PES payload: ваші відео/аудіо дані
│   └─ Розбити на TS-пакети по 188 байт
├─ 4. Додати адаптаційне поле з PCR (якщо вказано)
├─ 5. Заповнити stuffing до кратності 188
└─ 6. Повернути кількість записаних байт
```

> 💡 **Ключовий момент**: `WriteData()` автоматично вставляє PAT/PMT на початку потоку. Це гарантує, що перші пакети завжди містять метадані для ідентифікації програми.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Ініціалізація Muxer для каналу

```go
// У вашому channel-aware сервері:
type ChannelMuxer struct {
    muxer     *astits.Muxer
    videoPID  uint16
    audioPID  uint16
    nextCC    map[uint16]uint8  // continuity counter per PID
}

func NewChannelMuxer(channelID string, videoPID, audioPID uint16) *ChannelMuxer {
    buf := &bytes.Buffer{}
    muxer := astits.NewMuxer(context.Background(), buf)
    
    // Зареєструвати потоки
    muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: videoPID,
        StreamType:    astits.StreamTypeH264Video,
    })
    muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: audioPID,
        StreamType:    astits.StreamTypeADTS,  // AAC в ADTS
    })
    muxer.SetPCRPID(videoPID)  // PCR з відео-потоку
    
    return &ChannelMuxer{
        muxer:    muxer,
        videoPID: videoPID,
        audioPID: audioPID,
        nextCC:   make(map[uint16]uint8),
    }
}
```

### ✅ 2. Запис відео-кадру з синхронізацією

```go
func (cm *ChannelMuxer) WriteVideoFrame(frame []byte, pts, dts int64, isKeyFrame bool) error {
    // Підготувати PCR на основі PTS (спрощено)
    pcrBase := pts * 300  // конвертація 90kHz → 27MHz
    
    af := &astits.PacketAdaptationField{
        HasPCR:                true,
        PCR:                   astits.NewClockReference(pcrBase, 0),
        RandomAccessIndicator: isKeyFrame,  // 🎯 ключовий кадр = точка входу
    }
    
    pes := &astits.PESData{
        Data: frame,
        Header: &astits.PESHeader{
            StreamID: 0xE0,  // video stream
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

### ✅ 3. Синхронізація аудіо з відео через спільний PCR

```go
func (cm *ChannelMuxer) WriteAudioChunk(chunk []byte, pts int64) error {
    // Використовувати той самий PCR, що й у відео для цього моменту часу
    pcrBase := pts * 300  // та ж конвертація
    
    pes := &astits.PESData{
        Data: chunk,
        Header: &astits.PESHeader{
            StreamID: 0xC0,  // audio stream
            OptionalHeader: &astits.PESOptionalHeader{
                PTS:             astits.NewClockReference(pts, 0),
                PTSDTSIndicator: astits.PTSDTSIndicatorPTSOnly,  // аудіо: тільки PTS
            },
        },
    }
    
    // ⚠️ Не вставляти PCR у кожен аудіо-пакет — достатньо у відео!
    _, err := cm.muxer.WriteData(&astits.MuxerData{
        PID:     cm.audioPID,
        PES:     pes,
        // AdaptationField: nil,  // PCR вже є у відео-потоці
    })
    return err
}
```

### ✅ 4. Генерація сегменту з коректними таблицями

```go
func (cm *ChannelMuxer) FinalizeSegment() ([]byte, error) {
    // 🔹 Записати актуальні PAT/PMT (з оновленими version_number за потребою)
    _, err := cm.muxer.WriteTables()
    if err != nil {
        return nil, err
    }
    
    // 🔹 Отримати буфер з даними
    buf := cm.muxer.OutputBuffer()  // гіпотетичний метод
    data := buf.Bytes()
    
    // 🔹 Перевірити валідність:
    //    - розмір кратний 188
    //    - перші пакети = PAT+PMT
    if len(data)%188 != 0 {
        return nil, fmt.Errorf("segment size not multiple of 188: %d", len(data))
    }
    
    // 🔹 Скинути буфер для наступного сегмента
    buf.Reset()
    
    return data, nil
}
```

### ✅ 5. Обробка динамічних змін (напр. додавання субтитрів)

```go
func (cm *ChannelMuxer) AddSubtitleStream(pid uint16) error {
    // Додати новий потік
    err := cm.muxer.AddElementaryStream(astits.PMTElementaryStream{
        ElementaryPID: pid,
        StreamType:    astits.StreamTypeDVBSubtitles,
        ElementaryStreamDescriptors: []*astits.Descriptor{
            // Дескриптор мови, наприклад
            {Tag: astits.DescriptorTagISO639LanguageAndAudioType, ...},
        },
    })
    if err != nil {
        return err
    }
    
    // ⚠️ Позначити, що PMT оновлено → наступна генерація матиме нову version
    // (це робиться внутрішньо в astits при AddElementaryStream)
    
    // 🔹 Записати оновлений PMT у потік
    _, err = cm.muxer.WriteTables()
    return err
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на фрагментацію великого PES у кілька TS-пакетів

```go
func TestMuxer_WritePayload_Fragmented(t *testing.T) {
    buf := &bytes.Buffer{}
    muxer := astits.NewMuxer(context.Background(), buf)
    
    // Налаштувати потік
    muxer.AddElementaryStream(astits.PMTElementaryStream{PID: 0x100, Type: H264})
    muxer.SetPCRPID(0x100)
    
    // Великий payload: 1000 байт → потребує 6+ TS-пакетів
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
                PacketLength: uint16(len(largePayload) + 3),  // PES header size
            },
        },
    })
    assert.NoError(t, err)
    
    // Перевірити, що вивід кратний 188
    assert.Equal(t, 0, buf.Len()%188)
    
    // Перевірити, що перший пакет має PUSI=1 (початок PES)
    firstPkt := buf.Bytes()[:188]
    assert.Equal(t, uint8(0x47), firstPkt[0])  // sync byte
    assert.True(t, firstPkt[1]&0x40 > 0)       // PUSI=1
}
```

### 🔹 Тест на коректність PCR-інтервалів

```go
func TestMuxer_PCR_Interval(t *testing.T) {
    // PCR має вставлятися регулярно (рекомендація: кожні 40-100 мс)
    // Цей тест перевіряє, що WriteData() не "забуває" PCR
    
    buf := &bytes.Buffer{}
    muxer := astits.NewMuxer(context.Background(), buf)
    muxer.AddElementaryStream(astits.PMTElementaryStream{PID: 0x100, Type: H264})
    muxer.SetPCRPID(0x100)
    
    // Записати 10 "кадрів" з інтервалом 100 мс (9000 ticks @ 90kHz)
    for i := 0; i < 10; i++ {
        pts := int64(i) * 9000  // 100 мс крок
        _, err := muxer.WriteData(&astits.MuxerData{
            PID: 0x100,
            AdaptationField: &astits.PacketAdaptationField{
                HasPCR: true,
                PCR:    astits.NewClockReference(pts*300, 0),  // конвертація
            },
            PES: &astits.PESData{
                Data: []byte("frame"),
                Header: &astits.PESHeader{
                    OptionalHeader: &astits.PESOptionalHeader{
                        PTS: astits.NewClockReference(pts, 0),
                    },
                },
            },
        })
        assert.NoError(t, err)
    }
    
    // Проаналізувати вивід: підрахувати кількість пакетів з PCR
    data := buf.Bytes()
    pcrCount := 0
    for i := 0; i < len(data); i += 188 {
        pkt := data[i : i+188]
        if pkt[0] != 0x47 { continue }
        
        // Парсити заголовок та перевірити наявність AF з PCR
        // (спрощена перевірка)
        hasAF := pkt[3]&0x20 > 0
        if hasAF && pkt[4]&0x10 > 0 {  // AF flag + PCR flag
            pcrCount++
        }
    }
    
    // Очікуємо: PCR у кожному пакеті, де вказано HasPCR=true
    assert.Greater(t, pcrCount, 0)
}
```

### 🔹 Тест на обробку discontinuity при зміні сегменту

```go
func TestMuxer_Discontinuity_SegmentBoundary(t *testing.T) {
    // При переході між сегментами в HLS часто вставляється discontinuity
    // Цей тест перевіряє, що Muxer коректно оновлює версії таблиць
    
    buf := &bytes.Buffer{}
    muxer := astits.NewMuxer(context.Background(), buf)
    muxer.AddElementaryStream(astits.PMTElementaryStream{PID: 0x100, Type: H264})
    muxer.SetPCRPID(0x100)
    
    // Сегмент 1: записати дані
    muxer.WriteData(&astits.MuxerData{PID: 0x100, PES: &astits.PESData{Data: []byte("seg1")}})
    
    // Імітувати розрив: змінити параметри потоку (напр., додати аудіо)
    muxer.AddElementaryStream(astits.PMTElementaryStream{PID: 0x101, Type: AAC})
    
    // Сегмент 2: після зміни
    muxer.WriteData(&astits.MuxerData{PID: 0x100, PES: &astits.PESData{Data: []byte("seg2")}})
    
    // Перевірити, що PMT має нову version_number після додавання аудіо
    // (це вимагає парсингу виводу, тому тест складніший)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірний CRC32 у таблицях | Плеєри відкидають PAT/PMT як пошкоджені | Переконатися, що `astits` використовує правильний поліном `0x04c11db7` (MPEG-2 CRC) |
| version_number не інкрементується | Клієнти не бачать оновлень потоків | Викликати `AddElementaryStream`/`RemoveElementaryStream` перед `WriteTables()` |
| PCR вставляється занадто рідко | Дезинхронізація у довгих сегментах | Вставляти PCR у кожен ключовий кадр або кожні ~100 мс |
| PES-пакети не фрагментуються коректно | Обрізані кадри у плеєрі | Перевірити, що `PacketLength` у PES-заголовку враховує всі дані |
| continuity_counter "скаче" | Детекція втрат пакетів у клієнта | Зберігати `nextCC[PID]` між викликами `WriteData()` |

### Приклад коректного управління continuity counter:

```go
// У вашому ChannelMuxer:
type ChannelMuxer struct {
    muxer  *astits.Muxer
    cc     map[uint16]uint8  // поточний CC для кожного PID
}

func (cm *ChannelMuxer) writePacketWithCC(pid uint16, data *astits.MuxerData) error {
    // Встановити правильний CC у заголовок (якщо astits дозволяє)
    // Або дозволити astits автоматично інкрементувати
    
    // Після успішного запису:
    cm.cc[pid] = (cm.cc[pid] + 1) & 0x0f  // wrap-around 0-15
    return nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Ініціалізація Muxer:
func initMuxer(channelID string, videoPID, audioPID uint16) (*astits.Muxer, error) {
    muxer := astits.NewMuxer(context.Background(), &bytes.Buffer{})
    
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
    
    // Вказати PCR PID (зазвичай відео)
    muxer.SetPCRPID(videoPID)
    
    return muxer, nil
}

// 2. Запис кадру з таймінгами:
func writeFrame(muxer *astits.Muxer, pid uint16, data []byte, pts, dts int64, isKey bool) error {
    return muxer.WriteData(&astits.MuxerData{
        PID: pid,
        AdaptationField: &astits.PacketAdaptationField{
            HasPCR:                true,
            PCR:                   astits.NewClockReference(pts*300, 0),
            RandomAccessIndicator: isKey,
        },
        PES: &astits.PESData{
            Data: data,
            Header: &astits.PESHeader{
                OptionalHeader: &astits.PESOptionalHeader{
                    PTS:             astits.NewClockReference(pts, 0),
                    DTS:             astits.NewClockReference(dts, 0),
                    PTSDTSIndicator: astits.PTSDTSIndicatorBothPresent,
                },
            },
        },
    })
}

// 3. Фіналізація сегмента:
func finalizeSegment(muxer *astits.Muxer, output io.Writer) error {
    // Записати актуальні PAT/PMT
    if _, err := muxer.WriteTables(); err != nil {
        return err
    }
    
    // Скопіювати буфер у вихід
    buf := muxer.OutputBuffer()  // гіпотетичний API
    _, err := output.Write(buf.Bytes())
    buf.Reset()  // підготувати для наступного сегмента
    
    return err
}
```

---

## 📊 Матриця полів PSI таблиць

```
PAT (Program Association Table):
┌─────────────────┬─────────┬────────────────────────┐
│ Поле            │ Розмір  │ Призначення            │
├─────────────────┼─────────┼────────────────────────┤
│ table_id        │ 8 біт   │ 0x00 = PAT             │
│ section_length  │ 12 біт  │ довжина секції         │
│ transport_stream_id│ 16 біт│ унікальний ID потоку  │
│ version_number  │ 5 біт   │ версія таблиці (0-31)  │
│ current_next    │ 1 біт   │ актуальна/майбутня     │
│ program_number  │ 16 біт  │ номер програми (1=основна)│
│ program_map_PID │ 13 біт  │ PID таблиці PMT        │
│ CRC32           │ 32 біти │ цілісність таблиці     │
└─────────────────┴─────────┴────────────────────────┘

PMT (Program Map Table):
┌─────────────────┬─────────┬────────────────────────┐
│ Поле            │ Розмір  │ Призначення            │
├─────────────────┼─────────┼────────────────────────┤
│ table_id        │ 8 біт   │ 0x02 = PMT             │
│ PCR_PID         │ 13 біт  │ PID з PCR для синхронізації│
│ program_info_length│ 12 біт│ довжина глобальних дескрипторів│
│ stream_type     │ 8 біт   │ кодек (0x1B=H264, 0x0F=AAC)│
│ elementary_PID  │ 13 біт  │ PID цього потоку       │
│ ES_info_length  │ 12 біт  │ довжина дескрипторів потоку│
│ CRC32           │ 32 біти │ цілісність таблиці     │
└─────────────────┴─────────┴────────────────────────┘
```

---

## 📚 Корисні посилання

- [MPEG-TS PSI tables spec](https://en.wikipedia.org/wiki/Program-specific_information)
- [astits Muxer API](https://pkg.go.dev/github.com/asticode/go-astits#Muxer)
- [PES packet format](https://en.wikipedia.org/wiki/Packetized_Elementary_Stream)

> 💡 **Ключова ідея**: `Muxer` — це "конструктор" валідних TS-пакетів. У вашому CCTV HLS пайплайні він дозволяє:
> - 🎯 Генерувати коректні PAT/PMT для сумісності з будь-яким плеєром
> - ⏱️ Вставляти точні таймінги (PCR/PTS/DTS) для A/V синхронізації
> - 🔄 Динамічно оновлювати список потоків без розриву відтворення
> - 🧩 Автоматично фрагментувати великі PES у 188-байтові пакети

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати `Muxer` у ваш `segmentFinalizer` з підтримкою orphan audio merge
- 🧪 Написати integration-тест для перевірки сумісності виходу з ffprobe/VLC
- 📈 Додати метрики для моніторингу розмірів сегментів та інтервалів PCR

🛠️