# ⏱️ Глибокий розбір: pktque.Timeline — часовий менеджер для медіа-сегментів

Цей файл — **реалізація часової черги (timeline queue)** для керування таймінгами медіа-сегментів у бібліотеці `vdk`. Вона дозволяє коректно обробляти розриви, перекриття та нерівномірні інтервали між пакетами.

Розберемо архітектуру, алгоритми та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема Timeline

```
┌────────────────────────────────────────┐
│ 📦 pktque.Timeline — Time Queue Design │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • tlSeg — сегмент: {час, тривалість}  │
│  • segs[] — черга сегментів (FIFO)     │
│  • headtm — поточна позиція на таймлайні│
│                                         │
│  🔄 Операції:                           │
│  • Push(tm, dur) — додати сегмент      │
│  • Pop(dur) — "пройти" dur часу,       │
│               повернути початковий час │
│                                         │
│  🎯 Призначення:                        │
│  • Вирівнювання таймінгів потоків      │
│  • Обробка розривів/перекриттів        │
│  • Точна сегментація для HLS           │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Структура даних: tlSeg та Timeline

### Базові типи:

```go
// tlSeg — один сегмент на часовій осі
type tlSeg struct {
    tm  time.Duration  // початковий час сегменту
    dur time.Duration  // тривалість сегменту
}

// Timeline — черга сегментів з підтримкою часових операцій
type Timeline struct {
    segs   []tlSeg       // FIFO черга сегментів
    headtm time.Duration // поточна позиція "голови" на таймлайні
}
```

### 🎯 Візуалізація стану:

```
Часова вісь:
<---|----[seg0]----|----[seg1]----|----[seg2]----|--->
    ↑             ↑             ↑             ↑
  headtm      seg0.tm+dur   seg1.tm+dur   tail

Push(100ms, 20ms):
  Додає сегмент [100ms, 20ms] у кінець черги

Pop(15ms):
  "Проходить" 15ms вздовж таймлайну
  Повертає початковий час (до проходження)
  Оновлює headtm += 15ms
```

---

## ➕ 2. Push() — додавання сегменту з корекцією розривів

```go
func (self *Timeline) Push(tm time.Duration, dur time.Duration) {
    // 1. Якщо є попередній сегмент — перевіряємо неперервність
    if len(self.segs) > 0 {
        tail := self.segs[len(self.segs)-1]  // останній сегмент
        
        // Розрахунок розриву: очікуваний кінець → фактичний початок
        diff := tm - (tail.tm + tail.dur)
        
        // Якщо розрив від'ємний (перекриття) — зсуваємо новий сегмент
        if diff < 0 {
            tm -= diff  // tm = tm + |diff| → вирівнюємо до кінця попереднього
        }
        // Якщо diff > 0 (розрив) — залишаємо як є (допускаємо прогалини)
    }
    
    // 2. Додаємо скоригований сегмент у чергу
    self.segs = append(self.segs, tlSeg{tm, dur})
}
```

### 🔍 Логіка корекції перекриттів:

```
Приклад 1: Перекриття (diff < 0)
  Попередній сегмент: [100ms, 20ms] → закінчується на 120ms
  Новий сегмент:      [110ms, 30ms] → починається на 110ms (перекриває!)
  
  diff = 110 - (100+20) = -10ms
  tm -= diff → tm = 110 - (-10) = 120ms  ← зсуваємо до кінця попереднього
  
  Результат: [100-120ms] + [120-150ms] → неперервна послідовність ✓

Приклад 2: Розрив (diff > 0)
  Попередній сегмент: [100ms, 20ms] → закінчується на 120ms
  Новий сегмент:      [150ms, 30ms] → починається на 150ms (прогалина 30ms)
  
  diff = 150 - 120 = +30ms (>0)
  tm не змінюється
  
  Результат: [100-120ms] + [150-180ms] → прогалина 30ms (допустимо для live) ✓
```

### ✅ Ваш use-case: обробка розривів у CCTV потоці

```
Проблема: Камера втратила мережу на 5 секунд → таймінги "перестрибнули".

До корекції:
  Пакет 100: [1000ms, 40ms] → закінчується на 1040ms
  Пакет 101: [6000ms, 40ms] → починається на 6000ms (розрив 4960ms!)

