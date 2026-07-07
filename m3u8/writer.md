# 🎬 `m3u8/generator.go`: Генерація HLS-плейлистів у M3U8 форматі

Це **ключовий модуль** бібліотеки `github.com/grafov/m3u8`, який реалізує **генерацію M3U8-файлів** з типобезпечних структур даних. Він перетворює об'єкти `MasterPlaylist` та `MediaPlaylist` у текстовий формат, дотримуючись специфікації HLS (RFC 8216) та підтримуючи розширення (Widevine, SCTE-35, кастомні теги).

---

## 🎯 Коротка відповідь

> **Це "принтер" бібліотеки m3u8**: він бере типобезпечні Go-структури (плейлисти, сегменти, варіанти) і генерує валідні M3U8-файли з підтримкою шифрування, адаптивного стрімінгу, SCTE-35 сповіщень та кастомних тегів — ідеально для інтеграції CCTV-метаданих у ваш HLS-конвеєр.

---

## 🧱 Архітектура генерації: Огляд компонентів

### 🔹 Допоміжні функції

```go
// 🔹 Оновлення версії плейлиста згідно з розділом 7 специфікації
func version(ver *uint8, newver uint8) {
    if *ver < newver {
        *ver = newver  // 🔹 Підвищуємо версію тільки якщо потрібно
    }
}

// 🔹 Конвертація версії у рядок
func strver(ver uint8) string {
    return strconv.FormatUint(uint64(ver), 10)
}
```

**🎯 Призначення**: Забезпечити **автоматичне підвищення версії** при використанні функцій, що вимагають новішої специфікації.

**📋 Правила версій (згідно з розділом 7):**

| Версія | Додані можливості | Автоматичне підвищення у коді |
|--------|------------------|-----------------------------|
| **≥2** | `IV` у `#EXT-X-KEY` | `SetDefaultKey(..., iv, ...)` |
| **≥3** | Плаваючі тривалості в `#EXTINF` | `DurationAsInt(false)` |
| **≥4** | `#EXT-X-BYTERANGE`, `#EXT-X-I-FRAME-STREAM-INF`, `#EXT-X-MEDIA` | `SetRange()`, `Append(..., Iframe=true)`, `AppendAlternate()` |
| **≥5** | `KEYFORMAT`/`KEYFORMATVERSIONS` у `#EXT-X-KEY`, `#EXT-X-MAP` | `SetDefaultKey(..., keyformat, ...)` |

---

### 🔹 `NewMasterPlaylist` / `NewMediaPlaylist` — конструктори

```go
func NewMasterPlaylist() *MasterPlaylist {
    p := new(MasterPlaylist)
    p.ver = minver  // 🔹 Версія 3 за замовчуванням
    return p
}

func NewMediaPlaylist(winsize uint, capacity uint) (*MediaPlaylist, error) {
    p := new(MediaPlaylist)
    p.ver = minver
    p.capacity = capacity
    if err := p.SetWinSize(winsize); err != nil {  // 🔹 Валідація: winsize <= capacity
        return nil, err
    }
    p.Segments = make([]*MediaSegment, capacity)  // 🔹 Виділення масиву
    return p, nil
}
```

**🎯 Призначення**: Створити порожні плейлисти з правильними параметрами за замовчуванням.

---

### 🔹 `MasterPlaylist.Append` — додавання варіанту якості

```go
func (p *MasterPlaylist) Append(uri string, chunklist *MediaPlaylist, params VariantParams) {
    v := new(Variant)
    v.URI = uri
    v.Chunklist = chunklist
    v.VariantParams = params
    p.Variants = append(p.Variants, v)
    
    // 🔹 Автоматичне підвищення версії при використанні #EXT-X-MEDIA
    if len(v.Alternatives) > 0 {
        version(&p.ver, 4)  // 🔹 Альтернативні доріжки вимагають версію ≥4
    }
    p.buf.Reset()  // 🔹 Скидання кешу для наступної генерації
}
```

**🎯 Призначення**: Додати новий варіант якості до master-плейлиста з автоматичним оновленням версії.

---

