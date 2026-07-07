# Глибоке роз'яснення: `suggest.Stanza()` методи — генерація HLS playlist тегів

Цей файл містить **методи для генерації EXT-X-STREAM-INF та EXT-X-MEDIA тегів** — фундаментальних елементів HLS master playlist. Ці теги повідомляють плеєру про доступні варіанти відео, аудіо та субтитрів, їхні параметри та метадані.

---

## 🎯 Навіщо ці методи потрібні у вашому CCTV HLS пайплайні?

```
┌─────────────────────────────────────────┐
│ Stanza methods у контексті HLS:        │
│                                         │
│ 🔹 Формування master playlist:         │
│   • EXT-X-STREAM-INF: опис відео-варіантів│
│   • EXT-X-MEDIA: опис аудіо/субтитрів  │
│   • Автоматичний вибір якості плеєром  │
│                                         │
│ 🔹 Підтримка адаптивного стрімінгу:    │
│   • BANDWIDTH, RESOLUTION для ABR      │
│   • AUDIO/SUBTITLES groups для мультимовності│
│   • CHARACTERISTICS для accessibility  │
│                                         │
│ 🔹 Для CCTV:                            │
│   • Чітка ідентифікація потоків у плеєрі│
│   • Підтримка багатомовних інтерфейсів │
│   • Accessibility для слабозорих/глухих│
└─────────────────────────────────────────┘
```

---

## 🔧 Метод `VideoVariant.Stanza()`: генерація #EXT-X-STREAM-INF

```go
func (v VideoVariant) Stanza(streamPlaylistFilename string, audioGroup *string, subtitleGroup *string) string {
    // 🔹 Список опцій для тега
    var optionsList []string
    
    // 🔹 Обов'язкові атрибути
    optionsList = append(optionsList,
        fmt.Sprintf("BANDWIDTH=%v", v.Bandwidth),      // 🎯 Бітрейт у бітах/сек
        fmt.Sprintf("RESOLUTION=%v", v.Resolution))     // 🎯 Роздільна здатність WxH
    
    // 🔹 Опціональні групи
    if audioGroup != nil && len(*audioGroup) > 0 {
        optionsList = append(optionsList,
            fmt.Sprintf("AUDIO=\"%v\"", *audioGroup))   // 🎯 Група аудіо
    }
    if subtitleGroup != nil && len(*subtitleGroup) > 0 {
        optionsList = append(optionsList,
            fmt.Sprintf("SUBTITLES=\"%v\"", *subtitleGroup)) // 🎯 Група субтитрів
    }
    
    // 🔹 Формування фінального рядка
    // TODO: Add CODECS  // ⚠️ Важливо: додати CODECS для кращої сумісності!
    
    return fmt.Sprintf("#EXT-X-STREAM-INF:%v\n%v",
        strings.Join(optionsList, ","),  // опції через кому
        streamPlaylistFilename)           // URI медіа-плейлиста
}
```

### 🎯 Приклад виходу:

```
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1280x720,AUDIO="audio",SUBTITLES="subs"
channel1_0.m3u8
```

### 🔹 Ключові атрибути за специфікацією:

| Атрибут | Обов'язковий? | Призначення | Приклад |
|---------|--------------|-------------|---------|
| `BANDWIDTH` | ✅ Так | Середній бітрейт у бітах/сек для вибору якості | `2000000` |
| `RESOLUTION` | ⚠️ Рекомендовано | Роздільна здатність WxH для фільтрації пристроїв | `1280x720` |
| `CODECS` | ⚠️ Рекомендовано | Список кодеків для сумісності | `"avc1.42e01e,mp4a.40.2"` |
| `AUDIO` | ❌ Ні | Ім'я групи аудіо для прив'язки | `"audio"` |
| `SUBTITLES` | ❌ Ні | Ім'я групи субтитрів | `"subs"` |

> ⚠️ **Важливо**: Відсутність `CODECS` може призвести до проблем сумісності з деякими плеєрами (напр., Safari на iOS). Рекомендується додати.

---

## 🔧 Метод `AudioVariant.Stanza()`: генерація #EXT-X-MEDIA для аудіо

