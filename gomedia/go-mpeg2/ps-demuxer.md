# 📦 Глибокий розбір `mpeg2/ps.go` — Program Stream демуксер для MPEG-1/2

Це **реалізація демуксингу формату Program Stream (PS)** згідно специфікації ISO/IEC 13818-1 (MPEG-2 Systems). PS — це контейнерний формат для зберігання мультиплексированих аудіо/відео даних, що використовується у .mpg/.mpeg файлах та деяких системах відеоспостереження. Розберемо архітектурно:

---

## 🧱 1. Архітектура: Program Stream vs Transport Stream

### 🔍 Ключові відмінності:

| Аспект | Program Stream (PS) | Transport Stream (TS) |
|--------|-------------------|---------------------|
| **Розмір пакету** | Змінний (до 64KB) | Фіксований 188 байт |
| **Синхронізація** | Менш стійка до помилок | Стійка до помилок передачі |
| **Застосування** | Файли (.mpg), DVD, локальне зберігання | Мовлення (DVB, ATSC), IPTV, HLS |
| ** overhead** | Нижчий (~1-2%) | Вищий (~5-10%) |
| **Seek** | Простіший через індекси | Складніший через розподілені дані |

### 🔑 Чому підтримка PS важлива для CCTV:

```
Багато старих/бюджетних IP-камер та відеореєстраторів (DVR/NVR) 
використовують Program Stream для:
• Локального запису на SD-карту
• Експорту архівних записів
• Сумісності з legacy ПЗ для аналізу

Ваш демуксер дозволяє:
1. Приймати PS-потоки з камер без попередньої конвертації
2. Екстрагувати кадри для HLS-сегментації
3. Підтримувати міграцію зі старих систем без втрати даних
```

---

## 📦 2. `psstream` — внутрішнє представлення елементарного потоку

### 🔧 Структура:

```go
type psstream struct {
    sid       uint8           // Stream ID (0xC0-0xEF): ідентифікатор потоку
    cid       PS_STREAM_TYPE  // Внутрішній кодек (H264, AAC, тощо)
    pts       uint64          // Останній PTS для цього потоку (90 kHz clock)
    dts       uint64          // Останній DTS для цього потоку
    streamBuf []byte          // Буфер для накопичення даних між PES пакетами
}
```

### 🎯 Чому потрібен `streamBuf`?

```
PES пакети можуть розбивати один відео-кадр на кілька частин:
[ PES 1: частина 1 H.264 NAL ] [ PES 2: частина 2 того ж NAL ]

Без буферизації:
• Кожен PES обробляється окремо → неповні NAL units → помилки декодування

З буферизацією у psstream:
• Дані накопичуються у streamBuf
• Коли знайдено повний NAL (через start code) → відправка у callback
• Залишок зберігається для наступного PES
```

### 🔧 Конструктор:

```go
func newpsstream(sid uint8, cid PS_STREAM_TYPE) *psstream {
    return &psstream{
        sid:       sid,
        cid:       cid,
        streamBuf: make([]byte, 0, 4096),  // Початкова ємність 4KB
    }
}
```

> 💡 **Оптимізація**: `make([]byte, 0, 4096)` створює слайс з довжиною 0, але ємністю 4096 — це зменшує реалокації пам'яті при додаванні даних.

---

## 🎛️ 3. `PSDemuxer` — головний демуксер з інкрементальним парсингом

### 🔧 Ключові поля:

```go
type PSDemuxer struct {
    streamMap map[uint8]*psstream  // sid → psstream: мультиплекс кількох потоків
    pkg       *PSPacket            // Тимчасовий буфер для поточного пакету
    mpeg1     bool                 // Прапорець: MPEG-1 vs MPEG-2 формат
    cache     []byte               // Буфер для неповних даних між викликами Input()
    
    // Callbacks для події-орієнтованої обробки:
    OnFrame   func(frame []byte, cid PS_STREAM_TYPE, pts uint64, dts uint64)
    // OnPacket: для дебагу/моніторингу парсингу пакетів
    OnPacket func(pkg Display, decodeResult error)
}
```

### 🔑 Інкрементальний парсинг: чому `cache` критичний?

