package types

import (
	"fmt"

	"github.com/verniyyy/arkadia/bootstrap/internal/diag"
	"github.com/verniyyy/arkadia/bootstrap/internal/syntax"
)

// ------------------------------------------------------------------ constraints

type wanted struct {
	cls *ClassInfo
	ty  Type
	ref *syntax.DictRef
	pos diag.Pos
}

type wantedScope struct {
	list   []*wanted
	parent *wantedScope
}

type given struct {
	cls  *ClassInfo
	ty   Type
	dict syntax.DictExpr
}

func (c *Checker) addWanted(cls *ClassInfo, t Type, pos diag.Pos) *syntax.DictRef {
	ref := &syntax.DictRef{Class: cls.Name}
	c.wanted.list = append(c.wanted.list, &wanted{cls: cls, ty: t, ref: ref, pos: pos})
	return ref
}

func (c *Checker) pushWanted() { c.wanted = &wantedScope{parent: c.wanted} }

func (c *Checker) popWanted() []*wanted {
	ws := c.wanted.list
	c.wanted = c.wanted.parent
	return ws
}

func (c *Checker) floatWanted(ws ...*wanted) {
	c.wanted.list = append(c.wanted.list, ws...)
}

var errDict = &syntax.DictParam{Name: "$error"}

func (c *Checker) noInstance(w *wanted) {
	c.errorf(w.pos, "no instance for %s %s", shortName(w.cls.Name), newPrinter().typ(Zonk(w.ty), 2))
	w.ref.Sol = errDict
}

// solve discharges constraints using instances and givens. Constraints on
// unresolved type variables are returned.
func (c *Checker) solve(ws []*wanted) []*wanted {
	var residual []*wanted
	queue := append([]*wanted(nil), ws...)
	for len(queue) > 0 {
		w := queue[0]
		queue = queue[1:]
		if w.ref.Sol != nil {
			continue
		}
		t := Prune(w.ty)
		h, args := headOf(t)
		switch h := h.(type) {
		case *TCon:
			inst := c.g.Instances[w.cls.Name][h.Name]
			if inst == nil || len(args) != inst.NVars {
				if d := c.entail(w.cls, t); d != nil {
					w.ref.Sol = d
					continue
				}
				c.noInstance(w)
				continue
			}
			di := &syntax.DictInst{Inst: inst}
			for _, p := range inst.Context {
				sub := &wanted{cls: p.Class, ty: substGen(p.Type, args), ref: &syntax.DictRef{Class: p.Class.Name}, pos: w.pos}
				di.Args = append(di.Args, sub.ref)
				queue = append(queue, sub)
			}
			w.ref.Sol = di
		case *TSkol:
			if d := c.entail(w.cls, t); d != nil {
				w.ref.Sol = d
				continue
			}
			c.errorf(w.pos, "no instance for %s %s; add the constraint `%s %s` to the type signature", shortName(w.cls.Name), TypeString(Zonk(t)), shortName(w.cls.Name), TypeString(Zonk(t)))
			w.ref.Sol = errDict
		case *Meta:
			residual = append(residual, w)
		default:
			c.noInstance(w)
		}
	}
	return residual
}

func superPath(from, to *ClassInfo) []int {
	for i, s := range from.Supers {
		if s == to {
			return []int{i}
		}
		if p := superPath(s, to); p != nil {
			return append([]int{i}, p...)
		}
	}
	return nil
}

func (c *Checker) entail(cls *ClassInfo, t Type) syntax.DictExpr {
	for i := len(c.givens) - 1; i >= 0; i-- {
		g := c.givens[i]
		if !typesEqual(g.ty, t) {
			continue
		}
		if g.cls == cls {
			return g.dict
		}
		if path := superPath(g.cls, cls); path != nil {
			d := g.dict
			for _, idx := range path {
				d = &syntax.DictSuper{From: d, Index: idx}
			}
			return d
		}
	}
	return nil
}

// defaultAmbiguous resolves a constraint on type variables that do not
// appear in any type by defaulting them to Unit.
func (c *Checker) defaultAmbiguous(w *wanted) {
	acc := map[*Meta]bool{}
	var ms []*Meta
	metasOf(w.ty, acc, &ms)
	for _, m := range ms {
		if _, ok := pruneKind(m.Kind).(KStar); ok {
			m.Ref = c.tUnit()
		}
	}
	if res := c.solve([]*wanted{w}); len(res) > 0 {
		c.errorf(w.pos, "ambiguous type: cannot determine the type for constraint %s %s", shortName(w.cls.Name), TypeString(Zonk(w.ty)))
		w.ref.Sol = errDict
	}
}

