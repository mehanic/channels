# Глибоке роз'яснення: `data.go` — ядро класифікації та агрегації даних у astits

Цей файл містить **фундаментальну логіку демуксера**: визначення типу даних у пакеті (PSI чи PES), агрегацію фрагментованих пакетів та повернення структурованих `*DemuxerData`. Це "мозок" вашого пайплайну.

---

## 🎯 Навіщо цей файл потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ data.go у контексті HLS-стрімінгу:     │
│                                         │
│ 🔹 Класифікація даних:                  │
│   • isPSIPayload() → PSI таблиці       │
│   • isPESPayload() → PES-потоки        │
│   • Custom parser → кастомна логіка    │
│                                         │
│ 🔹 Агрегація фрагментів:                │
│   • З'єднання payload кількох пакетів  │
│   • Видалення адаптаційних полів       │
│   • Буферизація через bytesPool        │
│                                         │
│ 🔹 Парсинг структур:                    │
│   • PSI: parsePSIData() → PAT/PMT/EIT  │
│   • PES: parsePESData() → відео/аудіо  │
│                                         │
│ 🔹 Для CCTV HLS:                        │
│   • Автоматична ідентифікація потоків  │
│   • Фільтрація непотрібних даних       │
│   • Підтримка orphan audio merge       │
└─────────────────────────────────────────┘
```

---

## 🔧 Типи даних: `DemuxerData` та `MuxerData`

### `DemuxerData` — універсальний контейнер для парсених даних

```go
type DemuxerData struct {
    EIT         *EITData    // 🎯 EIT таблиця (розклад передач)
    FirstPacket *Packet     // 🎯 перший пакет цієї даних (для метаданих)
    NIT         *NITData    // 🎯 NIT таблиця (мережева інформація)
    PAT         *PATData    // 🎯 PAT таблиця (список програм)
    PES         *PESData    // 🎯 PES дані (відео/аудіо потік)
    PID         uint16      // 🎯 PID потоку, з якого отримано дані
    PMT         *PMTData    // 🎯 PMT таблиця (опис програми)
    SDT         *SDTData    // 🎯 SDT таблиця (опис сервісів)
    TOT         *TOTData    // 🎯 TOT таблиця (час + таймзони)
}
```

> 💡 **Ключова ідея**: Тільки одне поле (PAT/PMT/PES/EIT...) буде заповнене в кожному екземплярі. Це discriminated union реалізація в Go.

### `MuxerData` — контейнер для запису даних

```go
type MuxerData struct {
    PID             uint16              // 🎯 PID для запису
    AdaptationField *PacketAdaptationField // 🎯 адаптаційне поле (PCR, stuffing)
    PES             *PESData            // 🎯 PES дані для запису
}
```

> 💡 **Використання**: `MuxerData` використовується при генерації вихідного TS-потоку (наприклад, для HLS сегментів).

---

## 🔍 Функція `parseData`: головний класифікатор

```go
func parseData(ps []*Packet, prs PacketsParser, pm *programMap) ([]*DemuxerData, error) {
    // 🔹 1. Виклик кастомного парсера (якщо є)
    if prs != nil {
        ds, skip, err := prs(ps)
        if err != nil {
            return nil, fmt.Errorf("astits: custom packets parsing failed: %w", err)
        }
        if skip {
            return ds, nil  // кастомний парсер обробив дані
        }
    }
    
    // 🔹 2. Розрахунок загальної довжини payload
    var l int
    for _, p := range ps {
        l += len(p.Payload)
    }
    
    // 🔹 3. Отримання буфера з пулу (економія пам'яті)
    payload := bytesPool.get(l)
    defer bytesPool.put(payload)  // повернути буфер у пул
    
    // 🔹 4. Копіювання payload всіх пакетів у один буфер
    var c int
    for _, p := range ps {
        c += copy(payload.s[c:], p.Payload)
    }
    
    // 🔹 5. Створення ітератора для парсингу
    i := astikit.NewBytesIterator(payload.s)
    
    // 🔹 6. Отримання PID з першого пакету
    pid := ps[0].Header.PID
    
    // 🔹 7. Копіювання заголовків першого пакету (для метаданих)
    fp := &Packet{
        Header:          ps[0].Header,
        AdaptationField: ps[0].AdaptationField,
    }
    
    // 🔹 8. Класифікація та парсинг за типом
    if pid == PIDCAT {
        // CAT: приватні дані, не парсимо (потрібен кастомний парсер)
        
    } else if isPSIPayload(pid, pm) {
        // 🔹 PSI: парсити таблиці (PAT/PMT/EIT...)
        psiData, err := parsePSIData(i)
        if err != nil {
            return nil, fmt.Errorf("astits: parsing PSI data failed: %w", err)
        }
        ds = psiData.toData(fp, pid)  // конвертація у []DemuxerData
        
    } else if isPESPayload(payload.s) {
        // 🔹 PES: парсити відео/аудіо потік
        pesData, err := parsePESData(i)
        if err != nil {
            return nil, fmt.Errorf("astits: parsing PES data failed: %w", err)
        }
        ds = []*DemuxerData{{
            FirstPacket: fp,
            PES:         pesData,
            PID:         pid,
        }}
    }
    
    return ds, nil
}
```

### 🎯 Ключові моменти реалізації

#### 1. Кастомний парсер (`PacketsParser`)

```go
// Тип функції для кастомної обробки:
type PacketsParser func(ps []*Packet) (ds []*DemuxerData, skip bool, err error)

