# 🎵 Глибокий розбір `ogg/demuxer.go` — демуксинг Ogg контейнера для Opus та VP8

Це **реалізація демуксингу формату Ogg** згідно специфікації RFC 3533 та кодеків Opus (RFC 7845) та VP8. Ogg — це контейнерний формат з пакетною структурою, оптимізований для потокової передачі з корекцією помилок та підтримкою кількох потоків. Розберемо архітектурно:

---

## 🧱 1. Архітектура: від Ogg пакетів до медіа-кадрів

### 🔍 Потік даних у демуксері:

```
[Ogg потік: сторінки з пакетами]
         ↓
[OggDemuxer] ← цей файл!
├─ Синхронізація: пошук "OggS" magic bytes
├─ Парсинг сторінки: serial number, granule position, сегменти
├─ Збірка пакетів з сегментів (continuation logic)
├─ Детекція кодеку за "magic" заголовком (OpusHead/OVP80)
├─ Парсинг заголовків кодеків (OpusHead/VP8 ident header)
├─ Конвертація granule position → PTS
         ↓
[Raw Opus/VP8 Frames] → callback для подальшої обробки
```

### 🔑 Чому Ogg важливий для CCTV?

```
Хоча HLS використовує TS/fMP4, Ogg з Opus/VP8 популярний у:
• WebRTC → HLS транскодування (багато камер передають через WebRTC)
• Legacy IP-камери з підтримкою Ogg/Theora+Vorbis або Opus/VP8
• Open-source системи відеоспостереження (Shinobi, ZoneMinder)

Ваш демуксер дозволяє:
1. Приймати Ogg-потоки з камер без попередньої конвертації
2. Екстрагувати кадри для транскодування у HLS-сумісні формати
3. Підтримувати міграцію зі старих систем без втрати даних
```

---

## 🧩 2. Система кодеків: інтерфейс `oggCodec` та реєстрація

### 🔑 Абстракція через інтерфейс:

```go
type oggCodec interface {
    codecid() codec.CodecID  // Мапінг у внутрішній кодек
    magic() []byte           // "Magic bytes" для детекції заголовку
    magicSize() int          // Довжина magic bytes
}
```

### 🔧 Реалізації для підтримуваних кодеків:

```go
// Opus audio codec
type OpusCodec struct{}
func (opus OpusCodec) codecid() codec.CodecID { return codec.CODECID_AUDIO_OPUS }
func (opus OpusCodec) magic() []byte          { return []byte("OpusHead") }  // RFC 7845
func (opus OpusCodec) magicSize() int         { return 8 }

// VP8 video codec
type VP8Codec struct{}
func (vp8 VP8Codec) codecid() codec.CodecID { return codec.CODECID_VIDEO_VP8 }
func (vp8 VP8Codec) magic() []byte          { return []byte("OVP80") }  // Ogg VP8 ident header
func (vp8 VP8Codec) magicSize() int         { return 5 }
```

### 🔧 Реєстрація у `init()`:

```go
var codecs []oggCodec
func init() {
    codecs = make([]oggCodec, 2)
    codecs[0] = OpusCodec{}
    codecs[1] = VP8Codec{}
}
```

### 🎯 Чому така абстракція корисна?

```
1. Легко додати новий кодек: реалізувати oggCodec + додати у init()
2. Єдина логіка детекції заголовків через magic()
3. Type-safe мапінг у внутрішній codec.CodecID
4. Тестування: можна мокати oggCodec для юніт-тестів

Приклад додавання VP9:
type VP9Codec struct{}
func (vp9 VP9Codec) codecid() codec.CodecID { return codec.CODECID_VIDEO_VP9 }
func (vp9 VP9Codec) magic() []byte          { return []byte("OVP90") }
func (vp9 VP9Codec) magicSize() int         { return 5 }
// Додати у init(): codecs = append(codecs, VP9Codec{})
```

---

## 🎛️ 3. Інтерфейс `oggParser` — специфічна логіка для кожного кодеку

### 🔑 Абстракція парсера:

```go
type oggParser interface {
    // Парсинг заголовку кодеку (OpusHead/VP8 ident header)
    header(stream *oggStream, packet []byte) error
    
    // Обробка медіа-пакету: повертає кадр + часові мітки
    packet(stream *oggStream, packet []byte) (frame []byte, pts uint64, dts uint64)
    
    // Конвертація granule position → PTS (специфічна для кодеку)
    gptopts(granulePos uint64) uint64
    
    // Отримання extradata для ініціалізації декодера
    extraData() []byte
}
```

### 🔧 Фабрика парсерів:

```go
func createParser(cid codec.CodecID) oggParser {
    switch cid {
    case codec.CODECID_AUDIO_OPUS:
        return &opusDemuxer{
            lastpts: ^uint64(0),  // ініціалізація: "не встановлено"
        }
    case codec.CODECID_VIDEO_VP8:
        return &vp8Demuxer{
            lastpts: ^uint64(0),
            pktIdx:  0,  // лічильник пакетів у сторінці
        }
    default:
        panic("unsupport codecid")  // ⚠️ panic у продакшені!
    }
}
```

### 🎯 Чому окремий парсер для кожного кодеку?

```
Ogg — це універсальний контейнер, але кожен кодек має:
• Унікальний формат заголовку (OpusHead vs VP8 ident header)
• Різну логіку конвертації granule position → PTS
• Специфічні поля extradata для ініціалізації декодера

Розділення через інтерфейс дозволяє:
1. Інкапсулювати специфіку кодеку в окремому модулі
2. Уникнути великих switch/case у загальній логіці демуксингу
3. Легко тестувати парсери ізольовано
```

---

## 🎵 4. `opusDemuxer` — парсинг Opus у Ogg

### 🔍 OpusHead заголовок (RFC 7845, Section 5.1):

```
Байти 0-7:  "OpusHead" (magic signature)
Байт   8:   Version (завжди 1)
Байт   9:   Channel Count (1=mono, 2=stereo)
Байти 10-11: Pre-skip (uint16 LE) — семпли для пропуску на старті
Байти 12-15: Input Sample Rate (uint32 LE) — оригінальна частота дискретизації
Байти 16-17: Output Gain (Q7.8 fixed point) — посилення у dB
Байт   18:   Mapping Family (0=базова, 1=Vorbis, 255=немає)
Байти 19+:   Optional Channel Mapping Table (якщо Mapping Family != 0)
```

### 🔧 `header()` — парсинг OpusHead:

```go
func (opus *opusDemuxer) header(stream *oggStream, packet []byte) (err error) {
    // 1. Перевірка magic signature
    if bytes.Equal([]byte("OpusHead"), packet[0:8]) {
        // 2. Збереження extradata для подальшого використання
        opus.extradata = make([]byte, len(packet))
        copy(opus.extradata, packet)
        
        // 3. Парсинг через спільний кодек.ОpusContext
        err = opus.ctx.ParseExtranData(packet)
        if err != nil {
            return err
        }
    } else if bytes.Equal([]byte("OpusTags"), packet[0:8]) {
        // OpusTags — метадані (artist, title, тощо), можна ігнорувати
        return nil
    } else {
        // Невідомий заголовок → помилка
        return errors.New(`unsupported opus header` + strconv.Quote(string(packet)))
    }
    return nil
}
```

### 🔧 `packet()` — обробка медіа-пакетів з конвертацією часу:

```go
func (opus *opusDemuxer) packet(stream *oggStream, packet []byte) (frame []byte, pts uint64, dts uint64) {
    // 1. Обробка втрачених пакетів (lost=1)
    if stream.lost == 1 {
        return packet, opus.lastpts, opus.lastpts  // повернути як є
    }

    // 2. Ініціалізація lastpts при першому пакеті
    if opus.lastpts == ^uint64(0) {  // ^uint64(0) = 0xFFFFFFFFFFFFFFFF
        opus.lastpts = 0
    }
    
    frame = packet
    pts = opus.lastpts
    dts = pts  // Opus: PTS == DTS (немає B-frames)

    // 3. Детекція розриву у granule position
    if opus.granule != stream.currentPage.granulePos && !stream.currentPage.eos {
        opus.lastpts = 0  // скинути, якщо granule змінився неочікувано
        opus.granule = stream.currentPage.granulePos
    }

    // 4. Розрахунок початкового PTS за granule та тривалістю сторінки
    if opus.lastpts == 0 {
        var duration uint64
        // Сумувати тривалість всіх пакетів у поточній сторінці
        for _, seg := range stream.currentPage.packets {
            duration += codec.OpusPacketDuration(seg)  // з codec/opus.go
        }
        // Формула: start_pts = granule - total_duration - preskip
        opus.lastpts = opus.granule - duration - uint64(opus.ctx.Preskip)
    }

    // 5. Оновлення lastpts для наступного пакету
    duration := codec.OpusPacketDuration(packet)  // тривалість поточного пакету у семплах @ 48 kHz
    opus.lastpts = opus.lastpts + duration

    return
}
```

