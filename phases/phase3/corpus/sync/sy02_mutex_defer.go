package main

import (
	"fmt"
	"sync"
)

// Mutex with deferred unlock (per-iteration closure, so the defer runs
// each iteration) shared across two goroutines.
func main() {
	var mu sync.Mutex
	var wg sync.WaitGroup
	turns := 0
	bump := func() {
		mu.Lock()
		defer mu.Unlock()
		turns++
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			bump()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			mu.Lock()
			turns++
			mu.Unlock()
		}
	}()
	wg.Wait()
	fmt.Println("turns:", turns)
}
