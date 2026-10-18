package syntax

import "github.com/arkadia-lang/arkadia/bootstrap/internal/diag"

// Fixity of an infix operator.
type Fixity struct {
	Assoc int // 0 none, 1 left, 2 right
	Prec  int
}

// Fixities maps operator names to their fixity.
type Fixities map[string]Fixity

func DefaultFixities() Fixities {
	return Fixities{
		"::": {2, 5},
		"&&": {2, 3},
		"||": {2, 2},
	}
}

// CollectFixities adds the fixity declarations of a module.
func (f Fixities) Collect(m *Module) error {
	var errs diag.List
	for _, d := range m.Decls {
		if fd, ok := d.(*DFixity); ok {
			for _, op := range fd.Ops {
				if old, ok := f[op]; ok && old != (Fixity{fd.Assoc, fd.Prec}) {
					errs = append(errs, diag.Errorf(fd.P, "conflicting fixity declaration for %s", op))
				}
				f[op] = Fixity{fd.Assoc, fd.Prec}
			}
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func (f Fixities) get(op string) Fixity {
	if fx, ok := f[op]; ok {
		return fx
	}
	return Fixity{1, 9}
}

type fixResolver struct {
	fx   Fixities
	errs diag.List
	n    int
}

// ResolveFixity rewrites every operator chain of the module into binary
// operator applications according to the fixity table.
func ResolveFixity(m *Module, fx Fixities) error {
	r := &fixResolver{fx: fx}
	for _, d := range m.Decls {
		switch d := d.(type) {
		case *DFun:
			r.bind(d.Bind)
		case *DClass:
			for _, b := range d.Defaults {
				r.bind(b)
			}
		case *DInstance:
			for _, b := range d.Binds {
				r.bind(b)
			}
		case *DTest:
			d.Body = r.expr(d.Body)
		}
	}
	if len(r.errs) > 0 {
		return r.errs
	}
	return nil
}

func (r *fixResolver) bind(b *Bind) {
	for _, c := range b.Clauses {
		r.rhs(c.Rhs)
	}
}

func (r *fixResolver) rhs(rh *Rhs) {
	if rh.Body != nil {
		rh.Body = r.expr(rh.Body)
	}
	for _, g := range rh.Guards {
		g.Cond = r.expr(g.Cond)
		g.Body = r.expr(g.Body)
	}
	for _, b := range rh.Where {
		r.bind(b)
	}
}

func (r *fixResolver) exprs(es []Expr) {
	for i := range es {
		es[i] = r.expr(es[i])
	}
}

func (r *fixResolver) expr(e Expr) Expr {
	switch e := e.(type) {
	case *EApp:
		e.Fun = r.expr(e.Fun)
		r.exprs(e.Args)
	case *ELam:
		e.Body = r.expr(e.Body)
	case *ELet:
		for _, b := range e.Binds {
			r.bind(b)
		}
		e.Body = r.expr(e.Body)
	case *EBlock:
		for _, s := range e.Stmts {
			switch s := s.(type) {
			case *SExpr:
				s.E = r.expr(s.E)
			case *SLet:
				r.bind(s.Bind)
			}
		}
	case *EMatch:
		e.Scrut = r.expr(e.Scrut)
		for _, a := range e.Arms {
			r.rhs(a.Body)
		}
	case *EHandle:
		e.Body = r.expr(e.Body)
		if e.Return != nil {
			e.Return.Body = r.expr(e.Return.Body)
		}
		for _, c := range e.Ops {
			c.Body = r.expr(c.Body)
		}
	case *ETuple:
		r.exprs(e.Elems)
	case *EList:
		r.exprs(e.Elems)
	case *ENeg:
		e.E = r.expr(e.E)
		return desugarNeg(e)
	case *ESection:
		if e.Left != nil {
			e.Left = r.expr(e.Left)
		}
		if e.Right != nil {
			e.Right = r.expr(e.Right)
		}
		return r.desugarSection(e)
	case *EInterp:
		r.exprs(e.Parts)
		for i, p := range e.Parts {
			if l, ok := p.(*ELit); ok && l.Kind == LitString {
				continue
			}
			e.Parts[i] = &EApp{P: p.EPos(), Fun: &EVar{P: p.EPos(), Qual: "#Prelude", Name: "display"}, Args: []Expr{p}}
		}
	case *EAnnot:
		e.E = r.expr(e.E)
	case *EField:
		// `.f.g` is \x -> x.f.g
		inner := e
		for {
			next, ok := inner.E.(*EField)
			if !ok {
				break
			}
			inner = next
		}
		if inner.E == nil {
			r.n++
			x := "fld$" + itoa(r.n)
			inner.E = &EVar{P: inner.P, Name: x}
			return &ELam{P: inner.P, Params: []Pattern{&PVar{P: inner.P, Name: x}}, Body: e}
		}
		e.E = r.expr(e.E)
	case *ERecord:
		for _, f := range e.Fields {
			f.Value = r.expr(f.Value)
		}
	case *EUpdate:
		e.E = r.expr(e.E)
		for _, f := range e.Fields {
			f.Value = r.expr(f.Value)
		}
	case *EBinOp:
		e.Left = r.expr(e.Left)
		e.Right = r.expr(e.Right)
	case *EOpChain:
		r.exprs(e.Operands)
		return r.chain(e)
	}
	return e
}

// chain resolves an operator chain with a shunting-yard algorithm.
func (r *fixResolver) chain(c *EOpChain) Expr {
	var out []Expr
	var ops []*EVar
	reduce := func() {
		op := ops[len(ops)-1]
		ops = ops[:len(ops)-1]
		rhs := out[len(out)-1]
		lhs := out[len(out)-2]
		out = out[:len(out)-2]
		out = append(out, &EBinOp{P: op.P, Op: op, Left: lhs, Right: rhs})
	}
	out = append(out, c.Operands[0])
	for i, op := range c.Ops {
		fo := r.fx.get(op.Name)
		for len(ops) > 0 {
			top := ops[len(ops)-1]
			ft := r.fx.get(top.Name)
			if ft.Prec > fo.Prec || ft.Prec == fo.Prec && fo.Assoc == 1 && ft.Assoc == 1 {
				reduce()
				continue
			}
			if ft.Prec == fo.Prec && (fo.Assoc != ft.Assoc || fo.Assoc == 0) {
				r.errs = append(r.errs, diag.Errorf(op.P, "cannot mix operators '%s' and '%s' without parentheses", top.Name, op.Name))
			}
			break
		}
		ops = append(ops, op)
		out = append(out, c.Operands[i+1])
	}
	for len(ops) > 0 {
		reduce()
	}
	return out[0]
}

func desugarNeg(e *ENeg) Expr {
	if l, ok := e.E.(*ELit); ok {
		switch l.Kind {
		case LitInt:
			return &ELit{P: e.P, Kind: LitInt, Int: -l.Int}
		case LitFloat:
			return &ELit{P: e.P, Kind: LitFloat, Float: -l.Float}
		}
	}
	return &EApp{P: e.P, Fun: &EVar{P: e.P, Qual: "#Prelude", Name: "negate"}, Args: []Expr{e.E}}
}

func isSimple(e Expr) bool {
	switch e.(type) {
	case *EVar, *ECon, *ELit, *EUnit:
		return true
	}
	return false
}

// desugarSection turns (op e) into \x -> x op e and (e op) into \x -> e op x,
// evaluating e once.
func (r *fixResolver) desugarSection(s *ESection) Expr {
	r.n++
	x := "sec$" + itoa(r.n)
	operand := s.Right
	if operand == nil {
		operand = s.Left
	}
	var binds []*Bind
	if !isSimple(operand) {
		t := "secv$" + itoa(r.n)
		binds = append(binds, &Bind{P: s.P, Name: t, Clauses: []*Clause{{P: s.P, Rhs: &Rhs{Body: operand}}}})
		operand = &EVar{P: s.P, Name: t}
	}
	xv := &EVar{P: s.P, Name: x}
	var body Expr
	if s.Right != nil {
		body = &EBinOp{P: s.P, Op: s.Op, Left: xv, Right: operand}
	} else {
		body = &EBinOp{P: s.P, Op: s.Op, Left: operand, Right: xv}
	}
	var res Expr = &ELam{P: s.P, Params: []Pattern{&PVar{P: s.P, Name: x}}, Body: body}
	if binds != nil {
		res = &ELet{P: s.P, Binds: binds, Body: res}
	}
	return res
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
