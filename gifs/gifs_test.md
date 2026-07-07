# 🔍 Глибокий розбір тестів: інтеграційні тести пакету `gifs`

Цей файл містить **інтеграційні тести**, які взаємодіють з реальним зовнішнім API `https://api.gifs.com`. Розберемо архітектурно, з критичним аналізом та рекомендаціями.

---

## 📦 Архітектура тестового файлу: огляд

```
┌─────────────────────────────────────┐
│ Файл: gifs_test.go                  │
│ Тип тестів: Інтеграційні (E2E)      │
│ Залежності:                         │
│   • Реальний API gifs.com           │
│   • Змінна оточення AUTH            │
│   • Файлова система                 │
├─────────────────────────────────────┤
│ Тести:                              │
│   • TestSimpleYoutube — імпорт YouTube│
│   • TestYoutube — імпорт з метаданими│
│   • TestDownload — завантаження файлу│
│   • TestUpload — завантаження файлу в API│
│   • TestSaveGif — збереження результату│
│   • TestBulkUpload — пакетне завантаження│
└─────────────────────────────────────┘
```

### 🎯 Поточний стан: ⚠️ **Небезпечно для CI/CD**

| Проблема | Наслідок | Критичність |
|----------|----------|-------------|
| Реальні HTTP-запити | Тести ламаються при мережевих проблемах | 🔴 Висока |
| Відсутність моків | Неможливо тестувати без API-ключа | 🔴 Висока |
| `t.Fail()` без повідомлень | Неможливо зрозуміти причину провалу | 🟡 Середня |
| Файли не видаляються | Засмічення диска, конфлікти тестів | 🟡 Середня |
| Немає таймаутів | Тести можуть висіти назавжди | 🔴 Висока |
| Залежність між тестами | Порядок виконання впливає на результат | 🟡 Середня |

---

## 🔬 Детальний розбір кожного тесту

### 1️⃣ `init()` — ініціалізація аутентифікації

```go
func init() {
    Authentication = os.Getenv("AUTH")
}
```

#### ⚠️ Проблеми
```go
// ❌ Глобальна змінна змінюється в init()
// • Важко тестувати ізольовано
// • Неможливо мати різні ключі для різних тестів
// • Якщо AUTH не встановлено — тести тихо проваляться

// ✅ Краще: перевіряти наявність ключа та пропускати тести
func getTestAPIKey() string {
    key := os.Getenv("AUTH")
    if key == "" {
        return ""  // Тести мають перевіряти це
    }
    return key
}
```

---

### 2️⃣ `TestSimpleYoutube` — базовий імпорт

```go
func TestSimpleYoutube(t *testing.T) {
    file := &New{
        Source: "https://www.youtube.com/watch?v=V6wrI6DEZFk",
    }
    response, err := file.Create()
    if err != nil {
        t.Fail()  // ❌ Нічого не зрозуміло!
    }
    if response.Files.Gif == "" {
        t.Fail()  // ❌ Чому провал? Пустий GIF чи помилка парсингу?
    }
    // ...
    t.Log("Gif URL: ", response.Files.Gif)  // ✅ Логи допомагають, але після провалу
}
```

#### ⚠️ Проблеми
```go
// ❌ t.Fail() без повідомлення:
// • При провалі: "--- FAIL: TestSimpleYoutube (2.34s)" — і все
// • Не зрозуміло: мережа? авторизація? змінився формат API?

// ✅ Правильно:
if err != nil {
    t.Fatalf("Create() failed: %v", err)  // Зупиняє тест + повідомлення
}
// Або з testify:
require.NoError(t, err, "Create() should succeed")

// ❌ Перевірка порожніх рядків без контексту:
if response.Files.Gif == "" {
    t.Fail()
}
// ✅ Правильно:
assert.NotEmpty(t, response.Files.Gif, "GIF URL should not be empty")
```

---

### 3️⃣ `TestYoutube` — імпорт з метаданими

```go
func TestYoutube(t *testing.T) {
    input := &New{
        Source: "https://www.youtube.com/watch?v=dDmQ0byhus4",
        Title:  "Cute Kitten Drinking From Sink",
        Tags:   []string{"cute", "kitten", "drinking"},
        Attribution: &Attribution{Site: "twitter", User: "stronghold2d"},
        Trim: &Trim{Start: 10, End: 20},
        Safe: true,
    }
    response, err := input.Create()
    // ... ті самі проблеми з t.Fail()
}
```

