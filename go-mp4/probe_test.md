# 🧪 Тести `probe.go`: Перевірка аналізу та інспекції MP4-файлів

Це **комплексний тест-сьют** для модуля `probe.go` бібліотеки `go-mp4`, який перевіряє коректність роботи **швидкого аналізу (probing) MP4/fMP4 файлів** для отримання метаданих, інформації про доріжки, фрагменти та ключові кадри.

---

## 🎯 Коротка відповідь

> **Ці тести гарантують, що функція `Probe()` коректно витягує ключову інформацію з будь-якого MP4-файлу** — від базових метаданих до складних конфігурацій кодеків, таймінгів та зашифрованих потоків — без необхідності повного парсингу всього вмісту.

---

## 📋 Огляд тестових функцій

### 🔹 `TestProbe` — головний інтеграційний тест для звичайного MP4

```go
func TestProbe(t *testing.T) {
    f, err := os.Open("./testdata/sample.mp4")  // 🔹 Тестовий файл
    require.NoError(t, err)
    defer f.Close()

    info, err := Probe(f)  // 🔹 Головний виклик: аналіз файлу
    require.NoError(t, err)

    // 🔹 Перевірка базових метаданих файлу
    assert.Equal(t, BrandISOM(), info.MajorBrand)  // ✅ "isom"
    assert.Equal(t, uint32(0x200), info.MinorVersion)  // ✅ 512
    require.Len(t, info.CompatibleBrands, 4)  // ✅ 4 сумісних бренди
    assert.Equal(t, BrandISOM(), info.CompatibleBrands[0])  // "isom"
    assert.Equal(t, BrandISO2(), info.CompatibleBrands[1])  // "iso2"
    assert.Equal(t, BrandAVC1(), info.CompatibleBrands[2])  // "avc1"
    assert.Equal(t, BrandMP41(), info.CompatibleBrands[3])  // "mp41"
    assert.False(t, info.FastStart)  // ❌ moov після mdat
    assert.Equal(t, uint32(1000), info.Timescale)  // ✅ 1000 одиниць/сек
    assert.Equal(t, uint64(1024), info.Duration)  // ✅ 1.024 секунди

    // 🔹 Перевірка доріжок (2 треки: відео + аудіо)
    require.Len(t, info.Tracks, 2)

    // 🔹 Відео-доріжка (TrackID=1, H.264)
    assert.Equal(t, uint32(1), info.Tracks[0].TrackID)
    assert.Equal(t, uint32(10240), info.Tracks[0].Timescale)  // ✅ 10240 для відео
    assert.Equal(t, uint64(10240), info.Tracks[0].Duration)  // ✅ 1.024 сек @ 10240
    assert.Equal(t, CodecAVC1, info.Tracks[0].Codec)  // ✅ H.264
    assert.Equal(t, uint8(1), info.Tracks[0].AVC.ConfigurationVersion)  // ✅ Версія 1
    assert.Equal(t, uint8(0x64), info.Tracks[0].AVC.Profile)  // ✅ High Profile (100)
    assert.Equal(t, uint8(0), info.Tracks[0].AVC.ProfileCompatibility)  // ✅ Сумісність
    assert.Equal(t, uint8(0xc), info.Tracks[0].AVC.Level)  // ✅ Level 3.1 (12)
    assert.Equal(t, uint16(0x04), info.Tracks[0].AVC.LengthSize)  // ✅ 4 байти для NAL length
    assert.Equal(t, uint16(320), info.Tracks[0].AVC.Width)  // ✅ 320px ширина
    assert.Equal(t, uint16(180), info.Tracks[0].AVC.Height)  // ✅ 180px висота
    assert.False(t, info.Tracks[0].Encrypted)  // ❌ Не зашифровано
    require.Len(t, info.Tracks[0].EditList, 1)  // ✅ 1 запис edit list
    assert.Equal(t, int64(2048), info.Tracks[0].EditList[0].MediaTime)  // ✅ Пропуск 2048 одиниць
    assert.Equal(t, uint64(1000), info.Tracks[0].EditList[0].SegmentDuration)  // ✅ Тривалість 1 сек
    require.Len(t, info.Tracks[0].Samples, 10)  // ✅ 10 семплів (кадрів)
    assert.Equal(t, uint32(3679), info.Tracks[0].Samples[0].Size)  // ✅ Перший кадр: 3679 байт
    assert.Equal(t, uint32(15), info.Tracks[0].Samples[9].Size)  // ✅ Останній кадр: 15 байт
    assert.Equal(t, uint32(1024), info.Tracks[0].Samples[0].TimeDelta)  // ✅ 1024 одиниць між кадрами
    assert.Equal(t, int64(2048), info.Tracks[0].Samples[0].CompositionTimeOffset)  // ✅ CTS offset для B-фреймів
    require.Len(t, info.Tracks[0].Chunks, 9)  // ✅ 9 чанків
    assert.Equal(t, uint64(48), info.Tracks[0].Chunks[0].DataOffset)  // ✅ Перший чанк @ 48 байт
    assert.Equal(t, uint32(2), info.Tracks[0].Chunks[0].SamplesPerChunk)  // ✅ 2 кадри у першому чанку

    // 🔹 Аудіо-доріжка (TrackID=2, AAC)
    assert.Equal(t, uint32(2), info.Tracks[1].TrackID)
    assert.Equal(t, uint32(44100), info.Tracks[1].Timescale)  // ✅ 44.1 kHz для аудіо
    assert.Equal(t, uint64(45124), info.Tracks[1].Duration)  // ✅ ~1.023 сек @ 44100
    assert.Equal(t, CodecMP4A, info.Tracks[1].Codec)  // ✅ AAC
    assert.Equal(t, uint8(0x40), info.Tracks[1].MP4A.OTI)  // ✅ ObjectTypeIndication = AAC
    assert.Equal(t, uint8(2), info.Tracks[1].MP4A.AudOTI)  // ✅ Audio OTI = AAC LC (2)
    assert.Equal(t, uint16(2), info.Tracks[1].MP4A.ChannelCount)  // ✅ Стерео
    assert.False(t, info.Tracks[1].Encrypted)  // ❌ Не зашифровано

    // 🔹 Перевірка фрагментів (для звичайного MP4 їх немає)
    require.Len(t, info.Segments, 0)

    // 🔹 Пошук ключових кадрів (IDR) у відео-доріжці
    idxs, err := FindIDRFrames(f, info.Tracks[0])
    require.NoError(t, err)
    require.Len(t, idxs, 1)  // ✅ 1 ключовий кадр
    assert.Equal(t, 0, idxs[0])  // ✅ Перший кадр — IDR
}
```

