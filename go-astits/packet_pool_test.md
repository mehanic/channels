# Глибоке роз'яснення: Тести детекції розривів та `packetPool` у astits

Цей файл тестує **критичну логіку цілісності потоку**: детекцію розривів (discontinuity), дублікатів пакетів та механізм буферизації для збору фрагментованих даних (PES/PSI таблиці).

---

## 🎯 Навіщо це потрібно у вашому пайплайні?

```
┌─────────────────────────────────────────┐
│ Проблеми, які вирішують ці тести:       │
│                                         │
│ 🔹 Втрата пакетів у мережі/диску        │
│   → Continuity Counter "стрибає"        │
│   → Потрібно вставити #EXT-X-DISCONTINUITY│
│                                         │
│ 🔹 Дублікати при ретрансмісії           │
│   → Той самий Continuity Counter        │
│   → Потрібно відкинути, щоб не зламати  │
│     PTS синхронізацію                   │
│                                         │
│ 🔹 Фрагментація PES-пакетів             │
│   → Один логічний пакет = кілька TS     │
│   → Потрібен буфер для збору цілого     │
│                                         │
│ 🔹 Рідкісні таблиці (Teletext, EIT)     │
│   → Надходять нерегулярно               │
│   → Не можна відкидати через "дивний" CC│
└─────────────────────────────────────────┘
```

---

## 🔧 Функція `hasDiscontinuity` — логіка детекції розривів

### Сигнатура (гіпотетична)
```go
func hasDiscontinuity(prev []*Packet, curr *Packet) bool
```

### Розбір тест-кейсів

| Кейс | Попередній CC | Поточний CC | Умови | Очікуваний результат | Пояснення |
|------|--------------|-------------|-------|---------------------|-----------|
| 1 | 15 | 0 | `HasPayload=true` | ❌ False | ✅ Нормальний wrap-around (15→0) |
| 2 | 15 | 15 | — | ❌ False | ✅ Дублікат, а не розрив |
| 3 | 15 | 0 | `DiscontinuityIndicator=true` | ❌ False | ✅ Явний індикатор розриву — це **очікувана** подія, не помилка |
| 4 | 15 | — | `PID=PIDNull` (0x1FFF) | ❌ False | ✅ NULL-пакети ігноруються |
| 5 | — | 5 | Порожній history | ❌ False | ✅ Перший пакет не може бути розривом |
| 6 | 15 | 1 | `HasPayload=true` | ✅ True | ❌ Пропуск: очікувався 0, отримали 1 → втрата пакетів |
| 7 | 15 | 0 | `HasPayload=false` | ✅ True | ❌ Пакет тільки з адаптаційним полем, але CC=0 після 15 → підозріло |

### Алгоритм (реконструкція)

```go
func hasDiscontinuity(prev []*Packet, curr *Packet) bool {
    // 1. Ігнорувати NULL пакети
    if curr.Header.PID == PIDNull {
        return false
    }
    
    // 2. Якщо немає історії — не може бути розриву
    if len(prev) == 0 {
        return false
    }
    
    last := prev[len(prev)-1]
    
    // 3. Явний індикатор розриву в адаптаційному полі — це "легальний" розрив
    if curr.AdaptationField != nil && curr.AdaptationField.DiscontinuityIndicator {
        return false  // не помилка, а сигналізована подія
    }
    
    // 4. Дублікати не є розривами
    if curr.Header.ContinuityCounter == last.Header.ContinuityCounter {
        return false
    }
    
    // 5. Очікуване наступне значення (з урахуванням wrap-around)
    expected := (last.Header.ContinuityCounter + 1) % 16
    
    // 6. Якщо CC збігається з очікуваним — все добре
    if curr.Header.ContinuityCounter == expected {
        return false
    }
    
    // 7. Спеціальний випадок: пакет без payload може мати "завмерлий" CC
    // Але якщо ми очікували 0, а отримали щось інше без payload — це підозріло
    if !curr.Header.HasPayload && expected == 0 {
        // Дозволити деяку гнучкість для пакетів тільки з адаптаційним полем
        // Але в тесті кейс 7 повертає True → значить, така ситуація вважається розривом
    }
    
    // 8. Все інше — розрив
    return true
}
```

