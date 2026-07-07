# 🔧 `marshal.go` / `unmarshal.go`: Ядро серіалізації `go-mp4`

Це **основний модуль бібліотеки `go-mp4`**, який відповідає за **перетворення структур Go ↔ бінарний формат MP4** за допомогою рефлексії, бітового введення/виведення та метаданих полів.

---

## 🎯 Коротка відповідь

> **Це "двигун" бібліотеки**: він автоматично серіалізує/десеріалізує будь-який бокс, описаний тегами `mp4:"..."`, без написання ручних парсерів — обробляючи бітові поля, динамічні розміри, версії, прапорці та спеціальні типи даних.

---

## 🧱 Ключові константи та типи

### 🔹 `anyVersion = math.MaxUint8`

```go
const anyVersion = math.MaxUint8  // 255
```

**🎯 Призначення**: Спеціальне значення, що означає "будь-яка версія". Використовується в метаданих полів для позначення, що поле присутнє у всіх версіях боксу.

---

### 🔹 `ErrUnsupportedBoxVersion`

```go
var ErrUnsupportedBoxVersion = errors.New("unsupported box version")
```

**🎯 Призначення**: Помилка, що повертається, якщо версія боксу не підтримується (напр. `ver=0`, але файл має версію 1).

---

### 🔹 `marshaller` / `unmarshaller` — внутрішні структури

```go
type marshaller struct {
    writer bitio.Writer  // 🔹 Бітовий записувач
    wbits  uint64        // 🔹 Лічильник записаних біт
    src    IImmutableBox // 🔹 Вихідна структура
    ctx    Context       // 🔹 Контекст парсингу
}

type unmarshaller struct {
    reader bitio.ReadSeeker  // 🔹 Бітовий читач з позиціонуванням
    dst    IBox              // 🔹 Цільова структура
    size   uint64            // 🔹 Розмір корисного навантаження боксу
    rbits  uint64            // 🔹 Лічильник прочитаних біт
    ctx    Context           // 🔹 Контекст парсингу
}
```

**🎯 Призначення**: Інкапсулюють стан під час серіалізації/десеріалізації, відстежуючи бітову позицію та контекст.

---

## 🔍 Допоміжна функція: `readerHasSize`

```go
func readerHasSize(reader bitio.ReadSeeker, size uint64) bool {
    pre, err := reader.Seek(0, io.SeekCurrent)  // 🔹 Запам'ятовуємо поточну позицію
    if err != nil { return false }
    
    end, err := reader.Seek(0, io.SeekEnd)      // 🔹 Йдемо в кінець
    if err != nil { return false }
    
    if uint64(end-pre) < size {                 // 🔹 Чи вистачає байт?
        return false
    }
    
    _, err = reader.Seek(pre, io.SeekStart)     // 🔹 Повертаємось назад
    return err == nil
}
```

**🎯 Призначення**: Перевірити, чи в потоці достатньо даних для читання `size` байт, **не змінюючи поточну позицію** (окрім тимчасового переміщення).

**Використання**: У `unmarshalSlice` для безпечного читання масивів байт.

---

## 📤 СЕРІАЛІЗАЦІЯ: `Marshal` та `marshaller`

### 🔹 Основна функція `Marshal`

```go
func Marshal(w io.Writer, src IImmutableBox, ctx Context) (n uint64, err error) {
    // 🔹 Отримуємо метадані боксу
    boxDef := src.GetType().getBoxDef(ctx)
    if boxDef == nil { return 0, ErrBoxInfoNotFound }
    
    // 🔹 Рефлексія: отримуємо значення структури
    v := reflect.ValueOf(src).Elem()
    
    // 🔹 Ініціалізуємо маршалер
    m := &marshaller{
        writer: bitio.NewWriter(w),  // 🔹 Бітовий записувач
        src:    src,
        ctx:    ctx,
    }
    
    // 🔹 Рекурсивно маршалимо структуру
    if err := m.marshalStruct(v, boxDef.fields); err != nil {
        return 0, err
    }
    
    // 🔹 Перевірка: розмір має бути кратним 8 бітам (1 байту)
    if m.wbits%8 != 0 {
        return 0, fmt.Errorf("box size is not multiple of 8 bits: type=%s, bits=%d", 
            src.GetType().String(), m.wbits)
    }
    
    return m.wbits / 8, nil  // 🔹 Повертаємо розмір у байтах
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: структура `Trun{SampleCount: 3, DataOffset: 50, ...}`
│
▼
🔹 marshalStruct(v, fields):
   │
   ├── 🔹 Для кожного поля у `fields` (відсортованих за `order`):
   │   ├── 🔹 resolveFieldInstance() → обчислює динамічні `size`/`length`
   │   ├── 🔹 isTargetField() → чи читати це поле? (версія, прапорці...)
   │   ├── 🔹 fi.cfo.OnWriteField() → хук для кастомної логіки запису
   │   ├── 🔹 marshal() → рекурсивно записує значення поля
   │   └── 🔹 Оновлює лічильник `m.wbits`
   │
   ▼
🔹 Перевірка: `m.wbits % 8 == 0` → чи вирівняно по байтах?
│
▼
🔹 Вихід: записані байти + розмір у байтах
```

