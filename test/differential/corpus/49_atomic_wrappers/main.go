// sync/atomic wrappers: Store/Load round-trip, Swap/CAS old-value
// semantics, nil *T loads, And/Or, and cross-goroutine store/load.
// Deterministic by construction: concurrent results are reduced to
// order-independent checks (every Load sees a consistently-typed value,
// no torn reads, no panics).
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	// 1. atomic.Value Store/Load round-trip; dynamic type preserved.
	var v atomic.Value
	v.Store("hello")
	fmt.Println(v.Load().(string))
	// Load before any Store is nil (no panic).
	var v0 atomic.Value
	fmt.Println(v0.Load() == nil)

	// 2. Int64: Swap/CAS old-value semantics, Add, And, Or.
	var i atomic.Int64
	i.Store(3)
	fmt.Println(i.Swap(10))                         // 3
	fmt.Println(i.CompareAndSwap(10, 20), i.Load()) // true 20
	fmt.Println(i.CompareAndSwap(10, 30), i.Load()) // false 20
	i.Add(5)
	fmt.Println(i.Load())  // 25
	fmt.Println(i.And(12)) // 25 (old value; i becomes 8)
	fmt.Println(i.Or(3))   // 8 (old value; i becomes 11)

	// 3. atomic.Pointer[T]: nil *T before Store; round-trip; Swap/CAS.
	type node struct{ n int }
	var p atomic.Pointer[node]
	fmt.Println(p.Load() == nil)
	n := &node{n: 7}
	p.Store(n)
	fmt.Println(p.Load() == n, p.Load().n)
	old := p.Swap(&node{n: 8})
	fmt.Println(old == n, p.Load().n)
	fmt.Println(p.CompareAndSwap(p.Load(), &node{n: 9}), p.Load().n)

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
	fmt.Println("bad:", bad)
	fmt.Println(cv.Load().(int)%100 == 0)

	// 5. Remaining kind coverage: Uint32, Bool, Uintptr.
	var u atomic.Uint32
	u.Store(1)
	fmt.Println(u.Add(2), u.Load())
	var b atomic.Bool
	fmt.Println(b.CompareAndSwap(false, true), b.Load())
	var up atomic.Uintptr
	up.Store(5)
	fmt.Println(up.Load())
}
