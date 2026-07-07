# 🧪 Тест `TestTranscoder`: Перевірка налаштування `-protocol_whitelist`

Це **юніт-тест** для пакету `transcoder` бібліотеки `goffmpeg`, який перевіряє коректність роботи методу `SetWhiteListProtocols` — додавання опції `-protocol_whitelist` до команди FFmpeg для контролю дозволених мережевих протоколів.

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що бібліотека коректно додає `-protocol_whitelist` тільки коли це потрібно**: без списку → опція відсутня, зі списком → опція додається на початок команди з правильно відформатованим рядком протоколів.

---

## 📋 Структура тесту

```go
func TestTranscoder(t *testing.T) {
    // 🔹 Група тестів для функціоналу SetWhiteListProtocols
    t.Run("#SetWhiteListProtocols", func(t *testing.T) {
        
        // 🔹 ТЕСТ 1: Без встановленого whitelist → опція НЕ повинна з'явитися
        t.Run("Should not set -protocol_whitelist option if it isn't present", func(t *testing.T) {
            ts := Transcoder{}                    // 🔹 Створюємо порожній транскодер
            ts.SetMediaFile(&media.File{})        // 🔹 Встановлюємо порожній media.File
            
            // 🔹 Перевірка: перші два елементи НЕ дорівнюють ["-protocol_whitelist", "..."]
            require.NotEqual(t, ts.GetCommand()[0:2], 
                []string{"-protocol_whitelist", "file,http,https,tcp,tls"})
            
            // 🔹 Перевірка: команда НЕ містить рядок "protocol_whitelist" взагалі
            require.NotContains(t, ts.GetCommand(), "protocol_whitelist")
        })

        // 🔹 ТЕСТ 2: З встановленим whitelist → опція ПОВИННА з'явитися
        t.Run("Should set -protocol_whitelist option if it's present", func(t *testing.T) {
            ts := Transcoder{}
            ts.SetMediaFile(&media.File{})
            
            // 🔹 Встановлюємо список дозволених протоколів
            ts.SetWhiteListProtocols([]string{"file", "http", "https", "tcp", "tls"})
            
            // 🔹 Перевірка: перші два елементи ДОРІВНЮЮТЬ очікуваним
            require.Equal(t, ts.GetCommand()[0:2], 
                []string{"-protocol_whitelist", "file,http,https,tcp,tls"})
        })
    })
}
```

**🎯 Призначення**: Перевірити, що `GetCommand()`:
- ✅ Не додає `-protocol_whitelist`, якщо список порожній/не встановлений
- ✅ Додає `-protocol_whitelist` на **початок** команди, коли список встановлено
- ✅ Правильно форматує список: `[]string{"file","http"}` → `"file,http"`

---

## 🔍 Детальний розбір логіки

### 🔹 Як працює `GetCommand()` (з попереднього аналізу)

```go
func (t Transcoder) GetCommand() []string {
    media := t.mediafile
    // 🔹 Базова команда: -y + параметри з media.ToStrCommand()
    rcommand := append([]string{"-y"}, media.ToStrCommand()...)
    
    // 🔹 Якщо whitelist встановлено → додаємо на ПОЧАТОК команди
    if t.whiteListProtocols != nil {
        rcommand = append([]string{
            "-protocol_whitelist", 
            strings.Join(t.whiteListProtocols, ","),  // 🔹 "file,http,https,tcp,tls"
        }, rcommand...)  // 🔹 Важливо: додаємо ПЕРЕД rcommand, не після!
    }
    
    return rcommand
}
```

**🔄 Потік даних:**
```
🔹 Сценарій 1: whiteListProtocols = nil
   • rcommand = ["-y", ...параметри з media.ToStrCommand()...]
   • Умова if false → пропускаємо
   • Вихід: ["-y", "-c:v", "libx264", "-i", "input.mp4", "output.mp4"]

🔹 Сценарій 2: whiteListProtocols = ["file","http","https","tcp","tls"]
   • rcommand = ["-y", ...]
   • Умова if true → 
     strings.Join(...) = "file,http,https,tcp,tls"
     rcommand = append(["-protocol_whitelist", "file,http,https,tcp,tls"], rcommand...)
   • Вихід: ["-protocol_whitelist", "file,http,https,tcp,tls", "-y", ...]
```

