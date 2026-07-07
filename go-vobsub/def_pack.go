package vobsub

import (
	"fmt"
	"time"
)

const (
	// SCRFrequency is the System Clock Reference base frequency
	SCRFrequency = 27_000_000 // 27 MHz
	// PTSDTSClockFrequency is the Presentation TimeStamp and Decoding TimeStamp clock base frequency
	PTSDTSClockFrequency = 90_000 // 90 kHz
)

// PackHeader contains the data of the MPEG pack header. It itselfs contains a new MPEG header.
// More informations at https://dvd.sourceforge.net/dvdinfo/packhdr.html
type PackHeader struct {
	MPH       MPEGHeader
	Remaining [10]byte
}

// Validate check if the data within the PackHeader are valid
func (ph PackHeader) Validate() error {
	if err := ph.MPH.Validate(); err != nil {
		return err
	}
	// Validate the PACK identifier
	if ph.MPH[3] != StreamIDPackHeader {
		return fmt.Errorf("invalid PACK identifier: %08b (expected %08b)", ph.MPH[3], StreamIDPackHeader)
	}
	// Check for fixed bits in the SCR 6 first bytes
	if ph.Remaining[0]>>6 != 0b01 {
		return fmt.Errorf("invalid SCR 1st fixed bits: %02b (expected 0b01)", ph.Remaining[0]>>6)
	}
	if (ph.Remaining[0]&0b00000100)>>2 != 0b1 {
		return fmt.Errorf("invalid SCR 2nd fixed bits: %b (expected 0b1)", (ph.Remaining[0]&0b00000100)>>2)
	}
	if (ph.Remaining[2]&0b00000100)>>2 != 0b1 {
		return fmt.Errorf("invalid SCR 3rd fixed bits: %b (expected 0b1)", (ph.Remaining[2]&0b00000100)>>2)
	}
	if (ph.Remaining[4]&0b00000100)>>2 != 0b1 {
		return fmt.Errorf("invalid SCR 4th fixed bits: %b (expected 0b1)", (ph.Remaining[4]&0b00000100)>>2)
	}
	if ph.Remaining[5]&0b00000001 != 0b1 {
		return fmt.Errorf("invalid SCR 5th fixed bits: %b (expected 0b1)", ph.Remaining[5]&0b00000001)
	}
	// Check for fixed bits in the last 4 bytes
	if ph.Remaining[8]&0b00000011 != 0b11 {
		return fmt.Errorf("invalid SCR 5th fixed bits: %02b (expected 0b11)", ph.Remaining[8]&0b00000011)
	}
	// ProgramMuxRate can not be 0
	if ph.ProgramMuxRate() == 0 {
		return fmt.Errorf("program mux rate cannot be 0")
	}
	return nil
}

// SCRRaw yields the raw values of System Clock Reference contains in the pack header
func (ph PackHeader) SCRRaw() (quotient uint64, remainder uint64) {
	// Extract the quotient
	quotient = uint64(ph.Remaining[0]&0b00111000)<<(30-3) | uint64(ph.Remaining[0]&0b00000011)<<28
	quotient |= uint64(ph.Remaining[1]) << 20
	quotient |= uint64(ph.Remaining[2]&0b11111000)<<(15-3) | uint64(ph.Remaining[2]&0b00000011)<<13
	quotient |= uint64(ph.Remaining[3]) << 5
	quotient |= uint64(ph.Remaining[4]) >> 3
	// Extract the remainder
	remainder = uint64(ph.Remaining[4]&0b00000011) << 7
	remainder |= uint64(ph.Remaining[5]) >> 1
	return
}

// SCR returns the the parsed and computed System Clock Reference contained in the pack header
func (ph PackHeader) SCR() time.Duration {
	quotient, remainder := ph.SCRRaw()
	ticks := quotient*(SCRFrequency/PTSDTSClockFrequency) + uint64(remainder)
	return time.Duration(ticks * uint64(time.Second) / SCRFrequency)
}

// ProgramMuxRate is a (originally 22 bits) integer specifying the rate at which the program stream target decoder receives the Program Stream during the pack in which it is included.
// The value of ProgramMuxRate is measured in units of 50 bytes/second. The value 0 is forbidden.
func (ph PackHeader) ProgramMuxRate() uint64 {
	return uint64(ph.Remaining[6])<<(16-2) | uint64(ph.Remaining[7])<<(8-2) | uint64(ph.Remaining[8])>>2
}

// StuffingBytesLength returns the number of padding bytes (0xff) that follows the Pack Header in the stream
func (ph PackHeader) StuffingBytesLength() int64 {
	return int64(ph.Remaining[9] & 0b00000111)
}

// String implements the fmt.Stringer interface.
// It returns a string that represents the value of the receiver in a form suitable for printing.
// See https://pkg.go.dev/fmt#Stringer
func (ph PackHeader) String() string {
	return fmt.Sprintf("PackHeader{%s, SCR: %s, ProgramMuxRate: %d, StuffingBytesLength: %d}",
		ph.MPH, ph.SCR(), ph.ProgramMuxRate(), ph.StuffingBytesLength(),
	)
}

// GoString implements the fmt.GoStringer interface.
// It returns a string that represents the value of the receiver in a form suitable for debugging.
// See https://pkg.go.dev/fmt#GoStringer
func (ph PackHeader) GoString() string {
	return fmt.Sprintf("PackHeader{%s  PackHeader{%08b %08b %08b %08b %08b %08b  %08b %08b %08b  %08b}}",
		ph.MPH.GoString(),
		ph.Remaining[0], ph.Remaining[1], ph.Remaining[2], ph.Remaining[3], ph.Remaining[4], ph.Remaining[5],
		ph.Remaining[6], ph.Remaining[7], ph.Remaining[8], ph.Remaining[9],
	)
}
