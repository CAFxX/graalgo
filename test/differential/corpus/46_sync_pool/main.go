// sync.Pool: Put/Get round-trip, New func (field and literal forms),
// Put(nil) no-op, empty Get, and concurrent use. Deterministic by
// construction: item multisets are counted, goroutine results are summed
// (order-independent).
package main

import (
	"fmt"
	"sync"
)

func main() {
	// 1. Basic Put/Get round-trip. Get order is unspecified, so count.
	var p sync.Pool
	p.Put("a")
	p.Put("b")
	got := map[string]int{}
	for i := 0; i < 2; i++ {
		got[p.Get().(string)]++
	}
	fmt.Println(got["a"], got["b"])

	// 2. New func via field assignment: empty pool calls New.
	var p2 sync.Pool
	p2.New = func() any { return 42 }
	fmt.Println(p2.Get().(int))
	p2.Put(7)
	fmt.Println(p2.Get().(int))
	fmt.Println(p2.Get().(int)) // pool drained -> New again

	// 3. New func via keyed literal (value and pointer forms).
	p3 := sync.Pool{New: func() any { return "new" }}
	fmt.Println(p3.Get().(string))
	p4 := &sync.Pool{New: func() any { return "newptr" }}
	fmt.Println(p4.Get().(string))

	// 4. Put(nil) is a no-op; Get on an empty pool without New is nil.
	var p5 sync.Pool
	p5.Put(nil)
	fmt.Println(p5.Get() == nil)

	// 5. New field read: nil before assignment, callable after.
	var p6 sync.Pool
	fmt.Println(p6.New == nil)
	p6.New = func() any { return 9 }
	fmt.Println(p6.New().(int))
	g := p6.New
	fmt.Println(g().(int))

	// 6. Concurrent Put from goroutines, then drain: the sum is
	// order-independent.
	var p7 sync.Pool
	var wg sync.WaitGroup
	const n = 8
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			p7.Put(v)
		}(i)
	}
	wg.Wait()
	sum := 0
	for i := 0; i < n; i++ {
		sum += p7.Get().(int)
	}
	fmt.Println(sum)
	fmt.Println(p7.Get() == nil) // drained, no New -> nil
}
