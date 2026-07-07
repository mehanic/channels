# 🔐 `psshdump`: Інструмент для витягування DRM-метаданих (PSSH) з MP4

Це **спеціалізований CLI-інструмент** на основі бібліотеки `go-mp4`, який знаходить та виводить **PSSH (Protection System Specific Header) бокси** з MP4/fMP4 файлів — критично для аналізу та налаштування DRM-захисту (Widevine, PlayReady, FairPlay).

---

## 🎯 Коротка відповідь

> **Це "декодер DRM-метаданих" для MP4**: він знаходить усі PSSH бокси у файлі, виводить їхні параметри (SystemID, версія, прапорці) та надає Base64-кодовані сирі дані для інтеграції з DRM-серверами ліцензування.

---

## 🗂️ Структура інструменту

```bash
# 🔹 Базовий синтаксис:
$ mp4tool psshdump INPUT.mp4

# 🔹 Приклад виводу:
0:
  offset: 1024
  size: 64
  version: 0
  flags: 0x0
  systemId: eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1  # ← Widevine
  dataSize: 32
  base64: "AAAAHHBzc2gAAAAA7e+LZzQ..."  # ← Сирі дані для ліцензійного сервера
```

---

## 🧱 Основні компоненти

### 🔹 Головний вхід: `Main()`

```go
func Main(args []string) int {
    // 🔹 Перевірка аргументів
    if len(args) < 1 {
        println("USAGE: mp4tool psshdump INPUT.mp4")
        return 1
    }
    
    // 🔹 Виклик основної логіки
    if err := dump(args[0]); err != nil {
        fmt.Println("Error:", err)
        return 1
    }
    return 0
}
```

**🎯 Призначення**: Забезпечити простий CLI-інтерфейс з валідацією вхідних даних.

---

### 🔹 Основна логіка: `dump()`

```go
func dump(inputFilePath string) error {
    // 🔹 Відкриття файлу
    inputFile, err := os.Open(inputFilePath)
    if err != nil { return err }
    defer inputFile.Close()
    
    // 🔹 Буферизований читач (blockSize=1KB для швидкого сканування)
    r := bufseekio.NewReadSeeker(inputFile, 1024, 4)
    
    // 🔹 Крок 1: Пошук усіх PSSH боксів у файлі
    bs, err := mp4.ExtractBoxesWithPayload(r, nil, []mp4.BoxPath{
        {mp4.BoxTypeMoov(), mp4.BoxTypePssh()},  // 🔹 PSSH у moov (ініціалізація)
        {mp4.BoxTypeMoof(), mp4.BoxTypePssh()},  // 🔹 PSSH у moof (фрагменти)
    })
    if err != nil { return err }
    
    // 🔹 Крок 2: Обробка кожного знайденого PSSH
    for i := range bs {
        pssh := bs[i].Payload.(*mp4.Pssh)  // 🔹 Type assertion до *Pssh
        
        // 🔹 Форматування SystemID у UUID-рядок
        var sysid string
        for i, v := range pssh.SystemID {
            sysid += fmt.Sprintf("%02x", v)
            if i == 3 || i == 5 || i == 7 || i == 9 {  // 🔹 Додавання '-' для формату UUID
                sysid += "-"
            }
        }
        
        // 🔹 Читання сирих байтів усього боксу
        if _, err := bs[i].Info.SeekToStart(r); err != nil { return err }
        rawData := make([]byte, bs[i].Info.Size)
        if _, err := io.ReadFull(r, rawData); err != nil { return err }
        
        // 🔹 Вивід структурованої інформації
        fmt.Printf("%d:\n", i)
        fmt.Printf("  offset: %d\n", bs[i].Info.Offset)
        fmt.Printf("  size: %d\n", bs[i].Info.Size)
        fmt.Printf("  version: %d\n", pssh.Version)
        fmt.Printf("  flags: 0x%x\n", pssh.Flags)
        fmt.Printf("  systemId: %s\n", sysid)
        fmt.Printf("  dataSize: %d\n", pssh.DataSize)
        fmt.Printf("  base64: \"%s\"\n", base64.StdEncoding.EncodeToString(rawData))
        fmt.Println()
    }
    
    return nil
}
```