> 💡 **Важливо**: `DiscontinuityIndicator=true` **не** повертає `true` з функції, бо це **сигналізований** розрив, а не помилка. Ваша логіка має обробляти його окремо (напр., вставити `#EXT-X-DISCONTINUITY`).

---

## 🔧 Функція `isSameAsPrevious` — детекція дублікатів

```go
func TestIsSameAsPrevious(t *testing.T) {
    // Кейс 1: однаковий CC, але без payload → не дублікат (може бути AF-only пакет)
    assert.False(t, isSameAsPrevious(
        []*Packet{{Header: PacketHeader{ContinuityCounter: 1}}}, 
        &Packet{Header: PacketHeader{ContinuityCounter: 1}}))
    
    // Кейс 2: різний CC → очевидно не дублікат
    assert.False(t, isSameAsPrevious(
        []*Packet{{Header: PacketHeader{ContinuityCounter: 1}}}, 
        &Packet{Header: PacketHeader{ContinuityCounter: 2, HasPayload: true}}))
    
    // Кейс 3: однаковий CC + HasPayload=true → дублікат!
    assert.True(t, isSameAsPrevious(
        []*Packet{{Header: PacketHeader{ContinuityCounter: 1}}}, 
        &Packet{Header: PacketHeader{ContinuityCounter: 1, HasPayload: true}}))
}
```

### Алгоритм

```go
func isSameAsPrevious(prev []*Packet, curr *Packet) bool {
    if len(prev) == 0 {
        return false
    }
    last := prev[len(prev)-1]
    
    // Дублікат = однаковий CC + обидва мають payload
    return last.Header.ContinuityCounter == curr.Header.ContinuityCounter &&
           last.Header.HasPayload && 
           curr.Header.HasPayload
}
```

**Використання у вашому пайплайні:**
```go
// У segmentAssembler — відкидати дублікати до обробки:
if isSameAsPrevious(packetHistory, newPacket) {
    metrics.DuplicatePackets.WithLabelValues(channelID).Inc()
    continue  // пропустити, не ламати синхронізацію
}
```

---

## 🗂️ `packetPool` — буфер для збору фрагментованих даних

### Призначення

```
┌─────────────────────────────────────────┐
│ Проблема:                               │
│ • Один PES-пакет може займати 10+ TS    │
│   пакетів (особливо відео ключові кадри)│
│ • Потрібно зібрати всі частини перед    │
│   обробкою (парсинг PES-заголовка, PTS) │
│                                         │
│ Рішення: packetPool                     │
│ • Групує пакети за (PID, PUSI-групи)   │
│ • Повертає готові "логічні пакети"      │
│   коли набралася повна група           │
└─────────────────────────────────────────┘
```

### Ключові концепції

| Термін | Опис |
|--------|------|
| `PUSI` (Payload Unit Start Indicator) | Прапорець у заголовку: `1` = початок нового логічного блоку (PES/PSI) |
| `Група` | Послідовність пакетів одного PID між двома PUSI=1 |
| `dumpUnlocked()` | Повертає зібрані групи в порядку завершення |

### Розбір `TestPacketPool`

