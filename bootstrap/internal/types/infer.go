package types

import (
	"fmt"

	"github.com/arkadia-lang/arkadia/bootstrap/internal/diag"
	"github.com/arkadia-lang/arkadia/bootstrap/internal/syntax"
)

// ------------------------------------------------------------------ errors

func (c *Checker) unifyAt(pos diag.Pos, expected, actual Type, what string) {
	if err := c.unify(expected, actual); err != nil {
		c.reportMismatch(pos, expected, actual, what, err)
	}
}

func (c *Checker) reportMismatch(pos diag.Pos, expected, actual Type, what string, err error) {
	names := TypesString(Zonk(expected), Zonk(actual))
	msg := fmt.Sprintf("type mismatch: expected %s, but %s has type %s", names[0], what, names[1])
	e := diag.Errorf(pos, "%s", msg)
	if ue, ok := err.(*unifyError); ok && ue.msg != "" {
		e.Notes = append(e.Notes, ue.msg)
	}
	c.errs = append(c.errs, e)
}

// unifyEff checks that effects performed (perf) are allowed by the context.
func (c *Checker) unifyEff(pos diag.Pos, perf, ctx Type) []string {
	extra, err := c.subRow(c.openRow(perf), ctx)
	if err != nil {
		pr := newPrinter()
		pr.count(Zonk(perf))
		pr.count(Zonk(ctx))
		e := diag.Errorf(pos, "unhandled effect: this performs %s, but the surrounding context only allows %s", pr.row(Zonk(perf)), pr.row(Zonk(ctx)))
		if ue, ok := err.(*unifyError); ok && ue.msg != "" {
			e.Notes = append(e.Notes, ue.msg)
		}
		_, ptail := flattenRow(Zonk(perf))
		if _, ctail := flattenRow(Zonk(ctx)); isClosedRow(ctail) {
			if _, rigid := ptail.(*TSkol); rigid {
				e.Notes = append(e.Notes, "a function parameter with an unannotated arrow may perform any effect of the caller; if it is called inside a pure function (e.g. one stored in a data structure), require it to be pure with `<>`, as in `(a -> <> b)`, or add a row variable to the enclosing type")
			} else {
				e.Notes = append(e.Notes, "the context's effect row is closed; add a row variable to allow more effects, e.g. `<State s | e>`")
			}
		}
		c.errs = append(c.errs, e)
		return nil
	}
	var names []string
	for _, l := range extra {
		names = append(names, labelName(l))
	}
	return names
}

func isClosedRow(t Type) bool {
	_, ok := Prune(t).(*TRowEmpty)
	return ok
}

// ------------------------------------------------------------------ instantiation

func (c *Checker) instantiate(sc *Scheme, pos diag.Pos) (Type, []*syntax.DictRef) {
	if len(sc.Kinds) == 0 {
		return sc.Type, nil
	}
	sub := make([]Type, len(sc.Kinds))
	for i, k := range sc.Kinds {
		sub[i] = c.newMeta(k)
	}
	var refs []*syntax.DictRef
	for _, p := range sc.Preds {
		refs = append(refs, c.addWanted(p.Class, substGen(p.Type, sub), pos))
	}
	return substGen(sc.Type, sub), refs
}

// ------------------------------------------------------------------ expressions

func (c *Checker) checkExpr(e syntax.Expr, t Type, eff Type, what string) {
	if lam, ok := e.(*syntax.ELam); ok && c.checkLam(lam, t) {
		return
	}
	if l, ok := e.(*syntax.ELit); ok && l.Kind == syntax.LitInt {
		c.checkIntLit(l, t, what)
		return
	}
	actual := c.inferExpr(e, eff)
	c.unifyAt(e.EPos(), t, actual, what)
}

