# 📦 Глибокий розбір `mpeg2/ts_demuxer.go` — Transport Stream демуксер для MPEG-2

Це **реалізація демуксингу формату MPEG-2 Transport Stream (TS)** згідно специфікації ISO/IEC 13818-1. TS — це контейнерний формат, оптимізований для передачі по ненадійних каналах (мовлення, IPTV, HLS сегменти), з фіксованим розміром пакетів (188 байт) та вбудованою корекцією помилок. Розберемо архітектурно:

---

## 🧱 1. Архітектура: ієрархія TS → PES → елементарні потоки

### 🔍 Потік даних у демуксері:

```
[TS потік: 188-байтові пакети]
         ↓
[TSDemuxer.Input()] ← цей файл!
├─ Синхронізація: пошук 0x47 sync byte
├─ Парсинг TS заголовку: PID, payload_unit_start_indicator, тощо
├─ Обробка таблиць: PAT (Program Association Table), PMT (Program Map Table)
├─ Екстракція PES пакетів з елементарних потоків
├─ Розділення відео на кадри (H.264/H.265 NAL units)
├─ Конвертація часу: 90 kHz → milliseconds
         ↓
[Raw Video/Audio Frames] → callback OnFrame для подальшої обробки
```

### 🔑 Чому TS важливий для CCTV HLS:

```
HLS використовує MPEG-TS як основний формат сегментів:
• Кожен .ts файл — це окремий TS потік з одним program
• PAT/PMT таблиці описують, які PID містять відео/аудіо
• PES пакети всередині TS несуть сирі відео/аудіо дані з часовими мітками

Ваш демуксер дозволяє:
1. Приймати HLS-сегменти (.ts файли) без попередньої конвертації
2. Екстрагувати кадри для аналізу, транскодування або архівування
3. Підтримувати live-трансляції через інкрементальний парсинг
```

---

## 📦 2. Структури даних: від TS пакету до елементарного потоку

### 🔸 `pakcet_t` (опечатка: має бути `packet_t`) — буфер для накопичення кадрів:

```go
type pakcet_t struct {
    payload []byte  // накопичені дані кадру (може бути розрізаний між кількома TS пакетами)
    pts     uint64  // Presentation Time Stamp (90 kHz clock)
    dts     uint64  // Decoding Time Stamp (для B-frames)
}

func newPacket_t(size uint32) *pakcet_t {
    return &pakcet_t{
        payload: make([]byte, 0, size),  // початкова ємність для оптимізації
        pts:     0,
        dts:     0,
    }
}
```

### 🔸 `tsstream` — стан окремого елементарного потоку:

```go
type tsstream struct {
    cid     TS_STREAM_TYPE  // тип кодеку (наприклад, 0x1B=H.264, 0x0F=AAC)
    pes_sid PES_STREMA_ID   // PES stream_id (0xE0=відео, 0xC0=аудіо)
    pes_pkg *PesPacket      // поточний PES пакет, що парситься
    pkg     *pakcet_t       // буфер для накопичення повного кадру
}
```

### 🔸 `tsprogram` — програма (канал) у мультипрограмному потоці:

```go
type tsprogram struct {
    pn      uint16              // program_number з PAT/PMT
    streams map[uint16]*tsstream  // PID → tsstream: всі елементарні потоки програми
}
```

### 🔸 `TSDemuxer` — головний демуксер:

```go
type TSDemuxer struct {
    programs   map[uint16]*tsprogram  // PMT PID → tsprogram: підтримка кількох програм
    OnFrame    func(cid TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64)  // callback кадрів
    OnTSPacket func(pkg *TSPacket)  // callback для моніторингу/дебагу кожного TS пакету
}
```

> 💡 **Архітектурне рішення**: Карта `programs` за PMT PID дозволяє обробляти мультипрограмні потоки (наприклад, кілька каналів в одному транспортному потоці), хоча для CCTV зазвичай використовується одна програма на потік.

---

## 🔍 3. `Input()` — інкрементальний парсинг з синхронізацією

### 🔧 Ключова логіка циклу:

