package core

import (
	"fmt"
	"strconv"

	"github.com/verniyyy/arkadia/internal/syntax"
	"github.com/verniyyy/arkadia/internal/types"
)

type lowerer struct {
	g    *types.Global
	prog *Program
	n    int
}

// Lower translates type checked modules into a Core program.
func Lower(g *types.Global, mods []*syntax.Module, mainMod string) *Program {
	l := &lowerer{g: g, prog: &Program{}}
	if mainMod != "" {
		l.prog.Main = mainMod + ".main"
	}
	if _, ok := g.Effects[types.PreludeMod+".Fail"]; ok {
		l.prog.FailEff = types.PreludeMod + ".Fail"
	}
	for _, m := range mods {
		l.module(m)
	}
	for _, t := range g.Tests {
		l.prog.Tests = append(l.prog.Tests, &Test{Name: t.Name, Sym: t.Sym})
	}
	return l.prog
}

func (l *lowerer) fresh(base string) string {
	l.n++
	return fmt.Sprintf("%s$%d", base, l.n)
}

func (l *lowerer) module(m *syntax.Module) {
	for _, d := range m.Decls {
		switch d := d.(type) {
		case *syntax.DFun:
			l.topBind(d.Bind)
		case *syntax.DForeign:
			v := l.g.Values[m.Name+"."+d.Name]
			l.prog.Foreigns = append(l.prog.Foreigns, &Foreign{Sym: v.Sym, Target: d.Target, Arity: v.Arity})
		case *syntax.DEffect:
			ei := l.g.Effects[m.Name+"."+d.Name]
			eff := &Effect{Name: ei.Name}
			for _, op := range ei.Ops {
				eff.Ops = append(eff.Ops, op.Name)
				l.prog.Ops = append(l.prog.Ops, &Op{Sym: op.Sym, Eff: ei.Name, Index: op.Index, Arity: op.Arity})
			}
			l.prog.Effects = append(l.prog.Effects, eff)
		case *syntax.DClass:
			for _, b := range d.Defaults {
				if b.Info != nil {
					l.topBind(b)
				}
			}
		case *syntax.DTest:
			l.prog.Funcs = append(l.prog.Funcs, &Func{Sym: d.ID, Params: []string{"unit$"}, Body: l.expr(d.Body)})
		}
	}
	for _, inst := range l.g.InstList {
		if inst.Module == m.Name {
			l.instance(inst)
		}
	}
}

func (l *lowerer) topBind(b *syntax.Bind) {
	var dictParams []string
	if b.Info != nil {
		dictParams = b.Info.DictParams
	}
	params, body := l.bindBody(b)
	all := append(append([]string{}, dictParams...), params...)
	if len(all) == 0 {
		l.prog.Values = append(l.prog.Values, &Value{Sym: b.ID, Body: body})
		return
	}
	l.prog.Funcs = append(l.prog.Funcs, &Func{Sym: b.ID, Params: all, Body: body})
}

// bindBody lowers the equations of a binding to parameters and a body.
func (l *lowerer) bindBody(b *syntax.Bind) ([]string, Expr) {
	n := len(b.Clauses[0].Params)
	if n == 0 {
		return nil, l.rhsMatch(nil, nil, b.Clauses[0].Rhs, b.P.String())
	}
	// single equation with variable patterns: bind directly
	if len(b.Clauses) == 1 && b.Clauses[0].Rhs.Body != nil {
		var params []string
		simple := true
		for _, p := range b.Clauses[0].Params {
			pv, ok := p.(*syntax.PVar)
			if !ok {
				simple = false
				break
			}
			params = append(params, pv.ID)
		}
		if simple {
			return params, l.expr(b.Clauses[0].Rhs.Body)
		}
	}
	params := make([]string, n)
	scruts := make([]Expr, n)
	for i := range params {
		params[i] = l.fresh("arg")
		scruts[i] = &Var{params[i]}
	}
	m := &Match{Scruts: scruts, Where: b.P.String()}
	for _, cl := range b.Clauses {
		var pats []Pat
		for _, p := range cl.Params {
			pats = append(pats, l.pat(p))
		}
		m.Rows = append(m.Rows, l.row(pats, cl.Rhs))
	}
	return params, m
}

