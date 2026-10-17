// Package diag provides source positions and compiler diagnostics.
package diag

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Pos is a position in a source file (1-based line and column).
type Pos struct {
	File string
	Line int
	Col  int
}

func (p Pos) String() string {
	if p.File == "" {
		return fmt.Sprintf("%d:%d", p.Line, p.Col)
	}
	return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
}

// Error is a single compiler diagnostic.
type Error struct {
	Pos   Pos
	Msg   string
	Notes []string
}

func (e *Error) Error() string { return e.Pos.String() + ": " + e.Msg }

// Errorf builds a diagnostic.
func Errorf(pos Pos, format string, args ...any) *Error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// List is a collection of diagnostics that is itself an error.
type List []*Error

func (l List) Error() string {
	var sb strings.Builder
	for i, e := range l {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(e.Error())
	}
	return sb.String()
}

// Sources caches file contents to render snippets.
type Sources struct{ files map[string][]string }

func NewSources() *Sources { return &Sources{files: map[string][]string{}} }

func (s *Sources) Add(name, src string) { s.files[name] = strings.Split(src, "\n") }

func (s *Sources) line(file string, n int) (string, bool) {
	lines, ok := s.files[file]
	if !ok {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", false
		}
		lines = strings.Split(string(data), "\n")
		s.files[file] = lines
	}
	if n < 1 || n > len(lines) {
		return "", false
	}
	return lines[n-1], true
}

// DocComment returns the comment lines directly above the declaration of
// name at pos (or above its type signature), without the comment markers.
func (s *Sources) DocComment(pos Pos, name string) string {
	start := pos.Line
	// skip the signature: the closest line above at the same indentation
	// that is not a comment
	for k := pos.Line - 1; k >= 1; k-- {
		line, ok := s.line(pos.File, k)
		if !ok {
			return ""
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			break
		}
		if indentOf(line) > pos.Col-1 {
			continue // continuation of a multi-line signature
		}
		if isSignatureOf(strings.TrimPrefix(trimmed, "private "), name) {
			start = k
		}
		break
	}
	var lines []string
	for k := start - 1; k >= 1; k-- {
		line, ok := s.line(pos.File, k)
		if !ok {
			break
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "---") {
			break
		}
		text := strings.TrimPrefix(trimmed, "--")
		text = strings.TrimPrefix(text, " ")
		lines = append(lines, text)
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return strings.Join(lines, "\n")
}

// LeadingComment returns the comment block at the start of a file.
func (s *Sources) LeadingComment(file string) string {
	var lines []string
	for k := 1; ; k++ {
		line, ok := s.line(file, k)
		trimmed := strings.TrimSpace(line)
		if !ok || !strings.HasPrefix(trimmed, "--") {
			break
		}
		lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(trimmed, "--"), " "))
	}
	return strings.Join(lines, "\n")
}

func indentOf(line string) int { return len(line) - len(strings.TrimLeft(line, " ")) }

func isSignatureOf(line, name string) bool {
	for _, head := range []string{name, "(" + name + ")"} {
		if rest, ok := strings.CutPrefix(line, head); ok && strings.HasPrefix(strings.TrimLeft(rest, " "), ":") {
			return true
		}
	}
	return false
}

// Render formats an error with a source snippet.
func (s *Sources) Render(err error, color bool) string {
	var errs []*Error
	switch e := err.(type) {
	case List:
		errs = e
	case *Error:
		errs = []*Error{e}
	default:
		return "error: " + err.Error()
	}
	sort.SliceStable(errs, func(i, j int) bool {
		a, b := errs[i].Pos, errs[j].Pos
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	red, bold, blue, reset := "", "", "", ""
	if color {
		red, bold, blue, reset = "\x1b[31;1m", "\x1b[1m", "\x1b[34;1m", "\x1b[0m"
	}
	var sb strings.Builder
	for _, e := range errs {
		fmt.Fprintf(&sb, "%s%s:%s %serror:%s %s%s%s\n", bold, e.Pos, reset, red, reset, bold, e.Msg, reset)
		if line, ok := s.line(e.Pos.File, e.Pos.Line); ok && e.Pos.Line > 0 {
			num := fmt.Sprint(e.Pos.Line)
			pad := strings.Repeat(" ", len(num))
			fmt.Fprintf(&sb, "%s%s |%s\n", blue, pad, reset)
			fmt.Fprintf(&sb, "%s%s |%s %s\n", blue, num, reset, strings.ReplaceAll(line, "\t", " "))
			col := e.Pos.Col
			if col < 1 {
				col = 1
			}
			fmt.Fprintf(&sb, "%s%s |%s %s%s^%s\n", blue, pad, reset, strings.Repeat(" ", col-1), red, reset)
		}
		for _, n := range e.Notes {
			fmt.Fprintf(&sb, "  %s= note:%s %s\n", blue, reset, n)
		}
	}
	return sb.String()
}
