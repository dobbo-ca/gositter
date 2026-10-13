package bash

// Port of tree-sitter-bash v0.25.1 src/scanner.c. Statement order and the
// Advance/MarkEnd/GetColumn/EOF call sequence follow C on every path.

import (
	"encoding/binary"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/internal/cchar"
)

// enum TokenType, in the order of the grammar's externals.
const (
	tokHeredocStart = iota
	tokSimpleHeredocBody
	tokHeredocBodyBeginning
	tokHeredocContent
	tokHeredocEnd
	tokFileDescriptor
	tokEmptyValue
	tokConcat
	tokVariableName
	tokTestOperator
	tokRegex
	tokRegexNoSlash
	tokRegexNoSpace
	tokExpansionWord
	tokExtglobPattern
	tokBareDollar
	tokBraceStart
	tokImmediateDoubleHash
	tokExternalExpansionSymHash
	tokExternalExpansionSymBang
	tokExternalExpansionSymEqual
	tokClosingBrace
	tokClosingBracket
	tokHeredocArrow
	tokHeredocArrowDash
	tokNewline
	tokOpeningParen
	tokEsac
	tokErrorRecovery
)

// The isw* calls of C run in the "C" locale (nothing calls setlocale), where
// macOS libc classifies ASCII only. -1 (invalid UTF-8) and 0 are in no class.

// iswspace
func iswspace(c int32) bool { return c == ' ' || ('\t' <= c && c <= '\r') }

