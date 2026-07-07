package vobsub

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"time"
)

// Subtitle is the final, high level, representation of a subtitle
type Subtitle struct {
	Start time.Duration
	Stop  time.Duration
	Image image.Image
}

const (
	subtitleHeaderLength                  = 2
	subtitleHeaderDataLength              = 2
	subtitleHeadersTotalLen               = subtitleHeaderLength + subtitleHeaderDataLength
	subtitleCTRLSeqDateLen                = 2
	subtitleCTRLSeqCmdForceDisplaying     = 0x00
	subtitleCTRLSeqCmdStartDate           = 0x01
	subtitleCTRLSeqCmdStopDate            = 0x02
	subtitleCTRLSeqCmdPalette             = 0x03
	subtitleCTRLSeqCmdPaletteArgsLen      = 2
	subtitleCTRLSeqCmdAlphaChannel        = 0x04
	subtitleCTRLSeqCmdAlphaChannelArgsLen = 2
	subtitleCTRLSeqCmdAlphaChannelRatio   = float64(1) / float64(16) // Alphas levels are encoded on 4 bits : 0 (transparent) to 15 (opaque)
	subtitleCTRLSeqCmdCoordinates         = 0x05
	subtitleCTRLSeqCmdCoordinatesArgsLen  = 6
	subtitleCTRLSeqCmdRLEOffsets          = 0x06
	subtitleCTRLSeqCmdRLEOffsetsArgsLen   = 4
	subtitleCTRLSeqCmdEnd                 = 0xff
)

// SubtitleRaw struct contains the raw data of a subtitle and its associated control sequences.
// It is used to decode the subtitle into an image.
type SubtitleRaw struct {
	Data             []byte
	ControlSequences []ControlSequence
}

// Decode method takes the subtitle metadata and a boolean flag to determine if full-size images should be generated.
// It returns an image of the subtitle, its start delay, stop delay, and any errors that occurred during decoding.
func (sr SubtitleRaw) Decode(metadata IdxMetadata, fullSize bool) (img image.Image, startDelay, stopDelay time.Duration, err error) {
	// Consolidate rendering metadata
	var (
		paletteColors *ControlSequencePalette
		alphaChannels *ControlSequenceAlphaChannels
		coordinates   *ControlSequenceCoordinates
		RLEOffsets    *ControlSequenceRLEOffsets
	)
	for _, cs := range sr.ControlSequences {
		if cs.StartDate {
			startDelay = cs.Date.GetDelay()
		} else if cs.StopDate {
			stopDelay = cs.Date.GetDelay()
		}
		if cs.PaletteColors != nil {
			paletteColors = cs.PaletteColors
		}
		if cs.AlphaChannels != nil {
			alphaChannels = cs.AlphaChannels
		}
		if cs.Coordinates != nil {
			coordinates = cs.Coordinates
		}
		if cs.RLEOffsets != nil {
			RLEOffsets = cs.RLEOffsets
		}
	}
	if paletteColors == nil {
		err = fmt.Errorf("missing palette colors ids in subtitle")
		return
	}
	if alphaChannels == nil {
		err = fmt.Errorf("missing alpha channels ids in subtitle")
		return
	}
	if coordinates == nil {
		err = fmt.Errorf("missing coordinates in subtitle")
		return
	}
	if RLEOffsets == nil {
		err = fmt.Errorf("missing RLE offsets in subtitle")
		return
	}
	// Adjust the palette
	palette := make(color.Palette, 4)
	colorsIdx := paletteColors.GetIDs()
	alphaRatio := alphaChannels.GetRatios()
	for i := range 4 {
		r, g, b, a := metadata.Palette[colorsIdx[i]].RGBA()
		a = uint32(float64(a) * alphaRatio[i])
		palette[i] = color.RGBA{
			R: uint8(r),
			G: uint8(g),
			B: uint8(b),
			A: uint8(a),
		}
	}
	// Create the subtitle image
	coord := coordinates.Get()
	subtitleImg := image.NewRGBA(image.Rect(coord.Point1.X, coord.Point1.Y, coord.Point2.X, coord.Point2.Y))
	firstLineOffset, secondLineOffset := RLEOffsets.Get()
	//// odd lines
	iter := &nibbleIterator{
		data: sr.Data[firstLineOffset:secondLineOffset],
	}
	if err = drawOddOrEvenLines(subtitleImg, palette, iter, false); err != nil {
		err = fmt.Errorf("failed to draw even lines: %w", err)
		return
	}
	//// even lines
	iter = &nibbleIterator{
		data: sr.Data[secondLineOffset:],
	}
	if err = drawOddOrEvenLines(subtitleImg, palette, iter, true); err != nil {
		err = fmt.Errorf("failed to draw even lines: %w", err)
		return
	}
	if !fullSize {
		img = subtitleImg
		return
	}
	// Place the image within the full size screen (and apply idx offset if any)
	fullSizeImg := image.NewRGBA(image.Rect(0, 0, metadata.Width, metadata.Height))
	targetZone := image.Rectangle{
		Min: image.Point{
			X: metadata.Origin.X,
			Y: metadata.Origin.Y,
		},
		Max: image.Point{
			X: metadata.Width,
			Y: metadata.Height,
		},
	}
	draw.Draw(fullSizeImg, targetZone, subtitleImg, image.Point{}, draw.Src)
	img = fullSizeImg
	return
}

