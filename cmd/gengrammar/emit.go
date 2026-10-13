package main

import (
	"bufio"
	"bytes"
	"fmt"
	"go/format"
	"io"
	"strconv"
	"strings"
)

const importLine = "import \"github.com/dobbo-ca/gositter\"\n\n"

// Go element type and value range for each C scalar type.
var goScalar = map[string]struct {
	typ    string
	lo, hi int64
}{
	"TSSymbol":  {"gositter.Symbol", 0, 0xFFFF},
	"TSStateId": {"gositter.StateID", 0, 0xFFFF},
	"uint16_t":  {"uint16", 0, 0xFFFF},
	"uint32_t":  {"uint32", 0, 0xFFFFFFFF},
	"bool":      {"bool", 0, 1},
}

type field struct {
	name   string // Go field name
	lo, hi int64
	isBool bool
}

func fBool(n string) field { return field{n, 0, 1, true} }
func fU8(n string) field   { return field{n, 0, 0xFF, false} }
func fU16(n string) field  { return field{n, 0, 0xFFFF, false} }
func fI32(n string) field  { return field{n, -1 << 31, 1<<31 - 1, false} }

// Go struct type and fields for each C struct type, in C field order.
var goStruct = map[string]struct {
	typ    string
	fields []field
}{
	"TSSymbolMetadata": {"gositter.SymbolMetadata", []field{fBool("Visible"), fBool("Named"), fBool("Supertype")}},
	"TSFieldMapSlice":  {"gositter.MapSlice", []field{fU16("Index"), fU16("Length")}},
	"TSMapSlice":       {"gositter.MapSlice", []field{fU16("Index"), fU16("Length")}},
	"TSFieldMapEntry":  {"gositter.FieldMapEntry", []field{fU16("FieldID"), fU8("ChildIndex"), fBool("Inherited")}},
	"TSLexMode":        {"gositter.LexMode", []field{fU16("LexState"), fU16("ExternalLexState")}},
	"TSLexerMode":      {"gositter.LexMode", []field{fU16("LexState"), fU16("ExternalLexState"), fU16("ReservedWordSetID")}},
	"TSCharacterRange": {"gositter.CharacterRange", []field{fI32("Start"), fI32("End")}},
	"TSParseAction":    {"gositter.ParseAction", []field{fU8("Type"), fU8("ChildCount"), fU16("Symbol"), {"DynamicPrecedence", -1 << 15, 1<<15 - 1, false}, fU16("ProductionID")}},
}

type emitter struct {
	g   *grammar
	pkg string
	hdr string // first lines of every file

	wantsScanner bool // the grammar has an external scanner
}

func (e *emitter) fail(format string, args ...any) {
	panic(genError{e.pkg + ": " + fmt.Sprintf(format, args...)})
}

// gofmt formats a small piece of source. The big tables are written by hand
// in gofmt's layout instead, because go/format needs gigabytes for them.
func (e *emitter) gofmt(src string) string {
	out, err := format.Source([]byte(src))
	if err != nil {
		e.fail("gofmt: %v\n%s", err, src)
	}
	return string(out)
}

func (e *emitter) tables(out io.Writer) {
	w := bufio.NewWriterSize(out, 1<<20)
	defer w.Flush()
	g := e.g

	var c strings.Builder
	c.WriteString(e.hdr + "package " + e.pkg + "\n\n" + importLine)
	c.WriteString("const ts_builtin_sym_end = 0\n")
	for _, en := range g.enums {
		fmt.Fprintf(&c, "\n// enum %s\nconst (\n", en.name)
		for i, n := range en.names {
			fmt.Fprintf(&c, "%s = %d\n", n, en.vals[i])
		}
		c.WriteString(")\n")
	}
	w.WriteString(e.gofmt(c.String()))

	for _, name := range g.order {
		t := g.tables[name]
		switch {
		case t.ctype == "TSCharacterRange": // lex.go
		case t.ctype == "char*":
			fmt.Fprintf(w, "\nvar %s = []string{\n", name)
			for _, s := range t.strs {
				w.WriteString("\t" + strconv.Quote(s) + ",\n")
			}
			w.WriteString("}\n")
		case t.ctype == "TSParseActionEntry":
			// one header slot and its actions per line
			a := g.actions
			e.structTable(w, name, "TSParseAction", a, 0, func(i int) int { return int(a[i*5]) + 1 }, false)
		case t.fields != nil:
			e.structTable(w, name, t.ctype, t.vals, 8, nil, false)
		default:
			e.scalarTable(w, t)
		}
	}
}

