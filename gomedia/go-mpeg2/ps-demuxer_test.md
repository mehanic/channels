# 🧪 Глибокий розбір `mpeg2/ps_test.go` — тестування Program Stream демуксера

Це **набір інтеграційних тестів** для валідації інкрементального парсингу MPEG-2 Program Stream. Тести перевіряють критичні шляхи: обробку неповних даних, розпізнавання різних типів пакетів (pack header, system header, PSM) та підтримку MPEG-1. Розберемо архітектурно:

---

## 📦 1. Тестові дані: бітові патерни MPEG-PS пакетів

### 🔍 Розбір байтових масивів:

#### **ps1**: Тільки start code pack header
```go
var ps1 []byte = []byte{0x00, 0x00, 0x01, 0xBA}
//                     ↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑
//                     packet_start_code_prefix + stream_id
//                     0x000001BA = pack_header

// Очікуваний результат: wantErr: true
// Чому: після start code немає даних заголовку → парсер поверне errNeedMore
```

#### **ps2**: Повний pack header MPEG-2
```go
var ps2 []byte = []byte{
    0x00, 0x00, 0x01, 0xBA,  // start code
    0x40,                     // [01][SCR_extension:2][marker:1][SCR_base:4]
    0x01, 0x00, 0x01,        // SCR_base continuation (32 біти)
    0x33, 0x44,              // SCR_extension + marker
    0xFF, 0xFF, 0xFF, 0xF1,  // mux_rate (22 біти) + marker
    0xFF,                     // [marker:1][reserved:5][pack_stuffing_length:3]
}
// Очікуваний результат: wantErr: false
// Чому: повний pack header (14 байт після start code) → успішний парсинг
```

#### **ps3**: Pack header + початок system header (неповний)
```go
var ps3 []byte = []byte{
    // ... pack header як у ps2, але з 0xF0 замість 0xFF
    0xF0,                     // pack_stuffing_length = 0
    0x00, 0x00, 0x01, 0xBB,  // start code system_header
    // ← далі немає даних!
}
// Очікуваний результат: wantErr: true
// Чому: system_header має мінімум 6 байт даних після start code → не вистачає
```

#### **ps4**: Pack header + неповний system header
```go
var ps4 []byte = []byte{
    // ... pack header
    0xF1, 0x34,              // pack_stuffing_length = 1 → 1 байт 0x34
    0x00, 0x00, 0x01, 0xBB,  // system_header start code
    0x00, 0x01,              // header_length = 1 (невалідно, мінімум 6!)
    0x00, 0x01, 0x33, 0x44, 0xFF, 0x34  // часткові дані
}
// Очікуваний результат: wantErr: true
// Чому: header_length=1 < мінімально допустимого → помилка парсингу
```

#### **ps5**: Повний pack header + повний system header
```go
var ps5 []byte = []byte{
    // ... pack header
    0xF1, 0x34,              // stuffing_length=1, байт 0x34
    0x00, 0x00, 0x01, 0xBB,  // system_header start code
    0x00, 0x09,              // header_length = 9 байт
    0x00, 0x01, 0x33, 0x44, 0xFF, 0x34, 0x81, 0x00, 0x00  // 9 байт даних
}
// Очікуваний результат: wantErr: false
// Чому: всі обов'язкові поля присутні → успішний парсинг
```

#### **ps6**: Program Stream Map (PSM) пакет
```go
var ps6 []byte = []byte{
    0x00, 0x00, 0x01, 0xBC,  // start code program_stream_map
    0x40, 0x0a,              // PES_packet_length = 10
    0x00, 0x00,              // current_next + version
    0x00, 0x00,              // program_stream_info_length
    0x00, 0x03,              // stream_map_length = 3
    0x34, 0x81, 0x00, 0x00   // часткові дані мапи (неповні, але достатньо для парсингу заголовку)
}
// Очікуваний результат: wantErr: false
// Чому: PSM парситься окремо, і навіть неповна мапа не ламає базовий парсинг
```

#### **ps7**: MPEG-1 pack header (спрощений формат)
```go
var ps7 []byte = []byte{
    0x00, 0x00, 0x01, 0xBA,  // start code
    0x20,                    // [001][marker:1][SCR:4] ← біт 6=0 → MPEG-1!
    0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03  // SCR + mux_rate у MPEG-1 форматі
}
// Очікуваний результат: wantErr: false
// Чому: MPEG-1 pack header коротший (8 байт після start code) → валідний
```

---

## 🔬 2. Структура тесту: table-driven testing

### 🔧 Шаблон тесту:

```go
func TestPSDemuxer_Input(t *testing.T) {
    type fields struct {
        streamMap map[uint8]*psstream  // стан демуксера
        pkg       *PSPacket            // буфер поточного пакету
        cache     []byte               // кеш неповних даних
        OnPacket  func(pkg Display, decodeResult error)  // callback дебагу
        OnFrame   func(frame []byte, cid PS_STREAM_TYPE, pts, dts uint64)  // callback кадрів
    }
    type args struct {
        data []byte  // вхідні байти
    }
    
    tests := []struct {
        name    string
        fields  fields  // початковий стан
        args    args    // аргументи виклику
        wantErr bool    // очікуваний результат: помилка чи ні
    }{
        // ... тест-кейси ...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 1. Створення демуксера з заданим станом
            psdemuxer := &PSDemuxer{
                streamMap: tt.fields.streamMap,
                pkg:       tt.fields.pkg,
                cache:     tt.fields.cache,
                OnPacket:  tt.fields.OnPacket,
                OnFrame:   tt.fields.OnFrame,
            }
            
            // 2. Виклик тестованої функції
            if err := psdemuxer.Input(tt.args.data); (err != nil) != tt.wantErr {
                t.Errorf("PSDemuxer.Input() error = %v, wantErr %v", err, tt.wantErr)
            }
            // ← ⚠️ Але не перевіряється, ЩО саме було розпарсено!
        })
    }
}
```

### 🎯 Що перевіряється:

| Аспект | Очікувана поведінка | Чому це критично |
|--------|---------------------|-----------------|
| **Інкрементальний парсинг** | Неповні дані → `errNeedMore`, повні → `nil` | Реальні мережеві потоки приходять фрагментами |
| **Розпізнавання типів пакетів** | `0xBA`→pack, `0xBB`→system, `0xBC`→PSM | Кожен тип має різну структуру → неправильна класифікація = помилка |
| **MPEG-1 vs MPEG-2** | Біт 6 байту після start code визначає версію | Старі камери можуть використовувати MPEG-1 → потрібна зворотна сумісність |
| **Обробка stuffing bytes** | `pack_stuffing_length` коректно пропускається | Без цього зсув у парсингу → пошкоджені дані |

---

## 🐞 3. Потенційні проблеми у тестах

### ❗ Критичні недоліки:

1. **Відсутність перевірки результатів парсингу**:
   ```go
   // Тест перевіряє тільки наявність/відсутність помилки:
   if err := psdemuxer.Input(tt.args.data); (err != nil) != tt.wantErr {
       t.Errorf(...)
   }
   
   // Але не перевіряє:
   // • Чи правильно розпарсився pack_header?
   // • Чи оновився psdemuxer.pkg.Header?
   // • Чи викликався OnPacket callback?
   // • Чи змінився стан cache?
   
   // Краще додати перевірки:
   if tt.name == "test2" && psdemuxer.pkg.Header == nil {
       t.Error("pack header should be parsed")
   }
   if tt.name == "test5" && psdemuxer.pkg.System == nil {
       t.Error("system header should be parsed")
   }
   ```

2. **Спільний стан між тестами**:
   ```go
   // pkg: new(PSPacket) створюється один раз на тест, але:
   // • Якщо тест модифікує pkg, наступні тести можуть отримати "брудний" стан
   // Краще: глибоке копіювання або окремий pkg для кожного тесту
   
   fields: fields{
       pkg: func() *PSPacket { p := new(PSPacket); return p }(),  // factory function
   }
   ```

3. **Відсутність тестів на PES пакети**:
   ```go
   // Усі тестові дані — це pack/system/PSM заголовки
   // Але основна логіка демуксера — обробка PES пакетів з відео/аудіо!
   
   // Додати тести для:
   // • Відео PES (stream_id 0xE0) з H.264 NAL units
   // • Аудіо PES (stream_id 0xC0) з AAC frames
   // • Розрізаний PES між двома викликами Input()
   ```

4. **Не перевіряється `cache` логіка**:
   ```go
   // ps1 тестує неповні дані, але не перевіряє:
   // • Чи збереглися дані у psdemuxer.cache?
   // • Чи коректно об'єднаються з наступним чанком?
   
   // Краще:
   err1 := demuxer.Input(ps1)  // неповний pack header
   if err1 != errNeedMore {
       t.Error("expected errNeedMore for partial data")
   }
   if len(demuxer.cache) == 0 {
       t.Error("cache should contain partial data")
   }
   
   err2 := demuxer.Input(restOfPackHeader)  // решта даних
   if err2 != nil {
       t.Error("combined data should parse successfully")
   }
   ```

5. **Callback'и не тестуються**:
   ```go
   // OnPacket та OnFrame передаються у fields, але:
   // • Не встановлюються у тестах → не перевіряється їх виклик
   // • Критично для події-орієнтованої архітектури!
   
   // Краще:
   var packetCalled bool
   demuxer.OnPacket = func(pkg Display, err error) {
       packetCalled = true
       // Перевірити тип пакету, помилку, тощо
   }
   // Після Input():
   if !packetCalled {
       t.Error("OnPacket callback should be invoked")
   }
   ```

