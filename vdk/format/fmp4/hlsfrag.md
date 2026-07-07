# 📦 Глибокий розбір: `fmp4.MovieFragmenter` — Фрагментація медіа-потоку у fMP4 форматі

Цей файл — **реалізація фрагментатора для створення сегментів fMP4** з підтримкою кількох треків (відео + аудіо). Він інтегрує індивідуальні `TrackFragmenter` у єдиний інтерфейс `fragment.Fragmenter` для adaptive bitrate streaming.

---

## 🗺️ Архітектурна схема MovieFragmenter

```
┌────────────────────────────────────────┐
│ 📦 fmp4.MovieFragmenter               │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • MovieFragmenter — головний контролер│
│  • TrackFragmenter × N — окремі треки │
│  • fmp4io.Movie — генерація метаданих │
│  • fragment.Fragment — вихідний формат│
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet → TrackFragmenter          │
│  → MovieFragmenter.Fragment()         │
│  → fragment.Fragment → HTTP/WebSocket│
│                                         │
│  📡 Використання:                       │
│  • HLS fMP4 сегменти                  │
│  • DASH сегменти                      │
│  • Low-latency streaming              │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. MovieFragmenter — головна структура

### 🔧 Структура та призначення:

```go
type MovieFragmenter struct {
    tracks []*TrackFragmenter  // ⭐ масив фрагментаторів для кожного треку
    fhdr   []byte             // ⭐ init segment (ftyp + moov) у байтах
    vidx   int                // ⭐ індекс відео треку для розрахунку duration
    seqNum uint32             // ⭐ послідовний номер фрагменту для moof header
    shdrw  bool               // ⭐ прапорець чи вже записано segment header (styp)
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `tracks` | `[]*TrackFragmenter` | **Критично**: масив фрагментаторів для кожного треку (відео, аудіо, субтитри) | `tracks[0]` = відео, `tracks[1]` = аудіо |
| `fhdr` | `[]byte` | **Критично**: серіалізований init segment (ftyp + moov) для клієнта | `[]byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, ...}` |
| `vidx` | `int` | **Критично**: індекс відео треку для розрахунку загальної тривалості фрагменту | `0` якщо відео — перший трек |
| `seqNum` | `uint32` | **Критично**: послідовний номер для moof header (критично для порядку відтворення) | `1, 2, 3, ...` |
| `shdrw` | `bool` | **Критично**: чи вже записано segment header (styp) для поточного сегменту | `false` → наступний Fragment() включить styp |

### 🔍 Чому `vidx` критичний для duration?

```
У fMP4:
• Відео та аудіо можуть мати різну кількість семплів у фрагменті
• Тривалість фрагменту визначається за відео треком (основний потік)
• Аудіо може мати більше/менше семплів через різну частоту дискретизації

Приклад:
• Відео: 30 fps → 4 секунди = 120 кадрів
• Аудіо: 48 kHz → 4 секунди = 192000 семплів
• Тривалість фрагменту = 4 секунди (за відео), не за аудіо

Це забезпечує синхронізацію: клієнт використовує відео таймінги як основні.
```

### ✅ Ваш use-case**: ініціалізація фрагментатора з кодеками

```go
// NewMovieFromCodecs — створення фрагментатора з списком кодеків
func NewMovieFromCodecs(codecs []av.CodecData) (*fmp4.MovieFragmenter, error) {
    // Валідація вхідних даних
    if len(codecs) == 0 {
        return nil, fmt.Errorf("at least one codec required")
    }
    
    // Перевірка наявності відео треку
    hasVideo := false
    for _, codec := range codecs {
        if codec.Type().IsVideo() {
            hasVideo = true
            break
        }
    }
    if !hasVideo {
        return nil, fmt.Errorf("video track required for fMP4 fragmentation")
    }
    
    // Створення фрагментатора
    fragmenter, err := fmp4.NewMovie(codecs)
    if err != nil {
        return nil, fmt.Errorf("create fragmenter: %w", err)
    }
    
    return fragmenter, nil
}

// Використання:
codecs := []av.CodecData{
    h264CodecData,  // відео
    aacCodecData,   // аудіо
}
fragmenter, err := NewMovieFromCodecs(codecs)
if err != nil { /* handle error */ }

// Отримання init segment для клієнта
filename, contentType, initBytes := fragmenter.MovieHeader()
// filename = "init.mp4", contentType = "video/mp4"
```

---

## 🔑 2. NewMovie() — ініціалізація фрагментатора

### 🔧 Основна логіка:

```go
func NewMovie(streams []av.CodecData) (*MovieFragmenter, error) {
    f := &MovieFragmenter{
        tracks: make([]*TrackFragmenter, len(streams)),
        vidx:   -1,  // спочатку не знайдено відео
    }
    
    // 1. Створення TrackFragmenter для кожного кодека
    atoms := make([]*fmp4io.Track, len(streams))
    var err error
    for i, cd := range streams {
        f.tracks[i], err = NewTrack(cd)  // створення треку з кодеком
        if err != nil {
            return nil, fmt.Errorf("track %d: %w", i, err)
        }
        atoms[i] = f.tracks[i].atom  // збереження fmp4io.Track атому
        if cd.Type().IsVideo() {
            f.vidx = i  // запам'ятовуємо індекс відео треку
        }
    }
    
    // 2. Перевірка наявності відео треку
    if f.vidx < 0 {
        return nil, errors.New("no video track found")
    }
    
    // 3. Генерація init segment (ftyp + moov)
    f.fhdr, err = MovieHeader(atoms)  // серіалізація моов атому
    if err != nil {
        return nil, err
    }
    
    return f, err
}
```

### ⚠️ Критична проблема: жорстка вимога відео треку

```
У поточній реалізації:
    if f.vidx < 0 {
        return nil, errors.New("no video track found")
    }

Проблема:
• Неможливість створення аудіо-тільки фрагментатора (напр. для подкастів)
• Обмеження для use-cases з аудіо-стримінгом без відео

✅ Виправлення: опціональна підтримка аудіо-тільки режиму
    // Дозволити аудіо-тільки якщо явно вказано
    if f.vidx < 0 {
        // Перевірка чи є хоча б один аудіо трек
        hasAudio := false
        for _, cd := range streams {
            if cd.Type().IsAudio() {
                hasAudio = true
                break
            }
        }
        if !hasAudio {
            return nil, errors.New("at least one video or audio track required")
        }
        // Для аудіо-тільки: використовуємо перший трек для duration
        f.vidx = 0
    }
```

### ✅ Ваш use-case**: створення фрагментатора з валідацією

```go
// SafeNewMovie — безпечне створення з розширеною валідацією
func SafeNewMovie(streams []av.CodecData) (*fmp4.MovieFragmenter, error) {
    if len(streams) == 0 {
        return nil, fmt.Errorf("empty streams list")
    }
    
    // Валідація кожного кодека
    for i, cd := range streams {
        if cd == nil {
            return nil, fmt.Errorf("codec %d is nil", i)
        }
        switch cd.Type() {
        case av.H264, av.HEVC, av.VP9, av.AV1:
            // підтримувані відео кодеки
        case av.AAC, av.Opus, av.Vorbis:
            // підтримувані аудіо кодеки
        default:
            return nil, fmt.Errorf("unsupported codec %d: %v", i, cd.Type())
        }
    }
    
    return fmp4.NewMovie(streams)
}
```

---

## 🔑 3. Fragment() — генерація готового фрагменту

### 🔧 Основна логіка:

```go
func (f *MovieFragmenter) Fragment() (fragment.Fragment, error) {
    // 1. Отримання тривалості з відео треку
    dur := f.tracks[f.vidx].Duration()
    
    // 2. Збір фрагментів з усіх треків
    var tracks []fragmentWithData
    for _, track := range f.tracks {
        tf := track.makeFragment()  // отримання даних треку
        if tf.trackFrag != nil {    // тільки якщо є дані
            tracks = append(tracks, tf)
        }
    }
    
    // 3. Перевірка чи є дані для фрагменту
    if len(tracks) == 0 {
        return fragment.Fragment{}, nil  // порожній фрагмент
    }
    
    // 4. Оновлення стану
    f.seqNum++              // інкремент послідовного номера
    initial := !f.shdrw     // чи це перший фрагмент сегменту
    f.shdrw = true          // позначити що header вже записано
    
    // 5. Маршалінг у фінальний формат
    frag := marshalFragment(tracks, f.seqNum, initial)
    frag.Duration = dur     // встановлення тривалості
    return frag, nil
}
```

### 🔍 Що таке `fragmentWithData`?

```
(Не показано у цьому файлі, але ймовірна структура):

type fragmentWithData struct {
    trackFrag *fmp4io.TrackFrag  // fMP4 Track Fragment атом
    data      []byte             // сирі медіа-дані (mdat content)
    trackIdx  int                // індекс треку
}

Це проміжна структура для передачі даних треку у marshalFragment().
```

### 🔍 Логіка `shdrw` прапорця:

```
shdrw = "segment header written"

Призначення:
• Визначає чи потрібно включати styp атом у початку фрагменту
• styp (Segment Type) потрібен тільки для першого фрагменту сегменту

Потік:
1. NewSegment() → f.shdrw = false
2. Fragment() → initial = !f.shdrw = true → включити styp
3. Fragment() → f.shdrw = true → наступні фрагменти без styp
4. NewSegment() → повторення циклу

Це дозволяє:
• Клієнту розпізнати початок нового сегменту
• Економити байти не дублюючи styp у кожному фрагменті
```

### ✅ Ваш use-case**: отримання фрагментів у streaming циклі

```go
// StreamFragments — основний цикл відправки фрагментів клієнту
func StreamFragments(ctx context.Context, fragmenter *fmp4.MovieFragmenter, 
                    demuxer av.Demuxer, sender FragmentSender) error {
    // 1. Відправка init segment
    filename, contentType, initBytes := fragmenter.MovieHeader()
    if err := sender.SendInit(filename, contentType, initBytes); err != nil {
        return fmt.Errorf("send init: %w", err)
    }
    
    // 2. Основний цикл
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-ticker.C:
            // Читання пакетів з демуксера
            for {
                pkt, err := demuxer.ReadPacket()
                if err == io.EOF {
                    return nil
                }
                if err != nil {
                    return fmt.Errorf("read packet: %w", err)
                }
                
                if err := fragmenter.WritePacket(pkt); err != nil {
                    return fmt.Errorf("write packet: %w", err)
                }
                
                // Перериваємо якщо накопичено достатньо для фрагменту
                if fragmenter.Duration() >= 4*time.Second {
                    break
                }
            }
            
            // Генерація та відправка фрагменту
            frag, err := fragmenter.Fragment()
            if err != nil {
                return fmt.Errorf("generate fragment: %w", err)
            }
            if frag.Length == 0 {
                continue  // немає даних
            }
            
            if err := sender.SendFragment(frag); err != nil {
                return fmt.Errorf("send fragment: %w", err)
            }
            
            // Логування метрик
            log.Printf("Sent fragment %d: %d bytes, %v duration", 
                fragmenter.seqNum-1, frag.Length, frag.Duration)
        }
    }
}

type FragmentSender interface {
    SendInit(filename, contentType string, data []byte) error
    SendFragment(frag fragment.Fragment) error
}
```

---

## 🔑 4. WritePacket() — буферизація пакетів

### 🔧 Реалізація:

```go
func (f *MovieFragmenter) WritePacket(pkt av.Packet) error {
    return f.tracks[pkt.Idx].WritePacket(pkt)
}
```

### 🔍 Призначення:

```
Делегування запису пакету відповідному треку:

• pkt.Idx — індекс треку у масиві (0 = перший трек, тощо)
• Кожен TrackFragmenter буферизує пакети до завершення фрагменту
• Після накопичення достатньої кількості даних → Fragment() повертає готовий фрагмент

Важливо:
• Пакети мають бути відсортовані за часом (DTS) для коректної фрагментації
• Різні треки можуть мати різну кількість пакетів у фрагменті
• Синхронізація забезпечується через спільний seqNum та таймінги
```

### ✅ Ваш use-case**: фільтрація пакетів перед записом

```go
// FilteringMovieFragmenter — обгортка для фільтрації пакетів
type FilteringMovieFragmenter struct {
    base *fmp4.MovieFragmenter
    filter func(av.Packet) bool  // функція фільтрації
}

func NewFilteringMovieFragmenter(base *fmp4.MovieFragmenter, filter func(av.Packet) bool) *FilteringMovieFragmenter {
    return &FilteringMovieFragmenter{
        base: base,
        filter: filter,
    }
}

func (f *FilteringMovieFragmenter) WritePacket(pkt av.Packet) error {
    if f.filter != nil && !f.filter(pkt) {
        // Пропускаємо пакет (напр. для зменшення бітрейту)
        return nil
    }
    return f.base.WritePacket(pkt)
}

// Делегування інших методів
func (f *FilteringMovieFragmenter) Fragment() (fragment.Fragment, error) {
    return f.base.Fragment()
}
func (f *FilteringMovieFragmenter) Duration() time.Duration {
    return f.base.Duration()
}
// ... інші методи ...

// Використання для adaptive bitrate:
filter := func(pkt av.Packet) bool {
    // Пропускати неключові відео кадри якщо бітрейт занадто високий
    if pkt.Idx == 0 && !pkt.IsKeyFrame {  // припускаємо відео = трек 0
        return rand.Float32() > 0.3  // пропускати 30% неключових кадрів
    }
    return true
}
filtered := NewFilteringMovieFragmenter(fragmenter, filter)
```

---

## 🔑 5. MovieHeader() — генерація init segment

### 🔧 Реалізація:

```go
func (f *MovieFragmenter) MovieHeader() (filename, contentType string, blob []byte) {
    return "init.mp4", "video/mp4", f.fhdr
}
```

### 🔍 Призначення:

```
Init segment (init.mp4) містить:
• ftyp атом — ідентифікація формату та сумісних брендів
• moov атом — метадані треків (кодеки, timeScale, таблиці семплів)

Клієнт використовує init segment для:
• Ініціалізації декодерів (отримання SPS/PPS для H.264, AudioSpecificConfig для AAC)
• Налаштування таймінгів (timeScale для конвертації ticks → time)
• Підготовки буферів для відтворення

Важливо:
• Init segment відправляється тільки один раз на початку сесії
• Усі медіа-фрагменти посилаються на метадані з init segment
• Зміна кодеків вимагає нового init segment (новий сеанс)
```

### ✅ Ваш use-case**: валідація init segment перед відправкою

```go
// ValidateInitSegment — перевірка коректності init segment
func ValidateInitSegment(data []byte) error {
    if len(data) < 16 {
        return fmt.Errorf("init segment too short: %d bytes", len(data))
    }
    
    // Перевірка ftyp атому
    if string(data[4:8]) != "ftyp" {
        return fmt.Errorf("missing ftyp atom")
    }
    
    // Перевірка основних брендів
    majorBrand := string(data[8:12])
    if majorBrand != "iso6" && majorBrand != "iso5" && majorBrand != "mp42" {
        log.Printf("warning: unusual major brand: %s", majorBrand)
    }
    
    // Перевірка наявності moov атому (спрощено)
    if !bytes.Contains(data, []byte("moov")) {
        return fmt.Errorf("missing moov atom")
    }
    
    return nil
}

// Використання:
filename, contentType, initBytes := fragmenter.MovieHeader()
if err := ValidateInitSegment(initBytes); err != nil {
    log.Printf("warning: invalid init segment: %v", err)
    // Можна спробувати відновитися або повернути помилку
}
```

---

## 🔑 6. NewSegment() — початок нового сегменту

### 🔧 Реалізація:

```go
func (f *MovieFragmenter) NewSegment() {
    f.shdrw = false
}
```

### 🔍 Призначення:

```
NewSegment() сигналізує про початок нового логічного сегменту:

• Скидає прапорець shdrw → наступний Fragment() включить styp атом
• Дозволяє клієнту розпізнати початок нового сегменту у потоці
• Критично для low-latency streaming де сегменти можуть бути короткими

Типовий потік:
1. NewSegment() → shdrw = false
2. Запис пакетів у фрагментатор
3. Fragment() → повертає фрагмент з styp (бо initial = true)
4. Повторення для наступних фрагментів без styp
5. NewSegment() → початок нового сегменту

Це дозволяє:
• Динамічне перемикання бітрейтів (новий сегмент = нова якість)
• Швидке відновлення після втрати пакетів (початок нового сегменту)
• Ефективне буферування на клієнті (сегменти = одиниці буферування)
```

### ✅ Ваш use-case**: автоматичне завершення сегменту за часом

```go
// TimedSegmentController — автоматичне завершення сегментів за таймером
type TimedSegmentController struct {
    fragmenter *fmp4.MovieFragmenter
    segmentDuration time.Duration
    lastSegmentStart time.Time
}

func NewTimedSegmentController(fragmenter *fmp4.MovieFragmenter, duration time.Duration) *TimedSegmentController {
    return &TimedSegmentController{
        fragmenter: fragmenter,
        segmentDuration: duration,
        lastSegmentStart: time.Now(),
    }
}

func (c *TimedSegmentController) ShouldStartNewSegment() bool {
    if time.Since(c.lastSegmentStart) >= c.segmentDuration {
        c.fragmenter.NewSegment()
        c.lastSegmentStart = time.Now()
        return true
    }
    return false
}

// Використання у streaming циклі:
controller := NewTimedSegmentController(fragmenter, 4*time.Second)

for {
    // ... запис пакетів ...
    
    if controller.ShouldStartNewSegment() {
        log.Printf("Started new segment at %v", time.Now())
    }
    
    // ... генерація та відправка фрагментів ...
}
```

---

## 🔑 7. TimeScale() — шкала часу для конвертації

### 🔧 Реалізація:

```go
func (f *MovieFragmenter) TimeScale() uint32 {
    return 90000
}
```

### 🔍 Призначення:

```
TimeScale визначає одиницю виміру часу для таймінгів у fMP4:

• 90000 ticks/second — стандарт для відео (сумісність з MPEG-TS)
• Конвертація: duration_seconds = ticks / 90000
• Приклад: 360000 ticks = 360000/90000 = 4 секунди

Чому 90000?
• Кратне 30000 (30 fps), 24000 (24 fps), 25000 (25 fps)
• Забезпечує цілі значення ticks для більшості відео частот кадрів
• Сумісність з існуючими MPEG системами

Для аудіо:
• Аудіо треки можуть мати інший timeScale (напр. 48000 для AAC)
• Але MovieFragmenter повертає 90000 як основну шкалу
• Конвертація між шкалами виконується внутрішньо у TrackFragmenter
```

### ✅ Ваш use-case**: конвертація таймінгів для клієнта

```go
// ConvertTicksToDuration — допоміжна функція для клієнтів
func ConvertTicksToDuration(ticks uint64, timeScale uint32) time.Duration {
    return time.Duration(ticks) * time.Second / time.Duration(timeScale)
}

// Використання при обробці фрагментів:
frag, _ := fragmenter.Fragment()
duration := ConvertTicksToDuration(frag.Duration, fragmenter.TimeScale())
log.Printf("Fragment duration: %v (%d ticks @ %d Hz)", 
    duration, frag.Duration, fragmenter.TimeScale())

// Для відправки клієнту у метаданих:
metadata := FragmentMetadata{
    SequenceNumber: seqNum,
    DurationMs:     duration.Milliseconds(),
    TimeScale:      fragmenter.TimeScale(),
    // ... інші поля ...
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Повний цикл HLS fMP4 streaming

```go
// HLSFMP4Streamer — реалізація HLS streaming з fMP4 сегментами
type HLSFMP4Streamer struct {
    fragmenter *fmp4.MovieFragmenter
    demuxer    av.Demuxer
    playlist   *HLSPlaylist
    storage    SegmentStorage
}

func (s *HLSFMP4Streamer) Start(ctx context.Context) error {
    // 1. Генерація та збереження init segment
    filename, contentType, initBytes := s.fragmenter.MovieHeader()
    initPath := fmt.Sprintf("streams/%s/init.mp4", s.playlist.StreamID)
    if err := s.storage.Save(initPath, contentType, initBytes); err != nil {
        return fmt.Errorf("save init: %w", err)
    }
    
    // 2. Додавання init segment у playlist
    s.playlist.AddInitSegment(initPath)
    
    // 3. Основний цикл генерації сегментів
    segmentIndex := 0
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-ticker.C:
            // Накопичення пакетів до досягнення цільової тривалості
            targetDuration := 4 * time.Second
            for s.fragmenter.Duration() < targetDuration {
                pkt, err := s.demuxer.ReadPacket()
                if err == io.EOF {
                    goto finalize
                }
                if err != nil {
                    return fmt.Errorf("read packet: %w", err)
                }
                if err := s.fragmenter.WritePacket(pkt); err != nil {
                    return fmt.Errorf("write packet: %w", err)
                }
            }
            
            // Генерація фрагменту
            frag, err := s.fragmenter.Fragment()
            if err != nil {
                return fmt.Errorf("generate fragment: %w", err)
            }
            if frag.Length == 0 {
                continue
            }
            
            // Збереження сегменту
            segmentPath := fmt.Sprintf("streams/%s/seg_%05d.mp4", 
                s.playlist.StreamID, segmentIndex)
            if err := s.storage.Save(segmentPath, "video/mp4", frag.Bytes[:frag.Length]); err != nil {
                return fmt.Errorf("save segment: %w", err)
            }
            
            // Оновлення playlist
            s.playlist.AddSegment(segmentPath, frag.Duration)
            segmentIndex++
            
            // Ротація playlist якщо потрібно
            if segmentIndex%s.playlist.MaxSegments == 0 {
                if err := s.playlist.Save(); err != nil {
                    return fmt.Errorf("save playlist: %w", err)
                }
            }
        }
    }
    