### 🔹 `MasterPlaylist.Encode` — генерація master-плейлиста

**🔄 Потік генерації:**
```
🔹 Вхід: *MasterPlaylist з варіантами
│
▼
🔹 Перевірка кешу: if p.buf.Len() > 0 { return &p.buf }  // 🔹 Повертаємо кешований результат
│
▼
🔹 Заголовок:
   • "#EXTM3U\n#EXT-X-VERSION:" + версія
   • "#EXT-X-INDEPENDENT-SEGMENTS" (якщо встановлено)
   • Кастомні теги плейлиста
│
▼
🔹 Ітерація по варіантах:
   │
   ├── 🔹 Альтернативні доріжки (#EXT-X-MEDIA):
   │   • Форматування атрибутів: TYPE, GROUP-ID, NAME, DEFAULT, LANGUAGE, URI...
   │   • Уникнення дублікатів через altsWritten map
   │
   ├── 🔹 I-frame only варіанти (#EXT-X-I-FRAME-STREAM-INF):
   │   • Форматування: PROGRAM-ID, BANDWIDTH, CODECS, RESOLUTION, URI...
   │
   ├── 🔹 Звичайні варіанти (#EXT-X-STREAM-INF):
   │   • Форматування: BANDWIDTH, CODECS, RESOLUTION, AUDIO, VIDEO, SUBTITLES, CLOSED-CAPTIONS...
   │   • Особлива обробка CLOSED-CAPTIONS="NONE" (без лапок)
   │   • Додавання URI варіанту з опціональними аргументами (?args)
   │
   ▼
🔹 Вихід: *bytes.Buffer з готовим M3U8-файлом
```

**🔢 Приклад виводу:**
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Українська",DEFAULT=YES,LANGUAGE="uk",URI="audio/uk/index.m3u8"
#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=2500000,CODECS="avc1.64001f,mp4a.40.2",RESOLUTION=1280x720,AUDIO="audio",FRAME-RATE=30.000
720p/index.m3u8
#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=1200000,CODECS="avc1.64001f,mp4a.40.2",RESOLUTION=854x480,AUDIO="audio",FRAME-RATE=30.000
480p/index.m3u8
```

---

### 🔹 `MediaPlaylist.AppendSegment` — додавання сегмента з ковзним вікном

```go
func (p *MediaPlaylist) AppendSegment(seg *MediaSegment) error {
    // 🔹 Перевірка на заповнення: head == tail && count > 0 → playlist full
    if p.head == p.tail && p.count > 0 {
        return ErrPlaylistFull
    }
    
    // 🔹 Розрахунок SeqId: послідовний номер сегмента
    seg.SeqId = p.SeqNo
    if p.count > 0 {
        seg.SeqId = p.Segments[(p.capacity+p.tail-1)%p.capacity].SeqId + 1
    }
    
    // 🔹 Додавання у циклічний буфер
    p.Segments[p.tail] = seg
    p.tail = (p.tail + 1) % p.capacity
    p.count++
    
    // 🔹 Автоматичне оновлення TargetDuration
    if p.TargetDuration < seg.Duration {
        p.TargetDuration = math.Ceil(seg.Duration)  // 🔹 Згідно зі специфікацією: ціле число
    }
    
    p.buf.Reset()  // 🔹 Скидання кешу
    return nil
}
```

**🔄 Логіка циклічного буфера для live-плейлистів:**
```
🔹 Початок: capacity=6, winsize=3, head=0, tail=0, count=0
   Segments: [nil, nil, nil, nil, nil, nil]

🔹 Додавання seg0:
   • p.Segments[0] = seg0, tail=1, count=1
   • Segments: [seg0, nil, nil, nil, nil, nil]

🔹 Додавання seg1, seg2:
   • Segments: [seg0, seg1, seg2, nil, nil, nil], tail=3, count=3

🔹 Додавання seg3 (count >= winsize):
   • Якщо live: p.Remove() → head=1, count=2
   • Додавання: p.Segments[3] = seg3, tail=4, count=3
   • Segments: [seg0, seg1, seg2, seg3, nil, nil]
   • Активні сегменти: head=1 → tail=4: [seg1, seg2, seg3]