// Приклад використання для orphan audio merge:
func createOrphanParser(cache *AudioCache) PacketsParser {
    return func(ps []*Packet) ([]*DemuxerData, bool, error) {
        if isAudioPID(ps[0].Header.PID) {
            // Спробувати знайти відповідне відео
            if video := cache.FindMatchingVideo(ps); video != nil {
                // Merge аудіо+відео → повернути готові дані
                merged := mergeAudioVideo(video, ps)
                return []*DemuxerData{merged}, true, nil  // skip=true
            }
            // Орфан: зберегти у кеш
            cache.StoreOrphan(ps)
            return nil, true, nil  // skip=true, нічого не повертати
        }
        // Не аудіо → стандартна обробка
        return nil, false, nil
    }
}
```

#### 2. `bytesPool` — оптимізація пам'яті

```go
// bytesPool.get(l) повертає буфер з пулу замість нової алокації
// bytesPool.put(payload) повертає буфер у пул для повторного використання

// Переваги:
// • Зменшення тиску на GC (garbage collector)
// • Менше алокацій = вища продуктивність
// • Особливо важливо для high-throughput стрімінгу

// Приклад використання:
payload := bytesPool.get(188 * 10)  // буфер для 10 пакетів
defer bytesPool.put(payload)        // обов'язково повернути!

// Копіювання даних:
copy(payload.s[0:], packet1.Payload)
copy(payload.s[188:], packet2.Payload)
// ... тепер payload.s містить об'єднані дані
```

#### 3. Копіювання заголовків першого пакету

```go
// Чому копіюємо, а не використовуємо оригінал?
fp := &Packet{
    Header:          ps[0].Header,          // копія значення (struct)
    AdaptationField: ps[0].AdaptationField, // копія посилання (pointer)
}

// Причина: оригінальні пакети можуть бути звільнені після parseData()
// Копіювання гарантує, що метадані залишаться валідними
```

---

## 🔍 Допоміжні функції класифікації

### `isPSIPayload`: чи це PSI таблиця?

```go
func isPSIPayload(pid uint16, pm *programMap) bool {
    return pid == PIDPAT ||  // ✅ PAT завжди на PID 0x0000
           pm.existsUnlocked(pid) ||  // ✅ PMT PID з programMap
           ((pid >= 0x10 && pid <= 0x14) || (pid >= 0x1e && pid <= 0x1f))  // ✅ DVB зарезервовані
}
```

**Логіка перевірки:**
```
1. PID == 0x0000 → PAT (Program Association Table) ✅
2. PID існує в programMap → PMT (Program Map Table) ✅
3. PID у діапазонах 0x10-0x14 або 0x1E-0x1F → DVB SI таблиці ✅
4. Інші PID → не PSI (можливо, PES або приватні дані)
```

> 💡 **Важливо**: `programMap` заповнюється після парсингу PAT: `programMap[PMT_PID] = program_number`.

### `isPESPayload`: чи це PES потік?

```go
func isPESPayload(i []byte) bool {
    // 🔹 Перевірка мінімальної довжини
    if len(i) < 3 {
        return false
    }
    
    // 🔹 Перевірка PES start code prefix: 0x000001
    return uint32(i[0])<<16 | uint32(i[1])<<8 | uint32(i[2]) == 1
}
```

**Формат PES start code:**
```
Байти: [0x00][0x00][0x01] = 24 біти = 1 (десяткове)

