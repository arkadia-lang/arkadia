package types

import (
	"github.com/verniyyy/arkadia/bootstrap/internal/diag"
	"github.com/verniyyy/arkadia/bootstrap/internal/syntax"
)

// ------------------------------------------------------------------ type-level names

func (c *Checker) collectTypeNames() {
	for _, d := range c.mod.Decls {
		switch d := d.(type) {
		case *syntax.DType:
			var k Kind = Star
			kinds := make([]Kind, len(d.Params))
			for i := range d.Params {
				kinds[i] = newKVar(c.g)
			}
			for i := len(kinds) - 1; i >= 0; i-- {
				k = &KArrow{kinds[i], k}
			}
			ti := &TypeInfo{Name: c.sym(d.Name), Kind: k, Params: d.Params, Module: c.modName, Pos: d.P}
			c.g.Types[ti.Name] = ti
			c.defineType(d.Name, ti, d.P)
		case *syntax.DAlias:
			c.defineType(d.Name, &AliasInfo{Name: c.sym(d.Name), Params: d.Params, Body: d.Type, Module: c.modName, scope: c.scope}, d.P)
		case *syntax.DEffect:
			var k Kind = Effect
			for range d.Params {
				k = &KArrow{newKVar(c.g), k}
			}
			ei := &EffectInfo{Name: c.sym(d.Name), Kind: k, NParams: len(d.Params), Module: c.modName, Pos: d.P}
			c.g.Effects[ei.Name] = ei
			c.defineType(d.Name, ei, d.P)
		case *syntax.DClass:
			ci := &ClassInfo{Name: c.sym(d.Name), ParamKind: newKVar(c.g), Defaults: map[string]string{}, Module: c.modName, Pos: d.P}
			c.g.Classes[ci.Name] = ci
			c.classes[d] = ci
			c.defineType(d.Name, ci, d.P)
		}
	}
}

func paramKinds(k Kind, n int) []Kind {
	var ks []Kind
	for i := 0; i < n; i++ {
		a := pruneKind(k).(*KArrow)
		ks = append(ks, a.From)
		k = a.To
	}
	return ks
}

func (c *Checker) inferDataKinds() {
	type pending struct {
		d      *syntax.DType
		ti     *TypeInfo
		fields [][]Type
	}
	var ps []pending
	for _, d := range c.mod.Decls {
		dt, ok := d.(*syntax.DType)
		if !ok {
			continue
		}
		ti, ok := c.scope.Types[dt.Name].(*TypeInfo)
		if !ok || ti.Module != c.modName {
			continue
		}
		env := newTVEnv(true, arrowPure)
		for i, k := range paramKinds(ti.Kind, len(dt.Params)) {
			if _, dup := env.vars[dt.Params[i]]; dup {
				c.errorf(dt.P, "duplicate type parameter '%s'", dt.Params[i])
			}
			env.addGen(dt.Params[i], k)
		}
		env.fixed = true
		p := pending{d: dt, ti: ti}
		for _, cd := range dt.Ctors {
			var fs []Type
			for _, f := range cd.Fields {
				fs = append(fs, c.convStar(f, env))
			}
			p.fields = append(p.fields, fs)
		}
		ps = append(ps, p)
	}
	for _, p := range ps {
		p.ti.Kind = DefaultKind(p.ti.Kind)
		n := len(p.d.Params)
		res := Type(&TCon{p.ti.Name, p.ti.Kind})
		for i := 0; i < n; i++ {
			res = &TApp{res, &TGen{i}}
		}
		pks := paramKinds(p.ti.Kind, n)
		for i, cd := range p.d.Ctors {
			ci := &CtorInfo{Name: c.sym(cd.Name), Short: cd.Name, Type: p.ti, Tag: i, Arity: len(cd.Fields), Fields: p.fields[i], FieldNames: cd.Named}
			ci.Scheme = ctorScheme(n, p.fields[i], res)
			copy(ci.Scheme.Kinds, pks)
			p.ti.Ctors = append(p.ti.Ctors, ci)
			c.defineCtor(cd.Name, ci, cd.P, p.d.Private)
		}
		c.checkFieldDecls(p.d, p.ti)
	}
}

