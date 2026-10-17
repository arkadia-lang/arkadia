package driver

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/verniyyy/arkadia/bootstrap/internal/types"
)

const replHelp = `Commands:
  <expression>      evaluate and print (Show) or run (Unit with IO)
  <definition>      add a definition (functions, types, classes, effects, imports)
  :t <expression>   show the type of an expression
  :defs             list the current definitions
  :reset            forget all definitions
  :load <file>      add the definitions of a file
  :q                quit
Lines ending with =, ->, with or where continue on the next lines until an
empty line.`

type repl struct {
	imports []string
	defs    []string
	dir     string
	paths   []string
	out     io.Writer
	counter int
}

// Repl runs the interactive read-eval-print loop.
func Repl(in io.Reader, out io.Writer) int {
	dir := filepath.Join(cacheDir(), "repl")
	os.MkdirAll(dir, 0o755)
	r := &repl{dir: dir, out: out}
	if mp, ok := FindManifest("."); ok {
		if p, err := ResolveProject(mp, false); err == nil {
			r.paths = p.SrcDirs
		}
	}
	fmt.Fprintf(out, "Arkadia %s REPL — :help for commands\n", Version)
	sc := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, "ark> ")
		if !sc.Scan() {
			fmt.Fprintln(out)
			return 0
		}
		input := sc.Text()
		for continues(input) {
			fmt.Fprint(out, "...  ")
			if !sc.Scan() {
				break
			}
			line := sc.Text()
			if strings.TrimSpace(line) == "" {
				break
			}
			input += "\n" + line
		}
		if strings.TrimSpace(input) == "" {
			continue
		}
		if r.command(strings.TrimSpace(input)) {
			return 0
		}
	}
}

func continues(s string) bool {
	lines := strings.Split(s, "\n")
	switch strings.Fields(lines[0])[0] {
	case "type", "class", "instance", "effect":
		// block declarations end with an empty line
		return strings.TrimSpace(lines[len(lines)-1]) != "" || len(lines) == 1
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if len(lines) > 1 && last != "" {
		return true
	}
	for _, suf := range []string{"=", "->", "with", "where"} {
		if strings.HasSuffix(last, suf) {
			return true
		}
	}
	return false
}

func (r *repl) command(input string) (quit bool) {
	switch {
	case input == ":q" || input == ":quit":
		return true
	case input == ":help" || input == ":h":
		fmt.Fprintln(r.out, replHelp)
	case input == ":reset":
		r.defs, r.imports = nil, nil
	case input == ":defs":
		for _, d := range append(append([]string{}, r.imports...), r.defs...) {
			fmt.Fprintln(r.out, d)
		}
	case strings.HasPrefix(input, ":load "):
		data, err := os.ReadFile(strings.TrimSpace(input[6:]))
		if err != nil {
			fmt.Fprintln(r.out, "error:", err)
			return
		}
		var body []string
		for _, l := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(l, "module ") {
				continue
			}
			if strings.HasPrefix(l, "import ") {
				r.imports = append(r.imports, l)
				continue
			}
			body = append(body, l)
		}
		r.addDef(strings.Join(body, "\n"))
	case strings.HasPrefix(input, ":t "):
		r.showType(strings.TrimSpace(input[3:]))
	case strings.HasPrefix(input, ":"):
		fmt.Fprintln(r.out, "unknown command; try :help")
	case strings.HasPrefix(input, "import "):
		r.imports = append(r.imports, input)
		if _, err := r.check(nil); err != nil {
			r.imports = r.imports[:len(r.imports)-1]
		}
	case isDefinition(input):
		r.addDef(input)
	default:
		r.eval(input)
	}
	return false
}

