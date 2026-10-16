package gositter_test

import (
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/c"
	"github.com/dobbo-ca/gositter/grammars/cpp"
	"github.com/dobbo-ca/gositter/grammars/csharp"
	"github.com/dobbo-ca/gositter/grammars/golang"
	"github.com/dobbo-ca/gositter/grammars/json"
	"github.com/dobbo-ca/gositter/grammars/julia"
	"github.com/dobbo-ca/gositter/grammars/kotlin"
	"github.com/dobbo-ca/gositter/grammars/ruby"
	"github.com/dobbo-ca/gositter/grammars/rust"
	"github.com/dobbo-ca/gositter/grammars/verilog"
)

func parseJSON(t testing.TB, src string) (*gositter.Parser, gositter.Node) {
	p := gositter.NewParser()
	if err := p.SetLanguage(json.Language()); err != nil {
		t.Fatal(err)
	}
	return p, p.Parse([]byte(src)).RootNode()
}

// The trees for these inputs were compared node for node with the C runtime
// (oracle cmp); the strings pin them.
func TestParseJSONString(t *testing.T) {
	cases := []struct{ src, want string }{
		{`{"a": [1, true, null]}`,
			`(document (object (pair key: (string (string_content)) value: (array (number) (true) (null)))))`},
		{"\xef\xbb\xbf[]\n", `(document (array))`},
		{``, `(document)`},
		{`[1,`, `(document (ERROR (number)))`},
		{`{"a": }`, `(document (object (pair key: (string (string_content)) value: (MISSING number))))`},
		{`[1 2]`, `(document (array (ERROR (number)) (number)))`},
		{"[\x00]", `(document (array (ERROR (UNEXPECTED '\0'))))`},
	}
	for _, tc := range cases {
		_, root := parseJSON(t, tc.src)
		if got := root.String(); got != tc.want {
			t.Errorf("%q:\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
}

func TestNodeAPI(t *testing.T) {
	src := `{"k": [10, "s"]}`
	_, root := parseJSON(t, src)
	if root.Kind() != "document" || !root.IsNamed() || root.HasError() || root.StartByte() != 0 || root.EndByte() != uint32(len(src)) {
		t.Fatalf("root: %s %d-%d", root.Kind(), root.StartByte(), root.EndByte())
	}
	if !root.Parent().IsNull() {
		t.Fatal("root has a parent")
	}
	obj := root.Child(0)
	pair := obj.NamedChild(0)
	if obj.ChildCount() != 3 || obj.NamedChildCount() != 1 || pair.Kind() != "pair" {
		t.Fatalf("object children: %d %d %s", obj.ChildCount(), obj.NamedChildCount(), pair.Kind())
	}
	if !pair.Parent().Equals(obj) || !obj.Parent().Equals(root) {
		t.Fatal("Parent")
	}

	key := pair.ChildByFieldName("key")
	value := pair.ChildByFieldName("value")
	if key.Utf8Text([]byte(src)) != `"k"` || value.Kind() != "array" {
		t.Fatalf("fields: %q %s", key.Utf8Text([]byte(src)), value.Kind())
	}
	lang := root.Language()
	if id := lang.FieldIDForName("value"); id == 0 || !pair.ChildByFieldID(id).Equals(value) || lang.FieldNameForID(id) != "value" {
		t.Fatal("ChildByFieldID")
	}
	if !pair.ChildByFieldName("nope").IsNull() || lang.FieldIDForName("") != 0 || lang.FieldIDForName("valu") != 0 {
		t.Fatal("unknown field")
	}
	if pair.FieldNameForChild(0) != "key" || pair.FieldNameForChild(1) != "" || pair.FieldNameForChild(2) != "value" {
		t.Fatal("FieldNameForChild")
	}
	if p := value.StartPosition(); p != (gositter.Point{Row: 0, Column: 6}) {
		t.Fatalf("StartPosition = %+v", p)
	}
	if !obj.Child(99).IsNull() || !obj.NamedChild(1).IsNull() {
		t.Fatal("out of range child must be null")
	}

	c := pair.Walk()
	defer c.Close()
	got := pair.ChildrenByFieldName("value", c)
	if len(got) != 1 || !got[0].Equals(value) {
		t.Fatalf("ChildrenByFieldName = %v", got)
	}
	if len(pair.ChildrenByFieldName("nope", c)) != 0 {
		t.Fatal("ChildrenByFieldName on an unknown field")
	}

	// A cursor walk visits the same nodes as Child.
	c.Reset(value)
	var kinds []string
	for ok := c.GotoFirstChild(); ok; ok = c.GotoNextSibling() {
		kinds = append(kinds, c.Node().Kind())
	}
	if strings.Join(kinds, " ") != "[ number , string ]" {
		t.Fatalf("cursor walk = %v", kinds)
	}
	if !c.GotoParent() || !c.Node().Equals(value) || c.GotoParent() {
		t.Fatal("GotoParent")
	}
}

func TestParserReuseAndNoLanguage(t *testing.T) {
	if gositter.NewParser().Parse([]byte("1")) != nil {
		t.Fatal("Parse without a language must return nil")
	}
	p := gositter.NewParser()
	if err := p.SetLanguage(&gositter.Language{ABIVersion: 12}); err == nil {
		t.Fatal("ABI 12 accepted")
	}
	if err := p.SetLanguage(&gositter.Language{ABIVersion: 16}); err == nil {
		t.Fatal("ABI 16 accepted")
	}
	if err := p.SetLanguage(json.Language()); err != nil {
		t.Fatal(err)
	}
	inputs := []string{`[1, 2]`, `{"a": `, ``, `[[[[]]]]`, `[1, 2]`}
	var first string
	for i, in := range inputs {
		tree := p.Parse([]byte(in))
		s := tree.RootNode().String()
		if i == 0 {
			first = s
		}
		tree.Close()
		if i == len(inputs)-1 && s != first {
			t.Fatalf("same input, different tree after reuse:\n%s\n%s", first, s)
		}
	}
}

func TestLoggerMessages(t *testing.T) {
	p := gositter.NewParser()
	p.SetLanguage(json.Language())
	var parse, lex []string
	p.SetLogger(func(typ gositter.LogType, msg string) {
		if typ == gositter.LogTypeLex {
			lex = append(lex, msg)
		} else {
			parse = append(parse, msg)
		}
	})
	p.Parse([]byte("[1]\t"))
	if parse[0] != "new_parse" || parse[len(parse)-1] != "done" {
		t.Fatalf("first/last: %q %q", parse[0], parse[len(parse)-1])
	}
	all := strings.Join(parse, "\n")
	for _, want := range []string{
		"process version:0, version_count:1, state:1, row:0, col:0",
		"lexed_lookahead sym:[, size:1",
		"shift state:",
		"reduce sym:array, child_count:3",
		"accept",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("parse log lacks %q", want)
		}
	}
	lexAll := strings.Join(lex, "\n")
	for _, want := range []string{"consume character:'['", "skip character:9"} {
		if !strings.Contains(lexAll, want) {
			t.Errorf("lex log lacks %q", want)
		}
	}
	p.SetLogger(nil)
	p.Parse([]byte("[1]"))
}

// A warm parser must not allocate per parse.
func TestParseAllocs(t *testing.T) {
	p := gositter.NewParser()
	p.SetLanguage(json.Language())
	src := []byte(strings.Repeat(`{"a": [1, 2.5, true, null, "x\n"], "b": {"c": []}}`+"\n", 1))
	big := []byte("[" + strings.Repeat(`{"a": [1, 2.5, true, null, "x\n"], "b": {"c": []}},`, 500) + "0]")
	p.Parse(big)
	if n := testing.AllocsPerRun(20, func() { p.Parse(src) }); n != 0 {
		t.Errorf("small parse: %v allocs, want 0", n)
	}
	if n := testing.AllocsPerRun(5, func() { p.Parse(big) }); n != 0 {
		t.Errorf("big parse: %v allocs, want 0", n)
	}
}

func BenchmarkParseJSON(b *testing.B) {
	p := gositter.NewParser()
	p.SetLanguage(json.Language())
	src := []byte("[" + strings.Repeat(`{"a": [1, 2.5, true, null, "x\n"], "b": {"c": []}},`, 2000) + "0]")
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		p.Parse(src)
	}
}

// Inputs that never leave error recovery. Every array the recovery drops has
// to be reusable, also by an array of another size: these took 100 to 3000
// children slab entries per input byte when it was not, and at 100,000 bytes
// the 32-bit slab offset wrapped. limit is in slab entries per input byte.
func TestErrorRecoveryMemory(t *testing.T) {
	cases := []struct {
		name  string
		lang  *gositter.Language
		src   string
		limit int
	}{
		{"kotlin_lt", kotlin.Language(), strings.Repeat("<", 5000), 128},
		{"kotlin_rbracket", kotlin.Language(), strings.Repeat("]", 5000), 128},
		{"json_open", json.Language(), strings.Repeat(`{"a": "b`, 2500), 128},
		// An ERROR node that grows by a few children per token.
		{"c_define", c.Language(), strings.Repeat("#define A( ", 1000), 16},
		{"cpp_define", cpp.Language(), strings.Repeat("#define A( ", 1000), 16},
		{"c_comment_end", c.Language(), strings.Repeat("*/\n", 1000), 64},
		{"cpp_comment_end", cpp.Language(), strings.Repeat("*/\n", 1000), 64},
		{"csharp_comment_end", csharp.Language(), strings.Repeat("*/\n", 1000), 64},
		{"go_comment_end", golang.Language(), strings.Repeat("*/\n", 1000), 64},
		{"c_comment_start", c.Language(), strings.Repeat("/*\n", 1000), 64},
		{"cpp_comment_start", cpp.Language(), strings.Repeat("/*\n", 1000), 64},
		{"csharp_comment_start", csharp.Language(), strings.Repeat("/*\n", 1000), 64},
		{"go_comment_start", golang.Language(), strings.Repeat("/*\n", 1000), 64},
		{"csharp_string", csharp.Language(), strings.Repeat(`"a `, 3000), 16},
		{"csharp_verbatim_string", csharp.Language(), strings.Repeat(`@"a `, 3000), 16},
		{"csharp_raw_string", csharp.Language(), strings.Repeat("\"\"\"\n", 3000), 16},
		{"ruby_symbol", ruby.Language(), strings.Repeat(`:"a `, 3000), 16},
		{"verilog_module", verilog.Language(), strings.Repeat("module\n", 3000), 16},
		{"rust_raw_string", rust.Language(), strings.Repeat(`r#"a `, 3000), 16},
		// Reduces between the frees of error recovery.
		{"go_comment_end_string", golang.Language(), strings.Repeat(strings.Repeat("*/\n", 3)+strings.Repeat(`"a `, 5), 1500), 16},
		// 64 paths at the end of input: accept holds one array at a time.
		{"cpp_template", cpp.Language(), strings.Repeat("std::vector<std::map<int, x = 1;\n", 1000), 4},
		{"rust_match_arm", rust.Language(), strings.Repeat("x => {\n", 3000), 128},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := gositter.NewParser()
			p.SetLanguage(c.lang)
			src := []byte(c.src)
			root := p.Parse(src).RootNode()
			if !root.HasError() || root.EndByte() != uint32(len(src)) {
				t.Fatalf("root: error=%v end=%d", root.HasError(), root.EndByte())
			}
			if got, limit := p.ChildrenSlabCap(), c.limit*len(src); got > limit {
				t.Errorf("children slab: %d entries, limit %d", got, limit)
			}
			// Not checked where the external scanner allocates on every parse,
			// nor under the race detector: there sync.Pool drops one Put in
			// four, so the heap chunk pool runs short, and the compiler
			// allocates the make in stack.moreLink's append.
			if !raceEnabled && testing.AllocsPerRun(1, func() { p.Parse(nil) }) == 0 {
				if n := testing.AllocsPerRun(1, func() { p.Parse(src) }); n != 0 {
					t.Errorf("second parse: %v allocs, want 0", n)
				}
			}
		})
	}
}

