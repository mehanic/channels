# 🧪 Тест `TestProbe`: Перевірка інструменту аналізу метаданих MP4

Це **інтеграційний тест** для модуля `probe`, який перевіряє коректність роботи **CLI-інструменту `mp4tool beta probe`** — генерацію структурованих звітів про метадані файлу у форматах JSON та YAML.

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що `probe` коректно аналізує будь-який MP4-файл та генерує стабільний, машино-читабельний звіт** — з підтримкою різних форматів виводу, точним розрахунком тривалості, бітрейту та стандартними рядками кодеків.

---

## 📋 Структура тесту

```go
func TestProbe(t *testing.T) {
    testCases := []struct {
        name    string          // 🔹 Назва тест-кейсу
        file    string          // 🔹 Шлях до тестового файлу
        options []string        // 🔹 Прапорці командного рядка
        wants   string          // 🔹 Очікуваний вивід (golden file)
    }{
        {
            name:  "sample.mp4 no-options",
            file:  "../../../../testdata/sample.mp4",
            wants: sampleMP4JSONOutput,  // 🔹 Базовий JSON-вивід
        },
        {
            name:    "sample.mp4 format-json",
            file:    "../../../../testdata/sample.mp4",
            options: []string{"-format", "json"},  // 🔹 Явний вибір JSON
            wants:   sampleMP4JSONOutput,
        },
        {
            name:    "sample.mp4 format-yaml",
            file:    "../../../../testdata/sample.mp4",
            options: []string{"-format", "yaml"},  // 🔹 Вивід у YAML
            wants:   sampleMP4YamlOutput,
        },
    }
    
    for _, tc := range testCases {
        // 🔹 Перехоплення stdout через pipe (як у dump/extract)
        stdout := os.Stdout
        r, w, err := os.Pipe()
        require.NoError(t, err)
        defer func() { os.Stdout = stdout }()
        os.Stdout = w
        
        // 🔹 Запуск Main у горутині
        go func() {
            require.Zero(t, Main(append(tc.options, tc.file)))
            w.Close()
        }()
        
        // 🔹 Читання виводу
        b, err := io.ReadAll(r)
        require.NoError(t, err)
        
        // 🔹 Порівняння з очікуваним
        assert.Equal(t, tc.wants, string(b))
    }
}
```

**🎯 Призначення**: Перевірити, що інструмент `probe`:
- ✅ Коректно обробляє різні формати виводу (JSON/YAML)
- ✅ Генерує стабільний, детермінований вивід для порівняння
- ✅ Точно розраховує тривалість, бітрейт та інші метрики
- ✅ Форматує кодеки у стандартні рядки (avc1.64000C, mp4a.40.2)

---

## 🔍 Детальний розбір тест-кейсів

### 🔹 Кейс 1: Базовий вивід у JSON (за замовчуванням)

```go
{
    name:  "sample.mp4 no-options",
    file:  "../../../../testdata/sample.mp4",
    wants: sampleMP4JSONOutput,
},
```

**📊 Очікуваний JSON-вивід (фрагмент):**
```json
{
  "MajorBrand": "isom",
  "MinorVersion": 512,
  "CompatibleBrands": [
    "isom",
    "iso2",
    "avc1",
    "mp41"
  ],
  "FastStart": false,
  "Timescale": 1000,
  "Duration": 1024,
  "DurationSeconds": 1.024,
  "Tracks": [
    {
      "TrackID": 1,
      "Timescale": 10240,
      "Duration": 10240,
      "DurationSeconds": 1,
      "Codec": "avc1.64000C",
      "Encrypted": false,
      "Width": 320,
      "Height": 180,
      "SampleNum": 10,
      "ChunkNum": 9,
      "IDRFrameNum": 1,
      "Bitrate": 40336,
      "MaxBitrate": 40336
    },
    {
      "TrackID": 2,
      "Timescale": 44100,
      "Duration": 45124,
      "DurationSeconds": 1.02322,
      "Codec": "mp4a.40.2",
      "Encrypted": false,
      "SampleNum": 44,
      "ChunkNum": 9,
      "Bitrate": 10570,
      "MaxBitrate": 10632
    }
  ]
}
```

**🔑 Ключові перевірки:**

