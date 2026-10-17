package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/verniyyy/arkadia/internal/core"
	"github.com/verniyyy/arkadia/internal/types"
)

// val is a compiled Go expression. typ is "" for rt.Value, otherwise an
// unboxed Go type (bool, int64, float64, string, rune).
type val struct {
	code   string
	typ    string
	atomic bool
}

func boxed(code string) val { return val{code: code} }

type destKind int

const (
	dRet destKind = iota
	dAssign
	dDiscard
)

type dest struct {
	kind destKind
	v    string
}

type selfInfo struct {
	global string // global symbol of the current function
	local  string // or name of the local function
	arity  int
	used   bool
}

type ctx struct {
	g      *gen
	body   *strings.Builder
	indent int
	self   *selfInfo
	vars   map[string]string // local variable -> unboxed type ("" = Value)
	lfuns  map[string]int    // local functions with a direct Go func (name -> arity)
}

func (g *gen) newCtx() *ctx {
	return &ctx{g: g, body: &strings.Builder{}, indent: 1, vars: map[string]string{}, lfuns: map[string]int{}}
}

// sub creates a context for a nested Go function literal.
func (c *ctx) sub() *ctx {
	n := &ctx{g: c.g, body: &strings.Builder{}, indent: 1, vars: map[string]string{}, lfuns: map[string]int{}}
	for k, v := range c.vars {
		n.vars[k] = v
	}
	for k, v := range c.lfuns {
		n.lfuns[k] = v
	}
	return n
}

func (c *ctx) line(format string, args ...any) {
	c.body.WriteString(strings.Repeat("\t", c.indent))
	fmt.Fprintf(c.body, format, args...)
	c.body.WriteByte('\n')
}

func (c *ctx) open(format string, args ...any) {
	c.line(format, args...)
	c.indent++
}

func (c *ctx) close(s string) {
	c.indent--
	c.line("%s", s)
}

func (c *ctx) newTmp() string {
	c.g.tmp++
	return "t" + strconv.Itoa(c.g.tmp)
}

// as converts a compiled expression to the requested unboxed type.
func as(v val, typ string) string {
	if v.typ == typ {
		return v.code
	}
	if v.typ == "" {
		return v.code + ".(" + typ + ")"
	}
	if typ == "" {
		return v.code
	}
	panic(fmt.Sprintf("codegen: cannot convert %s to %s", v.typ, typ))
}

// atom ensures that v can be duplicated or reordered safely.
func (c *ctx) atom(v val) val {
	if v.atomic {
		return v
	}
	t := c.newTmp()
	if v.typ == "" {
		c.line("var %s rt.Value = %s", t, v.code)
	} else {
		c.line("%s := %s", t, v.code)
	}
	return val{code: t, typ: v.typ, atomic: true}
}

func (c *ctx) emit(d dest, v val) {
	switch d.kind {
	case dRet:
		c.line("return %s", v.code)
	case dAssign:
		c.line("%s = %s", d.v, v.code)
	case dDiscard:
		if !v.atomic {
			c.line("_ = %s", v.code)
		}
	}
}

// ---------------------------------------------------------------- statements

func (c *ctx) stmt(e core.Expr, d dest) {
	switch e := e.(type) {
	case *core.Let:
		c.let(e.Name, e.Val)
		c.stmt(e.Body, d)
	case *core.LetRec:
		c.letrec(e)
		c.stmt(e.Body, d)
	case *core.If:
		cond := c.expr(e.Cond)
		c.open("if %s {", as(cond, "bool"))
		c.stmt(e.Then, d)
		c.indent--
		c.open("} else {")
		c.stmt(e.Else, d)
		c.close("}")
	case *core.Match:
		c.match(e, d)
	case *core.App:
		if d.kind == dRet && c.tailCall(e) {
			return
		}
		c.emit(d, c.expr(e))
	default:
		c.emit(d, c.expr(e))
	}
}

