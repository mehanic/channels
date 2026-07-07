# 🧪 Глибокий розбір: h264parser Test — Юніт-тести для SplitNALUs

Цей файл — **мінімалістичний юніт-тест** для функції `SplitNALUs()` у пакеті `h264parser`. Він перевіряє авто-детект та парсинг двох основних форматів H.264 потоків: **Annex B** (потоковий) та **AVCC** (контейнерний).

Розберемо тестові кейси, очікувану поведінку та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема тесту

```
┌────────────────────────────────────────┐
│ 📦 TestParser — SplitNALUs Validation  │
├────────────────────────────────────────┤
│                                         │
│  🎯 Тестує:                             │
│  • Авто-детект формату (Annex B vs AVCC)│
│  • Коректне розбиття на NALU            │
│  • Обробку start codes (3-byte/4-byte) │
│  • Обробку length-prefix (4-byte)      │
│                                         │
│  📊 Вхідні дані:                        │
│  • annexbFrame: hex з start codes      │
│  • avccFrame: hex з length prefixes    │
│                                         │
│  📤 Очікуваний результат:               │
│  • ok = true (формат розпізнано)       │
│  • len(nalus) = правильна кількість    │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔍 Детальний розбір тестових кейсів

### 1️⃣ Annex B тест: розбиття за start codes

```go
annexbFrame, _ := hex.DecodeString(
    "00000001223322330000000122332233223300000133000001000001",
)
```

### 🔢 Декодована структура:

```
Байти (hex):
00 00 00 01  |  22 33 22 33  |  00 00 00 01  |  22 33 22 33 22 33  |  00 00 01  |  33  |  00 00 01  |  00 00 01
├───────────┤  ├───────────┤  ├───────────┤  ├─────────────────┤  ├────────┤  ├──┤  ├────────┤  ├───────────┤
   Start Code    NALU #1        Start Code      NALU #2 (довший)    3-byte     │    3-byte      Start Code
   (4-byte)      (4 байти)      (4-byte)        (5 байт)           Start Code │    Start Code  (4-byte)
                                                                  (3-byte)   │    (3-byte)
                                                                             │
                                                                       NALU #3  NALU #4
                                                                       (1 байт) (3 байти)
```

### 🔍 Очікуваний результат `SplitNALUs()`:

```go
nalus = [][]byte{
    {0x22, 0x33, 0x22, 0x33},           // NALU #1
    {0x22, 0x33, 0x22, 0x33, 0x22, 0x33}, // NALU #2
    {0x33},                              // NALU #3
    {0x00, 0x00, 0x01},                  // NALU #4 (це насправді start code без payload — edge case!)
}
ok = h264parser.NALU_ANNEXB  // константа = 2
len(nalus) = 4
```

> ⚠️ **Примітка**: Останній `000001` без даних після нього — це граничний випадок. Реальна реалізація може або повернути порожній NALU, або проігнорувати його.

---

### 2️⃣ AVCC тест: розбиття за length-prefix

```go
avccFrame, _ := hex.DecodeString(
    "00000008aabbccaabbccaabb00000001aa",
)
```

### 🔢 Декодована структура:

```
Байти (hex):
00 00 00 08  |  aa bb cc aa bb cc aa bb  |  00 00 00 01  |  aa
├───────────┤  ├──────────────────────┤  ├───────────┤  ├──┤
   Length      NALU #1 payload           Length        │
   (8 байт)    (рівно 8 байт)            (1 байт)      │
                                                    │
                                              NALU #2 payload
                                              (1 байт: 0xaa)
```

### 🔍 Очікуваний результат `SplitNALUs()`:

```go
nalus = [][]byte{
    {0xaa, 0xbb, 0xcc, 0xaa, 0xbb, 0xcc, 0xaa, 0xbb}, // NALU #1 (8 байт)
    {0xaa},                                            // NALU #2 (1 байт)
}
ok = h264parser.NALU_AVCC  // константа = 1
len(nalus) = 2
```

---

## 🔄 Як працює `SplitNALUs()` — алгоритм детекту

### Крок 1: Перевірка на AVCC (length-prefix)

```go
val4 := pio.U32BE(b)  // перші 4 байти як big-endian uint32

