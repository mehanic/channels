# 🔍 Глибоке пояснення: Teletext Character Sets в astisub

Цей код — частина бібліотеки **astisub** (Go), яка працює з субтитрами, зокрема з **DVB/Teletext** форматами. Давайте розберемо детально, що це, навіщо і як використовувати.

---

## 📦 Що це за структура?

### 1. **Teletext Charset — базова концепція**

```go
type teletextCharset struct {
    // масив з 96 байт, де індекс = вхідний байт з потоку,
    // а значення = UTF-8 представлення символу
    []byte, []byte, ... // 96 елементів (0x20-0x7F)
}
```

**Навіщо?** Teletext використовує 7-бітні коди символів (0x20–0x7F), які потрібно перетворити в Unicode (UTF-8) для відображення.

---

### 2. **G0 та G2 набори символів**

| Набір | Призначення | Приклад |
|-------|-------------|---------|
| **G0** | Основний алфавіт (літери, цифри) | `teletextCharsetG0Arabic` |
| **G2** | Додаткові символи (валюті, стрілки, дроби) | `teletextCharsetG2Latin` |

```go
// Приклад: байт 0x41 в G0 Arabic → арабська літера
// Приклад: байт 0x3F в G2 Latin → символ ¿
```

---

### 3. **National Subsets — регіональні варіації**

Один і той самий байт може означати різні символи залежно від країни:

```go
// Байт 0x23 (позиція #3 в таблиці)
teletextNationalSubsetEnglish:    []byte{0xc2, 0xa3} // £ (фунт)
teletextNationalSubsetGerman:     []byte{0x23}       // # (хеш)
teletextNationalSubsetTurkish:    []byte{0xee, 0xa0, 0x80} // ₺ (ліра)
```

Позиції цих символів у G0 визначені масивом:
```go
teletextNationalSubsetCharactersPositionInG0 = [13]uint8{0x03, 0x04, 0x20, ...}
```

---

### 4. **Ієрархія маппінгу: triplet1 → national option → charset**

```go
teletextCharsets = map[uint8]map[uint8]struct {
    g0       *teletextCharset      // основний набір
    g2       *teletextCharset      // додатковий набір  
    national *teletextNationalSubset // регіональні заміни
}
```

**Приклад використання:**
```
triplet1 = 8 (Arabic region)
national option = 7 (Pure Arabic)
→ g0: teletextCharsetG0Arabic
→ g2: teletextCharsetG2Arabic
→ national: nil (не потрібен)
```

---

## 🌍 Підтримувані мови/регіони

```
🇸🇦 Arabic      → triplet1: 8,10
🇷🇺 Cyrillic    → triplet1: 4 (3 варіанти: Option1/2/3)
🇬🇷 Greek       → triplet1: 6, option: 7
🇮🇱 Hebrew      → triplet1: 10, option: 5
🇪🇺 Latin       → triplet1: 0,1,2 (з national subsets для 13+ країн)
🇹🇷 Turkish     → triplet1: 6, option: 3
```

---

## ⚙️ Як це використовується на практиці?

### Крок 1: Визначення кодировки з потоку
```go
// З DVB-потоку отримуємо:
triplet1 := uint8(8)    // Arabic region
nationalOption := uint8(7) // Pure Arabic mode

charset := teletextCharsets[triplet1][nationalOption]
```

### Крок 2: Декодування байта в UTF-8
```go
func decodeByte(b byte, cs *teletextCharset, g2Active bool) string {
    if b < 0x20 || b > 0x7F {
        return "" // невідображуваний символ
    }
    idx := int(b - 0x20)
    
    // Спочатку пробуємо national subset (якщо є)
    if cs.national != nil {
        for posIdx, pos := range teletextNationalSubsetCharactersPositionInG0 {
            if pos == b {
                return string(cs.national[posIdx])
            }
        }
    }
    
    // Потім G0 або G2
    if g2Active {
        return string(cs.g2[idx])
    }
    return string(cs.g0[idx])
}
```

