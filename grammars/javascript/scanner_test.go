package javascript

import (
	"testing"

	"github.com/dobbo-ca/gositter"
)

// C serialize returns 0 and deserialize ignores its input.
func TestSerializeRoundTrip(t *testing.T) {
	s := newScanner()
	buf := make([]byte, gositter.SerializationBufferSize)
	for i := range buf {
		buf[i] = 0xAA
	}
	if n := s.Serialize(buf); n != 0 {
		t.Fatalf("Serialize = %d, want 0", n)
	}
	for i, b := range buf {
		if b != 0xAA {
			t.Fatalf("Serialize wrote buf[%d]", i)
		}
	}
	s.Deserialize(nil)
	s.Deserialize(buf)
	if n := s.Serialize(buf); n != 0 {
		t.Fatalf("Serialize after Deserialize = %d, want 0", n)
	}
}

// The token order must be the grammar's externals order.
func TestTokenOrder(t *testing.T) {
	want := []gositter.Symbol{
		tokAutomaticSemicolon: sym__automatic_semicolon,
		tokTemplateChars:      sym__template_chars,
		tokTernaryQmark:       sym__ternary_qmark,
		tokHTMLComment:        sym_html_comment,
		tokLogicalOr:          anon_sym_PIPE_PIPE,
		tokEscapeSequence:     sym_escape_sequence,
		tokRegexPattern:       sym_regex_pattern,
		tokJSXText:            sym_jsx_text,
	}
	for i, w := range want {
		if ts_external_scanner_symbol_map[i] != w {
			t.Errorf("symbol_map[%d] = %d, want %d", i, ts_external_scanner_symbol_map[i], w)
		}
	}
}

// libc "C" locale on darwin: ASCII only (scratch/scan/javascript/isw.c).
func TestIsw(t *testing.T) {
	for c := int32(-1); c <= 0x3000; c++ {
		sp := c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
		al := (c|0x20) >= 'a' && (c|0x20) <= 'z' && c > 0
		di := c >= '0' && c <= '9'
		if iswspace(c) != sp || iswalpha(c) != al || iswdigit(c) != di {
			t.Fatalf("isw*(%#x) = %v %v %v, want %v %v %v", c, iswspace(c), iswalpha(c), iswdigit(c), sp, al, di)
		}
	}
}

func TestScanSmoke(t *testing.T) {
	p := gositter.NewParser()
	if err := p.SetLanguage(Language()); err != nil {
		t.Fatal(err)
	}
	tree := p.Parse([]byte("let a = 1\nlet b = `x${a}y`\nconst c = a ? b : /re/g\n"))
	defer tree.Close()
	if tree.RootNode().HasError() {
		t.Fatalf("unexpected error: %s", tree.RootNode().String())
	}
}
