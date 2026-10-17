package rt

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Runtime support for Std.Regex, Std.MArray, Std.HashMap, Std.BigInt and
// Std.Crypto.

func tuple2(a, b Value) Value { return &D2{0, a, b} }

// ---------------------------------------------------------------- regex

type Regex struct{ R *regexp.Regexp }

func RegexCompile(s Value) Value {
	r, err := regexp.Compile(s.(string))
	if err != nil {
		return Err(err.Error())
	}
	return Ok(&Regex{R: r})
}

func RegexMust(s Value) Value {
	r, err := regexp.Compile(s.(string))
	if err != nil {
		panic(&ArkError{Msg: "invalid regular expression: " + err.Error()})
	}
	return &Regex{R: r}
}

func re(v Value) *regexp.Regexp { return v.(*Regex).R }

func RegexSource(r Value) Value           { return re(r).String() }
func RegexMatches(r, s Value) Value       { return re(r).MatchString(s.(string)) }
func RegexFindAll(r, s Value) Value       { return stringList(re(r).FindAllString(s.(string), -1)) }
func RegexSplit(r, s Value) Value         { return stringList(re(r).Split(s.(string), -1)) }
func RegexReplace(r, repl, s Value) Value { return re(r).ReplaceAllString(s.(string), repl.(string)) }
func RegexReplaceLiteral(r, repl, s Value) Value {
	return re(r).ReplaceAllLiteralString(s.(string), repl.(string))
}

func RegexFind(r, s Value) Value {
	loc := re(r).FindStringIndex(s.(string))
	if loc == nil {
		return None
	}
	return Some(s.(string)[loc[0]:loc[1]])
}

// groups converts submatch indices to a list of the whole match and its
// groups; a group that did not participate is the empty string.
func groups(s string, loc []int) Value {
	parts := make([]string, len(loc)/2)
	for i := range parts {
		if loc[2*i] >= 0 {
			parts[i] = s[loc[2*i]:loc[2*i+1]]
		}
	}
	return stringList(parts)
}

func RegexCaptures(r, s Value) Value {
	str := s.(string)
	loc := re(r).FindStringSubmatchIndex(str)
	if loc == nil {
		return None
	}
	return Some(groups(str, loc))
}

func RegexCapturesAll(r, s Value) Value {
	str := s.(string)
	var out []Value
	for _, loc := range re(r).FindAllStringSubmatchIndex(str, -1) {
		out = append(out, groups(str, loc))
	}
	return SliceToList(out)
}

func RegexNamedGroups(r Value) Value {
	var out []Value
	for _, n := range re(r).SubexpNames()[1:] {
		out = append(out, n)
	}
	return SliceToList(out)
}

// RegexReplaceWith replaces every match with f applied to its groups.
func RegexReplaceWith(r, f, s Value) Value {
	str := s.(string)
	var sb strings.Builder
	last := 0
	for _, loc := range re(r).FindAllStringSubmatchIndex(str, -1) {
		sb.WriteString(str[last:loc[0]])
		sb.WriteString(Apply1(f, groups(str, loc)).(string))
		last = loc[1]
	}
	sb.WriteString(str[last:])
	return sb.String()
}

func RegexQuote(s Value) Value { return regexp.QuoteMeta(s.(string)) }

// ---------------------------------------------------------------- mutable arrays

type MArray struct{ S []Value }

func marr(v Value) *MArray { return v.(*MArray) }

func outOfRange(i int64, n int) {
	panic(&ArkError{Msg: fmt.Sprintf("index %d out of range for a mutable array of length %d", i, n)})
}

func MArrayMake(n, v Value) Value {
	s := make([]Value, max(n.(int64), 0))
	for i := range s {
		s[i] = v
	}
	return &MArray{S: s}
}