// iswalpha
func iswalpha(c int32) bool { return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') }

// iswdigit, isdigit
func iswdigit(c int32) bool { return '0' <= c && c <= '9' }

// iswalnum
func iswalnum(c int32) bool { return iswalpha(c) || iswdigit(c) }

// Heredoc. The two strings are C `Array(char)`: lookahead is cut to its low
// byte on push and read back as a char (cchar.Value).
type heredoc struct {
	isRaw              bool
	started            bool
	allowsIndent       bool
	delimiter          []byte
	currentLeadingWord []byte
}

// Scanner
type scanner struct {
	lastGlobParenDepth  uint8
	extWasInDoubleQuote bool
	extSawOutsideQuote  bool
	heredocs            []heredoc
}

// tree_sitter_bash_external_scanner_create
func newScanner() gositter.ExternalScanner { return &scanner{} }

// heredoc_new + array_push. A popped slot's buffers are reused.
func (s *scanner) pushHeredoc(allowsIndent bool) {
	n := len(s.heredocs)
	if n < cap(s.heredocs) {
		s.heredocs = s.heredocs[:n+1]
	} else {
		s.heredocs = append(s.heredocs, heredoc{})
	}
	h := &s.heredocs[n]
	h.isRaw = false
	h.started = false
	h.allowsIndent = allowsIndent
	h.delimiter = h.delimiter[:0]
	h.currentLeadingWord = h.currentLeadingWord[:0]
}

// reset_heredoc
func (h *heredoc) reset() {
	h.isRaw = false
	h.started = false
	h.allowsIndent = false
	h.delimiter = h.delimiter[:0] // reset_string
}

func b2c(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// serialize
func (s *scanner) Serialize(buf []byte) int {
	size := 0

	buf[size] = s.lastGlobParenDepth
	buf[size+1] = b2c(s.extWasInDoubleQuote)
	buf[size+2] = b2c(s.extSawOutsideQuote)
	buf[size+3] = byte(len(s.heredocs)) // (char) of the count, wraps at 256
	size += 4

	for i := range s.heredocs {
		h := &s.heredocs[i]
		if size+3+4+len(h.delimiter) >= gositter.SerializationBufferSize {
			return 0
		}

		buf[size] = b2c(h.isRaw)
		buf[size+1] = b2c(h.started)
		buf[size+2] = b2c(h.allowsIndent)
		size += 3

		binary.LittleEndian.PutUint32(buf[size:], uint32(len(h.delimiter)))
		size += 4
		size += copy(buf[size:], h.delimiter)
	}
	return size
}

// deserialize
func (s *scanner) Deserialize(buf []byte) {
	if len(buf) == 0 {
		// reset: C clears each heredoc and keeps the count.
		for i := range s.heredocs {
			s.heredocs[i].reset()
		}
		return
	}
	size := 0
	s.lastGlobParenDepth = buf[size]
	s.extWasInDoubleQuote = buf[size+1] != 0
	s.extSawOutsideQuote = buf[size+2] != 0
	heredocCount := int(buf[size+3])
	size += 4
	// C never shrinks heredocs here: slots past heredocCount stay as they are.
	for i := 0; i < heredocCount; i++ {
		if i >= len(s.heredocs) {
			s.pushHeredoc(false)
		}
		h := &s.heredocs[i]

		h.isRaw = buf[size] != 0
		h.started = buf[size+1] != 0
		h.allowsIndent = buf[size+2] != 0
		size += 3

		n := int(binary.LittleEndian.Uint32(buf[size:]))
		size += 4
		h.delimiter = append(h.delimiter[:0], buf[size:size+n]...)
		size += n
	}
}

// advance_word
func advanceWord(l *gositter.Lexer, unquotedWord *[]byte) bool {
	empty := true

	quote := int32(0)
	if l.Lookahead == '\'' || l.Lookahead == '"' {
		quote = l.Lookahead
		l.Advance(false)
	}

	for l.Lookahead != 0 {
		if quote != 0 {
			if l.Lookahead == quote || l.Lookahead == '\r' || l.Lookahead == '\n' {
				break
			}
		} else if iswspace(l.Lookahead) {
			break
		}
		if l.Lookahead == '\\' {
			l.Advance(false)
			if l.Lookahead == 0 {
				return false
			}
		}
		empty = false
		*unquotedWord = append(*unquotedWord, byte(l.Lookahead))
		l.Advance(false)
	}
	*unquotedWord = append(*unquotedWord, 0)

	if quote != 0 && l.Lookahead == quote {
		l.Advance(false)
	}

	return !empty
}

// scan_bare_dollar
func scanBareDollar(l *gositter.Lexer) bool {
	for iswspace(l.Lookahead) && l.Lookahead != '\n' && !l.EOF() {
		l.Advance(true)
	}

	if l.Lookahead == '$' {
		l.Advance(false)
		l.ResultSymbol = tokBareDollar
		l.MarkEnd()
		return iswspace(l.Lookahead) || l.EOF() || l.Lookahead == '"'
	}

	return false
}

// scan_heredoc_start
func scanHeredocStart(h *heredoc, l *gositter.Lexer) bool {
	for iswspace(l.Lookahead) {
		l.Advance(true)
	}

	l.ResultSymbol = tokHeredocStart
	h.isRaw = l.Lookahead == '\'' || l.Lookahead == '"' || l.Lookahead == '\\'

	// Appends to whatever the delimiter already holds, as C does.
	foundDelimiter := advanceWord(l, &h.delimiter)
	if !foundDelimiter {
		h.delimiter = h.delimiter[:0]
		return false
	}
	return foundDelimiter
}

// strcmp(a, b) == 0
func cstrEqual(a, b []byte) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return false
		}
		if a[i] == 0 {
			return true
		}
	}
	return false
}

// scan_heredoc_end_identifier
func scanHeredocEndIdentifier(h *heredoc, l *gositter.Lexer) bool {
	h.currentLeadingWord = h.currentLeadingWord[:0]
	// Scan the first 'n' characters on this line, to see if they match the
	// heredoc delimiter
	size := 0
	if len(h.delimiter) > 0 {
		// A non-empty delimiter ends in NUL and lookahead is not 0 here, so
		// size stays in bounds. Where char is signed, 0xFF equals the -1 of
		// an invalid UTF-8 byte and 0x80..0xFE equal nothing; where it is
		// unsigned (linux/arm64), 0x80..0xFF equal U+0080..U+00FF.
		for l.Lookahead != 0 && l.Lookahead != '\n' &&
			size < len(h.delimiter) && cchar.Value(h.delimiter[size]) == l.Lookahead &&
			len(h.currentLeadingWord) < len(h.delimiter) {
			h.currentLeadingWord = append(h.currentLeadingWord, byte(l.Lookahead))
			l.Advance(false)
			size++
		}
	}
	h.currentLeadingWord = append(h.currentLeadingWord, 0)
	if len(h.delimiter) == 0 {
		return false
	}
	return cstrEqual(h.currentLeadingWord, h.delimiter)
}

