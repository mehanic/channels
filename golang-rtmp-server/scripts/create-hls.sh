#!/bin/bash

set -e

INPUT_FILE=""
OUTPUT_DIR=""
SEGMENT_DURATION=4
PLAYLIST_WINDOW=3
VIDEO_CODEC="libx264"
AUDIO_CODEC="aac"
VIDEO_BITRATE="1000k"
AUDIO_BITRATE="128k"
RESOLUTION="1280x720"
FPS="30"
EXTRA_FLAGS=""

show_usage() {
    echo "Usage: $0 -i <input_file> -o <output_dir> [options]"
    echo ""
    echo "Required arguments:"
    echo "  -i <input_file>     Input video file (e.g., input.mp4)"
    echo "  -o <output_dir>     Output directory for HLS files"
    echo ""
    echo "Optional arguments:"
    echo "  -d <duration>       Segment duration in seconds (default: 4)"
    echo "  -w <window>         Playlist window size (default: 3)"
    echo "  -vc <codec>         Video codec (default: libx264)"
    echo "  -ac <codec>         Audio codec (default: aac)"
    echo "  -vb <bitrate>       Video bitrate (default: 1000k)"
    echo "  -ab <bitrate>       Audio bitrate (default: 128k)"
    echo "  -r <resolution>     Resolution (default: 1280x720)"
    echo "  -f <fps>            Frame rate (default: 30)"
    echo "  -e <flags>          Extra FFmpeg flags"
    echo "  -h                  Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 -i input.mp4 -o output/"
    echo "  $0 -i input.mp4 -o output/ -d 6 -w 5 -vb 2000k -r 1920x1080"
    echo "  $0 -i input.mp4 -o output/ -e '-preset fast -crf 23'"
}

validate_input() {
    if [[ ! -f "$INPUT_FILE" ]]; then
        echo "Error: Input file '$INPUT_FILE' does not exist or is not a file"
        exit 1
    fi

    if [[ ! -r "$INPUT_FILE" ]]; then
        echo "Error: Input file '$INPUT_FILE' is not readable"
        exit 1
    fi

    if [[ -z "$OUTPUT_DIR" ]]; then
        echo "Error: Output directory is required"
        exit 1
    fi
}

create_output_dir() {
    mkdir -p "$OUTPUT_DIR"
    if [[ $? -ne 0 ]]; then
        echo "Error: Failed to create output directory '$OUTPUT_DIR'"
        exit 1
    fi
}

build_ffmpeg_command() {
    local playlist_path="$OUTPUT_DIR/stream.m3u8"
    local segment_pattern="$OUTPUT_DIR/segment_%03d.ts"

    local cmd="ffmpeg -i \"$INPUT_FILE\""
    cmd="$cmd -c:v $VIDEO_CODEC"
    cmd="$cmd -c:a $AUDIO_CODEC"
    cmd="$cmd -b:v $VIDEO_BITRATE"
    cmd="$cmd -b:a $AUDIO_BITRATE"
    cmd="$cmd -s $RESOLUTION"
    cmd="$cmd -r $FPS"
    cmd="$cmd -f hls"
    cmd="$cmd -hls_time $SEGMENT_DURATION"
    cmd="$cmd -hls_list_size $PLAYLIST_WINDOW"
    cmd="$cmd -hls_flags delete_segments"
    cmd="$cmd -hls_segment_filename \"$segment_pattern\""

    if [[ -n "$EXTRA_FLAGS" ]]; then
        cmd="$cmd $EXTRA_FLAGS"
    fi

    cmd="$cmd \"$playlist_path\""

    echo "$cmd"
}

cleanup_on_error() {
    echo "Cleaning up partial output..."
    if [[ -d "$OUTPUT_DIR" ]]; then
        rm -f "$OUTPUT_DIR"/*.ts "$OUTPUT_DIR"/*.m3u8 2>/dev/null || true
    fi
}

trap cleanup_on_error ERR

while getopts "i:o:d:w:vc:ac:vb:ab:r:f:e:h" opt; do
    case $opt in
        i) INPUT_FILE="$OPTARG" ;;
        o) OUTPUT_DIR="$OPTARG" ;;
        d) SEGMENT_DURATION="$OPTARG" ;;
        w) PLAYLIST_WINDOW="$OPTARG" ;;
        vc) VIDEO_CODEC="$OPTARG" ;;
        ac) AUDIO_CODEC="$OPTARG" ;;
        vb) VIDEO_BITRATE="$OPTARG" ;;
        ab) AUDIO_BITRATE="$OPTARG" ;;
        r) RESOLUTION="$OPTARG" ;;
        f) FPS="$OPTARG" ;;
        e) EXTRA_FLAGS="$OPTARG" ;;
        h) show_usage; exit 0 ;;
        *) show_usage; exit 1 ;;
    esac
done

if [[ -z "$INPUT_FILE" || -z "$OUTPUT_DIR" ]]; then
    echo "Error: Input file and output directory are required"
    show_usage
    exit 1
fi

validate_input
create_output_dir

echo "Creating HLS stream..."
echo "Input: $INPUT_FILE"
echo "Output: $OUTPUT_DIR"
echo "Segment duration: ${SEGMENT_DURATION}s"
echo "Playlist window: $PLAYLIST_WINDOW segments"
echo ""

ffmpeg_cmd=$(build_ffmpeg_command)
echo "FFmpeg command:"
echo "$ffmpeg_cmd"
echo ""

eval "$ffmpeg_cmd"

if [[ $? -eq 0 ]]; then
    echo ""
    echo "HLS stream created successfully!"
    echo "Playlist file: $OUTPUT_DIR/stream.m3u8"
    echo "Segment files: $OUTPUT_DIR/segment_*.ts"
    echo ""
    echo "You can now serve these files with any HTTP server."
    echo "Example: python3 -m http.server 8080"
    echo "Then access: http://localhost:8080/stream.m3u8"
else
    echo "Error: FFmpeg failed to create HLS stream"
    exit 1
fi 