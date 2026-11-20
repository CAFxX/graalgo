// runtime.Goexit called from inside a deferred function (it runs the
// remaining defers, recover() sees nil, panics in progress are dropped),
// Goexit after recover, and main returning while goroutines stay blocked
// forever (exit status 0, no deadlock report).
package main

import (
	"runtime"
	"sync"
)

func goexitInDefer(done chan<- string) {
	defer func() { done <- "outer defer ran" }()
	defer func() {
		r := recover()
		println("recover after goexit-in-defer is nil:", r == nil)
	}()
	defer runtime.Goexit()
	println("body")
}

func goexitDuringPanic(done chan<- string) {
	defer func() { done <- "panic dropped" }()
	defer func() {
		runtime.Goexit()
	}()
	panic("this panic is abandoned by Goexit")
}

func recoverThenGoexit(done chan<- string) {
	defer func() { done <- "recovered then exited" }()
	defer func() {
		if r := recover(); r != nil {
			println("recovered:", r.(string))
			runtime.Goexit()
		}
	}()
	panic("boom")
}

func nestedGoexit(done chan<- string) {
	defer func() { done <- "nested" }()
	func() {
		defer func() {
			println("inner defer, recover nil:", recover() == nil)
		}()
		runtime.Goexit()
	}()
	println("unreachable after inner goexit")
}

func goexitWithResult(out *[3]int, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { out[2] = 3 }()
	out[0] = 1
	func() {
		defer func() { out[1] = 2 }()
		runtime.Goexit()
	}()
	out[0] = 100
}

func main() {
	done := make(chan string)
	go goexitInDefer(done)
	println(<-done)
	go goexitDuringPanic(done)
	println(<-done)
	go recoverThenGoexit(done)
	println(<-done)
	go nestedGoexit(done)
	println(<-done)

	var wg sync.WaitGroup
	var res [3]int
	wg.Add(1)
	go goexitWithResult(&res, &wg)
	wg.Wait()
	println("result", res[0], res[1], res[2])

	var once sync.Once
	wg.Add(1)
	go func() {
		defer wg.Done()
		once.Do(func() { runtime.Goexit() })
	}()
	wg.Wait()
	ran := false
	once.Do(func() { ran = true })
	println("once after goexit inside Do, ran again:", ran)

	never := make(chan int)
	var mu sync.Mutex
	mu.Lock()
	for i := 0; i < 4; i++ {
		go func() { <-never }()
		go func() { mu.Lock() }()
	}
	go func() {
		select {}
	}()
	cond := sync.NewCond(&sync.Mutex{})
	go func() {
		cond.L.Lock()
		cond.Wait()
	}()
	runtime.Gosched()
	println("main returns with leaked goroutines")
}