**📊 Що тестується:**

| Категорія | Поле | Очікуване значення | Чому це важливо |
|-----------|------|-------------------|----------------|
| **Бренди файлу** | `MajorBrand` | `"isom"` | ✅ Визначає базовий стандарт (ISO Base Media File Format) |
| **Сумісні бренди** | `CompatibleBrands` | `["isom","iso2","avc1","mp41"]` | ✅ Гарантує сумісність з різними плеєрами |
| **FastStart** | `FastStart` | `false` | ✅ Вказує, чи оптимізовано файл для web-стрімінгу |
| **Глобальні таймінги** | `Timescale/Duration` | `1000 / 1024` | ✅ Базові одиниці часу для всього файлу |
| **Відео-доріжка** | `Codec/AVC.*` | `H.264 High@3.1, 320x180` | ✅ Параметри для ініціалізації декодера |
| **Edit List** | `MediaTime/SegmentDuration` | `2048 / 1000` | ✅ Пропуск початку для синхронізації |
| **Семпли** | `Size/TimeDelta/CTS` | `3679/1024/2048` | ✅ Таймінги та розміри кадрів для синхронізації |
| **Чанки** | `DataOffset/SamplesPerChunk` | `48 / 2` | ✅ Фізичне розташування даних у файлі |
| **Аудіо-доріжка** | `OTI/AudOTI/ChannelCount` | `0x40 / 2 / 2` | ✅ Параметри AAC LC стерео для декодера |
| **Ключові кадри** | `FindIDRFrames()` | `[0]` | ✅ Перший кадр — IDR для швидкого seek |

---

### 🔹 `TestProbeEncryptedVideo` / `TestProbeEncryptedAudio` — тест зашифрованих доріжок

