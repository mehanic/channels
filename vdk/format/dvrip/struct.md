# 🎬 Глибокий розбір: dvrip utils — Протокольні утиліти для DVR/IP камер

Цей файл — **набір допоміжних функцій та констант** для пропрієтарного протоколу DVRIp, що використовується у багатьох китайських системах відеоспостереження (XMeye, VStarcam, тощо). Він містить критичні компоненти для автентифікації, парсингу медіа-типів, конвертації часових міток та бітової серіалізації.

Розберемо архітектуру, ключові алгоритми та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема dvrip utils

```
┌────────────────────────────────────────┐
│ 📦 dvrip utils — Protocol Helpers      │
├────────────────────────────────────────┤
│                                         │
│  🔐 sofiaHash() — кастомне хешування  │
│  • MD5 → pair sum → base62 encoding   │
│                                         │
│  🎬 parseMediaType() — детект формату │
│  • dataType + mediaCode → string      │
│  • H.264/H.265/PCM_ALAW/JPEG support  │
│                                         │
│  🕐 parseDatetime() — unpack timestamp│
│  • 32-bit packed: YYYYYY MMMMM DDDDD  │
│                       HHHHH MMMMM SSSSSS│
│                                         │
│  🔢 binSize() — AVCC length prefix    │
│  • int → 4-byte big-endian            │
│                                         │
│  📦 Payload/LoginResp — структури     │
│  • Заголовки протоколу та відповіді   │
│                                         │
│  🔢 requestCode/statusCode — константи│
│  • Команди та коди результатів        │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔐 1. sofiaHash — кастомне хешування паролів

### Алгоритм крок за кроком:

```go
func sofiaHash(password string) string {
    // 1. MD5 хеш (16 байт)
    digest := md5.Sum([]byte(password))
    
    // 2. Ініціалізація результату (макс. 8 символів)
    hash := make([]byte, 0, 8)
    
    // 3. Обробка пар байт: (digest[i-1] + digest[i]) % 62
    for i := 1; i < len(digest); i += 2 {
        sum := int(digest[i-1]) + int(digest[i])  // 0-510
        hash = append(hash, alnum[sum%62])        // base62: 0-9A-Za-z
    }
    
    return string(hash)  // 8-символьний рядок
}
```

### 🔍 Приклад розрахунку:

```
Вхід: password = "admin"

1. MD5("admin") = [0x21, 0x23, 0x2F, 0x6B, 0x8C, 0x12, 0x45, 0x9A, ...]
2. Пари байт:
   • (0,1): 0x21+0x23 = 68 → 68%62 = 6 → alnum[6] = '6'
   • (2,3): 0x2F+0x6B = 156 → 156%62 = 32 → alnum[32] = 'W'
   • (4,5): 0x8C+0x12 = 158 → 158%62 = 34 → alnum[34] = 'Y'
   • ... продовжуємо до 8 символів
3. Результат: "6WY..." (8 символів)
```

### ⚠️ Критичні застереження:

```
❌ sofiaHash НЕ є криптографічно стійким:
• Використовує слабкий MD5
• Проста лінійна комбінація байт
• Вихідний простір лише 62^8 ≈ 2.18×10^14 (замість 2^128 для MD5)

✅ Призначення: тільки для сумісності з протоколом DVR
• Не використовуйте для зберігання паролів у вашій системі
• Для автентифікації користувачів використовуйте bcrypt/argon2

✅ Безпечне використання:
• Зберігайте пароль у відкритому вигляді тільки в пам'яті
• Викликайте sofiaHash безпосередньо перед відправкою у мережу
• Не логувайте хешовані паролі
```

### ✅ Ваш use-case: безпечна автентифікація

```go
// DVRIpAuth — безпечна обгортка для автентифікації
type DVRIpAuth struct {
    username string
    password string  // зберігаємо у відкритому вигляді (тільки в пам'яті!)
}