// checkFieldDecls rejects duplicate field names, and a field shared by
// several constructors with different types.
func (c *Checker) checkFieldDecls(d *syntax.DType, ti *TypeInfo) {
	types := map[string]Type{}
	for i, ci := range ti.Ctors {
		seen := map[string]bool{}
		for j, f := range ci.FieldNames {
			if seen[f] {
				c.errorf(d.Ctors[i].P, "field '%s' is declared twice in constructor %s", f, ci.Short)
				continue
			}
			seen[f] = true
			if t, ok := types[f]; ok && !typesEqual(t, ci.Fields[j]) {
				c.errorf(d.Ctors[i].P, "field '%s' has different types in the constructors of %s", f, d.Name)
			}
			types[f] = ci.Fields[j]
		}
	}
}

// ------------------------------------------------------------------ effects

func (c *Checker) processEffects() {
	for _, d := range c.mod.Decls {
		de, ok := d.(*syntax.DEffect)
		if !ok {
			continue
		}
		ei := c.scope.Types[de.Name].(*EffectInfo)
		pks := paramKinds(ei.Kind, len(de.Params))
		label := Type(&TCon{ei.Name, ei.Kind})
		for i := range de.Params {
			label = &TApp{label, &TGen{i}}
		}
		for i, op := range de.Ops {
			env := newTVEnv(true, arrowAmbient)
			for j, p := range de.Params {
				env.addGen(p, pks[j])
			}
			if len(op.Scheme.Preds) > 0 {
				c.errorf(op.P, "effect operations cannot have class constraints")
			}
			t := c.convSpine(op.Scheme.Type, env)
			f, ok := t.(*TFun)
			if !ok {
				c.errorf(op.P, "operation '%s' must have a function type (use `Unit -> T` for operations without arguments)", op.Name)
				continue
			}
			oi := &OpInfo{Effect: ei, Name: op.Name, Index: i, Sym: c.sym(op.Name), Ambient: -1}
			// walk the spine and add the effect label to the last arrow
			var params []Type
			var build func(t *TFun) Type
			build = func(t *TFun) Type {
				params = append(params, t.Arg)
				if r, ok := t.Res.(*TFun); ok {
					return &TFun{Arg: t.Arg, Eff: t.Eff, Res: build(r)}
				}
				oi.Res = t.Res
				return &TFun{Arg: t.Arg, Eff: &TRowExt{label, t.Eff}, Res: t.Res}
			}
			full := build(f)
			oi.Params = params
			oi.Arity = len(params)
			if g, ok := env.ambient.(*TGen); ok {
				oi.Ambient = g.Index
			}
			sc := &Scheme{Type: full}
			for _, n := range env.names {
				sc.Kinds = append(sc.Kinds, env.kinds[n])
				sc.Names = append(sc.Names, n)
			}
			oi.Scheme = sc
			ei.Ops = append(ei.Ops, oi)
			c.defineValue(op.Name, &ValueInfo{Name: op.Name, Sym: oi.Sym, Scheme: sc, Op: oi, Arity: oi.Arity, Pos: op.P}, false)
		}
		ei.Kind = DefaultKind(ei.Kind)
		for _, oi := range ei.Ops {
			for i, k := range oi.Scheme.Kinds {
				oi.Scheme.Kinds[i] = DefaultKind(k)
			}
		}
	}
}

// ------------------------------------------------------------------ classes

