// Closures: captured variables, func values passed to higher-order
// functions, closures returned from functions.
package main

import "graalgo/bench/h"

func counter() func() int {
	c := 0
	return func() int { c++; return c }
}

func apply(xs []int, f func(int) int) int {
	t := 0
	for _, x := range xs {
		t += f(x)
	}
	return t
}

func work() int {
	xs := make([]int, 1000)
	for i := range xs {
		xs[i] = i
	}
	t := 0
	k := 3
	for r := 0; r < 3000; r++ {
		t += apply(xs, func(x int) int { return x*k + r })
		next := counter()
		for i := 0; i < 100; i++ {
			t += next()
		}
	}
	return t
}

func main() { h.Run("closures", 30, work) }
