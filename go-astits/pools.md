# Глибоке роз'яснення: `bytesPool` — оптимізація пам'яті в astits

Ця реалізація — **кастомний пул байтових буферів** на основі `sync.Pool`, призначений для мінімізації навантаження на Garbage Collector при парсингі високобітрейтних MPEG-TS потоків.

---

## 🎯 Навіщо це потрібно?

```
┌─────────────────────────────────────────┐
│ Проблема:                               │
│ • Кожен TS-пакет = 188 байт             │
│ • 10 Мбіт/с потік ≈ 6600 пакетів/сек    │
│ • Кожен пакет → новий []byte → GC тиск  │
│                                         │
│ Наслідки без пулу:                      │
│ • Часті стоп-світи GC (10-50 мс паузи)  │
│ • Втрата пакетів через затримки         │
│ • Нестабільна латентність у HLS         │
│                                         │
│ Рішення: bytesPool                      │
│ • Повторне використання буферів         │
│ • Менше алокацій → менше GC → стабільність│
└─────────────────────────────────────────┘
```

**Математика економії:**
```
Без пулу (10 Мбіт/с, 1 хвилина):
• 6600 пакетів/сек × 60 сек = 396_000 алокацій
• Кожен []byte = ~24B header + 188B data = 212B
• Разом: ~84 MB тимчасової пам'яті/хв → тиск на GC

З bytesPool:
• Пул зберігає ~100-500 буферів у ротации
• Алокації тільки при розширенні (>1024B)
• Економія: 90-95% алокацій, стабільний GC
```

---

## 🔧 Архітектура реалізації

### 1. Структури

```go
// bytesPoolItem — "контейнер" для буфера
type bytesPoolItem struct {
    s []byte  // сам байтовий слайс
}

// bytesPooler — обгортка над sync.Pool
type bytesPooler struct {
    sp sync.Pool  // стандартний пул з runtime
}

// Глобальний екземпляр (singleton)
var bytesPool = &bytesPooler{
    sp: sync.Pool{
        New: func() interface{} {
            return &bytesPoolItem{
                s: make([]byte, 0, 1024),  // ⚠️ ключова оптимізація!
            }
        },
    },
}
```

### 2. Чому `cap=1024`, а не 188?

```
┌─────────────────────────────────────────┐
│ make([]byte, 0, 1024) — свідомий вибір:│
│                                         │
│ • 188B — розмір одного TS-пакету       │
│ • Але часто потрібно збирати:           │
│   - Цілі PES-пакети (до 64KB)           │
│   - PSI/SI таблиці (PAT/PMT ~1KB)       │
│   - Адаптаційні поля з кількох пакетів  │
│                                         │
│ • Якщо cap=188: при потребі 200B →     │
│   runtime.growslice → нова алокація!   │
│                                         │
│ • cap=1024 покриває ~95% випадків      │
│   без додаткових алокацій               │
└─────────────────────────────────────────┘
```

### 3. Метод `get(size int)` — розумне управління буфером

```go
func (bp *bytesPooler) get(size int) (payload *bytesPoolItem) {
    // 1. Отримати предмет з пулу (або створити новий через New())
    payload = bp.sp.Get().(*bytesPoolItem)
    
    // 2. Налаштувати довжину слайсу
    if cap(payload.s) >= size {
        // ✅ Є місце: просто змінити length (O(1), без алокацій)
        payload.s = payload.s[:size]
    } else {
        // ⚠️ Потрібно більше місця: розширити
        n := size - cap(payload.s)
        payload.s = append(payload.s[:cap(payload.s)], make([]byte, n)...)[:size]
        // ⚠️ Це викличе алокацію, але рідко
    }
    return
}
```

**Візуалізація:**
```
Пул повертає: &bytesPoolItem{s: []byte(len=0, cap=1024)}

Виклик get(188):
• cap(1024) >= 188 → true
• payload.s = payload.s[:188]
• Результат: []byte(len=188, cap=1024) — без алокацій!

Виклик get(2000):
• cap(1024) >= 2000 → false
• append + make для додаткових 976 байт
• Результат: новий масив ~2048B (runtime rounding)
```

### 4. Метод `put(payload *bytesPoolItem)` — повернення у пул

