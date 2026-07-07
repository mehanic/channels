# 📦 Глибокий розбір: `fmp4io.ParseError` — Система обробки помилок для fMP4

Цей файл — **реалізація структурованої системи помилок** для парсингу фрагментованого MP4 (fMP4). Він надає механізми для відстеження позиції помилок у файлі, побудови ланцюжка контексту та збереження оригінальних помилок низького рівня.

---

## 🗺️ Архітектурна схема ParseError

```
┌────────────────────────────────────────┐
│ 📦 fmp4io.ParseError — Error System   │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • ParseError struct — деталі помилки  │
│  • Error() string — форматування       │
│  • parseErr() helper — створення ланцюжка│
│                                         │
│  🔄 Ланцюжок помилок:                  │
│  ParseError{                           │
│    Debug: "TagSizeInvalid",            │
│    Offset: 256,                        │
│    prev: &ParseError{                  │
│      Debug: "moof",                    │
│      Offset: 200,                      │
│      prev: &ParseError{                │
│        Debug: "trun",                  │
│        Offset: 220,                    │
│        orig: io.ErrUnexpectedEOF       │
│      }                                 │
│    }                                   │
│  }                                     │
│                                         │
│  📡 Вивід:                             │
│  "mp4io: parse error: trun:220,moof:200,TagSizeInvalid:256,unexpected EOF"│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. ParseError struct — структура деталізованої помилки

### 🔧 Поля та їх призначення:

```go
type ParseError struct {
    Debug  string      // опис помилки (напр. "TagSizeInvalid", "moof")
    Offset int         // позиція у файлі де сталася помилка
    prev   *ParseError // попередня помилка у ланцюжку (для контексту)
    orig   error       // оригінальна помилка низького рівня (напр. io.EOF)
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `Debug` | `string` | Людино-читабельний опис помилки | `"TagSizeInvalid"`, `"avcC"`, `"moof"` |
| `Offset` | `int` | Позиція у файлі (байти від початку) | `256` = помилка на 256-му байті |
| `prev` | `*ParseError` | Посилання на попередню помилку для контексту | Ланцюжок: trun → moof → root |
| `orig` | `error` | Оригінальна помилка низького рівня | `io.ErrUnexpectedEOF`, `io.EOF` |

### ✅ Ваш use-case**: логування з контекстом

```go
// LogParseError — форматування помилки для логування
func LogParseError(err error, filename string) {
    if pe, ok := err.(*fmp4io.ParseError); ok {
        log.Printf("File %s: parse error chain:", filename)
        for p := pe; p != nil; p = p.prev {
            log.Printf("  • %s at offset %d (0x%X)", p.Debug, p.Offset, p.Offset)
        }
        if pe.orig != nil {
            log.Printf("  • Original error: %v", pe.orig)
        }
    } else {
        log.Printf("File %s: error: %v", filename, err)
    }
}

// Використання:
atoms, err := fmp4io.ReadFileAtoms(file)
if err != nil {
    LogParseError(err, "video.mp4")
    return err
}
```

---

## 🔑 2. Error() — форматування помилки у рядок

### 🔧 Реалізація:

```go
func (a *ParseError) Error() string {
    s := []string{}
    for p := a; p != nil; p = p.prev {
        s = append(s, fmt.Sprintf("%s:%d", p.Debug, p.Offset))
        if p.prev == nil && p.orig != nil {
            s = append(s, p.orig.Error())
        }
    }
    return "mp4io: parse error: " + strings.Join(s, ",")
}
```

### 🔍 Як працює:

```
Приклад ланцюжка:
    ParseError{
        Debug: "TagSizeInvalid", Offset: 256,
        prev: &ParseError{
            Debug: "moof", Offset: 200,
            prev: &ParseError{
                Debug: "trun", Offset: 220,
                orig: io.ErrUnexpectedEOF
            }
        }
    }

Форматування:
1. Ітерація 1 (p = root):
   • s = ["TagSizeInvalid:256"]
   • p.prev != nil → не додаємо orig
2. Ітерація 2 (p = prev):
   • s = ["TagSizeInvalid:256", "moof:200"]
3. Ітерація 3 (p = prev.prev):
   • s = ["TagSizeInvalid:256", "moof:200", "trun:220"]
   • p.prev == nil && p.orig != nil → додаємо orig
   • s = [..., "unexpected EOF"]
4. Результат:
   "mp4io: parse error: TagSizeInvalid:256,moof:200,trun:220,unexpected EOF"
```

### ⚠️ Критична проблема: можливий нескінченний цикл

```
У поточному коді:
    for p := a; p != nil; p = p.prev { ... }

Проблема:
• Якщо у ланцюжку є циклічне посилання (p.prev == p або цикл) → нескінченний цикл
• Це може статися при помилковому створенні ParseError

✅ Виправлення: обмеження глибини ланцюжка
    func (a *ParseError) Error() string {
        s := []string{}
        depth := 0
        const maxDepth = 10  // запобігання нескінченним циклам
        
        for p := a; p != nil && depth < maxDepth; p = p.prev {
            s = append(s, fmt.Sprintf("%s:%d", p.Debug, p.Offset))
            if p.prev == nil && p.orig != nil {
                s = append(s, p.orig.Error())
            }
            depth++
        }
        if depth >= maxDepth {
            s = append(s, "...(truncated)")
        }
        return "mp4io: parse error: " + strings.Join(s, ",")
    }
```

### ✅ Ваш use-case**: аналіз помилок у production

```go
// AnalyzeParseError — класифікація помилок для моніторингу
func AnalyzeParseError(err error) ErrorCategory {
    pe, ok := err.(*fmp4io.ParseError)
    if !ok {
        return CategoryUnknown
    }
    
    // Аналіз останньої помилки у ланцюжку (найбільш специфічна)
    last := pe
    for last.prev != nil {
        last = last.prev
    }
    
    switch last.Debug {
    case "TagSizeInvalid":
        return CategoryCorruptedFile
    case "avcC", "esds":
        return CategoryInvalidCodecConfig
    case "moof", "traf", "trun":
        return CategoryFragmentError
    default:
        if last.orig == io.ErrUnexpectedEOF {
            return CategoryTruncatedFile
        }
        return CategoryUnknown
    }
}

type ErrorCategory int

const (
    CategoryUnknown ErrorCategory = iota
    CategoryCorruptedFile
    CategoryInvalidCodecConfig
    CategoryFragmentError
    CategoryTruncatedFile
)

// Використання у метриках:
category := AnalyzeParseError(err)
metrics.ParseErrors.WithLabelValues(category.String()).Inc()
```

---

## 🔑 3. parseErr() — helper для створення ланцюжка помилок

### 🔧 Реалізація:

```go
func parseErr(debug string, offset int, prev error) (err error) {
    _prev, _ := prev.(*ParseError)
    if _prev != nil {
        prev = nil  // ⚠️ Скидання orig якщо prev вже ParseError
    }
    return &ParseError{
        Debug:  debug,
        Offset: offset,
        prev:   _prev,
        orig:   prev,  // orig = nil якщо prev був ParseError
    }
}
```

### 🔍 Логіка обробки prev/orig:

```
Призначення:
• Якщо prev вже є *ParseError → використовуємо його як prev, orig = nil
• Якщо prev звичайний error → зберігаємо як orig, prev = nil

Приклади:

1. parseErr("moof", 200, io.ErrUnexpectedEOF):
   • _prev = nil (io.ErrUnexpectedEOF не *ParseError)
   • Результат: ParseError{Debug:"moof", Offset:200, prev:nil, orig:io.ErrUnexpectedEOF}

2. parseErr("trun", 220, parseErr("moof", 200, nil)):
   • _prev = &ParseError{Debug:"moof", ...}
   • prev = nil (скидаємо бо _prev != nil)
   • Результат: ParseError{Debug:"trun", Offset:220, prev:&ParseError{...}, orig:nil}

3. parseErr("TagSizeInvalid", 256, parseErr("trun", 220, io.ErrUnexpectedEOF)):
   • _prev = &ParseError{Debug:"trun", orig:io.ErrUnexpectedEOF}
   • prev = nil
   • Результат: ParseError{Debug:"TagSizeInvalid", Offset:256, prev:&ParseError{...}, orig:nil}
   • При форматуванні: ... → trun:220 → (orig: unexpected EOF)
```

### ⚠️ Критична проблема: втрата оригінальної помилки

```
У поточній логіці:
    if _prev != nil {
        prev = nil  // ← Скидаємо orig якщо prev вже ParseError!
    }

Проблема:
• Якщо виклик: parseErr("new", offset, parseErr("old", offset2, originalErr))
• originalErr буде втрачено бо prev = nil
• Це може призвести до втрати важливої інформації про низькорівневу помилку

✅ Виправлення: збереження orig через ланцюжок
    func parseErr(debug string, offset int, prev error) (err error) {
        _prev, _ := prev.(*ParseError)
        
        var orig error
        if _prev != nil {
            // Якщо prev вже ParseError, беремо його orig
            orig = _prev.orig
        } else {
            // Інакше prev є оригінальною помилкою
            orig = prev
        }
        
        return &ParseError{
            Debug:  debug,
            Offset: offset,
            prev:   _prev,
            orig:   orig,  // Завжди зберігаємо оригінальну помилку
        }
    }
```

### ✅ Ваш use-case**: створення контекстних помилок

```go
// parseWithContext — helper для додавання контексту до помилок
func parseWithContext(debug string, offset int, err error) error {
    if err == nil {
        return nil
    }
    return parseErr(debug, offset, err)
}

// Приклад використання у парсингу:
func parseAtom(b []byte, offset int) (Atom, error) {
    if len(b) < 8 {
        return nil, parseWithContext("atomHeader", offset, io.ErrUnexpectedEOF)
    }
    
    size := pio.U32BE(b[0:])
    tag := Tag(pio.U32BE(b[4:]))
    
    if size < 8 {
        return nil, parseWithContext("atomSize", offset, fmt.Errorf("invalid size: %d", size))
    }
    
    if len(b) < int(size) {
        return nil, parseWithContext("atomData", offset+8, io.ErrUnexpectedEOF)
    }
    
    // ... парсинг атому ...
    return atom, nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Обробка помилок при парсингу fMP4

```go
// ParseFMP4WithRecovery — парсинг з можливістю відновлення після помилок
func ParseFMP4WithRecovery(r io.ReadSeeker, callback func(Atom) error) error {
    for {
        offset, _ := r.Seek(0, 1)
        taghdr := make([]byte, 8)
        if _, err := io.ReadFull(r, taghdr); err != nil {
            if err == io.EOF {
                return nil  // нормальне завершення
            }
            return parseErr("readHeader", int(offset), err)
        }
        
        size := pio.U32BE(taghdr[0:])
        tag := Tag(pio.U32BE(taghdr[4:]))
        
        // Валідація розміру
        if size < 8 {
            return parseErr("invalidSize", int(offset), fmt.Errorf("size=%d", size))
        }
        
        // Читання даних атому
        data := make([]byte, size)
        copy(data, taghdr)
        if _, err := io.ReadFull(r, data[8:]); err != nil {
            return parseErr("readData", int(offset)+8, err)
        }
        
        // Парсинг атому
        var atom Atom
        switch tag {
        case MOOF: atom = &MovieFrag{}
        case SIDX: atom = &SegmentIndex{}
        // ... інші атоми ...
        default:
            // Невідомий атом: пропускаємо
            if _, err := r.Seek(int64(size)-8, 1); err != nil {
                return parseErr("skipUnknown", int(offset), err)
            }
            continue
        }
        
        if _, err := atom.Unmarshal(data, int(offset)); err != nil {
            // Логування помилки з контекстом
            LogParseError(err, "stream")
            
            // Спроба відновлення: пропуск атому
            if _, seekErr := r.Seek(int64(size)-8, 1); seekErr != nil {
                return parseErr("recoverSeek", int(offset), seekErr)
            }
            continue
        }
        
        // Обробка успішно распарсеного атому
        if err := callback(atom); err != nil {
            return parseErr("callback", int(offset), err)
        }
    }
}
```

### 🔧 Приклад: Моніторинг помилок парсингу

```go
// ParseErrorMetrics — метрики для моніторингу помилок
type ParseErrorMetrics struct {
    TotalErrors     prometheus.CounterVec
    ErrorsByType    prometheus.CounterVec
    ErrorsByOffset  prometheus.HistogramVec
    RecoverySuccess prometheus.CounterVec
}

func (m *ParseErrorMetrics) RecordError(err error, recovered bool) {
    m.TotalErrors.Inc()
    
    if pe, ok := err.(*fmp4io.ParseError); ok {
        // Класифікація за типом останньої помилки
        last := pe
        for last.prev != nil {
            last = last.prev
        }
        m.ErrorsByType.WithLabelValues(last.Debug).Inc()
        
        // Гістограма позицій помилок
        m.ErrorsByOffset.Observe(float64(last.Offset))
    }
    
    if recovered {
        m.RecoverySuccess.Inc()
    }
}

// Використання:
err := ParseFMP4WithRecovery(reader, processAtom)
if err != nil {
    metrics.RecordError(err, false)
    return err
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Нескінченний цикл у Error()** | Паніка або зависання при форматуванні помилки | Додайте обмеження глибини ланцюжка (maxDepth) |
| **Втрата оригінальної помилки** | orig = nil у вкладених ParseError | Зберігайте orig через ланцюжок у parseErr() |
| **Некоректні offset'и** | Позиції помилок не співпадають з реальними | Переконайтеся що offset передається коректно на кожному рівні |
| **Паніка при nil prev** | Доступ до p.prev коли p == nil | Перевірка `p != nil` у циклі форматування |
| **Надлишкове логування** | Одна помилка логується багато разів | Логуйте тільки останню помилку у ланцюжку |

---

## ⚡ Оптимізації для production

### 1. Кешування форматованих помилок:

```go
type CachedParseError struct {
    *ParseError
    formatted string
    mu        sync.RWMutex
}

func (c *CachedParseError) Error() string {
    c.mu.RLock()
    if c.formatted != "" {
        s := c.formatted
        c.mu.RUnlock()
        return s
    }
    c.mu.RUnlock()
    
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if c.formatted == "" {
        c.formatted = c.ParseError.Error()
    }
    return c.formatted
}
```

### 2. Обмеження глибини ланцюжка:

```go
const maxErrorDepth = 10

func parseErrLimited(debug string, offset int, prev error) error {
    depth := 0
    for p := prev; p != nil && depth < maxErrorDepth; depth++ {
        if pe, ok := p.(*ParseError); ok {
            p = pe.prev
        } else {
            break
        }
    }
    
    if depth >= maxErrorDepth {
        // Обрізаємо ланцюжок
        return &ParseError{
            Debug:  debug,
            Offset: offset,
            prev:   nil,
            orig:   fmt.Errorf("...(truncated, max depth %d)", maxErrorDepth),
        }
    }
    
    return parseErr(debug, offset, prev)
}
```

### 3. Моніторинг продуктивності обробки помилок:

```go
type ErrorHandlingMetrics struct {
    ErrorCreationLatency prometheus.HistogramVec
    ErrorFormattingLatency prometheus.HistogramVec
    ChainLengths prometheus.HistogramVec
}

func (m *ErrorHandlingMetrics) RecordErrorCreation(duration time.Duration, chainLength int) {
    m.ErrorCreationLatency.Observe(duration.Seconds())
    m.ChainLengths.Observe(float64(chainLength))
}
```

---

## 📋 Чек-лист безпечного використання ParseError

```go
// ✅ 1. Завжди передавайте коректний offset
err := parseErr("atomHeader", int(offset), io.ErrUnexpectedEOF)

// ✅ 2. Обмежуйте глибину ланцюжка помилок
const maxDepth = 10
for p := err; p != nil && depth < maxDepth; p = p.prev { ... }

// ✅ 3. Зберігайте оригінальну помилку через orig
if pe, ok := err.(*ParseError); ok && pe.orig != nil {
    log.Printf("Original error: %v", pe.orig)
}

// ✅ 4. Логуйте тільки останню помилку у ланцюжку
last := err
for pe, ok := last.(*ParseError); ok && pe.prev != nil; last = pe.prev {
    pe, _ = last.(*ParseError)
}
log.Printf("Last error: %v", last)

// ✅ 5. Використовуйте helper для додавання контексту
func withContext(debug string, offset int, err error) error {
    if err == nil { return nil }
    return parseErr(debug, offset, err)
}

// ✅ 6. Метрики для моніторингу
metrics.RecordError(err, recovered)
```

---

## 🔗 Корисні посилання

- 💻 [Go errors Package](https://pkg.go.dev/errors) — стандартна бібліотека для роботи з помилками
- 📄 [Error Handling Best Practices in Go](https://go.dev/blog/error-handling-and-go) — офіційний гайд
- 🧪 [Prometheus Histograms for Latency](https://prometheus.io/docs/practices/histograms/) — моніторинг латентності
- 📦 [sync.Mutex Documentation](https://pkg.go.dev/sync#Mutex) — синхронізація для кешування

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Додайте обмеження глибини ланцюжка** — запобігання нескінченним циклам у Error().
> 2. **Зберігайте оригінальну помилку через orig** — уникнення втрати важливої інформації.
> 3. **Використовуйте helper parseWithContext** — спрощення створення контекстних помилок.
> 4. **Моніторьте `ErrorsByType` метрику** — виявлення частих типів помилок для покращення парсингу.
> 5. **Логуйте тільки останню помилку** — уникнення надмірного логування при вкладених помилках.

Потрібен приклад реалізації повної системи обробки помилок з автоматичним відновленням після пошкоджених fMP4 файлів, або інтеграція `fmp4io.ParseError` з вашим моніторингом через Prometheus? Готовий допомогти! 🚀