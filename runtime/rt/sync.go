package rt

import (
	"sync"
	"sync/atomic"
)

// Values shared between tasks: channels, atomic references and mutexes.
// The blocking operations are cancellation points.

// ---------------------------------------------------------------- channels

// Channel carries values between tasks.
type Channel struct {
	c      chan Value
	closed chan struct{} // closed by Close
	once   sync.Once
}

func NewChannel(capacity Value) Value {
	n := capacity.(int64)
	if n < 0 {
		panic(&ArkError{Msg: "newChannel: negative capacity"})
	}
	return &Channel{c: make(chan Value, n), closed: make(chan struct{})}
}

func chanOf(v Value) *Channel { return v.(*Channel) }

func closedSendError() *ArkError { return &ArkError{Msg: "send on a closed channel"} }

// ChannelSend sends a value, waiting while the channel is full.
func ChannelSend(ev *Ev, cv, v Value) Value {
	ch := chanOf(cv)
	select {
	case <-ch.closed:
		panic(closedSendError())
	default:
	}
	defer closedSend()
	select {
	case ch.c <- v:
	case <-ch.closed:
		panic(closedSendError())
	case <-ev.cs.done:
		unwind(ev)
	}
	return Unit
}

// closedSend turns the panic of a Go send on a channel closed meanwhile
// into a runtime error.
func closedSend() {
	if r := recover(); r != nil {
		if e, ok := r.(error); ok && e.Error() == "send on closed channel" {
			panic(closedSendError())
		}
		panic(r)
	}
}

// ChannelTrySend sends a value if the channel has room: whether it did.
func ChannelTrySend(cv, v Value) (sent Value) {
	ch := chanOf(cv)
	defer func() {
		if r := recover(); r != nil {
			sent = false
		}
	}()
	select {
	case <-ch.closed:
		return false
	case ch.c <- v:
		return true
	default:
		return false
	}
}

// ChannelReceive receives a value, waiting while the channel is empty:
// None once it is closed and empty.
func ChannelReceive(ev *Ev, cv Value) Value {
	ch := chanOf(cv)
	select {
	case v, ok := <-ch.c:
		if !ok {
			return None
		}
		return Some(v)
	case <-ev.cs.done:
		unwind(ev)
	}
	return None
}

// ChannelTryReceive receives a value if one is waiting.
func ChannelTryReceive(cv Value) Value {
	select {
	case v, ok := <-chanOf(cv).c:
		if !ok {
			return None
		}
		return Some(v)
	default:
		return None
	}
}

// ChannelClose closes a channel: receivers get the remaining values, then
// None. Closing it again does nothing.
func ChannelClose(cv Value) Value {
	ch := chanOf(cv)
	ch.once.Do(func() {
		close(ch.closed)
		close(ch.c)
	})
	return Unit
}

func ChannelLength(cv Value) Value { return int64(len(chanOf(cv).c)) }

// ---------------------------------------------------------------- atomic references

// Atomic is a reference that tasks read and update atomically.
type Atomic struct{ p atomic.Pointer[atomicBox] }

type atomicBox struct{ v Value }

func atomicOf(v Value) *Atomic { return v.(*Atomic) }

func AtomicNew(v Value) Value {
	a := &Atomic{}
	a.p.Store(&atomicBox{v})
	return a
}

func AtomicRead(a Value) Value { return atomicOf(a).p.Load().v }

func AtomicWrite(a, v Value) Value {
	atomicOf(a).p.Store(&atomicBox{v})
	return Unit
}

func AtomicSwap(a, v Value) Value { return atomicOf(a).p.Swap(&atomicBox{v}).v }

// AtomicUpdate applies f to the value and stores the first component of
// the result, atomically, and returns the second. f runs again when
// another task changed the value meanwhile.
func AtomicUpdate(ev *Ev, av, f Value) Value {
	a := atomicOf(av)
	for {
		old := a.p.Load()
		r := Apply1(ev, f, old.v).(*D2)
		if a.p.CompareAndSwap(old, &atomicBox{r.A}) {
			return r.B
		}
	}
}

// ---------------------------------------------------------------- mutexes

// Mutex is a lock; waiting for it is a cancellation point.
type Mutex struct{ c chan struct{} }

func NewMutex(_ Value) Value { return &Mutex{c: make(chan struct{}, 1)} }

// WithLock runs f () holding the lock, which is released however f ends.
func WithLock(ev *Ev, mv, f Value) Value {
	m := mv.(*Mutex)
	select {
	case m.c <- struct{}{}:
	case <-ev.cs.done:
		unwind(ev)
	}
	defer func() { <-m.c }()
	return Apply1(ev, f, Unit)
}