```go
func (demuxer *TSDemuxer) Input(r io.Reader) error {
    var buf []byte
    for {
        // 1. Обрізання буфера: залишити тільки останній неопрацьований пакет
        if len(buf) > TS_PAKCET_SIZE {  // TS_PAKCET_SIZE = 188
            buf = buf[TS_PAKCET_SIZE:]
        } else {
            // 2. Читання нового пакету з входу
            if err != nil {
                if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
                    break  // кінець потоку
                }
                return err
            }
            buf, err = demuxer.probe(r)  // синхронізація та читання
            if err != nil && buf == nil {
                if errors.Is(err, io.EOF) {
                    break
                }
                return err
            }
        }

        // 3. Парсинг заголовку TS пакету
        bs := codec.NewBitStream(buf[:TS_PAKCET_SIZE])
        var pkg TSPacket
        if err := pkg.DecodeHeader(bs); err != nil {
            return err
        }

        // 4. Обробка за типом пакету (PAT/PMT/елементарні потоки)
        if pkg.PID == uint16(TS_PID_PAT) {
            // === Program Association Table ===
            // ... парсинг PAT, реєстрація PMT PID ...
        } else if pkg.PID == TS_PID_Nil {
            continue  // Null packets — пропускаємо
        } else {
            // === Елементарні потоки або PMT ===
            for p, s := range demuxer.programs {
                if p == pkg.PID {  // PMT table
                    // ... парсинг PMT, реєстрація елементарних потоків ...
                } else {
                    // Елементарний потік (відео/аудіо)
                    for sid, stream := range s.streams {
                        if sid != pkg.PID {
                            continue
                        }
                        // Обробка PES пакету
                        if pkg.Payload_unit_start_indicator == 1 {
                            // Початок нового PES → парсити заголовок
                            err := stream.pes_pkg.Decode(bs)
                            if err != nil && !(errors.Is(err, errNeedMore) && stream.pes_pkg.Pes_payload != nil) {
                                return err
                            }
                            pkg.Payload = stream.pes_pkg
                        } else {
                            // Продовження PES → тільки payload
                            stream.pes_pkg.Pes_payload = bs.RemainData()
                            pkg.Payload = bs.RemainData()
                        }
                        // Диспетчеризація за типом потоку
                        stype := findPESIDByStreamType(stream.cid)
                        if stype == PES_STREAM_AUDIO {
                            demuxer.doAudioPesPacket(stream, pkg.Payload_unit_start_indicator)
                        } else if stype == PES_STREAM_VIDEO {
                            demuxer.doVideoPesPacket(stream, pkg.Payload_unit_start_indicator)
                        }
                    }
                }
            }
        }

        // 5. Callback для моніторингу
        if demuxer.OnTSPacket != nil {
            demuxer.OnTSPacket(&pkg)
        }
    }
    // 6. Фіналізація: відправити залишкові кадри
    demuxer.flush()
    return nil
}
```

### 🎯 Чому `probe()` критична для синхронізації?

```
TS пакети мають фіксований розмір 188 байт і починаються з 0x47.
Але вхідний потік може:
• Починатися з середини пакету (наприклад, при розрізанні файлу)
• Містити шум/помилки передачі
• Бути розрізаним на межі мережевих пакетів

`probe()` вирішує це через:
1. Читання 188 байт → перевірка, чи перший байт = 0x47
2. Якщо ні → читання ще 188 байт → пошук двох послідовних 0x47 на відстані 188 байт
3. Зсув буфера до знайденої синхронізації → повернення вирівняного пакету

Це гарантує, що подальший парсинг працює з коректно вирівняними пакетами.
```

### 🔧 Реалізація `probe()`:

```go
func (demuxer *TSDemuxer) probe(r io.Reader) ([]byte, error) {
    // 1. Спробувати прочитати один пакет
    buf := make([]byte, TS_PAKCET_SIZE, 2*TS_PAKCET_SIZE)  // ємність 2× для зсуву
    if _, err := io.ReadFull(r, buf); err != nil {
        return nil, err
    }
    
    // 2. Якщо перший байт = 0x47 → синхронізація знайдена
    if buf[0] == 0x47 {
        return buf, nil
    }
    
    // 3. Інакше: прочитати ще один пакет для пошуку синхронізації
    buf = buf[:2*TS_PAKCET_SIZE]  // розширити до 376 байт
    if _, err := io.ReadFull(r, buf[TS_PAKCET_SIZE:]); err != nil {
        return nil, err
    }
    
    // 4. Пошук двох послідовних 0x47 на відстані 188 байт
LOOP:
    i := 0
    for ; i < TS_PAKCET_SIZE; i++ {
        if buf[i] == 0x47 && buf[i+TS_PAKCET_SIZE] == 0x47 {
            break  // знайдено синхронізацію на позиції i
        }
    }
    
    // 5. Обробка результатів пошуку
    if i == 0 {
        return buf, nil  // синхронізація на початку
    } else if i < TS_PAKCET_SIZE {
        // Зсув: скопіювати з позиції i до початку, дочитати решту
        copy(buf, buf[i:])
        if _, err := io.ReadFull(r, buf[2*TS_PAKCET_SIZE-i:]); err != nil {
            return buf[:TS_PAKCET_SIZE], err
        } else {
            return buf, nil
        }
    } else {
        // Синхронізація не знайдена в перших 188 байт → повторити з другої половини
        copy(buf, buf[TS_PAKCET_SIZE:])
        if _, err := io.ReadFull(r, buf[TS_PAKCET_SIZE:]); err != nil {
            return buf[:TS_PAKCET_SIZE], err
        }
        goto LOOP  // рекурсивний пошук
    }
}
```

> 💡 **Оптимізація**: `goto LOOP` використовується для уникнення вкладеності, але може бути замінено на цикл `for` для кращої читабельності.

---

## 🗂️ 4. Обробка таблиць: PAT та PMT

### 🔸 PAT (Program Association Table, PID = 0x0000):

```go
if pkg.PID == uint16(TS_PID_PAT) {
    // Пропустити pointer_field, якщо це початок секції
    if pkg.Payload_unit_start_indicator == 1 {
        bs.SkipBits(8)
    }
    
    // Парсинг секції PAT
    pkg.Payload, err = ReadSection(TS_TID_PAS, bs)
    if err != nil {
        return err
    }
    
    // Обробка записів PAT: program_number → PMT PID
    pat := pkg.Payload.(*Pat)
    for _, pmt := range pat.Pmts {
        if pmt.Program_number != 0x0000 {  // 0x0000 = NIT, пропускаємо
            // Зареєструвати PMT PID для подальшого парсингу
            if _, found := demuxer.programs[pmt.PID]; !found {
                demuxer.programs[pmt.PID] = &tsprogram{
                    pn: 0,  // program_number заповниться при парсингу PMT
                    streams: make(map[uint16]*tsstream),
                }
            }
        }
    }
}
```

### 🔸 PMT (Program Map Table, PID з PAT):

```go
if p == pkg.PID {  // p — це PMT PID з demuxer.programs
    // Пропустити pointer_field
    if pkg.Payload_unit_start_indicator == 1 {
        bs.SkipBits(8)
    }
    
    // Парсинг секції PMT
    pkg.Payload, err = ReadSection(TS_TID_PMS, bs)
    if err != nil {
        return err
    }
    
    pmt := pkg.Payload.(*Pmt)
    s.pn = pmt.Program_number  // зберегти program_number
    
    // Зареєструвати елементарні потоки програми
    for _, ps := range pmt.Streams {
        if _, found := s.streams[ps.Elementary_PID]; !found {
            s.streams[ps.Elementary_PID] = &tsstream{
                cid:     TS_STREAM_TYPE(ps.StreamType),  // тип кодеку з PMT
                pes_sid: findPESIDByStreamType(TS_STREAM_TYPE(ps.StreamType)),  // PES stream_id
                pes_pkg: NewPesPacket(),  // новий буфер для PES парсингу
            }
        }
    }
}
```

### 🎯 Чому реєстрація через PAT/PMT критична?

