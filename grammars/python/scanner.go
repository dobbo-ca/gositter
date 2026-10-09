package python

// Port of tree-sitter-python v0.25.0 src/scanner.c. Statement order and the
// Advance/MarkEnd call sequence follow C on every path.

import "github.com/dobbo-ca/gositter"

// enum TokenType. The scanner's own order; it is not the order of the
// grammar's externals for the three closers, and C only ORs those together.
const (
	tokNewline = iota
	tokIndent
	tokDedent
	tokStringStart
	tokStringContent
	tokEscapeInterpolation
	tokStringEnd
	tokComment
	tokCloseParen
	tokCloseBracket
	tokCloseBrace
	tokExcept
)

// enum Flags. A Delimiter is one byte of these.
const (
	flagSingleQuote = 1 << 0
	flagDoubleQuote = 1 << 1
	flagBackQuote   = 1 << 2
	flagRaw         = 1 << 3
	flagFormat      = 1 << 4
	flagTriple      = 1 << 5
	flagBytes       = 1 << 6
)

// end_character
func endCharacter(delimiter byte) int32 {
	if delimiter&flagSingleQuote != 0 {
		return '\''
	}
	if delimiter&flagDoubleQuote != 0 {
		return '"'
	}
	if delimiter&flagBackQuote != 0 {
		return '`'
	}
	return 0
}

// Scanner
type scanner struct {
	indents                  []uint16
	delimiters               []byte
	insideInterpolatedString bool
}

// tree_sitter_python_external_scanner_create
func newScanner() gositter.ExternalScanner {
	s := &scanner{}
	s.Deserialize(nil)
	return s
}

