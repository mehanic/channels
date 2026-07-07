# 🧪 `m3u8/parser_test.go`: Комплексне тестування парсингу HLS-плейлистів

Це **всебічний тест-сьют** бібліотеки `github.com/grafov/m3u8`, який перевіряє коректність парсингу **Master** та **Media** плейлистів з підтримкою кастомних тегів, різних форматів часу, шифрування, SCTE-35 сповіщень та інших розширень стандарту HLS.

---

## 🎯 Коротка відповідь

> **Цей тест-сьют — "золотий стандарт" для надійного парсингу M3U8**: він покриває 25+ сценаріїв від базового парсингу до складних випадків з кастомними тегами, автоматичною детекцією типу плейлиста, обробкою помилок та бенчмарками продуктивності — ідеально для валідації вашого CCTV HLS-конвеєра.

---

## 📋 Структура тестів: Огляд за категоріями

### 🔹 Тести Master-плейлистів

| Тест | Перевіряє | Ключові асерції |
|------|-----------|----------------|
| `TestDecodeMasterPlaylist` | Базовий парсинг | `ver=3`, `len(Variants)=5` |
| `TestDecodeMasterPlaylistWithMultipleCodecs` | Кодеки з комами | `Codecs="avc1.42c015,mp4a.40.2"` |
| `TestDecodeMasterPlaylistWithAlternatives*` | Аудіо/субтитри доріжки | `len(Alternatives)=3` для перших 3 варіантів |
| `TestDecodeMasterPlaylistWithClosedCaptionEqNone` | `CLOSED-CAPTIONS=NONE` | `Captions="NONE"` для всіх варіантів |
| `TestDecodeMasterPlaylistWithStreamInfName` | Атрибут `NAME` у `#EXT-X-STREAM-INF` | `variant.Name != ""` |
| `TestDecodeMasterPlaylistWithIFrameStreamInf` | I-frame only варіанти | `Iframe=true`, правильні `Bandwidth`, `Resolution` |
| `TestDecodeMasterPlaylistWithStreamInfAverageBandwidth` | `AVERAGE-BANDWIDTH` | `AverageBandwidth != 0` |
| `TestDecodeMasterPlaylistWithStreamInfFrameRate` | `FRAME-RATE` | `FrameRate != 0` |
| `TestDecodeMasterPlaylistWithIndependentSegments` | `#EXT-X-INDEPENDENT-SEGMENTS` | `p.IndependentSegments() == true` |
| `TestDecodeMasterWithHLSV7` | HLS v7 з HDR/Dolby Vision | 18 варіантів з `VideoRange`, `HDCPLevel`, `Codecs` |

---

### 🔹 Тести Media-плейлистів

| Тест | Перевіряє | Ключові асерції |
|------|-----------|----------------|
| `TestDecodeMediaPlaylist` | Базовий парсинг VoD | `ver=3`, `TargetDuration=12`, `Closed=true`, `Count=522` |
| `TestDecodeMediaPlaylistExtInfNonStrict2` | `#EXTINF` у strict/non-strict режимах | Обробка невалідних значень, тайтлів з комами |
| `TestDecodeMediaPlaylistWithWidevine` | DRM-метадані Widevine | `ver=2`, `TargetDuration=9`, парсинг `#WV-*` тегів |
| `TestDecodeMediaPlaylistByteRange` | `#EXT-X-BYTERANGE` | Правильні `Limit`, `Offset` для часткового читання |
| `TestDecodeMediaPlaylistWithDiscontinuitySeq` | `#EXT-X-DISCONTINUITY-SEQUENCE` | `DiscontinuitySeq != 0`, правильні `SeqId` |
| `TestDecodeMediaPlaylistWithProgramDateTime` | `#EXT-X-PROGRAM-DATE-TIME` | Парсинг `time.Time` у різних форматах |
| `TestDecodeMediaPlaylistStartTime` | `#EXT-X-START:TIME-OFFSET` | `StartTime == 8.0` |
| `TestDecodeMediaPlaylistWithCueOutCueIn` | SCTE-35 сповіщення (без OATCLS) | `CueType=Start/Mid/End`, правильні `Time` |
| `TestMediaPlaylistWithOATCLSSCTE35Tag` | SCTE-35 з синтаксисом OATCLS | `Syntax=SCTE35_OATCLS`, `Cue`, `Elapsed` |
| `TestMediaPlaylistWithSCTE35Tag` | Класичний синтаксис SCTE-35 | `Cue`, `ID`, `Time` парсинг |

