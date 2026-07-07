# 🔍 Глибокий розбір тестів: `Reader` — інтеграційне тестування парсингу M3U8

Цей файл містить **комплексну батарею інтеграційних тестів** для функції `ReadFile`, яка читає та парсить реальні M3U8-файли з фікстур. Розберемо архітектурно та детально кожен сценарій.

---

## 📦 Архітектура тестового файлу: матриця покриття

```
┌─────────────────────────────────────────────────┐
│ Тести Reader: 13 функцій → 7 категорій          │
├─────────────────────────────────────────────────┤
│ 🔹 Master Playlist парсинг                      │
│    • TestReader (базовий)                       │
│    • TestReader_IFrame (I-Frame варіанти)       │
│    • TestReader_VariantAudio (аудіо-доріжки)    │
│    • TestReader_VariantAngles (відео-кути)      │
│    • TestReader_SessionData (метадані сесії)    │
│                                                 │
│ 🔹 Media Playlist парсинг                       │
│    • TestReader_MediaPlaylist (VOD)             │
│    • TestReader_PlaylistLiveCheck (live vs VOD) │
│    • TestReader_IFramePlaylist (I-Frame media)  │
│    • TestReader_PlaylistWithComments (коментарі)│
│    • TestReader_Timestamp (PROGRAM-DATE-TIME)   │
│    • TestReader_DateRange (SCTE-35 події)       │
│                                                 │
│ 🔹 Шифрування та безпека                        │
│    • TestReader_Encrypted (#EXT-X-KEY)          │
│                                                 │
│ 🔹 fMP4/CMAF підтримка                          │
│    • TestReader_Map (#EXT-X-MAP + BYTERANGE)    │
│                                                 │
│ 🔹 Обробка помилок                              │
│    • TestReader_Invalid (неіснуючий файл)       │
└─────────────────────────────────────────────────┘
```

### 🎯 Навіщо таке розділення?
| Категорія | Призначення | Приклад у вашому проекті |
|-----------|-------------|-------------------------|
| **Master Playlist** | Парсинг варіантів якості, аудіо, метаданих | Генерація `master.m3u8` для Al Arabiya |
| **Media Playlist** | Парсинг сегментів, таймштампів, подій | Обробка live-ковзного вікна сегментів |
| **Шифрування** | Парсинг `#EXT-X-KEY` для DRM | Захищений стрімінг платного контенту |
| **fMP4/CMAF** | Парсинг `#EXT-X-MAP` + `BYTERANGE` | Оптимізація init-файлів для CCTV |
| **Помилки** | Стійкість до невалідного вводу | Моніторинг корумпованих плейлистів |

---

## 🔬 Детальний розбір ключових тестів

### 1️⃣ `TestReader` — базовий Master Playlist парсинг

```go
func TestReader(t *testing.T) {
    // 🎯 Читання реального файлу з фікстури
    p, err := m3u8.ReadFile("fixtures/master.m3u8")
    assert.Nil(t, err)
    
    // 🎯 Валідація типу та структури
    assert.True(t, p.IsValid())      // ✅ Не змішані типи
    assert.True(t, p.IsMaster())     // ✅ Визначено як Master
    assert.Nil(t, p.DiscontinuitySequence)  // ✅ Опціональний, відсутній
    assert.True(t, p.IndependentSegments)   // ✅ Прапорець встановлено
    
    // 🎯 Перевірка першого елемента: SessionKeyItem (шифрування)
    item := p.Items[0]
    assert.IsType(t, &m3u8.SessionKeyItem{}, item)  // ✅ Type assertion
    keyItem := item.(*m3u8.SessionKeyItem)
    assert.Equal(t, "AES-128", keyItem.Encryptable.Method)
    assertNotNilEqual(t, "https://priv.example.com/key.php?r=52", keyItem.Encryptable.URI)
    
    // 🎯 Другий елемент: PlaybackStart (точка старту)
    item = p.Items[1]
    assert.IsType(t, &m3u8.PlaybackStart{}, item)
    psi := item.(*m3u8.PlaybackStart)
    assert.Equal(t, 20.2, psi.TimeOffset)  // ✅ Почати з 20.2 секунди
    
    // 🎯 Третій елемент: PlaylistItem (варіант якості)
    item = p.Items[2]
    assert.IsType(t, &m3u8.PlaylistItem{}, item)
    pi := item.(*m3u8.PlaylistItem)
    assert.Equal(t, "hls/1080-7mbps/1080-7mbps.m3u8", pi.URI)
    assertNotNilEqual(t, "1", pi.ProgramID)
    assertNotNilEqual(t, 1920, pi.Width)   // ✅ З Resolution парсинг
    assertNotNilEqual(t, 1080, pi.Height)
    assert.Equal(t, "1920x1080", pi.Resolution.String())  // ✅ Серіалізація
    assert.Equal(t, "avc1.640028,mp4a.40.2", pi.CodecsString())  // ✅ CODECS генерація
    assert.Equal(t, 5042000, pi.Bandwidth)  // ✅ 5 Mbps варіант
    assert.False(t, pi.IFrame)  // ✅ Звичайний варіант, не I-Frame
    
    // 🎯 Останній елемент: варіант без Resolution (аудіо-тільки?)
    item = p.Items[7]
    pi = item.(*m3u8.PlaylistItem)
    assert.Equal(t, "hls/64k/64k.m3u8", pi.URI)  // ✅ 64 kbps аудіо-варіант
    assert.Nil(t, pi.Height)  // ✅ Немає відео → nil
    assert.Nil(t, pi.Width)
    assert.Empty(t, pi.Resolution.String())  // ✅ Порожній рядок для nil Resolution
    assert.Equal(t, 6400, pi.Bandwidth)  // ✅ 6.4 kbps
    
    // 🎯 Загальна перевірка: 8 елементів у плейлисті
    assert.Equal(t, 8, p.ItemSize())
}
```

