# 📦 Глибокий розбір `mpeg2/ts_muxer.go` — Transport Stream мультиплексор для MPEG-2

Це **реалізація мультиплексингу формату MPEG-2 Transport Stream (TS)** згідно специфікації ISO/IEC 13818-1. На відміну від Program Stream, TS оптимізований для передачі по ненадійних каналах (мовлення, IPTV, HLS) з фіксованим розміром пакетів (188 байт), вбудованою корекцією помилок та підтримкою мультиплексування кількох програм. Розберемо архітектурно:

---

## 🧱 1. Архітектура: від кадрів до транспортних пакетів

### 🔍 Потік даних у муксері:

```
[Raw Video/Audio Frames]
         ↓
[TSMuxer.Write()] ← цей файл!
├─ Конвертація часу: ms → 90 kHz clock (PTS/DTS × 90)
├─ Детекція ключових кадрів (IDR/IRAP) для random access
├─ Авто-вставка AUD (Access Unit Delimiter)
├─ Пакування у PES пакети (з заголовками)
├─ Розбиття PES на 188-байтові TS пакети
├─ Генерація PAT/PMT таблиць періодично
├─ Вставка PCR (Program Clock Reference) для синхронізації
         ↓
[TS пакети: 188 байт кожен] → .ts файл / UDP потік / HLS сегмент
```

### 🔑 Чому TS важливий для CCTV HLS:

```
HLS використовує MPEG-TS як основний формат сегментів:
• Кожен .ts файл — це послідовність 188-байтових пакетів
• Фіксований розмір дозволяє легке розділення/з'єднання
• PCR забезпечує синхронізацію аудіо/відео у плеєрах
• PAT/PMT описують структуру програми для декодерів

Ваш муксер дозволяє:
1. Генерувати валідні HLS-сегменти з сирих кадрів
2. Підтримувати live-трансляції через періодичні PAT/PMT
3. Забезпечувати сумісність з браузерами та плеєрами
```

---

## 📊 2. Структури даних: внутрішнє представлення програми

### 🔸 `pes_stream` — елементарний потік:

```go
type pes_stream struct {
    pid        uint16          // PID (Packet Identifier) цього потоку у TS
    cc         uint8           // Continuity counter (0-15) для детекції втрат
    streamtype TS_STREAM_TYPE  // Тип кодеку (0x1B=H.264, 0x0F=AAC, тощо)
}

func NewPESStream(pid uint16, cid TS_STREAM_TYPE) *pes_stream {
    return &pes_stream{
        pid:        pid,
        cc:         0,  // ініціалізація лічильника
        streamtype: cid,
    }
}
```

### 🔸 `table_pmt` — Program Map Table:

```go
type table_pmt struct {
    pid            uint16          // PID, на якому передається ця PMT
    cc             uint8           // Continuity counter для PMT пакетів
    pcr_pid        uint16          // PID потоку, що несе PCR (зазвичай відео)
    version_number uint8           // Версія таблиці (інкремент при зміні)
    pm             uint16          // Program number (ідентифікатор програми)
    streams        []*pes_stream   // Масив елементарних потоків програми
}
```

### 🔸 `table_pat` — Program Association Table:

```go
type table_pat struct {
    cc             uint8            // Continuity counter для PAT пакетів
    version_number uint8            // Версія таблиці
    pmts           []*table_pmt     // Масив PMT: кожна програма → своя PMT
}
```

### 🔸 `TSMuxer` — головний мультиплексор:

```go
type TSMuxer struct {
    pat        *table_pat      // PAT: корінь ієрархії програм
    stream_pid uint16          // Лічильник для наступного PID потоку
    pmt_pid    uint16          // Лічильник для наступного PID PMT
    pat_period uint64          // Останній час запису PAT (для періодичності)
    OnPacket   func(pkg []byte) // Callback: відправка 188-байтового пакету
}
```

> 💡 **Архітектурне рішення**: PAT містить масив PMT, кожна PMT — масив потоків. Це дозволяє підтримувати мультипрограмні потоки (наприклад, кілька каналів в одному транспортному потоці), хоча для CCTV зазвичай використовується одна програма.