func MArrayNew(_ Value) Value      { return &MArray{} }
func MArrayFromList(l Value) Value { return &MArray{S: ListToSlice(l)} }
func MArrayToList(a Value) Value   { return SliceToList(marr(a).S) }
func MArrayLength(a Value) Value   { return int64(len(marr(a).S)) }
func MArrayFreeze(a Value) Value   { return &Array{S: append([]Value(nil), marr(a).S...)} }
func MArrayThaw(a Value) Value     { return &MArray{S: append([]Value(nil), a.(*Array).S...)} }
func MArrayCopy(a Value) Value     { return &MArray{S: append([]Value(nil), marr(a).S...)} }
func MArrayClear(a Value) Value    { marr(a).S = nil; return Unit }
func MArrayPush(v, a Value) Value  { m := marr(a); m.S = append(m.S, v); return Unit }
func MArrayAppendList(l, a Value) Value {
	m := marr(a)
	m.S = append(m.S, ListToSlice(l)...)
	return Unit
}

func MArrayGet(i, a Value) Value {
	s := marr(a).S
	n := i.(int64)
	if n < 0 || n >= int64(len(s)) {
		return None
	}
	return Some(s[n])
}

func MArrayAt(i, a Value) Value {
	s := marr(a).S
	n := i.(int64)
	if n < 0 || n >= int64(len(s)) {
		outOfRange(n, len(s))
	}
	return s[n]
}

func MArraySet(i, v, a Value) Value {
	s := marr(a).S
	n := i.(int64)
	if n < 0 || n >= int64(len(s)) {
		outOfRange(n, len(s))
	}
	s[n] = v
	return Unit
}

func MArrayPop(a Value) Value {
	m := marr(a)
	if len(m.S) == 0 {
		return None
	}
	v := m.S[len(m.S)-1]
	m.S[len(m.S)-1] = nil
	m.S = m.S[:len(m.S)-1]
	return Some(v)
}

func MArraySwap(i, j, a Value) Value {
	s := marr(a).S
	x, y := i.(int64), j.(int64)
	if x < 0 || x >= int64(len(s)) {
		outOfRange(x, len(s))
	}
	if y < 0 || y >= int64(len(s)) {
		outOfRange(y, len(s))
	}
	s[x], s[y] = s[y], s[x]
	return Unit
}

func MArrayReverse(a Value) Value {
	s := marr(a).S
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return Unit
}

// MArraySortBy sorts in place (stably) with a comparison function.
func MArraySortBy(cmp, a Value) Value {
	s := marr(a).S
	sortStable(s, func(x, y Value) bool { return TagOf(Apply2(cmp, x, y)) == 0 })
	return Unit
}

// sortStable is a merge sort that calls less at most O(n log n) times and
// never with inconsistent arguments, so comparison functions may perform
// effects.
func sortStable(s []Value, less func(a, b Value) bool) {
	if len(s) < 2 {
		return
	}
	tmp := make([]Value, len(s))
	var rec func(lo, hi int)
	rec = func(lo, hi int) {
		if hi-lo < 2 {
			return
		}
		mid := (lo + hi) / 2
		rec(lo, mid)
		rec(mid, hi)
		i, j, k := lo, mid, lo
		for i < mid && j < hi {
			if less(s[j], s[i]) {
				tmp[k] = s[j]
				j++
			} else {
				tmp[k] = s[i]
				i++
			}
			k++
		}
		k += copy(tmp[k:], s[i:mid])
		copy(tmp[k:], s[j:hi])
		copy(s[lo:hi], tmp[lo:hi])
	}
	rec(0, len(s))
}

// ---------------------------------------------------------------- hash maps

// hashKey converts a value to a comparable Go value with the same
// structural equality. Functions cannot be keys.
func hashKey(v Value) any {
	switch x := v.(type) {
	case int64, string, rune, bool, UnitT:
		return x
	case float64:
		if x == 0 {
			return float64(0) // -0.0 == 0.0
		}
		return x
	}
	var sb strings.Builder
	writeKey(&sb, v)
	return keyString(sb.String())
}

type keyString string