// Якщо val4 <= len(b), це МОЖЕ бути довжина першого NALU
if val4 <= uint32(len(b)) {
    // Спроба парсити як AVCC:
    // 1. Прочитати length (4 байти)
    // 2. Взяти наступні length байт як NALU
    // 3. Повторити до кінця буфера
    // 4. Якщо все спожито без залишку → це точно AVCC
}
```

### Крок 2: Перевірка на Annex B (start codes)

```go
val3 := pio.U24BE(b)  // перші 3 байти
val4 := pio.U32BE(b)  // перші 4 байти

// Якщо знайдено 0x000001 або 0x00000001 → це Annex B
if val3 == 1 || val4 == 1 {
    // Спроба парсити як Annex B:
    // 1. Шукати наступний start code (0x000001 або 0x00000001)
    // 2. Виділити дані між start codes як NALU
    // 3. Повторити до кінця буфера
}
```

### Крок 3: Fallback — RAW формат

```go
// Якщо ні один з форматів не підійшов:
return [][]byte{b}, NALU_RAW  // один NALU без розділювачів
```

---

## ✅ Ваш use-case: валідація вхідних потоків у CCTV Processor

### Сценарій 1: Авто-детект формату при підключенні камери

```go
// DetectStreamFormat — визначення формату перших байт потоку
func DetectStreamFormat(data []byte) (format int, nalus [][]byte, err error) {
    nalus, format = h264parser.SplitNALUs(data)
    
    switch format {
    case h264parser.NALU_ANNEXB:
        log.Printf("Detected Annex B format, %d NALUs", len(nalus))
        return format, nalus, nil
        
    case h264parser.NALU_AVCC:
        log.Printf("Detected AVCC format, %d NALUs", len(nalus))
        return format, nalus, nil
        
    case h264parser.NALU_RAW:
        // Може бути один NALU або невідомий формат
        if len(nalus) == 1 && len(nalus[0]) > 0 {
            naluType := nalus[0][0] & 0x1f
            log.Printf("Detected RAW format, NALU type=%d", naluType)
            return format, nalus, nil
        }
        return format, nil, fmt.Errorf("unknown stream format")
        
    default:
        return format, nil, fmt.Errorf("unexpected format code: %d", format)
    }
}

