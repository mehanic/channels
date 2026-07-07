# 📦 Глибокий розбір: flvio — AMF0 парсер/серіалізатор для RTMP/FLV

Цей файл — **реалізація парсингу та серіалізації AMF0 (Action Message Format version 0)**, бінарного формату даних, що використовується у протоколах RTMP та FLV для передачі метаданих, команд та структурованих даних.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема flvio пакету

```
┌────────────────────────────────────────┐
│ 📦 flvio — AMF0 Parser/Serializer      │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • AMF0 маркери (0-17) — типи даних    │
│  • LenAMF0Val() — розрахунок розміру   │
│  • FillAMF0Val() — серіалізація        │
│  • ParseAMF0Val() — парсинг            │
│  • AMF0ParseError — ланцюжок помилок   │
│                                         │
│  📊 Підтримувані типи:                  │
│  • number (float64), boolean, string   │
│  • object, null, undefined             │
│  • ECMAArray (асоціативний масив)      │
│  • StrictArray (індексований масив)    │
│  • Date (time.Time)                    │
│                                         │
│  🔄 Потік даних:                        │
│  Go interface{} ↔ AMF0 binary ↔ RTMP/FLV│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. AMF0 маркери — типи даних

### Константи маркерів:

```go
const (
    numbermarker = iota      // 0: число (float64)
    booleanmarker            // 1: булеве значення
    stringmarker             // 2: рядок (до 65535 байт)
    objectmarker             // 3: асоціативний масив (ключ-значення)
    movieclipmarker          // 4: застарілий (Flash MovieClip)
    nullmarker               // 5: null
    undefinedmarker          // 6: undefined
    referencemarker          // 7: посилання (застаріле)
    ecmaarraymarker          // 8: ECMA Array (асоціативний масив з лічильником)
    objectendmarker          // 9: маркер кінця об'єкта (0x000009)
    strictarraymarker        // 10: Strict Array (індексований масив)
    datemarker               // 11: дата (float64 timestamp + timezone)
    longstringmarker         // 12: довгий рядок (до 2^32 байт)
    unsupportedmarker        // 13: unsupported type
    recordsetmarker          // 14: RecordSet (застарілий)
    xmldocumentmarker        // 15: XML документ
    typedobjectmarker        // 16: типізований об'єкт
    avmplusobjectmarker      // 17: об'єкт AVM+ (AMF3)
)
```

### 📋 Формати даних у бінарному вигляді:

| Тип | Маркер | Структура | Приклад |
|-----|--------|-----------|---------|
| **Number** | `0x00` | `[0x00][8-byte BE float64]` | `00 3F F0 00 00 00 00 00 00` (= 1.0) |
| **Boolean** | `0x01` | `[0x01][1-byte 0/1]` | `01 01` (= true) |
| **String** | `0x02` | `[0x02][2-byte len][UTF-8 data]` | `02 00 05 68 65 6C 6C 6F` (= "hello") |
| **Object** | `0x03` | `[0x03][key-value pairs...][0x00 0x00 0x09]` | див. нижче |
| **Null** | `0x05` | `[0x05]` | `05` |
| **ECMAArray** | `0x08` | `[0x08][4-byte count][key-value pairs...][0x00 0x00 0x09]` | асоціативний масив |
| **Date** | `0x0B` | `[0x0B][8-byte BE float64 ms][2-byte timezone]` | timestamp у мілісекундах |
| **LongString** | `0x0C` | `[0x0C][4-byte len][UTF-8 data]` | рядок > 65535 байт |

### 🔍 Приклад серіалізації об'єкта:

```
Вхід: AMFMap{"name": "camera1", "fps": 30}

Бінарний вихід:
03                          // object marker
  00 04 6E 61 6D 65        // key length=4, "name"
  02 00 07 63 61 6D 65 72 61 31  // string marker, len=7, "camera1"
  00 03 66 70 73           // key length=3, "fps"
  00 40 3E 00 00 00 00 00 00  // number marker, float64(30.0)
00 00 09                   // object end marker

