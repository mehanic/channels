# 🧪 Глибокий розбір `codec/h265_test.go` — тестування H.265/HEVC парсера

Це **комплексний набір інтеграційних тестів** для валідації парсингу та генерації H.265 параметрів. Він перевіряє критичні шляхи: декодування VPS/SPS/PPS, інкрементальне оновлення `hvcc`, та roundtrip-конвертацію. Розберемо архітектурно:

---

## 📦 1. Тестові дані: реальні H.265 NAL units у Annex-B форматі

### 🔍 Розбір байтових масивів:

```go
// VPS (Video Parameter Set) — NAL type 32 (0x40 = 0100 0000)
var vps []byte = []byte{
    0x00, 0x00, 0x00, 0x01,  // Start code (Annex-B)
    0x40,                    // NAL header: 0100 0000
                             // ├─ forbidden_zero_bit: 0 ✓
                             // ├─ nal_unit_type: 32 (VPS) ✓
                             // ├─ nuh_layer_id: 0
                             // └─ nuh_temporal_id_plus1: 1
    0x01, 0x0C, 0x01, 0xFF, 0xFF, 0x01, 0x60, ...  // Payload
}

// SPS (Sequence Parameter Set) — NAL type 33 (0x42 = 0100 0010)
var sps []byte = []byte{
    0x00, 0x00, 0x00, 0x01,  // Start code
    0x42,                    // NAL header: nal_unit_type = 33 (SPS)
    0x01, 0x01, 0x01, 0x60, ...  // Payload з роздільною здатністю, профілем, VUI
}

// PPS (Picture Parameter Set) — NAL type 34 (0x44 = 0100 0100)
var pps []byte = []byte{
    0x00, 0x00, 0x00, 0x01,  // Start code
    0x44,                    // NAL header: nal_unit_type = 34 (PPS)
    0x01, 0xC1, 0x72, 0xB4, 0x62, 0x40  // Payload
}
```

### 🔑 Чому `vps` vs `vps2` та `sps` vs `h265sps2`?

```go
// vps та vps2 відрізняються останнім байтом:
vps : ... 0x78, 0x99, 0x98, 0x09  // VPS ID = 0x09 & 0x0F = 1?
vps2: ... 0x78, 0x99, 0x98, 0x0A  // VPS ID = 0x0A & 0x0F = 2?

// Це тестує логіку UpdateVPS:
// 1. Додавання нового VPS (якщо ID різний)
// 2. Оновлення існуючого (якщо ID співпадає, але контент змінено)
// 3. Уникнення дублікатів (якщо байти ідентичні)
```

> 💡 **Практичне значення**: У реальному CCTV-потоці параметри можуть змінюватись динамічно (наприклад, переключення між денним/нічним режимом камери). Ваш `hvcc` має коректно обробляти такі зміни без створення дублікатів.

---

## 🧪 2. `TestVPS_Decode` / `TestH265RawSPS_Decode` / `TestH265RawPPS_Decode`

### 🔬 Структура тесту:

```go
func TestVPS_Decode(t *testing.T) {
    tests := []struct {
        name string
        args struct{ nalu []byte }  // Annex-B формат зі start code
    }{
        {name: "decode vps", args: args{nalu: vps}},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            vps := &VPS{}
            start, sc := FindStartCode(tt.args.nalu, 0)  // ← пошук 0x00000001
            ShowPacketHexdump(tt.args.nalu[start+int(sc):])  // ← дебаг-лог
            vps.Decode(tt.args.nalu[start+int(sc):])  // ← парсинг без start code
            t.Logf("%+v\n", vps)  // ← логування результату (але без assert!)
        })
    }
}
```

### ⚠️ Критична проблема: відсутність валідації

```go
// Тест логує результат, але НЕ перевіряє коректність!
t.Logf("%+v\n", vps)  // ← лише друк

// Краще: додати golden assertions
if vps.Vps_video_parameter_set_id != 1 {
    t.Errorf("VPS ID = %d, want 1", vps.Vps_video_parameter_set_id)
}
if vps.Ptl.General_profile_idc != 1 {  // Main profile
    t.Errorf("Profile = %d, want 1 (Main)", vps.Ptl.General_profile_idc)
}
if vps.TimeInfo.Vps_time_scale != 90000 {  // приклад: 90kHz clock
    t.Errorf("Time scale = %d, want 90000", vps.TimeInfo.Vps_time_scale)
}
```