---

### 🔹 Тести авто-детекції типу плейлиста

```go
func TestDecodeMasterPlaylistWithAutodetection(t *testing.T) {
    m, listType, err := DecodeFrom(bufio.NewReader(f), false)
    if listType != MASTER { t.Error("...") }
    CheckType(t, m.(*MasterPlaylist))  // 🔹 Перевірка type assertion
}

func TestDecodeMediaPlaylistWithAutodetection(t *testing.T) {
    p, listType, err := DecodeFrom(bufio.NewReader(f), true)
    if listType != MEDIA { t.Error("...") }
    CheckType(t, p.(*MediaPlaylist))
}
```

**🎯 Призначення**: Перевірити, що `DecodeFrom()` коректно визначає тип плейлиста без явного вказівки.

---

### 🔹 Тести кастомних тегів

```go
func TestDecodeMasterPlaylistWithCustomTags(t *testing.T) {
    cases := []struct {
        src            string
        customDecoders []CustomDecoder
        expectedError  error
        expectedPlaylistTags []string
    }{
        // 🔹 Кейс 1: Без кастомних декодерів → теги ігноруються
        {src: "...", customDecoders: nil, expectedPlaylistTags: nil},
        
        // 🔹 Кейс 2: Декодер повертає помилку → помилка парсингу
        {customDecoders: []CustomDecoder{&MockCustomTag{err: errors.New("...")}}, expectedError: ...},
        
        // 🔹 Кейс 3: Успішний парсинг кастомного тегу
        {customDecoders: []CustomDecoder{&MockCustomTag{name: "#CUSTOM-PLAYLIST-TAG:"}}, 
         expectedPlaylistTags: []string{"#CUSTOM-PLAYLIST-TAG:"}},
    }
    // ... виконання тестів
}
```

**🔹 `MockCustomTag` — тестовий декодер:**
```go
type MockCustomTag struct {
    name          string
    err           error
    segment       bool
    encodedString string
}

func (m *MockCustomTag) TagName() string { return m.name }
func (m *MockCustomTag) Decode(line string) (CustomTag, error) { return m, m.err }
func (m *MockCustomTag) SegmentTag() bool { return m.segment }
func (m *MockCustomTag) Encode() *bytes.Buffer { return bytes.NewBufferString(m.encodedString) }
func (m *MockCustomTag) String() string { return m.encodedString }
```

**🎯 Призначення**: Перевірити механізм реєстрації, парсингу та прив'язки кастомних тегів.

---

### 🔹 Тести парсингу часу: `FullTimeParse` vs `StrictTimeParse`

```go
func TestFullTimeParse(t *testing.T) {
    timestamps := []struct{name, value string}{
        {"time_in_utc", "2006-01-02T15:04:05Z"},
        {"time_in_utc_nano", "2006-01-02T15:04:05.123456789Z"},
        {"time_with_positive_zone_and_colon", "2006-01-02T15:04:05+01:00"},
        {"time_with_positive_zone_no_colon", "2006-01-02T15:04:05+0100"},
        {"time_with_positive_zone_2digits", "2006-01-02T15:04:05+01"},
        // ... негативні зони
    }
    for _, ts := range timestamps {
        _, err := FullTimeParse(ts.value)
        if err != nil { t.Errorf("FullTimeParse Error at %s: %s", ts.name, err) }
    }
}

func TestStrictTimeParse(t *testing.T) {
    // 🔹 Тільки формати, сумісні з RFC3339
    timestamps := []struct{name, value string}{
        {"time_in_utc", "2006-01-02T15:04:05Z"},
        {"time_in_utc_nano", "2006-01-02T15:04:05.123456789Z"},
        {"time_with_positive_zone_and_colon", "2006-01-02T15:04:05+01:00"},
        {"time_with_negative_zone_and_colon", "2006-01-02T15:04:05-01:00"},
    }
    // ... перевірка
}
```

