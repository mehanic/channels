# 📦 Глибокий розбір: `mpeg.Audio` — MPEG-1 Audio Layer II (MP2) декодер

Цей файл — **повноцінна реалізація декодера MP2** з підтримкою субсемплових трансформ, психоакустичного квантування та синтезу піддіапазонів. Він перетворює стиснуті дані у вихідні аудіо-семпли різних форматів.

---

## 🗺️ Архітектурна схема MP2 декодера

```
┌────────────────────────────────────────┐
│ 📦 mpeg.Audio — MP2 Decoder           │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • decodeHeader() — парсинг заголовка │
│  • decodeFrame() — декодування кадру  │
│  • idct36() — обернена DCT для синтезу│
│  • readSamples() — читання квантованих семплів│
│                                         │
│  🔄 Потік декодування:                  │
│  MP2 frame → decodeHeader()           │
│  → read allocation/scale factors      │
│  → readSamples() → idct36()           │
│  → synthesis window → PCM output      │
│                                         │
│  📡 Вихідні формати:                    │
│  • AudioF32N — normalized float32     │
│  • AudioS16 — signed 16-bit integer   │
│  • AudioF32 — raw float32             │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. AudioFormat та Samples — представлення аудіо-даних

### 🔧 Типи аудіо-форматів:

```go
type AudioFormat int

