# 🖥️ `cmd/getFindCommand`: Кросплатформний пошук виконуваних файлів

Це **мінімалістичний допоміжний модуль**, який визначає правильну системну команду для пошуку бінарників залежно від операційної системи. Він слугує абстракцією над `which` (Unix/macOS) та `where` (Windows), дозволяючи уніфікувати виклик `exec.Command` у конвеєрі обробки медіа.

---

## 🎯 Коротка відповідь

> **Це "адаптер ОС" для пошуку бінарників**: він автоматично вибирає `which` або `where` на основі `runtime.GOOS`, щоб уникнути хардкоду шляхів та забезпечити кросплатформну сумісність при пошуку `ffmpeg`, `ffprobe` чи `mp4tool` у `PATH`.

---

## 🔍 Детальний розбір

```go
package cmd

import "runtime"

var platform = runtime.GOOS  // 🔹 Визначається під час завантаження пакету

func getFindCommand() string {
    switch platform {
    case "windows":
        return "where"   // 🔹 Windows: шукає .exe, .bat, .cmd автоматично
    default:
        return "which"   // 🔹 Unix-like: Linux, macOS, BSD
    }
}
```

**🔹 Як це працює в парі з `FindBinPath`:**
```go
// Виклик: FindBinPath(ctx, "ffmpeg")
// → getFindCommand() повертає "which" (на Linux)
// → execBufferOutput(ctx, "which", "ffmpeg")
// → Повертає: "/usr/bin/ffmpeg" або помилку
```

**📊 Мапінг `runtime.GOOS`:**
| Значення | ОС | Команда |
|----------|----|---------|
| `"windows"` | Windows 10/11, Server | `where` |
| `"linux"` | Ubuntu, Alpine, Debian, RHEL | `which` |
| `"darwin"` | macOS | `which` |
| `"freebsd"`, `"openbsd"` | BSD-системи | `which` |

---

## ⚖️ Переваги vs Недоліки

| ✅ Переваги | ❌ Недоліки |
|------------|------------|
| Простий та зрозумілий код | Створює **зайвий дочірній процес** на кожен виклик |
| Працює у більшості середовищ | `which`/`where` можуть бути **відсутні** у мінімальних контейнерах (Alpine, distroless) |
| Легко розширити для нових ОС (напр. `"plan9"`) | Вивід `where` у Windows може містити **кілька шляхів** (потрібен парсинг) |
| Ізольована логіка вибору команди | Не враховує розширення виконуваних файлів (`.exe`, `.bat`) |

---

## 🚀 Ідіоматичне рішення у Go: `exec.LookPath`