```go
func TestProbeEncryptedVideo(t *testing.T) {
    f, err := os.Open("./testdata/sample_init.encv.mp4")  // 🔹 Зашифроване відео
    info, err := Probe(f)
    
    require.Len(t, info.Tracks, 2)
    assert.Equal(t, CodecAVC1, info.Tracks[0].Codec)
    assert.True(t, info.Tracks[0].Encrypted)  // ✅ Відео зашифроване
    assert.Equal(t, CodecMP4A, info.Tracks[1].Codec)
    assert.False(t, info.Tracks[1].Encrypted)  // ❌ Аудіо не зашифроване
}

func TestProbeEncryptedAudio(t *testing.T) {
    f, err := os.Open("./testdata/sample_init.enca.mp4")  // 🔹 Зашифроване аудіо
    info, err := Probe(f)
    
    assert.Equal(t, CodecAVC1, info.Tracks[0].Codec)
    assert.False(t, info.Tracks[0].Encrypted)  // ❌ Відео не зашифроване
    assert.Equal(t, CodecMP4A, info.Tracks[1].Codec)
    assert.True(t, info.Tracks[1].Encrypted)  // ✅ Аудіо зашифроване
}
```

**🎯 Призначення**: Перевірити, що `Probe()` коректно визначає прапорець `Encrypted` для окремих доріжок — критично для обробки DRM-контенту.

---

### 🔹 `TestProbeWithFMP4` / `TestProbeFra` — тест фрагментованих файлів (fMP4)

```go
func TestProbeWithFMP4(t *testing.T) {
    f, err := os.Open("./testdata/sample_fragmented.mp4")  // 🔹 fMP4 файл
    info, err := Probe(f)
    
    require.Equal(t, 2, len(info.Tracks))  // ✅ 2 доріжки
    require.Equal(t, 8, len(info.Segments))  // ✅ 8 фрагментів

    // 🔹 Перевірка першого відео-сегмента
    assert.Equal(t, uint32(1), info.Segments[0].TrackID)  // ✅ Відео-доріжка
    assert.Equal(t, uint64(1227), info.Segments[0].MoofOffset)  // ✅ Зміщення moof
    assert.Equal(t, uint64(0), info.Segments[0].BaseMediaDecodeTime)  // ✅ Початковий час
    assert.Equal(t, uint32(9000), info.Segments[0].DefaultSampleDuration)  // ✅ Типова тривалість
    assert.Equal(t, uint32(3), info.Segments[0].SampleCount)  // ✅ 3 семпли у фрагменті
    assert.Equal(t, uint32(27000), info.Segments[0].Duration)  // ✅ Загальна тривалість
    assert.Equal(t, int32(18000), info.Segments[0].CompositionTimeOffset)  // ✅ CTS offset
    assert.Equal(t, uint32(1054), info.Segments[0].Size)  // ✅ Розмір фрагмента у байтах

    // 🔹 Перевірка першого аудіо-сегмента
    assert.Equal(t, uint32(2), info.Segments[1].TrackID)  // ✅ Аудіо-доріжка
    assert.Equal(t, uint32(8830), info.Segments[1].DefaultSampleDuration)  // ✅ Інша типова тривалість
    assert.Equal(t, uint32(5), info.Segments[1].SampleCount)  // ✅ 5 семплів
    // ... інші перевірки ...
}
```

**🎯 Призначення**: Перевірити коректність аналізу **фрагментованих файлів (fMP4)**, де кожен сегмент має власні таймінги — критично для HLS/DASH стрімінгу.

---

### 🔹 `TestDetectAACProfile` — тест визначення профілю AAC