**🔄 Потік даних:**
```
🔹 Вхід: шлях до файлу
│
▼
🔹 ExtractBoxesWithPayload():
   • Пошук за шляхами: moov→pssh, moof→pssh
   • Повертає: [] *BoxInfoWithPayload з розпаршеними Pssh структурами
│
▼
🔹 Для кожного PSSH:
   • Форматування SystemID у UUID-рядок
   • Читання сирих байтів усього боксу
   • Вивід: offset, size, version, flags, systemId, dataSize, base64
│
▼
🔹 Вихід: структурований текст у stdout
```

---

## 🔍 PSSH бокс: Що це і навіщо потрібен?

### 🔹 Структура PSSH (ISO/IEC 23001-7)

```
📦 PSSH Box (Protection System Specific Header):
├── size: uint32          # 🔹 Розмір боксу
├── type: "pssh"          # 🔹 Тип боксу
├── version: uint8        # 🔹 Версія: 0 або 1
├── flags: uint24         # 🔹 Прапорці
├── SystemID: [16]byte    # 🔹 UUID системи захисту:
│   • 🟢 Widevine:    eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1
│   • 🔵 PlayReady: 9a04f079-9840-4286-ab92-e65be0885f95
│   • 🔴 FairPlay:  94ce86fb-0790-49ed-b8e4-8d397b47f985
├── DataSize: uint32      # 🔹 Розмір специфічних даних
└── Data: []byte          # 🔹 Сирі дані для DRM-сервера
```

**🎯 Призначення**: Надати клієнту **необхідну інформацію** для отримання ліцензії на відтворення зашифрованого контенту.

---

### 🔹 Форматування SystemID у UUID

```go
var sysid string
for i, v := range pssh.SystemID {
    sysid += fmt.Sprintf("%02x", v)  // 🔹 Кожний байт у hex
    if i == 3 || i == 5 || i == 7 || i == 9 {  // 🔹 Позиції для '-'
        sysid += "-"  // 🔹 Формат: 8-4-4-4-12
    }
}
```

**🔢 Приклад:**
```
🔹 Вхід: [16]byte{0xee,0xd8,0x77,0x50, 0x3c,0x00, 0x4d,0x7c, 0xb2,0xa1, 0x39,0xb0,0xb6,0xf0,0xb2,0xa1}

🔹 Вихід: "eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1"
           └──8──┘└4┘└4┘└4┘└────12────┘
```

**🎯 Призначення**: Перетворити сирі байти у **стандартний UUID-рядок** для зручної ідентифікації DRM-системи.

---

### 🔹 Base64-кодування сирих даних

```go
fmt.Printf("  base64: \"%s\"\n", base64.StdEncoding.EncodeToString(rawData))
```

**🎯 Призначення**: Надати **машино-читабельне представлення** сирих даних боксу для:
- ✅ Передачі у DRM-ліцензійні сервери (Widevine, PlayReady)
- ✅ Збереження у конфігураціях без проблем з кодуванням
- ✅ Інтеграції з веб-інтерфейсами та API

**🔢 Приклад:**
```
🔹 Сирі дані: [0x00, 0x00, 0x00, 0x1c, 0x70, 0x73, 0x73, 0x68, ...]
🔹 Base64: "AAAAHHBzc2gAAAAA7e+LZzQ..."
```

---

## 🔍 Відомі SystemID для DRM-систем

| Система | SystemID (UUID) | Платформи |
|---------|----------------|-----------|
| 🟢 **Widevine** | `eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1` | Android, Chrome, Firefox, Smart TV |
| 🔵 **PlayReady** | `9a04f079-9840-4286-ab92-e65be0885f95` | Windows, Xbox, Edge, Smart TV |
| 🔴 **FairPlay** | `94ce86fb-0790-49ed-b8e4-8d397b47f985` | iOS, macOS, Safari, Apple TV |
| 🟡 **ClearKey** | `1077efec-c0b2-4d02-ace3-3c1e52e2fb4b` | Тестування, EME Clear Key |

**🎯 Призначення**: Швидко ідентифікувати, яка DRM-система використовується у файлі.

---

## 🛠️ Практичне використання

### 🔹 Приклад 1: Отримання PSSH для Widevine-ліцензії