Загальний розмір: 1 + (2+4) + (1+2+7) + (2+3) + (1+8) + 3 = 34 байти
```

---

## 🔑 2. LenAMF0Val() — розрахунок розміру серіалізації

### Призначення:
Обчислює, скільки байт займе серіалізація значення у форматі AMF0, без фактичного запису. Це потрібно для попереднього виділення буфера.

### 🔧 Логіка для різних типів:

```go
func LenAMF0Val(_val interface{}) (n int) {
    switch val := _val.(type) {
    // Числа: завжди 1 байт маркер + 8 байт float64 = 9 байт
    case int8, int16, int32, int64, int, uint8, uint16, uint32, uint64, uint, float32, float64:
        n += lenAMF0Number  // = 9
    
    // Рядки: маркер + довжина (2 або 4 байти) + дані
    case string:
        u := len(val)
        if u <= 65536 {
            n += 3  // marker(1) + len(2)
        } else {
            n += 5  // marker(1) + len(4) для longstring
        }
        n += int(u)  // самі дані
    
    // ECMAArray: маркер + count(4) + пари ключ-значення + end(3)
    case AMFECMAArray:
        n += 5  // marker(1) + count(4)
        for k, v := range val {
            n += 2 + len(k)      // key length(2) + key data
            n += LenAMF0Val(v)   // рекурсивний розрахунок значення
        }
        n += 3  // object end marker (0x000009)
    
    // AMFMap (object): маркер + пари ключ-значення + end(3)
    case AMFMap:
        n++  // marker
        for k, v := range val {
            if len(k) > 0 {  // порожні ключі ігноруються
                n += 2 + len(k)
                n += LenAMF0Val(v)
            }
        }
        n += 3  // end marker
    
    // StrictArray: маркер + count(4) + елементи
    case AMFArray:
        n += 5  // marker(1) + count(4)
        for _, v := range val {
            n += LenAMF0Val(v)
        }
    
    // Date: маркер + timestamp(8) + timezone(2) = 11 байт
    case time.Time:
        n += 1 + 8 + 2
    
    // Boolean: маркер + 1 байт значення = 2 байти
    case bool:
        n += 2
    
    // Null: тільки маркер = 1 байт
    case nil:
        n++
    }
    return
}
```

### ✅ Ваш use-case: попереднє виділення буфера для ефективності

```go
// SerializeAMF0WithPrealloc — серіалізація з попереднім виділенням буфера
func SerializeAMF0WithPrealloc(val interface{}) ([]byte, error) {
    // 1. Розрахунок потрібного розміру
    size := LenAMF0Val(val)
    
    // 2. Попереднє виділення буфера (уникаємо реаллокацій)
    buf := make([]byte, size)
    
    // 3. Серіалізація у виділений буфер
    n := FillAMF0Val(buf, val)
    
    if n != size {
        return nil, fmt.Errorf("size mismatch: expected %d, got %d", size, n)
    }
    
    return buf[:n], nil
}

// Використання для RTMP команд:
cmd := flvio.AMFMap{
    "cmd":      "publish",
    "streamId": "live/camera1",
    "type":     "live",
}
cmdBytes, err := SerializeAMF0WithPrealloc(cmd)
if err != nil { /* handle error */ }
// cmdBytes готовий для відправки у RTMP пакет
```

---

## 🔑 3. FillAMF0Val() — серіалізація у бінарний формат

### 🔧 Ключові моменти серіалізації:

```go
func FillAMF0Val(b []byte, _val interface{}) (n int) {
    switch val := _val.(type) {
    // Числа: конвертація у float64 + запис у big-endian
    case int, float64, etc.:
        b[n] = numbermarker
        n++
        fillBEFloat64(b[n:], float64(val))  // 8 байт big-endian
        n += 8
    
    // Рядки: вибір між stringmarker та longstringmarker
    case string:
        u := len(val)
        if u <= 65536 {
            b[n] = stringmarker
            n++
            pio.PutU16BE(b[n:], uint16(u))  // 2-байтова довжина
            n += 2
        } else {
            b[n] = longstringmarker
            n++
            pio.PutU32BE(b[n:], uint32(u))  // 4-байтова довжина
            n += 4
        }
        copy(b[n:], []byte(val))  // копіювання даних
        n += len(val)
    
    // ECMAArray: заголовок + пари ключ-значення + end marker
    case AMFECMAArray:
        b[n] = ecmaarraymarker
        n++
        pio.PutU32BE(b[n:], uint32(len(val)))  // кількість елементів
        n += 4
        for k, v := range val {
            pio.PutU16BE(b[n:], uint16(len(k)))  // довжина ключа
            n += 2
            copy(b[n:], []byte(k))  // ключ
            n += len(k)
            n += FillAMF0Val(b[n:], v)  // рекурсивна серіалізація значення
        }
        pio.PutU24BE(b[n:], 0x000009)  // object end marker
        n += 3
    
    // Date: конвертація time.Time → millisecond timestamp
    case time.Time:
        b[n] = datemarker
        n++
        u := val.UnixNano()
        f := float64(u / 1000000)  // конвертація у мілісекунди
        n += fillBEFloat64(b[n:], f)  // 8 байт timestamp
        pio.PutU16BE(b[n:], uint16(0))  // timezone offset (зазвичай 0)
        n += 2
    }
    return
}
```

### 🔍 Допоміжні функції для бітових операцій:

```go
// parseBEFloat64 — читання float64 з big-endian байтів
func parseBEFloat64(b []byte) float64 {
    return math.Float64frombits(pio.U64BE(b))
}

