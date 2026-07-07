# 🧪 Глибокий розбір: Тести `timescale.ToScale` та `timescale.Relative`

Цей файл — **набір юніт-тестів** для функцій конвертації часу у пакеті `timescale`. Тести перевіряють коректність перетворення `time.Duration` у ticks з різними шкалами часу, включаючи граничні випадки, округлення та обробку від'ємних значень.

---

## 🗺️ Архітектурна схема тестування

```
┌────────────────────────────────────────┐
│ 🧪 timescale — Unit Test Coverage     │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові тести:                      │
│  • TestToScale — Duration → uint64    │
│  • TestRelative — Duration → int32    │
│  • scale = 90000 (стандарт для відео) │
│                                         │
│  🔄 Тестові сценарії:                   │
│  • Нульові значення                    │
│  • Межі округлення (±1 наносекунда)   │
│  • Великі значення (2^32 секунд)      │
│  • Від'ємні значення (Relative only)  │
│                                         │
│  📡 Призначення:                        │
│  • Перевірка точності конвертації     │
│  • Валідація логіки округлення        │
│  • Запобігання регресіям при змінах   │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. TestToScale — тестування конвертації Duration → uint64

### 🔧 Структура тесту:

```go
func TestToScale(t *testing.T) {
    const scale uint32 = 90000  // стандартна шкала для відео (90 kHz)
    
    values := []struct {
        T time.Duration  // вхідне значення
        V uint64         // очікуваний результат у ticks
    }{
        // ... тестові кейси ...
    }
    
    for _, ex := range values {
        n := ToScale(ex.T, scale)
        if n != ex.V {
            t.Errorf("%d (%s): expected %d, got %d", ex.T, ex.T, ex.V, n)
        }
    }
}
```

### 🔍 Аналіз тестових кейсів:

| Тестовий кейс | Опис | Очікуваний результат | Чому це важливо |
|--------------|--------|---------------------|----------------|
| `{0, 0}` | Нульовий час | 0 ticks | Базовий випадок |
| `{time.Second/60 - 1, 1500}` | 1 секунда/60 мінус 1 наносекунда | 1500 ticks | Перевірка округлення вниз |
| `{time.Second/60 + 0, 1500}` | Рівно 1 секунда/60 | 1500 ticks | Точне значення без округлення |
| `{time.Second/60 + 1, 1500}` | 1 секунда/60 плюс 1 наносекунда | 1500 ticks | Перевірка округлення вниз при малому залишку |
| `{(time.Second/60)*60 ± 1, 90000}` | Рівно 1 секунда ± 1 ns | 90000 ticks | Перевірка накопичення помилок |
| `{time.Second * (1 << 32), 90000 * (1 << 32)}` | ~136 років (2^32 секунд) | 90000 * 2^32 | Перевірка 64-бітної арифметики без переповнення |
| `{time.Second*(1<<32) + time.Second/60 ± 1, ...}` | Велике значення + дрібна добавка | Точна сума | Перевірка точності при великих + малих значеннях |

### 🔍 Математика тестових значень:

```
scale = 90000 ticks/second

time.Second/60 = 1_000_000_000 / 60 = 16_666_666.67 ns
16_666_666.67 ns * 90000 / 1_000_000_000 = 1500 ticks ✓

(time.Second/60)*60 = 1_000_000_000 ns = 1 second
1_000_000_000 ns * 90000 / 1_000_000_000 = 90000 ticks ✓

