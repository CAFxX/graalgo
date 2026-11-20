// Targets an unrecovered panic in a non-main goroutine killing the whole
// program while main is blocked: the panic text from a runtime error
// (integer divide by zero) and the "goroutine N [running]:" header; exit 2.
package main

import "sync"

func divide(a, b int) int { return a / b }

func main() {
	var wg sync.WaitGroup
	results := make([]int, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = divide(100, i+1)
		}()
	}
	wg.Wait()
	for i, r := range results {
		println("result", i, r)
	}
	block := make(chan int)
	go func() {
		defer println("goroutine defer runs before death")
		zero := len(results) - 4
		block <- divide(1, zero)
	}()
	<-block
	println("unreachable")
}