// Використання при ініціалізації каналу:
func (p *CCTVProcessor) initVideoStream(channelID string, initialData []byte) error {
    format, nalus, err := DetectStreamFormat(initialData)
    if err != nil {
        return fmt.Errorf("detect format: %w", err)
    }
    
    // Збереження формату для подальшої обробки
    p.channelFormats[channelID] = format
    
    // Пошук SPS/PPS у перших NALU
    for _, nalu := range nalus {
        typ := nalu[0] & 0x1f
        if typ == h264parser.NALU_SPS {
            p.spsCache[channelID] = nalu
        } else if typ == h264parser.NALU_PPS {
            p.ppsCache[channelID] = nalu
        }
    }
    
    return nil
}
```

### Сценарій 2: Конвертація Annex B → AVCC для HLS

```go
// ConvertForHLS — підготовка відео-даних для MP4/HLS контейнера
func ConvertForHLS(data []byte, format int, sps, pps []byte) ([]byte, error) {
    // 1. Розбиття на NALU за вхідним форматом
    nalus, detectedFormat := h264parser.SplitNALUs(data)
    if detectedFormat != format {
        log.Printf("warning: expected format %d, detected %d", format, detectedFormat)
    }
    
    // 2. Якщо вже AVCC — повертаємо як є
    if format == h264parser.NALU_AVCC {
        return data, nil
    }
    
    // 3. Конвертація Annex B → AVCC
    var avccData []byte
    
    // Додаємо заголовок якщо це початок потоку
    if sps != nil && pps != nil {
        recordInfo := h264parser.AVCDecoderConfRecord{
            AVCProfileIndication: sps[1],
            ProfileCompatibility: sps[2],
            AVCLevelIndication:   sps[3],
            LengthSizeMinusOne:   3,  // 4-byte length
            SPS:                  [][]byte{sps},
            PPS:                  [][]byte{pps},
        }
        header := make([]byte, recordInfo.Len())
        recordInfo.Marshal(header)
        avccData = append(avccData, header...)
    }
    
    // Конвертуємо кожен NALU
    for _, nalu := range nalus {
        // Додаємо 4-байтову довжину
        length := uint32(len(nalu))
        avccData = append(avccData, 
            byte(length>>24), byte(length>>16), byte(length>>8), byte(length),
        )
        avccData = append(avccData, nalu...)
    }
    
    return avccData, nil
}
```

### Сценарій 3: Тестування з реальними даними камери

```go
// TestCameraStreamFormat — інтеграційний тест для конкретного виробника камер
func TestCameraStreamFormat(t *testing.T) {
    // Приклад реальних даних з Hikvision камери (Annex B)
    hikvisionFrame, _ := hex.DecodeString(
        "000000016742c01e95a810101010101010101010" +  // SPS
        "0000000168ce3880" +                          // PPS
        "0000000165b80000",                            // IDR frame
    )
    
    nalus, format := h264parser.SplitNALUs(hikvisionFrame)
    
    assert.Equal(t, h264parser.NALU_ANNEXB, format)
    assert.Equal(t, 3, len(nalus))
    
    // Перевірка типів NALU
    assert.Equal(t, byte(7), nalus[0][0]&0x1f)  // SPS
    assert.Equal(t, byte(8), nalus[1][0]&0x1f)  // PPS
    assert.Equal(t, byte(5), nalus[2][0]&0x1f)  // IDR
}

