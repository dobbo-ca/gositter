package gositter_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

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
	"github.com/dobbo-ca/gositter/internal/treedump"
)

// Keys are the directory names under testdata/regress.
var regressLangs = map[string]func() *gositter.Language{
	"bash":       bash.Language,
	"c":          c.Language,
	"cpp":        cpp.Language,
	"c-sharp":    csharp.Language,
	"go":         golang.Language,
	"hcl":        hcl.Language,
	"java":       java.Language,
	"javascript": javascript.Language,
	"json":       json.Language,
	"julia":      julia.Language,
	"kotlin":     kotlin.Language,
	"lua":        lua.Language,
	"php":        php.Language,
	"python":     python.Language,
	"ruby":       ruby.Language,
	"rust":       rust.Language,
	"scala":      scala.Language,
	"tsx":        tsx.Language,
	"typescript": typescript.Language,
	"verilog":    verilog.Language,
	"zig":        zig.Language,
}

// The kinds each grammar names to KindMask: ERROR and up to 31 symbol names
// spread over the table, named here because a language takes them before it parses.
var regressKinds = map[string]map[string]uint32{}

func init() {
	for name, lang := range regressLangs {
		l := lang()
		kinds := map[string]uint32{"ERROR": l.KindMask("ERROR")}
		for i := 1; i < len(l.SymbolNames) && len(kinds) < 32; i += len(l.SymbolNames)/40 + 1 {
			kinds[l.SymbolNames[i]] = l.KindMask(l.SymbolNames[i])
		}
		regressKinds[name] = kinds
	}
}

// Replays testdata/regress/<grammar>/<file> against <file>.dump.gz, the dump
// the C runtime gave for it (oracle dump-c, see scratch/ident/mkregress.sh).
// Each grammar runs twice on one parser, the second time in reverse order, so
// state left over from another file shows up as well.
func TestRegress(t *testing.T) {
	for name, lang := range regressLangs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dumps, _ := filepath.Glob(filepath.Join("testdata", "regress", name, "*.dump.gz"))
			if len(dumps) == 0 {
				t.Fatal("no cases")
			}
			l := lang()
			var fields []string
			if len(l.FieldNames) > 0 {
				fields = l.FieldNames[1:]
			}
			p := gositter.NewParser()
			if err := p.SetLanguage(l); err != nil {
				t.Fatal(err)
			}
			withErrors, oneChild := 0, 0
			var summaries gositter.SummaryCheck
			for pass := 0; pass < 2; pass++ {
				for i := range dumps {
					if pass == 1 {
						i = len(dumps) - 1 - i
					}
					src, err := os.ReadFile(strings.TrimSuffix(dumps[i], ".dump.gz"))
					if err != nil {
						t.Fatal(err)
					}
					want := gunzip(t, dumps[i])
					root := p.Parse(src).RootNode()
					if pass == 0 && root.HasError() {
						withErrors++
					}
					if pass == 0 {
						checkChildren(t, dumps[i], root)
						checkPreorder(t, dumps[i], root)
						checkCursor(t, dumps[i], root)
						checkPreorderMask(t, dumps[i], root, regressKinds[name])
						if !p.RepeatFlagsOK() {
							t.Errorf("%s: fHasRepeat misses a repeat node", dumps[i])
						}
						n, ok := p.OneChildSummaryOK()
						if !ok {
							t.Errorf("%s: summarizeOneChild differs from summarizeChildren", dumps[i])
						}
						oneChild += n
						addSummaries(t, dumps[i], p, &summaries)
					}
					var got bytes.Buffer
					treedump.Dump(&got, treedump.Wrap(root), fields, src)
					if !bytes.Equal(got.Bytes(), want) {
						t.Errorf("%s (pass %d): %s", dumps[i], pass, firstDiff(want, got.Bytes()))
					}
				}
			}
			scans, ok := p.ErrorScansOK()
			if !ok {
				t.Error("a kept error scan differs from a fresh one")
			}
			t.Logf("%d cases, %d with errors, %d one-child nodes checked, %d error scans kept", len(dumps), withErrors, oneChild, scans)
			t.Logf("%d summaries checked, %d of two heap children, %d that depend on the column, %d resumed",
				summaries.Nodes, summaries.TwoHeap, summaries.Column, summaries.Resumed)
		})
	}
}

