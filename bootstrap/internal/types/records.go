package types

import (
	"sort"

	"github.com/arkadia-lang/arkadia/bootstrap/internal/diag"
	"github.com/arkadia-lang/arkadia/bootstrap/internal/syntax"
)

// ------------------------------------------------------------------ records
//
// Field access and update are resolved by the type of the record: `e.f`
// needs the type constructor of e. When it is not known yet, the access is
// recorded as a pending field constraint and retried before the enclosing
// binding is generalized (and before its class constraints are solved). A
// constraint whose record type is still unknown at that point is resolved
// by the field names: if exactly one record type in scope has them all, it
// is used; otherwise the program needs a type annotation.

type fieldWanted struct {
	pos   diag.Pos
	rec   Type
	name  string
	res   Type     // the field's type (access) or the new value's type (update)
	group []string // all fields named together with this one
	what  string   // describes res in mismatch errors
	set   func(*TypeInfo)
}

// recordField returns the type of field name (in terms of TGen) if every
// constructor of ti has it.
func recordField(ti *TypeInfo, name string) (Type, bool) {
	if len(ti.Ctors) == 0 {
		return nil, false
	}
	var t Type
	for _, ci := range ti.Ctors {
		i := ci.FieldIndex(name)
		if i < 0 {
			return nil, false
		}
		t = ci.Fields[i]
	}
	return t, true
}

func hasAnyField(ti *TypeInfo, name string) bool {
	for _, ci := range ti.Ctors {
		if ci.FieldIndex(name) >= 0 {
			return true
		}
	}
	return false
}

// recordArgs instantiates the parameters of ti and unifies t with the
// resulting type.
func (c *Checker) recordArgs(pos diag.Pos, ti *TypeInfo, t Type) []Type {
	args := make([]Type, len(ti.Params))
	for i, k := range paramKinds(ti.Kind, len(ti.Params)) {
		args[i] = c.newMeta(k)
	}
	c.unifyAt(pos, mkApp(&TCon{ti.Name, ti.Kind}, args...), t, "the record")
	return args
}

// fieldOf resolves field name of a value of type t whose head is known.
// It returns false when the head is still unknown.
func (c *Checker) fieldOf(pos diag.Pos, t Type, name string) (*TypeInfo, Type, bool) {
	h, _ := headOf(t)
	switch h := h.(type) {
	case *Meta:
		return nil, nil, false
	case *TCon:
		ti := c.g.Types[h.Name]
		if ti != nil {
			if ft, ok := recordField(ti, name); ok {
				args := c.recordArgs(pos, ti, t)
				return ti, substGen(ft, args), true
			}
			if hasAnyField(ti, name) {
				c.errorf(pos, "field '%s' is not present in every constructor of %s", name, TypeString(Zonk(t)))
				return nil, nil, true
			}
		}
	}
	c.errorf(pos, "type %s has no field '%s'", TypeString(Zonk(t)), name)
	return nil, nil, true
}

func (c *Checker) tryField(w *fieldWanted) bool {
	ti, ft, ok := c.fieldOf(w.pos, w.rec, w.name)
	if !ok {
		return false
	}
	if ti != nil {
		c.unifyAt(w.pos, ft, w.res, w.what)
		w.set(ti)
	}
	return true
}

func (c *Checker) needField(w *fieldWanted) {
	if !c.tryField(w) {
		c.fields = append(c.fields, w)
	}
}

// settleFields retries the pending field constraints. Those whose record
// type is a variable above level lvl (about to be generalized) are resolved
// by their field names; the others stay pending at level lvl.
func (c *Checker) settleFields(lvl int) {
	for {
		progress := false
		var rest []*fieldWanted
		for _, w := range c.fields {
			if c.tryField(w) {
				progress = true
			} else {
				rest = append(rest, w)
			}
		}
		c.fields = rest
		if progress {
			continue
		}
		var keep []*fieldWanted
		for _, w := range c.fields {
			if progress {
				keep = append(keep, w)
				continue
			}
			m, _ := Prune(w.rec).(*Meta)
			if m == nil || m.Level <= lvl {
				keep = append(keep, w)
				continue
			}
			c.fieldByName(w)
			progress = true
		}
		c.fields = keep
		if !progress {
			break
		}
	}
	for _, w := range c.fields {
		lowerLevels(w.rec, lvl)
		lowerLevels(w.res, lvl)
	}
}

// fieldByName resolves a constraint by the unique record type in scope
// that has all the fields of its group.
func (c *Checker) fieldByName(w *fieldWanted) {
	var names []string
	for n := range c.scope.Types {
		names = append(names, n)
	}
	sort.Strings(names)
	var cands []*TypeInfo
	var candNames []string
	for _, n := range names {
		ti, ok := c.scope.Types[n].(*TypeInfo)
		if !ok {
			continue
		}
		all := true
		for _, f := range w.group {
			if _, ok := recordField(ti, f); !ok {
				all = false
				break
			}
		}
		if all {
			cands = append(cands, ti)
			candNames = append(candNames, n)
		}
	}
	switch len(cands) {
	case 0:
		c.errorf(w.pos, "no record type in scope has the field '%s'", w.name)
	case 1:
		c.recordArgs(w.pos, cands[0], w.rec)
		c.tryField(w)
	default:
		c.errorf(w.pos, "cannot tell which record the field '%s' belongs to (%s have it); add a type annotation", w.name, joinNames(candNames))
	}
}

