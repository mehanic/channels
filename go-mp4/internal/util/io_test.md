# 🧪 Тести `util`: Перевірка роботи з нуль-термінованими рядками (C-strings)

Це **інтеграційний тест** для модуля `util`, який перевіряє коректність роботи функцій `ReadString` та `WriteString` — читання та запис нуль-термінованих рядків (C-style strings), що критично для парсингу текстових полів у форматах MP4/ISOBMFF.

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що робота з рядками у C-стилі працює коректно**: точне читання до першого `\0`, правильний запис з додаванням термінатора, обробка кількох рядків поспіль та коректна обробка кінця файлу (EOF).

---

## 📋 Огляд тестових функцій

### 🔹 `TestReadString` — тест читання нуль-термінованих рядків

```go
func TestReadString(t *testing.T) {
    // 🔹 Вхідний буфер: три рядки, розділені нуль-байтами
    r := bytes.NewReader([]byte{
        'f', 'i', 'r', 's', 't', 0,      // "first\0"
        's', 'e', 'c', 'o', 'n', 'd', 0,  // "second\0"
        't', 'h', 'i', 'r', 'd', 0,       // "third\0"
    })
    
    // 🔹 ТЕСТ 1: Читання першого рядка
    s, err := ReadString(r)
    require.NoError(t, err)              // ✅ Немає помилок
    assert.Equal(t, "first", s)          // ✅ Прочитано до першого \0
    
    // 🔹 ТЕСТ 2: Читання другого рядка (продовження з тієї ж позиції)
    s, err = ReadString(r)
    require.NoError(t, err)
    assert.Equal(t, "second", s)         // ✅ Прочитано наступний рядок
    
    // 🔹 ТЕСТ 3: Читання третього рядка
    s, err = ReadString(r)
    require.NoError(t, err)
    assert.Equal(t, "third", s)          // ✅ Останній рядок
    
    // 🔹 ТЕСТ 4: Спроба читання після кінця даних
    _, err = ReadString(r)
    assert.Equal(t, io.EOF, err)         // ✅ Очікувана помилка: кінець файлу
}
```

**🎯 Що тестується:**

| Операція | Вхід | Очікуваний результат | Чому це важливо |
|----------|------|---------------------|----------------|
| `ReadString()` #1 | `"first\0second\0third\0"` | `"first"`, позиція після першого `\0` | ✅ Читання зупиняється на першому термінаторі |
| `ReadString()` #2 | продовження потоку | `"second"` | ✅ Позиція зберігається між викликами |
| `ReadString()` #3 | продовження потоку | `"third"` | ✅ Коректна обробка останнього рядка |
| `ReadString()` #4 | потік вичерпано | `io.EOF` | ✅ Коректна обробка кінця файлу |

**🔑 Ключова логіка `ReadString`:**
```
🔹 Алгоритм:
1. Створюємо буфер для одного байта: b := make([]byte, 1)
2. Створюємо буфер для результату: buf := bytes.NewBuffer(nil)
3. Цикл:
   • Читаємо один байт: r.Read(b)
   • Якщо байт == 0 → повертаємо buf.String() (кінець рядка)
   • Інакше → додаємо байт у buf: buf.Write(b)
4. Якщо Read() повертає помилку (напр. EOF) → повертаємо помилку

🔹 Приклад для "first\0":
• Читання 'f' → b[0]=0x66 != 0 → buf="f"
• Читання 'i' → buf="fi"
• Читання 'r' → buf="fir"
• Читання 's' → buf="firs"
• Читання 't' → buf="first"
• Читання 0x00 → b[0]==0 → повертаємо "first" ✅
```

---

### 🔹 `TestWriteString` — тест запису нуль-термінованих рядків