// scalarTable writes a numeric or bool array as keyed runs of non-zero
// values: "start: v, v, v,". The zeros in between cost nothing to compile.
func (e *emitter) scalarTable(w *bufio.Writer, t *table) {
	sc := goScalar[t.ctype]
	for _, v := range t.vals {
		if v < sc.lo || v > sc.hi {
			e.fail("%s: value %d does not fit %s", t.name, v, t.ctype)
		}
	}
	row := t.width()
	if len(t.dims) == 1 {
		row = len(t.vals)
	}
	// lines calls yield for each run [i, j) that starts with a non-zero value.
	end := 0 // of the last run
	lines := func(yield func(i, j int)) {
		const perLine, maxGap = 16, 3
		v := t.vals
		for i := 0; i < len(v); {
			if v[i] == 0 {
				i++
				continue
			}
			rowEnd := min((i/row+1)*row, i+perLine)
			j, zeros := i+1, 0
			for k := i + 1; k < rowEnd && zeros <= maxGap; k++ {
				if v[k] == 0 {
					zeros++
				} else {
					j, zeros = k+1, 0
				}
			}
			// gofmt aligns the keys of one-element lines; avoid such lines.
			if j == i+1 && j < len(v) {
				j++
			}
			yield(i, j)
			i, end = j, j
		}
	}
	fmt.Fprintf(w, "\nvar %s = []%s{", t.name, sc.typ)
	if len(t.vals) > 0 {
		w.WriteString("\n")
	}
	var buf []byte
	lines(func(i, j int) {
		buf = append(buf[:0], '\t')
		buf = strconv.AppendInt(buf, int64(i), 10)
		buf = append(buf, ':')
		for _, x := range t.vals[i:j] {
			buf = append(buf, ' ')
			if t.ctype == "bool" {
				buf = strconv.AppendBool(buf, x != 0)
			} else {
				buf = strconv.AppendInt(buf, x, 10)
			}
			buf = append(buf, ',')
		}
		buf = append(buf, '\n')
		w.Write(buf)
	})
	// A slice literal is as long as its last element: name it if it is zero.
	if n := len(t.vals); n > 0 && end < n {
		zero := "0"
		if t.ctype == "bool" {
			zero = "false"
		}
		fmt.Fprintf(w, "\t%d: %s,\n", n-1, zero)
	}
	w.WriteString("}\n")
}

// structLit appends a keyed struct literal without its zero fields. Keyed,
// because go vet rejects positional literals of an imported struct type.
func (e *emitter) structLit(buf []byte, table, ctype string, vals []int64, hex bool) []byte {
	buf = append(buf, '{')
	first := true
	for i, f := range goStruct[ctype].fields {
		v := vals[i]
		if v < f.lo || v > f.hi {
			e.fail("%s: value %d does not fit %s", table, v, f.name)
		}
		if v == 0 {
			continue
		}
		if !first {
			buf = append(buf, ", "...)
		}
		first = false
		buf = append(append(buf, f.name...), ": "...)
		switch {
		case f.isBool:
			buf = append(buf, "true"...)
		case hex:
			buf = fmt.Appendf(buf, "%#x", v)
		default:
			buf = strconv.AppendInt(buf, v, 10)
		}
	}
	return append(buf, '}')
}