```go
func (bp *bytesPooler) put(payload *bytesPoolItem) {
    bp.sp.Put(payload)  // повертаємо "контейнер", не слайс!
}
```

> ⚠️ **Критичне правило**: Після `put()` **не використовуйте** `payload.s`! Пул може віддати цей самий буфер іншій горутині.

---

## 🔄 Патерн використання у astits

```go
// Типовий цикл парсингу даних:
func parseData(r io.Reader) (*DemuxerData, error) {
    // 1. Взяти буфер з пулу
    buf := bytesPool.get(expectedSize)
    
    // 2. ⚠️ Важливо: гарантувати, що при помилці буфер повернеться
    defer bytesPool.put(buf)
    
    // 3. Читати дані у buf.s
    n, err := io.ReadFull(r, buf.s)
    if err != nil {
        return nil, err  // defer автоматично поверне буфер
    }
    
    // 4. Якщо дані потрібно зберегти поза функцією:
    //    — скопіювати, а не передати посилання!
    result := make([]byte, n)
    copy(result, buf.s)  // ✅ безпечна копія
    
    return &DemuxerData{Payload: result}, nil
    // defer поверне buf у пул для повторного використання
}
```

---

## 🛠️ Інтеграція з вашим CCTV HLS пайплайном

### ✅ 1. Оптимізація `segmentAssembler` — збір PES без алокацій

```go
// У вашому segmentAssembler.go:
type AssemblerBuffer struct {
    pool     *bytesPooler  // або використовуйте глобальний bytesPool
    videoBuf *bytesPoolItem
    audioBuf *bytesPoolItem
}

func (ab *AssemblerBuffer) AppendVideoChunk(data []byte) {
    // Поточна реалізація (може аллокувати):
    // ab.videoData = append(ab.videoData, data...)
    
    // Оптимізована версія з пулом:
    needed := len(ab.videoData) + len(data)
    
    if ab.videoBuf == nil || cap(ab.videoBuf.s) < needed {
        // Повернути старий буфер у пул, якщо є
        if ab.videoBuf != nil {
            bytesPool.put(ab.videoBuf)
        }
        // Взяти новий, більший
        ab.videoBuf = bytesPool.get(needed)
    }
    
    // Скопіювати дані у пере-використаний буфер
    copy(ab.videoBuf.s[len(ab.videoData):], data)
    ab.videoData = ab.videoBuf.s[:needed]  // оновити length
}

func (ab *AssemblerBuffer) Finalize() ([]byte, error) {
    // ⚠️ Перед поверненням даних — обов'язково скопіювати!
    result := make([]byte, len(ab.videoData))
    copy(result, ab.videoData)
    
    // Повернути буфери у пул
    if ab.videoBuf != nil {
        bytesPool.put(ab.videoBuf)
        ab.videoBuf = nil
    }
    if ab.audioBuf != nil {
        bytesPool.put(ab.audioBuf)
        ab.audioBuf = nil
    }
    
    return result, nil
}
```

### ✅ 2. Thread-safe доступ до глобального пулу

```go
// bytesPool — глобальна змінна, але sync.Pool вже thread-safe!
// Тому можна використовувати напряму з будь-якої горутини:

func processTSChunk(chunk []byte, channelID string) {
    // Безпека: sync.Pool гарантує конкурентний доступ
    buf := bytesPool.get(len(chunk))
    defer bytesPool.put(buf)
    
    copy(buf.s, chunk)
    
    // Обробка...
    result := process(buf.s)
    
    // Якщо result потрібно зберегти — скопіювати
    storeResult(channelID, append([]byte(nil), result...))
}
```

### ✅ 3. Адаптація пулу під ваші розміри сегментів

```go
// Якщо ваші сегменти завжди 4 секунди відео + 2×4с аудіо:
// Оцініть типовий розмір та налаштуйте cap:

const (
    // Приблизні розміри для H.264 + AAC:
    VideoSegment4s = 2 * 1024 * 1024  // ~2MB
    AudioChunk4s   = 256 * 1024        // ~256KB
)

// Створити спеціалізований пул для великих буферів:
var segmentPool = &bytesPooler{
    sp: sync.Pool{
        New: func() interface{} {
            return &bytesPoolItem{
                s: make([]byte, 0, VideoSegment4s),  // ⚠️ великий cap!
            }
        },
    },
}

// Використання у segmentFinalizer:
func createTSSegment(video, audio []byte) ([]byte, error) {
    buf := segmentPool.get(len(video) + len(audio) + 4096)  // + запас для заголовків
    defer segmentPool.put(buf)
    
    // Збирати TS...
    return assembleTS(buf.s, video, audio)
}
```