func (l *lowerer) row(pats []Pat, r *syntax.Rhs) *Row {
	row := &Row{Pats: pats}
	if r.Body != nil {
		row.Body = l.expr(r.Body)
		return row
	}
	for _, g := range r.Guards {
		row.Guards = append(row.Guards, &Guard{Cond: l.expr(g.Cond), Body: l.expr(g.Body)})
	}
	return row
}

func (l *lowerer) rhsMatch(scruts []Expr, pats []Pat, r *syntax.Rhs, where string) Expr {
	if r.Body != nil && len(scruts) == 0 {
		return l.expr(r.Body)
	}
	return &Match{Scruts: scruts, Rows: []*Row{l.row(pats, r)}, Where: where}
}

// ---------------------------------------------------------------- instances

func (l *lowerer) instance(inst *types.InstInfo) {
	cl := inst.Class
	d := &Dict{Sym: inst.Sym, Params: inst.CtxParams}
	var ctxVars []Expr
	for _, p := range inst.CtxParams {
		ctxVars = append(ctxVars, &Var{p})
	}
	for _, sd := range inst.SuperDicts {
		d.Slots = append(d.Slots, l.dict(sd))
	}
	binds := map[string]*syntax.Bind{}
	if inst.Decl != nil {
		for _, b := range inst.Decl.Binds {
			binds[b.Name] = b
		}
	}
	for _, m := range cl.Methods {
		if sym, ok := inst.Methods[m.Name]; ok {
			l.topBind(binds[m.Name])
			if len(ctxVars) == 0 {
				d.Slots = append(d.Slots, &Global{sym})
			} else {
				d.Slots = append(d.Slots, &App{Fn: &Global{sym}, Args: ctxVars})
			}
			continue
		}
		d.Slots = append(d.Slots, &App{Fn: &Global{cl.Defaults[m.Name]}, Args: []Expr{&Var{"self"}}})
	}
	l.prog.Dicts = append(l.prog.Dicts, d)
}

func (l *lowerer) dict(d syntax.DictExpr) Expr {
	switch d := d.(type) {
	case *syntax.DictRef:
		if d.Sol == nil {
			panic("unsolved dictionary for " + d.Class)
		}
		return l.dict(d.Sol)
	case *syntax.DictInst:
		inst := d.Inst.(*types.InstInfo)
		if len(d.Args) == 0 {
			return &Global{inst.Sym}
		}
		var args []Expr
		for _, a := range d.Args {
			args = append(args, l.dict(a))
		}
		return &App{Fn: &Global{inst.Sym}, Args: args}
	case *syntax.DictParam:
		return &Var{d.Name}
	case *syntax.DictSuper:
		return &Select{Dict: l.dict(d.From), Slot: d.Index}
	}
	panic(fmt.Sprintf("dict: %T", d))
}

// ---------------------------------------------------------------- expressions

func (l *lowerer) varRef(v *syntax.EVar) Expr {
	ref := v.Ref
	if ref == nil {
		panic("unresolved variable " + v.Name)
	}
	var base Expr
	dicts := v.Dicts
	switch {
	case ref.Local:
		base = &Var{ref.ID}
	case ref.Method != nil:
		mi := ref.Method.(*types.MethodInfo)
		base = &Select{Dict: l.dict(dicts[0]), Slot: mi.Slot}
		dicts = dicts[1:]
	default:
		base = &Global{ref.ID}
	}
	var args []Expr
	if ref.Group != nil {
		for _, p := range ref.Group.DictParams {
			args = append(args, &Var{p})
		}
	} else {
		for _, d := range dicts {
			args = append(args, l.dict(d))
		}
	}
	if len(args) == 0 {
		return base
	}
	return &App{Fn: base, Args: args}
}

func (l *lowerer) ctorValue(ci *types.CtorInfo) Expr {
	if ci.Arity == 0 {
		return &Con{Ctor: ci}
	}
	lam := &Lam{}
	var args []Expr
	for i := 0; i < ci.Arity; i++ {
		p := l.fresh("c")
		lam.Params = append(lam.Params, p)
		args = append(args, &Var{p})
	}
	lam.Body = &Con{Ctor: ci, Args: args}
	return lam
}

