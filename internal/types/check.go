package types

import (
	"fmt"
	"strings"

	"github.com/verniyyy/arkadia/internal/diag"
	"github.com/verniyyy/arkadia/internal/syntax"
)

// Checker type checks one module.
type Checker struct {
	g       *Global
	mod     *syntax.Module
	modName string
	scope   *Scope
	exports *Exports
	errs    diag.List
	level   int
	locals  *local
	wanted  *wantedScope
	givens  []*given
	nextLoc int

	// declarations by kind
	sigs     map[string]*syntax.DSig
	funs     []*syntax.DFun
	tests    []*syntax.DTest
	insts    []*syntax.DInstance
	instInfo map[*syntax.DInstance]*InstInfo
	classes  map[*syntax.DClass]*ClassInfo
	matches  []matchSite
}

type local struct {
	name string
	v    *localVar
	next *local
}

type localVar struct {
	pos    diag.Pos
	id     string
	scheme *Scheme // nil when monomorphic
	mono   Type
	group  *groupState
}

type groupState struct {
	info *syntax.BindGroupInfo
}

// CheckModule type checks a parsed module and registers its exports.
func CheckModule(g *Global, m *syntax.Module) error {
	c := &Checker{
		g:        g,
		mod:      m,
		modName:  m.Name,
		scope:    newScope(),
		exports:  newExports(m.Name),
		sigs:     map[string]*syntax.DSig{},
		instInfo: map[*syntax.DInstance]*InstInfo{},
		classes:  map[*syntax.DClass]*ClassInfo{},
	}
	c.wanted = &wantedScope{}
	c.run()
	if len(c.errs) > 0 {
		return c.errs
	}
	g.Modules[m.Name] = c.exports
	return nil
}

func (c *Checker) run() {
	if c.modName == PreludeMod {
		c.installBuiltins()
	}
	c.processImports()
	if len(c.errs) > 0 {
		return
	}
	c.collectTypeNames()
	c.inferDataKinds()
	c.processEffects()
	c.processClasses()
	if len(c.errs) > 0 {
		return
	}
	c.deriveInstances()
	c.processInstanceHeads()
	c.processForeigns()
	c.collectValues()
	if len(c.errs) > 0 {
		return
	}
	c.checkTopBindings()
	c.checkInstanceBodies()
	c.checkClassDefaults()
	c.checkTests()
	if len(c.errs) == 0 {
		c.checkMatches()
	}
}

func (c *Checker) record(pos diag.Pos, name string, t Type, sc *Scheme, def diag.Pos, kind string) {
	if !c.g.CollectInfo || pos.Line == 0 {
		return
	}
	c.g.Info = append(c.g.Info, &IdentInfo{Pos: pos, Len: len([]rune(name)), Name: name, Type: t, Scheme: sc, Def: def, Kind: kind})
}

func (c *Checker) errorf(pos diag.Pos, format string, args ...any) {
	c.errs = append(c.errs, diag.Errorf(pos, format, args...))
}

func (c *Checker) sym(name string) string { return c.modName + "." + name }

func (c *Checker) freshLocalID(name string) string {
	c.nextLoc++
	return fmt.Sprintf("%s_%d", sanitize(name), c.nextLoc)
}

var opNames = map[rune]string{
	'!': "bang", '#': "hash", '$': "dollar", '%': "pct", '&': "amp", '*': "star",
	'+': "plus", '.': "dot", '/': "slash", '<': "lt", '=': "eq", '>': "gt",
	'?': "qm", '@': "at", '\\': "bslash", '^': "caret", '|': "bar", '-': "minus",
	'~': "tilde", ':': "colon",
}

// sanitize turns a name (possibly an operator) into an identifier fragment.
func sanitize(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			sb.WriteRune(r)
		} else if r == '\'' {
			sb.WriteString("_q")
		} else if r > 127 {
			sb.WriteRune(r)
		} else if n, ok := opNames[r]; ok {
			sb.WriteString("_" + n)
		} else {
			sb.WriteString("_op")
		}
	}
	return sb.String()
}

// ------------------------------------------------------------------ builtins

