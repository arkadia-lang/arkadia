package rt

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// ---------------------------------------------------------------- errors

// CatchError calls f () and turns a runtime error raised inside it into
// Err message. Other panics (exit, effect control flow) pass through.
func CatchError(ev *Ev, f Value) (result Value) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *ArkError:
				result = Err(e.Msg)
			case runtime.Error:
				result = Err(e.Error())
			default:
				panic(r)
			}
		}
	}()
	return Ok(Apply1(ev, f, Unit))
}

// ---------------------------------------------------------------- TCP

// Listener is a TCP listener.
type Listener struct{ L net.Listener }

// Conn is a TCP connection with a buffered reader for line input.
type Conn struct {
	C        net.Conn
	R        *bufio.Reader
	deadline time.Time // set by NetSetTimeout
}

func newConn(c net.Conn) *Conn { return &Conn{C: c, R: bufio.NewReader(c)} }

func errString(err error) Value { return Err(err.Error()) }

func NetListen(addr Value) Value {
	l, err := net.Listen("tcp", addr.(string))
	if err != nil {
		return errString(err)
	}
	return Ok(&Listener{L: l})
}

// The blocking operations on sockets are cancellation points: cancelling
// the computation sets a deadline in the past, which makes them return,
// and then puts back the deadline of the program.

type deadliner interface{ SetDeadline(time.Time) error }

func NetAccept(ev *Ev, l Value) Value {
	Flush()
	ln := l.(*Listener).L
	var c net.Conn
	var err error
	interrupt, restore := func() {}, func() {}
	if d, ok := ln.(deadliner); ok {
		interrupt = func() { d.SetDeadline(time.Unix(1, 0)) }
		restore = func() { d.SetDeadline(time.Time{}) }
	}
	interruptible(ev, interrupt, func() { c, err = ln.Accept() }, restore)
	if err != nil {
		return errString(err)
	}
	return Ok(newConn(c))
}

func NetListenerAddr(l Value) Value { return l.(*Listener).L.Addr().String() }

func NetCloseListener(l Value) Value {
	l.(*Listener).L.Close()
	return Unit
}

func NetConnect(ev *Ev, addr, timeout Value) Value {
	ctx, cancel := context.WithTimeout(context.Background(), seconds(timeout))
	defer cancel()
	var c net.Conn
	var err error
	var d net.Dialer
	interruptible(ev, cancel, func() { c, err = d.DialContext(ctx, "tcp", addr.(string)) }, func() {
		if c != nil {
			c.Close()
		}
	})
	if err != nil {
		return errString(err)
	}
	return Ok(newConn(c))
}

// connIO runs a blocking operation on a connection as a cancellation
// point.
func connIO(ev *Ev, c *Conn, op func()) {
	interruptible(ev, func() { c.C.SetDeadline(time.Unix(1, 0)) }, op, func() { c.C.SetDeadline(c.deadline) })
}

// NetRead reads up to n bytes; the empty string means end of input.
func NetRead(ev *Ev, cv, n Value) Value {
	Flush()
	c := cv.(*Conn)
	buf := make([]byte, max(n.(int64), 1))
	var k int
	var err error
	connIO(ev, c, func() { k, err = c.R.Read(buf) })
	if k == 0 && err != nil && !errors.Is(err, io.EOF) {
		return errString(err)
	}
	return Ok(string(buf[:k]))
}

// NetReadAll reads until the peer closes the connection.
func NetReadAll(ev *Ev, cv Value) Value {
	Flush()
	c := cv.(*Conn)
	var b []byte
	var err error
	connIO(ev, c, func() { b, err = io.ReadAll(c.R) })
	if err != nil {
		return errString(err)
	}
	return Ok(string(b))
}

// NetReadLine reads a line without its "\n" or "\r\n"; None at end of input.
func NetReadLine(ev *Ev, cv Value) Value {
	Flush()
	c := cv.(*Conn)
	var s string
	var err error
	connIO(ev, c, func() { s, err = c.R.ReadString('\n') })
	if err != nil && !errors.Is(err, io.EOF) {
		return errString(err)
	}
	if err != nil && s == "" {
		return Ok(None)
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r")
	return Ok(Some(s))
}

func NetWrite(ev *Ev, cv, s Value) Value {
	c := cv.(*Conn)
	var err error
	connIO(ev, c, func() { _, err = io.WriteString(c.C, s.(string)) })
	if err != nil {
		return errString(err)
	}
	return Ok(Unit)
}

func NetClose(c Value) Value {
	c.(*Conn).C.Close()
	return Unit
}

// NetCloseWrite shuts down the sending side so the peer sees end of input.
func NetCloseWrite(c Value) Value {
	if tc, ok := c.(*Conn).C.(*net.TCPConn); ok {
		tc.CloseWrite()
	}
	return Unit
}

// NetSetTimeout sets a deadline for reads and writes, in seconds from now;
// zero or less removes it.
func NetSetTimeout(cv, secs Value) Value {
	c := cv.(*Conn)
	var t time.Time
	if secs.(float64) > 0 {
		t = time.Now().Add(seconds(secs))
	}
	c.deadline = t
	c.C.SetDeadline(t)
	return Unit
}

func NetLocalAddr(c Value) Value  { return c.(*Conn).C.LocalAddr().String() }
func NetRemoteAddr(c Value) Value { return c.(*Conn).C.RemoteAddr().String() }

func NetLookupHost(host Value) Value {
	addrs, err := net.LookupHost(host.(string))
	if err != nil {
		return errString(err)
	}
	return Ok(stringList(addrs))
}

// NetSplitHostPort splits "host:port".
func NetSplitHostPort(addr Value) Value {
	h, p, err := net.SplitHostPort(addr.(string))
	if err != nil {
		return errString(err)
	}
	return Ok(Tuple(h, p))
}

func NetJoinHostPort(host, port Value) Value { return net.JoinHostPort(host.(string), port.(string)) }

func seconds(v Value) time.Duration { return time.Duration(v.(float64) * float64(time.Second)) }

func stringList(xs []string) Value {
	vs := make([]Value, len(xs))
	for i, x := range xs {
		vs[i] = x
	}
	return SliceToList(vs)
}

// ---------------------------------------------------------------- URLs

func UrlQueryEscape(s Value) Value { return url.QueryEscape(s.(string)) }
func UrlPathEscape(s Value) Value  { return url.PathEscape(s.(string)) }

func UrlQueryUnescape(s Value) Value {
	r, err := url.QueryUnescape(s.(string))
	if err != nil {
		return None
	}
	return Some(r)
}

func UrlPathUnescape(s Value) Value {
	r, err := url.PathUnescape(s.(string))
	if err != nil {
		return None
	}
	return Some(r)
}

// UrlParse splits a URL into (scheme, user info, host, port, path, raw
// query, fragment).
func UrlParse(s Value) Value {
	u, err := url.Parse(s.(string))
	if err != nil {
		return errString(err)
	}
	user := ""
	if u.User != nil {
		user = u.User.String()
	}
	return Ok(Tuple(u.Scheme, user, u.Hostname(), u.Port(), u.Path, u.RawQuery, u.Fragment))
}

// UrlResolve resolves a reference against a base URL.
func UrlResolve(base, ref Value) Value {
	b, err := url.Parse(base.(string))
	if err != nil {
		return errString(err)
	}
	r, err := url.Parse(ref.(string))
	if err != nil {
		return errString(err)
	}
	return Ok(b.ResolveReference(r).String())
}