func (c *Checker) flushRoot() {
	for c.wanted.parent != nil {
		c.popWanted()
	}
	c.settle(-1)
	ws := c.wanted.list
	c.wanted.list = nil
	for _, w := range c.solve(ws) {
		c.defaultAmbiguous(w)
	}
}

// ------------------------------------------------------------------ generalization

func lowerLevels(t Type, lvl int) {
	acc := map[*Meta]bool{}
	var ms []*Meta
	metasOf(t, acc, &ms)
	for _, m := range ms {
		if m.Level > lvl {
			m.Level = lvl
		}
	}
}

func genType(t Type, idx map[*Meta]int) Type {
	t = Prune(t)
	switch t := t.(type) {
	case *Meta:
		if i, ok := idx[t]; ok {
			return &TGen{i}
		}
		return t
	case *TApp:
		return &TApp{genType(t.Fun, idx), genType(t.Arg, idx)}
	case *TFun:
		return &TFun{genType(t.Arg, idx), genType(t.Eff, idx), genType(t.Res, idx)}
	case *TRowExt:
		return &TRowExt{genType(t.Label, idx), genType(t.Rest, idx)}
	}
	return t
}

func (c *Checker) freshDictName(cls *ClassInfo) string {
	c.nextLoc++
	return fmt.Sprintf("dict_%s_%d", sanitize(shortName(cls.Name)), c.nextLoc)
}

// generalize closes the inferred types of a binding group. It must be
// called after leaving the group's level and popping its wanted scope.
func (c *Checker) generalize(monos []Type, ws []*wanted, canGen bool) ([]*Scheme, []string) {
	if canGen {
		c.settle(c.level)
	}
	if !canGen {
		for _, t := range monos {
			lowerLevels(t, c.level)
		}
		for _, w := range ws {
			lowerLevels(w.ty, c.level)
		}
		c.floatWanted(ws...)
		var scs []*Scheme
		for _, t := range monos {
			scs = append(scs, Mono(t))
		}
		return scs, nil
	}
	residual := c.solve(ws)
	monoAcc := map[*Meta]bool{}
	var monoMetas []*Meta
	for _, t := range monos {
		metasOf(t, monoAcc, &monoMetas)
	}
	var params []*wanted
	for _, w := range residual {
		acc := map[*Meta]bool{}
		var ms []*Meta
		metasOf(w.ty, acc, &ms)
		outer, inType := false, true
		for _, m := range ms {
			if m.Level <= c.level {
				outer = true
			}
			if !monoAcc[m] {
				inType = false
			}
		}
		switch {
		case outer:
			lowerLevels(w.ty, c.level)
			c.floatWanted(w)
		case !inType:
			c.defaultAmbiguous(w)
		default:
			params = append(params, w)
		}
	}
	idx := map[*Meta]int{}
	var kinds []Kind
	for _, m := range monoMetas {
		if Prune(m) != Type(m) {
			continue
		}
		if m.Level > c.level {
			idx[m] = len(kinds)
			kinds = append(kinds, m.Kind)
		}
	}
	var preds []Pred
	var names []string
	var paramWs []*wanted
outer:
	for _, w := range params {
		for i, pw := range paramWs {
			if pw.cls == w.cls && typesEqual(pw.ty, w.ty) {
				w.ref.Sol = &syntax.DictParam{Name: names[i]}
				continue outer
			}
		}
		name := c.freshDictName(w.cls)
		w.ref.Sol = &syntax.DictParam{Name: name}
		names = append(names, name)
		paramWs = append(paramWs, w)
		preds = append(preds, Pred{w.cls, genType(w.ty, idx)})
	}
	var scs []*Scheme
	for _, t := range monos {
		scs = append(scs, &Scheme{Kinds: kinds, Preds: preds, Type: genType(t, idx)})
	}
	return scs, names
}

// ------------------------------------------------------------------ bindings

func bindArity(b *syntax.Bind) int { return len(b.Clauses[0].Params) }

