package rt

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ArkError is a runtime error raised by `error` or by the runtime.
type ArkError struct {
	Msg   string
	Trace []string // Arkadia functions on the stack where it was raised
}

func (e *ArkError) Error() string { return e.Msg }

// ExitSignal requests program termination with a status code.
type ExitSignal struct{ Code int }

var stdout = bufio.NewWriterSize(os.Stdout, 64*1024)
var stdin = bufio.NewReader(os.Stdin)

// Flush flushes buffered standard output.
func Flush() { stdout.Flush() }

// InitEv is the evidence of the program initialization (top-level values
// and dictionaries).
var InitEv = newTaskEv()

// Main runs the program entry point in the main task and reports runtime
// errors.
func Main(entry func(*Ev)) {
	debug.SetMaxStack(4 << 30)
	if os.Getenv("GOGC") == "" {
		// functional programs allocate many short-lived objects
		debug.SetGCPercent(400)
	}
	if path := os.Getenv("ARKADIA_CPUPROFILE"); path != "" {
		if f, err := os.Create(path); err == nil {
			pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}
	}
	code := run(entry)
	Flush()
	pprof.StopCPUProfile()
	os.Exit(code)
}

func run(entry func(*Ev)) (code int) {
	defer func() {
		if r := recover(); r != nil {
			Flush()
			switch e := r.(type) {
			case *ExitSignal:
				code = e.Code
			case *ArkError:
				fmt.Fprintf(os.Stderr, "runtime error: %s\n", e.Msg)
				if e.Trace == nil {
					e.Trace = arkTrace()
				}
				printTrace(e.Trace)
				code = 1
			default:
				fmt.Fprintf(os.Stderr, "runtime error: %v\n", r)
				printTrace(arkTrace())
				if os.Getenv("ARKADIA_TRACE") != "" {
					os.Stderr.Write(debug.Stack())
				}
				code = 2
			}
		}
	}()
	entry(newTaskEv())
	return 0
}

// MatchFail is raised when no pattern matches.
func MatchFail(where string) Value {
	panic(&ArkError{Msg: "no pattern matched at " + where})
}

func Error(msg Value) Value { panic(&ArkError{Msg: msg.(string)}) }

func Exit(code Value) Value { panic(&ExitSignal{Code: int(code.(int64))}) }

// ---------------------------------------------------------------- Int

func IntAdd(a, b Value) Value { return a.(int64) + b.(int64) }
func IntSub(a, b Value) Value { return a.(int64) - b.(int64) }
func IntMul(a, b Value) Value { return a.(int64) * b.(int64) }
func IntDiv(a, b Value) Value { return IDiv(a.(int64), b.(int64)) }
func IntRem(a, b Value) Value { return IRem(a.(int64), b.(int64)) }
func IntMod(a, b Value) Value { return IMod(a.(int64), b.(int64)) }

// IDiv, IRem and IMod are the unboxed forms used by generated code.
func IDiv(x, y int64) int64 {
	if y == 0 {
		panic(&ArkError{Msg: "division by zero"})
	}
	return x / y
}

func IRem(x, y int64) int64 {
	if y == 0 {
		panic(&ArkError{Msg: "division by zero"})
	}
	return x % y
}

func IMod(x, y int64) int64 {
	if y == 0 {
		panic(&ArkError{Msg: "division by zero"})
	}
	m := x % y
	if m != 0 && (m < 0) != (y < 0) {
		m += y
	}
	return m
}
func IntNeg(a Value) Value   { return -a.(int64) }
func IntEq(a, b Value) Value { return a.(int64) == b.(int64) }
func IntLt(a, b Value) Value { return a.(int64) < b.(int64) }
func IntLe(a, b Value) Value { return a.(int64) <= b.(int64) }
func IntGt(a, b Value) Value { return a.(int64) > b.(int64) }
func IntGe(a, b Value) Value { return a.(int64) >= b.(int64) }
func IntCompare(a, b Value) Value {
	x, y := a.(int64), b.(int64)
	switch {
	case x < y:
		return LT
	case x > y:
		return GT
	}
	return EQ
}
func IntToFloat(a Value) Value { return float64(a.(int64)) }
func ShowInt(a Value) Value    { return strconv.FormatInt(a.(int64), 10) }