finalize:
    // Фіналізація останнього сегменту
    if s.fragmenter.Duration() > 0 {
        frag, _ := s.fragmenter.Fragment()
        if frag.Length > 0 {
            // ... збереження останнього сегменту ...
        }
    }
    
    // Фінальне збереження playlist
    s.playlist.MarkComplete()
    return s.playlist.Save()
}

type HLSPlaylist struct {
    StreamID    string
    Version     int
    TargetDuration time.Duration
    MaxSegments int
    InitSegment string
    Segments    []PlaylistSegment
    Complete    bool
}

type PlaylistSegment struct {
    Path     string
    Duration time.Duration
}
```

### 🔧 Приклад: Low-latency streaming з WebSocket

```go
// WebSocketFMP4Streamer — streaming через WebSocket з мінімальною затримкою
type WebSocketFMP4Streamer struct {
    fragmenter *fmp4.MovieFragmenter
    conn       *websocket.Conn
    mu         sync.Mutex
}

func (s *WebSocketFMP4Streamer) Stream(ctx context.Context, demuxer av.Demuxer) error {
    // 1. Відправка init segment
    _, _, initBytes := s.fragmenter.MovieHeader()
    if err := s.sendBinary(initBytes); err != nil {
        return err
    }
    
    // 2. Основний цикл з низькою затримкою
    flushTicker := time.NewTicker(50 * time.Millisecond)  // частіша перевірка
    defer flushTicker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-flushTicker.C:
            // Примусова генерація фрагменту навіть якщо не набрано повний
            frag, err := s.fragmenter.Fragment()
            if err != nil {
                return fmt.Errorf("generate fragment: %w", err)
            }
            if frag.Length == 0 {
                continue
            }
            
            // Відправка через WebSocket
            if err := s.sendFragment(frag); err != nil {
                return fmt.Errorf("send fragment: %w", err)
            }
            
        default:
            // Неблокуюче читання пакетів
            pkt, err := demuxer.ReadPacket()
            if err == io.EOF {
                return nil
            }
            if err != nil && err != io.ErrNoData {  // припустимо таку помилку для non-blocking
                return fmt.Errorf("read packet: %w", err)
            }
            if err == nil {
                if err := s.fragmenter.WritePacket(pkt); err != nil {
                    return fmt.Errorf("write packet: %w", err)
                }
            }
        }
    }
}

