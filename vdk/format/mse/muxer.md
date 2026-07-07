# 📦 Глибокий розбір: `mse.Muxer` — WebSocket муксер для Media Source Extensions (MSE)

Цей файл — **реалізація муксера для потокової передачі відео через WebSocket** з використанням стандарту Media Source Extensions (MSE) у браузерах. Він перетворює `av.Packet` у фрагментований MP4 (fMP4) та надсилає клієнту через WebSocket для відтворення без додаткових плагінів.

---

## 🗺️ Архітектурна схема mse.Muxer

```
┌────────────────────────────────────────┐
│ 📦 mse.Muxer — WebSocket MSE Streamer │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • Muxer — основна структура           │
│  • mp4f.Muxer — фрагментація MP4       │
│  • gobwas/ws — WebSocket управління    │
│  • WriteHeader/WritePacket — стрімінг  │
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet → mp4f.Muxer → fMP4 segments│
│  → WebSocket (text: init, binary: media)│
│  → Browser MSE API → <video> playback  │
│                                         │
│  📡 Підтримка:                          │
│  • Відео: H.264 (через mp4f.Muxer)    │
│  • Аудіо: AAC (через mp4f.Muxer)      │
│  • Транспорт: WebSocket (RFC 6455)    │
│  • Клієнт: Chrome/Firefox/Safari MSE  │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. Muxer — основна структура

### Поля та їх призначення:

```go
type Muxer struct {
    m    *mp4f.Muxer        // внутрішній fMP4 муксер
    r    *http.Request      // вхідний HTTP запит (для контексту)
    w    http.ResponseWriter // HTTP відповідь (для апгрейду)
    conn net.Conn           // WebSocket з'єднання (після апгрейду)
}
```

### 🔧 NewMuxer() — апгрейд HTTP → WebSocket:

```go
func NewMuxer(r *http.Request, w http.ResponseWriter) (*Muxer, error) {
    // 1. Апгрейд з'єднання до WebSocket
    conn, _, _, err := ws.UpgradeHTTP(r, w)
    if err != nil {
        return nil, err
    }
    
    // 2. Фоновий читач для "поглинання" клієнтських повідомлень
    // ⚠️ Без цього браузер може закрити з'єднання через ping/pong таймаути
    go func() {
        defer conn.Close()
        for {
            // Читання наступного повідомлення (ігноруємо дані)
            if _, _, err = wsutil.NextReader(conn, ws.StateServerSide); err != nil {
                return  // з'єднання закрите або помилка
            }
        }
    }()
    
    // 3. Ініціалізація mplexer
    return &Muxer{
        conn: conn,
        m:    mp4f.NewMuxer(nil),  // ⚠️ nil writer — дані повертаються у буфер
        r:    r,
        w:    w,
    }, nil
}
```

### ⚠️ Критичний момент: `mp4f.NewMuxer(nil)`

```
У вихідному коді:
    m: mp4f.NewMuxer(nil)  // ← nil замість io.Writer!

Це означає, що mp4f.Muxer не пише напряму у з'єднання,
а повертає дані через методи `GetInit()` та `WritePacket()`.

Переваги:
• Гнучкість: можна модифікувати дані перед відправкою
• Контроль: можна додати метадані, логування, метрики

Недоліки:
• Додаткові аллокації буферів
• Потрібно пам'ятати про виклик `GetInit()` та обробку `gotFrame`
```

---

## 🔑 2. WriteHeader() — відправка init segment

### 🔧 Логіка ініціалізації MSE:

```go
func (m *Muxer) WriteHeader(streams []av.CodecData) (err error) {
    // 1. Генерація fMP4 заголовку через mp4f.Muxer
    if err = m.m.WriteHeader(streams); err != nil {
        return
    }
    
    // 2. Отримання init segment (метадані кодеків) та першого медіа-сегменту
    meta, fist := m.m.GetInit(streams)  // ⚠️ "fist" — мабуть, опечатка для "first"
    
    // 3. Відправка init segment як текстового повідомлення
    // 📝 MSE очікує init segment як перше повідомлення
    if err = wsutil.WriteServerText(m.conn, []byte(meta)); err != nil {
        return
    }
    
    // 4. Відправка першого медіа-сегменту як бінарного повідомлення
    if err = wsutil.WriteServerBinary(m.conn, fist); err != nil {
        return
    }
    
    return
}
```

### 🔍 Формат повідомлень для MSE:

```
Протокол обміну (сервер → клієнт):