---

## ➕ 3. `AddStream()` — реєстрація елементарних потоків

### 🔧 Логіка додавання:

```go
func (mux *TSMuxer) AddStream(cid TS_STREAM_TYPE) uint16 {
    // 1. Ініціалізація PAT, якщо потрібно
    if mux.pat == nil {
        mux.pat = NewTablePat()
    }
    
    // 2. Створення PMT, якщо ще немає (для першої програми)
    if len(mux.pat.pmts) == 0 {
        tmppmt := NewTablePmt()
        tmppmt.pid = mux.pmt_pid  // виділити PID для PMT
        tmppmt.pm = 1             // program_number = 1
        mux.pmt_pid++             // інкремент для наступної
        mux.pat.pmts = append(mux.pat.pmts, tmppmt)
    }
    
    // 3. Виділення PID для нового потоку
    sid := mux.stream_pid
    tmpstream := NewPESStream(sid, cid)
    mux.stream_pid++  // інкремент для наступного
    
    // 4. Додавання потоку до першої програми (індекс 0)
    mux.pat.pmts[0].streams = append(mux.pat.pmts[0].streams, tmpstream)
    
    return sid  // повернути PID для використання у Write()
}
```

### 🎯 Чому PID виділяються послідовно?

```
Специфікація MPEG-TS резервує діапазони PID:
• 0x0000: PAT (обов'язково)
• 0x0001-0x000F: зарезервовано
• 0x0010-0x1FFE: для елементарних потоків та PMT
• 0x1FFF: null packets (заповнення)

У вашому коді:
• stream_pid починається з 0x100 (256) → безпечний діапазон
• pmt_pid починається з 0x200 (512) → окремо від потоків
• Інкремент гарантує унікальність без колізій

Це спрощує управління, але не підтримує динамічне видалення потоків.
```

### 🎯 Практичне застосування у вашому пайплайні:

```go
// У TSFileWriter при ініціалізації запису:
func (w *TSFileWriter) Start(outputPath string, videoCodec, audioCodec codec.CodecID) error {
    muxer := NewTSMuxer()
    
    // Реєстрація потоків з отриманням PID
    videoPID := muxer.AddStream(convertCodecToTS(videoCodec))  // H264 → TS_STREAM_H264
    audioPID := muxer.AddStream(convertCodecToTS(audioCodec))  // AAC → TS_STREAM_AAC
    
    // Налаштування callback для запису у файл
    file, _ := os.Create(outputPath)
    muxer.OnPacket = func(pkg []byte) {
        file.Write(pkg)  // Запис 188 байт у файл
    }
    
    // Збереження muxer для подальших викликів Write()
    w.muxer = muxer
    w.videoPID = videoPID
    w.audioPID = audioPID
    
    return nil
}
```

---

## ✍️ 4. `Write()` — ядро мультиплексингу: від кадрів до TS пакетів

### 🔧 Підпис методу:

```go
func (mux *TSMuxer) Write(pid uint16, data []byte, pts uint64, dts uint64) error
// pid: PID потоку, отриманий від AddStream()
// data: сирі дані (Annex-B для відео, ADTS для аудіо)
// pts/dts: часові мітки у мілісекундах (конвертуються у 90 kHz всередині)
```

### 🔍 Крок 1: Пошук потоку за PID

```go
var whichpmt *table_pmt = nil
var whichstream *pes_stream = nil
for _, pmt := range mux.pat.pmts {
    for _, stream := range pmt.streams {
        if stream.pid == pid {
            whichpmt = pmt
            whichstream = stream
            break
        }
    }
}
if whichpmt == nil || whichstream == nil {
    return errors.New("not Found pid stream")  // ⚠️ Помилка: потік не зареєстровано
}
```

### 🔍 Крок 2: Встановлення PCR PID (якщо потрібно)

