# 🔐 `pssh` та `tenc`: DRM та Common Encryption у MP4

Це код з бібліотеки `go-mp4` для роботи з **захищеним контентом** у форматі MP4/fMP4 (ISOBMFF) згідно зі стандартом **ISO/IEC 23001-7 (Common Encryption, CENC)**. Ці бокси використовуються для **DRM-систем**: Widevine (Google), PlayReady (Microsoft), FairPlay (Apple).

---

## 🎯 Коротка відповідь

> **`pssh` та `tenc` — це "ключі" до зашифрованого відео/аудіо**.  
> • `pssh` (Protection System Specific Header) — містить ліцензійні дані для конкретної DRM-системи  
> • `tenc` (Track Encryption) — описує, **як саме** зашифровані семпли у доріжці

---

## 🧱 Архітектура: Два типи боксів для DRM

### 🔹 `pssh` — Protection System Specific Header

```go
func BoxTypePssh() BoxType { return StrToBoxType("pssh") }

func init() {
	AddBoxDef(&Pssh{}, 0, 1)  // підтримуються версії 0 та 1
}
```

**Де зустрічається**: Зазвичай на рівні `moov` або `moof`:
```
📦 moov
├── 📦 pssh ← Заголовок DRM-системи (Widevine/PlayReady/FairPlay)
└── 📦 trak
    └── ...
```

**Призначення**: Передає **системо-специфічні дані** для отримання ліцензії на декодування.

---

### 🔹 `tenc` — Track Encryption Box

```go
func BoxTypeTenc() BoxType { return StrToBoxType("tenc") }

func init() {
	AddBoxDef(&Tenc{}, 0, 1)
}
```

**Де зустрічається**: Всередині `sinf` → `schm` → `tenc`:
```
📦 trak
└── 📦 mdia
    └── 📦 minf
        └── 📦 sinf (Protection Scheme Info)
            ├── 📦 schm (Scheme Type: "cenc")
            └── 📦 tenc ← Параметри шифрування доріжки
```

**Призначення**: Описує **параметри шифрування**: який ключ, який розмір IV, які семпли зашифровані.

---

## 🔍 Детальний розбір `Pssh` — DRM-заголовок

```go
type Pssh struct {
	FullBox  `mp4:"0,extend"`
	SystemID [16]byte  `mp4:"1,size=8,uuid"`  // 🔹 UUID DRM-системи
	KIDCount uint32    `mp4:"2,size=32,nver=0"`  // 🔹 Кількість Key IDs (тільки версія 1!)
	KIDs     []PsshKID `mp4:"3,nver=0,len=dynamic,size=128"`  // 🔹 Список Key IDs
	DataSize int32     `mp4:"4,size=32"`  // 🔹 Розмір DRM-даних
	Data     []byte    `mp4:"5,size=8,len=dynamic"`  // 🔹 Сирі DRM-дані (напр. Widevine PSSH)
}
```

### 🔹 `SystemID` — UUID DRM-системи (16 байт)

| UUID | Система | Використання |
|------|---------|-------------|
| `1077efec-c0b2-4d02-ace3-3c1e52e2fb4b` | 🔹 **Widevine** (Google) | ✅ Android, Chrome, Smart TV |
| `9a04f079-9840-4286-ab92-e65be0885f95` | 🔹 **PlayReady** (Microsoft) | ✅ Edge, Xbox, Windows |
| `94ce86fb-07ff-4f43-adb8-93d2fa968ca2` | 🔹 **FairPlay** (Apple) | ✅ iOS, macOS, Safari |
| `a2d843c5-3c5e-4d4f-9c1e-8e5e5c5e5c5e` | ClearKey (тестування) | 🔧 Розробка |

**Приклад отримання UUID у коді:**
```go
widevineUUID := uuid.MustParse("1077efec-c0b2-4d02-ace3-3c1e52e2fb4b")
var systemID [16]byte
copy(systemID[:], widevineUUID[:])
```

---

### 🔹 `KIDs` — Key IDs (тільки версія 1!)

```go
type PsshKID struct {
	KID [16]byte `mp4:"0,size=8,uuid"`  // UUID ключа шифрування
}
```