```
TS потік може містити десятки програм (каналів) з сотнями елементарних потоків.
Без PAT/PMT демуксер не знає:
• Які PID належать якій програмі
• Який PID містить відео, а який — аудіо
• Який кодек використовується у кожному потоці

Реєстрація через PAT/PMT дозволяє:
1. Фільтрувати тільки потрібні програми/потоки
2. Автоматично визначати тип кодеку для правильного парсингу
3. Підтримувати динамічні зміни (наприклад, додавання нового аудіо треку)
```

---

## 🎞️ 5. Обробка відео потоків: `doVideoPesPacket()` + `splitH264Frame()`/`splitH265Frame()`

### 🔧 `doVideoPesPacket()` — накопичення та детекція кадрів:

```go
func (demuxer *TSDemuxer) doVideoPesPacket(stream *tsstream, start uint8) {
    // Підтримка тільки H.264/H.265
    if stream.cid != TS_STREAM_H264 && stream.cid != TS_STREAM_H265 {
        return
    }
    
    // Ініціалізація буфера кадру при першому пакеті
    if stream.pkg == nil {
        stream.pkg = newPacket_t(1024)  // початкова ємність 1KB
        stream.pkg.pts = stream.pes_pkg.Pts
        stream.pkg.dts = stream.pes_pkg.Dts
    }
    
    // Додати нові дані до буфера
    stream.pkg.payload = append(stream.pkg.payload, stream.pes_pkg.Pes_payload...)
    
    // Спроба розділити буфер на повні кадри
    update := false
    if stream.cid == TS_STREAM_H264 {
        update = demuxer.splitH264Frame(stream)
    } else {
        update = demuxer.splitH265Frame(stream)
    }
    
    // Якщо знайдено новий кадр → оновити PTS/DTS для наступного
    if update {
        stream.pkg.pts = stream.pes_pkg.Pts
        stream.pkg.dts = stream.pes_pkg.Dts
    }
}
```

### 🔧 `splitH264Frame()` — детекція меж кадрів за NAL units:

```go
func (demuxer *TSDemuxer) splitH264Frame(stream *tsstream) bool {
    data := stream.pkg.payload
    start, sct := codec.FindStartCode(data, 0)  // пошук 0x000001/0x00000001
    datalen := len(data)
    
    vcl := 0  // лічильник VCL (Video Coding Layer) NAL units
    newAcessUnit := false  // прапорець: чи знайдено початок нового кадру
    needUpdate := false  // чи потрібно оновити PTS/DTS
    frameBeg := start  // початок поточного кадру
    if frameBeg < 0 {
        frameBeg = 0
    }
    
    for start < datalen {
        // Перевірка меж буфера
        if start < 0 || len(data)-start <= int(sct)+1 {
            break
        }

        // Визначення типу NAL unit
        naluType := codec.H264NaluTypeWithoutStartCode(data[start+int(sct):])
        switch naluType {
        case codec.H264_NAL_AUD, codec.H264_NAL_SPS,
             codec.H264_NAL_PPS, codec.H264_NAL_SEI:
            // Не-VCL NAL units: можуть маркувати початок нового кадру
            if vcl > 0 {  // якщо вже були відео-дані → новий кадр
                newAcessUnit = true
            }
        case codec.H264_NAL_I_SLICE, codec.H264_NAL_P_SLICE,
             codec.H264_NAL_SLICE_A, codec.H264_NAL_SLICE_B, codec.H264_NAL_SLICE_C:
            // VCL NAL units: містять відео-дані
            if vcl > 0 {
                // Перший байт payload після заголовку: біт 7 = first_mb_in_slice_flag
                if data[start+int(sct)+1]&0x80 > 0 {
                    newAcessUnit = true  // first_mb_in_slice = 1 → новий кадр
                }
            } else {
                vcl++  // перший VCL у кадрі
            }
        }

        // Якщо знайдено початок нового кадру → відправити попередній
        if vcl > 0 && newAcessUnit {
            if demuxer.OnFrame != nil {
                // Видалити AUD NAL units з payload (не потрібні для відтворення)
                audLen := 0
                codec.SplitFrameWithStartCode(data[frameBeg:start], func(nalu []byte) bool {
                    if codec.H264NaluType(nalu) == codec.H264_NAL_AUD {
                        audLen += len(nalu)
                    }
                    return false
                })
                // Відправити кадр у callback (без AUD)
                demuxer.OnFrame(stream.cid, data[frameBeg+audLen:start], 
                               stream.pkg.pts/90, stream.pkg.dts/90)
            }
            frameBeg = start  // початок нового кадру
            needUpdate = true
            vcl = 0
            newAcessUnit = false
        }
        
        // Перейти до наступного NAL unit
        end, sct2 := codec.FindStartCode(data, start+3)
        if end < 0 {
            break
        }
        start = end
        sct = sct2
    }

    // Зберегти залишок (неповний кадр) для наступного виклику
    if frameBeg == 0 {
        return needUpdate
    }
    copy(stream.pkg.payload, data[frameBeg:datalen])
    stream.pkg.payload = stream.pkg.payload[0 : datalen-frameBeg]
    return needUpdate
}
```

