package rt

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// TestCase is a compiled `test` declaration.
type TestCase struct {
	Module string
	Name   string
	Run    func(*Ev)
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
	passed, failed, skipped := 0, 0, 0
	filter := os.Getenv("ARKADIA_TEST_RUN")
	start := time.Now()
	for _, t := range tests {
		if filter != "" && !strings.Contains(t.Module+" › "+t.Name, filter) {
			skipped++
			continue
		}
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
	skip := ""
	if skipped > 0 {
		skip = fmt.Sprintf(", %d skipped", skipped)
	}
	if failed > 0 {
		fmt.Fprintf(stdout, "\n%s%d failed%s, %d passed%s (%s)\n", red, failed, reset, passed, skip, elapsed)
		panic(&ExitSignal{Code: 1})
	}
	if passed == 0 {
		fmt.Fprintf(stdout, "no tests match %q\n", filter)
		return
	}
	fmt.Fprintf(stdout, "\n%sall %d tests passed%s%s (%s)\n", green, passed, reset, skip, elapsed)
}

func runOne(failEff int, t TestCase) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *testFailure:
				msg = e.msg
			case *ArkError:
				msg = "runtime error: " + e.Msg
				if e.Trace == nil {
					e.Trace = arkTrace()
				}
				for i, line := range e.Trace {
					if i == 8 || strings.Contains(line, "<test>") {
						break
					}
					msg += "\n      in " + line
				}
			case *ExitSignal:
				msg = fmt.Sprintf("test called exit %d", e.Code)
			default:
				msg = fmt.Sprintf("runtime error: %v", r)
			}
		}
	}()
	ev := newTaskEv()
	if failEff < 0 {
		t.Run(ev)
		return ""
	}
	h := &Handler{
		Eff: failEff,
		Ops: []func(*Ev, []Value, Value) Value{func(_ *Ev, args []Value, _ Value) Value {
			panic(&testFailure{msg: args[0].(string)})
		}},
		UsesK: []bool{false},
		Tail:  true,
	}
	Handle(ev, h, func(ev *Ev) Value { t.Run(ev); return Unit })
	return ""
}
