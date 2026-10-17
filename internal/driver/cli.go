package driver

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/verniyyy/arkadia/internal/diag"
	"github.com/verniyyy/arkadia/internal/types"
)

// Version of the Arkadia toolchain.
const Version = "0.1.0"

const usage = `Arkadia — a strict functional language with algebraic effects

Usage:
  ark <command> [arguments]

Commands:
  new <name>         create a new project in directory <name>
  build [file.ark]   compile the project (or a single file) to a native executable
  run [file.ark]     build and run; arguments after -- are passed to the program
  check [file.ark]   type check without building
  test [file.ark]    run the project's tests (declarations ` + "`test \"name\" = ...`" + `)
  doc <Module>       show the public interface of a module
  repl               start an interactive session
  emit [file.ark]    print the generated Go code
  lsp                start the language server (stdio)
  version            print the version

Run 'ark <command> -h' for the options of a command.
`

// CLI runs the ark command and returns the exit status.
func CLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "new":
		return cmdNew(rest)
	case "build":
		return cmdBuild(rest, false)
	case "run":
		return cmdBuild(rest, true)
	case "check":
		return cmdCheck(rest)
	case "test":
		return cmdTest(rest)
	case "doc":
		return cmdDoc(rest)
	case "emit":
		return cmdEmit(rest)
	case "repl":
		return Repl(os.Stdin, os.Stdout)
	case "version", "--version", "-v":
		fmt.Println("ark", Version)
		return 0
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	}
	fmt.Fprintf(os.Stderr, "ark: unknown command %q\n\n%s", cmd, usage)
	return 2
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func fail(err error, l *Loader) int {
	if l != nil {
		fmt.Fprint(os.Stderr, l.Sources.Render(err, useColor()))
	} else {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	return 1
}

// target describes what a command operates on.
type target struct {
	file    string   // single entry file (may be empty in project mode)
	project *Project // nil in single-file mode
	paths   []string
	natives []string
}

func resolveTarget(file string, update bool) (*target, error) {
	t := &target{file: file}
	start := "."
	if file != "" {
		start = filepath.Dir(file)
	}
	if mp, ok := FindManifest(start); ok {
		p, err := ResolveProject(mp, update)
		if err != nil {
			return nil, err
		}
		t.project = p
		t.paths = p.SrcDirs
		t.natives = p.Natives
		if file == "" {
			t.file = filepath.Join(p.Manifest.Dir, p.Manifest.Entry)
		}
	} else if file == "" {
		return nil, fmt.Errorf("no %s found in this directory or its parents; pass a .ark file or run `ark new`", ManifestName)
	}
	if file != "" {
		t.paths = append([]string{filepath.Dir(file)}, t.paths...)
	}
	return t, nil
}

func splitArgs(args []string) ([]string, []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func cmdBuild(args []string, run bool) int {
	args, progArgs := splitArgs(args)
	name := "build"
	if run {
		name = "run"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	out := fs.String("o", "", "output executable")
	keep := fs.String("work", "", "directory to keep the generated Go module in")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	file := fs.Arg(0)
	if run && fs.NArg() > 1 {
		progArgs = append(fs.Args()[1:], progArgs...)
	}
	t, err := resolveTarget(file, false)
	if err != nil {
		return fail(err, nil)
	}
	l := NewLoader(t.paths)
	p, err := l.LoadFile(t.file)
	if err != nil {
		return fail(err, l)
	}
	p.Natives = t.natives
	exe := *out
	if exe == "" {
		switch {
		case run:
			exe = filepath.Join(cacheDir(), "bin", exeName(t))
		case t.project != nil && file == "":
			exe = filepath.Join(t.project.Manifest.Dir, "bin", t.project.Manifest.Name)
		default:
			exe = strings.TrimSuffix(filepath.Base(t.file), ".ark")
		}
	}
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		return fail(err, nil)
	}
	if err := Build(p, BuildOptions{Output: exe, WorkDir: *keep}); err != nil {
		return fail(err, l)
	}
	if !run {
		return 0
	}
	cmd := exec.Command(exe, progArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return fail(err, nil)
	}
	return 0
}

func exeName(t *target) string {
	abs, _ := filepath.Abs(t.file)
	base := strings.TrimSuffix(filepath.Base(abs), ".ark")
	return fmt.Sprintf("%s-%x", base, hashString(abs))
}

func hashString(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func cmdCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	t, err := resolveTarget(fs.Arg(0), false)
	if err != nil {
		return fail(err, nil)
	}
	l := NewLoader(t.paths)
	p, err := l.LoadFile(t.file)
	if err != nil {
		return fail(err, l)
	}
	if err := types.CheckMain(p.Global, p.Main); err != nil {
		return fail(err, l)
	}
	fmt.Fprintf(os.Stderr, "ok: %d modules checked\n", len(p.Modules))
	return 0
}

func cmdEmit(args []string) int {
	t, err := resolveTarget(firstArg(args), false)
	if err != nil {
		return fail(err, nil)
	}
	l := NewLoader(t.paths)
	p, err := l.LoadFile(t.file)
	if err != nil {
		return fail(err, l)
	}
	src, err := GenerateGo(p, false)
	if err != nil {
		return fail(err, l)
	}
	os.Stdout.Write(src)
	return 0
}

func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func cmdTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	file := fs.Arg(0)
	t, err := resolveTarget(file, false)
	if err != nil {
		return fail(err, nil)
	}
	var mods []string
	paths := t.paths
	if file != "" {
		l := NewLoader(paths)
		p, err := l.LoadFile(file)
		if err != nil {
			return fail(err, l)
		}
		return runTests(l, p, t)
	}
	m := t.project.Manifest
	testDir := filepath.Join(m.Dir, "tests")
	paths = append(paths, testDir)
	mods = append(mods, modulesIn(filepath.Join(m.Dir, "src"))...)
	mods = append(mods, modulesIn(testDir)...)
	l := NewLoader(paths)
	p, err := l.LoadModules(mods)
	if err != nil {
		return fail(err, l)
	}
	return runTests(l, p, t)
}

func runTests(l *Loader, p *Program, t *target) int {
	// keep only tests from the user's own modules
	own := map[string]bool{}
	for _, m := range p.Modules {
		if !strings.HasPrefix(m.File, "<std>") {
			own[m.Name] = true
		}
	}
	var kept []*types.TestInfo
	for _, ti := range p.Global.Tests {
		if own[ti.Module] {
			kept = append(kept, ti)
		}
	}
	p.Global.Tests = kept
	if len(kept) == 0 {
		fmt.Println("no tests found")
		return 0
	}
	p.Natives = t.natives
	exe := filepath.Join(cacheDir(), "bin", "test-"+exeName(t))
	os.MkdirAll(filepath.Dir(exe), 0o755)
	if err := Build(p, BuildOptions{Output: exe, Tests: true}); err != nil {
		return fail(err, l)
	}
	cmd := exec.Command(exe)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if !useColor() {
		cmd.Env = append(os.Environ(), "NO_COLOR=1")
	}
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		return fail(err, nil)
	}
	return 0
}

