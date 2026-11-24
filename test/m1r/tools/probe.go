// The program of test/m1r/tools.sh (graalgo.launcher.ToolsProbe): Truffle
// tools on a running Go program. "count" computes fib(10) (177 calls, two
// statements each: the if and one return) for a statement and root
// listener; "spin" keeps a goroutine in fib for a sampler. ToolsProbe pins
// the line numbers below: keep fib on lines 13-18.
package main

import (
	"fmt"
	"os"
)

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() {
	if os.Args[1] == "count" {
		fmt.Println(fib(10))
		return
	}
	done := make(chan int)
	go func() {
		s := 0
		for i := 0; i < 400; i++ {
			s += fib(22)
		}
		done <- s
	}()
	fmt.Println(<-done)
}