> ⚠️ **Попередження**: Великий `cap` = більше пам'яті на один буфер. Балансуйте між економій на алокаціях та загальним споживанням пам'яті.

---

## 🧪 Benchmark: вплив пулу на продуктивність

```go
// benchmark_test.go
func BenchmarkTSParser_WithoutPool(b *testing.B) {
    data := generateTestTS(10 * 1024 * 1024)  // 10MB тестових даних
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        parseWithoutPool(bytes.NewReader(data))
    }
}

func BenchmarkTSParser_WithPool(b *testing.B) {
    data := generateTestTS(10 * 1024 * 1024)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        parseWithPool(bytes.NewReader(data))  // використовує bytesPool
    }
}
```

**Очікувані результати (на 4-ядерному CPU):**
```
BenchmarkTSParser_WithoutPool-4    100    12500000 ns/op    85 MB alloc/op
BenchmarkTSParser_WithPool-4       150     8200000 ns/op    12 MB alloc/op

• Швидкість: +34% (менше пауз GC)
• Пам'ять: -86% алокацій
• P99 латентність: стабільніша на 2-3×
```

Запуск: `go test -bench=. -benchmem -memprofile=mem.prof`

---

## 🐛 Поширені проблеми та рішення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| Використання після `put()` | Псевдо-дані, корупція пам'яті, race condition | Завжди `defer put()` на початку функції; ніколи не зберігати посилання на `buf.s` поза областю видимості |
| Неправильний `cap` → часті алокації | Профайлер показує багато `runtime.growslice` | Збільшити початковий `cap` у `New()` або додати логіку адаптивного росту |
| Пул "забруднений" великими буферами | Споживання пам'яті росте, хоча потік малий | Додати `maxCap` перевірку у `put()`: якщо `cap > threshold`, не повертати у пул |
| Конкурентний доступ до одного буфера | Race detector скаржиться | Пам'ятати: `sync.Pool` thread-safe, але **окремий `bytesPoolItem` — ні!** Не передавати один і той самий предмет між горутинами |
| Витік пам'яті при паніці | `put()` не викликається, буфер не повертається | Використовувати `defer put()` **одразу після `get()`**, до будь-якої логіки |

### Приклад безпечного патерну:

```go
func safeParse(r io.Reader) ([]byte, error) {
    buf := bytesPool.get(2048)
    defer bytesPool.put(buf)  // ✅ гарантоване повернення навіть при паніці
    
    // Вся логіка нижче...
    n, err := io.ReadFull(r, buf.s)
    if err != nil {
        return nil, err  // defer спрацює
    }
    
    // Якщо потрібно повернути дані — копіювати!
    result := make([]byte, n)
    copy(result, buf.s)
    return result, nil
}
```

---

## 📦 Адаптація під вашу архітектуру

### 🔹 Channel-aware пули (ізоляція за каналом)

```go
// У багатоканальному сервері — окремий пул на канал для кращої локальності:
type ChannelBufferPool struct {
    mu    sync.RWMutex
    pools map[string]*bytesPooler
}

func (cbp *ChannelBufferPool) Get(channelID string) *bytesPooler {
    cbp.mu.RLock()
    pool, ok := cbp.pools[channelID]
    cbp.mu.RUnlock()
    
    if ok { return pool }
    
    cbp.mu.Lock()
    defer cbp.mu.Unlock()
    
    if pool, ok = cbp.pools[channelID]; ok {
        return pool
    }
    
    pool = &bytesPooler{
        sp: sync.Pool{
            New: func() interface{} {
                return &bytesPoolItem{
                    s: make([]byte, 0, 1024),
                }
            },
        },
    }
    cbp.pools[channelID] = pool
    return pool
}

// Використання:
func processChannelData(channelID string, data []byte) {
    pool := channelPools.Get(channelID)
    buf := pool.get(len(data))
    defer pool.put(buf)
    
    copy(buf.s, data)
    // обробка...
}
```