Розрахунок:
  (0x00 << 16) | (0x00 << 8) | 0x01 = 0 | 0 | 1 = 1 ✅

Це універсальний маркер для:
• Відео потоків (0xE0-0xEF)
• Аудіо потоків (0xC0-0xDF)
• Приватних даних (0xBD, 0xFD...)
```

---

## 🔍 Функції перевірки цілісності: `isPSIComplete` / `isPESComplete`

### `isPSIComplete`: чи достатньо пакетів для парсингу PSI?

```go
func isPSIComplete(ps []*Packet) bool {
    // 🔹 1. Об'єднати payload всіх пакетів
    var l int
    for _, p := range ps { l += len(p.Payload) }
    
    payload := bytesPool.get(l)
    defer bytesPool.put(payload)
    
    var o int
    for _, p := range ps {
        o += copy(payload.s[o:], p.Payload)
    }
    
    // 🔹 2. Створити ітератор
    i := astikit.NewBytesIterator(payload.s)
    
    // 🔹 3. Пропустити pointer_field
    b, _ := i.NextByte()
    i.Skip(int(b))
    
    // 🔹 4. Цикл по секціях
    for i.HasBytesLeft() {
        // Читати table_id
        b, _ = i.NextByte()
        if shouldStopPSIParsing(PSITableID(b)) {
            break  // Null або Unknown таблиця
        }
        
        // Читати section_length (12 біт)
        bs, _ := i.NextBytesNoCopy(2)
        sectionLength := int(binary.BigEndian.Uint16(bs) & 0x0fff)
        
        // Пропустити секцію
        i.Skip(sectionLength)
    }
    
    // 🔹 5. Перевірити, чи вистачило даних
    return i.Len() >= i.Offset()  // true = всі секції прочитані
}
```

**Коли використовувати:**
```
✅ Перед парсингом: чи достатньо пакетів для валідної секції?
✅ У packetPool: чи можна вже повертати агреговані дані?
✅ Для оптимізації: не парсити неповні дані
```

### `isPESComplete`: чи повний PES пакет?

```go
func isPESComplete(ps []*Packet) bool {
    // 🔹 1. Об'єднати payload (як у isPSIComplete)
    // ... код об'єднання ...
    
    // 🔹 2. Пропустити PES prefix (0x000001)
    i.Seek(3)
    
    // 🔹 3. Парсити PES header для отримання packet_length
    h, _, dataEnd, err := parsePESHeader(i)
    if err != nil {
        return false
    }
    
    // 🔹 4. Особлива обробка для video PES (packet_length = 0)
    if h.PacketLength == 0 {
        // Для відео: немає способу дізнатися довжину заздалегідь
        // Повертаємо false → чекати більше пакетів або EOF
        return false
    }
    
    // 🔹 5. Перевірити, чи вистачило даних до dataEnd
    return i.Len() >= dataEnd
}
```

> ⚠️ **Важливо**: Для відео потоків `PacketLength` може бути 0 (необмежена довжина). У цьому випадку `isPESComplete` завжди повертає `false` — демуксер покладається на зміну `PayloadUnitStartIndicator=1` для детекції нового PES.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Кастомний парсер для orphan audio merge

```go
// У segmentAssembler — ручна агрегація аудіо/відео за seqNum:
func createOrphanAudioParser(audioCache *AudioCache) astits.PacketsParser {
    return func(ps []*astits.Packet) ([]*astits.DemuxerData, bool, error) {
        if len(ps) == 0 {
            return nil, false, nil
        }
        
        pid := ps[0].Header.PID
        
        // 🔹 Якщо це аудіо-потік — спробувати merge з відео
        if isAudioPID(pid) {
            // Знайти відповідне відео за seqNum (з вашої логіки)
            videoPackets := audioCache.FindMatchingVideo(ps)
            if videoPackets != nil {
                // Merge аудіо+відео → створити валідну DemuxerData
                merged := mergeAudioVideoPackets(videoPackets, ps)
                return []*astits.DemuxerData{merged}, true, nil
            }
            
            // Орфан: зберегти у кеш для пізнішого merge
            audioCache.StoreOrphan(ps)
            return nil, true, nil  // skip стандартну обробку
        }
        
        // 🔹 Для відео — стандартна обробка
        return nil, false, nil
    }
}

