# 🔊 `Dac3`: AC-3 (Dolby Digital) Codec Configuration Box у MP4

Це код для роботи з **аудіо-кодеком AC-3** (Dolby Digital) у форматі MP4/fMP4 (ISOBMFF). AC-3 — це стандарт багатоканального аудіо, що широко використовується у телебаченні, кіно та стрімінгу.

---

## 🎯 Коротка відповідь

> **`Dac3` — це "паспорт" AC-3 аудіо**, який каже плеєру: *"Це аудіо закодовано в Dolby Digital, ось параметри для декодування: частота дискретизації, бітрейт, кількість каналів, режим бітстріму"*.

---

## 🧱 Архітектура: Два типи боксів

### 🔹 `ac-3` — AC-3 Audio Sample Entry (в `stsd`)

```go
func BoxTypeAC3() BoxType { return StrToBoxType("ac-3") }

func init() {
	AddAnyTypeBoxDef(&AudioSampleEntry{}, BoxTypeAC3())
}
```

**Де зустрічається**: `moov → trak → mdia → minf → stbl → stsd → ac-3`

**Призначення**: Оголошує, що ця аудіо-доріжка містить **звук у кодеку AC-3**.

```
📦 stsd (Sample Description) для аудіо:
├── 📦 mp4a  ← AAC (найпоширеніший)
├── 📦 ac-3  ← Dolby Digital ✅ (цей бокс!)
├── 📦 ec-3  ← Dolby Digital Plus (E-AC-3)
└── 📦 dtsc  ← DTS Core
```

> 🎯 `AudioSampleEntry` — базова структура для всіх аудіо-кодеків. Бібліотека сама підставить специфічні поля для AC-3.

---

### 🔹 `dac3` — AC-3 Decoder Configuration Box

```go
func BoxTypeDAC3() BoxType { return StrToBoxType("dac3") }

func init() {
	AddBoxDef(&Dac3{})  // реєструємо структуру для парсингу
}
```

**Де зустрічається**: Зазвичай всередині `ac-3` боксу як додаткова конфігурація.

**Призначення**: Містить **детальні параметри декодера** у компактному 3-байтовому форматі.

---

## 🔍 Детальний розбір структури `Dac3`

```go
type Dac3 struct {
	Box  // ← базовий тип для будь-якого боксу
	
	// 🔹 Усього 3 байти (24 біти) для всіх параметрів!
	
	// Байт 0: [Fscod:2][Bsid:5][Bsmod:3] = 10 біт → вирівнюється до 2 байт
	Fscod       uint8 `mp4:"0,size=2"`   // частота дискретизації (0=48kHz, 1=44.1kHz, 2=32kHz)
	Bsid        uint8 `mp4:"1,size=5"`   // версія бітстріму (8-10 для AC-3, 11+ для E-AC-3)
	Bsmod       uint8 `mp4:"2,size=3"`   // режим бітстріму (0=main, 1=commentary, 2=hearing impaired...)
	
	// Байт 1-2: [Acmod:3][LfeOn:1][BitRateCode:5][Reserved:5] = 14 біт
	Acmod       uint8 `mp4:"3,size=3"`   // режим аудіо-каналів (1=моно, 2=стерео, 5=5.1, 7=7.1...)
	LfeOn       uint8 `mp4:"4,size=1"`   // чи є LFE-канал (сабвуфер)? 0=ні, 1=так
	BitRateCode uint8 `mp4:"5,size=5"`   // код бітрейту (індекс у таблиці: 0-31)
	
	// Зарезервовано: 5 біт (завжди 0)
	Reserved    uint8 `mp4:"6,size=5,const=0"`  // завжди 0
}
```

---

## 📐 Візуалізація бітового формату (3 байти)

```
📦 Dac3 бокс (рівно 3 байти + заголовок боксу)
┌─────────────────────────────────┐
│ Байт 0: [Fscod:2][Bsid:5][Bsmod:3] │
│         [10][01010][001] = 0xA8   │ ← Fscod=2(32kHz), Bsid=10, Bsmod=1
├─────────────────────────────────┤
│ Байт 1: [Acmod:3][Lfe:1][BitRate:5]│
│         [101][1][01100] = 0xEC    │ ← Acmod=5(5.1), LFE=1, BitRate=12
├─────────────────────────────────┤
│ Байт 2: [Reserved:5][...pad:3]    │
│         [00000][000] = 0x00       │ ← завжди 0
└─────────────────────────────────┘
```

> 🎯 **Магія бібліотеки**: Ви працюєте з окремими полями (`Fscod`, `Acmod`), а бібліотека сама пакує їх у 3 байти за тегами `mp4:"...,size=N"`.

---

## 🔑 Розбір важливих полів

### 🔹 `Fscod` (2 біти) — частота дискретизації

