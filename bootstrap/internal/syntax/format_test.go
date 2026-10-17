package syntax

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFormat formats every tests/fmt/NAME.in and compares the result with
// NAME.out (the self-hosted compiler is checked against the same files).
func TestFormat(t *testing.T) {
	ins, _ := filepath.Glob("../../../tests/fmt/*.in")
	if len(ins) == 0 {
		t.Fatal("no test cases found")
	}
	for _, in := range ins {
		t.Run(strings.TrimSuffix(filepath.Base(in), ".in"), func(t *testing.T) {
			src, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(in, ".in") + ".out")
			if err != nil {
				t.Fatal(err)
			}
			got, err := Format("test.ark", string(src))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("got\n%s\nwant\n%s", got, want)
			}
			again, err := Format("test.ark", got)
			if err != nil {
				t.Fatal(err)
			}
			if again != got {
				t.Errorf("not idempotent:\n%s", again)
			}
		})
	}
}

func TestFormatErrors(t *testing.T) {
	for _, src := range []string{"f =\n\tx\n", "f = \"abc\n", "f = x)\n"} {
		if _, err := Format("test.ark", src); err == nil {
			t.Errorf("no error for %q", src)
		}
	}
}

// TestFormatSources formats every Arkadia source of the repository: the
// result must be stable, and the same after the source is messed up (wider
// indentation, more spaces between tokens, more blank lines).
func TestFormatSources(t *testing.T) {
	var files []string
	for _, dir := range []string{"lib", "examples", "selfhost/src", "tests/run", "tests/unit"} {
		filepath.WalkDir(filepath.Join("../../..", dir), func(path string, d os.DirEntry, err error) error {
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
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		want, err := Format(f, string(data))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if again, _ := Format(f, want); again != want {
			t.Errorf("%s: formatting is not idempotent", f)
		}
		messy := mess(want)
		if got, err := Format(f, messy); err != nil {
			t.Errorf("%s (messed up): %v", f, err)
		} else if got != want {
			t.Errorf("%s: the messed up source formats differently:\n%s", f, firstDiff(got, want))
		}
	}
}

// mess doubles the indentation, the spaces between tokens and the blank
// lines of a source without changing its meaning.
func mess(src string) string {
	lx := &lexer{file: "", src: src, line: 1, col: 1, sawNL: true, sawSpc: true, keepComments: true}
	lx.run()
	var b strings.Builder
	its := items(src, lx.toks, lx.comments)
	for i, it := range its {
		if it.first {
			if i > 0 {
				b.WriteString(strings.Repeat("\n", 2*(it.line-its[i-1].endLine)-1))
			}
			b.WriteString(strings.Repeat(" ", 2*(it.col-1)))
		} else {
			// (the spaces before a trailing comment are kept: they align it)
			gap := src[its[i-1].end:it.off]
			if !it.lineCmt {
				gap += gap
			}
			b.WriteString(gap)
		}
		b.WriteString(src[it.off:it.end])
	}
	return b.String()
}

func firstDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range min(len(g), len(w)) {
		if g[i] != w[i] {
			return fmt.Sprintf("line %d\n got: %s\nwant: %s", i+1, g[i], w[i])
		}
	}
	return "different number of lines"
}
