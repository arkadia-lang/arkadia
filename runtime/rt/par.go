package rt

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// Deterministic parallelism for pure functions (Std.Par).
//
// The functions are pure — the type system rules out every effect they do
// not handle themselves — so evaluating them in parallel gives the same
// result as evaluating them in order. Errors are deterministic too: when
// several elements fail, the error raised is the one of the first failing
// element, as with a sequential map.

// parFor calls body(i) for every i in [0, n) on up to GOMAXPROCS tasks
// that take chunks of indices in increasing order.
func parFor(n int, body func(i int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	if workers <= 1 {
		for i := 0; i < n; i++ {
			body(i)
		}
		return
	}
	chunk := max(1, n/(workers*8))
	var next atomic.Int64
	var mu sync.Mutex
	failAt, failure := int64(n), any(nil)
	failedBefore := func(i int) bool {
		mu.Lock()
		defer mu.Unlock()
		return int64(i) > failAt
	}
	// run calls body(i) and reports whether it raised an error, recording
	// the error of the smallest index. Cancellation passes through.
	run := func(i int) (failed bool) {
		defer func() {
			if r := recover(); r != nil {
				if _, ok := r.(cancelSignal); ok {
					panic(r)
				}
				r = withTrace(r)
				mu.Lock()
				if int64(i) < failAt {
					failAt, failure = int64(i), r
				}
				mu.Unlock()
				failed = true
			}
		}()
		body(i)
		return false
	}
	runScope(func(s *Scope) Value {
		for w := 0; w < workers; w++ {
			s.fork(func() Value {
				for {
					start := int(next.Add(int64(chunk))) - chunk
					if start >= n || failedBefore(start) {
						return Unit
					}
					for i := start; i < min(start+chunk, n); i++ {
						if run(i) {
							return Unit
						}
					}
				}
			})
		}
		return Unit
	}, nil)
	if failure != nil {
		panic(failure)
	}
}

func ParMap(f, xs Value) Value {
	in := ListToSlice(xs)
	out := make([]Value, len(in))
	parFor(len(in), func(i int) { out[i] = Apply1(f, in[i]) })
	return SliceToList(out)
}

func ParMapArray(f, a Value) Value {
	in := a.(*Array).S
	out := make([]Value, len(in))
	parFor(len(in), func(i int) { out[i] = Apply1(f, in[i]) })
	return &Array{S: out}
}

func ParGenerate(n, f Value) Value {
	out := make([]Value, max(n.(int64), 0))
	parFor(len(out), func(i int) { out[i] = Apply1(f, int64(i)) })
	return SliceToList(out)
}

func ParFilter(p, xs Value) Value {
	in := ListToSlice(xs)
	keep := make([]bool, len(in))
	parFor(len(in), func(i int) { keep[i] = Apply1(p, in[i]).(bool) })
	out := make([]Value, 0, len(in))
	for i, x := range in {
		if keep[i] {
			out = append(out, x)
		}
	}
	return SliceToList(out)
}

func ParBoth(f, g Value) Value {
	var a, b Value
	parFor(2, func(i int) {
		if i == 0 {
			a = Apply1(f, Unit)
		} else {
			b = Apply1(g, Unit)
		}
	})
	return tuple2(a, b)
}

// ParReduce combines the elements with an associative operator and its
// identity: chunks are reduced in parallel, then their results in order.
func ParReduce(op, z, xs Value) Value {
	in := ListToSlice(xs)
	if len(in) == 0 {
		return z
	}
	chunks := min(len(in), runtime.GOMAXPROCS(0)*4)
	size := (len(in) + chunks - 1) / chunks
	chunks = (len(in) + size - 1) / size
	part := make([]Value, chunks)
	parFor(chunks, func(c int) {
		lo, hi := c*size, min((c+1)*size, len(in))
		acc := in[lo]
		for _, x := range in[lo+1 : hi] {
			acc = Apply2(op, acc, x)
		}
		part[c] = acc
	})
	acc := z
	for _, p := range part {
		acc = Apply2(op, acc, p)
	}
	return acc
}

// ParSortBy sorts stably: runs are sorted in parallel, then merged pairwise
// in parallel.
func ParSortBy(cmp, xs Value) Value {
	s := ListToSlice(xs)
	less := func(a, b Value) bool { return TagOf(Apply2(cmp, a, b)) == 0 }
	n := len(s)
	runs := min(runtime.GOMAXPROCS(0)*2, n/256)
	if runs <= 1 {
		sortStable(s, less)
		return SliceToList(s)
	}
	bounds := make([]int, runs+1)
	for i := range bounds {
		bounds[i] = i * n / runs
	}
	parFor(runs, func(r int) { sortStable(s[bounds[r]:bounds[r+1]], less) })
	tmp := make([]Value, n)
	for len(bounds) > 2 {
		pairs := (len(bounds) - 1) / 2
		parFor(pairs, func(p int) {
			lo, mid, hi := bounds[2*p], bounds[2*p+1], bounds[2*p+2]
			merge(tmp[lo:hi], s[lo:mid], s[mid:hi], less)
			copy(s[lo:hi], tmp[lo:hi])
		})
		nb := make([]int, 0, pairs+2)
		for i := 0; i < len(bounds); i += 2 {
			nb = append(nb, bounds[i])
		}
		if nb[len(nb)-1] != n {
			nb = append(nb, n)
		}
		bounds = nb
	}
	return SliceToList(s)
}

// merge merges two sorted runs into out, taking from a on ties.
func merge(out, a, b []Value, less func(x, y Value) bool) {
	i, j, k := 0, 0, 0
	for i < len(a) && j < len(b) {
		if less(b[j], a[i]) {
			out[k] = b[j]
			j++
		} else {
			out[k] = a[i]
			i++
		}
		k++
	}
	k += copy(out[k:], a[i:])
	copy(out[k:], b[j:])
}
