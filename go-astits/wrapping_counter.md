# Глибоке роз'яснення: `wrappingCounter` у MPEG-TS та astits

Цей тип реалізує **циклічний лічильник** (wrapping counter) — критично важливий механізм для роботи з **Continuity Counter** у заголовках TS-пакетів (4 біти, діапазон 0–15).

---

## 🎯 Навіщо потрібен `wrappingCounter`?

```
┌─────────────────────────────────────────┐
│ Continuity Counter у MPEG-TS:           │
│ • 4-бітне поле у заголовку пакета       │
│ • Діапазон: 0 → 1 → ... → 15 → 0 → 1... │
│ • Призначення:                          │
│   - Детекція втрачених пакетів          │
│   - Виявлення дублікатів                │
│   - Перевірка порядку пакетів у потоці  │
└─────────────────────────────────────────┘
```

**Приклад TS-пакету:**
```
[0x47 | PID | ... | Continuity Counter: 0xE]
                              ↑
                        4 біти = 0–15
```

---

## 🔧 Розбір коду

### Структура
```go
type wrappingCounter struct {
    value  int  // поточне значення
    wrapAt int  // максимальне значення перед обнуленням (напр. 15)
}
```

### Ініціалізація: чому `value = wrapAt + 1`?
```go
func newWrappingCounter(wrapAt int) wrappingCounter {
    return wrappingCounter{
        value:  wrapAt + 1,  // ⚠️ спеціальний стан "неініціалізовано"
        wrapAt: wrapAt,
    }
}
```

**Логіка:**
```
wrapAt = 15 (для Continuity Counter)
→ value = 16 при створенні

Перший inc():
  c.value++ → 17
  17 > 15 → true → c.value = 0
  повертає 0

Другий inc():
  c.value++ → 1
  1 > 15 → false
  повертає 1
```

> 💡 **Навіщо так?** Це дозволяє відрізнити "ще не було жодного пакета" (`value=16`) від "щойно отримано пакет з контр-значенням 0". Перший виклик `inc()` гарантовано поверне `0`, що коректно ініціалізує синхронізацію.

### Методи

| Метод | Опис | Повертає |
|-------|------|----------|
| `get()` | Поточне значення | `int` (0…wrapAt або wrapAt+1) |
| `inc()` | Інкремент + wrap | Нове значення після інкременту |

```go
func (c *wrappingCounter) inc() int {
    c.value++
    if c.value > c.wrapAt {  // >, а не >=
        c.value = 0
    }
    return c.value
}
```

---

## 🔄 Практичне використання: Continuity Counter у TS

### ✅ 1. Детекція втрачених пакетів

```go
// segmentAssembler.go — перевірка цілісності потоку за PID
type PIDState struct {
    expectedCC wrappingCounter  // очікуване наступне значення
    lastCC     *int             // останнє отримане (для логів)
}

func (s *PIDState) checkContinuity(pkt *astits.Packet) (lost, duplicate bool) {
    cc := int(pkt.Header.ContinuityCounter)  // 0–15 з пакета
    
    // Перший пакет для цього PID
    if s.expectedCC.get() > s.expectedCC.wrapAt {
        s.expectedCC = newWrappingCounter(15)
        s.expectedCC.inc()  // ініціалізуємо на 0
        s.lastCC = &cc
        return false, false
    }
    
    expected := s.expectedCC.get()
    
    if cc == expected {
        // ✅ Очікуваний пакет
        s.expectedCC.inc()
        s.lastCC = &cc
        return false, false
    } else if cc == s.lastCC {
        // ⚠️ Дублікат (можливо, ретрансмісія)
        return false, true
    } else {
        // ❌ Втрата пакетів: розрахувати скільки
        lostCount := (cc - expected + 16) % 16
        if lostCount == 0 { lostCount = 16 }  // випадок повного кола
        
        // Оновити очікування
        s.expectedCC.value = (cc + 1) % 16
        s.lastCC = &cc
        return true, false
    }
}
```

**Інтеграція з вашим пайплайном:**
```go
// У videoQueue/audioQueue обробці:
pidStates := make(map[uint16]*PIDState)  // key = PID

func processPacket(pkt *astits.Packet, channelID string) {
    state := pidStates[pkt.Header.PID]
    if state == nil {
        state = &PIDState{}
        pidStates[pkt.Header.PID] = state
    }
    
    lost, dup := state.checkContinuity(pkt)
    
    if lost {
        metrics.TSPacketLoss.WithLabelValues(channelID).Inc()
        log.Warnf("PID %d: lost packets (channel=%s)", pkt.Header.PID, channelID)
    }
    if dup {
        metrics.TSDuplicates.WithLabelValues(channelID).Inc()
        // Можна відкинути дублікат або обробити обережно
    }
    
    // Далі — нормальна обробка пакета...
}
```

### ✅ 2. Синхронізація аудіо/відео за Continuity Counter