```go
func TestDetectAACProfile(t *testing.T) {
    testCases := []struct {
        name           string
        esds           *Esds
        expectedOTI    uint8
        expectedAudOTI uint8
    }{
        {
            name: "40.2",  // 🔹 AAC LC
            esds: &Esds{
                Descriptors: []Descriptor{
                    {Tag: DecoderConfigDescrTag, DecoderConfigDescriptor: &DecoderConfigDescriptor{ObjectTypeIndication: 0x40}},
                    {Tag: DecSpecificInfoTag, Data: []byte{0x10, 0x00}},  // 🔹 AudioSpecificConfig
                },
            },
            expectedOTI:    0x40,  // ✅ AAC
            expectedAudOTI: 2,     // ✅ AAC LC
        },
        {
            name: "40.5 ExtAudType=5 SBR=1 SFI=0x0",  // 🔹 HE-AAC (AAC+SBR)
            esds: &Esds{
                Descriptors: []Descriptor{
                    {Tag: DecoderConfigDescrTag, DecoderConfigDescriptor: &DecoderConfigDescriptor{ObjectTypeIndication: 0x40}},
                    {Tag: DecSpecificInfoTag, Data: []byte{
                        0x10, 0x02, 0xb7,  // 🔹 syncExtensionType=0x2b7
                        0x2c, 0x00,        // 🔹 extAudioObjectType=5, sbr=1
                    }},
                },
            },
            expectedOTI:    0x40,
            expectedAudOTI: 5,  // ✅ SBR (Spectral Band Replication)
        },
        {
            name: "40.29 ExtAudType=5 SBR=1 SFI=0xf PS=1",  // 🔹 HE-AAC v2 (AAC+SBR+PS)
            esds: &Esds{
                Descriptors: []Descriptor{
                    {Tag: DecoderConfigDescrTag, DecoderConfigDescriptor: &DecoderConfigDescriptor{ObjectTypeIndication: 0x40}},
                    {Tag: DecSpecificInfoTag, Data: []byte{
                        0x10, 0x02, 0xb7,  // 🔹 syncExtensionType=0x2b7
                        0x2f, 0xc0, 0x00, 0x00, 0x2a, 0x44,  // 🔹 PS=1
                    }},
                },
            },
            expectedOTI:    0x40,
            expectedAudOTI: 29,  // ✅ PS (Parametric Stereo)
        },
        // ... ще 3 кейси для edge cases ...
    }
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            oti, audOTI, err := detectAACProfile(tc.esds)
            require.NoError(t, err)
            assert.Equal(t, tc.expectedOTI, oti)
            assert.Equal(t, tc.expectedAudOTI, audOTI)
        })
    }
}
```

**🎯 Що тестується:**

| Назва кейсу | Опис | Очікуваний `AudOTI` | Чому це важливо |
|-------------|------|-------------------|----------------|
| `40.2` | Базовий AAC LC | `2` | ✅ Найпоширеніший профіль для стрімінгу |
| `40.5 ... SBR=1` | HE-AAC (AAC+SBR) | `5` | ✅ Економія бітрейту для низьких швидкостей |
| `40.29 ... PS=1` | HE-AAC v2 (AAC+SBR+PS) | `29` | ✅ Ще більша економія для моно-аудіо |
| `40.2 sample-frequency-index=0xf` | Explicit frequency | `2` | ✅ Обробка рідкісних частот дискретизації |
| `40.42` | Extended audio object type | `42` | ✅ Підтримка екзотичних кодеків |

**🔑 Ключова логіка `detectAACProfile()`**:
```
🔹 Крок 1: Перевірка ObjectTypeIndication = 0x40 (AAC)
│
▼
🔹 Крок 2: Бітовий парсинг AudioSpecificConfig:
   • audioObjectType (5 біт, або 11 біт якщо 0x1F)
   • samplingFrequencyIndex (4 біти, або 24 біти якщо 0xF)
   • channelConfig (4 біти)
   • [опціонально] syncExtensionType, extAudioObjectType, sbr, ps
│
▼
🔹 Крок 3: Визначення профілю:
   • audioObjectType = 2 → AAC LC (AudOTI=2)
   • extAudioObjectType = 5 + sbr=1 → HE-AAC (AudOTI=5)
   • extAudioObjectType = 5 + sbr=1 + ps=1 → HE-AAC v2 (AudOTI=29)
```

---

### 🔹 `TestSamplesGetBitrate` / `TestSegmentsGetBitrate` — тест розрахунку бітрейту

