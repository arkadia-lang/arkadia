package driver

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/arkadia-lang/arkadia/bootstrap/internal/diag"
	"github.com/arkadia-lang/arkadia/bootstrap/internal/syntax"
)

// cmdFmt formats .ark files in place, or reports the files that are not
// formatted (-check). A "-" argument formats stdin to stdout.
func cmdFmt(args []string) int {
	fset := flag.NewFlagSet("fmt", flag.ContinueOnError)
	check := fset.Bool("check", false, "list the files that are not formatted instead of rewriting them; exit status 1 if there are any")
	fset.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: ark fmt [-check] [file.ark | dir | -]...")
		fset.PrintDefaults()
	}
	if err := fset.Parse(args); err != nil {
		return 2
	}
	targets := fset.Args()
	if len(targets) == 0 {
		targets = []string{"."}
	}
	if len(targets) == 1 && targets[0] == "-" {
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fail(err, nil)
		}
		out, err := syntax.Format("<stdin>", string(src))
		if err != nil {
			return fmtFail(err, "<stdin>", string(src))
		}
		os.Stdout.WriteString(out)
		return 0
	}
	var files []string
	for _, t := range targets {
		fi, err := os.Stat(t)
		if err != nil {
			return fail(err, nil)
		}
		if !fi.IsDir() {
			files = append(files, t)
			continue
		}
		err = filepath.WalkDir(t, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && path != t && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(path, ".ark") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return fail(err, nil)
		}
	}
	status := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fail(err, nil)
		}
		src := string(data)
		out, err := syntax.Format(f, src)
		if err != nil {
			fmtFail(err, f, src)
			status = 1
			continue
		}
		if out == src {
			continue
		}
		if *check {
			fmt.Println(f)
			status = 1
			continue
		}
		if err := os.WriteFile(f, []byte(out), 0o644); err != nil {
			return fail(err, nil)
		}
		fmt.Println(f)
	}
	return status
}

func fmtFail(err error, file, src string) int {
	s := diag.NewSources()
	s.Add(file, src)
	fmt.Fprint(os.Stderr, s.Render(err, useColor()))
	return 1
}