// structTable writes per elements per line; group, if set, says how many
// elements the line starting at element i takes instead. Tables are slices,
// not arrays, because "x[:]" in the language literal would be an assignment
// at init time, while a slice variable is copied by the linker.
func (e *emitter) structTable(w *bufio.Writer, name, ctype string, vals []int64, per int, group func(i int) int, array bool) {
	nf := len(goStruct[ctype].fields)
	n := len(vals) / nf
	size := ""
	if array {
		size = strconv.Itoa(n)
	}
	fmt.Fprintf(w, "\nvar %s = [%s]%s{", name, size, goStruct[ctype].typ)
	if n > 0 {
		w.WriteString("\n")
	}
	var buf []byte
	for i := 0; i < n; {
		if group != nil {
			per = group(i)
		}
		if per < 1 || i+per > n && group != nil {
			e.fail("%s: entry %d runs past the end", name, i)
		}
		buf = buf[:0]
		for k := i; k < min(i+per, n); k++ {
			buf = append(buf, ' ')
			if k == i {
				buf[len(buf)-1] = '\t'
			}
			buf = e.structLit(buf, name, ctype, vals[k*nf:(k+1)*nf], ctype == "TSCharacterRange")
			buf = append(buf, ',')
		}
		w.Write(append(buf, '\n'))
		i += per
	}
	w.WriteString("}\n")
}

func (e *emitter) lex(out io.Writer) {
	w := bufio.NewWriterSize(out, 1<<20)
	defer w.Flush()
	g := e.g
	w.WriteString(e.hdr + "package " + e.pkg + "\n\n" + importLine[:len(importLine)-1])
	for _, name := range g.order {
		t := g.tables[name]
		if t.ctype != "TSCharacterRange" {
			continue
		}
		e.structTable(w, name, t.ctype, t.vals, 8, nil, true)
		// set_contains for every code point below 128.
		fmt.Fprintf(w, "\nvar %s_ascii = [128]bool{", name)
		n := 0
		for c := int64(0); c < 128; c++ {
			if !setContains(t.vals, c) {
				continue
			}
			if n%16 == 0 {
				w.WriteString("\n\t")
			} else {
				w.WriteString(" ")
			}
			fmt.Fprintf(w, "%d: true,", c)
			n++
		}
		if n > 0 {
			w.WriteString("\n")
		}
		w.WriteString("}\n")
	}
	// One table per distinct run set, in order of first use.
	runTable := map[[128]bool]int{}
	runOf := map[*lexCase][2]int{} // table index + 1 for ADVANCE and for SKIP
	for _, f := range g.lexFns {
		for i := range f.cases {
			c := &f.cases[i]
			adv, skip := e.runSets(*c)
			var ids [2]int
			for k, set := range [2][128]bool{adv, skip} {
				if set == ([128]bool{}) {
					continue
				}
				if _, ok := runTable[set]; !ok {
					runTable[set] = len(runTable)
					fmt.Fprintf(w, "\nvar lexRun%d = [256]bool{", runTable[set])
					n := 0
					for ch, in := range set {
						if !in {
							continue
						}
						if n%16 == 0 {
							w.WriteString("\n\t")
						} else {
							w.WriteString(" ")
						}
						fmt.Fprintf(w, "%d: true,", ch)
						n++
					}
					w.WriteString("\n}\n")
				}
				ids[k] = runTable[set] + 1
			}
			runOf[c] = ids
		}
	}
	for _, f := range g.lexFns {
		name := map[string]string{"ts_lex": "lex", "ts_lex_keywords": "lexKeywords"}[f.cname]
		if name == "" {
			e.fail("unexpected lex function %s", f.cname)
		}
		fmt.Fprintf(w, `
// %s
func %s(l *gositter.Lexer, state gositter.StateID) bool {
	result := false
	skip := false
	eof := false
	var lookahead int32
	goto start
next_state:
	l.Advance(skip)
start:
	skip = false
	lookahead = l.Lookahead
	eof = l.EOF()
	_, _ = eof, lookahead
	switch state {
`, f.cname, name)
		for i := range f.cases {
			c := &f.cases[i]
			fmt.Fprintf(w, "\tcase %d:\n", c.state)
			// A state that both advances and skips on itself gets the
			// ADVANCE run only. The skips stay one step at a time.
			ids := runOf[c]
			if id, skip := ids[0], false; id != 0 || ids[1] != 0 {
				if id == 0 {
					id, skip = ids[1], true
				}
				fmt.Fprintf(w, "\t\tif uint32(lookahead) < 128 && lexRun%d[lookahead] {\n\t\t\tl.AdvanceRun(&lexRun%d, %t)\n\t\t\tlookahead = l.Lookahead\n\t\t\teof = l.EOF()\n\t\t}\n", id-1, id-1, skip)
			}
			for _, s := range c.stmts {
				switch s.kind {
				case 'e':
					w.WriteString("\t\treturn result\n")
				case 'a':
					fmt.Fprintf(w, "\t\tresult = true\n\t\tl.ResultSymbol = %s\n\t\tl.MarkEnd()\n", s.sym)
				case 'i':
					fmt.Fprintf(w, "\t\tif %s {\n", s.cond)
					if s.skip {
						w.WriteString("\t\t\tskip = true\n")
					}
					fmt.Fprintf(w, "\t\t\tstate = %d\n\t\t\tgoto next_state\n\t\t}\n", s.target)
				case 'm':
					w.WriteString("\t\tswitch lookahead {\n")
					for i := 0; i < len(s.pairs); i += 2 {
						fmt.Fprintf(w, "\t\tcase %s:\n\t\t\tstate = %d\n\t\t\tgoto next_state\n", goRune(s.pairs[i]), s.pairs[i+1])
					}
					w.WriteString("\t\t}\n")
				}
			}
		}
		w.WriteString("\tdefault:\n\t\treturn false\n\t}\n}\n")
	}
}