**🎯 Призначення**: Гарантувати підтримку різних форматів дат у `#EXT-X-PROGRAM-DATE-TIME`.

---

### 🔹 Бенчмарки продуктивності

```go
func BenchmarkDecodeMasterPlaylist(b *testing.B) {
    for i := 0; i < b.N; i++ {
        f, _ := os.Open("sample-playlists/master.m3u8")
        p := NewMasterPlaylist()
        p.DecodeFrom(bufio.NewReader(f), false)
    }
}

func BenchmarkDecodeMediaPlaylist(b *testing.B) {
    for i := 0; i < b.N; i++ {
        f, _ := os.Open("sample-playlists/media-playlist-large.m3u8")  // 🔹 40,001 сегмент!
        p, _ := NewMediaPlaylist(50000, 50000)
        p.DecodeFrom(bufio.NewReader(f), true)
    }
}
```

**🎯 Призначення**: Оцінити продуктивність парсингу великих плейлистів (критично для CCTV з тисячами сегментів).

---

## 🔍 Детальний розбір ключових тестів

### 🔹 `TestDecodeMediaPlaylistExtInfNonStrict2`: Обробка `#EXTINF` у різних режимах

```go
tests := []struct {
    strict      bool
    extInf      string
    wantError   bool
    wantSegment *MediaSegment
}{
    // 🔹 Strict mode: помилки парсингу повертаються
    {true, "#EXTINF:invalid,", true, nil},           // ❌ Невалідне число
    {true, "#EXTINF:10.000", true, nil},             // ❌ Відсутня кома
    
    // 🔹 Non-strict mode: помилки ігноруються, значення за замовчуванням
    {false, "#EXTINF:invalid,", false, &MediaSegment{Duration: 0.0, Title: ""}},
    {false, "#EXTINF:10.000", false, &MediaSegment{Duration: 10.0, Title: ""}},
    
    // 🔹 Тайтли з комами: парситься все після першої коми
    {true, "#EXTINF:10.000,Title,Track", false, &MediaSegment{Duration: 10.0, Title: "Title,Track"}},
}
```

**🎯 Призначення**: Перевірити поведінку парсингу у `strict` та `non-strict` режимах — критично для обробки "брудних" плейлистів з камер.

---

### 🔹 `TestDecodeMediaPlaylistWithProgramDateTime`: Парсинг часових міток

```go
// 🔹 Очікувана дата першого сегмента: 2018-12-31T09:47:22+08:00
st, _ := time.Parse(time.RFC3339, "2018-12-31T09:47:22+08:00")
if !pp.Segments[0].ProgramDateTime.Equal(st) {
    t.Errorf("The program date time of the 1st segment should be: %v, actual value: %v",
        st, pp.Segments[0].ProgramDateTime)
}
```

**🎯 Призначення**: Гарантувати коректну синхронізацію сегментів з абсолютним часом — критично для інтеграції з системами аналітики подій.

---

### 🔹 `TestDecodeMediaPlaylistWithCustomTags`: Прив'язка тегів до сегментів

```go
expectedSegmentTags: []*struct {
    index int
    names []string
}{
    {1, []string{"#CUSTOM-SEGMENT-TAG:"}},                    // 🔹 Сегмент 1: один тег
    {2, []string{"#CUSTOM-SEGMENT-TAG:", "#CUSTOM-SEGMENT-TAG-B"}},  // 🔹 Сегмент 2: два теги
},
```

**🔄 Логіка перевірки:**
```
🔹 Ітерація по сегментах:
   • Для кожного сегмента перевіряємо seg.Custom map
   • Порівнюємо кількість та імена тегів з очікуваними
   • Перевіряємо, що теги прив'язані до правильних індексів
```

**🎯 Призначення**: Перевірити механізм прив'язки кастомних тегів до конкретних сегментів — критично для маркування подій у CCTV.

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Жорсткі шляхи до тестових файлів

```go
f, err := os.Open("sample-playlists/master.m3u8")  // 🔹 Відносний шлях
```

