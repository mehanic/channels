# Глибоке роз'яснення: Тест парсингу SDT (Service Description Table) у astits

Цей файл тестує **парсинг секції SDT (Service Description Table)** — таблиці службової інформації стандарту DVB, що містить описи каналів/сервісів: назви, провайдери, статус, можливості (EIT, CA) та дескриптори. Це ключовий компонент для EPG та навігації у плеєрах.

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

## 🔧 Структура `SDTData` та тестові дані

### Тип даних

```go
type SDTData struct {
    OriginalNetworkID uint16              // 🎯 ID мережі-джерела (для ідентифікації)
    Services          []*SDTDataService   // 🎯 список сервісів (каналів)
    TransportStreamID uint16              // 🎯 ID цього транспортного потоку
}

type SDTDataService struct {
    Descriptors            []*Descriptor  // 🎯 метадані: назва, провайдер, логотип...
    HasEITPresentFollowing bool           // 🎯 чи є EIT для поточної/наступної події
    HasEITSchedule         bool           // 🎯 чи є EIT з розкладом
    HasFreeCSAMode         bool           // 🎯 чи канал без шифрування (Free-to-Air)
    RunningStatus          uint8          // 🎯 статус: 0=undefined, 1-4=running/pausing...
    ServiceID              uint16         // 🎯 унікальний ID сервісу у потоці
}
```

### Тестові дані: `sdt` та `sdtBytes()`

```go
// Глобальна змінна — еталонне значення для тесту
var sdt = &SDTData{
    OriginalNetworkID: 2,
    Services: []*SDTDataService{{
        Descriptors:            descriptors,  // посилання на тестові дескриптори
        HasEITPresentFollowing: true,         // ✅ EIT для поточної події
        HasEITSchedule:         true,         // ✅ EIT з розкладом
        HasFreeCSAMode:         true,         // ✅ канал без шифрування
        RunningStatus:          5,            // ⚠️ 5 = undefined/reserved (тестове значення)
        ServiceID:              3,            // ID сервісу = 3
    }},
    TransportStreamID: 1,
}

// Генератор "еталонних" байтів для SDT секції (payload без заголовка)
func sdtBytes() []byte {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // 🔹 1. Original network ID (2 байти)
    w.Write(uint16(2))
    
    // 🔹 2. Reserved (1 байт = 0)
    w.Write(uint8(0))
    
    // 🔹 3. Service loop: Service ID (2 байти)
    w.Write(uint16(3))
    
    // 🔹 4. Service flags (6 біт reserved + 3 біти прапорців)
    w.Write("000000")              // reserved_future_use
    w.Write("1")                   // EIT_schedule_flag = true
    w.Write("1")                   // EIT_present_following_flag = true
    w.Write("101")                 // running_status = 5 (3 біти)
    w.Write("1")                   // free_CA_mode = true
    
    // 🔹 5. Дескриптори сервісу
    descriptorsBytes(w)           // виклик з тестів дескрипторів
    
    // Результат: []byte з серіалізованим payload SDT секції
    return buf.Bytes()
}
```

> 💡 **Важливо**: `sdtBytes()` генерує тільки **payload секції** (без PSI заголовка та CRC32). Функція `parseSDTSection` очікує саме payload + довжину, бо заголовок парситься на вищому рівні.

---

## 🔍 Тест `TestParseSDTSection`

```go
func TestParseSDTSection(t *testing.T) {
    // 🔹 1. Отримати тестові байти
    b := sdtBytes()
    
    // 🔹 2. Парсити секцію: ітератор + довжина + transport_stream_id
    d, err := parseSDTSection(astikit.NewBytesIterator(b), len(b), uint16(1))
    
    // 🔹 3. Перевірити результат
    assert.Equal(t, d, sdt)        // ✅ структурна рівність з еталоном
    assert.NoError(t, err)         // ✅ без помилок
}
```

### Що робить `parseSDTSection` (гіпотетична реалізація)

