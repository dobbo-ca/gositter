package bash

import (
	"bytes"
	"testing"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/internal/cchar"
)

func serialize(s *scanner) []byte {
	buf := make([]byte, gositter.SerializationBufferSize)
	return buf[:s.Serialize(buf)]
}

func TestSerializeBytes(t *testing.T) {
	s := newScanner().(*scanner)
	if b := serialize(s); !bytes.Equal(b, []byte{0, 0, 0, 0}) {
		t.Fatalf("fresh scanner = %v", b)
	}

	s.lastGlobParenDepth = 0x81
	s.extSawOutsideQuote = true
	s.pushHeredoc(true)
	s.heredocs[0].isRaw = true
	s.heredocs[0].delimiter = append(s.heredocs[0].delimiter, 'E', 0xFF, 0)
	s.pushHeredoc(false)
	s.heredocs[1].started = true
	b := serialize(s)
	// header, then per heredoc: is_raw, started, allows_indent, uint32 LE size, bytes.
	want := []byte{0x81, 0, 1, 2, 1, 0, 1, 3, 0, 0, 0, 'E', 0xFF, 0, 0, 1, 0, 0, 0, 0, 0}
	if !bytes.Equal(b, want) {
		t.Fatalf("bytes = %v, want %v", b, want)
	}

	// Dirty target with one stale heredoc: every serialized field is replaced.
	got := &scanner{lastGlobParenDepth: 7, extWasInDoubleQuote: true}
	got.pushHeredoc(false)
	got.heredocs[0].started = true
	got.heredocs[0].delimiter = append(got.heredocs[0].delimiter, "STALESTALE\x00"...)
	got.Deserialize(b)
	if b2 := serialize(got); !bytes.Equal(b2, want) {
		t.Fatalf("after round trip = %v, want %v", b2, want)
	}
	h := got.heredocs[0]
	if got.lastGlobParenDepth != 0x81 || got.extWasInDoubleQuote || !got.extSawOutsideQuote ||
		!h.isRaw || h.started || !h.allowsIndent || !bytes.Equal(h.delimiter, []byte{'E', 0xFF, 0}) {
		t.Fatalf("state = %+v", got)
	}
}

// C resets each heredoc on an empty state but keeps heredocs.size, and a
// state with fewer heredocs does not shrink the array either.
func TestDeserializeKeepsCount(t *testing.T) {
	s := &scanner{lastGlobParenDepth: 3, extWasInDoubleQuote: true}
	for range 3 {
		s.pushHeredoc(true)
		h := &s.heredocs[len(s.heredocs)-1]
		h.isRaw, h.started = true, true
		h.delimiter = append(h.delimiter, 'X', 0)
	}
	one := []byte{0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 'Y', 0}
	s.Deserialize(one)
	if len(s.heredocs) != 3 || string(s.heredocs[0].delimiter) != "Y\x00" || string(s.heredocs[2].delimiter) != "X\x00" {
		t.Fatalf("state = %+v", s)
	}

	s.Deserialize(nil)
	want := []byte{0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if b := serialize(s); !bytes.Equal(b, want) {
		t.Fatalf("after reset = %v, want %v", b, want)
	}
}

func TestSerializeLimit(t *testing.T) {
	// 4 + 3 + 4 + len must stay below 1024, else C returns 0.
	for _, c := range []struct{ n, want int }{{1012, 1023}, {1013, 0}, {5000, 0}} {
		s := newScanner().(*scanner)
		s.pushHeredoc(false)
		s.heredocs[0].delimiter = bytes.Repeat([]byte{'D'}, c.n)
		b := serialize(s)
		if len(b) != c.want {
			t.Fatalf("delimiter %d: size %d, want %d", c.n, len(b), c.want)
		}
		if c.want > 0 {
			got := newScanner().(*scanner)
			got.Deserialize(b)
			if !bytes.Equal(serialize(got), b) {
				t.Fatalf("delimiter %d: round trip differs", c.n)
			}
		}
	}
	// 147 empty heredocs: 4 + 146*7 = 1026 would not fit.
	s := newScanner().(*scanner)
	for range 145 {
		s.pushHeredoc(false)
	}
	if n := len(serialize(s)); n != 4+145*7 {
		t.Fatalf("145 heredocs: size %d", n)
	}
	s.pushHeredoc(false)
	if n := len(serialize(s)); n != 0 {
		t.Fatalf("146 heredocs: size %d, want 0", n)
	}
}

// The delimiter is compared as C chars against lookahead, so the two
// non-ASCII cases swap where char is unsigned.
func TestHeredocEndChar(t *testing.T) {
	p := gositter.NewParser()
	if err := p.SetLanguage(Language()); err != nil {
		t.Fatal(err)
	}
	for src, wantErr := range map[string]bool{
		"cat <<E\nx\nE\n":               false,
		"cat <<\xff\nx\n\xff\n":         cchar.Unsigned,  // signed: 0xFF is -1, as is an invalid byte
		"cat <<\xc3\xa9\nx\n\xc3\xa9\n": !cchar.Unsigned, // signed: U+00E9 is cut to 0xE9 = -23, never matches
	} {
		tree := p.Parse([]byte(src))
		if got := tree.RootNode().HasError(); got != wantErr {
			t.Errorf("%q: HasError = %v, want %v\n%s", src, got, wantErr, tree.RootNode().String())
		}
		tree.Close()
	}
}
