# 🎬 WebVTT (Web Video Text Tracks) — Розбір прикладу

Це **WebVTT файл** — стандартний формат субтитрів для веб-відео (HTML5 `<video>`). Він підтримує не тільки текст і таймінги, а й **стилі, регіони, коментарі та позиціонування**.

Розберемо ваш приклад детально та покажу, як це інтегрується у ваш **CCTV HLS Processor**.

---

## 🗺️ Структура WebVTT файлу

```
┌────────────────────────────────────────┐
│ 📦 WebVTT File Anatomy                  │
├────────────────────────────────────────┤
│                                         │
│  1️⃣  Signature: WEBVTT                 │
│  2️⃣  NOTES: коментарі для людей        │
│  3️⃣  STYLE blocks: CSS для ::cue       │
│  4️⃣  Region definitions: макети екрану │
│  5️⃣  Cues: субтитри з таймінгами      │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔍 Детальний розбір вашого прикладу

### 1️⃣ Заголовок та коментарі

```vtt
WEBVTT

NOTE this a nice example
NOTE of a VTT
```

| Елемент | Призначення |
|---------|-------------|
| `WEBVTT` | **Обов'язкова сигнатура** — без неї файл не розпізнається |
| `NOTE` | Коментарі для людей/редакторів — ігноруються плеєром |

> 💡 **Порада**: Використовуйте `NOTE` для версіонування файлів або опису мови: `NOTE Language: Arabic, Channel: Al-Arabiya`

---

### 2️⃣ STYLE Blocks — CSS стилі для субтитрів

```vtt
STYLE
::cue(b) {
  color: peachpuff;
}
::cue(c) {
  color: white;
}

