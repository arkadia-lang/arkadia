package syntax

import "github.com/verniyyy/arkadia/internal/diag"

type Pos = diag.Pos

// ---------------------------------------------------------------- types

type TypeExpr interface{ TPos() Pos }

type TVarE struct {
	P    Pos
	Name string
}
type TConE struct {
	P    Pos
	Qual string
	Name string
}
type TAppE struct {
	P    Pos
	Fun  TypeExpr
	Args []TypeExpr
}

// TFunE is `Arg -> <Eff> Res`; Eff is nil when no row was written.
type TFunE struct {
	P   Pos
	Arg TypeExpr
	Eff *RowE
	Res TypeExpr
}
type TTupleE struct {
	P     Pos
	Elems []TypeExpr
}
type TUnitE struct{ P Pos }

// RowE is an effect row `<L1, L2 | tail>`.
type RowE struct {
	P      Pos
	Labels []TypeExpr
	Tail   string // "" = closed
}

func (t *TVarE) TPos() Pos   { return t.P }
func (t *TConE) TPos() Pos   { return t.P }
func (t *TAppE) TPos() Pos   { return t.P }
func (t *TFunE) TPos() Pos   { return t.P }
func (t *TTupleE) TPos() Pos { return t.P }
func (t *TUnitE) TPos() Pos  { return t.P }

// PredE is a class constraint `C t`.
type PredE struct {
	P     Pos
	Qual  string
	Class string
	Type  TypeExpr
}

// SchemeE is `ctx => type`.
type SchemeE struct {
	Preds []*PredE
	Type  TypeExpr
}

// ---------------------------------------------------------------- patterns

type Pattern interface{ PPos() Pos }

type PVar struct {
	P    Pos
	Name string
	ID   string // unique name assigned by the checker
}
type PWild struct{ P Pos }
type PLit struct {
	P   Pos
	Lit *ELit
}
type PCon struct {
	P    Pos
	Qual string
	Name string
	Args []Pattern
	Con  any // *types.CtorInfo, resolved by the checker
}
type PTuple struct {
	P     Pos
	Elems []Pattern
}
type PList struct {
	P     Pos
	Elems []Pattern
	Cons  any // resolved constructors (set by checker)
	Nil   any
}
type PAs struct {
	P    Pos
	Name string
	ID   string
	Pat  Pattern
}
type PUnit struct{ P Pos }
type PAnnot struct {
	P    Pos
	Pat  Pattern
	Type TypeExpr
}

func (p *PVar) PPos() Pos   { return p.P }
func (p *PWild) PPos() Pos  { return p.P }
func (p *PLit) PPos() Pos   { return p.P }
func (p *PCon) PPos() Pos   { return p.P }
func (p *PTuple) PPos() Pos { return p.P }
func (p *PList) PPos() Pos  { return p.P }
func (p *PAs) PPos() Pos    { return p.P }
func (p *PUnit) PPos() Pos  { return p.P }
func (p *PAnnot) PPos() Pos { return p.P }

// ---------------------------------------------------------------- expressions

type Expr interface{ EPos() Pos }

type LitKind int

const (
	LitInt LitKind = iota
	LitFloat
	LitString
	LitChar
)

type ELit struct {
	P     Pos
	Kind  LitKind
	Int   int64
	Float float64
	Str   string // string / char value
}

// EVar is a variable or operator reference. Fields after Name are filled in
// by the type checker.
type EVar struct {
	P     Pos
	Qual  string
	Name  string
	Ref   *VarRef
	Dicts []*DictRef
}

// VarRef is the resolution of a variable occurrence.
type VarRef struct {
	Local  bool
	ID     string         // local unique id or global symbol
	Group  *BindGroupInfo // non-nil for a monomorphic recursive reference
	Method any            // *types.MethodInfo for class methods
	Op     any            // *types.OpInfo for effect operations
	Ctor   any            // never set here (constructors use ECon)
}

