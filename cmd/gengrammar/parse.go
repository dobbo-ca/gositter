package main

import (
	"slices"
	"strconv"
	"strings"
)

type enumDecl struct {
	name  string
	names []string
	vals  []int64
}

// table is one static array, flattened row-major. Struct elements take one
// value per field.
type table struct {
	name   string
	ctype  string
	dims   []int // as declared; 0 is an open dimension
	fields []string
	vals   []int64
	strs   []string // ctype "char*" only
}

func (t *table) width() int {
	w := max(1, len(t.fields))
	for _, d := range t.dims[1:] {
		w *= d
	}
	return w
}

func (t *table) len() int {
	if t.ctype == "char*" {
		return len(t.strs)
	}
	return len(t.vals) / t.width()
}

type lexStmt struct {
	kind   byte // 'i' if, 'm' ADVANCE_MAP, 'a' ACCEPT_TOKEN, 'e' END_STATE
	cond   string
	skip   bool
	target int64
	sym    string
	pairs  []int64 // key, state, key, state, ...
}

type lexCase struct {
	state int64
	stmts []lexStmt
}

type lexFunc struct {
	cname string
	cases []lexCase
}

type grammar struct {
	name    string // the <name> of tree_sitter_<name>
	defines map[string]int64
	consts  map[string]int64 // enum members and builtin symbols
	enums   []enumDecl
	tables  map[string]*table
	order   []string // table names in source order
	actions []int64  // ts_parse_actions, 5 values per slot
	lexFns  []*lexFunc
	lang    map[string][]token // TSLanguage initialiser, nested keys joined with "."
}

// struct layouts from parser.h, in field order
var structFields = map[string][]string{
	"TSSymbolMetadata": {"visible", "named", "supertype"},
	"TSFieldMapSlice":  {"index", "length"},
	"TSMapSlice":       {"index", "length"},
	"TSFieldMapEntry":  {"field_id", "child_index", "inherited"},
	"TSLexMode":        {"lex_state", "external_lex_state"},
	"TSLexerMode":      {"lex_state", "external_lex_state", "reserved_word_set_id"},
	"TSCharacterRange": {"start", "end"},
}

var scalarTypes = []string{"TSSymbol", "TSStateId", "uint16_t", "uint32_t", "bool"}

type parser struct {
	tz     tokenizer
	tok    token
	consts map[string]int64
	g      *grammar
}

func parseGrammar(src string) (g *grammar, err error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(genError)
			if !ok {
				panic(r)
			}
			err = e
		}
	}()
	p := &parser{
		tz: tokenizer{src: src, defines: map[string]int64{}},
		consts: map[string]int64{
			"ts_builtin_sym_end":          0,
			"ts_builtin_sym_error":        0xFFFF,
			"ts_builtin_sym_error_repeat": 0xFFFE,
		},
		g: &grammar{tables: map[string]*table{}, lang: map[string][]token{}},
	}
	p.g.defines, p.g.consts = p.tz.defines, p.consts
	p.next()
	for p.tok.kind != tEOF {
		switch {
		case p.accept("enum"):
			p.enum()
		case p.accept("static"):
			p.static()
		case p.accept("extern"): // extern "C" {
			p.next()
			p.expect("{")
		case p.accept("}"):
		case p.accept("TS_PUBLIC"):
			p.language()
		case p.is("void"), p.is("unsigned"), p.is("bool"): // scanner prototypes
			for !p.accept(";") {
				p.next()
			}
		default:
			p.fail("unexpected %q at top level", p.tok.s)
		}
	}
	return p.g, nil
}

func (e genError) Error() string { return e.msg }

func (p *parser) fail(format string, args ...any) { p.tz.fail(p.tok.pos, format, args...) }

func (p *parser) next() { p.tok = p.tz.next() }

func (p *parser) is(s string) bool {
	return (p.tok.kind == tIdent || p.tok.kind == tPunct) && p.tok.s == s
}

