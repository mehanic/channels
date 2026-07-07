# 🎬 Глибокий розбір: h264parser — Парсер H.264/AVC потоків

Цей файл — **реалізація парсингу H.264/AVC відео-потоків** згідно зі стандартом ITU-T H.264 (MPEG-4 Part 10). Він надає інструменти для роботи з двома основними форматами: **Annex B** (для потокового передавання) та **AVCC** (для контейнерів на кшталт MP4/FLV).

Розберемо архітектуру, бітові формати та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема h264parser

```
┌────────────────────────────────────────┐
│ 📦 h264parser — H.264 Stream Handling  │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • NALU Parser — розпізнавання типів   │
│  • Annex B / AVCC детектор             │
│  • SPS/PPS Parser — метадані відео     │
│  • Slice Header Parser — тип кадру (I/P/B)│
│  • CodecData — інтеграція з av.CodecData│
│                                         │
│  📊 NALU Types:                         │
│  • 7=SPS, 8=PPS, 5=IDR, 1-5=VCL        │
│  • 6=SEI, 9=AUD, інші=metadata         │
│                                         │
│  🔄 Формати потоків:                    │
│  • Annex B: start codes (0x000001)     │
│  • AVCC: length-prefixed (4-byte size) │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. NALU — Network Abstraction Layer Unit

### Типи NALU (найважливіші):

```go
const (
    NALU_SEI = 6   // Supplemental Enhancement Information (metadata)
    NALU_SPS = 7   // Sequence Parameter Set (конфігурація декодера)
    NALU_PPS = 8   // Picture Parameter Set (параметри кадру)
    NALU_AUD = 9   // Access Unit Delimiter (маркер початку кадру)
)
```

### 📋 Повна таблиця NALU типів:

| Тип | Назва | VCL/non-VCL | Призначення |
|-----|-------|-------------|-------------|
| 0 | Unspecified | non-VCL | Зарезервовано |
| 1 | Coded slice of non-IDR | VCL | Звичайний кадр (залежний) |
| 2-4 | Data partitions | VCL | Розділені дані (рідко) |
| **5** | **Coded slice of IDR** | **VCL** | **Ключовий кадр (незалежний)** |
| 6 | SEI | non-VCL | Метадані (таймінги, субтитри) |
| **7** | **SPS** | **non-VCL** | **Глобальні параметри: роздільна здатність, FPS** |
| **8** | **PPS** | **non-VCL** | **Параметри кадру: ентропія, фільтри** |
| 9 | AUD | non-VCL | Маркер початку Access Unit |
| 10-23 | Різні | non-VCL | Розширені функції |

### ✅ Ваш use-case: детекція ключових кадрів для HLS

```go
// IsKeyFrame — перевірка чи пакет містить IDR кадр
func IsKeyFrame(nalu []byte) bool {
    if len(nalu) == 0 {
        return false
    }
    // Тип NALU у бітах 0-4 першого байта
    naluType := nalu[0] & 0x1f
    return naluType == 5  // 5 = IDR slice
}

// IsDataNALU — перевірка чи це відео-дані (не metadata)
func IsDataNALU(b []byte) bool {
    typ := b[0] & 0x1f
    return typ >= 1 && typ <= 5  // 1-5 = VCL NALUs
}

// Використання у сегментації:
for _, nalu := range nalus {
    if IsKeyFrame(nalu) {
        // Початок нового HLS-сегменту
        startNewSegment()
    }
    writeNALUToSegment(nalu)
}
```

---

## 🔄 2. Формати потоків: Annex B vs AVCC

### Annex B (потоковий формат):

```
Структура: [Start Code][NALU][Start Code][NALU]...

Start Code: 0x000001 (3 байти) або 0x00000001 (4 байти)
Приклад: 00 00 00 01 67... (SPS) 00 00 00 01 68... (PPS) 00 00 00 01 65... (IDR)

Переваги:
• Легко знайти початок NALU по синхронізації
• Підходить для потокової передачі (RTSP, MPEG-TS)

Недоліки:
• Потрібна обробка emulation prevention bytes (0x000003)
• Важко визначити розмір NALU без парсингу
```

### AVCC (контейнерний формат):

```
Структура: [4-byte length][NALU][4-byte length][NALU]...

Приклад: 00 00 02 41 65... (NALU довжиною 0x241 = 577 байт)

