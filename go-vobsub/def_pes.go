package vobsub

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PESPacket represents a Packetized Elementary Stream headers and its payload.
type PESPacket struct {
	Header  PESHeader
	Payload []byte
}

// ExtractSubtitle extract the raw subtitle contained in the PES packet if the pes packet contains a subtitle packet (private stream 1)
func (pesp PESPacket) ExtractSubtitle() (subtitle SubtitleRaw, err error) {
	// Check if the packet is a subtitle packet
	if pesp.Header.MPH.StreamID() != StreamIDPrivateStream1 {
		err = fmt.Errorf("the packet stream ID (%s) does not match the expected private stream 1", pesp.Header.MPH.StreamID())
		return
	}
	return extractRawSubtitle(pesp)
}

// PESHeader represents the headers and associated data of aPacketized Elementary Stream header.
// More infos on https://dvd.sourceforge.net/dvdinfo/pes-hdr.html
type PESHeader struct {
	MPH          MPEGHeader
	PacketLength [2]byte
	Extension    *PESExtension
	SubStreamID  SubStreamID // Only for private streams (StreamID == 0xBD or 0xBF)
}

// Validate check the values of the PESHeader
func (pesh *PESHeader) Validate() (err error) {
	if err = pesh.MPH.Validate(); err != nil {
		return fmt.Errorf("invalid PES header: invalid start code: %w", err)
	}
	if pesh.Extension != nil {
		if err = pesh.Extension.Validate(); err != nil {
			return fmt.Errorf("invalid PES header: invalid extension: %w", err)
		}
	}
	return nil
}

// GetPacketLength returns the parsed packet length contained within the PES header
func (pesh *PESHeader) GetPacketLength() int {
	return int(binary.BigEndian.Uint16(pesh.PacketLength[:]))
}

// ParseExtensionData allows to parse the Extension data after the extensions headers (they must be read already)
// and validate the remaining bytes are padding.
func (pesh *PESHeader) ParseExtensionData(data []byte) (err error) {
	// Parse the data
	index, err := pesh.Extension.ParsePESExtensionData(data)
	if err != nil {
		return fmt.Errorf("failed to parse extension data: %w", err)
	}
	// Check final padding
	for i := index; i < len(data); i++ {
		if data[i] != 0xFF {
			return fmt.Errorf("invalid padding at the end of the extension data")
		}
		// fmt.Println("valid padding yay")
	}
	return
}

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (pesh *PESHeader) String() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("PESHeader{StartCodeHeader: %s, PacketLength: %d, Extension: ",
		pesh.MPH, pesh.GetPacketLength()))
	if pesh.Extension != nil {
		builder.WriteString(pesh.Extension.String())
	} else {
		builder.WriteString("<nil>")
	}
	if pesh.MPH.StreamID() == StreamIDPrivateStream1 ||
		pesh.MPH.StreamID() == StreamIDPrivateStream2 {
		// Private stream, print sub stream id too
		builder.WriteString(fmt.Sprintf(", SubStreamID: 0x%02x", pesh.SubStreamID[0]))
	}
	builder.WriteString("}")
	return builder.String()
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (pesh *PESHeader) GoString() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("PESHeader{StartCodeHeader: %#v, PacketLength: %08b%08b, Extension: ",
		pesh.MPH, byte(pesh.PacketLength[0]), byte(pesh.PacketLength[1])))
	if pesh.Extension != nil {
		builder.WriteString(pesh.Extension.GoString())
	} else {
		builder.WriteString("<nil>")
	}
	if pesh.MPH.StreamID() == StreamIDPrivateStream1 ||
		pesh.MPH.StreamID() == StreamIDPrivateStream2 {
		// Private stream, print sub stream id too
		builder.WriteString(fmt.Sprintf(", SubStreamID: %08b", pesh.SubStreamID[0]))
	}
	builder.WriteString("}")
	return builder.String()
}

