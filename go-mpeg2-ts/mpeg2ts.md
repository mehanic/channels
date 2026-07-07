# 🔍 Глибокий розбір коду: `MPEG2TS` — завантаження, фільтрація та валідація TS потоків

Цей код реалізує **базовий парсер MPEG-2 Transport Stream** із функціями завантаження файлів, фільтрації за PID та перевірки цілісності потоку через continuity counter. Розберемо архітектурно, детально та з критичним аналізом.

---

## 📦 Архітектура модуля: огляд компонентів

```
┌─────────────────────────────────────┐
│ Модуль: mpeg2ts (завантажувач)      │
│ Призначення: читання та обробка TS  │
│ Вхід: .ts файл (188/204 байт пакети)│
├─────────────────────────────────────┤
│ 🔹 Публічні функції:                 │
│    • New() — створення порожнього   │
│    • LoadStandardTS() — завантаження│
│    • LoadStandardTSWithFEC() — з FEC│
│    • CheckStream() — continuity check│
│    • FilterByPIDs() — фільтрація    │
│                                      │
│ 🔹 Внутрішні:                        │
│    • loadFile() — ядро завантаження │
│                                      │
│ 🔹 Глобальні константи:              │
│    • PacketSizeDefault = 188        │
│    • PacketSizeWithFEC = 204        │
│    • PID_NullPacket = 0x1FFF        │
└─────────────────────────────────────┘
```

### 🎯 Контекст: MPEG-2 TS пакети
```
📦 Стандартний TS пакет (188 байт):
   ├─ Синхробайт 0x47 — 1 байт
   ├─ Заголовок — 3 байти
   │  ├─ transport_error_indicator (1 біт)
   │  ├─ payload_unit_start_indicator (1 біт)
   │  ├─ transport_priority (1 біт)
   │  ├─ PID (13 біт) ← ключове поле для фільтрації
   │  ├─ transport_scrambling_control (2 біти)
   │  ├─ adaptation_field_control (2 біти)
   │  └─ continuity_counter (4 біти) ← для CheckStream
   ├─ Adaptation Field (опціонально) — 0-183 байти
   └─ Payload — решта даних

📦 TS з FEC (204 байт):
   • Ті самі 188 байт + 16 байт Reed-Solomon ECC
   • Використовується у супутниковому мовленні (DVB-S)
   • Парсер має відкидати FEC байти перед обробкою
```

---

## 🔬 Детальний розбір кожної функції

### 1️⃣ `New()` та конструктори

```go
func New(chunkSize int) *MPEG2TS {
    m := MPEG2TS{}
    m.PacketList, _ = NewPacketList(chunkSize)  // ❌ Помилка ігнорується!
    return &m
}
```

#### ⚠️ Критичні проблеми
```go
// ❌ Ігнорування помилки NewPacketList:
m.PacketList, _ = NewPacketList(chunkSize)
// • Якщо NewPacketList поверне error → PacketList = nil → паніка далі!
// ✅ Правильно:
func New(chunkSize int) (*MPEG2TS, error) {
    pl, err := NewPacketList(chunkSize)
    if err != nil {
        return nil, fmt.Errorf("failed to create packet list: %w", err)
    }
    return &MPEG2TS{PacketList: pl, chunkSize: chunkSize}, nil
}

// ❌ Value receiver vs pointer receiver інконсистентність:
// • New() повертає *MPEG2TS (pointer)
// • CheckStream() має value receiver (copy!)
// • FilterByPIDs() має pointer receiver
// → Може призвести до неочікуваної поведінки

// ✅ Правильно: використовувати pointer receivers для всіх методів, що працюють зі станом:
func (m *MPEG2TS) CheckStream() StreamCheckResult { ... }  // ✅ pointer
```

---

### 2️⃣ `loadFile()` — ядро завантаження

