package main

import (
	"fmt"
	"sync"
)

// `go wg.Add(n)`: the Go spec evaluates the function value and parameters
// in the calling goroutine, so the Add must observe n=1 even though n is
// reassigned before the spawned goroutine runs. The two Done calls then
// drive the counter negative in every interleaving: gc 1.27.1
// deterministically panics "sync: negative WaitGroup counter", exit 2.
//
// spin separates the racy steps so the pre-fix bug is observable: the
// spawned goroutine's argument read happens after its spin (observing
// n=2 -> "survived"), while main's Dones wait until the Add has landed.
func spin(iters int) {
	x := 0
	for i := 0; i < iters; i++ {
		x += i
	}
	_ = x
}

func readAfterSpin(n int) int {
	spin(5000000)
	return n
}

func main() {
	var wg sync.WaitGroup
	n := 1
	go wg.Add(readAfterSpin(n))
	n = 2
	spin(20000000) // let the spawned Add land before the Dones
	wg.Done()
	wg.Done()
	fmt.Println("survived")
}
