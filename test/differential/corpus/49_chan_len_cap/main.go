package main

// 49_chan_len_cap: R1-P1-1 differential — len/cap on channels.
// len tracks buffer occupancy across sends/recvs; cap is the buffer
// capacity; nil and unbuffered channels report 0/0 (gc parity).
// Single-threaded: deterministic by construction.
// (builtin println: prints to stderr in gc and graalgo alike.)
func main() {
	ch := make(chan int, 5)
	println(len(ch), cap(ch)) // 0 5
	ch <- 1
	ch <- 2
	ch <- 3
	println(len(ch), cap(ch)) // 3 5
	<-ch
	println(len(ch), cap(ch)) // 2 5
	close(ch)
	println(len(ch), cap(ch)) // 2 5 (closed, partially drained)

	unbuf := make(chan int)
	println(len(unbuf), cap(unbuf)) // 0 0

	var nilch chan int
	println(len(nilch), cap(nilch)) // 0 0
}