🔹 Генерація: ітерація від head до tail (останні winsize сегментів)
```

**🎯 Призначення**: Ефективне керування **ковзним вікном** для live-стрімінгу без постійного виділення пам'яті.

---

### 🔹 `MediaPlaylist.Encode` — генерація media-плейлиста

**🔄 Потік генерації (спрощено):**
```
🔹 Заголовок:
   • "#EXTM3U\n#EXT-X-VERSION:" + версія
   • Кастомні теги плейлиста
   • #EXT-X-KEY (якщо p.Key != nil)
   • #EXT-X-MAP (якщо p.Map != nil)
   • #EXT-X-PLAYLIST-TYPE: EVENT/VOD
   • #EXT-X-MEDIA-SEQUENCE, #EXT-X-TARGETDURATION
   • #EXT-X-START, #EXT-X-DISCONTINUITY-SEQUENCE, #EXT-X-I-FRAMES-ONLY
   • Widevine теги (#WV-*)

🔹 Ітерація по сегментах (останні winsize або всі для VOD):
   │
   ├── 🔹 SCTE-35 сповіщення:
   │   • SCTE35_67_2014: #EXT-SCTE35:CUE="...",ID="...",TIME=...
   │   • SCTE35_OATCLS: #EXT-OATCLS-SCTE35, #EXT-X-CUE-OUT, #EXT-X-CUE-OUT-CONT, #EXT-X-CUE-IN
   │
   ├── 🔹 Ключ шифрування (якщо seg.Key != p.Key):
   │   • #EXT-X-KEY:METHOD=...,URI="...",IV=...,KEYFORMAT=...
   │
   ├── 🔹 Розрив кодування:
   │   • #EXT-X-DISCONTINUITY
   │
   ├── 🔹 Ініціалізаційний сегмент (якщо seg.Map != nil && p.Map == nil):
   │   • #EXT-X-MAP:URI="...",BYTERANGE=...
   │
   ├── 🔹 Абсолютний час:
   │   • #EXT-X-PROGRAM-DATE-TIME:2024-01-15T10:30:00.123456789Z
   │
   ├── 🔹 Часткове читання:
   │   • #EXT-X-BYTERANGE:123456@789012
   │
   ├── 🔹 Кастомні теги сегмента:
   │   • #CCTV-EVENT:TYPE="motion",CONFIDENCE=0.95
   │
   ├── 🔹 Тривалість та назва:
   │   • #EXTINF:4.000,Motion detected
   │
   ├── 🔹 URI сегмента:
   │   • seg_001.ts?args
   │
   ▼
🔹 Завершення (якщо Closed=true):
   • #EXT-X-ENDLIST

🔹 Вихід: *bytes.Buffer з готовим M3U8-файлом
```

**🔑 Ключові особливості:**
- ✅ **Кешування**: `if p.buf.Len() > 0 { return &p.buf }` — уникнення повторної генерації
- ✅ **Кешування тривалостей**: `durationCache` — уникнення повторного форматування `float64`
- ✅ **Циклічна ітерація**: `(head + i) % capacity` — коректна робота з ковзним вікном
- ✅ **Умовне форматування**: `durationAsInt` — цілі числа для старих плеєрів, плаваючі для нових

**🔢 Приклад виводу:**
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#CCTV-CAMERA-ID:CAM-001
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="https://license.server/key?id=camera_001",KEYFORMAT="urn:uuid:eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1"
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-MEDIA-SEQUENCE:100
#EXT-X-TARGETDURATION:4
#EXTINF:4.000,
#CCTV-EVENT:TYPE="motion",CONFIDENCE=0.95
seg_100.ts
#EXTINF:4.000,
seg_101.ts
#EXT-X-DISCONTINUITY
#EXTINF:4.000,
seg_102.ts
```

---

## 🔐 Підтримка розширень

### 🔹 Шифрування: `SetDefaultKey` / `SetKey`

```go
func (p *MediaPlaylist) SetDefaultKey(method, uri, iv, keyformat, keyformatversions string) error {
    if keyformat != "" || keyformatversions != "" {
        version(&p.ver, 5)  // 🔹 KEYFORMAT вимагає версію ≥5
    }
    p.Key = &Key{method, uri, iv, keyformat, keyformatversions}
    return nil
}

func (p *MediaPlaylist) SetKey(method, uri, iv, keyformat, keyformatversions string) error {
    if p.count == 0 { return errors.New("playlist is empty") }
    if keyformat != "" || keyformatversions != "" {
        version(&p.ver, 5)
    }
    p.Segments[p.last()].Key = &Key{method, uri, iv, keyformat, keyformatversions}
    return nil
}
```

**🎯 Призначення**: Налаштувати шифрування для всього плейлиста (`SetDefaultKey`) або окремого сегмента (`SetKey`).

**🔢 Приклад для CCTV з DRM:**
```go
// 🔹 Глобальний ключ для всього плейлиста
playlist.SetDefaultKey("SAMPLE-AES", "https://license.server/key?id=camera_001", "", 
    "urn:uuid:eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1", "1")

// 🔹 Індивідуальний ключ для чутливого сегмента
playlist.SetKey("AES-128", "keys/segment_123.key", "0x1234567890abcdef", "", "")
```

---

### 🔹 SCTE-35 сповіщення: `SetSCTE35`

```go
func (p *MediaPlaylist) SetSCTE35(scte35 *SCTE) error {
    if p.count == 0 { return errors.New("playlist is empty") }
    p.Segments[p.last()].SCTE = scte35
    return nil
}
```

**🔄 Генерація різних синтаксисів:**

```go
// 🔹 SCTE35_67_2014 (стандартний)
#EXT-SCTE35:CUE="/DAlAAAAAAAAAP/wFAUAAAABf+/+ANgNkv4AFJlwAAEBAQAA5xULLA==",ID="123",TIME=123.12

// 🔹 SCTE35_OATCLS (поширений не-стандартний)
#EXT-OATCLS-SCTE35:/DAlAAAAAAAAAP/wFAUAAAABf+/+ANgNkv4AFJlwAAEBAQAA5xULLA==
#EXT-X-CUE-OUT:15.0
#EXT-X-CUE-OUT-CONT:ElapsedTime=8.844,Duration=15.0,SCTE35=/DAl...
#EXT-X-CUE-IN
```

**🎯 Призначення**: Інтеграція з **кабельним ТВ/аналітикою** через стандартизовані сповіщення про події.

---

### 🔹 Кастомні теги: `SetCustomTag` / `SetCustomSegmentTag`

```go
func (p *MediaPlaylist) SetCustomTag(tag CustomTag) {
    if p.Custom == nil { p.Custom = make(map[string]CustomTag) }
    p.Custom[tag.TagName()] = tag  // 🔹 Плейлист-тег
}

func (p *MediaPlaylist) SetCustomSegmentTag(tag CustomTag) error {
    if p.count == 0 { return errors.New("playlist is empty") }
    last := p.Segments[p.last()]
    if last.Custom == nil { last.Custom = make(map[string]CustomTag) }
    last.Custom[tag.TagName()] = tag  // 🔹 Сегмент-тег
    return nil
}
```

**🔄 Потік генерації кастомних тегів:**
```
🔹 Плейлист-теги: ітерація по p.Custom → v.Encode()
🔹 Сегмент-теги: ітерація по seg.Custom → v.Encode()
🔹 Якщо Encode() повертає nil → тег не додається у вивід
```

**🎯 Призначення**: Дозволити **розширення формату** без зміни ядра бібліотеки.

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Скидання кешу при кожній модифікації

```go
// 🔹 У багатьох методах:
p.buf.Reset()  // ← Скидання кешу після кожної зміни
```

**🎯 Ризик**: При частому додаванні сегментів у live-плейлист постійне скидання кешу може знизити продуктивність.

**✅ Рішення**: Додати опціональний режим "відкладеної генерації":
```go
func (p *MediaPlaylist) SetAutoResetCache(auto bool) {
    p.autoResetCache = auto  // 🔹 За замовчуванням true для сумісності
}

func (p *MediaPlaylist) AppendSegment(seg *MediaSegment) error {
    // ... логіка додавання ...
    if p.autoResetCache {
        p.buf.Reset()
    }
    return nil
}

func (p *MediaPlaylist) ForceResetCache() {
    p.buf.Reset()  // 🔹 Явне скидання, коли потрібно
}
```

---

### 🔴 Проблема 2: Відсутність валідації вхідних даних у `Set*` методах

```go
// 🔹 Користувач може встановити невалідні значення:
playlist.SetKey("INVALID_METHOD", "", "", "", "")  // ❌ Без валідації
```

**✅ Рішення**: Додати конструктори з валідацією:
```go
func NewKey(method, uri string) (*Key, error) {
    validMethods := map[string]bool{"AES-128": true, "SAMPLE-AES": true, "NONE": true}
    if !validMethods[method] {
        return nil, fmt.Errorf("invalid encryption method: %s", method)
    }
    if method != "NONE" && uri == "" {
        return nil, fmt.Errorf("URI required for method %s", method)
    }
    return &Key{Method: method, URI: uri}, nil
}

// 🔹 Використання:
key, err := NewKey("AES-128", "keys/key.bin")
if err != nil { return err }
playlist.SetDefaultKey(key.Method, key.URI, "", "", "")
```

---

### 🟡 Проблема 3: Складність логіки циклічного буфера

```go
// 🔹 last() метод:
func (p *MediaPlaylist) last() uint {
    if p.tail == 0 {
        return p.capacity - 1
    }
    return p.tail - 1
}
```

**✅ Рішення**: Додати методи-хелпери з документацією:
```go
// LastSegmentIndex повертає індекс останнього доданого сегмента у циклічному буфері
func (p *MediaPlaylist) LastSegmentIndex() uint {
    if p.count == 0 {
        return 0  // 🔹 Немає сегментів
    }
    return (p.tail + p.capacity - 1) % p.capacity
}

// IsActiveSegment перевіряє, чи сегмент входить у активне вікно
func (p *MediaPlaylist) IsActiveSegment(index uint) bool {
    if p.winsize == 0 { return true }  // 🔹 VOD: всі сегменти активні
    if p.count < p.winsize { return index < p.count }
    
    start := (p.tail + p.capacity - p.winsize) % p.capacity
    end := p.tail
    if start <= end {
        return index >= start && index < end
    }
    return index >= start || index < end
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Генерація live-плейлиста для камери

```go
func GenerateLivePlaylist(cameraID string, segments []Segment) (*m3u8.MediaPlaylist, error) {
    // 🔹 Створення плейлиста: winsize=10 (40 секунд буфера), capacity=20
    playlist, err := m3u8.NewMediaPlaylist(10, 20)
    if err != nil {
        return nil, err
    }
    
    // 🔹 Базові налаштування
    playlist.SetVersion(7)
    playlist.SetTargetDuration(4)
    playlist.SetPlaylistType("event")  // 🔹 Live-подія
    playlist.SetIndependentSegments(true)
    
    // 🔹 Шифрування (опціонально)
    playlist.SetDefaultKey("SAMPLE-AES", 
        fmt.Sprintf("https://license.server/key?id=%s", cameraID), 
        "", "urn:uuid:eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1", "1")
    
    // 🔹 Реєстрація кастомних тегів
    playlist.WithCustomDecoders([]m3u8.CustomDecoder{
        &CameraIDTag{ID: cameraID},
        &EventTag{},
    })
    
    // 🔹 Додавання плейлист-тегу
    playlist.SetCustomTag(&CameraIDTag{ID: cameraID})
    
    // 🔹 Додавання сегментів
    for _, seg := range segments {
        // 🔹 Створення сегмента
        mediaSeg := &m3u8.MediaSegment{
            URI:      seg.Filename,
            Duration: seg.Duration,
            Title:    seg.Title,
        }
        
        // 🔹 Додавання кастомного тегу події, якщо є
        if seg.Event != nil {
            mediaSeg.Custom = map[string]m3u8.CustomTag{
                "#CCTV-EVENT:": &EventTag{
                    Type: seg.Event.Type,
                    Confidence: seg.Event.Confidence,
                    Timestamp: seg.Event.Timestamp,
                },
            }
        }
        
        // 🔹 Додавання у плейлист
        if err := playlist.AppendSegment(mediaSeg); err != nil {
            return nil, err
        }
    }
    
    return playlist, nil
}

// 🔹 Використання:
playlist, err := GenerateLivePlaylist("CAM-001", segments)
if err != nil {
    log.Printf("❌ Generate failed: %v", err)
} else {
    // 🔹 Запис плейлиста на диск
    os.WriteFile("camera_001/playlist.m3u8", []byte(playlist.String()), 0644)
    log.Printf("✅ Live playlist generated with %d segments", playlist.Count())
}
```

---

### 🔹 Приклад 2: Адаптивний master-плейлист з аудіо-доріжками

```go
func GenerateAdaptiveMaster(cameraID string, variants []StreamVariant) (*m3u8.MasterPlaylist, error) {
    p := m3u8.NewMasterPlaylist()
    p.SetVersion(7)
    p.SetIndependentSegments(true)
    
    // 🔹 Додавання кастомного тегу камери
    p.SetCustomTag(&CameraIDTag{ID: cameraID})
    
    for _, v := range variants {
        // 🔹 Створення варіанту
        variant := &m3u8.Variant{
            URI: v.URI,
            VariantParams: m3u8.VariantParams{
                Bandwidth:  v.Bandwidth,
                Codecs:     v.Codecs,
                Resolution: v.Resolution,
                Audio:      "audio-stereo",  // 🔹 Посилання на групу аудіо
                FrameRate:  v.FrameRate,
            },
        }
        p.Append(v.URI, nil, variant.VariantParams)
    }
    
    // 🔹 Додавання аудіо-доріжки
    p.AppendAlternate("audio-stereo", "AUDIO", "Українська", "audio/uk/index.m3u8", true, true)
    p.AppendAlternate("audio-stereo", "AUDIO", "English", "audio/en/index.m3u8", false, false)
    
    return p, nil
}

// 🔹 Використання:
variants := []StreamVariant{
    {URI: "720p/index.m3u8", Bandwidth: 2500000, Resolution: "1280x720", Codecs: "avc1.64001f,mp4a.40.2", FrameRate: 30},
    {URI: "480p/index.m3u8", Bandwidth: 1200000, Resolution: "854x480", Codecs: "avc1.64001f,mp4a.40.2", FrameRate: 30},
}

master, err := GenerateAdaptiveMaster("CAM-001", variants)
if err != nil {
    log.Printf("❌ Master generate failed: %v", err)
} else {
    os.WriteFile("master.m3u8", []byte(master.String()), 0644)
    log.Printf("✅ Master playlist generated with %d variants", len(master.Variants))
}
```

---

### 🔹 Приклад 3: Оновлення live-плейлиста в реальному часі

```go
type LivePlaylistManager struct {
    playlist *m3u8.MediaPlaylist
    mu       sync.Mutex
}

func NewLivePlaylistManager(cameraID string, windowSize uint) (*LivePlaylistManager, error) {
    p, err := m3u8.NewMediaPlaylist(windowSize, windowSize*2)
    if err != nil {
        return nil, err
    }
    
    p.SetVersion(7)
    p.SetTargetDuration(4)
    p.SetPlaylistType("event")
    p.SetIndependentSegments(true)
    
    // 🔹 Реєстрація кастомних тегів
    p.WithCustomDecoders([]m3u8.CustomDecoder{&CameraIDTag{ID: cameraID}, &EventTag{}})
    p.SetCustomTag(&CameraIDTag{ID: cameraID})
    
    return &LivePlaylistManager{playlist: p}, nil
}

func (m *LivePlaylistManager) AddSegment(uri string, duration float64, event *Event) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // 🔹 Створення сегмента
    seg := &m3u8.MediaSegment{
        URI:      uri,
        Duration: duration,
    }
    
    // 🔹 Додавання кастомного тегу події
    if event != nil {
        seg.Custom = map[string]m3u8.CustomTag{
            "#CCTV-EVENT:": &EventTag{
                Type: event.Type,
                Confidence: event.Confidence,
                Timestamp: event.Timestamp,
            },
        }
    }
    
    // 🔹 Додавання у плейлист (автоматичне видалення найстарішого при переповненні)
    if err := m.playlist.AppendSegment(seg); err != nil {
        return err
    }
    
    // 🔹 Генерація оновленого плейлиста
    output := m.playlist.Encode().String()
    
    // 🔹 Запис на диск (або відправка у CDN)
    return os.WriteFile("live/playlist.m3u8", []byte(output), 0644)
}

