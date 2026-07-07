# 📦 Глибокий розбір: `fmp4.makeFragment()` та `marshalFragment()` — Побудова fMP4 фрагментів

Цей файл — **реалізація ключових функцій для створення fMP4 фрагментів**: `makeFragment()` для побудови метаданих треку та `marshalFragment()` для серіалізації повного фрагменту з медіа-даними. Він обробляє таймінги, прапорці семплів, оптимізацію за замовчуванням та побудову бінарного контенту.

---

## 🗺️ Архітектурна схема фрагментації

```
┌────────────────────────────────────────┐
│ 📦 fmp4 — Fragment Construction       │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові функції:                    │
│  • makeFragment() — побудова TrackFrag │
│  • marshalFragment() — серіалізація   │
│  • fragmentWithData — проміжна структура│
│                                         │
│  🔄 Потік даних:                        │
│  av.Packet[] → makeFragment()          │
│  → TrackFrag + packets                 │
│  → marshalFragment()                   │
│  → fragment.Fragment (binary)         │
│                                         │
│  📡 Оптимізації:                        │
│  • Default fields для економії місця  │
│  • 64-бітні таймінги для точності     │
│  • Composition time для B-frames      │
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. fragmentWithData — проміжна структура

### 🔧 Структура та призначення:

```go
type fragmentWithData struct {
    trackFrag   *fmp4io.TrackFrag  // ⭐ метадані фрагменту треку (moof частина)
    packets     []av.Packet        // ⭐ сирі медіа-пакети для запису у mdat
    independent bool               // ⭐ чи можна почати відтворення з цього фрагменту
}
```

### 🔍 Призначення полів:

| Поле | Тип | Призначення | Приклад |
|------|-----|-------------|---------|
| `trackFrag` | `*fmp4io.TrackFrag` | **Критично**: метадані фрагменту для moof атому (таймінги, розміри, прапорці) | `TrackFrag{Header: {...}, Run: {...}}` |
| `packets` | `[]av.Packet` | **Критично**: сирі медіа-дані для запису у mdat атом | `[]av.Packet{{Data: []byte{...}}, ...}` |
| `independent` | `bool` | **Критично**: чи фрагмент починається з ключового кадру (можна почати відтворення) | `true` = IDR frame для H.264 |

### ✅ Ваш use-case**: агрегація даних треку

```
makeFragment() повертає fragmentWithData для кожного треку:
• trackFrag містить структуровані метадані (для moof)
• packets містить сирі дані (для mdat)
• independent визначає чи можна seek до цього фрагменту