```go
// PCR (Program Clock Reference) має передаватись у потоці, що несе відео
if whichpmt.pcr_pid == 0 || (findPESIDByStreamType(whichstream.streamtype) == PES_STREAM_VIDEO && whichpmt.pcr_pid != pid) {
    whichpmt.pcr_pid = pid  // встановити цей потік як носій PCR
}
```

### 📐 Чому PCR важливий?

```
PCR — 42-бітне значення для синхронізації системного годинника декодера:
• Частота: 27 MHz → точність до 37 ns
• Передається у adaptation field TS пакету
• Декодер використовує PCR для:
  - Синхронізації аудіо/відео потоків
  - Відновлення тактової частоти після розривів
  - Запобігання buffer underflow/overflow

Специфікація вимагає:
• Передавати PCR щонайменше кожні 100 ms
• Передавати у потоці з найвищим пріоритетом (зазвичай відео)

У вашому коді: PCR вставляється автоматично у перший пакет кожного кадру, якщо pid == pcr_pid.
```

### 🔍 Крок 3: Детекція AUD у відео потоках

```go
var withaud bool = false
if whichstream.streamtype == TS_STREAM_H264 || whichstream.streamtype == TS_STREAM_H265 {
    codec.SplitFrame(data, func(nalu []byte) bool {
        if whichstream.streamtype == TS_STREAM_H264 {
            nalu_type := codec.H264NaluTypeWithoutStartCode(nalu)
            if nalu_type == codec.H264_NAL_AUD {
                withaud = true    // AUD знайдено → не вставляти дублікат
                return false      // зупинити ітерацію
            } else if codec.IsH264VCLNaluType(nalu_type) {
                return false      // зупинити після першого VCL
            }
            return true           // продовжити пошук
        }
        // Аналогічно для H.265...
    })
}
```

### 🔍 Крок 4: Періодична генерація PAT/PMT

```go
// PAT/PMT мають передаватись періодично (зазвичай кожні 100-500 ms)
if mux.pat_period == 0 || mux.pat_period+400 < dts {  // 400 ms інтервал
    mux.pat_period = dts
    if mux.pat_period == 0 {
        mux.pat_period = 1  // уникнути подвійного запису на старті
    }
    
    // Генерація PAT
    tmppat := NewPat()
    tmppat.Version_number = mux.pat.version_number
    for _, pmt := range mux.pat.pmts {
        tmppm := PmtPair{
            Program_number: pmt.pm,
            PID:            pmt.pid,
        }
        tmppat.Pmts = append(tmppat.Pmts, tmppm)
    }
    mux.writePat(tmppat)  // запис у callback
    
    // Генерація PMT для кожної програми
    for _, pmt := range mux.pat.pmts {
        tmppmt := NewPmt()
        tmppmt.Program_number = pmt.pm
        tmppmt.Version_number = pmt.version_number
        tmppmt.PCR_PID = pmt.pcr_pid
        for _, stream := range pmt.streams {
            var sp StreamPair
            sp.StreamType = uint8(stream.streamtype)
            sp.Elementary_PID = stream.pid
            sp.ES_Info_Length = 0
            tmppmt.Streams = append(tmppmt.Streams, sp)
        }
        mux.writePmt(tmppmt, pmt)
    }
}
```

### 🎯 Чому періодичність важлива?

```
Декодери можуть приєднатись до потоку в будь-який момент (наприклад, при seek).
Якщо PAT/PMT передаються рідко:
• Новий клієнт може чекати секунди на отримання таблиць
• Під час цього час відео/аудіо не можуть бути декодовані

Періодичність 100-500 ms гарантує:
• Швидке відновлення після розривів
• Підтримку live-переключення між каналами
• Сумісність з вимогами стандартів (DVB, ATSC)

У вашому коді: інтервал 400 ms — компроміс між overhead та швидкістю відновлення.
```

### 🔍 Крок 5: Детекція IDR кадрів для random access

```go
flag := false
switch whichstream.streamtype {
case TS_STREAM_H264:
    flag = codec.IsH264IDRFrame(data)  // детекція IDR через NAL types
case TS_STREAM_H265:
    flag = codec.IsH265IDRFrame(data)  // детекція IRAP через NAL types
}
```

