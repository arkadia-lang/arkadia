package syntax

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/verniyyy/arkadia/internal/diag"
)

const opChars = "!#$%&*+./<=>?@\\^|-~:"

func isOpChar(r rune) bool { return r < 128 && strings.ContainsRune(opChars, r) }

func isIdentStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }

func isIdentChar(r rune) bool {
	return r == '_' || r == '\'' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

type lexer struct {
	file   string
	src    string
	off    int
	line   int
	col    int
	toks   []Token
	errs   diag.List
	sawNL  bool
	sawSpc bool
	start  int // byte offset where the current token starts
	// comments are recorded only when keepComments is set (for the
	// formatter)
	keepComments bool
	comments     []comment
}

// comment is the position of a line or block comment.
type comment struct {
	off, end  int
	line, col int
}

// Lex tokenizes a source file and applies the layout algorithm.
func Lex(file, src string) ([]Token, error) {
	raw, err := lexRaw(file, src, 1, 1)
	if err != nil {
		return nil, err
	}
	return layout(raw)
}

func lexRaw(file, src string, line, col int) ([]Token, error) {
	lx := &lexer{file: file, src: src, line: line, col: col, sawNL: true, sawSpc: true}
	lx.run()
	if len(lx.errs) > 0 {
		return nil, lx.errs
	}
	return lx.toks, nil
}

func (lx *lexer) pos() diag.Pos { return diag.Pos{File: lx.file, Line: lx.line, Col: lx.col} }

func (lx *lexer) errorf(p diag.Pos, f string, a ...any) {
	lx.errs = append(lx.errs, diag.Errorf(p, f, a...))
}

func (lx *lexer) peek() rune {
	if lx.off >= len(lx.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(lx.src[lx.off:])
	return r
}

func (lx *lexer) peekAt(n int) rune {
	off := lx.off
	for i := 0; i < n; i++ {
		if off >= len(lx.src) {
			return 0
		}
		_, sz := utf8.DecodeRuneInString(lx.src[off:])
		off += sz
	}
	if off >= len(lx.src) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(lx.src[off:])
	return r
}

func (lx *lexer) next() rune {
	if lx.off >= len(lx.src) {
		return 0
	}
	r, sz := utf8.DecodeRuneInString(lx.src[lx.off:])
	lx.off += sz
	if r == '\n' {
		lx.line++
		lx.col = 1
	} else {
		lx.col++
	}
	return r
}

func (lx *lexer) emit(t Token) {
	t.first = lx.sawNL
	t.space = lx.sawSpc
	t.off = lx.start
	t.end = lx.off
	lx.sawNL = false
	lx.sawSpc = false
	lx.toks = append(lx.toks, t)
}

func (lx *lexer) prevEndsExpr() bool {
	if len(lx.toks) == 0 {
		return false
	}
	switch lx.toks[len(lx.toks)-1].Kind {
	case LIDENT, UIDENT, INT, FLOAT, STRING, INTERP, CHAR, RPAREN, RBRACKET, RBRACE, UNDERSCORE, FIELD, FIELDFN:
		return true
	}
	return false
}

func (lx *lexer) run() {
	for lx.off < len(lx.src) {
		r := lx.peek()
		lx.start = lx.off
		switch {
		case r == '\n':
			lx.next()
			lx.sawNL = true
			lx.sawSpc = true
		case r == ' ' || r == '\r':
			lx.next()
			lx.sawSpc = true
		case r == '\t':
			lx.errorf(lx.pos(), "tab characters are not allowed; use spaces for indentation")
			lx.next()
		case r == '-' && lx.peekAt(1) == '-' && !isOpChar(lx.peekAt(2)) || r == '-' && lx.peekAt(1) == '-' && lx.peekAt(2) == '-':
			p := lx.pos()
			for lx.off < len(lx.src) && lx.peek() != '\n' {
				lx.next()
			}
			lx.comment(p)
			lx.sawSpc = true
		case r == '{' && lx.peekAt(1) == '-':
			p := lx.pos()
			lx.blockComment()
			lx.comment(p)
			lx.sawSpc = true
		case unicode.IsDigit(r):
			lx.number(false)
		case r == '-' && unicode.IsDigit(lx.peekAt(1)) && (lx.sawSpc || !lx.prevEndsExpr()) && !lx.prevEndsExprTight():
			lx.number(true)
		case isIdentStart(r):
			lx.ident()
		case r == '"':
			lx.str()
		case r == '\'':
			lx.char()
		default:
			lx.punct()
		}
	}
	lx.start = lx.off
	lx.emit(Token{Kind: EOF, Pos: lx.pos()})
}

// comment records the comment that started at p and ends at the current
// position.
func (lx *lexer) comment(p diag.Pos) {
	if lx.keepComments {
		lx.comments = append(lx.comments, comment{off: lx.start, end: lx.off, line: p.Line, col: p.Col})
	}
}

// prevEndsExprTight reports whether the previous token ends an expression and
// there was no whitespace between it and the current position (e.g. `x-1`).
func (lx *lexer) prevEndsExprTight() bool { return !lx.sawSpc && lx.prevEndsExpr() }

func (lx *lexer) blockComment() {
	start := lx.pos()
	depth := 0
	for lx.off < len(lx.src) {
		if lx.peek() == '{' && lx.peekAt(1) == '-' {
			lx.next()
			lx.next()
			depth++
			continue
		}
		if lx.peek() == '-' && lx.peekAt(1) == '}' {
			lx.next()
			lx.next()
			depth--
			if depth == 0 {
				return
			}
			continue
		}
		if lx.next() == '\n' {
			lx.sawNL = true
		}
	}
	lx.errorf(start, "unterminated block comment")
}

func (lx *lexer) number(neg bool) {
	p := lx.pos()
	start := lx.off
	if neg {
		lx.next()
	}
	isFloat := false
	if lx.peek() == '0' && (lx.peekAt(1) == 'x' || lx.peekAt(1) == 'b' || lx.peekAt(1) == 'o') {
		lx.next()
		lx.next()
		for isIdentChar(lx.peek()) {
			lx.next()
		}
	} else {
		for unicode.IsDigit(lx.peek()) || lx.peek() == '_' {
			lx.next()
		}
		if lx.peek() == '.' && unicode.IsDigit(lx.peekAt(1)) {
			isFloat = true
			lx.next()
			for unicode.IsDigit(lx.peek()) || lx.peek() == '_' {
				lx.next()
			}
		}
		if lx.peek() == 'e' || lx.peek() == 'E' {
			if unicode.IsDigit(lx.peekAt(1)) || (lx.peekAt(1) == '-' || lx.peekAt(1) == '+') && unicode.IsDigit(lx.peekAt(2)) {
				isFloat = true
				lx.next()
				if lx.peek() == '-' || lx.peek() == '+' {
					lx.next()
				}
				for unicode.IsDigit(lx.peek()) {
					lx.next()
				}
			}
		}
	}
	text := strings.ReplaceAll(lx.src[start:lx.off], "_", "")
	if isFloat {
		if _, err := strconv.ParseFloat(text, 64); err != nil {
			lx.errorf(p, "invalid float literal %q", text)
		}
		lx.emit(Token{Kind: FLOAT, Text: text, Pos: p})
		return
	}
	if _, err := strconv.ParseInt(text, 0, 64); err != nil {
		lx.errorf(p, "invalid integer literal %q", text)
	}
	lx.emit(Token{Kind: INT, Text: text, Pos: p})
}

func (lx *lexer) ident() {
	p := lx.pos()
	readSeg := func() string {
		s := lx.off
		for isIdentChar(lx.peek()) {
			lx.next()
		}
		return lx.src[s:lx.off]
	}
	seg := readSeg()
	var quals []string
	// Qualified names: Upper.Upper.name
	for unicode.IsUpper([]rune(seg)[0]) && lx.peek() == '.' && isIdentStart(lx.peekAt(1)) {
		lx.next()
		quals = append(quals, seg)
		seg = readSeg()
	}
	qual := strings.Join(quals, ".")
	if qual == "" {
		if seg == "_" {
			lx.emit(Token{Kind: UNDERSCORE, Text: "_", Pos: p})
			return
		}
		if k, ok := keywords[seg]; ok {
			lx.emit(Token{Kind: k, Text: seg, Pos: p})
			return
		}
	}
	kind := LIDENT
	if unicode.IsUpper([]rune(seg)[0]) {
		kind = UIDENT
	}
	lx.emit(Token{Kind: kind, Text: seg, Qual: qual, Pos: p})
}

func (lx *lexer) escape(p diag.Pos) (rune, bool) {
	c := lx.next()
	switch c {
	case 'n':
		return '\n', true
	case 't':
		return '\t', true
	case 'r':
		return '\r', true
	case '0':
		return 0, true
	case '\\', '"', '\'', '$':
		return c, true
	case 'u':
		if lx.peek() == '{' {
			lx.next()
			s := lx.off
			for lx.peek() != '}' && lx.peek() != 0 && lx.peek() != '\n' {
				lx.next()
			}
			hex := lx.src[s:lx.off]
			lx.next()
			v, err := strconv.ParseUint(hex, 16, 32)
			if err != nil {
				lx.errorf(p, "invalid unicode escape")
				return 0, false
			}
			return rune(v), true
		}
	}
	lx.errorf(p, "invalid escape sequence '\\%c'", c)
	return 0, false
}

func (lx *lexer) str() {
	p := lx.pos()
	lx.next()
	// triple-quoted raw multi-line string
	if lx.peek() == '"' && lx.peekAt(1) == '"' {
		lx.next()
		lx.next()
		s := lx.off
		for lx.off < len(lx.src) && !(lx.peek() == '"' && lx.peekAt(1) == '"' && lx.peekAt(2) == '"') {
			lx.next()
		}
		body := lx.src[s:lx.off]
		if lx.off >= len(lx.src) {
			lx.errorf(p, "unterminated string literal")
			return
		}
		lx.next()
		lx.next()
		lx.next()
		body = strings.TrimPrefix(body, "\n")
		lx.emit(Token{Kind: STRING, Text: strconv.Quote(body), Str: body, Pos: p})
		return
	}
	var parts []InterpPart
	var sb strings.Builder
	for {
		c := lx.peek()
		if c == 0 || c == '\n' {
			lx.errorf(p, "unterminated string literal")
			return
		}
		if c == '"' {
			lx.next()
			break
		}
		if c == '\\' {
			ep := lx.pos()
			lx.next()
			if r, ok := lx.escape(ep); ok {
				sb.WriteRune(r)
			}
			continue
		}
		if c == '$' && lx.peekAt(1) == '{' {
			ep := lx.pos()
			lx.next()
			lx.next()
			if sb.Len() > 0 || len(parts) == 0 {
				parts = append(parts, InterpPart{Lit: sb.String()})
			}
			sb.Reset()
			startLine, startCol := lx.line, lx.col
			s := lx.off
			depth := 1
			for depth > 0 {
				c := lx.peek()
				if c == 0 || c == '\n' {
					lx.errorf(ep, "unterminated interpolation")
					return
				}
				if c == '{' {
					depth++
				} else if c == '}' {
					depth--
					if depth == 0 {
						break
					}
				} else if c == '"' {
					// skip nested string
					lx.next()
					for lx.peek() != '"' && lx.peek() != 0 && lx.peek() != '\n' {
						if lx.peek() == '\\' {
							lx.next()
						}
						lx.next()
					}
				}
				lx.next()
			}
			inner := lx.src[s:lx.off]
			lx.next() // '}'
			toks, err := lexRaw(lx.file, inner, startLine, startCol)
			if err != nil {
				lx.errs = append(lx.errs, err.(diag.List)...)
				return
			}
			for i := range toks {
				toks[i].first = false
			}
			parts = append(parts, InterpPart{Tokens: toks, IsExpr: true, Pos: ep})
			continue
		}
		sb.WriteRune(lx.next())
	}
	if parts == nil {
		s := sb.String()
		lx.emit(Token{Kind: STRING, Text: strconv.Quote(s), Str: s, Pos: p})
		return
	}
	if sb.Len() > 0 {
		parts = append(parts, InterpPart{Lit: sb.String()})
	}
	lx.emit(Token{Kind: INTERP, Text: "\"...\"", Parts: parts, Pos: p})
}

func (lx *lexer) char() {
	p := lx.pos()
	lx.next()
	var r rune
	if lx.peek() == '\\' {
		lx.next()
		r, _ = lx.escape(p)
	} else {
		r = lx.next()
	}
	if lx.peek() != '\'' {
		lx.errorf(p, "unterminated character literal")
		return
	}
	lx.next()
	lx.emit(Token{Kind: CHAR, Text: strconv.QuoteRune(r), Str: string(r), Pos: p})
}

func (lx *lexer) punct() {
	p := lx.pos()
	r := lx.peek()
	single := map[rune]Kind{
		'(': LPAREN, ')': RPAREN, '[': LBRACKET, ']': RBRACKET, '{': LBRACE, '}': RBRACE,
		',': COMMA, '`': BACKTICK, ';': SEMI,
	}
	if k, ok := single[r]; ok {
		lx.next()
		lx.emit(Token{Kind: k, Text: string(r), Pos: p})
		return
	}
	if !isOpChar(r) {
		lx.next()
		lx.errorf(p, "unexpected character %q", r)
		return
	}
	// `.name`: field access when glued to the preceding expression,
	// otherwise a field accessor function
	if r == '.' && isIdentStart(lx.peekAt(1)) && !unicode.IsUpper(lx.peekAt(1)) && lx.peekAt(1) != '_' {
		kind := FIELDFN
		if !lx.sawSpc && lx.prevEndsExpr() {
			kind = FIELD
		}
		lx.next()
		s := lx.off
		for isIdentChar(lx.peek()) {
			lx.next()
		}
		lx.emit(Token{Kind: kind, Text: lx.src[s:lx.off], Pos: p})
		return
	}
	s := lx.off
	for isOpChar(lx.peek()) {
		lx.next()
	}
	op := lx.src[s:lx.off]
	switch op {
	case "=":
		lx.emit(Token{Kind: EQUALS, Text: op, Pos: p})
	case "|":
		lx.emit(Token{Kind: BAR, Text: op, Pos: p})
	case "->":
		lx.emit(Token{Kind: ARROW, Text: op, Pos: p})
	case ":":
		lx.emit(Token{Kind: COLON, Text: op, Pos: p})
	case "=>":
		lx.emit(Token{Kind: DARROW, Text: op, Pos: p})
	case "@":
		lx.emit(Token{Kind: AT, Text: op, Pos: p})
	case "\\":
		lx.emit(Token{Kind: BACKSLASH, Text: op, Pos: p})
	default:
		if strings.HasPrefix(op, "\\") {
			// `\x` is lexed as BACKSLASH; `\(` etc. as BACKSLASH followed by more.
			lx.off = s
			lx.col = p.Col
			lx.next()
			lx.emit(Token{Kind: BACKSLASH, Text: "\\", Pos: p})
			return
		}
		lx.emit(Token{Kind: OP, Text: op, Pos: p})
	}
}