// A reset hands the heap chunks of a large parse to the pool all parsers
// share. A parser that takes them over as they are must build the same tree.
func TestHeapChunksShared(t *testing.T) {
	src := []byte(strings.Repeat("func f() { g(1, 2, 3) }\n", 5000))
	other := []byte(strings.Repeat("var x = []int{1, 2, 3}\n", 5000))
	a, b := gositter.NewParser(), gositter.NewParser()
	a.SetLanguage(golang.Language())
	b.SetLanguage(golang.Language())
	want := a.Parse(src).RootNode().String()
	if n := a.HeapChunks(); n <= 2*gositter.KeptHeapChunks {
		t.Fatalf("input too small: %d chunks", n)
	}
	a.Reset()
	if n := a.HeapChunks(); n != gositter.KeptHeapChunks {
		t.Errorf("after reset: %d chunks, want %d", n, gositter.KeptHeapChunks)
	}
	b.Parse(other)
	b.Reset()
	if got := b.Parse(src).RootNode().String(); got != want {
		t.Error("tree differs on chunks another parse has used")
	}
	if got := a.Parse(src).RootNode().String(); got != want {
		t.Error("tree differs after the chunks went through the pool")
	}
}

// A reset keeps the children slab of a large parse for the next one, and
// drops it once a parse has used a small part of it.
func TestChildrenSlabDropped(t *testing.T) {
	large := []byte(strings.Repeat("func f() { g(1, 2, 3) }\n", 20000))
	small := []byte("func f() { g(1, 2, 3) }\n")
	p := gositter.NewParser()
	p.SetLanguage(golang.Language())
	p.Parse(large)
	n := p.ChildrenSlabCap()
	if n <= gositter.ShrinkChildren {
		t.Fatalf("input too small: slab of %d entries", n)
	}
	p.Reset()
	p.Reset()
	if got := p.ChildrenSlabCap(); got != n {
		t.Errorf("after the large parse: slab of %d entries, want %d", got, n)
	}
	want := p.Parse(small).RootNode().String()
	p.Reset()
	if got := p.ChildrenSlabCap(); got >= gositter.ShrinkChildren {
		t.Errorf("after the small parse: slab of %d entries, want it dropped", got)
	}
	if got := p.Parse(small).RootNode().String(); got != want {
		t.Error("tree differs on the new slab")
	}
	q := gositter.NewParser()
	q.SetLanguage(golang.Language())
	wantLarge := q.Parse(large).RootNode().String()
	if got := p.Parse(large).RootNode().String(); got != wantLarge {
		t.Error("large tree differs on the regrown slab")
	}
	// The dropped slab comes back from the free lists until a GC frees it.
	// The race detector allocates in the heap chunk pool.
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	cycle := func() { p.Parse(small); p.Parse(small); p.Parse(large) }
	cycle()
	if n := testing.AllocsPerRun(3, cycle); !raceEnabled && n != 0 {
		t.Errorf("drop and regrow: %v allocs, want 0", n)
	}
}

