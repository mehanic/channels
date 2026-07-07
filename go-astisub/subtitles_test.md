# 🧪 Глибокий розбір: Тести astisub — API для маніпуляції субтитрами

Цей файл — **тестовий набір бібліотеки astisub**, який демонструє ключові функції для роботи з субтитрами: маніпуляція часом, об'єднання, фрагментація, стилізація. Розберемо, як ці можливості використати у вашому **CCTV HLS Processor**.

---

## 📊 Огляд тестованих функцій

```
┌─────────────────────────────────────────┐
│ 🎯 Ключові можливості astisub.Subtitles │
├─────────────────────────────────────────┤
│ ⏱️  Time manipulation:                   │
│    • Add()          — зсув таймінгів     │
│    • Fragment()     — розбиття по інтервалах │
│    • Unfragment()   — злиття суміжних    │
│    • ApplyLinearCorrection() — масштабування часу │
│                                         │
│ 🔗 Merge & Order:                       │
│    • Merge()        — об'єднання потоків │
│    • Order()        — сортування за часом │
│                                         │
│ ✂️  Optimization:                        │
│    • Optimize()     — видалення невикористаних стилів │
│    • RemoveStyling() — "очищення" формату │
│    • ForceDuration() — заповнення прогалин │
│                                         │
│ 🌐 Format handling:                     │
│    • HTML entities  — &nbsp; &amp; тощо │
│    • Line endings   — \r\n, \n, \r      │
└─────────────────────────────────────────┘
```

---

## ⚙️ Детальний розбір функцій + приклади для вашого проекту

### 1️⃣ **Add() — зсув таймінгів**

```go
// Тест:
s.Add(time.Second)        // +1с до всіх таймінгів
s.Add(-3 * time.Second)   // -3с (може видалити субтитри з start < 0)

// ✅ Ваш use-case: корекція дрейфу PTS у HLS-сегментах
func (p *SubtitleProcessor) correctPTSDrift(items []*astisub.Item, drift time.Duration) {
    for _, item := range items {
        item.StartAt += drift
        item.EndAt += drift
        // Видаляємо "від'ємні" субтитри
        if item.EndAt < 0 {
            // логуємо або ігноруємо
        }
        if item.StartAt < 0 {
            item.StartAt = 0
        }
    }
}
```

> 💡 **Порада**: Використовуйте `Add()` після `VideoManifestProxy` синхронізації, коли виявлено розрив >1с між сегментами.

---

### 2️⃣ **Fragment() / Unfragment() — робота з перекриттями**

```go
// Fragment: розбиває субтитри на інтервали
s.Fragment(2 * time.Second)
// Результат: субтитр [1с-3с] → [1с-2с] + [2с-3с]

// Unfragment: зливає суміжні субтитри з однаковим текстом
s.Unfragment()
// [1с-2с "текст"] + [2с-3с "текст"] → [1с-3с "текст"]

// ✅ Ваш use-case: синхронізація аудіо/відео чанків
// Коли 10с відео = 2×4с аудіо, субтитри можуть "розриватися"

func (p *SubtitleProcessor) mergeAudioChunks(subs *astisub.Subtitles, audioChunkDuration time.Duration) {
    // 1. Фрагментуємо по межах аудіо-чанків
    subs.Fragment(audioChunkDuration)
    
    // 2. Для кожного фрагмента додаємо metadata про audio_source
    for _, item := range subs.Items {
        chunkIdx := int(item.StartAt / audioChunkDuration)
        // Тут можна додати video_source URL для парних чанків
        // (як у вашій логіці: парний seq → video_source, непарний → null)
    }
    
    // 3. Зливаємо назад, якщо потрібно для відправки клієнту
    subs.Unfragment()
}
```

---

### 3️⃣ **Merge() — об'єднання кількох потоків субтитрів**

