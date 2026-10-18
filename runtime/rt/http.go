package rt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------- HTTP server

// The Go HTTP server accepts connections and reads requests on goroutines
// of its own. The program then uses a server in one of two ways:
//
//   - HttpServe answers every request in a task of its own that runs the
//     Arkadia handler directly on the goroutine of the connection, so
//     requests are handled in parallel on all processors. The task of a
//     request whose client goes away is cancelled.
//   - HttpNext takes the requests one at a time from a queue and
//     HttpRespond hands each response back to the goroutine of the
//     connection, for programs that answer requests in order.

// HttpServer is a running HTTP server.
type HttpServer struct {
	srv     *http.Server
	addr    string
	queue   chan *Exchange
	closed  chan struct{}
	once    sync.Once
	handler atomic.Pointer[httpHandler] // set while HttpServe runs
}

// Exchange is one request waiting for its response.
type Exchange struct {
	req  *http.Request
	body string
	resp chan httpResponse
	done atomic.Bool
}

type httpResponse struct {
	status  int
	headers [][2]string
	body    string
}

func (resp httpResponse) write(w http.ResponseWriter) {
	h := w.Header()
	for _, kv := range resp.headers {
		h.Add(kv[0], kv[1])
	}
	w.WriteHeader(resp.status)
	io.WriteString(w, resp.body)
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
	if h := s.handler.Load(); h != nil {
		h.serve(w, r, string(b))
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
		resp.write(w)
	case <-r.Context().Done():
	}
}

// requestTuple is the request passed to Arkadia code: (exchange, method,
// path, raw query, headers, body, remote address).
func requestTuple(ex *Exchange, r *http.Request, body string) Value {
	return Tuple(ex, r.Method, r.URL.Path, r.URL.RawQuery, headerList(r.Header, r.Host), body, r.RemoteAddr)
}

// httpHandler answers the requests of a server given to HttpServe.
type httpHandler struct {
	fn       Value        // request tuple -> (status, headers, body)
	cs       *cancelScope // the scope of the request tasks
	mu       sync.Mutex
	idle     *sync.Cond // signalled when no request is running
	active   int        // running requests
	stopping bool       // no more requests are accepted
	fatal    any        // a panic that stops the server (exit)
	stopped  chan struct{}
}

// serve answers a request in a new task.
func (h *httpHandler) serve(w http.ResponseWriter, r *http.Request, body string) {
	h.mu.Lock()
	if h.stopping {
		h.mu.Unlock()
		http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
		return
	}
	h.active++
	h.mu.Unlock()
	defer h.leave()

	t := &Task{done: make(chan struct{})}
	t.cs = newScope(h.cs, true)
	stop := context.AfterFunc(r.Context(), t.cs.cancel) // the client went away
	ev := &Ev{eff: -1, task: t, cs: t.cs}
	v, rec := try(func() Value { return Apply1(ev, h.fn, requestTuple(nil, r, body)) })
	stop()
	t.cs.close()
	close(t.done)
	switch e := rec.(type) {
	case nil:
		d := v.(*D3)
		httpResponse{status: int(d.A.(int64)), headers: pairs(d.B), body: d.C.(string)}.write(w)
	case *cancelSignal:
		if r.Context().Err() == nil {
			// cancelled by the server, not by the client
			http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
		}
	case *ExitSignal:
		h.stop(e)
		http.Error(w, "server is shutting down", http.StatusServiceUnavailable)
	default:
		// handlers turn runtime errors into responses; this is a failure of
		// the server itself, which keeps serving
		msg := fmt.Sprint(rec)
		if ae, ok := rec.(*ArkError); ok {
			msg = ae.Msg
		}
		Flush()
		fmt.Fprintf(os.Stderr, "http: request %s %s failed: %s\n", r.Method, r.URL.Path, msg)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func (h *httpHandler) leave() {
	h.mu.Lock()
	h.active--
	if h.active == 0 {
		h.idle.Broadcast()
	}
	h.mu.Unlock()
}

// stop makes HttpServe stop and raise r.
func (h *httpHandler) stop(r any) {
	h.mu.Lock()
	if h.fatal == nil {
		h.fatal = r
		close(h.stopped)
	}
	h.mu.Unlock()
}

// drain refuses new requests and waits for the running ones.
func (h *httpHandler) drain() {
	h.mu.Lock()
	h.stopping = true
	for h.active > 0 {
		h.idle.Wait()
	}
	h.mu.Unlock()
}

// HttpServe answers the requests of a server with fn, each in a task of
// its own, until the server is closed or the computation is cancelled (the
// running requests are then cancelled too). It returns once all the
// requests it started have finished.
func HttpServe(ev *Ev, sv, fn Value) Value {
	s := sv.(*HttpServer)
	h := &httpHandler{fn: fn, cs: newScope(ev.cs, false), stopped: make(chan struct{})}
	h.idle = sync.NewCond(&h.mu)
	if !s.handler.CompareAndSwap(nil, h) {
		panic(&ArkError{Msg: "the server is already being served"})
	}
	Flush()
	select {
	case <-s.closed:
	case <-h.stopped:
	case <-ev.cs.done:
	}
	HttpClose(s)
	h.drain()
	h.cs.close()
	if h.fatal != nil {
		panic(h.fatal)
	}
	Poll(ev)
	return Unit
}

// HttpNext waits for the next request: Some (exchange, method, path, raw
// query, headers, body, remote address), or None once the server is closed.
func HttpNext(ev *Ev, sv Value) Value {
	s := sv.(*HttpServer)
	Flush()
	select {
	case ex := <-s.queue:
		return Some(requestTuple(ex, ex.req, ex.body))
	case <-s.closed:
		return None
	case <-ev.cs.done:
		unwind(ev)
	}
	return None
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
// (for testing handlers) or answered by HttpServe; responding to it does
// nothing.
func HttpNoExchange() Value { return (*Exchange)(nil) }

// HttpRespond sends the response of an exchange. Only the first response
// of an exchange is sent.
func HttpRespond(exv, status, headers, body Value) Value {
	ex := exv.(*Exchange)
	if ex == nil || !ex.done.CompareAndSwap(false, true) {
		return Unit
	}
	ex.resp <- httpResponse{status: int(status.(int64)), headers: pairs(headers), body: body.(string)}
	return Unit
}

func HttpServerAddr(s Value) Value { return s.(*HttpServer).addr }

// HttpClose stops the server: it accepts no more requests, HttpNext
// returns None and HttpServe returns once the running requests finish.
func HttpClose(sv Value) Value {
	s := sv.(*HttpServer)
	s.once.Do(func() {
		close(s.closed)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
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
// Cancelling the computation aborts the request.
func HttpFetch(ev *Ev, method, rawURL, headers, body, timeout Value) Value {
	Flush()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if t := timeout.(float64); t > 0 {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, seconds(timeout))
		defer cancelTimeout()
	}
	defer ev.cs.onCancel(cancel)()
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
		Poll(ev)
		return errString(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		Poll(ev)
		return errString(err)
	}
	return Ok(Tuple(int64(resp.StatusCode), headerList(resp.Header, ""), string(b)))
}
