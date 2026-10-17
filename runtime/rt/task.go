package rt

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Tasks: Arkadia computations running in parallel, each on a goroutine of
// its own (and on more goroutines when it runs handlers, see effect.go).
// The Go scheduler runs them on all processors and preempts long-running
// ones, so a busy task never starves the others.
//
// Tasks are forked in scopes (structured concurrency): a scope ends when
// its body and all the tasks forked in it have finished. When a task of a
// scope fails, the other tasks and the body are cancelled and the scope
// raises the error. A task started with spawn belongs to no scope.
//
// Cancellation is cooperative: a cancelled task is unwound at its next
// cancellation point (an effect operation, a loop iteration, a blocking
// operation of the runtime). It cannot be caught, so it always reaches the
// root of the task.

// pending counts the cancellations not yet delivered: tasks cancelled
// while running and cancelled scopes still open. While it is zero, a
// cancellation point costs one atomic load.
var pending atomic.Int64

// Poll is a cancellation point; generated code calls it in loops.
func Poll() {
	if pending.Load() != 0 {
		pollSlow()
	}
}

func pollSlow() {
	if fb := peekFiber(); fb != nil {
		fb.poll()
	}
}

// cancelSignal unwinds a cancelled task.
type cancelSignal struct{}

// scopeAbort unwinds the body of a cancelled scope.
type scopeAbort struct{ s *Scope }

// poll raises the cancellation of the task or of the outermost cancelled
// region of the goroutine.
func (fb *fiber) poll() {
	if t := fb.task; t != nil && t.cancelled.Load() {
		panic(cancelSignal{})
	}
	for _, s := range fb.regions {
		if s.cancelled.Load() {
			panic(&scopeAbort{s})
		}
	}
}

// interrupts returns the channels a blocking operation of fb waits on
// besides its own: they are ready when fb may have been cancelled, and
// the operation then calls fb.poll. They are nil (never ready) for a
// goroutine without a fiber.
func (fb *fiber) interrupts() (cancel, kick <-chan struct{}) {
	if fb == nil {
		return nil, nil
	}
	if fb.task != nil {
		cancel = fb.task.cancelCh
	}
	return cancel, fb.kick
}

// waitOn blocks until ready is closed, delivering cancellation.
func waitOn(fb *fiber, ready <-chan struct{}) {
	for {
		if fb != nil {
			fb.poll()
		}
		cancel, kick := fb.interrupts()
		select {
		case <-ready:
			return
		case <-cancel:
		case <-kick:
		}
	}
}

// ---------------------------------------------------------------- tasks

// Task is a running or finished task.
type Task struct {
	done      chan struct{} // closed when the task has finished
	cancelled atomic.Bool
	cancelCh  chan struct{} // closed when the task is cancelled
	scope     *Scope        // the scope the task was forked in (nil for spawn)

	mu       sync.Mutex
	finished bool
	val      Value
	err      *ArkError // the error the task failed with
	aborted  bool      // the task was unwound by its cancellation
	scopes   map[*Scope]struct{}
}

func newTask(s *Scope) *Task {
	return &Task{done: make(chan struct{}), cancelCh: make(chan struct{}), scope: s}
}

// start runs body as the task t.
func (t *Task) start(body func() Value) {
	goFiber(&fiber{task: t}, func(fb *fiber) {
		var v Value
		defer func() { t.finish(v, recover()) }()
		v = body()
	})
}

// finish records the outcome of t: its value, or the panic it ended with.
func (t *Task) finish(v Value, r any) {
	var err *ArkError
	aborted := false
	switch e := r.(type) {
	case nil:
	case cancelSignal:
		aborted = true
	case *ExitSignal:
		exitProgram(e.Code)
	default:
		err = asArkError(r)
	}
	t.mu.Lock()
	t.finished = true
	t.val, t.err, t.aborted = v, err, aborted
	if t.cancelled.Load() {
		pending.Add(-1)
	}
	t.mu.Unlock()
	close(t.done)
	if t.scope != nil {
		t.scope.childDone(t)
	}
}

// asArkError converts the panic that ended a task into the error it
// raises where the task is awaited.
func asArkError(r any) *ArkError {
	switch e := withTrace(r).(type) {
	case *ArkError:
		return e
	case runtime.Error:
		return &ArkError{Msg: e.Error(), Trace: arkTrace()}
	case abortSignal, *abortTo, *scopeAbort, stmRetry, stmConflict:
		return &ArkError{Msg: "internal error: control flow escaped a task", Trace: arkTrace()}
	default:
		return &ArkError{Msg: fmt.Sprint(r), Trace: arkTrace()}
	}
}

