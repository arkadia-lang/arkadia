package rt

import (
	"sync"
	"sync/atomic"
)

// Goroutine-local state.
//
// Arkadia code runs on many goroutines at once: every task has its own, and
// a handled computation may run on a goroutine of its own (see Handle). The
// dynamic context of the code running on a goroutine — its handler stack,
// the task it belongs to, the transaction it is part of — is a fiber,
// found from the goroutine by a lock-free table keyed by the runtime's g
// pointer (getg). A g is reused once its goroutine exits, so every
// goroutine that registers a fiber unregisters it before it exits.

type fiber struct {
	g    uintptr
	cur  *frame // the handler stack
	task *Task  // nil on goroutines the runtime did not start
	txn  *txn   // the running STM transaction, if any
	// regions are the cancellable scopes open on this goroutine, innermost
	// last (see Scope).
	regions []*Scope
	// kick wakes the goroutine from a blocking wait when one of its regions
	// is cancelled.
	kick chan struct{}
}

type fiberTable struct {
	slots []atomic.Pointer[fiber]
	shift uint
}

var (
	fibers   atomic.Pointer[fiberTable]
	fibersMu sync.Mutex
	// live and used (live + tombstones) slots of the current table, under
	// fibersMu.
	fiberLive, fiberUsed int
	tombstone            = &fiber{}
)

func init() { fibers.Store(newFiberTable(6)) }

func newFiberTable(bits uint) *fiberTable {
	return &fiberTable{slots: make([]atomic.Pointer[fiber], 1<<bits), shift: 64 - bits}
}

func (t *fiberTable) home(g uintptr) uintptr {
	return uintptr(uint64(g) * 0x9E3779B97F4A7C15 >> t.shift)
}

// Only the goroutine of a fiber looks it up, and only it removes it, so a
// lookup finds the fiber in whichever table it loads.
func lookupFiber(g uintptr) *fiber {
	t := fibers.Load()
	mask := uintptr(len(t.slots) - 1)
	for i := t.home(g); ; i = (i + 1) & mask {
		f := t.slots[i].Load()
		if f == nil {
			return nil
		}
		if f.g == g {
			return f
		}
	}
}

// peekFiber returns the fiber of the current goroutine, or nil if the
// runtime did not start it and it runs no handler.
func peekFiber() *fiber { return lookupFiber(getg()) }

// registerFiber makes f the fiber of the current goroutine.
func registerFiber(f *fiber) *fiber {
	f.g = getg()
	fibersMu.Lock()
	t := fibers.Load()
	if (fiberUsed+1)*2 > len(t.slots) {
		bits := uint(6)
		for 1<<bits < (fiberLive+1)*4 {
			bits++
		}
		nt := newFiberTable(bits)
		for i := range t.slots {
			if o := t.slots[i].Load(); o != nil && o != tombstone {
				nt.insert(o)
			}
		}
		fibers.Store(nt)
		t, fiberUsed = nt, fiberLive
	}
	if t.insert(f) {
		fiberUsed++
	}
	fiberLive++
	fibersMu.Unlock()
	return f
}

// insert reports whether it used an empty (not a tombstone) slot.
func (t *fiberTable) insert(f *fiber) bool {
	mask := uintptr(len(t.slots) - 1)
	for i := t.home(f.g); ; i = (i + 1) & mask {
		switch t.slots[i].Load() {
		case nil:
			t.slots[i].Store(f)
			return true
		case tombstone:
			t.slots[i].Store(f)
			return false
		}
	}
}

// unregisterFiber removes the fiber of the current goroutine.
func unregisterFiber(f *fiber) {
	fibersMu.Lock()
	t := fibers.Load()
	mask := uintptr(len(t.slots) - 1)
	for i := t.home(f.g); ; i = (i + 1) & mask {
		o := t.slots[i].Load()
		if o == nil {
			break
		}
		if o == f {
			t.slots[i].Store(tombstone)
			fiberLive--
			break
		}
	}
	fibersMu.Unlock()
}

// withFiber runs body on a goroutine the runtime did not start (FFI code,
// package initialization) with a fiber of its own for the duration of the
// call.
func withFiber(body func(fb *fiber) Value) Value {
	fb := registerFiber(&fiber{})
	defer unregisterFiber(fb)
	return body(fb)
}

// goFiber starts a goroutine running body with a fresh fiber.
func goFiber(fb *fiber, body func(fb *fiber)) {
	go func() {
		registerFiber(fb)
		defer unregisterFiber(fb)
		body(fb)
	}()
}