// 🔹 Використання у конвеєрі:
manager, _ := NewLivePlaylistManager("CAM-001", 10)
go func() {
    for segment := range segmentChannel {
        if err := manager.AddSegment(segment.URI, segment.Duration, segment.Event); err != nil {
            log.Printf("❌ Failed to add segment: %v", err)
        }
    }
}()
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні плейлистів:
    • Використовуйте winsize > 0 для live, winsize = 0 для VOD
    • Встановлюйте capacity >= 2×winsize для уникнення частих розширень масиву
    • Реєструйте кастомні теги через WithCustomDecoders() перед додаванням

[ ] Для шифрування:
    • Використовуйте конструктор NewKey() з валідацією методу та URI
    • Для Widevine: заповнюйте Keyformat та Keyformatversions
    • Для fMP4: додавайте Map з URI init-файлу через SetDefaultMap()

[ ] Для live-стрімінгу:
    • Налаштовуйте TargetDuration на основі максимальної тривалості сегмента
    • Використовуйте Slide() замість Append()+Remove() для атомарного оновлення
    • Очищайте кеш через ResetCache() тільки при необхідності

[ ] Для кастомних тегів:
    • Реалізуйте всі методи інтерфейсів: TagName, Decode/Encode, SegmentTag, String
    • Використовуйте унікальні префікси: #CCTV-*, #MYAPP-* для уникнення конфліктів
    • Валідуйте атрибути у Decode() з чіткими помилками

