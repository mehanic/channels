# Глибоке роз'яснення: `packetAccumulator` та `packetPool` у astits

Ці структури реалізують **інтелектуальний буфер для збору фрагментованих MPEG-TS пакетів** у цілісні логічні одиниці (PES-пакети, PSI таблиці). Це "мозок" демуксера, що вирішує: *коли пакет готовий до обробки?*

---

## 🎯 Архітектура: навіщо два рівні буферизації?

```
┌─────────────────────────────────────────┐
│ Рівень 1: packetAccumulator (на PID)   │
│ • Буферизує пакети ОДНОГО PID           │
│ • Детектує: розриви, дублікати, PUSI   │
│ • Визначає завершення PSI/PES           │
│                                         │
│ Рівень 2: packetPool (глобальний)      │
│ • Керує множинами packetAccumulator    │
│ • Фільтрує: помилкові пакети, без payload│
│ • Забезпечує fair-порядок через dump() │
│                                         │
│ Потік даних:                            │
│ [TS потік] → packetPool.addUnlocked()  │
│              ↓                          │
│   [packetAccumulator для цього PID]    │
│              ↓                          │
│   [готові []*Packet] → парсинг PES/PSI │
└─────────────────────────────────────────┘
```

---

## 🔧 `packetAccumulator` — логіка збору для одного PID

### Структура

```go
type packetAccumulator struct {
    pid        uint16              // PID, за який відповідає цей акумулятор
    programMap *programMap         // Для перевірки: чи це PMT/PAT/відомий PID?
    q          []*Packet           // Поточний буфер пакетів (очікує завершення)
}
```

### Метод `add(p *Packet)` — серце логіки

```go
func (b *packetAccumulator) add(p *Packet) (ps []*Packet) {
    mps := b.q  // mps = "maybe pending slice"
    
    // 🔹 Крок 1: Детекція розриву → скинути буфер
    if hasDiscontinuity(mps, p) {
        if cap(mps) > 0 {
            mps = mps[:0]  // reuse capacity, zero length
        } else {
            mps = make([]*Packet, 0, 10)  // new allocation
        }
    }
    
    // 🔹 Крок 2: Фільтрація дублікатів → відкинути пакет
    if isSameAsPrevious(mps, p) {
        return  // порожній результат = пакет проігноровано
    }
    
    // 🔹 Крок 3: Флаш при новому PES/PSI (PUSI=1)
    if p.Header.PayloadUnitStartIndicator {
        ps = mps           // повернути попередню завершену групу
        mps = make([]*Packet, 0, cap(mps))  // почати нову групу
    }
    
    // 🔹 Крок 4: Додати поточний пакет у буфер
    mps = append(mps, p)
    
    // 🔹 Крок 5: Перевірка завершення PSI (PAT/PMT/SDT...)
    if b.programMap != nil &&
       (b.pid == PIDPAT || b.programMap.existsUnlocked(b.pid)) &&
       isPSIComplete(mps) {
        ps = mps    // PSI завершено → повернути
        mps = nil   // буфер порожній
    }
    // 🔹 Крок 6: Перевірка завершення PES (відео/аудіо дані)
    else if isPESPayload(mps[0].Payload) && isPESComplete(mps) {
        ps = mps
        mps = nil
    }
    
    b.q = mps  // зберегти стан
    return     // ps = готові до обробки пакети (може бути порожнім)
}
```

### Візуалізація станів акумулятора

```
Сценарій: відео-потік з ключовим кадром (3 TS-пакети)

Пакет 1: CC=0, PUSI=1, payload=PES_header+H264_start
┌─────────────────┐
│ q: [pkt1]       │
│ PUSI=1 → flush  │
└─────────────────┘
→ повертає: [] (попередній буфер був порожній)

Пакет 2: CC=1, PUSI=0, payload=H264_continuation
┌─────────────────┐
│ q: [pkt1, pkt2] │
│ чекаємо завершення...│
└─────────────────┘
→ повертає: []

Пакет 3: CC=2, PUSI=0, payload=H264_end + stuffing
┌─────────────────┐
│ q: [pkt1,pkt2,pkt3]│
│ isPESComplete()=true│
└─────────────────┘
→ повертає: [pkt1, pkt2, pkt3] ✅ ГОТОВИЙ PES!
→ q стає nil
```