// tree_sitter_python_external_scanner_scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	errorRecoveryMode := validSymbols[tokStringContent] && validSymbols[tokIndent]
	withinBrackets := validSymbols[tokCloseBrace] || validSymbols[tokCloseParen] || validSymbols[tokCloseBracket]

	advancedOnce := false
	if validSymbols[tokEscapeInterpolation] && len(s.delimiters) > 0 &&
		(l.Lookahead == '{' || l.Lookahead == '}') && !errorRecoveryMode {
		delimiter := s.delimiters[len(s.delimiters)-1]
		if delimiter&flagFormat != 0 {
			l.MarkEnd()
			isLeftBrace := l.Lookahead == '{'
			l.Advance(false)
			advancedOnce = true
			if (l.Lookahead == '{' && isLeftBrace) || (l.Lookahead == '}' && !isLeftBrace) {
				l.Advance(false)
				l.MarkEnd()
				l.ResultSymbol = tokEscapeInterpolation
				return true
			}
			return false
		}
	}

	if validSymbols[tokStringContent] && len(s.delimiters) > 0 && !errorRecoveryMode {
		delimiter := s.delimiters[len(s.delimiters)-1]
		endChar := endCharacter(delimiter)
		hasContent := advancedOnce
		for l.Lookahead != 0 {
			if (advancedOnce || l.Lookahead == '{' || l.Lookahead == '}') && delimiter&flagFormat != 0 {
				l.MarkEnd()
				l.ResultSymbol = tokStringContent
				return hasContent
			}
			if l.Lookahead == '\\' {
				if delimiter&flagRaw != 0 {
					// Step over the backslash.
					l.Advance(false)
					// Step over any escaped quotes.
					if l.Lookahead == endCharacter(delimiter) || l.Lookahead == '\\' {
						l.Advance(false)
					}
					// Step over newlines
					if l.Lookahead == '\r' {
						l.Advance(false)
						if l.Lookahead == '\n' {
							l.Advance(false)
						}
					} else if l.Lookahead == '\n' {
						l.Advance(false)
					}
					continue
				}
				if delimiter&flagBytes != 0 {
					l.MarkEnd()
					l.Advance(false)
					if l.Lookahead == 'N' || l.Lookahead == 'u' || l.Lookahead == 'U' {
						// In a bytes string \N{...}, \uXXXX and \UXXXXXXXX
						// are not escape sequences.
						l.Advance(false)
					} else {
						l.ResultSymbol = tokStringContent
						return hasContent
					}
				} else {
					l.MarkEnd()
					l.ResultSymbol = tokStringContent
					return hasContent
				}
			} else if l.Lookahead == endChar {
				if delimiter&flagTriple != 0 {
					l.MarkEnd()
					l.Advance(false)
					if l.Lookahead == endChar {
						l.Advance(false)
						if l.Lookahead == endChar {
							if hasContent {
								l.ResultSymbol = tokStringContent
							} else {
								l.Advance(false)
								l.MarkEnd()
								s.delimiters = s.delimiters[:len(s.delimiters)-1]
								l.ResultSymbol = tokStringEnd
								s.insideInterpolatedString = false
							}
							return true
						}
						l.MarkEnd()
						l.ResultSymbol = tokStringContent
						return true
					}
					l.MarkEnd()
					l.ResultSymbol = tokStringContent
					return true
				}
				if hasContent {
					l.ResultSymbol = tokStringContent
				} else {
					l.Advance(false)
					s.delimiters = s.delimiters[:len(s.delimiters)-1]
					l.ResultSymbol = tokStringEnd
					s.insideInterpolatedString = false
				}
				l.MarkEnd()
				return true
			} else if l.Lookahead == '\n' && hasContent && delimiter&flagTriple == 0 {
				return false
			}
			l.Advance(false)
			hasContent = true
		}
	}

	l.MarkEnd()

	foundEndOfLine := false
	indentLength := uint16(0) // wraps at 65536 as in C
	firstCommentIndentLength := int32(-1)
	for {
		if l.Lookahead == '\n' {
			foundEndOfLine = true
			indentLength = 0
			l.Advance(true)
		} else if l.Lookahead == ' ' {
			indentLength++
			l.Advance(true)
		} else if l.Lookahead == '\r' || l.Lookahead == '\f' {
			indentLength = 0
			l.Advance(true)
		} else if l.Lookahead == '\t' {
			indentLength += 8
			l.Advance(true)
		} else if l.Lookahead == '#' && (validSymbols[tokIndent] || validSymbols[tokDedent] ||
			validSymbols[tokNewline] || validSymbols[tokExcept]) {
			// No EOL yet: a comment after an expression. No indent or
			// dedent token for it.
			if !foundEndOfLine {
				return false
			}
			if firstCommentIndentLength == -1 {
				firstCommentIndentLength = int32(indentLength)
			}
			for l.Lookahead != 0 && l.Lookahead != '\n' {
				l.Advance(true)
			}
			l.Advance(true)
			indentLength = 0
		} else if l.Lookahead == '\\' {
			l.Advance(true)
			if l.Lookahead == '\r' {
				l.Advance(true)
			}
			if l.Lookahead == '\n' || l.EOF() {
				l.Advance(true)
			} else {
				return false
			}
		} else if l.EOF() {
			indentLength = 0
			foundEndOfLine = true
			break
		} else {
			break
		}
	}

	if foundEndOfLine {
		if len(s.indents) > 0 {
			currentIndentLength := s.indents[len(s.indents)-1]

			if validSymbols[tokIndent] && indentLength > currentIndentLength {
				s.indents = append(s.indents, indentLength)
				l.ResultSymbol = tokIndent
				return true
			}

			nextTokIsStringStart := l.Lookahead == '"' || l.Lookahead == '\'' || l.Lookahead == '`'

			if (validSymbols[tokDedent] ||
				(!validSymbols[tokNewline] && !(validSymbols[tokStringStart] && nextTokIsStringStart) &&
					!withinBrackets)) &&
				indentLength < currentIndentLength && !s.insideInterpolatedString &&
				// Hold the dedent until comments at the block's own
				// indentation are consumed.
				firstCommentIndentLength < int32(currentIndentLength) {
				s.indents = s.indents[:len(s.indents)-1]
				l.ResultSymbol = tokDedent
				return true
			}
		}

		if validSymbols[tokNewline] && !errorRecoveryMode {
			l.ResultSymbol = tokNewline
			return true
		}
	}

	if firstCommentIndentLength == -1 && validSymbols[tokStringStart] {
		delimiter := byte(0)

		hasFlags := false
		for l.Lookahead != 0 {
			if l.Lookahead == 'f' || l.Lookahead == 'F' || l.Lookahead == 't' || l.Lookahead == 'T' {
				delimiter |= flagFormat
			} else if l.Lookahead == 'r' || l.Lookahead == 'R' {
				delimiter |= flagRaw
			} else if l.Lookahead == 'b' || l.Lookahead == 'B' {
				delimiter |= flagBytes
			} else if l.Lookahead != 'u' && l.Lookahead != 'U' {
				break
			}
			hasFlags = true
			l.Advance(false)
		}

		if l.Lookahead == '`' {
			delimiter |= flagBackQuote
			l.Advance(false)
			l.MarkEnd()
		} else if l.Lookahead == '\'' {
			delimiter |= flagSingleQuote
			l.Advance(false)
			l.MarkEnd()
			if l.Lookahead == '\'' {
				l.Advance(false)
				if l.Lookahead == '\'' {
					l.Advance(false)
					l.MarkEnd()
					delimiter |= flagTriple
				}
			}
		} else if l.Lookahead == '"' {
			delimiter |= flagDoubleQuote
			l.Advance(false)
			l.MarkEnd()
			if l.Lookahead == '"' {
				l.Advance(false)
				if l.Lookahead == '"' {
					l.Advance(false)
					l.MarkEnd()
					delimiter |= flagTriple
				}
			}
		}

		if endCharacter(delimiter) != 0 {
			s.delimiters = append(s.delimiters, delimiter)
			l.ResultSymbol = tokStringStart
			s.insideInterpolatedString = delimiter&flagFormat != 0
			return true
		}
		if hasFlags {
			return false
		}
	}

	return false
}

