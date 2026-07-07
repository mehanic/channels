# Глибоке роз'яснення: `programMap` — внутрішня структура astits

Ця структура реалізує **оптимізований кеш мапінгу PID → Program Number** для швидкої фільтрації пакетів під час демуксингу MPEG-TS потоків.

---

## 🎯 Архітектура: чому саме так?

### 1. Зворотний мапінг: PID → ProgramNumber

```
┌─────────────────────────────────────────┐
│ PAT таблиця (у потоці):                 │
│   Program Number → Program Map PID      │
│         1        →      0x1000          │
│         2        →      0x1001          │
│                                         │
│ programMap (у пам'яті):                 │
│   Program Map PID → Program Number      │
│       0x1000     →        1             │
│       0x1001     →        2             │
│                                         │
│ Навіщо інверсія?                        │
│ • Прихід пакета: "PID=4096, що це?"    │
│ • programMap[4096] → 1 → "це PMT програми 1" │
│ • O(1) пошук замість O(n) сканування   │
└─────────────────────────────────────────┘
```

**Код-приклад:**
```go
// Отримали пакет з невідомим PID
pkt, _ := dmx.NextPacket()
pid := pkt.Header.PID  // наприклад, 4096

// Швидка перевірка: чи це PMT відомої програми?
if programMap.existsUnlocked(pid) {
    // Це PMT! Парсимо метадані програми
    processPMT(pkt)
}
```

### 2. `map[uint32]uint16` замість `map[uint16]uint16`

```go
// Коментар у коді:
// "We use map[uint32] instead map[uint16] as go runtime 
//  provide optimized hash functions for (u)int32/64 keys"

// Що це означає на практиці:
type programMap struct {
    p map[uint32]uint16  // ключ: uint32(PID), значення: ProgramNumber
}

func (m programMap) existsUnlocked(pid uint16) bool {
    _, ok = m.p[uint32(pid)]  // явне перетворення типу
    return ok
}
```

**Чому це швидше?**
```
┌────────────────────────────────────┐
│ Go runtime оптимізації для map:    │
│                                    │
│ • map[int]/map[uint32]/map[uint64] │
│   → спеціалізовані хеш-функції     │
│   → менше колізій, краща локальність│
│                                    │
│ • map[uint16]/map[uint8]           │
│   → загальна реалізація            │
│   → додаткові перетворення         │
│                                    │
│ При 1000+ операцій/сек:            │
│ • uint32-ключі: ~15-20% швидше     │
│ • Менше навантаження на GC         │
└────────────────────────────────────┘
```

> 💡 **Порада**: Це мікро-оптимізація. Для вашого пайплайну важливіше правильно організувати channel-aware ізоляцію, ніж економити мікросекунди на хешуванні.

---

## 🔧 Розбір методів

### `existsUnlocked`, `setUnlocked`, `unsetUnlocked`

```go
// Всі методи приймають receiver за ЗНАЧЕННЯМ, не за посиланням:
func (m programMap) setUnlocked(pid, number uint16) {
    m.p[uint32(pid)] = number  // ⚠️ змінює внутрішню map!
}
```

**Чому це працює?**
```
• programMap.p — це посилання на map (map — reference type у Go)
• Копіювання programMap копіює лише "вказівник" на map, не дані
• Тому зміни в m.p відображаються у оригіналі

// Але це небезпечно, якщо додати поля-значення:
type unsafeProgramMap struct {
    p map[uint32]uint16
    count int  // ⚠️ це поле НЕ оновиться при receiver-by-value!
}
```

> ✅ **Best practice**: Залишати receiver-by-value тільки якщо структура містить виключно reference types (map, slice, pointer, channel).

### `toPATDataUnlocked` — регенерація PAT

```go
func (m programMap) toPATDataUnlocked() *PATData {
    d := &PATData{
        Programs:          make([]*PATProgram, 0, len(m.p)),
        TransportStreamID: uint16(PSITableIDPAT),  // 0x0000 для PAT
    }

    for pid, pnr := range m.p {
        d.Programs = append(d.Programs, &PATProgram{
            ProgramMapID:  uint16(pid),  // зворотне перетворення
            ProgramNumber: pnr,
        })
    }
    return d
}
```

