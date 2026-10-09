package main

import (
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"strconv"
)

// Self-loops. A lex state that sends a character back to itself (the body of
// an identifier, a comment, a string, a run of blanks) costs one trip through
// the state switch per character. For each state the generator works out
// which ASCII characters do that, and lex.go consumes a run of them with one
// Lexer.AdvanceRun call before the state's statements. The statements still
// run afterwards, on the first character outside the run, so what they do is
// unchanged.

// runSets returns, for one case, the ASCII characters whose first matching
// statement is ADVANCE(self) and those where it is SKIP(self). Character 0
// is never in a set: the lexer also shows 0 at end of input.
func (e *emitter) runSets(c lexCase) (adv, skip [128]bool) {
	conds := make([]ast.Expr, len(c.stmts))
	for i, s := range c.stmts {
		if s.kind != 'i' {
			continue
		}
		x, err := goparser.ParseExpr(s.cond)
		if err != nil {
			e.fail("lex state %d: condition %q: %v", c.state, s.cond, err)
		}
		conds[i] = x
	}
	for ch := int64(1); ch < 128; ch++ {
	stmts:
		for i, s := range c.stmts {
			switch s.kind {
			case 'i':
				if e.cond(conds[i], ch) {
					if s.target == c.state {
						if s.skip {
							skip[ch] = true
						} else {
							adv[ch] = true
						}
					}
					break stmts
				}
			case 'm':
				for j := 0; j < len(s.pairs); j += 2 {
					if s.pairs[j] == ch {
						adv[ch] = s.pairs[j+1] == c.state
						break stmts
					}
				}
			}
		}
	}
	return adv, skip
}

// cond evaluates a lex condition, as lexCond wrote it, for one lookahead
// that is not at end of input.
func (e *emitter) cond(x ast.Expr, lookahead int64) bool {
	switch x := x.(type) {
	case *ast.ParenExpr:
		return e.cond(x.X, lookahead)
	case *ast.UnaryExpr:
		if x.Op == gotoken.NOT {
			return !e.cond(x.X, lookahead)
		}
	case *ast.Ident:
		if x.Name == "eof" {
			return false
		}
	case *ast.CallExpr: // gositter.SetContainsASCII(&name_ascii, name[:], lookahead)
		if len(x.Args) == 3 {
			if sl, ok := x.Args[1].(*ast.SliceExpr); ok {
				if id, ok := sl.X.(*ast.Ident); ok && e.g.tables[id.Name] != nil {
					return setContains(e.g.tables[id.Name].vals, lookahead)
				}
			}
		}
	case *ast.BinaryExpr:
		switch x.Op {
		case gotoken.LAND:
			return e.cond(x.X, lookahead) && e.cond(x.Y, lookahead)
		case gotoken.LOR:
			return e.cond(x.X, lookahead) || e.cond(x.Y, lookahead)
		}
		a, b := e.value(x.X, lookahead), e.value(x.Y, lookahead)
		switch x.Op {
		case gotoken.EQL:
			return a == b
		case gotoken.NEQ:
			return a != b
		case gotoken.LSS:
			return a < b
		case gotoken.LEQ:
			return a <= b
		case gotoken.GTR:
			return a > b
		case gotoken.GEQ:
			return a >= b
		}
	}
	e.fail("lex condition: cannot evaluate %T", x)
	return false
}

func (e *emitter) value(x ast.Expr, lookahead int64) int64 {
	switch x := x.(type) {
	case *ast.ParenExpr:
		return e.value(x.X, lookahead)
	case *ast.Ident:
		if x.Name == "lookahead" {
			return lookahead
		}
	case *ast.BasicLit:
		switch x.Kind {
		case gotoken.CHAR:
			if r, _, _, err := strconv.UnquoteChar(x.Value[1:len(x.Value)-1], '\''); err == nil {
				return int64(r)
			}
		case gotoken.INT:
			if v, err := strconv.ParseInt(x.Value, 0, 64); err == nil {
				return v
			}
		}
	}
	e.fail("lex condition: cannot evaluate operand %T", x)
	return 0
}
