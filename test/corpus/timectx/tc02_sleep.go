package main

import "time"

// tc02: time.Sleep — timing-sensitive, prints a fixed string only.
func main() {
	time.Sleep(20 * time.Millisecond)
	println("slept")
}
