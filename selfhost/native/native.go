// Primitives used by the self-hosted compiler that the standard library
// does not provide: mutable references, Go-compatible number and path
// handling, and Go source formatting.
package main

import (
	"go/format"
	"os"
	"path/filepath"
	"strconv"

	rt "arkprog/rt"
)

type nativeRef struct{ v rt.Value }

func NativeRefNew(v rt.Value) rt.Value    { return &nativeRef{v} }
func NativeRefGet(r rt.Value) rt.Value    { return r.(*nativeRef).v }
func NativeRefSet(r, v rt.Value) rt.Value { r.(*nativeRef).v = v; return rt.Unit }

// NativeTag returns the constructor tag of a data value.
func NativeTag(v rt.Value) rt.Value { return int64(rt.TagOf(v)) }

// NativeParseInt parses an integer literal with Go's base prefixes.
func NativeParseInt(s rt.Value) rt.Value {
	n, err := strconv.ParseInt(s.(string), 0, 64)
	if err != nil {
		return rt.None
	}
	return rt.Some(n)
}

// NativeParseFloat parses a float literal.
func NativeParseFloat(s rt.Value) rt.Value {
	f, err := strconv.ParseFloat(s.(string), 64)
	if err != nil {
		return rt.None
	}
	return rt.Some(f)
}

// NativeFormatFloat formats a float like strconv.FormatFloat(f, 'g', -1, 64).
func NativeFormatFloat(f rt.Value) rt.Value { return strconv.FormatFloat(f.(float64), 'g', -1, 64) }

// NativeFormatGo formats Go source code.
func NativeFormatGo(src rt.Value) rt.Value {
	out, err := format.Source([]byte(src.(string)))
	if err != nil {
		return rt.Err(err.Error())
	}
	return rt.Ok(string(out))
}

func NativePathJoin(a, b rt.Value) rt.Value { return filepath.Join(a.(string), b.(string)) }
func NativePathDir(p rt.Value) rt.Value     { return filepath.Dir(p.(string)) }
func NativePathBase(p rt.Value) rt.Value    { return filepath.Base(p.(string)) }
func NativePathIsAbs(p rt.Value) rt.Value   { return filepath.IsAbs(p.(string)) }

func NativePathAbs(p rt.Value) rt.Value {
	a, err := filepath.Abs(p.(string))
	if err != nil {
		return p
	}
	return a
}

func NativeIsDir(p rt.Value) rt.Value {
	fi, err := os.Stat(p.(string))
	return err == nil && fi.IsDir()
}

// NativeExecutable returns the path of the running executable.
func NativeExecutable(_ rt.Value) rt.Value {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

// NativeUserCacheDir returns the user's cache directory.
func NativeUserCacheDir(_ rt.Value) rt.Value {
	d, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return d
}
