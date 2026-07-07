# 🔍 `cmd`: Утиліта для пошуку бінарників та виконання зовнішніх команд

Це **допоміжний пакет** для безпечного запуску зовнішніх процесів у Go, який реалізує дві ключові функції: пошук шляху до виконуваного файлу та виконання команди з перехопленням `stdout` у буфер. Ідеально підходить для інтеграції `ffmpeg`, `ffprobe`, `mp4tool` у ваш конвеєр обробки медіа.

---

## 🎯 Коротка відповідь

> **Це "контролер зовнішніх процесів"**: він знаходить абсолютний шлях до бінарника, виконує команду з підтримкою `context` (для таймаутів/скасування) та повертає стандартний вивід — без блокування основного потоку та з мінімальними накладними витратами.

---

## 🔍 Детальний розбір коду

### 🔹 `FindBinPath` — пошук виконуваного файлу
```go
func FindBinPath(ctx context.Context, command string) (string, error)
```
**🎯 Призначення**: Знайти повний шлях до команди (наприклад, `ffmpeg`) у змінній оточення `PATH`.  
**🔄 Як працює**: Викликає `execBufferOutput` з внутрішньою функцією `getFindCommand()` (ймовірно, повертає `which` для Unix або `where` для Windows) та шуканим ім'ям.  
**✅ Переваги**: Підтримка `context` для скасування пошуку при довгому очікуванні.

---

### 🔹 `execBufferOutput` — виконання команди з буферизацією
```go
func execBufferOutput(ctx context.Context, command string, args ...string) (string, error)
```
**🎯 Призначення**: Запустити зовнішній процес, перехопити `stdout` у пам'ять та повернути його як рядок.  
**🔄 Як працює**:
1. Створює `exec.CommandContext(ctx, command, args...)` → процес успадковує контекст для контролю життєвого циклу.
2. Перенаправляє `Stdout` у `bytes.Buffer`.
3. Виконує `c.Run()` (блокує до завершення).
4. Повертає вміст буфера або помилку з повною рядковою формою команди.

**✅ Переваги**: Ізоляція виводу, підтримка асинхронного скасування через `ctx`, простий API.

---

## ⚠️ Критичні зауваження та покращення

| Проблема | Симптоми | Рішення |
|----------|----------|---------|
| 🔹 `getFindCommand()` не визначено | Помилка компіляції або рантайм-панік | Замініть на вбудований `exec.LookPath(command)` — кросплатформний та без зайвих процесів |
| 🔹 Не захоплюється `stderr` | При помилці FFmpeg виводить діагностику в `stderr`, яку ви втрачаєте | Додайте `var errBuf bytes.Buffer; c.Stderr = &errBuf` та включайте в повідомлення про помилку |
| 🔹 `c.String()` у помилці | Може містити конфіденційні дані (напр. шляхи, ключі шифрування) | Форматуйте помилку безпечно: `fmt.Errorf("command %q failed: %w", command, err)` |
| 🔹 Відсутність таймауту | Процес може зависнути назавжди, якщо `ctx` не має deadline | Використовуйте `context.WithTimeout` на рівні викликаючого коду |
| 🔹 Відсутність перевірки шляху | Спроба виконати неіснуючу команду → помилка `exec: not found` | Попередньо перевіряйте через `exec.LookPath` перед виконанням |

---

## 🛠️ Вдосконалена версія (Production-Ready)

```go
package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// FindBinPath шукає абсолютний шлях до бінарника, використовуючи системний PATH.
// Використовує вбудований exec.LookPath замість виклику which/where.
func FindBinPath(ctx context.Context, command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

	// 🔹 exec.LookPath кросплатформний, не створює зайвих процесів
	path, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("binary %q not found in PATH: %w", command, err)
	}

	return path, nil
}

// ExecBufferOutput виконує команду, повертає stdout.
// Захоплює stderr для детальних повідомлень про помилки.
func ExecBufferOutput(ctx context.Context, command string, args ...string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

	var outBuf, errBuf bytes.Buffer

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf // 🔹 Захоплюємо помилки кодувальника

	if err := cmd.Run(); err != nil {
		// 🔹 Формуємо зрозумілу помилку з діагностикою
		errMsg := strings.TrimSpace(errBuf.String())
		if errMsg != "" {
			return "", fmt.Errorf("command %q failed: %w\nstderr: %s", cmd.Path, err, errMsg)
		}
		return "", fmt.Errorf("command %q failed: %w", cmd.Path, err)
	}

	return strings.TrimSpace(outBuf.String()), nil
}
```

**🔑 Ключові покращення:**
- ✅ `exec.LookPath` замість `which`/`where` → швидше, надійніше, без залежностей від ОС.
- ✅ Захоплення `stderr` → ви бачите реальну причину помилки FFmpeg (`Invalid data`, `Codec not found`, тощо).
- ✅ Безпечне форматування помилок → без витоку конфіденційних аргументів.
- ✅ `strings.TrimSpace` → прибирає зайві переноси рядків у виводі.

---

## 📡 Практичне використання у вашому CCTV HLS Processor

### 🔹 Приклад 1: Перевірка залежностей перед стартом