// stallOf finds the smallest limit that lets src parse, by bisection.
func stallOf(t *testing.T, lang *gositter.Language, src []byte) uint32 {
	p := gositter.NewParser()
	p.SetLanguage(lang)
	lo, hi := uint32(1), uint32(1<<20)
	if p.ParseBudget(src, hi) == nil {
		t.Fatalf("stops at %d", hi)
	}
	for lo < hi {
		if mid := (lo + hi) / 2; p.ParseBudget(src, mid) != nil {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	if lo < 3 {
		t.Fatalf("input too simple: stall %d", lo)
	}
	return lo
}

// An unclosed julia macro block retries the same byte without end: 14 s and
// 1.4 GB with no limit. The stop must leave nothing behind, in this parser
// or in the heap chunks it hands to the others.
func TestParseBudgetStops(t *testing.T) {
	stuck := []byte("@testset \"t0\" begin\n    @testset \"t1\" begin\n        l2")
	src := []byte(strings.Repeat("func f() { g(1, 2, 3) }\n", 5000))
	a, b := gositter.NewParser(), gositter.NewParser()
	b.SetLanguage(golang.Language())
	want := b.Parse(src).RootNode().String()
	// progress resets the count
	if b.ParseBudget(src, 1) == nil {
		t.Fatal("clean input stopped at limit 1")
	}
	b.Reset()

	a.SetLanguage(julia.Language())
	if a.ParseBudget(stuck, 65536) != nil {
		t.Fatal("parsed, want a stop")
	}
	if n := a.HeapChunks(); n != gositter.KeptHeapChunks {
		t.Errorf("after the stop: %d chunks, want %d", n, gositter.KeptHeapChunks)
	}
	if got := b.Parse(src).RootNode().String(); got != want {
		t.Error("tree differs on another parser after the stop")
	}
	a.SetLanguage(golang.Language())
	if got := a.Parse(src).RootNode().String(); got != want {
		t.Error("tree differs on the stopped parser")
	}
}

// The limit is exact, and at the limit the tree is the one Parse builds.
func TestParseBudgetBoundary(t *testing.T) {
	src := []byte(strings.Repeat("func f() { g(1, 2, 3 }\n", 100))
	m := stallOf(t, golang.Language(), src)
	p := gositter.NewParser()
	p.SetLanguage(golang.Language())
	want := p.Parse(src).RootNode().String()
	if p.ParseBudget(src, m-1) != nil {
		t.Errorf("limit %d: parsed, want a stop", m-1)
	}
	if tree := p.ParseBudget(src, m); tree == nil {
		t.Errorf("limit %d: stopped", m)
	} else if tree.RootNode().String() != want {
		t.Errorf("limit %d: tree differs from Parse", m)
	}
}

// The count depends on the language and the input only, not on what the
// parser did before.
func TestParseBudgetCountStable(t *testing.T) {
	cases := []struct {
		name string
		lang *gositter.Language
		src  string
	}{
		// 17924: the count grows with the input, 8.5 per byte from here.
		{"kotlin_lt", kotlin.Language(), strings.Repeat("<", 2500)},
		{"rust_match_arm", rust.Language(), strings.Repeat("x => {\n", 3000)},
	}
	for _, c := range cases {
		src := []byte(c.src)
		large := []byte(strings.Repeat(c.src, 2))
		m := stallOf(t, c.lang, src)
		t.Logf("%s: stall %d", c.name, m)
		before := map[string]func(p *gositter.Parser){
			"fresh":   func(p *gositter.Parser) {},
			"reused":  func(p *gositter.Parser) { p.Parse(large) },
			"stopped": func(p *gositter.Parser) { p.ParseBudget(large, 1) },
		}
		for name, prep := range before {
			for _, limit := range []uint32{m - 1, m} {
				p := gositter.NewParser()
				p.SetLanguage(c.lang)
				prep(p)
				if got := p.ParseBudget(src, limit) != nil; got != (limit == m) {
					t.Errorf("%s, %s parser, limit %d of %d: parsed=%v", c.name, name, limit, m, got)
				}
			}
		}
	}
}

// A deep stack's slabs go to the free lists at reset, as children slabs, and
// a warm parser takes them back without allocating.
func TestStackSlabsDropped(t *testing.T) {
	deep := []byte("func f() {" + strings.Repeat("g(", 10000))
	small := []byte("func f() { g(1, 2, 3) }\n")
	q := gositter.NewParser()
	q.SetLanguage(golang.Language())
	want := q.Parse(deep).RootNode().String()
	p := gositter.NewParser()
	p.SetLanguage(golang.Language())
	p.Parse(deep)
	if n := p.StackSlabCap(); n <= gositter.ShrinkNodes {
		t.Fatalf("input too shallow: stack slab of %d entries", n)
	}
	p.Parse(small)
	p.Reset()
	if n := p.StackSlabCap(); n > gositter.ShrinkNodes {
		t.Errorf("after the small parse: stack slab of %d entries, want it dropped", n)
	}
	if p.Parse(deep).RootNode().String() != want {
		t.Error("deep tree differs on the pooled slabs")
	}
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	cycle := func() { p.Parse(small); p.Parse(small); p.Parse(deep) }
	cycle()
	if n := testing.AllocsPerRun(3, cycle); !raceEnabled && n != 0 {
		t.Errorf("drop and regrow: %v allocs, want 0", n)
	}
}

// After a huge input, a few small parses and two GCs free its slabs: no
// parser and no free list holds them.
func TestHugeSlabsFreed(t *testing.T) {
	deep := []byte("func f() {" + strings.Repeat("g(", 10000) + strings.Repeat("func f() { g(1, 2, 3) }\n", 20000))
	small := []byte("func f() { g(1, 2, 3) }\n")
	p := gositter.NewParser()
	p.SetLanguage(golang.Language())
	p.Parse(deep)
	if p.ChildrenSlabCap() <= gositter.ShrinkChildren || p.StackSlabCap() <= gositter.ShrinkNodes {
		t.Fatalf("input too small: slabs of %d and %d entries", p.ChildrenSlabCap(), p.StackSlabCap())
	}
	freed := p.SlabsFreed()
	for range gositter.KeepResets + 2 {
		p.Parse(small)
	}
	runtime.GC()
	runtime.GC()
	if !freed() {
		t.Error("the huge input's slabs outlived two GCs")
	}
	runtime.KeepAlive(p)
}

// A deep input's stack link slab, given up by a reset, outlives outside
// GCs until KeepResets parses end.
func TestKeptLinkSlabOutlivesGC(t *testing.T) {
	deep := []byte("func f() {" + strings.Repeat("g(", 10000))
	small := []byte("func f() { g(1, 2, 3) }\n")
	p := gositter.NewParser()
	p.SetLanguage(golang.Language())
	p.Parse(deep)
	if n := p.LinkSlabCap(); n <= 4*gositter.ShrinkLinks {
		t.Fatalf("input too small: link slab of %d", n)
	}
	alive := p.LinkSlabAlive()
	p.Parse(small)
	p.Parse(small)
	runtime.GC()
	runtime.GC()
	if !alive() {
		t.Fatal("a GC freed the kept link slab")
	}
	for range gositter.KeepResets + 1 {
		p.Parse(small)
	}
	runtime.GC()
	runtime.GC()
	if alive() {
		t.Error("the link slab outlived KeepResets parses and two GCs")
	}
	runtime.KeepAlive(p)
}

// A small parse that adds no stack link must still give up a huge link slab.
func TestHugeLinkSlabFreedByLinklessParses(t *testing.T) {
	deep := []byte("func f() {" + strings.Repeat("g(", 10000) + strings.Repeat("func f() { g(1, 2, 3) }\n", 20000))
	p := gositter.NewParser()
	p.SetLanguage(golang.Language())
	p.Parse(deep)
	freed := p.SlabsFreed()
	for range gositter.KeepResets + 2 {
		p.Parse([]byte("package p\n\nvar x = 1\n"))
	}
	p.Reset()
	runtime.GC()
	runtime.GC()
	if !freed() {
		t.Error("the huge input's slabs outlived two GCs")
	}
	runtime.KeepAlive(p)
}

// After a huge input gives up its slabs, a small parse takes slabs of
// at most 4 times its need.
func TestSmallParseTakesSmallSlabs(t *testing.T) {
	huge := []byte("func f() {" + strings.Repeat("g(", 10000) + strings.Repeat("func f() { g(1, 2, 3) }\n", 20000))
	small := []byte("func f() { g(1, 2, 3) }\n")
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	p := gositter.NewParser()
	p.SetLanguage(golang.Language())
	p.Parse(huge)
	if p.ChildrenSlabCap() <= gositter.ShrinkChildren || p.StackSlabCap() <= gositter.ShrinkNodes {
		t.Fatalf("input too small: slabs of %d and %d entries", p.ChildrenSlabCap(), p.StackSlabCap())
	}
	p.Parse(small)
	p.Reset() // the huge slabs are given up here
	p.Parse(small)
	if n := p.ChildrenSlabCap(); n > 4*4096 {
		t.Errorf("children slab of %d entries for a small parse", n)
	}
	if n := p.StackSlabCap(); n > 4*64 {
		t.Errorf("stack slab of %d entries for a small parse", n)
	}
}

// Parsers on 8 goroutines pass slabs through the shared free lists. Each
// tree must be the one a lone parser builds.
func TestSlabsAcrossParsers(t *testing.T) {
	inputs := [][]byte{
		[]byte("func f() {" + strings.Repeat("g(", 3000)),
		[]byte(strings.Repeat("func f() { g(1, 2, 3) }\n", 8000)),
		[]byte("func f() { g(1, 2, 3) }\n"),
		[]byte("package p\nvar x = [...]int{1, 2, 3}\n"),
		[]byte("func f() {" + strings.Repeat("g(", 9000) + strings.Repeat("func f() { g(1, 2, 3) }\n", 3000)),
	}
	lone := gositter.NewParser()
	lone.SetLanguage(golang.Language())
	want := make([]string, len(inputs))
	for i, in := range inputs {
		want[i] = lone.Parse(in).RootNode().String()
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := gositter.NewParser()
			p.SetLanguage(golang.Language())
			for i := 0; i < 3*len(inputs); i++ {
				k := (i + g) % len(inputs)
				if got := p.Parse(inputs[k]).RootNode().String(); got != want[k] {
					t.Errorf("goroutine %d: input %d differs", g, k)
					return
				}
				runtime.Gosched()
			}
		}()
	}
	wg.Wait()
}
