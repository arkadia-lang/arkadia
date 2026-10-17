package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/verniyyy/arkadia/bootstrap/internal/syntax"
)

// TestSelfHost builds the self-hosted compiler (selfhost/) with this
// implementation and checks that it is a faithful port: it generates the
// same Go code for the example and test programs and for its own sources,
// reports the same errors, formats sources the same way, and the compiler it
// builds from its own sources generates the same code again (the bootstrap
// reaches a fixpoint).
func TestSelfHost(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the self-hosted compiler")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "selfhost")
	proj, err := ResolveProject(filepath.Join(dir, ManifestName), false)
	if err != nil {
		t.Fatal(err)
	}
	l := NewLoader(proj.SrcDirs)
	prog, err := l.LoadFile(filepath.Join(proj.Manifest.Dir, proj.Manifest.Entry))
	if err != nil {
		t.Fatal(l.Sources.Render(err, false))
	}
	prog.Natives = proj.Natives
	ownCode, err := GenerateGo(prog, false)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	stage1 := filepath.Join(bin, "arkc1")
	if err := Build(prog, BuildOptions{Output: stage1, WorkDir: filepath.Join(bin, "arkc1-go")}); err != nil {
		t.Fatal(l.Sources.Render(err, false))
	}

	arkcIn := func(stdin []byte, exe, cwd string, args ...string) ([]byte, []byte, error) {
		cmd := exec.Command(exe, args...)
		cmd.Dir = cwd
		cmd.Stdin = bytes.NewReader(stdin)
		cmd.Env = append(os.Environ(), "ARKADIA_ROOT="+root, "ARKADIA_CACHE="+filepath.Join(bin, "cache"), "NO_COLOR=1")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}
	arkc := func(exe, cwd string, args ...string) ([]byte, []byte, error) {
		return arkcIn(nil, exe, cwd, args...)
	}

	t.Run("programs", func(t *testing.T) {
		files, _ := filepath.Glob("../../../tests/run/*.ark")
		examples, _ := filepath.Glob("../../../examples/*.ark")
		for _, f := range append(files, examples...) {
			l := NewLoader([]string{filepath.Dir(f)})
			p, err := l.LoadFile(f)
			if err != nil {
				t.Fatal(l.Sources.Render(err, false))
			}
			want, err := GenerateGo(p, false)
			if err != nil {
				t.Fatal(err)
			}
			got, stderr, err := arkc(stage1, ".", "emit", f)
			if err != nil {
				t.Fatalf("arkc emit %s: %v\n%s", f, err, stderr)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("arkc generates different code for %s", f)
			}
		}
	})

	t.Run("errors", func(t *testing.T) {
		files, _ := filepath.Glob("../../../tests/errors/*.ark")
		for _, f := range files {
			want, err := os.ReadFile(strings.TrimSuffix(f, ".ark") + ".err")
			if err != nil {
				t.Fatal(err)
			}
			_, stderr, err := arkc(stage1, ".", "check", f)
			if err == nil {
				t.Errorf("arkc check %s: expected a compile error", f)
				continue
			}
			for _, line := range strings.Split(strings.TrimSpace(string(want)), "\n") {
				if !bytes.Contains(stderr, []byte(line)) {
					t.Errorf("arkc error output for %s does not contain %q:\n%s", f, line, stderr)
				}
			}
		}
	})

	t.Run("fmt", func(t *testing.T) {
		ins, _ := filepath.Glob("../../../tests/fmt/*.in")
		for _, in := range ins {
			src, _ := os.ReadFile(in)
			want, err := os.ReadFile(strings.TrimSuffix(in, ".in") + ".out")
			if err != nil {
				t.Fatal(err)
			}
			got, stderr, err := arkcIn(src, stage1, ".", "fmt", "-")
			if err != nil {
				t.Fatalf("arkc fmt %s: %v\n%s", in, err, stderr)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("arkc fmt %s:\n%s\nwant\n%s", in, got, want)
			}
		}
		// every source of the repository, as it is and with its indentation
		// doubled, formats as with this implementation
		var files []string
		for _, dir := range []string{"lib", "examples", "selfhost/src", "tests/run", "tests/unit"} {
			filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
				if err == nil && strings.HasSuffix(path, ".ark") {
					files = append(files, path)
				}
				return nil
			})
		}
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, src := range []string{string(data), doubleIndent(string(data))} {
				want, err := syntax.Format(f, src)
				if err != nil {
					t.Fatal(err)
				}
				got, stderr, err := arkcIn([]byte(src), stage1, ".", "fmt", "-")
				if err != nil {
					t.Fatalf("arkc fmt %s: %v\n%s", f, err, stderr)
				}
				if string(got) != want {
					t.Errorf("arkc formats %s differently", f)
				}
			}
		}
	})

	t.Run("bootstrap", func(t *testing.T) {
		got, stderr, err := arkc(stage1, dir, "emit")
		if err != nil {
			t.Fatalf("arkc emit: %v\n%s", err, stderr)
		}
		if !bytes.Equal(got, ownCode) {
			t.Fatal("arkc generates different code for its own sources")
		}
		stage2 := filepath.Join(bin, "arkc2")
		if _, stderr, err := arkc(stage1, dir, "build", "-o", stage2); err != nil {
			t.Fatalf("arkc build: %v\n%s", err, stderr)
		}
		got, stderr, err = arkc(stage2, dir, "emit")
		if err != nil {
			t.Fatalf("stage 2 emit: %v\n%s", err, stderr)
		}
		if !bytes.Equal(got, ownCode) {
			t.Fatal("the compiler built by arkc generates different code for its own sources")
		}
	})
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