```go
func TestSamplesGetBitrate(t *testing.T) {
    // 🔹 Порожній список → 0
    assert.Equal(t, uint64(0), Samples{}.GetBitrate(100))

    // 🔹 Розрахунок: (сума розмірів * 8 * timescale) / сума тривалостей
    // = (900 байт * 8 * 100) / 50 одиниць = 14400 біт/сек
    assert.Equal(t, uint64(14400),
        Samples{
            {TimeDelta: 10, Size: 100},
            {TimeDelta: 10, Size: 200},
            {TimeDelta: 10, Size: 300},
            {TimeDelta: 10, Size: 100},
            {TimeDelta: 10, Size: 200},
        }.GetBitrate(100))
}

func TestSegmentsGetBitrate(t *testing.T) {
    // 🔹 Фільтрація за trackID=2: сегменти з Size=[100,200,300,100,200], Duration=[10,10,10,10,10]
    // = (900 * 8 * 100) / 50 = 14400 біт/сек
    assert.Equal(t, uint64(14400),
        Segments{
            {TrackID: 1, Duration: 10, Size: 300},
            {TrackID: 2, Duration: 10, Size: 100},  // ✅ Враховується
            {TrackID: 2, Duration: 10, Size: 200},  // ✅
            {TrackID: 1, Duration: 10, Size: 200},
            {TrackID: 2, Duration: 10, Size: 300},  // ✅
            {TrackID: 3, Duration: 10, Size: 700},
            {TrackID: 2, Duration: 10, Size: 100},  // ✅
            {TrackID: 1, Duration: 10, Size: 800},
            {TrackID: 2, Duration: 10, Size: 200},  // ✅
        }.GetBitrate(2, 100))
}
```

**🎯 Формула бітрейту**:
```
бітрейт = (загальний_розмір_у_байтах * 8 * timescale) / загальна_тривалість_у_одиницях
```

**🎯 Призначення**: Перевірити коректність розрахунку **середнього бітрейту** для адаптивного стрімінгу або валідації обмежень мережі.

---

### 🔹 `TestSamplesGetMaxBitrate` / `TestSegmentsGetMaxBitrate` — тест пікового бітрейту

```go
func TestSamplesGetMaxBitrate(t *testing.T) {
    // 🔹 Скользяче вікно 20 одиниць часу
    // Найвищий бітрейт у вікні [100+200+300] байт за 30 одиниць:
    // = (600 * 8 * 100) / 30 = 16000? Ні, алгоритм складніший...
    // ✅ Очікуємо 20000 біт/сек для вікна [200+300] за 20 одиниць
    assert.Equal(t, uint64(20000),
        Samples{
            {TimeDelta: 10, Size: 100},
            {TimeDelta: 10, Size: 200},  // ✅ Початок вікна
            {TimeDelta: 10, Size: 300},  // ✅ Кінець вікна
            {TimeDelta: 10, Size: 100},
            {TimeDelta: 10, Size: 200},
        }.GetMaxBitrate(100, 20))  // 🔹 timeDelta=20 одиниць
}
```

**🎯 Алгоритм пікового бітрейту**:
```
🔹 Скользяче вікно фіксованої тривалості (timeDelta)
🔹 Для кожного положення вікна:
   • Розрахувати бітрейт = (розмір_у_вікні * 8 * timescale) / тривалість_вікна
   • Запам'ятати максимум
🔹 Повернути максимальний бітрейт
```

**🎯 Призначення**: Знайти **піковий бітрейт** для валідації мережевих обмежень або оптимізації буферизації.

---

## 🔍 Як це працює разом: Повний потік

```
🔹 Вхід: io.ReadSeeker (файл, буфер, мережа)
│
▼
🔹 Probe():
   ├── 🔹 ExtractBoxes() → швидкий пошук ключових боксів (ftyp, moov, trak, moof...)
   │
   ├── 🔹 Для ftyp:
   │   • Парсинг брендів, версій → info.MajorBrand, info.CompatibleBrands
   │
   ├── 🔹 Для moov/mvhd:
   │   • Глобальні таймінги → info.Timescale, info.Duration
   │   • FastStart перевірка → info.FastStart = !mdatAppeared
   │
   ├── 🔹 Для кожного trak:
   │   • Виклик probeTrak() → детальний аналіз доріжки
   │   • Збір інформації: codec, timescale, duration, samples, chunks, AVC/MP4A config
   │
   ├── 🔹 Для кожного moof:
   │   • Виклик probeMoof() → аналіз фрагмента
   │   • Збір: BaseMediaDecodeTime, SampleCount, Duration, Size, CTS offset
   │
   ▼
🔹 Вихід: *ProbeInfo з усією зведеною інформацією
```

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Автоматична валідація вхідного сегмента

