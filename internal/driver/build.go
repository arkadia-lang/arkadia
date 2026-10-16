package driver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/verniyyy/arkadia/internal/codegen"
	"github.com/verniyyy/arkadia/internal/core"
	"github.com/verniyyy/arkadia/internal/types"
	"github.com/verniyyy/arkadia/runtime"
)

// BuildOptions controls compilation to a native executable.
type BuildOptions struct {
	Output  string // path of the executable
	WorkDir string // directory for generated Go sources (default: temp)
	Tests   bool   // build the test runner
	KeepGo  bool   // keep generated sources
	Natives []string
	Verbose bool
}

// GenerateGo lowers a checked program to Go source.
func GenerateGo(p *Program, tests bool) ([]byte, error) {
	mainMod := p.Main
	if !tests {
		if err := types.CheckMain(p.Global, p.Main); err != nil {
			return nil, err
		}
	} else {
		mainMod = ""
	}
	prog := core.Lower(p.Global, p.Modules, mainMod)
	return codegen.Generate(prog, codegen.Options{Tests: tests})
}

// cacheDir returns the directory for build artifacts.
func cacheDir() string {
	if d := os.Getenv("ARKADIA_CACHE"); d != "" {
		return d
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "arkadia")
}

// Build compiles a checked program to a native executable using the Go
// toolchain.
func Build(p *Program, opts BuildOptions) error {
	src, err := GenerateGo(p, opts.Tests)
	if err != nil {
		return err
	}
	work := opts.WorkDir
	if work == "" {
		sum := sha256.Sum256([]byte(p.Main + "\x00" + opts.Output))
		work = filepath.Join(cacheDir(), "build", hex.EncodeToString(sum[:8]))
	}
	if err := writeGoModule(work, src, append(p.Natives, opts.Natives...)); err != nil {
		return err
	}
	out, err := filepath.Abs(opts.Output)
	if err != nil {
		return err
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("the Go toolchain is required to produce native executables (https://go.dev/dl/): %v", err)
	}
	cmd := exec.Command(goBin, "build", "-trimpath", "-ldflags=-s -w", "-o", out, ".")
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "CGO_ENABLED=0", "GOTOOLCHAIN=local")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed (generated code in %s):\n%s", work, stderr.String())
	}
	if !opts.KeepGo && opts.WorkDir == "" {
		// keep the directory so that subsequent builds reuse the Go cache,
		// but remove nothing: sources are tiny.
	}
	return nil
}

func writeGoModule(dir string, mainSrc []byte, natives []string) error {
	if err := os.MkdirAll(filepath.Join(dir, "rt"), 0o755); err != nil {
		return err
	}
	// remove stale native files
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "native_") {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	files := map[string][]byte{
		"go.mod":  []byte("module arkprog\n\ngo 1.22\n"),
		"main.go": mainSrc,
	}
	err := fs.WalkDir(runtime.FS, "rt", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(runtime.FS, path)
		if err != nil {
			return err
		}
		files[path] = data
		return nil
	})
	if err != nil {
		return err
	}
	for i, n := range natives {
		data, err := os.ReadFile(n)
		if err != nil {
			return err
		}
		files[fmt.Sprintf("native_%d_%s", i, filepath.Base(n))] = data
	}
	for name, data := range files {
		p := filepath.Join(dir, name)
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, data) {
			continue
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
