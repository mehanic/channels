# 📦 Глибокий розбір: `pio` — Scatter-Gather (iovec) утиліти для векторів байтів

Цей файл містить **низькорівневі функції для роботи з векторами буферів** (`[][]byte`), що реалізують патерн **scatter-gather I/O** (також відомий як `iovec` у Unix). Вони дозволяють обробляти фрагментовані дані без проміжного копіювання, що критично важливо для високопродуктивних мережевих та медіа-пайплайнів.

---

## 🗺️ Архітектурна схема `pio`

```
┌────────────────────────────────────────┐
│ 📦 pio — Vector Buffer Utilities       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові функції:                    │
│  • VecLen() — сумарна довжина          │
│  • VecSliceTo() — безкопійний зріз у prealloc out│
│  • VecSlice() — зручна обгортка        │
│                                         │
│  🔄 Патерн: Scatter-Gather I/O          │
│  [][]byte → flatten → Read/Write       │
│  (без виділення нового []byte)          │
│                                         │
│  📡 Використання у vdk:                 │
│  • ts.Muxer.datav — збірка TS пакетів  │
│  • rtspv2.RTPDemuxer — фрагментація    │
│  • Ефективний запис у io.Writer/net.Conn│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. VecLen() — розрахунок сумарної довжини

```go
func VecLen(vec [][]byte) (n int) {
    for _, b := range vec {
        n += len(b)
    }
    return
}
```

### Призначення:
Швидко обчислює загальну кількість байт у всіх фрагментах вектора. Використовується для:
- Розрахунку `Content-Length` у HTTP/RTSP
- Перевірки чи вистачить місця у буфері
- Розподілу даних по TS/RTP пакетам

### ✅ Ваш use-case: валідація перед записом
```go
// ValidatePacketSize — перевірка чи пакет не перевищує MTU
func ValidatePacketSize(vec [][]byte, maxMTU int) error {
    total := pio.VecLen(vec)
    if total > maxMTU {
        return fmt.Errorf("packet size %d exceeds MTU %d", total, maxMTU)
    }
    return nil
}
```

---

## 🔑 2. VecSliceTo() — безкопійний зріз фрагментованих даних

### 🔧 Логіка роботи (розшифровка):

```go
func VecSliceTo(in [][]byte, out [][]byte, s int, e int) (n int)
```

| Параметр | Тип | Призначення |
|----------|-----|-------------|
| `in` | `[][]byte` | Вхідний вектор буферів |
| `out` | `[][]byte` | Попередньо виділений масив для результатів |
| `s` | `int` | Початкове зміщення (від 0) |
| `e` | `int` | Кінцеве зміщення. `-1` = до кінця |
| `n` | `int` | Кількість записаних слайсів у `out` |

### 🔄 Алгоритм покроково:

```
1. Корекція s: if s < 0 → s = 0
2. Перевірка валідності: if e >= 0 && e < s → panic
3. Пропуск байт до s:
   • Ітеруємо in[i], віднімаємо від s та e
   • Зсуваємо off всередині буфера
4. Копіювання фрагментів до e == 0:
   • out[n] = in[i][off : off+read]  ← БЕЗ КОПІЮВАННЯ ДАНИХ!
   • Зменшуємо e на прочитану кількість
   • Переходимо до наступного буфера якщо поточний вичерпано
5. Повертаємо n (кількість заповнених елементів out)
```

### 🔍 Приклад роботи:
```
in = [ []byte{0x00,0x01}, []byte{0x02,0x03,0x04}, []byte{0x05} ]
VecSliceTo(in, out, 1, 4)  // взяти байти з індексу 1 до 4 (не включаючи)

Результат out:
  [0] = in[0][1:2] → {0x01}
  [1] = in[1][0:2] → {0x02,0x03}
  n = 2
