# 📦 Глибокий розбір: `mp4f.Muxer` — Фрагментований MP4 (fMP4) для low-latency streaming

Цей файл — **повноцінна реалізація муксера для фрагментованого MP4 (fMP4)**, що використовується для low-latency streaming (HLS fMP4, DASH, CMAF). Він генерує ініціалізаційні дані (`moov` + `ftyp`) та інкрементальні фрагменти (`moof` + `mdat`) для потокової передачі відео/аудіо без необхідності знати загальну тривалість файлу заздалегідь.

---

## 🗺️ Архітектурна схема mp4f.Muxer

```
┌────────────────────────────────────────┐
│ 📦 mp4f.Muxer — Fragmented MP4 Writer │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Muxer — основний контролер          │
│  • Stream — обробка окремого треку     │
│  • GetInit() — генерація init segment  │
│  • WritePacketV2/V3/V4 — фрагментація  │
│  • Finalize() — завершення фрагменту   │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet → буферизація → fMP4 фрагмент│
│  → WebSocket/HTTP для MSE/DASH        │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (avc1), H.265 (hev1)  │
│  • Аудіо: AAC (mp4a)                  │
│  • Формат: fMP4 (moof+mdat пари)      │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Muxer — основна структура

### Поля та їх призначення:

```go
type Muxer struct {
    maxFrames     int           // ліміт кадрів у фрагменті (для ротації)
    bufw          *bufio.Writer // буферизований writer для файлу (не використовується!)
    wpos          int64         // поточна позиція запису (не використовується!)
    fragmentIndex int           // лічильник фрагментів для seqnum
    streams       []*Stream     // масив треків (відео=0, аудіо=1...)
    path          string        // шлях для запису (не використовується!)
}
```

### ⚠️ Критична проблема: невикористані поля

```
У вихідному коді:
    bufw, wpos, path — оголошені, але ніде не використовуються!
    
    NewMuxer(w *os.File) *Muxer {
        return &Muxer{}  // ← параметр w ігнорується!
    }

Наслідки:
• Неможливо записувати у файл без модифікації коду
• Плутанина для розробників: які поля дійсно потрібні?

✅ Виправлення: видалити невикористані поля або реалізувати запис у файл:

type Muxer struct {
    maxFrames     int
    fragmentIndex int
    streams       []*Stream
    w             io.Writer  // ← додати для запису
}

func NewMuxer(w io.Writer) *Muxer {
    return &Muxer{
        w: w,
        // ... інші ініціалізації ...
    }
}
```

---

## 🔑 2. GetInit() — генерація init segment для MSE/DASH

### 🔧 Логіка генерації:

```go
func (element *Muxer) GetInit(streams []av.CodecData) (string, []byte) {
    // 1. Створення moov атому з метаданими
    moov := &mp4io.Movie{
        Header: &mp4io.MovieHeader{
            PreferredRate:   1,
            PreferredVolume: 1,
            Matrix:          [9]int32{0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000},
            NextTrackId:     3,  // ⚠️ Завжди 3, навіть якщо треків інша кількість!
            Duration:        0,  // 0 для fMP4 (тривалість у фрагментах)
            TimeScale:       1000, // ⚠️ Фіксована шкала часу, не залежить від треків!
            // ... часи ініціалізації (1904 епоха) ...
        },
        Unknowns: []mp4io.Atom{element.buildMvex()},  // mvex для fMP4
    }
    
    // 2. Заповнення треків та генерація codec string
    var meta string
    for _, stream := range element.streams {
        if err := stream.fillTrackAtom(); err != nil {
            return meta, []byte{}
        }
        moov.Tracks = append(moov.Tracks, stream.trackAtom)
        meta += stream.codecString + ","  // напр. "avc1.42001e,mp4a.40.2"
    }
    meta = meta[:len(meta)-1]  // видалення останньої коми
    
    // 3. Генерація ftyp атому (ручне створення байтів)
    ftypeData := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 
                        0x69, 0x73, 0x6f, 0x36, 0x00, 0x00, 0x00, 0x01, 
                        0x69, 0x73, 0x6f, 0x36, 0x64, 0x61, 0x73, 0x68}
    // Розшифровка: size=24, tag='ftyp', major='iso6', compatible brands...
    
    // 4. Серіалізація moov + об'єднання з ftyp
    file := make([]byte, moov.Len()+len(ftypeData))
    copy(file, ftypeData)
    moov.Marshal(file[len(ftypeData):])
    
    return meta, file  // meta для MSE, file для відправки клієнту
}
```

### 🔍 Формат init segment:

```
[ftyp][moov]

