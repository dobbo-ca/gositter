package hcl

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dobbo-ca/gositter"
)

var resetInputs = []string{
	"",
	"a = 1\n",
	"a = \"x ${y} z\"\n",
	"a = <<EOF\nbody ${x}\nEOF\n",
	"a = <<-EOF\n  %{ if x }\n  b\n  %{ endif }\n  EOF\n",
	"a = <<EOF\nbody without an end",
	"a = \"x ${y + \"${z\n",
	"a = \"%{ for i in l }${i}%{ endfor }\"\nb = <<A\n${\n",
	"a = \"${ \"${ \"${ \"unclosed\n",
}

// Capacity is the only allowed difference, so nil and empty slices are
// the same.
func norm(s scanner) scanner {
	if len(s.contextStack) == 0 {
		s.contextStack = nil
	}
	for i := range s.contextStack {
		if len(s.contextStack[i].heredocIdentifier) == 0 {
			s.contextStack[i].heredocIdentifier = nil
		}
	}
	return s
}

func checkReset(t *testing.T, s *scanner, what string) {
	t.Helper()
	s.Reset()
	if got, want := norm(*s), norm(*newScanner().(*scanner)); !reflect.DeepEqual(got, want) {
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
	s.push(ctxHeredocTemplate).heredocIdentifier = []byte("EOF")
	s.push(ctxQuotedTemplate)
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

func TestResetDropsBigBuffers(t *testing.T) {
	for _, in := range []string{
		strings.Repeat("a = \"${b(\"${", 20000),
		"a = <<" + strings.Repeat("A", 2000000) + "\nbody\n",
	} {
		var kept *scanner
		orig := language.ExternalScanner.New
		language.ExternalScanner.New = func() gositter.ExternalScanner {
			kept = orig().(*scanner)
			return kept
		}
		p := gositter.NewParser()
		p.SetLanguage(&language)
		p.Parse([]byte(in))
		p.Parse([]byte("a = 1\n"))
		language.ExternalScanner.New = orig
		held := cap(kept.contextStack)
		for _, c := range kept.contextStack[:cap(kept.contextStack)] {
			held += cap(c.heredocIdentifier)
		}
		if held > 4096 {
			t.Errorf("small parse kept %d slots and bytes", held)
		}
	}
}
