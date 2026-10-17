// Package syntax implements the lexer, layout resolution and parser of Arkadia.
package syntax

import (
	"fmt"

	"github.com/verniyyy/arkadia/internal/diag"
)

type Kind int

const (
	EOF Kind = iota
	NEWLINE
	INDENT
	DEDENT

	LIDENT // lower-case identifier (possibly qualified: Qual set)
	UIDENT // upper-case identifier (possibly qualified)
	OP     // symbolic operator
	INT
	FLOAT
	STRING
	INTERP // interpolated string
	CHAR

	LPAREN
	RPAREN
	LBRACKET
	RBRACKET
	LBRACE
	RBRACE
	COMMA
	SEMI
	BACKTICK
	BACKSLASH
	EQUALS // =
	BAR    // |
	ARROW  // ->
	COLON  // :
	DARROW // =>
	AT     // @
	UNDERSCORE
	FIELD   // `.name` directly after an expression (field access)
	FIELDFN // `.name` elsewhere (field accessor function)

	// keywords
	KwModule
	KwImport
	KwAs
	KwType
	KwAlias
	KwClass
	KwInstance
	KwEffect
	KwWhere
	KwLet
	KwIn
	KwMatch
	KwWith
	KwHandle
	KwReturn
	KwForeign
	KwInfixl
	KwInfixr
	KwInfix
	KwPrivate
	KwTest
	KwDeriving
)

var keywords = map[string]Kind{
	"module":   KwModule,
	"import":   KwImport,
	"type":     KwType,
	"class":    KwClass,
	"instance": KwInstance,
	"effect":   KwEffect,
	"where":    KwWhere,
	"let":      KwLet,
	"in":       KwIn,
	"match":    KwMatch,
	"with":     KwWith,
	"handle":   KwHandle,
	"return":   KwReturn,
	"foreign":  KwForeign,
	"infixl":   KwInfixl,
	"infixr":   KwInfixr,
	"infix":    KwInfix,
	"private":  KwPrivate,
}

var kindNames = map[Kind]string{
	EOF: "end of file", NEWLINE: "newline", INDENT: "indent", DEDENT: "dedent",
	LIDENT: "identifier", UIDENT: "constructor", OP: "operator", INT: "integer",
	FLOAT: "float", STRING: "string", INTERP: "string", CHAR: "character",
	LPAREN: "'('", RPAREN: "')'", LBRACKET: "'['", RBRACKET: "']'", LBRACE: "'{'",
	RBRACE: "'}'", COMMA: "','", SEMI: "';'", BACKTICK: "'`'", BACKSLASH: "'\\'", EQUALS: "'='",
	BAR: "'|'", ARROW: "'->'", COLON: "':'", DARROW: "'=>'", AT: "'@'", UNDERSCORE: "'_'",
	FIELD: "field", FIELDFN: "field",
}

func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	for name, kk := range keywords {
		if kk == k {
			return "'" + name + "'"
		}
	}
	return fmt.Sprintf("token(%d)", int(k))
}

// InterpPart is a piece of an interpolated string: either literal text or
// the tokens of an embedded expression.
type InterpPart struct {
	Lit    string
	Tokens []Token // nil for a literal part
	IsExpr bool
	Pos    diag.Pos
}

type Token struct {
	Kind  Kind
	Text  string // identifier name / operator / literal source
	Qual  string // module qualifier for identifiers ("Std.List")
	Pos   diag.Pos
	Str   string // decoded string / char literal
	Parts []InterpPart

	first bool // first token on its line
	space bool // preceded by whitespace
	off   int  // byte offsets of the token's source text
	end   int
}

func (t Token) String() string {
	switch t.Kind {
	case LIDENT, UIDENT, OP:
		if t.Qual != "" {
			return "'" + t.Qual + "." + t.Text + "'"
		}
		return "'" + t.Text + "'"
	case INT, FLOAT, STRING, CHAR:
		return t.Text
	case FIELD, FIELDFN:
		return "'." + t.Text + "'"
	}
	return t.Kind.String()
}