#### 🎯 Що тестує цей кейс?
| Поле | Призначення | Ризик |
|------|-------------|-------|
| `Title` | Перевірка передачі метаданих | Низький |
| `Tags` | Масив рядків у JSON | Низький |
| `Attribution` | Вкладена структура | Середній (може змінитися в API) |
| `Trim` | Обрізка відео | 🔴 Високий (логіка на стороні сервера) |
| `Safe` | NSFW-фільтр | Середній (може вплинути на результат) |

#### ⚠️ Ризик: зміна поведінки зовнішнього сервісу
```go
// ❌ YouTube-відео може бути видалене, приватне, або змінити формат
// ❌ API gifs.com може змінити обробку параметра Trim
// ❌ Результат тесту залежить від третіх сторін!

// ✅ Рішення: мок-сервер для стабільних тестів
// (див. рекомендації нижче)
```

---

### 4️⃣ `TestDownload` — завантаження файлу

```go
func TestDownload(t *testing.T) {
    file := DownloadFile("echo-hereweare.mp4", "https://raw.githubusercontent.com/...")
    if file == "" {
        t.Fail()  // ❌ Не зрозуміло: мережа? права на запис?
    }
    t.Log("Downloaded File: ", file)
    // ❌ Файл не видаляється після тесту!
}
```

#### ⚠️ Проблеми
```go
// ❌ Файл залишається на диску:
// • Наступний запуск тесту може мати конфлікт
// • Засмічення диска в CI/CD
// • Тести не ізольовані

// ✅ Правильно: очищати після тесту
func TestDownload(t *testing.T) {
    filename := "echo-hereweare_test.mp4"  // Унікальне ім'я
    defer os.Remove(filename)  // ✅ Гарантоване видалення
    
    result := DownloadFile(filename, testURL)
    require.NotEmpty(t, result, "Download should return filename")
    
    // ✅ Додаткова перевірка: файл існує та не порожній
    info, err := os.Stat(filename)
    require.NoError(t, err)
    assert.Greater(t, info.Size(), int64(0), "Downloaded file should not be empty")
}
```

---

### 5️⃣ `TestUpload` — завантаження файлу в API

```go
func TestUpload(t *testing.T) {
    input := &New{
        File:  "echo-hereweare.mp4",  // ❌ Залежить від TestDownload!
        Title: "Echo Here We Are",
        Tags:  []string{"echo", "here", "we"},
    }
    response, err := input.Upload()
    // ... t.Fail() проблеми ...
}
```

#### ⚠️ Проблеми
```go
// ❌ Залежність між тестами:
// • Якщо TestDownload провалиться → TestUpload провалиться
// • Порядок виконання тестів стає критичним
// • Неможливо запустити окремий тест

// ✅ Рішення: кожен тест самостійний
func TestUpload(t *testing.T) {
    // 🎯 Створити тимчасовий файл для тесту
    tmpFile, err := os.CreateTemp("", "test_*.mp4")
    require.NoError(t, err)
    defer os.Remove(tmpFile.Name())
    
    // 🎯 Записати мінімальні дані (не потрібно реальне відео для тесту структури)
    tmpFile.Write([]byte("fake video data"))
    tmpFile.Close()
    
    input := &New{File: tmpFile.Name(), Title: "Test"}
    // ...
}
```

---

### 6️⃣ `TestSaveGif` — збереження результату

```go
func TestSaveGif(t *testing.T) {
    file := &New{Source: "https://www.youtube.com/watch?v=V6wrI6DEZFk"}
    response, err := file.Create()
    // ...
    gifFile := response.SaveGif()  // ❌ Завжди "newgif.gif" — конфлікт імен!
    if gifFile == "" {
        t.Fail()
    }
    // ❌ Файл не видаляється
}
```

#### ⚠️ Проблеми
```go
// ❌ Hardcoded ім'я файлу в SaveGif():
func (r *ImportResponse) SaveGif() string {
    file := DownloadFile("newgif.gif", r.Files.Gif)  // ❌ Завжди однакове ім'я!
    return file
}
// • Два паралельних тести → конфлікт запису
// • Файл перезатирається

// ✅ Правильно: приймати ім'я як параметр або генерувати унікальне
func (r *ImportResponse) SaveGif(filename string) (string, error) {
    if filename == "" {
        filename = fmt.Sprintf("gif_%d.gif", time.Now().UnixNano())
    }
    return DownloadFile(filename, r.Files.Gif), nil
}
```