// cancel requests the cancellation of t and of the tasks forked in its
// scopes.
func (t *Task) cancel() {
	t.mu.Lock()
	if t.finished || t.cancelled.Load() {
		t.mu.Unlock()
		return
	}
	t.cancelled.Store(true)
	pending.Add(1)
	close(t.cancelCh)
	scopes := make([]*Scope, 0, len(t.scopes))
	for s := range t.scopes {
		scopes = append(scopes, s)
	}
	t.mu.Unlock()
	for _, s := range scopes {
		s.cancelChildren()
	}
}

// outcome waits for t (delivering the cancellation of the waiting task)
// and returns its value or its error.
func (t *Task) outcome() (Value, *ArkError) {
	waitOn(peekFiber(), t.done)
	if t.err != nil {
		return nil, t.err
	}
	if t.aborted {
		return nil, &ArkError{Msg: "the task was cancelled"}
	}
	return t.val, nil
}

// ---------------------------------------------------------------- scopes

// Scope is a group of tasks that ends when all of them have finished. Its
// body runs on the goroutine that opened it; while the body runs, the
// scope is one of the cancellable regions of that goroutine.
type Scope struct {
	fb        *fiber
	owner     *Task
	cancelled atomic.Bool
	changed   chan struct{} // signalled when a task of the scope finishes

	mu      sync.Mutex
	closed  bool
	live    map[*Task]struct{}
	failure *ArkError // the first error of a task of the scope
}

// fork starts a task in s.
func (s *Scope) fork(body func() Value) *Task {
	t := newTask(s)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		panic(&ArkError{Msg: "fork: the scope has already ended"})
	}
	s.live[t] = struct{}{}
	cancelled := s.cancelled.Load() || s.owner != nil && s.owner.cancelled.Load()
	s.mu.Unlock()
	t.start(body)
	if cancelled {
		t.cancel()
	}
	return t
}

func (s *Scope) childDone(t *Task) {
	s.mu.Lock()
	delete(s.live, t)
	failed := t.err != nil && s.failure == nil
	if failed {
		s.failure = t.err
	}
	s.mu.Unlock()
	select {
	case s.changed <- struct{}{}:
	default:
	}
	if failed {
		s.cancel()
	}
}

// cancel cancels the tasks of s and the body of s.
func (s *Scope) cancel() {
	s.mu.Lock()
	if s.closed || s.cancelled.Load() {
		s.mu.Unlock()
		return
	}
	s.cancelled.Store(true)
	pending.Add(1)
	s.mu.Unlock()
	select {
	case s.fb.kick <- struct{}{}:
	default:
	}
	s.cancelChildren()
}

func (s *Scope) cancelChildren() {
	s.mu.Lock()
	tasks := make([]*Task, 0, len(s.live))
	for t := range s.live {
		tasks = append(tasks, t)
	}
	s.mu.Unlock()
	for _, t := range tasks {
		t.cancel()
	}
}

// waitChildren waits until all the tasks of s have finished. An
// interruptible wait delivers the cancellation of the waiting goroutine.
func (s *Scope) waitChildren(interruptible bool) {
	for {
		s.mu.Lock()
		n := len(s.live)
		s.mu.Unlock()
		if n == 0 {
			return
		}
		if !interruptible {
			<-s.changed
			continue
		}
		s.fb.poll()
		cancel, kick := s.fb.interrupts()
		select {
		case <-s.changed:
		case <-cancel:
		case <-kick:
		}
	}
}

func (s *Scope) close() {
	s.mu.Lock()
	s.closed = true
	if s.cancelled.Load() {
		pending.Add(-1)
	}
	s.mu.Unlock()
	if o := s.owner; o != nil {
		o.mu.Lock()
		delete(o.scopes, s)
		o.mu.Unlock()
	}
}

