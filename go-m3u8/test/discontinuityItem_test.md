# 🔍 Глибокий розбір тесту: `DiscontinuityItem` для HLS `#EXT-X-DISCONTINUITY`

Цей файл містить **мінімалістичний юніт-тест** для тега `#EXT-X-DISCONTINUITY` — маркера розриву у часовій шкалі або кодуванні медіа-потоку HLS. Розберемо, чому цей "простий" тег насправді критично важливий.

---

## 📦 Що таке `#EXT-X-DISCONTINUITY` і навіщо він потрібен?

### Контекст: розриви у HLS-потоці
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:1000

#EXTINF:4.0,
seg1000.ts
#EXTINF:4.0,
seg1001.ts

#EXT-X-DISCONTINUITY  ← 🚨 РОЗРИВ ТУТ

#EXTINF:4.0,
seg1002.ts  ← Новий кодувальний контекст
#EXTINF:4.0,
seg1003.ts
```

### Призначення тега
| Аспект | Пояснення |
|--------|-----------|
| **Формат** | Просто `#EXT-X-DISCONTINUITY` — **без атрибутів**, без значень |
| **Семантика** | Позначає, що наступний сегмент має інший кодувальний контекст |
| **Вплив на плеєр** | Скидання декодера, синхронізація PTS/DTS, оновлення метаданих |

### 🎯 Критичні сценарії використання
```
🔄 Зміна кодека/бітрейту на льоту:
• seg1001.ts: H.264, 1Mbps
• #EXT-X-DISCONTINUITY
• seg1002.ts: H.265, 2Mbps
→ Плеєр перезавантажує декодер без зупинки відтворення

⏱️ Корекція часової шкали (PTS reset):
• PTS сегментів: 0→4→8→12 секунд
• #EXT-X-DISCONTINUITY
• PTS знову з 0: 0→4→8...
→ Плеєр не "плутає" час, коректно будує таймлайн

📡 Перемикання джерел у live-стрімі:
• Камера 1 → #EXT-X-DISCONTINUITY → Камера 2
• Різні часові мітки, різні параметри кодування
→ Плавний перехід без артефактів

🔐 Зміна ключа шифрування (key rotation):
• Сегменти зашифровані ключом А
• #EXT-X-DISCONTINUITY + #EXT-X-KEY:URI="key_B"
• Сегменти зашифровані ключем B
→ Безпечна ротація без розриву відтворення
```

---

## 🔬 Детальний розбір тесту `TestDiscontinuityItem_Parse`

```go
func TestDiscontinuityItem_Parse(t *testing.T) {
    // 🎯 Конструктор БЕЗ параметрів — тег не має атрибутів!
    di, err := m3u8.NewDiscontinuityItem()
    
    // 🎯 Перевірка відсутності помилок
    assert.Nil(t, err)
    
    // 🎯 Перевірка серіалізації: тег + перенос рядка
    assert.Equal(t, m3u8.DiscontinuityItemTag+"\n", di.String())
    // Очікуваний вивід: "#EXT-X-DISCONTINUITY\n"
}
```

### 🎯 Чому конструктор без параметрів?
```go
// ✅ #EXT-X-DISCONTINUITY — єдиний тип тега, який:
// • Не має атрибутів (нічого після двокрапки)
// • Не приймає вхідних даних для парсингу
// • Просто "маркер" у потоці

// 📋 Порівняння з іншими тегами:
// #EXT-X-KEY:METHOD=AES-128,URI="..."  ← атрибути є → парсинг потрібен
// #EXT-X-DISCONTINUITY                   ← атрибутів немає → конструктор порожній

// ✅ Це відображає специфікацію: тег є само достатнім маркером
```

### 🎯 Чому перевіряємо `+"\n"` у виводі?
```go
// ✅ Специфікація M3U8: кожен тег закінчується переносом рядка
// • "\n" (LF) — стандарт для Unix/Linux/Web
// • Плеєри очікують рядок-орієнтований формат

// ✅ Тест гарантує:
// • di.String() повертає повний рядок, готовий до запису у файл
// • Не потрібно додатково додавати "\n" при серіалізації плейлиста

// 🔄 Приклад використання у плейлисті:
var sb strings.Builder
sb.WriteString(di.String())  // Вже містить "\n" → готово!
// Результат: "#EXT-X-DISCONTINUITY\n"
```

---

## 🏗️ Припустима реалізація `DiscontinuityItem`