---

### 7️⃣ `TestBulkUpload` — пакетне завантаження

```go
func TestBulkUpload(t *testing.T) {
    files := []New{
        {File: "echo-hereweare.mp4", Title: "New Video"},
        {File: "echo-hereweare.mp4", Title: "New Video 2"},  // ❌ Той самий файл!
        {File: "echo-hereweare.mp4", Title: "New Video 3"},
    }
    bulk := Bulk{New: files}
    response, err := bulk.Upload()
    // ...
    if len(response) != 3 {  // ❌ Очікує 3 успіхи, але помилки ігноруються в Upload()
        t.Fail()
    }
}
```

#### ⚠️ Проблеми
```go
// ❌ Bulk.Upload() ігнорує помилки (див. огляд основного коду):
// • Навіть якщо 2 з 3 файлів не завантажились → повертає 1 успіх + nil error
// • Тест перевіряє len(response) != 3 → провал, але незрозуміло чому

// ❌ Один і той самий файл тричі:
// • Не тестує реальну пакетну обробку різних файлів
// • Може впертися в rate limiting API

// ✅ Правильно:
func TestBulkUpload(t *testing.T) {
    // 🎯 Створити різні тимчасові файли
    var files []New
    for i := 0; i < 3; i++ {
        tmpFile, _ := os.CreateTemp("", "test_*.mp4")
        tmpFile.Write([]byte(fmt.Sprintf("video data %d", i)))
        tmpFile.Close()
        defer os.Remove(tmpFile.Name())
        
        files = append(files, New{
            File:  tmpFile.Name(),
            Title: fmt.Sprintf("Test Video %d", i),
        })
    }
    
    bulk := Bulk{New: files}
    responses, err := bulk.Upload()
    
    // 🎯 Перевірити, що всі успішні (або збирати помилки)
    require.NoError(t, err)
    assert.Len(t, responses, 3, "All 3 files should upload successfully")
}
```

---

## ⚠️ Загальні проблеми всіх тестів

### 1️⃣ Відсутність `t.Parallel()` для прискорення
```go
// ✅ Додати на початок кожного тесту:
func TestSimpleYoutube(t *testing.T) {
    t.Parallel()  // ✅ Дозволяє паралельне виконання
    // ...
}
// 📊 Ефект: 6 тестів × ~3с кожен → 18с послідовно → ~5с паралельно
```

### 2️⃣ Немає перевірки наявності API-ключа
```go
// ✅ Додати skip якщо AUTH не встановлено:
func TestSimpleYoutube(t *testing.T) {
    if os.Getenv("AUTH") == "" {
        t.Skip("AUTH environment variable not set, skipping integration test")
    }
    t.Parallel()
    // ...
}
```

### 3️⃣ Немає таймаутів на тест
```go
// ✅ Обмежити час виконання тесту:
func TestSimpleYoutube(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()
    
    // 🎯 Передати ctx у методи (потрібно змінити сигнатури)
    response, err := file.CreateWithContext(ctx)
    // ...
}
```

### 4️⃣ Використання `t.Fail()` замість `t.Fatalf`/`require`
```go
// ❌ Поточний патерн:
if err != nil {
    t.Fail()  // Тест продовжується, далі будуть паніки на nil pointer!
}

// ✅ Правильний патерн:
if err != nil {
    t.Fatalf("Create() failed: %v", err)  // Зупиняє тест негайно
}
// Або з testify:
require.NoError(t, err, "Create() should succeed")
```

### 5️⃣ Відсутність cleanup для завантажених файлів
```go
// ✅ Завжди видаляти тимчасові файли:
func TestDownload(t *testing.T) {
    filename := "test_download.mp4"
    defer os.Remove(filename)  // ✅ Гарантоване видалення навіть при провалі
    
    result := DownloadFile(filename, url)
    require.NotEmpty(t, result)
}
```

---

## 🔗 Рекомендації для рефакторингу

### 🎯 Рішення 1: Додати мок-сервер для стабільних тестів

