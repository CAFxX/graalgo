// Targets sync.Mutex/RWMutex mutual exclusion counters, TryLock/TryRLock,
// WaitGroup.Go (go1.25+), Cond Signal/Broadcast with a predicate loop, and
// the fatal-free usage of RWMutex readers concurrently. Prints only totals.
package main

import (
	"runtime"
	"sync"
)

type counter struct {
	mu sync.Mutex
	n  int
}

func main() {
	runtime.GOMAXPROCS(0)
	var c counter
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			for j := 0; j < 500; j++ {
				c.mu.Lock()
				c.n++
				c.mu.Unlock()
				if j%100 == 0 {
					runtime.Gosched()
				}
			}
		})
	}
	wg.Wait()
	println("mutex total", c.n)

	var m sync.Mutex
	println("trylock free", m.TryLock())
	println("trylock held", m.TryLock())
	m.Unlock()
	println("trylock again", m.TryLock())
	m.Unlock()

	var rw sync.RWMutex
	rw.RLock()
	println("tryrlock with reader", rw.TryRLock())
	println("trylock with readers", rw.TryLock())
	rw.RUnlock()
	rw.RUnlock()
	println("trylock no readers", rw.TryLock())
	println("tryrlock with writer", rw.TryRLock())
	rw.Unlock()

	shared := map[int]int{}
	for i := 0; i < 10; i++ {
		shared[i] = i * 10
	}
	var rsum [8]int
	var rwg sync.WaitGroup
	for r := 0; r < 8; r++ {
		rwg.Go(func() {
			for k := 0; k < 50; k++ {
				rw.RLock()
				rsum[r] += shared[k%10]
				rw.RUnlock()
			}
		})
	}
	for w := 0; w < 3; w++ {
		rwg.Go(func() {
			rw.Lock()
			shared[100+w] = w
			rw.Unlock()
		})
	}
	rwg.Wait()
	tot := 0
	for _, s := range rsum {
		tot += s
	}
	println("rw reads total", tot, "len", len(shared))

	// Cond: producers add items, consumers wait for predicate
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	queue := []int{}
	consumed := 0
	sum := 0
	var cwg sync.WaitGroup
	for k := 0; k < 4; k++ {
		cwg.Go(func() {
			for {
				mu.Lock()
				for len(queue) == 0 && consumed < 40 {
					cond.Wait()
				}
				if consumed >= 40 {
					mu.Unlock()
					return
				}
				v := queue[0]
				queue = queue[1:]
				consumed++
				sum += v
				if consumed == 40 {
					cond.Broadcast()
				}
				mu.Unlock()
			}
		})
	}
	for i := 1; i <= 40; i++ {
		mu.Lock()
		queue = append(queue, i)
		cond.Signal()
		mu.Unlock()
	}
	cwg.Wait()
	println("cond consumed", consumed, "sum", sum)

	// Broadcast wakes all waiters on a gate
	ready := false
	woke := 0
	var gwg sync.WaitGroup
	for i := 0; i < 6; i++ {
		gwg.Go(func() {
			mu.Lock()
			for !ready {
				cond.Wait()
			}
			woke++
			mu.Unlock()
		})
	}
	mu.Lock()
	ready = true
	cond.Broadcast()
	mu.Unlock()
	gwg.Wait()
	println("broadcast woke", woke)
}