ftyp (File Type Box):
  • size: 24 байти
  • major_brand: 'iso6' (ISO Base Media v6)
  • minor_version: 1
  • compatible_brands: ['iso6', 'dash'] → підтримка DASH streaming

moov (Movie Box) для fMP4:
  • mvhd: Duration=0 (тривалість у фрагментах)
  • mvex: Movie Extend — обов'язково для fMP4, містить trex для кожного треку
  • trak × N: метадані треків (кодек, роздільна здатність, тощо)

✅ Ваш use-case: ініціалізація MSE у браузері

// Клієнтська частина (JavaScript):
const [codecString, initData] = await fetchInitSegment();
const mediaSource = new MediaSource();
video.src = URL.createObjectURL(mediaSource);

mediaSource.addEventListener('sourceopen', () => {
    const sourceBuffer = mediaSource.addSourceBuffer(
        `video/mp4; codecs="${codecString}"`
    );
    sourceBuffer.appendBuffer(initData);  // init segment
    // Далі додаються фрагменти через appendBuffer()
});
```

### ⚠️ Проблеми у GetInit():

#### ❌ 1. Фіксований NextTrackId = 3

```go
NextTrackId: 3,  // ← Завжди 3, незалежно від кількості треків!
```

**Наслідки**: Порушення специфікації для файлів з ≠2 треками. Хоча більшість плеєрів ігнорують це поле, це може призвести до проблем з суворими валідаторами.

**✅ Виправлення**:

```go
NextTrackId: int32(len(element.streams) + 1),
```

#### ❌ 2. Фіксована TimeScale = 1000

```go
TimeScale: 1000,  // ← Фіксована шкала для moov, не для треків!
```

**Проблема**: Треки можуть мати різні timeScale (відео=90000, аудіо=48000), але moov.TimeScale використовується для загальної тривалості. Фіксоване значення може призвести до неточностей у синхронізації.

**✅ Виправлення**: Використовувати найбільший timeScale серед треків:

```go
maxTimeScale := int32(0)
for _, stream := range element.streams {
    if int32(stream.timeScale) > maxTimeScale {
        maxTimeScale = int32(stream.timeScale)
    }
}
if maxTimeScale == 0 { maxTimeScale = 1000 }  // fallback
moov.Header.TimeScale = maxTimeScale
```

#### ❌ 3. Ручне створення ftyp байтів

```go
ftypeData := []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, ...}
```

**Проблема**: "Магічні числа" важко підтримувати, легко помилитися при зміні специфікації.

**✅ Виправлення**: Використовувати структурований підхід через mp4io:

```go
ftyp := &mp4io.FileType{
    MajorBrand:       mp4io.StringToTag("iso6"),
    MinorVersion:     1,
    CompatibleBrands: []uint32{
        mp4io.StringToTag("iso6"),
        mp4io.StringToTag("dash"),
    },
}
ftypBuf := make([]byte, ftyp.Len())
ftyp.Marshal(ftypBuf)
```

---

## 🔑 3. WritePacketV2/V3/V4 — три версії фрагментації

### 🔍 Порівняння версій:

| Версія | Призначення | Особливості |
|--------|-------------|-------------|
| **V2** | Базова фрагментація | Фіксована кількість кадрів (`maxFrames`), повертає фрагмент при досягненні ліміту |
| **V3** | GOP-базована фрагментація | Ротація тільки на ключових кадрах, підтримка `GOP` прапорця |
| **V4** | Інкрементальне накопичення | Не повертає фрагменти, тільки накопичує у буфері (для `Finalize()`) |

### 🔧 WritePacketV2 — базова логіка:

```go
func (element *Stream) writePacketV2(pkt av.Packet, rawdur time.Duration, maxFrames int) (bool, []byte, error) {
    trackID := pkt.Idx + 1
    
    // 1. Ініціалізація нового фрагменту
    if element.sampleIndex == 0 {
        element.moof.Header = &mp4fio.MovieFragHeader{
            Seqnum: uint32(element.muxer.fragmentIndex + 1)
        }
        element.moof.Tracks = []*mp4fio.TrackFrag{
            &mp4fio.TrackFrag{
                Header: &mp4fio.TrackFragHeader{
                    Data: []byte{0x00, 0x02, 0x00, 0x20, 0x00, 0x00, 0x00, uint8(trackID), 0x01, 0x01, 0x00, 0x00},
                    // ⚠️ "Магічні" байти: версія, прапорці, trackID, default flags...
                },
                DecodeTime: &mp4fio.TrackFragDecodeTime{
                    Version: 1,  // 64-бітний час
                    Flags:   0,
                    Time:    uint64(element.dts),  // базовий DTS фрагменту
                },
                Run: &mp4fio.TrackFragRun{
                    Flags:            0x000b05,  // бітова маска опціональних полів
                    FirstSampleFlags: 0x02000000, // прапорці для першого семплу
                    DataOffset:       0,  // буде оновлено при фіналізації
                    Entries:          []mp4io.TrackFragRunEntry{},
                },
            },
        }
        // Ініціалізація mdat буфера з заголовком
        element.buffer = []byte{0x00, 0x00, 0x00, 0x00, 0x6d, 0x64, 0x61, 0x74}  // size(4) + 'mdat'(4)
    }
    
    // 2. Додавання семплу у TrackFragRun
    runEntry := mp4io.TrackFragRunEntry{
        Duration: uint32(element.timeToTs(rawdur)),
        Size:     uint32(len(pkt.Data)),
        Cts:      uint32(element.timeToTs(pkt.CompositionTime)),
    }
    element.moof.Tracks[0].Run.Entries = append(element.moof.Tracks[0].Run.Entries, runEntry)
    
    // 3. Додавання даних у mdat буфер
    element.buffer = append(element.buffer, pkt.Data...)
    element.sampleIndex++
    element.dts += element.timeToTs(rawdur)
    
    // 4. Перевірка чи потрібно фіналізувати фрагмент
    if element.sampleIndex > maxFrames {
        // Встановлення DataOffset (зміщення даних відносно базового)
        element.moof.Tracks[0].Run.DataOffset = uint32(element.moof.Len() + 8)
        
        // Серіалізація moof + mdat у один буфер
        file := make([]byte, element.moof.Len()+len(element.buffer))
        element.moof.Marshal(file)  // запис moof
        pio.PutU32BE(element.buffer, uint32(len(element.buffer)))  // оновлення розміру mdat
        copy(file[element.moof.Len():], element.buffer)  // копіювання mdat
        
        // Скидання стану для наступного фрагменту
        element.sampleIndex = 0
        element.muxer.fragmentIndex++
        
        return true, file, nil  // фрагмент готовий
    }
    
    return false, []byte{}, nil  // ще накопичуємо
}
```

### 🔧 WritePacketV3 — GOP-базована ротація:

```go
func (element *Stream) writePacketV3(pkt av.Packet, rawdur time.Duration, maxFrames int) (bool, []byte, error) {
    // ... ініціалізація аналогічна V2 ...
    
    // Ключова відмінність: ротація тільки на ключових кадрах
    if element.sampleIndex > maxFrames && pkt.IsKeyFrame {
        // Фіналізація фрагменту тільки якщо це ключовий кадр
        element.moof.Tracks[0].Run.DataOffset = uint32(element.moof.Len() + 8)
        out = make([]byte, element.moof.Len()+len(element.buffer))
        element.moof.Marshal(out)
        pio.PutU32BE(element.buffer, uint32(len(element.buffer)))
        copy(out[element.moof.Len():], element.buffer)
        
        element.sampleIndex = 0
        element.muxer.fragmentIndex++
        got = true
    }
    
    // ... додавання семплу аналогічне V2 ...
    return got, out, nil
}
```

**✅ Ваш use-case**: низька затримка стрімінгу

```
GOP-базована фрагментація (V3) критична для:
• Low-latency HLS/DASH — кожен фрагмент починається з ключового кадру
• Швидкий seek — плеєр може почати відтворення з будь-якого фрагменту
• Ефективне кодування — уникнення розривів у прогнозуванні між кадрами

