package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSelfHost builds the self-hosted compiler (selfhost/) with this
// implementation and checks that it is a faithful port: it generates the
// same Go code for the example and test programs and for its own sources,
// reports the same errors, and the compiler it builds from its own sources
// generates the same code again (the bootstrap reaches a fixpoint).
func TestSelfHost(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the self-hosted compiler")
	}
	root, err := filepath.Abs("../..")
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

	arkc := func(exe, cwd string, args ...string) ([]byte, []byte, error) {
		cmd := exec.Command(exe, args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "ARKADIA_ROOT="+root, "ARKADIA_CACHE="+filepath.Join(bin, "cache"), "NO_COLOR=1")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
	}

	t.Run("programs", func(t *testing.T) {
		files, _ := filepath.Glob("../../tests/run/*.ark")
		examples, _ := filepath.Glob("../../examples/*.ark")
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
		files, _ := filepath.Glob("../../tests/errors/*.ark")
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
