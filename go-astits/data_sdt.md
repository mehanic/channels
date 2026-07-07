# Глибоке роз'яснення: `sdt.go` — парсинг SDT (Service Description Table) у astits

Цей файл містить **реалізацію парсингу секції SDT (Service Description Table)** — таблиці службової інформації стандарту DVB, що містить описи каналів/сервісів: назви, провайдери, статус, можливості (EIT, CA) та дескриптори. Це ключовий компонент для EPG та навігації у плеєрах.

---

## 🎯 Що таке SDT і навіщо він потрібен?

```
┌─────────────────────────────────────────┐
│ SDT (Service Description Table) у DVB: │
│                                         │
│ 🔹 Призначення:                         │
│   • Опис доступних сервісів (каналів)  │
│   • Назва каналу, провайдер, логотип   │
│   • Статус (running_status)            │
│   • Можливості: EIT, Free/CA mode      │
│   • Дескриптори для розширеної інформації│
│                                         │
│ 🔹 Формат (ETSI EN 300 468, §5.2.4):   │
│ • table_id = 0x42 (Actual) / 0x46 (Other)|
│ • transport_stream_id: ID цього потоку  │
│ • original_network_id: ID мережі-джерела│
│ • services_loop: цикл описів сервісів  │
│   • service_id, running_status, flags │
│   • descriptors_loop: метадані сервісу │
│ • CRC32: валідація цілісності          │
│                                         │
│ 🔹 Для вашого CCTV HLS пайплайну:      │
│   • Генерація назв каналів у плейлистах│
│   • EPG інтеграція через дескриптори   │
│   • Фільтрація активних/неактивних каналів│
└─────────────────────────────────────────┘
```

---

## 🔧 Константи статусів запуску

```go
const (
    RunningStatusUndefined           = 0  // невизначено
    RunningStatusNotRunning          = 1  // не працює
    RunningStatusStartsInAFewSeconds = 2  // стартує за кілька секунд
    RunningStatusPausing             = 3  // пауза
    RunningStatusRunning             = 4  // працює ✅
    RunningStatusServiceOffAir       = 5  // в ефірі немає
)
```

> 💡 **Важливо**: У вашому пайплайні фільтруйте канали за `RunningStatusRunning` (4) для відображення тільки активних потоків у плейлистах.

---

## 📦 Структури даних

### `SDTData` — контейнер для всієї таблиці

```go
type SDTData struct {
    OriginalNetworkID uint16              // 🎯 ID мережі-джерела (для ідентифікації)
    Services          []*SDTDataService   // 🎯 список сервісів (каналів)
    TransportStreamID uint16              // 🎯 ID цього транспортного потоку
}
```

### `SDTDataService` — опис одного каналу

```go
type SDTDataService struct {
    Descriptors            []*Descriptor  // 🎯 метадані: назва, провайдер, логотип...
    HasEITPresentFollowing bool           // 🎯 чи є EIT для поточної/наступної події
    HasEITSchedule         bool           // 🎯 чи є EIT з розкладом
    HasFreeCSAMode         bool           // 🎯 чи канал без шифрування (Free-to-Air)
    RunningStatus          uint8          // 🎯 статус: 0-5 (див. константи вище)
    ServiceID              uint16         // 🎯 унікальний ID сервісу у потоці
}
```

---

## 🔍 Функція `parseSDTSection`: покроковий розбір