**Навіщо це?** Один контент може бути зашифрований **кількома ключами** (напр. для різних регіонів або рівнів доступу). `KIDs` перелічує всі ключі, потрібні для декодування.

> ⚠️ **Важливо**: Поля `KIDCount` та `KIDs` мають тег `nver=0` → вони **відсутні у версії 0**, присутні тільки у версії 1!

---

### 🔹 `Data` — сирі DRM-дані 🔥 Найважливіше!

```go
Data []byte `mp4:"5,size=8,len=dynamic"`
```

**Що тут міститься?** Залежить від `SystemID`:

#### 🟢 Для Widevine:
```protobuf
// Pssh.proto (спрощено)
message Pssh {
  bytes content_id = 1;
  bytes policy = 2;
  // ... інші поля ...
}
```
**Приклад байтів**: `08 01 12 10 ...` (protobuf-кодування)

#### 🔵 Для PlayReady:
```xml
<!-- WRMSHEADER (XML у base64) -->
<WRMHEADER>
  <DATA>
    <PROTECTINFO>
      <KEYID>...</KEYID>
      <ALGID>AESCTR</ALGID>
    </PROTECTINFO>
  </DATA>
</WRMHEADER>
```

#### 🍎 Для FairPlay:
```
fps_cert: <сертифікат>
fps_content_id: <ідентифікатор контенту>
fps_license_type: <тип ліцензії>
```

> 🎯 **Це "серце" DRM**: без коректних `Data` плеєр не зможе отримати ліцензію → відео не відтвориться.

---

### 🔹 `GetFieldLength` — динамічні поля

```go
func (pssh *Pssh) GetFieldLength(name string, ctx Context) uint {
	switch name {
	case "KIDs":
		return uint(pssh.KIDCount)  // 🔹 Кількість елементів = значення KIDCount
	case "Data":
		return uint(pssh.DataSize)  // 🔹 Довжина байтів = значення DataSize
	}
	panic(fmt.Errorf("invalid field: %s", name))
}
```

**🎯 Магія**: Бібліотека питає: "Скільки елементів у `KIDs`?" → Ви відповідаєте: "Стільки, скільки в `KIDCount`!" → Бібліотека читає точно стільки.

---

### 🔹 `StringifyField` — людино-читабельний вивід UUID

```go
func (pssh *Pssh) StringifyField(name string, indent string, depth int, ctx Context) (string, bool) {
	switch name {
	case "KIDs":
		buf := bytes.NewBuffer(nil)
		buf.WriteString("[")
		for i, e := range pssh.KIDs {
			if i != 0 { buf.WriteString(", ") }
			// 🔹 Перетворюємо [16]byte у uuid.UUID для гарного виводу
			buf.WriteString(uuid.UUID(e.KID).String())
		}
		buf.WriteString("]")
		return buf.String(), true
	default:
		return "", false
	}
}
```

**Результат у логах**:
```
🔐 PSSH: SystemID=1077efec-c0b2-4d02-ace3-3c1e52e2fb4b (Widevine)
   KIDs=[a1b2c3d4-e5f6-7890-abcd-ef1234567890, ...]
   DataSize=128, Data=[08 01 12 10 ...]
```

---

## 🔍 Детальний розбір `Tenc` — параметри шифрування доріжки

```go
type Tenc struct {
	FullBox                `mp4:"0,extend"`
	Reserved               uint8    `mp4:"1,size=8,dec"`
	DefaultCryptByteBlock  uint8    `mp4:"2,size=4,dec"`  // завжди 0 у версії 0
	DefaultSkipByteBlock   uint8    `mp4:"3,size=4,dec"`  // завжди 0 у версії 0
	DefaultIsProtected     uint8    `mp4:"4,size=8,dec"`  // 🔹 1=зашифровано, 0=немає
	DefaultPerSampleIVSize uint8    `mp4:"5,size=8,dec"`  // 🔹 Розмір IV на семпл (0, 8, 16)
	DefaultKID             [16]byte `mp4:"6,size=8,uuid"` // 🔹 UUID ключа шифрування
	// 🔹 Опціональні поля для Constant IV:
	DefaultConstantIVSize  uint8    `mp4:"7,size=8,opt=dynamic,dec"`
	DefaultConstantIV      []byte   `mp4:"8,size=8,opt=dynamic,len=dynamic"`
}
```

