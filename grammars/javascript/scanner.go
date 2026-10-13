package javascript

// Port of tree-sitter-javascript v0.25.0 src/scanner.c. Statement order and
// the Advance/MarkEnd call sequence follow C on every path. The C scanner has
// no state: create returns NULL and serialize writes nothing.

import "github.com/dobbo-ca/gositter"

// enum TokenType
const (
	tokAutomaticSemicolon = iota
	tokTemplateChars
	tokTernaryQmark
	tokHTMLComment
	tokLogicalOr
	tokEscapeSequence
	tokRegexPattern
	tokJSXText
)

// iswspace, iswalpha, iswdigit: libc in the "C" locale, which is what the cgo
// build runs in (nothing calls setlocale). Measured on darwin/arm64 with
// scratch/scan/javascript/isw.c over -1..0x10FFFF: ASCII only, so U+0085,
// U+00A0 and U+2028 are not spaces here.

// iswspace
func iswspace(c int32) bool { return c == ' ' || ('\t' <= c && c <= '\r') }

// iswalpha
func iswalpha(c int32) bool { return ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') }

// iswdigit
func iswdigit(c int32) bool { return '0' <= c && c <= '9' }

// Scanner
type scanner struct{}

// tree_sitter_javascript_external_scanner_create
func newScanner() gositter.ExternalScanner { return scanner{} }

// tree_sitter_javascript_external_scanner_serialize
func (scanner) Serialize(buf []byte) int { return 0 }

// tree_sitter_javascript_external_scanner_deserialize
func (scanner) Deserialize(buf []byte) {}

// scan_template_chars
func scanTemplateChars(l *gositter.Lexer) bool {
	l.ResultSymbol = tokTemplateChars
	for hasContent := false; ; hasContent = true {
		l.MarkEnd()
		switch l.Lookahead {
		case '`':
			return hasContent
		case 0:
			return false
		case '$':
			l.Advance(false)
			if l.Lookahead == '{' {
				return hasContent
			}
		case '\\':
			return hasContent
		default:
			l.Advance(false)
		}
	}
}

// WhitespaceResult
const (
	wsReject    = iota // Semicolon is illegal, ie a syntax error occurred
	wsNoNewline        // Unclear if semicolon will be legal, continue
	wsAccept           // Semicolon is legal, assuming a comment was encountered
)

// scan_whitespace_and_comments
func scanWhitespaceAndComments(l *gositter.Lexer, scannedComment *bool, consume bool) int {
	sawBlockNewline := false

	for {
		for iswspace(l.Lookahead) {
			l.Advance(true)
		}

		if l.Lookahead == '/' {
			l.Advance(true)

			if l.Lookahead == '/' {
				l.Advance(true)
				for l.Lookahead != 0 && l.Lookahead != '\n' && l.Lookahead != 0x2028 &&
					l.Lookahead != 0x2029 {
					l.Advance(true)
				}
				*scannedComment = true
			} else if l.Lookahead == '*' {
				l.Advance(true)
				for l.Lookahead != 0 {
					if l.Lookahead == '*' {
						l.Advance(true)
						if l.Lookahead == '/' {
							l.Advance(true)
							*scannedComment = true

							if l.Lookahead != '/' && !consume {
								if sawBlockNewline {
									return wsAccept
								}
								return wsNoNewline
							}

							break
						}
					} else if l.Lookahead == '\n' || l.Lookahead == 0x2028 || l.Lookahead == 0x2029 {
						sawBlockNewline = true
						l.Advance(true)
					} else {
						l.Advance(true)
					}
				}
			} else {
				return wsReject
			}
		} else {
			return wsAccept
		}
	}
}

// scan_automatic_semicolon
func scanAutomaticSemicolon(l *gositter.Lexer, commentCondition bool, scannedComment *bool) bool {
	l.ResultSymbol = tokAutomaticSemicolon
	l.MarkEnd()

	for {
		if l.Lookahead == 0 {
			return true
		}

		if l.Lookahead == '/' {
			result := scanWhitespaceAndComments(l, scannedComment, false)
			if result == wsReject {
				return false
			}

			if result == wsAccept && commentCondition && l.Lookahead != ',' && l.Lookahead != '=' {
				return true
			}
		}

		if l.Lookahead == '}' {
			return true
		}

		if l.IsAtIncludedRangeStart() {
			return true
		}

		if l.Lookahead == '\n' || l.Lookahead == 0x2028 || l.Lookahead == 0x2029 {
			break
		}

		if !iswspace(l.Lookahead) {
			return false
		}

		l.Advance(true)
	}

	l.Advance(true)

	if scanWhitespaceAndComments(l, scannedComment, true) == wsReject {
		return false
	}

	switch l.Lookahead {
	case '`', ',', ':', ';', '*', '%', '>', '<', '=', '[', '(', '?', '^', '|', '&', '/':
		return false

	// Insert a semicolon before decimals literals but not otherwise.
	case '.':
		l.Advance(true)
		return iswdigit(l.Lookahead)

	// Insert a semicolon before `--` and `++`, but not before binary `+` or `-`.
	case '+':
		l.Advance(true)
		return l.Lookahead == '+'
	case '-':
		l.Advance(true)
		return l.Lookahead == '-'

	// Don't insert a semicolon before `!=`, but do insert one before a unary `!`.
	case '!':
		l.Advance(true)
		return l.Lookahead != '='

	// Don't insert a semicolon before `in` or `instanceof`, but do insert one
	// before an identifier.
	case 'i':
		l.Advance(true)

		if l.Lookahead != 'n' {
			return true
		}
		l.Advance(true)

		if !iswalpha(l.Lookahead) {
			return false
		}

		for i := 0; i < 8; i++ {
			if l.Lookahead != int32("stanceof"[i]) {
				return true
			}
			l.Advance(true)
		}

		if !iswalpha(l.Lookahead) {
			return false
		}
	}

	return true
}

// scan_ternary_qmark
func scanTernaryQmark(l *gositter.Lexer) bool {
	for {
		if !iswspace(l.Lookahead) {
			break
		}
		l.Advance(true)
	}

	if l.Lookahead == '?' {
		l.Advance(false)

		if l.Lookahead == '?' {
			return false
		}

		l.MarkEnd()
		l.ResultSymbol = tokTernaryQmark

		if l.Lookahead == '.' {
			l.Advance(false)
			if iswdigit(l.Lookahead) {
				return true
			}
			return false
		}
		return true
	}
	return false
}

// scan_html_comment
func scanHTMLComment(l *gositter.Lexer) bool {
	for iswspace(l.Lookahead) || l.Lookahead == 0x2028 || l.Lookahead == 0x2029 {
		l.Advance(true)
	}

	const commentStart = "<!--"
	const commentEnd = "-->"

	if l.Lookahead == '<' {
		for i := 0; i < 4; i++ {
			if l.Lookahead != int32(commentStart[i]) {
				return false
			}
			l.Advance(false)
		}
	} else if l.Lookahead == '-' {
		for i := 0; i < 3; i++ {
			if l.Lookahead != int32(commentEnd[i]) {
				return false
			}
			l.Advance(false)
		}
	} else {
		return false
	}

	for l.Lookahead != 0 && l.Lookahead != '\n' && l.Lookahead != 0x2028 &&
		l.Lookahead != 0x2029 {
		l.Advance(false)
	}

	l.ResultSymbol = tokHTMLComment
	l.MarkEnd()

	return true
}

// scan_jsx_text
func scanJSXText(l *gositter.Lexer) bool {
	// sawText: any non-whitespace, or whitespace that is not a newline and
	// does not directly follow one.
	sawText := false
	// atNewline: at a newline, or at whitespace that directly follows one.
	atNewline := false

	for l.Lookahead != 0 && l.Lookahead != '<' && l.Lookahead != '>' && l.Lookahead != '{' &&
		l.Lookahead != '}' && l.Lookahead != '&' {
		isWspace := iswspace(l.Lookahead)
		if l.Lookahead == '\n' {
			atNewline = true
		} else {
			atNewline = atNewline && isWspace
			if !atNewline {
				sawText = true
			}
		}

		l.Advance(false)
	}

	l.ResultSymbol = tokJSXText
	return sawText
}

// tree_sitter_javascript_external_scanner_scan
func (scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	if validSymbols[tokTemplateChars] {
		if validSymbols[tokAutomaticSemicolon] {
			return false
		}
		return scanTemplateChars(l)
	}

	if validSymbols[tokJSXText] && scanJSXText(l) {
		return true
	}

	if validSymbols[tokAutomaticSemicolon] {
		scannedComment := false
		ret := scanAutomaticSemicolon(l, !validSymbols[tokLogicalOr], &scannedComment)
		if !ret && !scannedComment && validSymbols[tokTernaryQmark] && l.Lookahead == '?' {
			return scanTernaryQmark(l)
		}
		return ret
	}

	if validSymbols[tokTernaryQmark] {
		return scanTernaryQmark(l)
	}

	if validSymbols[tokHTMLComment] && !validSymbols[tokLogicalOr] && !validSymbols[tokEscapeSequence] &&
		!validSymbols[tokRegexPattern] {
		return scanHTMLComment(l)
	}

	return false
}