func writeKey(sb *strings.Builder, v Value) {
	switch x := v.(type) {
	case int64:
		sb.WriteString("i")
		sb.WriteString(strconv.FormatInt(x, 10))
		sb.WriteByte(';')
	case float64:
		if x == 0 {
			x = 0
		}
		sb.WriteString("f")
		sb.WriteString(strconv.FormatUint(math.Float64bits(x), 16))
		sb.WriteByte(';')
	case string:
		sb.WriteString("s")
		sb.WriteString(strconv.Itoa(len(x)))
		sb.WriteByte(':')
		sb.WriteString(x)
	case rune:
		sb.WriteString("c")
		sb.WriteString(strconv.Itoa(int(x)))
		sb.WriteByte(';')
	case bool:
		if x {
			sb.WriteString("T")
		} else {
			sb.WriteString("F")
		}
	case UnitT:
		sb.WriteString("u")
	case *D0:
		fmt.Fprintf(sb, "(%d)", x.T)
	case *D1:
		fmt.Fprintf(sb, "(%d ", x.T)
		writeKey(sb, x.A)
		sb.WriteByte(')')
	case *D2:
		fmt.Fprintf(sb, "(%d ", x.T)
		writeKey(sb, x.A)
		writeKey(sb, x.B)
		sb.WriteByte(')')
	case *D3:
		fmt.Fprintf(sb, "(%d ", x.T)
		writeKey(sb, x.A)
		writeKey(sb, x.B)
		writeKey(sb, x.C)
		sb.WriteByte(')')
	case *DN:
		fmt.Fprintf(sb, "(%d ", x.T)
		for _, f := range x.F {
			writeKey(sb, f)
		}
		sb.WriteByte(')')
	case *Array:
		sb.WriteString("[")
		for _, f := range x.S {
			writeKey(sb, f)
		}
		sb.WriteByte(']')
	case *BigInt:
		sb.WriteString("b")
		sb.WriteString(x.N.String())
		sb.WriteByte(';')
	default:
		panic(&ArkError{Msg: fmt.Sprintf("a value of this kind (%T) cannot be used as a hash map key", v)})
	}
}

type hmEntry struct {
	key, val   Value
	prev, next *hmEntry
}

// HashMap is a mutable hash map that remembers insertion order.
type HashMap struct {
	m           map[any]*hmEntry
	first, last *hmEntry
}

func hmap(v Value) *HashMap { return v.(*HashMap) }

func HashMapNew(_ Value) Value { return &HashMap{m: map[any]*hmEntry{}} }

func HashMapSize(h Value) Value { return int64(len(hmap(h).m)) }

func HashMapInsert(k, v, h Value) Value {
	m := hmap(h)
	key := hashKey(k)
	if e, ok := m.m[key]; ok {
		e.val = v
		return Unit
	}
	e := &hmEntry{key: k, val: v, prev: m.last}
	if m.last != nil {
		m.last.next = e
	} else {
		m.first = e
	}
	m.last = e
	m.m[key] = e
	return Unit
}

func HashMapLookup(k, h Value) Value {
	if e, ok := hmap(h).m[hashKey(k)]; ok {
		return Some(e.val)
	}
	return None
}

func HashMapMember(k, h Value) Value {
	_, ok := hmap(h).m[hashKey(k)]
	return ok
}

func HashMapRemove(k, h Value) Value {
	m := hmap(h)
	key := hashKey(k)
	e, ok := m.m[key]
	if !ok {
		return Unit
	}
	delete(m.m, key)
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		m.first = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		m.last = e.prev
	}
	return Unit
}

func HashMapClear(h Value) Value {
	m := hmap(h)
	m.m = map[any]*hmEntry{}
	m.first, m.last = nil, nil
	return Unit
}

func HashMapToList(h Value) Value {
	var out []Value
	for e := hmap(h).first; e != nil; e = e.next {
		out = append(out, tuple2(e.key, e.val))
	}
	return SliceToList(out)
}

func HashMapKeys(h Value) Value {
	var out []Value
	for e := hmap(h).first; e != nil; e = e.next {
		out = append(out, e.key)
	}
	return SliceToList(out)
}

