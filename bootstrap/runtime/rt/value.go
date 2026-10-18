// Package rt is the runtime support library linked into every compiled
// Arkadia program.
package rt

// Value is the uniform representation of Arkadia values:
//
//	Int    -> int64        Float -> float64     String -> string
//	Char   -> rune         Bool  -> bool        Unit   -> UnitT
//	data constructors -> *D0 / *D1 / *D2 / *D3 / *DN
//	functions -> Fn1 / Fn2 / Fn3 / Fn4 / *FnN
//	dictionaries -> *Dict
type Value = any

// UnitT is the type of the unit value.
type UnitT struct{}

// Unit is the unit value ().
var Unit Value = UnitT{}

// Data is implemented by all constructor values.
type Data interface{ Tag() int }

type D0 struct{ T int }
type D1 struct {
	T int
	A Value
}
type D2 struct {
	T    int
	A, B Value
}
type D3 struct {
	T       int
	A, B, C Value
}
type DN struct {
	T int
	F []Value
}

func (d *D0) Tag() int { return d.T }
func (d *D1) Tag() int { return d.T }
func (d *D2) Tag() int { return d.T }
func (d *D3) Tag() int { return d.T }
func (d *DN) Tag() int { return d.T }

// TagOf returns the constructor tag of a data value.
func TagOf(v Value) int { return v.(Data).Tag() }

// nullary is initialized by a var initializer (not init) because other
// package-level variables such as Nil depend on it.
var nullary = func() (n [256]*D0) {
	for i := range n {
		n[i] = &D0{T: i}
	}
	return
}()

// Con0 returns the shared value of a nullary constructor.
func Con0(tag int) Value {
	if tag < len(nullary) {
		return nullary[tag]
	}
	return &D0{T: tag}
}

// Field returns field i of a constructor value with the given arity.
func Field(v Value, arity, i int) Value {
	switch arity {
	case 1:
		return v.(*D1).A
	case 2:
		d := v.(*D2)
		if i == 0 {
			return d.A
		}
		return d.B
	case 3:
		d := v.(*D3)
		switch i {
		case 0:
			return d.A
		case 1:
			return d.B
		}
		return d.C
	}
	return v.(*DN).F[i]
}

// Mk builds a constructor value.
func Mk(tag int, fields ...Value) Value {
	switch len(fields) {
	case 0:
		return Con0(tag)
	case 1:
		return &D1{tag, fields[0]}
	case 2:
		return &D2{tag, fields[0], fields[1]}
	case 3:
		return &D3{tag, fields[0], fields[1], fields[2]}
	}
	return &DN{tag, fields}
}

// ---------------------------------------------------------------- lists

// Nil is the empty list.
var Nil Value = Con0(0)

// Cons prepends an element.
func Cons(h, t Value) Value { return &D2{1, h, t} }

// ListToSlice converts an Arkadia list to a Go slice.
func ListToSlice(v Value) []Value {
	var out []Value
	for {
		c, ok := v.(*D2)
		if !ok {
			return out
		}
		out = append(out, c.A)
		v = c.B
	}
}

// SliceToList converts a Go slice to an Arkadia list.
func SliceToList(xs []Value) Value {
	l := Nil
	for i := len(xs) - 1; i >= 0; i-- {
		l = Cons(xs[i], l)
	}
	return l
}

// ---------------------------------------------------------------- option / result

// Option and Result follow the constructor order in the Prelude:
//   type Option a = None | Some a
//   type Result e a = Ok a | Err e

var None Value = Con0(0)

func Some(v Value) Value { return &D1{1, v} }
func Ok(v Value) Value   { return &D1{0, v} }
func Err(v Value) Value  { return &D1{1, v} }

// Ordering: LT | EQ | GT
var (
	LT Value = Con0(0)
	EQ Value = Con0(1)
	GT Value = Con0(2)
)

// ---------------------------------------------------------------- tuples

func Tuple(xs ...Value) Value { return Mk(0, xs...) }

// ---------------------------------------------------------------- dictionaries

// Dict is a type class dictionary: superclass dictionaries followed by
// method implementations.
type Dict struct{ S []Value }

func NewDict(n int) *Dict { return &Dict{S: make([]Value, n)} }

// Sel selects a dictionary slot.
func Sel(d Value, i int) Value { return d.(*Dict).S[i] }