| Значення | Частота | Використання |
|----------|---------|-------------|
| `0` | 48000 Hz | ✅ Стандарт для ТВ/стрімінгу |
| `1` | 44100 Hz | CD-якість, рідше для відео |
| `2` | 32000 Hz | ✅ Економія бітрейту для мовлення |
| `3` | Reserved | Не використовувати |

**Для вашого CCTV**: Зазвичай `0` (48 kHz) для якісного звуку, або `2` (32 kHz) для економії бітрейту.

---

### 🔹 `Bsid` (5 біт) — версія бітстріму

| Значення | Опис |
|----------|------|
| `8` | AC-3 (стандартний) |
| `9-10` | AC-3 з розширеннями |
| `11+` | E-AC-3 (Dolby Digital Plus) |

**Для вашого CCTV**: Зазвичай `8` або `10` для стандартного AC-3.

---

### 🔹 `Bsmod` (3 біти) — режим бітстріму

| Значення | Режим | Опис |
|----------|-------|------|
| `0` | Main | Основний аудіопотік |
| `1` | Commentary | Коментарі/озвучка |
| `2` | VisuallyImpaired | Для слабозорих (аудіо-опис) |
| `3` | HearingImpaired | Для слабочуючих (підсилені діалоги) |
| `4` | Dialogue | Тільки діалоги |
| `5` | Music | Тільки музика |
| `6-7` | Reserved | Майбутнє використання |

**Для вашого CCTV**: Зазвичай `0` (Main), але можна додавати окремі доріжки для `2` (аудіо-опис) або `3` (підсилені діалоги).

---

### 🔹 `Acmod` (3 біти) — режим аудіо-каналів ⭐ Найважливіше!

| Значення | Конфігурація | Канали | Опис |
|----------|-------------|--------|------|
| `0` | 1+1 | 2 | Два моно-канали (не стерео!) |
| `1` | 1/0 | 1 | Моно |
| `2` | 2/0 | 2 | Стерео (L, R) |
| `3` | 3/0 | 3 | Стерео + центр (L, C, R) |
| `4` | 2/1 | 3 | Стерео + задній центр (L, R, Cs) |
| `5` | 3/1 | 4 | 3.1 (L, C, R, Cs) |
| `6` | 2/2 | 4 | Квадро (L, R, Ls, Rs) |
| `7` | 3/2 | 6 | ✅ 5.1 (L, C, R, Ls, Rs, LFE) |

**Для вашого CCTV**: 
- `2` (стерео) — для більшості камер
- `7` (5.1) — для професійних студійних трансляцій

---

### 🔹 `LfeOn` (1 біт) — низькочастотний ефект (сабвуфер)

| Значення | Опис |
|----------|------|
| `0` | Немає LFE-каналу |
| `1` | ✅ Є LFE-канал (сабвуфер, 20-120 Hz) |

