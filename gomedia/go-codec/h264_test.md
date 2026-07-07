# 🧪 Глибокий розбір `codec/h264_test.go` — тестування H.264 парсера

Це **інтеграційний набір тестів** для валідації низькорівневого парсингу H.264. Він перевіряє критичні шляхи: декодування SPS, витягнення роздільної здатності та генерацію `avcC` для fMP4. Розберемо архітектурно:

---

## 📦 1. Тестові дані: реальні байти з продакшену

### 🔍 `sps1` vs `sps2` — різниця у форматуванні:

```go
// sps1: "сирий" NALU без start code (як у fMP4/AVCC)
var sps1 []byte = []byte{0x64, 0x00, 0x28, 0xAC, ...}
//                    ↑
//              NAL header: 0x64 = 0110 0100
//              ├─ forbidden_zero_bit: 0 ✓
//              ├─ nal_ref_idc: 3 (highest priority) ✓
//              └─ nal_unit_type: 7 (SPS) ✓

// sps2: Annex-B формат зі start code (як у RTP/TS)
var sps2 []byte = []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x28, ...}
//                    ↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑↑
//              Start code: 0x00000001
//              Далі: 0x67 = NAL header для SPS (0x64 & 0x7F = 0x67? Ні, це інший SPS)
```

> 💡 **Чому це важливо**: Ваш парсер має коректно обробляти **обидва формати**. Функції `FindStartCode()` та `CovertRbspToSodb()` саме для цього.

---

## 🧪 2. `TestSPS_Decode` — перевірка бітового парсингу

```go
func TestSPS_Decode(t *testing.T) {
    tests := []struct {
        name string
        sps  *SPS
        args args  // *BitStream з тестовими даними
    }{
        {
            name: "sps1",
            sps:  new(SPS),
            args: args{ bs: NewBitStream(sps1) },
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tt.sps.Decode(tt.args.bs)  // ← виклик парсера
            t.Log(tt.sps)               // ← логування результату
        })
    }
}
```

### 🔬 Що перевіряється:

| Крок | Очікувана поведінка | Чому це критично |
|------|---------------------|-----------------|
| `bs.Uint8(8)` для `Profile_idc` | Має прочитати `0x64` (100 = High Profile) | Визначає, чи є розширені поля (chroma_format, bit_depth) |
| `bs.ReadUE()` для `Seq_parameter_set_id` | Має коректно декодувати код Голомба | Це ID для посилання з PPS/слайсів |
| Умовні поля (`if Profile_idc == 100`) | Мають парситись тільки для High Profile | Без цього — зсув бітового курсору → всі наступні поля неправильні |

### 🐞 Потенційна проблема у тесті:

```go
// Тест логує результат, але НЕ перевіряє значення!
t.Log(tt.sps)  // ← лише друк, немає assert

// Краще:
if tt.sps.Profile_idc != 100 {
    t.Errorf("Profile_idc = %d, want 100 (High)", tt.sps.Profile_idc)
}
if tt.sps.Pic_width_in_mbs_minus1 != 119 { // для 1920px: (119+1)*16=1920
    t.Errorf("Width mismatch")
}
```

> 💡 **Порада**: Додайте `golden file` з очікуваними значеннями всіх полів SPS — це дозволить детектувати регресії при зміні парсера.

---

## 🎯 3. `TestGetH264Resolution` — валідація формули роздільної здатності

```go
func TestGetH264Resolution(t *testing.T) {
    tests := []struct {
        name       string
        args       args  // sps []byte у Annex-B форматі
        wantWidth  uint32  // очікувана ширина
        wantHeight uint32  // очікувана висота
    }{
        {
            name: "Resolution1",
            args: args{ sps: sps2 },  // Annex-B формат зі start code
            wantWidth: 1920, wantHeight: 1080,  // Full HD
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            gotWidth, gotHeight := GetH264Resolution(tt.args.sps)
            t.Logf("%d,%d", gotWidth, gotHeight)  // ← корисно для дебагу
            
            // ← критичні перевірки
            if gotWidth != tt.wantWidth {
                t.Errorf("GetH264Resolution() gotWidth = %v, want %v", gotWidth, tt.wantWidth)
            }
            if gotHeight != tt.wantHeight {
                t.Errorf("GetH264Resolution() gotHeight = %v, want %v", gotHeight, tt.wantHeight)
            }
        })
    }
}
```

### 🔢 Як працює `GetH264Resolution` (нагадую):