```go
// 🎯 Максимально проста структура — немає даних для зберігання
type DiscontinuityItem struct{}

// 🎯 Конструктор: нічого не приймає, нічого не парсить
func NewDiscontinuityItem() (*DiscontinuityItem, error) {
    return &DiscontinuityItem{}, nil  // ✅ Завжди успіх, немає помилок парсингу
}

// 🎯 Серіалізація: повертає фіксований рядок тегу
func (di *DiscontinuityItem) String() string {
    return DiscontinuityItemTag + "\n"  // "#EXT-X-DISCONTINUITY\n"
}

// ✅ Реалізує інтерфейс m3u8.Item для поліморфізму:
// var _ m3u8.Item = (*DiscontinuityItem)(nil)
```

### 🎯 Чому повертає `error`, якщо помилок не може бути?
```go
// ✅ Консистентність API: всі New*Item функції мають підпис (..., error)
// • Дозволяє уніфіковану обробку у фабричних методах
// • Майбутнє розширення: якщо додадуть опціональні атрибути — не зламати API

// 🔄 Приклад уніфікованої фабрики:
func ParseItem(line string) (m3u8.Item, error) {
    switch {
    case strings.HasPrefix(line, m3u8.DiscontinuityItemTag):
        return m3u8.NewDiscontinuityItem()  // ✅ Той самий підпис
        
    case strings.HasPrefix(line, m3u8.SegmentItemTag):
        return m3u8.NewSegmentItem(line)    // ✅ Той самий підпис
        
    // ... інші теги ...
    }
}
```

---

## ⚠️ Критичний аналіз: що можна покращити у тесті

### 1️⃣ Тест надто мінімалістичний
```go
// ❌ Поточний тест перевіряє тільки "щасливий шлях"
// ✅ Додати перевірки на:

// • Чи реалізує тип інтерфейс m3u8.Item (compile-time check)
func TestDiscontinuityItem_ImplementsItem(t *testing.T) {
    var _ m3u8.Item = (*m3u8.DiscontinuityItem)(nil)  // ✅ Компілятор перевірить
}

// • Чи String() завжди повертає однаковий результат (детермінованість)
func TestDiscontinuityItem_String_Deterministic(t *testing.T) {
    di1, _ := m3u8.NewDiscontinuityItem()
    di2, _ := m3u8.NewDiscontinuityItem()
    
    assert.Equal(t, di1.String(), di2.String(), 
        "String() should be deterministic for stateless item")
}

// • Чи можна створювати багато екземплярів без побічних ефектів
func TestDiscontinuityItem_MultipleInstances(t *testing.T) {
    for i := 0; i < 100; i++ {
        di, err := m3u8.NewDiscontinuityItem()
        assert.NoError(t, err)
        assert.Equal(t, "#EXT-X-DISCONTINUITY\n", di.String())
    }
}
```

### 2️⃣ Відсутність інтеграційного тесту з плейлистом
```go
// ✅ Додати тест, що показує використання у реальному плейлисті:
func TestDiscontinuityItem_InPlaylist(t *testing.T) {
    pl := m3u8.NewPlaylist()
    pl.Target = 4
    
    // 🎯 Додавання сегментів до і після розриву
    pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg1.ts"})
    pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg2.ts"})
    
    // 🎯 Вставка маркера розриву
    discontinuity, _ := m3u8.NewDiscontinuityItem()
    pl.AppendItem(discontinuity)
    
    pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg3.ts"})
    pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg4.ts"})
    
    // 🎯 Серіалізація всього плейлиста
    output, err := m3u8.Write(pl)
    assert.NoError(t, err)
    
    // 🎯 Перевірка наявності тегу у правильному місці
    lines := strings.Split(strings.TrimSpace(output), "\n")
    assert.Contains(t, lines, "#EXT-X-DISCONTINUITY")
    
    // 🎯 Перевірка порядку: seg2.ts → DISCONTINUITY → seg3.ts
    seg2Idx := indexOf(lines, "seg2.ts")
    discIdx := indexOf(lines, "#EXT-X-DISCONTINUITY")
    seg3Idx := indexOf(lines, "seg3.ts")
    
    assert.Less(t, seg2Idx, discIdx, "discontinuity should come after seg2")
    assert.Less(t, discIdx, seg3Idx, "discontinuity should come before seg3")
}

// Helper для тесту
func indexOf(slice []string, value string) int {
    for i, v := range slice {
        if strings.Contains(v, value) {
            return i
        }
    }
    return -1
}
```

