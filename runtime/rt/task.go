package rt

import (
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Tasks: Arkadia computations running in parallel.
//
// Each task runs on a goroutine of its own (and on more goroutines while it
// runs general effect handlers, see effect.go) with its own evidence. The
// Go scheduler runs the goroutines on all processors and preempts those
// that run for long, so a busy task never starves the others, and a task
// blocked on I/O does not hold a thread.
//
// Tasks are structured: runAsync opens a task scope (a nursery) and spawn
// starts tasks in the innermost one, found through the evidence. A scope
// ends when its body and all its tasks have finished. When a task fails
// (a runtime error), the scope is cancelled and runAsync raises the error
// once everything in it has finished.
//
// Cancellation is asynchronous but cooperative. Cancelling a task or a
// scope marks its cancellation scope and the scopes nested in it. The
// computation is unwound (with a cancelSignal panic) at its next
// cancellation point: the generated code polls at the entry of functions
// that call others and in loops, and blocking operations of the runtime
// wait for the cancellation along with their event. Unwinding runs the
// cleanups of `finally` and stops at the outermost cancelled scope of the
// task: a timeout returns None, a cancelled task finishes as Cancelled.

// Task is a running computation started by spawn. The program runs in the
// main task.
type Task struct {
	cs      *cancelScope  // the root cancellation scope of the task
	done    chan struct{} // closed when the task has finished
	outcome int           // how it finished, valid once done is closed
	result  Value         // the value of a completed task
	err     any           // the panic of a failed task
}

// How a task finished. Completed and cancelled tasks are reported by
// AwaitOutcome as the constructors of Outcome in Std.Task.
const (
	completed = iota
	cancelled
	failed
)

// newTaskEv returns the evidence at the root of a task that belongs to no
// scope.
func newTaskEv() *Ev {
	t := &Task{cs: newScope(nil, true), done: make(chan struct{})}
	return &Ev{eff: -1, task: t, cs: t.cs}
}

// ---------------------------------------------------------------- cancellation scopes

// cancelScope is a region of a task that is cancelled as a whole: a task,
// a task scope, the body of a timeout. Cancelling a scope cancels the
// scopes nested in it, including the root scopes of the tasks started in
// it, but not shielded scopes.
type cancelScope struct {
	parent    *cancelScope  // the scope it inherits cancellation from, or nil
	root      bool          // the root scope of a task: unwinding stops here
	cancelled atomic.Bool   // set once, by cancel
	done      chan struct{} // closed by cancel
	mu        sync.Mutex
	children  map[*cancelScope]struct{} // open scopes that inherit from it
	hooks     map[*func()]struct{}      // functions to call when it is cancelled
}

// newScope opens a scope that inherits the cancellation of parent (nil
// for none).
func newScope(parent *cancelScope, root bool) *cancelScope {
	s := &cancelScope{parent: parent, root: root, done: make(chan struct{})}
	if parent != nil {
		parent.mu.Lock()
		if parent.cancelled.Load() {
			parent.mu.Unlock()
			s.cancel()
		} else {
			if parent.children == nil {
				parent.children = map[*cancelScope]struct{}{}
			}
			parent.children[s] = struct{}{}
			parent.mu.Unlock()
		}
	}
	return s
}

// cancel cancels the scope and the scopes nested in it.
func (s *cancelScope) cancel() {
	if !s.cancelled.CompareAndSwap(false, true) {
		return
	}
	close(s.done)
	s.mu.Lock()
	children, hooks := s.children, s.hooks
	s.children, s.hooks = nil, nil
	s.mu.Unlock()
	for f := range hooks {
		(*f)()
	}
	for c := range children {
		c.cancel()
	}
}

// onCancel arranges for f to be called when the scope is cancelled (at
// once if it already is) until stop is called. Blocking operations that
// cannot wait for a channel use it to interrupt themselves.
func (s *cancelScope) onCancel(f func()) (stop func()) {
	s.mu.Lock()
	if s.cancelled.Load() {
		s.mu.Unlock()
		f()
		return func() {}
	}
	if s.hooks == nil {
		s.hooks = map[*func()]struct{}{}
	}
	key := &f
	s.hooks[key] = struct{}{}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.hooks, key)
		s.mu.Unlock()
	}
}