### 📐 Формула розрахунку PTS для Opus:

```
Opus використовує 48 kHz clock внутрішньо:
• 1 семпл = 1/48000 секунди ≈ 20.83 мкс
• Granule position = кількість семплів від початку потоку

Розрахунок PTS для пакету:
1. Знайти granule position поточної сторінки (останній семпл сторінки)
2. Відняти сумарну тривалість всіх пакетів у сторінці → початок сторінки
3. Відняти Pre-skip (семпли, що пропускаються на старті)
4. Додати тривалість попередніх пакетів у сторінці → початок поточного пакету

Приклад:
• Granule = 48000 (1 секунда)
• Сторінка містить 2 пакети по 960 семплів (20 ms кожен)
• Pre-skip = 3840 семплів (80 ms)
• Перший пакет: PTS = 48000 - (960+960) - 3840 = 42240 семплів = 880 ms
• Другий пакет: PTS = 42240 + 960 = 43200 семплів = 900 ms

У вашому коді: PTS повертається у семплах @ 48 kHz, конвертація у секунди/мілісекунди відбувається у викликаючому коді.
```

### 🎯 Чому `lastpts == ^uint64(0)` для ініціалізації?

```
^uint64(0) = 0xFFFFFFFFFFFFFFFF — максимальне значення uint64.
Це використовується як "магічне значення" для позначення "не встановлено".

Переваги перед 0:
• 0 — валідне значення PTS (початок потоку)
• ^uint64(0) — ніколи не зустрічається у реальному потоці
• Чітка семантика: "ще не ініціалізовано"

У вашому коді: при першому пакеті lastpts ініціалізується у 0,
що дозволяє коректно розрахувати початкові часові мітки.
```

---

## 🎬 5. `vp8Demuxer` — парсинг VP8 у Ogg

### 🔍 VP8 ident header (Ogg VP8 mapping):

```
Байти 0-4:  "OVP80" (magic signature для Ogg VP8)
Байт   5:   Type (0x01=ident header, 0x02=comment header)
Байт   6:   Version (1 для ident header)

Для Type=0x01 (ident header):
Байти 8-9:   Width (uint16 BE)
Байти 10-11: Height (uint16 BE)
Байти 12-14: Sample aspect ratio numerator (24 біти BE)
Байти 15-17: Sample aspect ratio denominator (24 біти BE)
Байти 18-21: Frame rate numerator (uint32 BE)
Байти 22-25: Frame rate denominator (uint32 BE)
```

### 🔧 `header()` — парсинг VP8 ident header:

```go
func (vp8 *vp8Demuxer) header(stream *oggStream, packet []byte) (err error) {
    // 1. Перевірка magic signature
    if !bytes.Equal([]byte("OVP80"), packet[0:5]) {
        return  // не помилка, просто не цей тип заголовку
    }

    // 2. Обробка за типом заголовку
    switch packet[5] {
    case 0x01:  // ident header
        if packet[6] != 1 {  // версія має бути 1
            return
        }
        // Парсинг параметрів відео
        vp8.width = binary.BigEndian.Uint16(packet[8:])
        vp8.height = binary.BigEndian.Uint16(packet[10:])
        
        // Sample aspect ratio: 24-бітні чисельник/знаменник
        num := uint32(packet[12])
        num = (num << 8) | uint32(packet[13])
        num = (num << 8) | uint32(packet[14])
        den := uint32(packet[15])
        den = (den << 8) | uint32(packet[16])
        den = (den << 8) | uint32(packet[17])
        vp8.sampleAspectratio = num / den
        
        // Frame rate: 32-бітні чисельник/знаменник
        num = binary.BigEndian.Uint32(packet[18:])
        den = binary.BigEndian.Uint32(packet[22:])
        vp8.frameRate = num / den
        
        // Збереження extradata
        vp8.extradata = make([]byte, len(packet))
        copy(vp8.extradata, packet)
        
    case 0x02:  // comment header
        if packet[6] != 0x20 {  // версія для comment header
            return
        }
        // TODO Parse Comment — метадані, можна ігнорувати
        
    default:
        return nil  // невідомий тип — не помилка
    }
    return nil
}
```

