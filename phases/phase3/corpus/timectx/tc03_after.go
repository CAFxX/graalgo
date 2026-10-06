package main

import "time"

// tc03: time.After — timing-sensitive, prints a fixed string only.
func main() {
	<-time.After(20 * time.Millisecond)
	println("after-fired")
}
