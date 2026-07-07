# 🔍 `probe`: Інструмент для структурованого звіту про MP4-файли

Це **практичний CLI-інструмент** на основі бібліотеки `go-mp4`, який аналізує вхідний MP4/fMP4 файл та генерує **структурований звіт** у форматі JSON або YAML з детальною інформацією про метадані, доріжки, кодеки та бітрейти.

---

## 🎯 Коротка відповідь

> **Це "аналізатор метаданих" для MP4**: він швидко сканує файл, витягує ключову інформацію (кодеки, роздільність, тривалість, бітрейт) та виводить у машино-читабельному форматі — ідеально для автоматизації, валідації та інтеграції з іншими системами.

---

## 🗂️ Структура інструменту

```bash
# 🔹 Базовий синтаксис:
$ mp4tool beta probe [OPTIONS] INPUT.mp4

# 🔹 Доступні опції:
--format=FORMAT   # 🔹 Формат виводу: json (за замовчуванням) або yaml

# 🔹 Приклади:
# Базовий аналіз у JSON:
$ mp4tool beta probe video.mp4

# Аналіз у YAML для кращої читабельності:
$ mp4tool beta probe -format=yaml video.mp4

# Інтеграція з іншими інструментами:
$ mp4tool beta probe video.mp4 | jq '.tracks[] | {codec, bitrate, resolution}'
```

---

## 🧱 Основні компоненти

### 🔹 Конфігурація через прапорці

```go
func Main(args []string) int {
    flagSet := flag.NewFlagSet("fragment", flag.ExitOnError)
    
    // 🔹 Опція: формат виводу (json/yaml)
    format := flagSet.String("format", "json", "output format (yaml|json)")
    
    flagSet.Usage = func() {
        println("USAGE: mp4tool beta probe [OPTIONS] INPUT.mp4")
        flagSet.PrintDefaults()
    }
    flagSet.Parse(args)
    
    // 🔹 Перевірка аргументів
    if len(flagSet.Args()) < 1 {
        flagSet.Usage()
        return 1
    }
    
    // 🔹 Відкриття файлу
    input, err := os.Open(flagSet.Args()[0])
    if err != nil {
        fmt.Println("Failed to open the input file:", err)
        return 1
    }
    defer input.Close()
    
    // 🔹 Буферизований читач (менший blockSize=1KB для швидкого сканування)
    r := bufseekio.NewReadSeeker(input, 1024, 4)
    
    // 🔹 Побудова звіту
    rep, err := buildReport(r)
    if err != nil {
        fmt.Println("Error:", err)
        return 1
    }
    
    // 🔹 Вивід у вказаному форматі
    switch *format {
    case "json":
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")  // 🔹 Красиве форматування
        err = enc.Encode(rep)
    default:
        err = yaml.NewEncoder(os.Stdout).Encode(rep)
    }
    // ...
}
```

**🎯 Призначення**: Забезпечити гнучкий вивід для різних сценаріїв:
- ✅ **JSON**: для інтеграції з API, jq, веб-інтерфейсами
- ✅ **YAML**: для читабельності у логах, конфігураціях, документації

---

### 🔹 Структура звіту: `report` та `track`

```go
type report struct {
    MajorBrand       string   `yaml:"major_brand"`        // 🔹 Основний бренд файлу
    MinorVersion     uint32   `yaml:"minor_version"`      // 🔹 Мінорна версія
    CompatibleBrands []string `yaml:"compatible_brands"`  // 🔹 Список сумісних брендів
    FastStart        bool     `yaml:"fast_start"`         // 🔹 Оптимізація для web (moov перед mdat)
    Timescale        uint32   `yaml:"timescale"`          // 🔹 Глобальна частота дискретизації
    Duration         uint64   `yaml:"duration"`           // 🔹 Тривалість у одиницях timescale
    DurationSeconds  float32  `yaml:"duration_seconds"`   // 🔹 Тривалість у секундах (зручно для людей)
    Tracks           []*track `yaml:"tracks"`             // 🔹 Список доріжок
}

type track struct {
    TrackID         uint32  `yaml:"track_id"`              // 🔹 Унікальний ID доріжки
    Timescale       uint32  `yaml:"timescale"`             // 🔹 Частота дискретизації доріжки
    Duration        uint64  `yaml:"duration"`              // 🔹 Тривалість у одиницях timescale
    DurationSeconds float32 `yaml:"duration_seconds"`      // 🔹 Тривалість у секундах
    Codec           string  `yaml:"codec"`                 // 🔹 Стандартний рядок кодека: avc1.64001f, mp4a.40.2
    Encrypted       bool    `yaml:"encrypted"`             // 🔹 Чи зашифрована доріжка (DRM)
    
    // 🔹 Відео-специфічні поля (опціональні, тільки для відео)
    Width  uint16 `json:",omitempty" yaml:"width,omitempty"`
    Height uint16 `json:",omitempty" yaml:"height,omitempty"`
    
    // 🔹 Статистика семплів/чанків
    SampleNum   int `json:",omitempty" yaml:"sample_num,omitempty"`   // 🔹 Кількість кадрів/семплів
    ChunkNum    int `json:",omitempty" yaml:"chunk_num,omitempty"`    // 🔹 Кількість чанків
    IDRFrameNum int `json:",omitempty" yaml:"idr_frame_num,omitempty"` // 🔹 Кількість ключових кадрів (H.264)
    
    // 🔹 Бітрейт (опціональні)
    Bitrate    uint64 `json:",omitempty" yaml:"bitrate,omitempty"`     // 🔹 Середній бітрейт
    MaxBitrate uint64 `json:",omitempty" yaml:"max_bitrate,omitempty"` // 🔹 Піковий бітрейт
}
```