```
Мережеві дані приходять чанками довільного розміру:
[Чанк 1: 2000 байт] [Чанк 2: 1500 байт] [Чанк 3: 3000 байт]

Проблема: PES пакет може бути розрізаний між чанками:
[Чанк 1: початок PES... ] [Чанк 2: ...кінець PES]

Рішення: кешування неповних даних
1. Input() отримує новий чанк
2. Якщо є дані у cache → об'єднати з новим чанком
3. Парсити доки є повні пакети
4. Залишок (неповний пакет) зберегти у cache для наступного виклику
```

### 🔧 Механізм `Input()`: скінченний автомат парсингу

```go
func (psdemuxer *PSDemuxer) Input(data []byte) error {
    // 1. Об'єднання з кешем
    var bs *codec.BitStream
    if len(psdemuxer.cache) > 0 {
        psdemuxer.cache = append(psdemuxer.cache, data...)
        bs = codec.NewBitStream(psdemuxer.cache)
    } else {
        bs = codec.NewBitStream(data)
    }

    // Helper для збереження залишку
    saveReseved := func() {
        tmpcache := make([]byte, bs.RemainBytes())
        copy(tmpcache, bs.RemainData())
        psdemuxer.cache = tmpcache
    }

    // 2. Цикл парсингу: поки є дані → шукати start codes
    for !bs.EOS() {
        // Перевірка на потребу в більших даних
        if mpegerr, ok := ret.(Error); ok {
            if mpegerr.NeedMore() {
                saveReseved()
            }
            break
        }
        
        // Мінімальна перевірка: 32 біти для start code prefix
        if bs.RemainBits() < 32 {
            ret = errNeedMore
            saveReseved()
            break
        }
        
        // 3. Детекція типу пакету за start code
        prefix_code := bs.NextBits(32)  // Не споживає біти, тільки дивиться
        switch prefix_code {
```

---

## 🔍 4. Start Code Detection: магічні числа специфікації

### 📊 Таблиця start codes у Program Stream:

```go
switch prefix_code {
case 0x000001BA: // pack_header — заголовок програми (синхронізація, clock)
    // Парсинг PSPackHeader: system_clock_reference, mux_rate, тощо
    
case 0x000001BB: // system_header — метадані потоків (бітрейти, буфери)
    // Парсинг System_header: інформація про кожен елементарний потік
    
case 0x000001BC: // program_stream_map (PSM) — мапінг stream_id → codec
    // Критично для динамічної реєстрації нових потоків!
    for _, streaminfo := range psdemuxer.pkg.Psm.Stream_map {
        if _, found := psdemuxer.streamMap[streaminfo.Elementary_stream_id]; !found {
            stream := newpsstream(streaminfo.Elementary_stream_id, 
                                 PS_STREAM_TYPE(streaminfo.Stream_type))
            psdemuxer.streamMap[stream.sid] = stream
        }
    }
    
case 0x000001BD, 0x000001BE, ...: // private_stream_1, padding_stream, тощо
    // Пропуск або обробка специфічних даних
    
case 0x000001FF: // program_stream_directory — індекс для швидкого seek
    // Може використовуватись для навігації у великих файлах
    
case 0x000001B9: // MPEG_program_end_code — кінець файлу
    continue  // Просто пропускаємо
    
default:
    // === КЛЮЧОВИЙ ВИПАДОК: PES пакети ===
    // Stream IDs: 0xC0-0xDF = audio, 0xE0-0xFF = video
    if prefix_code&0xFFFFFFE0 == 0x000001C0 ||  // 0xC0-0xDF: audio
       prefix_code&0xFFFFFFE0 == 0x000001E0 {    // 0xE0-0xFF: video
        
        // Парсинг PES пакету
        if psdemuxer.mpeg1 {
            ret = psdemuxer.pkg.Pes.DecodeMpeg1(bs)  // Спрощений MPEG-1 формат
        } else {
            ret = psdemuxer.pkg.Pes.Decode(bs)       // Повний MPEG-2 формат
        }
        
        // Обробка успішного парсингу
        if ret == nil {
            if stream, found := psdemuxer.streamMap[psdemuxer.pkg.Pes.Stream_id]; found {
                // Потік вже відомий → пряма обробка
                psdemuxer.demuxPespacket(stream, psdemuxer.pkg.Pes)
            } else {
                // Новий потік без PSM → heuristic detection
                if psdemuxer.mpeg1 {
                    stream := newpsstream(psdemuxer.pkg.Pes.Stream_id, PS_STREAM_UNKNOW)
                    psdemuxer.streamMap[stream.sid] = stream
                    // Накопичення даних для подальшої детекції
                    stream.streamBuf = append(stream.streamBuf, psdemuxer.pkg.Pes.Pes_payload...)
                }
            }
        }
    } else {
        // Невідомий start code → пропустити 1 байт і продовжити пошук
        bs.SkipBits(8)
    }
}
```