// Checks the summaries of the last tree of p and adds the counts to sum.
func addSummaries(t *testing.T, name string, p *gositter.Parser, sum *gositter.SummaryCheck) {
	c := p.SummariesOK()
	if c.Bad != "" {
		t.Errorf("%s: %s", name, c.Bad)
	}
	sum.Nodes += c.Nodes
	sum.TwoHeap += c.TwoHeap
	sum.Column += c.Column
	sum.Resumed += c.Resumed
}

// Shapes the summary paths split on, by hand: wide repeat nodes, extras
// between repeat children, ERROR nodes that grow by one recovery after
// another, errors inside errors, tokens that depend on the column. No C dump
// here: each summary is checked against the reference in export_test.go.
var summaryCases = []struct {
	name string
	lang func() *gositter.Language
	src  string
}{
	{"json wide", json.Language, "[" + strings.Repeat("1,", 6000) + "1]"},
	{"json pairs", json.Language, "{" + strings.Repeat(`"a":[{"b":null}],`, 2000) + `"z":0}`},
	{"json errors", json.Language, strings.Repeat("[1,,{]\"a\" 2}", 300)},
	{"c define", c.Language, strings.Repeat("#define A( ", 400)},
	{"c extras", c.Language, strings.Repeat("int a; /* c */ // d\n", 2000)},
	{"c nested errors", c.Language, strings.Repeat("int f( { if (x { #define B(\n ) y = ; } ", 200)},
	{"cpp define", cpp.Language, strings.Repeat("#define A( ", 400)},
	{"bash expansion", bash.Language, strings.Repeat("${", 400)},
	{"bash heredocs", bash.Language, strings.Repeat("cat <<-EOF\n\tx $y\n\tEOF\n", 300) + strings.Repeat("a=(1 2\n", 50)},
	{"hcl", hcl.Language, strings.Repeat("a = 1 # c\nb \"x\" {\n  c = [1, 2]\n}\n", 1000)},
	{"hcl errors", hcl.Language, strings.Repeat("a = { b = [ 1, \n", 300)},
	{"scala indent", scala.Language, strings.Repeat("def f(x: Int) =\n  if x > 0 then\n    x\n  else\n    -x\n", 300)},
	{"scala errors", scala.Language, strings.Repeat("object A:\n  def f =\n    val x = (\n  ]\n", 100)},
}

func TestSummaries(t *testing.T) {
	for _, tc := range summaryCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := gositter.NewParser()
			if err := p.SetLanguage(tc.lang()); err != nil {
				t.Fatal(err)
			}
			var sum gositter.SummaryCheck
			p.Parse([]byte(tc.src))
			addSummaries(t, tc.name, p, &sum)
			t.Logf("%d summaries checked, %d of two heap children, %d that depend on the column, %d resumed",
				sum.Nodes, sum.TwoHeap, sum.Column, sum.Resumed)
		})
	}
}