```go
func loadFile(fname string, packetLength int) (*MPEG2TS, error) {
    file, err := os.Open(fname)
    if err != nil {
        return nil, err
    }
    defer file.Close()  // ✅ Правильне закриття ресурсу
    
    // 🎯 Перевірка розміру файлу
    var fsize int64
    if fi, err := file.Stat(); err == nil {
        fsize = fi.Size()
    } else {
        return nil, err
    }
    
    if fsize < PacketSizeDefault {
        return nil, fmt.Errorf("filesize (%d) is smaller than the minimum (%d)", fsize, PacketSizeDefault)
    }
    
    m := New(packetLength)  // ❌ Ігнорує помилку New()!
    
    packetBuffer := make([]byte, packetLength)
    i := 0
    for {
        n, err := file.Read(packetBuffer)
        if err != nil {
            // ❌ Неправильна перевірка EOF:
            if err.Error() == "EOF" {  // ❌ Ніколи не робіть так!
                break
            }
            return nil, err
        }
        if n < packetLength {
            return nil, fmt.Errorf("sirikire %d", n)  // ❌ Японська помилка + незрозуміло
        }
        
        err = m.PacketList.AddBytes(packetBuffer, packetLength)
        if err != nil {
            return nil, err
        }
        i++
    }
    return m, nil
}
```

#### ⚠️ Критичні проблеми

| Проблема | Наслідок | Рішення |
|----------|----------|---------|
| `err.Error() == "EOF"` | Не працює з wrapped errors, нестабільно | `errors.Is(err, io.EOF)` |
| Ігнорування помилки `New()` | Можлива паніка при `m.PacketList.AddBytes` | Повертати error з `New()` |
| "sirikire" помилка | Незрозуміло для міжнародної команди | `"incomplete packet: got %d bytes, expected %d"` |
| Читання пакет-за-пакетом | Повільно для великих файлів | Використовувати `bufio.Reader` |
| Відсутність перевірки синхробайта | Може парсити пошкоджені файли | Перевіряти `packetBuffer[0] == 0x47` |

#### ✅ Правильна перевірка EOF та читання
```go
import (
    "bufio"
    "errors"
    "io"
)

func loadFile(fname string, packetLength int) (*MPEG2TS, error) {
    file, err := os.Open(fname)
    if err != nil {
        return nil, fmt.Errorf("failed to open file: %w", err)
    }
    defer file.Close()
    
    // 🎯 Перевірка розміру
    fi, err := file.Stat()
    if err != nil {
        return nil, fmt.Errorf("failed to stat file: %w", err)
    }
    if fi.Size() < int64(packetLength) {
        return nil, fmt.Errorf("file too small: %d < %d", fi.Size(), packetLength)
    }
    
    // 🎯 Створення парсера з обробкою помилок
    m, err := New(packetLength)
    if err != nil {
        return nil, fmt.Errorf("failed to init MPEG2TS: %w", err)
    }
    
    // 🎯 Буферизоване читання для продуктивності
    reader := bufio.NewReaderSize(file, packetLength*1024)  // 1KB буфер
    packetBuffer := make([]byte, packetLength)
    
    for {
        // 🎯 Читання рівно packetLength байт
        _, err := io.ReadFull(reader, packetBuffer)
        if err == io.EOF {
            break  // ✅ Кінець файлу — нормальне завершення
        }
        if err != nil {
            if errors.Is(err, io.ErrUnexpectedEOF) {
                return nil, fmt.Errorf("incomplete packet at EOF: %w", err)
            }
            return nil, fmt.Errorf("read error: %w", err)
        }
        
        // 🎯 Перевірка синхробайта (опціонально, але рекомендовано)
        if packetBuffer[0] != 0x47 {
            // 🎯 Спроба ресинхронізації: пошук наступного 0x47
            if !resyncToSyncByte(reader, packetBuffer) {
                return nil, fmt.Errorf("lost sync: expected 0x47, got 0x%02X", packetBuffer[0])
            }
        }
        
        if err := m.PacketList.AddBytes(packetBuffer, packetLength); err != nil {
            return nil, fmt.Errorf("failed to add packet: %w", err)
        }
    }
    
    return m, nil
}

// 🎯 Helper для ресинхронізації при втраті синхробайта
func resyncToSyncByte(r *bufio.Reader, buf []byte) bool {
    for {
        b, err := r.ReadByte()
        if err != nil {
            return false
        }
        if b == 0x47 {
            buf[0] = 0x47
            _, err := io.ReadFull(r, buf[1:])
            return err == nil
        }
    }
}
```