### 🎯 Чому IDR важливий для TS?

```
IDR (Instantaneous Decoder Refresh) кадр — точка входу для декодера:
• Не залежить від попередніх кадрів (немає посилань)
• Дозволяє почати відтворення з будь-якого місця потоку
• Критичний для:
  - HLS seek (перемотування)
  - Відновлення після втрати пакетів
  - Мульти-бітрейт адаптації (переключення між варіантами)

У вашому коді:
• flag = true → встановлюється random_access_indicator у adaptation field
• Це сигналізує декодеру: "тут можна почати відтворення"
```

### 🔍 Крок 6: Пакування у PES та розбиття на TS пакети

```go
mux.writePES(whichstream, whichpmt, data, pts*90, dts*90, flag, withaud)
// Конвертація: ms → 90 kHz clock (×90)
```

---

## 📦 5. `writePES()` — пакування в елементарні пакети та розбиття на TS

### 🔧 Ключова логіка:

```go
func (mux *TSMuxer) writePES(pes *pes_stream, pmt *table_pmt, data []byte, pts uint64, dts uint64, idr_flag bool, withaud bool) {
    var firstPesPacket bool = true
    bsw := codec.NewBitStreamWriter(TS_PAKCET_SIZE)  // буфер на 188 байт
    
    for {  // Цикл: розбивати великі PES на кілька TS пакетів
        bsw.Reset()
        var tshdr TSPacket
        
        // Налаштування заголовку TS пакету
        if firstPesPacket {
            tshdr.Payload_unit_start_indicator = 1  // початок нового PES
        }
        tshdr.PID = pes.pid
        tshdr.Adaptation_field_control = 0x01  // тільки payload
        tshdr.Continuity_counter = pes.cc
        pes.cc = (pes.cc + 1) % 16  // інкремент лічильника
        
        headlen := 4  // базовий заголовок TS: 4 байти
        
        // === Адаптаційне поле для random access ===
        var adaptation *Adaptation_field = nil
        if firstPesPacket && idr_flag {
            adaptation = new(Adaptation_field)
            tshdr.Adaptation_field_control = tshdr.Adaptation_field_control | 0x20  // додати adaptation field
            adaptation.Random_access_indicator = 1  // сигнал: точка входу
            headlen += 2  // довжина adaptation field
        }

        // === Адаптаційне поле для PCR ===
        if firstPesPacket && pes.pid == pmt.pcr_pid {
            if adaptation == nil {
                adaptation = new(Adaptation_field)
                headlen += 2
            }
            tshdr.Adaptation_field_control = tshdr.Adaptation_field_control | 0x20
            adaptation.PCR_flag = 1
            // Розрахунок PCR: конвертація 90 kHz → 27 MHz
            var pcr_base uint64 = 0
            var pcr_ext uint16 = 0
            if dts == 0 {
                pcr_base = pts * 300 / 300  // спрощено: pts
                pcr_ext = uint16(pts * 300 % 300)
            } else {
                pcr_base = dts * 300 / 300
                pcr_ext = uint16(dts * 300 % 300)
            }
            adaptation.Program_clock_reference_base = pcr_base
            adaptation.Program_clock_reference_extension = pcr_ext
            headlen += 6  // PCR займає 6 байт
        }

        // === Формування PES заголовку (тільки для першого пакету) ===
        var payload []byte
        var pespkg *PesPacket = nil
        if firstPesPacket {
            oldheadlen := headlen
            headlen += 19  // базовий PES заголовок з PTS+DTS
            
            // Авто-вставка AUD, якщо відсутній
            if !withaud && pes.streamtype == TS_STREAM_H264 {
                headlen += 6  // H264_AUD_NALU = 6 байт
                payload = append(payload, H264_AUD_NALU...)
            } else if !withaud && pes.streamtype == TS_STREAM_H265 {
                payload = append(payload, H265_AUD_NALU...)  // 7 байт
                headlen += 7
            }
            
            // Налаштування PES пакету
            pespkg = NewPesPacket()
            pespkg.PTS_DTS_flags = 0x03  // PTS+DTS присутні
            pespkg.PES_header_data_length = 10  // 5 байт PTS + 5 байт DTS
            pespkg.Pts = pts
            pespkg.Dts = dts
            pespkg.Stream_id = uint8(findPESIDByStreamType(pes.streamtype))
            if idr_flag {
                pespkg.Data_alignment_indicator = 1  // вирівнювання для IDR
            }
            // Розрахунок довжини PES пакету
            if headlen-oldheadlen-6+len(data) > 0xFFFF {
                pespkg.PES_packet_length = 0  // необмежена довжина
            } else {
                pespkg.PES_packet_length = uint16(len(data) + headlen - oldheadlen - 6)
            }
        }

        // === Розбиття payload на 188-байтові пакети ===
        if len(data)+headlen < TS_PAKCET_SIZE {
            // Весь payload поміщається в один пакет
            if adaptation == nil {
                adaptation = new(Adaptation_field)
                headlen += 1
                if TS_PAKCET_SIZE-len(data)-headlen >= 1 {
                    headlen += 1
                } else {
                    adaptation.SingleStuffingByte = true
                }
            }
            adaptation.Stuffing_byte = uint8(TS_PAKCET_SIZE - len(data) - headlen)
            payload = append(payload, data...)
            data = data[:0]  // все оброблено
        } else {
            // Великий payload → взяти тільки частину
            payload = append(payload, data[0:TS_PAKCET_SIZE-headlen]...)
            data = data[TS_PAKCET_SIZE-headlen:]  // залишок для наступної ітерації
        }

        // === Серіалізація пакету ===
        if adaptation != nil {
            tshdr.Field = adaptation
            tshdr.Adaptation_field_control |= 0x02  // включити adaptation field
        }
        tshdr.EncodeHeader(bsw)  // записати 4-байтовий TS заголовок
        if pespkg != nil {
            pespkg.Pes_payload = payload
            pespkg.Encode(bsw)  // записати PES заголовок + payload
        } else {
            bsw.PutBytes(payload)  // тільки payload для продовження PES
        }
        
        firstPesPacket = false
        if mux.OnPacket != nil {
            if len(bsw.Bits()) != TS_PAKCET_SIZE {
                panic("packet ts packet failed")  // ⚠️ panic у продакшені!
            }
            mux.OnPacket(bsw.Bits())  // відправка 188 байт
        }
        if len(data) == 0 {
            break  // все оброблено
        }
    }
}
```

