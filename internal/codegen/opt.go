package codegen

import (
	"fmt"

	"github.com/verniyyy/arkadia/internal/core"
)

// Optimize performs simple whole-program simplifications on Core:
// dictionary method selection on statically known instances, alias
// chasing, inlining of tiny wrapper functions, and substitution of
// trivially bound variables.
func Optimize(p *core.Program) {
	o := &optimizer{
		aliases: map[string]string{},
		dicts:   map[string]*core.Dict{},
		small:   map[string]*core.Func{},
	}
	for _, v := range p.Values {
		if g, ok := v.Body.(*core.Global); ok {
			o.aliases[v.Sym] = g.Sym
		}
	}
	for _, d := range p.Dicts {
		o.dicts[d.Sym] = d
	}
	for _, f := range p.Funcs {
		if isSmall(f) {
			o.small[f.Sym] = f
		}
	}
	for round := 0; round < 3; round++ {
		for _, f := range p.Funcs {
			f.Body = o.expr(f.Body)
		}
		for _, v := range p.Values {
			v.Body = o.expr(v.Body)
		}
		for _, d := range p.Dicts {
			for i, s := range d.Slots {
				d.Slots[i] = o.expr(s)
			}
		}
	}
}

type optimizer struct {
	aliases map[string]string
	dicts   map[string]*core.Dict
	small   map[string]*core.Func
	n       int
}

func (o *optimizer) resolve(sym string) string {
	for i := 0; i < 20; i++ {
		t, ok := o.aliases[sym]
		if !ok {
			return sym
		}
		sym = t
	}
	return sym
}

// isSmall reports whether a function body is a tiny expression worth
// inlining (wrappers produced by dictionary passing).
func isSmall(f *core.Func) bool {
	size := 0
	ok := true
	core.Walk(f.Body, func(e core.Expr) bool {
		size++
		switch x := e.(type) {
		case *core.App, *core.Select, *core.Var, *core.Lit:
		case *core.Global:
			if x.Sym == f.Sym {
				ok = false
			}
		default:
			ok = false
		}
		return ok
	})
	return ok && size <= 8
}

func isAtom(e core.Expr) bool {
	switch e.(type) {
	case *core.Var, *core.Global, *core.Lit:
		return true
	}
	return false
}

func subst(e core.Expr, env map[string]core.Expr) core.Expr {
	return core.Map(e, func(x core.Expr) core.Expr {
		if v, ok := x.(*core.Var); ok {
			if r, ok := env[v.Name]; ok {
				return r
			}
		}
		return x
	})
}

func (o *optimizer) expr(e core.Expr) core.Expr {
	return core.Map(e, o.node)
}

func (o *optimizer) node(e core.Expr) core.Expr {
	switch x := e.(type) {
	case *core.Global:
		if t := o.resolve(x.Sym); t != x.Sym {
			return &core.Global{Sym: t}
		}
	case *core.App:
		if inner, ok := x.Fn.(*core.App); ok {
			return o.node(&core.App{Fn: inner.Fn, Args: append(append([]core.Expr{}, inner.Args...), x.Args...)})
		}
		if g, ok := x.Fn.(*core.Global); ok {
			if f, ok := o.small[g.Sym]; ok && len(x.Args) >= len(f.Params) {
				return o.inline(f, x.Args)
			}
		}
	case *core.Select:
		if slot := o.knownSlot(x); slot != nil {
			return slot
		}
	case *core.Let:
		if isAtom(x.Val) && x.Name != "_" {
			return o.expr(subst(x.Body, map[string]core.Expr{x.Name: x.Val}))
		}
	}
	return e
}

func (o *optimizer) inline(f *core.Func, args []core.Expr) core.Expr {
	env := map[string]core.Expr{}
	var lets []*core.Let
	for i, p := range f.Params {
		a := args[i]
		if isAtom(a) {
			env[p] = a
			continue
		}
		o.n++
		name := fmt.Sprintf("%s$i%d", p, o.n)
		env[p] = &core.Var{Name: name}
		lets = append(lets, &core.Let{Name: name, Val: a})
	}
	var body core.Expr = subst(f.Body, env)
	if extra := args[len(f.Params):]; len(extra) > 0 {
		body = &core.App{Fn: body, Args: extra}
	}
	body = o.expr(body)
	for i := len(lets) - 1; i >= 0; i-- {
		body = &core.Let{Name: lets[i].Name, Val: lets[i].Val, Body: body}
	}
	return body
}

// knownSlot resolves a dictionary selection when the dictionary is known.
func (o *optimizer) knownSlot(s *core.Select) core.Expr {
	switch d := s.Dict.(type) {
	case *core.Global:
		dict, ok := o.dicts[d.Sym]
		if !ok || len(dict.Params) > 0 || s.Slot >= len(dict.Slots) {
			return nil
		}
		if g, ok := dict.Slots[s.Slot].(*core.Global); ok {
			return &core.Global{Sym: o.resolve(g.Sym)}
		}
	case *core.App:
		g, ok := d.Fn.(*core.Global)
		if !ok {
			return nil
		}
		dict, ok := o.dicts[g.Sym]
		if !ok || len(dict.Params) != len(d.Args) || s.Slot >= len(dict.Slots) {
			return nil
		}
		for _, a := range d.Args {
			if !isAtom(a) {
				return nil
			}
		}
		slot := dict.Slots[s.Slot]
		if core.Mentions(slot, "self") {
			return nil
		}
		ok = true
		core.Walk(slot, func(e core.Expr) bool {
			switch e.(type) {
			case *core.App, *core.Global, *core.Var, *core.Select, *core.Lit:
			default:
				ok = false
			}
			return ok
		})
		if !ok {
			return nil
		}
		env := map[string]core.Expr{}
		for i, p := range dict.Params {
			env[p] = d.Args[i]
		}
		return subst(slot, env)
	}
	return nil
}