```go
func ValidateDependencies(ctx context.Context) error {
	tools := []string{"ffmpeg", "ffprobe", "mp4tool"}
	
	for _, tool := range tools {
		path, err := cmd.FindBinPath(ctx, tool)
		if err != nil {
			return fmt.Errorf("missing required dependency %q: %w", tool, err)
		}
		log.Printf("✅ Found %s at %s", tool, path)
	}
	
	return nil
}

// 🔹 Виклик у main():
if err := ValidateDependencies(context.Background()); err != nil {
	log.Fatalf("🛑 Startup aborted: %v", err)
}
```

---

### 🔹 Приклад 2: Безпечне виконання `ffprobe` з таймаутом

```go
func ProbeMedia(ctx context.Context, inputPath string) (string, error) {
	// 🔹 Обмежуємо час виконання до 30 секунд
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	
	args := []string{
		"-v", "error",
		"-print_format", "json",
		"-show_streams",
		"-show_format",
		inputPath,
	}
	
	// 🔹 Виконання з перехопленням виводу та помилок
	output, err := cmd.ExecBufferOutput(probeCtx, "ffprobe", args...)
	if err != nil {
		// 🔹 Логуємо деталі для дебагу
		log.Printf("❌ ffprobe failed for %s: %v", inputPath, err)
		return "", fmt.Errorf("media probe failed: %w", err)
	}
	
	return output, nil
}
```

---

### 🔹 Приклад 3: Транскодування з контролем ресурсів

```go
func TranscodeToHLS(ctx context.Context, input, outputDir string) error {
	// 🔹 Перевіряємо, чи ffmpeg доступний
	ffmpegPath, err := cmd.FindBinPath(ctx, "ffmpeg")
	if err != nil { return err }
	
	// 🔹 Формуємо аргументи
	args := []string{
		"-y",                       // Перезапис без запиту
		"-i", input,
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "23",
		"-c:a", "aac",
		"-b:a", "128k",
		"-f", "hls",
		"-hls_time", "4",
		"-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(outputDir, "seg_%03d.ts"),
		filepath.Join(outputDir, "playlist.m3u8"),
	}
	
	// 🔹 Виконання з контекстом (можна скасувати при зупинці сервіса)
	output, err := cmd.ExecBufferOutput(ctx, ffmpegPath, args...)
	if err != nil {
		return fmt.Errorf("transcoding failed: %w", err)
	}
	
	// 🔹 Логуємо попередження/інфо від ffmpeg (якщо є в stdout)
	if output != "" {
		log.Printf("ℹ️  FFmpeg output: %s", output)
	}
	
	log.Printf("✅ HLS generated in %s", outputDir)
	return nil
}
```

---

## 📋 Чекліст для вашого проекту

```
[ ] При пошуку бінарників:
    • Використовуйте exec.LookPath замість which/where
    • Перевіряйте наявність ffmpeg, ffprobe, mp4tool перед стартом конвеєра
    • Логувайте знайдені шляхи для дебагу розгорнутих середовищ

[ ] При виконанні зовнішніх команд:
    • Завжди передавайте context з таймаутом: context.WithTimeout(ctx, 30s)
    • Захоплюйте stderr для діагностики помилок кодувальника
    • Не передавайте конфіденційні дані (ключі, токени) у аргументи команд
    • Валідуйте вхідні шляхи перед передачею у exec.CommandContext

[ ] Для обробки помилок:
    • Форматуйте помилки з контекстом: fmt.Errorf("ffprobe failed for %q: %w", file, err)
    • Логувайте stderr тільки при рівні DEBUG, щоб не засмічувати продакшен-логи
    • Реалізуйте retry-логіку для тимчасових помилок (напр. "Resource temporarily unavailable")

[ ] Для безпеки:
    • Санітизуйте вхідні шляхи: filepath.Clean(), перевірка на існування
    • Уникайте shell-інтерпретації: exec.Command не запускає shell за замовчуванням ✅
    • Обмежуйте час виконання через context для запобігання zombie-процесів

[ ] Для дебагу:
    • Додайте прапорець --dry-run для виводу команди без виконання
    • Логувайте повний набір аргументів: log.Printf("🔧 Executing: %s %v", cmd, args)
    • Зберігайте останні 100 рядків stderr у тимчасовий файл при збої
```

---

## 🎯 Висновок

> **`cmd` — це "безпечний міст" між Go та системними інструментами**, який забезпечує:
> • ✅ Кросплатформний пошук бінарників без зайвих процесів
> • ✅ Контроль життєвого циклу через `context` (таймаути, скасування)
> • ✅ Повне захоплення виводу (`stdout` + `stderr`) для точної діагностики
> • ✅ Ізоляцію від shell-ін'єкцій та витоку конфіденційних даних
> • ✅ Інтеграцію з конвеєром обробки медіа через типобезпечний API

Для вашого **CCTV HLS Processor** це означає:
- 🛡️ Надійна перевірка залежностей перед запуском конвеєра
- ⚡ Безпечне виконання `ffmpeg`/`ffprobe` з контролем часу та ресурсів
- 🔍 Детальна діагностика помилок кодування через захоплення `stderr`
- 🔄 Легке скасування довгих операцій при перезавантаженні сервіса
- 🌐 Кросплатформна сумісність (Linux, macOS, Windows) без змін коду

Потребуєте допомоги з інтеграцією цього пакету у ваш конвеєр транскодування або з налаштуванням retry-логіки для нестабільних процесів? Напишіть — покажу готовий код для вашого сценарію! 🚀🔧