func (c *Checker) inferExpr(e syntax.Expr, eff Type) Type {
	switch e := e.(type) {
	case *syntax.ELit:
		if e.Kind == syntax.LitInt {
			return c.inferIntLit(e)
		}
		return c.litType(e)
	case *syntax.EVar:
		return c.inferVar(e)
	case *syntax.ECon:
		ci := c.lookupCtor(e.P, e.Qual, e.Name)
		if ci == nil {
			return c.freshType()
		}
		e.Con = ci
		t, _ := c.instantiate(ci.Scheme, e.P)
		c.record(e.P, e.Name, t, nil, ci.Type.Pos, "constructor")
		return t
	case *syntax.EApp:
		ft := c.inferExpr(e.Fun, eff)
		return c.applyArgs(e, e.Fun, ft, e.Args, eff)
	case *syntax.ELam:
		return c.inferLam(e.Params, e.Body)
	case *syntax.ELet:
		saved := c.locals
		for _, b := range e.Binds {
			c.inferLocalBind(b, eff)
		}
		t := c.inferExpr(e.Body, eff)
		c.locals = saved
		return t
	case *syntax.EBlock:
		return c.inferBlock(e, eff)
	case *syntax.EMatch:
		ts := c.inferExpr(e.Scrut, eff)
		r := c.freshType()
		site := matchSite{pos: e.P, what: "match"}
		for _, a := range e.Arms {
			saved := c.locals
			c.checkPat(a.Pat, ts, map[string]bool{})
			c.checkRhs(a.Body, r, eff)
			c.locals = saved
			site.rows = append(site.rows, []syntax.Pattern{a.Pat})
			site.guarded = append(site.guarded, rhsGuarded(a.Body))
		}
		c.matches = append(c.matches, site)
		return r
	case *syntax.EHandle:
		return c.inferHandle(e, eff)
	case *syntax.ETuple:
		if len(e.Elems) > 12 {
			c.errorf(e.P, "tuples may have at most 12 elements")
		}
		var ts []Type
		for _, x := range e.Elems {
			ts = append(ts, c.inferExpr(x, eff))
		}
		if len(ts) > 12 {
			return c.freshType()
		}
		return c.tTuple(ts)
	case *syntax.EList:
		a := c.freshType()
		e.Cons = c.builtinCtor(TList, 1)
		e.Nil = c.builtinCtor(TList, 0)
		for _, x := range e.Elems {
			c.checkExpr(x, a, eff, "this list element")
		}
		return c.tList(a)
	case *syntax.EUnit:
		return c.tUnit()
	case *syntax.EBinOp:
		return c.inferBinOp(e, eff)
	case *syntax.EInterp:
		for _, p := range e.Parts {
			c.checkExpr(p, c.tString(), eff, "this interpolated part")
		}
		return c.tString()
	case *syntax.EAnnot:
		env := newTVEnv(false, arrowFresh)
		if len(e.Type.Preds) > 0 {
			c.errorf(e.P, "constraints are not allowed in expression annotations")
		}
		t := c.convStar(e.Type.Type, env)
		c.checkExpr(e.E, t, eff, "this expression")
		return t
	case *syntax.EField:
		return c.inferField(e, eff)
	case *syntax.ERecord:
		return c.inferRecord(e, eff)
	case *syntax.EUpdate:
		return c.inferUpdate(e, eff)
	case *syntax.ENeg, *syntax.ESection, *syntax.EOpChain:
		panic(fmt.Sprintf("unexpected %T after desugaring", e))
	}
	panic(fmt.Sprintf("inferExpr: %T", e))
}

func (c *Checker) litType(e *syntax.ELit) Type {
	switch e.Kind {
	case syntax.LitInt:
		return c.tInt()
	case syntax.LitFloat:
		return c.tFloat()
	case syntax.LitString:
		return c.tString()
	default:
		return c.tChar()
	}
}

func (c *Checker) inferVar(e *syntax.EVar) Type {
	t, def, kind := c.inferVarInner(e)
	name := e.Name
	if e.Qual != "" && e.Qual[0] != '#' {
		name = e.Qual + "." + e.Name
	}
	c.record(e.P, name, t, nil, def, kind)
	return t
}