```go
// Тест:
s1.Merge(s2)  // Об'єднує Items, Regions, Styles з сортуванням за часом

// ✅ Ваш use-case: мультиязычні субтитри (AR + EN + RU)
type MultiLangSubtitles struct {
    Arabic  *astisub.Subtitles
    English *astisub.Subtitles
    Russian *astisub.Subtitles
}

func (m *MultiLangSubtitles) MergeForBroadcast() *astisub.Subtitles {
    result := m.Arabic // основа — арабська (оригінал)
    
    // Додаємо перекладені версії як окремі "регіони" або "стилі"
    if m.English != nil {
        // Варіант 1: додати як окремі Items з іншим Region.ID
        for _, item := range m.English.Items {
            newItem := *item
            newItem.Region = &astisub.Region{ID: "en_overlay"}
            result.Items = append(result.Items, &newItem)
        }
    }
    
    // Сортуємо за часом для коректного відтворення
    result.Order()
    return result
}
```

> ⚠️ **Увага**: `Merge()` не робить глибокого копіювання — якщо модифікуєте об'єднані субтитри, клонуйте `Item` перед зміною.

---

### 4️⃣ **Order() — сортування за часом**

```go
// Тест:
s.Items = []*Item{{4s,5s}, {2s,3s}, {3s,4s}, {1s,2s}}
s.Order()  // → [{1s,2s}, {2s,3s}, {3s,4s}, {4s,5s}]

// ✅ Ваш use-case: гарантований порядок у WebSocket-відправці
func (b *Broadcaster) sendOrdered(subs *astisub.Subtitles, client *Client) {
    // Копіюємо та сортуємо, щоб не змінювати оригінал
    items := make([]*astisub.Item, len(subs.Items))
    copy(items, subs.Items)
    
    // Створюємо тимчасову Subtitles для сортування
    temp := &astisub.Subtitles{Items: items}
    temp.Order()
    
    // Відправляємо по черзі з контролем дублікатів
    for _, item := range temp.Items {
        if item.StartAt >= client.lastSentTime {
            msg := p.subtitleToMessage(item, client.channelID)
            b.sendNonBlocking(client, msg)
        }
    }
}
```

---

### 5️⃣ **ApplyLinearCorrection() — масштабування часу**

```go
// Тест:
// Коригує таймінги лінійно між двома точками:
// (3с→5с) мапиться на (5с→8с), інші інтерполируються
s.ApplyLinearCorrection(3*time.Second, 5*time.Second, 5*time.Second, 8*time.Second)

// ✅ Ваш use-case: корекція розбіжності між серверним та медіа-часом
func (p *VideoManifestProxy) syncSubtitleTime(subs *astisub.Subtitles, 
                                              mediaStart, mediaEnd, 
                                              serverStart, serverEnd time.Time) {
    // Лінійна корекція: медіа-час → серверний час
    subs.ApplyLinearCorrection(
        mediaStart.Sub(p.streamStartTime),  // oldStart
        mediaEnd.Sub(p.streamStartTime),    // oldEnd
        serverStart.Sub(p.streamStartTime), // newStart
        serverEnd.Sub(p.streamStartTime),   // newEnd
    )
}
```

> 📐 **Формула**: `newTime = newStart + (oldTime - oldStart) * (newEnd-newStart)/(oldEnd-oldStart)`

---

### 6️⃣ **Optimize() / RemoveStyling() — очищення**

```go
// Тест Optimize():
// Видаляє Regions/Styles, які не використовуються в Items

// Тест RemoveStyling():
// Прибирає всі StyleAttributes, Region, Style посилання

// ✅ Ваш use-case: підготовка субтитрів для TTS або перекладу
func (p *TranslationPipeline) prepareForNLLB(subs *astisub.Subtitles) string {
    // 1. Створюємо копію, щоб не змінити оригінал
    clean := &astisub.Subtitles{}
    *clean = *subs
    clean.Items = make([]*astisub.Item, len(subs.Items))
    for i, item := range subs.Items {
        newItem := *item
        clean.Items[i] = &newItem
    }
    
    // 2. Видаляємо стилі — NLLB працює з чистим текстом
    clean.RemoveStyling()
    
    // 3. Експортуємо в текст (один рядок на субтитр)
    var text strings.Builder
    for _, item := range clean.Items {
        text.WriteString(item.String()) // item.String() = concat всіх Lines
        text.WriteString("\n")
    }
    return text.String()
}
```

