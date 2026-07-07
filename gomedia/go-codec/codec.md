# 🎬 Глибоке пояснення `codec.go`

Цей файл — **фундаментальний шар абстракції** для роботи з медіа-кодеками у вашому CCTV HLS Processor. Він визначає типізовані ідентифікатори кодеків та структуру бітстріму для H.264/H.265. Розберемо детально:

---

## 🔢 1. `CodecID` — уніфікована ідентифікація кодеків

```go
type CodecID int

const (
    CODECID_VIDEO_H264 CodecID = iota        // 0
    CODECID_VIDEO_H265                        // 1
    CODECID_VIDEO_VP8                         // 2

    CODECID_AUDIO_AAC CodecID = iota + 98     // 98
    CODECID_AUDIO_G711A                         // 99
    CODECID_AUDIO_G711U                         // 100
    CODECID_AUDIO_OPUS                          // 101
    CODECID_AUDIO_MP3                           // 102

    CODECID_UNRECOGNIZED = 999
)
```

### 🔍 Чому така структура?

| Аспект | Пояснення |
|--------|-----------|
| **`iota + 98`** | Розділяє простір ID: відео (0-2), аудіо (98-102). Це дозволяє швидко класифікувати тип: `if id >= 98 → audio` без додаткових перевірок. |
| **Type-safe** | `CodecID` — окремий тип, а не `int`. Компілятор не дозволить випадково передати `CodecID` туди, де очікується інший тип. |
| **Ефективність** | `int` — це нативний тип, порівняння в `switch` компілюється в jump-table (O(1)). |
| **Розширюваність** | Легко додати новий кодек, не ламаючи існуючу логіку. |

### 🎯 Практичне застосування у вашому пайплайні:

```go
// У segmentAssembler при обробці fMP4-фрагменту:
func (sa *SegmentAssembler) handleSample(track Track, data []byte) {
    switch track.CodecID {
    case codec.CODECID_VIDEO_H264:
        sa.processH264NALs(data)  // парсинг NAL, пошук ключових кадрів
    case codec.CODECID_AUDIO_AAC:
        sa.mergeOrphanAudio(data) // синхронізація за seqNum
    }
}
```

---

## 🧬 2. H.264 NAL Unit Types — атомарні одиниці відеопотоку

```go
type H264_NAL_TYPE int

const (
    H264_NAL_P_SLICE      = 1  // Залежний кадр (передбачення)
    H264_NAL_I_SLICE      = 5  // Ключовий кадр (intra) ⭐
    H264_NAL_SEI          = 6  // Додаткова інформація (таймкоди, метадані)
    H264_NAL_SPS          = 7  // Sequence Parameter Set 🗝️
    H264_NAL_PPS          = 8  // Picture Parameter Set 🗝️
    H264_NAL_AUD          = 9  // Access Unit Delimiter (маркер початку кадру)
)
```

### 🔬 Що таке NAL (Network Abstraction Layer)?

H.264 потік — це послідовність **NAL units**, кожен з яких має:
```
[3-byte header][payload]
├─ forbidden_zero_bit: 1 біт
├─ nal_ref_idc: 2 біти (пріоритет для декодування)
├─ nal_unit_type: 5 біт → це наші константи вище
```

### 🎯 Чому це критично для HLS?

| NAL Type | Роль у вашому пайплайні |
|----------|-------------------------|
| **I_SLICE (5)** | Точка старту для нового сегменту. `segmentAssembler` чекає на I-frame, щоб розпочати новий `.ts` файл. |
| **SPS/PPS (7,8)** | Параметри декодування. Мають бути в кожному сегменті або перед першим ключовим кадром. Без них FFmpeg відхилить сегмент. |
| **SEI (6)** | Може містити `timecode` або `user_data_registered_itu_t_t35` для синхронізації з субтитрами. |
| **AUD (9)** | Опціональний маркер. Допомагає детектувати початок кадру при парсингі "сирих" бітстрімів. |

### 🛠 Приклад використання:

```go
func extractH264NalType(data []byte) codec.H264_NAL_TYPE {
    if len(data) < 1 {
        return codec.H264_NAL_RESERVED
    }
    // Перший байт після start code (0x00000001 або 0x000001)
    return codec.H264_NAL_TYPE(data[0] & 0x1F) // mask: 00011111
}

// У segmentFinalizer: перевірка, чи сегмент валідний
func (sf *SegmentFinalizer) validateH264Segment(nals [][]byte) error {
    hasSPS, hasPPS, hasIDR := false, false, false
    for _, nal := range nals {
        switch extractH264NalType(nal) {
        case codec.H264_NAL_SPS: hasSPS = true
        case codec.H264_NAL_PPS: hasPPS = true
        case codec.H264_NAL_I_SLICE: hasIDR = true
        }
    }
    if !hasSPS || !hasPPS {
        return errors.New("missing SPS/PPS - segment not decodable")
    }
    return nil
}
```

---

## 🌀 3. H.265/HEVC NAL Types — складніша ієрархія

```go
type H265_NAL_TYPE int

const (
    // Звичайні кадри
    H265_NAL_Slice_TRAIL_N = 0  // Залежний, без посилань на майбутнє
    H265_NAL_Slice_TRAIL_R = 1  // Залежний, може бути референсом
    
    // IDR-кадри (точка синхронізації)
    H265_NAL_SLICE_IDR_W_RADL = 19  // IDR з RADL
    H265_NAL_SLICE_IDR_N_LP   = 20  // IDR без leading pictures
    
    // Параметри
    H265_NAL_VPS = 32  // Video Parameter Set (новий у H.265!)
    H265_NAL_SPS = 33
    H265_NAL_PPS = 34
    
    // Метадані
    H265_NAL_SEI = 39
    H265_NAL_SEI_SUFFIX = 40
)
```