marshalFragment() об'єднує дані з усіх треків:
• Будує moof атом з TrackFrag масиву
• Розраховує зміщення даних для кожного треку
• Записує mdat атом з об'єднаними пакетами
• Повертає готовий binary фрагмент для відправки
```

---

## 🔑 2. makeFragment() — побудова TrackFrag з пакетів

### 🔧 Основна логіка:

```go
func (f *TrackFragmenter) makeFragment() fragmentWithData {
    // 1. Перевірка мінімальної кількості пакетів
    if len(f.pending) < 2 {
        return fragmentWithData{}  // недостатньо даних
    }
    
    // 2. Розрахунок базових таймінгів
    entryCount := len(f.pending) - 1  // кількість семплів у цьому фрагменті
    startTime := f.pending[0].Time
    startDTS := timescale.ToScale(startTime, f.timeScale)  // конвертація у ticks
    
    // 3. Ініціалізація прапорців за замовчуванням
    defaultFlags := fmp4io.SampleNoDependencies
    if f.codecData.Type().IsVideo() {
        defaultFlags = fmp4io.SampleNonKeyframe  // відео: за замовчуванням не ключовий
    }
    
    // 4. Створення TrackFrag структури
    track := &fmp4io.TrackFrag{
        Header: &fmp4io.TrackFragHeader{
            Flags:   fmp4io.TrackFragDefaultBaseIsMOOF,  // offset відносно moof
            TrackID: f.trackID,
        },
        DecodeTime: &fmp4io.TrackFragDecodeTime{
            Version: 1,  // 64-бітний час для точності
            Time:    startDTS,
        },
        Run: &fmp4io.TrackFragRun{
            Flags:   fmp4io.TrackRunDataOffset,  // присутній DataOffset
            Entries: make([]fmp4io.TrackFragRunEntry, entryCount),
        },
    }
    
    // 5. Заповнення Entries та оптимізація default полів
    curDTS := startDTS
    for i, pkt := range f.pending[:entryCount] {
        // Розрахунок duration як різниця DTS поточного та наступного семплу
        nextTime := f.pending[i+1].Time
        nextDTS := timescale.ToScale(nextTime, f.timeScale)
        
        entry := fmp4io.TrackFragRunEntry{
            Duration: uint32(nextDTS - curDTS),
            Flags:    defaultFlags,
            Size:     uint32(len(pkt.Data)),
        }
        
        // Оновлення прапорців для ключових кадрів
        if pkt.IsKeyFrame {
            entry.Flags = fmp4io.SampleNoDependencies
        }
        
        // Оптимізація: використання default полів якщо значення однакові
        if i == 0 {
            // Перший семпл: встановлення потенційних default значень
            track.Header.DefaultDuration = entry.Duration
            track.Header.DefaultSize = entry.Size
            track.Header.DefaultFlags = entry.Flags
            track.Run.FirstSampleFlags = entry.Flags
        } else {
            // Перевірка чи значення відрізняються від default
            if entry.Duration != track.Header.DefaultDuration {
                track.Header.DefaultDuration = 0  // вимкнути default duration
            }
            if entry.Size != track.Header.DefaultSize {
                track.Header.DefaultSize = 0  // вимкнути default size
            }
            // Default flags беруться з другого семплу (перший може бути особливим)
            if i == 1 {
                track.Header.DefaultFlags = entry.Flags
            } else if entry.Flags != track.Header.DefaultFlags {
                track.Header.DefaultFlags = 0  // вимкнути default flags
            }
        }
        
        // Обробка Composition Time Offset (для B-frames)
        if pkt.CompositionTime != 0 {
            track.Run.Flags |= fmp4io.TrackRunSampleCTS  // увімкнути CTS у прапорцях
            relCTS := timescale.Relative(pkt.CompositionTime, f.timeScale)
            if relCTS < 0 {
                track.Run.Version = 1  // від'ємний CTS вимагає Version 1
            }
            entry.CTS = relCTS
        }
        
        curDTS = nextDTS
        track.Run.Entries[i] = entry
    }
    
    // 6. Встановлення прапорців для default полів
    if track.Header.DefaultSize != 0 {
        track.Header.Flags |= fmp4io.TrackFragDefaultSize
    } else {
        track.Run.Flags |= fmp4io.TrackRunSampleSize  // кожен семпл має свій розмір
    }
    // Аналогічно для Duration та Flags...
    
    // 7. Підготовка результату
    d := fragmentWithData{
        trackFrag:   track,
        packets:     f.pending[:entryCount],  // пакети для цього фрагменту
        independent: track.Run.FirstSampleFlags&fmp4io.SampleNoDependencies != 0,
    }
    
    // 8. Залишення останнього пакету для наступного фрагменту
    f.pending = []av.Packet{f.pending[entryCount]}
    
    return d
}
```

### 🔍 Чому `entryCount = len(f.pending) - 1`?

```
Це дозволяє розрахувати duration для останнього семплу:

• duration[i] = DTS[i+1] - DTS[i]
• Для останнього семплу у фрагменті немає "наступного" семплу
• Тому ми беремо перші N-1 пакетів для цього фрагменту
• Останній пакет залишається у f.pending для наступного фрагменту

Приклад:
  pending = [pkt0, pkt1, pkt2, pkt3]  // 4 пакети
  entryCount = 3
  
  Фрагмент містить:
  • Семпл 0: duration = DTS[1] - DTS[0]
  • Семпл 1: duration = DTS[2] - DTS[1]
  • Семпл 2: duration = DTS[3] - DTS[2]  ← використовує pkt3 для розрахунку
  
  Після фрагментації:
  • f.pending = [pkt3]  ← залишається для наступного фрагменту
```

### 🔍 Оптимізація default полів:

```
MP4 формат дозволяє вказувати "default" значення для всіх семплів:
• Це економить місце у файлі (не потрібно повторювати однакові значення)
• Прапорці у TrackFragHeader визначають які поля використовують default

