# 🧪 Глибокий розбір `codec/vp8_test.go` — тестування парсингу роздільної здатності VP8

Це **цілеспрямований юніт-тест** для валідації функції `GetResloution` (з опечаткою!), яка витягує ширину та висоту з ключового кадру VP8. Розберемо архітектурно, побайтово:

---

## 📦 1. Тестові дані: розбір байтового масиву

```go
frame := []byte{
    // [0-2] Frame Tag (3 байти, little-endian)
    0xB0, 0xF0, 0x00,  // tmp = 0x00F0B0 = 0b0000_1111_0000_1011_0000
    
    // [3-9] Key Frame Header (7 байт)
    0x9D, 0x01, 0x2A,  // Magic start code ✓
    0x00, 0x03,        // Width: 14 bits + 2 scale bits
    0x40, 0x01,        // Height: 14 bits + 2 scale bits
}
// Загальна довжина: 10 байт ✓
```

### 🔍 Кроки парсингу:

#### Крок 1: `IsKeyFrame(frame)` → перевірка FrameType

```go
// DecodeFrameTag:
tmp = frame[0] | (frame[1]<<8) | (frame[2]<<16)
    = 0xB0 | (0xF0<<8) | (0x00<<16)
    = 0xB0 | 0xF000 | 0x0
    = 0xF0B0 = 0b1111_0000_1011_0000

FrameType = tmp & 0x01 = 0b...0000 & 0x01 = 0 → I-frame ✓
```

#### Крок 2: `DecodeKeyFrameHead(frame[3:])` → парсинг заголовку

```go
// Зсув на 3 байти: frame[3:] = [0x9D, 0x01, 0x2A, 0x00, 0x03, 0x40, 0x01]

// 1. Перевірка start code:
frame[0]=0x9D, frame[1]=0x01, frame[2]=0x2A ✓

// 2. Розрахунок Width:
//    frame[3] = 0x00 (молодший байт)
//    frame[4] = 0x03 (старший байт + scale у старших 2 бітах)
Width = (frame[4] & 0x3F) << 8 | frame[3]
      = (0x03 & 0x3F) << 8 | 0x00
      = 0x03 << 8 | 0x00
      = 768 ✓

HorizScale = frame[4] >> 6 = 0x03 >> 6 = 0 → масштаб 1:1

// 3. Розрахунок Height:
//    frame[5] = 0x40 (молодший байт)
//    frame[6] = 0x01 (старший байт + scale)
Height = (frame[6] & 0x3F) << 8 | frame[5]
       = (0x01 & 0x3F) << 8 | 0x40
       = 0x01 << 8 | 0x40
       = 256 + 64 = 320 ✓

VertScale = frame[6] >> 6 = 0x01 >> 6 = 0 → масштаб 1:1
```

### ✅ Результат: `width=768, height=320` — тест проходить ✓

---

## 🐞 2. Потенційні проблеми у тесті

### ❗ Критичні:

1. **Опечатка у назві функції**:
   ```go
   func TestGetResloution(t *testing.T)  // ← має бути TestGetResolution
   // Це порушує консистентність, ускладнює пошук та автодоповнення в IDE
   ```

2. **Відсутність тестів на помилки**:
   ```go
   // Тест має wantErr: false, але не перевіряє сценарії з помилками:
   // - Не ключовий кадр (FrameType = 1)
   // - Замалий буфер (< 10 байт)
   // - Невірний start code (не 0x9D012A)
   // - Некоректні scale значення (>3)
   ```

3. **`fmt.Printf` замість `t.Logf`**:
   ```go
   fmt.Printf("w:%d,h:%d\n", gotWidth, gotHeight)  // ← вивід у stdout, не у тест-лог
   // Краще:
   t.Logf("got resolution: %dx%d", gotWidth, gotHeight)
   // Це дозволить бачити вивід тільки при -v або при падінні тесту
   ```

4. **Відсутність перевірки масштабів**:
   ```go
   // Тест перевіряє тільки Width/Height, але не HorizScale/VertScale
   // Якщо масштаб ≠ 0, фактична роздільна здатність для відтворення інша!
   ```

### 💡 Покращення тесту:

```go
func TestGetResolution_Comprehensive(t *testing.T) {  // ← виправлена назва
    tests := []struct {
        name       string
        frame      []byte
        wantWidth  int
        wantHeight int
        wantScaleH int
        wantScaleV int
        wantErr    bool
    }{
        {
            name: "768x320 no scale",
            frame: []byte{
                0xB0, 0xF0, 0x00,  // FrameTag: I-frame
                0x9D, 0x01, 0x2A,  // Start code
                0x00, 0x03,        // Width: 768, scale=0
                0x40, 0x01,        // Height: 320, scale=0
            },
            wantWidth: 768, wantHeight: 320,
            wantScaleH: 0, wantScaleV: 0,
            wantErr: false,
        },
        {
            name: "P-frame (not key)",
            frame: []byte{0xB1, 0xF0, 0x00},  // FrameType = 1
            wantErr: true,
        },
        {
            name: "buffer too short",
            frame: []byte{0xB0, 0xF0},  // < 3 байти для FrameTag
            wantErr: true,
        },
        {
            name: "invalid start code",
            frame: []byte{
                0xB0, 0xF0, 0x00,
                0x9E, 0x01, 0x2A,  // ≠ 0x9D012A
                0x00, 0x03, 0x40, 0x01,
            },
            wantErr: true,
        },
        {
            name: "16:9 scale factor",
            frame: []byte{
                0xB0, 0xF0, 0x00,
                0x9D, 0x01, 0x2A,
                0x00, 0xC3,  // Width=768, HorizScale=3 (16:9)
                0x00, 0x40,  // Height=1024, VertScale=1 (5:4)
            },
            wantWidth: 768, wantHeight: 1024,
            wantScaleH: 3, wantScaleV: 1,
            wantErr: false,
        },
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            gotW, gotH, err := GetResolution(tt.frame)  // ← виправлена назва
            
            if (err != nil) != tt.wantErr {
                t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
                return
            }
            if err != nil {
                return  // не перевіряти значення при помилці
            }
            
            if gotW != tt.wantWidth {
                t.Errorf("width = %d, want %d", gotW, tt.wantWidth)
            }
            if gotH != tt.wantHeight {
                t.Errorf("height = %d, want %d", gotH, tt.wantHeight)
            }
            
            // Додатково: перевірити масштаби через внутрішній доступ або новий helper
            // head, _ := DecodeKeyFrameHead(tt.frame[3:])
            // if head.HorizScale != tt.wantScaleH { ... }
        })
    }
}
```

---

## 🎯 3. Інтеграція з вашим пайплайном: навіщо цей тест критичний

### 📍 У `segmentAssembler`:

```go
// Без коректного розпарсингу роздільної здатності:
func (sa *SegmentAssembler) handleVP8Frame(data []byte) {
    if codec.IsKeyFrame(data) {
        // ⚠️ Якщо тест не покриває edge cases:
        w, h, err := codec.GetResolution(data)
        if err != nil {
            // Помилка може бути прихована через неправильну обробку
            logger.Warn("VP8 resolution parse failed", "error", err)
            // Але сегмент все одно створюється з неправильними метаданими!
        }
        sa.currentResolution = fmt.Sprintf("%dx%d", w, h)
    }
}

// З тестом: гарантія, що 768×320 парситься коректно, а невалідні входи відхиляються
```

### 📍 У `VideoManifestProxy`:

```go
// Генерація HLS-плейлиста вимагає точної роздільної здатності:
func generateStreamInfo(codecID codec.CodecID, width, height int) string {
    // VP8: CODECS="vp8", RESOLUTION=768x320
    return fmt.Sprintf(`#EXT-X-STREAM-INF:RESOLUTION=%dx%d,CODECS="vp8"`, width, height)
}

// Якщо GetResolution повертає неправильні значення:
// - Клієнт може відмовитись відтворювати через невідповідність RESOLUTION
// - Адаптивний бітрейт може обрати неправильний варіант
```

---

## 🧭 Висновок: чому цей один тест вартий розширення

| Аспект | Поточний стан | Ризик без покращення |
|--------|--------------|---------------------|
| **Покрытие** | 1 успішний кейс | Непомічені баги при неключових кадрах, коротких буферах |
| **Назви** | Опечатка `Resloution` | Плутанина при рефакторингу, пошуку, документації |
| **Логування** | `fmt.Printf` | Зайвий шум у stdout, важкий дебаг у CI |
| **Масштаби** | Не перевіряються | Неправильне відображення 16:9 контенту як 1:1 |

> 🔑 **Головна ідея**: Цей тест — **точка входу** для валідації всієї логіки роботи з VP8 у вашому пайплайні. Він гарантує, що:
> 1. Бітові маски `(x & 0x3F) << 8 | y` працюють коректно для 14-бітних значень
> 2. Зсув `frame[3:]` правильно відділяє Frame Tag від Key Frame Header
> 3. Магічний start code `0x9D012A` надійно відрізняє ключові кадри

Без розширення цього тесту будь-яка зміна у `DecodeKeyFrameHead` (наприклад, оптимізація бітових операцій) може непомітно зламати парсинг — і ви дізнаєтесь про це тільки коли камери з нестандартними налаштуваннями масштабу перестануть відтворюватись коректно.

💡 **Фінальна порада**: 
1. Виправте опечатку: `GetResloution` → `GetResolution` (пошук/заміна в усьому проекті)
2. Додайте 3-4 додаткових тест-кейси з `wantErr: true` для валідації обробки помилок
3. Замініть `fmt.Printf` на `t.Logf` для чистого виводу в CI/CD
4. Додайте helper-функцію `DisplayResolution()` у `VP8KeyFrameHead` для розрахунку фактичних розмірів з урахуванням масштабу — і протестуйте її окремо

Це перетворить цей тест з "перевірки одного випадку" на "гарантію надійності всієї VP8-підсистеми".