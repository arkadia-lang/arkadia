package types

import "github.com/verniyyy/arkadia/bootstrap/internal/syntax"

// ------------------------------------------------------------------ integer literals
//
// An integer literal is an Int, or a Float where a Float is expected
// (`x * 2` with x : Float). When its type is not known yet, the decision
// waits like a field constraint: it is retried before the enclosing
// binding is generalized, and a literal whose type is still open then
// becomes an Int. So inferred types are the same as if literals were
// always Int, except where that would be a type error.

type litWanted struct {
	lit *syntax.ELit
	t   Type
}

func (c *Checker) inferIntLit(e *syntax.ELit) Type {
	t := c.freshType()
	c.markLit(t.(*Meta))
	c.lits = append(c.lits, &litWanted{lit: e, t: t})
	return t
}

func (c *Checker) markLit(m *Meta) {
	if c.litMetas == nil {
		c.litMetas = map[*Meta]bool{}
	}
	c.litMetas[m] = true
}

// checkIntLit checks a literal against an expected type.
func (c *Checker) checkIntLit(e *syntax.ELit, t Type, what string) {
	switch tt := Prune(t).(type) {
	case *Meta:
		c.markLit(tt)
		c.lits = append(c.lits, &litWanted{lit: e, t: t})
		return
	case *TCon:
		if tt.Name == TFloat {
			toFloatLit(e)
			return
		}
	}
	c.unifyAt(e.P, t, c.tInt(), what)
}

func toFloatLit(e *syntax.ELit) {
	e.Kind = syntax.LitFloat
	e.Float = float64(e.Int)
}

// settle retries the deferred decisions (fields, then literals) whose
// types may have become known; see settleFields for lvl.
func (c *Checker) settle(lvl int) {
	c.settleFields(lvl)
	c.settleLits(lvl)
}

func (c *Checker) settleLits(lvl int) {
	var rest []*litWanted
	for _, w := range c.lits {
		switch t := Prune(w.t).(type) {
		case *Meta:
			if t.Level > lvl {
				c.unifyAt(w.lit.P, w.t, c.tInt(), "this integer literal")
			} else {
				rest = append(rest, w)
			}
		case *TCon:
			switch t.Name {
			case TFloat:
				toFloatLit(w.lit)
			case TInt:
			default:
				c.unifyAt(w.lit.P, w.t, c.tInt(), "this integer literal")
			}
		default:
			c.unifyAt(w.lit.P, w.t, c.tInt(), "this integer literal")
		}
	}
	c.lits = rest
}