func gunzip(t *testing.T, path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func firstDiff(want, got []byte) string {
	w, g := bytes.Split(want, []byte("\n")), bytes.Split(got, []byte("\n"))
	for i := 0; i < len(w) && i < len(g); i++ {
		if !bytes.Equal(w[i], g[i]) {
			return "line " + strconv.Itoa(i+1) + "\n want: " + clip(w[i]) + "\n  got: " + clip(g[i])
		}
	}
	return "line counts differ: want " + strconv.Itoa(len(w)) + ", got " + strconv.Itoa(len(g))
}

func clip(b []byte) string { return string(b[:min(len(b), 400)]) }

// Children and NamedChildren must list exactly what Child(i) and
// NamedChild(i) return. Wide nodes are sampled, as in treedump.
func checkChildren(t *testing.T, name string, root gositter.Node) {
	var all, named []gositter.Node
	stack := []gositter.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		all = n.Children(all[:0])
		named = n.NamedChildren(named[:0])
		if uint32(len(all)) != n.ChildCount() || uint32(len(named)) != n.NamedChildCount() {
			t.Errorf("%s: %s at %d: Children %d of %d, NamedChildren %d of %d", name, n.Kind(), n.StartByte(),
				len(all), n.ChildCount(), len(named), n.NamedChildCount())
			return
		}
		for i, c := range all {
			if (len(all) <= 256 || i%37 == 0) && c != n.Child(uint32(i)) {
				t.Errorf("%s: %s at %d: Children[%d] differs from Child", name, n.Kind(), n.StartByte(), i)
				return
			}
		}
		for i, c := range named {
			if (len(named) <= 256 || i%37 == 0) && c != n.NamedChild(uint32(i)) {
				t.Errorf("%s: %s at %d: NamedChildren[%d] differs from NamedChild", name, n.Kind(), n.StartByte(), i)
				return
			}
		}
		// Every index and two past the end, against the loop Child had
		// before the one for a node with no alias sequence.
		for _, anonymous := range []bool{true, false} {
			count := uint32(len(named))
			if anonymous {
				count = uint32(len(all))
			}
			for i := uint32(0); i < count+2; i++ {
				if count > 2000 && i < count && i%37 != 0 {
					continue
				}
				got := n.NamedChild(i)
				if anonymous {
					got = n.Child(i)
				}
				if got != n.ChildRef(i, anonymous) {
					t.Errorf("%s: %s at %d: child %d of %d (anonymous %v) differs from ChildRef", name, n.Kind(), n.StartByte(), i, count, anonymous)
					return
				}
			}
		}
		// The dump compares a field's child by index only; this pins its
		// position and alias.
		for id := gositter.FieldID(1); uint32(id) <= n.Language().FieldCount; id++ {
			if n.ChildByFieldID(id) != n.ChildByFieldIDRef(id) {
				t.Errorf("%s: %s at %d: ChildByFieldID(%d) differs from the iterator form", name, n.Kind(), n.StartByte(), id)
				return
			}
		}
		stack = append(stack, all...)
	}
}

// A UTF-16 copy of a source parses to an ERROR node with thousands of
// children and no alias sequence: the node a Child(i) walk spends its time in.
func TestChildrenWide(t *testing.T) {
	for _, name := range []string{"json", "rust", "tsx", "typescript"} {
		paths, _ := filepath.Glob(filepath.Join("testdata", "regress", name, "*.dump.gz"))
		wide := []byte{0xFF, 0xFE}
		for _, path := range paths {
			src, err := os.ReadFile(strings.TrimSuffix(path, ".dump.gz"))
			if err != nil {
				t.Fatal(err)
			}
			for _, b := range src {
				wide = append(wide, b, 0)
			}
			if len(wide) > 60000 {
				break
			}
		}
		p := gositter.NewParser()
		if err := p.SetLanguage(regressLangs[name]()); err != nil {
			t.Fatal(err)
		}
		root := p.Parse(wide).RootNode()
		widest := uint32(0)
		root.Preorder(func(n gositter.Node) bool {
			widest = max(widest, n.ChildCount())
			return true
		})
		if widest < 5000 {
			t.Errorf("%s: widest node has %d children, want 5000 or more", name, widest)
		}
		checkChildren(t, name+" as UTF-16", root)
	}
}

// Preorder must visit what a recursive Children loop visits, in that order,
// and skip the nodes below one fn refuses (here: every third named node).
func checkPreorder(t *testing.T, name string, root gositter.Node) {
	skip := func(n gositter.Node) bool { return n.IsNamed() && n.StartByte()%3 == 0 }
	var want []gositter.Node
	var rec func(gositter.Node)
	rec = func(n gositter.Node) {
		want = append(want, n)
		if skip(n) {
			return
		}
		for _, c := range n.Children(nil) {
			rec(c)
		}
	}
	for _, c := range root.Children(nil) {
		rec(c)
	}
	i, bad := 0, false
	visit := func(n gositter.Node) bool {
		bad = bad || i >= len(want) || want[i] != n
		i++
		return !skip(n)
	}
	for _, c := range root.Children(nil) {
		c.Preorder(visit)
	}
	if bad || i != len(want) {
		t.Errorf("%s: Preorder visits %d nodes, the Children walk %d, or the order differs", name, i, len(want))
	}
	i, want = 0, want[:0]
	rec = func(n gositter.Node) {
		want = append(want, n)
		for _, c := range n.Children(nil) {
			rec(c)
		}
	}
	rec(root)
	skip = func(gositter.Node) bool { return false }
	root.Preorder(visit)
	if bad || i != len(want) {
		t.Errorf("%s: full Preorder visits %d nodes, the Children walk %d, or the order differs", name, i, len(want))
	}
}

