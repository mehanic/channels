# 🛠️ Глибокий розбір: CLI інструмент astisub (main.go)

Цей файл — **командно-рядковий інтерфейс (CLI)** для бібліотеки `astisub`. Він демонструє практичне використання всіх ключових функцій бібліотеки для маніпуляції субтитрами. Розберемо архітектуру, команди та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема CLI

```
┌────────────────────────────────────────┐
│ 🎯 astisub CLI — основні команди        │
├────────────────────────────────────────┤
│                                         │
│ 📥 Вхід:                                │
│ • -i path1 [path2]  — вхідні файли     │
│ • -p N              — Teletext page    │
│ • -o path           — вихідний файл    │
│                                         │
│ ⚙️  Команди:                            │
│ • convert           — конвертація форматів │
│ • sync -s DURATION  — зсув таймінгів   │
│ • fragment -f DUR   — розбиття на фрагменти │
│ • unfragment        — злиття фрагментів │
│ • merge             — об'єднання двох файлів │
│ • optimize          — видалення невикористаних стилів │
│ • apply-linear-correction — масштабування часу │
│                                         │
│ 📤 Вихід:                               │
│ • Авто-детект формату за розширенням   │
│ • Підтримка: SRT, VTT, SSA, STL, Teletext │
│                                         │
└────────────────────────────────────────┘
```

---

## ⚙️ Детальний розбір команд

### 1️⃣ **convert** — конвертація між форматами

```bash
# SRT → WebVTT
astisub convert -i input.srt -o output.vtt

# Teletext (TS) → SRT
astisub convert -i stream.ts -p 888 -o subtitles.srt

# SSA → TTML
astisub convert -i anime.ass -o subtitles.ttml
```

### ✅ Ваш use-case: підготовка субтитрів для HLS

```go
// У вашому pipeline: конвертація будь-якого вхідного формату у WebVTT
func (p *SubtitleConverter) ConvertToWebVTT(inputData []byte, inputFormat string) ([]byte, error) {
    // 1. Створюємо тимчасовий файл або використовуємо bytes.Reader
    // (astisub.Open вимагає filename, тому для stream потрібен файл)
    tmpFile, err := os.CreateTemp("", "subtitles.*." + inputFormat)
    if err != nil {
        return nil, err
    }
    defer os.Remove(tmpFile.Name())
    
    if _, err := tmpFile.Write(inputData); err != nil {
        return nil, err
    }
    tmpFile.Close()
    
    // 2. Відкриваємо через astisub.Open
    subs, err := astisub.Open(astisub.Options{Filename: tmpFile.Name()})
    if err != nil {
        return nil, fmt.Errorf("parse %s: %w", inputFormat, err)
    }
    
    // 3. Експортуємо у WebVTT
    var buf bytes.Buffer
    if err := subs.WriteToWebVTT(&buf); err != nil {
        return nil, fmt.Errorf("write webvtt: %w", err)
    }
    
    return buf.Bytes(), nil
}
```

> 💡 **Порада**: Для real-time обробки уникайте тимчасових файлів — використовуйте `ReadFromSRT()`, `ReadFromWebVTT()` тощо напряму з `bytes.Reader`.

---

### 2️⃣ **sync -s DURATION** — зсув таймінгів

```bash
# +2 секунди до всіх субтитрів
astisub sync -s 2s -i input.srt -o output.srt

# -500ms (виправлення випередження)
astisub sync -s -500ms -i input.vtt -o output.vtt
```

### ✅ Ваш use-case: корекція дрейфу між аудіо/відео

```go
// У VideoManifestProxy: виправлення розбіжності між аудіо та відео таймінгами
func (p *VideoManifestProxy) correctSubtitleDrift(subs *astisub.Subtitles, drift time.Duration) {
    // Add() автоматично:
    // • Зсуває всі StartAt/EndAt
    // • Видаляє субтитри, що стали повністю від'ємними
    // • Обрізає початок до 0 якщо StartAt < 0
    subs.Add(drift)
    
    // Логування для моніторингу
    if drift != 0 {
        log.Info("subtitle drift corrected", "drift_ms", drift.Milliseconds(), "channel", p.channelID)
        monitoring.SubtitleDriftCorrected.Inc()
    }
}

// Визначення drift на основі PTS розбіжності
func (p *VideoManifestProxy) calculateDrift(audioPTS, videoPTS uint64) time.Duration {
    diff := int64(videoPTS) - int64(audioPTS)  // різниця у 90kHz одиницях
    return time.Duration(diff * 1e6 / 90)       // конвертація у наносекунди
}
```

---

### 3️⃣ **fragment -f DURATION** — розбиття на фрагменти

```bash
# Розбити субтитри на 4-секундні фрагменти
astisub fragment -f 4s -i input.srt -o output.srt

# Корисно для синхронізації з аудіо-чанками
astisub fragment -f 5s -i long_subtitle.ass -o fragmented.vtt
```

