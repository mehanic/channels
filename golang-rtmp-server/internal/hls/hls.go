package hls

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
)

type HLSOptions struct {
	SegmentDuration int
	PlaylistWindow  int
	VideoCodec      string
	AudioCodec      string
	VideoBitrate    string
	AudioBitrate    string
	Resolution      string
	FPS             string
	ExtraFlags      []string
}

func DefaultHLSOptions() *HLSOptions {
	return &HLSOptions{
		SegmentDuration: 4,
		PlaylistWindow:  3,
		VideoCodec:      "libx264",
		AudioCodec:      "aac",
		VideoBitrate:    "1000k",
		AudioBitrate:    "128k",
		Resolution:      "1280x720",
		FPS:             "30",
		ExtraFlags:      []string{},
	}
}

func CreateHLS(inputPath, outputDir string, segmentDuration, playlistWindow int) error {
	opts := DefaultHLSOptions()
	opts.SegmentDuration = segmentDuration
	opts.PlaylistWindow = playlistWindow
	return CreateHLSWithOptions(inputPath, outputDir, opts)
}

func CreateHLSWithOptions(inputPath, outputDir string, opts *HLSOptions) error {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	if err := validateInput(inputPath); err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	playlistPath := filepath.Join(outputDir, "stream.m3u8")
	segmentPattern := filepath.Join(outputDir, "segment_%03d.ts")

	args := buildFFmpegArgs(inputPath, playlistPath, segmentPattern, opts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	go streamOutput(stdout, "STDOUT", logger)
	go streamOutput(stderr, "STDERR", logger)

	setupSignalHandlers(cancel, logger)

	logger.Infof("Starting FFmpeg with args: %s", strings.Join(args, " "))

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	err = cmd.Wait()
	if err != nil {
		cleanupOnFailure(outputDir, logger)
		return fmt.Errorf("FFmpeg process failed: %w", err)
	}

	logger.Infof("HLS stream created successfully in: %s", outputDir)
	return nil
}

func validateInput(inputPath string) error {
	if inputPath == "" {
		return fmt.Errorf("input path cannot be empty")
	}

	fileInfo, err := os.Stat(inputPath)
	if err != nil {
		return fmt.Errorf("failed to stat input file: %w", err)
	}

	if fileInfo.IsDir() {
		return fmt.Errorf("input path is a directory, expected a file")
	}

	if fileInfo.Size() == 0 {
		return fmt.Errorf("input file is empty")
	}

	return nil
}

func buildFFmpegArgs(inputPath, playlistPath, segmentPattern string, opts *HLSOptions) []string {
	args := []string{
		"-i", inputPath,
		"-c:v", opts.VideoCodec,
		"-c:a", opts.AudioCodec,
		"-b:v", opts.VideoBitrate,
		"-b:a", opts.AudioBitrate,
		"-s", opts.Resolution,
		"-r", opts.FPS,
		"-f", "hls",
		"-hls_time", strconv.Itoa(opts.SegmentDuration),
		"-hls_list_size", strconv.Itoa(opts.PlaylistWindow),
		"-hls_flags", "delete_segments",
		"-hls_segment_filename", segmentPattern,
	}

	args = append(args, opts.ExtraFlags...)
	args = append(args, playlistPath)

	return args
}

func streamOutput(pipe io.ReadCloser, prefix string, logger *logrus.Logger) {
	defer pipe.Close()

	buffer := make([]byte, 1024)
	for {
		n, err := pipe.Read(buffer)
		if n > 0 {
			output := strings.TrimSpace(string(buffer[:n]))
			if output != "" {
				logger.Infof("[%s] %s", prefix, output)
			}
		}
		if err != nil {
			if err != io.EOF {
				logger.Errorf("[%s] Error reading output: %v", prefix, err)
			}
			break
		}
	}
}

func setupSignalHandlers(cancel context.CancelFunc, logger *logrus.Logger) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Infof("Received signal %v, shutting down gracefully...", sig)
		cancel()
	}()
}

func cleanupOnFailure(outputDir string, logger *logrus.Logger) {
	logger.Warnf("Cleaning up partial output in: %s", outputDir)

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		logger.Errorf("Failed to read output directory for cleanup: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			filePath := filepath.Join(outputDir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				logger.Errorf("Failed to remove file %s during cleanup: %v", filePath, err)
			} else {
				logger.Infof("Removed file during cleanup: %s", filePath)
			}
		}
	}
}