// scan_heredoc_content
func (s *scanner) scanHeredocContent(l *gositter.Lexer, middleType, endType gositter.Symbol) bool {
	didAdvance := false
	h := &s.heredocs[len(s.heredocs)-1]

	for {
		switch l.Lookahead {
		case 0:
			if l.EOF() && didAdvance {
				h.reset()
				l.ResultSymbol = endType
				return true
			}
			return false

		case '\\':
			didAdvance = true
			l.Advance(false)
			l.Advance(false)

		case '$':
			if h.isRaw {
				didAdvance = true
				l.Advance(false)
				break
			}
			if didAdvance {
				l.MarkEnd()
				l.ResultSymbol = middleType
				h.started = true
				l.Advance(false)
				if iswalpha(l.Lookahead) || l.Lookahead == '{' || l.Lookahead == '(' {
					return true
				}
				break
			}
			if middleType == tokHeredocBodyBeginning && l.GetColumn() == 0 {
				l.ResultSymbol = middleType
				h.started = true
				return true
			}
			return false

		case '\n':
			if !didAdvance {
				l.Advance(true)
			} else {
				l.Advance(false)
			}
			didAdvance = true
			if h.allowsIndent {
				for iswspace(l.Lookahead) {
					l.Advance(false)
				}
			}
			if h.started {
				l.ResultSymbol = middleType
			} else {
				l.ResultSymbol = endType
			}
			l.MarkEnd()
			if scanHeredocEndIdentifier(h, l) {
				if l.ResultSymbol == tokHeredocEnd {
					s.heredocs = s.heredocs[:len(s.heredocs)-1]
				}
				return true
			}

		default:
			if l.GetColumn() == 0 {
				// an alternative is to check the starting column of the
				// heredoc body and track that statefully
				for iswspace(l.Lookahead) {
					if didAdvance {
						l.Advance(false)
					} else {
						l.Advance(true)
					}
				}
				if endType != tokSimpleHeredocBody {
					l.ResultSymbol = middleType
					if scanHeredocEndIdentifier(h, l) {
						return true
					}
				}
				if endType == tokSimpleHeredocBody {
					l.ResultSymbol = endType
					l.MarkEnd()
					if scanHeredocEndIdentifier(h, l) {
						return true
					}
				}
			}
			didAdvance = true
			l.Advance(false)
		}
	}
}

