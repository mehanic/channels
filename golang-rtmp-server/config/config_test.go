package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Server.HTTPPort != 8080 {
		t.Errorf("Expected HTTP port 8080, got %d", config.Server.HTTPPort)
	}

	if config.RTMP.Port != 1935 {
		t.Errorf("Expected RTMP port 1935, got %d", config.RTMP.Port)
	}

	if config.HLS.OutputDir != "./hls" {
		t.Errorf("Expected HLS output dir './hls', got '%s'", config.HLS.OutputDir)
	}

	if config.HLS.SegmentDuration != 4 {
		t.Errorf("Expected segment duration 4, got %d", config.HLS.SegmentDuration)
	}

	if config.HLS.PlaylistWindow != 10 {
		t.Errorf("Expected playlist window 10, got %d", config.HLS.PlaylistWindow)
	}

	if config.FFmpeg.BinaryPath != "ffmpeg" {
		t.Errorf("Expected FFmpeg binary path 'ffmpeg', got '%s'", config.FFmpeg.BinaryPath)
	}

	if config.Logging.Level != "info" {
		t.Errorf("Expected logging level 'info', got '%s'", config.Logging.Level)
	}

	if !config.Metrics.Enabled {
		t.Error("Expected metrics to be enabled")
	}

	if config.Metrics.Port != 9090 {
		t.Errorf("Expected metrics port 9090, got %d", config.Metrics.Port)
	}

	if config.FFmpeg.Params["video_codec"] != "libx264" {
		t.Errorf("Expected video codec 'libx264', got '%s'", config.FFmpeg.Params["video_codec"])
	}

	if config.FFmpeg.Params["audio_codec"] != "aac" {
		t.Errorf("Expected audio codec 'aac', got '%s'", config.FFmpeg.Params["audio_codec"])
	}
}

func TestLoadConfig(t *testing.T) {
	configContent := `
server:
  http_port: 9000

rtmp:
  port: 1936

hls:
  output_dir: "/tmp/hls"
  segment_duration: 6
  playlist_window: 15

ffmpeg:
  binary_path: "/usr/bin/ffmpeg"
  params:
    video_codec: "libx265"
    audio_codec: "mp3"
    video_bitrate: "2000k"
    audio_bitrate: "256k"
    resolution: "1920x1080"
    fps: "60"

logging:
  level: "debug"

metrics:
  enabled: false
  port: 9091
`

	tmpFile, err := os.CreateTemp("", "test_config_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write config content: %v", err)
	}
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if config.Server.HTTPPort != 9000 {
		t.Errorf("Expected HTTP port 9000, got %d", config.Server.HTTPPort)
	}

	if config.RTMP.Port != 1936 {
		t.Errorf("Expected RTMP port 1936, got %d", config.RTMP.Port)
	}

	if config.HLS.OutputDir != "/tmp/hls" {
		t.Errorf("Expected HLS output dir '/tmp/hls', got '%s'", config.HLS.OutputDir)
	}

	if config.HLS.SegmentDuration != 6 {
		t.Errorf("Expected segment duration 6, got %d", config.HLS.SegmentDuration)
	}

	if config.HLS.PlaylistWindow != 15 {
		t.Errorf("Expected playlist window 15, got %d", config.HLS.PlaylistWindow)
	}

	if config.FFmpeg.BinaryPath != "/usr/bin/ffmpeg" {
		t.Errorf("Expected FFmpeg binary path '/usr/bin/ffmpeg', got '%s'", config.FFmpeg.BinaryPath)
	}

	if config.Logging.Level != "debug" {
		t.Errorf("Expected logging level 'debug', got '%s'", config.Logging.Level)
	}

	if config.Metrics.Enabled {
		t.Error("Expected metrics to be disabled")
	}

	if config.Metrics.Port != 9091 {
		t.Errorf("Expected metrics port 9091, got %d", config.Metrics.Port)
	}

	if config.FFmpeg.Params["video_codec"] != "libx265" {
		t.Errorf("Expected video codec 'libx265', got '%s'", config.FFmpeg.Params["video_codec"])
	}

	if config.FFmpeg.Params["audio_codec"] != "mp3" {
		t.Errorf("Expected audio codec 'mp3', got '%s'", config.FFmpeg.Params["audio_codec"])
	}
}

func TestLoadConfig_FileNotExists(t *testing.T) {
	_, err := LoadConfig("nonexistent_file.yaml")
	if err == nil {
		t.Error("Expected error when loading non-existent config file")
	}
}
