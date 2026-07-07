# 📦 Глибокий розбір: `sdp.Parse` — Парсинг SDP для RTSP/медіа-потоків

Цей файл — **реалізація парсера SDP (Session Description Protocol)** для витягування метаданих медіа-потоків (кодек, частота кадрів, параметри SPS/PPS для H.264/H.265, конфігурація AAC тощо). Він використовується у RTSP клієнтах для налаштування демуксингу перед початком стрімінгу.

---

## 🗺️ Архітектурна схема sdp пакету

```
┌────────────────────────────────────────┐
│ 📦 sdp.Parse — SDP Metadata Extractor │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові структури:                  │
│  • Session — базова інформація сесії   │
│  • Media — параметри аудіо/відео потоку│
│                                         │
│  🔄 Потік парсингу:                     │
│  SDP текст → рядок за рядком → атрибути│
│  → заповнення Media struct             │
│                                         │
│  📡 Підтримувані кодеки:                │
│  • Відео: H.264, H.265/HEVC, JPEG      │
│  • Аудіо: AAC, Opus, PCMA/PCMU, PCM    │
│                                         │
│  🧬 Ключові поля:                       │
│  • SpropSPS/PPS/VPS — параметр-сети    │
│  • Config — MPEG4AudioConfig для AAC   │
│  • FPS, TimeScale, ChannelCount        │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Структури даних

### Session — базова інформація:
```go
type Session struct {
    Uri string  // URL сесії (з атрибута "u=")
}
```

### Media — детальні параметри потоку:
```go
type Media struct {
    AVType             string            // "audio" або "video"
    Type               av.CodecType      // тип кодека (av.H264, av.AAC тощо)
    FPS                int               // частота кадрів (з "x-framerate")
    TimeScale          int               // частота дискретизації (з rtpmap)
    Control            string            // URL для SETUP (відносний/абсолютний)
    Rtpmap             int               // RTP payload type
    ChannelCount       int               // кількість аудіо каналів
    Config             []byte            // MPEG4AudioConfig для AAC
    SpropParameterSets [][]byte          // SPS+PPS для H.264 (base64)
    SpropVPS           []byte            // VPS для H.265
    SpropSPS           []byte            // SPS для H.265
    SpropPPS           []byte            // PPS для H.265
    PayloadType        int               // RTP payload type (0=PCMU, 8=PCMA...)
    SizeLength         int               // для AAC: розмір заголовка фрейму
    IndexLength        int               // для AAC: розмір індексу фрейму
}
```

---

## 🔍 Покроковий аналіз функції `Parse()`

### 🔧 Основний цикл парсингу:

```go
func Parse(content string) (sess Session, medias []Media) {
    var media *Media  // поточний медіа-потік

    for _, line := range strings.Split(content, "\n") {
        line = strings.TrimSpace(line)
        
        // 🐛 Workaround для камер з пробілом у "x-framerate"
        if strings.Contains(line, "x-framerate") {
            line = strings.Replace(line, " ", "", -1)  // "a=x-framerate: 25" → "a=x-framerate:25"
        }
        
        typeval := strings.SplitN(line, "=", 2)  // розділення "a=value" → ["a", "value"]
        if len(typeval) != 2 { continue }
        
        fields := strings.SplitN(typeval[1], " ", 2)  // додатковий поділ за пробілом
        
        switch typeval[0] {
        case "m":  // media description
            // ... обробка "m=video 0 RTP/AVP 96" ...
            
        case "u":  // URI сесії
            sess.Uri = typeval[1]
            
        case "a":  // атрибути
            if media != nil {
                // ... парсинг атрибутів: control, rtpmap, x-framerate, sprop-* тощо ...
            }
        }
    }
    return
}
```

### 🔧 Парсинг атрибутів "a=":

```go
// Три рівні парсингу одного рядка:
// 1. За ":" — для control, rtpmap, x-framerate
keyval := strings.SplitN(field, ":", 2)  // "control:track1" → ["control", "track1"]

// 2. За "/" — для rtpmap кодеків
keyval = strings.Split(field, "/")  // "H264/90000" → ["H264", "90000"]