func (c *ctx) let(name string, v core.Expr) {
	if name == "_" {
		c.stmt(v, dest{kind: dDiscard})
		return
	}
	if lam, ok := v.(*core.Lam); ok && len(lam.Params) <= 4 {
		c.localFunc(name, lam, false)
		return
	}
	x := c.expr(v)
	if x.typ == "" {
		c.line("var %s rt.Value = %s", local(name), x.code)
	} else {
		c.line("%s := %s", local(name), x.code)
	}
	c.line("_ = %s", local(name))
	c.vars[name] = x.typ
}

// localFunc declares a local function with a direct Go func value.
func (c *ctx) localFunc(name string, lam *core.Lam, rec bool) {
	n := len(lam.Params)
	fn := "lf_" + mangle(name)
	c.line("var %s func(%s) rt.Value", fn, params(n, "a"))
	c.line("var %s rt.Value", local(name))
	c.lfuns[name] = n
	c.vars[name] = ""
	body := c.lambdaBody(lam, name)
	c.line("%s = func(%s) rt.Value {", fn, params(n, "a"))
	c.body.WriteString(body)
	c.line("}")
	c.line("%s = %s", local(name), closureOf(fn, n))
	c.line("_ = %s", local(name))
}

func (c *ctx) letrec(e *core.LetRec) {
	// declare everything first so that bodies can refer to each other
	for i, name := range e.Names {
		n := len(e.Lams[i].Params)
		if n <= 4 {
			c.line("var %s func(%s) rt.Value", "lf_"+mangle(name), params(n, "a"))
			c.lfuns[name] = n
		}
		c.line("var %s rt.Value", local(name))
		c.vars[name] = ""
	}
	for i, name := range e.Names {
		lam := e.Lams[i]
		n := len(lam.Params)
		if n <= 4 {
			fn := "lf_" + mangle(name)
			body := c.lambdaBody(lam, name)
			c.line("%s = func(%s) rt.Value {", fn, params(n, "a"))
			c.body.WriteString(body)
			c.line("}")
			c.line("%s = %s", local(name), closureOf(fn, n))
		} else {
			v := c.lambda(lam)
			c.line("%s = %s", local(name), v.code)
		}
		c.line("_ = %s", local(name))
	}
}

// lambdaBody compiles the body of a function literal with parameters a0...
func (c *ctx) lambdaBody(lam *core.Lam, selfName string) string {
	s := c.sub()
	s.indent = c.indent + 1
	if selfName != "" {
		s.self = &selfInfo{local: selfName, arity: len(lam.Params)}
		s.indent++
	}
	for i, p := range lam.Params {
		s.line("%s := a%d", local(p), i)
		s.line("_ = %s", local(p))
		s.vars[p] = ""
	}
	s.stmt(lam.Body, dest{kind: dRet})
	body := s.body.String()
	if s.self != nil {
		ind := strings.Repeat("\t", c.indent+1)
		if s.self.used {
			return ind + "for {\n" + body + ind + "}\n"
		}
		return dedent(body)
	}
	return body
}

func (c *ctx) lambda(lam *core.Lam) val {
	n := len(lam.Params)
	if n <= 4 {
		body := c.lambdaBody(lam, "")
		return boxed(fmt.Sprintf("rt.Fn%d(func(%s) rt.Value {\n%s%s})", n, params(n, "a"), body, strings.Repeat("\t", c.indent)))
	}
	s := c.sub()
	s.indent = c.indent + 1
	for i, p := range lam.Params {
		s.line("%s := a[%d]", local(p), i)
		s.line("_ = %s", local(p))
		s.vars[p] = ""
	}
	s.stmt(lam.Body, dest{kind: dRet})
	return boxed(fmt.Sprintf("&rt.FnN{N: %d, F: func(a []rt.Value) rt.Value {\n%s%s}}", n, s.body.String(), strings.Repeat("\t", c.indent)))
}