```go
// 1. Знайти start code та пропустити NAL header
start, sc := FindStartCode(sps, 0)  // для sps2: start=4, sc=4
sodb := CovertRbspToSodb(sps[start+int(sc)+1:])  // пропустити 0x67 (NAL header)

// 2. Розпарсити SPS
var s SPS
s.Decode(NewBitStream(sodb))

// 3. Застосувати формулу ITU-T H.264 Annex E:
widthInSample := (uint32(s.Pic_width_in_mbs_minus1) + 1) * 16
widthCrop := uint32(s.Frame_crop_left_offset)*2 + uint32(s.Frame_crop_right_offset)*2
width = widthInSample - widthCrop

heightInSample := ((2 - uint32(s.Frame_mbs_only_flag)) * (uint32(s.Pic_height_in_map_units_minus1) + 1) * 16)
heightCrop := uint32(s.Frame_crop_top_offset + s.Frame_crop_bottom_offset) * 2  // ← у вашому коді баг: було "-" замість "+"
height = heightInSample - heightCrop
```

### 📐 Розрахунок для вашого `sps2` (очікується 1920×1080):

| Поле SPS | Значення | Розрахунок |
|----------|----------|------------|
| `Pic_width_in_mbs_minus1` | 119 | `(119+1)*16 = 1920` ✓ |
| `Pic_height_in_map_units_minus1` | 67 | `(67+1)*16 = 1088` |
| `Frame_mbs_only_flag` | 1 | `2-1 = 1` → `1*1088 = 1088` |
| `Frame_crop_top_offset + bottom_offset` | 4 | `4*2 = 8` → `1088-8 = 1080` ✓ |

> ⚠️ **Пастка**: Якщо у вашому коді залишився баг з `-` замість `+` у `heightCrop`, тест **не пройде** для відео з кропом. Переконайтесь, що виправили:
> ```go
> // НЕПРАВИЛЬНО:
> heightCrop := uint32(s.Frame_crop_bottom_offset)*2 - uint32(s.Frame_crop_top_offset)*2
> // ПРАВИЛЬНО:
> heightCrop := (uint32(s.Frame_crop_top_offset) + uint32(s.Frame_crop_bottom_offset)) * 2
> ```

---

## 🧩 4. `TestCreateH264AVCCExtradata` — генерація `avcC` для fMP4

Це **найскладніший тест**, який перевіряє створення `AVCDecoderConfigurationRecord`:

```go
var spss1 [][]byte = [][]byte{{0x00, 0x00, 0x00, 0x01, 0x67, 0x64, 0x00, 0x0A, ...}}  // SPS у Annex-B
var ppss1 [][]byte = [][]byte{{0x00, 0x00, 0x00, 0x01, 0x68, 0xE8, 0x43, 0x8F, ...}}  // PPS у Annex-B
var want1 []byte = []byte{0x01, 0x64, 0x00, 0x0A, 0xFF, 0xE1, 0x00, 0x19, ...}        // Очікуваний avcC
```

### 🔍 Розбір очікуваного `want1` (avcC структура):

```
Offset  Hex     Field                          Значення
0       01      configurationVersion           1 (завжди)
1       64      AVCProfileIndication           100 (High Profile)
2       00      profile_compatibility          0
3       0A      AVCLevelIndication             10 (Level 3.1)
4       FF      reserved (6 bits) + lengthSizeMinusOne (2 bits=3 → 4-byte length)
5       E1      reserved (3 bits) + numOfSequenceParameterSets (5 bits=1)
6-7     00 19   sequenceParameterSetLength     25 байт (big-endian)
8-32    67...   SPS NALU (без start code!)     25 байт
33      01      numOfPictureParameterSets      1
34-35   00 07   pictureParameterSetLength      7 байт
36-42   68...   PPS NALU (без start code!)     7 байт
```

### 🎯 Чому цей тест критичний для HLS:

1. **fMP4 ініціалізація**: Без коректного `avcC` у треку `avc1` — браузер не ініціалізує MSE SourceBuffer.
2. **FFmpeg валідація**: `createTSSegment` використовує ffprobe, який вимагає валідні параметри.
3. **Мульти-SPS/PPS**: Функція підтримує масиви `[][]byte` — важливо для потоків зі зміною профілю (наприклад, адаптивний бітрейт).

### 🐞 Потенційні проблеми у тесті:

```go
// 1. Ігнорування помилки:
if got, _ := CreateH264AVCCExtradata(...); !reflect.DeepEqual(got, tt.want) {
//                          ↑
//                     помилка ігнорується! Може приховати panic або неправильну обробку

// Краще:
got, err := CreateH264AVCCExtradata(tt.args.spss, tt.args.ppss)
if err != nil {
    t.Fatalf("CreateH264AVCCExtradata() error = %v", err)
}

// 2. reflect.DeepEqual не показує диф:
t.Errorf("CreateH264AVCCExtradata() = %v, want %v", got, tt.want)
// Краще для байтів:
if !bytes.Equal(got, tt.want) {
    t.Errorf("Diff at offset: %d", findFirstDiff(got, tt.want))
    t.Errorf("Got:  %x", got)
    t.Errorf("Want: %x", tt.want)
}
```

