# 🎬 Глибокий розбір `codec/vp8.go` — парсинг VP8 відеокадрів

Це **низькорівневий парсер стандарту VP8** (від Google, основа WebM/VP9), який дозволяє вашому CCTV HLS Processor обробляти аудіопотоки у форматі VP8: детектувати ключові кадри для сегментації, витягувати роздільну здатність та валідувати структуру кадрів перед передачею у FFmpeg. Розберемо архітектурно:

---

## 🧱 1. Архітектура кадру VP8: Frame Tag + Payload

### 🔍 Структура VP8 кадру (RFC 6386, Section 9.1):

```
[VP8 Frame]
├─ Frame Tag (3 байти, little-endian)
│  ├─ Bit 0:    Key frame flag (0=I-frame, 1=P-frame) ← критично для сегментації!
│  ├─ Bits 1-3: Version (0-7)
│  ├─ Bit 4:    Show frame flag (чи відображати цей кадр)
│  ├─ Bits 5-23: First partition size (19 біт = до 524,287 байт)
│
├─ Ключовий кадр? → додатковий заголовок:
│  ├─ Start code: 0x9D 0x01 0x2A (magic signature)
│  ├─ Width: 14 біт + 2 біти горизонтального масштабу
│  ├─ Height: 14 біт + 2 біти вертикального масштабу
│
└─ Основні дані: партиції, макроблоки, residuals...
```

### 🎯 Чому це важливо для HLS:

| Компонент | Роль у вашому пайплайні |
|-----------|-------------------------|
| **Frame Tag** | Миттєва детекція типу кадру без повного парсингу → швидке вирішення: "чи починати новий сегмент?" |
| **Key Frame Start Code** | Валідація цілісності ключового кадру → відхилення пошкоджених кадрів до FFmpeg |
| **Width/Height з масштабом** | Точний розрахунок роздільної здатності → коректні атрибути у HLS-плейлисті |

---

## 🔢 2. `VP8FrameTag` — 3 байти, що керують потоком

### 🔧 Бітова структура (літл-ендіан!):

```
Байти [0][1][2] у пам'яті → 24-бітне число:
tmp = frame[0] | (frame[1] << 8) | (frame[2] << 16)

Біти 0:    FrameType (0=I-frame, 1=P-frame)
Біти 1-3:  Version (0-7, зазвичай 0)
Біт   4:   Display/Show flag (1=показати, 0=приховати для B-frames)
Біти 5-23: FirstPartSize (розмір першої партиції у байтах)
```

### 🔧 Функція `DecodeFrameTag`:

```go
func DecodeFrameTag(frame []byte) (*VP8FrameTag, error) {
    if len(frame) < 3 {
        return nil, errors.New("frame bytes < 3")  // ⚠️ Повідомлення неточне: має бути "frame bytes < 3"
    }
    
    // 1. Зібрати 3 байти у 24-бітне число (литл-ендіан)
    var tmp uint32 = (uint32(frame[2]) << 16) | (uint32(frame[1]) << 8) | uint32(frame[0])
    
    tag := &VP8FrameTag{}
    
    // 2. Виділити поля бітовими масками
    tag.FrameType = tmp & 0x01              // біт 0: 0x01 = 0b00000001
    tag.Version = (tmp >> 1) & 0x07         // біти 1-3: 0x07 = 0b00000111
    tag.Display = (tmp >> 4) & 0x01         // біт 4
    tag.FirstPartSize = (tmp >> 5) & 0x7FFFF // біти 5-23: 0x7FFFF = 19 біт '1'
    
    return tag, nil
}
```

### 📐 Приклад декодування:

```
Вхід: frame = []byte{0x30, 0x02, 0x00}

1. tmp = 0x30 | (0x02 << 8) | (0x00 << 16) 
         = 0x30 | 0x200 | 0x0 
         = 0x230 = 0b0010_0011_0000

2. Розбір:
   • FrameType = 0x230 & 0x01 = 0 → I-frame ✓
   • Version = (0x230 >> 1) & 0x07 = 0x118 & 0x07 = 0 → version 0 ✓
   • Display = (0x230 >> 4) & 0x01 = 0x23 & 0x01 = 1 → show frame ✓
   • FirstPartSize = (0x230 >> 5) & 0x7FFFF = 0x11 & 0x7FFFF = 17 байт ✓
```

> 💡 **Практичне значення**: `FirstPartSize` вказує розмір **першої партиції** (заголовки + глобальні параметри). Решта кадру — це окремі партиції для паралельного декодування. Ваш `segmentAssembler` може використовувати це для попереднього виділення буферів.

---