// tree_sitter_python_external_scanner_serialize
func (s *scanner) Serialize(buf []byte) int {
	size := 0

	buf[size] = 0
	if s.insideInterpolatedString {
		buf[size] = 1
	}
	size++

	delimiterCount := min(len(s.delimiters), 255)
	buf[size] = byte(delimiterCount)
	size++

	size += copy(buf[size:], s.delimiters[:delimiterCount])

	// C tests only size < 1024 and then writes two bytes. With an odd
	// delimiter count and 511+ indents it writes one byte past the buffer
	// and returns 1025, which trips the runtime's assert. Stop one pair
	// earlier there instead of panicking.
	for iter := 1; iter < len(s.indents) && size+1 < len(buf); iter++ {
		indentValue := s.indents[iter]
		buf[size] = byte(indentValue)
		buf[size+1] = byte(indentValue >> 8)
		size += 2
	}

	return size
}

// tree_sitter_python_external_scanner_deserialize
func (s *scanner) Deserialize(buf []byte) {
	s.delimiters = s.delimiters[:0]
	s.indents = append(s.indents[:0], 0)
	// insideInterpolatedString is not reset for an empty buffer; C leaves it.

	if len(buf) > 0 {
		size := 0

		s.insideInterpolatedString = buf[size] != 0
		size++

		delimiterCount := int(buf[size])
		size++
		if delimiterCount > 0 {
			s.delimiters = append(s.delimiters, buf[size:size+delimiterCount]...)
			size += delimiterCount
		}

		for ; size+1 < len(buf); size += 2 {
			s.indents = append(s.indents, uint16(buf[size])|uint16(buf[size+1])<<8)
		}
	}
}