// tailCall turns a saturated self call in tail position into a loop jump.
func (c *ctx) tailCall(e *core.App) bool {
	if c.self == nil || len(e.Args) != c.self.arity {
		return false
	}
	switch f := e.Fn.(type) {
	case *core.Global:
		if f.Sym != c.self.global || c.self.global == "" {
			return false
		}
	case *core.Var:
		if f.Name != c.self.local || c.self.local == "" {
			return false
		}
	default:
		return false
	}
	var args []val
	for _, a := range e.Args {
		args = append(args, c.atom(c.expr(a)))
	}
	for i, a := range args {
		c.line("a%d = %s", i, a.code)
	}
	c.line("continue")
	c.self.used = true
	return true
}

// ---------------------------------------------------------------- expressions

func (c *ctx) exprs(es []core.Expr) []val {
	out := make([]val, len(es))
	for i, e := range es {
		out[i] = c.atom(c.expr(e))
	}
	return out
}

func codes(vs []val) string {
	var parts []string
	for _, v := range vs {
		parts = append(parts, v.code)
	}
	return strings.Join(parts, ", ")
}

func (c *ctx) expr(e core.Expr) val {
	switch e := e.(type) {
	case *core.Var:
		return val{code: local(e.Name), typ: c.vars[e.Name], atomic: true}
	case *core.Lit:
		return literal(e.Val)
	case *core.Global:
		return c.global(e.Sym)
	case *core.App:
		return c.app(e)
	case *core.Lam:
		return c.lambda(e)
	case *core.Con:
		return c.con(e)
	case *core.Select:
		if gl, ok := e.Dict.(*core.Global); ok {
			if info := c.g.globals[gl.Sym]; info != nil && info.kind == gDict {
				return boxed(fmt.Sprintf("%s.S[%d]", info.name, e.Slot))
			}
		}
		d := c.atom(c.expr(e.Dict))
		return boxed(fmt.Sprintf("rt.Sel(%s, %d)", d.code, e.Slot))
	case *core.Concat:
		parts := c.exprs(e.Parts)
		var ss []string
		for _, p := range parts {
			ss = append(ss, as(p, "string"))
		}
		return val{code: "(" + strings.Join(ss, " + ") + ")", typ: "string"}
	case *core.Let:
		c.let(e.Name, e.Val)
		return c.expr(e.Body)
	case *core.LetRec:
		c.letrec(e)
		return c.expr(e.Body)
	case *core.Handle:
		return c.handle(e)
	case *core.Mask:
		var ids []string
		for _, eff := range e.Effs {
			ids = append(ids, strconv.Itoa(c.g.effIDs[eff]))
		}
		s := c.sub()
		s.indent = c.indent + 1
		s.stmt(e.Body, dest{kind: dRet})
		return boxed(fmt.Sprintf("rt.Mask([]int{%s}, func() rt.Value {\n%s%s})", strings.Join(ids, ", "), s.body.String(), strings.Repeat("\t", c.indent)))
	case *core.If, *core.Match:
		t := c.newTmp()
		c.line("var %s rt.Value", t)
		c.stmt(e, dest{kind: dAssign, v: t})
		c.line("_ = %s", t) // the value may be discarded
		return val{code: t, atomic: true}
	}
	panic(fmt.Sprintf("codegen expr: %T", e))
}

func literal(v any) val {
	switch v := v.(type) {
	case int64:
		return val{code: fmt.Sprintf("int64(%d)", v), typ: "int64", atomic: true}
	case float64:
		s := strconv.FormatFloat(v, 'g', -1, 64)
		return val{code: fmt.Sprintf("float64(%s)", s), typ: "float64", atomic: true}
	case string:
		return val{code: strconv.Quote(v), typ: "string", atomic: true}
	case rune:
		return val{code: fmt.Sprintf("rune(%d)", v), typ: "rune", atomic: true}
	case bool:
		return val{code: strconv.FormatBool(v), typ: "bool", atomic: true}
	case core.Unit:
		return val{code: "rt.Unit", atomic: true}
	}
	panic(fmt.Sprintf("literal %T", v))
}