Логіка оптимізації у коді:
1. Спочатку припускаємо що всі семпли однакові (беремо значення з першого)
2. Для кожного наступного семплу перевіряємо чи значення збігається
3. Якщо хоча б одне значення відрізняється → вимикаємо default для цього поля
4. Прапорці оновлюються відповідно:
   • DefaultSize != 0 → TrackFragDefaultFlags |= TrackFragDefaultSize
   • DefaultSize == 0 → TrackRunFlags |= TrackRunSampleSize

Приклад економії:
• 100 семплів з однаковим розміром 1024 байти:
  - Без оптимізації: 100 × 4 байти = 400 байт для розмірів
  - З оптимізацією: 4 байти для DefaultSize + 0 для семплів = 4 байти
  - Економія: 99% для цього поля!
```

### ⚠️ Критична проблема: обробка від'ємного CTS

```
У поточному коді:
    if relCTS < 0 {
        track.Run.Version = 1  // від'ємний CTS вимагає Version 1
    }

Проблема:
• Version 1 trun вимагає signed int32 для CTS
• Але entry.CTS завжди int32, тому присвоєння коректне
• Однак, якщо Version залишається 0 при від'ємному CTS → некоректне декодування

✅ Це вже оброблено коректно у коді, але варто додати коментар:
    // Від'ємний CTS вимагає trun Version 1 (signed int32 замість uint32)
    // Див. ISO/IEC 14496-12 Section 8.8.8
```

### ✅ Ваш use-case**: розрахунок таймінгів для відео з B-frames

```go
// Приклад для H.264 з B-frames:
// Порядок декодування: I0, P3, B1, B2, P6, B4, B5...
// Порядок відтворення: I0, B1, B2, P3, B4, B5, P6...

// Для B1 (декодується після P3, але відтворюється після I0):
// • DTS = 1 (час декодування)
// • PTS = 1 (час відтворення) 
// • CTS = PTS - DTS = 0

// Для складніших випадків з затримкою:
// • B-frame може мати CTS = -500ms (відтворення раніше декодування)
// • timescale.Relative(-500ms, 90000) = -45000 ticks
// • entry.CTS = -45000, track.Run.Version = 1

// Перевірка коректності:
func ValidateCTSEntries(trun *fmp4io.TrackFragRun) error {
    if trun.Version == 0 {
        // Version 0: CTS має бути unsigned (не від'ємним)
        for i, entry := range trun.Entries {
            if entry.CTS < 0 {
                return fmt.Errorf("entry %d: negative CTS %d with trun version 0", i, entry.CTS)
            }
        }
    }
    return nil
}
```

---

## 🔑 3. marshalFragment() — серіалізація повного фрагменту

### 🔧 Основна логіка:

```go
func marshalFragment(tracks []fragmentWithData, seqNum uint32, initial bool) fragment.Fragment {
    // 1. Створення MovieFrag (moof) атому
    moof := &fmp4io.MovieFrag{
        Header: &fmp4io.MovieFragHeader{
            Seqnum: seqNum,  // послідовний номер фрагменту
        },
        Tracks: make([]*fmp4io.TrackFrag, len(tracks)),
    }
    
    // 2. Заповнення TrackFrag масиву та розрахунок independent прапорця
    independent := true
    for i, track := range tracks {
        moof.Tracks[i] = track.trackFrag
        if !track.independent {
            independent = false  // фрагмент незалежний тільки якщо всі треки незалежні
        }
    }
    
    // 3. Розрахунок зміщень даних відносно початку moof
    dataBase := moof.Len() + 8  // розмір moof + заголовок mdat (size+tag)
    dataOffset := dataBase
    for i, track := range tracks {
        moof.Tracks[i].Run.DataOffset = uint32(dataOffset)  // зміщення для цього треку
        for _, pkt := range track.packets {
            dataOffset += len(pkt.Data)  // додавання розміру даних треку
        }
    }
    
    // 4. Підготовка буфера для серіалізації
    var shdrSize int
    if initial {
        // Додавання segment header (styp) для першого фрагменту сегменту
        shdrOnce.Do(func() {
            shdr = FragmentHeader()  // генерація styp атому (once)
        })
        shdrSize = len(shdr)
    }
    
    // Алокація буфера: shdr + moof + mdat header + медіа-дані
    b := make([]byte, shdrSize+dataBase, shdrSize+dataOffset)
    var n int
    
    // 5. Запис segment header якщо потрібно
    if initial {
        copy(b, shdr)
        n = len(shdr)
    }
    
    // 6. Серіалізація moof атому
    n += moof.Marshal(b[n:])
    
    // 7. Запис заголовку mdat атому
    pio.PutU32BE(b[n:], uint32(dataOffset-dataBase+8))  // розмір mdat content + header
    pio.PutU32BE(b[n+4:], uint32(fmp4io.MDAT))          // tag 'mdat'
    
    // 8. Запис медіа-даних у mdat
    for i, track := range tracks {
        // Оновлення DataOffset після запису moof (може змінитися через змінну довжину)
        moof.Tracks[i].Run.DataOffset = uint32(dataOffset)
        for _, pkt := range track.packets {
            b = append(b, pkt.Data...)  // додавання сирих даних пакету
        }
    }
    
    // 9. Повернення готового фрагменту
    return fragment.Fragment{
        Bytes:       b,           // весь буфер (може мати cap > len)
        Length:      len(b),      // фактична довжина валідних даних
        Independent: independent, // чи можна почати відтворення з цього фрагменту
    }
}
```

### 🔍 Розрахунок DataOffset:

```
DataOffset — це зміщення початку медіа-даних треку відносно початку moof атому.

