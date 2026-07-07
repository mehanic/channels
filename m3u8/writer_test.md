# 🧪 `m3u8/generator_test.go`: Комплексне тестування генерації HLS-плейлистів

Це **всебічний тест-сьют** бібліотеки `github.com/grafov/m3u8`, який перевіряє коректність **генерації M3U8-файлів** з типобезпечних структур даних. Він покриває 40+ сценаріїв від базового створення плейлистів до складних випадків з шифруванням, SCTE-35, кастомними тегами, ковзним вікном та бенчмарками продуктивності.

---

## 🎯 Коротка відповідь

> **Цей тест-сьют — "золотий стандарт" для надійної генерації M3U8**: він перевіряє коректність створення Master/Media плейлистів, додавання сегментів, шифрування, SCTE-35 сповіщень, кастомних тегів, ковзного вікна для live-стрімінгу та бенчмарки продуктивності — ідеально для валідації вашого CCTV HLS-конвеєра.

---

## 📋 Структура тестів: Огляд за категоріями

### 🔹 Тести базової функціональності

| Тест | Перевіряє | Ключові асерції |
|------|-----------|----------------|
| `TestInterfaceImplemented` | Реалізація інтерфейсу `Playlist` | `CheckType()` для Master/Media |
| `TestCreateMediaPlaylistWithWrongSize` | Валідація параметрів конструктора | `winsize > capacity` → помилка |
| `TestLastSegmentMediaPlaylist` | Метод `last()` для циклічного буфера | Коректний індекс останнього сегмента |
| `TestAddSegmentToMediaPlaylist` | Додавання сегмента через `Append()` | URI, Duration, Title, SeqId |
| `TestAppendSegmentToMediaPlaylist` | Додавання через `AppendSegment()` | `TargetDuration`, `ErrPlaylistFull`, `SeqId` |

---

### 🔹 Тести медіа-плейлистів

| Тест | Перевіряє | Ключові асерції |
|------|-----------|----------------|
| `TestDiscontinuityForMediaPlaylist` | `#EXT-X-DISCONTINUITY` | Прапорець `Discontinuity` у сегменті |
| `TestProgramDateTimeForMediaPlaylist` | `#EXT-X-PROGRAM-DATE-TIME` | Парсинг `time.Time` з часовим поясом |
| `TestTargetDurationForMediaPlaylist` | Автоматичне оновлення `TargetDuration` | `math.Ceil(max_duration)` |
| `TestOverAddSegmentsToMediaPlaylist` | Обробка переповнення | `ErrPlaylistFull` при `count == capacity` |
| `TestMediaPlaylistWithIntegerDurations` | Форматування тривалостей | `DurationAsInt(true/false)` |
| `TestClosedMediaPlaylist` | Закриття плейлиста (VOD) | `#EXT-X-ENDLIST` у виводі |

---

### 🔹 Тести шифрування та метаданих

| Тест | Перевіряє | Ключові асерції |
|------|-----------|----------------|
| `TestSetSCTE35` | SCTE-35 сповіщення | Прив'язка `*SCTE` до сегмента |
| `TestSetSCTEForMediaPlaylist` | Форматування `#EXT-SCTE35` | Різні комбінації `Cue`, `ID`, `Time` |
| `TestSetKeyForMediaPlaylist` / `TestSetDefaultKeyForMediaPlaylist` | Автоматичне підвищення версії | `KEYFORMAT` → версія ≥5 |
| `TestSetDefaultMapForMediaPlaylist` / `TestSetMapForMediaPlaylist` | `#EXT-X-MAP` для fMP4 | Правильне форматування `BYTERANGE` |
| `TestEncryptionKeysInMediaPlaylist` | Індивідуальні ключі для сегментів | Кожен сегмент має свій `Key` |
| `TestEncryptionKeyMethodNoneInMediaPlaylist` | Вимкнення шифрування | `METHOD=NONE` без лапок |

---

### 🔹 Тести кастомних тегів

| Тест | Перевіряє | Ключові асерції |
|------|-----------|----------------|
| `TestEncodeMediaPlaylistWithCustomTags` | Плейлист- та сегмент-теги | Наявність/відсутність тегів у виводі |
| `TestEncodeMasterPlaylistWithCustomTags` | Кастомні теги master-плейлиста | `#CustomMTag` у заголовку |