// 3. За ";" — для параметрів fmtp/config
keyval = strings.Split(field, ";")  // "config=1234;sizelength=13" → ["config=1234", "sizelength=13"]
```

### 🔧 Декодування параметрів:

```go
switch key {
case "config":
    // AAC: hex-encoded MPEG4AudioConfig
    media.Config, _ = hex.DecodeString(val)  // ⚠️ помилка ігнорується!
    
case "sprop-sps":
    // H.264/H.265: base64-encoded SPS
    val, err := base64.StdEncoding.DecodeString(val)
    if err == nil {
        media.SpropSPS = val
    } else {
        log.Println("SDP: decode sps error", err)  // ⚠️ тільки лог, не помилка!
    }
    
case "sprop-parameter-sets":
    // H.264: кілька SPS/PPS через кому
    fields := strings.Split(val, ",")
    for _, field := range fields {
        if field == "" { continue }
        val, _ := base64.StdEncoding.DecodeString(field)  // ⚠️ помилка ігнорується!
        media.SpropParameterSets = append(media.SpropParameterSets, val)
    }
}
```

---

## 🚨 Критичні проблеми у вихідному коді

### ❌ 1. Масове ігнорування помилок

```go
media.PayloadType, _ = strconv.Atoi(mfields[2])  // ⚠️ якщо не число → 0
media.FPS, _ = strconv.Atoi(val)                  // ⚠️ якщо не число → 0
media.Config, _ = hex.DecodeString(val)           // ⚠️ якщо невалидний hex → порожній слайс
val, _ := base64.StdEncoding.DecodeString(field)  // ⚠️ якщо невалидний base64 → порожній слайс
```

**Наслідки**:
- Неправильний `PayloadType` → неможливо визначити кодек
- `FPS=0` → неправильний розрахунок тривалості кадрів
- Порожній `Config`/`SPS` → неможливо ініціалізувати декодер

**✅ Виправлення**: Повертати помилку або валідувати значення:
```go
if fps, err := strconv.Atoi(val); err == nil && fps > 0 {
    media.FPS = fps
} else {
    return sess, medias, fmt.Errorf("invalid fps: %q", val)
}
```

---

### ❌ 2. Конвульсивний парсинг атрибутів

```go
// Три незалежні Split для одного рядка:
keyval := strings.SplitN(field, ":", 2)  // 1-й прохід
keyval = strings.Split(field, "/")       // 2-й прохід (перезаписує!)
keyval = strings.Split(field, ";")       // 3-й прохід (знову перезаписує!)
```

**Проблема**: Якщо атрибут містить кілька роздільників (напр. `rtpmap:96 H264/90000`), логіка може зламатися.

**✅ Виправлення**: Використовувати регулярні вирази або структурований парсер:
```go
// Приклад для rtpmap: "96 H264/90000"
func parseRtpmap(val string) (pt int, codec string, clock int, err error) {
    parts := strings.Fields(val)  // ["96", "H264/90000"]
    if len(parts) < 2 { return 0, "", 0, fmt.Errorf("invalid rtpmap") }
    
    pt, err = strconv.Atoi(parts[0])
    if err != nil { return }
    
    codecParts := strings.Split(parts[1], "/")  // ["H264", "90000"]
    if len(codecParts) < 2 { return }
    
    codec = codecParts[0]
    clock, err = strconv.Atoi(codecParts[1])
    return
}
```

---

### ❌ 3. Відсутність валідації значень

```go
// Немає перевірок:
media.FPS = 999999  // нереалістичне значення
media.TimeScale = -1  // від'ємна частота
media.ChannelCount = 100  // нереально для аудіо
```

**✅ Виправлення**: Додати діапазонні перевірки:
```go
func validateMedia(m *Media) error {
    if m.FPS < 0 || m.FPS > 240 {
        return fmt.Errorf("invalid FPS: %d", m.FPS)
    }
    if m.TimeScale < 8000 || m.TimeScale > 90000 {
        return fmt.Errorf("invalid TimeScale: %d", m.TimeScale)
    }
    if m.ChannelCount < 1 || m.ChannelCount > 8 {
        return fmt.Errorf("invalid ChannelCount: %d", m.ChannelCount)
    }
    return nil
}
```

---

### ❌ 4. Необроблені SDP атрибути

```
Відсутня підтримка критичних полів:
• "b=" — bandwidth (AS, CT, RS, RR)
• "c=" — connection info (IP address, TTL)
• "k=" — encryption key
• "z=" — time zone adjustments
• "fmtp" — format-specific parameters (часто містить SPS/PPS для H.264!)
```

**Наслідки**: Неможливо обробити повні SDP від деяких камер (напр. Axis, Bosch).

**✅ Виправлення**: Додати обробку `fmtp` для H.264:
```go
case "fmtp":
    // Приклад: "fmtp:96 profile-level-id=42001e;sprop-parameter-sets=Z0IAHqtA...,aM48gA=="
    if media.Type == av.H264 {
        parts := strings.SplitN(val, " ", 2)
        if len(parts) == 2 {
            params := strings.Split(parts[1], ";")
            for _, param := range params {
                kv := strings.SplitN(param, "=", 2)
                if len(kv) == 2 {
                    switch strings.TrimSpace(kv[0]) {
                    case "sprop-parameter-sets":
                        // Парсинг SPS/PPS з fmtp
                        sets := strings.Split(kv[1], ",")
                        for _, s := range sets {
                            if dec, err := base64.StdEncoding.DecodeString(s); err == nil {
                                media.SpropParameterSets = append(media.SpropParameterSets, dec)
                            }
                        }
                    }
                }
            }
        }
    }
