@echo off
setlocal enabledelayedexpansion

echo Starting RTMP to HLS server test...

where ffmpeg >nul 2>&1
if %errorlevel% neq 0 (
    echo Error: FFmpeg is not installed or not in PATH
    exit /b 1
)

where go >nul 2>&1
if %errorlevel% neq 0 (
    echo Error: Go is not installed or not in PATH
    exit /b 1
)

echo Building server...
go build -o rtmp-server.exe cmd/server/main.go
if %errorlevel% neq 0 (
    echo Error: Failed to build server
    exit /b 1
)

echo Starting server in background...
start /b rtmp-server.exe -config config.yaml
set SERVER_PID=%errorlevel%

echo Server started

timeout /t 5 /nobreak >nul

echo Testing server health...
curl -f http://localhost:8080/health >nul 2>&1
if %errorlevel% neq 0 (
    echo Error: Server health check failed
    taskkill /f /im rtmp-server.exe >nul 2>&1
    exit /b 1
)

echo Server is healthy

echo Testing API endpoints...
curl -f http://localhost:8080/api/v1/streams >nul 2>&1
if %errorlevel% neq 0 (
    echo Error: API endpoint test failed
    taskkill /f /im rtmp-server.exe >nul 2>&1
    exit /b 1
)

echo API endpoints are working

echo Testing metrics endpoint...
curl -f http://localhost:9090/metrics >nul 2>&1
if %errorlevel% neq 0 (
    echo Error: Metrics endpoint test failed
    taskkill /f /im rtmp-server.exe >nul 2>&1
    exit /b 1
)

echo Metrics endpoint is working

echo Creating test video...
ffmpeg -f lavfi -i testsrc=duration=10:size=320x240:rate=1 -f lavfi -i sine=frequency=1000:duration=10 -c:v libx264 -c:a aac -shortest test-video.mp4 -y

echo Streaming test video to RTMP server...
start /b ffmpeg -re -i test-video.mp4 -c copy -f flv rtmp://localhost:1935/live/test

echo FFmpeg streaming started

timeout /t 15 /nobreak >nul

echo Checking for HLS output...
if not exist "hls\live\test\playlist.m3u8" (
    echo Error: HLS playlist not found
    taskkill /f /im ffmpeg.exe >nul 2>&1
    taskkill /f /im rtmp-server.exe >nul 2>&1
    exit /b 1
)

echo HLS playlist found

echo Testing HLS playlist access...
curl -f http://localhost:8080/hls/live/test/playlist.m3u8 >nul 2>&1
if %errorlevel% neq 0 (
    echo Error: HLS playlist access failed
    taskkill /f /im ffmpeg.exe >nul 2>&1
    taskkill /f /im rtmp-server.exe >nul 2>&1
    exit /b 1
)

echo HLS playlist is accessible

echo Stopping FFmpeg...
taskkill /f /im ffmpeg.exe >nul 2>&1

echo Stopping server...
taskkill /f /im rtmp-server.exe >nul 2>&1

echo Cleaning up...
if exist test-video.mp4 del test-video.mp4
if exist hls rmdir /s /q hls

echo Test completed successfully! 