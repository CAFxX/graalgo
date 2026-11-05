package main

import (
	"runtime"
	"time"
)

// RV-P1-19: main Goexit'ed with a pending timer translator live — gc
// prints "fatal error: no goroutines (main called runtime.Goexit) -
// deadlock!" and exits 2 (a pending timer is not a goroutine; zero
// goroutines remain). Port regression: awaiting quiescence before
// draining the translators hangs (the translator's heap timer stays
// pending, suppressing the deadlock detector); counting the translator
// as a live goroutine in the C2 decision would exit 0 instead.
func main() {
	_ = time.NewTicker(time.Hour)
	runtime.Goexit()
}