// scan, tree_sitter_bash_external_scanner_scan. No variable is declared at
// function level: the gotos jump forward to labels there.
func (s *scanner) Scan(l *gositter.Lexer, v []bool) bool {
	if v[tokConcat] && !v[tokErrorRecovery] {
		if !(l.Lookahead == 0 || iswspace(l.Lookahead) || l.Lookahead == '>' ||
			l.Lookahead == '<' || l.Lookahead == ')' || l.Lookahead == '(' ||
			l.Lookahead == ';' || l.Lookahead == '&' || l.Lookahead == '|' ||
			(l.Lookahead == '}' && v[tokClosingBrace]) ||
			(l.Lookahead == ']' && v[tokClosingBracket])) {
			l.ResultSymbol = tokConcat
			// So for a`b`, we want to return a concat. We check if the
			// 2nd backtick has whitespace after it, and if it does we
			// return concat.
			if l.Lookahead == '`' {
				l.MarkEnd()
				l.Advance(false)
				for l.Lookahead != '`' && !l.EOF() {
					l.Advance(false)
				}
				if l.EOF() {
					return false
				}
				if l.Lookahead == '`' {
					l.Advance(false)
				}
				return iswspace(l.Lookahead) || l.EOF()
			}
			// strings w/ expansions that contains escaped quotes or
			// backslashes need this to return a concat
			if l.Lookahead == '\\' {
				l.MarkEnd()
				l.Advance(false)
				if l.Lookahead == '"' || l.Lookahead == '\'' || l.Lookahead == '\\' {
					return true
				}
				if l.EOF() {
					return false
				}
			} else {
				return true
			}
		}
		if iswspace(l.Lookahead) && v[tokClosingBrace] && !v[tokExpansionWord] {
			l.ResultSymbol = tokConcat
			return true
		}
	}

	if v[tokImmediateDoubleHash] && !v[tokErrorRecovery] {
		// advance two # and ensure not } after
		if l.Lookahead == '#' {
			l.MarkEnd()
			l.Advance(false)
			if l.Lookahead == '#' {
				l.Advance(false)
				if l.Lookahead != '}' {
					l.ResultSymbol = tokImmediateDoubleHash
					l.MarkEnd()
					return true
				}
			}
		}
	}

	if v[tokExternalExpansionSymHash] && !v[tokErrorRecovery] {
		if l.Lookahead == '#' || l.Lookahead == '=' || l.Lookahead == '!' {
			if l.Lookahead == '#' {
				l.ResultSymbol = tokExternalExpansionSymHash
			} else if l.Lookahead == '!' {
				l.ResultSymbol = tokExternalExpansionSymBang
			} else {
				l.ResultSymbol = tokExternalExpansionSymEqual
			}
			l.Advance(false)
			l.MarkEnd()
			for l.Lookahead == '#' || l.Lookahead == '=' || l.Lookahead == '!' {
				l.Advance(false)
			}
			for iswspace(l.Lookahead) {
				l.Advance(true)
			}
			if l.Lookahead == '}' {
				return true
			}
			return false
		}
	}

	if v[tokEmptyValue] {
		if iswspace(l.Lookahead) || l.EOF() || l.Lookahead == ';' || l.Lookahead == '&' {
			l.ResultSymbol = tokEmptyValue
			return true
		}
	}

	if (v[tokHeredocBodyBeginning] || v[tokSimpleHeredocBody]) && len(s.heredocs) > 0 &&
		!s.heredocs[len(s.heredocs)-1].started && !v[tokErrorRecovery] {
		return s.scanHeredocContent(l, tokHeredocBodyBeginning, tokSimpleHeredocBody)
	}

	if v[tokHeredocEnd] && len(s.heredocs) > 0 {
		h := &s.heredocs[len(s.heredocs)-1]
		if scanHeredocEndIdentifier(h, l) {
			s.heredocs = s.heredocs[:len(s.heredocs)-1]
			l.ResultSymbol = tokHeredocEnd
			return true
		}
	}

	if v[tokHeredocContent] && len(s.heredocs) > 0 && s.heredocs[len(s.heredocs)-1].started &&
		!v[tokErrorRecovery] {
		return s.scanHeredocContent(l, tokHeredocContent, tokHeredocEnd)
	}

	if v[tokHeredocStart] && !v[tokErrorRecovery] && len(s.heredocs) > 0 {
		return scanHeredocStart(&s.heredocs[len(s.heredocs)-1], l)
	}

	if v[tokTestOperator] && !v[tokExpansionWord] {
		for iswspace(l.Lookahead) && l.Lookahead != '\n' {
			l.Advance(true)
		}

		if l.Lookahead == '\\' {
			if v[tokExtglobPattern] {
				goto extglob_pattern
			}
			if v[tokRegexNoSpace] {
				goto regex
			}
			l.Advance(true)

			if l.EOF() {
				return false
			}

			if l.Lookahead == '\r' {
				l.Advance(true)
				if l.Lookahead == '\n' {
					l.Advance(true)
				}
			} else if l.Lookahead == '\n' {
				l.Advance(true)
			} else {
				return false
			}

			for iswspace(l.Lookahead) {
				l.Advance(true)
			}
		}

		if l.Lookahead == '\n' && !v[tokNewline] {
			l.Advance(true)

			for iswspace(l.Lookahead) {
				l.Advance(true)
			}
		}

		if l.Lookahead == '-' {
			l.Advance(false)

			advancedOnce := false
			for iswalpha(l.Lookahead) {
				advancedOnce = true
				l.Advance(false)
			}

			if iswspace(l.Lookahead) && advancedOnce {
				l.MarkEnd()
				l.Advance(false)
				if l.Lookahead == '}' && v[tokClosingBrace] {
					if v[tokExpansionWord] {
						l.MarkEnd()
						l.ResultSymbol = tokExpansionWord
						return true
					}
					return false
				}
				l.ResultSymbol = tokTestOperator
				return true
			}
			if iswspace(l.Lookahead) && v[tokExtglobPattern] {
				l.ResultSymbol = tokExtglobPattern
				return true
			}
		}

		if v[tokBareDollar] && !v[tokErrorRecovery] && scanBareDollar(l) {
			return true
		}
	}

	if (v[tokVariableName] || v[tokFileDescriptor] || v[tokHeredocArrow]) &&
		!v[tokRegexNoSlash] && !v[tokErrorRecovery] {
		for {
			if (l.Lookahead == ' ' || l.Lookahead == '\t' || l.Lookahead == '\r' ||
				(l.Lookahead == '\n' && !v[tokNewline])) &&
				!v[tokExpansionWord] {
				l.Advance(true)
			} else if l.Lookahead == '\\' {
				l.Advance(true)

				if l.EOF() {
					l.MarkEnd()
					l.ResultSymbol = tokVariableName
					return true
				}

				if l.Lookahead == '\r' {
					l.Advance(true)
				}
				if l.Lookahead == '\n' {
					l.Advance(true)
				} else {
					if l.Lookahead == '\\' && v[tokExpansionWord] {
						goto expansion_word
					}
					return false
				}
			} else {
				break
			}
		}

		// no '*', '@', '?', '-', '$', '0', '_'
		if !v[tokExpansionWord] &&
			(l.Lookahead == '*' || l.Lookahead == '@' || l.Lookahead == '?' || l.Lookahead == '-' ||
				l.Lookahead == '0' || l.Lookahead == '_') {
			l.MarkEnd()
			l.Advance(false)
			if l.Lookahead == '=' || l.Lookahead == '[' || l.Lookahead == ':' ||
				l.Lookahead == '-' || l.Lookahead == '%' || l.Lookahead == '#' ||
				l.Lookahead == '/' {
				return false
			}
			if v[tokExtglobPattern] && iswspace(l.Lookahead) {
				l.MarkEnd()
				l.ResultSymbol = tokExtglobPattern
				return true
			}
		}

		if v[tokHeredocArrow] && l.Lookahead == '<' {
			l.Advance(false)
			if l.Lookahead == '<' {
				l.Advance(false)
				if l.Lookahead == '-' {
					l.Advance(false)
					s.pushHeredoc(true)
					l.ResultSymbol = tokHeredocArrowDash
				} else if l.Lookahead == '<' || l.Lookahead == '=' {
					return false
				} else {
					s.pushHeredoc(false)
					l.ResultSymbol = tokHeredocArrow
				}
				return true
			}
			return false
		}

		{
			isNumber := true
			if iswdigit(l.Lookahead) {
				l.Advance(false)
			} else if iswalpha(l.Lookahead) || l.Lookahead == '_' {
				isNumber = false
				l.Advance(false)
			} else {
				if l.Lookahead == '{' {
					goto brace_start
				}
				if v[tokExpansionWord] {
					goto expansion_word
				}
				if v[tokExtglobPattern] {
					goto extglob_pattern
				}
				return false
			}

			for {
				if iswdigit(l.Lookahead) {
					l.Advance(false)
				} else if iswalpha(l.Lookahead) || l.Lookahead == '_' {
					isNumber = false
					l.Advance(false)
				} else {
					break
				}
			}

			if isNumber && v[tokFileDescriptor] && (l.Lookahead == '>' || l.Lookahead == '<') {
				l.ResultSymbol = tokFileDescriptor
				return true
			}

			if v[tokVariableName] {
				if l.Lookahead == '+' {
					l.MarkEnd()
					l.Advance(false)
					if l.Lookahead == '=' || l.Lookahead == ':' || v[tokClosingBrace] {
						l.ResultSymbol = tokVariableName
						return true
					}
					return false
				}
				if l.Lookahead == '/' {
					return false
				}
				if l.Lookahead == '=' || l.Lookahead == '[' ||
					(l.Lookahead == ':' && !v[tokClosingBrace] && !v[tokOpeningParen]) ||
					l.Lookahead == '%' ||
					(l.Lookahead == '#' && !isNumber) || l.Lookahead == '@' ||
					(l.Lookahead == '-' && v[tokClosingBrace]) {
					l.MarkEnd()
					l.ResultSymbol = tokVariableName
					return true
				}

				if l.Lookahead == '?' {
					l.MarkEnd()
					l.Advance(false)
					l.ResultSymbol = tokVariableName
					return iswalpha(l.Lookahead)
				}
			}

			return false
		}
	}

	if v[tokBareDollar] && !v[tokErrorRecovery] && scanBareDollar(l) {
		return true
	}

regex:
	if (v[tokRegex] || v[tokRegexNoSlash] || v[tokRegexNoSpace]) && !v[tokErrorRecovery] {
		if v[tokRegex] || v[tokRegexNoSpace] {
			for iswspace(l.Lookahead) {
				l.Advance(true)
			}
		}

		if (l.Lookahead != '"' && l.Lookahead != '\'') ||
			((l.Lookahead == '$' || l.Lookahead == '\'') && v[tokRegexNoSlash]) ||
			(l.Lookahead == '\'' && v[tokRegexNoSpace]) {
			if l.Lookahead == '$' && v[tokRegexNoSlash] {
				l.MarkEnd()
				l.Advance(false)
				if l.Lookahead == '(' {
					return false
				}
			}

			l.MarkEnd()

			// State. The depths are uint32 and wrap below zero as in C.
			var done, advancedOnce, foundNonAlnumdollarunderdash, lastWasEscape, inSingleQuote bool
			var parenDepth, bracketDepth, braceDepth uint32
			for !done {
				if inSingleQuote {
					if l.Lookahead == '\'' {
						inSingleQuote = false
						l.Advance(false)
						l.MarkEnd()
					}
				}
				switch l.Lookahead {
				case '\\':
					lastWasEscape = true
				case 0:
					return false
				case '(':
					parenDepth++
					lastWasEscape = false
				case '[':
					bracketDepth++
					lastWasEscape = false
				case '{':
					if !lastWasEscape {
						braceDepth++
					}
					lastWasEscape = false
				case ')':
					if parenDepth == 0 {
						done = true
					}
					parenDepth--
					lastWasEscape = false
				case ']':
					if bracketDepth == 0 {
						done = true
					}
					bracketDepth--
					lastWasEscape = false
				case '}':
					if braceDepth == 0 {
						done = true
					}
					braceDepth--
					lastWasEscape = false
				case '\'':
					// Enter or exit a single-quoted string.
					inSingleQuote = !inSingleQuote
					l.Advance(false)
					advancedOnce = true
					lastWasEscape = false
					continue
				default:
					lastWasEscape = false
				}

				if !done {
					if v[tokRegex] {
						wasSpace := !inSingleQuote && iswspace(l.Lookahead)
						l.Advance(false)
						advancedOnce = true
						if !wasSpace || parenDepth > 0 {
							l.MarkEnd()
						}
					} else if v[tokRegexNoSlash] {
						if l.Lookahead == '/' {
							l.MarkEnd()
							l.ResultSymbol = tokRegexNoSlash
							return advancedOnce
						}
						if l.Lookahead == '\\' {
							l.Advance(false)
							advancedOnce = true
							if !l.EOF() && l.Lookahead != '[' && l.Lookahead != '/' {
								l.Advance(false)
								l.MarkEnd()
							}
						} else {
							wasSpace := !inSingleQuote && iswspace(l.Lookahead)
							l.Advance(false)
							advancedOnce = true
							if !wasSpace {
								l.MarkEnd()
							}
						}
					} else if v[tokRegexNoSpace] {
						if l.Lookahead == '\\' {
							foundNonAlnumdollarunderdash = true
							l.Advance(false)
							if !l.EOF() {
								l.Advance(false)
							}
						} else if l.Lookahead == '$' {
							l.MarkEnd()
							l.Advance(false)
							// do not parse a command
							// substitution
							if l.Lookahead == '(' {
								return false
							}
							// end $ always means regex, e.g.
							// 99999999$
							if iswspace(l.Lookahead) {
								l.ResultSymbol = tokRegexNoSpace
								l.MarkEnd()
								return true
							}
						} else {
							wasSpace := !inSingleQuote && iswspace(l.Lookahead)
							if wasSpace && parenDepth == 0 {
								l.MarkEnd()
								l.ResultSymbol = tokRegexNoSpace
								return foundNonAlnumdollarunderdash
							}
							if !iswalnum(l.Lookahead) && l.Lookahead != '$' && l.Lookahead != '-' &&
								l.Lookahead != '_' {
								foundNonAlnumdollarunderdash = true
							}
							l.Advance(false)
						}
					}
				}
			}

			if v[tokRegexNoSlash] {
				l.ResultSymbol = tokRegexNoSlash
			} else if v[tokRegexNoSpace] {
				l.ResultSymbol = tokRegexNoSpace
			} else {
				l.ResultSymbol = tokRegex
			}
			if v[tokRegex] && !advancedOnce {
				return false
			}
			return true
		}
	}

extglob_pattern:
	if v[tokExtglobPattern] && !v[tokErrorRecovery] {
		// first skip ws, then check for ? * + @ !
		for iswspace(l.Lookahead) {
			l.Advance(true)
		}

		if l.Lookahead == '?' || l.Lookahead == '*' || l.Lookahead == '+' || l.Lookahead == '@' ||
			l.Lookahead == '!' || l.Lookahead == '-' || l.Lookahead == ')' || l.Lookahead == '\\' ||
			l.Lookahead == '.' || l.Lookahead == '[' || iswalpha(l.Lookahead) {
			if l.Lookahead == '\\' {
				l.Advance(false)
				if (iswspace(l.Lookahead) || l.Lookahead == '"') && l.Lookahead != '\r' &&
					l.Lookahead != '\n' {
					l.Advance(false)
				} else {
					return false
				}
			}

			if l.Lookahead == ')' && s.lastGlobParenDepth == 0 {
				l.MarkEnd()
				l.Advance(false)

				if iswspace(l.Lookahead) {
					return false
				}
			}

			l.MarkEnd()
			wasNonAlpha := !iswalpha(l.Lookahead)
			if l.Lookahead != '[' {
				// no esac
				if l.Lookahead == 'e' {
					l.MarkEnd()
					l.Advance(false)
					if l.Lookahead == 's' {
						l.Advance(false)
						if l.Lookahead == 'a' {
							l.Advance(false)
							if l.Lookahead == 'c' {
								l.Advance(false)
								if iswspace(l.Lookahead) {
									return false
								}
							}
						}
					}
				} else {
					l.Advance(false)
				}
			}

			// -\w is just a word, find something else special
			if l.Lookahead == '-' {
				l.MarkEnd()
				l.Advance(false)
				for iswalnum(l.Lookahead) {
					l.Advance(false)
				}

				if l.Lookahead == ')' || l.Lookahead == '\\' || l.Lookahead == '.' {
					return false
				}
				l.MarkEnd()
			}

			// case item -) or *)
			if l.Lookahead == ')' && s.lastGlobParenDepth == 0 {
				l.MarkEnd()
				l.Advance(false)
				if iswspace(l.Lookahead) {
					l.ResultSymbol = tokExtglobPattern
					return wasNonAlpha
				}
			}

			if iswspace(l.Lookahead) {
				l.MarkEnd()
				l.ResultSymbol = tokExtglobPattern
				s.lastGlobParenDepth = 0
				return true
			}

			if l.Lookahead == '$' {
				l.MarkEnd()
				l.Advance(false)
				if l.Lookahead == '{' || l.Lookahead == '(' {
					l.ResultSymbol = tokExtglobPattern
					return true
				}
			}

			if l.Lookahead == '|' {
				l.MarkEnd()
				l.Advance(false)
				l.ResultSymbol = tokExtglobPattern
				return true
			}

			if !iswalnum(l.Lookahead) && l.Lookahead != '(' && l.Lookahead != '"' &&
				l.Lookahead != '[' && l.Lookahead != '?' && l.Lookahead != '/' &&
				l.Lookahead != '\\' && l.Lookahead != '_' && l.Lookahead != '*' {
				return false
			}

			// State. The depths are uint32 and wrap below zero as in C.
			done := false
			sawNonAlphadot := wasNonAlpha
			parenDepth := uint32(s.lastGlobParenDepth)
			bracketDepth := uint32(0)
			braceDepth := uint32(0)
			for !done {
				switch l.Lookahead {
				case 0:
					return false
				case '(':
					parenDepth++
				case '[':
					bracketDepth++
				case '{':
					braceDepth++
				case ')':
					if parenDepth == 0 {
						done = true
					}
					parenDepth--
				case ']':
					if bracketDepth == 0 {
						done = true
					}
					bracketDepth--
				case '}':
					if braceDepth == 0 {
						done = true
					}
					braceDepth--
				}

				if l.Lookahead == '|' {
					l.MarkEnd()
					l.Advance(false)
					if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
						l.ResultSymbol = tokExtglobPattern
						return true
					}
				}

				if !done {
					wasSpace := iswspace(l.Lookahead)
					if l.Lookahead == '$' {
						l.MarkEnd()
						if !iswalpha(l.Lookahead) && l.Lookahead != '.' && l.Lookahead != '\\' {
							sawNonAlphadot = true
						}
						l.Advance(false)
						if l.Lookahead == '(' || l.Lookahead == '{' {
							l.ResultSymbol = tokExtglobPattern
							s.lastGlobParenDepth = uint8(parenDepth) // uint32 cut to uint8
							return sawNonAlphadot
						}
					}
					if wasSpace {
						l.MarkEnd()
						l.ResultSymbol = tokExtglobPattern
						s.lastGlobParenDepth = 0
						return sawNonAlphadot
					}
					if l.Lookahead == '"' {
						l.MarkEnd()
						l.ResultSymbol = tokExtglobPattern
						s.lastGlobParenDepth = 0
						return sawNonAlphadot
					}
					if l.Lookahead == '\\' {
						if !iswalpha(l.Lookahead) && l.Lookahead != '.' && l.Lookahead != '\\' {
							sawNonAlphadot = true
						}
						l.Advance(false)
						if iswspace(l.Lookahead) || l.Lookahead == '"' {
							l.Advance(false)
						}
					} else {
						if !iswalpha(l.Lookahead) && l.Lookahead != '.' && l.Lookahead != '\\' {
							sawNonAlphadot = true
						}
						l.Advance(false)
					}
					if !wasSpace {
						l.MarkEnd()
					}
				}
			}

			l.ResultSymbol = tokExtglobPattern
			s.lastGlobParenDepth = 0
			return sawNonAlphadot
		}
		s.lastGlobParenDepth = 0

		return false
	}

