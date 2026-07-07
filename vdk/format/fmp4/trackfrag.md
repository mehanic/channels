# 📦 Глибокий розбір: `fmp4.TrackFragmenter` — Фрагментація окремого треку у CMAF/fMP4

Цей файл — **реалізація фрагментатора для окремого медіа-треку** (відео або аудіо) у форматі CMAF (Common Media Application Format) / fMP4. Він обробляє вхідні `av.Packet`, переформатовує дані (наприклад, NALU для H.264), та генерує готові фрагменти для streaming.

---

## 🗺️ Архітектурна схема TrackFragmenter

```
┌────────────────────────────────────────┐
│ 📦 fmp4.TrackFragmenter               │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • TrackFragmenter — фрагментатор треку│
│  • WritePacket() — буферизація пакетів │
│  • makeFragment() — побудова метаданих │
│  • marshalFragment() — серіалізація   │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet → WritePacket()            │
│  → pending buffer                     │
│  → Fragment() → fragment.Fragment     │
│                                         │
│  📡 Особливості:                        │
│  • H.264 NALU → AVCC переформатування │
│  • CMAF single-track підтримка        │
│  • Low-latency сегментація            │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. TrackFragmenter — структура фрагментатора треку

### 🔧 Структура та призначення:

```go
type TrackFragmenter struct {
    codecData av.CodecData      // ⭐ метадані кодека (напр. H.264 SPS/PPS)
    trackID   uint32            // ⭐ унікальний ідентифікатор треку (1=аудіо, 2=відео)
    timeScale uint32            // ⭐ ticks per second для цього треку (90000/48000)
    atom      *fmp4io.Track     // ⭐ fMP4 Track атом для init segment
    pending   []av.Packet       // ⭐ буфер пакетів очікуючих фрагментації
    
    // для CMAF (single track) only
    seqNum uint32              // ⭐ послідовний номер фрагменту
    fhdr   []byte              // ⭐ init segment (ftyp+moov) у байтах
    shdrw  bool                // ⭐ прапорець чи записано segment header
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `codecData` | `av.CodecData` | **Критично**: метадані кодека для ініціалізації декодера | `h264parser.CodecData` з SPS/PPS |
| `trackID` | `uint32` | **Критично**: унікальний ідентифікатор треку у межах файлу | `2` для відео, `1` для аудіо |
| `timeScale` | `uint32` | **Критично**: шкала часу для конвертації таймінгів | `90000` для відео, `48000` для аудіо |
| `pending` | `[]av.Packet` | **Критично**: буфер пакетів для поточного фрагменту | `[pkt0, pkt1, pkt2, ...]` |
| `seqNum` | `uint32` | **Критично**: послідовний номер для порядку відтворення | `1, 2, 3, ...` |
| `fhdr` | `[]byte` | **Критично**: серіалізований init segment для клієнта | `[]byte{0x00, 0x00, 0x00, 0x18, ...}` |
| `shdrw` | `bool` | **Критично**: чи вже записано segment header (styp) | `false` → наступний фрагмент включить styp |

### 🔍 Чому `trackID = 2` для відео?

```
У прикладі коду:
    var trackID uint32 = 1
    if codecData.Type().IsVideo() {
        trackID = 2
    }

Це конвенція для single-track фрагментації:
• Аудіо треки: trackID = 1
• Відео треки: trackID = 2

У multi-track MovieFragmenter:
• TrackID призначаються послідовно: 1, 2, 3...
• Але для CMAF single-track: фіксовані значення для сумісності

Важливо:
• TrackID має бути унікальним у межах одного файлу
• Клієнт використовує TrackID для ідентифікації потоку
• Неправильний TrackID може призвести до помилок синхронізації
```

### ✅ Ваш use-case**: ініціалізація фрагментатора з кодеком

```go
// NewTrackWithValidation — безпечне створення з розширеною валідацією
func NewTrackWithValidation(codecData av.CodecData) (*fmp4.TrackFragmenter, error) {
    if codecData == nil {
        return nil, fmt.Errorf("codecData cannot be nil")
    }
    
    // Валідація підтримуваних кодеків
    switch codecData.Type() {
    case av.H264, av.AAC, av.Opus:
        // підтримувані
    default:
        return nil, fmt.Errorf("unsupported codec type: %v", codecData.Type())
    }
    
    // Для H.264: перевірка наявності SPS/PPS
    if codecData.Type() == av.H264 {
        h264, ok := codecData.(h264parser.CodecData)
        if !ok {
            return nil, fmt.Errorf("failed to cast to H.264 CodecData")
        }
        if len(h264.SPS()) == 0 || len(h264.PPS()) == 0 {
            return nil, fmt.Errorf("H.264 codec missing SPS/PPS")
        }
    }
    
    return fmp4.NewTrack(codecData)
}

// Використання:
codec := getH264CodecData()  // отримання кодека з демуксера
fragmenter, err := NewTrackWithValidation(codec)
if err != nil {
    log.Printf("error creating fragmenter: %v", err)
    return
}

// Отримання init segment
filename, contentType, initBytes := fragmenter.MovieHeader()
// filename = "init.mp4", contentType = "video/mp4"
```

---

## 🔑 2. WritePacket() — буферизація та переформатування пакетів

### 🔧 Основна логіка:

```go
func (f *TrackFragmenter) WritePacket(pkt av.Packet) error {
    switch f.codecData.(type) {
    case h264parser.CodecData:
        // 1. Розбиття даних на NALU
        nalus, typ := h264parser.SplitNALUs(pkt.Data)
        if typ == h264parser.NALU_AVCC {
            // Вже у AVCC форматі → нічого не робити
            break
        }
        
        // 2. Переформатування у AVCC (Annex B → AVCC)
        b := make([]byte, 0, len(pkt.Data)+3*len(nalus))
        for _, nalu := range nalus {
            j := len(nalu)
            // Запис 4-байтового розміру у big-endian
            b = append(b, byte(j>>24), byte(j>>16), byte(j>>8), byte(j))
            // Запис даних NALU
            b = append(b, nalu...)
        }
        pkt.Data = b  // заміна даних пакету на переформатовані
    }
    
    // 3. Додавання пакету у буфер pending
    f.pending = append(f.pending, pkt)
    return nil
}
```

### 🔍 Чому потрібно переформатування H.264 NALU?

```
Існують два формати зберігання H.264 NALU:

1. Annex B (використовується у HLS TS, RTMP, тощо):
   • Кожен NALU починається з 3-4 байтового start code: 0x000001 або 0x00000001
   • Приклад: [00 00 00 01][NALU data][00 00 00 01][NALU data]...

2. AVCC / length-prefixed (використовується у MP4/fMP4):
   • Кожен NALU починається з 4-байтового розміру у big-endian
   • Приклад: [00 00 00 1C][NALU data][00 00 00 0A][NALU data]...

Чому fMP4 вимагає AVCC:
• MP4 специфікація вимагає length-prefixed формат для Sample Entry
• Дозволяє швидкий доступ до окремих NALU без сканування на start codes
• Сумісність з існуючими плеєрами та інструментами

Логіка переформатування:
  for _, nalu := range nalus {
      j := len(nalu)
      // Запис 4-байтового розміру: j>>24 = старший байт, j = молодший
      b = append(b, byte(j>>24), byte(j>>16), byte(j>>8), byte(j))
      b = append(b, nalu...)  // дані NALU
  }

Приклад:
  Вхід (Annex B): [00 00 00 01][67 42 00 1E...]  // SPS NALU
  Вихід (AVCC):   [00 00 00 06][67 42 00 1E...]  // 6 байт = розмір NALU
```

### ⚠️ Критична проблема: неефективна аллокація буфера

```
У поточному коді:
    b := make([]byte, 0, len(pkt.Data)+3*len(nalus))

Проблема:
• `3*len(nalus)` — це оцінка додаткових байт для 4-байтових розмірів
• Але кожен NALU додає рівно 4 байти, не 3
• Правильна формула: `len(pkt.Data) + 4*len(nalus)`

Наслідки:
• Може статися realloc під час append → зниження продуктивності
• Для великих пакетів з багатьма NALU це може бути суттєвим

✅ Виправлення:
    b := make([]byte, 0, len(pkt.Data) + 4*len(nalus))
```

### ✅ Ваш use-case**: фільтрація пакетів перед буферизацією

```go
// FilteringTrackFragmenter — обгортка для фільтрації пакетів
type FilteringTrackFragmenter struct {
    base   *fmp4.TrackFragmenter
    filter func(av.Packet) bool
}

func NewFilteringTrackFragmenter(base *fmp4.TrackFragmenter, filter func(av.Packet) bool) *FilteringTrackFragmenter {
    return &FilteringTrackFragmenter{
        base:   base,
        filter: filter,
    }
}

func (f *FilteringTrackFragmenter) WritePacket(pkt av.Packet) error {
    if f.filter != nil && !f.filter(pkt) {
        // Пропускаємо пакет (напр. для зменшення бітрейту)
        return nil
    }
    return f.base.WritePacket(pkt)
}

// Делегування інших методів
func (f *FilteringTrackFragmenter) Fragment() (fragment.Fragment, error) {
    return f.base.Fragment()
}
func (f *FilteringTrackFragmenter) Duration() time.Duration {
    return f.base.Duration()
}
// ... інші методи ...

// Використання для adaptive bitrate:
filter := func(pkt av.Packet) bool {
    // Пропускати неключові відео кадри якщо бітрейт занадто високий
    if !pkt.IsKeyFrame && pkt.Time%time.Second < 100*time.Millisecond {
        return rand.Float32() > 0.5  // пропускати 50% неключових кадрів
    }
    return true
}
filtered := NewFilteringTrackFragmenter(fragmenter, filter)
```

---

## 🔑 3. Duration() — розрахунок тривалості буфера

### 🔧 Реалізація:

```go
func (f *TrackFragmenter) Duration() time.Duration {
    if len(f.pending) < 2 {
        return 0
    }
    return f.pending[len(f.pending)-1].Time - f.pending[0].Time
}
```

### 🔍 Призначення:

```
Duration() повертає різницю часу між першим та останнім пакетом у буфері:

• Використовується для визначення коли фрагмент готовий до відправки
• Типовий поріг: 2-4 секунди для HLS, 0.5-2 секунди для low-latency
• Для single-track CMAF: тривалість визначається за цим треком

Формула:
  duration = last_packet.Time - first_packet.Time

Приклад:
  pending = [
    {Time: 1000ms, ...},  // перший пакет
    {Time: 1500ms, ...},
    {Time: 2000ms, ...},  // останній пакет
  ]
  Duration() = 2000 - 1000 = 1000ms = 1 секунда
```

### ⚠️ Критична проблема: неврахування розривів у часі

```
У поточній реалізації:
    return f.pending[len(f.pending)-1].Time - f.pending[0].Time

Проблема:
• Якщо є розрив у потоці (напр. втрата пакетів, seek), duration може бути некоректним
• Приклад: pending = [pkt@1000ms, pkt@5000ms] → duration = 4000ms, але фактично тільки 2 пакети

✅ Виправлення: розрахунок кумулятивної тривалості
    func (f *TrackFragmenter) CumulativeDuration() time.Duration {
        if len(f.pending) < 2 {
            return 0
        }
        
        var total time.Duration
        for i := 1; i < len(f.pending); i++ {
            total += f.pending[i].Time - f.pending[i-1].Time
        }
        return total
    }
    
    // АБО: використання Duration пакетів замість різниці часу
    func (f *TrackFragmenter) PacketBasedDuration() time.Duration {
        var total time.Duration
        for _, pkt := range f.pending {
            total += pkt.Duration
        }
        return total
    }
```

### ✅ Ваш use-case**: визначення готовності фрагменту

```go
// ShouldFlushFragment — перевірка чи накопичено достатньо даних для фрагменту
func ShouldFlushFragment(fragmenter *fmp4.TrackFragmenter, targetDuration time.Duration) bool {
    // 1. Перевірка тривалості
    if fragmenter.Duration() >= targetDuration {
        return true
    }
    
    // 2. Перевірка наявності ключового кадру (для відео)
    // (якщо це відео трек і є ключовий кадр — можна завершити фрагмент)
    for _, pkt := range fragmenter.pending {  // припустимо pending експортовано або є метод
        if pkt.IsKeyFrame {
            return true
        }
    }
    
    // 3. Перевірка мінімальної кількості пакетів
    if len(fragmenter.pending) >= 10 {  // принаймні 10 пакетів
        return true
    }
    
    return false
}

// Використання у streaming циклі:
targetDuration := 4 * time.Second  // 4-секундні сегменти для HLS
if ShouldFlushFragment(fragmenter, targetDuration) {
    frag, err := fragmenter.Fragment()
    if err != nil { /* handle error */ }
    if frag.Length > 0 {
        sendFragment(frag)  // відправка клієнту
    }
}
```

---

## 🔑 4. Fragment() — генерація готового фрагменту

### 🔧 Основна логіка:

```go
func (f *TrackFragmenter) Fragment() (fragment.Fragment, error) {
    // 1. Розрахунок тривалості
    dur := f.Duration()
    
    // 2. Побудова метаданих фрагменту
    tf := f.makeFragment()
    if tf.trackFrag == nil {
        // Недостатньо пакетів для фрагментації
        return fragment.Fragment{}, nil
    }
    
    // 3. Оновлення стану
    f.seqNum++              // інкремент послідовного номера
    initial := !f.shdrw     // чи це перший фрагмент сегменту
    f.shdrw = true          // позначити що header вже записано
    
    // 4. Серіалізація у фінальний формат
    frag := marshalFragment([]fragmentWithData{tf}, f.seqNum, initial)
    frag.Duration = dur     // встановлення тривалості
    return frag, nil
}
```

### 🔍 Логіка `shdrw` прапорця для single-track:

```
shdrw = "segment header written"

Для single-track CMAF:
• NewSegment() → f.shdrw = false
• Fragment() → initial = !f.shdrw = true → включити styp у фрагмент
• Fragment() → f.shdrw = true → наступні фрагменти без styp
• NewSegment() → повторення циклу

Це дозволяє:
• Клієнту розпізнати початок нового сегменту
• Економити байти не дублюючи styp у кожному фрагменті
• Підтримувати low-latency streaming з короткими сегментами
```

### ✅ Ваш use-case**: low-latency streaming з примусовою фрагментацією

```go
// LowLatencyTrackFragmenter — фрагментатор з примусовою генерацією
type LowLatencyTrackFragmenter struct {
    base        *fmp4.TrackFragmenter
    minDuration time.Duration  // мінімальна тривалість фрагменту
    maxPackets  int           // максимальна кількість пакетів у фрагменті
}

func NewLowLatencyTrackFragmenter(base *fmp4.TrackFragmenter, minDur time.Duration, maxPkt int) *LowLatencyTrackFragmenter {
    return &LowLatencyTrackFragmenter{
        base:        base,
        minDuration: minDur,
        maxPackets:  maxPkt,
    }
}

func (f *LowLatencyTrackFragmenter) ShouldGenerateFragment() bool {
    // Примусова генерація якщо:
    // 1. Досягнуто мінімальної тривалості АБО
    // 2. Накопичено максимальну кількість пакетів
    return f.base.Duration() >= f.minDuration || 
           len(f.base.pending) >= f.maxPackets
}

func (f *LowLatencyTrackFragmenter) GenerateFragment() (fragment.Fragment, error) {
    if !f.ShouldGenerateFragment() {
        return fragment.Fragment{}, nil  // ще не готовий
    }
    
    // Примусова генерація навіть з малою кількістю даних
    return f.base.Fragment()
}

// Використання для low-latency streaming:
llFragmenter := NewLowLatencyTrackFragmenter(
    fragmenter,
    500*time.Millisecond,  // мінімум 0.5с тривалості
    15,                    // або максимум 15 пакетів
)

// У streaming циклі:
if llFragmenter.ShouldGenerateFragment() {
    frag, err := llFragmenter.GenerateFragment()
    if err != nil { /* handle error */ }
    if frag.Length > 0 {
        sendFragmentLowLatency(frag)  // відправка з мінімальною затримкою
    }
}
```

---

## 🔑 5. NewSegment() та MovieHeader() — управління сегментами

### 🔧 Реалізація:

```go
func (f *TrackFragmenter) NewSegment() {
    f.shdrw = false  // скидання прапорця для включення styp у наступний фрагмент
}

func (f *TrackFragmenter) MovieHeader() (filename, contentType string, blob []byte) {
    return "init.mp4", "video/mp4", f.fhdr  // повернення кешованого init segment
}
```

### 🔍 Призначення:

```
NewSegment():
• Сигналізує про початок нового логічного сегменту
• Скидає shdrw → наступний Fragment() включить styp атом
• Критично для adaptive bitrate switching та low-latency streaming

MovieHeader():
• Повертає кешований init segment (ftyp + moov)
• filename = "init.mp4", contentType = "video/mp4"
• Клієнт використовує це для ініціалізації декодера

Важливо:
• Init segment відправляється тільки один раз на початку сесії
• Усі медіа-фрагменти посилаються на метадані з init segment
• Зміна кодеків вимагає нового init segment (новий сеанс)
```

### ✅ Ваш use-case**: адаптивне перемикання якості

```go
// AdaptiveQualitySwitcher — перемикання між якостями для single-track
type AdaptiveQualitySwitcher struct {
    fragmenters map[string]*fmp4.TrackFragmenter  // quality -> fragmenter
    currentQuality string
    clientState *ClientState
}

type ClientState struct {
    Bandwidth   int64  // оцінка пропускної здатності клієнта
    LastSeq     uint32 // останній отриманий seqNum
    BufferLevel time.Duration // рівень буферу клієнта
}

func (s *AdaptiveQualitySwitcher) ShouldSwitchQuality() bool {
    state := s.clientState
    
    // Логіка перемикання на основі пропускної здатності та буферу
    if state.Bandwidth < 1_000_000 && s.currentQuality != "low" {
        return true  // перемкнути на low якщо бітрейт < 1 Mbps
    }
    if state.Bandwidth > 5_000_000 && s.currentQuality != "high" {
        return true  // перемкнути на high якщо бітрейт > 5 Mbps
    }
    if state.BufferLevel < 2*time.Second && s.currentQuality != "low" {
        return true  // перемкнути на low якщо буфер майже порожній
    }
    
    return false
}

func (s *AdaptiveQualitySwitcher) SwitchQuality(newQuality string) error {
    // 1. Відправка нового init segment для нової якості
    fragmenter := s.fragmenters[newQuality]
    filename, contentType, initBytes := fragmenter.MovieHeader()
    
    // Спеціальне повідомлення для клієнта про зміну якості
    if err := sendQualitySwitchInit(s.clientState.Conn, filename, contentType, initBytes); err != nil {
        return err
    }
    
    // 2. Оновлення стану
    s.currentQuality = newQuality
    s.clientState.LastSeq = 0  // скидання seqNum для нової якості
    
    // 3. Сигнал про початок нового сегменту
    fragmenter.NewSegment()
    
    return nil
}

// Використання у streaming циклі:
if switcher.ShouldSwitchQuality() {
    newQuality := determineBestQuality(switcher.clientState)
    if err := switcher.SwitchQuality(newQuality); err != nil {
        log.Printf("error switching quality: %v", err)
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Single-track CMAF streaming через WebSocket

```go
// CMAFSingleTrackStreamer — streaming одного треку через WebSocket
type CMAFSingleTrackStreamer struct {
    fragmenter *fmp4.TrackFragmenter
    conn       *websocket.Conn
    mu         sync.Mutex
}

func (s *CMAFSingleTrackStreamer) Stream(ctx context.Context, demuxer av.Demuxer) error {
    // 1. Відправка init segment
    _, _, initBytes := s.fragmenter.MovieHeader()
    if err := s.sendBinary(initBytes); err != nil {
        return fmt.Errorf("send init: %w", err)
    }
    
    // 2. Основний цикл з low-latency налаштуваннями
    flushInterval := 200 * time.Millisecond  // часта перевірка для low-latency
    ticker := time.NewTicker(flushInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-ticker.C:
            // Примусова генерація фрагменту для low-latency
            frag, err := s.fragmenter.Fragment()
            if err != nil {
                return fmt.Errorf("generate fragment: %w", err)
            }
            if frag.Length == 0 {
                continue  // немає достатньо даних
            }
            
            // Відправка через WebSocket
            if err := s.sendFragment(frag); err != nil {
                return fmt.Errorf("send fragment: %w", err)
            }
            
            // Логування метрик
            log.Printf("Sent fragment %d: %d bytes, %v duration", 
                s.fragmenter.seqNum-1, frag.Length, frag.Duration)
            
        default:
            // Неблокуюче читання пакетів
            pkt, err := demuxer.ReadPacket()
            if err == io.EOF {
                return nil
            }
            if err != nil && err != io.ErrNoData {
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

func (s *CMAFSingleTrackStreamer) sendFragment(frag fragment.Fragment) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // Формат повідомлення: [4-byte length][fragment data]
    header := make([]byte, 4)
    binary.BigEndian.PutUint32(header, uint32(frag.Length))
    
    message := append(header, frag.Bytes[:frag.Length]...)
    return s.conn.WriteMessage(websocket.BinaryMessage, message)
}

func (s *CMAFSingleTrackStreamer) sendBinary(data []byte) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.conn.WriteMessage(websocket.BinaryMessage, data)
}
```

### 🔧 Приклад: Мульти-трек CMAF streaming (відео + аудіо)

```go
// CMAFMultiTrackStreamer — об'єднання окремих TrackFragmenter у multi-track
type CMAFMultiTrackStreamer struct {
    videoFrag *fmp4.TrackFragmenter
    audioFrag *fmp4.TrackFragmenter
    conn      *websocket.Conn
    mu        sync.Mutex
    seqNum    uint32  // спільний seqNum для синхронізації
}

func (s *CMAFMultiTrackStreamer) Stream(ctx context.Context, videoDemux, audioDemux av.Demuxer) error {
    // 1. Відправка init segment з обома треками
    videoTrack, _ := s.videoFrag.Track()
    audioTrack, _ := s.audioFrag.Track()
    initBytes, _ := MovieHeader([]*fmp4io.Track{videoTrack, audioTrack})
    
    if err := s.sendBinary(initBytes); err != nil {
        return fmt.Errorf("send init: %w", err)
    }
    
    // 2. Основний цикл з синхронізацією треків
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-ticker.C:
            // Читання пакетів з обох демуксерів
            readPackets(videoDemux, s.videoFrag)
            readPackets(audioDemux, s.audioFrag)
            
            // Генерація фрагменту тільки якщо обидва треки готові
            if s.videoFrag.Duration() >= 2*time.Second && 
               s.audioFrag.Duration() >= 2*time.Second {
                
                // Отримання фрагментів з кожного треку
                videoFrag, _ := s.videoFrag.Fragment()
                audioFrag, _ := s.audioFrag.Fragment()
                
                if videoFrag.Length > 0 || audioFrag.Length > 0 {
                    // Об'єднання фрагментів у один multi-track фрагмент
                    combined := combineFragments([]fragment.Fragment{videoFrag, audioFrag}, s.seqNum)
                    s.seqNum++
                    
                    if err := s.sendFragment(combined); err != nil {
                        return fmt.Errorf("send fragment: %w", err)
                    }
                }
            }
        }
    }
}

