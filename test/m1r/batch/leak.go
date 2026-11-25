// A goroutine still printing when main returns (test/m1r/batch.sh): its
// context's teardown must stop it, or its lines land in the next
// program's stderr.
package main

import "time"

func main() {
	go func() {
		for i := 0; ; i++ {
			println("leaked goroutine", i)
			time.Sleep(time.Millisecond)
		}
	}()
	time.Sleep(20 * time.Millisecond)
	println("main returns")
}
