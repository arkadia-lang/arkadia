package syntax

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/verniyyy/arkadia/internal/diag"
)

// Formatting
//
// The formatter works on the token stream rather than the syntax tree, so it
// keeps comments, literals and the author's line breaks exactly. It
//
//   - indents every block two spaces past the line that opens it (four past
//     a guard `| cond =`) and continuation lines two spaces past their item
//     unless they are aligned with the previous line (see layoutColumns),
//   - normalizes the spaces between tokens on a line: one space around `=`,
//     `->`, `=>`, `|`, `:` and binary operators and after `,`; none inside
//     `(..)` / `[..]` on one line and before `,`; one inside `{..}` on one
//     line; runs of spaces become one (except before a trailing comment,
//     where alignment is kept),
//   - removes trailing whitespace, collapses runs of blank lines into one and
//     ends the file with a single newline.
//
// The contents of multi-line block comments and triple-quoted strings are
// never touched. As a safety net the result is lexed again and must produce
// the same tokens, layout and comments as the input.

const indentStep = 2

// fitem is a token or a comment of the source, in source order.
type fitem struct {
	off, end  int
	line, col int
	endLine   int
	tok       int // index into the raw tokens; -1 for a comment
	lineCmt   bool
	first     bool // first item on its line
}

// Format returns the formatted source of a file.
func Format(file, src string) (string, error) {
	lx := &lexer{file: file, src: src, line: 1, col: 1, sawNL: true, sawSpc: true, keepComments: true}
	lx.run()
	if len(lx.errs) > 0 {
		return "", lx.errs
	}
	raw := lx.toks
	want, err := layout(raw)
	if err != nil {
		return "", err
	}
	out := formatTokens(src, raw, lx.comments)
	if err := sameTokens(file, src, want, lx.comments, out); err != nil {
		return "", err
	}
	return out, nil
}

// items merges the tokens (without EOF) and comments in source order.
func items(src string, raw []Token, comments []comment) []fitem {
	var items []fitem
	ci := 0
	addComment := func(c comment) {
		items = append(items, fitem{off: c.off, end: c.end, line: c.line, col: c.col, tok: -1, lineCmt: strings.HasPrefix(src[c.off:], "--")})
	}
	for i, t := range raw[:len(raw)-1] {
		for ci < len(comments) && comments[ci].off < t.off {
			addComment(comments[ci])
			ci++
		}
		items = append(items, fitem{off: t.off, end: t.end, line: t.Pos.Line, col: t.Pos.Col, tok: i})
	}
	for ; ci < len(comments); ci++ {
		addComment(comments[ci])
	}
	for i := range items {
		it := &items[i]
		it.endLine = it.line + strings.Count(src[it.off:it.end], "\n")
		it.first = i == 0 || items[i-1].endLine < it.line
	}
	return items
}

func formatTokens(src string, raw []Token, comments []comment) string {
	items := items(src, raw, comments)
	if len(items) == 0 {
		return ""
	}

	sp := newSpacing(src, raw)
	gaps := make([]string, len(items))
	lineOff := make([]int, len(raw)) // a token's new offset from the start of its line
	off := 0
	for i, it := range items {
		if it.first {
			off = 0
		} else {
			gaps[i] = sp.gap(items[i-1], it)
			prev := src[items[i-1].off:items[i-1].end]
			off += utf8.RuneCountInString(prev[strings.LastIndexByte(prev, '\n')+1:]) + len(gaps[i])
		}
		if it.tok >= 0 {
			lineOff[it.tok] = off
		}
	}
	tokCol := layoutColumns(raw, lineOff)

	// the new column of every item that starts a line
	type lineStart struct{ item, old, new int }
	var starts []lineStart
	for i, it := range items {
		if it.first {
			nc := 0
			if it.tok >= 0 {
				nc = tokCol[it.tok]
			}
			starts = append(starts, lineStart{i, it.col, nc})
		}
	}
	// comment lines: aligned with the next code line when they were aligned
	// with it, else with the previous line, else shifted like the next line
	for k := range starts {
		if items[starts[k].item].tok >= 0 {
			continue
		}
		old := starts[k].old
		next := -1
		for j := k + 1; j < len(starts); j++ {
			if items[starts[j].item].tok >= 0 {
				next = j
				break
			}
		}
		switch {
		case next >= 0 && starts[next].old == old:
			starts[k].new = starts[next].new
		case k > 0 && starts[k-1].old == old:
			starts[k].new = starts[k-1].new
		case next >= 0:
			starts[k].new = old + starts[next].new - starts[next].old
		case k > 0:
			starts[k].new = old + starts[k-1].new - starts[k-1].old
		default:
			starts[k].new = 1
		}
		if starts[k].new < 1 {
			starts[k].new = 1
		}
	}

	var b strings.Builder
	k := 0
	for i, it := range items {
		if it.first {
			if i > 0 {
				b.WriteByte('\n')
				if it.line-items[i-1].endLine > 1 {
					b.WriteByte('\n')
				}
			}
			b.WriteString(strings.Repeat(" ", starts[k].new-1))
			k++
		} else {
			b.WriteString(gaps[i])
		}
		text := src[it.off:it.end]
		if it.lineCmt {
			text = strings.TrimRight(text, " \r")
		}
		b.WriteString(text)
	}
	b.WriteByte('\n')
	return b.String()
}

