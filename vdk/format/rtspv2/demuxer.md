# 📦 Глибокий розбір: `rtspv2.RTPDemuxer` — Парсинг RTP пакетів у av.Packet

Цей файл — **реалізація демуксингу RTP пакетів** для RTSP клієнта `vdk`. Він перетворює сирий RTP потік (отриманий через TCP interleaved) у уніфіковані `av.Packet` об'єкти, підтримуючи відео (H.264/H.265) та аудіо (AAC, Opus, G.711) кодеки.

---

## 🔧 Критичне виправлення: парсинг timestamp

**❌ Помилка у вихідному коді:**
```go
client.timestamp = int64(binary.BigEndian.Uint32(content[8:16]))  // Читає 8 байт!
```

**✅ Правильна реалізація:**
```go
// RTP timestamp — завжди 4 байти (32 біти)
client.timestamp = int64(binary.BigEndian.Uint32(content[8:12]))
```

**Наслідки помилки:**
- Читання за межі буфера → паніка при коротких пакетах
- Неправильне значення timestamp → розсинхронізація аудіо/відео

---

## 🗺️ Архітектурна схема

```
┌────────────────────────────────────────┐
│ 📦 RTPDemuxer — RTP → av.Packet        │
├────────────────────────────────────────┤
│                                         │
│  🔑 Ключові компоненти:                 │
│  • RTPDemuxer() — головний диспетчер   │
│  • handleVideo()/handleAudio() — обробка│
│  • handleH264Payload()/handleH265Payload()│
│  • appendVideoPacket()/appendAudioPacket()│
│                                         │
│  🔄 Потік даних:                        │
│  [RTP header][payload] → parse header  │
│  → extract NALU/AU → av.Packet queue   │
│                                         │
│  📊 Підтримка кодеків:                  │
│  • Відео: H.264 (Single/FU-A/STAP-A), H.265│
│  • Аудіо: AAC (AU-headers), Opus, G.711│
│                                         │
└────────────────────────────────────────┘
```

---

## 🔑 1. RTPDemuxer() — парсинг заголовку

### 🔧 Виправлена логіка:

```go
func (client *RTSPClient) RTPDemuxer(payloadRAW *[]byte) ([]*av.Packet, bool) {
    content := *payloadRAW
    
    // 1. Перевірка мінімальної довжини
    if len(content) < 4+12 {  // 4 = interleaved header, 12 = RTP header
        return nil, false
    }
    
    // 2. Парсинг базових полів
    firstByte := content[4]
    padding := (firstByte>>5)&1 == 1
    extension := (firstByte>>4)&1 == 1
    CSRCCnt := int(firstByte & 0x0f)
    
    // 3. ✅ ВИПРАВЛЕНО: timestamp — 4 байти, не 8!
    client.sequenceNumber = int(binary.BigEndian.Uint16(content[6:8]))
    client.timestamp = int64(binary.BigEndian.Uint32(content[8:12]))  // ✅ 4 байти
    
    // 4. Пропуск RTCP
    if isRTCPPacket(content) {
        return nil, false
    }
    
    // 5. Розрахунок зміщення до payload
    offset := 12  // RTP header size
    if offset+4*CSRCCnt <= len(content) {
        offset += 4 * CSRCCnt
    }
    if extension && offset+4 <= len(content) {
        extLen := 4 * int(binary.BigEndian.Uint16(content[offset+2:offset+4]))
        offset += 4 + extLen
    }
    if padding && offset < len(content) {
        paddingLen := int(content[len(content)-1])
        if len(content)-offset >= paddingLen {
            content = content[:len(content)-paddingLen]
        }
    }
    
    // 6. Диспетчеризація за channel ID
    switch int(content[1]) {
    case client.videoID:
        return client.handleVideo(content, offset)
    case client.audioID:
        return client.handleAudio(content, offset)
    }
    
    return nil, false
}
```

### ✅ Ваш use-case: валідація RTP заголовку

```go
// ValidateRTPHeader — перевірка коректності перед обробкою
func ValidateRTPHeader(content []byte) error {
    if len(content) < 4+12 {
        return fmt.Errorf("RTP packet too short: %d bytes", len(content))
    }
    
    if content[0] != 0x24 {
        return fmt.Errorf("invalid interleaved magic: 0x%02X", content[0])
    }
    
    version := (content[4] >> 6) & 0x3
    if version != 2 {
        return fmt.Errorf("unsupported RTP version: %d", version)
    }
    
    return nil
}
```

---