type ECon struct {
	P    Pos
	Qual string
	Name string
	Con  any // *types.CtorInfo
}
type EApp struct {
	P    Pos
	Fun  Expr
	Args []Expr
	// Masks[i] lists effects (global names) that must be masked while the
	// application to argument i runs (filled by the checker when an
	// effect row was subsumed under extra handlers).
	Masks map[int][]string
}
type ELam struct {
	P      Pos
	Params []Pattern
	Body   Expr
}

// ELet is `let binds in body` (also produced for block statements).
type ELet struct {
	P     Pos
	Binds []*Bind
	Body  Expr
}

// EBlock is a sequence of statements; the value is the last expression.
type EBlock struct {
	P     Pos
	Stmts []Stmt
}
type EMatch struct {
	P     Pos
	Scrut Expr
	Arms  []*Arm
}
type EHandle struct {
	P      Pos
	Body   Expr
	Return *HandlerClause // may be nil
	Ops    []*HandlerClause
	Effect any // *types.EffectInfo
}
type ETuple struct {
	P     Pos
	Elems []Expr
}
type EList struct {
	P     Pos
	Elems []Expr
	Cons  any
	Nil   any
}
type EUnit struct{ P Pos }

// EOpChain is an unresolved sequence `e1 op1 e2 op2 e3 ...`.
type EOpChain struct {
	P        Pos
	Operands []Expr
	Ops      []*EVar
}

// EBinOp is a resolved binary operator application.
type EBinOp struct {
	P     Pos
	Op    *EVar
	Left  Expr
	Right Expr
}

// ENeg is prefix negation.
type ENeg struct {
	P Pos
	E Expr
}

// ESection is `(op e)` (Left == nil) or `(e op)` (Right == nil) or `(op)`.
type ESection struct {
	P     Pos
	Op    *EVar
	Left  Expr
	Right Expr
}
type EInterp struct {
	P     Pos
	Parts []Expr  // ELit strings and arbitrary expressions
	Disp  []*EVar // display references for expression parts (filled by checker)
}
type EAnnot struct {
	P    Pos
	E    Expr
	Type *SchemeE
}

func (e *ELit) EPos() Pos     { return e.P }
func (e *EVar) EPos() Pos     { return e.P }
func (e *ECon) EPos() Pos     { return e.P }
func (e *EApp) EPos() Pos     { return e.P }
func (e *ELam) EPos() Pos     { return e.P }
func (e *ELet) EPos() Pos     { return e.P }
func (e *EBlock) EPos() Pos   { return e.P }
func (e *EMatch) EPos() Pos   { return e.P }
func (e *EHandle) EPos() Pos  { return e.P }
func (e *ETuple) EPos() Pos   { return e.P }
func (e *EList) EPos() Pos    { return e.P }
func (e *EUnit) EPos() Pos    { return e.P }
func (e *EOpChain) EPos() Pos { return e.P }
func (e *EBinOp) EPos() Pos   { return e.P }
func (e *ENeg) EPos() Pos     { return e.P }
func (e *ESection) EPos() Pos { return e.P }
func (e *EInterp) EPos() Pos  { return e.P }
func (e *EAnnot) EPos() Pos   { return e.P }

type Stmt interface{ SPos() Pos }

type SExpr struct{ E Expr }
type SLet struct{ Bind *Bind }

func (s *SExpr) SPos() Pos { return s.E.EPos() }
func (s *SLet) SPos() Pos  { return s.Bind.P }

// Arm is a match arm: a pattern followed by either a body or guards.
type Arm struct {
	P    Pos
	Pat  Pattern
	Body *Rhs
}

// Rhs is either a plain body or a list of guarded bodies.
type Rhs struct {
	Body   Expr
	Guards []*Guard
}
type Guard struct {
	P    Pos
	Cond Expr
	Body Expr
}

// HandlerClause is `return x -> e` or `op p1 .. pn k -> e`.
type HandlerClause struct {
	P      Pos
	Name   string // "" for return
	Params []Pattern
	Body   Expr
	Op     any // *types.OpInfo
}