```

---

### ❌ 5. Відносні/абсолютні URL у `control`

```go
case "control":
    media.Control = val  // ⚠️ не нормалізує відносні шляхи!
```

**Проблема**: `control:trackID=1` vs `control:rtsp://camera/stream/trackID=1`

**✅ Виправлення**: Нормалізація через `url.Parse`:
```go
import "net/url"

case "control":
    if strings.HasPrefix(val, "rtsp://") || strings.HasPrefix(val, "rtsps://") {
        media.Control = val  // абсолютний URL
    } else if sess.Uri != "" {
        // Відносний шлях: об'єднати з базовим URI
        base, err := url.Parse(sess.Uri)
        if err == nil {
            rel, err := url.Parse(val)
            if err == nil {
                media.Control = base.ResolveReference(rel).String()
            }
        }
    } else {
        media.Control = val  // fallback
    }
```

---

## ✅ Production-ready версія парсера

### 🔧 Рефакторинг з обробкою помилок:

```go
type ParseError struct {
    Line   int
    Field  string
    Reason string
}

func (e ParseError) Error() string {
    return fmt.Sprintf("SDP parse error at line %d, field %q: %s", e.Line, e.Field, e.Reason)
}

func ParseWithValidation(content string) (Session, []Media, error) {
    var sess Session
    var medias []Media
    var media *Media
    
    for lineNum, line := range strings.Split(content, "\n") {
        line = strings.TrimSpace(line)
        if line == "" { continue }
        
        parts := strings.SplitN(line, "=", 2)
        if len(parts) != 2 {
            continue  // пропускаємо невалидні рядки
        }
        
        key, val := parts[0], parts[1]
        
        switch key {
        case "m":
            m, err := parseMediaLine(val)
            if err != nil {
                return sess, nil, ParseError{lineNum, "m", err.Error()}
            }
            medias = append(medias, m)
            media = &medias[len(medias)-1]
            
        case "a":
            if media == nil { continue }
            if err := parseAttribute(media, val); err != nil {
                return sess, nil, ParseError{lineNum, "a:" + val, err.Error()}
            }
            
        case "u":
            sess.Uri = val
        }
    }
    
    // Фінальна валідація
    for i := range medias {
        if err := validateMedia(&medias[i]); err != nil {
            return sess, nil, fmt.Errorf("invalid media %d: %w", i, err)
        }
    }
    
    return sess, medias, nil
}
```

### 🔧 Оптимізований парсинг атрибутів:

```go
func parseAttribute(m *Media, attr string) error {
    if strings.HasPrefix(attr, "control:") {
        m.Control = strings.TrimPrefix(attr, "control:")
        return nil
    }
    
    if strings.HasPrefix(attr, "rtpmap:") {
        return parseRtpmap(m, strings.TrimPrefix(attr, "rtpmap:"))
    }
    
    if strings.HasPrefix(attr, "fmtp:") {
        return parseFmtp(m, strings.TrimPrefix(attr, "fmtp:"))
    }
    
    if strings.HasPrefix(attr, "x-framerate:") {
        fps, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(attr, "x-framerate:")))
        if err != nil {
            return fmt.Errorf("invalid fps: %w", err)
        }
        m.FPS = fps
        return nil
    }
    
    // Обробка параметрів через ";"
    if strings.Contains(attr, ";") {
        return parseParams(m, attr)
    }
    
    return nil  // невідомий атрибут — ігноруємо
}
```