```go
func (v AudioVariant) Stanza(streamPlaylistFilename string) string {
    var optionsList []string
    
    // 🔹 Група аудіо (за замовчуванням "audio")
    groupID := DefaultAudioGroupID
    if v.GroupID != nil {
        groupID = *v.GroupID
    }
    
    // 🔹 Обов'язкові атрибути
    optionsList = append(optionsList,
        "TYPE=AUDIO",                              // 🎯 Тип: AUDIO
        "AUTOSELECT=YES",                          // 🎯 Автовибір, якщо мова співпадає з налаштуваннями
        fmt.Sprintf("GROUP-ID=\"%v\"", groupID),   // 🎯 Група для прив'язки до відео
        fmt.Sprintf("NAME=\"%v\"", v.Name))        // 🎯 Назва для вибору у плеєрі
    
    // 🔹 Кількість каналів
    switch v.Type {
    case SurroundSound, StereoSound:
        optionsList = append(optionsList, fmt.Sprintf("CHANNELS=\"%d\"", v.Type))
    default:
        log.Println("WARNING: Unknown number of channels")
    }
    
    // 🔹 Мова (RFC 5646)
    if v.Language != input.Unknown {
        optionsList = append(optionsList,
            fmt.Sprintf("LANGUAGE=\"%v\"", v.Language))
    }
    
    // 🔹 Accessibility характеристики
    var characteristics []string
    if v.DescribesVideo != nil && *v.DescribesVideo {
        characteristics = append(characteristics, "public.accessibility.describes-video")
    }
    if len(characteristics) > 0 {
        optionsList = append(optionsList,
            fmt.Sprintf("CHARACTERISTICS=\"%v\"", strings.Join(characteristics, ",")))
    }
    
    // 🔹 URI медіа-плейлиста
    optionsList = append(optionsList,
        fmt.Sprintf("URI=\"%v\"", streamPlaylistFilename))
    
    return "#EXT-X-MEDIA:" + strings.Join(optionsList, ",")
}
```

### 🎯 Приклад виходу:

```
#EXT-X-MEDIA:TYPE=AUDIO,AUTOSELECT=YES,GROUP-ID="audio",NAME="English AAC Stereo",CHANNELS="2",LANGUAGE="eng",URI="channel1_1.m3u8"
```

### 🔹 Ключові атрибути за специфікацією:

| Атрибут | Обов'язковий? | Призначення | Приклад |
|---------|--------------|-------------|---------|
| `TYPE` | ✅ Так | Тип медіа: `AUDIO` | `AUDIO` |
| `GROUP-ID` | ✅ Так | Ідентифікатор групи для прив'язки | `"audio"` |
| `NAME` | ✅ Так | Назва для відображення у плеєрі | `"English AAC Stereo"` |
| `URI` | ⚠️ Якщо не DEFAULT | Шлях до медіа-плейлиста | `"channel1_1.m3u8"` |
| `LANGUAGE` | ❌ Ні | Мова за RFC 5646 для автовибору | `"eng"`, `"ukr"` |
| `AUTOSELECT` | ❌ Ні | Чи вибирати автоматично за мовою | `YES` |
| `CHANNELS` | ❌ Ні | Кількість аудіо-каналів | `"2"`, `"6"` |
| `CHARACTERISTICS` | ❌ Ні | Accessibility теги для слабозорих/глухих | `"public.accessibility.describes-video"` |

> 💡 **Порада**: Для мультимовного контенту обов'язково вказуйте `LANGUAGE` — це дозволяє плеєру автоматично вибирати доріжку за налаштуваннями користувача.

---

## 🔧 Метод `SubtitleVariant.Stanza()`: генерація #EXT-X-MEDIA для субтитрів

```go
func (v SubtitleVariant) Stanza() string {
    streamPlaylistFilename := v.PlaylistName("")  // 🔹 Генерує ім'я файлу
    
    var optionsList []string
    
    // 🔹 Група субтитрів (за замовчуванням "subs")
    groupID := DefaultSubtitlesGroupID
    if v.GroupID != nil {
        groupID = *v.GroupID
    }
    
    // 🔹 Обов'язкові атрибути
    optionsList = append(optionsList,
        "TYPE=SUBTITLES",                        // 🎯 Тип: SUBTITLES
        "AUTOSELECT=YES",                        // 🎯 Автовибір
        fmt.Sprintf("GROUP-ID=\"%v\"", groupID), // 🎯 Група
        fmt.Sprintf("NAME=\"%v\"", v.Name))      // 🎯 Назва
    
    // 🔹 Мова
    if v.Language != input.Unknown {
        optionsList = append(optionsList,
            fmt.Sprintf("LANGUAGE=\"%v\"", v.Language))
    }
    
    // 🔹 FORCED: чи показувати субтитри автоматично для іноземних діалогів
    forced := "NO"
    if v.Forced {
        forced = "YES"
    }
    optionsList = append(optionsList,
        fmt.Sprintf("FORCED=%v", forced))
    
    // 🔹 Accessibility характеристики
    var characteristics []string
    if v.HearingImpaired {
        characteristics = append(characteristics,
            "public.accessibility.transcribes-spoken-dialog",
            "public.accessibility.describes-music-and-sound")
    }
    if len(characteristics) > 0 {
        optionsList = append(optionsList,
            fmt.Sprintf("CHARACTERISTICS=\"%v\"", strings.Join(characteristics, ",")))
    }
    
    // 🔹 URI
    optionsList = append(optionsList,
        fmt.Sprintf("URI=\"%v\"", streamPlaylistFilename))
    
    return "#EXT-X-MEDIA:" + strings.Join(optionsList, ",")
}
```

