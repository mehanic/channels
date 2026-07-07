package vobsub

import (
	"errors"
	"fmt"
	"io"
)

// StreamParsePacket try to read a packet from the stream at the given position and returns any underlying privatestream1 packets found.
// Test the return stream id as it might also encounter a padding stream. Any other streamid will end with an error.
// If no error, nextAt indicate the next packet position to read.
func StreamParsePacket(stream io.ReaderAt, currentPosition int64) (packet PESPacket, nextAt int64, err error) {
	// Read Start code and verify it is a pack header
	var (
		mph    MPEGHeader
		nbRead int
	)
	if nbRead, err = stream.ReadAt(mph[:], currentPosition); err != nil {
		if errors.Is(err, io.EOF) {
			// strange but seen in the wild
			err = nil
			nextAt = -1 // return invalid offset to indicate stop
		} else {
			err = fmt.Errorf("failed to read start code header: %w", err)
		}
		return
	}
	currentPosition += int64(nbRead)
	if err = mph.Validate(); err != nil {
		err = fmt.Errorf("invalid MPEG header: %w", err)
		return
	}
	// Act depending on stream ID
	switch mph.StreamID() {
	case StreamIDPackHeader:
		if packet, nextAt, err = streamParsePackHeader(stream, currentPosition, mph); err != nil {
			err = fmt.Errorf("failed to parse Pack Header: %w", err)
			return
		}
		return
	case StreamIDPaddingStream:
		if nextAt, err = streamParsePaddingStream(stream, currentPosition, mph); err != nil {
			err = fmt.Errorf("failed to parse padding stream: %w", err)
			return
		}
		return
	case StreamIDProgramEnd:
		nextAt = -1 // return invalid offset to indicate stop
		return
	default:
		err = fmt.Errorf("unexpected stream ID: %s", mph.StreamID())
		return
	}
}

func streamParsePackHeader(stream io.ReaderAt, currentPosition int64, mph MPEGHeader) (packet PESPacket, nextPacketPosition int64, err error) {
	var nbRead int
	// Finish reading pack header
	ph := PackHeader{
		MPH: mph,
	}
	if nbRead, err = stream.ReadAt(ph.Remaining[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read pack header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	if err = ph.Validate(); err != nil {
		err = fmt.Errorf("invalid pack header: %w", err)
		return
	}
	currentPosition += ph.StuffingBytesLength()
	// fmt.Println(ph.String())
	// fmt.Println(ph.GoString())
	return parsePESHeader(stream, currentPosition)
}

func parsePESHeader(stream io.ReaderAt, currentPosition int64) (packet PESPacket, nextPacketPosition int64, err error) {
	var nbRead int
	// Read the PES header
	var pes PESHeader
	if nbRead, err = stream.ReadAt(pes.MPH[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	if err = pes.MPH.Validate(); err != nil {
		err = fmt.Errorf("invalid PES header: invalid start code: %w", err)
		return
	}
	if nbRead, err = stream.ReadAt(pes.PacketLength[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES Packet Length header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	nextPacketPosition = currentPosition + int64(pes.GetPacketLength()) // packet len is all data after the header ending with the data len
	// Continue depending on stream ID
	switch pes.MPH.StreamID() {
	case StreamIDPrivateStream1:
		if packet, err = streamParsePESPrivateStream1Packet(stream, currentPosition, pes); err != nil {
			err = fmt.Errorf("failed to parse subtitle stream (private stream 1) packet: %w", err)
			return
		}
		return
	default:
		err = fmt.Errorf("unexpected PES Stream ID: %s", pes.MPH.StreamID())
		return
	}
}

func streamParsePESPrivateStream1Packet(stream io.ReaderAt, currentPosition int64, preHeader PESHeader) (packet PESPacket, err error) {
	var nbRead int
	packet.Header = preHeader
	// Finish reading PES header
	//// 0xBD stream type has PES header extension, read it
	packet.Header.Extension = new(PESExtension)
	if nbRead, err = stream.ReadAt(packet.Header.Extension.Header[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES extension header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	//// Read PES Extension Data
	extensionData := make([]byte, packet.Header.Extension.RemainingHeaderLength())
	if nbRead, err = stream.ReadAt(extensionData, currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES extension data: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	if err = packet.Header.ParseExtensionData(extensionData); err != nil {
		err = fmt.Errorf("failed to parse extension header data: %w", err)
		return
	}
	//// Read sub stream id for private streams
	if nbRead, err = stream.ReadAt(packet.Header.SubStreamID[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read sub stream id: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	//// Headers done
	// fmt.Println(packet.Header.String())
	// fmt.Println(packet.Header.GoString())
	// Payload
	payloadLen := packet.Header.GetPacketLength() - len(packet.Header.Extension.Header) - len(extensionData) - len(packet.Header.SubStreamID)
	packet.Payload = make([]byte, payloadLen)
	if _, err = stream.ReadAt(packet.Payload, currentPosition); err != nil {
		err = fmt.Errorf("failed to read the payload: %w", err)
		return
	}
	return
}

func streamParsePaddingStream(stream io.ReaderAt, currentPosition int64, mph MPEGHeader) (nextPacketPosition int64, err error) {
	var nbRead int
	// Read the PES header used in padding
	pes := PESHeader{
		MPH: mph,
	}
	if nbRead, err = stream.ReadAt(pes.PacketLength[:], currentPosition); err != nil {
		err = fmt.Errorf("failed to read PES Packet Length header: %w", err)
		return
	}
	currentPosition += int64(nbRead)
	nextPacketPosition = currentPosition + int64(pes.GetPacketLength()) // packet len is all data (no extension) after the header ending with the data len
	// // Debug
	// fmt.Println("Padding len:", pes.GetPacketLength())
	// buffer := make([]byte, pes.GetPacketLength())
	// if _, err = stream.ReadAt(buffer, currentPosition); err != nil {
	// 	err = fmt.Errorf("failed to read the payload: %w", err)
	// 	return
	// }
	// for _, b := range buffer {
	// 	fmt.Printf("0x%02x ", b) // all should be 0xff
	// }
	// fmt.Println()
	return
}
