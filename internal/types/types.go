// Package types implements kind checking and type inference for Arkadia:
// Hindley–Milner with type classes (dictionary passing), higher-kinded type
// variables and row-polymorphic effect types.
package types

import (
	"fmt"
	"sort"
	"strings"
)

// ------------------------------------------------------------------ kinds

type Kind interface{ isKind() }

type KStar struct{}
type KRow struct{}
type KEffect struct{}
type KArrow struct{ From, To Kind }
type KVar struct {
	ID  int
	Ref Kind
}

func (KStar) isKind()   {}
func (KRow) isKind()    {}
func (KEffect) isKind() {}
func (*KArrow) isKind() {}
func (*KVar) isKind()   {}

var (
	Star   Kind = KStar{}
	Row    Kind = KRow{}
	Effect Kind = KEffect{}
)

func pruneKind(k Kind) Kind {
	for {
		v, ok := k.(*KVar)
		if !ok || v.Ref == nil {
			return k
		}
		k = v.Ref
	}
}

// DefaultKind replaces unresolved kind variables with *.
func DefaultKind(k Kind) Kind {
	k = pruneKind(k)
	switch k := k.(type) {
	case *KVar:
		k.Ref = Star
		return Star
	case *KArrow:
		return &KArrow{DefaultKind(k.From), DefaultKind(k.To)}
	}
	return k
}

func KindString(k Kind) string {
	k = pruneKind(k)
	switch k := k.(type) {
	case KStar:
		return "Type"
	case KRow:
		return "Row"
	case KEffect:
		return "Effect"
	case *KArrow:
		from := KindString(k.From)
		if _, ok := pruneKind(k.From).(*KArrow); ok {
			from = "(" + from + ")"
		}
		return from + " -> " + KindString(k.To)
	case *KVar:
		return fmt.Sprintf("k%d", k.ID)
	}
	return "?"
}

func unifyKind(a, b Kind) bool {
	a, b = pruneKind(a), pruneKind(b)
	if va, ok := a.(*KVar); ok {
		if vb, ok := b.(*KVar); ok && va == vb {
			return true
		}
		if occursKind(va, b) {
			return false
		}
		va.Ref = b
		return true
	}
	if _, ok := b.(*KVar); ok {
		return unifyKind(b, a)
	}
	switch a := a.(type) {
	case KStar:
		_, ok := b.(KStar)
		return ok
	case KRow:
		_, ok := b.(KRow)
		return ok
	case KEffect:
		_, ok := b.(KEffect)
		return ok
	case *KArrow:
		bb, ok := b.(*KArrow)
		return ok && unifyKind(a.From, bb.From) && unifyKind(a.To, bb.To)
	}
	return false
}

func occursKind(v *KVar, k Kind) bool {
	k = pruneKind(k)
	switch k := k.(type) {
	case *KVar:
		return k == v
	case *KArrow:
		return occursKind(v, k.From) || occursKind(v, k.To)
	}
	return false
}

// ------------------------------------------------------------------ types

type Type interface{ isType() }

// TCon is a type constructor identified by its global name.
type TCon struct {
	Name string
	Kind Kind
}

// TApp is type application.
type TApp struct{ Fun, Arg Type }

// TFun is a function type with an effect row.
type TFun struct{ Arg, Eff, Res Type }

// TRowEmpty is the closed empty effect row.
type TRowEmpty struct{}

// TRowExt extends a row with an effect label.
type TRowExt struct{ Label, Rest Type }

// Meta is a unification variable.
type Meta struct {
	ID    int
	Kind  Kind
	Level int
	Ref   Type
}

// TSkol is a rigid (skolem) type variable.
type TSkol struct {
	ID    int
	Name  string
	Kind  Kind
	Level int
}

// TGen is a quantified variable inside a Scheme.
type TGen struct {
	Index int
}