## 🔑 2. handleVideo() — обробка відео з корекцією таймінгів

### 🔧 Виправлена логіка переповнення:

```go
func (client *RTSPClient) handleVideo(content []byte, offset int) ([]*av.Packet, bool) {
    // 1. Ініціалізація таймінгів
    if client.PreVideoTS == 0 {
        client.PreVideoTS = client.timestamp
    }
    
    // 2. ✅ ВИПРАВЛЕНО: коректна обробка переповнення 32-бітного timestamp
    if client.timestamp < client.PreVideoTS {
        // Перевіряємо чи це дійсно переповнення, а не втрата пакетів
        if client.PreVideoTS-client.timestamp < 9000 {  // <100ms @ 90kHz
            // Переповнення: додаємо 2^32
            client.PreVideoTS -= (1 << 32)
        } else {
            // Втрата пакетів: скидаємо
            client.PreVideoTS = client.timestamp
        }
    }
    
    // 3. Перевірка послідовності
    if client.PreSequenceNumber != 0 && client.sequenceNumber != client.PreSequenceNumber+1 {
        lost := client.sequenceNumber - client.PreSequenceNumber - 1
        if lost > 0 && lost < 100 {
            client.Println("packet loss:", lost, "packets")
        }
    }
    client.PreSequenceNumber = client.sequenceNumber
    
    // 4. Захист буфера
    if client.BufferRtpPacket.Len() > 4*1024*1024 {  // 4MB ліміт
        client.Println("Big Buffer Flush")
        client.BufferRtpPacket.Reset()
    }
    
    // 5. Обробка payload
    payload := content[offset:]
    nalRaw, _ := h264parser.SplitNALUs(payload)
    
    var retmap []*av.Packet
    for _, nal := range nalRaw {
        if client.videoCodec == av.H265 {
            retmap = client.handleH265Payload(nal, retmap)
        } else if client.videoCodec == av.H264 {
            retmap = client.handleH264Payload(content, nal, offset, retmap)
        }
    }
    
    if len(retmap) > 0 {
        client.PreVideoTS = client.timestamp
        return retmap, true
    }
    return nil, false
}
```

### ✅ Ваш use-case: обробка втрачених пакетів

```go
// HandlePacketLoss — логіка відновлення
func (c *RTSPClient) HandlePacketLoss(expectedSeq, receivedSeq int) {
    lost := receivedSeq - expectedSeq - 1
    if lost > 0 && lost < 100 {
        c.Println("packet loss:", lost)
        
        // Опціонально: надіслати PLI через RTCP для запиту ключового кадру
        // c.sendPLI()
    }
}
```

---

## 🔑 3. handleH264Payload() — виправлена обробка фрагментації

### 🔧 Ключові виправлення:

```go
func (client *RTSPClient) handleH264Payload(content, nal []byte, offset int, retmap []*av.Packet) []*av.Packet {
    naluType := nal[0] & 0x1f
    
    switch {
    // Single NALU (1-5)
    case naluType >= 1 && naluType <= 5:
        retmap = client.appendVideoPacket(retmap, nal, naluType == 5)
        
    // SPS/PPS
    case naluType == 7:
        client.CodecUpdateSPS(nal)
    case naluType == 8:
        client.CodecUpdatePPS(nal)
        
    // STAP-A (агрегація)
    case naluType == 24:
        packet := nal[1:]
        for len(packet) >= 2 {
            size := int(binary.BigEndian.Uint16(packet[0:2]))
            if size+2 > len(packet) { break }
            
            subNALU := packet[2 : size+2]
            subType := subNALU[0] & 0x1f
            
            switch {
            case subType >= 1 && subType <= 5:
                retmap = client.appendVideoPacket(retmap, subNALU, subType == 5)
            case subType == 7:
                client.CodecUpdateSPS(subNALU)
            case subType == 8:
                client.CodecUpdatePPS(subNALU)
            }
            packet = packet[size+2:]
        }
        
    // FU-A (фрагментація) — ✅ ВИПРАВЛЕНО
    case naluType == 28:
        if len(content) < offset+2 {
            return retmap  // недостатньо даних
        }
        
        fuHeader := content[offset+1]
        isStart := fuHeader&0x80 != 0
        isEnd := fuHeader&0x40 != 0
        naluTypeOrig := fuHeader & 0x1f
        
        if isStart {
            client.fuStarted = true
            client.BufferRtpPacket.Reset()
            // Відновлення оригінального заголовку
            header := (content[offset] & 0xE0) | naluTypeOrig
            client.BufferRtpPacket.WriteByte(byte(header))
        }
        
        if client.fuStarted {
            client.BufferRtpPacket.Write(content[offset+2:])
            
            if isEnd {
                client.fuStarted = false
                completeNALU := client.BufferRtpPacket.Bytes()
                
                // Спеціальна обробка SPS/PPS/IDR
                origType := completeNALU[0] & 0x1f
                if origType == 5 {
                    retmap = client.appendVideoPacket(retmap, completeNALU, true)
                } else if origType == 7 {
                    client.CodecUpdateSPS(completeNALU)
                } else if origType == 8 {
                    client.CodecUpdatePPS(completeNALU)
                } else {
                    retmap = client.appendVideoPacket(retmap, completeNALU, false)
                }
            }
        }
    }
    
    return retmap
}
```

