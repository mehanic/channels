package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"golang-rtmp/internal/hls"
)

func main() {
	var (
		inputPath       = flag.String("input", "", "Input video file path")
		outputDir       = flag.String("output", "", "Output directory for HLS files")
		segmentDuration = flag.Int("duration", 4, "Segment duration in seconds")
		playlistWindow  = flag.Int("window", 3, "Playlist window size")
		videoBitrate    = flag.String("vb", "1000k", "Video bitrate")
		audioBitrate    = flag.String("ab", "128k", "Audio bitrate")
		resolution      = flag.String("resolution", "1280x720", "Video resolution")
		fps             = flag.String("fps", "30", "Frame rate")
		videoCodec      = flag.String("vc", "libx264", "Video codec")
		audioCodec      = flag.String("ac", "aac", "Audio codec")
		extraFlags      = flag.String("flags", "", "Extra FFmpeg flags")
		showHelp        = flag.Bool("help", false, "Show help message")
	)
	flag.Parse()

	if *showHelp {
		showUsage()
		return
	}

	if *inputPath == "" || *outputDir == "" {
		fmt.Println("Error: Input file and output directory are required")
		showUsage()
		os.Exit(1)
	}

	fmt.Printf("Creating HLS stream...\n")
	fmt.Printf("Input: %s\n", *inputPath)
	fmt.Printf("Output: %s\n", *outputDir)
	fmt.Printf("Segment duration: %ds\n", *segmentDuration)
	fmt.Printf("Playlist window: %d segments\n", *playlistWindow)
	fmt.Printf("Video: %s %s %s %s\n", *videoCodec, *videoBitrate, *resolution, *fps)
	fmt.Printf("Audio: %s %s\n", *audioCodec, *audioBitrate)
	fmt.Println()

	opts := hls.DefaultHLSOptions()
	opts.SegmentDuration = *segmentDuration
	opts.PlaylistWindow = *playlistWindow
	opts.VideoCodec = *videoCodec
	opts.AudioCodec = *audioCodec
	opts.VideoBitrate = *videoBitrate
	opts.AudioBitrate = *audioBitrate
	opts.Resolution = *resolution
	opts.FPS = *fps

	if *extraFlags != "" {
		opts.ExtraFlags = []string{*extraFlags}
	}

	err := hls.CreateHLSWithOptions(*inputPath, *outputDir, opts)
	if err != nil {
		log.Fatalf("Failed to create HLS stream: %v", err)
	}

	fmt.Printf("\nHLS stream created successfully!\n")
	fmt.Printf("Playlist file: %s/stream.m3u8\n", *outputDir)
	fmt.Printf("Segment files: %s/segment_*.ts\n", *outputDir)
	fmt.Println()
	fmt.Println("You can now serve these files with any HTTP server.")
	fmt.Println("Example: python3 -m http.server 8080")
	fmt.Printf("Then access: http://localhost:8080/stream.m3u8\n")
}

func showUsage() {
	fmt.Println("Usage: hls-example -input <file> -output <dir> [options]")
	fmt.Println()
	fmt.Println("Required arguments:")
	fmt.Println("  -input <file>      Input video file (e.g., input.mp4)")
	fmt.Println("  -output <dir>      Output directory for HLS files")
	fmt.Println()
	fmt.Println("Optional arguments:")
	fmt.Println("  -duration <sec>    Segment duration in seconds (default: 4)")
	fmt.Println("  -window <num>      Playlist window size (default: 3)")
	fmt.Println("  -vc <codec>        Video codec (default: libx264)")
	fmt.Println("  -ac <codec>        Audio codec (default: aac)")
	fmt.Println("  -vb <bitrate>      Video bitrate (default: 1000k)")
	fmt.Println("  -ab <bitrate>      Audio bitrate (default: 128k)")
	fmt.Println("  -resolution <res>  Resolution (default: 1280x720)")
	fmt.Println("  -fps <fps>         Frame rate (default: 30)")
	fmt.Println("  -flags <flags>     Extra FFmpeg flags")
	fmt.Println("  -help              Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  hls-example -input input.mp4 -output output/")
	fmt.Println("  hls-example -input input.mp4 -output output/ -duration 6 -window 5 -vb 2000k -resolution 1920x1080")
	fmt.Println("  hls-example -input input.mp4 -output output/ -flags '-preset fast -crf 23'")
}
