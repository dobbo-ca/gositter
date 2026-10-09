package treedump

// One dump routine for both runtimes, so a format difference can't hide or
// fake a tree difference.

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
)

// Node is a non-null Node of either runtime. A null Node is a nil interface.
type Node interface {
	Kind() string
	Symbol() uint16
	Named() bool
	Extra() bool
	Missing() bool
	IsError() bool
	HasError() bool
	Start() uint32
	End() uint32
	StartPt() (row, col uint32)
	EndPt() (row, col uint32)
	CC() uint32
	NCC() uint32
	Child(i uint32) Node
	NamedChild(i uint32) Node
	ByField(name string) Node
	FieldForChild(i uint32) string
	Parent() Node
	Eq(o Node) bool
	Text(src []byte) string
	Walk() Cursor
	Sexp() string
	ChildrenByField(name string, c Cursor) []Node
}

type Cursor interface {
	Node() Node
	Field() string
	FirstChild() bool
	Next() bool
	Up() bool
	Close()
}

// ts_node_child, ts_node_named_child, ts_node_field_name_for_child and
// ts_node_parent are linear in the index or the depth. Calling them for every
// Node of a 5 MB file or a 100000-deep tree is quadratic, so wide and deep
// nodes are sampled. The rule is fixed, both sides sample the same nodes.
func sampled(i, n uint32) bool {
	return n <= 256 || i < 128 || i >= n-64 || i%257 == 0
}

func ident(n Node) string {
	if n == nil {
		return "nil"
	}
	return fmt.Sprintf("%d:%d:%q", n.Start(), n.End(), n.Kind())
}

// indexFrom finds n among kids at or after from, by Node equality.
func indexFrom(kids []Node, n Node, from int) int {
	for i := from; i < len(kids); i++ {
		if kids[i].Eq(n) {
			return i
		}
	}
	return -1
}

type dumper struct {
	w        *bufio.Writer
	src      []byte
	fields   []string
	tc       Cursor // traversal
	scratch  Cursor // for childrenByField, which resets it
	ordinal  int
	maxDepth int
	// per-depth scratch, reused
	kids   [][]Node
	kfield [][]string
}

// dump writes one line per Node, pre-order:
//
//	depth "kind" symbol flags cursorField start-end row:col-row:col childCount namedChildCount [checks]
//
// flags are n(amed) x(extra) m(issing) e(rror) h(as_error), '-' when unset.
// Checks, each printed only when it has something to say:
//
//	P=ok|<Node>        Parent() against the real parent (sampled)
//	<field>=i[i,j]     ChildByFieldName -> child index, then ChildrenByFieldName
//	                   -> child indexes, for every field of the language
//	F<i>=<field>       FieldNameForChild(i) when non-empty (sampled)
//	C<i>=<Node>        Child(i) when it is not the Cursor's i-th child (sampled)
//	N=i,j,...          NamedChild(k) -> child index (sampled)
//	T!                 Utf8Text differs from src[start:end]
//	OOB=...            Child(cc), NamedChild(ncc), FieldNameForChild(cc) when not all null
//
// Returns the Node count.
func Dump(w io.Writer, root Node, fields []string, src []byte) int {
	// Own copy: the probe name must not land in the caller's backing array.
	d := &dumper{w: bufio.NewWriterSize(w, 1<<16), src: src}
	d.fields = append(append(d.fields, fields...), "nonexistent_zz")
	d.tc = root.Walk()
	d.scratch = root.Walk()
	d.visit(0, nil, 0)
	d.tc.Close()
	d.scratch.Close()
	// ts_node_string recurses per subtree level, hidden ones included, on a
	// 512 KB cgo thread stack: only call it on shallow trees.
	if d.maxDepth <= sexpMaxDepth {
		d.w.WriteString("SEXP " + root.Sexp() + "\n")
	} else {
		d.w.WriteString(SexpSkipped)
	}
	d.w.Flush()
	return d.ordinal
}

const (
	sexpMaxDepth = 100
	SexpSkipped  = "SEXP skipped\n"
)

