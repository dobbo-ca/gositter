package hcl

// Port of tree-sitter-hcl v1.2.0 src/scanner.c. Statement order and the
// advance/skip/mark_end call sequence follow C on every path.
//
// skip_comment is not ported: C defines it and never calls it.

import (
	"encoding/binary"

	"github.com/dobbo-ca/gositter"
	"github.com/dobbo-ca/gositter/grammars/internal/cchar"
)

// enum TokenType
const (
	tokQuotedTemplateStart = iota
	tokQuotedTemplateEnd
	tokTemplateLiteralChunk
	tokTemplateInterpolationStart
	tokTemplateInterpolationEnd
	tokTemplateDirectiveStart
	tokTemplateDirectiveEnd
	tokHeredocIdentifier
)

// enum ContextType. uint32 because C serializes sizeof(ContextType) == 4.
const (
	ctxTemplateInterpolation uint32 = iota
	ctxTemplateDirective
	ctxQuotedTemplate
	ctxHeredocTemplate
)

// CHAR_MAX: 127, or 255 where char is unsigned (linux/arm64).
const charMax = cchar.Max

// Context
type context struct {
	typ uint32
	// String heredoc_identifier. Only bytes: C stores each code point in a
	// char, and iswalnum admits ASCII only (see iswalnum below).
	heredocIdentifier []byte
}

// Scanner
type scanner struct {
	// context_vec. Slots above len keep their identifier buffers for reuse.
	contextStack []context
}

// The isw* functions are libc and locale dependent. The parser never calls
// setlocale, so the "C" locale applies; there darwin and glibc classify only
// ASCII. Checked on darwin/arm64 against libc for every value -2..0x2FFFF.

// iswspace
func iswspace(c int32) bool { return c == ' ' || ('\t' <= c && c <= '\r') }

// iswalnum
func iswalnum(c int32) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// iswxdigit
func iswxdigit(c int32) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

// VEC_PUSH with a fresh string_new() identifier; returns the new top.
func (s *scanner) push(typ uint32) *context {
	n := len(s.contextStack)
	if n < cap(s.contextStack) {
		s.contextStack = s.contextStack[:n+1]
	} else {
		s.contextStack = append(s.contextStack, context{})
	}
	ctx := &s.contextStack[n]
	ctx.typ = typ
	ctx.heredocIdentifier = ctx.heredocIdentifier[:0]
	return ctx
}

// VEC_POP
func (s *scanner) pop() { s.contextStack = s.contextStack[:len(s.contextStack)-1] }

// serialize, tree_sitter_hcl_external_scanner_serialize
func (s *scanner) Serialize(buf []byte) int {
	size := 0

	if len(s.contextStack) > charMax {
		return 0
	}

	binary.LittleEndian.PutUint32(buf[size:], uint32(len(s.contextStack)))
	size += 4
	for i := range s.contextStack {
		ctx := &s.contextStack[i]
		if size+4+4+len(ctx.heredocIdentifier) >= gositter.SerializationBufferSize {
			return 0
		}
		if len(ctx.heredocIdentifier) > charMax {
			return 0
		}
		binary.LittleEndian.PutUint32(buf[size:], ctx.typ)
		size += 4
		binary.LittleEndian.PutUint32(buf[size:], uint32(len(ctx.heredocIdentifier)))
		size += 4
		size += copy(buf[size:], ctx.heredocIdentifier)
	}
	return size
}

// deserialize, tree_sitter_hcl_external_scanner_deserialize
func (s *scanner) Deserialize(buf []byte) {
	s.contextStack = s.contextStack[:0]

	if len(buf) == 0 {
		return
	}

	size := 0
	contextStackSize := binary.LittleEndian.Uint32(buf[size:])
	size += 4
	for j := uint32(0); j < contextStackSize; j++ {
		ctx := s.push(binary.LittleEndian.Uint32(buf[size:]))
		size += 4

		heredocIdentifierSize := int(binary.LittleEndian.Uint32(buf[size:]))
		size += 4
		ctx.heredocIdentifier = append(ctx.heredocIdentifier, buf[size:size+heredocIdentifierSize]...)
		size += heredocIdentifierSize
	}
	// C asserts size == length here; true for anything Serialize wrote.
}

// accept_inplace
func acceptInplace(l *gositter.Lexer, token gositter.Symbol) bool {
	l.ResultSymbol = token
	return true
}

// accept_and_advance
func acceptAndAdvance(l *gositter.Lexer, token gositter.Symbol) bool {
	l.Advance(false)
	return acceptInplace(l, token)
}

// consume_wxdigit
func consumeWxdigit(l *gositter.Lexer) bool {
	l.Advance(false)
	return iswxdigit(l.Lookahead)
}

// in_context_type
func (s *scanner) inContextType(typ uint32) bool {
	if len(s.contextStack) == 0 {
		return false
	}
	return s.contextStack[len(s.contextStack)-1].typ == typ
}

// in_quoted_context
func (s *scanner) inQuotedContext() bool { return s.inContextType(ctxQuotedTemplate) }

// in_heredoc_context
func (s *scanner) inHeredocContext() bool { return s.inContextType(ctxHeredocTemplate) }

// in_template_context
func (s *scanner) inTemplateContext() bool { return s.inQuotedContext() || s.inHeredocContext() }

// in_interpolation_context
func (s *scanner) inInterpolationContext() bool { return s.inContextType(ctxTemplateInterpolation) }

// in_directive_context
func (s *scanner) inDirectiveContext() bool { return s.inContextType(ctxTemplateDirective) }