// layoutColumns mirrors the layout algorithm and returns the new column of
// every token that starts a line (0 for the others); lineOff holds the new
// offset of every token from the start of its line.
//
// A block is indented past the line that opens it: the line of the opener,
// or for `with` the line of its `match` / `handle`. The step is two spaces,
// four past the `|` of a guard (when it starts the line or more guards
// follow). A block inside brackets is also indented by four when a `match`
// on its base line is still open, to set it apart from the block after that
// match's `with`:
//
//	combineAll rs = match foldr (\r (es, as) -> match r with
//	    Ok a -> (es, a :: as)
//	    Err e -> (e :: es, as)) ([], []) rs with
//	  ([], as) -> Ok as
//	  (es, _) -> Err es
//
// A continuation line of an item goes where an earlier continuation line in
// the same column went, else stays aligned with a token of the same kind on
// the previous line that it was aligned with (`|` under `|`, an argument
// under an argument), else is indented two spaces past the item (or past the
// deepest earlier continuation line left of it).
func layoutColumns(raw []Token, lineOff []int) []int {
	// a line, or the guard `|` on a line (the body of a guard is indented
	// past the `|`)
	type line struct {
		ln, col, newCol int
		guard           bool
	}
	type fctx struct {
		bracket bool
		col     int // column in the input
		newCol  int // column in the output
		// for blocks: the line starting the current item, the new columns
		// of its continuation lines by their old column and the new column
		// of its first guard `|` (where later guards go)
		item     line
		cont     map[int]int
		guardCol int
		typeDecl bool // the item is a type declaration: `|` separates constructors
	}
	cols := make([]int, len(raw))
	first := line{ln: raw[0].Pos.Line, col: raw[0].Pos.Col, newCol: 1}
	isType := func(i int) bool { return raw[i].Kind == KwType || raw[i].Kind == KwPrivate && raw[i+1].Kind == KwType }
	stack := []*fctx{{col: first.col, newCol: 1, item: first, cont: map[int]int{}, typeDecl: isType(0)}}
	top := func() *fctx { return stack[len(stack)-1] }
	enclosing := func() *fctx {
		for i := len(stack) - 1; i >= 0; i-- {
			if !stack[i].bracket {
				return stack[i]
			}
		}
		return stack[0]
	}
	// the new column of a continuation line at column c of the item of enc
	continuation := func(enc *fctx, t Token, prevToks []int) int {
		c := t.Pos.Col
		if c <= enc.item.col {
			return c + enc.newCol - enc.col
		}
		if t.Kind == BAR && enc.guardCol > 0 {
			return enc.guardCol
		}
		if nc, ok := enc.cont[c]; ok {
			return nc
		}
		nc := 0
		for _, j := range prevToks {
			if raw[j].Pos.Col == c && raw[j].Kind == t.Kind && raw[j].Text == t.Text {
				nc = cols[prevToks[0]] + lineOff[j]
				break
			}
		}
		if nc == 0 {
			nc = enc.item.newCol + indentStep
			best := 0
			for old, n := range enc.cont {
				if old < c && old > best {
					nc, best = n+indentStep, old
				}
			}
		}
		enc.cont[c] = nc
		return nc
	}
	cur := first                // the current line
	var guard line              // its guard `|`, if any
	var matches []line          // lines of the open `match` / `handle`s
	var withLine line           // line of the last `with`'s `match`
	var prevToks, curToks []int // tokens of the previous and the current line
	depth, lineDepth := 0, 0    // bracket nesting, at the start of the line
	for i, t := range raw {
		if t.Kind == EOF {
			break
		}
		if i == 0 {
			cols[i] = 1
		} else if t.first {
			prev := raw[i-1].Kind
			c := t.Pos.Col
			enc := enclosing()
			prevToks, curToks = curToks, nil
			nc := 0
			newItem := false
			switch {
			case c > top().col || top().bracket:
				if isOpener(prev) && c > enc.col {
					base := cur
					if guard.guard {
						base = guard
					}
					if prev == KwWith {
						base = withLine
					}
					step := indentStep
					if base.guard || top().bracket && slices.Contains(matches, base) {
						step = 2 * indentStep
					}
					nc = base.newCol + step
					stack = append(stack, &fctx{col: c, newCol: nc})
					newItem = true
				} else {
					nc = continuation(enc, t, prevToks)
				}
			case c == top().col:
				nc = top().newCol
				newItem = true
			default:
				popped := 0
				for !top().bracket && c < top().col && len(stack) > 1 {
					popped = top().newCol
					stack = stack[:len(stack)-1]
				}
				if !top().bracket && c == top().col {
					nc = top().newCol
					newItem = true
				} else {
					// a continuation line left of the closed blocks
					nc = min(continuation(enclosing(), t, prevToks), popped-1)
				}
			}
			cols[i] = nc
			cur = line{ln: t.Pos.Line, col: c, newCol: nc}
			if newItem {
				top().item, top().cont, top().guardCol = cur, map[int]int{}, 0
				top().typeDecl = isType(i)
			}
			guard = line{}
			lineDepth = depth
		}
		curToks = append(curToks, i)
		switch t.Kind {
		case BAR:
			if depth == lineDepth && !guard.guard && !enclosing().typeDecl {
				nc := cur.newCol + lineOff[i]
				if enc := enclosing(); enc.guardCol == 0 {
					enc.guardCol = nc
				}
				if t.first || moreGuards(raw, i, enclosing().item.col) {
					guard = line{ln: cur.ln, col: t.Pos.Col, newCol: nc, guard: true}
				}
			}
		case KwMatch, KwHandle:
			if guard.guard {
				matches = append(matches, guard)
			} else {
				matches = append(matches, cur)
			}
		case KwWith:
			withLine = cur
			if n := len(matches); n > 0 {
				withLine = matches[n-1]
				matches = matches[:n-1]
			}
		case LPAREN, LBRACKET, LBRACE:
			stack = append(stack, &fctx{bracket: true})
			depth++
		case RPAREN, RBRACKET, RBRACE:
			depth--
			for len(stack) > 1 && !top().bracket {
				stack = stack[:len(stack)-1]
			}
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return cols
}

// moreGuards reports whether the guard raw[i] in the middle of a line is
// followed by a line starting with another guard `|` before a line at or left
// of the item's column col ends the item. Then the bodies are indented past
// the `|`s.
func moreGuards(raw []Token, i, col int) bool {
	depth := 0
	for j := i + 1; j < len(raw); j++ {
		t := raw[j]
		if t.first {
			if t.Pos.Col <= col {
				return false
			}
			if t.Kind == BAR && depth == 0 {
				return true
			}
		}
		switch t.Kind {
		case LPAREN, LBRACKET, LBRACE:
			depth++
		case RPAREN, RBRACKET, RBRACE:
			depth--
		}
	}
	return false
}

// spacing decides the spaces between the items of a line.
type spacing struct {
	src     string
	raw     []Token
	partner []int  // index of the matching bracket (or -1)
	row     []bool // `<` / `>` delimiting an effect row
}

func newSpacing(src string, raw []Token) *spacing {
	sp := &spacing{src: src, raw: raw, partner: make([]int, len(raw)), row: make([]bool, len(raw))}
	var brackets, rows []int
	for i, t := range raw {
		sp.partner[i] = -1
		switch t.Kind {
		case LPAREN, LBRACKET, LBRACE:
			brackets = append(brackets, i)
		case RPAREN, RBRACKET, RBRACE:
			if n := len(brackets); n > 0 {
				sp.partner[i], sp.partner[brackets[n-1]] = brackets[n-1], i
				brackets = brackets[:n-1]
			}
		case OP:
			// a row is written tight: `<IO, State s | e>`
			switch {
			case t.Text == "<" && !raw[i+1].space && (i == 0 || !endsOperand(raw[i-1].Kind)):
				rows = append(rows, i)
				sp.row[i] = true
			case t.Text == ">" && !t.space && len(rows) > 0:
				rows = rows[:len(rows)-1]
				sp.row[i] = true
			}
		}
	}
	return sp
}

// gap returns the spaces between two items on the same line.
func (sp *spacing) gap(a, b fitem) string {
	raw := sp.raw
	orig := sp.src[a.end:b.off]
	if b.tok >= 0 && raw[b.tok].first {
		// a token that follows a multi-line comment keeps its column
		return orig
	}
	spaced := orig != ""
	if b.tok < 0 || a.tok < 0 {
		switch {
		case b.lineCmt && spaced:
			return orig // keeps trailing comments aligned
		case b.lineCmt || spaced:
			return " "
		}
		return ""
	}
	ak, bk := raw[a.tok].Kind, raw[b.tok].Kind
	// `( x )` becomes `(x)` and `{x}` becomes `{ x }`, but brackets spanning
	// lines keep their style (`[ a` / `, b` / `]`)
	oneLine := func(i int) bool { return sp.partner[i] >= 0 && raw[sp.partner[i]].Pos.Line == raw[i].Pos.Line }
	switch {
	case ak == COMMA:
		return " "
	case bk == COMMA, (ak == LPAREN || ak == LBRACKET) && oneLine(a.tok), (bk == RPAREN || bk == RBRACKET) && oneLine(b.tok):
		return ""
	case ak == LBRACE && bk == RBRACE:
		return ""
	case ak == LBRACE && oneLine(a.tok), bk == RBRACE && oneLine(b.tok):
		return " "
	case spacedToken(ak) || spacedToken(bk) || sp.binaryOp(a.tok) || sp.binaryOp(b.tok):
		return " "
	case spaced:
		return " "
	}
	return ""
}

func spacedToken(k Kind) bool {
	switch k {
	case EQUALS, ARROW, DARROW, BAR, COLON:
		return true
	}
	return false
}

// binaryOp reports whether raw[i] is an operator used between two operands.
func (sp *spacing) binaryOp(i int) bool {
	raw := sp.raw
	if raw[i].Kind != OP || sp.row[i] || i == 0 || i+1 >= len(raw) {
		return false
	}
	return endsOperand(raw[i-1].Kind) && startsOperand(raw[i+1].Kind)
}

func endsOperand(k Kind) bool {
	switch k {
	case LIDENT, UIDENT, INT, FLOAT, STRING, INTERP, CHAR, RPAREN, RBRACKET, RBRACE, UNDERSCORE, FIELD, FIELDFN:
		return true
	}
	return false
}

func startsOperand(k Kind) bool {
	switch k {
	case LIDENT, UIDENT, INT, FLOAT, STRING, INTERP, CHAR, LPAREN, LBRACKET, LBRACE, UNDERSCORE, FIELDFN, BACKSLASH:
		return true
	}
	return false
}

// sameTokens checks that the formatted source has the same tokens, layout
// and comments as the original.
func sameTokens(file, src string, want []Token, comments []comment, out string) error {
	lx := &lexer{file: file, src: out, line: 1, col: 1, sawNL: true, sawSpc: true, keepComments: true}
	lx.run()
	bug := func(p diag.Pos, what string) error {
		return diag.Errorf(p, "internal error: formatting would change %s; please report this", what)
	}
	if len(lx.errs) > 0 {
		return bug(lx.errs[0].Pos, "the tokens")
	}
	got, err := layout(lx.toks)
	if err != nil {
		return bug(err.(diag.List)[0].Pos, "the layout")
	}
	for i := 0; i < len(want) || i < len(got); i++ {
		if i >= len(want) || i >= len(got) {
			return bug(want[min(i, len(want)-1)].Pos, "the layout")
		}
		w, g := want[i], got[i]
		if w.Kind != g.Kind || w.Text != g.Text || w.Qual != g.Qual || src[w.off:w.end] != out[g.off:g.end] {
			return bug(w.Pos, fmt.Sprintf("the layout near %s", w))
		}
	}
	if len(lx.comments) != len(comments) {
		return bug(diag.Pos{File: file, Line: 1, Col: 1}, "the comments")
	}
	for i, c := range comments {
		g := lx.comments[i]
		if strings.TrimRight(src[c.off:c.end], " \r") != strings.TrimRight(out[g.off:g.end], " \r") {
			return bug(diag.Pos{File: file, Line: c.line, Col: c.col}, "a comment")
		}
	}
	return nil
}
