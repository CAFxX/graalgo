// Targets the "fatal error: all goroutines are asleep - deadlock!" ending
// with several goroutines blocked (chan receive, chan send, select, mutex,
// WaitGroup) and the goroutine dump headers with wait reasons; exit 2.
package main

import "sync"

func main() {
	var mu sync.Mutex
	var wg sync.WaitGroup
	recvC := make(chan int)
	sendC := make(chan int)
	started := make(chan struct{}, 4)
	mu.Lock()
	go func() {
		started <- struct{}{}
		<-recvC
	}()
	go func() {
		started <- struct{}{}
		sendC <- 1
	}()
	go func() {
		started <- struct{}{}
		var nilc chan int
		select {
		case <-nilc:
		case nilc <- 1:
		}
	}()
	go func() {
		started <- struct{}{}
		mu.Lock()
	}()
	for i := 0; i < 4; i++ {
		<-started
	}
	println("all blocked helpers started")
	wg.Add(1)
	wg.Wait()
}