Після Push() з Timeline:
  Пакет 100: [1000ms, 40ms] → закінчується на 1040ms
  Пакет 101: [6000ms, 40ms] → diff = +4960ms (>0) → залишаємо як є
  
Результат: Прогалина зберігається → HLS плеєр може показати "буферизацію" 
           або пропустити цей інтервал, але не "зламається".
```

---

## ➖ 3. Pop() — "проходження" часу вздовж таймлайну

```go
func (self *Timeline) Pop(dur time.Duration) (tm time.Duration) {
    // 1. Якщо черга порожня — повертаємо поточний headtm
    if len(self.segs) == 0 {
        return self.headtm
    }
    
    // 2. Запам'ятовуємо початковий час (до проходження)
    tm = self.segs[0].tm
    
    // 3. "Проходимо" dur вздовж черги сегментів
    for dur > 0 && len(self.segs) > 0 {
        seg := &self.segs[0]  // працюємо з першим сегментом
        
        // Скільки можемо "взяти" з цього сегменту
        sub := dur
        if seg.dur < sub {
            sub = seg.dur  // беремо тільки те, що залишилось у сегменті
        }
        
        // Оновлюємо стан
        seg.dur -= sub      // зменшуємо тривалість сегменту
        dur -= sub          // зменшуємо залишок для проходження
        seg.tm += sub       // зсуваємо початок сегменту (він "скорочується" зліва)
        self.headtm += sub  // просуваємо голову таймлайну
        
        // Якщо сегмент "вичерпано" — видаляємо з черги
        if seg.dur == 0 {
            // Ефективне видалення з початку слайсу:
            copy(self.segs[0:], self.segs[1:])  // зсув елементів вліво
            self.segs = self.segs[:len(self.segs)-1]  // зменшення довжини
        }
    }
    
    // 4. Повертаємо початковий час (до проходження)
    return tm
}
```

### 🔍 Покроковий приклад `Pop(25ms)`:

```
Початковий стан:
  segs = [
    {tm: 100ms, dur: 20ms},  // сегмент 0: [100-120ms]
    {tm: 120ms, dur: 30ms},  // сегмент 1: [120-150ms]
  ]
  headtm = 100ms

Крок 1: dur=25ms, seg0.dur=20ms
  sub = min(25, 20) = 20ms
  seg0.dur = 20-20 = 0  ← сегмент вичерпано!
  dur = 25-20 = 5ms (ще треба пройти)
  seg0.tm = 100+20 = 120ms
  headtm = 100+20 = 120ms
  Видаляємо seg0 → segs = [{tm: 120ms, dur: 30ms}]

Крок 2: dur=5ms, seg1.dur=30ms
  sub = min(5, 30) = 5ms
  seg1.dur = 30-5 = 25ms
  dur = 5-5 = 0ms ← готово!
  seg1.tm = 120+5 = 125ms
  headtm = 120+5 = 125ms

Результат:
  Повернуто: tm = 100ms (початковий час до проходження)
  Новий стан:
    segs = [{tm: 125ms, dur: 25ms}]  // сегмент 1: [125-150ms]
    headtm = 125ms
```

### ✅ Ваш use-case: точна сегментація для HLS

```go
// HLSGenerator — генерація 10-секундних сегментів
type HLSGenerator struct {
    timeline      *pktque.Timeline
    segmentDuration time.Duration  // 10 секунд
    currentSegment []av.Packet
}

