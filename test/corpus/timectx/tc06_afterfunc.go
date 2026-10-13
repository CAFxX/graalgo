package main

import "time"

// tc06: time.AfterFunc — prints a fixed string only.
func main() {
	fired := make(chan bool, 1)
	t := time.AfterFunc(10*time.Millisecond, func() { fired <- true })
	<-fired
	t.Stop()
	println("afterfunc-fired")
}