### 🎯 Приклад виходу:

```
#EXT-X-MEDIA:TYPE=SUBTITLES,AUTOSELECT=YES,GROUP-ID="subs",NAME="English Subtitles",LANGUAGE="eng",FORCED=NO,CHARACTERISTICS="public.accessibility.transcribes-spoken-dialog,public.accessibility.describes-music-and-sound",URI="channel1_subs_eng.m3u8"
```

### 🔹 Ключові атрибути для субтитрів:

| Атрибут | Обов'язковий? | Призначення | Приклад |
|---------|--------------|-------------|---------|
| `TYPE` | ✅ Так | Тип: `SUBTITLES` | `SUBTITLES` |
| `GROUP-ID` | ✅ Так | Група для прив'язки | `"subs"` |
| `NAME` | ✅ Так | Назва для вибору | `"English Subtitles"` |
| `URI` | ⚠️ Якщо не DEFAULT | Шлях до WebVTT плейлиста | `"channel1_subs_eng.m3u8"` |
| `LANGUAGE` | ❌ Ні | Мова субтитрів | `"eng"`, `"ukr"` |
| `FORCED` | ❌ Ні | Чи показувати автоматично для іноземної мови | `YES`, `NO` |
| `CHARACTERISTICS` | ❌ Ні | Accessibility теги для слабозорих/глухих | `"public.accessibility.transcribes-spoken-dialog"` |

> 💡 **Порада**: `FORCED=YES` використовується для субтитрів, які мають показуватися автоматично, коли в аудіо є іноземна мова (напр., російські репліки в англомовному фільмі).

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1: Побудова master playlist з варіантами

```go
// У converter/converter.go — генерація master playlist:
func buildMasterPlaylist(videoVars []suggest.VideoVariant, 
                        audioVars []suggest.AudioVariant,
                        subtitleVars []suggest.SubtitleVariant,
                        outputDir string) error {
    
    f, err := os.Create(filepath.Join(outputDir, "master.m3u8"))
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 Заголовок
    f.WriteString("#EXTM3U\n")
    f.WriteString("#EXT-X-VERSION:7\n")  // ✅ Версія 7 підтримує fMP4, субтитри...
    
    // 🔹 Аудіо групи
    audioGroupID := "audio"
    for _, av := range audioVars {
        f.WriteString(av.Stanza(playlistFilenameForAudio(av)) + "\n")
    }
    if len(audioVars) > 0 { f.WriteString("\n") }
    
    // 🔹 Субтитри групи
    subtitleGroupID := "subs"
    for _, sv := range subtitleVars {
        f.WriteString(sv.Stanza() + "\n")
    }
    if len(subtitleVars) > 0 { f.WriteString("\n") }
    
    // 🔹 Відео варіанти (посилання на медіа-плейлисти)
    for i, vv := range videoVars {
        f.WriteString(vv.Stanza(
            playlistFilenameForVideo(i),
            &audioGroupID,
            &subtitleGroupID,
        ) + "\n")
    }
    
    return nil
}

func playlistFilenameForVideo(index int) string {
    return fmt.Sprintf("stream_%d.m3u8", index)
}

func playlistFilenameForAudio(av suggest.AudioVariant) string {
    return fmt.Sprintf("audio_%s.m3u8", strings.ToLower(av.Language.String()))
}
```

### ✅ 2: Валідація згенерованих тегів

