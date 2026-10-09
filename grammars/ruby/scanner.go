package ruby

// Port of tree-sitter-ruby v0.23.1 src/scanner.c. Statement order and the
// Advance/MarkEnd/EOF call sequence follow C on every path.
//
// The wctype calls run in the "C" locale (nothing calls setlocale), where
// they are ASCII only. Checked on darwin for -1 and every code point.

import (
	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/internal/cchar"
)

// enum TokenType
const (
	tokLineBreak = iota
	tokNoLineBreak

	// Delimited literals
	tokSimpleSymbol
	tokStringStart
	tokSymbolStart
	tokSubshellStart
	tokRegexStart
	tokStringArrayStart
	tokSymbolArrayStart
	tokHeredocBodyStart
	tokStringContent
	tokHeredocContent
	tokStringEnd
	tokHeredocBodyEnd
	tokHeredocStart

	// Whitespace-sensitive tokens
	tokForwardSlash
	tokBlockAmpersand
	tokSplatStar
	tokUnaryMinus
	tokUnaryMinusNum
	tokBinaryMinus
	tokBinaryStar
	tokSingletonClassLeftAngleLeftAngle
	tokHashKeySymbol
	tokIdentifierSuffix
	tokConstantSuffix
	tokHashSplatStarStar
	tokBinaryStarStar
	tokElementReferenceBracket
	tokShortInterpolation

	tokNone
)

// Literal
type literal struct {
	typ                 gositter.Symbol
	openDelimiter       int32
	closeDelimiter      int32
	nestingDepth        int32
	allowsInterpolation bool
}

// Heredoc. word is C's Array(char): each element is the low byte of a code
// point and is read back as a char (cchar.Value).
type heredoc struct {
	word                      []byte
	endWordIndentationAllowed bool
	allowsInterpolation       bool
	started                   bool
}

// Scanner
type scanner struct {
	hasLeadingWhitespace bool
	literalStack         []literal
	openHeredocs         []heredoc
}

// NON_IDENTIFIER_CHARS, indexed by byte.
var nonIdentifierChars = func() (t [256]bool) {
	for _, c := range []byte("\x00\n\r\t :;`\"'@$#.,|^&<=>+-*/\\%?!~()[]{}") {
		t[c] = true
	}
	return
}()

// strchr("!@&`'+~=/\\,;.<>*$?:\"", c) != NULL, indexed by byte. strchr also
// finds the terminating NUL, so 0 is in the set.
var shortInterpolationGlobals = func() (t [256]bool) {
	for _, c := range []byte("!@&`'+~=/\\,;.<>*$?:\"\x00") {
		t[c] = true
	}
	return
}()

// iswspace
func iswspace(c int32) bool { return c == ' ' || ('\t' <= c && c <= '\r') }

// iswdigit
func iswdigit(c int32) bool { return '0' <= c && c <= '9' }

// iswupper
func iswupper(c int32) bool { return 'A' <= c && c <= 'Z' }

// iswlower
func iswlower(c int32) bool { return 'a' <= c && c <= 'z' }

// iswalpha
func iswalpha(c int32) bool { return iswupper(c) || iswlower(c) }

// iswalnum
func iswalnum(c int32) bool { return iswalpha(c) || iswdigit(c) }

