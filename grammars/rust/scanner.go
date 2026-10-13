package rust

// Port of tree-sitter-rust v0.24.2 src/scanner.c. Statement order and the
// Advance/MarkEnd/EOF call sequence follow C on every path.

import "github.com/dobbo-ca/gositter"

// enum TokenType
const (
	tokStringContent = iota
	tokStringClose
	tokRawStringLiteralStart
	tokRawStringLiteralContent
	tokRawStringLiteralEnd
	tokFloatLiteral
	tokBlockOuterDocMarker
	tokBlockInnerDocMarker
	tokBlockCommentContent
	tokLineDocContent
	tokErrorSentinel
)

// Scanner
type scanner struct {
	openingHashCount uint8
}

// tree_sitter_rust_external_scanner_create
func newScanner() gositter.ExternalScanner { return &scanner{} }

// tree_sitter_rust_external_scanner_serialize
func (s *scanner) Serialize(buf []byte) int {
	buf[0] = s.openingHashCount
	return 1
}

// tree_sitter_rust_external_scanner_deserialize
func (s *scanner) Deserialize(buf []byte) {
	s.openingHashCount = 0
	if len(buf) == 1 {
		s.openingHashCount = buf[0]
	}
}

// The isw* functions run in the C locale (nothing calls setlocale), where
// they are true for ASCII only; -1 (invalid UTF-8) is WEOF and false.

// iswdigit
func iswdigit(c int32) bool { return c >= '0' && c <= '9' }

// iswalpha
func iswalpha(c int32) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// iswspace
func iswspace(c int32) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

// is_num_char
func isNumChar(c int32) bool { return c == '_' || iswdigit(c) }

// process_string
func processString(l *gositter.Lexer) bool {
	hasContent := false
	for {
		if l.Lookahead == '"' || l.Lookahead == '\\' {
			break
		}
		if l.EOF() {
			return false
		}
		hasContent = true
		l.Advance(false)
	}
	l.ResultSymbol = tokStringContent
	l.MarkEnd()
	return hasContent
}

// scan_raw_string_start
func (s *scanner) scanRawStringStart(l *gositter.Lexer) bool {
	if l.Lookahead == 'b' || l.Lookahead == 'c' {
		l.Advance(false)
	}
	if l.Lookahead != 'r' {
		return false
	}
	l.Advance(false)

	// uint8_t: 256 hashes wrap to 0, as in C.
	var openingHashCount uint8
	for l.Lookahead == '#' {
		l.Advance(false)
		openingHashCount++
	}

	if l.Lookahead != '"' {
		return false
	}
	l.Advance(false)
	s.openingHashCount = openingHashCount

	l.ResultSymbol = tokRawStringLiteralStart
	return true
}

// scan_raw_string_content
func (s *scanner) scanRawStringContent(l *gositter.Lexer) bool {
	for {
		if l.EOF() {
			return false
		}
		if l.Lookahead == '"' {
			l.MarkEnd()
			l.Advance(false)
			hashCount := uint32(0)
			for l.Lookahead == '#' && hashCount < uint32(s.openingHashCount) {
				l.Advance(false)
				hashCount++
			}
			if hashCount == uint32(s.openingHashCount) {
				l.ResultSymbol = tokRawStringLiteralContent
				return true
			}
		} else {
			l.Advance(false)
		}
	}
}

// scan_raw_string_end
func (s *scanner) scanRawStringEnd(l *gositter.Lexer) bool {
	l.Advance(false)
	for i := uint32(0); i < uint32(s.openingHashCount); i++ {
		l.Advance(false)
	}
	l.ResultSymbol = tokRawStringLiteralEnd
	return true
}

// process_float_literal
func processFloatLiteral(l *gositter.Lexer) bool {
	l.ResultSymbol = tokFloatLiteral

	l.Advance(false)
	for isNumChar(l.Lookahead) {
		l.Advance(false)
	}

	hasFraction, hasExponent := false, false

	if l.Lookahead == '.' {
		hasFraction = true
		l.Advance(false)
		if iswalpha(l.Lookahead) {
			// 1.max(2) is an integer and a method call.
			return false
		}

		if l.Lookahead == '.' {
			return false
		}
		for isNumChar(l.Lookahead) {
			l.Advance(false)
		}
	}

	l.MarkEnd()

	if l.Lookahead == 'e' || l.Lookahead == 'E' {
		hasExponent = true
		l.Advance(false)
		if l.Lookahead == '+' || l.Lookahead == '-' {
			l.Advance(false)
		}
		if !isNumChar(l.Lookahead) {
			return true
		}
		l.Advance(false)
		for isNumChar(l.Lookahead) {
			l.Advance(false)
		}

		l.MarkEnd()
	}

	if !hasExponent && !hasFraction {
		return false
	}

	if l.Lookahead != 'u' && l.Lookahead != 'i' && l.Lookahead != 'f' {
		return true
	}
	l.Advance(false)
	if !iswdigit(l.Lookahead) {
		return true
	}

	for iswdigit(l.Lookahead) {
		l.Advance(false)
	}

	l.MarkEnd()
	return true
}

