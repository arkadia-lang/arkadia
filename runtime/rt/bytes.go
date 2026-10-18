package rt

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Bytes is an immutable byte string. It is kept apart from String, whose
// values are text, so that binary data is never read as UTF-8.
type Bytes struct{ S string }

func bs(v Value) string { return v.(*Bytes).S }

func BytesEmpty() Value             { return &Bytes{} }
func BytesFromString(s Value) Value { return &Bytes{S: s.(string)} }
func BytesLength(b Value) Value     { return int64(len(bs(b))) }
func BytesAppend(a, b Value) Value  { return &Bytes{S: bs(a) + bs(b)} }
func BytesEq(a, b Value) Value      { return bs(a) == bs(b) }
func BytesCompare(a, b Value) Value { return Con0(strings.Compare(bs(a), bs(b)) + 1) }

// BytesToString decodes UTF-8 text.
func BytesToString(b Value) Value {
	s := bs(b)
	if !utf8.ValidString(s) {
		return Err("invalid UTF-8")
	}
	return Ok(s)
}

func BytesFromList(l Value) Value {
	var sb strings.Builder
	for _, v := range ListToSlice(l) {
		n := v.(int64)
		if n < 0 || n > 255 {
			panic(&ArkError{Msg: "Std.Bytes.fromList: " + strconv.FormatInt(n, 10) + " is not a byte"})
		}
		sb.WriteByte(byte(n))
	}
	return &Bytes{S: sb.String()}
}

func BytesToList(b Value) Value {
	s := bs(b)
	l := Nil
	for i := len(s) - 1; i >= 0; i-- {
		l = Cons(int64(s[i]), l)
	}
	return l
}

func BytesGet(i, b Value) Value {
	s := bs(b)
	n := i.(int64)
	if n < 0 || n >= int64(len(s)) {
		return None
	}
	return Some(int64(s[n]))
}

func BytesSlice(from, to, b Value) Value {
	s := bs(b)
	i, j := int(from.(int64)), int(to.(int64))
	i = max(0, min(i, len(s)))
	j = max(i, min(j, len(s)))
	return &Bytes{S: s[i:j]}
}
