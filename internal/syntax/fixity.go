package syntax

import "github.com/verniyyy/arkadia/internal/diag"

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
	case *ESection:
		if e.Left != nil {
			e.Left = r.expr(e.Left)
		}
		if e.Right != nil {
			e.Right = r.expr(e.Right)
		}
	case *EInterp:
		r.exprs(e.Parts)
	case *EAnnot:
		e.E = r.expr(e.E)
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