STYLE
::cue(a) {
  color: red;
}
::cue(d) {
  color: red;
  background-image: linear-gradient(to bottom, dimgray, lightgray);
}
```

| Селектор | Що стилізує | Приклад використання |
|----------|-------------|---------------------|
| `::cue(b)` | Текст у тегах `<b>` | Жирний текст кольору "peachpuff" |
| `::cue(c)` | Текст у тегах `<c>` або `<c.class>` | Білий текст для перекладу |
| `::cue(a)` | Текст у тегах `<a>` | Червоний для акцентів |
| `::cue(d)` | Текст у тегах `<d>` | Червоний з градієнтним фоном |

### ✅ Ваш use-case: кольорове кодування мов

```css
/* У вашому WebVTT для мультиязычних субтитрів: */
STYLE
::cue(.ar) { color: #FFD700; }  /* золотий — арабська */
::cue(.en) { color: #4169E1; }  /* блакитний — англійська */
::cue(.ru) { color: #DC143C; }  /* червоний — російська */
```

```vtt
<!-- Використання у cue: -->
<c.ar>مرحبا بكم</c>
<c.en>Welcome</c>
<c.ru>Добро пожаловать</c>
```

> ⚠️ **Увага**: Не всі браузери підтримують складні CSS у `::cue` (напр. `background-image`). Для максимальної сумісності використовуйте прості властивості: `color`, `font-weight`, `text-shadow`.

---

### 3️⃣ Region Definitions — макети для позиціонування

```vtt
Region: id=fred width=40% lines=3 regionanchor=0%,100% viewportanchor=10%,90% scroll=up
Region: id=bill width=40% lines=3 regionanchor=100%,100% viewportanchor=90%,90% scroll=up
```

| Атрибут | Значення | Пояснення |
|---------|----------|-----------|
| `id` | `fred`, `bill` | Унікальний ідентифікатор регіону |
| `width` | `40%` | Ширина регіону відносно відео |
| `lines` | `3` | Максимальна кількість рядків тексту |
| `regionanchor` | `0%,100%` | Точка прив'язки **всередині регіону** (0%,100% = лівий нижній кут) |
| `viewportanchor` | `10%,90%` | Точка прив'язки **на екрані** (10%,90% = 10% зліва, 90% зверху) |
| `scroll` | `up` | Прокрутка тексту вгору при переповненні |

### 🎯 Візуалізація регіонів:

```
Екран відео (100%×100%):
┌────────────────────────┐
│                        │
│   [Регіон "fred"]      │
│   viewportanchor=10%,90%│
│   regionanchor=0%,100%  │
│   ┌────────────┐       │
│   │текст рядок 1│       │
│   │текст рядок 2│       │
│   │текст рядок 3│       │
│   └────────────┘       │
│                        │
│   [Регіон "bill"]      │
│   viewportanchor=90%,90%│
│   regionanchor=100%,100%│
│            ┌────────┐  │
│            │текст 1 │  │
│            │текст 2 │  │
│            │текст 3 │  │
│            └────────┘  │
└────────────────────────┘
```

### ✅ Ваш use-case: позиціонування перекладів

```go
// У вашому pipeline: створення регіонів для різних мов
func (p *SubtitleExporter) createLanguageRegions() map[string]*astisub.Region {
    return map[string]*astisub.Region{
        "ar": {  // Арабська — знизу по центру
            ID: "arabic_region",
            InlineStyle: &astisub.StyleAttributes{
                WebVTTRegionAnchor:   "50%,100%",  // центр-низ регіону
                WebVTTViewportAnchor: "50%,85%",   // 85% висоти екрану
                WebVTTWidth:          "80%",
                WebVTTLines:          3,
                WebVTTScroll:         "up",
            },
        },
        "en": {  // Англійська — трохи вище
            ID: "english_region",
            InlineStyle: &astisub.StyleAttributes{
                WebVTTRegionAnchor:   "50%,100%",
                WebVTTViewportAnchor: "50%,70%",   // 70% висоти
                WebVTTWidth:          "80%",
                WebVTTLines:          3,
                WebVTTScroll:         "up",
            },
        },
    }
}
```

---

### 4️⃣ Cues — субтитри з таймінгами та метаданими

#### Cue 1: Простий субтитр з регіоном

```vtt
1
00:01:39 --> 00:01:41.04 region:bill
(deep rumbling)
```

| Частина | Значення |
|---------|----------|
| `1` | Номер субтитру (опціонально, astisub ігнорує) |
| `00:01:39 --> 00:01:41.04` | Таймінг: початок → кінець |
| `region:bill` | Посилання на регіон `bill` (визначений вище) |
| `(deep rumbling)` | Текст субтитру |

#### Cue 2: Субтитр з позиціонуванням та вирівнюванням

```vtt
2
00:02:04.08 --> 00:02:07.12  region:fred position:10%,start align:left size:35%
MAN:
How did we end up here?
```

| Атрибут cue | Значення | Пояснення |
|-------------|----------|-----------|
| `position:10%,start` | Позиція по горизонталі | 10% від лівого краю, вирівнювання `start` (для LTR — ліворуч) |
| `align:left` | Вирівнювання тексту | Текст вирівняний по лівому краю |
| `size:35%` | Ширина текстового блоку | 35% від ширини регіону/екрану |

> 💡 **Порада**: `position` і `align` працюють разом: `position:50%,center align:center` = текст по центру екрану.

#### Cue 3-6: Інші приклади

```vtt
00:02:12.16 --> 00:02:15.20
This place is horrible.
```
- Простий субтитр без додаткових атрибутів — відображається у дефолтному регіоні (знизу по центру).

```vtt
5
00:02:28.32 --> 00:02:31.36
We don't belong
in this shithole.
```
- Багаторядковий субтитр — автоматичний перенос рядка.

```vtt
6
00:02:31.40 --> 00:02:33.44
(computer playing
electronic melody)
```
- Звукові ефекти у дужках — стандартна конвенція для опису не-мовного аудіо.

---

## ⚙️ Як astisub парсить цей файл

### Потік обробки в `ReadFromWebVTT()`:

```
1. Перевірка сигнатури "WEBVTT"
   ↓
2. Цикл по рядках:
   ├─ STYLE block → парсинг CSS, збереження у Metadata.WebVTTStyles
   ├─ Region: → парсинг атрибутів, створення astisub.Region
   ├─ NOTE: → ігнорування (або збереження у Metadata.Comments)
   ├─ Рядок з "-->" → це cue header:
   │  ├─ Парсинг таймінгів через parseDuration()
   │  ├─ Парсинг атрибутів: region:, position:, align:, size:
   │  └─ Створення astisub.Item з таймінгами та InlineStyle
   ├─ Наступні рядки без "-->" → текст cue:
   │  ├─ Парсинг тексту з тегами <b>, <i>, <c.class>
   │  └─ Додавання до поточного Item.Lines
   ↓
3. Повернення *Subtitles
```

### ✅ Ваш use-case: парсинг WebVTT з пам'яті

```go
// ProcessWebVTTSegment — обробка WebVTT-сегменту в реальному часі
func (p *SubtitleProcessor) ProcessWebVTTSegment(data []byte, seqNum uint64, pts time.Duration) error {
    // 1. Парсинг з bytes.Reader
    reader := bytes.NewReader(data)
    subs, err := astisub.ReadFromWebVTT(reader)
    if err != nil {
        return fmt.Errorf("webvtt parse: %w", err)
    }
    
    // 2. Корекція часу відносно початку стріму
    streamOffset := pts.Sub(p.streamStartTime)
    for _, item := range subs.Items {
        item.StartAt += streamOffset
        item.EndAt += streamOffset
    }
    
    // 3. Експорт тексту для перекладу (видаляємо теги)
    arabicText := p.extractTextWithoutTags(subs)
    
    // 4. Асинхронний переклад + TTS
    go p.translateAndSend(seqNum, arabicText, subs.Items)
    
    return nil
}
```

---

## 🔄 Конвертація між форматами

### WebVTT ↔ інші формати в astisub:

```go
// WebVTT → SRT (спрощення для сумісності)
subs, _ := astisub.ReadFromWebVTT(bytes.NewReader(webvttData))
var srtBuf bytes.Buffer
subs.WriteToSRT(&srtBuf)  // стилі/регіони втрачаються, залишається текст+таймінги

// SRT → WebVTT (додавання базових можливостей)
subs, _ := astisub.ReadFromSRT(bytes.NewReader(srtData))
var vttBuf bytes.Buffer
subs.WriteToWebVTT(&vttBuf)  // додає "WEBVTT" заголовок, базові ::cue стилі

// Teletext → WebVTT (повна конвертація атрибутів)
subs, _ := astisub.ReadFromTeletext(reader, opts)
// propagateSTLAttributes() автоматично конвертує:
// • STLJustification → WebVTTAlign
// • STLPosition.VerticalPosition → WebVTTLine
// • STLColor → <c.class> теги
```

### ✅ Ваш use-case: генерація WebVTT для HLS-плейлиста

```go
// GenerateWebVTTForHLS — створення WebVTT для сегменту HLS
func (p *HLSGenerator) GenerateWebVTTForHLS(subs *astisub.Subtitles, segmentStart time.Duration) ([]byte, error) {
    // 1. Фільтрація субтитрів для цього сегменту
    filtered := filterItemsByTime(subs, segmentStart, segmentStart+10*time.Second)
    
    // 2. Додавання регіонів для мов
    for lang, region := range p.createLanguageRegions() {
        filtered.Regions[region.ID] = region
        // Прив'язка регіону до субтитрів цієї мови
        for _, item := range filtered.Items {
            if detectLanguage(item) == lang {
                item.Region = region
            }
        }
    }
    
    // 3. Експорт у WebVTT
    var buf bytes.Buffer
    if err := filtered.WriteToWebVTT(&buf); err != nil {
        return nil, err
    }
    
    return buf.Bytes(), nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Плеєр ігнорує регіони** | Текст відображається у дефолтному місці | Переконайтеся, що `id` регіону співпадає з `region:XXX` у cue; деякі плеєри потребують `shaka-player` або `video.js` для підтримки регіонів |
| **Стилі не застосовуються** | `<c.ar>` не змінює колір | Додайте `::cue(.ar) { color: #FFD700; }` у STYLE block; перевірте, що клас у `<c.ar>` співпадає з селектором `::cue(.ar)` |
| **Таймінги "з'їжджають"** | Субтитри з'являються раніше/пізніше | Використовуйте однаковий формат часу (`HH:MM:SS.mmm`) та уникайте зайвих пробілів у таймінгах |
| **Багаторядковий текст не переноситься** | Текст виходить за межі екрану | Додайте `size:XX%` та `align:center` для контролю ширини; або використовуйте `\n` для явного переносу |
| **Арабський текст відображається "задом наперед"** | Проблемы з RTL направленням | Додайте `direction: rtl` у `::cue(.ar)`; або використовуйте Unicode RTL маркери у тексті |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування парсингу атрибутів cue:

```go
type CachedCueAttributes struct {
    raw      string
    region   string
    position string
    align    string
    size     string
}

var cueAttrCache = sync.Map{}  // attrString → CachedCueAttributes

func parseCueAttributesCached(raw string) *CachedCueAttributes {
    if cached, ok := cueAttrCache.Load(raw); ok {
        return cached.(*CachedCueAttributes)
    }
    
    attrs := parseCueAttributes(raw)  // ваша функція парсингу
    cueAttrCache.Store(raw, attrs)
    return attrs
}
```

### 2. Пакетна генерація таймінгів:

```go
// Замість індивідуального formatDuration для кожного таймінгу:
func batchFormatWebVTTSRTTimestamps(items []*astisub.Item) [][2]string {
    results := make([][2]string, len(items))
    for i, item := range items {
        results[i][0] = formatDuration(item.StartAt, ".", 3)  // "00:01:39.000"
        results[i][1] = formatDuration(item.EndAt, ".", 3)
    }
    return results
}
```

### 3. Lazy CSS генерація для STYLE blocks:

```go
// Не генеруйте CSS до моменту експорту
type LazyWebVTTStyles struct {
    styles   map[string]string  // class → CSS rules
    rendered string
    dirty    bool
}

func (l *LazyWebVTTStyles) Render() string {
    if l.dirty || l.rendered == "" {
        var buf strings.Builder
        buf.WriteString("STYLE\n")
        for class, rules := range l.styles {
            buf.WriteString(fmt.Sprintf("::cue(.%s) {\n  %s\n}\n", class, rules))
        }
        l.rendered = buf.String()
        l.dirty = false
    }
    return l.rendered
}
```

---

## 📋 Чек-лист інтеграції WebVTT

```go
// ✅ 1. Парсинг без проміжних файлів
reader := bytes.NewReader(vttData)
subs, err := astisub.ReadFromWebVTT(reader)

// ✅ 2. Корекція таймінгів під сегмент
streamOffset := pts.Sub(streamStartTime)
for _, item := range subs.Items {
    item.StartAt += streamOffset
    item.EndAt += streamOffset
}

// ✅ 3. Додавання регіонів/стилів для мов
for lang, region := range createLanguageRegions() {
    subs.Regions[region.ID] = region
}
addWebVTTStyles(subs, LanguageWebVTTStyles)  // ваша функція

// ✅ 4. Експорт у цільовий формат
var buf bytes.Buffer
switch targetFormat {
case "webvtt":
    subs.WriteToWebVTT(&buf)
case "srt":
    subs.WriteToSRT(&buf)  // стилі/регіони втрачаються
}

// ✅ 5. Валідація арабського тексту
if err := validateArabicText(extractTextWithoutTags(subs)); err != nil {
    log.Warn("arabic text validation", "err", err)
}

// ✅ 6. Метрики
monitoring.WebVTTParsed.Inc()
monitoring.WebVTTParseLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 📄 [WebVTT Spec (W3C)](https://w3c.github.io/webvtt/) — офіційна специфікація
- 📄 [WebVTT CSS Extensions](https://w3c.github.io/webvtt/#css-extensions) — підтримка `::cue`, регіонів
- 💻 [astisub webvtt.go](https://github.com/asticode/go-astisub/blob/master/webvtt.go) — вихідний код парсингу
- 🎬 [video.js VTT Support](https://docs.videojs.com/tutorial-text-tracks.html) — підтримка у популярному плеєрі
- 🧪 [astisub testdata](https://github.com/asticode/go-astisub/tree/master/testdata) — приклади WebVTT файлів

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **реальним часом** та **арабською мовою**:
> 1. **Використовуйте `::cue(.ar) { direction: rtl; }`** для коректного відображення арабського тексту.
> 2. **Кешуйте регіони на рівні каналу** — вони не змінюються між сегментами.
> 3. **Уникайте складних CSS у `::cue`** — не всі плеєри підтримують `background-image`, `linear-gradient`.
> 4. **Тестуйте у цільовому плеєрі** (Shaka, video.js, hls.js) — підтримка регіонів/стилів відрізняється.
> 5. **Додайте `NOTE Language: ara`** у заголовок WebVTT — допомагає плеєрам з авто-вибором доріжки.

Потрібен приклад функції `addWebVTTStyles()` для додавання кольорових класів для арабської/англійської/російської мов? Готовий допомогти! 🚀