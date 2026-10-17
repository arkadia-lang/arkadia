// Package tests runs the test suite of the repository against the Arkadia
// compiler (compiler/).
//
// The compiler is bootstrapped as in `just build`: the bootstrap compiler
// (bootstrap/) builds it once (stage 1), and stage 1 builds it again from
// its own sources (stage 2). The tests use stage 2, the compiler built by
// itself.
package tests

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var (
	root   string // the repository
	work   string // temporary files of the tests
	stage1 string // the compiler built by the bootstrap compiler
	ark    string // the compiler built by stage 1
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		fmt.Println("skipping: the tests build the compiler and native executables")
		return 0
	}
	var err error
	if root, err = filepath.Abs(".."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if work, err = os.MkdirTemp("", "arkadia-tests-"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(work)
	if err := buildCompiler(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

// buildCompiler bootstraps the compiler into stage1 and ark.
func buildCompiler() error {
	ark0 := filepath.Join(work, "ark0")
	cmd := exec.Command("go", "build", "-o", ark0, "./bootstrap/cmd/ark")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("building the bootstrap compiler: %v\n%s", err, out)
	}
	dir := filepath.Join(root, "compiler")
	stage1 = filepath.Join(work, "ark1")
	if _, stderr, err := arkWith(ark0, nil, dir, "build", "-o", stage1); err != nil {
		return fmt.Errorf("building the compiler with the bootstrap compiler: %v\n%s", err, stderr)
	}
	ark = filepath.Join(work, "ark2")
	if _, stderr, err := arkWith(stage1, nil, dir, "build", "-o", ark); err != nil {
		return fmt.Errorf("building the compiler with itself: %v\n%s", err, stderr)
	}
	return nil
}

// arkWith runs a compiler executable in directory cwd and returns its
// standard output and standard error.
func arkWith(exe string, stdin []byte, cwd string, args ...string) ([]byte, []byte, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = cwd
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Env = append(os.Environ(), "ARKADIA_ROOT="+root, "ARKADIA_CACHE="+filepath.Join(work, "cache"), "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func runArk(stdin []byte, args ...string) ([]byte, []byte, error) {
	return arkWith(ark, stdin, root, args...)
}

func glob(t *testing.T, patterns ...string) []string {
	var files []string
	for _, p := range patterns {
		fs, err := filepath.Glob(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, fs...)
	}
	if len(files) == 0 {
		t.Fatalf("no files match %v", patterns)
	}
	return files
}

func stem(f string) string { return strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)) }

// TestBootstrap checks that the bootstrap reaches a fixpoint: the compiler
// built by itself generates the same code for its own sources as the
// compiler built by the bootstrap compiler.
func TestBootstrap(t *testing.T) {
	dir := filepath.Join(root, "compiler")
	want, stderr, err := arkWith(stage1, nil, dir, "emit")
	if err != nil {
		t.Fatalf("stage 1 emit: %v\n%s", err, stderr)
	}
	got, stderr, err := arkWith(ark, nil, dir, "emit")
	if err != nil {
		t.Fatalf("stage 2 emit: %v\n%s", err, stderr)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("the compiler built by itself generates different code for its own sources")
	}
}

// TestPrograms builds every program in tests/run and examples and compares
// its output with the corresponding .out file (NAME.in, if present, is its
// standard input).
func TestPrograms(t *testing.T) {
	bin := t.TempDir()
	for _, f := range glob(t, "tests/run/*.ark", "examples/*.ark") {
		name := filepath.Base(filepath.Dir(f)) + "/" + stem(f)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(strings.TrimSuffix(f, ".ark") + ".out")
			if err != nil {
				t.Fatal(err)
			}
			exe := filepath.Join(bin, strings.ReplaceAll(name, "/", "-"))
			if _, stderr, err := runArk(nil, "build", "-o", exe, f); err != nil {
				t.Fatalf("ark build: %v\n%s", err, stderr)
			}
			cmd := exec.Command(exe)
			if in, err := os.ReadFile(strings.TrimSuffix(f, ".ark") + ".in"); err == nil {
				cmd.Stdin = bytes.NewReader(in)
			}
			got, _ := cmd.CombinedOutput()
			if !bytes.Equal(got, want) {
				t.Errorf("output mismatch\n--- got\n%s\n--- want\n%s", got, want)
			}
		})
	}
}