**🎯 Ризик**: Тести ламаються при запуску з іншої директорії або у CI/CD.

**✅ Рішення**: Використовувати `embed` (Go 1.16+) або `t.Chdir()`:
```go
//go:embed sample-playlists/*.m3u8
var testFiles embed.FS

f, err := testFiles.Open("sample-playlists/master.m3u8")
```

---

### 🔴 Проблема 2: Ігнорування помилок у бенчмарках

```go
f, _ := os.Open(...)  // 🔹 Помилка ігнорується
p, _ := NewMediaPlaylist(...)
p.DecodeFrom(...)     // 🔹 Помилка ігнорується
```

**🎯 Ризик**: Бенчмарки можуть вимірювати "успішний" парсинг невалідних даних.

**✅ Рішення**: Хоча б логувати помилки у бенчмарках:
```go
if err != nil { b.Fatal(err) }
```

---

### 🟡 Проблема 3: Відсутність тестів для `DecodeWith` з `io.Reader`

```go
// 🔹 Тести використовують тільки bytes.Buffer через bufio.NewReader
// 🔹 Але DecodeWith підтримує обидва типи
```

**✅ Рішення**: Додати тест з `strings.Reader` або `bytes.Reader` для покриття альтернативного шляху.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Валідація вхідного плейлиста перед обробкою

```go
func ValidatePlaylist(r io.Reader) error {
    // 🔹 Реєстрація кастомних декодерів для CCTV
    customTags := []m3u8.CustomDecoder{
        &CameraIDTag{},
        &EventTag{},
        &EncryptionTag{},
    }
    
    playlist, listType, err := m3u8.DecodeWith(r, true, customTags)
    if err != nil {
        return fmt.Errorf("playlist parse failed: %w", err)
    }
    
    if listType != m3u8.MEDIA {
        return fmt.Errorf("expected MEDIA playlist, got %v", listType)
    }
    
    media := playlist.(*m3u8.MediaPlaylist)
    
    // 🔹 Базові перевірки
    if media.TargetDuration <= 0 {
        return fmt.Errorf("invalid TargetDuration: %f", media.TargetDuration)
    }
    if media.Count() == 0 {
        return fmt.Errorf("playlist contains no segments")
    }
    
    // 🔹 Перевірка сегментів
    for i, seg := range media.Segments {
        if seg == nil { continue }
        if seg.Duration <= 0 {
            return fmt.Errorf("segment %d has invalid duration: %f", i, seg.Duration)
        }
        if seg.URI == "" {
            return fmt.Errorf("segment %d has empty URI", i)
        }
        // 🔹 Перевірка кастомних тегів (опціонально)
        if eventTag, ok := seg.Custom["#CCTV-EVENT:"]; ok {
            if e := eventTag.(*EventTag); e.Confidence < 0 || e.Confidence > 1 {
                return fmt.Errorf("segment %d has invalid confidence: %f", i, e.Confidence)
            }
        }
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Тестування власних кастомних тегів

```go
// 🔹 Ваш кастомний тег
type CameraIDTag struct {
    ID string
}

func (t *CameraIDTag) TagName() string { return "#CCTV-CAMERA-ID:" }
func (t *CameraIDTag) Decode(line string) (m3u8.CustomTag, error) {
    newTag := new(CameraIDTag)
    _, err := fmt.Sscanf(line, "#CCTV-CAMERA-ID:%s", &newTag.ID)
    return newTag, err
}
func (t *CameraIDTag) SegmentTag() bool { return false }
func (t *CameraIDTag) Encode() *bytes.Buffer {
    buf := new(bytes.Buffer)
    buf.WriteString(t.TagName())
    buf.WriteString(t.ID)
    return buf
}
func (t *CameraIDTag) String() string { return t.Encode().String() }