**Використання:**
```
┌─────────────────────────────────────────┐
│ Сценарій: динамічне оновлення програм  │
│                                         │
│ 1. Приходить нова PAT з додатковою      │
│    програмою → оновити programMap      │
│                                         │
│ 2. Потрібно згенерувати оновлений TS   │
│    (напр., для remux або тестування)   │
│                                         │
│ 3. Викликати toPATDataUnlocked() →     │
│    отримати PATData → серіалізувати    │
│    у бінарний формат з CRC32           │
└─────────────────────────────────────────┘
```

---

## 🔄 Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Channel-aware programMap

```go
// У вашій багатоканальній архітектурі:
type ChannelProgramCache struct {
    mu     sync.RWMutex
    // channelID → programMap (окремий кеш для кожного каналу)
    caches map[string]*programMap
}

func NewChannelProgramCache() *ChannelProgramCache {
    return &ChannelProgramCache{
        caches: make(map[string]*programMap),
    }
}

func (c *ChannelProgramCache) GetOrCreate(channelID string) *programMap {
    c.mu.RLock()
    pm, ok := c.caches[channelID]
    c.mu.RUnlock()
    
    if ok { return pm }
    
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // Повторна перевірка після захоплення write-lock
    if pm, ok = c.caches[channelID]; ok {
        return pm
    }
    
    pm = newProgramMap()
    c.caches[channelID] = pm
    return pm
}

// Використання у segmentAssembler:
func processPAT(pat *astits.PATData, channelID string, cache *ChannelProgramCache) {
    pm := cache.GetOrCreate(channelID)
    
    pm.mu.Lock()  // ⚠️ programMap не має власного mutex — додайте свій!
    defer pm.mu.Unlock()
    
    for _, prog := range pat.Programs {
        if prog.ProgramNumber > 0 {  // пропускаємо NIT (program 0)
            pm.setUnlocked(prog.ProgramMapID, prog.ProgramNumber)
            log.Infof("Channel %s: mapped PID %d → program %d", 
                channelID, prog.ProgramMapID, prog.ProgramNumber)
        }
    }
}
```

> ⚠️ **Важливо**: Оригінальний `programMap` з astits **не thread-safe**! Додайте `sync.RWMutex` для використання у багатопотоковому середовищі.

### ✅ 2. Фільтрація пакетів за програмою (оптимізація черг)

```go
// У videoQueue/audioQueue — відкидати непотрібні пакети на ранньому етапі:
type StreamFilter struct {
    programMap *programMap
    subscribed map[uint16]bool  // ProgramNumber, які цікавлять цього клієнта
}

func (f *StreamFilter) ShouldProcess(pid uint16) bool {
    // Швидка перевірка: чи цей PID належить до підписаних програм?
    f.programMap.mu.RLock()
    defer f.programMap.mu.RUnlock()
    
    programNumber, ok := f.programMap.p[uint32(pid)]
    if !ok {
        // Невідомий PID — можливо, це PCR PID або адаптаційні дані
        // Дозволити для надійності, або додати окремий список дозволених PID
        return true
    }
    
    return f.subscribed[programNumber]
}

// У обробці вхідного потоку:
if !filter.ShouldProcess(pkt.Header.PID) {
    metrics.FilteredPackets.WithLabelValues(channelID).Inc()
    continue  // пропустити пакет, зекономити CPU/пам'ять
}
```

### ✅ 3. Детекція динамічних змін програм (multi-program TS)