```go
func TestWriteString(t *testing.T) {
    // 🔹 Створюємо буфер для запису
    w := bytes.NewBuffer(nil)
    
    // 🔹 Запис трьох рядків поспіль
    require.NoError(t, WriteString(w, "first"))   // ✅ Запис "first\0"
    require.NoError(t, WriteString(w, "second"))  // ✅ Запис "second\0"
    require.NoError(t, WriteString(w, "third"))   // ✅ Запис "third\0"
    
    // 🔹 Перевірка фінального вмісту буфера
    assert.Equal(t, []byte{
        'f', 'i', 'r', 's', 't', 0,      // "first\0"
        's', 'e', 'c', 'o', 'n', 'd', 0,  // "second\0"
        't', 'h', 'i', 'r', 'd', 0,       // "third\0"
    }, w.Bytes())
}
```

**🎯 Що тестується:**

| Операція | Вхід | Очікуваний результат | Чому це важливо |
|----------|------|---------------------|----------------|
| `WriteString("first")` | рядок "first" | байти `[0x66,0x69,0x72,0x73,0x74,0x00]` | ✅ Додавання нуль-термінатора |
| `WriteString("second")` | рядок "second" | дописується після попереднього | ✅ Позиція зберігається між викликами |
| `WriteString("third")` | рядок "third" | дописується в кінець | ✅ Коректне накопичення даних |
| Фінальна перевірка | — | `[... "first\0second\0third\0" ...]` | ✅ Точна відповідність очікуваному формату |

**🔑 Ключова логіка `WriteString`:**
```
🔹 Алгоритм:
1. Перетворюємо рядок у байти: []byte(s)
2. Записуємо основну частину: w.Write([]byte(s))
3. Записуємо нуль-термінатор: w.Write([]byte{0})
4. Повертаємо помилку, якщо будь-який Write() не вдався

🔹 Приклад для "first":
• []byte("first") = [0x66, 0x69, 0x72, 0x73, 0x74]
• Запис 5 байт у потік
• Запис 0x00 (термінатор)
• Результат: [0x66, 0x69, 0x72, 0x73, 0x74, 0x00] ✅
```

---

## 🔍 Як це працює разом: Round-trip тест

Хоча окремий round-trip тест відсутній, комбінація `TestReadString` + `TestWriteString` фактично перевіряє **ідентичність перетворення**:

```
🔹 Сценарій: Запис → Читання → Порівняння
│
▼
🔹 Крок 1: WriteString(w, "test")
   • Вихід у буфері: [0x74, 0x65, 0x73, 0x74, 0x00] ("test\0")
│
▼
🔹 Крок 2: ReadString(r) з того ж буфера
   • Читаємо байти до 0x00
   • Результат: "test"
│
▼
🔹 Крок 3: Порівняння
   • "test" == "test" → ✅ Round-trip успішний
```

**🎯 Призначення**: Гарантувати, що записаний рядок може бути коректно прочитаний назад — критично для серіалізації/десеріалізації метаданих.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Читання метаданих камери з боксу `udta`

```go
// 🔹 Функція для читання C-string полів з метаданих
func readMetadataField(r io.Reader, fieldName string) (string, error) {
    value, err := util.ReadString(r)
    if err != nil {
        if err == io.EOF {
            return "", fmt.Errorf("unexpected EOF while reading %s", fieldName)
        }
        return "", fmt.Errorf("failed to read %s: %w", fieldName, err)
    }
    
    // 🔹 Валідація: перевірка на порожній рядок або надто довгий
    if value == "" {
        log.Printf("⚠️  Empty value for field %s", fieldName)
    }
    if len(value) > 255 {
        log.Printf("⚠️  Field %s exceeds recommended length: %d chars", fieldName, len(value))
    }
    
    return value, nil
}

// 🔹 Використання у парсері метаданих:
func parseUserMetadata(r io.Reader) (*Metadata, error) {
    meta := &Metadata{}
    
    // 🔹 Читання полів у порядку, визначеному специфікацією
    var err error
    meta.CameraID, err = readMetadataField(r, "camera_id")
    if err != nil { return nil, err }
    
    meta.Location, err = readMetadataField(r, "location")
    if err != nil { return nil, err }
    
    meta.Description, err = readMetadataField(r, "description")
    if err != nil { return nil, err }
    
    return meta, nil
}
```

