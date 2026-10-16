// Package core defines the untyped intermediate language that the type
// checked AST is lowered to before code generation. Type classes are
// already turned into explicit dictionaries, and syntactic sugar is gone.
package core

import "github.com/verniyyy/arkadia/internal/types"

type Expr interface{ isExpr() }

// Var is a local variable.
type Var struct{ Name string }

// Global refers to a top-level symbol.
type Global struct{ Sym string }

// Lit is a literal: int64, float64, string, rune, bool or Unit{}.
type Lit struct{ Val any }

// Unit is the unit literal value.
type Unit struct{}

// App applies a function to arguments.
type App struct {
	Fn   Expr
	Args []Expr
}

// Lam is a curried function of len(Params) arguments.
type Lam struct {
	Params []string
	Body   Expr
}

type Let struct {
	Name string
	Val  Expr
	Body Expr
}

// LetRec binds mutually recursive local functions.
type LetRec struct {
	Names []string
	Lams  []*Lam
	Body  Expr
}

// Con is a saturated constructor application.
type Con struct {
	Ctor *types.CtorInfo
	Args []Expr
}

// Match tests rows of patterns in order against the scrutinees.
type Match struct {
	Scruts []Expr
	Rows   []*Row
	Where  string // source position for match failure
}

// Row is one alternative: patterns plus either a body or guards.
type Row struct {
	Pats   []Pat
	Body   Expr     // nil when guarded
	Guards []*Guard // tried in order; falls through when all fail
}

type Guard struct {
	Cond Expr
	Body Expr
}

type If struct{ Cond, Then, Else Expr }

// Select reads a dictionary slot.
type Select struct {
	Dict Expr
	Slot int
}

// Handle installs an effect handler around Body.
type Handle struct {
	Eff   string
	Body  Expr
	Ret   *Lam // nil = identity
	Ops   []*Lam
	UsesK []bool
	Tail  bool
	// Direct is set when every clause either resumes in tail position or
	// never resumes (abortive); such handlers run without goroutines.
	Direct bool
	// State-passing variant, set when the handler has the shape
	//   return x -> \s -> r      op ... k -> \s -> ... k v s'
	// SRet takes (x, s); SOps take (args..., s) and return the pair
	// (v, s'). Used when the handler is immediately applied to a state.
	SRet *Lam
	SOps []*Lam
}

// Concat concatenates strings (interpolation).
type Concat struct{ Parts []Expr }

// Mask runs Body with the innermost handlers of the given effects hidden.
type Mask struct {
	Effs []string
	Body Expr
}

func (*Var) isExpr()    {}
func (*Global) isExpr() {}
func (*Lit) isExpr()    {}
func (*App) isExpr()    {}
func (*Lam) isExpr()    {}
func (*Let) isExpr()    {}
func (*LetRec) isExpr() {}
func (*Con) isExpr()    {}
func (*Match) isExpr()  {}
func (*If) isExpr()     {}
func (*Select) isExpr() {}
func (*Handle) isExpr() {}
func (*Concat) isExpr() {}
func (*Mask) isExpr()   {}

type Pat interface{ isPat() }

type PWild struct{}
type PVar struct{ Name string }
type PAs struct {
	Name string
	Pat  Pat
}
type PLit struct{ Val any }
type PCon struct {
	Ctor *types.CtorInfo
	Args []Pat
}

func (*PWild) isPat() {}
func (*PVar) isPat()  {}
func (*PAs) isPat()   {}
func (*PLit) isPat()  {}
func (*PCon) isPat()  {}

// ---------------------------------------------------------------- program

// Func is a top-level function of fixed arity (> 0).
type Func struct {
	Sym    string
	Params []string
	Body   Expr
}

// Value is a top-level constant evaluated at program start.
type Value struct {
	Sym  string
	Body Expr
}

// Dict is an instance dictionary. With Params it is a function from the
// context dictionaries to a fresh dictionary; slots may refer to Var "self".
type Dict struct {
	Sym    string
	Params []string
	Slots  []Expr
}

// Foreign binds a Go function.
type Foreign struct {
	Sym    string
	Target string
	Arity  int
}

// Op is the function performing an effect operation.
type Op struct {
	Sym   string
	Eff   string
	Index int
	Arity int
}

// Effect lists the operations of an effect (for diagnostics).
type Effect struct {
	Name string
	Ops  []string
}

type Test struct {
	Name string
	Sym  string // a Value of type Unit -> <Fail, IO> Unit
}

type Program struct {
	Funcs    []*Func
	Values   []*Value // in initialization order
	Dicts    []*Dict
	Foreigns []*Foreign
	Ops      []*Op
	Effects  []*Effect
	Main     string // symbol of main (empty for libraries / tests)
	Tests    []*Test
	FailEff  string // name of the Prelude Fail effect (for the test runner)
}