### 🔑 Ключові відмінності від H.264:

| Аспект | H.264 | H.265 |
|--------|-------|-------|
| **Parameter Sets** | SPS + PPS | **VPS + SPS + PPS** (VPS для multi-layer/stream scalability) |
| **IDR типи** | Один тип (5) | **Два типи**: `IDR_W_RADL` (з leading pictures) та `IDR_N_LP` (без) |
| **Кодування NAL** | 5 біт тип | **6 біт тип** (більше простору для розширення) |
| **Ефективність** | Базова | Краща компресія, але складніший парсинг |

### ⚠️ Пастка з `iota` у H.265:

```go
// У вашому коді:
H265_NAL_SLICE_BLA_W_LP H265_NAL_TYPE = iota + 6  // 6
// ... ще 4 значення ...
H265_NAL_VPS H265_NAL_TYPE = iota + 16            // 32 ✓
H265_NAL_SEI H265_NAL_TYPE = iota + 19            // 39 ✓
```

Це працює, але **крихко**: якщо додати/видалити константу в середині — всі наступні зсунуться. Безпечніша альтернатива:

```go
// Явне присвоєння (рекомендовано для протоколів)
const (
    H265_NAL_VPS H265_NAL_TYPE = 32
    H265_NAL_SPS H265_NAL_TYPE = 33
    H265_NAL_PPS H265_NAL_TYPE = 34
)
```

---

## 🔄 4. Інтеграція з вашим пайплайном

### 📦 У `segmentAssembler` (keyframe-based сегментація):

```go
func (sa *SegmentAssembler) shouldStartNewSegment(nalType codec.H264_NAL_TYPE) bool {
    // Нова сегментація тільки на I-frame + наявність SPS/PPS
    return nalType == codec.H264_NAL_I_SLICE && sa.hasParamSets
}
```

### 🎞 У `createTSSegment` (валідація через ffprobe):

```go
// Перед запуском ffprobe - швидка попередня перевірка
func preValidateTS(nals [][]byte, codecID codec.CodecID) error {
    switch codecID {
    case codec.CODECID_VIDEO_H264:
        return validateH264ParamSets(nals) // шукаємо SPS/PPS
    case codec.CODECID_VIDEO_H265:
        return validateH265ParamSets(nals) // шукаємо VPS/SPS/PPS
    }
    return nil
}
```

### 🌐 У WebSocket-повідомленнях (оптимізація трафіку):

```go
// SubtitleMessage може включати codec_hint для клієнта:
type SubtitleMessage struct {
    // ... інші поля ...
    VideoCodec codec.CodecID `json:"video_codec,omitempty"` // клієнт знає, який декодер ініціалізувати
}
```

---

## 🧪 5. Тестування та відладка

### 🔍 Helper для дебагу:

```go
func DebugNalInfo(codecID codec.CodecID, data []byte) string {
    switch codecID {
    case codec.CODECID_VIDEO_H264:
        nalType := codec.H264_NAL_TYPE(data[0] & 0x1F)
        return fmt.Sprintf("H264 NAL type=%d (%s)", nalType, nalType.String())
    case codec.CODECID_VIDEO_H265:
        nalType := codec.H265_NAL_TYPE((data[0] >> 1) & 0x3F) // 6 біт
        return fmt.Sprintf("H265 NAL type=%d (%s)", nalType, nalType.String())
    }
    return "unknown codec"
}
```

### 🧪 Unit-тест для `CodecString`:

```go
func TestCodecString(t *testing.T) {
    tests := []struct{
        in codec.CodecID
        want string
    }{
        {codec.CODECID_VIDEO_H264, "H264"},
        {codec.CODECID_AUDIO_AAC, "AAC"},
        {codec.CODECID_UNRECOGNIZED, "UNRECOGNIZED"},
        {9999, "UNRECOGNIZED"}, // edge case
    }
    for _, tt := range tests {
        if got := codec.CodecString(tt.in); got != tt.want {
            t.Errorf("CodecString(%d) = %q, want %q", tt.in, got, tt.want)
        }
    }
}
```

---

## 🎯 Висновок: навіщо це все?

1. **Безпека типів** — компілятор ловить помилки на етапі збірки.
2. **Швидкість** — `switch` на `CodecID` компілюється в O(1) jump table.
3. **Читабельність** — `CODECID_VIDEO_H264` зрозуміліше за магічне `0`.
4. **Масштабованість** — легко додати AV1, AC-3 тощо.
5. **Протокольна сумісність** — NAL типи відповідають специфікаціям ITU-T H.264/H.265.

Цей файл — **міст між сирими бітами з WebSocket та логікою вашого HLS-процесора**. Без нього `segmentAssembler` не міг би коректно детектувати ключові кадри, а `createTSSegment` генерував би невалідні `.ts` файли, які FFmpeg відхиляє.

> 💡 **Порада**: Додайте методи `IsVideo()`, `IsKeyFrame(nalType)` та `String()` для NAL типів — це спростить логіку в інших модулях і зменшить кількість `switch`.