### 🔍 Як це працює:

```
Вхід: [0с-10с "текст"]
Fragment(4с):
  → [0с-4с "текст"] + [4с-8с "текст"] + [8с-10с "текст"]

Вхід: [2с-6с "текст"]
Fragment(4с):
  → [2с-4с "текст"] + [4с-6с "текст"]
```

### ✅ Ваш use-case: синхронізація з аудіо-чанками

```go
// У вашому pipeline: 10с відео = 2×4с аудіо-чанки
func (p *SubtitleProcessor) fragmentForAudioChunks(subs *astisub.Subtitles, audioChunkDuration time.Duration) {
    // Розбиваємо субтитри по межах аудіо-чанків
    subs.Fragment(audioChunkDuration)
    
    // Тепер кожен фрагмент можна прив'язати до конкретного аудіо-чанка
    for _, item := range subs.Items {
        chunkIdx := int(item.StartAt / audioChunkDuration)
        // Додаємо metadata про аудіо-чанк для подальшої обробки
        item.InlineStyle = &astisub.StyleAttributes{
            // Можна зберігати chunkIdx у коментарях або кастомних полях
        }
    }
}
```

> ⚠️ **Увага**: `Fragment()` — O(n²) операція. Для великих файлів або real-time обробки кешуйте результати або уникайте виклику на кожному сегменті.

---

### 4️⃣ **unfragment** — злиття суміжних фрагментів

```bash
# Злити фрагменти з однаковим текстом
astisub unfragment -i fragmented.srt -o merged.srt

# [0с-4с "текст"] + [4с-8с "текст"] → [0с-8с "текст"]
```

### ✅ Ваш use-case: оптимізація перед відправкою клієнту

```go
// Перед масовою відправкою через WebSocket: зменшуємо кількість повідомлень
func (p *Broadcaster) optimizeForBroadcast(subs *astisub.Subtitles) {
    // Зливаємо суміжні субтитри з однаковим текстом
    subs.Unfragment()
    
    // Сортуємо за часом для гарантованого порядку
    subs.Order()
    
    // Тепер відправляємо меншу кількість повідомлень
    for _, item := range subs.Items {
        msg := p.itemToMessage(item)
        p.sendNonBlocking(msg)
    }
}
```

---

### 5️⃣ **merge** — об'єднання двох файлів

```bash
# Об'єднати арабські та англійські субтитри
astisub merge -i arabic.srt -i english.srt -o bilingual.vtt

# Автоматичне сортування за часом після об'єднання
```

### ✅ Ваш use-case: мультиязычні субтитри

```go
// Об'єднання субтитрів різних мов у один потік
func (p *SubtitleMerger) mergeMultilang(arabic, english, russian *astisub.Subtitles) *astisub.Subtitles {
    // Копіюємо арабську як основу
    result := cloneSubtitles(arabic)
    
    // Додаємо англійські та російські як окремі "регіони" або стилі
    if english != nil {
        for _, item := range english.Items {
            newItem := cloneItem(item)
            // Позначаємо мову через Region або InlineStyle
            newItem.Region = &astisub.Region{ID: "en_overlay"}
            result.Items = append(result.Items, newItem)
        }
    }
    
    if russian != nil {
        for _, item := range russian.Items {
            newItem := cloneItem(item)
            newItem.Region = &astisub.Region{ID: "ru_overlay"}
            result.Items = append(result.Items, newItem)
        }
    }
    
    // Сортуємо за часом для коректного відтворення
    result.Order()
    return result
}

// Глибоке копіювання Subtitles (щоб не змінити оригінали)
func cloneSubtitles(src *astisub.Subtitles) *astisub.Subtitles {
    dst := astisub.NewSubtitles()
    dst.Metadata = src.Metadata  // shallow copy, але зазвичай достатньо
    dst.Regions = make(map[string]*astisub.Region, len(src.Regions))
    dst.Styles = make(map[string]*astisub.Style, len(src.Styles))
    
    for id, r := range src.Regions { dst.Regions[id] = r }
    for id, s := range src.Styles { dst.Styles[id] = s }
    
    dst.Items = make([]*astisub.Item, len(src.Items))
    for i, item := range src.Items {
        dst.Items[i] = cloneItem(item)
    }
    return dst
}
```

---

### 6️⃣ **optimize** — видалення невикористаних стилів

```bash
# Видалити стилі/регіони, що не використовуються в Items
astisub optimize -i input.ass -o cleaned.ass
```

### ✅ Ваш use-case: підготовка для перекладу (NLLB)