func (c *ctx) global(sym string) val {
	info := c.g.globals[sym]
	if info == nil {
		panic("codegen: unknown global " + sym)
	}
	switch info.kind {
	case gValue:
		return val{code: info.name, atomic: true}
	case gDict:
		return val{code: info.name, atomic: true}
	case gForeign:
		if info.arity == 0 {
			return boxed(info.target + "()")
		}
	}
	return val{code: closureName(sym), atomic: true}
}

func (c *ctx) app(e *core.App) val {
	if h, ok := e.Fn.(*core.Handle); ok && h.SRet != nil {
		init := c.atom(c.expr(e.Args[0]))
		res := c.handleState(h, init)
		if len(e.Args) == 1 {
			return res
		}
		return c.applyValue(c.atom(res), c.exprs(e.Args[1:]))
	}
	// known global function: call directly
	if gl, ok := e.Fn.(*core.Global); ok {
		info := c.g.globals[gl.Sym]
		if info != nil && info.kind != gValue && info.kind != gDict && info.arity > 0 {
			args := c.exprs(e.Args)
			return c.callKnown(info, args)
		}
	}
	if v, ok := e.Fn.(*core.Var); ok {
		if n, ok := c.lfuns[v.Name]; ok {
			args := c.exprs(e.Args)
			fn := "lf_" + mangle(v.Name)
			if len(args) == n {
				return boxed(fmt.Sprintf("%s(%s)", fn, codes(args)))
			}
			if len(args) > n {
				head := boxed(fmt.Sprintf("%s(%s)", fn, codes(args[:n])))
				return c.applyValue(c.atom(head), args[n:])
			}
			// partial application: apply the closure (the arguments are
			// already evaluated)
			return c.applyValue(c.atom(c.expr(e.Fn)), args)
		}
	}
	f := c.atom(c.expr(e.Fn))
	args := c.exprs(e.Args)
	return c.applyValue(f, args)
}

func (c *ctx) applyValue(f val, args []val) val {
	if len(args) <= 4 {
		return boxed(fmt.Sprintf("rt.Apply%d(%s, %s)", len(args), f.code, codes(args)))
	}
	return boxed(fmt.Sprintf("rt.ApplyN(%s, %s)", f.code, codes(args)))
}

func (c *ctx) callKnown(info *globalInfo, args []val) val {
	n := info.arity
	if len(args) < n {
		// partial application: capture the given arguments
		rest := n - len(args)
		var all []string
		for _, a := range args {
			all = append(all, a.code)
		}
		for i := 0; i < rest; i++ {
			all = append(all, fmt.Sprintf("b%d", i))
		}
		call := c.directCall(info, all, args)
		if rest <= 4 {
			return boxed(fmt.Sprintf("rt.Fn%d(func(%s) rt.Value { return %s })", rest, params(rest, "b"), call.code))
		}
		var idx []string
		for i := 0; i < rest; i++ {
			idx = append(idx, fmt.Sprintf("b%d := a[%d]", i, i))
		}
		return boxed(fmt.Sprintf("&rt.FnN{N: %d, F: func(a []rt.Value) rt.Value { %s; return %s }}", rest, strings.Join(idx, "; "), call.code))
	}
	var codesN []string
	for _, a := range args[:n] {
		codesN = append(codesN, a.code)
	}
	res := c.directCall(info, codesN, args[:n])
	if len(args) == n {
		return res
	}
	return c.applyValue(c.atom(val{code: res.code, typ: res.typ}), args[n:])
}

// directCall produces a saturated call; foreign primitives may be inlined.
func (c *ctx) directCall(info *globalInfo, argCodes []string, args []val) val {
	if info.kind == gForeign && len(args) == len(argCodes) && !allConstant(args) {
		if p, ok := prims[info.target]; ok {
			conv := make([]any, len(args))
			for i, a := range args {
				conv[i] = as(a, p.in)
			}
			return val{code: fmt.Sprintf(p.tmpl, conv...), typ: p.out}
		}
	}
	return boxed(fmt.Sprintf("%s(%s)", info.name, strings.Join(argCodes, ", ")))
}

