package codegen

import (
	"fmt"
	"strings"

	"github.com/verniyyy/arkadia/internal/core"
	"github.com/verniyyy/arkadia/internal/types"
)

// irrefutable reports whether a pattern always matches a value of its type.
func irrefutable(p core.Pat) bool {
	switch p := p.(type) {
	case *core.PWild, *core.PVar:
		return true
	case *core.PAs:
		return irrefutable(p.Pat)
	case *core.PCon:
		if p.Ctor.Type.Name == types.TBool {
			return false
		}
		if len(p.Ctor.Type.Ctors) != 1 {
			return false
		}
		for _, a := range p.Args {
			if !irrefutable(a) {
				return false
			}
		}
		return true
	}
	return false
}

func (c *ctx) match(m *core.Match, d dest) {
	var scruts []val
	for _, s := range m.Scruts {
		scruts = append(scruts, c.atom(c.expr(s)))
	}
	// a single irrefutable row needs no tests
	if len(m.Rows) == 1 && m.Rows[0].Body != nil {
		ok := true
		for _, p := range m.Rows[0].Pats {
			if !irrefutable(p) {
				ok = false
			}
		}
		if ok {
			for i, p := range m.Rows[0].Pats {
				c.pattern(p, scruts[i])
			}
			c.stmt(m.Rows[0].Body, d)
			return
		}
	}
	label := "M" + c.newTmp()
	saved := c.body
	c.body = &strings.Builder{}
	c.indent++
	usedBreak := false
	for _, r := range m.Rows {
		c.row(r, scruts, d, label, &usedBreak)
	}
	c.line("panic(rt.MatchFail(%q))", m.Where)
	c.indent--
	rows := c.body.String()
	c.body = saved
	if usedBreak {
		c.line("%s:", label)
	}
	c.line("switch {")
	c.line("default:")
	c.body.WriteString(rows)
	c.line("}")
}

func (c *ctx) row(r *core.Row, scruts []val, d dest, label string, usedBreak *bool) {
	c.open("{")
	depth := 0
	for i, p := range r.Pats {
		depth += c.pattern(p, scruts[i])
	}
	finish := func() {
		if d.kind != dRet {
			c.line("break %s", label)
			*usedBreak = true
		}
	}
	if r.Body != nil {
		c.stmt(r.Body, d)
		finish()
	} else {
		for _, g := range r.Guards {
			cond := c.expr(g.Cond)
			c.open("if %s {", as(cond, "bool"))
			c.stmt(g.Body, d)
			finish()
			c.close("}")
		}
	}
	for ; depth > 0; depth-- {
		c.close("}")
	}
	c.close("}")
}

func fieldAccess(f string, arity, i int) string {
	if arity <= 3 {
		return f + "." + string(rune('A'+i))
	}
	return fmt.Sprintf("%s.F[%d]", f, i)
}

func structName(arity int) string {
	if arity <= 3 {
		return fmt.Sprintf("*rt.D%d", arity)
	}
	return "*rt.DN"
}

// pattern emits tests and bindings for p against s and returns the number
// of opened blocks.
func (c *ctx) pattern(p core.Pat, s val) int {
	switch p := p.(type) {
	case *core.PWild:
		return 0
	case *core.PVar:
		c.line("%s := %s", local(p.Name), s.code)
		c.line("_ = %s", local(p.Name))
		c.vars[p.Name] = s.typ
		return 0
	case *core.PAs:
		c.line("%s := %s", local(p.Name), s.code)
		c.line("_ = %s", local(p.Name))
		c.vars[p.Name] = s.typ
		return c.pattern(p.Pat, s)
	case *core.PLit:
		l := literal(p.Val)
		c.open("if %s == %s {", as(s, l.typ), l.code)
		return 1
	case *core.PCon:
		ci := p.Ctor
		switch ci.Type.Name {
		case types.TBool:
			if ci.Tag == 1 {
				c.open("if %s {", as(s, "bool"))
			} else {
				c.open("if !%s {", as(s, "bool"))
			}
			return 1
		case types.TUnit:
			return 0
		}
		n := 0
		if len(ci.Type.Ctors) > 1 {
			c.open("if rt.TagOf(%s) == %d {", s.code, ci.Tag)
			n++
		}
		needFields := false
		for _, a := range p.Args {
			if _, ok := a.(*core.PWild); !ok {
				needFields = true
			}
		}
		if needFields {
			f := c.newTmp()
			c.line("%s := %s.(%s)", f, s.code, structName(ci.Arity))
			for i, a := range p.Args {
				n += c.pattern(a, val{code: fieldAccess(f, ci.Arity, i), atomic: true})
			}
		}
		return n
	}
	panic(fmt.Sprintf("pattern %T", p))
}