```go
// Перед відправкою тексту у NLLB: видаляємо стилі, залишаємо тільки текст
func (p *TranslationPipeline) prepareForNLLB(subs *astisub.Subtitles) string {
    // Видаляємо всі стилі — NLLB працює з чистим текстом
    subs.RemoveStyling()
    
    // Експортуємо у текст (один рядок на субтитр)
    var text strings.Builder
    for _, item := range subs.Items {
        text.WriteString(item.String())  // concat всіх Lines
        text.WriteString("\n")
    }
    return text.String()
}
```

---

### 7️⃣ **apply-linear-correction** — масштабування часу

```bash
# Масштабувати таймінги: [10с, 60с] → [12с, 65с]
astisub apply-linear-correction \
  -a1 10s -a2 60s -d1 12s -d2 65s \
  -i input.srt -o corrected.srt
```

### 🔢 Формула корекції:
```
newTime = a * oldTime + b
де:
  a = (desired2 - desired1) / (actual2 - actual1)
  b = desired1 - a * actual1
```

### ✅ Ваш use-case: синхронізація з серверним часом

```go
// У VideoManifestProxy: корекція розбіжності між медіа-часом та серверним часом
func (p *VideoManifestProxy) syncSubtitleTime(subs *astisub.Subtitles, 
                                              mediaStart, mediaEnd,
                                              serverStart, serverEnd time.Time) {
    // Лінійна корекція: медіа-час → серверний час
    subs.ApplyLinearCorrection(
        mediaStart.Sub(p.streamStartTime),  // actual1
        mediaEnd.Sub(p.streamStartTime),    // actual2
        serverStart.Sub(p.streamStartTime), // desired1
        serverEnd.Sub(p.streamStartTime),   // desired2
    )
}

// Приклад: якщо медіа-потік відстає на 2с на початку і на 5с в кінці:
// actual1=0с, actual2=60с, desired1=2с, desired2=65с
// Результат: субтитри масштабуються під новий таймлайн
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// subtitle_processor.go — центральний обробник субтитрів
type SubtitleProcessor struct {
    channelID       string
    wsSender        *WSSender
    translator      *NLLBClient
    tts             *TTSService
    segmentDuration time.Duration
    audioChunkDuration time.Duration  // напр. 4с
}

// ProcessSegment — головна точка входу для обробки сегменту
func (p *SubtitleProcessor) ProcessSegment(data []byte, format string, seqNum uint64, pts time.Duration) error {
    // 1. Парсинг вхідного формату
    subs, err := p.parseSubtitleData(data, format)
    if err != nil {
        return fmt.Errorf("parse: %w", err)
    }
    
    // 2. Корекція часу відносно початку стріму
    streamOffset := pts.Sub(p.getStreamStartTime())
    subs.Add(streamOffset)
    
    // 3. Фрагментація під аудіо-чанки
    subs.Fragment(p.audioChunkDuration)
    
    // 4. Підготовка тексту для перекладу
    arabicText := p.extractText(subs)
    
    // 5. Асинхронний переклад + TTS
    go func() {
        enText, _ := p.translator.Translate(arabicText, "en")
        ruText, _ := p.translator.Translate(arabicText, "ru")
        
        enURL, _ := p.tts.Generate(enText, "en")
        ruURL, _ := p.tts.Generate(ruText, "ru")
        
        // 6. Формування WebSocket-повідомлень
        for _, item := range subs.Items {
            msg := &SubtitleMessage{
                Seq:          seqNum,
                TimeStart:    item.StartAt.Milliseconds(),
                TimeEnd:      item.EndAt.Milliseconds(),
                StartTimeUTC: time.Now().UTC().Format(time.RFC3339),
                Arabic:       p.extractArabic(item),
                English:      enText,
                Russian:      ruText,
                VideoSource:  p.getVideoSourceURL(seqNum),
                AudioFile:    p.getAudioFilePath(seqNum),
                TTSEnURL:     enURL,
                TTSRuURL:     ruURL,
            }
            p.wsSender.Broadcast(p.channelID, msg)
        }
    }()
    
    return nil
}

// parseSubtitleData — універсальний парсер за форматом
func (p *SubtitleProcessor) parseSubtitleData(data []byte, format string) (*astisub.Subtitles, error) {
    reader := bytes.NewReader(data)
    
    switch strings.ToLower(format) {
    case "srt":
        return astisub.ReadFromSRT(reader)
    case "vtt", "webvtt":
        return astisub.ReadFromWebVTT(reader)
    case "ssa", "ass":
        return astisub.ReadFromSSA(reader)
    case "stl":
        return astisub.ReadFromSTL(reader, astisub.STLOptions{})
    case "ts", "teletext":
        return astisub.ReadFromTeletext(reader, astisub.TeletextOptions{Page: 888})
    case "ttml":
        return astisub.ReadFromTTML(reader)
    default:
        // Спроба авто-детекту через astisub.Open (потребує файлу)
        return nil, fmt.Errorf("unsupported format: %s", format)
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Команда/Симптом | Рішення |
|----------|----------------|---------|
| **Таймінги "з'їжджають" після сегменту** | `sync` не застосовано | Зберігайте `lastPTS` на рівні каналу, застосовуйте `Add(drift)` при розриві |
| **Дублікати після фрагментації** | `fragment` + `unfragment` не зливає | Використовуйте `content hash + seq` dedup, як у вашій архітектурі |
| **Стилі "просочуються" між субтитрами** | `optimize` не видаляє невикористані | Викликайте `RemoveStyling()` перед перекладом, якщо стилі не потрібні |
| **Прогалини в кінці сегменту** | `fragment` створює порожні фрагменти | `ForceDuration(segmentDuration, false)` для заповнення без "..." |
| **Некоректний порядок відправки** | `merge` без `Order()` | Завжди `Order()` перед broadcast, особливо після `Merge()` |

---

## ⚡ Оптимізації для real-time обробки

### 1. Уникайте тимчасових файлів:
```go
// ❌ Повільно (диск I/O):
tmpFile, _ := os.CreateTemp("", "sub.*.srt")
astisub.Open(astisub.Options{Filename: tmpFile.Name()})