```go
b := newPacketPool(nil)

// 1. CC=0, PUSI=1, PID=1 → початок нової групи (ще не завершена)
ps := b.addUnlocked(&Packet{Header: PacketHeader{
    ContinuityCounter: 0, HasPayload: true, PID: 1, PayloadUnitStartIndicator: true}})
assert.Len(t, ps, 0)  // ✅ Нічого не повернуто — група ще збирається

// 2. CC=1, PUSI=1, PID=1 → ПОЧАТОК НОВОЇ групи → попередня (з кроку 1) завершена!
ps = b.addUnlocked(&Packet{Header: PacketHeader{
    ContinuityCounter: 1, HasPayload: true, PayloadUnitStartIndicator: true, PID: 1}})
assert.Len(t, ps, 1)  // ✅ Повернуто 1 завершений пакет (з кроку 1)

// 3. Той самий CC/PUSI, але PID=2 → інший потік, не впливає на PID=1
ps = b.addUnlocked(&Packet{Header: PacketHeader{..., PID: 2}})
assert.Len(t, ps, 0)  // ✅ Група для PID=2 щойно почалася

// 4. CC=2, PID=1, без PUSI → продовження поточної групи
ps = b.addUnlocked(&Packet{Header: PacketHeader{ContinuityCounter: 2, ..., PID: 1}})
assert.Len(t, ps, 0)  // ✅ Ще збираємо

// 5. CC=3, PUSI=1, PID=1 → нова група → попередня (кроки 2+4) завершена!
ps = b.addUnlocked(&Packet{Header: PacketHeader{ContinuityCounter: 3, PUSI: true, PID: 1}})
assert.Len(t, ps, 2)  // ✅ Повернуто 2 пакети: з кроку 2 і кроку 4

// 6. CC=5, PID=1 → ПРОПУСК (очікувався 4) → розрив! Скидаємо групу
ps = b.addUnlocked(&Packet{Header: PacketHeader{ContinuityCounter: 5, ..., PID: 1}})
assert.Len(t, ps, 0)  // ✅ Група відкинута через розрив

// 7-8. Нова група з CC=6,7 → збирається нормально

// 9. dumpUnlocked() → повертає всі незавершені групи в порядку додавання
ps = b.dumpUnlocked()
assert.Len(t, ps, 2)  // ✅ Дві групи: одна з PID=1, інша з PID=2
assert.Equal(t, uint16(1), ps[0].Header.PID)  // Перша додана

ps = b.dumpUnlocked()
assert.Len(t, ps, 1)   // ✅ Друга група
assert.Equal(t, uint16(2), ps[0].Header.PID)

ps = b.dumpUnlocked()
assert.Len(t, ps, 0)   // ✅ Все повернуто
```

### Візуалізація стану пулу

```
Крок 1: PID=1, CC=0, PUSI=1
┌─────────────────┐
│ PID=1: [pkt0*]  │  * = початок групи
└─────────────────┘
→ повертає: []

Крок 2: PID=1, CC=1, PUSI=1 (нова група!)
┌─────────────────┐
│ PID=1: [pkt1*]  │  pkt0 завершено → повертаємо
└─────────────────┘
→ повертає: [pkt0]

Крок 4: PID=1, CC=2, PUSI=0 (продовження)
┌─────────────────┐
│ PID=1: [pkt1*, pkt2] │
└─────────────────┘
→ повертає: []

Крок 5: PID=1, CC=3, PUSI=1 (нова група!)
┌─────────────────┐
│ PID=1: [pkt3*]  │  pkt1+pkt2 завершено → повертаємо
└─────────────────┘
→ повертає: [pkt1, pkt2]
```

---

## 📡 `TestPacketPoolWithRarePackets` — обробка нерегулярних потоків

### Контекст: DVB Teletext

```
• PID 1004 — типовий для Teletext у європейських потоках
• Пакети надходять рідко: раз на кілька секунд
• Кожен пакет має `PUSI=1` (самодостатній)
• Continuity Counter може "стрибати" через довгі паузи
```

### Тест перевіряє:

```go
payloadDVBTeletext := hexToBytes(`000001bd00b284...`)  // Реальний PES з телетекстом

b := newPacketPool(nil)

// Додаємо 11 пакетів з "іржавим" CC: 0,1,2,3,3,4,6,7,7,9,10
// (пропуски: 3→3 дублікат, 4→6 пропуск, 7→7 дублікат, 8 пропущено)

for _, cc := range []int{0,1,2,3,3,4,6,7,7,9,10} {
    ps := b.addUnlocked(&Packet{
        Header: PacketHeader{
            ContinuityCounter: uint8(cc),
            HasPayload: true,
            PayloadUnitStartIndicator: true,  // ⚠️ Кожен пакет — самодостатній!
            PID: 1004,
        },
        Payload: payloadDVBTeletext,
    })
    assert.Len(t, ps, 1)  // ✅ Кожен пакет одразу повертається!
}

// Чому завжди повертає 1 пакет?
// Тому що PUSI=1 означає: "цей пакет починає І завершує логічний блок"
// → не потрібно буферизувати, можна одразу обробляти

// В кінці пул порожній — все повернуто
ps = b.dumpUnlocked()
assert.Len(t, ps, 0)
```