type ControlSequence struct {
	Date            ControlSequenceDate
	ForceDisplaying bool
	StartDate       bool
	StopDate        bool
	PaletteColors   *ControlSequencePalette
	AlphaChannels   *ControlSequenceAlphaChannels
	Coordinates     *ControlSequenceCoordinates
	RLEOffsets      *ControlSequenceRLEOffsets
}

type ControlSequenceDate [subtitleCTRLSeqDateLen]byte

// GetDelay convert the control sequence date to the actual delay it represents
func (csd ControlSequenceDate) GetDelay() time.Duration {
	return time.Duration(int(csd[0])<<8|int(csd[1])) * (time.Second / 100)
}

type ControlSequencePalette [subtitleCTRLSeqCmdPaletteArgsLen]byte

// GetPaletteIDs returns the 4 palette IDs colors that are used by the subtitle
func (csp ControlSequencePalette) GetIDs() (colorsIdx [4]uint8) {
	colorsIdx[3] = uint8(csp[0] & 0b11110000 >> 4)
	colorsIdx[2] = uint8(csp[0] & 0b00001111)
	colorsIdx[1] = uint8(csp[1] & 0b11110000 >> 4)
	colorsIdx[0] = uint8(csp[1] & 0b00001111)
	return
}

type ControlSequenceAlphaChannels [subtitleCTRLSeqCmdAlphaChannelArgsLen]byte

// GetAlphaChannelRatios return the ratios of the alpha channels used by the 4 colors of the subtitle.
// 0 means full transparent, 1 means 100% opaque (actually 100% of the maximum opacity defined in the idx file, often 100% itself)
func (csac ControlSequenceAlphaChannels) GetRatios() (alphas [4]float64) {
	alphas[3] = float64(int(csac[0]&0b11110000>>4)) * subtitleCTRLSeqCmdAlphaChannelRatio
	alphas[2] = float64(int(csac[0]&0b00001111)) * subtitleCTRLSeqCmdAlphaChannelRatio
	alphas[1] = float64(int(csac[1]&0b11110000>>4)) * subtitleCTRLSeqCmdAlphaChannelRatio
	alphas[0] = float64(int(csac[1]&0b00001111)) * subtitleCTRLSeqCmdAlphaChannelRatio
	return
}

type ControlSequenceCoordinates [subtitleCTRLSeqCmdCoordinatesArgsLen]byte

// GetCoordinates returns the coordinates of the subtitle canvea on the screen : x1, x2, y1, y2
func (csc ControlSequenceCoordinates) Get() (coord SubtitlesWindow) {
	coord.Point1.X = int(csc[0])<<4 | int(csc[1]&0b11110000)>>4
	coord.Point2.X = int(csc[1]&0b00001111)<<8 | int(csc[2])
	coord.Point1.Y = int(csc[3])<<4 | int(csc[4]&0b11110000)>>4
	coord.Point2.Y = int(csc[4]&0b00001111)<<8 | int(csc[5])
	return
}

type ControlSequenceRLEOffsets [subtitleCTRLSeqCmdRLEOffsetsArgsLen]byte