#### 🎯 Що тестує цей кейс?
| Аспект | Перевірка | Чому це важливо |
|--------|-----------|----------------|
| **Поліморфізм** | `assert.IsType` + type assertion | `[]Item` містить різні типи → потрібна динамічна типізація |
| **Композиція** | `keyItem.Encryptable.Method` | Перевірка архітектурного патерну делегування |
| **Resolution парсинг** | `Width/Height` → `Resolution.String()` | Зворотна сумісність: старі поля → новий тип |
| **CodecsString()** | Авто-генерація або прямий доступ | Логіка `formatCodecs()` працює коректно |
| **nil-безпека** | `pi.Resolution.String()` для nil | Методи мають бути безпечні для nil-приймача |

#### ⚠️ Потенційні проблеми
```go
// ❌ "fixtures/master.m3u8" — жорстко закодований шлях
// • Тест залежить від наявності файлу у конкретній директорії
// • При запуску з іншої директорії → файл не знайдено → false negative

// ✅ Рішення: використовувати embed (Go 1.16+)
//go:embed fixtures/*.m3u8
var testFixtures embed.FS

func TestReader(t *testing.T) {
    content, err := testFixtures.ReadFile("fixtures/master.m3u8")
    assert.NoError(t, err)
    p, err := m3u8.ParseReader(bytes.NewReader(content))  // Парсинг з memory
    assert.NoError(t, err)
    // ... решта тесту ...
}
```

---

### 2️⃣ `TestReader_IFrame` — парсинг I-Frame варіантів

```go
func TestReader_IFrame(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/masterIframes.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.True(t, p.IsMaster())
    assert.Equal(t, 7, p.ItemSize())
    
    // 🎯 Перевірка I-Frame варіанту
    item := p.Items[1]
    assert.IsType(t, &m3u8.PlaylistItem{}, item)
    pi := item.(*m3u8.PlaylistItem)
    
    assert.Equal(t, "low/iframe.m3u8", pi.URI)
    assert.Equal(t, 86000, pi.Bandwidth)  // ✅ Низький бітрейт для iframe
    assert.True(t, pi.IFrame)  // ✅ Ключовий прапорець!
}
```

#### 🎯 Чому I-Frame варіанти важливі?
```
🔄 Швидке перемотування архіву:
• Звичайні сегменти: 4с відео = ~100 ключових кадрів
• I-Frame сегменти: тільки ключові кадри → менший розмір, швидкий seek

📺 CCTV архів:
• Користувач клікає на таймлайн → плеєр завантажує I-Frame варіант
• Миттєвий перехід до будь-якого моменту без декодування всього потоку

🔗 Інтеграція з вашим проектом:
• Генерувати I-Frame варіанти через FFmpeg: -g ключові кадри
• Додавати #EXT-X-I-FRAME-STREAM-INF у Master Playlist
• Клієнти автоматично використають для швидкого seek
```

---

### 3️⃣ `TestReader_MediaPlaylist` — парсинг VOD media-плейлиста

```go
func TestReader_MediaPlaylist(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/playlist.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.False(t, p.IsMaster())  // ✅ Media, не Master
    
    // 🎯 Перевірка заголовків Media Playlist
    assertNotNilEqual(t, 4, p.Version)           // #EXT-X-VERSION:4
    assert.Equal(t, 1, p.Sequence)               // #EXT-X-MEDIA-SEQUENCE:1
    assertNotNilEqual(t, 8, p.DiscontinuitySequence)  // #EXT-X-DISCONTINUITY-SEQUENCE:8
    assertNotNilEqual(t, false, p.Cache)         // #EXT-X-ALLOW-CACHE:NO
    assert.Equal(t, 12, p.Target)                // #EXT-X-TARGETDURATION:12
    assertNotNilEqual(t, "VOD", p.Type)          // #EXT-X-PLAYLIST-TYPE:VOD
    
    // 🎯 Перевірка сегмента
    item := p.Items[0]
    assert.IsType(t, &m3u8.SegmentItem{}, item)
    si := item.(*m3u8.SegmentItem)
    assert.Equal(t, 11.344644, si.Duration)  // ✅ Точність до мікросекунд
    assert.Nil(t, si.Comment)  # ✅ Коментар відсутній
    
    // 🎯 Перевірка таймштампу
    item = p.Items[4]
    assert.IsType(t, &m3u8.TimeItem{}, item)
    ti := item.(*m3u8.TimeItem)
    assert.Equal(t, "2010-02-19T14:54:23Z", m3u8.FormatTime(ti.Time))  # ✅ RFC3339
    
    // 🎯 Загальна перевірка: 140 елементів (багато сегментів)
    assert.Equal(t, 140, p.ItemSize())
}
```

