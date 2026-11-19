// Deadlock through sync: the scheduler's report names gc's wait reasons
// (sync.Mutex.Lock, sync.RWMutex.Lock/RLock, sync.Cond.Wait,
// sync.WaitGroup.Wait).
package main

import "sync"

func main() {
	var mu sync.Mutex
	var rw sync.RWMutex
	c := sync.NewCond(&sync.Mutex{})
	mu.Lock()
	rw.Lock()
	go func() { mu.Lock() }()
	go func() { rw.RLock() }()
	go func() { c.L.Lock(); c.Wait() }()
	var wg sync.WaitGroup
	wg.Add(1)
	println("waiting")
	wg.Wait()
}
