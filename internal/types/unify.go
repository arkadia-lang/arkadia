package types

import (
	"fmt"
)

// unifyError describes a unification failure.
type unifyError struct {
	msg string
}

func (e *unifyError) Error() string { return e.msg }

func (c *Checker) newMeta(k Kind) *Meta {
	return &Meta{ID: c.g.freshID(), Kind: k, Level: c.level}
}

func (c *Checker) freshType() Type { return c.newMeta(Star) }
func (c *Checker) freshRow() Type  { return c.newMeta(Row) }

func (c *Checker) newSkolem(name string, k Kind) *TSkol {
	return &TSkol{ID: c.g.freshID(), Name: name, Kind: k, Level: c.level}
}

// occursAdjust performs the occurs check and lowers levels of metas in t.
func (c *Checker) occursAdjust(m *Meta, t Type) error {
	t = Prune(t)
	switch t := t.(type) {
	case *Meta:
		if t == m {
			return &unifyError{"infinite type"}
		}
		if t.Level > m.Level {
			t.Level = m.Level
		}
	case *TSkol:
		if t.Level > m.Level {
			return &unifyError{fmt.Sprintf("the rigid type variable '%s' would escape its scope", t.Name)}
		}
	case *TApp:
		if err := c.occursAdjust(m, t.Fun); err != nil {
			return err
		}
		return c.occursAdjust(m, t.Arg)
	case *TFun:
		if err := c.occursAdjust(m, t.Arg); err != nil {
			return err
		}
		if err := c.occursAdjust(m, t.Eff); err != nil {
			return err
		}
		return c.occursAdjust(m, t.Res)
	case *TRowExt:
		if err := c.occursAdjust(m, t.Label); err != nil {
			return err
		}
		return c.occursAdjust(m, t.Rest)
	}
	return nil
}

func (c *Checker) bindMeta(m *Meta, t Type) error {
	if err := c.occursAdjust(m, t); err != nil {
		return err
	}
	m.Ref = t
	return nil
}

func isRow(t Type) bool {
	switch t := Prune(t).(type) {
	case *TRowEmpty, *TRowExt:
		return true
	case *Meta:
		return isRowKind(t.Kind)
	case *TSkol:
		return isRowKind(t.Kind)
	}
	return false
}

// unify makes two types equal.
func (c *Checker) unify(a, b Type) error {
	a, b = Prune(a), Prune(b)
	if a == b {
		return nil
	}
	if ma, ok := a.(*Meta); ok {
		if mb, ok := b.(*Meta); ok {
			// bind the younger (deeper) variable
			if mb.Level > ma.Level {
				return c.bindMeta(mb, ma)
			}
		}
		return c.bindMeta(ma, b)
	}
	if _, ok := b.(*Meta); ok {
		return c.unify(b, a)
	}
	if isRow(a) || isRow(b) {
		return c.unifyRows(a, b)
	}
	switch a := a.(type) {
	case *TCon:
		if bb, ok := b.(*TCon); ok && bb.Name == a.Name {
			return nil
		}
	case *TSkol:
		if bb, ok := b.(*TSkol); ok && bb == a {
			return nil
		}
	case *TApp:
		if bb, ok := b.(*TApp); ok {
			if err := c.unify(a.Fun, bb.Fun); err != nil {
				return err
			}
			return c.unify(a.Arg, bb.Arg)
		}
	case *TFun:
		if bb, ok := b.(*TFun); ok {
			if err := c.unify(a.Arg, bb.Arg); err != nil {
				return err
			}
			if err := c.unify(a.Eff, bb.Eff); err != nil {
				return err
			}
			return c.unify(a.Res, bb.Res)
		}
	}
	return &unifyError{""}
}

// unifyRows unifies two effect rows (scoped labels, Leijen-style).
func (c *Checker) unifyRows(a, b Type) error {
	la, ta := flattenRow(a)
	lb, tb := flattenRow(b)
	used := make([]bool, len(lb))
	var restA []Type
	for _, l := range la {
		name := labelName(l)
		found := false
		for j, l2 := range lb {
			if !used[j] && labelName(l2) == name {
				used[j] = true
				found = true
				if err := c.unify(l, l2); err != nil {
					return err
				}
				break
			}
		}
		if !found {
			restA = append(restA, l)
		}
	}
	var restB []Type
	for j, l := range lb {
		if !used[j] {
			restB = append(restB, l)
		}
	}
	ta, tb = Prune(ta), Prune(tb)
	if len(restA) == 0 && len(restB) == 0 {
		return c.unifyTails(ta, tb)
	}
	if ma, ok := ta.(*Meta); ok {
		if mb, ok := tb.(*Meta); ok && ma == mb {
			return &unifyError{"recursive row"}
		}
	}
	if len(restA) == 0 {
		// tail of a must absorb restB
		return c.extendTail(ta, restB, tb)
	}
	if len(restB) == 0 {
		return c.extendTail(tb, restA, ta)
	}
	ma, okA := ta.(*Meta)
	mb, okB := tb.(*Meta)
	if !okA || !okB {
		if !okA {
			return c.missingEffect(restB, ta)
		}
		return c.missingEffect(restA, tb)
	}
	r := c.newMeta(Row)
	lvl := ma.Level
	if mb.Level < lvl {
		lvl = mb.Level
	}
	r.Level = lvl
	if err := c.bindMeta(ma, mkRow(restB, r)); err != nil {
		return err
	}
	return c.unify(mb, mkRow(restA, r))
}