func (s *WebSocketFMP4Streamer) sendFragment(frag fragment.Fragment) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // Формат повідомлення: [4-byte length][fragment data]
    header := make([]byte, 4)
    binary.BigEndian.PutUint32(header, uint32(frag.Length))
    
    message := append(header, frag.Bytes[:frag.Length]...)
    return s.conn.WriteMessage(websocket.BinaryMessage, message)
}

func (s *WebSocketFMP4Streamer) sendBinary(data []byte) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.conn.WriteMessage(websocket.BinaryMessage, data)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"no video track found" при ініціалізації** | Помилка NewMovie() для аудіо-тільки потоків | Реалізуйте опціональну підтримку аудіо-тільки режиму або додайте dummy video track |
| **Невірний seqNum у фрагментах** | Клієнт не може відтворити фрагменти у правильному порядку | Переконайтеся що seqNum інкрементується тільки у Fragment(), не при перезапусках |
| **Відсутній styp у першому фрагменті сегменту** | Клієнт не розпізнає початок нового сегменту | Перевірте логіку shdrw прапорця та виклик NewSegment() |
| **Розсинхронізація аудіо/відео** | Аудіо відстає або випереджає відео | Переконайтеся що всі треки використовують спільний timeScale або коректно конвертують таймінги |
| **Переповнення буфера при великих пакетах** | Паніка або втрата даних | Додайте перевірку розміру пакетів перед записом або збільшіть буфери у TrackFragmenter |