// allConstant reports whether all arguments are literals; inlining them
// would make Go fold (and possibly reject, on overflow) the expression.
func allConstant(args []val) bool {
	for _, a := range args {
		if !strings.HasPrefix(a.code, "int64(") && !strings.HasPrefix(a.code, "float64(") && !strings.HasPrefix(a.code, "rune(") && !strings.HasPrefix(a.code, "\"") {
			return false
		}
	}
	return true
}

type prim struct {
	in, out string
	tmpl    string
}

// prims are foreign functions compiled inline with unboxed arithmetic.
var prims = map[string]prim{
	"rt.IntAdd":       {"int64", "int64", "(%s + %s)"},
	"rt.IntSub":       {"int64", "int64", "(%s - %s)"},
	"rt.IntMul":       {"int64", "int64", "(%s * %s)"},
	"rt.IntNeg":       {"int64", "int64", "(-%s)"},
	"rt.IntEq":        {"int64", "bool", "(%s == %s)"},
	"rt.IntLt":        {"int64", "bool", "(%s < %s)"},
	"rt.IntLe":        {"int64", "bool", "(%s <= %s)"},
	"rt.IntGt":        {"int64", "bool", "(%s > %s)"},
	"rt.IntGe":        {"int64", "bool", "(%s >= %s)"},
	"rt.IntToFloat":   {"int64", "float64", "float64(%s)"},
	"rt.FloatAdd":     {"float64", "float64", "(%s + %s)"},
	"rt.FloatSub":     {"float64", "float64", "(%s - %s)"},
	"rt.FloatMul":     {"float64", "float64", "(%s * %s)"},
	"rt.FloatDiv":     {"float64", "float64", "(%s / %s)"},
	"rt.FloatNeg":     {"float64", "float64", "(-%s)"},
	"rt.FloatEq":      {"float64", "bool", "(%s == %s)"},
	"rt.FloatLt":      {"float64", "bool", "(%s < %s)"},
	"rt.FloatLe":      {"float64", "bool", "(%s <= %s)"},
	"rt.FloatGt":      {"float64", "bool", "(%s > %s)"},
	"rt.FloatGe":      {"float64", "bool", "(%s >= %s)"},
	"rt.StringEq":     {"string", "bool", "(%s == %s)"},
	"rt.StringConcat": {"string", "string", "(%s + %s)"},
	"rt.CharEq":       {"rune", "bool", "(%s == %s)"},
}

func (c *ctx) con(e *core.Con) val {
	ci := e.Ctor
	switch ci.Type.Name {
	case types.TBool:
		return val{code: strconv.FormatBool(ci.Tag == 1), typ: "bool", atomic: true}
	case types.TUnit:
		return val{code: "rt.Unit", atomic: true}
	}
	if ci.Arity == 0 {
		if ci.Type.Name == types.TList {
			return val{code: "rt.Nil", atomic: true}
		}
		return val{code: fmt.Sprintf("rt.Con0(%d)", ci.Tag), atomic: false}
	}
	args := c.exprs(e.Args)
	switch ci.Arity {
	case 1:
		return boxed(fmt.Sprintf("&rt.D1{T: %d, A: %s}", ci.Tag, args[0].code))
	case 2:
		return boxed(fmt.Sprintf("&rt.D2{T: %d, A: %s, B: %s}", ci.Tag, args[0].code, args[1].code))
	case 3:
		return boxed(fmt.Sprintf("&rt.D3{T: %d, A: %s, B: %s, C: %s}", ci.Tag, args[0].code, args[1].code, args[2].code))
	}
	return boxed(fmt.Sprintf("&rt.DN{T: %d, F: []rt.Value{%s}}", ci.Tag, codes(args)))
}

// ---------------------------------------------------------------- handlers

