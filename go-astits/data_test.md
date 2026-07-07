# Глибоке роз'яснення: Тести `parseData` у astits — ядро класифікації та агрегації даних

Цей файл тестує **функцію `parseData`** — центральний компонент демуксера, який визначає тип даних у пакеті (PSI чи PES), агрегує фрагментовані пакети та повертає структуровані `*DemuxerData`.

---

## 🎯 Призначення `parseData`: що вона робить?

```
┌─────────────────────────────────────────┐
│ parseData — класифікатор та агрегатор: │
│                                         │
│ Вхід: []*Packet (зібрані packetPool)   │
│                                         │
│ 🔹 Крок 1: Визначити тип даних         │
│    • isPSIPayload() → PSI таблиці      │
│    • isPESPayload() → PES-потоки       │
│    • Custom parser → кастомна логіка   │
│                                         │
│ 🔹 Крок 2: Агрегувати фрагменти        │
│    • З'єднати payload кількох пакетів  │
│    • Видалити адаптаційні поля         │
│                                         │
│ 🔹 Крок 3: Парсити структуру           │
│    • PSI: parsePSISection()            │
│    • PES: parsePESHeader() + payload   │
│                                         │
│ Вихід: []*DemuxerData (готові дані)    │
└─────────────────────────────────────────┘
```

---

## 🔧 Розбір тесту `TestParseData`

### 📦 Кейс 1: Кастомний парсер (PacketsParser)

```go
// Init
pm := newProgramMap()
ps := []*Packet{}  // порожній набір пакетів

// Кастомний парсер: завжди повертає фіксовані дані
cds := []*DemuxerData{{PID: 1}}
var c = func(ps []*Packet) (o []*DemuxerData, skip bool, err error) {
    o = cds      // 🎯 повертаємо наші дані
    skip = true  // 🎯 пропускаємо стандартну обробку
    return
}

ds, err := parseData(ps, c, pm)
assert.NoError(t, err)
assert.Equal(t, cds, ds)  // ✅ кастомний парсер перевизначив логіку
```

**Коли використовувати кастомний парсер:**
```
✅ Орфан аудіо/відео merge: ручна агрегація за seqNum замість PID
✅ Кастомні дескриптори: парсинг приватних метаданих
✅ Фільтрація на рівні даних: відкидати непотрібні типи
```

---

### 📦 Кейс 2: CAT (Conditional Access Table) — ігнорування

```go
// Do nothing for CAT
ps = []*Packet{{Header: PacketHeader{PID: PIDCAT}}}  // PID=0x0001
ds, err = parseData(ps, nil, pm)
assert.NoError(t, err)
assert.Empty(t, ds)  // ✅ CAT не парситься, повертаємо порожній результат
```

**Чому CAT ігнорується?**
```
• CAT (PID=0x0001) містить інформацію про умовний доступ (шифрування)
• Більшість плеєрів не потребують CAT для відтворення
• Парсинг CAT складний (приватні дескриптори, ECM/EMM)
• astits фокусується на PSI/PES для базового відтворення

Якщо потрібно парсити CAT:
→ Реалізувати кастомний PacketsParser для PIDCAT
```

---

### 📦 Кейс 3: PES (Packetized Elementary Stream)

```go
// PES: два фрагментовані пакети одного потоку
p := pesWithHeaderBytes()  // сирі байти PES з заголовком

ps = []*Packet{
    {
        Header:  PacketHeader{PID: uint16(256)},
        Payload: p[:33],  // перші 33 байти
    },
    {
        Header:  PacketHeader{PID: uint16(256)},
        Payload: p[33:],  // решта байт
    },
}

ds, err = parseData(ps, nil, pm)
assert.NoError(t, err)
assert.Equal(t, []*DemuxerData{
    {
        FirstPacket: &Packet{Header: ps[0].Header, AdaptationField: ps[0].AdaptationField},
        PES:         pesWithHeader(),  // парсена PES структура
        PID:         uint16(256),
    }}, ds)
```