func (c *Checker) processClasses() {
	var decls []*syntax.DClass
	for _, d := range c.mod.Decls {
		if dc, ok := d.(*syntax.DClass); ok {
			decls = append(decls, dc)
		}
	}
	for _, d := range decls {
		ci := c.classes[d]
		for _, s := range d.Supers {
			sc := c.lookupClass(s.P, s.Qual, s.Class)
			if sc == nil {
				continue
			}
			if v, ok := s.Type.(*syntax.TVarE); !ok || v.Name != d.Param {
				c.errorf(s.P, "superclass constraint must be on the class parameter '%s'", d.Param)
				continue
			}
			if !unifyKind(sc.ParamKind, ci.ParamKind) {
				c.errorf(s.P, "kind mismatch between class %s and superclass %s", d.Name, s.Class)
			}
			ci.Supers = append(ci.Supers, sc)
		}
	}
	for _, d := range decls {
		ci := c.classes[d]
		for i, s := range d.Sigs {
			env := newTVEnv(true, arrowAmbient)
			env.addGen(d.Param, ci.ParamKind)
			sc := c.convScheme(s.Scheme, env)
			mentions := false
			var check func(t Type)
			check = func(t Type) {
				switch t := t.(type) {
				case *TGen:
					if t.Index == 0 {
						mentions = true
					}
				case *TApp:
					check(t.Fun)
					check(t.Arg)
				case *TFun:
					check(t.Arg)
					check(t.Eff)
					check(t.Res)
				case *TRowExt:
					check(t.Label)
					check(t.Rest)
				}
			}
			check(sc.Type)
			if !mentions {
				c.errorf(s.P, "the type of method '%s' must mention the class parameter '%s'", s.Name, d.Param)
			}
			sc.Preds = append([]Pred{{ci, &TGen{0}}}, sc.Preds...)
			mi := &MethodInfo{Class: ci, Name: s.Name, Slot: len(ci.Supers) + i, Scheme: sc}
			ci.Methods = append(ci.Methods, mi)
			c.defineValue(s.Name, &ValueInfo{Name: s.Name, Sym: c.sym(s.Name), Scheme: sc, Method: mi, Pos: s.P}, false)
		}
		ci.ParamKind = DefaultKind(ci.ParamKind)
		for _, mi := range ci.Methods {
			mi.Scheme.Kinds[0] = ci.ParamKind
		}
		for _, b := range d.Defaults {
			if ci.Method(b.Name) == nil {
				c.errorf(b.P, "'%s' is not a method of class %s", b.Name, d.Name)
				continue
			}
			ci.Defaults[b.Name] = c.sym(d.Name + "$dflt$" + sanitize(b.Name))
		}
	}
}

// ------------------------------------------------------------------ deriving

func pv(name string) *syntax.EVar { return &syntax.EVar{Qual: "#Prelude", Name: name} }

func (c *Checker) deriveInstances() {
	var extra []syntax.Decl
	for _, d := range c.mod.Decls {
		dt, ok := d.(*syntax.DType)
		if !ok || len(dt.Deriving) == 0 {
			continue
		}
		ti := c.scope.Types[dt.Name].(*TypeInfo)
		pks := paramKinds(ti.Kind, len(dt.Params))
		for _, cls := range dt.Deriving {
			qual := "#Prelude"
			if cls == "ToJson" || cls == "FromJson" {
				qual = "#" + jsonMod
				if c.g.Modules[jsonMod] == nil {
					c.errorf(dt.P, "deriving %s requires importing %s", cls, jsonMod)
					continue
				}
				if len(dt.Ctors) == 0 {
					c.errorf(dt.P, "cannot derive %s for a type without constructors", cls)
					continue
				}
			}
			inst := &syntax.DInstance{P: dt.P, Qual: qual, Class: cls, Derived: true}
			var head syntax.TypeExpr = &syntax.TConE{P: dt.P, Name: dt.Name}
			if len(dt.Params) > 0 {
				var args []syntax.TypeExpr
				for i, p := range dt.Params {
					args = append(args, &syntax.TVarE{P: dt.P, Name: p})
					if _, ok := pruneKind(pks[i]).(KStar); ok {
						inst.Context = append(inst.Context, &syntax.PredE{P: dt.P, Qual: qual, Class: cls, Type: &syntax.TVarE{P: dt.P, Name: p}})
					}
				}
				head = &syntax.TAppE{P: dt.P, Fun: head, Args: args}
			}
			inst.Head = head
			var b *syntax.Bind
			switch cls {
			case "Eq":
				b = deriveEq(dt)
			case "Ord":
				b = deriveOrd(dt)
			case "Show":
				b = deriveShow(dt)
			case "ToJson":
				b = deriveToJson(dt)
			case "FromJson":
				b = deriveFromJson(dt)
			default:
				c.errorf(dt.P, "cannot derive class '%s' (only Eq, Ord, Show, ToJson and FromJson can be derived)", cls)
				continue
			}
			inst.Binds = []*syntax.Bind{b}
			extra = append(extra, inst)
		}
	}
	c.mod.Decls = append(c.mod.Decls, extra...)
}

