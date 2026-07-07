# 🎬 Глибокий розбір: h265parser — Парсер H.265/HEVC потоків

Цей файл — **реалізація парсингу H.265/HEVC відео-потоків** згідно зі стандартом ITU-T H.265 (MPEG-H Part 2). Він надає інструменти для роботи з **NALU**, **SPS парсингу**, **форматами потоків (Annex B/AVCC)** та інтеграцією з бібліотекою `vdk`.

Розберемо архітектуру, ключові відмінності від H.264 та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема h265parser

```
┌────────────────────────────────────────┐
│ 📦 h265parser — HEVC Stream Handling   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • NALU Type Constants (0-63)          │
│  • SPS Parser для HEVC                 │
│  • Annex B / AVCC детектор             │
│  • AVCDecoderConfRecord для HEVC       │
│  • CodecData інтеграція з av.CodecData │
│                                         │
│  📊 NALU Types (HEVC):                  │
│  • 0-31: VCL (відео-дані)              │
│  • 32: VPS (Video Parameter Set)       │
│  • 33: SPS (Sequence Parameter Set)    │
│  • 34: PPS (Picture Parameter Set)     │
│  • 19-21: IRAP (ключові кадри)         │
│                                         │
│  🔄 Формати потоків:                    │
│  • Annex B: start codes (0x000001)     │
│  • AVCC: length-prefixed (4-byte size) │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. NALU Types у H.265/HEVC

### Основні типи (порівняння з H.264):

```go
// VCL NAL Units (відео-дані):
const (
    NAL_UNIT_CODED_SLICE_TRAIL_R = 1  // Звичайний P-кадр (аналог H.264 type 1)
    NAL_UNIT_CODED_SLICE_IDR_W_RADL = 19  // IDR кадр (аналог H.264 type 5)
    NAL_UNIT_CODED_SLICE_CRA = 21  // Clean Random Access (новий тип у HEVC)
)

// Parameter Sets (метадані):
const (
    NAL_UNIT_VPS = 32  // Video Parameter Set (НОВИЙ у HEVC!)
    NAL_UNIT_SPS = 33  // Sequence Parameter Set
    NAL_UNIT_PPS = 34  // Picture Parameter Set
)

// Інші важливі типи:
const (
    NAL_UNIT_ACCESS_UNIT_DELIMITER = 35  // AUD (маркер початку кадру)
    NAL_UNIT_PREFIX_SEI = 39   // SEI перед кадром
    NAL_UNIT_SUFFIX_SEI = 40   // SEI після кадру
)
```

### 🆚 Відмінності від H.264:

| Характеристика | H.264 | H.265/HEVC |
|---------------|-------|------------|
| **Кількість типів NALU** | 32 (0-31) | 64 (0-63) |
| **VPS** | ❌ Ні | ✅ Так (тип 32) |
| **IRAP кадри** | IDR (type 5) | IDR_W_RADL (19), CRA (21) |
| **Профілі** | Baseline/Main/High | Main/Main10/RExt тощо |
| **Ефективність** | Базова | ~50% краща компресія |

### ✅ Ваш use-case: детекція ключових кадрів для HLS

```go
// IsKeyFrameHEVC — перевірка чи NALU є точкою входу (IRAP)
func IsKeyFrameHEVC(nalu []byte) bool {
    if len(nalu) == 0 {
        return false
    }
    // Тип у бітах 1-6 (біт 0 завжди 0 у HEVC)
    naluType := (nalu[0] >> 1) & 0x3f
    
    // IRAP типи: 16-23 (BLA, IDR, CRA)
    return naluType >= 16 && naluType <= 23
}

// IsDataNALU_HEVC — перевірка чи це відео-дані
func IsDataNALU_HEVC(b []byte) bool {
    typ := (b[0] >> 1) & 0x3f
    return typ <= 31  // 0-31 = VCL NALUs
}