// Застосування:
dmx := astits.NewDemuxer(ctx, reader,
    astits.DemuxerOptPacketsParser(createOrphanAudioParser(audioCache)),
)
```

### ✅ 2: Фільтрація непотрібних PID на ранньому етапі

```go
// У channel-aware архітектурі — відкидати непотрібні потоки:
func createPIDFilter(expectedPIDs map[uint16]bool) astits.PacketSkipper {
    return func(p *astits.Packet) bool {
        // 🔹 Пропустити пакети з неочікуваними PID
        return !expectedPIDs[p.Header.PID]
    }
}

// Використання:
expectedPIDs := map[uint16]bool{
    0x0000: true,  // PAT
    0x1000: true,  // PMT для програми 1
    0x101:  true,  // Відео потік
    0x102:  true,  // Аудіо потік
}

dmx := astits.NewDemuxer(ctx, reader,
    astits.DemuxerOptPacketSkipper(createPIDFilter(expectedPIDs)),
)
```

### ✅ 3: Моніторинг типів даних для відладки

```go
// monitoring.Monitor — метрики для класифікації даних:
type DataMetrics struct {
    PSIPacketsParsed  *prometheus.CounterVec  // кількість PSI по типу
    PESPacketsParsed  *prometheus.CounterVec  // кількість PES по PID
    CustomParserHits  *prometheus.CounterVec  // виклики кастомного парсера
    UnknownDataTypes  *prometheus.CounterVec  // нерозпізнані типи
    BytesAggregated   *prometheus.CounterVec  // загальний обсяг об'єднаних даних
}