---

### 🔹 `marshalStruct` — запис структури

```go
func (m *marshaller) marshalStruct(v reflect.Value, fs []*field) error {
    for _, f := range fs {
        fi := resolveFieldInstance(f, m.src, v, m.ctx)  // 🔹 Динамічні параметри
        
        if !isTargetField(m.src, fi, m.ctx) {            // 🔹 Чи потрібне це поле?
            continue
        }
        
        // 🔹 Хук для кастомної логіки запису
        wbits, override, err := fi.cfo.OnWriteField(f.name, m.writer, m.ctx)
        if err != nil { return err }
        m.wbits += wbits
        if override { continue }  // 🔹 Якщо хук обробив поле — пропускаємо стандартну логіку
        
        // 🔹 Стандартний запис поля
        err = m.marshal(v.FieldByName(f.name), fi)
        if err != nil { return err }
    }
    return nil
}
```

**🎯 Ключова особливість**: Підтримка **хуків** `OnWriteField`, які дозволяють структурі перевизначити логіку запису конкретного поля.

---

### 🔹 `marshalSlice` — запис масиву (з оптимізацією)

```go
func (m *marshaller) marshalSlice(v reflect.Value, fi *fieldInstance) error {
    length := uint64(v.Len())
    if fi.length != LengthUnlimited {
        if length < uint64(fi.length) {
            return fmt.Errorf("the slice has too few elements: required=%d actual=%d", 
                fi.length, length)
        }
        length = uint64(fi.length)  // 🔹 Обрізаємо до фіксованої довжини
    }
    
    // 🔹 ОПТИМІЗАЦІЯ: якщо масив байт, розмір елемента 8 біт, і вирівняно по байтах
    elemType := v.Type().Elem()
    if elemType.Kind() == reflect.Uint8 && fi.size == 8 && m.wbits%8 == 0 {
        // 🔹 Прямий копіювання байт без бітової обробки!
        if _, err := io.CopyN(m.writer, bytes.NewBuffer(v.Bytes()), int64(length)); err != nil {
            return err
        }
        m.wbits += length * 8
        return nil
    }
    
    // 🔹 Стандартний поелементний запис
    for i := 0; i < int(length); i++ {
        m.marshal(v.Index(i), fi)
    }
    return nil
}
```

**🎯 Оптимізація**: Для масивів байт (`[]uint8`) з розміром елемента 8 біт і вирівнюванням по байтах — використовується **пряме копіювання** через `io.CopyN`, що значно швидше за поелементну бітову обробку.

---

### 🔹 `marshalUint` / `marshalInt` — запис цілих чисел з довільним розміром

```go
func (m *marshaller) marshalUint(v reflect.Value, fi *fieldInstance) error {
    val := v.Uint()
    
    // 🔹 Спеціальна обробка varint (MPEG-4)
    if fi.is(fieldVarint) {
        m.writeUvarint(val)
        return nil
    }
    
    // 🔹 Запис біт за бітом (з підтримкою розмірів, не кратних 8)
    for i := uint(0); i < fi.size; i += 8 {
        v := val
        size := uint(8)
        if fi.size > i+8 {
            v = v >> (fi.size - (i + 8))  // 🔹 Зсуваємо для запису старших біт
        } else if fi.size < i+8 {
            size = fi.size - i            // 🔹 Останній фрагмент менше 8 біт
        }
        if err := m.writer.WriteBits([]byte{byte(v)}, size); err != nil {
            return err
        }
        m.wbits += uint64(size)
    }
    return nil
}
```