---

## ⚡ Оптимізації для high-performance streaming

### 1. Reuse буферів для фрагментів:

```go
var fragmentBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір фрагменту: 1-4 MB для відео
        buf := make([]byte, 0, 4*1024*1024)
        return &buf
    },
}

func getFragmentBuffer() *[]byte {
    return fragmentBufferPool.Get().(*[]byte)
}

func putFragmentBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    fragmentBufferPool.Put(b)
}

// Використання у marshalFragment():
buf := getFragmentBuffer()
defer putFragmentBuffer(buf)
// ... запис даних у buf ...
return fragment.Fragment{
    Bytes: *buf,
    Length: buf.Len(),
    // ...
}, nil
```

### 2. Асинхронна генерація фрагментів:

```go
type AsyncMovieFragmenter struct {
    base     *fmp4.MovieFragmenter
    workChan chan av.Packet
    fragChan chan fragment.Fragment
    errChan  chan error
    done     chan struct{}
}

func NewAsyncMovieFragmenter(base *fmp4.MovieFragmenter, workerCount int) *AsyncMovieFragmenter {
    f := &AsyncMovieFragmenter{
        base:     base,
        workChan: make(chan av.Packet, 100),
        fragChan: make(chan fragment.Fragment, 10),
        errChan:  make(chan error, 1),
        done:     make(chan struct{}),
    }
    
    for i := 0; i < workerCount; i++ {
        go f.worker()
    }
    
    return f
}

func (f *AsyncMovieFragmenter) worker() {
    for {
        select {
        case pkt := <-f.workChan:
            if err := f.base.WritePacket(pkt); err != nil {
                f.errChan <- err
                return
            }
        case <-f.done:
            return
        }
    }
}

func (f *AsyncMovieFragmenter) WritePacket(pkt av.Packet) error {
    select {
    case f.workChan <- pkt:
        return nil
    case err := <-f.errChan:
        return err
    case <-f.done:
        return fmt.Errorf("fragmenter closed")
    }
}

func (f *AsyncMovieFragmenter) Fragment() (fragment.Fragment, error) {
    // Синхронний виклик базового фрагментатора
    // (асинхронність тільки для WritePacket)
    return f.base.Fragment()
}
```