### 🎯 Чому `FindStartCode` перед `Decode`?

```go
// Decode() очікує NALU без start code (як у fMP4/AVCC)
// Але тестові дані у Annex-B форматі (як у RTP/TS)
start, sc := FindStartCode(tt.args.nalu, 0)  // sc = 4 для 0x00000001
vps.Decode(tt.args.nalu[start+int(sc):])     // пропускаємо start code + NAL header
```

> 💡 **Архітектурний висновок**: Ваш парсер має чіткий контракт — `Decode([]byte)` працює з "сирым" NALU, а `FindStartCode` — це утиліта для підготовки даних. Це правильне розділення відповідальностей.

---

## 🔄 3. `TestHevc_Update` — інкрементальне оновлення hvcc

Це **найважливіший тест** для вашого HLS-процесора, який перевіряє динамічну обробку параметрів:

```go
func TestHevc_Update(t *testing.T) {
    hvcc := &HEVCRecordConfiguration{}
    
    // Сценарій: потік змінює параметри "на льоту"
    hvcc.UpdateVPS(vps)    // Додаємо перший VPS (ID=1)
    hvcc.UpdateVPS(vps2)   // Додаємо/оновлюємо VPS (ID=2 або оновлення ID=1)
    
    hvcc.UpdateSPS(sps)        // Додаємо перший SPS
    hvcc.UpdateSPS(h265sps2)   // Додаємо другий SPS (інша роздільна здатність?)
    
    hvcc.UpdatePPS(pps)    // Додаємо PPS
    hvcc.UpdatePPS(pps)    // ← дублікат: має бути проігноровано (оптимізація)
    hvcc.UpdatePPS(pps2)   // Новий PPS з іншими параметрами
    
    fmt.Printf("%+v\n", hvcc)  // Інспекція стану
}
```

### 🔍 Що перевіряється внутрішньо:

| Операція | Очікувана поведінка | Чому це критично |
|----------|---------------------|-----------------|
| `UpdateVPS(vps)` | Створити масив для NAL type 32, додати VPS | Без VPS неможливо ініціалізувати HEVC декодер |
| `UpdateVPS(vps2)` | Якщо ID співпадає → оновити контент; якщо ні → додати новий | Підтримка multi-layer coding або динамічної зміни профілю |
| `UpdatePPS(pps)` двічі | Другий виклик має повернутися одразу (байтове порівняння) | Уникнення дублікатів економить пам'ять та bandwidth |
| `UpdateSPS(h265sps2)` | Оновити `BitDepthLumaMinus8`, `ChromaFormat` у hvcc | Клієнт має знати, чи підтримує він 10-bit HDR |

### 🐞 Потенційна проблема у тесті:

```go
// Тест не перевіряє, чи hvcc містить очікувану кількість елементів!
// Після всіх Update* має бути:
// - 1-2 VPS arrays (NAL type 32)
// - 1-2 SPS arrays (NAL type 33)  
// - 1-2 PPS arrays (NAL type 34)

// Краще додати:
if len(hvcc.Arrays) != 3 {  // VPS + SPS + PPS
    t.Errorf("Expected 3 arrays, got %d", len(hvcc.Arrays))
}
vpsCount := countNalType(hvcc.Arrays, 32)
if vpsCount < 1 {
    t.Error("Missing VPS in hvcc")
}
```

---

## ♻️ 4. `TestHEVCRecordConfiguration_Decode_Encode` — roundtrip валідація

Це **золотий стандарт тестування серіалізації**:

```go
func TestHEVCRecordConfiguration_Decode_Encode(t *testing.T) {
    hvcc := &HEVCRecordConfiguration{}
    hvcc.Decode(tt.args.hevc)  // src: байти hvcc у бінарному форматі
    
    // Логування для дебагу
    t.Logf("%+v\n", hvcc)
    for _, a := range hvcc.Arrays {
        t.Logf("%+v\n", *a)  // Інспекція масивів NAL units
    }
    
    b, _ := hvcc.Encode()  // ← серіалізація назад у байти
    ShowPacketHexdump(b)   // Візуальна перевірка
    
    // Критична перевірка: байти мають співпадати побайтово!
    if !bytes.Equal(src, b) {
        t.Error("encode error")  // ← але не показує, де саме різниця!
    }
}
```