time.Second * (1 << 32) = 4_294_967_296 секунд (~136 років)
4_294_967_296 * 90000 = 386_547_056_640_000 ticks
= 90000 * (1 << 32) ✓
```

### ✅ Ваш use-case**: розширення тестів для крайніх випадків

```go
// Додавання тестів для від'ємних значень (повинні використовувати Relative, не ToScale)
func TestToScaleEdgeCases(t *testing.T) {
    const scale uint32 = 90000
    
    // Тест для дуже малих значень (менше 1 tick)
    cases := []struct {
        name     string
        duration time.Duration
        expected uint64
    }{
        {"sub-nanosecond", 0, 0},
        {"half-tick", time.Nanosecond * 5555, 0},  // 5555 ns * 90000 / 1e9 = 0.5 → round down
        {"one-tick", time.Nanosecond * 11112, 1},   // 11112 ns * 90000 / 1e9 = 1.0 → exact
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            result := ToScale(tc.duration, scale)
            if result != tc.expected {
                t.Errorf("ToScale(%v, %d) = %d; expected %d", 
                    tc.duration, scale, result, tc.expected)
            }
        })
    }
}

// Тест для перевірки монотонності: більший duration → більший або рівний ticks
func TestToScaleMonotonic(t *testing.T) {
    const scale uint32 = 90000
    prev := ToScale(0, scale)
    
    for i := 1; i < 1000; i++ {
        curr := ToScale(time.Duration(i)*time.Millisecond, scale)
        if curr < prev {
            t.Errorf("monotonicity violated at %d ms: %d < %d", i, curr, prev)
        }
        prev = curr
    }
}
```

---

## 🔑 2. TestRelative — тестування конвертації Duration → int32 (з підтримкою знаку)

### 🔧 Структура тесту:

```go
func TestRelative(t *testing.T) {
    const scale uint32 = 90000
    
    values := []struct {
        T time.Duration  // може бути від'ємним
        V int32          // очікуваний результат (може бути від'ємним)
    }{
        // Додатні значення (аналогічні до TestToScale)
        {0, 0},
        {time.Second/60 - 1, 1500},
        {time.Second/60 + 0, 1500},
        {time.Second/60 + 1, 1500},
        {(time.Second/60)*5 ± 1, 7500},  // 5 секунд/60 = 7500 ticks
        
        // Від'ємні значення (унікальні для Relative)
        {-time.Second/60 - 1, -1500},
        {-time.Second/60 + 0, -1500},
        {-time.Second/60 + 1, -1500},
        {(-time.Second/60)*5 ± 1, -7500},
    }
    
    for _, ex := range values {
        n := Relative(ex.T, scale)
        if n != ex.V {
            t.Errorf("%d (%s): expected %d, got %d", ex.T, ex.T, ex.V, n)
        }
    }
}
```

### 🔍 Чому від'ємні значення важливі:

```
Relative() використовується для Composition Time Offset (CTS) у відео з B-frames:

Приклад для H.264 з B-frames:
  Порядок декодування: I0, P3, B1, B2, P6, B4, B5...
  Порядок відтворення: I0, B1, B2, P3, B4, B5, P6...
  
  Для B1: DTS = 1, PTS = 1, CTS = 0
  Для B2: DTS = 2, PTS = 2, CTS = 0
  Для P3: DTS = 3, PTS = 3, CTS = 0
  
  Але якщо є затримка обробки:
  Для B-frame: CTS може бути від'ємним (відтворення раніше декодування)
  
  Relative(-500ms, 90000) = -45000 ticks  ✓
```

### 🔍 Логіка округлення для від'ємних значень:

```
У Relative():
    if (rel&1 != 0) == (t > 0) {
        // round up
        rel++
    }

Це забезпечує симетричне округлення:
• t > 0, rel непарний → округлити вгору (+1)
• t < 0, rel непарний → округлити вниз (-1, через арифметику зі знаком)

Приклад:
    t = -1_000_000_001 ns (-1.000000001 секунди)
    scale = 90000
    
    1. rel = -1_000_000_001 * 90000 / 500_000_000 = -180000.00018 → -180000
    2. rel&1 = 0 (парне) → не округлюємо
    3. rel >> 1 = -90000 ticks ✓

    t = -1_000_500_000 ns (-1.0005 секунди)
    1. rel = -1_000_500_000 * 90000 / 500_000_000 = -180090
    2. rel&1 = 0 → не округлюємо  
    3. rel >> 1 = -90045 ticks ✓