### 🔧 `packet()` — обробка відео-пакетів з granule position:

```go
func (vp8 *vp8Demuxer) packet(stream *oggStream, packet []byte) (frame []byte, pts uint64, dts uint64) {
    // 1. Обробка втрачених пакетів
    if stream.lost == 1 {
        return packet, vp8.lastpts, vp8.lastpts
    }

    // 2. Детекція розриву у granule position
    if vp8.granule != stream.currentPage.granulePos {
        vp8.lastpts = 0  // скинути при зміні granule
        vp8.pktIdx = 0   // скинути лічильник пакетів
        vp8.granule = stream.currentPage.granulePos
    }
    
    // 3. Розрахунок тривалості попередніх пакетів у сторінці
    var duration uint64 = 0
    for i := int(vp8.pktIdx); i < len(stream.currentPage.packets); i++ {
        // VP8: біт 4 у першому байті пакету = 1 для ключових кадрів
        // Але тут використовується для розрахунку тривалості? ⚠️ Підозріло!
        duration += uint64((stream.currentPage.packets[i][0] >> 4) & 1)
    }
    
    // 4. Конвертація granule position → PTS через gptopts()
    vp8.lastpts = vp8.gptopts(stream.currentPage.granulePos) - duration
    frame = packet
    pts = vp8.lastpts
    dts = pts  // VP8: PTS == DTS (немає B-frames у базовому профілі)
    
    // 5. Інкремент лічильника пакетів для наступного виклику
    vp8.pktIdx++
    return
}
```

### ⚠️ Підозріла логіка у розрахунку тривалості:

```go
duration += uint64((stream.currentPage.packets[i][0] >> 4) & 1)
// Це витягує біт 4 першого байту пакету → 0 або 1
// Але тривалість відео-кадру не може бути 0 або 1!

// Ймовірно, має бути:
// • Фіксована тривалість кадру (наприклад, 1000/fps ms)
// • Або читання тривалості з заголовку пакету

// Поточна реалізація призведе до неправильних PTS, якщо:
// • Кілька пакетів у одній сторінці
// • Змінна частота кадрів
```

### 🔧 `gptopts()` — конвертація granule position → PTS для VP8:

```go
func (vp8 *vp8Demuxer) gptopts(granulePos uint64) uint64 {
    var invcnt uint64 = 0
    // Перевірка біт 30-31: якщо 0 → інвертувати лічильник?
    if ((granulePos >> 30) & 3) == 0 {
        invcnt = 1
    }
    // Основна формула: старші 32 біти гранули мінус інверсія
    pts := (granulePos >> 32) - invcnt
    return pts
}
```

### 📐 Формула granule position для VP8:

```
Специфікація Ogg VP8 (https://wiki.xiph.org/OggVP8):
• Granule position = (frame_count << 32) | flags
• Старші 32 біти: кількість кадрів від початку потоку
• Молодші 32 біти: прапорці (біти 30-31 для інверсії)

Конвертація у PTS:
• PTS = frame_count (старші 32 біти)
• Якщо біти 30-31 = 0 → відняти 1 (invcnt=1)

Приклад:
• Granule = 0x0000000100000000 (frame_count=1, flags=0)
• ((granule >> 30) & 3) = (0x00000004 & 3) = 0 → invcnt=1
• PTS = (granule >> 32) - 1 = 1 - 1 = 0 ✓

• Granule = 0x0000000200000003 (frame_count=2, flags=3)
• ((granule >> 30) & 3) = (0x00000008 & 3) = 0 → invcnt=1
• PTS = 2 - 1 = 1 ✓

У вашому коді: PTS повертається у кадрах (не у часі),
конвертація у секунди відбувається через frameRate у викликаючому коді.
```

