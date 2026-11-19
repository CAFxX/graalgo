// Starvation mode basics: a goroutine that re-acquires the mutex in a tight
// loop must not starve a waiter forever (internal/sync's starvation mode hands
// the lock to the waiter after 1 ms, with a direct handoff in the semaphore).
package main

import (
	"runtime"
	"sync"
	"sync/atomic"
)

func main() {
	var mu sync.Mutex
	var stop atomic.Bool
	var hog sync.WaitGroup
	hog.Add(1)
	go func() {
		defer hog.Done()
		for !stop.Load() {
			mu.Lock()
			for i := 0; i < 2000; i++ {
				_ = i * i
			}
			mu.Unlock()
		}
	}()
	acquired := 0
	for acquired < 50 {
		mu.Lock()
		acquired++
		mu.Unlock()
	}
	stop.Store(true)
	hog.Wait()
	println("waiter acquired", acquired, "times")

	// Many waiters behind one holder that sleeps: all of them get through,
	// each exactly once.
	var wg sync.WaitGroup
	got := make([]int, 16)
	mu.Lock()
	for i := range got {
		wg.Add(1)
		go func() { defer wg.Done(); mu.Lock(); got[i]++; mu.Unlock() }()
	}
	for i := 0; i < 5000; i++ { // waiters park for > 1 ms: starving
		runtime.Gosched()
	}
	mu.Unlock()
	wg.Wait()
	println("each once", got[0], got[7], got[15], sum(got))
}

func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
