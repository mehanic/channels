package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang-rtmp/config"
	"golang-rtmp/internal/http"
	"golang-rtmp/internal/rtmp"
	"golang-rtmp/internal/stream"

	"github.com/sirupsen/logrus"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	flag.Parse()

	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	var cfg *config.Config
	var err error

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		logger.Warnf("Configuration file %s not found, using default configuration", configPath)
		cfg = config.DefaultConfig()
	} else {
		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			logger.Fatalf("Failed to load configuration: %v", err)
		}
	}

	level, err := logrus.ParseLevel(cfg.Logging.Level)
	if err != nil {
		logger.Warnf("Invalid log level %s, using info", cfg.Logging.Level)
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	logger.Info("Starting RTMP to HLS server")

	streamManager := stream.NewStreamManager(logger)

	rtmpAddr := fmt.Sprintf(":%d", cfg.RTMP.Port)
	rtmpServer := rtmp.NewServer(rtmpAddr, streamManager, logger)
	rtmpServer.SetFFmpegConfig(cfg.FFmpeg.BinaryPath, cfg.FFmpeg.Params)
	rtmpServer.SetHLSConfig(cfg.HLS.OutputDir, cfg.HLS.SegmentDuration, cfg.HLS.PlaylistWindow)

	httpAddr := fmt.Sprintf(":%d", cfg.Server.HTTPPort)
	httpServer := http.NewServer(httpAddr, streamManager, logger, cfg.HLS.OutputDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := rtmpServer.Start(); err != nil {
			logger.Errorf("RTMP server error: %v", err)
			cancel()
		}
	}()

	go func() {
		if err := httpServer.Start(); err != nil {
			logger.Errorf("HTTP server error: %v", err)
			cancel()
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		logger.Info("Server context cancelled")
	case sig := <-sigChan:
		logger.Infof("Received signal %v, shutting down", sig)
	}

	logger.Info("Shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	streams := streamManager.ListStreams()
	for _, stream := range streams {
		stream.Stop()
	}

	select {
	case <-shutdownCtx.Done():
		logger.Warn("Shutdown timeout reached")
	default:
		logger.Info("Server shutdown completed")
	}
}
