# 🎞️ `media.Streams` та `media.Disposition`: Детальні метадані медіа-потоків

Це **розширена структура** бібліотеки, яка точно відображає вихід `ffprobe -show_streams` у типобезпечному Go-форматі. Вона дозволяє аналізувати **кожен окремий потік** (відео, аудіо, субтитри, дані) з точністю до кодеку, роздільності, таймінгів та прапорців використання.

---

## 🎯 Коротка відповідь

> **Це "мікроскоп для потоків"**: він розбиває файл на окремі медіа-доріжки та надає детальну інформацію про кожну (кодек, профіль, розміри, частоту кадрів, бітрейт, прапорці доступності) — критично для вибору основного потоку, адаптивного стрімінгу та валідації сумісності.

---

## 🧱 Структура `Streams`: Розбір за категоріями

| Категорія | Поля | Призначення у CCTV HLS |
|-----------|------|------------------------|
| 🔹 **Ідентифікація** | `Index`, `ID`, `CodecType` | Розрізнення відео/аудіо/субтитрів; мапінг потоків для мульти-доріжкових записів |
| 🔹 **Кодек та Профіль** | `CodecName`, `CodecLongName`, `Profile`, `Level` | Валідація сумісності (`h264` vs `hevc`), вибір параметрів транскодування |
| 🔹 **Відео-специфічні** | `Width`, `Height`, `CodedWidth`, `CodedHeight`, `PixFmt`, `HasBFrames`, `Refs` | Визначення роздільності, формату кольору (`yuv420p`), оцінка складності декодування |
| 🔹 **Таймінги та Частота** | `RFrameRrate`, `AvgFrameRate`, `TimeBase`, `DurationTs`, `Duration` | Розрахунок тривалості, синхронізація, генеруються `#EXTINF` для HLS |
| 🔹 **Бітрейт та Теги** | `BitRate`, `CodecTagString`, `CodecTag` | Оцінка навантаження на мережу, перевірка контейнерних тегів |
| 🔹 **Прапорці використання** | `Disposition` | Визначення основного потоку, треків для слабозорих/слабочуючих, форсованих субтитрів |

---

## 🔍 Структура `Disposition`: Прапорці використання потоку

```go
type Disposition struct {
    Default         int `json:"default"`          // 🔹 1 = грати за замовчуванням
    Dub             int `json:"dub"`              // 🔹 1 = дубльований аудіо
    Original        int `json:"original"`         // 🔹 1 = оригінальна доріжка
    Comment         int `json:"comment"`          // 🔹 1 = коментар режисера
    Lyrics          int `json:"lyrics"`           // 🔹 1 = текст пісні
    Karaoke         int `json:"karaoke"`          // 🔹 1 = караоке-доріжка
    Forced          int `json:"forced"`           // 🔹 1 = обов'язкові субтитри (напр. іншомовні вставки)
    HearingImpaired int `json:"hearing_impaired"` // 🔹 1 = аудіо-опис для слабозорих
    VisualImpaired  int `json:"visual_impaired"`  // 🔹 1 = субтитри для слабочуючих
    CleanEffects    int `json:"clean_effects"`    // 🔹 1 = чистий аудіо без шумів
}
```

**🎯 Призначення**: Керувати **поведінкою плеєра** при виборі потоків:
- ✅ `Default=1` → автоматичний вибір при відтворенні
- ✅ `Forced=1` → субтитри показуються навіть якщо глядач вимкнув їх
- ✅ `HearingImpaired/VisualImpaired` → підтримка доступності (WCAG)

**🔢 Конвертація у bool:**
```go
func (d Disposition) IsDefault() bool { return d.Default != 0 }
func (d Disposition) IsForced() bool  { return d.Forced != 0 }
// ... інші методи
```

---

## 🔄 Мапінг на FFprobe JSON

```json
{
  "streams": [
    {
      "index": 0,
      "id": "0x1",
      "codec_name": "h264",
      "codec_type": "video",
      "width": 1920,
      "height": 1080,
      "pix_fmt": "yuv420p",
      "r_frame_rate": "30/1",
      "avg_frame_rate": "30000/1001",
      "bit_rate": "4500000",
      "duration": "3600.123",
      "disposition": {
        "default": 1,
        "forced": 0
      }
    }
  ]
}
```

**✅ Відповідність полів:**
- `r_frame_rate` → `RFrameRrate` *(примітка: у полі є одрук, але JSON-тег правильний, тому парсинг працює)*
- `codec_type` → `CodecType` (`"video"`, `"audio"`, `"subtitle"`, `"data"`)
- `disposition` → `Disposition` структура

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Вибір основного відео/аудіо потоку

