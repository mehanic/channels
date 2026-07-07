# 🧪 Глибокий розбір: `sdp.TestParse` — Тест парсингу SDP

Цей файл — **юніт-тест для SDP парсера**, що перевіряє обробку реального прикладу з камери (LIVE555). Однак тест має **критичні проблеми**: виклик неіснуючої функції `Decode()`, відсутність валідації результатів, та ігнорування ключових полів.

---

## 🔍 Аналіз вхідного SDP

```sdp
# 🎬 Відео потік (track1)
m=video 0 RTP/AVP 96                    # порт=0 (динамічний), протокол=AVP, payload=96
a=rtpmap:96 H264/90000                  # кодек=H.264, clock=90kHz
a=fmtp:96 profile-level-id=420029;      # Baseline profile, level 4.1
           packetization-mode=1;        # single-NALU mode
           sprop-parameter-sets=Z00AHpWoKA9k,aO48gA==  # SPS+PPS base64
a=x-dimensions: 720, 480                # роздільна здатність
a=x-framerate: 15                       # FPS
a=control:track1                        # відносний URL для SETUP

# 🔊 Аудіо потік 1 (AAC)
m=audio 0 RTP/AVP 96
a=rtpmap:96 MPEG4-GENERIC/16000/2      # AAC, 16kHz, stereo
a=fmtp:96 streamtype=5;profile-level-id=1;mode=AAC-hbr;
           sizelength=13;indexlength=3;indexdeltalength=3;
           config=1408                  # MPEG4AudioConfig (hex)
a=control:track2

# 🔊 Аудіо потік 2 (PCMU fallback)
m=audio 0 RTP/AVP 0                     # payload 0 = PCMU
a=rtpmap:0 PCMU/8000                    # G.711 μ-law, 8kHz
a=control:rtsp://109.195.127.207:554/... # абсолютний URL
```

---

## 🚨 Критичні проблеми у тесті

### ❌ 1. Виклик неіснуючої функції `Decode()`

```go
infos := Decode(`...`)  // ← ФУНКЦІЯ НЕ ІСНУЄ!
```

У попередньому коді пакету `sdp` була функція `Parse()`, а не `Decode()`.

**✅ Виправлення**:
```go
sess, medias, err := Parse(sdpContent)
if err != nil {
    t.Fatalf("Parse failed: %v", err)
}
```

---

### ❌ 2. Відсутність будь-яких перевірок

```go
t.Logf("%v", infos)  // ← Просто лог, без assert!
```

**Проблема**: Тест завжди "проходить", навіть якщо парсер повертає порожні результати.

**✅ Виправлення** — повна валідація:

```go
func TestParse(t *testing.T) {
    sess, medias, err := Parse(sdpContent)
    if err != nil {
        t.Fatalf("Parse failed: %v", err)
    }
    
    // ✅ Перевірка сесії
    if sess.Uri == "" {
        t.Error("expected non-empty session URI")
    }
    
    // ✅ Пошук відео потоку
    var video *Media
    for i := range medias {
        if medias[i].AVType == "video" {
            video = &medias[i]
            break
        }
    }
    if video == nil {
        t.Fatal("expected video stream not found")
    }
    
    // ✅ Валідація відео параметрів
    checks := []struct{
        name     string
        got      interface{}
        expected interface{}
    }{
        {"codec", video.Type, av.H264},
        {"payload", video.PayloadType, 96},
        {"clock", video.TimeScale, 90000},
        {"fps", video.FPS, 15},
        {"control", video.Control, "track1"},
    }
    
    for _, c := range checks {
        if c.got != c.expected {
            t.Errorf("%s: expected %v, got %v", c.name, c.expected, c.got)
        }
    }
    
    // ✅ Перевірка SPS/PPS
    if len(video.SpropParameterSets) != 2 {
        t.Errorf("expected 2 parameter sets, got %d", len(video.SpropParameterSets))
    }
    // SPS має починатися з 0x67 (NALU type 7)
    if len(video.SpropParameterSets[0]) == 0 || (video.SpropParameterSets[0][0] & 0x1f) != 7 {
        t.Error("first parameter set is not valid SPS")
    }
    
    // ✅ Пошук AAC аудіо потоку
    var aac *Media
    for i := range medias {
        if medias[i].AVType == "audio" && medias[i].Type == av.AAC {
            aac = &medias[i]
            break
        }
    }
    if aac == nil {
        t.Fatal("expected AAC audio stream not found")
    }
    
    // ✅ Валідація AAC параметрів
    if aac.TimeScale != 16000 {
        t.Errorf("AAC TimeScale: expected 16000, got %d", aac.TimeScale)
    }
    if aac.ChannelCount != 2 {
        t.Errorf("AAC channels: expected 2, got %d", aac.ChannelCount)
    }
    if hex.EncodeToString(aac.Config) != "1408" {
        t.Errorf("AAC config: expected '1408', got '%s'", hex.EncodeToString(aac.Config))
    }
    if aac.SizeLength != 13 || aac.IndexLength != 3 {
        t.Errorf("AAC framing: expected sizelength=13,indexlength=3, got %d,%d", 
            aac.SizeLength, aac.IndexLength)
    }
}
```