### 📐 Формула PCR розрахунку:

```
PCR = (DTS × 300) у 27 MHz clock:
• 90 kHz → 27 MHz: множник 300 (27,000,000 / 90,000 = 300)
• PCR_base: 33 біти (ціла частина)
• PCR_ext: 9 біт (дробова частина)

У вашому коді:
pcr_base = dts * 300 / 300  ← спрощено до dts (помилка!)
pcr_ext = uint16(dts * 300 % 300)

Правильно:
pcr_base = (dts * 300) >> 9  // старші 33 біти
pcr_ext = uint16((dts * 300) & 0x1FF)  // молодші 9 біт

Неправильний розрахунок → розсинхронізація аудіо/відео у декодерах.
```

### 🎯 Чому stuffing bytes потрібні?

```
TS пакети мають фіксований розмір 188 байт.
Якщо payload + заголовки < 188 → потрібно заповнити залишок.

Два способи:
1. Адаптаційне поле з stuffing_byte:
   • Гнучке: можна вставити будь-яку кількість байт
   • Використовується, коли потрібно додати метадані (PCR, random access)
   
2. SingleStuffingByte:
   • Спеціальний прапорець для одного байта 0xFF
   • Економія місця у заголовку

У вашому коді:
• Спочатку перевіряється, чи вистачає місця для адаптаційного поля
• Якщо ні → використовується SingleStuffingByte
• Це забезпечує коректне вирівнювання без переповнення пакету
```

---

## 🗂️ 6. `writePat()` / `writePmt()` — генерація таблиць