// isDefinition guesses whether the input is a declaration.
func isDefinition(s string) bool {
	first := strings.Fields(s)[0]
	switch first {
	case "type", "alias", "class", "instance", "effect", "foreign", "infixl", "infixr", "infix", "private":
		return true
	}
	// `name args = ...` or `name : type` at top level (not `==`)
	line := strings.SplitN(s, "\n", 2)[0]
	depth := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '"':
			return false
		case '=':
			if depth == 0 && (i+1 >= len(line) || !strings.ContainsRune("=>", rune(line[i+1]))) && (i == 0 || !strings.ContainsRune("=!<>", rune(line[i-1]))) {
				return true
			}
		case ':':
			if depth == 0 && (i+1 >= len(line) || line[i+1] != ':') && (i == 0 || line[i-1] != ':') {
				return true
			}
		}
	}
	return false
}

func (r *repl) source(extra []string) string {
	var sb strings.Builder
	sb.WriteString("module Main\n\n")
	for _, i := range r.imports {
		sb.WriteString(i + "\n")
	}
	sb.WriteString("\n")
	for _, d := range append(append([]string{}, r.defs...), extra...) {
		sb.WriteString(d + "\n\n")
	}
	return sb.String()
}

func (r *repl) load(extra []string) (*Loader, *Program, error) {
	path := filepath.Join(r.dir, "Main.ark")
	if err := os.WriteFile(path, []byte(r.source(extra)), 0o644); err != nil {
		return nil, nil, err
	}
	l := NewLoader(append([]string{r.dir}, r.paths...))
	p, err := l.LoadFile(path)
	return l, p, err
}

func (r *repl) check(extra []string) (*Program, error) {
	l, p, err := r.load(extra)
	if err != nil {
		r.report(l, err)
	}
	return p, err
}

func (r *repl) report(l *Loader, err error) {
	if l == nil {
		fmt.Fprintln(r.out, "error:", err)
		return
	}
	fmt.Fprint(r.out, l.Sources.Render(err, useColor()))
}

func (r *repl) addDef(def string) {
	if _, err := r.check([]string{def}); err != nil {
		return
	}
	r.defs = append(r.defs, def)
}

func indentBody(expr string) string {
	return "  " + strings.ReplaceAll(expr, "\n", "\n  ")
}

func (r *repl) showType(expr string) {
	r.counter++
	name := fmt.Sprintf("it%d", r.counter)
	p, err := r.check([]string{name + " () =\n" + indentBody(expr)})
	if err != nil {
		return
	}
	v := p.Global.Modules["Main"].Values[name]
	if f, ok := v.Scheme.Type.(*types.TFun); ok {
		sc := *v.Scheme
		sc.Type = f.Res
		fmt.Fprintf(r.out, "%s : %s", expr, types.SchemeString(&sc))
		if labels := types.EffectLabels(f.Eff); len(labels) > 0 {
			fmt.Fprintf(r.out, "  (performs %s)", strings.Join(labels, ", "))
		}
		fmt.Fprintln(r.out)
	}
}

func (r *repl) eval(expr string) {
	// first as an action of type Unit, then printed with Show
	candidates := []string{
		"main () =\n" + indentBody(expr),
		"main () =\n  printLn (\n" + indentBody(indentBody(expr)) + ")",
	}
	var errs []error
	var loaders []*Loader
	for _, main := range candidates {
		l, p, err := r.load([]string{main})
		if err == nil {
			err = types.CheckMain(p.Global, p.Main)
		}
		if err != nil {
			errs, loaders = append(errs, err), append(loaders, l)
			continue
		}
		exe := filepath.Join(r.dir, "repl-bin")
		if err := Build(p, BuildOptions{Output: exe, WorkDir: filepath.Join(r.dir, "go")}); err != nil {
			r.report(l, err)
			return
		}
		cmd := exec.Command(exe)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, r.out, r.out
		cmd.Run()
		return
	}
	// prefer the error of the printing variant unless it only complains
	// about a missing Show instance
	pick := 1
	if strings.Contains(errs[1].Error(), "no instance for Show") || strings.Contains(errs[1].Error(), "type mismatch") && !strings.Contains(errs[0].Error(), "main") {
		pick = 0
	}
	r.report(loaders[pick], errs[pick])
}