**🔹 `MockCustomTag` — тестовий мок:**
```go
type MockCustomTag struct {
    name          string  // 🔹 Ідентифікатор: "#CustomTag:"
    err           error   // 🔹 Помилка для симуляції збою
    segment       bool    // 🔹 Чи прив'язаний до сегмента?
    encodedString string  // 🔹 Рядок для Encode()/String()
}
// Реалізація інтерфейсів: TagName, Decode, Encode, SegmentTag, String
```

---

### 🔹 Тести ковзного вікна та live-плейлистів

| Тест | Перевіряє | Ключові асерції |
|------|-----------|----------------|
| `TestMediaPlaylistWinsize` | Ковзне вікно = ємність | Правильна ітерація при `winsize == capacity` |
| `TestMediaPlaylist_Slide` | Метод `Slide()` для live | Автоматичне видалення найстарішого, оновлення `SeqNo` |
| `TestLargeMediaPlaylistWithParallel` | Паралельна обробка великих плейлистів | 40,001 сегмент, порівняння байтів, `sync.WaitGroup` |

**🔄 Логіка `Slide()` у тесті:**
```
🔹 Початок: capacity=4, winsize=3
   • Append t00, t01, t02, t03 → count=4, head=0, tail=0 (циклічний)
   
🔹 Slide t04:
   • Remove() → head=1, count=3, SeqNo=1
   • Append t04 → tail=1, count=4
   • Активні: [t01, t02, t03, t04] (індекси 1,2,3,0)
   
🔹 Перевірка:
   • Count() == 4 ✅
   • SeqNo == 1 ✅
   • Сегменти мають правильні URI та SeqId ✅
```

---

### 🔹 Тести master-плейлистів

| Тест | Перевіряє | Ключові асерції |
|------|-----------|----------------|
| `TestNewMasterPlaylist` / `TestNewMasterPlaylistWithParams` | Базове додавання варіантів | `VariantParams` → `#EXT-X-STREAM-INF` |
| `TestNewMasterPlaylistWithAlternatives` | Аудіо/субтитри доріжки | `#EXT-X-MEDIA` з правильними атрибутами, версія ≥4 |
| `TestNewMasterPlaylistWithClosedCaptionEqNone` | `CLOSED-CAPTIONS=NONE` | Без лапок для `NONE`, з лапками для інших значень |
| `TestEncodeMasterPlaylistWithExistingQuery` | Додавання аргументів до URI | `?k1=v1&k2=v2&k3=v3` (не `??`) |
| `TestEncodeMasterPlaylistWithStreamInfName` | Нестандартний атрибут `NAME` | `NAME="HD 960p"` у `#EXT-X-STREAM-INF` |

---

### 🔹 Приклади коду (Example*)

```go
// 🔹 ExampleMediaPlaylist_String: базовий вивід
p, _ := NewMediaPlaylist(1, 2)
p.Append("test01.ts", 5.0, "")
p.Append("test02.ts", 6.0, "")
fmt.Printf("%s\n", p)
// Output: #EXTM3U... #EXTINF:5.000,\ntest01.ts (тільки перший сегмент, winsize=1)

// 🔹 ExampleMediaPlaylist_String_Winsize0_VOD: повний VOD плейлист
p, _ := NewMediaPlaylist(0, 2)  // 🔹 winsize=0 → всі сегменти
p.Append("test01.ts", 5.0, "")
p.Append("test02.ts", 6.0, "")
p.Close()  // 🔹 Додає #EXT-X-ENDLIST
// Output: обидва сегменти + #EXT-X-ENDLIST

// 🔹 ExampleMasterPlaylist_String_with_hlsv7: HLS v7 з HDR
m.SetVersion(7)
m.SetIndependentSegments(true)
m.Append("hdr10_1080/prog_index.m3u8", p, VariantParams{
    VideoRange: "PQ", Codecs: "hvc1.2.4.L123.B0", 
    Resolution: "1920x1080", Captions: "NONE", HDCPLevel: "TYPE-0"})
// Output: #EXT-X-VERSION:7, #EXT-X-INDEPENDENT-SEGMENTS, VIDEO-RANGE=PQ...
```

**🎯 Призначення**: Документувати використання бібліотеки через `go doc` та автоматично перевіряти приклади.

---

### 🔹 Бенчмарки продуктивності