func ctorPat(p diag.Pos, cd *syntax.CtorDecl, prefix string) (syntax.Pattern, []syntax.Expr) {
	pc := &syntax.PCon{P: p, Name: cd.Name}
	var vars []syntax.Expr
	for i := range cd.Fields {
		n := prefix + itoa(i)
		pc.Args = append(pc.Args, &syntax.PVar{P: p, Name: n})
		vars = append(vars, &syntax.EVar{P: p, Name: n})
	}
	return pc, vars
}

func simpleBind(p diag.Pos, name string, params []string, body syntax.Expr) *syntax.Bind {
	var ps []syntax.Pattern
	for _, n := range params {
		ps = append(ps, &syntax.PVar{P: p, Name: n})
	}
	return &syntax.Bind{P: p, Name: name, Clauses: []*syntax.Clause{{P: p, Params: ps, Rhs: &syntax.Rhs{Body: body}}}}
}

func app(f syntax.Expr, args ...syntax.Expr) syntax.Expr {
	return &syntax.EApp{P: f.EPos(), Fun: f, Args: args}
}

func ev(p diag.Pos, n string) *syntax.EVar { return &syntax.EVar{P: p, Name: n} }

func pairScrut(p diag.Pos) syntax.Expr {
	return &syntax.ETuple{P: p, Elems: []syntax.Expr{ev(p, "x$"), ev(p, "y$")}}
}

func deriveEq(dt *syntax.DType) *syntax.Bind {
	p := dt.P
	m := &syntax.EMatch{P: p, Scrut: pairScrut(p)}
	for _, cd := range dt.Ctors {
		pa, va := ctorPat(p, cd, "a$")
		pb, vb := ctorPat(p, cd, "b$")
		var body syntax.Expr = &syntax.ECon{P: p, Qual: "#Prelude", Name: "True"}
		for i := len(va) - 1; i >= 0; i-- {
			eq := &syntax.EBinOp{P: p, Op: &syntax.EVar{P: p, Qual: "#Prelude", Name: "=="}, Left: va[i], Right: vb[i]}
			if i == len(va)-1 {
				body = eq
			} else {
				body = &syntax.EBinOp{P: p, Op: ev(p, "&&"), Left: eq, Right: body}
			}
		}
		m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: &syntax.PTuple{P: p, Elems: []syntax.Pattern{pa, pb}}, Body: &syntax.Rhs{Body: body}})
	}
	if len(dt.Ctors) > 1 {
		m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: &syntax.PWild{P: p}, Body: &syntax.Rhs{Body: &syntax.ECon{P: p, Qual: "#Prelude", Name: "False"}}})
	}
	return simpleBind(p, "==", []string{"x$", "y$"}, m)
}