Формула:
  DataOffset = moof.Len() + 8 + sum(previous_tracks_data_sizes)

Де:
  • moof.Len() — розмір серіалізованого moof атому
  • 8 — розмір заголовку mdat атому (size:4 + tag:4)
  • sum(previous_tracks_data_sizes) — сума розмірів даних попередніх треків

Приклад для двох треків (відео + аудіо):
  moof.Len() = 200 байт
  dataBase = 200 + 8 = 208
  
  Відео трек:
  • DataOffset = 208
  • Дані: 100000 байт
  • Наступне зміщення: 208 + 100000 = 100208
  
  Аудіо трек:
  • DataOffset = 100208
  • Дані: 50000 байт
  • Кінець даних: 100208 + 50000 = 150208
  
  Загальний розмір фрагменту:
  • styp (якщо initial): 16 байт
  • moof: 200 байт
  • mdat header: 8 байт
  • медіа-дані: 150000 байт
  • Разом: 16 + 200 + 8 + 150000 = 150224 байт
```

### ⚠️ Критична проблема: повторний запис DataOffset

```
У коді є два місця де встановлюється DataOffset:

1. Перед серіалізацією moof:
   moof.Tracks[i].Run.DataOffset = uint32(dataOffset)

2. Після серіалізації moof, перед записом даних:
   moof.Tracks[i].Run.DataOffset = uint32(dataOffset)  // знову!

Проблема:
• moof.Marshal() може змінити розмір moof через variable-length поля
• Тому DataOffset розрахований до Marshal() може бути некоректним
• Друге присвоєння виправляє це, але це неочевидно

✅ Виправлення: додати коментар або винести розрахунок після Marshal():
    // Розрахунок DataOffset після серіалізації moof, 
    // оскільки moof.Len() може змінитися через variable-length encoding
    dataBase = moof.Len() + 8
    dataOffset = dataBase
    for i, track := range tracks {
        moof.Tracks[i].Run.DataOffset = uint32(dataOffset)
        for _, pkt := range track.packets {
            dataOffset += len(pkt.Data)
        }
    }
```

### ✅ Ваш use-case**: відправка фрагменту через HTTP chunked encoding

```go
// SendFragmentHTTP — відправка фрагменту через HTTP з chunked encoding
func SendFragmentHTTP(w http.ResponseWriter, frag fragment.Fragment) error {
    // Встановлення заголовків для chunked streaming
    w.Header().Set("Content-Type", "video/mp4")
    w.Header().Set("Transfer-Encoding", "chunked")
    w.Header().Set("Cache-Control", "no-cache")
    
    // Відправка даних частинами (chunked)
    // Важливо: використовувати тільки валідну частину буфера
    _, err := w.Write(frag.Bytes[:frag.Length])
    if err != nil {
        return fmt.Errorf("write fragment: %w", err)
    }
    
    // Flush для негайної відправки клієнту (low-latency)
    if flusher, ok := w.(http.Flusher); ok {
        flusher.Flush()
    }
    
    return nil
}