func (*TCon) isType()      {}
func (*TApp) isType()      {}
func (*TFun) isType()      {}
func (*TRowEmpty) isType() {}
func (*TRowExt) isType()   {}
func (*Meta) isType()      {}
func (*TSkol) isType()     {}
func (*TGen) isType()      {}

var emptyRow = &TRowEmpty{}

// Prune follows bound meta variables.
func Prune(t Type) Type {
	for {
		m, ok := t.(*Meta)
		if !ok || m.Ref == nil {
			return t
		}
		// path compression
		if mm, ok := m.Ref.(*Meta); ok && mm.Ref != nil {
			m.Ref = mm.Ref
		}
		t = m.Ref
	}
}

// Zonk fully substitutes bound meta variables.
func Zonk(t Type) Type {
	t = Prune(t)
	switch t := t.(type) {
	case *TApp:
		return &TApp{Zonk(t.Fun), Zonk(t.Arg)}
	case *TFun:
		return &TFun{Zonk(t.Arg), Zonk(t.Eff), Zonk(t.Res)}
	case *TRowExt:
		return &TRowExt{Zonk(t.Label), Zonk(t.Rest)}
	}
	return t
}

// headOf returns the head of a type application spine and its arguments.
func headOf(t Type) (Type, []Type) {
	t = Prune(t)
	var args []Type
	for {
		app, ok := t.(*TApp)
		if !ok {
			break
		}
		args = append(args, app.Arg)
		t = Prune(app.Fun)
	}
	for i, j := 0, len(args)-1; i < j; i, j = i+1, j-1 {
		args[i], args[j] = args[j], args[i]
	}
	return t, args
}

func mkApp(f Type, args ...Type) Type {
	for _, a := range args {
		f = &TApp{f, a}
	}
	return f
}

// flattenRow splits a row into its labels and its tail.
func flattenRow(t Type) ([]Type, Type) {
	var labels []Type
	t = Prune(t)
	for {
		ext, ok := t.(*TRowExt)
		if !ok {
			return labels, t
		}
		labels = append(labels, ext.Label)
		t = Prune(ext.Rest)
	}
}

func mkRow(labels []Type, tail Type) Type {
	for i := len(labels) - 1; i >= 0; i-- {
		tail = &TRowExt{labels[i], tail}
	}
	return tail
}

// labelName returns the effect constructor name of a row label.
func labelName(l Type) string {
	h, _ := headOf(l)
	switch h := h.(type) {
	case *TCon:
		return h.Name
	case *TSkol:
		return "'" + h.Name
	case *Meta:
		return fmt.Sprintf("?%d", h.ID)
	}
	return "?"
}

// ------------------------------------------------------------------ schemes

// Pred is a class constraint.
type Pred struct {
	Class *ClassInfo
	Type  Type
}

// Scheme is a polymorphic type ∀ vars. preds => type.
type Scheme struct {
	Kinds []Kind
	Names []string // optional display names
	Preds []Pred
	Type  Type
}

// Mono wraps a monomorphic type.
func Mono(t Type) *Scheme { return &Scheme{Type: t} }

func substGen(t Type, sub []Type) Type {
	switch t := t.(type) {
	case *TGen:
		return sub[t.Index]
	case *TApp:
		return &TApp{substGen(t.Fun, sub), substGen(t.Arg, sub)}
	case *TFun:
		return &TFun{substGen(t.Arg, sub), substGen(t.Eff, sub), substGen(t.Res, sub)}
	case *TRowExt:
		return &TRowExt{substGen(t.Label, sub), substGen(t.Rest, sub)}
	case *Meta:
		if t.Ref != nil {
			return substGen(Prune(t), sub)
		}
	}
	return t
}

// ------------------------------------------------------------------ printing

type printer struct {
	counts map[any]int
	names  map[any]string
	used   map[string]bool
	gen    []string
}

func newPrinter() *printer {
	return &printer{names: map[any]string{}, used: map[string]bool{}, counts: map[any]int{}}
}

