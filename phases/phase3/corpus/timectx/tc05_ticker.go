package main

import "time"

// tc05: Ticker — 3 ticks then Stop. Prints fixed strings only.
func main() {
	tk := time.NewTicker(10 * time.Millisecond)
	for i := 0; i < 3; i++ {
		<-tk.C
	}
	tk.Stop()
	println("three-ticks")
}
