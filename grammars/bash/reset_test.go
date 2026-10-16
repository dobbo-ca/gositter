package bash

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dobbo-ca/gositter"
)

var resetInputs = []string{
	"",
	"echo hi\n",
	"cat <<EOF\nbody\nEOF\n",
	"cat <<-A <<'B'\n\tone\n\tA\ntwo\nB\n",
	"cat <<EOF\nbody $(echo <<X\nin\nX\n) more\nEOF\n",
	"cat <<EOF\nbody without an end",
	"cat <<A <<B <<C\nx\nA\n",
	"echo ${x:-$(cat <<E\n",
	"echo \"$(( 1 + \n",
	"if [[ $x =~ ^a ]]; then echo 'unclosed\n",
	"echo ${a[@]#*} <<<\"s\"\nfoo=bar\n",
	"cat <<EOF >out; echo $(\n",
}

// Capacity is the only allowed difference, so nil and empty slices are
// the same.
func norm(s scanner) scanner {
	if len(s.heredocs) == 0 {
		s.heredocs = nil
	}
	for i := range s.heredocs {
		h := &s.heredocs[i]
		if len(h.delimiter) == 0 {
			h.delimiter = nil
		}
		if len(h.currentLeadingWord) == 0 {
			h.currentLeadingWord = nil
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

	// Fields Deserialize(nil) leaves alone.
	s := &scanner{lastGlobParenDepth: 3, extWasInDoubleQuote: true, extSawOutsideQuote: true}
	s.pushHeredoc(true)
	s.heredocs[0].isRaw, s.heredocs[0].started = true, true
	s.heredocs[0].delimiter = []byte("EOF")
	s.heredocs[0].currentLeadingWord = []byte("lead")
	s.pushHeredoc(false)
	s.heredocs[1].delimiter = []byte("X")
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

// A small parse after a huge one must not keep the huge buffers.
func TestResetDropsBigBuffers(t *testing.T) {
	for _, in := range []string{
		strings.Repeat("cat <<A; ", 20000),
		"cat <<" + strings.Repeat("A", 2000000) + "\nbody\n",
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
		p.Parse([]byte("echo hi\n"))
		language.ExternalScanner.New = orig
		held := cap(kept.heredocs)
		for _, h := range kept.heredocs[:cap(kept.heredocs)] {
			held += cap(h.delimiter) + cap(h.currentLeadingWord)
		}
		if held > 4096 {
			t.Errorf("small parse kept %d slots and bytes", held)
		}
	}
}
