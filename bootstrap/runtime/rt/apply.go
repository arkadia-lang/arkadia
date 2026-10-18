package rt

// Function values. Every Arkadia function is curried; functions of known
// arity are represented by the Go function type of that arity so that
// saturated calls avoid intermediate closures.
type Fn1 func(Value) Value
type Fn2 func(Value, Value) Value
type Fn3 func(Value, Value, Value) Value
type Fn4 func(Value, Value, Value, Value) Value

// FnN is a function of arity > 4.
type FnN struct {
	N int
	F func([]Value) Value
}

// Arity reports the arity of a function value.
func Arity(f Value) int {
	switch f := f.(type) {
	case Fn1:
		return 1
	case Fn2:
		return 2
	case Fn3:
		return 3
	case Fn4:
		return 4
	case *FnN:
		return f.N
	}
	panic(&ArkError{Msg: "internal error: applying a non-function value"})
}

func Apply1(f Value, a Value) Value {
	switch f := f.(type) {
	case Fn1:
		return f(a)
	case Fn2:
		return Fn1(func(b Value) Value { return f(a, b) })
	case Fn3:
		return Fn2(func(b, c Value) Value { return f(a, b, c) })
	case Fn4:
		return Fn3(func(b, c, d Value) Value { return f(a, b, c, d) })
	}
	return ApplyN(f, a)
}

func Apply2(f Value, a, b Value) Value {
	switch f := f.(type) {
	case Fn2:
		return f(a, b)
	case Fn1:
		return Apply1(f(a), b)
	case Fn3:
		return Fn1(func(c Value) Value { return f(a, b, c) })
	case Fn4:
		return Fn2(func(c, d Value) Value { return f(a, b, c, d) })
	}
	return ApplyN(f, a, b)
}

func Apply3(f Value, a, b, c Value) Value {
	switch f := f.(type) {
	case Fn3:
		return f(a, b, c)
	case Fn1:
		return Apply2(f(a), b, c)
	case Fn2:
		return Apply1(f(a, b), c)
	case Fn4:
		return Fn1(func(d Value) Value { return f(a, b, c, d) })
	}
	return ApplyN(f, a, b, c)
}

func Apply4(f Value, a, b, c, d Value) Value {
	switch f := f.(type) {
	case Fn4:
		return f(a, b, c, d)
	case Fn1:
		return Apply3(f(a), b, c, d)
	case Fn2:
		return Apply2(f(a, b), c, d)
	case Fn3:
		return Apply1(f(a, b, c), d)
	}
	return ApplyN(f, a, b, c, d)
}

// ApplyN applies a function to any number of arguments.
func ApplyN(f Value, args ...Value) Value {
	for len(args) > 0 {
		switch fn := f.(type) {
		case Fn1:
			f, args = fn(args[0]), args[1:]
			continue
		case Fn2:
			if len(args) >= 2 {
				f, args = fn(args[0], args[1]), args[2:]
				continue
			}
		case Fn3:
			if len(args) >= 3 {
				f, args = fn(args[0], args[1], args[2]), args[3:]
				continue
			}
		case Fn4:
			if len(args) >= 4 {
				f, args = fn(args[0], args[1], args[2], args[3]), args[4:]
				continue
			}
		case *FnN:
			if len(args) >= fn.N {
				f, args = fn.F(args[:fn.N:fn.N]), args[fn.N:]
				continue
			}
			got := append([]Value(nil), args...)
			return &FnN{N: fn.N - len(got), F: func(rest []Value) Value {
				all := make([]Value, 0, len(got)+len(rest))
				all = append(all, got...)
				all = append(all, rest...)
				return fn.F(all)
			}}
		default:
			Arity(f)
		}
		// partial application of Fn2..Fn4
		switch len(args) {
		case 1:
			return Apply1(f, args[0])
		case 2:
			return Apply2(f, args[0], args[1])
		case 3:
			return Apply3(f, args[0], args[1], args[2])
		}
	}
	return f
}