// handleState compiles a state-passing handler applied to its initial state.
func (c *ctx) handleState(h *core.Handle, init val) val {
	var sb strings.Builder
	ind := strings.Repeat("\t", c.indent)
	fmt.Fprintf(&sb, "rt.HandleState(&rt.Handler{Eff: %d, State: true, RetS: func(a0, a1 rt.Value) rt.Value {\n", c.g.effIDs[h.Eff])
	sb.WriteString(c.fnBody(h.SRet, "a"))
	fmt.Fprintf(&sb, "%s}, OpsS: []func([]rt.Value, rt.Value) rt.Value{\n", ind)
	for _, op := range h.SOps {
		s := c.sub()
		s.indent = c.indent + 2
		n := len(op.Params)
		for i, p := range op.Params[:n-1] {
			s.line("%s := args[%d]", local(p), i)
			s.line("_ = %s", local(p))
			s.vars[p] = ""
		}
		st := op.Params[n-1]
		s.line("%s := st", local(st))
		s.line("_ = %s", local(st))
		s.vars[st] = ""
		s.stmt(op.Body, dest{kind: dRet})
		fmt.Fprintf(&sb, "%s\tfunc(args []rt.Value, st rt.Value) rt.Value {\n%s%s\t},\n", ind, s.body.String(), ind)
	}
	fmt.Fprintf(&sb, "%s}}, %s, func() rt.Value {\n", ind, init.code)
	s := c.sub()
	s.indent = c.indent + 1
	s.stmt(h.Body, dest{kind: dRet})
	fmt.Fprintf(&sb, "%s%s})", s.body.String(), ind)
	return boxed(sb.String())
}

// fnBody compiles the body of a lambda whose parameters are named
// prefix0, prefix1, ...
func (c *ctx) fnBody(lam *core.Lam, prefix string) string {
	s := c.sub()
	s.indent = c.indent + 1
	for i, p := range lam.Params {
		s.line("%s := %s%d", local(p), prefix, i)
		s.line("_ = %s", local(p))
		s.vars[p] = ""
	}
	s.stmt(lam.Body, dest{kind: dRet})
	return s.body.String()
}

func (c *ctx) handle(h *core.Handle) val {
	var sb strings.Builder
	ind := strings.Repeat("\t", c.indent)
	fmt.Fprintf(&sb, "rt.Handle(&rt.Handler{Eff: %d, ", c.g.effIDs[h.Eff])
	if h.Ret != nil {
		fmt.Fprintf(&sb, "Ret: rt.Fn1(func(a0 rt.Value) rt.Value {\n%s%s}), ", c.lambdaBody(h.Ret, ""), ind)
	}
	sb.WriteString("Ops: []func([]rt.Value, rt.Value) rt.Value{\n")
	for _, op := range h.Ops {
		s := c.sub()
		s.indent = c.indent + 2
		n := len(op.Params)
		for i, p := range op.Params[:n-1] {
			s.line("%s := args[%d]", local(p), i)
			s.line("_ = %s", local(p))
			s.vars[p] = ""
		}
		k := op.Params[n-1]
		s.line("%s := k", local(k))
		s.line("_ = %s", local(k))
		s.vars[k] = ""
		s.stmt(op.Body, dest{kind: dRet})
		fmt.Fprintf(&sb, "%s\tfunc(args []rt.Value, k rt.Value) rt.Value {\n%s%s\t},\n", ind, s.body.String(), ind)
	}
	fmt.Fprintf(&sb, "%s}, UsesK: []bool{", ind)
	for _, u := range h.UsesK {
		fmt.Fprintf(&sb, "%v, ", u)
	}
	fmt.Fprintf(&sb, "}, Tail: %v, Direct: %v}, func() rt.Value {\n", h.Tail, h.Direct)
	s := c.sub()
	s.indent = c.indent + 1
	s.stmt(h.Body, dest{kind: dRet})
	fmt.Fprintf(&sb, "%s%s})", s.body.String(), ind)
	return boxed(sb.String())
}