Приклад налаштування:
    muxer.SetMaxFrames(30)  // ~1 секунда при 30fps
    // Фрагменти будуть формуватися кожні ~1с, але тільки на ключових кадрах
```

### 🔧 WritePacketV4 — інкрементальне накопичення:

```go
func (element *Stream) writePacketV4(pkt av.Packet) error {
    defaultFlags := fmp4io.SampleNonKeyframe
    if pkt.IsKeyFrame {
        defaultFlags = fmp4io.SampleNoDependencies  // ключовий кадр
    }
    
    // Ініціалізація тільки для першого пакету
    if element.sampleIndex == 0 {
        element.moof.Header = &mp4fio.MovieFragHeader{
            Seqnum: uint32(element.muxer.fragmentIndex + 1)
        }
        // ... ініціалізація TrackFrag аналогічна V2/V3 ...
        element.buffer = []byte{0x00, 0x00, 0x00, 0x00, 0x6d, 0x64, 0x61, 0x74}
    }
    
    // Додавання семплу з прапорцями
    runEntry := mp4io.TrackFragRunEntry{
        Duration: uint32(element.timeToTs(pkt.Duration)),
        Size:     uint32(len(pkt.Data)),
        Cts:      uint32(element.timeToTs(pkt.CompositionTime)),
        Flags:    uint32(defaultFlags),  // ⚠️ V4 передає прапорці у кожному семплі!
    }
    element.moof.Tracks[0].Run.Entries = append(element.moof.Tracks[0].Run.Entries, runEntry)
    element.buffer = append(element.buffer, pkt.Data...)
    element.sampleIndex++
    element.dts += element.timeToTs(pkt.Duration)
    
    return nil  // нічого не повертає, тільки накопичує
}
```

**Призначення**: Накопичення пакетів для подальшої фіналізації через `Finalize()`.

---

## 🔑 4. Finalize() — завершення фрагменту

### 🔧 Логіка фіналізації:

```go
func (element *Muxer) Finalize() []byte {
    stream := element.streams[0]  // ⚠️ Тільки перший потік!
    
    // 1. Встановлення DataOffset (зміщення даних у mdat)
    stream.moof.Tracks[0].Run.DataOffset = uint32(stream.moof.Len() + 8)
    
    // 2. Створення буфера для moof + mdat
    out := make([]byte, stream.moof.Len()+len(stream.buffer))
    stream.moof.Marshal(out)  // серіалізація moof
    
    // 3. Оновлення розміру mdat атому
    PutU32BE(stream.buffer, uint32(len(stream.buffer)))  // custom функція замість pio.PutU32BE
    
    // 4. Копіювання mdat даних після moof
    copy(out[stream.moof.Len():], stream.buffer)
    
    // 5. Скидання стану для наступного фрагменту
    stream.sampleIndex = 0
    stream.muxer.fragmentIndex++
    
    return out  // готовий фрагмент [moof][mdat]
}
```

### ⚠️ Критична проблема: тільки перший потік

```go
stream := element.streams[0]  // ← Ігнорує аудіо та інші треки!
```

**Наслідки**: 
• Аудіо пакети не включаються у фрагмент
• Розсинхронізація аудіо/відео у плеєрі
• Втрата аудіо даних

**✅ Виправлення**: Обробка всіх треків:

```go
func (element *Muxer) Finalize() ([]byte, error) {
    if len(element.streams) == 0 {
        return nil, fmt.Errorf("no streams to finalize")
    }
    
    // Вибір треку з найбільшою кількістю семплів (або відео як пріоритет)
    var mainStream *Stream
    maxSamples := 0
    for _, s := range element.streams {
        if s.sampleIndex > maxSamples {
            maxSamples = s.sampleIndex
            mainStream = s
        }
    }
    if mainStream == nil {
        return nil, fmt.Errorf("no samples to finalize")
    }
    
    // Фіналізація основного треку
    mainStream.moof.Tracks[0].Run.DataOffset = uint32(mainStream.moof.Len() + 8)
    out := make([]byte, mainStream.moof.Len()+len(mainStream.buffer))
    mainStream.moof.Marshal(out)
    PutU32BE(mainStream.buffer, uint32(len(mainStream.buffer)))
    copy(out[mainStream.moof.Len():], mainStream.buffer)
    
    // Скидання стану всіх треків
    for _, s := range element.streams {
        s.sampleIndex = 0
    }
    element.fragmentIndex++
    
    return out, nil
}
```

---

## 🔑 5. fillTrackAtom() — генерація кодек-специфічних метаданих

### 🔧 H.264/H.265: генерація codec string

```go
if self.Type() == av.H264 {
    codec := self.CodecData.(h264parser.CodecData)
    // ... заповнення AVC1Desc ...
    self.codecString = fmt.Sprintf("avc1.%02X%02X%02X", 
        codec.RecordInfo.AVCProfileIndication,
        codec.RecordInfo.ProfileCompatibility, 
        codec.RecordInfo.AVCLevelIndication)
} else if self.Type() == av.H265 {
    // ... заповнення HV1Desc ...
    self.codecString = "hev1.1.6.L120.90"  // ⚠️ Захардкожене значення!
}
```

### ⚠️ Проблема: захардкожений codec string для H.265

```go
self.codecString = "hev1.1.6.L120.90"  // ← Фіксоване значення, не залежить від реального кодека!
```

**Наслідки**: 
• Якщо реальний кодек не співпадає з "hev1.1.6.L120.90", плеєр може відмовитися відтворювати
• Неможливість підтримки різних профілів/рівнів H.265

**✅ Виправлення**: Динамічна генерація як для H.264:

```go
} else if self.Type() == av.H265 {
    codec := self.CodecData.(h265parser.CodecData)
    // Приклад формату: hev1.{profile}.{compatibility}.{level}.{constraint}
    self.codecString = fmt.Sprintf("hev1.%d.%d.L%d.%d",
        codec.ProfileSpace,  // profile space (0-3)
        codec.ProfileID,     // profile id (0-255)
        codec.LevelID,       // level id (0-255)
        codec.ConstraintFlags)  // constraint flags
}
```

### 🔧 AAC: генерація esds через FDummy

```go
} else if self.Type() == av.AAC {
    codec := self.CodecData.(aacparser.CodecData)
    self.sample.SampleDesc.MP4ADesc = &mp4io.MP4ADesc{
        // ... параметри ...
        Unknowns: []mp4io.Atom{self.buildEsds(codec.MPEG4AudioConfigBytes())},
    }
    self.codecString = "mp4a.40.2"  // ⚠️ Фіксований для AAC-LC
}
```

### 🔧 buildEsds() — створення esds атому через FDummy:

```go
func (self *Stream) buildEsds(conf []byte) *FDummy {
    esds := &mp4fio.ElemStreamDesc{DecConfig: conf}
    
    b := make([]byte, esds.Len())
    esds.Marshal(b)  // ⚠️ Може бути некоректним через проблеми у ElemStreamDesc.Marshal()
    
    esdsDummy := FDummy{
        Data: b,
        Tag_: mp4io.Tag(uint32(mp4io.ESDS)),
    }
    return &esdsDummy
}
```

**⚠️ Залежність**: Коректність `buildEsds()` залежить від виправлення проблем у `mp4fio.ElemStreamDesc` (див. попередній розбір).

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: WebSocket streaming з fMP4 фрагментами

```go
// StreamFMP4OverWebSocket — відправка fMP4 фрагментів через WebSocket
func StreamFMP4OverWebSocket(ws *websocket.Conn, source av.Demuxer) error {
    // 1. Ініціалізація муксера
    muxer := mp4f.NewMuxer(nil)  // ⚠️ параметр ігнорується, потрібно виправити
    muxer.SetMaxFrames(30)       // ~1 секунда при 30fps
    
    // 2. Отримання метаданих потоків
    streams, err := source.Streams()
    if err != nil { return err }
    
    // 3. Генерація init segment
    codecString, initData := muxer.GetInit(streams)
    if err := ws.WriteMessage(websocket.TextMessage, []byte(codecString)); err != nil {
        return err
    }
    if err := ws.WriteMessage(websocket.BinaryMessage, initData); err != nil {
        return err
    }
    
    // 4. Основний цикл стрімінгу
    for {
        pkt, err := source.ReadPacket()
        if err == io.EOF { break }
        if err != nil { return err }
        
        // 5. Фрагментація з GOP-базованою ротацією (V3)
        gotFragment, fragment, err := muxer.WritePacket(pkt, pkt.IsKeyFrame)
        if err != nil { return err }
        
        if gotFragment {
            // 6. Відправка фрагменту клієнту
            if err := ws.WriteMessage(websocket.BinaryMessage, fragment); err != nil {
                return err
            }
        }
    }
    
    // 7. Фіналізація останнього фрагменту
    if finalFragment := muxer.Finalize(); len(finalFragment) > 0 {
        ws.WriteMessage(websocket.BinaryMessage, finalFragment)
    }
    
    return nil
}
```

### 🔧 Приклад: Інтеграція з MSE у браузері

```javascript
// client-mse.js — клієнтська частина для відтворення fMP4
class FMP4Player {
    constructor(videoElement, wsUrl) {
        this.video = videoElement;
        this.ws = new WebSocket(wsUrl);
        this.mediaSource = new MediaSource();
        this.sourceBuffer = null;
        this.initReceived = false;
        
        this.video.src = URL.createObjectURL(this.mediaSource);
        
        this.mediaSource.addEventListener('sourceopen', () => {
            this.ws.onmessage = (event) => {
                if (typeof event.data === 'string') {
                    // Перше повідомлення: codec string
                    if (!this.initReceived) {
                        this.codecString = event.data;
                        this.initReceived = true;
                    }
                } else if (this.initReceived && !this.sourceBuffer) {
                    // Друге повідомлення: init segment
                    this.sourceBuffer = this.mediaSource.addSourceBuffer(
                        `video/mp4; codecs="${this.codecString}"`
                    );
                    this.sourceBuffer.addEventListener('updateend', () => {
                        this.startPlayback();
                    });
                    this.sourceBuffer.appendBuffer(event.data);
                } else if (this.sourceBuffer) {
                    // Подальші повідомлення: фрагменти
                    if (!this.sourceBuffer.updating) {
                        this.sourceBuffer.appendBuffer(event.data);
                    }
                }
            };
        });
    }
    