### 🔍 Формат FU-A:

```
RTP payload для FU-A:
  [0]: FU indicator (type=28)
  [1]: FU header
       • Bit 7: S (Start)
       • Bit 6: E (End)  
       • Bits 5-0: original NALU type
  [2...]: fragment data

Приклад збірки:
  Пакет 1 (S=1): [FU header][data1] → буфер = [orig header][data1]
  Пакет 2 (S=0,E=0): [FU header][data2] → буфер += [data2]
  Пакет 3 (S=0,E=1): [FU header][data3] → буфер += [data3] → готовий NALU
```

---

## 🔑 4. handleAudio() — виправлена обробка AAC

### 🔧 Ключові виправлення:

```go
func (client *RTSPClient) handleAudio(content []byte, offset int) ([]*av.Packet, bool) {
    if client.PreAudioTS == 0 {
        client.PreAudioTS = client.timestamp
    }
    
    payload := content[offset:]
    var retmap []*av.Packet
    
    switch client.audioCodec {
    case av.PCM_MULAW, av.PCM_ALAW:
        // G.711: кожен байт = 1 семпл @ 8kHz
        duration := time.Duration(len(payload)) * time.Second / time.Duration(client.AudioTimeScale)
        retmap = client.appendAudioPacket(retmap, payload, duration)
        
    case av.OPUS:
        // Opus: фіксовані 20ms фрейми
        duration := 20 * time.Millisecond
        retmap = client.appendAudioPacket(retmap, payload, duration)
        
    case av.AAC:
        // ✅ ВИПРАВЛЕНО: коректний парсинг AU-headers
        if len(payload) < 2 {
            return nil, false
        }
        
        auHeadersLength := binary.BigEndian.Uint16(payload[0:2])
        auHeadersCount := auHeadersLength >> 4  // кількість заголовків
        framesOffset := 2 + int(auHeadersCount)*2
        
        if framesOffset > len(payload) {
            return nil, false  // некоректні дані
        }
        
        auHeaders := payload[2:framesOffset]
        framesPayload := payload[framesOffset:]
        
        for i := 0; i < int(auHeadersCount) && len(auHeaders) >= 2; i++ {
            auHeader := binary.BigEndian.Uint16(auHeaders[0:2])
            frameSize := int(auHeader >> 3)  // старші 13 біт = розмір
            
            if frameSize > len(framesPayload) {
                break  // некоректний розмір
            }
            
            frame := framesPayload[:frameSize]
            
            // Видалення ADTS header якщо присутній
            if len(frame) >= 7 && frame[0] == 0xFF && (frame[1]&0xF0) == 0xF0 {
                frame = frame[7:]
            }
            
            // Розрахунок duration: 1024 семпли / sampleRate
            duration := time.Duration(1024) * time.Second / time.Duration(client.AudioTimeScale)
            
            retmap = client.appendAudioPacket(retmap, frame, duration)
            
            // Перехід до наступного
            auHeaders = auHeaders[2:]
            framesPayload = framesPayload[frameSize:]
        }
    }
    
    if len(retmap) > 0 {
        client.PreAudioTS = client.timestamp
        return retmap, true
    }
    return nil, false
}
```

### 🔍 Чому AAC складніший?

```
AAC у RTP (RFC 3640) може містити кілька фреймів в одному пакеті:

Структура:
  [AU-headers length: 16 біт]
  [AU-header 1: 16 біт] [AU-header 2: 16 біт] ...
  [Frame 1 data] [Frame 2 data] ...

Кожен AU-header:
  • Bits 15-3: AU-size (13 біт) — розмір фрейму
  • Bits 2-0: AU-Index — зазвичай 0

Це економить накладні витрати на заголовки для коротких аудіо фреймів.
```

