@echo off
setlocal enabledelayedexpansion

set INPUT_FILE=
set OUTPUT_DIR=
set SEGMENT_DURATION=4
set PLAYLIST_WINDOW=3
set VIDEO_CODEC=libx264
set AUDIO_CODEC=aac
set VIDEO_BITRATE=1000k
set AUDIO_BITRATE=128k
set RESOLUTION=1280x720
set FPS=30
set EXTRA_FLAGS=

:parse_args
if "%~1"=="" goto :check_args
if "%~1"=="-i" (
    set INPUT_FILE=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-o" (
    set OUTPUT_DIR=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-d" (
    set SEGMENT_DURATION=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-w" (
    set PLAYLIST_WINDOW=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-vc" (
    set VIDEO_CODEC=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-ac" (
    set AUDIO_CODEC=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-vb" (
    set VIDEO_BITRATE=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-ab" (
    set AUDIO_BITRATE=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-r" (
    set RESOLUTION=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-f" (
    set FPS=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-e" (
    set EXTRA_FLAGS=%~2
    shift
    shift
    goto :parse_args
)
if "%~1"=="-h" (
    call :show_usage
    exit /b 0
)
echo Error: Unknown option %~1
call :show_usage
exit /b 1

:check_args
if "%INPUT_FILE%"=="" (
    echo Error: Input file is required
    call :show_usage
    exit /b 1
)
if "%OUTPUT_DIR%"=="" (
    echo Error: Output directory is required
    call :show_usage
    exit /b 1
)

call :validate_input
call :create_output_dir
call :create_hls
exit /b %ERRORLEVEL%

:show_usage
echo Usage: %~nx0 -i ^<input_file^> -o ^<output_dir^> [options]
echo.
echo Required arguments:
echo   -i ^<input_file^>     Input video file ^(e.g., input.mp4^)
echo   -o ^<output_dir^>     Output directory for HLS files
echo.
echo Optional arguments:
echo   -d ^<duration^>       Segment duration in seconds ^(default: 4^)
echo   -w ^<window^>         Playlist window size ^(default: 3^)
echo   -vc ^<codec^>         Video codec ^(default: libx264^)
echo   -ac ^<codec^>         Audio codec ^(default: aac^)
echo   -vb ^<bitrate^>       Video bitrate ^(default: 1000k^)
echo   -ab ^<bitrate^>       Audio bitrate ^(default: 128k^)
echo   -r ^<resolution^>     Resolution ^(default: 1280x720^)
echo   -f ^<fps^>            Frame rate ^(default: 30^)
echo   -e ^<flags^>          Extra FFmpeg flags
echo   -h                    Show this help message
echo.
echo Examples:
echo   %~nx0 -i input.mp4 -o output\
echo   %~nx0 -i input.mp4 -o output\ -d 6 -w 5 -vb 2000k -r 1920x1080
echo   %~nx0 -i input.mp4 -o output\ -e "-preset fast -crf 23"
goto :eof

:validate_input
if not exist "%INPUT_FILE%" (
    echo Error: Input file '%INPUT_FILE%' does not exist
    exit /b 1
)
goto :eof

:create_output_dir
if not exist "%OUTPUT_DIR%" (
    mkdir "%OUTPUT_DIR%"
    if errorlevel 1 (
        echo Error: Failed to create output directory '%OUTPUT_DIR%'
        exit /b 1
    )
)
goto :eof

:create_hls
echo Creating HLS stream...
echo Input: %INPUT_FILE%
echo Output: %OUTPUT_DIR%
echo Segment duration: %SEGMENT_DURATION%s
echo Playlist window: %PLAYLIST_WINDOW% segments
echo.

set PLAYLIST_PATH=%OUTPUT_DIR%\stream.m3u8
set SEGMENT_PATTERN=%OUTPUT_DIR%\segment_%%03d.ts

set FFMPEG_CMD=ffmpeg -i "%INPUT_FILE%" -c:v %VIDEO_CODEC% -c:a %AUDIO_CODEC% -b:v %VIDEO_BITRATE% -b:a %AUDIO_BITRATE% -s %RESOLUTION% -r %FPS% -f hls -hls_time %SEGMENT_DURATION% -hls_list_size %PLAYLIST_WINDOW% -hls_flags delete_segments -hls_segment_filename "%SEGMENT_PATTERN%"

if not "%EXTRA_FLAGS%"=="" (
    set FFMPEG_CMD=%FFMPEG_CMD% %EXTRA_FLAGS%
)

set FFMPEG_CMD=%FFMPEG_CMD% "%PLAYLIST_PATH%"

echo FFmpeg command:
echo %FFMPEG_CMD%
echo.

%FFMPEG_CMD%

if errorlevel 1 (
    echo.
    echo Error: FFmpeg failed to create HLS stream
    call :cleanup_on_error
    exit /b 1
) else (
    echo.
    echo HLS stream created successfully!
    echo Playlist file: %OUTPUT_DIR%\stream.m3u8
    echo Segment files: %OUTPUT_DIR%\segment_*.ts
    echo.
    echo You can now serve these files with any HTTP server.
    echo Example: python -m http.server 8080
    echo Then access: http://localhost:8080/stream.m3u8
)
goto :eof

:cleanup_on_error
echo Cleaning up partial output...
if exist "%OUTPUT_DIR%\*.ts" del /q "%OUTPUT_DIR%\*.ts" 2>nul
if exist "%OUTPUT_DIR%\*.m3u8" del /q "%OUTPUT_DIR%\*.m3u8" 2>nul
goto :eof 