```go
// orphan audio merge — використання CC для валідації порядку
type SyncTracker struct {
    videoCC wrappingCounter
    audioCC wrappingCounter
}

func (t *SyncTracker) validatePair(videoPkt, audioPkt *astits.Packet) bool {
    // Перевірити, що обидва потоки мають коректну послідовність
    vLost, _ := checkCC(&t.videoCC, videoPkt.Header.ContinuityCounter)
    aLost, _ := checkCC(&t.audioCC, audioPkt.Header.ContinuityCounter)
    
    if vLost || aLost {
        log.Warnf("A/V desync detected via CC (video_lost=%v, audio_lost=%v)", vLost, aLost)
        // Спроба відновлення: скинути стан або використати PTS для ре-синхронізації
        return false
    }
    return true
}

func checkCC(counter *wrappingCounter, receivedCC uint8) (lost bool) {
    expected := counter.get()
    if expected > counter.wrapAt {
        // ініціалізація
        *counter = newWrappingCounter(15)
        counter.inc()
        return false
    }
    
    if int(receivedCC) != expected {
        // Втрата або розсинхронізація
        return true
    }
    counter.inc()
    return false
}
```

### ✅ 3. Генерація валідних TS-пакетів (для тестування або remux)

```go
// segmentFinalizer.go — при створенні нових TS-пакетів
type TSPacketBuilder struct {
    counters map[uint16]*wrappingCounter  // PID → counter
}

func (b *TSPacketBuilder) NewPacket(pid uint16, payload []byte) *astits.Packet {
    // Отримати або створити лічильник для PID
    cc, ok := b.counters[pid]
    if !ok {
        cc = newWrappingCounter(15)
        b.counters[pid] = cc
    }
    
    pkt := &astits.Packet{
        Header: &astits.PacketHeader{
            PID:              pid,
            ContinuityCounter: uint8(cc.inc()),  // 0→1→...→15→0
            PayloadUnitStartIndicator: true,
            HasPayload: true,
            // ... інші поля
        },
        Payload: payload,
    }
    return pkt
}
```

---

## ⚠️ Важливі обмеження та застереження

### ❌ Не thread-safe!

```go
// wrappingCounter НЕ використовує mutex/atomic:
func (c *wrappingCounter) inc() int {
    c.value++  // ⚠️ race condition при паралельному доступі!
    // ...
}
```

**Рішення для вашого багатопотокового пайплайну:**

```go
// Варіант 1: sync.Mutex (простий, але з блокуванням)
type safeWrappingCounter struct {
    mu     sync.Mutex
    value  int
    wrapAt int
}

func (c *safeWrappingCounter) inc() int {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.value++
    if c.value > c.wrapAt {
        c.value = 0
    }
    return c.value
}

// Варіант 2: atomic + CAS (для high-throughput)
type atomicWrappingCounter struct {
    value  atomic.Uint32
    wrapAt uint32
}

func (c *atomicWrappingCounter) inc() uint32 {
    for {
        old := c.value.Load()
        newVal := old + 1
        if newVal > c.wrapAt {
            newVal = 0
        }
        if c.value.CompareAndSwap(old, newVal) {
            return newVal
        }
        // retry при колізії
    }
}
```

### ❌ Не підходить для 33-бітних значень (PCR Base)

```go
// wrappingCounter використовує int (32/64 біти), але логіка wrapAt
// призначена для малих діапазонів (0–15, 0–255).

// Для PCR Base (33 біти) використовуйте спеціальну логіку:
func handlePCRWraparound(prev, curr uint64) uint64 {
    const maxPCR = (1 << 33) - 1
    if curr < prev && prev - curr > maxPCR/2 {
        return curr + maxPCR + 1  // врахувати перехід через максимум
    }
    return curr
}
```

---

## 🛠️ Інтеграція з вашою архітектурою

### У monitoring — метрики цілісності потоку

```go
// monitoring.Monitor
type Metrics struct {
    TSContinuityErrors *prometheus.CounterVec  // втрачені/дубльовані пакети
    PIDActiveCount     *prometheus.GaugeVec    // скільки активних PID
}

// У обробці пакетів:
func trackContinuity(pid uint16, cc uint8, channelID string) {
    state := getPIDState(pid, channelID)  // ваш cache
    lost, dup := state.checkContinuity(cc)
    
    if lost {
        metrics.TSContinuityErrors.WithLabelValues(channelID, "lost").Inc()
    }
    if dup {
        metrics.TSContinuityErrors.WithLabelValues(channelID, "duplicate").Inc()
    }
}
```

### У backpressure системі — детекція "завислих" потоків

```go
// Якщо Continuity Counter не змінюється протягом часу → потік "завис"
type PIDHealth struct {
    lastCCUpdate time.Time
    lastCCValue  int
}

func (h *PIDHealth) CheckStaleness(timeout time.Duration) bool {
    return time.Since(h.lastCCUpdate) > timeout
}

// У ShouldContinuePolling:
for pid, health := range pidHealthMap {
    if health.CheckStaleness(5 * time.Second) {
        log.Warnf("PID %d appears stalled (channel=%s)", pid, channelID)
        // Опція: пауза полігу, сповіщення, спроба відновлення
    }
}
```