### 💡 Покращення тестів:

```go
func TestPSDemuxer_Input_Comprehensive(t *testing.T) {
    tests := []struct {
        name           string
        input          []byte
        wantErr        bool
        wantMPEG1      bool           // очікуваний прапорець mpeg1
        wantPacketType string         // очікуваний тип пакету у callback
        wantCacheLen   int            // очікуваний розмір кешу після парсингу
        verifyState    func(*testing.T, *PSDemuxer)  // додаткові перевірки стану
    }{
        {
            name:         "partial pack header",
            input:        ps1,  // тільки start code
            wantErr:      true,
            wantCacheLen: 4,    // 4 байти start code мають зберегтись
            verifyState: func(t *testing.T, d *PSDemuxer) {
                if len(d.cache) != 4 {
                    t.Errorf("cache len = %d, want 4", len(d.cache))
                }
                if d.pkg.Header != nil {
                    t.Error("header should not be parsed from partial data")
                }
            },
        },
        {
            name:         "complete pack header MPEG-2",
            input:        ps2,
            wantErr:      false,
            wantMPEG1:    false,
            wantCacheLen: 0,  // все оброблено, кеш порожній
            verifyState: func(t *testing.T, d *PSDemuxer) {
                if d.pkg.Header == nil {
                    t.Error("pack header should be parsed")
                }
                if d.pkg.Header.IsMpeg1 {
                    t.Error("should detect MPEG-2 format")
                }
            },
        },
        {
            name:         "MPEG-1 pack header",
            input:        ps7,
            wantErr:      false,
            wantMPEG1:    true,  // ключова перевірка!
            verifyState: func(t *testing.T, d *PSDemuxer) {
                if !d.mpeg1 {
                    t.Error("should detect MPEG-1 format")
                }
            },
        },
        // Додати тести для PES пакетів...
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            demuxer := NewPSDemuxer()
            
            // Setup callbacks для перевірки викликів
            var packetCalls []struct{ typ string; err error }
            demuxer.OnPacket = func(pkg Display, err error) {
                switch pkg.(type) {
                case *PSPackHeader: packetCalls = append(packetCalls, struct{typ string; err error}{"pack", err})
                case *System_header: packetCalls = append(packetCalls, struct{typ string; err error}{"system", err})
                case *Program_stream_map: packetCalls = append(packetCalls, struct{typ string; err error}{"psm", err})
                }
            }
            
            err := demuxer.Input(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Input() error = %v, wantErr = %v", err, tt.wantErr)
            }
            
            if demuxer.mpeg1 != tt.wantMPEG1 {
                t.Errorf("mpeg1 flag = %v, want %v", demuxer.mpeg1, tt.wantMPEG1)
            }
            
            if len(demuxer.cache) != tt.wantCacheLen {
                t.Errorf("cache len = %d, want %d", len(demuxer.cache), tt.wantCacheLen)
            }
            
            if tt.verifyState != nil {
                tt.verifyState(t, demuxer)
            }
        })
    }
}

// Тест інкрементального парсингу: розрізаний пакет
func TestPSDemuxer_Input_Incremental(t *testing.T) {
    demuxer := NewPSDemuxer()
    
    // Частина 1: тільки start code + частина заголовку
    part1 := []byte{0x00, 0x00, 0x01, 0xBA, 0x40, 0x01, 0x00}
    err1 := demuxer.Input(part1)
    if err1 != errNeedMore {
        t.Errorf("expected errNeedMore, got %v", err1)
    }
    if len(demuxer.cache) == 0 {
        t.Error("cache should contain partial data")
    }
    
    // Частина 2: решта заголовку
    part2 := []byte{0x01, 0x33, 0x44, 0xFF, 0xFF, 0xFF, 0xF1, 0xFF}
    err2 := demuxer.Input(part2)
    if err2 != nil {
        t.Errorf("combined data should parse: %v", err2)
    }
    if demuxer.pkg.Header == nil {
        t.Error("pack header should be parsed after combining chunks")
    }
}

// Тест PES пакету з відео даними
func TestPSDemuxer_Input_PESVideo(t *testing.T) {
    demuxer := NewPSDemuxer()
    
    var frames []struct{ data []byte; cid PS_STREAM_TYPE; pts, dts uint64 }
    demuxer.OnFrame = func(frame []byte, cid PS_STREAM_TYPE, pts, dts uint64) {
        frames = append(frames, struct{ data []byte; cid PS_STREAM_TYPE; pts, dts uint64 }{frame, cid, pts, dts})
    }
    
    // Створити мінімальний PES пакет з відео даними:
    // [start code: 0x000001E1][length: 0x0005][flags: 0x80][header_len: 0x03][PTS: 5 байт][payload: H.264 start code]
    pes := []byte{
        0x00, 0x00, 0x01, 0xE1,  // stream_id = 0xE1 (відео, канал 1)
        0x00, 0x0C,              // PES_packet_length = 12
        0x80,                    // flags: PTS only
        0x05,                    // PES_header_data_length = 5
        0x21, 0x00, 0x01, 0x00, 0x01,  // PTS = 1 (90 kHz)
        0x00, 0x00, 0x00, 0x01, 0x67,  // H.264 start code + SPS NAL type
    }
    
    err := demuxer.Input(pes)
    if err != nil {
        t.Errorf("PES parse error: %v", err)
    }
    
    // Перевірити, що кадр відправлено у callback
    if len(frames) == 0 {
        t.Error("OnFrame callback should be invoked for video PES")
    } else if frames[0].cid != PS_STREAM_H264 {
        // Перевірити heuristic detection
        t.Logf("detected codec: %v", frames[0].cid)
    }
}
```