```go
func parseSDTSection(i *astikit.BytesIterator, offsetSectionsEnd int, tableIDExtension uint16) (*SDTData, error) {
    // 🔹 1. Ініціалізація з transport_stream_id (передається зовні)
    d := &SDTData{TransportStreamID: tableIDExtension}
    
    // 🔹 2. Original network ID (2 байти, big-endian)
    bs, _ := i.NextBytesNoCopy(2)
    d.OriginalNetworkID = uint16(bs[0])<<8 | uint16(bs[1])
    
    // 🔹 3. Пропустити reserved байт (1 байт = 0)
    i.Skip(1)
    
    // 🔹 4. Цикл по сервісах до кінця секції
    for i.Offset() < offsetSectionsEnd {
        s := &SDTDataService{}
        
        // ── Service ID (2 байти) ──
        bs, _ = i.NextBytesNoCopy(2)
        s.ServiceID = uint16(bs[0])<<8 | uint16(bs[1])
        
        // ── Прапорці байт 1 ──
        // [7-2] reserved | [1] EIT_schedule | [0] EIT_present_following
        b, _ := i.NextByte()
        s.HasEITSchedule = b&0x02 > 0          // біт 1
        s.HasEITPresentFollowing = b&0x01 > 0  // біт 0
        
        // ── Прапорці байт 2 ──
        // [7-5] running_status | [4] free_CA_mode | [3-0] reserved
        b, _ = i.NextByte()
        s.RunningStatus = uint8(b) >> 5        // старші 3 біти
        s.HasFreeCSAMode = b&0x10 > 0          // біт 4
        
        // 🔹 5. Критичний rewind: поточний байт використовується дескрипторами!
        i.Skip(-1)  // повернутися на 1 байт назад
        
        // 🔹 6. Парсинг дескрипторів сервісу
        s.Descriptors, err = parseDescriptors(i)
        if err != nil {
            return nil, fmt.Errorf("astits: parsing descriptors failed: %w", err)
        }
        
        // 🔹 7. Додати сервіс у результат
        d.Services = append(d.Services, s)
    }
    
    return d, nil
}
```

### 🎯 Ключовий момент: `i.Skip(-1)` перед дескрипторами

```
Проблема:
• Байт з running_status та free_CA_mode також є ПЕРШИМ байтом дескрипторів
• parseDescriptors() очікує початок з descriptor_tag
• Якщо не відмотати — перший дескриптор буде пошкоджений

Рішення:
• i.Skip(-1) повертає ітератор на 1 байт назад
• parseDescriptors() читає той самий байт як descriptor_tag
• Це стандартний патерн у astits для вкладених структур

Візуалізація:
Потік байтів: [running_status+free_CA][descriptor_tag][descriptor_length][payload...]
                           ↑
                    читаємо як прапорці
                    потім Skip(-1)
                           ↓
                    читаємо як descriptor_tag
```

> ⚠️ **Увага**: Цей патерн працює тільки якщо `parseDescriptors` викликається одразу після читання прапорців. Будь-яка додаткова обробка між ними зламає парсинг.

---

## 🧮 Формат SDT секції у деталях

```
SDT Section Payload (без PSI заголовка та CRC):
┌─────────────────────────────────┐
│ [16] original_network_id        │
├─────────────────────────────────┤
│ [8]  reserved_future_use = 0    │
├─────────────────────────────────┤
│ Service loop (повтор для кожного сервісу):
│   [16] service_id              │
│   [6]  reserved_future_use     │
│   [1]  EIT_schedule_flag       │
│   [1]  EIT_present_following_flag│
│   [3]  running_status          │
│   [1]  free_CA_mode            │
│   [12] descriptors_loop_length │
│   [N]  descriptors...          │
└─────────────────────────────────┘

Повна PSI секція (додається на вищому рівні):
[8]  table_id = 0x42 (actual) / 0x46 (other)
[12] section_length
[16] transport_stream_id
[16] reserved + version + current_next
[8]  section_number = 0
[8]  last_section_number = 0
[16] original_network_id
[8]  reserved
[... service loop ...]
[32] CRC32
```

### Приклад розбору прапорців сервісу

```
Вхідні байти: 0b00000011 0b10110000
              ↑          ↑
              │          └─ Байт 2: [7-5]running=5, [4]free_CA=1, [3-0]reserved=0
              └─ Байт 1: [7-2]reserved=0, [1]EIT_sched=1, [0]EIT_pres=1

Результат:
• HasEITSchedule = true (біт 1 байта 1)
• HasEITPresentFollowing = true (біт 0 байта 1)
• RunningStatus = 0b101 = 5 (reserved/off-air)
• HasFreeCSAMode = true (біт 4 байта 2)
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Збагачення метаданих каналів через SDT

```go
// У VideoManifestProxy — використання SDT для назв каналів:
type ChannelMetadata struct {
    ServiceID      uint16
    Name           string   // з дескриптора "service"
    Provider       string   // з дескриптора "service"
    HasEPG         bool     // з прапорців EIT
    IsFreeToAir    bool     // з free_CA_mode
    RunningStatus  string   // human-readable статус
}