```go
func parseSDTSection(i *astikit.BytesIterator, sectionLength int, tsID uint16) (*SDTData, error) {
    d := &SDTData{
        TransportStreamID: tsID,  // передається зовні
    }
    
    // 🔹 1. Original network ID (2 байти)
    bs, _ := i.NextBytesNoCopy(2)
    d.OriginalNetworkID = uint16(bs[0])<<8 | uint16(bs[1])
    
    // 🔹 2. Reserved байт (пропускаємо)
    i.NextByte()
    
    // 🔹 3. Цикл сервісів до кінця секції
    offsetEnd := i.Offset() + sectionLength - 3  // мінус вже прочитані 3 байти
    
    for i.Offset() < offsetEnd {
        svc := &SDTDataService{}
        
        // Service ID (2 байти)
        bs, _ = i.NextBytesNoCopy(2)
        svc.ServiceID = uint16(bs[0])<<8 | uint16(bs[1])
        
        // Flags байт: [6 reserved][EIT_sched][EIT_pres][running_status(3)]
        b, _ := i.NextByte()
        svc.HasEITSchedule = b&0x02 > 0
        svc.HasEITPresentFollowing = b&0x01 > 0
        // running_status = (b >> 5) & 0x07  // старші 3 біти
        
        // Free CA mode: окремий біт у наступному байті
        b, _ = i.NextByte()
        svc.HasFreeCSAMode = b&0x10 > 0  // біт 4
        // running_status продовження...
        
        // Дескриптори сервісу
        svc.Descriptors, _ = parseDescriptors(i)
        
        d.Services = append(d.Services, svc)
    }
    
    return d, nil
}
```

> ⚠️ **Важливо**: Реальна реалізація може мати інший порядок біт — завжди звіряйтесь зі специфікацією ETSI EN 300 468.

---

## 🧮 Формат SDT секції у деталях

```
SDT Section Payload (без заголовка та CRC):
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
Вхідні біти: "000000" + "1" + "1" + "101" + "1"
             ↑      ↑    ↑    ↑     ↑
             │      │    │    │     └─ free_CA_mode = 1 (канал без шифрування)
             │      │    │    └─ running_status = 0b101 = 5 (undefined/reserved)
             │      │    └─ EIT_present_following = 1 (є EIT для поточної події)
             │      └─ EIT_schedule = 1 (є EIT з розкладом)
             └─ reserved = 0

running_status значення:
• 0 = undefined
• 1 = not running
• 2 = pausing
• 3 = running
• 4 = off-air
• 5-7 = reserved for future use
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
        log.Infof("Channel %s: service %d = '%s' by %s (EPG=%v, FTA=%v)",
            channelID, svc.ServiceID, meta.Name, meta.Provider, 
            meta.HasEPG, meta.IsFreeToAir)
    }
    
    return metadata
}

func runningStatusToString(status uint8) string {
    switch status {
    case 0: return "undefined"
    case 1: return "not running"
    case 2: return "pausing"
    case 3: return "running"
    case 4: return "off-air"
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
        if svc.RunningStatus != 3 {  // 3 = running
            log.Debugf("Skipping service %d: status=%d", svc.ServiceID, svc.RunningStatus)
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
    ServicesDiscovered *prometheus.CounterVec  // кількість знайдених сервісів
    ActiveServicesGauge *prometheus.GaugeVec   // скільки каналів "running"
    EPGEnabledServices *prometheus.CounterVec  // скільки мають EIT
    FreeToAirServices  *prometheus.CounterVec  // скільки без шифрування
}

// У обробці SDT:
func monitorSDT(sdt *astits.SDTData, channelID string, metrics *SDTMetrics) {
    for _, svc := range sdt.Services {
        metrics.ServicesDiscovered.WithLabelValues(channelID).Inc()
        
        if svc.RunningStatus == 3 {
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

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на кілька сервісів у SDT

```go
func TestParseSDTSection_MultipleServices(t *testing.T) {
    // Створити SDT з 3 сервісами
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.Write(uint16(1))  // original_network_id
    w.Write(uint8(0))   // reserved
    
    // Сервіс 1
    w.Write(uint16(100))  // service_id
    w.Write("0000001101") // flags: EIT=1,1; running=3; free_CA=1
    writeServiceDescriptors(w, "News Channel", "Broadcaster A")
    
    // Сервіс 2
    w.Write(uint16(101))
    w.Write("0000000010") // flags: EIT=0,0; running=2; free_CA=0
    writeServiceDescriptors(w, "Sports HD", "Broadcaster B")
    
    // Сервіс 3
    w.Write(uint16(102))
    w.Write("0000001001") // flags: EIT=1,0; running=1; free_CA=1
    writeServiceDescriptors(w, "Movies", "Broadcaster A")
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    sdt, err := parseSDTSection(iter, buf.Len(), uint16(1))
    
    assert.NoError(t, err)
    assert.Len(t, sdt.Services, 3)
    
    // Перевірити перший сервіс
    assert.Equal(t, uint16(100), sdt.Services[0].ServiceID)
    assert.True(t, sdt.Services[0].HasEITSchedule)
    assert.Equal(t, uint8(3), sdt.Services[0].RunningStatus)  // running
}

