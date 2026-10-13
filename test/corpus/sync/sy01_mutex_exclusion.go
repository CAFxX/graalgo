package main

import (
	"fmt"
	"sync"
)

// Port of SyncStressTest.testMutexExclusion (Java level): 32 goroutines x
// 1000 increments under lock; the final counter must be exact.
func main() {
	var mu sync.Mutex
	var wg sync.WaitGroup
	counter := 0
	n := 32
	iters := 1000
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			for k := 0; k < iters; k++ {
				mu.Lock()
				counter++
				mu.Unlock()
			}
			wg.Done()
		}()
	}
	wg.Wait()
	fmt.Println("counter:", counter)
}
