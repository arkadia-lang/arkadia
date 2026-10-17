package rt

import (
	"reflect"
	"runtime"
	"time"
)

// Channels (Std.Channel) are Go channels. Every blocking operation is a
// cancellation point.

type Channel struct{ c chan Value }

func chanOf(v Value) chan Value { return v.(*Channel).c }

func ChanNew(capacity Value) Value {
	return &Channel{c: make(chan Value, max(capacity.(int64), 0))}
}

func closedSend() { panic(&ArkError{Msg: "send on a closed channel"}) }

// catchClosed turns the panic of Go sending on a closed channel into an
// Arkadia error.
func catchClosed() {
	if r := recover(); r != nil {
		if e, ok := r.(runtime.Error); ok && e.Error() == "send on closed channel" {
			closedSend()
		}
		panic(r)
	}
}

func ChanSend(ch, v Value) Value {
	defer catchClosed()
	c := chanOf(ch)
	fb := peekFiber()
	for {
		if fb != nil {
			fb.poll()
		}
		cancel, kick := fb.interrupts()
		select {
		case c <- v:
			return Unit
		case <-cancel:
		case <-kick:
		}
	}
}

func ChanTrySend(ch, v Value) Value {
	defer catchClosed()
	select {
	case chanOf(ch) <- v:
		return true
	default:
		return false
	}
}

// ChanReceive returns Some value, or None once the channel is closed and
// empty.
func ChanReceive(ch Value) Value {
	c := chanOf(ch)
	fb := peekFiber()
	for {
		if fb != nil {
			fb.poll()
		}
		cancel, kick := fb.interrupts()
		select {
		case v, ok := <-c:
			if !ok {
				return None
			}
			return Some(v)
		case <-cancel:
		case <-kick:
		}
	}
}

func ChanTryReceive(ch Value) Value {
	select {
	case v, ok := <-chanOf(ch):
		if !ok {
			return None
		}
		return Some(v)
	default:
		return None
	}
}

func ChanClose(ch Value) Value {
	defer func() {
		if r := recover(); r != nil {
			panic(&ArkError{Msg: "close of a closed channel"})
		}
	}()
	close(chanOf(ch))
	return Unit
}

func ChanLength(ch Value) Value { return int64(len(chanOf(ch))) }

// ---------------------------------------------------------------- select

// SelectCase is one alternative of ChanSelect; its function maps what
// happened to the result of the select.
type SelectCase struct {
	kind int // caseReceive, caseSend, caseAfter
	c    chan Value
	v    Value // the value sent
	secs float64
	f    Value // receive: Option a -> b; send and after: the result
}

const (
	caseReceive = iota
	caseSend
	caseAfter
)

func SelectOnReceive(ch, f Value) Value {
	return &SelectCase{kind: caseReceive, c: chanOf(ch), f: f}
}

func SelectOnSend(ch, v, r Value) Value {
	return &SelectCase{kind: caseSend, c: chanOf(ch), v: v, f: r}
}

func SelectAfter(secs, r Value) Value {
	return &SelectCase{kind: caseAfter, secs: secs.(float64), f: r}
}

// ChanSelect waits until one of the cases can proceed (choosing at random
// among those ready) and returns its result.
func ChanSelect(cases Value) Value {
	defer catchClosed()
	cs := ListToSlice(cases)
	if len(cs) == 0 {
		panic(&ArkError{Msg: "select: no cases"})
	}
	sel := make([]reflect.SelectCase, 0, len(cs)+2)
	owner := make([]*SelectCase, 0, len(cs))
	var timers []*time.Timer
	defer func() {
		for _, t := range timers {
			t.Stop()
		}
	}()
	for _, cv := range cs {
		c := cv.(*SelectCase)
		switch c.kind {
		case caseReceive:
			sel = append(sel, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(c.c)})
		case caseSend:
			sel = append(sel, reflect.SelectCase{Dir: reflect.SelectSend, Chan: reflect.ValueOf(c.c), Send: reflect.ValueOf(&c.v).Elem()})
		case caseAfter:
			t := time.NewTimer(time.Duration(c.secs * float64(time.Second)))
			timers = append(timers, t)
			sel = append(sel, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(t.C)})
		}
		owner = append(owner, c)
	}
	fb := peekFiber()
	cancel, kick := fb.interrupts()
	sel = append(sel,
		reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(cancel)},
		reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(kick)})
	for {
		if fb != nil {
			fb.poll()
		}
		i, v, ok := reflect.Select(sel)
		if i >= len(owner) {
			continue
		}
		c := owner[i]
		switch c.kind {
		case caseReceive:
			if !ok {
				return Apply1(c.f, None)
			}
			return Apply1(c.f, Some(v.Interface()))
		default:
			return c.f
		}
	}
}