// GetProtocolHash — отримання хешу для протоколу
func (a *DVRIpAuth) GetProtocolHash() string {
    // Викликаємо sofiaHash безпосередньо перед використанням
    return sofiaHash(a.password)
}

// Clear — очищення чутливих даних
func (a *DVRIpAuth) Clear() {
    // Перезаписуємо пароль нулями перед звільненням
    for i := range a.password {
        a.password[i] = 0
    }
    a.password = ""
}

// Використання:
auth := &DVRIpAuth{username: "admin", password: "secret123"}
defer auth.Clear()  // гарантоване очищення

// У Login():
body, _ := json.Marshal(map[string]string{
    "PassWord": auth.GetProtocolHash(),  // тільки тут хешуємо
    "UserName": auth.username,
})
```

---

## 🎬 2. parseMediaType — детекція типу медіа

### Логіка парсингу:

```go
func parseMediaType(dataType uint32, mediaCode byte) string {
    switch dataType {
    case 0x1FC, 0x1FD:  // Відео потоки
        switch mediaCode {
        case 1: return "MPEG4"
        case 2: return "H264"   // Найпоширеніший
        case 3: return "H265"   // HEVC
        }
    case 0x1F9:  // Інформаційні пакети
        if mediaCode == 1 || mediaCode == 6 {
            return "info"
        }
    case 0x1FA:  // Аудіо (PCM A-law)
        if mediaCode == 0xE {  // 14 decimal
            return "PCM_ALAW"  // G.711 A-law
        }
    case 0x1FE:  // JPEG зображення
        if mediaCode == 0 {
            return "JPEG"
        }
    default:
        return "unknown"
    }
    return "unexpected"
}
```

### 📊 Таблиця типів даних:

| dataType | mediaCode | Тип | Призначення | Обробка у вашому проекті |
|----------|-----------|-----|-------------|-------------------------|
| `0x1FC` | 2 | H.264 | Основний відео потік | ✅ Транскодування у HLS |
| `0x1FD` | 2 | H.264 | Додатковий потік | ⚠️ Може бути lower bitrate |
| `0x1FC` | 3 | H.265 | HEVC відео | ✅ Якщо підтримується |
| `0x1FA` | 0xE (14) | PCM_ALAW | Аудіо G.711 | ✅ Конвертація у AAC |
| `0x1FE` | 0 | JPEG | Снепшоти/прев'ю | ✅ Для прев'ю каналу |
| `0x1F9` | 1,6 | info | Метадані | ℹ️ Логування/аналітика |

### ✅ Ваш use-case: фільтрація потоків за типом

```go
// ShouldProcessFrame — чи обробляти фрейм за конфігурацією
func ShouldProcessFrame(dataType uint32, mediaCode byte, config *ChannelConfig) bool {
    mediaType := parseMediaType(dataType, mediaCode)
    
    switch mediaType {
    case "H264", "H265":
        return config.ProcessVideo  // обробляти відео?
    case "PCM_ALAW":
        return !config.DisableAudio  // обробляти аудіо?
    case "JPEG":
        return config.CaptureSnapshots  // зберігати прев'ю?
    case "info":
        return config.LogMetadata  // логувати метадані?
    default:
        // Логування невідомих типів для подальшого аналізу
        log.Printf("unknown media type: dataType=0x%X, mediaCode=0x%X", 
            dataType, mediaCode)
        return false
    }
}

// Використання у Monitor():
if !ShouldProcessFrame(dataType, frame.Media, p.config) {
    p.metrics.SkippedFrames.Inc()
    continue  // пропускаємо непотрібні пакети
}
```

---

## 🕐 3. parseDatetime — конвертація 32-бітної часової мітки

### Формат packed timestamp:

```
Бітова структура (32 біти, LittleEndian):

Біти: 31-26  25-22  21-17  16-12  11-6   5-0
      │      │      │      │      │      │
      рік    місяць день   година хвилина секунда
      (6)    (5)    (5)    (5)    (6)    (6)