**🎯 Підтримка нестандартних розмірів**: Напр., `size=17` для `Int17` — записується як 2 байти (16+1 біт).

---

### 🔹 `writeUvarint` — запис MPEG-4 varint

```go
func (m *marshaller) writeUvarint(u uint64) error {
    // 🔹 Записуємо старші 7-бітні групи з прапорцем продовження (біт 7=1)
    for i := 21; i > 0; i -= 7 {
        if err := m.writer.WriteBits([]byte{(byte(u >> uint(i))) | 0x80}, 8); err != nil {
            return err
        }
        m.wbits += 8
    }
    // 🔹 Остання група без прапорця продовження (біт 7=0)
    if err := m.writer.WriteBits([]byte{byte(u) & 0x7f}, 8); err != nil {
        return err
    }
    m.wbits += 8
    return nil
}
```

**🔢 Приклад**: `312` (0x138) → `[0x81, 0x38]`:
- `0x81` = `1000 0001` → біт 7=1 (продовження), дані=0000001
- `0x38` = `0011 1000` → біт 7=0 (кінець), дані=0111000
- Разом: `0000001 0111000` = `0000 0001 0011 1000` = `0x0138` = 312 ✅

---

## 📥 ДЕСЕРІАЛІЗАЦІЯ: `Unmarshal` та `unmarshaller`

### 🔹 Основні функції `UnmarshalAny` / `Unmarshal`

```go
func UnmarshalAny(r io.ReadSeeker, boxType BoxType, payloadSize uint64, ctx Context) (box IBox, n uint64, err error) {
    // 🔹 Створюємо екземпляр боксу за типом
    dst, err := boxType.New(ctx)
    if err != nil { return nil, 0, err }
    
    // 🔹 Делегуємо до Unmarshal
    n, err = Unmarshal(r, payloadSize, dst, ctx)
    return dst, n, err
}

func Unmarshal(r io.ReadSeeker, payloadSize uint64, dst IBox, ctx Context) (n uint64, err error) {
    boxDef := dst.GetType().getBoxDef(ctx)
    if boxDef == nil { return 0, ErrBoxInfoNotFound }
    
    v := reflect.ValueOf(dst).Elem()
    dst.SetVersion(anyVersion)  // 🔹 Тимчасово встановлюємо "будь-яку версію" для парсингу FullBox
    
    u := &unmarshaller{
        reader: bitio.NewReadSeeker(r),  // 🔹 Бітовий читач
        dst:    dst,
        size:   payloadSize,
        ctx:    ctx,
    }
    
    // 🔹 Хук BeforeUnmarshal для кастомної логіки перед парсингом
    if n, override, err := dst.BeforeUnmarshal(r, payloadSize, u.ctx); err != nil {
        return 0, err
    } else if override {
        return n, nil  // 🔹 Якщо хук обробив — повертаємо результат
    } else {
        u.rbits = n * 8  // 🔹 Оновлюємо лічильник прочитаних біт
    }
    
    sn, err := r.Seek(0, io.SeekCurrent)  // 🔹 Запам'ятовуємо позицію для відкату при помилці версії
    if err != nil { return 0, err }
    
    // 🔹 Рекурсивний парсинг структури
    if err := u.unmarshalStruct(v, boxDef.fields); err != nil {
        if err == ErrUnsupportedBoxVersion {
            r.Seek(sn, io.SeekStart)  // 🔹 Відкат позиції при непідтримуваній версії
        }
        return 0, err
    }
    
    // 🔹 Перевірки коректності
    if u.rbits%8 != 0 {
        return 0, fmt.Errorf("box size is not multiple of 8 bits: type=%s, size=%d, bits=%d", 
            dst.GetType().String(), u.size, u.rbits)
    }
    if u.rbits > u.size*8 {
        return 0, fmt.Errorf("overrun error: type=%s, size=%d, bits=%d", 
            dst.GetType().String(), u.size, u.rbits)
    }
    
    return u.rbits / 8, nil  // 🔹 Повертаємо прочитані байти
}
```