### 🎯 Логіка детекції меж кадрів:

```
H.264 кадр = один або кілька NAL units, що починаються з:
• AUD (Access Unit Delimiter) — опціональний маркер початку
• SPS/PPS — параметри (тільки на початку потоку або при зміні)
• VCL NAL units (I/P/B slices) — відео-дані

Детекція нового кадру:
1. Знайдено не-VCL NAL (SPS/PPS/SEI) після VCL → новий кадр
2. Знайдено VCL NAL з first_mb_in_slice_flag = 1 → новий кадр
3. Знайдено AUD після VCL → новий кадр

Це дозволяє коректно розділяти потік на кадри навіть якщо:
• Кадр розрізаний між кількома TS пакетами
• Потік починається з середини кадру
• Присутні помилки передачі (пропуск деяких NAL units)
```

### 🎯 Конвертація часу: `pts/90`

```
MPEG-TS використовує 90 kHz clock для PTS/DTS:
• 90,000 ticks = 1 секунда
• 1 tick = 1/90000 секунди ≈ 11.11 мкс

Для зручності у пайплайні конвертуємо у мілісекунди:
• 90,000 / 90 = 1,000 → 1 ms
• pts/90 = кількість мілісекунд від початку потоку

Це спрощує:
• Розрахунок тривалості сегментів для HLS (#EXTINF)
• Синхронізацію аудіо/відео за спільною шкалою часу
• Інтеграцію з іншими компонентами, що очікують ms
```

---

## 🎵 6. Обробка аудіо потоків: `doAudioPesPacket()`

### 🔧 Проста стратегія: розділення за PTS

```go
func (demuxer *TSDemuxer) doAudioPesPacket(stream *tsstream, start uint8) {
    // Підтримка AAC, MP3, MPEG-1/2 audio
    if stream.cid != TS_STREAM_AAC && stream.cid != TS_STREAM_AUDIO_MPEG1 && stream.cid != TS_STREAM_AUDIO_MPEG2 {
        return
    }

    // Ініціалізація буфера
    if stream.pkg == nil {
        stream.pkg = newPacket_t(1024)
        stream.pkg.pts = stream.pes_pkg.Pts
        stream.pkg.dts = stream.pes_pkg.Dts
    }

    // Детекція нового кадру за зміною PTS або початком PES
    if len(stream.pkg.payload) > 0 && (start == 1 || stream.pes_pkg.Pts != stream.pkg.pts) {
        // PTS змінився або новий PES → попередній кадр завершено
        if demuxer.OnFrame != nil {
            demuxer.OnFrame(stream.cid, stream.pkg.payload, stream.pkg.pts/90, stream.pkg.dts/90)
        }
        stream.pkg.payload = stream.pkg.payload[:0]  // очистити буфер
    }
    
    // Додати нові дані
    stream.pkg.payload = append(stream.pkg.payload, stream.pes_pkg.Pes_payload...)
    stream.pkg.pts = stream.pes_pkg.Pts
    stream.pkg.dts = stream.pes_pkg.Dts
}
```

### 🎯 Чому аудіо простіше за відео?

