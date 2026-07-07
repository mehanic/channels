# 🔐 Глибокий розбір: tsio — CRC32 для MPEG-TS пакетів

Цей файл — **реалізація обчислення контрольної суми CRC32** згідно зі стандартом IEEE 802.3, що використовується у форматах MPEG-TS (Transport Stream) для перевірки цілісності даних. Він надає ефективний алгоритм на основі попередньо обчисленої таблиці (lookup table).

Розберемо архітектуру, математичну основу та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема tsio.CRC32

```
┌────────────────────────────────────────┐
│ 📦 tsio — CRC32 for MPEG-TS Integrity │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • ieeeCrc32Tbl — таблиця на 256×4 байти│
│  • calcCRC32() — основна функція        │
│                                         │
│  📊 Алгоритм:                           │
│  • Polynomial: 0xEDB88320 (зворотний)  │
│  • Initial value: 0xFFFFFFFF           │
│  • Final XOR: 0xFFFFFFFF               │
│  • Table-driven: O(n) з низькою константою│
│                                         │
│  🔄 Використання у MPEG-TS:            │
│  • CRC32 у секціях PSI/SI (PAT, PMT...)│
│  • Перевірка цілісності при декодуванні│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. ieeeCrc32Tbl — таблиця попередньо обчислених значень

### Призначення:
Таблиця містить 256 попередньо обчислених 32-бітних значень, що дозволяє замінити повільні бітові операції на швидкий пошук у масиві.

### 🔍 Математична основа:

```
CRC32 використовує поліном:
  • Пряма форма: 0x04C11DB7
  • Зворотна форма (для цього коду): 0xEDB88320

Алгоритм обчислення одного байта:
  crc = (crc >> 8) ^ table[(crc ^ byte) & 0xFF]

Де:
  • (crc ^ byte) & 0xFF — індекс у таблиці (0-255)
  • table[index] — попередньо обчислене значення
  • (crc >> 8) — зсув поточного CRC для наступної ітерації
  • ^ — операція XOR для комбінування результатів
```

### 📊 Структура таблиці:

```go
var ieeeCrc32Tbl = []uint32{
    0x00000000, 0xB71DC104, 0x6E3B8209, 0xD926430D,  // індекси 0-3
    0xDC760413, 0x6B6BC517, 0xB24D861A, 0x0550471E,  // індекси 4-7
    // ... всього 256 елементів ...
    0x00000001,  // останній елемент (індекс 255)
}
```

### 🔧 Як генерується таблиця (довідково):

```go
// Генерація ieeeCrc32Tbl (виконується один раз при розробці)
func generateCRCTable() []uint32 {
    table := make([]uint32, 256)
    polynomial := uint32(0xEDB88320)  // зворотний поліном
    
    for i := uint32(0); i < 256; i++ {
        crc := i
        for j := 0; j < 8; j++ {
            if crc&1 == 1 {
                crc = (crc >> 1) ^ polynomial
            } else {
                crc >>= 1
            }
        }
        table[i] = crc
    }
    return table
}
```

> 💡 **Чому таблиця?** Без неї кожен байт потребував би 8 ітерацій бітових операцій. З таблицею — лише 1 операція доступу до пам'яті + 2 прості арифметичні операції.

---

## 🔑 2. calcCRC32() — основна функція обчислення

### Реалізація:

```go
func calcCRC32(crc uint32, data []byte) uint32 {
    for _, b := range data {
        crc = ieeeCrc32Tbl[b^byte(crc)] ^ (crc >> 8)
    }
    return crc
}
```

### 🔍 Покроковий розбір:

```
Вхід: crc = початкове значення (зазвичай 0xFFFFFFFF), data = масив байт