> 💡 **Ключовий інсайт**: Якщо `PUSI=1` **і** пакет має повний payload — він самодостатній. `packetPool` розуміє це і не буферизує зайве.

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Детекція розривів для HLS `#EXT-X-DISCONTINUITY`

```go
// У VideoManifestProxy або segmentFinalizer:
func shouldInsertDiscontinuity(prevPackets []*astits.Packet, curr *astits.Packet, channelID string) bool {
    // 1. Базова перевірка на розрив
    if hasDiscontinuity(prevPackets, curr) {
        log.Warnf("Channel %s: discontinuity detected (PID=%d, CC=%d)", 
            channelID, curr.Header.PID, curr.Header.ContinuityCounter)
        return true
    }
    
    // 2. Додаткова логіка для вашого пайплайну:
    //    - Розрив у часовій мітці > 1 секунди
    //    - Зміна параметрів кодека (SPS/PPS у H.264)
    //    - Явний DiscontinuityIndicator
    
    if curr.AdaptationField != nil && curr.AdaptationField.DiscontinuityIndicator {
        log.Infof("Channel %s: signaled discontinuity (PID=%d)", channelID, curr.Header.PID)
        return true
    }
    
    return false
}

// Використання при генерації плейлиста:
if shouldInsertDiscontinuity(lastPackets[pid], newPacket, channelID) {
    playlist.AddDiscontinuity()
    // Скинути стан синхронізації для цього PID
    syncState[pid].Reset()
}
```

### ✅ 2. Фільтрація дублікатів у `segmentAssembler`

```go
// У обробці вхідного потоку:
type PIDTracker struct {
    lastPacket *astits.Packet
    pool       *packetPool  // для збору фрагментованих PES
}

func (t *PIDTracker) ProcessPacket(pkt *astits.Packet) ([]*astits.Packet, error) {
    // 1. Відкинути дублікати
    if isSameAsPrevious([]*astits.Packet{t.lastPacket}, pkt) {
        metrics.DuplicatePackets.WithLabelValues(channelID).Inc()
        return nil, nil  // не помилка, просто ігноруємо
    }
    
    // 2. Додати у пул для збору
    completed := t.pool.addUnlocked(pkt)
    
    // 3. Оновити історію
    t.lastPacket = pkt
    
    // 4. Повернути готові до обробки пакети
    return completed, nil
}

// У main loop:
tracker := pidTrackers[pkt.Header.PID]
if tracker == nil {
    tracker = &PIDTracker{pool: newPacketPool(nil)}
    pidTrackers[pkt.Header.PID] = tracker
}

completed, err := tracker.ProcessPacket(pkt)
if err != nil {
    log.Errorf("Error processing packet: %v", err)
    continue
}

for _, cp := range completed {
    // Обробити повний логічний пакет (напр., парсити PES)
    processCompletePacket(cp, channelID)
}
```

### ✅ 3. Обробка рідкісних таблиць (EIT/SDT/Teletext)

```go
// Для каналів з телетекстом або EIT:
func handleRareDataPacket(pkt *astits.Packet, handler func(*astits.Packet)) {
    // Рідкісні таблиці часто мають PUSI=1 і самодостатні
    if pkt.Header.PayloadUnitStartIndicator && len(pkt.Payload) > 0 {
        // Можна обробляти одразу, без буферизації
        handler(pkt)
    } else {
        // Якщо фрагментований — використати packetPool
        pool := rareDataPools[pkt.Header.PID]
        if pool == nil {
            pool = newPacketPool(nil)
            rareDataPools[pkt.Header.PID] = pool
        }
        
        completed := pool.addUnlocked(pkt)
        for _, cp := range completed {
            handler(cp)
        }
    }
}
```

### ✅ 4. Моніторинг цілісності потоку

