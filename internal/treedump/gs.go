package treedump

import "github.com/dobbo-ca/gositter"

type gNode struct{ n gositter.Node }

// Wrap adapts a gositter node; a null node becomes a nil interface.
func Wrap(n gositter.Node) Node {
	if n.IsNull() {
		return nil
	}
	return gNode{n}
}

func (n gNode) Kind() string                  { return n.n.Kind() }
func (n gNode) Symbol() uint16                { return uint16(n.n.Symbol()) }
func (n gNode) Named() bool                   { return n.n.IsNamed() }
func (n gNode) Extra() bool                   { return n.n.IsExtra() }
func (n gNode) Missing() bool                 { return n.n.IsMissing() }
func (n gNode) IsError() bool                 { return n.n.IsError() }
func (n gNode) HasError() bool                { return n.n.HasError() }
func (n gNode) Start() uint32                 { return n.n.StartByte() }
func (n gNode) End() uint32                   { return n.n.EndByte() }
func (n gNode) CC() uint32                    { return n.n.ChildCount() }
func (n gNode) NCC() uint32                   { return n.n.NamedChildCount() }
func (n gNode) Child(i uint32) Node           { return Wrap(n.n.Child(i)) }
func (n gNode) NamedChild(i uint32) Node      { return Wrap(n.n.NamedChild(i)) }
func (n gNode) ByField(f string) Node         { return Wrap(n.n.ChildByFieldName(f)) }
func (n gNode) FieldForChild(i uint32) string { return n.n.FieldNameForChild(i) }
func (n gNode) Parent() Node                  { return Wrap(n.n.Parent()) }
func (n gNode) Eq(o Node) bool                { return n.n.Equals(o.(gNode).n) }
func (n gNode) Text(src []byte) string        { return n.n.Utf8Text(src) }
func (n gNode) Walk() Cursor                  { return gCursor{n.n.Walk()} }
func (n gNode) Sexp() string                  { return n.n.String() }
func (n gNode) StartPt() (uint32, uint32) {
	p := n.n.StartPosition()
	return p.Row, p.Column
}
func (n gNode) EndPt() (uint32, uint32) {
	p := n.n.EndPosition()
	return p.Row, p.Column
}
func (n gNode) ChildrenByField(f string, c Cursor) []Node {
	ns := n.n.ChildrenByFieldName(f, c.(gCursor).c)
	out := make([]Node, len(ns))
	for i := range ns {
		out[i] = gNode{ns[i]}
	}
	return out
}

type gCursor struct{ c *gositter.TreeCursor }

func (c gCursor) Node() Node       { return Wrap(c.c.Node()) }
func (c gCursor) Field() string    { return c.c.FieldName() }
func (c gCursor) FirstChild() bool { return c.c.GotoFirstChild() }
func (c gCursor) Next() bool       { return c.c.GotoNextSibling() }
func (c gCursor) Up() bool         { return c.c.GotoParent() }
func (c gCursor) Close()           { c.c.Close() }
