package python

import (
	"bytes"
	"slices"
	"testing"

	"github.com/dobbo-ca/gositter"
)

func roundTrip(t *testing.T, s *scanner) (*scanner, []byte) {
	t.Helper()
	buf := make([]byte, gositter.SerializationBufferSize)
	n := s.Serialize(buf)
	// Dirty target: Deserialize must replace all of it.
	got := &scanner{indents: []uint16{9, 9}, delimiters: []byte{0xFF}, insideInterpolatedString: true}
	got.Deserialize(buf[:n])
	buf2 := make([]byte, gositter.SerializationBufferSize)
	if n2 := got.Serialize(buf2); !bytes.Equal(buf[:n], buf2[:n2]) {
		t.Fatalf("serialize after deserialize differs:\n%v\n%v", buf[:n], buf2[:n2])
	}
	return got, buf[:n]
}

func TestSerializeBytes(t *testing.T) {
	s := newScanner().(*scanner)
	buf := make([]byte, gositter.SerializationBufferSize)
	if n := s.Serialize(buf); !bytes.Equal(buf[:n], []byte{0, 0}) {
		t.Fatalf("fresh scanner = %v, want [0 0]", buf[:n])
	}

	s.indents = []uint16{0, 4, 8, 0x1234}
	s.delimiters = []byte{flagDoubleQuote | flagFormat, flagSingleQuote | flagTriple | flagRaw}
	s.insideInterpolatedString = true
	got, b := roundTrip(t, s)
	// inside, count, delimiters, then indents[1:] little-endian.
	want := []byte{1, 2, 0x12, 0x29, 4, 0, 8, 0, 0x34, 0x12}
	if !bytes.Equal(b, want) {
		t.Fatalf("bytes = %v, want %v", b, want)
	}
	if !slices.Equal(got.indents, s.indents) || !bytes.Equal(got.delimiters, s.delimiters) || !got.insideInterpolatedString {
		t.Fatalf("state = %+v, want %+v", got, s)
	}
}

func TestDeserializeEmpty(t *testing.T) {
	s := &scanner{indents: []uint16{0, 4}, delimiters: []byte{1}, insideInterpolatedString: true}
	s.Deserialize(nil)
	if !slices.Equal(s.indents, []uint16{0}) || len(s.delimiters) != 0 {
		t.Fatalf("state = %+v", s)
	}
	// C does not touch inside_interpolated_string when length is 0.
	if !s.insideInterpolatedString {
		t.Fatal("insideInterpolatedString was reset")
	}
}

func TestSerializeLimits(t *testing.T) {
	// 255 delimiters at most; the rest is dropped.
	s := newScanner().(*scanner)
	s.delimiters = bytes.Repeat([]byte{flagDoubleQuote}, 300)
	got, b := roundTrip(t, s)
	if b[1] != 255 || len(b) != 2+255 || len(got.delimiters) != 255 {
		t.Fatalf("len = %d, count byte = %d", len(b), b[1])
	}

	// Even header: C fills the buffer exactly, 511 indents.
	s = newScanner().(*scanner)
	for i := 1; i <= 2000; i++ {
		s.indents = append(s.indents, uint16(i))
	}
	got, b = roundTrip(t, s)
	if len(b) != 1024 || len(got.indents) != 512 || got.indents[511] != 511 {
		t.Fatalf("len = %d, indents = %d", len(b), len(got.indents))
	}

	// Odd header: C overruns by one byte here. The port stops at 1023.
	s.delimiters = []byte{flagDoubleQuote}
	if _, b = roundTrip(t, s); len(b) != 1023 {
		t.Fatalf("len = %d, want 1023", len(b))
	}
}