1. Init Segment (текст):
   {
     "codec": "avc1.42001e,mp4a.40.2",
     "timescale": 90000,
     "duration": 0,
     "type": "init"
   }
   → Клієнт парсить як JSON, ініціалізує SourceBuffer

2. Media Segment (бінарний):
   [fMP4 fragment: moof + mdat boxes]
   → Клієнт додає у SourceBuffer.appendBuffer()

3. Подальші сегменти (бінарні):
   [новий fMP4 fragment]
   → Клієнт продовжує буферизацію

Примітка: Текстовий формат для init segment дозволяє:
• Легкий парсинг метаданих на клієнті
• Валідація підтримки кодеків перед завантаженням бінарних даних
```

### ✅ Ваш use-case: клієнтська частина (JavaScript)

```javascript
// browser-mse-client.js — приклад клієнта для mse.Muxer
class MSEPlayer {
    constructor(videoElement, wsUrl) {
        this.video = videoElement;
        this.ws = new WebSocket(wsUrl);
        this.mediaSource = new MediaSource();
        this.sourceBuffer = null;
        
        this.video.src = URL.createObjectURL(this.mediaSource);
        
        this.mediaSource.addEventListener('sourceopen', () => {
            this.ws.onmessage = (event) => {
                if (typeof event.data === 'string') {
                    // Init segment (JSON)
                    const init = JSON.parse(event.data);
                    if (init.type === 'init') {
                        this.sourceBuffer = this.mediaSource.addSourceBuffer(
                            `video/mp4; codecs="${init.codec}"`
                        );
                        this.sourceBuffer.addEventListener('updateend', () => {
                            this.startPlayback();
                        });
                    }
                } else {
                    // Media segment (binary)
                    if (this.sourceBuffer && !this.sourceBuffer.updating) {
                        this.sourceBuffer.appendBuffer(event.data);
                    }
                }
            };
        });
    }
    
    startPlayback() {
        if (this.mediaSource.readyState === 'open') {
            this.video.play().catch(err => console.error('Play failed:', err));
        }
    }
}

// Використання:
// const player = new MSEPlayer(document.querySelector('video'), 'ws://server/stream');
```

---

## 🔑 3. WritePacket() — стрімінг медіа-сегментів

### 🔧 Логіка відправки пакетів:

```go
func (m *Muxer) WritePacket(pkt av.Packet) (err error) {
    // 1. Додавання пакету у fMP4 муксер
    // ⚠️ Другий параметр `false` — мабуть, прапорець "force keyframe" або "flush"
    gotFrame, buffer, err := m.m.WritePacket(pkt, false)
    if err != nil {
        return
    }
    
    // 2. Відправка тільки якщо сформовано повний сегмент
    if gotFrame {
        // 📦 buffer містить fMP4 fragment (moof + mdat)
        if err = wsutil.WriteServerBinary(m.conn, buffer); err != nil {
            return
        }
    }
    
    return
}
```

### 🔍 Чому `gotFrame`?

```
mp4f.Muxer буферизує пакети до формування повного фрагменту:
• fMP4 вимагає групування пакетів у фрагменти (зазвичай 2-10 секунд)
• Кожен фрагмент містить:
  • moof box (metadata: timestamp, duration, offsets)
  • mdat box (media data: стиснуті відео/аудіо дані)

`gotFrame == true` означає:
• Набрано достатньо пакетів для фрагменту
• Або досягнуто ключового кадру (точка розрізу)
• Або таймаут буферизації