---

### 3️⃣ `CheckStream()` — валідація continuity counter

```go
func (m MPEG2TS) CheckStream() StreamCheckResult {
    cr := StreamCheckResult{}
    ci := map[PID]byte{}  // 🎯 continuity_index для кожного PID
    dc := 0
    
    // 🎯 Ініціалізація всіх можливих PID (0x0000-0x1FFF) значенням 16 ("не встановлено")
    for i := uint16(0); i < 0x2000; i++ {
        ci[PID(i)] = byte(16)
    }
    
    for i, p := range m.PacketList.All() {
        if p.PID == PID_NullPacket {
            continue  // ✅ Пропускаємо null пакети
        }
        
        if ci[p.PID] == 16 {
            // 🎯 Перший пакет для цього PID
            if p.AdaptationFieldControl != 0 && p.AdaptationFieldControl != 2 {
                ci[p.PID] = p.ContinuityCheckIndex  // ✅ Запам'ятовуємо перше значення
            }
        } else if (ci[p.PID]+1)%16 != p.ContinuityCheckIndex {
            // 🎯 Очікували (prev+1)%16, отримали інше → втрата пакету
            if p.AdaptationFieldControl != 0 && p.AdaptationFieldControl != 2 {
                dc++
                ci[p.PID] = p.ContinuityCheckIndex  // ✅ Синхронізуємо з новим значенням
                cr.DropList = append(cr.DropList, struct {
                    Description string
                    Index       int
                }{"frame drop detected", i})
            }
        } else {
            // 🎯 Нормальний випадок: continuity збігається
            if p.AdaptationFieldControl != 0 && p.AdaptationFieldControl != 2 {
                ci[p.PID] = p.ContinuityCheckIndex
            }
        }
    }
    cr.DropCount = dc
    return cr
}
```

#### 🎯 Логіка continuity counter за специфікацією
```
📋 MPEG-2 TS специфікація (13818-1 §2.4.3.2):
• continuity_counter: 4 біти (0-15), зростає по модулю 16
• Збільшується ТІЛЬКИ якщо adaptation_field_control ∈ {1, 3} (є payload)
• НЕ збільшується якщо adaptation_field_control ∈ {0, 2} (тільки adaptation field або null)

🔍 Алгоритм перевірки:
1. Для кожного PID запам'ятовуємо останній valid continuity_counter
2. При отриманні нового пакету:
   • Якщо adaptation_field_control ∈ {0, 2} → пропускаємо перевірку
   • Інакше: очікуємо (last+1)%16, якщо не збігається → drop detected
3. Null пакети (PID=0x1FFF) завжди ігноруються

⚠️ Нюанси:
• Після втрати пакету: синхронізуємо з поточним значенням (не повертаємо помилку)
• Перший пакет для PID: будь-яке значення приймається як початкове
• Переповнення: 15 → 0 обробляється коректно через %16
```

#### ⚠️ Проблеми реалізації
```go
// ❌ Value receiver у CheckStream():
func (m MPEG2TS) CheckStream() StreamCheckResult  // ❌ Копіює весь struct!
// • Якщо MPEG2TS містить великий PacketList → дорога копія
// ✅ Правильно: pointer receiver
func (m *MPEG2TS) CheckStream() StreamCheckResult  // ✅ Без копії

// ❌ Ініціалізація map з 8192 записами для кожного виклику:
for i := uint16(0); i < 0x2000; i++ { ci[PID(i)] = 16 }
// • O(8192) операцій навіть якщо використовується 10 PID
// ✅ Правильно: використовувати "не встановлено" через відсутність ключа
ci := make(map[PID]byte)  // Порожній map
// ...
if last, ok := ci[p.PID]; !ok {
    // Перший пакет для цього PID
    ci[p.PID] = p.ContinuityCheckIndex
} else if (last+1)%16 != p.ContinuityCheckIndex {
    // Drop detected
}

// ❌ Анонімний struct у DropList:
struct {
    Description string
    Index       int
}{"frame drop detected", i}
// • Неможливо розширити без зміни коду
// • Не інтуїтивно для користувачів бібліотеки
// ✅ Правильно: окремий тип
type DropEntry struct {
    Description string
    PacketIndex int
    PID         PID  // Додати PID для зручності дебагу
}
type StreamCheckResult struct {
    DropCount int
    DropList  []DropEntry
}
```

