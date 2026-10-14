package cpp

// Port of tree-sitter-cpp v0.23.4 src/scanner.c. Statement order and the
// Advance/MarkEnd/EOF call sequence follow C on every path.

import "github.com/dobbo-ca/gositter"

// enum TokenType
const (
	tokRawStringDelimiter = iota
	tokRawStringContent
)

// MAX_DELIMITER_LENGTH
const maxDelimiterLength = 16

// sizeof(wchar_t). 4 on every target of the C build (darwin, linux); the
// serialized state is delimiter_length of them, little-endian.
const sizeofWchar = 4

// Scanner
type scanner struct {
	delimiterLength uint8
	delimiter       [maxDelimiterLength]int32 // wchar_t is a signed 32-bit int
}

// iswspace in the "C" locale, which is what the C build runs in: checked on
// darwin for every code point and -1, only these six are space.
func iswspace(c int32) bool { return c == ' ' || ('\t' <= c && c <= '\r') }

// reset
func (s *scanner) reset() {
	s.delimiterLength = 0
	s.delimiter = [maxDelimiterLength]int32{}
}

// scan_raw_string_delimiter
func (s *scanner) scanRawStringDelimiter(l *gositter.Lexer) bool {
	if s.delimiterLength > 0 {
		// Closing delimiter: must match the opening one exactly.
		for i := 0; i < int(s.delimiterLength); i++ {
			if l.Lookahead != s.delimiter[i] {
				return false
			}
			l.Advance(false)
		}
		s.reset()
		return true
	}

	// Opening delimiter. A failed scan leaves delimiterLength dirty, as C
	// does; the runtime deserializes before the next scan.
	for {
		if s.delimiterLength >= maxDelimiterLength || l.EOF() || l.Lookahead == '\\' ||
			iswspace(l.Lookahead) {
			return false
		}
		if l.Lookahead == '(' {
			// Empty delimiter: fail, the grammar has a rule without one.
			return s.delimiterLength > 0
		}
		s.delimiter[s.delimiterLength] = l.Lookahead
		s.delimiterLength++
		l.Advance(false)
	}
}

// scan_raw_string_content
func (s *scanner) scanRawStringContent(l *gositter.Lexer) bool {
	// Progress through the delimiter since the last ')'.
	for delimiterIndex := -1; ; {
		// At EOF the content ends there: an incomplete raw_string_literal.
		if l.EOF() {
			l.MarkEnd()
			return true
		}

		if delimiterIndex >= 0 {
			if delimiterIndex == int(s.delimiterLength) {
				if l.Lookahead == '"' {
					return true
				}
				delimiterIndex = -1
			} else {
				if l.Lookahead == s.delimiter[delimiterIndex] {
					delimiterIndex++
				} else {
					delimiterIndex = -1
				}
			}
		}

		if delimiterIndex == -1 && l.Lookahead == ')' {
			// The token excludes the )delimiter" part.
			l.MarkEnd()
			delimiterIndex = 0
		}

		l.Advance(false)
	}
}

// tree_sitter_cpp_external_scanner_create
func newScanner() gositter.ExternalScanner { return &scanner{} }

// tree_sitter_cpp_external_scanner_scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	if validSymbols[tokRawStringDelimiter] && validSymbols[tokRawStringContent] {
		// error recovery
		return false
	}

	// No whitespace skipping: the raw-string grammar is space-sensitive.
	if validSymbols[tokRawStringDelimiter] {
		l.ResultSymbol = tokRawStringDelimiter
		return s.scanRawStringDelimiter(l)
	}

	if validSymbols[tokRawStringContent] {
		l.ResultSymbol = tokRawStringContent
		return s.scanRawStringContent(l)
	}

	return false
}

// tree_sitter_cpp_external_scanner_serialize
func (s *scanner) Serialize(buf []byte) int {
	size := 0
	for _, c := range s.delimiter[:s.delimiterLength] {
		buf[size] = byte(c)
		buf[size+1] = byte(c >> 8)
		buf[size+2] = byte(c >> 16)
		buf[size+3] = byte(c >> 24)
		size += sizeofWchar
	}
	return size
}

// tree_sitter_cpp_external_scanner_deserialize
func (s *scanner) Deserialize(buf []byte) {
	// C asserts length % sizeof(wchar_t) == 0. The runtime only passes back
	// what Serialize wrote, so at most 64 bytes in whole wchar_t units.
	s.delimiterLength = uint8(len(buf) / sizeofWchar)
	// Entries past the new length keep their old value, as with C's memcpy.
	// Nothing reads them.
	for i := 0; i+sizeofWchar <= len(buf); i += sizeofWchar {
		s.delimiter[i/sizeofWchar] = int32(uint32(buf[i]) | uint32(buf[i+1])<<8 | uint32(buf[i+2])<<16 | uint32(buf[i+3])<<24)
	}
}

// Reset: the state newScanner returns.
func (s *scanner) Reset() { *s = scanner{} }
