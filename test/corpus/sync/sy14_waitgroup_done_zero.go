package main

import "sync"

// Done on a zero WaitGroup counter. gc 1.27.1: panic
// "sync: negative WaitGroup counter", exit 2. graalgo M2: same Go panic
// (ADR-0002 D4 — full parity: gc panics here too).
func main() {
	var wg sync.WaitGroup
	wg.Done()
}
