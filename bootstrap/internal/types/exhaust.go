package types

import (
	"strconv"
	"strings"

	"github.com/verniyyy/arkadia/bootstrap/internal/diag"
	"github.com/verniyyy/arkadia/bootstrap/internal/syntax"
)

// matchSite records a pattern match for exhaustiveness checking.
type matchSite struct {
	pos     diag.Pos
	what    string
	rows    [][]syntax.Pattern
	guarded []bool
}

// spat is a simplified pattern.
type spat struct {
	wild bool
	con  *CtorInfo // constructor (nil with lit set for literals)
	lit  string
	args []spat
}

var wildPat = spat{wild: true}

func simplify(p syntax.Pattern) spat {
	switch p := p.(type) {
	case *syntax.PVar, *syntax.PWild:
		return wildPat
	case *syntax.PAs:
		return simplify(p.Pat)
	case *syntax.PAnnot:
		return simplify(p.Pat)
	case *syntax.PUnit:
		return wildPat
	case *syntax.PLit:
		return spat{lit: litKey(p.Lit)}
	case *syntax.PCon:
		ci, _ := p.Con.(*CtorInfo)
		if ci == nil {
			return wildPat
		}
		s := spat{con: ci}
		for _, a := range p.Args {
			s.args = append(s.args, simplify(a))
		}
		return s
	case *syntax.PTuple:
		// tuples have a single constructor: treat as a constructor pattern
		s := spat{con: &CtorInfo{Name: "tuple", Short: "", Arity: len(p.Elems), Type: &TypeInfo{Name: "tuple"}}}
		for _, a := range p.Elems {
			s.args = append(s.args, simplify(a))
		}
		s.con = tupleCtor(len(p.Elems))
		return s
	case *syntax.PList:
		cons, _ := p.Cons.(*CtorInfo)
		nil_, _ := p.Nil.(*CtorInfo)
		if cons == nil || nil_ == nil {
			return wildPat
		}
		s := spat{con: nil_}
		for i := len(p.Elems) - 1; i >= 0; i-- {
			s = spat{con: cons, args: []spat{simplify(p.Elems[i]), s}}
		}
		return s
	}
	return wildPat
}

var tupleCtors = map[int]*CtorInfo{}

func tupleCtor(n int) *CtorInfo {
	if c, ok := tupleCtors[n]; ok {
		return c
	}
	ti := &TypeInfo{Name: "tuple" + itoa(n)}
	c := &CtorInfo{Name: ti.Name, Short: "", Arity: n, Type: ti}
	ti.Ctors = []*CtorInfo{c}
	tupleCtors[n] = c
	return c
}

func litKey(l *syntax.ELit) string {
	switch l.Kind {
	case syntax.LitInt:
		return "i" + itoa64(l.Int)
	case syntax.LitFloat:
		return "f" + fmtFloat(l.Float)
	case syntax.LitString:
		return "s" + l.Str
	default:
		return "c" + l.Str
	}
}

func itoa64(n int64) string {
	if n < 0 {
		return "-" + itoa64(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa64(n/10) + string(rune('0'+n%10))
}

func (c *Checker) checkMatches() {
	for _, m := range c.matches {
		var rows [][]spat
		for i, r := range m.rows {
			if m.guarded[i] {
				continue
			}
			var row []spat
			for _, p := range r {
				row = append(row, simplify(p))
			}
			rows = append(rows, row)
		}
		n := len(m.rows[0])
		if w := missing(rows, n); w != nil {
			var parts []string
			for _, p := range w {
				parts = append(parts, showSpat(p, n > 1))
			}
			c.errorf(m.pos, "non-exhaustive patterns in %s: the case `%s` is not handled", m.what, strings.Join(parts, " "))
		}
	}
}

func showSpat(p spat, atom bool) string {
	if p.wild {
		return "_"
	}
	if p.con == nil {
		return "<literal>"
	}
	if strings.HasPrefix(p.con.Type.Name, "tuple") {
		var parts []string
		for _, a := range p.args {
			parts = append(parts, showSpat(a, false))
		}
		return "(" + strings.Join(parts, ", ") + ")"
	}
	if p.con.Type.Name == TList {
		if p.con.Short == "Nil" {
			return "[]"
		}
		s := showSpat(p.args[0], true) + " :: " + showSpat(p.args[1], false)
		if atom {
			return "(" + s + ")"
		}
		return s
	}
	if len(p.args) == 0 {
		return p.con.Short
	}
	parts := []string{p.con.Short}
	for _, a := range p.args {
		parts = append(parts, showSpat(a, true))
	}
	s := strings.Join(parts, " ")
	if atom {
		return "(" + s + ")"
	}
	return s
}

func wilds(n int) []spat {
	out := make([]spat, n)
	for i := range out {
		out[i] = wildPat
	}
	return out
}

// specialize keeps rows matching constructor con, expanding its arguments.
func specialize(rows [][]spat, con *CtorInfo) [][]spat {
	var out [][]spat
	for _, r := range rows {
		h := r[0]
		switch {
		case h.wild:
			out = append(out, append(wilds(con.Arity), r[1:]...))
		case h.con == con:
			out = append(out, append(append([]spat{}, h.args...), r[1:]...))
		}
	}
	return out
}

func specializeLit(rows [][]spat, lit string) [][]spat {
	var out [][]spat
	for _, r := range rows {
		if r[0].wild || r[0].lit == lit && r[0].con == nil {
			out = append(out, r[1:])
		}
	}
	return out
}

func defaultRows(rows [][]spat) [][]spat {
	var out [][]spat
	for _, r := range rows {
		if r[0].wild {
			out = append(out, r[1:])
		}
	}
	return out
}

// missing returns a pattern vector not covered by rows, or nil.
func missing(rows [][]spat, n int) []spat {
	if n == 0 {
		if len(rows) == 0 {
			return []spat{}
		}
		return nil
	}
	var ti *TypeInfo
	seen := map[*CtorInfo]bool{}
	hasLit := false
	for _, r := range rows {
		h := r[0]
		if h.con != nil {
			ti = h.con.Type
			seen[h.con] = true
		} else if !h.wild {
			hasLit = true
		}
	}
	if ti != nil && len(seen) == len(ti.Ctors) {
		for _, con := range ti.Ctors {
			if w := missing(specialize(rows, con), con.Arity+n-1); w != nil {
				return append([]spat{{con: con, args: w[:con.Arity]}}, w[con.Arity:]...)
			}
		}
		return nil
	}
	if hasLit && ti == nil {
		// literals never cover their type; check specializations anyway so
		// that unreachable combinations in later columns are found
		w := missing(defaultRows(rows), n-1)
		if w == nil {
			return nil
		}
		return append([]spat{wildPat}, w...)
	}
	w := missing(defaultRows(rows), n-1)
	if w == nil {
		return nil
	}
	if ti != nil {
		for _, con := range ti.Ctors {
			if !seen[con] {
				return append([]spat{{con: con, args: wilds(con.Arity)}}, w...)
			}
		}
	}
	return append([]spat{wildPat}, w...)
}

func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