Діапазони значень:
• рік: 0-63 → 2000-2063 (додаємо 2000)
• місяць: 1-12 (5 біт = 0-31, але валідні 1-12)
• день: 1-31 (5 біт)
• година: 0-23 (5 біт = 0-31, але валідні 0-23)
• хвилина: 0-59 (6 біт)
• секунда: 0-59 (6 біт)
```

### 🔧 Реалізація з коментарями:

```go
func parseDatetime(value uint32) time.Time {
    // Витягування полів через бітові маски та зсуви
    second := int(value & 0x3F)                    // біти 0-5: 0-59
    minute := int((value & 0xFC0) >> 6)           // біти 6-11: 0-59
    hour := int((value & 0x1F000) >> 12)          // біти 12-16: 0-23
    day := int((value & 0x3E0000) >> 17)          // біти 17-21: 1-31
    month := int((value & 0x3C00000) >> 22)       // біти 22-26: 1-12
    year := int(((value & 0xFC000000) >> 26) + 2000)  // біти 26-31: 2000-2063
    
    // Створення time.Time у UTC
    return time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
}
```

### 🔍 Приклад розпакування:

```
Вхід: value = 0x12345678 (LittleEndian: байти [0x78, 0x56, 0x34, 0x12])

Бінарне представлення (big-endian для читання):
  00010010 00110100 01010110 01111000

Розбиття:
  біти 31-26: 000100 → 4 → рік = 4+2000 = 2004
  біти 25-22: 1000 → 8 → місяць = 8 (серпень)
  біти 21-17: 11010 → 26 → день = 26
  біти 16-12: 00101 → 5 → година = 5
  біти 11-6:  010110 → 22 → хвилина = 22
  біти 5-0:   011000 → 24 → секунда = 24

Результат: 2004-08-26 05:22:24 UTC
```

### ✅ Ваш use-case: синхронізація часу та логування

```go
// LogFrameWithTimestamp — логування з часовими мітками камери
func LogFrameWithTimestamp(frameTime uint32, mediaType string, size int, channelID string) {
    cameraTime := parseDatetime(frameTime)
    serverTime := time.Now()
    
    // Розрахунок затримки
    delay := serverTime.Sub(cameraTime)
    
    log.Printf("[%s] %s: %d bytes, camera=%s, server=%s, delay=%v",
        channelID,
        mediaType,
        size,
        cameraTime.Format(time.RFC3339),
        serverTime.Format(time.RFC3339),
        delay,
    )
    
    // Метрики для моніторингу
    metrics.FrameDelay.WithLabelValues(channelID).Observe(delay.Seconds())
    metrics.CameraTimeSkew.WithLabelValues(channelID).Set(float64(delay.Milliseconds()))
}

// ValidateTimestamp — перевірка коректності часової мітки
func ValidateTimestamp(value uint32) bool {
    ts := parseDatetime(value)
    
    // Перевірка діапазонів
    if ts.Year() < 2000 || ts.Year() > 2063 {
        return false
    }
    if ts.Month() < 1 || ts.Month() > 12 {
        return false
    }
    if ts.Day() < 1 || ts.Day() > 31 {
        return false
    }
    if ts.Hour() > 23 || ts.Minute() > 59 || ts.Second() > 59 {
        return false
    }
    
    // Перевірка чи час не занадто у майбутньому/минулому
    now := time.Now()
    if ts.Before(now.Add(-24*time.Hour)) || ts.After(now.Add(1*time.Hour)) {
        return false
    }
    
    return true
}
```

---

## 🔢 4. binSize — бітова серіалізація для AVCC формату

### Реалізація:

```go
func binSize(val int) []byte {
    buf := make([]byte, 4)
    binary.BigEndian.PutUint32(buf, uint32(val))
    return buf
}
```

### 🔍 Призначення та використання:

```
Перетворює ціле число у 4-байтовий big-endian масив.

Приклади:
  binSize(1024) → [0x00, 0x00, 0x04, 0x00]
  binSize(65536) → [0x00, 0x01, 0x00, 0x00]
  binSize(1) → [0x00, 0x00, 0x00, 0x01]