| Поле | Очікуване значення | Чому це важливо |
|------|-------------------|----------------|
| `MajorBrand` | `"isom"` | ✅ Базовий стандарт файлу (ISO Base Media File Format) |
| `CompatibleBrands` | `["isom","iso2","avc1","mp41"]` | ✅ Сумісність з різними плеєрами та стандартами |
| `FastStart` | `false` | ⚠️ Файл не оптимізовано для web-стрімінгу (moov після mdat) |
| `DurationSeconds` | `1.024` | ✅ Зручне представлення тривалості для людей |
| `Codec` (відео) | `"avc1.64000C"` | ✅ Стандартний рядок для HLS/DASH: Profile=0x64 (High), Compatibility=0x00, Level=0x0C (1.2) |
| `Codec` (аудіо) | `"mp4a.40.2"` | ✅ Стандартний рядок: OTI=0x40 (AAC), AudOTI=2 (AAC LC) |
| `Width`/`Height` | `320`/`180` | ✅ Роздільність відео для адаптивного стрімінгу |
| `IDRFrameNum` | `1` | ✅ Кількість ключових кадрів для оцінки якості seek |
| `Bitrate`/`MaxBitrate` | `40336`/`40336` | ✅ Середній та піковий бітрейт для мережевої оптимізації |

---

### 🔹 Кейс 2: Явний вибір формату JSON

```go
{
    name:    "sample.mp4 format-json",
    options: []string{"-format", "json"},
    wants:   sampleMP4JSONOutput,
},
```

**🎯 Призначення**: Перевірити, що явний прапорець `-format json` дає такий самий результат, як вивід за замовчуванням.

**🔑 Перевірка**: `wants` співпадає з `sampleMP4JSONOutput` → ✅ форматування консистентне.

---

### 🔹 Кейс 3: Вивід у YAML для кращої читабельності

```go
{
    name:    "sample.mp4 format-yaml",
    options: []string{"-format", "yaml"},
    wants:   sampleMP4YamlOutput,
},
```

**📊 Очікуваний YAML-вивід (фрагмент):**
```yaml
major_brand: isom
minor_version: 512
compatible_brands:
- isom
- iso2
- avc1
- mp41
fast_start: false
timescale: 1000
duration: 1024
duration_seconds: 1.024
tracks:
- track_id: 1
  timescale: 10240
  duration: 10240
  duration_seconds: 1
  codec: avc1.64000C
  encrypted: false
  width: 320
  height: 180
  sample_num: 10
  chunk_num: 9
  idr_frame_num: 1
  bitrate: 40336
  max_bitrate: 40336
- track_id: 2
  timescale: 44100
  duration: 45124
  duration_seconds: 1.02322
  codec: mp4a.40.2
  encrypted: false
  sample_num: 44
  chunk_num: 9
  bitrate: 10570
  max_bitrate: 10632
```

**🔑 Відмінності від JSON:**
| Аспект | JSON | YAML |
|--------|------|------|
| Ключі | `"MajorBrand"` (CamelCase) | `major_brand` (snake_case) |
| Структура | `{}` для об'єктів, `[]` для масивів | Відступи та `-` для списків |
| Читабельність | Машино-орієнтований | Людино-орієнтований |

**🎯 Призначення**: Забезпечити гнучкість виводу для різних сценаріїв:
- ✅ **JSON**: для інтеграції з API, jq, веб-інтерфейсами
- ✅ **YAML**: для конфігурацій, логів, документації

---

## 🔍 Технічні деталі тестування

### 🔹 Перехоплення stdout через pipe

```go
stdout := os.Stdout
r, w, err := os.Pipe()
require.NoError(t, err)
defer func() { os.Stdout = stdout }()  // 🔹 Відновлення після тесту
os.Stdout = w

go func() {
    require.Zero(t, Main(append(tc.options, tc.file)))  // 🔹 Запуск інструменту
    w.Close()  // 🔹 Закриття запису для сигналу завершення читання
}()

b, err := io.ReadAll(r)  // 🔹 Читання всього виводу
require.NoError(t, err)
assert.Equal(t, tc.wants, string(b))  // 🔹 Порівняння з golden file
```

**🎯 Призначення**: Тестувати CLI-інструмент як "чорний ящик" — без модифікації коду `probe`, тільки через стандартний вивід.

**⚠️ Важливо**: Використання горутини для `Main()` запобігає deadlock, бо `io.ReadAll()` блокується, поки `w.Close()` не сигналізує про кінець даних.

---

