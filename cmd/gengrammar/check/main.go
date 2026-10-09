// Command check proves the output of gengrammar against the C compiler's
// view of the same parser.c: it loads each real TSLanguage through cgo and
// compares every table with the generated Go tables, then runs the C lex
// functions and the generated Go ones on the same inputs with twin mock
// lexers. Run it with ./run.sh.
package main

// #include "parser.h"
import "C"

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/dobbo-ca/gositter"
)

type checker struct {
	name   string
	c      *cLang
	g      *gositter.Language
	errs   []string
	values int // table values compared

	cTime, goTime time.Duration // text test, lex function plus mock lexer
}

func (c *checker) errf(format string, args ...any) {
	if len(c.errs) < 20 {
		c.errs = append(c.errs, fmt.Sprintf(format, args...))
	}
}

type integer interface {
	~uint8 | ~uint16 | ~uint32 | ~bool
}

// num reads any of the element types as its raw bits; both sides of a
// comparison have the same size.
func num[T integer](v T) int64 {
	switch unsafe.Sizeof(v) {
	case 1:
		return int64(*(*uint8)(unsafe.Pointer(&v)))
	case 2:
		return int64(*(*uint16)(unsafe.Pointer(&v)))
	case 4:
		return int64(*(*uint32)(unsafe.Pointer(&v)))
	}
	return *(*int64)(unsafe.Pointer(&v))
}

// same compares a C array with a Go table element by element.
func same[T, U integer](c *checker, what string, cv []T, gv []U) {
	if len(cv) != len(gv) {
		c.errf("%s: C has %d entries, Go has %d", what, len(cv), len(gv))
		return
	}
	for i := range cv {
		if num(cv[i]) != num(gv[i]) {
			c.errf("%s[%d]: C %d, Go %d", what, i, num(cv[i]), num(gv[i]))
			return
		}
	}
	c.values += len(cv)
}

func (c *checker) eq(what string, cv, gv int64) {
	if cv != gv {
		c.errf("%s: C %d, Go %d", what, cv, gv)
	}
	c.values++
}

// ts_language_lookup, on any pair of tables
func lookup(parseTable, small []uint16, smallMap []uint32, large, symbolCount uint32, state, symbol uint32) uint16 {
	if state >= large {
		data := small[smallMap[state-large]:]
		groupCount := data[0]
		data = data[1:]
		for i := uint16(0); i < groupCount; i++ {
			sectionValue, n := data[0], data[1]
			data = data[2:]
			for j := uint16(0); j < n; j++ {
				if uint32(data[j]) == symbol {
					return sectionValue
				}
			}
			data = data[n:]
		}
		return 0
	}
	return parseTable[state*symbolCount+symbol]
}

