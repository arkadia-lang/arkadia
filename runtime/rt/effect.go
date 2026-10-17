package rt

import (
	"fmt"
	"sync/atomic"
)

// Algebraic effect handlers.
//
// Each goroutine has its own handler stack, in its fiber (fiber.go).
//
// A handled computation normally runs on its own goroutine. Performing an
// operation sends a message to the goroutine running the handler and blocks
// until the handler resumes the continuation. This gives deep handlers with
// one-shot continuations. The goroutines of one handled computation pass
// control with unbuffered channel operations, so exactly one of them runs
// at a time and they share the handler frames below the handler without
// locking. Tasks (task.go) start with an empty handler stack and so never
// share handler frames.
//
// When every operation clause of a handler is tail-resumptive (it ends by
// resuming the continuation exactly once), the compiler marks the handler
// Tail and the clauses run directly in the performing goroutine instead.

type frame struct {
	state  Value // current state of a state-passing handler
	eff    int
	mask   bool // a mask frame hides the next handler of eff
	h      *Handler
	ch     chan msg
	parent *frame
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
	target *frame
	err    any
}

type resumeMsg struct {
	val    Value
	abort  bool
	target *frame
	// the task and transaction of the goroutine resuming the computation,
	// which now runs on their behalf
	task *Task
	txn  *txn
}

// abortSignal unwinds a computation whose continuation was discarded.
type abortSignal struct{ target *frame }

// Handler describes a handler for one effect.
type Handler struct {
	Eff int
	// Ret is the return clause (nil for the identity).
	Ret func(Value) Value
	// Ops are the operation clauses: arguments and the continuation.
	Ops []func(args []Value, k Value) Value
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
	RetS  func(x, s Value) Value
	OpsS  []func(args []Value, s Value) Value
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
func Perform(eff, op int, args []Value) Value {
	fb := peekFiber()
	var f *frame
	if fb != nil {
		if pending.Load() != 0 {
			fb.poll()
		}
		f = fb.cur
	}
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
	saved := fb.cur
	if f.h.State {
		fb.cur = f.parent
		r := f.h.OpsS[op](args, f.state).(*D2)
		f.state = r.B
		fb.cur = saved
		return r.A
	}
	if f.h.Tail || f.h.Direct {
		fb.cur = f.parent
		r := f.h.Ops[op](args, nil)
		if f.h.Direct && !f.h.UsesK[op] {
			panic(&abortTo{f: f, v: r})
		}
		fb.cur = saved
		return r
	}
	rch := make(chan resumeMsg)
	f.ch <- msg{kind: msgOp, op: op, args: args, resume: rch}
	r := <-rch
	if r.abort {
		panic(abortSignal{target: r.target})
	}
	fb.cur = saved
	fb.task, fb.txn = r.task, r.txn
	return r.val
}

// abortTo unwinds to the Handle of frame f with the result v.
type abortTo struct {
	f *frame
	v Value
}

func handleDirect(fb *fiber, h *Handler, body func() Value) (result Value) {
	outer := fb.cur
	f := &frame{eff: h.Eff, h: h, parent: outer}
	defer func() {
		if r := recover(); r != nil {
			if a, ok := r.(*abortTo); ok && a.f == f {
				fb.cur = outer
				result = a.v
				return
			}
			panic(r)
		}
	}()
	fb.cur = f
	v := body()
	fb.cur = outer
	if h.Ret != nil {
		return h.Ret(v)
	}
	return v
}

// Handle runs body under the handler h.
func Handle(h *Handler, body func() Value) Value {
	fb := peekFiber()
	if fb == nil {
		return withFiber(func(*fiber) Value { return Handle(h, body) })
	}
	if h.Direct {
		return handleDirect(fb, h, body)
	}
	outer := fb.cur
	if h.Tail {
		f := &frame{eff: h.Eff, h: h, parent: outer}
		fb.cur = f
		v := body()
		fb.cur = outer
		if h.Ret != nil {
			return h.Ret(v)
		}
		return v
	}
	f := &frame{eff: h.Eff, h: h, ch: make(chan msg), parent: outer}
	goFiber(&fiber{cur: f, task: fb.task, txn: fb.txn}, func(*fiber) {
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
		v := body()
		f.ch <- msg{kind: msgReturn, val: v}
	})
	return h.loop(fb, f, outer)
}

// loop waits for the next message of the computation handled by frame f.
// It runs on the goroutine of fb: the one that started the computation, or
// the one that last resumed it.
func (h *Handler) loop(fb *fiber, f *frame, outer *frame) Value {
	m := <-f.ch
	switch m.kind {
	case msgAbort:
		panic(abortSignal{target: m.target})
	case msgPanic:
		panic(m.err)
	case msgReturn:
		fb.cur = outer
		if h.Ret != nil {
			return h.Ret(m.val)
		}
		return m.val
	}
	fb.cur = outer
	if !h.UsesK[m.op] {
		m.resume <- resumeMsg{abort: true, target: f}
		return h.Ops[m.op](m.args, nil)
	}
	var resumed atomic.Bool
	k := Fn1(func(v Value) Value {
		if resumed.Swap(true) {
			panic(&ArkError{Msg: "continuation of " + effName(h.Eff) + " resumed more than once"})
		}
		rf := peekFiber()
		if rf == nil {
			return withFiber(func(rf *fiber) Value { return resume(h, rf, f, outer, m.resume, v) })
		}
		return resume(h, rf, f, outer, m.resume, v)
	})
	return h.Ops[m.op](m.args, k)
}

// resume resumes the computation handled by frame f from the goroutine of
// fb and handles it there until its next operation.
func resume(h *Handler, fb *fiber, f, outer *frame, rch chan resumeMsg, v Value) Value {
	saved := fb.cur
	rch <- resumeMsg{val: v, task: fb.task, txn: fb.txn}
	r := h.loop(fb, f, outer)
	fb.cur = saved
	return r
}

// Mask runs body with the innermost handler of each listed effect hidden,
// so that operations performed by body reach the next outer handler.
func Mask(effs []int, body func() Value) Value {
	fb := peekFiber()
	if fb == nil {
		return withFiber(func(*fiber) Value { return Mask(effs, body) })
	}
	saved := fb.cur
	for _, e := range effs {
		fb.cur = &frame{eff: e, mask: true, parent: fb.cur}
	}
	v := body()
	fb.cur = saved
	return v
}

// HandleState runs body under a state-passing handler: each operation
// clause maps (arguments, state) to (resume value, new state) and runs in
// the performing goroutine.
func HandleState(h *Handler, init Value, body func() Value) Value {
	fb := peekFiber()
	if fb == nil {
		return withFiber(func(*fiber) Value { return HandleState(h, init, body) })
	}
	outer := fb.cur
	f := &frame{eff: h.Eff, h: h, parent: outer, state: init}
	fb.cur = f
	v := body()
	fb.cur = outer
	return h.RetS(v, f.state)
}
