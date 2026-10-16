package syntax

import "github.com/verniyyy/arkadia/internal/diag"

// Layout rules
//
// A line that is indented more than the enclosing block opens a new block
// (INDENT) only when the previous line ends with a block opener: `=`, `->`,
// `where` or `with`. Otherwise it is a continuation of the previous line.
// A line at the block's column starts a new item (NEWLINE); a line left of
// it closes blocks (DEDENT). Inside brackets newlines are insignificant,
// except that an opener may still start a nested block; closing the bracket
// closes those blocks.

type ctx struct {
	bracket bool
	col     int
}

func isOpener(k Kind) bool {
	switch k {
	case EQUALS, ARROW, KwWhere, KwWith:
		return true
	}
	return false
}

func layout(raw []Token) ([]Token, error) {
	var out []Token
	var errs diag.List
	if len(raw) == 0 {
		return raw, nil
	}
	base := raw[0].Pos.Col
	if raw[0].Kind == EOF {
		base = 1
	}
	stack := []ctx{{col: base}}
	top := func() ctx { return stack[len(stack)-1] }
	enclosingBlockCol := func() int {
		for i := len(stack) - 1; i >= 0; i-- {
			if !stack[i].bracket {
				return stack[i].col
			}
		}
		return 0
	}
	mk := func(k Kind, p diag.Pos) Token { return Token{Kind: k, Pos: p} }

	for i, t := range raw {
		if t.Kind == EOF {
			for len(stack) > 1 {
				if !top().bracket {
					out = append(out, mk(DEDENT, t.Pos))
				}
				stack = stack[:len(stack)-1]
			}
			out = append(out, mk(NEWLINE, t.Pos), t)
			break
		}
		if t.first && i > 0 {
			prev := raw[i-1].Kind
			c := t.Pos.Col
			if top().bracket {
				if isOpener(prev) && c > enclosingBlockCol() {
					stack = append(stack, ctx{col: c})
					out = append(out, mk(INDENT, t.Pos))
				}
			} else if c > top().col {
				if isOpener(prev) {
					stack = append(stack, ctx{col: c})
					out = append(out, mk(INDENT, t.Pos))
				}
			} else if c == top().col {
				out = append(out, mk(NEWLINE, t.Pos))
			} else {
				for !top().bracket && c < top().col && len(stack) > 1 {
					stack = stack[:len(stack)-1]
					out = append(out, mk(DEDENT, t.Pos))
				}
				if !top().bracket {
					if c == top().col {
						out = append(out, mk(NEWLINE, t.Pos))
					} else if c < top().col {
						errs = append(errs, diag.Errorf(t.Pos, "inconsistent indentation"))
					}
				}
			}
		}
		switch t.Kind {
		case LPAREN, LBRACKET, LBRACE:
			out = append(out, t)
			stack = append(stack, ctx{bracket: true})
		case RPAREN, RBRACKET, RBRACE:
			for len(stack) > 1 && !top().bracket {
				stack = stack[:len(stack)-1]
				out = append(out, mk(DEDENT, t.Pos))
			}
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			} else {
				errs = append(errs, diag.Errorf(t.Pos, "unbalanced %s", t.Kind))
			}
			out = append(out, t)
		default:
			out = append(out, t)
		}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return out, nil
}
