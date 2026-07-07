# Глибоке роз'яснення: `TestProgramMap` — тест внутрішньої структури astits

Цей простий на перший погляд тест перевіряє базові операції внутрішньої структури `programMap`, яка використовується `astits.Demuxer` для відстеження **мапінгу PID → Program Number** у MPEG-TS потоках.

---

## 🎯 Що таке `programMap` і навіщо він потрібен?

```
┌─────────────────────────────────────────┐
│ programMap у MPEG-TS контексті:         │
│                                         │
│ PAT (Program Association Table) містить │
│ список програм і їх PMT PID:            │
│                                         │
│ Program Number → Program Map PID        │
│       1        →      0x1000 (4096)     │
│       2        →      0x1001 (4097)     │
│                                         │
│ programMap — це internal cache для:     │
│ • Швидкого пошуку: "який PID містить    │
│   метадані для програми X?"             │
│ • Фільтрації пакетів під час демуксингу │
│ • Уникнення дублювання обробки          │
└─────────────────────────────────────────┘
```

**Приклад використання у demuxer:**
```go
// Коли приходить пакет з PID=4096:
if pm.existsUnlocked(4096) {
    // Це PMT для відомої програми → парсити як PMT
    processPMT(packet)
} else {
    // Невідомий PID → ігнорувати або буферизувати
}
```

---

## 🔧 Розбір тесту

```go
func TestProgramMap(t *testing.T) {
    // 1. Створення порожньої мапи
    pm := newProgramMap()
    
    // 2. Перевірка: програма 1 ще не існує
    assert.False(t, pm.existsUnlocked(1))
    
    // 3. Додати мапінг: program 1 → PID 1
    pm.setUnlocked(1, 1)
    
    // 4. Перевірка: тепер програма 1 існує
    assert.True(t, pm.existsUnlocked(1))
    
    // 5. Видалити мапінг
    pm.unsetUnlocked(1)
    
    // 6. Перевірка: програма 1 знову не існує
    assert.False(t, pm.existsUnlocked(1))
}
```

### Чому методи з суфіксом `Unlocked`?

```go
// Приклад реалізації (гіпотетичний):
type programMap struct {
    mu   sync.RWMutex
    data map[uint16]uint16  // program_number → PMT_PID
}

func (pm *programMap) existsUnlocked(pid uint16) bool {
    // ⚠️ Не бере лок! Викликається ТІЛЬКИ коли mu вже захоплено
    _, ok := pm.data[pid]
    return ok
}

func (pm *programMap) setUnlocked(programNumber, pmtPID uint16) {
    // ⚠️ Не бере лок! Викликається ТІЛЬКИ коли mu вже захоплено
    pm.data[programNumber] = pmtPID
}

// Публічні методи з блокуванням:
func (pm *programMap) Exists(pid uint16) bool {
    pm.mu.RLock()
    defer pm.mu.RUnlock()
    return pm.existsUnlocked(pid)  // делегування на "unlocked" версію
}
```

> 💡 **Патерн**: `Unlocked` методи — це внутрішні допоміжні функції для уникнення подвійного локування (deadlock prevention) та оптимізації продуктивності.

---

## 🔄 Практичне значення для вашого CCTV HLS пайплайну

### ✅ 1. Розуміння, як astits фільтрує пакети

```go
// У вашому segmentAssembler при обробці вхідного потоку:
dmx := astits.NewDemuxer(ctx, reader)

for {
    pkt, err := dmx.NextPacket()
    if err != nil { break }
    
    // astits внутрішньо використовує programMap для:
    // 1. Визначення: це PAT/PMT/PES/NULL пакет?
    // 2. Фільтрації: чи потрібен нам цей PID?
    
    if pkt.Header.PID == 0x0000 {
        // PAT — завжди обробляти, оновити programMap
    } else if programMap.exists(pkt.Header.PID) {
        // PMT для відомої програми — парсити метадані
    }
    // Інакше — пропустити або буферизувати
}
```

### ✅ 2. Діагностика "зниклих" програм

