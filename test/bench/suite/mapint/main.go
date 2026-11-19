// map[int]int: insert, lookup (comma-ok), update, delete, iteration count.
package main

import "graalgo/bench/h"

func work() int {
	m := make(map[int]int)
	for i := 0; i < 100_000; i++ {
		m[i*7] = i
	}
	t := 0
	for i := 0; i < 200_000; i++ {
		if v, ok := m[i]; ok {
			t += v
		}
	}
	for i := 0; i < 100_000; i += 2 {
		m[i*7] += 1
	}
	for i := 0; i < 100_000; i += 3 {
		delete(m, i*7)
	}
	return t + len(m) + m[14]
}

func main() { h.Run("mapint", 20, work) }