### 3. Моніторинг продуктивності фрагментації:

```go
type FragmenterMetrics struct {
    PacketsWritten prometheus.CounterVec
    FragmentsGenerated prometheus.CounterVec
    FragmentLatency prometheus.HistogramVec
    FragmentSizes prometheus.HistogramVec
    SegmentDuration prometheus.HistogramVec
}

func (m *FragmenterMetrics) RecordPacket(trackIdx int, codec av.CodecType, size int) {
    m.PacketsWritten.WithLabelValues(fmt.Sprintf("track_%d", trackIdx), codec.String()).Inc()
}

func (m *FragmenterMetrics) RecordFragment(duration time.Duration, size int, seqNum uint32) {
    m.FragmentsGenerated.Inc()
    m.FragmentLatency.Observe(duration.Seconds())
    m.FragmentSizes.Observe(float64(size))
    m.SegmentDuration.Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист безпечного використання MovieFragmenter

```go
// ✅ 1. Валідація вхідних кодеків перед створенням
for i, cd := range codecs {
    if cd == nil {
        return fmt.Errorf("codec %d is nil", i)
    }
    // Перевірка підтримуваних типів...
}

// ✅ 2. Перевірка наявності відео треку (або аудіо для спеціальних випадків)
if fragmenter.vidx < 0 {
    // Обробка аудіо-тільки випадку або повернення помилки
}

