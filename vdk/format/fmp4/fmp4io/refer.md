# 📦 Глибокий розбір: `fmp4io.DataRefer`, `HandlerRefer` — Посилання на дані та обробники треків у fMP4

Цей файл — **реалізація атомів `dref` (Data Reference), `url ` (Data Reference URL), та `hdlr` (Handler Reference)** для опису посилань на медіа-дані та типів обробників треків у форматі Fragmented MP4 (fMP4). Ці атоми критичні для визначення де знаходяться дані та як їх інтерпретувати.

---

## 🗺️ Архітектурна схема DataRefer та HandlerRefer

```
┌────────────────────────────────────────┐
│ 📦 fmp4io — Data & Handler References │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • DataRefer (dref) — посилання на дані│
│  • DataReferUrl (url ) — URL посилання │
│  • HandlerRefer (hdlr) — тип обробника │
│                                         │
│  🔄 Ієрархія атомів:                    │
│  dinf (DataInfo)                       │
│  └─ dref (DataRefer)                   │
│      └─ url  (DataReferUrl) — self-ref│
│                                         │
│  mdia (Media)                          │
│  └─ hdlr (HandlerRefer) — 'vide'/'soun'│
│                                         │
│  📡 Використання:                       │
│  • Визначення локації медіа-даних     │
│  • Визначення типу треку (відео/аудіо)│
│  • Підтримка зовнішніх джерел даних   │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. DataRefer (dref) — посилання на медіа-дані

### 🔧 Структура та призначення:

```go
type DataRefer struct {
    Version uint8          // версія формату (зазвичай 0)
    Flags   uint32         // бітові прапорці (0x000001 = self-reference)
    Url     *DataReferUrl  // ⭐ посилання на дані (зазвичай self-reference)
    AtomPos                // offset/size у файлі
}
```

### 🔍 Призначення dref атому:

```
dref (Data Reference) містить посилання на медіа-дані:

• Зазвичай містить один url  атом для self-reference
• Дозволяє посилатися на зовнішні джерела даних (рідко використовується)
• Критичний для розподілених медіа-систем

Структура:
  dref (DataRefer)
  ├─ Version: 0
  ├─ Flags: 0x000001 (self-reference) ⭐
  ├─ EntryCount: 1 (кількість посилань)
  └─ url  (DataReferUrl) — порожній атом для self-reference
     ├─ Version: 0
     ├─ Flags: 0x000001 (self-reference)
```

### 🔍 Прапорці self-reference:

```
Flags бітова маска для dref та url :
• 0x000001 — self-reference (дані у тому ж файлі) ⭐
• 0x000002 — external reference (дані у зовнішньому файлі)

Для локальних файлів завжди використовується 0x000001.
```

### ✅ Ваш use-case**: перевірка self-reference

```go
// IsDataSelfReferenced — перевірка чи дані знаходяться у тому ж файлі
func IsDataSelfReferenced(dref *fmp4io.DataRefer) bool {
    if dref == nil || dref.Url == nil {
        return false
    }
    
    // Перевірка прапорця self-reference у dref та url 
    return (dref.Flags&0x000001 != 0) && (dref.Url.Flags&0x000001 != 0)
}

// Використання:
if !IsDataSelfReferenced(track.Media.Info.Data.Refer) {
    log.Printf("warning: track data may be external")
    // Може знадобитися додаткова обробка для зовнішніх даних
}
```

---

## 🔑 2. DataReferUrl (url ) — URL посилання на дані

### 🔧 Структура та призначення:

```go
type DataReferUrl struct {
    Version uint8  // версія формату (зазвичай 0)
    Flags   uint32 // бітові прапорці (0x000001 = self-reference)
    AtomPos        // offset/size у файлі
}
```

### 🔍 Призначення url  атому:

```
url  (Data Reference URL) визначає локацію даних:

• Для self-reference: порожній атом з прапорцем 0x000001
• Для external reference: містить URL рядок (не підтримується у цій реалізації)

Структура для self-reference:
  url  (DataReferUrl)
  ├─ Version: 0
  ├─ Flags: 0x000001 (self-reference) ⭐
  └─ (порожній вміст)