[ ] Для безпеки:
    • Валідуйте вхідні URI сегментів (заборона `file://`, `../`)
    • Обмежуйте довжину кастомних атрибутів (напр., max 255 символів)
    • Не довіряйте кастомним тегам з ненадійних джерел

[ ] Для дебагу:
    • Логувайте закодований плейлист: log.Printf("📝 Output:\n%s", playlist.String())
    • Перевіряйте версію плейлиста: log.Printf("🔢 Version: %d", playlist.Version())
    • Тестуйте з різними розмірами вікна: 3, 10, 100 сегментів
```

---

## 🎯 Висновок

> **Цей модуль — "принтер" бібліотеки m3u8**, який забезпечує:
> • ✅ Типобезпечну генерацію валідних M3U8-файлів відповідно до специфікації HLS
> • ✅ Автоматичне підвищення версії при використанні нових функцій
> • ✅ Ефективне керування пам'яттю через циклічні буфери для live-плейлистів
> • ✅ Гнучке розширення через кастомні теги без зміни ядра
> • ✅ Підтримку шифрування, SCTE-35, Widevine та інших розширень

Для вашого **CCTV HLS Processor** це означає:
- 🎯 Надійна генерація live/VOD плейлистів для камер з правильними метаданими
- 📡 Ефективне оновлення live-плейлистів у реальному часі з ковзним вікном
- 🔐 Безпечне керування шифруванням та DRM-метаданими
- 🔄 Легке розширення через кастомні теги для маркування подій, камер, аналітики
- 🛡️ Валідація вхідних даних та чіткі інтерфейси запобігають помилкам

Потребуєте допомоги з оптимізацією генерації великих плейлистів або з реалізацією специфічних кастомних тегів для ваших сценаріїв? Напишіть — покажу готовий код для вашого випадку! 🚀🎬