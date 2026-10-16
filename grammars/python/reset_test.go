package python

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dobbo-ca/gositter"
)

var resetInputs = []string{
	"",
	"x = 1\n",
	"if a:\n    if b:\n        if c:\n            pass\n",
	"def f():\n    return f\"a{b}c{{d}}\"\n",
	"x = f\"a{f'b{c}'}d\"\n",
	"if a:\n    if b:\n        x = f\"a{b",
	"def f():\n    s = '''text\n  more",
	"class A:\n    def f(self):\n        if x:\n            y = rb\"raw",
	"x = (1,\n   2,\n",
	"if a:\n  if b:\n    f(\"\"\"doc {x}\n",
	"try:\n    pass\nexcept* E:\n    pass\n",
	"x = f\"{a!r:>{w}}\" + (\n",
}

// Capacity is the only allowed difference, so nil and empty slices are
// the same.
func norm(s scanner) scanner {
	if len(s.indents) == 0 {
		s.indents = nil
	}
	if len(s.delimiters) == 0 {
		s.delimiters = nil
	}
	return s
}

func checkReset(t *testing.T, s *scanner, what string) {
	t.Helper()
	s.Reset()
	if got, want := norm(*s), norm(*newScanner().(*scanner)); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: after Reset %+v, New gives %+v", what, got, want)
	}
	buf, want := make([]byte, gositter.SerializationBufferSize), make([]byte, gositter.SerializationBufferSize)
	if !reflect.DeepEqual(buf[:s.Serialize(buf)], want[:newScanner().Serialize(want)]) {
		t.Errorf("%s: Reset and New serialize differently", what)
	}
}

func TestResetIsNew(t *testing.T) {
	var kept *scanner
	orig := language.ExternalScanner.New
	language.ExternalScanner.New = func() gositter.ExternalScanner {
		kept = orig().(*scanner)
		return kept
	}
	t.Cleanup(func() { language.ExternalScanner.New = orig })

	// insideInterpolatedString is a field Deserialize(nil) leaves alone.
	s := &scanner{
		indents:                  []uint16{0, 4, 8},
		delimiters:               []byte{flagDoubleQuote | flagFormat, flagSingleQuote},
		insideInterpolatedString: true,
	}
	checkReset(t, s, "hand-made")

	dirty := 0
	p := gositter.NewParser()
	p.SetLanguage(&language)
	for _, in := range resetInputs {
		for _, budget := range []uint32{0, 1} {
			p.Reset()
			p.ParseBudget([]byte(in), budget)
			if kept == nil {
				t.Fatalf("%q: no scanner made", in)
			}
			if !reflect.DeepEqual(norm(*kept), norm(*newScanner().(*scanner))) {
				dirty++
			}
			checkReset(t, kept, strings.TrimSpace(in))
			// Reset must hold after the scanner is reused too.
			p.Reset()
			p.ParseBudget([]byte(in), budget)
		}
	}
	if dirty == 0 {
		t.Error("no input left the scanner dirty, the test checks nothing")
	}
}

// B parsed after A on one parser has the tree of B on a fresh parser.
func TestReusedScannerSameTree(t *testing.T) {
	reused := gositter.NewParser()
	reused.SetLanguage(&language)
	for _, a := range resetInputs {
		for _, b := range resetInputs {
			reused.Parse([]byte(a))
			got := reused.Parse([]byte(b)).RootNode().String()
			fresh := gositter.NewParser()
			fresh.SetLanguage(&language)
			if want := fresh.Parse([]byte(b)).RootNode().String(); got != want {
				t.Fatalf("%q after %q:\n got %s\nwant %s", b, a, got, want)
			}
		}
	}
}