func (c *checker) tables(cName string) {
	l, g := c.c, c.g
	abi := uint32(l.abi_version)
	c.eq("abi_version", int64(abi), int64(g.ABIVersion))
	c.eq("symbol_count", int64(l.symbol_count), int64(g.SymbolCount))
	c.eq("alias_count", int64(l.alias_count), int64(g.AliasCount))
	c.eq("token_count", int64(l.token_count), int64(g.TokenCount))
	c.eq("external_token_count", int64(l.external_token_count), int64(g.ExternalTokenCount))
	c.eq("state_count", int64(l.state_count), int64(g.StateCount))
	c.eq("large_state_count", int64(l.large_state_count), int64(g.LargeStateCount))
	c.eq("production_id_count", int64(l.production_id_count), int64(g.ProductionIDCount))
	c.eq("field_count", int64(l.field_count), int64(g.FieldCount))
	c.eq("max_alias_sequence_length", int64(l.max_alias_sequence_length), int64(g.MaxAliasSequenceLength))
	c.eq("keyword_capture_token", int64(l.keyword_capture_token), int64(g.KeywordCaptureToken))
	if len(c.errs) > 0 {
		return // the sizes below come from these counts
	}
	states, large, syms := int(l.state_count), int(l.large_state_count), int(l.symbol_count)
	allSyms := syms + int(l.alias_count)

	// parse table, small parse table and its map
	parseTable := cSlice[uint16](l.parse_table, large*syms)
	same(c, "parse_table", parseTable, g.ParseTable)
	smallMap := cSlice[uint32](l.small_parse_table_map, states-large)
	same(c, "small_parse_table_map", smallMap, g.SmallParseTableMap)
	smallLen := 0
	if states > large {
		// C gives no length; walk every small state to find the end
		p := (*uint16)(unsafe.Pointer(l.small_parse_table))
		for _, off := range smallMap {
			i := int(off)
			groups := int(*(*uint16)(unsafe.Add(unsafe.Pointer(p), 2*i)))
			i++
			for ; groups > 0; groups-- {
				i += 2 + int(*(*uint16)(unsafe.Add(unsafe.Pointer(p), 2*(i+1))))
			}
			smallLen = max(smallLen, i)
		}
	}
	small := cSlice[uint16](l.small_parse_table, smallLen)
	same(c, "small_parse_table", small, g.SmallParseTable)
	if len(c.errs) > 0 {
		return
	}

	// parse actions: every slot of the Go table against the C unions
	actions := cSlice[[8]byte](l.parse_actions, len(g.ParseActions))
	headers := map[int]bool{}
	for i := 0; i < len(actions); {
		count := int(actions[i][0])
		h := g.ParseActions[i]
		if int(h.Count()) != count || h.Reusable() != (actions[i][1] != 0) || h.Symbol != 0 || h.DynamicPrecedence != 0 || h.ProductionID != 0 {
			c.errf("parse_actions[%d]: header differs: C %v, Go %+v", i, actions[i], h)
			return
		}
		headers[i] = true
		if i+count >= len(actions) {
			c.errf("parse_actions[%d]: runs past the Go table", i)
			return
		}
		for k := i + 1; k <= i+count; k++ {
			b, a := actions[k], g.ParseActions[k]
			u16 := func(o int) uint16 { return uint16(b[o]) | uint16(b[o+1])<<8 }
			var want gositter.ParseAction
			switch b[0] {
			case 0: // shift: type, state at 2, extra at 4, repetition at 5
				want = gositter.ParseAction{Symbol: gositter.Symbol(u16(2)), DynamicPrecedence: int16(b[4]) | int16(b[5])<<1}
				if a.State() != gositter.StateID(u16(2)) || a.Extra() != (b[4] != 0) || a.Repetition() != (b[5] != 0) {
					c.errf("parse_actions[%d]: shift accessors differ", k)
				}
			case 1: // reduce: type, child_count, symbol, dynamic_precedence, production_id
				want = gositter.ParseAction{Type: 1, ChildCount: b[1], Symbol: gositter.Symbol(u16(2)), DynamicPrecedence: int16(u16(4)), ProductionID: u16(6)}
			case 2, 3:
				want = gositter.ParseAction{Type: b[0]}
			default:
				c.errf("parse_actions[%d]: C type %d", k, b[0])
				return
			}
			if a != want {
				c.errf("parse_actions[%d]: C %v, Go %+v", k, b, a)
				return
			}
		}
		c.values += count + 1
		i += count + 1
	}

	// every (state, symbol) lookup, and that terminals land on a header
	for state := 0; state < states; state++ {
		for sym := 0; sym < syms; sym++ {
			cv := lookup(parseTable, small, smallMap, uint32(large), uint32(syms), uint32(state), uint32(sym))
			gv := lookup(g.ParseTable, g.SmallParseTable, g.SmallParseTableMap, g.LargeStateCount, g.SymbolCount, uint32(state), uint32(sym))
			if cv != gv {
				c.errf("lookup(%d, %d): C %d, Go %d", state, sym, cv, gv)
				return
			}
			if sym < int(l.token_count) && !headers[int(cv)] {
				c.errf("lookup(%d, %d) = %d is not a parse action header", state, sym, cv)
				return
			}
		}
	}
	c.values += states * syms

	// names
	names := cSlice[*C.char](l.symbol_names, allSyms)
	if len(g.SymbolNames) != allSyms {
		c.errf("symbol_names: Go has %d, want %d", len(g.SymbolNames), allSyms)
	} else {
		for i, p := range names {
			if s := C.GoString(p); s != g.SymbolNames[i] {
				c.errf("symbol_names[%d]: C %q, Go %q", i, s, g.SymbolNames[i])
			}
		}
		c.values += allSyms
	}
	fields := int(l.field_count)
	if l.field_names == nil {
		if g.FieldNames != nil {
			c.errf("field_names: C NULL, Go has %d", len(g.FieldNames))
		}
	} else if len(g.FieldNames) != fields+1 {
		c.errf("field_names: Go has %d, want %d", len(g.FieldNames), fields+1)
	} else {
		for i, p := range cSlice[*C.char](l.field_names, fields+1) {
			s := ""
			if p != nil {
				s = C.GoString(p)
			}
			if (p == nil) != (i == 0) || s != g.FieldNames[i] {
				c.errf("field_names[%d]: C %q, Go %q", i, s, g.FieldNames[i])
			}
		}
		c.values += fields + 1
	}

	// field map
	prods := int(l.production_id_count)
	if l.field_map_slices == nil {
		if g.FieldMapSlices != nil || g.FieldMapEntries != nil {
			c.errf("field map: C NULL, Go set")
		}
	} else {
		slices := cSlice[C.TSMapSlice](l.field_map_slices, prods)
		entries := 0
		if len(g.FieldMapSlices) != prods {
			c.errf("field_map_slices: Go has %d, want %d", len(g.FieldMapSlices), prods)
		} else {
			for i, s := range slices {
				if gs := g.FieldMapSlices[i]; uint16(s.index) != gs.Index || uint16(s.length) != gs.Length {
					c.errf("field_map_slices[%d]: C %v, Go %v", i, s, gs)
				}
				entries = max(entries, int(s.index)+int(s.length))
			}
			c.values += prods
		}
		if len(g.FieldMapEntries) != entries {
			c.errf("field_map_entries: Go has %d, the slices reach %d", len(g.FieldMapEntries), entries)
		} else {
			for i, e := range cSlice[C.TSFieldMapEntry](l.field_map_entries, entries) {
				if ge := g.FieldMapEntries[i]; uint16(e.field_id) != uint16(ge.FieldID) || uint8(e.child_index) != ge.ChildIndex || bool(e.inherited) != ge.Inherited {
					c.errf("field_map_entries[%d]: C %v, Go %v", i, e, ge)
				}
			}
			c.values += entries
		}
	}

	// symbol metadata, public symbols, aliases
	if len(g.SymbolMetadata) != allSyms {
		c.errf("symbol_metadata: Go has %d, want %d", len(g.SymbolMetadata), allSyms)
	} else {
		for i, m := range cSlice[C.TSSymbolMetadata](l.symbol_metadata, allSyms) {
			if gm := g.SymbolMetadata[i]; bool(m.visible) != gm.Visible || bool(m.named) != gm.Named || bool(m.supertype) != gm.Supertype {
				c.errf("symbol_metadata[%d]: C %v, Go %v", i, m, gm)
			}
		}
		c.values += allSyms
	}
	same(c, "public_symbol_map", cSlice[uint16](l.public_symbol_map, allSyms), g.PublicSymbolMap)
	aliasLen := 0
	for p := unsafe.Pointer(l.alias_map); ; { // ts_language_aliases_for_symbol's walk
		at := func(i int) int { return int(*(*uint16)(unsafe.Add(p, 2*i))) }
		if at(aliasLen) == 0 {
			aliasLen++
			break
		}
		aliasLen += 2 + at(aliasLen+1)
	}
	same(c, "alias_map", cSlice[uint16](l.alias_map, aliasLen), g.AliasMap)
	same(c, "alias_sequences", cSlice[uint16](l.alias_sequences, prods*int(l.max_alias_sequence_length)), g.AliasSequences)

	// lex modes
	maxExt, maxReserved := 0, 0
	if len(g.LexModes) != states {
		c.errf("lex_modes: Go has %d, want %d", len(g.LexModes), states)
	} else {
		for i := range states {
			var want gositter.LexMode
			if abi < 15 {
				m := cSlice[C.TSLexMode](l.lex_modes, states)[i]
				want = gositter.LexMode{LexState: uint16(m.lex_state), ExternalLexState: uint16(m.external_lex_state)}
			} else {
				m := cSlice[C.TSLexerMode](l.lex_modes, states)[i]
				want = gositter.LexMode{LexState: uint16(m.lex_state), ExternalLexState: uint16(m.external_lex_state), ReservedWordSetID: uint16(m.reserved_word_set_id)}
			}
			if g.LexModes[i] != want {
				c.errf("lex_modes[%d]: C %v, Go %v", i, want, g.LexModes[i])
			}
			maxExt = max(maxExt, int(want.ExternalLexState))
			maxReserved = max(maxReserved, int(want.ReservedWordSetID))
		}
		c.values += states
	}
	if !cHasLexFn(l) || g.LexFn == nil || cHasKeywordFn(l) != (g.KeywordLexFn != nil) {
		c.errf("lex_fn or keyword_lex_fn: presence differs")
	}

	// external scanner
	ext := int(l.external_token_count)
	if cHasCreate(l) != (ext > 0) || (ext > 0) != (g.ExternalScanner.States != nil) {
		c.errf("external scanner: presence differs")
	}
	if ext > 0 {
		// C gives no row count. Go has the declared one; the lex modes must
		// stay inside it.
		rows := len(g.ExternalScanner.States) / ext
		if len(g.ExternalScanner.States)%ext != 0 || rows <= maxExt {
			c.errf("external_scanner.states: %d Go values, largest external lex state %d", len(g.ExternalScanner.States), maxExt)
		} else {
			same(c, "external_scanner.states", cSlice[C.bool](l.external_scanner.states, rows*ext), g.ExternalScanner.States)
		}
		same(c, "external_scanner.symbol_map", cSlice[uint16](l.external_scanner.symbol_map, ext), g.ExternalScanner.SymbolMap)
	}
	same(c, "primary_state_ids", cSlice[uint16](l.primary_state_ids, states), g.PrimaryStateIDs)

	if g.Name != cName {
		c.errf("name: Go %q, want %q", g.Name, cName)
	}
	if abi < 15 {
		if g.ReservedWords != nil || g.MaxReservedWordSetSize != 0 || g.SupertypeCount != 0 || g.SupertypeSymbols != nil ||
			g.SupertypeMapSlices != nil || g.SupertypeMapEntries != nil || g.Metadata != (gositter.LanguageMetadata{}) {
			c.errf("ABI %d grammar with ABI 15 fields set in Go", abi)
		}
		return
	}
	if s := C.GoString(l.name); s != g.Name {
		c.errf("name: C %q, Go %q", s, g.Name)
	}
	c.eq("max_reserved_word_set_size", int64(l.max_reserved_word_set_size), int64(g.MaxReservedWordSetSize))
	if size := int(l.max_reserved_word_set_size); l.reserved_words == nil {
		if g.ReservedWords != nil || maxReserved != 0 {
			c.errf("reserved_words: C NULL, Go has %d", len(g.ReservedWords))
		}
	} else if size == 0 || len(g.ReservedWords)%size != 0 || len(g.ReservedWords)/size <= maxReserved {
		c.errf("reserved_words: %d Go values, set size %d, largest set id %d", len(g.ReservedWords), size, maxReserved)
	} else {
		same(c, "reserved_words", cSlice[uint16](l.reserved_words, len(g.ReservedWords)), g.ReservedWords)
	}
	c.eq("supertype_count", int64(l.supertype_count), int64(g.SupertypeCount))
	super := cSlice[uint16](l.supertype_symbols, int(l.supertype_count))
	same(c, "supertype_symbols", super, g.SupertypeSymbols)
	if len(super) == 0 {
		if g.SupertypeMapSlices != nil || g.SupertypeMapEntries != nil {
			c.errf("supertype maps: no supertypes, Go set")
		}
	} else {
		n, entries := int(slices.Max(super))+1, 0
		if len(g.SupertypeMapSlices) != n {
			c.errf("supertype_map_slices: Go has %d, want %d", len(g.SupertypeMapSlices), n)
		} else {
			for i, s := range cSlice[C.TSMapSlice](l.supertype_map_slices, n) {
				if gs := g.SupertypeMapSlices[i]; uint16(s.index) != gs.Index || uint16(s.length) != gs.Length {
					c.errf("supertype_map_slices[%d]: C %v, Go %v", i, s, gs)
				}
				entries = max(entries, int(s.index)+int(s.length))
			}
			c.values += n
			same(c, "supertype_map_entries", cSlice[uint16](l.supertype_map_entries, entries), g.SupertypeMapEntries)
		}
	}
	m := l.metadata
	if (gositter.LanguageMetadata{MajorVersion: uint8(m.major_version), MinorVersion: uint8(m.minor_version), PatchVersion: uint8(m.patch_version)}) != g.Metadata {
		c.errf("metadata: C %v, Go %v", m, g.Metadata)
	}
}