Використовується для:
• Додавання 4-байтового префіксу довжини перед кожним NALU
• Конвертація Annex B (start codes: 0x00000001) → AVCC (length-prefixed)
• Сумісність з контейнерами MP4/FLV/HLS, де NALU зберігаються з префіксом довжини
```

### ✅ Ваш use-case: підготовка NALU для HLS

```go
// PrepareNALUForContainer — конвертація у AVCC формат для HLS/MP4
func PrepareNALUForContainer(nalu []byte) []byte {
    // AVCC формат: [4-byte big-endian length][NALU data]
    return append(binSize(len(nalu)), nalu...)
}

// ProcessVideoFrame — обробка відео фрейму для запису у HLS
func (p *HLSProcessor) ProcessVideoFrame(nalus [][]byte) error {
    for _, nalu := range nalus {
        naluType := nalu[0] & 0x1f
        
        // Обробляємо тільки VCL NALU (відео дані)
        if naluType < 1 || naluType > 5 {
            continue
        }
        
        // Підготовка для AVCC формату
        prepared := PrepareNALUForContainer(nalu)
        
        // Створення пакету з метаданими
        pkt := &av.Packet{
            Duration:   p.frameDuration,  // розрахована з FPS
            Idx:        0,                // відео потік
            IsKeyFrame: naluType == 5,    // IDR = ключовий кадр
            Data:       prepared,
        }
        
        // Запис у muxer
        if err := p.muxer.WritePacket(*pkt); err != nil {
            return fmt.Errorf("write packet: %w", err)
        }
        
        p.metrics.VideoPacketsWritten.Inc()
    }
    return nil
}
```

---

## 📦 5. Payload та LoginResp — структури протоколу

### Payload — заголовок пакету (20 байт):

```go
type Payload struct {
    Head           byte   // завжди 0xFF (магічний байт)
    Version        byte   // версія протоколу (зазвичай 0)
    _              byte   // padding/reserved
    _              byte   // padding/reserved
    Session        int32  // ID сесії після успішного логіну
    SequenceNumber int32  // лічильник пакетів (збільшується з кожним обміном)
    _              byte   // padding
    _              byte   // padding
    MsgID          int16  // тип повідомлення (requestCode)
    BodyLength     int32  // довжина тіла + 2 (для magicEnd: 0x0A, 0x00)
}
```

### 🔍 Розмір та вирівнювання:

```
Загальний розмір: 20 байт (LittleEndian)
• 0-3:   Head(1) + Version(1) + padding(2) = 4 байти
• 4-7:   Session (int32) = 4 байти
• 8-11:  SequenceNumber (int32) = 4 байти
• 12-13: padding(2) = 2 байти
• 14-15: MsgID (int16) = 2 байти
• 16-19: BodyLength (int32) = 4 байти
```

### LoginResp — відповідь на логін (з помилкою!):

```go
type LoginResp struct {
    AliveInterval int    `json:"AliveInterval"`   // інтервал KeepAlive у секундах
    ChannelNum    int    `json:"ChannelNum"`      // кількість каналів
    DeviceType    string `json:"DeviceType "`     // ⚠️ ЗАЙВИЙ ПРОБІЛ у ключі!
    ExtraChannel  int    `json:"ExtraChannel"`    // додаткові канали?
    Ret           int    `json:"Ret"`             // код результату (statusCode)
    SessionID     string `json:"SessionID"`       // hex рядок сесії (напр. "0x12345678")
}
```

### ⚠️ Критична помилка у `DeviceType`:

```go
`json:"DeviceType "`  // ← зайвий пробіл у кінці!

Це призведе до того, що поле не розпарситься, якщо сервер повертає "DeviceType" без пробілу.

✅ Виправлення:
`json:"DeviceType"`  // без пробілу