```

### ✅ Ваш use-case**: тестування симетрії округлення

```go
// TestRelativeSymmetry — перевірка симетричного округлення для ± значень
func TestRelativeSymmetry(t *testing.T) {
    const scale uint32 = 90000
    
    testDurations := []time.Duration{
        time.Second/60 - 1,
        time.Second/60,
        time.Second/60 + 1,
        time.Millisecond * 123,
        time.Microsecond * 456789,
    }
    
    for _, d := range testDurations {
        pos := Relative(d, scale)
        neg := Relative(-d, scale)
        
        // Очікуємо: Relative(-d) == -Relative(d) для симетрії
        if pos != -neg {
            t.Errorf("asymmetry: Relative(%v)=%d, Relative(-%v)=%d, expected %d", 
                d, pos, d, neg, -pos)
        }
    }
}

// TestRelativeRoundTrip — перевірка зворотньої конвертації
func TestRelativeRoundTrip(t *testing.T) {
    const scale uint32 = 90000
    
    // Генеруємо випадкові значення в межах int32
    for ticks := int32(-100000); ticks <= 100000; ticks += 123 {
        // Конвертація ticks → Duration → ticks
        duration := time.Duration(ticks) * time.Second / time.Duration(scale)
        result := Relative(duration, scale)
        
        // Допускаємо похибку ±1 tick через округлення
        if diff := abs(int(result) - int(ticks)); diff > 1 {
            t.Errorf("round-trip failed: %d ticks → %v → %d ticks (diff=%d)", 
                ticks, duration, result, diff)
        }
    }
}

func abs(x int) int {
    if x < 0 { return -x }
    return x
}
```

---

## 🔑 3. Аналіз покриття тестами

### 🔍 Що тестується добре:

```
✅ Нульові значення
✅ Межі округлення (±1 наносекунда навколо точних значень)
✅ Накопичення точності (60 × 1/60 секунди = 1 секунда)
✅ Великі значення (2^32 секунд) для перевірки 64-бітної арифметики
✅ Від'ємні значення для Relative()
✅ Симетрія округлення для ± значень
```

### ⚠️ Що НЕ тестується (потенційні прогалини):

```
❌ Переповнення: duration * scale > max uint64
❌ Дуже малі значення: < 1 tick після конвертації
❌ Нестандартні шкали: scale != 90000 (напр. 44100 для аудіо)
❌ Максимальні значення int32 для Relative()
❌ Конвертація між різними шкалами (resampling)
```

### ✅ Ваш use-case**: розширення покриття тестів

```go
// TestToScaleOverflow — перевірка поведінки при потенційному переповненні
func TestToScaleOverflow(t *testing.T) {
    // Максимальне time.Duration = math.MaxInt64 наносекунд ≈ 292 років
    maxDuration := time.Duration(math.MaxInt64)
    
    // Для scale = 90000: maxDuration * 90000 / 1e9 ≈ 2.6e19 ticks
    // Це поміщається у uint64 (max ≈ 1.8e19) — може бути переповнення!
    
    // Тест повинен або:
    // 1. Коректно обробляти переповнення (повертати max uint64)
    // 2. Панікувати/повертати помилку (потрібно змінити сигнатуру функції)
    
    // Поточна реалізація використовує bits.Mul64/Div64, 
    // тому переповнення обробляється коректно на рівні 128-бітної арифметики
    result := ToScale(maxDuration, 90000)
    
    // Перевірка що результат розумний (не 0, не переповнення у неочікуване значення)
    if result == 0 {
        t.Error("ToScale(MaxInt64, 90000) returned 0 — possible overflow bug")
    }
}