// process_line_doc_content
func processLineDocContent(l *gositter.Lexer) bool {
	l.ResultSymbol = tokLineDocContent
	for {
		if l.EOF() {
			return true
		}
		if l.Lookahead == '\n' {
			l.Advance(false)
			return true
		}
		l.Advance(false)
	}
}

// BlockCommentState
const (
	leftForwardSlash = iota
	leftAsterisk
	continuing
)

// BlockCommentProcessing
type blockCommentProcessing struct {
	state        int
	nestingDepth uint32
}

// process_left_forward_slash
func (p *blockCommentProcessing) processLeftForwardSlash(current byte) {
	if current == '*' {
		p.nestingDepth += 1
	}
	p.state = continuing
}

// process_left_asterisk
func (p *blockCommentProcessing) processLeftAsterisk(current byte, l *gositter.Lexer) {
	if current == '*' {
		l.MarkEnd()
		p.state = leftAsterisk
		return
	}

	if current == '/' {
		p.nestingDepth -= 1
	}

	p.state = continuing
}

// process_continuing
func (p *blockCommentProcessing) processContinuing(current byte) {
	switch current {
	case '/':
		p.state = leftForwardSlash
	case '*':
		p.state = leftAsterisk
	}
}

// process_block_comment
func processBlockComment(l *gositter.Lexer, validSymbols []bool) bool {
	// (char)lookahead: C keeps the low byte only, so U+012A acts as '*'.
	// Only compared for equality with ASCII, so signedness does not matter.
	first := byte(l.Lookahead)
	if validSymbols[tokBlockInnerDocMarker] && first == '!' {
		l.ResultSymbol = tokBlockInnerDocMarker
		l.Advance(false)
		return true
	}
	if validSymbols[tokBlockOuterDocMarker] && first == '*' {
		l.Advance(false)
		l.MarkEnd()
		// "/**/" is an empty block comment.
		if l.Lookahead == '/' {
			return false
		}
		// Three or more stars is not a doc marker.
		if l.Lookahead != '*' {
			l.ResultSymbol = tokBlockOuterDocMarker
			return true
		}
	} else {
		l.Advance(false)
	}

	if validSymbols[tokBlockCommentContent] {
		processing := blockCommentProcessing{continuing, 1}
		switch first {
		case '*':
			processing.state = leftAsterisk
			if l.Lookahead == '/' {
				// Empty doc block comment such as /*!*/.
				return false
			}
		case '/':
			processing.state = leftForwardSlash
		default:
			processing.state = continuing
		}

		// An unterminated comment still succeeds at EOF.
		for !l.EOF() && processing.nestingDepth != 0 {
			first = byte(l.Lookahead)
			switch processing.state {
			case leftForwardSlash:
				processing.processLeftForwardSlash(first)
			case leftAsterisk:
				processing.processLeftAsterisk(first, l)
			case continuing:
				l.MarkEnd()
				processing.processContinuing(first)
			}
			l.Advance(false)
			if first == '/' && processing.nestingDepth != 0 {
				l.MarkEnd()
			}
		}
		l.ResultSymbol = tokBlockCommentContent
		return true
	}

	return false
}

// tree_sitter_rust_external_scanner_scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	// Error recovery marks every token valid; the scanner gives up then.
	if validSymbols[tokErrorSentinel] {
		return false
	}

	if validSymbols[tokBlockCommentContent] || validSymbols[tokBlockInnerDocMarker] ||
		validSymbols[tokBlockOuterDocMarker] {
		return processBlockComment(l, validSymbols)
	}

	if validSymbols[tokStringContent] && !validSymbols[tokFloatLiteral] {
		if processString(l) {
			return true
		}
		// No content before '"' or '\': fall through to STRING_CLOSE.
	}

	if validSymbols[tokStringClose] && l.Lookahead == '"' {
		l.Advance(false)
		l.ResultSymbol = tokStringClose
		l.MarkEnd()
		return true
	}

	if validSymbols[tokLineDocContent] {
		return processLineDocContent(l)
	}

	for iswspace(l.Lookahead) {
		l.Advance(true)
	}

	if validSymbols[tokRawStringLiteralStart] &&
		(l.Lookahead == 'r' || l.Lookahead == 'b' || l.Lookahead == 'c') {
		return s.scanRawStringStart(l)
	}

	if validSymbols[tokRawStringLiteralContent] {
		return s.scanRawStringContent(l)
	}

	if validSymbols[tokRawStringLiteralEnd] && l.Lookahead == '"' {
		return s.scanRawStringEnd(l)
	}

	if validSymbols[tokFloatLiteral] && iswdigit(l.Lookahead) {
		return processFloatLiteral(l)
	}

	return false
}