```go
// 🔹 Функція для пошуку найкращого відео та аудіо потоку
func SelectPrimaryStreams(metadata *media.Metadata) (*media.Streams, *media.Streams) {
    var bestVideo, bestAudio *media.Streams

    for i := range metadata.Streams {
        s := &metadata.Streams[i]
        switch s.CodecType {
        case "video":
            // 🔹 Пріоритет: потік за замовчуванням > більша роздільність
            if bestVideo == nil || 
               (s.Disposition.Default == 1 && bestVideo.Disposition.Default == 0) ||
               (s.Width*s.Height > bestVideo.Width*bestVideo.Height) {
                bestVideo = s
            }
        case "audio":
            // 🔹 Пріоритет: default > hearing_impaired > original
            if bestAudio == nil ||
               s.Disposition.Default == 1 ||
               (s.Disposition.HearingImpaired == 0 && bestAudio.Disposition.HearingImpaired == 1) {
                bestAudio = s
            }
        }
    }
    return bestVideo, bestAudio
}

// 🔹 Використання:
video, audio := SelectPrimaryStreams(&meta)
if video == nil {
    log.Fatal("❌ No video stream found")
}
log.Printf("📺 Primary: %dx%d @ %s, %s | 🎧 Audio: %s, %dch",
    video.Width, video.Height, video.RFrameRrate, video.CodecName,
    audio.CodecName, audio.Channels)
```

---

### 🔹 Приклад 2: Валідація сумісності для HLS

```go
func ValidateForHLS(stream *media.Streams) error {
    // 🔹 Перевірка типу потоку
    if stream.CodecType != "video" {
        return nil // ✅ Перевіряємо тільки відео
    }

    // 🔹 Підтримувані кодеки для HLS (Safari, hls.js, ExoPlayer)
    allowedCodecs := map[string]bool{
        "h264": true, "hevc": true, "av1": false, "vp9": false, // Safari підтримує H264/HEVC
    }
    if !allowedCodecs[stream.CodecName] {
        return fmt.Errorf("unsupported codec for HLS: %s", stream.CodecName)
    }

    // 🔹 Формат пікселів (обов'язково yuv420p для широкої сумісності)
    if stream.PixFmt != "yuv420p" {
        log.Printf("⚠️  Non-standard pix_fmt: %s. Transcoding to yuv420p recommended", stream.PixFmt)
    }

    // 🔹 Перевірка рівня H.264 (макс Level 4.1 для 1080p @ 30fps)
    if stream.CodecName == "h264" && stream.Level > 41 {
        return fmt.Errorf("H.264 level %d too high for broad compatibility", stream.Level)
    }

    // 🔹 Роздільність має бути парною (вимога кодерів)
    if stream.Width%2 != 0 || stream.Height%2 != 0 {
        return fmt.Errorf("resolution must be even: %dx%d", stream.Width, stream.Height)
    }

    return nil
}
```

---

### 🔹 Приклад 3: Форматування частоти кадрів для плейлиста

```go
// 🔹 FFprobe повертає дріб: "30000/1001" або ціле: "30/1"
func ParseFrameRate(rateStr string) float64 {
    if rateStr == "" { return 0 }
    
    parts := strings.Split(rateStr, "/")
    if len(parts) == 2 {
        num, _ := strconv.ParseFloat(parts[0], 64)
        den, _ := strconv.ParseFloat(parts[1], 64)
        if den > 0 { return num / den }
    }
    
    fps, _ := strconv.ParseFloat(rateStr, 64)
    return fps
}

// 🔹 Використання у генераторі HLS:
fps := ParseFrameRate(stream.RFrameRrate)
segmentDuration := 4.0 // секунди
framesPerSegment := int(fps * segmentDuration)

log.Printf("🎞️  %.2f fps → %d frames/segment (%.1fs)", fps, framesPerSegment, segmentDuration)
```

---

### 🔹 Приклад 4: Обробка потоків доступності

```go
func GenerateAccessibilityTracks(metadata *media.Metadata) []HLSTrack {
    var tracks []HLSTrack
    
    for _, s := range metadata.Streams {
        if s.CodecType == "audio" {
            if s.Disposition.HearingImpaired == 1 {
                tracks = append(tracks, HLSTrack{
                    Type:        "AUDIO",
                    GroupID:     "audio",
                    Name:        "Audio Description",
                    Default:     false,
                    AutoSelect:  true,
                    URI:         fmt.Sprintf("audio_ad/index.m3u8"),
                })
            }
        }
        if s.CodecType == "subtitle" {
            if s.Disposition.Forced == 1 {
                tracks = append(tracks, HLSTrack{
                    Type:        "SUBTITLES",
                    GroupID:     "subs",
                    Name:        "Forced Subtitles",
                    Default:     true,
                    AutoSelect:  true,
                    URI:         fmt.Sprintf("subs_forced/index.m3u8"),
                })
            }
        }
    }
    return tracks
}
```

