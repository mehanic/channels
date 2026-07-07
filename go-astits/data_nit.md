# Глибоке роз'яснення: `nit.go` — парсинг NIT (Network Information Table) у astits

Цей файл містить **реалізацію парсингу секції NIT (Network Information Table)** — таблиці службової інформації стандарту DVB, що описує параметри мережі мовлення: ID мережі, транспортні потоки, їхні характеристики та дескриптори. Це важливо для мультисервісних та мультимережевих систем.

---

## 🎯 Навіщо NIT потрібен у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ NIT у контексті HLS-стрімінгу:         │
│                                         │
│ 🔹 Ідентифікація мережі:                │
│   • NetworkID — унікальний ID мережі   │
│   • TransportStreamID — ID потоку      │
│   • OriginalNetworkID — ID джерела     │
│                                         │
│ 🔹 Опис транспортних потоків:           │
│   • Список доступних потоків у мережі  │
│   • Дескриптори для кожного потоку     │
│   • Частоти, модуляції (для DVB-T/S/C) │
│                                         │
│ 🔹 Для CCTV HLS:                        │
│   • Розрізнення джерел сигналу         │
│   • Маршрутизація між мережами         │
│   • EPG інтеграція через мережеві дані │
│   • Моніторинг доступності потоків     │
└─────────────────────────────────────────┘
```

---

## 🔧 Структури даних

### `NITData` — контейнер для всієї таблиці

```go
type NITData struct {
    NetworkDescriptors []*Descriptor              // 🎯 дескриптори мережі (назва, провайдер...)
    NetworkID          uint16                     // 🎯 ID цієї мережі (з PSI заголовка)
    TransportStreams   []*NITDataTransportStream  // 🎯 список транспортних потоків
}
```

### `NITDataTransportStream` — опис одного транспортного потоку

```go
type NITDataTransportStream struct {
    OriginalNetworkID    uint16        // 🎯 ID мережі-джерела (для ідентифікації)
    TransportDescriptors []*Descriptor // 🎯 дескриптори потоку (частота, модуляція...)
    TransportStreamID    uint16        // 🎯 ID цього транспортного потоку
}
```

> 💡 **Важливо**: `OriginalNetworkID` дозволяє відрізняти потоки, отримані з інших мереж (наприклад, через ретрансляцію).

---

## 🔍 Функція `parseNITSection`: покроковий розбір

```go
func parseNITSection(i *astikit.BytesIterator, tableIDExtension uint16) (*NITData, error) {
    // 🔹 1. Ініціалізація з network_id (передається зовні з PSI заголовка)
    d := &NITData{NetworkID: tableIDExtension}
    
    // 🔹 2. Парсинг network descriptors (дескриптори мережі)
    // parseDescriptors читає до кінця, визначеного network_descriptors_length
    if d.NetworkDescriptors, err = parseDescriptors(i); err != nil {
        return nil, fmt.Errorf("astits: parsing descriptors failed: %w", err)
    }
    
    // 🔹 3. Читання transport_stream_loop_length (12 біт)
    // Формат: [4 reserved][12 loop_length] у 2 байтах
    bs, _ := i.NextBytesNoCopy(2)
    transportStreamLoopLength := int(uint16(bs[0]&0xf)<<8 | uint16(bs[1]))
    // bs[0]&0x0f = молодші 4 біти байта 0 (біти 3-0)
    // bs[1] = весь байт 1 (біти 7-0)
    // Разом: 4 + 8 = 12 біт для довжини
    
    // 🔹 4. Цикл по транспортних потоках до кінця циклу
    offsetEnd := i.Offset() + transportStreamLoopLength
    for i.Offset() < offsetEnd {
        ts := &NITDataTransportStream{}
        
        // ── TransportStreamID (2 байти, big-endian) ──
        bs, _ = i.NextBytesNoCopy(2)
        ts.TransportStreamID = uint16(bs[0])<<8 | uint16(bs[1])
        
        // ── OriginalNetworkID (2 байти, big-endian) ──
        bs, _ = i.NextBytesNoCopy(2)
        ts.OriginalNetworkID = uint16(bs[0])<<8 | uint16(bs[1])
        
        // ── Transport descriptors (дескриптори потоку) ──
        if ts.TransportDescriptors, err = parseDescriptors(i); err != nil {
            return nil, fmt.Errorf("astits: parsing descriptors failed: %w", err)
        }
        
        // ── Додати потік у результат ──
        d.TransportStreams = append(d.TransportStreams, ts)
    }
    
    return d, nil
}
```

### 🎯 Ключовий момент: читання 12-бітного `transport_stream_loop_length`

```
Формат: [4 reserved][12 loop_length] розподілені у 2 байтах

