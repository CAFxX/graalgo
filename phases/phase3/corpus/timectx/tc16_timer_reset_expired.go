package main

import "time"

// tc16: Reset on an expired timer — then Stop succeeds.
func main() {
	t := time.NewTimer(10 * time.Millisecond)
	<-t.C
	t.Reset(time.Hour)
	if t.Stop() {
		println("reset-expired-stop-true")
	} else {
		println("reset-expired-stop-false")
	}
}