const (
    AudioF32N  AudioFormat = iota  // normalized float32: [-1.0, 1.0]
    AudioF32NLR                     // separate channels float32: Left/Right
    AudioF32                        // raw float32: [-32768, 32767] scaled
    AudioS16                        // signed 16-bit: [-32768, 32767]
)
```

### 🔧 Структура Samples:

```go
type Samples struct {
    Time        float64      // час семплу у секундах
    S16         []int16      // 16-бітні семпли (для AudioS16)
    F32         []float32    // float32 семпли (для AudioF32)
    Left        []float32    // лівий канал (для AudioF32NLR)
    Right       []float32    // правий канал (для AudioF32NLR)
    Interleaved []float32    // інтерліовані семпли (для AudioF32N)
    
    format AudioFormat       // поточний формат виводу
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `Time` | `float64` | **Критично**: час відтворення семплу у секундах | `1.234` = 1.234 секунди від початку |
| `S16` | `[]int16` | 16-бітні семпли для сумісності з аудіо-апаратним забезпеченням | `[]int16{0, 32767, -32768, ...}` |
| `F32` | `[]float32` | float32 семпли для обробки сигналу | `[]float32{0.0, 1.0, -1.0, ...}` |
| `Left/Right` | `[]float32` | окремі канали для стерео обробки | `Left: [0.5, 0.6], Right: [0.4, 0.3]` |
| `Interleaved` | `[]float32` | інтерліовані семпли [L0, R0, L1, R1, ...] | `[0.5, 0.4, 0.6, 0.3, ...]` |

### 🔧 Bytes() — отримання сирих байтів:

```go
func (s *Samples) Bytes() []byte {
    switch s.format {
    case AudioF32N:
        // unsafe.Slice для ефективної конвертації []float32 → []byte
        return unsafe.Slice((*byte)(unsafe.Pointer(&s.Interleaved[0])), len(s.Interleaved)*4)
    case AudioF32:
        return unsafe.Slice((*byte)(unsafe.Pointer(&s.F32[0])), len(s.F32)*4)
    case AudioS16:
        return unsafe.Slice((*byte)(unsafe.Pointer(&s.S16[0])), len(s.S16)*2)
    default:
        return nil
    }
}
```

### ⚠️ Критична проблема: використання unsafe

```
У поточному коді:
    return unsafe.Slice((*byte)(unsafe.Pointer(&s.Interleaved[0])), len(s.Interleaved)*4)

Проблема:
• unsafe.Slice вимагає Go 1.17+
• Пряме перетворення пам'яті може призвести до некоректних даних при вирівнюванні
• Неможливість серіалізації/десеріалізації через мережу без копіювання

✅ Виправлення: додавання перевірки та fallback
    func (s *Samples) Bytes() []byte {
        switch s.format {
        case AudioF32N:
            if len(s.Interleaved) == 0 {
                return nil
            }
            // Перевірка вирівнювання пам'яті
            if uintptr(unsafe.Pointer(&s.Interleaved[0]))%4 != 0 {
                // Fallback на копіювання
                result := make([]byte, len(s.Interleaved)*4)
                for i, f := range s.Interleaved {
                    binary.LittleEndian.PutUint32(result[i*4:], math.Float32bits(f))
                }
                return result
            }
            return unsafe.Slice((*byte)(unsafe.Pointer(&s.Interleaved[0])), len(s.Interleaved)*4)
        // ... інші випадки ...
        }
    }
```

### ✅ Ваш use-case**: конвертація між форматами

```go
// ConvertSamplesFormat — конвертація Samples між різними форматами
func ConvertSamplesFormat(src *mpeg.Samples, dstFormat mpeg.AudioFormat) *mpeg.Samples {
    dst := &mpeg.Samples{
        Time:   src.Time,
        format: dstFormat,
    }
    
    switch dstFormat {
    case mpeg.AudioF32N:
        // Конвертація у normalized [-1, 1]
        dst.Interleaved = make([]float32, len(src.Interleaved))
        for i, v := range src.Interleaved {
            // Припускаємо що вхідні дані у діапазоні [-32768, 32767]
            dst.Interleaved[i] = v / 32768.0
        }
        
    case mpeg.AudioS16:
        // Конвертація у 16-бітні семпли
        dst.S16 = make([]int16, len(src.Interleaved))
        for i, v := range src.Interleaved {
            // Clamp до діапазону int16
            if v < -32768 {
                dst.S16[i] = -32768
            } else if v > 32767 {
                dst.S16[i] = 32767
            } else {
                dst.S16[i] = int16(v)
            }
        }
        
    case mpeg.AudioF32NLR:
        // Розділення на окремі канали
        if len(src.Interleaved) >= 2 {
            dst.Left = make([]float32, len(src.Interleaved)/2)
            dst.Right = make([]float32, len(src.Interleaved)/2)
            for i := 0; i < len(src.Interleaved); i += 2 {
                dst.Left[i/2] = src.Interleaved[i]
                dst.Right[i/2] = src.Interleaved[i+1]
            }
        }
    }
    
    return dst
}

// Використання:
decoded := audioDecoder.Decode()
normalized := ConvertSamplesFormat(decoded, mpeg.AudioF32N)
// normalized.Interleaved тепер містить дані у діапазоні [-1.0, 1.0]
```

---

## 🔑 2. Audio — основна структура декодера

### 🔧 Структура та призначення:

```go
type Audio struct {
    // Стан декодування
    time              float64      // поточний час у секундах
    samplesDecoded    int          // кількість декодованих семплів
    samplerateIndex   int          // індекс частоти дискретизації
    bitrateIndex      int          // індекс бітрейту
    version           int          // MPEG version (1/2/2.5)
    layer             int          // audio layer (I/II/III)
    mode              int          // stereo mode
    channels          int          // кількість каналів (1/2)
    bound             int          // stereo bound для joint stereo
    vPos              int          // позиція у буфері синтезу
    nextFrameDataSize int          // розмір наступного кадру у байтах
    hasHeader         bool         // чи знайдено валідний заголовок
    
    buf *Buffer                   // вхідний буфер з даними
    
    // Таблиці квантування
    allocation      [2][32]*quantizerSpec  // таблиця розподілу біт
    scaleFactorInfo [2][32]byte            // інформація про scale factors
    scaleFactor     [2][32][3]int          // scale factors для 3 гранул
    sample          [2][32][3]int          // декодовані семпли
    
    // Вихідні дані
    samples Samples                 // буфер вихідних семплів
    format  AudioFormat             // формат виводу
    
    // Буфери для синтезу піддіапазонів
    d [1024]float32                // синтезуюче вікно
    v [2][1024]float32             // буфер історії піддіапазонів
    u [32]float32                  // проміжний буфер для виводу
}
```

### 🔍 Призначення ключових полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `samplerateIndex` | `int` | **Критично**: індекс у таблиці `samplerate` для отримання частоти дискретизації | `0` = 44100 Hz, `1` = 48000 Hz, `2` = 32000 Hz |
| `bitrateIndex` | `int` | **Критично**: індекс у таблиці `bitrate` для отримання бітрейту | `0` = 32 kbps, `13` = 384 kbps для MPEG-1 |
| `mode` | `int` | **Критично**: режим стерео для коректної обробки каналів | `modeStereo`, `modeJointStereo`, `modeMono` |
| `bound` | `int` | **Критично**: межа для joint stereo (субдіапазони < bound обробляються окремо) | `12` = перші 12 субдіапазонів окремо, решта спільно |
| `allocation` | `[2][32]*quantizerSpec` | **Критично**: скільки біт виділено на кожен субдіапазон/канал | `allocation[0][5]` = квантизер для лівого каналу, субдіапазон 5 |
| `scaleFactor` | `[2][32][3]int` | **Критично**: scale factors для 3 гранул у кадрі | `scaleFactor[1][10][2]` = правий канал, субдіапазон 10, гранула 2 |

### ✅ Ваш use-case**: ініціалізація декодера з валідацією

```go
// NewAudioWithValidation — безпечне створення декодера
func NewAudioWithValidation(buf *mpeg.Buffer) (*mpeg.Audio, error) {
    if buf == nil {
        return nil, fmt.Errorf("buffer cannot be nil")
    }
    
    decoder := mpeg.NewAudio(buf)
    
    // Спроба декодувати заголовок для валідації
    if !decoder.HasHeader() {
        return nil, fmt.Errorf("invalid MP2 stream: no valid header found")
    }
    
    // Перевірка підтримуваних параметрів
    samplerate := decoder.Samplerate()
    if samplerate == 0 {
        return nil, fmt.Errorf("unsupported sample rate")
    }
    
    channels := decoder.Channels()
    if channels != 1 && channels != 2 {
        return nil, fmt.Errorf("unsupported channel count: %d", channels)
    }
    
    return decoder, nil
}

// Використання:
buffer := getMP2Buffer()  // отримання буфера з даними
decoder, err := NewAudioWithValidation(buffer)
if err != nil {
    log.Printf("error creating decoder: %v", err)
    return
}

log.Printf("Initialized MP2 decoder: %d Hz, %d channels", 
    decoder.Samplerate(), decoder.Channels())
```

---

## 🔑 3. decodeHeader() — парсинг заголовка кадру

### 🔧 Основна логіка:

```go
func (a *Audio) decodeHeader() int {
    // 1. Перевірка наявності достатньо даних для заголовка
    if !a.buf.has(48) {  // 48 біт = 6 байт мінімум для заголовка
        return 0
    }
    
    // 2. Пошук синхронізаційного слова (11 біт = 0x7FF)
    a.buf.skipBytes(0x00)  // пропуск вирівнювання
    sync := a.buf.read(11)
    
    if sync != frameSync && !a.buf.findFrameSync() {
        return 0  // не знайдено синхронізацію
    }
    
    // 3. Читання основних полів заголовка
    a.version = a.buf.read(2)      // MPEG version
    a.layer = a.buf.read(2)        // audio layer
    hasCRC := a.buf.read1() == 0   // прапорець CRC
    
    // 4. Валідація підтримуваних версій
    if a.version != mpeg1 || a.layer != layerII {
        return 0  // підтримуємо тільки MPEG-1 Layer II
    }
    
    // 5. Читання бітрейту та частоти дискретизації
    bitrateIndex := a.buf.read(4) - 1
    if bitrateIndex > 13 {
        return 0
    }
    
    samplerateIndex := a.buf.read(2)
    if samplerateIndex == 3 {  // зарезервоване значення
        return 0
    }
    
    // 6. Читання додаткових полів
    padding := a.buf.read1()
    a.buf.skip(1)  // f_private
    mode := a.buf.read(2)  // stereo mode
    
    // 7. Перевірка узгодженості параметрів (для resync)
    if a.hasHeader && (a.bitrateIndex != bitrateIndex || 
                       a.samplerateIndex != samplerateIndex || 
                       a.mode != mode) {
        return 0  // параметри змінилися → можлива втрата синхронізації
    }
    
    // 8. Збереження параметрів
    a.bitrateIndex = bitrateIndex
    a.samplerateIndex = samplerateIndex
    a.mode = mode
    a.hasHeader = true
    
    // 9. Налаштування кількості каналів
    if mode == modeStereo || mode == modeJointStereo {
        a.channels = 2
    } else if mode == modeMono {
        a.channels = 1
    }
    
    // 10. Обробка joint stereo mode
    if mode == modeJointStereo {
        a.bound = (a.buf.read(2) + 1) << 2  // bound = 4, 8, 12, or 16
    } else {
        a.buf.skip(2)
        if mode == modeMono {
            a.bound = 0
        } else {
            a.bound = 32  // всі 32 субдіапазони окремо
        }
    }
    
    // 11. Пропуск останніх полів заголовка
    a.buf.skip(4)  // copyright, original, emphasis
    if hasCRC {
        a.buf.skip(16)  // CRC value
    }
    
    // 12. Розрахунок розміру кадру
    br := bitrate[a.bitrateIndex]      // бітрейт у kbps
    sr := samplerate[a.samplerateIndex] // частота дискретизації у Hz
    frameSize := (144000 * int(br) / int(sr)) + padding  // формула ISO/IEC 11172-3
    
    // 13. Корекція на заголовок та CRC
    r := 4  // розмір заголовка без CRC
    if hasCRC {
        r = 6  // +2 байти CRC
    }
    
    return frameSize - r  // розмір корисних даних кадру
}
```

### 🔍 Формула розрахунку розміру кадру:

```
frameSize = (144000 * bitrate / samplerate) + padding

Де:
• 144000 = 12 (семплів на гранулу) × 1000 (для kbps) × 12 (гранул на кадр) / 1000
• bitrate = бітрейт у kbps (з таблиці)
• samplerate = частота дискретизації у Hz
• padding = 0 або 1 байт (для вирівнювання)

Приклад для 128 kbps, 44100 Hz:
  frameSize = (144000 × 128 / 44100) + 0
            = (18432000 / 44100) + 0
            = 418.0... → 418 байт
  
  Корисні дані = 418 - 4 (заголовок) = 414 байт
```

### ⚠️ Критична проблема: обробка resync

```
У поточному коді:
    if sync != frameSync && !a.buf.findFrameSync() {
        return 0
    }

Проблема:
• findFrameSync() може шукати дуже довго у пошкоджених даних
• Можливість нескінченного циклу або великої затримки
• Немає обмеження на кількість біт для пошуку

✅ Виправлення: обмеження пошуку синхронізації
    const maxResyncBits = 8192  // максимум 8192 біт для пошуку
    
    if sync != frameSync {
        if !a.buf.findFrameSyncLimited(maxResyncBits) {
            return 0
        }
    }
    
    // У Buffer:
    func (b *Buffer) findFrameSyncLimited(maxBits int) bool {
        start := b.pos
        for b.pos - start < maxBits && !b.HasEnded() {
            if b.peek(11) == frameSync {
                return true
            }
            b.skip(1)  // пошук наступного біта
        }
        return false
    }
```

### ✅ Ваш use-case**: обробка змін параметрів у потоці

```go
// AdaptiveMP2Decoder — декодер з підтримкою зміни параметрів
type AdaptiveMP2Decoder struct {
    base     *mpeg.Audio
    callback func(samplerate, bitrate, channels int)  // callback при зміні параметрів
}

func (a *AdaptiveMP2Decoder) Decode() *mpeg.Samples {
    // Спроба декодувати кадр
    samples := a.base.Decode()
    
    // Перевірка чи змінилися параметри
    if a.base.HasHeader() {
        currentSR := a.base.Samplerate()
        currentCh := a.base.Channels()
        
        // Якщо параметри змінилися → виклик callback
        if a.lastSamplerate != currentSR || a.lastChannels != currentCh {
            if a.callback != nil {
                a.callback(currentSR, a.getBitrate(), currentCh)
            }
            a.lastSamplerate = currentSR
            a.lastChannels = currentCh
        }
    }
    
    return samples
}

// Використання:
decoder := &AdaptiveMP2Decoder{
    base: mpeg.NewAudio(buffer),
    callback: func(sr, br, ch int) {
        log.Printf("Stream parameters changed: %d Hz, %d kbps, %d channels", sr, br, ch)
        // Переналаштування аудіо-пайплайну...
    },
}
```

---

## 🔑 4. decodeFrame() — основний цикл декодування

### 🔧 Основна логіка:

```go
func (a *Audio) decodeFrame() {
    // 1. Підготовка таблиць квантування
    tab1 := 1
    if a.mode == modeMono {
        tab1 = 0
    }
    tab2 := int(quantLutStep1[tab1][a.bitrateIndex])
    tab3 := int(quantLutStep2[tab2][a.samplerateIndex])
    
    sblimit := tab3 & 63  // кількість субдіапазонів
    tab3 >>= 6            // індекс таблиці квантизерів
    
    if a.bound > sblimit {
        a.bound = sblimit
    }
    
    // 2. Читання таблиці розподілу біт (allocation)
    for sb := 0; sb < a.bound; sb++ {
        a.allocation[0][sb] = a.readAllocation(sb, tab3)  // лівий канал
        a.allocation[1][sb] = a.readAllocation(sb, tab3)  // правий канал
    }
    for sb := a.bound; sb < sblimit; sb++ {
        // Для joint stereo: субдіапазони >= bound мають спільне allocation
        a.allocation[0][sb] = a.readAllocation(sb, tab3)
        a.allocation[1][sb] = a.allocation[0][sb]
    }
    
    // 3. Читання інформації про scale factors
    channels := 2
    if a.mode == modeMono {
        channels = 1
    }
    
    for sb := 0; sb < sblimit; sb++ {
        for ch := 0; ch < channels; ch++ {
            if a.allocation[ch][sb] != nil {
                a.scaleFactorInfo[ch][sb] = byte(a.buf.read(2))
            }
        }
        if a.mode == modeMono {
            a.scaleFactorInfo[1][sb] = a.scaleFactorInfo[0][sb]
        }
    }
    
    // 4. Читання scale factors (3 значення на субдіапазон для 3 гранул)
    for sb := 0; sb < sblimit; sb++ {
        for ch := 0; ch < channels; ch++ {
            if a.allocation[ch][sb] != nil {
                switch a.scaleFactorInfo[ch][sb] {
                case 0:  // 3 окремих scale factors
                    a.scaleFactor[ch][sb][0] = a.buf.read(6)
                    a.scaleFactor[ch][sb][1] = a.buf.read(6)
                    a.scaleFactor[ch][sb][2] = a.buf.read(6)
                case 1:  // 2 scale factors: [0]=shared, [1]=shared, [2]=okремий
                    tmp := a.buf.read(6)
                    a.scaleFactor[ch][sb][0] = tmp
                    a.scaleFactor[ch][sb][1] = tmp
                    a.scaleFactor[ch][sb][2] = a.buf.read(6)
                case 2:  // 1 scale factor для всіх 3 гранул
                    tmp := a.buf.read(6)
                    a.scaleFactor[ch][sb][0] = tmp
                    a.scaleFactor[ch][sb][1] = tmp
                    a.scaleFactor[ch][sb][2] = tmp
                case 3:  // 2 scale factors: [0]=okремий, [1]=shared, [2]=shared
                    a.scaleFactor[ch][sb][0] = a.buf.read(6)
                    tmp := a.buf.read(6)
                    a.scaleFactor[ch][sb][1] = tmp
                    a.scaleFactor[ch][sb][2] = tmp
                }
            }
        }
        if a.mode == modeMono {
            // Копіювання для другого каналу у mono mode
            a.scaleFactor[1][sb][0] = a.scaleFactor[0][sb][0]
            a.scaleFactor[1][sb][1] = a.scaleFactor[0][sb][1]
            a.scaleFactor[1][sb][2] = a.scaleFactor[0][sb][2]
        }
    }
    
    // 5. Основний цикл декодування: 3 частини × 4 гранули = 12 гранул на кадр
    outPos := 0
    for part := 0; part < 3; part++ {
        for granule := 0; granule < 4; granule++ {
            // 5a. Читання квантованих семплів
            for sb := 0; sb < a.bound; sb++ {
                a.readSamples(0, sb, part)  // лівий канал
                a.readSamples(1, sb, part)  // правий канал
            }
            for sb := a.bound; sb < sblimit; sb++ {
                // Joint stereo: спільні семпли для обох каналів
                a.readSamples(0, sb, part)
                a.sample[1][sb][0] = a.sample[0][sb][0]
                a.sample[1][sb][1] = a.sample[0][sb][1]
                a.sample[1][sb][2] = a.sample[0][sb][2]
            }
            for sb := sblimit; sb < 32; sb++ {
                // Нульові семпли для невикористаних субдіапазонів
                a.sample[0][sb][0] = 0
                a.sample[0][sb][1] = 0
                a.sample[0][sb][2] = 0
                a.sample[1][sb][0] = 0
                a.sample[1][sb][1] = 0
                a.sample[1][sb][2] = 0
            }
            
            // 5b. Синтез піддіапазонів через IDCT та віконну функцію
            for p := 0; p < 3; p++ {  // 3 семпли на гранулу
                // Зсув буфера історії
                a.vPos = (a.vPos - 64) & 1023
                
                for ch := 0; ch < 2; ch++ {
                    // Обернена DCT для 36 точок
                    idct36(&a.sample[ch], p, &a.v[ch], a.vPos)
                    
                    // Віконна функція та накопичення
                    for i := range a.u {
                        a.u[i] = 0
                    }
                    
                    // Перша половина вікна
                    dIndex := 512 - (a.vPos >> 1)
                    vIndex := (a.vPos % 128) >> 1
                    for vIndex < 1024 {
                        for i := 0; i < 32; i++ {
                            a.u[i] += a.d[dIndex] * a.v[ch][vIndex]
                            dIndex++
                            vIndex++
                        }
                        vIndex += 128 - 32
                        dIndex += 64 - 32
                    }
                    
                    // Друга половина вікна (симетрична)
                    dIndex -= 512 - 32
                    vIndex = (128 - 32 + 1024) - vIndex
                    for vIndex < 1024 {
                        for i := 0; i < 32; i++ {
                            a.u[i] += a.d[dIndex] * a.v[ch][vIndex]
                            dIndex++
                            vIndex++
                        }
                        vIndex += 128 - 32
                        dIndex += 64 - 32
                    }
                    
                    // 5c. Вивід семплів з нормалізацією
                    var out []float32
                    if ch == 0 {
                        out = a.samples.Left
                    } else {
                        out = a.samples.Right
                    }
                    
                    for j := 0; j < 32; j++ {
                        // Нормалізація: ділення на константу для діапазону [-1, 1]
                        s := a.u[j] / -1090519040.0
                        
                        // Запис у вихідний буфер згідно з форматом
                        switch a.format {
                        case AudioF32N:
                            a.samples.Interleaved[((outPos+j)<<1)+ch] = s
                        case AudioF32NLR:
                            out[outPos+j] = s
                        case AudioS16:
                            if s < 0 {
                                a.samples.S16[((outPos+j)<<1)+ch] = int16(s * 0x8000)
                            } else {
                                a.samples.S16[((outPos+j)<<1)+ch] = int16(s * 0x7FFF)
                            }
                        case AudioF32:
                            if s < 0 {
                                a.samples.F32[((outPos+j)<<1)+ch] = s * 0x80000000
                            } else {
                                a.samples.F32[((outPos+j)<<1)+ch] = s * 0x7FFFFFFF
                            }
                        }
                    }
                }
                outPos += 32
            }
        }
    }
    
    // 6. Вирівнювання буфера після кадру
    a.buf.align()
}
```

### 🔍 Чому 3 частини × 4 гранули?

```
MP2 кадр містить 1152 семпли на канал:
• 3 частини (part) × 4 гранули (granule) × 3 семпли на гранулу × 32 субдіапазони = 1152

Структура:
  Кадр
  ├─ Частина 0
  │  ├─ Гранула 0: 3 семпли × 32 субдіапазони = 96 семплів
  │  ├─ Гранула 1: 96 семплів
  │  ├─ Гранула 2: 96 семплів
  │  └─ Гранула 3: 96 семплів
  ├─ Частина 1: 384 семпли
  └─ Частина 2: 384 семплів
  Разом: 1152 семпли на канал

Це дозволяє:
• Ефективне квантування через групування семплів
• Психоакустичне моделювання на рівні гранул
• Гнучке розподілення біт між субдіапазонами
```

### 🔍 Синтез піддіапазонів: IDCT + віконна функція

```
Процес синтезу перетворює 32 частотних субдіапазони у 32 часових семпли:

1. IDCT36 (Inverse Discrete Cosine Transform):
   • Перетворює частотні коефіцієнти у часову область
   • 36-точкова DCT для кожної групи з 3 семплів
   • Оптимізована реалізація з попередньо обчисленими коефіцієнтами

2. Віконна функція (synthesisWindow):
   • Згладжує переходи між гранулами
   • Запобігає артефактам на межах кадрів
   • Симетрична функія довжиною 512 точок

3. Накопичення з перекриттям:
   • Кожний вихідний семпл формується з 2 частин вікна
   • Перекриття 50% забезпечує безперервність сигналу
   • Буфер v[ch][1024] зберігає історію для накопичення

Математика:
  output[i] = Σ(window[j] * v[ch][pos+j]) для j=0..511
  де pos зсувається на 64 кожні 3 семпли
```

### ✅ Ваш use-case**: оптимізація синтезу для real-time

```go
// FastSynthesisDecoder — оптимізований декодер з SIMD підтримкою
type FastSynthesisDecoder struct {
    base *mpeg.Audio
    // Кешовані таблиці для IDCT
    idctCoeffs [36][36]float32
    // Вирівняні буфери для SIMD
    vAligned [2][1024]float32 `align:"32"`
    uAligned [32]float32       `align:"32"`
}

func (f *FastSynthesisDecoder) Decode() *mpeg.Samples {
    // Використання базового декодера для читання даних
    samples := f.base.Decode()
    
    // Оптимізований синтез з SIMD (псевдокод)
    if hasAVX2() {
        f.synthesisAVX2()
    } else if hasNEON() {
        f.synthesisNEON()
    } else {
        // Fallback на скалярну реалізацію
        f.base.decodeFrame()
    }
    
    return samples
}

// synthesisAVX2 — SIMD-оптимізована версія синтезу (псевдокод)
func (f *FastSynthesisDecoder) synthesisAVX2() {
    // Використання 256-бітних регістрів для паралельної обробки 8 float32
    for ch := 0; ch < 2; ch++ {
        for i := 0; i < 32; i += 8 {
            // Завантаження 8 значень з буфера v
            vVec := _mm256_load_ps(&f.vAligned[ch][f.vPos+i])
            
            // Множення на 8 коефіцієнтів вікна
            windowVec := _mm256_load_ps(&f.synthesisWindow[i])
            product := _mm256_mul_ps(vVec, windowVec)
            
            // Накопичення у результат
            result := _mm256_add_ps(f.uAligned[i:], product)
            _mm256_store_ps(&f.uAligned[i], result)
        }
    }
}
```

---

## 🔑 5. readSamples() — читання квантованих семплів

### 🔧 Основна логіка:

```go
func (a *Audio) readSamples(ch, sb, part int) {
    q := a.allocation[ch][sb]  // квантизер для цього субдіапазону/каналу
    sf := a.scaleFactor[ch][sb][part]  // scale factor для цієї гранули
    val := 0
    
    if q == nil {
        // Немає біт виділено → нульові семпли
        a.sample[ch][sb][0] = 0
        a.sample[ch][sb][1] = 0
        a.sample[ch][sb][2] = 0
        return
    }
    
    // 1. Розрахунок scale factor з 6-бітного індексу
    if sf == 63 {
        sf = 0  // спеціальне значення: вимкнути масштабування
    } else {
        shift := sf / 3
        // Базовий scale factor + округлення
        sf = (scalefactorBase[sf%3] + ((1 << shift) >> 1)) >> shift
    }
    
    // 2. Декодування квантованих значень
    adj := int(q.Levels)  // кількість рівнів квантування
    if q.Group != 0 {
        // Групове кодування: 3 семпли в одному значенні
        val = a.buf.read(int(q.Bits))  // читання packed значення
        a.sample[ch][sb][0] = val % adj
        val /= adj
        a.sample[ch][sb][1] = val % adj
        a.sample[ch][sb][2] = val / adj
    } else {
        // Пряме кодування: кожен семпл окремо
        a.sample[ch][sb][0] = a.buf.read(int(q.Bits))
        a.sample[ch][sb][1] = a.buf.read(int(q.Bits))
        a.sample[ch][sb][2] = a.buf.read(int(q.Bits))
    }
    
    // 3. Пост-множення: масштабування та нормалізація
    scale := 65536 / (adj + 1)  // коефіцієнт масштабування
    adj = ((adj + 1) >> 1) - 1   // зміщення для симетричного діапазону
    
    for i := 0; i < 3; i++ {
        val = (adj - a.sample[ch][sb][i]) * scale
        // Фіксована крапка: (val * sf) >> 24
        a.sample[ch][sb][i] = (val*(sf>>12) + ((val*(sf&4095) + 2048) >> 12)) >> 12
    }
}
```

### 🔍 Квантування та групування:

```
MP2 використовує адаптивне квантування з групуванням:

1. Квантизер (quantizerSpec):
   • Levels: кількість рівнів квантування (3, 5, 7, ..., 65535)
   • Group: чи використовується групування (0 = ні, 1 = так)
   • Bits: кількість біт на закодоване значення

2. Групове кодування:
   • Для малих Levels (3, 5, 7) ефективніше кодувати 3 семпли разом
   • Приклад для Levels=3:
     - Кожен семпл: 0, 1, або 2 (2 біти теоретично)
     - 3 семпли: 3^3 = 27 комбінацій → 5 біт замість 6
   • Формула: packed = s0 + s1*Levels + s2*Levels^2

3. Scale factors:
   • 6-бітний індекс → множник для масштабування
   • 3 значення на субдіапазон для адаптації до динаміки сигналу
   • Формула: sf = (base[sf%3] + rounding) >> (sf/3)

Приклад декодування:
  q.Levels = 5, q.Group = 1, q.Bits = 7
  packed = 123 (7 біт з потоку)
  
  s0 = 123 % 5 = 3
  s1 = (123 / 5) % 5 = 4
  s2 = 123 / 25 = 4
  
  → семпли: [3, 4, 4] у діапазоні [0, 4]
```

### ⚠️ Критична проблема: переповнення у фіксованій крапці

```
У поточному коді:
    val = (adj - a.sample[ch][sb][i]) * scale
    a.sample[ch][sb][i] = (val*(sf>>12) + ((val*(sf&4095) + 2048) >> 12)) >> 12

Проблема:
• val може бути великим (до 65536 * 32767 = 2.1e9)
• val * sf може переповнити int32 (max 2.1e9)
• Особливо для великих scale factors

✅ Виправлення: використання int64 для проміжних обчислень
    val64 := int64(adj - a.sample[ch][sb][i]) * int64(scale)
    sfHigh := int64(sf >> 12)
    sfLow := int64(sf & 4095)
    
    result := (val64*sfHigh + ((val64*sfLow + 2048) >> 12)) >> 12
    a.sample[ch][sb][i] = int(result)
```

### ✅ Ваш use-case**: налаштування якості декодування

```go
// QualityConfig — конфігурація якості декодування
type QualityConfig struct {
    UseGrouping    bool  // чи використовувати групове кодування (економія біт)
    ScaleFactorPrec int  // точність scale factors (6/5/4 біти)
    Dithering      bool  // чи додавати dithering для зменшення квантування
}

// ConfigureDecoder — налаштування декодера
func ConfigureDecoder(decoder *mpeg.Audio, config QualityConfig) {
    // Приклад: вимкнення групування для кращої якості
    if !config.UseGrouping {
        // Модифікація таблиць квантування (спрощено)
        for i := range quantTab {
            if quantTab[i].Group != 0 {
                // Створення не-групованої версії квантизера
                // ... реалізація ...
            }
        }
    }
    
    // Приклад: додавання dithering
    if config.Dithering {
        // Ініціалізація генератора випадкових чисел для dithering
        // ... реалізація ...
    }
}

// Використання:
config := QualityConfig{
    UseGrouping:    false,  // краща якість, більший бітрейт
    ScaleFactorPrec: 6,     // максимальна точність
    Dithering:      true,   // зменшення артефактів квантування
}
ConfigureDecoder(decoder, config)
```

---

## 🔑 6. idct36() — обернена DCT для синтезу

### 🔧 Призначення:

```
idct36() реалізує 36-точкову обернену дискретну косинус-трансформацію:

• Вхід: 36 частотних коефіцієнтів (3 семпли × 32 субдіапазони + запас)
• Вихід: 36 часових семплів у буфері v[ch][1024]
• Оптимізація: попередньо обчислені коефіцієнти, факторизація

Математика:
  x[n] = Σ X[k] * cos(π*(2n+1)*k / 72) для k=0..35, n=0..35

Оптимізації у реалізації:
• Симетрія косинуса: cos(θ) = -cos(π-θ)
• Факторизація: розбиття на менші DCT (8, 4, 2 точки)
• Попередньо обчислені константи: 0.500602998235, 0.505470959898, тощо
```

### 🔍 Структура функції:

```
Функція містить ~300 рядків з 100+ тимчасових змінних (t01..t33):

1. Перший етап: парне-непарне розбиття
   • t01 = s[0] + s[31], t02 = (s[0] - s[31]) * coeff
   • Аналогічно для пар (1,30), (2,29), ..., (15,16)

2. Другий етап: рекурсивна факторизація
   • Групування результатів першого етапу
   • Застосування додаткових коефіцієнтів

3. Третій етап: фінальне комбінування
   • Запис результатів у буфер d[dp+...]
   • Симетричне заповнення для віконної функції

Приклад оптимізації:
  Замість 36×36 = 1296 множень у прямій DCT:
  • Факторизація зменшує до ~200 множень
  • Попередньо обчислені коефіцієнти усувають обчислення cos()
  • Симетрія зменшує кількість унікальних операцій
```

### ✅ Ваш use-case**: заміна на швидшу DCT бібліотеку

```go
// FastIDCTDecoder — декодер з оптимізованою DCT
type FastIDCTDecoder struct {
    base *mpeg.Audio
    // Кешовані коефіцієнти для швидкої DCT
    dctPlan *DCTPlan
}

func (f *FastIDCTDecoder) decodeFrame() {
    // Використання базового декодера для читання даних
    // ...
    
    // Замість виклику idct36():
    for ch := 0; ch < 2; ch++ {
        for p := 0; p < 3; p++ {
            // Швидка DCT через попередньо обчислену таблицю
            fastIDCT36(&a.sample[ch], p, &a.v[ch], a.vPos, f.dctPlan)
        }
    }
    
    // ... решта синтезу ...
}

// fastIDCT36 — оптимізована версія з використанням таблиць
func fastIDCT36(s *[32][3]int, ss int, d *[1024]float32, dp int, plan *DCTPlan) {
    // Використання SIMD-оптимізованої DCT з бібліотеки
    // Наприклад: https://github.com/ibireme/yyjson або власна реалізація
    simdIDCT36(&s[0][ss], &d[dp], plan.coeffs)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Real-time MP2 streaming через WebSocket

```go
// MP2WebSocketStreamer — streaming MP2 аудіо через WebSocket
type MP2WebSocketStreamer struct {
    decoder *mpeg.Audio
    conn    *websocket.Conn
    mu      sync.Mutex
}

func (s *MP2WebSocketStreamer) Stream(ctx context.Context, source io.Reader) error {
    // 1. Ініціалізація буфера та декодера
    buf := mpeg.NewBuffer(source)
    decoder, err := NewAudioWithValidation(buf)
    if err != nil {
        return fmt.Errorf("init decoder: %w", err)
    }
    s.decoder = decoder
    
    // 2. Налаштування вихідного формату
    decoder.SetFormat(mpeg.AudioF32N)  // normalized float32 для веб-клієнтів
    
    // 3. Основний цикл декодування та відправки
    ticker := time.NewTicker(20 * time.Millisecond)  // 50 fps для low-latency
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-ticker.C:
            // Декодування одного кадру (1152 семпли)
            samples := decoder.Decode()
            if samples == nil {
                if decoder.HasEnded() {
                    return nil  // нормальне завершення
                }
                continue  // ще немає достатньо даних
            }
            
            // Конвертація у байти для відправки
            data := samples.Bytes()
            if data == nil {
                continue
            }
            
            // Формування повідомлення з метаданими
            message := AudioMessage{
                Time:     samples.Time,
                Channels: decoder.Channels(),
                SampleRate: decoder.Samplerate(),
                Data:     data,
            }
            
            // Серіалізація та відправка
            msgBytes, err := json.Marshal(message)
            if err != nil {
                return fmt.Errorf("marshal message: %w", err)
            }
            
            if err := s.sendBinary(msgBytes); err != nil {
                return fmt.Errorf("send audio: %w", err)
            }
            
            // Логування метрик
            log.Printf("Sent audio frame: %d bytes, time=%.3fs", 
                len(data), samples.Time)
        }
    }
}