```go
func BenchmarkEncodeMasterPlaylist(b *testing.B) {
    // 🔹 Завантаження тестового master-плейлиста
    f, _ := os.Open("sample-playlists/master.m3u8")
    p := NewMasterPlaylist()
    p.DecodeFrom(bufio.NewReader(f), true)
    
    for i := 0; i < b.N; i++ {
        p.ResetCache()      // 🔹 Скидання кешу перед кодуванням
        _ = p.Encode()      // 🔹 Вимірюємо тільки генерацію
    }
}

func BenchmarkEncodeMediaPlaylist(b *testing.B) {
    // 🔹 Великий media-плейлист: 50,000 сегментів
    f, _ := os.Open("sample-playlists/media-playlist-large.m3u8")
    p, _ := NewMediaPlaylist(50000, 50000)
    p.DecodeFrom(bufio.NewReader(f), true)
    
    for i := 0; i < b.N; i++ {
        p.ResetCache()
        _ = p.Encode()
    }
}
```

**🎯 Призначення**: Оцінити продуктивність генерації великих плейлистів — критично для CCTV з тисячами сегментів.

---

## 🔍 Детальний розбір ключових тестів

### 🔹 `TestSetSCTEForMediaPlaylist`: Форматування різних синтаксисів SCTE-35

```go
tests := []struct {
    Cue      string
    ID       string
    Time     float64
    Expected string  // 🔹 Очікуваний рядок у виводі
}{
    {"CueData1", "", 0, `#EXT-SCTE35:CUE="CueData1"` + "\n"},
    {"CueData2", "ID2", 0, `#EXT-SCTE35:CUE="CueData2",ID="ID2"` + "\n"},
    {"CueData3", "ID3", 3.141, `#EXT-SCTE35:CUE="CueData3",ID="ID3",TIME=3.141` + "\n"},
    // ... інші варіанти
}

for _, test := range tests {
    p, _ := NewMediaPlaylist(1, 1)
    p.Append("test01.ts", 5.0, "")
    p.SetSCTE(test.Cue, test.ID, test.Time)  // 🔹 Виклик методу
    
    if !strings.Contains(p.String(), test.Expected) {
        t.Errorf("Test %+v did not contain: %q, playlist: %v", test, test.Expected, p.String())
    }
}
```

**🎯 Призначення**: Перевірити коректне форматування атрибутів `CUE`, `ID`, `TIME` з урахуванням порожніх значень.

---

### 🔹 `TestEncryptionKeysInMediaPlaylist`: Індивідуальні ключі для кожного сегмента

```go
p, _ := NewMediaPlaylist(5, 5)
for i := uint(0); i < 5; i++ {
    uri := fmt.Sprintf("uri-%d", i)
    expected := &Key{
        Method: "AES-128", URI: uri, IV: fmt.Sprintf("%d", i),
        Keyformat: "identity", Keyformatversions: "1",
    }
    _ = p.Append(uri+".ts", 4, "")
    _ = p.SetKey(expected.Method, expected.URI, expected.IV, 
                 expected.Keyformat, expected.Keyformatversions)
    
    // 🔹 Перевірка: ключ прив'язаний саме до цього сегмента
    if *p.Segments[i].Key != *expected {
        t.Errorf("Key %+v does not match expected %+v", p.Segments[i].Key, expected)
    }
}
```

**🎯 Призначення**: Гарантувати, що `SetKey()` змінює ключ тільки для **останнього доданого сегмента**, а не для всього плейлиста.

---

### 🔹 `TestMediaPlaylist_Slide`: Ковзне вікно в дії

```go
m, _ := NewMediaPlaylist(3, 4)  // 🔹 winsize=3, capacity=4
_ = m.Append("t00.ts", 10, "")  // [t00], head=0, tail=1
_ = m.Append("t01.ts", 10, "")  // [t00,t01], tail=2
_ = m.Append("t02.ts", 10, "")  // [t00,t01,t02], tail=3
_ = m.Append("t03.ts", 10, "")  // [t00,t01,t02,t03], tail=0 (циклічний)

// 🔹 Перевірка перед Slide: 4 сегменти, SeqNo=0
if m.Count() != 4 { t.Fatalf(...) }
if m.SeqNo != 0 { t.Errorf(...) }

// 🔹 Slide t04: Remove() + Append()
m.Slide("t04.ts", 10, "")
// • Remove(): head=1, count=3, SeqNo=1
// • Append(): tail=1, count=4
// • Активні: [t01,t02,t03,t04] (індекси 1,2,3,0)

