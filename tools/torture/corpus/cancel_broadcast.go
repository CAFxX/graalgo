package main

// cancel_broadcast: deterministic broadcast-cancellation pattern.
//
// Eight workers block on a done channel; the main goroutine closes it,
// each worker reports through a buffered channel, and the main goroutine
// sums the worker ids. The sum (0+1+...+7 = 28) is schedule-independent,
// so the printed line is identical under graalgo and gc. This exercises
// the same broadcast-wakeup path as context cancellation without importing
// the context package (not yet executable through the product launcher on
// the current base); see gmp_compute.go.
//
// The stress-differential script compares output byte-for-byte.
func main() {
	done := make(chan struct{})
	ack := make(chan int, 8)
	for w := 0; w < 8; w++ {
		go func(w int) {
			<-done
			ack <- w
		}(w)
	}
	close(done)
	sum := 0
	for w := 0; w < 8; w++ {
		sum += <-ack
	}
	println("canceled", sum)
}
