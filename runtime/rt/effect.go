package rt

import (
	"fmt"
	"sync/atomic"
)

// Algebraic effect handlers with evidence passing.
//
// Every Arkadia function receives the evidence of its caller, an *Ev: the
// innermost handler frame of the dynamic context. Frames are immutable
// links to their parent, so the evidence is a persistent stack shared by
// all the code running in its scope. Performing an operation searches the
// evidence for the innermost (unmasked) frame of the effect; installing a
// handler calls the body with a new frame on top. Nothing is global, so
// any number of computations (tasks) run in parallel, each with its own
// evidence, and no state has to be restored when a computation unwinds.
//
// Every frame also records the task running the computation and its
// innermost cancellation scope and task scope (see task.go), so that the
// generated code reaches them in constant time.
//
// A handled computation normally runs on its own goroutine. Performing an
// operation sends a message to the goroutine running the handler and blocks
// until the handler resumes the continuation. This gives deep handlers with
// one-shot continuations. The goroutines of one task pass control with
// unbuffered channel operations, so exactly one of them runs at a time.
//
// When every operation clause of a handler is tail-resumptive (it ends by
// resuming the continuation exactly once), the compiler marks the handler
// Tail and the clauses run directly in the performing goroutine instead.

// Ev is the evidence: a handler frame and the context of the computation.
type Ev struct {
	parent *Ev
	eff    int  // the handled effect; -1 for frames that handle none
	mask   bool // a mask frame hides the next handler of eff
	h      *Handler
	ch     chan msg
	state  Value // current state of a state-passing handler

	task    *Task        // the task running the computation
	cs      *cancelScope // the innermost cancellation scope
	nursery *nursery     // the innermost task scope (runAsync), or nil
}

// child returns a frame on top of ev that handles no effect.
func (ev *Ev) child() *Ev {
	return &Ev{parent: ev, eff: -1, task: ev.task, cs: ev.cs, nursery: ev.nursery}
}

const (
	msgOp = iota
	msgReturn
	msgAbort
	msgPanic
)

type msg struct {
	kind   int
	op     int
	args   []Value
	resume chan resumeMsg
	val    Value
	target *Ev
	err    any
}

type resumeMsg struct {
	val    Value
	abort  bool
	target *Ev
}

// abortSignal unwinds a computation whose continuation was discarded.
type abortSignal struct{ target *Ev }

// Handler describes a handler for one effect. Clauses receive the evidence
// of the handler's context (handlers are deep: operations performed by a
// clause go to the enclosing handlers).
type Handler struct {
	Eff int
	// Ret is the return clause (nil for the identity).
	Ret func(*Ev, Value) Value
	// Ops are the operation clauses: arguments and the continuation.
	Ops []func(ev *Ev, args []Value, k Value) Value
	// UsesK reports whether a clause mentions its continuation at all. A
	// clause that does not can never resume, so the suspended computation
	// is discarded as soon as the clause is entered.
	UsesK []bool
	// Tail is set when all clauses are tail-resumptive: clauses are called
	// with a nil continuation and return the value to resume with.
	Tail bool
	// Direct handlers run in the current goroutine: clauses that use their
	// continuation are tail-resumptive, the others abort with a panic
	// caught by Handle.
	Direct bool
	// State marks a state-passing handler (see HandleState).
	State bool
	RetS  func(ev *Ev, x, s Value) Value
	OpsS  []func(ev *Ev, args []Value, s Value) Value
}

// EffectNames is filled by generated code for error messages.
var EffectNames []string

// OpNames is filled by generated code for error messages.
var OpNames [][]string

func effName(eff int) string {
	if eff < len(EffectNames) {
		return EffectNames[eff]
	}
	return fmt.Sprint(eff)
}