// eval reads a TSLanguage field that holds one constant.
func (e *emitter) eval(key string) (int64, bool) {
	toks, ok := e.g.lang[key]
	if !ok {
		return 0, false
	}
	delete(e.g.lang, key)
	if len(toks) == 1 {
		switch t := toks[0]; t.kind {
		case tNum:
			return t.v, true
		case tIdent:
			if v, ok := e.g.consts[t.s]; ok {
				return v, true
			}
			if v, ok := e.g.defines[t.s]; ok {
				return v, true
			}
		}
	}
	e.fail("TSLanguage.%s: cannot evaluate", key)
	return 0, false
}

// ref reads a TSLanguage field that points at a table or function and
// returns the name it points at.
func (e *emitter) ref(key string) string {
	toks, ok := e.g.lang[key]
	if !ok {
		return ""
	}
	delete(e.g.lang, key)
	name := ""
	for _, t := range toks {
		if t.kind == tIdent && (strings.HasPrefix(t.s, "ts_") || strings.HasPrefix(t.s, "tree_sitter_")) {
			if name != "" {
				e.fail("TSLanguage.%s: two names", key)
			}
			name = t.s
		}
	}
	if name == "" {
		e.fail("TSLanguage.%s: no table name", key)
	}
	return name
}

func (e *emitter) language(out io.Writer, haveScanner bool) {
	g := e.g
	var b bytes.Buffer
	b.WriteString(e.hdr + "package " + e.pkg + "\n\n" + importLine + "var language = gositter.Language{\n")

	num := map[string]int64{}
	for _, f := range [][2]string{
		{"version", "ABIVersion"}, {"abi_version", "ABIVersion"},
		{"symbol_count", "SymbolCount"}, {"alias_count", "AliasCount"},
		{"token_count", "TokenCount"}, {"external_token_count", "ExternalTokenCount"},
		{"state_count", "StateCount"}, {"large_state_count", "LargeStateCount"},
		{"production_id_count", "ProductionIDCount"}, {"field_count", "FieldCount"},
		{"max_alias_sequence_length", "MaxAliasSequenceLength"},
		{"keyword_capture_token", "KeywordCaptureToken"},
		{"max_reserved_word_set_size", "MaxReservedWordSetSize"},
		{"supertype_count", "SupertypeCount"},
	} {
		if v, ok := e.eval(f[0]); ok {
			if _, dup := num[f[1]]; dup {
				e.fail("TSLanguage.%s set twice", f[0])
			}
			num[f[1]] = v
			fmt.Fprintf(&b, "%s: %d,\n", f[1], v)
		}
	}
	if num["ABIVersion"] != g.defines["LANGUAGE_VERSION"] || num["ABIVersion"] == 0 {
		e.fail("ABI version %d does not match LANGUAGE_VERSION", num["ABIVersion"])
	}

	// want is the element count the runtime indexes each table by.
	states, syms := int(num["StateCount"]), int(num["SymbolCount"]+num["AliasCount"])
	want := map[string]int{
		"ParseTable":         int(num["LargeStateCount"] * num["SymbolCount"]),
		"SmallParseTableMap": states - int(num["LargeStateCount"]),
		"SymbolNames":        syms,
		"SymbolMetadata":     syms,
		"PublicSymbolMap":    syms,
		"FieldNames":         int(num["FieldCount"]) + 1,
		"FieldMapSlices":     int(num["ProductionIDCount"]),
		"AliasSequences":     int(num["ProductionIDCount"] * num["MaxAliasSequenceLength"]),
		"LexModes":           states,
		"PrimaryStateIDs":    states,
		"SupertypeSymbols":   int(num["SupertypeCount"]),
	}
	used := map[string]bool{}
	table := func(key, field string) *table {
		name := e.ref(key)
		if name == "" {
			return nil
		}
		t := g.tables[name]
		if t == nil {
			e.fail("TSLanguage.%s: no table %s", key, name)
		}
		n := len(t.vals) / max(1, len(t.fields))
		switch {
		case t.ctype == "TSParseActionEntry":
			n = len(g.actions) / 5
		case t.ctype == "char*":
			n = len(t.strs)
		}
		if w, ok := want[field]; ok && w != n {
			e.fail("%s has %d entries, the counts say %d", name, n, w)
		}
		used[name] = true
		fmt.Fprintf(&b, "%s: %s,\n", field, name)
		return t
	}
	for _, f := range [][2]string{
		{"parse_table", "ParseTable"}, {"small_parse_table", "SmallParseTable"},
		{"small_parse_table_map", "SmallParseTableMap"}, {"parse_actions", "ParseActions"},
		{"symbol_names", "SymbolNames"}, {"field_names", "FieldNames"},
		{"field_map_slices", "FieldMapSlices"}, {"field_map_entries", "FieldMapEntries"},
		{"symbol_metadata", "SymbolMetadata"}, {"public_symbol_map", "PublicSymbolMap"},
		{"alias_map", "AliasMap"}, {"alias_sequences", "AliasSequences"},
		{"lex_modes", "LexModes"}, {"primary_state_ids", "PrimaryStateIDs"},
		{"supertype_symbols", "SupertypeSymbols"}, {"supertype_map_slices", "SupertypeMapSlices"},
		{"supertype_map_entries", "SupertypeMapEntries"},
	} {
		table(f[0], f[1])
	}
	if t := table("reserved_words", "ReservedWords"); t != nil && t.width() != int(num["MaxReservedWordSetSize"]) {
		e.fail("ts_reserved_words rows are %d wide, max_reserved_word_set_size is %d", t.width(), num["MaxReservedWordSetSize"])
	}

	if fn := e.ref("lex_fn"); fn != "ts_lex" {
		e.fail("lex_fn is %q", fn)
	}
	b.WriteString("LexFn: lex,\n")
	if fn := e.ref("keyword_lex_fn"); fn == "ts_lex_keywords" {
		b.WriteString("KeywordLexFn: lexKeywords,\n")
	} else if fn != "" {
		e.fail("keyword_lex_fn is %q", fn)
	}
	fns := 1
	if _, ok := num["KeywordCaptureToken"]; ok {
		fns = 2
	}
	if len(g.lexFns) != fns {
		e.fail("%d lex functions, TSLanguage uses %d", len(g.lexFns), fns)
	}

	// Name is the <name> of tree_sitter_<name>, also where C has NULL.
	if toks, ok := g.lang["name"]; ok {
		delete(g.lang, "name")
		if len(toks) != 1 || toks[0].kind != tStr || toks[0].s != g.name {
			e.fail("TSLanguage.name does not match tree_sitter_%s", g.name)
		}
	}
	fmt.Fprintf(&b, "Name: %q,\n", g.name)

	if _, ok := g.lang["metadata.major_version"]; ok {
		b.WriteString("Metadata: gositter.LanguageMetadata{")
		for _, k := range [][2]string{{"major_version", "MajorVersion"}, {"minor_version", "MinorVersion"}, {"patch_version", "PatchVersion"}} {
			v, _ := e.eval("metadata." + k[0])
			if v < 0 || v > 0xFF {
				e.fail("metadata.%s out of range", k[0])
			}
			fmt.Fprintf(&b, "%s: %d, ", k[1], v)
		}
		b.WriteString("},\n")
	}

	if _, ok := g.lang["external_scanner.0"]; ok {
		e.wantsScanner = true
		b.WriteString("ExternalScanner: gositter.ExternalScannerInfo{\n")
		ext := int(num["ExternalTokenCount"])
		if t := table("external_scanner.0", "States"); t.width() != ext {
			e.fail("%s rows are %d wide, external_token_count is %d", t.name, t.width(), ext)
		}
		t := table("external_scanner.1", "SymbolMap")
		if len(t.vals) != ext {
			e.fail("%s has %d entries, external_token_count is %d", t.name, len(t.vals), ext)
		}
		for i, fn := range []string{"create", "destroy", "scan", "serialize", "deserialize"} {
			if got, want := e.ref("external_scanner."+strconv.Itoa(i+2)), "tree_sitter_"+g.name+"_external_scanner_"+fn; got != want {
				e.fail("external_scanner.%s is %q", fn, got)
			}
		}
		if haveScanner {
			b.WriteString("New: newScanner,\n")
		} else {
			b.WriteString("// New stays nil: there was no scanner.go when this file was\n" +
				"// generated. Add scanner.go (func newScanner), then rerun gen.sh.\n")
		}
		b.WriteString("},\n")
	} else if num["ExternalTokenCount"] != 0 {
		e.fail("external tokens without external_scanner")
	}
	b.WriteString("}\n\n")

	for k := range g.lang {
		e.fail("TSLanguage.%s is not handled", k)
	}
	for _, name := range g.order {
		if !used[name] && g.tables[name].ctype != "TSCharacterRange" {
			e.fail("table %s is not referenced by TSLanguage", name)
		}
	}
	fmt.Fprintf(&b, "// tree_sitter_%s\nfunc Language() *gositter.Language { return &language }\n", g.name)
	io.WriteString(out, e.gofmt(b.String()))
}

// setContains is the C set_contains over the flat {start, end} pairs, so the
// ASCII bits are what the binary search answers even for an odd table.
func setContains(vals []int64, lookahead int64) bool {
	index, size := 0, len(vals)/2
	if size == 0 {
		return false
	}
	for size > 1 {
		halfSize := size / 2
		midIndex := index + halfSize
		start, end := vals[midIndex*2], vals[midIndex*2+1]
		if lookahead >= start && lookahead <= end {
			return true
		} else if lookahead > end {
			index = midIndex
		}
		size -= halfSize
	}
	return lookahead >= vals[index*2] && lookahead <= vals[index*2+1]
}
