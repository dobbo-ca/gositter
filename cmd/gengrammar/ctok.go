package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A tokenizer for the C subset tree-sitter writes into parser.c.

type tokKind uint8

const (
	tEOF tokKind = iota
	tIdent
	tNum
	tChar
	tStr
	tPunct
)

type token struct {
	kind tokKind
	s    string // source text; for tStr the decoded bytes
	v    int64  // tNum and tChar
	pos  int
}

type tokenizer struct {
	src     string
	i       int
	defines map[string]int64 // #define NAME <integer>
}

type genError struct{ msg string }

func (t *tokenizer) fail(pos int, format string, args ...any) {
	line := 1 + strings.Count(t.src[:pos], "\n")
	panic(genError{fmt.Sprintf("parser.c:%d: %s", line, fmt.Sprintf(format, args...))})
}

func isIdentByte(c byte, first bool) bool {
	return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || !first && '0' <= c && c <= '9'
}

func (t *tokenizer) next() token {
	s := t.src
	for t.i < len(s) {
		c := s[t.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			t.i++
		case c == '#':
			end := strings.IndexByte(s[t.i:], '\n')
			if end < 0 {
				end = len(s) - t.i
			}
			if end > 0 && s[t.i+end-1] == '\\' {
				t.fail(t.i, "preprocessor line continuation")
			}
			f := strings.Fields(s[t.i : t.i+end])
			if len(f) == 3 && f[0] == "#define" {
				if v, err := strconv.ParseInt(f[2], 0, 64); err == nil {
					t.defines[f[1]] = v
				}
			}
			t.i += end
		case c == '/' && t.i+1 < len(s) && s[t.i+1] == '/':
			end := strings.IndexByte(s[t.i:], '\n')
			if end < 0 {
				end = len(s) - t.i
			}
			t.i += end
		case c == '/' && t.i+1 < len(s) && s[t.i+1] == '*':
			end := strings.Index(s[t.i+2:], "*/")
			if end < 0 {
				t.fail(t.i, "unterminated comment")
			}
			t.i += end + 4
		default:
			return t.scan()
		}
	}
	return token{kind: tEOF, pos: t.i}
}

func (t *tokenizer) scan() token {
	s, start := t.src, t.i
	c := s[start]
	switch {
	case isIdentByte(c, true):
		j := start + 1
		for j < len(s) && isIdentByte(s[j], false) {
			j++
		}
		t.i = j
		return token{kind: tIdent, s: s[start:j], pos: start}
	case '0' <= c && c <= '9':
		j := start + 1
		for j < len(s) && (isIdentByte(s[j], false)) {
			j++
		}
		v, err := strconv.ParseInt(s[start:j], 0, 64)
		// a leading 0 would be octal in C and in ParseInt alike
		if err != nil {
			t.fail(start, "bad number %q", s[start:j])
		}
		t.i = j
		return token{kind: tNum, s: s[start:j], v: v, pos: start}
	case c == '\'':
		j := start + 1
		var v int64
		if j < len(s) && s[j] == '\\' {
			var b []byte
			b, j = t.escape(j, nil)
			if len(b) != 1 {
				t.fail(start, "multi-byte character literal")
			}
			v = int64(b[0])
		} else if j < len(s) && s[j] < 0x80 && s[j] != '\'' && s[j] != '\n' {
			v = int64(s[j])
			j++
		} else {
			t.fail(start, "bad character literal")
		}
		if j >= len(s) || s[j] != '\'' {
			t.fail(start, "bad character literal")
		}
		t.i = j + 1
		return token{kind: tChar, s: s[start : j+1], v: v, pos: start}
	case c == '"':
		j := start + 1
		var b []byte
		for {
			if j >= len(s) || s[j] == '\n' {
				t.fail(start, "unterminated string")
			}
			if s[j] == '"' {
				break
			}
			if s[j] == '\\' {
				b, j = t.escape(j, b)
			} else {
				b = append(b, s[j])
				j++
			}
		}
		t.i = j + 1
		return token{kind: tStr, s: string(b), pos: start}
	}
	if start+1 < len(s) {
		switch s[start : start+2] {
		case "==", "!=", "<=", ">=", "&&", "||", "->":
			t.i = start + 2
			return token{kind: tPunct, s: s[start : start+2], pos: start}
		}
	}
	if strings.IndexByte("{}[]()<>=!,;:.&*-", c) < 0 {
		t.fail(start, "unexpected byte %q", c)
	}
	t.i = start + 1
	return token{kind: tPunct, s: s[start : start+1], pos: start}
}

// escape decodes the C escape sequence at s[j] == '\\' and appends its bytes.
func (t *tokenizer) escape(j int, b []byte) ([]byte, int) {
	s := t.src
	if j+1 >= len(s) {
		t.fail(j, "bad escape")
	}
	c := s[j+1]
	j += 2
	switch c {
	case 'a':
		return append(b, 7), j
	case 'b':
		return append(b, 8), j
	case 'e':
		return append(b, 27), j
	case 'f':
		return append(b, 12), j
	case 'n':
		return append(b, '\n'), j
	case 'r':
		return append(b, '\r'), j
	case 't':
		return append(b, '\t'), j
	case 'v':
		return append(b, 11), j
	case '\\', '\'', '"', '?':
		return append(b, c), j
	case 'x':
		k := j
		for k < len(s) && isHex(s[k]) {
			k++
		}
		v, err := strconv.ParseUint(s[j:k], 16, 8)
		if err != nil {
			t.fail(j, "bad \\x escape")
		}
		return append(b, byte(v)), k
	case 'u', 'U':
		n := 4
		if c == 'U' {
			n = 8
		}
		if j+n > len(s) {
			t.fail(j, "bad \\u escape")
		}
		v, err := strconv.ParseUint(s[j:j+n], 16, 32)
		if err != nil || !utf8.ValidRune(rune(v)) {
			t.fail(j, "bad \\u escape")
		}
		return utf8.AppendRune(b, rune(v)), j + n
	}
	if '0' <= c && c <= '7' {
		k := j
		for k < len(s) && k < j+2 && '0' <= s[k] && s[k] <= '7' {
			k++
		}
		v, err := strconv.ParseUint(s[j-1:k], 8, 8)
		if err != nil {
			t.fail(j, "bad octal escape")
		}
		return append(b, byte(v)), k
	}
	t.fail(j, "unknown escape \\%c", c)
	return nil, 0
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