### 🔹 `DefaultIsProtected` — чи зашифрована доріжка?

| Значення | Опис |
|----------|------|
| `0` | 🔹 Доріжка **не зашифрована** (clear) |
| `1` | 🔹 Доріжка **зашифрована** (protected) |

> 🎯 **Важливо**: Якщо `0` — решта полів ігноруються, контент відтворюється без ліцензії.

---

### 🔹 `DefaultPerSampleIVSize` — розмір Initialization Vector (IV)

IV — це "сіль" для шифрування, щоб однакові дані давали різний зашифрований результат.

| Значення | Розмір | Використання |
|----------|--------|-------------|
| `0` | 🔹 Немає IV на семпл | Використовується `DefaultConstantIV` |
| `8` | 8 байт (64 біти) | Рідко, застарілі системи |
| `16` | 🔹 16 байт (128 біт) | ✅ Стандарт для AES-CTR/CBCS |

**Як це працює:**
```
🔐 AES-CTR шифрування:
• Кожний семпл шифрується окремо
• IV = counter, що інкрементується для кожного блоку
• Без правильного IV → неможливо декодувати

📦 Приклад:
Семпл 1: IV = 0x0000000000000001
Семпл 2: IV = 0x0000000000000002
...
```

---

### 🔹 `DefaultKID` — Key ID (UUID ключа шифрування)

```go
DefaultKID [16]byte `mp4:"6,size=8,uuid"`
```

**Навіщо це?** Один контент може мати **кілька ключів** (напр. для різних рівнів доступу). `DefaultKID` вказує, який ключ використовувати за замовчуванням.

**Приклад**: 
```
KID: a1b2c3d4-e5f6-7890-abcd-ef1234567890
→ Плеєр запитує ліцензію для цього KID у DRM-сервері
→ Отримує ключ дешифрування
→ Декодує контент
```

---

### 🔹 `DefaultConstantIV` — постійний IV (опціонально)

```go
func (tenc *Tenc) IsOptFieldEnabled(name string, ctx Context) bool {
	switch name {
	case "DefaultConstantIVSize", "DefaultConstantIV":
		// 🔹 Увімкнути, тільки якщо:
		// • Контент зашифровано (IsProtected=1)
		// • Немає пер-семпл IV (PerSampleIVSize=0)
		return tenc.DefaultIsProtected == 1 && tenc.DefaultPerSampleIVSize == 0
	}
	return false
}
```

**Коли використовується?**
- Для режимів шифрування, де **один IV для всього контенту** (рідко)
- Для тестування/прототипів

**Приклад**:
```
DefaultConstantIVSize = 16
DefaultConstantIV = [0x00, 0x01, 0x02, ..., 0x0F]  // 16 байт
→ Всі семпли шифруються з одним і тим самим IV
```

> ⚠️ **Увага**: Використання одного IV для всього контенту **знижує безпеку**! Рекомендується `PerSampleIVSize=16`.

---

## 🔑 Ключові концепції Common Encryption (CENC)

### 🔹 Схема шифрування: `cenc` vs `cbcs`

| Схема | Опис | Використання |
|-------|------|-------------|
| `cenc` (AES-CTR) | 🔹 Шифрує тільки "важливі" байти (напр. не заголовки) | ✅ Widevine, PlayReady |
| `cbcs` (AES-CBC) | 🔹 Шифрує блоки з "пропуском" байтів (Crypt/Skip pattern) | ✅ FairPlay, нові стандарти |

**Як вказати схему?** Через `schm` бокс:
```go
type Schm struct {
	FullBox       `mp4:"0,extend"`
	SchemeType    [4]byte `mp4:"1,size=8,string"`  // "cenc" або "cbcs"
	SchemeVersion uint32  `mp4:"2,size=32,hex"`
	// ...
}
```

---

### 🔹 Crypt/Skip pattern (`DefaultCryptByteBlock` / `DefaultSkipByteBlock`)

