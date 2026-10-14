package gositter_test

import (
	"testing"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/bash"
	"github.com/dobbo-ca/gositter/grammars/golang"
	"github.com/dobbo-ca/gositter/grammars/python"
)

// The kept scanner serves the next parse of its language only.
func TestScannerReuseAcrossLanguages(t *testing.T) {
	py := []byte("if a:\n    x = f\"a{b}\"\n")
	sh := []byte("cat <<EOF\nbody\nEOF\n")
	fresh := func(l *gositter.Language, src []byte) string {
		p := gositter.NewParser()
		p.SetLanguage(l)
		return p.Parse(src).RootNode().String()
	}
	p := gositter.NewParser()
	p.SetLanguage(python.Language())
	p.Parse(py)
	first := p.KeptScanner()
	if first == nil {
		t.Fatal("no scanner kept")
	}
	p.Parse(py)
	if p.KeptScanner() != first {
		t.Error("scanner not reused for the same language")
	}
	p.SetLanguage(bash.Language())
	if got := p.Parse(sh).RootNode().String(); got != fresh(bash.Language(), sh) {
		t.Error("bash tree differs after python")
	}
	if p.KeptScanner() == first {
		t.Error("python scanner handed to bash")
	}
	p.SetLanguage(golang.Language()) // no scanner of its own
	p.Parse([]byte("package p\n"))
	p.SetLanguage(python.Language())
	if got := p.Parse(py).RootNode().String(); got != fresh(python.Language(), py) {
		t.Error("python tree differs after bash and go")
	}
}

func TestSetLanguageDropsKeptScanner(t *testing.T) {
	p := gositter.NewParser()
	p.SetLanguage(bash.Language())
	p.Parse([]byte("cat <<A\nx\nA\n"))
	p.SetLanguage(golang.Language())
	if p.KeptScanner() != nil {
		t.Fatal("SetLanguage kept the bash scanner")
	}
}
