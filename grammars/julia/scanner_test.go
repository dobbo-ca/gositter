package julia

import (
	"testing"

	"github.com/dobbo-ca/gositter"
)

// C serialize returns 0 and deserialize ignores its input: every scanner
// state must serialize to the same empty byte string.
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
	s.Deserialize([]byte{1, 2, 3})
	if n := s.Serialize(buf); n != 0 {
		t.Fatalf("Serialize after Deserialize = %d, want 0", n)
	}
}

func TestLanguageWiresScanner(t *testing.T) {
	if Language().ExternalScanner.New == nil {
		t.Fatal("ExternalScanner.New is nil: rerun grammars/gen.sh julia")
	}
}

func TestParseScannerTokens(t *testing.T) {
	p := gositter.NewParser()
	p.SetLanguage(Language())
	src := []byte("#= a #= b =# c =#\nf(x) = \"a$(x)b\" * raw\"\\\"q\" * `ls $x` * \"\"\"t \"u\" \"\"\"\n")
	tree := p.Parse(src)
	if got := tree.RootNode().String(); got == "" || tree.RootNode().HasError() {
		t.Fatalf("tree = %s", got)
	}
}
