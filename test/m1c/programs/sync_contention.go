// Mutex, RWMutex and WaitGroup under contention: many goroutines, every
// increment under the lock, readers and writers interleaved. A lost wakeup
// in the semaphore deadlocks the program; a broken lock loses increments.
package main

import (
	"runtime"
	"sync"
)

func main() {
	var mu sync.Mutex
	var rw sync.RWMutex
	var wg sync.WaitGroup
	n, shared, reads := 0, 0, 0
	var readMu sync.Mutex
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				mu.Lock()
				n++
				if j%50 == 0 {
					runtime.Gosched() // hold the lock across a yield: waiters park
				}
				mu.Unlock()
				if i%4 == 0 {
					rw.Lock()
					shared++
					rw.Unlock()
				} else {
					rw.RLock()
					_ = shared
					rw.RUnlock()
					readMu.Lock()
					reads++
					readMu.Unlock()
				}
			}
		}(i)
	}
	wg.Wait()
	println("mutex", n, "writes", shared, "reads", reads)

	// TryLock / TryRLock
	println("trylock", mu.TryLock(), mu.TryLock())
	mu.Unlock()
	rw.RLock()
	println("tryrlock", rw.TryRLock(), rw.TryLock())
	rw.RUnlock()
	rw.RUnlock()
	println("trylock rw", rw.TryLock())
	rw.Unlock()

	// RLocker, and a writer waiting behind readers blocks new readers.
	l := rw.RLocker()
	l.Lock()
	done := make(chan bool)
	go func() { rw.Lock(); println("writer in"); rw.Unlock(); done <- true }()
	for i := 0; i < 100; i++ {
		runtime.Gosched()
	}
	println("reader still holds")
	l.Unlock()
	<-done

	// WaitGroup.Go and reuse.
	var wg2 sync.WaitGroup
	total := 0
	for round := 0; round < 3; round++ {
		for k := 0; k < 10; k++ {
			wg2.Go(func() { mu.Lock(); total += k; mu.Unlock() })
		}
		wg2.Wait()
	}
	println("waitgroup reuse", total)
}