Для кожного байта b у data:
  1. b ^ byte(crc) — XOR байта даних з молодшим байтом поточного CRC
  2. [результат] — використання як індексу у таблиці (0-255)
  3. ieeeCrc32Tbl[індекс] — отримання попередньо обчисленого значення
  4. (crc >> 8) — зсув поточного CRC на 8 біт вправо
  5. ^ — комбінування табличного значення зі зсунутим CRC
  6. Присвоєння результату назад у crc

Вихід: фінальне значення CRC32 (потрібно інвертувати біти для стандартного результату)
```

### 🔢 Приклад обчислення:

```
Вхід: crc = 0xFFFFFFFF, data = []byte{0x00, 0x01}

Ітерація 1 (b=0x00):
  index = 0x00 ^ byte(0xFFFFFFFF) = 0x00 ^ 0xFF = 0xFF = 255
  table[255] = 0x00000001  (останній елемент таблиці)
  crc >> 8 = 0xFFFFFFFF >> 8 = 0x00FFFFFF
  new_crc = 0x00000001 ^ 0x00FFFFFF = 0x00FFFFFE

Ітерація 2 (b=0x01):
  index = 0x01 ^ byte(0x00FFFFFE) = 0x01 ^ 0xFE = 0xFF = 255
  table[255] = 0x00000001
  crc >> 8 = 0x00FFFFFE >> 8 = 0x0000FFFF
  new_crc = 0x00000001 ^ 0x0000FFFF = 0x0000FFFE

Результат: 0x0000FFFE (потрібно інвертувати: ^0xFFFFFFFF = 0xFFFF0001)
```

### ✅ Ваш use-case: обчислення CRC32 для MPEG-TS секції

```go
// ComputeSectionCRC32 — обчислення контрольної суми для PSI/SI секції
func ComputeSectionCRC32(sectionData []byte) uint32 {
    // MPEG-TS вимагає: початкове значення 0xFFFFFFFF, фінальна інверсія
    crc := uint32(0xFFFFFFFF)
    crc = calcCRC32(crc, sectionData)
    return crc ^ 0xFFFFFFFF  // фінальна інверсія біт
}

// ValidateSectionCRC32 — перевірка цілісності секції
func ValidateSectionCRC32(sectionData []byte, expectedCRC uint32) bool {
    computed := ComputeSectionCRC32(sectionData)
    return computed == expectedCRC
}

// Використання для PAT (Program Association Table):
patData := getPATSectionData()  // дані секції без 4-байтового CRC в кінці
expectedCRC := pio.U32BE(patData[len(patData)-4:])  // останні 4 байти = CRC
payload := patData[:len(patData)-4]  // корисне навантаження без CRC

if ValidateSectionCRC32(payload, expectedCRC) {
    log.Printf("PAT section CRC valid")
} else {
    log.Printf("warning: PAT section CRC mismatch!")
}
```

---

## 🔑 3. Використання CRC32 у MPEG-TS

### 📦 Структура MPEG-TS секції з CRC:

```
PSI/SI секція (напр. PAT, PMT, SDT):
  [table_id: 1 байт]
  [section_syntax_indicator: 1 біт + ... + section_length: 12 біт]
  [інші поля заголовку...]
  [корисне навантаження: N байт]
  [CRC32: 4 байти]  ← обчислюється для всіх байт від table_id до кінця payload

Формула:
  CRC32 = calcCRC32(0xFFFFFFFF, section_without_crc) ^ 0xFFFFFFFF
```

### 🔍 Приклад: PAT секція

```
PAT (Program Association Table) структура:
  0x00                          // table_id = 0x00 для PAT
  0xB0 0x0D                     // section_syntax_indicator=1, section_length=13
  0x00 0x01                     // transport_stream_id
  0xC1 0x00                     // version_number=0, current_next_indicator=1, section_number=0, last_section_number=0
  0x00 0x01 0xE1 0x00          // program_number=1, PID=0x100 (NIT/PMT)
  [4 байти CRC32]              // обчислюється для байт 0x00...0x00 (до цього місця)