```
Аудіо кодеки (AAC, MP3) мають:
• Фіксовану тривалість кадрів (наприклад, 1024 семпли для AAC)
• Чіткі межі кадрів у бітстрімі (ADTS заголовки для AAC)
• Відсутність B-frames → PTS == DTS

Тому достатньо:
1. Накопичувати дані у буфері
2. Відправляти кадр при зміні PTS (новий аудіо-кадр)
3. Не потрібна складна детекція меж за вмістом

Це значно спрощує парсинг порівняно з відео, де межі кадрів визначаються семантикою NAL units.
```

---

## 🔄 7. `flush()` — фіналізація накопичених даних

### 🔧 Логіка завершення:

```go
func (demuxer *TSDemuxer) flush() {
    for _, pm := range demuxer.programs {
        for _, stream := range pm.streams {
            if stream.pkg == nil || len(stream.pkg.payload) == 0 {
                continue  // порожній буфер → нічого робити
            }
            
            if demuxer.OnFrame == nil {
                continue  // немає callback → нікуди відправляти
            }
            
            // Спеціальна обробка для відео: видалення AUD
            if stream.cid == TS_STREAM_H264 || stream.cid == TS_STREAM_H265 {
                audLen := 0
                codec.SplitFrameWithStartCode(stream.pkg.payload, func(nalu []byte) bool {
                    if stream.cid == TS_STREAM_H264 {
                        if codec.H264NaluType(nalu) == codec.H264_NAL_AUD {
                            audLen += len(nalu)
                        }
                    } else {
                        if codec.H265NaluType(nalu) == codec.H265_NAL_AUD {
                            audLen += len(nalu)
                        }
                    }
                    return false
                })
                // Відправити залишкові дані (без AUD)
                demuxer.OnFrame(stream.cid, stream.pkg.payload[audLen:], 
                               stream.pkg.pts/90, stream.pkg.dts/90)
            } else {
                // Аудіо: відправити як є
                demuxer.OnFrame(stream.cid, stream.pkg.payload, 
                               stream.pkg.pts/90, stream.pkg.dts/90)
            }
            stream.pkg = nil  // очистити буфер
        }
    }
}
```

### 🎯 Коли викликати `flush()`:

| Сценарій | Коли викликати | Наслідки без flush() |
|----------|---------------|---------------------|
| **Кінець файлу** | Після останнього `Input()` | Втрата останніх кадрів у буферах |
| **Зміна потоку** | Перед перемиканням на новий джерело | "Залипання" старих даних у новому контексті |
| **Періодичний експорт** | Кожні N секунд для low-latency | Затримка доставки кадрів до клієнта |
| **Обробка помилок** | При детекції розриву потоку | Накопичення пошкоджених даних |

> 💡 **Порада**: У real-time сценаріях викликайте `flush()` періодично (наприклад, кожні 100ms) для мінімізації затримки, навіть якщо це означає відправку неповних кадрів.

---

## 🐞 8. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Опечатка у назві типу**:
   ```go
   type pakcet_t struct { ... }  // ← має бути packet_t
   // Це ламає консистентність, ускладнює пошук та автодоповнення в IDE
   ```

2. **Відсутня валідація `Payload_unit_start_indicator`**:
   ```go
   // У обробці PES:
   if pkg.Payload_unit_start_indicator == 1 {
       err := stream.pes_pkg.Decode(bs)
       // Але якщо Decode() повертає errNeedMore, а Pes_payload != nil,
       // ми ігноруємо помилку → можлива втрата даних при неповному пакеті
   }
   ```

3. **Необроблений випадок `frameBeg < 0` у `splitH264Frame()`**:
   ```go
   frameBeg := start
   if frameBeg < 0 {
       frameBeg = 0  // ← але start може бути -1, якщо не знайдено start code!
   }
   // Краще: перевірити start перед використанням
   if start < 0 {
       return false  // немає start code → неможливо розділити
   }
   ```