✅ Альтернатива (якщо сервер дійсно надсилає з пробілом):
`json:"DeviceType ,omitempty"`  // але це не стандартно
```

---

## 🔢 6. requestCode та statusCode — константи протоколу

### requestCode — типи команд:

```go
const (
    codeLogin            requestCode = 1000  // автентифікація
    codeKeepAlive        requestCode = 1006  // підтримка з'єднання
    codeOPMonitor        requestCode = 1413  // старт моніторингу потоку
    codeOPTimeSetting    requestCode = 1450  // синхронізація часу
    // ... інші команди
)

var requestCodes = map[requestCode]string{
    codeOPMonitor:     "OPMonitor",
    codeOPTimeSetting: "OPTimeSetting",
}
```

### statusCode — коди результатів:

```go
const (
    statusOK                                  statusCode = 100
    statusUsernameOrPasswordIsIncorrect       statusCode = 106
    statusUserDoesNotHaveNecessaryPermissions statusCode = 107
    // ... інші коди
)

var statusCodes = map[statusCode]string{
    statusOK: "OK",
    statusUsernameOrPasswordIsIncorrect: "Username or password is incorrect",
    // ... мапінг для логування
}
```

### ✅ Ваш use-case: обробка помилок автентифікації

```go
// HandleLoginResponse — обробка відповіді на логін
func HandleLoginResponse(resp LoginResp) error {
    switch statusCode(resp.Ret) {
    case statusOK:
        return nil  // успіх
    case statusUsernameOrPasswordIsIncorrect:
        return fmt.Errorf("authentication failed: invalid credentials")
    case statusUserDoesNotHaveNecessaryPermissions:
        return fmt.Errorf("authentication failed: insufficient permissions")
    case statusUserAlreadyLoggedIn:
        log.Printf("warning: user already logged in, proceeding anyway")
        return nil  // можна продовжити
    default:
        if msg, ok := statusCodes[statusCode(resp.Ret)]; ok {
            return fmt.Errorf("login failed: %s (code=%d)", msg, resp.Ret)
        }
        return fmt.Errorf("login failed: unknown error code %d", resp.Ret)
    }
}

// Використання у Login():
respBody, err := client.recv(true)
if err != nil {
    return fmt.Errorf("receive response: %w", err)
}

var resp LoginResp
if err := json.Unmarshal(respBody, &resp); err != nil {
    return fmt.Errorf("parse response: %w", err)
}

if err := HandleLoginResponse(resp); err != nil {
    return err
}

// Збереження сесії
session, err := strconv.ParseUint(resp.SessionID, 0, 32)
if err != nil {
    return fmt.Errorf("parse session ID: %w", err)
}
client.session = int32(session)
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// dvrip_protocol_handler.go — інтеграція протокольних утиліт
type DVRIpProtocolHandler struct {
    channelID    string
    credentials  *DVRIpCredentials
    sessionID    int32
    sequenceNum  int32
    metrics      *ProtocolMetrics
}

// BuildLoginPayload — створення тіла запиту на логін
func (h *DVRIpProtocolHandler) BuildLoginPayload() ([]byte, error) {
    return json.Marshal(map[string]string{
        "EncryptType": "MD5",
        "LoginType":   "DVRIP-WEB",
        "PassWord":    sofiaHash(h.credentials.Password),  // кастомний хеш
        "UserName":    h.credentials.Username,
    })
}

// BuildMonitorCommand — створення команди старту моніторингу
func (h *DVRIpProtocolHandler) BuildMonitorCommand(streamType string) ([]byte, error) {
    return json.Marshal(map[string]interface{}{
        "Name":      "OPMonitor",
        "SessionID": fmt.Sprintf("0x%08X", h.sessionID),
        "OPMonitor": map[string]interface{}{
            "Action": "Start",
            "Parameter": map[string]interface{}{
                "Channel":    0,
                "CombinMode": "NONE",
                "StreamType": streamType,
                "TransMode":  "TCP",
            },
        },
    })
}

