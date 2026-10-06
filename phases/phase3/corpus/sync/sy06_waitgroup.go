package main

import (
	"fmt"
	"sync"
)

// Port of SyncStressTest.testWaitGroup (Java level): 50 workers, main
// awaits; all workers observed, counter back at zero.
func main() {
	var wg sync.WaitGroup
	var mu sync.Mutex
	counter := 0
	n := 50
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			mu.Lock()
			counter++
			mu.Unlock()
			wg.Done()
		}()
	}
	wg.Wait()
	fmt.Println("counter:", counter)
}