// PreorderMask must call fn for the nodes of its kinds that Preorder visits
// when fn refuses some of them (here: every third), and for no other node.
func checkPreorderMask(t *testing.T, name string, root gositter.Node, kinds map[string]uint32) {
	skip := func(n gositter.Node) bool { return n.StartByte()%3 == 0 }
	var want, got []gositter.Node
	walk := func(mask uint32) {
		want, got = want[:0], got[:0]
		root.Preorder(func(n gositter.Node) bool {
			if kinds[n.Kind()]&mask == 0 {
				return true
			}
			want = append(want, n)
			return !skip(n)
		})
		root.PreorderMask(mask, func(n gositter.Node) bool {
			got = append(got, n)
			return !skip(n)
		})
		if !slices.Equal(got, want) {
			t.Errorf("%s: PreorderMask(%#x) calls fn on %d nodes, Preorder visits %d of those kinds, or they differ", name, mask, len(got), len(want))
		}
	}
	all := uint32(0)
	for _, bit := range kinds {
		walk(bit)
		all |= bit
	}
	walk(all)
	walk(all & 0x00ff00ff)
}

// A cursor must visit what Preorder visits, in that order, from the root and
// from inner nodes (one cursor, reused through Reset), report the node's parent on
// GotoParent, and name each field as the parent's FieldNameForChild does.
func checkCursor(t *testing.T, name string, root gositter.Node) {
	c := root.Walk()
	var all []gositter.Node
	root.Preorder(func(n gositter.Node) bool { all = append(all, n); return true })
	starts := []gositter.Node{root}
	for i := len(all) / 7; i < len(all) && len(starts) < 9; i += len(all)/7 + 1 {
		starts = append(starts, all[i])
	}
	for _, n := range all {
		if n.IsExtra() {
			starts = append(starts, n)
			break
		}
	}
	for _, start := range starts {
		var want []gositter.Node
		start.Preorder(func(n gositter.Node) bool { want = append(want, n); return true })
		c.Reset(start)
		var got []gositter.Node
		parents, index := []gositter.Node{}, []uint32{}
		visit := func() {
			n := c.Node()
			got = append(got, n)
			if d := len(index); d > 0 && (index[d-1] < 256 || index[d-1]%37 == 0) {
				if f, g := parents[d-1].FieldNameForChild(index[d-1]), c.FieldName(); f != g {
					t.Errorf("%s: %s at %d: FieldName %q, FieldNameForChild %q", name, n.Kind(), n.StartByte(), g, f)
				}
			}
		}
		visit()
		for done := false; !done; {
			if c.GotoFirstChild() {
				parents, index = append(parents, got[len(got)-1]), append(index, 0)
				visit()
				continue
			}
			for {
				if len(index) == 0 {
					done = true
					break
				}
				if c.GotoNextSibling() {
					index[len(index)-1]++
					visit()
					break
				}
				if !c.GotoParent() || c.Node() != parents[len(parents)-1] {
					t.Errorf("%s: GotoParent from %d does not reach the parent", name, start.StartByte())
					return
				}
				parents, index = parents[:len(parents)-1], index[:len(index)-1]
			}
		}
		if len(got) != len(want) {
			t.Errorf("%s: cursor from %s at %d visits %d nodes, Preorder %d", name, start.Kind(), start.StartByte(), len(got), len(want))
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: cursor from %s at %d differs from Preorder at node %d", name, start.Kind(), start.StartByte(), i)
				return
			}
		}
	}
}
