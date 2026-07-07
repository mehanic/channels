package http

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang-rtmp/internal/stream"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

type Server struct {
	addr          string
	streamManager *stream.StreamManager
	logger        *logrus.Logger
	hlsOutputDir  string
	metrics       *Metrics
}

type Metrics struct {
	activeStreams prometheus.Gauge
	httpRequests  prometheus.Counter
	httpDuration  prometheus.Histogram
}

func NewMetrics() *Metrics {
	return &Metrics{
		activeStreams: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "rtmp_active_streams",
			Help: "Number of active RTMP streams",
		}),
		httpRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		}),
		httpDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		}),
	}
}

func (m *Metrics) Register() {
	prometheus.MustRegister(m.activeStreams)
	prometheus.MustRegister(m.httpRequests)
	prometheus.MustRegister(m.httpDuration)
}

func NewServer(addr string, streamManager *stream.StreamManager, logger *logrus.Logger, hlsOutputDir string) *Server {
	metrics := NewMetrics()
	metrics.Register()

	return &Server{
		addr:          addr,
		streamManager: streamManager,
		logger:        logger,
		hlsOutputDir:  hlsOutputDir,
		metrics:       metrics,
	}
}

func (s *Server) Start() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	s.setupRoutes(router)

	s.logger.Infof("HTTP server started on %s", s.addr)
	return router.Run(s.addr)
}

func (s *Server) setupRoutes(router *gin.Engine) {
	router.Use(s.middleware())

	api := router.Group("/api/v1")
	{
		api.GET("/streams", s.listStreams)
		api.GET("/streams/:streamID", s.getStream)
		api.POST("/streams/:streamID/start", s.startStream)
		api.POST("/streams/:streamID/stop", s.stopStream)
		api.DELETE("/streams/:streamID", s.deleteStream)
	}

	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	router.GET("/hls/:app/:stream/playlist.m3u8", s.servePlaylist)
	router.GET("/hls/:app/:stream/:segment", s.serveSegment)

	router.GET("/stream.m3u8", s.serveDirectPlaylist)
	router.GET("/segment_:segment", s.serveDirectSegment)

	router.GET("/health", s.healthCheck)
}

func (s *Server) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		s.metrics.httpRequests.Inc()

		c.Next()

		duration := time.Since(start).Seconds()
		s.metrics.httpDuration.Observe(duration)
	}
}

func (s *Server) listStreams(c *gin.Context) {
	streams := s.streamManager.ListStreams()

	streamList := make([]map[string]interface{}, 0, len(streams))
	for _, stream := range streams {
		streamList = append(streamList, stream.GetStatus())
	}

	s.metrics.activeStreams.Set(float64(len(streams)))

	c.JSON(http.StatusOK, gin.H{
		"streams": streamList,
		"count":   len(streams),
	})
}

func (s *Server) getStream(c *gin.Context) {
	streamID := c.Param("streamID")

	stream, exists := s.streamManager.GetStream(streamID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stream not found"})
		return
	}

	c.JSON(http.StatusOK, stream.GetStatus())
}

func (s *Server) startStream(c *gin.Context) {
	streamID := c.Param("streamID")

	stream, exists := s.streamManager.GetStream(streamID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stream not found"})
		return
	}

	if stream.IsActive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Stream is already active"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Stream started", "stream": stream.GetStatus()})
}

func (s *Server) stopStream(c *gin.Context) {
	streamID := c.Param("streamID")

	stream, exists := s.streamManager.GetStream(streamID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "Stream not found"})
		return
	}

	stream.Stop()

	c.JSON(http.StatusOK, gin.H{"message": "Stream stopped", "stream": stream.GetStatus()})
}

func (s *Server) deleteStream(c *gin.Context) {
	streamID := c.Param("streamID")

	s.streamManager.RemoveStream(streamID)

	c.JSON(http.StatusOK, gin.H{"message": "Stream deleted"})
}

func (s *Server) servePlaylist(c *gin.Context) {
	app := c.Param("app")
	stream := c.Param("stream")

	playlistPath := filepath.Join(s.hlsOutputDir, app, stream, "playlist.m3u8")

	if _, err := os.Stat(playlistPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Playlist not found"})
		return
	}

	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type")

	c.File(playlistPath)
}

func (s *Server) serveSegment(c *gin.Context) {
	app := c.Param("app")
	stream := c.Param("stream")
	segment := c.Param("segment")

	if !strings.HasSuffix(segment, ".ts") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid segment file"})
		return
	}

	segmentPath := filepath.Join(s.hlsOutputDir, app, stream, segment)

	if _, err := os.Stat(segmentPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Segment not found"})
		return
	}

	c.Header("Content-Type", "video/mp2t")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Range")

	file, err := os.Open(segmentPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open segment file"})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get file info"})
		return
	}

	c.Header("Content-Length", strconv.FormatInt(stat.Size(), 10))
	c.Header("Accept-Ranges", "bytes")

	http.ServeContent(c.Writer, c.Request, segment, stat.ModTime(), file)
}

func (s *Server) serveDirectPlaylist(c *gin.Context) {
	playlistPath := filepath.Join(s.hlsOutputDir, "stream.m3u8")

	if _, err := os.Stat(playlistPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Playlist not found"})
		return
	}

	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type")

	c.File(playlistPath)
}

func (s *Server) serveDirectSegment(c *gin.Context) {
	segment := c.Param("segment")

	if !strings.HasSuffix(segment, ".ts") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid segment file"})
		return
	}

	segmentPath := filepath.Join(s.hlsOutputDir, "segment_"+segment)

	if _, err := os.Stat(segmentPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Segment not found"})
		return
	}

	c.Header("Content-Type", "video/mp2t")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Range")

	file, err := os.Open(segmentPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open segment file"})
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get file info"})
		return
	}

	c.Header("Content-Length", strconv.FormatInt(stat.Size(), 10))
	c.Header("Accept-Ranges", "bytes")

	http.ServeContent(c.Writer, c.Request, "segment_"+segment, stat.ModTime(), file)
}

func (s *Server) healthCheck(c *gin.Context) {
	streams := s.streamManager.ListStreams()
	activeCount := 0

	for _, stream := range streams {
		if stream.IsActive {
			activeCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "healthy",
		"active_streams": activeCount,
		"total_streams":  len(streams),
		"timestamp":      time.Now().Unix(),
	})
}
