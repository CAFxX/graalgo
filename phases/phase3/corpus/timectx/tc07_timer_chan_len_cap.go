package main

import "time"

// tc07: DIV-M3-1 regression — len/cap of After/Timer.C/Ticker.C channels
// are 0 (the runtime uses unbuffered channels).
func main() {
	ch := time.After(time.Hour)
	println(len(ch), cap(ch))
	t := time.NewTimer(time.Hour)
	println(len(t.C), cap(t.C))
	t.Stop()
	tk := time.NewTicker(time.Hour)
	println(len(tk.C), cap(tk.C))
	tk.Stop()
}