**🔄 Алгоритм:**
```
🔹 Вхід: потік байт, тип боксу, розмір
│
▼
🔹 unmarshalStruct(v, fields):
   │
   ├── 🔹 Для кожного поля:
   │   ├── 🔹 resolveFieldInstance() → динамічні параметри
   │   ├── 🔹 isTargetField() → чи читати поле?
   │   ├── 🔹 fi.cfo.OnReadField() → хук для кастомного читання
   │   ├── 🔹 unmarshal() → рекурсивно читає значення
   │   └── 🔹 Оновлює лічильник `u.rbits`
   │
   ▼
🔹 Перевірки: вирівнювання, переповнення
│
▼
🔹 Вихід: заповнена структура + кількість прочитаних байт
```

---

### 🔹 `unmarshalSlice` — читання масиву (з оптимізацією)

```go
func (u *unmarshaller) unmarshalSlice(v reflect.Value, fi *fieldInstance) error {
    var slice reflect.Value
    elemType := v.Type().Elem()
    
    // 🔹 Визначення довжини масиву
    length := uint64(fi.length)
    if fi.length == LengthUnlimited {
        if fi.size != 0 {
            left := (u.size)*8 - u.rbits  // 🔹 Залишок біт у боксі
            if left%uint64(fi.size) != 0 {
                return errors.New("invalid alignment")
            }
            length = left / uint64(fi.size)  // 🔹 Обчислюємо кількість елементів
        } else {
            length = 0  // 🔹 Якщо size=0 — читаємо до кінця (рідкісний випадок)
        }
    }
    
    // 🔹 ОПТИМІЗАЦІЯ: пряме читання масиву байт
    if u.rbits%8 == 0 && elemType.Kind() == reflect.Uint8 && fi.size == 8 {
        totalSize := length * uint64(fi.size) / 8  // 🔹 Розмір у байтах
        
        if !readerHasSize(u.reader, totalSize) {   // 🔹 Перевірка наявності даних
            return fmt.Errorf("not enough bits")
        }
        
        buf := bytes.NewBuffer(make([]byte, 0, totalSize))
        if _, err := io.CopyN(buf, u.reader, int64(totalSize)); err != nil {
            return err
        }
        slice = reflect.ValueOf(buf.Bytes())  // 🔹 Створюємо слайс з прочитаних байт
        u.rbits += uint64(totalSize) * 8
    } else {
        // 🔹 Стандартний поелементний парсинг
        slice = reflect.MakeSlice(v.Type(), 0, 0)
        for i := 0; ; i++ {
            if fi.length != LengthUnlimited && uint(i) >= fi.length { break }
            if fi.length == LengthUnlimited && u.rbits >= u.size*8 { break }
            
            slice = reflect.Append(slice, reflect.Zero(elemType))
            if err := u.unmarshal(slice.Index(i), fi); err != nil { return err }
            if u.rbits > u.size*8 {  // 🔹 Захист від переповнення
                return fmt.Errorf("failed to read array completely: fieldName=\"%s\"", fi.name)
            }
        }
    }
    
    v.Set(slice)  // 🔹 Встановлюємо результат у цільове поле
    return nil
}
```

**🎯 Оптимізація**: Аналогічно до маршалінгу — для масивів байт використовується **пряме читання** через `io.CopyN`, що значно швидше.

---

### 🔹 `unmarshalUint` / `unmarshalInt` — читання цілих чисел

```go
func (u *unmarshaller) unmarshalUint(v reflect.Value, fi *fieldInstance) error {
    // 🔹 Обробка varint
    if fi.is(fieldVarint) {
        val, err := u.readUvarint()
        if err != nil { return err }
        v.SetUint(val)
        return nil
    }
    
    if fi.size == 0 {
        return fmt.Errorf("size must not be zero: %s", fi.name)
    }
    
    // 🔹 Читання біт за бітом
    data, err := u.reader.ReadBits(fi.size)  // 🔹 Читання фіксованої кількості біт
    if err != nil { return err }
    u.rbits += uint64(fi.size)
    
    // 🔹 Збірка значення з байт
    val := uint64(0)
    for i := range data {
        val <<= 8
        val |= uint64(data[i])
    }
    v.SetUint(val)
    return nil
}
```

**🎯 Підтримка нестандартних розмірів**: Напр., `size=17` читається як 3 байти (24 біти), але використовуються тільки перші 17 біт.

---

### 🔹 `readUvarint` — читання MPEG-4 varint

