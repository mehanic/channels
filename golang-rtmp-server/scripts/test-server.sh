#!/bin/bash

set -e

echo "Starting RTMP to HLS server test..."

if ! command -v ffmpeg &> /dev/null; then
    echo "Error: FFmpeg is not installed or not in PATH"
    exit 1
fi

if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed or not in PATH"
    exit 1
fi

echo "Building server..."
go build -o rtmp-server cmd/server/main.go

echo "Starting server in background..."
./rtmp-server -config config.yaml &
SERVER_PID=$!

echo "Server started with PID: $SERVER_PID"

sleep 5

echo "Testing server health..."
if ! curl -f http://localhost:8080/health > /dev/null 2>&1; then
    echo "Error: Server health check failed"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

echo "Server is healthy"

echo "Testing API endpoints..."
if ! curl -f http://localhost:8080/api/v1/streams > /dev/null 2>&1; then
    echo "Error: API endpoint test failed"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

echo "API endpoints are working"

echo "Testing metrics endpoint..."
if ! curl -f http://localhost:9090/metrics > /dev/null 2>&1; then
    echo "Error: Metrics endpoint test failed"
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

echo "Metrics endpoint is working"

echo "Creating test video..."
ffmpeg -f lavfi -i testsrc=duration=10:size=320x240:rate=1 -f lavfi -i sine=frequency=1000:duration=10 -c:v libx264 -c:a aac -shortest test-video.mp4 -y

echo "Streaming test video to RTMP server..."
ffmpeg -re -i test-video.mp4 -c copy -f flv rtmp://localhost:1935/live/test &
FFMPEG_PID=$!

echo "FFmpeg streaming started with PID: $FFMPEG_PID"

sleep 15

echo "Checking for HLS output..."
if [ ! -f "hls/live/test/playlist.m3u8" ]; then
    echo "Error: HLS playlist not found"
    kill $FFMPEG_PID 2>/dev/null || true
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

echo "HLS playlist found"

echo "Testing HLS playlist access..."
if ! curl -f http://localhost:8080/hls/live/test/playlist.m3u8 > /dev/null 2>&1; then
    echo "Error: HLS playlist access failed"
    kill $FFMPEG_PID 2>/dev/null || true
    kill $SERVER_PID 2>/dev/null || true
    exit 1
fi

echo "HLS playlist is accessible"

echo "Stopping FFmpeg..."
kill $FFMPEG_PID 2>/dev/null || true

echo "Stopping server..."
kill $SERVER_PID 2>/dev/null || true

echo "Cleaning up..."
rm -f test-video.mp4
rm -rf hls/

echo "Test completed successfully!" 