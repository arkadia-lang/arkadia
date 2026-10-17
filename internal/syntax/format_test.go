package syntax

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"spaces", `module   Main
add : Int->Int  ->Int
add a b=a+b
pair = ( 1,2 )
xs = [ 1,2 ,3 ]
lam = \x->x*2
rec = Point {x=1, y=2}
none = Empty { }
upd p = {p|x=3}
r = [1..10]
`, `module Main
add : Int -> Int -> Int
add a b = a + b
pair = (1, 2)
xs = [1, 2, 3]
lam = \x -> x * 2
rec = Point { x = 1, y = 2 }
none = Empty {}
upd p = { p | x = 3 }
r = [1 .. 10]
`},
		{"operators", `neg x = x-1
g = f -1
h = (+ 1)
k = (x +)
s = (-)
fld p = p.x + (.y) p
lt a b = a<b && b>a
`, `neg x = x - 1
g = f -1
h = (+ 1)
k = (x +)
s = (-)
fld p = p.x + (.y) p
lt a b = a < b && b > a
`},
		{"effect rows", `run : (Unit -> <IO|e> Unit) -> <e> Unit
f : Int -> <IO, State Int> Unit
`, `run : (Unit -> <IO | e> Unit) -> <e> Unit
f : Int -> <IO, State Int> Unit
`},
		{"indentation", `main () =
    let ys = map (\x ->
                x * 2) xs
    match ys with
          [] -> ()
          _ -> ()
`, `main () =
  let ys = map (\x ->
    x * 2) xs
  match ys with
    [] -> ()
    _ -> ()
`},
		{"guards", `abs n
    | n < 0 = -n
    | otherwise =
        n
f x = match x with
    Some y | y > 0 ->
        y
    _ | x == None ->
              0
      | otherwise -> 1
`, `abs n
  | n < 0 = -n
  | otherwise =
      n
f x = match x with
  Some y | y > 0 ->
    y
  _ | x == None ->
        0
    | otherwise -> 1
`},
		{"continuation lines", `type Shape
    = Circle Float
    | Rect Float Float
    deriving (Show)
sum = foo (bar 1)
          (baz 2)
xs =
    [ 1
    , 2
    ]
`, `type Shape
  = Circle Float
  | Rect Float Float
  deriving (Show)
sum = foo (bar 1)
          (baz 2)
xs =
  [ 1
  , 2
  ]
`},
		{"with after a nested block", `combine rs = match foldr (\r acc -> match r with
        Ok a -> a :: acc
        Err _ -> acc) [] rs with
    [] -> None
    as -> Some as
`, `combine rs = match foldr (\r acc -> match r with
    Ok a -> a :: acc
    Err _ -> acc) [] rs with
  [] -> None
  as -> Some as
`},
		// (written with explicit "\n" to keep the trailing spaces)
		{"comments and blank lines",
			"\n\n-- header\nmodule Main   \n\n\n-- doc\nf x =   -- trailing   \n    -- inside\n" +
				"    x   {- inline -}   + 1\n-- before g\ng = \"\"\"\n  raw   text   \n\"\"\"   \n" +
				"{- multi   \n     line -}\nh = 1",
			"-- header\nmodule Main\n\n-- doc\nf x =   -- trailing\n  -- inside\n" +
				"  x {- inline -} + 1\n-- before g\ng = \"\"\"\n  raw   text   \n\"\"\"\n" +
				"{- multi   \n     line -}\nh = 1\n"},
		{"where", `f x =
    y + 1
    where
        y = x
`, `f x =
  y + 1
  where
    y = x
`},
		{"empty", "\n\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Format("test.ark", tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got\n%s\nwant\n%s", got, tt.want)
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
		filepath.WalkDir(filepath.Join("../..", dir), func(path string, d os.DirEntry, err error) error {
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
