# Глибоке роз'яснення: як використовувати TS Demuxer CLI

Цей Go-інструмент призначений для аналізу MPEG-TS (Transport Stream) потоків — формат, що використовується у цифровому телебаченні, HLS-стрімінгу та супутниковому мовленні.

---

## 📦 Основні можливості

```
┌─────────────────────────────────────────┐
│ • Читання з файлу (.ts) або UDP multicast │
│ • Демодексування PAT/PMT/PES/EIT/SDT/NIT │
│ • Три режими роботи: packets, data, programs │
│ • JSON або текстовий вивід               │
│ • CPU/Memory профілювання                │
│ • Graceful shutdown (SIGINT/SIGTERM)    │
└─────────────────────────────────────────┘
```

---

## 🚀 Швидкий старт

### 1. Збірка
```bash
go build -o tsdemux main.go
```

### 2. Базові приклади використання

#### 🔹 Аналіз файлу .ts (режим programs — за замовчуванням)
```bash
# Текстовий вивід програм
./tsdemux -i input.ts

# JSON вивід
./tsdemux -i input.ts -f json

# Тільки певні типи даних
./tsdemux -i input.ts -d pat,pmt,pes data
```

#### 🔹 Аналіз UDP multicast потоку
```bash
# Прослуховування multicast адреси
./tsdemux -i "udp://239.0.0.1:5000" data -d all
```

#### 🔹 Режим packets — сирі TS-пакети
```bash
./tsdemux -i input.ts packets | head -20
```

#### 🔹 Профілювання
```bash
# CPU профіль (збереже cpu.pprof)
./tsdemux -i input.ts -cp programs

# Memory профіль
./tsdemux -i input.ts -mp programs

# Аналіз профілю:
go tool pprof cpu.pprof
```

---

## 📋 Повний список прапорів

| Прапор | Опис | Приклад |
|--------|--------|---------|
| `-i` | Шлях до входу: файл або `udp://host:port` | `-i stream.ts` |
| `-d` | Whitelist типів даних: `all,pat,pmt,pes,eit,nit,sdt,tot` | `-d pat,pmt` |
| `-f` | Формат виводу: `json` або текстовий (за замовчуванням) | `-f json` |
| `-cp` | Увімкнути CPU профілювання | `-cp` |
| `-mp` | Увімкнути Memory профілювання | `-mp` |

> ⚠️ Прапори `-cp`/`-mp` зберігають профайли в поточній директорії як `cpu.pprof`/`mem.pprof`.

---

## 🔍 Режими роботи (команди)

### 1️⃣ `programs` (за замовчуванням)
Аналізує PAT → PMT і будує структуру програм:
```bash
./tsdemux -i broadcast.ts -f json programs
```

**Приклад виводу (JSON):**
```json
[
  {
    "id": 101,
    "map_id": 501,
    "descriptors": ["[Service] service: Channel1 | provider: Broadcaster"],
    "streams": [
      {
        "id": 256,
        "type": 27,
        "descriptors": ["[Stream identifier] stream identifier component tag: 1"]
      }
    ]
  }
]
```

### 2️⃣ `data` — демодексування таблиць
Виводить вміст PSI/SI таблиць:
```bash
# Тільки PAT та PMT
./tsdemux -i stream.ts -d pat,pmt data

# Всі доступні таблиці
./tsdemux -i stream.ts -d all data
```

**Що логується:**
- **PAT**: список програм та їх PMT PID
- **PMT**: PID відео/аудіо потоків, дескриптори
- **PES**: заголовки PES-пакетів (stream_id, PTS/DTS якщо є)
- **EIT**: події програми (назва, час, опис)
- **SDT/NIT/TOT**: метадані мережі, часу, сервісів

### 3️⃣ `packets` — низькорівневий аналіз
Показує кожен TS-пакет (188 байт):
```bash
./tsdemux -i capture.ts packets | grep "PKT: 256"
```

**Приклад логу:**
```
PKT: 256
  Continuity Counter: 3
  Payload Unit Start Indicator: true
  Has Payload: true
  Has Adaptation Field: false
  Transport Error Indicator: false
  ...
```

---

## 🌐 Робота з UDP Multicast