func (c *Checker) installBuiltins() {
	prim := func(name string, k Kind) *TypeInfo {
		ti := &TypeInfo{Name: c.sym(name), Kind: k, Builtin: true, Module: PreludeMod}
		c.g.Types[ti.Name] = ti
		c.defineType(name, ti, diag.Pos{})
		return ti
	}
	for _, n := range []string{"Int", "Float", "String", "Char"} {
		prim(n, Star)
	}
	bool_ := prim("Bool", Star)
	c.addCtors(bool_, []string{"False", "True"}, [][]Type{nil, nil})
	unit := prim("Unit", Star)
	c.addCtors(unit, []string{"()"}, [][]Type{nil})
	list := prim("List", &KArrow{Star, Star})
	list.Params = []string{"a"}
	listA := mkApp(&TCon{list.Name, list.Kind}, &TGen{0})
	c.addCtors(list, []string{"Nil", "Cons"}, [][]Type{nil, {&TGen{0}, listA}})
	// `::` is an alias for Cons
	c.scope.Ctors["::"] = list.Ctors[1]
	c.exports.Ctors["::"] = list.Ctors[1]
	for n := 2; n <= 12; n++ {
		var k Kind = Star
		for i := 0; i < n; i++ {
			k = &KArrow{Star, k}
		}
		name := "Tuple" + itoa(n)
		ti := prim(name, k)
		var fields []Type
		for i := 0; i < n; i++ {
			ti.Params = append(ti.Params, string(rune('a'+i)))
			fields = append(fields, &TGen{i})
		}
		c.addCtors(ti, []string{name}, [][]Type{fields})
	}
	// IO is the builtin, unhandleable effect of the outside world
	io := &EffectInfo{Name: c.sym("IO"), Kind: Effect, Module: PreludeMod}
	c.g.Effects[io.Name] = io
	c.defineType("IO", io, diag.Pos{})
}

func (c *Checker) addCtors(ti *TypeInfo, names []string, fields [][]Type) {
	n := len(ti.Params)
	res := Type(&TCon{ti.Name, ti.Kind})
	for i := 0; i < n; i++ {
		res = &TApp{res, &TGen{i}}
	}
	for i, name := range names {
		ci := &CtorInfo{Name: c.sym(name), Short: name, Type: ti, Tag: i, Arity: len(fields[i]), Fields: fields[i]}
		ci.Scheme = ctorScheme(n, fields[i], res)
		ti.Ctors = append(ti.Ctors, ci)
		c.scope.Ctors[name] = ci
		if name != "()" && !strings.HasPrefix(name, "Tuple") {
			c.exports.Ctors[name] = ci
		}
	}
}

// ctorScheme builds the type of a constructor function. Every arrow gets
// its own effect variable: applying a constructor has no effect.
func ctorScheme(nparams int, fields []Type, res Type) *Scheme {
	kinds := make([]Kind, nparams, nparams+len(fields))
	for i := 0; i < nparams; i++ {
		kinds[i] = Star
	}
	t := res
	for i := len(fields) - 1; i >= 0; i-- {
		t = &TFun{Arg: fields[i], Eff: &TGen{nparams + i}, Res: t}
	}
	for range fields {
		kinds = append(kinds, Row)
	}
	return &Scheme{Kinds: kinds, Type: t}
}

// builtin lookups

func (c *Checker) builtinType(name string) *TCon {
	ti := c.g.Types[name]
	if ti == nil {
		panic("missing builtin type " + name)
	}
	return &TCon{ti.Name, ti.Kind}
}

func (c *Checker) tInt() Type    { return c.builtinType(TInt) }
func (c *Checker) tFloat() Type  { return c.builtinType(TFloat) }
func (c *Checker) tString() Type { return c.builtinType(TString) }
func (c *Checker) tChar() Type   { return c.builtinType(TChar) }
func (c *Checker) tBool() Type   { return c.builtinType(TBool) }
func (c *Checker) tUnit() Type   { return c.builtinType(TUnit) }
func (c *Checker) tList(a Type) Type {
	return &TApp{c.builtinType(TList), a}
}
func (c *Checker) tTuple(ts []Type) Type {
	return mkApp(c.builtinType(TupleName(len(ts))), ts...)
}