// Використання у сегментації:
for _, nalu := range nalus {
    if IsKeyFrameHEVC(nalu) {
        // Початок нового HLS-сегменту
        startNewSegment()
    }
    writeNALUToSegment(nalu)
}
```

---

## 🧠 2. SPS Parser для HEVC — складніший ніж у H.264

### Структура `SPSInfo` для HEVC:

```go
type SPSInfo struct {
    // Профіль/рівень
    ProfileIdc                       uint
    LevelIdc                         uint
    generalProfileSpace              uint
    generalTierFlag                  uint
    generalProfileIDC                uint
    generalProfileCompatibilityFlags uint32
    generalConstraintIndicatorFlags  uint64
    generalLevelIDC                  uint
    
    // Роздільна здатність (у пікселях, не макроблоках!)
    PicWidthInLumaSamples  uint  // Ширина у пікселях
    PicHeightInLumaSamples uint  // Висота у пікселях
    Width  uint  // фінальна ширина (після кропу)
    Height uint  // фінальна висота (після кропу)
    
    // Параметри кропу
    CropLeft, CropRight, CropTop, CropBottom uint
    
    // Розширені параметри
    chromaFormat             uint  // 1=4:2:0, 2=4:2:2, 3=4:4:4
    bitDepthLumaMinus8       uint  // Глибина біт: 0=8-bit, 2=10-bit
    bitDepthChromaMinus8     uint
    
    // Temporal layers (для scalable coding)
    numTemporalLayers  uint
    temporalIdNested   uint
    
    // FPS (якщо вказано у VUI)
    fps uint
}
```

### 🔧 Ключові відмінності парсингу від H.264:

```go
func ParseSPS(sps []byte) (ctx SPSInfo, err error) {
    // 1. HEVC NALU має 2-байтовий заголовок (не 1 байт як у H.264)
    if len(sps) < 2 {
        err = ErrorH265IncorectUnitSize
        return
    }
    
    // 2. Видалення emulation prevention bytes
    rbsp := nal2rbsp(sps[2:])  // пропускаємо 2-байтовий заголовок
    
    // 3. Ініціалізація читача
    br := &bits.GolombBitReader{R: bytes.NewReader(rbsp)}
    
    // 4. Пропуск sps_video_parameter_set_id (4 біти)
    br.ReadBits(4)
    
    // 5. Читання sps_max_sub_layers_minus1 (3 біти)
    spsMaxSubLayersMinus1, _ := br.ReadBits(3)
    
    // 6. Парсинг profile_tier_level() — складніша структура ніж у H.264
    parsePTL(br, &ctx, spsMaxSubLayersMinus1)
    
    // 7. Читання роздільної здатності ПРЯМО у пікселях (не макроблоках!)
    ctx.PicWidthInLumaSamples, _ = br.ReadExponentialGolombCode()
    ctx.PicHeightInLumaSamples, _ = br.ReadExponentialGolombCode()
    ctx.Width = ctx.PicWidthInLumaSamples   // простіше ніж у H.264!
    ctx.Height = ctx.PicHeightInLumaSamples
    
    // 8. Обробка conformance_window (кропування)
    conformanceWindowFlag, _ := br.ReadBit()
    if conformanceWindowFlag != 0 {
        // Читання конфігурації кропу...
    }
    
    // 9. Читання bit depth та chroma format
    ctx.bitDepthLumaMinus8, _ = br.ReadExponentialGolombCode()
    ctx.bitDepthChromaMinus8, _ = br.ReadExponentialGolombCode()
    ctx.chromaFormat, _ = br.ReadExponentialGolombCode()
    
    // 10. Парсинг VUI для FPS (аналогічно H.264)
    // ...
    
    return ctx, nil
}
```

### 🔢 Формула розрахунку роздільної здатності (простіша ніж H.264):

```
HEVC: Width = PicWidthInLumaSamples - CropLeft*2 - CropRight*2
      Height = PicHeightInLumaSamples - CropTop*2 - CropBottom*2

H.264: Width = (MbWidth * 16) - CropLeft*2 - CropRight*2  ← складніше!

