package rt

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------- errors

// CatchError calls f () and turns a runtime error raised inside it into
// Err message. Other panics (exit, effect control flow) pass through.
func CatchError(f Value) (result Value) {
	fb := peekFiber()
	var saved *frame
	if fb != nil {
		saved = fb.cur
	}
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
			if fb != nil {
				fb.cur = saved
			}
		}
	}()
	return Ok(Apply1(f, Unit))
}

// ---------------------------------------------------------------- TCP

// Listener is a TCP listener.
type Listener struct{ L net.Listener }

// Conn is a TCP connection with a buffered reader for line input.
type Conn struct {
	C net.Conn
	R *bufio.Reader
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

func NetAccept(l Value) Value {
	Flush()
	c, err := l.(*Listener).L.Accept()
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

func NetConnect(addr, timeout Value) Value {
	c, err := net.DialTimeout("tcp", addr.(string), seconds(timeout))
	if err != nil {
		return errString(err)
	}
	return Ok(newConn(c))
}

// NetRead reads up to n bytes; the empty string means end of input.
func NetRead(c, n Value) Value {
	Flush()
	buf := make([]byte, max(n.(int64), 1))
	k, err := c.(*Conn).R.Read(buf)
	if k == 0 && err != nil && !errors.Is(err, io.EOF) {
		return errString(err)
	}
	return Ok(string(buf[:k]))
}

// NetReadAll reads until the peer closes the connection.
func NetReadAll(c Value) Value {
	Flush()
	b, err := io.ReadAll(c.(*Conn).R)
	if err != nil {
		return errString(err)
	}
	return Ok(string(b))
}

// NetReadLine reads a line without its "\n" or "\r\n"; None at end of input.
func NetReadLine(c Value) Value {
	Flush()
	s, err := c.(*Conn).R.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return errString(err)
	}
	if err != nil && s == "" {
		return Ok(None)
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r")
	return Ok(Some(s))
}

func NetWrite(c, s Value) Value {
	if _, err := io.WriteString(c.(*Conn).C, s.(string)); err != nil {
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
func NetSetTimeout(c, secs Value) Value {
	var t time.Time
	if secs.(float64) > 0 {
		t = time.Now().Add(seconds(secs))
	}
	c.(*Conn).C.SetDeadline(t)
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

// ---------------------------------------------------------------- HTTP server

// The Go HTTP server accepts connections and reads requests on its own
// goroutines and queues them; the Arkadia program takes them with HttpNext
// and answers with HttpRespond, which hands the response back to the
// connection's goroutine. Handlers thus run in the tasks of the program —
// one at a time in a loop (serve), or each in a task of its own
// (serveParallel) — while slow clients only occupy Go goroutines.

// HttpServer is a running HTTP server.
type HttpServer struct {
	srv    *http.Server
	addr   string
	queue  chan *Exchange
	closed chan struct{}
	once   sync.Once
}

// Exchange is one request waiting for its response.
type Exchange struct {
	req  *http.Request
	body string
	resp chan httpResponse
	done bool
}

type httpResponse struct {
	status  int
	headers [][2]string
	body    string
}

// MaxBodyBytes limits the size of request bodies.
const MaxBodyBytes = 32 << 20

func HttpListen(addr Value) Value {
	l, err := net.Listen("tcp", addr.(string))
	if err != nil {
		return errString(err)
	}
	s := &HttpServer{addr: l.Addr().String(), queue: make(chan *Exchange), closed: make(chan struct{})}
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 30 * time.Second}
	go s.srv.Serve(l)
	return Ok(s)
}

func (s *HttpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	ex := &Exchange{req: r, body: string(b), resp: make(chan httpResponse, 1)}
	select {
	case s.queue <- ex:
	case <-s.closed:
		http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
		return
	case <-r.Context().Done():
		return
	}
	select {
	case resp := <-ex.resp:
		h := w.Header()
		for _, kv := range resp.headers {
			h.Add(kv[0], kv[1])
		}
		w.WriteHeader(resp.status)
		io.WriteString(w, resp.body)
	case <-r.Context().Done():
	}
}

// HttpNext waits for the next request: Some (exchange, method, path, raw
// query, headers, body, remote address), or None once the server is closed.
func HttpNext(sv Value) Value {
	s := sv.(*HttpServer)
	Flush()
	fb := peekFiber()
	for {
		if fb != nil {
			fb.poll()
		}
		cancel, kick := fb.interrupts()
		select {
		case ex := <-s.queue:
			r := ex.req
			return Some(Tuple(ex, r.Method, r.URL.Path, r.URL.RawQuery, headerList(r.Header, r.Host), ex.body, r.RemoteAddr))
		case <-s.closed:
			return None
		case <-cancel:
		case <-kick:
		}
	}
}

// headerList flattens headers into (name, value) pairs sorted by name.
// Go keeps the Host header apart from the others; it is put back here.
func headerList(h http.Header, host string) Value {
	names := make([]string, 0, len(h)+1)
	for k := range h {
		names = append(names, k)
	}
	if host != "" {
		names = append(names, "Host")
	}
	sort.Strings(names)
	var out []Value
	for _, k := range names {
		if k == "Host" && h.Get("Host") == "" {
			out = append(out, Tuple("Host", host))
			continue
		}
		for _, v := range h[k] {
			out = append(out, Tuple(k, v))
		}
	}
	return SliceToList(out)
}

func pairs(l Value) [][2]string {
	var out [][2]string
	for _, p := range ListToSlice(l) {
		d := p.(*D2)
		out = append(out, [2]string{d.A.(string), d.B.(string)})
	}
	return out
}

// HttpNoExchange stands for the exchange of a request built in the program
// (for testing handlers); responding to it does nothing.
func HttpNoExchange() Value { return (*Exchange)(nil) }

// HttpRespond sends the response of an exchange. Only the first response
// of an exchange is sent.
func HttpRespond(exv, status, headers, body Value) Value {
	ex := exv.(*Exchange)
	if ex == nil || ex.done {
		return Unit
	}
	ex.done = true
	ex.resp <- httpResponse{status: int(status.(int64)), headers: pairs(headers), body: body.(string)}
	return Unit
}

func HttpServerAddr(s Value) Value { return s.(*HttpServer).addr }

// HttpClose stops the server: HttpNext returns None from now on.
func HttpClose(sv Value) Value {
	s := sv.(*HttpServer)
	s.once.Do(func() {
		close(s.closed)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		go func() {
			s.srv.Shutdown(ctx)
		}()
	})
	return Unit
}

// ---------------------------------------------------------------- HTTP client

var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	},
}

// HttpFetch performs a request: Ok (status, headers, body) or Err message.
func HttpFetch(method, rawURL, headers, body, timeout Value) Value {
	Flush()
	ctx := context.Background()
	if t := timeout.(float64); t > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, seconds(timeout))
		defer cancel()
	}
	var rd io.Reader
	if b := body.(string); b != "" {
		rd = strings.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method.(string), rawURL.(string), rd)
	if err != nil {
		return errString(err)
	}
	for _, kv := range pairs(headers) {
		if strings.EqualFold(kv[0], "Host") {
			req.Host = kv[1]
			continue
		}
		req.Header.Add(kv[0], kv[1])
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return errString(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return errString(err)
	}
	return Ok(Tuple(int64(resp.StatusCode), headerList(resp.Header, ""), string(b)))
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