#### 🎯 Ключові аспекти Media Playlist
| Заголовок | Значення | Призначення |
|-----------|----------|-------------|
| `#EXT-X-VERSION:4` | 4 | Підтримка базових функцій HLS |
| `#EXT-X-MEDIA-SEQUENCE:1` | 1 | Номер першого сегмента (для live) |
| `#EXT-X-DISCONTINUITY-SEQUENCE:8` | 8 | Лічильник розривів у потоці |
| `#EXT-X-ALLOW-CACHE:NO` | false | Заборона кешування (live-контент) |
| `#EXT-X-TARGETDURATION:12` | 12 | Макс. тривалість сегмента (сек) |
| `#EXT-X-PLAYLIST-TYPE:VOD` | "VOD" | Весь контент доступний, не полінгувати |

---

### 4️⃣ `TestReader_PlaylistLiveCheck` — розрізнення live vs VOD

```go
func TestReader_PlaylistLiveCheck(t *testing.T) {
    // 🎯 VOD плейлист: має #EXT-X-ENDLIST
    p, err := m3u8.ReadFile("fixtures/playlist.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.False(t, p.IsLive())  # ✅ VOD → !IsLive()
    
    // 🎯 Live плейлист: немає #EXT-X-ENDLIST
    p, err = m3u8.ReadFile("fixtures/playlist-live.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.True(t, p.IsLive())  # ✅ Live → IsLive()
}
```

#### 🎯 Як `IsLive()` визначає тип?
```go
// Припустима реалізація:
func (pl *Playlist) IsLive() bool {
    // 🎯 Master-плейлисти не бувають "live" у сенсі сегментів
    if pl.IsMaster() {
        return false
    }
    
    // 🎯 Для Media: перевірка прапорця Live
    // (встановлюється парсером, якщо немає #EXT-X-ENDLIST)
    return pl.Live
}

// 🎯 Парсер встановлює Live=true, якщо:
// • Плейлист типу Media
// • Відсутній #EXT-X-ENDLIST у кінці файлу
// • (Опціонально) #EXT-X-PLAYLIST-TYPE відсутній або != "VOD"
```

---

### 5️⃣ `TestReader_IFramePlaylist` — media-плейлист з byte ranges

```go
func TestReader_IFramePlaylist(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/iframes.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.True(t, p.IFramesOnly)  # ✅ Прапорець: тільки iframe-сегменти
    assert.Equal(t, 3, p.ItemSize())
    
    // 🎯 Перший сегмент: з ByteRange
    item := p.Items[0]
    assert.IsType(t, &m3u8.SegmentItem{}, item)
    si := item.(*m3u8.SegmentItem)
    
    assert.Equal(t, 4.12, si.Duration)
    assert.NotNil(t, si.ByteRange)  # ✅ BYTERANGE присутній
    assertNotNilEqual(t, 9400, si.ByteRange.Length)   # ✅ 9400 байт
    assertNotNilEqual(t, 376, si.ByteRange.Start)     # ✅ Починаючи з 376
    assert.Equal(t, "segment1.ts", si.Segment)
    
    // 🎯 Другий сегмент: ByteRange без Start (offset=0 за замовчуванням)
    item = p.Items[1]
    si = item.(*m3u8.SegmentItem)
    assert.NotNil(t, si.ByteRange)
    assertNotNilEqual(t, 7144, si.ByteRange.Length)  # ✅ 7144 байт
    assert.Nil(t, si.ByteRange.Start)  # ✅ Start=nil → offset=0
}
```

#### 🎯 Семантика `ByteRange.Start = nil`
```go
// ✅ Специфікація: якщо offset не вказано, за замовчуванням 0
// ❌ Але: nil ≠ 0 у коді → може призвести до помилок

// 🎯 Приклад серіалізації:
br1 := &ByteRange{Length: pointer.ToInt(9400), Start: pointer.ToInt(376)}
fmt.Println(br1.String())  # "9400@376"

br2 := &ByteRange{Length: pointer.ToInt(7144), Start: nil}
fmt.Println(br2.String())  # "7144" (без @0)

// ✅ Це коректно за специфікацією, але варто документувати:
// • nil Start = "початок файлу" = еквівалентно 0
// • Плеєри мають обробляти обидва формати однаково
```

---

### 6️⃣ `TestReader_PlaylistWithComments` — парсинг коментарів у сегментах

```go
func TestReader_PlaylistWithComments(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/playlistWithComments.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    
    // 🎯 Перевірка заголовків
    assert.False(t, p.IsMaster())
    assertNotNilEqual(t, 4, p.Version)
    assert.Equal(t, 1, p.Sequence)
    assertNotNilEqual(t, false, p.Cache)
    assert.Equal(t, 12, p.Target)
    assertNotNilEqual(t, "VOD", p.Type)
    
    // 🎯 Сегмент з коментарем
    item := p.Items[0]
    assert.IsType(t, &m3u8.SegmentItem{}, item)
    si := item.(*m3u8.SegmentItem)
    
    assert.Equal(t, 11.344644, si.Duration)
    assertNotNilEqual(t, "anything", si.Comment)  # ✅ Коментар розпаршено!
    
    // 🎯 Сегмент з #EXT-X-DISCONTINUITY
    item = p.Items[1]
    assert.IsType(t, &m3u8.DiscontinuityItem{}, item)  # ✅ Розрив розпізнано
    
    assert.Equal(t, 139, p.ItemSize())
}
```