    startPlayback() {
        if (this.mediaSource.readyState === 'open' && !this.video.paused) {
            this.video.play().catch(err => console.warn('Autoplay prevented:', err));
        }
    }
    
    disconnect() {
        this.ws.close();
        if (this.mediaSource.readyState === 'open') {
            this.mediaSource.endOfStream();
        }
    }
}

// Використання:
// const player = new FMP4Player(document.querySelector('video'), 'wss://server/stream');
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Невикористані поля у Muxer** | Неможливо записувати у файл | Реалізувати запис через `io.Writer` або видалити зайві поля |
| **Фіксований NextTrackId=3** | Помилки у суворих валідаторах | Використовувати `len(streams)+1` для динамічного розрахунку |
| **Фіксована TimeScale=1000** | Неточності у синхронізації | Використовувати max timeScale серед треків |
| **Захардкожений codec string для H.265** | Плеєр відмовляється відтворювати | Динамічна генерація codec string як для H.264 |
| **Finalize() тільки для першого треку** | Втрата аудіо, розсинхронізація | Обробка всіх треків у `Finalize()` |
| **"Магічні" байти у TrackFragHeader** | Некоректні прапорці, помилки парсингу | Використовувати структуровані поля замість сирих байт |

---

## ⚡ Оптимізації для low-latency streaming

