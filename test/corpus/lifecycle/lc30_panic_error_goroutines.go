package main

// Error() needs other goroutines (a WaitGroup, a Mutex, a channel) while
// the crash report is rendered: they keep running until the report is
// printed (gc preprintpanics runs before the world stops).

import (
	"fmt"
	"sync"
)

type E struct{ ch chan string }

func (e E) Error() string {
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mu.Lock()
		e.ch <- "from goroutine"
		mu.Unlock()
	}()
	wg.Wait()
	return <-e.ch
}

func main() {
	fmt.Println("start")
	panic(E{ch: make(chan string, 1)})
}