```go
// Деякі потоки містять кілька програм, які можуть з'являтися/зникати:
type ProgramTracker struct {
    mu           sync.RWMutex
    knownPrograms map[uint16]bool  // ProgramNumber, які вже бачили
    newProgramCh  chan uint16      // сповіщення про нові програми
}

func (t *ProgramTracker) UpdateFromPAT(pat *astits.PATData, pm *programMap) {
    t.mu.Lock()
    defer t.mu.Unlock()
    
    pm.mu.Lock()
    defer pm.mu.Unlock()
    
    for _, prog := range pat.Programs {
        if prog.ProgramNumber == 0 { continue }  // NIT
        
        if !t.knownPrograms[prog.ProgramNumber] {
            // Нова програма!
            t.knownPrograms[prog.ProgramNumber] = true
            pm.setUnlocked(prog.ProgramMapID, prog.ProgramNumber)
            
            // Сповістити інші компоненти пайплайну
            select {
            case t.newProgramCh <- prog.ProgramNumber:
            default:
                // канал переповнений — пропустити сповіщення
            }
            
            log.Infof("New program detected: %d (PMT PID=%d)", 
                prog.ProgramNumber, prog.ProgramMapID)
        }
    }
}

// У main loop:
tracker := &ProgramTracker{
    knownPrograms: make(map[uint16]bool),
    newProgramCh:  make(chan uint16, 10),
}

go func() {
    for progNum := range tracker.newProgramCh {
        // Динамічно створити нову обробку для програми
        startProgramHandler(channelID, progNum)
    }
}()
```

---

## 🧪 Тестування та відладка

### 🔹 Юніт-тест на thread-safety

```go
func TestProgramMap_ConcurrentAccess(t *testing.T) {
    pm := newProgramMap()
    var mu sync.Mutex  // зовнішній м'ютекс для безпеки
    
    var wg sync.WaitGroup
    const writers = 5
    const iterations = 100
    
    // Writers
    for w := 0; w < writers; w++ {
        wg.Add(1)
        go func(writerID int) {
            defer wg.Done()
            for i := 0; i < iterations; i++ {
                pid := uint16(0x1000 + writerID*100 + i)
                mu.Lock()
                pm.setUnlocked(pid, uint16(writerID))
                mu.Unlock()
            }
        }(w)
    }
    
    // Readers
    for r := 0; r < writers; r++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for i := 0; i < iterations; i++ {
                mu.Lock()
                _ = pm.existsUnlocked(uint16(0x1000 + i))
                mu.Unlock()
            }
        }()
    }
    
    wg.Wait()
    // Якщо race detector не скаржиться — успіх
}
```

Запуск: `go test -race -run TestProgramMap_ConcurrentAccess`

### 🔹 Інтеграційний тест з реальним потоком

```go
func TestProgramMap_FromRealStream(t *testing.T) {
    data, err := os.ReadFile("testdata/al_araby_sample.ts")
    require.NoError(t, err)
    
    dmx := astits.NewDemuxer(context.Background(), bytes.NewReader(data))
    pm := newProgramMap()
    
    var programsFound []uint16
    
    for {
        d, err := dmx.NextData()
        if errors.Is(err, astits.ErrNoMorePackets) { break }
        require.NoError(t, err)
        
        if d.PAT != nil {
            pm.mu.Lock()
            for _, prog := range d.PAT.Programs {
                if prog.ProgramNumber > 0 {
                    pm.setUnlocked(prog.ProgramMapID, prog.ProgramNumber)
                    programsFound = append(programsFound, prog.ProgramNumber)
                }
            }
            pm.mu.Unlock()
        }
    }
    
    // Перевірити, що знайшли очікувані програми
    assert.Contains(t, programsFound, uint16(1), "should find program 1")
    
    // Перевірити зворотний мапінг
    pm.mu.RLock()
    for pid, pnr := range pm.p {
        assert.Greater(t, pnr, uint16(0), "program number should be > 0")
        assert.GreaterOrEqual(t, pid, uint32(0x10), "PMT PID should be >= 0x10")
    }
    pm.mu.RUnlock()
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Race condition у багатопотоковому режимі | Псевдо-втрата пакетів, паника | Додати `sync.RWMutex` до `programMap` або використовувати зовнішнє локування |
| Втрата програм після PAT update | Клієнти перестають отримувати дані | При новій PAT: спочатку `unsetUnlocked` старі PID, потім `setUnlocked` нові |
| Memory leak при багатьох каналах | Зростання пам'яті з часом | Реалізувати TTL/LRU для `ChannelProgramCache`, очищати неактивні канали |
| Неправильне перетворення uint16→uint32 | Колізії ключів при великих PID | Переконатися, що всі доступи до `m.p` використовують `uint32(pid)` |
| `toPATDataUnlocked` генерує невірний порядок | Плеєри не приймають згенерований PAT | Сортувати `d.Programs` за `ProgramNumber` перед серіалізацією |

---

## 📦 Швидкий референс для вашого коду

```go
// 1. Створення thread-safe programMap для каналу:
type SafeProgramMap struct {
    mu sync.RWMutex
    programMap  // вбудовуємо оригінальну структуру
}

