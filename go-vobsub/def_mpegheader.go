package vobsub

import (
	"encoding/binary"
	"fmt"
)

const (
	// StartCodeMarker is the 3 bytes values that mark the beginning of a MPEG header, Pack header and PES header.
	StartCodeMarker = 0x000001

	// StreamIDPackHeader is the ID of a Pack header
	StreamIDPackHeader = 0xBA
	// StreamIDPrivateStream1 is the ID of a Private Stream 1
	StreamIDPrivateStream1 = 0xBD
	// StreamIDPaddingStream is the ID of a Padding Stream
	StreamIDPaddingStream = 0xBE
	// StreamIDPrivateStream2 is the ID of a Private Stream 2
	StreamIDPrivateStream2 = 0xBF
	// StreamIDProgramEnd is the ID marking the end of a stream
	StreamIDProgramEnd = 0xB9
)

// MPEGHeader represents the top level header encountered in a packetized MPEG stream.
// See https://dvd.sourceforge.net/dvdinfo/mpeghdrs.html for more informations.
type MPEGHeader [4]byte

// Validate verifies the content of the header
func (mph MPEGHeader) Validate() error {
	if binary.BigEndian.Uint32(mph[:])>>8 != StartCodeMarker {
		return fmt.Errorf("invalid start code marker: %x (expected %x)", mph, StartCodeMarker)
	}
	return nil
}

// StreamID returns the Stream ID contained within the header
func (mph MPEGHeader) StreamID() StreamID {
	return StreamID(mph[3])
}

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (mph MPEGHeader) String() string {
	return fmt.Sprintf("StartCodeHeader{Marker: %06x, StreamID: %s}", binary.BigEndian.Uint32(mph[:])>>8, mph.StreamID())
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (mph MPEGHeader) GoString() string {
	return fmt.Sprintf("StartCodeHeader{Marker:%06b StreamID: 0x%02X}", binary.BigEndian.Uint32(mph[:])>>8, byte(mph.StreamID()))
}

// StreamID represents a Stream ID
type StreamID byte

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (sid StreamID) String() string {
	switch {
	case sid == 0x00: // https://dvd.sourceforge.net/dvdinfo/mpeghdrs.html#picture
		return "Picture"
	case sid >= 0x01 && sid <= 0xAF:
		return "slice"
	case sid == 0xB0 || sid == 0xB1:
		return "reserved"
	case sid == 0xB2:
		return "user private"
	case sid == 0xB3: // https://dvd.sourceforge.net/dvdinfo/mpeghdrs.html#seq
		return "Sequence header"
	case sid == 0xB4:
		return "sequence error"
	case sid == 0xB5: // https://dvd.sourceforge.net/dvdinfo/mpeghdrs.html#ext
		return "extension"
	case sid == 0xB6:
		return "reserved"
	case sid == 0xB7:
		return "sequence end"
	case sid == 0xB8: // https://dvd.sourceforge.net/dvdinfo/mpeghdrs.html#gop
		return "Group of Pictures"
	case sid == StreamIDProgramEnd:
		return "Program end"
	case sid == StreamIDPackHeader: // https://dvd.sourceforge.net/dvdinfo/packhdr.html
		return "Pack header"
	case sid == 0xBB:
		return "System Header"
	case sid == 0xBC:
		return "Program Stream Map"
	case sid == StreamIDPrivateStream1: // https://dvd.sourceforge.net/dvdinfo/pes-hdr.html
		return "Private stream 1"
	case sid == StreamIDPaddingStream: // https://dvd.sourceforge.net/dvdinfo/pes-hdr.html
		return "Padding stream"
	case sid == StreamIDPrivateStream2: // https://dvd.sourceforge.net/dvdinfo/pes-hdr.html
		return "Private stream 2"
	case sid >= 0xC0 && sid <= 0xDF: // https://dvd.sourceforge.net/dvdinfo/pes-hdr.html
		return "MPEG-1 or MPEG-2 audio stream"
	case sid >= 0xE0 && sid <= 0xEF: // https://dvd.sourceforge.net/dvdinfo/pes-hdr.html
		return "MPEG-1 or MPEG-2 video stream"
	case sid == 0xF0:
		return "ECM Stream"
	case sid == 0xF1:
		return "EMM Stream"
	case sid == 0xF2:
		return "ITU-T Rec. H.222.0 | ISO/IEC 13818-1 Annex A or ISO/IEC 13818-6_DSMCC_stream"
	case sid == 0xF3:
		return "ISO/IEC_13522_stream"
	case sid == 0xF4:
		return "ITU-T Rec. H.222.1 type A"
	case sid == 0xF5:
		return "ITU-T Rec. H.222.1 type B"
	case sid == 0xF6:
		return "ITU-T Rec. H.222.1 type C"
	case sid == 0xF7:
		return "ITU-T Rec. H.222.1 type D"
	case sid == 0xF8:
		return "ITU-T Rec. H.222.1 type E"
	case sid == 0xF9:
		return "ancillary_stream"
	case sid >= 0xFA && sid <= 0xFE:
		return "reserved"
	case sid == 0xFF:
		return "Program Stream Directory"
	default:
		return "<unknown stream ID>"
	}
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (sid StreamID) GoString() string {
	return fmt.Sprintf("%s (%02X)", sid, sid)
}
