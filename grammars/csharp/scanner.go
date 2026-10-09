package csharp

// Port of tree-sitter-c-sharp v0.23.5 src/scanner.c. Statement order and the
// Advance/MarkEnd call sequence follow C on every path.

import "github.com/dobbo-ca/gositter"

// enum TokenType
const (
	tokOptSemi = iota
	tokInterpolationRegularStart
	tokInterpolationVerbatimStart
	tokInterpolationRawStart
	tokInterpolationStartQuote
	tokInterpolationEndQuote
	tokInterpolationOpenBrace
	tokInterpolationCloseBrace
	tokInterpolationStringContent
	tokRawStringStart
	tokRawStringEnd
	tokRawStringContent
)

// enum StringType
const (
	stringRegular  = 1 << 0
	stringVerbatim = 1 << 1
	stringRaw      = 1 << 2
)

// Interpolation. string_type is a 4-byte enum in C, but only values 0..7 are
// ever stored and serialize keeps one byte, so a byte holds it.
type interpolation struct {
	dollarCount    uint8
	openBraceCount uint8
	quoteCount     uint8
	stringType     uint8
}

// is_regular
func (i *interpolation) isRegular() bool { return i.stringType&stringRegular != 0 }

// is_verbatim
func (i *interpolation) isVerbatim() bool { return i.stringType&stringVerbatim != 0 }

// is_raw
func (i *interpolation) isRaw() bool { return i.stringType&stringRaw != 0 }

// Scanner
type scanner struct {
	quoteCount         uint8
	interpolationStack []interpolation
}

// iswspace in the "C" locale, which is what a cgo binary runs in (nothing
// calls setlocale): the six ASCII spaces, nothing above 127, not -1.
func iswspace(c int32) bool {
	return c == ' ' || ('\t' <= c && c <= '\r')
}

// tree_sitter_c_sharp_external_scanner_create
func newScanner() gositter.ExternalScanner {
	return &scanner{}
}

// tree_sitter_c_sharp_external_scanner_serialize
func (s *scanner) Serialize(buf []byte) int {
	if len(s.interpolationStack)*4+2 > gositter.SerializationBufferSize {
		return 0
	}

	size := 0

	buf[size] = s.quoteCount
	size++
	// At most 255 entries pass the test above, so the byte does not wrap.
	buf[size] = byte(len(s.interpolationStack))
	size++

	for i := range s.interpolationStack {
		in := &s.interpolationStack[i]
		buf[size] = in.dollarCount
		buf[size+1] = in.openBraceCount
		buf[size+2] = in.quoteCount
		buf[size+3] = in.stringType
		size += 4
	}

	return size
}

// tree_sitter_c_sharp_external_scanner_deserialize
func (s *scanner) Deserialize(buf []byte) {
	s.quoteCount = 0
	s.interpolationStack = s.interpolationStack[:0]
	size := 0

	if len(buf) > 0 {
		s.quoteCount = buf[size]
		size++
		n := int(buf[size])
		size++

		for i := 0; i < n; i++ {
			s.interpolationStack = append(s.interpolationStack, interpolation{
				dollarCount:    buf[size],
				openBraceCount: buf[size+1],
				quoteCount:     buf[size+2],
				stringType:     buf[size+3],
			})
			size += 4
		}
	}
	// C: assert(size == length). Holds for every buffer Serialize wrote.
}