**🎯 Призначення**: Забезпечити **машино-читабельний формат** з людсько-зрозумілими полями:
- ✅ `DurationSeconds` замість розрахунку `Duration / Timescale` вручну
- ✅ Стандартні рядки кодеків (`avc1.64001f`) для сумісності з плеєрами
- ✅ Опціональні поля з `omitempty` для компактності виводу

---

### 🔹 Основна логіка: `buildReport()`

```go
func buildReport(r io.ReadSeeker) (*report, error) {
    // 🔹 Крок 1: Швидкий аналіз файлу через mp4.Probe()
    info, err := mp4.Probe(r)
    if err != nil { return nil, err }
    
    // 🔹 Крок 2: Побудова базового звіту
    rep := &report{
        MajorBrand:       string(info.MajorBrand[:]),
        MinorVersion:     info.MinorVersion,
        CompatibleBrands: make([]string, 0, len(info.CompatibleBrands)),
        FastStart:        info.FastStart,
        Timescale:        info.Timescale,
        Duration:         info.Duration,
        DurationSeconds:  float32(info.Duration) / float32(info.Timescale),  // 🔹 Конвертація у секунди
        Tracks:           make([]*track, 0, len(info.Tracks)),
    }
    
    // 🔹 Крок 3: Конвертація сумісних брендів у рядки
    for _, brand := range info.CompatibleBrands {
        rep.CompatibleBrands = append(rep.CompatibleBrands, string(brand[:]))
    }
    
    // 🔹 Крок 4: Обробка кожної доріжки
    for _, tr := range info.Tracks {
        // 🔹 Розрахунок бітрейту: спочатку з семплів, потім з фрагментів (для fMP4)
        bitrate := tr.Samples.GetBitrate(tr.Timescale)
        maxBitrate := tr.Samples.GetMaxBitrate(tr.Timescale, uint64(tr.Timescale))
        if bitrate == 0 || maxBitrate == 0 {
            // 🔹 Fallback на фрагменти для fMP4
            bitrate = info.Segments.GetBitrate(tr.TrackID, tr.Timescale)
            maxBitrate = info.Segments.GetMaxBitrate(tr.TrackID, tr.Timescale)
        }
        
        t := &track{
            TrackID:         tr.TrackID,
            Timescale:       tr.Timescale,
            Duration:        tr.Duration,
            DurationSeconds: float32(tr.Duration) / float32(tr.Timescale),
            Encrypted:       tr.Encrypted,
            Bitrate:         bitrate,
            MaxBitrate:      maxBitrate,
            SampleNum:       len(tr.Samples),
            ChunkNum:        len(tr.Chunks),
        }
        
        // 🔹 Крок 5: Визначення рядка кодека
        switch tr.Codec {
        case mp4.CodecAVC1:  // 🔹 H.264 відео
            if tr.AVC != nil {
                // 🔹 Стандартний формат: avc1.PPCCLL (Profile-Compatibility-Level)
                t.Codec = fmt.Sprintf("avc1.%02X%02X%02X",
                    tr.AVC.Profile,              // 🔹 Profile: 66=Baseline, 77=Main, 100=High
                    tr.AVC.ProfileCompatibility, // 🔹 Compatibility mask
                    tr.AVC.Level,                // 🔹 Level: 30=3.0, 41=4.1, 51=5.1
                )
                t.Width = tr.AVC.Width   // 🔹 Роздільність
                t.Height = tr.AVC.Height
            } else {
                t.Codec = "avc1"  // 🔹 Fallback без деталей
            }
            // 🔹 Пошук ключових кадрів (IDR) для H.264
            idxs, err := mp4.FindIDRFrames(r, tr)
            if err != nil { return nil, err }
            t.IDRFrameNum = len(idxs)
            
        case mp4.CodecMP4A:  // 🔹 AAC аудіо
            if tr.MP4A == nil || tr.MP4A.OTI == 0 {
                t.Codec = "mp4a"
            } else if tr.MP4A.AudOTI == 0 {
                t.Codec = fmt.Sprintf("mp4a.%X", tr.MP4A.OTI)  // 🔹 mp4a.40
            } else {
                t.Codec = fmt.Sprintf("mp4a.%X.%d", tr.MP4A.OTI, tr.MP4A.AudOTI)  // 🔹 mp4a.40.2
            }
            
        default:
            t.Codec = "unknown"
        }
        
        rep.Tracks = append(rep.Tracks, t)
    }
    
    return rep, nil
}
```