func HashMapValues(h Value) Value {
	var out []Value
	for e := hmap(h).first; e != nil; e = e.next {
		out = append(out, e.val)
	}
	return SliceToList(out)
}

// ---------------------------------------------------------------- big integers

type BigInt struct{ N *big.Int }

func bi(v Value) *big.Int    { return v.(*BigInt).N }
func mkBig(n *big.Int) Value { return &BigInt{N: n} }

func BigFromInt(n Value) Value { return mkBig(big.NewInt(n.(int64))) }

func BigParse(s Value) Value {
	str := strings.ReplaceAll(strings.TrimSpace(s.(string)), "_", "")
	n, ok := new(big.Int).SetString(str, 0)
	if !ok {
		return None
	}
	return Some(mkBig(n))
}

func BigToString(n Value) Value { return bi(n).String() }

func BigToStringBase(base, n Value) Value {
	b := base.(int64)
	if b < 2 || b > 36 {
		panic(&ArkError{Msg: "base must be between 2 and 36"})
	}
	return bi(n).Text(int(b))
}

func BigToInt(n Value) Value {
	x := bi(n)
	if !x.IsInt64() {
		return None
	}
	return Some(x.Int64())
}

func BigToFloat(n Value) Value {
	f, _ := new(big.Float).SetInt(bi(n)).Float64()
	return f
}

func BigAdd(a, b Value) Value { return mkBig(new(big.Int).Add(bi(a), bi(b))) }
func BigSub(a, b Value) Value { return mkBig(new(big.Int).Sub(bi(a), bi(b))) }
func BigMul(a, b Value) Value { return mkBig(new(big.Int).Mul(bi(a), bi(b))) }
func BigNeg(a Value) Value    { return mkBig(new(big.Int).Neg(bi(a))) }
func BigAbs(a Value) Value    { return mkBig(new(big.Int).Abs(bi(a))) }
func BigSign(a Value) Value   { return int64(bi(a).Sign()) }

func checkZero(b *big.Int) {
	if b.Sign() == 0 {
		panic(&ArkError{Msg: "division by zero"})
	}
}

// BigDiv and BigMod truncate toward zero, like Int division.
func BigDiv(a, b Value) Value {
	checkZero(bi(b))
	return mkBig(new(big.Int).Quo(bi(a), bi(b)))
}

func BigMod(a, b Value) Value {
	checkZero(bi(b))
	return mkBig(new(big.Int).Rem(bi(a), bi(b)))
}

func BigPow(a, e Value) Value {
	n := e.(int64)
	if n < 0 {
		panic(&ArkError{Msg: "negative exponent"})
	}
	return mkBig(new(big.Int).Exp(bi(a), big.NewInt(n), nil))
}

func BigPowMod(a, e, m Value) Value {
	checkZero(bi(m))
	if bi(e).Sign() < 0 {
		panic(&ArkError{Msg: "negative exponent"})
	}
	return mkBig(new(big.Int).Exp(bi(a), bi(e), bi(m)))
}

func BigGcd(a, b Value) Value {
	return mkBig(new(big.Int).GCD(nil, nil, new(big.Int).Abs(bi(a)), new(big.Int).Abs(bi(b))))
}

func BigSqrt(a Value) Value {
	if bi(a).Sign() < 0 {
		panic(&ArkError{Msg: "square root of a negative number"})
	}
	return mkBig(new(big.Int).Sqrt(bi(a)))
}

func BigEq(a, b Value) Value      { return bi(a).Cmp(bi(b)) == 0 }
func BigCompare(a, b Value) Value { return Con0(bi(a).Cmp(bi(b)) + 1) }

// ---------------------------------------------------------------- encodings & hashes

func Base64Encode(s Value) Value    { return base64.StdEncoding.EncodeToString([]byte(s.(string))) }
func Base64URLEncode(s Value) Value { return base64.RawURLEncoding.EncodeToString([]byte(s.(string))) }

func decodeResult(b []byte, err error) Value {
	if err != nil {
		return Err(err.Error())
	}
	return Ok(string(b))
}

