package php

// Port of tree-sitter-php v0.24.2 common/scanner.h (php/src/scanner.c only
// forwards to it). Statement order and the Advance/MarkEnd/EOF call sequence
// follow C on every path.

import (
	"encoding/binary"
	"slices"

	"github.com/dobbo-ca/gositter"
)

// enum TokenType
const (
	tokAutomaticSemicolon = iota
	tokEncapsedStringChars
	tokEncapsedStringCharsAfterVariable
	tokExecutionStringChars
	tokExecutionStringCharsAfterVariable
	tokEncapsedStringCharsHeredoc
	tokEncapsedStringCharsAfterVariableHeredoc
	tokEOFToken
	tokHeredocStart
	tokHeredocEnd
	tokNowdocString
	tokSentinelError // only valid during error recovery
)

// Heredoc
type heredoc struct {
	endWordIndentationAllowed bool
	word                      []int32
}

// Scanner. has_leading_whitespace is written but never read or serialized.
type scanner struct {
	hasLeadingWhitespace bool
	heredocs             []heredoc
}

// iswspace, iswalnum, iswdigit, iswxdigit in the "C" locale (the scanner never
// calls setlocale): ASCII only, false for the -1 of an invalid byte. Checked
// against the darwin libc for -2..0x2FFFF.

// iswspace
func iswspace(c int32) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

// iswdigit
func iswdigit(c int32) bool { return c >= '0' && c <= '9' }