// Clause is one equation of a function definition.
type Clause struct {
	P      Pos
	Params []Pattern
	Rhs    *Rhs
}

// Bind is a (possibly multi-clause) function or a pattern binding.
type Bind struct {
	P       Pos
	Name    string    // function name ("" for pattern binding)
	Pat     Pattern   // pattern binding (Name == "")
	Clauses []*Clause // for function bindings
	Sig     *SchemeE  // optional local signature
	ID      string    // unique id assigned by checker
	Info    *BindInfo
}

// BindInfo records elaboration results for a binding.
type BindInfo struct {
	DictParams []string // dictionary parameter names (in scheme order)
	Group      *BindGroupInfo
	Scheme     any // *types.Scheme
}

type BindGroupInfo struct {
	DictParams []string
}

// DictRef is a placeholder for a dictionary argument, solved by the checker.
type DictRef struct {
	Class string
	Sol   DictExpr
}

type DictExpr interface{ isDict() }

// DictInst applies an instance to dictionaries for its context.
type DictInst struct {
	Inst any // *types.InstInfo
	Args []DictExpr
}

// DictParam refers to a dictionary parameter in scope.
type DictParam struct{ Name string }

// DictSuper extracts a superclass dictionary.
type DictSuper struct {
	From  DictExpr
	Index int // slot in the dictionary
}

func (*DictInst) isDict()  {}
func (*DictParam) isDict() {}
func (*DictSuper) isDict() {}

// ---------------------------------------------------------------- declarations

type Decl interface{ DPos() Pos }

type Import struct {
	P     Pos
	Path  string
	Alias string
	Names []ImportName
	All   bool
}
type ImportName struct {
	P    Pos
	Name string
	Kind int  // 0 value, 1 type/class/effect
	All  bool // T(..)
}

type DSig struct {
	P       Pos
	Name    string
	Scheme  *SchemeE
	Private bool
}
type DFun struct {
	P       Pos
	Bind    *Bind
	Private bool
}
type CtorDecl struct {
	P      Pos
	Name   string
	Fields []TypeExpr
	Named  []string // record field names (nil for positional)
}
type DType struct {
	P        Pos
	Name     string
	Params   []string
	Ctors    []*CtorDecl
	Deriving []string
	Private  bool
}
type DAlias struct {
	P      Pos
	Name   string
	Params []string
	Type   TypeExpr
}
type DClass struct {
	P        Pos
	Name     string
	Param    string
	Supers   []*PredE
	Sigs     []*DSig
	Defaults []*Bind
}
type DInstance struct {
	P       Pos
	Context []*PredE
	Qual    string
	Class   string
	Head    TypeExpr
	Binds   []*Bind
	Derived bool
}
type DEffect struct {
	P      Pos
	Name   string
	Params []string
	Ops    []*DSig
}
type DForeign struct {
	P      Pos
	Name   string
	Scheme *SchemeE
	Target string
}
type DFixity struct {
	P     Pos
	Assoc int // 0 none, 1 left, 2 right
	Prec  int
	Ops   []string
}
type DTest struct {
	P    Pos
	Name string
	Body Expr
	ID   string
}

func (d *DSig) DPos() Pos      { return d.P }
func (d *DFun) DPos() Pos      { return d.P }
func (d *DType) DPos() Pos     { return d.P }
func (d *DAlias) DPos() Pos    { return d.P }
func (d *DClass) DPos() Pos    { return d.P }
func (d *DInstance) DPos() Pos { return d.P }
func (d *DEffect) DPos() Pos   { return d.P }
func (d *DForeign) DPos() Pos  { return d.P }
func (d *DFixity) DPos() Pos   { return d.P }
func (d *DTest) DPos() Pos     { return d.P }

// Module is a parsed source file.
type Module struct {
	Name    string
	File    string
	Imports []*Import
	Decls   []Decl
}

// A DictRef can appear inside dictionary expressions (solved later).
func (*DictRef) isDict() {}