/*
	PES Extension
*/

const (
	// pesExtensionMarker is the first two fixed bits the start the PES extension headers
	pesExtensionMarker = 0b10
)

// PESExtension represents the PES extension header and data
type PESExtension struct {
	Header [3]byte
	Data   PESExtensionData
}

// Valid checks if the optional header marker is present at the start of the optional headers.
func (pese *PESExtension) Validate() error {
	if pese.Header[0]>>6 != pesExtensionMarker {
		return fmt.Errorf("invalid PES extension header marker: expected 0x%02X, got 0x%02X",
			pesExtensionMarker, pese.Header[0]>>6)
	}
	// Extension is validated during parsing with ParseExtensionData()
	return nil
}

// ScramblingControl returns the scrambling value. Check the related constants.
func (pese *PESExtension) ScramblingControl() ScramblingControl {
	return ScramblingControl((pese.Header[0] & 0b00110000) >> 4)
}

// Priority returns true if the priority bit is set to 1, indicating that the PES packet has higher priority than other packets in the same PID. false = Normal priority, true = High priority.
func (pese *PESExtension) HighPriority() bool {
	return (pese.Header[0]&0b00001000)>>3 == 1
}

// DataAligned returns true if the data alignment indicator is set to 1, indicating that the payload starts with a byte-aligned elementary stream. false = Not aligned, true = Aligned.
func (pese *PESExtension) DataAligned() bool {
	return (pese.Header[0]&0b00000100)>>2 == 1
}

// Copyright returns true if the copyright bit is set to 1, indicating that the PES packet contains copyrighted material. false = Not copyrighted, true = Copyrighted.
func (pese *PESExtension) Copyright() bool {
	return (pese.Header[0]&0b00000010)>>1 == 1
}

// Original returns true if the original bit is set to 1, indicating that the PES packet contains original material. false = Copy, true = Original.
func (pese *PESExtension) Original() bool {
	return pese.Header[0]&0b00000001 == 1
}

// PTSDTSPresence returns the Presentation Time Stamp and Decode Time Stamp presence
func (pese *PESExtension) PTSDTSPresence() PTSDTSPresence {
	return PTSDTSPresence(pese.Header[1] >> 6)
}

// PTSPresent returns if the Presentation Time Stamp is present
func (pese *PESExtension) PTSPresent() bool {
	return pese.PTSDTSPresence()&JustPTS == 1
}

// DTSPresent returns if the Decode Time Stamp is present
func (pese *PESExtension) DTSPresent() bool {
	return pese.PTSDTSPresence()&JustDTS == 1
}

// ESCRPresent returns if the Elementary Stream Clock Reference is present
func (pese *PESExtension) ESCRPresent() bool {
	return (pese.Header[1]&0b00100000)>>5 == 1
}

// ESRatePresent returns if the ES Rate is present
func (pese *PESExtension) ESRatePresent() bool {
	return (pese.Header[1]&0b00010000)>>4 == 1
}

// DSMTrickMode returns if the DSM Trick Mode flag is set
func (pese *PESExtension) DSMTrickMode() bool {
	return (pese.Header[1]&0b00001000)>>3 == 1
}

// AdditionalCopyInfoPresent returns if Additional Copy Informations are present
func (pese *PESExtension) AdditionalCopyInfoPresent() bool {
	return (pese.Header[1]&0b00000100)>>2 == 1
}

// CRCPresent returns if the previous packet CRC is present
func (pese *PESExtension) CRCPresent() bool {
	return (pese.Header[1]&0b00000010)>>1 == 1
}

// SecondExtensionPresent returns if the second EPS extension is present
func (pese *PESExtension) SecondExtensionPresent() bool {
	return pese.Header[1]&0b00000001 == 1
}

// RemainingHeaderLength returns the remaining length of the PES extension headers after the flags has been parsed and before the payload starts
func (pese *PESExtension) RemainingHeaderLength() int {
	return int(pese.Header[2])
}