```go
func (u *unmarshaller) readUvarint() (uint64, error) {
    var val uint64
    for {
        octet, err := u.reader.ReadBits(8)  // 🔹 Читаємо байт
        if err != nil { return 0, err }
        u.rbits += 8
        
        val = (val << 7) + uint64(octet[0]&0x7f)  // 🔹 Додаємо 7 біт даних
        
        if octet[0]&0x80 == 0 {  // 🔹 Якщо біт 7=0 — це останній байт
            return val, nil
        }
        // 🔹 Інакше продовжуємо читати наступний байт
    }
}
```

**🔢 Приклад**: `[0x81, 0x38]` → `312`:
- `0x81` = `1000 0001` → біт 7=1 (продовження), дані=0000001 → `val = 0<<7 + 1 = 1`
- `0x38` = `0011 1000` → біт 7=0 (кінець), дані=0111000 → `val = 1<<7 + 56 = 128+56 = 184` ❌

> ⚠️ **Увага**: У наведеному коді є помилка в логіці збірки varint! Правильна формула:
> ```
> val = (val << 7) | uint64(octet[0] & 0x7f)
> ```
> Але навіть це не дасть 312 для `[0x81, 0x38]`. Насправді, MPEG-4 varint кодує числа **від молодших біт до старших**, тому правильний алгоритм:
> ```go
> func readUvarint() (uint64, error) {
>     var val uint64
>     var shift uint
>     for {
>         octet, err := u.reader.ReadBits(8)
>         if err != nil { return 0, err }
>         u.rbits += 8
>         val |= uint64(octet[0]&0x7f) << shift
>         if octet[0]&0x80 == 0 { break }
>         shift += 7
>     }
>     return val, nil
> }
> ```

---

## 🔑 Спеціальні обробки типів

### 🔹 Рядки: `string`, `boxstring`, `iso639-2`

```go
func (m *marshaller) marshalString(v reflect.Value, fi *fieldInstance) error {
    data := []byte(v.String())
    for _, b := range data {
        if err := m.writer.WriteBits([]byte{b}, 8); err != nil { return err }
        m.wbits += 8
    }
    // 🔹 boxstring не додає нуль-термінатор!
    if fi.is(fieldBoxString) {
        return nil
    }
    // 🔹 Звичайний string додає нуль-термінатор
    if err := m.writer.WriteBits([]byte{0x00}, 8); err != nil { return err }
    m.wbits += 8
    return nil
}
```

**🎯 Різниця**:
- `string` → C-string: `"foo"` → `['f','o','o',0x00]`
- `boxstring` → рядок до кінця боксу: `"foo"` → `['f','o','o']` (без 0x00)

---

### 🔹 Булеві значення

```go
func (m *marshaller) marshalBool(v reflect.Value, fi *fieldInstance) error {
    var val byte
    if v.Bool() {
        val = 0xff  // 🔹 true → 0xFF (всі біти 1)
    } else {
        val = 0x00  // 🔹 false → 0x00
    }
    if err := m.writer.WriteBits([]byte{val}, fi.size); err != nil { return err }
    m.wbits += uint64(fi.size)
    return nil
}
```

**🎯 Особливість**: Булеве значення займає `fi.size` біт (напр., 1 біт), але записується як `0x00` або `0xFF`.

---

### 🔹 UUID (16 байт)

```go
// 🔹 Обробляється як масив байт з прапорцем fieldUUID
// 🔹 При виводі через Stringify() форматується як "01020304-0506-0708-090a-0b0c0d0e0f10"
```

---

## ⚠️ Обробка помилок та валідація

### 🔹 Перевірка вирівнювання

```go
if m.wbits%8 != 0 {
    return 0, fmt.Errorf("box size is not multiple of 8 bits: type=%s, bits=%d", 
        src.GetType().String(), m.wbits)
}
```

**🎯 Призначення**: Забезпечити, що розмір боксу кратний 8 бітам (1 байту), як вимагає стандарт MP4.

---

### 🔹 Перевірка переповнення

```go
if u.rbits > u.size*8 {
    return 0, fmt.Errorf("overrun error: type=%s, size=%d, bits=%d", 
        dst.GetType().String(), u.size, u.rbits)
}
```

**🎯 Призначення**: Запобігти читанню за межі заявленого розміру боксу.

---