Приклад HEVC:
  PicWidthInLumaSamples = 1920, PicHeightInLumaSamples = 1080
  CropLeft = CropRight = CropTop = CropBottom = 0
  
  Width = 1920, Height = 1080  ← без множення на 16!
```

### ✅ Ваш use-case: валідація відео-параметрів для HLS

```go
// ValidateHEVCForHLS — перевірка чи HEVC відео сумісне з HLS
func ValidateHEVCForHLS(spsData []byte) error {
    spsInfo, err := h265parser.ParseSPS(spsData)
    if err != nil {
        return fmt.Errorf("parse SPS: %w", err)
    }
    
    // HLS підтримує обмежену кількість профілів HEVC
    validProfiles := map[uint]bool{
        1: true,  // Main
        2: true,  // Main10 (10-bit)
    }
    if !validProfiles[spsInfo.generalProfileIDC] {
        return fmt.Errorf("unsupported HEVC profile: %d", spsInfo.generalProfileIDC)
    }
    
    // Рівень: ≤ 4.1 для широкої сумісності
    if spsInfo.generalLevelIDC > 123 {  // 4.1 = 123 у HEVC
        return fmt.Errorf("unsupported HEVC level: %d.%d", 
            spsInfo.generalLevelIDC/30, spsInfo.generalLevelIDC%30)
    }
    
    // Роздільна здатність: не більше 3840×2160 (4K) для сумісності
    if spsInfo.Width > 3840 || spsInfo.Height > 2160 {
        return fmt.Errorf("resolution too high: %dx%d", spsInfo.Width, spsInfo.Height)
    }
    
    // Bit depth: 8-bit або 10-bit
    if spsInfo.bitDepthLumaMinus8 > 2 {
        return fmt.Errorf("unsupported bit depth: %d-bit", 8+spsInfo.bitDepthLumaMinus8)
    }
    
    return nil
}
```

---

## 🔄 3. Формати потоків: Annex B vs AVCC (аналогічно H.264)

### 🆚 Відмінності у заголовках NALU:

```
H.264 NALU header (1 байт):
  [0][ref_idc][nal_unit_type]
  Приклад: 0x67 = SPS (type 7)

HEVC NALU header (2 байти):
  [0][nal_unit_type][nuh_layer_id][nuh_temporal_id_plus1]
  Приклад: 0x40 0x01 = SPS (type 33)