```go
// Перевірити, що теги валідні перед записом:
func validateStanza(tag string, tagType string) error {
    // 🔹 Перевірити обов'язкові атрибути
    required := map[string][]string{
        "#EXT-X-STREAM-INF": {"BANDWIDTH", "RESOLUTION"},
        "#EXT-X-MEDIA":      {"TYPE", "GROUP-ID", "NAME"},
    }
    
    for _, attr := range required[tagType] {
        if !strings.Contains(tag, attr+"=") {
            return fmt.Errorf("missing required attribute %s in %s", attr, tagType)
        }
    }
    
    // 🔹 Перевірити формат BANDWIDTH (число)
    if tagType == "#EXT-X-STREAM-INF" {
        if !regexp.MustCompile(`BANDWIDTH=\d+`).MatchString(tag) {
            return fmt.Errorf("invalid BANDWIDTH format in %s", tag)
        }
    }
    
    return nil
}
```

### ✅ 3: Підтримка CODECS для кращої сумісності

```go
// 🔹 Додати CODECS у VideoVariant.Stanza():
func (v VideoVariant) Stanza(streamPlaylistFilename string, audioGroup *string, subtitleGroup *string) string {
    var optionsList []string
    
    optionsList = append(optionsList,
        fmt.Sprintf("BANDWIDTH=%v", v.Bandwidth),
        fmt.Sprintf("RESOLUTION=%v", v.Resolution))
    
    // 🔹 Додати CODECS (якщо вказано)
    if v.Codecs != "" {
        optionsList = append(optionsList, fmt.Sprintf("CODECS=\"%v\"", v.Codecs))
    }
    
    // ... решта логіки ...
}

// 🔹 Приклад значень Codecs:
// H.264 + AAC: "avc1.42e01e,mp4a.40.2"
// HEVC + AAC:  "hvc1.1.6.L120.90,mp4a.40.2"
// H.264 + AC3: "avc1.42e01e,ac-3"
```

### ✅ 4: Локалізація назв для міжнародних користувачів

```go
// 🔹 Додати перекладені назви для аудіо/субтитрів:
type LocalizedNames map[input.Language]string

func (v AudioVariant) StanzaWithLocales(streamPlaylistFilename string, locales LocalizedNames) string {
    // 🔹 Використовувати назву для поточної мови плеєра
    name := v.Name
    if localized, ok := locales[v.Language]; ok {
        name = localized
    }
    
    // 🔹 Замінити NAME у optionsList
    // ... реалізація ...
    
    return "#EXT-X-MEDIA:" + strings.Join(optionsList, ",")
}

// Використання:
locales := LocalizedNames{
    input.English: "English Audio",
    input.Ukrainian: "Українське аудіо",
    input.French: "Audio français",
}
```

---

## 🧪 Тестування: стратегії валідації

### 🔹 Тест на генерацію EXT-X-STREAM-INF

```go
func TestVideoVariant_Stanza(t *testing.T) {
    variant := suggest.VideoVariant{
        Bandwidth:  2000000,
        Resolution: "1280x720",
        Codecs:     "avc1.42e01e,mp4a.40.2",  // 🔹 Додано для тесту
    }
    
    audioGroup := "audio"
    subtitleGroup := "subs"
    
    result := variant.Stanza("stream_0.m3u8", &audioGroup, &subtitleGroup)
    
    // 🔹 Перевірити структуру
    assert.Contains(t, result, "#EXT-X-STREAM-INF:")
    assert.Contains(t, result, "BANDWIDTH=2000000")
    assert.Contains(t, result, "RESOLUTION=1280x720")
    assert.Contains(t, result, `AUDIO="audio"`)
    assert.Contains(t, result, `SUBTITLES="subs"`)
    assert.Contains(t, result, "stream_0.m3u8")
    
    // 🔹 Перевірити порядок: теги перед URI
    lines := strings.Split(result, "\n")
    assert.Equal(t, 2, len(lines))
    assert.True(t, strings.HasPrefix(lines[0], "#EXT-X-STREAM-INF:"))
    assert.Equal(t, "stream_0.m3u8", lines[1])
}
```

### 🔹 Тест на EXT-X-MEDIA для аудіо