// У parseData (модифікована версія):
func parseDataWithMetrics(ps []*astits.Packet, parser astits.PacketsParser, 
                          pm *programMap, channelID string, metrics *DataMetrics) ([]*astits.DemuxerData, error) {
    
    // 🔹 Розрахувати загальний обсяг
    var totalBytes int
    for _, p := range ps {
        totalBytes += len(p.Payload)
    }
    metrics.BytesAggregated.WithLabelValues(channelID).Add(float64(totalBytes))
    
    // 🔹 Кастомний парсер
    if parser != nil {
        ds, skip, err := parser(ps)
        if skip {
            metrics.CustomParserHits.WithLabelValues(channelID).Inc()
            return ds, err
        }
    }
    
    // 🔹 Визначити тип даних
    if len(ps) > 0 {
        pid := ps[0].Header.PID
        if isPSIPayload(pid, pm) {
            metrics.PSIPacketsParsed.WithLabelValues(channelID, psiTypeName(pid)).Inc()
        } else if isPESPayload(ps[0].Payload) {
            metrics.PESPacketsParsed.WithLabelValues(channelID, fmt.Sprintf("pid_%d", pid)).Inc()
        } else {
            metrics.UnknownDataTypes.WithLabelValues(channelID).Inc()
        }
    }
    
    // Стандартна обробка...
    return parseData(ps, nil, pm)
}
```

### ✅ 4: Оптимізація через bytesPool

```go
// Якщо ви хочете налаштувати розмір пулу для вашого навантаження:
func configureBytesPool(maxBufferSize int, poolSize int) {
    // astits використовує внутрішній bytesPool
    // Ви можете налаштувати параметри через конфігурацію демуксера
    
    // Приклад: збільшити пул для high-throughput стрімінгу
    dmx := astits.NewDemuxer(ctx, reader,
        astits.DemuxerOptPacketSize(188),  // явний розмір
        // Інші опції...
    )
    
    // Моніторинг ефективності пулу:
    metrics.BytesPoolHits.WithLabelValues("demux").Inc()  // при успішному get()
    metrics.BytesPoolMisses.WithLabelValues("demux").Inc()  // при створенні нового буфера
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на класифікацію PSI vs PES

```go
func TestDataClassification(t *testing.T) {
    // 🔹 PSI: PAT на PID 0x0000
    patPacket := &astits.Packet{
        Header: astits.PacketHeader{PID: 0x0000},
        Payload: []byte{0x00, 0x00, 0xb0, 0x0d, /* ... PAT payload ... */},
    }
    
    assert.True(t, isPSIPayload(0x0000, newProgramMap()))
    assert.False(t, isPESPayload(patPacket.Payload))
    
    // 🔹 PES: відео на PID 0x101 з start code 0x000001
    pesPacket := &astits.Packet{
        Header: astits.PacketHeader{PID: 0x0101},
        Payload: []byte{0x00, 0x00, 0x01, 0xE0, /* ... PES header ... */},
    }
    
    pm := newProgramMap()
    pm.setUnlocked(0x101, 1)  // додати PMT PID
    
    assert.False(t, isPSIPayload(0x101, pm))  // це не PSI, це елементарний потік
    assert.True(t, isPESPayload(pesPacket.Payload))
    
    // 🔹 Unknown: приватні дані без start code
    unknownPacket := &astits.Packet{
        Header: astits.PacketHeader{PID: 0x1FF0},
        Payload: []byte{0xDE, 0xAD, 0xBE, 0xEF},
    }
    
    assert.False(t, isPSIPayload(0x1FF0, pm))
    assert.False(t, isPESPayload(unknownPacket.Payload))
    // → parseData поверне порожній результат
}
```

### 🔹 Тест на агрегацію фрагментованих даних

```go
func TestParseData_FragmentedPSI(t *testing.T) {
    // Створити фрагментовану PAT (2 пакети)
    patPayload := generatePATPayload()  // ваша helper-функція
    
    packet1 := &astits.Packet{
        Header: astits.PacketHeader{
            PID: 0x0000,
            PayloadUnitStartIndicator: true,  // початок фрагмента
        },
        Payload: patPayload[:100],  // перші 100 байт
    }
    
    packet2 := &astits.Packet{
        Header: astits.PacketHeader{
            PID: 0x0000,
            PayloadUnitStartIndicator: false,  // продовження
        },
        Payload: patPayload[100:],  // решта байт
    }
    
    packets := []*astits.Packet{packet1, packet2}
    
    // Парсинг
    ds, err := parseData(packets, nil, newProgramMap())
    assert.NoError(t, err)
    assert.Len(t, ds, 1)
    assert.NotNil(t, ds[0].PAT)  // ✅ PAT успішно зібрана з фрагментів
}
```

### 🔹 Тест на кастомний парсер

```go
func TestParseData_CustomParser(t *testing.T) {
    // Кастомний парсер: завжди повертає фіксовані дані
    customData := []*astits.DemuxerData{{PID: 999}}
    customParser := func(ps []*astits.Packet) ([]*astits.DemuxerData, bool, error) {
        return customData, true, nil  // skip=true → стандартна обробка пропускається
    }
    
    // Виклик parseData з кастомним парсером
    ds, err := parseData([]*astits.Packet{}, customParser, nil)
    
    assert.NoError(t, err)
    assert.Equal(t, customData, ds)  // ✅ кастомний парсер перевизначив логіку
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| PES не розпізнається | `isPESPayload` повертає false для валідних даних | Перевірити, що payload не зміщений через адаптаційне поле; використовувати `payloadOffset()` для коректного початку |
| PSI не парситься | `parseData` повертає помилку для відомих PID | Перевірити, що `programMap` оновлено після парсингу PAT; додати логування `isPSIPayload` результатів |
| Фрагментація не працює | Великі PES обрізаються | Перевірити, що `PayloadUnitStartIndicator=1` тільки на першому пакеті фрагмента; packetPool має збирати пакети до завершення |
| Кастомний парсер не викликається | `skip=true` не спрацьовує | Перевірити порядок: кастомний парсер має викликатися ДО стандартної логіки у `parseData` |
| bytesPool вичерпується | Високе споживання пам'яті, паніки | Збільшити розмір пулу; перевірити, що `defer bytesPool.put()` викликається завжди |

### Приклад діагностики класифікації:

```go
func debugPayloadClassification(pid uint16, payload []byte, pm *programMap) {
    log.Infof("Classifying payload: PID=0x%04X, len=%d, first_bytes=%X", 
        pid, len(payload), payload[:min(8, len(payload))])
    
    if isPSIPayload(pid, pm) {
        log.Infof("  → Detected as PSI payload")
    } else if isPESPayload(payload) {
        log.Infof("  → Detected as PES payload (start_code=0x000001)")
    } else {
        log.Warnf("  → Unknown payload type")
    }
}

func min(a, b int) int {
    if a < b { return a }
    return b
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Визначення типу даних:
func classifyPacketData(pkt *astits.Packet, pm *programMap) string {
    if isPSIPayload(pkt.Header.PID, pm) {
        return "PSI"
    }
    if isPESPayload(pkt.Payload) {
        return "PES"
    }
    return "Unknown"
}

// 2: Кастомний парсер для специфічних потреб:
func createMetadataParser(channelID string) astits.PacketsParser {
    return func(ps []*astits.Packet) ([]*astits.DemuxerData, bool, error) {
        // Приклад: витягувати тільки метадані, ігнорувати медіа
        if len(ps) == 0 {
            return nil, false, nil
        }
        
        pid := ps[0].Header.PID
        if isPSIPayload(pid, nil) {
            // Парсити тільки PSI, пропустити PES
            return parseData(ps, nil, nil)  // стандартний парсинг
        }
        
        // Ігнорувати PES для цього каналу
        return nil, true, nil
    }
}

// 3: Обробка помилок парсингу:
func safeParseData(ps []*astits.Packet, parser astits.PacketsParser, 
                   pm *programMap, channelID string) ([]*astits.DemuxerData, error) {
    ds, err := parseData(ps, parser, pm)
    if err != nil {
        log.Warnf("Channel %s: parseData error for %d packets: %v", 
            channelID, len(ps), err)
        
        // Спробувати відновити: пропустити пошкоджені пакети
        if len(ps) > 1 {
            log.Debugf("  First packet: PID=%d, payload_len=%d, first_bytes=%X",
                ps[0].Header.PID, len(ps[0].Payload), ps[0].Payload[:min(8, len(ps[0].Payload))])
        }
        
        // Повернути порожній результат замість помилки
        return nil, nil
    }
    return ds, nil
}

// 4: Моніторинг типів даних:
func trackDataTypes(ds []*astits.DemuxerData, channelID string, metrics *DataMetrics) {
    for _, d := range ds {
        if d.PAT != nil {
            metrics.PSIPacketsParsed.WithLabelValues(channelID, "PAT").Inc()
        } else if d.PMT != nil {
            metrics.PSIPacketsParsed.WithLabelValues(channelID, "PMT").Inc()
        } else if d.PES != nil {
            metrics.PESPacketsParsed.WithLabelValues(channelID, fmt.Sprintf("pid_%d", d.PID)).Inc()
        } else if d.EIT != nil {
            metrics.PSIPacketsParsed.WithLabelValues(channelID, "EIT").Inc()
        }
    }
}
```

---

## 📊 Матриця рішень для різних сценаріїв

```
Сценарій                     | Використати          | Чому
─────────────────────────────┼──────────────────────┼─────────────────────────────
Стандартний потік           | parseData(..., nil)  | Базова логіка достатня
Орфан аудіо/відео merge     | Custom PacketsParser | Ручна агрегація за seqNum
Channel-aware фільтрація    | Custom PacketsParser | Відкидати непотрібні програми
Діагностика / відладка      | NextPacket() + ручна | Низькорівневий контроль
Файловий аналіз             | Rewind() + двопрохід | Збір метаданих перед обробкою
High-throughput стрімінг    | bytesPool оптимізація| Зменшення алокацій пам'яті
```

---

## 📚 Корисні посилання

- [astits parseData source](https://github.com/asticode/go-astits/blob/master/data.go)
- [MPEG-TS PSI/PES specification](https://www.iso.org/standard/61236.html)
- [ETSI EN 300 468: DVB SI tables](https://www.etsi.org/deliver/etsi_en/300400_300499/300468/)
- [Go sync.Pool best practices](https://www.alexedwards.net/blog/using-sync-pool-in-go)

> 💡 **Ключова ідея**: `parseData` — це "мозок" демуксера, що вирішує: *що це за дані і як їх обробити?* У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Автоматично розрізняти PSI/PES для коректної агрегації
> - 🧩 Реалізувати кастомну логіку через PacketsParser (напр., orphan audio merge)
> - 🔍 Фільтрувати непотрібні дані на ранньому етапі (економія CPU/пам'яті)
> - 📊 Збирати метрики про типи даних для моніторингу якості потоку

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати кастомний PacketsParser у ваш `segmentAssembler` для orphan audio merge
- 🧪 Написати integration-тест для перевірки агрегації фрагментованих PES
- 📈 Додати Prometheus-метрики для моніторингу типів даних по каналах

🛠️