// interruptible runs a blocking operation that interrupt makes return
// early. If the computation is cancelled meanwhile, it calls restore (to
// undo the effect of interrupt) and unwinds. interrupt is never called
// after the operation has returned.
func interruptible(ev *Ev, interrupt func(), op func(), restore func()) {
	var mu sync.Mutex
	finished, interrupted := false, false
	stop := ev.cs.onCancel(func() {
		mu.Lock()
		if !finished {
			interrupted = true
			interrupt()
		}
		mu.Unlock()
	})
	op()
	stop()
	mu.Lock()
	finished = true
	mu.Unlock()
	if interrupted {
		restore()
		unwind(ev)
	}
}

// close ends a scope: it no longer inherits cancellation.
func (s *cancelScope) close() {
	if p := s.parent; p != nil {
		p.mu.Lock()
		delete(p.children, s)
		p.mu.Unlock()
	}
}

// cancelSignal unwinds a computation to the end of a cancelled scope.
type cancelSignal struct{ scope *cancelScope }

// Poll is a cancellation point. It costs two loads and a branch when the
// computation is not cancelled.
func Poll(ev *Ev) {
	if ev.cs.cancelled.Load() {
		unwind(ev)
	}
}

// unwind unwinds a cancelled computation to its outermost cancelled scope
// in the current task. It is kept out of line so that Poll inlines to a
// load and a branch.
//
//go:noinline
func unwind(ev *Ev) {
	s := ev.cs
	for !s.root && s.parent != nil && s.parent.cancelled.Load() {
		s = s.parent
	}
	panic(&cancelSignal{scope: s})
}

// block waits until ch is closed, unless the computation is cancelled
// first.
func block(ev *Ev, ch <-chan struct{}) {
	select {
	case <-ch:
	case <-ev.cs.done:
		unwind(ev)
	}
}

// withScope returns evidence on top of ev whose cancellation scope is s.
func withScope(ev *Ev, s *cancelScope) *Ev {
	c := ev.child()
	c.cs = s
	return c
}

// try calls f and returns its result, or the panic that unwound it.
func try(f func() Value) (v Value, r any) {
	defer func() {
		if p := recover(); p != nil {
			r = withTrace(p)
		}
	}()
	return f(), nil
}

// ---------------------------------------------------------------- task scopes

// nursery is a task scope: the tasks started by spawn in a runAsync.
type nursery struct {
	cs  *cancelScope
	wg  sync.WaitGroup // the running tasks
	mu  sync.Mutex
	err any // the first failure of a task
}

// fail records the failure of a task and cancels the scope.
func (n *nursery) fail(err any) {
	n.mu.Lock()
	if n.err == nil {
		n.err = err
	}
	n.mu.Unlock()
	n.cs.cancel()
}

// RunAsync runs body () in a new task scope and waits for the tasks
// started in it. If the body or a task fails, the scope is cancelled and
// the failure is raised once all the tasks have finished.
func RunAsync(ev *Ev, body Value) Value {
	n := &nursery{cs: newScope(ev.cs, false)}
	nev := withScope(ev, n.cs)
	nev.nursery = n
	v, r := try(func() Value { return Apply1(nev, body, Unit) })
	if r != nil {
		n.cs.cancel()
	}
	n.wg.Wait()
	n.cs.close()
	n.mu.Lock()
	err := n.err
	n.mu.Unlock()
	if r != nil {
		if c, ok := r.(*cancelSignal); ok && c.scope == n.cs && err != nil {
			// the body was cancelled because a task failed
			panic(err)
		}
		panic(r)
	}
	if err != nil {
		panic(err)
	}
	return v
}

// Spawn starts a task running f () in the innermost task scope.
func Spawn(ev *Ev, f Value) Value {
	n := ev.nursery
	if n == nil {
		panic(&ArkError{Msg: "spawn outside of runAsync"})
	}
	t := &Task{done: make(chan struct{})}
	t.cs = newScope(n.cs, true)
	tev := &Ev{eff: -1, task: t, cs: t.cs, nursery: n}
	n.wg.Add(1)
	go t.run(tev, f, n)
	return t
}

func (t *Task) run(ev *Ev, f Value, n *nursery) {
	defer n.wg.Done()
	v, r := try(func() Value { return Apply1(ev, f, Unit) })
	t.cs.close()
	c, _ := r.(*cancelSignal)
	switch {
	case r == nil:
		t.outcome, t.result = completed, v
	case c != nil && c.scope == t.cs:
		t.outcome = cancelled
	default:
		t.outcome, t.err = failed, r
	}
	close(t.done)
	if t.outcome == failed {
		n.fail(r)
	}
}