### 3️⃣ Назва тесту не відображає суть
```go
// ❌ TestDiscontinuityItem_Parse — але парсингу немає!
// ✅ Кращі назви:
func TestDiscontinuityItem_Creation(t *testing.T)           // Акцент на створенні
func TestDiscontinuityItem_Serialization(t *testing.T)      // Акцент на String()
func TestDiscontinuityItem_NoAttributes(t *testing.T)       // Акцент на відсутності атрибутів

// ✅ Або subtests для покриття всіх аспектів:
func TestDiscontinuityItem(t *testing.T) {
    t.Run("Creation/NoError", func(t *testing.T) {
        di, err := m3u8.NewDiscontinuityItem()
        assert.NoError(t, err)
        assert.NotNil(t, di)
    })
    
    t.Run("Serialization/Format", func(t *testing.T) {
        di, _ := m3u8.NewDiscontinuityItem()
        assert.Equal(t, "#EXT-X-DISCONTINUITY\n", di.String())
    })
    
    t.Run("Interface/ImplementsItem", func(t *testing.T) {
        var _ m3u8.Item = (*m3u8.DiscontinuityItem)(nil)
    })
}
```

### 4️⃣ Відсутність тесту на nil-безпеку
```go
// ✅ Перевірити, що методи безпечні для nil-приймача (якщо це підтримується):
func TestDiscontinuityItem_NilReceiver(t *testing.T) {
    var di *m3u8.DiscontinuityItem  // nil
    
    // 🎯 Якщо String() реалізовано з nil-чеком:
    // result := di.String()  // Не повинно панікувати
    
    // ✅ Або документувати, що nil-приймач не підтримується:
    // Це важливо для поліморфних слайсів []m3u8.Item
}
```

---

## 🔗 Інтеграція у ваш CCTV HLS Processor

З урахуванням вашої архітектури з **live-ковзним вікном** та **синхронізацією аудіо/відео**:

### 🎯 Сценарій: вставка `#EXT-X-DISCONTINUITY` при зміні кодека
```go
// У segmentFinalizer при виявленні зміни параметрів кодування:
func (sf *SegmentFinalizer) handleCodecChange(oldCodec, newCodec string, seqNum int) {
    sf.logger.Info("codec change detected", 
        "old", oldCodec, "new", newCodec, "seq", seqNum)
    
    // 🎯 Вставка маркера розриву перед новим сегментом
    discontinuity, _ := m3u8.NewDiscontinuityItem()
    sf.playlist.AppendItem(discontinuity)
    
    // 🎯 Оновлення #EXT-X-DISCONTINUITY-SEQUENCE (лічильник розривів)
    sf.discontinuitySequence++
    // Цей лічильник виводиться у заголовку плейлиста
    
    // 🎯 Оновлення #EXT-X-MAP якщо змінився init-файл
    if sf.initURI != oldInitURI {
        sf.playlist.AppendItem(&m3u8.MapItem{URI: sf.initURI})
    }
}
```

### 🎯 Сценарій: корекція часової шкали (PTS reset)
```go
// У segmentAssembler при виявленні скидання PTS:
func (sa *SegmentAssembler) detectPTSReset(currentPTS, lastPTS int64) bool {
    // 🎯 PTS "відкотився назад" → розрив у часовій шкалі
    if currentPTS < lastPTS {
        sa.logger.Warn("PTS reset detected", 
            "last", lastPTS, "current", currentPTS,
            "action", "inserting discontinuity")
        return true
    }
    // 🎯 Занадто великий стрибок вперед → можливий розрив
    if currentPTS-lastPTS > maxPTSJump {  // Напр. > 60 секунд
        sa.logger.Warn("large PTS jump", 
            "gap", currentPTS-lastPTS,
            "action", "inserting discontinuity")
        return true
    }
    return false
}

// Використання:
if sa.detectPTSReset(newPTS, sa.lastPTS) {
    disc, _ := m3u8.NewDiscontinuityItem()
    sa.playlist.AppendItem(disc)
    sa.ptsBase = newPTS  // Скидання базового відліку
}
```