Це дозволяє:
• Зменшити overhead на заголовки (один moof на багато пакетів)
• Забезпечити точність синхронізації аудіо/відео
• Підтримувати low-latency стрімінг (короткі фрагменти)
```

### ⚠️ Критична проблема: відсутність обробки помилок WebSocket

```go
if err = wsutil.WriteServerBinary(m.conn, buffer); err != nil {
    return  // ← помилка повертається, але з'єднання не закривається явно!
}
```

**Наслідки**: Якщо клієнт відключився, помилка запису не призведе до очищення ресурсів.

**✅ Виправлення**:

```go
if err = wsutil.WriteServerBinary(m.conn, buffer); err != nil {
    // Логування для дебагу
    if Debug {
        log.Printf("mse: write error: %v", err)
    }
    // Явне закриття з'єднання
    m.conn.Close()
    return err
}
```

---

## 🔑 4. WriteTrailer() — завершення сесії

```go
func (m *Muxer) WriteTrailer() (err error) {
    return m.conn.Close()
}
```

**✅ Правильно**: Закриття WebSocket сигналізує клієнту про кінець потоку.

**💡 Покращення**: Додати фінальний сигнал для клієнта:

```go
func (m *Muxer) WriteTrailer() (err error) {
    // Опціонально: відправити "кінець потоку" перед закриттям
    if Debug {
        wsutil.WriteServerText(m.conn, []byte(`{"type":"eof"}`))
    }
    return m.conn.Close()
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Серверна частина: HTTP handler для MSE стрімінгу

```go
// mse_handler.go — HTTP endpoint для MSE клієнтів
func MSEHandler(w http.ResponseWriter, r *http.Request) {
    // 1. Перевірка підтримки WebSocket
    if !ws.IsWebSocketUpgrade(r) {
        http.Error(w, "WebSocket upgrade required", http.StatusUpgradeRequired)
        return
    }
    
    // 2. Створення MSE муксера
    mseMuxer, err := mse.NewMuxer(r, w)
    if err != nil {
        log.Printf("mse: upgrade failed: %v", err)
        return
    }
    
    // 3. Підключення до джерела відео (напр. RTSP)
    rtspClient, err := rtsp.Dial("rtsp://camera/stream")
    if err != nil {
        log.Printf("mse: rtsp dial failed: %v", err)
        mseMuxer.WriteTrailer()
        return
    }
    defer rtspClient.Close()
    
    // 4. Отримання метаданих потоків
    streams, err := rtspClient.Streams()
    if err != nil {
        log.Printf("mse: get streams failed: %v", err)
        return
    }
    
    // 5. Запис заголовка (init segment)
    if err := mseMuxer.WriteHeader(streams); err != nil {
        log.Printf("mse: write header failed: %v", err)
        return
    }
    
    // 6. Основний цикл стрімінгу
    for {
        pkt, err := rtspClient.ReadPacket()
        if err == io.EOF {
            break
        }
        if err != nil {
            log.Printf("mse: read packet error: %v", err)
            break
        }
        
        if err := mseMuxer.WritePacket(pkt); err != nil {
            log.Printf("mse: write packet error: %v", err)
            break
        }
    }
    
    // 7. Завершення сесії
    mseMuxer.WriteTrailer()
}

// Реєстрація handler:
http.HandleFunc("/stream", MSEHandler)
log.Fatal(http.ListenAndServe(":8080", nil))
```

### 🔧 Клієнтська частина: HTML + JavaScript

```html
<!-- mse-player.html -->
<!DOCTYPE html>
<html>
<head>
    <title>MSE CCTV Player</title>
    <style>
        video { width: 100%; max-width: 1280px; background: #000; }
        #status { margin: 10px 0; color: #666; }
    </style>
</head>
<body>
    <video id="video" controls autoplay muted></video>
    <div id="status">Connecting...</div>
    
    <script>
    class MSEPlayer {
        constructor(videoEl, wsUrl) {
            this.video = videoEl;
            this.wsUrl = wsUrl;
            this.mediaSource = null;
            this.sourceBuffer = null;
            this.pendingSegments = [];
            this.isConnected = false;
            
            this.init();
        }
        
        async init() {
            if (!window.MediaSource) {
                this.setStatus('MSE not supported in this browser');
                return;
            }
            
            this.mediaSource = new MediaSource();
            this.video.src = URL.createObjectURL(this.mediaSource);
            
            this.mediaSource.addEventListener('sourceopen', () => {
                this.connect();
            });
            
            // Обробка помилок відтворення
            this.video.addEventListener('error', (e) => {
                console.error('Video error:', this.video.error);
                this.setStatus('Playback error');
            });
        }
        
        connect() {
            this.setStatus('Connecting to stream...');
            this.ws = new WebSocket(this.wsUrl);
            
            this.ws.binaryType = 'arraybuffer';
            
            this.ws.onopen = () => {
                this.setStatus('Connected, waiting for stream...');
                this.isConnected = true;
            };
            
            this.ws.onmessage = (event) => {
                if (typeof event.data === 'string') {
                    // Init segment (JSON metadata)
                    this.handleInitSegment(event.data);
                } else {
                    // Media segment (binary fMP4)
                    this.handleMediaSegment(event.data);
                }
            };
            
            this.ws.onclose = () => {
                this.isConnected = false;
                this.setStatus('Disconnected');
                // Спроба перепідключення через 5 секунд
                setTimeout(() => this.connect(), 5000);
            };
            
            this.ws.onerror = (err) => {
                console.error('WebSocket error:', err);
                this.setStatus('Connection error');
            };
        }
        
        handleInitSegment(json) {
            try {
                const init = JSON.parse(json);
                if (init.type !== 'init') return;
                
                const mimeType = `video/mp4; codecs="${init.codec}"`;
                
                if (!this.mediaSource.isTypeSupported(mimeType)) {
                    this.setStatus(`Codec not supported: ${init.codec}`);
                    return;
                }
                
                this.sourceBuffer = this.mediaSource.addSourceBuffer(mimeType);
                this.sourceBuffer.mode = 'segments';
                
                this.sourceBuffer.addEventListener('updateend', () => {
                    // Відтворення після ініціалізації
                    if (this.video.paused && this.mediaSource.readyState === 'open') {
                        this.video.play().catch(err => {
                            console.warn('Autoplay prevented:', err);
                        });
                    }
                    
                    // Обробка відкладених сегментів
                    this.flushPendingSegments();
                });
                
                this.sourceBuffer.addEventListener('error', (e) => {
                    console.error('SourceBuffer error:', e);
                    this.setStatus('Buffer error');
                });
                
                this.setStatus('Stream initialized');
                
            } catch (err) {
                console.error('Init parse error:', err);
            }
        }
        
        handleMediaSegment(data) {
            if (!this.sourceBuffer) {
                // Буфер ще не готовий — відкладаємо сегмент
                this.pendingSegments.push(data);
                return;
            }
            
            if (this.sourceBuffer.updating) {
                // Буфер зайнятий — відкладаємо
                this.pendingSegments.push(data);
                return;
            }
            
            try {
                this.sourceBuffer.appendBuffer(data);
            } catch (err) {
                console.error('Append error:', err);
                // Спроба відновлення: очищення буфера
                if (err.name === 'QuotaExceededError') {
                    this.sourceBuffer.remove(0, this.video.currentTime - 10);
                }
            }
        }
        
        flushPendingSegments() {
            while (this.pendingSegments.length > 0 && !this.sourceBuffer.updating) {
                const segment = this.pendingSegments.shift();
                this.handleMediaSegment(segment);
            }
        }
        
        setStatus(text) {
            document.getElementById('status').textContent = text;
        }
        
        disconnect() {
            if (this.ws) {
                this.ws.close();
            }
            if (this.mediaSource && this.mediaSource.readyState === 'open') {
                this.mediaSource.endOfStream();
            }
        }
    }
    
    // Ініціалізація при завантаженні сторінки
    document.addEventListener('DOMContentLoaded', () => {
        const video = document.getElementById('video');
        const player = new MSEPlayer(video, `ws://${location.host}/stream`);
        
        // Додавання контролів для дебагу
        window.msePlayer = player;  // для консолі
    });
    </script>