func (l *lowerer) apply(f Expr, args []Expr) Expr {
	if len(args) == 0 {
		return f
	}
	if app, ok := f.(*App); ok {
		return &App{Fn: app.Fn, Args: append(append([]Expr{}, app.Args...), args...)}
	}
	return &App{Fn: f, Args: args}
}

func (l *lowerer) exprs(es []syntax.Expr) []Expr {
	out := make([]Expr, len(es))
	for i, e := range es {
		out[i] = l.expr(e)
	}
	return out
}

func (l *lowerer) tupleCtor(n int) *types.CtorInfo {
	return l.g.Types[types.TupleName(n)].Ctors[0]
}

func (l *lowerer) expr(e syntax.Expr) Expr {
	switch e := e.(type) {
	case *syntax.ELit:
		return lit(e)
	case *syntax.EVar:
		return l.varRef(e)
	case *syntax.ECon:
		return l.ctorValue(e.Con.(*types.CtorInfo))
	case *syntax.EApp:
		if c, ok := e.Fun.(*syntax.ECon); ok {
			ci := c.Con.(*types.CtorInfo)
			args := l.exprs(e.Args)
			if len(args) >= ci.Arity {
				return l.apply(&Con{Ctor: ci, Args: args[:ci.Arity]}, args[ci.Arity:])
			}
			return l.apply(l.ctorValue(ci), args)
		}
		if len(e.Masks) > 0 {
			return l.maskedApply(e)
		}
		return l.apply(l.expr(e.Fun), l.exprs(e.Args))
	case *syntax.ELam:
		return l.lambda(e.Params, e.Body, e.P.String())
	case *syntax.ELet:
		body := l.expr(e.Body)
		for i := len(e.Binds) - 1; i >= 0; i-- {
			body = l.localBind(e.Binds[i], body)
		}
		return body
	case *syntax.EBlock:
		return l.stmts(e.Stmts)
	case *syntax.EMatch:
		m := &Match{Scruts: []Expr{l.expr(e.Scrut)}, Where: e.P.String()}
		for _, a := range e.Arms {
			m.Rows = append(m.Rows, l.row([]Pat{l.pat(a.Pat)}, a.Body))
		}
		return m
	case *syntax.EHandle:
		return l.handle(e)
	case *syntax.ETuple:
		return &Con{Ctor: l.tupleCtor(len(e.Elems)), Args: l.exprs(e.Elems)}
	case *syntax.EList:
		cons, nil_ := e.Cons.(*types.CtorInfo), e.Nil.(*types.CtorInfo)
		var res Expr = &Con{Ctor: nil_}
		for i := len(e.Elems) - 1; i >= 0; i-- {
			res = &Con{Ctor: cons, Args: []Expr{l.expr(e.Elems[i]), res}}
		}
		return res
	case *syntax.EUnit:
		return &Lit{Unit{}}
	case *syntax.EBinOp:
		if e.Op.Qual == "" && e.Op.Name == "&&" {
			return &If{Cond: l.expr(e.Left), Then: l.expr(e.Right), Else: &Lit{false}}
		}
		if e.Op.Qual == "" && e.Op.Name == "||" {
			return &If{Cond: l.expr(e.Left), Then: &Lit{true}, Else: l.expr(e.Right)}
		}
		if e.Op.Ref != nil && e.Op.Ref.Ctor != nil {
			return &Con{Ctor: e.Op.Ref.Ctor.(*types.CtorInfo), Args: []Expr{l.expr(e.Left), l.expr(e.Right)}}
		}
		return l.apply(l.varRef(e.Op), []Expr{l.expr(e.Left), l.expr(e.Right)})
	case *syntax.EInterp:
		parts := l.exprs(e.Parts)
		var kept []Expr
		for _, p := range parts {
			if lt, ok := p.(*Lit); ok && lt.Val == "" {
				continue
			}
			kept = append(kept, p)
		}
		if len(kept) == 1 {
			return kept[0]
		}
		return &Concat{Parts: kept}
	case *syntax.EAnnot:
		return l.expr(e.E)
	}
	panic(fmt.Sprintf("lower: %T", e))
}

func isWildLike(p syntax.Pattern) bool {
	switch p.(type) {
	case *syntax.PWild, *syntax.PUnit:
		return true
	}
	return false
}