func isSyntacticValue(b *syntax.Bind) bool {
	if bindArity(b) > 0 {
		return true
	}
	r := b.Clauses[0].Rhs
	if r.Body == nil {
		return false
	}
	switch r.Body.(type) {
	case *syntax.ELam, *syntax.EVar, *syntax.ECon, *syntax.ELit, *syntax.EUnit:
		return true
	}
	return false
}

// checkClauses checks the equations of a binding against type t.
func (c *Checker) checkClauses(b *syntax.Bind, t Type, eff Type) {
	n := bindArity(b)
	if n == 0 {
		c.checkRhs(b.Clauses[0].Rhs, t, eff)
		return
	}
	pts := make([]Type, n)
	for i := range pts {
		pts[i] = c.freshType()
	}
	bodyEff := c.freshRow()
	r := c.freshType()
	ft := c.funType(pts, bodyEff, r)
	what := fmt.Sprintf("'%s'", b.Name)
	if err := c.unify(t, ft); err != nil {
		c.reportMismatch(b.P, t, ft, what, err)
	}
	site := matchSite{pos: b.P, what: "function " + what}
	trivial := len(b.Clauses) == 1
	for _, cl := range b.Clauses {
		saved := c.locals
		seen := map[string]bool{}
		for i, p := range cl.Params {
			c.checkPat(p, pts[i], seen)
			if !irrefutable(p) {
				trivial = false
			}
		}
		c.checkRhs(cl.Rhs, r, bodyEff)
		c.locals = saved
		site.rows = append(site.rows, cl.Params)
		site.guarded = append(site.guarded, rhsGuarded(cl.Rhs))
		if rhsGuarded(cl.Rhs) {
			trivial = false
		}
	}
	if !trivial {
		c.matches = append(c.matches, site)
	}
}

// checkPure reports effects performed while evaluating a top-level value.
func (c *Checker) checkPure(b *syntax.Bind, eff Type) {
	if bindArity(b) > 0 {
		return
	}
	if labels, _ := flattenRow(eff); len(labels) > 0 {
		var names []string
		for _, l := range labels {
			names = append(names, TypeString(l))
		}
		c.errorf(b.P, "top-level value '%s' cannot perform effects (it performs %s); make it a function taking ()", b.Name, joinNames(names))
	}
}

// checkAgainst checks a binding against a declared scheme. The first
// len(fixed) quantified variables are instantiated with fixed; the rest
// become rigid. A nil eff means a pure top-level context. Predicates (except the first when skipFirst) become
// givens; their dictionary parameter names are returned.
func (c *Checker) checkAgainst(b *syntax.Bind, sc *Scheme, fixed []Type, skipFirst bool, eff Type) []string {
	c.level++
	sub := make([]Type, len(sc.Kinds))
	copy(sub, fixed)
	for i := len(fixed); i < len(sub); i++ {
		name := ""
		if i < len(sc.Names) {
			name = sc.Names[i]
		}
		sub[i] = c.newSkolem(name, sc.Kinds[i])
	}
	t := substGen(sc.Type, sub)
	nGivens := len(c.givens)
	var names []string
	for i, p := range sc.Preds {
		if i == 0 && skipFirst {
			continue
		}
		name := c.freshDictName(p.Class)
		names = append(names, name)
		c.givens = append(c.givens, &given{cls: p.Class, ty: substGen(p.Type, sub), dict: &syntax.DictParam{Name: name}})
	}
	c.pushWanted()
	if eff == nil {
		pure := c.freshRow()
		c.checkClauses(b, t, pure)
		c.checkPure(b, pure)
	} else {
		c.checkClauses(b, t, eff)
	}
	ws := c.popWanted()
	c.settle(c.level - 1)
	for _, w := range c.solve(ws) {
		acc := map[*Meta]bool{}
		var ms []*Meta
		metasOf(w.ty, acc, &ms)
		local := true
		for _, m := range ms {
			if m.Level < c.level {
				local = false
			}
		}
		if local {
			c.defaultAmbiguous(w)
		} else {
			c.floatWanted(w)
		}
	}
	c.givens = c.givens[:nGivens]
	c.level--
	return names
}

