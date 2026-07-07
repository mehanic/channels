# 🔍 Глибокий розбір тестів: `MapItem` для HLS `#EXT-X-MAP`

Цей файл містить **два юніт-тести** для парсингу та серіалізації тега `#EXT-X-MAP` — критичного елемента HLS Media Playlist для підтримки **Fragmented MP4 (fMP4/CMAF)** форматів. Розберемо архітектурно та детально.

---

## 📦 Що таке `#EXT-X-MAP` і навіщо він потрібен?

### Контекст: fMP4 vs традиційний TS
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4

#EXT-X-MAP:URI="init.mp4"  ← 🗺️ ІНІЦІАЛІЗАЦІЙНИЙ ФАЙЛ
#EXTINF:4.0,
seg1.m4s  ← Медіа-сегмент (без moov box)
#EXTINF:4.0,
seg2.m4s
```

### Призначення тега
| Аспект | Пояснення |
|--------|-----------|
| **Формат** | `#EXT-X-MAP:URI="..."[,BYTERANGE="N[@O]"]` |
| **Для fMP4** | Вказує на файл ініціалізації з метаданими: кодеки, timescale, track IDs (`moov` box) |
| **BYTERANGE** | Опціональний діапазон байтів, якщо init-дані в середині великого файлу |
| **Обов'язковість** | ✅ Обов'язковий для fMP4 плейлистів перед першим сегментом |

### 🎯 Чому це критично для вашого проекту?
```
📹 Ваш CCTV HLS Processor використовує fMP4:
• WebSocket приймає бінарні fMP4-фрагменти
• segmentAssembler формує сегменти без moov
• #EXT-X-MAP вказує клієнту, де взяти метадані для декодування

🔄 Без #EXT-X-MAP:
• Плеєр не знає кодеки/параметри → помилка відтворення
• Кожен сегмент мав би містити moov → +20-30% overhead
• Неможливе ефективне кешування init-файлу

✅ З #EXT-X-MAP:
• Клієнт завантажує init.mp4 один раз → кешує
• Кожен сегмент = тільки медіа-дані (економія трафіку)
• Швидший старт відтворення (parallel fetch init + first segment)
```

---

## 🔬 Детальний розбір тестів

### Тест 1: `TestMapItem_Parse` — з `BYTERANGE`

```go
func TestMapItem_Parse(t *testing.T) {
    // 🎯 Вхідний рядок: URI + BYTERANGE у форматі "length@offset"
    line := `#EXT-X-MAP:URI="frelo/prog_index.m3u8",BYTERANGE="3500@300"`
    
    // 🎯 Парсинг через конструктор
    mi, err := m3u8.NewMapItem(line)
    assert.Nil(t, err)
    
    // 🎯 Перевірка обов'язкового URI
    assert.Equal(t, "frelo/prog_index.m3u8", mi.URI)
    
    // 🎯 Перевірка опціонального ByteRange (композиція)
    assert.NotNil(t, mi.ByteRange)  // ✅ Не nil, бо BYTERANGE був у вхідному рядку
    assertNotNilEqual(t, 3500, mi.ByteRange.Length)  // *int → 3500
    assertNotNilEqual(t, 300, mi.ByteRange.Start)    // *int → 300
    
    // 🎯 Кругова перевірка: серіалізація має відтворити оригінал
    assertToString(t, line, mi)  // \n нормалізуються хелпером
}
```

### Тест 2: `TestMapItem_Parse_2` — тільки `URI` (без `BYTERANGE`)

```go
func TestMapItem_Parse_2(t *testing.T) {
    // 🎯 Мінімальний валідний формат: тільки URI
    line := `#EXT-X-MAP:URI="frelo/prog_index.m3u8"`
    
    mi, err := m3u8.NewMapItem(line)
    assert.Nil(t, err)
    
    assert.Equal(t, "frelo/prog_index.m3u8", mi.URI)
    
    // 🎯 Ключова перевірка: ByteRange = nil, коли атрибут відсутній
    assert.Nil(t, mi.ByteRange)  // ✅ Опціональний атрибут → nil за замовчуванням
    
    assertToString(t, line, mi)
}
```

### 🎯 Що тестують ці кейси?
| Аспект | Тест 1 (з BYTERANGE) | Тест 2 (без BYTERANGE) | Чому це важливо |
|--------|---------------------|----------------------|----------------|
| **URI парсинг** | ✅ `"frelo/..."` | ✅ `"frelo/..."` | Обов'язкове поле, основа функціоналу |
| **ByteRange парсинг** | ✅ `3500@300` → `Length=3500, Start=300` | ✅ `nil` | Опціональне поле має коректно обробляти відсутність |
| **Типи даних** | `*int` для Length/Start | `*int` = nil | Покажчики дозволяють розрізняти "не вказано" vs "0" |
| **Кругова перевірка** | `Parse → String() == original` | `Parse → String() == original` | Гарантія консистентності парсингу/серіалізації |
| **Композиція** | `mi.ByteRange` — окремий тип | `mi.ByteRange` — nil | Архітектурний патерн: делегування логіки ByteRange |

---

## 🏗️ Припустима структура `MapItem`

```go
// 🎯 MapItem — обгортка для поліморфізму (реалізує m3u8.Item)
type MapItem struct {
    URI       string      // ✅ Обов'язковий: посилання на init-файл
    ByteRange *ByteRange  // Опціональний діапазон байтів (композиція)
}

