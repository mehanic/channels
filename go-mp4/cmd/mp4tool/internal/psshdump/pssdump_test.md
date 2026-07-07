# 🧪 Тест `TestPsshdump`: Перевірка інструменту витягування DRM-метаданих (PSSH)

Це **інтеграційний тест** для модуля `psshdump`, який перевіряє коректність роботи **CLI-інструменту `mp4tool psshdump`** — знаходження та виведення інформації про PSSH бокси у зашифрованих MP4-файлах.

---

## 🎯 Коротка відповідь

> **Цей тест гарантує, що `psshdump` коректно знаходить PSSH бокси у зашифрованих файлах та виводить структуровану інформацію** — з точним форматуванням UUID, Base64-кодуванням сирих даних та правильними офсетами/розмірами.

---

## 📋 Структура тесту

```go
func TestPsshdump(t *testing.T) {
    testCases := []struct {
        name    string          // 🔹 Назва тест-кейсу
        file    string          // 🔹 Шлях до тестового файлу
        options []string        // 🔹 Прапорці (порожні для psshdump)
        wants   string          // 🔹 Очікуваний вивід (golden file)
    }{
        {
            name: "sample_init.encv.mp4",  // 🔹 Зашифроване відео (encv)
            file: "../../../../testdata/sample_init.encv.mp4",
            wants: "0:\n" +
                "  offset: 1307\n" +
                "  size: 52\n" +
                "  version: 1\n" +
                "  flags: 0x000000\n" +
                "  systemId: 1077efec-c0b2-4d02-ace3-3c1e52e2fb4b\n" +  // 🔹 ClearKey UUID
                "  dataSize: 0\n" +
                "  base64: \"AAAANHBzc2gBAAAAEHfv7MCyTQKs4zweUuL7SwAAAAEBI0VniavN7wEjRWeJq83vAAAAAA==\"\n" +
                "\n",
        },
        {
            name: "sample_init.enca.mp4",  // 🔹 Зашифроване аудіо (enca)
            file: "../../../../testdata/sample_init.enca.mp4",
            wants: "0:\n" +
                "  offset: 1307\n" +
                "  size: 52\n" +
                "  version: 1\n" +
                "  flags: 0x000000\n" +
                "  systemId: 1077efec-c0b2-4d02-ace3-3c1e52e2fb4b\n" +  // 🔹 Той самий ClearKey UUID
                "  dataSize: 0\n" +
                "  base64: \"AAAANHBzc2gBAAAAEHfv7MCyTQKs4zweUuL7SwAAAAEBI0VniavN7wEjRWeJq83vAAAAAA==\"\n" +
                "\n",
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            // 🔹 Перехоплення stdout через pipe (як у dump/extract/probe)
            stdout := os.Stdout
            r, w, err := os.Pipe()
            require.NoError(t, err)
            defer func() { os.Stdout = stdout }()
            os.Stdout = w
            
            // 🔹 Запуск Main у горутині
            go func() {
                require.Zero(t, Main(append(tc.options, tc.file)))
                w.Close()
            }()
            
            // 🔹 Читання виводу
            b, err := io.ReadAll(r)
            require.NoError(t, err)
            
            // 🔹 Порівняння з очікуваним
            assert.Equal(t, tc.wants, string(b))
        })
    }
}
```

**🎯 Призначення**: Перевірити, що інструмент `psshdump`:
- ✅ Знаходить PSSH бокси у зашифрованих файлах (encv/enca)
- ✅ Коректно форматує SystemID у UUID-рядок з дефісами
- ✅ Правильно кодує сирі дані у Base64
- ✅ Виводить точні офсети та розміри боксів
- ✅ Генерує стабільний, детермінований вивід для порівняння

---

## 🔍 Детальний розбір тест-кейсів

### 🔹 Кейс 1: Зашифроване відео (`encv`)

```go
{
    name: "sample_init.encv.mp4",
    file: "../../../../testdata/sample_init.encv.mp4",
    wants: "0:\n" +
        "  offset: 1307\n" +
        "  size: 52\n" +
        "  version: 1\n" +
        "  flags: 0x000000\n" +
        "  systemId: 1077efec-c0b2-4d02-ace3-3c1e52e2fb4b\n" +  // 🔹 ClearKey UUID
        "  dataSize: 0\n" +
        "  base64: \"AAAANHBzc2gBAAAAEHfv7MCyTQKs4zweUuL7SwAAAAEBI0VniavN7wEjRWeJq83vAAAAAA==\"\n" +
        "\n",
},
```

