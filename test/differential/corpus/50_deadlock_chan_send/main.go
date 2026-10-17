package main

// RV-P1-19: send on an unbuffered channel with no receiver blocks
// forever — gc fatal deadlock, exit 2.
func main() {
	ch := make(chan int)
	ch <- 1
}