func writeServiceDescriptors(w *astikit.BitsWriter, name, provider string) {
    // Спрощено: записати service descriptor
    w.Write(uint8(astits.DescriptorTagService))
    w.Write(uint8(3 + len(name) + len(provider)))  // length
    w.Write(uint8(astits.ServiceTypeDigitalTelevisionService))
    w.Write(uint8(len(provider)))
    w.Write([]byte(provider))
    w.Write(uint8(len(name)))
    w.Write([]byte(name))
}
```

### 🔹 Тест на різні running_status значення

```go
func TestSDT_RunningStatusValues(t *testing.T) {
    testCases := []struct {
        status   uint8
        expected string
    }{
        {0, "undefined"},
        {1, "not running"},
        {2, "pausing"},
        {3, "running"},
        {4, "off-air"},
        {5, "reserved(5)"},
        {7, "reserved(7)"},
    }
    
    for _, tc := range testCases {
        t.Run(fmt.Sprintf("status_%d", tc.status), func(t *testing.T) {
            result := runningStatusToString(tc.status)
            assert.Equal(t, tc.expected, result)
        })
    }
}
```

### 🔹 Тест на round-trip SDT секції

```go
func TestSDTSection_RoundTrip(t *testing.T) {
    original := &astits.SDTData{
        OriginalNetworkID: 123,
        TransportStreamID: 456,
        Services: []*astits.SDTDataService{
            {
                ServiceID:              10,
                HasEITSchedule:         true,
                HasEITPresentFollowing: false,
                HasFreeCSAMode:         true,
                RunningStatus:          3,
                Descriptors: []*astits.Descriptor{
                    {
                        Tag: astits.DescriptorTagService,
                        Service: &astits.DescriptorService{
                            Name:     []byte("Test Channel"),
                            Provider: []byte("Test Provider"),
                            Type:     astits.ServiceTypeDigitalTelevisionService,
                        },
                    },
                },
            },
        },
    }
    
    // Серіалізувати (спрощено — тільки payload)
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    w.Write(original.OriginalNetworkID)
    w.Write(uint8(0))  // reserved
    
    for _, svc := range original.Services {
        w.Write(svc.ServiceID)
        // Flags...
        writeDescriptors(w, svc.Descriptors)
    }
    
    // Парсити назад
    parsed, err := parseSDTSection(astikit.NewBytesIterator(buf.Bytes()), buf.Len(), original.TransportStreamID)
    assert.NoError(t, err)
    
    // Порівняти ключові поля
    assert.Equal(t, original.OriginalNetworkID, parsed.OriginalNetworkID)
    assert.Len(t, parsed.Services, 1)
    assert.Equal(t, original.Services[0].ServiceID, parsed.Services[0].ServiceID)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання прапорців | `HasEITSchedule` не співпадає з очікуваним | Перевірити порядок біт у специфікації: `b&0x02` для EIT_schedule, `b&0x01` для EIT_present_following |
| Дескриптори не парсяться | `svc.Descriptors` порожній | Перевірити, що `parseDescriptors` викликається з правильним `offsetEnd`; перевірити довжину дескрипторів |
| `running_status` некоректний | Значення 5-7 інтерпретуються як помилка | Додати fallback: `default: return fmt.Sprintf("reserved(%d)", status)` |
| SDT не надходить у потоці | `NextData()` ніколи не повертає `*DemuxerData` з `SDT` | Це нормально: SDT передається періодично (раз на ~10 сек); реалізувати кешування останньої валідної SDT |
| Multiple SDT sections (actual/other) | Плутанина між table_id 0x42 та 0x46 | Фільтрувати за `table_id`: 0x42 = actual TS, 0x46 = other TS; обробляти тільки actual для вашого потоку |

### Приклад кешування останньої валідної SDT:

```go
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

## 📦 Швидкий референс для вашого коду

```go
// 1. Парсинг SDT з вхідного потоку:
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
        if svc.RunningStatus == 3 {  // running
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
        if svc.RunningStatus == 3 {
            active++
        }
    }
    metrics.ActiveServiceCount.WithLabelValues(channelID).Set(float64(active))
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