```go
func TestAudioVariant_Stanza(t *testing.T) {
    variant := suggest.AudioVariant{
        Type:            suggest.StereoSound,
        GroupID:         stringPtr("audio"),
        Name:            "English AAC Stereo",
        Language:        input.English,
        ConvertToStereo: false,
    }
    
    result := variant.Stanza("audio_eng.m3u8")
    
    // 🔹 Перевірити обов'язкові атрибути
    assert.Contains(t, result, "#EXT-X-MEDIA:")
    assert.Contains(t, result, "TYPE=AUDIO")
    assert.Contains(t, result, `GROUP-ID="audio"`)
    assert.Contains(t, result, `NAME="English AAC Stereo"`)
    assert.Contains(t, result, `LANGUAGE="eng"`)
    assert.Contains(t, result, `CHANNELS="2"`)
    assert.Contains(t, result, `URI="audio_eng.m3u8"`)
    
    // 🔹 Перевірити AUTOSELECT
    assert.Contains(t, result, "AUTOSELECT=YES")
}

func stringPtr(s string) *string { return &s }
```

### 🔹 Тест на FORCED субтитри

```go
func TestSubtitleVariant_Stanza_Forced(t *testing.T) {
    variant := suggest.SubtitleVariant{
        GroupID:        stringPtr("subs"),
        Name:           "English Subtitles",
        Language:       input.English,
        Forced:         true,  // 🔹 FORCED = YES
        HearingImpaired: false,
    }
    
    result := variant.Stanza()
    
    assert.Contains(t, result, "FORCED=YES")
    assert.NotContains(t, result, "CHARACTERISTICS")  // 🔹 Немає accessibility, бо HearingImpaired=false
}

func TestSubtitleVariant_Stanza_Accessibility(t *testing.T) {
    variant := suggest.SubtitleVariant{
        Name:           "English SDH",
        Language:       input.English,
        HearingImpaired: true,  // 🔹 Accessibility теги
    }
    
    result := variant.Stanza()
    
    assert.Contains(t, result, "CHARACTERISTICS=")
    assert.Contains(t, result, "public.accessibility.transcribes-spoken-dialog")
    assert.Contains(t, result, "public.accessibility.describes-music-and-sound")
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Відсутність `CODECS` | Плеєри не розпізнають формат, помилки відтворення | 🔹 Додати поле `Codecs` у `VideoVariant` та включити у `Stanza()` |
| Неправильний формат `CHANNELS` | Помилка валідації плеєром | 🔹 Перевірити, що `v.Type` (2 або 6) конвертується у строку `"2"`/`"6"` |
| `LANGUAGE` не у форматі RFC 5646 | Автовибір мови не працює | 🔹 Перевірити, що `input.Language` реалізує `String()` як `"eng"`, `"ukr"`, а не `"English"` |
| `URI` з неправильним шляхом | 404 помилки при завантаженні плейлиста | 🔹 Перевірити, що `streamPlaylistFilename` — це відносний шлях від master playlist |
| Дублювання `GROUP-ID` | Плутанина у плеєрі, неправильне групування | 🔹 Використовувати однаковий `GroupID` для всіх варіантів однієї групи |

### Приклад коректного формату мови:

```go
// ❌ Неправильно:
language := "English"  // не розпізнається плеєром

// ✅ Правильно (RFC 5646):
language := "eng"      // ISO 639-2/B
// або
language := "en"       // ISO 639-1

// Реалізація у input.Language:
func (l Language) String() string {
    switch l {
    case English: return "eng"
    case Ukrainian: return "ukr"
    case French: return "fra"
    // ...
    default: return "und"  // undefined
    }
}
```

---

## 📦 Швидкий референс для вашого коду

```go
// 1: Генерація master playlist:
func writeMasterPlaylist(outputDir string, videoVars []suggest.VideoVariant, 
                        audioVars []suggest.AudioVariant, 
                        subtitleVars []suggest.SubtitleVariant) error {
    
    path := filepath.Join(outputDir, "master.m3u8")
    f, err := os.Create(path)
    if err != nil { return err }
    defer f.Close()
    
    f.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
    
    // Аудіо
    audioGroup := "audio"
    for _, av := range audioVars {
        f.WriteString(av.Stanza(audioPlaylistName(av)) + "\n")
    }
    
    // Субтитри
    subtitleGroup := "subs"
    for _, sv := range subtitleVars {
        f.WriteString(sv.Stanza() + "\n")
    }
    
    // Відео
    for i, vv := range videoVars {
        f.WriteString(vv.Stanza(
            fmt.Sprintf("stream_%d.m3u8", i),
            &audioGroup, &subtitleGroup) + "\n")
    }
    
    return nil
}

