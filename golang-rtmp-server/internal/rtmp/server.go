package rtmp

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"golang-rtmp/internal/stream"

	"github.com/nareix/joy4/format/rtmp"
	"github.com/sirupsen/logrus"
)

type Server struct {
	addr          string
	streamManager *stream.StreamManager
	logger        *logrus.Logger
	ffmpegPath    string
	ffmpegParams  map[string]string
	hlsConfig     struct {
		outputDir       string
		segmentDuration int
		playlistWindow  int
	}
	mu sync.RWMutex
}

func NewServer(addr string, streamManager *stream.StreamManager, logger *logrus.Logger) *Server {
	return &Server{
		addr:          addr,
		streamManager: streamManager,
		logger:        logger,
	}
}

func (s *Server) SetFFmpegConfig(ffmpegPath string, params map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ffmpegPath = ffmpegPath
	s.ffmpegParams = params
}

func (s *Server) SetHLSConfig(outputDir string, segmentDuration, playlistWindow int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hlsConfig.outputDir = outputDir
	s.hlsConfig.segmentDuration = segmentDuration
	s.hlsConfig.playlistWindow = playlistWindow
}

func (s *Server) Start() error {
	rtmpServer := &rtmp.Server{
		Addr:          s.addr,
		HandlePublish: s.handlePublish,
		HandlePlay:    s.handlePlay,
	}

	s.logger.Infof("RTMP server started on %s", s.addr)
	return rtmpServer.ListenAndServe()
}

func (s *Server) handlePublish(conn *rtmp.Conn) {
	appName := strings.TrimPrefix(conn.URL.Path, "/")
	if appName == "" {
		appName = "live"
	}

	streamName := conn.URL.RawQuery
	if streamName == "" {
		streamName = "stream"
	}

	streamID := fmt.Sprintf("%s/%s", appName, streamName)
	s.logger.Infof("Publish request: %s", streamID)

	s.mu.RLock()
	outputDir := filepath.Join(s.hlsConfig.outputDir, appName, streamName)
	segmentDuration := s.hlsConfig.segmentDuration
	playlistWindow := s.hlsConfig.playlistWindow
	ffmpegPath := s.ffmpegPath
	ffmpegParams := s.ffmpegParams
	s.mu.RUnlock()

	stream := s.streamManager.CreateStream(appName, streamName, outputDir)

	if err := stream.StartFFmpeg(ffmpegPath, ffmpegParams, segmentDuration, playlistWindow); err != nil {
		s.logger.Errorf("Failed to start FFmpeg for stream %s: %v", streamID, err)
		return
	}

	defer func() {
		stream.Stop()
		s.streamManager.RemoveStream(streamID)
	}()

	s.logger.Infof("Started publishing stream: %s", streamID)

	for {
		_, err := conn.ReadPacket()
		if err != nil {
			s.logger.Errorf("Error reading packet from stream %s: %v", streamID, err)
			break
		}

		stream.UpdateLastActivity()
	}

	s.logger.Infof("Stopped publishing stream: %s", streamID)
}

func (s *Server) handlePlay(conn *rtmp.Conn) {
	appName := strings.TrimPrefix(conn.URL.Path, "/")
	if appName == "" {
		appName = "live"
	}

	streamName := conn.URL.RawQuery
	if streamName == "" {
		streamName = "stream"
	}

	streamID := fmt.Sprintf("%s/%s", appName, streamName)
	s.logger.Infof("Play request: %s", streamID)

	stream, exists := s.streamManager.GetStream(streamID)
	if !exists {
		s.logger.Errorf("Stream not found: %s", streamID)
		return
	}

	if !stream.IsActive {
		s.logger.Errorf("Stream is not active: %s", streamID)
		return
	}

	s.logger.Infof("Started playing stream: %s", streamID)

	stream.UpdateLastActivity()
}