```go
// Якщо у вашому пайплайні раптово перестають надходити дані:
func debugProgramMap(dmx *astits.Demuxer) {
    // ⚠️ programMap — приватне поле, але можна спостерігати побічно:
    
    // 1. Логувати всі знайдені PID
    seenPIDs := make(map[uint16]bool)
    
    for i := 0; i < 1000; i++ {  // обмежити сканування
        pkt, err := dmx.NextPacket()
        if err != nil { break }
        
        if !seenPIDs[pkt.Header.PID] {
            log.Infof("New PID detected: %d (0x%X)", pkt.Header.PID, pkt.Header.PID)
            seenPIDs[pkt.Header.PID] = true
        }
    }
    
    // 2. Перевірити, чи PAT містить очікувані програми
    // 3. Порівняти з programMap логікою: чи всі PMT PID відомі?
}
```

### ✅ 3. Реалізація власного `programMap` для channel-aware архітектури

```go
// У вашому multi-channel сервері:
type ChannelProgramMap struct {
    mu sync.RWMutex
    // channelID → (programNumber → PMT_PID)
    maps map[string]map[uint16]uint16
}

func (cpm *ChannelProgramMap) Set(channelID string, programNumber, pmtPID uint16) {
    cpm.mu.Lock()
    defer cpm.mu.Unlock()
    
    if cpm.maps[channelID] == nil {
        cpm.maps[channelID] = make(map[uint16]uint16)
    }
    cpm.maps[channelID][programNumber] = pmtPID
}

func (cpm *ChannelProgramMap) Get(channelID string, programNumber) (uint16, bool) {
    cpm.mu.RLock()
    defer cpm.mu.RUnlock()
    
    if m, ok := cpm.maps[channelID]; ok {
        pid, exists := m[programNumber]
        return pid, exists
    }
    return 0, false
}

// Використання у WebSocketDistributor:
func routeSubtitle(msg SubtitleMessage, cpm *ChannelProgramMap) {
    pmtPID, ok := cpm.Get(msg.ChannelID, msg.ProgramNumber)
    if !ok {
        log.Warnf("Unknown program %d for channel %s", msg.ProgramNumber, msg.ChannelID)
        return
    }
    // Маршрутизувати субтитри у відповідний потік...
}
```

---

## 🧪 Розширення тесту для ваших потреб

### 🔹 Тест на конкурентний доступ (thread-safety)

```go
func TestProgramMap_Concurrent(t *testing.T) {
    pm := newProgramMap()
    var wg sync.WaitGroup
    
    // 10 горутин пишуть, 10 читають
    for i := 0; i < 10; i++ {
        wg.Add(2)
        
        // Writer
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                pm.mu.Lock()
                pm.setUnlocked(uint16(id), uint16(j))
                pm.mu.Unlock()
            }
        }(i)
        
        // Reader
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                pm.mu.RLock()
                _ = pm.existsUnlocked(uint16(id))
                pm.mu.RUnlock()
            }
        }(i)
    }
    
    wg.Wait()
    // Якщо race detector не скаржиться — тест пройдено
}
```

> Запустіть з `-race`: `go test -race -run TestProgramMap_Concurrent`

### 🔹 Тест на велику кількість програм

```go
func TestProgramMap_Scale(t *testing.T) {
    pm := newProgramMap()
    
    // Додати 1000 програм
    for i := uint16(1); i <= 1000; i++ {
        pm.mu.Lock()
        pm.setUnlocked(i, i+0x1000)  // PMT PID = program + 4096
        pm.mu.Unlock()
    }
    
    // Перевірити всі
    pm.mu.RLock()
    for i := uint16(1); i <= 1000; i++ {
        assert.True(t, pm.existsUnlocked(i+0x1000), "PID %d should exist", i+0x1000)
    }
    pm.mu.RUnlock()
    
    // Видалити половину
    for i := uint16(1); i <= 500; i++ {
        pm.mu.Lock()
        pm.unsetUnlocked(i)
        pm.mu.Unlock()
    }
    
    // Перевірити, що видалені більше не існують
    pm.mu.RLock()
    for i := uint16(1); i <= 500; i++ {
        assert.False(t, pm.existsUnlocked(i), "PID %d should be removed", i)
    }
    pm.mu.RUnlock()
}
```

---

## 🛠️ Інтеграція з вашим пайплайном

### У monitoring — метрики програм

