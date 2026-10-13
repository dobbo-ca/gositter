package tsx

import (
	"testing"

	"github.com/dobbo-ca/gositter"
)

// C serialize returns 0 and deserialize ignores its input.
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
	s.Deserialize(buf)
	if n := s.Serialize(buf); n != 0 {
		t.Fatalf("Serialize after Deserialize = %d, want 0", n)
	}
}

// The ranges measured from darwin libc in the C locale (scratch/scan/typescript/isw.c).
func TestIsw(t *testing.T) {
	for c := int32(-2); c <= 0x110000; c++ {
		sp := (c >= 9 && c <= 13) || c == 32
		al := (c >= 65 && c <= 90) || (c >= 97 && c <= 122)
		di := c >= 48 && c <= 57
		if iswspace(c) != sp || iswalpha(c) != al || iswdigit(c) != di {
			t.Fatalf("isw*(%d) differs", c)
		}
	}
}

// Parses through the scanner: template chars, ASI, ternary, html comment.
func TestParse(t *testing.T) {
	p := gositter.NewParser()
	if err := p.SetLanguage(Language()); err != nil {
		t.Fatal(err)
	}
	if language.ExternalScanner.New == nil {
		t.Fatal("ExternalScanner.New is nil: rerun grammars/gen.sh")
	}
	src := []byte("let a = `x${b}y`\nlet c = a ? 1 : 2\n<!-- c\nfunction f(x?: number) {}\n")
	tree := p.Parse(src)
	defer tree.Close()
	if root := tree.RootNode(); root.HasError() {
		t.Fatalf("tree has errors: %s", root.String())
	}
}