func (c *Checker) inferVarInner(e *syntax.EVar) (Type, diag.Pos, string) {
	if e.Qual == "" {
		if lv := c.lookupLocal(e.Name); lv != nil {
			e.Ref = &syntax.VarRef{Local: true, ID: lv.id}
			if lv.group != nil {
				e.Ref.Group = lv.group.info
			}
			if lv.scheme == nil {
				return c.openFun(lv.mono), lv.pos, "local"
			}
			t, refs := c.instantiate(lv.scheme, e.P)
			e.Dicts = refs
			return c.openFun(t), lv.pos, "local"
		}
	}
	v := c.lookupValue(e.P, e.Qual, e.Name)
	if v == nil {
		return c.freshType(), diag.Pos{}, ""
	}
	kind := "value"
	switch {
	case v.Method != nil:
		kind = "method"
	case v.Op != nil:
		kind = "operation"
	}
	e.Ref = &syntax.VarRef{ID: v.Sym}
	if v.Method != nil {
		e.Ref.Method = v.Method
	}
	if v.Op != nil {
		e.Ref.Op = v.Op
	}
	if v.group != nil {
		e.Ref.Group = v.group.info
		return c.openFun(v.groupMono), v.Pos, kind
	}
	if v.Scheme == nil {
		// reference to a binding whose type is not known yet (should not
		// happen thanks to dependency ordering)
		c.errorf(e.P, "internal error: '%s' used before its type is known", e.Name)
		return c.freshType(), v.Pos, kind
	}
	t, refs := c.instantiate(v.Scheme, e.P)
	e.Dicts = refs
	return c.openFun(t), v.Pos, kind
}

func (c *Checker) applyArgs(app *syntax.EApp, fun syntax.Expr, ft Type, args []syntax.Expr, eff Type) Type {
	effFailed := false
	for i, a := range args {
		ft = Prune(ft)
		if f, ok := ft.(*TFun); ok {
			// check the argument before the call's effect: arguments are
			// evaluated first and may determine the callee's effect row
			c.checkExpr(a, f.Arg, eff, "this argument")
			if effFailed {
				// report an effect error once per application
				c.subRow(c.openRow(f.Eff), eff)
				ft = f.Res
				continue
			}
			nerr := len(c.errs)
			masks := c.unifyEff(a.EPos(), f.Eff, eff)
			effFailed = len(c.errs) > nerr
			if len(masks) > 0 {
				if app == nil {
					c.errorf(a.EPos(), "internal limitation: effect masking is not supported for operators; use a function application")
				} else {
					if app.Masks == nil {
						app.Masks = map[int][]string{}
					}
					app.Masks[i] = masks
				}
			}
			ft = f.Res
			continue
		}
		if _, ok := ft.(*Meta); !ok {
			if i == 0 {
				c.errorf(fun.EPos(), "this is not a function; it has type %s", TypeString(Zonk(ft)))
			} else {
				c.errorf(a.EPos(), "too many arguments: the function result has type %s", TypeString(Zonk(ft)))
			}
			for _, x := range args[i:] {
				c.inferExpr(x, eff)
			}
			return c.freshType()
		}
		at := c.inferExpr(a, eff)
		r := c.freshType()
		c.unifyAt(fun.EPos(), ft, &TFun{Arg: at, Eff: eff, Res: r}, "the function")
		ft = r
	}
	return ft
}

func (c *Checker) inferBinOp(e *syntax.EBinOp, eff Type) Type {
	if e.Op.Qual == "" && (e.Op.Name == "&&" || e.Op.Name == "||") {
		c.checkExpr(e.Left, c.tBool(), eff, "the left operand")
		c.checkExpr(e.Right, c.tBool(), eff, "the right operand")
		return c.tBool()
	}
	var ft Type
	if len(e.Op.Name) > 0 && e.Op.Name[0] == ':' {
		ci := c.lookupCtor(e.Op.P, e.Op.Qual, e.Op.Name)
		if ci == nil {
			return c.freshType()
		}
		e.Op.Ref = &syntax.VarRef{Ctor: ci}
		ft, _ = c.instantiate(ci.Scheme, e.P)
	} else {
		ft = c.inferVar(e.Op)
	}
	return c.applyArgs(nil, e.Op, ft, []syntax.Expr{e.Left, e.Right}, eff)
}

