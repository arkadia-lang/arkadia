package rt

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"unicode"
)

// Backtraces of runtime errors in terms of Arkadia functions, recovered
// from the names of the generated Go functions.

const maxTraceLines = 24

// arkTrace lists the Arkadia functions on the current goroutine's stack,
// innermost first, with directly repeated entries (recursion) collapsed.
func arkTrace() []string {
	pcs := make([]uintptr, 4096)
	n := runtime.Callers(2, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	var out []string
	last, count := "", 0
	flush := func() {
		if count == 0 {
			return
		}
		line := last
		if count > 1 {
			line += fmt.Sprintf(" (%d times)", count)
		}
		out = append(out, line)
	}
	for {
		fr, more := frames.Next()
		if name := arkFuncName(fr.Function); name != "" {
			if name == last {
				count++
			} else {
				flush()
				last, count = name, 1
			}
		}
		if !more {
			break
		}
	}
	flush()
	return out
}

// printTrace writes a backtrace below a runtime error message.
func printTrace(trace []string) {
	for i, line := range trace {
		if i == maxTraceLines {
			fmt.Fprintf(os.Stderr, "  ... %d more\n", len(trace)-i)
			break
		}
		fmt.Fprintf(os.Stderr, "  in %s\n", line)
	}
}

// withTrace records the backtrace of a runtime error at the point where it
// is first caught, before the stack unwinds.
func withTrace(r any) any {
	if e, ok := r.(*ArkError); ok && e.Trace == nil {
		e.Trace = arkTrace()
	}
	return r
}

// arkFuncName turns the name of a generated Go function (main.f_Main_go,
// or a function literal inside it) into the Arkadia function it belongs
// to; it returns "" for runtime and library frames.
func arkFuncName(fn string) string {
	s, ok := strings.CutPrefix(fn, "main.")
	if !ok {
		return ""
	}
	local := false
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s, local = s[:i], true
	}
	s, ok = strings.CutPrefix(s, "f_")
	if !ok {
		return ""
	}
	name := arkSymbol(s)
	if local {
		return "a local function of " + name
	}
	return name
}

// arkSymbol decodes a mangled global symbol.
func arkSymbol(s string) string {
	// module segments: capitalized words followed by '_'
	var mod []string
	for {
		i := strings.IndexByte(s, '_')
		if i <= 0 || !unicode.IsUpper(rune(s[0])) || strings.ContainsRune(s[:i], 'ǂ') {
			break
		}
		mod = append(mod, s[:i])
		s = s[i+1:]
	}
	parts := strings.Split(s, "ǂ")
	var name string
	switch {
	case len(parts) == 5 && parts[0] == "" && parts[1] == "inst":
		name = "instance " + parts[2] + " " + parts[3] + " " + unsanitize(parts[4])
	case len(parts) == 3 && parts[1] == "dflt":
		name = parts[0] + " default " + unsanitize(parts[2])
	case len(parts) == 2 && parts[0] == "" && strings.HasPrefix(parts[1], "test"):
		name = "<test>"
	default:
		name = unsanitize(demangle(strings.Join(parts, "$")))
	}
	if len(mod) == 0 {
		return name
	}
	return strings.Join(mod, ".") + "." + name
}

// demangle decodes the escapes of mangled operator characters (ǀ2b).
func demangle(s string) string {
	var sb strings.Builder
	for {
		i := strings.Index(s, "ǀ")
		if i < 0 || len(s) < i+len("ǀ")+2 {
			sb.WriteString(s)
			break
		}
		sb.WriteString(s[:i])
		hex := s[i+len("ǀ") : i+len("ǀ")+2]
		if c, err := strconv.ParseUint(hex, 16, 8); err == nil {
			sb.WriteByte(byte(c))
		}
		s = s[i+len("ǀ")+2:]
	}
	return sb.String()
}

var opRunes = map[string]rune{
	"bang": '!', "hash": '#', "dollar": '$', "pct": '%', "amp": '&', "star": '*',
	"plus": '+', "dot": '.', "slash": '/', "lt": '<', "eq": '=', "gt": '>',
	"qm": '?', "at": '@', "bslash": '\\', "caret": '^', "bar": '|', "minus": '-',
	"tilde": '~', "colon": ':',
}

// unsanitize reverses the identifier encoding of an operator name such as
// "_eq_eq" for (==).
func unsanitize(s string) string {
	if !strings.HasPrefix(s, "_") {
		return s
	}
	var sb strings.Builder
	for _, p := range strings.Split(s[1:], "_") {
		r, ok := opRunes[p]
		if !ok {
			return s
		}
		sb.WriteRune(r)
	}
	return "(" + sb.String() + ")"
}