func (p *parser) accept(s string) bool {
	if p.is(s) {
		p.next()
		return true
	}
	return false
}

func (p *parser) expect(ss ...string) {
	for _, s := range ss {
		if !p.accept(s) {
			p.fail("expected %q, found %q", s, p.tok.s)
		}
	}
}

func (p *parser) ident() string {
	if p.tok.kind != tIdent {
		p.fail("expected identifier, found %q", p.tok.s)
	}
	s := p.tok.s
	p.next()
	return s
}

// comma consumes the separator after a list item and reports whether the
// list goes on.
func (p *parser) comma(closer string) bool {
	if p.accept(",") {
		return !p.accept(closer)
	}
	p.expect(closer)
	return false
}

// expr evaluates the constant expressions parser.c uses in initialisers.
func (p *parser) expr() int64 {
	t := p.tok
	switch t.kind {
	case tNum, tChar:
		p.next()
		return t.v
	case tIdent:
		p.next()
		switch t.s {
		case "true":
			return 1
		case "false":
			return 0
		case "ACTIONS", "STATE": // identity macros
			p.expect("(")
			v := p.expr()
			p.expect(")")
			return v
		case "SMALL_STATE":
			p.expect("(")
			v := p.expr()
			p.expect(")")
			return v - p.define("LARGE_STATE_COUNT")
		}
		if v, ok := p.consts[t.s]; ok {
			return v
		}
		if v, ok := p.tz.defines[t.s]; ok {
			return v
		}
		p.tz.fail(t.pos, "unknown identifier %q", t.s)
	case tPunct:
		switch t.s {
		case "-":
			p.next()
			return -p.expr()
		case "(":
			p.next()
			if p.accept("TSStateId") || p.accept("uint16_t") {
				p.expect(")")
				return p.expr() & 0xFFFF
			}
			v := p.expr()
			p.expect(")")
			return v
		}
	}
	p.fail("unexpected %q in expression", t.s)
	return 0
}

func (p *parser) define(name string) int64 {
	v, ok := p.tz.defines[name]
	if !ok {
		p.fail("missing #define %s", name)
	}
	return v
}

func (p *parser) enum() {
	e := enumDecl{name: p.ident()}
	p.expect("{")
	next := int64(0)
	for !p.accept("}") {
		name := p.ident()
		if p.accept("=") {
			next = p.expr()
		}
		if _, dup := p.consts[name]; dup {
			p.fail("duplicate constant %s", name)
		}
		p.consts[name] = next
		e.names = append(e.names, name)
		e.vals = append(e.vals, next)
		next++
		if !p.comma("}") {
			break
		}
	}
	p.expect(";")
	p.g.enums = append(p.g.enums, e)
}

func (p *parser) static() {
	if p.accept("bool") {
		p.lexFn()
		return
	}
	p.accept("const") // ABI 14 character sets are not const
	t := &table{}
	if p.accept("char") {
		p.expect("*", "const")
		t.ctype = "char*"
	} else {
		t.ctype = p.ident()
	}
	t.name = p.ident()
	for p.accept("[") {
		d := 0
		if !p.is("]") {
			d = int(p.expr())
		}
		p.expect("]")
		t.dims = append(t.dims, d)
	}
	if len(t.dims) == 0 || slices.Contains(t.dims[1:], 0) {
		p.fail("%s: unsupported dimensions", t.name)
	}
	p.expect("=")
	if _, dup := p.g.tables[t.name]; dup {
		p.fail("duplicate table %s", t.name)
	}
	switch {
	case t.ctype == "char*":
		p.strings(t)
	case t.ctype == "TSParseActionEntry":
		p.parseActions()
	case structFields[t.ctype] != nil || slices.Contains(scalarTypes, t.ctype):
		t.fields = structFields[t.ctype]
		t.vals = make([]int64, t.dims[0]*t.width())
		p.init(t, 0, t.dims)
	default:
		p.fail("%s: unknown element type %s", t.name, t.ctype)
	}
	p.expect(";")
	p.g.tables[t.name] = t
	p.g.order = append(p.g.order, t.name)
}

