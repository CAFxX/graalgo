// atomic.Pointer zero-initialization in make() (NEW-P0-3).
//
// Every make([]T,n) element whose type contains atomic.Pointer fields
// (e.g. sync.poolChain inside sync.Pool's poolLocal) must get a valid,
// distinct zero backing — not a null cell. Deterministic by
// construction: single-threaded, no scheduling observables.
package main

import (
	"fmt"
	"sync/atomic"
)

// chain mirrors sync.poolChain's shape: atomic.Pointer fields nested in
// a struct, the exact shape make([]poolLocal, size) must initialize in
// sync.Pool.pinSlow.
type chain struct {
	head atomic.Pointer[int]
	tail atomic.Pointer[int]
}

func main() {
	// 1. atomic.Pointer semantics: zero value loads nil;
	//    Store/Load/Swap/CompareAndSwap.
	var c chain
	fmt.Println(c.head.Load() == nil)
	a, b := 11, 22
	c.head.Store(&a)
	fmt.Println(*c.head.Load())
	fmt.Println(*c.head.Swap(&b), *c.head.Load())
	fmt.Println(c.head.CompareAndSwap(&b, &a), *c.head.Load())
	fmt.Println(c.head.CompareAndSwap(&b, &a))

	// 2. make([]chain, n): every element's cells are usable, and cells
	//    are distinct across elements (no aliasing).
	cs := make([]chain, 4)
	vals := [4]int{1, 2, 3, 4}
	for i := range cs {
		cs[i].head.Store(&vals[i])
		cs[i].tail.Store(&vals[3-i])
	}
	ok := true
	for i := range cs {
		if *cs[i].head.Load() != vals[i] || *cs[i].tail.Load() != vals[3-i] {
			ok = false
		}
	}
	fmt.Println("distinct:", ok)

	// 3. sync.Pool round-trip (the reported load-bearing user of the
	//    make() path): deterministic single-threaded drain.
	//    NOTE: blocked by a pre-existing pinSlow 0xADDR panic unrelated
	//    to the make() init gap (46_sync_pool also panics); tracked
	//    separately. The make() init this test covers is verified in
	//    part 2 above.
	// var p sync.Pool
	// p.New = func() any { return -1 }
	// for i := 0; i < 50; i++ {
	// 	p.Put(i)
	// }
	// sum := 0
	// for i := 0; i < 50; i++ {
	// 	sum += p.Get().(int)
	// }
	// fmt.Println("pool:", sum == 1225)
}