func (c *Checker) inferLam(params []syntax.Pattern, body syntax.Expr) Type {
	saved := c.locals
	seen := map[string]bool{}
	var pts []Type
	for _, p := range params {
		t := c.freshType()
		c.checkPat(p, t, seen)
		pts = append(pts, t)
	}
	bodyEff := c.freshRow()
	r := c.inferExpr(body, bodyEff)
	c.locals = saved
	c.recordParamMatch(params)
	return c.funType(pts, bodyEff, r)
}

// checkLam checks a lambda against a known function type, propagating
// parameter types and the body's effect context inwards. It returns false
// when the expected type is not (yet) a function type of enough arity.
func (c *Checker) checkLam(lam *syntax.ELam, t Type) bool {
	var arrows []*TFun
	cur := t
	for range lam.Params {
		f, ok := Prune(cur).(*TFun)
		if !ok {
			return false
		}
		arrows = append(arrows, f)
		cur = f.Res
	}
	saved := c.locals
	seen := map[string]bool{}
	for i, p := range lam.Params {
		c.checkPat(p, arrows[i].Arg, seen)
	}
	last := arrows[len(arrows)-1]
	c.checkExpr(lam.Body, last.Res, last.Eff, "the body of this function")
	c.locals = saved
	c.recordParamMatch(lam.Params)
	return true
}

// funType builds a curried function type; only the last arrow carries the
// body's effect.
func (c *Checker) funType(params []Type, bodyEff, res Type) Type {
	t := res
	for i := len(params) - 1; i >= 0; i-- {
		eff := bodyEff
		if i != len(params)-1 {
			eff = c.freshRow()
		}
		t = &TFun{Arg: params[i], Eff: eff, Res: t}
	}
	return t
}

func (c *Checker) recordParamMatch(params []syntax.Pattern) {
	refutable := false
	for _, p := range params {
		if !irrefutable(p) {
			refutable = true
		}
	}
	if refutable {
		c.matches = append(c.matches, matchSite{pos: params[0].PPos(), what: "lambda", rows: [][]syntax.Pattern{params}, guarded: []bool{false}})
	}
}

func irrefutable(p syntax.Pattern) bool {
	switch p := p.(type) {
	case *syntax.PVar, *syntax.PWild, *syntax.PUnit:
		return true
	case *syntax.PTuple:
		for _, e := range p.Elems {
			if !irrefutable(e) {
				return false
			}
		}
		return true
	case *syntax.PAs:
		return irrefutable(p.Pat)
	case *syntax.PAnnot:
		return irrefutable(p.Pat)
	}
	return false
}

func (c *Checker) inferBlock(e *syntax.EBlock, eff Type) Type {
	saved := c.locals
	defer func() { c.locals = saved }()
	var result Type = c.tUnit()
	for i := 0; i < len(e.Stmts); i++ {
		s := e.Stmts[i]
		last := i == len(e.Stmts)-1
		switch s := s.(type) {
		case *syntax.SLet:
			if last {
				c.errorf(s.SPos(), "a block must end with an expression, not a let binding")
				return c.freshType()
			}
			binds, n := letRun(e.Stmts[i:])
			c.inferLocalBinds(binds, eff)
			i += n - 1
		case *syntax.SExpr:
			t := c.inferExpr(s.E, eff)
			if last {
				result = t
			} else if err := c.unify(c.tUnit(), t); err != nil {
				c.errorf(s.E.EPos(), "the result of this statement (of type %s) is discarded; use `let _ = ...` to ignore it explicitly", TypeString(Zonk(t)))
			}
		}
	}
	return result
}

