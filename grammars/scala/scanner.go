package scala

// Port of tree-sitter-scala v0.26.0 src/scanner.c. Statement order and the
// Advance/MarkEnd/GetColumn/EOF call sequence follow C on every path.

import "github.com/dobbo-ca/gositter"

// enum TokenType
const (
	tokAutomaticSemicolon = iota
	tokIndent
	tokOutdent
	tokCommaOutdent
	tokSimpleStringStart
	tokSimpleStringMiddle
	tokSimpleMultilineStringStart
	tokInterpolatedStringMiddle
	tokInterpolatedMultilineStringMiddle
	tokRawStringStart
	tokRawStringMiddle
	tokRawStringMultilineMiddle
	tokSingleLineStringEnd
	tokMultilineStringEnd
	tokElse
	tokCatch
	tokFinally
	tokExtends
	tokDerives
	tokWith
	tokErrorSentinel
)

// Scanner. All four are int16_t in C and wrap the same way here.
type scanner struct {
	indents             []int16
	lastIndentationSize int16
	lastNewlineCount    int16
	lastColumn          int16
}

// tree_sitter_scala_external_scanner_create
func newScanner() gositter.ExternalScanner {
	return &scanner{lastIndentationSize: -1, lastColumn: -1}
}

// tree_sitter_scala_external_scanner_serialize
func (s *scanner) Serialize(buf []byte) int {
	// Too deep to fit: C stores nothing, so the state reads back as reset.
	if (len(s.indents)+3)*2 > gositter.SerializationBufferSize {
		return 0
	}

	size := 0
	put := func(v int16) { // memcpy of an int16_t, little-endian
		buf[size] = byte(v)
		buf[size+1] = byte(uint16(v) >> 8)
		size += 2
	}
	put(s.lastIndentationSize)
	put(s.lastNewlineCount)
	put(s.lastColumn)
	for _, v := range s.indents {
		put(v)
	}
	return size
}

// tree_sitter_scala_external_scanner_deserialize
func (s *scanner) Deserialize(buf []byte) {
	s.indents = s.indents[:0]
	s.lastIndentationSize = -1
	s.lastColumn = -1
	s.lastNewlineCount = 0

	if len(buf) == 0 {
		return
	}

	// The length is always even and at least 6: it comes from Serialize.
	get := func(i int) int16 { return int16(uint16(buf[i]) | uint16(buf[i+1])<<8) }
	s.lastIndentationSize = get(0)
	s.lastNewlineCount = get(2)
	s.lastColumn = get(4)
	for size := 6; size < len(buf); size += 2 {
		s.indents = append(s.indents, get(size))
	}
}

// iswspace in the "C" locale, which is what a cgo program runs in: measured
// on darwin/arm64 over -2..0x110000, true only for 9..13 and 32.
func iswspace(c int32) bool {
	return c == ' ' || ('\t' <= c && c <= '\r')
}

// iswalnum, same measurement: ASCII letters and digits only.
func iswalnum(c int32) bool {
	return ('0' <= c && c <= '9') || ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z')
}

// is_op_char
func isOpChar(c int32) bool {
	switch c {
	case '!', '#', '%', '&',
		'*', '+', '-', '<',
		'=', '>', '?', '@',
		'\\', '^', '|', '~',
		':':
		return true
	default:
		return false
	}
}

// StringMode
const (
	stringModeSimple = iota
	stringModeInterpolated
	stringModeRaw
)

// scan_string_content
func scanStringContent(l *gositter.Lexer, isMultiline bool, stringMode int) bool {
	closingQuoteCount := uint32(0)
	for {
		if l.Lookahead == '"' {
			l.Advance(false)
			closingQuoteCount++
			if !isMultiline {
				l.ResultSymbol = tokSingleLineStringEnd
				l.MarkEnd()
				return true
			}
			if closingQuoteCount >= 3 && l.Lookahead != '"' {
				l.ResultSymbol = tokMultilineStringEnd
				l.MarkEnd()
				return true
			}
		} else if l.Lookahead == '$' && stringMode != stringModeSimple {
			switch stringMode {
			case stringModeInterpolated:
				l.ResultSymbol = tokInterpolatedStringMiddle
				if isMultiline {
					l.ResultSymbol = tokInterpolatedMultilineStringMiddle
				}
			case stringModeRaw:
				l.ResultSymbol = tokRawStringMiddle
				if isMultiline {
					l.ResultSymbol = tokRawStringMultilineMiddle
				}
			}
			l.MarkEnd()
			return true
		} else {
			closingQuoteCount = 0
			if l.Lookahead == '\\' {
				// Multiline strings ignore escape sequences.
				if isMultiline || stringMode == stringModeRaw {
					l.Advance(false)
					// In a single-line raw string \" does not close the
					// string and \\ keeps the next quote able to.
					if !isMultiline && stringMode == stringModeRaw &&
						(l.Lookahead == '"' || l.Lookahead == '\\') {
						l.Advance(false)
					}
				} else {
					l.ResultSymbol = tokInterpolatedStringMiddle
					if stringMode == stringModeSimple {
						l.ResultSymbol = tokSimpleStringMiddle
					}
					l.MarkEnd()
					return true
				}
				// With every symbol valid (error recovery) this runs outside
				// a string; a newline or the end of input stops it.
			} else if l.Lookahead == '\n' && !isMultiline {
				return false
			} else if l.EOF() {
				return false
			} else {
				l.Advance(false)
			}
		}
	}
}