// 🔹 Юніт-тест для вашого тегу
func TestCameraIDTag(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        wantID   string
        wantErr  bool
    }{
        {"valid", "#CCTV-CAMERA-ID:CAM-001", "CAM-001", false},
        {"empty", "#CCTV-CAMERA-ID:", "", false},
        {"invalid format", "#CCTV-CAMERA-ID", "", true},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tag := &CameraIDTag{}
            result, err := tag.Decode(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !tt.wantErr {
                got := result.(*CameraIDTag).ID
                if got != tt.wantID {
                    t.Errorf("Decode() ID = %v, want %v", got, tt.wantID)
                }
            }
        })
    }
}
```

---

### 🔹 Приклад 3: Бенчмарк парсингу великих плейлистів камери

```go
func BenchmarkDecodeCCTVPlaylist(b *testing.B) {
    // 🔹 Генерація тестового плейлиста з 10,000 сегментів
    generateLargePlaylist := func() string {
        var sb strings.Builder
        sb.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:4\n")
        for i := 0; i < 10000; i++ {
            fmt.Fprintf(&sb, "#EXTINF:4.000,Camera 1 - Segment %d\nseg_%05d.ts\n", i, i)
        }
        sb.WriteString("#EXT-X-ENDLIST\n")
        return sb.String()
    }
    
    playlist := generateLargePlaylist()
    customTags := []m3u8.CustomDecoder{&CameraIDTag{}, &EventTag{}}
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _, err := m3u8.DecodeWith(strings.NewReader(playlist), false, customTags)
        if err != nil {
            b.Fatal(err)
        }
    }
}

// 🔹 Запуск: go test -bench=BenchmarkDecodeCCTVPlaylist -benchmem
// 🔹 Очікуваний результат: ~5-10 мс на плейлист з 10,000 сегментів
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При парсингу плейлистів:
    • Використовуйте DecodeWith() для реєстрації кастомних тегів
    • Завжди перевіряйте ListType перед type assertion
    • Обробляйте помилки з контекстом (шлях файлу, номер рядка)
    • Використовуйте strict=true для валідації, false для "брудних" даних

[ ] Для кастомних тегів:
    • Реалізуйте всі 5 методів інтерфейсу: TagName, Decode, SegmentTag, Encode, String
    • Використовуйте унікальні префікси: #CCTV-*, #MYAPP-* для уникнення конфліктів
    • Валідуйте атрибути у Decode() з чіткими помилками
    • Тестуйте round-trip: Encode → Decode → порівняння даних

[ ] Для тестування:
    • Використовуйте embed для тестових файлів замість відносних шляхів
    • Покрийте кейси: strict/non-strict, порожні значення, спеціальні символи
    • Додайте бенчмарки для великих плейлистів (10,000+ сегментів)
    • Тестуйте авто-детекцію типу плейлиста

[ ] Для безпеки:
    • Валідуйте вхідні URI сегментів (заборона `file://`, `../`)
    • Обмежуйте довжину кастомних атрибутів (напр., max 255 символів)
    • Не довіряйте кастомним тегам з ненадійних джерел

[ ] Для дебагу:
    • Логувайте сирі рядки при помилках парсингу
    • Використовуйте pp.Encode().String() для виводу відладженого плейлиста
    • Тестуйте з різними плеєрами: hls.js, Video.js, Safari, ExoPlayer
```

---

## 🎯 Висновок

> **Цей тест-сьют — ваш "компас надійності" для парсингу HLS**, який забезпечує:
> • ✅ Повне покриття стандартних тегів Master та Media плейлистів
> • ✅ Гнучку підтримку кастомних тегів з тестуванням механізму прив'язки
> • ✅ Надійну обробку помилок у strict/non-strict режимах
> • ✅ Підтримку різних форматів дат через `FullTimeParse`/`StrictTimeParse`
> • ✅ Бенчмарки продуктивності для великих плейлистів

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Надійна валідація вхідних плейлистів перед додаванням у конвеєр
- 🎯 Гнучке розширення через кастомні теги для маркування подій, камер, шифрування
- 🛡️ Захист від "брудних" даних через strict-режим та валідацію атрибутів
- ⚡ Продуктивний парсинг тисяч сегментів завдяки оптимізованому коду
- 🧪 Легке тестування власних розширень через шаблони з тест-сьюту

Потребуєте допомоги з інтеграцією парсингу плейлистів у ваш конвеєр або з реалізацією специфічних кастомних тегів для ваших сценаріїв? Напишіть — покажу готовий код для вашого випадку! 🚀🎬