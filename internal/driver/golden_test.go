package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGolden compiles every program in tests/run and compares its output
// with the corresponding .out file. Programs in tests/errors must fail to
// compile with a message containing the text in the .err file.
func TestGolden(t *testing.T) {
	if testing.Short() {
		t.Skip("golden tests build native executables")
	}
	files, _ := filepath.Glob("../../tests/run/*.ark")
	examples, _ := filepath.Glob("../../examples/*.ark")
	files = append(files, examples...)
	bin := t.TempDir()
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".ark")
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(strings.TrimSuffix(f, ".ark") + ".out")
			if err != nil {
				t.Fatal(err)
			}
			l := NewLoader([]string{filepath.Dir(f)})
			p, err := l.LoadFile(f)
			if err != nil {
				t.Fatal(l.Sources.Render(err, false))
			}
			exe := filepath.Join(bin, name)
			if err := Build(p, BuildOptions{Output: exe, WorkDir: filepath.Join(bin, name+"-go")}); err != nil {
				t.Fatal(l.Sources.Render(err, false))
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

func TestCompileErrors(t *testing.T) {
	files, _ := filepath.Glob("../../tests/errors/*.ark")
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".ark")
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(strings.TrimSuffix(f, ".ark") + ".err")
			if err != nil {
				t.Fatal(err)
			}
			l := NewLoader([]string{filepath.Dir(f)})
			p, err := l.LoadFile(f)
			if err == nil {
				_, err = GenerateGo(p, false)
			}
			if err == nil {
				t.Fatal("expected a compile error")
			}
			msg := l.Sources.Render(err, false)
			for _, line := range strings.Split(strings.TrimSpace(string(want)), "\n") {
				if !strings.Contains(msg, line) {
					t.Errorf("error output does not contain %q:\n%s", line, msg)
				}
			}
		})
	}
}

// TestUnitTests builds the Arkadia unit tests with the test runner.
func TestUnitTests(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a native executable")
	}
	f := "../../tests/unit/StdTests.ark"
	l := NewLoader([]string{filepath.Dir(f)})
	p, err := l.LoadModules([]string{"StdTests"})
	if err != nil {
		t.Fatal(l.Sources.Render(err, false))
	}
	exe := filepath.Join(t.TempDir(), "unit")
	if err := Build(p, BuildOptions{Output: exe, Tests: true}); err != nil {
		t.Fatal(l.Sources.Render(err, false))
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("unit tests failed:\n%s", out)
	}
	if !strings.Contains(string(out), "all ") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
