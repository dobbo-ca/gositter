//go:build cgoside

package main

// The cgo side: the C runtime through go-tree-sitter v0.25.0, used the way
// its API is meant to be used (Parser.Parse, Node, TreeCursor).

/*
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	hcl "github.com/tree-sitter-grammars/tree-sitter-hcl/bindings/go"
	kotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	lua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	zig "github.com/tree-sitter-grammars/tree-sitter-zig/bindings/go"
	ts "github.com/tree-sitter/go-tree-sitter"
	bash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	csharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	c "github.com/tree-sitter/tree-sitter-c/bindings/go"
	cpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	golang "github.com/tree-sitter/tree-sitter-go/bindings/go"
	java "github.com/tree-sitter/tree-sitter-java/bindings/go"
	javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	julia "github.com/tree-sitter/tree-sitter-julia/bindings/go"
	php "github.com/tree-sitter/tree-sitter-php/bindings/go"
	python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	ruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	scala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
	typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
	verilog "github.com/tree-sitter/tree-sitter-verilog/bindings/go"
)

const sideName = "cgo"

var langs = map[string]func() unsafe.Pointer{
	"bash": bash.Language, "c": c.Language, "cpp": cpp.Language, "csharp": csharp.Language,
	"golang": golang.Language, "hcl": hcl.Language, "java": java.Language,
	"javascript": javascript.Language, "json": json.Language, "julia": julia.Language,
	"kotlin": kotlin.Language, "lua": lua.Language, "php": php.LanguagePHP,
	"python": python.Language, "ruby": ruby.Language, "rust": rust.Language,
	"scala": scala.Language, "typescript": typescript.LanguageTypescript,
	"tsx": typescript.LanguageTSX, "verilog": verilog.Language, "zig": zig.Language,
}

var (
	counting          bool
	cCalls, cBytesReq atomic.Int64
)

// countCAllocs counts through go-tree-sitter's allocator hook. The hook is a
// Go callback per call even by default, so this adds only the two counters.
func countCAllocs() {
	counting = true
	ts.SetAllocator(
		func(size uint) unsafe.Pointer {
			cCalls.Add(1)
			cBytesReq.Add(int64(size))
			return C.malloc(C.size_t(size))
		},
		func(num, size uint) unsafe.Pointer {
			cCalls.Add(1)
			cBytesReq.Add(int64(num * size))
			return C.calloc(C.size_t(num), C.size_t(size))
		},
		func(ptr unsafe.Pointer, size uint) unsafe.Pointer {
			cCalls.Add(1)
			cBytesReq.Add(int64(size))
			return C.realloc(ptr, C.size_t(size))
		},
		nil,
	)
}

func cAllocCounts() (calls, bytes int64) {
	if !counting {
		return -1, -1
	}
	return cCalls.Load(), cBytesReq.Load()
}

type cgoWorker struct {
	p    *ts.Parser
	tree *ts.Tree
}

func newWorker(grammar string) (worker, error) {
	f := langs[grammar]
	if f == nil {
		return nil, fmt.Errorf("no grammar %q", grammar)
	}
	p := ts.NewParser()
	if err := p.SetLanguage(ts.NewLanguage(f())); err != nil {
		return nil, err
	}
	return &cgoWorker{p: p}, nil
}

func (w *cgoWorker) parse(src []byte) bool {
	if w.tree != nil {
		w.tree.Close()
	}
	w.tree = w.p.Parse(src, nil)
	return w.tree.RootNode().HasError()
}

func visit(n *ts.Node, a *acc) {
	k := n.Kind()
	if len(k) > 0 {
		a.sum += uint64(k[0])
	}
	a.sum += uint64(n.StartByte()) + uint64(n.EndByte())
	a.nodes++
}

func (w *cgoWorker) walk(a *acc) { walk(w.tree.RootNode(), a) }

func walk(n *ts.Node, a *acc) {
	visit(n, a)
	cc := n.ChildCount()
	for i := uint(0); i < cc; i++ {
		walk(n.Child(i), a)
	}
}

func (w *cgoWorker) cursor(a *acc) {
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
