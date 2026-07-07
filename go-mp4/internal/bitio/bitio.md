# ⚠️ `bitio/errors.go`: Помилки бітового введення/виведення у `go-mp4`

Це **мінімальний, але критичний модуль** бібліотеки `go-mp4`, який визначає **спеціалізовані помилки** для операцій бітового читання/запису — фундамент для надійної обробки низькорівневих бінарних даних у форматах MP4/ISOBMFF.

---

## 🎯 Коротка відповідь

> **Це "словник помилок" для бітових операцій**: він визначає дві специфічні помилки — `ErrInvalidAlignment` для вирівнювання бітів та `ErrDiscouragedReader` для проблемних реалізацій `io.Reader` — що дозволяє бібліотеці коректно обробляти крайні випадки при парсингу бітових полів.

---

## 🧱 Оголошені помилки

### 🔹 `ErrInvalidAlignment` — помилка вирівнювання бітів

```go
var ErrInvalidAlignment = errors.New("invalid alignment")
```

**🎯 Призначення**: Сигналізувати, що операція бітового читання/запису **порушує вимоги вирівнювання** стандарту MP4.

**🔢 Коли виникає:**
```
🔹 Приклад 1: Читання 17-бітного поля з позиції, не кратної 8
📐 Вимога: розмір боксу має бути кратним 8 бітам (1 байту)
❌ Якщо wbits%8 != 0 після запису → ErrInvalidAlignment

🔹 Приклад 2: Неправильна довжина масиву при бітовому парсингу
📐 Вимога: left%fi.size == 0 для обчислення кількості елементів
❌ Якщо залишок біт не ділиться на розмір елемента → ErrInvalidAlignment
```

**🔑 Код, що генерує цю помилку:**
```go
// 🔹 У marshal.go:
if m.wbits%8 != 0 {
    return 0, fmt.Errorf("box size is not multiple of 8 bits: type=%s, bits=%d", 
        src.GetType().String(), m.wbits)  // ← Аналог ErrInvalidAlignment
}

// 🔹 У unmarshal.go (для масивів):
if fi.length == LengthUnlimited {
    if fi.size != 0 {
        left := (u.size)*8 - u.rbits
        if left%uint64(fi.size) != 0 {  // ← Перевірка вирівнювання
            return errors.New("invalid alignment")  // ← ErrInvalidAlignment
        }
        length = left / uint64(fi.size)
    }
}
```

**🎯 Навіщо це?** Стандарт ISO/IEC 14496-12 вимагає, щоб **розмір кожного боксу був кратним 8 бітам**. Ця помилка запобігає створенню/читанню невалідних файлів.

---

### 🔹 `ErrDiscouragedReader` — попередження про проблемний `io.Reader`

```go
var ErrDiscouragedReader = errors.New("discouraged reader implementation")
```

**🎯 Призначення**: Попередити, що передана реалізація `io.Reader` **не підтримує ефективне бітове читання** і може призвести до повільної роботи або помилок.

**🔢 Коли виникає:**
```
🔹 Приклад: Використання io.Reader без підтримки Seek
📐 Вимога: бітовий читач повинен підтримувати позиціонування для назаднього читання
❌ Якщо reader не реалізує io.Seeker → ErrDiscouragedReader

🔹 Приклад: Читач, що не гарантує атомарність ReadBits()
📐 Вимога: ReadBits(n) має читати рівно n біт без побічних ефектів
❌ Якщо реалізація "заїдає" зайві байти → ErrDiscouragedReader
```

**🔑 Код, що генерує цю помилку (гіпотетичний):**
```go
// 🔹 У bitio.NewReader():
func NewReader(r io.Reader) Reader {
    if _, ok := r.(io.Seeker); !ok {
        // 🔹 Попередження: без Seek неможливо ефективно працювати з бітами
        log.Printf("⚠️  Reader without Seek support may cause issues")
        // Можливе повернення помилки у суворому режимі:
        // return nil, ErrDiscouragedReader
    }
    return &bitReader{r: r, ...}
}
```

**🎯 Навіщо це?** Бітові операції в MP4 часто вимагають:
- ✅ Позиціонування назад для повторного читання заголовків
- ✅ Точного контролю над кількістю прочитаних біт
- ✅ Буферизації для уникнення зайвих системних викликів

Без цих властивостей парсинг може бути **повільним або некоректним**.

---

## 🔄 Як ці помилки використовуються в бібліотеці

### 🔹 У `marshal.go` / `unmarshal.go`