---

## 🛠 5. Покращення для production-тестування

### ✅ Додати edge-case тести:

```go
// Тест для interlaced відео (frame_mbs_only_flag = 0)
func TestGetH264Resolution_Interlaced(t *testing.T) {
    // SPS з 1080i: pic_height_in_map_units_minus1 = 67, frame_mbs_only_flag = 0
    // Очікувана висота: ((2-0)*(67+1)*16) - crop = 2176 - crop = 1080 (після деінтерлейсу)
}

// Тест для SPS без кропу
func TestGetH264Resolution_NoCrop(t *testing.T) {
    // Frame_cropping_flag = 0 → crop offsets = 0
}

// Тест для порожніх spss/ppss
func TestCreateH264AVCCExtradata_Empty(t *testing.T) {
    _, err := CreateH264AVCCExtradata(nil, ppss1)
    if err == nil {
        t.Error("Expected error for empty SPS")
    }
}
```

### ✅ Додати fuzz-тести для парсера:

```go
func FuzzSPSDecode(f *testing.F) {
    f.Add(sps1)
    f.Add(sps2)
    f.Fuzz(func(t *testing.T, data []byte) {
        bs := NewBitStream(data)
        var sps SPS
        // Не повинно панікувати при будь-яких вхідних даних
        defer func() {
            if r := recover(); r != nil {
                t.Errorf("Panic on input %x: %v", data, r)
            }
        }()
        sps.Decode(bs)
    })
}
```

### ✅ Бенчмарки для продуктивності:

```go
func BenchmarkSPSDecode(b *testing.B) {
    bs := NewBitStream(sps1)
    var sps SPS
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        bs.Rewind()  // уявна функція скидання курсору
        sps.Decode(bs)
    }
}

func BenchmarkGetH264Resolution(b *testing.B) {
    for i := 0; i < b.N; i++ {
        GetH264Resolution(sps2)
    }
}
```

> 💡 **Результат бенчмарків** допоможе виявити вузькі місця: якщо `SPS.Decode` займає >100ns, це може вплинути на latency при обробці 30fps потоку (33ms на кадр).

---

## 🎯 6. Інтеграція з вашим пайплайном: як використовувати ці тести

### 📍 У CI/CD:

```yaml
# .github/workflows/test.yml
- name: Run codec tests
  run: go test -v -coverprofile=codec.cover ./codec/...
  
- name: Check coverage
  run: go tool cover -func=codec.cover | grep total
  
- name: Benchmark regression check
  run: |
    go test -bench=. -benchmem ./codec | tee bench_new.txt
    # Порівняти з bench_old.txt, alert якщо деградація >10%
```

### 📍 У розробці нових функцій:

```go
// Коли додаєте підтримку H.265, скопіюйте патерн:
func TestGetH265Resolution(t *testing.T) {
    // Аналогічно до H.264, але з іншими формулами (H.265 використовує CTU замість макроблоків)
}
```

### 📍 Для дебагу продакшен-проблем:

```go
// Якщо клієнт скаржиться на "невалідний сегмент", додайте логування:
func debugSPS(sps []byte) {
    width, height := GetH264Resolution(sps)
    logger.With("width", width, "height", height).Info("SPS resolution")
    
    // Додати у метрики Prometheus
    metrics.VideoResolution.WithLabelValues(fmt.Sprintf("%dx%d", width, height)).Inc()
}
```

---

## 🧭 Висновок: чому ці тести — інвестиція, а не витрати

| Тип тесту | Що захищає | Вартість помилки без тесту |
|-----------|------------|---------------------------|
| `TestSPS_Decode` | Коректність бітового парсингу | Неправильний `frame_num` → розсинхронізація сегментів → розриви у HLS |
| `TestGetH264Resolution` | Валідність роздільної здатності | Неправильний crop → розтягнуте відео у клієнта → скарги користувачів |
| `TestCreateH264AVCCExtradata` | Сумісність з fMP4/FFmpeg | `avcC` без SPS/PPS → ffprobe помиляється → сегмент відкидається → пропуски у плейлисті |

> 🔑 **Головна ідея**: Ці тести — **документація через код**. Вони показують:
> 1. Які формати вхідних даних очікуються (Annex-B vs AVCC)
> 2. Які значення вважаються валідними (1920×1080, High Profile)
> 3. Як має виглядати вихід (структура `avcC`)

Без них будь-яка зміна у `BitStream` або `SPS.Decode` може непомітно зламати продакшен — і ви дізнаєтесь про це тільки коли клієнти перестануть бачити відео.

💡 **Фінальна порада**: Додайте `golden files` для всіх тестів — збережіть очікувані `t.Log()` виводи у файли `testdata/sps1_decoded.json`. Це дозволить автоматично детектувати регресії при зміні логіки парсингу.