// Benchmark: unbuffered channel ping-pong between two goroutines.
package main

import (
	"fmt"
	"os"
	"time"
)

func work(n int) int {
	ping := make(chan int)
	pong := make(chan int)
	go func() {
		for v := range ping {
			pong <- v + 1
		}
		close(pong)
	}()
	t := 0
	for i := 0; i < n; i++ {
		ping <- i
		t += <-pong
	}
	close(ping)
	<-pong
	return t
}

func main() {
	var r int
	best := time.Duration(1 << 62)
	for i := 0; i < 10; i++ {
		t0 := time.Now()
		r = work(20000)
		if d := time.Since(t0); d < best {
			best = d
		}
	}
	fmt.Println(r)
	fmt.Fprintf(os.Stderr, "BENCH chanpingpong %d\n", best.Nanoseconds())
}
