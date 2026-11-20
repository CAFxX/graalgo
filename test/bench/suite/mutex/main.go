// sync.Mutex under contention: four goroutines increment a shared counter.
package main

import (
	"sync"

	"graalgo/bench/h"
)

func work() int {
	var mu sync.Mutex
	var wg sync.WaitGroup
	count := 0
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20_000; i++ {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return count
}

func main() { h.Run("mutex", 10, work) }
