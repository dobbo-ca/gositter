package php

import (
	"bytes"
	"slices"
	"testing"

	"github.com/dobbo-ca/gositter"
)

func ser(s *scanner) []byte {
	buf := make([]byte, gositter.SerializationBufferSize)
	return buf[:s.Serialize(buf)]
}

func word(s string) []int32 { return []int32(string(s)) }

func TestSerializeBytes(t *testing.T) {
	s := newScanner().(*scanner)
	if b := ser(s); !bytes.Equal(b, []byte{0}) {
		t.Fatalf("fresh scanner = %v, want [0]", b)
	}

	s.heredocs = []heredoc{{word: word("EOT")}, {endWordIndentationAllowed: true, word: word("é")}, {}}
	// count, then per heredoc: flag, uint32 length, int32 code points, all
	// little-endian.
	want := []byte{
		3,
		0, 3, 0, 0, 0, 'E', 0, 0, 0, 'O', 0, 0, 0, 'T', 0, 0, 0,
		1, 1, 0, 0, 0, 0xE9, 0, 0, 0,
		0, 0, 0, 0, 0,
	}
	b := ser(s)
	if !bytes.Equal(b, want) {
		t.Fatalf("bytes = %v, want %v", b, want)
	}

	// Dirty target with one heredoc: all of it is replaced.
	got := &scanner{hasLeadingWhitespace: true, heredocs: []heredoc{{endWordIndentationAllowed: true, word: word("ZZZZ")}}}
	got.Deserialize(b)
	if got.hasLeadingWhitespace || len(got.heredocs) != 3 {
		t.Fatalf("state = %+v", got)
	}
	for i := range s.heredocs {
		if got.heredocs[i].endWordIndentationAllowed != s.heredocs[i].endWordIndentationAllowed ||
			!slices.Equal(got.heredocs[i].word, s.heredocs[i].word) {
			t.Fatalf("heredoc %d = %+v, want %+v", i, got.heredocs[i], s.heredocs[i])
		}
	}
	if b2 := ser(got); !bytes.Equal(b, b2) {
		t.Fatalf("serialize after deserialize differs:\n%v\n%v", b, b2)
	}
}

// C's deserialize empties the heredocs but never lowers heredocs.size.
func TestDeserializeKeepsCount(t *testing.T) {
	s := &scanner{heredocs: []heredoc{{word: word("A")}, {endWordIndentationAllowed: true, word: word("B")}}}
	s.Deserialize(nil)
	if b := ser(s); !bytes.Equal(b, []byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("after empty state = %v", b)
	}

	s.heredocs[1].word = word("B")
	s.Deserialize([]byte{0})
	if len(s.heredocs) != 2 || s.heredocs[1].word != nil {
		t.Fatalf("after [0] = %+v", s.heredocs)
	}

	s.Deserialize([]byte{1, 0, 1, 0, 0, 0, 'C', 0, 0, 0})
	if b := ser(s); !bytes.Equal(b, []byte{2, 0, 1, 0, 0, 0, 'C', 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("after one heredoc = %v", b)
	}
}

func TestSerializeLimits(t *testing.T) {
	// size + 5 + word_size >= 1024 gives 0. One heredoc: 1 + 5 + 4n.
	s := &scanner{heredocs: []heredoc{{word: make([]int32, 254)}}}
	if b := ser(s); len(b) != 1+5+254*4 {
		t.Fatalf("254 code points: len = %d", len(b))
	}
	s.heredocs[0].word = make([]int32, 255)
	if b := ser(s); len(b) != 0 {
		t.Fatalf("255 code points: len = %d, want 0", len(b))
	}

	// Empty heredocs: 1 + 5n, the check fails for the 205th.
	s.heredocs = make([]heredoc, 204)
	b := ser(s)
	if len(b) != 1+5*204 {
		t.Fatalf("204 heredocs: len = %d", len(b))
	}
	got := &scanner{}
	got.Deserialize(b)
	if len(got.heredocs) != 204 || !bytes.Equal(ser(got), b) {
		t.Fatalf("204 heredocs do not round trip")
	}
	s.heredocs = make([]heredoc, 205)
	if b := ser(s); len(b) != 0 {
		t.Fatalf("205 heredocs: len = %d, want 0", len(b))
	}
}

func TestWctype(t *testing.T) {
	for c := int32(-2); c < 0x3100; c++ {
		sp := c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
		if iswspace(c) != sp {
			t.Fatalf("iswspace(%#x) = %v", c, !sp)
		}
		if c >= 0x80 && (iswalnum(c) || iswdigit(c) || iswxdigit(c)) {
			t.Fatalf("non-ASCII %#x classified", c)
		}
	}
}