func handleSDT(sdt *astits.SDTData, channelID string) map[uint16]*ChannelMetadata {
    metadata := make(map[uint16]*ChannelMetadata)
    
    for _, svc := range sdt.Services {
        meta := &ChannelMetadata{
            ServiceID:     svc.ServiceID,
            HasEPG:        svc.HasEITSchedule || svc.HasEITPresentFollowing,
            IsFreeToAir:   svc.HasFreeCSAMode,
            RunningStatus: runningStatusToString(svc.RunningStatus),
        }
        
        // 🔹 Витягнути назву та провайдера з дескрипторів
        for _, desc := range svc.Descriptors {
            if desc.Service != nil {
                meta.Name = string(desc.Service.Name)
                meta.Provider = string(desc.Service.Provider)
                break
            }
        }
        
        metadata[svc.ServiceID] = meta
        log.Infof("Channel %s: service %d = '%s' by %s (EPG=%v, FTA=%v, status=%s)",
            channelID, svc.ServiceID, meta.Name, meta.Provider, 
            meta.HasEPG, meta.IsFreeToAir, meta.RunningStatus)
    }
    
    return metadata
}

func runningStatusToString(status uint8) string {
    switch status {
    case astits.RunningStatusUndefined: return "undefined"
    case astits.RunningStatusNotRunning: return "not running"
    case astits.RunningStatusStartsInAFewSeconds: return "starting soon"
    case astits.RunningStatusPausing: return "pausing"
    case astits.RunningStatusRunning: return "running"
    case astits.RunningStatusServiceOffAir: return "off-air"
    default: return fmt.Sprintf("reserved(%d)", status)
    }
}
```

### ✅ 2: Фільтрація активних каналів для HLS

```go
// У генерації плейлиста — показувати тільки "running" канали:
func generateChannelPlaylist(sdt *astits.SDTData, activeServiceID uint16) *HLSPlaylist {
    playlist := NewHLSPlaylist()
    
    for _, svc := range sdt.Services {
        // 🔹 Пропустити неактивні канали
        if svc.RunningStatus != astits.RunningStatusRunning {
            log.Debugf("Skipping service %d: status=%d (%s)", 
                svc.ServiceID, svc.RunningStatus, runningStatusToString(svc.RunningStatus))
            continue
        }
        
        // 🔹 Додати канал у плейлист
        meta := extractServiceMetadata(svc)
        playlist.AddChannel(meta.ServiceID, meta.Name, meta.Provider)
        
        // 🔹 Якщо це активний канал — додати сегменти
        if svc.ServiceID == activeServiceID {
            addSegmentsToPlaylist(playlist, activeServiceID)
        }
    }
    
    return playlist
}
```

### ✅ 3: Моніторинг доступності каналів

```go
// monitoring.Monitor — метрики для SDT:
type SDTMetrics struct {
    ServicesDiscovered  *prometheus.CounterVec  // кількість знайдених сервісів
    ActiveServicesGauge *prometheus.GaugeVec    // скільки каналів "running"
    EPGEnabledServices  *prometheus.CounterVec  // скільки мають EIT
    FreeToAirServices   *prometheus.CounterVec  // скільки без шифрування
    LastSDTUpdate       *prometheus.GaugeVec    // timestamp останнього SDT
}

// У обробці SDT:
func monitorSDT(sdt *astits.SDTData, channelID string, metrics *SDTMetrics) {
    if sdt == nil {
        metrics.LastSDTUpdate.WithLabelValues(channelID).Set(0)
        return
    }
    
    metrics.LastSDTUpdate.WithLabelValues(channelID).Set(float64(time.Now().Unix()))
    
    for _, svc := range sdt.Services {
        metrics.ServicesDiscovered.WithLabelValues(channelID).Inc()
        
        if svc.RunningStatus == astits.RunningStatusRunning {
            metrics.ActiveServicesGauge.WithLabelValues(channelID).Inc()
        }
        if svc.HasEITSchedule || svc.HasEITPresentFollowing {
            metrics.EPGEnabledServices.WithLabelValues(channelID).Inc()
        }
        if svc.HasFreeCSAMode {
            metrics.FreeToAirServices.WithLabelValues(channelID).Inc()
        }
    }
}
```

### ✅ 4: Fallback при відсутності SDT

```go
// Якщо потік не містить SDT — використати дефолтні метадані:
func getDefaultChannelMetadata(channelID string, serviceID uint16) *ChannelMetadata {
    return &ChannelMetadata{
        ServiceID:     serviceID,
        Name:          fmt.Sprintf("Channel %d", serviceID),
        Provider:      "Unknown",
        HasEPG:        false,
        IsFreeToAir:   true,  // припускаємо FTA за замовчуванням
        RunningStatus: "running",
    }
}