func (csrleo ControlSequenceRLEOffsets) Get() (firstLineOffset int, secondLineOffset int) {
	// original offset is from the beginning of the paquet but we stripped the 2 headers from the data payload
	// during parsing (with extractRawSubtitle()) so we need to remove the headers length to get the correct offsets
	firstLineOffset = (int(csrleo[0])<<8 | int(csrleo[1])) - subtitleHeadersTotalLen
	secondLineOffset = (int(csrleo[2])<<8 | int(csrleo[3])) - subtitleHeadersTotalLen
	return
}

func (cs ControlSequence) String() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Delay: %s", cs.Date.GetDelay()))
	// Force Displaying
	if cs.ForceDisplaying {
		builder.WriteString(" | Force Displaying")
	}
	// Start Date
	if cs.StartDate {
		builder.WriteString(" | StartDate")
	}
	// Stop Date
	if cs.StopDate {
		builder.WriteString(" | StopDate")
	}
	// Palette
	if cs.PaletteColors != nil {
		colors := cs.PaletteColors.GetIDs()
		builder.WriteString(
			fmt.Sprintf(" | Palette: color1(%d) color2(%d) color3(%d) color4(%d)",
				colors[0], colors[1], colors[2], colors[3],
			),
		)
	}
	// AlphaChannel
	if cs.AlphaChannels != nil {
		alphas := cs.AlphaChannels.GetRatios()
		builder.WriteString(
			fmt.Sprintf(" | AlphaChannels: color1(%f) color2(%f) color3(%f) color4(%f)",
				alphas[0], alphas[1], alphas[2], alphas[3],
			),
		)
	}
	// Coordinates
	if cs.Coordinates != nil {
		coord := cs.Coordinates.Get()
		builder.WriteString(
			fmt.Sprintf(" | Coordinates: x1(%d) x2(%d) y1(%d) y2(%d)",
				coord.Point1.X, coord.Point2.X, coord.Point1.Y, coord.Point2.Y,
			),
		)
		width, length := coord.Size()
		builder.WriteString(
			fmt.Sprintf(" size(%dx%d)",
				width, length,
			),
		)
	}
	// RLE Offsets
	if cs.RLEOffsets != nil {
		firstLineOffset, secondLineOffset := cs.RLEOffsets.Get()
		builder.WriteString(
			fmt.Sprintf(" | RLE Offsets: 1st(%d) 2nd(%d)", firstLineOffset, secondLineOffset),
		)
	}
	return builder.String()
}

type SubtitlesWindow struct {
	Point1, Point2 image.Point
}

func (coord SubtitlesWindow) Size() (width, height int) {
	return coord.Point2.X - coord.Point1.X + 1, coord.Point2.Y - coord.Point1.Y + 1
}

/*
	Extract helpers
*/

func extractRawSubtitle(packet PESPacket) (subtitle SubtitleRaw, err error) {
	// Read the size first (size includes total header len)
	size := int(packet.Payload[0])<<8 | int(packet.Payload[1])
	// fmt.Printf("Packet len: 0b%08b 0b%08b -> %d\n", packet.Payload[0], packet.Payload[1], size)
	if size != len(packet.Payload) {
		err = fmt.Errorf("the packet size header value (%d) does not match the received packet length (%d)", size, len(packet.Payload))
		return
	}
	// Read the data packet size in order to split the data and the control sequences (size include the data header len)
	dataSize := int(packet.Payload[2])<<8 | int(packet.Payload[3])
	// fmt.Printf("Data Packet len: 0b%08b 0b%08b -> %d\n", packet.Payload[2], packet.Payload[3], dataSize)
	if dataSize > len(packet.Payload)-subtitleHeaderLength {
		fmt.Println(dataSize, len(packet.Payload))
		err = fmt.Errorf("the data packet size header value (%d) exceeds the total packet data size (%d)", size, len(packet.Payload))
		return
	}
	// Handle subtitle data and control sequences
	subtitle.Data = packet.Payload[subtitleHeadersTotalLen:dataSize]
	if subtitle.ControlSequences, err = parseCTRLSeqs(packet.Payload[dataSize:], dataSize); err != nil {
		err = fmt.Errorf("failed to parse control sequences: %w", err)
		return
	}
	return
}