// 🎯 ByteRange — спільний тип для #EXT-X-MAP та #EXT-X-BYTERANGE
type ByteRange struct {
    Length *int  // Довжина у байтах (обов'язкова, якщо BYTERANGE вказано)
    Start  *int  // Зміщення від початку файлу (опціональне, за замовчуванням 0)
}
```

### 🎯 Чому композиція `MapItem → ByteRange`?
```go
// ✅ Переваги патерну:
// • Єдине джерело правди для логіки BYTERANGE
// • Повторне використання: ByteRange використовується і в #EXT-X-BYTERANGE для сегментів
// • Чистіший код: MapItem відповідає за семантику "init-файл", ByteRange — за "діапазон байтів"

// 🔄 Приклад використання у парсері:
func NewMapItem(text string) (*MapItem, error) {
    attrs := ParseAttributes(text)  // map[string]string
    
    // 🎯 URI — обов'язковий, але не валідується тут ⚠️
    uri := attrs[URITag]
    
    // 🎯 BYTERANGE — опціональний, делегуємо парсинг до ByteRange
    var byteRange *ByteRange
    if brStr, ok := attrs[ByteRangeTag]; ok {
        var err error
        byteRange, err = NewByteRange(brStr)  // "3500@300" → *ByteRange
        if err != nil {
            return nil, fmt.Errorf("invalid BYTERANGE: %w", err)
        }
    }
    
    return &MapItem{
        URI:       uri,
        ByteRange: byteRange,
    }, nil
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Відсутність валідації обов'язкового `URI`
```go
// ❌ Поточний код не перевіряє, що URI не порожній:
func NewMapItem(text string) (*MapItem, error) {
    attrs := ParseAttributes(text)
    // ❌ Якщо attrs[URITag] == "" → mi.URI = "" → невалідний об'єкт!
    return &MapItem{URI: attrs[URITag], ...}, nil
}

// ✅ Додати валідацію:
func NewMapItem(text string) (*MapItem, error) {
    attrs := ParseAttributes(text)
    
    uri := attrs[URITag]
    if uri == "" {
        return nil, fmt.Errorf("EXT-X-MAP requires URI attribute")
    }
    
    // ✅ Опціональна валідація формату URI (відносний/абсолютний)
    if !isValidURI(uri) {
        return nil, fmt.Errorf("invalid URI format: %s", uri)
    }
    
    // ... парсинг ByteRange ...
}

func isValidURI(s string) bool {
    // Проста перевірка: не порожній, без пробілів, має розширення або шлях
    return s != "" && !strings.Contains(s, " ") && (strings.Contains(s, "/") || strings.Contains(s, "."))
}
```

### 2️⃣ Валідація формату `BYTERANGE`
```go
// ❌ Тест використовує "3500@300" — валідний формат
// ✅ Але не тестуються невалідні варіанти:

func TestMapItem_ByteRange_Invalid(t *testing.T) {
    cases := []struct{
        name  string
        input string
        wantErr bool
    }{
        {"valid", `#EXT-X-MAP:URI="init.mp4",BYTERANGE="3500@300"`, false},
        {"valid_no_offset", `#EXT-X-MAP:URI="init.mp4",BYTERANGE="3500"`, false},
        {"invalid_format", `#EXT-X-MAP:URI="init.mp4",BYTERANGE="3500-300"`, true},  // "-" замість "@"
        {"negative_length", `#EXT-X-MAP:URI="init.mp4",BYTERANGE="-3500@300"`, true},
        {"non_numeric", `#EXT-X-MAP:URI="init.mp4",BYTERANGE="abc@def"`, true},
        {"empty", `#EXT-X-MAP:URI="init.mp4",BYTERANGE=""`, true},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            mi, err := m3u8.NewMapItem(tc.input)
            if tc.wantErr {
                assert.Error(t, err)
                assert.Nil(t, mi)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, mi)
            }
        })
    }
}
```

### 3️⃣ Семантика `ByteRange.Start = nil` vs `Start = 0`
```go
// ✅ Специфікація: якщо offset не вказано, за замовчуванням 0
// ❌ Але: nil ≠ 0 у коді → може призвести до помилок