// Інтеграція у обробку:
func safeHandleSDT(data *astits.DemuxerData, channelID string) map[uint16]*ChannelMetadata {
    if data.SDT != nil {
        return handleSDT(data.SDT, channelID)
    }
    
    // Fallback: дефолтні метадані
    log.Warnf("Channel %s: no SDT data, using defaults", channelID)
    return map[uint16]*ChannelMetadata{
        1: getDefaultChannelMetadata(channelID, 1),
    }
}
```

### ✅ 5: Кешування останньої валідної SDT

```go
// SDT передається періодично (~10 сек) — кешувати для використання між оновленнями:
type SDTCache struct {
    mu         sync.RWMutex
    lastValid  *astits.SDTData
    lastUpdate time.Time
    ttl        time.Duration
}

func (c *SDTCache) Update(sdt *astits.SDTData) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.lastValid = sdt
    c.lastUpdate = time.Now()
}

func (c *SDTCache) Get() (*astits.SDTData, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    
    if c.lastValid == nil {
        return nil, false
    }
    if time.Since(c.lastUpdate) > c.ttl {
        return nil, false  // застарілі дані
    }
    return c.lastValid, true
}

// Використання:
sdtCache := &SDTCache{ttl: 30 * time.Second}

// При отриманні нової SDT:
if data.SDT != nil {
    sdtCache.Update(data.SDT)
}