**Логіка агрегації PES:**
```
1. Визначити PES за start code: 0x000001 (24 біти)
2. З'єднати payload всіх пакетів у порядку надходження
3. Парсити PES header: stream_id, flags, PTS/DTS
4. Зберегти payload для подальшої обробки (декодування)

Важливо: PES може бути фрагментований на багато пакетів!
```

---

### 📦 Кейс 4: PSI (Program Specific Information)

```go
// PSI: таблиця на відомому PID (з programMap)
pm.setUnlocked(uint16(256), uint16(1))  // PID 256 → program 1

p = psiBytes()  // сирі байти PSI секції
ps = []*Packet{
    {
        Header:  PacketHeader{PID: uint16(256)},
        Payload: p[:33],
    },
    {
        Header:  PacketHeader{PID: uint16(256)},
        Payload: p[33:],
    },
}

ds, err = parseData(ps, nil, pm)
assert.NoError(t, err)
// Перевіряємо, що результат співпадає з очікуваним
assert.Equal(t, psi.toData(
    &Packet{Header: ps[0].Header, AdaptationField: ps[0].AdaptationField},
    uint16(256),
), ds)
```

**Роль `programMap` у PSI-детекції:**
```
• programMap[PID] = program_number → цей PID містить метадані програми
• parseData перевіряє: isPSIPayload(PID, programMap)
• Якщо true → парсити як PSI секцію (PAT/PMT/SDT/EIT...)

Без programMap: демуксер не знає, що PID 256 = PMT, а не відео!
```

---

## 🔍 Допоміжні функції: `isPSIPayload` та `isPESPayload`

### `TestIsPSIPayload`: детекція PSI за PID

```go
func TestIsPSIPayload(t *testing.T) {
    pm := newProgramMap()
    var pids []int
    
    // Перевірити всі PID 0-255
    for i := 0; i <= 255; i++ {
        if isPSIPayload(uint16(i), pm) {
            pids = append(pids, i)
        }
    }
    
    // Очікувані PSI PID за стандартом:
    assert.Equal(t, []int{0, 16, 17, 18, 19, 20, 30, 31}, pids)
    // 0x00=PAT, 0x10-0x14=ST, 0x1E-0x1F=reserved
    
    // Додати custom PID у programMap → тепер він теж PSI
    pm.setUnlocked(uint16(1), uint16(0))
    assert.True(t, isPSIPayload(uint16(1), pm))  // ✅ тепер PID 1 = PSI
}
```

**Логіка `isPSIPayload`:**
```go
func isPSIPayload(pid uint16, pm *programMap) bool {
    // 🔹 Стандартні PSI PID (ETSI EN 300 468)
    switch pid {
    case PIDPAT:  // 0x0000
        return true
    case PIDCAT:  // 0x0001 — але парситься окремо!
        return false  // ігноруємо
    case 0x0010, 0x0011, 0x0012, 0x0013, 0x0014, 0x001E, 0x001F:
        return true  // інші зарезервовані PSI PID
    }
    
    // 🔹 Custom PSI PID: якщо programMap містить цей PID
    return pm.existsUnlocked(pid)
}
```

> 💡 **Ключова ідея**: `programMap` дозволяє динамічно реєструвати нові PSI PID (напр., при додаванні нових програм у потік).

---

### `TestIsPESPayload`: детекція PES за start code

```go
func TestIsPESPayload(t *testing.T) {
    buf := &bytes.Buffer{}
    w := astikit.NewBitsWriter(astikit.BitsWriterOptions{Writer: buf})
    
    // ❌ Невірний start code: 16 біт замість 24
    w.Write("0000000000000001")  // 0x0001
    assert.False(t, isPESPayload(buf.Bytes()))
    
    // ✅ Вірний start code: 24 біти = 0x000001
    buf.Reset()
    w.Write("000000000000000000000001")  // 0x000001
    assert.True(t, isPESPayload(buf.Bytes()))
}
```

