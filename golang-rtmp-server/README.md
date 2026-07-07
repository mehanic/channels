# RTMP Server in Go with Dynamic Transcoding to HLS

A high-performance RTMP ingest server written in Go that receives video streams, pipes them in real-time to FFmpeg to generate HLS segments, and exposes those segments over HTTP for compatible players. This project also includes standalone HLS stream generation tools for converting video files to HLS format.

## Features

### RTMP Server
- **RTMP Ingestion**: Accepts RTMP streams from OBS, FFmpeg, or any RTMP-compatible client
- **Real-time Transcoding**: Uses FFmpeg to convert RTMP streams to HLS format
- **HTTP Delivery**: Serves HLS playlists and segments with proper CORS headers
- **REST API**: Control streams via HTTP API endpoints
- **Metrics**: Prometheus metrics for monitoring
- **Concurrent Streams**: Supports multiple simultaneous streams
- **Graceful Shutdown**: Proper cleanup of resources and FFmpeg processes

### HLS Stream Generator
- **Video Segmentation**: Split videos into `.ts` segments of configurable length
- **Sliding Window Playlist**: Maintain a configurable number of segments in the playlist
- **Real-time Playlist Updates**: Playlist updates automatically as segments are created
- **Robust Error Handling**: Validates input files and cleans up on failure
- **Signal Handling**: Gracefully handles SIGINT/SIGTERM signals
- **Cross-platform**: Works on Windows, macOS, and Linux
- **Flexible Configuration**: Support for custom codecs, bitrates, resolutions, and extra FFmpeg flags

## Prerequisites

- **Go 1.21+**: For the Go implementation
- **FFmpeg**: Must be installed and available in your system PATH

### Installing FFmpeg

**Windows:**
```bash
# Using Chocolatey
choco install ffmpeg

# Using Scoop
scoop install ffmpeg

# Or download from https://ffmpeg.org/download.html
```

**macOS:**
```bash
# Using Homebrew
brew install ffmpeg
```

**Linux (Ubuntu/Debian):**
```bash
sudo apt update
sudo apt install ffmpeg
```

## Installation

1. Install dependencies:
```bash
go mod download
```

2. Build the server:
```bash
go build -o rtmp-server cmd/server/main.go
```

3. Build the HLS example:
```bash
go build -o hls-example cmd/hls-example/main.go
```

## Usage

### RTMP Server

#### Starting the Server

```bash
./rtmp-server -config config.yaml
```

The server will start:
- RTMP server on port 1935 (default)
- HTTP server on port 8080 (default)
- Metrics endpoint on port 9090 (if enabled)

#### Streaming with OBS

1. Open OBS Studio
2. Go to Settings > Stream
3. Set Service to "Custom"
4. Set Server to `rtmp://localhost:1935/live`
5. Set Stream Key to your desired stream name (e.g., `mystream`)
6. Click "Start Streaming"

#### Playing the Stream

The HLS stream will be available at:
- Playlist: `http://localhost:8080/hls/live/mystream/playlist.m3u8`
- Segments: `http://localhost:8080/hls/live/mystream/segment_000.ts`

You can play this in:
- Web browsers with HLS.js or Video.js
- VLC media player
- Any HLS-compatible player

### HLS Stream Generator

#### Command Line Tools

**Shell Script (Linux/macOS):**
```bash
# Make executable
chmod +x scripts/create-hls.sh

# Basic usage
./scripts/create-hls.sh -i input.mp4 -o output/

# Advanced usage
./scripts/create-hls.sh -i input.mp4 -o output/ \
    -d 6 -w 5 -vb 2000k -r 1920x1080 \
    -e "-preset fast -crf 23"
```

**Batch File (Windows):**
```cmd
# Basic usage
scripts\create-hls.bat -i input.mp4 -o output\

# Advanced usage
scripts\create-hls.bat -i input.mp4 -o output\ -d 6 -w 5 -vb 2000k -r 1920x1080 -e "-preset fast -crf 23"
```

**Go Example Program:**
```bash
# Basic usage
./hls-example -input input.mp4 -output output/

# Advanced usage
./hls-example -input input.mp4 -output output/ \
    -duration 6 -window 5 -vb 2000k -resolution 1920x1080 \
    -flags "-preset fast -crf 23"
```

#### Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `SegmentDuration` | 4 | Segment duration in seconds |
| `PlaylistWindow` | 3 | Number of segments to keep in playlist |
| `VideoCodec` | libx264 | Video codec |
| `AudioCodec` | aac | Audio codec |
| `VideoBitrate` | 1000k | Video bitrate |
| `AudioBitrate` | 128k | Audio bitrate |
| `Resolution` | 1280x720 | Video resolution |
| `FPS` | 30 | Frame rate |
| `ExtraFlags` | [] | Additional FFmpeg flags |

## API Endpoints

### Stream Management

- `GET /api/v1/streams` - List all streams
- `GET /api/v1/streams/{streamID}` - Get stream details
- `POST /api/v1/streams/{streamID}/start` - Start a stream
- `POST /api/v1/streams/{streamID}/stop` - Stop a stream
- `DELETE /api/v1/streams/{streamID}` - Delete a stream

### Health and Metrics

- `GET /health` - Health check endpoint
- `GET /metrics` - Prometheus metrics

### HLS Delivery