⚠️ Поточна реалізація підтримує ТОЛЬКИ self-reference!
```

### ⚠️ Критична проблема: обмежена підтримка external reference

```
У поточній реалізації:
    type DataReferUrl struct {
        Version uint8
        Flags   uint32
        // ⚠️ Немає поля для URL рядка!
    }

Проблема:
• Неможливість посилання на зовнішні джерела даних
• Обмеження для розподілених медіа-систем
• Файли з external reference не зможуть бути прочитані

✅ Виправлення: додавання підтримки URL рядка
    type DataReferUrl struct {
        Version uint8
        Flags   uint32
        Location string  // ⭐ URL для external reference
        AtomPos
    }
    
    // У Marshal/Unmarshal: обробка Location якщо Flags&0x000002 != 0
```

---

## 🔑 3. HandlerRefer (hdlr) — тип обробника треку

### 🔧 Структура та призначення:

```go
type HandlerRefer struct {
    Version    uint8       // версія формату (зазвичай 0)
    Flags      uint32      // бітові прапорці (зазвичай 0)
    Predefined uint32      // зарезервоване поле (зазвичай 0)
    Type       uint32      // ⭐ тип обробника ('vide'/'soun'/'subt')
    Reserved   [3]uint32   // зарезервовані поля
    Name       string      // ⭐ людино-читабельна назва обробника
    AtomPos                // offset/size у файлі
}
```

### 🔍 Призначення hdlr атому:

```
hdlr (Handler Reference) визначає тип та призначення треку:

• Type поле містить fourcc код типу треку:
  - 'vide' (0x76696465) = відео трек ⭐
  - 'soun' (0x736f756e) = аудіо трек ⭐
  - 'subt' = субтитри
  - 'text' = текстові дані
  - 'hint' = hint track для streaming

• Name поле містить людино-читабельну назву (напр. "VideoHandler")

Структура:
  hdlr (HandlerRefer)
  ├─ Version: 0
  ├─ Flags: 0
  ├─ Predefined: 0
  ├─ Type: 'vide'/'soun' тощо ⭐
  ├─ Reserved: [0, 0, 0]
  └─ Name: "Video Media Handler\x00" ⭐
```

### 🔍 Константи типів обробників:

```go
const (
    VideoHandler = 0x76696465 // 'vide' — відео трек ⭐
    SoundHandler = 0x736f756e // 'soun' — аудіо трек ⭐
)
```

### ✅ Ваш use-case**: визначення типу треку

```go
// GetTrackTypeFromHandler — визначення типу треку за hdlr атомом
func GetTrackTypeFromHandler(hdlr *fmp4io.HandlerRefer) TrackType {
    if hdlr == nil {
        return TrackTypeUnknown
    }
    
    switch hdlr.Type {
    case fmp4io.VideoHandler:  // 'vide'
        return TrackTypeVideo
    case fmp4io.SoundHandler:  // 'soun'
        return TrackTypeAudio
    case 0x73756274:  // 'subt'
        return TrackTypeSubtitle
    case 0x74657874:  // 'text'
        return TrackTypeText
    case 0x68696E74:  // 'hint'
        return TrackTypeHint
    default:
        return TrackTypeUnknown
    }
}

type TrackType int
const (
    TrackTypeUnknown TrackType = iota
    TrackTypeVideo
    TrackTypeAudio
    TrackTypeSubtitle
    TrackTypeText
    TrackTypeHint
)