// 2: Валідація тегів перед записом:
func validateHLSStanzas(videoVars []suggest.VideoVariant, 
                       audioVars []suggest.AudioVariant,
                       subtitleVars []suggest.SubtitleVariant) error {
    
    for i, vv := range videoVars {
        tag := vv.Stanza("", nil, nil)
        if err := validateTag(tag, "#EXT-X-STREAM-INF"); err != nil {
            return fmt.Errorf("video variant %d: %w", i, err)
        }
    }
    
    for i, av := range audioVars {
        tag := av.Stanza("")
        if err := validateTag(tag, "#EXT-X-MEDIA"); err != nil {
            return fmt.Errorf("audio variant %d: %w", i, err)
        }
    }
    
    // ... аналогічно для субтитрів ...
    
    return nil
}

// 3: Helper для форматування CODECS:
func formatCodecs(videoCodec, audioCodec string) string {
    codecMap := map[string]string{
        "h264": "avc1.42e01e",
        "hevc": "hvc1.1.6.L120.90",
        "aac":  "mp4a.40.2",
        "ac3":  "ac-3",
        "eac3": "ec-3",
    }
    
    var codecs []string
    if c, ok := codecMap[videoCodec]; ok {
        codecs = append(codecs, c)
    }
    if c, ok := codecMap[audioCodec]; ok {
        codecs = append(codecs, c)
    }
    
    return strings.Join(codecs, ",")
}

// 4: Логування згенерованих тегів для відладки:
func logGeneratedStanzas(videoVars []suggest.VideoVariant, 
                        audioVars []suggest.AudioVariant,
                        subtitleVars []suggest.SubtitleVariant) {
    
    log.Debug("Generated HLS stanzas:")
    
    for _, vv := range videoVars {
        log.Debugf("  VIDEO: %s", vv.Stanza("stream_X.m3u8", nil, nil))
    }
    for _, av := range audioVars {
        log.Debugf("  AUDIO: %s", av.Stanza("audio_X.m3u8"))
    }
    for _, sv := range subtitleVars {
        log.Debugf("  SUBS:  %s", sv.Stanza())
    }
}
```

---

## 📊 Матриця атрибутів для різних типів контенту

```
Тип контенту          | Обов'язкові атрибути                     | Рекомендовані додатково
──────────────────────┼──────────────────────────────────────────┼─────────────────────────
Основне відео         | BANDWIDTH, RESOLUTION                   | CODECS, AUDIO, SUBTITLES
Альтернативне відео   | BANDWIDTH, RESOLUTION, AUDIO            | CODECS, SUBTITLES
Аудіо-доріжка         | TYPE, GROUP-ID, NAME, URI               | LANGUAGE, AUTOSELECT, CHANNELS
Субтитри              | TYPE, GROUP-ID, NAME, URI               | LANGUAGE, FORCED, CHARACTERISTICS
Accessibility субтитри| TYPE, GROUP-ID, NAME, URI, CHARACTERISTICS| LANGUAGE, FORCED, AUTOSELECT
```

---

## 📚 Корисні посилання

- [HLS RFC Draft: EXT-X-STREAM-INF](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.4.2)
- [HLS RFC Draft: EXT-X-MEDIA](https://datatracker.ietf.org/doc/html/draft-pantos-hls-rfc8216bis#section-4.3.4.1)
- [RFC 5646: Language Tags](https://datatracker.ietf.org/doc/html/rfc5646)
- [Apple HLS Authoring Specification](https://developer.apple.com/documentation/http_live_streaming/hls_authoring_specification_for_apple_devices)

> 💡 **Ключова ідея**: Ці `Stanza()` методи — це "перекладачі" між вашою бізнес-логікою та стандартом HLS. Вони:
> - 🎯 Автоматично формують валідні теги за специфікацією
> - 🔧 Підтримують розширення (CODECS, CHARACTERISTICS) для кращої сумісності
> - 🌍 Дозволяють багатомовність через мовні теги та групування
> - ♿ Забезпечують accessibility через спеціальні характеристики

Якщо потрібно — можу допомогти:
- 🔄 Додати підтримку `CODECS` у `VideoVariant.Stanza()` для кращої сумісності з плеєрами
- 🧪 Написати integration-тест для перевірки валідності згенерованого master playlist у реальних плеєрах (hls.js, AVPlayer, VLC)
- 📈 Додати логування згенерованих тегів для відладки проблем з відтворенням

🛠️