### 🔍 Бітова маска для детекції PES stream IDs:

```go
// Аудіо потоки: 0xC0-0xDF (32 можливі канали)
// Бітова маска: 0xFFFFFFE0 = 11111111 11111111 11111111 11100000
//              &
//              0x000001C0 = 00000000 00000000 00000001 11000000
//              =
//              0x000001C0 ← співпадіння для 0xC0-0xDF

// Відео потоки: 0xE0-0xFF (16 можливих каналів)
// Аналогічно з маскою 0x000001E0

// Приклад: stream_id = 0xE5 (відео, канал 5)
// 0x000001E5 & 0xFFFFFFE0 = 0x000001E0 ✓ → відео потік
```

> 💡 **Практичне значення**: Ця маска дозволяє обробляти будь-який audio/video потік без явного переліку всіх 48 можливих значень.

---

## 🔮 5. `guessCodecid()` — евристична детекція кодеку для невідомих потоків

### 🔍 Проблема: потік без PSM (Program Stream Map)

```
У ідеальному випадку: PSM пакет каже "stream_id 0xE1 = H.264".
У реальному світі: старі камери можуть не надсилати PSM.

Рішення: аналіз вмісту для детекції кодеку "на льоту".
```

### 🔧 Алгоритм scoring для H.264 vs H.265:

```go
func (psdemuxer *PSDemuxer) guessCodecid(stream *psstream) {
    // 1. Груба класифікація за stream_id діапазоном
    if stream.sid&0xE0 == uint8(PES_STREAM_AUDIO) {
        stream.cid = PS_STREAM_AAC  // Припускаємо AAC для аудіо
    } else if stream.sid&0xE0 == uint8(PES_STREAM_VIDEO) {
        
        // 2. Детальний аналіз для відео: scoring система
        h264score := 0
        h265score := 0
        
        // Розділення буфера на NAL units
        codec.SplitFrame(stream.streamBuf, func(nalu []byte) bool {
            h264nalutype := codec.H264NaluTypeWithoutStartCode(nalu)
            h265nalutype := codec.H265NaluTypeWithoutStartCode(nalu)
            
            // === H.264 scoring ===
            if h264nalutype == codec.H264_NAL_PPS ||
               h264nalutype == codec.H264_NAL_SPS ||
               h264nalutype == codec.H264_NAL_I_SLICE {
                h264score += 2  // Критичні NAL types для H.264
            } else if h264nalutype < 5 {  // P/B-slices
                h264score += 1  // Звичайні VCL NAL units
            } else if h264nalutype > 20 {  // Невідомі типи
                h264score -= 1  // Штраф за підозрілі значення
            }
            
            // === H.265 scoring ===
            if h265nalutype == codec.H265_NAL_PPS ||
               h265nalutype == codec.H265_NAL_SPS ||
               h265nalutype == codec.H265_NAL_VPS ||  // VPS — унікальний для H.265!
               (h265nalutype >= codec.H265_NAL_SLICE_BLA_W_LP && 
                h265nalutype <= codec.H265_NAL_SLICE_CRA) {  // IRAP frames
                h265score += 2
            } else if h265nalutype >= codec.H265_NAL_Slice_TRAIL_N && 
                      h265nalutype <= codec.H265_NAL_SLICE_RASL_R {
                h265score += 1
            } else if h265nalutype > 40 {
                h265score -= 1
            }
            
            // 3. Раннє завершення при досягненні порогу
            if h264score > h265score && h264score >= 4 {
                stream.cid = PS_STREAM_H264
            } else if h264score < h265score && h265score >= 4 {
                stream.cid = PS_STREAM_H265
            }
            return true  // продовжити аналіз
        })
    }
}
```

