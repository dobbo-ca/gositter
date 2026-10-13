package lua

import (
	"bytes"
	"testing"

	"github.com/dobbo-ca/gositter"
)

func TestSerializeRoundTrip(t *testing.T) {
	buf := make([]byte, gositter.SerializationBufferSize)
	s := newScanner().(*scanner)
	if n := s.Serialize(buf); !bytes.Equal(buf[:n], []byte{0, 0}) {
		t.Fatalf("fresh scanner = %v, want [0 0]", buf[:n])
	}

	for _, want := range [][]byte{{0, 0}, {0, 3}, {0, 255}, {'"', 7}, {0x80, 1}, {0xFF, 0xFF}} {
		got := &scanner{endingChar: 5, levelCount: 9} // dirty target
		got.Deserialize(want)
		if n := got.Serialize(buf); !bytes.Equal(buf[:n], want) {
			t.Fatalf("round trip of %v = %v", want, buf[:n])
		}
	}
	// char is signed: 0x80 must not compare equal to code point U+0080.
	s.Deserialize([]byte{0x80, 0})
	if s.endingChar != -128 {
		t.Fatalf("endingChar = %d, want -128", s.endingChar)
	}
}

func TestDeserializeShort(t *testing.T) {
	// C returns early: length 0 touches nothing, length 1 only ending_char.
	s := &scanner{endingChar: 5, levelCount: 9}
	s.Deserialize(nil)
	if s.endingChar != 5 || s.levelCount != 9 {
		t.Fatalf("length 0 changed the state: %+v", s)
	}
	s.Deserialize([]byte{7})
	if s.endingChar != 7 || s.levelCount != 9 {
		t.Fatalf("length 1: %+v", s)
	}
}

// Trees for long brackets, incl. the uint8 wrap at 256 '='.
func TestLongBrackets(t *testing.T) {
	eq := func(n int) string { return string(bytes.Repeat([]byte{'='}, n)) }
	for _, tc := range []struct{ src, want string }{
		{"x = [==[a]]b]=]c]==]", "(chunk (assignment_statement (variable_list name: (identifier)) (expression_list value: (string content: (string_content)))))"},
		{"--[[ c ]] x()", "(chunk (comment content: (comment_content)) (function_call name: (identifier) arguments: (arguments)))"},
		// level 256 wraps to 0, so a plain ]] closes it
		{"x = [" + eq(256) + "[a]]", "(chunk (assignment_statement (variable_list name: (identifier)) (expression_list value: (string content: (string_content)))))"},
	} {
		p := gositter.NewParser()
		p.SetLanguage(Language())
		tree := p.Parse([]byte(tc.src))
		if got := tree.RootNode().String(); got != tc.want {
			t.Errorf("%q:\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
}
