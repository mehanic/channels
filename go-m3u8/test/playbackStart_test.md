# 🔍 Глибокий розбір тестів: `PlaybackStart` для HLS `#EXT-X-START`

Цей файл містить **два юніт-тести** для парсингу та серіалізації тега `#EXT-X-START` — механізму вказівки **початкової точки відтворення** у HLS-плейлистах. Розберемо архітектурно та детально.

---

## 📦 Що таке `#EXT-X-START` і навіщо він потрібен?

### Контекст: керування точкою старту відтворення
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:1000

#EXT-X-START:TIME-OFFSET=-10.0,PRECISE=YES  ← 🎯 ПОЧАТОК ТУТ

#EXTINF:4.0,
seg1000.ts
#EXTINF:4.0,
seg1001.ts
```

### Призначення атрибутів `PlaybackStart`
| Атрибут | Тип | Обов'язковий? | Призначення |
|---------|-----|---------------|-------------|
| `TIME-OFFSET` | `float64` | ✅ Так | Зміщення часу від початку/кінця плейлиста (секунди) |
| `PRECISE` | `*bool` | ❌ Ні | Чи шукати точний ключовий кадр (`YES`/`NO`) |

### 🎯 Критичні сценарії використання
```
🔴 Live CCTV моніторинг (низька затримка):
#EXT-X-START:TIME-OFFSET=-5.0,PRECISE=NO
→ Клієнт підключається і бачить події останніх 5 секунд
→ PRECISE=NO → швидкий старт без очікування keyframe

🎬 VOD архів (точний початок події):
#EXT-X-START:TIME-OFFSET=125.5,PRECISE=YES
→ Відтворення починається з 2:05.5 хвилини
→ PRECISE=YES → гарантований початок з ключового кадру (без артефактів)

🔄 Перепідключення після розриву:
#EXT-X-START:TIME-OFFSET=0.0
→ Почати з першого доступного сегмента після розриву
→ Корисно після #EXT-X-DISCONTINUITY

📊 Синхронізація з реальним часом:
• У поєднанні з #EXT-X-PROGRAM-DATE-TIME
• Клиєнт розраховує: server_time - program_date_time = затримка
• #EXT-X-START коригує точку старту для мінімізації drift
```

---

## 🔬 Детальний розбір тестів

### Тест 1: `TestPlaybackStart_Parse` — з `PRECISE=YES`

```go
func TestPlaybackStart_Parse(t *testing.T) {
    // 🎯 Вхідний рядок: повний формат з обома атрибутами
    line := `#EXT-X-START:TIME-OFFSET=20.2,PRECISE=YES`
    
    // 🎯 Парсинг через конструктор
    ps, err := m3u8.NewPlaybackStart(line)
    assert.Nil(t, err)
    
    // 🎯 Перевірка обов'язкового TIME-OFFSET (float64)
    assert.Equal(t, 20.2, ps.TimeOffset)  // ✅ Додатне значення = від початку плейлиста
    
    // 🎯 Перевірка опціонального PRECISE (*bool)
    assertNotNilEqual(t, true, ps.Precise)  // ✅ "YES" → true
    
    // 🎯 Кругова перевірка: серіалізація має відтворити оригінал
    assertToString(t, line, ps)  // \n нормалізуються хелпером
}
```

### Тест 2: `TestPlaybackStart_Parse_2` — тільки `TIME-OFFSET` (від'ємний)

```go
func TestPlaybackStart_Parse_2(t *testing.T) {
    // 🎯 Мінімальний валідний формат: тільки TIME-OFFSET
    // 🎯 Від'ємне значення = відлік від КІНЦЯ плейлиста (для live)
    line := `#EXT-X-START:TIME-OFFSET=-12.9`
    
    ps, err := m3u8.NewPlaybackStart(line)
    assert.Nil(t, err)
    
    assert.Equal(t, -12.9, ps.TimeOffset)  // ✅ Від'ємне = "останні 12.9 секунд"
    
    // 🎯 Ключова перевірка: PRECISE = nil, коли атрибут відсутній
    assert.Nil(t, ps.Precise)  // ✅ Опціональний атрибут → nil за замовчуванням
    
    assertToString(t, line, ps)
}
```

### 🎯 Що тестують ці кейси?
| Аспект | Тест 1 (з PRECISE) | Тест 2 (без PRECISE) | Чому це важливо |
|--------|-------------------|---------------------|----------------|
| **TIME-OFFSET парсинг** | ✅ `20.2` → `float64(20.2)` | ✅ `-12.9` → `float64(-12.9)` | Підтримка додатних/від'ємних значень |
| **PRECISE парсинг** | ✅ `"YES"` → `*bool(true)` | ✅ `nil` (відсутній) | Покажчик дозволяє розрізняти "не вказано" vs "false" |
| **Семантика offset** | Додатне = від початку (VOD) | Від'ємне = від кінця (live) | Критично для правильної інтерпретації плеєром |
| **Кругова перевірка** | `Parse → String() == original` | `Parse → String() == original` | Гарантія консистентності парсингу/серіалізації |

---

## 🏗️ Припустима структура `PlaybackStart`

```go
// 🎯 PlaybackStart — реалізує m3u8.Item для поліморфізму
type PlaybackStart struct {
    TimeOffset float64  // ✅ Обов'язковий: зміщення у секундах
    Precise    *bool    // Опціональний прапорець точності
}