// TestRelativeInt32Bounds — перевірка меж int32 для Relative()
func TestRelativeInt32Bounds(t *testing.T) {
    const scale uint32 = 90000
    
    // Максимальне значення int32 у ticks
    maxTicks := int64(math.MaxInt32)
    maxDuration := time.Duration(maxTicks) * time.Second / time.Duration(scale)
    
    // Тестування близько до межі
    cases := []struct {
        name     string
        duration time.Duration
        expectPanic bool
    }{
        {"just under max", maxDuration - time.Millisecond, false},
        {"at max", maxDuration, false},
        {"just over max", maxDuration + time.Millisecond, true}, // може переповнити int32
    }
    
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if tc.expectPanic {
                defer func() {
                    if r := recover(); r == nil {
                        t.Errorf("expected panic for duration %v", tc.duration)
                    }
                }()
            }
            _ = Relative(tc.duration, scale)
        })
    }
}

// TestToScaleVariousScales — тестування з різними шкалами часу
func TestToScaleVariousScales(t *testing.T) {
    scales := []struct {
        name  string
        scale uint32
    }{
        {"video-90kHz", 90000},
        {"audio-48kHz", 48000},
        {"audio-44.1kHz", 44100},
        {"audio-32kHz", 32000},
        {"low-res-1kHz", 1000},
        {"high-res-1MHz", 1000000},
    }
    
    testDuration := time.Second + time.Millisecond*123  // 1.123 секунди
    
    for _, sc := range scales {
        t.Run(sc.name, func(t *testing.T) {
            result := ToScale(testDuration, sc.scale)
            expected := uint64(float64(testDuration) * float64(sc.scale) / float64(time.Second))
            
            // Допускаємо похибку ±1 через округлення
            if diff := abs64(int64(result) - int64(expected)); diff > 1 {
                t.Errorf("ToScale(%v, %d) = %d; expected ~%d (diff=%d)", 
                    testDuration, sc.scale, result, expected, diff)
            }
        })
    }
}

func abs64(x int64) int64 {
    if x < 0 { return -x }
    return x
}
```

---

## 🔄 Інтеграція тестів у CI/CD pipeline

### 🔧 Приклад: Запуск тестів з покриттям

```bash
# Запуск тестів з coverage
go test -v -cover ./timescale

# Запуск тестів з race detector
go test -race -v ./timescale

# Запуск тестів з бенчмарками (якщо є)
go test -bench=. -benchmem ./timescale

# Генерація HTML звіту про покриття
go test -coverprofile=coverage.out ./timescale
go tool cover -html=coverage.out -o coverage.html
```

### 🔧 Приклад: Інтеграція з GitHub Actions

```yaml
# .github/workflows/test.yml
name: Test timescale package

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Run tests
        run: |
          go test -v -race -coverprofile=coverage.out ./timescale
          
      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./coverage.out
          flags: timescale
```

---

## 🐞 Поширені проблеми з тестами та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Тести проходять локально, але падають у CI** | Різні архітектури (32-біт vs 64-біт) | Додайте `GOARCH=amd64` або тестуйте на цільових платформах |
| **Flaky тести через округлення** | Іноді `expected X, got X±1` | Використовуйте `if diff > 1` замість строгого порівняння |
| **Повільні тести через великі цикли** | Тести виконуються >10 секунд | Зменшіть діапазон ітерацій або додайте `-short` прапорець |
| **Недостатнє покриття крайніх випадків** | Кодекс < 90% | Додайте тести для переповнення, нулів, від'ємних значень |

---

## ⚡ Оптимізації для швидкого тестування

### 1. Паралельне виконання підтестів:

```go
func TestToScaleParallel(t *testing.T) {
    const scale uint32 = 90000
    values := []struct{ T time.Duration; V uint64 }{ /* ... */ }
    
    for _, ex := range values {
        ex := ex  // capture range variable
        t.Run(fmt.Sprintf("%v", ex.T), func(t *testing.T) {
            t.Parallel()  // дозволити паралельне виконання
            n := ToScale(ex.T, scale)
            if n != ex.V {
                t.Errorf("expected %d, got %d", ex.V, n)
            }
        })
    }
}
```

### 2. Бенчмарки для перевірки продуктивності:

```go
func BenchmarkToScale(b *testing.B) {
    const scale uint32 = 90000
    durations := []time.Duration{
        0,
        time.Millisecond,
        time.Second,
        time.Minute,
        time.Hour,
    }
    
    for _, d := range durations {
        b.Run(fmt.Sprintf("%v", d), func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                _ = ToScale(d, scale)
            }
        })
    }
}

