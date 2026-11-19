// Worker pool: buffered job and result channels, four workers,
// sync.WaitGroup.
package main

import (
	"sync"

	"graalgo/bench/h"
)

func work() int {
	jobs := make(chan int, 64)
	results := make(chan int, 64)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				s := 0
				for k := 0; k < 200; k++ {
					s += (j * k) & 31
				}
				results <- s
			}
		}()
	}
	go func() {
		for i := 0; i < 5000; i++ {
			jobs <- i
		}
		close(jobs)
	}()
	go func() { wg.Wait(); close(results) }()
	t := 0
	for r := range results {
		t += r
	}
	return t
}

func main() { h.Run("workerpool", 10, work) }