### 🔍 Розбір `src` (вхідні байти hvcc):

```
Offset  Hex     Field                          Значення
0       01      configurationVersion           1 ✓
1       01      profile_space(2)+tier(1)+profile_idc(5) = 0b00_0_00001 → profile_idc=1 (Main)
2-5     60 00...  general_profile_compatibility_flags (32 біти)
6-11    00 00...  general_constraint_indicator_flags (48 біт)
12      b4      general_level_idc = 180 (Level 4.0?)
13      f0      reserved(4) + min_spatial_segmentation_idc(12 bits, high byte)
14      00      min_spatial_segmentation_idc (low byte) = 0x0f00 = 3840?
...     ...     chromaFormat, bitDepth, frameRate info
~23     03      numOfArrays = 3 (VPS + SPS + PPS)
~24     fc      array_completeness(1)+reserved(1)+NAL_unit_type(6) = 0b1_0_111100 = type 60? ← підозріло!
```

> ⚠️ **Підозрілий момент**: `NAL_unit_type = 60` не є валідним для H.265 (максимум 63, але стандартні: 32=VPS, 33=SPS, 34=PPS). Можливо, це артефакт тестових даних або баг у парсингу.

### 💡 Покращення для дебагу:

```go
if !bytes.Equal(src, b) {
    // Знайти першу відмінність для швидкого дебагу
    for i := 0; i < len(src) && i < len(b); i++ {
        if src[i] != b[i] {
            t.Errorf("First diff at offset %d: src=0x%02x, got=0x%02x", i, src[i], b[i])
            t.Errorf("Context: src[%d:%d]=%x, got[%d:%d]=%x", 
                max(0, i-8), min(len(src), i+8), src[max(0,i-8):min(len(src),i+8)],
                max(0, i-8), min(len(b), i+8), b[max(0,i-8):min(len(b),i+8)])
            break
        }
    }
    if len(src) != len(b) {
        t.Errorf("Length mismatch: src=%d, got=%d", len(src), len(b))
    }
}
```

---

## 🔄 5. `TestHEVCRecordConfiguration_ToNalus` — конвертація hvcc → Annex-B

Цей тест перевіряє зворотну операцію: з `hvcc` структури назад у потік NAL units для відправки у WebSocket або FFmpeg:

```go
func TestHEVCRecordConfiguration_ToNalus(t *testing.T) {
    hvcc := &HEVCRecordConfiguration{}
    hvcc.Decode(src)  // Спочатку завантажуємо стан з байтів
    
    gotNalus := hvcc.ToNalus()  // Конвертація у Annex-B формат
    
    // Перевірка: результат має співпадати з очікуваним dst
    if !reflect.DeepEqual(gotNalus, tt.wantNalus) {
        t.Errorf("HEVCRecordConfiguration.ToNalus() = %v, want %v", gotNalus, tt.wantNalus)
    }
}
```

### 🔍 Розбір `dst` (очікуваний вихід):

```
dst починається з:
0x00, 0x00, 0x00, 0x01, 0x40, 0x01, 0x0c, 0x01...  // VPS у Annex-B
... потім:
0x00, 0x00, 0x00, 0x01, 0x42, 0x01, 0x01, 0x01...  // SPS у Annex-B  
... потім:
0x00, 0x00, 0x00, 0x01, 0x44, 0x01, 0xc1, 0x73...  // PPS у Annex-B
```

### 🎯 Практичне застосування у вашому пайплайні:

```go
// У createTSSegment: перед запуском ffprobe може знадобитися конвертувати hvcc у Annex-B
func prepareH265InitSegment(hvcc *codec.HEVCRecordConfiguration) ([]byte, error) {
    // ffprobe/FFmpeg часто очікують параметри у Annex-B форматі
    annexb := hvcc.ToNalus()
    
    // Додати у fMP4 'hvc1' track як extradata або окремий NAL stream
    return buildFMP4WithAnnexBParams(annexb), nil
}

// У WebSocket-відправці: клієнт може очікувати Annex-B для прямого декодування
func sendH265Params(ws *websocket.Conn, hvcc *codec.HEVCRecordConfiguration) error {
    annexb := hvcc.ToNalus()
    return ws.WriteMessage(websocket.BinaryMessage, annexb)
}
```

---

## 🐞 6. Потенційні баги та покращення тестів

### ❗ Критичні проблеми:

1. **Відсутність `t.Fatal` при помилках парсингу**:
   ```go
   b, _ := hvcc.Encode()  // ← помилка ігнорується!
   // Краще:
   b, err := hvcc.Encode()
   if err != nil {
       t.Fatalf("Encode failed: %v", err)
   }
   ```

2. **`reflect.DeepEqual` для байтів — неефективно**:
   ```go
   if !reflect.DeepEqual(gotNalus, tt.wantNalus)  // O(n) з великим overhead
   // Краще для []byte:
   if !bytes.Equal(gotNalus, tt.wantNalus)  // оптимізовано в stdlib
   ```

3. **Тести не перевіряють edge cases**:
   ```go
   // Додати тести для:
   // - Порожніх вхідних даних: UpdateVPS(nil)
   // - Некоректних NAL units: пошкоджені байти, неправильний start code
   // - Великих параметрів: SPS з розширеними VUI, multiple layers
   ```

### 💡 Покращення для production-тестування:

```go
// 1. Додати fuzz-тести для стійкості парсера
func FuzzH265SPSDecode(f *testing.F) {
    f.Add(sps)
    f.Add(h265sps2)
    f.Fuzz(func(t *testing.T, data []byte) {
        var rawsps codec.H265RawSPS
        defer func() {
            if r := recover(); r != nil {
                t.Errorf("Panic on input %x: %v", data, r)
            }
        }()
        // Не повинно панікувати навіть на сміттєвих даних
        rawsps.Decode(data)
    })
}

// 2. Бенчмарки для продуктивності
func BenchmarkH265SPSDecode(b *testing.B) {
    start, sc := FindStartCode(sps, 0)
    nalu := sps[start+int(sc):]
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        var rawsps codec.H265RawSPS
        rawsps.Decode(nalu)
    }
}

// 3. Інтеграційний тест з реальним ffprobe
func TestH265InitSegment_FFmpegCompatible(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping ffmpeg integration test")
    }
    
    hvcc := codec.NewHEVCRecordConfiguration()
    hvcc.UpdateVPS(vps)
    hvcc.UpdateSPS(sps)
    hvcc.UpdatePPS(pps)
    
    extradata, _ := hvcc.Encode()
    initSegment := buildFMP4InitSegment(extradata, "hvc1")
    
    // Записати у тимчасовий файл та перевірити ffprobe
    tmpfile := writeTempFile(initSegment)
    defer os.Remove(tmpfile)
    
    cmd := exec.Command("ffprobe", "-v", "error", "-show_streams", tmpfile)
    output, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("ffprobe failed: %v\n%s", err, output)
    }
    
    // Перевірити, що потік розпізнано як hevc
    if !bytes.Contains(output, []byte("codec_name=hevc")) {
        t.Errorf("ffprobe did not recognize HEVC stream:\n%s", output)
    }
}
```

---

## 🎯 7. Інтеграція з вашим CCTV HLS Processor

### 📍 У `segmentAssembler` — уніфікована логіка для H.264/H.265:

```go
func (sa *SegmentAssembler) handleParameterSet(codecID codec.CodecID, nalu []byte) error {
    start, sc := codec.FindStartCode(nalu, 0)
    payload := nalu[start+int(sc):]
    
    switch codecID {
    case codec.CODECID_VIDEO_H264:
        switch codec.ExtractH264NalType(payload) {
        case codec.H264_NAL_SPS:
            return sa.updateH264SPS(payload)
        case codec.H264_NAL_PPS:
            return sa.updateH264PPS(payload)
        }
        
    case codec.CODECID_VIDEO_H265:
        var hdr codec.H265NaluHdr
        bs := codec.NewBitStream(payload)
        hdr.Decode(bs)
        
        switch hdr.Nal_unit_type {
        case codec.H265_NAL_VPS:
            return sa.updateH265VPS(payload)
        case codec.H265_NAL_SPS:
            return sa.updateH265SPS(payload)
        case codec.H265_NAL_PPS:
            return sa.updateH265PPS(payload)
        }
    }
    return nil
}
```