```go
// 🔹 Перевірка вирівнювання після запису:
func Marshal(w io.Writer, src IImmutableBox, ctx Context) (n uint64, err error) {
    // ... запис полів ...
    
    if m.wbits%8 != 0 {
        // 🔹 Помилка: розмір не кратний 8 бітам
        return 0, fmt.Errorf("box size is not multiple of 8 bits")
        // ← Концептуально це ErrInvalidAlignment
    }
    return m.wbits / 8, nil
}

// 🔹 Перевірка вирівнювання для масивів:
func (u *unmarshaller) unmarshalSlice(v reflect.Value, fi *fieldInstance) error {
    if fi.length == LengthUnlimited && fi.size != 0 {
        left := (u.size)*8 - u.rbits
        if left%uint64(fi.size) != 0 {
            return ErrInvalidAlignment  // ← Явне використання
        }
        length = left / uint64(fi.size)
    }
    // ...
}
```

---

### 🔹 У `bitio` пакеті (гіпотетична реалізація)

```go
// 🔹 Приклад ReadBits з перевіркою вирівнювання:
func (r *bitReader) ReadBits(n uint) ([]byte, error) {
    if n == 0 {
        return nil, nil
    }
    
    // 🔹 Перевірка: чи можемо прочитати ціле число байт?
    if n%8 != 0 && r.bitOffset != 0 {
        // 🔹 Складний випадок: читання між байтами
        // Може вимагати спеціальної обробки або повернення помилки
        return nil, ErrInvalidAlignment
    }
    
    // ... логіка читання ...
}
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Валідація вирівнювання при створенні кастомних боксів

```go
// 🔹 Кастомний бокс з бітовими полями
type CustomBitField struct {
    mp4.Box
    Flag1    bool  `mp4:"0,size=1"`   // 🔹 1 біт
    Reserved uint8 `mp4:"1,size=3"`   // 🔹 3 біти паддінгу
    Value    uint8 `mp4:"2,size=4"`   // 🔹 4 біти даних
    // ✅ Разом: 1+3+4 = 8 біт = 1 байт → вирівняно!
}

// 🔹 Перевірка перед серіалізацією:
func validateBitAlignment(box mp4.IImmutableBox) error {
    // 🔹 Спроба маршалінгу у буфер для перевірки
    var buf bytes.Buffer
    _, err := mp4.Marshal(&buf, box, mp4.Context{})
    
    if errors.Is(err, ErrInvalidAlignment) {
        return fmt.Errorf("box %s has invalid bit alignment — ensure total size is multiple of 8", 
            box.GetType())
    }
    return err
}

// 🔹 Використання:
custom := &CustomBitField{Flag1: true, Reserved: 0, Value: 15}
if err := validateBitAlignment(custom); err != nil {
    log.Printf("❌ Invalid alignment: %v", err)
    // 🔹 Виправлення: додати ще 4 біти паддінгу
}
```

---

### 🔹 Приклад 2: Безпечне читання з мережевих потоків

```go
// 🔹 Обгортка для мережевого читача з підтримкою Seek
type SeekableNetworkReader struct {
    conn   net.Conn
    buffer bytes.Buffer
    pos    int64
}

func (r *SeekableNetworkReader) Read(p []byte) (int, error) {
    // 🔹 Реалізація Read з буферизацією
    // ...
}

func (r *SeekableNetworkReader) Seek(offset int64, whence int) (int64, error) {
    // 🔹 Реалізація Seek для підтримки бітових операцій
    // ⚠️ Для мережевих потоків це може бути неефективно!
    if whence == io.SeekCurrent && offset < 0 {
        // 🔹 Спроба читати назад → може вимагати повторного з'єднання
        return 0, fmt.Errorf("backward seek not supported on network stream")
    }
    // ...
}