4. **Витік пам'яті у `stream.pkg.payload`**:
   ```go
   // При копіюванні залишку:
   copy(stream.pkg.payload, data[frameBeg:datalen])
   stream.pkg.payload = stream.pkg.payload[0 : datalen-frameBeg]
   // Але якщо frameBeg великий, а datalen-frameBeg малий,
   // слайс все ще посилається на великий масив → витік пам'яті
   // Краще: створити новий слайс з потрібною ємністю
   stream.pkg.payload = append([]byte(nil), data[frameBeg:datalen]...)
   ```

5. **Race condition у `programs`/`streams` maps**:
   ```go
   // Якщо Input() викликається з кількох горутин → data race!
   // Рішення: додати sync.RWMutex до TSDemuxer
   type TSDemuxer struct {
       mu sync.RWMutex
       programs map[uint16]*tsprogram
       // ...
   }
   ```

6. **Некоректна обробка `errNeedMore` для аудіо**:
   ```go
   // У doAudioPesPacket() немає перевірки на errNeedMore,
   // тому неповні PES пакети можуть призвести до відправки пошкоджених кадрів
   // Краще: буферизувати до отримання повного PES
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для безпечної конвертації часу
func TsTimeToMs(time90kHz uint64) uint32 {
    return uint32((time90kHz + 45) / 90)  // округлення замість відсічення
}

// 2. Метрики для моніторингу демуксингу
func (demuxer *TSDemuxer) recordMetrics(streamType TS_STREAM_TYPE, payloadSize int) {
    metrics.TSDemuxBytesReceived.WithLabelValues(
        codec.StreamTypeString(streamType),
    ).Add(float64(payloadSize))
    
    metrics.TSDemuxFramesProcessed.Inc()
}

// 3. Обмеження розміру буфера для захисту від переповнення
const MAX_FRAME_BUFFER = 10 * 1024 * 1024  // 10MB

func (demuxer *TSDemuxer) safeAppend(stream *tsstream, data []byte) error {
    if len(stream.pkg.payload) + len(data) > MAX_FRAME_BUFFER {
        // Скинути буфер та логувати попередження
        logger.Warn("frame buffer overflow, resetting", 
                   "size", len(stream.pkg.payload), 
                   "adding", len(data))
        stream.pkg.payload = stream.pkg.payload[:0]
        return errors.New("frame buffer overflow")
    }
    stream.pkg.payload = append(stream.pkg.payload, data...)
    return nil
}

// 4. Юніт-тести для edge cases
func TestTSDemuxer_SplitFrame_AcrossPackets(t *testing.T) {
    demuxer := NewTSDemuxer()
    var frames []byte
    
    demuxer.OnFrame = func(cid TS_STREAM_TYPE, frame []byte, pts, dts uint64) {
        frames = append(frames, frame...)
    }
    
    // Створити H.264 кадр, розрізаний на два TS пакети
    // Пакет 1: початок кадру з SPS+PPS+початок IDR
    // Пакет 2: решта IDR
    packet1 := createTSPacketWithH264Start(...)  // helper function
    packet2 := createTSPacketWithH264End(...)
    
    // Перший виклик: неповний кадр
    err1 := demuxer.Input(bytes.NewReader(packet1))
    if err1 != nil && err1 != io.EOF {
        t.Errorf("unexpected error: %v", err1)
    }
    if len(frames) > 0 {
        t.Error("frame should not be complete after first packet")
    }
    
    // Другий виклик: завершення кадру
    err2 := demuxer.Input(bytes.NewReader(packet2))
    if err2 != nil && err2 != io.EOF {
        t.Errorf("unexpected error: %v", err2)
    }
    if len(frames) == 0 {
        t.Error("frame should be complete after second packet")
    }
}
```

---

## 🎯 9. Інтеграція з вашим CCTV HLS Processor

### 📍 У `HLSDownloader` — прийом та парсинг сегментів:

```go
type HLSDownloader struct {
    demuxer *TSDemuxer
    assembler *SegmentAssembler
}

func (d *HLSDownloader) ProcessSegment(segmentURL string) error {
    resp, err := http.Get(segmentURL)
    if err != nil { return err }
    defer resp.Body.Close()
    
    d.demuxer = NewTSDemuxer()
    d.demuxer.OnFrame = func(cid TS_STREAM_TYPE, frame []byte, pts, dts uint64) {
        // Конвертація TS_STREAM_TYPE → codec.CodecID
        codecid := convertTSToCodecID(cid)
        
        // Передача у segmentAssembler
        switch {
        case codecid.IsVideo():
            d.assembler.HandleVideoFrame(codecid, frame, pts, dts)
        case codecid.IsAudio():
            d.assembler.HandleAudioFrame(codecid, frame, pts)
        }
    }
    
    // Інкрементальний парсинг прямо з HTTP тіла
    if err := d.demuxer.Input(resp.Body); err != nil && err != io.EOF {
        return fmt.Errorf("demux error: %w", err)
    }
    
    // Фіналізація залишкових даних
    d.demuxer.flush()
    return nil
}
```

### 📍 У `TSArchiver` — запис TS сегментів у файл:

```go
type TSArchiver struct {
    file *os.File
}

func (a *TSArchiver) WritePacket(pkg *TSPacket) error {
    // Запис сирого 188-байтового пакету у файл
    buf := make([]byte, TS_PAKCET_SIZE)
    // ... серіалізація pkg у buf ...
    _, err := a.file.Write(buf)
    return err
}

// Використання з TSDemuxer.OnTSPacket callback:
demuxer.OnTSPacket = func(pkg *TSPacket) {
    archiver.WritePacket(pkg)  // запис для архівування
}
```

### 📍 У метриках — моніторинг якості демуксингу:

```go
func (demuxer *TSDemuxer) recordHealthMetrics() {
    // Кількість активних програм/потоків
    var totalStreams int
    for _, pm := range demuxer.programs {
        totalStreams += len(pm.streams)
    }
    metrics.TSDemuxActiveStreams.Observe(float64(totalStreams))
    
    // Розмір буферів кадрів (детекція "завислих" потоків)
    for _, pm := range demuxer.programs {
        for sid, stream := range pm.streams {
            if stream.pkg != nil && len(stream.pkg.payload) > 0 {
                metrics.TSDemuxBufferedBytes.WithLabelValues(
                    fmt.Sprintf("program_%d_stream_%d", pm.pn, sid),
                ).Observe(float64(len(stream.pkg.payload)))
            }
        }
    }
}
```

---

## 🧭 Висновок: чому TS демуксер — критичний компонент для HLS

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **Синхронізація `probe()`** | Коректне вирівнювання на 0x47 у потоці | Зсув при парсингу → пошкоджені пакети → втрата кадрів |
| **PAT/PMT обробка** | Динамічна реєстрація потоків без попередньої конфігурації | Неможливість обробки мультипрограмних потоків або змін у конфігурації |
| **Інкрементальний PES парсинг** | Обробка кадрів, розрізаних між пакетами | Втрата даних при розрізанні кадрів на межі пакетів |
| **Детекція меж кадрів за NAL units** | Точне розділення відео на кадри для сегментації | Неповні кадри → помилки декодування у подальших етапах |
| **Конвертація часу 90 kHz → ms** | Сумісність з іншими компонентами пайплайну | Розсинхронізація аудіо/відео, неправильні часові мітки у HLS |

> 🔑 **Головна ідея**: Цей код — **протокольний адаптер**, який перетворює "брудний" TS потік з мережі на "чисті" відео/аудіо кадри з коректними часовими мітками. Без нього ваш `segmentAssembler` змушений був би знати деталі формату TS, PAT/PMT таблиць, PES пакування — це порушило б принцип єдиної відповідальності.

💡 **Фінальна порада**: 
1. Виправте опечатку: `pakcet_t` → `packet_t` у всьому проекті
2. Додайте `sync.RWMutex` до `TSDemuxer` для потокобезпеки
3. Реалізуйте безпечне копіювання залишку у `splitH264Frame()`/`splitH265Frame()` для уникнення витоку пам'яті
4. Додайте юніт-тести для розрізаних кадрів між пакетами та неповних даних
5. Використовуйте `TsTimeToMs()` helper для консистентної конвертації часу

Це перетворить цей демуксер з "робочого прототипу" на "надійний компонент продакшен-рівня" для вашого CCTV HLS Processor.