func (g *HLSGenerator) AddPacket(pkt av.Packet) {
    // 1. Додаємо пакет у timeline (з його таймінгом та тривалістю)
    g.timeline.Push(pkt.Time, pkt.Duration)
    
    // 2. Додаємо у поточний сегмент
    g.currentSegment = append(g.currentSegment, pkt)
    
    // 3. Перевіряємо чи набрався повний сегмент
    //    "Проходимо" segmentDuration вздовж таймлайну
    startTime := g.timeline.Pop(g.segmentDuration)
    
    // Якщо повернутий startTime відрізняється від попереднього — 
    // ми "перейшли" межу сегменту!
    if startTime != g.lastSegmentStart {
        // Фіналізуємо поточний сегмент
        g.finalizeSegment(g.currentSegment, g.lastSegmentStart)
        
        // Починаємо новий
        g.currentSegment = nil
        g.lastSegmentStart = startTime
    }
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// timeline_sync.go — синхронізація відео/аудіо/субтитрів через Timeline
type StreamSynchronizer struct {
    channelID   string
    videoTL     *pktque.Timeline  // таймлайн для відео
    audioTL     *pktque.Timeline  // таймлайн для аудіо
    subtitleTL  *pktque.Timeline  // таймлайн для субтитрів
    targetDur   time.Duration     // цільова тривалість сегменту (10с)
}

func NewStreamSynchronizer(channelID string) *StreamSynchronizer {
    return &StreamSynchronizer{
        channelID:  channelID,
        videoTL:    &pktque.Timeline{},
        audioTL:    &pktque.Timeline{},
        subtitleTL: &pktque.Timeline{},
        targetDur:  10 * time.Second,
    }
}

// ProcessPacket — обробка пакету з будь-якого потоку
func (s *StreamSynchronizer) ProcessPacket(pkt av.Packet, streamType string) error {
    // 1. Вибір таймлайну за типом потоку
    var tl *pktque.Timeline
    switch streamType {
    case "video":
        tl = s.videoTL
    case "audio":
        tl = s.audioTL
    case "subtitle":
        tl = s.subtitleTL
    default:
        return fmt.Errorf("unknown stream type: %s", streamType)
    }
    
    // 2. Додавання пакету у таймлайн
    tl.Push(pkt.Time, pkt.Duration)
    
    // 3. Перевірка чи можна сформувати сегмент
    if s.canFormSegment() {
        return s.generateSegment()
    }
    
    return nil
}

// canFormSegment — перевірка чи всі потоки мають достатньо даних
func (s *StreamSynchronizer) canFormSegment() bool {
    // Сегмент можна формувати якщо всі таймлайни мають >= targetDur даних
    return s.videoHas(s.targetDur) && 
           s.audioHas(s.targetDur) && 
           s.subtitleHas(s.targetDur)
}

func (s *StreamSynchronizer) videoHas(dur time.Duration) bool {
    // "Поп"-ємо targetDur і перевіряємо чи вистачило даних
    // (спрощена логіка — на практиці треба кешувати результат)
    _ = s.videoTL.Pop(dur)
    return true  // спрощено
}

// generateSegment — формування синхронізованого сегменту
func (s *StreamSynchronizer) generateSegment() error {
    // 1. "Проходимо" targetDur по всіх таймлайнах
    videoStart := s.videoTL.Pop(s.targetDur)
    audioStart := s.audioTL.Pop(s.targetDur)
    subtitleStart := s.subtitleTL.Pop(s.targetDur)
    
    // 2. Визначаємо загальний початок (мінімум для синхронізації)
    segmentStart := min(videoStart, audioStart, subtitleStart)
    
    // 3. Витягуємо пакети, що потрапляють у [segmentStart, segmentStart+targetDur]
    videoPkts := s.extractPacketsInRange(s.videoTL, segmentStart, s.targetDur)
    audioPkts := s.extractPacketsInRange(s.audioTL, segmentStart, s.targetDur)
    subtitlePkts := s.extractPacketsInRange(s.subtitleTL, segmentStart, s.targetDur)
    
    // 4. Формуємо HLS-сегмент
    segment := &HLSSegment{
        ChannelID:  s.channelID,
        StartTime:  segmentStart,
        Duration:   s.targetDur,
        Video:      videoPkts,
        Audio:      audioPkts,
        Subtitles:  subtitlePkts,
    }
    
    // 5. Запис у файл / відправка у WebSocket
    return s.writeSegment(segment)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Pop() повертає невірний час** | Неправильна ініціалізація headtm | Переконайтеся, що перший `Push()` має коректний `tm` (не 0, якщо потік не з початку) |
| **Сегменти "плавають" у часі** | Розриви між пакетами не обробляються | Використовуйте `FixTime` фільтр перед `Timeline` для вирівнювання таймінгів |
| **Пам'ять росте через segs[]** | `Pop()` не викликається регулярно | Додайте періодичний `Pop(0)` для "очищення" вже оброблених сегментів |
| **Перекриття ламають синхронізацію** | `Push()` зсуває лише новий сегмент, не попередній | Якщо потрібна строга неперервність — додайте логіку корекції попередніх сегментів |
| **headtm "відстає" від реального часу** | `Pop()` викликається з неправильним `dur` | Переконайтеся, що `dur` у `Pop()` відповідає реальній тривалості оброблених даних |

---

## ⚡ Оптимізації для real-time обробки

### 1. Попереднє виділення segs[]:

```go
// Замість росту слайсу "на льоту":
func NewPreallocatedTimeline(capacity int) *Timeline {
    return &Timeline{
        segs: make([]tlSeg, 0, capacity),  // попередньо виділена пам'ять
    }
}

// Для CCTV з високим фреймрейтом:
tl := NewPreallocatedTimeline(100)  // 100 сегментів ≈ 1-2 секунди відео
```

### 2. Пакетний Pop для зменшення накладних витрат:

```go
// PopBatch — "проходження" кількох інтервалів за один виклик
func (tl *Timeline) PopBatch(durations []time.Duration) []time.Duration {
    results := make([]time.Duration, 0, len(durations))
    for _, dur := range durations {
        results = append(results, tl.Pop(dur))
    }
    return results
}
```

### 3. Моніторинг стану таймлайну:

```go
type TimelineMetrics struct {
    SegmentCount   prometheus.Gauge
    TotalDuration  prometheus.Gauge
    GapCount       prometheus.Counter  // кількість виявлених розривів
}

func (tl *Timeline) ReportMetrics(m *TimelineMetrics) {
    m.SegmentCount.Set(float64(len(tl.segs)))
    
    var totalDur time.Duration
    for _, seg := range tl.segs {
        totalDur += seg.dur
    }
    m.TotalDuration.Set(totalDur.Seconds())
}
```

---

## 📋 Чек-лист інтеграції Timeline

```go
// ✅ 1. Ініціалізація таймлайнів для кожного потоку
videoTL := &pktque.Timeline{}
audioTL := &pktque.Timeline{}

// ✅ 2. Додавання пакетів з корекцією перекриттів
for _, pkt := range videoPackets {
    videoTL.Push(pkt.Time, pkt.Duration)
}

// ✅ 3. "Проходження" часу для сегментації
segmentStart := videoTL.Pop(10 * time.Second)

// ✅ 4. Синхронізація з іншими потоками
audioStart := audioTL.Pop(10 * time.Second)
// Якщо videoStart != audioStart → логувати десинхронізацію

// ✅ 5. Очищення "старих" сегментів (якщо потрібно)
// Pop(0) не змінює headtm, але видаляє вичерпані сегменти
_ = videoTL.Pop(0)

// ✅ 6. Метрики
monitoring.TimelineSegments.Set(float64(len(videoTL.segs)))
monitoring.TimelineHeadTime.Set(float64(videoTL.headtm.Milliseconds()))
```

---

## 🔗 Корисні посилання

- 💻 [vdk pktque Package](https://pkg.go.dev/github.com/deepch/vdk/av/pktque) — GoDoc documentation
- 📄 [HLS Segment Timing](https://datatracker.ietf.org/doc/html/rfc8216#section-3.4.2) — вимоги до таймінгів у HLS
- 🎬 [MPEG-TS Timing Model](https://www.iso.org/standard/61246.html) — як таймінги працюють у транспортному потоці
- 🧪 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади використання Timeline

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Використовуйте окремий Timeline для кожного потоку** (відео/аудіо/субтитри) — це спрощує синхронізацію.
> 2. **Викликайте `Pop()` регулярно** — інакше `segs[]` буде рости без обмежень.
> 3. **Логувайте великі розриви (`diff > 1s`)** — це допомагає виявляти проблеми з мережею або камерою.
> 4. **Тестуйте `Push()` з перекриттями** — переконайтеся, що корекція `tm -= diff` працює як очікується.
> 5. **Додайте метрики для `headtm`** — відставання `headtm` від реального часу може вказувати на проблеми з обробкою.

Потрібен приклад інтеграції `Timeline` з вашим `segmentAssembler` для точної синхронізації відео/аудіо/субтитрів перед створенням HLS-сегментів? Готовий допомогти! 🚀