### 🔧 `writePat()`:

```go
func (mux *TSMuxer) writePat(pat *Pat) {
    var tshdr TSPacket
    tshdr.Payload_unit_start_indicator = 1  // початок секції
    tshdr.PID = 0  // PAT завжди на PID 0
    tshdr.Adaptation_field_control = 0x01  // тільки payload
    tshdr.Continuity_counter = mux.pat.cc
    mux.pat.cc = (mux.pat.cc + 1) % 16  // інкремент лічильника
    
    bsw := codec.NewBitStreamWriter(TS_PAKCET_SIZE)
    tshdr.EncodeHeader(bsw)  // 4-байтовий TS заголовок
    bsw.PutByte(0x00)        // pointer_field = 0 (секція починається відразу)
    pat.Encode(bsw)          // серіалізація PAT структури
    bsw.FillRemainData(0xff) // заповнення залишку 0xFF
    if mux.OnPacket != nil {
        mux.OnPacket(bsw.Bits())  // відправка 188 байт
    }
}
```

### 🔧 `writePmt()`:

```go
func (mux *TSMuxer) writePmt(pmt *Pmt, t_pmt *table_pmt) {
    var tshdr TSPacket
    tshdr.Payload_unit_start_indicator = 1
    tshdr.PID = t_pmt.pid  // PID з table_pmt
    tshdr.Adaptation_field_control = 0x01
    tshdr.Continuity_counter = t_pmt.cc
    t_pmt.cc = (t_pmt.cc + 1) % 16  // інкремент лічильника PMT
    
    bsw := codec.NewBitStreamWriter(TS_PAKCET_SIZE)
    tshdr.EncodeHeader(bsw)
    bsw.PutByte(0x00)  // pointer_field
    pmt.Encode(bsw)    // серіалізація PMT структури
    bsw.FillRemainData(0xff)
    if mux.OnPacket != nil {
        mux.OnPacket(bsw.Bits())
    }
}
```

### 🎯 Чому continuity counter важливий?

```
Continuity counter (4 біти у TS заголовку) — лічильник 0-15 для детекції втрат пакетів:
• Збільшується на 1 для кожного пакету з тим же PID
• Скидається на 0 після 15
• Декодер використовує його для:
  - Детекції пропущених пакетів (стрибок >1)
  - Детекції дублікатів (те саме значення підряд)
  - Відновлення синхронізації після помилок

У вашому коді:
• Кожен потік (pes_stream), PMT (table_pmt), PAT (table_pat) має окремий cc
• Інкремент: (cc + 1) % 16 — коректна реалізація
• Це забезпечує сумісність з вимогами стандарту
```

---

## 🐞 7. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Некоректний розрахунок PCR**:
   ```go
   // У writePES():
   pcr_base = pts * 300 / 300  // ← це просто pts, а не pts*300!
   pcr_ext = uint16(pts * 300 % 300)  // ← залишок від ділення на 300
   
   // Правильно:
   pcr_value := dts * 300  // конвертація 90 kHz → 27 MHz
   pcr_base = pcr_value >> 9  // старші 33 біти
   pcr_ext = uint16(pcr_value & 0x1FF)  // молодші 9 біт
   
   // Неправильний PCR → розсинхронізація аудіо/відео у плеєрах
   ```

2. **`panic` замість повернення помилки**:
   ```go
   // У writePES():
   if len(bsw.Bits()) != TS_PAKCET_SIZE {
       panic("packet ts packet failed")  // ← crash сервера!
   }
   
   // Краще:
   if len(bsw.Bits()) != TS_PAKCET_SIZE {
       return fmt.Errorf("invalid TS packet size: %d", len(bsw.Bits()))
   }
   ```

3. **Race condition у спільних структурах**:
   ```go
   // mux.pat, mux.pat.cc, stream.cc модифікуються у Write()
   // Якщо викликається з кількох горутин → data race!
   // Рішення: додати sync.Mutex до TSMuxer
   type TSMuxer struct {
       mu sync.Mutex
       // ...
   }
   func (mux *TSMuxer) Write(...) error {
       mux.mu.Lock()
       defer mux.mu.Unlock()
       // ... існуюча логіка ...
   }
   ```