func (c *Checker) builtinCtor(typ string, tag int) *CtorInfo { return c.g.Types[typ].Ctors[tag] }

// ------------------------------------------------------------------ imports & scope

func (c *Checker) defineType(name string, v any, pos diag.Pos) {
	if old, ok := c.scope.Types[name]; ok && c.ownedHere(old) {
		c.errorf(pos, "type-level name '%s' is already defined", name)
		return
	}
	c.scope.Types[name] = v
	c.exports.Types[name] = v
}

func (c *Checker) ownedHere(v any) bool {
	switch v := v.(type) {
	case *TypeInfo:
		return v.Module == c.modName
	case *ClassInfo:
		return v.Module == c.modName
	case *EffectInfo:
		return v.Module == c.modName
	case *AliasInfo:
		return v.Module == c.modName
	}
	return false
}

func (c *Checker) defineValue(name string, v *ValueInfo, private bool) {
	if old, ok := c.scope.Values[name]; ok {
		if ov, ok := old.(*ValueInfo); ok && strings.HasPrefix(ov.Sym, c.modName+".") && ov.Sym == c.sym(name) {
			c.errorf(v.Pos, "'%s' is already defined", name)
			return
		}
	}
	c.scope.Values[name] = v
	if !private {
		c.exports.Values[name] = v
	}
	c.g.Values[v.Sym] = v
}

func (c *Checker) defineCtor(name string, ci *CtorInfo, pos diag.Pos, private bool) {
	if old, ok := c.scope.Ctors[name]; ok {
		if oc, ok := old.(*CtorInfo); ok && oc.Type.Module == c.modName {
			c.errorf(pos, "constructor '%s' is already defined", name)
			return
		}
	}
	c.scope.Ctors[name] = ci
	if !private {
		c.exports.Ctors[name] = ci
	}
}

func addUnqual(m map[string]any, name string, v any, mod string) {
	if old, ok := m[name]; ok {
		if same(old, v) {
			return
		}
		if a, ok := old.(ambiguous); ok {
			m[name] = ambiguous{append(a.mods, mod)}
			return
		}
		m[name] = ambiguous{[]string{moduleOf(old), mod}}
		return
	}
	m[name] = v
}

func same(a, b any) bool { return a == b }

func moduleOf(v any) string {
	switch v := v.(type) {
	case *ValueInfo:
		if i := strings.LastIndex(v.Sym, "."); i >= 0 {
			return v.Sym[:i]
		}
	case *TypeInfo:
		return v.Module
	case *ClassInfo:
		return v.Module
	case *EffectInfo:
		return v.Module
	case *AliasInfo:
		return v.Module
	case *CtorInfo:
		return v.Type.Module
	}
	return "?"
}