- `GET /hls/{app}/{stream}/playlist.m3u8` - HLS playlist
- `GET /hls/{app}/{stream}/{segment}` - HLS segment files

## FFmpeg Commands

The HLS generator creates FFmpeg commands similar to this:

### Basic Command
```bash
ffmpeg -i input.mp4 \
    -c:v libx264 -c:a aac \
    -b:v 1000k -b:a 128k \
    -s 1280x720 -r 30 \
    -f hls \
    -hls_time 4 \
    -hls_list_size 3 \
    -hls_flags delete_segments \
    -hls_segment_filename output/segment_%03d.ts \
    output/stream.m3u8
```

### Advanced Command with Custom Settings
```bash
ffmpeg -i input.mp4 \
    -c:v libx264 -c:a aac \
    -b:v 2000k -b:a 192k \
    -s 1920x1080 -r 60 \
    -preset fast -crf 23 \
    -f hls \
    -hls_time 6 \
    -hls_list_size 5 \
    -hls_flags delete_segments \
    -hls_segment_filename output/segment_%03d.ts \
    output/stream.m3u8
```

### Playlist File (stream.m3u8)

The generated playlist file will look like:

```m3u8
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:4.000000,
segment_000.ts
#EXTINF:4.000000,
segment_001.ts
#EXTINF:4.000000,
segment_002.ts
#EXT-X-ENDLIST
```

## Testing

### RTMP Server Testing

**Test with FFmpeg:**
1. Start the server:
```bash
./rtmp-server
```

2. Stream a test video:
```bash
ffmpeg -re -i test-video.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

3. Play the HLS stream:
```bash
ffmpeg -i http://localhost:8080/hls/live/test/playlist.m3u8 -c copy output.mp4
```

**Test with OBS:**
1. Configure OBS to stream to `rtmp://localhost:1935/live/obs-test`
2. Start streaming in OBS
3. Open the playlist URL in a web browser or VLC

### HLS Generator Testing

**Running Unit Tests:**
```bash
# Run all HLS tests
go test ./internal/hls/...

# Run with verbose output
go test -v ./internal/hls/...

# Run with coverage
go test -cover ./internal/hls/...
```

**Manual Testing:**
1. **Create a test video file** (or use an existing one)
2. **Run the conversion**:
   ```bash
   go run cmd/hls-example/main.go -input test.mp4 -output test-output/
   ```
3. **Verify the output**:
   ```bash
   ls -la test-output/
   cat test-output/stream.m3u8
   ```
4. **Serve and test**:
   ```bash
   cd test-output/
   python3 -m http.server 8080
   # Open http://localhost:8080/stream.m3u8 in a video player
   ```

## Docker

### Building the Docker Image

```bash
docker build -t rtmp-server .
```

### Running with Docker

```bash
docker run -p 1935:1935 -p 8080:8080 -p 9090:9090 \
  -v $(pwd)/hls:/app/hls \
  rtmp-server
```

## Architecture

The server consists of several components:

1. **RTMP Server**: Handles RTMP connections using the joy4 library
2. **Stream Manager**: Manages stream lifecycle and FFmpeg processes
3. **HTTP Server**: Serves HLS content and provides REST API
4. **Configuration**: YAML-based configuration management
5. **Metrics**: Prometheus metrics collection

## Performance

The server is designed to handle:
- Multiple concurrent RTMP streams
- Real-time transcoding with configurable quality
- Efficient HLS segment delivery
- Proper resource cleanup

### Performance Considerations

- **Segment Duration**: Shorter segments (2-4s) provide faster start times but more files
- **Playlist Window**: Larger windows use more disk space but provide better buffering
- **Video Quality**: Higher bitrates and resolutions increase file sizes and processing time
- **Codec Selection**: Hardware acceleration (e.g., `-c:v h264_nvenc`) can significantly improve performance

## Error Handling

The HLS module includes comprehensive error handling:

- **Input Validation**: Checks file existence, readability, and non-empty status
- **Directory Creation**: Automatically creates output directories if missing
- **Process Management**: Properly handles FFmpeg process lifecycle
- **Signal Handling**: Gracefully responds to SIGINT/SIGTERM
- **Cleanup**: Removes partial output files on failure
- **Logging**: Detailed logging of FFmpeg stdout/stderr

## Troubleshooting

### Common Issues

1. **FFmpeg not found**: Ensure FFmpeg is installed and in PATH
2. **Port conflicts**: Check if ports 1935, 8080, or 9090 are already in use
3. **Permission errors**: Ensure the HLS output directory is writable
4. **Stream not appearing**: Check RTMP URL format and stream key

### HLS Generator Issues

**FFmpeg Not Found:**
```
Error: failed to start FFmpeg: exec: "ffmpeg": executable file not found in $PATH
```
**Solution**: Install FFmpeg and ensure it's in your system PATH.

**Permission Denied:**
```
Error: failed to create output directory: permission denied
```
**Solution**: Check write permissions for the output directory.

**Invalid Input File:**
```
Error: input validation failed: failed to stat input file: stat /path/to/file: no such file or directory
```
**Solution**: Verify the input file path is correct and the file exists.

**Insufficient Disk Space:**
```
Error: FFmpeg process failed: exit status 1
```
**Solution**: Ensure sufficient disk space for the output files.