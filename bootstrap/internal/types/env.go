package types

import (
	"github.com/arkadia-lang/arkadia/bootstrap/internal/diag"
	"github.com/arkadia-lang/arkadia/bootstrap/internal/syntax"
)

// TypeInfo describes a data type or builtin type constructor.
type TypeInfo struct {
	Name    string // global name
	Kind    Kind
	Params  []string
	Ctors   []*CtorInfo
	Builtin bool
	Module  string
	Pos     diag.Pos
}

// CtorInfo describes a data constructor.
type CtorInfo struct {
	Name   string // global name (Module.Ctor)
	Short  string
	Type   *TypeInfo
	Tag    int
	Arity  int
	Scheme *Scheme // constructor function type
	Fields []Type  // field types in terms of TGen (type params)
	// FieldNames names the fields of a record constructor (nil otherwise).
	FieldNames []string
}

// FieldIndex returns the position of a named field, or -1.
func (ci *CtorInfo) FieldIndex(name string) int {
	for i, n := range ci.FieldNames {
		if n == name {
			return i
		}
	}
	return -1
}

// AliasInfo describes a type alias.
type AliasInfo struct {
	Name   string
	Params []string
	Body   syntax.TypeExpr
	Module string
	scope  *Scope
	// expanding guards against recursive aliases
	expanding bool
}

// ClassInfo describes a type class.
type ClassInfo struct {
	Name      string
	ParamKind Kind
	Supers    []*ClassInfo
	Methods   []*MethodInfo
	Defaults  map[string]string // method name -> default implementation symbol
	Module    string
	Pos       diag.Pos
}

// NumSlots is the number of dictionary slots (superclasses + methods).
func (c *ClassInfo) NumSlots() int { return len(c.Supers) + len(c.Methods) }

func (c *ClassInfo) Method(name string) *MethodInfo {
	for _, m := range c.Methods {
		if m.Name == name {
			return m
		}
	}
	return nil
}

// MethodInfo describes a class method.
type MethodInfo struct {
	Class  *ClassInfo
	Name   string
	Slot   int     // dictionary slot index
	Scheme *Scheme // TGen 0 is the class variable; Preds[0] is the class itself
	// own predicates beyond the class constraint are Scheme.Preds[1:]
}

// InstInfo describes an instance declaration.
type InstInfo struct {
	Class   *ClassInfo
	TyCon   string // head type constructor name
	NVars   int
	Kinds   []Kind
	Head    Type              // in terms of TGen
	Context []Pred            // constraints on TGen vars
	Sym     string            // dictionary symbol
	Methods map[string]string // method name -> implementation symbol
	// SuperDicts gives, per superclass, how to obtain its dictionary in terms
	// of the context dictionary parameters CtxParams.
	SuperDicts []syntax.DictExpr
	CtxParams  []string
	Module     string
	Pos        diag.Pos
	Decl       *syntax.DInstance
}

// EffectInfo describes an effect declaration.
type EffectInfo struct {
	Name    string
	Kind    Kind
	NParams int
	Ops     []*OpInfo
	Module  string
	Pos     diag.Pos
}

// OpInfo describes an effect operation.
type OpInfo struct {
	Effect *EffectInfo
	Name   string
	Index  int
	Arity  int
	Sym    string
	// Scheme of the operation as a function. TGen 0..NParams-1 are the
	// effect's parameters, followed by operation-local variables.
	Scheme  *Scheme
	Params  []Type // parameter types (TGen terms)
	Res     Type   // result type (TGen terms)
	Ambient int    // index of the ambient row variable
}

// ValueInfo describes a top-level value-level entity.
type ValueInfo struct {
	Name    string // short name
	Sym     string // global symbol
	Scheme  *Scheme
	Method  *MethodInfo
	Op      *OpInfo
	Foreign string // foreign target, if any
	Arity   int    // syntactic arity for functions
	Pos     diag.Pos
	Bind    *syntax.Bind
	// for monomorphic recursive references during group inference
	group     *groupState
	groupMono Type
}

// Exports is the public interface of a module.
type Exports struct {
	Module string
	Values map[string]*ValueInfo
	Types  map[string]any // *TypeInfo | *ClassInfo | *EffectInfo | *AliasInfo
	Ctors  map[string]*CtorInfo
}

func newExports(mod string) *Exports {
	return &Exports{Module: mod, Values: map[string]*ValueInfo{}, Types: map[string]any{}, Ctors: map[string]*CtorInfo{}}
}

// ambiguous marks a name imported unqualified from several modules.
type ambiguous struct{ mods []string }

// Scope is the top-level naming environment of a module being checked.
type Scope struct {
	Values map[string]any // *ValueInfo | ambiguous
	Types  map[string]any // *TypeInfo | *ClassInfo | *EffectInfo | *AliasInfo | ambiguous
	Ctors  map[string]any // *CtorInfo | ambiguous
	Quals  map[string][]*Exports
}

func newScope() *Scope {
	return &Scope{Values: map[string]any{}, Types: map[string]any{}, Ctors: map[string]any{}, Quals: map[string][]*Exports{}}
}

// Global holds information shared across all modules of a program.
type Global struct {
	Modules   map[string]*Exports
	Instances map[string]map[string]*InstInfo // class -> tycon -> instance
	InstList  []*InstInfo
	Types     map[string]*TypeInfo
	Classes   map[string]*ClassInfo
	Effects   map[string]*EffectInfo
	Values    map[string]*ValueInfo // by symbol
	Tests     []*TestInfo
	nextID    int

	// CollectInfo enables recording of identifier information for editor
	// tooling (hover, go to definition).
	CollectInfo bool
	Info        []*IdentInfo
}

// IdentInfo describes an identifier occurrence for editor tooling.
type IdentInfo struct {
	Pos    diag.Pos
	Len    int
	Name   string
	Type   Type    // monomorphic type at this occurrence (zonk before use)
	Scheme *Scheme // for definitions with a polymorphic type
	Def    diag.Pos
	Kind   string // "local", "value", "constructor", "method", "operation", "definition"
}

// Describe renders the type information of an identifier.
func (i *IdentInfo) Describe() string {
	if i.Scheme != nil {
		return i.Name + " : " + SchemeString(i.Scheme)
	}
	return i.Name + " : " + TypeString(Zonk(i.Type))
}

// TestInfo describes a `test` declaration.
type TestInfo struct {
	Name   string
	Sym    string
	Module string
	Decl   *syntax.DTest
}

func NewGlobal() *Global {
	g := &Global{
		Modules:   map[string]*Exports{},
		Instances: map[string]map[string]*InstInfo{},
		Types:     map[string]*TypeInfo{},
		Classes:   map[string]*ClassInfo{},
		Effects:   map[string]*EffectInfo{},
		Values:    map[string]*ValueInfo{},
	}
	return g
}

// Builtin type names.
const (
	PreludeMod = "Prelude"
	TInt       = "Prelude.Int"
	TFloat     = "Prelude.Float"
	TString    = "Prelude.String"
	TChar      = "Prelude.Char"
	TBool      = "Prelude.Bool"
	TUnit      = "Prelude.Unit"
	TList      = "Prelude.List"
	TIO        = "Prelude.IO"
)

func TupleName(n int) string { return "Prelude.Tuple" + itoa(n) }

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

func (g *Global) freshID() int {
	g.nextID++
	return g.nextID
}