func deriveOrd(dt *syntax.DType) *syntax.Bind {
	p := dt.P
	m := &syntax.EMatch{P: p, Scrut: pairScrut(p)}
	for _, cd := range dt.Ctors {
		pa, va := ctorPat(p, cd, "a$")
		pb, vb := ctorPat(p, cd, "b$")
		l := &syntax.EList{P: p}
		for i := range va {
			l.Elems = append(l.Elems, app(pv("compare"), va[i], vb[i]))
		}
		m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: &syntax.PTuple{P: p, Elems: []syntax.Pattern{pa, pb}}, Body: &syntax.Rhs{Body: app(pv("lexicoOrder"), l)}})
	}
	if len(dt.Ctors) == 1 {
		return simpleBind(p, "compare", []string{"x$", "y$"}, m)
	}
	idx := &syntax.EMatch{P: p, Scrut: ev(p, "v$")}
	for i, cd := range dt.Ctors {
		pc := &syntax.PCon{P: p, Name: cd.Name}
		for range cd.Fields {
			pc.Args = append(pc.Args, &syntax.PWild{P: p})
		}
		idx.Arms = append(idx.Arms, &syntax.Arm{P: p, Pat: pc, Body: &syntax.Rhs{Body: &syntax.ELit{P: p, Kind: syntax.LitInt, Int: int64(i)}}})
	}
	m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: &syntax.PWild{P: p}, Body: &syntax.Rhs{Body: app(pv("compare"), app(ev(p, "idx$"), ev(p, "x$")), app(ev(p, "idx$"), ev(p, "y$")))}})
	body := &syntax.EBlock{P: p, Stmts: []syntax.Stmt{
		&syntax.SLet{Bind: simpleBind(p, "idx$", []string{"v$"}, idx)},
		&syntax.SExpr{E: m},
	}}
	return simpleBind(p, "compare", []string{"x$", "y$"}, body)
}

func deriveShow(dt *syntax.DType) *syntax.Bind {
	p := dt.P
	m := &syntax.EMatch{P: p, Scrut: ev(p, "x$")}
	for _, cd := range dt.Ctors {
		pa, va := ctorPat(p, cd, "a$")
		l := &syntax.EList{P: p}
		name := &syntax.ELit{P: p, Kind: syntax.LitString, Str: cd.Name}
		if cd.Named != nil {
			for i, v := range va {
				l.Elems = append(l.Elems, &syntax.ETuple{P: p, Elems: []syntax.Expr{&syntax.ELit{P: p, Kind: syntax.LitString, Str: cd.Named[i]}, app(pv("show"), v)}})
			}
			m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: pa, Body: &syntax.Rhs{Body: app(pv("showRecord"), name, l)}})
			continue
		}
		for _, v := range va {
			l.Elems = append(l.Elems, app(pv("showArg"), app(pv("show"), v)))
		}
		body := app(pv("showCon"), name, l)
		m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: pa, Body: &syntax.Rhs{Body: body}})
	}
	return simpleBind(p, "show", []string{"x$"}, m)
}

const jsonMod = "Std.Json"

func jv(p diag.Pos, name string) syntax.Expr {
	return &syntax.EVar{P: p, Qual: "#" + jsonMod, Name: name}
}
func jc(p diag.Pos, name string) syntax.Expr {
	return &syntax.ECon{P: p, Qual: "#" + jsonMod, Name: name}
}
func strLit(p diag.Pos, s string) syntax.Expr {
	return &syntax.ELit{P: p, Kind: syntax.LitString, Str: s}
}
func pair(p diag.Pos, a, b syntax.Expr) syntax.Expr {
	return &syntax.ETuple{P: p, Elems: []syntax.Expr{a, b}}
}

