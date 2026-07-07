# 🎭 Глибокий розбір: fake.CodecData — Mock для тестування медіа-пайплайнів

Цей файл — **мінімалістична заглушка (mock/stub)** для інтерфейсу `av.CodecData` з бібліотеки `vdk`. Він призначений для **юніт-тестів**, де потрібно створити фейкові метадані кодеків без ініціалізації реальних енкодерів/декодерів.

Розберемо архітектуру, use-case'и та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема fake.CodecData

```
┌────────────────────────────────────────┐
│ 📦 fake — Testing Utilities            │
├────────────────────────────────────────┤
│                                         │
│  🔑 Призначення:                        │
│  • Юніт-тести без реальних кодеків     │
│  • Mock-об'єкти для інтерфейсів vdk    │
│  • Швидка ініціалізація тестових даних │
│                                         │
│  📦 fake.CodecData:                    │
│  • Реалізує av.CodecData інтерфейс     │
│  • Поля зі суфіксом "_" для уникнення  │
│    конфліктів імен                      │
│  • Тільки читання (getter-методи)      │
│                                         │
│  🎯 Коли використовувати:               │
│  • Тестування логіки маршрутизації     │
│  • Перевірка фільтрів без транскодування│
│  • Мокування вхідних потоків           │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Структура та реалізація

### Базова структура:

```go
type CodecData struct {
    CodecType_     av.CodecType        // Тип кодека: av.AAC, av.H264 тощо
    SampleRate_    int                 // Частота дискретизації (для аудіо)
    SampleFormat_  av.SampleFormat     // Формат семплів: S16, FLTP тощо
    ChannelLayout_ av.ChannelLayout    // Розкладка каналів: STEREO, 5.1 тощо
}
```

### Реалізація інтерфейсу `av.CodecData`:

```go
// Для аудіо-кодеків:
func (self CodecData) Type() av.CodecType         { return self.CodecType_ }
func (self CodecData) SampleFormat() av.SampleFormat { return self.SampleFormat_ }
func (self CodecData) ChannelLayout() av.ChannelLayout { return self.ChannelLayout_ }
func (self CodecData) SampleRate() int            { return self.SampleRate_ }

// Для відео-кодеків потрібно додати:
// func (self CodecData) Width() int { return self.Width_ }
// func (self CodecData) Height() int { return self.Height_ }
```

### 🔍 Чому суфікс `_` у полях?

```
Причина: уникнення конфлікту імен з методами інтерфейсу.

Без суфікса:
  type CodecData struct {
      Type av.CodecType  // ← конфлікт з методом Type()!
  }
  func (self CodecData) Type() av.CodecType { return self.Type }  // рекурсія!

З суфіксом:
  type CodecData struct {
      CodecType_ av.CodecType  // ← унікальне ім'я поля
  }
  func (self CodecData) Type() av.CodecType { return self.CodecType_ }  // ✓