**🔄 Потік даних:**
```
🔹 Вхід: io.ReadSeeker (файл)
│
▼
🔹 mp4.Probe(r) → швидкий аналіз структури файлу
│
▼
🔹 Побудова report:
   │
   ├── 🔹 Базові метадані: бренди, timescale, duration
   │
   ├── 🔹 Для кожної доріжки:
   │   • Розрахунок бітрейту (семпли → фрагменти)
   │   • Форматування кодека у стандартний рядок
   │   • Пошук IDR-кадрів для H.264
   │   • Додавання роздільності для відео
   │
   ▼
🔹 Вихід: *report з усіма метаданими
```

---

## 🔍 Ключові особливості

### 🔹 Стандартні рядки кодеків

```go
// 🔹 H.264: avc1.PPCCLL
t.Codec = fmt.Sprintf("avc1.%02X%02X%02X",
    tr.AVC.Profile,              // 🔹 Profile: 0x42=66 (Baseline), 0x4D=77 (Main), 0x64=100 (High)
    tr.AVC.ProfileCompatibility, // 🔹 Compatibility mask (бітова маска)
    tr.AVC.Level,                // 🔹 Level: 0x1E=30 (3.0), 0x28=40 (4.0), 0x33=51 (5.1)
)

// 🔹 AAC: mp4a.OTI.AudOTI
if tr.MP4A.AudOTI == 0 {
    t.Codec = fmt.Sprintf("mp4a.%X", tr.MP4A.OTI)  // 🔹 mp4a.40
} else {
    t.Codec = fmt.Sprintf("mp4a.%X.%d", tr.MP4A.OTI, tr.MP4A.AudOTI)  // 🔹 mp4a.40.2 (AAC LC)
}
```

**🎯 Призначення**: Забезпечити **сумісність зі стандартами** (HLS, DASH, MPEG-DASH), де кодеки вказуються у форматі `avc1.64001f`, `mp4a.40.2`.

**🔢 Приклади:**
| Codec | Profile | Compatibility | Level | Рядок |
|-------|---------|--------------|-------|-------|
| H.264 Baseline 3.0 | 0x42 (66) | 0x00 | 0x1E (30) | `avc1.42001e` |
| H.264 High 4.1 | 0x64 (100) | 0x00 | 0x28 (40) | `avc1.640028` |
| AAC LC | 0x40 | 2 | - | `mp4a.40.2` |
| HE-AAC | 0x40 | 5 | - | `mp4a.40.5` |

---

### 🔹 Розрахунок бітрейту: семпли → фрагменти

```go
// 🔹 Спочатку пробуємо розрахувати з семплів (для звичайних MP4)
bitrate := tr.Samples.GetBitrate(tr.Timescale)
maxBitrate := tr.Samples.GetMaxBitrate(tr.Timescale, uint64(tr.Timescale))

// 🔹 Якщо не вдалося (fMP4) → пробуємо з фрагментів
if bitrate == 0 || maxBitrate == 0 {
    bitrate = info.Segments.GetBitrate(tr.TrackID, tr.Timescale)
    maxBitrate = info.Segments.GetMaxBitrate(tr.TrackID, tr.Timescale)
}
```

**🎯 Призначення**: Підтримка **як звичайних, так і фрагментованих файлів**:
- ✅ Звичайні MP4: бітрейт розраховується з `stts`/`stsz` таблиць
- ✅ fMP4: бітрейт розраховується з `moof`/`mdat` фрагментів