## 🗝️ 3. `VP8KeyFrameHead` — роздільна здатність з масштабуванням

### 🔍 Магічний start code ключового кадру:

```
Байти 0-2: 0x9D 0x01 0x2A ← унікальна сигнатура ключового кадру VP8
           (не плутати з H.264 0x00000001!)
```

### 🔧 Бітова структура ширини/висоти:

```
Байти [3][4] для Width:
• Біти 0-13 байтів [3]+[4]&0x3F: фактична ширина (14 біт = до 16,383 пікселів)
• Біти 14-15 байту [4]>>6: горизонтальний масштаб (0=1:1, 1=5:4, 2=4:3, 3=16:9)

Аналогічно для Height у байтах [5][6]
```

### 🔧 Функція `DecodeKeyFrameHead`:

```go
func DecodeKeyFrameHead(frame []byte) (*VP8KeyFrameHead, error) {
    if len(frame) < 7 {  // 3 байти start code + 2 байти width + 2 байти height
        return nil, errors.New("frame bytes < 3")  // ⚠️ Опечатка: має бути "< 7"
    }

    // 1. Перевірка магічної сигнатури
    if frame[0] != 0x9d || frame[1] != 0x01 || frame[2] != 0x2a {
        return nil, errors.New("not find Start code")
    }

    head := &VP8KeyFrameHead{}
    
    // 2. Розпарсити Width: 14 біт даних + 2 біти масштабу
    head.Width = int(uint16(frame[4]&0x3f)<<8 | uint16(frame[3]))  // 0x3F = 0b00111111
    head.HorizScale = int(frame[4] >> 6)  // старші 2 біти
    
    // 3. Аналогічно для Height
    head.Height = int(uint16(frame[6]&0x3f)<<8 | uint16(frame[5]))
    head.VertScale = int(frame[6] >> 6)
    
    return head, nil
}
```

### 📐 Приклад розрахунку роздільної здатності:

```
Вхід: frame[3:7] = []byte{0x80, 0x07, 0x38, 0x04}

Width:
• frame[3] = 0x80 = 0b10000000
• frame[4] = 0x07 = 0b00000111
• frame[4] & 0x3F = 0x07 = 0b00000111
• Width = (0x07 << 8) | 0x80 = 0x780 | 0x80 = 0x780 + 128 = 1920 ✓
• HorizScale = 0x07 >> 6 = 0 → масштаб 1:1

Height:
• frame[5] = 0x38 = 0b00111000
• frame[6] = 0x04 = 0b00000100
• frame[6] & 0x3F = 0x04
• Height = (0x04 << 8) | 0x38 = 0x400 + 56 = 1080 ✓
• VertScale = 0x04 >> 6 = 0 → масштаб 1:1

Результат: 1920×1080 @ 1:1 scale ✓
```

### 🎯 Таблиця масштабів (специфікація VP8):

| Scale value | Aspect ratio | Коли використовується |
|-------------|-------------|---------------------|
| 0 | 1:1 (квадратні пікселі) | Більшість CCTV-камер |
| 1 | 5:4 | Старі 4:3 дисплеї |
| 2 | 4:3 | Класичний TV формат |
| 3 | 16:9 | Сучасні widescreen дисплеї |

> 💡 **Практичне значення**: Якщо `HorizScale = 3`, фактичне відображення буде `Width × (16/9)`. Ваш `VideoManifestProxy` має враховувати це при генерації `#EXT-X-STREAM-INF` атрибутів.

---

## 🎯 4. Helper-функції: `IsKeyFrame` та `GetResolution`

### 🔸 `IsKeyFrame` — швидка детекція точок сегментації:

```go
func IsKeyFrame(frame []byte) bool {
    tag, err := DecodeFrameTag(frame)
    if err != nil {
        return false  // ⚠️ Приховує помилку парсингу!
    }
    
    // VP8: FrameType == 0 → I-frame (ключовий кадр)
    if tag.FrameType == 0 {
        return true
    } else {
        return false  // ← можна спростити до: return tag.FrameType == 0
    }
}
```

### 🔸 `GetResolution` — витягнення розмірів тільки з ключових кадрів:

```go
func GetResloution(frame []byte) (width int, height int, err error) {  // ⚠️ Опечатка: "Resloution" → "Resolution"
    // 1. Перевірити, що це ключовий кадр (тільки вони містять заголовок з розмірами)
    if !IsKeyFrame(frame) {
        return 0, 0, errors.New("the frame is not Key frame")
    }

    // 2. Пропустити 3-байтовий Frame Tag → парсити KeyFrameHead
    head, err := DecodeKeyFrameHead(frame[3:])  // ← зсув на 3 байти!
    if err != nil {
        return 0, 0, err
    }
    
    return head.Width, head.Height, nil
}
```