// init reads one initialiser into t.vals[off:]. Like C, a designator moves
// the cursor and the items after it continue from there.
func (p *parser) init(t *table, off int, dims []int) {
	if len(dims) == 0 && t.fields == nil {
		t.vals[off] = p.expr()
		return
	}
	p.expect("{")
	if p.accept("}") {
		return
	}
	if len(dims) == 0 {
		for fi := 0; ; fi++ {
			if p.accept(".") {
				name := p.ident()
				if fi = slices.Index(t.fields, name); fi < 0 {
					p.fail("%s has no field %s", t.ctype, name)
				}
				p.expect("=")
			}
			if fi >= len(t.fields) {
				p.fail("too many initialisers for %s", t.ctype)
			}
			t.vals[off+fi] = p.expr()
			if !p.comma("}") {
				return
			}
		}
	}
	stride := max(1, len(t.fields))
	for _, d := range dims[1:] {
		stride *= d
	}
	for idx := 0; ; idx++ {
		if p.accept("[") {
			idx = int(p.expr())
			p.expect("]", "=")
		}
		if idx < 0 || dims[0] != 0 && idx >= dims[0] {
			p.fail("%s: index %d out of range", t.name, idx)
		}
		for len(t.vals) < off+(idx+1)*stride {
			t.vals = append(t.vals, 0)
		}
		p.init(t, off+idx*stride, dims[1:])
		if !p.comma("}") {
			return
		}
	}
}

func (p *parser) strings(t *table) {
	p.expect("{")
	for idx := 0; !p.accept("}"); idx++ {
		if p.accept("[") {
			idx = int(p.expr())
			p.expect("]", "=")
		}
		for len(t.strs) <= idx {
			t.strs = append(t.strs, "")
		}
		if !p.accept("NULL") {
			if p.tok.kind != tStr {
				p.fail("expected string")
			}
			// The runtime reads these as C strings, which end at a NUL
			// (Go's symbol "\0" is the C name "").
			t.strs[idx], _, _ = strings.Cut(p.tok.s, "\x00")
			p.next()
		}
		if !p.comma("}") {
			break
		}
	}
	if t.dims[0] != 0 && len(t.strs) != t.dims[0] {
		p.fail("%s: %d strings for dimension %d", t.name, len(t.strs), t.dims[0])
	}
}

func (p *parser) rng(v, lo, hi int64, what string) int64 {
	if v < lo || v > hi {
		p.fail("%s %d out of range", what, v)
	}
	return v
}

// parseActions reads ts_parse_actions into the 5-field slots of
// CONTRACT.md section 4.
func (p *parser) parseActions() {
	const u8, u16 = 0xFF, 0xFFFF
	p.expect("{")
	for idx := 0; !p.accept("}"); idx++ {
		if p.accept("[") {
			idx = int(p.expr())
			p.expect("]", "=")
		}
		var a [5]int64
		if p.accept("{") {
			p.expect(".", "entry", "=", "{", ".", "count", "=")
			a[0] = p.rng(p.expr(), 0, u8, "entry.count")
			p.expect(",", ".", "reusable", "=")
			a[1] = p.rng(p.expr(), 0, 1, "entry.reusable")
			p.expect("}", "}")
		} else {
			name := p.ident()
			var args []int64
			p.expect("(")
			for !p.accept(")") {
				args = append(args, p.expr())
				if !p.comma(")") {
					break
				}
			}
			want := 0
			switch name {
			case "SHIFT", "SHIFT_REPEAT":
				want = 1
			case "REDUCE":
				want = 4
			}
			if len(args) != want {
				p.fail("%s with %d arguments", name, len(args))
			}
			switch name {
			case "SHIFT":
				a = [5]int64{0, 0, p.rng(args[0], 0, u16, "shift state"), 0, 0}
			case "SHIFT_REPEAT":
				a = [5]int64{0, 0, p.rng(args[0], 0, u16, "shift state"), 2, 0}
			case "SHIFT_EXTRA":
				a = [5]int64{0, 0, 0, 1, 0}
			case "REDUCE":
				a = [5]int64{1,
					p.rng(args[1], 0, u8, "child_count"),
					p.rng(args[0], 0, u16, "symbol"),
					p.rng(args[2], -32768, 32767, "dynamic_precedence"),
					p.rng(args[3], 0, u16, "production_id")}
			case "ACCEPT_INPUT":
				a = [5]int64{2, 0, 0, 0, 0}
			case "RECOVER":
				a = [5]int64{3, 0, 0, 0, 0}
			default:
				p.fail("unknown parse action %s", name)
			}
		}
		for len(p.g.actions) < (idx+1)*5 {
			p.g.actions = append(p.g.actions, 0)
		}
		copy(p.g.actions[idx*5:], a[:])
		if !p.comma("}") {
			break
		}
	}
}