---

## 🔧 `packetPool` — глобальний диспетчер PID-акумуляторів

### Структура

```go
type packetPool struct {
    // map[uint32]*packetAccumulator — оптимізація хешування (як у programMap)
    b map[uint32]*packetAccumulator
    
    programMap *programMap  // спільний для всіх акумуляторів
}
```

### Метод `addUnlocked(p *Packet)` — вхідна точка

```go
func (b *packetPool) addUnlocked(p *Packet) (ps []*Packet) {
    // 🔹 Фільтр 1: пакети з помилкою транспорту
    if p.Header.TransportErrorIndicator {
        return  // відкинути негайно
    }
    
    // 🔹 Фільтр 2: пакети без payload (поки що)
    // TODO: адаптаційні поля з PCR можуть бути корисними!
    if !p.Header.HasPayload {
        return
    }
    
    // 🔹 Отримати або створити акумулятор для цього PID
    acc, ok := b.b[uint32(p.Header.PID)]
    if !ok {
        acc = newPacketAccumulator(p.Header.PID, b.programMap)
        b.b[uint32(p.Header.PID)] = acc
    }
    
    // 🔹 Делегувати логіку акумулятору
    return acc.add(p)
}
```

### Метод `dumpUnlocked()` — "аварійний злив" незавершених даних

```go
func (b *packetPool) dumpUnlocked() (ps []*Packet) {
    // 1. Зібрати всі PID та відсортувати (детермінований порядок)
    var keys []int
    for k := range b.b {
        keys = append(keys, int(k))
    }
    sort.Ints(keys)
    
    // 2. Повернути ПЕРШУ знайдену не-порожню чергу
    for _, k := range keys {
        ps = b.b[uint32(k)].q  // взяти буфер
        delete(b.b, uint32(k)) // видалити акумулятор (одноразовий dump!)
        if len(ps) > 0 {
            return ps  // повернути перший не-порожній
        }
    }
    return  // порожній результат, якщо всі буфери порожні
}
```

> ⚠️ **Важливо**: `dumpUnlocked()` видаляє акумулятори після зливу! Це призначено для **одноразового отримання** залишкових даних (напр., при завершенні сегмента).

---

## 🔍 Детальний розбір допоміжних функцій

### `hasDiscontinuity(ps []*Packet, p *Packet) bool`

```go
func hasDiscontinuity(ps []*Packet, p *Packet) bool {
    // 1. NULL-пакети ніколи не є розривом
    if p.Header.PID == PIDNull { return false }
    
    // 2. Явний індикатор розриву = сигналізована подія, не помилка
    if p.Header.HasAdaptationField && p.AdaptationField.DiscontinuityIndicator {
        return false
    }
    
    // 3. Немає історії → не може бути розриву
    if len(ps) == 0 { return false }
    
    // 4. Розрахувати очікуваний CC:
    var expected uint8
    if p.Header.HasPayload {
        // Пакет з payload: CC має інкрементитися
        expected = (ps[len(ps)-1].Header.ContinuityCounter + 1) & 0x0f
    } else {
        // Пакет тільки з адаптаційним полем: CC може не змінюватися
        expected = ps[len(ps)-1].Header.ContinuityCounter
    }
    
    // 5. Порівняти з фактичним
    return expected != p.Header.ContinuityCounter
}
```

**Ключові нюанси:**
```
• & 0x0f — маска для 4-бітного CC (wrap-around 15→0)
• Пакети без payload (тільки адаптаційне поле) можуть мати "завмерлий" CC
• Це дозволяє не відкидати PCR-пакети, які часто йдуть без payload
```

### `isSameAsPrevious(ps []*Packet, p *Packet) bool`