### 🎯 Чому поріг `>= 4`?

```
Емпіричне правило: мінімум 2-3 "сильних" індикаторів для впевненої детекції:
• 2 × (SPS + PPS) = 4 бали → достатньо для H.264
• 2 × (VPS + SPS) = 4 бали → достатньо для H.265

Це запобігає хибним спрацьовуванням на:
• Випадкових послідовностях байт, що нагадують NAL headers
• Пошкоджених даних з помилками передачі
• Коротких фрагментах з недостатньою інформацією
```

### ⚠️ Потенційна проблема: відсутність синхронізації між scoring та callback

```go
// У циклі SplitFrame:
if h264score > h265score && h264score >= 4 {
    stream.cid = PS_STREAM_H264  // ← змінюємо тип "на льоту"!
}

// Але попередні NAL units вже могли бути оброблені з неправильним cid!
// Рішення: або буферизувати до детекції, або дозволити "перемикання" з попередженням
```

---

## 🔀 6. `demuxPespacket()` — маршрутизація PES payload до кодек-специфічних обробників

### 🔧 Логіка диспетчеризації:

```go
func (psdemuxer *PSDemuxer) demuxPespacket(stream *psstream, pes *PesPacket) error {
    switch stream.cid {
    case PS_STREAM_AAC, PS_STREAM_G711A, PS_STREAM_G711U:
        return psdemuxer.demuxAudio(stream, pes)  // Аудіо: просте накопичення
        
    case PS_STREAM_H264, PS_STREAM_H265:
        return psdemuxer.demuxH26x(stream, pes)   // Відео: пошук NAL units
        
    case PS_STREAM_UNKNOW:
        // Невідомий кодек → накопичення з синхронізацією за PTS
        if stream.pts != pes.Pts {
            // Зміна часу → можливо новий кадр, очистити буфер
            stream.streamBuf = nil
        }
        stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
        stream.pts = pes.Pts
        stream.dts = pes.Dts
    }
    return nil
}
```

---

## 🎵 7. `demuxAudio()` — обробка аудіо потоків

### 🔧 Проста стратегія: накопичення за PTS

```go
func (psdemuxer *PSDemuxer) demuxAudio(stream *psstream, pes *PesPacket) error {
    // Детекція нового кадру за зміною PTS
    if stream.pts != pes.Pts && len(stream.streamBuf) > 0 {
        // PTS змінився → попередній кадр завершено, відправити у callback
        if psdemuxer.OnFrame != nil {
            // Конвертація: 90 kHz → milliseconds для зручності
            psdemuxer.OnFrame(stream.streamBuf, stream.cid, stream.pts/90, stream.dts/90)
        }
        stream.streamBuf = stream.streamBuf[:0]  // Очистити буфер
    }
    
    // Додати нові дані до буфера
    stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
    stream.pts = pes.Pts
    stream.dts = pes.Dts
    return nil
}
```

### 🎯 Чому `/90` для конвертації часу?

```
MPEG використовує 90 kHz clock для PTS/DTS:
• 90,000 ticks = 1 секунда
• 1 tick = 1/90000 секунди ≈ 11.11 мікросекунд

Для зручності у пайплайні конвертуємо у мілісекунди:
• 90,000 / 90 = 1,000 → 1 ms
• pts/90 = кількість мілісекунд від початку потоку

Це спрощує:
• Розрахунок тривалості сегментів для HLS (#EXTINF)
• Синхронізацію аудіо/відео за спільною шкалою часу
• Інтеграцію з іншими компонентами, що очікують ms
```

---

## 🎞️ 8. `demuxH26x()` — обробка відео потоків з детекцією NAL units

### 🔧 Ключова логіка: пошук start codes у накопиченому буфері

