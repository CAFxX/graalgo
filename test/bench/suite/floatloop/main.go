// Floating-point loop: float64 arithmetic and int/float conversions.
package main

import "graalgo/bench/h"

func work(n int) int {
	s := 0.0
	for i := 0; i < n; i++ {
		x := float64(i)
		s += x*0.5/(1+float64(i&7)) - s*1e-9
	}
	return int(s)
}

func main() { h.Run("floatloop", 30, func() int { return work(5_000_000) }) }