// Використання у handler:
func fragmentHandler(w http.ResponseWriter, r *http.Request) {
    // ... отримання фрагменту з фрагментатора ...
    frag, err := fragmenter.Fragment()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    if frag.Length == 0 {
        return  // немає даних
    }
    
    if err := SendFragmentHTTP(w, frag); err != nil {
        log.Printf("error sending fragment: %v", err)
    }
}
```

---

## 🔑 4. Оптимізації та edge cases

### 🔍 Обробка першого семплу з особливими прапорцями:

```
У MP4 форматі перший семпл у фрагменті може мати окремі прапорці:
• TrackRunFirstSampleFlags прапорець вмикає окреме поле FirstSampleFlags
• Це дозволяє мати інший прапорець для першого семплу при однакових інших

Логіка у коді:
    if i == 0 {
        track.Run.FirstSampleFlags = entry.Flags  // зберегти прапорці першого семплу
    }
    // ...
    if track.Header.DefaultFlags != 0 {
        track.Header.Flags |= fmp4io.TrackFragDefaultFlags
        if track.Run.FirstSampleFlags != track.Header.DefaultFlags {
            // Перший семпл відрізняється → увімкнути окреме поле
            track.Run.Flags |= fmp4io.TrackRunFirstSampleFlags
        }
    }

Приклад використання:
• Фрагмент починається з ключового кадру (IDR), але решта — P-frames
• FirstSampleFlags = SampleNoDependencies (ключовий)
• DefaultFlags = SampleNonKeyframe (не ключові)
• Економія: не потрібно вказувати прапорці для кожного з 100+ семплів
```

### 🔍 Обробка Composition Time Offset для B-frames:

```
CTS (Composition Time Offset) = PTS - DTS

Для відео з B-frames порядок декодування відрізняється від порядку відтворення:
• DTS (Decoding Time Stamp): коли семпл має бути декодований
• PTS (Presentation Time Stamp): коли семпл має бути відтворений
• CTS = PTS - DTS: зміщення між декодуванням та відтворенням

