// sync.Cond: Signal wakes waiters in the order they started waiting (the
// notify list's tickets); Broadcast wakes all of them.
package main

import (
	"runtime"
	"sync"
)

func main() {
	var mu sync.Mutex
	c := sync.NewCond(&mu)
	const n = 12
	var arrived, woken []int
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			arrived = append(arrived, i)
			c.Wait()
			woken = append(woken, i)
			mu.Unlock()
		}()
	}
	waitFor := func(cond func() bool) {
		for {
			mu.Lock()
			ok := cond()
			mu.Unlock()
			if ok {
				return
			}
			runtime.Gosched()
		}
	}
	waitFor(func() bool { return len(arrived) == n })
	for k := 1; k <= n/2; k++ {
		mu.Lock()
		c.Signal()
		mu.Unlock()
		waitFor(func() bool { return len(woken) == k })
	}
	fifo := true
	for k := 0; k < n/2; k++ {
		if woken[k] != arrived[k] {
			fifo = false
		}
	}
	println("signal order is wait order:", fifo)
	mu.Lock()
	c.Broadcast()
	mu.Unlock()
	wg.Wait()
	println("woken", len(woken))

	// Signal and Broadcast with no waiter are no-ops; a later waiter is not
	// woken by an earlier signal.
	c.Signal()
	c.Broadcast()
	done := make(chan bool)
	go func() { mu.Lock(); c.Wait(); mu.Unlock(); done <- true }()
	for i := 0; i < 50; i++ {
		runtime.Gosched()
	}
	select {
	case <-done:
		println("woken by a stale signal")
	default:
		println("still waiting")
	}
	for {
		mu.Lock()
		c.Signal()
		mu.Unlock()
		select {
		case <-done:
			println("signalled")
			return
		default:
			runtime.Gosched()
		}
	}
}