---

## 🎬 Інтеграція у CCTV Pipeline: Авто-конфігурація каналу

### 🔧 Приклад: Витягування параметрів для HLS muxer

```go
// ConfigureChannelFromSDP — налаштування каналу на основі SDP
func ConfigureChannelFromSDP(sdpContent string) (*ChannelConfig, error) {
    sess, medias, err := ParseWithValidation(sdpContent)
    if err != nil {
        return nil, fmt.Errorf("parse SDP: %w", err)
    }
    
    config := &ChannelConfig{
        SessionURI: sess.Uri,
        Streams:    make([]StreamConfig, 0, len(medias)),
    }
    
    for _, m := range medias {
        sc := StreamConfig{
            Type:         m.AVType,
            Codec:        m.Type.String(),
            PayloadType:  m.PayloadType,
            TimeScale:    m.TimeScale,
            ControlURL:   m.Control,
        }
        
        switch m.Type {
        case av.H264, av.H265:
            sc.Video = &VideoConfig{
                FPS:        m.FPS,
                SPS:        firstNonEmpty(m.SpropSPS, extractSPS(m.SpropParameterSets)),
                PPS:        firstNonEmpty(m.SpropPPS, extractPPS(m.SpropParameterSets)),
                VPS:        m.SpropVPS,  // тільки для H.265
                Width:      estimateResolution(m.SpropSPS),  // парсинг з SPS
                Height:     estimateResolution(m.SpropSPS),
            }
            
        case av.AAC:
            sc.Audio = &AudioConfig{
                SampleRate:   m.TimeScale,
                Channels:     m.ChannelCount,
                AudioConfig:  m.Config,  // MPEG4AudioConfig bytes
                SizeLength:   m.SizeLength,
                IndexLength:  m.IndexLength,
            }
        }
        
        config.Streams = append(config.Streams, sc)
    }
    
    return config, nil
}

// Допоміжні функції
func firstNonEmpty(slices ...[]byte) []byte {
    for _, s := range slices {
        if len(s) > 0 {
            return s
        }
    }
    return nil
}

func extractSPS(sets [][]byte) []byte {
    for _, s := range sets {
        if len(s) > 0 && (s[0]&0x1f) == 7 {  // H.264 SPS NALU type
            return s
        }
    }
    return nil
}
```

### ✅ Ваш use-case: валідація перед SETUP

```go
// ValidateSDPBeforeSetup — перевірка SDP перед відправкою SETUP
func ValidateSDPBeforeSetup(sdpContent string) error {
    _, medias, err := ParseWithValidation(sdpContent)
    if err != nil {
        return fmt.Errorf("invalid SDP: %w", err)
    }
    
    var hasVideo, hasAudio bool
    for _, m := range medias {
        if m.AVType == "video" && m.Type != av.UNKNOWN {
            hasVideo = true
            if m.Type == av.H264 && len(firstNonEmpty(m.SpropSPS, extractSPS(m.SpropParameterSets))) == 0 {
                return fmt.Errorf("H.264 video without SPS in SDP")
            }
        }
        if m.AVType == "audio" && m.Type != av.UNKNOWN {
            hasAudio = true
        }
    }
    
    if !hasVideo && !hasAudio {
        return fmt.Errorf("SDP contains no supported media streams")
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **`strconv.Atoi` ігнорує помилки** | `FPS=0`, `TimeScale=0` → помилки таймінгів | Валідуйте кожне числове поле, повертайте помилку |
| **`base64.DecodeString` не перевіряється** | Порожні `SPS/PPS` → неможливо декодувати відео | Логуйте помилки та повертайте `error` для критичних полів |
| **Відносний `control` не нормалізується** | Помилка `404` при SETUP | Використовуйте `url.Parse` + `ResolveReference` |
| **`fmtp` атрибут ігнорується** | Відсутні `SPS/PPS` для H.264 | Додайте парсинг `fmtp` з підтримкою `sprop-parameter-sets` |
| **Невалідні значення не відкидаються** | `FPS=999999` → переповнення буферів | Додайте `validateMedia()` з діапазонними перевірками |

---

## ⚡ Оптимізації для high-throughput парсингу

### 1. Кешування результатів парсингу:
```go
var sdpCache = sync.Map{}  // map[string]parsedResult