---

## 🔑 5. appendVideoPacket()/appendAudioPacket() — виправлені таймінги

### 🔧 appendVideoPacket():

```go
func (client *RTSPClient) appendVideoPacket(retmap []*av.Packet, nal []byte, isKeyFrame bool) []*av.Packet {
    // Розрахунок duration: різниця timestamp / 90kHz → ms
    duration := time.Duration(client.timestamp - client.PreVideoTS) * time.Millisecond / 90
    
    // Абсолютний час: timestamp / 90kHz → ms
    pts := time.Duration(client.timestamp) * time.Millisecond / 90
    
    return append(retmap, &av.Packet{
        Data:            append(binSize(len(nal)), nal...),  // AVCC формат
        CompositionTime: time.Duration(1) * time.Millisecond,
        Duration:        duration,
        Idx:             client.videoIDX,
        IsKeyFrame:      isKeyFrame,
        Time:            pts,
    })
}
```

### 🔧 appendAudioPacket():

```go
func (client *RTSPClient) appendAudioPacket(retmap []*av.Packet, nal []byte, duration time.Duration) []*av.Packet {
    // Аудіо: накопичувальна лінія часу
    client.AudioTimeLine += duration
    
    return append(retmap, &av.Packet{
        Data:            nal,
        CompositionTime: time.Duration(1) * time.Millisecond,
        Duration:        duration,
        Idx:             client.audioIDX,
        IsKeyFrame:      false,
        Time:            client.AudioTimeLine,
    })
}
```

### ✅ Ваш use-case: синхронізація таймінгів

```go
// SyncAVTimestamps — корекція розсинхронізації
func SyncAVTimestamps(videoPTS, audioPTS time.Duration, maxDrift time.Duration) (time.Duration, error) {
    drift := videoPTS - audioPTS
    if abs(drift) > maxDrift {
        return audioPTS + drift, fmt.Errorf("sync adjusted by %v", drift)
    }
    return audioPTS, nil
}

func abs(d time.Duration) time.Duration {
    if d < 0 { return -d }
    return d
}
```

---

## 📋 Чек-лист виправлень

```go
// ✅ 1. Виправити парсинг timestamp
client.timestamp = int64(binary.BigEndian.Uint32(content[8:12]))  // 4 байти

// ✅ 2. Додати валідацію довжини перед доступом до слайсів
if len(content) < offset+2 { return retmap }

// ✅ 3. Коректна обробка переповнення timestamp
if client.timestamp < client.PreVideoTS {
    if client.PreVideoTS - client.timestamp < 9000 {
        client.PreVideoTS -= (1 << 32)  // переповнення
    } else {
        client.PreVideoTS = client.timestamp  // втрата пакетів
    }
}

// ✅ 4. Захист буфера від переповнення
if client.BufferRtpPacket.Len() > 4*1024*1024 {
    client.BufferRtpPacket.Reset()
}

// ✅ 5. Коректний розрахунок duration для відео
duration := time.Duration(client.timestamp - client.PreVideoTS) * time.Millisecond / 90

// ✅ 6. Метрики для моніторингу
metrics.RecordPacket(time.Since(start), channelID)
```

---

## 🔗 Корисні посилання

- 📄 [RTP Timestamp Wraparound (RFC 3550)](https://datatracker.ietf.org/doc/html/rfc3550#section-5.1)
- 📄 [H.264 FU-A Fragmentation (RFC 6184)](https://datatracker.ietf.org/doc/html/rfc6184#section-5.8)
- 📄 [AAC in RTP (RFC 3640)](https://datatracker.ietf.org/doc/html/rfc3640#section-3.3.1)
- 💻 [Go encoding/binary](https://pkg.go.dev/encoding/binary)

---

> 💡 **Ключові рекомендації**:
> 1. **Виправте `content[8:16]` → `content[8:12]`** — це критично для коректних таймінгів.
> 2. **Обробляйте переповнення 32-бітного timestamp** — станеться кожні ~13 годин.
> 3. **Валідуйте довжину перед доступом до слайсів** — уникнення панік.
> 4. **Моніторьте стан `fuStarted`** — завислі фрагменти → витік пам'яті.
> 5. **Синхронізуйте аудіо/відео** — різні логіки розрахунку можуть призвести до дрейфу.

Потрібен приклад інтеграції `SyncAVTimestamps()` з вашим `pubsub.Queue`? Готовий допомогти! 🚀