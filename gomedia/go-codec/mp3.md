# 🎵 Глибокий розбір `codec/mp3.go` — парсинг MP3 бітстріму та ID3-тегів

Це **низькорівневий парсер стандарту MPEG Audio (Layer I/II/III)**, який дозволяє вашому CCTV HLS Processor обробляти аудіопотоки у форматі MP3: витягувати метадані, розраховувати розміри кадрів для сегментації та синхронізувати аудіо з відео. Розберемо архітектурно:

---

## 📦 1. Структура MP3 файлу: три шари даних

```
[MP3 File]
├─ ID3v2 Header (опціонально, на початку)
│  ├─ "ID3" magic bytes
│  ├─ Version + Revision
│  ├─ Flags (unsynchronisation, extended header, experimental)
│  └─ Size (28-bit sync-safe integer)
│
├─ Audio Frames (основний контент)
│  ├─ Frame Header (32 біти)
│  ├─ CRC (опціонально, 16 біт)
│  └─ Main Data (змінна довжина)
│
└─ ID3v1 Footer (опціонально, в кінці, рівно 128 байт)
   ├─ "TAG" magic
   ├─ Title/Artist/Album (по 30 байт)
   ├─ Year (4 байти), Comment (28 байт)
   └─ Track (1 байт), Genre (1 байт)
```

### 🔑 Ключова відмінність: потоковий режим

У вашому **CCTV HLS Processor** ви працюєте не з файлами, а з **потоками** (WebSocket → fMP4 → HLS). Тому:

| Компонент | У файловому режимі | У потоковому режимі (ваш випадок) |
|-----------|-------------------|-----------------------------------|
| **ID3v2** | Читати на початку файлу | **Пропускати/фільтрувати** — не потрібен у HLS |
| **ID3v1** | Читати в кінці файлу | **Ігнорувати** — може з'явитися тільки при завершенні запису |
| **Audio Frames** | Ітерувати по файлу | **Основний потік** — парсити "на льоту" для сегментації |

> 💡 **Практичне значення**: Функція `SplitMp3Frames` реалізує **потоковий парсинг** — вона приймає `[]byte` буфер і callback `onFrame`, що ідеально підходить для обробки чанків з WebSocket.

---

## 🧬 2. MP3 Frame Header — 32 біти, що керують декодуванням

### 🔍 Бітова структура заголовку (специфікація ISO/IEC 11172-3):

```
Біти 31-21: Syncword (0x7FF = 11 біт '1') ← маркер початку кадру
Біти 20-19: MPEG Version
            00 = MPEG 2.5, 01 = reserved, 10 = MPEG 2, 11 = MPEG 1
Біти 18-17: Layer
            00 = reserved, 01 = Layer III (MP3), 10 = Layer II, 11 = Layer I
Біт   16:   Protection (0 = CRC present, 1 = no CRC)
Біти 15-12: Bitrate Index (0-15, таблиця залежить від Version+Layer)
Біти 11-10: SampleRate Index (0-3, таблиця залежить від Version)
Біт    9:   Padding (0/1 — додає 1 байт до розміру кадру)
Біт    8:   Private (користувацький прапорець)
Біти  7-6:  Mode (00=Stereo, 01=Joint, 10=Dual, 11=Mono)
Біти  5-4:  Mode Extension (для Joint Stereo)
Біти  3-0:  Copyright/Original/Emphasis flags
```

### 🔧 Функція `DecodeMp3Head` — парсинг заголовку:

```go
func DecodeMp3Head(data []byte) (*MP3FrameHead, error) {
    bs := NewBitStream(data)
    
    // 1. Перевірка syncword (критично для виявлення початку кадру)
    syncWord := bs.GetBits(11)
    if syncWord != 0x7FF {  // 0b11111111111
        return nil, errors.New("mp3 frame must start with 0xFFE")
        // ⚠️ Помилка в повідомленні: має бути 0x7FF, а не 0xFFE
    }
    
    // 2. Розпарсити поля
    head.Version = uint8(bs.GetBits(2))  // 2 біти → мапінг на константи
    head.Layer = uint8(bs.GetBits(2))    // 2 біти → Layer I/II/III
    head.Protecttion = bs.GetBit()       // ⚠️ Опечатка: "Protecttion" замість "Protection"
    head.BitrateIndex = uint8(bs.GetBits(4))
    head.SampleRateIndex = uint8(bs.GetBits(2))
    // ... інші поля
    
    // 3. Розрахунок SampleSize (кількість семплів на кадр)
    if head.Layer == LAYER_1 {
        head.SampleSize = 384      // Layer I: 384 семпли
    } else if head.Layer == LAYER_2 {
        head.SampleSize = 1152     // Layer II: 1152 семпли
    } else { // Layer III (MP3)
        if head.Version == VERSION_MPEG_1 {
            head.SampleSize = 1152 // MPEG-1 Layer III
        } else {
            head.SampleSize = 576  // MPEG-2/2.5 Layer III ← половина!
        }
    }
    
    // 4. Розрахунок FrameSize у байтах
    br := head.GetBitRate()  // бітрейт у бітах/сек
    sr := head.GetSampleRate()  // семплрейт у Гц
    head.FrameSize = head.SampleSize / 8 * br / sr  // ⚠️ Потенційна проблема з цілочисельним діленням!
    
    // 5. Додати padding
    if head.Layer == LAYER_1 {
        head.FrameSize += int(head.Padding) * 4  // Layer I: padding кратний 4 байтам
    } else {
        head.FrameSize += int(head.Padding)       // інші: 1 байт
    }
    
    return head, nil
}
```

### 📐 Формула розрахунку розміру кадру:

```
FrameSize = (SampleSize / 8) × BitRate / SampleRate + Padding

Де:
- SampleSize: 384 (L1), 1152 (L2), 1152/576 (L3)
- BitRate: з таблиці × 1000 (переведення kbps → bps)
- SampleRate: 44100/48000/32000 Гц (залежить від версії)

Приклад для MPEG-1 Layer III, 128 kbps, 44100 Гц:
FrameSize = (1152 / 8) × 128000 / 44100 + 0
          = 144 × 2.902... ≈ 418 байт
```

> ⚠️ **Критична проблема**: У вашому коді цілочисельне ділення може дати неточний результат:
> ```go
> head.FrameSize = head.SampleSize / 8 * br / head.GetSampleRate()
> // Порядок операцій: ((1152/8) * 128000) / 44100 = (144 * 128000) / 44100 = 18432000 / 44100 = 417.96 → 417 ✓
> // Але якщо br/spf не ділиться націло — втрата точності!
> 
> // Безпечніше з округленням:
> head.FrameSize = int(float64(head.SampleSize) * float64(br) / (8.0 * float64(sr)) + 0.5)
> ```

---

## 📊 3. Таблиці бітрейтів та семплрейтів (з FFmpeg)

### 🔸 `BitRateTable[version][layer][index]`:

```go
var BitRateTable [2][3][16]int = [2][3][16]int{
    // [0] = MPEG-1
    {
        // Layer I:   [0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, -1]
        // Layer II:  [0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 380, -1]
        // Layer III: [0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, -1]
    },
    // [1] = MPEG-2/2.5 (нижчі бітрейти)
    {
        // Layer I:   [0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, -1]
        // Layer II:  [0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, -1] ← низькі!
        // Layer III: [0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, -1]
    },
}
```

**Практичне значення**: `BitrateIndex = 4` для MPEG-1 Layer III → 56 kbps → менший розмір кадру → більше кадрів на секунду.

### 🔸 `SampleRateTable[version][index]`:

```go
var SampleRateTable [3][4]int = [3][4]int{
    // MPEG-1:  [44100, 48000, 32000, 0(reserved)]
    // MPEG-2:  [22050, 24000, 16000, 0]
    // MPEG-2.5:[11025, 12000,  8000, 0] ← дуже низькі, для voice-only
}
```

> 💡 **Для CCTV**: Більшість камер використовують **MPEG-1 Layer III, 44100 Гц, 128 kbps** — це "золотий стандарт" для балансу якості/бітрейту.

---

