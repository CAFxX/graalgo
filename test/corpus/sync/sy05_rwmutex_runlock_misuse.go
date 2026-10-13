package main

import "sync"

// RUnlock of an unlocked RWMutex. gc 1.27.1: fatal error
// (unrecoverable), exit 2. graalgo M2: Go panic with gc's message text
// (ADR-0002 D4 — scoped divergence, same message, same exit code).
func main() {
	var rw sync.RWMutex
	rw.RUnlock()
}