**Логіка `isPESPayload`:**
```go
func isPESPayload(payload []byte) bool {
    // PES start code = 3 байти: 0x00 0x00 0x01
    if len(payload) < 3 {
        return false
    }
    return payload[0] == 0x00 && payload[1] == 0x00 && payload[2] == 0x01
}
```

> ⚠️ **Важливо**: `isPESPayload` перевіряє тільки перші 3 байти. Якщо payload пошкоджений або зміщений — детекція може не спрацювати.

---

## 🧮 Матриця типів даних у `parseData`

```
Тип даних | Визначення                    | Обробка                 | Приклад використання
──────────┼───────────────────────────────┼─────────────────────────┼─────────────────────
PSI       | isPSIPayload(PID, programMap) | parsePSISection()       │ PAT/PMT/EIT/SDT
PES       | isPESPayload(payload)         | parsePESHeader()+data   │ Відео/аудіо потоки
Custom    | optPacketsParser != nil       | кастомна логіка         │ Орфан merge, метадані
CAT       | PID == 0x0001                 │ ігнорується             │ Умовний доступ (шифрування)
Unknown   | не співпадає ні з чим         │ помилка / ігнорування   │ Приватні дані, сміття
```

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

### ✅ 2: Фільтрація PSI за programMap для channel-aware обробки

```go
// У багатоканальному сервері — обробляти тільки релевантні програми:
func createChannelAwareParser(channelID string, expectedPrograms map[uint16]bool) astits.PacketsParser {
    return func(ps []*astits.Packet) ([]*astits.DemuxerData, bool, error) {
        if len(ps) == 0 {
            return nil, false, nil
        }
        
        pid := ps[0].Header.PID
        
        // 🔹 Якщо це PSI — перевірити, чи програма потрібна цьому каналу
        if isPSIPayload(pid, nil) {  // programMap перевіряється всередині parseData
            // Отримати program_number з payload (спрощено)
            programNumber := extractProgramNumberFromPSI(ps)
            if !expectedPrograms[programNumber] {
                // Ця програма не для цього каналу → відкинути
                return nil, true, nil
            }
        }
        
        // Стандартна обробка
        return nil, false, nil
    }
}
```

### ✅ 3: Моніторинг типів даних для відладки

```go
// monitoring.Monitor — метрики для parseData:
type ParseMetrics struct {
    PSIPacketsParsed  *prometheus.CounterVec  // кількість PSI таблиць по типу
    PESPacketsParsed  *prometheus.CounterVec  // кількість PES по PID
    CustomParserHits  *prometheus.CounterVec  // виклики кастомного парсера
    UnknownDataTypes  *prometheus.CounterVec  // нерозпізнані типи
}

// У parseData (модифікована версія):
func parseDataWithMetrics(ps []*astits.Packet, parser astits.PacketsParser, 
                          pm *programMap, channelID string, metrics *ParseMetrics) ([]*astits.DemuxerData, error) {
    
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
    return parseData(ps, nil, pm)  // виклик оригінальної функції
}
```

---

## 🧪 Розширення тестів для ваших потреб

### 🔹 Тест на фрагментацію великого PES

```go
func TestParseData_PESFragmentation(t *testing.T) {
    // Згенерувати PES з 1000 байт даних → має розбитися на 6+ пакетів
    pesData := generatePESWithPayload(1000)  // ваша helper-функція
    
    // Розбити на пакети по 184 байти (188 - 4 заголовка)
    var packets []*astits.Packet
    for i := 0; i < len(pesData); i += 184 {
        end := i + 184
        if end > len(pesData) {
            end = len(pesData)
        }
        packets = append(packets, &astits.Packet{
            Header: astits.PacketHeader{
                PID: 256,
                PayloadUnitStartIndicator: i == 0,  // тільки перший пакет має PUSI
            },
            Payload: pesData[i:end],
        })
    }
    
    // Парсити
    ds, err := parseData(packets, nil, newProgramMap())
    assert.NoError(t, err)
    assert.Len(t, ds, 1)  // один завершений PES
    assert.Equal(t, uint16(256), ds[0].PID)
    assert.NotNil(t, ds[0].PES)
}
```

