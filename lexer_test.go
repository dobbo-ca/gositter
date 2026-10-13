package gositter

import "testing"

func newTestLexer(src string) *Lexer {
	l := &Lexer{}
	l.setInput([]byte(src))
	l.start()
	return l
}

type decoded struct {
	c    int32
	size uint32
}

func TestLexerDecode(t *testing.T) {
	bad := decoded{decodeError, 1}
	cases := []struct {
		name string
		src  string
		want []decoded
	}{
		{"ascii", "ab", []decoded{{'a', 1}, {'b', 1}}},
		{"nul byte is not eof", "a\x00b", []decoded{{'a', 1}, {0, 1}, {'b', 1}}},
		{"two byte", "é", []decoded{{0xE9, 2}}},
		{"three byte", "€", []decoded{{0x20AC, 3}}},
		{"four byte", "\U0001F600", []decoded{{0x1F600, 4}}},
		{"max", "\U0010FFFF", []decoded{{0x10FFFF, 4}}},
		{"valid U+FFFD", "\xef\xbf\xbd", []decoded{{0xFFFD, 3}}},
		{"lone continuation", "\x80", []decoded{bad}},
		{"0xff", "\xffa", []decoded{bad, {'a', 1}}},
		{"overlong 2", "\xc0\x80", []decoded{bad, bad}},
		{"overlong 3", "\xe0\x80\x80", []decoded{bad, bad, bad}},
		{"surrogate", "\xed\xa0\x80", []decoded{bad, bad, bad}},
		{"above max", "\xf4\x90\x80\x80", []decoded{bad, bad, bad, bad}},
		{"0xf5 lead", "\xf5\x80\x80\x80", []decoded{bad, bad, bad, bad}},
		{"truncated at end", "a\xe2\x82", []decoded{{'a', 1}, bad, bad}},
		{"truncated then ascii", "\xe2\x82a", []decoded{bad, bad, {'a', 1}}},
	}
	for _, tc := range cases {
		l := newTestLexer(tc.src)
		for i, want := range tc.want {
			if l.EOF() {
				t.Fatalf("%s: eof before char %d", tc.name, i)
			}
			if got := (decoded{l.Lookahead, l.lookaheadSize}); got != want {
				t.Fatalf("%s: char %d = %+v, want %+v", tc.name, i, got, want)
			}
			l.Advance(false)
		}
		if !l.EOF() || l.Lookahead != 0 {
			t.Fatalf("%s: at end eof=%v lookahead=%d", tc.name, l.EOF(), l.Lookahead)
		}
		if got := l.currentPosition.bytes; got != uint32(len(tc.src)) {
			t.Fatalf("%s: end position %d, want %d", tc.name, got, len(tc.src))
		}
	}
}

func TestLexerAdvanceAfterEOF(t *testing.T) {
	logged := 0
	l := newTestLexer("x")
	l.logger = func(LogType, string) { logged++ }
	l.Advance(false)
	if !l.EOF() || logged != 1 {
		t.Fatalf("eof=%v logged=%d", l.EOF(), logged)
	}
	before := l.currentPosition
	l.Advance(false)
	l.Advance(true)
	if l.currentPosition != before || logged != 1 || l.Lookahead != 0 {
		t.Fatalf("advance after eof moved or logged: %+v logged=%d", l.currentPosition, logged)
	}
}

func TestLexerEmptyInput(t *testing.T) {
	l := newTestLexer("")
	if !l.EOF() || l.Lookahead != 0 || l.IsAtIncludedRangeStart() {
		t.Fatalf("eof=%v lookahead=%d atStart=%v", l.EOF(), l.Lookahead, l.IsAtIncludedRangeStart())
	}
}

func TestLexerBOMAndPositions(t *testing.T) {
	l := newTestLexer("\xef\xbb\xbfa\néb")
	// The BOM is skipped by start: token start moves past it.
	if l.Lookahead != 'a' || l.tokenStartPosition.bytes != 3 || l.currentPosition.extent.Column != 3 {
		t.Fatalf("after BOM: lookahead=%q start=%+v pos=%+v", l.Lookahead, l.tokenStartPosition, l.currentPosition)
	}
	if got := l.GetColumn(); got != 0 {
		t.Fatalf("column after BOM = %d, want 0", got)
	}
	l.Advance(false) // a
	l.Advance(false) // \n
	if l.currentPosition != (length{5, Point{1, 0}}) {
		t.Fatalf("after newline: %+v", l.currentPosition)
	}
	l.Advance(false) // é, two bytes, one column of GetColumn
	if l.currentPosition != (length{7, Point{1, 2}}) {
		t.Fatalf("after é: %+v", l.currentPosition)
	}
	if got := l.GetColumn(); got != 1 {
		t.Fatalf("GetColumn = %d, want 1", got)
	}

	// With the cached column gone, GetColumn re-scans the line and comes
	// back to the same place.
	l.invalidateColumnData()
	if got := l.GetColumn(); got != 1 || !l.didGetColumn {
		t.Fatalf("GetColumn after invalidate = %d", got)
	}
	if l.currentPosition != (length{7, Point{1, 2}}) || l.Lookahead != 'b' {
		t.Fatalf("after re-scan: %+v lookahead=%q", l.currentPosition, l.Lookahead)
	}
}