// ---------------------------------------------------------------- Float

func FloatAdd(a, b Value) Value { return a.(float64) + b.(float64) }
func FloatSub(a, b Value) Value { return a.(float64) - b.(float64) }
func FloatMul(a, b Value) Value { return a.(float64) * b.(float64) }
func FloatDiv(a, b Value) Value { return a.(float64) / b.(float64) }
func FloatNeg(a Value) Value    { return -a.(float64) }
func FloatEq(a, b Value) Value  { return a.(float64) == b.(float64) }
func FloatLt(a, b Value) Value  { return a.(float64) < b.(float64) }
func FloatLe(a, b Value) Value  { return a.(float64) <= b.(float64) }
func FloatGt(a, b Value) Value  { return a.(float64) > b.(float64) }
func FloatGe(a, b Value) Value  { return a.(float64) >= b.(float64) }
func FloatCompare(a, b Value) Value {
	x, y := a.(float64), b.(float64)
	switch {
	case x < y:
		return LT
	case x > y:
		return GT
	}
	return EQ
}
func FloatTruncate(a Value) Value { return int64(a.(float64)) }
func FloatRound(a Value) Value    { return int64(math.Round(a.(float64))) }
func FloatFloor(a Value) Value    { return int64(math.Floor(a.(float64))) }
func FloatCeil(a Value) Value     { return int64(math.Ceil(a.(float64))) }
func FloatSqrt(a Value) Value     { return math.Sqrt(a.(float64)) }
func FloatPow(a, b Value) Value   { return math.Pow(a.(float64), b.(float64)) }

func ShowFloat(a Value) Value {
	f := a.(float64)
	if math.IsInf(f, 1) {
		return "Infinity"
	}
	if math.IsInf(f, -1) {
		return "-Infinity"
	}
	if math.IsNaN(f) {
		return "NaN"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEn") {
		s += ".0"
	}
	return s
}

// ---------------------------------------------------------------- String / Char

func StringEq(a, b Value) Value { return a.(string) == b.(string) }
func StringCompare(a, b Value) Value {
	switch strings.Compare(a.(string), b.(string)) {
	case -1:
		return LT
	case 1:
		return GT
	}
	return EQ
}
func StringConcat(a, b Value) Value { return a.(string) + b.(string) }

// Concat joins already evaluated string parts (string interpolation).
func Concat(parts ...Value) Value {
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.(string))
	}
	return sb.String()
}