### 🔹 Моніторинг ефективності пулу

```go
// monitoring.Monitor — метрики для bytesPool:
type PoolMetrics struct {
    PoolGets    *prometheus.CounterVec  // скільки разів брали з пулу
    PoolPuts    *prometheus.CounterVec  // скільки разів повертали
    PoolAllocs  *prometheus.CounterVec  // скільки разів довелося аллокувати новий
    PoolCapHits *prometheus.CounterVec  // скільки разів cap >= size (без алокацій)
}

// У bytesPooler додати лічильники:
type bytesPooler struct {
    sp sync.Pool
    metrics *PoolMetrics  // опціонально
}

func (bp *bytesPooler) get(size int) *bytesPoolItem {
    if bp.metrics != nil {
        bp.metrics.PoolGets.WithLabelValues().Inc()
    }
    
    payload := bp.sp.Get().(*bytesPoolItem)
    
    if bp.metrics != nil {
        if cap(payload.s) >= size {
            bp.metrics.PoolCapHits.WithLabelValues().Inc()
        } else {
            bp.metrics.PoolAllocs.WithLabelValues().Inc()
        }
    }
    
    // ... решта логіки
    return payload
}

func (bp *bytesPooler) put(payload *bytesPoolItem) {
    if bp.metrics != nil {
        bp.metrics.PoolPuts.WithLabelValues().Inc()
    }
    bp.sp.Put(payload)
}
```

---

## 📊 Коли НЕ використовувати bytesPool

```
┌─────────────────────────────────────────┐
│ bytesPool НЕ виправданий, якщо:         │
│                                         │
│ • Низький потік (< 100 пакетів/сек)    │
│   → витрати на синхронізацію > вигода  │
│                                         │
│ • Дуже різні розміри буферів            │
│   → пул "забруднюється", низький hit-rate│
│                                         │
│ • Короткоживучі об'єкти, які не         │
│   передаються між функціями             │
│   → compiler escape analysis оптимізує  │
│                                         │
│ • Обмежена пам'ять (embedded)           │
│   → пул тримає буфери "назапас", може  │
│     вичерпати доступну пам'ять          │
└─────────────────────────────────────────┘
```

**Правило великого пальця:**  
Використовуйте пул, якщо профайлер показує >1000 алокацій/сек для однотипних буферів.

---

## 📦 Швидкий чек-лист для вашого коду

```go
// ✅ Правильно:
func process() {
    buf := bytesPool.get(1024)
    defer bytesPool.put(buf)  // завжди defer одразу!
    
    // ... робота з buf.s ...
    
    // Якщо потрібно повернути дані назовні:
    result := make([]byte, len(buf.s))
    copy(result, buf.s)  // копія, не посилання!
    return result
}

// ❌ Неправильно:
func process() {
    buf := bytesPool.get(1024)
    // ... помилка тут → put() не викличеться → витік!
    return buf.s  // ⚠️ повертаємо посилання на буфер з пулу!
}

// ❌ Також неправильно:
func process() {
    buf := bytesPool.get(1024)
    bytesPool.put(buf)  // повернули завчасно
    // ... використовуємо buf.s після put() → race condition!
    return buf.s
}
```

---

## 📚 Корисні посилання

- [sync.Pool документація](https://pkg.go.dev/sync#Pool)
- [Go GC tuning guide](https://go.dev/doc/gc-guide)
- [astits parseData usage](https://github.com/asticode/go-astits/blob/master/data.go) (шукати `bytesPool.get`)

> 💡 **Ключова ідея**: `bytesPool` — це не просто оптимізація, а **інструмент стабільності** для real-time обробки. У вашому CCTV HLS пайплайні він допоможе уникнути мікро-пауз при зборі сегментів, що критично для підтримки низької латентності та синхронізації аудіо/відео.

Якщо потрібно — можу допомогти:
- 📈 Налаштувати Prometheus-метрики для моніторингу ефективності пулу
- 🧪 Написати benchmark для порівняння різних `cap` значень під ваші сегменти
- 🔄 Інтегрувати channel-aware пули у ваш `segmentAssembler` для кращої ізоляції

🛠️