func cmdDoc(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: ark doc <Module>")
		return 2
	}
	var paths []string
	if mp, ok := FindManifest("."); ok {
		if p, err := ResolveProject(mp, false); err == nil {
			paths = p.SrcDirs
		}
	}
	l := NewLoader(paths)
	p, err := l.LoadModules([]string{args[0]})
	if err != nil {
		return fail(err, l)
	}
	file := ""
	for _, m := range p.Modules {
		if m.Name == args[0] {
			file = m.File
		}
	}
	printDoc(os.Stdout, p.Global.Modules[args[0]], p.Sources, file)
	return 0
}

// printDocText writes documentation lines indented below an entry.
func printDocText(w io.Writer, doc string) {
	if doc == "" {
		return
	}
	for _, line := range strings.Split(doc, "\n") {
		fmt.Fprintln(w, strings.TrimRight("    "+line, " "))
	}
}

func printDoc(w io.Writer, ex *types.Exports, src *diag.Sources, file string) {
	fmt.Fprintf(w, "module %s\n", ex.Module)
	if doc := src.LeadingComment(file); doc != "" {
		fmt.Fprintln(w)
		printDocText(w, doc)
	}
	var tnames []string
	for n := range ex.Types {
		tnames = append(tnames, n)
	}
	sort.Strings(tnames)
	for _, n := range tnames {
		switch t := ex.Types[n].(type) {
		case *types.TypeInfo:
			if !strings.HasPrefix(t.Name, ex.Module+".") {
				continue
			}
			var ctors []string
			for _, c := range t.Ctors {
				ctors = append(ctors, types.CtorString(c))
			}
			fmt.Fprintf(w, "\ntype %s", strings.Join(append([]string{n}, t.Params...), " "))
			if len(ctors) > 0 {
				fmt.Fprintf(w, " = %s", strings.Join(ctors, " | "))
			}
			fmt.Fprintln(w)
			printDocText(w, src.DocComment(t.Pos, n))
		case *types.ClassInfo:
			if t.Module != ex.Module {
				continue
			}
			fmt.Fprintf(w, "\nclass %s\n", n)
			printDocText(w, src.DocComment(t.Pos, n))
			for _, m := range t.Methods {
				fmt.Fprintf(w, "  %s : %s\n", m.Name, types.SchemeString(m.Scheme))
			}
		case *types.EffectInfo:
			if t.Module != ex.Module {
				continue
			}
			fmt.Fprintf(w, "\neffect %s\n", n)
			printDocText(w, src.DocComment(t.Pos, n))
			for _, o := range t.Ops {
				fmt.Fprintf(w, "  %s : %s\n", o.Name, types.SchemeString(o.Scheme))
			}
		}
	}
	var vnames []string
	for n, v := range ex.Values {
		if v.Method == nil && v.Op == nil && strings.HasPrefix(v.Sym, ex.Module+".") {
			vnames = append(vnames, n)
		}
	}
	sort.Strings(vnames)
	fmt.Fprintln(w)
	for _, n := range vnames {
		v := ex.Values[n]
		if v.Scheme != nil {
			fmt.Fprintf(w, "%s : %s\n", n, types.SchemeString(v.Scheme))
			printDocText(w, src.DocComment(v.Pos, n))
		}
	}
}

func cmdNew(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: ark new <name>")
		return 2
	}
	dir := args[0]
	name := filepath.Base(dir)
	if _, err := os.Stat(dir); err == nil {
		return fail(fmt.Errorf("%s already exists", dir), nil)
	}
	files := map[string]string{
		ManifestName: fmt.Sprintf(`[package]
name = %q
version = "0.1.0"
description = ""

[dependencies]
# mylib = { path = "../mylib" }
# json = { git = "https://example.com/arkadia-json.git", tag = "v0.1.0" }
`, name),
		"src/Main.ark": `module Main

import Greeting

main () =
  println (greet "Arkadia")
`,
		"src/Greeting.ark": `module Greeting

greet : String -> String
greet name = "Hello, ${name}!"
`,
		"tests/GreetingTest.ark": `module GreetingTest

import Greeting

test "greet says hello" =
  assertEq (greet "world") "Hello, world!"
`,
		".gitignore": "/bin/\n",
	}
	for p, content := range files {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fail(err, nil)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return fail(err, nil)
		}
	}
	fmt.Printf("created project %s\n\n  cd %s\n  ark run\n  ark test\n", name, dir)
	return 0
}
