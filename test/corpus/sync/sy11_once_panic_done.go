package main

import (
	"fmt"
	"sync"
)

// If the Do func panics, the Once still counts as done: the panic
// propagates (recovered here), and a later Do does not re-run the action
// (gc parity).
func main() {
	var once sync.Once
	runs := 0
	func() {
		defer func() { recover() }()
		once.Do(func() {
			runs++
			panic("boom")
		})
	}()
	once.Do(func() { runs++ })
	fmt.Println("runs:", runs, "recovered")
}