var (
	caseRE = regexp.MustCompile(`(?m)^\tcase (\d+):$`)
	litRE  = regexp.MustCompile(`'(?:\\.[0-9a-fA-F]*|[^\\'])'|0x[0-9a-fA-F]+|\b\d+\b`)
)

// lexSource reads the states and the code points a generated lex function
// mentions. Only the choice of test inputs depends on it.
func lexSource(src string) (states []uint16, points []int32) {
	seen := map[int32]bool{}
	for _, line := range strings.Split(src, "\n") {
		if m := caseRE.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			states = append(states, uint16(n))
			continue
		}
		if !strings.Contains(line, "lookahead") && !strings.Contains(line, "case ") && !strings.Contains(line, "Start:") {
			continue
		}
		for _, lit := range litRE.FindAllString(line, -1) {
			var v int64
			if lit[0] == '\'' {
				r, _, _, err := strconv.UnquoteChar(lit[1:len(lit)-1], '\'')
				if err != nil {
					continue
				}
				v = int64(r)
			} else {
				v, _ = strconv.ParseInt(lit, 0, 64)
			}
			// neighbours catch off-by-one range ends; +0x10000 catches a
			// key cut to 16 bits
			for _, p := range []int64{v - 1, v, v + 1, v + 0x10000} {
				if p >= -1 && p <= 0x10FFFF && !seen[int32(p)] {
					seen[int32(p)] = true
					points = append(points, int32(p))
				}
			}
		}
	}
	for _, p := range []int32{-1, 0, 0x7F, 0x80, 0xFFFF, 0x10000, 0x10FFFF} {
		if !seen[p] {
			points = append(points, p)
		}
	}
	slices.Sort(points)
	return states, points
}