---

## 🔄 6. Інтеграція з `oggStream` та загальним демуксером

### 🔍 Контекст: як цей код використовується?

Хоча файл `demuxer.go` не показує повний цикл, з контексту видно:

```go
// Умовна структура OggDemuxer (не показана у файлі):
type OggDemuxer struct {
    streams map[uint32]*oggStream  // serial number → stream
    OnFrame func(cid codec.CodecID, frame []byte, pts, dts uint64)
}

type oggStream struct {
    serial      uint32
    codec       oggCodec
    parser      oggParser
    currentPage *oggPage  // поточна сторінка для збірки пакетів
    lost        uint8     // прапорець втрачених пакетів
}

// Умовний цикл обробки:
func (demuxer *OggDemuxer) Input(data []byte) error {
    // 1. Парсинг Ogg сторінок (пошук "OggS", парсинг заголовку)
    // 2. Для кожної сторінки:
    for _, page := range pages {
        stream := demuxer.getOrCreateStream(page.serial)
        
        // 3. Збірка пакетів з сегментів сторінки
        packets := assemblePackets(page.segments)
        
        // 4. Для кожного пакету:
        for _, packet := range packets {
            // 5. Детекція заголовку за magic bytes
            if isHeader(packet, stream.codec) {
                stream.parser.header(stream, packet)
            } else {
                // 6. Обробка медіа-пакету
                frame, pts, dts := stream.parser.packet(stream, packet)
                if demuxer.OnFrame != nil {
                    demuxer.OnFrame(stream.codec.codecid(), frame, pts, dts)
                }
            }
        }
    }
    return nil
}
```

### 🎯 Роль `oggStream` у парсингу:

```
oggStream зберігає стан для кожного потоку у мультиплексі:
• serial: унікальний ідентифікатор потоку у Ogg
• codec: інформація про кодек для детекції заголовків
• parser: специфічна логіка для цього кодеку
• currentPage: поточна сторінка для збірки пакетів з сегментів
• lost: прапорець для обробки втрачених даних

Це дозволяє обробляти мультиплексні Ogg файли з кількома аудіо/відео потоками.
```

---

## 🐞 7. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **`panic` у `createParser()`**:
   ```go
   default:
       panic("unsupport codecid")  // ← crash сервера при невідомому кодеку!
   
   // Краще повертати помилку:
   return nil, fmt.Errorf("unsupported codec for Ogg: %v", cid)
   // Або повернути nil і перевіряти у викликаючому коді
   ```

2. **Некоректний розрахунок тривалості у `vp8Demuxer.packet()`**:
   ```go
   duration += uint64((stream.currentPage.packets[i][0] >> 4) & 1)
   // Це дає 0 або 1, а не реальну тривалість кадру!
   
   // Ймовірно, має бути фіксована тривалість на основі frameRate:
   durationPerFrame := uint64(48000 / vp8.frameRate)  // у семплах @ 48 kHz
   // Або читання тривалості з заголовку пакету (якщо є)
   ```

3. **Відсутня валідація вхідних даних у парсерах**:
   ```go
   // У opusDemuxer.header():
   if bytes.Equal([]byte("OpusHead"), packet[0:8]) {
       // Але якщо len(packet) < 8 → panic: index out of range!
   
   // Краще додати перевірку:
   if len(packet) < opus.magicSize() {
       return errors.New("packet too short for OpusHead")
   }
   ```

4. **Race condition у спільних структурах**:
   ```go
   // Якщо OggDemuxer.Input() викликається з кількох горутин → data race на streams map!
   // Рішення: додати sync.RWMutex до OggDemuxer
   type OggDemuxer struct {
       mu sync.RWMutex
       streams map[uint32]*oggStream
       // ...
   }
   ```

5. **Необроблений випадок `lost=1` для ініціалізації**:
   ```go
   // У opusDemuxer.packet():
   if stream.lost == 1 {
       return packet, opus.lastpts, opus.lastpts
   }
   // Але якщо lastpts == ^uint64(0) (не ініціалізовано) → повертається невалідне значення!
   // Краще: ініціалізувати lastpts = 0 при lost=1, якщо ще не ініціалізовано
   ```

