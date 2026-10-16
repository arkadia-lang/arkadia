package rt

import (
	"fmt"
	"os"
	"time"
)

// TestCase is a compiled `test` declaration.
type TestCase struct {
	Module string
	Name   string
	Run    func()
}

type testFailure struct{ msg string }

// RunTests runs all tests, handling the Prelude Fail effect, and reports a
// summary. The process exits with status 1 when a test fails.
func RunTests(failEff int, tests []TestCase) {
	color := os.Getenv("NO_COLOR") == ""
	green, red, dim, reset := "\x1b[32m", "\x1b[31m", "\x1b[2m", "\x1b[0m"
	if !color {
		green, red, dim, reset = "", "", "", ""
	}
	passed, failed := 0, 0
	start := time.Now()
	for _, t := range tests {
		msg := runOne(failEff, t)
		if msg == "" {
			passed++
			fmt.Fprintf(stdout, "%s✓%s %s%s ›%s %s\n", green, reset, dim, t.Module, reset, t.Name)
		} else {
			failed++
			fmt.Fprintf(stdout, "%s✗ %s › %s%s\n    %s\n", red, t.Module, t.Name, reset, msg)
		}
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	if failed > 0 {
		fmt.Fprintf(stdout, "\n%s%d failed%s, %d passed (%s)\n", red, failed, reset, passed, elapsed)
		panic(&ExitSignal{Code: 1})
	}
	fmt.Fprintf(stdout, "\n%sall %d tests passed%s (%s)\n", green, passed, reset, elapsed)
}

func runOne(failEff int, t TestCase) (msg string) {
	saved := cur
	defer func() {
		cur = saved
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *testFailure:
				msg = e.msg
			case *ArkError:
				msg = "runtime error: " + e.Msg
			case *ExitSignal:
				msg = fmt.Sprintf("test called exit %d", e.Code)
			default:
				msg = fmt.Sprintf("runtime error: %v", r)
			}
		}
	}()
	if failEff < 0 {
		t.Run()
		return ""
	}
	h := &Handler{
		Eff: failEff,
		Ops: []func([]Value, Value) Value{func(args []Value, _ Value) Value {
			panic(&testFailure{msg: args[0].(string)})
		}},
		UsesK: []bool{false},
		Tail:  true,
	}
	Handle(h, func() Value { t.Run(); return Unit })
	return ""
}