#### 🎯 Парсинг коментарів у `#EXTINF`
```go
// 📋 Формат: #EXTINF:duration,[comment]
// Приклад: #EXTINF:11.344644,anything

// 🎯 Припустима реалізація парсингу:
func NewSegmentItem(text string) (*SegmentItem, error) {
    // Видалення префіксу
    line := strings.TrimPrefix(text, SegmentItemTag+":")
    
    // Розбиття за комою: [тривалість, коментар?]
    values := strings.SplitN(line, ",", 2)  # ✅ SplitN обмежує до 2 частин!
    
    duration, _ := strconv.ParseFloat(values[0], 64)
    
    var comment *string
    if len(values) > 1 && values[1] != "" {
        comment = &values[1]  # ✅ Зберігаємо коментар
    }
    
    return &SegmentItem{Duration: duration, Comment: comment}, nil
}

// ⚠️ Важливо: SplitN(2), а не Split!
// • Split("a,b,c", ",") → ["a", "b", "c"] → коментар="b" (втрата "c"!)
// • SplitN("a,b,c", ",", 2) → ["a", "b,c"] → коментар="b,c" ✅
```

---

### 7️⃣ `TestReader_VariantAudio` — парсинг аудіо-доріжок у Master Playlist

```go
func TestReader_VariantAudio(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/variantAudio.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.True(t, p.IsMaster())
    assert.Equal(t, 10, p.ItemSize())
    
    // 🎯 Перший елемент: MediaItem для аудіо
    item := p.Items[0]
    assert.IsType(t, &m3u8.MediaItem{}, item)
    mi := item.(*m3u8.MediaItem)
    
    assert.Equal(t, "AUDIO", mi.Type)
    assert.Equal(t, "audio-lo", mi.GroupID)
    assert.Equal(t, "English", mi.Name)
    assertNotNilEqual(t, "eng", mi.Language)           # ✅ RFC 5646 код
    assertNotNilEqual(t, "spoken", mi.AssocLanguage)   # ✅ Мова асоціації
    assertNotNilEqual(t, true, mi.AutoSelect)          # ✅ YES → true
    assertNotNilEqual(t, true, mi.Default)             # ✅ YES → true
    assertNotNilEqual(t, "englo/prog_index.m3u8", mi.URI)
    assertNotNilEqual(t, true, mi.Forced)              # ✅ YES → true
}
```

#### 🎯 Ключові аспекти аудіо-доріжок
| Атрибут | Значення | Призначення |
|---------|----------|-------------|
| `TYPE=AUDIO` | "AUDIO" | Тип медіа: аудіо-доріжка |
| `GROUP-ID="audio-lo"` | "audio-lo" | Ідентифікатор групи для прив'язки до варіантів |
| `NAME="English"` | "English" | Відображення в інтерфейсі плеєра |
| `LANGUAGE="eng"` | "eng" | Автовибір за мовою пристрою (RFC 5646) |
| `DEFAULT=YES` | true | Обрати за замовчуванням, якщо немає переваг |
| `AUTOSELECT=YES` | true | Дозволити плеєру обирати автоматично |
| `FORCED=YES` | true | Показувати примусово (для субтитрів) |

---

### 8️⃣ `TestReader_SessionData` — парсинг метаданих сесії

```go
func TestReader_SessionData(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/sessionData.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.Equal(t, 3, p.ItemSize())
    
    // 🎯 SessionDataItem для кастомних метаданих
    item := p.Items[0]
    assert.IsType(t, &m3u8.SessionDataItem{}, item)
    sdi := item.(*m3u8.SessionDataItem)
    
    assert.Equal(t, "com.example.lyrics", sdi.DataID)  # ✅ Зворотний DNS
    assertNotNilEqual(t, "lyrics.json", sdi.URI)        # ✅ Посилання на дані
}
```

#### 🎯 Сценарії використання `#EXT-X-SESSION-DATA`
```
📊 Аналітика переглядів:
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.viewers",VALUE="15420"
→ Плеєр може показувати статистику в реальному часі

🌐 Локалізація:
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.title",VALUE="Live Feed",LANGUAGE="en"
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.title",VALUE="بث مباشر",LANGUAGE="ar"
→ Автоматичний вибір заголовка за мовою користувача

🔗 Динамічні метадані:
#EXT-X-SESSION-DATA:DATA-ID="com.alarabiya.alerts",URI="/api/alerts/ch1.json"
→ Плеєнт завантажує актуальні попередження через URI
```

---

### 9️⃣ `TestReader_Encrypted` — парсинг шифрування

```go
func TestReader_Encrypted(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/encrypted.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.Equal(t, 6, p.ItemSize())
    
    // 🎯 KeyItem для AES-128 шифрування
    item := p.Items[0]
    assert.IsType(t, &m3u8.KeyItem{}, item)
    ki := item.(*m3u8.KeyItem)
    
    assert.Equal(t, "AES-128", ki.Encryptable.Method)
    assertNotNilEqual(t, "https://priv.example.com/key.php?r=52", ki.Encryptable.URI)
}
```