func Base64Decode(s Value) Value { return decodeResult(base64.StdEncoding.DecodeString(s.(string))) }
func Base64URLDecode(s Value) Value {
	return decodeResult(base64.RawURLEncoding.DecodeString(strings.TrimRight(s.(string), "=")))
}
func HexEncode(s Value) Value { return hex.EncodeToString([]byte(s.(string))) }
func HexDecode(s Value) Value { return decodeResult(hex.DecodeString(s.(string))) }

func digest(h hash.Hash, s Value) Value {
	h.Write([]byte(s.(string)))
	return hex.EncodeToString(h.Sum(nil))
}

func Md5(s Value) Value    { return digest(md5.New(), s) }
func Sha1(s Value) Value   { return digest(sha1.New(), s) }
func Sha256(s Value) Value { return digest(sha256.New(), s) }
func Sha512(s Value) Value { return digest(sha512.New(), s) }

func HmacSha256(key, s Value) Value { return digest(hmac.New(sha256.New, []byte(key.(string))), s) }

func RandomBytes(n Value) Value {
	b := make([]byte, max(n.(int64), 0))
	if _, err := rand.Read(b); err != nil {
		panic(&ArkError{Msg: "random: " + err.Error()})
	}
	return string(b)
}

func UUID4(_ Value) Value {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(&ArkError{Msg: "random: " + err.Error()})
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// ---------------------------------------------------------------- number formatting

func FormatFixed(digits, x Value) Value {
	return strconv.FormatFloat(x.(float64), 'f', int(max(digits.(int64), 0)), 64)
}

func FormatScientific(digits, x Value) Value {
	return strconv.FormatFloat(x.(float64), 'e', int(max(digits.(int64), 0)), 64)
}

func FormatIntBase(base, n Value) Value {
	b := base.(int64)
	if b < 2 || b > 36 {
		panic(&ArkError{Msg: "base must be between 2 and 36"})
	}
	return strconv.FormatInt(n.(int64), int(b))
}

func ParseIntBase(base, s Value) Value {
	b := base.(int64)
	if b < 2 || b > 36 {
		panic(&ArkError{Msg: "base must be between 2 and 36"})
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(s.(string), "_", ""), int(b), 64)
	if err != nil {
		return None
	}
	return Some(n)
}

// ---------------------------------------------------------------- dates and times

func unixOf(t time.Time) Value { return float64(t.UnixNano()) / 1e9 }

func parseTime(layout, s string, loc *time.Location) Value {
	t, err := time.ParseInLocation(layout, s, loc)
	if err != nil {
		return None
	}
	return Some(unixOf(t))
}

func TimeParseUTC(layout, s Value) Value { return parseTime(layout.(string), s.(string), time.UTC) }
func TimeParseLocal(layout, s Value) Value {
	return parseTime(layout.(string), s.(string), time.Local)
}

// TimeParseIso parses RFC 3339 (with an optional fraction of a second).
func TimeParseIso(s Value) Value { return parseTime(time.RFC3339Nano, s.(string), time.UTC) }

// TimeFromParts builds a time from (year, month, day, hour, minute,
// second) in UTC; out-of-range values are normalized (month 13 is January
// of the next year).
func TimeFromParts(parts Value) Value {
	p := parts.(*DN).F
	n := func(i int) int { return int(p[i].(int64)) }
	return unixOf(time.Date(n(0), time.Month(n(1)), n(2), n(3), n(4), n(5), 0, time.UTC))
}

func TimeWeekday(secs Value) Value   { return int64(timeOf(secs).UTC().Weekday()) }
func TimeDayOfYear(secs Value) Value { return int64(timeOf(secs).UTC().YearDay()) }

// TimeLocalOffset is the offset of local time from UTC in seconds.
func TimeLocalOffset(secs Value) Value {
	_, off := timeOf(secs).Local().Zone()
	return int64(off)
}

func TimeAddMonths(n, secs Value) Value {
	return unixOf(timeOf(secs).UTC().AddDate(0, int(n.(int64)), 0))
}