#### ✅ Оптимізована версія CheckStream
```go
func (m *MPEG2TS) CheckStream() StreamCheckResult {
    cr := StreamCheckResult{
        DropList: make([]DropEntry, 0),  // ✅ Попереднє виділення
    }
    // 🎯 Використовуємо map тільки для активних PID
    continuity := make(map[PID]byte)
    
    for idx, pkt := range m.PacketList.All() {
        // 🎯 Пропускаємо null пакети
        if pkt.PID == PID_NullPacket {
            continue
        }
        
        // 🎯 Пропускаємо пакети без payload (adaptation_field_control 0 або 2)
        if pkt.AdaptationFieldControl == 0 || pkt.AdaptationFieldControl == 2 {
            continue
        }
        
        last, exists := continuity[pkt.PID]
        if !exists {
            // 🎯 Перший пакет для цього PID: приймаємо будь-яке значення
            continuity[pkt.PID] = pkt.ContinuityCheckIndex
            continue
        }
        
        expected := (last + 1) % 16
        if expected != pkt.ContinuityCheckIndex {
            // 🎯 Виявлено втрату пакету
            cr.DropCount++
            cr.DropList = append(cr.DropList, DropEntry{
                Description: "continuity counter mismatch",
                PacketIndex: idx,
                PID:         pkt.PID,
                Expected:    expected,
                Actual:      pkt.ContinuityCheckIndex,
            })
            // 🎯 Синхронізуємо з поточним значенням для подальшої перевірки
            continuity[pkt.PID] = pkt.ContinuityCheckIndex
        } else {
            continuity[pkt.PID] = pkt.ContinuityCheckIndex
        }
    }
    
    return cr
}
```

---

### 4️⃣ `FilterByPIDs()` — фільтрація пакетів

```go
func (m *MPEG2TS) FilterByPIDs(pids ...PID) *MPEG2TS {
    mx := New(m.chunkSize)  // ❌ chunkSize може бути неекспортованим полем!
    for _, p := range m.PacketList.All() {
        for _, id := range pids {
            if p.PID == id {
                mx.AddPacket(p)  // ✅ Додає у відфільтрований список
                break
            }
        }
    }
    return mx
}
```

#### ⚠️ Проблеми
```go
// ❌ Доступ до неекспортованого поля:
mx := New(m.chunkSize)  // Якщо chunkSize не експортований — компіляція не пройде!
// ✅ Правильно: або експортувати, або використовувати константу/метод
mx := New(m.GetChunkSize())  // Якщо є геттер
// або
mx, _ := New(PacketSizeDefault)  // Якщо завжди стандартний розмір

// ❌ O(n*m) складність через nested loop:
// • n = кількість пакетів, m = кількість PID для фільтрації
// • При 10000 пакетів × 10 PID → 100000 порівнянь
// ✅ Правильно: використати map для O(1) lookup
func (m *MPEG2TS) FilterByPIDs(pids ...PID) *MPEG2TS {
    // 🎯 Створити set з PID для швидкого пошуку
    pidSet := make(map[PID]bool, len(pids))
    for _, pid := range pids {
        pidSet[pid] = true
    }
    
    mx, _ := New(m.chunkSize)  // TODO: обробити error
    for _, p := range m.PacketList.All() {
        if pidSet[p.PID] {
            mx.AddPacket(p)
        }
    }
    return mx
}

// ❌ Відсутність обробки помилок у AddPacket:
mx.AddPacket(p)  // Якщо AddPacket повертає error → ігнорується!
// ✅ Правильно:
if err := mx.AddPacket(p); err != nil {
    // Логувати або повертати error залежно від політики
    log.Printf("Failed to add packet PID=0x%X: %v", p.PID, err)
}
```

---

## ⚠️ Загальні проблеми модуля

