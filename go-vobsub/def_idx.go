package vobsub

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	idxSizePrefix       = "size: "
	idxOriginPrefix     = "org: "
	idxAlphaRatioPrefix = "alpha: "
	idxSmoothPrefix     = "smooth: "
	idxFadePrefix       = "fadein/out: "
	idxFadeUnit         = time.Millisecond
	idxAlignPrefix      = "align: "
	idxTimeOffsetPrefix = "time offset: "
	idxTimeOffsetUnit   = time.Millisecond
	idxForcedSubsPrefix = "forced subs: "
	idxLangIdxPrefix    = "langidx: "
	idxPalettePrefix    = "palette: "
	idxPaletteLen       = 16
)

// IdxMetadata contains the index metadata of a sub file (.idx file)
type IdxMetadata struct {
	Width, Height   int
	Origin          image.Point
	AlphaRatio      float64
	Smooth          bool
	FadeIn, FadeOut time.Duration
	Align           string // not supported yet
	TimeOffset      time.Duration
	ForcedSubs      bool
	LangIdx         int
	Palette         color.Palette
}

// ParseIdx scans a reader to extract the index metadata of a sub file (.idx file)
func ParseIdx(reader io.Reader) (metadata IdxMetadata, err error) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		// Width, Height
		case strings.HasPrefix(line, idxSizePrefix):
			values := strings.Split(line[len(idxSizePrefix):], "x")
			if len(values) != 2 {
				err = fmt.Errorf("expecting size to have only two values: %v", values)
				return
			}
			// Parse width
			if metadata.Width, err = strconv.Atoi(values[0]); err != nil {
				err = fmt.Errorf("failed to convert width to integer: %w", err)
				return
			}
			// Parse height
			if metadata.Height, err = strconv.Atoi(values[1]); err != nil {
				err = fmt.Errorf("failed to convert width to integer: %w", err)
				return
			}
		// Origin
		case strings.HasPrefix(line, idxOriginPrefix):
			values := strings.Split(line[len(idxOriginPrefix):], ", ")
			if len(values) != 2 {
				err = fmt.Errorf("expecting size to have only two values: %v", values)
				return
			}
			// Parse X
			if metadata.Origin.X, err = strconv.Atoi(values[0]); err != nil {
				err = fmt.Errorf("failed to convert width to integer: %w", err)
				return
			}
			// Parse Y
			if metadata.Origin.Y, err = strconv.Atoi(values[1]); err != nil {
				err = fmt.Errorf("failed to convert width to integer: %w", err)
				return
			}
		// Alpha ratio
		case strings.HasPrefix(line, idxAlphaRatioPrefix):
			value := line[len(idxAlphaRatioPrefix):]
			if value[len(value)-1] != '%' {
				err = fmt.Errorf("alpha ratio line should end with '%%': %q", value)
				return
			}
			strValue := line[len(idxAlphaRatioPrefix) : len(line)-1]
			var intValue int
			if intValue, err = strconv.Atoi(strValue); err != nil {
				err = fmt.Errorf("can not parse alpha value %q as integer: %w", value, err)
				return
			}
			if metadata.AlphaRatio = float64(intValue) / 100; metadata.AlphaRatio <= 0 || metadata.AlphaRatio > 1 {
				err = fmt.Errorf("alpha ratio can not be inferior to 0 or greater than 100: %f", metadata.AlphaRatio)
				return
			}
		// Smooth
		case strings.HasPrefix(line, idxSmoothPrefix):
			value := line[len(idxSmoothPrefix):]
			switch value {
			case "ON":
				metadata.Smooth = true
			case "OFF":
			default:
				err = fmt.Errorf("unexpected smooth value: %q", value)
				return
			}
		// Fade in / Fade out
		case strings.HasPrefix(line, idxFadePrefix):
			values := strings.Split(line[len(idxFadePrefix):], ", ")
			if len(values) != 2 {
				err = fmt.Errorf("expecting fade in/out to have only two values: %v", values)
				return
			}
			var fadeValue int
			// Parse fade in
			if fadeValue, err = strconv.Atoi(values[0]); err != nil {
				err = fmt.Errorf("failed to convert fade in to integer: %w", err)
				return
			}
			metadata.FadeIn = time.Duration(fadeValue) * idxFadeUnit
			// Parse fade out
			if fadeValue, err = strconv.Atoi(values[1]); err != nil {
				err = fmt.Errorf("failed to convert fade out to integer: %w", err)
				return
			}
			metadata.FadeOut = time.Duration(fadeValue) * idxFadeUnit
		// Align
		case strings.HasPrefix(line, idxAlignPrefix):
			metadata.Align = line[len(idxAlignPrefix):]
		// Time offset
		case strings.HasPrefix(line, idxTimeOffsetPrefix):
			value := line[len(idxTimeOffsetPrefix):]
			var valueRaw int
			if valueRaw, err = strconv.Atoi(value); err != nil {
				err = fmt.Errorf("failed to convert time offset to integer: %w", err)
				return
			}
			metadata.TimeOffset = time.Duration(valueRaw) * idxTimeOffsetUnit
		// Forced subs
		case strings.HasPrefix(line, idxForcedSubsPrefix):
			value := line[len(idxForcedSubsPrefix):]
			switch value {
			case "ON":
				metadata.ForcedSubs = true
			case "OFF":
			default:
				err = fmt.Errorf("unexpected forced subs value: %q", value)
				return
			}
		// Language Idx
		case strings.HasPrefix(line, idxLangIdxPrefix):
			value := line[len(idxLangIdxPrefix):]
			if metadata.LangIdx, err = strconv.Atoi(value); err != nil {
				err = fmt.Errorf("failed to convert language index to integer: %w", err)
				return
			}
		// Palette
		case strings.HasPrefix(line, idxPalettePrefix):
			value := line[len(idxPalettePrefix):]
			// Extract hexa codes
			values := strings.Split(strings.ReplaceAll(value, ", ", ","), ",") // both separator seen in the wild
			if len(values) != idxPaletteLen {
				err = fmt.Errorf("palette should have 16 colors, currently %d: %v", len(values), values)
				return
			}
			// Create the main alpha channel
			if metadata.AlphaRatio == 0 {
				err = errors.New("current alpha ratio is 0: continuing will produce 100%% transparent subtitles")
				return
			}
			mainAlpha := uint8(255 * metadata.AlphaRatio)
			// Create the colors
			metadata.Palette = make(color.Palette, len(values))
			for index, colorStr := range values {
				if len(colorStr) != 6 {
					err = fmt.Errorf("invalid len for palette color at index #%d (must be 6): %s -> %d",
						index, colorStr, len(colorStr),
					)
					return
				}
				var colorValues []byte
				if colorValues, err = hex.DecodeString(colorStr); err != nil {
					err = fmt.Errorf("failed to decode the palette hex color at index #%d: %w", index, err)
					return
				}
				metadata.Palette[index] = color.RGBA{
					R: colorValues[0],
					G: colorValues[1],
					B: colorValues[2],
					A: mainAlpha,
				}
			}
		default:
			// skip line
		}
	}
	if err = scanner.Err(); err != nil {
		err = fmt.Errorf("error while scanning Idx content: %w", err)
		return
	}
	return
}