```

### ✅ Ваш use-case: парсинг PAT з перевіркою цілісності

```go
// ParsePATSection — парсинг PAT секції з валідацією CRC
func ParsePATSection(data []byte) (programs map[uint16]uint16, err error) {
    if len(data) < 8 {  // мінімальний розмір секції + CRC
        return nil, fmt.Errorf("PAT section too short")
    }
    
    // Перевірка table_id
    if data[0] != 0x00 {
        return nil, fmt.Errorf("expected PAT table_id=0x00, got 0x%02X", data[0])
    }
    
    // Перевірка section_syntax_indicator
    if data[1]&0x80 == 0 {
        return nil, fmt.Errorf("section_syntax_indicator must be 1")
    }
    
    // Розрахунок довжини секції (12 біт)
    sectionLength := int(pio.U16BE(data[1:3]) & 0x0FFF)
    
    // Перевірка цілісності даних
    if len(data) < 3+sectionLength+4 {
        return nil, fmt.Errorf("PAT section length mismatch")
    }
    
    // Виділення даних без CRC та очікуваного CRC
    payload := data[3 : 3+sectionLength]
    expectedCRC := pio.U32BE(data[3+sectionLength : 3+sectionLength+4])
    
    // Обчислення та перевірка CRC
    computedCRC := ComputeSectionCRC32(append([]byte{data[0], data[1], data[2]}, payload...))
    if computedCRC != expectedCRC {
        return nil, fmt.Errorf("CRC mismatch: expected 0x%08X, got 0x%08X", expectedCRC, computedCRC)
    }
    
    // Парсинг списку програм
    programs = make(map[uint16]uint16)
    offset := 0
    for offset+4 <= len(payload) {
        programNumber := pio.U16BE(payload[offset:])
        pid := pio.U16BE(payload[offset+2:]) & 0x1FFF
        programs[programNumber] = pid
        offset += 4
    }
    
    return programs, nil
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// ts_section_validator.go — валідація MPEG-TS секцій з CRC
type TSSectionValidator struct {
    channelID string
    metrics   *SectionMetrics
}

// ValidateAndParseSection — універсальна функція для PSI/SI секцій
func (v *TSSectionValidator) ValidateAndParseSection(tableID uint8, data []byte) (interface{}, error) {
    if len(data) < 8 {
        v.metrics.ShortSections.Inc()
        return nil, fmt.Errorf("section too short")
    }
    
    // Перевірка table_id
    if data[0] != tableID {
        v.metrics.WrongTableID.Inc()
        return nil, fmt.Errorf("expected table_id=0x%02X, got 0x%02X", tableID, data[0])
    }
    
    // Перевірка синтаксису
    if data[1]&0x80 == 0 {
        v.metrics.InvalidSyntax.Inc()
        return nil, fmt.Errorf("section_syntax_indicator must be 1")
    }
    
    // Розрахунок довжини
    sectionLength := int(pio.U16BE(data[1:3]) & 0x0FFF)
    if len(data) < 3+sectionLength+4 {
        v.metrics.LengthMismatch.Inc()
        return nil, fmt.Errorf("section length mismatch")
    }
    
    // CRC валідація
    payload := data[3 : 3+sectionLength]
    expectedCRC := pio.U32BE(data[3+sectionLength : 3+sectionLength+4])
    fullData := append([]byte{data[0], data[1], data[2]}, payload...)
    
    start := time.Now()
    computedCRC := ComputeSectionCRC32(fullData)
    v.metrics.CRCComputeLatency.Observe(time.Since(start).Seconds())
    
    if computedCRC != expectedCRC {
        v.metrics.CRCFailures.Inc()
        log.Printf("Channel %s: CRC failed for table_id=0x%02X", v.channelID, tableID)
        return nil, fmt.Errorf("CRC mismatch")
    }
    
    v.metrics.ValidSections.Inc()
    
    // Парсинг залежно від типу таблиці
    switch tableID {
    case 0x00:  // PAT
        return ParsePATSection(data)
    case 0x02:  // PMT
        return ParsePMTSection(data)
    case 0x42:  // SDT
        return ParseSDTSection(data)
    default:
        return payload, nil  // повертаємо сирий payload для невідомих таблиць
    }
}

// ProcessTSPacket — обробка одного TS пакету з перевіркою секцій
func (v *TSSectionValidator) ProcessTSPacket(packet []byte) error {
    // Перевірка sync byte
    if packet[0] != 0x47 {
        v.metrics.SyncErrors.Inc()
        return fmt.Errorf("invalid sync byte")
    }
    
    // Перевірка прапорців
    pid := pio.U16BE(packet[1:3]) & 0x1FFF
    payloadStart := packet[3]&0x40 != 0  // payload_unit_start_indicator
    
    // Пропуск адаптаційного поля якщо є
    offset := 4
    if packet[3]&0x20 != 0 {  // adaptation_field_control
        adaptationLength := int(packet[offset])
        offset += 1 + adaptationLength
    }
    
    // Якщо це початок payload і це PSI/SI потік (PID 0x0000-0x001F)
    if payloadStart && pid <= 0x001F {
        // Пропуск pointer_field якщо є
        if packet[3]&0x40 != 0 && offset < len(packet) {
            pointerField := int(packet[offset])
            offset += 1 + pointerField
        }
        
        // Спроба парсингу секції
        if offset < len(packet) {
            sectionData := packet[offset:]
            tableID := sectionData[0]
            
            _, err := v.ValidateAndParseSection(tableID, sectionData)
            if err != nil {
                v.metrics.ParseErrors.Inc()
                log.Printf("Channel %s: parse error for table_id=0x%02X: %v", 
                    v.channelID, tableID, err)
                // Не повертаємо помилку — продовжуємо обробку інших пакетів
            }
        }
    }
    
    return nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **CRC завжди не співпадає** | Неправильне початкове значення або відсутність фінальної інверсії | Переконайтеся, що використовуєте `crc=0xFFFFFFFF` на старті та `^0xFFFFFFFF` у кінці |
| **Паніка при access out of bounds** | Індексація таблиці з неправильним значенням | Перевірте що `b^byte(crc)` завжди дає значення 0-255 (це гарантовано типом byte) |
| **Повільне обчислення** | Великі секції або часті виклики | Використовуйте `calcCRC32` без проміжних аллокацій; кешуйте результати якщо дані не змінюються |
| **Невірний порядок байт у очікуваному CRC** | Big-endian vs little-endian плутанина | MPEG-TS використовує big-endian для CRC; використовуйте `pio.U32BE()` для читання |
| **Секція обрізана через помилки передачі** | `len(data) < 3+sectionLength+4` | Реалізуйте буферизацію та очікування повних секцій; відкидайте пошкоджені пакети |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування результатів CRC для незмінних секцій:

```go
type CRCCache struct {
    mu    sync.RWMutex
    cache map[uint64]uint32  // hash(data) → crc
}

func (c *CRCCache) ComputeOrGet(data []byte) uint32 {
    // Простий хеш для ключа (на практиці використовуйте proper hashing)
    key := hashBytes(data)
    
    c.mu.RLock()
    if crc, ok := c.cache[key]; ok {
        c.mu.RUnlock()
        return crc
    }
    c.mu.RUnlock()
    
    // Обчислення якщо не в кеші
    crc := ComputeSectionCRC32(data)
    
    c.mu.Lock()
    if c.cache == nil {
        c.cache = make(map[uint64]uint32)
    }
    c.cache[key] = crc
    c.mu.Unlock()
    
    return crc
}
```

### 2. Пакетне обчислення CRC для кількох секцій:

```go
// BatchComputeCRC32 — обчислення для кількох масивів за один виклик
func BatchComputeCRC32(datas [][]byte) []uint32 {
    results := make([]uint32, len(datas))
    for i, data := range datas {
        results[i] = ComputeSectionCRC32(data)
    }
    return results
}
```

### 3. Моніторинг продуктивності обчислень:

```go
type CRCMetrics struct {
    ComputeLatency prometheus.HistogramVec
    SectionsValidated prometheus.CounterVec
    CRCFailures    prometheus.CounterVec
}

func (m *CRCMetrics) RecordValidation(success bool, duration time.Duration, tableID uint8, channelID string) {
    m.SectionsValidated.WithLabelValues(fmt.Sprintf("0x%02X", tableID), channelID).Inc()
    m.ComputeLatency.WithLabelValues(channelID).Observe(duration.Seconds())
    if !success {
        m.CRCFailures.WithLabelValues(fmt.Sprintf("0x%02X", tableID), channelID).Inc()
    }
}
```

---

## 📋 Чек-лист інтеграції tsio.CRC32

```go
// ✅ 1. Правильне ініціалізування CRC
crc := uint32(0xFFFFFFFF)  // початкове значення для MPEG-TS

// ✅ 2. Обчислення для даних без включення самого CRC
payload := sectionData[:len(sectionData)-4]  // виключаємо останні 4 байти
crc = calcCRC32(crc, payload)

// ✅ 3. Фінальна інверсія біт
crc ^= 0xFFFFFFFF

// ✅ 4. Порівняння з очікуваним значенням (big-endian)
expectedCRC := pio.U32BE(sectionData[len(sectionData)-4:])
if crc != expectedCRC {
    // Обробка помилки
}

// ✅ 5. Обробка коротких/пошкоджених секцій
if len(data) < 8 {
    return fmt.Errorf("section too short for CRC validation")
}

// ✅ 6. Логування для дебагу
if crc != expectedCRC {
    log.Printf("CRC mismatch: computed=0x%08X, expected=0x%08X, table_id=0x%02X", 
        crc, expectedCRC, data[0])
}

// ✅ 7. Метрики для моніторингу
metrics.RecordValidation(crc == expectedCRC, time.Since(start), data[0], channelID)
```

---

## 🔗 Корисні посилання

- 📄 [ISO/IEC 13818-1:2022 — MPEG-TS Specification](https://www.iso.org/standard/82746.html) — офіційний стандарт (платний)
- 📄 [CRC32 Algorithm (Rocksoft Model)](https://reveng.sourceforge.net/crc-catalogue/1-31.htm#crc.cat.crc-32) — опис алгоритму
- 📄 [MPEG-TS PSI/SI Tables](https://www.dvb.org/resources/public/standards/a101-1_dvb-si_specification.pdf) — структура PAT/PMT/SDT
- 💻 [Go hash/crc32 Package](https://pkg.go.dev/hash/crc32) — стандартна бібліотека Go (порівняння)
- 🧪 [IEEE 802.3 CRC32 Polynomial](https://standards.ieee.org/ieee/802.3/7456/) — специфікація поліному

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **MPEG-TS потоками у реальному часі**:
> 1. **Завжди валідуйте CRC перед парсингом секцій** — пошкоджені дані можуть призвести до некоректної обробки або падіння програми.
> 2. **Використовуйте `calcCRC32` без проміжних аллокацій** — функція працює in-place, що критично для обробки тисяч пакетів на секунду.
> 3. **Кешуйте результати для статичних секцій** — PAT/PMT змінюються рідко, повторне обчислення CRC марнує ресурси.
> 4. **Логуйте CRC помилки з table_id** — це допомагає швидко виявити джерело проблем (погана мережа, пошкоджене джерело, тощо).
> 5. **Моніторьте `CRCComputeLatency`** — різке зростання може вказувати на перевантаження процесора або проблеми з пам'яттю.

Потрібен приклад інтеграції `TSSectionValidator` з вашим `flv.Demuxer` для перевірки цілісності секцій при конвертації FLV → MPEG-TS? Готовий допомогти! 🚀