// tree_sitter_c_sharp_external_scanner_scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	// uint8 counters wrap at 256 as in C.
	braceAdvanced := uint8(0)
	quoteCount := uint8(0)
	didAdvance := false

	// error recovery, gives better trees this way
	if validSymbols[tokOptSemi] && validSymbols[tokInterpolationRegularStart] {
		return false
	}

	if validSymbols[tokOptSemi] {
		l.ResultSymbol = tokOptSemi
		if l.Lookahead == ';' {
			l.Advance(false)
		}
		return true
	}

	if validSymbols[tokRawStringStart] {
		for iswspace(l.Lookahead) {
			l.Advance(true)
		}

		if l.Lookahead == '"' {
			for l.Lookahead == '"' {
				l.Advance(false)
				quoteCount++
			}

			if quoteCount >= 3 {
				l.ResultSymbol = tokRawStringStart
				s.quoteCount = quoteCount
				return true
			}
		}
	}

	if validSymbols[tokRawStringEnd] && l.Lookahead == '"' {
		for l.Lookahead == '"' {
			l.Advance(false)
			quoteCount++
		}

		if quoteCount == s.quoteCount {
			l.ResultSymbol = tokRawStringEnd
			s.quoteCount = 0
			return true
		}

		didAdvance = quoteCount > 0
	}

	if validSymbols[tokRawStringContent] {
		for l.Lookahead != 0 {
			if l.Lookahead == '"' {
				l.MarkEnd()
				quoteCount = 0

				for l.Lookahead == '"' {
					l.Advance(false)
					quoteCount++
				}

				if quoteCount == s.quoteCount {
					l.ResultSymbol = tokRawStringContent
					return true
				}
			}
			l.Advance(false)
			didAdvance = true
		}
		l.MarkEnd()
		l.ResultSymbol = tokRawStringContent
		return true
	}

	if validSymbols[tokInterpolationRegularStart] || validSymbols[tokInterpolationVerbatimStart] ||
		validSymbols[tokInterpolationRawStart] {
		for iswspace(l.Lookahead) {
			l.Advance(true)
		}

		dollarAdvanced := uint8(0)

		isVerbatim := false

		if l.Lookahead == '@' {
			isVerbatim = true
			l.Advance(false)
		}

		// quoteCount can be non-zero here: the raw string branches above
		// may have consumed quotes without returning.
		for l.Lookahead == '$' && quoteCount == 0 {
			l.Advance(false)
			dollarAdvanced++
		}

		if dollarAdvanced > 0 && (l.Lookahead == '"' || l.Lookahead == '@') {
			l.ResultSymbol = tokInterpolationRegularStart
			in := interpolation{dollarCount: dollarAdvanced}

			if isVerbatim || l.Lookahead == '@' {
				if l.Lookahead == '@' {
					l.Advance(false)
					isVerbatim = true
				}
				l.ResultSymbol = tokInterpolationVerbatimStart
				in.stringType = stringVerbatim
			}

			l.MarkEnd()
			l.Advance(false)

			if l.Lookahead == '"' && !isVerbatim {
				l.Advance(false)
				if l.Lookahead == '"' {
					l.ResultSymbol = tokInterpolationRawStart
					in.stringType |= stringRaw
					s.interpolationStack = append(s.interpolationStack, in)
				}
				// If we find 1 or 3 quotes, we push an interpolation.
				// If there's only two quotes, that's just an empty string
			} else {
				in.stringType |= stringRegular
				s.interpolationStack = append(s.interpolationStack, in)
			}

			return true
		}
	}

	if validSymbols[tokInterpolationStartQuote] && len(s.interpolationStack) > 0 {
		cur := &s.interpolationStack[len(s.interpolationStack)-1]

		if cur.isVerbatim() || cur.isRegular() {
			if l.Lookahead == '"' {
				l.Advance(false)
				cur.quoteCount++
			}
		} else {
			for l.Lookahead == '"' {
				l.Advance(false)
				cur.quoteCount++
			}
		}

		l.ResultSymbol = tokInterpolationStartQuote
		return cur.quoteCount > 0
	}

	if validSymbols[tokInterpolationEndQuote] && len(s.interpolationStack) > 0 {
		cur := &s.interpolationStack[len(s.interpolationStack)-1]

		for l.Lookahead == '"' {
			l.Advance(false)
			quoteCount++
		}

		if quoteCount == cur.quoteCount {
			l.ResultSymbol = tokInterpolationEndQuote
			s.interpolationStack = s.interpolationStack[:len(s.interpolationStack)-1]
			return true
		}

		didAdvance = quoteCount > 0
	}

	if validSymbols[tokInterpolationOpenBrace] && len(s.interpolationStack) > 0 {
		cur := &s.interpolationStack[len(s.interpolationStack)-1]

		for l.Lookahead == '{' && braceAdvanced < cur.dollarCount {
			l.Advance(false)
			braceAdvanced++
		}

		if braceAdvanced > 0 && braceAdvanced == cur.dollarCount &&
			(braceAdvanced == 0 || l.Lookahead != '{') {
			cur.openBraceCount = braceAdvanced
			l.ResultSymbol = tokInterpolationOpenBrace
			return true
		}
	}

	if validSymbols[tokInterpolationCloseBrace] && len(s.interpolationStack) > 0 {
		braceAdvanced := uint8(0) // shadows the outer one, as in C
		cur := &s.interpolationStack[len(s.interpolationStack)-1]

		for iswspace(l.Lookahead) {
			l.Advance(false)
		}

		for l.Lookahead == '}' {
			l.Advance(false)
			braceAdvanced++

			if braceAdvanced == cur.openBraceCount {
				cur.openBraceCount = 0
				l.ResultSymbol = tokInterpolationCloseBrace
				return true
			}
		}

		return false
	}

	if validSymbols[tokInterpolationStringContent] && len(s.interpolationStack) > 0 {
		l.ResultSymbol = tokInterpolationStringContent
		cur := &s.interpolationStack[len(s.interpolationStack)-1]

		for l.Lookahead != 0 {
			// top-down approach, first see if it's raw
			if cur.isRaw() {
				if l.Lookahead == '"' {
					l.MarkEnd()
					l.Advance(false)
					if l.Lookahead == '"' {
						l.Advance(false)
						quoteAdvanced := uint8(2)
						for l.Lookahead == '"' {
							quoteAdvanced++
							l.Advance(false)
						}
						if quoteAdvanced == cur.quoteCount {
							return didAdvance
						}
					}
				}

				if l.Lookahead == '{' {
					l.MarkEnd()

					for l.Lookahead == '{' && braceAdvanced < cur.openBraceCount {
						l.Advance(false)
						braceAdvanced++
					}

					if braceAdvanced == cur.openBraceCount &&
						(braceAdvanced == 0 || l.Lookahead != '{') {
						return didAdvance
					}
				}
			} else if cur.isVerbatim() {
				// then verbatim, since it could be verbatim + raw, but run the raw branch first
				if l.Lookahead == '"' {
					l.MarkEnd()
					l.Advance(false)
					if l.Lookahead == '"' {
						l.Advance(false)
						continue
					}
					return didAdvance
				}

				if l.Lookahead == '{' {
					l.MarkEnd()

					for l.Lookahead == '{' && braceAdvanced < cur.openBraceCount {
						l.Advance(false)
						braceAdvanced++
					}

					if braceAdvanced == cur.openBraceCount &&
						(braceAdvanced == 0 || l.Lookahead != '{') {
						return didAdvance
					}
				}
			} else if cur.isRegular() {
				// finally regular
				if l.Lookahead == '\\' || l.Lookahead == '\n' || l.Lookahead == '"' {
					l.MarkEnd()
					return didAdvance
				}

				if l.Lookahead == '{' {
					l.MarkEnd()

					for l.Lookahead == '{' && braceAdvanced < cur.openBraceCount {
						l.Advance(false)
						braceAdvanced++
					}

					// in a brace, never collect more than open_brace_count
					if braceAdvanced == cur.openBraceCount &&
						(braceAdvanced == 0 || l.Lookahead != '{') {
						return didAdvance
					}
				}
			}

			if l.Lookahead != '{' {
				braceAdvanced = 0
			}
			l.Advance(false)
			didAdvance = true
		}

		l.MarkEnd()
		return didAdvance
	}

	return false
}
