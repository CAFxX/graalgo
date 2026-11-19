// Unbuffered channel ping-pong between two goroutines: the scheduler's
// park/unpark hop.
package main

import "graalgo/bench/h"

func work() int {
	ping := make(chan int)
	pong := make(chan int)
	go func() {
		for v := range ping {
			pong <- v + 1
		}
		close(pong)
	}()
	t := 0
	for i := 0; i < 2000; i++ {
		ping <- i
		t += <-pong
	}
	close(ping)
	<-pong
	return t
}

func main() { h.Run("chanpingpong", 10, work) }
