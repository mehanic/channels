package hls

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name        string
		inputPath   string
		expectError bool
		setup       func() string
		cleanup     func(string)
	}{
		{
			name:        "empty path",
			inputPath:   "",
			expectError: true,
		},
		{
			name:        "non-existent file",
			inputPath:   "/path/to/nonexistent/file.mp4",
			expectError: true,
		},
		{
			name:        "directory instead of file",
			inputPath:   ".",
			expectError: true,
		},
		{
			name:        "empty file",
			expectError: true,
			setup: func() string {
				emptyFile := filepath.Join(t.TempDir(), "empty.mp4")
				file, _ := os.Create(emptyFile)
				file.Close()
				return emptyFile
			},
		},
		{
			name:        "valid file",
			expectError: false,
			setup: func() string {
				validFile := filepath.Join(t.TempDir(), "valid.mp4")
				file, _ := os.Create(validFile)
				if _, err := file.WriteString("fake video content"); err != nil {
					t.Fatalf("failed to write fake video content: %v", err)
				}
				file.Close()
				return validFile
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var inputPath string
			if tt.setup != nil {
				inputPath = tt.setup()
			} else {
				inputPath = tt.inputPath
			}

			err := validateInput(inputPath)
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestDefaultHLSOptions(t *testing.T) {
	opts := DefaultHLSOptions()

	if opts.SegmentDuration != 4 {
		t.Errorf("expected SegmentDuration to be 4, got %d", opts.SegmentDuration)
	}

	if opts.PlaylistWindow != 3 {
		t.Errorf("expected PlaylistWindow to be 3, got %d", opts.PlaylistWindow)
	}

	if opts.VideoCodec != "libx264" {
		t.Errorf("expected VideoCodec to be libx264, got %s", opts.VideoCodec)
	}

	if opts.AudioCodec != "aac" {
		t.Errorf("expected AudioCodec to be aac, got %s", opts.AudioCodec)
	}

	if opts.VideoBitrate != "1000k" {
		t.Errorf("expected VideoBitrate to be 1000k, got %s", opts.VideoBitrate)
	}

	if opts.AudioBitrate != "128k" {
		t.Errorf("expected AudioBitrate to be 128k, got %s", opts.AudioBitrate)
	}

	if opts.Resolution != "1280x720" {
		t.Errorf("expected Resolution to be 1280x720, got %s", opts.Resolution)
	}

	if opts.FPS != "30" {
		t.Errorf("expected FPS to be 30, got %s", opts.FPS)
	}
}

func TestBuildFFmpegArgs(t *testing.T) {
	opts := DefaultHLSOptions()
	opts.ExtraFlags = []string{"-preset", "fast"}

	inputPath := "/path/to/input.mp4"
	playlistPath := "/path/to/output/stream.m3u8"
	segmentPattern := "/path/to/output/segment_%03d.ts"

	args := buildFFmpegArgs(inputPath, playlistPath, segmentPattern, opts)

	expectedArgs := []string{
		"-i", inputPath,
		"-c:v", "libx264",
		"-c:a", "aac",
		"-b:v", "1000k",
		"-b:a", "128k",
		"-s", "1280x720",
		"-r", "30",
		"-f", "hls",
		"-hls_time", "4",
		"-hls_list_size", "3",
		"-hls_flags", "delete_segments",
		"-hls_segment_filename", segmentPattern,
		"-preset", "fast",
		playlistPath,
	}

	if len(args) != len(expectedArgs) {
		t.Errorf("expected %d args, got %d", len(expectedArgs), len(args))
		return
	}

	for i, expected := range expectedArgs {
		if args[i] != expected {
			t.Errorf("arg[%d]: expected %s, got %s", i, expected, args[i])
		}
	}
}

func TestCreateHLSWithInvalidInput(t *testing.T) {
	outputDir := t.TempDir()

	err := CreateHLS("/nonexistent/file.mp4", outputDir, 4, 3)
	if err == nil {
		t.Error("expected error for non-existent input file")
	}
}

func TestCreateHLSWithEmptyInput(t *testing.T) {
	outputDir := t.TempDir()

	err := CreateHLS("", outputDir, 4, 3)
	if err == nil {
		t.Error("expected error for empty input path")
	}
}

func TestCreateHLSWithCustomOptions(t *testing.T) {
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "test.mp4")
	outputDir := filepath.Join(tempDir, "output")

	file, err := os.Create(inputFile)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if _, err := file.WriteString("fake video content"); err != nil {
		t.Fatalf("failed to write fake video content: %v", err)
	}
	file.Close()

	opts := DefaultHLSOptions()
	opts.SegmentDuration = 6
	opts.PlaylistWindow = 5
	opts.VideoBitrate = "2000k"
	opts.Resolution = "1920x1080"

	err = CreateHLSWithOptions(inputFile, outputDir, opts)
	if err == nil {
		t.Log("CreateHLSWithOptions completed (this is expected to fail without FFmpeg)")
	}
}