func (p *parser) lexFn() {
	f := &lexFunc{cname: p.ident()}
	p.expect("(", "TSLexer", "*", "lexer", ",", "TSStateId", "state", ")", "{",
		"START_LEXER", "(", ")", ";",
		"eof", "=", "lexer", "->", "eof", "(", "lexer", ")", ";",
		"switch", "(", "state", ")", "{")
	seen := map[int64]bool{}
	for p.accept("case") {
		c := lexCase{state: p.rng(p.expr(), 0, 0xFFFF, "lex state")}
		p.expect(":")
		if seen[c.state] {
			p.fail("duplicate case %d", c.state)
		}
		seen[c.state] = true
		for !p.is("case") && !p.is("default") {
			c.stmts = append(c.stmts, p.lexStmt())
		}
		// Each C case must end in END_STATE, or it would fall into the next.
		for i, s := range c.stmts {
			if (s.kind == 'e') != (i == len(c.stmts)-1) {
				p.fail("case %d: END_STATE is not the last statement", c.state)
			}
		}
		f.cases = append(f.cases, c)
	}
	p.expect("default", ":", "return", "false", ";", "}", "}")
	p.g.lexFns = append(p.g.lexFns, f)
}

func (p *parser) lexStmt() lexStmt {
	switch {
	case p.accept("END_STATE"):
		p.expect("(", ")", ";")
		return lexStmt{kind: 'e'}
	case p.accept("ACCEPT_TOKEN"):
		p.expect("(")
		pos := p.tok.pos
		s := lexStmt{kind: 'a', sym: p.ident()}
		if _, ok := p.consts[s.sym]; !ok {
			p.tz.fail(pos, "unknown symbol %s", s.sym)
		}
		p.expect(")", ";")
		return s
	case p.accept("ADVANCE_MAP"):
		s := lexStmt{kind: 'm'}
		p.expect("(")
		seen := map[int64]bool{}
		for !p.accept(")") {
			// C stores keys and states in a uint16_t map. A wider key would
			// be cut there, so refuse it.
			k := p.rng(p.expr(), 0, 0xFFFF, "ADVANCE_MAP key")
			p.expect(",")
			v := p.rng(p.expr(), 0, 0xFFFF, "ADVANCE_MAP state")
			if seen[k] {
				p.fail("duplicate ADVANCE_MAP key %d", k)
			}
			seen[k] = true
			s.pairs = append(s.pairs, k, v)
			if !p.comma(")") {
				break
			}
		}
		p.expect(";")
		return s
	case p.accept("if"):
		s := lexStmt{kind: 'i'}
		p.expect("(")
		s.cond = p.lexCond()
		switch {
		case p.accept("SKIP"):
			s.skip = true
		case p.accept("ADVANCE"):
		default:
			p.fail("expected ADVANCE or SKIP, found %q", p.tok.s)
		}
		p.expect("(")
		s.target = p.rng(p.expr(), 0, 0xFFFF, "lex state")
		p.expect(")", ";")
		return s
	}
	p.fail("unexpected %q in lex function", p.tok.s)
	return lexStmt{}
}

