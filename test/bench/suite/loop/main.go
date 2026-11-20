// Tight nested integer loops: the back-edge poll and integer arithmetic.
package main

import "graalgo/bench/h"

func work(n int) int {
	s := 0
	for i := 0; i < n; i++ {
		for j := 0; j < 1000; j++ {
			s += (i ^ j) & 15
		}
	}
	return s
}

func main() { h.Run("loop", 30, func() int { return work(20000) }) }