### 1. Пул буферів для фрагментів:

```go
var fragmentBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір фрагменту: 1с відео @ 2Mbps = 250KB
        buf := make([]byte, 0, 256*1024)
        return &buf
    },
}

func GetFragmentBuffer() *[]byte { return fragmentBufferPool.Get().(*[]byte) }
func PutFragmentBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    fragmentBufferPool.Put(b)
}

// Використання у WritePacketV2/V3:
buf := GetFragmentBuffer()
defer PutFragmentBuffer(buf)
// ... використання buf для накопичення даних ...
```

### 2. Асинхронна серіалізація фрагментів:

```go
// WritePacketAsync — асинхронна обробка для зменшення затримки
func (element *Stream) WritePacketAsync(pkt av.Packet, callback func([]byte, error)) {
    go func() {
        got, fragment, err := element.writePacketV3(pkt, pkt.Duration, 30)
        if got && fragment != nil {
            callback(fragment, err)
        }
    }()
}
```

### 3. Моніторинг продуктивності фрагментації:

```go
type FragmentMetrics struct {
    FragmentsGenerated prometheus.CounterVec
    FragmentLatency    prometheus.HistogramVec
    AvgFragmentSize    prometheus.HistogramVec
    GOPAlignment       prometheus.CounterVec  // чи фрагменти вирівняні по GOP
}

func (m *FragmentMetrics) RecordFragment(duration time.Duration, size int, aligned bool, streamID string) {
    m.FragmentsGenerated.WithLabelValues(streamID).Inc()
    m.FragmentLatency.WithLabelValues(streamID).Observe(duration.Seconds())
    m.AvgFragmentSize.WithLabelValues(streamID).Observe(float64(size))
    if aligned {
        m.GOPAlignment.WithLabelValues(streamID).Inc()
    }
}
```