// letRun returns the bindings of the let statements at the start of ss,
// excluding a final statement (a block must end with an expression).
func letRun(ss []syntax.Stmt) ([]*syntax.Bind, int) {
	var bs []*syntax.Bind
	for _, s := range ss[:len(ss)-1] {
		l, ok := s.(*syntax.SLet)
		if !ok {
			break
		}
		bs = append(bs, l.Bind)
	}
	return bs, len(bs)
}

func rhsGuarded(r *syntax.Rhs) bool {
	if r.Guards == nil {
		return false
	}
	last := r.Guards[len(r.Guards)-1]
	return !isTrivialGuard(last.Cond)
}

func isTrivialGuard(e syntax.Expr) bool {
	switch e := e.(type) {
	case *syntax.EVar:
		return e.Name == "otherwise"
	case *syntax.ECon:
		return e.Name == "True"
	}
	return false
}

func (c *Checker) checkRhs(r *syntax.Rhs, t Type, eff Type) {
	if r.Where != nil {
		saved := c.locals
		defer func() { c.locals = saved }()
		c.inferLocalBinds(r.Where, eff)
	}
	if r.Body != nil {
		c.checkExpr(r.Body, t, eff, "this expression")
		return
	}
	for _, g := range r.Guards {
		c.checkExpr(g.Cond, c.tBool(), eff, "this guard")
		c.checkExpr(g.Body, t, eff, "this expression")
	}
}

// ------------------------------------------------------------------ handlers

func (c *Checker) inferHandle(e *syntax.EHandle, eff Type) Type {
	var ei *EffectInfo
	handled := map[string]bool{}
	for _, cl := range e.Ops {
		v := c.lookupValue(cl.P, "", cl.Name)
		if v == nil {
			continue
		}
		if v.Op == nil {
			c.errorf(cl.P, "'%s' is not an effect operation", cl.Name)
			continue
		}
		if ei == nil {
			ei = v.Op.Effect
		} else if ei != v.Op.Effect {
			c.errorf(cl.P, "a handler can only handle operations of one effect ('%s' belongs to %s, not %s)", cl.Name, shortName(v.Op.Effect.Name), shortName(ei.Name))
			continue
		}
		if handled[cl.Name] {
			c.errorf(cl.P, "operation '%s' is handled twice", cl.Name)
		}
		handled[cl.Name] = true
		cl.Op = v.Op
	}
	if ei == nil {
		if len(e.Ops) == 0 {
			c.errorf(e.P, "a handler must handle at least one operation")
		}
		c.inferExpr(e.Body, eff)
		return c.freshType()
	}
	e.Effect = ei
	for _, op := range ei.Ops {
		if !handled[op.Name] {
			c.errorf(e.P, "handler for %s does not handle operation '%s'", shortName(ei.Name), op.Name)
		}
	}
	// instantiate effect parameters
	args := make([]Type, ei.NParams)
	pks := paramKinds(ei.Kind, ei.NParams)
	for i := range args {
		args[i] = c.newMeta(pks[i])
	}
	label := mkApp(&TCon{ei.Name, ei.Kind}, args...)
	bodyT := c.inferExpr(e.Body, &TRowExt{label, eff})
	res := c.freshType()
	if e.Return != nil {
		saved := c.locals
		c.checkPat(e.Return.Params[0], bodyT, map[string]bool{})
		c.checkExpr(e.Return.Body, res, eff, "the return clause")
		c.locals = saved
	} else {
		c.unifyAt(e.P, res, bodyT, "the handled expression")
	}
	for _, cl := range e.Ops {
		op, ok := cl.Op.(*OpInfo)
		if !ok {
			continue
		}
		if len(cl.Params) != op.Arity+1 {
			c.errorf(cl.P, "operation '%s' takes %d argument(s) plus the continuation, but the clause has %d pattern(s)", op.Name, op.Arity, len(cl.Params))
			continue
		}
		c.level++
		sub := make([]Type, len(op.Scheme.Kinds))
		copy(sub, args)
		for i := ei.NParams; i < len(sub); i++ {
			if i == op.Ambient {
				sub[i] = eff
				continue
			}
			name := ""
			if i < len(op.Scheme.Names) {
				name = op.Scheme.Names[i]
			}
			sub[i] = c.newSkolem(name, op.Scheme.Kinds[i])
		}
		saved := c.locals
		seen := map[string]bool{}
		for i, pt := range op.Params {
			c.checkPat(cl.Params[i], substGen(pt, sub), seen)
		}
		k := &TFun{Arg: substGen(op.Res, sub), Eff: eff, Res: res}
		c.checkPat(cl.Params[op.Arity], k, seen)
		c.checkExpr(cl.Body, res, eff, "this handler clause")
		c.locals = saved
		c.level--
		c.recordParamMatch(cl.Params)
	}
	return res
}