Приклад:
  Порядок декодування: I0, P3, B1, B2, P6...
  Порядок відтворення: I0, B1, B2, P3, B4, B5, P6...
  
  Для B1:
  • DTS = 3 (декодується після P3)
  • PTS = 1 (відтворюється після I0)
  • CTS = PTS - DTS = 1 - 3 = -2 ticks
  
  timescale.Relative(-2 ticks, 90000) = -2 (якщо ticks вже у правильній шкалі)
  • entry.CTS = -2
  • track.Run.Version = 1 (бо від'ємний CTS)

Важливо:
• Version 0 trun: CTS = uint32 (не може бути від'ємним)
• Version 1 trun: CTS = int32 (підтримує від'ємні значення)
```

### ✅ Ваш use-case**: валідація фрагменту перед відправкою

```go
// ValidateFragment — перевірка коректності фрагменту перед відправкою
func ValidateFragment(frag fragment.Fragment, tracks []fragmentWithData) error {
    // 1. Перевірка базових полів
    if frag.Length == 0 {
        return fmt.Errorf("empty fragment")
    }
    if frag.Length > len(frag.Bytes) {
        return fmt.Errorf("length %d > buffer size %d", frag.Length, len(frag.Bytes))
    }
    
    // 2. Перевірка незалежності
    if frag.Independent {
        // Фрагмент має починатися з ключового кадру
        hasKeyFrame := false
        for _, track := range tracks {
            if track.independent {
                hasKeyFrame = true
                break
            }
        }
        if !hasKeyFrame {
            return fmt.Errorf("fragment marked independent but no key frame found")
        }
    }
    
    // 3. Перевірка моожливості парсингу (спрощено)
    if len(frag.Bytes) < 16 {
        return fmt.Errorf("fragment too short for valid MP4: %d bytes", len(frag.Bytes))
    }
    
    // Перевірка наявності moof та mdat тегів
    hasMoof := bytes.Contains(frag.Bytes[:frag.Length], []byte("moof"))
    hasMdat := bytes.Contains(frag.Bytes[:frag.Length], []byte("mdat"))
    if !hasMoof || !hasMdat {
        return fmt.Errorf("missing required atoms: moof=%v, mdat=%v", hasMoof, hasMdat)
    }
    
    return nil
}

// Використання:
frag := marshalFragment(tracks, seqNum, initial)
if err := ValidateFragment(frag, tracks); err != nil {
    log.Printf("warning: invalid fragment: %v", err)
    // Можна спробувати відновитися або повернути помилку
}
```

---

## 🔄 Інтеграція у ваш pipeline: повний приклад

### 🔧 Приклад: Low-latency streaming з WebSocket

```go
// LowLatencyStreamer — streaming з мінімальною затримкою через WebSocket
type LowLatencyStreamer struct {
    fragmenter *fmp4.MovieFragmenter
    conn       *websocket.Conn
    mu         sync.Mutex
    seqNum     uint32
}

func (s *LowLatencyStreamer) Stream(ctx context.Context, demuxer av.Demuxer) error {
    // 1. Відправка init segment
    _, _, initBytes := s.fragmenter.MovieHeader()
    if err := s.sendBinary(initBytes); err != nil {
        return fmt.Errorf("send init: %w", err)
    }
    
    // 2. Основний цикл з низькою затримкою
    flushInterval := 50 * time.Millisecond  // частіша генерація фрагментів
    ticker := time.NewTicker(flushInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
            
        case <-ticker.C:
            // Примусова генерація фрагменту навіть з малою кількістю даних
            frag, err := s.fragmenter.Fragment()
            if err != nil {
                return fmt.Errorf("generate fragment: %w", err)
            }
            if frag.Length == 0 {
                continue  // немає достатньо даних
            }
            
            // Валідація перед відправкою
            if err := ValidateFragment(frag, nil); err != nil {
                log.Printf("warning: skipping invalid fragment: %v", err)
                continue
            }
            
            // Відправка через WebSocket
            if err := s.sendFragment(frag); err != nil {
                return fmt.Errorf("send fragment: %w", err)
            }
            
            // Логування метрик
            log.Printf("Sent fragment %d: %d bytes, %v duration, independent=%v", 
                s.seqNum, frag.Length, frag.Duration, frag.Independent)
            s.seqNum++
            
        default:
            // Неблокуюче читання пакетів
            pkt, err := demuxer.ReadPacket()
            if err == io.EOF {
                return nil
            }
            if err != nil && err != io.ErrNoData {
                return fmt.Errorf("read packet: %w", err)
            }
            if err == nil {
                if err := s.fragmenter.WritePacket(pkt); err != nil {
                    return fmt.Errorf("write packet: %w", err)
                }
            }
        }
    }
}

func (s *LowLatencyStreamer) sendFragment(frag fragment.Fragment) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // Формат повідомлення: [4-byte length][fragment data]
    header := make([]byte, 4)
    binary.BigEndian.PutUint32(header, uint32(frag.Length))
    
    message := append(header, frag.Bytes[:frag.Length]...)
    return s.conn.WriteMessage(websocket.BinaryMessage, message)
}

func (s *LowLatencyStreamer) sendBinary(data []byte) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.conn.WriteMessage(websocket.BinaryMessage, data)
}
```

### 🔧 Приклад: Adaptive bitrate streaming з кількома якостями

```go
// AdaptiveBitrateStreamer — streaming з декількома бітрейтами
type AdaptiveBitrateStreamer struct {
    fragmenters map[string]*fmp4.MovieFragmenter  // quality -> fragmenter
    demuxer     av.Demuxer
    clients     map[string]*ClientState  // clientID -> state
}

type ClientState struct {
    Quality   string  // поточна якість ("low", "medium", "high")
    LastSeq   uint32  // останній отриманий seqNum
    Conn      *websocket.Conn
}