```go
func isSameAsPrevious(ps []*Packet, p *Packet) bool {
    l := len(ps)
    // Дублікат = є історія + поточний має payload + CC збігається з останнім
    return l > 0 && p.Header.HasPayload && 
           p.Header.ContinuityCounter == ps[l-1].Header.ContinuityCounter
}
```

> 💡 **Чому тільки для пакетів з payload?** Пакети тільки з адаптаційним полем можуть повторюватися легально (напр., періодичні PCR).

---

## 🔄 Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Адаптація `packetPool` для channel-aware архітектури

```go
// У вашому багатоканальному сервері:
type ChannelPacketPool struct {
    mu     sync.RWMutex
    pools  map[string]*packetPool  // channelID → окремий пул
}

func (cpp *ChannelPacketPool) Get(channelID string) *packetPool {
    cpp.mu.RLock()
    pool, ok := cpp.pools[channelID]
    cpp.mu.RUnlock()
    
    if ok { return pool }
    
    cpp.mu.Lock()
    defer cpp.mu.Unlock()
    
    if pool, ok = cpp.pools[channelID]; ok {
        return pool
    }
    
    // Створити новий пул з channel-specific programMap
    pm := newProgramMap()  // або отримати з ChannelProgramCache
    pool = newPacketPool(pm)
    cpp.pools[channelID] = pool
    return pool
}

// Використання у segmentAssembler:
func processIncomingStream(channelID string, reader io.Reader) {
    pool := channelPools.Get(channelID)
    dmx := astits.NewDemuxer(ctx, reader)
    
    for {
        pkt, err := dmx.NextPacket()
        if err != nil { break }
        
        // Додати у channel-specific пул
        completed := pool.addUnlocked(pkt)
        
        for _, cp := range completed {
            // Обробити готовий PES/PSI
            handleCompletePacket(cp, channelID)
        }
    }
    
    // Злити залишки при завершенні сегмента
    if residual := pool.dumpUnlocked(); len(residual) > 0 {
        log.Warnf("Channel %s: %d residual packets at segment end", 
            channelID, len(residual))
        // Опція: спробувати обробити або відкинути
    }
}
```

### ✅ 2. Обробка orphan audio через `packetAccumulator`

```go
// Проблема: аудіо-пакети надходять, але відео затримується
// Рішення: розширити логіку завершення PES для аудіо

type AudioAwareAccumulator struct {
    *packetAccumulator
    audioConfig *AACConfig  // знання про розмір аудіо-фреймів
}

func (aa *AudioAwareAccumulator) add(p *Packet) (ps []*Packet) {
    // Спочатку стандартна логіка
    ps = aa.packetAccumulator.add(p)
    
    // Якщо це аудіо-PID і пакет не повернуто — перевірити "м'яке" завершення
    if len(ps) == 0 && aa.pid == audioPID {
        if isLikelyAudioFrameComplete(aa.q) {
            // Аудіо-фрейм, ймовірно, завершений, навіть якщо PES-прапорці не ідеальні
            ps = aa.q
            aa.q = nil
        }
    }
    return ps
}

func isLikelyAudioFrameComplete(packets []*Packet) bool {
    // Евристика: підрахувати загальний розмір payload
    total := 0
    for _, p := range packets {
        total += len(p.Payload)
    }
    // AAC ADTS фрейм: заголовок 7-9 байт + дані
    // Типовий розмір: 100-2000 байт
    return total >= 100 && total <= 4000 && total%2 == 0  // груба евристика
}
```

### ✅ 3. Інтеграція з вашим `segmentAssembler` для keyframe-based сегментації

```go
// У segmentAssembler — використовувати packetPool для збору PES перед аналізом:
type SegmentAssembler struct {
    videoPool *packetPool
    audioPool *packetPool
    // ... інші поля
}

func (sa *SegmentAssembler) AddPacket(pkt *astits.Packet, isVideo bool) error {
    pool := sa.audioPool
    if isVideo {
        pool = sa.videoPool
    }
    
    // Додати у відповідний пул
    completed := pool.addUnlocked(pkt)
    
    for _, cp := range completed {
        if isVideo {
            // Спробувати витягти PTS та перевірити ключовий кадр
            pts, isKeyFrame, err := parseVideoPES(cp.Payload)
            if err != nil { continue }
            
            if isKeyFrame {
                // Ключовий кадр → можливо, час сегментувати
                if sa.shouldStartNewSegment(pts) {
                    if err := sa.finalizeCurrentSegment(); err != nil {
                        return err
                    }
                }
            }
            sa.bufferVideoFrame(cp.Payload, pts)
        } else {
            // Аудіо: додати у орфан-кеш або синхронізувати з відео
            sa.handleAudioPES(cp.Payload, cp.Header.PID)
        }
    }
    return nil
}
```