// runScope runs body in a new scope on the current goroutine, then waits
// for the tasks forked in it. If the scope is cancelled by its own cancel
// (not by a failed task), runScope returns aborted().
func runScope(body func(s *Scope) Value, aborted func() Value) (result Value) {
	fb := peekFiber()
	if fb == nil {
		return withFiber(func(*fiber) Value { return runScope(body, aborted) })
	}
	if fb.kick == nil {
		fb.kick = make(chan struct{}, 1)
	}
	s := &Scope{fb: fb, owner: fb.task, changed: make(chan struct{}, 1), live: map[*Task]struct{}{}}
	if o := s.owner; o != nil {
		o.mu.Lock()
		if o.scopes == nil {
			o.scopes = map[*Scope]struct{}{}
		}
		o.scopes[s] = struct{}{}
		o.mu.Unlock()
	}
	fb.regions = append(fb.regions, s)
	saved := fb.cur
	done := false
	defer func() {
		if done {
			return
		}
		r := withTrace(recover())
		fb.cur = saved
		fb.regions = fb.regions[:len(fb.regions)-1]
		s.cancelChildren()
		s.waitChildren(false)
		s.close()
		if r == nil {
			return // runtime.Goexit
		}
		if a, ok := r.(*scopeAbort); ok && a.s == s {
			if s.failure != nil {
				panic(s.failure)
			}
			result = aborted()
			return
		}
		panic(r)
	}()
	v := body(s)
	s.waitChildren(true)
	done = true
	fb.regions = fb.regions[:len(fb.regions)-1]
	s.close()
	if s.failure != nil {
		panic(s.failure)
	}
	return v
}

// ---------------------------------------------------------------- Std.Task

func taskOf(v Value) *Task { return v.(*Task) }

func TaskScope(body Value) Value {
	return runScope(func(s *Scope) Value { return Apply1(body, s) }, func() Value {
		panic(&ArkError{Msg: "internal error: scope cancelled"})
	})
}

func TaskFork(s, f Value) Value {
	return s.(*Scope).fork(func() Value { return Apply1(f, Unit) })
}

func TaskSpawn(f Value) Value {
	t := newTask(nil)
	t.start(func() Value { return Apply1(f, Unit) })
	return t
}

func TaskAwait(t Value) Value {
	v, err := taskOf(t).outcome()
	if err != nil {
		panic(err)
	}
	return v
}

func TaskAwaitResult(t Value) Value {
	v, err := taskOf(t).outcome()
	if err != nil {
		return Err(err.Msg)
	}
	return Ok(v)
}

// TaskCancel cancels a task and waits until it has finished.
func TaskCancel(tv Value) Value {
	t := taskOf(tv)
	t.cancel()
	waitOn(peekFiber(), t.done)
	return Unit
}

func TaskIsDone(t Value) Value {
	select {
	case <-taskOf(t).done:
		return true
	default:
		return false
	}
}

// TaskRace runs the computations in parallel and returns the result of
// the first one to finish; the others are cancelled.
func TaskRace(fs Value) Value {
	thunks := ListToSlice(fs)
	if len(thunks) == 0 {
		panic(&ArkError{Msg: "race: no computations"})
	}
	return runScope(func(s *Scope) Value {
		first := make(chan Value, len(thunks))
		for _, f := range thunks {
			s.fork(func() Value {
				v := Apply1(f, Unit)
				first <- v
				return v
			})
		}
		fb := s.fb
		for {
			fb.poll()
			cancel, kick := fb.interrupts()
			select {
			case v := <-first:
				s.cancelChildren()
				return v
			case <-cancel:
			case <-kick:
			}
		}
	}, nil)
}

// TaskTimeout runs body with a time limit: Some result, or None if the
// time ran out first (body is then cancelled).
func TaskTimeout(secs, body Value) Value {
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	return runScope(func(s *Scope) Value {
		timer = time.AfterFunc(seconds(secs), s.cancel)
		return Some(Apply1(body, Unit))
	}, func() Value { return None })
}

// Sleep pauses the current task; it is a cancellation point.
func Sleep(secs Value) Value {
	Flush()
	d := seconds(secs)
	if d <= 0 {
		Poll()
		return Unit
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	fb := peekFiber()
	for {
		if fb != nil {
			fb.poll()
		}
		cancel, kick := fb.interrupts()
		select {
		case <-timer.C:
			return Unit
		case <-cancel:
		case <-kick:
		}
	}
}

func TaskYield(_ Value) Value {
	runtime.Gosched()
	Poll()
	return Unit
}

func TaskCheckCancelled(_ Value) Value {
	pollSlow()
	return Unit
}

func TaskProcessors(_ Value) Value { return int64(runtime.GOMAXPROCS(0)) }
