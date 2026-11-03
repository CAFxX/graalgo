package main

import "fmt"

// ch13: len()/cap() on channels (REVIEW-2026-11-03-16 P0-2, lane L-B).
// Buffered: len tracks sends/recvs, cap is fixed and survives close.
// Unbuffered: 0/0. Closed: len drains with the buffer, cap unchanged.
// Nil: 0/0 without blocking (gc parity).
func main() {
	buf := make(chan int, 3)
	fmt.Println("buf empty:", len(buf), cap(buf))
	buf <- 1
	buf <- 2
	fmt.Println("buf two:", len(buf), cap(buf))
	fmt.Println("recv:", <-buf)
	fmt.Println("buf one:", len(buf), cap(buf))
	close(buf)
	fmt.Println("buf closed:", len(buf), cap(buf))
	fmt.Println("recv:", <-buf)
	fmt.Println("buf drained:", len(buf), cap(buf))

	unbuf := make(chan int)
	fmt.Println("unbuf:", len(unbuf), cap(unbuf))

	var nilch chan int
	fmt.Println("nil:", len(nilch), cap(nilch))
	fmt.Println("done")
}
