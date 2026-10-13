package kotlin

// Port of tree-sitter-kotlin v1.1.0 src/scanner.c. Statement order and the
// Advance/MarkEnd/EOF call sequence follow C on every path. The C gotos out
// of the semicolon block are the entry values of Scan's tail.

import "github.com/dobbo-ca/gositter"

// enum TokenType
const (
	tokSemi = iota
	tokClassMemberSemi
	tokBlockComment
	tokNotIs
	tokIn
	tokQDot
	tokMultilineStringContent
	tokConstructor
	tokGet
	tokSet
	tokDollar
)

const maxWordSize = 16

// The wctype calls are libc and locale dependent. These are darwin in the
// "C" locale, which is what the cgo build runs in: ASCII only, checked for
// every code point from -2 to 0x110000 (scratch/scan/kotlin/isw.c). musl and
// some glibc locales also accept non-ASCII letters and spaces.

// iswspace
func iswspace(c int32) bool { return c == ' ' || ('\t' <= c && c <= '\r') }

// iswalpha
func iswalpha(c int32) bool { return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') }

// iswdigit
func iswdigit(c int32) bool { return '0' <= c && c <= '9' }

// iswalnum
func iswalnum(c int32) bool { return iswalpha(c) || iswdigit(c) }

// The two word lists of the C compound literals. C pads them to 16 empty
// entries; a scanned word is never empty when compared, so those never match.
var (
	modifierWords = [...]string{"public", "private", "protected", "internal", "abstract",
		"final", "open", "override", "lateinit", "vararg", "noinline",
		"crossinline", "external", "suspend", "inline"}
	keywordWords = [...]string{"else", "in", "instanceof", "get", "set", "constructor", "by", "as", "where"}
)

// scan_word
func scanWord(l *gositter.Lexer, word string) bool {
	for i := 0; i < len(word); i++ {
		if l.Lookahead != int32(word[i]) {
			return false
		}
		l.Advance(true)
	}
	return true
}

// scan_words
func scanWords(l *gositter.Lexer, words []string, scannedWord *[maxWordSize]byte, index *uint8) bool {
	if scannedWord[0] == 0 {
		// At most 15 letters; the rest of a longer word stays unread.
		for i := 0; i < maxWordSize-1; i++ {
			if !iswalpha(l.Lookahead) {
				if i == 0 {
					return false
				}
				break
			}
			scannedWord[i] = byte(l.Lookahead)
			l.Advance(true)
		}
	}

	n := 0
	for scannedWord[n] != 0 {
		n++
	}
	for i, w := range words {
		if string(scannedWord[:n]) == w {
			if index != nil {
				*index = uint8(i)
			}
			return true
		}
	}

	return false
}

// Where the semicolon block leaves to. The C labels, in source order.
const (
	entryStart   = iota // no semicolon token valid: line 358
	entryNotIs          // continue_not_is_from_semi
	entryQDot           // q_dot_from_semi
	entryComment        // comment
	entryReturn         // the block returned ret
)

// The scanner has no state: C create returns NULL.
type scanner struct{}

// tree_sitter_kotlin_external_scanner_create
func newScanner() gositter.ExternalScanner { return scanner{} }

// tree_sitter_kotlin_external_scanner_serialize
func (scanner) Serialize(buf []byte) int { return 0 }

// tree_sitter_kotlin_external_scanner_deserialize
func (scanner) Deserialize(buf []byte) {}

// The `if (valid_symbols[SEMI] || valid_symbols[CLASS_MEMBER_SEMI])` block of
// tree_sitter_kotlin_external_scanner_scan.
func scanSemi(l *gositter.Lexer, validSymbols []bool) (entry int, ret bool) {
	if validSymbols[tokSemi] {
		l.ResultSymbol = tokSemi
	} else {
		l.ResultSymbol = tokClassMemberSemi
	}
	l.MarkEnd()
	sawNewline := false
	for {
		if l.EOF() {
			return entryReturn, true
		}

		if l.Lookahead == ';' {
			l.Advance(false)
			l.MarkEnd()
			return entryReturn, true
		}

		if !iswspace(l.Lookahead) {
			break
		}

		if l.Lookahead == '\n' {
			l.Advance(true)
			sawNewline = true
			break
		}

		if l.Lookahead == '\r' {
			l.Advance(true)

			if l.Lookahead == '\n' {
				l.Advance(true)
			}

			sawNewline = true
			break
		}

		l.Advance(true)
	}

	// Skip whitespace and comments
	for iswspace(l.Lookahead) {
		l.Advance(true)
	}
	if l.Lookahead == '/' {
		return entryComment, false
	}

	if !sawNewline {
		switch l.Lookahead {
		case '!':
			l.Advance(true)
			return entryNotIs, false
		case '?':
			if validSymbols[tokQDot] {
				return entryQDot, false
			}
			return entryReturn, false
		case 'i':
			return entryReturn, scanWord(l, "import")
		case ';':
			l.Advance(false)
			l.MarkEnd()
			return entryReturn, true
		default:
			return entryReturn, false
		}
	}

	var scannedWord [maxWordSize]byte
	for { // _switch:
		switch l.Lookahead {
		case ',', '.', ':', '*', '%', '>', '<', '=', '{', '[', '|', '&', '/':
			return entryReturn, false
		// Insert a semicolon before `--` and `++`, but not before binary `+` or `-`.
		// Insert before +/-{float}
		case '+':
			l.Advance(true)
			if l.Lookahead == '+' {
				return entryReturn, true
			}
			return entryReturn, iswdigit(l.Lookahead)
		case '-':
			l.Advance(true)
			if l.Lookahead == '-' {
				return entryReturn, true
			}
			return entryReturn, iswdigit(l.Lookahead)
		// Don't insert a semicolon before `!=`, but do insert one before a unary `!`.
		case '!':
			l.Advance(true)
			if l.Lookahead == 'i' && validSymbols[tokNotIs] {
				l.Advance(true)
				if l.Lookahead == 's' {
					l.Advance(true)
					if !iswalnum(l.Lookahead) {
						return entryReturn, true
					}
				}
			}
			return entryReturn, l.Lookahead != '='
		case '?':
			if validSymbols[tokQDot] {
				return entryQDot, false
			}
			return entryReturn, true
		case 'e', 'i', 'g', 's', 'p', 'a', 'f', 'o', 'l', 'v', 'n', 'c', 'b', 'w':
			for scanWords(l, modifierWords[:], &scannedWord, nil) {
				scannedWord = [maxWordSize]byte{}
				for iswspace(l.Lookahead) {
					l.Advance(true)
				}
			}

			// A word that is no modifier stays in scannedWord and is
			// looked up again here without reading input.
			index := uint8(255) // C: uint8_t index = -1
			res := scanWords(l, keywordWords[:], &scannedWord, &index)

			// If `CLASS_MEMBER_SEMI` is valid, we found a secondary constructor and so we want to insert a semi, OR
			// we found a variable named constructor whose field is being accessed
			if index == 5 {
				for iswspace(l.Lookahead) {
					l.Advance(true)
				}
				if validSymbols[tokClassMemberSemi] || l.Lookahead == '.' || l.Lookahead == '=' {
					return entryReturn, true
				}
			} else if index == 0 {
				// Ordinarily, we should not insert a semicolon if there is an `else` on the next line,
				// except for when it's a 'when entry', which has a `->` after the `else`.
				for iswspace(l.Lookahead) {
					l.Advance(true)
				}
				if l.Lookahead == '-' {
					l.Advance(true)
					if l.Lookahead == '>' {
						return entryReturn, true
					}
				}
			} else if index == 3 && (!validSymbols[tokGet] || l.Lookahead == '[') {
				// If `get` was found and the keyword is not valid, return a semi since it's being used as an identifier
				return entryReturn, true
			} else if index == 4 && (!validSymbols[tokSet] || l.Lookahead == '[' || l.Lookahead == '(' ||
				l.Lookahead == '.') {
				// If `set` was found and the keyword is not valid, return a semi since it's being used as an identifier
				if l.Lookahead == '(' && validSymbols[tokSet] {
					// skip until the closing parenthesis
					for l.Lookahead != ')' && !l.EOF() {
						l.Advance(true)
					}
					l.Advance(true)

					for iswspace(l.Lookahead) {
						if l.Lookahead == '\n' {
							return entryReturn, true
						}
						l.Advance(true)
					}
					return entryReturn, false
				}
				return entryReturn, true
			} else if index == 1 && validSymbols[tokIn] {
				// If `in` was found and this specific external keyword is valid,
				// return a semi since it's being used in a range test
				return entryReturn, true
			}
			return entryReturn, !res
		case ';':
			l.Advance(false)
			l.MarkEnd()
			return entryReturn, true
		case '@':
			if validSymbols[tokConstructor] {
				// C does not test eof here. Lookahead stays 0 at the end
				// of input and Advance does nothing there, so C never
				// returns when the input ends inside the annotation.
				// Kept: the loop below does not return either.
				for !iswspace(l.Lookahead) {
					l.Advance(true)
				}
				for iswspace(l.Lookahead) {
					l.Advance(true)
				}
				const ctor = "constructor"
				for i := 0; i < 11; i++ {
					if l.Lookahead != int32(ctor[i]) {
						return entryReturn, true
					}
					l.Advance(true)
				}
				return entryReturn, false
			}
			if validSymbols[tokGet] || validSymbols[tokSet] {
				sawParen := false
				// Same as above: no eof test, endless at the end of input.
				for (sawParen && l.Lookahead != '\n') || (!sawParen && !iswspace(l.Lookahead)) {
					l.Advance(true)
					if l.Lookahead == '(' {
						sawParen = true
					}
					if l.Lookahead == ')' {
						sawParen = false
					}
				}
				for iswspace(l.Lookahead) {
					l.Advance(true)
				}
				if l.Lookahead == '/' {
					return entryReturn, true
				}
				continue // goto _switch
			}
			return entryReturn, true

		default:
			return entryReturn, true
		}
	}
}

// tree_sitter_kotlin_external_scanner_scan
func (scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	if validSymbols[tokMultilineStringContent] {
		didAdvance := false
		l.ResultSymbol = tokMultilineStringContent
		for !l.EOF() {
			switch l.Lookahead {
			case '$':
				l.MarkEnd()
				l.Advance(false)
				if iswalpha(l.Lookahead) || l.Lookahead == '{' {
					return didAdvance
				}
				didAdvance = true
			case '"':
				l.MarkEnd()
				// 3 or 4 quotes means we're done
				l.Advance(false)
				if l.Lookahead == '"' {
					l.Advance(false)
					if l.Lookahead == '"' {
						l.Advance(false)
						if l.Lookahead == '"' {
							l.Advance(false)
						}
						return didAdvance
					}
				}
				didAdvance = true
			default:
				l.Advance(false)
				didAdvance = true
			}
		}
	}

	entry := entryStart
	if validSymbols[tokSemi] || validSymbols[tokClassMemberSemi] {
		var ret bool
		if entry, ret = scanSemi(l, validSymbols); entry == entryReturn {
			return ret
		}
	}

	if entry == entryStart {
		for iswspace(l.Lookahead) {
			l.Advance(true)
		}
	}

	// continue_not_is_from_semi lands inside both ifs: it has skipped the
	// `!` already and does not test valid_symbols[NOT_IS].
	if entry == entryNotIs || (entry == entryStart && validSymbols[tokNotIs] && l.Lookahead == '!') {
		if entry == entryStart {
			l.Advance(false)
		}
		if l.Lookahead == 'i' {
			l.Advance(false)
			if l.Lookahead == 's' {
				l.Advance(false)
				l.ResultSymbol = tokNotIs
				l.MarkEnd()
				return !iswalnum(l.Lookahead)
			}
		}
	}

	if entry <= entryNotIs && validSymbols[tokIn] {
		if l.Lookahead == 'i' {
			l.Advance(false)
			if l.Lookahead == 'n' {
				l.Advance(false)
				l.ResultSymbol = tokIn
				l.MarkEnd()
				return !iswalnum(l.Lookahead)
			}
		}
	}

	// q_dot_from_semi:
	if entry <= entryQDot && validSymbols[tokQDot] {
		for iswspace(l.Lookahead) {
			l.Advance(true)
		}
		if l.Lookahead == '?' {
			l.Advance(false)
			for iswspace(l.Lookahead) {
				l.Advance(true)
			}
			if l.Lookahead == '.' {
				l.Advance(false)
				l.ResultSymbol = tokQDot
				l.MarkEnd()
				return true
			}
		}
	}

	// comment:
	if validSymbols[tokDollar] {
		return false
	}

	if l.Lookahead == '/' {
		l.Advance(false)
		if l.Lookahead != '*' {
			return false
		}
		l.Advance(false)

		afterStar := false
		nestingDepth := uint32(1)
		for {
			switch l.Lookahead {
			case 0:
				return false
			case '*':
				l.Advance(false)
				afterStar = true
			case '/':
				if afterStar {
					l.Advance(false)
					afterStar = false
					nestingDepth--
					if nestingDepth == 0 {
						l.ResultSymbol = tokBlockComment
						l.MarkEnd()
						return true
					}
				} else {
					l.Advance(false)
					afterStar = false
					if l.Lookahead == '*' {
						nestingDepth++
						l.Advance(false)
					}
				}
			default:
				l.Advance(false)
				afterStar = false
			}
		}
	}

	return false
}
