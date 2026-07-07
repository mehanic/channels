# 📝 WebVTT у MP4: Повне пояснення коду

Це код з бібліотеки `go-mp4` для роботи з **WebVTT субтитрами** у форматі MP4/fMP4 (ISOBMFF). WebVTT — це стандартний формат текстових доріжок для веб-відео, який використовується в HLS-стрімінгу для відображення субтитрів, описів для слабозорих, метаданих тощо.

---

## 🎯 Коротка відповідь

> **Це "контейнер" для WebVTT-субтитрів у MP4**, який дозволяє вбудовувати текстові доріжки (субтитри, титри, описи) прямо у fMP4-сегменти вашого HLS-стріму.

---

## 🗂️ Архітектура: Дві групи боксів

### 🔹 Група 1: Конфігурація доріжки (на рівні `stsd`)

| Бокс | Тип | Призначення |
|------|-----|-------------|
| `vttC` | WebVTTConfigurationBox | 🔹 Глобальні налаштування WebVTT (CSS, регіони) |
| `vlab` | WebVTTSourceLabelBox | Мітка джерела субтитрів (напр. "українська") |
| `wvtt` | WVTTSampleEntry | 🔹 Оголошення: "ця доріжка містить WebVTT" |

### 🔹 Група 2: Формат семплів (всередині `mdat`)

| Бокс | Тип | Призначення |
|------|-----|-------------|
| `vttc` | VTTCueBox | 🔹 Контейнер для одного "к'ю" (рядка субтитрів) |
| `vsid` | CueSourceIDBox | ID джерела (для багатомовних субтитрів) |
| `ctim` | CueTimeBox | 🔹 Час появи к'ю (напр. "00:01:23.456") |
| `iden` | CueIDBox | Унікальний ідентифікатор к'ю |
| `sttg` | CueSettingsBox | 🔹 Налаштування відображення (позиція, стиль) |
| `payl` | CuePayloadBox | 🔹 Текст субтитрів (основне навантаження!) |
| `vtte` | VTTEmptyCueBox | Порожній к'ю (для синхронізації) |
| `vtta` | VTTAdditionalTextBox | Додатковий текст (напр. опис сцени) |

---

## 🔍 Детальний розбір структури

### 🔹 Конфігураційні бокси

#### ✅ `WebVTTConfigurationBox` (`vttC`)

```go
type WebVTTConfigurationBox struct {
    Box
    Config string `mp4:"0,boxstring"`  // 🔹 WebVTT-заголовок + глобальні стилі
}
```

**Приклад вмісту `Config`:**
```webvtt
WEBVTT

REGION
id:lower-third
width:40%
lines:3
viewport-anchor:10%,90%
region-anchor:10%,90%
scroll:up

::cue {
  font-size: 18px;
  color: white;
  background: rgba(0,0,0,0.6);
}
```

> 🎯 **Призначення**: Глобальні налаштування, що застосовуються до всіх к'ю у доріжці.

---

#### ✅ `WebVTTSourceLabelBox` (`vlab`)

```go
type WebVTTSourceLabelBox struct {
    Box
    SourceLabel string `mp4:"0,boxstring"`  // 🔹 Мітка джерела
}
```

**Приклад**: `"українська"`, `"english"`, `"audio-description"`

> 🎯 **Призначення**: Допомога плеєру відобразити правильну назву доріжки у меню вибору субтитрів.

---

#### ✅ `WVTTSampleEntry` (`wvtt`)

```go
type WVTTSampleEntry struct {
    SampleEntry `mp4:"0,extend"`  // 🔹 Базові поля: DataReferenceIndex тощо
}
```

**Де зустрічається**: `moov → trak → mdia → minf → stbl → stsd → wvtt`

> 🎯 **Призначення**: Оголошує, що ця аудіо-доріжка насправді містить **текстові субтитри у форматі WebVTT**.

---

### 🔹 Бокси семплів (всередині `vttc` — Cue Container)

#### ✅ `VTTCueBox` (`vttc`) — контейнер для одного к'ю

```go
type VTTCueBox struct {
    Box  // 🔹 Пустий, але слугує "обгорткою" для вкладених боксів
}
```