### Налаштування мережі (Linux)
```bash
# Додати маршрут для multicast (якщо потрібно)
sudo ip route add 239.0.0.0/8 dev eth0

# Приєднатися до групи (тест)
socat -u UDP4-RECV:5000,ip-add-membership=239.0.0.1:eth0 -
```

### Запуск демуксера
```bash
./tsdemux -i "udp://239.0.0.1:5000" -d pat,pmt data
```

> 💡 Буфер читання встановлено в 4096 байт. Для високобітрейт потоків можна збільшити в коді: `c.SetReadBuffer(65536)`.

---

## 🧩 Інтеграція з вашим HLS/CCTV пайплайном

Оскільки ви розробляєте **CCTV HLS Processor на Go**, цей інструмент може бути корисним для:

### ✅ Валідація вхідних TS-сегментів
```go
// Приклад: перевірка, чи містить сегмент відео H.264
func hasH264Stream(tsPath string) (bool, error) {
    dmx := astits.NewDemuxer(ctx, os.Open(tsPath))
    for {
        d, err := dmx.NextData()
        if err != nil { break }
        if d.PMT != nil {
            for _, es := range d.PMT.ElementaryStreams {
                if es.StreamType == astits.StreamTypeH264Video {
                    return true, nil
                }
            }
        }
    }
    return false, nil
}
```

### ✅ Extract PID для відео/аудіо
```go
func extractPIDs(tsPath string) (videoPID, audioPID uint16, err error) {
    // ... аналіз PAT→PMT як у функції programs()
    // повертає PID для подальшої обробки через ffmpeg
}
```

### ✅ Синхронізація через EIT/TOT
```go
// Якщо потік містить EIT — можна витягти PROGRAM-DATE-TIME
// для корекції розривів у HLS-плейлисті
if d.EIT != nil {
    for _, event := range d.EIT.Events {
        startTime := event.StartTime // time.Time
        // синхронізувати з вашим segmentTimestamps
    }
}
```

---

## 🐛 Поширені проблеми та рішення

| Проблема | Причина | Рішення |
|----------|---------|---------|
| `no more packets` одразу | Потік закінчився або не TS-формат | Перевірте вхідний файл: `ffprobe input.ts` |
| Не бачить PAT/PMT | Потік зашифрований або scrambled | Перевірте `TransportScramblingControl` у пакетах |
| UDP не приймає дані | Firewall / відсутній IGMP | `sudo iptables -I INPUT -p udp --dport 5000 -j ACCEPT` |
| JSON вивід зламаний | Лог-повідомлення змішуються з JSON | Використовуйте `-f json` + перенаправте stderr: `2>/dev/null` |

---

## 🔧 Розширення для ваших потреб

### Додати фільтрацію за PID
```go
// У функції data():
if d.PES != nil && pidFilter[d.PID] {
    // обробка тільки потрібних PID
}
```

### Експорт у Prometheus-метрики
```go
// У циклі обробки:
metrics.TSPacketsProcessed.WithLabelValues(channelID).Inc()
if d.PAT != nil {
    metrics.PATDetected.WithLabelValues(channelID).Set(1)
}
```

### Інтеграція з вашим `segmentAssembler`
```go
// Після сегментації:
tsData, _ := io.ReadAll(segmentReader)
dmx := astits.NewDemuxer(ctx, bytes.NewReader(tsData))
// витягти PTS першого ключового кадру для синхронізації
```

---

## 📚 Корисні посилання

- [astits документація](https://pkg.go.dev/github.com/asticode/go-astits)
- [MPEG-TS специфікація (ISO/IEC 13818-1)](https://www.iso.org/standard/61236.html)
- [DVB SI таблиці (ETSI EN 300 468)](https://www.etsi.org/deliver/etsi_en/300400_300499/300468/01.15.01_60/en_300468v011501p.pdf)

---

> 💡 **Порада**: Для відладки використовуйте короткі .ts-файли, згенеровані через `ffmpeg -f mpegts`. Наприклад:
> ```bash
> ffmpeg -i test.mp4 -c copy -f mpegts -t 10 short.ts
> ```

Якщо потрібно — можу допомогти адаптувати цей код під ваш пайплайн (наприклад, додати експорт PID у ваш `segmentFinalizer` або інтегрувати валідацію сегментів перед FFmpeg-обробкою). 🛠️