func (c *Checker) inferLocalBind(b *syntax.Bind, eff Type) {
	if b.Pat != nil {
		t := c.inferExpr(b.Clauses[0].Rhs.Body, eff)
		c.checkPat(b.Pat, t, map[string]bool{})
		if !irrefutable(b.Pat) {
			c.matches = append(c.matches, matchSite{pos: b.P, what: "let pattern", rows: [][]syntax.Pattern{{b.Pat}}, guarded: []bool{false}})
		}
		return
	}
	b.ID = c.freshLocalID(b.Name)
	// value bindings are not recursive: in `let x = x + 1` the right-hand
	// side refers to the outer x
	recursive := bindArity(b) > 0
	if b.Sig != nil {
		sc := c.convScheme(b.Sig, newTVEnv(true, arrowAmbient))
		lv := &localVar{id: b.ID, scheme: sc, pos: b.P}
		if recursive {
			c.pushLocal(b.Name, lv)
		}
		names := c.checkAgainst(b, sc, nil, false, eff)
		if !recursive {
			c.pushLocal(b.Name, lv)
		}
		c.record(b.P, b.Name, nil, sc, b.P, "local")
		b.Info = &syntax.BindInfo{DictParams: names, Scheme: sc}
		return
	}
	gs := &groupState{info: &syntax.BindGroupInfo{}}
	saved := c.locals
	c.level++
	mono := c.freshType()
	if recursive {
		c.pushLocal(b.Name, &localVar{id: b.ID, mono: mono, group: gs, pos: b.P})
	}
	c.pushWanted()
	c.checkClauses(b, mono, eff)
	ws := c.popWanted()
	c.level--
	c.locals = saved
	scs, names := c.generalize([]Type{mono}, ws, isSyntacticValue(b))
	gs.info.DictParams = names
	b.Info = &syntax.BindInfo{DictParams: names, Group: gs.info, Scheme: scs[0]}
	if len(scs[0].Kinds) == 0 && len(names) == 0 {
		c.pushLocal(b.Name, &localVar{id: b.ID, mono: scs[0].Type, pos: b.P})
		c.record(b.P, b.Name, scs[0].Type, nil, b.P, "local")
	} else {
		c.pushLocal(b.Name, &localVar{id: b.ID, scheme: scs[0], pos: b.P})
		c.record(b.P, b.Name, nil, scs[0], b.P, "local")
	}
}

// ------------------------------------------------------------------ top level

func collectRefs(e syntax.Expr, acc map[string]bool) {
	var expr func(e syntax.Expr)
	var rhs func(r *syntax.Rhs)
	var bind func(b *syntax.Bind)
	bind = func(b *syntax.Bind) {
		for _, cl := range b.Clauses {
			rhs(cl.Rhs)
		}
	}
	rhs = func(r *syntax.Rhs) {
		if r.Body != nil {
			expr(r.Body)
		}
		for _, g := range r.Guards {
			expr(g.Cond)
			expr(g.Body)
		}
		for _, b := range r.Where {
			bind(b)
		}
	}
	expr = func(e syntax.Expr) {
		switch e := e.(type) {
		case *syntax.EVar:
			if e.Qual == "" {
				acc[e.Name] = true
			}
		case *syntax.EApp:
			expr(e.Fun)
			for _, a := range e.Args {
				expr(a)
			}
		case *syntax.ELam:
			expr(e.Body)
		case *syntax.ELet:
			for _, b := range e.Binds {
				bind(b)
			}
			expr(e.Body)
		case *syntax.EBlock:
			for _, s := range e.Stmts {
				switch s := s.(type) {
				case *syntax.SExpr:
					expr(s.E)
				case *syntax.SLet:
					bind(s.Bind)
				}
			}
		case *syntax.EMatch:
			expr(e.Scrut)
			for _, a := range e.Arms {
				rhs(a.Body)
			}
		case *syntax.EHandle:
			expr(e.Body)
			if e.Return != nil {
				expr(e.Return.Body)
			}
			for _, cl := range e.Ops {
				expr(cl.Body)
			}
		case *syntax.ETuple:
			for _, x := range e.Elems {
				expr(x)
			}
		case *syntax.EList:
			for _, x := range e.Elems {
				expr(x)
			}
		case *syntax.EBinOp:
			acc[e.Op.Name] = true
			expr(e.Left)
			expr(e.Right)
		case *syntax.EInterp:
			for _, x := range e.Parts {
				expr(x)
			}
		case *syntax.EAnnot:
			expr(e.E)
		case *syntax.EField:
			expr(e.E)
		case *syntax.ERecord:
			for _, f := range e.Fields {
				expr(f.Value)
			}
		case *syntax.EUpdate:
			expr(e.E)
			for _, f := range e.Fields {
				expr(f.Value)
			}
		}
	}
	expr(e)
}