> 🎯 **Важливо**: `LfeOn=1` має сенс тільки якщо `Acmod >= 5` (є об'ємний звук).

---

### 🔹 `BitRateCode` (5 біт) — код бітрейту

Це **індекс у таблиці** стандартних бітрейтів для даної конфігурації:

| Acmod | BitRateCode=0 | BitRateCode=10 | BitRateCode=31 |
|-------|--------------|----------------|----------------|
| 2 (стерео) | 32 kbps | 224 kbps | 640 kbps |
| 7 (5.1) | 96 kbps | 448 kbps | 640 kbps |

**Повна таблиця** (ETSI TS 102 366, Annex D):
```go
// Приклад: для Acmod=7 (5.1)
bitrateTable := map[int][]int{
	7: {96, 128, 160, 192, 224, 256, 320, 384, 448, 512, 576, 640},
	// ... інші Acmod ...
}
```

**Для вашого CCTV**: 
- `BitRateCode=5-8` (224-384 kbps) — оптимально для 5.1
- `BitRateCode=2-4` (96-160 kbps) — для стерео з економією бітрейту

---

## 🛠️ Практичне використання у вашому HLS-процесорі

### 🔹 Приклад 1: Читання конфігурації AC-3 з fMP4-сегмента

```go
import "github.com/abema/go-mp4"

func extractAC3Config(filePath string) (*mp4.Dac3, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	var dac3 *mp4.Dac3
	
	// Рекурсивний парсинг всіх боксів
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		if h.BoxInfo.Type == mp4.BoxTypeDAC3() {
			// Знайшли dac3 — розпарсити його
			dac3 = &mp4.Dac3{}
			_, err := h.ReadPayload(dac3)
			return dac3, err
		}
		return nil, nil
	})
	
	return dac3, err
}

// Використання:
config, _ := extractAC3Config("segment_000123.m4s")
if config != nil {
	// Декодуємо параметри
	sampleRate := []int{48000, 44100, 32000}[config.Fscod]
	channelConfig := []string{"1+1", "1/0", "2/0", "3/0", "2/1", "3/1", "2/2", "3/2"}[config.Acmod]
	hasLFE := config.LfeOn == 1
	
	log.Printf("🔊 AC-3: %s, %d Hz, %s%s, bitrate~%d kbps",
		channelConfig,
		sampleRate,
		channelConfig,
		map[bool]string{true: " + LFE", false: ""}[hasLFE],
		config.BitRateCode*32, // приблизно
	)
	
	// Перевірка сумісності:
	if config.Acmod == 7 && config.BitRateCode < 5 {
		log.Printf("⚠️  5.1 audio with low bitrate may sound poor")
	}
}
```

---

### 🔹 Приклад 2: Валідація вхідного аудіо-стріму

```go
func validateAC3Stream(dac3 *mp4.Dac3) error {
	// 🔹 Перевірка частоти дискретизації
	if dac3.Fscod == 3 {
		return fmt.Errorf("invalid Fscod=%d (reserved)", dac3.Fscod)
	}
	
	// 🔹 Перевірка версії бітстріму
	if dac3.Bsid > 10 {
		log.Printf("⚠️  Bsid=%d may indicate E-AC-3, not standard AC-3", dac3.Bsid)
	}
	
	// 🔹 Перевірка конфігурації каналів
	validAcmod := map[uint8]bool{1: true, 2: true, 3: true, 5: true, 7: true}
	if !validAcmod[dac3.Acmod] {
		return fmt.Errorf("unsupported Acmod=%d (channel config)", dac3.Acmod)
	}
	
	// 🔹 LFE має сенс тільки для об'ємного звуку
	if dac3.LfeOn == 1 && dac3.Acmod < 5 {
		log.Printf("⚠️  LFE enabled but Acmod=%d doesn't support surround", dac3.Acmod)
	}
	
	// 🔹 Перевірка бітрейту (для 5.1 мінімум 224 kbps)
	if dac3.Acmod == 7 && dac3.BitRateCode < 5 {
		log.Printf("⚠️  5.1 audio with BitRateCode=%d (<224kbps) may have poor quality", 
			dac3.BitRateCode)
	}
	
	// 🔹 Перевірка зарезервованих бітів
	if dac3.Reserved != 0 {
		return fmt.Errorf("invalid Reserved=%d (must be 0)", dac3.Reserved)
	}
	
	return nil
}
```

---

### 🔹 Приклад 3: Генерація HLS-плейлиста з аудіо-доріжками

```go
func generateAudioPlaylist(tracks []AudioTrack) string {
	var sb strings.Builder
	
	for _, track := range tracks {
		if track.Codec == "ac-3" && track.Dac3 != nil {
			// Формуємо атрибут CHANNELS для #EXT-X-MEDIA
			channels := acmodToChannels(track.Dac3.Acmod, track.Dac3.LfeOn)
			
			// Формуємо атрибут CODECS (опціонально для AC-3)
			// Деякі плеєри підтримують: CODECS="ac-3"
			
			sb.WriteString(fmt.Sprintf(
				`#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",`+
				`NAME="%s",LANGUAGE="%s",`+
				`CHANNELS="%d",AUTOSELECT=YES,DEFAULT=YES,`+
				`URI="%s"`+"\n",
				track.Name,
				track.Language,
				channels,
				track.PlaylistURL,
			))
		}
	}
	
	return sb.String()
}

// Допоміжна функція: Acmod + LFE → кількість каналів
func acmodToChannels(acmod, lfe uint8) int {
	base := []int{2, 1, 2, 3, 3, 4, 4, 6}[acmod] // без LFE
	if lfe == 1 {
		return base + 1
	}
	return base
}
```

**Результат у .m3u8**:
```m3u8
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Українська",LANGUAGE="uk",
CHANNELS="6",AUTOSELECT=YES,DEFAULT=YES,URI="audio_uk_ac3.m3u8"
```

---

## 🔍 Як це пов'язано з вашим CCTV HLS Processor?

```
📡 Ваш потік обробки аудіо:
1. Приймаєте fMP4-фрагмент з камери
   │
   ▼
2. Парсите dac3 бокс для:
   • Визначення кількості каналів (стерео/5.1?)
   • Валідації бітрейту (чи достатньо для якості?)
   • Виявлення спеціальних доріжок (аудіо-опис, субтитри для слабочуючих)
   │
   ▼
3. При необхідності:
   • Даунміксуєте 5.1 → стерео для мобільних клієнтів
   • Додаєте мітку "AUDIO-DESCRIPTION" для доріжки Bsmod=2
   • Логуєте параметри для моніторингу якості звуку
   │
   ▼
