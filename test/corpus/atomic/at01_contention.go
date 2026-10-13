package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {
	// 8 goroutines x 1000 Adds = 8000, deterministic under atomicity.
	var total atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				total.Add(1)
			}
		}()
	}
	wg.Wait()
	fmt.Println(total.Load())

	// CAS spin loop: 4 goroutines x 1000 increments = 4000.
	var spins atomic.Int64
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				for {
					old := spins.Load()
					if spins.CompareAndSwap(old, old+1) {
						break
					}
				}
			}
		}()
	}
	wg.Wait()
	fmt.Println(spins.Load())
}