6. **Відсутня підтримка multi-packet pages для VP8**:
   ```go
   // У vp8Demuxer.packet() розрахунок duration передбачає, що всі пакети у сторінці мають однакову тривалість
   // Але Ogg дозволяє кілька пакетів різної тривалості у одній сторінці
   // Краще: зберігати тривалість кожного пакету окремо або використовувати granule position кожного пакету
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для безпечної конвертації granule position → PTS для Opus
func OpusGranuleToPTS(granule, preskip uint64) uint64 {
    if granule < preskip {
        return 0  // ще не дійшли до корисних даних
    }
    return granule - preskip
}

// 2. Метрики для моніторингу демуксингу
func (demuxer *OggDemuxer) recordMetrics(codecID codec.CodecID, packetSize int, pts uint64) {
    metrics.OggDemuxBytesReceived.WithLabelValues(
        codec.CodecString(codecID),
    ).Add(float64(packetSize))
    
    metrics.OggDemuxPTS.Observe(float64(pts) / 48000.0)  // конвертація у секунди для Opus
}

// 3. Обмеження розміру пакетів для захисту від переповнення
const MAX_OGG_PACKET_SIZE = 64 * 1024  // 64KB

func (demuxer *OggDemuxer) safeAssemblePacket(segments [][]byte) ([]byte, error) {
    totalSize := 0
    for _, seg := range segments {
        totalSize += len(seg)
        if totalSize > MAX_OGG_PACKET_SIZE {
            return nil, fmt.Errorf("packet too large: %d > %d", totalSize, MAX_OGG_PACKET_SIZE)
        }
    }
    return bytes.Join(segments, nil), nil
}

// 4. Юніт-тести для granule position конвертації
func TestOpusGranuleToPTS(t *testing.T) {
    tests := []struct{
        granule uint64
        preskip uint64
        want uint64
    }{
        {0, 3840, 0},  // ще не дійшли до даних
        {3840, 3840, 0},  // перший корисний семпл
        {48000, 3840, 44160},  // 1 секунда - preskip
    }
    for _, tt := range tests {
        got := OpusGranuleToPTS(tt.granule, tt.preskip)
        if got != tt.want {
            t.Errorf("OpusGranuleToPTS(%d, %d) = %d, want %d", 
                tt.granule, tt.preskip, got, tt.want)
        }
    }
}

// 5. Підтримка змінної тривалості кадрів для VP8
type vp8Demuxer struct {
    // ... існуючі поля ...
    frameDurations []uint64  // тривалість кожного кадру у сторінці
}

func (vp8 *vp8Demuxer) packet(stream *oggStream, packet []byte) (frame []byte, pts uint64, dts uint64) {
    // ... існуюча логіка ...
    
    // Замість підозрілого розрахунку:
    // duration += uint64((stream.currentPage.packets[i][0] >> 4) & 1)
    
    // Використовувати збережені тривалості або фіксовану на основі frameRate:
    if len(vp8.frameDurations) > int(vp8.pktIdx) {
        duration = vp8.frameDurations[vp8.pktIdx]
    } else {
        duration = uint64(48000 / vp8.frameRate)  // fallback
    }
    
    // ...
}
```

---

## 🎯 8. Інтеграція з вашим CCTV HLS Processor

### 📍 У `OggFileReader` — читання .ogg/.webm файлів:

```go
type OggFileReader struct {
    demuxer *OggDemuxer
    file    *os.File
}

func (r *OggFileReader) Process(filePath string, assembler *SegmentAssembler) error {
    file, err := os.Open(filePath)
    if err != nil { return err }
    defer file.Close()
    
    r.demuxer = NewOggDemuxer()
    r.demuxer.OnFrame = func(cid codec.CodecID, frame []byte, pts, dts uint64) {
        // Конвертація: семпли @ 48 kHz → ms для Opus
        if cid == codec.CODECID_AUDIO_OPUS {
            pts = pts * 1000 / 48000
            dts = dts * 1000 / 48000
        }
        // Для VP8: кадри → ms через frameRate
        // ...
        
        // Передача у segmentAssembler
        switch {
        case cid.IsVideo():
            assembler.HandleVideoFrame(cid, frame, pts, dts)
        case cid.IsAudio():
            assembler.HandleAudioFrame(cid, frame, pts)
        }
    }
    
    // Інкрементальне читання файлу
    buf := make([]byte, 64*1024)
    for {
        n, err := file.Read(buf)
        if n > 0 {
            if err := r.demuxer.Input(buf[:n]); err != nil && err != io.EOF {
                return fmt.Errorf("demux error: %w", err)
            }
        }
        if err == io.EOF {
            break
        }
    }
    return nil
}
```

