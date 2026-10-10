package gositter_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
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
						if !p.RepeatFlagsOK() {
							t.Errorf("%s: fHasRepeat misses a repeat node", dumps[i])
						}
						n, ok := p.OneChildSummaryOK()
						if !ok {
							t.Errorf("%s: summarizeOneChild differs from summarizeChildren", dumps[i])
						}
						oneChild += n
					}
					var got bytes.Buffer
					treedump.Dump(&got, treedump.Wrap(root), fields, src)
					if !bytes.Equal(got.Bytes(), want) {
						t.Errorf("%s (pass %d): %s", dumps[i], pass, firstDiff(want, got.Bytes()))
					}
				}
			}
			t.Logf("%d cases, %d with errors, %d one-child nodes checked", len(dumps), withErrors, oneChild)
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
		stack = append(stack, all...)
	}
}