```bash
# 🔹 Витягнути PSSH з файлу:
$ mp4tool psshdump encrypted_video.mp4

# 🔹 Приклад виводу:
0:
  offset: 1024
  size: 64
  version: 0
  flags: 0x0
  systemId: eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1  # ← Widevine
  dataSize: 32
  base64: "AAAAHHBzc2gAAAAA7e+LZzQ..."

# 🔹 Використання base64 для запиту ліцензії:
$ curl -X POST https://license.widevine.com \
  -H "Content-Type: application/json" \
  -d '{"pssh": "AAAAHHBzc2gAAAAA7e+LZzQ..."}'
```

---

### 🔹 Приклад 2: Перевірка підтримки DRM на різних платформах

```bash
# 🔹 Скрипт для перевірки DRM-сумісності:
#!/bin/bash
file="$1"
echo "🔐 Аналіз DRM у: $file"

mp4tool psshdump "$file" | grep -E "systemId:|base64:" | while read line; do
    if [[ $line == *"systemId:"* ]]; then
        sysid=$(echo "$line" | awk '{print $2}')
        case "$sysid" in
            eed87750-*) echo "  🟢 Widevine: $sysid" ;;
            9a04f079-*) echo "  🔵 PlayReady: $sysid" ;;
            94ce86fb-*) echo "  🔴 FairPlay: $sysid" ;;
            *) echo "  ⚪ Unknown: $sysid" ;;
        esac
    elif [[ $line == *"base64:"* ]]; then
        b64=$(echo "$line" | sed 's/.*"\(.*\)".*/\1/')
        echo "  📦 Base64 length: ${#b64} chars"
    fi
done
```

**🔹 Приклад виводу:**
```
🔐 Аналіз DRM у: encrypted_video.mp4
  🟢 Widevine: eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1
  📦 Base64 length: 88 chars
  🔵 PlayReady: 9a04f079-9840-4286-ab92-e65be0885f95
  📦 Base64 length: 124 chars
```

---

### 🔹 Приклад 3: Інтеграція у CCTV HLS Processor для DRM-підтримки

```go
// 🔹 Структура для зберігання PSSH-метаданих
type DRMConfig struct {
    SystemID string `json:"system_id"`  // 🔹 UUID системи
    Version  uint8  `json:"version"`    // 🔹 Версія PSSH
    Flags    uint32 `json:"flags"`      // 🔹 Прапорці
    Base64   string `json:"base64"`     // 🔹 Сирі дані для сервера
}

// 🔹 Функція для витягування PSSH з сегмента
func extractDRMConfig(segmentPath string) ([]DRMConfig, error) {
    // 🔹 Запускаємо psshdump як підпроцес
    cmd := exec.Command("mp4tool", "psshdump", segmentPath)
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("psshdump failed: %w", err)
    }
    
    // 🔹 Парсинг виводу (спрощено)
    var configs []DRMConfig
    lines := strings.Split(string(output), "\n")
    
    var current *DRMConfig
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" {
            if current != nil {
                configs = append(configs, *current)
                current = nil
            }
            continue
        }
        
        parts := strings.SplitN(line, ":", 2)
        if len(parts) != 2 { continue }
        
        key := strings.TrimSpace(parts[0])
        value := strings.TrimSpace(parts[1])
        
        switch key {
        case "systemId":
            current = &DRMConfig{SystemID: value}
        case "version":
            if current != nil {
                v, _ := strconv.Atoi(value)
                current.Version = uint8(v)
            }
        case "flags":
            if current != nil {
                v, _ := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 32)
                current.Flags = uint32(v)
            }
        case "base64":
            if current != nil {
                current.Base64 = strings.Trim(value, `"`)
            }
        }
    }
    
    return configs, nil
}

// 🔹 Використання у конвеєрі:
go func() {
    for segment := range segmentQueue {
        drmConfigs, err := extractDRMConfig(segment.Path)
        if err != nil {
            log.Printf("⚠️  No DRM config in %s: %v", segment.Path, err)
            continue
        }
        
        // 🔹 Додавання DRM-метаданих у плейлист
        for _, cfg := range drmConfigs {
            playlist.WriteString(fmt.Sprintf(
                "#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"license?system_id=%s\",KEYFORMAT=\"%s\"\n",
                cfg.SystemID,
                drmKeyFormat(cfg.SystemID),  // 🔹 "urn:uuid:..." для відповідної системи
            ))
        }
    }
}()