---

## 📋 Чек-лист безпечного використання mp4f.Muxer

```go
// ✅ 1. Ініціалізація з правильними параметрами
muxer := mp4f.NewMuxer(writer)  // ← виправити NewMuxer для прийняття io.Writer
muxer.SetMaxFrames(30)          // ліміт кадрів для ротації

// ✅ 2. Генерація init segment перед відправкою фрагментів
codecString, initData := muxer.GetInit(streams)
// Відправка codecString та initData клієнту

// ✅ 3. Використання GOP-базованої фрагментації (V3) для low-latency
gotFragment, fragment, err := muxer.WritePacket(pkt, pkt.IsKeyFrame)  // GOP=true для ключових кадрів

// ✅ 4. Обробка всіх треків у Finalize()
// ✅ 5. Динамічна генерація codec string для H.265
// ✅ 6. Валідація прапорців у TrackFragRun (не "магічні" байти)
// ✅ 7. Метрики для моніторингу затримки та розміру фрагментів
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 23009-1 (DASH)](https://www.iso.org/standard/79329.html) — стандарт для fMP4 у streaming
- 📄 [CMAF Specification](https://www.iso.org/standard/74428.html) — Common Media Application Format
- 📄 [HLS fMP4 Guide](https://developer.apple.com/documentation/http_live_streaming/about_the_common_media_application_format_with_http_live_streaming_hls) — Apple documentation
- 🧪 [Media Source Extensions API](https://w3c.github.io/media-source/) — офіційна специфікація W3C
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Виправте NewMuxer для прийняття io.Writer** — уникнення плутанини з невикористаними полями.
> 2. **Динамічно розраховуйте NextTrackId та TimeScale** — забезпечення сумісності зі специфікацією.
> 3. **Генеруйте codec string динамічно для H.265** — підтримка різних профілів/рівнів кодека.
> 4. **Обробляйте всі треки у Finalize()** — уникнення втрати аудіо та розсинхронізації.
> 5. **Замініть "магічні" байти на структуровані поля** — покращення читабельності та підтримки коду.

Потрібен приклад інтеграції `mp4f.Muxer` з вашим `mse.Muxer` для створення повного pipeline: RTSP → fMP4 → WebSocket → MSE у браузері? Готовий допомогти! 🚀