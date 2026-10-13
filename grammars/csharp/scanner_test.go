package csharp

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
	got := &scanner{quoteCount: 9, interpolationStack: []interpolation{{1, 2, 3, 4}, {5, 6, 7, 1}}}
	got.Deserialize(buf[:n])
	buf2 := make([]byte, gositter.SerializationBufferSize)
	if n2 := got.Serialize(buf2); !bytes.Equal(buf[:n], buf2[:n2]) {
		t.Fatalf("serialize after deserialize differs:\n%v\n%v", buf[:n], buf2[:n2])
	}
	return got, buf[:n]
}

func TestSerializeBytes(t *testing.T) {
	s := newScanner().(*scanner)
	if _, b := roundTrip(t, s); !bytes.Equal(b, []byte{0, 0}) {
		t.Fatalf("fresh scanner = %v, want [0 0]", b)
	}

	s.quoteCount = 5
	s.interpolationStack = []interpolation{
		{dollarCount: 1, openBraceCount: 1, quoteCount: 1, stringType: stringRegular},
		{dollarCount: 255, openBraceCount: 0, quoteCount: 200, stringType: stringRaw},
		{dollarCount: 2, openBraceCount: 2, quoteCount: 1, stringType: stringVerbatim | stringRegular},
	}
	got, b := roundTrip(t, s)
	// quote_count, stack size, then 4 bytes per entry in struct order.
	want := []byte{5, 3, 1, 1, 1, 1, 255, 0, 200, 4, 2, 2, 1, 3}
	if !bytes.Equal(b, want) {
		t.Fatalf("bytes = %v, want %v", b, want)
	}
	if got.quoteCount != 5 || !slices.Equal(got.interpolationStack, s.interpolationStack) {
		t.Fatalf("state = %+v, want %+v", got, s)
	}
}

func TestDeserializeEmpty(t *testing.T) {
	s := &scanner{quoteCount: 4, interpolationStack: []interpolation{{1, 1, 1, 1}}}
	s.Deserialize(nil)
	if s.quoteCount != 0 || len(s.interpolationStack) != 0 {
		t.Fatalf("state = %+v", s)
	}
}

func TestSerializeLimits(t *testing.T) {
	// 255 entries is the most that fits: 255*4+2 = 1022.
	s := newScanner().(*scanner)
	for i := 0; i < 255; i++ {
		s.interpolationStack = append(s.interpolationStack, interpolation{uint8(i), 0, 1, stringRegular})
	}
	got, b := roundTrip(t, s)
	if len(b) != 1022 || b[1] != 255 || !slices.Equal(got.interpolationStack, s.interpolationStack) {
		t.Fatalf("len = %d, count byte = %d", len(b), b[1])
	}

	// One more and C writes nothing at all, so the state resets on reload.
	s.quoteCount = 3
	s.interpolationStack = append(s.interpolationStack, interpolation{1, 0, 1, stringRegular})
	if n := s.Serialize(make([]byte, gositter.SerializationBufferSize)); n != 0 {
		t.Fatalf("len = %d, want 0", n)
	}
}

// Parses through the scanner and checks the state left behind.
func TestScanState(t *testing.T) {
	p := gositter.NewParser()
	if err := p.SetLanguage(Language()); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ src, want string }{
		{`var s = $"a{b}c";`, "interpolated_string_expression"},
		{`var s = $@"a""{b}c";`, "interpolated_string_expression"},
		{"var s = $$\"\"\"a{{b}}c\"\"\";", "interpolated_string_expression"},
		{"var s = \"\"\"\"a\"\"\"b\"\"\"\";", "raw_string_literal"},
	} {
		tree := p.Parse([]byte(c.src))
		sexp := tree.RootNode().String()
		if tree.RootNode().HasError() || !bytes.Contains([]byte(sexp), []byte(c.want)) {
			t.Errorf("%s: %s", c.src, sexp)
		}
		tree.Close()
	}
}

func TestIswspace(t *testing.T) {
	// What darwin libc answers in the C locale for -2..0x10FFFF
	// (scratch/scan/csharp/isw.c).
	want := []int32{9, 10, 11, 12, 13, 32}
	var got []int32
	for c := int32(-2); c < 0x110000; c++ {
		if iswspace(c) {
			got = append(got, c)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("iswspace true for %v, want %v", got, want)
	}
}
