package main

// goroutine_storm: scheduler admission under a spawn burst.
//
// Spawns 500 goroutines; each does a small amount of compute, then signals
// completion through a buffered channel. The main goroutine drains the
// channel and prints the count. The count is deterministic (500); the gate
// is clean completion (exit 0, no lost wakeup, no hang) on both graalgo
// and gc — a scheduler-level stress the Java-side F6 leg also covers.
//
// (A finalizer storm would be the ideal runtime-pattern counterpart, but
// runtime.SetFinalizer is not yet executable through the product launcher
// on the current base; see gmp_compute.go.)
//
// The stress-differential script runs this in "completion" mode.
func main() {
	const N = 500
	done := make(chan int, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			x := i
			for k := 0; k < 100; k++ {
				x = x*31 + k
			}
			_ = x
			done <- i
		}(i)
	}
	count := 0
	for i := 0; i < N; i++ {
		<-done
		count++
	}
	println("done", count)
}