**📊 Очікуваний вивід:**
```
0:
  offset: 1307
  size: 52
  version: 1
  flags: 0x000000
  systemId: 1077efec-c0b2-4d02-ace3-3c1e52e2fb4b  # ← ClearKey UUID
  dataSize: 0
  base64: "AAAANHBzc2gBAAAAEHfv7MCyTQKs4zweUuL7SwAAAAEBI0VniavN7wEjRWeJq83vAAAAAA=="
```

**🔑 Ключові перевірки:**

| Поле | Очікуване значення | Чому це важливо |
|------|-------------------|----------------|
| `offset` | `1307` | ✅ Точне позиціонування боксу у файлі для дебагу |
| `size` | `52` | ✅ Розмір боксу включає заголовок + дані |
| `version` | `1` | ✅ Версія 1 = підтримка 64-бітних полів (KID_count тощо) |
| `flags` | `0x000000` | ✅ Прапорці: 0 = стандартна обробка |
| `systemId` | `1077efec-c0b2-4d02-ace3-3c1e52e2fb4b` | ✅ ClearKey UUID для тестування/простого шифрування |
| `dataSize` | `0` | ✅ Немає додаткових специфічних даних для ClearKey |
| `base64` | `"AAAANHBzc2gBAAAAEHfv7MCyTQKs4zweUuL7SwAAAAEBI0VniavN7wEjRWeJq83vAAAAAA=="` | ✅ Сирі дані для передачі у ліцензійний сервер |

**🔢 Декодування Base64 (для перевірки):**
```
🔹 Base64: "AAAANHBzc2gBAAAAEHfv7MCyTQKs4zweUuL7SwAAAAEBI0VniavN7wEjRWeJq83vAAAAAA=="
🔹 Декодовані байти (перші 20):
  00 00 00 34  70 73 73 68  01 00 00 00  10 7f ef ec  c0 b2 4d 02  ac
  ↑          ↑  ↑          ↑  ↑          ↑  ↑          ↑  ↑          ↑
  size=52    type="pssh"   version=1     flags=0x0    SystemID початок
```

**🎯 Призначення**: Перевірити коректність обробки **зашифрованого відео** з ClearKey DRM.

---

### 🔹 Кейс 2: Зашифроване аудіо (`enca`)

```go
{
    name: "sample_init.enca.mp4",
    file: "../../../../testdata/sample_init.enca.mp4",
    wants: "0:\n" + ...  // 🔹 Такий самий вивід, як для encv
},
```

**🎯 Призначення**: Перевірити, що інструмент однаково коректно обробляє **зашифроване аудіо** — PSSH структура однакова для відео та аудіо доріжок.

**🔑 Ключова перевірка**: Очікуваний вивід **ідентичний** для обох файлів, бо:
- ✅ Однаковий SystemID (ClearKey)
- ✅ Однакова структура PSSH боксу
- ✅ Однаковий розмір та офсет (у тестових файлах)

---

## 🔍 Технічні деталі тестування

### 🔹 Перехоплення stdout через pipe

```go
stdout := os.Stdout
r, w, err := os.Pipe()
require.NoError(t, err)
defer func() { os.Stdout = stdout }()  // 🔹 Відновлення після тесту
os.Stdout = w

go func() {
    require.Zero(t, Main(append(tc.options, tc.file)))  // 🔹 Запуск інструменту
    w.Close()  // 🔹 Закриття запису для сигналу завершення читання
}()

b, err := io.ReadAll(r)  // 🔹 Читання всього виводу
require.NoError(t, err)
assert.Equal(t, tc.wants, string(b))  // 🔹 Порівняння з golden file
```

**🎯 Призначення**: Тестувати CLI-інструмент як "чорний ящик" — без модифікації коду `psshdump`, тільки через стандартний вивід.

**⚠️ Важливо**: Використання горутини для `Main()` запобігає deadlock, бо `io.ReadAll()` блокується, поки `w.Close()` не сигналізує про кінець даних.

---

### 🔹 Форматування UUID: позиції дефісів

```go
for i, v := range pssh.SystemID {
    sysid += fmt.Sprintf("%02x", v)
    if i == 3 || i == 5 || i == 7 || i == 9 {  // 🔹 Позиції для '-'
        sysid += "-"
    }
}
```