type AudioMessage struct {
    Time       float64 `json:"time"`
    Channels   int     `json:"channels"`
    SampleRate int     `json:"sampleRate"`
    Data       []byte  `json:"data"`
}

func (s *MP2WebSocketStreamer) sendBinary(data []byte) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.conn.WriteMessage(websocket.BinaryMessage, data)
}
```

### 🔧 Приклад: Обробка змін параметрів у потоці

```go
// AdaptiveMP2Handler — обробка змін параметрів у реальному часі
type AdaptiveMP2Handler struct {
    decoder  *mpeg.Audio
    output   AudioOutput  // інтерфейс для виводу аудіо
    lastSR   int          // остання частота дискретизації
    lastCh   int          // остання кількість каналів
}

func (h *AdaptiveMP2Handler) Process(ctx context.Context, source io.Reader) error {
    buf := mpeg.NewBuffer(source)
    h.decoder = mpeg.NewAudio(buf)
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        default:
            samples := h.decoder.Decode()
            if samples == nil {
                if h.decoder.HasEnded() {
                    return nil
                }
                time.Sleep(10 * time.Millisecond)  // очікування даних
                continue
            }
            
            // Перевірка змін параметрів
            currentSR := h.decoder.Samplerate()
            currentCh := h.decoder.Channels()
            
            if currentSR != h.lastSR || currentCh != h.lastCh {
                log.Printf("Stream params changed: %d Hz, %d channels", 
                    currentSR, currentCh)
                
                // Переналаштування аудіо-виводу
                if err := h.output.Reconfigure(currentSR, currentCh); err != nil {
                    return fmt.Errorf("reconfigure output: %w", err)
                }
                
                h.lastSR = currentSR
                h.lastCh = currentCh
            }
            
            // Вивід семплів
            if err := h.output.Write(samples); err != nil {
                return fmt.Errorf("write samples: %w", err)
            }
        }
    }
}