// Використання:
for _, track := range moov.Tracks {
    if track.Media != nil && track.Media.Handler != nil {
        trackType := GetTrackTypeFromHandler(track.Media.Handler)
        log.Printf("Track %d: type=%v, name=%q", 
            track.Header.TrackID, trackType, track.Media.Handler.Name)
    }
}
```

---

## 🔑 4. Marshal/Unmarshal — серіалізація атомів

### 🔧 Основна логіка Marshal для DataRefer:

```go
func (a DataRefer) marshal(b []byte) (n int) {
    // 1. Запис заголовку FullAtom (version+flags)
    pio.PutU8(b[n:], a.Version); n += 1
    pio.PutU24BE(b[n:], a.Flags); n += 3
    
    // 2. Запис кількості дочірніх атомів (entry count)
    _childrenNR := 0
    if a.Url != nil {
        _childrenNR++
    }
    pio.PutI32BE(b[n:], int32(_childrenNR)); n += 4
    
    // 3. Рекурсивна серіалізація дочірніх атомів
    if a.Url != nil {
        n += a.Url.Marshal(b[n:])  // url  атом
    }
    return
}
```

### 🔧 Основна логіка Unmarshal для HandlerRefer:

```go
func (a *HandlerRefer) Unmarshal(b []byte, offset int) (n int, err error) {
    (&a.AtomPos).setPos(offset, len(b))
    n += 8  // пропуск заголовку атому (size+tag)
    
    // Читання FullAtom заголовку
    if len(b) < n+1 { err = parseErr("Version", n+offset, err); return }
    a.Version = pio.U8(b[n:]); n += 1
    if len(b) < n+3 { err = parseErr("Flags", n+offset, err); return }
    a.Flags = pio.U24BE(b[n:]); n += 3
    
    // Читання основних полів
    if len(b) < n+4 { err = parseErr("Predefined", n+offset, err); return }
    a.Predefined = pio.U32BE(b[n:]); n += 4
    if len(b) < n+4 { err = parseErr("Type", n+offset, err); return }
    a.Type = pio.U32BE(b[n:]); n += 4
    
    // Пропуск зарезервованих полів
    n += 3 * 4
    
    // Читання Name рядка (null-terminated string)
    i := bytes.IndexByte(b[n:], 0)  // пошук нуль-термінатора
    if i > 0 {
        a.Name = string(b[n : n+i])  // копіювання до нуль-байта
        n += i + 1  // пропуск нуль-байта
    }
    return
}
```

### ⚠️ Критична проблема: обробка Name рядка

```
У поточному коді:
    i := bytes.IndexByte(b[n:], 0)
    if i > 0 {
        a.Name = string(b[n : n+i])
        n += i + 1
    }

Проблема:
• Якщо Name не null-terminated → IndexByte поверне -1 → Name залишиться порожнім
• Якщо рядок містить нуль-байт всередині → обрізання на першому нулі
• Неможливість обробки Unicode назв (тільки ASCII)

✅ Виправлення: валідація та обробка крайніх випадків
    // Пошук нуль-термінатора з обмеженням
    maxLen := len(b) - n
    if maxLen > 256 { maxLen = 256 }  // обмеження довжини назви
    
    i := bytes.IndexByte(b[n:n+maxLen], 0)
    if i >= 0 {
        a.Name = string(b[n : n+i])
        n += i + 1
    } else {
        // Якщо нуль-термінатор не знайдено — читаємо до кінця або обмеження
        a.Name = string(b[n : n+maxLen])
        n += maxLen
        log.Printf("warning: handler name not null-terminated: %q", a.Name)
    }
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Створення повного Media атому з dref та hdlr

