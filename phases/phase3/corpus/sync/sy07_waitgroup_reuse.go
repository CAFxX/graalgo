package main

import (
	"fmt"
	"sync"
)

// WaitGroup reuse across rounds: Add/Done/Wait three times on the same
// group; the counter must return to zero each round.
func main() {
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for round := 0; round < 3; round++ {
		wg.Add(10)
		for i := 0; i < 10; i++ {
			go func() {
				mu.Lock()
				total++
				mu.Unlock()
				wg.Done()
			}()
		}
		wg.Wait()
	}
	fmt.Println("total:", total)
}