### У WebSocketDistributor — валідація перед відправкою

```go
// Перед відправкою субтитрів, прив'язаних до відео-сегмента:
func validateSegmentIntegrity(tsData []byte, expectedCC map[uint16]int) error {
    dmx := astits.NewDemuxer(ctx, bytes.NewReader(tsData))
    
    for {
        pkt, err := dmx.NextPacket()
        if err != nil { break }
        
        expected, ok := expectedCC[pkt.Header.PID]
        if !ok { continue }  // новий PID, ініціалізувати
        
        actual := int(pkt.Header.ContinuityCounter)
        if actual != expected {
            return fmt.Errorf("CC mismatch on PID %d: expected %d, got %d", 
                pkt.Header.PID, expected, actual)
        }
        expectedCC[pkt.Header.PID] = (expected + 1) % 16
    }
    return nil
}
```

---

## 🧪 Unit-тести для `wrappingCounter`

```go
func TestWrappingCounter_Basic(t *testing.T) {
    c := newWrappingCounter(15)
    
    // Початковий стан: неініціалізовано
    if c.get() != 16 {
        t.Errorf("expected 16, got %d", c.get())
    }
    
    // Перший inc() → 0
    if v := c.inc(); v != 0 {
        t.Errorf("expected 0, got %d", v)
    }
    
    // Нормальний інкремент
    for i := 1; i <= 15; i++ {
        if v := c.inc(); v != i {
            t.Errorf("expected %d, got %d", i, v)
        }
    }
    
    // Wrap-around: 15 → 0
    if v := c.inc(); v != 0 {
        t.Errorf("expected wrap to 0, got %d", v)
    }
}

func TestWrappingCounter_DifferentWrapAt(t *testing.T) {
    // Для 3-бітного контр-значення (0–7)
    c := newWrappingCounter(7)
    c.inc()  // → 0
    for i := 1; i <= 7; i++ {
        if v := c.inc(); v != i%8 {
            t.Errorf("expected %d, got %d", i%8, v)
        }
    }
}
```

---

## 📊 Continuity Counter vs інші лічильники у TS

```
┌────────────────────┬────────┬──────────────┬─────────────────┐
│ Поле               │ Біти   │ Діапазон     │ Призначення     │
├────────────────────┼────────┼──────────────┼─────────────────┤
│ Continuity Counter │ 4      │ 0–15         │ Порядок пакетів │
│ PCR Extension      │ 9      │ 0–299        │ Точність часу   │
│ PCR Base           │ 33     │ 0–2³³-1      │ Еталонний час   │
│ PES PTS/DTS        │ 33     │ 0–2³³-1      │ Таймінг кадрів  │
└────────────────────┴────────┴──────────────┴─────────────────┘

wrappingCounter ідеально підходить тільки для першого рядка!
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| `value` починається з 16 | Перший `inc()` повертає 0, але `get()` до `inc()` показує 16 | Це **фича**, а не баг: дозволяє детектувати "перший пакет" |
| Гонка даних у багатопотоковому режимі | Невірні значення, псевдо-втрата пакетів | Додати `sync.Mutex` або `atomic` операції |
| Неправильний `wrapAt` | Лічильник зациклюється не там, де треба | Завжди передавати `15` для Continuity Counter, `255` для 8-бітних полів |
| Ігнорування `get() > wrapAt` | Пропуск ініціалізації першого пакета | Завжди перевіряти цей стан перед порівнянням з отриманим CC |

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Створення для Continuity Counter (4 біти)
cc := newWrappingCounter(15)

// 2. Перевірка отриманого пакета
receivedCC := int(pkt.Header.ContinuityCounter)
expected := cc.get()

if expected > 15 {
    // перший пакет: ініціалізувати
    cc.inc()  // тепер expected = 0
} else if receivedCC != expected {
    // втрата або дублікат
    lostCount := (receivedCC - expected + 16) % 16
    log.Warnf("Lost %d packets on PID %d", lostCount, pkt.Header.PID)
}

// 3. Оновити очікування для наступного пакета
cc.inc()

// 4. Для thread-safe версії — використовуйте mutex:
type safeCC struct {
    mu sync.Mutex
    wrappingCounter
}
func (s *safeCC) inc() int {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.wrappingCounter.inc()
}
```

---

> 💡 **Ключова ідея**: `wrappingCounter` — це простий, але ефективний інструмент для відстеження циклічних послідовностей у бінарних протоколах. У вашому CCTV HLS пайплайні він допоможе детектувати втрати пакетів на ранніх етапах (`segmentAssembler`), що покращить надійність A/V синхронізації та зменшить артефакти у фінальному HLS-потоці.

Якщо потрібно — можу допомогти реалізувати:
- 🔒 Thread-safe версію з `atomic` для high-throughput обробки
- 📈 Інтеграцію з Prometheus для моніторингу цілісності потоків
- 🧪 Автоматичні тести для детекції втрат пакетів у вашому `segmentFinalizer`

🛠️