### 🔹 Відкат при непідтримуваній версії

```go
if err := u.unmarshalStruct(v, boxDef.fields); err != nil {
    if err == ErrUnsupportedBoxVersion {
        r.Seek(sn, io.SeekStart)  // 🔹 Відкат позиції!
    }
    return 0, err
}
```

**🎯 Призначення**: Якщо версія боксу не підтримується, відновити позицію читача, щоб дозволити іншому коду обробити цей бокс.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Серіалізація кастомного боксу

```go
type CustomCCTVBox struct {
    Box
    CameraID    uint32   `mp4:"0,size=32"`
    Timestamp   uint64   `mp4:"1,size=64"`
    Flags       uint8    `mp4:"2,size=8"`
    MetadataLen uint16   `mp4:"3,size=16"`
    Metadata    []byte   `mp4:"4,size=8,len=dynamic"`  // 🔹 Динамічна довжина!
}

// 🔹 Реалізація інтерфейсу для динамічної довжини
func (c *CustomCCTVBox) GetFieldLength(name string, ctx Context) uint {
    if name == "Metadata" {
        return uint(len(c.Metadata))
    }
    return 0
}

// 🔹 Використання:
box := &CustomCCTVBox{
    CameraID: 12345,
    Timestamp: 1678901234567,
    Flags: 0x01,
    Metadata: []byte("camera-001"),
}

var buf bytes.Buffer
n, err := mp4.Marshal(&buf, box, mp4.Context{})
if err != nil { log.Fatal(err) }
log.Printf("✅ Записано %d байт", n)
```

---

### 🔹 Приклад 2: Десеріалізація з хуком `BeforeUnmarshal`

```go
type MetaBox struct {
    FullBox `mp4:"0,extend"`
    // ... поля ...
}

// 🔹 Хук для обробки QuickTime-специфічного формату
func (m *MetaBox) BeforeUnmarshal(r io.ReadSeeker, size uint64, ctx Context) (n uint64, override bool, err error) {
    // 🔹 Перевіряємо, чи це QuickTime-формат (перші 4 байти не нулі)
    buf := make([]byte, 4)
    if _, err := io.ReadFull(r, buf); err != nil { return 0, false, err }
    if _, err := r.Seek(-4, io.SeekCurrent); err != nil { return 0, false, err }
    
    if buf[0]|buf[1]|buf[2]|buf[3] != 0x00 {
        // 🔹 QuickTime: немає FullBox заголовка → встановлюємо версію/прапорці вручну
        m.Version = 0
        m.Flags = [3]byte{0,0,0}
        return 0, true, nil  // 🔹 override=true → пропускаємо стандартний парсинг FullBox
    }
    return 0, false, nil  // 🔹 Стандартний парсинг
}
```

---

### 🔹 Приклад 3: Обробка `opt=dynamic` полів

```go
type ConditionalBox struct {
    FullBox `mp4:"0,extend"`
    Flags   uint32 `mp4:"1,size=32"`
    // 🔹 Поле присутнє, тільки якщо біт 0 у Flags встановлено
    ExtraData []byte `mp4:"2,size=8,opt=dynamic,len=dynamic"`
}

func (c *ConditionalBox) IsOptFieldEnabled(name string, ctx Context) bool {
    if name == "ExtraData" {
        return c.Flags & 0x01 != 0  // 🔹 Увімкнути, тільки якщо біт 0 = 1
    }
    return false
}

func (c *ConditionalBox) GetFieldLength(name string, ctx Context) uint {
    if name == "ExtraData" {
        return uint(len(c.ExtraData))
    }
    return 0
}
```

