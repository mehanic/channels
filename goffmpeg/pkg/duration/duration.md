# ⏱️ `duration.DurToSec`: Конвертація часових рядків у секунди

Це **мінімалістична утиліта** для перетворення часових рядків у форматі `HH:MM:SS` у кількість секунд. Часто використовується для парсингу тривалості відео з виводу FFmpeg, таймкодів субтитрів або розрахунку сегментів HLS.

---

## 🎯 Коротка відповідь

> **Це "часовий калькулятор" для медіа-файлів**: він розбиває рядок `HH:MM:SS` на компоненти та конвертує їх у `float64` секунди, що критично для генерації `#EXTINF`, розрахунку бітрейту та синхронізації аудіо/відео.

---

## ⚠️ Критичні проблеми у поточній реалізації

| Проблема | Симптоми | Ризик для CCTV HLS |
|----------|----------|-------------------|
| 🔴 **Ігнорування помилок парсингу** | `strconv.ParseFloat(...)` повертає `err`, який відкидається `_` | Невалідні дані (`"abc:def:ghi"`) повертають `0` → помилкові `#EXTINF` у плейлисті |
| 🔴 **Неоднозначний поверт `0`** | `0` означає і "помилку парсингу", і "нульову тривалість" | Неможливо відрізнити збій від короткого запису → помилки в логіці ротації |
| 🟡 **Жорсткий формат без мілісекунд** | Не підтримує `HH:MM:SS.mmm` | FFmpeg виводить `time=00:00:01.234` → функція поверне `0` |
| 🟡 **Відсутність валідації діапазонів** | Приймає `99:99:99` без застережень | Може призвести до переповнення бітрейту або некоректного розрізання сегментів |

---

## 🛠️ Production-Ready Версія

```go
package duration

import (
	"fmt"
	"strconv"
	"strings"
)

// DurToSec конвертує рядок тривалості у форматі "HH:MM:SS" або "HH:MM:SS.mmm" у секунди.
// Повертає помилку, якщо формат невалідний або містить нечислові значення.
func DurToSec(dur string) (float64, error) {
	if dur == "" {
		return 0, fmt.Errorf("duration string is empty")
	}

	parts := strings.Split(dur, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid duration format %q: expected HH:MM:SS[.mmm]", dur)
	}

	hrs, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid hours %q: %w", parts[0], err)
	}

	mins, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid minutes %q: %w", parts[1], err)
	}

	secs, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid seconds %q: %w", parts[2], err)
	}

	// 🔹 Опціональна валідація діапазонів (можна вимкнути для кумулятивних тривалостей)
	if mins < 0 || mins >= 60 {
		return 0, fmt.Errorf("minutes out of range [0, 60): %f", mins)
	}
	if secs < 0 || secs >= 60 {
		return 0, fmt.Errorf("seconds out of range [0, 60): %f", secs)
	}

	total := hrs*3600 + mins*60 + secs
	return total, nil
}

// ParseDurationFlex намагається розпарсити тривалість у двох форматах:
// 1. "HH:MM:SS[.mmm]" (вивід ffmpeg progress)
// 2. "123.456000" (вивід ffprobe JSON)
func ParseDurationFlex(raw string) (float64, error) {
	// Спроба 1: HH:MM:SS
	if strings.Contains(raw, ":") {
		return DurToSec(raw)
	}
	// Спроба 2: Plain float (ffprobe)
	sec, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, fmt.Errorf("unsupported duration format %q", raw)
	}
	return sec, nil
}
```

**🔑 Ключові покращення:**
- ✅ Повертає `(float64, error)` → чітке розрізнення між `0.0` та помилкою
- ✅ Підтримує мілісекунди (`00:00:01.234`) → сумісність з FFmpeg
- ✅ Валідація діапазонів `[0, 60)` для хвилин/секунд
- ✅ Додатковий `ParseDurationFlex` для роботи з `ffprobe` JSON
- ✅ Безпечне прибирання пробілів через `strings.TrimSpace`

---

## 📡 Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Генерація точних `#EXTINF` для HLS-плейлиста

```go
func GenerateHLSPlaylist(segments []Segment) (string, error) {
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	
	maxDur := 0.0
	for _, seg := range segments {
		// 🔹 Парсинг тривалості з метаданих камери
		durSec, err := duration.ParseDurationFlex(seg.DurationRaw)
		if err != nil {
			return "", fmt.Errorf("invalid segment duration %q: %w", seg.DurationRaw, err)
		}
		
		fmt.Fprintf(&sb, "#EXTINF:%.3f,\n%s\n", durSec, seg.Filename)
		if durSec > maxDur {
			maxDur = durSec
		}
	}
	
	fmt.Fprintf(&sb, "#EXT-X-TARGETDURATION:%d\n#EXT-X-ENDLIST\n", int(math.Ceil(maxDur)))
	return sb.String(), nil
}
```