type AudioOutput interface {
    Reconfigure(sampleRate, channels int) error
    Write(samples *mpeg.Samples) error
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Некоректне вирівнювання пам'яті у unsafe.Slice** | Паніка або некоректні дані на деяких архітектурах | Додайте перевірку вирівнювання або fallback на копіювання |
| **Переповнення у фіксованій крапці** | Артефакти або перекручений звук для гучних сигналів | Використовуйте int64 для проміжних обчислень у readSamples() |
| **Зависання при пошуку синхронізації** | Велика затримка або зависання на пошкоджених даних | Обмежте кількість біт для пошуку frameSync (maxResyncBits) |
| **Некоректна обробка joint stereo** | Дисбаланс каналів або артефакти у стерео | Перевірте логіку bound та копіювання семплів для sb >= bound |
| **Втрата синхронізації при зміні параметрів** | Раптові скачки або тиша у виводі | Реалізуйте адаптивну обробку змін параметрів через callback |

---

## ⚡ Оптимізації для high-performance декодування

### 1. Кешування таблиць квантування:

```go
var quantizerCache = sync.Map{}  // map[quantKey]*quantizerSpec

type quantKey struct {
    levels uint16
    group  uint8
    bits   uint8
}

func getCachedQuantizer(levels uint16, group uint8, bits uint8) *quantizerSpec {
    key := quantKey{levels, group, bits}
    
    if cached, ok := quantizerCache.Load(key); ok {
        return cached.(*quantizerSpec)
    }
    
    // Пошук у таблиці quantTab
    for i := range quantTab {
        q := &quantTab[i]
        if q.Levels == levels && q.Group == group && q.Bits == bits {
            quantizerCache.Store(key, q)
            return q
        }
    }
    
    return nil
}
```

### 2. SIMD-оптимізація синтезу:

```go
//go:build amd64 && !nosimd

package mpeg

import "golang.org/x/sys/cpu"

func hasAVX2() bool {
    return cpu.X86.HasAVX2
}

// synthesisAVX2 — AVX2-оптимізована версія синтезу (псевдокод)
//go:noescape
func synthesisAVX2(u *[32]float32, v *[1024]float32, window *[512]float32, pos int) {
    // Використання 256-бітних регістрів для 8 float32 одночасно
    // ... реалізація з intrinsics ...
}
```

### 3. Моніторинг продуктивності декодування:

```go
type DecoderMetrics struct {
    FramesDecoded prometheus.CounterVec
    DecodeLatency prometheus.HistogramVec
    SampleRateChanges prometheus.CounterVec
    DecodeErrors prometheus.CounterVec
}

func (m *DecoderMetrics) RecordFrame(sampleRate int, duration time.Duration, err error) {
    m.FramesDecoded.Inc()
    m.DecodeLatency.Observe(duration.Seconds())
    if err != nil {
        m.DecodeErrors.Inc()
    }
}

func (m *DecoderMetrics) RecordSampleRateChange(oldSR, newSR int) {
    m.SampleRateChanges.WithLabelValues(
        fmt.Sprintf("%d_to_%d", oldSR, newSR)).Inc()
}
```

---

## 📋 Чек-лист безпечного використання MP2 декодера

```go
// ✅ 1. Валідація вхідного буфера перед створенням декодера
if buf == nil {
    return fmt.Errorf("buffer cannot be nil")
}

// ✅ 2. Перевірка вирівнювання пам'яті для unsafe.Slice
if uintptr(unsafe.Pointer(&slice[0]))%4 != 0 {
    // Fallback на безпечне копіювання
    result := make([]byte, len(slice)*4)
    for i, f := range slice {
        binary.LittleEndian.PutUint32(result[i*4:], math.Float32bits(f))
    }
    return result
}

// ✅ 3. Обмеження пошуку синхронізації
const maxResyncBits = 8192
if !buf.findFrameSyncLimited(maxResyncBits) {
    return fmt.Errorf("sync not found within %d bits", maxResyncBits)
}

// ✅ 4. Використання int64 для фіксованої крапки
val64 := int64(adj - sample) * int64(scale)
result := (val64*(sf>>12) + ((val64*(sf&4095) + 2048) >> 12)) >> 12

// ✅ 5. Обробка змін параметрів у реальному часі
if currentSR != lastSR || currentCh != lastCh {
    // Переналаштування виводу перед продовженням декодування
    output.Reconfigure(currentSR, currentCh)
}

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Decoded frame: time=%.3fs, sr=%d, ch=%d, samples=%d", 
    samples.Time, decoder.Samplerate(), decoder.Channels(), len(samples.Interleaved))

// ✅ 7. Метрики для моніторингу
metrics.RecordFrame(decoder.Samplerate(), time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 11172-3:1993 (MPEG-1 Audio)](https://www.iso.org/standard/22412.html) — офіційний стандарт MP2
- 📄 [MPEG Audio Layer II Specification](https://www.mp2-tech.org/) — технічна документація та приклади
- 📄 [Subband Coding Explained](https://wiki.xiph.org/Subband_coding) — теорія піддіапазонного кодування
- 🧪 [Go unsafe Package](https://pkg.go.dev/unsafe) — робота з пам'яттю низького рівня
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Перевіряйте вирівнювання пам'яті перед unsafe.Slice** — уникнення панік на різних архітектурах.
> 2. **Використовуйте int64 для фіксованої крапки** — уникнення переповнення при гучних сигналах.
> 3. **Обмежуйте пошук синхронізації** — запобігання зависанням на пошкоджених даних.
> 4. **Реалізуйте адаптивну обробку змін параметрів** — підтримка потоків зі змінним бітрейтом/частотою.
> 5. **Моніторьте DecodeLatency** — виявлення продуктивних вузьких місць у реальному часі.

Потрібен приклад реалізації повного циклу MP2 streaming з адаптивною обробкою параметрів, або інтеграція цього декодера з вашим аудіо-пайплайном для WebSocket стрімінгу? Готовий допомогти! 🚀