---

### 🔹 Приклад 2: Запис кастомних метаданих у новий бокс

```go
// 🔹 Функція для запису C-string полів у метадані
func writeMetadataField(w io.Writer, fieldName, value string) error {
    // 🔹 Валідація перед записом
    if len(value) > 255 {
        return fmt.Errorf("field %s value too long: %d > 255", fieldName, len(value))
    }
    
    // 🔹 Запис через util.WriteString (автоматичне додавання \0)
    if err := util.WriteString(w, value); err != nil {
        return fmt.Errorf("failed to write %s: %w", fieldName, err)
    }
    
    return nil
}

// 🔹 Використання у серіалізаторі:
func writeUserMetadata(w io.Writer, meta *Metadata) error {
    // 🔹 Запис полів у тому ж порядку, що й при читанні
    if err := writeMetadataField(w, "camera_id", meta.CameraID); err != nil {
        return err
    }
    if err := writeMetadataField(w, "location", meta.Location); err != nil {
        return err
    }
    if err := writeMetadataField(w, "description", meta.Description); err != nil {
        return err
    }
    
    return nil
}

// 🔹 Round-trip перевірка:
func TestMetadataRoundTrip(t *testing.T) {
    original := &Metadata{
        CameraID: "CAM-001",
        Location: "Building A, Floor 3",
        Description: "Entrance camera",
    }
    
    // 🔹 Запис у буфер
    var buf bytes.Buffer
    err := writeUserMetadata(&buf, original)
    require.NoError(t, err)
    
    // 🔹 Читання з того ж буфера
    parsed, err := parseUserMetadata(&buf)
    require.NoError(t, err)
    
    // 🔹 Порівняння
    assert.Equal(t, original.CameraID, parsed.CameraID)
    assert.Equal(t, original.Location, parsed.Location)
    assert.Equal(t, original.Description, parsed.Description)
}
```

---

### 🔹 Приклад 3: Безпечне читання з обмеженням для захисту від злонамірних файлів

```go
// 🔹 Функція для читання рядка з максимальним розміром (захист від DoS)
func ReadStringWithLimit(r io.Reader, maxLen int) (string, error) {
    b := make([]byte, 1)
    buf := bytes.NewBuffer(nil)
    
    for buf.Len() < maxLen {
        n, err := r.Read(b)
        if err != nil {
            if err == io.EOF && buf.Len() > 0 {
                // 🔹 Кінець файлу без термінатора — можливо, пошкоджений файл
                return "", fmt.Errorf("string not null-terminated: %q", buf.String())
            }
            return "", err
        }
        if n != 1 {
            return "", fmt.Errorf("unexpected read size: %d", n)
        }
        if b[0] == 0 {
            return buf.String(), nil
        }
        buf.Write(b)
    }
    
    // 🔹 Досягнуто ліміт без знаходження \0
    // 🔹 Пропускаємо решту рядка до термінатора або кінця
    for {
        n, err := r.Read(b)
        if err != nil || n == 0 {
            break
        }
        if b[0] == 0 {
            break  // 🔹 Знайшли термінатор, зупиняємо пропуск
        }
    }
    
    return "", fmt.Errorf("string exceeds max length %d", maxLen)
}

// 🔹 Використання для валідації вхідних даних:
func parseSafeMetadata(r io.Reader) (*Metadata, error) {
    const maxFieldLen = 256
    
    meta := &Metadata{}
    var err error
    
    meta.CameraID, err = ReadStringWithLimit(r, maxFieldLen)
    if err != nil {
        return nil, fmt.Errorf("invalid camera_id: %w", err)
    }
    
    // ... інші поля ...
    
    return meta, nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Відсутність нуль-термінатора у файлі | `ReadString` читає до кінця файлу → `io.EOF` | Перевіряйте вхідні файли: чи всі рядки закінчуються `\0`; додавайте валідацію |
| Запис рядка без термінатора | Інші інструменти не можуть розпарсити поле | Завжди використовуйте `WriteString`, не `w.Write([]byte(s))` |
| Читання після кінця рядка | Зсув позиції, пошкодження наступних полів | `ReadString` зупиняється на `\0` — не читайте зайвого після виклику |
| Неправильне кодування (ASCII vs UTF-8) | "Кракозябри" у назвах з не-ASCII символами | Використовуйте UTF-8 для нових боксів; перевіряйте специфікацію боксу |
| Порожній рядок обробляється некоректно | Очікується "" але отримується помилка | `ReadString` коректно повертає "" для [0x00] — перевіряйте логіку обробки порожніх значень |

---

## 📋 Чекліст для вашого проекту

```
[ ] При читанні рядків з файлу:
    • Використовуйте ReadString для полів, визначених як C-string
    • Перевіряйте помилки: EOF може означати пошкоджений файл
    • Для безпеки: додавайте ліміт довжини через ReadStringWithLimit
    • Логувайте прочитані значення для дебагу: log.Printf("📝 %s: %q", field, value)

