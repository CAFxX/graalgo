package main

import "fmt"

// R2313-P1-1: select receive into a target captured by a goroutine
// closure in the case body, with the select inside a loop (the
// loopcap prescan boxes the loop-body target). The exporter must
// receive into a plain temp and copy back.
func main() {
	ch := make(chan int, 1)
	ch <- 7
	done := make(chan bool, 1)
	for i := 0; i < 1; i++ {
		_ = i
		select {
		case v := <-ch:
			go func() { fmt.Println(v); done <- true }()
		}
	}
	<-done
}
