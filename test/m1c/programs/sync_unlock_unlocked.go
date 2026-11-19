// Unlocking an unlocked mutex is a fatal error (gc's fatal: not a panic,
// recover does not stop it, deferred calls do not run).
package main

import "sync"

func main() {
	var mu sync.Mutex
	mu.Lock()
	mu.Unlock()
	defer println("deferred: must not run")
	defer func() { println("recover:", recover() == nil) }()
	println("unlocking twice")
	mu.Unlock()
}
