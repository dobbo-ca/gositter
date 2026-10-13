package kotlin

import (
	"testing"

	"github.com/dobbo-ca/gositter"
)

// C create returns NULL and serialize returns 0: the state is always empty.
func TestSerializeRoundTrip(t *testing.T) {
	s := newScanner()
	buf := make([]byte, gositter.SerializationBufferSize)
	for i := range buf {
		buf[i] = 0xAA
	}
	if n := s.Serialize(buf); n != 0 {
		t.Fatalf("Serialize = %d, want 0", n)
	}
	for i, b := range buf {
		if b != 0xAA {
			t.Fatalf("Serialize wrote buf[%d]", i)
		}
	}
	s.Deserialize(nil)
	s.Deserialize(buf[:0])
	s.Deserialize([]byte{1, 2, 3}) // C ignores any buffer
	if n := s.Serialize(buf); n != 0 {
		t.Fatalf("Serialize after Deserialize = %d, want 0", n)
	}
	if s != newScanner() {
		t.Fatal("scanner is not stateless")
	}
}

// darwin "C" locale: ASCII only, also for -1 (invalid UTF-8) and Latin-1.
func TestWctype(t *testing.T) {
	for c := int32(-2); c < 0x300; c++ {
		sp := c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
		al := (c|0x20) >= 'a' && (c|0x20) <= 'z' && c < 128 && c > 0
		dg := c >= '0' && c <= '9'
		if iswspace(c) != sp || iswalpha(c) != al || iswdigit(c) != dg || iswalnum(c) != (al || dg) {
			t.Fatalf("code point %d", c)
		}
	}
}

func TestScan(t *testing.T) {
	p := gositter.NewParser()
	if err := p.SetLanguage(Language()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ src, want string }{
		// automatic semicolon, none before `else` or `.`
		{"val a = 1\nval b = 2\n", "(source_file (property_declaration (variable_declaration (identifier)) (number_literal)) (property_declaration (variable_declaration (identifier)) (number_literal)))"},
		{"val s = \"\"\"a$b\"\"\"\n", "(source_file (property_declaration (variable_declaration (identifier)) (multiline_string_literal (string_content) (interpolation (identifier)))))"},
		{"/* a /* b */ c */\n", "(source_file (block_comment))"},
	} {
		tree := p.Parse([]byte(tc.src))
		if got := tree.RootNode().String(); got != tc.want {
			t.Errorf("%q:\n got %s\nwant %s", tc.src, got, tc.want)
		}
		tree.Close()
	}
}
