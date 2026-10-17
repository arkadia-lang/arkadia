package rt

import (
	"sync"
	"sync/atomic"
)

// Software transactional memory (Std.Stm).
//
// The algorithm is TL2: a transaction reads the global clock when it
// starts and only accepts TVar values whose version is not newer, so it
// always sees a consistent snapshot; its writes are buffered and published
// at commit, after checking that nothing it read has changed. Commits are
// serialized by a global lock, and a TVar is marked locked while a commit
// writes it so that concurrent readers never see a half-done commit.
//
// The type system guarantees that a transaction has no other effect than
// STM (and the effects it handles itself), so running it again after a
// conflict is safe.

var (
	stmClock    atomic.Uint64
	stmCommitMu sync.Mutex
)

// TVar is a transactional variable.
type TVar struct {
	ver atomic.Uint64 // version << 1 | locked
	val atomic.Pointer[Value]

	mu      sync.Mutex
	waiters []*stmWaiter // transactions blocked in retry after reading it
}

type stmWaiter struct {
	ch   chan struct{}
	once sync.Once
}

func (w *stmWaiter) wake() { w.once.Do(func() { close(w.ch) }) }

// txn is a running transaction.
type txn struct {
	rv     uint64
	reads  map[*TVar]uint64 // the version of each TVar read
	writes map[*TVar]Value
}

// stmConflict restarts a transaction that saw a value newer than its
// snapshot.
type stmConflict struct{}

// stmRetry is raised by retry: the transaction waits until one of the
// TVars it read changes, then runs again.
type stmRetry struct{}

func tvar(v Value) *TVar { return v.(*TVar) }

func currentTxn() (*fiber, *txn) {
	fb := peekFiber()
	if fb == nil || fb.txn == nil {
		panic(&ArkError{Msg: "an STM operation was used outside atomically"})
	}
	return fb, fb.txn
}

func TVarNew(v Value) Value {
	tv := &TVar{}
	tv.val.Store(&v)
	return tv
}

func TVarRead(tv Value) Value {
	_, x := currentTxn()
	t := tvar(tv)
	if v, ok := x.writes[t]; ok {
		return v
	}
	v1 := t.ver.Load()
	if v1&1 != 0 || v1>>1 > x.rv {
		panic(stmConflict{})
	}
	p := t.val.Load()
	if t.ver.Load() != v1 {
		panic(stmConflict{})
	}
	x.reads[t] = v1
	return *p
}

func TVarWrite(tv, v Value) Value {
	_, x := currentTxn()
	x.writes[tvar(tv)] = v
	return Unit
}

// TVarReadIO reads the committed value of a TVar outside a transaction.
func TVarReadIO(tv Value) Value {
	t := tvar(tv)
	for {
		v1 := t.ver.Load()
		if v1&1 == 0 {
			p := t.val.Load()
			if t.ver.Load() == v1 {
				return *p
			}
		}
		stmCommitMu.Lock() // wait for the commit writing t
		stmCommitMu.Unlock()
	}
}

func STMRetry(_ Value) Value { panic(stmRetry{}) }

// STMOrElse runs a; if it retries, its writes are undone and b runs
// instead. The TVars read by a still wake the transaction if both retry.
func STMOrElse(a, b Value) Value {
	fb, x := currentTxn()
	saved := fb.cur
	snapshot := make(map[*TVar]Value, len(x.writes))
	for t, v := range x.writes {
		snapshot[t] = v
	}
	v, retried := func() (v Value, retried bool) {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(stmRetry); !ok {
					panic(r)
				}
				fb.cur = saved
				retried = true
			}
		}()
		return Apply1(a, Unit), false
	}()
	if !retried {
		return v
	}
	x.writes = snapshot
	return Apply1(b, Unit)
}

// STMAtomically runs a transaction until it commits.
func STMAtomically(f Value) Value {
	fb := peekFiber()
	if fb == nil {
		return withFiber(func(*fiber) Value { return STMAtomically(f) })
	}
	if fb.txn != nil {
		panic(&ArkError{Msg: "atomically: nested transaction"})
	}
	for {
		x := &txn{rv: stmClock.Load(), reads: map[*TVar]uint64{}, writes: map[*TVar]Value{}}
		v, sig := runTxn(fb, x, f)
		switch sig.(type) {
		case stmConflict:
			continue
		case stmRetry:
			x.await(fb)
			continue
		}
		if x.commit() {
			return v
		}
	}
}

// runTxn runs the body of x; sig is the conflict or retry that ended it
// early.
func runTxn(fb *fiber, x *txn, f Value) (v Value, sig any) {
	saved := fb.cur
	fb.txn = x
	defer func() {
		fb.txn = nil
		if r := recover(); r != nil {
			switch r.(type) {
			case stmConflict, stmRetry:
				fb.cur = saved
				sig = r
				return
			}
			panic(r)
		}
	}()
	return Apply1(f, Unit), nil
}

// commit publishes the writes of x unless a TVar it read has changed.
func (x *txn) commit() bool {
	if len(x.writes) == 0 {
		return true // its reads were a consistent snapshot
	}
	stmCommitMu.Lock()
	for t := range x.writes {
		t.ver.Store(t.ver.Load() | 1)
	}
	wv := stmClock.Add(1)
	for t, v := range x.reads {
		cur := t.ver.Load()
		if _, w := x.writes[t]; w {
			cur &^= 1
		}
		if cur != v {
			for t := range x.writes {
				t.ver.Store(t.ver.Load() &^ 1)
			}
			stmCommitMu.Unlock()
			return false
		}
	}
	for t, v := range x.writes {
		v := v
		t.val.Store(&v)
	}
	for t := range x.writes {
		t.ver.Store(wv << 1)
	}
	stmCommitMu.Unlock()
	for t := range x.writes {
		t.mu.Lock()
		ws := t.waiters
		t.waiters = nil
		t.mu.Unlock()
		for _, w := range ws {
			w.wake()
		}
	}
	return true
}

// await blocks a transaction that retried until a TVar it read changes.
func (x *txn) await(fb *fiber) {
	if len(x.reads) == 0 {
		panic(&ArkError{Msg: "retry: the transaction read no TVar, so it would wait forever"})
	}
	w := &stmWaiter{ch: make(chan struct{})}
	for t := range x.reads {
		t.mu.Lock()
		t.waiters = append(t.waiters, w)
		t.mu.Unlock()
	}
	defer func() {
		for t := range x.reads {
			t.mu.Lock()
			for i, o := range t.waiters {
				if o == w {
					t.waiters = append(t.waiters[:i], t.waiters[i+1:]...)
					break
				}
			}
			t.mu.Unlock()
		}
	}()
	for t, v := range x.reads {
		if t.ver.Load()&^1 != v {
			return // changed already
		}
	}
	waitOn(fb, w.ch)
}
