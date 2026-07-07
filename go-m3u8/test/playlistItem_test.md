// Для ефективної генерації Master Playlist з багатьма варіантами:
func generateOptimizedVariants(channelID string) []*m3u8.PlaylistItem {
variants := []struct{
bw int; width, height int; fps float64; profile, level, audio string
}{
{800000, 854, 480, 25, "main", "3.1", "aac-lc"},   // 480p
{2500000, 1280, 720, 25, "main", "4.0", "aac-lc"}, // 720p
{5000000, 1920, 1080, 25, "high", "4.1", "aac-lc"},// 1080p
}

    var items []*m3u8.PlaylistItem
    for _, v := range variants {
        items = append(items, &m3u8.PlaylistItem{
            Bandwidth:  v.bw,
            URI:        fmt.Sprintf("/channels/%s/video_%dp.m3u8", channelID, v.height),
            Resolution: &m3u8.Resolution{Width: v.width, Height: v.height},
            FrameRate:  pointer(v.fps),
            Profile:    pointer(v.profile),
            Level:      pointer(v.level),
            AudioCodec: pointer(v.audio),
            Audio:      pointer("audio"),
            Subtitles:  pointer("subs"),
        })
    }
    return items
}
// → Клієнти автоматично обирають оптимальну якість за мережевими умовами