---

### ❌ 3. Парсер не обробляє ключові поля з тестового SDP

| Поле у тесті | Чи обробляє поточний `Parse()`? | Наслідки |
|--------------|--------------------------------|----------|
| `a=fmtp:96 ... sprop-parameter-sets=...` | ❌ Ні (тільки `a=...;sprop-*` без `fmtp:`) | SPS/PPS не витягуються |
| `a=x-dimensions: 720, 480` | ❌ Ні | Роздільна здатність втрачається |
| `a=fmtp:96 ... config=1408` | ✅ Так (частково) | Але без валідації hex |
| `b=AS:300` (bandwidth) | ❌ Ні | Неможливо оцінити бітрейт |
| `c=IN IP4 0.0.0.0` (connection) | ❌ Ні | Неможливо перевірити мережеві налаштування |

---

## ✅ Виправлений тест + доповнений парсер

### 🔧 Додайте підтримку `fmtp` у `Parse()`:

```go
case "a":
    if media != nil {
        // ... існуючий код ...
        
        // ➕ НОВА ЛОГІКА: обробка fmtp
        if strings.HasPrefix(field, "fmtp:") {
            fmtpVal := strings.TrimPrefix(field, "fmtp:")
            // fmtp:96 profile-level-id=420029;sprop-parameter-sets=Z00AHpWoKA9k,aO48gA==
            parts := strings.SplitN(fmtpVal, " ", 2)
            if len(parts) == 2 {
                params := strings.Split(parts[1], ";")
                for _, param := range params {
                    kv := strings.SplitN(strings.TrimSpace(param), "=", 2)
                    if len(kv) != 2 { continue }
                    key, val := strings.TrimSpace(kv[0]), kv[1]
                    
                    switch key {
                    case "sprop-parameter-sets":
                        // H.264: кілька base64 значень через кому
                        sets := strings.Split(val, ",")
                        for _, s := range sets {
                            if s == "" { continue }
                            if dec, err := base64.StdEncoding.DecodeString(s); err == nil {
                                media.SpropParameterSets = append(media.SpropParameterSets, dec)
                            }
                        }
                    case "config":
                        // AAC: hex-encoded MPEG4AudioConfig
                        if cfg, err := hex.DecodeString(val); err == nil {
                            media.Config = cfg
                        }
                    case "sizelength":
                        media.SizeLength, _ = strconv.Atoi(val)
                    case "indexlength":
                        media.IndexLength, _ = strconv.Atoi(val)
                    case "profile-level-id":
                        // Можна зберегти для валідації профілю
                        media.PayloadType = media.PayloadType // noop, але можна додати поле
                    }
                }
            }
        }
        
        // ➕ Обробка x-dimensions
        if strings.HasPrefix(field, "x-dimensions:") {
            dims := strings.TrimPrefix(field, "x-dimensions:")
            parts := strings.Split(strings.TrimSpace(dims), ",")
            if len(parts) == 2 {
                // Можна додати поля Width/Height у Media struct
                // media.Width, _ = strconv.Atoi(parts[0])
                // media.Height, _ = strconv.Atoi(parts[1])
            }
        }
    }
```