### 📍 У `createTSSegment` — генерація init-сегменту:

```go
func createH265InitSegment(hvcc *codec.HEVCRecordConfiguration) ([]byte, error) {
    extradata, err := hvcc.Encode()
    if err != nil {
        return nil, fmt.Errorf("failed to encode hvcc: %w", err)
    }
    
    // Побудувати fMP4 init сегмент з 'hvc1' track
    return mp4.BuildInitSegment(mp4.TrackConfig{
        Codec:      "hvc1",
        Extradata:  extradata,
        Width:      hvcc.GetWidth(),   // helper method
        Height:     hvcc.GetHeight(),  // helper method
        Timescale:  90000,             // HLS standard
    }), nil
}
```

### 📍 У метриках — моніторинг параметрів потоку:

```go
func (sa *SegmentAssembler) recordH265Metrics(sps *codec.H265RawSPS, vps *codec.VPS) {
    // Профіль/рівень для сумісності
    metrics.VideoCodecProfile.WithLabelValues(
        fmt.Sprintf("HEVC-Main%d", sps.Ptl.General_profile_idc),
        fmt.Sprintf("L%d", sps.Ptl.General_level_idc),
    ).Inc()
    
    // Роздільна здатність для адаптивного бітрейту
    metrics.VideoResolution.WithLabelValues(
        fmt.Sprintf("%dx%d", sps.Pic_width_in_luma_samples, sps.Pic_height_in_luma_samples),
    ).Inc()
    
    // HDR підтримка (10/12-bit)
    if sps.Bit_depth_luma_minus8 > 0 {
        metrics.VideoHDR.WithLabelValues(
            fmt.Sprintf("%d-bit", 8+sps.Bit_depth_luma_minus8),
        ).Inc()
    }
    
    // Timing info для PTS корекції
    if vps.TimeInfo.Vps_time_scale > 0 {
        fps := float64(vps.TimeInfo.Vps_time_scale) / 
               float64(vps.TimeInfo.Vps_num_units_in_tick)
        metrics.VideoFPS.Observe(fps)
    }
}
```

---

## 🧭 Висновок: чому ці тести — фундамент надійності

| Тип тесту | Що захищає | Вартість помилки без тесту |
|-----------|------------|---------------------------|
| `TestVPS_Decode` | Коректний парсинг ієрархії параметрів | Неправильний VPS → SPS не може посилатися → потік не декодується |
| `TestHevc_Update` | Інкрементальне оновлення без дублікатів | Дублікати параметрів → зростання розміру init.mp4 → bandwidth waste |
| `TestDecode_Encode` | Сумісність серіалізації | Розбіжність байтів → ffprobe помиляється → сегмент відкидається |
| `TestToNalus` | Конвертація для зовнішніх систем | Неправильний Annex-B → WebSocket-клієнт не може декодувати |

> 🔑 **Головна ідея**: Ці тести — **жива документація** вашого H.265 підтримки. Вони показують:
> 1. Які формати вхідних даних очікуються (Annex-B для парсингу, hvcc для fMP4)
> 2. Як обробляються динамічні зміни параметрів (Update* методи)
> 3. Як гарантується сумісність з інструментами (ffprobe, браузерні MSE)

Без них будь-яка зміна у `BitStream` або логіці `UpdateSPS` може непомітно зламати продакшен — і ви дізнаєтесь про це тільки коли нові H.265 камери перестануть відтворюватись.

💡 **Фінальна порада**: Додайте `golden files` для `src`/`dst` тестів — збережіть очікувані `t.Logf()` виводи у `testdata/hvcc_roundtrip.json`. Це дозволить автоматично детектувати регресії при рефакторингу парсера, навіть якщо байтовий roundtrip продовжує проходити.