**🎯 Ключовий момент**: `append([]string{...}, rcommand...)` додає нові елементи **на початок**, що критично для FFmpeg — опції мають йти перед `-i` та вихідним файлом.

---

## 🔍 Чому `-protocol_whitelist` важливий для CCTV?

### 🔹 Проблема: Безпека мережевих протоколів у FFmpeg

FFmpeg за замовчуванням **блокує небезпечні протоколи** (наприклад, `file://` у поєднанні з мережевими вхідними даними) для запобігання:
- ❌ SSRF-атакам (Server-Side Request Forgery)
- ❌ Доступу до локальних файлів через мережеві входи
- ❌ Виконання небажаних операцій через маніпуляцію шляхами

### 🔹 Рішення: Явний дозвіл протоколів

```bash
# 🔹 Без whitelist (FFmpeg може відхилити):
$ ffmpeg -i rtsp://camera/stream output.mp4
# ⚠️  [rtsp @ 0x...] Protocol not whitelisted!

# 🔹 З whitelist (явний дозвіл):
$ ffmpeg -protocol_whitelist file,http,https,tcp,tls,rtsp,crypto \
    -i rtsp://camera/stream output.mp4
# ✅ Успішне відкриття потоку
```

**🎯 Для CCTV HLS Processor це означає:**
- ✅ Безпечна робота з RTSP/HTTP/HTTPS камерами
- ✅ Контроль над дозволеними протоколами (напр. заборона `file://` для мережевих входів)
- ✅ Сумісність з політиками безпеки enterprise-середовищ

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Безпечна ініціалізація транскодера для мережевих камер

```go
func NewCameraTranscoder(rtspURL string) (*transcoder.Transcoder, error) {
    trans := &transcoder.Transcoder{}
    
    // 🔹 Ініціалізація без ffprobe (для live-стрімів)
    if err := trans.InitializeEmptyTranscoder(); err != nil {
        return nil, err
    }
    
    // 🔹 Налаштування входу
    file := trans.MediaFile()
    file.SetInputPath(rtspURL)
    file.SetRtmpLive("live")  // Оптимізація для live
    
    // 🔹 Явний дозвіл протоколів для RTSP
    trans.SetWhiteListProtocols([]string{
        "file", "http", "https", "tcp", "tls", 
        "rtsp", "rtp", "udp", "crypto",  // 🔹 Додаткові для RTSP
    })
    
    // 🔹 Налаштування виходу (HLS)
    file.SetOutputFormat("hls")
    file.SetHlsSegmentDuration(4)
    file.SetHlsListSize(10)
    
    return trans, nil
}
```

---

### 🔹 Приклад 2: Динамічний whitelist на основі типу входу

```go
func BuildProtocolWhitelist(inputURL string) []string {
    base := []string{"file", "http", "https", "tcp", "tls"}
    
    // 🔹 Аналіз схеми URL
    if strings.HasPrefix(inputURL, "rtsp://") || strings.HasPrefix(inputURL, "rtsps://") {
        return append(base, "rtsp", "rtp", "udp", "crypto", "srtp")
    }
    if strings.HasPrefix(inputURL, "srt://") {
        return append(base, "srt", "udp")
    }
    if strings.HasPrefix(inputURL, "http://") || strings.HasPrefix(inputURL, "https://") {
        return append(base, "http", "https", "tls")
    }
    
    // 🔹 Для локальних файлів — тільки file
    return []string{"file"}
}

// 🔹 Використання:
whitelist := BuildProtocolWhitelist("rtsp://192.168.1.100/stream")
trans.SetWhiteListProtocols(whitelist)
```

---

### 🔹 Приклад 3: Тестування безпеки через unit-тести