### 1️⃣ Відсутність підтримки streaming / великих файлів
```go
// ❌ loadFile() завантажує ВЕСЬ файл у пам'ять через PacketList:
// • Файл 1GB → 1GB RAM + overhead для PacketList
// • Неможливо обробляти live-потік або файли > available RAM

// ✅ Рішення: додати streaming API
type TSStreamReader struct {
    reader     io.Reader
    packetSize int
    // ... буфери, стан ...
}

func NewTSStreamReader(r io.Reader, packetSize int) *TSStreamReader {
    return &TSStreamReader{reader: r, packetSize: packetSize}
}

func (sr *TSStreamReader) NextPacket() (*TSPacket, error) {
    buf := make([]byte, sr.packetSize)
    _, err := io.ReadFull(sr.reader, buf)
    if err != nil {
        return nil, err
    }
    return ParseTSPacket(buf)  // Парсинг одного пакету
}

// Використання:
sr := NewTSStreamReader(file, PacketSizeDefault)
for {
    pkt, err := sr.NextPacket()
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    // Обробка пакету...
}
```

### 2️⃣ Відсутність валідації вхідних даних
```go
// ❌ Немає перевірки:
// • Чи файл дійсно є MPEG-2 TS (наявність синхробайтів)
// • Чи packetLength валідний (188 або 204)
// • Чи PacketList ініціалізований перед використанням

// ✅ Додати валідацію:
func LoadStandardTS(fname string) (*MPEG2TS, error) {
    if err := validateTSFile(fname, PacketSizeDefault); err != nil {
        return nil, fmt.Errorf("invalid TS file: %w", err)
    }
    return loadFile(fname, PacketSizeDefault)
}

func validateTSFile(fname string, packetSize int) error {
    file, err := os.Open(fname)
    if err != nil {
        return err
    }
    defer file.Close()
    
    // 🎯 Перевірка перших 10 пакетів на наявність синхробайта 0x47
    buf := make([]byte, packetSize)
    for i := 0; i < 10; i++ {
        n, err := file.Read(buf)
        if err != nil {
            return fmt.Errorf("failed to read sync check: %w", err)
        }
        if n < packetSize || buf[0] != 0x47 {
            return fmt.Errorf("invalid TS sync byte at packet %d: 0x%02X", i, buf[0])
        }
    }
    return nil
}
```

### 3️⃣ Відсутність тестів
```go
// ❌ Немає жодного _test.go файлу
// • Неможливо перевірити коректність continuity check
// • Неможливо покрити edge cases (пошкоджені пакети, незвичні PID)

// ✅ Додати мінімальні тести:
func TestCheckStream_ContinuityValid(t *testing.T) {
    // 🎯 Створити моковий TS з коректними continuity counters
    ts := createMockTSWithContinuity()
    result := ts.CheckStream()
    assert.Equal(t, 0, result.DropCount)
}

func TestCheckStream_ContinuityDrop(t *testing.T) {
    // 🎯 Створити TS з пропущеним пакетом
    ts := createMockTSWithDrop()
    result := ts.CheckStream()
    assert.Greater(t, result.DropCount, 0)
    assert.Len(t, result.DropList, result.DropCount)
}

func TestFilterByPIDs_Selective(t *testing.T) {
    ts := createMockTSWithMultiplePIDs()
    filtered := ts.FilterByPIDs(0x100, 0x101)
    
    for _, pkt := range filtered.PacketList.All() {
        assert.Contains(t, []PID{0x100, 0x101}, pkt.PID)
    }
}
```

### 4️⃣ Інтернаціоналізація та логування
```go
// ❌ Змішані мови в помилках:
return nil, fmt.Errorf("sirikire %d", n)  // Японська + незрозуміло
// ✅ Правильно: англійські помилки з контекстом:
return nil, fmt.Errorf("incomplete packet: read %d bytes, expected %d", n, packetLength)

// ❌ Закоментовані debug prints:
// fmt.Println("skip")  // Залишки розробки
// ✅ Правильно: використовувати logger з рівнями:
type TSLogger interface {
    Debug(msg string, fields ...interface{})
    Info(msg string, fields ...interface{})
    Warn(msg string, fields ...interface{})
    Error(msg string, err error, fields ...interface{})
}

// Використання:
if p.AdaptationFieldControl == 0 || p.AdaptationFieldControl == 2 {
    logger.Debug("skipping packet without payload", "pid", p.PID, "afc", p.AdaptationFieldControl)
    continue
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **WebSocket-приймачем TS-фрагментів**:

### 🎯 Сценарій: валідація вхідного TS потоку
```go
// У TSValidator для перевірки якості вхідного потоку:
type TSValidator struct {
    continuity map[mpeg2ts.PID]byte
    logger     *log.Logger
}

