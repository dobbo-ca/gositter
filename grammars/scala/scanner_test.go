package scala

import (
	"bytes"
	"slices"
	"testing"

	"github.com/dobbo-ca/gositter"
)

func serialize(s *scanner) []byte {
	buf := make([]byte, gositter.SerializationBufferSize)
	return buf[:s.Serialize(buf)]
}

func roundTrip(t *testing.T, s *scanner) (*scanner, []byte) {
	t.Helper()
	b := serialize(s)
	// Dirty target: Deserialize must replace all of it.
	got := &scanner{indents: []int16{9, 9}, lastIndentationSize: 7, lastNewlineCount: 7, lastColumn: 7}
	got.Deserialize(b)
	if b2 := serialize(got); !bytes.Equal(b, b2) {
		t.Fatalf("serialize after deserialize differs:\n%v\n%v", b, b2)
	}
	return got, b
}

func TestSerializeBytes(t *testing.T) {
	// create() leaves -1, 0, -1 and no indents.
	if b := serialize(newScanner().(*scanner)); !bytes.Equal(b, []byte{0xFF, 0xFF, 0, 0, 0xFF, 0xFF}) {
		t.Fatalf("fresh scanner = %v", b)
	}

	s := &scanner{indents: []int16{0, 2, 0x1234, -2}, lastIndentationSize: 4, lastNewlineCount: -32768, lastColumn: 0x0102}
	got, b := roundTrip(t, s)
	// Three int16 then the indents, all little-endian.
	want := []byte{4, 0, 0, 0x80, 2, 1, 0, 0, 2, 0, 0x34, 0x12, 0xFE, 0xFF}
	if !bytes.Equal(b, want) {
		t.Fatalf("bytes = %v, want %v", b, want)
	}
	if !slices.Equal(got.indents, s.indents) || got.lastIndentationSize != 4 ||
		got.lastNewlineCount != -32768 || got.lastColumn != 0x0102 {
		t.Fatalf("state = %+v, want %+v", got, s)
	}
}

func TestDeserializeEmpty(t *testing.T) {
	s := &scanner{indents: []int16{0, 4}, lastIndentationSize: 3, lastNewlineCount: 2, lastColumn: 1}
	s.Deserialize(nil)
	if len(s.indents) != 0 || s.lastIndentationSize != -1 || s.lastNewlineCount != 0 || s.lastColumn != -1 {
		t.Fatalf("state = %+v", s)
	}
}

func TestSerializeLimits(t *testing.T) {
	// 509 indents fill the buffer exactly.
	s := newScanner().(*scanner)
	for i := range 509 {
		s.indents = append(s.indents, int16(i))
	}
	got, b := roundTrip(t, s)
	if len(b) != 1024 || len(got.indents) != 509 || got.indents[508] != 508 {
		t.Fatalf("len = %d, indents = %d", len(b), len(got.indents))
	}

	// One more and C writes nothing, so the state comes back reset.
	s.indents = append(s.indents, 509)
	s.lastNewlineCount = 5
	b = serialize(s)
	got.Deserialize(b)
	if len(b) != 0 || len(got.indents) != 0 || got.lastNewlineCount != 0 || got.lastColumn != -1 {
		t.Fatalf("len = %d, state = %+v", len(b), got)
	}
}

// The libc predicates in the "C" locale are ASCII only (measured, see
// scanner.go). U+00A0, U+0085, U+2028, U+3000 and -1 are the usual traps.
func TestWctype(t *testing.T) {
	for c := int32(-2); c < 0x3100; c++ {
		space := c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
		alnum := c >= 0 && c < 128 && (c >= '0' && c <= '9' || c|0x20 >= 'a' && c|0x20 <= 'z')
		if iswspace(c) != space || iswalnum(c) != alnum {
			t.Fatalf("c = %d: iswspace %v, iswalnum %v", c, iswspace(c), iswalnum(c))
		}
	}
}