---

### 🔹 Приклад 2: Розрахунок бітрейту для адаптивного стрімінгу

```go
func CalculateBitrate(fileSizeBytes int64, durationRaw string) (uint64, error) {
	durSec, err := duration.ParseDurationFlex(durationRaw)
	if err != nil {
		return 0, err
	}
	if durSec <= 0 {
		return 0, fmt.Errorf("duration must be > 0, got %.3fs", durSec)
	}
	
	// 🔹 Бітрейт = (байти * 8) / секунди
	bitrate := uint64(float64(fileSizeBytes) * 8 / durSec)
	return bitrate, nil
}

// 🔹 Використання:
bitrate, err := CalculateBitrate(10_000_000, "00:00:15.500")
if err != nil {
    log.Printf("❌ Bitrate calc failed: %v", err)
} else {
    log.Printf("📡 Calculated bitrate: %d kbps", bitrate/1000)
}
```

---

### 🔹 Приклад 3: Валідація тривалості запису перед обробкою

```go
func ValidateRecordingDuration(raw string) error {
	sec, err := duration.ParseDurationFlex(raw)
	if err != nil {
		return fmt.Errorf("invalid recording duration: %w", err)
	}
	
	if sec < 1.0 {
		return fmt.Errorf("recording too short: %.3fs (min 1.0s)", sec)
	}
	if sec > 86400 { // 24 години
		log.Printf("⚠️  Unusually long recording: %.1f hours", sec/3600)
	}
	
	return nil
}
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При парсингу тривалостей:
    • Завжди перевіряйте повернуту помилку
    • Використовуйте ParseDurationFlex для сумісності з ffmpeg та ffprobe
    • Округлюйте до 3 знаків після коми для #EXTINF

[ ] Для HLS-генерації:
    • Розраховуйте #EXT-X-TARGETDURATION як ceil(max_segment_duration)
    • Валідуйте, що кожен сегмент має duration > 0
    • Логувайте аномалії: сегменти >10s або <0.1s

[ ] Для розрахунку бітрейту:
    • Перевіряйте duration > 0 перед діленням (уникнення Inf/NaN)
    • Використовуйте float64 для проміжних обчислень, конвертуйте в uint64 тільки на виході
    • Тестуйте з файлами різного розміру: 1 МБ, 1 ГБ, 10 ГБ

[ ] Для дебагу:
    • Логувайте сирі рядки тривалості: log.Printf("⏱️ Raw duration: %q", raw)
    • Порівнюйте розраховані секунди з очікуваними
    • Тестуйте крайні випадки: "00:00:00.000", "99:59:59.999", "abc"

[ ] Для тестування:
    • Покрийте кейси: HH:MM:SS, HH:MM:SS.mmm, plain float, порожній рядок, нечислові символи
    • Перевіряйте помилки парсингу: некоректний формат, від'ємні значення, переповнення
    • Тестуйте інтеграцію з реальним виводом ffmpeg/ffprobe
```

---

## 🎯 Висновок

> **`duration` — це "часовий міст" між рядковими метаданими та числовою логікою**, який забезпечує:
> • ✅ Точне перетворення `HH:MM:SS[.mmm]` у секунди з підтримкою дробових значень
> • ✅ Безпечну обробку помилок замість мовчазного повернення `0`
> • ✅ Гнучкість: підтримка обох форматів FFmpeg (progress) та FFprobe (JSON)
> • ✅ Інтеграцію з HLS-логікою: `#EXTINF`, `TARGETDURATION`, бітрейт
> • ✅ Валідацію діапазонів для запобігання аномаліям у конвеєрі

Для вашого **CCTV HLS Processor** це означає:
- ⏱️ Точна генерація плейлистів без розривів та десинхронізації
- 📡 Надійний розрахунок бітрейту для адаптивного вибору якості
- 🛡️ Захист від пошкоджених метаданих через чітку валідацію
- 🔍 Прозорий дебаг через інформативні повідомлення про помилки
- 🔄 Готовність до інтеграції з різними джерелами метаданих (ffmpeg, ffprobe, RTSP-камери)

Потребуєте допомоги з інтеграцією парсингу тривалостей у ваш конвеєр генерації HLS або з налаштуванням адаптивного розрахунку `TARGETDURATION`? Напишіть — покажу готовий код для вашого сценарію! 🚀⏱️