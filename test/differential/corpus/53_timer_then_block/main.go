package main

import "time"

// RV-P1-19 (gc probe p5): a pending timer suppresses the deadlock
// report; the sleep fires, the channel delivers, exit 0.
func main() {
	done := make(chan bool)
	go func() {
		time.Sleep(50 * time.Millisecond)
		done <- true
	}()
	<-done
}