// scan, tree_sitter_hcl_external_scanner_scan
func (s *scanner) Scan(l *gositter.Lexer, validSymbols []bool) bool {
	hasLeadingWhitespaceWithNewline := false
	for iswspace(l.Lookahead) {
		if l.Lookahead == '\n' {
			hasLeadingWhitespaceWithNewline = true
		}
		l.Advance(true)
	}
	if l.Lookahead == 0 {
		return false
	}
	// manage quoted context
	if validSymbols[tokQuotedTemplateStart] && !s.inQuotedContext() && l.Lookahead == '"' {
		s.push(ctxQuotedTemplate)
		return acceptAndAdvance(l, tokQuotedTemplateStart)
	}
	if validSymbols[tokQuotedTemplateEnd] && s.inQuotedContext() && l.Lookahead == '"' {
		s.pop()
		return acceptAndAdvance(l, tokQuotedTemplateEnd)
	}

	// manage template interpolations
	if validSymbols[tokTemplateInterpolationStart] && validSymbols[tokTemplateLiteralChunk] &&
		!s.inInterpolationContext() && l.Lookahead == '$' {
		l.Advance(false)
		if l.Lookahead == '{' {
			s.push(ctxTemplateInterpolation)
			return acceptAndAdvance(l, tokTemplateInterpolationStart)
		}
		// try to scan escape sequence
		if l.Lookahead == '$' {
			l.Advance(false)
			if l.Lookahead == '{' {
				// $${
				return acceptAndAdvance(l, tokTemplateLiteralChunk)
			}
		}
		return acceptInplace(l, tokTemplateLiteralChunk)
	}
	if validSymbols[tokTemplateInterpolationEnd] && s.inInterpolationContext() && l.Lookahead == '}' {
		s.pop()
		return acceptAndAdvance(l, tokTemplateInterpolationEnd)
	}

	// manage template directives
	if validSymbols[tokTemplateDirectiveStart] && validSymbols[tokTemplateLiteralChunk] &&
		!s.inDirectiveContext() && l.Lookahead == '%' {
		l.Advance(false)
		if l.Lookahead == '{' {
			s.push(ctxTemplateDirective)
			return acceptAndAdvance(l, tokTemplateDirectiveStart)
		}
		// try to scan escape sequence
		if l.Lookahead == '%' {
			l.Advance(false)
			if l.Lookahead == '{' {
				// %%{
				return acceptAndAdvance(l, tokTemplateLiteralChunk)
			}
		}
		return acceptInplace(l, tokTemplateLiteralChunk)
	}
	if validSymbols[tokTemplateDirectiveEnd] && s.inDirectiveContext() && l.Lookahead == '}' {
		s.pop()
		return acceptAndAdvance(l, tokTemplateDirectiveEnd)
	}

	// manage heredoc context
	if validSymbols[tokHeredocIdentifier] && !s.inHeredocContext() {
		ctx := s.push(ctxHeredocTemplate)
		for iswalnum(l.Lookahead) || l.Lookahead == '_' || l.Lookahead == '-' {
			// C narrows to char here; every accepted code point is ASCII.
			ctx.heredocIdentifier = append(ctx.heredocIdentifier, byte(l.Lookahead))
			l.Advance(false)
		}
		return acceptInplace(l, tokHeredocIdentifier)
	}
	if validSymbols[tokHeredocIdentifier] && s.inHeredocContext() && hasLeadingWhitespaceWithNewline {
		expectedIdentifier := s.contextStack[len(s.contextStack)-1].heredocIdentifier

		for i := 0; i < len(expectedIdentifier); i++ {
			if l.Lookahead == int32(expectedIdentifier[i]) {
				l.Advance(false)
			} else {
				return acceptInplace(l, tokTemplateLiteralChunk)
			}
		}
		// check if the identifier is on a line of its own
		l.MarkEnd()
		for iswspace(l.Lookahead) && l.Lookahead != '\n' {
			l.Advance(false)
		}
		if l.Lookahead == '\n' {
			s.pop()
			return acceptInplace(l, tokHeredocIdentifier)
		}
		l.Advance(false)
		l.MarkEnd()
		return acceptInplace(l, tokTemplateLiteralChunk)
	}
	// manage template literal chunks

	// handle template literal chunks in quoted contexts
	//
	// they may not contain newlines and may contain escape sequences
	if validSymbols[tokTemplateLiteralChunk] && s.inQuotedContext() {
		if l.Lookahead == '\\' {
			l.Advance(false)
			switch l.Lookahead {
			case '"', 'n', 'r', 't', '\\':
				return acceptAndAdvance(l, tokTemplateLiteralChunk)
			case 'u':
				for i := 0; i < 4; i++ {
					if !consumeWxdigit(l) {
						return false
					}
				}
				return acceptAndAdvance(l, tokTemplateLiteralChunk)
			case 'U':
				for i := 0; i < 8; i++ {
					if !consumeWxdigit(l) {
						return false
					}
				}
				return acceptAndAdvance(l, tokTemplateLiteralChunk)
			default:
				return false
			}
		}
	}

	// handle all other quoted template or string literal characters
	if validSymbols[tokTemplateLiteralChunk] && s.inTemplateContext() {
		return acceptAndAdvance(l, tokTemplateLiteralChunk)
	}

	// probably not handled by the external scanner
	return false
}

// tree_sitter_hcl_external_scanner_create
func newScanner() gositter.ExternalScanner { return &scanner{} }

// Reset: the state newScanner returns, with the context slots kept.
func (s *scanner) Reset() {
	c := s.contextStack[:0]
	// Give back what a huge input grew.
	if cap(c) > 16 {
		c = nil
	}
	for _, ctx := range c[:cap(c)] {
		if cap(ctx.heredocIdentifier) > 1024 {
			c = nil
			break
		}
	}
	s.contextStack = c
}