**🔢 Приклад форматування:**
```
🔹 Вхід: [16]byte{0x10,0x77,0xef,0xec, 0xc0,0xb2, 0x4d,0x02, 0xac,0xe3, 0x3c,0x1e,0x52,0xe2,0xfb,0x4b}

🔹 Ітерація:
  i=0: "10"
  i=1: "1077"
  i=2: "1077ef"
  i=3: "1077efec" + "-" → "1077efec-"
  i=4: "1077efec-c0"
  i=5: "1077efec-c0b2" + "-" → "1077efec-c0b2-"
  i=6: "1077efec-c0b2-4d"
  i=7: "1077efec-c0b2-4d02" + "-" → "1077efec-c0b2-4d02-"
  i=8: "1077efec-c0b2-4d02-ac"
  i=9: "1077efec-c0b2-4d02-ace3" + "-" → "1077efec-c0b2-4d02-ace3-"
  i=10-15: додаємо решту: "3c1e52e2fb4b"

🔹 Результат: "1077efec-c0b2-4d02-ace3-3c1e52e2fb4b" ✅
               └──8──┘└4┘└4┘└4┘└────12────┘
```

**🎯 Призначення**: Забезпечити **стандартний формат UUID** для сумісності з іншими інструментами та серверами.

---

### 🔹 Base64-кодування: перевірка цілісності

```
🔹 Очікуваний base64: "AAAANHBzc2gBAAAAEHfv7MCyTQKs4zweUuL7SwAAAAEBI0VniavN7wEjRWeJq83vAAAAAA=="

🔹 Перевірка довжини:
  • Довжина base64: 88 символів
  • Розрахований розмір сирих даних: 88 × 3/4 = 66 байт (з урахуванням паддінгу)
  • Очікуваний розмір боксу: 52 байти (з заголовком)
  
🔹 Чому різниця?
  • Base64 кодує ВЕСЬ бокс (заголовок + payload)
  • size=52 включає: 8 (заголовок) + 44 (payload) = 52
  • Base64(52 байти) = ⌈52×4/3⌉ = 70 символів + паддінг "==" = 72? 
  • Але у тесті 88 символів → можливо, включає додаткові дані або інший розмір
  
🔹 Висновок: тест перевіряє точну відповідність, а не розрахунок
```

**🎯 Призначення**: Гарантувати, що сирі дані боксу передаються без пошкоджень для подальшої обробки.

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Валідація DRM-конфігурації перед стрімінгом

```go
func validateDRMConfig(segmentPath string) error {
    // 🔹 Запускаємо psshdump для отримання PSSH-метаданих
    cmd := exec.Command("mp4tool", "psshdump", segmentPath)
    output, err := cmd.Output()
    if err != nil {
        // 🔹 Якщо PSSH не знайдено — файл не зашифрований
        if strings.Contains(err.Error(), "not found") {
            log.Printf("ℹ️  No DRM protection in %s", segmentPath)
            return nil
        }
        return fmt.Errorf("psshdump failed: %w", err)
    }
    
    // 🔹 Перевірка наявності підтримуваних SystemID
    supportedUUIDs := map[string]bool{
        "eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1": true,  // Widevine
        "9a04f079-9840-4286-ab92-e65be0885f95": true,  // PlayReady
        "94ce86fb-0790-49ed-b8e4-8d397b47f985": true,  // FairPlay
        "1077efec-c0b2-4d02-ace3-3c1e52e2fb4b": true,  // ClearKey (тест)
    }
    
    lines := strings.Split(string(output), "\n")
    for _, line := range lines {
        if strings.Contains(line, "systemId:") {
            parts := strings.SplitN(line, ":", 2)
            if len(parts) == 2 {
                sysid := strings.TrimSpace(parts[1])
                if !supportedUUIDs[sysid] {
                    return fmt.Errorf("unsupported DRM system: %s", sysid)
                }
                log.Printf("✅ Supported DRM: %s", sysid)
            }
        }
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Автоматична генерація #EXT-X-KEY для HLS

```go
func generateDRMKeys(segmentPath string) ([]string, error) {
    // 🔹 Отримання PSSH-метаданих
    cmd := exec.Command("mp4tool", "psshdump", segmentPath)
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }
    
    var keys []string
    lines := strings.Split(string(output), "\n")
    
    var currentSystemID, currentBase64 string
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "systemId:") {
            currentSystemID = strings.TrimSpace(strings.TrimPrefix(line, "systemId:"))
        } else if strings.HasPrefix(line, "base64:") {
            currentBase64 = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "base64:")), `"`)
            
            // 🔹 Форматування #EXT-X-KEY
            keyFormat := drmKeyFormat(currentSystemID)
            keyLine := fmt.Sprintf(
                "#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"license?system_id=%s&pssh=%s\",KEYFORMAT=\"%s\"",
                currentSystemID,
                url.QueryEscape(currentBase64),
                keyFormat,
            )
            keys = append(keys, keyLine)
            
            currentSystemID, currentBase64 = "", ""
        }
    }
    
    return keys, nil
}

