package main

import "time"

// tc04: Timer Stop/Reset — deterministic booleans, no timing dependence.
func main() {
	t := time.NewTimer(time.Hour)
	if t.Stop() {
		println("stop-before-fire-true")
	} else {
		println("stop-before-fire-false")
	}
	t2 := time.NewTimer(10 * time.Millisecond)
	t2.Reset(time.Hour)
	if t2.Stop() {
		println("reset-then-stop-true")
	} else {
		println("reset-then-stop-false")
	}
}