</body>
</html>
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"WebSocket upgrade failed"** | Помилка 400/426 при підключенні | Перевірте заголовки `Upgrade: websocket` та `Connection: Upgrade` у клієнті |
| **"MSE not supported"** | Помилка у браузері | Перевірте підтримку `MediaSource` та кодеків через `MediaSource.isTypeSupported()` |
| **"Codec not supported"** | Init segment відхиляється | Переконайтеся, що `mp4f.Muxer` генерує сумісні codec strings (напр. `avc1.42001e`) |
| **Буфер переповнюється** | `QuotaExceededError` у `appendBuffer()` | Реалізуйте видалення старих даних: `sourceBuffer.remove(start, end)` |
| **Затримка відтворення** | Відео відстає на 10+ секунд | Зменшіть розмір фрагментів у `mp4f.Muxer` або використовуйте `lowLatency` режим |

---

## ⚡ Оптимізації для low-latency стрімінгу

### 1. Налаштування розміру фрагментів:

```go
// NewMuxerLowLatency — муксер з коротшими фрагментами
func NewMuxerLowLatency(r *http.Request, w http.ResponseWriter, fragmentDuration time.Duration) (*Muxer, error) {
    conn, _, _, err := ws.UpgradeHTTP(r, w)
    if err != nil {
        return nil, err
    }
    
    // Налаштування mp4f.Muxer для низької затримки
    mp4Muxer := mp4f.NewMuxer(nil)
    // ⚠️ Якщо mp4f підтримує налаштування фрагментів:
    // mp4Muxer.SetFragmentDuration(fragmentDuration)
    
    return &Muxer{
        conn: conn,
        m:    mp4Muxer,
        r:    r,
        w:    w,
    }, nil
}
```