// maskedApply lowers an application where some steps must run with
// handlers masked. Arguments are evaluated first, outside any mask.
func (l *lowerer) maskedApply(e *syntax.EApp) Expr {
	f := l.expr(e.Fun)
	var lets []*Let
	var args []Expr
	for _, a := range e.Args {
		x := l.expr(a)
		if !isAtomic(x) {
			name := l.fresh("marg")
			lets = append(lets, &Let{Name: name, Val: x})
			x = &Var{name}
		}
		args = append(args, x)
	}
	cur := f
	start := 0
	for i := range args {
		if effs, ok := e.Masks[i]; ok {
			cur = &Mask{Effs: effs, Body: l.apply(cur, args[start:i+1])}
			start = i + 1
		}
	}
	var res Expr = l.apply(cur, args[start:])
	for i := len(lets) - 1; i >= 0; i-- {
		res = &Let{Name: lets[i].Name, Val: lets[i].Val, Body: res}
	}
	return res
}

func isAtomic(e Expr) bool {
	switch e.(type) {
	case *Var, *Global, *Lit:
		return true
	}
	return false
}

func lit(e *syntax.ELit) Expr {
	switch e.Kind {
	case syntax.LitInt:
		return &Lit{e.Int}
	case syntax.LitFloat:
		return &Lit{e.Float}
	case syntax.LitString:
		return &Lit{e.Str}
	default:
		return &Lit{[]rune(e.Str)[0]}
	}
}

func (l *lowerer) lambda(params []syntax.Pattern, body syntax.Expr, where string) Expr {
	lam := &Lam{}
	var pats []Pat
	simple := true
	for _, p := range params {
		if pv, ok := p.(*syntax.PVar); ok {
			lam.Params = append(lam.Params, pv.ID)
			pats = append(pats, &PWild{})
			continue
		}
		if isWildLike(p) {
			lam.Params = append(lam.Params, l.fresh("w"))
			pats = append(pats, &PWild{})
			continue
		}
		simple = false
		lam.Params = append(lam.Params, l.fresh("p"))
		pats = append(pats, l.pat(p))
	}
	if simple {
		lam.Body = l.expr(body)
		return lam
	}
	var scruts []Expr
	for _, p := range lam.Params {
		scruts = append(scruts, &Var{p})
	}
	lam.Body = &Match{Scruts: scruts, Rows: []*Row{{Pats: pats, Body: l.expr(body)}}, Where: where}
	return lam
}

func (l *lowerer) stmts(ss []syntax.Stmt) Expr {
	if len(ss) == 0 {
		return &Lit{Unit{}}
	}
	switch s := ss[0].(type) {
	case *syntax.SExpr:
		e := l.expr(s.E)
		if len(ss) == 1 {
			return e
		}
		return &Let{Name: "_", Val: e, Body: l.stmts(ss[1:])}
	case *syntax.SLet:
		return l.localBind(s.Bind, l.stmts(ss[1:]))
	}
	panic("stmts")
}

func (l *lowerer) localBind(b *syntax.Bind, body Expr) Expr {
	if b.Pat != nil {
		return &Match{Scruts: []Expr{l.expr(b.Clauses[0].Rhs.Body)}, Rows: []*Row{{Pats: []Pat{l.pat(b.Pat)}, Body: body}}, Where: b.P.String()}
	}
	var dictParams []string
	if b.Info != nil {
		dictParams = b.Info.DictParams
	}
	params, val := l.bindBody(b)
	all := append(append([]string{}, dictParams...), params...)
	if len(all) == 0 {
		return &Let{Name: b.ID, Val: val, Body: body}
	}
	lam := &Lam{Params: all, Body: val}
	if Mentions(val, b.ID) {
		return &LetRec{Names: []string{b.ID}, Lams: []*Lam{lam}, Body: body}
	}
	return &Let{Name: b.ID, Val: lam, Body: body}
}

// ---------------------------------------------------------------- handlers

