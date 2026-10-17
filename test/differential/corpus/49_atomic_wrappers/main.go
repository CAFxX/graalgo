// sync/atomic wrappers: Store/Load round-trip, Swap/CAS old-value
// semantics, nil *T loads, And/Or, and cross-goroutine store/load.
// Deterministic by construction: concurrent results are reduced to
// order-independent checks (every Load sees a consistently-typed value,
// no torn reads, no panics).
//
// Uses the println builtin (not fmt): the real fmt package does not yet
// build through the backend (pre-existing comma-ok/multi-result assign
// export gap, unrelated to sync/atomic).
package main

import (
	"sync"
	"sync/atomic"
)

func main() {
	// 1. atomic.Value Store/Load round-trip; dynamic type preserved.
	var v atomic.Value
	v.Store("hello")
	println(v.Load().(string))
	// Load before any Store is nil (no panic).
	var v0 atomic.Value
	println(v0.Load() == nil)

	// 2. Int64: Swap/CAS old-value semantics, Add, And, Or.
	var i atomic.Int64
	i.Store(3)
	println(i.Swap(10))                         // 3
	println(i.CompareAndSwap(10, 20), i.Load()) // true 20
	println(i.CompareAndSwap(10, 30), i.Load()) // false 20
	i.Add(5)
	println(i.Load())  // 25
	println(i.And(12)) // 25 (old value; i becomes 8)
	println(i.Or(3))   // 8 (old value; i becomes 11)

	// 3. atomic.Pointer[T]: nil *T before Store; round-trip; Swap/CAS.
	type node struct{ n int }
	var p atomic.Pointer[node]
	println(p.Load() == nil)
	n := &node{n: 7}
	p.Store(n)
	println(p.Load() == n, p.Load().n)
	old := p.Swap(&node{n: 8})
	println(old == n, p.Load().n)
	println(p.CompareAndSwap(p.Load(), &node{n: 9}), p.Load().n)

	// 4. Cross-goroutine: N goroutines Store same-typed values; every
	// Load must see a consistently-typed, in-range value.
	var wg sync.WaitGroup
	var cv atomic.Value
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			cv.Store(g * 100)
		}(g)
	}
	bad := 0
	for k := 0; k < 1000; k++ {
		if x, ok := cv.Load().(int); ok {
			if x%100 != 0 || x < 0 || x >= 800 {
				bad++
			}
		}
	}
	wg.Wait()
	println("bad:", bad)
	println(cv.Load().(int)%100 == 0)

	// 5. Remaining kind coverage: Uint32, Bool, Uintptr.
	var u atomic.Uint32
	u.Store(1)
	println(u.Add(2), u.Load())
	var b atomic.Bool
	println(b.CompareAndSwap(false, true), b.Load())
	var up atomic.Uintptr
	up.Store(5)
	println(up.Load())
}