### ✅ 4. Моніторинг ефективності буферизації

```go
// monitoring.Monitor — метрики для packetPool:
type BufferMetrics struct {
    PoolFlushesByPUSI    *prometheus.CounterVec  // скільки разів флашнули через PUSI
    PoolFlushesByPSI     *prometheus.CounterVec  // скільки разів PSI завершено
    PoolFlushesByPES     *prometheus.CounterVec  // скільки разів PES завершено
    PoolDiscontinuities  *prometheus.CounterVec  // скидання через розриви
    PoolDuplicates       *prometheus.CounterVec  // відкинуті дублікати
    QueueDepthHistogram  *prometheus.HistogramVec  // розмір черги перед флашем
}

// У packetAccumulator.add():
func (b *packetAccumulator) add(p *Packet, metrics *BufferMetrics) (ps []*Packet) {
    // ... існуюча логіка ...
    
    if hasDiscontinuity(mps, p) {
        metrics.PoolDiscontinuities.WithLabelValues(
            fmt.Sprintf("pid_%d", b.pid)).Inc()
        // ...
    }
    
    if p.Header.PayloadUnitStartIndicator {
        metrics.PoolFlushesByPUSI.WithLabelValues(
            fmt.Sprintf("pid_%d", b.pid)).Inc()
        metrics.QueueDepthHistogram.WithLabelValues(
            fmt.Sprintf("pid_%d", b.pid)).Observe(float64(len(mps)))
        // ...
    }
    
    // Аналогічно для PSI/PES completion
    return ps
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на обробку фрагментованого орфан-аудіо

```go
func TestPacketAccumulator_OrphanAudioFragmented(t *testing.T) {
    pm := newProgramMap()
    acc := newPacketAccumulator(audioPID, pm)
    
    // Сценарій: аудіо-PES з 3 фрагментів, PUSI тільки на першому
    pesHeader := buildAACPesHeader(pts=90000*4)  // 4 секунди
    audioFrame1 := generateAACFrame(500)  // 500 байт
    audioFrame2 := generateAACFrame(300)  // продовження
    
    // Пакет 1: початок PES
    pkt1 := &Packet{
        Header: PacketHeader{
            PID: audioPID,
            ContinuityCounter: 0,
            HasPayload: true,
            PayloadUnitStartIndicator: true,  // ⚠️ PUSI=1
        },
        Payload: append(pesHeader, audioFrame1[:100]...),  // частка даних
    }
    ps := acc.add(pkt1)
    assert.Len(t, ps, 0)  // ще не завершено
    
    // Пакет 2: продовження
    pkt2 := &Packet{
        Header: PacketHeader{
            PID: audioPID,
            ContinuityCounter: 1,
            HasPayload: true,
            PayloadUnitStartIndicator: false,
        },
        Payload: audioFrame1[100:],
    }
    ps = acc.add(pkt2)
    assert.Len(t, ps, 0)  // ще не завершено
    
    // Пакет 3: завершення + новий фрейм
    pkt3 := &Packet{
        Header: PacketHeader{
            PID: audioPID,
            ContinuityCounter: 2,
            HasPayload: true,
            PayloadUnitStartIndicator: true,  // ⚠️ Новий PES починається!
        },
        Payload: append(audioFrame2, 0xFF, 0xFF, 0xFF...),  // + stuffing
    }
    ps = acc.add(pkt3)
    
    // Очікуємо: попередній PES завершено через PUSI нового пакета
    assert.Len(t, ps, 3)  // pkt1+pkt2+початок pkt3 = завершений перший PES
    assert.Equal(t, uint16(audioPID), ps[0].Header.PID)
}
```

### 🔹 Тест на обробку розриву з відновленням

```go
func TestPacketAccumulator_DiscontinuityRecovery(t *testing.T) {
    pm := newProgramMap()
    acc := newPacketAccumulator(videoPID, pm)
    
    // Нормальна послідовність
    for cc := 0; cc < 3; cc++ {
        pkt := &Packet{
            Header: PacketHeader{
                PID: videoPID,
                ContinuityCounter: uint8(cc),
                HasPayload: true,
                PayloadUnitStartIndicator: cc == 0,  // PUSI тільки на початку
            },
            Payload: []byte("video_data"),
        }
        ps := acc.add(pkt)
        if cc < 2 {
            assert.Len(t, ps, 0)  // ще збираємо
        }
    }
    
    // Розрив: CC стрибає з 2 на 5
    pktBroken := &Packet{
        Header: PacketHeader{
            PID: videoPID,
            ContinuityCounter: 5,  // ❌ пропуск 3,4
            HasPayload: true,
            PayloadUnitStartIndicator: true,  // новий початок
        },
        Payload: []byte("new_video_data"),
    }
    ps := acc.add(pktBroken)
    
    // Очікуємо: попередній буфер скинуто, новий пакет почав нову групу
    assert.Len(t, ps, 0)  // попередній відкинуто через розрив
    assert.NotNil(t, acc.q)  // новий буфер ініціалізовано
    assert.Len(t, acc.q, 1)  // містить тільки pktBroken
}
```

### 🔹 Бенчмарк на продуктивність буферизації

```go
func BenchmarkPacketPool_HighThroughput(b *testing.B) {
    pool := newPacketPool(nil)
    
    // Згенерувати 1000 пакетів з чергуючимися PID
    packets := make([]*Packet, 1000)
    for i := range packets {
        pid := uint16(256 + i%4)  // 4 різних PID
        packets[i] = &Packet{
            Header: PacketHeader{
                PID: pid,
                ContinuityCounter: uint8(i % 16),
                HasPayload: true,
                PayloadUnitStartIndicator: i%10 == 0,  // PUSI кожні 10 пакетів
            },
            Payload: make([]byte, 100),  // 100 байт payload
        }
    }
    
    b.ResetTimer()
    b.ReportAllocs()
    
    for i := 0; i < b.N; i++ {
        for _, pkt := range packets {
            _ = pool.addUnlocked(pkt)
        }
        // Періодичний dump для очищення
        if i%100 == 0 {
            _ = pool.dumpUnlocked()
        }
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `dumpUnlocked()` видаляє акумулятори | Після першого dump() пакети нового PID не буферизуються | Викликати `dumpUnlocked()` тільки при завершенні сегмента, або реалізувати "мягкий" dump, що не видаляє |
| Аудіо-орфани не завершуються | `isPESComplete()` не розпізнає фрагментовані аудіо-фрейми | Додати евристику `isLikelyAudioFrameComplete()` для аудіо-PID |
| Розриви через пакети без payload | PCR-пакети (без payload) ламають continuity-логіку | Перевірити, що `hasDiscontinuity` правильно обробляє `!HasPayload` випадок (вже реалізовано ✅) |
| Пам'ять росте через "завислі" буфери | Акумулятори тримають дані, якщо PES ніколи не завершується | Додати TTL: якщо буфер не оновлювався >2 сек → скинути з логуванням |
| Конкурентний доступ без синхронізації | Паніка при паралельному `addUnlocked()` з різних горутин | Додати `sync.Mutex` до `packetPool` або використовувати один воркер на пул |

### Приклад thread-safe обгортки:

```go
type SafePacketPool struct {
    mu sync.Mutex
    *packetPool
}

func (spp *SafePacketPool) Add(p *Packet) []*Packet {
    spp.mu.Lock()
    defer spp.mu.Unlock()
    return spp.packetPool.addUnlocked(p)
}

func (spp *SafePacketPool) Dump() []*Packet {
    spp.mu.Lock()
    defer spp.mu.Unlock()
    return spp.packetPool.dumpUnlocked()
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Ініціалізація пулів для каналу:
func setupChannelPools(channelID string) (*packetPool, *packetPool) {
    pm := newProgramMap()  // або отримати з кешу
    
    // Окремий пул для відео та аудіо — краща ізоляція
    videoPool := newPacketPool(pm)
    audioPool := newPacketPool(pm)
    
    return videoPool, audioPool
}

// 2. Обробка пакета з детекцією завершення:
func handlePacket(pkt *astits.Packet, videoPool, audioPool *packetPool, 
                 channelID string, metrics *BufferMetrics) {
    
    pool := audioPool
    if isVideoPID(pkt.Header.PID) {
        pool = videoPool
    }
    
    completed := pool.addUnlocked(pkt)
    
    for _, cp := range completed {
        if isVideoPID(cp.Header.PID) {
            pts, keyframe, _ := parseVideoPES(cp.Payload)
            if keyframe {
                triggerSegmentBoundary(channelID, pts)
            }
        } else {
            handleAudioPES(cp.Payload, channelID)
        }
    }
}

// 3. Завершення сегмента — злити залишки:
func finalizeSegment(videoPool, audioPool *packetPool, channelID string) {
    // Злити відео-залишки
    if residual := videoPool.dumpUnlocked(); len(residual) > 0 {
        log.Debugf("Channel %s: flushing %d residual video packets", 
            channelID, len(residual))
        for _, pkt := range residual {
            handlePacket(pkt, videoPool, audioPool, channelID, nil)
        }
    }
    
    // Аналогічно для аудіо...
}

// 4. Додати моніторинг:
if config.EnableBufferMetrics {
    metrics.PoolDiscontinuities.WithLabelValues(channelID).Add(discontinuityCount)
    metrics.QueueDepthHistogram.WithLabelValues(channelID).Observe(avgQueueDepth)
}
```

---

## 📊 Матриця станів packetAccumulator

```
Вхідний пакет          | Умови                    | Дія               | Повертає
───────────────────────┼──────────────────────────┼───────────────────┼─────────
PUSI=1, новий PID      | порожній буфер           | почати нову групу | []
PUSI=1, є буфер        | попередня група готова   | флаш + нова група | [попередні]
PUSI=0, CC=очікуваний  | продовження групи        | додати у буфер    | []
PUSI=0, CC≠очікуваний  | розрив                   | скинути буфер     | []
CC=попередній+payload  | дублікат                 | відкинути пакет   | []
PSI-завершено          | programMap+parsing OK    | повернути PSI     | [PSI-пакети]
PES-завершено          | payload analysis OK      | повернути PES     | [PES-пакети]
dumpUnlocked()         | будь-який стан           | злити + видалити  | [залишки] або []
```

---

## 📚 Корисні посилання

- [MPEG-TS PES packetization](https://en.wikipedia.org/wiki/Packetized_Elementary_Stream)
- [PSI/SI table completion logic](https://www.dvb.org/resources/public/standards/a003r14_Specification_for_Service_Information.pdf)
- [astits packet_pool.go source](https://github.com/asticode/go-astits/blob/master/packet_pool.go)

> 💡 **Ключова ідея**: `packetAccumulator`/`packetPool` — це "розумний буфер", що розуміє семантику MPEG-TS. У вашому CCTV HLS пайплайні це дозволяє:
> - 🎯 Точно детектувати ключові кадри для сегментації (через PUSI + PES-парсинг)
> - 🧩 Надійно збирати фрагментоване орфан-аудіо (через буферизацію за PID)
> - 🚦 Коректно обробляти розриви без втрати синхронізації (через continuity-логіку)

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати цю логіку у ваш `segmentAssembler` з підтримкою orphan audio merge
- 🧪 Написати integration-тест для перевірки збору фрагментованих аудіо-пакетів
- 📈 Додати метрики для моніторингу ефективності буферизації по каналах

🛠️