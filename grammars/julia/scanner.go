package julia

// Port of tree-sitter-julia v0.25.0 src/scanner.c. Statement order and the
// Advance/MarkEnd call sequence follow C on every path.

import "github.com/dobbo-ca/gositter"

// enum TokenType
const (
	tokBlockCommentRest = iota
	tokImmediateParen
	tokImmediateBracket
	tokImmediateBrace
	tokImmediateStringStart
	tokImmediateCommandStart
	tokContentCmd1
	tokContentCmd1Raw
	tokContentCmd3
	tokContentCmd3Raw
	tokContentStr1
	tokContentStr1Raw
	tokContentStr3
	tokContentStr3Raw
	tokEndCmd
	tokEndStr
)

// C has no scanner state: create returns NULL.
type scanner struct{}

// tree_sitter_julia_external_scanner_create
func newScanner() gositter.ExternalScanner { return scanner{} }

// tree_sitter_julia_external_scanner_serialize
func (scanner) Serialize([]byte) int { return 0 }

// tree_sitter_julia_external_scanner_deserialize
func (scanner) Deserialize([]byte) {}

// scan_content
func scanContent(l *gositter.Lexer, contentSymbol gositter.Symbol, endChar int32, nDelim uint, interp bool) bool {
	endSymbol := gositter.Symbol(tokEndCmd)
	if endChar == '"' {
		endSymbol = tokEndStr
	}
	hasContent := false
	for {
		next := l.Lookahead
		if next == 0 {
			break
		}
		l.MarkEnd()
		if interp && (next == '$' || next == '\\') {
			l.ResultSymbol = contentSymbol
			return hasContent
		} else if next == '\\' {
			// Raw strings: only \<delimiter> and \\ end the content.
			l.Advance(false)
			next = l.Lookahead
			if next == endChar || next == '\\' {
				l.ResultSymbol = contentSymbol
				return hasContent
			}
		} else {
			isEndDelimiter := true
			for i := uint(1); i <= nDelim; i++ {
				if l.Lookahead == endChar {
					l.Advance(false)
				} else {
					isEndDelimiter = false
					break
				}
			}
			if isEndDelimiter {
				if hasContent {
					l.ResultSymbol = contentSymbol
					return true
				}
				l.MarkEnd()
				l.ResultSymbol = endSymbol
				return true
			}
		}
		l.Advance(false)
		hasContent = true
	}
	return false
}

// scan_block_comment
func scanBlockComment(l *gositter.Lexer) bool {
	// The first `#=` is scanned by the generated lexer.
	afterEq := false
	nestingDepth := uint32(1)
	for {
		switch l.Lookahead {
		case '=':
			l.Advance(false)
			afterEq = true
		case '#':
			l.Advance(false)
			if afterEq {
				afterEq = false
				nestingDepth--
				if nestingDepth == 0 {
					l.ResultSymbol = tokBlockCommentRest
					return true
				}
			} else {
				afterEq = false
				if l.Lookahead == '=' {
					l.Advance(false)
					nestingDepth++
				}
			}
		case 0:
			return false
		default:
			l.Advance(false)
			afterEq = false
		}
	}
}

// tree_sitter_julia_external_scanner_scan
func (scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	if validSymbols[tokImmediateParen] && l.Lookahead == '(' {
		l.ResultSymbol = tokImmediateParen
		return true
	} else if validSymbols[tokImmediateBracket] && l.Lookahead == '[' {
		l.ResultSymbol = tokImmediateBracket
		return true
	} else if validSymbols[tokImmediateBrace] && l.Lookahead == '{' {
		l.ResultSymbol = tokImmediateBrace
		return true
	} else if validSymbols[tokImmediateStringStart] && l.Lookahead == '"' {
		l.ResultSymbol = tokImmediateStringStart
		return true
	} else if validSymbols[tokImmediateCommandStart] && l.Lookahead == '`' {
		l.ResultSymbol = tokImmediateCommandStart
		return true
	}

	if validSymbols[tokBlockCommentRest] && scanBlockComment(l) {
		return true
	}

	// Each failed attempt leaves the lexer advanced; the next one starts
	// from there, as in C.
	if validSymbols[tokContentStr1] && scanContent(l, tokContentStr1, '"', 1, true) {
		return true
	}
	if validSymbols[tokContentStr3] && scanContent(l, tokContentStr3, '"', 3, true) {
		return true
	}
	if validSymbols[tokContentCmd1] && scanContent(l, tokContentCmd1, '`', 1, true) {
		return true
	}
	if validSymbols[tokContentCmd3] && scanContent(l, tokContentCmd3, '`', 3, true) {
		return true
	}
	if validSymbols[tokContentStr1Raw] && scanContent(l, tokContentStr1Raw, '"', 1, false) {
		return true
	}
	if validSymbols[tokContentStr3Raw] && scanContent(l, tokContentStr3Raw, '"', 3, false) {
		return true
	}
	if validSymbols[tokContentCmd1Raw] && scanContent(l, tokContentCmd1Raw, '`', 1, false) {
		return true
	}
	if validSymbols[tokContentCmd3Raw] && scanContent(l, tokContentCmd3Raw, '`', 3, false) {
		return true
	}

	return false
}
