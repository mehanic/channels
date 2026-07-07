# 🧪 Глибокий розбір `codec/utils_test.go` — тестування утиліт бітстріму

Це **критичний набір юніт-тестів** для валідації низькорівневих функцій пошуку start code та видалення emulation prevention bytes. Ці тести гарантують, що ваш парсер коректно обробляє крайні випадки, які часто зустрічаються у реальних CCTV-потоках. Розберемо архітектурно:

---

## 🔍 1. `TestCovertRbspToSodb` — валідація видалення emulation prevention bytes

### 📦 Тестові дані: реальний H.265 VPS NALU з "екранованими" байтами

```go
var nalu []byte = []byte{
    // Start code (4-byte)
    0x00, 0x00, 0x00, 0x01,
    
    // NAL header + payload (H.265 VPS)
    0x40, 0x01, 0x0C, 0x01, 0xFF, 0xFF, 0x01, 0x60,
    
    // ← Emulation prevention sequences (0x000003):
    0x00, 0x00, 0x03, 0x00,  // #1: має стати 0x00 0x00 0x00
    0x90, 0x00, 0x00, 0x03, 0x00,  // #2: має стати 0x90 0x00 0x00 0x00
    0x00, 0x00, 0x03, 0x00,  // #3: має стати 0x00 0x00 0x00
    
    // Кінець payload
    0x78, 0x99, 0x98, 0x09,
}
```

### 🎯 Очікуваний результат:

```go
var result []byte = []byte{
    // Start code зберігається (не чіпаємо!)
    0x00, 0x00, 0x00, 0x01,
    
    // Payload без emulation prevention bytes (0x03 видалено):
    0x40, 0x01, 0x0C, 0x01, 0xFF, 0xFF, 0x01, 0x60,
    0x00, 0x00, 0x00, 0x90,  // ← 0x03 видалено з 0x00000300 → 0x00000090
    0x00, 0x00, 0x00, 0x00, 0x00,  // ← 0x03 видалено
    0x00, 0x00, 0x00,  // ← 0x03 видалено
    0x78, 0x99, 0x98, 0x09,
}
```

### 🔬 Що перевіряється:

| Аспект | Очікувана поведінка | Чому це критично |
|--------|---------------------|-----------------|
| **Start code зберігається** | `0x00000001` не чіпається | Без start code неможливо розділити NAL units у потоці |
| **Emulation prevention видаляється** | `0x000003XX` → `0x0000XX` | Без цього парсинг SPS/PPS зламається на колізіях зі start code |
| **Межі буфера** | Не панікує при останніх байтах | Реальні пакети часто обрізані на межі мережевого MTU |

### 🐞 Потенційна проблема у реалізації:

```go
// У CovertRbspToSodb:
if bs.RemainBytes() > 3 && bs.NextBits(24) == 0x000003 {
    // ⚠️ bs.NextBits(24) читає 3 байти, але потім bs.Uint8(8) читає по 1 байту
    // Якщо буфер закінчується на 0x000003, може бути reading out of bounds!
    
    // Безпечніше:
    if bs.RemainBytes() >= 3 {
        b0 := bs.PeekByte(0)
        b1 := bs.PeekByte(1) 
        b2 := bs.PeekByte(2)
        if b0 == 0x00 && b1 == 0x00 && b2 == 0x03 {
            // ...
        }
    }
}
```

### 💡 Покращення тесту:

```go
// Додати edge-case тести:
func TestCovertRbspToSodb_EdgeCases(t *testing.T) {
    tests := []struct{
        name string
        input []byte
        want []byte
    }{
        {
            name: "empty buffer",
            input: []byte{},
            want: []byte{},
        },
        {
            name: "no emulation bytes",
            input: []byte{0x67, 0x64, 0x00, 0x28},
            want: []byte{0x67, 0x64, 0x00, 0x28},
        },
        {
            name: "emulation at end",
            input: []byte{0x00, 0x00, 0x03},  // обрізаний пакет!
            want: []byte{0x00, 0x00},  // 0x03 видалено
        },
        {
            name: "consecutive emulation",
            input: []byte{0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x01},
            want: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := CovertRbspToSodb(tt.input)
            if !bytes.Equal(got, tt.want) {
                t.Errorf("got=%x, want=%x", got, tt.want)
            }
        })
    }
}
```

---

## 🔍 2. `TestFindStartCode` — валідація пошуку маркерів Annex-B

### 📊 Матриця тестових випадків:

| Тест | Вхідні дані | Очікуваний результат | Що перевіряється |
|------|-------------|---------------------|-----------------|
| **test1** | `[0x00,0x00,0x00,0x01,0x67]` | `(0, START_CODE_4)` | 4-байтовий start code на початку |
| **test2** | `[0x00,0x00,0x01,0x67]` | `(0, START_CODE_3)` | 3-байтовий start code на початку |
| **test3** | `[0x99,0x00,0x00,0x01,0x67]` | `(1, START_CODE_3)` | 3-байтовий start code зі зсувом |
| **test4** | `[0x99,0x00,0x00,0x00,0x01,0x67]` | `(1, START_CODE_4)` | 4-байтовий start code зі зсувом |
| **test5** | `[0x99,0x88,0x77,0x00,0x01,0x67]` | `(-1, START_CODE_3)` | Start code не знайдено |

### 🔬 Детальний розбір логіки:

#### ✅ test1: 4-byte start code на початку
```
Вхід:  [00][00][00][01][67]
        ↑↑↑↑↑↑↑↑↑↑↑↑
        | 4-byte start code |
        
bytes.Index шукає [00][00][01] → знаходить на позиції 1 (другий 0x00)
Перевірка: nalu[1-1] = nalu[0] = 0x00 → це 4-byte start code!
Повертає: (0, START_CODE_4) ✓
```

#### ✅ test2: 3-byte start code на початку
```
Вхід:  [00][00][01][67]
        ↑↑↑↑↑↑↑↑
        | 3-byte start code |
        
bytes.Index шукає [00][00][01] → знаходить на позиції 0
idx == 0 → не можна перевірити nalu[-1], тому повертаємо 3-byte
Повертає: (0, START_CODE_3) ✓
```

#### ✅ test3: 3-byte start code зі зсувом
```
Вхід:  [99][00][00][01][67]
        ↑↑↑↑↑↑↑↑↑↑↑↑
        | дані | 3-byte start code |
        
bytes.Index(nalu[0:], [00,00,01]) → знаходить на позиції 1
Перевірка: nalu[1-1] = nalu[0] = 0x99 ≠ 0x00 → це 3-byte start code
Повертає: (1, START_CODE_3) ✓
```

#### ✅ test4: 4-byte start code зі зсувом
```
Вхід:  [99][00][00][00][01][67]
        ↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑
        | дані | 4-byte start code |
        
bytes.Index(nalu[0:], [00,00,01]) → знаходить на позиції 2 (третій 0x00)
Перевірка: nalu[2-1] = nalu[1] = 0x00 → це 4-byte start code!
Повертає: (1, START_CODE_4) ← позиція ПЕРШОГО 0x00 з четвірки ✓
```

#### ✅ test5: Start code не знайдено
```
Вхід:  [99][88][77][00][01][67]
        ↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑
        | дані | 00 01 (не достатньо для start code) |
        
bytes.Index шукає [00][00][01] → не знаходить → idx = -1
Повертає: (-1, START_CODE_3) ← ⚠️ START_CODE_3 тут не має сенсу!
```

### 🐞 Критична проблема: повернення `START_CODE_3` при `-1`

```go
// У реалізації:
return -1, START_CODE_3  // ← Якщо start code не знайдено, навіщо повертати тип?

// Це може призвести до помилок у коді, який не перевіряє позицію:
pos, typ := FindStartCode(data, 0)
if typ == START_CODE_4 {  // ⚠️ Може бути хибно-позитивним, якщо pos == -1!
    // ...
}

// Краще:
const START_CODE_NONE START_CODE_TYPE = 0

func FindStartCode(nalu []byte, offset int) (int, START_CODE_TYPE) {
    // ...
    if idx == -1 {
        return -1, START_CODE_NONE  // ← Явний "не знайдено"
    }
    // ...
}

// Тоді у тестах:
{name: "test5", ..., want: -1, want1: START_CODE_NONE},
```

### 💡 Покращення тестів:

```go
// Додати тести для:
func TestFindStartCode_Advanced(t *testing.T) {
    tests := []struct{
        name string
        data []byte
        offset int
        wantPos int
        wantType START_CODE_TYPE
    }{
        {
            name: "offset beyond start code",
            data: []byte{0x00,0x00,0x00,0x01,0x67,0x00,0x00,0x01,0x68},
            offset: 5,  // починаємо пошук після першого start code
            wantPos: 5,  // має знайти другий 3-byte start code
            wantType: START_CODE_3,
        },
        {
            name: "overlapping patterns",
            data: []byte{0x00,0x00,0x00,0x00,0x01},  // 0x0000 + 0x000001
            offset: 0,
            wantPos: 0,  // має знайти перший 4-byte start code
            wantType: START_CODE_4,
        },
        {
            name: "offset at end",
            data: []byte{0x67,0x64},
            offset: 2,  // offset == len(data)
            wantPos: -1,
            wantType: START_CODE_NONE,  // ← новий тип
        },
        {
            name: "offset out of bounds",
            data: []byte{0x67},
            offset: 10,  // offset > len(data)
            wantPos: -1,
            wantType: START_CODE_NONE,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, gotType := FindStartCode(tt.data, tt.offset)
            if got != tt.wantPos {
                t.Errorf("pos = %d, want %d", got, tt.wantPos)
            }
            if gotType != tt.wantType {
                t.Errorf("type = %v, want %v", gotType, tt.wantType)
            }
        })
    }
}
```