#### 🎯 Критичні аспекти шифрування
```
🔐 AES-128 у HLS:
• Ключ = 16 байт (128 біт), завантажується з URI
• IV (вектор ініціалізації) = 16 байт, може генеруватися з PTS
• Кожен сегмент шифрується окремо → паралельне декодування

🔄 Key rotation:
• Зміна ключа = #EXT-X-DISCONTINUITY + новий #EXT-X-KEY
• Обмежує збитки при компрометації одного ключа

🌍 Multi-DRM (через #EXT-X-SESSION-KEY):
• FairPlay для iOS: KEYFORMAT="com.apple.streamingkeydelivery"
• Widevine для Android: KEYFORMAT="urn:uuid:edef8ba9-..."
• Клієнт обирає підтримуваний формат автоматично
```

---

### 🔟 `TestReader_Map` — парсинг fMP4 init-файлу

```go
func TestReader_Map(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/mapPlaylist.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.Equal(t, 1, p.ItemSize())
    
    // 🎯 MapItem для fMP4 ініціалізації
    item := p.Items[0]
    assert.IsType(t, &m3u8.MapItem{}, item)
    mi := item.(*m3u8.MapItem)
    
    assert.Equal(t, "frelo/prog_index.m3u8", mi.URI)
    assert.NotNil(t, mi.ByteRange)  # ✅ BYTERANGE для partial fetch
    assertNotNilEqual(t, 4500, mi.ByteRange.Length)   # ✅ 4500 байт moov
    assertNotNilEqual(t, 600, mi.ByteRange.Start)     # ✅ Починаючи з 600
}
```

#### 🎯 Оптимізація через `BYTERANGE` для init-файлу
```
📹 fMP4 структура:
• [ftyp][moov][moof][mdat][moof][mdat]...
• moov box = метадані (кодеки, timescale, track IDs)
• moof/mdat = медіа-дані сегментів

🔄 Partial fetch init-файлу:
• Замість завантаження всього init.mp4 (може бути 100KB+)
• BYTERANGE="4500@600" → завантажити тільки moov (4.5KB)
• Економія трафіку ×20, швидший старт відтворення

🔗 Інтеграція з вашим проектом:
• Визначити розмір moov через ffprobe: ffprobe -show_entries format_tags=movflags init.mp4
• Створити MapItem з ByteRange для оптимізації
• Клієнти автоматично використають partial request
```

---

### 1️⃣1️⃣ `TestReader_Timestamp` — парсинг `#EXT-X-PROGRAM-DATE-TIME`

```go
func TestReader_Timestamp(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/timestampPlaylist.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.Equal(t, 6, p.ItemSize())
    
    // 🎯 Сегмент з абсолютним таймштампом
    item := p.Items[0]
    assert.IsType(t, &m3u8.SegmentItem{}, item)
    si := item.(*m3u8.SegmentItem)
    
    assert.NotNil(t, si.ProgramDateTime)  # ✅ Таймштамп присутній
    assert.Equal(t, "2016-04-11T15:24:31Z", m3u8.FormatTime(si.ProgramDateTime.Time))  # ✅ RFC3339
}
```

#### 🎯 Критичність таймштампів для CCTV
```
⏱️ Синхронізація з реальним часом:
• Клієнт розраховує: server_time - program_date_time = затримка
• Дозволяє корекцію drift у реальному часі

🔍 Пошук в архіві:
• Користувач вводить час "14:30:00" → плеєр знаходить сегмент з найближчим PROGRAM-DATE-TIME
• Без таймштампів: тільки пошук по номеру сегмента (незручно)

🔗 Інтеграція з вашим WebSocketDistributor:
• Субтитри мають start_time_utc → прив'язка до сегмента через PROGRAM-DATE-TIME
• Синхронізація аудіо/відео/субтитрів за абсолютним часом
```

---

### 1️⃣2️⃣ `TestReader_DateRange` — парсинг `#EXT-X-DATERANGE` з SCTE-35

```go
func TestReader_DateRange(t *testing.T) {
    p, err := m3u8.ReadFile("fixtures/dateRangeScte35.m3u8")
    assert.Nil(t, err)
    assert.True(t, p.IsValid())
    assert.Equal(t, 5, p.ItemSize())
    
    // 🎯 Перевірка DateRangeItem на початку та в кінці
    item := &m3u8.DateRangeItem{}  # ✅ Створення для type assertion
    assert.IsType(t, item, p.Items[0])  # ✅ Початок події
    assert.IsType(t, item, p.Items[4])  # ✅ Кінець події
}
```

#### 🎯 SCTE-35 для рекламних вставок
```
📺 Маркування рекламних блоків:
#EXT-X-DATERANGE:ID="ad-break-1",CLASS="com.alarabiya.ad",START-DATE="2024-01-15T14:30:00Z",SCTE35-OUT=0xFC002F...
#EXTINF:4.0, seg_ad_001.ts
#EXT-X-DATERANGE:ID="ad-break-1",SCTE35-IN=0xFC002F...

🔄 Автоматизація вставок:
• Плеєр розпізнає SCTE35-OUT → перемикається на рекламний потік
• SCTE35-IN → повернення до основного контенту
• Без розриву відтворення, плавний перехід

🔗 Інтеграція з вашим проектом:
• При отриманні SCTE-35 маркерів через WebSocket → додавати #EXT-X-DATERANGE
• Синхронізація з субтитрами: не показувати субтитри під час реклами
```

