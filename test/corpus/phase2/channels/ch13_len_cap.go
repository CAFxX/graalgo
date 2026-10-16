package main

import "fmt"

// ch13: len/cap of channels — buffered (empty/partial/drained), unbuffered,
// nil, and closed. gc: len/cap of a nil channel are 0 (not a blocking op);
// timer channels are covered by timectx/tc07.
func main() {
	ch := make(chan int, 3)
	fmt.Println(len(ch), cap(ch))
	ch <- 1
	ch <- 2
	fmt.Println(len(ch), cap(ch))
	fmt.Println(<-ch)
	fmt.Println(len(ch), cap(ch))

	ub := make(chan int)
	fmt.Println(len(ub), cap(ub))

	var nilch chan int
	fmt.Println(len(nilch), cap(nilch))

	closed := make(chan int, 2)
	closed <- 7
	close(closed)
	fmt.Println(len(closed), cap(closed))
}