// 🎯 Конструктор: парсинг атрибутів
func NewPlaybackStart(text string) (*PlaybackStart, error) {
    attrs := ParseAttributes(text)  // map[string]string
    
    // 🎯 TIME-OFFSET — обов'язковий, парсинг float64
    timeOffsetStr := attrs[TimeOffsetTag]
    if timeOffsetStr == "" {
        return nil, fmt.Errorf("EXT-X-START requires TIME-OFFSET attribute")
    }
    
    timeOffset, err := strconv.ParseFloat(timeOffsetStr, 64)
    if err != nil {
        return nil, fmt.Errorf("invalid TIME-OFFSET value: %w", err)
    }
    
    // 🎯 PRECISE — опціональний, парсинг YES/NO → *bool
    var precise *bool
    if preciseStr, ok := attrs[PreciseTag]; ok {
        switch strings.ToUpper(preciseStr) {
        case "YES":
            b := true
            precise = &b
        case "NO":
            b := false
            precise = &b
        default:
            return nil, fmt.Errorf("invalid PRECISE value: %s", preciseStr)
        }
    }
    
    return &PlaybackStart{
        TimeOffset: timeOffset,
        Precise:    precise,
    }, nil
}

// 🎯 Серіалізація
func (ps *PlaybackStart) String() string {
    var attrs []string
    attrs = append(attrs, fmt.Sprintf("%s=%g", TimeOffsetTag, ps.TimeOffset))
    
    if ps.Precise != nil {
        val := YesValue
        if !*ps.Precise {
            val = NoValue
        }
        attrs = append(attrs, fmt.Sprintf("%s=%s", PreciseTag, val))
    }
    
    return fmt.Sprintf("%s:%s", PlaybackStartTag, strings.Join(attrs, ","))
}
```

---

## ⚠️ Критичний аналіз: потенційні проблеми та покращення

### 1️⃣ Форматування float: `%g` vs `%.1f` vs `%.3f`
```go
// ❌ Потенційна проблема: fmt.Sprintf("%g", 20.2) → "20.2" ✅
// Але: fmt.Sprintf("%g", 20.0) → "20" (без .0) → може бути несумісність

// ✅ Специфікація не вимагає фіксованої точності, але для консистентності:
func (ps *PlaybackStart) String() string {
    // 🎯 Варіант 1: завжди 1 знак після коми (достатньо для секунд)
    offsetStr := fmt.Sprintf("%.1f", ps.TimeOffset)
    
    // 🎯 Варіант 2: динамічна точність (цілі → .0, дробові → до 3 знаків)
    // if ps.TimeOffset == float64(int64(ps.TimeOffset)) {
    //     offsetStr = fmt.Sprintf("%.1f", ps.TimeOffset)
    // } else {
    //     offsetStr = fmt.Sprintf("%.3f", ps.TimeOffset)
    // }
    
    attrs := []string{fmt.Sprintf("%s=%s", TimeOffsetTag, offsetStr)}
    // ...
}
```

### 2️⃣ Відсутність валідації діапазону `TIME-OFFSET`
```go
// ❌ Поточний код приймає будь-яке float64:
// • TIME-OFFSET=999999.0 → нереально велике значення
// • TIME-OFFSET=NaN → невалідне число
// • TIME-OFFSET=Inf → нескінченність