### 🎯 Сценарій: перемикання джерел у multi-camera CCTV
```go
// У VideoManifestProxy при перемиканні між камерами:
func (p *VideoManifestProxy) switchCameraSource(oldCamID, newCamID string) {
    p.logger.Info("switching camera source", "from", oldCamID, "to", newCamID)
    
    // 🎯 Вставка розриву: нове джерело = новий кодувальний контекст
    disc, _ := m3u8.NewDiscontinuityItem()
    p.playlist.AppendItem(disc)
    
    // 🎯 Оновлення метаданих для нового джерела
    p.currentCamera = newCamID
    p.updateProgramDateTime()  // Синхронізація часу з новим джерелом
    
    // 🎯 Інвалідація кешу клієнтів (опціонально)
    p.broadcastPlaylistUpdate()
}
```

### 🎯 Сценарій: валідація послідовності розривів
```go
// У monitoring.Monitor для виявлення аномалій:
func (m *Monitor) validateDiscontinuitySequence(items []m3u8.Item) error {
    var discCount int
    var lastSeg *m3u8.SegmentItem
    
    for _, item := range items {
        switch v := item.(type) {
        case *m3u8.DiscontinuityItem:
            discCount++
            // 🎯 Після розриву має йти сегмент (не інший розрив!)
            // Це можна перевірити на наступній ітерації
            
        case *m3u8.SegmentItem:
            if lastSeg != nil && discCount == 0 {
                // 🎯 Перевірка монотонності PTS без розривів
                if v.PTS < lastSeg.PTS {
                    return fmt.Errorf("PTS regression without discontinuity: %d < %d", 
                        v.PTS, lastSeg.PTS)
                }
            }
            lastSeg = v
        }
    }
    
    // 🎯 Логування статистики розривів для аналітики
    m.metrics["discontinuity_count"].Add(float64(discCount))
    return nil
}
```

---

## 🧪 Приклад: розширений набір тестів для `DiscontinuityItem`

```go
// ✅ Повний набір тестів з subtests:
func TestDiscontinuityItem(t *testing.T) {
    t.Parallel()
    
    t.Run("Creation/NoError", func(t *testing.T) {
        t.Parallel()
        di, err := m3u8.NewDiscontinuityItem()
        assert.NoError(t, err)
        assert.NotNil(t, di)
    })
    
    t.Run("Serialization/ExactFormat", func(t *testing.T) {
        t.Parallel()
        di, _ := m3u8.NewDiscontinuityItem()
        
        expected := m3u8.DiscontinuityItemTag + "\n"
        actual := di.String()
        
        assert.Equal(t, expected, actual, 
            "String() should return tag with trailing newline")
        assert.Equal(t, "#EXT-X-DISCONTINUITY\n", actual)
    })
    
    t.Run("Serialization/Deterministic", func(t *testing.T) {
        t.Parallel()
        // 🎯 Багаторазові виклики мають повертати однаковий результат
        results := make([]string, 10)
        for i := range results {
            di, _ := m3u8.NewDiscontinuityItem()
            results[i] = di.String()
        }
        
        for i := 1; i < len(results); i++ {
            assert.Equal(t, results[0], results[i], 
                "String() should be deterministic")
        }
    })
    
    t.Run("Interface/ImplementsItem", func(t *testing.T) {
        t.Parallel()
        // 🎯 Compile-time check: тип реалізує інтерфейс
        var _ m3u8.Item = (*m3u8.DiscontinuityItem)(nil)
        
        // 🎯 Runtime check: можна використовувати поліморфно
        var item m3u8.Item = &m3u8.DiscontinuityItem{}
        assert.Implements(t, (*m3u8.Item)(nil), item)
    })
    
    t.Run("Integration/InPlaylist", func(t *testing.T) {
        t.Parallel()
        // 🎯 Тест вставки у реальний плейлист
        pl := m3u8.NewPlaylist()
        pl.Target = 4
        pl.Sequence = 100
        
        pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg100.ts"})
        
        disc, _ := m3u8.NewDiscontinuityItem()
        pl.AppendItem(disc)
        
        pl.AppendItem(&m3u8.SegmentItem{Duration: 4.0, Segment: "seg101.ts"})
        
        output, err := m3u8.Write(pl)
        assert.NoError(t, err)
        
        // 🎯 Перевірка структури виводу
        lines := strings.Split(strings.TrimSpace(output), "\n")
        assert.Contains(t, lines, "#EXT-X-DISCONTINUITY")
        
        // 🎯 Перевірка порядку
        seg100Idx := indexOf(lines, "seg100.ts")
        discIdx := indexOf(lines, "#EXT-X-DISCONTINUITY")
        seg101Idx := indexOf(lines, "seg101.ts")
        
        assert.Less(t, seg100Idx, discIdx)
        assert.Less(t, discIdx, seg101Idx)
    })
}

// Helper для пошуку рядка у слайсі
func indexOf(lines []string, substr string) int {
    for i, line := range lines {
        if strings.Contains(line, substr) {
            return i
        }
    }
    return -1
}
```