**Структура одного к'ю**:
```
📦 vttc (Cue Container)
├── 📦 ctim (Cue Time)      ← "00:01:23.456"
├── 📦 iden (Cue ID)        ← "cue-001" (опціонально)
├── 📦 sttg (Settings)      ← "align:center line:90%" (опціонально)
├── 📦 payl (Payload)       ← "Привіт, світ!" ← 🔹 ОСНОВНИЙ ТЕКСТ
└── 📦 vtta (Additional)    ← "[шум вітру]" (опціонально)
```

---

#### ✅ `CueTimeBox` (`ctim`) — час появи к'ю

```go
type CueTimeBox struct {
    Box
    CueCurrentTime string `mp4:"0,boxstring"`  // 🔹 Формат: "HH:MM:SS.mmm"
}
```

**Приклад**: `"00:01:23.456"`, `"01:45:00.000"`

> 🎯 **Важливо**: Це **рядок**, а не число! Плеєр сам парсить його у мікросекунди.

---

#### ✅ `CuePayloadBox` (`payl`) — 🔥 текст субтитрів

```go
type CuePayloadBox struct {
    Box
    CueText string `mp4:"0,boxstring"`  // 🔹 ОСНОВНИЙ ТЕКСТ СУБТИТРІВ
}
```

**Приклад**:
```
"Привіт, світ!"
"Це приклад багаторядкового
субтитру з переносом."
"<i>Курсивний текст</i> та <b>жирний</b>"
```

> 🎯 **Це найважливіший бокс!** Без `payl` к'ю порожній і не відображається.

---

#### ✅ `CueSettingsBox` (`sttg`) — налаштування відображення

```go
type CueSettingsBox struct {
    Box
    Settings string `mp4:"0,boxstring"`  // 🔹 WebVTT settings
}
```

**Приклад `Settings`**:
```webvtt
align:center line:90% position:50%,center size:80%
align:left line:0% position:10%,start
region:lower-third
```

| Параметр | Значення | Опис |
|----------|----------|------|
| `align` | `start`/`center`/`end` | Вирівнювання тексту |
| `line` | `0%`...`100%` або `-1`, `-2`... | Вертикальна позиція |
| `position` | `X%,anchor` | Горизонтальна позиція |
| `size` | `10%`...`100%` | Ширина області тексту |
| `region` | `id-регіону` | Посилання на REGION з `vttC` |

---

#### ✅ `CueIDBox` (`iden`) — унікальний ідентифікатор

```go
type CueIDBox struct {
    Box
    CueId string `mp4:"0,boxstring"`  // 🔹 Унікальний ID к'ю
}
```

**Приклад**: `"news-intro-001"`, `"ad-break-marker"`

> 🎯 **Призначення**: Дозволяє плеєру або серверу посилатися на конкретний к'ю (напр. для аналітики, пошуку).

---

#### ✅ `CueSourceIDBox` (`vsid`) — ID джерела

```go
type CueSourceIDBox struct {
    Box
    SourceId uint32 `mp4:"0,size=32"`  // 🔹 Числовий ID джерела
}
```

**Приклад**: `1` = українська, `2` = російська, `3` = англійська

> 🎯 **Призначення**: Для багатомовних стрімів — швидка фільтрація к'ю за мовою без парсингу тексту.

---

#### ✅ `VTTEmptyCueBox` (`vtte`) — синхронізація

```go
type VTTEmptyCueBox struct {
    Box  // 🔹 Порожній бокс без даних
}
```

> 🎯 **Призначення**: "Пустий" к'ю для підтримки таймінгу, коли немає тексту, але потрібно зберегти синхронізацію з відео.

---

#### ✅ `VTTAdditionalTextBox` (`vtta`) — додатковий текст

```go
type VTTAdditionalTextBox struct {
    Box
    CueAdditionalText string `mp4:"0,boxstring"`  // 🔹 Додатковий контент
}
```

**Приклад**: `"[шум вітру]"`, `"[музика грає]"`, `"[аплодисменти]"`

> 🎯 **Призначення**: Опис не-діалогового контенту для слабозорих (аудіо-опис).

---

## 🔑 Ключова особливість: `boxstring` тег

Усі текстові поля використовують тег `mp4:"0,boxstring"`:

```go
Config string `mp4:"0,boxstring"`
```

**Що робить `boxstring`?**
```
🔹 Це спеціальний тип для бібліотеки go-mp4:
• Читає всі байти до кінця боксу як рядок
• Не використовує нуль-термінацію (як C-string)
• Не використовує довжину-префікс (як Pascal-string)
• Просто: [байт1][байт2]...[байтN] → string

🎯 Перевага: Підтримка будь-якого тексту (UTF-8, переноси рядків, спецсимволи)
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Додавання українських субтитрів до fMP4-сегмента

```go
import "github.com/abema/go-mp4"