// ✅ Додати валідацію:
func NewPlaybackStart(text string) (*PlaybackStart, error) {
    // ... парсинг ...
    
    // 🎯 Перевірка на NaN/Inf
    if math.IsNaN(timeOffset) || math.IsInf(timeOffset, 0) {
        return nil, fmt.Errorf("TIME-OFFSET must be a finite number")
    }
    
    // 🎯 Розумні межі для live/VOD
    const (
        MaxLiveOffset  = -300.0  // Максимум 5 хвилин у минуле для live
        MaxVODOffset   = 86400.0 // Максимум 24 години для VOD
    )
    
    if timeOffset < MaxLiveOffset {
        return nil, fmt.Errorf("TIME-OFFSET too negative for live: %f", timeOffset)
    }
    if timeOffset > MaxVODOffset {
        return nil, fmt.Errorf("TIME-OFFSET too large for VOD: %f", timeOffset)
    }
    
    return &PlaybackStart{TimeOffset: timeOffset, Precise: precise}, nil
}
```

### 3️⃣ Семантика `PRECISE=nil` vs `PRECISE=false`
```go
// ✅ Специфікація: якщо PRECISE відсутній, плеєр вирішує сам (зазвичай = NO)
// ❌ Але: nil ≠ false у коді → може призвести до різної поведінки

// 🎯 Приклад:
ps1 := &PlaybackStart{TimeOffset: -10.0, Precise: nil}
ps2 := &PlaybackStart{TimeOffset: -10.0, Precise: pointer.ToBool(false)}

// Серіалізація:
// ps1 → "#EXT-X-START:TIME-OFFSET=-10" (без PRECISE)
// ps2 → "#EXT-X-START:TIME-OFFSET=-10,PRECISE=NO" (явний NO)

// ✅ Це коректно за специфікацією, але варто документувати:
// • nil = "не вказано" → плеєр обирає дефолт
// • &false = "явно заборонено" → плеєр не чекає keyframe
```

### 4️⃣ Назви тестів: нумерація замість опису
```go
// ❌ Поточні назви:
TestPlaybackStart_Parse      // Що саме тестується?
TestPlaybackStart_Parse_2    // Чим відрізняється?

// ✅ Рекомендовані описові назви:
func TestPlaybackStart_Parse_WithPrecise(t *testing.T)        // Тест 1
func TestPlaybackStart_Parse_WithoutPrecise_NegativeOffset(t *testing.T)  // Тест 2

