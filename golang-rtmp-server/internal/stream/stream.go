package stream

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type Stream struct {
	ID           string
	AppName      string
	StreamName   string
	OutputDir    string
	FFmpegCmd    *exec.Cmd
	FFmpegCtx    context.Context
	FFmpegCancel context.CancelFunc
	IsActive     bool
	StartTime    time.Time
	LastUpdate   time.Time
	mu           sync.RWMutex
	logger       *logrus.Logger
}

type StreamManager struct {
	streams map[string]*Stream
	mu      sync.RWMutex
	logger  *logrus.Logger
}

func NewStreamManager(logger *logrus.Logger) *StreamManager {
	return &StreamManager{
		streams: make(map[string]*Stream),
		logger:  logger,
	}
}

func (sm *StreamManager) CreateStream(appName, streamName, outputDir string) *Stream {
	streamID := fmt.Sprintf("%s/%s", appName, streamName)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if existing, exists := sm.streams[streamID]; exists {
		return existing
	}

	stream := &Stream{
		ID:         streamID,
		AppName:    appName,
		StreamName: streamName,
		OutputDir:  outputDir,
		IsActive:   false,
		StartTime:  time.Now(),
		LastUpdate: time.Now(),
		logger:     sm.logger,
	}

	sm.streams[streamID] = stream
	sm.logger.Infof("Created stream: %s", streamID)

	return stream
}

func (sm *StreamManager) GetStream(streamID string) (*Stream, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stream, exists := sm.streams[streamID]
	return stream, exists
}

func (sm *StreamManager) ListStreams() []*Stream {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	streams := make([]*Stream, 0, len(sm.streams))
	for _, stream := range sm.streams {
		streams = append(streams, stream)
	}
	return streams
}

func (sm *StreamManager) RemoveStream(streamID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if stream, exists := sm.streams[streamID]; exists {
		stream.Stop()
		delete(sm.streams, streamID)
		sm.logger.Infof("Removed stream: %s", streamID)
	}
}

func (s *Stream) StartFFmpeg(ffmpegPath string, params map[string]string, segmentDuration, playlistWindow int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsActive {
		return fmt.Errorf("stream %s is already active", s.ID)
	}

	if err := os.MkdirAll(s.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	playlistPath := filepath.Join(s.OutputDir, "playlist.m3u8")
	segmentPattern := filepath.Join(s.OutputDir, "segment_%03d.ts")

	args := []string{
		"-i", "rtmp://localhost/live/" + s.StreamName,
		"-c:v", params["video_codec"],
		"-c:a", params["audio_codec"],
		"-b:v", params["video_bitrate"],
		"-b:a", params["audio_bitrate"],
		"-s", params["resolution"],
		"-r", params["fps"],
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", segmentDuration),
		"-hls_list_size", fmt.Sprintf("%d", playlistWindow),
		"-hls_flags", "delete_segments",
		"-hls_segment_filename", segmentPattern,
		playlistPath,
	}

	s.FFmpegCtx, s.FFmpegCancel = context.WithCancel(context.Background())
	s.FFmpegCmd = exec.CommandContext(s.FFmpegCtx, ffmpegPath, args...)

	s.FFmpegCmd.Stdout = os.Stdout
	s.FFmpegCmd.Stderr = os.Stderr

	if err := s.FFmpegCmd.Start(); err != nil {
		s.FFmpegCancel()
		return fmt.Errorf("failed to start FFmpeg: %w", err)
	}

	s.IsActive = true
	s.logger.Infof("Started FFmpeg for stream: %s", s.ID)

	go s.monitorFFmpeg()

	return nil
}

func (s *Stream) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsActive {
		return
	}

	if s.FFmpegCancel != nil {
		s.FFmpegCancel()
	}

	if s.FFmpegCmd != nil && s.FFmpegCmd.Process != nil {
		if err := s.FFmpegCmd.Process.Kill(); err != nil {
			s.logger.Errorf("failed to kill FFmpeg process for stream %s: %v", s.ID, err)
		}
	}

	s.IsActive = false
	s.logger.Infof("Stopped stream: %s", s.ID)
}

func (s *Stream) monitorFFmpeg() {
	err := s.FFmpegCmd.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil && s.IsActive {
		s.logger.Errorf("FFmpeg process for stream %s exited with error: %v", s.ID, err)
		s.IsActive = false
	}
}

func (s *Stream) UpdateLastActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUpdate = time.Now()
}

func (s *Stream) GetStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"id":          s.ID,
		"app_name":    s.AppName,
		"stream_name": s.StreamName,
		"is_active":   s.IsActive,
		"start_time":  s.StartTime,
		"last_update": s.LastUpdate,
		"output_dir":  s.OutputDir,
	}
}
