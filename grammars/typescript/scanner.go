package typescript

// Port of tree-sitter-typescript v0.23.2 common/scanner.h (included by
// typescript/src/scanner.c and tsx/src/scanner.c). grammars/tsx/scanner.go is
// the same file but for the package name. Statement order and the
// Advance/MarkEnd call sequence follow C on every path.

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
	tokFunctionSignatureAutomaticSemicolon
	tokErrorRecovery
)

// iswspace. No setlocale call anywhere, so libc is in the "C" locale: measured
// on darwin/arm64 over -2..0x110000, true only for 9..13 and 32 (glibc's C
// locale agrees). -1 (invalid UTF-8) is false.
func iswspace(c int32) bool { return (c >= '\t' && c <= '\r') || c == ' ' }

// iswalpha. Same measurement: A-Z and a-z only.
func iswalpha(c int32) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }

// iswdigit
func iswdigit(c int32) bool { return c >= '0' && c <= '9' }

// The C scanner has no state: create returns NULL, serialize returns 0.
type scanner struct{}

// tree_sitter_typescript_external_scanner_create
func newScanner() gositter.ExternalScanner { return scanner{} }

// tree_sitter_typescript_external_scanner_serialize
func (scanner) Serialize(buf []byte) int { return 0 }

// tree_sitter_typescript_external_scanner_deserialize
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

// scan_whitespace_and_comments
func scanWhitespaceAndComments(l *gositter.Lexer, scannedComment *bool) bool {
	for {
		for iswspace(l.Lookahead) {
			l.Advance(true)
		}

		if l.Lookahead == '/' {
			l.Advance(true)

			if l.Lookahead == '/' {
				l.Advance(true)
				for l.Lookahead != 0 && l.Lookahead != '\n' {
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
							break
						}
					} else {
						l.Advance(true)
					}
				}
			} else {
				return false
			}
		} else {
			return true
		}
	}
}

// scan_automatic_semicolon
func scanAutomaticSemicolon(l *gositter.Lexer, validSymbols []bool, scannedComment *bool) bool {
	l.ResultSymbol = tokAutomaticSemicolon
	l.MarkEnd()

	for {
		if l.Lookahead == 0 {
			return true
		}
		if l.Lookahead == '}' {
			// No automatic semicolon before a type annotation:
			//   type F = ({a}: {a: number}) => number;
			for {
				l.Advance(true)
				if !iswspace(l.Lookahead) {
					break
				}
			}
			if l.Lookahead == ':' {
				return validSymbols[tokLogicalOr] // true inside a ternary
			}
			return true
		}
		if !iswspace(l.Lookahead) {
			return false
		}
		if l.Lookahead == '\n' {
			break
		}
		l.Advance(true)
	}

	l.Advance(true)

	if !scanWhitespaceAndComments(l, scannedComment) {
		return false
	}

	switch l.Lookahead {
	case '`', ',', '.', ';', '*', '%', '>', '<', '=', '?', '^', '|', '&', '/', ':':
		return false

	case '{':
		if validSymbols[tokFunctionSignatureAutomaticSemicolon] {
			return false
		}

	// No semicolon before '[' or '(' unless a type is being parsed; a
	// valid binary operator means an expression.
	case '(', '[':
		if validSymbols[tokLogicalOr] {
			return false
		}

	// Semicolon before `--` and `++`, not before binary `+` or `-`.
	case '+':
		l.Advance(true)
		return l.Lookahead == '+'
	case '-':
		l.Advance(true)
		return l.Lookahead == '-'

	// Not before `!=`, but before a unary `!`.
	case '!':
		l.Advance(true)
		return l.Lookahead != '='

	// Not before `in` or `instanceof`, but before an identifier.
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

		// Optional chaining.
		if l.Lookahead == '?' || l.Lookahead == '.' {
			return false
		}

		l.MarkEnd()
		l.ResultSymbol = tokTernaryQmark

		// Optional parameters have ?: with whitespace allowed between.
		for {
			if !iswspace(l.Lookahead) {
				break
			}
			l.Advance(false)
		}

		if l.Lookahead == ':' || l.Lookahead == ')' || l.Lookahead == ',' {
			return false
		}

		if l.Lookahead == '.' {
			l.Advance(false)
			return iswdigit(l.Lookahead)
		}
		return true
	}
	return false
}

// scan_closing_comment
func scanClosingComment(l *gositter.Lexer) bool {
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

	for l.Lookahead != 0 && l.Lookahead != '\n' && l.Lookahead != 0x2028 && l.Lookahead != 0x2029 {
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
	// atNewline: at a newline, or in whitespace directly after one.
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

// external_scanner_scan (tree_sitter_typescript_external_scanner_scan)
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

	if validSymbols[tokAutomaticSemicolon] || validSymbols[tokFunctionSignatureAutomaticSemicolon] {
		scannedComment := false
		ret := scanAutomaticSemicolon(l, validSymbols, &scannedComment)
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
		return scanClosingComment(l)
	}

	return false
}