func b2b(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// skip
func (s *scanner) skip(l *gositter.Lexer) {
	s.hasLeadingWhitespace = true
	l.Advance(true)
}

// reset
func (s *scanner) reset() {
	s.literalStack = s.literalStack[:0]
	s.openHeredocs = s.openHeredocs[:0]
}

// tree_sitter_ruby_external_scanner_create
func newScanner() gositter.ExternalScanner { return &scanner{} }

// serialize / tree_sitter_ruby_external_scanner_serialize
func (s *scanner) Serialize(buffer []byte) int {
	size := 0

	if len(s.literalStack)*5+2 >= gositter.SerializationBufferSize {
		return 0
	}

	buffer[size] = byte(len(s.literalStack))
	size++
	for i := range s.literalStack {
		literal := &s.literalStack[i]
		buffer[size] = byte(literal.typ)
		buffer[size+1] = byte(literal.openDelimiter)
		buffer[size+2] = byte(literal.closeDelimiter)
		buffer[size+3] = byte(literal.nestingDepth)
		buffer[size+4] = b2b(literal.allowsInterpolation)
		size += 5
	}

	buffer[size] = byte(len(s.openHeredocs))
	size++
	for i := range s.openHeredocs {
		heredoc := &s.openHeredocs[i]
		if size+2+len(heredoc.word) >= gositter.SerializationBufferSize {
			return 0
		}
		// Not in C. C tests size+2 but writes 4 bytes plus the word, so it
		// can write 1 byte past the buffer, and it stores the word
		// length in one byte, so a word over 255 bytes reads back short.
		// Both end in an assert abort in the C build. Drop the state the
		// way C does for a state that is too big.
		if size+4+len(heredoc.word) > gositter.SerializationBufferSize || len(heredoc.word) > 255 {
			return 0
		}
		buffer[size] = b2b(heredoc.endWordIndentationAllowed)
		buffer[size+1] = b2b(heredoc.allowsInterpolation)
		buffer[size+2] = b2b(heredoc.started)
		buffer[size+3] = byte(len(heredoc.word))
		size += 4
		size += copy(buffer[size:], heredoc.word)
	}

	return size
}

// deserialize / tree_sitter_ruby_external_scanner_deserialize
func (s *scanner) Deserialize(buffer []byte) {
	size := 0
	s.hasLeadingWhitespace = false
	s.reset()

	if len(buffer) == 0 {
		return
	}

	literalDepth := int(buffer[size])
	size++
	for j := 0; j < literalDepth; j++ {
		s.literalStack = append(s.literalStack, literal{
			typ:                 gositter.Symbol(buffer[size]),
			openDelimiter:       int32(buffer[size+1]),
			closeDelimiter:      int32(buffer[size+2]),
			nestingDepth:        int32(buffer[size+3]),
			allowsInterpolation: buffer[size+4] != 0,
		})
		size += 5
	}

	openHeredocCount := int(buffer[size])
	size++
	for j := 0; j < openHeredocCount; j++ {
		// Reuse the word buffer of the slot this heredoc lands in.
		var word []byte
		if n := len(s.openHeredocs); n < cap(s.openHeredocs) {
			word = s.openHeredocs[:n+1][n].word[:0]
		}
		wordLength := int(buffer[size+3])
		s.openHeredocs = append(s.openHeredocs, heredoc{
			endWordIndentationAllowed: buffer[size] != 0,
			allowsInterpolation:       buffer[size+1] != 0,
			started:                   buffer[size+2] != 0,
			word:                      append(word, buffer[size+4:size+4+wordLength]...),
		})
		size += 4 + wordLength
	}
}

// scan_whitespace
func (s *scanner) scanWhitespace(l *gositter.Lexer, validSymbols []bool) bool {
	heredocBodyStartIsValid := len(s.openHeredocs) > 0 && !s.openHeredocs[0].started &&
		validSymbols[tokHeredocBodyStart]
	crossedNewline := false

	for {
		if !validSymbols[tokNoLineBreak] && validSymbols[tokLineBreak] && l.IsAtIncludedRangeStart() {
			l.MarkEnd()
			l.ResultSymbol = tokLineBreak
			return true
		}

		switch l.Lookahead {
		case ' ', '\t':
			s.skip(l)
		case '\r':
			if heredocBodyStartIsValid {
				l.ResultSymbol = tokHeredocBodyStart
				s.openHeredocs[0].started = true
				return true
			}
			s.skip(l)
		case '\n':
			if heredocBodyStartIsValid {
				l.ResultSymbol = tokHeredocBodyStart
				s.openHeredocs[0].started = true
				return true
			} else if !validSymbols[tokNoLineBreak] && validSymbols[tokLineBreak] && !crossedNewline {
				l.MarkEnd()
				l.Advance(false)
				crossedNewline = true
			} else {
				s.skip(l)
			}
		case '\\':
			l.Advance(false)
			if l.Lookahead == '\r' {
				s.skip(l)
			}
			if iswspace(l.Lookahead) {
				s.skip(l)
			} else {
				return false
			}
		default:
			if crossedNewline {
				if l.Lookahead != '.' && l.Lookahead != '&' && l.Lookahead != '#' {
					l.ResultSymbol = tokLineBreak
				} else if l.Lookahead == '.' {
					// Don't return LINE_BREAK for the call operator (`.`) but do return one for range
					// operators (`..` and `...`)
					l.Advance(false)
					if !l.EOF() && l.Lookahead == '.' {
						l.ResultSymbol = tokLineBreak
					} else {
						return false
					}
				}
			}
			return true
		}
	}
}

// scan_operator
func scanOperator(l *gositter.Lexer) bool {
	switch l.Lookahead {
	// <, <=, <<, <=>
	case '<':
		l.Advance(false)
		if l.Lookahead == '<' {
			l.Advance(false)
		} else if l.Lookahead == '=' {
			l.Advance(false)
			if l.Lookahead == '>' {
				l.Advance(false)
			}
		}
		return true

	// >, >=, >>
	case '>':
		l.Advance(false)
		if l.Lookahead == '>' || l.Lookahead == '=' {
			l.Advance(false)
		}
		return true

	// ==, ===, =~
	case '=':
		l.Advance(false)
		if l.Lookahead == '~' {
			l.Advance(false)
			return true
		}
		if l.Lookahead == '=' {
			l.Advance(false)
			if l.Lookahead == '=' {
				l.Advance(false)
			}
			return true
		}
		return false

	// +, -, ~, +@, -@, ~@
	case '+', '-', '~':
		l.Advance(false)
		if l.Lookahead == '@' {
			l.Advance(false)
		}
		return true

	// ..
	case '.':
		l.Advance(false)
		if l.Lookahead == '.' {
			l.Advance(false)
			return true
		}
		return false

	// &, ^, |, /, %`
	case '&', '^', '|', '/', '%', '`':
		l.Advance(false)
		return true

	// !, !=, !~
	case '!':
		l.Advance(false)
		if l.Lookahead == '=' || l.Lookahead == '~' {
			l.Advance(false)
		}
		return true

	// *, **
	case '*':
		l.Advance(false)
		if l.Lookahead == '*' {
			l.Advance(false)
		}
		return true

	// [], []=
	case '[':
		l.Advance(false)
		if l.Lookahead == ']' {
			l.Advance(false)
		} else {
			return false
		}
		if l.Lookahead == '=' {
			l.Advance(false)
		}
		return true

	default:
		return false
	}
}

// is_iden_char. C passes (char)lookahead and memchr compares bytes, so only
// the low byte of the code point counts (U+0140 is '@' here).
func isIdenChar(c int32) bool { return !nonIdentifierChars[byte(c)] }

// scan_symbol_identifier
func scanSymbolIdentifier(l *gositter.Lexer) bool {
	if l.Lookahead == '@' {
		l.Advance(false)
		if l.Lookahead == '@' {
			l.Advance(false)
		}
	} else if l.Lookahead == '$' {
		l.Advance(false)
	}

	if isIdenChar(l.Lookahead) {
		l.Advance(false)
	} else if !scanOperator(l) {
		return false
	}

	for isIdenChar(l.Lookahead) {
		l.Advance(false)
	}

	if l.Lookahead == '?' || l.Lookahead == '!' {
		l.Advance(false)
	}

	if l.Lookahead == '=' {
		l.MarkEnd()
		l.Advance(false)
		if l.Lookahead != '>' {
			l.MarkEnd()
		}
	}

	return true
}

// scan_open_delimiter
func (s *scanner) scanOpenDelimiter(l *gositter.Lexer, literal *literal, validSymbols []bool) bool {
	switch l.Lookahead {
	case '"':
		literal.typ = tokStringStart
		literal.openDelimiter, literal.closeDelimiter = l.Lookahead, l.Lookahead
		literal.allowsInterpolation = true
		l.Advance(false)
		return true

	case '\'':
		literal.typ = tokStringStart
		literal.openDelimiter, literal.closeDelimiter = l.Lookahead, l.Lookahead
		literal.allowsInterpolation = false
		l.Advance(false)
		return true

	case '`':
		if !validSymbols[tokSubshellStart] {
			return false
		}
		literal.typ = tokSubshellStart
		literal.openDelimiter, literal.closeDelimiter = l.Lookahead, l.Lookahead
		literal.allowsInterpolation = true
		l.Advance(false)
		return true

	case '/':
		if !validSymbols[tokRegexStart] {
			return false
		}
		literal.typ = tokRegexStart
		literal.openDelimiter, literal.closeDelimiter = l.Lookahead, l.Lookahead
		literal.allowsInterpolation = true
		l.Advance(false)
		if validSymbols[tokForwardSlash] {
			if !s.hasLeadingWhitespace {
				return false
			}
			if l.Lookahead == ' ' || l.Lookahead == '\t' || l.Lookahead == '\n' ||
				l.Lookahead == '\r' {
				return false
			}
			if l.Lookahead == '=' {
				return false
			}
		}
		return true

	case '%':
		l.Advance(false)

		switch l.Lookahead {
		case 's':
			if !validSymbols[tokSimpleSymbol] {
				return false
			}
			literal.typ = tokSymbolStart
			literal.allowsInterpolation = false
			l.Advance(false)

		case 'r':
			if !validSymbols[tokRegexStart] {
				return false
			}
			literal.typ = tokRegexStart
			literal.allowsInterpolation = true
			l.Advance(false)

		case 'x':
			if !validSymbols[tokSubshellStart] {
				return false
			}
			literal.typ = tokSubshellStart
			literal.allowsInterpolation = true
			l.Advance(false)

		case 'q':
			if !validSymbols[tokStringStart] {
				return false
			}
			literal.typ = tokStringStart
			literal.allowsInterpolation = false
			l.Advance(false)

		case 'Q':
			if !validSymbols[tokStringStart] {
				return false
			}
			literal.typ = tokStringStart
			literal.allowsInterpolation = true
			l.Advance(false)

		case 'w':
			if !validSymbols[tokStringArrayStart] {
				return false
			}
			literal.typ = tokStringArrayStart
			literal.allowsInterpolation = false
			l.Advance(false)

		case 'i':
			if !validSymbols[tokSymbolArrayStart] {
				return false
			}
			literal.typ = tokSymbolArrayStart
			literal.allowsInterpolation = false
			l.Advance(false)

		case 'W':
			if !validSymbols[tokStringArrayStart] {
				return false
			}
			literal.typ = tokStringArrayStart
			literal.allowsInterpolation = true
			l.Advance(false)

		case 'I':
			if !validSymbols[tokSymbolArrayStart] {
				return false
			}
			literal.typ = tokSymbolArrayStart
			literal.allowsInterpolation = true
			l.Advance(false)

		default:
			if !validSymbols[tokStringStart] {
				return false
			}
			literal.typ = tokStringStart
			literal.allowsInterpolation = true
		}

		switch l.Lookahead {
		case '(':
			literal.openDelimiter = '('
			literal.closeDelimiter = ')'

		case '[':
			literal.openDelimiter = '['
			literal.closeDelimiter = ']'

		case '{':
			literal.openDelimiter = '{'
			literal.closeDelimiter = '}'

		case '<':
			literal.openDelimiter = '<'
			literal.closeDelimiter = '>'

		case '\r', '\n', ' ', '\t':
			// If the `/` operator is valid, then so is the `%` operator, which means
			// that a `%` followed by whitespace should be considered an operator,
			// not a percent string.
			if validSymbols[tokForwardSlash] {
				return false
			}
			// Both delimiters stay 0 here, as in C: the literal then ends
			// at a NUL or at the end of input.

		case '|', '!', '#', '/', '\\', '@', '$', '%', '^', '&', '*', ')', ']', '}', '>',
			'+', '-', '~', '`', ',', '.', '?', ':', ';', '_', '"', '\'':
			literal.openDelimiter = l.Lookahead
			literal.closeDelimiter = l.Lookahead
		default:
			return false
		}

		l.Advance(false)
		return true

	default:
		return false
	}
}

// scan_heredoc_word. Returns C's `String word` in place of the Heredoc
// out-param; word is the buffer to fill.
func scanHeredocWord(l *gositter.Lexer, word []byte) (_ []byte, allowsInterpolation bool) {
	var quote int32

	switch l.Lookahead {
	case '\'', '"', '`':
		quote = l.Lookahead
		l.Advance(false)
		for l.Lookahead != quote && !l.EOF() {
			word = append(word, byte(l.Lookahead))
			l.Advance(false)
		}
		l.Advance(false)

	default:
		if iswalnum(l.Lookahead) || l.Lookahead == '_' {
			word = append(word, byte(l.Lookahead))
			l.Advance(false)
			for iswalnum(l.Lookahead) || l.Lookahead == '_' {
				word = append(word, byte(l.Lookahead))
				l.Advance(false)
			}
		}
	}

	return word, quote != '\''
}

// scan_short_interpolation. start is a C char: the '@' and '$' tests see
// only the low byte of the code point, and so does strchr.
func scanShortInterpolation(l *gositter.Lexer, hasContent bool, contentSymbol gositter.Symbol) bool {
	start := byte(l.Lookahead)
	if start == '@' || start == '$' {
		if hasContent {
			l.ResultSymbol = contentSymbol
			return true
		}
		l.MarkEnd()
		l.Advance(false)
		isShortInterpolation := false
		if start == '$' {
			if shortInterpolationGlobals[byte(l.Lookahead)] {
				isShortInterpolation = true
			} else {
				if l.Lookahead == '-' {
					l.Advance(false)
					isShortInterpolation = iswalpha(l.Lookahead) || l.Lookahead == '_'
				} else {
					isShortInterpolation = iswalnum(l.Lookahead) || l.Lookahead == '_'
				}
			}
		}
		if start == '@' {
			if l.Lookahead == '@' {
				l.Advance(false)
			}
			isShortInterpolation = isIdenChar(l.Lookahead) && !iswdigit(l.Lookahead)
		}

		if isShortInterpolation {
			l.ResultSymbol = tokShortInterpolation
			return true
		}
	}
	return false
}

// array_delete(&heredoc->word); array_erase(&scanner->open_heredocs, 0)
func (s *scanner) eraseFirstHeredoc() {
	// Rotate the first word buffer to the end so Deserialize can reuse it.
	first := s.openHeredocs[0]
	n := copy(s.openHeredocs, s.openHeredocs[1:])
	s.openHeredocs[n] = heredoc{word: first.word[:0]}
	s.openHeredocs = s.openHeredocs[:n]
}

// scan_heredoc_content
func (s *scanner) scanHeredocContent(l *gositter.Lexer) bool {
	heredoc := &s.openHeredocs[0]
	positionInWord := 0
	lookForHeredocEnd := true
	hasContent := false

	for {
		if positionInWord == len(heredoc.word) {
			if !hasContent {
				l.MarkEnd()
			}
			for l.Lookahead == ' ' || l.Lookahead == '\t' {
				l.Advance(false)
			}
			if l.Lookahead == '\n' || l.Lookahead == '\r' {
				if hasContent {
					l.ResultSymbol = tokHeredocContent
				} else {
					s.eraseFirstHeredoc()
					l.ResultSymbol = tokHeredocBodyEnd
				}
				return true
			}
			hasContent = true
			positionInWord = 0
		}

		if l.EOF() {
			l.MarkEnd()
			if hasContent {
				l.ResultSymbol = tokHeredocContent
			} else {
				s.eraseFirstHeredoc()
				l.ResultSymbol = tokHeredocBodyEnd
			}
			return true
		}

		// The word is an Array(char). Where char is signed a byte >= 0x80
		// compares as negative: 0xFF equals the -1 of an invalid UTF-8
		// byte and nothing else matches. Where it is unsigned (linux/arm64)
		// 0x80..0xFF equal U+0080..U+00FF.
		// A word is never empty (Serialize and the scan of the word see to
		// it), so the index is in range as C asserts.
		if l.Lookahead == cchar.Value(heredoc.word[positionInWord]) && lookForHeredocEnd {
			l.Advance(false)
			positionInWord++
		} else {
			positionInWord = 0
			lookForHeredocEnd = false

			if heredoc.allowsInterpolation && l.Lookahead == '\\' {
				if hasContent {
					l.ResultSymbol = tokHeredocContent
					return true
				}
				return false
			}

			if heredoc.allowsInterpolation && l.Lookahead == '#' {
				l.MarkEnd()
				l.Advance(false)
				if l.Lookahead == '{' {
					if hasContent {
						l.ResultSymbol = tokHeredocContent
						return true
					}
					return false
				}
				if scanShortInterpolation(l, hasContent, tokHeredocContent) {
					return true
				}
			} else if l.Lookahead == '\r' || l.Lookahead == '\n' {
				if l.Lookahead == '\r' {
					l.Advance(false)
					if l.Lookahead == '\n' {
						l.Advance(false)
					}
				} else {
					l.Advance(false)
				}
				hasContent = true
				lookForHeredocEnd = true
				for l.Lookahead == ' ' || l.Lookahead == '\t' {
					l.Advance(false)
					if !heredoc.endWordIndentationAllowed {
						lookForHeredocEnd = false
					}
				}
				l.MarkEnd()
			} else {
				hasContent = true
				l.Advance(false)
				l.MarkEnd()
			}
		}
	}
}

// scan_literal_content
func (s *scanner) scanLiteralContent(l *gositter.Lexer) bool {
	literal := &s.literalStack[len(s.literalStack)-1]
	hasContent := false
	stopOnSpace := literal.typ == tokSymbolArrayStart || literal.typ == tokStringArrayStart

	for {
		if stopOnSpace && iswspace(l.Lookahead) {
			if hasContent {
				l.MarkEnd()
				l.ResultSymbol = tokStringContent
				return true
			}
			return false
		}
		if l.Lookahead == literal.closeDelimiter {
			l.MarkEnd()
			if literal.nestingDepth == 1 {
				if hasContent {
					l.ResultSymbol = tokStringContent
				} else {
					l.Advance(false)
					if literal.typ == tokRegexStart {
						for iswlower(l.Lookahead) {
							l.Advance(false)
						}
					}
					s.literalStack = s.literalStack[:len(s.literalStack)-1]
					l.ResultSymbol = tokStringEnd
					l.MarkEnd()
				}
				return true
			}
			literal.nestingDepth--
			l.Advance(false)
		} else if l.Lookahead == literal.openDelimiter {
			literal.nestingDepth++
			l.Advance(false)
		} else if literal.allowsInterpolation && l.Lookahead == '#' {
			l.MarkEnd()
			l.Advance(false)
			if l.Lookahead == '{' {
				if hasContent {
					l.ResultSymbol = tokStringContent
					return true
				}
				return false
			}
			if scanShortInterpolation(l, hasContent, tokStringContent) {
				return true
			}
		} else if l.Lookahead == '\\' {
			if literal.allowsInterpolation {
				if hasContent {
					l.MarkEnd()
					l.ResultSymbol = tokStringContent
					return true
				}
				return false
			}
			l.Advance(false)
			l.Advance(false)
		} else if l.EOF() {
			l.Advance(false)
			l.MarkEnd()
			return false
		} else {
			l.Advance(false)
		}

		hasContent = true
	}
}

// scan / tree_sitter_ruby_external_scanner_scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	s.hasLeadingWhitespace = false

	// Contents of literals, which match any character except for some close delimiter
	if !validSymbols[tokStringStart] {
		if (validSymbols[tokStringContent] || validSymbols[tokStringEnd]) && len(s.literalStack) > 0 {
			return s.scanLiteralContent(l)
		}
		if (validSymbols[tokHeredocContent] || validSymbols[tokHeredocBodyEnd]) && len(s.openHeredocs) > 0 {
			return s.scanHeredocContent(l)
		}
	}

	// Whitespace
	l.ResultSymbol = tokNone
	if !s.scanWhitespace(l, validSymbols) {
		return false
	}
	if l.ResultSymbol != tokNone {
		return true
	}

	switch l.Lookahead {
	case '&':
		if validSymbols[tokBlockAmpersand] {
			l.Advance(false)
			if l.Lookahead != '&' && l.Lookahead != '.' && l.Lookahead != '=' &&
				!iswspace(l.Lookahead) {
				l.ResultSymbol = tokBlockAmpersand
				return true
			}
			return false
		}

	case '<':
		if validSymbols[tokSingletonClassLeftAngleLeftAngle] {
			l.Advance(false)
			if l.Lookahead == '<' {
				l.Advance(false)
				l.ResultSymbol = tokSingletonClassLeftAngleLeftAngle
				return true
			}
			return false
		}

	case '*':
		if validSymbols[tokSplatStar] || validSymbols[tokBinaryStar] || validSymbols[tokHashSplatStarStar] ||
			validSymbols[tokBinaryStarStar] {
			l.Advance(false)
			if l.Lookahead == '=' {
				return false
			}
			if l.Lookahead == '*' {
				if validSymbols[tokHashSplatStarStar] || validSymbols[tokBinaryStarStar] {
					l.Advance(false)
					if l.Lookahead == '=' {
						return false
					}
					if validSymbols[tokBinaryStarStar] && !s.hasLeadingWhitespace {
						l.ResultSymbol = tokBinaryStarStar
						return true
					}
					if validSymbols[tokHashSplatStarStar] && !iswspace(l.Lookahead) {
						l.ResultSymbol = tokHashSplatStarStar
						return true
					}
					if validSymbols[tokBinaryStarStar] {
						l.ResultSymbol = tokBinaryStarStar
						return true
					}
					if validSymbols[tokHashSplatStarStar] {
						l.ResultSymbol = tokHashSplatStarStar
						return true
					}
					return false
				}
				return false
			}
			if validSymbols[tokBinaryStar] && !s.hasLeadingWhitespace {
				l.ResultSymbol = tokBinaryStar
				return true
			}
			if validSymbols[tokSplatStar] && !iswspace(l.Lookahead) {
				l.ResultSymbol = tokSplatStar
				return true
			}
			if validSymbols[tokBinaryStar] {
				l.ResultSymbol = tokBinaryStar
				return true
			}
			if validSymbols[tokSplatStar] {
				l.ResultSymbol = tokSplatStar
				return true
			}
			return false
		}

	case '-':
		if validSymbols[tokUnaryMinus] || validSymbols[tokUnaryMinusNum] || validSymbols[tokBinaryMinus] {
			l.Advance(false)
			if l.Lookahead != '=' && l.Lookahead != '>' {
				if validSymbols[tokUnaryMinusNum] &&
					(!validSymbols[tokBinaryStar] || s.hasLeadingWhitespace) &&
					iswdigit(l.Lookahead) {
					l.ResultSymbol = tokUnaryMinusNum
					return true
				}
				if validSymbols[tokUnaryMinus] && s.hasLeadingWhitespace && !iswspace(l.Lookahead) {
					l.ResultSymbol = tokUnaryMinus
				} else if validSymbols[tokBinaryMinus] {
					l.ResultSymbol = tokBinaryMinus
				} else {
					l.ResultSymbol = tokUnaryMinus
				}
				return true
			}
			return false
		}

	case ':':
		if validSymbols[tokSymbolStart] {
			literal := literal{typ: tokSymbolStart, nestingDepth: 1}
			l.Advance(false)

			switch l.Lookahead {
			case '"':
				l.Advance(false)
				literal.openDelimiter = '"'
				literal.closeDelimiter = '"'
				literal.allowsInterpolation = true
				s.literalStack = append(s.literalStack, literal)
				l.ResultSymbol = tokSymbolStart
				return true

			case '\'':
				l.Advance(false)
				literal.openDelimiter = '\''
				literal.closeDelimiter = '\''
				literal.allowsInterpolation = false
				s.literalStack = append(s.literalStack, literal)
				l.ResultSymbol = tokSymbolStart
				return true

			default:
				if scanSymbolIdentifier(l) {
					l.ResultSymbol = tokSimpleSymbol
					return true
				}
			}

			return false
		}

	case '[':
		// Treat a square bracket as an element reference if either:
		// * the bracket is not preceded by any whitespace
		// * an arbitrary expression is not valid at the current position.
		if validSymbols[tokElementReferenceBracket] &&
			(!s.hasLeadingWhitespace || !validSymbols[tokStringStart]) {
			l.Advance(false)
			l.ResultSymbol = tokElementReferenceBracket
			return true
		}
	}

	// Open delimiters for literals
	if ((validSymbols[tokHashKeySymbol] || validSymbols[tokIdentifierSuffix]) &&
		(iswalpha(l.Lookahead) || l.Lookahead == '_')) ||
		(validSymbols[tokConstantSuffix] && iswupper(l.Lookahead)) {
		var validIdentifierSymbol gositter.Symbol = tokIdentifierSuffix
		if iswupper(l.Lookahead) {
			validIdentifierSymbol = tokConstantSuffix
		}
		for iswalnum(l.Lookahead) || l.Lookahead == '_' {
			l.Advance(false)
		}

		if validSymbols[tokHashKeySymbol] && l.Lookahead == ':' {
			l.MarkEnd()
			l.Advance(false)
			if l.Lookahead != ':' {
				l.ResultSymbol = tokHashKeySymbol
				return true
			}
		} else if validSymbols[validIdentifierSymbol] && l.Lookahead == '!' {
			l.Advance(false)
			if l.Lookahead != '=' {
				l.ResultSymbol = validIdentifierSymbol
				return true
			}
		}

		return false
	}

	// Open delimiters for literals
	if validSymbols[tokStringStart] {
		literal := literal{nestingDepth: 1}

		if l.Lookahead == '<' {
			l.Advance(false)
			if l.Lookahead != '<' {
				return false
			}
			l.Advance(false)

			var hd heredoc
			if l.Lookahead == '-' || l.Lookahead == '~' {
				l.Advance(false)
				hd.endWordIndentationAllowed = true
			}

			// Fill the word buffer of the slot the heredoc would land in.
			n := len(s.openHeredocs)
			if n < cap(s.openHeredocs) {
				hd.word = s.openHeredocs[:n+1][n].word[:0]
			}
			hd.word, hd.allowsInterpolation = scanHeredocWord(l, hd.word)
			if len(hd.word) == 0 {
				if n < cap(s.openHeredocs) {
					s.openHeredocs[:n+1][n].word = hd.word
				}
				return false
			}
			s.openHeredocs = append(s.openHeredocs, hd)
			l.ResultSymbol = tokHeredocStart
			return true
		}

		if s.scanOpenDelimiter(l, &literal, validSymbols) {
			s.literalStack = append(s.literalStack, literal)
			l.ResultSymbol = literal.typ
			return true
		}
		return false
	}

	return false
}