// fillBEFloat64 — запис float64 у big-endian формат
func fillBEFloat64(b []byte, f float64) int {
    pio.PutU64BE(b, math.Float64bits(f))
    return 8
}
```

### ✅ Ваш use-case: серіалізація метаданих для FLV заголовка

```go
// CreateFLVMetadata — створення AMF0 метаданих для FLV файлу
func CreateFLVMetadata(width, height, fps int, duration time.Duration, codec string) ([]byte, error) {
    metadata := flvio.AMFECMAArray{
        "duration":   duration.Seconds(),
        "width":      width,
        "height":     height,
        "framerate":  fps,
        "videocodecid": codec,  // напр. "avc1" для H.264
        "audiocodecid": "mp4a", // напр. для AAC
        "creationdate": time.Now().Format(time.RFC3339),
    }
    
    // Серіалізація: ["onMetaData"][metadata]
    var buf bytes.Buffer
    
    // 1. Рядок "onMetaData"
    buf.WriteByte(flvio.StringMarker)
    pio.PutU16BE(buf.Bytes()[buf.Len():], uint16(len("onMetaData")))
    buf.WriteString("onMetaData")
    
    // 2. Метадані об'єкт
    size := flvio.LenAMF0Val(metadata)
    amfBuf := make([]byte, size)
    flvio.FillAMF0Val(amfBuf, metadata)
    buf.Write(amfBuf)
    
    return buf.Bytes(), nil
}
```

---

## 🔑 4. ParseAMF0Val() — парсинг бінарних даних у Go значення

### 🔧 Логіка парсингу:

```go
func parseAMF0Val(b []byte, offset int) (val interface{}, n int, err error) {
    // 1. Читання маркера типу
    if len(b) < n+1 {
        err = amf0ParseErr("marker", offset+n, err)
        return
    }
    marker := b[n]
    n++
    
    switch marker {
    case numbermarker:
        // Читання 8 байт float64
        if len(b) < n+8 {
            err = amf0ParseErr("number", offset+n, err)
            return
        }
        val = parseBEFloat64(b[n:])
        n += 8
    
    case stringmarker:
        // Читання довжини (2 байти) + даних
        if len(b) < n+2 {
            err = amf0ParseErr("string.length", offset+n, err)
            return
        }
        length := int(pio.U16BE(b[n:]))
        n += 2
        if len(b) < n+length {
            err = amf0ParseErr("string.body", offset+n, err)
            return
        }
        val = string(b[n : n+length])
        n += length
    
    case objectmarker:
        // Парсинг асоціативного масиву до end marker
        obj := AMFMap{}
        for {
            // Читання довжини ключа
            if len(b) < n+2 {
                err = amf0ParseErr("object.key.length", offset+n, err)
                return
            }
            length := int(pio.U16BE(b[n:]))
            n += 2
            if length == 0 {
                break  // кінець об'єкта
            }
            // Читання ключа
            if len(b) < n+length {
                err = amf0ParseErr("object.key.body", offset+n, err)
                return
            }
            okey := string(b[n : n+length])
            n += length
            // Рекурсивний парсинг значення
            var nval int
            var oval interface{}
            if oval, nval, err = parseAMF0Val(b[n:], offset+n); err != nil {
                err = amf0ParseErr("object.val", offset+n, err)
                return
            }
            n += nval
            obj[okey] = oval
        }
        // Пропуск end marker (0x000009)
        if len(b) < n+1 {
            err = amf0ParseErr("object.end", offset+n, err)
            return
        }
        n++
        val = obj
    
    case ecmaarraymarker:
        // Аналогічно object, але з 4-байтовим лічильником на початку
        // (який зазвичай ігнорується, бо ключі визначають розмір)
    
    case datemarker:
        // Читання timestamp у мілісекундах + timezone
        if len(b) < n+8+2 {
            err = amf0ParseErr("date", offset+n, err)
            return
        }
        ts := parseBEFloat64(b[n:])  // мілісекунди від epoch
        n += 8 + 2
        // Конвертація у time.Time
        val = time.Unix(int64(ts/1000), (int64(ts)%1000)*1000000)
    }
    return
}
```

### ✅ Ваш use-case: парсинг RTMP команд від клієнтів

```go
// ParseRTMPCommand — парсинг AMF0 команди з RTMP пакету
func ParseRTMPCommand(data []byte) (cmdName string, args flvio.AMFArray, err error) {
    // RTMP команда: [command name][transaction id][params...]
    
    // 1. Парсинг назви команди (рядок)
    val, n, err := flvio.ParseAMF0Val(data)
    if err != nil {
        return "", nil, fmt.Errorf("parse command name: %w", err)
    }
    cmdName, ok := val.(string)
    if !ok {
        return "", nil, fmt.Errorf("expected string command name, got %T", val)
    }
    
    // 2. Пропуск transaction id (число)
    _, n2, err := flvio.ParseAMF0Val(data[n:])
    if err != nil {
        return cmdName, nil, fmt.Errorf("parse transaction id: %w", err)
    }
    n += n2
    
    // 3. Парсинг аргументів (може бути ECMAArray або StrictArray)
    if n < len(data) {
        val, _, err := flvio.ParseAMF0Val(data[n:])
        if err != nil {
            return cmdName, nil, fmt.Errorf("parse args: %w", err)
        }
        switch v := val.(type) {
        case flvio.AMFArray:
            args = v
        case flvio.AMFECMAArray:
            // Конвертація асоціативного масиву у параметри
            args = flvio.AMFArray{v}
        }
    }
    
    return cmdName, args, nil
}