// 🎯 Приклад проблеми:
br := &ByteRange{Length: pointer.ToInt(3500), Start: nil}
// Серіалізація: "3500" (без @0) → коректно за специфікацією
// Але: якщо клієнт очікує явний @0 → може бути несумісність

// ✅ Рішення: документувати поведінку або нормалізувати при серіалізації:
func (br *ByteRange) String() string {
    if br.Start != nil {
        return fmt.Sprintf("%d@%d", *br.Length, *br.Start)
    }
    // ✅ Явно додавати @0 для однозначності (опціонально)
    // return fmt.Sprintf("%d@0", *br.Length)
    return fmt.Sprintf("%d", *br.Length)  // Поточна поведінка: без @0
}
```

### 4️⃣ Назви тестів: нумерація замість опису
```go
// ❌ Поточні назви:
TestMapItem_Parse      // Що саме тестується?
TestMapItem_Parse_2    // Чим відрізняється від першого?

// ✅ Рекомендовані описові назви:
func TestMapItem_Parse_WithByteRange(t *testing.T)      // Тест 1
func TestMapItem_Parse_WithoutByteRange(t *testing.T)   // Тест 2

// ✅ Або використання subtests:
func TestMapItem(t *testing.T) {
    t.Run("Parse/WithByteRange", func(t *testing.T) { ... })
    t.Run("Parse/WithoutByteRange", func(t *testing.T) { ... })
    t.Run("Parse/Invalid/EmptyURI", func(t *testing.T) { ... })
    t.Run("Parse/Invalid/InvalidByteRange", func(t *testing.T) { ... })
}
```

### 5️⃣ Відсутність інтеграційного тесту з плейлистом
```go
// ✅ Додати тест, що показує використання у реальному плейлисті:
func TestMapItem_InMediaPlaylist(t *testing.T) {
    pl := m3u8.NewPlaylist()
    pl.Target = 4
    pl.Sequence = 1000
    
    // 🎯 Додавання #EXT-X-MAP перед сегментами (критично для fMP4!)
    mapItem, _ := m3u8.NewMapItem(`#EXT-X-MAP:URI="init.mp4"`)
    pl.AppendItem(mapItem)  // ✅ Має бути ПЕРЕД першим сегментом
    
    pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg1000.m4s"})
    pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg1001.m4s"})
    
    output, err := m3u8.Write(pl)
    assert.NoError(t, err)
    
    // 🎯 Перевірка порядку: #EXT-X-MAP має йти перед #EXTINF
    lines := strings.Split(strings.TrimSpace(output), "\n")
    mapIdx := indexOf(lines, "#EXT-X-MAP")
    firstSegIdx := indexOf(lines, "seg1000.m4s")
    
    assert.Less(t, mapIdx, firstSegIdx, "#EXT-X-MAP must precede first segment")
}