func (c *Checker) processImports() {
	imports := c.mod.Imports
	if c.modName != PreludeMod {
		hasPrelude := false
		for _, imp := range imports {
			if imp.Path == PreludeMod {
				hasPrelude = true
			}
		}
		if !hasPrelude {
			imports = append([]*syntax.Import{{Path: PreludeMod, All: true}}, imports...)
		}
	}
	for _, imp := range imports {
		ex := c.g.Modules[imp.Path]
		if ex == nil {
			c.errorf(imp.P, "unknown module '%s'", imp.Path)
			continue
		}
		alias := imp.Alias
		if alias == "" {
			parts := strings.Split(imp.Path, ".")
			alias = parts[len(parts)-1]
		}
		c.scope.Quals[alias] = append(c.scope.Quals[alias], ex)
		if imp.Alias == "" && alias != imp.Path {
			c.scope.Quals[imp.Path] = append(c.scope.Quals[imp.Path], ex)
		}
		// `import M` brings every name into scope (unqualified and as M.x);
		// `import M as X` only qualified; `import M (a, b)` selected names.
		if imp.All || (imp.Alias == "" && imp.Names == nil) {
			for n, v := range ex.Values {
				addUnqual(c.scope.Values, n, v, ex.Module)
			}
			for n, v := range ex.Types {
				addUnqual(c.scope.Types, n, v, ex.Module)
			}
			for n, v := range ex.Ctors {
				addUnqual(c.scope.Ctors, n, v, ex.Module)
			}
			continue
		}
		for _, in := range imp.Names {
			if in.Kind == 0 {
				v, ok := ex.Values[in.Name]
				if !ok {
					c.errorf(in.P, "module '%s' does not export '%s'", imp.Path, in.Name)
					continue
				}
				addUnqual(c.scope.Values, in.Name, v, ex.Module)
				continue
			}
			t, ok := ex.Types[in.Name]
			if !ok {
				c.errorf(in.P, "module '%s' does not export type '%s'", imp.Path, in.Name)
				continue
			}
			addUnqual(c.scope.Types, in.Name, t, ex.Module)
			if in.All {
				switch t := t.(type) {
				case *TypeInfo:
					for _, ci := range t.Ctors {
						if _, ok := ex.Ctors[ci.Short]; ok {
							addUnqual(c.scope.Ctors, ci.Short, ci, ex.Module)
						}
					}
				case *ClassInfo:
					for _, m := range t.Methods {
						if v, ok := ex.Values[m.Name]; ok {
							addUnqual(c.scope.Values, m.Name, v, ex.Module)
						}
					}
				case *EffectInfo:
					for _, op := range t.Ops {
						if v, ok := ex.Values[op.Name]; ok {
							addUnqual(c.scope.Values, op.Name, v, ex.Module)
						}
					}
				}
			}
		}
	}
}

// qualified resolves a module qualifier to its export tables.
func (c *Checker) qualified(pos diag.Pos, qual string) []*Exports {
	if qual == "#Prelude" {
		if c.modName == PreludeMod {
			return []*Exports{c.exports}
		}
		return []*Exports{c.g.Modules[PreludeMod]}
	}
	exs, ok := c.scope.Quals[qual]
	if !ok {
		if qual == c.modName {
			return []*Exports{c.exports}
		}
		c.errorf(pos, "unknown module qualifier '%s'", qual)
		return nil
	}
	return exs
}

func (c *Checker) lookupValue(pos diag.Pos, qual, name string) *ValueInfo {
	if qual != "" {
		exs := c.qualified(pos, qual)
		if exs == nil {
			return nil
		}
		for _, ex := range exs {
			if v, ok := ex.Values[name]; ok {
				return v
			}
		}
		if qual == "#Prelude" && c.modName == PreludeMod {
			if v, ok := c.scope.Values[name].(*ValueInfo); ok {
				return v
			}
		}
		c.errorf(pos, "module '%s' has no value '%s'", qual, name)
		return nil
	}
	switch v := c.scope.Values[name].(type) {
	case *ValueInfo:
		return v
	case ambiguous:
		c.errorf(pos, "'%s' is ambiguous: it is exported by %s", name, joinNames(v.mods))
		return nil
	}
	c.errorf(pos, "unknown variable '%s'", name)
	return nil
}

func (c *Checker) lookupCtor(pos diag.Pos, qual, name string) *CtorInfo {
	if qual != "" {
		exs := c.qualified(pos, qual)
		for _, ex := range exs {
			if v, ok := ex.Ctors[name]; ok {
				return v
			}
		}
		if qual == "#Prelude" {
			if v, ok := c.scope.Ctors[name].(*CtorInfo); ok {
				return v
			}
		}
		if exs != nil {
			c.errorf(pos, "module '%s' has no constructor '%s'", qual, name)
		}
		return nil
	}
	switch v := c.scope.Ctors[name].(type) {
	case *CtorInfo:
		return v
	case ambiguous:
		c.errorf(pos, "constructor '%s' is ambiguous: it is exported by %s", name, joinNames(v.mods))
		return nil
	}
	c.errorf(pos, "unknown constructor '%s'", name)
	return nil
}

