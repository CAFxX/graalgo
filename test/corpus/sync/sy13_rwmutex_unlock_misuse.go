package main

import "sync"

// Unlock of an unlocked RWMutex. gc 1.27.1: fatal error (unrecoverable),
// exit 2: "sync: Unlock of unlocked RWMutex" (capital U). graalgo M2:
// Go panic with gc's message text (ADR-0002 D4 — scoped divergence:
// recoverable here, same message, same exit code).
func main() {
	var rw sync.RWMutex
	rw.Unlock()
}