---

## 🎯 3. Інтеграція з вашим пайплайном: навіщо ці тести критичні

### 📍 У `segmentAssembler`:

```go
// Без коректного FindStartCode:
func (sa *SegmentAssembler) extractSPS(data []byte) error {
    pos, typ := codec.FindStartCode(data, 0)
    if pos == -1 {
        return errors.New("no SPS found")  // ✓ коректна обробка
    }
    
    // ⚠️ Якщо тип повертається неправильно при pos==-1:
    if typ == codec.START_CODE_4 {  // ← Може бути хибно-позитивним!
        // Помилковий код, який панікує при pos==-1
        payload := data[pos+int(typ):]  // panic: slice bounds out of range!
    }
}

// З тестами: гарантія, що пос+тип завжди консистентні
```

### 📍 У `createTSSegment`:

```go
// Конвертація Annex-B → AVCC вимагає точного знання довжини start code:
func annexBToAVCC(nalu []byte) []byte {
    pos, typ := codec.FindStartCode(nalu, 0)
    if pos == -1 {
        return nil  // невалідний вхід
    }
    
    // Правильний зсув залежить від типу start code:
    payloadStart := pos + int(typ)  // 3 або 4
    payload := nalu[payloadStart:]
    
    // Записати 4-byte length prefix
    avcc := make([]byte, 4+len(payload))
    binary.BigEndian.PutUint32(avcc, uint32(len(payload)))
    copy(avcc[4:], payload)
    return avcc
}
```

### 📍 У `CovertRbspToSodb` для парсингу параметрів:

```go
// Без коректного видалення emulation bytes:
func parseSPS(nalu []byte) (*SPS, error) {
    pos, _ := FindStartCode(nalu, 0)
    payload := nalu[pos+4:]  // пропустити start code + NAL header
    
    // ⚠️ Якщо emulation bytes не видалені:
    bs := NewBitStream(payload)
    sps.Decode(bs)  // ← Може прочитати 0x03 як частину поля → неправильні параметри!
    
    // З тестом: гарантія, що 0x000003 → 0x0000
    sodb := CovertRbspToSodb(payload)
    bs := NewBitStream(sodb)
    sps.Decode(bs)  // ✓ коректний парсинг
    return &sps, nil
}
```

---

## 🧭 Висновок: чому ці 2 тести варті більше за 100 рядків коду

| Тест | Що захищає | Вартість помилки у продакшені |
|------|------------|------------------------------|
| `TestCovertRbspToSodb` | Коректне видалення `0x03` emulation bytes | Неправильні SPS параметри → невалідна роздільна здатність → розтягнуте відео у клієнта |
| `TestFindStartCode` | Точне визначення меж NAL units | Зміщення при розділенні кадрів → пошкоджені сегменти → FFmpeg відхиляє `.ts` файл |

> 🔑 **Головна ідея**: Ці тести — **страховка від тонких багів**, які важко відлагодити у продакшені. Вони документують:
> 1. Як обробляються послідовності `0x000003` (емалація prevention)
> 2. Як відрізняти 3-байтові та 4-байтові start codes
> 3. Як поводитися при відсутності start code або виході за межі буфера

Без них будь-яка зміна у `bytes.Index` логіці або оптимізація `BitStream` може непомітно зламати парсинг — і ви дізнаєтесь про це тільки коли камери з певним енкодером перестануть відтворюватись.

💡 **Фінальна порада**: Додайте **property-based тест** (наприклад, через `gopter` або `quick`), який:
1. Генерує випадкові байтові послідовності з вбудованими `0x000001`/`0x00000001`/`0x000003`
2. Перевіряє, що `FindStartCode` + `CovertRbspToSodb` інваріантні до порядку застосування
3. Гарантує, що "очищений" SODB можна безпечно парсити без ризику колізій зі start code

Це вбереже від регресій при рефакторингу низькорівневих утиліт.