// lexCond copies a C condition up to its closing parenthesis as Go source.
// The operators used have the same meaning and precedence in both languages.
func (p *parser) lexCond() string {
	var b strings.Builder
	for depth := 1; ; {
		t := p.tok
		p.next()
		switch {
		case t.kind == tChar:
			b.WriteString(goRune(t.v))
		case t.kind == tNum:
			b.WriteString(t.s)
		case t.kind == tIdent && (t.s == "lookahead" || t.s == "eof"):
			b.WriteString(t.s)
		case t.kind == tIdent && t.s == "set_contains":
			p.expect("(")
			name := p.ident()
			set := p.g.tables[name]
			if set == nil || set.ctype != "TSCharacterRange" {
				p.tz.fail(t.pos, "unknown character set %s", name)
			}
			p.expect(",")
			if n := p.expr(); int(n) != set.len() {
				p.tz.fail(t.pos, "set_contains(%s, %d): table has %d ranges", name, n, set.len())
			}
			p.expect(",", "lookahead", ")")
			b.WriteString("gositter.SetContainsASCII(&" + name + "_ascii, " + name + "[:], lookahead)")
		case t.kind == tPunct && t.s == "(":
			depth++
			b.WriteString("(")
		case t.kind == tPunct && t.s == ")":
			if depth--; depth == 0 {
				return stripParens(b.String())
			}
			b.WriteString(")")
		case t.kind == tPunct && t.s == "!":
			b.WriteString("!")
		case t.kind == tPunct && slices.Contains([]string{"==", "!=", "<", ">", "<=", ">=", "&&", "||"}, t.s):
			b.WriteString(" " + t.s + " ")
		default:
			p.tz.fail(t.pos, "unexpected %q in lex condition", t.s)
		}
	}
}

// stripParens drops parentheses around a whole condition, as gofmt does.
func stripParens(s string) string {
	for strings.HasPrefix(s, "(") {
		depth := 0
		for i := 0; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
			case '\'': // skip a rune literal, it may hold a parenthesis
				i++
				for ; s[i] != '\''; i++ {
					if s[i] == '\\' {
						i++
					}
				}
			}
			if depth == 0 && i < len(s)-1 {
				return s
			}
		}
		s = s[1 : len(s)-1]
	}
	return s
}

// goRune writes a C character value as a Go literal of the same value.
func goRune(v int64) string {
	if v < 0x80 {
		return strconv.QuoteRune(rune(v))
	}
	return "0x" + strconv.FormatInt(v, 16)
}

func (p *parser) language() {
	p.expect("const", "TSLanguage", "*")
	fn := p.ident()
	name, ok := strings.CutPrefix(fn, "tree_sitter_")
	if !ok || p.g.name != "" {
		p.fail("unexpected function %s", fn)
	}
	p.g.name = name
	p.expect("(", "void", ")", "{", "static", "const", "TSLanguage", "language", "=")
	p.langFields("")
	p.expect(";", "return", "&", "language", ";", "}")
}

func (p *parser) langFields(prefix string) {
	p.expect("{")
	for i := 0; !p.accept("}"); i++ {
		key := prefix + strconv.Itoa(i)
		if p.accept(".") {
			key = prefix + p.ident()
			p.expect("=")
		}
		if p.is("{") {
			p.langFields(key + ".")
		} else {
			var toks []token
			for depth := 0; depth > 0 || !p.is(",") && !p.is("}"); p.next() {
				if p.tok.kind == tEOF {
					p.fail("unterminated TSLanguage initialiser")
				}
				if p.is("(") || p.is("[") {
					depth++
				} else if p.is(")") || p.is("]") {
					depth--
				}
				toks = append(toks, p.tok)
			}
			if _, dup := p.g.lang[key]; dup {
				p.fail("duplicate TSLanguage field %s", key)
			}
			p.g.lang[key] = toks
		}
		if !p.comma("}") {
			break
		}
	}
}