---

### 1️⃣3️⃣ `TestReader_Invalid` — обробка помилок

```go
func TestReader_Invalid(t *testing.T) {
    // 🎯 Спроба читання неіснуючого файлу
    _, err := m3u8.ReadFile("path/to/file")
    assert.NotNil(t, err)  # ✅ Очікуємо помилку
}
```

#### 🎯 Найкращі практики обробки помилок
```go
// ✅ ReadFile має повертати інформативну помилку:
func ReadFile(path string) (*Playlist, error) {
    content, err := os.ReadFile(path)
    if err != nil {
        // ❌ Погано: return nil, err  (неясно, що саме не так)
        // ✅ Добре:
        return nil, fmt.Errorf("failed to read playlist file %q: %w", path, err)
    }
    return ParseReader(bytes.NewReader(content))
}

// ✅ Клієнтський код має обробляти помилки:
pl, err := m3u8.ReadFile("master.m3u8")
if err != nil {
    if os.IsNotExist(err) {
        log.Error("playlist file not found", "path", "master.m3u8")
        // Спробувати fallback або сповістити адміна
    } else if errors.Is(err, m3u8.ErrParse) {
        log.Error("invalid playlist format", "error", err)
        // Спробувати відновити або відхилити
    }
    return err
}
```

---

## ⚠️ Загальні проблеми та покращення для всього файлу

### 1️⃣ Відсутність `t.Parallel()` для прискорення тестів
```go
// ✅ Додати t.Parallel() у кожен тест:
func TestReader(t *testing.T) {
    t.Parallel()  # ✅ Дозволяє паралельне виконання
    // ... код ...
}

// 📊 Ефект: 13 тестів × ~20мс кожен → 260мс послідовно → ~40мс паралельно
```

### 2️⃣ Жорсткі залежності від фікстур-файлів
```go
// ❌ ReadFile("fixtures/...") → залежить від ФС
// ✅ Використовувати embed для ізольованості:

//go:embed fixtures/*.m3u8
var testFixtures embed.FS

func TestReader(t *testing.T) {
    content, err := testFixtures.ReadFile("fixtures/master.m3u8")
    assert.NoError(t, err)
    p, err := m3u8.ParseReader(bytes.NewReader(content))  # ✅ Парсинг з memory
    assert.NoError(t, err)
    // ...
}
```

### 3️⃣ Відсутність тестів на частково невалідні файли
```go
// ✅ Додати тести на "майже валідні" плейлисти:
func TestReader_PartiallyInvalid(t *testing.T) {
    cases := []struct{
        name  string
        content string
        wantErr bool
    }{
        {"missing_extm3u", "#EXT-X-VERSION:7\n", true},  # ❌ Без #EXTM3U
        {"invalid_target_duration", "#EXTM3U\n#EXT-X-TARGETDURATION:abc\n", true},
        {"mixed_item_types", "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100,URI=x\n#EXTINF:4.0,seg.ts\n", true},
        {"valid_with_unknown_tag", "#EXTM3U\n#EXT-X-CUSTOM-TAG:value\n#EXT-X-VERSION:7\n", false},  # ✅ Невідомі теги ігноруються
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            p, err := m3u8.ParseReader(strings.NewReader(tc.content))
            if tc.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, p)
            }
        })
    }
}
```

### 4️⃣ Відсутність бенчмарків для продуктивності парсингу
```go
// ✅ Додати бенчмарк для ReadFile:
func BenchmarkReadFile_MasterPlaylist(b *testing.B) {
    content, _ := testFixtures.ReadFile("fixtures/master.m3u8")
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := m3u8.ParseReader(bytes.NewReader(content))
        if err != nil {
            b.Fatal(err)
        }
    }
}

// 🚀 Запуск: go test -bench=. -benchmem
// Результат покаже, чи потрібна оптимізація парсера
```