func bindRefs(b *syntax.Bind) map[string]bool {
	acc := map[string]bool{}
	for _, cl := range b.Clauses {
		if cl.Rhs.Body != nil {
			collectRefs(cl.Rhs.Body, acc)
		}
		for _, g := range cl.Rhs.Guards {
			collectRefs(g.Cond, acc)
			collectRefs(g.Body, acc)
		}
		for _, w := range cl.Rhs.Where {
			for r := range bindRefs(w) {
				acc[r] = true
			}
		}
	}
	return acc
}

func isLocalFun(b *syntax.Bind) bool {
	return b.Name != "" && b.Pat == nil && len(b.Clauses[0].Params) > 0
}

// LocalGroups splits a sequence of local bindings into binding groups. A
// run of consecutive function bindings with distinct names is one mutually
// recursive group when one of them refers to a later one; otherwise every
// binding is a group of its own.
func LocalGroups(bs []*syntax.Bind) [][]*syntax.Bind {
	var out [][]*syntax.Bind
	for i := 0; i < len(bs); {
		seen := map[string]bool{}
		j := i
		for j < len(bs) && isLocalFun(bs[j]) && !seen[bs[j].Name] {
			seen[bs[j].Name] = true
			j++
		}
		if j-i > 1 && forwardRef(bs[i:j]) {
			out = append(out, bs[i:j])
			i = j
			continue
		}
		out = append(out, bs[i:i+1])
		i++
	}
	return out
}

func forwardRef(run []*syntax.Bind) bool {
	for k, b := range run {
		refs := bindRefs(b)
		for _, later := range run[k+1:] {
			if refs[later.Name] {
				return true
			}
		}
	}
	return false
}

// inferLocalBinds checks a sequence of local bindings (a run of block
// statements or a where clause), bringing them into scope.
func (c *Checker) inferLocalBinds(bs []*syntax.Bind, eff Type) {
	for _, g := range LocalGroups(bs) {
		if len(g) == 1 {
			c.inferLocalBind(g[0], eff)
		} else {
			c.inferLocalGroup(g, eff)
		}
	}
}

// inferLocalGroup checks mutually recursive local functions. Those with a
// signature are checked against it after the others have been inferred
// together.
func (c *Checker) inferLocalGroup(bs []*syntax.Bind, eff Type) {
	var sigd, unsig []*syntax.Bind
	for _, b := range bs {
		b.ID = c.freshLocalID(b.Name)
		if b.Sig != nil {
			sigd = append(sigd, b)
		} else {
			unsig = append(unsig, b)
		}
	}
	schemes := map[*syntax.Bind]*Scheme{}
	for _, b := range sigd {
		sc := c.convScheme(b.Sig, newTVEnv(true, arrowAmbient))
		schemes[b] = sc
		c.pushLocal(b.Name, &localVar{id: b.ID, scheme: sc, pos: b.P})
	}
	if len(unsig) > 0 {
		gs := &groupState{info: &syntax.BindGroupInfo{}}
		saved := c.locals
		c.level++
		var monos []Type
		for _, b := range unsig {
			m := c.freshType()
			monos = append(monos, m)
			c.pushLocal(b.Name, &localVar{id: b.ID, mono: m, group: gs, pos: b.P})
		}
		c.pushWanted()
		for i, b := range unsig {
			c.checkClauses(b, monos[i], eff)
		}
		ws := c.popWanted()
		c.level--
		c.locals = saved
		scs, names := c.generalize(monos, ws, true)
		gs.info.DictParams = names
		for i, b := range unsig {
			b.Info = &syntax.BindInfo{DictParams: names, Group: gs.info, Scheme: scs[i]}
			if len(scs[i].Kinds) == 0 && len(names) == 0 {
				c.pushLocal(b.Name, &localVar{id: b.ID, mono: scs[i].Type, pos: b.P})
				c.record(b.P, b.Name, scs[i].Type, nil, b.P, "local")
			} else {
				c.pushLocal(b.Name, &localVar{id: b.ID, scheme: scs[i], pos: b.P})
				c.record(b.P, b.Name, nil, scs[i], b.P, "local")
			}
		}
	}
	for _, b := range sigd {
		sc := schemes[b]
		names := c.checkAgainst(b, sc, nil, false, eff)
		c.record(b.P, b.Name, nil, sc, b.P, "local")
		b.Info = &syntax.BindInfo{DictParams: names, Scheme: sc}
	}
}