// 🔹 Допоміжна функція для формату KEYFORMAT
func drmKeyFormat(systemID string) string {
    switch systemID {
    case "eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1":
        return "urn:uuid:eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1"  // Widevine
    case "9a04f079-9840-4286-ab92-e65be0885f95":
        return "com.microsoft.playready"  // PlayReady
    case "94ce86fb-0790-49ed-b8e4-8d397b47f985":
        return "com.apple.streamingkeydelivery"  // FairPlay
    default:
        return "urn:uuid:" + systemID
    }
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильне форматування UUID | SystemID не розпізнається сервером | Дотримуйтесь формату 8-4-4-4-12 з дефісами на позиціях 3,5,7,9 |
| Забути Base64-кодування | Сирі байти не передаються коректно через JSON/API | Завжди використовуйте `base64.StdEncoding.EncodeToString()` |
| Ігнорування версії PSSH | Несумісність з клієнтами, що очікують v1 | Перевіряйте `pssh.Version`: v0 = 32-бітні поля, v1 = 64-бітні |
| Пошук PSSH тільки у moov | Пропуск фрагментованих PSSH у moof | Шукайте за обома шляхами: `moov→pssh` та `moof→pssh` |
| Неправильне читання сирих даних | Пошкоджені дані для ліцензійного сервера | Використовуйте `io.ReadFull()` для гарантованого читання всього буфера |

---

## 📋 Чекліст для вашого проекту

```
[ ] При витягуванні PSSH:
    • Шукайте у обох місцях: moov (ініціалізація) та moof (фрагменти)
    • Перевіряйте SystemID для ідентифікації DRM-системи
    • Завжди кодуйте сирі дані у Base64 для передачі

[ ] Для інтеграції з DRM-серверами:
    • Використовуйте стандартні UUID для system_id у плейлистах
    • Передавайте base64-дані у запиті ліцензії
    • Обробляйте різні версії PSSH (v0/v1) для сумісності

[ ] Для валідації захищених файлів:
    • Перевіряйте наявність PSSH перед спробою відтворення
    • Логувайте SystemID для моніторингу DRM-використання
    • Попереджайте про відсутність PSSH у зашифрованих сегментах

[ ] Для дебагу:
    • Порівнюйте SystemID з відомими UUID DRM-систем
    • Перевіряйте dataSize: має співпадати з довжиною Data
    • Тестуйте base64-декодування: чи відновлюються сирі байти

[ ] Для безпеки:
    • Не логуйте base64-дані у продакшені (можуть містити чутливу інформацію)
    • Валідуйте SystemID перед передачею у зовнішні системи
    • Обмежуйте доступ до psshdump-виводу для неавторизованих користувачів
```

---

## 🎯 Висновок

> **`psshdump` — це "ключ" до світу DRM у MP4**, який забезпечує:
> • ✅ Швидкий пошук PSSH боксів у будь-якому місці файлу (moov/moof)
> • ✅ Людсько-читабельне форматування SystemID у стандартний UUID
> • ✅ Base64-кодування сирих даних для інтеграції з ліцензійними серверами
> • ✅ Структурований вивід для автоматизації та скриптів
> • ✅ Підтримку всіх основних DRM-систем: Widevine, PlayReady, FairPlay

Для вашого **CCTV HLS Processor** це означає:
- 🔐 Легка інтеграція DRM-захисту через витягування PSSH для ліцензійних запитів
- 🌐 Сумісність з усіма основними платформами: Android, iOS, Windows, Web
- ⚡ Швидка валідація захищених записів перед публікацією у стрімінг
- 🔄 Автоматична генерація #EXT-X-KEY директив для HLS-плейлистів
- 🛡️ Безпечна обробка DRM-метаданих без розкриття чутливої інформації

Потребуєте допомоги з інтеграцією `psshdump` у ваш конвеєр DRM-підтримки або з налаштуванням взаємодії з ліцензійними серверами? Напишіть — покажу готовий код для вашого сценарію! 🚀🔐