```go
func (psdemuxer *PSDemuxer) demuxH26x(stream *psstream, pes *PesPacket) error {
    // Ініціалізація часу для першого пакету
    if len(stream.streamBuf) == 0 {
        stream.pts = pes.Pts
        stream.dts = pes.Dts
    }
    
    // Додати нові дані до буфера
    stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
    
    // Пошук та екстракція повних NAL units
    start, sc := codec.FindStartCode(stream.streamBuf, 0)
    for start >= 0 {
        end, sc2 := codec.FindStartCode(stream.streamBuf, start+int(sc))
        if end < 0 {
            // Недостатньо даних для наступного NAL → зупинитись
            break
        }
        
        // Фільтрація AUD (Access Unit Delimiter) — не потрібен для відтворення
        if stream.cid == PS_STREAM_H264 {
            naluType := codec.H264NaluType(stream.streamBuf[start:])
            if naluType != codec.H264_NAL_AUD {  // 0x09
                if psdemuxer.OnFrame != nil {
                    psdemuxer.OnFrame(stream.streamBuf[start:end], stream.cid, 
                                     stream.pts/90, stream.dts/90)
                }
            }
        } else if stream.cid == PS_STREAM_H265 {
            naluType := codec.H265NaluType(stream.streamBuf[start:])
            if naluType != codec.H265_NAL_AUD {  // 0x46
                if psdemuxer.OnFrame != nil {
                    psdemuxer.OnFrame(stream.streamBuf[start:end], stream.cid, 
                                     stream.pts/90, stream.dts/90)
                }
            }
        }
        
        // Перейти до наступного NAL
        start = end
        sc = sc2
    }
    
    // Зберегти залишок (неповний NAL) для наступного виклику
    stream.streamBuf = stream.streamBuf[start:]
    stream.pts = pes.Pts
    stream.dts = pes.Dts
    return nil
}
```

### 🎯 Чому фільтруємо AUD NAL units?

```
AUD (Access Unit Delimiter) — спеціальний NAL type для маркування початку кадру:
• H.264: type 9 (0x09F0)
• H.265: type 35 (0x46...)

Призначення:
• Допомагає декодерам синхронізуватись на початку кадру
• Не містить візуальних даних, тільки службову інформацію

У вашому пайплайні:
• segmentAssembler вже детектує початок кадру через I-frame detection
• AUD дублює цю функцію → зайві дані у HLS сегментах
• Фільтрація економить bandwidth та спрощує подальшу обробку
```

---

## 🔄 9. `Flush()` — фіналізація накопичених даних

### 🔧 Коли та навіщо викликати:

```go
func (psdemuxer *PSDemuxer) Flush() {
    for _, stream := range psdemuxer.streamMap {
        if len(stream.streamBuf) == 0 {
            continue  // Порожній буфер → нічого робити
        }
        
        // Відправити залишкові дані, навіть якщо кадр неповний
        if psdemuxer.OnFrame != nil {
            // Конвертація часу: 90 kHz → ms
            psdemuxer.OnFrame(stream.streamBuf, stream.cid, stream.pts/90, stream.dts/90)
        }
    }
}
```

### 🎯 Сценарії використання:

| Сценарій | Коли викликати Flush() | Наслідки без Flush() |
|----------|----------------------|---------------------|
| **Кінець файлу** | Після читання останнього байту | Втрата останніх кадрів у буферах |
| **Зміна потоку** | Перед перемиканням на новий джерело | "Залипання" старих даних у новому контексті |
| **Періодичний експорт** | Кожні N секунд для low-latency | Затримка доставки кадрів до клієнта |
| **Обробка помилок** | При детекції розриву потоку | Накопичення пошкоджених даних |

> 💡 **Порада**: У real-time сценаріях викликайте `Flush()` періодично (наприклад, кожні 100ms) для мінімізації затримки, навіть якщо це означає відправку неповних кадрів.

---

## 🐞 10. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Відсутня валідація `PES_packet_length` у парсингу**:
   ```go
   // У Decode() PES пакету: якщо length некоректний → читання за межами буфера
   // Краще додати перевірку перед читанням payload
   ```

2. **Race condition у `streamMap`**:
   ```go
   // Якщо Input() викликається з кількох горутин → data race!
   // Рішення: додати sync.RWMutex або гарантувати однопоточний доступ
   
   type PSDemuxer struct {
       mu sync.RWMutex
       streamMap map[uint8]*psstream
       // ...
   }
   ```

