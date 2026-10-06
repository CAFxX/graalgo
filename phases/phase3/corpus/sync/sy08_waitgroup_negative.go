package main

import "sync"

// Negative WaitGroup counter: a real Go panic in gc 1.27.1
// ("panic: sync: negative WaitGroup counter", exit 2) — full parity,
// recoverable on both.
func main() {
	var wg sync.WaitGroup
	wg.Add(-1)
}