### 2. Компресія бінарних повідомлень:

```
WebSocket підтримує пер-фрейм компресію (RFC 7692).
Для зменшення трафіку можна увімкнути deflate:

// У NewMuxer():
conn, _, _, err := ws.UpgradeHTTP(r, w, 
    ws.WithCompressor(ws.DeflateCompressor),
    ws.WithDecompressor(ws.DeflateDecompressor),
)

// На клієнті: браузер автоматично підтримує permessage-deflate
// якщо сервер пропонує цю опцію у handshake
```

### 3. Моніторинг затримки:

```go
type MSEMetrics struct {
    SegmentLatency prometheus.HistogramVec  // час від пакету до відправки
    BufferHealth   prometheus.GaugeVec      // розмір черги сегментів
    ClientCount    prometheus.Gauge         // активні WebSocket з'єднання
}

func (m *Muxer) WritePacketWithMetrics(pkt av.Packet, metrics *MSEMetrics) error {
    start := time.Now()
    
    gotFrame, buffer, err := m.m.WritePacket(pkt, false)
    if err != nil {
        return err
    }
    
    if gotFrame {
        latency := time.Since(start)
        metrics.SegmentLatency.Observe(latency.Seconds())
        
        if err = wsutil.WriteServerBinary(m.conn, buffer); err != nil {
            return err
        }
    }
    
    return nil
}
```

---

## 📋 Чек-лист інтеграції mse.Muxer

```go
// ✅ 1. Перевірка WebSocket upgrade
if !ws.IsWebSocketUpgrade(r) {
    http.Error(w, "WebSocket required", http.StatusUpgradeRequired)
    return
}

// ✅ 2. Створення муксера з обробкою помилок
mseMuxer, err := mse.NewMuxer(r, w)
if err != nil {
    log.Printf("mse init failed: %v", err)
    return
}

// ✅ 3. Отримання та валідація кодеків
streams, err := source.Streams()
if err != nil { /* handle */ }
// Перевірка підтримки: H.264 + AAC для максимальної сумісності

// ✅ 4. Запис заголовка перед медіа-даними
if err := mseMuxer.WriteHeader(streams); err != nil { /* handle */ }

// ✅ 5. Обробка помилок запису з закриттям з'єднання
if err := mseMuxer.WritePacket(pkt); err != nil {
    mseMuxer.WriteTrailer()  // явне закриття
    return
}

// ✅ 6. Закриття ресурсів при завершенні
defer mseMuxer.WriteTrailer()

// ✅ 7. Метрики для моніторингу
metrics.SegmentLatency.Observe(time.Since(start).Seconds())
```

---

## 🔗 Корисні посилання

- 💻 [gobwas/ws Documentation](https://pkg.go.dev/github.com/gobwas/ws) — швидка бібліотека WebSocket для Go
- 📄 [Media Source Extensions API](https://w3c.github.io/media-source/) — офіційна специфікація W3C
- 📄 [ISO BMFF / fMP4 Format](https://chromium.googlesource.com/chromium/src/+/main/media/formats/mp4/) — структура фрагментованого MP4
- 🧪 [WebSocket RFC 6455](https://datatracker.ietf.org/doc/html/rfc6455) — протокол передачі
- 🎬 [MSE Codecs Guide](https://cconcolato.github.io/media-mime-support/) — підтримка кодеків у браузерах

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Завжди перевіряйте `MediaSource.isTypeSupported()`** на клієнті перед додаванням SourceBuffer — уникнення помилок відтворення.
> 2. **Реалізуйте обробку `QuotaExceededError`** — видалення старих даних запобігає переповненню буфера.
> 3. **Відправляйте init segment як текст (JSON)** — це дозволяє клієнту валідувати кодеки до завантаження бінарних даних.
> 4. **Моніторьте `SegmentLatency`** — різке зростання може вказувати на перевантаження мережі або сервера.
> 5. **Тестуйте у різних браузерах** — підтримка MSE та кодеків може відрізнятися (напр. Safari вимагає fMP4, не підтримує WebM).

Потрібен приклад реалізації `mp4f.Muxer` налаштувань для оптимізації low-latency стрімінгу (менші фрагменти, частіші ключові кадри)? Готовий допомогти! 🚀