// 🔹 Перевірка перед використанням у bitio:
func createBitReader(r io.Reader) (bitio.Reader, error) {
    if _, ok := r.(io.Seeker); !ok {
        // 🔹 Попередження про можливі проблеми
        log.Printf("⚠️  Using reader without Seek support — may cause ErrDiscouragedReader")
        // 🔹 Або повернення помилки у суворому режимі:
        // return nil, bitio.ErrDiscouragedReader
    }
    return bitio.NewReader(r), nil
}
```

---

### 🔹 Приклад 3: Обробка помилок вирівнювання у конвеєрі парсингу

```go
// 🔹 Універсальна функція парсингу з обробкою помилок вирівнювання
func parseBoxSafely(r io.ReadSeeker, boxType mp4.BoxType, size uint64, ctx mp4.Context) (mp4.IBox, error) {
    box, err := boxType.New(ctx)
    if err != nil {
        return nil, err
    }
    
    n, err := mp4.Unmarshal(r, size, box, ctx)
    
    // 🔹 Спеціальна обробка помилок вирівнювання
    if errors.Is(err, ErrInvalidAlignment) || 
       (err != nil && strings.Contains(err.Error(), "invalid alignment")) {
        
        log.Printf("⚠️  Alignment error for box %s at offset %d: %v", 
            boxType, r.Seek(0, io.SeekCurrent), err)
        
        // 🔹 Спроба відновлення: пропустити до наступного байта
        if rbits, _ := r.Seek(0, io.SeekCurrent); rbits%8 != 0 {
            skip := 8 - (rbits % 8)
            r.Seek(int64(skip), io.SeekCurrent)
            log.Printf("🔧 Skipped %d bits to realign", skip)
        }
        
        // 🔹 Повторна спроба парсингу (опціонально)
        // ...
    }
    
    return box, err
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Ігнорування `ErrInvalidAlignment` | Пошкоджені файли, неможливість читання іншими плеєрами | Завжди перевіряйте, що сума розмірів полів кратна 8: `totalBits%8 == 0` |
| Неправильне вирівнювання бітових полів | Помилка при парсингу: "invalid alignment" | Додавайте паддінг-поля: `Reserved uint8 \`mp4:"size=3,const=0"\`` |
| Використання `io.Reader` без `Seek` | Повільне читання або `ErrDiscouragedReader` | Використовуйте `bufseekio.NewReadSeeker()` для буферизації з підтримкою seek |
| Забути перевірку `left%size == 0` для масивів | Неправильна кількість елементів у масиві | Завжди перевіряйте вирівнювання перед обчисленням `length = left / size` |
| Неправильна обробка `ReadBits` між байтами | "З'їдені" біти, зсув даних | Використовуйте готові реалізації `bitio.Reader`, не пишіть власні без глибокого розуміння |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні структур з бітовими полями:
    • Переконайтеся, що сума всіх `size=N` кратна 8
    • Додавайте паддінг-поля з `const=0` для вирівнювання: `Reserved uint8 \`mp4:"size=4,const=0"\``
    • Тестуйте маршалінг/анмаршалінг для перевірки вирівнювання

[ ] Для роботи з мережевими/потоковими джерелами:
    • Використовуйте буферизовані читачі з підтримкою Seek: bufseekio.NewReadSeeker()
    • Уникайте прямого використання net.Conn у бітових операціях без обгортки
    • Перевіряйте підтримку Seek перед передачею у bitio.NewReader()

[ ] Для обробки помилок:
    • Перевіряйте errors.Is(err, ErrInvalidAlignment) для специфічної обробки
    • Логувайте деталі вирівнювання: offset, rbits, expected alignment
    • Реалізуйте механізми відновлення: пропуск до наступного байта при помилці

[ ] Для дебагу:
    • Логувайте кількість прочитаних/записаних біт: log.Printf("🔢 Bits: %d", rbits)
    • Перевіряйте вирівнювання після кожної операції: if bits%8 != 0 { ... }
    • Використовуйте тестові файли з відомою бітовою структурою для валідації

[ ] Для тестування:
    • Створюйте тестові структури з нестандартними розмірами: size=1, size=17, size=59
    • Перевіряйте round-trip: Marshal → Unmarshal → порівняння з оригіналом
    • Тестуйте крайні випадки: порожні масиви, максимальні розміри, непарні суми біт
```

---

## 🎯 Висновок

> **Ці дві помилки — "вартові цілісності" бітових операцій у go-mp4**, які забезпечують:
> • ✅ Суворе дотримання стандарту вирівнювання (кратність 8 бітам)
> • ✅ Захист від повільних або некоректних реалізацій `io.Reader`
> • ✅ Чітку сигналізацію про низькорівневі проблеми парсингу
> • ✅ Можливість специфічної обробки помилок вирівнювання
> • ✅ Консистентність між різними частинами бібліотеки

Для вашого **CCTV HLS Processor** це означає:
- 🛡️ Надійна валідація кастомних боксів з бітовими полями перед записом
- ⚡ Ефективна робота з мережевими потоками через буферизовані читачі
- 🔍 Чітке розуміння причин помилок парсингу через специфічні типи помилок
- 🔄 Можливість відновлення після помилок вирівнювання без втрати даних
- 🧪 Легке тестування бітової логіки через контрольовані тестові випадки

Потребуєте допомоги з налаштуванням бітових полів у ваших кастомних боксах або з обробкою помилок вирівнювання у конвеєрі парсингу? Напишіть — покажу готовий код для вашого сценарію! 🚀⚠️