---

### 7️⃣ **ForceDuration() — заповнення кінця**

```go
// Тест:
s.ForceDuration(10*time.Second, true)  
// Якщо останній субтитр закінчується раніше 10с — додає "..." до 10с

// ✅ Ваш use-case: гарантія, що HLS-плейлист має субтитри до кінця сегменту
func (p *HLSGenerator) ensureSubtitleCoverage(subs *astisub.Subtitles, segmentDuration time.Duration) {
    lastEnd := time.Duration(0)
    for _, item := range subs.Items {
        if item.EndAt > lastEnd {
            lastEnd = item.EndAt
        }
    }
    
    // Якщо є "діра" в кінці — додаємо порожній субтитр
    if lastEnd < segmentDuration {
        subs.ForceDuration(segmentDuration, true)
    }
}
```

---

### 8️⃣ **HTML Entities & Scanner — робота з форматами**

```go
// Тест HTMLEntity:
// &nbsp; → \u00A0, &amp; → &, < → &lt; тощо

// Тест NewScanner:
// Коректна обробка \r\n (Windows), \n (Unix), \r (old Mac)

// ✅ Ваш use-case: парсинг вхідних субтитрів з різних джерел
func (p *SubtitleIngest) parseIncoming(data []byte, format string) (*astisub.Subtitles, error) {
    reader := bytes.NewReader(data)
    
    switch format {
    case "srt", "vtt", "ssa", "teletext":
        return astisub.ReadFromReader(reader, &astisub.ReaderOptions{
            Teletext: astisub.TeletextOptions{Page: 888, PID: 0},
        })
    default:
        // Спробуємо авто-детект через astisub.Open
        return astisub.ReadFromReader(reader, nil)
    }
}

// Після парсингу — нормалізуємо сутності для консистентності
func normalizeEntities(subs *astisub.Subtitles) {
    for _, item := range subs.Items {
        for lineIdx := range item.Lines {
            for itemIdx := range item.Lines[lineIdx].Items {
                // astisub вже декодує сутності при читанні,
                // але можна додати додаткову валідацію
                text := item.Lines[lineIdx].Items[itemIdx].Text
                item.Lines[lineIdx].Items[itemIdx].Text = strings.TrimSpace(text)
            }
        }
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// subtitle_processor.go
type SubtitleProcessor struct {
    channelID      string
    wsSender       *WSSender
    translator     *NLLBClient
    tts            *TTSService
    segmentDuration time.Duration
}

func (p *SubtitleProcessor) ProcessTeletextSegment(data []byte, seqNum uint64, pts time.Time) error {
    // 1. Парсинг Teletext → astisub.Subtitles
    subs, err := astisub.ReadFromTeletext(
        bytes.NewReader(data), 
        astisub.TeletextOptions{Page: 888, PID: 0},
    )
    if err != nil {
        return fmt.Errorf("teletext parse: %w", err)
    }
    
    // 2. Корекція часу відносно початку стріму
    streamOffset := pts.Sub(p.getStreamStartTime())
    for _, item := range subs.Items {
        item.StartAt += streamOffset
        item.EndAt += streamOffset
    }
    
    // 3. Фрагментація під аудіо-чанки (4с)
    subs.Fragment(4 * time.Second)
    
    // 4. Підготовка для перекладу
    arabicText := p.extractText(subs, "ar")
    
    // 5. Асинхронний переклад (Whisper → NLLB)
    go func() {
        enText, _ := p.translator.Translate(arabicText, "en")
        ruText, _ := p.translator.Translate(arabicText, "ru")
        
        // 6. TTS для перекладів
        enURL, _ := p.tts.Generate(enText, "en")
        ruURL, _ := p.tts.Generate(ruText, "ru")
        
        // 7. Формування WebSocket-повідомлення
        for _, item := range subs.Items {
            msg := &SubtitleMessage{
                Seq:          seqNum,
                TimeStart:    item.StartAt.Milliseconds(),
                TimeEnd:      item.EndAt.Milliseconds(),
                StartTimeUTC: time.Now().UTC().Format(time.RFC3339),
                Arabic:       p.extractArabic(item),
                English:      enText,
                Russian:      ruText,
                VideoSource:  p.getVideoSourceURL(seqNum), // парний/непарний логіка
                AudioFile:    p.getAudioFilePath(seqNum),
                TTSEnURL:     enURL,
                TTSRuURL:     ruURL,
            }
            p.wsSender.Broadcast(p.channelID, msg)
        }
    }()
    
    return nil
}

// Допоміжна: витяг тексту з підтримкою багаторядкових субтитрів
func (p *SubtitleProcessor) extractText(subs *astisub.Subtitles, lang string) string {
    var lines []string
    for _, item := range subs.Items {
        for _, line := range item.Lines {
            lines = append(lines, line.String())
        }
    }
    return strings.Join(lines, " ")
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Тест, що демонструє | Рішення для вашого проекту |
|----------|---------------------|---------------------------|
| **Субтитри "з'їжджають" після сегменту** | `TestSubtitles_Add` | Зберігайте `lastPTS` на рівні каналу, застосовуйте `Add(drift)` при розриві |
| **Дублікати при перекритті чанків** | `TestSubtitles_Unfragment` | Використовуйте `content hash + seq` dedup, як у вашій архітектурі |
| **Стилі "просочуються" між субтитрами** | `TestSubtitles_Optimize` | Викликайте `RemoveStyling()` перед перекладом, якщо стилі не потрібні |
| **Прогалини в кінці сегменту** | `TestSubtitles_ForceDuration` | `ForceDuration(segmentDuration, false)` для заповнення без "..." |
| **Некоректний порядок відправки** | `TestSubtitles_Order` | Завжди `Order()` перед broadcast, особливо після `Merge()` |

---

## 📋 Чек-лист інтеграції astisub

```go
// ✅ 1. Імпорт та ініціалізація
import "github.com/asticode/go-astisub"