### 🔹 Тест на динамічне оновлення programMap

```go
func TestParseData_DynamicProgramMap(t *testing.T) {
    pm := newProgramMap()
    
    // Спочатку PID 256 не відомий → не PSI
    assert.False(t, isPSIPayload(256, pm))
    
    // Додати у programMap (напр., після парсингу PAT)
    pm.setUnlocked(256, 1)  // PID 256 = program 1
    
    // Тепер PID 256 розпізнається як PSI
    assert.True(t, isPSIPayload(256, pm))
    
    // Парсинг має працювати з оновленим programMap
    psiBytes := generatePSIBytes()  // ваша helper-функція
    packets := []*astits.Packet{
        {Header: astits.PacketHeader{PID: 256}, Payload: psiBytes},
    }
    
    ds, err := parseData(packets, nil, pm)
    assert.NoError(t, err)
    assert.NotEmpty(t, ds)  // PSI успішно парсено
}
```

### 🔹 Тест на обробку пошкоджених PES start code

```go
func TestIsPESPayload_CorruptedStartCode(t *testing.T) {
    testCases := []struct {
        name     string
        payload  []byte
        expected bool
    }{
        {"Valid", []byte{0x00, 0x00, 0x01, 0xE0}, true},
        {"Corrupted byte 1", []byte{0x01, 0x00, 0x01, 0xE0}, false},
        {"Corrupted byte 2", []byte{0x00, 0x01, 0x01, 0xE0}, false},
        {"Corrupted byte 3", []byte{0x00, 0x00, 0x02, 0xE0}, false},
        {"Too short", []byte{0x00, 0x00}, false},
        {"Empty", []byte{}, false},
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            result := isPESPayload(tc.payload)
            assert.Equal(t, tc.expected, result)
        })
    }
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
| programMap не оновлюється | Нові PSI PID не розпізнаються | Додати логування після `updateData()`; перевірити, що PAT парситься коректно |

### Приклад діагностики фрагментації:

```go
func debugPESFragmentation(packets []*astits.Packet, pid uint16) {
    log.Infof("Debugging PES fragmentation for PID %d:", pid)
    
    for i, pkt := range packets {
        log.Infof("  Packet %d: PUSI=%v, payload_len=%d, first_3_bytes=%X",
            i,
            pkt.Header.PayloadUnitStartIndicator,
            len(pkt.Payload),
            pkt.Payload[:min(3, len(pkt.Payload))],
        )
    }
    
    // Перевірити start code на першому пакеті
    if len(packets) > 0 && len(packets[0].Payload) >= 3 {
        startCode := packets[0].Payload[:3]
        if startCode[0] == 0x00 && startCode[1] == 0x00 && startCode[2] == 0x01 {
            log.Infof("  ✅ Valid PES start code detected")
        } else {
            log.Warnf("  ❌ Invalid PES start code: %X", startCode)
        }
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
// 1. Визначення типу даних:
func classifyPacketData(pkt *astits.Packet, pm *programMap) string {
    if isPSIPayload(pkt.Header.PID, pm) {
        return "PSI"
    }
    if isPESPayload(pkt.Payload) {
        return "PES"
    }
    return "Unknown"
}

// 2. Кастомний парсер для специфічних потреб:
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

// 3. Обробка помилок парсингу:
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

// 4. Моніторинг типів даних:
func trackDataTypes(ds []*astits.DemuxerData, channelID string, metrics *ParseMetrics) {
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
```

---

## 📚 Корисні посилання

- [astits parseData source](https://github.com/asticode/go-astits/blob/master/data.go)
- [MPEG-TS PSI/PES specification](https://www.iso.org/standard/61236.html)
- [ETSI EN 300 468: DVB SI tables](https://www.etsi.org/deliver/etsi_en/300400_300499/300468/)

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