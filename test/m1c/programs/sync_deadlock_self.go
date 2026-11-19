// Locking a held mutex from its only goroutine is a deadlock.
package main

import "sync"

func main() {
	var rw sync.RWMutex
	rw.RLock()
	println("lock while reading")
	rw.Lock()
}
