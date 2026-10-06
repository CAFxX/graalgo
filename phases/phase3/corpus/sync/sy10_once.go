package main

import (
	"fmt"
	"sync"
)

// Port of SyncStressTest.testOnce (Java level): 32 goroutines race Do;
// the action runs exactly once; a second wave does not re-run it.
func main() {
	var once sync.Once
	var wg sync.WaitGroup
	runs := 0
	n := 32
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			once.Do(func() { runs++ })
		}()
	}
	wg.Wait()
	once.Do(func() { runs++ })
	fmt.Println("runs:", runs)
}