---

## ⚠️ Важливі зауваження

| Аспект | Деталі | Рекомендація |
|--------|--------|--------------|
| 🔤 **Типографіка** | `RFrameRrate` замість `RFrameRate` | JSON-тег правильний (`r_frame_rate`), тому парсинг працює. Виправте назву поля для чистоти коду. |
| 🔢 **Типи даних** | `BitRate`, `Duration`, `TimeBase` — `string` | FFprobe виводить їх як рядки. Парсіть через `strconv.ParseFloat/Int` з перевіркою помилок. |
| 🎚️ **Disposition** | Використовує `int` (`0`/`1`) замість `bool` | Це стандарт для FFprobe JSON. Додайте методи-хелпери `IsDefault() bool` для зручності. |
| 📐 **CodedWidth/Height** | Може відрізнятися від `Width/Height` | `Coded*` включає паддінг для макроблоків. Використовуйте `Width/Height` для відображення, `Coded*` для кодування. |
| 🔄 **RFrameRate vs AvgFrameRate** | `r_frame_rate` = номінальна, `avg_frame_rate` = реальна | Для HLS використовуйте `r_frame_rate`. Для аналізу якості — `avg_frame_rate`. |

---

## 📋 Чекліст для вашого проекту

```
[ ] При парсингу потоків:
    • Завжди перевіряйте stream.CodecType перед доступом до відео/аудіо полів
    • Парсіть числові string-поля з обробкою помилок
    • Використовуйте CodedWidth/Height тільки для технічних розрахунків кодера

[ ] Для валідації HLS-сумісності:
    • Перевіряйте PixFmt == "yuv420p" (обов'язково для Safari/Chrome)
    • Валідуйте H.264 Level ≤ 4.1 для 1080p, ≤ 5.1 для 4K
    • Переконайтеся, що Width/Height парні

[ ] Для роботи з Disposition:
    • Додайте методи-хелпери: IsDefault(), IsForced(), IsHearingImpaired()
    • Використовуйте default=1 для основного потоку, forced=1 для обов'язкових субтитрів
    • Логувайте потоки доступності для відповідності стандартам

[ ] Для дебагу:
    • Виводите повну інформацію про потік: log.Printf("🔍 Stream %d: %s %dx%d %s", s.Index, s.CodecName, s.Width, s.Height, s.BitRate)
    • Порівнюйте r_frame_rate та avg_frame_rate для виявлення VFR (Variable Frame Rate)
    • Тестуйте з різними джерелами: IP-камери, смартфони, професійні енкодери

[ ] Для тестування:
    • Створюйте mock-відповіді FFprobe з різними кодеками, роздільностями, disposition
    • Перевіряйте обробку порожніх/некоректних полів (напр. "" замість "30/1")
    • Тестуйте потоки з VFR, нестандартними pix_fmt, відсутніми disposition прапорцями
```

---

## 🎯 Висновок

> **`media.Streams` та `media.Disposition` — це "анатомія медіа-файлу"**, яка забезпечує:
> • ✅ Точне відображення структури FFprobe JSON у типобезпечні Go-типи
> • ✅ Детальний аналіз кожного потоку: кодек, роздільність, таймінги, бітрейт
> • ✅ Керування поведінкою плеєра через прапорці `Disposition`
> • ✅ Інтеграцію з логікою вибору потоків, валідації HLS, підтримки доступності
> • ✅ Гнучкість: легко розширюється новими полями без порушення зворотної сумісності

Для вашого **CCTV HLS Processor** це означає:
- 📡 Автоматичний вибір основного відео/аудіо потоку з IP-камер
- 🎬 Валідація сумісності кодеків, роздільності та форматів пікселів для HLS
- ♿ Підтримка потоків доступності (аудіо-опис, форсовані субтитри)
- ⚡ Точний розрахунок таймінгів, бітрейту та кількості кадрів на сегмент
- 🛡️ Захист від несумісних файлів через комплексну перевірку на рівні потоків

Потребуєте допомоги з інтеграцією парсингу потоків у ваш конвеєр або з налаштуванням автоматичного вибору треків для мульти-мовних камер? Напишіть — покажу готовий код для вашого сценарію! 🚀🎞️