### 🔹 Golden files: `sampleMP4JSONOutput` / `sampleMP4YamlOutput`

```go
var sampleMP4JSONOutput = "" +
    `{` + "\n" +
    `  "MajorBrand": "isom",` + "\n" +
    // ... ще 50+ рядків ...

var sampleMP4YamlOutput = "" +
    `major_brand: isom` + "\n" +
    `minor_version: 512` + "\n" +
    // ... ще 40+ рядків ...
```

**🎯 Призначення**: Зберігати **очікуваний вивід** як константи для порівняння — підхід "golden file testing".

**🔑 Переваги:**
- ✅ Детермінованість: однаковий вивід для однакових вхідних даних
- ✅ Легке оновлення: змінили логіку → оновили константи
- ✅ Повне покриття: перевіряється кожен символ виводу

**⚠️ Недоліки:**
- ❌ Крихкість: будь-яка зміна форматування ламає тест
- ❌ Великі константи: важко читати/підтримувати

**🎯 Рекомендація**: Для складних випадків використовувати окремі файли `.golden` замість констант у коді.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Валідація вхідного сегмента перед обробкою

```go
func validateSegment(filePath string) error {
    // 🔹 Запускаємо probe для отримання метаданих
    cmd := exec.Command("mp4tool", "beta", "probe", "-format=json", filePath)
    output, err := cmd.Output()
    if err != nil {
        return fmt.Errorf("probe failed: %w", err)
    }
    
    // 🔹 Парсинг JSON-відповіді
    var report struct {
        Tracks []*struct {
            Codec     string `json:"codec"`
            Width     uint16 `json:"width"`
            Height    uint16 `json:"height"`
            Bitrate   uint64 `json:"bitrate"`
            Encrypted bool   `json:"encrypted"`
        } `json:"tracks"`
        FastStart bool `json:"fast_start"`
    }
    
    if err := json.Unmarshal(output, &report); err != nil {
        return fmt.Errorf("json parse failed: %w", err)
    }
    
    // 🔹 Перевірка обов'язкових умов
    var videoTrack *struct {
        Codec     string
        Width     uint16
        Height    uint16
        Bitrate   uint64
        Encrypted bool
    }
    
    for _, tr := range report.Tracks {
        if tr.Width > 0 && tr.Height > 0 {  // 🔹 Відео-доріжка має роздільність
            videoTrack = tr
            break
        }
    }
    
    if videoTrack == nil {
        return fmt.Errorf("no video track found")
    }
    
    // 🔹 Валідація параметрів
    if !strings.HasPrefix(videoTrack.Codec, "avc1") {
        return fmt.Errorf("unsupported codec: %s", videoTrack.Codec)
    }
    if videoTrack.Width < 640 || videoTrack.Height < 360 {
        return fmt.Errorf("resolution too low: %dx%d", videoTrack.Width, videoTrack.Height)
    }
    if videoTrack.Bitrate > 5_000_000 {  // 5 Mbps limit
        log.Printf("⚠️  High bitrate: %d bps", videoTrack.Bitrate)
    }
    if videoTrack.Encrypted {
        log.Printf("🔐 Encrypted segment — DRM required")
    }
    if !report.FastStart {
        log.Printf("⚠️  File not optimized for web (FastStart=false)")
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Генерація метаданих для HLS-плейлиста

```go
func generateHLSStreamInfo(filePath string) (string, error) {
    // 🔹 Отримання метаданих через probe
    cmd := exec.Command("mp4tool", "beta", "probe", "-format=json", filePath)
    output, err := cmd.Output()
    if err != nil {
        return "", err
    }
    
    var report struct {
        Tracks []*struct {
            Codec   string `json:"codec"`
            Bitrate uint64 `json:"bitrate"`
            Width   uint16 `json:"width"`
            Height  uint16 `json:"height"`
        } `json:"tracks"`
    }
    
    if err := json.Unmarshal(output, &report); err != nil {
        return "", err
    }
    
    // 🔹 Пошук відео-доріжки
    for _, tr := range report.Tracks {
        if tr.Width > 0 {
            // 🔹 Форматування #EXT-X-STREAM-INF
            return fmt.Sprintf(
                "#EXT-X-STREAM-INF:BANDWIDTH=%d,CODECS=\"%s\",RESOLUTION=%dx%d",
                tr.Bitrate, tr.Codec, tr.Width, tr.Height,
            ), nil
        }
    }
    
    return "", fmt.Errorf("no video track found")
}

// 🔹 Використання:
streamInfo, err := generateHLSStreamInfo("segment.m4s")
if err != nil {
    log.Printf("❌ Failed to generate stream info: %v", err)
} else {
    playlist.WriteString(streamInfo + "\nsegment.m4s\n")
}
```

---

### 🔹 Приклад 3: Моніторинг якості записів у реальному часі

```go
// 🔹 Структура для метрик якості
type QualityMetrics struct {
    AvgBitrate    uint64
    PeakBitrate   uint64
    IDRFrequency  float32  // 🔹 Секунд між ключовими кадрами
    Resolution    string
    Codec         string
}

func monitorRecordingQuality(filePath string) (*QualityMetrics, error) {
    // 🔹 Запускаємо probe
    cmd := exec.Command("mp4tool", "beta", "probe", "-format=json", filePath)
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }
    
    var report struct {
        DurationSeconds float32 `json:"duration_seconds"`
        Tracks []*struct {
            Codec       string `json:"codec"`
            Width       uint16 `json:"width"`
            Height      uint16 `json:"height"`
            Bitrate     uint64 `json:"bitrate"`
            MaxBitrate  uint64 `json:"max_bitrate"`
            IDRFrameNum int    `json:"idr_frame_num"`
        } `json:"tracks"`
    }
    
    if err := json.Unmarshal(output, &report); err != nil {
        return nil, err
    }
    
    // 🔹 Пошук відео-доріжки
    for _, tr := range report.Tracks {
        if tr.Width > 0 {
            idrFreq := float32(0)
            if tr.IDRFrameNum > 0 {
                idrFreq = report.DurationSeconds / float32(tr.IDRFrameNum)
            }
            
            return &QualityMetrics{
                AvgBitrate:   tr.Bitrate,
                PeakBitrate:  tr.MaxBitrate,
                IDRFrequency: idrFreq,
                Resolution:   fmt.Sprintf("%dx%d", tr.Width, tr.Height),
                Codec:        tr.Codec,
            }, nil
        }
    }
    
    return nil, fmt.Errorf("no video track found")
}