func (c *Checker) lookupTypeName(pos diag.Pos, qual, name string) any {
	if qual != "" {
		exs := c.qualified(pos, qual)
		for _, ex := range exs {
			if v, ok := ex.Types[name]; ok {
				return v
			}
		}
		if qual == "#Prelude" {
			if v, ok := c.scope.Types[name]; ok {
				return v
			}
		}
		if exs != nil {
			c.errorf(pos, "module '%s' has no type '%s'", qual, name)
		}
		return nil
	}
	switch v := c.scope.Types[name].(type) {
	case ambiguous:
		c.errorf(pos, "type '%s' is ambiguous: it is exported by %s", name, joinNames(v.mods))
		return nil
	case nil:
		c.errorf(pos, "unknown type '%s'", name)
		return nil
	default:
		return v
	}
}

func (c *Checker) lookupClass(pos diag.Pos, qual, name string) *ClassInfo {
	v := c.lookupTypeName(pos, qual, name)
	if v == nil {
		return nil
	}
	cl, ok := v.(*ClassInfo)
	if !ok {
		c.errorf(pos, "'%s' is not a class", name)
		return nil
	}
	return cl
}

// ------------------------------------------------------------------ local environment

func (c *Checker) pushLocal(name string, v *localVar) {
	c.locals = &local{name: name, v: v, next: c.locals}
}

func (c *Checker) lookupLocal(name string) *localVar {
	for l := c.locals; l != nil; l = l.next {
		if l.name == name {
			return l.v
		}
	}
	return nil
}

// ------------------------------------------------------------------ type conversion

// arrow effect defaults for unannotated arrows
const (
	arrowAmbient = iota // share one implicit row variable (signatures)
	arrowPure           // closed empty row (data constructor fields)
	arrowFresh          // fresh meta per arrow (expression annotations)
)

// tvEnv tracks type variables while converting a written type.
type tvEnv struct {
	vars    map[string]Type
	kinds   map[string]Kind
	names   []string // TGen order
	gen     bool     // create TGen for new names (otherwise metas)
	fixed   bool     // reject unknown names
	arrows  int
	ambient Type
	spine   int
}

func newTVEnv(gen bool, arrows int) *tvEnv {
	return &tvEnv{vars: map[string]Type{}, kinds: map[string]Kind{}, gen: gen, arrows: arrows}
}

func (e *tvEnv) addGen(name string, k Kind) Type {
	t := &TGen{len(e.names)}
	e.vars[name] = t
	e.kinds[name] = k
	e.names = append(e.names, name)
	return t
}

func (c *Checker) tyVar(pos diag.Pos, env *tvEnv, name string, k Kind) Type {
	if t, ok := env.vars[name]; ok {
		if !unifyKind(env.kinds[name], k) {
			c.errorf(pos, "kind mismatch for type variable '%s': expected %s, but it is used as %s", name, KindString(k), KindString(env.kinds[name]))
		}
		return t
	}
	if env.fixed {
		c.errorf(pos, "unknown type variable '%s'", name)
		return c.freshType()
	}
	if env.gen {
		return env.addGen(name, k)
	}
	m := c.newMeta(k)
	env.vars[name] = m
	env.kinds[name] = k
	return m
}

func (c *Checker) ambientRow(env *tvEnv) Type {
	switch env.arrows {
	case arrowPure:
		return emptyRow
	case arrowFresh:
		return c.freshRow()
	}
	if env.ambient == nil {
		if env.gen {
			// "ε" cannot clash with user-written variables in practice
			env.ambient = env.addGen("ε", Row)
		} else {
			env.ambient = c.freshRow()
		}
	}
	return env.ambient
}

func newKVar(g *Global) *KVar { return &KVar{ID: g.freshID()} }