### 🎯 Використання у вашому пайплайні:

```go
// У segmentAssembler для детекції початку нового сегменту:
func (sa *SegmentAssembler) handleVP8Frame(data []byte) error {
    // 1. Швидка перевірка: чи це ключовий кадр?
    if codec.IsKeyFrame(data) {
        // 2. Витягнути роздільну здатність для метрик/валідації
        width, height, err := codec.GetResolution(data)
        if err != nil {
            logger.Warn("failed to get VP8 resolution", "error", err)
        } else {
            sa.currentResolution = fmt.Sprintf("%dx%d", width, height)
        }
        
        // 3. Почати новий сегмент тільки на ключовому кадрі
        if sa.shouldStartNewSegment() {
            sa.finalizeCurrentSegment()
            sa.startNewSegment()
        }
    }
    
    // 4. Додати кадр у поточний сегмент
    sa.currentVideoSegment.AppendFrame(data)
    return nil
}
```

---

## 🐞 5. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Некоректні повідомлення про помилки**:
   ```go
   // У DecodeFrameTag:
   return nil, errors.New("frame bytes < 3")  // ← має бути "frame bytes < 3" (опечатка)
   
   // У DecodeKeyFrameHead:
   return nil, errors.New("frame bytes < 3")  // ← має бути "frame bytes < 7"!
   ```

2. **Приховування помилок у `IsKeyFrame`**:
   ```go
   if err != nil {
       return false  // ← Клієнт не дізнається, чому кадр не розпарсився!
   }
   // Краще повертати (bool, error) або логувати:
   if err != nil {
       logger.Debug("VP8 frame tag parse error", "error", err)
       return false
   }
   ```

3. **Опечатка у назві функції**:
   ```go
   func GetResloution(...)  // ← має бути GetResolution
   // Це ламає консистентність API та ускладнює пошук у коді
   ```

4. **Відсутня валідація масштабу**:
   ```go
   head.HorizScale = int(frame[4] >> 6)  // ← може бути 0-3, але не перевіряється
   // Краще додати:
   if head.HorizScale > 3 {
       return nil, errors.New("invalid horizontal scale")
   }
   ```

5. **Не враховується `Display` флаг**:
   ```go
   // Якщо Display == 0, кадр не має відображатись (наприклад, B-frame reference)
   // Але GetResolution все одно повертає розміри → потенційна розсинхронізація
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для розрахунку фактичної роздільної здатності з урахуванням масштабу
func (head *VP8KeyFrameHead) DisplayResolution() (width, height int) {
    // Таблиця масштабів згідно специфікації VP8
    scaleNum := [4]int{1, 5, 4, 16}
    scaleDen := [4]int{1, 4, 3, 9}
    
    w := head.Width * scaleNum[head.HorizScale] / scaleDen[head.HorizScale]
    h := head.Height * scaleNum[head.VertScale] / scaleDen[head.VertScale]
    return w, h
}

// 2. Валідація ключового кадру перед використанням
func (head *VP8KeyFrameHead) Validate() error {
    if head.Width < 16 || head.Width > 16384 {
        return fmt.Errorf("invalid width: %d", head.Width)
    }
    if head.Height < 16 || head.Height > 16384 {
        return fmt.Errorf("invalid height: %d", head.Height)
    }
    if head.HorizScale > 3 || head.VertScale > 3 {
        return errors.New("invalid scale factor")
    }
    return nil
}

// 3. Інтеграція з метриками
func (sa *SegmentAssembler) recordVP8Metrics(tag *codec.VP8FrameTag, head *codec.VP8KeyFrameHead) {
    frameType := "P-frame"
    if tag.FrameType == 0 {
        frameType = "I-frame"
    }
    
    metrics.VideoCodecProfile.WithLabelValues("VP8", frameType).Inc()
    
    if head != nil {
        w, h := head.DisplayResolution()
        metrics.VideoResolution.WithLabelValues(fmt.Sprintf("%dx%d", w, h)).Inc()
        
        if head.HorizScale != 0 || head.VertScale != 0 {
            metrics.VideoAspectRatio.WithLabelValues(
                fmt.Sprintf("%d:%d", 
                    [4]int{1,5,4,16}[head.HorizScale],
                    [4]int{1,4,3,9}[head.HorizScale]),
            ).Inc()
        }
    }
}
```

---

## 🎯 6. Інтеграція з вашим CCTV HLS Processor

### 📍 У `segmentAssembler` — уніфікована обробка відео:

```go
func (sa *SegmentAssembler) handleVideoChunk(codecID codec.CodecID, data []byte) error {
    switch codecID {
    case codec.CODECID_VIDEO_VP8:
        // 1. Швидка детекція ключового кадру
        if codec.IsKeyFrame(data) {
            // 2. Витягнути роздільну здатність (тільки для ключових кадрів)
            width, height, err := codec.GetResolution(data)
            if err == nil {
                sa.updateResolution(width, height)
            }
            
            // 3. Почати новий HLS-сегмент тільки на I-frame
            if sa.shouldStartNewSegment() {
                sa.finalizeCurrentSegment()
                sa.startNewSegment()
            }
        }
        
        // 4. Додати кадр у поточний сегмент (з валідацією)
        if err := sa.validateVP8Frame(data); err != nil {
            logger.Warn("skipping invalid VP8 frame", "error", err)
            return nil
        }
        sa.currentVideoSegment.AppendFrame(data)
        
    case codec.CODECID_VIDEO_H264:
        // ... існуюча логіка для H.264
    }
    return nil
}
```

### 📍 У `createTSSegment` — валідація VP8 сегменту:

```go
func validateVP8Segment(frames [][]byte) error {
    if len(frames) == 0 {
        return errors.New("empty VP8 segment")
    }
    
    // Перший кадр має бути ключовим для самостійного відтворення
    if !codec.IsKeyFrame(frames[0]) {
        return errors.New("VP8 segment must start with key frame")
    }
    
    // Перевірити консистентність роздільної здатності
    baseWidth, baseHeight, _ := codec.GetResolution(frames[0])
    for i, frame := range frames[1:] {
        if codec.IsKeyFrame(frame) {
            w, h, _ := codec.GetResolution(frame)
            if w != baseWidth || h != baseHeight {
                return fmt.Errorf("resolution changed at frame %d: %dx%d → %dx%d", 
                    i+1, baseWidth, baseHeight, w, h)
            }
        }
    }
    return nil
}
```

### 📍 У `VideoManifestProxy` — генерація HLS-атрибутів:

```go
func generateVP8StreamInfo(width, height, horizScale, vertScale int) string {
    // Розрахувати фактичне співвідношення сторін
    scaleNum := [4]int{1, 5, 4, 16}
    scaleDen := [4]int{1, 4, 3, 9}
    
    displayW := width * scaleNum[horizScale] / scaleDen[horizScale]
    displayH := height * scaleNum[vertScale] / scaleDen[vertScale]
    
    // VP8 у HLS: CODECS="vp8" (RFC 6386)
    return fmt.Sprintf(`#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d,CODECS="vp8"`, 
        calculateBandwidth(width, height), displayW, displayH)
}
```

---

## 🧭 Висновок: чому VP8 підтримка важлива для вашого проекту

| Аспект | VP8 | H.264 | Вигода для CCTV HLS |
|--------|-----|-------|-------------------|
| **Ліцензія** | Безкоштовний, open-source | Патентований (може вимагати ліцензію) | Уникнення юридичних ризиків |
| **Веб-сумісність** | Нативна підтримка у Chrome/Firefox | Вимагає ліцензійний декодер у деяких браузерах | Пряме відтворення у веб-клієнтах |
| **Ключові кадри** | Чіткий FrameType у заголовку | Потрібен парсинг NAL type | Швидша детекція сегментів |
| **Роздільна здатність** | У заголовку ключового кадру | У SPS (потрібен повний парсинг) | Менше затримка при ініціалізації |
| **Парсинг** | Простіший (3-байтовий тег) | Складніший (bitstream parsing) | Менше код → менше багів |

> 🔑 **Головна ідея**: Цей код — **легковаговий альтернативний шлях** для обробки відео у вашому пайплайні. Якщо камера передає VP8 (наприклад, через WebRTC → HLS транскодування), ви можете:
> 1. Миттєво детектувати ключові кадри через 3-байтовий тег
> 2. Витягувати роздільну здатність без парсингу всього бітстріму
> 3. Генерувати валідні HLS-плейлисти без залежності від FFmpeg для базової валідації

💡 **Фінальна порада**: Додайте юніт-тести для:
1. Коректності бітових масок у `DecodeFrameTag` (перевірити всі 8 комбінацій `Version`)
2. Обробки всіх 4 значень `HorizScale`/`VertScale` у `DecodeKeyFrameHead`
3. Edge cases: кадри < 3 байт, невалідні start codes, масштаб > 3

Це вбереже від тонких багів, які проявляються тільки на специфічних камерах з нестандартними налаштуваннями кодування.