func indexOf(lines []string, substr string) int {
    for i, line := range lines {
        if strings.Contains(line, substr) {
            return i
        }
    }
    return -1
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **fMP4 сегментами** та **pipeline обробки**:

### 🎯 Сценарій: генерація `#EXT-X-MAP` у `segmentFinalizer`
```go
// У segmentFinalizer при ініціалізації каналу:
func (sf *SegmentFinalizer) initPlaylist(channelID string) error {
    // 🎯 Генерація init-файлу через FFmpeg (або отримання з вхідного потоку)
    initPath := fmt.Sprintf("/app/channels/%s/init.mp4", channelID)
    if err := sf.generateInitFile(initPath); err != nil {
        return fmt.Errorf("failed to generate init file: %w", err)
    }
    
    // 🎯 Створення MapItem з відносним URI для CDN-сумісності
    mapItem := &m3u8.MapItem{
        URI: fmt.Sprintf("/channels/%s/init.mp4", channelID),
        // ByteRange не вказуємо → завантажувати весь файл
    }
    
    // 🎯 Додавання у плейлист (ПЕРЕД першим сегментом!)
    sf.playlist.AppendItem(mapItem)
    
    sf.logger.Info("initialized playlist with init file", 
        "channel", channelID, "uri", mapItem.URI)
    return nil
}
```

### 🎯 Сценарій: partial fetch init-файлу через `BYTERANGE`
```go
// 🎯 Оптимізація: якщо init-файл великий, але moov box тільки на початку
func (sf *SegmentFinalizer) createMapItemWithByteRange(channelID string, moovSize int64) *m3u8.MapItem {
    // 🎯 moov box зазвичай на початку файлу → завантажуємо тільки перші N байт
    return &m3u8.MapItem{
        URI: fmt.Sprintf("/channels/%s/init.mp4", channelID),
        ByteRange: &m3u8.ByteRange{
            Length: pointer.ToInt(int(moovSize)),  // Напр. 1880 байт
            Start:  pointer.ToInt(0),               // Початок файлу
        },
    }
}

// 🎯 Використання:
mapItem := sf.createMapItemWithByteRange("alarabiya-1", 1880)
sf.playlist.AppendItem(mapItem)

// 📋 Результат у плейлисті:
// #EXT-X-MAP:URI="/channels/alarabiya-1/init.mp4",BYTERANGE="1880@0"
// → Клієнт завантажує тільки 1.8KB замість повного init.mp4 (може бути 100KB+)
```

### 🎯 Сценарій: валідація `MapItem` перед додаванням у плейлист
```go
// У segmentFinalizer для забезпечення валідності:
func (sf *SegmentFinalizer) validateMapItem(mi *m3u8.MapItem) error {
    // ✅ Обов'язковий URI
    if mi.URI == "" {
        return fmt.Errorf("MapItem.URI is required")
    }
    
    // ✅ Перевірка доступності init-файлу (опціонально, для дебагу)
    if sf.checkFileExistence {
        localPath := sf.resolveURI(mi.URI)
        if _, err := os.Stat(localPath); os.IsNotExist(err) {
            sf.logger.Warn("init file not found", "uri", mi.URI, "path", localPath)
            // Не блокуємо, але логуємо для моніторингу
        }
    }
    
    // ✅ Валідація ByteRange (якщо вказано)
    if mi.ByteRange != nil {
        if mi.ByteRange.Length == nil {
            return fmt.Errorf("ByteRange.Length is required when BYTERANGE is specified")
        }
        if *mi.ByteRange.Length <= 0 {
            return fmt.Errorf("ByteRange.Length must be positive, got %d", *mi.ByteRange.Length)
        }
        if mi.ByteRange.Start != nil && *mi.ByteRange.Start < 0 {
            return fmt.Errorf("ByteRange.Start must be non-negative, got %d", *mi.ByteRange.Start)
        }
    }
    
    return nil
}
```

### 🎯 Сценарій: динамічна зміна init-файлу (codec change)
```go
// У segmentAssembler при виявленні зміни кодека:
func (sa *SegmentAssembler) handleCodecChange(newCodec string) error {
    sa.logger.Info("codec change detected", "new_codec", newCodec)
    
    // 🎯 Вставка розриву + нового #EXT-X-MAP
    disc, _ := m3u8.NewDiscontinuityItem()
    sa.playlist.AppendItem(disc)
    
    // 🎯 Новий init-файл для нового кодека
    newInitURI := fmt.Sprintf("/channels/%s/init_%s.mp4", sa.channelID, newCodec)
    mapItem := &m3u8.MapItem{URI: newInitURI}
    sa.playlist.AppendItem(mapItem)
    
    // 🎯 Оновлення лічильника розривів
    sa.discontinuitySequence++
    
    sa.logger.Info("updated init file for codec change", 
        "codec", newCodec, "uri", newInitURI)
    return nil
}
```

---

## 🧪 Приклад: розширений набір тестів для `MapItem`

```go
// ✅ Повний набір тестів з subtests та валідацією:
func TestMapItem(t *testing.T) {
    t.Parallel()
    
    t.Run("Parse/WithByteRange", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-MAP:URI="init.mp4",BYTERANGE="3500@300"`
        mi, err := m3u8.NewMapItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "init.mp4", mi.URI)
        assert.NotNil(t, mi.ByteRange)
        assertNotNilEqual(t, 3500, mi.ByteRange.Length)
        assertNotNilEqual(t, 300, mi.ByteRange.Start)
        assertToString(t, line, mi)
    })
    
    t.Run("Parse/WithoutByteRange", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-MAP:URI="init.mp4"`
        mi, err := m3u8.NewMapItem(line)
        
        assert.NoError(t, err)
        assert.Equal(t, "init.mp4", mi.URI)
        assert.Nil(t, mi.ByteRange)  // ✅ Опціональний атрибут = nil
        assertToString(t, line, mi)
    })
    
    t.Run("Parse/Invalid/EmptyURI", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-MAP:BYTERANGE="1000@0"`  // ❌ Без URI
        _, err := m3u8.NewMapItem(line)
        assert.Error(t, err, "URI is required")
    })
    
    t.Run("Parse/Invalid/InvalidByteRange", func(t *testing.T) {
        t.Parallel()
        cases := []string{
            `#EXT-X-MAP:URI="init.mp4",BYTERANGE="abc@def"`,  // Не числа
            `#EXT-X-MAP:URI="init.mp4",BYTERANGE="-100@0"`,   // Від'ємна довжина
            `#EXT-X-MAP:URI="init.mp4",BYTERANGE="1000"`,     // ✅ Це валідно! (без offset)
        }
        for _, input := range cases[:2] {  // Тільки невалідні
            t.Run(input, func(t *testing.T) {
                _, err := m3u8.NewMapItem(input)
                assert.Error(t, err, "invalid BYTERANGE should fail")
            })
        }
    })
    
    t.Run("Serialize/ByteRangeNil", func(t *testing.T) {
        t.Parallel()
        mi := &m3u8.MapItem{
            URI:       "init.mp4",
            ByteRange: nil,  // ✅ Явно nil
        }
        output := mi.String()
        assert.Equal(t, `#EXT-X-MAP:URI="init.mp4"`, strings.TrimSpace(output))
        assert.NotContains(t, output, "BYTERANGE", "nil ByteRange should not be serialized")
    })
    
    t.Run("Integration/InPlaylist", func(t *testing.T) {
        t.Parallel()
        pl := m3u8.NewPlaylist()
        pl.Target = 4
        
        // 🎯 #EXT-X-MAP має йти ПЕРЕД сегментами
        mapItem, _ := m3u8.NewMapItem(`#EXT-X-MAP:URI="init.mp4"`)
        pl.AppendItem(mapItem)
        pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg1.m4s"})
        
        output, err := m3u8.Write(pl)
        assert.NoError(t, err)
        
        // 🎯 Перевірка порядку
        lines := strings.Split(strings.TrimSpace(output), "\n")
        mapIdx := indexOf(lines, "#EXT-X-MAP")
        segIdx := indexOf(lines, "seg1.m4s")
        assert.Less(t, mapIdx, segIdx)
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до `#EXT-X-MAP`

```
✅ #EXT-X-MAP — ОБОВ'ЯЗКОВИЙ для fMP4/CMAF плейлистів
✅ Має з'являтися ПЕРЕД першим медіа-сегментом у плейлисті
✅ Може з'являтися знову після #EXT-X-DISCONTINUITY (якщо init-файл змінився)

✅ URI — обов'язковий:
   • Абсолютний або відносний URL до init-файлу
   • Файл має містити валідний `moov` box на початку (або за вказаним BYTERANGE)
   • Клієнти кешують init-файл за URI → уникати змін URI без потреби

✅ BYTERANGE — опціональний, формат "N[@O]":
   • N = довжина у байтах (обов'язкова, додатне ціле)
   • O = зміщення від початку файлу (опціональне, за замовчуванням 0)
   • Використовується, якщо init-дані (moov) не на початку файлу
   • Економія трафіку: завантажувати тільки moov, а не весь файл

✅ Семантика для клієнта:
   • Завантажити init-файл (або діапазон) → розпарсити moov → отримати кодеки, timescale, track IDs
   • Використовувати ці метадані для декодування всіх наступних сегментів
   • Кешувати init-файл до зміни #EXT-X-MAP або #EXT-X-DISCONTINUITY

✅ Обмеження:
   • Не змінювати URI init-файлу без #EXT-X-DISCONTINUITY (клієнти кешують за URI)
   • Якщо init-файл змінюється (напр. codec change) → обов'язково #EXT-X-DISCONTINUITY + новий #EXT-X-MAP
   • BYTERANGE має вказувати на валідний діапазон, що містить повний moov box
```

---

## 🎯 Висновок

Ці тести — **солідна основа** для валідації `MapItem`:

✅ Покриття обох форматів: з `BYTERANGE` та без  
✅ Перевірка композиції `MapItem → ByteRange`  
✅ Кругова перевірка серіалізації

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати валідацію обов'язкового `URI` у конструкторі
2. ✅ Додати тести на невалідний `BYTERANGE` (не числа, від'ємні значення)
3. ✅ Документувати поведінку `ByteRange.Start = nil` (за замовчуванням 0)
4. ✅ Додати інтеграційний тест: `#EXT-X-MAP` має йти **перед** першим сегментом
5. ✅ Перейменувати тести за описовим патерном або використати subtests

**Приклад оптимізації для CCTV fMP4-стріму**:
```go
// Для мінімізації затримки старту: partial fetch init-файлу
func (sf *SegmentFinalizer) createOptimizedMapItem(channelID string) (*m3u8.MapItem, error) {
    initPath := fmt.Sprintf("/app/channels/%s/init.mp4", channelID)
    
    // 🎯 Визначити розмір moov box (напр. через ffprobe)
    moovSize, err := sf.getMoovBoxSize(initPath)
    if err != nil {
        return nil, fmt.Errorf("failed to get moov size: %w", err)
    }
    
    // 🎯 Створити MapItem з BYTERANGE для економії трафіку
    return &m3u8.MapItem{
        URI: fmt.Sprintf("/channels/%s/init.mp4", channelID),
        ByteRange: &m3u8.ByteRange{
            Length: pointer.ToInt(int(moovSize)),  // Тільки moov, не весь файл
            Start:  pointer.ToInt(0),               // Початок файлу
        },
    }, nil
}
// → Клієнт завантажує 2KB замість 200KB → швидший старт відтворення
```

Потрібно допомогти з:
- 🔗 Інтеграцією `MapItem` у ваш `segmentFinalizer` з підтримкою dynamic codec changes?
- 🧠 Реалізацією `getMoovBoxSize()` через ffprobe для оптимізації `BYTERANGE`?
- 🧪 Написанням інтеграційних тестів: парсинг плейлиста → відтворення у тестовому плеєрі?

Чекаю на ваші питання! 🛠️🗺️🎬