func (l *lowerer) handle(e *syntax.EHandle) Expr {
	ei := e.Effect.(*types.EffectInfo)
	h := &Handle{Eff: ei.Name, Body: l.expr(e.Body)}
	if e.Return != nil {
		h.Ret = l.lambda(e.Return.Params, e.Return.Body, e.Return.P.String()).(*Lam)
	}
	clauses := map[int]*syntax.HandlerClause{}
	for _, c := range e.Ops {
		clauses[c.Op.(*types.OpInfo).Index] = c
	}
	tail, direct := true, true
	var tailOK []bool
	for i := range ei.Ops {
		c := clauses[i]
		lam := l.lambda(c.Params, c.Body, c.P.String()).(*Lam)
		k := lam.Params[len(lam.Params)-1]
		kpat := c.Params[len(c.Params)-1]
		usesK := false
		if pv, ok := kpat.(*syntax.PVar); ok {
			usesK = Mentions(lam.Body, pv.ID)
			k = pv.ID
		}
		h.UsesK = append(h.UsesK, usesK)
		h.Ops = append(h.Ops, lam)
		ok := false
		if usesK {
			_, ok = tailResume(lam.Body, k)
		}
		tailOK = append(tailOK, ok)
		if !ok {
			tail = false
			if usesK {
				direct = false
			}
		}
	}
	if direct {
		for i, lam := range h.Ops {
			if tailOK[i] {
				k := lam.Params[len(lam.Params)-1]
				if kp, ok := clauses[i].Params[len(clauses[i].Params)-1].(*syntax.PVar); ok {
					k = kp.ID
				}
				lam.Body, _ = tailResume(lam.Body, k)
			}
		}
		h.Tail = tail
		h.Direct = !tail
		return h
	}
	l.statePattern(h)
	return h
}

// statePattern detects state-passing handlers and fills SRet/SOps.
func (l *lowerer) statePattern(h *Handle) {
	if h.Ret == nil || len(h.Ret.Params) != 1 {
		return
	}
	inner, ok := h.Ret.Body.(*Lam)
	if !ok || len(inner.Params) != 1 {
		return
	}
	sret := &Lam{Params: []string{h.Ret.Params[0], inner.Params[0]}, Body: inner.Body}
	var sops []*Lam
	pair := l.tupleCtor(2)
	for _, op := range h.Ops {
		k := op.Params[len(op.Params)-1]
		body, ok := op.Body.(*Lam)
		if !ok || len(body.Params) != 1 {
			return
		}
		nb, ok := tailResume2(body.Body, k, pair)
		if !ok {
			return
		}
		params := append(append([]string{}, op.Params[:len(op.Params)-1]...), body.Params[0])
		sops = append(sops, &Lam{Params: params, Body: nb})
	}
	h.SRet, h.SOps = sret, sops
}

// tailResume2 rewrites `k v s` in tail position into the pair (v, s).
func tailResume2(e Expr, k string, pair *types.CtorInfo) (Expr, bool) {
	switch e := e.(type) {
	case *App:
		if v, ok := e.Fn.(*Var); ok && v.Name == k && len(e.Args) == 2 && !Mentions(e.Args[0], k) && !Mentions(e.Args[1], k) {
			return &Con{Ctor: pair, Args: e.Args}, true
		}
	case *Let:
		if Mentions(e.Val, k) {
			return nil, false
		}
		b, ok := tailResume2(e.Body, k, pair)
		if !ok {
			return nil, false
		}
		return &Let{Name: e.Name, Val: e.Val, Body: b}, true
	case *If:
		if Mentions(e.Cond, k) {
			return nil, false
		}
		t, ok1 := tailResume2(e.Then, k, pair)
		f, ok2 := tailResume2(e.Else, k, pair)
		if !ok1 || !ok2 {
			return nil, false
		}
		return &If{Cond: e.Cond, Then: t, Else: f}, true
	case *Match:
		for _, s := range e.Scruts {
			if Mentions(s, k) {
				return nil, false
			}
		}
		m := &Match{Scruts: e.Scruts, Where: e.Where}
		for _, r := range e.Rows {
			nr := &Row{Pats: r.Pats}
			if r.Body != nil {
				b, ok := tailResume2(r.Body, k, pair)
				if !ok {
					return nil, false
				}
				nr.Body = b
			}
			for _, g := range r.Guards {
				if Mentions(g.Cond, k) {
					return nil, false
				}
				b, ok := tailResume2(g.Body, k, pair)
				if !ok {
					return nil, false
				}
				nr.Guards = append(nr.Guards, &Guard{Cond: g.Cond, Body: b})
			}
			m.Rows = append(m.Rows, nr)
		}
		return m, true
	}
	return nil, false
}

