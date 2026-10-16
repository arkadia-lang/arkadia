package rt

import (
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------- bits

func IntAnd(a, b Value) Value     { return a.(int64) & b.(int64) }
func IntOr(a, b Value) Value      { return a.(int64) | b.(int64) }
func IntXor(a, b Value) Value     { return a.(int64) ^ b.(int64) }
func IntShl(a, b Value) Value     { return a.(int64) << uint64(b.(int64)) }
func IntShr(a, b Value) Value     { return a.(int64) >> uint64(b.(int64)) }
func IntUShr(a, b Value) Value    { return int64(uint64(a.(int64)) >> uint64(b.(int64))) }
func IntComplement(a Value) Value { return ^a.(int64) }

// ---------------------------------------------------------------- arrays

// Array is an immutable array backed by a Go slice.
type Array struct{ S []Value }

func ArrayFromList(l Value) Value { return &Array{S: ListToSlice(l)} }
func ArrayToList(a Value) Value   { return SliceToList(a.(*Array).S) }
func ArrayLength(a Value) Value   { return int64(len(a.(*Array).S)) }
func ArrayEmpty() Value           { return &Array{} }

func ArrayGet(i, a Value) Value {
	s := a.(*Array).S
	n := i.(int64)
	if n < 0 || n >= int64(len(s)) {
		return None
	}
	return Some(s[n])
}

func ArrayGetUnsafe(i, a Value) Value {
	s := a.(*Array).S
	n := i.(int64)
	if n < 0 || n >= int64(len(s)) {
		panic(&ArkError{Msg: "array index out of range"})
	}
	return s[n]
}

func ArraySet(i, v, a Value) Value {
	s := a.(*Array).S
	n := i.(int64)
	if n < 0 || n >= int64(len(s)) {
		return a
	}
	c := make([]Value, len(s))
	copy(c, s)
	c[n] = v
	return &Array{S: c}
}

func ArrayPush(v, a Value) Value {
	s := a.(*Array).S
	c := make([]Value, len(s), len(s)+1)
	copy(c, s)
	return &Array{S: append(c, v)}
}

func ArrayMake(n, v Value) Value {
	c := make([]Value, max(n.(int64), 0))
	for i := range c {
		c[i] = v
	}
	return &Array{S: c}
}

func ArrayGenerate(n, f Value) Value {
	c := make([]Value, max(n.(int64), 0))
	for i := range c {
		c[i] = Apply1(f, int64(i))
	}
	return &Array{S: c}
}

func ArrayMap(f, a Value) Value {
	s := a.(*Array).S
	c := make([]Value, len(s))
	for i, x := range s {
		c[i] = Apply1(f, x)
	}
	return &Array{S: c}
}

func ArrayFoldl(f, z, a Value) Value {
	acc := z
	for _, x := range a.(*Array).S {
		acc = Apply2(f, acc, x)
	}
	return acc
}

func ArrayFoldr(f, z, a Value) Value {
	acc := z
	s := a.(*Array).S
	for i := len(s) - 1; i >= 0; i-- {
		acc = Apply2(f, s[i], acc)
	}
	return acc
}

func ArraySlice(from, to, a Value) Value {
	s := a.(*Array).S
	i, j := int(from.(int64)), int(to.(int64))
	i = max(0, min(i, len(s)))
	j = max(i, min(j, len(s)))
	c := make([]Value, j-i)
	copy(c, s[i:j])
	return &Array{S: c}
}

func ArrayAppend(a, b Value) Value {
	x, y := a.(*Array).S, b.(*Array).S
	c := make([]Value, 0, len(x)+len(y))
	return &Array{S: append(append(c, x...), y...)}
}

// ArraySortBy sorts with a comparison function returning Ordering.
func ArraySortBy(cmp, a Value) Value {
	s := a.(*Array).S
	c := make([]Value, len(s))
	copy(c, s)
	sort.SliceStable(c, func(i, j int) bool { return TagOf(Apply2(cmp, c[i], c[j])) == 0 })
	return &Array{S: c}
}

// ---------------------------------------------------------------- files & processes

func AppendFile(path, content Value) Value {
	f, err := os.OpenFile(path.(string), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Err(err.Error())
	}
	defer f.Close()
	if _, err := f.WriteString(content.(string)); err != nil {
		return Err(err.Error())
	}
	return Ok(Unit)
}

func FileExists(path Value) Value {
	_, err := os.Stat(path.(string))
	return err == nil
}

func RemoveFile(path Value) Value {
	if err := os.Remove(path.(string)); err != nil {
		return Err(err.Error())
	}
	return Ok(Unit)
}

func ListDir(path Value) Value {
	entries, err := os.ReadDir(path.(string))
	if err != nil {
		return Err(err.Error())
	}
	names := make([]Value, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return Ok(SliceToList(names))
}

func MakeDir(path Value) Value {
	if err := os.MkdirAll(path.(string), 0o755); err != nil {
		return Err(err.Error())
	}
	return Ok(Unit)
}

// RunCommand runs a program and returns (exit code, stdout, stderr).
func RunCommand(name, args Value) Value {
	var as []string
	for _, a := range ListToSlice(args) {
		as = append(as, a.(string))
	}
	Flush()
	cmd := exec.Command(name.(string), as...)
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	code := int64(0)
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = int64(ee.ExitCode())
		} else {
			return Err(err.Error())
		}
	}
	return Ok(Tuple(code, out.String(), errb.String()))
}

// ---------------------------------------------------------------- strings

func StringIndexOf(sub, s Value) Value {
	i := strings.Index(s.(string), sub.(string))
	if i < 0 {
		return None
	}
	return Some(int64(len([]rune(s.(string)[:i]))))
}

func StringRepeat(n, s Value) Value {
	return strings.Repeat(s.(string), int(max(n.(int64), 0)))
}

func StringReverse(s Value) Value {
	rs := []rune(s.(string))
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}

func StringCharAt(i, s Value) Value {
	rs := []rune(s.(string))
	n := i.(int64)
	if n < 0 || n >= int64(len(rs)) {
		return None
	}
	return Some(rs[n])
}

func StringTrimStart(s Value) Value { return strings.TrimLeft(s.(string), " \t\r\n") }
func StringTrimEnd(s Value) Value   { return strings.TrimRight(s.(string), " \t\r\n") }

func NanoTime(_ Value) Value { return time.Now().UnixNano() }