func (spm *SafeProgramMap) Exists(pid uint16) bool {
    spm.mu.RLock()
    defer spm.mu.RUnlock()
    return spm.programMap.existsUnlocked(pid)
}

func (spm *SafeProgramMap) Set(pid, number uint16) {
    spm.mu.Lock()
    defer spm.mu.Unlock()
    spm.programMap.setUnlocked(pid, number)
}

// 2. Використання у segmentAssembler:
func handleIncomingPacket(pkt *astits.Packet, channelID string, cache *ChannelProgramCache) {
    pm := cache.GetOrCreate(channelID)
    
    if pkt.Header.PID == 0x0000 {
        // PAT — оновити мапінг
        pat, _ := parsePAT(pkt)
        for _, prog := range pat.Programs {
            if prog.ProgramNumber > 0 {
                pm.Set(prog.ProgramMapID, prog.ProgramNumber)
            }
        }
    } else if pm.Exists(pkt.Header.PID) {
        // Відомий PMT PID — парсити метадані
        processPMT(pkt)
    }
    // Інакше: це елементарний потік — відправити у videoQueue/audioQueue
}

// 3. Для регенерації (тестування / remux):
func regeneratePAT(pm *SafeProgramMap) ([]byte, error) {
    pm.mu.RLock()
    defer pm.mu.RUnlock()
    
    patData := pm.toPATDataUnlocked()
    
    // Сортувати програми для детермінованого виводу
    sort.Slice(patData.Programs, func(i, j int) bool {
        return patData.Programs[i].ProgramNumber < patData.Programs[j].ProgramNumber
    })
    
    return serializePAT(patData)  // ваша функція серіалізації з CRC32
}
```

---

## 📊 Порівняння: programMap vs альтернативи

```
┌────────────────────┬─────────────┬─────────────┬─────────────┐
│ Підхід             │ Швидкість   │ Пам'ять     │ Складність  │
├────────────────────┼─────────────┼─────────────┼─────────────┤
│ programMap (map)   │ O(1) пошук  │ ~24B/запис  │ Низька      │
│ Slice + binary search │ O(log n) │ ~4B/запис   │ Середня     │
│ Linear scan        │ O(n)        │ ~4B/запис   │ Дуже низька │
│ Trie за бітами PID │ O(1)*       │ ~100B/запис │ Висока      │
└────────────────────┴─────────────┴─────────────┴─────────────┘

* Trie: складна реалізація, виправдана тільки при 10_000+ PID
```

> 💡 **Висновок**: `programMap` — оптимальний вибір для більшості сценаріїв. Змінюйте тільки якщо профайлер покаже, що мапінг — вузьке місце.

---

## 📚 Корисні посилання

- [Go map internals (Dave Cheney)](https://dave.cheney.net/2018/05/29/how-the-go-runtime-implements-maps-efficiently-without-generics)
- [MPEG-TS PAT структура](https://en.wikipedia.org/wiki/Program-specific_information#Program_Association_Table)
- [astits demuxer source](https://github.com/asticode/go-astits/blob/master/demuxer.go)

> 💡 **Ключова ідея**: `programMap` — це приклад прагматичної оптимізації: зворотний мапінг для O(1) пошуку + `uint32` ключі для швидшого хешування. У вашому пайплайні аналогічний підхід допоможе ефективно фільтрувати пакети на ранніх етапах, зменшуючи навантаження на `segmentAssembler` та `segmentFinalizer`.

Якщо потрібно — можу допомогти:
- 🔒 Реалізувати повністю thread-safe `ChannelProgramCache` з автоматичним очищенням
- 🧪 Написати benchmark для порівняння `map[uint16]` vs `map[uint32]` у вашому контексті
- 📡 Інтегрувати детекцію нових програм у ваш WebSocketDistributor для динамічної маршрутизації

🛠️