func addWebVTTSubtitles(segmentPath string, cues []SubtitleCue) error {
    f, err := os.OpenFile(segmentPath, os.O_RDWR, 0644)
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 1. Знайти або створити текстову доріжку
    var textTrackID uint32
    mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        if h.BoxInfo.Type == mp4.BoxTypeStsd() {
            // Шукаємо wvtt entry або додаємо новий
            // ... логіка додавання доріжки ...
            textTrackID = 2  // приклад
        }
        return nil, nil
    })
    
    // 🔹 2. Додати к'ю у mdat бокс
    for _, cue := range cues {
        // Створити vttc контейнер
        vttc := &mp4.VTTCueBox{}
        
        // Додати ctim (час)
        ctim := &mp4.CueTimeBox{
            CueCurrentTime: formatWebVTTTime(cue.StartTime),
        }
        
        // Додати payl (текст)
        payl := &mp4.CuePayloadBox{
            CueText: cue.Text,  // "Привіт, світ!"
        }
        
        // Додати sttg (налаштування, опціонально)
        var sttg *mp4.CueSettingsBox
        if cue.Position != "" {
            sttg = &mp4.CueSettingsBox{Settings: cue.Position}
        }
        
        // 🔹 Серіалізувати у файл
        // (спрощено — у реальності потрібно оновлювати офсети)
        mp4.WriteBoxInfo(f, &mp4.BoxInfo{Type: mp4.BoxTypeVttc()})
        ctim.Marshal(f)
        if sttg != nil { sttg.Marshal(f) }
        payl.Marshal(f)
    }
    
    return nil
}

// Допоміжна функція: Go time → WebVTT time string
func formatWebVTTTime(t time.Time) string {
    // WebVTT формат: "HH:MM:SS.mmm"
    return t.Format("15:04:05.000")
}

type SubtitleCue struct {
    StartTime time.Time
    EndTime   time.Time
    Text      string
    Position  string  // опціонально: "align:center line:90%"
}
```

---

### 🔹 Приклад 2: Читання субтитрів з fMP4 для відправки клієнту

```go
func extractWebVTTCues(filePath string) ([]SubtitleCue, error) {
    f, err := os.Open(filePath)
    if err != nil { return nil, err }
    defer f.Close()
    
    var cues []SubtitleCue
    
    _, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
        // 🔹 Шукаємо vttc бокси (контейнери к'ю)
        if h.BoxInfo.Type == mp4.BoxTypeVttc() {
            var ctim, payl, sttg string
            
            // 🔹 Парсимо вкладені бокси всередині vttc
            mp4.ReadBoxStructure(h.Reader(), func(inner *mp4.ReadHandle) (interface{}, error) {
                switch inner.BoxInfo.Type {
                case mp4.BoxTypeCtim():
                    box := &mp4.CueTimeBox{}
                    inner.ReadPayload(box)
                    ctim = box.CueCurrentTime
                    
                case mp4.BoxTypePayl():
                    box := &mp4.CuePayloadBox{}
                    inner.ReadPayload(box)
                    payl = box.CueText
                    
                case mp4.BoxTypeSttg():
                    box := &mp4.CueSettingsBox{}
                    inner.ReadPayload(box)
                    sttg = box.Settings
                }
                return nil, nil
            })
            
            // 🔹 Додаємо к'ю у результат, якщо є текст
            if payl != "" {
                startTime, _ := parseWebVTTTime(ctim)
                cues = append(cues, SubtitleCue{
                    StartTime: startTime,
                    Text:      payl,
                    Position:  sttg,
                })
            }
        }
        return nil, nil
    })
    
    return cues, err
}