```go
func validateIncomingSegment(filePath string) error {
    f, err := os.Open(filePath)
    if err != nil { return err }
    defer f.Close()
    
    // 🔹 Крок 1: Швидкий пробінг
    info, err := mp4.Probe(f)
    if err != nil {
        return fmt.Errorf("failed to probe: %w", err)
    }
    
    // 🔹 Крок 2: Перевірка відео-доріжки
    var videoTrack *mp4.Track
    for _, t := range info.Tracks {
        if t.Codec == mp4.CodecAVC1 {
            videoTrack = t
            break
        }
    }
    if videoTrack == nil {
        return fmt.Errorf("no H.264 video track")
    }
    
    // 🔹 Крок 3: Перевірка роздільності
    if videoTrack.AVC == nil {
        return fmt.Errorf("missing AVC configuration")
    }
    if videoTrack.AVC.Width < 640 || videoTrack.AVC.Height < 360 {
        return fmt.Errorf("resolution too low: %dx%d", 
            videoTrack.AVC.Width, videoTrack.AVC.Height)
    }
    
    // 🔹 Крок 4: Перевірка бітрейту
    bitrate := videoTrack.Samples.GetBitrate(videoTrack.Timescale)
    if bitrate > 5_000_000 {  // 5 Mbps limit
        log.Printf("⚠️  High bitrate: %d bps", bitrate)
    }
    
    // 🔹 Крок 5: Перевірка зашифрованості
    if videoTrack.Encrypted {
        log.Printf("🔐 Encrypted video track — DRM required")
        // 🔹 Додаткова логіка для DRM...
    }
    
    return nil
}
```

---

### 🔹 Приклад 2: Генерація HLS-плейлиста з ключовими кадрами

```go
func generateHLSPlaylistWithKeyframes(filePath string) (string, error) {
    f, err := os.Open(filePath)
    if err != nil { return "", err }
    defer f.Close()
    
    // 🔹 Пробінг файлу
    info, err := mp4.Probe(f)
    if err != nil { return "", err }
    
    // 🔹 Пошук відео-доріжки
    var videoTrack *mp4.Track
    for _, t := range info.Tracks {
        if t.Codec == mp4.CodecAVC1 {
            videoTrack = t
            break
        }
    }
    if videoTrack == nil {
        return "", fmt.Errorf("no video track")
    }
    
    // 🔹 Пошук ключових кадрів (IDR)
    keyframes, err := mp4.FindIDRFrames(f, videoTrack)
    if err != nil { return "", err }
    
    // 🔹 Генерація плейлиста
    var sb strings.Builder
    sb.WriteString("#EXTM3U\n")
    sb.WriteString("#EXT-X-VERSION:6\n")
    sb.WriteString("#EXT-X-TARGETDURATION:4\n")
    
    timescale := videoTrack.Timescale
    var currentTime uint64
    
    for _, kfIdx := range keyframes {
        if kfIdx >= len(videoTrack.Samples) { break }
        
        // 🔹 Розрахунок часу ключового кадру
        for i := 0; i < kfIdx; i++ {
            currentTime += uint64(videoTrack.Samples[i].TimeDelta)
        }
        extinf := float64(currentTime) / float64(timescale)
        
        // 🔹 Додавання сегмента у плейлист
        sb.WriteString(fmt.Sprintf("#EXTINF:%.3f,\n", extinf))
        sb.WriteString(fmt.Sprintf("segment_%06d.ts\n", kfIdx))
    }
    
    sb.WriteString("#EXT-X-ENDLIST\n")
    return sb.String(), nil
}
```

---

### 🔹 Приклад 3: Адаптивний стрімінг на основі бітрейту