**🔄 Логіка парсингу**:
```
🔹 Якщо Flags & 0x01 == 0:
   • isTargetField() → false → поле пропускається
   • Metadata не читається з потоку

🔹 Якщо Flags & 0x01 != 0:
   • isTargetField() → true → читаємо Metadata
   • GetFieldLength() → повертає довжину
   • unmarshalSlice() → читає стільки байт
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `order` у тегах | Поля записуються/читаються у неправильному порядку → пошкодження даних | Завжди вказуйте унікальні порядкові номери: `mp4:"0", mp4:"1", ...` |
| `boxstring` не останнім полем | Записує/читає зайві байти → зсув наступних боксів | Завжди розміщуйте `boxstring` останнім у структурі |
| Забути реалізувати `GetFieldLength` для `len=dynamic` | Помилка: "invalid name of dynamic-length field" | Завжди реалізуйте інтерфейс `ICustomFieldObject` для динамічних полів |
| Неправильна логіка `IsOptFieldEnabled` | Опціональні поля читаються завжди/ніколи → помилка парсингу | Перевіряйте логіку: повертати `true` тільки коли поле має бути присутнє |
| Ігнорування вирівнювання по байтах | Помилка: "box size is not multiple of 8 bits" | Переконайтеся, що сума розмірів усіх полів кратна 8 |
| Неправильна обробка `varint` | Числа читаються неправильно → помилки в метаданих | Використовуйте правильний алгоритм збірки varint (від молодших біт) |

---

## 📋 Чекліст для вашого проекту

```
[ ] При створенні нових структур боксів:
    • Вказуйте `order` для кожного поля: `mp4:"0", mp4:"1", ...`
    • Використовуйте `size=N` для фіксованих розмірів, `size=dynamic` для змінних
    • Для масивів: `len=dynamic` + реалізуйте `GetFieldLength()`
    • Для опціональних полів: `opt=0xXXXX` або `opt=dynamic` + `IsOptFieldEnabled()`
    • Для валідації: `const=...` для очікуваних значень

[ ] Для динамічних полів:
    • Реалізуйте інтерфейс `ICustomFieldObject` (GetFieldSize, GetFieldLength, IsOptFieldEnabled)
    • Переконайтеся, що методи повертають коректні значення для поточного контексту
    • Тестуйте з різними версіями/прапорцями для перевірки логіки

[ ] Для рядкових полів:
    • Використовуйте `string` для C-string, `boxstring` для рядків до кінця боксу
    • Переконайтеся, що `boxstring` є останнім полем у структурі
    • Для мов: використовуйте `iso639-2` для автоматичного кодування/декодування

[ ] Для оптимізації продуктивності:
    • Для масивів байт: переконайтеся, що `size=8` і вирівнювання по байтах → використовується швидкий шлях
    • Уникайте зайвих хуків `OnReadField`/`OnWriteField`, якщо не потрібні
    • Кешуйте результати `buildFields()` для повторного використання

[ ] Для дебагу:
    • Логуйте метадані полів: log.Printf("🔧 Field %s: size=%d, flags=%b", f.name, f.size, f.flags)
    • Перевіряйте isTargetField() логіку: log.Printf("🎯 Field %s: target=%v", name, isTargetField(...))
    • Використовуйте `Stringify()` для перевірки виводу полів

[ ] Для тестування:
    • Створюйте тестові структури з різними комбінаціями тегів
    • Перевіряйте round-trip: структура → байти → структура
    • Тестуйте граничні випадки: порожні масиви, максимальні розміри, різні версії
```

---

## 🎯 Висновок

> **Цей модуль — "серце" бібліотеки `go-mp4`**, яке перетворює декларації у код.  
> Він забезпечує:
> • ✅ Автоматичну серіалізацію/десеріалізацію за тегами `mp4:"..."`
> • ✅ Підтримку бітових полів довільного розміру (1 біт, 17 біт, тощо)
> • ✅ Гнучку обробку динамічних розмірів, довжин та умовних полів
> • ✅ Оптимізацію для масивів байт через пряме копіювання
> • ✅ Хуки `BeforeUnmarshal`, `OnReadField`, `OnWriteField` для кастомної логіки
> • ✅ Надійну валідацію: вирівнювання, переповнення, підтримка версій

Для вашого **CCTV HLS Processor** це означає:
- 🚀 Швидка розробка: описуйте структури боксів тегами, а не пишіть парсери вручну
- 🔧 Гнучкість: легко додавати нові типи боксів або модифікувати існуючі
- ⚡ Продуктивність: оптимізовані шляхи для масивів байт, мінімальні накладні витрати рефлексії
- 🛡️ Надійність: автоматична валідація, обробка краєвих випадків, захист від переповнення
- 🧪 Тестованість: кожен бокс можна протестувати ізольовано через `Marshal`/`Unmarshal`

Потребуєте допомоги зі створенням нової структури боксу з динамічними полями або з дебагом серіалізації? Напишіть — покажу готовий приклад для вашого сценарію! 🚀🔧