func parseCTRLSeqs(sequences []byte, baseOffset int) (ctrlSeqs []ControlSequence, err error) {
	ctrlSeqs = make([]ControlSequence, 0, 2) // most subtitles will have 2 ctrl sequences: the first with coordinates, palette, etc... and the second with the stop date
	nbSeqs := 0
	nextStart := 0
	nextOffset := 0
	read := 0
	var ctrlSeq ControlSequence
	for {
		nbSeqs++
		if ctrlSeq, nextOffset, read, err = parseCTRLSeq(sequences[nextStart:]); err != nil {
			err = fmt.Errorf("failed to parse control seq #%d: %w", nbSeqs, err)
			return
		}
		ctrlSeqs = append(ctrlSeqs, ctrlSeq)
		if (nextOffset - baseOffset) == nextStart {
			// next offset is ourself, meaning we are the last control seq
			nextStart += read
			break
		}
		nextStart = nextOffset - baseOffset
	}
	for i := nextStart; i < len(sequences); i++ {
		if sequences[i] != 0xff {
			err = errors.New("control sequences post commands bytes are not padding")
			return
		}
	}
	return
}

func parseCTRLSeq(sequences []byte) (cs ControlSequence, nextOffset, index int, err error) {
	if len(sequences) < 4 {
		err = fmt.Errorf("can not parse sequence: current index is %d and sequence length is %d: need at least 4 bytes to read date and next offset",
			index, len(sequences),
		)
		return
	}
	// Extract date
	cs.Date = [subtitleCTRLSeqDateLen]byte{
		sequences[0],
		sequences[1],
	}
	// Extract next sequence offset
	nextOffset = int(sequences[2])<<8 | int(sequences[3])
	// Read commands
	index = 4
	for {
		if index >= len(sequences) {
			err = fmt.Errorf("can not read sequence command: index is %d and sequences length is %d: need at least one byte to read the command",
				index, len(sequences),
			)
			return
		}
		cmd := sequences[index]
		index++
		switch cmd {
		case subtitleCTRLSeqCmdForceDisplaying:
			cs.ForceDisplaying = true
		case subtitleCTRLSeqCmdStartDate:
			cs.StartDate = true
		case subtitleCTRLSeqCmdStopDate:
			cs.StopDate = true
		case subtitleCTRLSeqCmdPalette:
			if index+subtitleCTRLSeqCmdPaletteArgsLen > len(sequences) {
				err = fmt.Errorf("can not read palette command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdPaletteArgsLen,
				)
				return
			}
			cs.PaletteColors = new(ControlSequencePalette)
			for i := range subtitleCTRLSeqCmdPaletteArgsLen {
				cs.PaletteColors[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdPaletteArgsLen
		case subtitleCTRLSeqCmdAlphaChannel:
			if index+subtitleCTRLSeqCmdAlphaChannelArgsLen > len(sequences) {
				err = fmt.Errorf("can not read alpha channel command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdAlphaChannelArgsLen,
				)
				return
			}
			cs.AlphaChannels = new(ControlSequenceAlphaChannels)
			for i := range subtitleCTRLSeqCmdAlphaChannelArgsLen {
				cs.AlphaChannels[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdAlphaChannelArgsLen
		case subtitleCTRLSeqCmdCoordinates:
			if index+subtitleCTRLSeqCmdCoordinatesArgsLen > len(sequences) {
				err = fmt.Errorf("can not read coordinates command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdCoordinatesArgsLen,
				)
				return
			}
			cs.Coordinates = new(ControlSequenceCoordinates)
			for i := range subtitleCTRLSeqCmdCoordinatesArgsLen {
				cs.Coordinates[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdCoordinatesArgsLen
		case subtitleCTRLSeqCmdRLEOffsets:
			if index+subtitleCTRLSeqCmdRLEOffsetsArgsLen > len(sequences) {
				err = fmt.Errorf("can not read RLE offsets command: index is %d and sequences length is %d: need at least %d bytes to read the command arguments",
					index, len(sequences), subtitleCTRLSeqCmdRLEOffsetsArgsLen,
				)
				return
			}
			cs.RLEOffsets = new(ControlSequenceRLEOffsets)
			for i := range subtitleCTRLSeqCmdRLEOffsetsArgsLen {
				cs.RLEOffsets[i] = sequences[index+i]
			}
			index += subtitleCTRLSeqCmdRLEOffsetsArgsLen
		case subtitleCTRLSeqCmdEnd:
			return
		default:
			err = fmt.Errorf("unknown command: 0x%02x", cmd)
			return
		}
	}
}

func drawOddOrEvenLines(rgbaImg *image.RGBA, palette color.Palette, iter *nibbleIterator, evenLines bool) (err error) {
	bounds := rgbaImg.Bounds()
	var (
		relativeX, relativeY int
		absoluteX, absoluteY int
		length               int
		pixel                rlePixel
	)
	if evenLines {
		relativeY = 1
	}
	absoluteY = bounds.Min.Y + relativeY
	for !iter.Ended() {
		if pixel, err = decodeRLE(iter); err != nil {
			return
		}
		if length = int(pixel.repeat); length == 0 {
			// going until the end of line
			length = bounds.Max.X - (bounds.Min.X + relativeX) + 1
		}
		for range length {
			absoluteX = bounds.Min.X + relativeX
			rgbaImg.Set(absoluteX, absoluteY, palette[pixel.color])
			if absoluteX == bounds.Max.X {
				// Need a new line for next pixel
				relativeX = 0
				relativeY += 2
				absoluteY = bounds.Min.Y + relativeY
				if absoluteY > bounds.Max.Y {
					return
				}
				iter.Align() // align decoder if needed for new line
				break        // discard any repetition remaining (cause we reached the end of the line)
			}
			relativeX++
		}
	}
	return
}

func decodeRLE(nibbles *nibbleIterator) (p rlePixel, err error) {
	// 1 nibble letters:  rrcc
	// 2 nibbles letters: 00rr rrcc
	// 3 nibbles letters: 0000 rrrr rrcc
	// 4 nibbles letters: 0000 00rr rrrr rrcc
	var firstNibble, secondNibble, thirdNibble, fourthNibble byte
	var ok bool
	if firstNibble, ok = nibbles.Next(); !ok {
		err = errors.New("no more data")
		return
	}
	if firstNibble&0b1100 != 0 {
		// 1 nibble letter
		p.repeat = (firstNibble & 0b1100) >> 2
		p.color = firstNibble & 0b0011
		return
	}
	// 3 possibilities left, all requiring a second nibble
	if secondNibble, ok = nibbles.Next(); !ok {
		if firstNibble != 0 {
			err = fmt.Errorf("missing second nibble after 0b%04b", firstNibble)
		}
		return
	}
	if firstNibble != 0 {
		// 2 nibbles letter
		p.repeat = (firstNibble&0b0011)<<2 | (secondNibble&0b1100)>>2
		p.color = secondNibble & 0b0011
		return
	}
	// 2 possibilities left, both requiring a third nibble
	if thirdNibble, ok = nibbles.Next(); !ok {
		if firstNibble != 0 || secondNibble != 0 {
			err = fmt.Errorf("missing third nibble after 0b%04b 0b%04b", firstNibble, secondNibble)
		}
		return
	}
	if secondNibble&0b1100 != 0 {
		// 3 nibbles letter
		p.repeat = secondNibble<<2 | (thirdNibble&0b1100)>>2
		p.color = thirdNibble & 0b0011
		return
	}
	// 4 nibbles letter
	if fourthNibble, ok = nibbles.Next(); !ok {
		if firstNibble != 0 || secondNibble != 0 || thirdNibble != 0 {
			err = fmt.Errorf("missing fourth nibble after 0b%04b 0b%04b 0b%04b", firstNibble, secondNibble, thirdNibble)
		}
		return
	}
	p.repeat = secondNibble<<6 | thirdNibble<<2 | (fourthNibble&0b1100)>>2
	p.color = fourthNibble & 0b0011
	return
}

type nibbleIterator struct {
	data []byte
	// instructions for next read
	index   int
	readLow bool
}

func (ni *nibbleIterator) Next() (nibble byte, ok bool) {
	if ni.Ended() {
		return
	}
	ok = true
	if !ni.readLow {
		// First read at index
		nibble = (ni.data[ni.index] & 0b11110000) >> 4
	} else {
		// Second read at index
		nibble = (ni.data[ni.index] & 0b00001111)
		ni.index++
	}
	ni.readLow = !ni.readLow
	return
}

func (ni *nibbleIterator) Align() {
	if ni.readLow {
		ni.index++
		ni.readLow = false
	}
}

func (ni *nibbleIterator) Ended() bool {
	return ni.index >= len(ni.data)
}

type rlePixel struct {
	color  uint8 // only 4 values are used: 0x00, 0x01, 0x02, 0x03
	repeat uint8
}
