// RUnlock of an unlocked RWMutex is a fatal error with gc's text.
package main

import "sync"

func main() {
	var rw sync.RWMutex
	rw.RLock()
	rw.RUnlock()
	println("runlock twice")
	rw.RUnlock()
}