func readPackets(demuxer av.Demuxer, fragmenter *fmp4.TrackFragmenter) {
    for {
        pkt, err := demuxer.ReadPacket()
        if err != nil {
            break  // немає даних або помилка
        }
        if err := fragmenter.WritePacket(pkt); err != nil {
            log.Printf("warning: write packet: %v", err)
            break
        }
    }
}

func combineFragments(frags []fragment.Fragment, seqNum uint32) fragment.Fragment {
    // Спрощена реалізація: об'єднання байтів з усіх фрагментів
    var combinedBytes []byte
    totalLength := 0
    
    for _, frag := range frags {
        if frag.Length > 0 {
            combinedBytes = append(combinedBytes, frag.Bytes[:frag.Length]...)
            totalLength += frag.Length
        }
    }
    
    return fragment.Fragment{
        Bytes:       combinedBytes,
        Length:      totalLength,
        Independent: frags[0].Independent,  // припускаємо що відео визначає independent
        Duration:    frags[0].Duration,     // припускаємо що відео визначає duration
    }
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Некоректне переформатування H.264 NALU** | Клієнт не може декодувати відео | Переконайтеся що `h264parser.SplitNALUs()` повертає правильний формат та що AVCC конвертація коректна |
| **Невірний розрахунок Duration()** | Фрагменти занадто короткі/довгі | Використовуйте кумулятивну тривалість або Duration пакетів замість різниці часу |
| **Відсутній styp у першому фрагменті сегменту** | Клієнт не розпізнає початок нового сегменту | Перевірте логіку `shdrw` прапорця та виклик `NewSegment()` |
| **Неунікальний seqNum між треками** | Розсинхронізація при multi-track streaming | Використовуйте спільний `seqNum` для всіх треків у multi-track режимі |
| **Переповнення буфера pending** | Витрата пам'яті при повільному клієнті | Додайте обмеження на розмір `pending` або таймаут для старих пакетів |

---

## ⚡ Оптимізації для high-performance фрагментації

### 1. Reuse буферів для AVCC конвертації:

```go
var avccBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір переформатованого пакету: 1-2 MB для відео
        buf := make([]byte, 0, 2*1024*1024)
        return &buf
    },
}

func getAVCCBuffer() *[]byte {
    return avccBufferPool.Get().(*[]byte)
}

func putAVCCBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    avccBufferPool.Put(b)
}

// Використання у WritePacket():
buf := getAVCCBuffer()
defer putAVCCBuffer(buf)
// ... переформатування у buf ...
pkt.Data = append(*buf, pkt.Data...)  // або заміна посилання
```

### 2. Попередній розрахунок розміру буфера для AVCC:

```go
// Точна оцінка розміру для AVCC конвертації
func estimateAVCCSize(originalSize int, naluCount int) int {
    // Кожен NALU додає 4 байти для розміру
    return originalSize + 4*naluCount
}

// Використання:
estimatedSize := estimateAVCCSize(len(pkt.Data), len(nalus))
b := make([]byte, 0, estimatedSize)  // cap = estimatedSize для уникнення realloc
```

### 3. Моніторинг продуктивності фрагментації:

```go
type TrackFragmenterMetrics struct {
    PacketsWritten prometheus.CounterVec
    FragmentsGenerated prometheus.CounterVec
    FragmentLatency prometheus.HistogramVec
    FragmentSizes prometheus.HistogramVec
    AVCCConversionLatency prometheus.HistogramVec
}

func (m *TrackFragmenterMetrics) RecordPacket(codec av.CodecType, size int) {
    m.PacketsWritten.WithLabelValues(codec.String()).Inc()
}

func (m *TrackFragmenterMetrics) RecordFragment(duration time.Duration, size int, seqNum uint32) {
    m.FragmentsGenerated.Inc()
    m.FragmentLatency.Observe(duration.Seconds())
    m.FragmentSizes.Observe(float64(size))
}

func (m *TrackFragmenterMetrics) RecordAVCCConversion(naluCount int, duration time.Duration) {
    m.AVCCConversionLatency.Observe(duration.Seconds())
}
```

---

## 📋 Чек-лист безпечного використання TrackFragmenter

```go
// ✅ 1. Валідація кодека перед створенням фрагментатора
if codecData == nil {
    return fmt.Errorf("codecData cannot be nil")
}
switch codecData.Type() {
case av.H264, av.AAC, av.Opus:
    // підтримувані
default:
    return fmt.Errorf("unsupported codec: %v", codecData.Type())
}

// ✅ 2. Коректне переформатування H.264 NALU
nalus, typ := h264parser.SplitNALUs(pkt.Data)
if typ != h264parser.NALU_AVCC {
    // Використання точної оцінки розміру буфера
    estimatedSize := len(pkt.Data) + 4*len(nalus)
    b := make([]byte, 0, estimatedSize)
    for _, nalu := range nalus {
        j := len(nalu)
        b = append(b, byte(j>>24), byte(j>>16), byte(j>>8), byte(j))
        b = append(b, nalu...)
    }
    pkt.Data = b
}

// ✅ 3. Обмеження розміру буфера pending
const maxPendingPackets = 1000
if len(f.pending) >= maxPendingPackets {
    // Видалення найстаріших пакетів або повернення помилки
    f.pending = f.pending[maxPendingPackets/2:]  // видалити половину
}

// ✅ 4. Коректне використання Bytes[:Length] для доступу до даних
data := frag.Bytes[:frag.Length]  // ✅ правильно
// data := frag.Bytes             // ❌ може включати неініціалізовану пам'ять

// ✅ 5. Синхронізація seqNum для multi-track streaming
// Використовуйте спільний seqNum для всіх треків у multi-track режимі

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Generated fragment: seq=%d, size=%d, duration=%v, independent=%v", 
    f.seqNum-1, frag.Length, frag.Duration, frag.Independent)

// ✅ 7. Метрики для моніторингу
metrics.RecordFragment(frag.Duration, frag.Length, f.seqNum-1)
```

---

## 🔗 Корисні посилання

- 💻 [CMAF Specification (ISO/IEC 23009-1)](https://www.iso.org/standard/74428.html) — офіційний стандарт
- 📄 [HLS fMP4 Guide](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 📄 [H.264 Annex B vs AVCC](https://wiki.videolan.org/NAL/) — порівняння форматів NALU
- 🧪 [Go sync.Pool Documentation](https://pkg.go.dev/sync#Pool) — ефективне управління пам'яттю
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Використовуйте точну оцінку розміру для AVCC буфера** (`+4*len(nalus)` замість `+3*len(nalus)`) — уникнення realloc.
> 2. **Валідуйте кодек перед створенням фрагментатора** — уникнення помилок ініціалізації.
> 3. **Обмежуйте розмір буфера `pending`** — уникнення витрати пам'яті при повільному клієнті.
> 4. **Використовуйте Bytes[:Length] для доступу до даних** — уникнення читання неініціалізованої пам'яті.
> 5. **Синхронізуйте seqNum для multi-track streaming** — забезпечення коректного порядку відтворення.

Потрібен приклад реалізації повного циклу low-latency CMAF streaming з WebSocket, або допомога з оптимізацією AVCC конвертації для high-FPS відео? Готовий допомогти! 🚀