### 5️⃣ Thread-safety не тестується
```go
// ❌ У вашому pipeline (8x workers + WebSocket) ReadFile може викликатися конкурентно
// ✅ Додати тести на race condition:

func TestReader_Concurrent(t *testing.T) {
    var wg sync.WaitGroup
    errors := make(chan error, 10)
    
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            p, err := m3u8.ReadFile("fixtures/master.m3u8")
            if err != nil {
                errors <- err
                return
            }
            if !p.IsMaster() {
                errors <- fmt.Errorf("goroutine %d: expected Master", id)
            }
        }(i)
    }
    
    wg.Wait()
    close(errors)
    
    for err := range errors {
        t.Error(err)
    }
}

// 🚀 Запуск з race detector: go test -race -run TestReader_Concurrent
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **live-ковзним вікном** та **WebSocket-оновленнями**:

### 🎯 Сценарій: парсинг вхідного Master Playlist від зовнішнього джерела
```go
// У VideoManifestProxy при отриманні плейлиста від upstream:
func (p *VideoManifestProxy) fetchUpstreamPlaylist(url string) error {
    // 🎯 Завантаження з мережі з таймаутом
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return fmt.Errorf("failed to create request: %w", err)
    }
    
    resp, err := p.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to fetch playlist: %w", err)
    }
    defer resp.Body.Close()
    
    // 🎯 Парсинг через ReadReader (аналог ReadFile для io.Reader)
    pl, err := m3u8.ParseReader(resp.Body)
    if err != nil {
        return fmt.Errorf("failed to parse playlist: %w", err)
    }
    
    // 🎯 Валідація типу
    if !pl.IsMaster() {
        return fmt.Errorf("expected master playlist, got media")
    }
    
    // 🎯 Збереження для подальшої обробки
    p.upstreamPlaylist = pl
    return nil
}
```

### 🎯 Сценарій: фільтрація варіантів за можливостями клієнта
```go
// У WebSocketDistributor при підключенні нового клієнта:
func (d *Distributor) selectVariantsForClient(clientCaps ClientCapabilities, pl *m3u8.Playlist) []*m3u8.PlaylistItem {
    var variants []*m3u8.PlaylistItem
    
    for _, item := range pl.Items {
        pi, ok := item.(*m3u8.PlaylistItem)
        if !ok {
            continue  # ❌ Не PlaylistItem → пропускаємо
        }
        
        // 🎯 Фільтр за кодеками
        if pi.Codecs != nil && !clientCaps.SupportsCodecs(*pi.Codecs) {
            continue
        }
        
        // 🎯 Фільтр за роздільною здатністю
        if pi.Resolution != nil {
            if pi.Resolution.Width > clientCaps.MaxWidth || 
               pi.Resolution.Height > clientCaps.MaxHeight {
                continue
            }
        }
        
        // 🎯 Фільтр за бітрейтом (для мобільних мереж)
        if pi.Bandwidth > clientCaps.MaxBandwidth {
            continue
        }
        
        variants = append(variants, pi)
    }
    
    return variants
}
```

### 🎯 Сценарій: моніторинг валідності плейлистів у реальному часі
```go
// У monitoring.Monitor для виявлення аномалій:
func (m *Monitor) validatePlaylist(pl *m3u8.Playlist, source string) []string {
    var warnings []string
    
    // 🎯 Перевірка базової валідності
    if !pl.IsValid() {
        warnings = append(warnings, "mixed item types (master+media)")
    }
    
    // 🎯 Перевірка TargetDuration для Media Playlist
    if !pl.IsMaster() {
        for _, item := range pl.Items {
            if seg, ok := item.(*m3u8.SegmentItem); ok {
                if seg.Duration > float64(pl.Target) {
                    warnings = append(warnings, 
                        fmt.Sprintf("segment %.3fs > TARGETDURATION %ds", 
                            seg.Duration, pl.Target))
                }
            }
        }
    }
    
    // 🎯 Перевірка наявності #EXT-X-MAP для fMP4
    if !pl.IsMaster() && pl.UsesFMP4 {  # ✅ Припустимо, є прапорець
        hasMap := false
        for _, item := range pl.Items {
            if _, ok := item.(*m3u8.MapItem); ok {
                hasMap = true
                break
            }
        }
        if !hasMap {
            warnings = append(warnings, "fMP4 playlist missing #EXT-X-MAP")
        }
    }
    
    // 🎯 Логування попереджень
    for _, w := range warnings {
        m.logger.Warn("playlist validation warning", 
            "source", source, "warning", w)
    }
    
    return warnings
}
```

---

## 🧪 Приклад: розширений набір тестів для `Reader`

```go
// ✅ Додати комплексні тести з subtests та валідацією:
func TestReader_Comprehensive(t *testing.T) {
    t.Parallel()
    
    t.Run("Master/WithAllFeatures", func(t *testing.T) {
        t.Parallel()
        p, err := m3u8.ReadFile("fixtures/master.m3u8")
        assert.NoError(t, err)
        assert.True(t, p.IsMaster())
        assert.Equal(t, 8, p.ItemSize())
        
        // 🎯 Перевірка порядку елементів
        assert.IsType(t, &m3u8.SessionKeyItem{}, p.Items[0])   # ✅ Шифрування першим
        assert.IsType(t, &m3u8.PlaybackStart{}, p.Items[1])    # ✅ Потім точка старту
        assert.IsType(t, &m3u8.PlaylistItem{}, p.Items[2])     # ✅ Потім варіанти
    })
    
    t.Run("Media/Live/WithoutEndList", func(t *testing.T) {
        t.Parallel()
        p, err := m3u8.ReadFile("fixtures/playlist-live.m3u8")
        assert.NoError(t, err)
        assert.False(t, p.IsMaster())
        assert.True(t, p.IsLive())  # ✅ Live = немає #EXT-X-ENDLIST
        
        // 🎯 Перевірка, що сегменти мають монотонні номери
        segments := p.Segments()
        for i := 1; i < len(segments); i++ {
            // 🎯 У реальному коді: перевірка URI або коментарів на послідовність
        }
    })
    
    t.Run("Media/VOD/WithEndList", func(t *testing.T) {
        t.Parallel()
        p, err := m3u8.ReadFile("fixtures/playlist.m3u8")
        assert.NoError(t, err)
        assert.False(t, p.IsMaster())
        assert.False(t, p.IsLive())  # ✅ VOD = є #EXT-X-ENDLIST
        assertNotNilEqual(t, "VOD", p.Type)
    })
    
    t.Run("Error/FileNotFound", func(t *testing.T) {
        t.Parallel()
        _, err := m3u8.ReadFile("nonexistent.m3u8")
        assert.Error(t, err)
        assert.True(t, os.IsNotExist(err) || strings.Contains(err.Error(), "no such file"))
    })
    
    t.Run("Error/InvalidFormat", func(t *testing.T) {
        t.Parallel()
        content := "#EXTM3U\n#EXT-X-INVALID-TAG:value\n"
        p, err := m3u8.ParseReader(strings.NewReader(content))
        # ✅ Невідомі теги мають ігноруватися, не викликати помилку
        assert.NoError(t, err)
        assert.NotNil(t, p)
    })
    
    t.Run("Concurrency/MultipleReads", func(t *testing.T) {
        t.Parallel()
        var wg sync.WaitGroup
        results := make(chan *m3u8.Playlist, 10)
        
        for i := 0; i < 10; i++ {
            wg.Add(1)
            go func() {
                defer wg.Done()
                p, err := m3u8.ReadFile("fixtures/master.m3u8")
                if err == nil {
                    results <- p
                }
            }()
        }
        
        wg.Wait()
        close(results)
        
        count := 0
        for range results {
            count++
        }
        assert.Equal(t, 10, count)  # ✅ Всі горутини успішно завершилися
    })
}
```

---

## 📋 Специфікація HLS (RFC 8216) — критичні вимоги до парсингу

```
✅ #EXTM3U — перший рядок, без пробілів, чутливий до регістру
✅ Кожен тег на окремому рядку, закінчується \n (не \r\n)
✅ Атрибути в тегах: KEY=VALUE або KEY="VALUE", розділені комами
✅ Рядкові значення в лапках: лапки всередині мають бути екрановані \`\"\`
✅ Числові значення: без лапок, десятковий формат для float
✅ Опціональні атрибути: не виводити/не парсити, якщо відсутні
✅ Невідомі теги/атрибути: клієнти МАЮТЬ ігнорувати (forward compatibility)
✅ Порядок тегів у заголовку: не регламентований, але рекомендується логічний
✅ #EXT-X-ENDLIST: тільки для VOD, ніколи для live/master
✅ Коментарі в #EXTINF: можуть містити коми → використовувати SplitN(2)
✅ BYTERANGE формат: "N[@O]" де N=довжина, O=зміщення (опціонально)
✅ TIME-OFFSET: додатне = від початку, від'ємне = від кінця плейлиста
✅ PRECISE: YES/NO, чутливо до регістру
✅ CODECS формат: RFC 6381, у лапках, розділені комами
✅ CLOSED-CAPTIONS=NONE: без лапок, спеціальний випадок
```