```go
// 📁 mocks_test.go
func createMockServer(t *testing.T) *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        switch r.URL.Path {
        case "/media/import":
            // 🎯 Mock відповідь для Create()
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            w.Write([]byte(`{
                "success": {
                    "page": "https://gifs.com/test",
                    "files": {
                        "gif": "https://cdn.gifs.com/test.gif",
                        "jpg": "https://cdn.gifs.com/test.jpg",
                        "mp4": "https://cdn.gifs.com/test.mp4"
                    },
                    "embed": "<iframe>...</iframe>",
                    "oembed": "https://gifs.com/oembed/test"
                }
            }`))
            
        case "/media/upload":
            // 🎯 Mock для multipart upload
            // Перевірити, що файл дійсно надіслано
            err := r.ParseMultipartForm(10 << 20)  // 10MB max
            require.NoError(t, err)
            
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusOK)
            w.Write([]byte(`{"success":{"page":"https://gifs.com/uploaded"}}`))
            
        default:
            w.WriteHeader(http.StatusNotFound)
        }
    }))
}
```

### 🎯 Рішення 2: Рефакторинг тестів з використанням testify

```go
// ✅ Приклад рефакторингу TestSimpleYoutube:
func TestSimpleYoutube(t *testing.T) {
    if os.Getenv("AUTH") == "" {
        t.Skip("AUTH not set, skipping integration test")
    }
    t.Parallel()
    
    file := &New{Source: "https://www.youtube.com/watch?v=V6wrI6DEZFk"}
    
    t.Run("Create", func(t *testing.T) {
        response, err := file.Create()
        require.NoError(t, err, "Create() should not return error")
        require.NotNil(t, response, "Response should not be nil")
        
        assert.NotEmpty(t, response.Files.Gif, "GIF URL should be present")
        assert.NotEmpty(t, response.Embed, "Embed code should be present")
        assert.NotEmpty(t, response.Page, "Page URL should be present")
    })
    
    t.Run("ResponseFields", func(t *testing.T) {
        response, _ := file.Create()  // Ігноруємо помилку, бо перевіряємо структуру
        
        // 🎯 Перевірка, що всі очікувані поля присутні
        fields := map[string]string{
            "Gif":  response.Files.Gif,
            "Jpg":  response.Files.Jpg,
            "Mp4":  response.Files.Mp4,
            "Page": response.Page,
        }
        for name, value := range fields {
            t.Logf("%s URL: %s", name, value)
            // Не fail-имо, якщо поле опціональне
        }
    })
}
```

### 🎯 Рішення 3: Додати helper-функції для повторного використання

```go
// 📁 helpers_test.go
func requireAuth(t *testing.T) {
    t.Helper()
    if os.Getenv("AUTH") == "" {
        t.Skip("AUTH environment variable required for integration tests")
    }
}

func withTempFile(t *testing.T, content []byte) string {
    t.Helper()
    tmpFile, err := os.CreateTemp(t.TempDir(), "test_*.mp4")
    require.NoError(t, err)
    
    _, err = tmpFile.Write(content)
    require.NoError(t, err)
    require.NoError(t, tmpFile.Close())
    
    return tmpFile.Name()  // os.Remove не потрібен: t.TempDir() очищує автоматично
}

func assertValidImportResponse(t *testing.T, resp *ImportResponse) {
    t.Helper()
    require.NotNil(t, resp)
    // Хоча б одне з посилань має бути
    assert.True(t, 
        resp.Files.Gif != "" || resp.Files.Mp4 != "" || resp.Files.Webm != "",
        "At least one media format should be present")
}
```

### 🎯 Рішення 4: Додати тест на помилки (negative testing)

```go
func TestCreate_InvalidInput(t *testing.T) {
    t.Parallel()
    
    tests := []struct{
        name     string
        input    *New
        wantErr  bool
        errContains string
    }{
        {
            name: "EmptySourceAndFile",
            input: &New{},
            wantErr: true,
            errContains: "Source or File",
        },
        {
            name: "InvalidYouTubeURL",
            input: &New{Source: "https://youtube.com/invalid"},
            wantErr: true,  // Залежить від поведінки API
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            resp, err := tt.input.Create()
            
            if tt.wantErr {
                assert.Error(t, err)
                if tt.errContains != "" {
                    assert.Contains(t, err.Error(), tt.errContains)
                }
                assert.Nil(t, resp)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, resp)
            }
        })
    }
}
```

---

## 🧪 Приклад: повністю рефакторинг одного тесту