## 🏷️ 4. ID3v2 Tag Parsing — синхронно-безпечний розмір

### 🔍 Структура ID3v2 заголовку (10 байт):

```
Байти 0-2:  "ID3" (magic bytes)
Байт   3:   Version (наприклад, 3 для ID3v2.3)
Байт   4:   Revision (зазвичай 0)
Байт   5:   Flags (битові прапорці)
Байти 6-9:  Size (28-bit sync-safe integer)
```

### 🔑 Sync-safe integer — чому 7 біт на байт?

```
ID3v2 використовує 7 біт на байт для розміру, щоб уникнути колізій з syncword (0xFF):

Звичайний 32-біт integer:  0x12 0x34 0x56 0x78
Sync-safe (7 біт/байт):    0x00 0x12 0x34 0x56 0x78 → кожен байт має старший біт = 0

Розрахунок у вашому коді:
var size uint32 = uint32(data[7])
size = size<<7 | uint32(data[8])  // зсув на 7 біт, не на 8!
size = size<<7 | uint32(data[9])
// Результат: 28-бітне число (макс. 256 MB)

Приклад: байти [0x00, 0x00, 0x02, 0x00] → 
  size = 0x00<<21 | 0x00<<14 | 0x02<<7 | 0x00 = 256 байт
```

> 💡 **Практичне значення**: У вашому `SplitMp3Frames` ID3v2 теги **пропускаються**, що правильно для потокового режиму — метадані не потрібні для HLS-сегментів.

---

## 🔄 5. `SplitMp3Frames` — потоковий ітератор кадрів

Це **ключова функція** для інтеграції з вашим пайплайном:

```go
func SplitMp3Frames(data []byte, onFrame func(head *MP3FrameHead, frame []byte)) error {
    for len(data) > 0 {
        // 1. Пропустити ID3v2 tag
        if bytes.HasPrefix(data, []byte{'I', 'D', '3'}) {
            if len(data) < 10 {
                return errors.New("ID3V2 tag head must has 10 bytes")
            }
            // Розрахувати розмір тега (sync-safe)
            var size uint32 = uint32(data[7])
            size = size<<7 | uint32(data[8])
            size = size<<7 | uint32(data[9])
            data = data[10+size:]  // пропустити заголовок + контент тега
            continue
        }
        
        // 2. Пропустити ID3v1 footer
        if bytes.HasPrefix(data, []byte{'T', 'A', 'G'}) {
            if len(data) < 128 {
                return errors.New("ID3V1 must has 128 bytes")
            }
            data = data[128:]
            continue
        }
        
        // 3. Парсити MP3 frame
        head, err := DecodeMp3Head(data)
        if err != nil {
            return err  // ⚠️ Помилка зупиняє весь потік!
        }
        
        // 4. Викликати callback з кадром
        if onFrame != nil {
            onFrame(head, data[:head.FrameSize])  // ← тут можна відправляти у segmentAssembler
        }
        
        // 5. Просунути буфер
        data = data[head.FrameSize:]
    }
    return nil
}
```

### 🎯 Використання у вашому `segmentAssembler`:

```go
// У handleAudioChunk для MP3:
func (sa *SegmentAssembler) handleMP3Chunk(data []byte) error {
    return codec.SplitMp3Frames(data, func(head *codec.MP3FrameHead, frame []byte) {
        // 1. Розрахунок тривалості кадру для PTS
        duration := time.Duration(float64(head.SampleSize) / float64(head.GetSampleRate()) * float64(time.Second))
        
        // 2. Додати кадр у поточний аудіо-сегмент
        sa.currentAudioSegment.AppendFrame(frame, sa.currentPTS)
        
        // 3. Оновити PTS для наступного кадру
        sa.currentPTS += duration
        
        // 4. Якщо сегмент досяг цільової довжини (наприклад, 4с) — фіналізувати
        if sa.currentAudioSegment.Duration() >= sa.targetSegmentDuration {
            sa.finalizeAudioSegment()
        }
    })
}
```

---

## 🐞 6. Потенційні баги та покращення

### ❗ Критичні проблеми:

1. **Опечатка в назві поля**:
   ```go
   type MP3FrameHead struct {
       Protecttion uint8  // ← має бути "Protection"
   }
   // Це не ламає функціонал, але порушує читабельність та може збити з пантелику при рефакторингу
   ```

2. **Некоректне повідомлення про помилку syncword**:
   ```go
   if syncWord != 0x7FF {
       return nil, errors.New("mp3 frame must start with 0xFFE")  // ← має бути 0x7FF!
   }
   ```

3. **Цілочисельне ділення у розрахунку FrameSize**:
   ```go
   head.FrameSize = head.SampleSize / 8 * br / head.GetSampleRate()
   // При br=128000, sr=44100: (144 * 128000) / 44100 = 417.96 → 417 (втрата 0.96 байта!)
   // Накопичення помилки: 100 кадрів × 0.96 = 96 байт розсинхронізації!
   
   // Краще з округленням:
   head.FrameSize = int(float64(head.SampleSize) * float64(br) / (8.0 * float64(head.GetSampleRate())) + 0.5)
   ```

4. **Відсутня валідація індексів перед доступом до таблиць**:
   ```go
   func (mp3 *MP3FrameHead) GetBitRate() int {
       // Якщо BitrateIndex = 15 (bad) → BitRateTable[i][layer][15] = -1 → негативний бітрейт!
       return BitRateTable[i][mp3.Layer-1][mp3.BitrateIndex] * 1000
   }
   
   // Краще додати перевірку:
   if mp3.BitrateIndex >= 15 || mp3.BitrateIndex == 0 { // 0=free, 15=bad
       return -1 // або повернути помилку
   }
   ```

5. **`SplitMp3Frames` зупиняється при першій помилці**:
   ```go
   head, err := DecodeMp3Head(data)
   if err != nil {
       fmt.Println(err)  // ← логування
       return err        // ← але це зупиняє обробку всього буфера!
   }
   
   // Для потокового режиму краще пропускати пошкоджені кадри:
   if err != nil {
       logger.Warn("skipping corrupted MP3 frame", "error", err)
       // Спробувати знайти наступний syncword (resync)
       nextSync := bytes.Index(data[1:], []byte{0xFF})
       if nextSync == -1 {
           return nil // кінець буфера
       }
       data = data[nextSync+1:]
       continue
   }
   ```

### 💡 Покращення для вашого пайплайну:

```go
// 1. Helper для отримання тривалості кадру у мікросекундах
func (mp3 *MP3FrameHead) FrameDurationUs() int64 {
    sr := mp3.GetSampleRate()
    if sr == 0 {
        return 0
    }
    return int64(float64(mp3.SampleSize) * 1_000_000 / float64(sr))
}

// 2. Валідація заголовку перед використанням
func (mp3 *MP3FrameHead) Validate() error {
    if mp3.Version == VERSION_RESERVED {
        return errors.New("reserved MPEG version")
    }
    if mp3.Layer == LAYER_RESERVED {
        return errors.New("reserved layer")
    }
    if mp3.BitrateIndex == 0 || mp3.BitrateIndex == 15 {
        return errors.New("invalid bitrate index (free/bad)")
    }
    if mp3.SampleRateIndex == 3 {
        return errors.New("reserved sample rate index")
    }
    return nil
}

// 3. Інтеграція з метриками
func (sa *SegmentAssembler) recordMP3Metrics(head *codec.MP3FrameHead) {
    metrics.AudioCodecProfile.WithLabelValues(
        fmt.Sprintf("MP3-Layer%d", head.Layer),
        fmt.Sprintf("MPEG-%d", head.Version),
    ).Inc()
    
    metrics.AudioBitrate.Observe(float64(head.GetBitRate()) / 1000) // kbps
    metrics.AudioSampleRate.Observe(float64(head.GetSampleRate()))
    
    if head.GetChannelCount() == 1 {
        metrics.AudioChannels.WithLabelValues("mono").Inc()
    } else {
        metrics.AudioChannels.WithLabelValues("stereo").Inc()
    }
}
```

---

## 🎯 7. Інтеграція з вашим CCTV HLS Processor

### 📍 У `segmentAssembler` — уніфікована обробка аудіо:

```go
func (sa *SegmentAssembler) handleAudioChunk(codecID codec.CodecID, data []byte) error {
    switch codecID {
    case codec.CODECID_AUDIO_AAC:
        return sa.handleAACChunk(data)  // ADTS parsing
        
    case codec.CODECID_AUDIO_MP3:
        return codec.SplitMp3Frames(data, func(head *codec.MP3FrameHead, frame []byte) {
            if err := head.Validate(); err != nil {
                logger.Warn("skipping invalid MP3 frame", "error", err)
                return
            }
            
            // Розрахунок PTS для цього кадру
            durationUs := head.FrameDurationUs()
            sa.processAudioFrame(frame, sa.currentAudioPTS, durationUs)
            sa.currentAudioPTS += durationUs
        })
        
    case codec.CODECID_AUDIO_OPUS:
        return sa.handleOpusChunk(data)  // Ogg/Opus parsing
    }
    return nil
}
```

### 📍 У `createTSSegment` — валідація аудіо-сегменту:

```go
func validateMP3Segment(frames []*MP3Frame) error {
    if len(frames) == 0 {
        return errors.New("empty audio segment")
    }
    
    // Перевірити консистентність параметрів (не можна мікшувати різні бітрейти у одному сегменті)
    first := frames[0]
    for i, f := range frames[1:] {
        if f.GetBitRate() != first.GetBitRate() || f.GetSampleRate() != first.GetSampleRate() {
            return fmt.Errorf("inconsistent audio params at frame %d: expected %d/%d, got %d/%d",
                i+1, first.GetBitRate(), first.GetSampleRate(),
                f.GetBitRate(), f.GetSampleRate())
        }
    }
    return nil
}
```

### 📍 У `VideoManifestProxy` — синхронізація аудіо/відео:

```go
func calculateAudioVideoSync(mp3Head *codec.MP3FrameHead, videoFPS float64) time.Duration {
    // Розрахунок "audio units per video frame" для точної синхронізації
    audioSamplesPerSec := mp3Head.GetSampleRate()
    videoFramesPerSec := videoFPS
    
    // Скільки аудіо-семплів припадає на один відео-кадр
    samplesPerVideoFrame := float64(audioSamplesPerSec) / videoFramesPerSec
    
    // Для MPEG-1 Layer III: 1152 семпли на кадр → скільки відео-кадрів на аудіо-кадр
    videoFramesPerAudioFrame := float64(mp3Head.SampleSize) / samplesPerVideoFrame
    
    return time.Duration(float64(time.Second) * videoFramesPerAudioFrame / videoFPS)
}
```

---

## 🧭 Висновок: чому цей код важливий для вашого проекту

| Компонент | Роль у CCTV HLS Processor |
|-----------|---------------------------|
| **MP3 Frame Parser** | Точний розрахунок розміру/тривалості кадрів → коректна сегментація без розривів |
| **ID3 Tag Handling** | Фільтрація метаданих → чисті HLS-сегменти без зайвих байтів |
| **Bitrate/SampleRate Tables** | Валідація вхідного потоку → відхилення некоректних аудіо-параметрів до потрапляння у FFmpeg |
| **Callback-based Splitting** | Потокова обробка → мінімальна затримка, підтримка великих буферів |

> 🔑 **Головна ідея**: Цей код — **аудіо-аналог** ваших H.264/H.265 парсерів. Він дає пайплайну семантичне розуміння аудіопотоку: де межі кадрів, яка їх тривалість, які параметри кодування. Без нього `segmentAssembler` працював би "наосліп", розрізаючи аудіо довільно → розсинхронізація з відео → артефакти відтворення.

💡 **Фінальна порада**: Додайте юніт-тест з реальним MP3-файлом (наприклад, запис з камери), який перевіряє:
1. Коректний розрахунок `FrameSize` для всіх комбінацій Version/Layer/Bitrate
2. Пропуск ID3-тегів без втрати аудіо-даних
3. Обробку пошкоджених кадрів (resync логіка)

Це вбереже від тонких багів, які проявляються тільки на реальних потоках з камер.