// Recursion: call overhead, function-entry poll, int arithmetic.
package main

import "graalgo/bench/h"

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() { h.Run("fib", 30, func() int { return fib(30) }) }
