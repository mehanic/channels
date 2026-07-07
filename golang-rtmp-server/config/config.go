package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	RTMP    RTMPConfig    `yaml:"rtmp"`
	HLS     HLSConfig     `yaml:"hls"`
	FFmpeg  FFmpegConfig  `yaml:"ffmpeg"`
	Logging LoggingConfig `yaml:"logging"`
	Metrics MetricsConfig `yaml:"metrics"`
}

type ServerConfig struct {
	HTTPPort int `yaml:"http_port"`
}

type RTMPConfig struct {
	Port int `yaml:"port"`
}

type HLSConfig struct {
	OutputDir       string `yaml:"output_dir"`
	SegmentDuration int    `yaml:"segment_duration"`
	PlaylistWindow  int    `yaml:"playlist_window"`
}

type FFmpegConfig struct {
	BinaryPath string            `yaml:"binary_path"`
	Params     map[string]string `yaml:"params"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"`
}

func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			HTTPPort: 8080,
		},
		RTMP: RTMPConfig{
			Port: 1935,
		},
		HLS: HLSConfig{
			OutputDir:       "./hls",
			SegmentDuration: 4,
			PlaylistWindow:  10,
		},
		FFmpeg: FFmpegConfig{
			BinaryPath: "ffmpeg",
			Params: map[string]string{
				"video_codec":   "libx264",
				"audio_codec":   "aac",
				"video_bitrate": "1000k",
				"audio_bitrate": "128k",
				"resolution":    "1280x720",
				"fps":           "30",
			},
		},
		Logging: LoggingConfig{
			Level: "info",
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Port:    9090,
		},
	}
}