// Допоміжна функція: WebVTT time string → Go time
func parseWebVTTTime(s string) (time.Time, error) {
    // Спрощений парсинг "00:01:23.456"
    parts := strings.Split(s, ":")
    if len(parts) != 3 {
        return time.Time{}, fmt.Errorf("invalid time format: %s", s)
    }
    
    hours, _ := strconv.Atoi(parts[0])
    minutes, _ := strconv.Atoi(parts[1])
    
    // Парсинг секунд.мілісекунд
    secParts := strings.Split(parts[2], ".")
    seconds, _ := strconv.Atoi(secParts[0])
    millis := 0
    if len(secParts) > 1 {
        millis, _ = strconv.Atoi(secParts[1])
    }
    
    return time.Date(0, 1, 1, hours, minutes, seconds, millis*1e6, time.UTC), nil
}
```

---

### 🔹 Приклад 3: Генерація HLS-плейлиста з текстовою доріжкою

```go
func generateHLSPlaylistWithSubtitles(videoSegments []string, subtitleCues []SubtitleCue) string {
    var sb strings.Builder
    
    // 🔹 Заголовок плейлиста
    sb.WriteString("#EXTM3U\n")
    sb.WriteString("#EXT-X-VERSION:7\n")  // WebVTT потребує версію 7+
    
    // 🔹 Додати текстову доріжку
    sb.WriteString("#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"subs\",")
    sb.WriteString("NAME=\"Українська\",LANGUAGE=\"uk\",")
    sb.WriteString("FORCED=NO,AUTOSELECT=YES,DEFAULT=NO,")
    sb.WriteString("URI=\"subtitles_uk.m3u8\"\n")
    
    // 🔹 Додати відео-сегменти
    for _, seg := range videoSegments {
        sb.WriteString(fmt.Sprintf("#EXTINF:4.0,\n%s\n", seg))
    }
    
    // 🔹 Створити окремий плейлист для субтитрів
    subtitlePlaylist := generateSubtitlePlaylist(subtitleCues)
    os.WriteFile("subtitles_uk.m3u8", []byte(subtitlePlaylist), 0644)
    
    return sb.String()
}

func generateSubtitlePlaylist(cues []SubtitleCue) string {
    var sb strings.Builder
    sb.WriteString("#EXTM3U\n")
    sb.WriteString("#EXT-X-TARGETDURATION:4\n")
    sb.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
    
    for i, cue := range cues {
        // 🔹 Кожен к'ю — окремий "сегмент" у плейлисті субтитрів
        sb.WriteString(fmt.Sprintf("#EXTINF:4.0,\n"))
        sb.WriteString(fmt.Sprintf("sub_uk_%06d.vtt\n", i))
        
        // 🔹 Створити .vtt файл з к'ю
        vttContent := generateVTTFile(cue)
        os.WriteFile(fmt.Sprintf("sub_uk_%06d.vtt", i), []byte(vttContent), 0644)
    }
    
    sb.WriteString("#EXT-X-ENDLIST\n")
    return sb.String()
}