// convType converts a type expression, returning its kind.
func (c *Checker) convType(te syntax.TypeExpr, env *tvEnv) (Type, Kind) {
	switch te := te.(type) {
	case *syntax.TVarE:
		k := Kind(newKVar(c.g))
		if old, ok := env.kinds[te.Name]; ok {
			k = old
		}
		return c.tyVar(te.P, env, te.Name, k), k
	case *syntax.TConE:
		return c.convApp(te, nil, env)
	case *syntax.TAppE:
		if con, ok := te.Fun.(*syntax.TConE); ok {
			return c.convApp(con, te.Args, env)
		}
		f, kf := c.convType(te.Fun, env)
		for _, a := range te.Args {
			at, ka := c.convArg(a, env)
			kr := Kind(newKVar(c.g))
			if !unifyKind(kf, &KArrow{ka, kr}) {
				c.errorf(a.TPos(), "kind mismatch: cannot apply a type of kind %s to %s", KindString(kf), KindString(ka))
			}
			f, kf = &TApp{f, at}, kr
		}
		return f, kf
	case *syntax.TFunE:
		a := c.convStar(te.Arg, env)
		var eff Type
		if te.Eff != nil {
			eff = c.convRow(te.Eff, env)
		} else {
			eff = c.ambientRow(env)
		}
		r := c.convStar(te.Res, env)
		return &TFun{Arg: a, Eff: eff, Res: r}, Star
	case *syntax.TTupleE:
		var ts []Type
		for _, e := range te.Elems {
			t, k := c.convArg(e, env)
			if !unifyKind(k, Star) {
				c.errorf(e.TPos(), "expected a type, but this has kind %s", KindString(k))
			}
			ts = append(ts, t)
		}
		if len(ts) > 12 {
			c.errorf(te.P, "tuples may have at most 12 elements")
			return c.freshType(), Star
		}
		return c.tTuple(ts), Star
	case *syntax.TUnitE:
		return c.tUnit(), Star
	}
	panic(fmt.Sprintf("convType: %T", te))
}

// convArg converts a type argument (of a type constructor or tuple). In
// signatures, unannotated arrows inside type arguments are pure: a
// `List (Int -> Int)` holds pure functions.
func (c *Checker) convArg(te syntax.TypeExpr, env *tvEnv) (Type, Kind) {
	if env.arrows != arrowAmbient {
		return c.convType(te, env)
	}
	env.arrows = arrowPure
	t, k := c.convType(te, env)
	env.arrows = arrowAmbient
	return t, k
}

func (c *Checker) convStar(te syntax.TypeExpr, env *tvEnv) Type {
	t, k := c.convType(te, env)
	if !unifyKind(k, Star) {
		c.errorf(te.TPos(), "expected a type, but this has kind %s", KindString(k))
	}
	return t
}

func (c *Checker) convRow(r *syntax.RowE, env *tvEnv) Type {
	var labels []Type
	for _, l := range r.Labels {
		t, k := c.convType(l, env)
		if !unifyKind(k, Effect) {
			c.errorf(l.TPos(), "expected an effect, but this has kind %s", KindString(k))
		}
		labels = append(labels, t)
	}
	var tail Type = emptyRow
	if r.Tail != "" {
		tail = c.tyVar(r.P, env, r.Tail, Row)
	}
	return mkRow(labels, tail)
}

func (c *Checker) convApp(con *syntax.TConE, args []syntax.TypeExpr, env *tvEnv) (Type, Kind) {
	v := c.lookupTypeName(con.P, con.Qual, con.Name)
	var head Type
	var k Kind
	switch v := v.(type) {
	case nil:
		return c.freshType(), newKVar(c.g)
	case *TypeInfo:
		head, k = &TCon{v.Name, v.Kind}, v.Kind
	case *EffectInfo:
		head, k = &TCon{v.Name, v.Kind}, v.Kind
	case *AliasInfo:
		if len(args) < len(v.Params) {
			c.errorf(con.P, "type alias '%s' expects %d arguments", con.Name, len(v.Params))
			return c.freshType(), newKVar(c.g)
		}
		if v.expanding {
			c.errorf(con.P, "type alias '%s' is recursive", con.Name)
			return c.freshType(), newKVar(c.g)
		}
		// expand in the alias's defining scope with parameters bound
		sub := newTVEnv(false, env.arrows)
		sub.fixed = true
		for i, p := range v.Params {
			t, kk := c.convArg(args[i], env)
			sub.vars[p] = t
			sub.kinds[p] = kk
		}
		sub.ambient = env.ambient
		if env.arrows == arrowAmbient && env.ambient == nil {
			sub.ambient = c.ambientRow(env)
		}
		saved := c.scope
		if v.scope != nil {
			c.scope = v.scope
		}
		v.expanding = true
		head, k = c.convType(v.Body, sub)
		v.expanding = false
		c.scope = saved
		args = args[len(v.Params):]
	case *ClassInfo:
		c.errorf(con.P, "'%s' is a class, not a type", con.Name)
		return c.freshType(), newKVar(c.g)
	}
	for _, a := range args {
		at, ka := c.convArg(a, env)
		kr := Kind(newKVar(c.g))
		if !unifyKind(k, &KArrow{ka, kr}) {
			c.errorf(a.TPos(), "kind mismatch: '%s' cannot be applied to a type of kind %s", con.Name, KindString(ka))
			return c.freshType(), newKVar(c.g)
		}
		head, k = &TApp{head, at}, kr
	}
	return head, k
}