// tailResume rewrites a clause body whose only use of k is a single
// resumption in tail position, replacing `k x` by `x`.
func tailResume(e Expr, k string) (Expr, bool) {
	switch e := e.(type) {
	case *App:
		if v, ok := e.Fn.(*Var); ok && v.Name == k && len(e.Args) == 1 && !Mentions(e.Args[0], k) {
			return e.Args[0], true
		}
	case *Let:
		if Mentions(e.Val, k) {
			return nil, false
		}
		b, ok := tailResume(e.Body, k)
		if !ok {
			return nil, false
		}
		return &Let{Name: e.Name, Val: e.Val, Body: b}, true
	case *LetRec:
		for _, lam := range e.Lams {
			if Mentions(lam, k) {
				return nil, false
			}
		}
		b, ok := tailResume(e.Body, k)
		if !ok {
			return nil, false
		}
		return &LetRec{Names: e.Names, Lams: e.Lams, Body: b}, true
	case *If:
		if Mentions(e.Cond, k) {
			return nil, false
		}
		t, ok1 := tailResume(e.Then, k)
		f, ok2 := tailResume(e.Else, k)
		if !ok1 || !ok2 {
			return nil, false
		}
		return &If{Cond: e.Cond, Then: t, Else: f}, true
	case *Match:
		for _, s := range e.Scruts {
			if Mentions(s, k) {
				return nil, false
			}
		}
		m := &Match{Scruts: e.Scruts, Where: e.Where}
		for _, r := range e.Rows {
			nr := &Row{Pats: r.Pats}
			if r.Body != nil {
				b, ok := tailResume(r.Body, k)
				if !ok {
					return nil, false
				}
				nr.Body = b
			}
			for _, g := range r.Guards {
				if Mentions(g.Cond, k) {
					return nil, false
				}
				b, ok := tailResume(g.Body, k)
				if !ok {
					return nil, false
				}
				nr.Guards = append(nr.Guards, &Guard{Cond: g.Cond, Body: b})
			}
			m.Rows = append(m.Rows, nr)
		}
		return m, true
	}
	return nil, false
}

// ---------------------------------------------------------------- patterns

func (l *lowerer) pat(p syntax.Pattern) Pat {
	switch p := p.(type) {
	case *syntax.PVar:
		return &PVar{p.ID}
	case *syntax.PWild, *syntax.PUnit:
		return &PWild{}
	case *syntax.PLit:
		return &PLit{lit(p.Lit).(*Lit).Val}
	case *syntax.PAs:
		return &PAs{Name: p.ID, Pat: l.pat(p.Pat)}
	case *syntax.PAnnot:
		return l.pat(p.Pat)
	case *syntax.PTuple:
		pc := &PCon{Ctor: l.tupleCtor(len(p.Elems))}
		for _, e := range p.Elems {
			pc.Args = append(pc.Args, l.pat(e))
		}
		return pc
	case *syntax.PList:
		cons, nil_ := p.Cons.(*types.CtorInfo), p.Nil.(*types.CtorInfo)
		var res Pat = &PCon{Ctor: nil_}
		for i := len(p.Elems) - 1; i >= 0; i-- {
			res = &PCon{Ctor: cons, Args: []Pat{l.pat(p.Elems[i]), res}}
		}
		return res
	case *syntax.PCon:
		pc := &PCon{Ctor: p.Con.(*types.CtorInfo)}
		for _, a := range p.Args {
			pc.Args = append(pc.Args, l.pat(a))
		}
		return pc
	}
	panic(fmt.Sprintf("pat: %T", p))
}

// ---------------------------------------------------------------- utilities

