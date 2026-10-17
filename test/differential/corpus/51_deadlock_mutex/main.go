package main

import "sync"

// RV-P1-19: deterministic lock-order deadlock — the child holds mu2
// and waits on mu1 while main holds mu1 and waits on mu2 (the ready
// channel fixes the acquisition order). gc fatal deadlock, exit 2.
func main() {
	var mu1, mu2 sync.Mutex
	ready := make(chan bool)
	mu1.Lock()
	go func() {
		mu2.Lock()
		ready <- true
		mu1.Lock()
		mu1.Unlock()
		mu2.Unlock()
	}()
	<-ready
	mu2.Lock()
}
