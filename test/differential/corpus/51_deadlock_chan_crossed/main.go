package main

// RV-P1-19: crossed channel deadlock — main receives on ch1 while the
// child receives on ch2, each holding the other's send, so neither can
// ever proceed. gc prints "fatal error: all goroutines are asleep -
// deadlock!" and exits 2.
//
// (A sync.Mutex lock-order variant was planned here, but sync.Mutex is
// broken on this build — pre-existing ClassCastException
// GoPointer->Number, unrelated to the deadlock detector — so the
// two-goroutine deadlock is exercised via channels instead.)
func main() {
	ch1 := make(chan int)
	ch2 := make(chan int)
	go func() { <-ch2; ch1 <- 1 }()
	<-ch1
}