// deriveToJson writes the conversion described in Std.Json.
func deriveToJson(dt *syntax.DType) *syntax.Bind {
	p := dt.P
	multi := len(dt.Ctors) > 1
	m := &syntax.EMatch{P: p, Scrut: ev(p, "x$")}
	for _, cd := range dt.Ctors {
		pa, va := ctorPat(p, cd, "a$")
		var conv []syntax.Expr
		for _, v := range va {
			conv = append(conv, app(jv(p, "toJson"), v))
		}
		tag := pair(p, strLit(p, "tag"), app(jc(p, "JString"), strLit(p, cd.Name)))
		var body syntax.Expr
		switch {
		case cd.Named != nil:
			l := &syntax.EList{P: p}
			if multi {
				l.Elems = append(l.Elems, tag)
			}
			for i, x := range conv {
				l.Elems = append(l.Elems, pair(p, strLit(p, cd.Named[i]), x))
			}
			body = app(jc(p, "JObject"), l)
		case len(va) == 0:
			body = app(jc(p, "JString"), strLit(p, cd.Name))
		case !multi && len(va) == 1:
			body = conv[0]
		case !multi:
			body = app(jc(p, "JArray"), &syntax.EList{P: p, Elems: conv})
		default:
			values := pair(p, strLit(p, "values"), app(jc(p, "JArray"), &syntax.EList{P: p, Elems: conv}))
			body = app(jc(p, "JObject"), &syntax.EList{P: p, Elems: []syntax.Expr{tag, values}})
		}
		m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: pa, Body: &syntax.Rhs{Body: body}})
	}
	return simpleBind(p, "toJson", []string{"x$"}, m)
}

func okPat(p diag.Pos, x syntax.Pattern) syntax.Pattern {
	return &syntax.PCon{P: p, Qual: "#Prelude", Name: "Ok", Args: []syntax.Pattern{x}}
}

// resultChain matches each of the results in turn, passing errors on, and
// finally returns Ok (con b$0 .. b$n).
func resultChain(p diag.Pos, results []syntax.Expr, con string) syntax.Expr {
	var final syntax.Expr = &syntax.ECon{P: p, Name: con}
	if len(results) > 0 {
		var args []syntax.Expr
		for i := range results {
			args = append(args, ev(p, "b$"+itoa(i)))
		}
		final = app(final, args...)
	}
	var e syntax.Expr = app(&syntax.ECon{P: p, Qual: "#Prelude", Name: "Ok"}, final)
	for i := len(results) - 1; i >= 0; i-- {
		e = &syntax.EMatch{P: p, Scrut: results[i], Arms: []*syntax.Arm{
			{P: p, Pat: &syntax.PCon{P: p, Qual: "#Prelude", Name: "Err", Args: []syntax.Pattern{&syntax.PVar{P: p, Name: "e$"}}}, Body: &syntax.Rhs{Body: app(&syntax.ECon{P: p, Qual: "#Prelude", Name: "Err"}, ev(p, "e$"))}},
			{P: p, Pat: okPat(p, &syntax.PVar{P: p, Name: "b$" + itoa(i)}), Body: &syntax.Rhs{Body: e}},
		}}
	}
	return e
}

// tagMatch dispatches on the constructor tag of j$.
func tagMatch(p diag.Pos, typeName string, arms []*syntax.Arm) syntax.Expr {
	m := &syntax.EMatch{P: p, Scrut: app(jv(p, "tagOf"), ev(p, "j$"))}
	m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: &syntax.PCon{P: p, Qual: "#Prelude", Name: "Err", Args: []syntax.Pattern{&syntax.PVar{P: p, Name: "e$"}}}, Body: &syntax.Rhs{Body: app(&syntax.ECon{P: p, Qual: "#Prelude", Name: "Err"}, ev(p, "e$"))}})
	m.Arms = append(m.Arms, arms...)
	m.Arms = append(m.Arms, &syntax.Arm{P: p, Pat: okPat(p, &syntax.PVar{P: p, Name: "t$"}), Body: &syntax.Rhs{Body: app(jv(p, "unknownTag"), strLit(p, typeName), ev(p, "t$"))}})
	return m
}