Для схеми `cbcs`:
```
📐 Формат: [Crypt N байт][Skip M байт][Crypt N байт][Skip M байт]...

🔹 Приклад: Crypt=1, Skip=9
[🔐1 байт][⚪9 байт][🔐1 байт][⚪9 байт]...

🎯 Навіщо? Шифрувати тільки "важливі" дані (напр. коефіцієнти DCT), 
   а заголовки залишати відкритими для швидкого аналізу.
```

> ⚠️ У версії 0 `tenc` ці поля завжди 0 — pattern задається на рівні семплів (`senc` бокс).

---

## 🛠️ Практичне використання у вашому HLS-процесорі

### 🔹 Приклад 1: Читання DRM-конфігурації з fMP4

```go
import (
	"github.com/abema/go-mp4"
	"github.com/google/uuid"
)

type DRMConfig struct {
	SystemID   uuid.UUID  // Widevine/PlayReady/FairPlay
	KIDs       []uuid.UUID  // Список ключів
	IsProtected bool
	IVSize     uint8
	DefaultKID uuid.UUID
	PSSHData   []byte  // сирі дані для ліцензійного запиту
}

func extractDRMConfig(filePath string) (*DRMConfig, error) {
	f, err := os.Open(filePath)
	if err != nil { return nil, err }
	defer f.Close()
	
	config := &DRMConfig{}
	
	_, err = mp4.ReadBoxStructure(f, func(h *mp4.ReadHandle) (interface{}, error) {
		// 🔹 Шукаємо pssh бокси
		if h.BoxInfo.Type == mp4.BoxTypePssh() {
			pssh := &mp4.Pssh{}
			if _, err := h.ReadPayload(pssh); err != nil {
				return nil, err
			}
			
			// Зберігаємо SystemID та PSSH-дані
			config.SystemID, _ = uuid.FromBytes(pssh.SystemID[:])
			config.PSSHData = pssh.Data
			
			// Збираємо KIDs (тільки для версії 1)
			if pssh.GetVersion() == 1 {
				for _, kid := range pssh.KIDs {
					kidUUID, _ := uuid.FromBytes(kid.KID[:])
					config.KIDs = append(config.KIDs, kidUUID)
				}
			}
		}
		
		// 🔹 Шукаємо tenc бокси
		if h.BoxInfo.Type == mp4.BoxTypeTenc() {
			tenc := &mp4.Tenc{}
			if _, err := h.ReadPayload(tenc); err != nil {
				return nil, err
			}
			
			config.IsProtected = tenc.DefaultIsProtected == 1
			config.IVSize = tenc.DefaultPerSampleIVSize
			config.DefaultKID, _ = uuid.FromBytes(tenc.DefaultKID[:])
		}
		
		return nil, nil
	})
	
	return config, err
}
```

---

### 🔹 Приклад 2: Генерація PSSH для Widevine

```go
func createWidevinePSSH(keyID uuid.UUID, contentID string) *mp4.Pssh {
	// 🔹 Формуємо Widevine PSSH-дані (спрощений protobuf)
	// Для продакшену використовуйте бібліотеку: github.com/google/uuid + protobuf
	widevineData := buildWidevinePsshData(keyID, contentID)
	
	return &mp4.Pssh{
		FullBox: mp4.FullBox{Version: 1, Flags: [3]byte{0, 0, 0}},
		SystemID: widevineUUID(),  // 1077efec-c0b2-4d02-ace3-3c1e52e2fb4b
		KIDCount: 1,
		KIDs: []mp4.PsshKID{
			{KID: toArray16(keyID)},
		},
		DataSize: int32(len(widevineData)),
		Data:     widevineData,
	}
}

func widevineUUID() [16]byte {
	u := uuid.MustParse("1077efec-c0b2-4d02-ace3-3c1e52e2fb4b")
	var arr [16]byte
	copy(arr[:], u[:])
	return arr
}

func toArray16(u uuid.UUID) [16]byte {
	var arr [16]byte
	copy(arr[:], u[:])
	return arr
}

// 🔹 Спрощена генерація Widevine PSSH-даних
// У реальності використовуйте protobuf-бібліотеку
func buildWidevinePsshData(keyID uuid.UUID, contentID string) []byte {
	// Це спрощений приклад — реальний PSSH складніший!
	// Формат: protobuf з полями content_id, key_id, policy...
	return []byte{
		0x08, 0x01,  // field 1 (content_id): varint 1
		0x12, 0x10,  // field 2 (key_id): 16 байт
	}
	// + keyID[:] + ... інші поля
}
```