### 🔧 Додайте валідацію у тест:

```go
const testSDP = `v=0
o=- 1459325504777324 1 IN IP4 192.168.0.123
s=RTSP/RTP stream from Network Video Server
m=video 0 RTP/AVP 96
a=rtpmap:96 H264/90000
a=fmtp:96 profile-level-id=420029; packetization-mode=1; sprop-parameter-sets=Z00AHpWoKA9k,aO48gA==
a=x-framerate: 15
a=control:track1
m=audio 0 RTP/AVP 96
a=rtpmap:96 MPEG4-GENERIC/16000/2
a=fmtp:96 streamtype=5;profile-level-id=1;mode=AAC-hbr;sizelength=13;indexlength=3;indexdeltalength=3;config=1408
a=control:track2`

func TestParse_Comprehensive(t *testing.T) {
    sess, medias, err := Parse(testSDP)
    if err != nil {
        t.Fatalf("Parse failed: %v", err)
    }
    
    if len(medias) != 2 {
        t.Fatalf("expected 2 media streams, got %d", len(medias))
    }
    
    // Відео
    video := medias[0]
    if video.Type != av.H264 {
        t.Errorf("video codec: expected H264, got %v", video.Type)
    }
    if video.FPS != 15 {
        t.Errorf("video FPS: expected 15, got %d", video.FPS)
    }
    if len(video.SpropParameterSets) != 2 {
        t.Errorf("SPS/PPS: expected 2 sets, got %d", len(video.SpropParameterSets))
    }
    
    // Аудіо
    audio := medias[1]
    if audio.Type != av.AAC {
        t.Errorf("audio codec: expected AAC, got %v", audio.Type)
    }
    if audio.TimeScale != 16000 {
        t.Errorf("audio clock: expected 16000, got %d", audio.TimeScale)
    }
    if audio.ChannelCount != 2 {
        t.Errorf("audio channels: expected 2, got %d", audio.ChannelCount)
    }
    if hex.EncodeToString(audio.Config) != "1408" {
        t.Errorf("audio config: expected '1408', got '%s'", hex.EncodeToString(audio.Config))
    }
}
```

---

## 🎬 Інтеграція у CCTV Pipeline

### 🔧 Приклад: Авто-налаштування кодека з SDP

```go
// SetupCodecFromSDP — підготовка кодека перед RTP демуксингом
func SetupCodecFromSDP(sdpContent string) ([]av.CodecData, error) {
    _, medias, err := Parse(sdpContent)
    if err != nil {
        return nil, fmt.Errorf("parse SDP: %w", err)
    }
    
    var codecs []av.CodecData
    
    for _, m := range medias {
        switch m.Type {
        case av.H264:
            sps := firstNonEmpty(m.SpropSPS, extractSPS(m.SpropParameterSets))
            pps := firstNonEmpty(m.SpropPPS, extractPPS(m.SpropParameterSets))
            
            if len(sps) == 0 || len(pps) == 0 {
                return nil, fmt.Errorf("H.264 stream missing SPS/PPS in SDP")
            }
            
            codec, err := h264parser.NewCodecDataFromSPSAndPPS(sps, pps)
            if err != nil {
                return nil, fmt.Errorf("create H.264 codec: %w", err)
            }
            codecs = append(codecs, codec)
            
        case av.AAC:
            if len(m.Config) == 0 {
                return nil, fmt.Errorf("AAC stream missing config in SDP")
            }
            
            codec, err := aacparser.NewCodecDataFromMPEG4AudioConfigBytes(m.Config)
            if err != nil {
                return nil, fmt.Errorf("create AAC codec: %w", err)
            }
            codecs = append(codecs, codec)
        }
    }
    
    return codecs, nil
}
```

### ✅ Ваш use-case: валідація перед підключенням

