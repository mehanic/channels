package stream

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestStreamManager_CreateStream(t *testing.T) {
	logger := logrus.New()
	sm := NewStreamManager(logger)

	stream := sm.CreateStream("testapp", "teststream", "/tmp/test")
	if stream == nil {
		t.Fatal("Expected stream to be created")
	}

	if stream.ID != "testapp/teststream" {
		t.Errorf("Expected stream ID 'testapp/teststream', got '%s'", stream.ID)
	}

	if stream.AppName != "testapp" {
		t.Errorf("Expected app name 'testapp', got '%s'", stream.AppName)
	}

	if stream.StreamName != "teststream" {
		t.Errorf("Expected stream name 'teststream', got '%s'", stream.StreamName)
	}

	if stream.OutputDir != "/tmp/test" {
		t.Errorf("Expected output dir '/tmp/test', got '%s'", stream.OutputDir)
	}

	if stream.IsActive {
		t.Error("Expected stream to be inactive initially")
	}
}

func TestStreamManager_GetStream(t *testing.T) {
	logger := logrus.New()
	sm := NewStreamManager(logger)

	createdStream := sm.CreateStream("testapp", "teststream", "/tmp/test")
	retrievedStream, exists := sm.GetStream("testapp/teststream")

	if !exists {
		t.Fatal("Expected stream to exist")
	}

	if retrievedStream != createdStream {
		t.Error("Expected retrieved stream to be the same as created stream")
	}

	_, exists = sm.GetStream("nonexistent")
	if exists {
		t.Error("Expected non-existent stream to not exist")
	}
}

func TestStreamManager_ListStreams(t *testing.T) {
	logger := logrus.New()
	sm := NewStreamManager(logger)

	sm.CreateStream("app1", "stream1", "/tmp/test1")
	sm.CreateStream("app2", "stream2", "/tmp/test2")

	streams := sm.ListStreams()
	if len(streams) != 2 {
		t.Errorf("Expected 2 streams, got %d", len(streams))
	}
}

func TestStreamManager_RemoveStream(t *testing.T) {
	logger := logrus.New()
	sm := NewStreamManager(logger)

	sm.CreateStream("testapp", "teststream", "/tmp/test")

	streams := sm.ListStreams()
	if len(streams) != 1 {
		t.Errorf("Expected 1 stream before removal, got %d", len(streams))
	}

	sm.RemoveStream("testapp/teststream")

	streams = sm.ListStreams()
	if len(streams) != 0 {
		t.Errorf("Expected 0 streams after removal, got %d", len(streams))
	}
}

func TestStream_UpdateLastActivity(t *testing.T) {
	logger := logrus.New()
	stream := &Stream{
		ID:         "test",
		AppName:    "testapp",
		StreamName: "teststream",
		OutputDir:  "/tmp/test",
		IsActive:   false,
		StartTime:  time.Now(),
		LastUpdate: time.Now(),
		logger:     logger,
	}

	originalUpdate := stream.LastUpdate
	time.Sleep(1 * time.Millisecond)

	stream.UpdateLastActivity()

	if stream.LastUpdate.Equal(originalUpdate) {
		t.Error("Expected LastUpdate to be updated")
	}
}

func TestStream_GetStatus(t *testing.T) {
	logger := logrus.New()
	now := time.Now()
	stream := &Stream{
		ID:         "testapp/teststream",
		AppName:    "testapp",
		StreamName: "teststream",
		OutputDir:  "/tmp/test",
		IsActive:   true,
		StartTime:  now,
		LastUpdate: now,
		logger:     logger,
	}

	status := stream.GetStatus()

	if status["id"] != "testapp/teststream" {
		t.Errorf("Expected ID 'testapp/teststream', got '%v'", status["id"])
	}

	if status["app_name"] != "testapp" {
		t.Errorf("Expected app_name 'testapp', got '%v'", status["app_name"])
	}

	if status["stream_name"] != "teststream" {
		t.Errorf("Expected stream_name 'teststream', got '%v'", status["stream_name"])
	}

	if status["is_active"] != true {
		t.Errorf("Expected is_active true, got '%v'", status["is_active"])
	}

	if status["output_dir"] != "/tmp/test" {
		t.Errorf("Expected output_dir '/tmp/test', got '%v'", status["output_dir"])
	}
}