4. Генеруєте .m3u8 з правильними атрибутами:
   #EXT-X-MEDIA:CHANNELS="2",NAME="Stereo"
   #EXT-X-MEDIA:CHANNELS="6",NAME="5.1 Surround"
```

---

## 🧪 Приклад тесту для `Dac3`

```go
func TestDac3_MarshalUnmarshal(t *testing.T) {
	// Створити тестову конфігурацію: 5.1 @ 48kHz, 448 kbps
	src := &mp4.Dac3{
		Fscod:       0,           // 48 kHz
		Bsid:        8,           // AC-3 standard
		Bsmod:       0,           // Main audio
		Acmod:       7,           // 3/2 = 5.1 channels
		LfeOn:       1,           // LFE enabled
		BitRateCode: 8,           // 448 kbps for 5.1
		Reserved:    0,           // always 0
	}
	
	// 🔹 Marshal: структура → 3 байти
	buf := bytes.NewBuffer(nil)
	n, err := mp4.Marshal(buf, src, mp4.Context{})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), n)  // Dac3 завжди 3 байти!
	
	// 🔹 Перевірка бітів (очікуємо: 0x00 0xEC 0x00)
	expected := []byte{0x00, 0xEC, 0x00}
	assert.Equal(t, expected, buf.Bytes())
	
	// 🔹 Unmarshal: 3 байти → структура
	dst := &mp4.Dac3{}
	r := bytes.NewReader(buf.Bytes())
	_, err = mp4.Unmarshal(r, 3, dst, mp4.Context{})
	require.NoError(t, err)
	
	// 🔹 Round-trip перевірка
	assert.Equal(t, src, dst)
	assert.Equal(t, uint8(7), dst.Acmod)  // 5.1
	assert.Equal(t, uint8(1), dst.LfeOn)  // LFE enabled
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильне вирівнювання бітів | Поля "з'їжджають", парсинг ламається | Дотримуйтесь порядку `mp4:"0", mp4:"1", ...` |
| `Reserved != 0` | Декодер відмовляє читати бокс | Завжди встановлюйте `Reserved=0` |
| `LfeOn=1` без `Acmod>=5` | Сабвуфер не працює або артефакти | Вмикайте LFE тільки для 3.1/5.1/7.1 |
| Неправильний `BitRateCode` | Звук "захлинається" або тихий | Використовуйте таблицю бітрейтів з стандарту |
| Ігнорування `Bsmod` | Спеціальні доріжки не відображаються в плеєрі | Передавайте `Bsmod` у метадані плейлиста |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні аудіо-сегмента:
    • Витягніть dac3 бокс через ReadBoxStructure + UnmarshalAny
    • Перевірте Reserved=0
    • Залогуйте конфігурацію: log.Printf("🔊 %s", Stringify(dac3, ctx))

[ ] Для сумісності з вебом:
    • Стерео (Acmod=2) працює всюди
    • 5.1 (Acmod=7) підтримується в Safari, Chrome, Firefox
    • Уникайте екзотичних конфігурацій (Acmod=0,4,6)

[ ] При генерації HLS-плейлиста:
    • Додайте CHANNELS="N" для кожної аудіо-доріжки
    • Для спеціальних доріжок: CHARACTERISTICS="public.accessibility.describes-video"
    • Вкажіть LANGUAGE="uk" для українського аудіо

[ ] Для дебагу:
    • Логуйте сирий вміст: log.Printf("📦 dac3: % x", dac3Bytes)
    • Перевіряйте Acmod + LfeOn: чи співпадає з очікуваним?
    • Використовуйте Stringify() для людського виводу

[ ] Для тестування:
    • Напишіть round-trip тест: Marshal → Unmarshal → порівняння
    • Протестуйте на реальних плеєрах: VLC, hls.js, ExoPlayer, Safari
    • Перевірте відтворення на різних пристроях (ТВ, телефон, ПК)
```

---

## 🎯 Висновок

> **`Dac3` — це ключ до коректного відтворення AC-3 аудіо у вашому HLS-стрімі**.  
> Він забезпечує:
> • ✅ Ініціалізацію декодера з правильними параметрами
> • ✅ Підтримку багатоканального звуку (стерео, 5.1)
> • ✅ Ефективне кодування: всього 3 байти для всіх параметрів
> • ✅ Сумісність зі стандартом ETSI TS 102 366

Для вашого **CCTV HLS Processor** це означає:
- 🔊 Клієнти чують звук без артефактів та десинхронізації
- 🌐 Підтримка багатоканального аудіо для професійних трансляцій
- 🎧 Можливість додавати спеціальні доріжки (аудіо-опис, посилені діалоги)
- 📉 Економія бітрейту завдяки точному опису параметрів

Потребуєте допомоги з кодуванням `CHANNELS=` для HLS-плейлиста або з валідацією бітрейтів? Напишіть — покажу готовий код! 🚀🔊