// Обробка команди "publish":
cmdName, args, err := ParseRTMPCommand(rtmpData)
if err != nil { /* handle error */ }

if cmdName == "publish" && len(args) >= 1 {
    streamName, _ := args[0].(string)
    log.Printf("Client wants to publish to: %s", streamName)
    // Дозволити або відхилити публікацію
}
```

---

## 🔑 5. AMF0ParseError — ланцюжок помилок парсингу

### Структура та призначення:

```go
type AMF0ParseError struct {
    Offset  int              // позиція у байтах, де сталася помилка
    Message string           // опис помилки
    Next    *AMF0ParseError  // попередня помилка у ланцюжку (для рекурсивного парсингу)
}

func (self *AMF0ParseError) Error() string {
    s := []string{}
    // Збір всіх помилок у ланцюжку від найновішої до найстарішої
    for p := self; p != nil; p = p.Next {
        s = append(s, fmt.Sprintf("%s:%d", p.Message, p.Offset))
    }
    return "amf0 parse error: " + strings.Join(s, ",")
}
```

### 🔍 Приклад ланцюжка помилок:

```
Вхід: пошкоджені дані об'єкта з неправильним ключем

Результат парсингу:
AMF0ParseError{
    Message: "object.key.body",
    Offset: 45,
    Next: &AMF0ParseError{
        Message: "object.val", 
        Offset: 32,
        Next: &AMF0ParseError{
            Message: "marker",
            Offset: 10,
        },
    },
}

Error() повертає:
"amf0 parse error: object.key.body:45,object.val:32,marker:10"
```

### ✅ Ваш use-case: детальне логування помилок парсингу

```go
// LogAMF0Error — форматування помилки для логування
func LogAMF0Error(err error, context string) {
    if amfErr, ok := err.(*flvio.AMF0ParseError); ok {
        log.Printf("[%s] AMF0 parse error chain:", context)
        for p := amfErr; p != nil; p = p.Next {
            log.Printf("  - %s at offset %d", p.Message, p.Offset)
        }
    } else {
        log.Printf("[%s] Error: %v", context, err)
    }
}

