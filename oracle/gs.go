//go:build !nogo

package main

// The gositter side. Grammars register from gs_<name>.go, one build tag each,
// so the binary builds while only some grammar packages exist.

import (
	"fmt"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/internal/treedump"
)

var goLangs = map[string]func() *gositter.Language{}

type gParser struct {
	p    *gositter.Parser
	lang *gositter.Language
	tree *gositter.Tree
	buf  []gositter.Node
}

func openGo(grammar string) (parser, error) {
	f := goLangs[grammar]
	if f == nil {
		return nil, fmt.Errorf("gositter grammar %q is not in this build (see build.sh)", grammar)
	}
	lang := f()
	p := gositter.NewParser()
	if err := p.SetLanguage(lang); err != nil {
		return nil, err
	}
	return &gParser{p: p, lang: lang}, nil
}

func (p *gParser) parse(src []byte) treedump.Node {
	if p.tree != nil {
		p.tree.Close()
	}
	p.tree = p.p.Parse(src)
	return treedump.Wrap(p.tree.RootNode())
}

func (p *gParser) fields() []string {
	if len(p.lang.FieldNames) == 0 { // verilog: FIELD_COUNT 0
		return nil
	}
	return p.lang.FieldNames[1:]
}

func (p *gParser) setLogger(fn func(lex bool, msg string)) {
	p.p.SetLogger(func(t gositter.LogType, msg string) { fn(t == gositter.LogTypeLex, msg) })
}

func (p *gParser) walk() int {
	if walkChildren {
		n := 0
		p.buf, n = gWalkChildren(p.tree.RootNode(), p.buf[:0])
		return n
	}
	return gWalk(p.tree.RootNode())
}

// The same walk through Node.Children: buf is the work list, so one slice
// serves the whole tree.
func gWalkChildren(root gositter.Node, buf []gositter.Node) ([]gositter.Node, int) {
	buf = append(buf, root)
	count := 0
	for len(buf) > 0 {
		n := buf[len(buf)-1]
		buf = buf[:len(buf)-1]
		_, _, _ = n.Kind(), n.StartByte(), n.EndByte()
		count++
		buf = n.Children(buf)
	}
	return buf, count
}

func (p *gParser) close() {
	if p.tree != nil {
		p.tree.Close()
	}
}

// Same shape as cWalk.
func gWalk(n gositter.Node) int {
	_, _, _ = n.Kind(), n.StartByte(), n.EndByte()
	count := 1
	cc := n.ChildCount()
	for i := uint32(0); i < cc; i++ {
		count += gWalk(n.Child(i))
	}
	return count
}