// sccs computes strongly connected components in dependency order (Tarjan).
func sccs(nodes []string, edges map[string][]string) [][]string {
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	var out [][]string
	i := 0
	var visit func(v string)
	visit = func(v string) {
		index[v] = i
		low[v] = i
		i++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges[v] {
			if _, seen := index[w]; !seen {
				visit(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && index[w] < low[v] {
				low[v] = index[w]
			}
		}
		if low[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			out = append(out, comp)
		}
	}
	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			visit(n)
		}
	}
	return out
}

func (c *Checker) checkTopBindings() {
	byName := map[string]*syntax.DFun{}
	var unsig []string
	for _, f := range c.funs {
		byName[f.Bind.Name] = f
		if vi := c.scope.Values[f.Bind.Name].(*ValueInfo); vi.Scheme == nil {
			unsig = append(unsig, f.Bind.Name)
		}
	}
	isUnsig := map[string]bool{}
	for _, n := range unsig {
		isUnsig[n] = true
	}
	edges := map[string][]string{}
	for _, n := range unsig {
		for r := range bindRefs(byName[n].Bind) {
			if isUnsig[r] {
				edges[n] = append(edges[n], r)
			}
		}
		sortStrings(edges[n])
	}
	for _, comp := range sccs(unsig, edges) {
		c.inferTopGroup(comp, byName)
	}
	for _, f := range c.funs {
		vi := c.scope.Values[f.Bind.Name].(*ValueInfo)
		if isUnsig[f.Bind.Name] {
			continue
		}
		names := c.checkAgainst(f.Bind, vi.Scheme, nil, false, nil)
		f.Bind.Info = &syntax.BindInfo{DictParams: names, Scheme: vi.Scheme}
		c.record(vi.Pos, vi.Name, nil, vi.Scheme, vi.Pos, "definition")
	}
	c.flushRoot()
}

func (c *Checker) inferTopGroup(names []string, byName map[string]*syntax.DFun) {
	gs := &groupState{info: &syntax.BindGroupInfo{}}
	var vis []*ValueInfo
	var monos []Type
	c.level++
	for _, n := range names {
		vi := c.scope.Values[n].(*ValueInfo)
		vi.group = gs
		vi.groupMono = c.freshType()
		vis = append(vis, vi)
		monos = append(monos, vi.groupMono)
	}
	c.pushWanted()
	for i, n := range names {
		eff := c.freshRow()
		c.checkClauses(byName[n].Bind, monos[i], eff)
		c.checkPure(byName[n].Bind, eff)
	}
	ws := c.popWanted()
	c.level--
	scs, dictNames := c.generalize(monos, ws, true)
	gs.info.DictParams = dictNames
	for i, vi := range vis {
		vi.group = nil
		vi.groupMono = nil
		vi.Scheme = scs[i]
		c.record(vi.Pos, vi.Name, nil, scs[i], vi.Pos, "definition")
		vi.Bind.Info = &syntax.BindInfo{DictParams: dictNames, Group: gs.info, Scheme: scs[i]}
	}
}

