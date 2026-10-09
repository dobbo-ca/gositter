package main

// The C runtime side: go-tree-sitter v0.25.0 over cgo.

import (
	"fmt"
	"unsafe"

	"github.com/dobbo-ca/gositter/internal/treedump"

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

// Keys are the gositter package names (CONTRACT.md section 8).
var cLangs = map[string]func() unsafe.Pointer{
	"bash": bash.Language, "c": c.Language, "cpp": cpp.Language, "csharp": csharp.Language,
	"golang": golang.Language, "hcl": hcl.Language, "java": java.Language,
	"javascript": javascript.Language, "json": json.Language, "julia": julia.Language,
	"kotlin": kotlin.Language, "lua": lua.Language, "php": php.LanguagePHP,
	"python": python.Language, "ruby": ruby.Language, "rust": rust.Language,
	"scala": scala.Language, "typescript": typescript.LanguageTypescript,
	"tsx": typescript.LanguageTSX, "verilog": verilog.Language, "zig": zig.Language,
}

type cParser struct {
	p    *ts.Parser
	lang *ts.Language
	tree *ts.Tree
}

func openC(grammar string) (parser, error) {
	f := cLangs[grammar]
	if f == nil {
		return nil, fmt.Errorf("no C grammar %q", grammar)
	}
	lang := ts.NewLanguage(f())
	p := ts.NewParser()
	if err := p.SetLanguage(lang); err != nil {
		return nil, err
	}
	return &cParser{p: p, lang: lang}, nil
}

func (p *cParser) parse(src []byte) treedump.Node {
	if p.tree != nil {
		p.tree.Close()
	}
	p.tree = p.p.Parse(src, nil)
	return cWrap(p.tree.RootNode())
}

func (p *cParser) fields() []string {
	n := p.lang.FieldCount()
	out := make([]string, 0, n)
	for i := uint32(1); i <= n; i++ {
		out = append(out, p.lang.FieldNameForId(uint16(i)))
	}
	return out
}

func (p *cParser) setLogger(fn func(lex bool, msg string)) {
	p.p.SetLogger(func(t ts.LogType, msg string) { fn(t == ts.LogTypeLex, msg) })
}

func (p *cParser) walk() int { return cWalk(p.tree.RootNode()) }

func (p *cParser) close() {
	if p.tree != nil {
		p.tree.Close()
	}
	p.p.Close()
}

// Child recursion, the way graphify walks.
func cWalk(n *ts.Node) int {
	_, _, _ = n.Kind(), n.StartByte(), n.EndByte()
	count := 1
	cc := n.ChildCount()
	for i := uint(0); i < cc; i++ {
		count += cWalk(n.Child(i))
	}
	return count
}

type cNode struct{ n *ts.Node }

func cWrap(n *ts.Node) treedump.Node {
	if n == nil {
		return nil
	}
	return cNode{n}
}

func (n cNode) Kind() string                      { return n.n.Kind() }
func (n cNode) Symbol() uint16                    { return n.n.KindId() }
func (n cNode) Named() bool                       { return n.n.IsNamed() }
func (n cNode) Extra() bool                       { return n.n.IsExtra() }
func (n cNode) Missing() bool                     { return n.n.IsMissing() }
func (n cNode) IsError() bool                     { return n.n.IsError() }
func (n cNode) HasError() bool                    { return n.n.HasError() }
func (n cNode) Start() uint32                     { return uint32(n.n.StartByte()) }
func (n cNode) End() uint32                       { return uint32(n.n.EndByte()) }
func (n cNode) CC() uint32                        { return uint32(n.n.ChildCount()) }
func (n cNode) NCC() uint32                       { return uint32(n.n.NamedChildCount()) }
func (n cNode) Child(i uint32) treedump.Node      { return cWrap(n.n.Child(uint(i))) }
func (n cNode) NamedChild(i uint32) treedump.Node { return cWrap(n.n.NamedChild(uint(i))) }
func (n cNode) ByField(f string) treedump.Node    { return cWrap(n.n.ChildByFieldName(f)) }
func (n cNode) FieldForChild(i uint32) string     { return n.n.FieldNameForChild(i) }
func (n cNode) Parent() treedump.Node             { return cWrap(n.n.Parent()) }
func (n cNode) Eq(o treedump.Node) bool           { return n.n.Equals(*o.(cNode).n) }
func (n cNode) Text(src []byte) string            { return n.n.Utf8Text(src) }
func (n cNode) Walk() treedump.Cursor             { return cCursor{n.n.Walk()} }
func (n cNode) Sexp() string                      { return n.n.ToSexp() }
func (n cNode) StartPt() (uint32, uint32) {
	p := n.n.StartPosition()
	return uint32(p.Row), uint32(p.Column)
}
func (n cNode) EndPt() (uint32, uint32) {
	p := n.n.EndPosition()
	return uint32(p.Row), uint32(p.Column)
}
func (n cNode) ChildrenByField(f string, c treedump.Cursor) []treedump.Node {
	ns := n.n.ChildrenByFieldName(f, c.(cCursor).c)
	out := make([]treedump.Node, len(ns))
	for i := range ns {
		out[i] = cNode{&ns[i]}
	}
	return out
}

type cCursor struct{ c *ts.TreeCursor }

func (c cCursor) Node() treedump.Node { return cWrap(c.c.Node()) }
func (c cCursor) Field() string       { return c.c.FieldName() }
func (c cCursor) FirstChild() bool    { return c.c.GotoFirstChild() }
func (c cCursor) Next() bool          { return c.c.GotoNextSibling() }
func (c cCursor) Up() bool            { return c.c.GotoParent() }
func (c cCursor) Close()              { c.c.Close() }
