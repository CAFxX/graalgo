package main

import (
	"fmt"
	"time"
)

func worker(done <-chan struct{}, out chan<- int) {
	n := 0
	for {
		select {
		case <-done:
			out <- n
			return
		default:
			n++
		}
	}
}

func main() {
	done := make(chan struct{})
	out := make(chan int, 1)
	go worker(done, out)
	time.Sleep(10 * time.Millisecond)
	close(done) // broadcast shutdown
	fmt.Println("worker stopped, counted to", <-out >= 0)
}