```go
func TestProtocolWhitelistSecurity(t *testing.T) {
    tests := []struct {
        name           string
        inputURL       string
        expectedProtos []string
        shouldContain  []string
        shouldNotContain []string
    }{
        {
            name:     "RTSP camera",
            inputURL: "rtsp://camera/stream",
            expectedProtos: []string{"file", "http", "https", "tcp", "tls", "rtsp", "rtp"},
            shouldContain:  []string{"-protocol_whitelist", "rtsp"},
            shouldNotContain: []string{"smb", "ftp"},  // 🔹 Небезпечні протоколи
        },
        {
            name:     "Local file",
            inputURL: "/var/recordings/cam1.mp4",
            expectedProtos: []string{"file"},
            shouldContain:  []string{"-protocol_whitelist", "file"},
            shouldNotContain: []string{"http", "rtsp"},  // 🔹 Не потрібні для локального файлу
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            trans := &transcoder.Transcoder{}
            trans.InitializeEmptyTranscoder()
            trans.MediaFile().SetInputPath(tt.inputURL)
            trans.SetWhiteListProtocols(tt.expectedProtos)
            
            cmd := trans.GetCommand()
            
            for _, expected := range tt.shouldContain {
                require.Contains(t, cmd, expected, "command should contain %s", expected)
            }
            for _, forbidden := range tt.shouldNotContain {
                require.NotContains(t, cmd, forbidden, "command should NOT contain %s", forbidden)
            }
        })
    }
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Додавання `-protocol_whitelist` у кінець команди | FFmpeg ігнорує опцію або видає помилку | Завжди додавайте опції **перед** `-i` та вихідним файлом (як у `GetCommand()`) |
| Неправильне форматування списку | `"file http https"` замість `"file,http,https"` | Використовуйте `strings.Join(protos, ",")` |
| Відсутність перевірки на nil | Паніка при `whiteListProtocols == nil` | Перевіряйте `if t.whiteListProtocols != nil` перед використанням |
| Надмірний whitelist | Дозвіл небезпечних протоколів (`smb`, `ftp`) | Дозволяйте тільки необхідні для конкретного сценарію |
| Ігнорування `crypto`/`srtp` для RTSPS | Помилка підключення до зашифрованих камер | Додавайте `crypto`, `srtp`, `tls` для `rtsps://` URL |

---

## 📋 Чекліст для вашого проекту

```
[ ] При налаштуванні whitelist:
    • Дозволяйте тільки необхідні протоколи для конкретного входу
    • Для RTSP: додавайте rtsp, rtp, udp, crypto, srtp
    • Для HTTP/HTTPS: достатньо http, https, tcp, tls
    • Для локальних файлів: тільки file

[ ] Для безпеки:
    • Ніколи не дозволяйте smb://, ftp://, gopher:// без крайньої потреби
    • Валідуйте вхідні URL перед додаванням у whitelist
    • Логувайте використані протоколи для аудиту

[ ] Для тестування:
    • Покрийте кейси: порожній whitelist, один протокол, багато протоколів
    • Перевіряйте порядок аргументів: -protocol_whitelist має йти ПЕРЕД -i
    • Тестуйте інтеграцію з реальними URL: rtsp://, http://, file://

[ ] Для дебагу:
    • Логувайте повну команду: log.Printf("🔧 FFmpeg: %v", trans.GetCommand())
    • Перевіряйте, що protocol_whitelist містить очікувані протоколи
    • Тестуйте з різними типами входів: локальні файли, мережеві потоки, pipe

[ ] Для документації:
    • Документуйте необхідні протоколи для кожного типу камери
    • Надавайте приклади конфігурації для RTSP, HTTP, локальних файлів
    • Попереджайте про ризики надмірного whitelist
```

---

## 🎯 Висновок

> **Цей тест — ваш "страж безпеки" для мережевих протоколів у FFmpeg**, який гарантує:
> • ✅ Коректне додавання `-protocol_whitelist` тільки коли це потрібно
> • ✅ Правильне форматування списку протоколів через `strings.Join`
> • ✅ Правильний порядок аргументів: опція на початку команди
> • ✅ Запобігання помилкам через чіткі assert-перевірки
> • ✅ Підтримку безпеки: контроль над дозволеними протоколами

Для вашого **CCTV HLS Processor** це означає:
- 🔐 Безпечна робота з різними типами камер (RTSP, HTTP, локальні файли)
- 🎯 Гнучке налаштування протоколів під конкретний сценарій
- 🛡️ Захист від SSRF та інших атак через обмеження протоколів
- 🧪 Надійне тестування безпеки через unit-тести
- 📋 Прозора документація дозволених протоколів для аудиту

Потребуєте допомоги з налаштуванням динамічного whitelist для різних типів камер або з інтеграцією тестів безпеки у ваш CI/CD? Напишіть — покажу готовий код для вашого сценарію! 🚀🔐