func (d *dumper) visit(depth int, parent Node, indexInParent int) {
	d.maxDepth = max(d.maxDepth, depth)
	w := d.w
	n := d.tc.Node()
	ordinal := d.ordinal
	d.ordinal++

	// Pass 1 over the children with the Cursor: linear, and it is the
	// reference list the index-based accessors are checked against.
	if depth == len(d.kids) {
		d.kids = append(d.kids, nil)
		d.kfield = append(d.kfield, nil)
	}
	kids, kfield := d.kids[depth][:0], d.kfield[depth][:0]
	cursorField := d.tc.Field()
	if d.tc.FirstChild() {
		for {
			kids = append(kids, d.tc.Node())
			kfield = append(kfield, d.tc.Field())
			if !d.tc.Next() {
				break
			}
		}
		d.tc.Up()
	}
	d.kids[depth], d.kfield[depth] = kids, kfield

	flags := []byte("-----")
	for i, on := range []bool{n.Named(), n.Extra(), n.Missing(), n.IsError(), n.HasError()} {
		if on {
			flags[i] = "nxmeh"[i]
		}
	}
	if cursorField == "" {
		cursorField = "-"
	}
	sr, sc := n.StartPt()
	er, ec := n.EndPt()
	cc, ncc := n.CC(), n.NCC()
	fmt.Fprintf(w, "%d %q %d %s %s %d-%d %d:%d-%d:%d %d %d", depth, n.Kind(), n.Symbol(), flags,
		cursorField, n.Start(), n.End(), sr, sc, er, ec, cc, ncc)
	if int(cc) != len(kids) {
		fmt.Fprintf(w, " CURSORKIDS=%d", len(kids))
	}

	if (depth <= 64 && indexInParent <= 256) || ordinal%101 == 0 {
		p := n.Parent()
		switch {
		case p == nil && parent == nil:
		case p != nil && parent != nil && p.Eq(parent):
			w.WriteString(" P=ok")
		default:
			w.WriteString(" P=" + ident(p))
		}
	}

	for _, f := range d.fields {
		c := n.ByField(f)
		inCursor := false
		for _, kf := range kfield {
			if kf == f {
				inCursor = true
				break
			}
		}
		if c == nil && !inCursor {
			continue
		}
		w.WriteString(" " + f + "=")
		if c == nil {
			w.WriteString("nil")
		} else if i := indexFrom(kids, c, 0); i >= 0 {
			w.WriteString(strconv.Itoa(i))
		} else {
			w.WriteString(ident(c))
		}
		w.WriteByte('[')
		from := 0
		for k, c := range n.ChildrenByField(f, d.scratch) {
			if k > 0 {
				w.WriteByte(',')
			}
			if i := indexFrom(kids, c, from); i >= 0 {
				w.WriteString(strconv.Itoa(i))
				from = i + 1
			} else {
				w.WriteString(ident(c))
			}
		}
		w.WriteByte(']')
	}

	for i := uint32(0); i < cc; i++ {
		if !sampled(i, cc) {
			continue
		}
		if f := n.FieldForChild(i); f != "" {
			fmt.Fprintf(w, " F%d=%s", i, f)
		}
		if c := n.Child(i); c == nil || int(i) >= len(kids) || !c.Eq(kids[i]) {
			fmt.Fprintf(w, " C%d=%s", i, ident(c))
		}
	}
	if ncc > 0 {
		w.WriteString(" N=")
		from := 0
		for k := uint32(0); k < ncc; k++ {
			if !sampled(k, ncc) {
				from = 0 // a gap: the next hit can be anywhere
				continue
			}
			if k > 0 {
				w.WriteByte(',')
			}
			c := n.NamedChild(k)
			if c == nil {
				w.WriteString("nil")
			} else if i := indexFrom(kids, c, from); i >= 0 {
				w.WriteString(strconv.Itoa(i))
				from = i + 1
			} else {
				w.WriteString(ident(c))
			}
		}
	}
	if s, e := n.Start(), n.End(); s > e || int(e) > len(d.src) || n.Text(d.src) != string(d.src[s:e]) {
		w.WriteString(" T!")
	}
	if a, b, f := n.Child(cc), n.NamedChild(ncc), n.FieldForChild(cc); a != nil || b != nil || f != "" {
		fmt.Fprintf(w, " OOB=%s/%s/%s", ident(a), ident(b), f)
	}
	w.WriteByte('\n')

	// Pass 2: descend. kids is not read after this point, so the recursion
	// may reuse deeper scratch rows only.
	if d.tc.FirstChild() {
		for i := 0; ; i++ {
			d.visit(depth+1, n, i)
			if !d.tc.Next() {
				break
			}
		}
		d.tc.Up()
	}
}