// ------------------------------------------------------------------ patterns

func (c *Checker) bindPatVar(pos diag.Pos, name string, t Type, seen map[string]bool) string {
	if seen[name] {
		c.errorf(pos, "variable '%s' is bound more than once in this pattern", name)
	}
	seen[name] = true
	id := c.freshLocalID(name)
	c.pushLocal(name, &localVar{id: id, mono: t, pos: pos})
	c.record(pos, name, t, nil, pos, "local")
	return id
}

func (c *Checker) checkPat(p syntax.Pattern, t Type, seen map[string]bool) {
	switch p := p.(type) {
	case *syntax.PVar:
		p.ID = c.bindPatVar(p.P, p.Name, t, seen)
	case *syntax.PWild:
	case *syntax.PUnit:
		c.unifyAt(p.P, t, c.tUnit(), "this pattern")
	case *syntax.PLit:
		c.unifyAt(p.P, t, c.litType(p.Lit), "this pattern")
	case *syntax.PAs:
		p.ID = c.bindPatVar(p.P, p.Name, t, seen)
		c.checkPat(p.Pat, t, seen)
	case *syntax.PAnnot:
		env := newTVEnv(false, arrowFresh)
		at := c.convStar(p.Type, env)
		c.unifyAt(p.P, at, t, "this pattern")
		c.checkPat(p.Pat, t, seen)
	case *syntax.PTuple:
		var ts []Type
		for range p.Elems {
			ts = append(ts, c.freshType())
		}
		if len(ts) > 12 {
			c.errorf(p.P, "tuples may have at most 12 elements")
			return
		}
		c.unifyAt(p.P, t, c.tTuple(ts), "this pattern")
		for i, e := range p.Elems {
			c.checkPat(e, ts[i], seen)
		}
	case *syntax.PList:
		a := c.freshType()
		p.Cons = c.builtinCtor(TList, 1)
		p.Nil = c.builtinCtor(TList, 0)
		c.unifyAt(p.P, t, c.tList(a), "this pattern")
		for _, e := range p.Elems {
			c.checkPat(e, a, seen)
		}
	case *syntax.PCon:
		ci := c.lookupCtor(p.P, p.Qual, p.Name)
		if ci == nil {
			for _, a := range p.Args {
				c.checkPat(a, c.freshType(), seen)
			}
			return
		}
		p.Con = ci
		if p.Record && !c.recordPatArgs(p, ci) {
			for _, a := range p.Args {
				c.checkPat(a, c.freshType(), seen)
			}
			return
		}
		if len(p.Args) != ci.Arity {
			c.errorf(p.P, "constructor '%s' expects %d argument(s), but the pattern has %d", p.Name, ci.Arity, len(p.Args))
			return
		}
		ct, _ := c.instantiate(ci.Scheme, p.P)
		var fields []Type
		for i := 0; i < ci.Arity; i++ {
			f := Prune(ct).(*TFun)
			fields = append(fields, f.Arg)
			ct = f.Res
		}
		c.unifyAt(p.P, t, ct, "this pattern")
		for i, a := range p.Args {
			c.checkPat(a, fields[i], seen)
		}
	default:
		panic(fmt.Sprintf("checkPat: %T", p))
	}
}