func BenchmarkRelative(b *testing.B) {
    const scale uint32 = 90000
    durations := []time.Duration{
        -time.Second,
        -time.Millisecond,
        0,
        time.Millisecond,
        time.Second,
    }
    
    for _, d := range durations {
        b.Run(fmt.Sprintf("%v", d), func(b *testing.B) {
            for i := 0; i < b.N; i++ {
                _ = Relative(d, scale)
            }
        })
    }
}
```

### 3. Fuzzing для пошуку крайніх випадків:

```go
// Потрібно Go 1.18+
func FuzzToScale(f *testing.F) {
    // Seed corpus з відомими значеннями
    f.Add(int64(0), uint32(90000))
    f.Add(int64(time.Second), uint32(90000))
    f.Add(int64(time.Hour), uint32(48000))
    
    f.Fuzz(func(t *testing.T, duration int64, scale uint32) {
        // Пропускаємо некоректні входи
        if scale == 0 {
            return
        }
        
        d := time.Duration(duration)
        
        // ToScale не повинен панікувати
        result := ToScale(d, scale)
        
        // Базова валідація: результат має бути розумним
        if duration >= 0 && result == 0 && d >= time.Second/time.Duration(scale) {
            t.Errorf("unexpected zero result for %v @ scale %d", d, scale)
        }
    })
}
```

---

## 📋 Чек-лист якісних тестів для timescale

```go
// ✅ 1. Тестування базових випадків
{0, 0},  // нуль
{exact_value, expected_ticks},  // точні значення

// ✅ 2. Тестування меж округлення
{exact - 1ns, expected},  // округлення вниз
{exact + 0ns, expected},  // точне значення  
{exact + 1ns, expected},  // округлення вниз при малому залишку

// ✅ 3. Тестування великих значень
{time.Second * (1 << 32), expected},  // перевірка 64-бітної арифметики

// ✅ 4. Тестування від'ємних значень (для Relative)
{-exact, -expected},  // симетрія

// ✅ 5. Перевірка монотонності
for i := 0; i < N; i++ {
    if ToScale(d[i+1]) < ToScale(d[i]) { /* error */ }
}

// ✅ 6. Перевірка round-trip
ticks → Duration → ticks має давати ±1 tolerance

// ✅ 7. Бенчмарки для продуктивності
func BenchmarkToScale(b *testing.B) { /* ... */ }

// ✅ 8. Fuzzing для пошуку неочікуваних входів
func FuzzToScale(f *testing.F) { /* ... */ }
```

---

## 🔗 Корисні посилання

- 💻 [Go Testing Package](https://pkg.go.dev/testing) — офіційна документація
- 📄 [Go Fuzzing Guide](https://go.dev/doc/fuzz/) — fuzzing у Go 1.18+
- 🧪 [Go Benchmarks](https://go.dev/blog/benchmark) — написання бенчмарків
- 📊 [Go Coverage Reports](https://go.dev/blog/cover) — аналіз покриття тестами
- 🔄 [GitHub Actions for Go](https://docs.github.com/en/actions/automating-builds-and-tests/building-and-testing-go) — CI/CD інтеграція

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Додайте тести для різних шкал часу** — не тільки 90000, але й 44100, 48000 для аудіо.
> 2. **Тестуйте межі int32 для Relative()** — уникнення переповнення у production.
> 3. **Додайте бенчмарки** — моніторинг регресій продуктивності при змінах алгоритму.
> 4. **Використовуйте fuzzing** — автоматичний пошук крайніх випадків, які важко передбачити.
> 5. **Інтегруйте coverage у CI** — забезпечення мінімального порогу покриття (напр. 90%).

Потрібен приклад реалізації інтеграції цих тестів з вашим основним проектом, або допомога з написанням додаткових тестів для інших функцій `timescale`? Готовий допомогти! 🚀