**🔢 Формула бітрейту:**
```
бітрейт = (загальний_розмір_у_байтах × 8 × timescale) / загальна_тривалість_у_одиницях
```

---

### 🔹 Пошук IDR-кадрів для H.264

```go
// 🔹 Тільки для H.264 відео
if tr.Codec == mp4.CodecAVC1 {
    idxs, err := mp4.FindIDRFrames(r, tr)
    if err != nil { return nil, err }
    t.IDRFrameNum = len(idxs)  // 🔹 Кількість ключових кадрів
}
```

**🎯 Призначення**: Визначити кількість **ключових кадрів (IDR)** — критично для:
- ✅ Оцінки якості seek (більше IDR = краща навігація)
- ✅ Валідації відповідності вимогам стрімінгу (напр. ключовий кадр кожні 2-4 секунди)
- ✅ Оптимізації енкодингу (частота GOP)

---

## 🛠️ Практичне використання

### 🔹 Приклад 1: Швидка валідація вхідного файлу

```bash
# 🔹 Перевірка, чи файл має відео-доріжку H.264:
$ mp4tool beta probe video.mp4 | jq '.tracks[] | select(.codec | startswith("avc1"))'

# 🔹 Перевірка бітрейту для адаптивного стрімінгу:
$ mp4tool beta probe video.mp4 | jq '.tracks[] | {codec, bitrate, max_bitrate}'

# 🔹 Перевірка оптимізації для web (FastStart):
$ mp4tool beta probe video.mp4 | jq '.fast_start'
# true = ✅ оптимізовано, false = ❌ потребує оптимізації
```

---

### 🔹 Приклад 2: Генерація метаданих для HLS-плейлиста

```bash
# 🔹 Отримання параметрів для #EXT-X-STREAM-INF:
$ mp4tool beta probe video.mp4 | jq -r '
  .tracks[] | 
  select(.codec | startswith("avc1")) | 
  "#EXT-X-STREAM-INF:BANDWIDTH=\(.bitrate),CODECS=\"\(.codec)\",RESOLUTION=\(.width)x\(.height)"
'

# 🔹 Результат:
# #EXT-X-STREAM-INF:BANDWIDTH=1500000,CODECS="avc1.64001f",RESOLUTION=1280x720
```

---

### 🔹 Приклад 3: Інтеграція у CCTV HLS Processor

```go
// 🔹 Функція для отримання метаданих сегмента
func getSegmentMetadata(filePath string) (*SegmentMetadata, error) {
    // 🔹 Запускаємо probe як підпроцес
    cmd := exec.Command("mp4tool", "beta", "probe", "-format=json", filePath)
    
    var stdout bytes.Buffer
    cmd.Stdout = &stdout
    
    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("probe failed: %w", err)
    }
    
    // 🔹 Парсинг JSON-відповіді
    var report struct {
        Tracks []*struct {
            Codec    string `json:"codec"`
            Bitrate  uint64 `json:"bitrate"`
            Width    uint16 `json:"width"`
            Height   uint16 `json:"height"`
            Encrypted bool  `json:"encrypted"`
        } `json:"tracks"`
    }
    
    if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
        return nil, fmt.Errorf("json parse failed: %w", err)
    }
    
    // 🔹 Пошук відео-доріжки
    for _, tr := range report.Tracks {
        if strings.HasPrefix(tr.Codec, "avc1") {
            return &SegmentMetadata{
                Codec:     tr.Codec,
                Bitrate:   tr.Bitrate,
                Width:     tr.Width,
                Height:    tr.Height,
                Encrypted: tr.Encrypted,
            }, nil
        }
    }
    
    return nil, fmt.Errorf("no video track found")
}

// 🔹 Використання у конвеєрі:
go func() {
    for segment := range segmentQueue {
        meta, err := getSegmentMetadata(segment.Path)
        if err != nil {
            log.Printf("❌ Failed to probe %s: %v", segment.Path, err)
            continue
        }
        
        // 🔹 Валідація параметрів
        if meta.Bitrate > 5_000_000 {
            log.Printf("⚠️  High bitrate %d in %s", meta.Bitrate, segment.Path)
        }
        if meta.Encrypted {
            log.Printf("🔐 Encrypted segment: %s", segment.Path)
            // 🔹 Додаткова обробка DRM...
        }
    }
}()
```

---

### 🔹 Приклад 4: Моніторинг якості записів