func (v *TSValidator) ValidatePacket(pkt *mpeg2ts.TSPacket) error {
    // 🎯 Пропускаємо null пакети
    if pkt.PID == mpeg2ts.PID_NullPacket {
        return nil
    }
    
    // 🎯 Перевірка синхробайта
    if !pkt.HasValidSyncByte() {
        return fmt.Errorf("invalid sync byte: 0x%02X", pkt.SyncByte)
    }
    
    // 🎯 Continuity check (спрощена версія)
    if pkt.HasPayload() {  // adaptation_field_control ∈ {1, 3}
        last, exists := v.continuity[pkt.PID]
        if !exists {
            v.continuity[pkt.PID] = pkt.ContinuityCheckIndex
            return nil
        }
        
        expected := (last + 1) % 16
        if expected != pkt.ContinuityCheckIndex {
            v.logger.Warn("continuity error", 
                "pid", pkt.PID,
                "expected", expected,
                "actual", pkt.ContinuityCheckIndex)
            // 🎯 Не повертаємо error — синхронізуємо та продовжуємо
            v.continuity[pkt.PID] = pkt.ContinuityCheckIndex
        } else {
            v.continuity[pkt.PID] = pkt.ContinuityCheckIndex
        }
    }
    
    return nil
}

// Використання у WebSocket хендлері:
func (h *WSHandler) onTSFragment(channelID string, data []byte) {
    validator := h.getValidator(channelID)
    
    // 🎯 Парсинг пакетів з вхідних байтів
    packets, err := mpeg2ts.ParsePackets(data, mpeg2ts.PacketSizeDefault)
    if err != nil {
        h.logger.Error("TS parse failed", "channel", channelID, "error", err)
        return
    }
    
    for _, pkt := range packets {
        if err := validator.ValidatePacket(pkt); err != nil {
            h.metrics.TSErrors.Inc()
            h.logger.Debug("packet validation failed", 
                "channel", channelID, "pid", pkt.PID, "error", err)
            // Опціонально: відкинути пошкоджений пакет
            continue
        }
        
        // 🎯 Відправка у ваш pipeline обробки
        h.assembler.ProcessPacket(channelID, pkt)
    }
}
```

### 🎯 Сценарій: фільтрація відео/аудіо потоків
```go
// У StreamRouter для розділення відео/аудіо/даних:
type StreamRouter struct {
    videoPID   mpeg2ts.PID
    audioPID   mpeg2ts.PID
    pcrPID     mpeg2ts.PID
}

func (r *StreamRouter) RoutePacket(pkt *mpeg2ts.TSPacket) StreamType {
    switch pkt.PID {
    case r.videoPID:
        return StreamVideo
    case r.audioPID:
        return StreamAudio
    case r.pcrPID:
        return StreamPCR  // Для синхронізації A/V
    case mpeg2ts.PID_PAT, mpeg2ts.PID_PMT:
        return StreamSI  // Service Information
    default:
        return StreamOther
    }
}

// Використання з FilterByPIDs:
func (r *StreamRouter) ExtractVideoStream(ts *mpeg2ts.MPEG2TS) *mpeg2ts.MPEG2TS {
    return ts.FilterByPIDs(r.videoPID, r.pcrPID)  // Відео + PCR для синхронізації
}
```

### 🎯 Сценарій: моніторинг якості транспорту
```go
// У monitoring.Monitor для агрегації метрик:
type TransportQualityMetrics struct {
    TotalPackets      int64
    ContinuityErrors  int64
    SyncErrors        int64
    DropRate          float64  // %
    ActivePIDs        int      // Кількість активних PID
}

