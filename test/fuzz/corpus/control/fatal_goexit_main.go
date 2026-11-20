// Targets runtime.Goexit called on the main goroutine: deferred calls of
// main run, other goroutines finish, and then gc reports
// "fatal error: no goroutines (main called runtime.Goexit) - deadlock!"; exit 2.
package main

import (
	"runtime"
	"sync"
)

func main() {
	var wg sync.WaitGroup
	var mu sync.Mutex
	sum := 0
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			sum += i
			mu.Unlock()
		}()
	}
	defer func() {
		println("main defer: recover is nil:", recover() == nil)
	}()
	defer println("main defer 1")
	done := make(chan struct{})
	go func() {
		wg.Wait()
		mu.Lock()
		println("helper sees sum", sum)
		mu.Unlock()
		close(done)
		runtime.Gosched()
	}()
	<-done
	runtime.Goexit()
	println("unreachable")
}