[ ] При записі рядків у файл:
    • Завжди використовуйте WriteString, не забувайте про \0
    • Перевіряйте помилки запису: io.Writer може повернути error
    • Для сумісності: використовуйте UTF-8 для нових метаданих
    • Валідуйте довжину перед записом: max 255 символів для сумісності

[ ] Для валідації вхідних даних:
    • Перевіряйте довжину рядків перед записом (напр. макс. 255 символів)
    • Відхиляйте рядки з контрольними символами (окрім \0)
    • Логувайте попередження для не-ASCII символів у старих боксах
    • Тестуйте з різними кодуваннями: ASCII, UTF-8, Latin-1

[ ] Для дебагу:
    • Логувайте прочитані рядки: log.Printf("📝 Read: %q", name)
    • Перевіряйте наявність \0 у вихідному файлі: hexdump -C file.mp4 | grep 00
    • Тестуйте з рядками різної довжини: "", "a", "Hello", дуже_довгий_рядок

[ ] Для тестування:
    • Створюйте тестові буфери з контрольованими C-рядками
    • Перевіряйте round-trip: WriteString → ReadString → порівняння
    • Тестуйте крайні випадки: порожній рядок, тільки \0, дуже довгі рядки
    • Тестуйте помилкові сценарії: відсутність \0, EOF посеред рядка
```

---

## 🎯 Висновок

> **Цей тест — ваш "золотий стандарт" для надійної роботи з текстовими полями у MP4**.  
> Він гарантує:
> • ✅ Коректне читання нуль-термінованих рядків до першого `\0`
> • ✅ Надійний запис рядків з автоматичним додаванням термінатора
> • ✅ Правильну обробку кількох рядків поспіль у одному потоці
> • ✅ Коректну обробку кінця файлу (EOF) як помилки, а не даних
> • ✅ Ідентичність round-trip: записаний рядок читається назад без змін

Для вашого **CCTV HLS Processor** це означає:
- 🔍 Надійне читання метаданих камери, назв записів, коментарів з файлів
- ✍️ Коректний запис кастомних текстових полів у нових боксах
- 🌐 Сумісність з усіма плеєрами, що очікують стандартні C-рядки
- 🛡️ Захист від пошкоджених або злонамірних файлів через валідацію довжини
- 🧪 Легке тестування через контрольовані буфери з нуль-термінаторами

Потребуєте допомоги з інтеграцією `ReadString`/`WriteString` у ваш конвеєр обробки метаданих або з валідацією текстових полів у вхідних файлах? Напишіть — покажу готовий код для вашого сценарію! 🚀🔤