expansion_word:
	if v[tokExpansionWord] {
		advancedOnce := false
		advanceOnceSpace := false
		for {
			if l.Lookahead == '"' {
				return false
			}
			if l.Lookahead == '$' {
				l.MarkEnd()
				l.Advance(false)
				if l.Lookahead == '{' || l.Lookahead == '(' || l.Lookahead == '\'' ||
					iswalnum(l.Lookahead) {
					l.ResultSymbol = tokExpansionWord
					return advancedOnce
				}
				advancedOnce = true
			}

			if l.Lookahead == '}' {
				l.MarkEnd()
				l.ResultSymbol = tokExpansionWord
				return advancedOnce || advanceOnceSpace
			}

			if l.Lookahead == '(' && !(advancedOnce || advanceOnceSpace) {
				l.MarkEnd()
				l.Advance(false)
				for l.Lookahead != ')' && !l.EOF() {
					// if we find a $( or ${ assume this is valid and is
					// a garbage concatenation of some weird word + an
					// expansion
					if l.Lookahead == '$' {
						l.MarkEnd()
						l.Advance(false)
						if l.Lookahead == '{' || l.Lookahead == '(' || l.Lookahead == '\'' ||
							iswalnum(l.Lookahead) {
							l.ResultSymbol = tokExpansionWord
							return advancedOnce
						}
						advancedOnce = true
					} else {
						advancedOnce = advancedOnce || !iswspace(l.Lookahead)
						advanceOnceSpace = advanceOnceSpace || iswspace(l.Lookahead)
						l.Advance(false)
					}
				}
				l.MarkEnd()
				if l.Lookahead == ')' {
					advancedOnce = true
					l.Advance(false)
					l.MarkEnd()
					if l.Lookahead == '}' {
						return false
					}
				} else {
					return false
				}
			}

			if l.Lookahead == '\'' {
				return false
			}

			if l.EOF() {
				return false
			}
			advancedOnce = advancedOnce || !iswspace(l.Lookahead)
			advanceOnceSpace = advanceOnceSpace || iswspace(l.Lookahead)
			l.Advance(false)
		}
	}

brace_start:
	if v[tokBraceStart] && !v[tokErrorRecovery] {
		for iswspace(l.Lookahead) {
			l.Advance(true)
		}

		if l.Lookahead != '{' {
			return false
		}

		l.Advance(false)
		l.MarkEnd()

		for iswdigit(l.Lookahead) {
			l.Advance(false)
		}

		if l.Lookahead != '.' {
			return false
		}
		l.Advance(false)

		if l.Lookahead != '.' {
			return false
		}
		l.Advance(false)

		for iswdigit(l.Lookahead) {
			l.Advance(false)
		}

		if l.Lookahead != '}' {
			return false
		}

		l.ResultSymbol = tokBraceStart
		return true
	}

	return false
}