func decode(b []byte, dst []int32) []int32 {
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		if r == utf8.RuneError && n <= 1 {
			r = -1
		}
		dst = append(dst, r)
		b = b[n:]
	}
	return dst
}

// corpus returns up to limit code points of real files for one grammar.
func corpus(dirs []string, limit int) []int32 {
	var text []int32
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*"))
		sort.Strings(files)
		for _, f := range files {
			if len(text) >= limit {
				break
			}
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			if len(b) > limit/8 {
				b = b[:limit/8]
			}
			text = decode(b, text)
			text = append(text, '\n')
		}
		limit += limit / 4 // room for the next directory
	}
	return text
}

func (c *checker) lex(lexGo string, dirs []string, textLimit int) (steps, textRuns int) {
	parts := strings.SplitN(lexGo, "\nfunc lexKeywords(", 2)
	var l gositter.Lexer
	rng := rand.New(rand.NewSource(1))
	text := corpus(dirs, textLimit)
	for fi, src := range parts {
		kw := fi == 1
		fn := c.g.LexFn
		if kw {
			fn = c.g.KeywordLexFn
		}
		states, points := lexSource(src)
		if len(states) == 0 {
			c.errf("lex.go: no states found")
			return
		}
		states = append(states, slices.Max(states)+1, 0xFFFF) // the default branch
		report := func(state uint16, in []int32, cv, gv uint64) {
			c.errf("lex kw=%v state %d input %v: C digest %x, Go %x", kw, state, in[:min(len(in), 12)], cv, gv)
		}

		// Step test: every state on every code point the function mentions,
		// alone and followed by a few tails.
		tails := [][]int32{nil}
		for range 5 {
			t := make([]int32, 1+rng.Intn(8))
			for i := range t {
				t[i] = points[rng.Intn(len(points))]
				if rng.Intn(3) > 0 { // mostly printable ASCII
					t[i] = int32(32 + rng.Intn(95))
				}
			}
			tails = append(tails, t)
		}
		out := make([]uint64, len(points))
		for _, state := range states {
			if cv, gv := cRun1(c.c, kw, state, nil), l.MockRun(fn, gositter.StateID(state), nil); cv != gv {
				report(state, nil, cv, gv)
			}
			for _, tail := range tails {
				buf := append([]int32{0}, tail...)
				cRunFirsts(c.c, kw, state, buf, points, out)
				for i, p := range points {
					buf[0] = p
					if gv := l.MockRun(fn, gositter.StateID(state), buf); gv != out[i] {
						report(state, buf, out[i], gv)
						break
					}
				}
			}
			steps += 1 + len(tails)*len(points)
			if len(c.errs) > 0 {
				return
			}
		}

		// Text test: lex real source from every offset, in every lex state
		// the parser can ask for.
		starts := []uint16{0}
		if !kw {
			starts = starts[:0]
			for _, m := range c.g.LexModes {
				if m.LexState != 0xFFFF && !slices.Contains(starts, m.LexState) {
					starts = append(starts, m.LexState)
				}
			}
		}
		out = make([]uint64, len(text)+1)
		for _, state := range starts {
			t0 := time.Now()
			cRunSuffixes(c.c, kw, state, text, 1, out)
			t1 := time.Now()
			for p := 0; p <= len(text); p++ {
				l.MockRun(fn, gositter.StateID(state), text[p:])
			}
			c.cTime += t1.Sub(t0)
			c.goTime += time.Since(t1)
			for p := 0; p <= len(text); p++ {
				if gv := l.MockRun(fn, gositter.StateID(state), text[p:]); gv != out[p] {
					report(state, text[p:], out[p], gv)
					return
				}
			}
			textRuns += len(text) + 1
		}
	}
	return
}