```go
// monitoring.Monitor
type Metrics struct {
    ActiveProgramsGauge *prometheus.GaugeVec  // скільки програм активно на канал
    PMTDiscoveryCounter *prometheus.CounterVec  // скільки разів знайдено новий PMT
}

// У segmentAssembler при обробці PAT:
func handlePAT(pat *astits.PATData, channelID string, metrics *Metrics) {
    for _, prog := range pat.Programs {
        if prog.ProgramNumber > 0 {  // 0 = NIT, пропускаємо
            metrics.ActiveProgramsGauge.WithLabelValues(channelID).Inc()
            metrics.PMTDiscoveryCounter.WithLabelValues(channelID, 
                fmt.Sprintf("prog_%d", prog.ProgramNumber)).Inc()
            
            log.Infof("Channel %s: discovered program %d → PMT PID %d", 
                channelID, prog.ProgramNumber, prog.ProgramMapID)
        }
    }
}
```

### У backpressure системі — фільтрація за програмою

```go
// Якщо клієнт підписаний тільки на одну програму:
type ClientSubscription struct {
    ChannelID      string
    ProgramNumbers map[uint16]bool  // тільки ці програми цікавлять клієнта
}

func (cs *ClientSubscription) ShouldProcessPacket(pid uint16, programMap *programMap) bool {
    // Перевірити, чи цей PID належить до підписаних програм
    programMap.mu.RLock()
    defer programMap.mu.RUnlock()
    
    for progNum, pmtPID := range programMap.data {
        if cs.ProgramNumbers[progNum] && (pid == pmtPID || isElementaryStreamOf(pid, progNum)) {
            return true
        }
    }
    return false
}

// У broadcast loop:
for _, client := range clients {
    if client.sub.ShouldProcessPacket(pkt.Header.PID, programMap) {
        sendToClient(client, pkt)
    }
}
```

---

## 🐛 Поширені проблеми, пов'язані з programMap

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Дедлок при локуванні | Зависання при одночасному читанні/запису | Використовувати `RWMutex`, `Unlocked` методи тільки всередині вже захопленого локу |
| Втрата програм після PAT оновлення | Клієнти перестають отримувати дані | При отриманні нової PAT — очищати старий `programMap` перед оновленням |
| Неправильний мапінг PID → програма | Дані програми 1 потрапляють у програму 2 | Перевіряти, що `setUnlocked(programNumber, pmtPID)` викликається з правильними аргументами |
| Memory leak при багатьох каналах | Зростання споживання пам'яті | Реалізувати TTL або LRU для неактивних записів у `programMap` |

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Створення та використання programMap (внутрішня логіка astits):
pm := newProgramMap()

// Додати мапінг (тільки після захоплення mutex!)
pm.mu.Lock()
pm.setUnlocked(programNumber, pmtPID)  // program 1 → PID 4096
pm.mu.Unlock()

// Перевірити наявність
pm.mu.RLock()
exists := pm.existsUnlocked(somePID)
pm.mu.RUnlock()

// 2. У вашому коді — аналогічний патерн для channel-aware мапінгу:
type ChannelProgramCache struct {
    mu   sync.RWMutex
    data map[string]map[uint16]uint16  // channel → (program → PID)
}

func (c *ChannelProgramCache) Set(ch string, prog, pid uint16) {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.data[ch] == nil { c.data[ch] = make(map[uint16]uint16) }
    c.data[ch][prog] = pid
}

func (c *ChannelProgramCache) Get(ch string, prog uint16) (uint16, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if m, ok := c.data[ch]; ok {
        pid, exists := m[prog]
        return pid, exists
    }
    return 0, false
}
```

---

## 📚 Корисні посилання

- [MPEG-TS PAT структура](https://en.wikipedia.org/wiki/Program-specific_information#Program_Association_Table)
- [astits source: programMap](https://github.com/asticode/go-astits/blob/master/demuxer.go) (шукати `type programMap`)
- [Go sync.RWMutex best practices](https://pkg.go.dev/sync#RWMutex)

> 💡 **Ключова ідея**: `programMap` — це простий, але критичний компонент для ефективного фільтрування потоків у real-time демуксингу. У вашому багатоканальному CCTV пайплайні аналогічна структура допоможе маршрутизувати дані між каналами без зайвого копіювання та з мінімальним локуванням.

Якщо потрібно — можу допомогти:
- 🗂️ Реалізувати `ChannelProgramCache` з TTL для автоматичного очищення неактивних програм
- 🧪 Написати integration-тест для перевірки маршрутизації пакетів між каналами
- 📈 Додати метрики для моніторингу "гарячих" програм у Prometheus

🛠️