// 🔹 Використання у конвеєрі:
go func() {
    for recording := range recordingQueue {
        metrics, err := monitorRecordingQuality(recording.Path)
        if err != nil {
            log.Printf("❌ Failed to monitor %s: %v", recording.Path, err)
            continue
        }
        
        // 🔹 Логування метрик
        log.Printf("📊 %s: %s @ %s, %d-%d kbps, IDR every %.1fs",
            recording.Path,
            metrics.Codec,
            metrics.Resolution,
            metrics.AvgBitrate/1000,
            metrics.PeakBitrate/1000,
            metrics.IDRFrequency,
        )
        
        // 🔹 Попередження про аномалії
        if metrics.IDRFrequency > 4.0 {
            log.Printf("⚠️  Low keyframe frequency: %.1fs > 4s", metrics.IDRFrequency)
        }
        if metrics.PeakBitrate > 2*metrics.AvgBitrate {
            log.Printf("⚠️  High bitrate variance: peak=%d, avg=%d", 
                metrics.PeakBitrate, metrics.AvgBitrate)
        }
    }
}()
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

> **Цей тест — ваш "золотий стандарт" для надійного інструменту `probe`**.  
> Він гарантує:
> • ✅ Коректну генерацію звітів у JSON та YAML з консистентним форматуванням
> • ✅ Точний розрахунок тривалості, бітрейту та інших метрик
> • ✅ Стандартизовані рядки кодеків для сумісності з HLS/DASH
> • ✅ Стабільний, детермінований вивід для порівняння через golden files
> • ✅ Безпечне перехоплення stdout через pipe без deadlock

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Миттєва валідація вхідних записів перед додаванням у стрімінг
- 🔧 Автоматична генерація параметрів для HLS-плейлистів
- 📊 Моніторинг якості: бітрейт, роздільність, частота ключових кадрів
- 🔄 Легка інтеграція з Python/Node.js скриптами через JSON-вивід
- 🛡️ Безпечна обробка DRM-контенту через прапорець `Encrypted`

Потребуєте допомоги з інтеграцією `probe` у ваш конвеєр валідації або з налаштуванням кастомного формату звіту? Напишіть — покажу готовий код для вашого сценарію! 🚀🔍