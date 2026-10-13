//go:build !cgoside

package main

// The gositter side: pure Go parse and walk.

import (
	"fmt"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/bash"
	"github.com/dobbo-ca/gositter/grammars/c"
	"github.com/dobbo-ca/gositter/grammars/cpp"
	"github.com/dobbo-ca/gositter/grammars/csharp"
	"github.com/dobbo-ca/gositter/grammars/golang"
	"github.com/dobbo-ca/gositter/grammars/hcl"
	"github.com/dobbo-ca/gositter/grammars/java"
	"github.com/dobbo-ca/gositter/grammars/javascript"
	"github.com/dobbo-ca/gositter/grammars/json"
	"github.com/dobbo-ca/gositter/grammars/julia"
	"github.com/dobbo-ca/gositter/grammars/kotlin"
	"github.com/dobbo-ca/gositter/grammars/lua"
	"github.com/dobbo-ca/gositter/grammars/php"
	"github.com/dobbo-ca/gositter/grammars/python"
	"github.com/dobbo-ca/gositter/grammars/ruby"
	"github.com/dobbo-ca/gositter/grammars/rust"
	"github.com/dobbo-ca/gositter/grammars/scala"
	"github.com/dobbo-ca/gositter/grammars/tsx"
	"github.com/dobbo-ca/gositter/grammars/typescript"
	"github.com/dobbo-ca/gositter/grammars/verilog"
	"github.com/dobbo-ca/gositter/grammars/zig"
)

const sideName = "gositter"

var langs = map[string]func() *gositter.Language{
	"bash": bash.Language, "c": c.Language, "cpp": cpp.Language, "csharp": csharp.Language,
	"golang": golang.Language, "hcl": hcl.Language, "java": java.Language,
	"javascript": javascript.Language, "json": json.Language, "julia": julia.Language,
	"kotlin": kotlin.Language, "lua": lua.Language, "php": php.Language,
	"python": python.Language, "ruby": ruby.Language, "rust": rust.Language,
	"scala": scala.Language, "tsx": tsx.Language, "typescript": typescript.Language,
	"verilog": verilog.Language, "zig": zig.Language,
}

// No C runtime on this side.
func countCAllocs()                      {}
func cAllocCounts() (calls, bytes int64) { return -1, -1 }

type gsWorker struct {
	p    *gositter.Parser
	tree *gositter.Tree
}

func newWorker(grammar string) (worker, error) {
	f := langs[grammar]
	if f == nil {
		return nil, fmt.Errorf("no grammar %q", grammar)
	}
	p := gositter.NewParser()
	if err := p.SetLanguage(f()); err != nil {
		return nil, err
	}
	return &gsWorker{p: p}, nil
}

func (w *gsWorker) parse(src []byte) bool {
	if w.tree != nil {
		w.tree.Close()
	}
	w.tree = w.p.Parse(src)
	return w.tree.RootNode().HasError()
}

func visit(n gositter.Node, a *acc) {
	k := n.Kind()
	if len(k) > 0 {
		a.sum += uint64(k[0])
	}
	a.sum += uint64(n.StartByte()) + uint64(n.EndByte())
	a.nodes++
}

func (w *gsWorker) walk(a *acc) { walk(w.tree.RootNode(), a) }

func walk(n gositter.Node, a *acc) {
	visit(n, a)
	cc := n.ChildCount()
	for i := uint32(0); i < cc; i++ {
		walk(n.Child(i), a)
	}
}

func (w *gsWorker) cursor(a *acc) {
	c := w.tree.RootNode().Walk()
	defer c.Close()
	for {
		visit(c.Node(), a)
		if c.GotoFirstChild() {
			continue
		}
		for !c.GotoNextSibling() {
			if !c.GotoParent() {
				return
			}
		}
	}
}