```

### 🔍 Авто-детект формату `SplitNALUs()` (ідентично H.264):

```go
func SplitNALUs(b []byte) (nalus [][]byte, typ int) {
    // Логіка ідентична h264parser.SplitNALUs():
    // 1. Перевірка на AVCC (4-byte length prefix)
    // 2. Перевірка на Annex B (start codes 0x000001/0x00000001)
    // 3. Fallback: RAW формат
    
    // Єдина відмінність: інтерпретація типів NALU після розбиття
    return nalus, typ  // тип: NALU_RAW/NALU_AVCC/NALU_ANNEXB
}
```

### ✅ Ваш use-case: конвертація Annex B → AVCC для HEVC

```go
// ConvertHEVCAnnexBToAVCC — конвертація для MP4/HLS контейнера
func ConvertHEVCAnnexBToAVCC(annexBData []byte, vps, sps, pps []byte) ([]byte, error) {
    // 1. Розбиття на NALU за start codes
    nalus, typ := h265parser.SplitNALUs(annexBData)
    if typ != h265parser.NALU_ANNEXB {
        return nil, fmt.Errorf("expected Annex B format")
    }
    
    // 2. Створення HEVCDecoderConfRecord (аналог AVCDecoderConfRecord)
    recordInfo := h265parser.AVCDecoderConfRecord{
        // Профіль/рівень з перших байтів SPS
        AVCProfileIndication: sps[1],  // byte 1 HEVC SPS
        ProfileCompatibility: sps[2],
        AVCLevelIndication:   sps[3],
        LengthSizeMinusOne:   3,  // 4-byte length prefix
        VPS:                  [][]byte{vps},  // НОВЕ: VPS обов'язковий у HEVC!
        SPS:                  [][]byte{sps},
        PPS:                  [][]byte{pps},
    }
    
    header := make([]byte, recordInfo.Len())
    recordInfo.Marshal(header, spsInfo)  // HEVC Marshal потребує SPSInfo
    
    // 3. Конвертація кожного NALU
    var avccData []byte
    avccData = append(avccData, header...)
    
    for _, nalu := range nalus {
        length := uint32(len(nalu))
        avccData = append(avccData, 
            byte(length>>24), byte(length>>16), byte(length>>8), byte(length),
        )
        avccData = append(avccData, nalu...)
    }
    
    return avccData, nil
}
```

---

## 📦 4. CodecData — інтеграція з av.CodecData

### Структура `AVCDecoderConfRecord` для HEVC:

```go
type AVCDecoderConfRecord struct {
    // Загальні поля (аналогічні H.264)
    AVCProfileIndication uint8
    ProfileCompatibility uint8
    AVCLevelIndication   uint8
    LengthSizeMinusOne   uint8
    
    // НОВЕ: VPS обов'язковий у HEVC!
    VPS [][]byte  // Video Parameter Set
    SPS [][]byte  // Sequence Parameter Set
    PPS [][]byte  // Picture Parameter Set
}
```

### 🔧 Методи маршалінгу (відмінності від H.264):

```go
// Unmarshal для HEVC — інша структура заголовка
func (self *AVCDecoderConfRecord) Unmarshal(b []byte) (n int, err error) {
    // HEVC заголовок має більше полів ніж H.264
    if len(b) < 30 {  // Мінімальний розмір для HEVC
        err = ErrDecconfInvalid
        return
    }
    
    // Читання базових полів
    self.AVCProfileIndication = b[1]
    self.ProfileCompatibility = b[2]
    self.AVCLevelIndication = b[3]
    self.LengthSizeMinusOne = b[4] & 0x03
    
    // Читання VPS (новий для HEVC)
    vpscount := int(b[25] & 0x1f)  // позиція 25, не 5 як у H.264!
    n += 26  // зміщення після заголовка
    
    for i := 0; i < vpscount; i++ {
        // Читання VPS NALU...
    }
    
    // Читання SPS та PPS (аналогічно H.264, але з іншими зміщеннями)
    // ...
    
    return n, nil
}