// ✅ Швидко (пам'ять):
subs, _ := astisub.ReadFromSRT(bytes.NewReader(data))
```

### 2. Кешування результатів фрагментації:
```go
// Фрагментація — дорого, тому кешуємо для одного каналу
var fragmentCache = sync.Map{}  // channelID → cached Fragments

func (p *SubtitleProcessor) getCachedFragments(channelID string, subs *astisub.Subtitles, duration time.Duration) *astisub.Subtitles {
    key := fmt.Sprintf("%s_%d", channelID, duration.Milliseconds())
    
    if cached, ok := fragmentCache.Load(key); ok {
        return cached.(*astisub.Subtitles)
    }
    
    // Копіюємо та фрагментуємо
    cloned := cloneSubtitles(subs)
    cloned.Fragment(duration)
    
    fragmentCache.Store(key, cloned)
    return cloned
}
```

### 3. Пакетна обробка таймінгів:
```go
// Замість індивідуального Add() для кожного Item:
func batchAdd(items []*astisub.Item, offset time.Duration) {
    for _, item := range items {
        item.StartAt += offset
        item.EndAt += offset
    }
    // Потім один виклик Order() замість сортування після кожного змінення
}
```

---

## 📋 Чек-лист інтеграції

```go
// ✅ 1. Валідація вхідних даних
if !utf8.Valid(data) {
    // Конвертація у UTF-8 або помилка
}

// ✅ 2. Парсинг без тимчасових файлів
subs, err := astisub.ReadFromSRT(bytes.NewReader(data))

// ✅ 3. Корекція часу
streamOffset := pts.Sub(streamStartTime)
subs.Add(streamOffset)

// ✅ 4. Фрагментація під аудіо-чанки
subs.Fragment(audioChunkDuration)

// ✅ 5. Підготовка для перекладу
subs.RemoveStyling()
text := extractText(subs)

// ✅ 6. Асинхронний переклад + TTS
go translateAndSend(text, subs.Items)

// ✅ 7. Сортування перед відправкою
subs.Order()

// ✅ 8. Метрики
monitoring.SubtitlesProcessed.Inc()
monitoring.SubtitleLatency.Observe(time.Since(receivedAt).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [astisub GitHub](https://github.com/asticode/go-astisub) — повна документація
- 📄 [Supported formats](https://github.com/asticode/go-astisub#supported-formats) — SRT, VTT, SSA, Teletext, STL, TTML
- 🧪 [astisub CLI examples](https://github.com/asticode/go-astisub#cli) — приклади використання
- 🎬 [astikit](https://github.com/asticode/go-astikit) — допоміжна бібліотека для flag parsing

---

> 💡 **Ключова рекомендація**: Оскільки ви працюєте з **реальним часом**:
> 1. **Уникайте `Fragment()/Unfragment()` на кожному сегменті** — це O(n²) операції. Зберігайте "базові" субтитри в кеші каналу.
> 2. **Використовуйте `bytes.Reader` замість тимчасових файлів** — зменшує I/O latency.
> 3. **Кешуйте результати `ApplyLinearCorrection()`** — корекція часу рідко змінюється в межах одного каналу.
> 4. **Додайте `format_version` у `SubtitleMessage`** — дозволить клієнту кешувати CSS-класи та уникати повторного парсингу.
> 5. **Тестуйте round-trip** (SRT → astisub → SRT) для вашого каналу, щоб переконатися, що арабський текст зберігається коректно.

Потрібен приклад інтеграції `apply-linear-correction` у ваш `VideoManifestProxy` для математичної синхронізації часу? Готовий допомогти! 🚀