---

### 🔹 Приклад 3: Валідація DRM-конфігурації перед стрімінгом

```go
func validateDRMConfig(config *DRMConfig) error {
	// 🔹 Перевірка SystemID
	switch config.SystemID.String() {
	case "1077efec-c0b2-4d02-ace3-3c1e52e2fb4b": // Widevine
		// ✅ Підтримується
	case "9a04f079-9840-4286-ab92-e65be0885f95": // PlayReady
		// ✅ Підтримується
	case "94ce86fb-07ff-4f43-adb8-93d2fa968ca2": // FairPlay
		// ✅ Підтримується
	default:
		return fmt.Errorf("unsupported DRM system: %s", config.SystemID)
	}
	
	// 🔹 Перевірка захисту
	if !config.IsProtected {
		log.Printf("ℹ️  Content is not encrypted — no DRM required")
		return nil
	}
	
	// 🔹 Перевірка IV
	if config.IVSize != 0 && config.IVSize != 16 {
		return fmt.Errorf("unsupported IV size: %d (expected 0 or 16)", config.IVSize)
	}
	
	// 🔹 Перевірка PSSH-даних
	if len(config.PSSHData) == 0 {
		return fmt.Errorf("missing PSSH data — cannot obtain license")
	}
	
	log.Printf("✅ DRM config valid: %s, KIDs=%d, IVSize=%d", 
		config.SystemID.String(), len(config.KIDs), config.IVSize)
	
	return nil
}
```

---

## ⚠️ Поширені помилки та як їх уникнути

| Помилка | Симптоми | Рішення |
|---------|----------|---------|
| Неправильний `SystemID` | Плеєр не розпізнає DRM-систему → помилка ліцензії | Завжди використовуйте офіційні UUID: Widevine, PlayReady, FairPlay |
| Відсутні `KIDs` у версії 1 | Плеєр не знає, який ключ запитувати → "license not found" | Для версії 1 завжди заповнюйте `KIDCount` та `KIDs` |
| Неправильний `DefaultPerSampleIVSize` | Декодер не може ініціалізувати AES → артефакти/краш | Використовуйте `16` для AES-CTR/CBCS, `0` тільки з `ConstantIV` |
| Пустий `Data` у `pssh` | DRM-сервер не може обробити запит → 400/403 помилка | Завжди генеруйте валідні PSSH-дані через офіційні бібліотеки |
| Одруківка в тегах `nver=0` | Поля читаються не в тій версії → зсув даних | Перевірте: `KIDCount` та `KIDs` мають `nver=0` (тільки версія 1) |

---

## 📋 Чекліст для вашого проекту

```
[ ] При отриманні захищеного контенту:
    • Шукайте `pssh` бокси на рівні `moov` або `moof`
    • Витягуйте `SystemID` для визначення DRM-системи
    • Зберігайте `Data` для ліцензійного запиту до DRM-сервера

[ ] Для валідації:
    • Перевіряйте `IsProtected == 1` перед спробою декодування
    • Логувайте `DefaultKID` для відладки ліцензійних запитів
    • Відхиляйте невідомі `SystemID`

[ ] При генерації нових сегментів:
    • Використовуйте офіційні бібліотеки для формування PSSH-даних
    • Завжди встановлюйте `IVSize = 16` для максимальної сумісності
    • Додавайте `pssh` на початку `moov` для швидкого доступу плеєра

[ ] Для дебагу:
    • Логуйте UUID у читабельному форматі: uuid.UUID(sysID[:]).String()
    • Виводьте перші 32 байти `Data` для перевірки: log.Printf("PSSH: % x", data[:32])
    • Використовуйте `Stringify()` для виводу `KIDs`: log.Printf("KIDs: %s", Stringify(pssh, ctx))

[ ] Для тестування:
    • Тестуйте з різними DRM-системами: Widevine (Android), FairPlay (iOS)
    • Перевіряйте відтворення на реальних пристроях: Chromecast, Apple TV, Smart TV
    • Симулюйте помилки ліцензії: неправильний KID, прострочена ліцензія
```

