package lua

// Port of tree-sitter-lua v0.5.0 src/scanner.c. Statement order and the
// Advance/MarkEnd call sequence follow C on every path.

import "github.com/dobbo-ca/gositter"

// enum TokenType
const (
	tokBlockCommentStart = iota
	tokBlockCommentContent
	tokBlockCommentEnd
	tokBlockStringStart
	tokBlockStringContent
	tokBlockStringEnd
)

// Scanner. C never assigns ending_char a non-zero value; it only comes back
// from Deserialize, so it is 0 on every reachable path. Kept for the bytes.
type scanner struct {
	endingChar int8 // char, signed on the oracle's targets
	levelCount uint8
}

// tree_sitter_lua_external_scanner_create
func newScanner() gositter.ExternalScanner { return &scanner{} }

// iswspace in the "C" locale, which is what a process that never calls
// setlocale gets: the six ASCII spaces, nothing above 0x7F, not -1.
func iswspace(c int32) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

// consume_char
func consumeChar(c int32, l *gositter.Lexer) bool {
	if l.Lookahead != c {
		return false
	}
	l.Advance(false)
	return true
}

// consume_and_count_char. uint8 on purpose: 256 '=' wrap to level 0.
func consumeAndCountChar(c int32, l *gositter.Lexer) uint8 {
	var count uint8
	for l.Lookahead == c {
		count++
		l.Advance(false)
	}
	return count
}

// reset_state
func (s *scanner) resetState() {
	s.endingChar = 0
	s.levelCount = 0
}

// tree_sitter_lua_external_scanner_serialize
func (s *scanner) Serialize(buf []byte) int {
	buf[0] = byte(s.endingChar)
	buf[1] = s.levelCount
	return 2
}

// tree_sitter_lua_external_scanner_deserialize. Length 0 leaves the state
// as it is, as C does.
func (s *scanner) Deserialize(buf []byte) {
	if len(buf) == 0 {
		return
	}
	s.endingChar = int8(buf[0])
	if len(buf) == 1 {
		return
	}
	s.levelCount = buf[1]
}

// scan_block_start
func (s *scanner) scanBlockStart(l *gositter.Lexer) bool {
	if consumeChar('[', l) {
		level := consumeAndCountChar('=', l)

		if consumeChar('[', l) {
			s.levelCount = level
			return true
		}
	}
	return false
}

// scan_block_end
func (s *scanner) scanBlockEnd(l *gositter.Lexer) bool {
	if consumeChar(']', l) {
		level := consumeAndCountChar('=', l)

		if s.levelCount == level && consumeChar(']', l) {
			return true
		}
	}
	return false
}

// scan_block_content
func (s *scanner) scanBlockContent(l *gositter.Lexer) bool {
	for l.Lookahead != 0 {
		if l.Lookahead == ']' {
			l.MarkEnd()

			if s.scanBlockEnd(l) {
				return true
			}
		} else {
			l.Advance(false)
		}
	}
	return false
}

// scan_comment_start
func (s *scanner) scanCommentStart(l *gositter.Lexer) bool {
	if consumeChar('-', l) && consumeChar('-', l) {
		l.MarkEnd()

		if s.scanBlockStart(l) {
			l.MarkEnd()
			l.ResultSymbol = tokBlockCommentStart
			return true
		}
	}
	return false
}

// scan_comment_content
func (s *scanner) scanCommentContent(l *gositter.Lexer) bool {
	if s.endingChar == 0 { // block comment
		if s.scanBlockContent(l) {
			l.ResultSymbol = tokBlockCommentContent
			return true
		}
		return false
	}

	for l.Lookahead != 0 {
		if l.Lookahead == int32(s.endingChar) {
			s.resetState()
			l.ResultSymbol = tokBlockCommentContent
			return true
		}
		l.Advance(false)
	}
	return false
}

// tree_sitter_lua_external_scanner_scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	if validSymbols[tokBlockStringEnd] && s.scanBlockEnd(l) {
		s.resetState()
		l.ResultSymbol = tokBlockStringEnd
		return true
	}

	if validSymbols[tokBlockStringContent] && s.scanBlockContent(l) {
		l.ResultSymbol = tokBlockStringContent
		return true
	}

	if validSymbols[tokBlockCommentEnd] && s.endingChar == 0 && s.scanBlockEnd(l) {
		s.resetState()
		l.ResultSymbol = tokBlockCommentEnd
		return true
	}

	if validSymbols[tokBlockCommentContent] && s.scanCommentContent(l) {
		return true
	}

	// skip_whitespaces
	for iswspace(l.Lookahead) {
		l.Advance(true)
	}

	if validSymbols[tokBlockStringStart] && s.scanBlockStart(l) {
		l.ResultSymbol = tokBlockStringStart
		return true
	}

	if validSymbols[tokBlockCommentStart] {
		if s.scanCommentStart(l) {
			return true
		}
	}

	return false
}
