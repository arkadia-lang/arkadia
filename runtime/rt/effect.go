package rt

import "fmt"

// Algebraic effect handlers.
//
// A handled computation normally runs on its own goroutine. Performing an
// operation sends a message to the goroutine running the handler and blocks
// until the handler resumes the continuation. This gives deep handlers with
// one-shot continuations. Exactly one goroutine runs at a time; control is
// passed with unbuffered channel operations, so the global handler stack
// `cur` needs no locking.
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

// cur is the current handler stack.
var cur *frame

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
	f := cur
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
	saved := cur
	if f.h.State {
		cur = f.parent
		r := f.h.OpsS[op](args, f.state).(*D2)
		f.state = r.B
		cur = saved
		return r.A
	}
	if f.h.Tail || f.h.Direct {
		cur = f.parent
		r := f.h.Ops[op](args, nil)
		if f.h.Direct && !f.h.UsesK[op] {
			panic(&abortTo{f: f, v: r})
		}
		cur = saved
		return r
	}
	rch := make(chan resumeMsg)
	f.ch <- msg{kind: msgOp, op: op, args: args, resume: rch}
	r := <-rch
	if r.abort {
		panic(abortSignal{target: r.target})
	}
	cur = saved
	return r.val
}

// abortTo unwinds to the Handle of frame f with the result v.
type abortTo struct {
	f *frame
	v Value
}

func handleDirect(h *Handler, body func() Value) (result Value) {
	outer := cur
	f := &frame{eff: h.Eff, h: h, parent: outer}
	defer func() {
		if r := recover(); r != nil {
			if a, ok := r.(*abortTo); ok && a.f == f {
				cur = outer
				result = a.v
				return
			}
			panic(r)
		}
	}()
	cur = f
	v := body()
	cur = outer
	if h.Ret != nil {
		return h.Ret(v)
	}
	return v
}

// Handle runs body under the handler h.
func Handle(h *Handler, body func() Value) Value {
	if h.Direct {
		return handleDirect(h, body)
	}
	outer := cur
	if h.Tail {
		f := &frame{eff: h.Eff, h: h, parent: outer}
		cur = f
		v := body()
		cur = outer
		if h.Ret != nil {
			return h.Ret(v)
		}
		return v
	}
	f := &frame{eff: h.Eff, h: h, ch: make(chan msg), parent: outer}
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
		cur = f
		v := body()
		f.ch <- msg{kind: msgReturn, val: v}
	}()
	return h.loop(f, outer)
}

func (h *Handler) loop(f *frame, outer *frame) Value {
	m := <-f.ch
	switch m.kind {
	case msgAbort:
		panic(abortSignal{target: m.target})
	case msgPanic:
		panic(m.err)
	case msgReturn:
		cur = outer
		if h.Ret != nil {
			return h.Ret(m.val)
		}
		return m.val
	}
	cur = outer
	resumed := false
	if !h.UsesK[m.op] {
		resumed = true
		m.resume <- resumeMsg{abort: true, target: f}
		return h.Ops[m.op](m.args, nil)
	}
	k := Fn1(func(v Value) Value {
		if resumed {
			panic(&ArkError{Msg: "continuation of " + effName(h.Eff) + " resumed more than once"})
		}
		resumed = true
		saved := cur
		m.resume <- resumeMsg{val: v}
		r := h.loop(f, outer)
		cur = saved
		return r
	})
	return h.Ops[m.op](m.args, k)
}

// Mask runs body with the innermost handler of each listed effect hidden,
// so that operations performed by body reach the next outer handler.
func Mask(effs []int, body func() Value) Value {
	saved := cur
	for _, e := range effs {
		cur = &frame{eff: e, mask: true, parent: cur}
	}
	v := body()
	cur = saved
	return v
}

// HandleState runs body under a state-passing handler: each operation
// clause maps (arguments, state) to (resume value, new state) and runs in
// the performing goroutine.
func HandleState(h *Handler, init Value, body func() Value) Value {
	outer := cur
	f := &frame{eff: h.Eff, h: h, parent: outer, state: init}
	cur = f
	v := body()
	cur = outer
	return h.RetS(v, f.state)
}
