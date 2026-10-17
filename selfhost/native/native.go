// Primitives used by the self-hosted compiler that the standard library
// does not provide: mutable references, Go-compatible number and path
// handling, and Go source formatting.
package main

import (
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

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

func stringList(l rt.Value) []string {
	var out []string
	for _, v := range rt.ListToSlice(l) {
		out = append(out, v.(string))
	}
	return out
}

// NativeRunIn runs a program in a directory with extra environment
// variables and returns (exit code, combined output).
func NativeRunIn(dir, name, args, env rt.Value) rt.Value {
	rt.Flush()
	cmd := exec.Command(name.(string), stringList(args)...)
	cmd.Dir = dir.(string)
	cmd.Env = append(os.Environ(), stringList(env)...)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	code := int64(0)
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = int64(ee.ExitCode())
		} else {
			return rt.Err(err.Error())
		}
	}
	return rt.Ok(rt.Tuple(code, out.String()))
}

// NativeExec runs a program with the standard streams of this process
// and extra environment variables, returning its exit code.
func NativeExec(name, args, env rt.Value) rt.Value {
	rt.Flush()
	cmd := exec.Command(name.(string), stringList(args)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), stringList(env)...)
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return rt.Ok(int64(ee.ExitCode()))
		}
		return rt.Err(err.Error())
	}
	return rt.Ok(int64(0))
}

// NativeLookPath finds an executable in PATH.
func NativeLookPath(name rt.Value) rt.Value {
	p, err := exec.LookPath(name.(string))
	if err != nil {
		return rt.None
	}
	return rt.Some(p)
}

// NativeStderrIsTerminal reports whether standard error is a terminal.
func NativeStderrIsTerminal(_ rt.Value) rt.Value {
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