const (
	pesExtensionDataPTSSize                          = 5
	pesExtensionDataDTSSize                          = 5
	pesExtensionDataESCRSize                         = 6
	pesExtensionDataESRateSize                       = 3
	pesExtensionDataCRCSize                          = 2
	pesExtensionDataPrivateDataSize                  = 16
	pesExtensionDataProgramPacketSequenceCounterSize = 2
	pesExtensionDataPSTDBufferSize                   = 2
	pesExtensionDataSecondExtensionSize              = 2
)

// ParsePESExtensionData parse raw PES extension data. PES extension headers must have been read and set first.
// Prefer PESHeader ParseExtensionData() high level method which also verify the final padding if present.
func (pese *PESExtension) ParsePESExtensionData(data []byte) (index int, err error) {
	if len(data) != pese.RemainingHeaderLength() {
		err = fmt.Errorf("received data len (%d) does not match expected len (%d)",
			len(data), pese.RemainingHeaderLength())
		return
	}
	// PTSDTS
	if pese.PTSDTSPresence()&JustPTS == JustPTS {
		pese.Data.PTS = make([]byte, pesExtensionDataPTSSize)
		for i := range pesExtensionDataPTSSize {
			pese.Data.PTS[i] = data[index+i]
		}
		// done
		index += pesExtensionDataPTSSize
		// fmt.Println("PTS extracted !")
	}
	if pese.PTSDTSPresence()&JustDTS == JustDTS {
		pese.Data.DTS = make([]byte, pesExtensionDataDTSSize)
		for i := range pesExtensionDataDTSSize {
			pese.Data.DTS[i] = data[index+i]
		}
		// done
		index += pesExtensionDataDTSSize
		// fmt.Println("DTS extracted !")
	}
	// ESCR
	if pese.ESCRPresent() {
		pese.Data.ESCR = make([]byte, pesExtensionDataESCRSize)
		for i := range pesExtensionDataESCRSize {
			pese.Data.ESCR[i] = data[index+i]
		}
		// done
		index += pesExtensionDataESCRSize
		// fmt.Println("ESCR extracted !")
	}
	// ES rate
	if pese.ESRatePresent() {
		pese.Data.ESRate = make([]byte, pesExtensionDataESRateSize)
		for i := range pesExtensionDataESRateSize {
			pese.Data.ESRate[i] = data[index+i]
		}
		// done
		index += pesExtensionDataESRateSize
		// fmt.Println("ESRate extracted !")
	}
	// additional copy info
	if pese.AdditionalCopyInfoPresent() {
		// Check fixed bit
		if data[index]&0b10000000 != 0b10000000 {
			err = errors.New("additionnal copy info fixed bit is invalid")
			return
		}
		// Extract value
		value := data[index] & 0b01111111
		pese.Data.AdditionalCopyInfo = &value
		// done
		index++
		// fmt.Println("Additional Copy Info parsed !")
	}
	// PES CRC
	if pese.CRCPresent() {
		pese.Data.PreviousPacketCRC = make([]byte, pesExtensionDataCRCSize)
		for i := range pesExtensionDataCRCSize {
			pese.Data.PreviousPacketCRC[i] = data[index+i]
		}
		// done
		index += pesExtensionDataCRCSize
		// fmt.Println("ESRate extracted !")
	}
	// PES extension flag
	if !pese.SecondExtensionPresent() {
		return
	}
	headers := data[index]
	index++
	// PES private data flag
	if headers&0b10000000 == 0b10000000 {
		pese.Data.PrivateData = make([]byte, pesExtensionDataPrivateDataSize)
		for i := range pesExtensionDataPrivateDataSize {
			pese.Data.PrivateData[i] = data[index+i]
		}
		index += pesExtensionDataPrivateDataSize
		// fmt.Println("Private Data extracted !")
	}
	// pack header field flag
	if headers&0b01000000 == 0b01000000 {
		value := data[index]
		pese.Data.PackHeaderField = &value
		// fmt.Println("PackHeader field flag set in the PES extension data: unsure of subsequent read") // mmm
		index++
	}
	// program packet sequence counter flag
	if headers&0b00100000 == 0b00100000 {
		pese.Data.ProgramPacketSequenceCounter = make([]byte, pesExtensionDataProgramPacketSequenceCounterSize)
		for i := range pesExtensionDataProgramPacketSequenceCounterSize {
			pese.Data.ProgramPacketSequenceCounter[i] = data[index+i]
		}
		index += pesExtensionDataProgramPacketSequenceCounterSize
		// fmt.Println("program packet sequence counter extracted !")
	}
	// P-STD buffer flag
	if headers&0b00010000 == 0b00010000 {
		pese.Data.PSTD = make([]byte, pesExtensionDataPSTDBufferSize)
		for i := range pesExtensionDataPSTDBufferSize {
			pese.Data.PSTD[i] = data[index+i]
		}
		index += pesExtensionDataPSTDBufferSize
		// fmt.Println("P-STD buffer extracted !")
	}
	// Fixed bytes
	if headers&0b00001110 != 0b00001110 {
		err = fmt.Errorf("PES second extension headers fixed bytes are invalid")
		return
	}
	// PES extension flag 2
	if headers&0b000000001 == 0b000000001 {
		// Parse headers
		header := make([]byte, pesExtensionDataSecondExtensionSize)
		for i := range pesExtensionDataSecondExtensionSize {
			header[i] = data[index+i]
		}
		index += pesExtensionDataSecondExtensionSize
		// Extract data
		additionnalDataLen := int(header[0] & 0b01111111)
		//// header[1] is reserved for futur use
		pese.Data.PESExtensionSecond = make([]byte, additionnalDataLen)
		for i := range additionnalDataLen {
			pese.Data.PSTD[i] = data[index+i]
		}
		index += additionnalDataLen
		// fmt.Println("PES Extension 2 data extracted !")
	}
	return
}

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (pese *PESExtension) String() string {
	if err := pese.Validate(); err != nil {
		return fmt.Sprintf("<invalid PES optional headers: %s>", err)
	}
	return fmt.Sprintf("PESExtension{ScramblingControl: %#v, HighPriority: %v, DataAligned: %v, Copyright: %v, Original: %v, PTSDTSPresence: %#v, ElementaryStreamClockReference: %v, ESRate: %v, DSMTrickMode: %v, AdditionalCopyInfo: %v, CRC: %v, SecondExtension: %v, RemainingHeaderLength: %d}",
		pese.ScramblingControl(), pese.HighPriority(), pese.DataAligned(), pese.Copyright(), pese.Original(),
		pese.PTSDTSPresence(), pese.ESCRPresent(), pese.ESRatePresent(), pese.DSMTrickMode(), pese.AdditionalCopyInfoPresent(), pese.CRCPresent(), pese.SecondExtensionPresent(),
		pese.RemainingHeaderLength(),
	)
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (pese *PESExtension) GoString() string {
	return fmt.Sprintf("%08b %08b %08b", pese.Header[0], pese.Header[1], pese.Header[2]) // TODO data
}

// ScramblingControl, more infos on https://patents.google.com/patent/WO2010000692A1/en
type ScramblingControl byte

const (
	ScramblingControlNotScrambled ScramblingControl = 0b00
	ScramblingControlReserved     ScramblingControl = 0b01 // Reserved as per standard, should not be used.
	ScramblingControlEvenKey      ScramblingControl = 0b10
	ScramblingControlOddKey       ScramblingControl = 0b11
)

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (sc ScramblingControl) String() string {
	switch sc {
	case ScramblingControlNotScrambled:
		return "Not scrambled"
	case ScramblingControlReserved:
		return "Reserved"
	case ScramblingControlEvenKey:
		return "Even key"
	case ScramblingControlOddKey:
		return "Odd key"
	default:
		return "Unknown"
	}
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (sc ScramblingControl) GoString() string {
	return fmt.Sprintf("%s (%02b)", sc.String(), sc)
}

// PTSDTSPresence is the presence of PTS and DTS (or not)
type PTSDTSPresence byte

const (
	NoPTSorDTSPresent PTSDTSPresence = 0b00
	JustDTS           PTSDTSPresence = 0b01 // Forbidden
	JustPTS           PTSDTSPresence = 0b10
	BothPTSandDTS     PTSDTSPresence = 0b11
)

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (ptd PTSDTSPresence) String() string {
	switch ptd {
	case NoPTSorDTSPresent:
		return "No PTS or DTS present"
	case JustDTS:
		return "Just DTS (forbidden)"
	case JustPTS:
		return "Just PTS"
	case BothPTSandDTS:
		return "Both PTS and DTS"
	default:
		return "Unknown"
	}
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (ptd PTSDTSPresence) GoString() string {
	return fmt.Sprintf("%s (%02b)", ptd.String(), ptd)
}

/*
	PES Extension data
*/

// PESExtensionData is the dynamic data contained in the PES extension
type PESExtensionData struct {
	PTS                []byte // Presentation Time Stamp
	DTS                []byte // Decode Time Stamp
	ESCR               []byte // Elementary Stream Clock Reference (used if the stream and system levels are not synchronized (i.e. ESCR differs from SCR in the PACK header))
	ESRate             []byte // The rate at which data is delivered for this stream, in units of 50 bytes/second
	AdditionalCopyInfo *byte  // unknown format
	PreviousPacketCRC  []byte // The polynomial used is X(16) + X(12) + X(5) + 1
	// Second extension
	PrivateData                  []byte
	PackHeaderField              *byte
	ProgramPacketSequenceCounter []byte
	PSTD                         []byte
	PESExtensionSecond           []byte
}

// ComputePTS computes the Presentation Time Stamp value
func (pesed *PESExtensionData) ComputePTS() (pts time.Duration) {
	if len(pesed.PTS) == 0 {
		return
	}
	var ticks uint64
	ticks |= (uint64(pesed.PTS[0]&0b00001110) >> 1) << 30
	ticks |= uint64(pesed.PTS[1]) << 22
	ticks |= (uint64(pesed.PTS[2]&0b11111110) >> 1) << 15
	ticks |= uint64(pesed.PTS[3]) << 7
	ticks |= uint64(pesed.PTS[4]&0b11111110) >> 1
	return time.Duration(ticks * uint64(time.Second) / PTSDTSClockFrequency)
}

// ComputeDTS computes the Decode Time Stamp value
func (pesed *PESExtensionData) ComputeDTS() (pts time.Duration) {
	if len(pesed.PTS) == 0 {
		return
	}
	var ticks uint64
	ticks |= (uint64(pesed.PTS[0]&0b00001110) >> 1) << 30
	ticks |= uint64(pesed.PTS[1]) << 22
	ticks |= (uint64(pesed.PTS[2]&0b11111110) >> 1) << 15
	ticks |= uint64(pesed.PTS[3]) << 7
	ticks |= uint64(pesed.PTS[4]&0b11111110) >> 1
	return time.Duration(ticks * uint64(time.Second) / PTSDTSClockFrequency)
}

/*
	SubStreamID for private streams
*/

const (
	// SubStreamIDBaseValue is the base value used by all sub stream IDs
	SubStreamIDBaseValue = 0x20
)

// SubStreamID represents a sub stream ID in a PES packet (only for private streams)
type SubStreamID [1]byte

// SubtitleID returns the actual subtitle stream ID by subtracting the base value
func (ssid SubStreamID) SubtitleID() int {
	return int(ssid[0]) - SubStreamIDBaseValue
}
