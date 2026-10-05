package main

import (
	"fmt"
	"time"
)

// tm14: Reset on a live ticker with a buffered tick drains the stale
// tick before re-arming. The next receive is a fresh post-reset tick,
// not the stale pre-reset one.
func main() {
	tk := time.NewTicker(20 * time.Millisecond)
	time.Sleep(300 * time.Millisecond) // 1 tick buffered
	tk.Reset(150 * time.Millisecond)
	fmt.Println("reset")
	t0 := time.Now()
	<-tk.C
	dt := time.Since(t0)
	// Fresh tick arrives ~150ms after reset; a stale tick would arrive
	// immediately. Report order only (no wall-clock values).
	if dt > 75*time.Millisecond {
		fmt.Println("fresh tick")
	} else {
		fmt.Println("STALE tick")
	}
	tk.Stop()
}