// ✅ Або використання subtests:
func TestPlaybackStart(t *testing.T) {
    t.Run("Parse/WithPrecise_PositiveOffset", func(t *testing.T) { ... })
    t.Run("Parse/WithoutPrecise_NegativeOffset", func(t *testing.T) { ... })
    t.Run("Parse/Invalid/NaN", func(t *testing.T) { ... })
    t.Run("Parse/Invalid/MissingTimeOffset", func(t *testing.T) { ... })
}
```

### 5️⃣ Відсутність тестів на помилки парсингу
```go
// ✅ Додати перевірки невалідного вводу:
func TestPlaybackStart_Parse_Invalid(t *testing.T) {
    cases := []struct{
        name  string
        input string
        wantErr bool
    }{
        {"missing_time_offset", `#EXT-X-START:PRECISE=YES`, true},
        {"invalid_float", `#EXT-X-START:TIME-OFFSET=abc`, true},
        {"invalid_precise", `#EXT-X-START:TIME-OFFSET=10.0,PRECISE=MAYBE`, true},
        {"nan_value", `#EXT-X-START:TIME-OFFSET=NaN`, true},
        {"valid_negative", `#EXT-X-START:TIME-OFFSET=-5.5`, false},
        {"valid_zero", `#EXT-X-START:TIME-OFFSET=0.0,PRECISE=NO`, false},
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            ps, err := m3u8.NewPlaybackStart(tc.input)
            if tc.wantErr {
                assert.Error(t, err)
                assert.Nil(t, ps)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, ps)
            }
        })
    }
}
```

### 6️⃣ Відсутність інтеграційного тесту з плейлистом
```go
// ✅ Додати тест, що показує використання у реальному плейлисті:
func TestPlaybackStart_InMediaPlaylist(t *testing.T) {
    pl := m3u8.NewPlaylist()
    pl.Target = 4
    pl.Sequence = 1000
    pl.Live = true  // ✅ Live-плейлист
    
    // 🎯 #EXT-X-START має з'являтися після заголовків, перед сегментами
    start, _ := m3u8.NewPlaybackStart(`#EXT-X-START:TIME-OFFSET=-10.0,PRECISE=NO`)
    pl.AppendItem(start)
    
    pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg1000.ts"})
    
    output, err := m3u8.Write(pl)
    assert.NoError(t, err)
    
    // 🎯 Перевірка порядку: заголовки → START → сегменти
    lines := strings.Split(strings.TrimSpace(output), "\n")
    targetIdx := indexOf(lines, "#EXT-X-TARGETDURATION")
    startIdx := indexOf(lines, "#EXT-X-START")
    firstSegIdx := indexOf(lines, "seg1000.ts")
    
    assert.Less(t, targetIdx, startIdx, "START should come after headers")
    assert.Less(t, startIdx, firstSegIdx, "START should come before segments")
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **live-ковзним вікном** та **синхронізацією часу**:

### 🎯 Сценарій: low-latency старт для CCTV моніторингу
```go
// У generateMediaPlaylist для live-каналу:
func (sf *SegmentFinalizer) generateLivePlaylist() string {
    pl := m3u8.NewPlaylist()
    pl.Target = 4
    pl.Live = true
    pl.Sequence = sf.currentSequence
    
    // 🎯 Ключове: починати з останніх 5-10 секунд для мінімальної затримки
    playbackStart := &m3u8.PlaybackStart{
        TimeOffset: -8.0,        // Останні 8 секунд = ~2 сегменти по 4с
        Precise:    pointer.ToBool(false),  // Швидкий старт без очікування keyframe
    }
    pl.AppendItem(playbackStart)
    
    // 🎯 Додавання сегментів (ковзне вікно)
    for _, seg := range sf.activeSegments {
        pl.AppendItem(&m3u8.SegmentItem{
            Duration: seg.Duration,
            Segment:  seg.URI,
        })
    }
    
    content, _ := m3u8.Write(pl)
    return content
}
// → Клієнти підключаються і миттєво бачать "зараз", а не початок вікна
```

### 🎯 Сценарій: корекція drift через `TIME-OFFSET` динамічно
```go
// У VideoManifestProxy для синхронізації з серверним часом:
func (p *VideoManifestProxy) calculateStartTimeOffset() float64 {
    // 🎯 Розрахунок: скільки секунд від кінця плейлиста має бачити клієнт
    // Мета: мінімізувати затримку, але уникнути "порожнього" плейлиста
    
    windowDuration := p.playlist.Duration()  // Загальна тривалість доступних сегментів
    targetLatency := 5.0  // Цільова затримка: 5 секунд
    
    // 🎯 Якщо вікно коротше за цільову затримку → показувати все
    if windowDuration < targetLatency {
        return 0.0  // Почати з початку доступного вікна
    }
    
    // 🎯 Інакше: відлік від кінця
    return -targetLatency  // "-5.0" = останні 5 секунд
}

// Використання при генерації плейлиста:
offset := p.calculateStartTimeOffset()
precise := pointer.ToBool(false)  // Завжди швидкий старт для live

start := &m3u8.PlaybackStart{
    TimeOffset: offset,
    Precise:    precise,
}
p.playlist.AppendItem(start)
```

### 🎯 Сценарій: VOD архів з точним початком події
```go
// У archiveGenerator при створенні VOD-плейлиста за часом:
func (ag *ArchiveGenerator) generateVODPlaylist(channelID string, startTime time.Time, duration time.Duration) (*m3u8.Playlist, error) {
    segments, err := ag.fetchSegmentsByTime(channelID, startTime, duration)
    if err != nil {
        return nil, err
    }
    
    pl := m3u8.NewPlaylist()
    pl.Target = 4
    pl.Live = false  // ✅ VOD, не live
    pl.Type = pointer.ToString("VOD")
    
    // 🎯 Почати з першого сегмента події (точний старт)
    playbackStart := &m3u8.PlaybackStart{
        TimeOffset: 0.0,         // Початок плейлиста
        Precise:    pointer.ToBool(true),  // ✅ Чекати на keyframe для чистої картинки
    }
    pl.AppendItem(playbackStart)
    
    // 🎯 Додавання сегментів події
    for _, seg := range segments {
        pl.AppendItem(&m3u8.SegmentItem{
            Duration:        seg.Duration,
            Segment:         seg.URI,
            ProgramDateTime: &m3u8.TimeItem{Time: seg.StartTime},
        })
    }
    
    // 🎯 Для VOD: додати ENDLIST
    // (це робиться у m3u8.Write() автоматично, якщо !pl.Live)
    
    return pl, nil
}
```

### 🎯 Сценарій: валідація `PlaybackStart` перед додаванням у плейлист
```go
// У segmentFinalizer для забезпечення валідності:
func (sf *SegmentFinalizer) validatePlaybackStart(ps *m3u8.PlaybackStart) error {
    // ✅ TIME-OFFSET має бути скінченним числом
    if math.IsNaN(ps.TimeOffset) || math.IsInf(ps.TimeOffset, 0) {
        return fmt.Errorf("TIME-OFFSET must be finite, got %f", ps.TimeOffset)
    }
    
    // ✅ Розумні межі залежно від режиму
    if sf.isLive {
        if ps.TimeOffset > 0 {
            return fmt.Errorf("positive TIME-OFFSET not recommended for live playlists")
        }
        if ps.TimeOffset < -300.0 {  // Максимум 5 хвилин у минуле
            return fmt.Errorf("TIME-OFFSET too negative for live: %f", ps.TimeOffset)
        }
    } else {
        // VOD: може бути додатним, але не більше тривалості плейлиста
        if ps.TimeOffset > sf.playlist.Duration() {
            return fmt.Errorf("TIME-OFFSET exceeds playlist duration")
        }
    }
    
    // ✅ PRECISE: попередження, якщо YES для live (може збільшити затримку)
    if sf.isLive && ps.Precise != nil && *ps.Precise {
        sf.logger.Warn("PRECISE=YES may increase startup latency for live stream",
            "channel", sf.channelID)
        // Не блокуємо, але логуємо для моніторингу
    }
    
    return nil
}
```

---

## 🧪 Приклад: розширений набір тестів для `PlaybackStart`

```go
// ✅ Повний набір тестів з subtests та валідацією:
func TestPlaybackStart(t *testing.T) {
    t.Parallel()
    
    t.Run("Parse/WithPrecise_PositiveOffset", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-START:TIME-OFFSET=20.2,PRECISE=YES`
        ps, err := m3u8.NewPlaybackStart(line)
        
        assert.NoError(t, err)
        assert.Equal(t, 20.2, ps.TimeOffset)
        assertNotNilEqual(t, true, ps.Precise)
        assertToString(t, line, ps)
    })
    
    t.Run("Parse/WithoutPrecise_NegativeOffset", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-START:TIME-OFFSET=-12.9`
        ps, err := m3u8.NewPlaybackStart(line)
        
        assert.NoError(t, err)
        assert.Equal(t, -12.9, ps.TimeOffset)
        assert.Nil(t, ps.Precise)  // ✅ Опціональний = nil
        assertToString(t, line, ps)
    })
    
    t.Run("Parse/ZeroOffset", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-START:TIME-OFFSET=0.0,PRECISE=NO`
        ps, err := m3u8.NewPlaybackStart(line)
        
        assert.NoError(t, err)
        assert.Equal(t, 0.0, ps.TimeOffset)
        assertNotNilEqual(t, false, ps.Precise)
    })
    
    t.Run("Parse/Invalid/MissingTimeOffset", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-START:PRECISE=YES`  // ❌ Без TIME-OFFSET
        _, err := m3u8.NewPlaybackStart(line)
        assert.Error(t, err, "TIME-OFFSET is required")
    })
    
    t.Run("Parse/Invalid/NonNumeric", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-START:TIME-OFFSET=abc`
        _, err := m3u8.NewPlaybackStart(line)
        assert.Error(t, err, "TIME-OFFSET must be a number")
    })
    
    t.Run("Parse/Invalid/NaN", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-START:TIME-OFFSET=NaN`
        _, err := m3u8.NewPlaybackStart(line)
        assert.Error(t, err, "TIME-OFFSET must be finite")
    })
    
    t.Run("Parse/Invalid/PreciseValue", func(t *testing.T) {
        t.Parallel()
        line := `#EXT-X-START:TIME-OFFSET=10.0,PRECISE=MAYBE`
        _, err := m3u8.NewPlaybackStart(line)
        assert.Error(t, err, "PRECISE must be YES or NO")
    })
    
    t.Run("Serialize/FloatFormatting", func(t *testing.T) {
        t.Parallel()
        // 🎯 Перевірка, що дробові числа форматуються коректно
        cases := []struct{
            offset   float64
            expected string
        }{
            {20.0, "20"},      // %g видаляє зайві нулі
            {20.2, "20.2"},
            {-12.9, "-12.9"},
            {0.0, "0"},
        }
        for _, tc := range cases {
            t.Run(fmt.Sprintf("%.1f", tc.offset), func(t *testing.T) {
                ps := &m3u8.PlaybackStart{TimeOffset: tc.offset}
                output := ps.String()
                assert.Contains(t, output, fmt.Sprintf("TIME-OFFSET=%s", tc.expected))
            })
        }
    })
    
    t.Run("Integration/InLivePlaylist", func(t *testing.T) {
        t.Parallel()
        pl := m3u8.NewPlaylist()
        pl.Target = 4
        pl.Live = true
        
        start, _ := m3u8.NewPlaybackStart(`#EXT-X-START:TIME-OFFSET=-5.0,PRECISE=NO`)
        pl.AppendItem(start)
        pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg.ts"})
        
        output, err := m3u8.Write(pl)
        assert.NoError(t, err)
        
        // 🎯 Перевірка наявності та порядку
        assert.Contains(t, output, "#EXT-X-START")
        assert.Contains(t, output, "TIME-OFFSET=-5")
        assert.Contains(t, output, "PRECISE=NO")
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до `#EXT-X-START`

```
✅ #EXT-X-START може з'являтися ТІЛЬКИ ОДИН раз у плейлисті
✅ Має розташовуватися ПІСЛЯ заголовків, але ПЕРЕД першим #EXTINF
✅ TIME-OFFSET — обов'язковий, додатне число з плаваючою комою:
   • Додатне значення: відлік від ПОЧАТКУ плейлиста (VOD)
   • Від'ємне значення: відлік від КІНЦЯ плейлиста (live)
   • Нуль: почати з першого доступного сегмента
✅ PRECISE — опціональний, значення: "YES" або "NO":
   • YES: плеєр має знайти найближчий ключовий кадр до/після TIME-OFFSET
   • NO (або відсутній): плеєр може почати з будь-якого фрейму (швидше)
✅ Для live-плейлистів:
   • Рекомендується від'ємний TIME-OFFSET (напр. -5.0 до -30.0)
   • PRECISE=NO для мінімальної затримки старту
✅ Для VOD-плейлистів:
   • Рекомендується додатний або нульовий TIME-OFFSET
   • PRECISE=YES для чистої картинки без артефактів декодування
✅ Клієнти МАЮТЬ підтримувати обробку #EXT-X-START для коректного старту
✅ Клієнти МОЖУТЬ ігнорувати #EXT-X-START, якщо не підтримують функцію
```

---

## 🎯 Висновок

Ці тести — **солідна основа** для валідації `PlaybackStart`:

✅ Покриття обох режимів: з `PRECISE` та без  
✅ Перевірка додатних/від'ємних значень `TIME-OFFSET`  
✅ Кругова перевірка серіалізації

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати валідацію `TIME-OFFSET` на `NaN`/`Inf` та розумні межі
2. ✅ Додати тести на невалідний ввід (відсутній TIME-OFFSET, нечислові значення)
3. ✅ Документувати поведінку `PRECISE=nil` vs `PRECISE=false`
4. ✅ Додати інтеграційний тест: порядок тегів у плейлисті
5. ✅ Перейменувати тести за описовим патерном або використати subtests

**Приклад оптимізації для low-latency CCTV**:
```go
// Для мінімальної затримки старту моніторингу:
playbackStart := &m3u8.PlaybackStart{
    TimeOffset: -6.0,              // Останні 6 секунд = 1.5 сегменти по 4с
    Precise:    pointer.ToBool(false),  // ✅ Не чекати keyframe → миттєвий старт
}
// → Клієнт підключається і бачить події "зараз", а не 10-20 секунд тому
// → Ідеально для охорони, моніторингу трафіку, екстрених ситуацій
```

**Комбінація з іншими тегами для максимальної синхронізації**:
```m3u8
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:1500
#EXT-X-PROGRAM-DATE-TIME:2024-01-15T14:30:00Z  ← Абсолютний час
#EXT-X-START:TIME-OFFSET=-5.0,PRECISE=NO         ← Почати з "зараз"

#EXTINF:4.0,
seg1500.ts  ← Клієнт розраховує: server_time - 2024-01-15T14:30:00Z = затримка
```

Потрібно допомогти з:
- 🧠 Реалізацією динамічного розрахунку `TIME-OFFSET` на основі мережевих метрик?
- 🔗 Інтеграцією `#EXT-X-START` з вашою математичною синхронізацією часу (`VideoManifestProxy`)?
- 🧪 Написанням бенчмарків для оцінки впливу `PRECISE=YES/NO` на час старту відтворення?

Чекаю на ваші питання! 🛠️⏱️🎬