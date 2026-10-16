package driver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CLI implements the `ark` command (minimal version).
func CLI(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ark run|build|emit FILE")
		return 2
	}
	l := NewLoader([]string{filepath.Dir(args[1])})
	p, err := l.LoadFile(args[1])
	if err != nil {
		fmt.Fprint(os.Stderr, l.Sources.Render(err, true))
		return 1
	}
	switch args[0] {
	case "emit":
		src, err := GenerateGo(p, false)
		if err != nil {
			fmt.Fprint(os.Stderr, l.Sources.Render(err, true))
			return 1
		}
		os.Stdout.Write(src)
	case "build", "run":
		out := strings.TrimSuffix(filepath.Base(args[1]), ".ark")
		if args[0] == "run" {
			out = filepath.Join(cacheDir(), "bin", out)
			os.MkdirAll(filepath.Dir(out), 0o755)
		}
		if err := Build(p, BuildOptions{Output: out}); err != nil {
			fmt.Fprint(os.Stderr, l.Sources.Render(err, true))
			return 1
		}
		if args[0] == "run" {
			cmd := exec.Command(out, args[2:]...)
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					return ee.ExitCode()
				}
				return 1
			}
		}
	}
	return 0
}
