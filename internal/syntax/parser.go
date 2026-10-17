package syntax

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/verniyyy/arkadia/internal/diag"
)

type parser struct {
	toks []Token
	i    int
	// pending layout tokens absorbed while parsing a type
	typeIndent int
	// set when the last parsed operand ended with an unparenthesized
	// lambda/match/handle, which extends as far as possible
	trailingBlock bool
	// whereOK is set while parsing the body of an equation: a block that
	// starts then may end with the equation's `where` clause, which is
	// left in pendingWhere
	whereOK      bool
	pendingWhere []*Bind
}

type bailout struct{ err *diag.Error }

// ParseModule parses a whole source file.
func ParseModule(file, src string) (mod *Module, err error) {
	toks, err := Lex(file, src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	defer func() {
		if r := recover(); r != nil {
			if b, ok := r.(bailout); ok {
				mod, err = nil, diag.List{b.err}
				return
			}
			panic(r)
		}
	}()
	return p.module(file), nil
}

// ParseExprString parses a standalone expression (used by tooling/tests).
func ParseExprString(file, src string) (e Expr, err error) {
	toks, err := Lex(file, src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	defer func() {
		if r := recover(); r != nil {
			if b, ok := r.(bailout); ok {
				e, err = nil, diag.List{b.err}
				return
			}
			panic(r)
		}
	}()
	e = p.expr()
	p.skipNewlines()
	p.expect(EOF)
	return e, nil
}

// ------------------------------------------------------------ token helpers

func (p *parser) peek() Token { return p.toks[p.i] }
func (p *parser) peekN(n int) Token {
	if p.i+n < len(p.toks) {
		return p.toks[p.i+n]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) at(k Kind) bool { return p.toks[p.i].Kind == k }
func (p *parser) atOp(op string) bool {
	t := p.toks[p.i]
	return t.Kind == OP && t.Text == op && t.Qual == ""
}
func (p *parser) next() Token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

// atWord reports whether the next token is the contextual keyword w.
func (p *parser) atWord(w string) bool {
	t := p.toks[p.i]
	return t.Kind == LIDENT && t.Qual == "" && t.Text == w
}

func (p *parser) accept(k Kind) bool {
	if p.at(k) {
		p.next()
		return true
	}
	return false
}

func (p *parser) fail(pos Pos, format string, args ...any) {
	panic(bailout{diag.Errorf(pos, format, args...)})
}

func (p *parser) unexpected(what string) {
	t := p.peek()
	switch t.Kind {
	case INDENT:
		p.fail(t.Pos, "unexpected indentation; expected %s", what)
	case DEDENT, NEWLINE:
		p.fail(t.Pos, "unexpected end of line; expected %s", what)
	}
	if t.first {
		p.fail(t.Pos, "unexpected %s at the start of a line; expected %s (check the indentation)", t, what)
	}
	p.fail(t.Pos, "unexpected %s; expected %s", t, what)
}

func (p *parser) expect(k Kind) Token {
	if !p.at(k) {
		p.unexpected(k.String())
	}
	return p.next()
}

func (p *parser) skipNewlines() {
	for p.at(NEWLINE) {
		p.next()
	}
}

// ------------------------------------------------------------ module

func (p *parser) module(file string) *Module {
	m := &Module{File: file}
	p.skipNewlines()
	if p.at(KwModule) {
		p.next()
		m.Name = p.qualifiedUpper()
		p.endDecl()
	}
	p.skipNewlines()
	for p.at(KwImport) {
		m.Imports = append(m.Imports, p.importDecl())
		p.endDecl()
		p.skipNewlines()
	}
	for !p.at(EOF) {
		d := p.decl()
		if d != nil {
			m.Decls = append(m.Decls, d)
		}
		p.endDecl()
		p.skipNewlines()
	}
	m.Decls = mergeClauses(m.Decls)
	return m
}

func (p *parser) endDecl() {
	if p.at(EOF) {
		return
	}
	if !p.at(NEWLINE) {
		p.unexpected("end of declaration")
	}
	p.next()
}

func (p *parser) qualifiedUpper() string {
	t := p.expect(UIDENT)
	if t.Qual != "" {
		return t.Qual + "." + t.Text
	}
	return t.Text
}

func (p *parser) importDecl() *Import {
	pos := p.expect(KwImport).Pos
	imp := &Import{P: pos, Path: p.qualifiedUpper()}
	if p.atWord("as") {
		p.next()
		imp.Alias = p.expect(UIDENT).Text
	}
	if p.at(LPAREN) {
		p.next()
		if p.at(OP) && p.peek().Text == ".." {
			p.next()
			imp.All = true
		} else {
			for !p.at(RPAREN) {
				t := p.peek()
				switch t.Kind {
				case LIDENT:
					p.next()
					imp.Names = append(imp.Names, ImportName{P: t.Pos, Name: t.Text})
				case UIDENT:
					p.next()
					in := ImportName{P: t.Pos, Name: t.Text, Kind: 1}
					if p.at(LPAREN) && p.peekN(1).Kind == OP && p.peekN(1).Text == ".." {
						p.next()
						p.next()
						p.expect(RPAREN)
						in.All = true
					}
					imp.Names = append(imp.Names, in)
				case LPAREN:
					p.next()
					op := p.expect(OP)
					p.expect(RPAREN)
					imp.Names = append(imp.Names, ImportName{P: t.Pos, Name: op.Text})
				default:
					p.unexpected("import name")
				}
				if !p.accept(COMMA) {
					break
				}
			}
		}
		p.expect(RPAREN)
	}
	return imp
}

// mergeClauses groups consecutive equations of the same function.
func mergeClauses(ds []Decl) []Decl {
	var out []Decl
	for _, d := range ds {
		if f, ok := d.(*DFun); ok && f.Bind.Name != "" && len(out) > 0 {
			if prev, ok := out[len(out)-1].(*DFun); ok && prev.Bind.Name == f.Bind.Name && prev.Bind.Pat == nil {
				prev.Bind.Clauses = append(prev.Bind.Clauses, f.Bind.Clauses...)
				continue
			}
		}
		out = append(out, d)
	}
	return out
}

func mergeBinds(bs []*Bind) []*Bind {
	var out []*Bind
	for _, b := range bs {
		if b.Name != "" && len(out) > 0 {
			prev := out[len(out)-1]
			if prev.Name == b.Name && prev.Pat == nil {
				prev.Clauses = append(prev.Clauses, b.Clauses...)
				continue
			}
		}
		out = append(out, b)
	}
	return out
}

// ------------------------------------------------------------ declarations

func (p *parser) decl() Decl {
	t := p.peek()
	switch t.Kind {
	case KwPrivate:
		p.next()
		d := p.decl()
		switch d := d.(type) {
		case *DFun:
			d.Private = true
		case *DSig:
			d.Private = true
		case *DType:
			d.Private = true
		default:
			p.fail(t.Pos, "'private' is only allowed on functions, signatures and types")
		}
		return d
	case KwType:
		return p.typeDecl()
	case KwClass:
		return p.classDecl()
	case KwInstance:
		return p.instanceDecl()
	case KwEffect:
		return p.effectDecl()
	case KwForeign:
		return p.foreignDecl()
	case KwInfixl, KwInfixr, KwInfix:
		return p.fixityDecl()
	}
	if p.atWord("test") && p.peekN(1).Kind == STRING {
		p.next()
		name := p.expect(STRING)
		p.expect(EQUALS)
		return &DTest{P: t.Pos, Name: name.Str, Body: p.body()}
	}
	if p.atWord("alias") && p.peekN(1).Kind == UIDENT {
		return p.aliasDecl()
	}
	if s := p.trySig(); s != nil {
		return s
	}
	b := p.funBind()
	return &DFun{P: b.P, Bind: b}
}

// trySig parses `name : scheme` if present.
func (p *parser) trySig() *DSig {
	t := p.peek()
	if t.Kind == LIDENT && t.Qual == "" && p.peekN(1).Kind == COLON {
		p.next()
		p.next()
		return &DSig{P: t.Pos, Name: t.Text, Scheme: p.schemeTop()}
	}
	if t.Kind == LPAREN && p.peekN(1).Kind == OP && p.peekN(2).Kind == RPAREN && p.peekN(3).Kind == COLON {
		p.next()
		op := p.next()
		p.next()
		p.next()
		return &DSig{P: t.Pos, Name: op.Text, Scheme: p.schemeTop()}
	}
	return nil
}

// funBind parses one equation: `f p1 .. pn = e`, `(op) a b = e` or `a op b = e`.
func (p *parser) funBind() *Bind {
	t := p.peek()
	var name string
	var params []Pattern
	switch {
	case t.Kind == LPAREN && p.peekN(1).Kind == OP && p.peekN(2).Kind == RPAREN:
		p.next()
		name = p.next().Text
		p.next()
		params = p.paramPatterns()
	case p.isInfixDef():
		left := p.patApp()
		if p.at(BACKTICK) {
			p.next()
			name = p.expect(LIDENT).Text
			p.expect(BACKTICK)
		} else {
			name = p.next().Text
		}
		right := p.patApp()
		params = []Pattern{left, right}
	case t.Kind == LIDENT && t.Qual == "":
		p.next()
		name = t.Text
		params = p.paramPatterns()
	default:
		p.unexpected("declaration")
	}
	saved := p.pendingWhere
	p.pendingWhere = nil
	rhs := p.equationRhs()
	if p.at(KwWhere) {
		if p.pendingWhere != nil {
			p.fail(p.peek().Pos, "an equation can have only one `where` clause")
		}
		rhs.Where = p.whereBinds()
	} else {
		rhs.Where = p.pendingWhere
	}
	p.pendingWhere = saved
	return &Bind{P: t.Pos, Name: name, Clauses: []*Clause{{P: t.Pos, Params: params, Rhs: rhs}}}
}

// equationRhs parses the right-hand side of an equation; its body blocks
// may end with the equation's where clause.
func (p *parser) equationRhs() *Rhs {
	if p.at(BAR) {
		var gs []*Guard
		for p.at(BAR) && p.pendingWhere == nil {
			pos := p.next().Pos
			cond := p.expr()
			p.expect(EQUALS)
			gs = append(gs, &Guard{P: pos, Cond: cond, Body: p.equationBody()})
		}
		return &Rhs{Guards: gs}
	}
	p.expect(EQUALS)
	return &Rhs{Body: p.equationBody()}
}

// equationBody parses a body of an equation; when it is an indented block,
// the block may end with the equation's where clause.
func (p *parser) equationBody() Expr {
	if p.at(INDENT) {
		p.whereOK = true
		return p.block()
	}
	return p.body()
}

// whereBinds parses `where` followed by local bindings, either on the same
// line (separated by ';') or as an indented block. A signature may be
// written on its own line before the binding.
func (p *parser) whereBinds() []*Bind {
	pos := p.expect(KwWhere).Pos
	var binds []*Bind
	sigs := map[string]*SchemeE{}
	var sigOrder []Token
	item := func() {
		t := p.peek()
		if t.Kind == LIDENT && t.Qual == "" && p.peekN(1).Kind == COLON {
			p.next()
			p.next()
			sc := p.schemeTop()
			if p.at(EQUALS) || p.at(BAR) {
				binds = append(binds, &Bind{P: t.Pos, Name: t.Text, Sig: sc, Clauses: []*Clause{{P: t.Pos, Rhs: p.rhs(EQUALS)}}})
				return
			}
			if _, dup := sigs[t.Text]; dup {
				p.fail(t.Pos, "duplicate signature for '%s'", t.Text)
			}
			sigs[t.Text] = sc
			sigOrder = append(sigOrder, t)
			return
		}
		b := p.letBind(t.Pos)
		if sc, ok := sigs[b.Name]; ok && b.Name != "" {
			b.Sig = sc
			delete(sigs, b.Name)
		}
		binds = append(binds, b)
	}
	if p.accept(INDENT) {
		for {
			item()
			if p.accept(NEWLINE) || p.accept(SEMI) {
				if p.at(DEDENT) {
					p.next()
					break
				}
				continue
			}
			p.expect(DEDENT)
			break
		}
	} else {
		item()
		for p.accept(SEMI) {
			item()
		}
	}
	for _, t := range sigOrder {
		if _, ok := sigs[t.Text]; ok {
			p.fail(t.Pos, "the signature for '%s' lacks an accompanying binding", t.Text)
		}
	}
	if len(binds) == 0 {
		p.fail(pos, "empty where clause")
	}
	// merge consecutive clauses of local functions
	var merged []*Bind
	for _, b := range binds {
		if n := len(merged); n > 0 && b.Name != "" && b.Sig == nil && len(b.Clauses[0].Params) > 0 {
			if pb := merged[n-1]; pb.Name == b.Name && pb.Pat == nil && len(pb.Clauses[0].Params) > 0 {
				pb.Clauses = append(pb.Clauses, b.Clauses...)
				continue
			}
		}
		merged = append(merged, b)
	}
	return merged
}

// isInfixDef speculatively parses a pattern and reports whether it is
// followed by an operator, i.e. the equation defines an infix operator.
func (p *parser) isInfixDef() (ok bool) {
	if !p.startsPatAtom() {
		return false
	}
	save := p.i
	defer func() {
		if r := recover(); r != nil {
			if _, isB := r.(bailout); !isB {
				panic(r)
			}
			ok = false
		}
		p.i = save
	}()
	p.patApp()
	t := p.peek()
	return t.Kind == OP || t.Kind == BACKTICK
}

func (p *parser) paramPatterns() []Pattern {
	var ps []Pattern
	for p.startsPatAtom() {
		ps = append(ps, p.patAtom())
	}
	return ps
}

// rhs parses `= body` or a sequence of guards `| cond = body`.
// sep is EQUALS for definitions and ARROW for match arms.
func (p *parser) rhs(sep Kind) *Rhs {
	if p.at(BAR) {
		var gs []*Guard
		for p.at(BAR) {
			pos := p.next().Pos
			cond := p.expr()
			p.expect(sep)
			gs = append(gs, &Guard{P: pos, Cond: cond, Body: p.body()})
			// guards may continue after a block body on a new line
		}
		return &Rhs{Guards: gs}
	}
	p.expect(sep)
	return &Rhs{Body: p.body()}
}

func (p *parser) typeDecl() Decl {
	pos := p.expect(KwType).Pos
	name := p.expect(UIDENT).Text
	d := &DType{P: pos, Name: name}
	for p.at(LIDENT) && !p.atWord("deriving") {
		d.Params = append(d.Params, p.next().Text)
	}
	if p.accept(EQUALS) {
		if p.at(INDENT) {
			p.next()
			for {
				if p.atWord("deriving") {
					d.Deriving = p.derivingClause()
				} else {
					p.accept(BAR)
					d.Ctors = append(d.Ctors, p.ctorDecl())
					for p.accept(BAR) {
						d.Ctors = append(d.Ctors, p.ctorDecl())
					}
				}
				if p.accept(NEWLINE) {
					continue
				}
				p.expect(DEDENT)
				break
			}
		} else {
			p.accept(BAR)
			d.Ctors = append(d.Ctors, p.ctorDecl())
			for p.accept(BAR) {
				d.Ctors = append(d.Ctors, p.ctorDecl())
			}
		}
	}
	if p.atWord("deriving") {
		d.Deriving = p.derivingClause()
	}
	return d
}

func (p *parser) derivingClause() []string {
	p.next() // deriving
	var names []string
	if p.accept(LPAREN) {
		for !p.at(RPAREN) {
			names = append(names, p.expect(UIDENT).Text)
			if !p.accept(COMMA) {
				break
			}
		}
		p.expect(RPAREN)
	} else {
		names = append(names, p.expect(UIDENT).Text)
	}
	return names
}

func (p *parser) ctorDecl() *CtorDecl {
	t := p.expect(UIDENT)
	c := &CtorDecl{P: t.Pos, Name: t.Text}
	if p.at(LBRACE) {
		// record constructor: C { f : T, g, h : U }
		p.next()
		c.Named = []string{}
		for !p.at(RBRACE) {
			names := []string{p.expect(LIDENT).Text}
			for p.accept(COMMA) {
				names = append(names, p.expect(LIDENT).Text)
			}
			p.expect(COLON)
			ty := p.typeTop()
			for _, n := range names {
				c.Named = append(c.Named, n)
				c.Fields = append(c.Fields, ty)
			}
			if !p.accept(COMMA) {
				break
			}
		}
		p.expect(RBRACE)
		return c
	}
	for p.startsTypeAtom() && !p.atWord("deriving") {
		c.Fields = append(c.Fields, p.typeAtom())
	}
	return c
}

func (p *parser) aliasDecl() Decl {
	pos := p.next().Pos // alias
	d := &DAlias{P: pos, Name: p.expect(UIDENT).Text}
	for p.at(LIDENT) {
		d.Params = append(d.Params, p.next().Text)
	}
	p.expect(EQUALS)
	d.Type = p.typeTop()
	return d
}

// predsFromType converts a parsed type (C a or (C a, D b)) into constraints.
func (p *parser) predsFromType(t TypeExpr) []*PredE {
	if tt, ok := t.(*TTupleE); ok {
		var ps []*PredE
		for _, e := range tt.Elems {
			ps = append(ps, p.predsFromType(e)...)
		}
		return ps
	}
	if _, ok := t.(*TUnitE); ok {
		return nil
	}
	app, ok := t.(*TAppE)
	if !ok || len(app.Args) != 1 {
		p.fail(t.TPos(), "invalid constraint")
	}
	con, ok := app.Fun.(*TConE)
	if !ok {
		p.fail(t.TPos(), "invalid constraint")
	}
	return []*PredE{{P: t.TPos(), Qual: con.Qual, Class: con.Name, Type: app.Args[0]}}
}

func (p *parser) classDecl() Decl {
	pos := p.expect(KwClass).Pos
	d := &DClass{P: pos}
	t := p.btype()
	if p.accept(DARROW) {
		d.Supers = p.predsFromType(t)
		t = p.btype()
	}
	ps := p.predsFromType(t)
	if len(ps) != 1 || ps[0].Qual != "" {
		p.fail(t.TPos(), "expected class head `Name a`")
	}
	v, ok := ps[0].Type.(*TVarE)
	if !ok {
		p.fail(t.TPos(), "class parameter must be a type variable")
	}
	d.Name, d.Param = ps[0].Class, v.Name
	if p.accept(KwWhere) {
		p.expect(INDENT)
		for {
			if s := p.trySig(); s != nil {
				d.Sigs = append(d.Sigs, s)
			} else {
				d.Defaults = append(d.Defaults, p.funBind())
			}
			if p.accept(NEWLINE) {
				if p.at(DEDENT) {
					p.next()
					break
				}
				continue
			}
			p.expect(DEDENT)
			break
		}
	}
	d.Defaults = mergeBinds(d.Defaults)
	return d
}

func (p *parser) instanceDecl() Decl {
	pos := p.expect(KwInstance).Pos
	d := &DInstance{P: pos}
	t := p.btype()
	if p.accept(DARROW) {
		d.Context = p.predsFromType(t)
		t = p.btype()
	}
	app, ok := t.(*TAppE)
	if !ok || len(app.Args) != 1 {
		p.fail(t.TPos(), "expected instance head `Class Type`")
	}
	con, ok := app.Fun.(*TConE)
	if !ok {
		p.fail(t.TPos(), "expected instance head `Class Type`")
	}
	d.Qual, d.Class, d.Head = con.Qual, con.Name, app.Args[0]
	if p.accept(KwWhere) {
		p.expect(INDENT)
		for {
			d.Binds = append(d.Binds, p.funBind())
			if p.accept(NEWLINE) {
				if p.at(DEDENT) {
					p.next()
					break
				}
				continue
			}
			p.expect(DEDENT)
			break
		}
	}
	d.Binds = mergeBinds(d.Binds)
	return d
}

func (p *parser) effectDecl() Decl {
	pos := p.expect(KwEffect).Pos
	d := &DEffect{P: pos, Name: p.expect(UIDENT).Text}
	for p.at(LIDENT) {
		d.Params = append(d.Params, p.next().Text)
	}
	if p.accept(KwWhere) {
		p.expect(INDENT)
		for {
			s := p.trySig()
			if s == nil {
				p.unexpected("operation signature")
			}
			d.Ops = append(d.Ops, s)
			if p.accept(NEWLINE) {
				if p.at(DEDENT) {
					p.next()
					break
				}
				continue
			}
			p.expect(DEDENT)
			break
		}
	}
	return d
}

func (p *parser) foreignDecl() Decl {
	pos := p.expect(KwForeign).Pos
	var name string
	if p.at(LPAREN) {
		p.next()
		name = p.expect(OP).Text
		p.expect(RPAREN)
	} else {
		name = p.expect(LIDENT).Text
	}
	p.expect(COLON)
	sc := p.schemeTop()
	p.expect(EQUALS)
	target := p.expect(STRING).Str
	return &DForeign{P: pos, Name: name, Scheme: sc, Target: target}
}

func (p *parser) fixityDecl() Decl {
	t := p.next()
	d := &DFixity{P: t.Pos}
	switch t.Kind {
	case KwInfixl:
		d.Assoc = 1
	case KwInfixr:
		d.Assoc = 2
	}
	n := p.expect(INT)
	d.Prec, _ = strconv.Atoi(n.Text)
	for p.at(OP) || p.at(BACKTICK) {
		if p.accept(BACKTICK) {
			d.Ops = append(d.Ops, p.expect(LIDENT).Text)
			p.expect(BACKTICK)
			continue
		}
		d.Ops = append(d.Ops, p.next().Text)
		p.accept(COMMA)
	}
	if len(d.Ops) == 0 {
		p.unexpected("operator")
	}
	return d
}

// ------------------------------------------------------------ types

func (p *parser) skipTypeLayout() {
	for {
		switch {
		case p.at(INDENT):
			p.typeIndent++
			p.next()
		case p.at(NEWLINE) && p.typeIndent > 0:
			p.next()
		case p.at(DEDENT) && p.typeIndent > 0:
			p.typeIndent--
			p.next()
		default:
			return
		}
	}
}

func (p *parser) finishTypeLayout() {
	for p.typeIndent > 0 && p.at(DEDENT) {
		p.typeIndent--
		p.next()
	}
	p.typeIndent = 0
}

func (p *parser) schemeTop() *SchemeE {
	saved := p.typeIndent
	p.typeIndent = 0
	s := p.scheme()
	p.finishTypeLayout()
	p.typeIndent = saved
	return s
}

func (p *parser) typeTop() TypeExpr {
	saved := p.typeIndent
	p.typeIndent = 0
	t := p.typ()
	p.finishTypeLayout()
	p.typeIndent = saved
	return t
}

func (p *parser) scheme() *SchemeE {
	t := p.typ()
	if p.accept(DARROW) {
		p.skipTypeLayout()
		preds := p.predsFromType(t)
		return &SchemeE{Preds: preds, Type: p.typ()}
	}
	return &SchemeE{Type: t}
}

func (p *parser) typ() TypeExpr {
	p.skipTypeLayout()
	t := p.btype()
	p.skipTypeLayout()
	if p.at(ARROW) {
		pos := p.next().Pos
		p.skipTypeLayout()
		var row *RowE
		if p.atOp("<") || p.atOp("<>") {
			row = p.row()
		}
		res := p.typ()
		return &TFunE{P: pos, Arg: t, Eff: row, Res: res}
	}
	return t
}

func (p *parser) row() *RowE {
	pos := p.peek().Pos
	if p.atOp("<>") {
		p.next()
		return &RowE{P: pos}
	}
	p.next() // <
	r := &RowE{P: pos}
	p.skipTypeLayout()
	if !p.atOp(">") && !p.at(BAR) {
		for {
			p.skipTypeLayout()
			r.Labels = append(r.Labels, p.btype())
			p.skipTypeLayout()
			if !p.accept(COMMA) {
				break
			}
		}
	}
	if p.accept(BAR) {
		r.Tail = p.expect(LIDENT).Text
	} else if n := len(r.Labels); n > 0 {
		// `<e>` / `<IO, e>`: a trailing type variable is the row tail
		if v, ok := r.Labels[n-1].(*TVarE); ok {
			r.Tail = v.Name
			r.Labels = r.Labels[:n-1]
		}
	}
	p.skipTypeLayout()
	if p.atOp(">") {
		p.next()
		return r
	}
	// tolerate `<IO>` lexed with a glued operator such as `>>`
	p.unexpected("'>'")
	return nil
}

func (p *parser) startsTypeAtom() bool {
	switch p.peek().Kind {
	case LIDENT, UIDENT, LPAREN:
		return true
	}
	return false
}

func (p *parser) btype() TypeExpr {
	p.skipTypeLayout()
	f := p.typeAtom()
	var args []TypeExpr
	for p.startsTypeAtom() {
		args = append(args, p.typeAtom())
	}
	if len(args) == 0 {
		return f
	}
	if app, ok := f.(*TAppE); ok {
		app.Args = append(app.Args, args...)
		return app
	}
	return &TAppE{P: f.TPos(), Fun: f, Args: args}
}

func (p *parser) typeAtom() TypeExpr {
	t := p.peek()
	switch t.Kind {
	case LIDENT:
		p.next()
		if t.Qual != "" {
			p.fail(t.Pos, "type variables cannot be qualified")
		}
		return &TVarE{P: t.Pos, Name: t.Text}
	case UIDENT:
		p.next()
		return &TConE{P: t.Pos, Qual: t.Qual, Name: t.Text}
	case LPAREN:
		p.next()
		p.skipTypeLayout()
		if p.accept(RPAREN) {
			return &TUnitE{P: t.Pos}
		}
		first := p.typ()
		if p.at(COMMA) {
			elems := []TypeExpr{first}
			for p.accept(COMMA) {
				elems = append(elems, p.typ())
			}
			p.skipTypeLayout()
			p.expect(RPAREN)
			return &TTupleE{P: t.Pos, Elems: elems}
		}
		p.skipTypeLayout()
		p.expect(RPAREN)
		return first
	}
	p.unexpected("type")
	return nil
}

// ------------------------------------------------------------ patterns

func (p *parser) startsPatAtom() bool {
	switch p.peek().Kind {
	case LIDENT, UIDENT, UNDERSCORE, INT, FLOAT, STRING, CHAR, LPAREN, LBRACKET:
		return p.peek().Kind != LIDENT || p.peek().Qual == ""
	}
	return false
}

func (p *parser) pattern() Pattern {
	pat := p.patApp()
	if p.atOp("::") {
		pos := p.next().Pos
		rest := p.pattern()
		return &PCon{P: pos, Name: "::", Args: []Pattern{pat, rest}}
	}
	return pat
}

func (p *parser) patApp() Pattern {
	t := p.peek()
	if t.Kind == UIDENT {
		p.next()
		c := &PCon{P: t.Pos, Qual: t.Qual, Name: t.Text}
		if p.at(LBRACE) {
			p.recordPat(c)
			return c
		}
		for p.startsPatAtom() {
			c.Args = append(c.Args, p.patAtom())
		}
		return c
	}
	return p.patAtom()
}

func (p *parser) litFromToken(t Token) *ELit {
	switch t.Kind {
	case INT:
		v, _ := strconv.ParseInt(t.Text, 0, 64)
		return &ELit{P: t.Pos, Kind: LitInt, Int: v}
	case FLOAT:
		v, _ := strconv.ParseFloat(t.Text, 64)
		return &ELit{P: t.Pos, Kind: LitFloat, Float: v}
	case STRING:
		return &ELit{P: t.Pos, Kind: LitString, Str: t.Str}
	case CHAR:
		return &ELit{P: t.Pos, Kind: LitChar, Str: t.Str}
	}
	p.fail(t.Pos, "expected literal")
	return nil
}

func (p *parser) patAtom() Pattern {
	t := p.peek()
	switch t.Kind {
	case LIDENT:
		p.next()
		if p.at(AT) {
			p.next()
			return &PAs{P: t.Pos, Name: t.Text, Pat: p.patAtom()}
		}
		return &PVar{P: t.Pos, Name: t.Text}
	case UIDENT:
		p.next()
		c := &PCon{P: t.Pos, Qual: t.Qual, Name: t.Text}
		if p.at(LBRACE) {
			p.recordPat(c)
		}
		return c
	case UNDERSCORE:
		p.next()
		return &PWild{P: t.Pos}
	case INT, FLOAT, STRING, CHAR:
		p.next()
		return &PLit{P: t.Pos, Lit: p.litFromToken(t)}
	case LBRACKET:
		p.next()
		l := &PList{P: t.Pos}
		for !p.at(RBRACKET) {
			l.Elems = append(l.Elems, p.pattern())
			if !p.accept(COMMA) {
				break
			}
		}
		p.expect(RBRACKET)
		return l
	case LPAREN:
		p.next()
		if p.accept(RPAREN) {
			return &PUnit{P: t.Pos}
		}
		first := p.pattern()
		if p.accept(COLON) {
			ty := p.typeTop()
			p.expect(RPAREN)
			return &PAnnot{P: t.Pos, Pat: first, Type: ty}
		}
		if p.at(COMMA) {
			elems := []Pattern{first}
			for p.accept(COMMA) {
				elems = append(elems, p.pattern())
			}
			p.expect(RPAREN)
			return &PTuple{P: t.Pos, Elems: elems}
		}
		p.expect(RPAREN)
		return first
	}
	p.unexpected("pattern")
	return nil
}

// recordPat parses `{ f = p, g }` after a constructor; a bare field name
// binds a variable of the same name.
func (p *parser) recordPat(c *PCon) {
	p.expect(LBRACE)
	c.Record = true
	c.Fields = []string{}
	for !p.at(RBRACE) {
		n := p.expect(LIDENT)
		var pat Pattern = &PVar{P: n.Pos, Name: n.Text}
		if p.accept(EQUALS) {
			pat = p.pattern()
		}
		c.Fields = append(c.Fields, n.Text)
		c.Args = append(c.Args, pat)
		if !p.accept(COMMA) {
			break
		}
	}
	p.expect(RBRACE)
}

// ------------------------------------------------------------ expressions

// body parses an expression that may be an indented block.
func (p *parser) body() Expr {
	if p.at(INDENT) {
		return p.block()
	}
	if p.at(KwLet) {
		// `let x = e; body` on one line
		return p.inlineBlock()
	}
	e := p.expr()
	if p.at(SEMI) {
		return p.inlineBlockFrom(e)
	}
	return e
}

func (p *parser) stmt() Stmt {
	if p.at(KwLet) {
		lp := p.next().Pos
		b := p.letBind(lp)
		if p.at(KwIn) {
			p.next()
			return &SExpr{E: &ELet{P: lp, Binds: []*Bind{b}, Body: p.body()}}
		}
		return &SLet{Bind: b}
	}
	return &SExpr{E: p.expr()}
}

// inlineBlock parses statements separated by ';' on one line.
func (p *parser) inlineBlock() Expr {
	pos := p.peek().Pos
	stmts := []Stmt{p.stmt()}
	for p.accept(SEMI) {
		stmts = append(stmts, p.stmt())
	}
	if len(stmts) == 1 {
		if e, ok := stmts[0].(*SExpr); ok {
			return e.E
		}
	}
	return &EBlock{P: pos, Stmts: stmts}
}

func (p *parser) inlineBlockFrom(first Expr) Expr {
	stmts := []Stmt{&SExpr{E: first}}
	for p.accept(SEMI) {
		stmts = append(stmts, p.stmt())
	}
	return &EBlock{P: first.EPos(), Stmts: stmts}
}

func (p *parser) block() Expr {
	pos := p.expect(INDENT).Pos
	whereOK := p.whereOK
	p.whereOK = false
	var stmts []Stmt
	for {
		if p.at(KwLet) {
			lp := p.next().Pos
			b := p.letBind(lp)
			if p.at(KwIn) {
				p.next()
				body := p.body()
				stmts = append(stmts, &SExpr{E: &ELet{P: lp, Binds: []*Bind{b}, Body: body}})
			} else {
				stmts = append(stmts, &SLet{Bind: b})
			}
		} else {
			stmts = append(stmts, &SExpr{E: p.expr()})
		}
		if p.accept(NEWLINE) || p.accept(SEMI) {
			if p.at(KwWhere) {
				if !whereOK {
					p.fail(p.peek().Pos, "a `where` clause belongs to an equation; it cannot end this block")
				}
				// `where` at the column of the equation's body ends it
				p.pendingWhere = p.whereBinds()
				p.accept(NEWLINE)
				p.expect(DEDENT)
				break
			}
			if p.at(DEDENT) {
				p.next()
				break
			}
			continue
		}
		if p.at(DEDENT) {
			p.next()
			break
		}
		p.unexpected("end of line")
	}
	// merge consecutive clauses of local functions
	var merged []Stmt
	for _, s := range stmts {
		if l, ok := s.(*SLet); ok && l.Bind.Name != "" && len(merged) > 0 {
			if pl, ok := merged[len(merged)-1].(*SLet); ok && pl.Bind.Name == l.Bind.Name && pl.Bind.Pat == nil && l.Bind.Sig == nil && len(l.Bind.Clauses) > 0 && len(pl.Bind.Clauses) > 0 && len(l.Bind.Clauses[0].Params) > 0 {
				pl.Bind.Clauses = append(pl.Bind.Clauses, l.Bind.Clauses...)
				continue
			}
		}
		merged = append(merged, s)
	}
	if len(merged) == 1 {
		if e, ok := merged[0].(*SExpr); ok {
			return e.E
		}
	}
	return &EBlock{P: pos, Stmts: merged}
}

// letBind parses the part after `let`.
func (p *parser) letBind(pos Pos) *Bind {
	t := p.peek()
	if t.Kind == LIDENT && t.Qual == "" && p.peekN(1).Kind == COLON {
		p.next()
		p.next()
		sc := p.schemeTop()
		rhs := p.rhs(EQUALS)
		return &Bind{P: t.Pos, Name: t.Text, Sig: sc, Clauses: []*Clause{{P: t.Pos, Rhs: rhs}}}
	}
	if (t.Kind == LIDENT && t.Qual == "" && !p.isInfixDef()) || (t.Kind == LPAREN && p.peekN(1).Kind == OP && p.peekN(2).Kind == RPAREN) {
		return p.funBind()
	}
	if p.isInfixDef() {
		return p.funBind()
	}
	pat := p.pattern()
	p.expect(EQUALS)
	return &Bind{P: pos, Pat: pat, Clauses: []*Clause{{P: pos, Rhs: &Rhs{Body: p.body()}}}}
}

func (p *parser) expr() Expr {
	e := p.opExpr(false)
	if p.at(COLON) {
		pos := p.next().Pos
		return &EAnnot{P: pos, E: e, Type: p.schemeTop()}
	}
	return e
}

func isBlockExpr(e Expr) bool {
	switch e.(type) {
	case *ELam, *EMatch, *EHandle, *ELet:
		return true
	}
	return false
}

// opExpr parses an operator chain. When allowTrailing is set, a chain ending
// with an operator (left section) is returned with the dangling operator.
func (p *parser) opExpr(allowTrailing bool) Expr {
	var operands []Expr
	var ops []*EVar
	for {
		p.trailingBlock = false
		e := p.prefixOperand()
		operands = append(operands, e)
		if p.trailingBlock {
			p.trailingBlock = false
			break
		}
		op := p.tryOperator()
		if op == nil {
			break
		}
		ops = append(ops, op)
		if allowTrailing && p.at(RPAREN) {
			return &ESection{P: op.P, Op: op, Left: chain(operands, ops[:len(ops)-1])}
		}
		// allow an operator at the end of a line followed by an indented block
		if p.at(INDENT) {
			operands = append(operands, p.block())
			break
		}
	}
	return chain(operands, ops)
}

func chain(operands []Expr, ops []*EVar) Expr {
	if len(ops) == 0 {
		return operands[0]
	}
	return &EOpChain{P: operands[0].EPos(), Operands: operands, Ops: ops}
}

func (p *parser) tryOperator() *EVar {
	t := p.peek()
	if t.Kind == OP {
		p.next()
		return &EVar{P: t.Pos, Qual: t.Qual, Name: t.Text}
	}
	if t.Kind == BACKTICK {
		p.next()
		id := p.peek()
		if id.Kind != LIDENT && id.Kind != UIDENT {
			p.unexpected("identifier")
		}
		p.next()
		p.expect(BACKTICK)
		return &EVar{P: id.Pos, Qual: id.Qual, Name: id.Text}
	}
	return nil
}

func (p *parser) prefixOperand() Expr {
	t := p.peek()
	if t.Kind == OP && t.Text == "-" {
		p.next()
		return &ENeg{P: t.Pos, E: p.prefixOperand()}
	}
	switch t.Kind {
	case BACKSLASH, KwMatch, KwHandle, KwLet:
		e := p.blockExpr()
		p.trailingBlock = true
		return e
	}
	return p.app()
}

func (p *parser) blockExpr() Expr {
	t := p.peek()
	switch t.Kind {
	case BACKSLASH:
		p.next()
		var params []Pattern
		for !p.at(ARROW) {
			if !p.startsPatAtom() {
				p.unexpected("lambda parameter or '->'")
			}
			params = append(params, p.patAtom())
		}
		if len(params) == 0 {
			p.fail(t.Pos, "lambda needs at least one parameter")
		}
		p.expect(ARROW)
		return &ELam{P: t.Pos, Params: params, Body: p.body()}
	case KwMatch:
		p.next()
		scrut := p.expr()
		p.expect(KwWith)
		m := &EMatch{P: t.Pos, Scrut: scrut}
		p.expect(INDENT)
		for {
			pos := p.peek().Pos
			pat := p.pattern()
			m.Arms = append(m.Arms, &Arm{P: pos, Pat: pat, Body: p.rhs(ARROW)})
			if p.accept(NEWLINE) {
				if p.at(DEDENT) {
					p.next()
					break
				}
				continue
			}
			p.expect(DEDENT)
			break
		}
		return m
	case KwHandle:
		p.next()
		body := p.expr()
		p.expect(KwWith)
		h := &EHandle{P: t.Pos, Body: body}
		p.expect(INDENT)
		for {
			c := p.peek()
			hc := &HandlerClause{P: c.Pos}
			if p.accept(KwReturn) {
				hc.Params = []Pattern{p.patAtom()}
				if h.Return != nil {
					p.fail(c.Pos, "duplicate return clause")
				}
				h.Return = hc
			} else {
				id := p.expect(LIDENT)
				hc.Name = id.Text
				hc.Params = p.paramPatterns()
				h.Ops = append(h.Ops, hc)
			}
			p.expect(ARROW)
			hc.Body = p.body()
			if p.accept(NEWLINE) {
				if p.at(DEDENT) {
					p.next()
					break
				}
				continue
			}
			p.expect(DEDENT)
			break
		}
		return h
	case KwLet:
		p.next()
		b := p.letBind(t.Pos)
		if p.at(NEWLINE) && p.peekN(1).Kind == KwIn {
			p.next()
		}
		p.expect(KwIn)
		return &ELet{P: t.Pos, Binds: []*Bind{b}, Body: p.body()}
	}
	p.unexpected("expression")
	return nil
}

func (p *parser) startsAtom() bool {
	switch p.peek().Kind {
	case LIDENT, UIDENT, INT, FLOAT, STRING, INTERP, CHAR, LPAREN, LBRACKET, LBRACE, FIELDFN:
		return true
	}
	return false
}

func (p *parser) app() Expr {
	f := p.atom()
	var args []Expr
	for {
		if p.startsAtom() {
			args = append(args, p.atom())
			continue
		}
		switch p.peek().Kind {
		case BACKSLASH, KwMatch, KwHandle:
			args = append(args, p.blockExpr())
			p.trailingBlock = true
		}
		break
	}
	if len(args) == 0 {
		return f
	}
	return &EApp{P: f.EPos(), Fun: f, Args: args}
}

// atom parses an atomic expression followed by any field accesses.
func (p *parser) atom() Expr {
	e := p.atomBase()
	for p.at(FIELD) {
		t := p.next()
		e = &EField{P: t.Pos, E: e, Name: t.Text}
	}
	return e
}

// fieldInits parses `f = e, g, ...` up to the closing brace; a bare field
// name stands for the variable of the same name.
func (p *parser) fieldInits() []*FieldInit {
	var fs []*FieldInit
	for !p.at(RBRACE) {
		n := p.expect(LIDENT)
		f := &FieldInit{P: n.Pos, Name: n.Text}
		if p.accept(EQUALS) {
			f.Value = p.body()
		} else {
			f.Value = &EVar{P: n.Pos, Name: n.Text}
		}
		fs = append(fs, f)
		if !p.accept(COMMA) {
			break
		}
	}
	p.expect(RBRACE)
	return fs
}

func (p *parser) atomBase() Expr {
	t := p.peek()
	switch t.Kind {
	case LIDENT:
		p.next()
		return &EVar{P: t.Pos, Qual: t.Qual, Name: t.Text}
	case UIDENT:
		p.next()
		if p.at(LBRACE) {
			p.next()
			return &ERecord{P: t.Pos, Qual: t.Qual, Name: t.Text, Fields: p.fieldInits()}
		}
		return &ECon{P: t.Pos, Qual: t.Qual, Name: t.Text}
	case FIELDFN:
		p.next()
		return &EField{P: t.Pos, Name: t.Text}
	case LBRACE:
		p.next()
		rec := p.expr()
		p.expect(BAR)
		return &EUpdate{P: t.Pos, E: rec, Fields: p.fieldInits()}
	case INT, FLOAT, STRING, CHAR:
		p.next()
		return p.litFromToken(t)
	case INTERP:
		p.next()
		return p.interp(t)
	case LBRACKET:
		p.next()
		l := &EList{P: t.Pos}
		for !p.at(RBRACKET) {
			l.Elems = append(l.Elems, p.expr())
			if !p.accept(COMMA) {
				break
			}
		}
		p.expect(RBRACKET)
		// [a .. b] is sugar for the range a .. b
		if len(l.Elems) == 1 {
			if c, ok := l.Elems[0].(*EOpChain); ok && len(c.Ops) == 1 && c.Ops[0].Name == ".." {
				return c
			}
		}
		return l
	case LPAREN:
		p.next()
		if p.accept(RPAREN) {
			return &EUnit{P: t.Pos}
		}
		// (op) and right sections (op e)
		if (p.at(OP) && p.peek().Text != "-") || p.at(BACKTICK) {
			op := p.tryOperator()
			if p.accept(RPAREN) {
				return op
			}
			right := p.expr()
			p.expect(RPAREN)
			return &ESection{P: t.Pos, Op: op, Right: right}
		}
		if p.at(OP) && p.peek().Text == "-" && p.peekN(1).Kind == RPAREN {
			op := p.tryOperator()
			p.next()
			return op
		}
		first := p.opExpr(true)
		if s, ok := first.(*ESection); ok {
			p.expect(RPAREN)
			return s
		}
		if p.at(COLON) {
			pos := p.next().Pos
			ty := p.schemeTop()
			p.expect(RPAREN)
			return &EAnnot{P: pos, E: first, Type: ty}
		}
		if p.at(COMMA) {
			elems := []Expr{first}
			for p.accept(COMMA) {
				elems = append(elems, p.expr())
			}
			p.expect(RPAREN)
			return &ETuple{P: t.Pos, Elems: elems}
		}
		p.expect(RPAREN)
		return first
	}
	p.unexpected("expression")
	return nil
}

func (p *parser) interp(t Token) Expr {
	e := &EInterp{P: t.Pos}
	for _, part := range t.Parts {
		if !part.IsExpr {
			e.Parts = append(e.Parts, &ELit{P: t.Pos, Kind: LitString, Str: part.Lit})
			continue
		}
		sub := &parser{toks: part.Tokens}
		if len(part.Tokens) <= 1 {
			p.fail(part.Pos, "empty interpolation")
		}
		x := sub.expr()
		if !sub.at(EOF) {
			sub.unexpected("'}'")
		}
		e.Parts = append(e.Parts, x)
	}
	return e
}

// ------------------------------------------------------------ debug printing

// ExprString renders an expression for debugging.
func ExprString(e Expr) string {
	switch e := e.(type) {
	case *EVar:
		if e.Qual != "" {
			return e.Qual + "." + e.Name
		}
		return e.Name
	case *ECon:
		return e.Name
	case *ELit:
		switch e.Kind {
		case LitInt:
			return fmt.Sprint(e.Int)
		case LitFloat:
			return fmt.Sprint(e.Float)
		case LitString:
			return strconv.Quote(e.Str)
		default:
			return "'" + e.Str + "'"
		}
	case *EApp:
		parts := []string{ExprString(e.Fun)}
		for _, a := range e.Args {
			parts = append(parts, ExprString(a))
		}
		return "(" + strings.Join(parts, " ") + ")"
	case *EBinOp:
		return "(" + ExprString(e.Left) + " " + e.Op.Name + " " + ExprString(e.Right) + ")"
	case *EOpChain:
		var sb strings.Builder
		sb.WriteString("{")
		for i, o := range e.Operands {
			if i > 0 {
				sb.WriteString(" " + e.Ops[i-1].Name + " ")
			}
			sb.WriteString(ExprString(o))
		}
		sb.WriteString("}")
		return sb.String()
	case *ELam:
		return fmt.Sprintf("(\\%d -> %s)", len(e.Params), ExprString(e.Body))
	case *ETuple:
		var ps []string
		for _, x := range e.Elems {
			ps = append(ps, ExprString(x))
		}
		return "(" + strings.Join(ps, ", ") + ")"
	case *EList:
		var ps []string
		for _, x := range e.Elems {
			ps = append(ps, ExprString(x))
		}
		return "[" + strings.Join(ps, ", ") + "]"
	case *EUnit:
		return "()"
	case *ENeg:
		return "-" + ExprString(e.E)
	case *EBlock:
		return fmt.Sprintf("{block %d}", len(e.Stmts))
	case *EMatch:
		return fmt.Sprintf("(match %s with %d arms)", ExprString(e.Scrut), len(e.Arms))
	}
	return fmt.Sprintf("%T", e)
}