func (c *Checker) inferField(e *syntax.EField, eff Type) Type {
	t := c.inferExpr(e.E, eff)
	r := c.freshType()
	c.needField(&fieldWanted{pos: e.P, rec: t, name: e.Name, res: r, group: []string{e.Name}, what: "the field", set: func(ti *TypeInfo) { e.Rec = ti }})
	c.record(e.P, "."+e.Name, r, nil, diag.Pos{}, "field")
	return r
}

func fieldNames(fs []*syntax.FieldInit) []string {
	var ns []string
	for _, f := range fs {
		ns = append(ns, f.Name)
	}
	return ns
}

func (c *Checker) checkDupFields(fs []*syntax.FieldInit) {
	seen := map[string]bool{}
	for _, f := range fs {
		if seen[f.Name] {
			c.errorf(f.P, "field '%s' is given twice", f.Name)
		}
		seen[f.Name] = true
	}
}

func (c *Checker) inferUpdate(e *syntax.EUpdate, eff Type) Type {
	t := c.inferExpr(e.E, eff)
	c.checkDupFields(e.Fields)
	group := fieldNames(e.Fields)
	set := func(ti *TypeInfo) { e.Rec = ti }
	if h, _ := headOf(t); !isMeta(h) {
		// the record type is known: check the new values against the fields
		for _, f := range e.Fields {
			ti, ft, _ := c.fieldOf(f.P, t, f.Name)
			if ti == nil {
				c.inferExpr(f.Value, eff)
				continue
			}
			set(ti)
			c.checkExpr(f.Value, ft, eff, "the value of field '"+f.Name+"'")
		}
		return t
	}
	for _, f := range e.Fields {
		v := c.inferExpr(f.Value, eff)
		c.needField(&fieldWanted{pos: f.P, rec: t, name: f.Name, res: v, group: group, what: "the value of field '" + f.Name + "'", set: set})
	}
	return t
}

func isMeta(t Type) bool {
	_, ok := Prune(t).(*Meta)
	return ok
}

func (c *Checker) inferRecord(e *syntax.ERecord, eff Type) Type {
	ci := c.lookupCtor(e.P, e.Qual, e.Name)
	if ci == nil {
		for _, f := range e.Fields {
			c.inferExpr(f.Value, eff)
		}
		return c.freshType()
	}
	e.Con = ci
	if ci.FieldNames == nil {
		c.errorf(e.P, "constructor '%s' has no named fields", e.Name)
		for _, f := range e.Fields {
			c.inferExpr(f.Value, eff)
		}
		return c.freshType()
	}
	ct, _ := c.instantiate(ci.Scheme, e.P)
	var fields []Type
	for i := 0; i < ci.Arity; i++ {
		f := Prune(ct).(*TFun)
		fields = append(fields, f.Arg)
		ct = f.Res
	}
	c.record(e.P, e.Name, ct, nil, ci.Type.Pos, "constructor")
	c.checkDupFields(e.Fields)
	given := map[string]bool{}
	for _, f := range e.Fields {
		i := ci.FieldIndex(f.Name)
		if i < 0 {
			c.errorf(f.P, "constructor '%s' has no field '%s'", e.Name, f.Name)
			c.inferExpr(f.Value, eff)
			continue
		}
		given[f.Name] = true
		c.checkExpr(f.Value, fields[i], eff, "the value of field '"+f.Name+"'")
	}
	var missing []string
	for _, n := range ci.FieldNames {
		if !given[n] {
			missing = append(missing, "'"+n+"'")
		}
	}
	if len(missing) > 0 {
		c.errorf(e.P, "missing field(s) %s in this construction of %s", joinNames(missing), e.Name)
	}
	return ct
}

// recordPatArgs turns the named sub-patterns of a record pattern into
// positional ones, filling the missing fields with wildcards.
func (c *Checker) recordPatArgs(p *syntax.PCon, ci *CtorInfo) bool {
	if ci.FieldNames == nil {
		c.errorf(p.P, "constructor '%s' has no named fields", p.Name)
		return false
	}
	args := make([]syntax.Pattern, ci.Arity)
	ok := true
	for i, f := range p.Fields {
		j := ci.FieldIndex(f)
		switch {
		case j < 0:
			c.errorf(p.Args[i].PPos(), "constructor '%s' has no field '%s'", p.Name, f)
			ok = false
		case args[j] != nil:
			c.errorf(p.Args[i].PPos(), "field '%s' is matched twice", f)
			ok = false
		default:
			args[j] = p.Args[i]
		}
	}
	for j := range args {
		if args[j] == nil {
			args[j] = &syntax.PWild{P: p.P}
		}
	}
	if ok {
		p.Args, p.Record, p.Fields = args, false, nil
	}
	return ok
}