### Крок 3: Інтеграція у ваш pipeline
```go
// У вашому CCTV HLS Processor:
// 1. Отримуєте Teletext-сегмент з DVB-потоку
// 2. Парсите PES-пакети → витягуєте subtitle data
// 3. Для кожного байта:
//    - визначаєте активний charset (G0/G2)
//    - застосовуєте national subset якщо потрібно
//    - конвертуєте в UTF-8
// 4. Формуєте SubtitleMessage для WebSocket
```

---

## 🎯 Практичні сценарії використання

### ✅ Сценарій 1: Мультиязычні субтитри для Al Arabiya
```go
// Якщо потік містить арабські Teletext-субтитри:
// - triplet1=8, national=7 → G0 Arabic
// - decodeByte(0x41) → "ا" (Alif)
// - Результат: коректне відображення арабського тексту
```

### ✅ Сценарій 2: Перехід між мовами в одному потоці
```go
// DVB дозволяє змінювати charset "на льоту" через control codes:
// 0x0F → активувати G2
// 0x0E → повернутися до G0
// 0x1B 0x24 0x40 → змінити national option

// Ваш парсер має відстежувати стан:
type teletextDecoder struct {
    currentCharset struct {
        g0, g2 *teletextCharset
        national *teletextNationalSubset
    }
    g2Active bool
}
```

### ✅ Сценарій 3: Фолбек при невідомій кодировці
```go
// Якщо triplet1 не знайдено в teletextCharsets:
// 1. Спробувати Latin як дефолт
// 2. Логувати попередження з channel_id та seq
// 3. Використовувати placeholder [?] для невідомих символів
```

---

## ⚠️ Важливі нюанси

1. **Byte order**: Teletext використовує **логічний порядок** для арабської/єврейської мови — текст може потребувати візуального реверсу після декодування.

2. **Combining characters**: Деякі символи в G2 (наприклад, діакритика) мають комбінуватися з попереднім символом.

3. **Private Use Area**: Символи типу `0xee 0x98 0x86` — це Unicode PUA (Private Use Area), які можуть відображатися некоректно без спеціального шрифту.

4. **Пам'ять**: Ця таблиця займає ~15 КБ — незначно, але якщо ви робите channel-aware ізоляцію, не дублюйте її для кожного каналу.

---

## 🛠 Як модифікувати/розширити?

```go
// Додати новий national subset (наприклад, для української):
var teletextNationalSubsetUkrainian = &teletextNationalSubset{
    []byte{0x23}, // £ → #
    []byte{0x40}, // @ → Ґ
    // ... інші позиції з teletextNationalSubsetCharactersPositionInG0
}

// Зареєструвати в мапі:
teletextCharsets[4][2] = struct {
    g0: teletextCharsetG0CyrillicOption3, // з українськими літерами
    g2: teletextCharsetG2Cyrillic,
    national: teletextNationalSubsetUkrainian,
}
```

---

## 🔗 Джерела та стандарти

- 📄 [ETSI EN 300 706](https://www.etsi.org/deliver/etsi_en/300700_300799/300706/02.01.01_60/en_300706v020101p.pdf) — Enhanced Teletext specification
- 📄 [ETSI TS 101 154](https://www.etsi.org/deliver/etsi_ts/101100_101199/101154/02.04.01_60/ts_101154v020401p.pdf) — DVB subtitle coding
- 💻 [astisub GitHub](https://github.com/asticode/go-astisub) — бібліотека для роботи з субтитрами

---

> 💡 **Порада для вашого проекту**: Оскільки ви працюєте з HLS-стрімінгом та WebSocket-дистрибуцією субтитрів, рекомендую кешувати розпарсені `teletextCharset` структури на рівні `ChannelConfig`, щоб уникнути повторних пошуків у мапі `teletextCharsets` для кожного сегмента.

Якщо потрібен приклад інтеграції цього декодера у ваш `segmentAssembler` або `SubtitleMessage` pipeline — дайте знати, підготую конкретний код! 🚀