func (c *Checker) checkInstanceBodies() {
	for _, di := range c.insts {
		info := c.instInfo[di]
		cl := info.Class
		c.level++
		sub := make([]Type, info.NVars)
		for i := range sub {
			sub[i] = c.newSkolem(string(rune('a'+i)), info.Kinds[i])
		}
		head := substGen(info.Head, sub)
		nGivens := len(c.givens)
		info.CtxParams = nil
		for i, p := range info.Context {
			name := fmt.Sprintf("ictx%d", i)
			info.CtxParams = append(info.CtxParams, name)
			c.givens = append(c.givens, &given{cls: p.Class, ty: substGen(p.Type, sub), dict: &syntax.DictParam{Name: name}})
		}
		info.SuperDicts = nil
		for _, s := range cl.Supers {
			c.pushWanted()
			ref := c.addWanted(s, head, di.P)
			ws := c.popWanted()
			if res := c.solve(ws); len(res) > 0 {
				c.errorf(di.P, "missing superclass instance %s for this instance", shortName(s.Name))
			}
			info.SuperDicts = append(info.SuperDicts, ref)
		}
		for _, b := range di.Binds {
			mi := cl.Method(b.Name)
			if mi == nil {
				c.errorf(b.P, "'%s' is not a method of class %s", b.Name, shortName(cl.Name))
				continue
			}
			if _, dup := info.Methods[b.Name]; dup {
				c.errorf(b.P, "method '%s' is defined twice", b.Name)
				continue
			}
			b.ID = info.Sym + "$" + sanitize(b.Name)
			names := c.checkAgainst(b, mi.Scheme, []Type{head}, true, nil)
			b.Info = &syntax.BindInfo{DictParams: append(append([]string{}, info.CtxParams...), names...)}
			info.Methods[b.Name] = b.ID
		}
		for _, m := range cl.Methods {
			if _, ok := info.Methods[m.Name]; !ok {
				if _, ok := cl.Defaults[m.Name]; !ok {
					c.errorf(di.P, "instance %s %s is missing method '%s'", shortName(cl.Name), shortName(info.TyCon), m.Name)
				}
			}
		}
		c.givens = c.givens[:nGivens]
		c.level--
	}
}

func (c *Checker) checkClassDefaults() {
	for _, decl := range c.mod.Decls {
		d, ok := decl.(*syntax.DClass)
		if !ok {
			continue
		}
		ci := c.classes[d]
		for _, b := range d.Defaults {
			mi := ci.Method(b.Name)
			if mi == nil {
				continue
			}
			b.ID = ci.Defaults[b.Name]
			names := c.checkAgainst(b, mi.Scheme, nil, false, nil)
			b.Info = &syntax.BindInfo{DictParams: names}
		}
	}
}

func (c *Checker) checkTests() {
	if len(c.tests) == 0 {
		return
	}
	var row Type = emptyRow
	if io, ok := c.g.Effects[TIO]; ok {
		row = &TRowExt{&TCon{io.Name, io.Kind}, row}
	}
	if fail, ok := c.g.Effects[PreludeMod+".Fail"]; ok {
		row = &TRowExt{&TCon{fail.Name, fail.Kind}, row}
	}
	for i, d := range c.tests {
		d.ID = c.sym(fmt.Sprintf("$test%d", i))
		c.level++
		c.pushWanted()
		c.checkExpr(d.Body, c.tUnit(), row, "this test")
		ws := c.popWanted()
		c.level--
		c.settle(c.level)
		for _, w := range c.solve(ws) {
			c.defaultAmbiguous(w)
		}
		c.g.Tests = append(c.g.Tests, &TestInfo{Name: d.Name, Sym: d.ID, Module: c.modName, Decl: d})
	}
	c.flushRoot()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// CheckMain verifies that `main` exists with type Unit -> <IO> Unit.
func CheckMain(g *Global, mod string) error {
	ex := g.Modules[mod]
	if ex == nil {
		return fmt.Errorf("unknown module %s", mod)
	}
	v, ok := ex.Values["main"]
	if !ok || v.Bind == nil {
		return diag.Errorf(diag.Pos{}, "module %s does not define `main`", mod)
	}
	c := &Checker{g: g, wanted: &wantedScope{}}
	t, _ := c.instantiate(v.Scheme, v.Pos)
	io := g.Effects[TIO]
	want := &TFun{Arg: &TCon{TUnit, Star}, Eff: &TRowExt{&TCon{io.Name, io.Kind}, emptyRow}, Res: &TCon{TUnit, Star}}
	if len(v.Scheme.Preds) > 0 {
		return diag.Errorf(v.Pos, "`main` must not have class constraints")
	}
	if err := c.unify(want, t); err != nil {
		e := diag.Errorf(v.Pos, "`main` must have type Unit -> <IO> Unit, but it has type %s", SchemeString(v.Scheme))
		if ue, ok := err.(*unifyError); ok && ue.msg != "" {
			e.Notes = append(e.Notes, ue.msg)
		}
		return diag.List{e}
	}
	return nil
}