// Await waits for a task and returns its value. The failure of the task is
// raised again; awaiting a cancelled task is a runtime error.
func Await(ev *Ev, tv Value) Value {
	t := tv.(*Task)
	block(ev, t.done)
	switch t.outcome {
	case completed:
		return t.result
	case failed:
		panic(t.err)
	}
	panic(&ArkError{Msg: "awaited a cancelled task"})
}

// AwaitOutcome waits for a task: Completed value or Cancelled. The failure
// of the task is raised again.
func AwaitOutcome(ev *Ev, tv Value) Value {
	t := tv.(*Task)
	block(ev, t.done)
	switch t.outcome {
	case completed:
		return &D1{T: completed, A: t.result}
	case failed:
		panic(t.err)
	}
	return Con0(cancelled)
}

// FirstDone waits for the first of a list of tasks to finish and returns
// it.
func FirstDone(ev *Ev, tasks Value) Value {
	ts := ListToSlice(tasks)
	if len(ts) == 0 {
		panic(&ArkError{Msg: "firstDone: no tasks"})
	}
	cases := make([]reflect.SelectCase, len(ts)+1)
	for i, t := range ts {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(t.(*Task).done)}
	}
	cases[len(ts)] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ev.cs.done)}
	i, _, _ := reflect.Select(cases)
	if i == len(ts) {
		unwind(ev)
	}
	return ts[i]
}

// CancelTask requests the cancellation of a task and returns at once.
func CancelTask(tv Value) Value {
	tv.(*Task).cs.cancel()
	return Unit
}

// TaskDone reports whether a task has finished.
func TaskDone(tv Value) Value {
	select {
	case <-tv.(*Task).done:
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------- timeouts and shields

// Timeout runs f () and returns Some result, or None when it has not
// finished after secs seconds: it is then cancelled.
func Timeout(ev *Ev, secs, f Value) Value {
	s := newScope(ev.cs, false)
	timer := time.AfterFunc(seconds(secs), s.cancel)
	v, r := try(func() Value { return Apply1(withScope(ev, s), f, Unit) })
	timer.Stop()
	s.close()
	if r != nil {
		if c, ok := r.(*cancelSignal); ok && c.scope == s {
			return None
		}
		panic(r)
	}
	return Some(v)
}

// CancelOnInterrupt runs f () and cancels it when the program is
// interrupted (Ctrl-C) or asked to terminate: Some result, or None when it
// was cancelled by the signal. After the first signal, a second one stops
// the program at once.
func CancelOnInterrupt(ev *Ev, f Value) Value {
	s := newScope(ev.cs, false)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigs:
			signal.Stop(sigs)
			s.cancel()
		case <-done:
		}
	}()
	v, r := try(func() Value { return Apply1(withScope(ev, s), f, Unit) })
	signal.Stop(sigs)
	close(done)
	s.close()
	if r != nil {
		if c, ok := r.(*cancelSignal); ok && c.scope == s {
			return None
		}
		panic(r)
	}
	return Some(v)
}

// shielded returns evidence on top of ev that is not cancelled with it.
func shielded(ev *Ev) *Ev {
	return withScope(ev, newScope(nil, false))
}

// Uncancellable runs f () protected from the cancellation of the current
// task and scopes. A cancellation that arrives meanwhile is delivered when
// it returns.
func Uncancellable(ev *Ev, f Value) Value {
	v := Apply1(shielded(ev), f, Unit)
	Poll(ev)
	return v
}

// Finally runs body () and then cleanup (), protected from cancellation,
// however body ends: with a value, a failure, a cancellation or an effect
// that discards its continuation.
func Finally(ev *Ev, body, cleanup Value) Value {
	v, r := try(func() Value { return Apply1(ev, body, Unit) })
	Apply1(shielded(ev), cleanup, Unit)
	if r != nil {
		panic(r)
	}
	return v
}

// Sleep waits for secs seconds, or until the computation is cancelled.
func Sleep(ev *Ev, secs Value) Value {
	Flush()
	d := seconds(secs)
	if d <= 0 {
		Poll(ev)
		return Unit
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ev.cs.done:
		unwind(ev)
	}
	return Unit
}

// NumProcessors is the number of processors that run tasks in parallel.
func NumProcessors(_ Value) Value { return int64(runtime.GOMAXPROCS(0)) }