// Використання при парсингу RTMP:
val, n, err := flvio.ParseAMF0Val(data)
if err != nil {
    LogAMF0Error(err, "RTMP command parse")
    return fmt.Errorf("parse failed: %w", err)
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// rtmp_metadata_handler.go — обробка AMF0 метаданих для CCTV HLS Processor
type RTMPMetadataHandler struct {
    channelID    string
    onMetadata   func(flvio.AMFECMAArray) error  // callback для обробки метаданих
    metrics      *MetadataMetrics
}

// HandleOnMetaData — обробка "onMetaData" команди від RTMP клієнта
func (h *RTMPMetadataHandler) HandleOnMetaData(data []byte) error {
    start := time.Now()
    
    // 1. Парсинг AMF0 значення
    val, n, err := flvio.ParseAMF0Val(data)
    if err != nil {
        h.metrics.ParseErrors.Inc()
        return fmt.Errorf("parse AMF0: %w", err)
    }
    
    h.metrics.ParseLatency.Observe(time.Since(start).Seconds())
    h.metrics.BytesParsed.Add(float64(n))
    
    // 2. Перевірка типу (має бути ECMAArray)
    metadata, ok := val.(flvio.AMFECMAArray)
    if !ok {
        return fmt.Errorf("expected AMFECMAArray, got %T", val)
    }
    
    // 3. Логування ключових полів
    if duration, ok := metadata["duration"].(float64); ok {
        h.metrics.StreamDuration.Set(duration)
    }
    if width, ok := metadata["width"].(float64); ok {
        h.metrics.VideoWidth.Set(width)
    }
    if height, ok := metadata["height"].(float64); ok {
        h.metrics.VideoHeight.Set(height)
    }
    
    // 4. Валідація обов'язкових полів
    required := []string{"duration", "width", "height", "framerate"}
    for _, field := range required {
        if _, exists := metadata[field]; !exists {
            log.Printf("warning: missing required metadata field: %s", field)
            h.metrics.MissingFields.WithLabelValues(field).Inc()
        }
    }
    
    // 5. Виклик callback для подальшої обробки
    if h.onMetadata != nil {
        if err := h.onMetadata(metadata); err != nil {
            return fmt.Errorf("metadata callback: %w", err)
        }
    }
    
    h.metrics.MetadataProcessed.Inc()
    return nil
}

// GenerateConnectResponse — створення AMF0 відповіді на "connect" команду
func GenerateConnectResponse(appName string, capabilities flvio.AMFECMAArray) ([]byte, error) {
    response := flvio.AMFECMAArray{
        "fmsVer":       "FMS/3,0,1,123",
        "capabilities": 31,  // бітова маска можливостей
        "mode":         1,   // 1 = multi-user
        "data": flvio.AMFECMAArray{
            "version": "0.0.0.0",
            "level":   "status",
            "code":    "NetConnection.Connect.Success",
            "description": fmt.Sprintf("Connection to %s succeeded", appName),
            "objectEncoding": 0,  // AMF0
        },
    }
    
    // Додавання custom capabilities якщо потрібно
    for k, v := range capabilities {
        response[k] = v
    }
    
    // Серіалізація
    size := flvio.LenAMF0Val(response)
    buf := make([]byte, size)
    n := flvio.FillAMF0Val(buf, response)
    
    return buf[:n], nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"amf0 parse error: marker:XX"** | Неправильний або невідомий маркер | Переконайтеся, що дані дійсно у форматі AMF0; перевірте чи не змішані AMF0/AMF3 |
| **"string.body" помилка** | Довжина рядка не співпадає з доступними даними | Перевірте цілісність вхідного потоку; можливе обрізання пакету при мережевих помилках |
| **"object.end" помилка** | Відсутній маркер кінця об'єкта (0x000009) | Переконайтеся, що серіалізація коректно додає end marker; перевірте парсинг рекурсивних структур |
| **Паніка при access out of bounds** | Недостатня перевірка `len(b)` перед доступом | Завжди перевіряйте `len(b) >= n+required` перед читанням; використовуйте існуючі перевірки у `ParseAMF0Val` |
| **Неправильний timestamp у Date** | Конвертація мілісекунд → time.Time дає зсув | Переконайтеся, що використовуєте `time.Unix(sec, nsec)` коректно; пам'ятайте про timezone offset (зазвичай 0) |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування розмірів для часто використовуваних структур:

```go
type AMF0SizeCache struct {
    mu    sync.RWMutex
    cache map[string]int  // hash(struct) → size
}

func (c *AMF0SizeCache) Get(val interface{}) (int, bool) {
    // Простий хеш для ключа (на практиці використовуйте proper hashing)
    key := fmt.Sprintf("%T:%v", val, val)
    
    c.mu.RLock()
    size, ok := c.cache[key]
    c.mu.RUnlock()
    
    if ok {
        return size, true
    }
    
    // Обчислення якщо не в кеші
    size = flvio.LenAMF0Val(val)
    
    c.mu.Lock()
    if c.cache == nil {
        c.cache = make(map[string]int)
    }
    c.cache[key] = size
    c.mu.Unlock()
    
    return size, true
}
```

### 2. Пакетний парсинг для зменшення накладних витрат:

```go
// ParseAMF0Batch — парсинг кількох AMF0 значень з одного буфера
func ParseAMF0Batch(data []byte, count int) ([]interface{}, error) {
    results := make([]interface{}, 0, count)
    offset := 0
    
    for i := 0; i < count; i++ {
        val, n, err := flvio.ParseAMF0Val(data[offset:])
        if err != nil {
            return results, fmt.Errorf("parse value %d: %w", i, err)
        }
        results = append(results, val)
        offset += n
    }
    return results, nil
}
```

### 3. Моніторинг продуктивності парсингу:

```go
type AMF0Metrics struct {
    ParseLatency   prometheus.HistogramVec
    SerializeLatency prometheus.HistogramVec
    BytesProcessed prometheus.CounterVec
    ParseErrors    prometheus.CounterVec
}

func (m *AMF0Metrics) RecordParse(bytes int, duration time.Duration, channelID string, err error) {
    if err != nil {
        m.ParseErrors.WithLabelValues(channelID).Inc()
        return
    }
    m.ParseLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    m.BytesProcessed.WithLabelValues(channelID).Add(float64(bytes))
}
```

---

## 📋 Чек-лист інтеграції flvio

```go
// ✅ 1. Парсинг з перевіркою типу
val, n, err := flvio.ParseAMF0Val(data)
if err != nil {
    return fmt.Errorf("parse: %w", err)
}
metadata, ok := val.(flvio.AMFECMAArray)
if !ok {
    return fmt.Errorf("expected AMFECMAArray, got %T", val)
}

// ✅ 2. Серіалізація з попереднім виділенням буфера
size := flvio.LenAMF0Val(response)
buf := make([]byte, size)
n := flvio.FillAMF0Val(buf, response)
// Використання buf[:n]

// ✅ 3. Обробка помилок з деталями
if amfErr, ok := err.(*flvio.AMF0ParseError); ok {
    for p := amfErr; p != nil; p = p.Next {
        log.Printf("AMF0 error: %s at offset %d", p.Message, p.Offset)
    }
}

// ✅ 4. Валідація обов'язкових полів у метаданих
required := []string{"duration", "width", "height"}
for _, field := range required {
    if _, exists := metadata[field]; !exists {
        log.Printf("warning: missing %s", field)
    }
}

// ✅ 5. Конвертація типів з перевіркою
if fps, ok := metadata["framerate"].(float64); ok {
    // Використання fps
} else {
    log.Printf("warning: framerate is not float64")
}

// ✅ 6. Метрики для моніторингу
metrics.RecordParse(len(data), time.Since(start), channelID, err)
```

---

## 🔗 Корисні посилання

- 💻 [vdk flvio Package](https://pkg.go.dev/github.com/deepch/vdk/format/flvio) — GoDoc documentation
- 📄 [AMF0 Specification (Adobe)](https://www.adobe.com/content/dam/acom/en/devnet/pdf/amf0-file-format-specification.pdf) — офіційна специфікація
- 📄 [RTMP Specification](https://www.adobe.com/devnet/rtmp.html) — використання AMF0 у RTMP
- 📄 [FLV File Format](https://download.macromedia.com/f4v/video_file_format_spec_v10_1.pdf) — структура FLV з AMF0 метаданими
- 🧪 [Go encoding/binary Documentation](https://pkg.go.dev/encoding/binary) — робота з бітовими даними

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **RTMP/FLV потоками у CCTV**:
> 1. **Завжди перевіряйте типи після парсингу** — AMF0 дозволяє різні типи для одного поля; помилка типу може зламати обробку.
> 2. **Використовуйте `LenAMF0Val()` для попереднього виділення буфера** — це значно зменшує аллокації пам'яті при серіалізації.
> 3. **Логууйте ланцюжок `AMF0ParseError`** — це допомагає швидко знайти корінь проблеми у складних вкладених структурах.
> 4. **Валідуйте обов'язкові поля метаданих** — відсутність `width`/`height`/`duration` може зламати HLS-генератор.
> 5. **Тестуйте з різними клієнтами** — OBS, FFmpeg, власні клієнти можуть надсилати AMF0 у трохи різних форматах.

Потрібен приклад інтеграції `RTMPMetadataHandler` з вашим `pubsub.Queue` для розподілу метаданих між підписниками (транскодер, HLS-генератор, архів)? Готовий допомогти! 🚀