```

### ⚠️ Критичні моменти:
- **Паніки при виході за межі**: `s > total` або `e > total` викликає `panic`. Це швидше, ніж повертати помилку, але вимагає валідації на стороні клієнта.
- `e = -1` → копіює все від `s` до кінця вектора.
- **Zero-copy**: `out` містить *посилання* на оригінальні байти, а не копії. Зміна `out[n]` змінить `in`.

### ✅ Ваш use-case: витягування RTP payload з заголовком
```go
// ExtractRTPPayload — отримання payload без копіювання
func ExtractRTPPayload(rtpPacket [][]byte, headerSize int) [][]byte {
    totalLen := pio.VecLen(rtpPacket)
    if totalLen <= headerSize {
        return nil
    }
    
    // out має бути достатньо великим (max = len(rtpPacket))
    out := make([][]byte, len(rtpPacket))
    n := pio.VecSliceTo(rtpPacket, out, headerSize, -1)
    return out[:n]
}
```

---

## 🔑 3. VecSlice() — зручна обгортка з аллокацією

```go
func VecSlice(in [][]byte, s int, e int) (out [][]byte) {
    out = make([][]byte, len(in))  // ⚠️ Може бути надмірним
    n := VecSliceTo(in, out, s, e)
    out = out[:n]
    return
}
```

### ⚠️ Проблема надмірної аллокації:
Якщо `in` має 100 буферів, а `VecSlice` повертає лише 2, `make([][]byte, len(in))` виділить пам'ять під 100 вказівників. Для high-load це може створювати тиск на GC.

### ✅ Оптимізована версія:
```go
// VecSliceOptimized — з розумною аллокацією
func VecSliceOptimized(in [][]byte, s int, e int) [][]byte {
    // Оцінка верхньої межі: не більше len(in), але спробуємо мінімізувати
    cap := len(in)
    if e >= 0 && e-s < cap {
        cap = e - s + 1  // груба оцінка
    }
    out := make([][]byte, 0, cap)
    
    // Використовуємо VecSliceTo вручну або реалізуємо аналог
    // Або просто залишити оригінал, якщо cap не критичний
    return pio.VecSlice(in, s, e)
}
```

> 💡 **Рекомендація**: У production використовуйте `VecSliceTo` з пулом буферів (`sync.Pool`), щоб уникнути аллокацій.

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### Сценарій 1: Збірка TS пакету з фрагментів без копіювання

```go
// BuildTSPacket — ефективна збірка TS пакету
func BuildTSPacket(header []byte, adaptation []byte, payload [][]byte) ([][]byte, error) {
    // Вектор: [header, adaptation, payload...]
    packet := make([][]byte, 0, 2+len(payload))
    packet = append(packet, header)
    if len(adaptation) > 0 {
        packet = append(packet, adaptation)
    }
    packet = append(packet, payload...)
    
    // Перевірка розміру (188 байт для TS)
    total := pio.VecLen(packet)
    if total > 188 {
        return nil, fmt.Errorf("packet size %d > 188", total)
    }
    
    // Додавання padding якщо потрібно
    if total < 188 {
        padding := make([]byte, 188-total)
        packet = append(packet, padding)
    }
    
    return packet, nil
}