func TestLexerIsAtIncludedRangeStart(t *testing.T) {
	l := newTestLexer("ab")
	if !l.IsAtIncludedRangeStart() {
		t.Fatal("want true at byte 0")
	}
	l.Advance(false)
	if l.IsAtIncludedRangeStart() {
		t.Fatal("want false at byte 1")
	}
}

func TestLexerFinish(t *testing.T) {
	l := newTestLexer("ab")
	l.Advance(false)
	end := uint32(0)
	l.finish(&end)
	// mark_end was never called, so the token ends at the current position.
	if l.tokenEndPosition.bytes != 1 || end != 2 {
		t.Fatalf("tokenEnd=%d lookaheadEnd=%d", l.tokenEndPosition.bytes, end)
	}

	l = newTestLexer("a\xff")
	l.Advance(false)
	l.MarkEnd()
	end = 0
	l.finish(&end)
	if end != 1+1+4 {
		t.Fatalf("lookaheadEnd on invalid UTF-8 = %d, want 6", end)
	}
}

func TestLexerResetBackwards(t *testing.T) {
	l := newTestLexer("abc")
	l.Advance(false)
	l.Advance(false)
	l.reset(length{1, Point{0, 1}})
	l.start()
	if l.Lookahead != 'b' || l.currentPosition.bytes != 1 {
		t.Fatalf("lookahead=%q pos=%+v", l.Lookahead, l.currentPosition)
	}
	// Reset to the end and back: EOF must clear.
	l.reset(length{3, Point{0, 3}})
	l.start()
	if !l.EOF() {
		t.Fatal("want eof at end")
	}
	l.reset(length{2, Point{0, 2}})
	l.start()
	if l.EOF() || l.Lookahead != 'c' {
		t.Fatalf("eof=%v lookahead=%q", l.EOF(), l.Lookahead)
	}
}

// advanceRunRef is the definition of AdvanceRun: Advance, one character at
// a time.
func advanceRunRef(l *Lexer, set *[256]bool, skip bool) {
	for uint32(l.Lookahead) < 128 && set[l.Lookahead] && l.hasChunk {
		l.Advance(skip)
	}
}

// Everything a lex function or the parser can see of a lexer.
func lexerSnapshot(l *Lexer) [16]uint32 {
	b := func(v bool) uint32 {
		if v {
			return 1
		}
		return 0
	}
	return [16]uint32{
		uint32(l.Lookahead), l.lookaheadSize, b(l.hasChunk), b(l.EOF()),
		l.currentPosition.bytes, l.currentPosition.extent.Row, l.currentPosition.extent.Column,
		l.tokenStartPosition.bytes, l.tokenStartPosition.extent.Row, l.tokenStartPosition.extent.Column,
		l.columnData.value, b(l.columnData.valid), l.chunkStart, l.chunkSize, l.GetColumn(),
	}
}

func checkAdvanceRun(t *testing.T, src []byte, set *[256]bool, start int, skip, columnValid bool) {
	t.Helper()
	var ls [2]*Lexer
	for i := range ls {
		l := newTestLexer(string(src))
		for l.currentPosition.bytes < uint32(start) && l.hasChunk {
			l.Advance(false)
		}
		if !columnValid {
			l.invalidateColumnData()
		}
		ls[i] = l
	}
	advanceRunRef(ls[0], set, skip)
	ls[1].AdvanceRun(set, skip)
	if want, got := lexerSnapshot(ls[0]), lexerSnapshot(ls[1]); got != want {
		t.Fatalf("src %q start %d skip %v:\n got %v\nwant %v", src, start, skip, got, want)
	}
}

func TestAdvanceRun(t *testing.T) {
	var ident, blank, notQuote [256]bool
	for c := 1; c < 128; c++ {
		ident[c] = c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z'
		blank[c] = c == ' ' || '\t' <= c && c <= '\r'
		notQuote[c] = c != '"'
	}
	srcs := []string{
		"", "a", "abc", "abc def", "  \n\t x", "\n\n\n", "ab\n", "abc\xc3\xa9def", "ab\xffcd",
		"\xef\xbb\xbfabc", "a\x00b", "line one\nline two\r\n  three\"x", "é\nabc", "abc\n\xe2\x82",
	}
	for _, src := range srcs {
		for _, set := range []*[256]bool{&ident, &blank, &notQuote} {
			for start := 0; start <= len(src); start++ {
				for _, skip := range []bool{false, true} {
					checkAdvanceRun(t, []byte(src), set, start, skip, true)
					checkAdvanceRun(t, []byte(src), set, start, skip, false)
				}
			}
		}
	}
}

func FuzzAdvanceRun(f *testing.F) {
	f.Add([]byte("ab cd\n  ef\xc3\xa9 \"g\"\n"), uint8(0), uint8(3), true)
	f.Fuzz(func(t *testing.T, src []byte, which, start uint8, skip bool) {
		var set [256]bool
		for c := 1; c < 128; c++ {
			switch which % 4 {
			case 0:
				set[c] = c == ' ' || '\t' <= c && c <= '\r'
			case 1:
				set[c] = c != '\n' && c != '"'
			case 2:
				set[c] = c&1 == 0
			default:
				set[c] = true
			}
		}
		checkAdvanceRun(t, src, &set, int(start)%(len(src)+1), skip, which&4 == 0)
	})
}
