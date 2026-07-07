# 🎬 Глибокий розбір: ffmpeg init — Ініціалізація та базові утиліти FFmpeg CGO

Цей файл — **базова ініціалізація та утиліти** для CGO-обгортки бібліотеки FFmpeg у пакеті `vdk/codec/ffmpeg`. Він забезпечує завантаження кодеків, налаштування логування, перевірку наявності енкодерів/декодерів та управління життєвим циклом контекстів.

Розберемо архітектуру, ключові компоненти та інтеграцію у ваш **CCTV HLS Processor**.

---

## 🗺️ Архітектурна схема ffmpeg init

```
┌────────────────────────────────────────┐
│ 📦 ffmpeg init — CGO Bootstrap         │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • #cgo LDFLAGS — лінкування бібліотек │
│  • ffinit() — реєстрація всіх кодеків  │
│  • Log levels — константи для логування│
│  • HasEncoder/Decoder — перевірка кодека│
│  • ffctx — базовий контекст для енкодерів/декодерів│
│                                         │
│  🔧 CGO інтеграція:                    │
│  • #include "ffmpeg.h" — C заголовки   │
│  • C.AV_LOG_* — константи логування    │
│  • C.av_register_all() — ініціалізація │
│                                         │
│  🔄 Життєвий цикл:                      │
│  newFFCtxByCodec() → runtime.SetFinalizer() → freeFFCtx()│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. CGO налаштування та лінкування

### Директиви компіляції:

```go
/*
#cgo LDFLAGS: -lavformat -lavutil -lavcodec -lavresample -lswscale
#include "ffmpeg.h"
void ffinit() {
	av_register_all();
}
*/
```

### 🔍 Розбір `#cgo LDFLAGS`:

| Бібліотека | Призначення | Чи обов'язкова |
|-----------|-------------|---------------|
| `-lavformat` | Контейнери (MP4, MKV, TS тощо) | ✅ Так для муксингу/демуксингу |
| `-lavutil` | Базові утиліти (пам'ять, математику) | ✅ Так (залежність інших) |
| `-lavcodec` | Кодеки (AAC, H.264, Opus тощо) | ✅ Так для кодування/декодування |
| `-lavresample` | Аудіо ресемплінг (застаріла) | ⚠️ Замінена на `libswresample` у нових версіях |
| `-lswscale` | Масштабування відео, конвертація форматів | ✅ Для відео-обробки |

> ⚠️ **Увага**: `libavresample` застаріла з FFmpeg 4.0+. У нових проектах використовуйте `libswresample`:
> ```go
> //#cgo LDFLAGS: -lavformat -lavutil -lavcodec -lswresample -lswscale
> ```

### 🔧 Функція `ffinit()`:

```c
void ffinit() {
    av_register_all();  // реєстрація всіх кодеків, форматів, протоколів
}
```

### Виклик у Go `init()`:

```go
func init() {
    C.ffinit()  // автоматичний виклик при завантаженні пакету
}
```

### ✅ Ваш use-case: перевірка наявності кодеків перед запуском

```go
// ValidateFFmpegCodecs — перевірка чи потрібні кодеки доступні
func ValidateFFmpegCodecs(requiredEncoders, requiredDecoders []string) error {
    for _, name := range requiredEncoders {
        if !ffmpeg.HasEncoder(name) {
            return fmt.Errorf("required encoder not found: %s", name)
        }
    }
    for _, name := range requiredDecoders {
        if !ffmpeg.HasDecoder(name) {
            return fmt.Errorf("required decoder not found: %s", name)
        }
    }
    return nil
}

// Використання на старті програми:
func main() {
    requiredEncoders := []string{"aac", "libx264", "libopus"}
    requiredDecoders := []string{"aac", "h264", "pcm_mulaw"}
    
    if err := ValidateFFmpegCodecs(requiredEncoders, requiredDecoders); err != nil {
        log.Fatalf("FFmpeg validation failed: %v", err)
    }
    
    // ... продовження ініціалізації ...
}
```

---

## 🔑 2. Рівні логування FFmpeg

### Константи для налаштування логування:

```go
const (
    QUIET   = int(C.AV_LOG_QUIET)   // -8: нічого не логувати
    PANIC   = int(C.AV_LOG_PANIC)   //  0: тільки паніки
    FATAL   = int(C.AV_LOG_FATAL)   //  8: фатальні помилки
    ERROR   = int(C.AV_LOG_ERROR)   // 16: помилки
    WARNING = int(C.AV_LOG_WARNING) // 24: попередження
    INFO    = int(C.AV_LOG_INFO)    // 32: інформація (дефолт)
    VERBOSE = int(C.AV_LOG_VERBOSE) // 40: деталізовано
    DEBUG   = int(C.AV_LOG_DEBUG)   // 48: відладка
    TRACE   = int(C.AV_LOG_TRACE)   // 56: трасування викликів
)
```

### Функція `SetLogLevel()`:

```go
func SetLogLevel(level int) {
    C.av_log_set_level(C.int(level))
}
```

### ✅ Ваш use-case: налаштування логування для production/debug

```go
// ConfigureFFmpegLogging — налаштування логування залежно від режиму
func ConfigureFFmpegLogging(debugMode bool) {
    if debugMode {
        // Детальне логування для відладки
        ffmpeg.SetLogLevel(ffmpeg.DEBUG)
        log.Printf("FFmpeg logging set to DEBUG level")
    } else {
        // Тільки помилки та попередження для production
        ffmpeg.SetLogLevel(ffmpeg.WARNING)
        log.Printf("FFmpeg logging set to WARNING level")
    }
}

// Використання з прапорцем командного рядка:
func main() {
    debug := flag.Bool("debug", false, "enable debug logging")
    flag.Parse()
    
    ConfigureFFmpegLogging(*debug)
    
    // ... інша ініціалізація ...
}
```

---

## 🔑 3. ffctx — базовий контекст для енкодерів/декодерів

### Структура:

```go
type ffctx struct {
    ff C.FFCtx  // C-структура, визначена у ffmpeg.h
}
```

### 🔧 Метод `newFFCtxByCodec()`:

```go
func newFFCtxByCodec(codec *C.AVCodec) (ff *ffctx, err error) {
    ff = &ffctx{}
    ff.ff.codec = codec  // збереження посилання на AVCodec
    
    // Виділення AVCodecContext для цього кодека
    ff.ff.codecCtx = C.avcodec_alloc_context3(codec)
    
    // Ініціалізація профілю як "невідомий"
    ff.ff.profile = C.FF_PROFILE_UNKNOWN
    
    // Реєстрація фіналізатора для автоматичного очищення
    runtime.SetFinalizer(ff, freeFFCtx)
    
    return ff, nil
}
```

### 🔧 Метод `freeFFCtx()` — очищення ресурсів:

```go
func freeFFCtx(self *ffctx) {
    ff := &self.ff
    
    // 1. Звільнення AVFrame якщо виділено
    if ff.frame != nil {
        C.av_frame_free(&ff.frame)
    }
    
    // 2. Закриття та звільнення AVCodecContext
    if ff.codecCtx != nil {
        C.avcodec_close(ff.codecCtx)           // закриття кодека
        C.av_free(unsafe.Pointer(ff.codecCtx)) // звільнення пам'яті
        ff.codecCtx = nil
    }
    
    // 3. Звільнення словника опцій
    if ff.options != nil {
        C.av_dict_free(&ff.options)
    }
}
```

### 🔍 Чому `runtime.SetFinalizer()`?

```
Go має збірник сміття (GC), але FFmpeg працює з нативною пам'яттю (C).
Якщо не звільнити C-пам'ять явно — виникне витік.

SetFinalizer реєструє callback, який викликається коли об'єкт *ffctx
стає недосяжним для GC. Це "страховка" на випадок якщо програміст
забуде викликати Close() явно.

⚠️ Але: фіналізатори не гарантовано виконуються негайно!
✅ Завжди викликайте Close() явно, не покладайтеся на фіналізатор.
```

### ✅ Ваш use-case: безпечне управління ресурсами енкодера

```go
// SafeAudioEncoder — обгортка з гарантованим Close()
type SafeAudioEncoder struct {
    *ffmpeg.AudioEncoder
    closed bool
}

func NewSafeAudioEncoder(typ av.CodecType) (*SafeAudioEncoder, error) {
    enc, err := ffmpeg.NewAudioEncoderByCodecType(typ)
    if err != nil {
        return nil, err
    }
    
    safe := &SafeAudioEncoder{AudioEncoder: enc}
    
    // Реєстрація фіналізатора як додаткова страховка
    runtime.SetFinalizer(safe, func(s *SafeAudioEncoder) {
        if !s.closed {
            log.Printf("warning: AudioEncoder not explicitly closed, forcing cleanup")
            s.Close()
        }
    })
    
    return safe, nil
}

func (s *SafeAudioEncoder) Close() {
    if s.closed {
        return
    }
    s.AudioEncoder.Close()  // явне закриття
    s.closed = true
    runtime.SetFinalizer(s, nil)  // скасування фіналізатора
}

// Використання:
enc, err := NewSafeAudioEncoder(av.AAC)
if err != nil { /* handle error */ }
defer enc.Close()  // гарантоване закриття

// Навіть якщо станеться паніка — фіналізатор очистить ресурси
```

---

## 🔧 4. Допоміжні функції

### `HasEncoder()` / `HasDecoder()` — перевірка наявності кодека:

```go
func HasEncoder(name string) bool {
    return C.avcodec_find_encoder_by_name(C.CString(name)) != nil
}

func HasDecoder(name string) bool {
    return C.avcodec_find_decoder_by_name(C.CString(name)) != nil
}
```

### 🔍 Як це працює:

```
1. C.CString(name) — конвертація Go string → C string (виділяє пам'ять!)
2. avcodec_find_encoder_by_name() — пошук кодека у зареєстрованих
3. Порівняння з nil — чи знайдено
4. Пам'ять від C.CString() автоматично звільняється після виклику

⚠️ Увага: C.CString() виділяє пам'ять через malloc, але FFmpeg
не вимагає її явного звільнення у цьому випадку.
```

### ✅ Ваш use-case: динамічна перевірка підтримки кодеків

```go
// GetBestAvailableEncoder — вибір найкращого доступного енкодера зі списку
func GetBestAvailableEncoder(preferred []string) (string, error) {
    for _, name := range preferred {
        if ffmpeg.HasEncoder(name) {
            log.Printf("Using encoder: %s", name)
            return name, nil
        }
    }
    return "", fmt.Errorf("no available encoder from list: %v", preferred)
}

// Приклад використання для AAC:
encoderName, err := GetBestAvailableEncoder([]string{
    "libfdk_aac",  // найкраща якість (якщо доступна)
    "aac",         // стандартний FFmpeg AAC
    "libfaac",     // застарілий, але ще іноді використовується
})
if err != nil {
    return fmt.Errorf("no AAC encoder available: %w", err)
}

// Створення енкодера за іменем
enc, err := ffmpeg.NewAudioEncoderByName(encoderName)
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

```go
// ffmpeg_manager.go — централізоване управління FFmpeg ресурсами
type FFmpegManager struct {
    logLevel    int
    codecCache  *CodecCache
    initOnce    sync.Once
    initError   error
}

func NewFFmpegManager() *FFmpegManager {
    return &FFmpegManager{
        logLevel:   ffmpeg.WARNING,
        codecCache: NewCodecCache(),
    }
}

// Init — одноразова ініціалізація FFmpeg
func (m *FFmpegManager) Init() error {
    m.initOnce.Do(func() {
        // 1. Налаштування логування
        ffmpeg.SetLogLevel(m.logLevel)
        
        // 2. Перевірка критичних кодеків
        required := []string{"aac", "h264", "pcm_mulaw"}
        for _, codec := range required {
            if !ffmpeg.HasDecoder(codec) {
                m.initError = fmt.Errorf("required decoder missing: %s", codec)
                return
            }
        }
        
        // 3. Логування успішної ініціалізації
        if m.logLevel <= ffmpeg.INFO {
            log.Printf("FFmpeg initialized, log level: %d", m.logLevel)
        }
    })
    return m.initError
}

// GetAudioEncoder — отримання або створення енкодера з кешуванням
func (m *FFmpegManager) GetAudioEncoder(channelID string, codecType av.CodecType) (*ffmpeg.AudioEncoder, error) {
    if err := m.Init(); err != nil {
        return nil, err
    }
    
    // Спроба отримати з кешу
    if enc, ok := m.codecCache.GetEncoder(channelID); ok {
        return enc, nil
    }
    
    // Створення нового енкодера
    enc, err := ffmpeg.NewAudioEncoderByCodecType(codecType)
    if err != nil {
        return nil, err
    }
    
    // Налаштування дефолтних параметрів для HLS
    enc.SetSampleRate(48000)
    enc.SetChannelLayout(av.CH_STEREO)
    enc.SetSampleFormat(av.FLTP)
    enc.SetBitrate(128000)
    
    // Збереження у кеш
    m.codecCache.SetEncoder(channelID, enc)
    
    return enc, nil
}

// SetLogLevel — зміна рівня логування на льоту
func (m *FFmpegManager) SetLogLevel(level int) {
    m.logLevel = level
    ffmpeg.SetLogLevel(level)
    log.Printf("FFmpeg log level changed to: %d", level)
}

// CloseAll — закриття всіх закешованих ресурсів (для graceful shutdown)
func (m *FFmpegManager) CloseAll() {
    m.codecCache.CloseAll()  // ваша реалізація закриття енкодерів/декодерів
    log.Printf("FFmpeg resources closed")
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **"undefined reference to av_register_all"** | FFmpeg зібрано без підтримки застарілих API | Використовуйте `avcodec_register_all()` замість `av_register_all()` у нових версіях; або додайте `#define AV_REGISTER_ALL` |
| **Витік пам'яті при створенні енкодерів** | `Close()` не викликається, фіналізатор затримується | Завжди використовуйте `defer enc.Close()`; не покладайтеся на фіналізатори для критичних ресурсів |
| **C.CString() витік пам'яті** | Багаторазові виклики `HasEncoder()` без звільнення | FFmpeg не вимагає явного звільнення результатів `C.CString()` у цих функціях, але для довгих циклів кешуйте результати |
| **Неправильний рівень логування** | Логи з'являються у stdout замість файлу | Налаштуйте `av_log_set_callback()` у C коді для перенаправлення логів у Go logger |
| **Помилки лінкування бібліотек** | "cannot find -lavresample" | Встановіть `libavresample-dev` або замініть на `libswresample-dev` у `#cgo LDFLAGS` |

---

## ⚡ Оптимізації для real-time обробки

### 1. Кешування результатів `HasEncoder()`/`HasDecoder()`:

```go
type CodecAvailabilityCache struct {
    mu       sync.RWMutex
    encoders map[string]bool
    decoders map[string]bool
}

func (c *CodecAvailabilityCache) HasEncoder(name string) bool {
    c.mu.RLock()
    if available, ok := c.encoders[name]; ok {
        c.mu.RUnlock()
        return available
    }
    c.mu.RUnlock()
    
    // Перевірка через CGO
    available := ffmpeg.HasEncoder(name)
    
    c.mu.Lock()
    if c.encoders == nil {
        c.encoders = make(map[string]bool)
    }
    c.encoders[name] = available
    c.mu.Unlock()
    
    return available
}
```

### 2. Попередня ініціалізація кодеків:

```go
// PreloadCodecs — примусова реєстрація кодеків для уникнення затримок при першому використанні
func PreloadCodecs(codecNames []string) error {
    for _, name := range codecNames {
        if !ffmpeg.HasEncoder(name) && !ffmpeg.HasDecoder(name) {
            return fmt.Errorf("codec not available: %s", name)
        }
        // Сам виклик Has*() вже реєструє кодек у внутрішніх структурах FFmpeg
    }
    return nil
}

// Виклик на старті програми:
criticalCodecs := []string{"aac", "h264", "libx264", "pcm_mulaw", "pcm_alaw"}
if err := PreloadCodecs(criticalCodecs); err != nil {
    log.Fatalf("Failed to preload codecs: %v", err)
}
```

### 3. Асинхронне логування для уникнення блокувань:

```go
// AsyncFFmpegLogger — перенаправлення логів FFmpeg у асинхронну чергу
type AsyncFFmpegLogger struct {
    queue chan string
    done  chan struct{}
}

func NewAsyncFFmpegLogger(bufferSize int) *AsyncFFmpegLogger {
    logger := &AsyncFFmpegLogger{
        queue: make(chan string, bufferSize),
        done:  make(chan struct{}),
    }
    
    // Фоновий воркер для запису логів
    go func() {
        for {
            select {
            case msg := <-logger.queue:
                log.Printf("[FFmpeg] %s", msg)  // або запис у файл
            case <-logger.done:
                // Обробка залишкових повідомлень
                for msg := range logger.queue {
                    log.Printf("[FFmpeg] %s", msg)
                }
                return
            }
        }
    }()
    
    return logger
}

func (l *AsyncFFmpegLogger) Write(msg string) {
    select {
    case l.queue <- msg:
        // успішно відправлено
    default:
        // черга переповнена — пропускаємо або логуємо напряму
        log.Printf("[FFmpeg] %s", msg)
    }
}

func (l *AsyncFFmpegLogger) Close() {
    close(l.done)
}
```

---

## 📋 Чек-лист інтеграції ffmpeg init

```go
// ✅ 1. Перевірка наявності бібліотек перед компіляцією
// Linux:
// sudo apt-get install libavcodec-dev libavformat-dev libavutil-dev libswresample-dev

// macOS (Homebrew):
// brew install ffmpeg

// ✅ 2. Налаштування #cgo LDFLAGS відповідно до версії FFmpeg
// Для FFmpeg >= 4.0:
// #cgo LDFLAGS: -lavformat -lavutil -lavcodec -lswresample -lswscale

// ✅ 3. Ініціалізація на старті програми
func init() {
    // ffinit() викликається автоматично через init() у ffmpeg пакеті
    ffmpeg.SetLogLevel(ffmpeg.WARNING)  // налаштування логування
}

// ✅ 4. Перевірка критичних кодеків
if !ffmpeg.HasEncoder("aac") {
    log.Fatal("AAC encoder not available")
}

// ✅ 5. Безпечне створення енкодерів/декодерів
enc, err := ffmpeg.NewAudioEncoderByCodecType(av.AAC)
if err != nil { /* handle error */ }
defer enc.Close()  // гарантоване закриття

// ✅ 6. Обробка помилок логування
if debugMode {
    ffmpeg.SetLogLevel(ffmpeg.DEBUG)
} else {
    ffmpeg.SetLogLevel(ffmpeg.WARNING)
}

// ✅ 7. Graceful shutdown
func shutdown() {
    // Закриття всіх активних енкодерів/декодерів
    for _, enc := range activeEncoders {
        enc.Close()
    }
    // Логування завершення
    log.Printf("FFmpeg resources released")
}
```

---

## 🔗 Корисні посилання

- 💻 [vdk ffmpeg Package](https://pkg.go.dev/github.com/deepch/vdk/codec/ffmpeg) — GoDoc documentation
- 📄 [FFmpeg Libavcodec API](https://ffmpeg.org/doxygen/trunk/group__lavc.html) — офіційна довідка
- 📄 [CGO Documentation](https://golang.org/cmd/cgo/) — інтеграція C/Go
- 🧪 [FFmpeg Build Instructions](https://ffmpeg.org/download.html#build-linux) — збірка з потрібними бібліотеками
- 🎬 [HLS Encoding Guide](https://trac.ffmpeg.org/wiki/StreamingGuide) — практичні поради для стрімінгу

---

> 💡 **Ключова рекомендація для вашого проекту**: Оскільки ви працюєте з **медіа в реальному часі**:
> 1. **Завжди викликайте `Close()` явно** — не покладайтеся на фіналізатори для звільнення C-пам'яті.
> 2. **Кешуйте результати `HasEncoder()`/`HasDecoder()`** — CGO виклики дорогі, не перевіряйте наявність кодека для кожного пакету.
> 3. **Налаштуйте логування відповідно до режиму** — `DEBUG` для розробки, `WARNING` для production, щоб уникнути зайвих накладних витрат.
> 4. **Використовуйте `libswresample` замість `libavresample`** — остання застаріла і може бути відсутня у нових дистрибутивах.
> 5. **Прелодуйте критичні кодеки на старті** — це уникне затримок при першому кодуванні/декодуванні.

Потрібен приклад реалізації `AsyncFFmpegLogger` з інтеграцією у вашу систему логування через `log/slog` або `zap`? Готовий допомогти! 🚀