---

## 📋 Специфікація HLS (RFC 8216) — вимоги до `#EXT-X-DISCONTINUITY`

```
✅ Формат: просто "#EXT-X-DISCONTINUITY" без атрибутів або значень
✅ Розташування: ПЕРЕД сегментом, до якого застосовується розрив
✅ Семантика розриву (будь-яка з наступних змін вимагає тегу):
   • Зміна часової шкали (PTS/DTS reset або стрибок)
   • Зміна формату файлу (напр. .ts → .mp4)
   • Зміна параметрів кодування (кодек, профіль, рівень, бітрейт)
   • Зміна кількості або типу доріжок (аудіо, субтитри)
   • Зміна ключа шифрування (без окремого #EXT-X-KEY)
✅ #EXT-X-DISCONTINUITY-SEQUENCE:
   • Опціональний лічильник розривів у заголовку плейлиста
   • Зростає на 1 при кожному новому #EXT-X-DISCONTINUITY
   • Допомагає клієнтам відстежувати розриви при оновленні live-плейлиста
✅ Кілька розривів поспіль: дозволено, але рідко має сенс
✅ Розрив на початку плейлиста: допустимо (напр. після перезапуску стріму)
✅ Клієнти МАЮТЬ підтримувати обробку розривів для коректного відтворення
```

---

## 🎯 Висновок

Цей тест — **мінімальна, але достатня перевірка** для найпростішого тегу HLS:

✅ Перевірка конструктора без параметрів  
✅ Валідація точного формату серіалізації (`#EXT-X-DISCONTINUITY\n`)  
✅ Консистентність з інтерфейсом `m3u8.Item`

**Для вашого проекту — критичні рекомендації**:

1. ✅ Додати `t.Parallel()` для прискорення прогону тестів
2. ✅ Розширити тест на інтеграцію з `Playlist` (порядок тегів)
3. ✅ Додати `assert.Implements(t, (*m3u8.Item)(nil), di)` для явної перевірки інтерфейсу
4. ✅ Перейменувати тест на більш описовий (`TestDiscontinuityItem_CreationAndSerialization`)
5. ✅ Документувати, чому `error` повертається, хоча помилок не може бути (API-консистентність)

**Приклад інтеграції для CCTV live-стріму**:
```go
// У segmentFinalizer: автоматична вставка розривів при аномаліях
func (sf *SegmentFinalizer) processSegment(seg *EncodedSegment) error {
    // 🎯 Виявлення умов для розриву
    if sf.needsDiscontinuity(seg) {
        disc, _ := m3u8.NewDiscontinuityItem()
        sf.playlist.AppendItem(disc)
        sf.discontinuitySequence++
        
        sf.logger.Info("inserted discontinuity", 
            "reason", sf.lastDiscontinuityReason,
            "seq", sf.discontinuitySequence)
    }
    
    // 🎯 Додавання самого сегмента
    sf.playlist.AppendItem(&m3u8.SegmentItem{
        Duration: seg.Duration,
        Segment:  seg.URI,
        PTS:      seg.PTS,
    })
    
    return nil
}

func (sf *SegmentFinalizer) needsDiscontinuity(seg *EncodedSegment) bool {
    // 🎯 Умови для вставки розриву:
    return sf.lastCodec != seg.Codec ||                    // Зміна кодека
           sf.ptsResetDetected(seg.PTS) ||                 // Скидання PTS
           sf.timeGapTooLarge(seg.StartTime) ||            // Великий часовий розрив
           sf.encryptionKeyChanged(seg.KeyID)              // Зміна ключа шифрування
}
```

Потрібно допомогти з:
- 🧠 Реалізацією детектора умов для `#EXT-X-DISCONTINUITY` у вашому pipeline?
- 🔗 Інтеграцією `#EXT-X-DISCONTINUITY-SEQUENCE` для відстеження розривів у live-плейлистах?
- 🧪 Написанням інтеграційних тестів для сценаріїв з перемиканням джерел/кодеків?

Чекаю на ваші питання! 🛠️🔀