```go
// CreateMediaWithReferences — генерація Media атому з посиланнями та обробником
func CreateMediaWithReferences(trackType TrackType, name string) (*fmp4io.Media, error) {
    // 1. Створення DataRefer з self-reference
    dref := &fmp4io.DataRefer{
        Version: 0,
        Flags:   0x000001,  // self-reference
        Url: &fmp4io.DataReferUrl{
            Version: 0,
            Flags:   0x000001,  // self-reference
        },
    }
    
    dinf := &fmp4io.DataInfo{
        Refer: dref,
    }
    
    // 2. Створення HandlerRefer за типом треку
    var handlerType uint32
    var handlerName string
    
    switch trackType {
    case TrackTypeVideo:
        handlerType = fmp4io.VideoHandler  // 'vide'
        handlerName = "Video Media Handler"
    case TrackTypeAudio:
        handlerType = fmp4io.SoundHandler  // 'soun'
        handlerName = "Sound Media Handler"
    case TrackTypeSubtitle:
        handlerType = 0x73756274  // 'subt'
        handlerName = "Subtitle Handler"
    default:
        return nil, fmt.Errorf("unsupported track type: %v", trackType)
    }
    
    hdlr := &fmp4io.HandlerRefer{
        Version:    0,
        Flags:      0,
        Predefined: 0,
        Type:       handlerType,
        Reserved:   [3]uint32{0, 0, 0},
        Name:       handlerName + "\x00",  // null-terminated string
    }
    
    // 3. Створення MediaInfo (спрощено — без SampleTable)
    minf := &fmp4io.MediaInfo{
        Data: dinf,
        // Sample: stbl,  // буде додано окремо
    }
    
    // 4. Створення MediaHeader
    now := time.Now()
    mdhd := &fmp4io.MediaHeader{
        Version:   0,
        Flags:     0,
        CreateTime: now,
        ModifyTime: now,
        TimeScale: 90000,  // 90kHz для відео
        Duration:  0,      // буде оновлено
        Language:  21956,  // 'und' = undefined
        Quality:   0,
    }
    
    // 5. Об'єднання у Media
    media := &fmp4io.Media{
        Header:  mdhd,
        Handler: hdlr,
        Info:    minf,
    }
    
    return media, nil
}

// Використання:
videoMedia, err := CreateMediaWithReferences(TrackTypeVideo, "Main Video")
if err != nil { /* handle error */ }

audioMedia, err := CreateMediaWithReferences(TrackTypeAudio, "Main Audio")
if err != nil { /* handle error */ }
```

### 🔧 Приклад: Валідація посилань на дані

```go
// ValidateDataReferences — перевірка коректності посилань на дані у треках
func ValidateDataReferences(moov *fmp4io.Movie) error {
    for i, track := range moov.Tracks {
        if track.Media == nil || track.Media.Info == nil {
            return fmt.Errorf("track %d: missing media info", i)
        }
        
        dinf := track.Media.Info.Data
        if dinf == nil || dinf.Refer == nil {
            return fmt.Errorf("track %d: missing data reference", i)
        }
        
        dref := dinf.Refer
        
        // Перевірка self-reference
        if !IsDataSelfReferenced(dref) {
            return fmt.Errorf("track %d: data reference is not self-referenced", i)
        }
        
        // Перевірка наявності url  атому
        if dref.Url == nil {
            return fmt.Errorf("track %d: missing url  atom in dref", i)
        }
        
        // Перевірка прапорців url 
        if dref.Url.Flags&0x000001 == 0 {
            return fmt.Errorf("track %d: url  atom not marked as self-reference", i)
        }
    }
    
    return nil
}

// Використання:
if err := ValidateDataReferences(moov); err != nil {
    log.Printf("warning: data reference validation failed: %v", err)
    // Можна спробувати відновитися або використати дефолтні значення
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка при читанні 3-байтових полів** | Доступ за межами буфера у Unmarshal | Додайте перевірку `if len(b) < n+3` перед `pio.U24BE()` |
| **Некоректна обробка Name рядка** | Порожня назва обробника або обрізання | Валідуйте наявність нуль-термінатора та обмежуйте довжину |
| **Відсутній url  атом у dref** | Неможливість знайти дані треку | Перевіряйте `if dref.Url != nil` перед використанням |
| **Невірний Type у hdlr** | Трек не розпізнається як відео/аудіо | Переконайтеся що Type = 'vide'/'soun' у big-endian форматі |
| **External reference не підтримується** | Помилка при читанні файлів з зовнішніми даними | Документуйте обмеження або реалізуйте підтримку URL рядка |

---

## ⚡ Оптимізації для high-performance обробки

### 1. Кешування серіалізованих hdlr атомів:

```go
var handlerCache = sync.Map{}  // map[uint32][]byte

func GetCachedHandler(typeCode uint32) ([]byte, error) {
    if cached, ok := handlerCache.Load(typeCode); ok {
        return cached.([]byte), nil
    }
    
    // Генерація hdlr (спрощено)
    hdlr := createHandler(typeCode)  // helper function
    blob := make([]byte, hdlr.Len())
    hdlr.Marshal(blob)
    
    handlerCache.Store(typeCode, blob)
    return blob, nil
}
```

### 2. Попередня аллокація буферів для Marshal:

```go
// PreallocateDataReferBuffer — виділення місця для серіалізації заздалегідь
func PreallocateDataReferBuffer(dref *fmp4io.DataRefer) []byte {
    estimatedSize := dref.Len()
    buf := make([]byte, estimatedSize)
    return buf
}

