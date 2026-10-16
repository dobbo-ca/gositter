package cpp

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dobbo-ca/gositter"
)

var resetInputs = []string{
	"",
	"int a;\n",
	"auto s = R\"x(body)x\";\n",
	"auto s = R\"(body)\";\n",
	"auto s = R\"abc(body without an end",
	"auto s = R\"abcdefghijklmnopq(x)\";\n",
	"auto s = R\"ab cd(x)\";\n",
	"auto s = R\"x(a)y\" R\"y(b",
	"auto s = LR\"d(\n\n",
}

func checkReset(t *testing.T, s *scanner, what string) {
	t.Helper()
	s.Reset()
	if got, want := *s, *newScanner().(*scanner); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: after Reset %+v, New gives %+v", what, got, want)
	}
	if !reflect.DeepEqual(serialize(s), serialize(newScanner().(*scanner))) {
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

	s := newScanner().(*scanner)
	s.delimiterLength = 3
	s.delimiter[0], s.delimiter[15] = 'a', 'z'
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
			if !reflect.DeepEqual(*kept, *newScanner().(*scanner)) {
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
