package ruby

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/dobbo-ca/gositter"
)

func serialize(s *scanner) []byte {
	buf := make([]byte, gositter.SerializationBufferSize)
	return buf[:s.Serialize(buf)]
}

// Deserialize into a dirty scanner, then compare state and bytes.
func roundTrip(t *testing.T, s *scanner) []byte {
	t.Helper()
	b := serialize(s)
	got := &scanner{
		hasLeadingWhitespace: true,
		literalStack:         []literal{{typ: tokRegexStart, nestingDepth: 9}},
		openHeredocs:         []heredoc{{word: []byte("OLD"), started: true}, {word: []byte("OLDER")}},
	}
	got.Deserialize(b)
	if got.hasLeadingWhitespace {
		t.Fatal("hasLeadingWhitespace not reset")
	}
	if b2 := serialize(got); !bytes.Equal(b, b2) {
		t.Fatalf("serialize after deserialize differs:\n%v\n%v", b, b2)
	}
	if len(got.literalStack) != len(s.literalStack) || len(got.openHeredocs) != len(s.openHeredocs) {
		t.Fatalf("state = %+v, want %+v", got, s)
	}
	for i := range s.openHeredocs {
		w, g := s.openHeredocs[i], got.openHeredocs[i]
		if !bytes.Equal(w.word, g.word) || w.started != g.started ||
			w.allowsInterpolation != g.allowsInterpolation || w.endWordIndentationAllowed != g.endWordIndentationAllowed {
			t.Fatalf("heredoc %d = %+v, want %+v", i, g, w)
		}
	}
	return b
}

func TestSerializeBytes(t *testing.T) {
	s := newScanner().(*scanner)
	if b := serialize(s); !bytes.Equal(b, []byte{0, 0}) {
		t.Fatalf("fresh scanner = %v, want [0 0]", b)
	}

	s.hasLeadingWhitespace = true // not serialized
	s.literalStack = []literal{
		{typ: tokStringStart, openDelimiter: '"', closeDelimiter: '"', nestingDepth: 1, allowsInterpolation: true},
		{typ: tokStringArrayStart, openDelimiter: '(', closeDelimiter: ')', nestingDepth: 3},
	}
	s.openHeredocs = []heredoc{
		{word: []byte("EOS"), endWordIndentationAllowed: true, allowsInterpolation: true, started: true},
		{word: []byte{0xFF, 'x'}},
	}
	want := []byte{
		2,
		tokStringStart, '"', '"', 1, 1,
		tokStringArrayStart, '(', ')', 3, 0,
		2,
		1, 1, 1, 3, 'E', 'O', 'S',
		0, 0, 0, 2, 0xFF, 'x',
	}
	if b := roundTrip(t, s); !bytes.Equal(b, want) {
		t.Fatalf("bytes = %v, want %v", b, want)
	}
	got := &scanner{}
	got.Deserialize(want)
	if !reflect.DeepEqual(got.literalStack, s.literalStack) {
		t.Fatalf("literals = %+v, want %+v", got.literalStack, s.literalStack)
	}
}

// C writes (char)nesting_depth and reads it back as unsigned char.
func TestNestingDepthWraps(t *testing.T) {
	s := &scanner{literalStack: []literal{
		{typ: tokStringStart, openDelimiter: '(', closeDelimiter: ')', nestingDepth: 300},
		{typ: tokStringStart, openDelimiter: '(', closeDelimiter: ')', nestingDepth: -1},
	}}
	got := &scanner{}
	got.Deserialize(serialize(s))
	if got.literalStack[0].nestingDepth != 44 || got.literalStack[1].nestingDepth != 255 {
		t.Fatalf("depths = %d, %d, want 44, 255", got.literalStack[0].nestingDepth, got.literalStack[1].nestingDepth)
	}
}

func TestDeserializeEmpty(t *testing.T) {
	s := &scanner{
		hasLeadingWhitespace: true,
		literalStack:         []literal{{typ: tokStringStart}},
		openHeredocs:         []heredoc{{word: []byte("A")}},
	}
	s.Deserialize(nil)
	if s.hasLeadingWhitespace || len(s.literalStack) != 0 || len(s.openHeredocs) != 0 {
		t.Fatalf("state = %+v", s)
	}
}

func TestSerializeLimits(t *testing.T) {
	// literal_stack.size * 5 + 2 >= 1024 gives 0: 204 fit, 205 do not.
	s := &scanner{literalStack: make([]literal, 204)}
	if n := len(roundTrip(t, s)); n != 204*5+2 {
		t.Fatalf("204 literals: %d bytes", n)
	}
	s.literalStack = make([]literal, 205)
	if n := len(serialize(s)); n != 0 {
		t.Fatalf("205 literals: %d bytes, want 0", n)
	}

	// size + 2 + word.size >= 1024 gives 0. size is 2 at the first heredoc.
	word := func(n int) []byte { return bytes.Repeat([]byte{'W'}, n) }
	four := func(n int) *scanner {
		return &scanner{openHeredocs: []heredoc{{word: word(250)}, {word: word(250)}, {word: word(250)}, {word: word(n)}}}
	}
	// Three heredocs take 2 + 3*254 = 764 bytes; the fourth has 260 left.
	if n := len(roundTrip(t, four(255))); n != 764+4+255 {
		t.Fatalf("fits: %d bytes", n)
	}
	// Exactly full: 764 + 4 + 256 would be 1024, but a 256 byte word does
	// not fit the length byte. C aborts there; the port drops the state.
	if n := len(serialize(four(256))); n != 0 {
		t.Fatalf("256 byte word: %d bytes, want 0", n)
	}
	if n := len(serialize(four(258))); n != 0 { // C's own test: 764+2+258 >= 1024
		t.Fatalf("too long: %d bytes, want 0", n)
	}
	// C passes its test (size+2+len == 1023) and writes one byte past the
	// buffer. The port drops the state and must not panic.
	// 2 + 5 + 762 = 769; 769 + 2 + 252 = 1023 passes C's test; 769 + 4 + 252 = 1025.
	s = &scanner{openHeredocs: []heredoc{{word: word(1)}, {word: word(250)}, {word: word(250)}, {word: word(250)}, {word: word(252)}}}
	if n := len(serialize(s)); n != 0 {
		t.Fatalf("overflowing state: %d bytes, want 0", n)
	}
}

// The word buffers are recycled between Deserialize calls; no two heredocs
// may end up sharing one.
func TestWordBuffersNotShared(t *testing.T) {
	a := &scanner{openHeredocs: []heredoc{{word: []byte("AAAA")}, {word: []byte("BBBB")}, {word: []byte("CCCC")}}}
	b := serialize(a)
	s := &scanner{}
	for range 3 {
		s.Deserialize(b)
		s.eraseFirstHeredoc()
		s.eraseFirstHeredoc()
		if w := string(s.openHeredocs[0].word); w != "CCCC" {
			t.Fatalf("after erase: %q", w)
		}
		s.Deserialize(b)
		if got := serialize(s); !bytes.Equal(got, b) {
			t.Fatalf("bytes = %v, want %v", got, b)
		}
	}
}