// Використання:
buf := PreallocateDataReferBuffer(dref)
n := dref.Marshal(buf)
result := buf[:n]  // обрізання до фактичного розміру
```

### 3. Моніторинг продуктивності парсингу:

```go
type ReferenceMetrics struct {
    ReferencesParsed prometheus.CounterVec
    ParseLatency     prometheus.HistogramVec
    HandlerTypes     prometheus.CounterVec
    ParseErrors      prometheus.CounterVec
}

func (m *ReferenceMetrics) RecordReference(handlerType uint32, duration time.Duration, err error) {
    m.ReferencesParsed.Inc()
    m.ParseLatency.Observe(duration.Seconds())
    if handlerType != 0 {
        m.HandlerTypes.WithLabelValues(fmt.Sprintf("0x%08X", handlerType)).Inc()
    }
    if err != nil {
        m.ParseErrors.Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання DataRefer/HandlerRefer

```go
// ✅ 1. Перевірка меж буфера перед читанням 3-байтових полів
if len(b) < n+3 {
    err = parseErr("Flags", n+offset, err)
    return
}
a.Flags = pio.U24BE(b[n:])
n += 3

// ✅ 2. Валідація self-reference прапорців
if (dref.Flags&0x000001) == 0 || (dref.Url.Flags&0x000001) == 0 {
    return fmt.Errorf("data reference is not self-referenced")
}

// ✅ 3. Безпечна обробка Name рядка з null-термінатором
i := bytes.IndexByte(b[n:], 0)
if i >= 0 {
    a.Name = string(b[n : n+i])
    n += i + 1
} else {
    // Обробка випадку без нуль-термінатора
    maxLen := min(len(b)-n, 256)
    a.Name = string(b[n : n+maxLen])
    n += maxLen
    log.Printf("warning: handler name not null-terminated")
}

// ✅ 4. Перевірка типу обробника перед використанням
switch hdlr.Type {
case fmp4io.VideoHandler:  // 'vide'
    // обробка відео треку
case fmp4io.SoundHandler:  // 'soun'
    // обробка аудіо треку
default:
    return fmt.Errorf("unsupported handler type: 0x%08X", hdlr.Type)
}

// ✅ 5. Логування з контекстом для дебагу
log.Printf("Parsed hdlr: type=%s (%08X), name=%q", 
    string(rune(hdlr.Type>>24), rune(hdlr.Type>>16), rune(hdlr.Type>>8), rune(hdlr.Type)),
    hdlr.Type, hdlr.Name)

// ✅ 6. Метрики для моніторингу
metrics.RecordReference(hdlr.Type, time.Since(start), err)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [MP4 Handler Reference Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25688) — Apple documentation про hdlr атом
- 📄 [Data Reference Atom Format](https://wiki.multimedia.cx/index.php/MP4#dref) — детальний опис dref/url  атомів
- 🧪 [Go bytes Package Documentation](https://pkg.go.dev/bytes) — робота з байтовими масивами у Go
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте межі буфера перед `pio.U24BE()`** — уникнення панік при пошкоджених файлах.
> 2. **Валідуйте self-reference прапорці** — забезпечення коректної локації медіа-даних.
> 3. **Безпечно обробляйте Name рядок з null-термінатором** — уникнення невірних назв обробників.
> 4. **Перевіряйте тип обробника перед використанням** — уникнення некоректної інтерпретації треків.
> 5. **Документуйте обмеження підтримки external reference** — уникнення плутанини при роботі з розподіленими даними.

Потрібен приклад реалізації повного циклу створення/парсингу Media атому з підтримкою різних типів треків, або інтеграція `fmp4io.HandlerRefer` з вашим `mse.Muxer` для стрімінгу через WebSocket? Готовий допомогти! 🚀