// ✅ 2. Парсинг (Teletext / SRT / VTT)
subs, _ := astisub.ReadFromTeletext(reader, opts)

// ✅ 3. Корекція часу
for _, item := range subs.Items {
    item.StartAt += offset
    item.EndAt += offset
}

// ✅ 4. Фрагментація під ваші чанки
subs.Fragment(audioChunkDuration)  // напр. 4с

// ✅ 5. Підготовка для перекладу
subs.RemoveStyling()  // якщо стилі не потрібні

// ✅ 6. Експорт тексту
text := item.String()  // або item.Lines[0].String() для першого рядка

// ✅ 7. Сортування перед відправкою
subs.Order()

// ✅ 8. (Опціонально) Об'єднання з іншими мовами
combined.Merge(englishSubs)
combined.Order()

// ✅ 9. Метрики
monitoring.SubtitlesProcessed.Inc()
monitoring.SubtitleLatency.Observe(time.Since(receivedAt).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [astisub GitHub](https://github.com/asticode/go-astisub) — повна документація
- 📄 [Supported formats](https://github.com/asticode/go-astisub#supported-formats) — SRT, VTT, SSA, Teletext, STL, TTML
- 🧪 [Testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади файлів для тестів

---

> 💡 **Ключова рекомендація**: Оскільки ваш pipeline працює в реальному часі з сегментами 4-10с, **уникайте `Fragment()`/`Unfragment()` на кожному сегменті** — це O(n²) операції. Замість цього:
> 1. Зберігайте "базові" субтитри в кеші каналу
> 2. Застосовуйте тільки `Add()` для корекції часу
> 3. Викликайте `Order()` тільки перед масовою відправкою (initial batch)
> 4. Для перекладу передавайте чистий текст без маніпуляцій зі структурою

Потрібен приклад інтеграції `ApplyLinearCorrection()` у ваш `VideoManifestProxy` для математичної синхронізації часу? Готовий допомогти! 🚀