func tagArm(p diag.Pos, name string, body syntax.Expr) *syntax.Arm {
	return &syntax.Arm{P: p, Pat: okPat(p, &syntax.PLit{P: p, Lit: &syntax.ELit{P: p, Kind: syntax.LitString, Str: name}}), Body: &syntax.Rhs{Body: body}}
}

// deriveFromJson writes the inverse of deriveToJson.
func deriveFromJson(dt *syntax.DType) *syntax.Bind {
	p := dt.P
	multi := len(dt.Ctors) > 1
	decode := func(cd *syntax.CtorDecl) syntax.Expr {
		var results []syntax.Expr
		switch {
		case cd.Named != nil:
			for _, f := range cd.Named {
				results = append(results, app(jv(p, "fieldOf"), strLit(p, f), ev(p, "j$")))
			}
		case len(cd.Fields) == 0:
		case !multi && len(cd.Fields) == 1:
			results = append(results, app(jv(p, "fromJson"), ev(p, "j$")))
		default:
			for i := range cd.Fields {
				results = append(results, app(jv(p, "elemOf"), &syntax.ELit{P: p, Kind: syntax.LitInt, Int: int64(i)}, ev(p, "vs$")))
			}
			return &syntax.EMatch{P: p, Scrut: app(jv(p, "valuesOf"), &syntax.ELit{P: p, Kind: syntax.LitInt, Int: int64(len(cd.Fields))}, ev(p, "j$")), Arms: []*syntax.Arm{
				{P: p, Pat: &syntax.PCon{P: p, Qual: "#Prelude", Name: "Err", Args: []syntax.Pattern{&syntax.PVar{P: p, Name: "e$"}}}, Body: &syntax.Rhs{Body: app(&syntax.ECon{P: p, Qual: "#Prelude", Name: "Err"}, ev(p, "e$"))}},
				{P: p, Pat: okPat(p, &syntax.PVar{P: p, Name: "vs$"}), Body: &syntax.Rhs{Body: resultChain(p, results, cd.Name)}},
			}}
		}
		return resultChain(p, results, cd.Name)
	}
	var body syntax.Expr
	if multi {
		var arms []*syntax.Arm
		for _, cd := range dt.Ctors {
			arms = append(arms, tagArm(p, cd.Name, decode(cd)))
		}
		body = tagMatch(p, dt.Name, arms)
	} else if cd := dt.Ctors[0]; cd.Named == nil && len(cd.Fields) == 0 {
		body = tagMatch(p, dt.Name, []*syntax.Arm{tagArm(p, cd.Name, decode(cd))})
	} else {
		body = decode(dt.Ctors[0])
	}
	return simpleBind(p, "fromJson", []string{"j$"}, body)
}

// ------------------------------------------------------------------ instances