Переваги:
• Простий парсинг: прочитаємо 4 байти → знаємо розмір
• Ефективний для довільного доступу (MP4, MKV)

Недоліки:
• Потрібен заголовок (AVCDecoderConfRecord) з SPS/PPS
• Не підходить для потокової передачі без конвертації
```

### 🔍 Авто-детект формату `SplitNALUs()`:

```go
func SplitNALUs(b []byte) (nalus [][]byte, typ int) {
    // 1. Перевірка на AVCC: перші 4 байти = довжина першого NALU
    val4 := pio.U32BE(b)  // big-endian uint32
    if val4 <= uint32(len(b)) {
        // Спроба парсити як AVCC
        // ... логіка розбиття по length-prefix ...
        return nalus, NALU_AVCC
    }
    
    // 2. Перевірка на Annex B: пошук start codes (0x000001 або 0x00000001)
    val3 := pio.U24BE(b)  // перші 3 байти
    if val3 == 1 || val4 == 1 {
        // Спроба парсити як Annex B
        // ... логіка пошуку start codes ...
        return nalus, NALU_ANNEXB
    }
    
    // 3. Fallback: один NALU без розділювачів
    return [][]byte{b}, NALU_RAW
}
```

### ✅ Ваш use-case: конвертація Annex B → AVCC для HLS

```go
// ConvertAnnexBToAVCC — конвертація потоку для MP4/HLS контейнера
func ConvertAnnexBToAVCC(annexBData []byte, sps, pps []byte) ([]byte, error) {
    // 1. Розбиття на NALU за start codes
    nalus, typ := h264parser.SplitNALUs(annexBData)
    if typ != h264parser.NALU_ANNEXB {
        return nil, fmt.Errorf("expected Annex B format")
    }
    
    // 2. Створення AVCC заголовка (AVCDecoderConfRecord)
    recordInfo := h264parser.AVCDecoderConfRecord{
        AVCProfileIndication: sps[1],
        ProfileCompatibility: sps[2],
        AVCLevelIndication:   sps[3],
        LengthSizeMinusOne:   3,  // 4-byte length prefix
        SPS:                  [][]byte{sps},
        PPS:                  [][]byte{pps},
    }
    
    header := make([]byte, recordInfo.Len())
    recordInfo.Marshal(header)
    
    // 3. Конвертація кожного NALU у length-prefixed формат
    var avccData []byte
    avccData = append(avccData, header...)
    
    for _, nalu := range nalus {
        // Додаємо 4-байтову довжину перед кожним NALU
        length := uint32(len(nalu))
        avccData = append(avccData, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
        avccData = append(avccData, nalu...)
    }
    
    return avccData, nil
}
```

---

## 🧠 3. SPS Parser — розбір Sequence Parameter Set

### Що таке SPS?

**Sequence Parameter Set (SPS)** — це NALU типу 7, що містить **глобальні параметри відео-потоку**:
- Роздільна здатність (width/height)
- Профіль/рівень кодека (Baseline, Main, High)
- Частота кадрів (FPS)
- Параметри кропування, aspect ratio тощо

### 🔍 Структура `SPSInfo`:

```go
type SPSInfo struct {
    // Базові параметри
    Id                uint  // ID параметр-сету (0-31)
    ProfileIdc        uint  // Профіль: 66=Baseline, 77=Main, 100=High
    LevelIdc          uint  // Рівень: 10-51 (1.0-5.1)
    ConstraintSetFlag uint  // Обмеження сумісності
    
    // Роздільна здатність у макроблоках (16×16 пікселів)
    MbWidth  uint  // кількість макроблоків по ширині
    MbHeight uint  // кількість макроблоків по висоті
    
    // Параметри кропування (для нестандартних aspect ratio)
    CropLeft, CropRight, CropTop, CropBottom uint
    
    // Обчислені значення
    Width  uint  // фінальна ширина у пікселях
    Height uint  // фінальна висота у пікселях
    FPS    uint  // частота кадрів (якщо вказано у VUI)
}
```

### 🔧 Ключові методи парсингу:

```go
func ParseSPS(data []byte) (s SPSInfo, err error) {
    // 1. Видалення emulation prevention bytes (0x000003 → 0x0000)
    data = RemoveH264orH265EmulationBytes(data)
    
    // 2. Ініціалізація Golomb-читача для бітового парсингу
    r := &bits.GolombBitReader{R: bytes.NewReader(data)}
    
    // 3. Пропуск NALU header (1 байт)
    r.ReadBits(8)
    
    // 4. Читання основних полів
    s.ProfileIdc, _ = r.ReadBits(8)      // профіль
    s.ConstraintSetFlag, _ = r.ReadBits(8) // обмеження
    s.LevelIdc, _ = r.ReadBits(8)         // рівень
    s.Id, _ = r.ReadExponentialGolombCode() // ID параметр-сету
    
    // 5. Розширені профілі (High, High 10, High 4:2:2)
    if s.ProfileIdc == 100 || s.ProfileIdc == 110 || ... {
        // Парсинг chroma_format, bit_depth, scaling matrices...
    }
    
    // 6. Параметри порядку кадрів (pic_order_cnt_type)
    pic_order_cnt_type, _ := r.ReadExponentialGolombCode()
    // ... обробка різних типів ...
    
    // 7. Розрахунок роздільної здатності
    s.MbWidth, _ = r.ReadExponentialGolombCode()
    s.MbWidth++  // +1 бо значення у SPS = width_in_mbs_minus1
    s.MbHeight, _ = r.ReadExponentialGolombCode()
    s.MbHeight++
    
    // 8. Обробка кропування
    frame_cropping_flag, _ := r.ReadBit()
    if frame_cropping_flag != 0 {
        s.CropLeft, _ = r.ReadExponentialGolombCode()
        s.CropRight, _ = r.ReadExponentialGolombCode()
        s.CropTop, _ = r.ReadExponentialGolombCode()
        s.CropBottom, _ = r.ReadExponentialGolombCode()
    }
    
    // 9. Фінальний розрахунок піксельних розмірів
    s.Width = (s.MbWidth * 16) - s.CropLeft*2 - s.CropRight*2
    s.Height = ((2 - frame_mbs_only_flag) * s.MbHeight * 16) - s.CropTop*2 - s.CropBottom*2
    
    // 10. Парсинг VUI (Video Usability Information) для FPS
    vui_parameter_present_flag, _ := r.ReadBit()
    if vui_parameter_present_flag != 0 {
        timing_info_present_flag, _ := r.ReadBit()
        if timing_info_present_flag != 0 {
            num_units_in_tick, _ := r.ReadBits(32)
            time_scale, _ := r.ReadBits(32)
            // Формула: FPS = time_scale / (num_units_in_tick * 2)
            s.FPS = uint(math.Floor(float64(time_scale) / float64(num_units_in_tick) / 2.0))
        }
    }
    
    return s, nil
}
```

### 🔢 Формула розрахунку роздільної здатності:

```
Width = (MbWidth * 16) - CropLeft*2 - CropRight*2
Height = ((2 - frame_mbs_only_flag) * MbHeight * 16) - CropTop*2 - CropBottom*2

Приклад:
  MbWidth = 120, MbHeight = 68, frame_mbs_only_flag = 1
  CropLeft = CropRight = CropTop = CropBottom = 0
  
  Width = (120 * 16) - 0 = 1920
  Height = (1 * 68 * 16) - 0 = 1088  ← але зазвичай кропується до 1080!
```

### ✅ Ваш use-case: валідація відео-параметрів для HLS

```go
// ValidateH264ForHLS — перевірка чи відео сумісне з HLS
func ValidateH264ForHLS(spsData []byte) error {
    spsInfo, err := h264parser.ParseSPS(spsData)
    if err != nil {
        return fmt.Errorf("parse SPS: %w", err)
    }
    
    // HLS вимагає: H.264 Baseline/Main/High profile, level ≤ 4.1
    validProfiles := map[uint]bool{
        66: true,  // Baseline
        77: true,  // Main
        100: true, // High
    }
    if !validProfiles[spsInfo.ProfileIdc] {
        return fmt.Errorf("unsupported profile: %d", spsInfo.ProfileIdc)
    }
    
    if spsInfo.LevelIdc > 41 {  // 4.1 = 41
        return fmt.Errorf("unsupported level: %d.%d", spsInfo.LevelIdc/10, spsInfo.LevelIdc%10)
    }
    
    // Роздільна здатність: не більше 1920×1080 для сумісності
    if spsInfo.Width > 1920 || spsInfo.Height > 1080 {
        return fmt.Errorf("resolution too high: %dx%d", spsInfo.Width, spsInfo.Height)
    }
    
    // FPS: бажано 24, 25, 30, 50, 60
    if spsInfo.FPS > 0 {
        validFPS := map[uint]bool{24: true, 25: true, 30: true, 50: true, 60: true}
        if !validFPS[spsInfo.FPS] {
            log.Printf("warning: non-standard FPS: %d", spsInfo.FPS)
        }
    }
    
    return nil
}
```

---

## 📦 4. CodecData — інтеграція з av.CodecData

### Структура `AVCDecoderConfRecord`:

```go
type AVCDecoderConfRecord struct {
    AVCProfileIndication uint8      // профіль з SPS[1]
    ProfileCompatibility uint8      // сумісність з SPS[2]
    AVCLevelIndication   uint8      // рівень з SPS[3]
    LengthSizeMinusOne   uint8      // 3 = 4-byte length prefix (AVCC)
    SPS                  [][]byte   // масив SPS NALU (зазвичай 1)
    PPS                  [][]byte   // масив PPS NALU (зазвичай 1)
}
```

### 🔧 Методи маршалінгу:

```go
// Len() — розрахунок розміру заголовка
func (self AVCDecoderConfRecord) Len() (n int) {
    n = 7  // базовий заголовок
    for _, sps := range self.SPS {
        n += 2 + len(sps)  // 2 байти для довжини + дані
    }
    for _, pps := range self.PPS {
        n += 2 + len(pps)
    }
    return
}

// Marshal() — запис у байтовий буфер
func (self AVCDecoderConfRecord) Marshal(b []byte) (n int) {
    b[0] = 1  // версія конфігурації
    b[1] = self.AVCProfileIndication
    b[2] = self.ProfileCompatibility
    b[3] = self.AVCLevelIndication
    b[4] = self.LengthSizeMinusOne | 0xfc  // верхні 6 біт = 1
    b[5] = uint8(len(self.SPS)) | 0xe0     // верхні 3 біти = 1
    
    n = 6
    for _, sps := range self.SPS {
        pio.PutU16BE(b[n:], uint16(len(sps)))  // 2-байтова довжина
        n += 2
        copy(b[n:], sps)
        n += len(sps)
    }
    
    b[n] = uint8(len(self.PPS))
    n++
    for _, pps := range self.PPS {
        pio.PutU16BE(b[n:], uint16(len(pps)))
        n += 2
        copy(b[n:], pps)
        n += len(pps)
    }
    return
}
```

### ✅ Ваш use-case: створення CodecData для HLS муксера

```go
// CreateH264CodecData — створення av.CodecData з SPS/PPS для заголовка
func CreateH264CodecData(sps, pps []byte) (av.CodecData, error) {
    // 1. Парсинг SPS для отримання метаданих
    spsInfo, err := h264parser.ParseSPS(sps)
    if err != nil {
        return nil, fmt.Errorf("parse SPS: %w", err)
    }
    
    // 2. Створення AVCDecoderConfRecord
    recordInfo := h264parser.AVCDecoderConfRecord{
        AVCProfileIndication: sps[1],
        ProfileCompatibility: sps[2],
        AVCLevelIndication:   sps[3],
        LengthSizeMinusOne:   3,  // 4-byte length для AVCC
        SPS:                  [][]byte{sps},
        PPS:                  [][]byte{pps},
    }
    
    // 3. Маршалінг у байти
    recordBytes := make([]byte, recordInfo.Len())
    recordInfo.Marshal(recordBytes)
    
    // 4. Створення CodecData
    codecData, err := h264parser.NewCodecDataFromAVCDecoderConfRecord(recordBytes)
    if err != nil {
        return nil, err
    }
    
    // 5. Перевірка сумісності з HLS
    if err := ValidateH264ForHLS(sps); err != nil {
        log.Printf("warning: H.264 may not be HLS-compatible: %v", err)
    }
    
    return codecData, nil
}

// Використання при ініціалізації HLS:
func (h *HLSMuxer) initVideoCodec(sps, pps []byte) error {
    codecData, err := CreateH264CodecData(sps, pps)
    if err != nil {
        return err
    }
    
    // Додавання у заголовок муксера
    return h.WriteHeader([]av.CodecData{codecData, audioCodecData})
}
```

---

## 🎯 5. Slice Header Parser — визначення типу кадру

### Чому це важливо?

**Slice Header** містить інформацію про тип кадру (I/P/B), що критично для:
- Визначення ключових кадрів (IDR) для початку сегментів
- Оптимізації буферизації (B-frames потребують майбутніх кадрів)
- Детекції змін сцени для адаптивного бітрейту

### 🔍 Метод `ParseSliceHeaderFromNALU()`:

```go
func ParseSliceHeaderFromNALU(packet []byte) (sliceType SliceType, err error) {
    // 1. Перевірка мінімальної довжини
    if len(packet) <= 1 {
        err = fmt.Errorf("packet too short")
        return
    }
    
    // 2. Визначення типу NALU
    nal_unit_type := packet[0] & 0x1f
    switch nal_unit_type {
    case 1, 2, 5, 19:  // VCL NALUs з slice header
        // OK
    default:
        err = fmt.Errorf("nal_unit_type=%d has no slice header", nal_unit_type)
        return
    }
    
    // 3. Ініціалізація Golomb-читача (пропускаємо NALU header)
    r := &bits.GolombBitReader{R: bytes.NewReader(packet[1:])}
    
    // 4. Пропуск first_mb_in_slice (не потрібен для визначення типу)
    r.ReadExponentialGolombCode()
    
    // 5. Читання slice_type (Golomb-кодоване)
    u, err := r.ReadExponentialGolombCode()
    if err != nil {
        return
    }
    
    // 6. Мапінг значень у типи кадрів
    switch u {
    case 0, 3, 5, 8:   // P-кадри (передбачувані)
        sliceType = SLICE_P
    case 1, 6:         // B-кадри (двонаправлені)
        sliceType = SLICE_B
    case 2, 4, 7, 9:   // I-кадри (внутрішньо-кодовані)
        sliceType = SLICE_I
    default:
        err = fmt.Errorf("slice_type=%d invalid", u)
        return
    }
    
    return
}
```

### 📋 Мапінг slice_type значень:

```
Golomb-значення → Тип кадру:
  0, 3, 5, 8 → P-frame (predicted)
  1, 6       → B-frame (bi-predictive)
  2, 4, 7, 9 → I-frame (intra)

Примітка: Значення можуть бути +10 для "конформних" типів,
але для базового визначення достатньо цього мапінгу.
```

### ✅ Ваш use-case: оптимізація сегментації за типом кадру

```go
// ShouldStartSegment — чи починати новий сегмент з цього NALU?
func ShouldStartSegment(nalu []byte, lastSegmentTime time.Duration, minSegmentDur time.Duration) bool {
    // 1. Перевірка чи це IDR кадр (найнадійніший маркер)
    if nalu[0]&0x1f == 5 {  // NALU type 5 = IDR
        return true
    }
    
    // 2. Для не-IDR кадрів: перевірка slice header
    sliceType, err := h264parser.ParseSliceHeaderFromNALU(nalu)
    if err != nil {
        return false  // не можемо визначити — краще не ризикувати
    }
    
    // 3. I-frame (не IDR) також може бути точкою входу
    if sliceType == h264parser.SLICE_I {
        return true
    }
    
    // 4. Для P/B-кадрів: тільки якщо минув мінімальний час сегменту
    currentTime := extractTimeFromNALU(nalu)  // ваша логіка
    return currentTime-lastSegmentTime >= minSegmentDur
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// h264_processor.go — обробка H.264 потоків для HLS
type H264Processor struct {
    channelID    string
    sps, pps     []byte          // збережені SPS/PPS для заголовка
    lastKeyFrame time.Duration   // час останнього IDR для сегментації
    codecData    av.CodecData    // готовий CodecData для муксера
}

func NewH264Processor(channelID string) *H264Processor {
    return &H264Processor{
        channelID: channelID,
    }
}

// ProcessNALU — обробка одного NALU
func (p *H264Processor) ProcessNALU(nalu []byte, timestamp time.Duration) error {
    naluType := nalu[0] & 0x1f
    
    switch naluType {
    case h264parser.NALU_SPS:
        // Збереження SPS для майбутнього використання
        p.sps = make([]byte, len(nalu))
        copy(p.sps, nalu)
        
        // Парсинг для отримання метаданих
        spsInfo, err := h264parser.ParseSPS(nalu)
        if err != nil {
            return fmt.Errorf("parse SPS: %w", err)
        }
        log.Printf("Channel %s: SPS parsed, resolution=%dx%d, fps=%d", 
            p.channelID, spsInfo.Width, spsInfo.Height, spsInfo.FPS)
        
    case h264parser.NALU_PPS:
        // Збереження PPS
        p.pps = make([]byte, len(nalu))
        copy(p.pps, nalu)
        
        // Якщо є і SPS, і PPS — створюємо CodecData
        if p.sps != nil && p.codecData == nil {
            codecData, err := h264parser.NewCodecDataFromSPSAndPPS(p.sps, p.pps)
            if err != nil {
                return fmt.Errorf("create codec data: %w", err)
            }
            p.codecData = codecData
            log.Printf("Channel %s: H.264 codec data ready", p.channelID)
        }
        
    case 5:  // IDR frame
        // Визначення чи починати новий сегмент
        if p.shouldStartNewSegment(timestamp) {
            if err := p.finalizeCurrentSegment(); err != nil {
                return err
            }
            p.startNewSegment(p.codecData)
            p.lastKeyFrame = timestamp
        }
        // Додавання у поточний сегмент
        p.addNALUToSegment(nalu, timestamp)
        
    case 1, 2, 3, 4:  // Non-IDR VCL frames
        // Додавання у поточний сегмент без перевірки ключового кадру
        p.addNALUToSegment(nalu, timestamp)
        
    case h264parser.NALU_SEI:
        // Обробка метаданих (таймінги, субтитри тощо)
        p.processSEI(nalu, timestamp)
        
    default:
        // Ігнорування інших типів NALU або логування
        if Debug {
            log.Printf("Channel %s: unknown NALU type %d", p.channelID, naluType)
        }
    }
    
    return nil
}

// shouldStartNewSegment — логіка вирішення про початок сегменту
func (p *H264Processor) shouldStartNewSegment(timestamp time.Duration) bool {
    // Умови для нового сегменту:
    // 1. Це IDR кадр (вже перевірено вище)
    // 2. Пройшло достатньо часу від останнього сегменту
    // 3. Є дійсний codecData для заголовка
    
    if p.codecData == nil {
        return false  // не можемо почати без метаданих
    }
    
    minSegmentDur := 10 * time.Second  // цільова тривалість сегменту
    return timestamp-p.lastKeyFrame >= minSegmentDur
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"no SPS found in AVCDecoderConfRecord"** | SPS не передано у заголовку контейнера | Переконайтеся, що SPS/PPS надсилаються перед першим відео-кадром; для Annex B → AVCC конвертації додайте їх у `AVCDecoderConfRecord` |
| **"parse SPS failed: unexpected EOF"** | Неповні або пошкоджені дані SPS | Перевірте чи видалено emulation prevention bytes перед парсингом; переконайтеся, що передається весь NALU |
| **Роздільна здатність невірна** | Неправильний розрахунок кропування | Переконайтеся, що `frame_cropping_flag` обробляється коректно; перевірте `frame_mbs_only_flag` для interlaced відео |
| **FPS = 0** | VUI timing info відсутній у потоці | Це нормально для деяких джерел; встановіть FPS вручну з конфігурації каналу або детектуйте за інтервалом між ключовими кадрами |
| **Annex B/AVCC детект не працює** | Потік у незвичному форматі | Додайте логування `SplitNALUs()` для дебагу; перевірте чи немає додаткових байтів перед першим NALU |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування SPS/PPS:

```go
type SPSPPSCache struct {
    mu    sync.RWMutex
    sps   []byte
    pps   []byte
    hash  string  // для детекту змін
}

func (c *SPSPPSCache) Update(sps, pps []byte) bool {
    newHash := fmt.Sprintf("%x", sha256.Sum256(append(sps, pps...)))
    
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if c.hash == newHash {
        return false  // без змін
    }
    
    c.sps = make([]byte, len(sps))
    c.pps = make([]byte, len(pps))
    copy(c.sps, sps)
    copy(c.pps, pps)
    c.hash = newHash
    
    return true  // змінились
}
```

### 2. Пакетний парсинг NALU:

```go
// ParseNALUsBatch — парсинг кількох NALU за один виклик
func ParseNALUsBatch(data []byte, format int) ([]NALUInfo, error) {
    nalus, typ := h264parser.SplitNALUs(data)
    if typ != format {
        return nil, fmt.Errorf("expected format %d, got %d", format, typ)
    }
    
    results := make([]NALUInfo, 0, len(nalus))
    for _, nalu := range nalus {
        info := NALUInfo{
            Type: nalu[0] & 0x1f,
            Data: nalu,
        }
        if info.Type == 7 {  // SPS
            spsInfo, _ := h264parser.ParseSPS(nalu)
            info.SPSInfo = &spsInfo
        }
        results = append(results, info)
    }
    return results, nil
}
```

### 3. Моніторинг параметрів потоку:

```go
type H264Metrics struct {
    Resolution  prometheus.GaugeVec
    FPS         prometheus.GaugeVec
    Profile     prometheus.GaugeVec
    KeyFrameInterval prometheus.Histogram
}

func (m *H264Metrics) RecordSPS(spsInfo h264parser.SPSInfo, channelID string) {
    m.Resolution.WithLabelValues(channelID).Set(float64(spsInfo.Width * spsInfo.Height))
    if spsInfo.FPS > 0 {
        m.FPS.WithLabelValues(channelID).Set(float64(spsInfo.FPS))
    }
    m.Profile.WithLabelValues(channelID).Set(float64(spsInfo.ProfileIdc))
}

func (m *H264Metrics) RecordKeyFrameInterval(interval time.Duration, channelID string) {
    m.KeyFrameInterval.WithLabelValues(channelID).Observe(interval.Seconds())
}
```

---

## 📋 Чек-лист інтеграції h264parser

```go
// ✅ 1. Визначення формату вхідного потоку
nalus, typ := h264parser.SplitNALUs(data)
switch typ {
case h264parser.NALU_ANNEXB:
    // Обробка Annex B (потоковий)
case h264parser.NALU_AVCC:
    // Обробка AVCC (контейнерний)
default:
    // Fallback або помилка
}

// ✅ 2. Парсинг SPS/PPS для метаданих
for _, nalu := range nalus {
    typ := nalu[0] & 0x1f
    if typ == h264parser.NALU_SPS {
        spsInfo, err := h264parser.ParseSPS(nalu)
        if err != nil { /* handle error */ }
        // Використання spsInfo.Width/Height/FPS
    }
}

// ✅ 3. Створення CodecData для контейнера
if sps != nil && pps != nil {
    codecData, err := h264parser.NewCodecDataFromSPSAndPPS(sps, pps)
    if err != nil { /* handle error */ }
    // Використання codecData у WriteHeader()
}

// ✅ 4. Детекція ключових кадрів для сегментації
for _, nalu := range nalus {
    if nalu[0]&0x1f == 5 {  // IDR frame
        // Початок нового сегменту
    }
}

// ✅ 5. Конвертація формату якщо потрібно
if inputFormat == ANNEXB && outputFormat == AVCC {
    avccData, err := ConvertAnnexBToAVCC(data, sps, pps)
    if err != nil { /* handle error */ }
}

// ✅ 6. Метрики
monitoring.H264Resolution.Set(float64(spsInfo.Width * spsInfo.Height))
monitoring.H264FPS.Set(float64(spsInfo.FPS))
```

---

## 🔗 Корисні посилання

- 📄 [ITU-T H.264 Specification](https://www.itu.int/rec/T-REC-H.264) — офіційний стандарт
- 📄 [Annex B vs AVCC Explained](https://wiki.multimedia.cx/index.php/H.264) — детальне порівняння форматів
- 📄 [SPS/PPS Structure](https://www.iitk.ac.in/mwn/videolab/h264/SPS_PPS_Structure.pdf) — бітова структура параметр-сетів
- 💻 [vdk h264parser Package](https://pkg.go.dev/github.com/deepch/vdk/codec/h264parser) — GoDoc documentation
- 🎬 [HLS Video Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до відео у HLS

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Завжди зберігайте SPS/PPS** при їх отриманні — вони потрібні для ініціалізації декодера та створення HLS-заголовків.
> 2. **Використовуйте IDR кадри (type 5) як точки входу** для нових сегментів — це гарантує коректне відтворення з будь-якого місця.
> 3. **Валідуйте роздільну здатність та FPS** після парсингу SPS — невідповідність вимогам HLS може зламати плеєри.
> 4. **Кешуйте результат `ParseSPS()`** — парсинг бітових полів дорогий, а SPS змінюється рідко.
> 5. **Моніторьте інтервал між ключовими кадрами** — занадто великий інтервал (>10с) може погіршити досвід перегляду при перемиканні каналів.

Потрібен приклад інтеграції `h264parser.CodecData` з вашим `pubsub.Queue` для розподілу вже оброблених відео-пакетів між підписниками (HLS, WebSocket, архів)? Готовий допомогти! 🚀