```

---

## 🎯 2. Коли використовувати fake.CodecData

### ✅ Use-case 1: Юніт-тести фільтрів пакетів

```go
// test_pktque_filter.go
func TestSubtitleFilter(t *testing.T) {
    // Створення фейкових метаданих потоків
    streams := []av.CodecData{
        fake.CodecData{CodecType_: av.H264},  // відео потік 0
        fake.CodecData{CodecType_: av.AAC, SampleRate_: 48000, ChannelLayout_: av.CH_STEREO},  // аудіо потік 1
        fake.CodecData{CodecType_: av.AAC},  // телетекст/субтитри потік 2
    }
    
    // Створення фільтра з фейковими даними
    filter := &SubtitleFilter{
        teletextPID: 2,
        // ... інші поля
    }
    
    // Тестовий пакет
    pkt := av.Packet{
        Idx:  2,  // індекс телетекст-потоку
        Time: 100 * time.Millisecond,
        Data: []byte{0x01, 0x02, 0x03},  // фейкові дані
    }
    
    // Виклик фільтра
    drop, err := filter.ModifyPacket(&pkt, streams, 0, 1)
    
    // Перевірка результатів
    assert.NoError(t, err)
    assert.True(t, drop)  // телетекст-пакет має бути відкинутий
}
```

### ✅ Use-case 2: Мокування вхідних потоків для інтеграційних тестів

```go
// test_cctv_pipeline.go
func TestCCTVPipeline_ProcessSegment(t *testing.T) {
    // Створення фейкового демуксера
    fakeDemuxer := &fake.Demuxer{
        StreamsFunc: func() ([]av.CodecData, error) {
            return []av.CodecData{
                fake.CodecData{CodecType_: av.H264},
                fake.CodecData{CodecType_: av.AAC, SampleRate_: 48000},
            }, nil
        },
        ReadPacketFunc: func() (av.Packet, error) {
            // Повертати фейкові пакети по черзі
            return av.Packet{Idx: 0, Time: time.Now(), Data: []byte{0x00, 0x00, 0x01}}, nil
        },
    }
    
    // Ініціалізація пайплайну з фейковим демуксером
    pipeline := NewCCTVPipeline("test_channel", fakeDemuxer)
    
    // Запуск тесту
    err := pipeline.ProcessSegment(context.Background(), 0)
    assert.NoError(t, err)
}
```

### ✅ Use-case 3: Тестування транскодера без реальних енкодерів

```go
// test_transcode.go
func TestTranscoder_Do_Passthrough(t *testing.T) {
    // Фейкові метадані: відео без транскодування
    videoStream := fake.CodecData{CodecType_: av.H264}
    
    // Опції транскодера: не транскодувати нічого
    options := transcode.Options{
        FindAudioDecoderEncoder: func(codec av.AudioCodecData, i int) (bool, av.AudioDecoder, av.AudioEncoder, error) {
            return false, nil, nil, nil  // need=false → passthrough
        },
    }
    
    // Створення транскодера з фейковими потоками
    transcoder, err := transcode.NewTranscoder([]av.CodecData{videoStream}, options)
    assert.NoError(t, err)
    defer transcoder.Close()
    
    // Тестовий пакет
    inputPkt := av.Packet{Idx: 0, Time: 100 * time.Millisecond, Data: []byte{0x01, 0x02}}
    
    // Виклик транскодування
    outputPkts, err := transcoder.Do(inputPkt)
    
    // Перевірка: пакет має пройти без змін (passthrough)
    assert.NoError(t, err)
    assert.Len(t, outputPkts, 1)
    assert.Equal(t, inputPkt.Data, outputPkts[0].Data)
}
```

---

## 🔧 3. Розширення fake.CodecData для ваших потреб

### Додавання відео-полів:

```go
// У вашому тестовому пакеті:
type VideoCodecData struct {
    fake.CodecData
    Width_  int
    Height_ int
}

func (self VideoCodecData) Width() int  { return self.Width_ }
func (self VideoCodecData) Height() int { return self.Height_ }

// Використання:
videoCodec := VideoCodecData{
    CodecData: fake.CodecData{CodecType_: av.H264},
    Width_:    1920,
    Height_:   1080,
}
```

### Додавання методу `PacketDuration()` для аудіо:

```go
type TimedAudioCodecData struct {
    fake.CodecData
    DurationPerPacket time.Duration
}

func (self TimedAudioCodecData) PacketDuration([]byte) (time.Duration, error) {
    return self.DurationPerPacket, nil
}

// Використання для тестування синхронізації:
audioCodec := TimedAudioCodecData{
    CodecData: fake.CodecData{
        CodecType_:     av.AAC,
        SampleRate_:    48000,
        ChannelLayout_: av.CH_STEREO,
    },
    DurationPerPacket: 1024 * time.Second / 48000,  // AAC-LC: 1024 семпли
}
```

### Створення фабричних функцій для зручності:

```go
// helpers_test.go
func NewFakeH264Codec(width, height int) av.CodecData {
    return fake.CodecData{
        CodecType_: av.H264,
        // Додайте Width_/Height_ якщо розширили структуру
    }
}