```go
func selectOptimalBitrate(segments mp4.Segments, trackID uint32, 
                         timescale uint32, maxBitrate uint64) *mp4.Segment {
    
    var best *mp4.Segment
    var bestScore float64
    
    for _, seg := range segments {
        if seg.TrackID != trackID { continue }
        
        // 🔹 Розрахунок бітрейту сегмента
        bitrate := 8 * uint64(seg.Size) * uint64(timescale) / uint64(seg.Duration)
        
        // 🔹 Score: чим ближче до maxBitrate без перевищення — тим краще
        if bitrate <= maxBitrate {
            score := float64(bitrate) / float64(maxBitrate)
            if score > bestScore {
                bestScore = score
                best = seg
            }
        }
    }
    
    return best  // 🔹 Повертаємо оптимальний сегмент або nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Ігнорування `FastStart` | Веб-плеєри довго завантажують початок відео | Перевіряйте `info.FastStart` і попереджайте користувачів |
| Неправильна обробка `LengthSizeMinusOne` | Неправильний парсинг NAL units → помилки декодування | Пам'ятайте: `LengthSize = LengthSizeMinusOne + 1` (0→1, 1→2, 2→4) |
| Забути перевірку `ctts` для B-фреймів | Десинхронізація аудіо/відео при наявності B-фреймів | Завжди обробляйте `CompositionTimeOffset` з `ctts` |
| Неправильний розрахунок бітрейту | Перевищення лімітів мережі → буферизація | Використовуйте `GetMaxBitrate()` з розумним `timeDelta` (напр. 1 секунда) |
| Ігнорування `Encrypted` прапорця | Спроба декодувати DRM-контент → помилка | Перевіряйте `track.Encrypted` перед спробою декодування |

---

## 📋 Чекліст для вашого проекту

```
[ ] При прийомі нових сегментів:
    • Викликайте Probe() для швидкої валідації структури
    • Перевіряйте FastStart для web-оптимізації
    • Логувайте codec, resolution, bitrate для моніторингу

[ ] Для HLS-генерації:
    • Використовуйте FindIDRFrames() для пошуку ключових точок
    • Розраховуйте тривалість сегментів через timescale
    • Додавайте #EXT-X-KEY якщо track.Encrypted=true

[ ] Для адаптивного стрімінгу:
    • Розраховуйте бітрейт через GetBitrate() / GetMaxBitrate()
    • Фільтруйте сегменти за trackID для мульти-доріжкових файлів
    • Обирайте оптимальний бітрейт на основі мережевих умов

[ ] Для дебагу:
    • Логуйте зведену інформацію: log.Printf("📊 %dx%d@%dbps", w, h, bitrate)
    • Перевіряйте наявність обов'язкових боксів: tkhd, mdhd, stts, stsc
    • Використовуйте String() для бокс-типів у логах: log.Printf("📦 %s", boxType)

[ ] Для тестування:
    • Створюйте тестові MP4 з різними конфігураціями (H.264/AAC, різні роздільності)
    • Перевіряйте коректність розрахунку таймінгів та бітрейту
    • Тестуйте edge cases: порожні файли, пошкоджені заголовки, DRM
```

---

## 🎯 Висновок

> **Ці тести — ваш "золотий стандарт" для надійного аналізу MP4-файлів**.  
> Вони гарантують:
> • ✅ Коректне витягування базових метаданих (бренди, таймінги, FastStart)
> • ✅ Детальний аналіз доріжок: кодек, роздільність, таймінги, конфігурації
> • ✅ Підтримку зашифрованих потоків через прапорець `Encrypted`
> • ✅ Точний розрахунок бітрейту (середнього та пікового) для адаптивного стрімінгу
> • ✅ Пошук ключових кадрів (IDR) для швидкого seek та HLS-генерації
> • ✅ Обробку фрагментованих файлів (fMP4) з окремими таймінгами для кожного сегмента

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Миттєва валідація вхідних сегментів без затримки обробки
- 🔍 Точна інформація про кодек, роздільність, таймінги для адаптації стріму
- 🎯 Швидкий пошук ключових кадрів для генерації HLS-плейлистів
- 📊 Моніторинг бітрейту для оптимізації під різні мережеві умови
- 🛡️ Безпечна обробка зашифрованих потоків через прапорець `Encrypted`

Потребуєте допомоги з інтеграцією `Probe()` у ваш конвеєр прийому сегментів або з генерацією HLS-плейлистів на основі ключових кадрів? Напишіть — покажу готовий код для вашого сценарію! 🚀🔍