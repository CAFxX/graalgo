package main

import (
	"fmt"
	"sync"
	"unique"
)

// Concurrent Make from 8 goroutines over a shared value domain.
// Deterministic output: fixed result count + a single all-ok bool
// (canonicalization is order-independent).
func main() {
	const workers = 8
	const perWorker = 200
	results := make(chan bool, workers*perWorker*2)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				v := (w*perWorker + i) % 50
				h := unique.Make(v)
				results <- h == unique.Make(v)
				results <- h.Value() == v
			}
		}(w)
	}
	wg.Wait()
	close(results)
	allOk := true
	n := 0
	for ok := range results {
		if !ok {
			allOk = false
		}
		n++
	}
	fmt.Println(n) // 3200
	fmt.Println(allOk)
}