func (s *AdaptiveBitrateStreamer) HandleClient(ctx context.Context, clientID string, conn *websocket.Conn) {
    // 1. Ініціалізація клієнта з якістю за замовчуванням
    s.clients[clientID] = &ClientState{
        Quality: "medium",
        Conn:    conn,
    }
    
    // 2. Відправка init segment для обраної якості
    fragmenter := s.fragmenters["medium"]
    _, _, initBytes := fragmenter.MovieHeader()
    conn.WriteMessage(websocket.BinaryMessage, initBytes)
    
    // 3. Основний цикл відправки фрагментів
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            state := s.clients[clientID]
            if state == nil {
                return
            }
            
            fragmenter := s.fragmenters[state.Quality]
            frag, err := fragmenter.Fragment()
            if err != nil || frag.Length == 0 {
                continue
            }
            
            // Перевірка чи клієнт не відстав (пропуск старих фрагментів)
            if fragSeq := fragmenter.seqNum - 1; fragSeq <= state.LastSeq {
                continue  // клієнт вже має цей фрагмент
            }
            
            // Відправка фрагменту
            if err := s.sendFragment(conn, frag); err != nil {
                log.Printf("client %s disconnected: %v", clientID, err)
                delete(s.clients, clientID)
                return
            }
            
            state.LastSeq = fragmenter.seqNum - 1
            
            // Адаптація якості на основі затримки (спрощено)
            if latency := calculateLatency(conn); latency > 2*time.Second {
                state.Quality = "low"  // зменшити якість при великій затримці
            } else if latency < 500*time.Millisecond {
                state.Quality = "high"  // збільшити якість при малій затримці
            }
        }
    }
}

func calculateLatency(conn *websocket.Conn) time.Duration {
    // Спрощена оцінка затримки через ping/pong
    start := time.Now()
    conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(5*time.Second))
    // ... очікування pong ...
    return time.Since(start)
}
```

---

## 🐞 Поширені проблеми та рішення

| Проблема | Симптом | Рішення |
|----------|---------|---------|
| **Невірний DataOffset після moof.Marshal()** | Клієнт не може знайти дані у mdat | Перерахуйте DataOffset після серіалізації moof, оскільки moof.Len() може змінитися |
| **Від'ємний CTS з Version 0 trun** | Некоректне відтворення B-frames | Переконайтеся що `track.Run.Version = 1` якщо `relCTS < 0` |
| **Некоректне використання default полів** | Збільшений розмір фрагменту або помилки парсингу | Перевірте логіку вимкнення default полів при першому відмінному значенні |
| **Переповнення uint32 для Duration/Size** | Помилки для дуже великих семплів | Додайте перевірку `if size > math.MaxUint32` перед конвертацією |
| **Необроблений останній пакет у pending** | Втрата даних при завершенні потоку | Після основного циклу обробіть залишкові пакети у f.pending |

---

## ⚡ Оптимізації для high-performance фрагментації

### 1. Reuse буферів для серіалізації:

```go
var fragmentBufferPool = sync.Pool{
    New: func() interface{} {
        // Типовий розмір фрагменту: 1-4 MB
        buf := make([]byte, 0, 4*1024*1024)
        return &buf
    },
}

func getFragmentBuffer() *[]byte {
    return fragmentBufferPool.Get().(*[]byte)
}

func putFragmentBuffer(b *[]byte) {
    *b = (*b)[:0]  // скидання без звільнення
    fragmentBufferPool.Put(b)
}

// Використання у marshalFragment():
buf := getFragmentBuffer()
defer putFragmentBuffer(buf)
// ... запис даних у buf ...
return fragment.Fragment{
    Bytes: *buf,
    Length: len(*buf),
    // ...
}, nil
```

### 2. Попередній розрахунок розміру буфера:

```go
// Розрахунок необхідного розміру буфера перед алокацією
func calculateFragmentSize(tracks []fragmentWithData, initial bool) int {
    size := 0
    if initial {
        size += len(shdr)  // styp атом
    }
    
    // Оцінка розміру moof (спрощено)
    size += 100  // базовий розмір moof header
    for _, track := range tracks {
        size += 50  // базовий розмір TrackFrag
        size += len(track.packets) * 12  // кожен entry ~12 байт
    }
    
    size += 8  // mdat header
    
    // Додавання розміру медіа-даних
    for _, track := range tracks {
        for _, pkt := range track.packets {
            size += len(pkt.Data)
        }
    }
    
    return size
}