func (c *Checker) missingEffect(labels []Type, tail Type) error {
	var names []string
	for _, l := range labels {
		names = append(names, TypeString(l))
	}
	return &unifyError{fmt.Sprintf("effect %s is not allowed here", joinNames(names))}
}

func joinNames(ns []string) string {
	s := ""
	for i, n := range ns {
		if i > 0 {
			if i == len(ns)-1 {
				s += " and "
			} else {
				s += ", "
			}
		}
		s += n
	}
	return s
}

func (c *Checker) extendTail(tail Type, labels []Type, otherTail Type) error {
	if m, ok := tail.(*Meta); ok {
		return c.bindMeta(m, mkRow(labels, otherTail))
	}
	return c.missingEffect(labels, tail)
}

func (c *Checker) unifyTails(a, b Type) error {
	if a == b {
		return nil
	}
	if m, ok := a.(*Meta); ok {
		return c.bindMeta(m, b)
	}
	if m, ok := b.(*Meta); ok {
		return c.bindMeta(m, a)
	}
	if _, ok := a.(*TRowEmpty); ok {
		if _, ok := b.(*TRowEmpty); ok {
			return nil
		}
	}
	if sa, ok := a.(*TSkol); ok {
		if sb, ok := b.(*TSkol); ok && sa == sb {
			return nil
		}
	}
	return &unifyError{"effect rows do not match"}
}

// openRow replaces a closed effect row with an open one (subsumption).
func (c *Checker) openRow(eff Type) Type {
	labels, tail := flattenRow(eff)
	if _, ok := tail.(*TRowEmpty); ok {
		return mkRow(labels, c.freshRow())
	}
	return eff
}

// openFun opens the closed effect rows along a function type's result spine.
func (c *Checker) openFun(t Type) Type {
	t = Prune(t)
	f, ok := t.(*TFun)
	if !ok {
		return t
	}
	return &TFun{Arg: f.Arg, Eff: c.openRow(f.Eff), Res: c.openFun(f.Res)}
}

// subRow checks that the effects of row a are included in row b. It
// behaves like unification except that a rigid or closed tail of a may be
// used under extra labels of b (effect subsumption). Extra labels that sit
// above a rigid tail are returned: the callee must run with those handlers
// masked so that its own operations skip them.
func (c *Checker) subRow(a, b Type) ([]Type, error) {
	la, ta := flattenRow(a)
	ta = Prune(ta)
	lb, tb := flattenRow(b)
	if _, ok := ta.(*Meta); ok && Prune(tb) != ta {
		return nil, c.unify(a, b)
	}
	used := make([]bool, len(lb))
	var missing []Type
	for _, l := range la {
		found := false
		for j, l2 := range lb {
			if !used[j] && labelName(l2) == labelName(l) {
				used[j] = true
				found = true
				if err := c.unify(l, l2); err != nil {
					return nil, err
				}
				break
			}
		}
		if !found {
			missing = append(missing, l)
		}
	}
	var extra []Type
	for j, l := range lb {
		if !used[j] {
			extra = append(extra, l)
		}
	}
	tb = Prune(tb)
	if m, ok := ta.(*Meta); ok && tb == ta {
		// same open tail: a is included in b when its labels are
		if len(missing) > 0 {
			return nil, c.missingEffect(missing, m)
		}
		return extra, nil
	}
	if _, ok := ta.(*TRowEmpty); ok {
		if len(missing) == 0 {
			return nil, nil
		}
		if m, ok := tb.(*Meta); ok {
			return nil, c.bindMeta(m, mkRow(missing, c.newMeta(Row)))
		}
		return nil, c.missingEffect(missing, tb)
	}
	// rigid tail: b's tail must be the same variable
	if tb == ta {
		if len(missing) > 0 {
			return nil, c.missingEffect(missing, tb)
		}
		return extra, nil
	}
	if m, ok := tb.(*Meta); ok {
		return extra, c.bindMeta(m, mkRow(missing, ta))
	}
	if len(missing) > 0 {
		return nil, c.missingEffect(missing, tb)
	}
	return nil, &unifyError{"effect rows do not match"}
}