// При потребі метаданих:
if sdt, ok := sdtCache.Get(); ok {
    metadata := handleSDT(sdt, channelID)
} else {
    // Fallback на дефолтні значення
    metadata = getDefaultChannelMetadata(channelID, 1)
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Базовий тест на парсинг

```go
func TestParseSDTSection_Basic(t *testing.T) {
    // Підготувати тестові байти: original_network_id + service loop
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.Write(uint16(123))  // original_network_id
    w.Write(uint8(0))     // reserved
    
    // Один сервіс
    w.Write(uint16(10))   // service_id
    w.Write(uint8(0b00000011))  // EIT flags: schedule=1, present=1
    w.Write(uint8(0b10110000))  // running=5, free_CA=1
    
    // Дескриптори (напр., service descriptor)
    writeServiceDescriptor(w, "Test Channel", "Test Provider")
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    sdt, err := parseSDTSection(iter, buf.Len(), uint16(456))  // transport_stream_id
    
    assert.NoError(t, err)
    assert.Equal(t, uint16(123), sdt.OriginalNetworkID)
    assert.Equal(t, uint16(456), sdt.TransportStreamID)
    assert.Len(t, sdt.Services, 1)
    
    svc := sdt.Services[0]
    assert.Equal(t, uint16(10), svc.ServiceID)
    assert.True(t, svc.HasEITSchedule)
    assert.True(t, svc.HasEITPresentFollowing)
    assert.Equal(t, uint8(5), svc.RunningStatus)
    assert.True(t, svc.HasFreeCSAMode)
}
```

### 🔹 Тест на кілька сервісів

```go
func TestParseSDTSection_MultipleServices(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.Write(uint16(1))   // original_network_id
    w.Write(uint8(0))    // reserved
    
    // Сервіс 1: running
    w.Write(uint16(100))
    w.Write(uint8(0b00000011))  // EIT=1,1
    w.Write(uint8(0b10000000))  // running=4 (running), free_CA=0
    writeServiceDescriptor(w, "News", "Broadcaster A")
    
    // Сервіс 2: off-air
    w.Write(uint16(101))
    w.Write(uint8(0b00000000))  // EIT=0,0
    w.Write(uint8(0b10100000))  // running=5 (off-air), free_CA=0
    writeServiceDescriptor(w, "Sports", "Broadcaster B")
    
    iter := astikit.NewBytesIterator(buf.Bytes())
    sdt, err := parseSDTSection(iter, buf.Len(), uint16(1))
    
    assert.NoError(t, err)
    assert.Len(t, sdt.Services, 2)
    
    assert.Equal(t, uint16(100), sdt.Services[0].ServiceID)
    assert.Equal(t, astits.RunningStatusRunning, sdt.Services[0].RunningStatus)
    
    assert.Equal(t, uint16(101), sdt.Services[1].ServiceID)
    assert.Equal(t, astits.RunningStatusServiceOffAir, sdt.Services[1].RunningStatus)
}
```

### 🔹 Тест на rewind логіку з дескрипторами

```go
func TestParseSDTSection_DescriptorRewind(t *testing.T) {
    // Перевірити, що дескриптори парсяться коректно після rewind
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.Write(uint16(1))   // original_network_id
    w.Write(uint8(0))    // reserved
    w.Write(uint16(1))   // service_id
    w.Write(uint8(0))    // EIT flags
    w.Write(uint8(0b00000000))  // running=0, free_CA=0
    
    // Дескриптор: StreamIdentifier (tag=0x52, length=1, component_tag=7)
    w.Write(uint8(0x52))  // descriptor_tag
    w.Write(uint8(1))     // descriptor_length
    w.Write(uint8(7))     // component_tag
    
    iter := astikit.NewBytesIterator(buf.Bytes())
    sdt, err := parseSDTSection(iter, buf.Len(), uint16(1))
    
    assert.NoError(t, err)
    assert.Len(t, sdt.Services, 1)
    assert.Len(t, sdt.Services[0].Descriptors, 1)
    
    desc := sdt.Services[0].Descriptors[0]
    assert.Equal(t, uint8(0x52), desc.Tag)
    assert.NotNil(t, desc.StreamIdentifier)
    assert.Equal(t, uint8(7), desc.StreamIdentifier.ComponentTag)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання прапорців | `HasEITSchedule` не співпадає з очікуваним | Перевірити бітові маски: `b&0x02` для EIT_schedule (біт 1), `b&0x01` для EIT_present_following (біт 0) |
| Дескриптори не парсяться | `svc.Descriptors` порожній або містить сміття | Перевірити `i.Skip(-1)` перед `parseDescriptors`; переконатися, що немає додаткових читань між прапорцями та дескрипторами |
| `running_status` некоректний | Значення 5-7 інтерпретуються як помилка | Використовувати `runningStatusToString()` з fallback для `default` випадку |
| SDT не надходить у потоці | `NextData()` ніколи не повертає `*DemuxerData` з `SDT` | Це нормально: SDT передається періодично (~10 сек); реалізувати кешування з TTL |
| Multiple SDT sections (actual/other) | Плутанина між table_id 0x42 та 0x46 | Фільтрувати за `table_id`: 0x42 = actual TS (ваш потік), 0x46 = other TS (інші потоки); обробляти тільки actual |
| `offsetSectionsEnd` неправильний | Цикл сервісів не зупиняється або обрізається | Переконатися, що `offsetSectionsEnd` передається з `parsePSISection` і враховує `section_length` мінус заголовок |

### Приклад діагностики прапорців:

```go
func debugSDTFlags(b1, b2 byte) {
    log.Infof("SDT flags debug: byte1=0x%02X, byte2=0x%02X", b1, b2)
    log.Infof("  EIT_schedule: %v (bit 1 of byte1: %d)", b1&0x02 > 0, (b1>>1)&1)
    log.Infof("  EIT_present_following: %v (bit 0 of byte1: %d)", b1&0x01 > 0, b1&1)
    log.Infof("  running_status: %d (bits 7-5 of byte2: 0b%03b)", b2>>5, (b2>>5)&0x07)
    log.Infof("  free_CA_mode: %v (bit 4 of byte2: %d)", b2&0x10 > 0, (b2>>4)&1)
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Парсинг SDT з вхідного потоку:
func handleSDTData(data *astits.DemuxerData, channelID string) error {
    if data.SDT == nil {
        return nil  // не SDT
    }
    
    sdt := data.SDT
    log.Infof("Channel %s: SDT with %d services (TS_ID=%d, Net_ID=%d)", 
        channelID, len(sdt.Services), sdt.TransportStreamID, sdt.OriginalNetworkID)
    
    // Оновити кеш метаданих
    metadata := handleSDT(sdt, channelID)
    updateChannelMetadataCache(channelID, metadata)
    
    return nil
}

// 2: Витягування назви каналу для HLS:
func getChannelName(sdt *astits.SDTData, serviceID uint16) string {
    for _, svc := range sdt.Services {
        if svc.ServiceID == serviceID {
            for _, desc := range svc.Descriptors {
                if desc.Service != nil {
                    return string(desc.Service.Name)
                }
            }
            return fmt.Sprintf("Service %d", serviceID)
        }
    }
    return fmt.Sprintf("Channel %d", serviceID)
}

// 3: Фільтрація активних каналів:
func getActiveServices(sdt *astits.SDTData) []uint16 {
    var active []uint16
    for _, svc := range sdt.Services {
        if svc.RunningStatus == astits.RunningStatusRunning {
            active = append(active, svc.ServiceID)
        }
    }
    return active
}

// 4: Моніторинг:
func monitorSDTHealth(sdt *astits.SDTData, channelID string, metrics *SDTMetrics) {
    if sdt == nil {
        metrics.LastSDTUpdate.WithLabelValues(channelID).Set(0)
        return
    }
    
    metrics.LastSDTUpdate.WithLabelValues(channelID).Set(float64(time.Now().Unix()))
    metrics.ServiceCount.WithLabelValues(channelID).Set(float64(len(sdt.Services)))
    
    active := 0
    for _, svc := range sdt.Services {
        if svc.RunningStatus == astits.RunningStatusRunning {
            active++
        }
    }
    metrics.ActiveServiceCount.WithLabelValues(channelID).Set(float64(active))
}

// 5: Helper для запису service descriptor у тестах:
func writeServiceDescriptor(w *astikit.BitsWriter, name, provider string) {
    w.Write(uint8(astits.DescriptorTagService))
    w.Write(uint8(3 + len(name) + len(provider)))  // length
    w.Write(uint8(astits.ServiceTypeDigitalTelevisionService))
    w.Write(uint8(len(provider)))
    w.Write([]byte(provider))
    w.Write(uint8(len(name)))
    w.Write([]byte(name))
}
```

---

## 📊 Матриця полів SDT для вашого пайплайну

```
Поле SDT               | Тип       | Використання у CCTV HLS
───────────────────────┼───────────┼─────────────────────────
TransportStreamID      | uint16    | ✅ Ідентифікація потоку для плейлиста
OriginalNetworkID      | uint16    | ✅ Розрізнення джерел (напр. супутник/кабель)
ServiceID              | uint16    | ✅ Унікальний ID каналу для маршрутизації
RunningStatus          | uint8     | ✅ Фільтрація активних/неактивних каналів
HasEITSchedule         | bool      | ✅ Ознака наявності EPG для каналу
HasFreeCSAMode         | bool      | ⚠️ Інформація про шифрування (логіка доступу)
Descriptors[Service]   | []byte    | ✅ Назва, провайдер, логотип для UI
Descriptors[ShortEvent]| []byte    | ⚠️ Короткий опис поточної передачі
```

---

## 📚 Корисні посилання

- [ETSI EN 300 468: SDT specification (§5.2.4)](https://dvb.org/wp-content/uploads/2019/12/a038_tm1217r37_en300468v1_17_1_-_rev-134_-_si_specification.pdf)
- [DVB Service Information poster](http://seidl.cs.vsb.cz/download/dvb/DVB_Poster.pdf)
- [astits SDT parsing source](https://github.com/asticode/go-astits/blob/master/data.go)

> 💡 **Ключова ідея**: SDT — це "каталог каналів" вашого DVB потоку. У вашому CCTV HLS пайплайні це дозволяє:
> - 📺 Автоматично генерувати назви каналів для плеєрів та EPG
> - 🎯 Фільтрувати активні канали для оптимізації ресурсів
> - 🔍 Відстежувати статус каналів (running/off-air) для моніторингу
> - 🌐 Підтримувати мультисервісні потоки з динамічним додаванням каналів

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати SDT-обробку у ваш `VideoManifestProxy` для динамічного оновлення плейлистів
- 🧩 Додати підтримку кешування метаданих каналів з TTL для fallback при втраті SDT
- 🧪 Написати integration-тест для перевірки генерації HLS-плейлистів з метаданими каналів

🛠️