// Приклад даних з камери у AVCC форматі (напр. через RTMP)
func TestRTMPStreamFormat(t *testing.T) {
    // AVCC: 4-byte length + NALU
    rtmpFrame, _ := hex.DecodeString(
        "0000000b6742c01e95a8101010" +  // length=11, SPS
        "0000000468ce3880",              // length=4, PPS
    )
    
    nalus, format := h264parser.SplitNALUs(rtmpFrame)
    
    assert.Equal(t, h264parser.NALU_AVCC, format)
    assert.Equal(t, 2, len(nalus))
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Неправильний детект формату** | Annex B розпізнається як AVCC або навпаки | Додайте логування `val3`, `val4` у `SplitNALUs()` для дебагу; перевірте чи немає зайвих байтів перед першим NALU |
| **Порожні NALU у результаті** | `len(nalus[N]) == 0` для останнього елемента | Це очікувано для граничних випадків (start code в кінці); фільтруйте порожні NALU перед подальшою обробкою |
| **Emulation prevention bytes не оброблені** | `ParseSPS()` падає з "unexpected EOF" | Викликайте `RemoveH264orH265EmulationBytes()` перед парсингом SPS/PPS |
| **Неповні NALU у потоці** | Останній NALU обрізаний через мережеві втрати | Реалізуйте буферизацію та очікування повних NALU перед викликом `SplitNALUs()` |
| **Mixed format у потоці** | Частина даних Annex B, частина AVCC | Це помилка джерела; логуйте попередження та спробуйте відновитися з наступного ключового кадру |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування детекту формату:

```go
type FormatCache struct {
    mu      sync.RWMutex
    formats map[string]int  // channelID → detected format
}

func (c *FormatCache) Get(channelID string) (int, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    format, ok := c.formats[channelID]
    return format, ok
}

func (c *FormatCache) Set(channelID string, format int) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.formats[channelID] = format
}

// Використання: уникати повторного детекту для кожного пакету
if format, ok := formatCache.Get(channelID); ok {
    // Використовуємо відомий формат без виклику SplitNALUs() для детекту
    nalus := splitByKnownFormat(data, format)
} else {
    // Перший пакет — детектуємо формат
    nalus, format = h264parser.SplitNALUs(data)
    formatCache.Set(channelID, format)
}
```

### 2. Пакетна обробка для зменшення накладних витрат:

```go
// SplitNALUsBatch — обробка кількох фреймів за один виклик
func SplitNALUsBatch(frames [][]byte, format int) ([][][]byte, error) {
    results := make([][][]byte, 0, len(frames))
    
    for _, frame := range frames {
        nalus, detected := h264parser.SplitNALUs(frame)
        if format != 0 && detected != format {
            return nil, fmt.Errorf("format mismatch: expected %d, got %d", format, detected)
        }
        results = append(results, nalus)
    }
    return results, nil
}
```

### 3. Моніторинг розпізнавання форматів:

```go
type FormatMetrics struct {
    DetectedFormat prometheus.CounterVec
    ParseErrors    prometheus.CounterVec
    NALUCount      prometheus.HistogramVec
}

func (m *FormatMetrics) RecordSplit(nalus [][]byte, format int, channelID string, err error) {
    if err != nil {
        m.ParseErrors.WithLabelValues(channelID).Inc()
        return
    }
    m.DetectedFormat.WithLabelValues(channelID, fmt.Sprintf("%d", format)).Inc()
    m.NALUCount.WithLabelValues(channelID).Observe(float64(len(nalus)))
}
```

---

## 📋 Чек-лист інтеграції тесту SplitNALUs

```go
// ✅ 1. Додайте цей тест у ваш test suite
// h264parser_test.go
func TestParser(t *testing.T) {
    // ... існуючий код ...
}

// ✅ 2. Додайте більше тестових кейсів для ваших камер
func TestHikvisionAnnexB(t *testing.T) {
    // Реальні дані з Hikvision/Dahua/Axis камер
}

func TestRTMPAVCC(t *testing.T) {
    // Дані з RTMP/WebRTC джерел
}

// ✅ 3. Валідація вхідних даних у runtime
func ValidateIncomingData(data []byte) error {
    nalus, format := h264parser.SplitNALUs(data)
    
    if format == h264parser.NALU_RAW {
        return fmt.Errorf("unknown format, cannot split NALUs")
    }
    
    if len(nalus) == 0 {
        return fmt.Errorf("no NALUs found in data")
    }
    
    return nil
}

// ✅ 4. Логування для дебагу
if Debug {
    log.Printf("SplitNALUs: format=%d, count=%d, first_nalu_type=%d", 
        format, len(nalus), nalus[0][0]&0x1f)
}

// ✅ 5. Обробка помилок
nalus, format := h264parser.SplitNALUs(data)
if format == h264parser.NALU_RAW && len(nalus) == 1 {
    // Спроба обробити як один NALU
    processSingleNALU(nalus[0])
} else if format == h264parser.NALU_RAW {
    log.Warn("cannot split RAW data, skipping")
    return nil
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk h264parser Test](https://github.com/deepch/vdk/blob/master/codec/h264parser/parser_test.go) — оригінальний тест у репозиторії
- 📄 [H.264 NALU Structure](https://wiki.multimedia.cx/index.php/H.264) — детальний опис структури NALU
- 📄 [Annex B Start Codes](https://www.itu.int/rec/T-REC-H.264-202104-I/en) — специфікація Annex B у стандарті H.264
- 🧪 [Go Testing Package](https://pkg.go.dev/testing) — документація стандартної бібліотеки тестування

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **різними джерелами CCTV відео**:
> 1. **Додайте реальні тестові дані** з ваших камер у цей тест — це допоможе виявити специфічні формати виробників.
> 2. **Логувайте `format` та `len(nalus)`** у production для моніторингу стабільності потоків.
> 3. **Обробляйте `NALU_RAW` як fallback** — деякі джерела можуть надсилати окремі NALU без розділювачів.
> 4. **Кешуйте детект формату** на рівні каналу — не викликайте `SplitNALUs()` для детекту кожного пакету.
> 5. **Тестуйте edge cases**: порожні дані, неповні NALU, змішані формати — це допоможе уникнути падінь у production.

Потрібен приклад реалізації `splitByKnownFormat()` для оптимізації обробки після першого детекту формату? Готовий допомогти! 🚀