// count records occurrences of variables so that a row variable occurring
// only once can be omitted when printing.
func (p *printer) count(t Type) {
	t = Prune(t)
	switch t := t.(type) {
	case *Meta:
		p.counts[t]++
	case *TSkol:
		p.counts[t]++
	case *TGen:
		p.counts[t.Index]++
	case *TApp:
		p.count(t.Fun)
		p.count(t.Arg)
	case *TFun:
		p.count(t.Arg)
		p.count(t.Eff)
		p.count(t.Res)
	case *TRowExt:
		p.count(t.Label)
		p.count(t.Rest)
	}
}

func (p *printer) occurrences(t Type) int {
	switch t := t.(type) {
	case *TGen:
		return p.counts[t.Index]
	default:
		return p.counts[t]
	}
}

func (p *printer) fresh(base string, row bool) string {
	letters := "abcdefghijklmnopqrstuvwxyz"
	if row {
		letters = "efghijklmnopqrstuvwxyzabcd"
	}
	if base != "" && !p.used[base] {
		p.used[base] = true
		return base
	}
	for i := 0; ; i++ {
		for _, l := range letters {
			n := string(l)
			if i > 0 {
				n = fmt.Sprintf("%c%d", l, i)
			}
			if !p.used[n] {
				p.used[n] = true
				return n
			}
		}
	}
}

func (p *printer) nameOf(key any, base string, row bool) string {
	if n, ok := p.names[key]; ok {
		return n
	}
	n := p.fresh(base, row)
	p.names[key] = n
	return n
}

func shortName(n string) string {
	if i := strings.LastIndex(n, "."); i >= 0 {
		return n[i+1:]
	}
	return n
}

// TypeString renders a type for diagnostics.
func TypeString(t Type) string {
	p := newPrinter()
	p.count(t)
	return p.typ(t, 0)
}

// TypesString renders several types with a shared variable naming.
func TypesString(ts ...Type) []string {
	p := newPrinter()
	for _, t := range ts {
		p.count(t)
	}
	var out []string
	for _, t := range ts {
		out = append(out, p.typ(t, 0))
	}
	return out
}

func isRowKind(k Kind) bool {
	_, ok := pruneKind(k).(KRow)
	return ok
}

// prec: 0 top, 1 function argument, 2 application argument
func (p *printer) typ(t Type, prec int) string {
	t = Prune(t)
	switch t := t.(type) {
	case *TCon:
		return shortName(t.Name)
	case *Meta:
		return p.nameOf(t, "", isRowKind(t.Kind))
	case *TSkol:
		return p.nameOf(t, t.Name, isRowKind(t.Kind))
	case *TGen:
		if t.Index < len(p.gen) {
			return p.gen[t.Index]
		}
		return fmt.Sprintf("g%d", t.Index)
	case *TFun:
		s := p.typ(t.Arg, 1) + " -> "
		if r := p.rowAnnot(t.Eff); r != "" {
			s += r + " "
		}
		s += p.typ(t.Res, 0)
		if prec > 0 {
			return "(" + s + ")"
		}
		return s
	case *TApp:
		h, args := headOf(t)
		if c, ok := h.(*TCon); ok {
			name := shortName(c.Name)
			if strings.HasPrefix(name, "Tuple") && len(args) > 1 && fmt.Sprintf("Tuple%d", len(args)) == name {
				var parts []string
				for _, a := range args {
					parts = append(parts, p.typ(a, 0))
				}
				return "(" + strings.Join(parts, ", ") + ")"
			}
		}
		parts := []string{p.typ(h, 2)}
		for _, a := range args {
			parts = append(parts, p.typ(a, 2))
		}
		s := strings.Join(parts, " ")
		if prec > 1 {
			return "(" + s + ")"
		}
		return s
	case *TRowEmpty, *TRowExt:
		return p.row(t)
	}
	return "?"
}

