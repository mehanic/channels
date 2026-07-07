
package ts

import (
	"bytes"
	"testing"
)

func makeTestPacket(size int, filler byte) []byte {
	p := make([]byte, size)
	p[0] = SyncByte
	for i := 1; i < size; i++ {
		p[i] = filler
	}
	return p
}

func concatPacket(p1 []byte, p2 []byte) []byte {
	for _, p := range p2 {
		p1 = append(p1, p)
	}
	return p1
}

func TestScanStandardSizePacket(t *testing.T) {
	p1 := makeTestPacket(188, 0x01)
	p2 := makeTestPacket(188, 0x02)
	p3 := makeTestPacket(188, 0x03)
	var p []byte
	p = concatPacket(p, p1)
	p = concatPacket(p, p2)
	p = concatPacket(p, p3)

	s := NewPacketScanner(bytes.NewReader(p))

	s.Scan()
	if !bytes.Equal(s.Bytes(), p1) {
		t.Errorf("got: %v, expected: %v", s.Bytes()[0:2], p1[0:2])
	}

	s.Scan()
	if !bytes.Equal(s.Bytes(), p2) {
		t.Errorf("got: %v, expected: %v", s.Bytes()[0:2], p2[0:2])
	}

	s.Scan()
	if !bytes.Equal(s.Bytes(), p3) {
		t.Errorf("got: %v, expected: %v", s.Bytes()[0:2], p3[0:2])
	}
}

func TestScanExtendedSizePacket(t *testing.T) {
	// mixed size
	p1 := makeTestPacket(204, 0x01)
	p2 := makeTestPacket(188, 0x02)
	p3 := makeTestPacket(204, 0x03)
	var p []byte
	p = concatPacket(p, p1)
	p = concatPacket(p, p2)
	p = concatPacket(p, p3)

	s := NewPacketScanner(bytes.NewReader(p))

	s.Scan()
	if !bytes.Equal(s.Bytes(), p1) {
		t.Errorf("got: %v, expected: %v", s.Bytes()[0:2], p1[0:2])
	}
	if len(s.Bytes()) != 204 {
		t.Errorf("got: %d, expected: %d", len(s.Bytes()), 204)
	}

	s.Scan()
	if !bytes.Equal(s.Bytes(), p2) {
		t.Errorf("got: %v, expected: %v", s.Bytes()[0:2], p2[0:2])
	}
	if len(s.Bytes()) != 188 {
		t.Errorf("got: %d, expected: %d", len(s.Bytes()), 188)
	}

	s.Scan()
	if !bytes.Equal(s.Bytes(), p3) {
		t.Errorf("got: %v, expected: %v", s.Bytes()[0:2], p3[0:2])
	}
	if len(s.Bytes()) != 204 {
		t.Errorf("got: %d, expected: %d", len(s.Bytes()), 204)
	}
}