// Marshal для HEVC — потребує SPSInfo для коректного заповнення
func (self AVCDecoderConfRecord) Marshal(b []byte, si SPSInfo) (n int) {
    // Запис заголовка
    b[0] = 1  // версія
    b[1] = self.AVCProfileIndication
    // ... інші поля ...
    
    // Запис VPS (обов'язково першим!)
    b[n] = (self.VPS[0][0] >> 1) & 0x3f  // витягування типу NALU з 2-байтового заголовка
    n++
    // ... запис довжини та даних VPS ...
    
    // Запис SPS та PPS (аналогічно)
    // ...
    
    return n
}
```

### ✅ Ваш use-case: створення CodecData для HEVC HLS

```go
// CreateHEVCCodecData — створення av.CodecData з VPS/SPS/PPS
func CreateHEVCCodecData(vps, sps, pps []byte) (av.CodecData, error) {
    // 1. Парсинг SPS для отримання метаданих
    spsInfo, err := h265parser.ParseSPS(sps)
    if err != nil {
        return nil, fmt.Errorf("parse SPS: %w", err)
    }
    
    // 2. Створення AVCDecoderConfRecord для HEVC
    recordInfo := h265parser.AVCDecoderConfRecord{
        AVCProfileIndication: sps[1],
        ProfileCompatibility: sps[2],
        AVCLevelIndication:   sps[3],
        LengthSizeMinusOne:   3,  // 4-byte length
        VPS:                  [][]byte{vps},  // ОБОВ'ЯЗКОВО для HEVC!
        SPS:                  [][]byte{sps},
        PPS:                  [][]byte{pps},
    }
    
    // 3. Маршалінг (HEVC потребує SPSInfo)
    recordBytes := make([]byte, recordInfo.Len())
    recordInfo.Marshal(recordBytes, spsInfo)
    
    // 4. Створення CodecData
    codecData, err := h265parser.NewCodecDataFromAVCDecoderConfRecord(recordBytes)
    if err != nil {
        return nil, err
    }
    
    // 5. Перевірка сумісності з HLS
    if err := ValidateHEVCForHLS(sps); err != nil {
        log.Printf("warning: HEVC may not be HLS-compatible: %v", err)
    }
    
    return codecData, nil
}
```

---

## 🎯 5. Slice Header Parser для HEVC

### Відмінності від H.264:

```go
func ParseSliceHeaderFromNALU(packet []byte) (sliceType SliceType, err error) {
    // 1. HEVC має 2-байтовий заголовок NALU
    if len(packet) <= 1 {
        err = fmt.Errorf("packet too short")
        return
    }
    
    // 2. Витягування типу з бітів 1-6 (не 0-4 як у H.264)
    nal_unit_type := (packet[0] >> 1) & 0x3f
    
    // 3. Перевірка чи це VCL NALU (тип 0-31)
    switch nal_unit_type {
    case 0, 1, 2, 19, 20, 21:  // HEVC VCL типи з slice header
        // OK
    default:
        err = fmt.Errorf("nal_unit_type=%d has no slice header", nal_unit_type)
        return
    }
    
    // 4. Парсинг slice header (аналогічно H.264, але з іншими полями)
    r := &bits.GolombBitReader{R: bytes.NewReader(packet[2:])}  // пропускаємо 2 байти!
    
    // ... парсинг first_slice_segment_in_pic_flag, slice_type тощо ...
    
    return sliceType, nil
}
```

### ✅ Ваш use-case: оптимізація сегментації для HEVC

```go
// ShouldStartSegmentHEVC — логіка для HEVC ключових кадрів
func ShouldStartSegmentHEVC(nalu []byte, timestamp time.Duration, lastKeyFrame time.Duration) bool {
    // 1. Перевірка типу NALU
    if len(nalu) < 2 {
        return false
    }
    naluType := (nalu[0] >> 1) & 0x3f
    
    // 2. IRAP кадри (16-23) — точки входу
    if naluType >= 16 && naluType <= 23 {
        return true
    }
    
    // 3. Для не-IRAP: тільки якщо минув мінімальний час
    minSegmentDur := 10 * time.Second
    return timestamp-lastKeyFrame >= minSegmentDur
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// hevc_processor.go — обробка H.265/HEVC потоків для HLS
type HEVCProcessor struct {
    channelID    string
    vps, sps, pps []byte  // збережені параметр-сети
    lastKeyFrame time.Duration
    codecData    av.CodecData
}

func NewHEVCProcessor(channelID string) *HEVCProcessor {
    return &HEVCProcessor{channelID: channelID}
}

// ProcessNALU — обробка одного HEVC NALU
func (p *HEVCProcessor) ProcessNALU(nalu []byte, timestamp time.Duration) error {
    if len(nalu) < 2 {
        return fmt.Errorf("NALU too short")
    }
    
    naluType := (nalu[0] >> 1) & 0x3f
    
    switch naluType {
    case h265parser.NAL_UNIT_VPS:
        // Збереження VPS (обов'язковий для HEVC!)
        p.vps = make([]byte, len(nalu))
        copy(p.vps, nalu)
        
    case h265parser.NAL_UNIT_SPS:
        p.sps = make([]byte, len(nalu))
        copy(p.sps, nalu)
        
        // Парсинг для метаданих
        spsInfo, err := h265parser.ParseSPS(nalu)
        if err != nil {
            return fmt.Errorf("parse SPS: %w", err)
        }
        log.Printf("Channel %s: HEVC SPS parsed, resolution=%dx%d", 
            p.channelID, spsInfo.Width, spsInfo.Height)
        
    case h265parser.NAL_UNIT_PPS:
        p.pps = make([]byte, len(nalu))
        copy(p.pps, nalu)
        
        // Створення CodecData якщо є всі параметр-сети
        if p.vps != nil && p.sps != nil && p.pps != nil && p.codecData == nil {
            codecData, err := h265parser.NewCodecDataFromVPSAndSPSAndPPS(
                p.vps, p.sps, p.pps)
            if err != nil {
                return fmt.Errorf("create codec data: %w", err)
            }
            p.codecData = codecData
            log.Printf("Channel %s: HEVC codec data ready", p.channelID)
        }
        
    case 16, 17, 18, 19, 20, 21:  // IRAP кадри (ключові)
        if p.shouldStartNewSegment(timestamp) {
            if err := p.finalizeCurrentSegment(); err != nil {
                return err
            }
            p.startNewSegment(p.codecData)
            p.lastKeyFrame = timestamp
        }
        p.addNALUToSegment(nalu, timestamp)
        
    case 0, 1, 2, 3, 4, 5:  // Звичайні VCL кадри
        p.addNALUToSegment(nalu, timestamp)
        
    default:
        if Debug {
            log.Printf("Channel %s: unknown HEVC NALU type %d", p.channelID, naluType)
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"no VPS found in AVCDecoderConfRecord"** | VPS не передано у заголовку | Переконайтеся, що VPS надсилається перед першим відео-кадром; для HEVC VPS обов'язковий! |
| **"parse SPS failed: unexpected EOF"** | Неповні дані або неправильний заголовок | Переконайтеся, що пропускаєте 2 байти заголовка перед парсингом; перевірте emulation prevention bytes |
| **Роздільна здатність невірна** | Неправильне читання піксельних значень | У HEVC роздільна здатність у пікселях, не макроблоках; не множте на 16! |
| **FPS = 0** | VUI timing info відсутній | Встановіть FPS з конфігурації каналу або детектуйте за інтервалом між ключовими кадрами |
| **Annex B/AVCC детект не працює** | Потік у незвичному форматі | Додайте логування `SplitNALUs()`; перевірте чи немає додаткових байтів перед першим NALU |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування VPS/SPS/PPS:

```go
type HEVCParamCache struct {
    mu    sync.RWMutex
    vps   []byte
    sps   []byte
    pps   []byte
    hash  string
}

func (c *HEVCParamCache) Update(vps, sps, pps []byte) bool {
    newHash := fmt.Sprintf("%x", sha256.Sum256(append(vps, append(sps, pps...)...)))
    
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if c.hash == newHash {
        return false
    }
    
    c.vps, c.sps, c.pps = make([]byte, len(vps)), make([]byte, len(sps)), make([]byte, len(pps))
    copy(c.vps, vps)
    copy(c.sps, sps)
    copy(c.pps, pps)
    c.hash = newHash
    
    return true
}
```

### 2. Пакетний парсинг NALU:

```go
// ParseHEVCNALUsBatch — обробка кількох NALU за один виклик
func ParseHEVCNALUsBatch(data []byte, format int) ([]HEVCNALUInfo, error) {
    nalus, typ := h265parser.SplitNALUs(data)
    if typ != format {
        return nil, fmt.Errorf("expected format %d, got %d", format, typ)
    }
    
    results := make([]HEVCNALUInfo, 0, len(nalus))
    for _, nalu := range nalus {
        info := HEVCNALUInfo{
            Type: (nalu[0] >> 1) & 0x3f,  // HEVC тип з 2-байтового заголовка
            Data: nalu,
        }
        if info.Type == h265parser.NAL_UNIT_SPS {
            spsInfo, _ := h265parser.ParseSPS(nalu)
            info.SPSInfo = &spsInfo
        }
        results = append(results, info)
    }
    return results, nil
}
```

### 3. Моніторинг параметрів потоку:

```go
type HEVCMetrics struct {
    Resolution  prometheus.GaugeVec
    FPS         prometheus.GaugeVec
    Profile     prometheus.GaugeVec
    BitDepth    prometheus.GaugeVec
    KeyFrameInterval prometheus.Histogram
}

func (m *HEVCMetrics) RecordSPS(spsInfo h265parser.SPSInfo, channelID string) {
    m.Resolution.WithLabelValues(channelID).Set(float64(spsInfo.Width * spsInfo.Height))
    if spsInfo.fps > 0 {
        m.FPS.WithLabelValues(channelID).Set(float64(spsInfo.fps))
    }
    m.Profile.WithLabelValues(channelID).Set(float64(spsInfo.generalProfileIDC))
    m.BitDepth.WithLabelValues(channelID).Set(float64(8 + spsInfo.bitDepthLumaMinus8))
}
```

---

## 📋 Чек-лист інтеграції h265parser

```go
// ✅ 1. Визначення формату вхідного потоку
nalus, typ := h265parser.SplitNALUs(data)
switch typ {
case h265parser.NALU_ANNEXB:
    // Обробка Annex B
case h265parser.NALU_AVCC:
    // Обробка AVCC
}

// ✅ 2. Парсинг VPS/SPS/PPS для метаданих
for _, nalu := range nalus {
    typ := (nalu[0] >> 1) & 0x3f
    switch typ {
    case h265parser.NAL_UNIT_VPS:
        // Зберегти VPS
    case h265parser.NAL_UNIT_SPS:
        spsInfo, err := h265parser.ParseSPS(nalu)
        // Використання spsInfo.Width/Height
    case h265parser.NAL_UNIT_PPS:
        // Зберегти PPS
    }
}

// ✅ 3. Створення CodecData для контейнера (ОБОВ'ЯЗКОВО з VPS!)
if vps != nil && sps != nil && pps != nil {
    codecData, err := h265parser.NewCodecDataFromVPSAndSPSAndPPS(vps, sps, pps)
    // Використання codecData у WriteHeader()
}

// ✅ 4. Детекція ключових кадрів (IRAP типи 16-23)
for _, nalu := range nalus {
    typ := (nalu[0] >> 1) & 0x3f
    if typ >= 16 && typ <= 23 {
        // Початок нового сегменту
    }
}

// ✅ 5. Конвертація формату якщо потрібно
if inputFormat == ANNEXB && outputFormat == AVCC {
    avccData, err := ConvertHEVCAnnexBToAVCC(data, vps, sps, pps)
}

// ✅ 6. Метрики
monitoring.HEVCResolution.Set(float64(spsInfo.Width * spsInfo.Height))
monitoring.HEVCBitDepth.Set(float64(8 + spsInfo.bitDepthLumaMinus8))
```

---

## 🔗 Корисні посилання

- 📄 [ITU-T H.265 Specification](https://www.itu.int/rec/T-REC-H.265) — офіційний стандарт HEVC
- 📄 [HEVC NALU Structure](https://wiki.multimedia.cx/index.php/HEVC) — детальний опис структури NALU
- 📄 [HEVC SPS/PPS/VPS](https://www.iitk.ac.in/mwn/videolab/hevc/HEVC_SPS_PPS_VPS.pdf) — бітова структура параметр-сетів
- 💻 [vdk h265parser Package](https://pkg.go.dev/github.com/deepch/vdk/codec/h265parser) — GoDoc documentation
- 🎬 [HLS HEVC Requirements](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4) — вимоги до відео у HLS

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Завжди зберігайте VPS/SPS/PPS** — VPS обов'язковий для HEVC, без нього декодер не ініціалізується.
> 2. **Використовуйте IRAP кадри (тип 16-23)** як точки входу для сегментів — це гарантує коректне відтворення.
> 3. **Валідуйте profile/level/bit depth** після парсингу SPS — HEVC має суворіші вимоги сумісності ніж H.264.
> 4. **Кешуйте результат `ParseSPS()`** — парсинг бітових полів дорогий, а параметр-сети змінюються рідко.
> 5. **Моніторьте інтервал між ключовими кадрами** — HEVC може мати довший GOP, що впливає на досвід перегляду.

Потрібен приклад інтеграції `h265parser.CodecData` з вашим `pubsub.Queue` для розподілу вже оброблених HEVC-пакетів між підписниками? Готовий допомогти! 🚀