func ParseCached(content string) (Session, []Media, error) {
    hash := fnv1a(content)  // швидкий хеш
    if cached, ok := sdpCache.Load(hash); ok {
        return cached.(parsedResult).sess, cached.(parsedResult).medias, nil
    }
    
    sess, medias, err := ParseWithValidation(content)
    if err == nil {
        sdpCache.Store(hash, parsedResult{sess, medias})
    }
    return sess, medias, err
}
```

### 2. Попередня компіляція регулярних виразів:
```go
var (
    rtpmapRe  = regexp.MustCompile(`^(\d+)\s+([A-Z0-9-]+)/(\d+)(?:/(\d+))?$`)
    fmtpRe    = regexp.MustCompile(`^(\d+)\s+(.+)$`)
    paramRe   = regexp.MustCompile(`([a-z0-9-]+)=([^;]+)`)
)

func parseRtpmapOptimized(val string) (pt int, codec string, clock int, channels int, err error) {
    m := rtpmapRe.FindStringSubmatch(val)
    if m == nil { return 0, "", 0, 0, fmt.Errorf("invalid rtpmap") }
    
    pt, _ = strconv.Atoi(m[1])
    codec = m[2]
    clock, _ = strconv.Atoi(m[3])
    if len(m) > 4 && m[4] != "" {
        channels, _ = strconv.Atoi(m[4])
    }
    return
}
```

### 3. Моніторинг продуктивності парсингу:
```go
type SDPMetrics struct {
    ParseLatency prometheus.HistogramVec
    ParseErrors  prometheus.CounterVec
    FieldsParsed prometheus.CounterVec
}

func (m *SDPMetrics) RecordParse(duration time.Duration, fieldCount int, err error) {
    if err != nil {
        m.ParseErrors.Inc()
        return
    }
    m.ParseLatency.Observe(duration.Seconds())
    m.FieldsParsed.Add(float64(fieldCount))
}
```

---

## 📋 Чек-лист production-готовності SDP парсера

```go
// ✅ 1. Обробка всіх помилок декодування (base64, hex, strconv)
// ✅ 2. Валідація діапазонів для числових полів (FPS, TimeScale, Channels)
// ✅ 3. Нормалізація відносних URL у control
// ✅ 4. Підтримка fmtp атрибуту для H.264 SPS/PPS
// ✅ 5. Логування помилок з контекстом (номер рядка, поле)
// ✅ 6. Тестування на реальних SDP від різних камер (Hikvision, Dahua, Axis)
// ✅ 7. Кешування результатів для уникнення повторного парсингу
// ✅ 8. Метрики для моніторингу помилок парсингу
```

---

## 🔗 Корисні посилання

- 📄 [SDP Specification (RFC 4566)](https://datatracker.ietf.org/doc/html/rfc4566) — офіційний стандарт
- 📄 [RTP Payload Format for H.264 (RFC 6184)](https://datatracker.ietf.org/doc/html/rfc6184) — sprop-parameter-sets
- 📄 [MPEG-4 AudioSpecificConfig](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — hex encoding
- 💻 [Go net/url Package](https://pkg.go.dev/net/url) — нормалізація відносних URL
- 🧪 [Go regexp Best Practices](https://go.dev/doc/effective_go#regexp) — оптимізація парсингу

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Ніколи не ігноруйте помилки декодування** — порожні `SPS/PPS` зламають декодер.
> 2. **Валідуйте числові поля** — `FPS=0` призведе до ділення на нуль при розрахунку тривалості.
> 3. **Нормалізуйте `control` URL** — відносні шляхи часто використовуються у RTSP.
> 4. **Підтримуйте `fmtp`** — багато камер надсилають `SPS/PPS` саме там.
> 5. **Тестуйте на різних камерах** — SDP формат може відрізнятися між виробниками.

Потрібен приклад повного парсингу **H.264 SPS** для витягування роздільної здатності та профілю без використання зовнішніх бібліотек? Готовий допомогти! 🚀