func StringLength(s Value) Value { return int64(utf8.RuneCountInString(s.(string))) }
func StringChars(s Value) Value {
	rs := []rune(s.(string))
	l := Nil
	for i := len(rs) - 1; i >= 0; i-- {
		l = Cons(rs[i], l)
	}
	return l
}
func StringFromChars(l Value) Value {
	var sb strings.Builder
	for _, c := range ListToSlice(l) {
		sb.WriteRune(c.(rune))
	}
	return sb.String()
}
func StringJoin(sep, l Value) Value {
	xs := ListToSlice(l)
	ss := make([]string, len(xs))
	for i, x := range xs {
		ss[i] = x.(string)
	}
	return strings.Join(ss, sep.(string))
}
func StringSplit(sep, s Value) Value {
	parts := strings.Split(s.(string), sep.(string))
	vs := make([]Value, len(parts))
	for i, p := range parts {
		vs[i] = p
	}
	return SliceToList(vs)
}
func StringTrim(s Value) Value            { return strings.TrimSpace(s.(string)) }
func StringToUpper(s Value) Value         { return strings.ToUpper(s.(string)) }
func StringToLower(s Value) Value         { return strings.ToLower(s.(string)) }
func StringContains(sub, s Value) Value   { return strings.Contains(s.(string), sub.(string)) }
func StringStartsWith(pre, s Value) Value { return strings.HasPrefix(s.(string), pre.(string)) }
func StringEndsWith(suf, s Value) Value   { return strings.HasSuffix(s.(string), suf.(string)) }
func StringReplace(old, new, s Value) Value {
	return strings.ReplaceAll(s.(string), old.(string), new.(string))
}
func StringSubstring(start, end, s Value) Value {
	rs := []rune(s.(string))
	a, b := int(start.(int64)), int(end.(int64))
	if a < 0 {
		a = 0
	}
	if b > len(rs) {
		b = len(rs)
	}
	if a >= b {
		return ""
	}
	return string(rs[a:b])
}
func ParseInt(s Value) Value {
	n, err := strconv.ParseInt(strings.TrimSpace(s.(string)), 10, 64)
	if err != nil {
		return None
	}
	return Some(n)
}
func ParseFloat(s Value) Value {
	f, err := strconv.ParseFloat(strings.TrimSpace(s.(string)), 64)
	if err != nil {
		return None
	}
	return Some(f)
}
func ShowString(s Value) Value   { return strconv.Quote(s.(string)) }
func ShowChar(c Value) Value     { return strconv.QuoteRune(c.(rune)) }
func CharToString(c Value) Value { return string(c.(rune)) }
func CharEq(a, b Value) Value    { return a.(rune) == b.(rune) }
func CharCompare(a, b Value) Value {
	x, y := a.(rune), b.(rune)
	switch {
	case x < y:
		return LT
	case x > y:
		return GT
	}
	return EQ
}
func CharCode(c Value) Value     { return int64(c.(rune)) }
func FromCharCode(n Value) Value { return rune(n.(int64)) }
func CharIsDigit(c Value) Value  { return unicode.IsDigit(c.(rune)) }
func CharIsAlpha(c Value) Value  { return unicode.IsLetter(c.(rune)) }
func CharIsSpace(c Value) Value  { return unicode.IsSpace(c.(rune)) }
func CharIsUpper(c Value) Value  { return unicode.IsUpper(c.(rune)) }
func CharIsLower(c Value) Value  { return unicode.IsLower(c.(rune)) }

// ShowArg parenthesizes a shown value when it is not atomic.
func ShowArg(s Value) Value {
	str := s.(string)
	if str == "" {
		return str
	}
	switch str[0] {
	case '(', '[', '"', '\'', '{':
		return str
	case '-':
		return "(" + str + ")"
	}
	if strings.ContainsRune(str, ' ') {
		return "(" + str + ")"
	}
	return str
}

// ---------------------------------------------------------------- IO

func Print(s Value) Value {
	stdout.WriteString(s.(string))
	return Unit
}

func Println(s Value) Value {
	stdout.WriteString(s.(string))
	stdout.WriteByte('\n')
	return Unit
}

func Eprintln(s Value) Value {
	Flush()
	fmt.Fprintln(os.Stderr, s.(string))
	return Unit
}

func ReadLine(_ Value) Value {
	Flush()
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return None
	}
	return Some(strings.TrimRight(line, "\r\n"))
}

func ReadAll(_ Value) Value {
	Flush()
	data, _ := io.ReadAll(stdin)
	return string(data)
}

func ReadFile(path Value) Value {
	data, err := os.ReadFile(path.(string))
	if err != nil {
		return Err(err.Error())
	}
	return Ok(string(data))
}

func WriteFile(path, content Value) Value {
	if err := os.WriteFile(path.(string), []byte(content.(string)), 0o644); err != nil {
		return Err(err.Error())
	}
	return Ok(Unit)
}

// ProgramArgs holds the command-line arguments (without the program name).
var ProgramArgs = os.Args[1:]

func Args(_ Value) Value {
	vs := make([]Value, len(ProgramArgs))
	for i, a := range ProgramArgs {
		vs[i] = a
	}
	return SliceToList(vs)
}

func GetEnv(name Value) Value {
	v, ok := os.LookupEnv(name.(string))
	if !ok {
		return None
	}
	return Some(v)
}

var startTime = time.Now()

func Clock(_ Value) Value { return time.Since(startTime).Seconds() }

func Trace(msg, v Value) Value {
	Flush()
	fmt.Fprintln(os.Stderr, "trace:", msg.(string))
	return v
}