// Mentions reports whether a local variable occurs free in e (ignoring
// shadowing, which only makes the answer conservative).
func Mentions(e Expr, name string) bool {
	found := false
	Walk(e, func(x Expr) bool {
		if found {
			return false
		}
		if v, ok := x.(*Var); ok && v.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// Walk visits e and its subexpressions in pre-order until f returns false
// for a node (its children are then skipped).
func Walk(e Expr, f func(Expr) bool) {
	if e == nil || !f(e) {
		return
	}
	switch e := e.(type) {
	case *App:
		Walk(e.Fn, f)
		for _, a := range e.Args {
			Walk(a, f)
		}
	case *Lam:
		Walk(e.Body, f)
	case *Let:
		Walk(e.Val, f)
		Walk(e.Body, f)
	case *LetRec:
		for _, lam := range e.Lams {
			Walk(lam, f)
		}
		Walk(e.Body, f)
	case *Con:
		for _, a := range e.Args {
			Walk(a, f)
		}
	case *Match:
		for _, s := range e.Scruts {
			Walk(s, f)
		}
		for _, r := range e.Rows {
			if r.Body != nil {
				Walk(r.Body, f)
			}
			for _, g := range r.Guards {
				Walk(g.Cond, f)
				Walk(g.Body, f)
			}
		}
	case *If:
		Walk(e.Cond, f)
		Walk(e.Then, f)
		Walk(e.Else, f)
	case *Select:
		Walk(e.Dict, f)
	case *Handle:
		Walk(e.Body, f)
		if e.Ret != nil {
			Walk(e.Ret, f)
		}
		for _, o := range e.Ops {
			Walk(o, f)
		}
		if e.SRet != nil {
			Walk(e.SRet, f)
		}
		for _, o := range e.SOps {
			Walk(o, f)
		}
	case *Concat:
		for _, p := range e.Parts {
			Walk(p, f)
		}
	case *Mask:
		Walk(e.Body, f)
	}
}

var _ = strconv.Itoa

// Map rebuilds e bottom-up, applying f to every node after its children.
func Map(e Expr, f func(Expr) Expr) Expr {
	switch x := e.(type) {
	case *App:
		n := &App{Fn: Map(x.Fn, f), Args: make([]Expr, len(x.Args))}
		for i, a := range x.Args {
			n.Args[i] = Map(a, f)
		}
		return f(n)
	case *Lam:
		return f(&Lam{Params: x.Params, Body: Map(x.Body, f)})
	case *Let:
		return f(&Let{Name: x.Name, Val: Map(x.Val, f), Body: Map(x.Body, f)})
	case *LetRec:
		n := &LetRec{Names: x.Names, Body: Map(x.Body, f)}
		for _, lam := range x.Lams {
			n.Lams = append(n.Lams, &Lam{Params: lam.Params, Body: Map(lam.Body, f)})
		}
		return f(n)
	case *Con:
		n := &Con{Ctor: x.Ctor, Args: make([]Expr, len(x.Args))}
		for i, a := range x.Args {
			n.Args[i] = Map(a, f)
		}
		return f(n)
	case *Match:
		n := &Match{Where: x.Where}
		for _, s := range x.Scruts {
			n.Scruts = append(n.Scruts, Map(s, f))
		}
		for _, r := range x.Rows {
			nr := &Row{Pats: r.Pats}
			if r.Body != nil {
				nr.Body = Map(r.Body, f)
			}
			for _, g := range r.Guards {
				nr.Guards = append(nr.Guards, &Guard{Cond: Map(g.Cond, f), Body: Map(g.Body, f)})
			}
			n.Rows = append(n.Rows, nr)
		}
		return f(n)
	case *If:
		return f(&If{Cond: Map(x.Cond, f), Then: Map(x.Then, f), Else: Map(x.Else, f)})
	case *Select:
		return f(&Select{Dict: Map(x.Dict, f), Slot: x.Slot})
	case *Handle:
		n := &Handle{Eff: x.Eff, Body: Map(x.Body, f), UsesK: x.UsesK, Tail: x.Tail, Direct: x.Direct}
		if x.Ret != nil {
			n.Ret = &Lam{Params: x.Ret.Params, Body: Map(x.Ret.Body, f)}
		}
		for _, o := range x.Ops {
			n.Ops = append(n.Ops, &Lam{Params: o.Params, Body: Map(o.Body, f)})
		}
		if x.SRet != nil {
			n.SRet = &Lam{Params: x.SRet.Params, Body: Map(x.SRet.Body, f)}
		}
		for _, o := range x.SOps {
			n.SOps = append(n.SOps, &Lam{Params: o.Params, Body: Map(o.Body, f)})
		}
		return f(n)
	case *Concat:
		n := &Concat{}
		for _, p := range x.Parts {
			n.Parts = append(n.Parts, Map(p, f))
		}
		return f(n)
	case *Mask:
		return f(&Mask{Effs: x.Effs, Body: Map(x.Body, f)})
	}
	return f(e)
}