```go
// ValidateRTSPSession — перевірка готовності сесії до стрімінгу
func ValidateRTSPSession(sdpContent string) error {
    _, medias, err := Parse(sdpContent)
    if err != nil {
        return fmt.Errorf("invalid SDP: %w", err)
    }
    
    var hasSupportedVideo, hasSupportedAudio bool
    
    for _, m := range medias {
        switch {
        case m.AVType == "video" && m.Type == av.H264:
            if len(firstNonEmpty(m.SpropSPS, extractSPS(m.SpropParameterSets))) == 0 {
                return fmt.Errorf("H.264 video without SPS")
            }
            hasSupportedVideo = true
            
        case m.AVType == "audio" && (m.Type == av.AAC || m.Type == av.PCM_ALAW || m.Type == av.PCM_MULAW):
            hasSupportedAudio = true
        }
    }
    
    if !hasSupportedVideo {
        return fmt.Errorf("no supported video codec found (expected H.264)")
    }
    // Аудіо опціональне для CCTV
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **`Decode()` не існує** | `undefined: Decode` | Використовуйте `Parse()` або перейменуйте функцію |
| **`fmtp` не парситься** | Порожні `SpropParameterSets` | Додайте обробку `fmtp:` з парсингом `sprop-parameter-sets` |
| **Hex config не декодується** | `Config = nil` для AAC | Використовуйте `hex.DecodeString` з перевіркою помилки |
| **Відносний `control`** | Помилка `404` при SETUP | Нормалізуйте через `url.Parse` + `ResolveReference` |
| **Немає перевірок у тесті** | Тест "проходить" при помилках | Додайте `t.Errorf`/`t.Fatalf` для кожного критичного поля |

---

## 📋 Чек-лист для коректного тесту SDP

```go
// ✅ 1. Виклик існуючої функції з правильними параметрами
sess, medias, err := Parse(sdpContent)

// ✅ 2. Перевірка помилки парсингу
if err != nil { t.Fatalf("Parse failed: %v", err) }

// ✅ 3. Валідація кількості потоків
if len(medias) != expectedCount { t.Errorf(...) }

// ✅ 4. Перевірка типів кодеків
if video.Type != av.H264 { t.Errorf(...) }

// ✅ 5. Валідація параметрів (FPS, clock, channels)
if video.FPS != 15 { t.Errorf(...) }

// ✅ 6. Перевірка SPS/PPS для H.264
if len(video.SpropParameterSets) == 0 { t.Error(...) }

// ✅ 7. Перевірка AAC config
if hex.EncodeToString(audio.Config) != "1408" { t.Errorf(...) }

// ✅ 8. Валідація control URL (відносний/абсолютний)
if !strings.HasPrefix(video.Control, "track") && !strings.HasPrefix(video.Control, "rtsp://") {
    t.Errorf("invalid control URL: %s", video.Control)
}
```

---

## 🔗 Корисні посилання

- 📄 [SDP Specification (RFC 4566)](https://datatracker.ietf.org/doc/html/rfc4566)
- 📄 [RTP Payload Format for H.264 (RFC 6184)](https://datatracker.ietf.org/doc/html/rfc6184#section-8.4) — sprop-parameter-sets
- 📄 [MPEG-4 AudioSpecificConfig](https://wiki.multimedia.cx/index.php/MPEG-4_Audio#AudioSpecificConfig) — hex encoding
- 💻 [Go testing Package](https://pkg.go.dev/testing) — best practices для юніт-тестів
- 🧪 [Go hex/base64 Packages](https://pkg.go.dev/encoding) — декодування параметрів

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди валідуйте результати парсингу** — порожні `SPS/PPS` зламають декодер у runtime.
> 2. **Підтримуйте `fmtp` атрибут** — багато камер надсилають параметри саме там.
> 3. **Перевіряйте hex/base64 декодування** — помилки у цих полях критичні для ініціалізації кодеків.
> 4. **Тестуйте на реальних SDP** — LIVE555, Hikvision, Dahua мають різні формати.
> 5. **Додайте логування помилок** — для дебагу проблем підключення до камер.

Потрібен приклад парсингу **H.264 SPS** для витягування роздільної здатності та профілю без зовнішніх залежностей? Готовий допомогти! 🚀