---

## 🎯 Висновок

Ці тести — **потужна інтеграційна основа** для валідації парсингу M3U8:

✅ Покриття всіх основних типів тегів та атрибутів  
✅ Перевірка поліморфізму через `[]Item` + type assertion  
✅ Інтеграція з реальною файловою системою (fixtures)  
✅ Обробка помилок та крайніх випадків

**Для вашого проекту — критичні рекомендації**:

1. ✅ Замінити `ReadFile` на `embed` + `ParseReader` для ізольованості тестів
2. ✅ Додати `t.Parallel()` для прискорення прогону тестів
3. ✅ Додати тести на частково невалідні плейлисти (forward compatibility)
4. ✅ Додати бенчмарки для оцінки продуктивності парсингу
5. ✅ Додати тести на конкурентний доступ (`-race` flag)

**Приклад оптимізації для CCTV high-load сценаріїв**:
```go
// Для швидкого парсингу великих плейлистів:
type CachedReader struct {
    mu sync.RWMutex
    cache map[string]*Playlist  # ✅ Кеш за URL/шляхом
    ttl   time.Duration
}

func (cr *CachedReader) ReadFile(path string) (*m3u8.Playlist, error) {
    // 🎯 Перевірка кешу
    cr.mu.RLock()
    if cached, ok := cr.cache[path]; ok {
        // 🎯 Перевірка TTL
        if time.Since(cached.lastUpdated) < cr.ttl {
            defer cr.mu.RUnlock()
            return cached.playlist, nil
        }
    }
    cr.mu.RUnlock()
    
    // 🎯 Парсинг з файлу
    pl, err := m3u8.ReadFile(path)
    if err != nil {
        return nil, err
    }
    
    // 🎯 Оновлення кешу
    cr.mu.Lock()
    cr.cache[path] = &CachedEntry{
        playlist: pl,
        lastUpdated: time.Now(),
    }
    cr.mu.Unlock()
    
    return pl, nil
}
// → При 1000 запитах/сек: 99% hit rate → парсинг тільки 10 раз/сек
```

Потрібно допомогти з:
- 🔗 Інтеграцією `embed` для фікстур у ваші тести?
- 🧠 Реалізацією кешування парсингу для high-load сценаріїв?
- 🧪 Написанням fuzz-тестів для пошуку крайніх випадків у парсері?

Чекаю на ваші питання! 🛠️📋🎬