// iswxdigit
func iswxdigit(c int32) bool {
	return iswdigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// iswalnum
func iswalnum(c int32) bool {
	return iswdigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// serialize
func (s *scanner) Serialize(buf []byte) int {
	size := 0

	// (char) cast: the count wraps at 256. It cannot get that far, 205
	// empty heredocs already hit the size check below.
	buf[size] = byte(len(s.heredocs))
	size++
	for j := range s.heredocs {
		h := &s.heredocs[j]
		wordSize := len(h.word) * 4
		if size+5+wordSize >= gositter.SerializationBufferSize {
			return 0
		}
		buf[size] = 0
		if h.endWordIndentationAllowed {
			buf[size] = 1
		}
		size++
		binary.LittleEndian.PutUint32(buf[size:], uint32(len(h.word)))
		size += 4
		for _, c := range h.word {
			binary.LittleEndian.PutUint32(buf[size:], uint32(c))
			size += 4
		}
	}

	return size
}

// deserialize
func (s *scanner) Deserialize(buf []byte) {
	size := 0
	s.hasLeadingWhitespace = false

	// C empties every heredoc but keeps heredocs.size. The array never
	// shrinks here, so a state with fewer heredocs (or the empty state)
	// leaves the extra ones in place with an empty word. Kept: the runtime
	// compares the serialized bytes, and scan reads heredocs.size.
	for i := range s.heredocs {
		s.heredocs[i] = heredoc{}
	}

	if len(buf) == 0 {
		return
	}

	openHeredocCount := int(buf[size])
	size++
	for i := 0; i < openHeredocCount; i++ {
		if i >= len(s.heredocs) {
			s.heredocs = append(s.heredocs, heredoc{})
		}
		h := &s.heredocs[i]

		h.endWordIndentationAllowed = buf[size] != 0
		size++
		n := int(binary.LittleEndian.Uint32(buf[size:]))
		size += 4
		if n > 0 {
			h.word = make([]int32, n)
			for k := range h.word {
				h.word[k] = int32(binary.LittleEndian.Uint32(buf[size:]))
				size += 4
			}
		}
	}
	// C: assert(size == length). Holds for anything Serialize wrote.
}

// scan_whitespace
func scanWhitespace(l *gositter.Lexer) bool {
	for {
		for iswspace(l.Lookahead) {
			l.Advance(false)
		}

		if l.Lookahead == '/' {
			l.Advance(false)

			if l.Lookahead == '/' {
				l.Advance(false)
				for l.Lookahead != 0 && l.Lookahead != '\n' {
					l.Advance(false)
				}
			} else {
				return false
			}
		} else {
			return true
		}
	}
}

// is_valid_name_char
func isValidNameChar(l *gositter.Lexer) bool {
	return iswalnum(l.Lookahead) || l.Lookahead == '_' || l.Lookahead >= 0x80
}

// is_escapable_sequence
func isEscapableSequence(l *gositter.Lexer) bool {
	letter := l.Lookahead

	if letter == 'n' || letter == 'r' || letter == 't' || letter == 'v' || letter == 'e' || letter == 'f' ||
		letter == '\\' || letter == '$' || letter == '"' {
		return true
	}

	// Hex
	if letter == 'x' {
		l.Advance(false)
		return iswxdigit(l.Lookahead)
	}

	// Unicode. grammar.js sorts out "\u{$a}".
	if letter == 'u' {
		return true
	}

	// Octal
	return iswdigit(l.Lookahead) && l.Lookahead >= '0' && l.Lookahead <= '7'
}

// scan_heredoc_word
func scanHeredocWord(l *gositter.Lexer) []int32 {
	var result []int32

	for isValidNameChar(l) {
		result = append(result, l.Lookahead)
		l.Advance(false)
	}

	return result
}

// The end tag test shared by scan_nowdoc_string and scan_encapsed_part_string.
func isTagTerminator(c int32) bool {
	return iswspace(c) || c == ';' || c == ',' || c == ')'
}

// scan_nowdoc_string
func (s *scanner) scanNowdocString(l *gositter.Lexer) bool {
	hasConsumedContent := false
	if len(s.heredocs) == 0 {
		return false
	}

	// Any whitespace may come before the closing tag.
	for iswspace(l.Lookahead) {
		l.Advance(false)
		hasConsumedContent = true
	}

	endTagMatched := false
	heredocTag := s.heredocs[len(s.heredocs)-1].word

	for i := range heredocTag {
		if l.Lookahead != heredocTag[i] {
			break
		}
		l.Advance(false)
		hasConsumedContent = true

		endTagMatched = i == len(heredocTag)-1 && isTagTerminator(l.Lookahead)
	}

	if endTagMatched {
		for iswspace(l.Lookahead) && l.Lookahead != '\r' && l.Lookahead != '\n' {
			l.Advance(false)
			hasConsumedContent = true
		}

		// Leave the end tag to HEREDOC_END.
		if l.Lookahead == ';' || l.Lookahead == ',' || l.Lookahead == ')' || l.Lookahead == '\n' ||
			l.Lookahead == '\r' {
			return false
		}
	}

	for hasContent := hasConsumedContent; ; hasContent = true {
		l.MarkEnd()

		switch l.Lookahead {
		case '\n', '\r':
			return hasContent
		default:
			if l.EOF() {
				return false
			}
			l.Advance(false)
		}
	}
}

// scan_encapsed_part_string
func (s *scanner) scanEncapsedPartString(l *gositter.Lexer, isAfterVariable, isHeredoc, isExecutionString bool) bool {
	hasConsumedContent := false

	if isHeredoc && len(s.heredocs) > 0 {
		// Whitespace before the closing tag, but not \r or \n.
		for iswspace(l.Lookahead) && l.Lookahead != '\r' && l.Lookahead != '\n' {
			l.Advance(false)
			hasConsumedContent = true
		}

		heredocTag := s.heredocs[len(s.heredocs)-1].word

		endTagMatched := false

		for i := range heredocTag {
			if l.Lookahead != heredocTag[i] {
				break
			}
			hasConsumedContent = true
			l.Advance(false)

			endTagMatched = i == len(heredocTag)-1 && isTagTerminator(l.Lookahead)
		}

		if endTagMatched {
			for iswspace(l.Lookahead) && l.Lookahead != '\r' && l.Lookahead != '\n' {
				l.Advance(false)
				hasConsumedContent = true
			}

			// Leave the end tag to HEREDOC_END.
			if l.Lookahead == ';' || l.Lookahead == ',' || l.Lookahead == ')' ||
				l.Lookahead == '\n' || l.Lookahead == '\r' {
				return false
			}
		}
	}

	for hasContent := hasConsumedContent; ; hasContent = true {
		l.MarkEnd()

		switch l.Lookahead {
		case '"':
			if !isHeredoc && !isExecutionString {
				return hasContent
			}
			l.Advance(false)
		case '`':
			if isExecutionString {
				return hasContent
			}
			l.Advance(false)
		case '\n', '\r':
			if isHeredoc {
				return hasContent
			}
			l.Advance(false)
		case '\\':
			l.Advance(false)

			// \{ is not an escape; both are plain characters.
			if l.Lookahead == '{' {
				l.Advance(false)
				break
			}

			if isExecutionString && l.Lookahead == '`' {
				return hasContent
			}

			if isHeredoc && l.Lookahead == '\\' {
				l.Advance(false)
				break
			}

			if isEscapableSequence(l) {
				return hasContent
			}
		case '$':
			l.Advance(false)

			if (isValidNameChar(l) && !iswdigit(l.Lookahead)) || l.Lookahead == '{' {
				return hasContent
			}
		case '-', '[':
			if l.Lookahead == '-' && isAfterVariable {
				l.Advance(false)
				if l.Lookahead == '>' {
					l.Advance(false)
					if isValidNameChar(l) {
						return hasContent
					}
				}
				break
			}
			// C falls through from '-' into '[' when not after a variable.
			if isAfterVariable {
				return hasContent
			}
			l.Advance(false)
		case '{':
			l.Advance(false)
			if l.Lookahead == '$' {
				return hasContent
			}
		default:
			if l.EOF() {
				return false
			}
			l.Advance(false)
		}

		isAfterVariable = false
	}
}

// scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	isErrorRecovery := validSymbols[tokSentinelError]

	if isErrorRecovery {
		return false
	}

	s.hasLeadingWhitespace = false

	l.MarkEnd()

	if validSymbols[tokEncapsedStringCharsAfterVariable] {
		l.ResultSymbol = tokEncapsedStringCharsAfterVariable
		return s.scanEncapsedPartString(l, true, false, false)
	}

	if validSymbols[tokEncapsedStringChars] {
		l.ResultSymbol = tokEncapsedStringChars
		return s.scanEncapsedPartString(l, false, false, false)
	}

	if validSymbols[tokExecutionStringCharsAfterVariable] {
		l.ResultSymbol = tokExecutionStringCharsAfterVariable
		return s.scanEncapsedPartString(l, true, false, true)
	}

	if validSymbols[tokExecutionStringChars] {
		l.ResultSymbol = tokExecutionStringChars
		return s.scanEncapsedPartString(l, false, false, true)
	}

	if validSymbols[tokEncapsedStringCharsAfterVariableHeredoc] {
		l.ResultSymbol = tokEncapsedStringCharsAfterVariableHeredoc
		return s.scanEncapsedPartString(l, true, true, false)
	}

	if validSymbols[tokEncapsedStringCharsHeredoc] {
		l.ResultSymbol = tokEncapsedStringCharsHeredoc
		return s.scanEncapsedPartString(l, false, true, false)
	}

	if validSymbols[tokNowdocString] {
		l.ResultSymbol = tokNowdocString
		return s.scanNowdocString(l)
	}

	if validSymbols[tokHeredocEnd] {
		l.ResultSymbol = tokHeredocEnd
		if len(s.heredocs) == 0 {
			return false
		}

		want := s.heredocs[len(s.heredocs)-1].word

		for iswspace(l.Lookahead) {
			l.Advance(true)
		}

		// string_eq. An emptied heredoc (see Deserialize) matches an
		// empty word, as in C.
		if !slices.Equal(scanHeredocWord(l), want) {
			return false
		}

		l.MarkEnd()
		s.heredocs = s.heredocs[:len(s.heredocs)-1]
		return true
	}

	if !scanWhitespace(l) {
		return false
	}

	if validSymbols[tokEOFToken] && l.EOF() {
		l.ResultSymbol = tokEOFToken
		return true
	}

	if validSymbols[tokHeredocStart] {
		l.ResultSymbol = tokHeredocStart

		for iswspace(l.Lookahead) {
			l.Advance(true)
		}

		word := scanHeredocWord(l)
		if len(word) == 0 {
			return false
		}
		l.MarkEnd()

		s.heredocs = append(s.heredocs, heredoc{word: word})
		return true
	}

	if validSymbols[tokAutomaticSemicolon] {
		l.ResultSymbol = tokAutomaticSemicolon

		if l.Lookahead != '?' {
			return false
		}

		l.Advance(false)

		return l.Lookahead == '>'
	}

	return false
}

// tree_sitter_php_external_scanner_create
func newScanner() gositter.ExternalScanner { return &scanner{} }