Байт 0: [7-4]reserved [3-0]length[11:8]
Байт 1: [7-0]length[7:0]

Приклад: loop_length = 9 = 0b000000001001

Байт 0: 0b00000000 = 0x00
  - reserved = 0b0000 (біти 7-4) ✅
  - length[11:8] = 0b0000 = 0 (біти 3-0)

Байт 1: 0b00001001 = 0x09 = 9
  - length[7:0] = 9

Розрахунок:
  loop_length = (0 << 8) | 9 = 9 ✅
```

> 💡 **Порада**: Завжди тестуйте бітові операції на відомих значеннях, щоб уникнути помилок зсуву.

---

## 🧮 Формат NIT секції у деталях

```
NIT Section Payload (без PSI заголовка та CRC):
┌─────────────────────────────────┐
│ [12] network_descriptors_length│ ← довжина дескрипторів мережі
│ [N]  network_descriptors...    │ ← цикл дескрипторів мережі
├─────────────────────────────────┤
│ [4]  reserved_future_use = 0    │
│ [12] transport_stream_loop_length│ ← загальна довжина циклу транспортних потоків
├─────────────────────────────────┤
│ Transport stream loop (повтор):│
│   [16] transport_stream_id     │ ← ID цього транспортного потоку
│   [16] original_network_id     │ ← ID мережі-джерела
│   [12] transport_descriptors_length│ ← довжина дескрипторів потоку
│   [N]  transport_descriptors...│ ← цикл дескрипторів потоку
└─────────────────────────────────┘

Повна PSI секція (додається на вищому рівні):
[8]  table_id = 0x40 (actual) / 0x41 (other)
[12] section_length
[16] network_id (table_id_extension)
[16] reserved + version + current_next
[8]  section_number = 0
[8]  last_section_number = 0
[... NIT payload ...]
[32] CRC32
```

### Приклад розбору запису транспортного потоку

```
Вхідні байти для TransportStreamID=2, OriginalNetworkID=3:

Байт 0-1: TransportStreamID = 2 = 0x0002
  • bs[0] = 0x00, bs[1] = 0x02

Байт 2-3: OriginalNetworkID = 3 = 0x0003
  • bs[2] = 0x00, bs[3] = 0x03

Байт 4-5: [4 reserved][12 descriptors_loop_length]
  • reserved = 0b0000
  • descriptors_loop_length = 9 (наприклад)
  • bs[4] = 0x00, bs[5] = 0x09

Результат: []byte{0x00, 0x02, 0x00, 0x03, 0x00, 0x09, ...}
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Ідентифікація мережі та потоків

```go
// У VideoManifestProxy — отримання інформації про мережу:
type NetworkInfo struct {
    NetworkID         uint16
    OriginalNetworkID uint16
    TransportStreamID uint16
    HasDescriptors    bool
}

func extractNetworkInfo(nit *astits.NITData) []NetworkInfo {
    var infos []NetworkInfo
    
    // 🔹 Додати інформацію про саму мережу
    infos = append(infos, NetworkInfo{
        NetworkID:      nit.NetworkID,
        HasDescriptors: len(nit.NetworkDescriptors) > 0,
    })
    
    // 🔹 Додати інформацію про кожен транспортний потік
    for _, ts := range nit.TransportStreams {
        infos = append(infos, NetworkInfo{
            NetworkID:         nit.NetworkID,
            OriginalNetworkID: ts.OriginalNetworkID,
            TransportStreamID: ts.TransportStreamID,
            HasDescriptors:    len(ts.TransportDescriptors) > 0,
        })
    }
    
    return infos
}
```