// ✅ 3. Коректне використання seqNum для порядку фрагментів
// seqNum інкрементується тільки у Fragment(), не скидається при помилках

// ✅ 4. Обробка порожніх фрагментів
frag, err := fragmenter.Fragment()
if err != nil { /* handle error */ }
if frag.Length == 0 {
    continue  // немає даних для відправки
}

// ✅ 5. Використання Bytes[:Length] для доступу до даних
data := frag.Bytes[:frag.Length]  // ✅ правильно
// data := frag.Bytes             // ❌ може включати неініціалізовану пам'ять

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Generated fragment %d: %d bytes, %v duration, tracks=%d", 
    fragmenter.seqNum-1, frag.Length, frag.Duration, len(tracks))

// ✅ 7. Метрики для моніторингу
metrics.RecordFragment(frag.Duration, frag.Length, fragmenter.seqNum-1)
```

---

## 🔗 Корисні посилання

- 💻 [vdk fmp4 Package](https://pkg.go.dev/github.com/deepch/vdk/format/fmp4) — GoDoc documentation
- 📄 [ISO/IEC 23009-1:2022 (DASH)](https://www.iso.org/standard/79329.html) — стандарт для adaptive streaming
- 📄 [HLS fMP4 Specification](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 🧪 [Go sync.Pool Documentation](https://pkg.go.dev/sync#Pool) — ефективне управління пам'яттю
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Валідуйте вхідні кодеки перед створенням фрагментатора** — уникнення помилок ініціалізації.
> 2. **Коректно обробляйте порожні фрагменти** — уникнення відправки нульових даних клієнту.
> 3. **Використовуйте Bytes[:Length] для доступу до даних** — уникнення читання неініціалізованої пам'яті.
> 4. **Моніторьте seqNum для відладки порядку фрагментів** — критично для коректного відтворення.
> 5. **Розгляньте асинхронну обробку для high-throughput сценаріїв** — уникнення блокування основного циклу.

Потрібен приклад реалізації повного циклу HLS/DASH streaming з адаптивним бітрейтом, або інтеграція `fmp4.MovieFragmenter` з вашим `mse.Muxer` для WebSocket стрімінгу з низькою затримкою? Готовий допомогти! 🚀