```go
// ✅ TestDownload після рефакторингу:
func TestDownload(t *testing.T) {
    t.Parallel()
    
    const testURL = "https://raw.githubusercontent.com/mediaelement/mediaelement-files/master/echo-hereweare.mp4"
    filename := filepath.Join(t.TempDir(), "download_test.mp4")  // ✅ Ізольована директорія
    
    result := DownloadFile(filename, testURL)
    
    // 🎯 Перевірка результату
    assert.NotEmpty(t, result, "DownloadFile should return filename on success")
    assert.Equal(t, filename, result, "Returned filename should match input")
    
    // 🎯 Перевірка, що файл існує та має контент
    info, err := os.Stat(filename)
    require.NoError(t, err, "Downloaded file should exist")
    assert.Greater(t, info.Size(), int64(0), "Downloaded file should not be empty")
    
    // 🎯 Перевірка, що це дійсно MP4 (опціонально)
    file, err := os.Open(filename)
    require.NoError(t, err)
    defer file.Close()
    
    header := make([]byte, 4)
    _, err = file.Read(header)
    require.NoError(t, err)
    // MP4 файли часто починаються з [0x00,0x00,0x00,0x18] або подібного
    // Не перевіряємо суворо, бо формат може відрізнятися
}
```

---

## 📋 Best Practices для інтеграційних тестів

```
✅ Ізоляція:
   • Кожен тест самодостатній (не залежить від інших)
   • Використовувати t.TempDir() для тимчасових файлів
   • Генерувати унікальні імена файлів

✅ Стабільність:
   • Додати t.Skip() якщо немає AUTH
   • Встановити таймаути на тест/запит
   • Використовувати мок-сервер для критичних шляхів

✅ Читабельність:
   • Використовувати testify assert/require замість t.Fail()
   • Додавати зрозумілі повідомлення про помилки
   • Групувати перевірки через t.Run()

✅ Очищення:
   • defer os.Remove() для завантажених файлів
   • Використовувати t.TempDir() для автоматичного очищення
   • Не залишати стан між тестами

✅ Паралелізм:
   • Додати t.Parallel() де можливо
   • Уникати спільних ресурсів (файли, глобальні змінні)
   • Тестувати race condition: go test -race

✅ Документація:
   • Коментувати, що саме тестує кожен кейс
   • Вказувати залежності (API, зовнішні сервіси)
   • Додавати приклади очікуваних відповідей
```

---

## 🎯 Висновок

Ці тести — **функціональні, але нестабільні** інтеграційні перевірки:

✅ Покривають основні сценарії використання пакету  
✅ Перевіряють реальну взаємодію з зовнішнім API  
✅ Використовують реальні дані (YouTube, GitHub)

**Критичні виправлення перед використанням в CI/CD**:

1. ✅ **Додати `t.Skip()`** якщо `AUTH` не встановлено
2. ✅ **Замінити `t.Fail()` на `t.Fatalf`/`require`** з повідомленнями
3. ✅ **Додати `defer os.Remove()`** або використовувати `t.TempDir()`
4. ✅ **Додати `t.Parallel()`** для прискорення виконання
5. ✅ **Встановити таймаути** на тести та HTTP-запити
6. ✅ **Додати мок-сервер** для стабільних юніт-тестів
7. ✅ **Розділити тести** на інтеграційні (потребують API) та юніт (моки)

**Приклад міграції на стабільні тести**:
```go
// 🎯 Стратегія: 80% юніт-тестів з моками + 20% інтеграційних з skip
// 
// 1. Створити інтерфейс для HTTP-клієнта:
type HTTPClient interface {
    Do(req *http.Request) (*http.Response, error)
}

// 2. Ін'єктувати клієнт у методи:
func (i *New) CreateWithClient(ctx context.Context, client HTTPClient) (*ImportResponse, error) {
    // Використовувати client замість http.DefaultClient
}

// 3. Юніт-тести з мок-клієнтом:
func TestCreate_Unit(t *testing.T) {
    mockClient := &MockHTTPClient{
        DoFunc: func(req *http.Request) (*http.Response, error) {
            // Повернути мок-відповідь
        },
    }
    resp, err := file.CreateWithClient(ctx, mockClient)
    // Тестуємо логіку без мережі
}

// 4. Інтеграційні тести з skip:
func TestCreate_Integration(t *testing.T) {
    if os.Getenv("AUTH") == "" {
        t.Skip("Skipping integration test: AUTH not set")
    }
    // Реальний запит до API
}
```

Потрібно допомогти з:
- 🔗 Рефакторингом пакету для ін'єкції залежностей (HTTP-клієнт, logger)?
- 🧠 Написанням мок-сервера для стабільних юніт-тестів?
- 🧪 Налаштуванням CI/CD пайплайну з розділенням юніт/інтеграційних тестів?

Чекаю на ваші питання! 🛠️🧪✨