### ✅ 2: Фільтрація потоків за мережею

```go
// У channel-aware архітектурі — обробляти тільки релевантні мережі:
func filterStreamsByNetwork(nit *astits.NITData, expectedNetworkID uint16) []*astits.NITDataTransportStream {
    var filtered []*astits.NITDataTransportStream
    
    for _, ts := range nit.TransportStreams {
        // 🔹 Фільтр за OriginalNetworkID (джерело сигналу)
        if ts.OriginalNetworkID == expectedNetworkID {
            filtered = append(filtered, ts)
        }
        // 🔹 Або фільтр за TransportStreamID (конкретний потік)
        // if ts.TransportStreamID == expectedStreamID { ... }
    }
    
    return filtered
}

// Використання:
relevantStreams := filterStreamsByNetwork(nit, 3)  // тільки потоки з мережі 3
for _, ts := range relevantStreams {
    log.Infof("Processing stream %d from network %d", 
        ts.TransportStreamID, ts.OriginalNetworkID)
}
```

### ✅ 3: Збагачення метаданих через дескриптори

```go
// Витягування назви мережі/потоку з дескрипторів:
func extractNetworkName(descs []*astits.Descriptor) string {
    for _, desc := range descs {
        if desc.NetworkName != nil {
            return string(desc.NetworkName.Name)
        }
        if desc.Service != nil {
            return string(desc.Service.Name)
        }
    }
    return ""
}

// Використання:
networkName := extractNetworkName(nit.NetworkDescriptors)
if networkName != "" {
    log.Infof("Network %d: '%s'", nit.NetworkID, networkName)
}

for _, ts := range nit.TransportStreams {
    streamName := extractNetworkName(ts.TransportDescriptors)
    if streamName != "" {
        log.Infof("  Stream %d: '%s'", ts.TransportStreamID, streamName)
    }
}
```

### ✅ 4: Моніторинг доступності мереж

```go
// monitoring.Monitor — метрики для NIT:
type NITMetrics struct {
    NITParsed             *prometheus.CounterVec  // кількість парсингів NIT
    NetworksDiscovered    *prometheus.CounterVec  // кількість знайдених мереж
    TransportStreamsGauge *prometheus.GaugeVec    // кількість потоків у мережі
    DescriptorCount       *prometheus.CounterVec  // кількість дескрипторів
}

// У обробці NIT:
func monitorNIT(nit *astits.NITData, channelID string, metrics *NITMetrics) {
    metrics.NITParsed.WithLabelValues(channelID).Inc()
    metrics.NetworksDiscovered.WithLabelValues(channelID).Inc()
    metrics.TransportStreamsGauge.WithLabelValues(channelID).Set(
        float64(len(nit.TransportStreams)),
    )
    
    // 🔹 Підрахувати дескриптори
    descCount := len(nit.NetworkDescriptors)
    for _, ts := range nit.TransportStreams {
        descCount += len(ts.TransportDescriptors)
    }
    metrics.DescriptorCount.WithLabelValues(channelID).Add(float64(descCount))
}
```

### ✅ 5: Fallback при відсутності NIT

```go
// Якщо потік не містить NIT — використати дефолтні значення:
func getDefaultNetworkInfo(networkID, transportStreamID uint16) NetworkInfo {
    return NetworkInfo{
        NetworkID:         networkID,
        TransportStreamID: transportStreamID,
        OriginalNetworkID: networkID,  // припускаємо, що джерело = поточна мережа
    }
}

// Інтеграція у обробку:
func safeHandleNIT(data *astits.DemuxerData, channelID string) []NetworkInfo {
    if data.NIT != nil {
        return extractNetworkInfo(data.NIT)
    }
    
    // Fallback: дефолтна інформація
    log.Warnf("Channel %s: no NIT data, using defaults", channelID)
    return []NetworkInfo{getDefaultNetworkInfo(1, 1)}
}
```