3. **Необроблений випадок `PS_STREAM_UNKNOW` з даними**:
   ```go
   // У demuxPespacket для PS_STREAM_UNKNOW:
   stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
   // Але ніколи не відправляється у callback!
   // Дані накопичуються, але не використовуються → витік пам'яті
   
   // Краще: або відправляти як "raw", або обмежити розмір буфера
   if len(stream.streamBuf) > MAX_UNKNOWN_BUFFER {
       stream.streamBuf = stream.streamBuf[len(stream.streamBuf)-MAX_UNKNOWN_BUFFER:]
   }
   ```

4. **Помилка у конвертації часу для аудіо**:
   ```go
   // У demuxAudio: stream.pts/90, stream.dts/90
   // Але PTS/DTS — це 90 kHz clock, ділення на 90 дає мілісекунди ✓
   // Однак: якщо pts < 90, результат = 0 → втрата точності для коротких сегментів!
   
   // Краще: зберігати у 90 kHz і конвертувати тільки при необхідності
   // Або використовувати float: float64(stream.pts) / 90.0
   ```

5. **Відсутня обробка `errNeedMore` у зовнішньому коді**:
   ```go
   // Input() повертає errNeedMore, але чи перевіряє це викликаючий код?
   // Якщо ні → помилки парсингу ігноруються, дані втрачаються
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для безпечної конвертації часу
func MpegTimeToMs(time90kHz uint64) uint32 {
    return uint32((time90kHz + 45) / 90)  // округлення замість відсічення
}

// 2. Метрики для моніторингу демуксингу
func (psd *PSDemuxer) recordMetrics(streamType PS_STREAM_TYPE, payloadSize int) {
    metrics.PSDemuxBytesReceived.WithLabelValues(
        codec.StreamTypeString(streamType),
    ).Add(float64(payloadSize))
    
    metrics.PSDemuxFramesProcessed.Inc()
}

// 3. Обмеження розміру буфера для невідомих потоків
const MAX_UNKNOWN_BUFFER = 64 * 1024  // 64KB

func (psd *PSDemuxer) demuxUnknown(stream *psstream, pes *PesPacket) {
    stream.streamBuf = append(stream.streamBuf, pes.Pes_payload...)
    
    // Захист від переповнення
    if len(stream.streamBuf) > MAX_UNKNOWN_BUFFER {
        // Зберегти тільки останні дані (можливо, початок нового кадру)
        copy(stream.streamBuf, stream.streamBuf[len(stream.streamBuf)-MAX_UNKNOWN_BUFFER:])
        stream.streamBuf = stream.streamBuf[:MAX_UNKNOWN_BUFFER]
    }
}

// 4. Юніт-тести для edge cases
func TestPSDemuxer_IncrementalParsing(t *testing.T) {
    demuxer := NewPSDemuxer()
    var frames []byte
    
    demuxer.OnFrame = func(frame []byte, cid PS_STREAM_TYPE, pts, dts uint64) {
        frames = append(frames, frame...)
    }
    
    // Симуляція розрізаного PES пакету
    chunk1 := []byte{0x00, 0x00, 0x01, 0xE0}  // початок video PES
    chunk2 := []byte{0x00, 0x10, /* ...payload... */}  // решта
    
    // Перший виклик: недостатньо даних
    err := demuxer.Input(chunk1)
    if err != errNeedMore {
        t.Errorf("expected errNeedMore, got %v", err)
    }
    
    // Другий виклик: повний пакет
    err = demuxer.Input(chunk2)
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    
    if len(frames) == 0 {
        t.Error("no frames extracted from split PES")
    }
}
```

---

## 🎯 11. Інтеграція з вашим CCTV HLS Processor

### 📍 У `PSFileReader` — читання .mpg файлів:

```go
type PSFileReader struct {
    demuxer *PSDemuxer
    file    *os.File
}

func (r *PSFileReader) Process(filePath string, assembler *SegmentAssembler) error {
    file, err := os.Open(filePath)
    if err != nil { return err }
    defer file.Close()
    
    r.demuxer = NewPSDemuxer()
    r.demuxer.OnFrame = func(frame []byte, cid PS_STREAM_TYPE, pts, dts uint64) {
        // Конвертація внутрішнього PS_STREAM_TYPE → codec.CodecID
        codecid := convertPSToCodecID(cid)
        
        // Передача у segmentAssembler з коректними часовими мітками
        switch {
        case codecid.IsVideo():
            assembler.HandleVideoFrame(codecid, frame, pts, dts)
        case codecid.IsAudio():
            assembler.HandleAudioFrame(codecid, frame, pts)
        }
    }
    
    // Інкрементальне читання файлу
    buf := make([]byte, 64*1024)
    for {
        n, err := file.Read(buf)
        if n > 0 {
            if err := r.demuxer.Input(buf[:n]); err != nil && err != errNeedMore {
                return fmt.Errorf("demux error: %w", err)
            }
        }
        if err == io.EOF {
            r.demuxer.Flush()  // Відправити залишкові дані
            break
        }
    }
    return nil
}
```

### 📍 У `NetworkPSReceiver` — прийом потоку по UDP/TCP:

```go
func (r *NetworkPSReceiver) Start(conn net.Conn) {
    demuxer := NewPSDemuxer()
    demuxer.OnFrame = r.handleFrame  // callback до основного пайплайну
    
    buf := make([]byte, 2048)  // Менший буфер для low-latency
    for {
        n, err := conn.Read(buf)
        if err != nil { break }
        
        // Інкрементальний парсинг з обробкою помилок
        if err := demuxer.Input(buf[:n]); err != nil {
            if err == errNeedMore {
                continue  // Очікуємо більше даних
            }
            logger.Warn("PS demux error", "error", err)
            // Спроба відновлення: скинути кеш і продовжити
            demuxer.cache = nil
        }
    }
    
    // Фіналізація при роз'єднанні
    demuxer.Flush()
}
```

### 📍 У метриках — моніторинг якості демуксингу:

```go
func (psd *PSDemuxer) recordHealthMetrics() {
    // Розмір кешу: великий кеш → можливі проблеми з парсингом
    metrics.PSDemuxCacheSize.Observe(float64(len(psd.cache)))
    
    // Кількість активних потоків
    metrics.PSDemuxActiveStreams.Observe(float64(len(psd.streamMap)))
    
    // Детекція "завислих" потоків (дані у буфері > 1 секунди)
    for sid, stream := range psd.streamMap {
        if len(stream.streamBuf) > 0 {
            metrics.PSDemuxBufferedBytes.WithLabelValues(
                fmt.Sprintf("stream_%d", sid),
            ).Observe(float64(len(stream.streamBuf)))
        }
    }
}
```

---

## 🧭 Висновок: чому PS демуксер — критичний компонент для legacy підтримки

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **Інкрементальний парсинг** | Обробка потоків реального часу без буферизації всього файлу | Висока затримка, неможливість live-трансляції |
| **Start code детекція** | Коректне розділення пакетів у бітовому потоці | Зміщення при парсингу → пошкоджені кадри → артефакти |
| **Heuristic codec detection** | Підтримка камер без PSM метаданих | Неможливість обробки старих/бюджетних пристроїв |
| **NAL unit extraction** | Витягнення повних відео-кадрів з розрізаних PES | Неповні NAL units → помилки декодування у FFmpeg/браузері |
| **PTS/DTS handling** | Точна синхронізація аудіо/відео | Розсинхронізація → "не в попадання губ" у відео з голосом |

> 🔑 **Головна ідея**: Цей код — **адаптер для legacy форматів**, який дозволяє вашому сучасному HLS-пайплайну працювати зі старими камерами та архівами без попередньої конвертації. Без нього ви були б змушені вимагати від усіх джерел сучасний MPEG-TS або fMP4, що нереально у реальному світі CCTV.

💡 **Фінальна порада**: 
1. Додайте `sync.RWMutex` до `PSDemuxer` для потокобезпеки
2. Реалізуйте обмеження розміру буфера для `PS_STREAM_UNKNOW`
3. Додайте юніт-тести для інкрементального парсингу з розрізаними пакетами
4. Використовуйте `MpegTimeToMs()` helper для консистентної конвертації часу
5. Додайте метрики для моніторингу розміру кешу та "завислих" потоків

Це перетворить цей демуксер з "робочого прототипу" на "надійний компонент продакшен-рівня" для вашого CCTV HLS Processor.