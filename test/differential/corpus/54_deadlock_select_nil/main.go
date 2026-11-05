package main

// RV-P1-19: all-nil select — every case is on a nil channel, so no case
// can ever become ready; gc prints "fatal error: all goroutines are
// asleep - deadlock!" and exits 2 (two nil cases render [select]).
func main() {
	var ch1, ch2 chan int
	select {
	case <-ch1:
	case ch2 <- 1:
	}
}