// TestCompileErrors checks that every program in tests/errors fails to
// compile with a message containing the lines of the corresponding .err
// file.
func TestCompileErrors(t *testing.T) {
	for _, f := range glob(t, "tests/errors/*.ark") {
		t.Run(stem(f), func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(strings.TrimSuffix(f, ".ark") + ".err")
			if err != nil {
				t.Fatal(err)
			}
			_, stderr, err := runArk(nil, "check", f)
			if err == nil {
				t.Fatal("expected a compile error")
			}
			for _, line := range strings.Split(strings.TrimSpace(string(want)), "\n") {
				if !bytes.Contains(stderr, []byte(line)) {
					t.Errorf("error output does not contain %q:\n%s", line, stderr)
				}
			}
		})
	}
}

// TestUnitTests runs the Arkadia unit tests (the test declarations of every
// module in tests/unit).
func TestUnitTests(t *testing.T) {
	for _, f := range glob(t, "tests/unit/*.ark") {
		t.Run(stem(f), func(t *testing.T) {
			t.Parallel()
			out, stderr, err := runArk(nil, "test", f)
			if err != nil {
				t.Fatalf("ark test: %v\n%s%s", err, out, stderr)
			}
			if !bytes.Contains(out, []byte("all ")) {
				t.Fatalf("unexpected output:\n%s%s", out, stderr)
			}
		})
	}
}

func format(t *testing.T, src []byte) string {
	t.Helper()
	got, stderr, err := runArk(src, "fmt", "-")
	if err != nil {
		t.Fatalf("ark fmt: %v\n%s", err, stderr)
	}
	return string(got)
}

// TestFormat formats every tests/fmt/NAME.in and compares the result with
// NAME.out.
func TestFormat(t *testing.T) {
	for _, in := range glob(t, "tests/fmt/*.in") {
		t.Run(stem(in), func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(in, ".in") + ".out")
			if err != nil {
				t.Fatal(err)
			}
			got := format(t, src)
			if got != string(want) {
				t.Errorf("got\n%s\nwant\n%s", got, want)
			}
			if again := format(t, []byte(got)); again != got {
				t.Errorf("not idempotent:\n%s", again)
			}
		})
	}
}

// TestFormatSources formats every Arkadia source of the repository: the
// result must be stable, and the same after the indentation of the source
// is doubled.
func TestFormatSources(t *testing.T) {
	var files []string
	for _, dir := range []string{"lib", "examples", "compiler/src", "tests/run", "tests/unit"} {
		filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err == nil && strings.HasSuffix(path, ".ark") {
				files = append(files, path)
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Fatal("no sources found")
	}
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			want := format(t, data)
			if again := format(t, []byte(want)); again != want {
				t.Error("formatting is not idempotent")
			}
			if got := format(t, []byte(doubleIndent(string(data)))); got != want {
				t.Error("the source with doubled indentation formats differently")
			}
		})
	}
}

// doubleIndent doubles the indentation of the lines of a source outside
// triple-quoted strings, which does not change its meaning.
func doubleIndent(src string) string {
	lines := strings.Split(src, "\n")
	raw := false
	for i, l := range lines {
		if !raw {
			n := len(l) - len(strings.TrimLeft(l, " "))
			lines[i] = strings.Repeat(" ", n) + l
		}
		if strings.Count(l, `"""`)%2 == 1 {
			raw = !raw
		}
	}
	return strings.Join(lines, "\n")
}
