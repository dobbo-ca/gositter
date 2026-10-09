package main

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"
)

const miniC = `
#define LANGUAGE_VERSION 14
#define LARGE_STATE_COUNT 2
enum ts_symbol_identifiers { sym_a = 1, sym_b = 2, };
static const char * const ts_symbol_names[] = { [ts_builtin_sym_end] = "end", [sym_a] = "\"a\?", [sym_b] = "\0", };
static TSCharacterRange set_1[] = { {'a', 'z'}, {0xaa, 0xaa}, };
static const uint16_t ts_small_parse_table[] = { [0] = 1, ACTIONS(3), 1, sym_b, [7] = 2, };
static const uint32_t ts_small_parse_table_map[] = { [SMALL_STATE(2)] = 0, [SMALL_STATE(3)] = 7, };
static const TSLexMode ts_lex_modes[4] = { [1] = {.lex_state = 5, .external_lex_state = 2}, [3] = {(TSStateId)(-1),}, };
static const TSParseActionEntry ts_parse_actions[] = {
  [0] = {.entry = {.count = 0, .reusable = false}},
  [1] = {.entry = {.count = 2, .reusable = true}}, REDUCE(sym_b, 2, -1, 7), SHIFT_REPEAT(9),
  [4] = {.entry = {.count = 1, .reusable = false}}, SHIFT_EXTRA(),
};
static bool ts_lex(TSLexer *lexer, TSStateId state) {
  START_LEXER();
  eof = lexer->eof(lexer);
  switch (state) {
    case 0:
      if (eof) ADVANCE(1);
      ADVANCE_MAP('"', 2, '\\', 3, 0xa0, 4, );
      if (('\t' <= lookahead && lookahead <= '\r') ||
          lookahead == ' ') SKIP(0);
      if ((!eof && set_contains(set_1, 2, lookahead))) ADVANCE(5);
      if (lookahead != 0 &&
          (lookahead < '(' || ')' < lookahead)) ADVANCE(6);
      END_STATE();
    case 1:
      ACCEPT_TOKEN(ts_builtin_sym_end);
      END_STATE();
    default:
      return false;
  }
}
`

func TestParse(t *testing.T) {
	g, err := parseGrammar(miniC)
	if err != nil {
		t.Fatal(err)
	}
	eq := func(what string, got any, want string) {
		t.Helper()
		if g := fmt.Sprint(got); g != want {
			t.Errorf("%s:\n got %s\nwant %s", what, g, want)
		}
	}
	eq("names", g.tables["ts_symbol_names"].strs, `[end "a? ]`) // "\0" is the C string ""
	eq("small", g.tables["ts_small_parse_table"].vals, "[1 3 1 2 0 0 0 2]")
	eq("small map", g.tables["ts_small_parse_table_map"].vals, "[0 7]")
	eq("lex modes", g.tables["ts_lex_modes"].vals, "[0 0 5 2 0 0 65535 0]")
	eq("actions", g.actions, "[0 0 0 0 0 2 1 0 0 0 1 2 2 -1 7 0 0 9 2 0 1 0 0 0 0 0 0 0 1 0]")

	var b bytes.Buffer
	e := &emitter{g: g, pkg: "mini"}
	w := bufio.NewWriter(&b)
	e.scalarTable(w, &table{name: "t", ctype: "uint16_t", dims: []int{12}, vals: []int64{0, 5, 0, 0, 0, 0, 0, 7, 8, 0, 0, 0}})
	w.Flush()
	e.lex(&b)
	for _, want := range []string{
		"var t = []uint16{\n\t1: 5, 0,\n\t7: 7, 8,\n\t11: 0,\n}", // zero runs skipped, length kept
		"var set_1 = [2]gositter.CharacterRange{\n\t{Start: 0x61, End: 0x7a}, {Start: 0xaa, End: 0xaa},\n}",
		"\t\tif eof {\n\t\t\tstate = 1\n\t\t\tgoto next_state\n\t\t}\n\t\tswitch lookahead {\n\t\tcase '\"':\n\t\t\tstate = 2",
		"\t\tcase '\\\\':\n\t\t\tstate = 3\n\t\t\tgoto next_state\n\t\tcase 0xa0:\n\t\t\tstate = 4",
		"\t\tif ('\\t' <= lookahead && lookahead <= '\\r') || lookahead == ' ' {\n\t\t\tskip = true\n\t\t\tstate = 0\n",
		"\t\tif !eof && gositter.SetContainsASCII(&set_1_ascii, set_1[:], lookahead) {",
		"var set_1_ascii = [128]bool{\n\t97: true, 98: true,", // a-z
		"\t\tif lookahead != 0 && (lookahead < '(' || ')' < lookahead) {",
		"\tcase 1:\n\t\tresult = true\n\t\tl.ResultSymbol = ts_builtin_sym_end\n\t\tl.MarkEnd()\n\t\treturn result\n\tdefault:\n\t\treturn false",
	} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("lex.go lacks %q\n%s", want, b.String())
		}
	}
}

// What C would silently accept or cut must fail here.
func TestRejects(t *testing.T) {
	lexFn := func(body string) string {
		return "enum e { sym_a = 1 };\nstatic TSCharacterRange set_1[] = { {'a', 'z'} };\n" +
			"static bool ts_lex(TSLexer *lexer, TSStateId state) {\nSTART_LEXER();\neof = lexer->eof(lexer);\nswitch (state) {\ncase 0:\n" +
			body + "\ndefault:\nreturn false;\n}\n}\n"
	}
	for name, src := range map[string]string{
		"map key above 16 bits": lexFn("ADVANCE_MAP(0x10000, 1,);\nEND_STATE();"),
		"duplicate map key":     lexFn("ADVANCE_MAP('a', 1, 0x61, 2,);\nEND_STATE();"),
		"wrong set length":      lexFn("if (set_contains(set_1, 2, lookahead)) ADVANCE(1);\nEND_STATE();"),
		"case falls through":    lexFn("ACCEPT_TOKEN(sym_a);\ncase 1:\nEND_STATE();"),
		"unknown symbol":        lexFn("ACCEPT_TOKEN(sym_zzz);\nEND_STATE();"),
		"unknown operator":      lexFn("if (lookahead & 1) ADVANCE(1);\nEND_STATE();"),
		"child count above 255": "enum e { sym_a = 1 };\nstatic const TSParseActionEntry ts_parse_actions[] = { REDUCE(sym_a, 256, 0, 0), };",
		"index past dimension":  "static const uint16_t t[2] = { [2] = 1, };",
	} {
		if _, err := parseGrammar(src); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