### 📍 У `WebRTCToHLSConverter` — транскодування WebRTC → HLS:

```go
func (conv *WebRTCToHLSConverter) onOggPacket(cid codec.CodecID, packet []byte, pts, dts uint64) {
    // 1. Конвертація часових міток у формат HLS
    hlsPTS := convertToHLSTime(pts, cid)  // Opus: семпли→ms, VP8: кадри→ms
    
    // 2. Пакування у TS/fMP4 сегмент
    if err := conv.segmentWriter.WriteFrame(cid, packet, hlsPTS, hlsPTS); err != nil {
        logger.Error("write segment failed", "error", err)
    }
    
    // 3. Якщо сегмент завершено → додати у HLS плейлист
    if conv.segmentWriter.SegmentReady() {
        conv.hlsPlaylist.AddSegment(conv.segmentWriter.Flush())
    }
}
```

### 📍 У метриках — моніторинг якості демуксингу:

```go
func (demuxer *OggDemuxer) recordHealthMetrics() {
    // Кількість активних потоків
    metrics.OggDemuxActiveStreams.Observe(float64(len(demuxer.streams)))
    
    // Розмір буферів пакетів (детекція "завислих" потоків)
    for serial, stream := range demuxer.streams {
        if stream.currentPage != nil {
            metrics.OggDemuxBufferedBytes.WithLabelValues(
                fmt.Sprintf("stream_%d", serial),
            ).Observe(float64(stream.currentPage.totalSegmentSize()))
        }
    }
    
    // Частота помилок парсингу
    metrics.OggDemuxParseErrors.Observe(float64(demuxer.parseErrorCount))
}
```

---

## 🧭 Висновок: чому Ogg демуксер — критичний компонент для сумісності

| Компонент | Роль у CCTV HLS Processor | Вартість помилки без нього |
|-----------|--------------------------|---------------------------|
| **Magic bytes детекція** | Коректна ідентифікація кодеків у мультиплексі | Неправильний парсер → неможливість розпакувати потік |
| **Granule position конвертація** | Точні часові мітки для синхронізації | Розсинхронізація аудіо/відео, "стрибаюче" відео |
| **Pre-skip обробка для Opus** | Коректне відтворення з початку потоку | Обрізані перші кадри, артефакти декодування |
| **Збірка пакетів з сегментів** | Обробка розрізаних даних у Ogg сторінках | Втрата кадрів при розрізанні на межі сегментів |
| **Інкапсуляція через oggParser** | Легке додавання нових кодеків без зміни ядра | Неможливість підтримки нових форматів без переписування демуксера |

> 🔑 **Головна ідея**: Цей код — **адаптер для контейнера Ogg**, який абстрагує складність пакетної структури, granule position та специфіки кодеків від бізнес-логіки вашого пайплайну. Без нього кожен компонент змушений був би знати деталі формату Ogg, що призвело б до дублювання коду, помилок у синхронізації та важкої підтримки.

💡 **Фінальна порада**: 
1. Замініть `panic` на повернення помилок у `createParser()`
2. Виправте розрахунок тривалості кадрів у `vp8Demuxer.packet()` через frameRate або granule position
3. Додайте валідацію довжини пакетів перед доступом до `packet[0:8]`
4. Реалізуйте `sync.RWMutex` для потокобезпеки при паралельній обробці
5. Додайте юніт-тести для конвертації granule position → PTS з граничними значеннями

Це перетворить цей демуксер з "робочого прототипу" на "надійний компонент продакшен-рівня" для обробки Ogg/Opus/VP8 потоків у вашому CCTV HLS Processor.