// 🔹 Перевірка після Slide: SeqNo=1, правильні URI/SeqId
if m.SeqNo != 1 { t.Errorf(...) }
for idx, seqId := 0, 1; idx < 3; idx, seqId = idx+1, seqId+1 {
    segIdx := (m.head + idx) % m.capacity  // 🔹 Циклічний індекс
    seg := m.Segments[segIdx]
    if seg.URI != fmt.Sprintf("t%02d.ts", seqId) || seg.SeqId != uint64(seqId) {
        t.Errorf(...)
    }
}
```

**🎯 Призначення**: Перевірити коректну роботу циклічного буфера при live-оновленні плейлиста.

---

## ⚠️ Критичні зауваження та покращення

### 🔴 Проблема 1: Жорсткі шляхи до тестових файлів

```go
f, err := os.Open("sample-playlists/media-playlist-large.m3u8")  // 🔹 Відносний шлях
```

**🎯 Ризик**: Тести ламаються при запуску з іншої директорії або у CI/CD.

**✅ Рішення**: Використовувати `embed` (Go 1.16+):
```go
//go:embed sample-playlists/*.m3u8
var testFiles embed.FS

f, err := testFiles.Open("sample-playlists/media-playlist-large.m3u8")
```

---

### 🔴 Проблема 2: Ігнорування помилок у бенчмарках

```go
f, _ := os.Open(...)  // 🔹 Помилка ігнорується
p, _ := NewMediaPlaylist(...)
_ = p.Encode()        // 🔹 Результат ігнорується
```

**🎯 Ризик**: Бенчмарки можуть вимірювати "успішне" кодування невалідних даних.

**✅ Рішення**: Хоча б логувати помилки:
```go
if err != nil { b.Fatal(err) }
```

---

### 🟡 Проблема 3: Відсутність тестів для `DurationAsInt` у виводі

```go
p.DurationAsInt(true)  // 🔹 Встановлено, але не перевіряється у виводі
```

**✅ Рішення**: Додати перевірку, що тривалості форматуються як цілі числа:
```go
output := p.String()
if strings.Contains(output, "5.600") {  // 🔹 Плаваюче число
    t.Errorf("Expected integer duration, got float: %s", output)
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Тестування власного кастомного тегу

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

// 🔹 Юніт-тест
func TestCameraIDTag_EncodeDecode(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        wantID   string
        wantErr  bool
    }{
        {"valid", "#CCTV-CAMERA-ID:CAM-001", "CAM-001", false},
        {"empty", "#CCTV-CAMERA-ID:", "", false},
        {"invalid", "#CCTV-CAMERA-ID", "", true},
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
                // 🔹 Round-trip тест
                encoded := tag.Encode().String()
                if encoded != tt.input {
                    t.Errorf("Encode() = %v, want %v", encoded, tt.input)
                }
            }
        })
    }
}
```

---

### 🔹 Приклад 2: Тестування live-плейлиста з ковзним вікном

```go
func TestLivePlaylist_SlideWithEvents(t *testing.T) {
    // 🔹 Створення live-плейлиста: 10-сегментне вікно, 20-сегментна ємність
    p, err := m3u8.NewMediaPlaylist(10, 20)
    if err != nil { t.Fatal(err) }
    
    p.SetVersion(7)
    p.SetTargetDuration(4)
    p.SetPlaylistType("event")
    
    // 🔹 Реєстрація кастомних тегів
    p.WithCustomDecoders([]m3u8.CustomDecoder{&EventTag{}})
    
    // 🔹 Додавання 15 сегментів з подіями
    for i := 0; i < 15; i++ {
        seg := &m3u8.MediaSegment{
            URI: fmt.Sprintf("seg_%03d.ts", i),
            Duration: 4.0,
        }
        
        // 🔹 Додавання події для кожного 3-го сегмента
        if i%3 == 0 {
            seg.Custom = map[string]m3u8.CustomTag{
                "#CCTV-EVENT:": &EventTag{
                    Type: "motion",
                    Confidence: 0.95,
                    Timestamp: time.Now().Unix(),
                },
            }
        }
        
        if err := p.AppendSegment(seg); err != nil {
            t.Fatalf("AppendSegment failed: %v", err)
        }
    }
    
    // 🔹 Перевірка: тільки останні 10 сегментів у виводі
    output := p.String()
    if strings.Contains(output, "seg_000.ts") {
        t.Error("Oldest segment should be removed from output")
    }
    if !strings.Contains(output, "seg_014.ts") {
        t.Error("Newest segment should be in output")
    }
    
    // 🔹 Перевірка: кастомні теги тільки для сегментів 12, 15 (останні з подіями)
    if !strings.Contains(output, "#CCTV-EVENT:TYPE=\"motion\"") {
        t.Error("Expected custom event tags in output")
    }
}
```

---

### 🔹 Приклад 3: Бенчмарк генерації live-плейлиста

```go
func BenchmarkGenerateLivePlaylist(b *testing.B) {
    // 🔹 Підготовка: 1000 сегментів, winsize=100
    segments := make([]Segment, 1000)
    for i := range segments {
        segments[i] = Segment{
            URI: fmt.Sprintf("seg_%04d.ts", i),
            Duration: 4.0,
            Event: nil,  // 🔹 Без подій для чистого бенчмарку
        }
    }
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        p, _ := m3u8.NewMediaPlaylist(100, 200)
        p.SetVersion(7)
        p.SetTargetDuration(4)
        p.SetPlaylistType("event")
        
        for _, seg := range segments {
            _ = p.Append(seg.URI, seg.Duration, "")
        }
        
        _ = p.String()  // 🔹 Вимірюємо генерацію
    }
}