---

## 🎯 Інтеграція у ваш CCTV HLS Processor

```
📡 Ваш потік обробки DRM-контенту:
1. Приймаєте зашифрований fMP4-сегмент
   │
   ▼
2. Парсите DRM-конфігурацію:
   • Витягуєте `pssh` для ліцензійного запиту
   • Читаєте `tenc` для параметрів дешифрування
   │
   ▼
3. (Опціонально) Проксі-запит до DRM-сервера:
   • Надсилаєте `PSSHData` + `KID` на ліцензійний сервер
   • Отримуєте ключ дешифрування
   │
   ▼
4. Дешифруєте семпли "на льоту" (якщо потрібно для обробки)
   │
   ▼
5. Передаєте клієнту з оригінальними DRM-боксами
   │
   ▼
6. Клієнт сам запитує ліцензію та відтворює контент ✅
```

> 🎯 **Важливо**: Ваш сервер **не повинен** дешифрувати контент для клієнтів!  
> Ви тільки передаєте `pssh`/`tenc`, а клієнт сам взаємодіє з DRM-сервером.

---

## ❓ Часті питання

**Q: Чи можу я сам дешифрувати контент на сервері?**  
A: Технічно — так, якщо у вас є ключі. Але:
- ⚠️ Це порушує ліцензійні угоди більшості DRM-систем
- ⚠️ Ключі ніколи не повинні покидати безпечне середовище (Hardware Security Module)
- ✅ Правильний підхід: сервер передає `pssh`, клієнт запитує ліцензію напряму

**Q: Як протестувати DRM без реальних ліцензій?**  
```bash
# 1. Використовуйте ClearKey (тестовий режим):
#    SystemID: a2d843c5-3c5e-4d4f-9c1e-8e5e5c5e5c5e
#    Ключі вказуються відкрито у PSSH

# 2. Використовуйте тестові сертифікати:
#    Widevine: https://storage.googleapis.com/wvdrmsample/
#    PlayReady: https://test.playready.microsoft.com/

# 3. Симулюйте відповідь сервера:
#    Створіть мок-сервер, що повертає фіктивні ліцензії
```

**Q: Чи підтримує цей код FairPlay Streaming?**  
A: Так, але з нюансами:
- ✅ `pssh` з FairPlay UUID працює
- ⚠️ FairPlay вимагає додаткові заголовки (`fps-cert`, `fps-license-type`)
- 🔧 Для повної підтримки використовуйте бібліотеку: `github.com/Comcast/gots` або офіційний Apple SDK

**Q: Як перевірити, чи коректний мій PSSH?**  
```bash
# Використайте mp4dump (з Bento4):
mp4dump --format hex segment.m4s | grep -A 20 "pssh"

# Або спеціалізовані інструменти:
# • Widevine: https://github.com/google/edash-packager
# • PlayReady: https://www.playready.com/tools/
```

---

## 🎯 Висновок

> **`pssh` та `tenc` — це ключі до безпечного стрімінгу з DRM**.  
> Вони забезпечують:
> • ✅ Сумісність з усіма основними DRM-системами (Widevine, PlayReady, FairPlay)
> • ✅ Гнучкість: підтримка кількох ключів, різних схем шифрування
> • ✅ Безпеку: ключі ніколи не передаються у відкритому вигляді
> • ✅ Стандартність: повна відповідність ISO/IEC 23001-7 (CENC)

Для вашого **CCTV HLS Processor** це означає:
- 🔐 Захист преміум-контенту від несанкціонованого копіювання
- 🌐 Підтримка всіх основних платформ: Android, iOS, Web, Smart TV
- 🔧 Гнучкість: легко додати нову DRM-систему через новий `SystemID`
- 📊 Моніторинг: логування `KID` та `SystemID` для аналізу ліцензійних запитів

Потребуєте допомоги з інтеграцією конкретної DRM-системи (Widevine/PlayReady/FairPlay) або з генерацією валідних PSSH-даних? Напишіть — покажу готовий код! 🚀🔐