// convSpine converts the type of a definition. Along the spine of a
// curried function type, unannotated intermediate arrows (partial
// application) get their own polymorphic effect, while the final arrow gets
// the ambient effect.
func (c *Checker) convSpine(te syntax.TypeExpr, env *tvEnv) Type {
	f, ok := te.(*syntax.TFunE)
	if !ok || env.arrows != arrowAmbient || !env.gen {
		return c.convStar(te, env)
	}
	if _, inner := f.Res.(*syntax.TFunE); !inner || f.Eff != nil {
		return c.convStar(te, env)
	}
	a := c.convStar(f.Arg, env)
	env.spine++
	eff := env.addGen(fmt.Sprintf("ρ%d", env.spine), Row)
	r := c.convSpine(f.Res, env)
	return &TFun{Arg: a, Eff: eff, Res: r}
}

func rowTails(te syntax.TypeExpr, acc map[string]bool) {
	switch te := te.(type) {
	case *syntax.TFunE:
		if te.Eff != nil && te.Eff.Tail != "" {
			acc[te.Eff.Tail] = true
		}
		rowTails(te.Arg, acc)
		rowTails(te.Res, acc)
	case *syntax.TAppE:
		rowTails(te.Fun, acc)
		for _, a := range te.Args {
			rowTails(a, acc)
		}
	case *syntax.TTupleE:
		for _, e := range te.Elems {
			rowTails(e, acc)
		}
	}
}

// convScheme converts a written signature to a closed scheme. Free type
// variables are implicitly quantified.
func (c *Checker) convScheme(se *syntax.SchemeE, env *tvEnv) *Scheme {
	// A signature that names exactly one row variable uses it as the
	// ambient effect of its unannotated arrows.
	if env.arrows == arrowAmbient && env.gen && env.ambient == nil {
		tails := map[string]bool{}
		rowTails(se.Type, tails)
		if len(tails) == 1 {
			for name := range tails {
				if _, taken := env.vars[name]; !taken {
					env.ambient = env.addGen(name, Row)
				}
			}
		}
	}
	t := c.convSpine(se.Type, env)
	var preds []Pred
	for _, p := range se.Preds {
		cl := c.lookupClass(p.P, p.Qual, p.Class)
		if cl == nil {
			continue
		}
		pt, pk := c.convType(p.Type, env)
		if !unifyKind(pk, cl.ParamKind) {
			c.errorf(p.P, "kind mismatch in constraint %s: expected %s, got %s", p.Class, KindString(cl.ParamKind), KindString(pk))
		}
		preds = append(preds, Pred{cl, pt})
	}
	sc := &Scheme{Preds: preds, Type: t}
	for _, n := range env.names {
		sc.Kinds = append(sc.Kinds, DefaultKind(env.kinds[n]))
		sc.Names = append(sc.Names, n)
	}
	return sc
}

// sigScheme converts a top-level signature.
func (c *Checker) sigScheme(se *syntax.SchemeE) *Scheme {
	return c.convScheme(se, newTVEnv(true, arrowAmbient))
}