func generateVTTFile(cue SubtitleCue) string {
    var sb strings.Builder
    sb.WriteString("WEBVTT\n\n")
    
    // Формат к'ю: "00:00:00.000 --> 00:00:04.000 [налаштування]"
    endTime := cue.StartTime.Add(4 * time.Second)  // приклад: 4 секунди
    sb.WriteString(fmt.Sprintf("%s --> %s", 
        formatWebVTTTime(cue.StartTime), 
        formatWebVTTTime(endTime)))
    
    if cue.Position != "" {
        sb.WriteString(" " + cue.Position)
    }
    sb.WriteString("\n")
    
    // Текст субтитрів
    sb.WriteString(cue.Text + "\n\n")
    
    return sb.String()
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний формат часу в `ctim` | Плеєр не відображає субтитри або показує не вчасно | Використовуйте строгий формат `"HH:MM:SS.mmm"` з трьома цифрами мілісекунд |
| Відсутній `payl` бокс у `vttc` | К'ю ігнорується плеєром | Завжди додавайте `CuePayloadBox` з текстом |
| Неправильне кодування тексту | Спецсимволи відображаються як "кракозябри" | Використовуйте UTF-8 без BOM, перевіряйте кодування при записі |
| Відсутній `vttC` бокс у конфігурації | Глобальні стилі не застосовуються | Додайте `WebVTTConfigurationBox` з базовими стилями при створенні доріжки |
| Переповнення `sttg` налаштувань | Плеєр ігнорує всі налаштування к'ю | Використовуйте тільки підтримувані параметри: `align`, `line`, `position`, `size`, `region` |

---

## 📋 Чекліст для вашого проекту

```
[ ] При додаванні субтитрів:
    • Створіть текстову доріжку з `wvtt` Sample Entry у `stsd`
    • Додайте `vttC` бокс з базовими CSS-стилями
    • Для кожного к'ю: створіть `vttc` контейнер з `ctim` + `payl`

[ ] Для багатомовності:
    • Використовуйте `vlab` для міток мов ("українська", "english")
    • Додавайте `vsid` для швидкої фільтрації за мовою
    • Генеруйте окремий HLS-плейлист для кожної мови

[ ] Для доступності:
    • Додавайте `vtta` бокси з описом звуків для слабозорих
    • Використовуйте `sttg` з `region:lower-third` для не-перекриваючих субтитрів
    • Тестуйте на плеєрах з підтримкою аудіо-опису

[ ] Для дебагу:
    • Логуйте сирий текст к'ю: log.Printf("📝 Cue: %q", cueText)
    • Перевіряйте формат часу: if !isValidWebVTTTime(ctim) { ... }
    • Використовуйте `Stringify()` для виводу структури боксів

[ ] Для тестування:
    • Перевіряйте відтворення у різних плеєрах: VLC, hls.js, Safari, ExoPlayer
    • Тестуйте з різними кодуваннями тексту (UTF-8, кирилиця, емодзі)
    • Перевіряйте синхронізацію: субтитри мають з'являтися вчасно
```

---

## 🎯 Інтеграція у ваш CCTV HLS Processor

```
📡 Ваш потік обробки з субтитрами:
1. Приймаєте відео-потік + текстові субтитри (окремим каналом або вбудовані)
   │
   ▼
2. Парсите/генеруєте WebVTT к'ю:
   • Конвертуєте таймстемпи у формат "00:00:00.000"
   • Додаєте налаштування позиції (`sttg`)
   • Формуєте `vttc` контейнери з `ctim` + `payl`
   │
   ▼
3. Вбудовуєте у fMP4-сегмент:
   • Додаєте текстову доріжку у `stsd` (якщо ще немає)
   • Записуєте `vttc` бокси у `mdat` разом з відео/аудіо
   │
   ▼
4. Генеруєте HLS-плейлист:
   • Додаєте #EXT-X-MEDIA для субтитрів
   • Створюєте окремий .m3u8 для кожної мови
   │
   ▼
5. Клієнт відображає синхронізовані субтитри ✅
```

---

## ❓ Часті питання

**Q: Чи можу я використовувати HTML-теги у `CueText`?**  
A: Так! WebVTT підтримує обмежений набір: `<b>`, `<i>`, `<u>`, `<c.class>`, `<v.name>`. Але не всі плеєри підтримують стилізацію.

**Q: Як додати субтитри у вже існуючий HLS-стрім?**  
1. Додайте текстову доріжку у `moov` (якщо ще немає)  
2. Для кожного нового сегмента: додайте `vttc` бокси у `mdat`  
3. Оновіть головний .m3u8: додайте `#EXT-X-MEDIA` з посиланням на плейлист субтитрів  

**Q: Чи підтримує цей код автоматичне перекладення субтитрів?**  
A: Ні, це лише парсинг/запис формату. Для перекладу потрібен окремий сервіс (напр. Whisper + NLLB), який генерує нові `CueText` перед записом у MP4.

**Q: Як перевірити, чи коректні мої WebVTT-сегменти?**  
```bash
# Використайте ffprobe:
ffprobe -show_frames -select_streams s -print_format json segment.m4s

# Або спеціалізовані інструменти:
vtt-validator https://github.com/w3c/webvtt
```

---

## 🎯 Висновок

> **Цей код — ваш міст між WebVTT-субтитрами та бінарним форматом MP4**.  
> Він дозволяє:
> • ✅ Вбудовувати текстові доріжки прямо у fMP4-сегменти
> • ✅ Підтримувати багатомовність та доступність
> • ✅ Контролювати позицію та стиль субтитрів через `sttg`
> • ✅ Сумісність зі стандартом WebVTT та усіма сучасними плеєрами

Для вашого **CCTV HLS Processor** це означає:
- 📺 Клієнти бачать синхронізовані субтитри рідною мовою
- ♿ Підтримка аудіо-опису для слабозорих
- 🌐 Гнучке керування стилями через CSS у `vttC`
- 🔧 Легке додавання нових мов без перезбірки відео

Потребуєте допомоги з інтеграцією WebVTT у ваш конвеєр обробки або з генерацією багатомовних плейлистів? Напишіть — покажу готовий код! 🚀📝