```go
// monitoring.Monitor — метрики для continuity-логіки:
type Metrics struct {
    TSDiscontinuities *prometheus.CounterVec  // виявлені розриви
    TSDuplicates    *prometheus.CounterVec  // відкинуті дублікати
    TSPacketGaps    *prometheus.HistogramVec  // розмір пропусків у CC
}

// У обробці пакетів:
if hasDiscontinuity(history, pkt) {
    metrics.TSDiscontinuities.WithLabelValues(channelID, fmt.Sprintf("pid_%d", pkt.Header.PID)).Inc()
    
    // Записати розмір пропуску для аналізу
    gap := calculateCCGap(history, pkt)
    metrics.TSPacketGaps.WithLabelValues(channelID).Observe(float64(gap))
}

if isSameAsPrevious(history, pkt) {
    metrics.TSDuplicates.WithLabelValues(channelID).Inc()
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на wrap-around у складних сценаріях

```go
func TestHasDiscontinuity_WrapAround(t *testing.T) {
    // Сценарій: 14→15→0→1 (нормальний цикл)
    history := []*Packet{
        {Header: PacketHeader{ContinuityCounter: 14, HasPayload: true}},
        {Header: PacketHeader{ContinuityCounter: 15, HasPayload: true}},
    }
    
    // 15→0 — нормальний wrap
    assert.False(t, hasDiscontinuity(history, &Packet{
        Header: PacketHeader{ContinuityCounter: 0, HasPayload: true}}))
    
    // Але 15→2 — пропуск (втрачено 0 і 1)
    assert.True(t, hasDiscontinuity(history, &Packet{
        Header: PacketHeader{ContinuityCounter: 2, HasPayload: true}}))
}
```

### 🔹 Тест на обробку орфан-аудіо з розривами

```go
func TestPacketPool_OrphanAudioWithGaps(t *testing.T) {
    // Сценарій: аудіо-потік з пропусками (орфан, що чекає на відео)
    pool := newPacketPool(nil)
    
    // Аудіо-пакети з пропусками у CC
    ccs := []int{0, 1, 3, 4, 6}  // пропущено 2 і 5
    
    var completed []*Packet
    for _, cc := range ccs {
        pkt := &Packet{
            Header: PacketHeader{
                PID: audioPID,
                ContinuityCounter: uint8(cc),
                HasPayload: true,
                PayloadUnitStartIndicator: cc%2 == 0,  // PUSI на парних
            },
            Payload: generateAACFrame(),
        }
        completed = append(completed, pool.addUnlocked(pkt)...)
    }
    
    // Очікуємо: групи з розривами мають скидатися
    // Тільки пакети з коректною послідовністю повертаються
    assert.Less(t, len(completed), 5)  // Не всі пакети пройшли
}
```

### 🔹 Інтеграційний тест з реальним потоком

```go
func TestPacketPool_FromRealStream(t *testing.T) {
    data, err := os.ReadFile("testdata/al_araby_sample.ts")
    require.NoError(t, err)
    
    dmx := astits.NewDemuxer(context.Background(), bytes.NewReader(data))
    pools := make(map[uint16]*packetPool)
    
    var processedPES int
    
    for {
        pkt, err := dmx.NextPacket()
        if errors.Is(err, astits.ErrNoMorePackets) { break }
        require.NoError(t, err)
        
        pool, ok := pools[pkt.Header.PID]
        if !ok {
            pool = newPacketPool(nil)
            pools[pkt.Header.PID] = pool
        }
        
        completed := pool.addUnlocked(pkt)
        for _, cp := range completed {
            // Спробувати парсити як PES
            if cp.Header.PayloadUnitStartIndicator {
                _, err := parsePESHeader(cp.Payload)
                if err == nil {
                    processedPES++
                }
            }
        }
    }
    
    // Перевірити, що хоча б деякі PES успішно зібрані
    assert.Greater(t, processedPES, 0, "should process at least some PES packets")
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Ложні розриви через NULL-пакети | `hasDiscontinuity` повертає `true` для PID=0x1FFF | Додати перевірку `if pkt.Header.PID == PIDNull { return false }` на початку |
| Дублікати не фільтруються | `isSameAsPrevious` не враховує `HasPayload` | Перевірити, що обидва пакети мають `HasPayload=true` перед порівнянням |
| `packetPool` не повертає останню групу | Незавершені пакети "зависають" у пулі | Викликати `dumpUnlocked()` при завершенні сегмента або таймауті |
| Рідкісні пакети відкидаються через "розриви" | Teletext/EIT мають нерегулярний CC | Використовувати окремий пул або вимкнути continuity-перевірку для відомих рідкісних PID |
| Пам'ять росте через нескинуті групи | `packetPool` тримає старі дані | Додати TTL: якщо група не завершена за >2 сек — скинути з логуванням |

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Ініціалізація трекерів для кожного PID:
type StreamProcessor struct {
    history map[uint16][]*astits.Packet  // останні 2-3 пакети для continuity
    pools   map[uint16]*packetPool        // для збору фрагментованих даних
}

func NewStreamProcessor() *StreamProcessor {
    return &StreamProcessor{
        history: make(map[uint16][]*astits.Packet),
        pools:   make(map[uint16]*packetPool),
    }
}

// 2. Обробка вхідного пакета:
func (sp *StreamProcessor) Process(pkt *astits.Packet, channelID string) error {
    pid := pkt.Header.PID
    
    // Ініціалізувати за потребою
    if sp.history[pid] == nil {
        sp.history[pid] = make([]*astits.Packet, 0, 3)
        sp.pools[pid] = newPacketPool(nil)
    }
    
    // Фільтр дублікатів
    if isSameAsPrevious(sp.history[pid], pkt) {
        return nil  // не помилка
    }
    
    // Перевірка на розрив
    if hasDiscontinuity(sp.history[pid], pkt) {
        log.Warnf("Discontinuity on PID %d (channel=%s)", pid, channelID)
        // Скинути пул для цього PID
        sp.pools[pid] = newPacketPool(nil)
        // Опціонально: сповістити про #EXT-X-DISCONTINUITY
    }
    
    // Додати у пул
    completed := sp.pools[pid].addUnlocked(pkt)
    
    // Оновити історію (зберігати останні 2 пакети)
    sp.history[pid] = append(sp.history[pid], pkt)
    if len(sp.history[pid]) > 2 {
        sp.history[pid] = sp.history[pid][1:]
    }
    
    // Обробити завершені логічні пакети
    for _, cp := range completed {
        if err := handleCompletePacket(cp, channelID); err != nil {
            log.Errorf("Error handling completed packet: %v", err)
        }
    }
    
    return nil
}

// 3. Очищення при завершенні сегмента:
func (sp *StreamProcessor) Flush(channelID string) {
    for pid, pool := range sp.pools {
        completed := pool.dumpUnlocked()
        for _, cp := range completed {
            handleCompletePacket(cp, channelID)  // остання спроба обробки
        }
        // Не очищати history — може знадобитися для наступного сегмента
    }
}
```

---

## 📊 Матриця покриття логіки

```
Функція              | Кейси в тесті | Покриття | Примітки
─────────────────────┼───────────────┼──────────┼─────────
hasDiscontinuity     | 7             | ✅ Високе | Додати тест на багато пропусків
isSameAsPrevious     | 3             | ✅ Базове | Додати тест на AF-only пакети
packetPool.add       | 9 + 11        | ✅ Високе | Додати тест на таймаут груп
packetPool.dump      | 3             | ✅ Базове | Додати тест на порожній пул
Rare packets (PUSI=1)| 11            | ✅ Спец.  | Додати тест на фрагментовані рідкісні
```

---

## 📚 Корисні посилання

- [MPEG-TS Continuity Counter spec](https://en.wikipedia.org/wiki/MPEG_transport_stream#Continuity_counter)
- [DVB Teletext carriage in TS](https://www.dvb.org/resources/public/standards/a039r2_Teletext_specification.pdf)
- [astits packet pooling source](https://github.com/asticode/go-astits/blob/master/packet_pool.go) (шукати `type packetPool`)

> 💡 **Ключова ідея**: Ця логіка — "імунна система" вашого пайплайну. Вона відсікає пошкоджені/дубльовані дані та збирає фрагменти в цілісні блоки. У вашому CCTV HLS контексті це критично для:
> - 🎯 Стабільної A/V синхронізації (не ламати PTS через дублікати)
> - 📡 Коректної генерації `#EXT-X-DISCONTINUITY` (детекція реальних розривів)
> - 🧩 Обробки орфан-аудіо (збирати фрагменти, навіть якщо відео затримується)

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати цю логіку у ваш `segmentAssembler` з урахуванням orphan audio merge
- 📈 Додати Prometheus-метрики для моніторингу розривів/дублікатів по каналах
- 🧪 Написати fuzz-тест для стійкості до пошкоджених потоків

🛠️