// 🔹 Запуск: go test -bench=BenchmarkGenerateLivePlaylist -benchmem
// 🔹 Очікуваний результат: ~1-5 мс на плейлист з 1000 сегментів
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні плейлистів:
    • Використовуйте winsize > 0 для live, winsize = 0 для VOD
    • Встановлюйте capacity >= 2×winsize для уникнення частих розширень масиву
    • Реєструйте кастомні теги через WithCustomDecoders() перед додаванням

[ ] Для шифрування:
    • Використовуйте конструктор з валідацією методу та URI
    • Для Widevine: заповнюйте Keyformat та Keyformatversions
    • Для fMP4: додавайте Map з URI init-файлу через SetDefaultMap()

[ ] Для live-стрімінгу:
    • Налаштовуйте TargetDuration на основі максимальної тривалості сегмента
    • Використовуйте Slide() замість Append()+Remove() для атомарного оновлення
    • Тестуйте ковзне вікно з циклічним буфером (head/tail/capacity)

[ ] Для кастомних тегів:
    • Реалізуйте всі методи інтерфейсів: TagName, Decode/Encode, SegmentTag, String
    • Використовуйте унікальні префікси: #CCTV-*, #MYAPP-* для уникнення конфліктів
    • Валідуйте атрибути у Decode() з чіткими помилками
    • Тестуйте round-trip: Encode → Decode → порівняння даних

[ ] Для тестування:
    • Використовуйте embed для тестових файлів замість відносних шляхів
    • Покрийте кейси: strict/non-strict, порожні значення, спеціальні символи
    • Додайте бенчмарки для великих плейлистів (10,000+ сегментів)
    • Тестуйте паралельну генерацію через sync.WaitGroup

[ ] Для дебагу:
    • Логувайте закодований плейлист: log.Printf("📝 Output:\n%s", playlist.String())
    • Перевіряйте версію плейлиста: log.Printf("🔢 Version: %d", playlist.Version())
    • Тестуйте з різними розмірами вікна: 3, 10, 100 сегментів
```

---

## 🎯 Висновок

> **Цей тест-сьют — ваш "компас надійності" для генерації HLS**, який забезпечує:
> • ✅ Повне покриття базової функціональності: створення, додавання, кодування
> • ✅ Надійну обробку шифрування, SCTE-35, кастомних тегів
> • ✅ Коректну роботу ковзного вікна для live-плейлистів
> • ✅ Бенчмарки продуктивності для великих плейлистів
> • ✅ Приклади коду через `Example*` функції для документації

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Надійна генерація live/VOD плейлистів з правильними метаданими
- 📡 Ефективне оновлення live-плейлистів у реальному часі з ковзним вікном
- 🔐 Безпечне керування шифруванням та DRM-метаданими
- 🔄 Легке розширення через кастомні теги для маркування подій, камер, аналітики
- 🛡️ Валідація вхідних даних та чіткі інтерфейси запобігають помилкам

Потребуєте допомоги з інтеграцією генерації плейлистів у ваш конвеєр або з налаштуванням тестів для специфічних сценаріїв CCTV? Напишіть — покажу готовий код для вашого випадку! 🚀🧪