### ✅ 6: Кешування останньої валідної NIT

```go
// NIT передається періодично (~хвилини) — кешувати для використання між оновленнями:
type NITCache struct {
    mu         sync.RWMutex
    lastValid  *astits.NITData
    lastUpdate time.Time
    ttl        time.Duration
}

func (c *NITCache) Update(nit *astits.NITData) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.lastValid = nit
    c.lastUpdate = time.Now()
}

func (c *NITCache) Get() (*astits.NITData, bool) {
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
nitCache := &NITCache{ttl: 5 * time.Minute}  // NIT оновлюється рідше

// При отриманні нової NIT:
if data.NIT != nil {
    nitCache.Update(data.NIT)
}

// При потребі метаданих:
if nit, ok := nitCache.Get(); ok {
    networkInfo := extractNetworkInfo(nit)
} else {
    // Fallback на дефолтні значення
    networkInfo := []NetworkInfo{getDefaultNetworkInfo(1, 1)}
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Базовий тест на парсинг

```go
func TestParseNITSection_Basic(t *testing.T) {
    // Підготувати тестові байти: network descriptors + один транспортний потік
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // Network descriptors (порожні)
    w.WriteN(uint16(0), 12)  // network_descriptors_length = 0
    
    // Transport stream loop length = 9 байт (один запис)
    w.WriteN(uint16(9), 12)  // loop_length
    
    // Транспортний потік: TS_ID=2, OrigNet_ID=3
    w.Write(uint16(2))  // TransportStreamID
    w.Write(uint16(3))  // OriginalNetworkID
    w.WriteN(uint16(0), 12)  // transport_descriptors_length = 0
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    nit, err := parseNITSection(iter, uint16(1))  // network_id = 1
    
    assert.NoError(t, err)
    assert.Equal(t, uint16(1), nit.NetworkID)
    assert.Len(t, nit.TransportStreams, 1)
    
    ts := nit.TransportStreams[0]
    assert.Equal(t, uint16(2), ts.TransportStreamID)
    assert.Equal(t, uint16(3), ts.OriginalNetworkID)
}
```

### 🔹 Тест на кілька транспортних потоків

```go
func TestParseNITSection_MultipleTransportStreams(t *testing.T) {
    // Створити NIT з 3 транспортними потоками
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    w.WriteN(uint16(0), 12)  // network_descriptors_length = 0
    w.WriteN(uint16(27), 12)  // loop_length = 27 байт (3 записи × 9 байт)
    
    // Потік 1: TS_ID=100, OrigNet_ID=1
    w.Write(uint16(100))
    w.Write(uint16(1))
    w.WriteN(uint16(0), 12)  // descriptors_length = 0
    
    // Потік 2: TS_ID=101, OrigNet_ID=2
    w.Write(uint16(101))
    w.Write(uint16(2))
    w.WriteN(uint16(0), 12)
    
    // Потік 3: TS_ID=102, OrigNet_ID=3
    w.Write(uint16(102))
    w.Write(uint16(3))
    w.WriteN(uint16(0), 12)
    
    // Парсинг
    iter := astikit.NewBytesIterator(buf.Bytes())
    nit, err := parseNITSection(iter, uint16(1))
    
    assert.NoError(t, err)
    assert.Equal(t, uint16(1), nit.NetworkID)
    assert.Len(t, nit.TransportStreams, 3)
    
    // Перевірити кожен потік
    assert.Equal(t, uint16(100), nit.TransportStreams[0].TransportStreamID)
    assert.Equal(t, uint16(1), nit.TransportStreams[0].OriginalNetworkID)
    
    assert.Equal(t, uint16(101), nit.TransportStreams[1].TransportStreamID)
    assert.Equal(t, uint16(2), nit.TransportStreams[1].OriginalNetworkID)
    
    assert.Equal(t, uint16(102), nit.TransportStreams[2].TransportStreamID)
    assert.Equal(t, uint16(3), nit.TransportStreams[2].OriginalNetworkID)
}
```

### 🔹 Тест на round-trip NIT секції

```go
func TestNITSection_RoundTrip(t *testing.T) {
    original := &astits.NITData{
        NetworkID: 123,
        NetworkDescriptors: []*astits.Descriptor{
            {
                Tag: astits.DescriptorTagNetworkName,
                NetworkName: &astits.DescriptorNetworkName{
                    Name: []byte("Test Network"),
                },
            },
        },
        TransportStreams: []*astits.NITDataTransportStream{
            {
                TransportStreamID: 456,
                OriginalNetworkID: 789,
                TransportDescriptors: []*astits.Descriptor{
                    {
                        Tag: astits.DescriptorTagService,
                        Service: &astits.DescriptorService{
                            Name: []byte("Test Stream"),
                            Type: astits.ServiceTypeDigitalTelevisionService,
                        },
                    },
                },
            },
        },
    }
    
    // Серіалізувати (спрощено — тільки payload)
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    // ... записати NIT payload ...
    
    // Парсити назад
    parsed, err := parseNITSection(astikit.NewBytesIterator(buf.Bytes()), original.NetworkID)
    assert.NoError(t, err)
    
    // Порівняти ключові поля
    assert.Equal(t, original.NetworkID, parsed.NetworkID)
    assert.Len(t, parsed.TransportStreams, 1)
    assert.Equal(t, original.TransportStreams[0].TransportStreamID, 
                 parsed.TransportStreams[0].TransportStreamID)
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Невірне читання 12-бітних довжин | `loopLength` не співпадає з очікуваним | Перевірити бітові маски: `(bs[0]&0x0F)<<8 \| bs[1]` для 12-бітних полів |
| Дескриптори не парсяться | `NetworkDescriptors` порожній | Перевірити, що `parseDescriptors` викликається з правильним `offsetEnd`; перевірити `network_descriptors_length` |
| NIT не надходить у потоці | `NextData()` ніколи не повертає `*DemuxerData` з `NIT` | Це нормально: NIT передається рідко (раз на кілька хвилин); реалізувати кешування останньої валідної NIT |
| Multiple NIT sections (actual/other) | Плутанина між table_id 0x40 та 0x41 | Фільтрувати за `table_id`: 0x40 = actual network (ваша мережа), 0x41 = other network; обробляти тільки actual для вашого потоку |
| `transport_stream_loop_length` неправильний | Цикл зупиняється завчасно або читає зайве | Перевірити розрахунок: `loopLength` = довжина решти даних циклу (включно з дескрипторами, але без заголовка запису) |

### Приклад коректного читання 12-бітного поля:

```go
func read12BitLength(i *astikit.BytesIterator) (uint16, error) {
    bs, err := i.NextBytesNoCopy(2)
    if err != nil { return 0, err }
    
    // Формат: [4 reserved][12 length] у перших двох байтах
    // Байт 0: [7-4]reserved, [3-0]length[11:8]
    // Байт 1: [7-0]length[7:0]
    
    lengthHigh := uint16(bs[0] & 0x0F)  // молодші 4 біти байта 0
    lengthLow := uint16(bs[1])          // весь байт 1
    
    return (lengthHigh << 8) | lengthLow, nil
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Парсинг NIT з вхідного потоку:
func handleNITData(data *astits.DemuxerData, channelID string) error {
    if data.NIT == nil {
        return nil  // не NIT
    }
    
    nit := data.NIT
    log.Infof("Channel %s: NIT with %d transport streams (Net_ID=%d)", 
        channelID, len(nit.TransportStreams), nit.NetworkID)
    
    // Оновити кеш метаданих мережі
    networkInfo := extractNetworkInfo(nit)
    updateNetworkMetadataCache(channelID, networkInfo)
    
    return nil
}

// 2: Витягування назви мережі для відображення:
func getNetworkName(nit *astits.NITData) string {
    for _, desc := range nit.NetworkDescriptors {
        if desc.NetworkName != nil {
            return string(desc.NetworkName.Name)
        }
    }
    return fmt.Sprintf("Network %d", nit.NetworkID)
}

// 3: Фільтрація за мережею-джерелом:
func getStreamsFromOriginalNetwork(nit *astits.NITData, origNetID uint16) []*astits.NITDataTransportStream {
    var filtered []*astits.NITDataTransportStream
    for _, ts := range nit.TransportStreams {
        if ts.OriginalNetworkID == origNetID {
            filtered = append(filtered, ts)
        }
    }
    return filtered
}

// 4: Моніторинг:
func monitorNITHealth(nit *astits.NITData, channelID string, metrics *NITMetrics) {
    if nit == nil {
        metrics.LastNITUpdate.WithLabelValues(channelID).Set(0)
        return
    }
    
    metrics.LastNITUpdate.WithLabelValues(channelID).Set(float64(time.Now().Unix()))
    metrics.TransportStreamCount.WithLabelValues(channelID).Set(float64(len(nit.TransportStreams)))
    
    // 🔹 Підрахувати дескриптори для оцінки складності
    descCount := len(nit.NetworkDescriptors)
    for _, ts := range nit.TransportStreams {
        descCount += len(ts.TransportDescriptors)
    }
    metrics.DescriptorCount.WithLabelValues(channelID).Set(float64(descCount))
}

// 5: Helper для запису network_name descriptor у тестах:
func writeNetworkNameDescriptor(w *astikit.BitsWriter, name string) {
    w.Write(uint8(astits.DescriptorTagNetworkName))
    w.Write(uint8(len(name)))  // length
    w.Write([]byte(name))      // name
}
```

---

## 📊 Матриця полів NIT для вашого пайплайну

```
Поле NIT                   | Тип       | Використання у CCTV HLS
───────────────────────────┼───────────┼─────────────────────────
NetworkID                  | uint16    | ✅ Ідентифікація поточної мережі
OriginalNetworkID          | uint16    | ✅ Розрізнення джерел сигналу
TransportStreamID          | uint16    | ✅ Ідентифікація потоку у мережі
NetworkDescriptors         | []Descriptor| ✅ Назва мережі, провайдер для UI
TransportDescriptors       | []Descriptor| ⚠️ Частота, модуляція (для DVB-T/S/C)
```

---

## 📚 Корисні посилання

- [ETSI EN 300 468: NIT specification (§5.2.2)](https://dvb.org/wp-content/uploads/2019/12/a038_tm1217r37_en300468v1_17_1_-_rev-134_-_si_specification.pdf)
- [DVB Network Information poster](http://seidl.cs.vsb.cz/download/dvb/DVB_Poster.pdf)
- [astits NIT parsing source](https://github.com/asticode/go-astits/blob/master/data.go)

> 💡 **Ключова ідея**: NIT — це "каталог мереж" вашого DVB-сигналу. У вашому CCTV HLS пайплайні це дозволяє:
> - 🌐 Розрізняти джерела сигналу через OriginalNetworkID для мультимережевих систем
> - 📺 Автоматично генерувати назви мереж для інтерфейсу користувача
> - 🔍 Відстежувати доступність транспортних потоків для моніторингу стабільності
> - 🧩 Підтримувати динамічне додавання нових потоків через оновлення NIT

Якщо потрібно — можу допомогти:
- 🔄 Інтегрувати NIT-обробку у ваш `VideoManifestProxy` для динамічного оновлення метаданих мереж
- 🧩 Додати підтримку кешування метаданих мереж з TTL для fallback при втраті NIT
- 🧪 Написати integration-тест для перевірки коректності фільтрації потоків за мережею

🛠️