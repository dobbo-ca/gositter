package gositter_test

import (
	"strings"
	"testing"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/c"
	"github.com/dobbo-ca/gositter/grammars/cpp"
	"github.com/dobbo-ca/gositter/grammars/csharp"
	"github.com/dobbo-ca/gositter/grammars/golang"
	"github.com/dobbo-ca/gositter/grammars/json"
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
		// 64 pop-all arrays that double together.
		{"cpp_template", cpp.Language(), strings.Repeat("std::vector<std::map<int, x = 1;\n", 1000), 96},
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
			// Not checked where the external scanner allocates on every parse.
			if testing.AllocsPerRun(1, func() { p.Parse(nil) }) == 0 {
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