// Використання:
estimatedSize := calculateFragmentSize(tracks, initial)
b := make([]byte, shdrSize+dataBase, estimatedSize)  // cap = estimatedSize для уникнення realloc
```

### 3. Моніторинг продуктивності фрагментації:

```go
type FragmentationMetrics struct {
    FragmentsGenerated prometheus.CounterVec
    FragmentLatency    prometheus.HistogramVec
    FragmentSizes      prometheus.HistogramVec
    OptimizationRatio  prometheus.GaugeVec  // співвідношення default/individual полів
}

func (m *FragmentationMetrics) RecordFragment(duration time.Duration, size int, defaultFields int, totalFields int) {
    m.FragmentsGenerated.Inc()
    m.FragmentLatency.Observe(duration.Seconds())
    m.FragmentSizes.Observe(float64(size))
    if totalFields > 0 {
        ratio := float64(defaultFields) / float64(totalFields)
        m.OptimizationRatio.Observe(ratio)
    }
}
```

---

## 📋 Чек-лист безпечного використання makeFragment/marshalFragment

```go
// ✅ 1. Перевірка мінімальної кількості пакетів перед фрагментацією
if len(f.pending) < 2 {
    return fragmentWithData{}  // недостатньо даних для розрахунку duration
}

// ✅ 2. Коректна обробка від'ємного CTS
if pkt.CompositionTime != 0 {
    relCTS := timescale.Relative(pkt.CompositionTime, f.timeScale)
    if relCTS < 0 {
        track.Run.Version = 1  // обов'язково для від'ємних значень
    }
    entry.CTS = relCTS
}

// ✅ 3. Перерахунок DataOffset після moof.Marshal()
// (оскільки moof.Len() може змінитися через variable-length encoding)
dataBase = moof.Len() + 8
// ... перерахунок DataOffset для кожного треку ...

// ✅ 4. Використання Bytes[:Length] для доступу до даних
data := frag.Bytes[:frag.Length]  // ✅ правильно
// data := frag.Bytes             // ❌ може включати неініціалізовану пам'ять

// ✅ 5. Валідація independent прапорця
if frag.Independent {
    // Перевірка що перший семпл дійсно ключовий
    hasKeyFrame := false
    for _, track := range tracks {
        if track.independent {
            hasKeyFrame = true
            break
        }
    }
    if !hasKeyFrame {
        log.Printf("warning: fragment marked independent but no key frame")
    }
}

// ✅ 6. Логування з контекстом для дебагу
log.Printf("Generated fragment: seq=%d, tracks=%d, size=%d, duration=%v, independent=%v", 
    seqNum, len(tracks), frag.Length, frag.Duration, frag.Independent)

// ✅ 7. Метрики для моніторингу
metrics.RecordFragment(frag.Duration, frag.Length, defaultFields, totalFields)
```

---

## 🔗 Корисні посилання

- 💻 [ISO/IEC 14496-12:2020 (ISO BMFF)](https://www.iso.org/standard/79428.html) — офіційний стандарт контейнера
- 📄 [MP4 Track Fragment Specification](https://developer.apple.com/library/archive/documentation/QuickTime/QTFF/QTFFChap2/qtff2.html#//apple_ref/doc/uid/TP40000939-CH204-25688) — Apple documentation про moof/trun атоми
- 📄 [Composition Time Offset](https://wiki.multimedia.cx/index.php/MP4#ctts) — детальний опис CTS для B-frames
- 🧪 [Go sync.Pool Documentation](https://pkg.go.dev/sync#Pool) — ефективне управління пам'яттю
- 📦 [Prometheus Client Go](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) — метрики продуктивності

---

> 💡 **Ключові рекомендації для вашого проекту**:
> 1. **Перераховуйте DataOffset після moof.Marshal()** — уникнення некоректних зміщень через variable-length encoding.
> 2. **Встановлюйте `track.Run.Version = 1` для від'ємного CTS** — забезпечення коректної обробки B-frames.
> 3. **Використовуйте Bytes[:Length] для доступу до даних** — уникнення читання неініціалізованої пам'яті.
> 4. **Валідуйте independent прапорець** — уникнення помилок seek та початку відтворення.
> 5. **Моніторьте OptimizationRatio** — оцінка ефективності default полів для економії пропускної здатності.

Потрібен приклад реалізації повного циклу low-latency streaming з WebSocket, або допомога з оптимізацією фрагментації для adaptive bitrate? Готовий допомогти! 🚀