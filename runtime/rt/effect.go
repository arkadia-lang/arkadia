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
	eff    int
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
	for f != nil && f.eff != eff {
		f = f.parent
	}
	if f == nil {
		name := effName(eff)
		if eff < len(OpNames) && op < len(OpNames[eff]) {
			name += "." + OpNames[eff][op]
		}
		panic(&ArkError{Msg: "unhandled effect operation " + name})
	}
	saved := cur
	if f.h.Tail {
		cur = f.parent
		r := f.h.Ops[op](args, nil)
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

// Handle runs body under the handler h.
func Handle(h *Handler, body func() Value) Value {
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
				f.ch <- msg{kind: msgPanic, err: r}
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