func main() {
	tree := flag.String("tree", "", "the copy of the generated grammars that this binary was built from")
	zoo := flag.String("zoo", "/tmp/gfy-nocgo/refute/zoo/same", "directory with real files per grammar")
	adv := flag.String("adv", "/tmp/gfy-nocgo/refute/adv", "directory with broken files per grammar")
	textLimit := flag.Int("text", 60000, "code points of real text per grammar")
	jobs := flag.Int("j", 0, "grammars checked at once (default: all; use 1 for stable timings)")
	flag.Parse()

	names := flag.Args()
	if len(names) == 0 {
		for n := range goLangs {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	results := make([]string, len(names))
	failed := false
	var wg sync.WaitGroup
	var mu sync.Mutex
	if *jobs <= 0 {
		*jobs = len(names)
	}
	sem := make(chan struct{}, *jobs)
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cl, ok := cLangs[name]
			gl := goLangs[name]
			if !ok || gl == nil {
				results[i] = "FAIL " + name + ": not in this build"
				mu.Lock()
				failed = true
				mu.Unlock()
				return
			}
			c := &checker{name: name, c: (*cLang)(cl.lang()), g: gl()}
			c.tables(cl.cName)
			steps, textRuns := 0, 0
			if len(c.errs) == 0 {
				src, err := os.ReadFile(filepath.Join(*tree, name, "lex.go"))
				if err != nil {
					c.errf("%v", err)
				} else {
					dir := map[string]string{"csharp": "c-sharp", "golang": "go"}[name]
					if dir == "" {
						dir = name
					}
					steps, textRuns = c.lex(string(src), []string{filepath.Join(*zoo, dir), filepath.Join(*adv, dir)}, *textLimit)
				}
			}
			if len(c.errs) > 0 {
				mu.Lock()
				failed = true
				mu.Unlock()
				results[i] = "FAIL " + name + "\n  " + strings.Join(c.errs, "\n  ")
				return
			}
			results[i] = fmt.Sprintf("ok   %-10s ABI %d  %9d table values and lookups equal  %9d lex step runs  %9d lex text runs  (text runs: C %4d ms, Go %4d ms, Go/C %.2f)",
				name, c.g.ABIVersion, c.values, steps, textRuns, c.cTime.Milliseconds(), c.goTime.Milliseconds(), float64(c.goTime)/float64(c.cTime))
		}()
	}
	wg.Wait()
	fmt.Println(strings.Join(results, "\n"))
	if failed {
		os.Exit(1)
	}
}