// ProcessIncomingFrame — обробка отриманого фрейму
func (h *DVRIpProtocolHandler) ProcessIncomingFrame(
    dataType uint32, 
    mediaCode byte, 
    payload []byte, 
    timestamp uint32,
) error {
    mediaType := parseMediaType(dataType, mediaCode)
    
    // Валідація часової мітки
    if timestamp != 0 && !ValidateTimestamp(timestamp) {
        log.Printf("warning: invalid timestamp 0x%X", timestamp)
    }
    
    // Логування з часовою міткою
    if timestamp != 0 {
        ts := parseDatetime(timestamp)
        h.metrics.FrameTimestamp.WithLabelValues(h.channelID).Set(float64(ts.Unix()))
    }
    
    switch mediaType {
    case "H264", "H265":
        return h.processVideoFrame(payload, mediaType)
    case "PCM_ALAW":
        return h.processAudioFrame(payload)
    case "JPEG":
        return h.processSnapshot(payload)
    case "info":
        return h.processMetadata(payload)
    default:
        h.metrics.UnknownMediaTypes.Inc()
        return nil  // ігноруємо невідомі типи
    }
}

// processVideoFrame — підготовка відео для HLS
func (h *DVRIpProtocolHandler) processVideoFrame(data []byte, codec string) error {
    // Розбиття на NALU
    nalus, _ := h264parser.SplitNALUs(data)
    
    for _, nalu := range nalus {
        naluType := nalu[0] & 0x1f
        
        // Обробляємо тільки VCL NALU (відео дані)
        if naluType < 1 || naluType > 5 {
            continue
        }
        
        // Підготовка для AVCC формату
        prepared := append(binSize(len(nalu)), nalu...)
        
        pkt := &av.Packet{
            Duration:   h.calculateFrameDuration(codec),
            Idx:        0,  // відео потік
            IsKeyFrame: naluType == 5,  // IDR = ключовий кадр
            Data:       prepared,
        }
        
        // Відправка у чергу обробки
        select {
        case h.packetQueue <- pkt:
            h.metrics.VideoPacketsSent.Inc()
        default:
            h.metrics.DroppedPackets.Inc()
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **LoginResp.DeviceType не парситься** | Поле залишається порожнім | Виправте `json:"DeviceType "` → `json:"DeviceType"` у структурі |
| **parseDatetime повертає невірний час** | Неправильні бітові маски або порядок байт | Переконайтеся, що використовуєте LittleEndian для читання; протестуйте з відомими значеннями |
| **sofiaHash дає різні результати** | Різні реалізації MD5 або base62 | Переконайтеся, що використовуєте стандартний `crypto/md5`; перевірте `alnum` рядок |
| **binSize не сумісний з плеєром** | Неправильний endian або розмір | Використовуйте `binary.BigEndian` для AVCC; перевірте, що результат — 4 байти |
| **parseMediaType повертає "unexpected"** | Невідомі комбінації dataType/mediaCode | Додайте логування невідомих значень; оновіть мапінг при виявленні нових типів |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування результатів parseMediaType:

```go
type MediaTypeCache struct {
    mu    sync.RWMutex
    cache map[uint64]string  // key: (dataType<<8)|mediaCode → mediaType
}

func (c *MediaTypeCache) Get(dataType uint32, mediaCode byte) string {
    key := uint64(dataType)<<8 | uint64(mediaCode)
    
    c.mu.RLock()
    if mt, ok := c.cache[key]; ok {
        c.mu.RUnlock()
        return mt
    }
    c.mu.RUnlock()
    
    // Обчислення якщо не в кеші
    mt := parseMediaType(dataType, mediaCode)
    
    c.mu.Lock()
    if c.cache == nil {
        c.cache = make(map[uint64]string)
    }
    c.cache[key] = mt
    c.mu.Unlock()
    
    return mt
}
```

### 2. Попереднє виділення буферів для binSize:

```go
// binSizePool — пул буферів для уникнення аллокацій
var binSizePool = sync.Pool{
    New: func() interface{} {
        buf := make([]byte, 4)
        return &buf
    },
}

func binSizePooled(val int) []byte {
    bufPtr := binSizePool.Get().(*[]byte)
    defer binSizePool.Put(bufPtr)
    
    binary.BigEndian.PutUint32(*bufPtr, uint32(val))
    
    // Копіюємо результат, бо буфер повертається у пул
    result := make([]byte, 4)
    copy(result, *bufPtr)
    return result
}
```

### 3. Моніторинг протокольних метрик:

```go
type ProtocolMetrics struct {
    LoginAttempts    prometheus.CounterVec
    LoginSuccess     prometheus.CounterVec
    FrameTypes       prometheus.CounterVec
    ParseErrors      prometheus.CounterVec
    FrameDelay       prometheus.HistogramVec
}

func (m *ProtocolMetrics) RecordLogin(success bool, channelID string) {
    m.LoginAttempts.WithLabelValues(channelID).Inc()
    if success {
        m.LoginSuccess.WithLabelValues(channelID).Inc()
    }
}

func (m *ProtocolMetrics) RecordFrame(mediaType string, delay time.Duration, channelID string) {
    m.FrameTypes.WithLabelValues(mediaType, channelID).Inc()
    m.FrameDelay.WithLabelValues(channelID).Observe(delay.Seconds())
}
```

---

## 📋 Чек-лист інтеграції dvrip utils

```go
// ✅ 1. Виправлення помилки у LoginResp
type LoginResp struct {
    // ... інші поля ...
    DeviceType string `json:"DeviceType"`  // без пробілу!
}

// ✅ 2. Валідація часових міток
ts := parseDatetime(frame.DateTime)
if ts.Year() < 2000 || ts.Year() > 2063 {
    log.Printf("warning: invalid timestamp: %v", ts)
}

// ✅ 3. Безпечне хешування паролів
// sofiaHash використовується тільки для протоколу, не для зберігання!
passwordHash := sofiaHash(plaintextPassword)

// ✅ 4. Обробка невідомих типів медіа
mediaType := parseMediaType(dataType, mediaCode)
if mediaType == "unknown" || mediaType == "unexpected" {
    log.Printf("unknown media: dataType=0x%X, mediaCode=0x%X", dataType, mediaCode)
    metrics.UnknownMedia.Inc()
}

// ✅ 5. Підготовка NALU для контейнерів
preparedNALU := append(binSize(len(nalu)), nalu...)  // AVCC формат

// ✅ 6. Логування з метриками
metrics.RecordFrame(mediaType, time.Since(cameraTime), channelID)
```

---

## 🔗 Корисні посилання

- 💻 [vdk dvrip Package](https://pkg.go.dev/github.com/deepch/vdk/format/dvrip) — GoDoc documentation
- 📄 [XMeye/DVRIP Protocol Analysis](https://github.com/bluenviron/mediamtx/issues/123) — спільнотний аналіз
- 📄 [MD5 Specification (RFC 1321)](https://datatracker.ietf.org/doc/html/rfc1321) — стандарт хешування
- 📄 [Base62 Encoding](https://www.crockford.com/base32.html) — альтернативні кодування
- 🧪 [Go encoding/binary Documentation](https://pkg.go.dev/encoding/binary) — робота з бітовими даними

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **пропрієтарними DVR камерами**:
> 1. **Виправте `json:"DeviceType "`** — зайвий пробіл призведе до помилок парсингу відповідей сервера.
> 2. **Валідуйте часові мітки** — `parseDatetime` може повернути некоректні дати при пошкоджених даних.
> 3. **Кешуйте результати `parseMediaType`** — це зменшить накладні витрати при обробці тисяч пакетів на секунду.
> 4. **Не використовуйте `sofiaHash` для зберігання паролів** — це не криптографічно стійкий алгоритм, тільки для сумісності з протоколом.
> 5. **Логуруйте невідомі типи медіа** — це допоможе виявляти нові моделі камер або оновлення прошивок.

Потрібен приклад реалізації автоматичного визначення та оновлення мапінгу `parseMediaType` для нових типів даних? Готовий допомогти! 🚀