// Запис у мережу без копіювання:
func WriteTSPacket(conn net.Conn, packet [][]byte) error {
    // net.Buffers (Go 1.10+) реалізує io.Reader для [][]byte
    // Або пишемо вручну:
    for _, buf := range packet {
        if _, err := conn.Write(buf); err != nil {
            return err
        }
    }
    return nil
}
```

### Сценарій 2: Обробка фрагментованих RTP пакетів

```go
// ProcessFragmentedRTP — збірка FU-A фрагментів
func ProcessFragmentedRTP(fragments [][]byte, startOff, endOff int) ([]byte, error) {
    // Витягуємо потрібний діапазон
    out := make([][]byte, len(fragments))
    n := pio.VecSliceTo(fragments, out, startOff, endOff)
    sliced := out[:n]
    
    // Об'єднуємо тільки якщо потрібно (напр. для парсингу)
    total := pio.VecLen(sliced)
    result := make([]byte, total)
    offset := 0
    for _, buf := range sliced {
        copy(result[offset:], buf)
        offset += len(buf)
    }
    return result, nil
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Паніка `VecSlice start > end`** | `e < s` при виклику | Валідуйте вхідні зміщення: `if e >= 0 && e < s { e = -1 }` |
| **Паніка `end out of range`** | `e` перевищує загальну довжину | Використовуйте `pio.VecLen()` для перевірки перед викликом |
| **Надмірна аллокація у `VecSlice`** | Високе навантаження на GC | Використовуйте `VecSliceTo` з `sync.Pool` для `out` |
| **Race condition при shared slices** | Зміна даних в одному місці ламає інше | Пам'ятайте: `VecSlice` zero-copy! Копіюйте якщо потрібно мутація |
| **Повільний `VecLen` у великих векторах** | O(N) сума для тисяч буферів | Кешуйте сумарну довжину при додаванні/видаленні буферів |

---

## ⚡ Оптимізації для high-performance I/O

### 1. Пул буферів для `VecSliceTo`:

```go
var vecSlicePool = sync.Pool{
    New: func() interface{} {
        // Зазвичай фрагментація <= 16 буферів
        buf := make([][]byte, 0, 16)
        return &buf
    },
}

func GetSliceBuffer() *[][]byte { return vecSlicePool.Get().(*[][]byte) }
func PutSliceBuffer(b *[][]byte) {
    *b = (*b)[:0]  // скидання без звільнення пам'яті
    vecSlicePool.Put(b)
}

// Використання:
outBuf := GetSliceBuffer()
n := pio.VecSliceTo(in, *outBuf, start, end)
result := (*outBuf)[:n]
// ... обробка ...
PutSliceBuffer(outBuf)
```

### 2. Кешування сумарної довжини:

```go
type CachedVec struct {
    Vec  [][]byte
    Len  int  // кешована довжина
}

func (v *CachedVec) Append(buf []byte) {
    v.Vec = append(v.Vec, buf)
    v.Len += len(buf)
}

func (v *CachedVec) Clear() {
    v.Vec = v.Vec[:0]
    v.Len = 0
}
```

### 3. Інтеграція з `net.Buffers` (сучасний Go стандарт):

```go
// Go 1.10+ має net.Buffers, який реалізує io.Reader/io.Writer
func WriteWithNetBuffers(conn net.Conn, vec [][]byte) (int64, error) {
    buffers := net.Buffers(vec)
    return buffers.WriteTo(conn)
}

func ReadWithNetBuffers(conn net.Conn, maxBytes int) ([][]byte, error) {
    // net.Buffers не реалізує Reader напряму, але можна використати io.LimitReader
    // або залишити pio для складних операцій зрізу
}
```

> 💡 **Порада**: `net.Buffers` оптимізований на рівні runtime та підтримує `sendmsg`/`writev` syscall. Використовуйте його для простого запису, а `pio.VecSlice*` — для складної фрагментації/зрізу.

---

## 📋 Чек-лист безпечного використання

```go
// ✅ 1. Валідація меж перед викликом
total := pio.VecLen(in)
if start >= total || (end >= 0 && end > total) {
    return nil, fmt.Errorf("slice range out of bounds")
}

// ✅ 2. Використання VecSliceTo з пулом
outBuf := GetSliceBuffer()
n := pio.VecSliceTo(in, *outBuf, start, end)
defer PutSliceBuffer(outBuf)

// ✅ 3. Zero-copy усвідомлення
// Змінення out[n][i] змінить in! Копіюйте якщо потрібна ізоляція:
isolated := make([]byte, len(out[n]))
copy(isolated, out[n])

// ✅ 4. Обробка e = -1
if end < 0 {
    end = total  // явна конвертація для читабельності
}

// ✅ 5. Метрики фрагментації
metrics.BufferFragmentCount.Observe(float64(len(in)))
metrics.BufferTotalBytes.Observe(float64(pio.VecLen(in)))
```

---

## 🔗 Корисні посилання

- 💻 [Go net.Buffers Documentation](https://pkg.go.dev/net#Buffers) — сучасний стандарт scatter-gather
- 📄 [Unix iovec (readv/writev)](https://man7.org/linux/man-pages/man2/readv.2.html) — системні виклики
- 📄 [Scatter-Gather I/O Pattern](https://en.wikipedia.org/wiki/Vectored_I/O) — теоретична основа
- 🧪 [Go sync.Pool Best Practices](https://go.dev/blog/pool) — ефективне управління пам'яттю

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Використовуйте `VecSliceTo` з `sync.Pool`** — уникнення аллокацій критично для real-time медіа.
> 2. **Пам'ятайте про zero-copy** — фрагменти посилаються на оригінальні дані; мутація небезпечна.
> 3. **Валідуйте межі перед викликом** — паніки у production неприпустимі; додайте `SafeVecSlice` обгортку.
> 4. **Віддавайте перевагу `net.Buffers` для запису** — воно використовує оптимізовані syscalls (`writev`).
> 5. **Кешуйте `VecLen`** — якщо вектор змінюється рідко, зберігайте суму в окремому полі.

Потрібен приклад реалізації `SafeVecSlice` з поверненням помилок замість панік, або інтеграція `net.Buffers` у ваш `ts.Muxer` для оптимізованого запису? Готовий допомогти! 🚀