func (c *Checker) processInstanceHeads() {
	for _, d := range c.mod.Decls {
		di, ok := d.(*syntax.DInstance)
		if !ok {
			continue
		}
		cl := c.lookupClass(di.P, di.Qual, di.Class)
		if cl == nil {
			continue
		}
		env := newTVEnv(true, arrowAmbient)
		head, k := c.convType(di.Head, env)
		if !unifyKind(k, cl.ParamKind) {
			c.errorf(di.Head.TPos(), "kind mismatch: class %s expects a type of kind %s, but this has kind %s", cl.Name, KindString(cl.ParamKind), KindString(k))
			continue
		}
		h, args := headOf(head)
		tc, ok := h.(*TCon)
		valid := ok
		seen := map[int]bool{}
		for _, a := range args {
			g, ok := a.(*TGen)
			if !ok || seen[g.Index] {
				valid = false
				break
			}
			seen[g.Index] = true
		}
		if !valid || len(args) != len(env.names) {
			c.errorf(di.Head.TPos(), "instance head must be a type constructor applied to distinct type variables")
			continue
		}
		env.fixed = true
		info := &InstInfo{Class: cl, TyCon: tc.Name, NVars: len(env.names), Head: head, Module: c.modName, Pos: di.P, Decl: di, Methods: map[string]string{}}
		for _, n := range env.names {
			info.Kinds = append(info.Kinds, DefaultKind(env.kinds[n]))
		}
		for _, pe := range di.Context {
			pc := c.lookupClass(pe.P, pe.Qual, pe.Class)
			if pc == nil {
				continue
			}
			v, ok := pe.Type.(*syntax.TVarE)
			if !ok {
				c.errorf(pe.P, "instance constraints must be on type variables")
				continue
			}
			t := c.tyVar(pe.P, env, v.Name, pc.ParamKind)
			info.Context = append(info.Context, Pred{pc, t})
		}
		info.Sym = c.sym("$inst$" + sanitize(shortName(cl.Name)) + "$" + sanitize(shortName(tc.Name)))
		byTy := c.g.Instances[cl.Name]
		if byTy == nil {
			byTy = map[string]*InstInfo{}
			c.g.Instances[cl.Name] = byTy
		}
		if old, ok := byTy[tc.Name]; ok {
			c.errorf(di.P, "duplicate instance %s %s (previous instance at %s)", shortName(cl.Name), shortName(tc.Name), old.Pos)
			continue
		}
		byTy[tc.Name] = info
		c.g.InstList = append(c.g.InstList, info)
		c.instInfo[di] = info
		c.insts = append(c.insts, di)
	}
}

// ------------------------------------------------------------------ foreign & values

func arity(t Type) int {
	n := 0
	for {
		f, ok := Prune(t).(*TFun)
		if !ok {
			return n
		}
		n++
		t = f.Res
	}
}

func (c *Checker) processForeigns() {
	for _, d := range c.mod.Decls {
		df, ok := d.(*syntax.DForeign)
		if !ok {
			continue
		}
		sc := c.sigScheme(df.Scheme)
		c.defineValue(df.Name, &ValueInfo{Name: df.Name, Sym: c.sym(df.Name), Scheme: sc, Foreign: df.Target, Arity: arity(sc.Type), Pos: df.P}, df.Private)
	}
}

func (c *Checker) collectValues() {
	for _, d := range c.mod.Decls {
		switch d := d.(type) {
		case *syntax.DSig:
			if _, dup := c.sigs[d.Name]; dup {
				c.errorf(d.P, "duplicate type signature for '%s'", d.Name)
			}
			c.sigs[d.Name] = d
		case *syntax.DTest:
			c.tests = append(c.tests, d)
		}
	}
	defined := map[string]bool{}
	for _, d := range c.mod.Decls {
		df, ok := d.(*syntax.DFun)
		if !ok {
			continue
		}
		b := df.Bind
		if defined[b.Name] {
			c.errorf(b.P, "'%s' is defined more than once (equations of a function must be adjacent)", b.Name)
			continue
		}
		defined[b.Name] = true
		n := len(b.Clauses[0].Params)
		for _, cl := range b.Clauses[1:] {
			if len(cl.Params) != n {
				c.errorf(cl.P, "equations for '%s' have different numbers of arguments", b.Name)
			}
		}
		if n == 0 && len(b.Clauses) > 1 {
			c.errorf(b.Clauses[1].P, "'%s' is defined more than once", b.Name)
		}
		vi := &ValueInfo{Name: b.Name, Sym: c.sym(b.Name), Arity: n, Pos: b.P, Bind: b}
		b.ID = vi.Sym
		b.Info = &syntax.BindInfo{}
		private := df.Private
		if s, ok := c.sigs[b.Name]; ok {
			vi.Scheme = c.sigScheme(s.Scheme)
			private = private || s.Private
		}
		c.funs = append(c.funs, df)
		c.defineValue(b.Name, vi, private)
	}
	for name, s := range c.sigs {
		if !defined[name] {
			c.errorf(s.P, "type signature for '%s' lacks a definition", name)
		}
	}
}