```bash
# 🔹 Скрипт для перевірки якості записів:
#!/bin/bash
for file in recordings/*.mp4; do
    echo "📊 Аналіз: $file"
    
    # 🔹 Отримання основних параметрів
    mp4tool beta probe "$file" | jq -r '
        "  🎬 Codec: \(.tracks[] | select(.width) | .codec)",
        "  📐 Resolution: \(.tracks[] | select(.width) | "\(.width)x\(.height)")",
        "  📡 Bitrate: \(.tracks[] | select(.bitrate) | "\(.bitrate / 1000 | floor) kbps")",
        "  🔑 IDR frames: \(.tracks[] | select(.idr_frame_num) | .idr_frame_num)",
        "  ⚡ FastStart: \(.fast_start)",
        ""
    '
done
```

**🔹 Приклад виводу:**
```
📊 Аналіз: recordings/cam1_20240101.mp4
  🎬 Codec: avc1.64001f
  📐 Resolution: 1920x1080
  📡 Bitrate: 2500 kbps
  🔑 IDR frames: 45
  ⚡ FastStart: true
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний формат кодека | Плеєр не розпізнає CODECS у плейлисті | Використовуйте стандартний формат: `avc1.PPCCLL`, `mp4a.OTI.AudOTI` |
| Ігнорування `DurationSeconds` | Неправильний розрахунок тривалості у секундах | Завжди використовуйте `DurationSeconds` замість ручного розрахунку |
| Забути перевірку `Encrypted` | Спроба відтворити DRM-контент без ключів | Перевіряйте `Encrypted: true` перед обробкою сегмента |
| Неправильний розрахунок бітрейту | Перевищення лімітів мережі → буферизація | Використовуйте `MaxBitrate` для валідації пікових навантажень |
| Ігнорування `IDRFrameNum` | Погана навігація при seek | Перевіряйте частоту IDR: `DurationSeconds / IDRFrameNum` має бути ≤ 4с |

---

## 📋 Чекліст для вашого проекту

```
[ ] При валідації вхідних файлів:
    • Перевіряйте наявність відео-доріжки з avc1 кодеком
    • Валідуйте роздільність: Width/Height ≥ 640×360 для веб-стрімінгу
    • Перевіряйте FastStart: false = потребує оптимізації перед публікацією

[ ] Для генерації плейлистів:
    • Використовуйте стандартні рядки кодеків: avc1.64001f, mp4a.40.2
    • Розраховуйте BANDWIDTH з Bitrate/MaxBitrate для адаптивного вибору
    • Додавайте RESOLUTION тільки для відео-доріжок

[ ] Для моніторингу якості:
    • Логувайте бітрейт: попередження при >5 Mbps для мобільних мереж
    • Перевіряйте частоту IDR: DurationSeconds / IDRFrameNum ≤ 4с
    • Відстежуйте Encrypted: окрема обробка для DRM-контенту

[ ] Для інтеграції з іншими системами:
    • Використовуйте -format=json для машинної обробки
    • Парсіть вивід через jq (bash) або json.Unmarshal (Go)
    • Кешуйте результати probe для повторного використання

[ ] Для дебагу:
    • Порівнюйте вивід probe з очікуваними значеннями
    • Перевіряйте DurationSeconds: має співпадати з реальною тривалостю
    • Тестуйте з різними типами файлів: звичайні, фрагментовані, DRM
```

---

## 🎯 Висновок

> **`probe` — це "універсальний сканер" для метаданих MP4**, який забезпечує:
> • ✅ Швидкий аналіз структури файлу без повного парсингу медіа-даних
> • ✅ Стандартизовані рядки кодеків для сумісності з HLS/DASH
> • ✅ Автоматичний розрахунок бітрейту для адаптивного стрімінгу
> • ✅ Підтримку як звичайних, так і фрагментованих файлів
> • ✅ Гнучкий вивід у JSON/YAML для інтеграції з іншими системами

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Миттєва валідація вхідних записів перед додаванням у стрімінг
- 🔧 Автоматична генерація параметрів для HLS-плейлистів
- 📊 Моніторинг якості: бітрейт, роздільність, частота ключових кадрів
- 🔄 Легка інтеграція з Python/Node.js скриптами через JSON-вивід
- 🛡️ Безпечна обробка DRM-контенту через прапорець `Encrypted`

Потребуєте допомоги з інтеграцією `probe` у ваш конвеєр валідації або з налаштуванням кастомного формату звіту? Напишіть — покажу готовий код для вашого сценарію! 🚀🔍