// detect_comment_start
func detectCommentStart(l *gositter.Lexer) bool {
	l.MarkEnd()
	// Comments should not affect indentation.
	if l.Lookahead == '/' {
		l.Advance(false)
		if l.Lookahead == '/' || l.Lookahead == '*' {
			return true
		}
	}
	return false
}

// scan_word
func scanWord(l *gositter.Lexer, word string) bool {
	for i := 0; i < len(word); i++ {
		if l.Lookahead != int32(word[i]) {
			return false
		}
		l.Advance(false)
	}
	return !iswalnum(l.Lookahead)
}

// tree_sitter_scala_external_scanner_scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	prev := int16(-1)
	if len(s.indents) > 0 {
		prev = s.indents[len(s.indents)-1]
	}
	newlineCount := int16(0)
	indentationSize := int16(0)

	for iswspace(l.Lookahead) {
		if l.Lookahead == '\n' {
			newlineCount++
			indentationSize = 0
		} else {
			indentationSize++
		}
		l.Advance(true)
	}

	// A comma that ends an indented block; its own token so the grammar
	// decides where that is allowed.
	if validSymbols[tokCommaOutdent] && l.Lookahead == ',' && prev != -1 {
		if len(s.indents) > 0 {
			s.indents = s.indents[:len(s.indents)-1]
		}
		l.MarkEnd()
		l.ResultSymbol = tokCommaOutdent
		return true
	}

	// Before advancing the lexer, check if we can double outdent.
	// Lookahead == 0 is also true for a NUL byte, as in C.
	if validSymbols[tokOutdent] &&
		(l.Lookahead == 0 ||
			(prev != -1 &&
				(l.Lookahead == ')' ||
					l.Lookahead == ']' ||
					l.Lookahead == '}')) ||
			(s.lastIndentationSize != -1 &&
				prev != -1 &&
				s.lastIndentationSize < prev)) {
		if len(s.indents) > 0 {
			s.indents = s.indents[:len(s.indents)-1]
		}
		l.ResultSymbol = tokOutdent
		return true
	}
	s.lastIndentationSize = -1

	if validSymbols[tokIndent] &&
		newlineCount > 0 &&
		(len(s.indents) == 0 ||
			indentationSize > s.indents[len(s.indents)-1]) {
		if detectCommentStart(l) {
			return false
		}
		s.indents = append(s.indents, indentationSize)
		l.ResultSymbol = tokIndent
		return true
	}

	// Saves indentationSize and newlineCount for the next calls: multiple
	// outdent or automatic semicolon.
	if validSymbols[tokOutdent] &&
		(l.Lookahead == 0 ||
			(newlineCount > 0 &&
				prev != -1 &&
				indentationSize < prev)) {
		if len(s.indents) > 0 {
			s.indents = s.indents[:len(s.indents)-1]
		}
		l.ResultSymbol = tokOutdent
		l.MarkEnd()
		if detectCommentStart(l) {
			return false
		}
		s.lastIndentationSize = indentationSize
		s.lastNewlineCount = newlineCount
		if l.EOF() {
			s.lastColumn = -1
		} else {
			s.lastColumn = int16(l.GetColumn()) // truncates as (int16_t)
		}
		return true
	}

	// Recover newlineCount from the outdent reset. GetColumn is only
	// called when the first operand is false, as C short-circuits.
	isEOF := l.EOF()
	if (s.lastNewlineCount > 0 &&
		(isEOF && s.lastColumn == -1)) ||
		// (uint32_t)int16_t sign-extends; so does Go.
		(!isEOF && l.GetColumn() == uint32(s.lastColumn)) {
		newlineCount += s.lastNewlineCount
	}
	s.lastNewlineCount = 0

	if validSymbols[tokAutomaticSemicolon] && newlineCount > 0 {
		// No semicolon in the middle of an expression: comments,
		// else/catch clauses and so on leave this branch.

		l.MarkEnd()
		l.ResultSymbol = tokAutomaticSemicolon

		// Probably a multi-line field expression.
		if l.Lookahead == '.' {
			return false
		}

		// Single-line and multi-line comments
		if l.Lookahead == '/' {
			l.Advance(false)
			if l.Lookahead == '/' {
				return false
			}
			if l.Lookahead == '*' {
				l.Advance(false)
				for !l.EOF() {
					if l.Lookahead == '*' {
						l.Advance(false)
						if l.Lookahead == '/' {
							l.Advance(false)
							break
						}
					} else {
						l.Advance(false)
					}
				}
				for iswspace(l.Lookahead) {
					if l.Lookahead == '\n' || l.Lookahead == '\r' {
						return false
					}
					l.Advance(true)
				}
				// Code on the same line after the comment still gets a
				// semicolon before it.
				return true
			}
		}

		if validSymbols[tokElse] {
			return !scanWord(l, "else")
		}

		if validSymbols[tokCatch] {
			if scanWord(l, "catch") {
				return false
			}
		}

		if validSymbols[tokFinally] {
			if scanWord(l, "finally") {
				return false
			}
		}

		if validSymbols[tokExtends] {
			if scanWord(l, "extends") {
				return false
			}
		}

		if validSymbols[tokWith] {
			if scanWord(l, "with") {
				return false
			}
		}

		if validSymbols[tokDerives] {
			if scanWord(l, "derives") {
				return false
			}
		}

		if newlineCount > 1 {
			return true
		}

		// No semicolon before a leading infix operator (symbolic or
		// back-ticked) that has an operand on the same line.
		if isOpChar(l.Lookahead) || l.Lookahead == '`' {
			if isOpChar(l.Lookahead) {
				l.Advance(false)
				for isOpChar(l.Lookahead) {
					l.Advance(false)
				}
				foundSpace := false
				for l.Lookahead == ' ' || l.Lookahead == '\t' {
					l.Advance(false)
					foundSpace = true
				}
				if foundSpace && !iswspace(l.Lookahead) && !l.EOF() {
					return false
				}
			} else if l.Lookahead == '`' {
				l.Advance(false)
				for l.Lookahead != '`' && !l.EOF() {
					l.Advance(false)
				}
				if l.Lookahead == '`' {
					l.Advance(false)
					foundSpace := false
					for l.Lookahead == ' ' || l.Lookahead == '\t' {
						l.Advance(false)
						foundSpace = true
					}
					if foundSpace && !iswspace(l.Lookahead) && !l.EOF() {
						return false
					}
				}
			}
		}

		return true
	}

	for iswspace(l.Lookahead) {
		if l.Lookahead == '\n' {
			newlineCount++
		}
		l.Advance(true)
	}

	if validSymbols[tokSimpleStringStart] && l.Lookahead == '"' {
		l.Advance(false)
		l.MarkEnd()

		if l.Lookahead == '"' {
			l.Advance(false)
			if l.Lookahead == '"' {
				l.Advance(false)
				l.ResultSymbol = tokSimpleMultilineStringStart
				l.MarkEnd()
				return true
			}
		}

		l.ResultSymbol = tokSimpleStringStart
		return true
	}

	// raw" needs two tokens of lookahead, hence the scanner.
	if validSymbols[tokRawStringStart] && l.Lookahead == 'r' {
		l.Advance(false)
		if l.Lookahead == 'a' {
			l.Advance(false)
			if l.Lookahead == 'w' {
				l.Advance(false)
				if l.Lookahead == '"' {
					l.MarkEnd()
					l.ResultSymbol = tokRawStringStart
					return true
				}
			}
		}
	}

	if validSymbols[tokSimpleStringMiddle] {
		return scanStringContent(l, false, stringModeSimple)
	}

	if validSymbols[tokInterpolatedStringMiddle] {
		return scanStringContent(l, false, stringModeInterpolated)
	}

	if validSymbols[tokRawStringMiddle] {
		return scanStringContent(l, false, stringModeRaw)
	}

	if validSymbols[tokRawStringMultilineMiddle] {
		return scanStringContent(l, true, stringModeRaw)
	}

	if validSymbols[tokInterpolatedMultilineStringMiddle] {
		return scanStringContent(l, true, stringModeInterpolated)
	}

	// The simple multiline case has no MIDDLE token and all three kinds
	// share MULTILINE_STRING_END, so this comes after the two checks above.
	if validSymbols[tokMultilineStringEnd] {
		return scanStringContent(l, true, stringModeSimple)
	}

	return false
}