4. **Жорстке припущення про одну програму**:
   ```go
   // У AddStream():
   mux.pat.pmts[0].streams = append(...)  // ← завжди індекс 0!
   
   // Це не підтримує додавання потоків до інших програм.
   // Краще: приймати program_number як параметр або підтримувати map[program_number]*table_pmt
   ```

5. **Необроблений випадок великих PES пакетів**:
   ```go
   // У розрахунку PES_packet_length:
   if headlen-oldheadlen-6+len(data) > 0xFFFF {
       pespkg.PES_packet_length = 0  // "необмежено"
   }
   
   // Але специфікація: 0 означає "пакет триває до кінця PES",
   // а не "необмежена довжина". Для великих даних потрібно розбивати на кілька PES.
   // Ваш код розбиває на кілька TS пакетів, але не на кілька PES → потенційна несумісність.
   ```

6. **Відсутня валідація вхідних даних**:
   ```go
   // Якщо data порожній або містить невалідні NAL units → помилки у подальшій обробці
   // Краще додати перевірку на початку Write():
   if len(data) == 0 {
       return nil  // або logger.Warn + return
   }
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для коректного розрахунку PCR
func CalculatePCR(timestamp90kHz uint64) (base uint64, ext uint16) {
    pcr_value := timestamp90kHz * 300  // 90 kHz → 27 MHz
    base = pcr_value >> 9              // старші 33 біти
    ext = uint16(pcr_value & 0x1FF)    // молодші 9 біт
    return
}

// 2. Метрики для моніторингу мультиплексингу
func (mux *TSMuxer) recordMetrics(streamType TS_STREAM_TYPE, packetSize int, pts uint64) {
    metrics.TSMuxBytesWritten.WithLabelValues(
        codec.StreamTypeString(streamType),
    ).Add(float64(packetSize))
    
    metrics.TSMuxPTS.Observe(float64(pts) / 90000.0)  // конвертація у секунди
}

// 3. Підтримка кількох програм
func (mux *TSMuxer) AddStreamToProgram(programNumber uint16, cid TS_STREAM_TYPE) uint16 {
    // Знайти або створити PMT для програми
    var pmt *table_pmt
    for _, p := range mux.pat.pmts {
        if p.pm == programNumber {
            pmt = p
            break
        }
    }
    if pmt == nil {
        pmt = NewTablePmt()
        pmt.pid = mux.pmt_pid
        pmt.pm = programNumber
        mux.pmt_pid++
        mux.pat.pmts = append(mux.pat.pmts, pmt)
    }
    // Додати потік до цієї програми
    sid := mux.stream_pid
    tmpstream := NewPESStream(sid, cid)
    mux.stream_pid++
    pmt.streams = append(pmt.streams, tmpstream)
    return sid
}

// 4. Юніт-тести для PCR розрахунку
func TestCalculatePCR(t *testing.T) {
    tests := []struct{
        input uint64
        wantBase uint64
        wantExt uint16
    }{
        {0, 0, 0},
        {90000, 27000000>>9, uint16(27000000 & 0x1FF)},  // 1 секунда
        {1<<33 - 1, (uint64(1<<33-1)*300)>>9, uint16((uint64(1<<33-1)*300) & 0x1FF)},
    }
    for _, tt := range tests {
        base, ext := CalculatePCR(tt.input)
        if base != tt.wantBase || ext != tt.wantExt {
            t.Errorf("CalculatePCR(%d) = (%d, %d), want (%d, %d)", 
                tt.input, base, ext, tt.wantBase, tt.wantExt)
        }
    }
}
```

---

## 🎯 8. Інтеграція з вашим CCTV HLS Processor

### 📍 У `TSFileWriter` — запис сегментів:

```go
type TSFileWriter struct {
    muxer    *TSMuxer
    file     *os.File
    videoPID uint16
    audioPID uint16
}

func (w *TSFileWriter) WriteFrame(codecid codec.CodecID, frame []byte, pts, dts uint32) error {
    // 1. Вибрати PID за кодеком
    pid := w.videoPID
    if codecid.IsAudio() {
        pid = w.audioPID
    }
    
    // 2. Викликати муксер (час уже у ms, конвертація всередині)
    return w.muxer.Write(pid, frame, uint64(pts), uint64(dts))
}

func (w *TSFileWriter) Close() error {
    // Фіналізація: записати кінцеві PAT/PMT якщо потрібно
    // (специфікація не вимагає, але можна додати для сумісності)
    return w.file.Close()
}
```

### 📍 У `HLSGenerator` — генерація сегментів:

```go
func (gen *HLSGenerator) WriteSegment(frames []Frame) error {
    // Створити тимчасовий файл для сегменту
    tmpfile, _ := os.CreateTemp("", "segment_*.ts")
    writer := NewTSFileWriter(tmpfile.Name(), frames[0].CodecID, frames[0].AudioCodecID)
    
    // Записати кожен кадр у TS формат
    for _, frame := range frames {
        writer.WriteFrame(frame.CodecID, frame.Data, frame.PTS, frame.DTS)
    }
    writer.Close()
    
    // Додати сегмент у плейлист
    gen.playlist.AddSegment(tmpfile.Name(), frames[len(frames)-1].PTS - frames[0].PTS)
    return nil
}
```

### 📍 У метриках — моніторинг якості мультиплексингу:

```go
func (mux *TSMuxer) recordHealthMetrics() {
    // Частота генерації PAT/PMT
    metrics.TSMuxPatInterval.Observe(float64(mux.pat_period) / 90000.0)  // у секундах
    
    // Розмір згенерованих пакетів (має бути завжди 188)
    metrics.TSMuxPacketSize.Observe(float64(TS_PAKCET_SIZE))
    
    // Детекція "завислих" лічильників continuity
    // (потрібно додати збереження останнього cc для кожного PID)
}
```

---

## 🧭 Висновок: чому TS муксер — критичний компонент для HLS

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **Авто-вставка AUD** | Гарантія синхронізації кадрів у декодерах | Розсинхронізація → артефакти відтворення у плеєрах |
| **IDR детекція + random_access_indicator** | Точки входу для seek/відновлення потоку | Неможливість перемотування у HLS, довге відновлення після помилок |
| **Періодичні PAT/PMT** | Швидке приєднання нових клієнтів до потоку | Затримка старту відтворення, несумісність з деякими плеєрами |
| **Коректний PCR розрахунок** | Синхронізація аудіо/відео на стороні клієнта | "Роз'їзд" звуку та відео, артефакти буфера у декодері |
| **Continuity counter** | Детекція втрат пакетів у ненадійних мережах | Невидимі помилки передачі → пошкоджені сегменти → скарги користувачів |
| **Розбиття великих кадрів на 188-байтові пакети** | Сумісність зі специфікацією TS | Невалідні пакети → відхилення декодерами, помилки у HLS плеєрах |

> 🔑 **Головна ідея**: Цей код — **міст між сирими кадрами та стандартом HLS**. Він гарантує, що ваші відео/аудіо дані будуть упаковані у валідний MPEG-TS формат, який можуть відтворювати будь-які сумісні плеєри — від браузерів до телевізорів. Без нього ви були б змушені покладатися на зовнішні інструменти (наприклад, ffmpeg) для конвертації, що додало б затримку, складність та залежність від зовнішніх процесів.

💡 **Фінальна порада**: 
1. Виправте розрахунок PCR через правильне бітове розділення 33+9 біт
2. Замініть `panic` на повернення помилок у критичних місцях
3. Додайте `sync.Mutex` для потокобезпеки при паралельному записі
4. Реалізуйте підтримку кількох програм через program_number параметр
5. Додайте юніт-тести для PCR, continuity counter та розбиття великих кадрів

Це перетворить цей муксер з "робочого прототипу" на "надійний компонент продакшен-рівня" для генерації HLS-сегментів у вашому CCTV HLS Processor.