func (m *Monitor) AnalyzeTransportQuality(ts *mpeg2ts.MPEG2TS) TransportQualityMetrics {
    metrics := TransportQualityMetrics{
        TotalPackets: int64(ts.PacketList.Len()),
    }
    
    // 🎯 Continuity check
    cr := ts.CheckStream()
    metrics.ContinuityErrors = int64(cr.DropCount)
    
    if metrics.TotalPackets > 0 {
        metrics.DropRate = float64(metrics.ContinuityErrors) / float64(metrics.TotalPackets) * 100
    }
    
    // 🎯 Підрахунок активних PID
    pidSet := make(map[mpeg2ts.PID]bool)
    for _, pkt := range ts.PacketList.All() {
        if pkt.PID != mpeg2ts.PID_NullPacket {
            pidSet[pkt.PID] = true
        }
    }
    metrics.ActivePIDs = len(pidSet)
    
    // 🎯 Алерти при поганій якості
    if metrics.DropRate > 1.0 {  // >1% втрат
        m.alerts["transport_high_drop_rate"].Inc()
    }
    if metrics.ContinuityErrors > 100 {
        m.alerts["transport_continuity_errors"].Inc()
    }
    
    return metrics
}
```

---

## 🧪 Приклад: рефакторинг з кращою структурою

```go
// ✅ Рефакторинг loadFile з streaming підтримкою:
func LoadStandardTS(fname string) (*MPEG2TS, error) {
    return LoadTSWithOptions(fname, TSLoadOptions{
        PacketSize: PacketSizeDefault,
        ValidateSync: true,
        MaxPackets: 0,  // 0 = без обмеження
    })
}

type TSLoadOptions struct {
    PacketSize   int
    ValidateSync bool
    MaxPackets   int  // 0 = необмежено
    ProgressCB   func(processed, total int64)  // Callback для прогресу
}

func LoadTSWithOptions(fname string, opts TSLoadOptions) (*MPEG2TS, error) {
    if opts.PacketSize != PacketSizeDefault && opts.PacketSize != PacketSizeWithFEC {
        return nil, fmt.Errorf("invalid packet size: %d (expected %d or %d)", 
            opts.PacketSize, PacketSizeDefault, PacketSizeWithFEC)
    }
    
    file, err := os.Open(fname)
    if err != nil {
        return nil, fmt.Errorf("failed to open file: %w", err)
    }
    defer file.Close()
    
    fi, err := file.Stat()
    if err != nil {
        return nil, fmt.Errorf("failed to stat file: %w", err)
    }
    
    m, err := New(opts.PacketSize)
    if err != nil {
        return nil, fmt.Errorf("failed to init parser: %w", err)
    }
    
    reader := bufio.NewReaderSize(file, opts.PacketSize*1024)
    packetBuffer := make([]byte, opts.PacketSize)
    var processed int64
    
    for {
        // 🎯 Читання з перевіркою синхробайта
        _, err := io.ReadFull(reader, packetBuffer)
        if err == io.EOF {
            break
        }
        if err != nil {
            if errors.Is(err, io.ErrUnexpectedEOF) {
                return nil, fmt.Errorf("truncated packet at offset %d", processed)
            }
            return nil, fmt.Errorf("read error at offset %d: %w", processed, err)
        }
        
        if opts.ValidateSync && packetBuffer[0] != 0x47 {
            // 🎯 Спроба ресинхронізації
            if !resyncToSyncByte(reader, packetBuffer) {
                return nil, fmt.Errorf("lost sync at offset %d", processed)
            }
        }
        
        if err := m.PacketList.AddBytes(packetBuffer, opts.PacketSize); err != nil {
            return nil, fmt.Errorf("failed to store packet %d: %w", processed/opts.PacketSize, err)
        }
        
        processed += int64(opts.PacketSize)
        
        // 🎯 Callback для прогресу
        if opts.ProgressCB != nil && processed%int64(opts.PacketSize*1000) == 0 {
            total := fi.Size()
            if total > 0 {
                opts.ProgressCB(processed, total)
            }
        }
        
        // 🎯 Обмеження кількості пакетів (для тестування)
        if opts.MaxPackets > 0 && processed/int64(opts.PacketSize) >= int64(opts.MaxPackets) {
            break
        }
    }
    
    return m, nil
}
```

---

## 📋 Best Practices для MPEG-2 TS обробки

```
✅ Обробка помилок:
   • Використовувати errors.Is(err, io.EOF) замість порівняння рядків
   • Обгортати помилки з контекстом: fmt.Errorf("context: %w", err)
   • Не ігнорувати помилки конструкторів та методів