// Perform performs an effect operation.
func Perform(ev *Ev, eff, op int, args []Value) Value {
	f := ev
	skip := 0
	for ; f != nil; f = f.parent {
		if f.eff != eff {
			continue
		}
		if f.mask {
			skip++
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		break
	}
	if f == nil {
		name := effName(eff)
		if eff < len(OpNames) && op < len(OpNames[eff]) {
			name += "." + OpNames[eff][op]
		}
		panic(&ArkError{Msg: "unhandled effect operation " + name})
	}
	if f.h.State {
		r := f.h.OpsS[op](f.parent, args, f.state).(*D2)
		f.state = r.B
		return r.A
	}
	if f.h.Tail || f.h.Direct {
		r := f.h.Ops[op](f.parent, args, nil)
		if f.h.Direct && !f.h.UsesK[op] {
			panic(&abortTo{f: f, v: r})
		}
		return r
	}
	rch := make(chan resumeMsg)
	f.ch <- msg{kind: msgOp, op: op, args: args, resume: rch}
	r := <-rch
	if r.abort {
		panic(abortSignal{target: r.target})
	}
	return r.val
}

// abortTo unwinds to the Handle of frame f with the result v.
type abortTo struct {
	f *Ev
	v Value
}

func handleDirect(ev *Ev, h *Handler, body func(*Ev) Value) (result Value) {
	f := &Ev{parent: ev, eff: h.Eff, h: h, task: ev.task, cs: ev.cs, nursery: ev.nursery}
	defer func() {
		if r := recover(); r != nil {
			if a, ok := r.(*abortTo); ok && a.f == f {
				result = a.v
				return
			}
			panic(r)
		}
	}()
	v := body(f)
	if h.Ret != nil {
		return h.Ret(ev, v)
	}
	return v
}

// Handle runs body under the handler h.
func Handle(ev *Ev, h *Handler, body func(*Ev) Value) Value {
	if h.Direct {
		return handleDirect(ev, h, body)
	}
	f := &Ev{parent: ev, eff: h.Eff, h: h, task: ev.task, cs: ev.cs, nursery: ev.nursery}
	if h.Tail {
		v := body(f)
		if h.Ret != nil {
			return h.Ret(ev, v)
		}
		return v
	}
	f.ch = make(chan msg)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if a, ok := r.(abortSignal); ok {
					if a.target != f {
						f.ch <- msg{kind: msgAbort, target: a.target}
					}
					return
				}
				f.ch <- msg{kind: msgPanic, err: withTrace(r)}
			}
		}()
		v := body(f)
		f.ch <- msg{kind: msgReturn, val: v}
	}()
	return h.loop(ev, f)
}

// loop serves the messages of the computation handled by frame f; ev is
// the evidence of the handler's context.
func (h *Handler) loop(ev *Ev, f *Ev) Value {
	m := <-f.ch
	switch m.kind {
	case msgAbort:
		panic(abortSignal{target: m.target})
	case msgPanic:
		panic(m.err)
	case msgReturn:
		if h.Ret != nil {
			return h.Ret(ev, m.val)
		}
		return m.val
	}
	if !h.UsesK[m.op] {
		m.resume <- resumeMsg{abort: true, target: f}
		return h.Ops[m.op](ev, m.args, nil)
	}
	var resumed atomic.Bool
	k := Fn1(func(_ *Ev, v Value) Value {
		if !resumed.CompareAndSwap(false, true) {
			panic(&ArkError{Msg: "continuation of " + effName(h.Eff) + " resumed more than once"})
		}
		m.resume <- resumeMsg{val: v}
		return h.loop(ev, f)
	})
	// A clause that unwinds (a runtime error, a cancellation, an abort to an
	// outer handler) before resuming leaves the computation suspended for
	// good: abort it so that its goroutine ends.
	defer func() {
		if resumed.Load() {
			return
		}
		if r := recover(); r != nil {
			r = withTrace(r)
			if resumed.CompareAndSwap(false, true) {
				m.resume <- resumeMsg{abort: true, target: f}
			}
			panic(r)
		}
	}()
	return h.Ops[m.op](ev, m.args, k)
}

// Mask runs body with the innermost handler of each listed effect hidden,
// so that operations performed by body reach the next outer handler.
func Mask(ev *Ev, effs []int, body func(*Ev) Value) Value {
	m := ev
	for _, e := range effs {
		m = &Ev{parent: m, eff: e, mask: true, task: ev.task, cs: ev.cs, nursery: ev.nursery}
	}
	return body(m)
}

// HandleState runs body under a state-passing handler: each operation
// clause maps (arguments, state) to (resume value, new state) and runs in
// the performing goroutine.
func HandleState(ev *Ev, h *Handler, init Value, body func(*Ev) Value) Value {
	f := &Ev{parent: ev, eff: h.Eff, h: h, state: init, task: ev.task, cs: ev.cs, nursery: ev.nursery}
	v := body(f)
	return h.RetS(ev, v, f.state)
}