func NewFakeAACCodec(sampleRate int, channels int) av.CodecData {
    layout := av.CH_MONO
    if channels == 2 {
        layout = av.CH_STEREO
    }
    return fake.CodecData{
        CodecType_:     av.AAC,
        SampleRate_:    sampleRate,
        ChannelLayout_: layout,
        SampleFormat_:  av.FLTP,
    }
}

// Використання у тестах:
streams := []av.CodecData{
    NewFakeH264Codec(1280, 720),
    NewFakeAACCodec(48000, 2),
}
```

---

## 🔄 Інтеграція у ваш pipeline: приклади тестів

### Тест 1: Фільтрація телетекст-пакетів

```go
// cctv_processor_test.go
func TestCCTVProcessor_FilterTeletext(t *testing.T) {
    // Налаштування
    processor := NewCCTVProcessor("test_channel")
    
    // Фейкові метадані: відео(0), аудіо(1), телетекст(2)
    streams := []av.CodecData{
        fake.CodecData{CodecType_: av.H264},
        fake.CodecData{CodecType_: av.AAC, SampleRate_: 48000},
        fake.CodecData{CodecType_: av.AAC},  // телетекст
    }
    
    // Тестові пакети
    testCases := []struct {
        name     string
        pkt      av.Packet
        wantDrop bool
    }{
        {
            name: "video packet passes through",
            pkt:  av.Packet{Idx: 0, Data: []byte{0x01}},
            wantDrop: false,
        },
        {
            name: "teletext packet is dropped",
            pkt:  av.Packet{Idx: 2, Data: []byte{0x02}},
            wantDrop: true,
        },
    }
    
    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            drop, err := processor.filterPacket(&tc.pkt, streams, 0, 1)
            assert.NoError(t, err)
            assert.Equal(t, tc.wantDrop, drop)
        })
    }
}
```

### Тест 2: Синхронізація таймінгів

```go
// sync_test.go
func TestTimelineSync_AudioVideo(t *testing.T) {
    // Фейкові кодеки з відомою тривалістю пакетів
    videoCodec := fake.CodecData{CodecType_: av.H264}
    audioCodec := fake.CodecData{
        CodecType_:  av.AAC,
        SampleRate_: 48000,
    }
    
    // Створення таймлайнів
    videoTL := &pktque.Timeline{}
    audioTL := &pktque.Timeline{}
    
    // Симуляція пакетів з різними таймінгами
    videoPkts := []av.Packet{
        {Idx: 0, Time: 0 * time.Millisecond, Duration: 40 * time.Millisecond},
        {Idx: 0, Time: 40 * time.Millisecond, Duration: 40 * time.Millisecond},
    }
    audioPkts := []av.Packet{
        {Idx: 1, Time: 0 * time.Millisecond, Duration: 21 * time.Millisecond},  // AAC: 1024/48000 ≈ 21ms
        {Idx: 1, Time: 21 * time.Millisecond, Duration: 21 * time.Millisecond},
    }
    
    // Додавання у таймлайни
    for _, pkt := range videoPkts {
        videoTL.Push(pkt.Time, pkt.Duration)
    }
    for _, pkt := range audioPkts {
        audioTL.Push(pkt.Time, pkt.Duration)
    }
    
    // "Проходження" 100 мс по обох таймлайнах
    videoStart := videoTL.Pop(100 * time.Millisecond)
    audioStart := audioTL.Pop(100 * time.Millisecond)
    
    // Перевірка синхронізації
    assert.Equal(t, time.Duration(0), videoStart)
    assert.Equal(t, time.Duration(0), audioStart)
    // Обидва починаються з 0 → синхронізовані ✓
}
```

### Тест 3: Генерация HLS-сегментів з фейковими даними

```go
// hls_generator_test.go
func TestHLSGenerator_CreateSegment(t *testing.T) {
    // Фейковий муксер, що записує у bytes.Buffer
    var buf bytes.Buffer
    fakeMuxer := &fake.Muxer{
        WriteHeaderFunc: func(streams []av.CodecData) error {
            assert.Len(t, streams, 2)  // відео + аудіо
            return nil
        },
        WritePacketFunc: func(pkt av.Packet) error {
            buf.Write(pkt.Data)
            return nil
        },
        WriteTrailerFunc: func() error { return nil },
    }
    
    // Ініціалізація генератора
    gen := NewHLSGenerator("test_channel", fakeMuxer)
    
    // Фейкові пакети для сегменту
    packets := []av.Packet{
        {Idx: 0, Time: 0, Duration: 40 * time.Millisecond, Data: []byte{0x01}},
        {Idx: 1, Time: 0, Duration: 21 * time.Millisecond, Data: []byte{0x02}},
    }
    
    // Створення сегменту
    err := gen.WriteSegment(packets)
    assert.NoError(t, err)
    
    // Перевірка: дані записані у буфер
    assert.Equal(t, []byte{0x01, 0x02}, buf.Bytes())
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"interface conversion: av.CodecData is fake.CodecData, not aacparser.CodecData"** | Тип-асерція не вдається у продукційному коді | Використовуйте `fake.CodecData` тільки у тестах; у продукційному коді перевіряйте тип перед асерцією |
| **Методи не реалізовані** | `panic: interface conversion: av.CodecData is fake.CodecData, missing method Width()` | Додайте необхідні методи у `fake.CodecData` або створіть спеціалізовану заглушку (напр. `VideoCodecData`) |
| **Тести не ізолюються** | Один тест впливає на інший через спільний стан | Створюйте новий `fake.CodecData` для кожного тесту; уникайте глобальних змінних |
| **Складність мокування складних інтерфейсів** | `av.Demuxer` має багато методів, важко реалізувати всі | Використовуйте бібліотеки для мокування, наприклад `github.com/golang/mock` або створіть мінімальну реалізацію тільки з потрібними методами |

---

## ⚡ Оптимізації для тестування

### 1. Шаблони для поширених конфігурацій:

```go
// test_helpers.go
var (
    // Готові фейкові кодеки для повторного використання
    FakeH264_1080p = fake.CodecData{CodecType_: av.H264}
    FakeAAC_48kStereo = fake.CodecData{
        CodecType_:     av.AAC,
        SampleRate_:    48000,
        ChannelLayout_: av.CH_STEREO,
        SampleFormat_:  av.FLTP,
    }
    FakeAAC_44kMono = fake.CodecData{
        CodecType_:     av.AAC,
        SampleRate_:    44100,
        ChannelLayout_: av.CH_MONO,
        SampleFormat_:  av.FLTP,
    }
)

// Використання:
streams := []av.CodecData{FakeH264_1080p, FakeAAC_48kStereo}
```

### 2. Генератор фейкових пакетів:

```go
// packet_generator_test.go
type FakePacketGenerator struct {
    idx       int8
    timeBase  time.Duration
    duration  time.Duration
    dataSize  int
    counter   int
}

func NewFakePacketGenerator(idx int8, timeBase, duration time.Duration, dataSize int) *FakePacketGenerator {
    return &FakePacketGenerator{
        idx: idx, timeBase: timeBase, duration: duration, dataSize: dataSize,
    }
}

func (g *FakePacketGenerator) Next() av.Packet {
    pkt := av.Packet{
        Idx:        g.idx,
        Time:       g.timeBase + time.Duration(g.counter)*g.duration,
        Duration:   g.duration,
        Data:       make([]byte, g.dataSize),
        IsKeyFrame: g.idx == 0 && g.counter%25 == 0,  // кожен 25-й кадр — ключовий
    }
    // Заповнення даних унікальним патерном для відладки
    for i := range pkt.Data {
        pkt.Data[i] = byte((g.counter + i) % 256)
    }
    g.counter++
    return pkt
}

// Використання у тесті:
videoGen := NewFakePacketGenerator(0, 0, 40*time.Millisecond, 1000)
for i := 0; i < 25; i++ {
    pkt := videoGen.Next()
    processor.HandlePacket(pkt)
}
```

### 3. Assert-хелпери для CodecData:

```go
// assert_codec_test.go
func AssertCodecData(t *testing.T, actual av.CodecData, expected fake.CodecData) {
    t.Helper()
    assert.Equal(t, expected.CodecType_, actual.Type())
    
    if expected.SampleRate_ != 0 {
        assert.Equal(t, expected.SampleRate_, actual.(av.AudioCodecData).SampleRate())
    }
    if expected.ChannelLayout_ != 0 {
        assert.Equal(t, expected.ChannelLayout_, actual.(av.AudioCodecData).ChannelLayout())
    }
    if expected.SampleFormat_ != 0 {
        assert.Equal(t, expected.SampleFormat_, actual.(av.AudioCodecData).SampleFormat())
    }
}

// Використання:
streams, _ := demuxer.Streams()
AssertCodecData(t, streams[0], fake.CodecData{CodecType_: av.H264})
AssertCodecData(t, streams[1], FakeAAC_48kStereo)
```

---

## 📋 Чек-лист використання fake.CodecData

```go
// ✅ 1. Імпорт тільки у тестових файлах
// В main.go: не імпортувати fake!
// В *_test.go: import "github.com/deepch/vdk/codec/fake"

// ✅ 2. Створення фейкових метаданих
videoCodec := fake.CodecData{CodecType_: av.H264}
audioCodec := fake.CodecData{
    CodecType_:     av.AAC,
    SampleRate_:    48000,
    ChannelLayout_: av.CH_STEREO,
}

// ✅ 3. Використання у тестах фільтрів/обробників
filter.ModifyPacket(&pkt, []av.CodecData{videoCodec, audioCodec}, 0, 1)

// ✅ 4. Уникнення тип-асерцій у продукційному коді
// ❌ Не робіть:
// codec := stream.(fake.CodecData)  // зламається у продакшені!

// ✅ Робіть:
// if fakeCodec, ok := stream.(fake.CodecData); ok {
//     // тільки у тестах
// }

// ✅ 5. Очищення ресурсів (якщо fake реалізує Close)
// fake.CodecData не потребує Close(), але інші моки можуть

// ✅ 6. Документування тестів
// Коментуйте, що використовується fake для ізоляції тесту
```

---

## 🔗 Корисні посилання

- 💻 [vdk fake Package](https://pkg.go.dev/github.com/deepch/vdk/codec/fake) — GoDoc documentation
- 📄 [Go Testing Package](https://pkg.go.dev/testing) — стандартна бібліотека для тестів
- 🧪 [Go Mock Best Practices](https://github.com/golang/mock) — бібліотека для генерації моків
- 🎬 [vdk Examples](https://github.com/deepch/vdk/tree/master/example) — приклади використання

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **CCTV потоками в реальному часі**:
> 1. **Використовуйте `fake.CodecData` тільки у `*_test.go` файлах** — ніколи не імпортуйте `fake` у продукційний код.
> 2. **Створіть фабричні функції** для поширених конфігурацій (H264_1080p, AAC_48kStereo) — це зменшить дублювання коду у тестах.
> 3. **Додайте метод `PacketDuration()`** у ваші фейкові аудіо-кодеки — це дозволить тестувати синхронізацію таймінгів.
> 4. **Тестуйте граничні випадки**: порожні пакети, неправильні індекси потоків, розриви у таймінгах.
> 5. **Використовуйте `t.Helper()`** у допоміжних функціях тестів — це покращить читабельність повідомлень про помилки.

Потрібен приклад створення повного мока для `av.Demuxer` або `av.Muxer` для інтеграційних тестів вашого HLS-генератора? Готовий допомогти! 🚀