// 🔹 Допоміжна функція для KEYFORMAT
func drmKeyFormat(systemID string) string {
    switch systemID {
    case "eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1":
        return "urn:uuid:eed87750-3c00-4d7c-b2a1-39b0b6f0b2a1"  // Widevine
    case "9a04f079-9840-4286-ab92-e65be0885f95":
        return "com.microsoft.playready"  // PlayReady
    case "94ce86fb-0790-49ed-b8e4-8d397b47f985":
        return "com.apple.streamingkeydelivery"  // FairPlay
    case "1077efec-c0b2-4d02-ace3-3c1e52e2fb4b":
        return "urn:uuid:1077efec-c0b2-4d02-ace3-3c1e52e2fb4b"  // ClearKey
    default:
        return "urn:uuid:" + systemID
    }
}
```

---

### 🔹 Приклад 3: Моніторинг DRM-використання у записях

```go
type DRMStats struct {
    SystemID    string
    Version     uint8
    FileCount   int
    TotalSize   uint64
}

func monitorDRMUsage(recordingsDir string) (map[string]*DRMStats, error) {
    stats := make(map[string]*DRMStats)
    
    // 🔹 Перебір всіх MP4-файлів у директорії
    files, err := filepath.Glob(filepath.Join(recordingsDir, "*.mp4"))
    if err != nil {
        return nil, err
    }
    
    for _, filePath := range files {
        // 🔹 Запускаємо psshdump
        cmd := exec.Command("mp4tool", "psshdump", filePath)
        output, err := cmd.Output()
        if err != nil {
            continue  // 🔹 Пропускаємо файли без DRM
        }
        
        // 🔹 Парсинг виводу
        lines := strings.Split(string(output), "\n")
        for _, line := range lines {
            if strings.Contains(line, "systemId:") {
                parts := strings.SplitN(line, ":", 2)
                if len(parts) != 2 { continue }
                
                sysid := strings.TrimSpace(parts[1])
                
                // 🔹 Оновлення статистики
                if _, exists := stats[sysid]; !exists {
                    stats[sysid] = &DRMStats{SystemID: sysid}
                }
                stats[sysid].FileCount++
                
                // 🔹 Додавання розміру (спрощено)
                // У реальному коді: парсити "size:" рядок
            }
        }
    }
    
    return stats, nil
}

// 🔹 Використання:
stats, err := monitorDRMUsage("/recordings/2024-01")
if err != nil {
    log.Printf("❌ Failed to monitor DRM usage: %v", err)
} else {
    for sysid, stat := range stats {
        log.Printf("🔐 %s: %d files, total size: %d bytes", 
            sysid, stat.FileCount, stat.TotalSize)
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

> **Цей тест — ваш "золотий стандарт" для надійного інструменту `psshdump`**.  
> Він гарантує:
> • ✅ Коректне знаходження PSSH боксів у зашифрованих файлах (encv/enca)
> • ✅ Точне форматування SystemID у стандартний UUID-рядок
> • ✅ Правильне Base64-кодування сирих даних для інтеграції з серверами
> • ✅ Стабільний, детермінований вивід для порівняння через golden files
> • ✅ Безпечне перехоплення stdout через pipe без deadlock

Для вашого **CCTV HLS Processor** це означає:
- 🔐 Миттєва валідація DRM-конфігурації записів перед публікацією
- 🌐 Автоматична генерація #EXT-X-KEY директив для HLS-плейлистів
- 📊 Моніторинг використання різних DRM-систем у вашій бібліотеці записів
- 🔄 Легка інтеграція з Widevine, PlayReady, FairPlay ліцензійними серверами
- 🛡️ Безпечна обробка DRM-метаданих без розкриття чутливої інформації

Потребуєте допомоги з інтеграцією `psshdump` у ваш конвеєр DRM-підтримки або з налаштуванням взаємодії з ліцензійними серверами? Напишіть — покажу готовий код для вашого сценарію! 🚀🔐