✅ Продуктивність:
   • Використовувати bufio.Reader для буферизованого читання
   • Уникати O(n) ініціалізації map — використовувати "відсутність ключа" як стан
   • Попередньо виділяти буфери та слайси де можливо

✅ Валідація:
   • Перевіряти синхробайт 0x47 для виявлення пошкоджених потоків
   • Валідувати adaptation_field_control перед continuity check
   • Перевіряти розмір пакету перед обробкою

✅ Архітектура:
   • Використовувати pointer receivers для методів, що працюють зі станом
   • Уникати глобальних змінних — ін'єктувати залежності
   • Додати streaming API для великих файлів та live-потоків

✅ Тестування:
   • Додати юніт-тести для continuity check з моковими даними
   • Покрити edge cases: пошкоджені пакети, втрата синхронізації
   • Додати інтеграційні тести з реальними фікстурами

✅ Моніторинг:
   • Збирати метрики: continuity errors, sync errors, drop rate
   • Інтегрувати з Prometheus/Grafana для production alerting
   • Логувати аномалії з достатнім контекстом для дебагу
```

---

## 🎯 Висновок

Цей модуль — **функціональна основа** для роботи з MPEG-2 TS:

✅ Правильна логіка continuity counter перевірки  
✅ Підтримка стандартних розмірів пакетів (188/204 байт)  
✅ Зручна фільтрація за PID для подальшої обробки

**Критичні виправлення перед продакшеном**:

1. ✅ **Замінити `err.Error() == "EOF"` на `errors.Is(err, io.EOF)`**
2. ✅ **Обробляти помилку `New()`** замість ігнорування через `_`
3. ✅ **Замінити value receiver на pointer receiver** у `CheckStream()`
4. ✅ **Оптимізувати ініціалізацію map** у continuity check (не створювати 8192 записи)
5. ✅ **Додати валідацію синхробайта** для виявлення пошкоджених потоків
6. ✅ **Замінити анонімний struct** у `DropList` на іменований тип
7. ✅ **Додати streaming API** для обробки великих файлів без завантаження у пам'ять

**Приклад інтеграції у ваш CCTV pipeline**:
```go
// 🎯 TSIngestor для вашого WebSocket-сервера:
type TSIngestor struct {
    channelID  string
    validator  *TSValidator
    router     *StreamRouter
    assembler  *SegmentAssembler  // Ваш існуючий компонент
}

func (i *TSIngestor) ProcessChunk(data []byte) error {
    // 🎯 Парсинг пакетів з чанку
    packets, err := mpeg2ts.ParsePackets(data, mpeg2ts.PacketSizeDefault)
    if err != nil {
        return fmt.Errorf("packet parse failed: %w", err)
    }
    
    for _, pkt := range packets {
        // 🎯 Валідація
        if err := i.validator.ValidatePacket(pkt); err != nil {
            // Логувати, але не зупиняти потік
            continue
        }
        
        // 🎯 Маршрутизація
        switch i.router.RoutePacket(pkt) {
        case StreamVideo:
            i.assembler.ProcessVideoPacket(pkt)
        case StreamAudio:
            i.assembler.ProcessAudioPacket(pkt)
        case StreamPCR:
            i.assembler.SyncWithPCR(pkt)
        }
    }
    
    return nil
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом `CheckStream()` з оптимізацією map та кращою структурою `DropEntry`?
- 🧠 Інтеграцією streaming API для обробки live TS потоків без завантаження у пам'ять?
- 🧪 Написанням тестів для continuity check з моковими даними та edge cases?

Чекаю на ваші питання! 🛠️📡🎬