**Чому варто замінити цей підхід?**
- ✅ `exec.LookPath` — **вбудований у стандартну бібліотеку**
- ✅ Не створює дочірніх процесів (швидше, менше пам'яті)
- ✅ Працює навіть у середовищах без `which`/`where`
- ✅ Автоматично обробляє розширення `.exe`, `.bat`, `.cmd` на Windows
- ✅ Повертає абсолютний шлях або чітку помилку `exec.ErrNotFound`

### 🔹 Заміна на 3 рядки

```go
package cmd

import (
	"context"
	"fmt"
	"os/exec"
)

// FindBinPath шукає абсолютний шлях до бінарника у PATH.
// Використовує вбудований exec.LookPath замість which/where.
func FindBinPath(ctx context.Context, command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

	// 🔹 exec.LookPath кросплатформний, не залежить від зовнішніх утиліт
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("binary %q not found in PATH: %w", command, err)
	}

	return path, nil
}
```

**🔍 Порівняння підходів:**
| Критерій | `which`/`where` через `exec.Command` | `exec.LookPath` |
|----------|--------------------------------------|----------------|
| Швидкість | ~2-5 мс (створення процесу + запуск) | < 0.1 мс (syscall) |
| Залежності | Вимагає наявності `which`/`where` у PATH | Жодних зовнішніх залежностей |
| Надійність | Може впасти в `scratch`/`distroless` контейнерах | Працює скрізь, де є Go runtime |
| Кросплатформність | Потрібно підтримувати switch/case | Вбудована підтримка всіх ОС |

---

## 🛠️ Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Валідація залежностей при старті сервісу

```go
func ValidateMediaDependencies(ctx context.Context) error {
	required := []string{"ffmpeg", "ffprobe", "mp4tool"}
	
	for _, bin := range required {
		path, err := cmd.FindBinPath(ctx, bin)
		if err != nil {
			return fmt.Errorf("🛑 Missing dependency %q: %w", bin, err)
		}
		log.Printf("✅ Found %s at %s", bin, path)
	}
	
	return nil
}

// 🔹 Виклик у main():
func main() {
	ctx := context.Background()
	if err := ValidateMediaDependencies(ctx); err != nil {
		log.Fatal(err)
	}
	// ... запуск конвеєра ...
}
```

---

### 🔹 Приклад 2: Безпечний запуск транскодування з перевіркою шляху

```go
func TranscodeRecording(ctx context.Context, input, output string) error {
	// 🔹 Знаходимо ffmpeg без виклику which/where
	ffmpegPath, err := cmd.FindBinPath(ctx, "ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg unavailable: %w", err)
	}
	
	// 🔹 Виконуємо транскодування
	args := []string{
		"-y", "-i", input,
		"-c:v", "libx264", "-preset", "fast", "-crf", "23",
		"-c:a", "aac", "-b:a", "128k",
		"-f", "hls", "-hls_time", "4",
		"-hls_segment_filename", fmt.Sprintf("%s/seg_%%03d.ts", output),
		fmt.Sprintf("%s/playlist.m3u8", output),
	}
	
	_, err = cmd.ExecBufferOutput(ctx, ffmpegPath, args...)
	return err
}
```

---

### 🔹 Приклад 3: Підтримка кастомних шляхів (коли бінарник не в PATH)

```go
// 🔹 Якщо ffmpeg встановлено у нестандартне місце
func FindBinary(ctx context.Context, command string, fallbackPaths []string) (string, error) {
	// 🔹 Спочатку шукаємо у PATH
	path, err := cmd.FindBinPath(ctx, command)
	if err == nil {
		return path, nil
	}
	
	// 🔹 Fallback: перевіряємо кастомні шляхи
	for _, p := range fallbackPaths {
		fullPath := filepath.Join(p, command)
		if runtime.GOOS == "windows" {
			fullPath += ".exe"
		}
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath, nil
		}
	}
	
	return "", fmt.Errorf("binary %q not found in PATH or fallback paths", command)
}
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При пошуку бінарників:
    • Замініть getFindCommand() + exec.Command на exec.LookPath
    • Використовуйте FindBinPath тільки для валідації наявності залежностей
    • Не викликайте пошук у гарячому шляху (hot path) — кешуйте результати

[ ] Для кросплатформної сумісності:
    • exec.LookPath автоматично додає .exe на Windows
    • Уникайте хардкоду /usr/bin/ffmpeg або C:\ffmpeg\ffmpeg.exe
    • Тестуйте збірку на Linux, macOS та Windows (CI/CD)

[ ] Для контейнеризації (Docker/K8s):
    • Використовуйте multi-stage build з офіційних образів: jrottenberg/ffmpeg
    • У distroless/scratch образах бінарник має бути скопійований явно
    • exec.LookPath працюватиме, якщо бінарник у PATH або вказано абсолютний шлях

[ ] Для безпеки:
    • Не передавайте користувацький ввід у exec.Command без валідації
    • Використовуйте context.WithTimeout для запобігання зависань
    • Логувайте знайдені шляхи тільки у debug-режимі

[ ] Для тестування:
    • Мокайте exec.LookPath у юніт-тестах через інтерфейс
    • Тестуйте сценарії: бінарник знайдено, не знайдено, помилка доступу
    • Перевіряйте поведінку у середовищах без which/where (Alpine, BusyBox)
```

---

## 🎯 Висновок

> **`getFindCommand` — це робочий, але застарілий підхід** до кросплатформного пошуку бінарників.  
> Хоча він коректно вибирає `which`/`where` на основі `runtime.GOOS`, сучасний Go надає **`exec.LookPath`**, який:
> • ✅ Швидший (без створення процесів)
> • ✅ Надійніший (працює у мінімальних контейнерах)
> • ✅ Безпечніший (не залежить від стану зовнішніх утиліт)
> • ✅ Ідіоматичніший (стандартна бібліотека замість кастомної абстракції)

Для вашого **CCTV HLS Processor** це означає:
- ⚡ Миттєва перевірка наявності `ffmpeg`/`ffprobe` без зайвих накладних витрат
- 🐳 Стабільна робота у Docker/Kubernetes навіть у `distroless` образах
- 🛡️ Зменшення поверхні атаки (відсутність виклику `which`/`where`)
- 🔄 Легша підтримка та менше коду для рефакторингу

**Рекомендація**: Замініть `getFindCommand` + `exec.Command("which", ...)` на `exec.LookPath` у наступному релізі. Це займе 5 хвилин, але значно підвищить стабільність та продуктивність системи.

Потребуєте допомоги з рефакторингом пакету `cmd` або з налаштуванням кешування шляхів бінарників для високонавантаженого конвеєра? Напишіть — покажу готовий патч та benchmark-порівняння! 🚀🔧