func (p *printer) row(t Type) string {
	labels, tail := flattenRow(t)
	var parts []string
	for _, l := range labels {
		parts = append(parts, p.typ(l, 0))
	}
	s := "<" + strings.Join(parts, ", ")
	if _, ok := tail.(*TRowEmpty); !ok {
		if len(parts) > 0 {
			s += " | "
		}
		s += p.typ(tail, 0)
	}
	return s + ">"
}

// rowAnnot renders the effect of an arrow; a bare row variable is omitted.
func (p *printer) rowAnnot(t Type) string {
	labels, tail := flattenRow(t)
	if len(labels) == 0 {
		switch tail.(type) {
		case *Meta, *TGen, *TSkol:
			if p.occurrences(tail) <= 1 {
				return ""
			}
		}
	}
	return p.row(t)
}

// SchemeString renders a scheme.
func SchemeString(s *Scheme) string {
	p := newPrinter()
	for i, k := range s.Kinds {
		base := ""
		if i < len(s.Names) {
			base = s.Names[i]
		}
		p.gen = append(p.gen, p.fresh(base, isRowKind(k)))
	}
	p.count(s.Type)
	for _, pr := range s.Preds {
		p.count(pr.Type)
	}
	var sb strings.Builder
	if len(s.Preds) > 0 {
		var ps []string
		for _, pr := range s.Preds {
			ps = append(ps, shortName(pr.Class.Name)+" "+p.typ(pr.Type, 2))
		}
		if len(ps) == 1 {
			sb.WriteString(ps[0] + " => ")
		} else {
			sb.WriteString("(" + strings.Join(ps, ", ") + ") => ")
		}
	}
	sb.WriteString(p.typ(s.Type, 0))
	return sb.String()
}

// ------------------------------------------------------------------ free variables

func metasOf(t Type, acc map[*Meta]bool, order *[]*Meta) {
	t = Prune(t)
	switch t := t.(type) {
	case *Meta:
		if !acc[t] {
			acc[t] = true
			*order = append(*order, t)
		}
	case *TApp:
		metasOf(t.Fun, acc, order)
		metasOf(t.Arg, acc, order)
	case *TFun:
		metasOf(t.Arg, acc, order)
		metasOf(t.Eff, acc, order)
		metasOf(t.Res, acc, order)
	case *TRowExt:
		metasOf(t.Label, acc, order)
		metasOf(t.Rest, acc, order)
	}
}

func skolemsOf(t Type, acc map[*TSkol]bool) {
	t = Prune(t)
	switch t := t.(type) {
	case *TSkol:
		acc[t] = true
	case *TApp:
		skolemsOf(t.Fun, acc)
		skolemsOf(t.Arg, acc)
	case *TFun:
		skolemsOf(t.Arg, acc)
		skolemsOf(t.Eff, acc)
		skolemsOf(t.Res, acc)
	case *TRowExt:
		skolemsOf(t.Label, acc)
		skolemsOf(t.Rest, acc)
	}
}

func typesEqual(a, b Type) bool {
	a, b = Prune(a), Prune(b)
	switch a := a.(type) {
	case *TCon:
		bb, ok := b.(*TCon)
		return ok && a.Name == bb.Name
	case *Meta:
		return a == b
	case *TSkol:
		return a == b
	case *TGen:
		bb, ok := b.(*TGen)
		return ok && a.Index == bb.Index
	case *TApp:
		bb, ok := b.(*TApp)
		return ok && typesEqual(a.Fun, bb.Fun) && typesEqual(a.Arg, bb.Arg)
	case *TFun:
		bb, ok := b.(*TFun)
		return ok && typesEqual(a.Arg, bb.Arg) && typesEqual(a.Eff, bb.Eff) && typesEqual(a.Res, bb.Res)
	case *TRowEmpty:
		_, ok := b.(*TRowEmpty)
		return ok
	case *TRowExt:
		bb, ok := b.(*TRowExt)
		return ok && typesEqual(a.Label, bb.Label) && typesEqual(a.Rest, bb.Rest)
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