---

## 🎯 4. Інтеграція з вашим CCTV HLS Processor

### 📍 Чому ці тести критичні для продакшену:

```
У реальному CCTV пайплайні:
1. Камери надсилають дані чанками по 1-2KB через TCP/UDP
2. PS пакети можуть розрізатися на межі чанків
3. Старі камери використовують MPEG-1, нові — MPEG-2
4. Відсутність PSM вимагає heuristic codec detection

Без цих тестів:
• Неповні пакети → помилки парсингу → втрата кадрів
• Неправильна детекція MPEG-1/2 → зсув у парсингу → пошкоджені NAL units
• Відсутність тестів на PES → непомічені баги у основній логіці демуксингу
```

### 📍 Як використовувати ці тести у CI/CD:

```yaml
# .github/workflows/test.yml
- name: Run PS demuxer tests
  run: go test -v -coverprofile=ps.cover ./mpeg2/...
  
- name: Check incremental parsing coverage
  run: |
    go tool cover -func=ps.cover | grep -E "(Input|demux)" 
    # Перевірити, що критичні функції мають >90% покриття

- name: Fuzz test for robustness
  run: |
    # Додати fuzz тест для генерації випадкових байтів
    go test -fuzz=FuzzPSDemuxer -fuzztime=30s ./mpeg2
```

### 📍 Додати property-based тести:

```go
// Генерувати випадкові послідовності байт і перевіряти, що парсер не панікує
func FuzzPSDemuxer(f *testing.F) {
    // Seed з валідними пакетами
    f.Add(ps2)  // valid pack header
    f.Add(ps5)  // pack + system header
    f.Add(ps6)  // PSM packet
    
    f.Fuzz(func(t *testing.T, data []byte) {
        demuxer := NewPSDemuxer()
        // Не повинно панікувати навіть на сміттєвих даних
        defer func() {
            if r := recover(); r != nil {
                t.Errorf("panic on input %x: %v", data, r)
            }
        }()
        _ = demuxer.Input(data)  // ігноруємо помилку, головне — стабільність
    })
}
```

---

## 🧭 Висновок: чому ці тести потребують розширення

| Проблема | Ризик | Вартість виправлення |
|----------|-------|---------------------|
| Перевірка тільки `err != nil` | Помилки парсингу "проковзують", дані втрачаються | Низька: додати перевірки стану демуксера |
| Відсутність тестів на PES | Основна логіка демуксингу не покрита → баги у продакшені | Середня: додати 3-5 тестів на відео/аудіо PES |
| Не тестується інкрементальність | Розрізані пакети не обробляються коректно → втрата кадрів | Низька: додати тест з двома викликами Input() |
| Callback'и не перевіряються | Події не доходять до пайплайну → "тихий" збій | Низька: встановити мок-функції у тестах |
| Спільний стан між тестами | Хибні позитиви/негативи через "брудний" стан | Низька: factory function для ізоляції стану |

> 🔑 **Головна ідея**: Ці тести — **перша лінія оборони** для вашого PS демуксера. Вони мають гарантувати не тільки "код не падає", а й "дані коректно екстрагуються". Без перевірки результатів парсингу, стану кешу та викликів callback'ів ви отримуєте лише ілюзію надійності.

💡 **Фінальна порада**: 
1. Додайте перевірки стану демуксера після `Input()` (чи розпарсився header, чи оновився cache)
2. Додайте тести на PES пакети з відео/аудіо даними — це основна логіка демуксингу!
3. Реалізуйте тест інкрементального парсингу: розрізаний пакет → два виклики `Input()` → успішна обробка
4. Встановіть мок-функції для `OnFrame`/`OnPacket` і перевіряйте їх виклик
5. Додайте fuzz-тест для перевірки стійкості до випадкових/пошкоджених даних

Це перетворить ці тести з "формальної перевірки" на "гарантію коректності демуксингу" для всього вашого MPEG-PS/HLS пайплайну.